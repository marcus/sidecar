package filebrowser

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/plugin"
)

const (
	pluginID   = "file-browser"
	pluginName = "files"
	pluginIcon = "F"

	// Quick open limits
	quickOpenMaxFiles   = 50000           // Max files to cache (prevents OOM on huge repos)
	quickOpenMaxResults = 50              // Max matches to show
	quickOpenTimeout    = 2 * time.Second // Max time to spend scanning
)

// FileOpMode represents the current file operation mode.
type FileOpMode int

const (
	FileOpNone FileOpMode = iota
	FileOpMove
	FileOpRename
)

// Message types
type (
	RefreshMsg      struct{}
	TreeBuiltMsg    struct{ Err error }
	WatchStartedMsg struct{ Watcher *Watcher }
	WatchEventMsg   struct{}
	OpenFileMsg     struct {
		Editor string
		Path   string
	}
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
	activePane FocusPane

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

	// Quick open state
	quickOpenMode    bool
	quickOpenQuery   string
	quickOpenMatches []QuickOpenMatch
	quickOpenCursor  int
	quickOpenFiles   []string // Cached file paths (relative)
	quickOpenError   string   // Error message if scan failed/limited

	// Project-wide search state (ctrl+s)
	projectSearchMode  bool
	projectSearchState *ProjectSearchState

	// File operation state (move/rename)
	fileOpMode          FileOpMode
	fileOpTarget        *FileNode       // The file being operated on
	fileOpTextInput     textinput.Model // Text input for rename/move
	fileOpError         string          // Error message if operation failed
	fileOpConfirmCreate bool            // True when waiting for directory creation confirmation
	fileOpConfirmPath   string          // The directory path to create

	// File watcher
	watcher *Watcher
}

// New creates a new File Browser plugin.
func New() *Plugin {
	return &Plugin{}
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
}

// startWatcher initializes the file system watcher.
func (p *Plugin) startWatcher() tea.Cmd {
	return func() tea.Msg {
		watcher, err := NewWatcher(p.ctx.WorkDir)
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

// refresh rebuilds the file tree, preserving expanded state.
func (p *Plugin) refresh() tea.Cmd {
	return func() tea.Msg {
		err := p.tree.Refresh()
		return TreeBuiltMsg{Err: err}
	}
}

// openFile returns a command to open a file in the user's editor.
func (p *Plugin) openFile(path string) tea.Cmd {
	return func() tea.Msg {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			editor = "vim"
		}
		fullPath := filepath.Join(p.ctx.WorkDir, path)
		return OpenFileMsg{Editor: editor, Path: fullPath}
	}
}

// revealInFileManager reveals the file/directory in the system file manager.
func (p *Plugin) revealInFileManager(path string) tea.Cmd {
	return func() tea.Msg {
		fullPath := filepath.Join(p.ctx.WorkDir, path)
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			// macOS: open -R reveals in Finder with file selected
			cmd = exec.Command("open", "-R", fullPath)
		case "windows":
			// Windows: explorer /select, reveals in Explorer with file selected
			cmd = exec.Command("explorer", "/select,", fullPath)
		case "linux":
			// Linux: xdg-open opens the parent directory
			cmd = exec.Command("xdg-open", filepath.Dir(fullPath))
		default:
			return RevealErrorMsg{Err: fmt.Errorf("reveal not supported on %s", runtime.GOOS)}
		}
		if err := cmd.Start(); err != nil {
			return RevealErrorMsg{Err: err}
		}
		return nil
	}
}

// validateDestPath checks that destination path is within workdir.
// Returns error if path escapes the project directory.
func (p *Plugin) validateDestPath(dstPath string) error {
	// Clean and resolve the destination path
	cleanDst := filepath.Clean(dstPath)

	// Get absolute paths for comparison
	absDst, err := filepath.Abs(cleanDst)
	if err != nil {
		return fmt.Errorf("invalid destination path")
	}

	absWorkDir, err := filepath.Abs(p.ctx.WorkDir)
	if err != nil {
		return fmt.Errorf("failed to resolve work directory")
	}

	// Check if destination is within workdir
	relPath, err := filepath.Rel(absWorkDir, absDst)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("cannot move files outside project directory")
	}

	return nil
}

// validateFilename checks for invalid filename characters and patterns.
func validateFilename(name string) error {
	if name == "" {
		return fmt.Errorf("filename cannot be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid filename")
	}
	// Check for null bytes and control characters
	for _, r := range name {
		if r == 0 || (r < 32 && r != '\t') {
			return fmt.Errorf("filename contains invalid characters")
		}
	}
	// Check for characters invalid on common filesystems
	invalidChars := []rune{'<', '>', ':', '"', '|', '?', '*'}
	for _, c := range invalidChars {
		if strings.ContainsRune(name, c) {
			return fmt.Errorf("filename contains invalid character: %c", c)
		}
	}
	return nil
}

