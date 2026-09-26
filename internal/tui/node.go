// Package tui menyediakan tampilan terminal interaktif untuk mode node.
package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TaskRecord adalah riwayat satu task yang telah diproses.
type TaskRecord struct {
	ID         string
	Filename   string
	Status     string
	DurationMs int64
	DoneAt     time.Time
}

// NodeStateSnapshot adalah salinan data status node untuk pembacaan aman tanpa race condition.
type NodeStateSnapshot struct {
	NodeID         string
	SessionID      string
	MasterAddr     string
	AdvertisedAddr string
	Connected      bool
	LastHB         time.Time
	HBCount        int64
	ActiveTasks    int
	DoneTasks      int
	FailedTasks    int
	TaskHistory    []TaskRecord
}

// NodeState menyimpan status runtime node yang aman diakses bersamaan (thread-safe).
type NodeState struct {
	mu             sync.RWMutex
	nodeID         string
	sessionID      string
	masterAddr     string
	advertisedAddr string
	connected      bool
	lastHB         time.Time
	hbCount        int64
	activeTasks    int
	doneTasks      int
	failedTasks    int
	taskHistory    []TaskRecord
}

// NewNodeState membuat instance NodeState baru.
func NewNodeState(nodeID, sessionID, masterAddr, advAddr string) *NodeState {
	return &NodeState{
		nodeID:         nodeID,
		sessionID:      sessionID,
		masterAddr:     masterAddr,
		advertisedAddr: advAddr,
	}
}

// SetNodeID memperbarui identitas unik node setelah dialokasikan oleh master.
func (s *NodeState) SetNodeID(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeID = nodeID
}

// SetConnected memperbarui status konektivitas ke master.
func (s *NodeState) SetConnected(connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = connected
}

// RecordHeartbeatSuccess mencatat pengiriman heartbeat yang berhasil.
func (s *NodeState) RecordHeartbeatSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = true
	s.lastHB = time.Now()
	s.hbCount++
}

// RecordHeartbeatFailure mencatat kegagalan pengiriman heartbeat.
func (s *NodeState) RecordHeartbeatFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = false
}

// OnTaskStart mencatat dimulainya pemrosesan sebuah task.
func (s *NodeState) OnTaskStart(taskID, filename string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeTasks++
}

// OnTaskComplete mencatat penyelesaian pemrosesan task.
func (s *NodeState) OnTaskComplete(taskID, filename string, success bool, durationMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeTasks > 0 {
		s.activeTasks--
	}
	status := "DONE"
	if success {
		s.doneTasks++
	} else {
		s.failedTasks++
		status = "FAILED"
	}
	record := TaskRecord{
		ID:         taskID,
		Filename:   filename,
		Status:     status,
		DurationMs: durationMs,
		DoneAt:     time.Now(),
	}
	s.taskHistory = append(s.taskHistory, record)
	if len(s.taskHistory) > 20 {
		s.taskHistory = s.taskHistory[len(s.taskHistory)-20:]
	}
}

// ActiveTaskCount mengembalikan jumlah task aktif secara aman.
func (s *NodeState) ActiveTaskCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeTasks
}

// Snapshot mengembalikan salinan state untuk dibaca dengan aman oleh model TUI.
func (s *NodeState) Snapshot() NodeStateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hist := make([]TaskRecord, len(s.taskHistory))
	copy(hist, s.taskHistory)
	return NodeStateSnapshot{
		NodeID:         s.nodeID,
		SessionID:      s.sessionID,
		MasterAddr:     s.masterAddr,
		AdvertisedAddr: s.advertisedAddr,
		Connected:      s.connected,
		LastHB:         s.lastHB,
		HBCount:        s.hbCount,
		ActiveTasks:    s.activeTasks,
		DoneTasks:      s.doneTasks,
		FailedTasks:    s.failedTasks,
		TaskHistory:    hist,
	}
}

// NodeModel adalah Bubble Tea model untuk mode node.
type NodeModel struct {
	state    *NodeState
	log      *EventLog
	width    int
	height   int
	quitting bool
}

// NewNodeModel membuat model TUI untuk mode node.
func NewNodeModel(state *NodeState, log *EventLog) NodeModel {
	return NodeModel{
		state:  state,
		log:    log,
		width:  100,
		height: 32,
	}
}

