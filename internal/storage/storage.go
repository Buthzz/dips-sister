// Package storage mengelola penyimpanan file sementara di master.
//
// Desain kunci:
//   - Hanya master yang menyimpan file; node bersifat stateless
//   - Semua penulisan menggunakan pola temp-file + rename agar atomik
//   - Tidak ada pembaca yang pernah melihat file setengah jadi
//
// Referensi: DESIGN.md §7.4 (atomic write untuk first-result-wins)
package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Store merepresentasikan direktori penyimpanan satu instance master.
// Semua operasi thread-safe di level filesystem; OS menjamin atomisitas rename.
type Store struct {
	root string
}

// New membuat Store yang berakar di dir, menciptakan direktori jika belum ada.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("storage: buat direktori %s: %w", dir, err)
	}
	return &Store{root: dir}, nil
}

// Init menghapus sisa data job dari sesi sebelumnya dan membuat ulang direktori bersih.
// Harus dipanggil sekali saat startup, konsisten dengan keputusan D9 (state tidak dipulihkan).
func (s *Store) Init() error {
	jobsDir := filepath.Join(s.root, "jobs")
	_ = os.RemoveAll(jobsDir)
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		return fmt.Errorf("storage: inisialisasi direktori jobs: %w", err)
	}
	return nil
}

// SaveUpload menyimpan byte gambar yang di-upload ke jobs/{jobID}/input/{filename}.
// Penulisan atomik mencegah task yang membaca upload melihat data parsial.
func (s *Store) SaveUpload(jobID, filename string, data []byte) error {
	dir := filepath.Join(s.root, "jobs", jobID, "input")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("storage: buat direktori input: %w", err)
	}
	return atomicWrite(filepath.Join(dir, filename), data)
}

// ReadUpload mengembalikan byte gambar yang sudah di-upload untuk task tertentu.
func (s *Store) ReadUpload(jobID, filename string) ([]byte, error) {
	path := filepath.Join(s.root, "jobs", jobID, "input", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage: baca upload %s: %w", filename, err)
	}
	return data, nil
}

// SaveResult menyimpan hasil pemrosesan ke jobs/{jobID}/results/{filename}.
// Jika file sudah ada (first-result-wins), penulisan baru menimpa — atomik,
// tidak ada jendela waktu di mana file terlihat rusak.
func (s *Store) SaveResult(jobID, filename string, data []byte) error {
	dir := filepath.Join(s.root, "jobs", jobID, "results")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("storage: buat direktori results: %w", err)
	}
	return atomicWrite(filepath.Join(dir, filename), data)
}

// ReadResult mengembalikan byte hasil pemrosesan.
func (s *Store) ReadResult(jobID, filename string) ([]byte, error) {
	path := filepath.Join(s.root, "jobs", jobID, "results", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage: baca result %s: %w", filename, err)
	}
	return data, nil
}

// ResultExists mengecek apakah file hasil sudah ada tanpa membuka isinya.
// Digunakan scheduler untuk menerapkan first-result-wins.
func (s *Store) ResultExists(jobID, filename string) bool {
	path := filepath.Join(s.root, "jobs", jobID, "results", filename)
	_, err := os.Stat(path)
	return err == nil
}

// DeleteJob menghapus semua file milik satu job secara rekursif.
// Di Windows, RemoveAll bisa gagal dengan "Access Denied" jika goroutine lain
// masih menulis ke direktori yang sama. Retry dengan backoff singkat menangani kasus ini.
func (s *Store) DeleteJob(jobID string) error {
	dir := filepath.Join(s.root, "jobs", jobID)
	var lastErr error
	for range 5 {
		if err := os.RemoveAll(dir); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("storage: hapus job %s gagal setelah retry: %w", jobID, lastErr)
}

// OpenResult membuka file hasil sebagai ReadCloser untuk di-stream ke klien HTTP.
// Pemanggil bertanggung jawab menutup ReadCloser setelah selesai.
func (s *Store) OpenResult(jobID, filename string) (io.ReadCloser, int64, error) {
	path := filepath.Join(s.root, "jobs", jobID, "results", filename)
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("storage: buka result: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("storage: stat result: %w", err)
	}
	return f, info.Size(), nil
}

// atomicWrite menulis data ke path menggunakan pola temp-file + rename.
//
// Urutan operasi:
//  1. Buat file sementara di direktori yang sama dengan tujuan
//  2. Tulis semua data ke file sementara
//  3. Tutup file sementara (flush ke disk)
//  4. Rename file sementara ke path tujuan (atomik di POSIX dan Windows NTFS)
//
// Jika langkah 2 atau 3 gagal, file sementara dihapus agar tidak meninggalkan sampah.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return fmt.Errorf("storage: buat file sementara: %w", err)
	}
	tmpName := tmp.Name()

	_, werr := tmp.Write(data)
	cerr := tmp.Close()

	if werr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("storage: tulis file sementara: %w", werr)
	}
	if cerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("storage: tutup file sementara: %w", cerr)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("storage: rename gagal: %w", err)
	}
	return nil
}
