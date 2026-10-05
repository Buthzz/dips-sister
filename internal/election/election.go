// Package election mengimplementasikan algoritma pemilihan koordinator
// berbasis Bully Algorithm (Garcia-Molina, 1982) serta failover dinamis di dalam kluster.
package election

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"distapi/internal/rmi"
)

// ParsePriority menghitung bobot prioritas pemilihan sebuah node.
func ParsePriority(nodeID string, fallback int) int {
	if fallback > 0 {
		return fallback
	}
	clean := strings.ToLower(strings.TrimSpace(nodeID))
	if clean == "master" || strings.HasPrefix(clean, "master-") {
		return 100
	}
	for _, prefix := range []string{"node-", "node", "laptop-", "laptop", "worker-", "worker"} {
		if strings.HasPrefix(clean, prefix) {
			numStr := strings.TrimPrefix(clean, prefix)
			if n, err := strconv.Atoi(numStr); err == nil && n > 0 {
				return n
			}
		}
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(clean))
	return int(h.Sum32()%90) + 1
}

// Config memuat parameter operasional pemilihan koordinator.
type Config struct {
	NodeID             string
	GRPCAddr           string
	RMIAddr            string
	HTTPAddr           string
	Priority           int
	IsInitialLeader    bool
	AutoFailover       bool
	HeartbeatInterval  time.Duration
	CoordinatorTimeout time.Duration
	InitialCoordinator rmi.Peer
}

// Engine mengelola topologi kluster, pemilihan koordinator (Bully), dan failover.
type Engine struct {
	mu           sync.RWMutex
	self         rmi.Peer
	coordinator  rmi.Peer
	peers        map[string]rmi.Peer // Kunci: NodeID
	isLeader     bool
	isElecting   bool
	autoFailover bool
	hbInterval   time.Duration
	coordTimeout time.Duration

	consecutiveFailures int

	onPromoted           func(coordinator rmi.Peer)
	onCoordinatorChanged func(coordinator rmi.Peer)
	onLog                func(level slog.Level, msg string)

	log *slog.Logger
}

// NewEngine membuat instance Engine pemilihan baru.
func NewEngine(cfg Config, log *slog.Logger) *Engine {
	prio := ParsePriority(cfg.NodeID, cfg.Priority)
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 2 * time.Second
	}
	if cfg.CoordinatorTimeout <= 0 {
		cfg.CoordinatorTimeout = 6 * time.Second
	}

	self := rmi.Peer{
		NodeID:   cfg.NodeID,
		GRPCAddr: cfg.GRPCAddr,
		RMIAddr:  cfg.RMIAddr,
		HTTPAddr: cfg.HTTPAddr,
		Priority: prio,
		IsLeader: cfg.IsInitialLeader,
		LastSeen: time.Now().Unix(),
	}

	coord := cfg.InitialCoordinator
	if cfg.IsInitialLeader {
		coord = self
	}

	return &Engine{
		self:         self,
		coordinator:  coord,
		peers:        make(map[string]rmi.Peer),
		isLeader:     cfg.IsInitialLeader,
		autoFailover: cfg.AutoFailover,
		hbInterval:   cfg.HeartbeatInterval,
		coordTimeout: cfg.CoordinatorTimeout,
		log:          log,
	}
}

// SetCallbacks menyetel handler untuk peristiwa pergantian koordinator.
func (e *Engine) SetCallbacks(
	onPromoted func(rmi.Peer),
	onChanged func(rmi.Peer),
	onLog func(slog.Level, string),
) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onPromoted = onPromoted
	e.onCoordinatorChanged = onChanged
	e.onLog = onLog
}

// UpdateSelf memperbarui alamat atau identitas lokal node.
func (e *Engine) UpdateSelf(nodeID, grpcAddr, rmiAddr, httpAddr string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.self.NodeID = nodeID
	e.self.GRPCAddr = grpcAddr
	e.self.RMIAddr = rmiAddr
	e.self.HTTPAddr = httpAddr
	e.self.Priority = ParsePriority(nodeID, e.self.Priority)
}

