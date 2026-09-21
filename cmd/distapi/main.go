// Package main adalah titik masuk tunggal binary distapi.
//
// Binary yang sama berjalan sebagai master atau node tergantung flag --mode.
// Desain single-binary memudahkan distribusi: cukup salin satu file .exe ke semua laptop.
//
// Contoh penggunaan:
//
//	# Laptop 1 — master (jalankan ini dulu)
//	distapi.exe --mode=master --http-port=8080 --grpc-port=9000 --token=demo123
//
//	# Laptop 2–4 — node (ganti IP sesuai jaringan)
//	distapi.exe --mode=node --node-id=node-1 \
//	  --master=192.168.1.10:9000 --advertise=192.168.1.11:9000 --token=demo123
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"distapi/gen/cluster"
	"distapi/internal/api"
	"distapi/internal/config"
	"distapi/internal/registry"
	rpcpkg "distapi/internal/rpc"
	"distapi/internal/scheduler"
	"distapi/internal/storage"
)

func main() {
	cfg := config.Parse()

	// Siapkan logger terstruktur (slog) — mudah difilter dan di-parse oleh log aggregator.
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	log.Info("distapi mulai",
		slog.String("mode", string(cfg.Mode)),
		slog.String("versi", "0.1.0"))

	// NotifyContext menangkap SIGINT (Ctrl+C) dan SIGTERM (docker stop / systemctl stop).
	// Semua goroutine menggunakan ctx ini untuk shutdown yang terkoordinasi.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cfg.Mode {
	case config.ModeMaster:
		jalankanMaster(ctx, cfg, log)
	case config.ModeNode:
		jalankanNode(ctx, cfg, log)
	}
}

