// Package scheduler mengimplementasikan scatter-gather task distribution.
//
// Alur kerja (DESIGN.md §6):
//  1. Client POST /api/v1/jobs → handler memanggil scheduler.Submit
//  2. Submit membuat Job + Tasks, menyimpan upload, lalu launch goroutine dispatchJob
//  3. dispatchJob menjalankan setiap task secara paralel menggunakan sync.WaitGroup
//  4. Setiap task dipilih node menggunakan round-robin, lalu NodeClient.ProcessImage dipanggil
//  5. Hasil disimpan ke storage secara atomik; first-result-wins jika ada retry paralel
//  6. Jika task gagal < MaxRetries, task dikembalikan ke PENDING dan dicoba ulang
//  7. Setelah semua task selesai, status Job diperbarui (DONE atau FAILED)
//
// Antarmuka NodeClient memisahkan scheduler dari detail transport gRPC,
// sehingga scheduler bisa diuji dengan mock tanpa jaringan nyata.
package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"distapi/internal/registry"
	"distapi/internal/storage"
)

// TaskStatus merepresentasikan mesin status task (forward-only, DESIGN.md §6.2).
// Transisi yang valid: PENDING → RUNNING → DONE
//
//	PENDING → RUNNING → FAILED (jika MaxRetries terlampaui)
//	RUNNING → PENDING (jika node mati, untuk dijadwalkan ulang)
type TaskStatus string

const (
	TaskPending TaskStatus = "PENDING"
	TaskRunning TaskStatus = "RUNNING"
	TaskDone    TaskStatus = "DONE"
	TaskFailed  TaskStatus = "FAILED"
)

// JobStatus adalah agregasi dari status semua task dalam satu job.
type JobStatus string

const (
	JobPending    JobStatus = "PENDING"
	JobProcessing JobStatus = "PROCESSING"
	JobDone       JobStatus = "DONE"
	// JobFailed: minimal satu task FAILED dan tidak bisa di-retry lagi.
	JobFailed JobStatus = "FAILED"
)

// Task merepresentasikan satu unit kerja — satu gambar yang akan diproses.
// ID task bersifat deterministik: "{job_id}-{index:03d}" sehingga idempoten
// bahkan jika master crash dan restart (walaupun state hilang per desain saat ini).
type Task struct {
	ID         string
	JobID      string
	Index      int    // urutan dalam job, dimulai dari 0
	Filename   string // nama file yang akan diambil dari storage
	Retries    int
	Status     TaskStatus
	AssignedTo string // nodeID yang sedang mengerjakan task ini
	StartedAt  time.Time
	DoneAt     time.Time
	DurationMs int64  // durasi yang dilaporkan node
	ErrMsg     string // pesan error jika gagal
}

// Job merepresentasikan satu permintaan pemrosesan dari klien.
type Job struct {
	ID        string
	Filenames []string // nama file asli dari upload
	Tasks     []*Task
	Status    JobStatus
	CreatedAt time.Time
	DoneAt    time.Time
	Options   ProcessOptions
}

// ProcessOptions berisi parameter transformasi yang diteruskan ke setiap task.
type ProcessOptions struct {
	ResizeWidth  int
	ResizeHeight int
	Grayscale    bool
}

// NodeClient adalah antarmuka yang digunakan scheduler untuk berkomunikasi dengan node.
// Menggunakan antarmuka (bukan struct konkret) agar scheduler bisa diuji dengan mock
// tanpa memerlukan koneksi gRPC nyata.
type NodeClient interface {
	ProcessImage(ctx context.Context, nodeID string, task *Task, imageData []byte, opts ProcessOptions) ([]byte, error)
}

// Scheduler mengelola semua job dan mendistribusikan task ke node kluster.
type Scheduler struct {
	mu         sync.RWMutex
	jobs       map[string]*Job
	reg        *registry.Registry
	store      *storage.Store
	client     NodeClient
	maxRetries int
	taskTmt    time.Duration
	log        *slog.Logger

	// rrCounter untuk round-robin dispatch; atomic agar tidak perlu mutex tambahan.
	rrCounter atomic.Uint64
}

// New membuat Scheduler baru. Semua dependensi diinjeksi via constructor
// (tidak ada global variable) sesuai prinsip testability dan SRP.
func New(
	reg *registry.Registry,
	store *storage.Store,
	client NodeClient,
	maxRetries int,
	taskTimeout time.Duration,
	log *slog.Logger,
) *Scheduler {
	return &Scheduler{
		jobs:       make(map[string]*Job),
		reg:        reg,
		store:      store,
		client:     client,
		maxRetries: maxRetries,
		taskTmt:    taskTimeout,
		log:        log,
	}
}

