package conversations

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"sort"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/adapter/tieredwatcher"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/clip"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/queryfield"
	"github.com/marcus/sidecar/internal/startuptrace"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	pluginID   = "conversations"
	pluginName = "conversations"
	pluginIcon = "C"

	// Default page size for messages
	defaultPageSize     = 50
	maxMessagesInMemory = 500

	// Default page size for session list pagination (td-7198a5)
	defaultSessionPageSize = 50

	previewDebounce     = 150 * time.Millisecond
	watchReloadDebounce = 200 * time.Millisecond
	loadSettleDelay     = 300 * time.Millisecond // Wait for sessions to settle before hiding skeleton

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
	regionSearchClear = "search-clear" // × on the Sessions `/` bar
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
	expanded  bool   // whether content is expanded (affects render)
	styleKey  string // active markdown theme identity (td-031c89)
}

// Plugin implements the conversations plugin.
type Plugin struct {
	ctx      *plugin.Context
	adapters map[string]adapter.Adapter
	// discoveryAdapters contains global sources retained after Detect returned
	// false so the first session for this project can be discovered live.
	discoveryAdapters map[string]bool
	focused           bool
	mouseHandler      *mouse.Handler
	hoverDivider      bool

	// Current view
	view View

	// Session list state
	sessions        []adapter.Session
	cursor          int
	scrollOff       int
	displayedCount  int  // sessions currently surfaced to UI (td-7198a5)
	hasMoreSessions bool // displayedCount < len(sessions) (td-7198a5)
	loadingAdapters bool // true while adapter batches are still arriving (td-7198a5)
	// detectingAdapters is true between Start() and AdaptersDetectedMsg. Adapter
	// detection walks each tool's session store, which is slow enough to block
	// the first frame, so it runs off the startup path (td-9c7bf2).
	detectingAdapters bool

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
	activePane         FocusPane // Which pane is focused
	sidebarRestore     FocusPane // Tracks pane focused before collapse; restored on expand via toggleSidebar()
	sidebarWidth       int       // Calculated width (~30%)
	sidebarVisible     bool      // Toggle sidebar visibility with \
	previewToken       int       // monotonically increasing token for debounced preview loads
	messageReloadToken int       // monotonically increasing token for debounced watch reloads

	// View dimensions
	width  int
	height int

	// Watcher channel
	watchChan    <-chan adapter.Event
	watchClosers []io.Closer
	watchCancel  context.CancelFunc // cancel function for watcher goroutines (td-eb2699b4)
	stopped      bool

	// Tiered watcher manager for FD reduction (td-dca6fe)
	tieredManager *tieredwatcher.Manager

	// Event coalescing for watch events
	coalescer         *EventCoalescer
	coalesceChan      chan CoalescedRefreshMsg
	coalesceChanClose sync.Once

	// Incremental adapter session batches (td-7198a5)
	adapterBatchChan chan AdapterBatchMsg
	adapterSpinner   ui.BrailleSpinner // animated loading indicator while adapters load

	// Search state
	searchMode  bool
	searchField queryfield.Field
	// searchClearRect is where the search row's × landed, in the row's own
	// coordinates, so the region pass can register it.
	searchClearRect mouse.Rect
	searchResults   []adapter.Session

	// Filter state
	filterMode            bool
	filters               SearchFilters
	filterActive          bool     // true when any filter is active
	defaultCategoryFilter []string // from config, used by C toggle to restore

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

	// Interactive session-list scrollbar (td-550ce1)
	listScroll listScrollState
	// Unfocused refresh throttling (td-05149f66)
	pendingRefresh bool // true when refresh was skipped due to unfocused state

	// Worktree cache to avoid git commands on every refresh (td-e74a4aaa)
	cachedWorktreePaths []string          // cached GetAllRelatedPaths result
	cachedWorktreeNames map[string]string // cached wtPath -> name mapping
	worktreeCacheTime   time.Time         // when the cache was last updated

	// Session loading serialization to prevent FD accumulation (td-023577)
	loadingMu        sync.Mutex // guards loadingSessions/load generation
	loadingSessions  bool       // true when loadSessions() goroutine is running
	sessionLoadSeq   uint64     // monotonically increasing whole-load generation
	activeLoadSeq    uint64     // generation allowed to publish/clear loading state
	loadCancel       context.CancelFunc
	refreshCtx       context.Context // canceled on Stop/project switch
	refreshCancel    context.CancelFunc
	sessionLoadMu    sync.Mutex               // guards in-flight per-adapter/worktree session loads
	sessionCallGate  map[string]chan struct{} // serializes stateful adapter reads by adapter ID
	sessionCallMapMu sync.Mutex
	sessionPathSeq   uint64 // monotonically increasing token for in-flight path loads
	sessionLoads     map[string]uint64

	// Large session warning tracking (td-ee67d8)
	warnedSessions map[string]bool // session ID -> already warned about size

	// Pi adapter discovery toast (td-697e89)
	piDiscoveryToastShown bool // true after showing one-time Pi discovery toast

	// Initial load state (td-6cc19f)
	initialLoadDone bool        // true after sessions settle (no new arrivals for settleDelay)
	skeleton        ui.Skeleton // shimmer loading animation
	loadSettleToken int         // token for debounced settle check

	// Resume modal state (td-aa4136)
	showResumeModal       bool
	resumeModal           *modal.Modal
	resumeModalWidth      int
	resumeType            int // 0=shell, 1=worktree
	resumeNameInput       textinput.Model
	resumeBaseBranchInput textinput.Model
	resumeAgentIdx        int
	resumeSkipPermissions bool
	resumeFocus           int
	resumeSession         *adapter.Session

	// Content search state (td-6ac70a: cross-conversation search)
	contentSearchMode  bool                // True when content search modal is open
	contentSearchState *ContentSearchState // Content search state

	// Pending scroll target after messages load (td-b74d9f)
	// Uses message ID (not index) to handle pagination correctly
	pendingScrollMsgID  string // Target message ID to scroll to after load ("" = none)
	pendingScrollActive bool   // True when we have a pending scroll request
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
		displayedCount:      defaultSessionPageSize,
		expandedThinking:    make(map[string]bool),
		expandedMessages:    make(map[string]bool),
		expandedToolResults: make(map[string]bool),
		mouseHandler:        mouse.NewHandler(),
		contentRenderer:     renderer,
		coalesceChan:        coalesceChan,
		adapterBatchChan:    make(chan AdapterBatchMsg, 8),
		adapterSpinner:      ui.NewBrailleSpinner(),
		renderCache:         make(map[renderCacheKey]string),
		hitRegionsDirty:     true, // Start dirty to ensure first render builds regions
		sidebarVisible:      true, // Sidebar visible by default
		sidebarRestore:      PaneSidebar,
		warnedSessions:      make(map[string]bool),
		sessionLoads:        make(map[string]uint64),
		sessionCallGate:     make(map[string]chan struct{}),
		skeleton:            ui.NewSkeleton(8, nil), // 8 placeholder rows
	}
	p.refreshCtx, p.refreshCancel = context.WithCancel(context.Background())
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