// jalankanMaster menginisialisasi dan menjalankan semua komponen mode master:
//   - Storage untuk file upload dan hasil
//   - Registry untuk home-based naming node
//   - gRPC server (Coordinator + Worker lokal)
//   - Dead-node monitor (goroutine periodik)
//   - Job TTL cleaner (goroutine periodik)
//   - REST API (HTTP server)
func jalankanMaster(ctx context.Context, cfg config.Config, log *slog.Logger) {
	// Inisialisasi penyimpanan; hapus sisa dari sesi sebelumnya (state tidak dipulihkan).
	store, err := storage.New(cfg.DataDir)
	if err != nil {
		log.Error("inisialisasi storage gagal", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := store.Init(); err != nil {
		log.Error("storage.Init gagal", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Registry: home-based naming (nodeID → IP:port).
	reg := registry.New(cfg.NodeTimeout, log)

	// NodeClientAdapter: jembatan antara scheduler dan gRPC nyata.
	nodeClient := rpcpkg.NewNodeClientAdapter(cfg.Token)
	defer nodeClient.Close()

	// Scheduler: scatter-gather dengan round-robin dispatch dan retry.
	sched := scheduler.New(reg, store, nodeClient, cfg.MaxRetries, cfg.TaskTimeout, log)

	// gRPC server master: Coordinator (menerima Register/Heartbeat dari node)
	// dan Worker (untuk master-local processing saat tidak ada node aktif).
	grpcSrv := rpcpkg.NewGRPCServer(cfg.Token)
	coordSrv := rpcpkg.NewCoordinatorServer(reg, log)

	// hookPendaftaran mengintersep Register untuk membuka koneksi balik ke node.
	// Ini perlu karena master harus bisa memanggil ProcessImage ke node yang baru mendaftar.
	hook := &hookPendaftaran{
		CoordinatorServer: coordSrv,
		nodeClient:        nodeClient,
		token:             cfg.Token,
		log:               log,
	}
	cluster.RegisterCoordinatorServer(grpcSrv, hook)
	cluster.RegisterWorkerServer(grpcSrv, rpcpkg.NewWorkerServer(log))

	// Monitor node mati: jalankan TickDeadCheck setiap detik.
	// Node yang mati memicu reschedule task di scheduler.
	go monitorNodeMati(ctx, reg, sched, log)

	// Pembersih job TTL: hapus job lama setiap 5 menit.
	go pembersihJobTTL(ctx, sched, store, cfg.JobTTL, log)

	// Mulai gRPC server di goroutine terpisah.
	go func() {
		if err := rpcpkg.ServeGRPC(ctx, grpcSrv, cfg.GRPCPort, log); err != nil {
			log.Error("gRPC server error", slog.String("error", err.Error()))
		}
	}()

	// Mulai HTTP server; timeout header 5 detik untuk mencegah slowloris attack.
	alamatHTTP := fmt.Sprintf(":%d", cfg.HTTPPort)
	srvHTTP := &http.Server{
		Addr:              alamatHTTP,
		Handler:           api.New(sched, reg, store, cfg.MaxImageMB, cfg.MaxImages, log),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Info("HTTP server mendengarkan", slog.String("addr", alamatHTTP))
	go func() {
		if err := srvHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP server error", slog.String("error", err.Error()))
		}
	}()

	<-ctx.Done()
	log.Info("master: mulai shutdown...")

	// Beri waktu 10 detik untuk request yang sedang berjalan selesai.
	shutCtx, batal := context.WithTimeout(context.Background(), 10*time.Second)
	defer batal()
	_ = srvHTTP.Shutdown(shutCtx)
	log.Info("master: shutdown selesai")
}

// jalankanNode menginisialisasi mode node:
//   - Mulai gRPC Worker server untuk menerima task dari master
//   - Connect ke master dan lakukan Register
//   - Jalankan heartbeat loop secara periodik
func jalankanNode(ctx context.Context, cfg config.Config, log *slog.Logger) {
	// sessionID acak membedakan instance node yang restart dari yang baru.
	sessionID := buatSessionID()
	log.Info("node mulai",
		slog.String("node_id", cfg.NodeID),
		slog.String("session", sessionID),
		slog.String("master", cfg.MasterAddr),
		slog.String("advertise", cfg.AdvertiseAddr))

	// Mulai Worker server agar master bisa langsung mengirim task setelah Register.
	grpcSrv := rpcpkg.NewGRPCServer(cfg.Token)
	cluster.RegisterWorkerServer(grpcSrv, rpcpkg.NewWorkerServer(log))
	go func() {
		if err := rpcpkg.ServeGRPC(ctx, grpcSrv, cfg.GRPCPort, log); err != nil {
			log.Error("node gRPC error", slog.String("error", err.Error()))
		}
	}()

	// Buka koneksi ke master. Jika gagal, biner berhenti (fail-fast).
	conn, err := rpcpkg.DialNode(cfg.MasterAddr, cfg.Token)
	if err != nil {
		log.Error("tidak bisa connect ke master", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer conn.Close()

	coordClient := cluster.NewCoordinatorClient(conn)

	// Coba Register ke master dengan retry exponential backoff (maks 5 kali).
	if err := registerDenganRetry(ctx, coordClient, cfg, sessionID, log); err != nil {
		log.Error("Register ke master gagal total", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Heartbeat loop berjalan seumur ctx.
	go loopHeartbeat(ctx, coordClient, cfg, sessionID, log)

	<-ctx.Done()
	log.Info("node: menghentikan server...")
	grpcSrv.GracefulStop()
	log.Info("node: shutdown selesai")
}

// registerDenganRetry mencoba mendaftarkan node ke master hingga 5 kali
// dengan jeda yang meningkat (backoff) agar tidak membanjiri master saat startup.
func registerDenganRetry(
	ctx context.Context,
	client cluster.CoordinatorClient,
	cfg config.Config,
	sessionID string,
	log *slog.Logger,
) error {
	for percobaan := 1; percobaan <= 5; percobaan++ {
		rCtx, batal := context.WithTimeout(ctx, 5*time.Second)
		resp, err := client.Register(rCtx, &cluster.RegisterRequest{
			NodeId:        cfg.NodeID,
			SessionId:     sessionID,
			AdvertiseAddr: cfg.AdvertiseAddr,
			Capacity:      int32(cfg.Workers),
		})
		batal()

		if err == nil && resp.GetAccepted() {
			log.Info("terdaftar ke master",
				slog.String("node_id", cfg.NodeID),
				slog.String("session", sessionID))
			return nil
		}

		log.Warn("Register gagal, mencoba ulang",
			slog.Int("percobaan", percobaan),
			slog.String("error", fmt.Sprintf("%v", err)))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(percobaan) * time.Second):
		}
	}
	return fmt.Errorf("semua 5 percobaan Register gagal")
}

// loopHeartbeat mengirim heartbeat secara periodik ke master.
// Jika heartbeat ditolak (master tidak mengenali sesi), node mendaftar ulang.
func loopHeartbeat(
	ctx context.Context,
	client cluster.CoordinatorClient,
	cfg config.Config,
	sessionID string,
	log *slog.Logger,
) {
	ticker := time.NewTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hCtx, batal := context.WithTimeout(ctx, 3*time.Second)
			resp, err := client.Heartbeat(hCtx, &cluster.HeartbeatRequest{
				NodeId:      cfg.NodeID,
				SessionId:   sessionID,
				ActiveTasks: 0, // TODO: dilacak dari semaphore worker
			})
			batal()

			if err != nil {
				log.Warn("heartbeat error", slog.String("error", err.Error()))
			} else if !resp.GetOk() {
				// Master menolak heartbeat — kemungkinan master restart dan kehilangan state.
				log.Warn("heartbeat ditolak, mendaftar ulang ke master")
				_ = registerDenganRetry(ctx, client, cfg, sessionID, log)
			}
		}
	}
}

// monitorNodeMati menjalankan TickDeadCheck setiap detik dan memicu reschedule
// untuk task yang sebelumnya dijalankan di node yang kini dinyatakan mati.
func monitorNodeMati(ctx context.Context, reg *registry.Registry, sched *scheduler.Scheduler, log *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mati := reg.TickDeadCheck()
			if len(mati) > 0 {
				log.Warn("node mati terdeteksi, task dijadwalkan ulang",
					slog.Any("node_ids", mati))
				sched.RescheduleDeadNodeTasks(mati)
			}
		}
	}
}

// pembersihJobTTL menghapus job yang sudah melewati batas waktu retensi.
// Berjalan setiap 5 menit untuk menghindari penumpukan file di disk.
func pembersihJobTTL(ctx context.Context, sched *scheduler.Scheduler, store *storage.Store, ttl time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sekarang := time.Now()
			for _, j := range sched.ListJobs() {
				if j.Status != scheduler.JobDone && j.Status != scheduler.JobFailed {
					continue
				}
				if sekarang.Sub(j.CreatedAt) > ttl {
					if err := sched.CancelJob(j.ID); err == nil {
						log.Info("job dihapus (TTL kadaluarsa)",
							slog.String("job_id", j.ID),
							slog.Duration("umur", sekarang.Sub(j.CreatedAt)))
					}
				}
			}
		}
	}
}