// Submit membuat job baru dan memulai pemrosesan secara asinkron.
// Handler REST langsung mendapat ID job untuk polling status (202 Accepted).
//
// imageData[i] harus sesuai dengan filenames[i]; keduanya harus memiliki panjang sama.
func (s *Scheduler) Submit(ctx context.Context, filenames []string, imageData [][]byte, opts ProcessOptions) (*Job, error) {
	if len(filenames) == 0 {
		return nil, fmt.Errorf("scheduler: tidak ada file untuk diproses")
	}

	jobID := buatID("job")
	tasks := make([]*Task, len(filenames))
	for i, name := range filenames {
		tasks[i] = &Task{
			ID:       fmt.Sprintf("%s-%03d", jobID, i),
			JobID:    jobID,
			Index:    i,
			Filename: name,
			Status:   TaskPending,
		}
		// Simpan upload ke disk dulu agar task bisa di-retry ke node manapun.
		if err := s.store.SaveUpload(jobID, name, imageData[i]); err != nil {
			return nil, fmt.Errorf("scheduler: simpan upload %s gagal: %w", name, err)
		}
	}

	job := &Job{
		ID:        jobID,
		Filenames: filenames,
		Tasks:     tasks,
		Status:    JobPending,
		CreatedAt: time.Now(),
		Options:   opts,
	}

	s.mu.Lock()
	s.jobs[jobID] = job
	s.mu.Unlock()

	s.log.Info("job diterima",
		slog.String("job_id", jobID),
		slog.Int("jumlah_task", len(tasks)))

	// Proses secara asinkron agar HTTP handler langsung bisa balas 202.
	go s.dispatchJob(context.Background(), job)

	return job, nil
}

// GetJob mengembalikan snapshot job atau nil jika tidak ditemukan.
// Snapshot (bukan pointer langsung) mencegah data race saat serialisasi ke JSON.
func (s *Scheduler) GetJob(jobID string) *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j := s.jobs[jobID]
	if j == nil {
		return nil
	}
	cp := *j
	cp.Tasks = make([]*Task, len(j.Tasks))
	for i, t := range j.Tasks {
		tc := *t
		cp.Tasks[i] = &tc
	}
	return &cp
}

// ListJobs mengembalikan semua job, diurutkan dari yang terbaru.
func (s *Scheduler) ListJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		cp := *j
		out = append(out, &cp)
	}
	// Urutkan: terbaru di depan (insertion sort, N kecil di konteks tugas kuliah).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// CancelJob membatalkan job dan menghapus semua filenya dari storage.
// Jika job sedang berjalan, goroutine task yang aktif akan menganggur
// (tidak ada mekanisme paksa stop goroutine — desain sengaja sederhana).
func (s *Scheduler) CancelJob(jobID string) error {
	s.mu.Lock()
	_, ok := s.jobs[jobID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("scheduler: job %s tidak ditemukan", jobID)
	}
	delete(s.jobs, jobID)
	s.mu.Unlock()

	return s.store.DeleteJob(jobID)
}

// RescheduleDeadNodeTasks menjadwalkan ulang task yang sedang berjalan
// di node yang baru saja dinyatakan mati. Dipanggil oleh dead-node monitor di main.
func (s *Scheduler) RescheduleDeadNodeTasks(deadNodeIDs []string) {
	if len(deadNodeIDs) == 0 {
		return
	}

	// Buat set untuk pencarian O(1).
	mati := make(map[string]bool, len(deadNodeIDs))
	for _, id := range deadNodeIDs {
		mati[id] = true
	}

	s.mu.Lock()
	jobsUntukDispatch := make(map[string]*Job)
	for _, j := range s.jobs {
		if j.Status == JobDone || j.Status == JobFailed {
			continue
		}
		for _, t := range j.Tasks {
			if t.Status == TaskRunning && mati[t.AssignedTo] {
				// Reset ke PENDING agar bisa diambil oleh node lain.
				t.Status = TaskPending
				t.AssignedTo = ""
				jobsUntukDispatch[j.ID] = j
				s.log.Warn("task dijadwalkan ulang",
					slog.String("task_id", t.ID),
					slog.String("alasan", "node mati"))
			}
		}
	}
	s.mu.Unlock()

	for _, j := range jobsUntukDispatch {
		go s.dispatchJob(context.Background(), j)
	}
}

// dispatchJob menjalankan semua task PENDING dalam job secara paralel.
// Goroutine ini adalah implementasi "gather" dalam scatter-gather:
// ia menunggu semua task selesai lalu memperbarui status job.
func (s *Scheduler) dispatchJob(ctx context.Context, job *Job) {
	s.mu.Lock()
	job.Status = JobProcessing
	s.mu.Unlock()

	var wg sync.WaitGroup
	for _, t := range job.Tasks {
		s.mu.RLock()
		lewati := t.Status == TaskDone || t.Status == TaskFailed
		s.mu.RUnlock()
		if lewati {
			continue
		}

		wg.Add(1)
		go func(task *Task) {
			defer wg.Done()
			s.jalankanTask(ctx, job, task)
		}(t)
	}
	wg.Wait()

	// Hitung status akhir job berdasarkan agregasi task.
	s.mu.Lock()
	adaFailed := false
	semuaDone := true
	for _, t := range job.Tasks {
		if t.Status != TaskDone {
			semuaDone = false
		}
		if t.Status == TaskFailed {
			adaFailed = true
		}
	}
	if semuaDone {
		job.Status = JobDone
		job.DoneAt = time.Now()
	} else if adaFailed {
		job.Status = JobFailed
		job.DoneAt = time.Now()
	}
	s.mu.Unlock()

	s.log.Info("job selesai",
		slog.String("job_id", job.ID),
		slog.String("status", string(job.Status)))
}

