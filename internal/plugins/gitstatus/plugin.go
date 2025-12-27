package gitstatus

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sst/sidecar/internal/app"
	"github.com/sst/sidecar/internal/plugin"
	"github.com/sst/sidecar/internal/state"
)

const (
	pluginID   = "git-status"
	pluginName = "Git Status"
	pluginIcon = "G"
)

// ViewMode represents the current view state.
type ViewMode int

const (
	ViewModeStatus       ViewMode = iota // Current file list (three-pane layout)
	ViewModeHistory                      // Commit browser
	ViewModeCommitDetail                 // Single commit files
	ViewModeDiff                         // Full-screen diff view (from history)
	ViewModeCommit                       // Commit message editor
)

// FocusPane represents which pane is active in the three-pane view.
type FocusPane int

const (
	PaneSidebar FocusPane = iota
	PaneDiff
)

// Plugin implements the git status plugin.
type Plugin struct {
	ctx       *plugin.Context
	tree      *FileTree
	focused   bool
	cursor    int
	scrollOff int

	// View mode state machine
	viewMode ViewMode

	// Three-pane layout state
	activePane     FocusPane // Which pane is focused
	sidebarVisible bool      // Toggle sidebar with Tab
	sidebarWidth   int       // Calculated width (~30%)
	diffPaneWidth  int       // Calculated width (~70%)
	recentCommits  []*Commit // Cached recent commits for sidebar

	// Inline diff state (for three-pane view)
	selectedDiffFile    string      // File being previewed in diff pane
	diffPaneScroll      int         // Vertical scroll for inline diff
	diffPaneHorizScroll int         // Horizontal scroll for inline diff
	diffPaneParsedDiff  *ParsedDiff // Parsed diff for inline view

	// Diff state (for full-screen diff view)
	showDiff     bool
	diffContent  string
	diffFile     string
	diffScroll   int
	diffRaw      string       // Raw diff before delta processing
	diffCommit   string       // Commit hash if viewing commit diff
	diffViewMode DiffViewMode // Line or side-by-side
	diffHorizOff int          // Horizontal scroll for side-by-side
	parsedDiff   *ParsedDiff  // Parsed diff for enhanced rendering

	// History state
	commits            []*Commit
	selectedCommit     *Commit
	historyCursor      int
	historyScroll      int
	commitDetailCursor int
	commitDetailScroll int

	// External tool integration
	externalTool *ExternalTool

	// View dimensions
	width  int
	height int

	// Watcher
	watcher *Watcher

	// Commit state
	commitMessage    textarea.Model
	commitError      string
	commitInProgress bool
}

