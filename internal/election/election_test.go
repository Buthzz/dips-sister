package election

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"distapi/internal/rmi"
)

func TestParsePriority(t *testing.T) {
	kasus := []struct {
		nodeID   string
		fallback int
		want     int
	}{
		{"master", 0, 100},
		{"master-1", 0, 100},
		{"node-1", 0, 1},
		{"node-2", 0, 2},
		{"node-10", 0, 10},
		{"laptop-3", 0, 3},
		{"custom-node", 42, 42},
	}

	for _, k := range kasus {
		got := ParsePriority(k.nodeID, k.fallback)
		if got != k.want {
			t.Errorf("ParsePriority(%q, %d): mau %d, dapat %d", k.nodeID, k.fallback, k.want, got)
		}
	}
}

func TestBullyAlgorithmElection(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Buat 3 node simulasi: Node 1 (prio 1), Node 2 (prio 2), Node 3 (prio 3)
	type testNode struct {
		engine *Engine
		server *rmi.Server
		addr   string
	}

	nodes := make([]*testNode, 3)

	for i := 0; i < 3; i++ {
		nodeID := fmt.Sprintf("node-%d", i+1)
		cfg := Config{
			NodeID:          nodeID,
			GRPCAddr:        fmt.Sprintf("127.0.0.1:%d", 9100+i),
			RMIAddr:         "", // ditentukan setelah port listen
			HTTPAddr:        fmt.Sprintf("127.0.0.1:%d", 8100+i),
			Priority:        i + 1,
			IsInitialLeader: false,
			AutoFailover:    true,
		}

		eng := NewEngine(cfg, log)
		coordSvc := rmi.NewCoordinatorService(nodeID, "node", eng, log)
		procSvc := rmi.NewImageProcessorService(nodeID, log)

		srv := rmi.NewServer(0, coordSvc, procSvc, log)
		go func(s *rmi.Server) {
			_ = s.Start(ctx)
		}(srv)

		// Tunggu port listener aktif
		for j := 0; j < 30; j++ {
			if srv.Port() > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		addr := fmt.Sprintf("127.0.0.1:%d", srv.Port())
		eng.UpdateSelf(nodeID, cfg.GRPCAddr, addr, cfg.HTTPAddr)

		nodes[i] = &testNode{
			engine: eng,
			server: srv,
			addr:   addr,
		}
	}

	// Saling daftarkan peer antar-3 node
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if i != j {
				nodes[i].engine.RegisterPeer(rmi.Peer{
					NodeID:   fmt.Sprintf("node-%d", j+1),
					GRPCAddr: fmt.Sprintf("127.0.0.1:%d", 9100+j),
					RMIAddr:  nodes[j].addr,
					Priority: j + 1,
				})
			}
		}
	}

	var victoryReceived sync.WaitGroup
	victoryReceived.Add(2) // Node 1 dan Node 2 harus menerima VICTORY dari Node 3

	var electedLeader string
	var muLeader sync.Mutex
	var once1, once2 sync.Once

	nodes[0].engine.SetCallbacks(
		nil,
		func(coord rmi.Peer) {
			once1.Do(func() {
				muLeader.Lock()
				electedLeader = coord.NodeID
				muLeader.Unlock()
				victoryReceived.Done()
			})
		},
		nil,
	)

	nodes[1].engine.SetCallbacks(
		nil,
		func(coord rmi.Peer) {
			once2.Do(func() {
				muLeader.Lock()
				electedLeader = coord.NodeID
				muLeader.Unlock()
				victoryReceived.Done()
			})
		},
		nil,
	)

	// Node 1 (prioritas terendah) mendeteksi koordinator mati dan memulai pemilihan Bully
	go nodes[0].engine.StartElection()

	// Tunggu proses pemilihan selesai
	doneCh := make(chan struct{})
	go func() {
		victoryReceived.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// Berhasil!
	case <-time.After(6 * time.Second):
		t.Fatal("Timeout menunggu deklarasi kemenangan pemilihan koordinator")
	}

	muLeader.Lock()
	leader := electedLeader
	muLeader.Unlock()

	// Node 3 (prioritas tertinggi: 3) harus menang pemilihan!
	if leader != "node-3" {
		t.Errorf("Koordinator terpilih harus 'node-3', dapat: %s", leader)
	}

	if !nodes[2].engine.IsLeader() {
		t.Errorf("Node 3 harus menganggap dirinya sebagai Leader, dapat: %v", nodes[2].engine.IsLeader())
	}
}

