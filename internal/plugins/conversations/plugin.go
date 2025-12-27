package conversations

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sst/sidecar/internal/adapter"
	"github.com/sst/sidecar/internal/plugin"
)

const (
	pluginID   = "conversations"
	pluginName = "Conversations"
	pluginIcon = "C"

	// Default page size for messages
	defaultPageSize     = 50
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

	// Search state
	searchMode    bool
	searchQuery   string
	searchResults []adapter.Session
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
		return nil
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

	case WatchStartedMsg:
		// Watcher started, store channel and start listening
		if msg.Channel == nil {
			return p, nil // Watcher failed
		}
		p.watchChan = msg.Channel
		return p, p.listenForWatchEvents()

	case WatchEventMsg:
		// Session data changed, refresh and continue listening
		return p, tea.Batch(
			p.loadSessions(),
			p.listenForWatchEvents(),
		)

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
	}

	return p, nil
}

// updateSessions handles key events in session list view.
func (p *Plugin) updateSessions(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	// Handle search mode input
	if p.searchMode {
		return p.updateSearch(msg)
	}

	sessions := p.visibleSessions()

	switch msg.String() {
	case "j", "down":
		if p.cursor < len(sessions)-1 {
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
		if len(sessions) > 0 {
			p.cursor = len(sessions) - 1
			p.ensureCursorVisible()
		}

	case "enter":
		if len(sessions) > 0 && p.cursor < len(sessions) {
			p.selectedSession = sessions[p.cursor].ID
			p.view = ViewMessages
			p.msgCursor = 0
			p.msgScrollOff = 0
			return p, p.loadMessages(p.selectedSession)
		}

	case "/":
		p.searchMode = true
		p.searchQuery = ""
		p.cursor = 0
		p.scrollOff = 0

	case "r":
		return p, p.loadSessions()
	}

	return p, nil
}

// updateSearch handles key events in search mode.
func (p *Plugin) updateSearch(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.searchMode = false
		p.searchQuery = ""
		p.searchResults = nil
		p.cursor = 0
		p.scrollOff = 0

	case "enter":
		sessions := p.visibleSessions()
		if len(sessions) > 0 && p.cursor < len(sessions) {
			p.selectedSession = sessions[p.cursor].ID
			p.view = ViewMessages
			p.msgCursor = 0
			p.msgScrollOff = 0
			p.searchMode = false
			return p, p.loadMessages(p.selectedSession)
		}

	case "backspace":
		if len(p.searchQuery) > 0 {
			p.searchQuery = p.searchQuery[:len(p.searchQuery)-1]
			p.filterSessions()
			p.cursor = 0
			p.scrollOff = 0
		}

	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisible()
		}

	case "down", "ctrl+n":
		sessions := p.visibleSessions()
		if p.cursor < len(sessions)-1 {
			p.cursor++
			p.ensureCursorVisible()
		}

	default:
		// Add character to search query
		if len(msg.String()) == 1 {
			p.searchQuery += msg.String()
			p.filterSessions()
			p.cursor = 0
			p.scrollOff = 0
		}
	}

	return p, nil
}

// filterSessions filters sessions based on search query.
func (p *Plugin) filterSessions() {
	if p.searchQuery == "" {
		p.searchResults = nil
		return
	}

	query := strings.ToLower(p.searchQuery)
	var results []adapter.Session
	for _, s := range p.sessions {
		if strings.Contains(strings.ToLower(s.Name), query) ||
			strings.Contains(strings.ToLower(s.Slug), query) ||
			strings.Contains(s.ID, query) {
			results = append(results, s)
		}
	}
	p.searchResults = results
}

// visibleSessions returns sessions to display (filtered or all).
func (p *Plugin) visibleSessions() []adapter.Session {
	if p.searchMode && p.searchQuery != "" {
		return p.searchResults
	}
	return p.sessions
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

	var content string
	if p.adapter == nil {
		content = renderNoAdapter()
	} else if p.view == ViewMessages {
		content = p.renderMessages()
	} else {
		content = p.renderSessions()
	}

	// Constrain output to allocated height to prevent header scrolling off-screen.
	// MaxHeight truncates content that exceeds the allocated space.
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
}

// IsFocused returns whether the plugin is focused.
func (p *Plugin) IsFocused() bool { return p.focused }

// SetFocused sets the focus state.
func (p *Plugin) SetFocused(f bool) { p.focused = f }

// Commands returns the available commands.
func (p *Plugin) Commands() []plugin.Command {
	if p.searchMode {
		return []plugin.Command{
			{ID: "select", Name: "Select", Context: "conversations-search"},
			{ID: "cancel", Name: "Cancel", Context: "conversations-search"},
		}
	}
	if p.view == ViewMessages {
		return []plugin.Command{
			{ID: "back", Name: "Back", Context: "conversation-detail"},
			{ID: "scroll", Name: "Scroll", Context: "conversation-detail"},
		}
	}
	return []plugin.Command{
		{ID: "view-session", Name: "View", Context: "conversations"},
		{ID: "search", Name: "Search", Context: "conversations"},
		{ID: "refresh", Name: "Refresh", Context: "conversations"},
	}
}

// FocusContext returns the current focus context.
func (p *Plugin) FocusContext() string {
	if p.searchMode {
		return "conversations-search"
	}
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
		// Add active session count
		active := 0
		for _, s := range p.sessions {
			if s.IsActive {
				active++
			}
		}
		if active > 0 {
			detail = fmt.Sprintf("%s (%d active)", detail, active)
		}
	}

	// Add watcher status
	watchStatus := "off"
	if p.watchChan != nil {
		watchStatus = "on"
	}

	return []plugin.Diagnostic{
		{ID: "conversations", Status: status, Detail: detail},
		{ID: "watcher", Status: watchStatus, Detail: "fsnotify"},
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
			return WatchStartedMsg{Channel: nil}
		}
		ch, err := p.adapter.Watch(p.ctx.WorkDir)
		if err != nil {
			return WatchStartedMsg{Channel: nil}
		}
		return WatchStartedMsg{Channel: ch}
	}
}

// listenForWatchEvents waits for the next file system event.
func (p *Plugin) listenForWatchEvents() tea.Cmd {
	if p.watchChan == nil {
		return nil
	}
	return func() tea.Msg {
		_, ok := <-p.watchChan
		if !ok {
			// Channel closed
			return nil
		}
		return WatchEventMsg{}
	}
}

// ensureCursorVisible adjusts scroll to keep cursor visible.
func (p *Plugin) ensureCursorVisible() {
	visibleRows := p.height - 2
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
	visibleRows := p.height - 2
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

// shortID returns the first 8 characters of an ID, or the full ID if shorter.
func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// Message types
type SessionsLoadedMsg struct {
	Sessions []adapter.Session
}

type MessagesLoadedMsg struct {
	Messages []adapter.Message
}

type WatchEventMsg struct{}
type WatchStartedMsg struct {
	Channel <-chan adapter.Event
}
type ErrorMsg struct{ Err error }

// TickCmd returns a command that triggers periodic refresh.
func TickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return WatchEventMsg{}
	})
}