// executeFileOp performs the pending file operation.
func (p *Plugin) executeFileOp() (plugin.Plugin, tea.Cmd) {
	input := p.fileOpTextInput.Value()
	if p.fileOpTarget == nil || input == "" {
		p.fileOpMode = FileOpNone
		return p, nil
	}

	// Validate filename (for rename: the input, for move: basename of path)
	var nameToValidate string
	if p.fileOpMode == FileOpRename {
		nameToValidate = input
	} else {
		nameToValidate = filepath.Base(input)
	}
	if err := validateFilename(nameToValidate); err != nil {
		p.fileOpError = err.Error()
		return p, nil
	}

	srcPath := filepath.Join(p.ctx.WorkDir, p.fileOpTarget.Path)
	var dstPath string

	switch p.fileOpMode {
	case FileOpRename:
		// Rename: new name in same directory
		// Disallow path separators in rename (would be a move)
		if strings.Contains(input, string(filepath.Separator)) || strings.Contains(input, "/") {
			p.fileOpError = "use 'm' to move to a different directory"
			return p, nil
		}
		dstPath = filepath.Join(filepath.Dir(srcPath), input)
	case FileOpMove:
		// Move: relative path from workdir only (no absolute paths)
		if filepath.IsAbs(input) {
			p.fileOpError = "absolute paths not allowed"
			return p, nil
		}
		dstPath = filepath.Join(p.ctx.WorkDir, input)
	}

	// Validate destination is within project directory
	if err := p.validateDestPath(dstPath); err != nil {
		p.fileOpError = err.Error()
		return p, nil
	}

	// For moves, check if parent directory exists
	if p.fileOpMode == FileOpMove {
		parentDir := filepath.Dir(dstPath)
		if _, err := os.Stat(parentDir); os.IsNotExist(err) {
			// Enter confirmation mode to ask user if they want to create the directory
			p.fileOpConfirmCreate = true
			p.fileOpConfirmPath = parentDir
			return p, nil
		}
	}

	return p, p.doFileOp(srcPath, dstPath)
}

// doFileOp performs the actual file move/rename operation.
func (p *Plugin) doFileOp(src, dst string) tea.Cmd {
	return func() tea.Msg {
		// Create parent directories if needed (for move)
		dstDir := filepath.Dir(dst)
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return FileOpErrorMsg{Err: err}
		}

		// Check if destination exists
		if _, err := os.Stat(dst); err == nil {
			return FileOpErrorMsg{Err: fmt.Errorf("destination already exists: %s", filepath.Base(dst))}
		}

		// Check if source and destination are the same
		if src == dst {
			return FileOpErrorMsg{Err: fmt.Errorf("source and destination are the same")}
		}

		// Perform the move/rename
		if err := os.Rename(src, dst); err != nil {
			return FileOpErrorMsg{Err: err}
		}

		return FileOpSuccessMsg{Src: src, Dst: dst}
	}
}

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height

	case TreeBuiltMsg:
		if msg.Err != nil {
			p.ctx.Logger.Error("file browser: tree build failed", "error", msg.Err)
		}

	case PreviewLoadedMsg:
		if msg.Path == p.previewFile {
			p.previewLines = msg.Result.Lines
			p.previewHighlighted = msg.Result.HighlightedLines
			p.isBinary = msg.Result.IsBinary
			p.isTruncated = msg.Result.IsTruncated
			p.previewError = msg.Result.Error
			p.previewScroll = 0
			// Re-run search if still in search mode (e.g., navigating files with j/k)
			if p.contentSearchMode && p.contentSearchQuery != "" {
				p.updateContentMatches()
			}
		}

	case RefreshMsg:
		return p, p.refresh()

	case WatchStartedMsg:
		p.watcher = msg.Watcher
		return p, p.listenForWatchEvents()

	case WatchEventMsg:
		// File system changed, refresh tree and continue listening
		return p, tea.Batch(
			p.refresh(),
			p.listenForWatchEvents(),
		)

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

	case tea.KeyMsg:
		return p.handleKey(msg)
	}

	return p, nil
}

func (p *Plugin) handleKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	// Quick open can be triggered from any context (except when already open)
	if key == "ctrl+p" && !p.quickOpenMode && !p.projectSearchMode {
		return p.openQuickOpen()
	}

	// Project search can be triggered from any context (except when already open)
	if key == "ctrl+s" && !p.projectSearchMode && !p.quickOpenMode {
		return p.openProjectSearch()
	}

	// Handle project search mode
	if p.projectSearchMode {
		return p.handleProjectSearchKey(msg)
	}

	// Handle quick open mode
	if p.quickOpenMode {
		return p.handleQuickOpenKey(msg)
	}

	// Handle file operation mode (move/rename input)
	if p.fileOpMode != FileOpNone {
		return p.handleFileOpKey(msg)
	}

	// Handle content search mode input (preview pane search)
	if p.contentSearchMode {
		return p.handleContentSearchKey(msg)
	}

	// Handle tree search mode input
	if p.searchMode {
		return p.handleSearchKey(msg)
	}

	// Handle keys based on active pane
	if p.activePane == PanePreview {
		return p.handlePreviewKey(key)
	}
	return p.handleTreeKey(key)
}

