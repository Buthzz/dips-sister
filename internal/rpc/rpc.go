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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

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
	s.log.Info("RPC Register diterima",
		slog.String("node_id", req.GetNodeId()),
		slog.String("session", req.GetSessionId()),
		slog.String("addr", req.GetAdvertiseAddr()))

	ok := s.reg.Register(
		req.GetNodeId(),
		req.GetSessionId(),
		req.GetAdvertiseAddr(),
		int(req.GetCapacity()),
	)
	return &cluster.RegisterResponse{Accepted: ok, Message: "ok"}, nil
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

// WorkerServer mengimplementasikan gRPC service Worker pada node.
type WorkerServer struct {
	cluster.UnimplementedWorkerServer
	log *slog.Logger
}

// NewWorkerServer membuat instance WorkerServer baru.
func NewWorkerServer(log *slog.Logger) *WorkerServer {
	if log == nil {
		log = slog.Default()
	}
	return &WorkerServer{log: log}
}

// ProcessImage mengeksekusi transformasi citra yang ditugaskan oleh master.
func (s *WorkerServer) ProcessImage(_ context.Context, req *cluster.ProcessRequest) (*cluster.ProcessResponse, error) {
	mulai := time.Now()
	s.log.Info("mulai ProcessImage",
		slog.String("task_id", req.GetTaskId()),
		slog.String("filename", req.GetFilename()))

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
		grpc.MaxRecvMsgSize(8*1024*1024),
		grpc.MaxSendMsgSize(8*1024*1024),
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
		grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(UnaryTokenClientInterceptor(token)),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(8*1024*1024),
			grpc.MaxCallSendMsgSize(8*1024*1024),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("rpc: dial %s gagal: %w", addr, err)
	}
	return conn, nil
}
