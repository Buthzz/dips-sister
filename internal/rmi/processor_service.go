package rmi

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"distapi/internal/worker"
)

// ImageProcessorService adalah Remote Object RMI untuk mengeksekusi komputasi citra digital jarak jauh.
type ImageProcessorService struct {
	mu          sync.RWMutex
	nodeID      string
	activeTasks int64
	log         *slog.Logger
}

// NewImageProcessorService membuat instance ImageProcessorService baru.
func NewImageProcessorService(nodeID string, log *slog.Logger) *ImageProcessorService {
	return &ImageProcessorService{
		nodeID: nodeID,
		log:    log,
	}
}

// SetNodeID memperbarui identitas node saat ini.
func (s *ImageProcessorService) SetNodeID(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeID = nodeID
}

// TransformImage mengeksekusi transformasi citra (aspect resize dan/atau grayscale) melalui RMI.
func (s *ImageProcessorService) TransformImage(args ProcessImageArgs, reply *ProcessImageReply) error {
	atomic.AddInt64(&s.activeTasks, 1)
	defer atomic.AddInt64(&s.activeTasks, -1)

	mulai := time.Now()
	s.log.Info("RMI: TransformImage dimulai",
		slog.String("task_id", args.TaskID),
		slog.String("filename", args.Filename),
		slog.Int("bytes", len(args.ImageData)))

	opts := worker.Options{
		ResizeWidth:  args.ResizeWidth,
		ResizeHeight: args.ResizeHeight,
		Grayscale:    args.Grayscale,
	}

	hasil, err := worker.Process(args.ImageData, args.Filename, opts)
	durasi := time.Since(mulai).Milliseconds()

	if err != nil {
		s.log.Error("RMI: TransformImage gagal",
			slog.String("task_id", args.TaskID),
			slog.String("error", err.Error()))

		reply.TaskID = args.TaskID
		reply.Success = false
		reply.Error = err.Error()
		reply.DurationMs = durasi
		return nil
	}

	s.log.Info("RMI: TransformImage sukses",
		slog.String("task_id", args.TaskID),
		slog.Int64("duration_ms", durasi),
		slog.Int("result_bytes", len(hasil)))

	reply.TaskID = args.TaskID
	reply.Success = true
	reply.ResultData = hasil
	reply.Filename = args.Filename
	reply.DurationMs = durasi
	return nil
}

// Ping memeriksa status ketersediaan node dan jumlah task aktif.
func (s *ImageProcessorService) Ping(args PingArgs, reply *PingReply) error {
	s.mu.RLock()
	reply.NodeID = s.nodeID
	s.mu.RUnlock()

	reply.Role = "processor"
	reply.Status = "ready"
	reply.ActiveTasks = int(atomic.LoadInt64(&s.activeTasks))
	reply.Timestamp = time.Now().UnixMilli()
	return nil
}

// String representasi informasi service.
func (s *ImageProcessorService) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("ImageProcessorService(node=%s, active=%d)", s.nodeID, atomic.LoadInt64(&s.activeTasks))
}