func (p *Plugin) handleTreeKey(key string) (plugin.Plugin, tea.Cmd) {
	switch key {
	case "j", "down":
		if p.treeCursor < p.tree.Len()-1 {
			p.treeCursor++
			p.ensureTreeCursorVisible()
		}

	case "k", "up":
		if p.treeCursor > 0 {
			p.treeCursor--
			p.ensureTreeCursorVisible()
		}

	case "l", "right":
		node := p.tree.GetNode(p.treeCursor)
		if node != nil {
			if node.IsDir {
				_ = p.tree.Expand(node)
			} else {
				// Load file preview and switch to preview pane
				p.previewFile = node.Path
				p.previewScroll = 0
				p.previewLines = nil
				p.previewError = nil
				p.isBinary = false
				p.isTruncated = false
				p.activePane = PanePreview // Switch to preview pane
				return p, LoadPreview(p.ctx.WorkDir, node.Path)
			}
		}

	case "enter":
		node := p.tree.GetNode(p.treeCursor)
		if node != nil {
			if node.IsDir {
				// Toggle expand/collapse
				_ = p.tree.Toggle(node)
			} else {
				// Load file preview and switch to preview pane
				p.previewFile = node.Path
				p.previewScroll = 0
				p.previewLines = nil
				p.previewError = nil
				p.isBinary = false
				p.isTruncated = false
				p.activePane = PanePreview
				return p, LoadPreview(p.ctx.WorkDir, node.Path)
			}
		}

	case "h", "left":
		node := p.tree.GetNode(p.treeCursor)
		if node != nil {
			if node.IsDir && node.IsExpanded {
				p.tree.Collapse(node)
			} else if node.Parent != nil && node.Parent != p.tree.Root {
				if idx := p.tree.IndexOf(node.Parent); idx >= 0 {
					p.treeCursor = idx
					p.ensureTreeCursorVisible()
				}
			}
		}

	case "g":
		p.treeCursor = 0
		p.treeScrollOff = 0

	case "G":
		if p.tree.Len() > 0 {
			p.treeCursor = p.tree.Len() - 1
			p.ensureTreeCursorVisible()
		}

	case "ctrl+d":
		visibleHeight := p.visibleContentHeight()
		p.treeCursor += visibleHeight / 2
		if p.treeCursor >= p.tree.Len() {
			p.treeCursor = p.tree.Len() - 1
		}
		p.ensureTreeCursorVisible()

	case "ctrl+u":
		visibleHeight := p.visibleContentHeight()
		p.treeCursor -= visibleHeight / 2
		if p.treeCursor < 0 {
			p.treeCursor = 0
		}
		p.ensureTreeCursorVisible()

	case "ctrl+f", "pgdown":
		visibleHeight := p.visibleContentHeight()
		p.treeCursor += visibleHeight
		if p.treeCursor >= p.tree.Len() {
			p.treeCursor = p.tree.Len() - 1
		}
		p.ensureTreeCursorVisible()

	case "ctrl+b", "pgup":
		visibleHeight := p.visibleContentHeight()
		p.treeCursor -= visibleHeight
		if p.treeCursor < 0 {
			p.treeCursor = 0
		}
		p.ensureTreeCursorVisible()

	case "e", "o":
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && !node.IsDir {
			return p, p.openFile(node.Path)
		}

	case "R":
		// Reveal in file manager (Finder/Explorer/etc.)
		node := p.tree.GetNode(p.treeCursor)
		if node != nil {
			return p, p.revealInFileManager(node.Path)
		}

	case "r":
		// Rename file/directory
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && node != p.tree.Root {
			p.fileOpMode = FileOpRename
			p.fileOpTarget = node
			p.fileOpTextInput = textinput.New()
			p.fileOpTextInput.SetValue(node.Name)
			p.fileOpTextInput.Focus()
			p.fileOpTextInput.CursorEnd()
			p.fileOpError = ""
		}

	case "m":
		// Move file/directory
		node := p.tree.GetNode(p.treeCursor)
		if node != nil && node != p.tree.Root {
			p.fileOpMode = FileOpMove
			p.fileOpTarget = node
			p.fileOpTextInput = textinput.New()
			p.fileOpTextInput.SetValue(node.Path)
			p.fileOpTextInput.Focus()
			p.fileOpTextInput.CursorEnd()
			p.fileOpError = ""
		}

	case "/":
		p.searchMode = true
		p.searchQuery = ""
		p.searchMatches = nil
		p.searchCursor = 0

	case "n":
		// Next match
		if len(p.searchMatches) > 0 {
			p.searchCursor = (p.searchCursor + 1) % len(p.searchMatches)
			p.jumpToSearchMatch()
		}

	case "N":
		// Previous match
		if len(p.searchMatches) > 0 {
			p.searchCursor--
			if p.searchCursor < 0 {
				p.searchCursor = len(p.searchMatches) - 1
			}
			p.jumpToSearchMatch()
		}
	}

	return p, nil
}

