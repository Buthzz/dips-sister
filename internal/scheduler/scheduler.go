// Package scheduler mengelola pembagian tugas pemrosesan citra ke worker node.
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

// TaskStatus mendefinisikan status eksekusi satu unit task.
type TaskStatus string

const (
	TaskPending TaskStatus = "PENDING"
	TaskRunning TaskStatus = "RUNNING"
	TaskDone    TaskStatus = "DONE"
	TaskFailed  TaskStatus = "FAILED"
)

// JobStatus mendefinisikan status agregat dari sebuah batch job.
type JobStatus string

const (
	JobPending    JobStatus = "PENDING"
	JobProcessing JobStatus = "PROCESSING"
	JobDone       JobStatus = "DONE"
	JobFailed     JobStatus = "FAILED"
)

// Task merepresentasikan satu berkas citra yang diproses.
type Task struct {
	ID         string
	JobID      string
	Index      int
	Filename   string
	Retries    int
	Status     TaskStatus
	AssignedTo string
	StartedAt  time.Time
	DoneAt     time.Time
	DurationMs int64
	ErrMsg     string
}

// Job merepresentasikan kumpulan task dalam satu permintaan unggahan.
type Job struct {
	ID        string
	Filenames []string
	Tasks     []*Task
	Status    JobStatus
	CreatedAt time.Time
	DoneAt    time.Time
	Options   ProcessOptions
}

// ProcessOptions memuat parameter transformasi citra.
type ProcessOptions struct {
	ResizeWidth  int
	ResizeHeight int
	Grayscale    bool
}

// NodeClient adalah antarmuka pemanggilan eksekusi ke worker node.
type NodeClient interface {
	ProcessImage(ctx context.Context, nodeID string, task *Task, imageData []byte, opts ProcessOptions) ([]byte, error)
}

// Scheduler mengorkestrasi pembuatan job, distribusi task, dan penanganan kegagalan.
type Scheduler struct {
	mu         sync.RWMutex
	jobs       map[string]*Job
	reg        *registry.Registry
	store      *storage.Store
	client     NodeClient
	maxRetries int
	taskTmt    time.Duration
	log        *slog.Logger
	rrCounter  atomic.Uint64
}

// New membuat instance Scheduler baru.
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
func (s *Scheduler) Submit(ctx context.Context, filenames []string, imageData [][]byte, opts ProcessOptions) (*Job, error) {
	if len(filenames) == 0 {
		return nil, fmt.Errorf("scheduler: tidak ada berkas untuk diproses")
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
		slog.Int("tasks", len(tasks)))

	go s.dispatchJob(context.Background(), job)

	return job, nil
}

// GetJob mengambil snapshot data job berdasarkan ID-nya.
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

// ListJobs mengembalikan seluruh job yang terurut dari yang terbaru.
func (s *Scheduler) ListJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		cp := *j
		out = append(out, &cp)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// CancelJob membatalkan job dan membersihkan berkas terkait.
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

// RescheduleDeadNodeTasks menjadwalkan ulang task yang terdampak kematian node.
func (s *Scheduler) RescheduleDeadNodeTasks(deadNodeIDs []string) {
	if len(deadNodeIDs) == 0 {
		return
	}

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
				t.Status = TaskPending
				t.AssignedTo = ""
				jobsUntukDispatch[j.ID] = j
				s.log.Warn("task dijadwalkan ulang",
					slog.String("task_id", t.ID),
					slog.String("node_sebelumnya", deadNodeIDs[0]))
			}
		}
	}
	s.mu.Unlock()

	for _, j := range jobsUntukDispatch {
		go s.dispatchJob(context.Background(), j)
	}
}

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

func (s *Scheduler) jalankanTask(ctx context.Context, job *Job, task *Task) {
	for {
		s.mu.Lock()
		if task.Status == TaskDone || task.Status == TaskFailed {
			s.mu.Unlock()
			return
		}
		if task.Retries >= s.maxRetries {
			task.Status = TaskFailed
			task.ErrMsg = "maksimum percobaan tercapai"
			s.mu.Unlock()
			s.log.Error("task gagal permanen",
				slog.String("task_id", task.ID),
				slog.Int("retries", task.Retries))
			return
		}
		s.mu.Unlock()

		node := s.pilihNode()
		if node == nil {
			node = &registry.NodeInfo{NodeID: "master-local"}
			s.log.Warn("tidak ada worker aktif, menggunakan master-local",
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

		tCtx, batal := context.WithTimeout(ctx, s.taskTmt)
		hasilData, err := s.client.ProcessImage(tCtx, node.NodeID, task, imageData, job.Options)
		batal()

		if err != nil {
			s.log.Warn("task gagal, mencoba ulang",
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

func (s *Scheduler) pilihNode() *registry.NodeInfo {
	nodes := s.reg.AliveNodes()
	if len(nodes) == 0 {
		return nil
	}
	idx := s.rrCounter.Add(1) - 1
	n := nodes[int(idx)%len(nodes)]
	return &n
}

func buatID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}