// New creates a new git status plugin.
func New() *Plugin {
	return &Plugin{
		sidebarVisible: true,
		activePane:     PaneSidebar,
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
	// Check if we're in a git repository
	gitDir := filepath.Join(ctx.WorkDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return err // Not a git repo, silently degrade
	}

	p.ctx = ctx
	p.tree = NewFileTree(ctx.WorkDir)
	p.externalTool = NewExternalTool(ToolModeAuto)

	// Load saved diff view mode preference
	if state.GetGitDiffMode() == "side-by-side" {
		p.diffViewMode = DiffViewSideBySide
	} else {
		p.diffViewMode = DiffViewUnified
	}

	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	return tea.Batch(
		p.refresh(),
		p.startWatcher(),
		p.loadRecentCommits(),
	)
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	if p.watcher != nil {
		p.watcher.Stop()
	}
}

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch p.viewMode {
		case ViewModeStatus:
			return p.updateStatus(msg)
		case ViewModeHistory:
			return p.updateHistory(msg)
		case ViewModeCommitDetail:
			return p.updateCommitDetail(msg)
		case ViewModeDiff:
			return p.updateDiff(msg)
		case ViewModeCommit:
			return p.updateCommit(msg)
		}

	case app.RefreshMsg:
		return p, p.refresh()

	case WatchEventMsg:
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case RefreshDoneMsg:
		// Auto-load diff for first file if nothing selected
		if p.selectedDiffFile == "" && p.viewMode == ViewModeStatus {
			return p, p.autoLoadDiff()
		}
		return p, nil

	case DiffLoadedMsg:
		p.diffContent = msg.Content
		p.diffRaw = msg.Raw
		// Parse diff for built-in rendering (when not using delta)
		if p.externalTool == nil || !p.externalTool.ShouldUseDelta() {
			p.parsedDiff, _ = ParseUnifiedDiff(msg.Raw)
		} else {
			p.parsedDiff = nil
		}
		return p, nil

	case HistoryLoadedMsg:
		p.commits = msg.Commits
		return p, nil

	case CommitDetailLoadedMsg:
		p.selectedCommit = msg.Commit
		return p, nil

	case CommitSuccessMsg:
		// Commit succeeded, return to status view and refresh
		p.viewMode = ViewModeStatus
		p.commitMessage.Reset()
		p.commitInProgress = false
		p.commitError = ""
		return p, p.refresh()

	case CommitErrorMsg:
		// Commit failed, show error and keep message for retry
		p.commitError = msg.Err.Error()
		p.commitInProgress = false
		return p, nil

	case InlineDiffLoadedMsg:
		// Only update if this is still the selected file
		if msg.File == p.selectedDiffFile {
			p.diffPaneParsedDiff = msg.Parsed
			p.diffPaneScroll = 0
		}
		return p, nil

	case RecentCommitsLoadedMsg:
		p.recentCommits = msg.Commits
		return p, nil

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
	}

	return p, nil
}

// updateStatus handles key events in the status view.
func (p *Plugin) updateStatus(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	// Handle diff pane keys when focused on diff
	if p.activePane == PaneDiff {
		return p.updateStatusDiffPane(msg)
	}

	entries := p.tree.AllEntries()

	switch msg.String() {
	case "j", "down":
		if p.cursor < len(entries)-1 {
			p.cursor++
			p.ensureCursorVisible()
			return p, p.autoLoadDiff()
		}

	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisible()
			return p, p.autoLoadDiff()
		}

	case "g":
		p.cursor = 0
		p.scrollOff = 0
		return p, p.autoLoadDiff()

	case "G":
		if len(entries) > 0 {
			p.cursor = len(entries) - 1
			p.ensureCursorVisible()
			return p, p.autoLoadDiff()
		}

	case "l", "right":
		// Focus diff pane
		if p.sidebarVisible && p.selectedDiffFile != "" {
			p.activePane = PaneDiff
		}

	case "tab":
		// Toggle sidebar visibility
		p.sidebarVisible = !p.sidebarVisible
		if !p.sidebarVisible {
			p.activePane = PaneDiff
		} else {
			p.activePane = PaneSidebar
		}

	case "s":
		if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			if !entry.Staged {
				stagedCount := len(p.tree.Staged)
				totalEntries := len(entries)

				if err := p.tree.StageFile(entry.Path); err == nil {
					// After staging, move cursor to first unstaged file position
					newFirstUnstaged := stagedCount + 1
					if newFirstUnstaged < totalEntries {
						p.cursor = newFirstUnstaged
					} else {
						p.cursor = totalEntries - 1
					}
					return p, tea.Batch(p.refresh(), p.loadRecentCommits())
				}
			}
		}

	case "u":
		if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			if entry.Staged {
				if err := p.tree.UnstageFile(entry.Path); err == nil {
					return p, tea.Batch(p.refresh(), p.loadRecentCommits())
				}
			}
		}

	case "d":
		// Open full-screen diff view
		if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			p.viewMode = ViewModeDiff
			p.diffFile = entry.Path
			p.diffCommit = ""
			p.diffScroll = 0
			return p, p.loadDiff(entry.Path, entry.Staged, entry.Status)
		}

	case "enter":
		if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			return p, p.openFile(entry.Path)
		}

	case "h":
		// Switch to history view
		p.viewMode = ViewModeHistory
		p.historyCursor = 0
		p.historyScroll = 0
		return p, p.loadHistory()

	case "r":
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case "S":
		// Stage all files
		if err := p.tree.StageAll(); err == nil {
			return p, tea.Batch(p.refresh(), p.loadRecentCommits())
		}

	case "c":
		// Enter commit mode only if staged files exist
		if p.tree.HasStagedFiles() {
			p.viewMode = ViewModeCommit
			p.initCommitTextarea()
			return p, nil
		}
	}

	return p, nil
}