func (p *Plugin) handlePreviewKey(key string) (plugin.Plugin, tea.Cmd) {
	lines := p.previewHighlighted
	if len(lines) == 0 {
		lines = p.previewLines
	}
	visibleHeight := p.visibleContentHeight()
	maxScroll := len(lines) - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch key {
	case "j", "down":
		if p.previewScroll < maxScroll {
			p.previewScroll++
		}

	case "k", "up":
		if p.previewScroll > 0 {
			p.previewScroll--
		}

	case "g":
		p.previewScroll = 0

	case "G":
		p.previewScroll = maxScroll

	case "ctrl+d":
		p.previewScroll += visibleHeight / 2
		if p.previewScroll > maxScroll {
			p.previewScroll = maxScroll
		}

	case "ctrl+u":
		p.previewScroll -= visibleHeight / 2
		if p.previewScroll < 0 {
			p.previewScroll = 0
		}

	case "ctrl+f", "pgdown":
		p.previewScroll += visibleHeight
		if p.previewScroll > maxScroll {
			p.previewScroll = maxScroll
		}

	case "ctrl+b", "pgup":
		p.previewScroll -= visibleHeight
		if p.previewScroll < 0 {
			p.previewScroll = 0
		}

	case "h", "left", "esc":
		// Return to tree pane
		p.activePane = PaneTree

	case "e":
		// Open previewed file in editor
		if p.previewFile != "" {
			return p, p.openFile(p.previewFile)
		}

	case "/":
		// Enter content search mode if we have content to search
		if len(p.previewLines) > 0 && !p.isBinary {
			p.contentSearchMode = true
			p.contentSearchCommitted = false
			p.contentSearchQuery = ""
			p.contentSearchMatches = nil
			p.contentSearchCursor = 0
		}

	case "R":
		// Reveal in file manager (Finder/Explorer/etc.)
		if p.previewFile != "" {
			return p, p.revealInFileManager(p.previewFile)
		}
	}

	return p, nil
}

// handleFileOpKey handles key input during file operation mode (move/rename).
func (p *Plugin) handleFileOpKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	// Handle confirmation mode for directory creation
	if p.fileOpConfirmCreate {
		switch key {
		case "y", "Y":
			// Create directory and proceed with move
			if err := os.MkdirAll(p.fileOpConfirmPath, 0755); err != nil {
				p.fileOpError = fmt.Sprintf("failed to create directory: %v", err)
				p.fileOpConfirmCreate = false
				p.fileOpConfirmPath = ""
				return p, nil
			}
			p.fileOpConfirmCreate = false
			p.fileOpConfirmPath = ""
			return p.executeFileOp() // Retry the operation
		case "esc":
			// Cancel entire operation
			p.fileOpMode = FileOpNone
			p.fileOpTarget = nil
			p.fileOpError = ""
			p.fileOpConfirmCreate = false
			p.fileOpConfirmPath = ""
			return p, nil
		default:
			// Any other key returns to edit mode
			p.fileOpConfirmCreate = false
			p.fileOpConfirmPath = ""
			return p, nil
		}
	}

	switch key {
	case "esc":
		// Cancel file operation
		p.fileOpMode = FileOpNone
		p.fileOpTarget = nil
		p.fileOpError = ""
		return p, nil

	case "enter":
		// Execute file operation
		return p.executeFileOp()

	default:
		// Delegate all other keys to textinput
		var cmd tea.Cmd
		p.fileOpTextInput, cmd = p.fileOpTextInput.Update(msg)
		p.fileOpError = "" // Clear error on input change
		return p, cmd
	}
}

// handleContentSearchKey handles key input during content search mode.
// Implements vim-style two-phase search: type query, Enter to commit, then n/N navigate.
func (p *Plugin) handleContentSearchKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	// Esc always exits search mode completely
	if key == "esc" {
		p.contentSearchMode = false
		p.contentSearchCommitted = false
		p.contentSearchQuery = ""
		p.contentSearchMatches = nil
		p.contentSearchCursor = 0
		return p, nil
	}

	// Phase 1: Typing query (not yet committed)
	if !p.contentSearchCommitted {
		switch key {
		case "enter":
			// Commit the search - now n/N will navigate matches
			if len(p.contentSearchQuery) > 0 {
				p.contentSearchCommitted = true
			}
		case "backspace":
			if len(p.contentSearchQuery) > 0 {
				p.contentSearchQuery = p.contentSearchQuery[:len(p.contentSearchQuery)-1]
				p.updateContentMatches()
			}
		default:
			// All printable characters go to query (including n, N, etc.)
			if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
				p.contentSearchQuery += key
				p.updateContentMatches()
			}
		}
		return p, nil
	}

	// Phase 2: Search committed - n/N navigate matches, j/k exit and navigate tree
	switch key {
	case "n":
		if len(p.contentSearchMatches) > 0 {
			p.contentSearchCursor = (p.contentSearchCursor + 1) % len(p.contentSearchMatches)
			p.scrollToContentMatch()
		}
	case "N":
		if len(p.contentSearchMatches) > 0 {
			p.contentSearchCursor--
			if p.contentSearchCursor < 0 {
				p.contentSearchCursor = len(p.contentSearchMatches) - 1
			}
			p.scrollToContentMatch()
		}
	case "j", "down":
		// Move to next file, keep search active
		if p.treeCursor < p.tree.Len()-1 {
			p.treeCursor++
			p.ensureTreeCursorVisible()
			p.contentSearchMatches = nil
			p.contentSearchCursor = 0
			return p, p.loadPreviewForCursor()
		}
	case "k", "up":
		// Move to previous file, keep search active
		if p.treeCursor > 0 {
			p.treeCursor--
			p.ensureTreeCursorVisible()
			p.contentSearchMatches = nil
			p.contentSearchCursor = 0
			return p, p.loadPreviewForCursor()
		}
	case "enter":
		// Exit search, keep position at current match
		p.contentSearchMode = false
		p.contentSearchCommitted = false
	}

	return p, nil
}

