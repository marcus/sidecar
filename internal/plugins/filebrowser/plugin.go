package filebrowser

import (
	"os"
	"path/filepath"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/filefind"
	"github.com/marcus/sidecar/internal/image"
	"github.com/marcus/sidecar/internal/inlineedit"
	"github.com/marcus/sidecar/internal/livewatch"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/projectsearch"
	"github.com/marcus/sidecar/internal/queryfield"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/tabs"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	pluginID   = "file-browser"
	pluginName = "Files"
	pluginIcon = "F"

	// Directory cache limits (for path auto-complete)
	dirCacheMaxResults = 5 // Max suggestions to show

	// treePreviewQuiet keeps cursor movement live while avoiding a preview tab,
	// watcher, and file load for every event in a wheel or key-repeat burst.
	treePreviewQuiet = 80 * time.Millisecond
)

// FileOpMode represents the current file operation mode.
type FileOpMode int

const (
	FileOpNone FileOpMode = iota
	FileOpMove
	FileOpRename
	FileOpCreateFile
	FileOpCreateDir
	FileOpDelete
)

// Message types
type (
	// TreeBuiltMsg carries a tree built on a background goroutine. The plugin
	// swaps Tree in wholesale rather than mutating the live tree in place.
	TreeBuiltMsg struct {
		Tree       *FileTree
		Err        error
		Epoch      uint64
		Gen        uint64 // Build generation; only the newest build is applied
		CursorPath string // Path the cursor sat on when the build was requested
	}
	StateRestoredMsg struct {
		State state.FileBrowserState
	}
	WatchStartedMsg struct{ Watcher *TreeWatcher }
	// WatchEventMsg carries a coalesced batch of filesystem changes. Its fields
	// mirror FSEvent exactly so the watcher event converts directly.
	WatchEventMsg struct {
		TreeChanged    bool
		PreviewChanged bool
		Dirs           []string
	}
	// FileCacheBuiltMsg carries the result of a background quick-open cache
	// scan. Dirs distinguishes the path auto-complete scan from the file scan.
	FileCacheBuiltMsg = filefind.ScannedMsg
	// NavigateToFileMsg requests navigation to a specific file (from other plugins).
	NavigateToFileMsg = app.NavigateToFileMsg
	// RevealErrorMsg is sent when reveal in file manager fails.
	RevealErrorMsg = docview.RevealErrorMsg
	// FileOpErrorMsg is sent when a file operation fails.
	FileOpErrorMsg struct {
		Err error
	}
	// FileOpSuccessMsg is sent when a file operation succeeds.
	FileOpSuccessMsg struct {
		Src string
		Dst string
	}
	// CreateSuccessMsg is sent when a file/directory is created.
	CreateSuccessMsg struct {
		Path  string
		IsDir bool
	}
	// DeleteSuccessMsg is sent when a file/directory is deleted.
	DeleteSuccessMsg struct {
		Path string
	}
	// PasteSuccessMsg is sent when a file/directory is pasted.
	PasteSuccessMsg struct {
		Src string
		Dst string
	}
	// DragMoveResultMsg reports the outcome of a move started by a drag-drop.
	// It is deliberately a distinct type from FileOpSuccessMsg/FileOpErrorMsg:
	// a drag move has no file-op bar to render an error into, and a bare
	// "pending" flag on the plugin would let a concurrent rename's result be
	// reported as the drag's (and vice versa).
	DragMoveResultMsg struct {
		Name string // Basename that was moved, for the toast
		Dir  string // Destination directory, relative to the project root
		Err  error
	}
	// DragSpringLoadMsg fires once the cursor has rested long enough over a
	// collapsed directory during a drag, asking for it to auto-expand. Gen
	// identifies the hover it was scheduled for: the cursor has usually moved
	// on by the time it lands, and a stale tick must not expand a directory
	// the user is no longer pointing at.
	DragSpringLoadMsg struct {
		Gen uint64
	}
	// treePreviewQuietMsg activates only the file where a tree movement burst
	// came to rest. Generation and epoch keep an old timer from changing a newer
	// selection or project.
	treePreviewQuietMsg struct {
		Gen   uint64
		Epoch uint64
		Path  string
	}
	// GitInfoMsg contains git status for a file.
	GitInfoMsg = docview.GitInfoMsg
)

// GetEpoch implements plugin.EpochMessage for staleness detection.
func (m TreeBuiltMsg) GetEpoch() uint64 { return m.Epoch }

// ContentMatch represents a match position within file content.
type ContentMatch struct {
	LineNo   int // 0-indexed line number
	StartCol int // Start column (byte offset)
	EndCol   int // End column (byte offset)
}