// resetState clears all session/UI state for reinitialization (td-84a1cb).
// Called from Init() to ensure clean state when switching projects.
func (p *Plugin) resetState() {
	// Reset loading serialization flag (td-6f6ba1: prevent stale load blocking new project)
	// Must be reset so new project's loadSessions() isn't skipped
	p.loadingMu.Lock()
	if p.loadCancel != nil {
		p.loadCancel()
		p.loadCancel = nil
	}
	p.activeLoadSeq++
	p.loadingSessions = false
	p.loadingMu.Unlock()
	if p.refreshCancel != nil {
		p.refreshCancel()
	}
	p.refreshCtx, p.refreshCancel = context.WithCancel(context.Background())
	p.sessionLoadMu.Lock()
	p.sessionLoads = make(map[string]uint64)
	p.sessionLoadMu.Unlock()

	// Session list state
	p.sessions = nil
	p.cursor = 0
	p.scrollOff = 0
	p.displayedCount = defaultSessionPageSize
	p.hasMoreSessions = false
	p.loadingAdapters = false
	p.discoveryAdapters = make(map[string]bool)

	// Message view state
	p.selectedSession = ""
	p.loadedSession = ""
	p.messages = nil
	p.turns = nil
	p.turnCursor = 0
	p.turnScrollOff = 0
	p.msgCursor = 0
	p.msgScrollOff = 0
	p.hasMore = false

	// Pagination state
	p.messageOffset = 0
	p.totalMessages = 0
	p.hasOlderMsgs = false
	p.expandedThinking = make(map[string]bool)
	p.sessionSummary = nil
	p.summaryModelCounts = nil
	p.summaryFileSet = nil
	p.showToolSummary = false
	p.turnViewMode = false

	// Message detail view state
	p.detailMode = false
	p.detailTurn = nil
	p.detailScroll = 0

	// Analytics view state
	p.analyticsScrollOff = 0
	p.analyticsLines = nil

	// Layout state - reset to defaults but preserve sidebarWidth (persisted)
	p.activePane = PaneSidebar
	p.sidebarRestore = PaneSidebar
	p.sidebarVisible = true
	p.previewToken = 0
	p.messageReloadToken = 0

	// Search state
	p.searchMode = false
	p.searchField.Reset()
	p.searchResults = nil

	// Filter state
	p.filterMode = false
	p.filters = SearchFilters{}
	p.filterActive = false
	p.defaultCategoryFilter = nil

	// Conversation flow view state
	p.expandedMessages = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)
	p.messageScroll = 0
	p.messageCursor = 0

	// Line tracking
	p.visibleMsgRanges = nil
	p.msgLinePositions = nil

	// Render cache
	p.renderCache = make(map[renderCacheKey]string)
	p.hitRegionsDirty = true

	// Refresh throttling
	p.pendingRefresh = false

	// Worktree cache - invalidate to force fresh discovery
	p.cachedWorktreePaths = nil
	p.cachedWorktreeNames = nil
	p.worktreeCacheTime = time.Time{}

	// Large session warning tracking
	p.warnedSessions = make(map[string]bool)

	// Recreate coalescer infrastructure (td-84a1cb)
	// The old coalescer has closed=true and channel is closed after Stop()
	p.coalesceChanClose = sync.Once{}
	p.coalesceChan = make(chan CoalescedRefreshMsg, 8)
	p.coalescer = NewEventCoalescer(0, p.coalesceChan)

	// Recreate adapter batch channel (td-7198a5)
	p.adapterBatchChan = make(chan AdapterBatchMsg, 8)

	// Initial load state (td-6cc19f)
	p.initialLoadDone = false
	p.skeleton = ui.NewSkeleton(8, nil)
	p.loadSettleToken = 0

	// Content search state (td-6ac70a)
	p.contentSearchMode = false
	p.contentSearchState = nil

	// Pending scroll state (td-b74d9f)
	p.pendingScrollMsgID = ""
	p.pendingScrollActive = false

	// Tiered watcher manager (td-dca6fe)
	// Close existing manager before resetting (handled by closeWatchers in Stop)
	p.tieredManager = nil
}

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	p.ctx = ctx

	// Reset all state for clean reinitialization (td-84a1cb)
	p.resetState()

	// Load persisted sidebar width
	if savedWidth := state.GetConversationsSideWidth(); savedWidth > 0 {
		p.sidebarWidth = savedWidth
	}

	// Store default category filter from config for C toggle (td-91bbc4)
	// Don't apply on startup — non-Pi adapters leave SessionCategory empty,
	// so filtering by "interactive" would hide all their sessions (td-d3b1f6)
	if ctx.Config != nil && len(ctx.Config.Plugins.Conversations.DefaultCategoryFilter) > 0 {
		p.defaultCategoryFilter = ctx.Config.Plugins.Conversations.DefaultCategoryFilter
	} else {
		p.defaultCategoryFilter = []string{adapter.SessionCategoryInteractive}
	}

	// Adapter detection is deliberately NOT done here. Detect() walks each
	// tool's session store (Codex alone can be thousands of files), which used
	// to add seconds to the pre-first-frame path — much worse on machines where
	// an endpoint security agent intercepts every file open. Start() kicks it
	// off asynchronously instead (td-9c7bf2).
	p.adapters = make(map[string]adapter.Adapter)
	p.discoveryAdapters = make(map[string]bool)

	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	p.stopped = false
	p.detectingAdapters = true

	return tea.Batch(
		p.detectAdapters(),
		p.listenForCoalescedRefresh(),
		p.skeleton.Start(), // Start skeleton animation (td-6cc19f)
	)
}