// RegisterPeer mendaftarkan atau memperbarui informasi satu peer di tabel topologi.
func (e *Engine) RegisterPeer(peer rmi.Peer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if peer.NodeID == "" || peer.NodeID == e.self.NodeID {
		return
	}
	peer.LastSeen = time.Now().Unix()
	e.peers[peer.NodeID] = peer
}

// SyncPeers memperbarui daftar peer secara massal dari balasan koordinator.
func (e *Engine) SyncPeers(peers []rmi.Peer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now().Unix()
	for _, p := range peers {
		if p.NodeID == "" || p.NodeID == e.self.NodeID {
			continue
		}
		p.LastSeen = now
		e.peers[p.NodeID] = p
	}
}

// GetPeers mengembalikan salinan seluruh peer yang diketahui.
func (e *Engine) GetPeers() []rmi.Peer {
	e.mu.RLock()
	defer e.mu.RUnlock()
	list := make([]rmi.Peer, 0, len(e.peers))
	for _, p := range e.peers {
		list = append(list, p)
	}
	return list
}

// Coordinator mengembalikan informasi koordinator saat ini.
func (e *Engine) Coordinator() rmi.Peer {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.coordinator
}

// IsLeader mengembalikan true jika node ini saat ini bertindak sebagai koordinator.
func (e *Engine) IsLeader() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isLeader
}

// HandleElection mengimplementasikan rmi.CoordinatorHandler untuk pesan pemilihan.
func (e *Engine) HandleElection(args rmi.ElectionArgs) (rmi.ElectionReply, error) {
	e.mu.Lock()
	selfPrio := e.self.Priority
	selfID := e.self.NodeID

	// Catat candidate ke tabel peer
	if args.CandidateID != "" && args.CandidateRMI != "" {
		e.peers[args.CandidateID] = rmi.Peer{
			NodeID:   args.CandidateID,
			GRPCAddr: args.CandidateAddr,
			RMIAddr:  args.CandidateRMI,
			Priority: args.Priority,
			LastSeen: time.Now().Unix(),
		}
	}

	isHigher := selfPrio > args.Priority
	e.mu.Unlock()

	e.emitLog(slog.LevelInfo, fmt.Sprintf("Bully: Menerima pesan ELECTION dari %s (prioritas %d, kita %d)",
		args.CandidateID, args.Priority, selfPrio))

	if isHigher {
		// Kita lebih tinggi: balas OK, lalu ambil alih pemilihan
		go e.StartElection()
		return rmi.ElectionReply{
			OK:          true,
			ResponderID: selfID,
			Priority:    selfPrio,
		}, nil
	}

	return rmi.ElectionReply{
		OK:          false,
		ResponderID: selfID,
		Priority:    selfPrio,
	}, nil
}

// HandleVictory mengimplementasikan rmi.CoordinatorHandler untuk pengumuman kemenangan koordinator baru.
func (e *Engine) HandleVictory(args rmi.VictoryArgs) (rmi.VictoryReply, error) {
	e.mu.Lock()
	newCoord := rmi.Peer{
		NodeID:   args.CoordinatorID,
		GRPCAddr: args.CoordinatorAddr,
		HTTPAddr: args.CoordinatorHTTP,
		RMIAddr:  args.CoordinatorRMI,
		Priority: args.Priority,
		IsLeader: true,
		LastSeen: time.Now().Unix(),
	}

	e.coordinator = newCoord
	e.isLeader = (args.CoordinatorID == e.self.NodeID)
	e.isElecting = false
	e.consecutiveFailures = 0

	if args.CoordinatorID != e.self.NodeID {
		e.peers[args.CoordinatorID] = newCoord
	}

	cb := e.onCoordinatorChanged
	e.mu.Unlock()

	e.emitLog(slog.LevelWarn, fmt.Sprintf("Bully: Koordinator baru diakui: %s di %s (RMI: %s)",
		args.CoordinatorID, args.CoordinatorAddr, args.CoordinatorRMI))

	if cb != nil && args.CoordinatorID != e.self.NodeID {
		cb(newCoord)
	}

	return rmi.VictoryReply{
		Acknowledged: true,
		NodeID:       e.self.NodeID,
	}, nil
}