// Plugin implements file browser functionality.
type Plugin struct {
	ctx     *plugin.Context
	tree    *FileTree
	focused bool

	// treeSourceOverride replaces the listing seam. Tests bind a fake so a
	// bound tree can be proven without ssh or a second machine; production
	// leaves it nil and treeSource builds one from the context.
	treeSourceOverride TreeSource
	// contentSourceOverride replaces the host document adapter, for the same
	// reason and on the same terms.
	contentSourceOverride contentpanes.Source
	// previewRevision is the host revision of the bytes the preview pane is
	// holding right now, and previewRevisionPath the file they came from.
	//
	// It is deliberately one entry rather than a per-path cache. A conditional
	// read is only honest while the pane still holds the bytes the host would
	// decline to resend; the pane holds one file at a time. Remembering a
	// revision for a file that has since been navigated away from is how a
	// revisited file came back as a filename over an empty pane (td-94d7f7).
	// It is set only immediately after the bytes it describes are installed,
	// and cleared by everything that replaces or drops them.
	previewRevision     string
	previewRevisionPath string

	// Pane state
	activePane       FocusPane
	treeVisible      bool // Toggle tree pane visibility with \
	showIgnored      bool // Toggle git-ignored file visibility with H
	paneFocusManaged bool // Set once an outer deck composes Files' pane focus.
	paneFocusActive  bool // Whether Files' inner active border should be drawn.

	// Tree state
	treeCursor    int
	treeScrollOff int

	// Search results manual scroll override. -1 (the default) follows the
	// cursor the way the view always has; a scrollbar gesture pins it until
	// keyboard navigation resumes. See scrollbar.go.
	searchScrollOff int

	// Preview state
	previewFile        string
	previewLines       []string
	previewHighlighted []string
	previewScroll      int
	previewError       error
	isBinary           bool
	isTruncated        bool
	previewSize        int64
	previewModTime     time.Time
	previewMode        os.FileMode

	// Tab state
	tabs      []FileTab
	activeTab int
	tabHits   []tabHit

	// Line wrapping state
	previewWrapEnabled bool // Wrap long lines instead of truncating

	// Markdown rendering state
	markdownRenderer   *markdown.Renderer // Shared Glamour renderer
	markdownRenderMode bool               // true=rendered, false=raw
	markdownRendered   []string           // Cached rendered lines

	// Image preview state
	imageRenderer *image.Renderer     // Terminal graphics renderer
	isImage       bool                // True if current preview is an image
	imageResult   *image.RenderResult // Cached render result for current image

	// Dimensions
	width, height int
	treeWidth     int
	previewWidth  int

	// Search state (tree filename search). The query is the app's shared
	// query field, so the bar edits like every other `/` bar: the caret moves,
	// alt+backspace deletes a word, home and end work, and a paste arrives
	// whole. searchQuery() reads it.
	searchMode  bool
	searchField queryfield.Field
	// searchClearRect / contentSearchClearRect are where each bar's × landed,
	// in the row's own coordinates, so the regions pass can register them.
	treeSearchClearRect    mouse.Rect
	contentSearchClearRect mouse.Rect
	searchMatches          []filefind.Match
	searchCursor           int

	// Auto-open state
	pendingOpenFile     string // Relative path to open after next tree rebuild
	pendingNavigatePath string
	pendingNavigateLine int
	pendingNavigateGen  uint64
	navigateGen         uint64

	// Content search state (preview pane)
	contentSearchMode      bool
	contentSearchCommitted bool // True after Enter confirms query (enables n/N navigation)
	contentSearchField     queryfield.Field
	contentSearchMatches   []ContentMatch
	contentSearchCursor    int // Index into contentSearchMatches

	// Text selection state (preview pane) - character-level via shared ui package
	selection ui.SelectionState

	// Quick open state. The finder owns the query, matches, cursor, and the
	// modal's rendering and input handling; the plugin owns only whether it is
	// showing and what opening a file means here.
	quickOpenMode bool
	finder        *filefind.Finder
	// quickOpen holds the cached project file list (relative paths) and its
	// scan bookkeeping. quickOpen and dirCache each own their own dirty flag: a
	// scan clears only its own, and a change arriving while that scan is in
	// flight re-sets it, so the landing result cannot pass itself off as
	// current. The stale cache keeps rendering until the next scan lands.
	quickOpen filefind.Cache

	// Project-wide search state (ctrl+s). The search owns its state, its modal,
	// and its input handling; the plugin owns only whether it is showing and
	// what opening a result means here.
	projectSearchMode bool
	projectSearch     *projectsearch.Search

	// Info modal state
	infoMode       bool
	infoModal      *modal.Modal
	infoModalWidth int
	gitStatus      string
	gitLastCommit  string

	// Blame view state
	blameMode       bool
	blameState      *BlameState
	blameModal      *modal.Modal // Modal instance
	blameModalWidth int          // Cached width for rebuild detection

	// File operation state (move/rename/create/delete)
	fileOpMode          FileOpMode
	fileOpTarget        *FileNode       // The file being operated on
	fileOpTextInput     textinput.Model // Text input for rename/move/create
	fileOpError         string          // Error message if operation failed
	fileOpConfirmCreate bool            // True when waiting for directory creation confirmation
	fileOpConfirmPath   string          // The directory path to create
	fileOpConfirmDelete bool            // True when waiting for delete confirmation
	fileOpButtonFocus   int             // Button focus: 0=input, 1=confirm, 2=cancel
	fileOpButtonHover   int             // Button hover: 0=none, 1=confirm, 2=cancel
	hoverDivider        bool            // Pointer is over the tree/preview handle
	hoverTabClose       tabs.CloseHover // Per-tab × under the pointer

	// Interactive scrollbar state (see scrollbar.go).
	bars             [sbViewCount]scrollbarBar // What the last render pass drew
	hoverScrollbar   scrollbarView             // Bar under the pointer
	dragScrollbar    scrollbarView             // Bar with a gesture in flight
	scrollbarGrabRow int                       // Track-local row the thumb was grabbed at

	// Line jump state (vim-style :<number>)
	lineJumpMode   bool
	lineJumpBuffer string

	// Path auto-complete state (for move modal)
	dirCache              filefind.Cache // Cached directory paths
	fileOpSuggestions     []string       // Current filtered suggestions
	fileOpSuggestionIdx   int            // Selected suggestion (-1 = none)
	fileOpShowSuggestions bool           // Show suggestions dropdown

	// Clipboard state (yank/paste)
	clipboardPath  string // Relative path of yanked file/directory
	clipboardIsDir bool   // Whether yanked item is a directory

	// File watcher
	watcher *TreeWatcher
	// previewLive gates watcher-driven re-reads of the previewed file: single
	// flight, host veto, and the fingerprint that keeps an unchanged re-read off
	// the screen. See live_preview.go.
	previewLive        livewatch.Refresher
	lastRefresh        time.Time // Debounce rapid refreshes on focus
	pendingAutoRefresh bool      // A watched change arrived while a modal/search/editor was open
	stopped            bool      // Set by Stop(); guards late WatchStartedMsg delivery
	treeBuildGen       uint64    // Newest requested tree build; older results are dropped

	// Mouse support
	mouseHandler *mouse.Handler
	wheelBursts  tty.WheelBursts
	wheelNow     func() time.Time

	// A held wheel event changed no visible state. Reuse exactly the preceding
	// same-dimension frame once instead of rebuilding both panes and hit maps.
	reuseViewOnce bool
	viewCache     string
	viewCacheW    int
	viewCacheH    int
	viewCacheOK   bool

	// treePreviewGen owns the one quiet-period preview activation in flight.
	// Every newer selection or direct tab activation invalidates the old timer.
	treePreviewGen uint64

	// State restoration flag
	stateRestored bool

	// Inline editor state (tmux-based editing). Session lifecycle, mouse and
	// cursor mapping, and the exit confirmation live in internal/inlineedit;
	// only filebrowser-specific state stays here.
	edit                inlineedit.Session
	inlineEditOrigMtime time.Time // Original file mtime (to detect changes)
	lastDragForwardTime time.Time // Throttle: last time a drag event was forwarded to tmux

	// Inline edit copy/paste hint state
	inlineEditCopyPasteHintShown bool // True after showing copy/paste hint toast

	// Selection copy hint state
	selectionCopyHintShown bool // True after showing selection copy hint toast

	// Drag-to-move state (tree rows). dragArmed means the button went down on a
	// tree row but the click-vs-drag threshold has not been crossed yet, so the
	// gesture is still a plain click. dragActive means it has been crossed and
	// the gesture is a real drag.
	//
	// dragSourcePath is the source of truth for *what* is being dragged, and
	// deliberately the only record of it: the watcher can rebuild the tree
	// mid-gesture and renumber every flat index, so a stored index would name a
	// different file. Everything that needs the source row looks it up by path
	// (tree.FindByPath / tree.IndexOfPath) at the moment it needs it.
	//
	// The -1 sentinels below are only established by New() and Init(); a Plugin
	// built as a bare literal (tests do this) has them at 0. Never treat an
	// index as "armed" on its own — always guard on dragArmed/dragActive first.
	dragSourcePath string // Path of the dragged node, "" when idle
	dragArmed      bool   // Pressed on a tree row, threshold not yet crossed
	dragActive     bool   // Threshold crossed, really dragging
	// dragDropIdx is the row to highlight as the drop target, and doubles as
	// the validity flag: -1 means "no valid drop here". dragDropDir (the
	// destination directory, relative to the project root, "" = the root
	// itself) is only meaningful while dragDropIdx >= 0 — "" is a legitimate
	// destination, so it can never signal invalidity on its own.
	dragDropIdx int
	dragDropDir string
	// dragHoverIdx / dragHoverSince drive spring-loaded folders: how long the
	// cursor has rested on one row. dragHoverGen invalidates the tick
	// scheduled for a row the cursor has since left.
	dragHoverIdx   int
	dragHoverSince time.Time
	dragHoverGen   uint64
	// dragLastScroll throttles edge auto-scroll: motion events arrive far
	// faster than a readable scroll rate.
	dragLastScroll time.Time
}

