package tui_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"distapi/internal/registry"
	"distapi/internal/scheduler"
	"distapi/internal/tui"
)

type mockNodeLister struct {
	nodes []registry.NodeInfo
}

func (m *mockNodeLister) AllNodes() []registry.NodeInfo {
	return m.nodes
}

type mockJobLister struct {
	jobs []*scheduler.Job
}

func (m *mockJobLister) ListJobs() []*scheduler.Job {
	return m.jobs
}

func TestEventLog(t *testing.T) {
	el := tui.NewEventLog(3)
	el.Info("pesan 1")
	el.Warn("pesan 2")
	el.Append(tui.LogError, "pesan 3")
	el.Info("pesan 4 (evicts 1)")

	entries := el.Snapshot()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Message != "pesan 2" {
		t.Errorf("expected first entry to be 'pesan 2', got %q", entries[0].Message)
	}
	if entries[2].Message != "pesan 4 (evicts 1)" {
		t.Errorf("expected last entry to be 'pesan 4', got %q", entries[2].Message)
	}
}

func TestEventLog_Concurrent(t *testing.T) {
	el := tui.NewEventLog(50)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			el.Info("event dari goroutine")
			_ = el.Snapshot()
		}(i)
	}
	wg.Wait()
	if el.Len() == 0 {
		t.Fatal("expected entries in event log")
	}
}

func TestNodeState(t *testing.T) {
	state := tui.NewNodeState("node-1", "sess-1", "127.0.0.1:9000", "127.0.0.1:9001")

	state.SetConnected(true)
	state.RecordHeartbeatSuccess()

	state.OnTaskStart("task-01", "img1.jpg")
	if count := state.ActiveTaskCount(); count != 1 {
		t.Fatalf("expected 1 active task, got %d", count)
	}

	state.OnTaskComplete("task-01", "img1.jpg", true, 45)
	if count := state.ActiveTaskCount(); count != 0 {
		t.Fatalf("expected 0 active tasks, got %d", count)
	}

	snap := state.Snapshot()
	if !snap.Connected {
		t.Error("expected connected = true")
	}
	if snap.DoneTasks != 1 {
		t.Errorf("expected 1 done task, got %d", snap.DoneTasks)
	}
	if len(snap.TaskHistory) != 1 {
		t.Fatalf("expected 1 task history, got %d", len(snap.TaskHistory))
	}
	if snap.TaskHistory[0].ID != "task-01" {
		t.Errorf("expected task-01, got %s", snap.TaskHistory[0].ID)
	}
}

func TestMasterModel_Lifecycle(t *testing.T) {
	nl := &mockNodeLister{
		nodes: []registry.NodeInfo{
			{
				NodeID:        "node-1",
				AdvertiseAddr: "192.168.1.11:9000",
				Status:        registry.StatusAlive,
				Capacity:      4,
				ActiveTasks:   1,
				LastHeartbeat: time.Now(),
			},
		},
	}
	jl := &mockJobLister{
		jobs: []*scheduler.Job{
			{
				ID:        "job_abc",
				Status:    scheduler.JobProcessing,
				CreatedAt: time.Now().Add(-5 * time.Second),
				Tasks: []*scheduler.Task{
					{Status: scheduler.TaskDone},
					{Status: scheduler.TaskRunning},
				},
			},
		},
	}
	el := tui.NewEventLog(10)
	el.Info("master mulai")

	model := tui.NewMasterModel(nl, jl, el, "127.0.0.1", 8080, 9000, "token-test")

	// Inisialisasi
	cmd := model.Init()
	if cmd == nil {
		t.Error("expected non-nil Init command")
	}

	// Update window size
	m, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = m.(tui.MasterModel)

	// Ganti tab
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = m.(tui.MasterModel)

	// Navigasi panah
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = m.(tui.MasterModel)

	// Render view
	view := model.View()
	if !strings.Contains(view, "distapi  MASTER") {
		t.Errorf("view does not contain expected header, got:\n%s", view)
	}
	if !strings.Contains(view, "Log Aktivitas") {
		t.Errorf("view does not contain tab title, got:\n%s", view)
	}
}

func TestNodeModel_Lifecycle(t *testing.T) {
	state := tui.NewNodeState("node-worker-1", "sess-xyz", "192.168.1.10:9000", "192.168.1.12:9000")
	state.RecordHeartbeatSuccess()
	state.OnTaskStart("task-02", "photo.png")
	state.OnTaskComplete("task-02", "photo.png", true, 30)

	el := tui.NewEventLog(10)
	el.Info("terhubung ke master")

	model := tui.NewNodeModel(state, el)

	cmd := model.Init()
	if cmd == nil {
		t.Error("expected non-nil Init command")
	}

	m, _ := model.Update(tea.WindowSizeMsg{Width: 90, Height: 25})
	model = m.(tui.NodeModel)

	view := model.View()
	if !strings.Contains(view, "distapi  NODE") {
		t.Errorf("view does not contain node header, got:\n%s", view)
	}
	if !strings.Contains(view, "node-worker-1") {
		t.Errorf("view does not contain node-id, got:\n%s", view)
	}
	if !strings.Contains(view, "photo.png") {
		t.Errorf("view does not contain task history, got:\n%s", view)
	}
}
