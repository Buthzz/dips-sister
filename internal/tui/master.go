// Package tui menyediakan tampilan dashboard kluster untuk mode master.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"distapi/internal/registry"
	"distapi/internal/scheduler"
)

type tickMsg time.Time

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// NodeSnapshot adalah snapshot satu node untuk rendering.
type NodeSnapshot struct {
	NodeID      string
	Addr        string
	Status      string
	ActiveTasks int
	Capacity    int
	LastHB      time.Time
}

// JobSnapshot adalah snapshot satu job untuk rendering.
type JobSnapshot struct {
	ID     string
	Status string
	Total  int
	Done   int
	Failed int
	Age    time.Duration
}

// NodeLister menyediakan abstraksi untuk mengambil daftar node aktif dan terdaftar.
type NodeLister interface {
	AllNodes() []registry.NodeInfo
}

// JobLister menyediakan abstraksi untuk mengambil daftar job yang sedang dan telah diproses.
type JobLister interface {
	ListJobs() []*scheduler.Job
}

// MasterModel adalah Bubble Tea model untuk mode master.
type MasterModel struct {
	nodes    NodeLister
	jobs     JobLister
	log      *EventLog
	masterIP string
	httpPort int
	grpcPort int
	token    string
	width    int
	height   int
	nodeSnap []NodeSnapshot
	jobSnap  []JobSnapshot
	cursor   int // baris node yang dipilih
	tab      int // 0 = nodes+jobs, 1 = log
	quitting bool
}

// NewMasterModel membuat model TUI untuk mode master.
func NewMasterModel(nodes NodeLister, jobs JobLister, log *EventLog, masterIP string, httpPort, grpcPort int, token string) MasterModel {
	m := MasterModel{
		nodes:    nodes,
		jobs:     jobs,
		log:      log,
		masterIP: masterIP,
		httpPort: httpPort,
		grpcPort: grpcPort,
		token:    token,
		width:    120,
		height:   36,
	}
	m.refresh()
	return m
}

func (m *MasterModel) refresh() {
	raw := m.nodes.AllNodes()
	m.nodeSnap = make([]NodeSnapshot, len(raw))
	for i, n := range raw {
		m.nodeSnap[i] = NodeSnapshot{
			NodeID:      n.NodeID,
			Addr:        n.AdvertiseAddr,
			Status:      string(n.Status),
			ActiveTasks: n.ActiveTasks,
			Capacity:    n.Capacity,
			LastHB:      n.LastHeartbeat,
		}
	}

	rawJobs := m.jobs.ListJobs()
	m.jobSnap = make([]JobSnapshot, 0, len(rawJobs))
	for _, j := range rawJobs {
		done, failed := 0, 0
		for _, t := range j.Tasks {
			if t.Status == scheduler.TaskDone {
				done++
			}
			if t.Status == scheduler.TaskFailed {
				failed++
			}
		}
		m.jobSnap = append(m.jobSnap, JobSnapshot{
			ID:     j.ID,
			Status: string(j.Status),
			Total:  len(j.Tasks),
			Done:   done,
			Failed: failed,
			Age:    time.Since(j.CreatedAt),
		})
	}

	// Jaga cursor dalam batas
	if m.cursor >= len(m.nodeSnap) && len(m.nodeSnap) > 0 {
		m.cursor = len(m.nodeSnap) - 1
	}
}

// Init memulai loop tick.
func (m MasterModel) Init() tea.Cmd {
	return tickEvery(time.Second)
}

// Update memproses pesan Bubble Tea.
func (m MasterModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		m.refresh()
		return m, tickEvery(time.Second)

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			m.tab = (m.tab + 1) % 2
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.nodeSnap)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

// View merender seluruh TUI master.
func (m MasterModel) View() string {
	if m.quitting {
		return StyleMuted.Render("distapi master berhenti.\n")
	}

	var b strings.Builder

	alive := 0
	for _, n := range m.nodeSnap {
		if n.Status == "alive" {
			alive++
		}
	}
	running, done, failed := 0, 0, 0
	for _, j := range m.jobSnap {
		switch j.Status {
		case "PROCESSING":
			running++
		case "DONE":
			done++
		case "FAILED":
			failed++
		}
	}

	header := StyleTitle.Render(" distapi  MASTER ") +
		"  " + StyleBold.Render(fmt.Sprintf("%s:%d", m.masterIP, m.grpcPort)) +
		"  " + StyleGreen.Render(fmt.Sprintf("(%d/%d node aktif)", alive, len(m.nodeSnap))) +
		"   " + StyleMuted.Render(fmt.Sprintf("Web UI: http://%s:%d", m.masterIP, m.httpPort)) +
		"   " + StyleMuted.Render(time.Now().Format("15:04:05"))
	b.WriteString(header + "\n")

	workerCmd := fmt.Sprintf("Perintah Laptop Worker: .\\dist\\distapi.exe --mode=node --master=%s:%d --token=%s --tui",
		m.masterIP, m.grpcPort, m.token)
	b.WriteString(StyleMuted.Render("  "+workerCmd) + "\n\n")

	tab0 := "  Node & Job  "
	tab1 := "  Log Aktivitas  "
	if m.tab == 0 {
		tab0 = StyleTitle.Render(tab0)
		tab1 = StyleMuted.Render(tab1)
	} else {
		tab0 = StyleMuted.Render(tab0)
		tab1 = StyleTitle.Render(tab1)
	}
	b.WriteString(tab0 + "  " + tab1 + "\n\n")

	if m.tab == 0 {
		b.WriteString(m.viewNodes())
		b.WriteString("\n")
		b.WriteString(m.viewJobs())
	} else {
		b.WriteString(m.viewLog())
	}

	help := StyleHelp.Width(m.width - 4).Render(
		"[tab] ganti panel   [↑/↓] navigasi node   [q] keluar",
	)
	b.WriteString("\n" + help)

	return b.String()
}

