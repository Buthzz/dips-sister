// Package tui menyediakan model event log ring buffer untuk antarmuka terminal.
package tui

import (
	"fmt"
	"sync"
	"time"
)

// LogLevel menyatakan tingkat keparahan sebuah event.
type LogLevel string

const (
	LogInfo  LogLevel = "INFO"
	LogWarn  LogLevel = "WARN"
	LogError LogLevel = "ERROR"
)

// LogEntry adalah satu baris event yang dicatat.
type LogEntry struct {
	Time    time.Time
	Level   LogLevel
	Message string
}

// String memformat entry menjadi baris teks terminal.
func (e LogEntry) String() string {
	ts := StyleLogTime.Render(e.Time.Format("15:04:05"))
	var lvl string
	switch e.Level {
	case LogWarn:
		lvl = StyleAmber.Render("WARN ")
	case LogError:
		lvl = StyleRed.Render("ERROR")
	default:
		lvl = StyleBlue.Render("INFO ")
	}
	return fmt.Sprintf("%s  %s  %s", ts, lvl, e.Message)
}

// EventLog adalah ring buffer thread-safe untuk menyimpan event kluster.
type EventLog struct {
	mu      sync.Mutex
	entries []LogEntry
	max     int
}

// NewEventLog membuat ring buffer dengan kapasitas max entri.
func NewEventLog(max int) *EventLog {
	return &EventLog{max: max, entries: make([]LogEntry, 0, max)}
}

// Append menambahkan entry baru; menghapus yang paling lama jika penuh.
func (l *EventLog) Append(level LogLevel, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := LogEntry{Time: time.Now(), Level: level, Message: msg}
	if len(l.entries) >= l.max {
		l.entries = l.entries[1:]
	}
	l.entries = append(l.entries, entry)
}

// Info mencatat pesan level INFO.
func (l *EventLog) Info(msg string) { l.Append(LogInfo, msg) }

// Warn mencatat pesan level WARN.
func (l *EventLog) Warn(msg string) { l.Append(LogWarn, msg) }

// Snapshot mengembalikan salinan seluruh entri (terbaru di akhir).
func (l *EventLog) Snapshot() []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LogEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Len mengembalikan jumlah entri yang tersimpan.
func (l *EventLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}