// updateContentMatches finds all matches of the search query in preview content.
func (p *Plugin) updateContentMatches() {
	p.contentSearchMatches = nil
	p.contentSearchCursor = 0

	if p.contentSearchQuery == "" {
		return
	}

	query := strings.ToLower(p.contentSearchQuery)

	for lineNo, line := range p.previewLines {
		lineLower := strings.ToLower(line)
		startIdx := 0
		for {
			idx := strings.Index(lineLower[startIdx:], query)
			if idx == -1 {
				break
			}
			absIdx := startIdx + idx
			p.contentSearchMatches = append(p.contentSearchMatches, ContentMatch{
				LineNo:   lineNo,
				StartCol: absIdx,
				EndCol:   absIdx + len(p.contentSearchQuery),
			})
			startIdx = absIdx + 1
		}
	}

	// Scroll to first match if any
	if len(p.contentSearchMatches) > 0 {
		p.scrollToContentMatch()
	}
}

// scrollToContentMatch scrolls the preview to show the current match.
func (p *Plugin) scrollToContentMatch() {
	if len(p.contentSearchMatches) == 0 || p.contentSearchCursor >= len(p.contentSearchMatches) {
		return
	}

	match := p.contentSearchMatches[p.contentSearchCursor]
	visibleHeight := p.visibleContentHeight()

	// Center the match line in viewport if possible
	targetScroll := match.LineNo - visibleHeight/2
	if targetScroll < 0 {
		targetScroll = 0
	}

	maxScroll := len(p.previewLines) - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if targetScroll > maxScroll {
		targetScroll = maxScroll
	}

	p.previewScroll = targetScroll
}

// openQuickOpen enters quick open mode.
func (p *Plugin) openQuickOpen() (plugin.Plugin, tea.Cmd) {
	// Build file cache if empty
	if len(p.quickOpenFiles) == 0 {
		p.buildFileCache()
	}

	p.quickOpenMode = true
	p.quickOpenQuery = ""
	p.quickOpenCursor = 0
	p.updateQuickOpenMatches()

	return p, nil
}

// handleQuickOpenKey handles key input during quick open mode.
func (p *Plugin) handleQuickOpenKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		p.quickOpenMode = false
		p.quickOpenQuery = ""
		p.quickOpenMatches = nil
		p.quickOpenCursor = 0

	case "enter":
		if len(p.quickOpenMatches) > 0 && p.quickOpenCursor < len(p.quickOpenMatches) {
			return p.selectQuickOpenMatch()
		}

	case "up", "ctrl+p":
		if p.quickOpenCursor > 0 {
			p.quickOpenCursor--
		}

	case "down", "ctrl+n":
		if p.quickOpenCursor < len(p.quickOpenMatches)-1 {
			p.quickOpenCursor++
		}

	case "backspace":
		if len(p.quickOpenQuery) > 0 {
			p.quickOpenQuery = p.quickOpenQuery[:len(p.quickOpenQuery)-1]
			p.updateQuickOpenMatches()
		}

	default:
		// Append printable characters
		if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
			p.quickOpenQuery += key
			p.updateQuickOpenMatches()
		}
	}

	return p, nil
}

// updateQuickOpenMatches filters files using fuzzy matching.
func (p *Plugin) updateQuickOpenMatches() {
	p.quickOpenMatches = FuzzyFilter(p.quickOpenFiles, p.quickOpenQuery, quickOpenMaxResults)

	// Reset cursor if out of bounds
	if p.quickOpenCursor >= len(p.quickOpenMatches) {
		if len(p.quickOpenMatches) > 0 {
			p.quickOpenCursor = len(p.quickOpenMatches) - 1
		} else {
			p.quickOpenCursor = 0
		}
	}
}