func TestCoordinatorFailover(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Node 1: Koordinator Awal (prio 10)
	// Node 2: Worker (prio 2)
	// Node 3: Worker (prio 5)
	cfg1 := Config{
		NodeID:          "node-coord",
		GRPCAddr:        "127.0.0.1:9201",
		Priority:        10,
		IsInitialLeader: true,
		AutoFailover:    true,
	}
	eng1 := NewEngine(cfg1, log)
	coordSvc1 := rmi.NewCoordinatorService("node-coord", "master", eng1, log)
	srv1 := rmi.NewServer(0, coordSvc1, nil, log)
	go func() { _ = srv1.Start(ctx) }()
	for srv1.Port() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	addr1 := fmt.Sprintf("127.0.0.1:%d", srv1.Port())
	eng1.UpdateSelf("node-coord", cfg1.GRPCAddr, addr1, "127.0.0.1:8201")

	// Node 2
	cfg2 := Config{
		NodeID:              "node-worker-2",
		GRPCAddr:            "127.0.0.1:9202",
		Priority:            2,
		IsInitialLeader:     false,
		AutoFailover:        true,
		HeartbeatInterval:   50 * time.Millisecond,
		CoordinatorTimeout:  150 * time.Millisecond,
		InitialCoordinator:  rmi.Peer{NodeID: "node-coord", RMIAddr: addr1, Priority: 10},
	}
	eng2 := NewEngine(cfg2, log)
	coordSvc2 := rmi.NewCoordinatorService("node-worker-2", "node", eng2, log)
	srv2 := rmi.NewServer(0, coordSvc2, nil, log)
	go func() { _ = srv2.Start(ctx) }()
	for srv2.Port() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	addr2 := fmt.Sprintf("127.0.0.1:%d", srv2.Port())
	eng2.UpdateSelf("node-worker-2", cfg2.GRPCAddr, addr2, "127.0.0.1:8202")

	// Node 3 (akan menjadi koordinator pengganti karena prioritasnya 5 > 2)
	cfg3 := Config{
		NodeID:              "node-worker-3",
		GRPCAddr:            "127.0.0.1:9203",
		Priority:            5,
		IsInitialLeader:     false,
		AutoFailover:        true,
		HeartbeatInterval:   50 * time.Millisecond,
		CoordinatorTimeout:  150 * time.Millisecond,
		InitialCoordinator:  rmi.Peer{NodeID: "node-coord", RMIAddr: addr1, Priority: 10},
	}
	eng3 := NewEngine(cfg3, log)
	coordSvc3 := rmi.NewCoordinatorService("node-worker-3", "node", eng3, log)
	srv3 := rmi.NewServer(0, coordSvc3, nil, log)
	go func() { _ = srv3.Start(ctx) }()
	for srv3.Port() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	addr3 := fmt.Sprintf("127.0.0.1:%d", srv3.Port())
	eng3.UpdateSelf("node-worker-3", cfg3.GRPCAddr, addr3, "127.0.0.1:8203")

	// Saling daftarkan peers
	eng2.RegisterPeer(rmi.Peer{NodeID: "node-worker-3", RMIAddr: addr3, Priority: 5})
	eng3.RegisterPeer(rmi.Peer{NodeID: "node-worker-2", RMIAddr: addr2, Priority: 2})

	var failoverDone sync.WaitGroup
	failoverDone.Add(1)

	var promotedLeader string
	var mu sync.Mutex
	var onceFailover sync.Once

	eng3.SetCallbacks(
		func(c rmi.Peer) {
			onceFailover.Do(func() {
				mu.Lock()
				promotedLeader = c.NodeID
				mu.Unlock()
				failoverDone.Done()
			})
		},
		nil,
		nil,
	)

	// Jalankan monitor di background untuk Node 2 dan Node 3
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	defer stopMonitor()
	go eng2.StartMonitor(monitorCtx)
	go eng3.StartMonitor(monitorCtx)

	// Beri jeda agar heartbeat awal berjalan
	time.Sleep(100 * time.Millisecond)

	// SIMULASIKAN KOORDINATOR MATI!
	srv1.Stop()

	// Tunggu failure detector mendeteksi dan Bully algorithm memilih pengganti
	doneCh := make(chan struct{})
	go func() {
		failoverDone.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// Berhasil!
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout: Node pengganti gagal mengambil alih kepemimpinan koordinator")
	}

	mu.Lock()
	winner := promotedLeader
	mu.Unlock()

	if winner != "node-worker-3" {
		t.Errorf("Koordinator baru yang dipromosikan harus 'node-worker-3', dapat: %s", winner)
	}
}
