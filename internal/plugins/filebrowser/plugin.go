package filebrowser

import (
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/image"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/tty"
)

const (
	pluginID   = "file-browser"
	pluginName = "files"
	pluginIcon = "F"

	// Quick open limits
	quickOpenMaxFiles   = 50000           // Max files to cache (prevents OOM on huge repos)
	quickOpenMaxResults = 50              // Max matches to show
	quickOpenTimeout    = 2 * time.Second // Max time to spend scanning

	// Directory cache limits (for path auto-complete)
	dirCacheMaxDirs    = 10000 // Max directories to cache
	dirCacheMaxResults = 5     // Max suggestions to show
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
	RefreshMsg   struct{}
	TreeBuiltMsg struct {
		Err error
	}
	StateRestoredMsg struct {
		State state.FileBrowserState
	}
	WatchStartedMsg struct{ Watcher *Watcher }
	WatchEventMsg   struct{}
	// NavigateToFileMsg requests navigation to a specific file (from other plugins).
	NavigateToFileMsg struct {
		Path string // Relative path from workdir
	}
	// RevealErrorMsg is sent when reveal in file manager fails.
	RevealErrorMsg struct {
		Err error
	}
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
	// GitInfoMsg contains git status for a file.
	GitInfoMsg struct {
		Status     string
		LastCommit string
	}
)

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

	// Pane state
	activePane  FocusPane
	treeVisible bool // Toggle tree pane visibility with \
	showIgnored bool // Toggle git-ignored file visibility with I

	// Tree state
	treeCursor    int
	treeScrollOff int

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

	// Search state (tree filename search)
	searchMode    bool
	searchQuery   string
	searchMatches []*FileNode
	searchCursor  int

	// Content search state (preview pane)
	contentSearchMode      bool
	contentSearchCommitted bool // True after Enter confirms query (enables n/N navigation)
	contentSearchQuery     string
	contentSearchMatches   []ContentMatch
	contentSearchCursor    int // Index into contentSearchMatches

	// Text selection state (preview pane)
	textSelectionActive bool // True when user is actively dragging
	textSelectionStart  int  // First line selected (0-indexed into previewLines)
	textSelectionEnd    int  // Last line selected (0-indexed, inclusive)
	textSelectionAnchor int  // Line where selection started (for drag direction)

	// Quick open state
	quickOpenMode    bool
	quickOpenQuery   string
	quickOpenMatches []QuickOpenMatch
	quickOpenCursor  int
	quickOpenFiles   []string // Cached file paths (relative)
	quickOpenError   string   // Error message if scan failed/limited

	// Project-wide search state (ctrl+s)
	projectSearchMode       bool
	projectSearchState      *ProjectSearchState
	projectSearchModal      *modal.Modal
	projectSearchModalWidth int

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

	// Line jump state (vim-style :<number>)
	lineJumpMode   bool
	lineJumpBuffer string

	// Path auto-complete state (for move modal)
	dirCache              []string // Cached directory paths
	fileOpSuggestions     []string // Current filtered suggestions
	fileOpSuggestionIdx   int      // Selected suggestion (-1 = none)
	fileOpShowSuggestions bool     // Show suggestions dropdown

	// Clipboard state (yank/paste)
	clipboardPath  string // Relative path of yanked file/directory
	clipboardIsDir bool   // Whether yanked item is a directory

	// File watcher
	watcher     *Watcher
	lastRefresh time.Time // Debounce rapid refreshes on focus

	// Mouse support
	mouseHandler *mouse.Handler

	// State restoration flag
	stateRestored bool

	// Inline editor state (tmux-based editing)
	inlineEditor        *tty.Model  // Embeddable tty model for inline editing
	inlineEditMode      bool        // True when inline editing is active
	inlineEditSession   string      // Tmux session name for editor
	inlineEditFile      string      // Path of file being edited
	inlineEditOrigMtime time.Time   // Original file mtime (to detect changes)
	inlineEditEditor    string      // Editor command used (vim, nano, emacs, etc.)

	// Exit confirmation state (when clicking away from editor)
	showExitConfirmation bool        // True when confirmation dialog is shown
	pendingClickRegion   string      // Region that was clicked (regionTreePane, etc)
	pendingClickData     interface{} // Data associated with the click
	exitConfirmSelection int         // 0=Save&Exit, 1=Exit without saving, 2=Cancel
}