// selectQuickOpenMatch opens the selected file in preview.
func (p *Plugin) selectQuickOpenMatch() (plugin.Plugin, tea.Cmd) {
	if len(p.quickOpenMatches) == 0 || p.quickOpenCursor >= len(p.quickOpenMatches) {
		return p, nil
	}

	match := p.quickOpenMatches[p.quickOpenCursor]

	// Close quick open
	p.quickOpenMode = false
	p.quickOpenQuery = ""
	p.quickOpenMatches = nil
	p.quickOpenCursor = 0

	// Find the file in tree and expand parents
	var targetNode *FileNode
	p.walkTree(p.tree.Root, func(node *FileNode) {
		if node.Path == match.Path {
			targetNode = node
		}
	})

	if targetNode != nil {
		// Expand parents to make visible
		p.expandParents(targetNode)
		p.tree.Flatten()

		// Move tree cursor to file
		if idx := p.tree.IndexOf(targetNode); idx >= 0 {
			p.treeCursor = idx
			p.ensureTreeCursorVisible()
		}
	}

	// Load preview
	p.previewFile = match.Path
	p.previewScroll = 0
	p.previewLines = nil
	p.previewError = nil
	p.isBinary = false
	p.isTruncated = false
	p.activePane = PanePreview

	return p, LoadPreview(p.ctx.WorkDir, match.Path)
}

// openProjectSearch enters project-wide search mode.
func (p *Plugin) openProjectSearch() (plugin.Plugin, tea.Cmd) {
	p.projectSearchMode = true
	p.projectSearchState = NewProjectSearchState()
	return p, nil
}

// handleProjectSearchKey handles key input during project search mode.
func (p *Plugin) handleProjectSearchKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()
	state := p.projectSearchState

	switch key {
	case "esc":
		// Close project search
		p.projectSearchMode = false
		p.projectSearchState = nil

	case "enter":
		// Open selected file/match
		if state != nil && len(state.Results) > 0 {
			return p.openProjectSearchResult()
		}

	case "up", "ctrl+p":
		if state != nil && state.Cursor > 0 {
			state.Cursor--
		}

	case "down", "ctrl+n":
		if state != nil {
			maxIdx := state.FlatLen() - 1
			if state.Cursor < maxIdx {
				state.Cursor++
			}
		}

	case "ctrl+d":
		// Page down
		if state != nil {
			state.Cursor += 10
			maxIdx := state.FlatLen() - 1
			if state.Cursor > maxIdx {
				state.Cursor = maxIdx
			}
			if state.Cursor < 0 {
				state.Cursor = 0
			}
		}

	case "ctrl+u":
		// Page up
		if state != nil {
			state.Cursor -= 10
			if state.Cursor < 0 {
				state.Cursor = 0
			}
		}

	case "tab", " ":
		// Toggle file collapse
		if state != nil {
			state.ToggleFileCollapse()
		}

	case "alt+r":
		// Toggle regex mode
		if state != nil {
			state.UseRegex = !state.UseRegex
			if state.Query != "" {
				state.IsSearching = true
				return p, RunProjectSearch(p.ctx.WorkDir, state)
			}
		}

	case "alt+c":
		// Toggle case sensitivity
		if state != nil {
			state.CaseSensitive = !state.CaseSensitive
			if state.Query != "" {
				state.IsSearching = true
				return p, RunProjectSearch(p.ctx.WorkDir, state)
			}
		}

	case "alt+w":
		// Toggle whole word
		if state != nil {
			state.WholeWord = !state.WholeWord
			if state.Query != "" {
				state.IsSearching = true
				return p, RunProjectSearch(p.ctx.WorkDir, state)
			}
		}

	case "backspace":
		if state != nil && len(state.Query) > 0 {
			state.Query = state.Query[:len(state.Query)-1]
			if state.Query == "" {
				state.Results = nil
				state.Error = ""
			} else {
				state.IsSearching = true
				return p, RunProjectSearch(p.ctx.WorkDir, state)
			}
		}

	default:
		// Append printable characters
		if state != nil && len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
			state.Query += key
			state.IsSearching = true
			return p, RunProjectSearch(p.ctx.WorkDir, state)
		}
	}

	return p, nil
}

// openProjectSearchResult opens the selected search result.
func (p *Plugin) openProjectSearchResult() (plugin.Plugin, tea.Cmd) {
	state := p.projectSearchState
	if state == nil || len(state.Results) == 0 {
		return p, nil
	}

	path, lineNo := state.GetSelectedFile()
	if path == "" {
		return p, nil
	}

	// Close project search
	p.projectSearchMode = false
	p.projectSearchState = nil

	// Find the file in tree and expand parents
	var targetNode *FileNode
	p.walkTree(p.tree.Root, func(node *FileNode) {
		if node.Path == path {
			targetNode = node
		}
	})

	if targetNode != nil {
		// Expand parents to make visible
		p.expandParents(targetNode)
		p.tree.Flatten()

		// Move tree cursor to file
		if idx := p.tree.IndexOf(targetNode); idx >= 0 {
			p.treeCursor = idx
			p.ensureTreeCursorVisible()
		}
	}

	// Load preview
	p.previewFile = path
	p.previewScroll = 0
	p.previewLines = nil
	p.previewError = nil
	p.isBinary = false
	p.isTruncated = false
	p.activePane = PanePreview

	// If we have a line number, scroll to it after preview loads
	if lineNo > 0 {
		p.previewScroll = lineNo - 1 // Convert to 0-indexed
		if p.previewScroll < 0 {
			p.previewScroll = 0
		}
	}

	return p, LoadPreview(p.ctx.WorkDir, path)
}