// Init memulai loop tick setiap detik.
func (m NodeModel) Init() tea.Cmd {
	return tickEvery(time.Second)
}

// Update memproses pesan Bubble Tea.
func (m NodeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		return m, tickEvery(time.Second)
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// View merender seluruh TUI node.
func (m NodeModel) View() string {
	if m.quitting {
		return StyleMuted.Render("distapi node berhenti.\n")
	}

	s := m.state.Snapshot()
	var b strings.Builder

	connStatus := StyleRed.Render("● Terputus")
	if s.Connected {
		connStatus = StyleGreen.Render("● Terhubung")
	}
	header := StyleTitle.Render(" distapi  NODE ") +
		"  " + StyleBold.Render(s.NodeID) +
		"   " + connStatus +
		"   " + StyleMuted.Render(time.Now().Format("15:04:05"))
	b.WriteString(header + "\n\n")

	b.WriteString(StyleCardTitle.Render("Informasi Node") + "\n")

	infoRows := [][]string{
		{"Node ID", s.NodeID},
		{"Session", s.SessionID},
		{"Master", s.MasterAddr},
		{"Advertise", s.AdvertisedAddr},
		{"Heartbeat terakhir", fmtHB(s.LastHB)},
		{"Total HB terkirim", fmt.Sprintf("%d", s.HBCount)},
	}
	for _, row := range infoRows {
		b.WriteString(fmt.Sprintf("  %-22s %s\n",
			StyleMuted.Render(row[0]+":"),
			row[1],
		))
	}
	b.WriteString("\n")

	b.WriteString(StyleCardTitle.Render("Statistik Task") + "\n")
	b.WriteString(fmt.Sprintf("  %-22s %s\n",
		StyleMuted.Render("Sedang berjalan:"),
		StyleAmber.Render(fmt.Sprintf("%d", s.ActiveTasks)),
	))
	b.WriteString(fmt.Sprintf("  %-22s %s\n",
		StyleMuted.Render("Selesai (sesi ini):"),
		StyleGreen.Render(fmt.Sprintf("%d", s.DoneTasks)),
	))
	b.WriteString(fmt.Sprintf("  %-22s %s\n",
		StyleMuted.Render("Gagal (sesi ini):"),
		StyleRed.Render(fmt.Sprintf("%d", s.FailedTasks)),
	))
	b.WriteString("\n")

	b.WriteString(StyleCardTitle.Render("Riwayat Task Terakhir") + "\n")

	colW := []int{28, 30, 10, 10}
	header2 := "  " + rowStr(colW,
		StyleBold.Render("Task ID"),
		StyleBold.Render("File"),
		StyleBold.Render("Status"),
		StyleBold.Render("Durasi"),
	)
	b.WriteString(header2 + "\n")
	b.WriteString("  " + StyleMuted.Render(strings.Repeat("-", 80)) + "\n")

	hist := s.TaskHistory
	maxHist := 6
	if m.height < 32 {
		maxHist = 3
	}
	if len(hist) > maxHist {
		hist = hist[len(hist)-maxHist:]
	}
	if len(hist) == 0 {
		b.WriteString(StyleMuted.Render("  Belum ada task selesai diproses.\n"))
	}
	for _, t := range hist {
		b.WriteString("  " + rowStr(colW,
			Trunc(t.ID, colW[0]-1),
			Trunc(t.Filename, colW[1]-1),
			Badge(t.Status),
			fmt.Sprintf("%dms", t.DurationMs),
		) + "\n")
	}
	b.WriteString("\n")

	b.WriteString(StyleCardTitle.Render("Log Node") + "\n")
	entries := m.log.Snapshot()
	maxLines := m.height - 25
	if maxLines < 3 {
		maxLines = 3
	}
	if len(entries) > maxLines {
		entries = entries[len(entries)-maxLines:]
	}
	if len(entries) == 0 {
		b.WriteString(StyleMuted.Render("  Belum ada event.\n"))
	}
	for _, e := range entries {
		b.WriteString("  " + e.String() + "\n")
	}

	b.WriteString(StyleHelp.Width(m.width - 4).Render("[q] keluar"))

	return b.String()
}

func fmtHB(t time.Time) string {
	if t.IsZero() {
		return StyleMuted.Render("belum ada")
	}
	ago := time.Since(t)
	return fmt.Sprintf("%s (%s lalu)", t.Format("15:04:05"), FormatDur(ago))
}
