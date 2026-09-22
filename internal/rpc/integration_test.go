// Integration test: menjalankan master gRPC + node nyata dalam satu proses,
// memverifikasi registrasi, heartbeat, dan pemrosesan gambar end-to-end.
//
// Jalankan dengan:
//
//	go test distapi/internal/rpc -run TestIntegration -v -timeout 30s
package rpc_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"distapi/gen/cluster"
	"distapi/internal/registry"
	rpcpkg "distapi/internal/rpc"

	"google.golang.org/grpc"
)

const integrationToken = "test-token-integ"

// TestIntegration_RegisterAndHeartbeat memverifikasi alur:
//  1. Master gRPC server berjalan di port acak.
//  2. Node mendaftar ke master (Register RPC).
//  3. Node mengirim heartbeat berturut-turut.
//  4. Registry master mencatat node sebagai alive.
//  5. Heartbeat berhenti → master mendeteksi node mati setelah timeout.
func TestIntegration_RegisterAndHeartbeat(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Jalankan master gRPC di port acak
	masterPort := freePort(t)
	masterAddr := fmt.Sprintf("127.0.0.1:%d", masterPort)

	reg := registry.New(3*time.Second, log) // timeout node mati: 3 detik

	grpcSrv := rpcpkg.NewGRPCServer(integrationToken)
	coordSrv := rpcpkg.NewCoordinatorServer(reg, log)
	cluster.RegisterCoordinatorServer(grpcSrv, coordSrv)
	cluster.RegisterWorkerServer(grpcSrv, rpcpkg.NewWorkerServer(log))

	lis, err := net.Listen("tcp", masterAddr)
	if err != nil {
		t.Fatalf("listen gagal: %v", err)
	}
	t.Logf("master gRPC di %s", masterAddr)

	go grpcSrv.Serve(lis) //nolint:errcheck
	t.Cleanup(func() { grpcSrv.GracefulStop() })

	// Jalankan gRPC server node di port acak
	nodePort := freePort(t)
	nodeAddr := fmt.Sprintf("127.0.0.1:%d", nodePort)

	nodeGRPC := rpcpkg.NewGRPCServer(integrationToken)
	cluster.RegisterWorkerServer(nodeGRPC, rpcpkg.NewWorkerServer(log))

	nodeLis, err := net.Listen("tcp", nodeAddr)
	if err != nil {
		t.Fatalf("node listen gagal: %v", err)
	}
	go nodeGRPC.Serve(nodeLis) //nolint:errcheck
	t.Cleanup(func() { nodeGRPC.GracefulStop() })

	// Koneksi klien node ke master
	conn, err := rpcpkg.DialNode(masterAddr, integrationToken)
	if err != nil {
		t.Fatalf("dial master gagal: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	coordClient := cluster.NewCoordinatorClient(conn)

	// Registrasi node ke coordinator
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := coordClient.Register(ctx, &cluster.RegisterRequest{
		NodeId:        "integ-node-1",
		SessionId:     "sess-abc",
		AdvertiseAddr: nodeAddr,
		Capacity:      2,
	})
	if err != nil {
		t.Fatalf("Register RPC gagal: %v", err)
	}
	if !resp.GetAccepted() {
		t.Fatalf("Register ditolak: %s", resp.GetMessage())
	}
	t.Logf("Register OK: %s", resp.GetMessage())

	// Verifikasi entri tersimpan di registry
	time.Sleep(100 * time.Millisecond)
	alive := reg.AliveNodes()
	if len(alive) == 0 {
		t.Fatal("registry tidak mencatat node sebagai alive")
	}
	if alive[0].NodeID != "integ-node-1" {
		t.Fatalf("node ID tidak cocok: got %s", alive[0].NodeID)
	}
	t.Logf("Registry: node %q tercatat di %s (status=%s)", alive[0].NodeID, alive[0].AdvertiseAddr, alive[0].Status)

	// Kirim 3 kali heartbeat periodik
	for i := range 3 {
		hCtx, hCancel := context.WithTimeout(context.Background(), 2*time.Second)
		hResp, hErr := coordClient.Heartbeat(hCtx, &cluster.HeartbeatRequest{
			NodeId:      "integ-node-1",
			SessionId:   "sess-abc",
			ActiveTasks: int32(i),
		})
		hCancel()
		if hErr != nil {
			t.Fatalf("Heartbeat #%d gagal: %v", i+1, hErr)
		}
		if !hResp.GetOk() {
			t.Fatalf("Heartbeat #%d ditolak", i+1)
		}
		t.Logf("Heartbeat #%d OK", i+1)
		time.Sleep(200 * time.Millisecond)
	}

	// Tunggu node timeout (timeout registry 3 detik)
	t.Log("menunggu node timeout (5 detik)...")
	time.Sleep(5 * time.Second)

	dead := reg.TickDeadCheck()
	// Setelah TickDeadCheck, node yang baru saja ditandai mati ada di slice dead
	// atau sudah ditandai pada tick sebelumnya — cek status langsung
	nodeInfo := reg.GetNode("integ-node-1")
	if nodeInfo == nil {
		t.Fatal("GetNode mengembalikan nil")
	}
	if nodeInfo.Status != registry.StatusDead {
		t.Logf("dead dari TickDeadCheck: %v", dead)
		t.Fatalf("node seharusnya dead, got status=%s", nodeInfo.Status)
	}
	t.Logf("Node berhasil dideteksi mati (status=%s) ✓", nodeInfo.Status)
}

// TestIntegration_ProcessImage memverifikasi end-to-end:
//  1. Node Worker gRPC berjalan.
//  2. Master memanggil ProcessImage RPC ke node.
//  3. Node memproses gambar dan mengembalikan hasil.
func TestIntegration_ProcessImage(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Jalankan node Worker gRPC
	nodePort := freePort(t)
	nodeAddr := fmt.Sprintf("127.0.0.1:%d", nodePort)

	nodeGRPC := rpcpkg.NewGRPCServer(integrationToken)
	cluster.RegisterWorkerServer(nodeGRPC, rpcpkg.NewWorkerServer(log))

	lis, err := net.Listen("tcp", nodeAddr)
	if err != nil {
		t.Fatalf("listen gagal: %v", err)
	}
	go nodeGRPC.Serve(lis) //nolint:errcheck
	t.Cleanup(func() { nodeGRPC.GracefulStop() })

	// Master menghubungkan koneksi ke node
	conn, err := rpcpkg.DialNode(nodeAddr, integrationToken)
	if err != nil {
		t.Fatalf("dial node gagal: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	workerClient := cluster.NewWorkerClient(conn)

	// Buat gambar PNG valid menggunakan stdlib
	testPNG := makeTestPNG(t, 10, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := workerClient.ProcessImage(ctx, &cluster.ProcessRequest{
		TaskId:       "integ-task-001",
		Filename:     "test.png",
		ImageData:    testPNG,
		ResizeWidth:  5,
		ResizeHeight: 5,
		Grayscale:    true,
	}, grpc.MaxCallRecvMsgSize(8*1024*1024))

	if err != nil {
		t.Fatalf("ProcessImage RPC gagal: %v", err)
	}
	if !resp.GetSuccess() {
		t.Fatalf("ProcessImage gagal di node: %s", resp.GetError())
	}
	if len(resp.GetResultData()) == 0 {
		t.Fatal("hasil gambar kosong")
	}

	t.Logf("ProcessImage OK — task=%s, durasi=%dms, hasil=%d byte ✓",
		resp.GetTaskId(), resp.GetDurationMs(), len(resp.GetResultData()))
}

// TestIntegration_InvalidToken memverifikasi bahwa node dengan token salah ditolak.
func TestIntegration_InvalidToken(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Master dengan token valid
	masterPort := freePort(t)
	masterAddr := fmt.Sprintf("127.0.0.1:%d", masterPort)

	reg := registry.New(6*time.Second, log)
	grpcSrv := rpcpkg.NewGRPCServer(integrationToken)
	cluster.RegisterCoordinatorServer(grpcSrv, rpcpkg.NewCoordinatorServer(reg, log))

	lis, err := net.Listen("tcp", masterAddr)
	if err != nil {
		t.Fatalf("listen gagal: %v", err)
	}
	go grpcSrv.Serve(lis) //nolint:errcheck
	t.Cleanup(func() { grpcSrv.GracefulStop() })

	// Dial dengan token salah
	conn, err := rpcpkg.DialNode(masterAddr, "token-salah")
	if err != nil {
		t.Fatalf("dial gagal: %v", err)
	}
	defer conn.Close()

	coordClient := cluster.NewCoordinatorClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = coordClient.Register(ctx, &cluster.RegisterRequest{
		NodeId:        "penyusup",
		SessionId:     "x",
		AdvertiseAddr: "127.0.0.1:9999",
		Capacity:      1,
	})
	if err == nil {
		t.Fatal("Register dengan token salah seharusnya error, tapi tidak")
	}
	t.Logf("Token salah ditolak dengan benar: %v ✓", err)
}

// TestIntegration_AutoAssignNodeID menguji bahwa registrasi dengan ID "auto" atau kosong
// secara otomatis diberikan ID berurutan oleh CoordinatorServer.
func TestIntegration_AutoAssignNodeID(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	masterPort := freePort(t)
	masterAddr := fmt.Sprintf("127.0.0.1:%d", masterPort)

	reg := registry.New(3*time.Second, log)
	grpcSrv := rpcpkg.NewGRPCServer(integrationToken)
	coordSrv := rpcpkg.NewCoordinatorServer(reg, log)
	cluster.RegisterCoordinatorServer(grpcSrv, coordSrv)

	lis, err := net.Listen("tcp", masterAddr)
	if err != nil {
		t.Fatalf("listen gagal: %v", err)
	}
	go grpcSrv.Serve(lis) //nolint:errcheck
	t.Cleanup(func() { grpcSrv.GracefulStop() })

	conn, err := rpcpkg.DialNode(masterAddr, integrationToken)
	if err != nil {
		t.Fatalf("dial gagal: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	client := cluster.NewCoordinatorClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Worker 1 mendaftar dengan NodeId: "auto"
	resp1, err := client.Register(ctx, &cluster.RegisterRequest{
		NodeId:        "auto",
		SessionId:     "s1",
		AdvertiseAddr: "127.0.0.1:9001",
		Capacity:      2,
	})
	if err != nil || !resp1.GetAccepted() {
		t.Fatalf("register 1 gagal: %v, msg=%s", err, resp1.GetMessage())
	}
	if !strings.HasPrefix(resp1.GetMessage(), "ASSIGNED:node-1") {
		t.Fatalf("mau ASSIGNED:node-1, dapat: %s", resp1.GetMessage())
	}

	// Worker 2 mendaftar dengan NodeId: "" (kosong)
	resp2, err := client.Register(ctx, &cluster.RegisterRequest{
		NodeId:        "",
		SessionId:     "s2",
		AdvertiseAddr: "127.0.0.1:9002",
		Capacity:      2,
	})
	if err != nil || !resp2.GetAccepted() {
		t.Fatalf("register 2 gagal: %v, msg=%s", err, resp2.GetMessage())
	}
	if !strings.HasPrefix(resp2.GetMessage(), "ASSIGNED:node-2") {
		t.Fatalf("mau ASSIGNED:node-2, dapat: %s", resp2.GetMessage())
	}
}

// freePort mendapatkan port TCP bebas di localhost untuk digunakan tes.
func freePort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tidak bisa mendapat port bebas: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	lis.Close()
	return port
}

// makeTestPNG menghasilkan berkas citra PNG valid untuk pengujian.
func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("makeTestPNG gagal: %v", err)
	}
	return buf.Bytes()
}

