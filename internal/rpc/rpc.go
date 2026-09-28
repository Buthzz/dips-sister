// Package rpc mengelola komunikasi gRPC antar-node dan master.
package rpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"distapi/gen/cluster"
	"distapi/internal/registry"
	"distapi/internal/worker"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// maxGRPCMsgSize adalah batas ukuran pesan gRPC (8 MB) yang diberlakukan
// pada sisi server maupun client. Nilainya dipilih agar gambar berukuran
// hingga ~5 MB (setelah overhead encoding) tetap dapat diproses dengan aman.
const maxGRPCMsgSize = 8 * 1024 * 1024

const tokenMetaKey = "x-cluster-token"

// UnaryTokenClientInterceptor menyisipkan shared token ke metadata RPC keluar.
func UnaryTokenClientInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx, tokenMetaKey, token)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// UnaryTokenServerInterceptor memvalidasi shared token dari metadata RPC masuk.
func UnaryTokenServerInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "metadata tidak ditemukan")
		}
		vals := md.Get(tokenMetaKey)
		if len(vals) == 0 || vals[0] != token {
			return nil, status.Error(codes.Unauthenticated, "token tidak valid")
		}
		return handler(ctx, req)
	}
}

// CoordinatorServer mengimplementasikan gRPC service Coordinator pada master.
type CoordinatorServer struct {
	cluster.UnimplementedCoordinatorServer
	reg *registry.Registry
	log *slog.Logger
}

// NewCoordinatorServer membuat instance CoordinatorServer baru.
func NewCoordinatorServer(reg *registry.Registry, log *slog.Logger) *CoordinatorServer {
	return &CoordinatorServer{reg: reg, log: log}
}

// Register menerima permintaan pendaftaran dari worker node.
func (s *CoordinatorServer) Register(_ context.Context, req *cluster.RegisterRequest) (*cluster.RegisterResponse, error) {
	addr := req.GetAdvertiseAddr()

	s.log.Info("RPC Register diterima",
		slog.String("node_id", req.GetNodeId()),
		slog.String("session", req.GetSessionId()),
		slog.String("addr", addr))

	// B1: validasi format alamat di sisi server — tolak jika tidak host:port
	if _, _, err := net.SplitHostPort(addr); err != nil {
		s.log.Warn("register ditolak: format advertise tidak valid",
			slog.String("addr", addr),
			slog.String("node_id", req.GetNodeId()))
		return &cluster.RegisterResponse{
			Accepted: false,
			Message:  fmt.Sprintf("format alamat tidak valid (harus host:port): %v", err),
		}, nil
	}

	ok, assignedID, regMsg := s.reg.RegisterNode(
		req.GetNodeId(),
		req.GetSessionId(),
		addr,
		int(req.GetCapacity()),
	)
	if !ok {
		return &cluster.RegisterResponse{Accepted: false, Message: regMsg}, nil
	}

	// B2: probe balik TCP 1 detik — registrasi tetap diterima, hanya beri peringatan
	msg := "ok"
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		s.log.Warn("probe balik ke node gagal — kemungkinan terhalang firewall",
			slog.String("node_id", assignedID),
			slog.String("addr", addr),
			slog.String("error", err.Error()))
		msg = "PERINGATAN: node terdaftar, tapi master tidak bisa menjangkau " + addr +
			" — periksa Windows Firewall di laptop node (izinkan port " + portOf(addr) + " TCP masuk)"
	} else {
		conn.Close()
	}

	return &cluster.RegisterResponse{
		Accepted: true,
		Message:  fmt.Sprintf("ASSIGNED:%s|%s", assignedID, msg),
	}, nil
}


// Heartbeat menerima sinyal detak jantung dari worker node.
func (s *CoordinatorServer) Heartbeat(_ context.Context, req *cluster.HeartbeatRequest) (*cluster.HeartbeatResponse, error) {
	ok := s.reg.Heartbeat(req.GetNodeId(), req.GetSessionId(), int(req.GetActiveTasks()))
	if !ok {
		s.log.Warn("heartbeat ditolak",
			slog.String("node_id", req.GetNodeId()),
			slog.String("alasan", "node tidak terdaftar atau sesi kedaluwarsa"))
	}
	return &cluster.HeartbeatResponse{Ok: ok}, nil
}

