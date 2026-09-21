// Package rpc menyediakan server dan client gRPC untuk komunikasi antar node kluster.
//
// Pola komunikasi (DESIGN.md §5.3):
//   - Coordinator server berjalan di master; dipanggil node untuk Register dan Heartbeat
//   - Worker server berjalan di setiap node; dipanggil master untuk ProcessImage
//   - Kedua arah komunikasi diamankan dengan shared token via gRPC metadata
//
// Pemilihan gRPC (vs REST untuk internal):
//   - Binary encoding Protocol Buffers lebih efisien untuk payload gambar besar
//   - Strongly-typed interface mengurangi risiko kesalahan serialisasi
//   - Mendukung deadline propagation secara native via context
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

// tokenMetaKey adalah kunci metadata gRPC untuk shared cluster token.
// Menggunakan lowercase sesuai konvensi gRPC metadata (case-insensitive di HTTP/2).
const tokenMetaKey = "x-cluster-token"

// UnaryTokenClientInterceptor menyisipkan token ke setiap RPC keluar (outgoing).
// Dipasang sebagai dial option agar setiap panggilan otomatis terauthentikasi
// tanpa harus ingat menyisipkan token secara manual di setiap call site.
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

// UnaryTokenServerInterceptor memvalidasi token pada setiap RPC masuk (incoming).
// Jika token tidak ada atau salah, request ditolak dengan status Unauthenticated
// sebelum menyentuh logika handler — prinsip defense-in-depth.
func UnaryTokenServerInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "metadata tidak ada")
		}
		vals := md.Get(tokenMetaKey)
		if len(vals) == 0 || vals[0] != token {
			return nil, status.Error(codes.Unauthenticated, "token kluster tidak valid")
		}
		return handler(ctx, req)
	}
}

// CoordinatorServer mengimplementasikan service Coordinator (sisi master).
// Node memanggil Register saat pertama kali terhubung, lalu Heartbeat secara periodik.
type CoordinatorServer struct {
	cluster.UnimplementedCoordinatorServer
	reg *registry.Registry
	log *slog.Logger
}

// NewCoordinatorServer membuat server Coordinator yang didukung registry tertentu.
func NewCoordinatorServer(reg *registry.Registry, log *slog.Logger) *CoordinatorServer {
	return &CoordinatorServer{reg: reg, log: log}
}

// Register menerima pendaftaran dari node dan mencatatnya ke registry.
// Selalu mengembalikan accepted=true; penolakan tidak diimplementasikan saat ini.
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

// Heartbeat mencatat waktu hidup node terakhir dan jumlah task aktifnya.
// Jika heartbeat ditolak (sesi tidak cocok), node harus melakukan Register ulang.
func (s *CoordinatorServer) Heartbeat(_ context.Context, req *cluster.HeartbeatRequest) (*cluster.HeartbeatResponse, error) {
	ok := s.reg.Heartbeat(req.GetNodeId(), req.GetSessionId(), int(req.GetActiveTasks()))
	if !ok {
		s.log.Warn("heartbeat ditolak",
			slog.String("node_id", req.GetNodeId()),
			slog.String("alasan", "node tidak dikenal atau sesi kadaluarsa"))
	}
	return &cluster.HeartbeatResponse{Ok: ok}, nil
}

// WorkerServer mengimplementasikan service Worker (sisi node).
// Master memanggil ProcessImage untuk setiap task dalam job scatter-gather.
// Server ini juga berjalan di master untuk menangani "master-local" processing
// saat tidak ada node yang tersedia (graceful degradation).
type WorkerServer struct {
	cluster.UnimplementedWorkerServer
	log *slog.Logger
}

// NewWorkerServer membuat WorkerServer. Logger boleh nil jika dipakai secara lokal.
func NewWorkerServer(log *slog.Logger) *WorkerServer {
	if log == nil {
		log = slog.Default()
	}
	return &WorkerServer{log: log}
}

// ProcessImage menerima gambar mentah, memproses sesuai parameter, dan mengembalikan hasilnya.
// Error dari worker.Process dikembalikan sebagai response dengan success=false
// (bukan gRPC error) agar scheduler dapat membedakan "gagal proses" vs "gagal jaringan".
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

// NewGRPCServer membuat gRPC server dengan interceptor token dan batas ukuran pesan 8 MB.
// Batas 8 MB dipilih untuk mengakomodasi gambar hingga 5 MB (max upload) ditambah
// overhead enkoding dan metadata protobuf.
func NewGRPCServer(token string) *grpc.Server {
	return grpc.NewServer(
		grpc.UnaryInterceptor(UnaryTokenServerInterceptor(token)),
		grpc.MaxRecvMsgSize(8*1024*1024),
		grpc.MaxSendMsgSize(8*1024*1024),
	)
}

// ServeGRPC memulai server gRPC di port yang ditentukan dan menunggu ctx selesai.
// Ketika ctx dibatalkan (misal Ctrl+C), server di-stop secara graceful —
// request yang sedang berjalan dibiarkan selesai sebelum server benar-benar berhenti.
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
		// Context selesai (SIGINT/SIGTERM) — graceful stop agar task aktif bisa selesai.
		srv.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}

// DialNode membuka koneksi gRPC ke node dengan interceptor token dan batas pesan 8 MB.
// Koneksi ini di-cache oleh NodeClientAdapter untuk menghindari overhead dial berulang.
func DialNode(addr, token string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithInsecure(), //nolint:staticcheck // TLS ditambahkan pada iterasi berikutnya (DESIGN.md §13.4)
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
