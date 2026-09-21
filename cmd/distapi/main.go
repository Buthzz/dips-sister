// Package main menyediakan titik masuk aplikasi distapi (mode master dan node).
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

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	log.Info("distapi mulai",
		slog.String("mode", string(cfg.Mode)),
		slog.String("versi", "0.1.0"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cfg.Mode {
	case config.ModeMaster:
		jalankanMaster(ctx, cfg, log)
	case config.ModeNode:
		jalankanNode(ctx, cfg, log)
	}
}

func jalankanMaster(ctx context.Context, cfg config.Config, log *slog.Logger) {
	store, err := storage.New(cfg.DataDir)
	if err != nil {
		log.Error("inisialisasi storage gagal", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := store.Init(); err != nil {
		log.Error("storage.Init gagal", slog.String("error", err.Error()))
		os.Exit(1)
	}

	reg := registry.New(cfg.NodeTimeout, log)
	nodeClient := rpcpkg.NewNodeClientAdapter(cfg.Token)
	defer nodeClient.Close()

	sched := scheduler.New(reg, store, nodeClient, cfg.MaxRetries, cfg.TaskTimeout, log)

	grpcSrv := rpcpkg.NewGRPCServer(cfg.Token)
	coordSrv := rpcpkg.NewCoordinatorServer(reg, log)

	hook := &hookPendaftaran{
		CoordinatorServer: coordSrv,
		nodeClient:        nodeClient,
		token:             cfg.Token,
		log:               log,
	}
	cluster.RegisterCoordinatorServer(grpcSrv, hook)
	cluster.RegisterWorkerServer(grpcSrv, rpcpkg.NewWorkerServer(log))

	go monitorNodeMati(ctx, reg, sched, log)
	go pembersihJobTTL(ctx, sched, store, cfg.JobTTL, log)

	go func() {
		if err := rpcpkg.ServeGRPC(ctx, grpcSrv, cfg.GRPCPort, log); err != nil {
			log.Error("gRPC server error", slog.String("error", err.Error()))
		}
	}()

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

	shutCtx, batal := context.WithTimeout(context.Background(), 10*time.Second)
	defer batal()
	_ = srvHTTP.Shutdown(shutCtx)
	log.Info("master: shutdown selesai")
}

func jalankanNode(ctx context.Context, cfg config.Config, log *slog.Logger) {
	sessionID := buatSessionID()
	log.Info("node mulai",
		slog.String("node_id", cfg.NodeID),
		slog.String("session", sessionID),
		slog.String("master", cfg.MasterAddr),
		slog.String("advertise", cfg.AdvertiseAddr))

	grpcSrv := rpcpkg.NewGRPCServer(cfg.Token)
	cluster.RegisterWorkerServer(grpcSrv, rpcpkg.NewWorkerServer(log))
	go func() {
		if err := rpcpkg.ServeGRPC(ctx, grpcSrv, cfg.GRPCPort, log); err != nil {
			log.Error("node gRPC error", slog.String("error", err.Error()))
		}
	}()

	conn, err := rpcpkg.DialNode(cfg.MasterAddr, cfg.Token)
	if err != nil {
		log.Error("tidak bisa terhubung ke master", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer conn.Close()

	coordClient := cluster.NewCoordinatorClient(conn)

	if err := registerDenganRetry(ctx, coordClient, cfg, sessionID, log); err != nil {
		log.Error("registrasi ke master gagal", slog.String("error", err.Error()))
		os.Exit(1)
	}

	go loopHeartbeat(ctx, coordClient, cfg, sessionID, log)

	<-ctx.Done()
	log.Info("node: menghentikan server...")
	grpcSrv.GracefulStop()
	log.Info("node: shutdown selesai")
}

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

		log.Warn("registrasi gagal, mencoba ulang",
			slog.Int("percobaan", percobaan),
			slog.String("error", fmt.Sprintf("%v", err)))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(percobaan) * time.Second):
		}
	}
	return fmt.Errorf("semua percobaan registrasi gagal")
}

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
				ActiveTasks: 0,
			})
			batal()

			if err != nil {
				log.Warn("heartbeat error", slog.String("error", err.Error()))
			} else if !resp.GetOk() {
				log.Warn("heartbeat ditolak, mendaftar ulang ke master")
				_ = registerDenganRetry(ctx, client, cfg, sessionID, log)
			}
		}
	}
}

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
				log.Warn("node mati terdeteksi", slog.Any("node_ids", mati))
				sched.RescheduleDeadNodeTasks(mati)
			}
		}
	}
}

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
						log.Info("job kedaluwarsa dibersihkan",
							slog.String("job_id", j.ID),
							slog.Duration("umur", sekarang.Sub(j.CreatedAt)))
					}
				}
			}
		}
	}
}

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

	go func() {
		conn, dialErr := rpcpkg.DialNode(req.GetAdvertiseAddr(), h.token)
		if dialErr != nil {
			h.log.Warn("gagal membuka koneksi ke node",
				slog.String("node_id", req.GetNodeId()),
				slog.String("addr", req.GetAdvertiseAddr()),
				slog.String("error", dialErr.Error()))
			return
		}
		h.nodeClient.SetConn(req.GetNodeId(), conn)
		h.log.Info("koneksi ke node dibuka",
			slog.String("node_id", req.GetNodeId()),
			slog.String("addr", req.GetAdvertiseAddr()))
	}()

	return resp, nil
}

func buatSessionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
