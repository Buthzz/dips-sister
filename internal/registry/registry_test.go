package registry

import (
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// buat registry untuk tes dengan timeout yang bisa dikonfigurasi.
func buatRegistry(timeout time.Duration) *Registry {
	return New(timeout, slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

// TestRegisterDanResolve memastikan node yang baru daftar bisa di-resolve.
func TestRegisterDanResolve(t *testing.T) {
	r := buatRegistry(5 * time.Second)

	ok := r.Register("node-1", "sesi-abc", "192.168.1.11:9000", 4)
	if !ok {
		t.Fatal("Register harus mengembalikan true")
	}

	addr, ditemukan := r.Resolve("node-1")
	if !ditemukan {
		t.Fatal("node-1 seharusnya bisa di-resolve")
	}
	if addr != "192.168.1.11:9000" {
		t.Errorf("alamat salah: mau 192.168.1.11:9000, dapat %q", addr)
	}
}

// TestHeartbeatNodeTidakDikenal memastikan heartbeat dari node asing ditolak.
func TestHeartbeatNodeTidakDikenal(t *testing.T) {
	r := buatRegistry(5 * time.Second)
	if r.Heartbeat("node-asing", "sesi", 0) {
		t.Fatal("heartbeat dari node tidak dikenal harus ditolak")
	}
}

// TestHeartbeatSesiKadaluarsa memastikan heartbeat dengan sesi lama ditolak.
func TestHeartbeatSesiKadaluarsa(t *testing.T) {
	r := buatRegistry(5 * time.Second)
	r.Register("node-1", "sesi-baru", "addr:9000", 4)

	// Sesi lama harus ditolak meski node-id cocok.
	if r.Heartbeat("node-1", "sesi-lama", 0) {
		t.Fatal("heartbeat dengan sesi lama harus ditolak")
	}
}

// TestDeadCheck memastikan node ditandai mati setelah melewati batas timeout.
func TestDeadCheck(t *testing.T) {
	const timeout = 100 * time.Millisecond
	r := buatRegistry(timeout)
	r.Register("node-1", "sesi-1", "addr:9000", 4)

	time.Sleep(timeout + 50*time.Millisecond)

	dead := r.TickDeadCheck()
	if len(dead) != 1 || dead[0] != "node-1" {
		t.Errorf("mau [node-1] mati, dapat %v", dead)
	}

	// Node mati seharusnya tidak bisa di-resolve.
	_, ditemukan := r.Resolve("node-1")
	if ditemukan {
		t.Fatal("node mati seharusnya tidak bisa di-resolve")
	}
}

// TestSesiBaruMenimpaLama memastikan node restart (sesi baru pada host yang sama) menggantikan entry lama.
func TestSesiBaruMenimpaLama(t *testing.T) {
	r := buatRegistry(5 * time.Second)
	r.Register("node-1", "sesi-lama", "alamat:9000", 4)
	r.Register("node-1", "sesi-baru", "alamat:9000", 8)

	info := r.GetNode("node-1")
	if info == nil || info.SessionID != "sesi-baru" || info.Capacity != 8 {
		t.Errorf("sesi baru harus menimpa metadata lama: dapat %+v", info)
	}
}

// TestAliveNodes memastikan AliveNodes hanya mengembalikan node yang hidup.
func TestAliveNodes(t *testing.T) {
	const timeout = 100 * time.Millisecond
	r := buatRegistry(timeout)
	r.Register("node-1", "s1", "a1:9000", 4)
	r.Register("node-2", "s2", "a2:9000", 4)

	// Tunggu keduanya mati.
	time.Sleep(timeout + 50*time.Millisecond)
	r.TickDeadCheck()

	// Daftarkan ulang node-1 dengan sesi baru.
	r.Register("node-1", "s1-baru", "a1:9000", 4)

	alive := r.AliveNodes()
	if len(alive) != 1 || alive[0].NodeID != "node-1" {
		t.Errorf("mau [node-1] hidup, dapat %v", alive)
	}
}

// TestAllNodes memastikan AllNodes mengembalikan semua node termasuk yang mati.
func TestAllNodes(t *testing.T) {
	const timeout = 100 * time.Millisecond
	r := buatRegistry(timeout)
	r.Register("node-1", "s1", "a1:9000", 4)
	r.Register("node-2", "s2", "a2:9000", 4)

	time.Sleep(timeout + 50*time.Millisecond)
	r.TickDeadCheck()

	all := r.AllNodes()
	if len(all) != 2 {
		t.Errorf("AllNodes harus mengembalikan 2, dapat %d", len(all))
	}
}

// TestPulihViaHeartbeat memastikan node dead bisa pulih kembali via heartbeat.
func TestPulihViaHeartbeat(t *testing.T) {
	const timeout = 100 * time.Millisecond
	r := buatRegistry(timeout)
	r.Register("node-1", "sesi", "addr:9000", 4)

	// Biarkan mati.
	time.Sleep(timeout + 50*time.Millisecond)
	r.TickDeadCheck()

	_, ditemukan := r.Resolve("node-1")
	if ditemukan {
		t.Fatal("seharusnya mati sebelum heartbeat")
	}

	// Daftar ulang (sama seperti node kirim register lagi setelah koneksi terputus).
	r.Register("node-1", "sesi", "addr:9000", 4)

	_, ditemukan = r.Resolve("node-1")
	if !ditemukan {
		t.Fatal("seharusnya hidup setelah mendaftar ulang")
	}
}

// TestCapacityDilaporkan memastikan capacity node tersimpan dengan benar.
func TestCapacityDilaporkan(t *testing.T) {
	r := buatRegistry(5 * time.Second)
	r.Register("node-1", "sesi", "addr:9000", 8)

	all := r.AllNodes()
	if len(all) == 0 {
		t.Fatal("tidak ada node")
	}
	if all[0].Capacity != 8 {
		t.Errorf("capacity salah: mau 8, dapat %d", all[0].Capacity)
	}
}

// TestKonflikNodeIDSesiBeda memastikan dua laptop dengan IP berbeda tidak bisa merebut node-id yang sedang aktif.
func TestKonflikNodeIDSesiBeda(t *testing.T) {
	r := buatRegistry(5 * time.Second)

	// Laptop B mendaftar sebagai node-1
	ok1, _, _ := r.RegisterNode("node-1", "sesi-b", "192.168.1.11:9000", 4)
	if !ok1 {
		t.Fatal("registrasi pertama harus berhasil")
	}

	// Laptop C mencoba mendaftar sebagai node-1 saat laptop B masih hidup
	ok2, _, msg := r.RegisterNode("node-1", "sesi-c", "192.168.1.12:9000", 4)
	if ok2 {
		t.Fatal("registrasi kedua dengan IP berbeda saat node masih hidup seharusnya ditolak")
	}
	if !strings.Contains(msg, "sedang aktif digunakan") {
		t.Errorf("pesan error penolakan tidak sesuai: %s", msg)
	}

	// Pastikan node-1 di registry tetap milik Laptop B
	addr, _ := r.Resolve("node-1")
	if addr != "192.168.1.11:9000" {
		t.Errorf("pemilik node-1 seharusnya tetap laptop B, dapat %s", addr)
	}
}

// TestRestartNodeSamaDiterima memastikan laptop yang sama dapat langsung restart tanpa terblokir.
func TestRestartNodeSamaDiterima(t *testing.T) {
	r := buatRegistry(5 * time.Second)

	// Node-1 mendaftar
	r.RegisterNode("node-1", "sesi-lama", "192.168.1.11:9000", 4)

	// Node-1 crash dan restart di laptop yang sama (IP sama, sesi baru)
	ok, _, _ := r.RegisterNode("node-1", "sesi-baru", "192.168.1.11:9000", 8)
	if !ok {
		t.Fatal("node restart di laptop yang sama harus langsung diterima")
	}

	info := r.GetNode("node-1")
	if info.SessionID != "sesi-baru" || info.Capacity != 8 {
		t.Errorf("metadata node harus terupdate setelah restart: %+v", info)
	}
}

// TestAutoAssignNodeID memastikan worker tanpa node-id atau dengan "auto" mendapatkan ID berurutan secara otomatis.
func TestAutoAssignNodeID(t *testing.T) {
	r := buatRegistry(5 * time.Second)

	// Worker pertama mendaftar tanpa ID (kosong)
	ok1, id1, _ := r.RegisterNode("", "s1", "192.168.1.11:9000", 4)
	if !ok1 || id1 != "node-1" {
		t.Fatalf("worker 1 harus dapat node-1, dapat: %s (ok=%v)", id1, ok1)
	}

	// Worker kedua mendaftar dengan "auto"
	ok2, id2, _ := r.RegisterNode("auto", "s2", "192.168.1.12:9000", 4)
	if !ok2 || id2 != "node-2" {
		t.Fatalf("worker 2 harus dapat node-2, dapat: %s (ok=%v)", id2, ok2)
	}

	// Worker ketiga mendaftar dengan "AUTO"
	ok3, id3, _ := r.RegisterNode("AUTO", "s3", "192.168.1.13:9000", 4)
	if !ok3 || id3 != "node-3" {
		t.Fatalf("worker 3 harus dapat node-3, dapat: %s (ok=%v)", id3, ok3)
	}

	// Worker pertama restart di host yang sama tanpa tentukan ID -> harus tetap dapat node-1 (sticky)
	ok1Restart, id1Restart, _ := r.RegisterNode("auto", "s1-new", "192.168.1.11:9000", 4)
	if !ok1Restart || id1Restart != "node-1" {
		t.Fatalf("worker 1 restart harus tetap node-1, dapat: %s", id1Restart)
	}
}

