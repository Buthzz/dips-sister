// Package scheduler — tes unit untuk logika scatter-gather dan manajemen job.
//
// Menggunakan mockNodeClient untuk mengisolasi scheduler dari jaringan,
// memungkinkan tes berjalan cepat dan deterministik tanpa koneksi gRPC.
package scheduler

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"log/slog"
	"os"
	"testing"
	"time"

	"distapi/internal/registry"
	"distapi/internal/storage"
)

// mockNodeClient adalah implementasi NodeClient tiruan untuk tes.
// Bisa dikonfigurasi untuk berhasil (mengembalikan data) atau gagal (mengembalikan error).
type mockNodeClient struct {
	hasilData []byte // data yang dikembalikan jika berhasil
	err       error  // error yang dikembalikan jika gagal
	panggilanKe int  // hitungan berapa kali ProcessImage dipanggil
}

func (m *mockNodeClient) ProcessImage(_ context.Context, _ string, _ *Task, _ []byte, _ ProcessOptions) ([]byte, error) {
	m.panggilanKe++
	return m.hasilData, m.err
}

// buatGambarJPEG membuat data JPEG minimal untuk dipakai dalam tes.
func buatGambarJPEG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	return buf.Bytes()
}

// buatSetupTes membuat semua komponen yang diperlukan scheduler untuk tes.
// Mendaftarkan cleanup yang menunggu goroutine selesai sebelum TempDir dihapus.
func buatSetupTes(t *testing.T, client NodeClient) *Scheduler {
	t.Helper()
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatalf("buat storage gagal: %v", err)
	}
	reg := registry.New(5*time.Second, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	sched := New(reg, store, client, 3, 30*time.Second, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Tunggu semua goroutine selesai sebelum t.TempDir dihapus (penting di Windows).
	t.Cleanup(func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			semuaSelesai := true
			sched.mu.RLock()
			for _, j := range sched.jobs {
				if j.Status == JobPending || j.Status == JobProcessing {
					semuaSelesai = false
					break
				}
			}
			sched.mu.RUnlock()
			if semuaSelesai {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	return sched
}

// TestSubmitBuatJob memastikan Submit menghasilkan job dengan jumlah task yang benar.
func TestSubmitBuatJob(t *testing.T) {
	client := &mockNodeClient{hasilData: buatGambarJPEG()}
	sched := buatSetupTes(t, client)

	gambar := buatGambarJPEG()
	job, err := sched.Submit(context.Background(),
		[]string{"a.jpg", "b.jpg"},
		[][]byte{gambar, gambar},
		ProcessOptions{Grayscale: true})

	if err != nil {
		t.Fatalf("Submit gagal: %v", err)
	}
	if job.ID == "" {
		t.Error("job ID tidak boleh kosong")
	}
	if len(job.Tasks) != 2 {
		t.Errorf("mau 2 task, dapat %d", len(job.Tasks))
	}
}

// TestSubmitTanpaFile memastikan Submit mengembalikan error jika tidak ada file.
func TestSubmitTanpaFile(t *testing.T) {
	client := &mockNodeClient{}
	sched := buatSetupTes(t, client)

	_, err := sched.Submit(context.Background(), nil, nil, ProcessOptions{})
	if err == nil {
		t.Fatal("Submit tanpa file seharusnya error")
	}
}

// TestGetJobDitemukanDanTidak memverifikasi GetJob untuk job yang ada dan tidak ada.
func TestGetJobDitemukanDanTidak(t *testing.T) {
	client := &mockNodeClient{hasilData: buatGambarJPEG()}
	sched := buatSetupTes(t, client)

	gambar := buatGambarJPEG()
	job, _ := sched.Submit(context.Background(), []string{"c.jpg"}, [][]byte{gambar}, ProcessOptions{})

	// Job yang baru dibuat harus ditemukan.
	ditemukan := sched.GetJob(job.ID)
	if ditemukan == nil {
		t.Fatal("GetJob harus menemukan job yang baru dibuat")
	}

	// Job dengan ID acak tidak boleh ditemukan.
	if sched.GetJob("id-tidak-ada") != nil {
		t.Error("GetJob ID tidak ada seharusnya nil")
	}
}

// TestListJobs memastikan ListJobs mengembalikan semua job yang pernah dibuat.
func TestListJobs(t *testing.T) {
	client := &mockNodeClient{hasilData: buatGambarJPEG()}
	sched := buatSetupTes(t, client)

	gambar := buatGambarJPEG()
	sched.Submit(context.Background(), []string{"x.jpg"}, [][]byte{gambar}, ProcessOptions{})
	sched.Submit(context.Background(), []string{"y.jpg"}, [][]byte{gambar}, ProcessOptions{})

	jobs := sched.ListJobs()
	if len(jobs) != 2 {
		t.Errorf("mau 2 job, dapat %d", len(jobs))
	}
}

// TestCancelJob memastikan job terhapus dari daftar setelah dibatalkan.
// Menunggu job selesai dulu agar goroutine dispatch tidak conflict dengan delete.
func TestCancelJob(t *testing.T) {
	client := &mockNodeClient{hasilData: buatGambarJPEG()}
	sched := buatSetupTes(t, client)

	gambar := buatGambarJPEG()
	job, _ := sched.Submit(context.Background(), []string{"d.jpg"}, [][]byte{gambar}, ProcessOptions{})

	// Tunggu hingga job selesai (goroutine dispatch masih menulis ke disk).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := sched.GetJob(job.ID)
		if snapshot == nil || snapshot.Status == JobDone || snapshot.Status == JobFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := sched.CancelJob(job.ID); err != nil {
		t.Fatalf("CancelJob gagal: %v", err)
	}

	if sched.GetJob(job.ID) != nil {
		t.Error("job harus nil setelah dibatalkan")
	}
}

// TestCancelJobTidakAda memastikan CancelJob mengembalikan error untuk ID yang tidak ada.
func TestCancelJobTidakAda(t *testing.T) {
	client := &mockNodeClient{}
	sched := buatSetupTes(t, client)

	err := sched.CancelJob("job-tidak-ada")
	if err == nil {
		t.Fatal("CancelJob ID tidak ada seharusnya error")
	}
}

// TestJobSelesaiDenganBenar memverifikasi bahwa job berakhir DONE setelah semua task selesai.
func TestJobSelesaiDenganBenar(t *testing.T) {
	client := &mockNodeClient{hasilData: buatGambarJPEG()}
	sched := buatSetupTes(t, client)

	// Daftarkan node agar round-robin bisa memilih node (bukan master-local).
	sched.reg.Register("node-1", "sesi", "localhost:9001", 4)

	gambar := buatGambarJPEG()
	job, err := sched.Submit(context.Background(),
		[]string{"e.jpg", "f.jpg"},
		[][]byte{gambar, gambar},
		ProcessOptions{Grayscale: true})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Tunggu hingga job selesai (timeout 5 detik untuk CI).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := sched.GetJob(job.ID)
		if snapshot != nil && (snapshot.Status == JobDone || snapshot.Status == JobFailed) {
			if snapshot.Status != JobDone {
				t.Errorf("status job: mau DONE, dapat %s", snapshot.Status)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("job tidak selesai dalam 5 detik")
}

// TestRescheduleDeadNode memastikan task yang sedang berjalan di node mati
// dikembalikan ke status PENDING sehingga bisa dijadwalkan ulang.
func TestRescheduleDeadNode(t *testing.T) {
	client := &mockNodeClient{hasilData: buatGambarJPEG()}
	sched := buatSetupTes(t, client)

	// Buat job dan paksa satu task ke status RUNNING di node mati.
	gambar := buatGambarJPEG()
	job, _ := sched.Submit(context.Background(), []string{"g.jpg"}, [][]byte{gambar}, ProcessOptions{})

	// Manipulasi langsung untuk simulasi (dalam tes ini kita bypass dispatch).
	sched.mu.Lock()
	j := sched.jobs[job.ID]
	if j != nil && len(j.Tasks) > 0 {
		j.Tasks[0].Status = TaskRunning
		j.Tasks[0].AssignedTo = "node-mati"
	}
	sched.mu.Unlock()

	// Panggil reschedule.
	sched.RescheduleDeadNodeTasks([]string{"node-mati"})

	// Task harus kembali ke PENDING.
	sched.mu.RLock()
	j = sched.jobs[job.ID]
	var statusTask TaskStatus
	if j != nil && len(j.Tasks) > 0 {
		statusTask = j.Tasks[0].Status
	}
	sched.mu.RUnlock()

	if statusTask != TaskPending {
		t.Errorf("task seharusnya PENDING setelah reschedule, dapat %s", statusTask)
	}
}

// TestBuatIDUnik memastikan setiap panggilan buatID menghasilkan ID yang berbeda.
func TestBuatIDUnik(t *testing.T) {
	ids := make(map[string]bool)
	for range 100 {
		id := buatID("test")
		if ids[id] {
			t.Fatalf("ID duplikat: %s", id)
		}
		ids[id] = true
	}
}