// hookPendaftaran mengintersep Register dari Coordinator untuk membuka
// koneksi balik (back-connection) ke node yang baru mendaftar.
// Ini diperlukan karena master perlu bisa memanggil ProcessImage ke node.
type hookPendaftaran struct {
	*rpcpkg.CoordinatorServer
	nodeClient *rpcpkg.NodeClientAdapter
	token      string
	log        *slog.Logger
}

func (h *hookPendaftaran) Register(ctx context.Context, req *cluster.RegisterRequest) (*cluster.RegisterResponse, error) {
	resp, err := h.CoordinatorServer.Register(ctx, req)
	if err != nil || !resp.GetAccepted() {
		return resp, err
	}

	// Buka koneksi ke node secara asinkron agar tidak memblok Register response.
	go func() {
		conn, dialErr := rpcpkg.DialNode(req.GetAdvertiseAddr(), h.token)
		if dialErr != nil {
			h.log.Warn("tidak bisa dial node (akan dicoba saat dispatch)",
				slog.String("node_id", req.GetNodeId()),
				slog.String("addr", req.GetAdvertiseAddr()),
				slog.String("error", dialErr.Error()))
			return
		}
		h.nodeClient.SetConn(req.GetNodeId(), conn)
		h.log.Info("koneksi ke node berhasil dibuka",
			slog.String("node_id", req.GetNodeId()),
			slog.String("addr", req.GetAdvertiseAddr()))
	}()

	return resp, nil
}

// buatSessionID menghasilkan 4-byte hex acak sebagai identifier sesi node.
func buatSessionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
