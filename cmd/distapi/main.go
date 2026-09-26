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
	"strings"
	"syscall"
	"time"

	"distapi/gen/cluster"
	"distapi/internal/api"
	"distapi/internal/config"
	"distapi/internal/registry"
	rpcpkg "distapi/internal/rpc"
	"distapi/internal/scheduler"
	"distapi/internal/storage"
	"distapi/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg := config.Parse()

	// Jika TUI aktif, alihkan log ke file agar antarmuka terminal tetap bersih
	var log *slog.Logger
	if cfg.TUI {
		f, err := os.OpenFile("distapi-tui.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			log = slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: cfg.LogLevel}))
		} else {
			log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
		}
	} else {
		log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	}
	slog.SetDefault(log)

	log.Info("distapi mulai",
		slog.String("mode", string(cfg.Mode)),
		slog.String("versi", "0.1.0"),
		slog.Bool("tui", cfg.TUI))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cfg.Mode {
	case config.ModeMaster:
		jalankanMaster(ctx, cfg, log)
	case config.ModeNode:
		jalankanNode(ctx, cfg, log)
	}
}

func jalankanMaster(parentCtx context.Context, cfg config.Config, log *slog.Logger) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

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

	// Event log untuk TUI (tetap dibuat agar bisa dipakai meski TUI nonaktif)
	evLog := tui.NewEventLog(200)

	hook := &hookPendaftaran{
		CoordinatorServer: coordSrv,
		nodeClient:        nodeClient,
		token:             cfg.Token,
		log:               log,
		evLog:             evLog,
	}
	cluster.RegisterCoordinatorServer(grpcSrv, hook)
	cluster.RegisterWorkerServer(grpcSrv, rpcpkg.NewWorkerServer(log))

	go monitorNodeMati(ctx, reg, sched, log, evLog)
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

	masterIP := config.DetectLocalIP()
	evLog.Info(fmt.Sprintf("master aktif di %s:%d (HTTP :%d)", masterIP, cfg.GRPCPort, cfg.HTTPPort))
	evLog.Info(fmt.Sprintf("perintah worker: .\\dist\\distapi.exe --mode=node --master=%s:%d --token=%s --tui",
		masterIP, cfg.GRPCPort, cfg.Token))

	if !cfg.TUI {
		fmt.Printf("\n[distapi Master Aktif]\n"+
			"  IP Master Terdeteksi : %s\n"+
			"  Web UI / REST API    : http://%s:%d (atau http://localhost:%d)\n"+
			"  gRPC Cluster Port    : %s:%d\n\n"+
			"Perintah yang dapat disalin untuk laptop Node Worker:\n"+
			"  .\\dist\\distapi.exe --mode=node --master=%s:%d --token=%s --tui\n\n",
			masterIP, masterIP, cfg.HTTPPort, cfg.HTTPPort, masterIP, cfg.GRPCPort, masterIP, cfg.GRPCPort, cfg.Token)
	}

	if cfg.TUI {
		model := tui.NewMasterModel(reg, sched, evLog, masterIP, cfg.HTTPPort, cfg.GRPCPort, cfg.Token)
		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			log.Error("TUI error", slog.String("error", err.Error()))
		}
		cancel()
	} else {
		<-ctx.Done()
	}

	log.Info("master: mulai shutdown...")
	shutCtx, batal := context.WithTimeout(context.Background(), 10*time.Second)
	defer batal()
	_ = srvHTTP.Shutdown(shutCtx)
	log.Info("master: shutdown selesai")
}