// jalankanTask adalah "scatter" dalam scatter-gather: ia mengambil satu task,
// memilih node via round-robin, mengirim request, dan menangani hasilnya.
// Loop retry terus berjalan selama task belum DONE/FAILED dan belum melebihi MaxRetries.
func (s *Scheduler) jalankanTask(ctx context.Context, job *Job, task *Task) {
	for {
		s.mu.Lock()
		// Periksa apakah task sudah selesai (bisa dari retry paralel — first-result-wins).
		if task.Status == TaskDone || task.Status == TaskFailed {
			s.mu.Unlock()
			return
		}
		if task.Retries >= s.maxRetries {
			task.Status = TaskFailed
			task.ErrMsg = "jumlah percobaan maksimum terlampaui"
			s.mu.Unlock()
			s.log.Error("task gagal permanen",
				slog.String("task_id", task.ID),
				slog.Int("retries", task.Retries))
			return
		}
		s.mu.Unlock()

		// Pilih node; fallback ke master-local jika tidak ada node.
		node := s.pilihNode()
		if node == nil {
			node = &registry.NodeInfo{NodeID: "master-local"}
			s.log.Warn("tidak ada node aktif, proses di master-local",
				slog.String("task_id", task.ID))
		}

		imageData, err := s.store.ReadUpload(job.ID, task.Filename)
		if err != nil {
			s.log.Error("baca upload gagal",
				slog.String("task_id", task.ID),
				slog.String("error", err.Error()))
			s.mu.Lock()
			task.Retries++
			s.mu.Unlock()
			continue
		}

		s.mu.Lock()
		task.Status = TaskRunning
		task.AssignedTo = node.NodeID
		task.StartedAt = time.Now()
		s.mu.Unlock()

		s.log.Info("task dikirim ke node",
			slog.String("task_id", task.ID),
			slog.String("node_id", node.NodeID))

		tCtx, batal := context.WithTimeout(ctx, s.taskTmt)
		hasilData, err := s.client.ProcessImage(tCtx, node.NodeID, task, imageData, job.Options)
		batal()

		if err != nil {
			s.log.Warn("task gagal, akan dicoba ulang",
				slog.String("task_id", task.ID),
				slog.String("node_id", node.NodeID),
				slog.String("error", err.Error()))
			s.mu.Lock()
			task.Status = TaskPending
			task.Retries++
			task.AssignedTo = ""
			s.mu.Unlock()
			continue
		}

		// Simpan hasil secara atomik. Jika sudah ada (dari retry lain), tetap ok —
		// ini adalah implementasi first-result-wins; operasi rename atomik di OS.
		if !s.store.ResultExists(job.ID, task.Filename) {
			if err := s.store.SaveResult(job.ID, task.Filename, hasilData); err != nil {
				s.log.Error("simpan hasil gagal",
					slog.String("task_id", task.ID),
					slog.String("error", err.Error()))
				s.mu.Lock()
				task.Retries++
				task.Status = TaskPending
				s.mu.Unlock()
				continue
			}
		}

		s.mu.Lock()
		task.Status = TaskDone
		task.DoneAt = time.Now()
		task.DurationMs = time.Since(task.StartedAt).Milliseconds()
		s.mu.Unlock()

		s.log.Info("task berhasil",
			slog.String("task_id", task.ID),
			slog.String("node_id", node.NodeID),
			slog.Int64("duration_ms", task.DurationMs))
		return
	}
}

// pilihNode memilih node aktif menggunakan round-robin.
// Round-robin dipilih karena sederhana dan cukup adil untuk demo;
// load-based dispatch bisa ditambahkan kemudian (DESIGN.md §13.2).
func (s *Scheduler) pilihNode() *registry.NodeInfo {
	nodes := s.reg.AliveNodes()
	if len(nodes) == 0 {
		return nil
	}
	idx := s.rrCounter.Add(1) - 1
	n := nodes[int(idx)%len(nodes)]
	return &n
}

// buatID menghasilkan ID unik dengan prefix dan 8 byte acak (hex).
// Menggunakan crypto/rand (bukan math/rand) untuk menghindari tabrakan ID
// meskipun beberapa job dibuat dalam waktu yang sangat berdekatan.
func buatID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}