// updateStatusDiffPane handles key events when the diff pane is focused.
func (p *Plugin) updateStatusDiffPane(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// ESC always returns to sidebar
		p.activePane = PaneSidebar

	case "h":
		// h scrolls left if scrolled, otherwise returns to sidebar
		if p.diffPaneHorizScroll > 0 {
			p.diffPaneHorizScroll -= 10
			if p.diffPaneHorizScroll < 0 {
				p.diffPaneHorizScroll = 0
			}
		} else {
			p.activePane = PaneSidebar
		}

	case "left":
		// Horizontal scroll left
		if p.diffPaneHorizScroll > 0 {
			p.diffPaneHorizScroll -= 10
			if p.diffPaneHorizScroll < 0 {
				p.diffPaneHorizScroll = 0
			}
		}

	case "l", "right":
		// Horizontal scroll right
		p.diffPaneHorizScroll += 10

	case "j", "down":
		p.diffPaneScroll++

	case "k", "up":
		if p.diffPaneScroll > 0 {
			p.diffPaneScroll--
		}

	case "g":
		p.diffPaneScroll = 0
		p.diffPaneHorizScroll = 0

	case "G":
		if p.diffPaneParsedDiff != nil {
			lines := countParsedDiffLines(p.diffPaneParsedDiff)
			maxScroll := lines - (p.height - 6)
			if maxScroll > 0 {
				p.diffPaneScroll = maxScroll
			}
		}

	case "ctrl+d":
		p.diffPaneScroll += 10
		// Clamp to max
		if p.diffPaneParsedDiff != nil {
			lines := countParsedDiffLines(p.diffPaneParsedDiff)
			maxScroll := lines - (p.height - 6)
			if maxScroll < 0 {
				maxScroll = 0
			}
			if p.diffPaneScroll > maxScroll {
				p.diffPaneScroll = maxScroll
			}
		}

	case "ctrl+u":
		p.diffPaneScroll -= 10
		if p.diffPaneScroll < 0 {
			p.diffPaneScroll = 0
		}

	case "0":
		// Reset horizontal scroll
		p.diffPaneHorizScroll = 0

	case "v":
		// Toggle view mode (unified/side-by-side) - affects all diff views
		if p.diffViewMode == DiffViewUnified {
			p.diffViewMode = DiffViewSideBySide
		} else {
			p.diffViewMode = DiffViewUnified
		}

	case "tab":
		// Toggle sidebar visibility
		p.sidebarVisible = !p.sidebarVisible
		if p.sidebarVisible {
			p.activePane = PaneSidebar
		}

	case "d":
		// Open full-screen diff view for current file
		entries := p.tree.AllEntries()
		if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			p.viewMode = ViewModeDiff
			p.diffFile = entry.Path
			p.diffCommit = ""
			p.diffScroll = 0
			return p, p.loadDiff(entry.Path, entry.Staged, entry.Status)
		}
	}

	return p, nil
}

// autoLoadDiff triggers loading the diff for the currently selected file.
func (p *Plugin) autoLoadDiff() tea.Cmd {
	entries := p.tree.AllEntries()
	if len(entries) == 0 || p.cursor >= len(entries) {
		p.selectedDiffFile = ""
		p.diffPaneParsedDiff = nil
		return nil
	}

	entry := entries[p.cursor]
	if entry.Path == p.selectedDiffFile {
		return nil // Already loaded
	}

	p.selectedDiffFile = entry.Path
	p.diffPaneParsedDiff = nil
	p.diffPaneScroll = 0
	return p.loadInlineDiff(entry.Path, entry.Staged, entry.Status)
}

