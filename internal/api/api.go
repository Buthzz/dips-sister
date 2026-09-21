// Package api mengimplementasikan RESTful API gateway pada master node.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"distapi/internal/registry"
	"distapi/internal/scheduler"
	"distapi/internal/storage"
	webui "distapi/web"
)

// Handler mengelola routing dan dispatch request HTTP.
type Handler struct {
	sched     *scheduler.Scheduler
	reg       *registry.Registry
	store     *storage.Store
	maxImgMB  int
	maxImages int
	log       *slog.Logger
	mux       *http.ServeMux
}

// New membuat instance Handler dengan seluruh route yang terdaftar.
func New(
	sched *scheduler.Scheduler,
	reg *registry.Registry,
	store *storage.Store,
	maxImgMB, maxImages int,
	log *slog.Logger,
) *Handler {
	h := &Handler{
		sched:     sched,
		reg:       reg,
		store:     store,
		maxImgMB:  maxImgMB,
		maxImages: maxImages,
		log:       log,
		mux:       http.NewServeMux(),
	}
	h.daftarkanRoute()
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) daftarkanRoute() {
	h.mux.HandleFunc("GET /healthz", h.handleHealth)
	h.mux.HandleFunc("POST /api/v1/jobs", h.handleBuatJob)
	h.mux.HandleFunc("GET /api/v1/jobs", h.handleListJob)
	h.mux.HandleFunc("GET /api/v1/jobs/{id}", h.handleGetJob)
	h.mux.HandleFunc("DELETE /api/v1/jobs/{id}", h.handleHapusJob)
	h.mux.HandleFunc("GET /api/v1/jobs/{id}/results/{file}", h.handleUnduhHasil)
	h.mux.HandleFunc("GET /api/v1/nodes", h.handleListNode)
	h.daftarkanFrontend()
}

// daftarkanFrontend menyajikan aset statis Vue yang di-embed ke binary.
func (h *Handler) daftarkanFrontend() {
	sub, err := fs.Sub(webui.Dist, "dist")
	if err != nil {
		h.log.Error("aset frontend tidak tersedia",
			slog.String("error", err.Error()))
		return
	}

	h.mux.Handle("GET /assets/",
		http.FileServer(http.FS(sub)))
	h.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		nama := strings.TrimPrefix(r.URL.Path, "/")
		if nama == "" {
			nama = "index.html"
		}
		if f, err := sub.Open(nama); err != nil {
			nama = "index.html"
		} else {
			f.Close()
		}
		http.ServeFileFS(w, r, sub, nama)
	})
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	tulisJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleBuatJob(w http.ResponseWriter, r *http.Request) {
	batasByte := int64(h.maxImgMB) * int64(h.maxImages) * 1024 * 1024
	if err := r.ParseMultipartForm(batasByte); err != nil {
		tulisError(w, http.StatusBadRequest, "form multipart tidak valid: "+err.Error())
		return
	}

	opts := scheduler.ProcessOptions{Grayscale: true, ResizeWidth: 800, ResizeHeight: 800}
	if optsStr := r.FormValue("options"); optsStr != "" {
		var req struct {
			ResizeWidth  int  `json:"resize_width"`
			ResizeHeight int  `json:"resize_height"`
			Grayscale    bool `json:"grayscale"`
		}
		if err := json.Unmarshal([]byte(optsStr), &req); err == nil {
			opts.ResizeWidth = req.ResizeWidth
			opts.ResizeHeight = req.ResizeHeight
			opts.Grayscale = req.Grayscale
		}
	}

	fileHeaders := r.MultipartForm.File["images"]
	if len(fileHeaders) == 0 {
		tulisError(w, http.StatusBadRequest, "field 'images' wajib diisi")
		return
	}
	if len(fileHeaders) > h.maxImages {
		tulisError(w, http.StatusBadRequest,
			fmt.Sprintf("jumlah berkas melebihi batas maksimum (%d)", h.maxImages))
		return
	}

	batasSatuFile := int64(h.maxImgMB) * 1024 * 1024
	var namaFile []string
	var dataGambar [][]byte

	for _, fh := range fileHeaders {
		ekst := strings.ToLower(fh.Filename)
		if !strings.HasSuffix(ekst, ".jpg") && !strings.HasSuffix(ekst, ".jpeg") &&
			!strings.HasSuffix(ekst, ".png") {
			tulisError(w, http.StatusUnsupportedMediaType,
				fmt.Sprintf("format berkas tidak didukung: %s", fh.Filename))
			return
		}
		if fh.Size > batasSatuFile {
			tulisError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("%s melebihi batas ukuran %d MB", fh.Filename, h.maxImgMB))
			return
		}

		f, err := fh.Open()
		if err != nil {
			tulisError(w, http.StatusInternalServerError, "gagal membuka berkas: "+err.Error())
			return
		}
		data, err := io.ReadAll(io.LimitReader(f, batasSatuFile+1))
		f.Close()
		if err != nil {
			tulisError(w, http.StatusInternalServerError, "gagal membaca berkas: "+err.Error())
			return
		}
		if int64(len(data)) > batasSatuFile {
			tulisError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("%s melebihi batas ukuran %d MB", fh.Filename, h.maxImgMB))
			return
		}

		namaFile = append(namaFile, fh.Filename)
		dataGambar = append(dataGambar, data)
	}

	job, err := h.sched.Submit(r.Context(), namaFile, dataGambar, opts)
	if err != nil {
		h.log.Error("submit job gagal", slog.String("error", err.Error()))
		tulisError(w, http.StatusInternalServerError, "gagal membuat job")
		return
	}

	h.log.Info("job diterima",
		slog.String("job_id", job.ID),
		slog.Int("berkas", len(namaFile)))

	tulisJSON(w, http.StatusAccepted, map[string]any{
		"job_id": job.ID,
		"status": string(job.Status),
		"tasks":  len(job.Tasks),
	})
}

