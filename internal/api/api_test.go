// Package api — tes integrasi untuk semua endpoint REST.
//
// Menggunakan httptest.NewRecorder dan httptest.NewRequest untuk menguji
// handler tanpa perlu server HTTP nyata. Pola ini memungkinkan tes
// jalankan dalam hitungan milidetik tanpa binding port.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"distapi/internal/registry"
	"distapi/internal/scheduler"
	"distapi/internal/storage"
)

// mockClient implementasi NodeClient tiruan untuk tes — selalu berhasil.
type mockClient struct {
	hasil []byte
}

func (m *mockClient) ProcessImage(_ context.Context, _ string, _ *scheduler.Task, _ []byte, _ scheduler.ProcessOptions) ([]byte, error) {
	return m.hasil, nil
}

// buatJPEGData menghasilkan byte JPEG minimal untuk tes.
func buatJPEGData() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	return buf.Bytes()
}

// buatHandler membuat Handler lengkap dengan semua dependensi untuk tes.
// Mendaftarkan cleanup yang menunggu goroutine background selesai
// sebelum t.TempDir dihapus (diperlukan di Windows).
func buatHandler(t *testing.T) *Handler {
	t.Helper()
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatalf("buat storage: %v", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	reg := registry.New(5*time.Second, log)
	client := &mockClient{hasil: buatJPEGData()}
	sched := scheduler.New(reg, store, client, 3, 30*time.Second, log)
	h := New(sched, reg, store, 5, 20, log)

	t.Cleanup(func() {
		// Beri waktu hingga 3 detik agar goroutine dispatch background selesai.
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			semuaSelesai := true
			for _, j := range sched.ListJobs() {
				if j.Status == scheduler.JobPending || j.Status == scheduler.JobProcessing {
					semuaSelesai = false
					break
				}
			}
			if semuaSelesai {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	return h
}

// buatMultipartBody membuat body multipart dengan file JPEG untuk tes POST /jobs.
func buatMultipartBody(t *testing.T, namaFile string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("images", namaFile)
	if err != nil {
		t.Fatalf("buat form file: %v", err)
	}
	fw.Write(data)
	w.Close()
	return &body, w.FormDataContentType()
}

// TestHealth memastikan /healthz selalu mengembalikan 200.
func TestHealth(t *testing.T) {
	h := buatHandler(t)

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("mau 200, dapat %d", rec.Code)
	}
}

// TestBuatJobBerhasil memastikan POST /api/v1/jobs mengembalikan 202 dengan job_id.
func TestBuatJobBerhasil(t *testing.T) {
	h := buatHandler(t)

	body, ct := buatMultipartBody(t, "foto.jpg", buatJPEGData())
	req := httptest.NewRequest("POST", "/api/v1/jobs", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("mau 202, dapat %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["job_id"] == "" {
		t.Error("response harus mengandung job_id")
	}
}

// TestBuatJobTanpaFile memastikan POST tanpa file menghasilkan 400.
func TestBuatJobTanpaFile(t *testing.T) {
	h := buatHandler(t)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.Close() // body multipart kosong

	req := httptest.NewRequest("POST", "/api/v1/jobs", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("mau 400, dapat %d", rec.Code)
	}
}

// TestBuatJobFormatTidakDidukung memastikan GIF menghasilkan 415.
func TestBuatJobFormatTidakDidukung(t *testing.T) {
	h := buatHandler(t)

	// Kirim file dengan ekstensi .gif
	body, ct := buatMultipartBody(t, "animasi.gif", []byte("data gif"))
	req := httptest.NewRequest("POST", "/api/v1/jobs", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("mau 415, dapat %d", rec.Code)
	}
}

// TestGetJobDitemukan memastikan GET /api/v1/jobs/{id} mengembalikan detail job.
func TestGetJobDitemukan(t *testing.T) {
	h := buatHandler(t)

	// Buat job dulu.
	body, ct := buatMultipartBody(t, "foto.jpg", buatJPEGData())
	req := httptest.NewRequest("POST", "/api/v1/jobs", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var buatResp map[string]any
	json.NewDecoder(rec.Body).Decode(&buatResp)
	jobID := fmt.Sprintf("%v", buatResp["job_id"])

	// Ambil detail job.
	req2 := httptest.NewRequest("GET", "/api/v1/jobs/"+jobID, nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("mau 200, dapat %d — body: %s", rec2.Code, rec2.Body.String())
	}
	var detail map[string]any
	json.NewDecoder(rec2.Body).Decode(&detail)
	if detail["id"] != jobID {
		t.Errorf("id job tidak cocok: mau %s, dapat %v", jobID, detail["id"])
	}
}

// TestGetJobTidakDitemukan memastikan GET job dengan ID salah menghasilkan 404.
func TestGetJobTidakDitemukan(t *testing.T) {
	h := buatHandler(t)

	req := httptest.NewRequest("GET", "/api/v1/jobs/id-tidak-ada", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("mau 404, dapat %d", rec.Code)
	}
}

// TestHapusJob memastikan DELETE mengembalikan 204 dan job hilang.
// Test ini menunggu job selesai dulu sebelum menghapus untuk menghindari
// race condition antara goroutine dispatch dan os.RemoveAll di Windows.
func TestHapusJob(t *testing.T) {
	h := buatHandler(t)

	body, ct := buatMultipartBody(t, "foto.jpg", buatJPEGData())
	req := httptest.NewRequest("POST", "/api/v1/jobs", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var buatResp map[string]any
	json.NewDecoder(rec.Body).Decode(&buatResp)
	jobID := fmt.Sprintf("%v", buatResp["job_id"])

	// Tunggu hingga job selesai sebelum menghapus.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest("GET", "/api/v1/jobs/"+jobID, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var detail map[string]any
		json.NewDecoder(rec.Body).Decode(&detail)
		st := fmt.Sprintf("%v", detail["status"])
		if st == "DONE" || st == "FAILED" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Hapus job.
	req2 := httptest.NewRequest("DELETE", "/api/v1/jobs/"+jobID, nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNoContent {
		t.Errorf("mau 204, dapat %d", rec2.Code)
	}

	// Pastikan job sudah hilang.
	req3 := httptest.NewRequest("GET", "/api/v1/jobs/"+jobID, nil)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNotFound {
		t.Errorf("setelah hapus, mau 404, dapat %d", rec3.Code)
	}
}

// TestHapusJobTidakAda memastikan DELETE job yang tidak ada menghasilkan 404.
func TestHapusJobTidakAda(t *testing.T) {
	h := buatHandler(t)

	req := httptest.NewRequest("DELETE", "/api/v1/jobs/tidak-ada", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("mau 404, dapat %d", rec.Code)
	}
}

// TestListJobKosong memastikan GET /api/v1/jobs mengembalikan array kosong (bukan null).
func TestListJobKosong(t *testing.T) {
	h := buatHandler(t)

	req := httptest.NewRequest("GET", "/api/v1/jobs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("mau 200, dapat %d", rec.Code)
	}

	// Response harus array JSON valid.
	var resp []any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Errorf("response bukan JSON array valid: %v", err)
	}
}

// TestListNode memastikan GET /api/v1/nodes mengembalikan daftar node.
func TestListNode(t *testing.T) {
	h := buatHandler(t)

	// Daftarkan satu node ke registry.
	h.reg.Register("node-1", "sesi", "192.168.1.2:9000", 4)

	req := httptest.NewRequest("GET", "/api/v1/nodes", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("mau 200, dapat %d", rec.Code)
	}

	var nodes []map[string]any
	json.NewDecoder(rec.Body).Decode(&nodes)
	if len(nodes) != 1 {
		t.Errorf("mau 1 node, dapat %d", len(nodes))
	}
	if nodes[0]["node_id"] != "node-1" {
		t.Errorf("node_id salah: %v", nodes[0]["node_id"])
	}
}

// TestContentTypeJSON memastikan semua endpoint mengembalikan Content-Type JSON.
func TestContentTypeJSON(t *testing.T) {
	h := buatHandler(t)

	endpoint := []struct {
		method string
		path   string
	}{
		{"GET", "/healthz"},
		{"GET", "/api/v1/jobs"},
		{"GET", "/api/v1/nodes"},
		{"GET", "/api/v1/jobs/tidak-ada"},
	}

	for _, ep := range endpoint {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			ct := rec.Header().Get("Content-Type")
			if !bytes.Contains([]byte(ct), []byte("application/json")) {
				t.Errorf("Content-Type bukan JSON: %s", ct)
			}
		})
	}
}
