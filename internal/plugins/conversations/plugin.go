package conversations

import (
	"fmt"
	"log"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/state"
)

const (
	pluginID   = "conversations"
	pluginName = "conversations"
	pluginIcon = "C"

	// Default page size for messages
	defaultPageSize     = 50
	maxMessagesInMemory = 500

	previewDebounce     = 150 * time.Millisecond
	watchReloadDebounce = 200 * time.Millisecond

	// Divider width for pane separator
	dividerWidth = 1

	// Hybrid content display thresholds
	ShortMessageCharLimit = 500 // Messages shorter than this display inline
	ShortMessageLineLimit = 13  // Messages with fewer lines display inline
	CollapsedPreviewChars = 300 // Preview length for collapsed messages

	// Worktree cache TTL to avoid repeated git commands (td-e74a4aaa)
	worktreeCacheTTL = 5 * time.Second
)

// Mouse hit region identifiers
const (
	regionSidebar     = "sidebar"
	regionMainPane    = "main-pane"
	regionPaneDivider = "pane-divider"
	regionSessionItem = "session-item" // Individual session row (Data: session index)
	regionTurnItem    = "turn-item"    // Individual turn row (Data: turn index)
	regionMessageItem = "message-item" // Conversation flow: click to select (Data: msg index)
	regionToolExpand  = "tool-expand"  // Conversation flow: toggle tool output (Data: tool_use_id)
	regionShowMore    = "show-more"    // Conversation flow: expand long message (Data: msg ID)
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

// renderCacheKey is used to cache rendered message content (td-8910b218).
type renderCacheKey struct {
	messageID string
	width     int
	expanded  bool // whether content is expanded (affects render)
}

// Plugin implements the conversations plugin.
type Plugin struct {
	ctx          *plugin.Context
	adapters     map[string]adapter.Adapter
	focused      bool
	mouseHandler *mouse.Handler

	// Current view
	view View

	// Session list state
	sessions  []adapter.Session
	cursor    int
	scrollOff int

	// Message view state
	selectedSession string
	loadedSession   string // sessionID that p.messages currently represent
	messages        []adapter.Message
	turns           []Turn // messages grouped into turns
	turnCursor      int    // cursor for turn selection in list view
	turnScrollOff   int    // scroll offset for turns
	msgCursor       int
	msgScrollOff    int
	pageSize        int
	hasMore         bool

	// Pagination state (td-313ea851)
	messageOffset      int             // Start index in full message list (0 = most recent)
	totalMessages      int             // Total message count from adapter
	hasOlderMsgs       bool            // True if there are older messages to load
	expandedThinking   map[string]bool // message ID -> thinking expanded
	sessionSummary     *SessionSummary // computed summary for current session
	summaryModelCounts map[string]int  // model usage counts for incremental summary updates
	summaryFileSet     map[string]bool // unique files for incremental summary updates
	showToolSummary    bool            // toggle for tool impact view
	turnViewMode       bool            // false = conversation flow (default), true = turn view

	// Message detail view state
	detailMode   bool  // true when showing detail in right pane (two-pane mode)
	detailTurn   *Turn // turn being viewed in detail
	detailScroll int

	// Analytics view state
	analyticsScrollOff int
	analyticsLines     []string // pre-rendered lines for scrolling

	// Layout state
	activePane     FocusPane // Which pane is focused
	sidebarRestore FocusPane // Tracks pane focused before collapse; restored on expand via toggleSidebar()
	sidebarWidth   int       // Calculated width (~30%)
	sidebarVisible bool      // Toggle sidebar visibility with \
	previewToken       int       // monotonically increasing token for debounced preview loads
	messageReloadToken int       // monotonically increasing token for debounced watch reloads

	// View dimensions
	width  int
	height int

	// Watcher channel
	watchChan <-chan adapter.Event

	// Event coalescing for watch events
	coalescer    *EventCoalescer
	coalesceChan chan CoalescedRefreshMsg

	// Search state
	searchMode    bool
	searchQuery   string
	searchResults []adapter.Session

	// Filter state
	filterMode   bool
	filters      SearchFilters
	filterActive bool // true when any filter is active

	// Markdown rendering
	contentRenderer *GlamourRenderer

	// Conversation flow view state (Claude Code web UI style)
	expandedMessages    map[string]bool // message ID -> content expanded (for long messages)
	expandedToolResults map[string]bool // tool_use_id -> result expanded
	messageScroll       int             // global scroll offset for conversation view
	messageCursor       int             // selected message index in conversation view

	// Visible message line tracking (populated during render for accurate hit regions)
	visibleMsgRanges []msgLineRange // message index -> visible line range (populated each render)

	// Full message line positions (all rendered messages, before scroll window)
	// Used for accurate scroll calculations in ensureMessageCursorVisible
	msgLinePositions []msgLinePos

	// Render cache for message content (td-8910b218)
	renderCache      map[renderCacheKey]string
	renderCacheMutex sync.RWMutex

	// Hit region optimization (td-ea784b03)
	hitRegionsDirty bool
	prevWidth       int
	prevHeight      int
	prevScrollOff   int
	prevMsgScroll   int
	prevTurnScroll  int

	// Unfocused refresh throttling (td-05149f66)
	pendingRefresh bool // true when refresh was skipped due to unfocused state

	// Worktree cache to avoid git commands on every refresh (td-e74a4aaa)
	cachedWorktreePaths []string            // cached GetAllRelatedPaths result
	cachedWorktreeNames map[string]string   // cached wtPath -> name mapping
	worktreeCacheTime   time.Time           // when the cache was last updated
}

// msgLineRange tracks which screen lines a message occupies (after scroll).
type msgLineRange struct {
	MsgIdx    int // index in p.messages
	StartLine int // first visible line (relative to content area)
	LineCount int // number of visible lines
}

// msgLinePos tracks actual line position for each rendered message (before scroll).
type msgLinePos struct {
	MsgIdx    int // index in p.messages
	StartLine int // starting line in full content (0 = first line)
	LineCount int // number of lines this message takes
}

// New creates a new conversations plugin.
func New() *Plugin {
	renderer, err := NewGlamourRenderer()
	if err != nil {
		log.Printf("warn: glamour init failed: %v", err)
	}

	coalesceChan := make(chan CoalescedRefreshMsg, 8)
	p := &Plugin{
		pageSize:            defaultPageSize,
		expandedThinking:    make(map[string]bool),
		expandedMessages:    make(map[string]bool),
		expandedToolResults: make(map[string]bool),
		mouseHandler:        mouse.NewHandler(),
		contentRenderer:     renderer,
		coalesceChan:        coalesceChan,
		renderCache:         make(map[renderCacheKey]string),
		hitRegionsDirty:     true, // Start dirty to ensure first render builds regions
		sidebarVisible:      true, // Sidebar visible by default
		sidebarRestore:      PaneSidebar,
	}
	p.coalescer = NewEventCoalescer(0, coalesceChan)
	return p
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return pluginID }

// Name returns the plugin display name.
func (p *Plugin) Name() string { return pluginName }

// Icon returns the plugin icon character.
func (p *Plugin) Icon() string { return pluginIcon }

// renderContent renders markdown content to styled lines, falling back to plain text.
func (p *Plugin) renderContent(content string, width int) []string {
	if p.contentRenderer != nil {
		return p.contentRenderer.RenderContent(content, width)
	}
	return wrapText(content, width)
}

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	p.ctx = ctx

	// Load persisted sidebar width
	if savedWidth := state.GetConversationsSideWidth(); savedWidth > 0 {
		p.sidebarWidth = savedWidth
	}

	p.adapters = make(map[string]adapter.Adapter)
	for id, a := range ctx.Adapters {
		found, err := a.Detect(ctx.WorkDir)
		if err != nil || !found {
			continue
		}
		p.adapters[id] = a
	}
	if len(p.adapters) == 0 {
		return nil
	}

	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	if len(p.adapters) == 0 {
		return nil
	}

	return tea.Batch(
		p.loadSessions(),
		p.startWatcher(),
		p.listenForCoalescedRefresh(),
	)
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	// Stop event coalescer
	if p.coalescer != nil {
		p.coalescer.Stop()
	}
	// Watcher cleanup handled by adapter
}

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	switch msg := msg.(type) {
	case app.PluginFocusedMsg:
		// Catch up on pending refresh when plugin regains focus (td-05149f66)
		if p.pendingRefresh {
			p.pendingRefresh = false
			return p, p.loadSessions()
		}
		return p, nil

	case tea.MouseMsg:
		return p.handleMouse(msg)

	case tea.KeyMsg:
		switch p.view {
		case ViewAnalytics:
			return p.updateAnalytics(msg)
		default:
			// Route based on active pane
			if p.activePane == PaneMessages {
				return p.updateMessages(msg)
			}
			return p.updateSessions(msg)
		}

	case SessionsLoadedMsg:
		p.sessions = msg.Sessions
		// Update worktree cache from message (td-0e43c080: safe update in Update())
		if msg.WorktreePaths != nil {
			p.cachedWorktreePaths = msg.WorktreePaths
			p.cachedWorktreeNames = msg.WorktreeNames
			p.worktreeCacheTime = time.Now()
		}
		// Keep selection valid when sessions refresh.
		if p.selectedSession != "" {
			found := false
			for i := range p.sessions {
				if p.sessions[i].ID == p.selectedSession {
					found = true
					break
				}
			}
			if !found {
				p.selectedSession = ""
				p.loadedSession = ""
				p.messages = nil
				p.turns = nil
				p.sessionSummary = nil
			}
		}

		// Ensure a selection so the right pane can render.
		if p.selectedSession == "" && len(p.sessions) > 0 {
			if p.cursor >= len(p.sessions) {
				p.cursor = len(p.sessions) - 1
			}
			if p.cursor < 0 {
				p.cursor = 0
			}
			p.setSelectedSession(p.sessions[p.cursor].ID)
			return p, p.schedulePreviewLoad(p.selectedSession)
		}
		return p, nil

	case PreviewLoadMsg:
		if msg.Token != p.previewToken {
			return p, nil
		}
		if msg.SessionID == "" || msg.SessionID != p.selectedSession {
			return p, nil
		}
		if p.loadedSession == msg.SessionID && len(p.messages) > 0 {
			return p, nil
		}
		return p, tea.Batch(
			p.loadMessages(msg.SessionID),
			p.loadUsage(msg.SessionID),
		)

	case MessageReloadMsg:
		if msg.Token != p.messageReloadToken {
			return p, nil
		}
		if msg.SessionID == "" || msg.SessionID != p.selectedSession {
			return p, nil
		}
		return p, p.loadMessages(msg.SessionID)

	case MessagesLoadedMsg:
		if msg.SessionID == "" || msg.SessionID != p.selectedSession {
			// Ignore out-of-order loads when cursor moves quickly.
			return p, nil
		}

		// Check if this is an incremental update (same session, more messages)
		isIncremental := p.loadedSession == msg.SessionID &&
			len(p.messages) > 0 &&
			len(msg.Messages) >= len(p.messages) &&
			p.messagesMatch(p.messages, msg.Messages[:len(p.messages)])

		if isIncremental && len(msg.Messages) == len(p.messages) {
			// No new messages, skip re-processing entirely
			return p, nil
		}

		// Get session duration for summary
		var duration time.Duration
		for _, s := range p.sessions {
			if s.ID == p.selectedSession {
				duration = s.Duration
				break
			}
		}

		if isIncremental {
			// Incremental update: only process new messages
			oldLen := len(p.messages)
			newMessages := msg.Messages[oldLen:]
			p.messages = msg.Messages

			// Incrementally update turns (handles extending last turn if same role)
			p.turns = AppendMessagesToTurns(p.turns, newMessages, oldLen)

			// Incrementally update summary
			if p.sessionSummary != nil {
				UpdateSessionSummary(p.sessionSummary, newMessages, p.summaryModelCounts, p.summaryFileSet)
			}
			// Mark hit regions dirty for new content (td-ea784b03)
			p.hitRegionsDirty = true
			// Don't reset cursors - user may be scrolled
		} else {
			// Full reload: different session or messages don't match
			p.loadedSession = msg.SessionID
			p.messages = msg.Messages
			p.turns = GroupMessagesIntoTurns(msg.Messages)
			p.turnCursor = 0
			p.turnScrollOff = 0
			// Snap messageCursor to first visible message (skip tool-result-only)
			visibleIndices := p.visibleMessageIndices()
			if len(visibleIndices) > 0 {
				p.messageCursor = visibleIndices[0]
			}

			// Full summary computation - also initialize tracking maps for future incremental updates
			summary := ComputeSessionSummary(msg.Messages, duration)
			p.sessionSummary = &summary
			p.summaryModelCounts = make(map[string]int)
			p.summaryFileSet = make(map[string]bool)
			for _, m := range msg.Messages {
				if m.Model != "" {
					p.summaryModelCounts[m.Model]++
				}
				for _, tu := range m.ToolUses {
					if fp := extractFilePath(tu.Input); fp != "" {
						p.summaryFileSet[fp] = true
					}
				}
			}
			// Mark hit regions dirty for new content (td-ea784b03)
			p.hitRegionsDirty = true
		}

		p.hasMore = len(msg.Messages) >= p.pageSize

		// Update pagination state (td-313ea851)
		p.totalMessages = msg.TotalCount
		p.messageOffset = msg.Offset // Sync offset with actual loaded offset (td-39018be2)
		// hasOlderMsgs: true when there are messages beyond the current window (td-07fc795d)
		p.hasOlderMsgs = (msg.Offset + len(msg.Messages)) < msg.TotalCount

		return p, nil

	case WatchStartedMsg:
		// Watcher started, store channel and start listening
		if msg.Channel == nil {
			return p, nil // Watcher failed
		}
		p.watchChan = msg.Channel
		return p, p.listenForWatchEvents()

	case WatchEventMsg:
		// Queue event for coalescing instead of immediate reload
		p.coalescer.Add(msg.SessionID)

		cmds := []tea.Cmd{
			p.listenForWatchEvents(),
		}

		// Still reload messages immediately if selected session changed
		// (coalescer handles session list refresh)
		if msg.SessionID != "" && msg.SessionID == p.selectedSession {
			cmds = append(cmds, p.scheduleMessageReload(p.selectedSession))
		}

		return p, tea.Batch(cmds...)

	case CoalescedRefreshMsg:
		// Coalesced watch events - batch refresh
		cmds := []tea.Cmd{
			p.listenForCoalescedRefresh(), // Continue listening for more batches
		}

		// Skip full session refresh when unfocused to reduce CPU (td-05149f66).
		// Set pendingRefresh so we catch up on focus.
		if !p.focused {
			p.pendingRefresh = true
			return p, tea.Batch(cmds...)
		}

		if msg.RefreshAll || len(msg.SessionIDs) == 0 {
			// Full refresh needed
			cmds = append(cmds, p.loadSessions())
		} else {
			// Targeted refresh: only update specific sessions (td-2b8ebe)
			cmds = append(cmds, p.refreshSessions(msg.SessionIDs))
		}

		return p, tea.Batch(cmds...)

	case OpenSessionReturnedMsg:
		// After returning from external session, re-enable mouse and refresh
		cmds := []tea.Cmd{
			func() tea.Msg { return tea.EnableMouseAllMotion() },
			p.loadSessions(), // Refresh sessions in case something changed
		}
		if p.selectedSession != "" {
			cmds = append(cmds, p.loadMessages(p.selectedSession))
		}
		if msg.Err != nil {
			cmds = append(cmds, func() tea.Msg {
				return app.ToastMsg{Message: "Session exited: " + msg.Err.Error(), Duration: 2 * time.Second, IsError: true}
			})
		}
		return p, tea.Batch(cmds...)

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		// Ensure a session is selected so the right pane can render
		if p.selectedSession == "" && len(p.sessions) > 0 {
			if p.cursor >= len(p.sessions) {
				p.cursor = len(p.sessions) - 1
			}
			if p.cursor < 0 {
				p.cursor = 0
			}
			p.setSelectedSession(p.sessions[p.cursor].ID)
			return p, p.schedulePreviewLoad(p.selectedSession)
		}
		return p, nil
	}

	return p, nil
}

