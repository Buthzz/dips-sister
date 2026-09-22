// Package tui menyediakan antarmuka terminal interaktif menggunakan Bubble Tea.
package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Palet warna ANSI hex untuk komponen visual TUI.
const (
	colGreen  = "#22c55e"
	colRed    = "#ef4444"
	colAmber  = "#f59e0b"
	colBlue   = "#3b82f6"
	colSlate  = "#64748b"
	colWhite  = "#f8fafc"
	colBorder = "#334155"
	colMuted  = "#94a3b8"
)

// Style teks dan elemen antarmuka terminal.
var (
	StyleBold  = lipgloss.NewStyle().Bold(true)
	StyleMuted = lipgloss.NewStyle().Foreground(lipgloss.Color(colMuted))
	StyleGreen = lipgloss.NewStyle().Foreground(lipgloss.Color(colGreen))
	StyleRed   = lipgloss.NewStyle().Foreground(lipgloss.Color(colRed))
	StyleAmber = lipgloss.NewStyle().Foreground(lipgloss.Color(colAmber))
	StyleBlue  = lipgloss.NewStyle().Foreground(lipgloss.Color(colBlue))

	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colWhite)).
			Background(lipgloss.Color(colBlue)).
			Padding(0, 1)

	StyleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colBorder)).
			Padding(0, 1)

	StyleCardTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colBlue))

	StyleBadgeGreen = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colGreen))
	StyleBadgeRed   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colRed))
	StyleBadgeAmber = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colAmber))
	StyleBadgeSlate = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colSlate))

	StyleHelp = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colMuted)).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colBorder)).
			MarginTop(1).
			PaddingTop(1)

	StyleLogTime = lipgloss.NewStyle().Foreground(lipgloss.Color(colSlate))
)

// Badge mengembalikan teks status berwarna sesuai nilainya.
func Badge(status string) string {
	switch status {
	case "alive", "DONE":
		return StyleBadgeGreen.Render("● " + status)
	case "dead", "FAILED":
		return StyleBadgeRed.Render("● " + status)
	case "RUNNING", "PROCESSING":
		return StyleBadgeAmber.Render("◉ " + status)
	default:
		return StyleBadgeSlate.Render("○ " + status)
	}
}

// FormatDur memformat durasi menjadi string ringkas.
func FormatDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// Trunc memotong string agar tidak melebihi lebar tertentu.
func Trunc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