// New creates a new File Browser plugin.
func New() *Plugin {
	return &Plugin{
		mouseHandler:  mouse.NewHandler(),
		imageRenderer: image.New(), // Detect terminal graphics protocol once
		treeVisible:   true,        // Tree pane visible by default
		showIgnored:   true,        // Show git-ignored files by default
		inlineEditor:  tty.New(nil), // Initialize inline editor with default config
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
	p.tree = NewFileTree(ctx.WorkDir)

	// Reset state flags for reinit support (project switching)
	p.stateRestored = false

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
	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	return tea.Batch(
		p.refresh(),
		p.startWatcher(),
	)
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	if p.watcher != nil {
		p.watcher.Stop()
	}
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
		if tab.Path == "" {
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
	return func() tea.Msg {
		fbState := state.GetFileBrowserState(workDir)
		return StateRestoredMsg{State: fbState}
	}
}

// startWatcher initializes the file system watcher.
func (p *Plugin) startWatcher() tea.Cmd {
	return func() tea.Msg {
		watcher, err := NewWatcher()
		if err != nil {
			p.ctx.Logger.Error("file browser: watcher failed", "error", err)
			return nil
		}
		return WatchStartedMsg{Watcher: watcher}
	}
}

// listenForWatchEvents waits for the next file system event.
func (p *Plugin) listenForWatchEvents() tea.Cmd {
	if p.watcher == nil {
		return nil
	}
	return func() tea.Msg {
		<-p.watcher.Events()
		return WatchEventMsg{}
	}
}

// updateWatchedFile updates the file watcher to watch the current preview file.
func (p *Plugin) updateWatchedFile() {
	if p.watcher == nil {
		return
	}
	if p.previewFile != "" {
		_ = p.watcher.WatchFile(filepath.Join(p.ctx.WorkDir, p.previewFile))
	} else {
		_ = p.watcher.WatchFile("")
	}
}

// refresh rebuilds the file tree, preserving expanded state.
func (p *Plugin) refresh() tea.Cmd {
	return func() tea.Msg {
		expandedPaths := p.tree.GetExpandedPaths()
		err := p.tree.Build()
		p.tree.RestoreExpandedPaths(expandedPaths)
		return TreeBuiltMsg{Err: err}
	}
}

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	// Handle exit confirmation dialog first
	if p.showExitConfirmation {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "j", "down":
				p.exitConfirmSelection = (p.exitConfirmSelection + 1) % 3
				return p, nil
			case "k", "up":
				p.exitConfirmSelection = (p.exitConfirmSelection + 2) % 3
				return p, nil
			case "enter":
				return p.handleExitConfirmationChoice()
			case "esc", "q":
				// Cancel - return to editing
				p.showExitConfirmation = false
				p.pendingClickRegion = ""
				p.pendingClickData = nil
				return p, nil
			}
		}
		return p, nil
	}

	// Handle inline edit mode - delegate most messages to tty model
	if p.inlineEditMode && p.inlineEditor != nil {
		// Check if editor became inactive (vim exited normally)
		// Also check if tmux session died (handles :wq case before SessionDeadMsg arrives)
		if !p.inlineEditor.IsActive() || !p.isInlineEditSessionAlive() {
			editedFile := p.inlineEditFile // Save before exitInlineEditMode clears it
			p.exitInlineEditMode()
			// Refresh preview to show updated file
			if editedFile != "" {
				return p, LoadPreview(p.ctx.WorkDir, editedFile)
			}
			return p, p.refresh()
		}

		switch msg := msg.(type) {
		case tea.WindowSizeMsg:
			p.width = msg.Width
			p.height = msg.Height
			// Update inline editor dimensions
			return p, p.inlineEditor.SetDimensions(p.calculateInlineEditorWidth(), p.calculateInlineEditorHeight())

		case tea.MouseMsg:
			// Route mouse through handleMouse for click-away detection
			return p.handleMouse(msg)

		case tea.KeyMsg, tty.EscapeTimerMsg, tty.CaptureResultMsg,
			tty.PollTickMsg, tty.PaneResizedMsg, tty.SessionDeadMsg, tty.PasteResultMsg:
			cmd := p.inlineEditor.Update(msg)
			// Check if editor exited
			if !p.inlineEditor.IsActive() {
				p.exitInlineEditMode()
				return p, tea.Batch(cmd, p.refresh())
			}
			return p, cmd
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

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		// Invalidate markdown cache when size changes (width affects rendering)
		if p.markdownRenderMode && p.isMarkdownFile() {
			p.markdownRendered = nil
		}
		// Invalidate image cache when size changes (will re-render at new size)
		p.imageResult = nil

	case TreeBuiltMsg:
		if msg.Err != nil {
			p.ctx.Logger.Error("tree build failed", "error", msg.Err)
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
			return p, LoadPreview(p.ctx.WorkDir, p.previewFile)
		}

		p.previewFile = ""
		p.previewScroll = 0

	case PreviewLoadedMsg:
		if msg.Path == p.previewFile {
			p.applyPreviewResult(msg.Result)
			p.updateActiveTabResult(msg.Result)
			p.clampPreviewScroll()

			// Re-run search if still in search mode (e.g., navigating files with j/k)
			if p.contentSearchMode && p.contentSearchQuery != "" {
				targetScroll := p.previewScroll
				p.updateContentMatches()
				// Jump to match nearest the target line from project search
				if targetScroll > 0 && len(p.contentSearchMatches) > 0 {
					p.scrollToNearestMatch(targetScroll)
				}
			}
		}

	case RefreshMsg:
		return p, p.refresh()

	case WatchStartedMsg:
		p.watcher = msg.Watcher
		return p, p.listenForWatchEvents()

	case WatchEventMsg:
		// Watched file changed - reload preview (watcher only watches the previewed file)
		cmds := []tea.Cmd{p.listenForWatchEvents()}
		if p.previewFile != "" {
			cmds = append(cmds, LoadPreview(p.ctx.WorkDir, p.previewFile))
		}
		return p, tea.Batch(cmds...)

	case NavigateToFileMsg:
		return p.navigateToFile(msg.Path)

	case RevealErrorMsg:
		p.ctx.Logger.Error("file browser: reveal failed", "error", msg.Err)

	case FileOpErrorMsg:
		p.fileOpError = msg.Err.Error()

	case FileOpSuccessMsg:
		// Clear file operation state and refresh
		p.fileOpMode = FileOpNone
		p.fileOpTarget = nil
		p.fileOpError = ""
		return p, p.refresh()

	case CreateSuccessMsg:
		// Clear file operation state and refresh
		p.fileOpMode = FileOpNone
		p.fileOpTarget = nil
		p.fileOpError = ""
		return p, p.refresh()

	case DeleteSuccessMsg:
		// Clear file operation state and refresh
		p.fileOpMode = FileOpNone
		p.fileOpTarget = nil
		p.fileOpError = ""
		p.fileOpConfirmDelete = false
		return p, p.refresh()

	case PasteSuccessMsg:
		// Refresh after paste
		return p, p.refresh()

	case GitInfoMsg:
		p.gitStatus = msg.Status
		p.gitLastCommit = msg.LastCommit
		return p, nil

	case BlameLoadedMsg:
		if p.blameState != nil {
			p.blameState.IsLoading = false
			if msg.Error != nil {
				p.blameState.Error = msg.Error
			} else {
				p.blameState.Lines = msg.Lines
			}
		}
		return p, nil

	case ProjectSearchResultsMsg:
		if p.projectSearchState != nil {
			p.projectSearchState.IsSearching = false
			if msg.Error != nil {
				p.projectSearchState.Error = msg.Error.Error()
				p.projectSearchState.Results = nil
			} else {
				p.projectSearchState.Error = ""
				p.projectSearchState.Results = msg.Results
				p.projectSearchState.Cursor = 0
				p.projectSearchState.ScrollOffset = 0
			}
		}

	case InlineEditStartedMsg:
		return p, p.handleInlineEditStarted(msg)

	case InlineEditExitedMsg:
		// Check if there was a pending click action (from Save & Exit)
		if p.pendingClickRegion != "" {
			return p.processPendingClickAction()
		}
		// Normal exit - refresh preview after editing
		if msg.FilePath != "" {
			return p, LoadPreview(p.ctx.WorkDir, msg.FilePath)
		}

	case tea.KeyMsg:
		return p.handleKey(msg)
	}

	return p, nil
}