func jalankanNode(parentCtx context.Context, cfg config.Config, log *slog.Logger) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	sessionID := buatSessionID()
	log.Info("node mulai",
		slog.String("node_id", cfg.NodeID),
		slog.String("session", sessionID),
		slog.String("master", cfg.MasterAddr),
		slog.String("advertise", cfg.AdvertiseAddr))

	nodeIP := config.DetectLocalIP()
	nodeIDTeks := cfg.NodeID
	if nodeIDTeks == "auto" || nodeIDTeks == "" {
		nodeIDTeks = "auto (menunggu alokasi dari master...)"
	}
	if !cfg.TUI {
		fmt.Printf("\n[distapi Node Worker Aktif]\n"+
			"  Node ID              : %s\n"+
			"  IP Node Terdeteksi   : %s\n"+
			"  Advertise ke Master  : %s\n"+
			"  Master Tujuan        : %s\n\n",
			nodeIDTeks, nodeIP, cfg.AdvertiseAddr, cfg.MasterAddr)
	}

	evLog := tui.NewEventLog(100)
	evLog.Info(fmt.Sprintf("node aktif di IP %s (advertise: %s, master: %s)", nodeIP, cfg.AdvertiseAddr, cfg.MasterAddr))
	state := tui.NewNodeState(cfg.NodeID, sessionID, cfg.MasterAddr, cfg.AdvertiseAddr)

	grpcSrv := rpcpkg.NewGRPCServer(cfg.Token)
	workerSrv := rpcpkg.NewWorkerServer(log)
	workerSrv.SetObserver(state)
	cluster.RegisterWorkerServer(grpcSrv, workerSrv)
	go func() {
		if err := rpcpkg.ServeGRPC(ctx, grpcSrv, cfg.GRPCPort, log); err != nil {
			log.Error("node gRPC error", slog.String("error", err.Error()))
		}
	}()

	conn, err := rpcpkg.DialNode(cfg.MasterAddr, cfg.Token)
	if err != nil {
		log.Error("tidak bisa terhubung ke master", slog.String("error", err.Error()))
		fmt.Fprintf(os.Stderr, "\n[BANTUAN TROUBLESHOOTING JARINGAN]\n"+
			"  - Alamat Master: %s\n"+
			"  - Pastikan proses Master sudah aktif terlebih dahulu.\n"+
			"  - Jika menggunakan Wi-Fi kampus/lab, periksa apakah ada Client Isolation.\n"+
			"    Rekomendasi demo: gunakan Windows Mobile Hotspot dari Laptop Master.\n"+
			"  - Pastikan port 9000 TCP diizinkan di Windows Firewall.\n\n", cfg.MasterAddr)
		os.Exit(1)
	}
	defer conn.Close()

	coordClient := cluster.NewCoordinatorClient(conn)

	assignedID, err := registerDenganRetry(ctx, coordClient, cfg, sessionID, log)
	if err != nil {
		log.Error("registrasi ke master gagal", slog.String("error", err.Error()))
		fmt.Fprintf(os.Stderr, "\n[BANTUAN TROUBLESHOOTING JARINGAN]\n"+
			"  - Gagal registrasi ke Master di %s (5x percobaan timeout/ditolak).\n"+
			"  - Kemungkinan Penyebab:\n"+
			"    1. Wi-Fi Isolation: AP router kampus memblokir koneksi antar-laptop.\n"+
			"       Solusi demo: Aktifkan Windows Mobile Hotspot di Laptop Master (Laptop 1).\n"+
			"    2. Windows Firewall memblokir port 9000 TCP masuk di Laptop Master.\n"+
			"    3. Token autentikasi berbeda (default: demo123).\n"+
			"    4. Laptop berada di subnet/VLAN berbeda.\n\n", cfg.MasterAddr)
		os.Exit(1)
	}
	cfg.NodeID = assignedID
	state.SetNodeID(assignedID)
	state.RecordHeartbeatSuccess()
	evLog.Info("terdaftar ke master sebagai " + assignedID)

	if !cfg.TUI {
		fmt.Printf("[Node Terdaftar ke Master]\n"+
			"  Identitas Terkonfirmasi : %s\n"+
			"  Alamat Advertise        : %s\n"+
			"  Master Hubungan         : %s\n\n",
			assignedID, cfg.AdvertiseAddr, cfg.MasterAddr)
	}

	go loopHeartbeat(ctx, coordClient, cfg, sessionID, log, state, evLog)

	if cfg.TUI {
		model := tui.NewNodeModel(state, evLog)
		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			log.Error("TUI error", slog.String("error", err.Error()))
		}
		cancel()
	} else {
		<-ctx.Done()
	}

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
) (string, error) {
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
			assignedID := cfg.NodeID
			msg := resp.GetMessage()
			if strings.HasPrefix(msg, "ASSIGNED:") {
				parts := strings.SplitN(msg[9:], "|", 2)
				assignedID = parts[0]
				if len(parts) > 1 && parts[1] != "ok" {
					log.Warn("peringatan registrasi dari master", slog.String("detail", parts[1]))
				}
			}
			log.Info("terdaftar ke master",
				slog.String("node_id", assignedID),
				slog.String("session", sessionID))
			return assignedID, nil
		}

		errMsg := "koneksi gagal"
		if resp != nil && !resp.GetAccepted() {
			errMsg = resp.GetMessage()
		} else if err != nil {
			errMsg = err.Error()
		}

		log.Warn("registrasi gagal, mencoba ulang",
			slog.Int("percobaan", percobaan),
			slog.String("alasan", errMsg))

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(percobaan) * time.Second):
		}
	}
	return "", fmt.Errorf("semua percobaan registrasi gagal")
}

