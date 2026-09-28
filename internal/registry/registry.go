// Package registry mengelola penamaan node (home-based naming) dan deteksi kesehatan cluster.
package registry

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// NodeStatus menyatakan status operasional sebuah node.
type NodeStatus string

const (
	StatusAlive NodeStatus = "alive"
	StatusDead  NodeStatus = "dead"
)

// NodeInfo menyimpan metadata dan status liveness satu node.
type NodeInfo struct {
	NodeID        string
	SessionID     string
	AdvertiseAddr string
	Capacity      int
	ActiveTasks   int
	Status        NodeStatus
	LastHeartbeat time.Time
	RegisteredAt  time.Time
}

// Registry menyimpan informasi semua node dalam cluster secara thread-safe.
type Registry struct {
	mu      sync.RWMutex
	nodes   map[string]*NodeInfo
	timeout time.Duration
	log     *slog.Logger
}

// New membuat Registry baru dengan batas waktu deteksi node mati.
func New(timeout time.Duration, log *slog.Logger) *Registry {
	return &Registry{
		nodes:   make(map[string]*NodeInfo),
		timeout: timeout,
		log:     log,
	}
}

// Register mendaftarkan node baru. Mengembalikan true jika registrasi berhasil diterima.
func (r *Registry) Register(nodeID, sessionID, advertiseAddr string, capacity int) bool {
	ok, _, _ := r.RegisterNode(nodeID, sessionID, advertiseAddr, capacity)
	return ok
}

// RegisterNode mendaftarkan node baru dan mengembalikan status, ID yang dialokasikan, dan pesan alasan.
// Jika nodeID bernilai kosong atau "auto", master secara otomatis mengalokasikan ID (node-1, node-2, dst).
func (r *Registry) RegisterNode(nodeID, sessionID, advertiseAddr string, capacity int) (bool, string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cleanID := strings.TrimSpace(nodeID)

	// Alokasi otomatis jika ID tidak ditentukan atau "auto"
	if cleanID == "" || strings.EqualFold(cleanID, "auto") {
		// Cek apakah alamat host ini sudah pernah terdaftar sebelumnya (sticky assignment saat restart)
		assigned := ""
		for id, node := range r.nodes {
			if node.AdvertiseAddr == advertiseAddr {
				assigned = id
				break
			}
		}

		// Jika belum pernah terdaftar atau alamatnya baru, cari nomor node-1, node-2, dst yang tersedia
		if assigned == "" {
			for i := 1; ; i++ {
				candidate := fmt.Sprintf("node-%d", i)
				existing, exists := r.nodes[candidate]
				if !exists {
					assigned = candidate
					break
				}
				// Jika kandidat sudah berstatus dead dan melebihi batas timeout, dapat dialokasikan ulang
				if existing.Status == StatusDead && now.Sub(existing.LastHeartbeat) >= r.timeout {
					assigned = candidate
					break
				}
			}
		}
		cleanID = assigned
	}

	if existing, ok := r.nodes[cleanID]; ok {
		// Sesi sama: refresh heartbeat & kapasitas biasa
		if existing.SessionID == sessionID {
			existing.AdvertiseAddr = advertiseAddr
			existing.Capacity = capacity
			existing.Status = StatusAlive
			existing.LastHeartbeat = now
			r.log.Info("node refresh registrasi",
				slog.String("node_id", cleanID),
				slog.String("addr", advertiseAddr))
			return true, cleanID, "ok"
		}

		// Sesi beda dari alamat berbeda: tolak jika node lama masih berstatus alive
		if existing.AdvertiseAddr != advertiseAddr &&
			existing.Status == StatusAlive &&
			now.Sub(existing.LastHeartbeat) < r.timeout {
			errMsg := fmt.Sprintf("node ID '%s' sedang aktif digunakan oleh worker di %s (gunakan flag --node-id berbeda)", cleanID, existing.AdvertiseAddr)
			r.log.Warn("registrasi ditolak: konflik identitas node",
				slog.String("node_id", cleanID),
				slog.String("alamat_aktif", existing.AdvertiseAddr),
				slog.String("alamat_baru", advertiseAddr))
			return false, cleanID, errMsg
		}

		// Sesi beda dari alamat yang sama (restart node di laptop sama),
		// atau node lama sudah mati (> timeout): sesi baru diterima dan menimpa yang lama
	}

	r.nodes[cleanID] = &NodeInfo{
		NodeID:        cleanID,
		SessionID:     sessionID,
		AdvertiseAddr: advertiseAddr,
		Capacity:      capacity,
		Status:        StatusAlive,
		LastHeartbeat: now,
		RegisteredAt:  now,
	}
	r.log.Info("node terdaftar",
		slog.String("node_id", cleanID),
		slog.String("addr", advertiseAddr),
		slog.Int("capacity", capacity))
	return true, cleanID, "ok"
}

// Heartbeat memperbarui timestamp aktif terakhir dari node.
func (r *Registry) Heartbeat(nodeID, sessionID string, activeTasks int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, ok := r.nodes[nodeID]
	if !ok || n.SessionID != sessionID {
		return false
	}

	n.LastHeartbeat = time.Now()
	n.ActiveTasks = activeTasks

	if n.Status == StatusDead {
		n.Status = StatusAlive
		r.log.Info("node pulih via heartbeat", slog.String("node_id", nodeID))
	}
	return true
}

// Deregister menandai node sebagai dead secara eksplisit (graceful shutdown).
// Mengembalikan true jika node ditemukan dan session cocok.
func (r *Registry) Deregister(nodeID, sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, ok := r.nodes[nodeID]
	if !ok {
		return false
	}
	if n.SessionID != sessionID {
		return false
	}

	n.Status = StatusDead
	n.ActiveTasks = 0
	r.log.Info("node deregister (graceful shutdown)",
		slog.String("node_id", nodeID),
		slog.String("session", sessionID))
	return true
}

// Resolve mengembalikan alamat jaringan node aktif berdasarkan ID-nya.
func (r *Registry) Resolve(nodeID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.nodes[nodeID]
	if !ok || n.Status == StatusDead {
		return "", false
	}
	return n.AdvertiseAddr, true
}

// AliveNodes mengembalikan daftar seluruh node yang saat ini aktif.
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

// AllNodes mengembalikan daftar seluruh node baik yang hidup maupun yang mati.
func (r *Registry) AllNodes() []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NodeInfo, 0, len(r.nodes))
	for _, n := range r.nodes {
		out = append(out, *n)
	}
	return out
}

// GetNode mengembalikan salinan NodeInfo berdasarkan node ID.
// Mengembalikan nil jika node tidak ditemukan.
func (r *Registry) GetNode(nodeID string) *NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.nodes[nodeID]
	if !ok {
		return nil
	}
	cp := *n
	return &cp
}


// TickDeadCheck memindai node dan menandai yang melewati batas timeout sebagai dead.
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