func (h *Handler) handleListJob(w http.ResponseWriter, _ *http.Request) {
	jobs := h.sched.ListJobs()
	resp := make([]ringkasanJob, len(jobs))
	for i, j := range jobs {
		resp[i] = keRingkasanJob(j)
	}
	tulisJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job := h.sched.GetJob(id)
	if job == nil {
		tulisError(w, http.StatusNotFound, "job tidak ditemukan")
		return
	}
	tulisJSON(w, http.StatusOK, keDetailJob(job))
}

func (h *Handler) handleHapusJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.sched.CancelJob(id); err != nil {
		tulisError(w, http.StatusNotFound, "job tidak ditemukan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleUnduhHasil(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	namaFile := r.PathValue("file")

	rc, ukuran, err := h.store.OpenResult(jobID, namaFile)
	if err != nil {
		tulisError(w, http.StatusNotFound, "hasil olahan tidak ditemukan")
		return
	}
	defer rc.Close()

	tipeKonten := "image/jpeg"
	if strings.HasSuffix(strings.ToLower(namaFile), ".png") {
		tipeKonten = "image/png"
	}
	w.Header().Set("Content-Type", tipeKonten)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", ukuran))
	w.Header().Set("Content-Disposition", `attachment; filename="`+namaFile+`"`)
	io.Copy(w, rc)
}

func (h *Handler) handleListNode(w http.ResponseWriter, _ *http.Request) {
	nodes := h.reg.AllNodes()
	resp := make([]responNode, len(nodes))
	for i, n := range nodes {
		resp[i] = responNode{
			NodeID:        n.NodeID,
			Status:        string(n.Status),
			AdvertiseAddr: n.AdvertiseAddr,
			ActiveTasks:   n.ActiveTasks,
			Capacity:      n.Capacity,
			LastHeartbeat: n.LastHeartbeat,
		}
	}
	tulisJSON(w, http.StatusOK, resp)
}

type ringkasanTask struct {
	ID         string    `json:"id"`
	Filename   string    `json:"filename"`
	Status     string    `json:"status"`
	AssignedTo string    `json:"assigned_to,omitempty"`
	Retries    int       `json:"retries"`
	DurationMs int64     `json:"duration_ms,omitempty"`
	ErrMsg     string    `json:"error,omitempty"`
	DoneAt     time.Time `json:"done_at,omitempty"`
}

type ringkasanJob struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Total     int       `json:"total"`
	Done      int       `json:"done"`
	Failed    int       `json:"failed"`
	CreatedAt time.Time `json:"created_at"`
}

type detailJob struct {
	ringkasanJob
	Tasks []ringkasanTask `json:"tasks"`
}

type responNode struct {
	NodeID        string    `json:"node_id"`
	Status        string    `json:"status"`
	AdvertiseAddr string    `json:"addr"`
	ActiveTasks   int       `json:"active_tasks"`
	Capacity      int       `json:"capacity"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

func keRingkasanJob(j *scheduler.Job) ringkasanJob {
	selesai, gagal := 0, 0
	for _, t := range j.Tasks {
		if t.Status == scheduler.TaskDone {
			selesai++
		}
		if t.Status == scheduler.TaskFailed {
			gagal++
		}
	}
	return ringkasanJob{
		ID:        j.ID,
		Status:    string(j.Status),
		Total:     len(j.Tasks),
		Done:      selesai,
		Failed:    gagal,
		CreatedAt: j.CreatedAt,
	}
}

func keDetailJob(j *scheduler.Job) detailJob {
	tasks := make([]ringkasanTask, len(j.Tasks))
	for i, t := range j.Tasks {
		tasks[i] = ringkasanTask{
			ID:         t.ID,
			Filename:   t.Filename,
			Status:     string(t.Status),
			AssignedTo: t.AssignedTo,
			Retries:    t.Retries,
			DurationMs: t.DurationMs,
			ErrMsg:     t.ErrMsg,
			DoneAt:     t.DoneAt,
		}
	}
	return detailJob{
		ringkasanJob: keRingkasanJob(j),
		Tasks:        tasks,
	}
}

func tulisJSON(w http.ResponseWriter, kode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(kode)
	_ = json.NewEncoder(w).Encode(v)
}

func tulisError(w http.ResponseWriter, kode int, pesan string) {
	tulisJSON(w, kode, map[string]string{"error": pesan})
}