// Deregister menerima permintaan pelepasan diri dari worker node (graceful shutdown).
func (s *CoordinatorServer) Deregister(_ context.Context, req *cluster.DeregisterRequest) (*cluster.DeregisterResponse, error) {
	s.log.Info("RPC Deregister diterima",
		slog.String("node_id", req.GetNodeId()),
		slog.String("session", req.GetSessionId()),
		slog.String("reason", req.GetReason()))

	ok := s.reg.Deregister(req.GetNodeId(), req.GetSessionId())
	if !ok {
		return &cluster.DeregisterResponse{
			Accepted: false,
			Message:  "node tidak terdaftar atau sesi tidak cocok",
		}, nil
	}
	return &cluster.DeregisterResponse{
		Accepted: true,
		Message:  "node berhasil di-deregister",
	}, nil
}

// TaskObserver menerima notifikasi event pemrosesan task pada worker.
type TaskObserver interface {
	OnTaskStart(taskID, filename string)
	OnTaskComplete(taskID, filename string, success bool, durationMs int64)
}

// WorkerServer mengimplementasikan gRPC service Worker pada node.
type WorkerServer struct {
	cluster.UnimplementedWorkerServer
	log      *slog.Logger
	observer TaskObserver
}

// NewWorkerServer membuat instance WorkerServer baru.
func NewWorkerServer(log *slog.Logger) *WorkerServer {
	if log == nil {
		log = slog.Default()
	}
	return &WorkerServer{log: log}
}

// SetObserver mendaftarkan observer untuk memantau siklus hidup task.
func (s *WorkerServer) SetObserver(obs TaskObserver) {
	s.observer = obs
}

// ProcessImage mengeksekusi transformasi citra yang ditugaskan oleh master.
func (s *WorkerServer) ProcessImage(_ context.Context, req *cluster.ProcessRequest) (*cluster.ProcessResponse, error) {
	mulai := time.Now()
	s.log.Info("mulai ProcessImage",
		slog.String("task_id", req.GetTaskId()),
		slog.String("filename", req.GetFilename()))

	if s.observer != nil {
		s.observer.OnTaskStart(req.GetTaskId(), req.GetFilename())
	}

	opts := worker.Options{
		ResizeWidth:  int(req.GetResizeWidth()),
		ResizeHeight: int(req.GetResizeHeight()),
		Grayscale:    req.GetGrayscale(),
	}

	hasil, err := worker.Process(req.GetImageData(), req.GetFilename(), opts)
	durasi := time.Since(mulai).Milliseconds()

	if err != nil {
		s.log.Error("ProcessImage gagal",
			slog.String("task_id", req.GetTaskId()),
			slog.String("error", err.Error()))
		if s.observer != nil {
			s.observer.OnTaskComplete(req.GetTaskId(), req.GetFilename(), false, durasi)
		}
		return &cluster.ProcessResponse{
			TaskId:     req.GetTaskId(),
			Success:    false,
			Error:      err.Error(),
			DurationMs: durasi,
		}, nil
	}

	s.log.Info("ProcessImage selesai",
		slog.String("task_id", req.GetTaskId()),
		slog.Int64("duration_ms", durasi))

	if s.observer != nil {
		s.observer.OnTaskComplete(req.GetTaskId(), req.GetFilename(), true, durasi)
	}

	return &cluster.ProcessResponse{
		TaskId:     req.GetTaskId(),
		Success:    true,
		ResultData: hasil,
		Filename:   req.GetFilename(),
		DurationMs: durasi,
	}, nil
}

// NewGRPCServer membuat gRPC server terkonfigurasi dengan interceptor token.
func NewGRPCServer(token string) *grpc.Server {
	return grpc.NewServer(
		grpc.UnaryInterceptor(UnaryTokenServerInterceptor(token)),
		grpc.MaxRecvMsgSize(maxGRPCMsgSize),
		grpc.MaxSendMsgSize(maxGRPCMsgSize),
	)
}

// ServeGRPC menjalankan server gRPC hingga context selesai.
func ServeGRPC(ctx context.Context, srv *grpc.Server, port int, log *slog.Logger) error {
	addr := fmt.Sprintf(":%d", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("rpc: listen %s gagal: %w", addr, err)
	}
	log.Info("gRPC server mendengarkan", slog.String("addr", addr))

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(lis); err != nil {
			errCh <- fmt.Errorf("rpc: server error: %w", err)
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		srv.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}

// DialNode membuat koneksi gRPC klien ke alamat target dengan interceptor token.
func DialNode(addr, token string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(UnaryTokenClientInterceptor(token)),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxGRPCMsgSize),
			grpc.MaxCallSendMsgSize(maxGRPCMsgSize),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("rpc: dial %s gagal: %w", addr, err)
	}
	return conn, nil
}

// portOf mengekstrak bagian port dari alamat host:port.
// Jika gagal, mengembalikan addr apa adanya.
func portOf(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return addr
}