// New creates a new File Browser plugin.
func New() *Plugin {
	p := &Plugin{
		mouseHandler:    mouse.NewHandler(),
		imageRenderer:   image.New(), // Detect terminal graphics protocol once
		treeVisible:     true,        // Tree pane visible by default
		showIgnored:     true,        // Show git-ignored files by default
		dragDropIdx:     -1,
		dragHoverIdx:    -1,
		searchScrollOff: -1, // -1 = search results follow the cursor
	}
	p.edit.Model = tty.New(nil) // Inline editor with default config
	p.edit.Host = p
	p.applyInlineEditorAttachKey()
	return p
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return pluginID }

// Name returns the plugin display name.
func (p *Plugin) Name() string { return pluginName }

// Icon returns the plugin icon character.
func (p *Plugin) Icon() string { return pluginIcon }

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	// Reject editor start/exit messages still queued from the previous project.
	p.edit.Activation++
	p.treePreviewGen++
	p.wheelBursts = tty.WheelBursts{}
	p.reuseViewOnce = false
	p.viewCacheOK = false
	if p.edit.Model != nil {
		p.edit.Model.Close()
	}
	p.edit.Host = p
	p.ctx = ctx
	if ctx != nil && ctx.HostID != "" {
		// The caches, the search, and the drag all describe the project being
		// left. Reset them exactly as a local reinit does, then build a tree
		// only when the bind can actually list the host; when it cannot, the
		// unavailable view says which of the three reasons applies.
		p.stateRestored = false
		p.stopped = false
		p.pendingAutoRefresh = false
		p.searchScrollOff = -1
		p.clearDragState()
		p.quickOpen.Reset()
		p.dirCache.Reset()
		p.quickOpenMode = false
		p.closeProjectSearch()
		p.tree = nil
		if source := p.treeSource(); source != nil {
			p.tree = NewFileTreeWithSource(p.remoteRoot(), source)
		}
		renderer, err := markdown.NewRenderer()
		if err != nil && ctx.Logger != nil {
			ctx.Logger.Warn("markdown renderer init failed", "error", err)
		}
		p.markdownRenderer = renderer
		if saved := state.GetFileBrowserTreeWidth(); saved > 0 {
			p.treeWidth = saved
		}
		p.previewWrapEnabled = state.GetLineWrapEnabled()
		return nil
	}
	p.tree = NewFileTree(ctx.WorkDir)

	// Reset state flags for reinit support (project switching)
	p.stateRestored = false
	p.stopped = false
	p.pendingAutoRefresh = false
	p.searchScrollOff = -1
	p.clearDragState()

	// The quick-open caches describe the old project's disk; drop them, along
	// with any search still open over it — a live project search also has a
	// ripgrep process running in a directory the plugin is leaving.
	p.quickOpen.Reset()
	p.dirCache.Reset()
	p.quickOpenMode = false
	p.closeProjectSearch()

	// Initialize markdown renderer
	renderer, err := markdown.NewRenderer()
	if err != nil {
		ctx.Logger.Warn("markdown renderer init failed", "error", err)
	}
	p.markdownRenderer = renderer

	// Load saved pane width from state
	if saved := state.GetFileBrowserTreeWidth(); saved > 0 {
		p.treeWidth = saved
	}
	p.previewWrapEnabled = state.GetLineWrapEnabled()
	p.applyInlineEditorAttachKey()
	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	if p.remoteBound() {
		// No watcher: internal/livewatch is a filesystem signal and does not
		// cross the host boundary. A bound tree refreshes on request and on
		// the host snapshot the viewer already receives, and says nothing
		// about being live that it cannot keep.
		return p.refresh()
	}
	return tea.Batch(
		p.refresh(),
		p.startWatcher(),
	)
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	p.stopped = true
	p.treePreviewGen++
	p.wheelBursts = tty.WheelBursts{}
	if p.watcher != nil {
		p.watcher.Stop()
		p.watcher = nil
	}
	// Kill any active inline edit sessions
	p.cleanupAllEditSessions()
	// Save state on shutdown
	p.saveState()
}

// saveState persists the current file browser state to disk.
func (p *Plugin) saveState() {
	if p.tree == nil {
		return
	}

	p.saveActiveTabState()

	// Get expanded directory paths
	expandedPaths := p.tree.GetExpandedPaths()
	expandedList := make([]string, 0, len(expandedPaths))
	for path := range expandedPaths {
		expandedList = append(expandedList, path)
	}

	// Determine selected file
	var selectedFile string
	if node := p.tree.GetNode(p.treeCursor); node != nil {
		selectedFile = node.Path
	}

	// Determine active pane string
	activePane := "tree"
	if p.activePane == PanePreview {
		activePane = "preview"
	}

	tabStates := make([]state.FileBrowserTabState, 0, len(p.tabs))
	for _, tab := range p.tabs {
		if tab.Path == "" || tab.IsPreview {
			continue
		}
		tabStates = append(tabStates, state.FileBrowserTabState{
			Path:   tab.Path,
			Scroll: tab.Scroll,
		})
	}

	activeTab := p.activeTab
	if activeTab < 0 {
		activeTab = 0
	} else if activeTab >= len(tabStates) && len(tabStates) > 0 {
		activeTab = len(tabStates) - 1
	}

	fbState := state.FileBrowserState{
		SelectedFile:  selectedFile,
		TreeScroll:    p.treeScrollOff,
		PreviewScroll: p.previewScroll,
		ExpandedDirs:  expandedList,
		ActivePane:    activePane,
		PreviewFile:   p.previewFile,
		TreeCursor:    p.treeCursor,
		ShowIgnored:   &p.showIgnored,
		Tabs:          tabStates,
		ActiveTab:     activeTab,
	}

	if err := state.SetFileBrowserState(p.ctx.WorkDir, fbState); err != nil {
		p.ctx.Logger.Error("file browser: failed to save state", "error", err)
	}
}

// restoreState loads saved file browser state from disk.
func (p *Plugin) restoreState() tea.Cmd {
	workDir := p.ctx.WorkDir
	projectRoot := p.ctx.ProjectRoot
	return func() tea.Msg {
		fbState := state.GetFileBrowserStateForWorkDir(workDir, projectRoot)
		return StateRestoredMsg{State: fbState}
	}
}

// startWatcher initializes the file system watcher.
func (p *Plugin) startWatcher() tea.Cmd {
	logger := p.ctx.Logger
	return func() tea.Msg {
		watcher, err := NewTreeWatcher()
		if err != nil {
			logger.Error("file browser: watcher failed", "error", err)
			return nil
		}
		return WatchStartedMsg{Watcher: watcher}
	}
}