// buildFileCache walks the filesystem to build the quick open file list.
// Respects gitignore and has limits to prevent issues on huge repos.
func (p *Plugin) buildFileCache() {
	p.quickOpenFiles = nil
	p.quickOpenError = ""

	ctx, cancel := context.WithTimeout(context.Background(), quickOpenTimeout)
	defer cancel()

	count := 0
	limited := false

	err := filepath.WalkDir(p.ctx.WorkDir, func(path string, d fs.DirEntry, err error) error {
		// Check timeout
		select {
		case <-ctx.Done():
			limited = true
			return filepath.SkipAll
		default:
		}

		if err != nil {
			return nil // Skip unreadable entries
		}

		// Get relative path
		rel, err := filepath.Rel(p.ctx.WorkDir, path)
		if err != nil {
			return nil
		}

		// Skip root
		if rel == "." {
			return nil
		}

		// Skip common large/irrelevant directories
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "node_modules" || name == "vendor" ||
				name == ".next" || name == "dist" || name == "build" ||
				name == "__pycache__" || name == ".venv" || name == "venv" ||
				name == ".idea" || name == ".vscode" {
				return filepath.SkipDir
			}
			// Check gitignore for directories
			if p.tree != nil && p.tree.gitIgnore != nil {
				if p.tree.gitIgnore.IsIgnored(rel, true) {
					return filepath.SkipDir
				}
			}
			return nil // Don't add directories to file list
		}

		// Skip hidden files (starting with .)
		if strings.HasPrefix(name, ".") {
			return nil
		}

		// Check gitignore for files
		if p.tree != nil && p.tree.gitIgnore != nil {
			if p.tree.gitIgnore.IsIgnored(rel, false) {
				return nil
			}
		}

		// Check file limit
		if count >= quickOpenMaxFiles {
			limited = true
			return filepath.SkipAll
		}

		p.quickOpenFiles = append(p.quickOpenFiles, rel)
		count++
		return nil
	})

	if err != nil && err != filepath.SkipAll {
		p.quickOpenError = "scan error: " + err.Error()
	} else if limited {
		if ctx.Err() != nil {
			p.quickOpenError = "scan timed out"
		} else {
			p.quickOpenError = "limited to 50000 files"
		}
	}

	// Sort files by path for consistent ordering
	sort.Strings(p.quickOpenFiles)
}

// visibleContentHeight returns the number of lines available for content.
func (p *Plugin) visibleContentHeight() int {
	// height - footer (1) - search bar (0 or 1) - pane border (2) - header (2)
	searchBar := 0
	if p.searchMode || p.contentSearchMode {
		searchBar = 1
	}
	h := p.height - 1 - searchBar - 2 - 2
	if h < 1 {
		return 1
	}
	return h
}

// ensureTreeCursorVisible adjusts scroll offset to keep cursor visible.
func (p *Plugin) ensureTreeCursorVisible() {
	visibleHeight := p.visibleContentHeight()

	if p.treeCursor < p.treeScrollOff {
		p.treeScrollOff = p.treeCursor
	} else if p.treeCursor >= p.treeScrollOff+visibleHeight {
		p.treeScrollOff = p.treeCursor - visibleHeight + 1
	}
}

// loadPreviewForCursor loads the preview for the file at the current tree cursor.
func (p *Plugin) loadPreviewForCursor() tea.Cmd {
	node := p.tree.GetNode(p.treeCursor)
	if node == nil || node.IsDir {
		return nil
	}
	p.previewFile = node.Path
	p.previewScroll = 0
	p.previewLines = nil
	p.previewError = nil
	p.isBinary = false
	p.isTruncated = false
	return LoadPreview(p.ctx.WorkDir, node.Path)
}

// handleSearchKey handles key input during search mode.
func (p *Plugin) handleSearchKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		p.searchMode = false
		p.searchQuery = ""

	case "enter":
		// Jump to selected match and exit search
		if len(p.searchMatches) > 0 {
			p.jumpToSearchMatch()
		}
		p.searchMode = false

	case "backspace":
		if len(p.searchQuery) > 0 {
			p.searchQuery = p.searchQuery[:len(p.searchQuery)-1]
			p.updateSearchMatches()
		}

	case "up", "ctrl+p":
		if p.searchCursor > 0 {
			p.searchCursor--
		}

	case "down", "ctrl+n":
		if p.searchCursor < len(p.searchMatches)-1 {
			p.searchCursor++
		}

	default:
		// Append printable characters to query
		if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
			p.searchQuery += key
			p.updateSearchMatches()
		}
	}

	return p, nil
}

