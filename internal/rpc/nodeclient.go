package rpc

import (
	"context"
	"fmt"
	"sync"

	"distapi/gen/cluster"
	"distapi/internal/scheduler"

	"google.golang.org/grpc"
)

// NodeClientAdapter mengadaptasi antarmuka scheduler.NodeClient ke pemanggilan gRPC.
type NodeClientAdapter struct {
	token string
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

// NewNodeClientAdapter membuat adapter client baru.
func NewNodeClientAdapter(token string) *NodeClientAdapter {
	return &NodeClientAdapter{
		token: token,
		conns: make(map[string]*grpc.ClientConn),
	}
}

// ProcessImage mengirimkan tugas pemrosesan ke node via gRPC atau fallback lokal.
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
		return nil, fmt.Errorf("rpc: worker %s gagal: %s", nodeID, resp.GetError())
	}
	return resp.GetResultData(), nil
}

// SetConn mendaftarkan atau mengganti koneksi client untuk node tertentu.
func (a *NodeClientAdapter) SetConn(nodeID string, conn *grpc.ClientConn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if old, ok := a.conns[nodeID]; ok {
		_ = old.Close()
	}
	a.conns[nodeID] = conn
}

// Close menutup seluruh koneksi gRPC yang tersimpan.
func (a *NodeClientAdapter) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.conns {
		_ = c.Close()
	}
	a.conns = make(map[string]*grpc.ClientConn)
}

func (a *NodeClientAdapter) ambilKoneksi(nodeID string) (*grpc.ClientConn, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	conn, ok := a.conns[nodeID]
	if !ok {
		return nil, fmt.Errorf("rpc: belum ada koneksi ke %s", nodeID)
	}
	return conn, nil
}

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
