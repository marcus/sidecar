package conversations

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sst/sidecar/internal/adapter"
	"github.com/sst/sidecar/internal/plugin"
)

const (
	pluginID   = "conversations"
	pluginName = "Conversations"
	pluginIcon = "C"

	// Default page size for messages
	defaultPageSize = 50
	maxMessagesInMemory = 500
)

// View represents the current view mode.
type View int

const (
	ViewSessions View = iota
	ViewMessages
)

// Plugin implements the conversations plugin.
type Plugin struct {
	ctx     *plugin.Context
	adapter adapter.Adapter
	focused bool

	// Current view
	view View

	// Session list state
	sessions  []adapter.Session
	cursor    int
	scrollOff int

	// Message view state
	selectedSession string
	messages        []adapter.Message
	msgCursor       int
	msgScrollOff    int
	pageSize        int
	hasMore         bool

	// View dimensions
	width  int
	height int

	// Watcher channel
	watchChan <-chan adapter.Event
}

// New creates a new conversations plugin.
func New() *Plugin {
	return &Plugin{
		pageSize: defaultPageSize,
	}
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return pluginID }

// Name returns the plugin display name.
func (p *Plugin) Name() string { return pluginName }

// Icon returns the plugin icon character.
func (p *Plugin) Icon() string { return pluginIcon }

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	p.ctx = ctx

	// Get Claude Code adapter
	if a, ok := ctx.Adapters["claude-code"]; ok {
		p.adapter = a
	} else {
		return nil // No adapter, silent degradation
	}

	// Check if adapter can detect this project
	found, err := p.adapter.Detect(ctx.WorkDir)
	if err != nil || !found {
		return nil // No sessions for this project
	}

	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	if p.adapter == nil {
		return nil
	}

	return tea.Batch(
		p.loadSessions(),
		p.startWatcher(),
	)
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	// Watcher cleanup handled by adapter
}

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if p.view == ViewMessages {
			return p.updateMessages(msg)
		}
		return p.updateSessions(msg)

	case SessionsLoadedMsg:
		p.sessions = msg.Sessions
		return p, nil

	case MessagesLoadedMsg:
		p.messages = msg.Messages
		p.hasMore = len(msg.Messages) >= p.pageSize
		return p, nil

	case WatchEventMsg:
		return p, p.loadSessions()

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
	}

	return p, nil
}

// updateSessions handles key events in session list view.
func (p *Plugin) updateSessions(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if p.cursor < len(p.sessions)-1 {
			p.cursor++
			p.ensureCursorVisible()
		}

	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisible()
		}

	case "g":
		p.cursor = 0
		p.scrollOff = 0

	case "G":
		if len(p.sessions) > 0 {
			p.cursor = len(p.sessions) - 1
			p.ensureCursorVisible()
		}

	case "enter":
		if len(p.sessions) > 0 && p.cursor < len(p.sessions) {
			p.selectedSession = p.sessions[p.cursor].ID
			p.view = ViewMessages
			p.msgCursor = 0
			p.msgScrollOff = 0
			return p, p.loadMessages(p.selectedSession)
		}

	case "r":
		return p, p.loadSessions()
	}

	return p, nil
}

// updateMessages handles key events in message view.
func (p *Plugin) updateMessages(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		p.view = ViewSessions
		p.messages = nil
		p.selectedSession = ""

	case "j", "down":
		if p.msgCursor < len(p.messages)-1 {
			p.msgCursor++
			p.ensureMsgCursorVisible()
		}

	case "k", "up":
		if p.msgCursor > 0 {
			p.msgCursor--
			p.ensureMsgCursorVisible()
		}

	case "g":
		p.msgCursor = 0
		p.msgScrollOff = 0

	case "G":
		if len(p.messages) > 0 {
			p.msgCursor = len(p.messages) - 1
			p.ensureMsgCursorVisible()
		}

	case " ":
		// Load more messages (would need to implement paging in adapter)
		return p, nil
	}

	return p, nil
}