func loopHeartbeat(
	ctx context.Context,
	client cluster.CoordinatorClient,
	cfg config.Config,
	sessionID string,
	log *slog.Logger,
	state *tui.NodeState,
	evLog *tui.EventLog,
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
				ActiveTasks: int32(state.ActiveTaskCount()),
			})
			batal()

			if err != nil {
				log.Warn("heartbeat error", slog.String("error", err.Error()))
				state.RecordHeartbeatFailure()
				evLog.Warn("heartbeat gagal: " + err.Error())
			} else if !resp.GetOk() {
				log.Warn("heartbeat ditolak, mendaftar ulang ke master")
				evLog.Warn("heartbeat ditolak — mendaftar ulang")
				state.RecordHeartbeatFailure()
				if newID, err := registerDenganRetry(ctx, client, cfg, sessionID, log); err == nil {
					cfg.NodeID = newID
					state.SetNodeID(newID)
					state.RecordHeartbeatSuccess()
				}
			} else {
				state.RecordHeartbeatSuccess()
			}
		}
	}
}

func monitorNodeMati(ctx context.Context, reg *registry.Registry, sched *scheduler.Scheduler, log *slog.Logger, evLog *tui.EventLog) {
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
				for _, id := range mati {
					evLog.Warn("node " + id + " dinyatakan mati — task dijadwalkan ulang")
				}
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
	evLog      *tui.EventLog
}

func (h *hookPendaftaran) Register(ctx context.Context, req *cluster.RegisterRequest) (*cluster.RegisterResponse, error) {
	resp, err := h.CoordinatorServer.Register(ctx, req)
	if err != nil || !resp.GetAccepted() {
		return resp, err
	}

	actualID := req.GetNodeId()
	msg := resp.GetMessage()
	if strings.HasPrefix(msg, "ASSIGNED:") {
		parts := strings.SplitN(msg[9:], "|", 2)
		actualID = parts[0]
	}

	h.evLog.Info(fmt.Sprintf("node %s bergabung dari %s", actualID, req.GetAdvertiseAddr()))

	go func() {
		conn, dialErr := rpcpkg.DialNode(req.GetAdvertiseAddr(), h.token)
		if dialErr != nil {
			h.log.Warn("gagal membuka koneksi ke node",
				slog.String("node_id", actualID),
				slog.String("addr", req.GetAdvertiseAddr()),
				slog.String("error", dialErr.Error()))
			h.evLog.Warn(fmt.Sprintf("koneksi ke %s gagal: %s", actualID, dialErr.Error()))
			return
		}
		h.nodeClient.SetConn(actualID, conn)
		h.log.Info("koneksi ke node dibuka",
			slog.String("node_id", actualID),
			slog.String("addr", req.GetAdvertiseAddr()))
	}()

	return resp, nil
}

func buatSessionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
