package rmi

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// CoordinatorHandler mendefinisikan antarmuka delegasi penanganan pemilihan dan sinkronisasi kluster.
type CoordinatorHandler interface {
	HandleElection(args ElectionArgs) (ElectionReply, error)
	HandleVictory(args VictoryArgs) (VictoryReply, error)
	HandleHeartbeat(args HeartbeatArgs) (HeartbeatReply, error)
	GetClusterView() ClusterViewReply
}

// CoordinatorService adalah Remote Object RMI untuk koordinasi dan pemilihan koordinator kluster.
type CoordinatorService struct {
	mu      sync.RWMutex
	nodeID  string
	role    string
	handler CoordinatorHandler
	log     *slog.Logger
}

// NewCoordinatorService membuat instance remote object CoordinatorService baru.
func NewCoordinatorService(nodeID, role string, handler CoordinatorHandler, log *slog.Logger) *CoordinatorService {
	return &CoordinatorService{
		nodeID:  nodeID,
		role:    role,
		handler: handler,
		log:     log,
	}
}

// SetRole memperbarui peran node saat ini (misal: "master" atau "node").
func (s *CoordinatorService) SetRole(role string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.role = role
}

// SetNodeID memperbarui identitas unik node saat ini.
func (s *CoordinatorService) SetNodeID(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeID = nodeID
}

// SetHandler memperbarui handler delegasi pemilihan.
func (s *CoordinatorService) SetHandler(h CoordinatorHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

// Elect memproses pesan pemilihan koordinator (Bully Algorithm) dari node berprioritas lebih rendah.
func (s *CoordinatorService) Elect(args ElectionArgs, reply *ElectionReply) error {
	s.mu.RLock()
	h := s.handler
	s.mu.RUnlock()

	if h != nil {
		res, err := h.HandleElection(args)
		if err != nil {
			return err
		}
		*reply = res
		return nil
	}

	// Default: balasan standar jika belum ada handler kustom
	reply.OK = true
	reply.ResponderID = s.nodeID
	return nil
}

// AnnounceCoordinator menerima pengumuman deklarasi koordinator baru (Victory message).
func (s *CoordinatorService) AnnounceCoordinator(args VictoryArgs, reply *VictoryReply) error {
	s.mu.RLock()
	h := s.handler
	s.mu.RUnlock()

	s.log.Info("RMI: Pengumuman koordinator baru diterima",
		slog.String("coordinator_id", args.CoordinatorID),
		slog.String("grpc_addr", args.CoordinatorAddr),
		slog.Int("priority", args.Priority))

	if h != nil {
		res, err := h.HandleVictory(args)
		if err != nil {
			return err
		}
		*reply = res
		return nil
	}

	reply.Acknowledged = true
	reply.NodeID = s.nodeID
	return nil
}

// Heartbeat memproses pertukaran sinyal detak jantung dan sinkronisasi topologi peer via RMI.
func (s *CoordinatorService) Heartbeat(args HeartbeatArgs, reply *HeartbeatReply) error {
	s.mu.RLock()
	h := s.handler
	s.mu.RUnlock()

	if h != nil {
		res, err := h.HandleHeartbeat(args)
		if err != nil {
			return err
		}
		*reply = res
		return nil
	}

	reply.OK = true
	reply.CoordinatorID = s.nodeID
	return nil
}

// GetClusterView mengembalikan status topologi seluruh node di kluster.
func (s *CoordinatorService) GetClusterView(_ EmptyArgs, reply *ClusterViewReply) error {
	s.mu.RLock()
	h := s.handler
	s.mu.RUnlock()

	if h != nil {
		*reply = h.GetClusterView()
		return nil
	}

	reply.CoordinatorID = s.nodeID
	return nil
}

// Ping mengembalikan status konektivitas dan kesehatan remote object.
func (s *CoordinatorService) Ping(args PingArgs, reply *PingReply) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.log.Debug("RMI: Ping diterima", slog.String("dari", args.SenderID))
	reply.NodeID = s.nodeID
	reply.Role = s.role
	reply.Status = "online"
	reply.Timestamp = time.Now().UnixMilli()
	return nil
}

// String representasi informasi service.
func (s *CoordinatorService) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("CoordinatorService(node=%s, role=%s)", s.nodeID, s.role)
}
