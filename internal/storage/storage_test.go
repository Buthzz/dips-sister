package storage

import (
	"bytes"
	"testing"
)

// newTestStore membuat Store di direktori sementara yang otomatis dibersihkan.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("gagal buat store: %v", err)
	}
	return store
}

// TestInit memastikan Init membersihkan direktori lama dan membuat yang baru.
func TestInit(t *testing.T) {
	s := newTestStore(t)

	// Simpan sesuatu sebelum Init
	_ = s.SaveUpload("job-lama", "a.jpg", []byte("data"))

	if err := s.Init(); err != nil {
		t.Fatalf("Init gagal: %v", err)
	}

	// File lama seharusnya hilang setelah Init.
	_, err := s.ReadUpload("job-lama", "a.jpg")
	if err == nil {
		t.Error("file lama seharusnya sudah terhapus setelah Init")
	}
}

// TestSaveAndReadUpload memastikan upload tersimpan dan bisa dibaca kembali.
func TestSaveAndReadUpload(t *testing.T) {
	s := newTestStore(t)
	data := []byte("konten gambar tiruan")

	if err := s.SaveUpload("job-1", "foto.jpg", data); err != nil {
		t.Fatalf("SaveUpload gagal: %v", err)
	}

	hasil, err := s.ReadUpload("job-1", "foto.jpg")
	if err != nil {
		t.Fatalf("ReadUpload gagal: %v", err)
	}

	if !bytes.Equal(hasil, data) {
		t.Errorf("data berubah: mau %q, dapat %q", data, hasil)
	}
}

// TestSaveAndReadResult memastikan hasil pemrosesan tersimpan dan bisa dibaca.
func TestSaveAndReadResult(t *testing.T) {
	s := newTestStore(t)
	hasil := []byte("hasil gambar terproses")

	if err := s.SaveResult("job-1", "foto.jpg", hasil); err != nil {
		t.Fatalf("SaveResult gagal: %v", err)
	}

	baca, err := s.ReadResult("job-1", "foto.jpg")
	if err != nil {
		t.Fatalf("ReadResult gagal: %v", err)
	}

	if !bytes.Equal(baca, hasil) {
		t.Errorf("data result berubah: mau %d byte, dapat %d byte", len(hasil), len(baca))
	}
}

// TestResultExists memeriksa keberadaan file result dengan benar.
func TestResultExists(t *testing.T) {
	s := newTestStore(t)

	if s.ResultExists("job-1", "foto.jpg") {
		t.Error("ResultExists harus false sebelum file disimpan")
	}

	_ = s.SaveResult("job-1", "foto.jpg", []byte("data"))

	if !s.ResultExists("job-1", "foto.jpg") {
		t.Error("ResultExists harus true setelah file disimpan")
	}
}

// TestDeleteJob memastikan semua file job terhapus setelah DeleteJob.
func TestDeleteJob(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveUpload("job-1", "a.jpg", []byte("a"))
	_ = s.SaveResult("job-1", "a.jpg", []byte("hasil-a"))

	if err := s.DeleteJob("job-1"); err != nil {
		t.Fatalf("DeleteJob gagal: %v", err)
	}

	// Upload dan result seharusnya sudah tidak ada.
	if _, err := s.ReadUpload("job-1", "a.jpg"); err == nil {
		t.Error("upload seharusnya sudah terhapus")
	}
	if _, err := s.ReadResult("job-1", "a.jpg"); err == nil {
		t.Error("result seharusnya sudah terhapus")
	}
}

// TestDeleteJobNotExist memastikan DeleteJob tidak error untuk job yang tidak ada.
func TestDeleteJobNotExist(t *testing.T) {
	s := newTestStore(t)
	// Seharusnya tidak error meski direktori tidak ada (os.RemoveAll toleran).
	if err := s.DeleteJob("job-tidak-ada"); err != nil {
		t.Errorf("DeleteJob job tidak ada seharusnya tidak error: %v", err)
	}
}

// TestOpenResult memverifikasi streaming result file melalui ReadCloser.
func TestOpenResult(t *testing.T) {
	s := newTestStore(t)
	konten := []byte("hasil streaming")
	_ = s.SaveResult("job-1", "foto.jpg", konten)

	rc, size, err := s.OpenResult("job-1", "foto.jpg")
	if err != nil {
		t.Fatalf("OpenResult gagal: %v", err)
	}
	defer rc.Close()

	if size != int64(len(konten)) {
		t.Errorf("ukuran salah: mau %d, dapat %d", len(konten), size)
	}

	buf := make([]byte, size)
	n, _ := rc.Read(buf)
	if !bytes.Equal(buf[:n], konten) {
		t.Errorf("konten stream berubah")
	}
}

// TestOpenResultNotExist memastikan OpenResult error untuk file yang tidak ada.
func TestOpenResultNotExist(t *testing.T) {
	s := newTestStore(t)
	_, _, err := s.OpenResult("job-1", "tidak-ada.jpg")
	if err == nil {
		t.Error("OpenResult harus error untuk file yang tidak ada")
	}
}

// TestMultipleJobsIsolated memastikan data antar job tidak tercampur.
func TestMultipleJobsIsolated(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveResult("job-A", "foto.jpg", []byte("hasil-A"))
	_ = s.SaveResult("job-B", "foto.jpg", []byte("hasil-B"))

	dataA, _ := s.ReadResult("job-A", "foto.jpg")
	dataB, _ := s.ReadResult("job-B", "foto.jpg")

	if bytes.Equal(dataA, dataB) {
		t.Error("data job-A dan job-B seharusnya berbeda")
	}
	if !bytes.Equal(dataA, []byte("hasil-A")) {
		t.Errorf("data job-A salah: %q", dataA)
	}
}