// HandleHeartbeat mengimplementasikan rmi.CoordinatorHandler untuk pertukaran sinyal detak jantung.
func (e *Engine) HandleHeartbeat(args rmi.HeartbeatArgs) (rmi.HeartbeatReply, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if args.NodeID != "" && args.RMIAddr != "" {
		e.peers[args.NodeID] = rmi.Peer{
			NodeID:   args.NodeID,
			RMIAddr:  args.RMIAddr,
			GRPCAddr: args.GRPCAddr,
			Priority: args.Priority,
			LastSeen: time.Now().Unix(),
		}
	}

	peerList := make([]rmi.Peer, 0, len(e.peers)+1)
	peerList = append(peerList, e.self)
	for _, p := range e.peers {
		peerList = append(peerList, p)
	}

	return rmi.HeartbeatReply{
		OK:            true,
		CoordinatorID: e.coordinator.NodeID,
		Peers:         peerList,
	}, nil
}

// GetClusterView mengembalikan topologi kluster lengkap.
func (e *Engine) GetClusterView() rmi.ClusterViewReply {
	e.mu.RLock()
	defer e.mu.RUnlock()

	peerList := make([]rmi.Peer, 0, len(e.peers)+1)
	peerList = append(peerList, e.self)
	for _, p := range e.peers {
		peerList = append(peerList, p)
	}

	return rmi.ClusterViewReply{
		CoordinatorID: e.coordinator.NodeID,
		Peers:         peerList,
	}
}

// StartElection menjalankan Algoritma Bully untuk memilih koordinator baru.
func (e *Engine) StartElection() {
	e.mu.Lock()
	if e.isElecting {
		e.mu.Unlock()
		return
	}
	e.isElecting = true
	self := e.self
	candidates := make([]rmi.Peer, 0)
	for _, p := range e.peers {
		if p.Priority > self.Priority && p.RMIAddr != "" {
			candidates = append(candidates, p)
		}
	}
	e.mu.Unlock()

	e.emitLog(slog.LevelWarn, fmt.Sprintf("Bully: Memulai pemilihan koordinator (ID: %s, Prioritas: %d)...",
		self.NodeID, self.Priority))

	// Jika tidak ada node berprioritas lebih tinggi, node ini otomatis menang
	if len(candidates) == 0 {
		e.declareVictory()
		return
	}

	// Kirim pesan ELECTION ke seluruh node yang lebih tinggi secara paralel
	higherAnswered := false
	var wg sync.WaitGroup
	var muAnswer sync.Mutex

	for _, cand := range candidates {
		wg.Add(1)
		go func(target rmi.Peer) {
			defer wg.Done()
			args := rmi.ElectionArgs{
				CandidateID:   self.NodeID,
				CandidateAddr: self.GRPCAddr,
				CandidateRMI:  self.RMIAddr,
				Priority:      self.Priority,
			}
			reply, err := rmi.SendElection(target.RMIAddr, args, 2*time.Second)
			if err == nil && reply.OK {
				muAnswer.Lock()
				higherAnswered = true
				muAnswer.Unlock()
				e.emitLog(slog.LevelInfo, fmt.Sprintf("Bully: Node lebih tinggi %s menjawab OK", target.NodeID))
			}
		}(cand)
	}

	wg.Wait()

	if higherAnswered {
		e.emitLog(slog.LevelInfo, "Bully: Menunggu deklarasi VICTORY dari node berprioritas lebih tinggi...")
		// Tunggu pengumuman koordinator baru
		time.Sleep(4 * time.Second)
		e.mu.RLock()
		isStillElecting := e.isElecting
		e.mu.RUnlock()
		if isStillElecting {
			// Node lebih tinggi tampaknya gagal mendeklarasikan diri, ulangi pemilihan
			e.mu.Lock()
			e.isElecting = false
			e.mu.Unlock()
			e.emitLog(slog.LevelWarn, "Bully: Batas waktu menunggu VICTORY habis, mengulang pemilihan...")
			go e.StartElection()
		}
		return
	}

	// Tidak ada node lebih tinggi yang menjawab: KITA MENANG!
	e.declareVictory()
}