// View renders the plugin.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height
	// Note: sidebarWidth is calculated in renderTwoPane, not here,
	// to avoid resetting drag-adjusted widths on every render

	var content string
	if len(p.adapters) == 0 {
		content = renderNoAdapter()
	} else {
		switch p.view {
		case ViewAnalytics:
			content = p.renderAnalytics()
		default:
			content = p.renderTwoPane()
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
			{ID: "select", Name: "Select", Description: "Select search result", Category: plugin.CategoryActions, Context: "conversations-search", Priority: 1},
			{ID: "cancel", Name: "Cancel", Description: "Cancel search", Category: plugin.CategoryActions, Context: "conversations-search", Priority: 1},
		}
	}
	if p.filterMode {
		return []plugin.Command{
			{ID: "select", Name: "Select", Description: "Apply filter", Category: plugin.CategoryActions, Context: "conversations-filter", Priority: 1},
			{ID: "cancel", Name: "Cancel", Description: "Cancel filter", Category: plugin.CategoryActions, Context: "conversations-filter", Priority: 1},
		}
	}
	// Detail mode (right pane shows turn detail)
	if p.detailMode {
		return []plugin.Command{
			{ID: "back", Name: "Back", Description: "Return to turn list", Category: plugin.CategoryNavigation, Context: "turn-detail", Priority: 1},
			{ID: "scroll", Name: "Scroll", Description: "Scroll detail", Category: plugin.CategoryNavigation, Context: "turn-detail", Priority: 2},
			{ID: "yank", Name: "Yank", Description: "Yank turn content", Category: plugin.CategoryActions, Context: "turn-detail", Priority: 3},
		}
	}
	if p.activePane == PaneMessages {
		return []plugin.Command{
			{ID: "toggle-view", Name: "View", Description: "Toggle conversation/turn view", Category: plugin.CategoryView, Context: "conversations-main", Priority: 1},
			{ID: "detail", Name: "Detail", Description: "View turn details", Category: plugin.CategoryView, Context: "conversations-main", Priority: 2},
			{ID: "expand", Name: "Expand", Description: "Expand selected item", Category: plugin.CategoryView, Context: "conversations-main", Priority: 3},
			{ID: "back", Name: "Back", Description: "Return to sidebar", Category: plugin.CategoryNavigation, Context: "conversations-main", Priority: 4},
			{ID: "open", Name: "Open", Description: "Open in CLI", Category: plugin.CategoryActions, Context: "conversations-main", Priority: 5},
			{ID: "yank", Name: "Yank", Description: "Yank turn content", Category: plugin.CategoryActions, Context: "conversations-main", Priority: 6},
			{ID: "toggle-sidebar", Name: "Panel", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "conversations-main", Priority: 7},
		}
	}
	if p.view == ViewAnalytics {
		return []plugin.Command{
			{ID: "back", Name: "Back", Description: "Return to conversations", Category: plugin.CategoryNavigation, Context: "analytics", Priority: 1},
		}
	}
	return []plugin.Command{
		{ID: "view-session", Name: "View", Description: "View session messages", Category: plugin.CategoryView, Context: "conversations-sidebar", Priority: 1},
		{ID: "search", Name: "Search", Description: "Search conversations", Category: plugin.CategorySearch, Context: "conversations-sidebar", Priority: 2},
		{ID: "filter", Name: "Filter", Description: "Filter by project", Category: plugin.CategorySearch, Context: "conversations-sidebar", Priority: 2},
		{ID: "open", Name: "Open", Description: "Open in CLI", Category: plugin.CategoryActions, Context: "conversations-sidebar", Priority: 3},
		{ID: "yank", Name: "Yank", Description: "Yank session details", Category: plugin.CategoryActions, Context: "conversations-sidebar", Priority: 4},
		{ID: "yank-resume", Name: "Resume", Description: "Yank resume command", Category: plugin.CategoryActions, Context: "conversations-sidebar", Priority: 4},
		{ID: "toggle-sidebar", Name: "Panel", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "conversations-sidebar", Priority: 5},
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
	// Detail mode (right pane shows turn detail)
	if p.detailMode {
		return "turn-detail"
	}
	switch p.view {
	case ViewAnalytics:
		return "analytics"
	default:
		// Return context based on active pane
		if p.activePane == PaneSidebar {
			return "conversations-sidebar"
		}
		return "conversations-main"
	}
}

