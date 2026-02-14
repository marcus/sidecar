package event

import "time"

// Event represents a typed event in the system.
type Event struct {
	Type      Type
	Topic     string
	Timestamp time.Time
	Data      any
}

// Type identifies the kind of event.
type Type string

const (
	// File change events
	TypeFileChanged Type = "file_changed"
	TypeGitChanged  Type = "git_changed"
	TypeSessionFile Type = "session_file"

	// Data update events
	TypeTDUpdate      Type = "td_update"
	TypeSessionUpdate Type = "session_update"

	// UI events
	TypeFocusChanged  Type = "focus_changed"
	TypeRefreshNeeded Type = "refresh_needed"

	// Error events
	TypeError Type = "error"
)

// NewEvent creates a new event with the current timestamp.
func NewEvent(t Type, topic string, data any) Event {
	return Event{
		Type:      t,
		Topic:     topic,
		Timestamp: time.Now(),
		Data:      data,
	}
}
