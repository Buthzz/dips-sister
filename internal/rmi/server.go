package rmi

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/rpc"
	"sync"
)

// Server mengelola server TCP untuk melayani panggilan Remote Method Invocation (RMI).
type Server struct {
	port      int
	server    *rpc.Server
	listener  net.Listener
	coordSvc  *CoordinatorService
	procSvc   *ImageProcessorService
	log       *slog.Logger
	mu        sync.Mutex
	isRunning bool
}

// NewServer membuat instance Server RMI baru dengan service yang terdaftar.
func NewServer(port int, coordSvc *CoordinatorService, procSvc *ImageProcessorService, log *slog.Logger) *Server {
	s := &Server{
		port:     port,
		server:   rpc.NewServer(),
		coordSvc: coordSvc,
		procSvc:  procSvc,
		log:      log,
	}

	if coordSvc != nil {
		if err := s.server.RegisterName("Coordinator", coordSvc); err != nil {
			log.Error("RMI: Gagal mendaftarkan service Coordinator", slog.String("error", err.Error()))
		}
	}

	if procSvc != nil {
		if err := s.server.RegisterName("ImageProcessor", procSvc); err != nil {
			log.Error("RMI: Gagal mendaftarkan service ImageProcessor", slog.String("error", err.Error()))
		}
	}

	return s
}

// Start menjalankan listener TCP server RMI sampai context dibatalkan.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return fmt.Errorf("rmi: server sudah berjalan")
	}

	addr := fmt.Sprintf(":%d", s.port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("rmi: listen %s gagal: %w", addr, err)
	}
	s.listener = lis
	s.isRunning = true
	s.mu.Unlock()

	s.log.Info("RMI server mendengarkan", slog.String("addr", addr))

	// Channel untuk menangani penutupan listener saat context selesai
	go func() {
		<-ctx.Done()
		s.Stop()
	}()

	for {
		conn, err := lis.Accept()
		if err != nil {
			s.mu.Lock()
			running := s.isRunning
			s.mu.Unlock()
			if !running {
				return nil
			}
			s.log.Warn("RMI: accept error", slog.String("error", err.Error()))
			continue
		}

		go s.server.ServeConn(conn)
	}
}

// Stop menghentikan listener server RMI.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isRunning {
		return
	}
	s.isRunning = false
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.log.Info("RMI server dihentikan")
}

// Close menghentikan listener server RMI (alias kompatibel io.Closer untuk Stop).
func (s *Server) Close() error {
	s.Stop()
	return nil
}

// Port mengembalikan nomor port tempat server mendengarkan.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		if tcpAddr, ok := s.listener.Addr().(*net.TCPAddr); ok {
			return tcpAddr.Port
		}
	}
	return s.port
}