// declareVictory mendeklarasikan kemenangan pemilihan koordinator.
func (e *Engine) declareVictory() {
	e.mu.Lock()
	e.isLeader = true
	e.isElecting = false
	e.self.IsLeader = true
	e.coordinator = e.self
	self := e.self
	peersList := make([]rmi.Peer, 0, len(e.peers))
	for _, p := range e.peers {
		peersList = append(peersList, p)
	}
	promotedCB := e.onPromoted
	e.mu.Unlock()

	e.emitLog(slog.LevelWarn, fmt.Sprintf("Bully: KITA MENANG PEMILIHAN! Mengambil alih sebagai Koordinator (%s)", self.NodeID))

	// Siarkan pengumuman kemenangan (VICTORY) ke seluruh peer
	var wg sync.WaitGroup
	for _, p := range peersList {
		if p.RMIAddr == "" {
			continue
		}
		wg.Add(1)
		go func(target rmi.Peer) {
			defer wg.Done()
			args := rmi.VictoryArgs{
				CoordinatorID:   self.NodeID,
				CoordinatorAddr: self.GRPCAddr,
				CoordinatorHTTP: self.HTTPAddr,
				CoordinatorRMI:  self.RMIAddr,
				Priority:        self.Priority,
			}
			_, _ = rmi.SendVictory(target.RMIAddr, args, 2*time.Second)
		}(p)
	}
	wg.Wait()

	if promotedCB != nil {
		promotedCB(self)
	}
}

// StartMonitor menjalankan loop pemantauan koordinator (Failure Detector) di background.
func (e *Engine) StartMonitor(ctx context.Context) {
	ticker := time.NewTicker(e.hbInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.mu.RLock()
			isLeader := e.isLeader
			auto := e.autoFailover
			coordRMI := e.coordinator.RMIAddr
			coordID := e.coordinator.NodeID
			selfID := e.self.NodeID
			selfRMI := e.self.RMIAddr
			selfGRPC := e.self.GRPCAddr
			selfPrio := e.self.Priority
			isElecting := e.isElecting
			e.mu.RUnlock()

			// Jika node adalah leader atau failover tidak diaktifkan, tidak perlu memonitor
			if isLeader || !auto || isElecting || coordRMI == "" {
				continue
			}

			// Kirim detak jantung via RMI ke koordinator
			args := rmi.HeartbeatArgs{
				NodeID:   selfID,
				RMIAddr:  selfRMI,
				GRPCAddr: selfGRPC,
				Priority: selfPrio,
			}

			reply, err := rmi.SendHeartbeat(coordRMI, args, 2*time.Second)
			if err != nil {
				e.mu.Lock()
				e.consecutiveFailures++
				fails := e.consecutiveFailures
				e.mu.Unlock()

				e.emitLog(slog.LevelWarn, fmt.Sprintf("Detektor Kegagalan: Gagal kontak koordinator %s di %s (%d kali berturut-turut)",
					coordID, coordRMI, fails))

				// Jika melebihi batas toleransi (3 kali atau timeout terlampaui)
				if fails >= 3 {
					e.emitLog(slog.LevelError, fmt.Sprintf("Detektor Kegagalan: Koordinator %s dinyatakan MATI! Memicu pemilihan koordinator baru...",
						coordID))
					go e.StartElection()
				}
			} else {
				e.mu.Lock()
				e.consecutiveFailures = 0
				e.mu.Unlock()

				// Sinkronkan topologi peer dari balasan koordinator
				if len(reply.Peers) > 0 {
					e.SyncPeers(reply.Peers)
				}
			}
		}
	}
}

func (e *Engine) emitLog(level slog.Level, msg string) {
	if e.log != nil {
		e.log.Log(context.Background(), level, msg)
	}
	e.mu.RLock()
	cb := e.onLog
	e.mu.RUnlock()
	if cb != nil {
		cb(level, msg)
	}
}
