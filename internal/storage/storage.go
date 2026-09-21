// Package storage mengelola persistensi berkas unggahan dan hasil pemrosesan pada master.
package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Store menyediakan operasi I/O berkas pada direktori root penyimpanan.
type Store struct {
	root string
}

// New menginisialisasi Store pada direktori yang ditentukan.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("storage: gagal membuat direktori %s: %w", dir, err)
	}
	return &Store{root: dir}, nil
}

// Init membersihkan sisa berkas dari eksekusi sebelumnya dan menyiapkan direktori kerja baru.
func (s *Store) Init() error {
	jobsDir := filepath.Join(s.root, "jobs")
	_ = os.RemoveAll(jobsDir)
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		return fmt.Errorf("storage: inisialisasi direktori jobs gagal: %w", err)
	}
	return nil
}

// SaveUpload menyimpan data berkas unggahan secara atomik.
func (s *Store) SaveUpload(jobID, filename string, data []byte) error {
	dir := filepath.Join(s.root, "jobs", jobID, "input")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("storage: gagal membuat direktori input: %w", err)
	}
	return atomicWrite(filepath.Join(dir, filename), data)
}

// ReadUpload membaca berkas unggahan untuk task tertentu.
func (s *Store) ReadUpload(jobID, filename string) ([]byte, error) {
	path := filepath.Join(s.root, "jobs", jobID, "input", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage: gagal membaca upload %s: %w", filename, err)
	}
	return data, nil
}

// SaveResult menyimpan data hasil pemrosesan secara atomik.
func (s *Store) SaveResult(jobID, filename string, data []byte) error {
	dir := filepath.Join(s.root, "jobs", jobID, "results")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("storage: gagal membuat direktori results: %w", err)
	}
	return atomicWrite(filepath.Join(dir, filename), data)
}

// ReadResult membaca berkas hasil pemrosesan.
func (s *Store) ReadResult(jobID, filename string) ([]byte, error) {
	path := filepath.Join(s.root, "jobs", jobID, "results", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage: gagal membaca result %s: %w", filename, err)
	}
	return data, nil
}

// ResultExists memeriksa keberadaan berkas hasil pemrosesan.
func (s *Store) ResultExists(jobID, filename string) bool {
	path := filepath.Join(s.root, "jobs", jobID, "results", filename)
	_, err := os.Stat(path)
	return err == nil
}

// DeleteJob menghapus seluruh direktori data milik satu job.
func (s *Store) DeleteJob(jobID string) error {
	dir := filepath.Join(s.root, "jobs", jobID)
	var lastErr error
	for range 5 {
		if err := os.RemoveAll(dir); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(20 * time.Millisecond) // jeda untuk rilis file lock di Windows
	}
	return fmt.Errorf("storage: gagal menghapus direktori job %s: %w", jobID, lastErr)
}

// OpenResult membuka stream pembacaan berkas hasil olahan.
func (s *Store) OpenResult(jobID, filename string) (io.ReadCloser, int64, error) {
	path := filepath.Join(s.root, "jobs", jobID, "results", filename)
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("storage: gagal membuka result: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("storage: gagal membaca info berkas: %w", err)
	}
	return f, info.Size(), nil
}

// atomicWrite menulis data ke berkas sementara lalu me-rename ke path target.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return fmt.Errorf("storage: gagal membuat berkas sementara: %w", err)
	}
	tmpName := tmp.Name()

	_, werr := tmp.Write(data)
	cerr := tmp.Close()

	if werr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("storage: gagal menulis data sementara: %w", werr)
	}
	if cerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("storage: gagal menutup berkas sementara: %w", cerr)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("storage: rename atomik gagal: %w", err)
	}
	return nil
}
