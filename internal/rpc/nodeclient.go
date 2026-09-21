// nodeclient.go menjembatani antarmuka scheduler.NodeClient dengan panggilan gRPC nyata.
//
// Berada di package rpc (bukan scheduler) agar bisa mengimpor gen/cluster
// tanpa menciptakan dependency cycle (scheduler → rpc → scheduler).
//
// Pola fallback "master-local":
//   - Jika semua node mati, scheduler memilih "master-local" sebagai target
//   - NodeClientAdapter menangani kasus ini dengan memanggil WorkerServer secara in-process
//   - Ini implementasi graceful degradation sesuai DESIGN.md §7.5
package rpc

import (
	"context"
	"fmt"
	"sync"

	"distapi/gen/cluster"
	"distapi/internal/scheduler"

	"google.golang.org/grpc"
)

// NodeClientAdapter mengimplementasikan scheduler.NodeClient menggunakan gRPC nyata.
// Koneksi di-cache berdasarkan nodeID untuk menghindari overhead handshake berulang.
//
// Manajemen koneksi:
//   - SetConn dipanggil saat node berhasil Register (dari registrationHook di main)
//   - UpdateAddr dipanggil jika alamat node berubah (misalnya setelah restart)
//   - Close menutup semua koneksi saat binary berhenti (graceful shutdown)
type NodeClientAdapter struct {
	token string

	mu    sync.Mutex
	conns map[string]*grpc.ClientConn // kunci: nodeID
}

// NewNodeClientAdapter membuat adapter baru dengan token autentikasi.
func NewNodeClientAdapter(token string) *NodeClientAdapter {
	return &NodeClientAdapter{
		token: token,
		conns: make(map[string]*grpc.ClientConn),
	}
}

// ProcessImage mengirimkan request ProcessImage ke node yang ditentukan.
// Jika nodeID adalah "master-local", pemrosesan dilakukan in-process tanpa jaringan.
func (a *NodeClientAdapter) ProcessImage(
	ctx context.Context,
	nodeID string,
	task *scheduler.Task,
	imageData []byte,
	opts scheduler.ProcessOptions,
) ([]byte, error) {
	if nodeID == "master-local" {
		return a.prosesLokal(task, imageData, opts)
	}

	conn, err := a.ambilKoneksi(nodeID)
	if err != nil {
		return nil, err
	}

	resp, err := cluster.NewWorkerClient(conn).ProcessImage(ctx, &cluster.ProcessRequest{
		TaskId:       task.ID,
		JobId:        task.JobID,
		ImageIndex:   int32(task.Index),
		ImageData:    imageData,
		Filename:     task.Filename,
		ResizeWidth:  int32(opts.ResizeWidth),
		ResizeHeight: int32(opts.ResizeHeight),
		Grayscale:    opts.Grayscale,
	})
	if err != nil {
		return nil, fmt.Errorf("rpc: ProcessImage ke %s gagal: %w", nodeID, err)
	}
	if !resp.GetSuccess() {
		// Worker mengembalikan error proses (bukan error jaringan) — scheduler akan retry.
		return nil, fmt.Errorf("rpc: worker %s gagal: %s", nodeID, resp.GetError())
	}
	return resp.GetResultData(), nil
}

// SetConn mendaftarkan atau mengganti koneksi untuk node tertentu.
// Koneksi lama ditutup sebelum diganti untuk mencegah kebocoran resource.
func (a *NodeClientAdapter) SetConn(nodeID string, conn *grpc.ClientConn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if old, ok := a.conns[nodeID]; ok {
		_ = old.Close()
	}
	a.conns[nodeID] = conn
}

// Close menutup semua koneksi yang di-cache. Dipanggil saat shutdown.
func (a *NodeClientAdapter) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.conns {
		_ = c.Close()
	}
	a.conns = make(map[string]*grpc.ClientConn)
}

// ambilKoneksi mengembalikan koneksi yang sudah di-cache untuk nodeID.
// Koneksi harus sudah ada sebelumnya (didaftarkan via SetConn saat Register).
// Jika belum ada, kemungkinan node belum selesai Register — scheduler akan retry.
func (a *NodeClientAdapter) ambilKoneksi(nodeID string) (*grpc.ClientConn, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	conn, ok := a.conns[nodeID]
	if !ok {
		return nil, fmt.Errorf("rpc: belum ada koneksi ke %s (node mungkin belum register)", nodeID)
	}
	return conn, nil
}

// prosesLokal menjalankan pemrosesan gambar secara in-process menggunakan WorkerServer.
// Digunakan sebagai fallback saat semua node mati — master bekerja sendiri.
// Ini memastikan sistem tetap berfungsi meski dalam kondisi degradasi.
func (a *NodeClientAdapter) prosesLokal(
	task *scheduler.Task,
	imageData []byte,
	opts scheduler.ProcessOptions,
) ([]byte, error) {
	ws := NewWorkerServer(nil)
	resp, err := ws.ProcessImage(context.Background(), &cluster.ProcessRequest{
		TaskId:       task.ID,
		JobId:        task.JobID,
		ImageIndex:   int32(task.Index),
		ImageData:    imageData,
		Filename:     task.Filename,
		ResizeWidth:  int32(opts.ResizeWidth),
		ResizeHeight: int32(opts.ResizeHeight),
		Grayscale:    opts.Grayscale,
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetSuccess() {
		return nil, fmt.Errorf("worker lokal gagal: %s", resp.GetError())
	}
	return resp.GetResultData(), nil
}