// updateSearchMatches finds all nodes matching the search query.
func (p *Plugin) updateSearchMatches() {
	p.searchMatches = nil
	if p.searchQuery == "" {
		return
	}

	query := strings.ToLower(p.searchQuery)

	// Walk entire tree (not just visible nodes)
	p.walkTree(p.tree.Root, func(node *FileNode) {
		name := strings.ToLower(node.Name)
		if strings.Contains(name, query) {
			p.searchMatches = append(p.searchMatches, node)
		}
	})

	// Limit matches to prevent UI clutter
	if len(p.searchMatches) > 20 {
		p.searchMatches = p.searchMatches[:20]
	}

	p.searchCursor = 0
}

// walkTree recursively visits all nodes in the tree.
func (p *Plugin) walkTree(node *FileNode, fn func(*FileNode)) {
	if node == nil {
		return
	}
	for _, child := range node.Children {
		fn(child)
		if child.IsDir {
			// Load children if not already loaded
			if len(child.Children) == 0 {
				_ = p.tree.loadChildren(child)
			}
			p.walkTree(child, fn)
		}
	}
}

// jumpToSearchMatch navigates to the currently selected search match.
func (p *Plugin) jumpToSearchMatch() {
	if len(p.searchMatches) == 0 || p.searchCursor >= len(p.searchMatches) {
		return
	}

	match := p.searchMatches[p.searchCursor]

	// Expand all parent directories to make the match visible
	p.expandParents(match)

	// Reflatten the tree after expanding
	p.tree.Flatten()

	// Find the match in the flat list
	for i, node := range p.tree.FlatList {
		if node == match {
			p.treeCursor = i
			p.ensureTreeCursorVisible()
			break
		}
	}
}

// expandParents expands all ancestor directories of a node.
func (p *Plugin) expandParents(node *FileNode) {
	if node == nil || node.Parent == nil || node.Parent == p.tree.Root {
		return
	}

	// Recursively expand parents first
	p.expandParents(node.Parent)

	// Then expand this node's parent
	if node.Parent.IsDir && !node.Parent.IsExpanded {
		node.Parent.IsExpanded = true
	}
}

// navigateToFile navigates the file browser to a specific file path.
// Used when other plugins request navigation (e.g., git plugin opening file in browser).
func (p *Plugin) navigateToFile(path string) (plugin.Plugin, tea.Cmd) {
	// Find the file node in tree
	var targetNode *FileNode
	p.walkTree(p.tree.Root, func(node *FileNode) {
		if node.Path == path {
			targetNode = node
		}
	})

	if targetNode == nil {
		// File not found in tree, maybe it's new or ignored
		return p, nil
	}

	// Expand parents to make the file visible
	p.expandParents(targetNode)
	p.tree.Flatten()

	// Move tree cursor to file
	if idx := p.tree.IndexOf(targetNode); idx >= 0 {
		p.treeCursor = idx
		p.ensureTreeCursorVisible()
	}

	// Load preview
	p.previewFile = path
	p.previewScroll = 0
	p.previewLines = nil
	p.previewError = nil
	p.isBinary = false
	p.isTruncated = false
	p.activePane = PanePreview

	return p, LoadPreview(p.ctx.WorkDir, path)
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
		{ID: "project-search", Name: "Find", Description: "Search in project", Category: plugin.CategorySearch, Context: "file-browser-tree", Priority: 2},
		{ID: "search", Name: "Filter", Description: "Filter files by name", Category: plugin.CategorySearch, Context: "file-browser-tree", Priority: 3},
		{ID: "rename", Name: "Rename", Description: "Rename file or directory", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 4},
		{ID: "move", Name: "Move", Description: "Move file or directory", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 4},
		{ID: "reveal", Name: "Reveal", Description: "Reveal in file manager", Category: plugin.CategoryActions, Context: "file-browser-tree", Priority: 5},
		// Preview pane commands
		{ID: "quick-open", Name: "Open", Description: "Quick open file by name", Category: plugin.CategorySearch, Context: "file-browser-preview", Priority: 1},
		{ID: "project-search", Name: "Find", Description: "Search in project", Category: plugin.CategorySearch, Context: "file-browser-preview", Priority: 2},
		{ID: "search-content", Name: "Search", Description: "Search file content", Category: plugin.CategorySearch, Context: "file-browser-preview", Priority: 3},
		{ID: "back", Name: "Back", Description: "Return to file tree", Category: plugin.CategoryNavigation, Context: "file-browser-preview", Priority: 4},
		{ID: "reveal", Name: "Reveal", Description: "Reveal in file manager", Category: plugin.CategoryActions, Context: "file-browser-preview", Priority: 5},
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
		// File operation commands (move/rename)
		{ID: "confirm", Name: "Confirm", Description: "Confirm operation", Category: plugin.CategoryActions, Context: "file-browser-file-op", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Cancel operation", Category: plugin.CategoryActions, Context: "file-browser-file-op", Priority: 1},
	}
}

// FocusContext returns the current focus context.
func (p *Plugin) FocusContext() string {
	if p.projectSearchMode {
		return "file-browser-project-search"
	}
	if p.quickOpenMode {
		return "file-browser-quick-open"
	}
	if p.fileOpMode != FileOpNone {
		return "file-browser-file-op"
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
