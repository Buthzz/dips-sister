// Package registry mengelola penamaan node (home-based naming) dan deteksi kesehatan cluster.
package registry

import (
	"log/slog"
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

// Registry menyimpan dan memetakan identitas node ke alamat jaringan.
type Registry struct {
	mu      sync.RWMutex
	nodes   map[string]*NodeInfo
	timeout time.Duration
	log     *slog.Logger
}

// New membuat instance Registry dengan batas timeout heartbeat tertentu.
func New(timeout time.Duration, log *slog.Logger) *Registry {
	return &Registry{
		nodes:   make(map[string]*NodeInfo),
		timeout: timeout,
		log:     log,
	}
}

// Register mendaftarkan node baru atau memperbarui entri node yang sudah ada.
func (r *Registry) Register(nodeID, sessionID, advertiseAddr string, capacity int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	if existing, ok := r.nodes[nodeID]; ok && existing.SessionID == sessionID {
		existing.AdvertiseAddr = advertiseAddr
		existing.Capacity = capacity
		existing.Status = StatusAlive
		existing.LastHeartbeat = now
		r.log.Info("node refresh registrasi",
			slog.String("node_id", nodeID),
			slog.String("addr", advertiseAddr))
		return true
	}

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