// detectAdapters probes every registered adapter for this project concurrently
// and reports the ones that matched. Probes are independent, so the command
// costs roughly the slowest adapter rather than the sum of all of them.
func (p *Plugin) detectAdapters() tea.Cmd {
	var epoch uint64
	var projectRoot string
	var logger *slog.Logger
	all := map[string]adapter.Adapter{}
	if p.ctx != nil {
		epoch = p.ctx.Epoch
		projectRoot = p.ctx.ProjectRoot
		logger = p.ctx.Logger
		all = p.ctx.Adapters
	}

	return func() tea.Msg {
		var mu sync.Mutex
		found := make(map[string]adapter.Adapter, len(all))
		discoveryOnly := make(map[string]bool)

		var wg sync.WaitGroup
		for id, a := range all {
			wg.Add(1)
			go func(id string, a adapter.Adapter) {
				defer wg.Done()
				// Detect used to run inside registry.safeInit, which recovers
				// panics and degrades the plugin to "unavailable". Here it runs
				// in a bare goroutine, which Bubble Tea's command panic handler
				// does NOT cover, so a panicking adapter would take the whole
				// process down with the terminal still in raw mode. Treat a
				// panic as "not detected" instead (td-9c7bf2).
				defer func() {
					if rec := recover(); rec != nil && logger != nil {
						logger.Error("adapter detect panicked", "adapter", id, "panic", rec)
					}
				}()
				var ok bool
				var err error
				startuptrace.Track("adapter.Detect:"+id, func() {
					ok, err = a.Detect(projectRoot)
				})
				if err != nil {
					return
				}
				if !ok {
					discovery, watches := a.(adapter.ProjectDiscoveryWatcher)
					if !watches || !discovery.WatchForProjectDiscovery() {
						return
					}
					mu.Lock()
					discoveryOnly[id] = true
					mu.Unlock()
				}
				mu.Lock()
				found[id] = a
				mu.Unlock()
			}(id, a)
		}
		wg.Wait()

		return AdaptersDetectedMsg{Epoch: epoch, Adapters: found, DiscoveryOnly: discoveryOnly}
	}
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	p.stopped = true
	if p.refreshCancel != nil {
		p.refreshCancel()
	}
	p.loadingMu.Lock()
	if p.loadCancel != nil {
		p.loadCancel()
		p.loadCancel = nil
	}
	p.activeLoadSeq++
	p.loadingSessions = false
	p.loadingMu.Unlock()
	// Cancel watcher goroutines (td-eb2699b4)
	if p.watchCancel != nil {
		p.watchCancel()
	}
	// Stop event coalescer
	if p.coalescer != nil {
		p.coalescer.Stop()
	}
	// Close coalesce channel to unblock any listening goroutines (td-e2791614)
	p.coalesceChanClose.Do(func() {
		if p.coalesceChan != nil {
			close(p.coalesceChan)
		}
	})
	p.closeWatchers()
	p.watchChan = nil
}