// listenForWatchEvents waits for the next file system event. The watcher is
// captured here so the command never reads plugin state off the update goroutine.
func (p *Plugin) listenForWatchEvents() tea.Cmd {
	watcher := p.watcher
	if watcher == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-watcher.Events()
		if !ok {
			return nil // Watcher stopped
		}
		return WatchEventMsg(ev)
	}
}

// handleWatchStarted adopts a watcher created off the update goroutine.
func (p *Plugin) handleWatchStarted(msg WatchStartedMsg) (plugin.Plugin, tea.Cmd) {
	// The plugin may have been stopped while the watcher was starting.
	if p.stopped {
		msg.Watcher.Stop()
		return p, nil
	}
	// A project switch runs Stop -> Init -> Start, so a watcher from the
	// previous Start can still land after Init cleared p.stopped. Whichever one
	// is not adopted has to be stopped, or its goroutine and its descriptors
	// live for the rest of the process.
	if p.watcher != nil && p.watcher != msg.Watcher {
		p.watcher.Stop()
	}
	p.watcher = msg.Watcher
	p.updateWatchedFile()
	p.syncWatcherDirs()
	return p, p.listenForWatchEvents()
}

// handleWatchEvent applies a coalesced batch of filesystem changes and re-arms
// the listener.
func (p *Plugin) handleWatchEvent(msg WatchEventMsg) (plugin.Plugin, tea.Cmd) {
	cmds := []tea.Cmd{p.listenForWatchEvents()}
	if msg.PreviewChanged {
		if cmd := p.refreshPreview(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// A background tab's file may have been rewritten in place, which is not a
	// tree change but does make the tab's cached content wrong.
	p.invalidateTabsInDirs(msg.Dirs)
	if msg.TreeChanged && autoRefreshEnabled() {
		// Caches that describe the disk are now behind it, whether or not the
		// rebuild itself can run right now.
		p.quickOpen.MarkDirty()
		p.dirCache.MarkDirty()
		if cmd := p.requestAutoRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return p, tea.Batch(cmds...)
}

// updateWatchedFile updates the file watcher to watch the current preview file.
func (p *Plugin) updateWatchedFile() {
	if p.watcher == nil {
		return
	}
	if p.previewFile != "" {
		_ = p.watcher.SetPreviewFile(filepath.Join(p.ctx.WorkDir, p.previewFile))
	} else {
		_ = p.watcher.SetPreviewFile("")
	}
}

// autoRefreshEnabled reports whether watched directories should drive tree refreshes.
func autoRefreshEnabled() bool {
	return features.IsEnabled(features.FilesAutoRefresh.Name)
}

// syncWatcherDirs points the watcher at the root plus every expanded directory,
// in visible order so the watcher's cap keeps the directories nearest the top.
func (p *Plugin) syncWatcherDirs() {
	if p.watcher == nil {
		return
	}
	if p.tree == nil || !autoRefreshEnabled() {
		p.watcher.SyncDirs(nil)
		return
	}

	dirs := make([]string, 0, len(p.tree.FlatList)+1)
	dirs = append(dirs, p.tree.RootDir)
	for _, node := range p.tree.FlatList {
		if node.IsDir && node.IsExpanded {
			dirs = append(dirs, filepath.Join(p.tree.RootDir, node.Path))
		}
	}
	p.watcher.SyncDirs(dirs)
}

// autoRefreshBlocked reports whether a tree rebuild would disrupt what the user
// is doing: rebuilding under a modal, a search, or the inline editor would move
// the ground out from under it.
func (p *Plugin) autoRefreshBlocked() bool {
	return p.ConsumesTextInput() ||
		p.infoMode ||
		p.blameMode ||
		p.edit.ShowExitConfirm
}

// requestAutoRefresh refreshes the tree, or defers it until the user is done.
func (p *Plugin) requestAutoRefresh() tea.Cmd {
	if p.autoRefreshBlocked() {
		p.pendingAutoRefresh = true
		return nil
	}
	p.pendingAutoRefresh = false
	p.lastRefresh = time.Now()
	return p.refresh()
}

// refresh rebuilds the file tree, preserving expanded state.
//
// Everything the build needs is snapshotted here, on the update goroutine, so
// the returned command only ever touches its own copies. The rebuilt tree is
// swapped in when TreeBuiltMsg is handled; p.tree is never mutated in the
// background.
func (p *Plugin) refresh() tea.Cmd {
	if p.tree == nil {
		return nil
	}
	if p.remoteBound() && !p.remoteAvailable() {
		return nil
	}

	spec := BuildSpec{
		RootDir:       p.tree.RootDir,
		SortMode:      p.tree.SortMode,
		ShowIgnored:   p.tree.ShowIgnored,
		ExpandedPaths: p.tree.GetExpandedPaths(),
		Source:        p.treeSource(),
	}

	var cursorPath string
	if node := p.tree.GetNode(p.treeCursor); node != nil {
		cursorPath = node.Path
	}

	var epoch uint64
	if p.ctx != nil {
		epoch = p.ctx.Epoch
	}

	// Builds of the same project share an epoch, so they need their own
	// ordering: a slow earlier build must not land on top of a faster later one.
	p.treeBuildGen++
	gen := p.treeBuildGen

	return func() tea.Msg {
		tree, err := BuildTree(spec)
		return TreeBuiltMsg{Tree: tree, Err: err, Epoch: epoch, Gen: gen, CursorPath: cursorPath}
	}
}

// applyBuiltTree swaps in a freshly built tree, re-anchoring anything that held
// a pointer into the tree that just got replaced.
//
// The build ran off the update goroutine, so the user may have expanded,
// collapsed, or moved the cursor while it was in flight. Live state wins over
// the snapshot the build started from; otherwise a directory the user just
// opened snaps shut when the rebuild lands.
func (p *Plugin) applyBuiltTree(tree *FileTree, cursorPath string) {
	if p.tree != nil {
		tree.SetExpandedPaths(p.tree.GetExpandedPaths())
		if node := p.tree.GetNode(p.treeCursor); node != nil {
			cursorPath = node.Path
		}
	}
	p.tree = tree
	p.reanchorTreeCursor(cursorPath)
	p.reresolveFileOpTarget()
	p.reanchorDragSource()
}

// reanchorDragSource re-checks an in-flight drag against the tree that just
// replaced the one it was armed against. The dragged node is tracked by path,
// so it survives the renumbering a rebuild causes (a build dropping a file, a
// branch switch, `go mod tidy`); if the dragged path is gone from the tree
// entirely, the gesture is cancelled.
func (p *Plugin) reanchorDragSource() {
	if !p.dragArmed && !p.dragActive {
		return
	}
	if p.dragSourcePath == "" {
		p.clearDragState()
		return
	}
	if p.tree.IndexOfPath(p.dragSourcePath) < 0 {
		p.clearDragState()
		return
	}
	// The drop target is recomputed from the pointer on the next motion event;
	// its old index means nothing in the new tree. The same goes for the
	// spring-load hover, whose row may now be a different directory entirely.
	p.dragDropIdx = -1
	p.dragDropDir = ""
	p.dragHoverIdx = -1
	p.dragHoverGen++
	p.dragHoverSince = time.Now()
}

// reanchorTreeCursor puts the cursor back on the node it was on before the
// rebuild. If that path is gone (deleted, or hidden by a collapsed parent) the
// old index is kept and clamped to the new tree.
func (p *Plugin) reanchorTreeCursor(cursorPath string) {
	if cursorPath != "" {
		if idx := p.tree.IndexOfPath(cursorPath); idx >= 0 {
			p.treeCursor = idx
			p.ensureTreeCursorVisible()
			return
		}
	}
	p.clampTreeCursor()
}

// clampTreeCursor keeps the cursor inside the current flat list.
func (p *Plugin) clampTreeCursor() {
	if p.treeCursor >= p.tree.Len() {
		p.treeCursor = p.tree.Len() - 1
	}
	if p.treeCursor < 0 {
		p.treeCursor = 0
	}
	p.ensureTreeCursorVisible()
}

// reresolveFileOpTarget re-points an in-flight file operation at the equivalent
// node in the new tree. If the path is not visible in the new tree the old node
// is kept: it may simply live under a collapsed directory, and operations only
// read its Path/Name/IsDir.
func (p *Plugin) reresolveFileOpTarget() {
	if p.fileOpTarget == nil {
		return
	}
	if node := p.tree.FindByPath(p.fileOpTarget.Path); node != nil {
		p.fileOpTarget = node
	}
}

// Update handles messages. A tree refresh deferred while a modal, search, or the
// inline editor was open is flushed here, once the message that closed it has
// been handled.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	if p.remoteBound() && (p.tree == nil || !p.remoteAvailable()) {
		return p, nil
	}
	updated, cmd := p.update(msg)
	if p.pendingAutoRefresh && !p.autoRefreshBlocked() {
		if refreshCmd := p.requestAutoRefresh(); refreshCmd != nil {
			return updated, tea.Batch(cmd, refreshCmd)
		}
	}
	return updated, cmd
}

func (p *Plugin) update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	// The watcher's listen loop is one-shot: whoever handles an event has to
	// re-arm it. Both are handled before the early returns below, because a
	// single event swallowed by a modal or the inline editor would kill
	// auto-refresh for the rest of the session.
	switch msg := msg.(type) {
	case WatchStartedMsg:
		return p.handleWatchStarted(msg)
	case WatchEventMsg:
		return p.handleWatchEvent(msg)
	case treePreviewQuietMsg:
		return p.handleTreePreviewQuiet(msg)
	}

	// Handle exit confirmation dialog first
	if p.edit.ShowExitConfirm {
		if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
			switch keyMsg.String() {
			case "j", "down":
				p.edit.MoveConfirmSelection(1)
				return p, nil
			case "k", "up":
				p.edit.MoveConfirmSelection(-1)
				return p, nil
			case "enter":
				return p.handleExitConfirmationChoice()
			case "esc", "q":
				// Cancel - return to editing
				p.edit.ShowExitConfirm = false
				p.edit.ClearPendingClick()
				return p, nil
			}
		}
		return p, nil
	}

	// Handle inline edit mode - delegate most messages to tty model
	if p.edit.Active && p.edit.Model != nil {
		// Check if editor became inactive (vim exited normally)
		// Also check if tmux session died (handles :wq case before SessionDeadMsg arrives)
		if !p.edit.Model.IsActive() || !p.isInlineEditSessionAlive() {
			editedFile := p.edit.Path // Save before exitInlineEditMode clears it
			p.exitInlineEditMode()
			// Refresh preview to show updated file
			if editedFile != "" {
				return p, p.loadPreview(editedFile)
			}
			return p, p.refresh()
		}

		switch msg := msg.(type) {
		case tea.WindowSizeMsg:
			p.width = msg.Width
			p.height = msg.Height
			return p, p.edit.Model.Resize(p.calculateInlineEditorWidth(), p.calculateInlineEditorHeight())

		case tea.MouseMsg:
			// Route mouse through handleMouse for click-away detection
			return p.handleMouse(msg)

		case tea.KeyPressMsg:
			// Intercept copy key before delegating to tty model
			if msg.String() == p.getInlineEditCopyKey() {
				return p, p.copyInlineEditorOutputCmd()
			}
			cmd := p.edit.Model.Update(msg)
			// Check if editor exited
			if !p.edit.Model.IsActive() {
				p.exitInlineEditMode()
				return p, tea.Batch(cmd, p.refresh())
			}
			return p, cmd

		case tty.EscapeTimerMsg, tty.CaptureResultMsg,
			tty.PollTickMsg, tty.PaneResizedMsg, tty.SessionDeadMsg, tty.PasteResultMsg:
			cmd := p.edit.Model.Update(msg)
			// Check if editor exited
			if !p.edit.Model.IsActive() {
				p.exitInlineEditMode()
				return p, tea.Batch(cmd, p.refresh())
			}
			return p, cmd

		default:
			// The shared terminal component owns its model/control listener
			// messages. Route unknown messages through it so plugins do not need
			// transport-specific branches as that private protocol evolves.
			if cmd := p.edit.Model.Update(msg); cmd != nil {
				return p, cmd
			}
		}
	}

	switch msg := msg.(type) {
	case app.PluginFocusedMsg:
		// Refresh tree when plugin gains focus to pick up external file changes
		if time.Since(p.lastRefresh) < 500*time.Millisecond {
			return p, nil
		}
		p.lastRefresh = time.Now()
		return p, p.refresh()

	case tea.MouseMsg:
		return p.handleMouse(msg)

	case appmsg.ThemeChangedMsg:
		// Presentation-only invalidation: rendered markdown carries theme
		// colours, so it must be rebuilt while preserving user state.
		p.handleThemeChanged()

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		// Invalidate markdown cache when size changes (width affects rendering)
		if p.markdownRenderMode && p.isMarkdownFile() {
			p.markdownRendered = nil
			p.renderMarkdownContent()
			if p.contentSearchMode && p.contentSearchQuery() != "" {
				p.updateContentMatches()
			}
		}
		// Invalidate image cache when size changes (will re-render at new size)
		p.imageResult = nil

	case TreeBuiltMsg:
		// Drop trees built for a project we've since switched away from.
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		// Two rebuilds can be in flight at once (the watcher fires while an
		// earlier build is still walking); only the newest result is current.
		if msg.Gen != 0 && msg.Gen != p.treeBuildGen {
			return p, nil
		}
		if msg.Err != nil {
			p.ctx.Logger.Error("tree build failed", "error", msg.Err)
		} else if msg.Tree != nil {
			p.applyBuiltTree(msg.Tree, msg.CursorPath)
			p.syncWatcherDirs()
		}
		// Handle pending auto-open from file creation
		if p.pendingOpenFile != "" {
			path := p.pendingOpenFile
			p.pendingOpenFile = "" // Clear immediately to avoid re-processing
			_, navCmd := p.navigateToFile(path)
			// Restore state after first tree build
			if !p.stateRestored {
				p.stateRestored = true
				return p, tea.Batch(navCmd, p.restoreState())
			}
			return p, navCmd
		}
		// Restore state after first tree build
		if !p.stateRestored {
			p.stateRestored = true
			return p, p.restoreState()
		}

	case StateRestoredMsg:
		// Apply restored state
		fbState := msg.State

		// Restore expanded directories
		if len(fbState.ExpandedDirs) > 0 {
			expandedPaths := make(map[string]bool, len(fbState.ExpandedDirs))
			for _, path := range fbState.ExpandedDirs {
				expandedPaths[path] = true
			}
			p.tree.RestoreExpandedPaths(expandedPaths)
			p.syncWatcherDirs()
		}

		// Restore ignored file visibility (nil = default true)
		if fbState.ShowIgnored != nil {
			p.showIgnored = *fbState.ShowIgnored
			p.tree.ShowIgnored = p.showIgnored
			p.tree.Flatten()
		}

		// Restore tree cursor position
		if fbState.TreeCursor > 0 && fbState.TreeCursor < p.tree.Len() {
			p.treeCursor = fbState.TreeCursor
			p.ensureTreeCursorVisible()
		}

		// Restore scroll offsets
		if fbState.TreeScroll > 0 {
			p.treeScrollOff = fbState.TreeScroll
		}
		// The saved offset came from a session that may have had a different
		// pane height or tree size; a tree that now fits must draw from the top.
		p.clampTreeScroll()

		// Restore active pane
		if fbState.ActivePane == "preview" {
			p.activePane = PanePreview
		}

		// Restore tabs and preview file
		p.tabs = nil
		if len(fbState.Tabs) > 0 {
			for _, tab := range fbState.Tabs {
				if tab.Path == "" {
					continue
				}
				p.tabs = append(p.tabs, FileTab{Path: tab.Path, Scroll: tab.Scroll})
			}
		} else if fbState.PreviewFile != "" {
			p.tabs = append(p.tabs, FileTab{Path: fbState.PreviewFile, Scroll: fbState.PreviewScroll})
		}

		if len(p.tabs) > 0 {
			p.activeTab = fbState.ActiveTab
			if p.activeTab < 0 || p.activeTab >= len(p.tabs) {
				p.activeTab = 0
			}
			p.previewFile = p.tabs[p.activeTab].Path
			p.previewScroll = p.tabs[p.activeTab].Scroll
			p.updateWatchedFile()
			return p, p.loadPreview(p.previewFile)
		}

		p.previewFile = ""
		p.previewScroll = 0

	case previewRefreshedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		return p, p.applyPreviewRefresh(msg)

	case plugin.HostInventoryMsg:
		// The host's snapshot moved. That is the whole of a bound tree's live
		// refresh: there is no filesystem watch across the boundary, so this
		// signal and an explicit r are what it has, and the auto-refresh gate
		// defers it while a modal or an open search is on screen exactly as a
		// local watcher signal is deferred.
		if !p.remoteBound() {
			return p, nil
		}
		return p, p.requestAutoRefresh()

	case remotePreviewLoadedMsg:
		// A host read is the same payload with a revision attached.
		// Re-entering as the ordinary message is what keeps one preview
		// pipeline rather than two.
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		next, cmd := p.update(msg.Msg)
		// The revision describes bytes, so it is recorded after those bytes
		// land and only if they did. A read the user navigated away from
		// leaves nothing behind for a later read to be conditional against.
		if msg.Msg.Path == p.previewFile {
			p.rememberPreviewRevision(msg.Msg.Path, msg.Revision)
		}
		return next, cmd

	case remotePreviewUnchangedMsg:
		// The pane already holds these bytes: no repaint, and the refresh gate
		// still counts the read so a later signal is not reported as a change.
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.Path != p.previewFile {
			return p, nil
		}
		p.rememberPreviewRevision(msg.Path, msg.Revision)
		return p, nil

	case PreviewLoadedMsg:
		// Check for stale message from previous project context
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.Path == p.previewFile {
			// An explicit load defines what is on screen; the refresh gate
			// measures from here so the next watcher signal is not reported as
			// a change.
			p.adoptPreviewFingerprint(msg.Result)
			p.applyPreviewResult(msg.Result)
			p.updateActiveTabResult(msg.Result)
			p.clampPreviewScroll()
			if p.pendingNavigateLine > 0 && p.pendingNavigatePath == msg.Path &&
				p.pendingNavigateGen == msg.NavigateGeneration {
				p.previewScroll = p.pendingNavigateLine - 1
				p.clampPreviewScroll()
				if p.activeTab >= 0 && p.activeTab < len(p.tabs) {
					p.tabs[p.activeTab].Scroll = p.previewScroll
				}
				p.pendingNavigateLine = 0
				p.pendingNavigatePath = ""
				p.pendingNavigateGen = 0
			}

			// Re-run search if still in search mode (e.g., navigating files with j/k)
			if p.contentSearchMode && p.contentSearchQuery() != "" {
				targetScroll := p.previewScroll
				p.updateContentMatches()
				// Jump to match nearest the target line from project search
				if targetScroll > 0 && len(p.contentSearchMatches) > 0 {
					p.scrollToNearestMatch(targetScroll)
				}
			}
		}

	case app.RefreshMsg:
		p.lastRefresh = time.Now()
		return p, p.refresh()

	case FileCacheBuiltMsg:
		// Drop scans of a project we've since switched away from.
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.Dirs {
			p.dirCache.Apply(msg)
			// The move modal filtered an empty cache on the keystroke that
			// started this scan; recompute so the dropdown appears without
			// needing another one. A dropdown the user already dismissed by
			// accepting a suggestion stays dismissed.
			if p.fileOpShowSuggestions || len(p.fileOpSuggestions) == 0 {
				return p, p.updateFileOpSuggestions()
			}
			return p, nil
		}
		// The finder shares this cache, so applying the scan through it keeps
		// its matches in step with the file list they were computed from.
		p.fileFinder().Update(msg)
		if p.searchMode {
			// The cache is fresh, so this only re-filters, and it keeps the
			// user's selection: the scan landing is not a new query.
			p.refilterSearchMatches()
		}
		return p, nil

	case NavigateToFileMsg:
		p.navigateGen++
		if msg.Line > 0 {
			p.pendingNavigatePath = msg.Path
			p.pendingNavigateLine = msg.Line
			p.pendingNavigateGen = p.navigateGen
		} else {
			p.pendingNavigatePath = ""
			p.pendingNavigateLine = 0
			p.pendingNavigateGen = 0
		}
		updated, cmd := p.navigateToFile(msg.Path)
		if cmd == nil {
			p.pendingNavigatePath = ""
			p.pendingNavigateLine = 0
			p.pendingNavigateGen = 0
			return updated, nil
		}
		generation := p.navigateGen
		return updated, func() tea.Msg {
			result := cmd()
			if loaded, ok := result.(PreviewLoadedMsg); ok {
				loaded.NavigateGeneration = generation
				return loaded
			}
			return result
		}

	case RevealErrorMsg:
		p.ctx.Logger.Error("file browser: reveal failed", "error", msg.Err)

	case DragSpringLoadMsg:
		return p.handleDragSpringLoad(msg)

	case FileOpErrorMsg:
		p.fileOpError = msg.Err.Error()

	case FileOpSuccessMsg:
		// Clear file operation state and refresh
		p.fileOpMode = FileOpNone
		p.fileOpTarget = nil
		p.fileOpError = ""
		return p, p.refresh()

	case DragMoveResultMsg:
		// A drag-drop move has no file-op bar to render an error into, so both
		// outcomes are surfaced as a toast.
		if msg.Err != nil {
			return p, appmsg.ShowToast("Move failed: "+msg.Err.Error(), 3*time.Second)
		}
		return p, tea.Batch(
			p.refresh(),
			// The browser already shows the move (audit row 66).
			appmsg.ShowFlash("Moved "+msg.Name+" → "+displayDropDir(msg.Dir)),
		)

	case CreateSuccessMsg:
		// Clear file operation state and refresh
		p.fileOpMode = FileOpNone
		p.fileOpTarget = nil
		p.fileOpError = ""
		// If we created a file (not directory), schedule auto-open after tree refresh
		if !msg.IsDir {
			if relPath, err := filepath.Rel(p.ctx.WorkDir, msg.Path); err == nil {
				p.pendingOpenFile = relPath
			}
		}
		return p, p.refresh()

	case DeleteSuccessMsg:
		// Clear file operation state and refresh
		p.fileOpMode = FileOpNone
		p.fileOpTarget = nil
		p.fileOpError = ""
		p.fileOpConfirmDelete = false
		// Clean up tabs for the deleted file/directory
		closeCmd := p.closeTabsForPath(msg.Path)
		return p, tea.Batch(closeCmd, p.refresh())

	case PasteSuccessMsg:
		// Refresh after paste
		return p, p.refresh()

	case GitInfoMsg:
		p.gitStatus = msg.Status
		p.gitLastCommit = msg.LastCommit
		return p, nil

	case BlameLoadedMsg:
		// Check for stale message from previous project context
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if p.blameState != nil {
			p.blameState.IsLoading = false
			if msg.Error != nil {
				p.blameState.Error = msg.Error
			} else {
				p.blameState.Lines = msg.Lines
			}
			// The modal's sections read blameState live, so its cached layout
			// bounds no longer describe the content that will be rendered.
			if p.blameModal != nil {
				p.blameModal.Invalidate()
			}
		}
		return p, nil

	case projectsearch.DebounceMsg:
		// The search itself decides whether this tick is still the newest one.
		if search := p.projectSearchSurface(); search != nil {
			return p, search.Update(msg)
		}
		return p, nil

	case projectsearch.ResultsMsg:
		// Check for stale message from previous project context
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if search := p.projectSearchSurface(); search != nil {
			search.Apply(msg)
		}

	case InlineEditStartedMsg:
		if !p.ownsInlineEditMessage(msg.Activation, msg.Epoch) {
			return p, p.cleanupStaleInlineEditStart(msg)
		}
		return p, p.handleInlineEditStarted(msg)

	case InlineEditExitedMsg:
		if !p.ownsInlineEditMessage(msg.Activation, msg.Epoch) {
			return p, nil
		}
		// Check if there was a pending click action (from Save & Exit)
		if p.edit.PendingClickRegion != "" {
			return p.processPendingClickAction()
		}
		// Normal exit - refresh preview after editing
		if msg.FilePath != "" {
			return p, p.loadPreview(msg.FilePath)
		}

	case tea.PasteMsg:
		// A live search bar is a text input: a bracketed paste lands in it
		// exactly as typed characters do. Nothing else in this plugin takes a
		// paste, so an unclaimed one is simply dropped as before.
		if handled, cmd := p.handleSearchPaste(msg); handled {
			return p, cmd
		}

	case tea.KeyPressMsg:
		return p.handleKey(msg)
	}

	return p, nil
}

