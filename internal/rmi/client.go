package rmi

import (
	"fmt"
	"net"
	"net/rpc"
	"time"
)

// Call mengeksekusi pemanggilan remote method invocation (RMI) ke node target dengan batas waktu timeout.
func Call(addr, serviceMethod string, args any, reply any, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("rmi: gagal konek ke %s: %w", addr, err)
	}
	defer conn.Close()

	client := rpc.NewClient(conn)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- client.Call(serviceMethod, args, reply)
	}()

	select {
	case <-time.After(timeout):
		return fmt.Errorf("rmi: timeout memanggil %s di %s (%v)", serviceMethod, addr, timeout)
	case err := <-done:
		if err != nil {
			return fmt.Errorf("rmi: call %s gagal: %w", serviceMethod, err)
		}
		return nil
	}
}

// Ping memverifikasi status hidup remote node melalui RMI.
func Ping(addr, senderID string, timeout time.Duration) (*PingReply, error) {
	var reply PingReply
	err := Call(addr, "Coordinator.Ping", PingArgs{SenderID: senderID}, &reply, timeout)
	if err != nil {
		// Coba fallback ke ImageProcessor.Ping
		err = Call(addr, "ImageProcessor.Ping", PingArgs{SenderID: senderID}, &reply, timeout)
	}
	if err != nil {
		return nil, err
	}
	return &reply, nil
}

// SendElection mengirimkan pesan pemilihan (Bully Algorithm) ke node berprioritas lebih tinggi.
func SendElection(addr string, args ElectionArgs, timeout time.Duration) (*ElectionReply, error) {
	var reply ElectionReply
	err := Call(addr, "Coordinator.Elect", args, &reply, timeout)
	if err != nil {
		return nil, err
	}
	return &reply, nil
}

// SendVictory menyiarkan pengumuman koordinator terpilih ke peer node.
func SendVictory(addr string, args VictoryArgs, timeout time.Duration) (*VictoryReply, error) {
	var reply VictoryReply
	err := Call(addr, "Coordinator.AnnounceCoordinator", args, &reply, timeout)
	if err != nil {
		return nil, err
	}
	return &reply, nil
}

// SendHeartbeat mengirimkan sinyal detak jantung dan meminta topologi peer via RMI.
func SendHeartbeat(addr string, args HeartbeatArgs, timeout time.Duration) (*HeartbeatReply, error) {
	var reply HeartbeatReply
	err := Call(addr, "Coordinator.Heartbeat", args, &reply, timeout)
	if err != nil {
		return nil, err
	}
	return &reply, nil
}

// InvokeProcessImage mengeksekusi transformasi citra pada remote worker via RMI.
func InvokeProcessImage(addr string, args ProcessImageArgs, timeout time.Duration) (*ProcessImageReply, error) {
	var reply ProcessImageReply
	err := Call(addr, "ImageProcessor.TransformImage", args, &reply, timeout)
	if err != nil {
		return nil, err
	}
	return &reply, nil
}