// countParsedDiffLines counts total lines in a parsed diff.
func countParsedDiffLines(diff *ParsedDiff) int {
	if diff == nil {
		return 0
	}
	count := 0
	for _, hunk := range diff.Hunks {
		count += len(hunk.Lines) + 1 // +1 for hunk header
	}
	return count
}

// updateHistory handles key events in the history view.
func (p *Plugin) updateHistory(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "h":
		p.viewMode = ViewModeStatus
		p.commits = nil

	case "j", "down":
		if p.commits != nil && p.historyCursor < len(p.commits)-1 {
			p.historyCursor++
			p.ensureHistoryCursorVisible()
		}

	case "k", "up":
		if p.historyCursor > 0 {
			p.historyCursor--
			p.ensureHistoryCursorVisible()
		}

	case "g":
		p.historyCursor = 0
		p.historyScroll = 0

	case "G":
		if p.commits != nil && len(p.commits) > 0 {
			p.historyCursor = len(p.commits) - 1
			p.ensureHistoryCursorVisible()
		}

	case "enter", "d":
		if p.commits != nil && p.historyCursor < len(p.commits) {
			commit := p.commits[p.historyCursor]
			p.viewMode = ViewModeCommitDetail
			p.commitDetailCursor = 0
			p.commitDetailScroll = 0
			return p, p.loadCommitDetail(commit.Hash)
		}
	}

	return p, nil
}

// updateCommitDetail handles key events in the commit detail view.
func (p *Plugin) updateCommitDetail(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		p.viewMode = ViewModeHistory
		p.selectedCommit = nil

	case "j", "down":
		if p.selectedCommit != nil && p.commitDetailCursor < len(p.selectedCommit.Files)-1 {
			p.commitDetailCursor++
			p.ensureCommitDetailCursorVisible()
		}

	case "k", "up":
		if p.commitDetailCursor > 0 {
			p.commitDetailCursor--
			p.ensureCommitDetailCursorVisible()
		}

	case "g":
		p.commitDetailCursor = 0
		p.commitDetailScroll = 0

	case "G":
		if p.selectedCommit != nil && len(p.selectedCommit.Files) > 0 {
			p.commitDetailCursor = len(p.selectedCommit.Files) - 1
			p.ensureCommitDetailCursorVisible()
		}

	case "enter", "d":
		if p.selectedCommit != nil && p.commitDetailCursor < len(p.selectedCommit.Files) {
			file := p.selectedCommit.Files[p.commitDetailCursor]
			p.viewMode = ViewModeDiff
			p.diffFile = file.Path
			p.diffCommit = p.selectedCommit.Hash
			p.diffScroll = 0
			return p, p.loadCommitFileDiff(p.selectedCommit.Hash, file.Path)
		}
	}

	return p, nil
}

