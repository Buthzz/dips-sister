package rmi

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"os"
	"testing"
	"time"
)

// mockHandler mengimplementasikan CoordinatorHandler untuk pengujian unit.
type mockHandler struct {
	electionCalled bool
	victoryCalled  bool
	hbCalled       bool
}

func (m *mockHandler) HandleElection(args ElectionArgs) (ElectionReply, error) {
	m.electionCalled = true
	return ElectionReply{
		OK:          args.Priority < 5,
		ResponderID: "mock-coordinator",
		Priority:    5,
	}, nil
}

func (m *mockHandler) HandleVictory(args VictoryArgs) (VictoryReply, error) {
	m.victoryCalled = true
	return VictoryReply{
		Acknowledged: true,
		NodeID:       "mock-node",
	}, nil
}

func (m *mockHandler) HandleHeartbeat(args HeartbeatArgs) (HeartbeatReply, error) {
	m.hbCalled = true
	return HeartbeatReply{
		OK:            true,
		CoordinatorID: "mock-coordinator",
		Peers: []Peer{
			{NodeID: "mock-coordinator", Priority: 10, IsLeader: true},
			{NodeID: args.NodeID, Priority: args.Priority, IsLeader: false},
		},
	}, nil
}

func (m *mockHandler) GetClusterView() ClusterViewReply {
	return ClusterViewReply{
		CoordinatorID: "mock-coordinator",
		Peers: []Peer{
			{NodeID: "mock-coordinator", Priority: 10, IsLeader: true},
		},
	}
}

func buatGambarUjiPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("gagal encode png: %v", err)
	}
	return buf.Bytes()
}

func TestRMIServerClient(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mock := &mockHandler{}

	coordSvc := NewCoordinatorService("node-test", "master", mock, log)
	procSvc := NewImageProcessorService("node-test", log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Port 0 agar OS memilih port yang bebas
	srv := NewServer(0, coordSvc, procSvc, log)
	go func() {
		_ = srv.Start(ctx)
	}()

	// Tunggu server mulai
	var port int
	for i := 0; i < 20; i++ {
		port = srv.Port()
		if port > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("RMI server gagal mendapatkan port")
	}

	addr := srv.listener.Addr().String()

	t.Run("Ping Coordinator & ImageProcessor", func(t *testing.T) {
		reply, err := Ping(addr, "tester", 2*time.Second)
		if err != nil {
			t.Fatalf("Ping gagal: %v", err)
		}
		if reply.NodeID != "node-test" {
			t.Errorf("NodeID mau 'node-test', dapat %s", reply.NodeID)
		}
		if reply.Status != "online" && reply.Status != "ready" {
			t.Errorf("Status tidak sesuai: %s", reply.Status)
		}
	})

	t.Run("Election via RMI", func(t *testing.T) {
		args := ElectionArgs{
			CandidateID:   "node-low",
			CandidateAddr: "127.0.0.1:9001",
			CandidateRMI:  "127.0.0.1:9051",
			Priority:      2,
		}
		reply, err := SendElection(addr, args, 2*time.Second)
		if err != nil {
			t.Fatalf("SendElection gagal: %v", err)
		}
		if !reply.OK {
			t.Errorf("Election harus dibalas OK=true untuk prioritas rendah, dapat %v", reply.OK)
		}
		if !mock.electionCalled {
			t.Error("mock handler HandleElection tidak dipanggil")
		}
	})

	t.Run("AnnounceCoordinator via RMI", func(t *testing.T) {
		args := VictoryArgs{
			CoordinatorID:   "node-winner",
			CoordinatorAddr: "127.0.0.1:9002",
			CoordinatorHTTP: "127.0.0.1:8082",
			CoordinatorRMI:  "127.0.0.1:9052",
			Priority:        10,
		}
		reply, err := SendVictory(addr, args, 2*time.Second)
		if err != nil {
			t.Fatalf("SendVictory gagal: %v", err)
		}
		if !reply.Acknowledged {
			t.Errorf("Victory harus di-acknowledge, dapat %v", reply.Acknowledged)
		}
		if !mock.victoryCalled {
			t.Error("mock handler HandleVictory tidak dipanggil")
		}
	})

	t.Run("Heartbeat via RMI", func(t *testing.T) {
		args := HeartbeatArgs{
			NodeID:   "node-worker-1",
			RMIAddr:  "127.0.0.1:9053",
			Priority: 1,
		}
		reply, err := SendHeartbeat(addr, args, 2*time.Second)
		if err != nil {
			t.Fatalf("SendHeartbeat gagal: %v", err)
		}
		if !reply.OK {
			t.Errorf("Heartbeat harus OK, dapat %v", reply.OK)
		}
		if len(reply.Peers) == 0 {
			t.Error("Heartbeat harus mengembalikan daftar peers")
		}
		if !mock.hbCalled {
			t.Error("mock handler HandleHeartbeat tidak dipanggil")
		}
	})

	t.Run("Image Transform via RMI", func(t *testing.T) {
		data := buatGambarUjiPNG(t, 200, 200)
		args := ProcessImageArgs{
			TaskID:       "task-rmi-001",
			JobID:        "job-rmi",
			ImageIndex:   0,
			ImageData:    data,
			Filename:     "test.png",
			ResizeWidth:  80,
			ResizeHeight: 80,
			Grayscale:    true,
		}

		reply, err := InvokeProcessImage(addr, args, 3*time.Second)
		if err != nil {
			t.Fatalf("InvokeProcessImage gagal: %v", err)
		}
		if !reply.Success {
			t.Fatalf("TransformImage gagal: %s", reply.Error)
		}
		if len(reply.ResultData) == 0 {
			t.Fatal("ResultData kosong")
		}

		// Validasi format gambar terkonversi
		cfg, _, err := image.DecodeConfig(bytes.NewReader(reply.ResultData))
		if err != nil {
			t.Fatalf("Gagal membaca config gambar hasil: %v", err)
		}
		if cfg.Width != 80 || cfg.Height != 80 {
			t.Errorf("Dimensi gambar mau 80x80, dapat %dx%d", cfg.Width, cfg.Height)
		}
	})
}