// View renders the plugin.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height
	content := p.renderView()
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
	return []plugin.Command{
		// Tree pane commands
		{ID: "quick-open", Name: "Open", Description: "Quick open file by name", Category: plugin.CategorySearch, Context: "file-browser-tree", Priority: 1},
		{ID: "new-tab", Name: "Tab+", Description: "Open file in new tab", Category: plugin.CategoryNavigation, Context: "file-browser-tree", Priority: 2},
		{ID: "project-search", Name: "Find", Description: "Search in project", Category: plugin.CategorySearch, Context: "file-browser-tree", Priority: 2},
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
		{ID: "toggle-sidebar", Name: "Panel", Description: "Toggle tree pane visibility", Category: plugin.CategoryView, Context: "file-browser-tree", Priority: 9},
		{ID: "toggle-ignored", Name: "Ignored", Description: "Toggle git-ignored file visibility", Category: plugin.CategoryView, Context: "file-browser-tree", Priority: 9},
		// Preview pane commands
		{ID: "quick-open", Name: "Open", Description: "Quick open file by name", Category: plugin.CategorySearch, Context: "file-browser-preview", Priority: 1},
		{ID: "project-search", Name: "Find", Description: "Search in project", Category: plugin.CategorySearch, Context: "file-browser-preview", Priority: 2},
		{ID: "info", Name: "Info", Description: "Show file info", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 2},
		{ID: "edit", Name: "Edit", Description: "Edit file inline", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 2},
		{ID: "edit-external", Name: "Edit+", Description: "Edit in full terminal", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 2},
		{ID: "prev-tab", Name: "Tab←", Description: "Previous tab", Category: plugin.CategoryNavigation, Context: "file-browser-preview", Priority: 3},
		{ID: "next-tab", Name: "Tab→", Description: "Next tab", Category: plugin.CategoryNavigation, Context: "file-browser-preview", Priority: 3},
		{ID: "blame", Name: "Blame", Description: "Show git blame", Category: plugin.CategoryView, Context: "file-browser-preview", Priority: 3},
		{ID: "search-content", Name: "Search", Description: "Search file content", Category: plugin.CategorySearch, Context: "file-browser-preview", Priority: 3},
		{ID: "toggle-markdown", Name: "Render", Description: "Toggle markdown rendering", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 4},
		{ID: "close-tab", Name: "Close", Description: "Close active tab", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 4},
		{ID: "back", Name: "Back", Description: "Return to file tree", Category: plugin.CategoryNavigation, Context: "file-browser-preview", Priority: 5},
		{ID: "refresh", Name: "Refresh", Description: "Refresh file tree", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 5},
		{ID: "rename", Name: "Rename", Description: "Rename file", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 6},
		{ID: "reveal", Name: "Reveal", Description: "Reveal in file manager", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 7},
		{ID: "yank-contents", Name: "Yank", Description: "Copy file contents", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 7},
		{ID: "yank-path", Name: "Path", Description: "Copy file path", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 8},
		{ID: "toggle-sidebar", Name: "Panel", Description: "Toggle tree pane visibility", Category: plugin.CategoryView, Context: "file-browser-preview", Priority: 9},
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
		{ID: "toggle", Name: "Toggle", Description: "Expand/collapse file", Category: plugin.CategoryActions, Context: "file-browser-project-search", Priority: 2},
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
	if p.inlineEditMode {
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