func (m MasterModel) viewNodes() string {
	title := StyleCardTitle.Render("Status Node")

	colW := []int{14, 24, 10, 12, 10, 16}
	header := rowStr(colW,
		StyleBold.Render("Node ID"),
		StyleBold.Render("Alamat"),
		StyleBold.Render("Status"),
		StyleBold.Render("Task Aktif"),
		StyleBold.Render("Kapasitas"),
		StyleBold.Render("Heartbeat"),
	)
	sep := strings.Repeat("-", min(m.width-4, 90))

	var rows []string
	rows = append(rows, title, header, StyleMuted.Render(sep))

	for i, n := range m.nodeSnap {
		hb := "–"
		if !n.LastHB.IsZero() {
			ago := time.Since(n.LastHB)
			hb = FormatDur(ago) + " lalu"
		}
		line := rowStr(colW,
			Trunc(n.NodeID, colW[0]-1),
			Trunc(n.Addr, colW[1]-1),
			Badge(n.Status),
			fmt.Sprintf("%d", n.ActiveTasks),
			fmt.Sprintf("%d", n.Capacity),
			hb,
		)
		if i == m.cursor {
			line = StyleBlue.Render("▶ ") + line
		} else {
			line = "  " + line
		}
		rows = append(rows, line)
	}

	if len(m.nodeSnap) == 0 {
		rows = append(rows, StyleMuted.Render("  Belum ada node terdaftar."))
	}

	return strings.Join(rows, "\n")
}

func (m MasterModel) viewJobs() string {
	title := StyleCardTitle.Render("Daftar Job")

	colW := []int{26, 12, 8, 8, 8, 10}
	header := "  " + rowStr(colW,
		StyleBold.Render("Job ID"),
		StyleBold.Render("Status"),
		StyleBold.Render("Total"),
		StyleBold.Render("Selesai"),
		StyleBold.Render("Gagal"),
		StyleBold.Render("Umur"),
	)
	sep := StyleMuted.Render(strings.Repeat("-", min(m.width-4, 82)))

	var rows []string
	rows = append(rows, title, header, sep)

	limit := 8
	for i, j := range m.jobSnap {
		if i >= limit {
			rows = append(rows, StyleMuted.Render(fmt.Sprintf("  … %d job lainnya", len(m.jobSnap)-limit)))
			break
		}
		rows = append(rows, "  "+rowStr(colW,
			Trunc(j.ID, colW[0]-1),
			Badge(j.Status),
			fmt.Sprintf("%d", j.Total),
			fmt.Sprintf("%d", j.Done),
			fmt.Sprintf("%d", j.Failed),
			FormatDur(j.Age),
		))
	}

	if len(m.jobSnap) == 0 {
		rows = append(rows, StyleMuted.Render("  Belum ada job. Upload gambar lewat web atau curl."))
	}

	return strings.Join(rows, "\n")
}

func (m MasterModel) viewLog() string {
	title := StyleCardTitle.Render("Log Aktivitas Kluster")
	entries := m.log.Snapshot()

	maxLines := m.height - 10
	if maxLines < 5 {
		maxLines = 5
	}
	if len(entries) > maxLines {
		entries = entries[len(entries)-maxLines:]
	}

	var rows []string
	rows = append(rows, title, "")
	if len(entries) == 0 {
		rows = append(rows, StyleMuted.Render("  Belum ada event."))
	}
	for _, e := range entries {
		rows = append(rows, "  "+e.String())
	}

	return strings.Join(rows, "\n")
}

// rowStr menyusun kolom-kolom teks dengan lebar tetap.
func rowStr(widths []int, cols ...string) string {
	var b strings.Builder
	for i, col := range cols {
		w := 12
		if i < len(widths) {
			w = widths[i]
		}
		// Strip ANSI untuk hitung panjang visible, lalu pad
		visible := lipgloss.Width(col)
		pad := w - visible
		if pad < 0 {
			pad = 0
		}
		b.WriteString(col)
		b.WriteString(strings.Repeat(" ", pad))
	}
	return b.String()
}