// View renders the plugin.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height
	if p.reuseViewOnce && p.viewCacheOK && p.viewCacheW == width && p.viewCacheH == height {
		p.reuseViewOnce = false
		return p.viewCache
	}
	p.reuseViewOnce = false
	content := p.renderView()
	// Constrain output to allocated height to prevent header scrolling off-screen.
	// MaxHeight truncates content that exceeds the allocated space.
	view := lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
	p.viewCache = view
	p.viewCacheW = width
	p.viewCacheH = height
	p.viewCacheOK = true
	return view
}

// IsFocused returns whether the plugin is focused.
func (p *Plugin) IsFocused() bool { return p.focused }

// SetFocused sets the focus state.
func (p *Plugin) SetFocused(f bool) {
	// Losing focus (a plugin switch, which can happen on a key the plugin never
	// sees) ends any drag gesture: the release will be delivered somewhere else,
	// if at all.
	if !f {
		p.clearDragState()
	}
	p.focused = f
	if p.edit.Model != nil {
		p.edit.Model.SetFocused(f)
	}
}

// Commands returns the available commands.
func (p *Plugin) Commands() []plugin.Command {
	if p.remoteBound() {
		// The footer says what this surface can do, not what Files can do
		// somewhere else. A hint for a command that answers "unavailable on
		// [aerie]" is worse than no hint.
		return p.remoteCommands()
	}
	return p.localCommands()
}

