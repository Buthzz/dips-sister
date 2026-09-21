// Package registry mengimplementasikan home-based naming untuk kluster node.
//
// Konsep Naming (Slide 05 — Penamaan):
//   - Flat naming: "node-1#a3f9" adalah nama datar (opaque, tidak mengandung lokasi)
//   - Home-based approach: master bertindak sebagai "rumah" yang memetakan nama ke alamat
//   - Resolusi: scheduler bertanya "di mana node-2?" → registry menjawab "192.168.1.12:9000"
//
// Deteksi kegagalan menggunakan mekanisme heartbeat (DESIGN.md §7.1):
//   - Node mengirim heartbeat setiap 2 detik
//   - Master menandai node mati setelah 6 detik tanpa heartbeat (3× interval)
//   - Node yang mati bisa pulih dengan mendaftar ulang (sesi baru atau heartbeat lagi)
package registry

import (
	"log/slog"
	"sync"
	"time"
)

// NodeStatus mewakili kondisi kesehatan sebuah node.
type NodeStatus string

const (
	// StatusAlive: node aktif mengirim heartbeat dan siap menerima task.
	StatusAlive NodeStatus = "alive"
	// StatusDead: node tidak merespons dalam batas waktu; tasknya perlu dijadwalkan ulang.
	StatusDead NodeStatus = "dead"
)

// NodeInfo menyimpan semua informasi yang master ketahui tentang satu node.
type NodeInfo struct {
	// NodeID adalah nama datar yang ditetapkan oleh operator (misal "node-1").
	NodeID string

	// SessionID dibuat secara acak setiap node restart, memungkinkan master
	// membedakan antara node yang reboot versus node berbeda dengan nama sama.
	SessionID string

	// AdvertiseAddr adalah IP:port yang digunakan master untuk memanggil ProcessImage.
	// Nilainya diisi dari flag --advertise atau hasil auto-detect IP.
	AdvertiseAddr string

	// Capacity adalah jumlah goroutine pemroses yang tersedia di node tersebut.
	Capacity int

	// ActiveTasks adalah jumlah task yang sedang berjalan, dilaporkan via heartbeat.
	ActiveTasks int

	Status        NodeStatus
	LastHeartbeat time.Time
	RegisteredAt  time.Time
}

// Registry adalah penyimpanan thread-safe untuk seluruh informasi node kluster.
type Registry struct {
	mu      sync.RWMutex
	nodes   map[string]*NodeInfo // diindeks oleh NodeID
	timeout time.Duration        // durasi tanpa heartbeat sebelum node dianggap mati
	log     *slog.Logger
}

// New membuat Registry baru. timeout menentukan kapan node dianggap mati.
// Disarankan nilai 3× heartbeat interval untuk toleransi delay jaringan sesaat.
func New(timeout time.Duration, log *slog.Logger) *Registry {
	return &Registry{
		nodes:   make(map[string]*NodeInfo),
		timeout: timeout,
		log:     log,
	}
}

// Register mendaftarkan atau memperbarui informasi node. Mengembalikan true jika diterima.
//
// Logika session:
//   - Session sama: perbarui alamat dan tandai alive (node setelah hiatus singkat)
//   - Session berbeda: node sudah restart; entry lama ditimpa sepenuhnya
func (r *Registry) Register(nodeID, sessionID, advertiseAddr string, capacity int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	if existing, ok := r.nodes[nodeID]; ok && existing.SessionID == sessionID {
		// Sesi yang sama: cukup refresh tanpa mengganti entry.
		existing.AdvertiseAddr = advertiseAddr
		existing.Capacity = capacity
		existing.Status = StatusAlive
		existing.LastHeartbeat = now
		r.log.Info("node mendaftar ulang (sesi sama)",
			slog.String("node_id", nodeID),
			slog.String("addr", advertiseAddr))
		return true
	}

	// Sesi baru: node restart atau node baru.
	r.nodes[nodeID] = &NodeInfo{
		NodeID:        nodeID,
		SessionID:     sessionID,
		AdvertiseAddr: advertiseAddr,
		Capacity:      capacity,
		Status:        StatusAlive,
		LastHeartbeat: now,
		RegisteredAt:  now,
	}
	r.log.Info("node terdaftar",
		slog.String("node_id", nodeID),
		slog.String("addr", advertiseAddr),
		slog.Int("capacity", capacity))
	return true
}

// Heartbeat memperbarui waktu terakhir node aktif dan jumlah task yang sedang berjalan.
// Mengembalikan false jika node tidak dikenal atau menggunakan sesi lama.
func (r *Registry) Heartbeat(nodeID, sessionID string, activeTasks int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, ok := r.nodes[nodeID]
	if !ok || n.SessionID != sessionID {
		// Node belum register atau sesi sudah kedaluwarsa — tolak heartbeat.
		return false
	}

	n.LastHeartbeat = time.Now()
	n.ActiveTasks = activeTasks

	if n.Status == StatusDead {
		// Node pulih kembali setelah sempat dianggap mati.
		n.Status = StatusAlive
		r.log.Info("node pulih via heartbeat", slog.String("node_id", nodeID))
	}
	return true
}

// Resolve mengembalikan alamat tujuan untuk node yang hidup (name → address).
// Mengembalikan ("", false) jika node tidak dikenal atau sudah mati.
func (r *Registry) Resolve(nodeID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.nodes[nodeID]
	if !ok || n.Status == StatusDead {
		return "", false
	}
	return n.AdvertiseAddr, true
}

// AliveNodes mengembalikan snapshot semua node yang saat ini hidup.
// Snapshot aman dari modifikasi konkuren karena nilai di-copy keluar dari lock.
func (r *Registry) AliveNodes() []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NodeInfo, 0, len(r.nodes))
	for _, n := range r.nodes {
		if n.Status == StatusAlive {
			out = append(out, *n)
		}
	}
	return out
}

// AllNodes mengembalikan snapshot semua node (hidup maupun mati).
// Digunakan oleh REST endpoint GET /api/v1/nodes untuk menampilkan status kluster.
func (r *Registry) AllNodes() []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NodeInfo, 0, len(r.nodes))
	for _, n := range r.nodes {
		out = append(out, *n)
	}
	return out
}

// TickDeadCheck memeriksa semua node dan menandai yang sudah melewati batas timeout.
// Mengembalikan slice NodeID yang baru saja ditandai mati dalam iterasi ini.
//
// Fungsi ini harus dipanggil secara periodik (misalnya setiap detik) oleh goroutine
// monitor di main. NodeID yang dikembalikan akan dipakai scheduler untuk menjadwalkan
// ulang task yang sebelumnya ditugaskan ke node tersebut.
func (r *Registry) TickDeadCheck() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	var dead []string
	for _, n := range r.nodes {
		if n.Status == StatusAlive && now.Sub(n.LastHeartbeat) > r.timeout {
			n.Status = StatusDead
			dead = append(dead, n.NodeID)
			r.log.Warn("node dinyatakan mati",
				slog.String("node_id", n.NodeID),
				slog.Time("last_heartbeat", n.LastHeartbeat),
				slog.Duration("timeout", r.timeout))
		}
	}
	return dead
}