// Diagnostics returns plugin health info.
func (p *Plugin) Diagnostics() []plugin.Diagnostic {
	status := "ok"
	detail := ""
	if len(p.adapters) == 0 {
		status = "disabled"
		detail = "no adapters"
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

// copySessionToClipboard copies the current session as markdown to clipboard.
func (p *Plugin) copySessionToClipboard() tea.Cmd {
	session := p.findSelectedSession()
	messages := p.messages

	return func() tea.Msg {
		md := ExportSessionAsMarkdown(session, messages)
		if err := CopyToClipboard(md); err != nil {
			return app.ToastMsg{Message: "Copy failed: " + err.Error(), Duration: 2 * time.Second, IsError: true}
		}
		return app.ToastMsg{Message: "Session copied to clipboard", Duration: 2 * time.Second}
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
			return app.ToastMsg{Message: "Export failed: " + err.Error(), Duration: 2 * time.Second, IsError: true}
		}
		return app.ToastMsg{Message: "Exported to " + filename, Duration: 2 * time.Second}
	}
}

// Message types
type SessionsLoadedMsg struct {
	Sessions []adapter.Session
	// Worktree cache data (td-0e43c080: computed in cmd, stored in Update)
	WorktreePaths []string
	WorktreeNames map[string]string
}

type MessagesLoadedMsg struct {
	SessionID  string
	Messages   []adapter.Message
	TotalCount int // Total message count before truncation (td-313ea851)
	Offset     int // Offset into the message list (td-313ea851)
}

type WatchEventMsg struct {
	SessionID string // ID of the session that changed (empty for periodic refresh)
}
type WatchStartedMsg struct {
	Channel <-chan adapter.Event
}
type ErrorMsg struct{ Err error }

type PreviewLoadMsg struct {
	Token     int
	SessionID string
}

type MessageReloadMsg struct {
	Token     int
	SessionID string
}

// TickCmd returns a command that triggers periodic refresh.
func TickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return WatchEventMsg{}
	})
}