// updateDiff handles key events in the diff view.
func (p *Plugin) updateDiff(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		// Return to previous view based on context
		p.diffContent = ""
		p.diffRaw = ""
		p.parsedDiff = nil
		p.diffHorizOff = 0
		if p.diffCommit != "" {
			// Came from commit detail
			p.viewMode = ViewModeCommitDetail
			p.diffCommit = ""
		} else {
			// Came from status
			p.viewMode = ViewModeStatus
		}
		p.diffFile = ""

	case "j", "down":
		p.diffScroll++

	case "k", "up":
		if p.diffScroll > 0 {
			p.diffScroll--
		}

	case "g":
		p.diffScroll = 0
		p.diffHorizOff = 0

	case "G":
		lines := countLines(p.diffContent)
		maxScroll := lines - (p.height - 2)
		if maxScroll > 0 {
			p.diffScroll = maxScroll
		}

	case "v":
		// Toggle between unified and side-by-side view
		if p.diffViewMode == DiffViewUnified {
			p.diffViewMode = DiffViewSideBySide
			_ = state.SetGitDiffMode("side-by-side")
		} else {
			p.diffViewMode = DiffViewUnified
			_ = state.SetGitDiffMode("unified")
		}
		p.diffHorizOff = 0

	case "<", "H":
		// Horizontal scroll left in side-by-side mode
		if p.diffViewMode == DiffViewSideBySide && p.diffHorizOff > 0 {
			p.diffHorizOff -= 10
			if p.diffHorizOff < 0 {
				p.diffHorizOff = 0
			}
		}

	case ">", "L":
		// Horizontal scroll right in side-by-side mode
		if p.diffViewMode == DiffViewSideBySide {
			p.diffHorizOff += 10
		}

	case "ctrl+d":
		// Page down (~10 lines)
		p.diffScroll += 10
		// Clamp to max
		lines := countLines(p.diffContent)
		maxScroll := lines - (p.height - 2)
		if maxScroll < 0 {
			maxScroll = 0
		}
		if p.diffScroll > maxScroll {
			p.diffScroll = maxScroll
		}

	case "ctrl+u":
		// Page up (~10 lines)
		p.diffScroll -= 10
		if p.diffScroll < 0 {
			p.diffScroll = 0
		}
	}

	return p, nil
}

// View renders the plugin.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height

	var content string
	switch p.viewMode {
	case ViewModeHistory:
		content = p.renderHistory()
	case ViewModeCommitDetail:
		content = p.renderCommitDetail()
	case ViewModeDiff:
		content = p.renderDiffModal()
	case ViewModeCommit:
		content = p.renderCommit()
	default:
		// Use three-pane layout for status view
		content = p.renderThreePaneView()
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
	return []plugin.Command{
		{ID: "stage-file", Name: "Stage", Context: "git-status"},
		{ID: "unstage-file", Name: "Unstage", Context: "git-status"},
		{ID: "stage-all", Name: "Stage all", Context: "git-status"},
		{ID: "commit", Name: "Commit", Context: "git-status"},
		{ID: "show-diff", Name: "Diff", Context: "git-status"},
		{ID: "show-history", Name: "History", Context: "git-status"},
		{ID: "open-file", Name: "Open", Context: "git-status"},
		{ID: "back", Name: "Back", Context: "git-history"},
		{ID: "view-commit", Name: "View", Context: "git-history"},
		{ID: "back", Name: "Back", Context: "git-commit-detail"},
		{ID: "view-diff", Name: "Diff", Context: "git-commit-detail"},
		{ID: "close-diff", Name: "Close", Context: "git-diff"},
		{ID: "scroll", Name: "Scroll", Context: "git-diff"},
		{ID: "cancel", Name: "Cancel", Context: "git-commit"},
		{ID: "execute-commit", Name: "Commit", Context: "git-commit"},
	}
}

// FocusContext returns the current focus context.
func (p *Plugin) FocusContext() string {
	switch p.viewMode {
	case ViewModeHistory:
		return "git-history"
	case ViewModeCommitDetail:
		return "git-commit-detail"
	case ViewModeDiff:
		return "git-diff"
	case ViewModeCommit:
		return "git-commit"
	default:
		if p.activePane == PaneDiff {
			return "git-status-diff"
		}
		return "git-status"
	}
}

// Diagnostics returns plugin health info.
func (p *Plugin) Diagnostics() []plugin.Diagnostic {
	status := "ok"
	detail := p.tree.Summary()
	if p.tree.TotalCount() == 0 {
		status = "clean"
	}
	return []plugin.Diagnostic{
		{ID: "git-status", Status: status, Detail: detail},
	}
}

// refresh reloads the git status.
func (p *Plugin) refresh() tea.Cmd {
	return func() tea.Msg {
		if err := p.tree.Refresh(); err != nil {
			return ErrorMsg{Err: err}
		}
		return RefreshDoneMsg{}
	}
}

// startWatcher starts the file system watcher.
func (p *Plugin) startWatcher() tea.Cmd {
	return func() tea.Msg {
		watcher, err := NewWatcher(p.ctx.WorkDir)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		p.watcher = watcher
		return WatchStartedMsg{}
	}
}