// View renders the plugin.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height

	if p.adapter == nil {
		return renderNoAdapter()
	}

	if p.view == ViewMessages {
		return p.renderMessages()
	}

	return p.renderSessions()
}

// IsFocused returns whether the plugin is focused.
func (p *Plugin) IsFocused() bool { return p.focused }

// SetFocused sets the focus state.
func (p *Plugin) SetFocused(f bool) { p.focused = f }

// Commands returns the available commands.
func (p *Plugin) Commands() []plugin.Command {
	return []plugin.Command{
		{ID: "view-session", Name: "View session", Context: "conversations"},
		{ID: "back", Name: "Back to sessions", Context: "conversation-detail"},
	}
}

// FocusContext returns the current focus context.
func (p *Plugin) FocusContext() string {
	if p.view == ViewMessages {
		return "conversation-detail"
	}
	return "conversations"
}

// Diagnostics returns plugin health info.
func (p *Plugin) Diagnostics() []plugin.Diagnostic {
	status := "ok"
	detail := ""
	if p.adapter == nil {
		status = "disabled"
		detail = "no adapter"
	} else if len(p.sessions) == 0 {
		status = "empty"
		detail = "no sessions"
	} else {
		detail = formatSessionCount(len(p.sessions))
	}
	return []plugin.Diagnostic{
		{ID: "conversations", Status: status, Detail: detail},
	}
}

// loadSessions loads sessions from the adapter.
func (p *Plugin) loadSessions() tea.Cmd {
	return func() tea.Msg {
		if p.adapter == nil {
			return SessionsLoadedMsg{}
		}
		sessions, err := p.adapter.Sessions(p.ctx.WorkDir)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return SessionsLoadedMsg{Sessions: sessions}
	}
}

// loadMessages loads messages for a session.
func (p *Plugin) loadMessages(sessionID string) tea.Cmd {
	return func() tea.Msg {
		if p.adapter == nil {
			return MessagesLoadedMsg{}
		}
		messages, err := p.adapter.Messages(sessionID)
		if err != nil {
			return ErrorMsg{Err: err}
		}

		// Limit to last N messages
		if len(messages) > maxMessagesInMemory {
			messages = messages[len(messages)-maxMessagesInMemory:]
		}

		return MessagesLoadedMsg{Messages: messages}
	}
}

// startWatcher starts watching for session changes.
func (p *Plugin) startWatcher() tea.Cmd {
	return func() tea.Msg {
		if p.adapter == nil {
			return nil
		}
		ch, err := p.adapter.Watch(p.ctx.WorkDir)
		if err != nil {
			return nil
		}
		p.watchChan = ch
		return WatchStartedMsg{}
	}
}

// ensureCursorVisible adjusts scroll to keep cursor visible.
func (p *Plugin) ensureCursorVisible() {
	visibleRows := p.height - 6
	if visibleRows < 1 {
		visibleRows = 1
	}

	if p.cursor < p.scrollOff {
		p.scrollOff = p.cursor
	} else if p.cursor >= p.scrollOff+visibleRows {
		p.scrollOff = p.cursor - visibleRows + 1
	}
}

// ensureMsgCursorVisible adjusts scroll to keep message cursor visible.
func (p *Plugin) ensureMsgCursorVisible() {
	visibleRows := p.height - 6
	if visibleRows < 1 {
		visibleRows = 1
	}

	if p.msgCursor < p.msgScrollOff {
		p.msgScrollOff = p.msgCursor
	} else if p.msgCursor >= p.msgScrollOff+visibleRows {
		p.msgScrollOff = p.msgCursor - visibleRows + 1
	}
}

// formatSessionCount formats a session count.
func formatSessionCount(n int) string {
	if n == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", n)
}

// Message types
type SessionsLoadedMsg struct {
	Sessions []adapter.Session
}

type MessagesLoadedMsg struct {
	Messages []adapter.Message
}

type WatchEventMsg struct{}
type WatchStartedMsg struct{}
type ErrorMsg struct{ Err error }

// TickCmd returns a command that triggers periodic refresh.
func TickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return WatchEventMsg{}
	})
}