func (p *Plugin) closeWatchers() {
	if p.watchCancel != nil {
		p.watchCancel()
		p.watchCancel = nil
	}
	for _, closer := range p.watchClosers {
		_ = closer.Close()
	}
	p.watchClosers = nil
	// Close tiered manager if present (td-dca6fe)
	if p.tieredManager != nil {
		_ = p.tieredManager.Close()
		p.tieredManager = nil
	}
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

	case ui.SkeletonTickMsg:
		// Forward tick to skeleton for animation (td-6cc19f)
		var cmds []tea.Cmd
		if cmd := p.skeleton.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Advance braille spinner frame (td-7198a5)
		p.adapterSpinner.Tick()
		// Also forward to content search skeleton if modal is open (td-e740e4)
		if p.contentSearchMode && p.contentSearchState != nil {
			if cmd := p.contentSearchState.Skeleton.Update(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if len(cmds) > 0 {
			return p, tea.Batch(cmds...)
		}
		return p, nil

	case tea.MouseMsg:
		return p.handleMouse(msg)

	case tea.PasteMsg:
		// A live Sessions search bar is a text input: a bracketed paste lands
		// in it exactly as typed characters do.
		if handled, cmd := p.handleSearchPaste(msg); handled {
			return p, cmd
		}

	case tea.KeyPressMsg:
		// Handle content search modal first if open (td-6ac70a)
		if p.contentSearchMode {
			return p.handleContentSearchKey(msg)
		}

		// Handle resume modal first if open (td-aa4136)
		if p.showResumeModal {
			cmd := p.handleResumeModalKeys(msg)
			return p, cmd
		}

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

	case AdaptersDetectedMsg:
		if plugin.IsStale(p.ctx, msg) || p.stopped {
			return p, nil
		}
		p.detectingAdapters = false
		p.adapters = msg.Adapters
		p.discoveryAdapters = msg.DiscoveryOnly
		if len(p.adapters) == 0 {
			p.skeleton.Stop()
			p.initialLoadDone = true
			return p, nil
		}
		return p, p.loadSessions()

	case LoadingStartedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.loadingAdapters = true
		p.adapterSpinner.Start()
		return p, p.listenForAdapterBatch()

	case AdapterBatchMsg:
		if plugin.IsStale(p.ctx, msg) {
			if !msg.Final {
				return p, p.listenForAdapterBatch() // keep draining stale batches
			}
			return p, nil
		}
		var cmds []tea.Cmd
		for _, notice := range msg.Notices {
			n := notice
			cmds = append(cmds, func() tea.Msg {
				return app.ToastMsg{Message: n, Duration: 4 * time.Second, IsError: true}
			})
		}
		if msg.AdapterID != "" && len(msg.Sessions) > 0 {
			delete(p.discoveryAdapters, msg.AdapterID)
		}

		// Merge new sessions, deduplicating by ID
		seen := make(map[string]bool, len(p.sessions))
		for _, s := range p.sessions {
			seen[s.ID] = true
		}
		for _, s := range msg.Sessions {
			if !seen[s.ID] {
				seen[s.ID] = true
				p.sessions = append(p.sessions, s)
			}
		}
		// Re-sort by UpdatedAt descending
		sort.Slice(p.sessions, func(i, j int) bool {
			return p.sessions[i].UpdatedAt.After(p.sessions[j].UpdatedAt)
		})

		// Update pagination state (td-7198a5)
		if p.displayedCount == 0 {
			p.displayedCount = defaultSessionPageSize
		}
		p.hasMoreSessions = len(p.sessions) > p.displayedCount

		// Update coalescer with session sizes
		if p.coalescer != nil {
			p.coalescer.UpdateSessionSizes(p.sessions)
		}

		if !msg.Final {
			// Keep listening for more adapter batches
			cmds = append(cmds, p.listenForAdapterBatch())
		} else {
			// All adapters done (td-7198a5)
			p.loadingAdapters = false
			p.adapterSpinner.Stop()
			// Final batch: update worktree cache
			if msg.WorktreePaths != nil {
				p.cachedWorktreePaths = msg.WorktreePaths
				p.cachedWorktreeNames = msg.WorktreeNames
				p.worktreeCacheTime = time.Now()
			}
			// Check for large session warnings
			if cmd := p.checkLargeSessionWarnings(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			// Check for Pi adapter discovery toast (td-697e89)
			if cmd := p.checkPiDiscoveryToast(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			// Schedule settle check for skeleton hide
			if !p.initialLoadDone {
				p.loadSettleToken++
				token := p.loadSettleToken
				cmds = append(cmds, tea.Tick(loadSettleDelay, func(time.Time) tea.Msg {
					return LoadSettledMsg{Token: token}
				}))
			}
			// Watcher setup starts only after the completed load so it can reuse the
			// collected sessions and cannot race Sessions() over a stateful adapter.
			if p.watchCancel == nil && p.watchChan == nil {
				cmds = append(cmds, p.startWatcher())
			}
		}

		// Ensure a selection so the right pane can render
		if p.selectedSession == "" && len(p.sessions) > 0 {
			if p.cursor >= len(p.visibleSessions()) {
				p.cursor = 0
			}
			sessions := p.visibleSessions()
			if len(sessions) > 0 {
				p.setSelectedSession(sessions[p.cursor].ID)
				cmds = append(cmds, p.schedulePreviewLoad(p.selectedSession))
			}
		}

		p.updateTieredHotTargets()
		return p, tea.Batch(cmds...)

	case SessionsLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
		p.sessions = msg.Sessions
		// Update session pagination state (td-7198a5)
		if p.displayedCount == 0 {
			p.displayedCount = defaultSessionPageSize
		}
		p.hasMoreSessions = len(p.sessions) > p.displayedCount
		// Update coalescer with session sizes for dynamic debounce (td-190095)
		if p.coalescer != nil {
			p.coalescer.UpdateSessionSizes(msg.Sessions)
		}
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

		// Check for large session warnings (td-ee67d8)
		warningCmd := p.checkLargeSessionWarnings()

		// Schedule settle check for skeleton hide (td-6cc19f)
		// If more sessions arrive before settle, the token will be invalidated
		var settleCmd tea.Cmd
		if !p.initialLoadDone {
			p.loadSettleToken++
			token := p.loadSettleToken
			settleCmd = tea.Tick(loadSettleDelay, func(t time.Time) tea.Msg {
				return LoadSettledMsg{Token: token}
			})
		}

		// Ensure a selection so the right pane can render.
		var cmds []tea.Cmd
		if p.selectedSession == "" && len(p.sessions) > 0 {
			if p.cursor >= len(p.sessions) {
				p.cursor = len(p.sessions) - 1
			}
			if p.cursor < 0 {
				p.cursor = 0
			}
			p.setSelectedSession(p.sessions[p.cursor].ID)
			cmds = append(cmds, p.schedulePreviewLoad(p.selectedSession))
		}
		if warningCmd != nil {
			cmds = append(cmds, warningCmd)
		}
		if settleCmd != nil {
			cmds = append(cmds, settleCmd)
		}
		p.updateTieredHotTargets()
		if len(cmds) > 0 {
			return p, tea.Batch(cmds...)
		}
		return p, nil

	case SessionsRefreshedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		// Merge refreshed sessions into current list (not a stale snapshot).
		// This avoids overwriting sessions added concurrently by loadSessions.
		refreshMap := make(map[string]*adapter.Session, len(msg.Refreshed))
		for i := range msg.Refreshed {
			refreshMap[msg.Refreshed[i].ID] = &msg.Refreshed[i]
		}
		for i := range p.sessions {
			if refreshed, ok := refreshMap[p.sessions[i].ID]; ok {
				// Preserve worktree fields from existing session
				refreshed.WorktreeName = p.sessions[i].WorktreeName
				refreshed.WorktreePath = p.sessions[i].WorktreePath
				p.sessions[i] = *refreshed
				delete(refreshMap, refreshed.ID)
			}
		}
		// Append any truly new sessions not already in the list
		for _, s := range refreshMap {
			p.sessions = append(p.sessions, *s)
		}
		// Re-sort by UpdatedAt descending
		sort.Slice(p.sessions, func(i, j int) bool {
			return p.sessions[i].UpdatedAt.After(p.sessions[j].UpdatedAt)
		})
		p.hasMoreSessions = len(p.sessions) > p.displayedCount
		p.updateTieredHotTargets()
		return p, nil

	case LoadSettledMsg:
		// Only settle if token matches (no new sessions arrived) (td-6cc19f)
		if msg.Token == p.loadSettleToken && !p.initialLoadDone {
			p.initialLoadDone = true
			p.skeleton.Stop()
		}
		return p, nil

	case PreviewLoadMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
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
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
		if msg.Token != p.messageReloadToken {
			return p, nil
		}
		if msg.SessionID == "" || msg.SessionID != p.selectedSession {
			return p, nil
		}
		return p, p.loadMessages(msg.SessionID)

	case MessagesLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
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

		// Process pending scroll request from content search (td-b74d9f)
		// Uses message ID (not index) to handle pagination correctly
		if p.pendingScrollActive && p.pendingScrollMsgID != "" {
			p.pendingScrollActive = false
			targetMsgID := p.pendingScrollMsgID
			p.pendingScrollMsgID = ""

			// Find the message by ID in the loaded messages
			foundIdx := -1
			for i, m := range p.messages {
				if m.ID == targetMsgID {
					foundIdx = i
					break
				}
			}

			// If found, scroll to it
			if foundIdx >= 0 {
				// Find the corresponding visible index (skip tool-result-only messages)
				visibleIndices := p.visibleMessageIndices()
				for i, idx := range visibleIndices {
					if idx >= foundIdx {
						p.messageCursor = idx
						p.ensureMessageCursorVisible()
						break
					}
					// If we're at the last visible index, use it
					if i == len(visibleIndices)-1 {
						p.messageCursor = idx
						p.ensureMessageCursorVisible()
					}
				}
			}
		}

		return p, nil

	case WatchStartedMsg:
		if plugin.IsStale(p.ctx, msg) || p.stopped {
			if msg.Cancel != nil {
				msg.Cancel()
			}
			if msg.Manager != nil {
				_ = msg.Manager.Close()
			}
			for _, closer := range msg.Closers {
				_ = closer.Close()
			}
			return p, nil
		}
		var watchCmds []tea.Cmd
		for _, notice := range msg.Notices {
			n := notice
			watchCmds = append(watchCmds, func() tea.Msg {
				return app.ToastMsg{Message: n, Duration: 4 * time.Second, IsError: true}
			})
		}
		// Watcher started, store channel and start listening
		if msg.Channel == nil {
			for _, closer := range msg.Closers {
				_ = closer.Close()
			}
			return p, tea.Batch(watchCmds...) // Watcher failed
		}
		p.closeWatchers()
		p.watchCancel = msg.Cancel
		p.tieredManager = msg.Manager
		p.watchClosers = msg.Closers
		p.watchChan = msg.Channel
		watchCmds = append(watchCmds, p.listenForWatchEvents())
		return p, tea.Batch(watchCmds...)

	case WatchEventMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
		// Queue event for coalescing instead of immediate reload
		// Pass epoch so CoalescedRefreshMsg can be validated for staleness
		var epoch uint64
		if p.ctx != nil {
			epoch = p.ctx.Epoch
		}
		sessionID := msg.SessionID
		if p.discoveryAdapters[msg.AdapterID] || p.globalEventNeedsProjectRefresh(msg.AdapterID, sessionID) {
			// SessionByID alone cannot prove that a global event belongs to the
			// current project. Refresh through Sessions(project) until a matching
			// session has arrived and discovery mode is cleared.
			sessionID = ""
		}
		p.coalescer.Add(sessionID, epoch)

		cmds := []tea.Cmd{
			p.listenForWatchEvents(),
		}

		// Still reload messages immediately if selected session changed
		// (coalescer handles session list refresh)
		if sessionID != "" && sessionID == p.selectedSession {
			cmds = append(cmds, p.scheduleMessageReload(p.selectedSession))
		}

		return p, tea.Batch(cmds...)

	case CoalescedRefreshMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
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

	// Content search messages (td-6ac70a)
	case ContentSearchDebounceMsg:
		if p.contentSearchState != nil && msg.Version == p.contentSearchState.DebounceVersion {
			// Capture epoch for stale detection on project switch
			var epoch uint64
			if p.ctx != nil {
				epoch = p.ctx.Epoch
			}
			return p, RunContentSearch(
				msg.Query,
				p.sessions,
				p.adapters,
				adapter.SearchOptions{
					UseRegex:      p.contentSearchState.UseRegex,
					CaseSensitive: p.contentSearchState.CaseSensitive,
					MaxResults:    50,
				},
				epoch,
			)
		}
		return p, nil

	case ContentSearchResultsMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
		if p.contentSearchState != nil {
			// Only accept results if query matches current query (td-5b9928: prevent stale results)
			if msg.Query != p.contentSearchState.Query {
				return p, nil // Discard stale results
			}
			p.contentSearchState.Results = msg.Results
			p.contentSearchState.IsSearching = false
			p.contentSearchState.Skeleton.Stop()               // Stop skeleton animation (td-e740e4)
			p.contentSearchState.Cursor = 0                    // Reset cursor to first result
			p.contentSearchState.ScrollOffset = 0              // Reset scroll
			p.contentSearchState.TotalFound = msg.TotalMatches // (td-8e1a2b)
			p.contentSearchState.Truncated = msg.Truncated     // (td-8e1a2b)
			if msg.Error != nil {
				p.contentSearchState.Error = msg.Error.Error()
			} else {
				p.contentSearchState.Error = ""
			}
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

	// Handle content search modal overlay (td-6ac70a, td-435ae6)
	if p.contentSearchMode && p.contentSearchState != nil {
		background := p.renderTwoPane()
		modalContent := renderContentSearchModal(p.contentSearchState, width, height)
		return ui.OverlayModal(background, modalContent, width, height)
	}

	// Handle resume modal overlay (td-aa4136)
	if p.showResumeModal {
		content := p.renderResumeModal(width, height)
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
	}

	var content string
	if len(p.adapters) == 0 && !p.detectingAdapters {
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
	// Content search mode commands (td-6ac70a, td-2467e8: updated shortcuts)
	if p.contentSearchMode {
		return []plugin.Command{
			{ID: "close", Name: "Close", Description: "Close search", Category: plugin.CategoryNavigation, Context: "conversations-content-search", Priority: 1},
			{ID: "select", Name: "Select", Description: "Jump to result", Category: plugin.CategoryActions, Context: "conversations-content-search", Priority: 2},
			{ID: "navigate", Name: "Nav", Description: "Navigate \u2191/\u2193", Category: plugin.CategoryNavigation, Context: "conversations-content-search", Priority: 3},
			{ID: "expand", Name: "Expand", Description: "Toggle tab", Category: plugin.CategoryView, Context: "conversations-content-search", Priority: 4},
			{ID: "regex", Name: "Regex", Description: "Toggle ctrl+r", Category: plugin.CategoryView, Context: "conversations-content-search", Priority: 5},
			{ID: "case", Name: "Case", Description: "Toggle alt+c", Category: plugin.CategoryView, Context: "conversations-content-search", Priority: 6},
		}
	}
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
			{ID: "content-search", Name: "Find", Description: "Search content (F)", Category: plugin.CategorySearch, Context: "conversations-main", Priority: 3},
			{ID: "back", Name: "Back", Description: "Return to sidebar", Category: plugin.CategoryNavigation, Context: "conversations-main", Priority: 4},
			{ID: "open", Name: "Open", Description: "Open in CLI", Category: plugin.CategoryActions, Context: "conversations-main", Priority: 5},
			{ID: "yank", Name: "Yank", Description: "Yank turn content", Category: plugin.CategoryActions, Context: "conversations-main", Priority: 6},
			{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "conversations-main", Priority: 7},
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
		{ID: "content-search", Name: "Find", Description: "Search content (F)", Category: plugin.CategorySearch, Context: "conversations-sidebar", Priority: 2},
		{ID: "toggle-category", Name: "Category", Description: "Toggle category filter", Category: plugin.CategorySearch, Context: "conversations-sidebar", Priority: 3},
		{ID: "resume-in-workspace", Name: "Resume", Description: "Resume in workspace", Category: plugin.CategoryActions, Context: "conversations-sidebar", Priority: 3},
		{ID: "yank-details", Name: "Copy Details", Description: "Copy session details", Category: plugin.CategoryActions, Context: "conversations-sidebar", Priority: 3},
		{ID: "yank-resume", Name: "Copy Resume", Description: "Copy resume command", Category: plugin.CategoryActions, Context: "conversations-sidebar", Priority: 4},
		{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "conversations-sidebar", Priority: 5},
	}
}

// FocusContext returns the current focus context.
func (p *Plugin) FocusContext() string {
	// Content search modal takes precedence (td-6ac70a)
	if p.contentSearchMode {
		return "conversations-content-search"
	}
	// Resume modal takes precedence (td-aa4136)
	if p.showResumeModal {
		return "conversations-resume-modal"
	}
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

// ConsumesTextInput reports whether conversation UI currently has a focused
// text-entry flow where app shortcuts should not intercept characters.
func (p *Plugin) ConsumesTextInput() bool {
	return p.searchMode || p.filterMode || p.contentSearchMode
}

// BlocksGlobalKeys reports whether a plugin-owned modal has keyboard focus.
func (p *Plugin) BlocksGlobalKeys() bool {
	return p.showResumeModal
}

// Diagnostics returns plugin health info.
func (p *Plugin) Diagnostics() []plugin.Diagnostic {
	status := "ok"
	detail := ""
	if len(p.adapters) == 0 {
		if p.detectingAdapters {
			// Detection runs off the startup path, so "no adapters" is not yet
			// a real answer during this window (td-9c7bf2).
			status = "loading"
			detail = "detecting adapters"
		} else {
			status = "disabled"
			detail = "no adapters"
		}
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
	watchStatus := "error"
	if p.watchChan != nil {
		watchStatus = "ok"
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

	return clip.CopyFrom(
		func() (string, tea.Msg) {
			return ExportSessionAsMarkdown(session, messages), app.FlashMsg{Text: "No session to copy"}
		},
		func(r clip.Result, _ string) tea.Msg {
			return app.FlashMsg{Text: r.Message("Session copied to clipboard")}
		},
	)
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

// AdaptersDetectedMsg reports the adapters that have sessions for this project.
// Detection runs off the startup path, so this arrives after the first frame.
type AdaptersDetectedMsg struct {
	Epoch         uint64
	Adapters      map[string]adapter.Adapter
	DiscoveryOnly map[string]bool
}

// GetEpoch implements plugin.EpochMessage.
func (m AdaptersDetectedMsg) GetEpoch() uint64 { return m.Epoch }

type SessionsLoadedMsg struct {
	Epoch    uint64 // Epoch when request was issued (for stale detection)
	Sessions []adapter.Session
	// Worktree cache data (td-0e43c080: computed in cmd, stored in Update)
	WorktreePaths []string
	WorktreeNames map[string]string
}

// GetEpoch implements plugin.EpochMessage.
func (m SessionsLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// SessionsRefreshedMsg carries only refreshed sessions as a delta update.
// Unlike SessionsLoadedMsg which replaces the entire session list, this merges
// into the current list to avoid overwriting concurrent updates from loadSessions.
type SessionsRefreshedMsg struct {
	Epoch     uint64
	Refreshed []adapter.Session // Only the sessions that were successfully refreshed
}

// GetEpoch implements plugin.EpochMessage.
func (m SessionsRefreshedMsg) GetEpoch() uint64 { return m.Epoch }

// LoadSettledMsg signals that session loading has settled (no new arrivals).
type LoadSettledMsg struct {
	Token int // Must match loadSettleToken to be valid
}

type MessagesLoadedMsg struct {
	Epoch      uint64 // Epoch when request was issued (for stale detection)
	SessionID  string
	Messages   []adapter.Message
	TotalCount int // Total message count before truncation (td-313ea851)
	Offset     int // Offset into the message list (td-313ea851)
}

// GetEpoch implements plugin.EpochMessage.
func (m MessagesLoadedMsg) GetEpoch() uint64 { return m.Epoch }

type WatchEventMsg struct {
	Epoch     uint64 // Epoch when request was issued (for stale detection)
	AdapterID string
	SessionID string // ID of the session that changed (empty for periodic refresh)
}

// GetEpoch implements plugin.EpochMessage.
func (m WatchEventMsg) GetEpoch() uint64 { return m.Epoch }

type WatchStartedMsg struct {
	Epoch   uint64
	Channel <-chan adapter.Event
	Closers []io.Closer
	Cancel  context.CancelFunc
	Manager *tieredwatcher.Manager
	Notices []string
}

// GetEpoch implements plugin.EpochMessage.
func (m WatchStartedMsg) GetEpoch() uint64 { return m.Epoch }

type ErrorMsg struct{ Err error }

type PreviewLoadMsg struct {
	Epoch     uint64 // Epoch when request was issued (for stale detection)
	Token     int
	SessionID string
}

// GetEpoch implements plugin.EpochMessage.
func (m PreviewLoadMsg) GetEpoch() uint64 { return m.Epoch }

type MessageReloadMsg struct {
	Epoch     uint64 // Epoch when request was issued (for stale detection)
	Token     int
	SessionID string
}

// GetEpoch implements plugin.EpochMessage.
func (m MessageReloadMsg) GetEpoch() uint64 { return m.Epoch }

// checkLargeSessionWarnings returns toast warnings for any large sessions not yet warned.
// Marks sessions as warned to avoid duplicate notifications.
func (p *Plugin) checkLargeSessionWarnings() tea.Cmd {
	var cmds []tea.Cmd
	for i := range p.sessions {
		s := &p.sessions[i]
		if s.FileSize < adapter.LargeSessionThreshold {
			continue
		}
		if p.warnedSessions[s.ID] {
			continue
		}
		p.warnedSessions[s.ID] = true

		level := s.SizeLevel()
		sizeMB := s.SizeMB()
		var msg string
		var isError bool
		var slow bool
		switch level {
		case 2: // Huge (500MB+)
			msg = fmt.Sprintf("⚠ Session %s (%.0fMB) - auto-reload disabled", s.Slug, sizeMB)
			isError = true
		case 1: // Large (100MB+)
			msg = fmt.Sprintf("Session %s (%.0fMB) - may be slow", s.Slug, sizeMB)
			isError = false
			// A slow-but-working session is a heads-up, not a record.
			slow = true
		}
		if msg != "" {
			if slow {
				cmds = append(cmds, app.ShowFlash(msg))
			} else {
				cmds = append(cmds, func() tea.Msg {
					return app.ToastMsg{Message: msg, Duration: 4 * time.Second, IsError: isError}
				})
			}
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	// Only show one warning at a time to avoid toast spam
	return cmds[0]
}

// checkPiDiscoveryToast returns a one-time toast when Pi sessions first appear (td-697e89).
func (p *Plugin) checkPiDiscoveryToast() tea.Cmd {
	if p.piDiscoveryToastShown {
		return nil
	}
	for _, s := range p.sessions {
		if s.AdapterID == "pi" {
			p.piDiscoveryToastShown = true
			// A one-time hint about a key, not an event to keep.
			return app.ShowFlash("Pi sessions found — press C to filter by category")
		}
	}
	return nil
}