// loadDiff loads the diff for a file, rendering through delta if available.
func (p *Plugin) loadDiff(path string, staged bool, status FileStatus) tea.Cmd {
	workDir := p.ctx.WorkDir
	extTool := p.externalTool
	width := p.width
	return func() tea.Msg {
		var rawDiff string
		var err error

		// Untracked files need special handling - create new file diff
		if status == StatusUntracked {
			rawDiff, err = GetNewFileDiff(workDir, path)
		} else {
			rawDiff, err = GetDiff(workDir, path, staged)
		}
		if err != nil {
			return ErrorMsg{Err: err}
		}

		// Try to render with delta if available
		content := rawDiff
		if extTool != nil && extTool.ShouldUseDelta() {
			rendered, _ := extTool.RenderWithDelta(rawDiff, false, width)
			content = rendered
		}

		return DiffLoadedMsg{Content: content, Raw: rawDiff}
	}
}

// openFile opens a file in the default editor.
func (p *Plugin) openFile(path string) tea.Cmd {
	return func() tea.Msg {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vim"
		}
		fullPath := filepath.Join(p.ctx.WorkDir, path)
		return OpenFileMsg{Editor: editor, Path: fullPath}
	}
}

// ensureCursorVisible adjusts scroll to keep cursor visible.
func (p *Plugin) ensureCursorVisible() {
	visibleRows := p.height - 4 // Account for header and section spacing
	if visibleRows < 1 {
		visibleRows = 1
	}

	if p.cursor < p.scrollOff {
		p.scrollOff = p.cursor
	} else if p.cursor >= p.scrollOff+visibleRows {
		p.scrollOff = p.cursor - visibleRows + 1
	}
}

// countLines counts newlines in a string.
func countLines(s string) int {
	n := 1
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}

// Message types
type RefreshDoneMsg struct{}
type WatchEventMsg struct{}
type WatchStartedMsg struct{}
type ErrorMsg struct{ Err error }
type DiffLoadedMsg struct {
	Content string // Rendered content (may be from delta)
	Raw     string // Raw diff for built-in rendering
}
type OpenFileMsg struct {
	Editor string
	Path   string
}
type HistoryLoadedMsg struct {
	Commits []*Commit
}
type CommitDetailLoadedMsg struct {
	Commit *Commit
}
type CommitSuccessMsg struct {
	Hash    string
	Subject string
}
type CommitErrorMsg struct {
	Err error
}

// InlineDiffLoadedMsg is sent when an inline diff finishes loading.
type InlineDiffLoadedMsg struct {
	File   string
	Raw    string
	Parsed *ParsedDiff
}

// RecentCommitsLoadedMsg is sent when recent commits are loaded for sidebar.
type RecentCommitsLoadedMsg struct {
	Commits []*Commit
}

// loadHistory loads commit history.
func (p *Plugin) loadHistory() tea.Cmd {
	return func() tea.Msg {
		commits, err := GetCommitHistory(p.ctx.WorkDir, 50)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return HistoryLoadedMsg{Commits: commits}
	}
}

// loadInlineDiff loads a diff for inline preview in the three-pane view.
func (p *Plugin) loadInlineDiff(path string, staged bool, status FileStatus) tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		var rawDiff string
		var err error

		// Untracked files need special handling - create new file diff
		if status == StatusUntracked {
			rawDiff, err = GetNewFileDiff(workDir, path)
		} else {
			rawDiff, err = GetDiff(workDir, path, staged)
		}
		if err != nil {
			return InlineDiffLoadedMsg{File: path, Raw: "", Parsed: nil}
		}
		parsed, _ := ParseUnifiedDiff(rawDiff)
		return InlineDiffLoadedMsg{File: path, Raw: rawDiff, Parsed: parsed}
	}
}

