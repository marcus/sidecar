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
	pluginName = "conversations"
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
	ViewAnalytics
	ViewMessageDetail
)

// FocusPane represents which pane is active in two-pane mode.
type FocusPane int

const (
	PaneSidebar FocusPane = iota
	PaneMessages
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
	selectedSession  string
	messages         []adapter.Message
	msgCursor        int
	msgScrollOff     int
	pageSize         int
	hasMore          bool
	expandedThinking map[string]bool // message ID -> thinking expanded
	sessionSummary   *SessionSummary // computed summary for current session
	showToolSummary  bool            // toggle for tool impact view

	// Message detail view state
	detailMessage *adapter.Message
	detailScroll  int

	// Analytics view state
	analyticsScrollOff int
	analyticsLines     []string // pre-rendered lines for scrolling

	// Two-pane layout state
	twoPane      bool      // Enable when width >= 120
	activePane   FocusPane // Which pane is focused
	sidebarWidth int       // Calculated width (~30%)

	// View dimensions
	width  int
	height int

	// Watcher channel
	watchChan <-chan adapter.Event

	// Search state
	searchMode    bool
	searchQuery   string
	searchResults []adapter.Session

	// Filter state
	filterMode   bool
	filters      SearchFilters
	filterActive bool // true when any filter is active
}