// localCommands is the full command set, for a project on this machine.
func (p *Plugin) localCommands() []plugin.Command {
	return []plugin.Command{
		// Tree pane commands
		{ID: "quick-open", Name: "Find", Description: "Find a file by name", Category: plugin.CategorySearch, Context: "file-browser-tree", Priority: 1},
		{ID: "new-tab", Name: "Tab+", Description: "Open file in new tab", Category: plugin.CategoryNavigation, Context: "file-browser-tree", Priority: 2},
		{ID: "project-search", Name: "Search", Description: "Search the project's contents", Category: plugin.CategorySearch, Context: "file-browser-tree", Priority: 2},
		{ID: "info", Name: "Info", Description: "Show file info", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 2},
		{ID: "edit", Name: "Edit", Description: "Edit file inline", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 2},
		{ID: "edit-external", Name: "Edit+", Description: "Edit in full terminal", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 2},
		{ID: "blame", Name: "Blame", Description: "Show git blame", Category: plugin.CategoryView, Context: "file-browser-tree", Priority: 3},
		{ID: "search", Name: "Filter", Description: "Filter files by name", Category: plugin.CategorySearch, Context: "file-browser-tree", Priority: 3},
		{ID: "close-tab", Name: "Close", Description: "Close active tab", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 4},
		{ID: "create-file", Name: "New", Description: "Create new file", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 4},
		{ID: "create-dir", Name: "Mkdir", Description: "Create new directory", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 4},
		{ID: "delete", Name: "Delete", Description: "Delete file or directory", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 4},
		{ID: "prev-tab", Name: "Tab←", Description: "Previous tab", Category: plugin.CategoryNavigation, Context: "file-browser-tree", Priority: 5},
		{ID: "next-tab", Name: "Tab→", Description: "Next tab", Category: plugin.CategoryNavigation, Context: "file-browser-tree", Priority: 5},
		{ID: "yank", Name: "Yank", Description: "Mark file for copy (use p to paste)", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 5},
		{ID: "copy-path", Name: "CopyPath", Description: "Copy relative path to clipboard", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 5},
		{ID: "paste", Name: "Paste", Description: "Paste yanked file", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 5},
		{ID: "sort", Name: "Sort", Description: "Cycle sort mode", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 6},
		{ID: "refresh", Name: "Refresh", Description: "Refresh file tree", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 6},
		{ID: "rename", Name: "Rename", Description: "Rename file or directory", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 7},
		{ID: "move", Name: "Move", Description: "Move file or directory", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 7},
		{ID: "reveal", Name: "Reveal", Description: "Reveal in file manager", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 8},
		{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle tree pane visibility", Category: plugin.CategoryView, Context: "file-browser-tree", Priority: 9},
		{ID: "toggle-ignored", Name: "Ignored", Description: "Toggle git-ignored file visibility", Category: plugin.CategoryView, Context: "file-browser-tree", Priority: 9},
		// Preview pane commands
		{ID: "quick-open", Name: "Find", Description: "Find a file by name", Category: plugin.CategorySearch, Context: "file-browser-preview", Priority: 1},
		{ID: "project-search", Name: "Search", Description: "Search the project's contents", Category: plugin.CategorySearch, Context: "file-browser-preview", Priority: 2},
		{ID: "info", Name: "Info", Description: "Show file info", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 2},
		{ID: "edit", Name: "Edit", Description: "Edit file inline", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 2},
		{ID: "edit-external", Name: "Edit+", Description: "Edit in full terminal", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 2},
		{ID: "prev-tab", Name: "Tab←", Description: "Previous tab", Category: plugin.CategoryNavigation, Context: "file-browser-preview", Priority: 3},
		{ID: "next-tab", Name: "Tab→", Description: "Next tab", Category: plugin.CategoryNavigation, Context: "file-browser-preview", Priority: 3},
		{ID: "blame", Name: "Blame", Description: "Show git blame", Category: plugin.CategoryView, Context: "file-browser-preview", Priority: 3},
		{ID: "search-content", Name: "InFile", Description: "Search this file's contents", Category: plugin.CategorySearch, Context: "file-browser-preview", Priority: 3},
		{ID: "toggle-wrap", Name: "Wrap", Description: "Toggle line wrapping", Category: plugin.CategoryView, Context: "file-browser-preview", Priority: 3},
		{ID: "toggle-markdown", Name: "Render", Description: "Toggle markdown rendering", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 4},
		{ID: "close-tab", Name: "Close", Description: "Close active tab", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 4},
		{ID: "back", Name: "Back", Description: "Return to file tree", Category: plugin.CategoryNavigation, Context: "file-browser-preview", Priority: 5},
		{ID: "refresh", Name: "Refresh", Description: "Refresh file tree", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 5},
		{ID: "rename", Name: "Rename", Description: "Rename file", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 6},
		{ID: "reveal", Name: "Reveal", Description: "Reveal in file manager", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 7},
		{ID: "yank-contents", Name: "Yank", Description: "Copy file contents", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 7},
		{ID: "yank-path", Name: "Path", Description: "Copy file path", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 8},
		{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle tree pane visibility", Category: plugin.CategoryView, Context: "file-browser-preview", Priority: 9},
		{ID: "toggle-ignored", Name: "Ignored", Description: "Toggle git-ignored file visibility", Category: plugin.CategoryView, Context: "file-browser-preview", Priority: 9},
		// Tree search commands
		{ID: "confirm", Name: "Go", Description: "Jump to match", Category: plugin.CategoryNavigation, Context: "file-browser-search", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Cancel search", Category: plugin.CategoryActions, Context: "file-browser-search", Priority: 1},
		// Content search commands
		{ID: "confirm", Name: "Go", Description: "Jump to match", Category: plugin.CategoryNavigation, Context: "file-browser-content-search", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Cancel search", Category: plugin.CategoryActions, Context: "file-browser-content-search", Priority: 1},
		// Quick open commands
		{ID: "select", Name: "Open", Description: "Open selected file", Category: plugin.CategoryActions, Context: "file-browser-quick-open", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Cancel quick open", Category: plugin.CategoryActions, Context: "file-browser-quick-open", Priority: 1},
		// Project search commands
		{ID: "select", Name: "Open", Description: "Open selected result", Category: plugin.CategoryActions, Context: "file-browser-project-search", Priority: 1},
		{ID: "toggle", Name: "Focus", Description: "Toggle input/results focus (j/k/g/G in results)", Category: plugin.CategoryNavigation, Context: "file-browser-project-search", Priority: 2},
		{ID: "cancel", Name: "Close", Description: "Close search", Category: plugin.CategoryActions, Context: "file-browser-project-search", Priority: 3},
		// File operation commands (move/rename/create/delete)
		{ID: "confirm", Name: "Confirm", Description: "Confirm operation", Category: plugin.CategoryActions, Context: "file-browser-file-op", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Cancel operation", Category: plugin.CategoryActions, Context: "file-browser-file-op", Priority: 1},
		// Line jump commands
		{ID: "confirm", Name: "Go", Description: "Jump to line", Category: plugin.CategoryNavigation, Context: "file-browser-line-jump", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Cancel jump", Category: plugin.CategoryActions, Context: "file-browser-line-jump", Priority: 1},
		// Info modal commands
		{ID: "close", Name: "Close", Description: "Close info modal", Category: plugin.CategoryActions, Context: "file-browser-info", Priority: 1},
		// Blame view commands
		{ID: "close", Name: "Close", Description: "Close blame view", Category: plugin.CategoryActions, Context: "file-browser-blame", Priority: 1},
		{ID: "view-commit", Name: "Details", Description: "View commit details", Category: plugin.CategoryActions, Context: "file-browser-blame", Priority: 2},
		{ID: "yank-hash", Name: "Yank", Description: "Copy commit hash", Category: plugin.CategoryActions, Context: "file-browser-blame", Priority: 3},
	}
}

// FocusContext returns the current focus context.
func (p *Plugin) FocusContext() string {
	if p.edit.Active {
		return "file-browser-inline-edit"
	}
	if p.projectSearchMode {
		return "file-browser-project-search"
	}
	if p.quickOpenMode {
		return "file-browser-quick-open"
	}
	if p.infoMode {
		return "file-browser-info"
	}
	if p.blameMode {
		return "file-browser-blame"
	}
	if p.fileOpMode != FileOpNone {
		return "file-browser-file-op"
	}
	if p.lineJumpMode {
		return "file-browser-line-jump"
	}
	if p.contentSearchMode {
		return "file-browser-content-search"
	}
	if p.searchMode {
		return "file-browser-search"
	}
	if p.activePane == PanePreview {
		return "file-browser-preview"
	}
	return "file-browser-tree"
}

// ConsumesTextInput reports whether the file browser currently expects typed
// text input and should suppress app-level shortcut interception.
func (p *Plugin) ConsumesTextInput() bool {
	return p.searchMode ||
		p.contentSearchMode ||
		p.quickOpenMode ||
		p.projectSearchMode ||
		p.fileOpMode != FileOpNone ||
		p.lineJumpMode ||
		p.edit.Active
}

// BlocksGlobalKeys reports whether a plugin-owned modal has keyboard focus.
func (p *Plugin) BlocksGlobalKeys() bool {
	return p.infoMode || p.blameMode || p.fileOpMode != FileOpNone
}