// loadRecentCommits loads recent commits for the sidebar.
func (p *Plugin) loadRecentCommits() tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		commits, err := GetCommitHistory(workDir, 8)
		if err != nil {
			return RecentCommitsLoadedMsg{Commits: nil}
		}
		return RecentCommitsLoadedMsg{Commits: commits}
	}
}

// loadCommitDetail loads full commit information.
func (p *Plugin) loadCommitDetail(hash string) tea.Cmd {
	return func() tea.Msg {
		commit, err := GetCommitDetail(p.ctx.WorkDir, hash)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return CommitDetailLoadedMsg{Commit: commit}
	}
}

// loadCommitFileDiff loads diff for a file in a commit.
func (p *Plugin) loadCommitFileDiff(hash, path string) tea.Cmd {
	return func() tea.Msg {
		rawDiff, err := GetCommitDiff(p.ctx.WorkDir, hash, path)
		if err != nil {
			return ErrorMsg{Err: err}
		}

		// Try to render with delta if available
		content := rawDiff
		if p.externalTool != nil && p.externalTool.ShouldUseDelta() {
			rendered, _ := p.externalTool.RenderWithDelta(rawDiff, false, p.width)
			content = rendered
		}

		return DiffLoadedMsg{Content: content, Raw: rawDiff}
	}
}

// ensureHistoryCursorVisible adjusts scroll to keep history cursor visible.
func (p *Plugin) ensureHistoryCursorVisible() {
	visibleRows := p.height - 3
	if visibleRows < 1 {
		visibleRows = 1
	}

	if p.historyCursor < p.historyScroll {
		p.historyScroll = p.historyCursor
	} else if p.historyCursor >= p.historyScroll+visibleRows {
		p.historyScroll = p.historyCursor - visibleRows + 1
	}
}

// ensureCommitDetailCursorVisible adjusts scroll to keep commit detail cursor visible.
func (p *Plugin) ensureCommitDetailCursorVisible() {
	visibleRows := p.height - 12 // Account for commit metadata
	if visibleRows < 1 {
		visibleRows = 1
	}

	if p.commitDetailCursor < p.commitDetailScroll {
		p.commitDetailScroll = p.commitDetailCursor
	} else if p.commitDetailCursor >= p.commitDetailScroll+visibleRows {
		p.commitDetailScroll = p.commitDetailCursor - visibleRows + 1
	}
}

// TickCmd returns a command that triggers a refresh every second.
func TickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return app.RefreshMsg{}
	})
}

// initCommitTextarea initializes the commit message textarea.
func (p *Plugin) initCommitTextarea() {
	p.commitMessage = textarea.New()
	p.commitMessage.SetValue("") // Ensure empty
	p.commitMessage.Placeholder = "Type your commit message..."
	// Make placeholder more visible (default color 240 is too dim)
	p.commitMessage.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
	p.commitMessage.Focus()
	p.commitMessage.CharLimit = 0
	p.commitMessage.SetWidth(60)
	p.commitMessage.SetHeight(5)
	p.commitError = ""
}

// updateCommit handles key events in the commit view.
func (p *Plugin) updateCommit(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel commit, return to status
		p.viewMode = ViewModeStatus
		p.commitError = ""
		return p, nil

	case "alt+enter", "alt+s":
		// Execute commit if message not empty
		message := strings.TrimSpace(p.commitMessage.Value())
		if message == "" {
			p.commitError = "Commit message cannot be empty"
			return p, nil
		}
		p.commitInProgress = true
		return p, p.doCommit(message)
	}

	// Pass other keys to textarea
	var cmd tea.Cmd
	p.commitMessage, cmd = p.commitMessage.Update(msg)
	return p, cmd
}

// doCommit executes the git commit asynchronously.
func (p *Plugin) doCommit(message string) tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		hash, err := ExecuteCommit(workDir, message)
		if err != nil {
			return CommitErrorMsg{Err: err}
		}
		// Extract first line as subject
		subject := strings.Split(message, "\n")[0]
		return CommitSuccessMsg{Hash: hash, Subject: subject}
	}
}
