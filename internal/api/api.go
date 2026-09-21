// Package api mengimplementasikan REST API publik untuk node master.
//
// Desain REST (DESIGN.md §5.1 — RESTful API, Slide 06):
//   - Resource berbasis kata benda (jobs, nodes), method HTTP sesuai semantik
//   - POST /api/v1/jobs → 202 Accepted (asinkron; klien polling status)
//   - GET  /api/v1/jobs/{id}/results/{file} → stream langsung dari storage
//   - Semua error menggunakan amplop JSON yang konsisten: {"error": "..."}
//   - /healthz untuk monitoring dan load balancer
//
// Routing menggunakan net/http built-in Go 1.22+ yang mendukung
// pola "METHOD /path/{wildcard}" tanpa library eksternal.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"distapi/internal/registry"
	"distapi/internal/scheduler"
	"distapi/internal/storage"
)

// Handler menggabungkan semua endpoint REST dan dependensinya.
// Dependensi diinjeksi via konstruktor, bukan global variable.
type Handler struct {
	sched     *scheduler.Scheduler
	reg       *registry.Registry
	store     *storage.Store
	maxImgMB  int
	maxImages int
	log       *slog.Logger
	mux       *http.ServeMux
}

// New membuat Handler dan mendaftarkan semua route.
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

// daftarkanRoute mendaftarkan semua endpoint ke ServeMux bawaan Go 1.22+.
// Pola "METHOD /path/{param}" otomatis dimatching dan param diambil via r.PathValue.
func (h *Handler) daftarkanRoute() {
	h.mux.HandleFunc("GET /healthz", h.handleHealth)

	h.mux.HandleFunc("POST /api/v1/jobs", h.handleBuatJob)
	h.mux.HandleFunc("GET /api/v1/jobs", h.handleListJob)
	h.mux.HandleFunc("GET /api/v1/jobs/{id}", h.handleGetJob)
	h.mux.HandleFunc("DELETE /api/v1/jobs/{id}", h.handleHapusJob)

	h.mux.HandleFunc("GET /api/v1/jobs/{id}/results/{file}", h.handleUnduhHasil)

	h.mux.HandleFunc("GET /api/v1/nodes", h.handleListNode)
}

// handleHealth mengembalikan 200 OK selalu; dipakai oleh health check dan monitoring.
func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	tulisJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleBuatJob menerima upload gambar multipart, memvalidasi, dan membuat job baru.
// Menggunakan 202 Accepted karena pemrosesan berjalan asinkron di background goroutine.
func (h *Handler) handleBuatJob(w http.ResponseWriter, r *http.Request) {
	batasByte := int64(h.maxImgMB) * int64(h.maxImages) * 1024 * 1024
	if err := r.ParseMultipartForm(batasByte); err != nil {
		tulisError(w, http.StatusBadRequest, "form multipart tidak valid: "+err.Error())
		return
	}

	// Baca opsi pemrosesan dari field "options" (JSON opsional).
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
		tulisError(w, http.StatusBadRequest, "tidak ada gambar (gunakan field name: images)")
		return
	}
	if len(fileHeaders) > h.maxImages {
		tulisError(w, http.StatusBadRequest,
			fmt.Sprintf("terlalu banyak gambar (maksimum %d)", h.maxImages))
		return
	}

	batasSatuFile := int64(h.maxImgMB) * 1024 * 1024
	var namaFile []string
	var dataGambar [][]byte

	for _, fh := range fileHeaders {
		// Validasi format berdasarkan ekstensi (DESIGN.md §4: hanya JPEG dan PNG).
		ekst := strings.ToLower(fh.Filename)
		if !strings.HasSuffix(ekst, ".jpg") && !strings.HasSuffix(ekst, ".jpeg") &&
			!strings.HasSuffix(ekst, ".png") {
			tulisError(w, http.StatusUnsupportedMediaType,
				fmt.Sprintf("format tidak didukung: %s (hanya JPEG dan PNG)", fh.Filename))
			return
		}
		if fh.Size > batasSatuFile {
			tulisError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("%s melebihi batas %d MB", fh.Filename, h.maxImgMB))
			return
		}

		f, err := fh.Open()
		if err != nil {
			tulisError(w, http.StatusInternalServerError, "gagal membuka file: "+err.Error())
			return
		}
		// Batasi pembacaan tepat 1 byte di atas limit untuk mendeteksi file yang terlalu besar.
		data, err := io.ReadAll(io.LimitReader(f, batasSatuFile+1))
		f.Close()
		if err != nil {
			tulisError(w, http.StatusInternalServerError, "gagal membaca file: "+err.Error())
			return
		}
		if int64(len(data)) > batasSatuFile {
			tulisError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("%s melebihi batas %d MB", fh.Filename, h.maxImgMB))
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

	h.log.Info("job diterima via REST",
		slog.String("job_id", job.ID),
		slog.Int("file", len(namaFile)))

	tulisJSON(w, http.StatusAccepted, map[string]any{
		"job_id": job.ID,
		"status": string(job.Status),
		"tasks":  len(job.Tasks),
	})
}

// handleListJob mengembalikan ringkasan semua job.
func (h *Handler) handleListJob(w http.ResponseWriter, _ *http.Request) {
	jobs := h.sched.ListJobs()
	resp := make([]ringkasanJob, len(jobs))
	for i, j := range jobs {
		resp[i] = keRingkasanJob(j)
	}
	tulisJSON(w, http.StatusOK, resp)
}

// handleGetJob mengembalikan detail job termasuk status setiap task.
func (h *Handler) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job := h.sched.GetJob(id)
	if job == nil {
		tulisError(w, http.StatusNotFound, "job tidak ditemukan")
		return
	}
	tulisJSON(w, http.StatusOK, keDetailJob(job))
}

// handleHapusJob membatalkan job dan menghapus semua filenya.
func (h *Handler) handleHapusJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.sched.CancelJob(id); err != nil {
		tulisError(w, http.StatusNotFound, "job tidak ditemukan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUnduhHasil meng-stream file hasil pemrosesan langsung ke klien.
// Menggunakan io.Copy untuk efisiensi memori — tidak muat semua ke RAM dulu.
func (h *Handler) handleUnduhHasil(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	namaFile := r.PathValue("file")

	rc, ukuran, err := h.store.OpenResult(jobID, namaFile)
	if err != nil {
		tulisError(w, http.StatusNotFound, "hasil tidak ditemukan")
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
	io.Copy(w, rc) //nolint:errcheck // klien mungkin putus koneksi, itu wajar
}

// handleListNode mengembalikan status semua node (hidup maupun mati).
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

// ringkasanTask adalah bentuk ringkas task untuk response API.
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

// ringkasanJob adalah tampilan ringkas job untuk daftar.
type ringkasanJob struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Total     int       `json:"total"`
	Done      int       `json:"done"`
	Failed    int       `json:"failed"`
	CreatedAt time.Time `json:"created_at"`
}

// detailJob adalah tampilan lengkap job dengan semua task.
type detailJob struct {
	ringkasanJob
	Tasks []ringkasanTask `json:"tasks"`
}

// responNode mewakili satu node dalam response API.
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

// tulisJSON menyetel Content-Type dan mengodekan v sebagai JSON ke w.
func tulisJSON(w http.ResponseWriter, kode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(kode)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Header sudah terkirim; tidak bisa berbuat banyak selain log.
		_ = err
	}
}

// tulisError mengirimkan respons error dengan amplop JSON yang konsisten.
func tulisError(w http.ResponseWriter, kode int, pesan string) {
	tulisJSON(w, kode, map[string]string{"error": pesan})
}
