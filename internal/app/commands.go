package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Message types for tea.Cmd
type (
	// TickMsg is sent on each clock tick.
	TickMsg time.Time

	// ToastMsg displays a temporary message.
	ToastMsg struct {
		Message  string
		Duration time.Duration
	}

	// RefreshMsg triggers a full refresh.
	RefreshMsg struct{}

	// ErrorMsg represents an error condition.
	ErrorMsg struct {
		Err error
	}
)

// tickCmd returns a command that ticks every second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// ShowToast returns a command to show a toast message.
func ShowToast(msg string, duration time.Duration) tea.Cmd {
	return func() tea.Msg {
		return ToastMsg{
			Message:  msg,
			Duration: duration,
		}
	}
}

// Refresh returns a command to trigger a refresh.
func Refresh() tea.Cmd {
	return func() tea.Msg {
		return RefreshMsg{}
	}
}

// ReportError returns a command to report an error.
func ReportError(err error) tea.Cmd {
	return func() tea.Msg {
		return ErrorMsg{Err: err}
	}
}

// Tick returns a custom tick command with a tag.
func Tick(d time.Duration, tag string) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return TaggedTickMsg{Time: t, Tag: tag}
	})
}

// TaggedTickMsg is a tick with an identifying tag.
type TaggedTickMsg struct {
	Time time.Time
	Tag  string
}