// New creates a new conversations plugin.
func New() *Plugin {
	return &Plugin{
		pageSize:         defaultPageSize,
		expandedThinking: make(map[string]bool),
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
		switch p.view {
		case ViewMessageDetail:
			return p.updateMessageDetail(msg)
		case ViewMessages:
			return p.updateMessages(msg)
		case ViewAnalytics:
			return p.updateAnalytics(msg)
		default:
			// In two-pane mode, route based on active pane
			if p.twoPane && p.activePane == PaneMessages {
				return p.updateMessages(msg)
			}
			return p.updateSessions(msg)
		}

	case SessionsLoadedMsg:
		p.sessions = msg.Sessions
		return p, nil

	case MessagesLoadedMsg:
		p.messages = msg.Messages
		p.hasMore = len(msg.Messages) >= p.pageSize
		// Compute session summary
		var duration time.Duration
		for _, s := range p.sessions {
			if s.ID == p.selectedSession {
				duration = s.Duration
				break
			}
		}
		summary := ComputeSessionSummary(msg.Messages, duration)
		p.sessionSummary = &summary
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

	// Handle filter mode input
	if p.filterMode {
		return p.updateFilter(msg)
	}

	sessions := p.visibleSessions()

	switch msg.String() {
	case "j", "down":
		if p.cursor < len(sessions)-1 {
			p.cursor++
			p.ensureCursorVisible()
			// In two-pane mode, auto-load messages when cursor moves
			if p.twoPane && p.cursor < len(sessions) {
				p.selectedSession = sessions[p.cursor].ID
				return p, tea.Batch(
					p.loadMessages(p.selectedSession),
					p.loadUsage(p.selectedSession),
				)
			}
		}

	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisible()
			// In two-pane mode, auto-load messages when cursor moves
			if p.twoPane && p.cursor < len(sessions) {
				p.selectedSession = sessions[p.cursor].ID
				return p, tea.Batch(
					p.loadMessages(p.selectedSession),
					p.loadUsage(p.selectedSession),
				)
			}
		}

	case "g":
		p.cursor = 0
		p.scrollOff = 0
		// In two-pane mode, auto-load messages when jumping
		if p.twoPane && len(sessions) > 0 {
			p.selectedSession = sessions[0].ID
			return p, tea.Batch(
				p.loadMessages(p.selectedSession),
				p.loadUsage(p.selectedSession),
			)
		}

	case "G":
		if len(sessions) > 0 {
			p.cursor = len(sessions) - 1
			p.ensureCursorVisible()
			// In two-pane mode, auto-load messages when jumping
			if p.twoPane {
				p.selectedSession = sessions[p.cursor].ID
				return p, tea.Batch(
					p.loadMessages(p.selectedSession),
					p.loadUsage(p.selectedSession),
				)
			}
		}

	case "ctrl+d":
		// Page down
		pageSize := 10
		if p.cursor+pageSize < len(sessions) {
			p.cursor += pageSize
		} else {
			p.cursor = len(sessions) - 1
		}
		p.ensureCursorVisible()
		if p.twoPane && p.cursor < len(sessions) {
			p.selectedSession = sessions[p.cursor].ID
			return p, tea.Batch(
				p.loadMessages(p.selectedSession),
				p.loadUsage(p.selectedSession),
			)
		}

	case "ctrl+u":
		// Page up
		pageSize := 10
		if p.cursor-pageSize >= 0 {
			p.cursor -= pageSize
		} else {
			p.cursor = 0
		}
		p.ensureCursorVisible()
		if p.twoPane && p.cursor < len(sessions) {
			p.selectedSession = sessions[p.cursor].ID
			return p, tea.Batch(
				p.loadMessages(p.selectedSession),
				p.loadUsage(p.selectedSession),
			)
		}

	case "tab":
		// In two-pane mode, toggle focus between sidebar and messages
		if p.twoPane && p.selectedSession != "" {
			p.activePane = PaneMessages
		}

	case "l", "right":
		// In two-pane mode, switch focus to messages pane
		if p.twoPane && p.selectedSession != "" {
			p.activePane = PaneMessages
		}

	case "enter":
		if len(sessions) > 0 && p.cursor < len(sessions) {
			p.selectedSession = sessions[p.cursor].ID
			// In two-pane mode, switch focus to messages pane
			if p.twoPane {
				p.activePane = PaneMessages
				return p, tea.Batch(
					p.loadMessages(p.selectedSession),
					p.loadUsage(p.selectedSession),
				)
			}
			// In single-pane mode, switch view
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

	case "f":
		// Open filter menu
		p.filterMode = true

	case "r":
		return p, p.loadSessions()

	case "U":
		// Toggle global analytics view
		p.view = ViewAnalytics
		return p, nil
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

	// Apply filters if active
	if p.filterActive && p.filters.IsActive() {
		var filtered []adapter.Session
		for _, s := range p.sessions {
			if p.filters.Matches(s) {
				filtered = append(filtered, s)
			}
		}
		return filtered
	}

	return p.sessions
}

// updateAnalytics handles key events in analytics view.
func (p *Plugin) updateAnalytics(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	// Calculate max scroll based on content
	maxScroll := len(p.analyticsLines) - (p.height - 2)
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch msg.String() {
	case "esc", "q", "U":
		p.view = ViewSessions
		p.analyticsScrollOff = 0

	case "j", "down":
		if p.analyticsScrollOff < maxScroll {
			p.analyticsScrollOff++
		}

	case "k", "up":
		if p.analyticsScrollOff > 0 {
			p.analyticsScrollOff--
		}

	case "g":
		p.analyticsScrollOff = 0

	case "G":
		p.analyticsScrollOff = maxScroll

	case "ctrl+d":
		p.analyticsScrollOff += 10
		if p.analyticsScrollOff > maxScroll {
			p.analyticsScrollOff = maxScroll
		}

	case "ctrl+u":
		p.analyticsScrollOff -= 10
		if p.analyticsScrollOff < 0 {
			p.analyticsScrollOff = 0
		}
	}
	return p, nil
}

// updateMessages handles key events in message view.
func (p *Plugin) updateMessages(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		// In two-pane mode, ESC returns focus to sidebar
		if p.twoPane {
			p.activePane = PaneSidebar
			return p, nil
		}
		// In single-pane mode, return to sessions view
		p.view = ViewSessions
		p.messages = nil
		p.selectedSession = ""
		p.expandedThinking = make(map[string]bool) // reset thinking state
		p.sessionSummary = nil
		p.showToolSummary = false

	case "h", "left":
		// In two-pane mode, return focus to sidebar
		if p.twoPane {
			p.activePane = PaneSidebar
			return p, nil
		}

	case "tab":
		// In two-pane mode, toggle focus between sidebar and messages
		if p.twoPane {
			p.activePane = PaneSidebar
			return p, nil
		}

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

	case "T":
		// Toggle thinking block expansion for current message
		if p.msgCursor < len(p.messages) && len(p.messages[p.msgCursor].ThinkingBlocks) > 0 {
			msgID := p.messages[p.msgCursor].ID
			p.expandedThinking[msgID] = !p.expandedThinking[msgID]
		}

	case "t":
		// Toggle tool impact summary
		p.showToolSummary = !p.showToolSummary

	case "enter":
		// Open full message detail view
		if p.msgCursor < len(p.messages) {
			p.detailMessage = &p.messages[p.msgCursor]
			p.detailScroll = 0
			p.view = ViewMessageDetail
		}

	case "c":
		// Copy session to clipboard as markdown
		if p.selectedSession != "" {
			return p, p.copySessionToClipboard()
		}

	case "e":
		// Export session to file
		if p.selectedSession != "" {
			return p, p.exportSessionToFile()
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

	// Enable two-pane for wide terminals (>= 120 columns)
	p.twoPane = width >= 120
	if p.twoPane {
		p.sidebarWidth = width * 30 / 100
		if p.sidebarWidth < 25 {
			p.sidebarWidth = 25
		}
	}

	var content string
	if p.adapter == nil {
		content = renderNoAdapter()
	} else {
		switch p.view {
		case ViewMessages:
			if p.twoPane {
				content = p.renderTwoPane()
			} else {
				content = p.renderMessages()
			}
		case ViewMessageDetail:
			content = p.renderMessageDetail()
		case ViewAnalytics:
			content = p.renderAnalytics()
		default:
			if p.twoPane {
				content = p.renderTwoPane()
			} else {
				content = p.renderSessions()
			}
		}
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
	if p.filterMode {
		return []plugin.Command{
			{ID: "select", Name: "Select", Context: "conversations-filter"},
			{ID: "cancel", Name: "Cancel", Context: "conversations-filter"},
		}
	}
	if p.view == ViewMessageDetail {
		return []plugin.Command{
			{ID: "back", Name: "Back", Context: "message-detail"},
			{ID: "scroll", Name: "Scroll", Context: "message-detail"},
		}
	}
	if p.view == ViewMessages || (p.twoPane && p.activePane == PaneMessages) {
		return []plugin.Command{
			{ID: "back", Name: "Back", Context: "conversation-detail"},
			{ID: "copy", Name: "Copy", Context: "conversation-detail"},
			{ID: "export", Name: "Export", Context: "conversation-detail"},
			{ID: "detail", Name: "Detail", Context: "conversation-detail"},
		}
	}
	if p.view == ViewAnalytics {
		return []plugin.Command{
			{ID: "back", Name: "Back", Context: "analytics"},
		}
	}
	return []plugin.Command{
		{ID: "view-session", Name: "View", Context: "conversations"},
		{ID: "analytics", Name: "Analytics", Context: "conversations"},
		{ID: "search", Name: "Search", Context: "conversations"},
		{ID: "filter", Name: "Filter", Context: "conversations"},
	}
}

// FocusContext returns the current focus context.
func (p *Plugin) FocusContext() string {
	if p.searchMode {
		return "conversations-search"
	}
	if p.filterMode {
		return "conversations-filter"
	}
	switch p.view {
	case ViewMessageDetail:
		return "message-detail"
	case ViewMessages:
		return "conversation-detail"
	case ViewAnalytics:
		return "analytics"
	default:
		if p.twoPane {
			if p.activePane == PaneSidebar {
				return "conversations-sidebar"
			}
			return "conversations-main"
		}
		return "conversations"
	}
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
	var visibleRows int
	if p.twoPane {
		// In two-pane mode: pane height - borders(2) - header(1-2)
		paneHeight := p.height - 2
		visibleRows = paneHeight - 3 // -2 for inner height calc, -1 for header
	} else {
		// Single pane: total height - header lines
		visibleRows = p.height - 4
	}
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
	var visibleRows int
	if p.twoPane {
		// In two-pane mode: pane height - borders(2) - header(4-5)
		paneHeight := p.height - 2
		visibleRows = paneHeight - 6 // Account for header, stats, resume cmd, separator
	} else {
		// Single pane: total height - header lines
		visibleRows = p.height - 4
	}
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

// loadUsage loads usage stats for a session (placeholder for future implementation).
func (p *Plugin) loadUsage(sessionID string) tea.Cmd {
	// Usage is already computed from messages in MessagesLoadedMsg handler
	return nil
}

// updateMessageDetail handles key events in the message detail view.
func (p *Plugin) updateMessageDetail(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		p.view = ViewMessages
		p.detailMessage = nil
		p.detailScroll = 0

	case "j", "down":
		p.detailScroll++

	case "k", "up":
		if p.detailScroll > 0 {
			p.detailScroll--
		}

	case "g":
		p.detailScroll = 0

	case "G":
		// Scroll to bottom - calculate based on content
		p.detailScroll = 100 // Placeholder, will be clamped by renderer

	case "ctrl+d":
		p.detailScroll += 10

	case "ctrl+u":
		p.detailScroll -= 10
		if p.detailScroll < 0 {
			p.detailScroll = 0
		}
	}
	return p, nil
}

// updateFilter handles key events in filter mode.
func (p *Plugin) updateFilter(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.filterMode = false

	case "enter":
		p.filterMode = false
		p.filterActive = p.filters.IsActive()
		p.cursor = 0
		p.scrollOff = 0

	case "1":
		// Toggle model filter: opus
		p.filters.ToggleModel("opus")

	case "2":
		// Toggle model filter: sonnet
		p.filters.ToggleModel("sonnet")

	case "3":
		// Toggle model filter: haiku
		p.filters.ToggleModel("haiku")

	case "t":
		// Toggle date filter: today
		p.filters.SetDateRange("today")

	case "y":
		// Toggle date filter: yesterday
		p.filters.SetDateRange("yesterday")

	case "w":
		// Toggle date filter: week
		p.filters.SetDateRange("week")

	case "a":
		// Toggle active only
		p.filters.ActiveOnly = !p.filters.ActiveOnly

	case "c":
		// Clear all filters
		p.filters = SearchFilters{}
	}
	return p, nil
}

// copySessionToClipboard copies the current session as markdown to clipboard.
func (p *Plugin) copySessionToClipboard() tea.Cmd {
	session := p.findSelectedSession()
	messages := p.messages

	return func() tea.Msg {
		md := ExportSessionAsMarkdown(session, messages)
		if err := CopyToClipboard(md); err != nil {
			return ToastMsg{Message: "Copy failed: " + err.Error(), IsError: true}
		}
		return ToastMsg{Message: "Session copied to clipboard"}
	}
}

// exportSessionToFile exports the current session to a markdown file.
func (p *Plugin) exportSessionToFile() tea.Cmd {
	session := p.findSelectedSession()
	messages := p.messages
	workDir := p.ctx.WorkDir

	return func() tea.Msg {
		filename, err := ExportSessionToFile(session, messages, workDir)
		if err != nil {
			return ToastMsg{Message: "Export failed: " + err.Error(), IsError: true}
		}
		return ToastMsg{Message: "Exported to " + filename}
	}
}

// findSelectedSession returns the currently selected session.
func (p *Plugin) findSelectedSession() *adapter.Session {
	for i := range p.sessions {
		if p.sessions[i].ID == p.selectedSession {
			return &p.sessions[i]
		}
	}
	return nil
}

// Message types
type ToastMsg struct {
	Message string
	IsError bool
}
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
