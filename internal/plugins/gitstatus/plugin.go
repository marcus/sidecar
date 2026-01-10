package gitstatus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/filebrowser"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	pluginID   = "git-status"
	pluginName = "git"
	pluginIcon = "G"
)

// ViewMode represents the current view state.
type ViewMode int

const (
	ViewModeStatus          ViewMode = iota // Current file list (three-pane layout)
	ViewModeDiff                            // Full-screen diff view
	ViewModeCommit                          // Commit message editor
	ViewModePushMenu                        // Push options popup menu
	ViewModeConfirmDiscard                  // Confirm discard changes modal
	ViewModeBranchPicker                    // Branch selection modal
	ViewModeConfirmStashPop                 // Confirm stash pop modal
)

// FocusPane represents which pane is active in the three-pane view.
type FocusPane int

const (
	PaneSidebar FocusPane = iota
	PaneDiff
)

const commitHistoryPageSize = 50

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
	recentCommits      []*Commit // Cached recent commits for sidebar
	commitScrollOff    int       // Scroll offset for commits section in sidebar
	loadingMoreCommits bool      // Prevents duplicate load-more requests
	moreCommitsAvailable bool    // Whether more commits are available to load

	// Inline diff state (for three-pane view)
	selectedDiffFile    string       // File being previewed in diff pane
	diffPaneScroll      int          // Vertical scroll for inline diff
	diffPaneHorizScroll int          // Horizontal scroll for inline diff
	diffPaneParsedDiff  *ParsedDiff  // Parsed diff for inline view
	diffPaneViewMode    DiffViewMode // Unified or side-by-side for inline diff

	// Commit preview state (for three-pane view when on commit)
	previewCommit       *Commit // Commit being previewed in right pane
	previewCommitCursor int     // Cursor for file list in preview
	previewCommitScroll int     // Scroll offset for preview content

	// Diff state (for full-screen diff view)
	showDiff       bool
	diffContent    string
	diffFile       string
	diffScroll     int
	diffRaw        string       // Raw diff before delta processing
	diffCommit     string       // Commit hash if viewing commit diff
	diffViewMode   DiffViewMode // Line or side-by-side
	diffHorizOff   int          // Horizontal scroll for side-by-side
	parsedDiff     *ParsedDiff  // Parsed diff for enhanced rendering
	diffReturnMode ViewMode     // View mode to return to on esc


	// Push status state
	pushStatus         *PushStatus
	pushInProgress     bool
	pushError          string
	pushSuccess        bool      // Show success indicator after push
	pushSuccessTime    time.Time // When to auto-clear success
	pushMenuReturnMode ViewMode // Mode to return to when push menu closes
	pushMenuFocus      int      // 0=push, 1=force, 2=upstream
	pushMenuHover      int      // -1=none, 0=push, 1=force, 2=upstream

	// View dimensions
	width  int
	height int

	// Watcher
	watcher     *Watcher
	lastRefresh time.Time // Debounce rapid refreshes

	// Commit state
	commitMessage     textarea.Model
	commitError       string
	commitInProgress  bool
	commitButtonFocus bool // true when button is focused instead of textarea
	commitButtonHover bool // true when mouse is hovering over button

	// Mouse support
	mouseHandler *mouse.Handler

	// Discard confirm state
	discardFile            *FileEntry // File being confirmed for discard
	discardReturnMode      ViewMode   // Mode to return to when modal closes
	discardButtonFocus     int        // 0=none, 1=confirm, 2=cancel
	discardButtonHover     int        // 0=none, 1=confirm, 2=cancel

	// Stash pop confirm state
	stashPopItem        *Stash // Stash being confirmed for pop
	stashPopButtonFocus int    // 0=none, 1=confirm, 2=cancel
	stashPopButtonHover int    // 0=none, 1=confirm, 2=cancel

	// Syntax highlighting
	syntaxHighlighter     *SyntaxHighlighter // Cached highlighter for current file
	syntaxHighlighterFile string             // File the highlighter was created for

	// Branch picker state
	branches          []*Branch // List of branches
	branchCursor      int       // Current cursor position
	branchReturnMode  ViewMode  // Mode to return to when modal closes
	branchPickerHover int       // -1=none, 0+=branch index for hover state

	// Fetch/Pull state
	fetchInProgress bool
	pullInProgress  bool
	fetchSuccess    bool
	pullSuccess     bool
	fetchError      string
	pullError       string

	// History search state (/ in commit section)
	historySearchState *HistorySearchState
	historySearchMode  bool // True when search modal is open

	// History filter state
	historyFilterActive bool   // True when any filter is active
	historyFilterAuthor string // Filter by author name/email
	historyFilterPath   string // Filter by file path
	filteredCommits     []*Commit

	// Path filter input state
	pathFilterMode  bool   // True when path input modal is open
	pathFilterInput string // Current path input

	// Commit graph display state
	showCommitGraph  bool        // True when graph column is displayed
	commitGraphLines []GraphLine // Cached graph computation
}

// New creates a new git status plugin.
func New() *Plugin {
	return &Plugin{
		sidebarVisible: true,
		activePane:     PaneSidebar,
		mouseHandler:   mouse.NewHandler(),
	}
}

// getHighlighter returns a syntax highlighter for the given filename.
// Caches the highlighter to avoid re-creating on every render.
func (p *Plugin) getHighlighter(filename string) *SyntaxHighlighter {
	if filename == "" {
		return nil
	}
	if p.syntaxHighlighterFile == filename && p.syntaxHighlighter != nil {
		return p.syntaxHighlighter
	}
	p.syntaxHighlighter = NewSyntaxHighlighter(filename)
	p.syntaxHighlighterFile = filename
	return p.syntaxHighlighter
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

	// Load saved diff view mode preference
	if state.GetGitDiffMode() == "side-by-side" {
		p.diffViewMode = DiffViewSideBySide
	} else {
		p.diffViewMode = DiffViewUnified
	}

	// Load saved sidebar width from state
	if saved := state.GetGitStatusSidebarWidth(); saved > 0 {
		p.sidebarWidth = saved
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
		// Handle modal overlays first
		if p.historySearchMode {
			return p.updateHistorySearch(msg)
		}
		if p.pathFilterMode {
			return p.updatePathFilter(msg)
		}
		switch p.viewMode {
		case ViewModeStatus:
			return p.updateStatus(msg)
		case ViewModeDiff:
			return p.updateDiff(msg)
		case ViewModeCommit:
			return p.updateCommit(msg)
		case ViewModePushMenu:
			return p.updatePushMenu(msg)
		case ViewModeConfirmDiscard:
			return p.updateConfirmDiscard(msg)
		case ViewModeConfirmStashPop:
			return p.updateConfirmStashPop(msg)
		case ViewModeBranchPicker:
			return p.updateBranchPicker(msg)
		}

	case tea.MouseMsg:
		// Handle mouse events based on view mode
		switch p.viewMode {
		case ViewModeStatus:
			return p.handleMouse(msg)
		case ViewModeDiff:
			return p.handleDiffMouse(msg)
		case ViewModeBranchPicker:
			return p.handleBranchPickerMouse(msg)
		case ViewModeCommit:
			return p.handleCommitMouse(msg)
		}

	case app.RefreshMsg:
		return p, p.refresh()

	case app.PluginFocusedMsg:
		// Refresh data when navigating to this plugin
		p.lastRefresh = time.Now()
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case WatchStartedMsg:
		p.watcher = msg.Watcher
		return p, p.listenForWatchEvents()

	case WatchEventMsg:
		// File system changed, refresh and continue listening (debounce 500ms)
		if time.Since(p.lastRefresh) < 500*time.Millisecond {
			return p, p.listenForWatchEvents() // Skip refresh, keep listening
		}
		p.lastRefresh = time.Now()
		return p, tea.Batch(p.refresh(), p.loadRecentCommits(), p.listenForWatchEvents())

	case RefreshDoneMsg:
		// Clamp cursor to valid range if files changed
		maxCursor := p.totalSelectableItems() - 1
		if maxCursor < 0 {
			maxCursor = 0
		}
		if p.cursor > maxCursor {
			p.cursor = maxCursor
		}
		// Auto-load diff for first file if nothing selected
		if p.selectedDiffFile == "" && p.viewMode == ViewModeStatus {
			return p, p.autoLoadDiff()
		}
		return p, nil

	case DiffLoadedMsg:
		p.diffContent = msg.Content
		p.diffRaw = msg.Raw
		// Always parse diff for built-in rendering (even if delta is available)
		// This allows toggling between delta and built-in rendering at runtime
		p.parsedDiff, _ = ParseUnifiedDiff(msg.Raw)
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
		if msg.Commits == nil {
			if msg.PushStatus != nil {
				p.pushStatus = msg.PushStatus
				PopulatePushStatus(p.recentCommits, p.pushStatus)
			}
			return p, nil
		}

		p.moreCommitsAvailable = len(msg.Commits) >= commitHistoryPageSize
		prevCommitHash := ""
		if !p.historyFilterActive && p.cursorOnCommit() {
			commits := p.activeCommits()
			commitIdx := p.selectedCommitIndex()
			if commitIdx >= 0 && commitIdx < len(commits) {
				prevCommitHash = commits[commitIdx].Hash
			}
		}

		p.recentCommits = mergeRecentCommits(p.recentCommits, msg.Commits)
		p.pushStatus = msg.PushStatus
		PopulatePushStatus(p.recentCommits, p.pushStatus)
		// Recompute graph for new commits
		if p.showCommitGraph && len(p.recentCommits) > 0 {
			p.commitGraphLines = ComputeGraphForCommits(p.recentCommits)
		}
		if prevCommitHash != "" {
			if idx := indexOfCommitHash(p.recentCommits, prevCommitHash); idx >= 0 {
				p.cursor = len(p.tree.AllEntries()) + idx
			}
		}
		if !p.historyFilterActive {
			p.clampCommitScroll()
		}
		// Clamp cursor to valid range if commits changed
		maxCursor := p.totalSelectableItems() - 1
		if maxCursor < 0 {
			maxCursor = 0
		}
		if p.cursor > maxCursor {
			p.cursor = maxCursor
		}
		return p, p.ensureCommitListFilled()

	case MoreCommitsLoadedMsg:
		p.loadingMoreCommits = false
		if msg.Commits != nil && len(msg.Commits) > 0 {
			if len(msg.Commits) < commitHistoryPageSize {
				p.moreCommitsAvailable = false
			}
			p.recentCommits = append(p.recentCommits, msg.Commits...)
			// Recompute entire graph when commits are added
			if p.showCommitGraph {
				commits := p.activeCommits()
				p.commitGraphLines = ComputeGraphForCommits(commits)
			}
			return p, p.ensureCommitListFilled()
		}
		p.moreCommitsAvailable = false
		return p, nil

	case FilteredCommitsLoadedMsg:
		if msg.Commits != nil {
			p.filteredCommits = msg.Commits
			p.pushStatus = msg.PushStatus
			// Recompute graph for filtered commits
			if p.showCommitGraph && len(p.filteredCommits) > 0 {
				p.commitGraphLines = ComputeGraphForCommits(p.filteredCommits)
			} else if len(p.filteredCommits) == 0 {
				p.commitGraphLines = nil // Clear graph cache
			}
			// Reset cursor to first commit when filter applied
			entries := p.tree.AllEntries()
			if len(p.filteredCommits) > 0 {
				p.cursor = len(entries)
				p.commitScrollOff = 0
			}
		}
		return p, nil

	case CommitStatsLoadedMsg:
		// Find commit and update its stats
		for _, c := range p.recentCommits {
			if c.Hash == msg.Hash {
				c.Stats = msg.Stats
				break
			}
		}
		// Also check filtered commits
		for _, c := range p.filteredCommits {
			if c.Hash == msg.Hash {
				c.Stats = msg.Stats
				break
			}
		}
		return p, nil

	case CommitPreviewLoadedMsg:
		// Commit preview loaded for right pane (in status view)
		p.previewCommit = msg.Commit
		p.previewCommitCursor = 0
		p.previewCommitScroll = 0
		// Copy stats to the commit in the list for inline display
		if msg.Commit != nil {
			for _, c := range p.recentCommits {
				if c.Hash == msg.Commit.Hash {
					c.Stats = msg.Commit.Stats
					break
				}
			}
			for _, c := range p.filteredCommits {
				if c.Hash == msg.Commit.Hash {
					c.Stats = msg.Commit.Stats
					break
				}
			}
		}
		return p, nil

	case PushSuccessMsg:
		p.pushInProgress = false
		p.pushError = ""
		p.pushSuccess = true
		p.pushSuccessTime = time.Now()
		// Refresh to show updated push status
		return p, tea.Batch(p.refresh(), p.loadRecentCommits(), p.clearPushSuccessAfterDelay())

	case PushErrorMsg:
		p.pushInProgress = false
		p.pushError = msg.Err.Error()
		// Reload recent commits to update push status in case of partial push
		return p, p.loadRecentCommits()

	case PushSuccessClearMsg:
		p.pushSuccess = false
		return p, nil

	case StashResultMsg:
		if msg.Err != nil {
			// Show error toast
			toastMsg := "Stash failed: " + msg.Err.Error()
			return p, func() tea.Msg {
				return app.ToastMsg{Message: toastMsg, Duration: 3 * time.Second, IsError: true}
			}
		}
		// Show success toast and refresh
		var toastMsg string
		if msg.Operation == "push" {
			toastMsg = "Stashed changes"
		} else {
			toastMsg = "Applied " + msg.Ref
		}
		return p, tea.Batch(
			p.refresh(),
			p.loadRecentCommits(),
			func() tea.Msg {
				return app.ToastMsg{Message: toastMsg, Duration: 2 * time.Second}
			},
		)

	case BranchListLoadedMsg:
		p.branches = msg.Branches
		// Position cursor on current branch
		for i, b := range p.branches {
			if b.IsCurrent {
				p.branchCursor = i
				break
			}
		}
		return p, nil

	case BranchSwitchSuccessMsg:
		// Branch switched, close picker and refresh
		p.viewMode = p.branchReturnMode
		p.branches = nil
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case BranchErrorMsg:
		// TODO: Show error in UI
		return p, nil

	case FetchSuccessMsg:
		p.fetchInProgress = false
		p.fetchSuccess = true
		p.fetchError = ""
		// Refresh to show updated ahead/behind
		return p, tea.Batch(p.refresh(), p.loadRecentCommits(), p.clearFetchSuccessAfterDelay())

	case FetchErrorMsg:
		p.fetchInProgress = false
		p.fetchError = msg.Err.Error()
		return p, nil

	case PullSuccessMsg:
		p.pullInProgress = false
		p.pullSuccess = true
		p.pullError = ""
		return p, tea.Batch(p.refresh(), p.loadRecentCommits(), p.clearPullSuccessAfterDelay())

	case PullErrorMsg:
		p.pullInProgress = false
		p.pullError = msg.Err.Error()
		return p, nil

	case FetchSuccessClearMsg:
		p.fetchSuccess = false
		return p, nil

	case PullSuccessClearMsg:
		p.pullSuccess = false
		return p, nil

	case StashPopConfirmMsg:
		// Show stash pop confirmation modal
		p.stashPopItem = msg.Stash
		p.viewMode = ViewModeConfirmStashPop
		return p, nil

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, p.ensureCommitListFilled()
	}

	return p, nil
}

// totalSelectableItems returns the count of all selectable items (files + commits).
func (p *Plugin) totalSelectableItems() int {
	entries := p.tree.AllEntries()
	commits := p.recentCommits
	if p.historyFilterActive && p.filteredCommits != nil {
		commits = p.filteredCommits
	}
	return len(entries) + len(commits)
}

// cursorOnCommit returns true if cursor is on a commit (past all files).
func (p *Plugin) cursorOnCommit() bool {
	return p.cursor >= len(p.tree.AllEntries())
}

// activeCommits returns filtered commits if filter active, otherwise recent commits.
func (p *Plugin) activeCommits() []*Commit {
	if p.historyFilterActive && p.filteredCommits != nil {
		return p.filteredCommits
	}
	return p.recentCommits
}

// selectedCommitIndex returns the index into active commits when cursor is on commit.
func (p *Plugin) selectedCommitIndex() int {
	entries := p.tree.AllEntries()
	return p.cursor - len(entries)
}

// updateStatus handles key events in the status view.
func (p *Plugin) updateStatus(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	// Handle diff pane keys when focused on diff
	if p.activePane == PaneDiff {
		return p.updateStatusDiffPane(msg)
	}

	entries := p.tree.AllEntries()
	totalItems := p.totalSelectableItems()

	switch msg.String() {
	case "j", "down":
		if p.cursor < totalItems-1 {
			p.cursor++
			p.ensureCursorVisible()
			if p.cursorOnCommit() {
				commitIdx := p.selectedCommitIndex()
				p.ensureCommitVisible(commitIdx)
				// Trigger load-more when within 3 commits of end (only for unfiltered view)
				var loadMoreCmd tea.Cmd
				commits := p.activeCommits()
				if !p.historyFilterActive && p.moreCommitsAvailable && commitIdx >= len(commits)-3 && !p.loadingMoreCommits {
					loadMoreCmd = p.loadMoreCommits()
				}
				return p, tea.Batch(p.autoLoadCommitPreview(), loadMoreCmd)
			}
			return p, p.autoLoadDiff()
		}
		return p, nil

	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisible()
			if p.cursorOnCommit() {
				p.ensureCommitVisible(p.selectedCommitIndex())
				return p, p.autoLoadCommitPreview()
			}
			return p, p.autoLoadDiff()
		}
		return p, nil

	case "g":
		p.cursor = 0
		p.scrollOff = 0
		p.commitScrollOff = 0 // Reset commit scroll when jumping to top
		if p.cursorOnCommit() {
			return p, p.autoLoadCommitPreview()
		}
		return p, p.autoLoadDiff()

	case "G":
		if totalItems > 0 {
			p.cursor = totalItems - 1
			p.ensureCursorVisible()
			if p.cursorOnCommit() {
				commitIdx := p.selectedCommitIndex()
				p.ensureCommitVisible(commitIdx)
				// Trigger load-more when jumping to end
				var loadMoreCmd tea.Cmd
				if p.moreCommitsAvailable && commitIdx >= len(p.recentCommits)-3 && !p.loadingMoreCommits {
					loadMoreCmd = p.loadMoreCommits()
				}
				return p, tea.Batch(p.autoLoadCommitPreview(), loadMoreCmd)
			}
			return p, p.autoLoadDiff()
		}
		return p, nil

	case "l", "right":
		// Focus diff pane (when on a file) or commit preview pane (when on a commit)
		if p.sidebarVisible {
			if p.cursorOnCommit() && p.previewCommit != nil {
				p.activePane = PaneDiff
			} else if p.selectedDiffFile != "" {
				p.activePane = PaneDiff
			}
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

				// Handle folder entries - stage all children
				if entry.IsFolder {
					for _, child := range entry.Children {
						_ = p.tree.StageFile(child.Path)
					}
				} else {
					if err := p.tree.StageFile(entry.Path); err != nil {
						return p, nil
					}
				}
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
		// Open full-screen diff view for files
		if !p.cursorOnCommit() && len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			p.diffReturnMode = p.viewMode
			p.viewMode = ViewModeDiff
			p.diffFile = entry.Path
			p.diffCommit = ""
			p.diffScroll = 0
			if entry.IsFolder {
				return p, p.loadFullFolderDiff(entry)
			}
			return p, p.loadDiff(entry.Path, entry.Staged, entry.Status)
		}
		// For commits, focus the preview pane (same as l/right)
		if p.cursorOnCommit() && p.previewCommit != nil {
			p.activePane = PaneDiff
		}

	case "enter":
		// For folders: toggle expand/collapse
		// For files: open in editor
		// For commits: focus the preview pane
		if p.cursorOnCommit() {
			if p.previewCommit != nil {
				p.activePane = PaneDiff
			}
		} else if len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			if entry.IsFolder {
				// Toggle folder expansion
				entry.IsExpanded = !entry.IsExpanded
				// Reload diff for this folder
				return p, p.autoLoadDiff()
			}
			return p, p.openFile(entry.Path)
		}

	case "r":
		p.pushError = "" // Clear any stale push error
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case "S":
		// Stage all files
		if err := p.tree.StageAll(); err == nil {
			return p, tea.Batch(p.refresh(), p.loadRecentCommits())
		}

	case "O":
		// Open file in file browser (for files only, not commits)
		if !p.cursorOnCommit() && len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			return p, p.openInFileBrowser(entry.Path)
		}

	case "c":
		// Enter commit mode only if staged files exist
		if p.tree.HasStagedFiles() {
			p.viewMode = ViewModeCommit
			p.initCommitTextarea()
			return p, nil
		}

	case "P":
		// Open push menu (following lazygit convention)
		if p.canPush() && !p.pushInProgress {
			p.pushMenuReturnMode = p.viewMode
			p.viewMode = ViewModePushMenu
		}

	case "y":
		// Yank commit as markdown (when on commit in sidebar)
		if p.cursorOnCommit() {
			return p, p.copyCommitToClipboard()
		}

	case "Y":
		// Yank commit ID (when on commit in sidebar)
		if p.cursorOnCommit() {
			return p, p.copyCommitIDToClipboard()
		}

	case "D":
		// Discard changes (confirm modal) - only for modified/staged files, not commits
		if !p.cursorOnCommit() && len(entries) > 0 && p.cursor < len(entries) {
			entry := entries[p.cursor]
			// Don't allow discard on untracked folders (would delete)
			if entry.IsFolder && entry.Status == StatusUntracked {
				return p, nil
			}
			p.discardFile = entry
			p.discardReturnMode = p.viewMode
			p.viewMode = ViewModeConfirmDiscard
		}

	case "z":
		// Stash current changes (if there are any)
		if p.tree.TotalCount() > 0 {
			return p, p.doStashPush()
		}

	case "Z":
		// Pop latest stash - show confirm modal first
		return p, p.confirmStashPop()

	case "b":
		// Open branch picker
		p.branchReturnMode = p.viewMode
		p.branchCursor = 0
		p.viewMode = ViewModeBranchPicker
		return p, p.loadBranches()

	case "f":
		// On commit: filter by author; on file: fetch
		if p.cursorOnCommit() {
			commits := p.activeCommits()
			commitIdx := p.selectedCommitIndex()
			if commitIdx >= 0 && commitIdx < len(commits) {
				commit := commits[commitIdx]
				p.historyFilterAuthor = commit.Author
				p.historyFilterActive = true
				return p, p.loadFilteredCommits()
			}
		} else {
			// Fetch from remote
			if !p.fetchInProgress {
				p.fetchInProgress = true
				p.fetchError = ""
				p.fetchSuccess = false
				return p, p.doFetch()
			}
		}

	case "F":
		// Clear all history filters
		if p.historyFilterActive {
			p.historyFilterAuthor = ""
			p.historyFilterPath = ""
			p.historyFilterActive = false
			p.filteredCommits = nil
			// Recompute graph for unfiltered commits
			if p.showCommitGraph && len(p.recentCommits) > 0 {
				p.commitGraphLines = ComputeGraphForCommits(p.recentCommits)
			}
		}

	case "p":
		// On commit: filter by path (open modal); on file: pull
		if p.cursorOnCommit() {
			p.pathFilterMode = true
			p.pathFilterInput = ""
			return p, nil
		}
		// Pull from remote (only if no local changes to avoid conflicts)
		if !p.pullInProgress {
			p.pullInProgress = true
			p.pullError = ""
			p.pullSuccess = false
			return p, p.doPull()
		}

	case "/":
		// Open history search modal (only when on commits)
		if p.cursorOnCommit() {
			if p.historySearchState == nil {
				p.historySearchState = NewHistorySearchState()
			}
			p.historySearchState.Reset()
			p.historySearchMode = true
			return p, nil
		}

	case "n":
		// Next search match (after search committed)
		if p.historySearchState != nil && p.historySearchState.Committed && len(p.historySearchState.Matches) > 0 {
			p.historySearchState.Cursor++
			if p.historySearchState.Cursor >= len(p.historySearchState.Matches) {
				p.historySearchState.Cursor = 0 // Wrap around
			}
			return p, p.jumpToSearchMatch()
		}

	case "N":
		// Previous search match
		if p.historySearchState != nil && p.historySearchState.Committed && len(p.historySearchState.Matches) > 0 {
			p.historySearchState.Cursor--
			if p.historySearchState.Cursor < 0 {
				p.historySearchState.Cursor = len(p.historySearchState.Matches) - 1 // Wrap around
			}
			return p, p.jumpToSearchMatch()
		}

	case "esc":
		// ESC clears search state (if any active search)
		if p.historySearchState != nil && p.historySearchState.Committed {
			p.clearSearchState()
			return p, nil
		}

	case "v":
		// Toggle commit graph display (only when on commits)
		if p.cursorOnCommit() {
			p.showCommitGraph = !p.showCommitGraph
			if p.showCommitGraph {
				commits := p.activeCommits()
				p.commitGraphLines = ComputeGraphForCommits(commits)
			}
		}
		return p, nil
	}

	return p, nil
}

// updateStatusDiffPane handles key events when the diff pane is focused.
func (p *Plugin) updateStatusDiffPane(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	// If showing commit preview, handle file list navigation
	if p.previewCommit != nil && p.cursorOnCommit() {
		return p.updateCommitPreviewPane(msg)
	}

	switch msg.String() {
	case "esc":
		// ESC always returns to sidebar
		p.activePane = PaneSidebar

	case "h", "left":
		// Horizontal scroll left (use ESC or Tab to switch panes)
		if p.diffPaneHorizScroll > 0 {
			p.diffPaneHorizScroll -= 10
			if p.diffPaneHorizScroll < 0 {
				p.diffPaneHorizScroll = 0
			}
		}

	case "l", "right":
		// Horizontal scroll right
		p.diffPaneHorizScroll += 10
		p.clampDiffPaneHorizScroll()

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
		// Toggle view mode (unified/side-by-side) for inline diff pane
		if p.diffPaneViewMode == DiffViewUnified {
			p.diffPaneViewMode = DiffViewSideBySide
		} else {
			p.diffPaneViewMode = DiffViewUnified
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
			p.diffReturnMode = p.viewMode
			p.viewMode = ViewModeDiff
			p.diffFile = entry.Path
			p.diffCommit = ""
			p.diffScroll = 0
			return p, p.loadDiff(entry.Path, entry.Staged, entry.Status)
		}
	}

	return p, nil
}

// updateCommitPreviewPane handles key events when viewing commit preview in the diff pane.
func (p *Plugin) updateCommitPreviewPane(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	c := p.previewCommit
	if c == nil {
		return p, nil
	}

	switch msg.String() {
	case "esc", "h", "left":
		// Return to sidebar
		p.activePane = PaneSidebar

	case "j", "down":
		// Navigate file list
		if p.previewCommitCursor < len(c.Files)-1 {
			p.previewCommitCursor++
			p.ensurePreviewCursorVisible()
		}

	case "k", "up":
		if p.previewCommitCursor > 0 {
			p.previewCommitCursor--
			p.ensurePreviewCursorVisible()
		}

	case "g":
		p.previewCommitCursor = 0
		p.previewCommitScroll = 0

	case "G":
		if len(c.Files) > 0 {
			p.previewCommitCursor = len(c.Files) - 1
			p.ensurePreviewCursorVisible()
		}

	case "enter", "d":
		// Open full-screen diff for selected file in commit
		if p.previewCommitCursor < len(c.Files) {
			file := c.Files[p.previewCommitCursor]
			p.diffReturnMode = p.viewMode
			p.viewMode = ViewModeDiff
			p.diffFile = file.Path
			p.diffCommit = c.Hash
			p.diffScroll = 0
			return p, p.loadCommitFileDiff(c.Hash, file.Path)
		}

	case "tab":
		// Toggle sidebar visibility
		p.sidebarVisible = !p.sidebarVisible
		if p.sidebarVisible {
			p.activePane = PaneSidebar
		}

	case "y":
		// Yank commit as markdown
		return p, p.copyCommitToClipboard()

	case "Y":
		// Yank commit ID
		return p, p.copyCommitIDToClipboard()
	}

	return p, nil
}

// ensurePreviewCursorVisible adjusts scroll to keep commit preview cursor visible.
func (p *Plugin) ensurePreviewCursorVisible() {
	// Estimate visible file rows (rough - matches renderCommitPreview calculation)
	visibleRows := p.height - 15
	if visibleRows < 3 {
		visibleRows = 3
	}

	if p.previewCommitCursor < p.previewCommitScroll {
		p.previewCommitScroll = p.previewCommitCursor
	} else if p.previewCommitCursor >= p.previewCommitScroll+visibleRows {
		p.previewCommitScroll = p.previewCommitCursor - visibleRows + 1
	}
}

// autoLoadDiff triggers loading the diff for the currently selected file or folder.
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
	// Keep old diff visible until new one loads (prevents flashing)
	p.diffPaneScroll = 0
	// Clear commit preview when switching to file
	p.previewCommit = nil

	// Handle folder entries
	if entry.IsFolder {
		return p.loadFolderDiff(entry)
	}

	return p.loadInlineDiff(entry.Path, entry.Staged, entry.Status)
}

// autoLoadCommitPreview triggers loading commit detail for the currently selected commit.
func (p *Plugin) autoLoadCommitPreview() tea.Cmd {
	if !p.cursorOnCommit() {
		p.previewCommit = nil
		return nil
	}

	commits := p.activeCommits()
	commitIdx := p.selectedCommitIndex()
	if commitIdx < 0 || commitIdx >= len(commits) {
		p.previewCommit = nil
		return nil
	}

	commit := commits[commitIdx]
	// Already loaded this commit?
	if p.previewCommit != nil && p.previewCommit.Hash == commit.Hash {
		return nil
	}

	// Clear file diff when switching to commit
	p.selectedDiffFile = ""
	p.diffPaneParsedDiff = nil
	p.previewCommitCursor = 0
	p.previewCommitScroll = 0

	return p.loadCommitDetailForPreview(commit.Hash)
}

// loadCommitDetailForPreview loads commit detail for inline preview.
func (p *Plugin) loadCommitDetailForPreview(hash string) tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		commit, err := GetCommitDetail(workDir, hash)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return CommitPreviewLoadedMsg{Commit: commit}
	}
}

// CommitPreviewLoadedMsg is sent when commit preview is loaded.
type CommitPreviewLoadedMsg struct {
	Commit *Commit
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

// updateDiff handles key events in the diff view.
func (p *Plugin) updateDiff(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		// Return to previous view
		p.diffContent = ""
		p.diffRaw = ""
		p.parsedDiff = nil
		p.diffHorizOff = 0
		p.diffCommit = ""
		p.diffFile = ""
		p.viewMode = p.diffReturnMode
		// If returning to status view with commit preview, focus the preview pane
		if p.diffReturnMode == ViewModeStatus && p.previewCommit != nil {
			p.activePane = PaneDiff
		}

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

	case "tab":
		// Toggle sidebar visibility (match status view behavior)
		p.sidebarVisible = !p.sidebarVisible

	case "h", "left", "<", "H":
		// Horizontal scroll left
		if p.diffHorizOff > 0 {
			p.diffHorizOff -= 10
			if p.diffHorizOff < 0 {
				p.diffHorizOff = 0
			}
		}

	case "l", "right", ">", "L":
		// Horizontal scroll right
		p.diffHorizOff += 10
		p.clampDiffHorizScroll()

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

	case "O":
		// Open file in file browser
		if p.diffFile != "" {
			return p, p.openInFileBrowser(p.diffFile)
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
	case ViewModeDiff:
		// Use two-pane layout when sidebar is visible, otherwise full-width diff
		if p.sidebarVisible {
			content = p.renderDiffTwoPane()
		} else {
			content = p.renderDiffModal()
		}
	case ViewModeCommit:
		content = p.renderCommitModal()
	case ViewModePushMenu:
		content = p.renderPushMenu()
	case ViewModeConfirmDiscard:
		content = p.renderConfirmDiscard()
	case ViewModeConfirmStashPop:
		content = p.renderConfirmStashPop()
	case ViewModeBranchPicker:
		content = p.renderBranchPicker()
	default:
		// Use three-pane layout for status view
		content = p.renderThreePaneView()
	}

	// Overlay modals if active
	if p.historySearchMode {
		modal := p.renderHistorySearchModal(width)
		content = ui.OverlayModal(content, modal, width, height)
	}
	if p.pathFilterMode {
		modal := p.renderPathFilterModal(width)
		content = ui.OverlayModal(content, modal, width, height)
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
		// git-status context (files)
		{ID: "stage-file", Name: "Stage", Description: "Stage selected file for commit", Category: plugin.CategoryGit, Context: "git-status", Priority: 1},
		{ID: "unstage-file", Name: "Unstage", Description: "Remove file from staging area", Category: plugin.CategoryGit, Context: "git-status", Priority: 1},
		{ID: "commit", Name: "Commit", Description: "Open commit message editor", Category: plugin.CategoryGit, Context: "git-status", Priority: 1},
		{ID: "show-diff", Name: "Diff", Description: "View file changes", Category: plugin.CategoryView, Context: "git-status", Priority: 2},
		{ID: "stage-all", Name: "Stage all", Description: "Stage all modified files", Category: plugin.CategoryGit, Context: "git-status", Priority: 2},
		{ID: "push", Name: "Push", Description: "Push commits to remote", Category: plugin.CategoryGit, Context: "git-status", Priority: 2},
		{ID: "open-file", Name: "Open", Description: "Open file in editor", Category: plugin.CategoryActions, Context: "git-status", Priority: 3},
		{ID: "discard-changes", Name: "Discard", Description: "Discard changes to file", Category: plugin.CategoryGit, Context: "git-status", Priority: 3},
		{ID: "branch-picker", Name: "Branch", Description: "Switch branch", Category: plugin.CategoryGit, Context: "git-status", Priority: 3},
		{ID: "fetch", Name: "Fetch", Description: "Fetch from remote", Category: plugin.CategoryGit, Context: "git-status", Priority: 3},
		{ID: "pull", Name: "Pull", Description: "Pull from remote", Category: plugin.CategoryGit, Context: "git-status", Priority: 3},
		{ID: "open-in-file-browser", Name: "Browse", Description: "Open file in file browser", Category: plugin.CategoryNavigation, Context: "git-status", Priority: 4},
		// git-status-commits context (recent commits in sidebar)
		{ID: "view-commit", Name: "View", Description: "View commit details", Category: plugin.CategoryView, Context: "git-status-commits", Priority: 1},
		{ID: "push", Name: "Push", Description: "Push commits to remote", Category: plugin.CategoryGit, Context: "git-status-commits", Priority: 2},
		{ID: "search-history", Name: "Search", Description: "Search commit messages", Category: plugin.CategorySearch, Context: "git-status-commits", Priority: 2},
		{ID: "filter-author", Name: "Author", Description: "Filter by author", Category: plugin.CategorySearch, Context: "git-status-commits", Priority: 3},
		{ID: "filter-path", Name: "Path", Description: "Filter by file path", Category: plugin.CategorySearch, Context: "git-status-commits", Priority: 3},
		{ID: "clear-filter", Name: "Clear", Description: "Clear history filters", Category: plugin.CategoryActions, Context: "git-status-commits", Priority: 3},
		{ID: "next-match", Name: "Next", Description: "Next search match", Category: plugin.CategoryNavigation, Context: "git-status-commits", Priority: 4},
		{ID: "prev-match", Name: "Prev", Description: "Previous search match", Category: plugin.CategoryNavigation, Context: "git-status-commits", Priority: 4},
		{ID: "yank-commit", Name: "Yank", Description: "Copy commit as markdown", Category: plugin.CategoryActions, Context: "git-status-commits", Priority: 3},
		{ID: "yank-id", Name: "YankID", Description: "Copy commit ID", Category: plugin.CategoryActions, Context: "git-status-commits", Priority: 3},
		{ID: "toggle-graph", Name: "Graph", Description: "Toggle commit graph display", Category: plugin.CategoryView, Context: "git-status-commits", Priority: 2},
		// git-commit-preview context (commit preview in right pane)
		{ID: "view-diff", Name: "Diff", Description: "View file diff", Category: plugin.CategoryView, Context: "git-commit-preview", Priority: 1},
		{ID: "back", Name: "Back", Description: "Return to sidebar", Category: plugin.CategoryNavigation, Context: "git-commit-preview", Priority: 1},
		{ID: "yank-commit", Name: "Yank", Description: "Copy commit as markdown", Category: plugin.CategoryActions, Context: "git-commit-preview", Priority: 3},
		{ID: "yank-id", Name: "YankID", Description: "Copy commit ID", Category: plugin.CategoryActions, Context: "git-commit-preview", Priority: 3},
		// git-status-diff context (inline diff pane)
		{ID: "toggle-diff-view", Name: "View", Description: "Toggle unified/split diff view", Category: plugin.CategoryView, Context: "git-status-diff", Priority: 2},
		{ID: "toggle-sidebar", Name: "Panel", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "git-status-diff", Priority: 3},
		// git-diff context
		{ID: "close-diff", Name: "Close", Description: "Close diff view", Category: plugin.CategoryView, Context: "git-diff", Priority: 1},
		{ID: "scroll", Name: "Scroll", Description: "Scroll diff content", Category: plugin.CategoryNavigation, Context: "git-diff", Priority: 2},
		{ID: "toggle-sidebar", Name: "Panel", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "git-diff", Priority: 2},
		{ID: "toggle-diff-view", Name: "View", Description: "Toggle unified/split diff view", Category: plugin.CategoryView, Context: "git-diff", Priority: 3},
		{ID: "open-in-file-browser", Name: "Browse", Description: "Open file in file browser", Category: plugin.CategoryNavigation, Context: "git-diff", Priority: 4},
		// git-commit context
		{ID: "execute-commit", Name: "Commit", Description: "Create commit with message", Category: plugin.CategoryGit, Context: "git-commit", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Cancel commit", Category: plugin.CategoryActions, Context: "git-commit", Priority: 1},
		// git-push-menu context
		{ID: "push", Name: "Push", Description: "Push to remote", Category: plugin.CategoryGit, Context: "git-push-menu", Priority: 1},
		{ID: "force-push", Name: "Force", Description: "Force push", Category: plugin.CategoryGit, Context: "git-push-menu", Priority: 1},
		{ID: "push-upstream", Name: "Upstream", Description: "Push & set upstream", Category: plugin.CategoryGit, Context: "git-push-menu", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Cancel", Category: plugin.CategoryNavigation, Context: "git-push-menu", Priority: 2},
	}
}

// FocusContext returns the current focus context.
func (p *Plugin) FocusContext() string {
	switch p.viewMode {
	case ViewModeDiff:
		return "git-diff"
	case ViewModeCommit:
		return "git-commit"
	case ViewModePushMenu:
		return "git-push-menu"
	default:
		if p.activePane == PaneDiff {
			// Commit preview pane has different context than file diff pane
			if p.previewCommit != nil && p.cursorOnCommit() {
				return "git-commit-preview"
			}
			return "git-status-diff"
		}
		// Show different context when on a commit in sidebar
		if p.cursorOnCommit() {
			return "git-status-commits"
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
		return WatchStartedMsg{Watcher: watcher}
	}
}

// listenForWatchEvents waits for the next file system event.
func (p *Plugin) listenForWatchEvents() tea.Cmd {
	// Capture watcher reference to avoid race with Stop()
	w := p.watcher
	if w == nil {
		return nil
	}
	return func() tea.Msg {
		// When watcher is stopped, Events() channel is closed and this returns
		<-w.Events()
		return WatchEventMsg{}
	}
}

// loadDiff loads the diff for a file.
func (p *Plugin) loadDiff(path string, staged bool, status FileStatus) tea.Cmd {
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
			return ErrorMsg{Err: err}
		}

		return DiffLoadedMsg{Content: rawDiff, Raw: rawDiff}
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

// openInFileBrowser returns commands to switch to file browser and navigate to the file.
func (p *Plugin) openInFileBrowser(path string) tea.Cmd {
	return tea.Batch(
		app.FocusPlugin("file-browser"),
		func() tea.Msg {
			return filebrowser.NavigateToFileMsg{Path: path}
		},
	)
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

// visibleCommitCount returns how many commits can display in the sidebar.
func (p *Plugin) visibleCommitCount() int {
	// paneHeight = p.height - 2 (borders)
	// innerHeight = paneHeight - 2 (header)
	paneHeight := p.height - 2
	if paneHeight < 4 {
		paneHeight = 4
	}
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	return p.commitSectionCapacity(innerHeight)
}

func mergeRecentCommits(existing, latest []*Commit) []*Commit {
	if len(latest) == 0 {
		return existing
	}
	if len(existing) <= len(latest) {
		return latest
	}

	seen := make(map[string]struct{}, len(latest))
	merged := make([]*Commit, 0, len(existing)+len(latest))
	for _, c := range latest {
		if c == nil {
			continue
		}
		seen[c.Hash] = struct{}{}
		merged = append(merged, c)
	}
	for _, c := range existing {
		if c == nil {
			continue
		}
		if _, ok := seen[c.Hash]; ok {
			continue
		}
		merged = append(merged, c)
	}
	return merged
}

func indexOfCommitHash(commits []*Commit, hash string) int {
	for i, c := range commits {
		if c != nil && c.Hash == hash {
			return i
		}
	}
	return -1
}

func (p *Plugin) clampCommitScroll() {
	visibleCommits := p.visibleCommitCount()
	if visibleCommits < 1 {
		visibleCommits = 1
	}
	maxOff := len(p.recentCommits) - visibleCommits
	if maxOff < 0 {
		maxOff = 0
	}
	if p.commitScrollOff < 0 {
		p.commitScrollOff = 0
	}
	if p.commitScrollOff > maxOff {
		p.commitScrollOff = maxOff
	}
}

func (p *Plugin) ensureCommitListFilled() tea.Cmd {
	if p.historyFilterActive || p.loadingMoreCommits || !p.moreCommitsAvailable {
		return nil
	}
	visibleCommits := p.visibleCommitCount()
	if visibleCommits < 1 || len(p.recentCommits) >= visibleCommits {
		return nil
	}
	return p.loadMoreCommits()
}

// commitSectionCapacity returns how many commits can display in the sidebar
// given the current layout and inner height.
func (p *Plugin) commitSectionCapacity(visibleHeight int) int {
	// visibleHeight already excludes the "Files" header lines.
	linesUsed := 0

	entries := p.tree.AllEntries()
	if len(entries) == 0 {
		// "Working tree clean"
		linesUsed++
	} else {
		// Reserve space for commits when files are present (match renderSidebar)
		commitsReserve := 5
		if len(p.recentCommits) > 3 {
			commitsReserve = 6
		}
		filesHeight := visibleHeight - commitsReserve - 2 // -2 for section headers
		if filesHeight < 3 {
			filesHeight = 3
		}

		lineNum := 0
		if len(p.tree.Staged) > 0 && lineNum < filesHeight {
			lineNum++ // section header
			for range p.tree.Staged {
				if lineNum >= filesHeight {
					break
				}
				lineNum++
			}
		}
		if len(p.tree.Modified) > 0 && lineNum < filesHeight {
			if len(p.tree.Staged) > 0 {
				if lineNum < filesHeight {
					lineNum++ // blank line between sections
				}
			}
			if lineNum < filesHeight {
				lineNum++ // section header
			}
			for range p.tree.Modified {
				if lineNum >= filesHeight {
					break
				}
				lineNum++
			}
		}
		if len(p.tree.Untracked) > 0 && lineNum < filesHeight {
			if len(p.tree.Staged) > 0 || len(p.tree.Modified) > 0 {
				if lineNum < filesHeight {
					lineNum++ // blank line between sections
				}
			}
			if lineNum < filesHeight {
				lineNum++ // section header
			}
			for range p.tree.Untracked {
				if lineNum >= filesHeight {
					break
				}
				lineNum++
			}
		}
		linesUsed += lineNum
	}

	// Separator (blank line + separator line)
	linesUsed += 2

	// Remote operation status line if present
	if p.pushInProgress || p.fetchInProgress || p.pullInProgress ||
		p.pushSuccess || p.fetchSuccess || p.pullSuccess ||
		p.pushError != "" || p.fetchError != "" || p.pullError != "" {
		linesUsed++
	}

	// Commit header
	linesUsed += 1

	available := visibleHeight - linesUsed
	if available < 2 {
		available = 2
	}
	return available
}

// ensureCommitVisible adjusts commitScrollOff to keep selected commit visible.
// commitIdx is the absolute index into activeCommits.
func (p *Plugin) ensureCommitVisible(commitIdx int) {
	commits := p.activeCommits()
	if len(commits) == 0 {
		p.commitScrollOff = 0
		return
	}

	// Use a conservative estimate for visible commits
	visibleCommits := p.visibleCommitCount()
	if visibleCommits < 1 {
		visibleCommits = 1
	}

	// Only adjust scroll if absolutely necessary
	if commitIdx < p.commitScrollOff {
		// Commit is above visible area - scroll up to show it at top
		p.commitScrollOff = commitIdx
	} else if commitIdx >= p.commitScrollOff+visibleCommits {
		// Commit is below visible area - scroll down minimally
		p.commitScrollOff = commitIdx - visibleCommits + 1
	}
	// If commit is within visible range, don't adjust scroll at all

	// Clamp scroll offset to valid range
	if p.commitScrollOff < 0 {
		p.commitScrollOff = 0
	}
	// Ensure we don't scroll past where we'd have empty rows
	// This is the key: maxOff ensures the visible area is always filled
	maxOff := len(commits) - visibleCommits
	if maxOff < 0 {
		maxOff = 0
	}
	if p.commitScrollOff > maxOff {
		p.commitScrollOff = maxOff
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
type WatchStartedMsg struct{ Watcher *Watcher }
type ErrorMsg struct{ Err error }
type DiffLoadedMsg struct {
	Content string // Rendered content (may be from delta)
	Raw     string // Raw diff for built-in rendering
}
type OpenFileMsg struct {
	Editor string
	Path   string
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
	Commits    []*Commit
	PushStatus *PushStatus
}

// MoreCommitsLoadedMsg is sent when additional commits are fetched for infinite scroll.
type MoreCommitsLoadedMsg struct {
	Commits    []*Commit
	PushStatus *PushStatus
}

// FilteredCommitsLoadedMsg is sent when filtered commits are fetched.
type FilteredCommitsLoadedMsg struct {
	Commits    []*Commit
	PushStatus *PushStatus
}

// CommitStatsLoadedMsg is sent when commit stats are loaded.
type CommitStatsLoadedMsg struct {
	Hash  string
	Stats CommitStats
}

// PushSuccessMsg is sent when a push completes successfully.
type PushSuccessMsg struct {
	Output string
}

// PushErrorMsg is sent when a push fails.
type PushErrorMsg struct {
	Err error
}

// PushStatusLoadedMsg is sent when push status is loaded.
type PushStatusLoadedMsg struct {
	Status *PushStatus
}

// PushSuccessClearMsg is sent to clear the push success indicator.
type PushSuccessClearMsg struct{}

// StashResultMsg is sent when a stash operation completes.
type StashResultMsg struct {
	Operation string // "push" or "pop"
	Ref       string // stash ref for display (e.g. "stash@{0}")
	Err       error
}

// BranchListLoadedMsg is sent when branch list is loaded.
type BranchListLoadedMsg struct {
	Branches []*Branch
}

// BranchSwitchSuccessMsg is sent when branch switch succeeds.
type BranchSwitchSuccessMsg struct {
	Branch string
}

// BranchErrorMsg is sent when a branch operation fails.
type BranchErrorMsg struct {
	Err error
}

// FetchSuccessMsg is sent when fetch succeeds.
type FetchSuccessMsg struct {
	Output string
}

// FetchErrorMsg is sent when fetch fails.
type FetchErrorMsg struct {
	Err error
}

// PullSuccessMsg is sent when pull succeeds.
type PullSuccessMsg struct {
	Output string
}

// PullErrorMsg is sent when pull fails.
type PullErrorMsg struct {
	Err error
}

// StashErrorMsg is sent when stash operations fail.
type StashErrorMsg struct {
	Err error
}

// FetchSuccessClearMsg is sent to clear the fetch success indicator.
type FetchSuccessClearMsg struct{}

// PullSuccessClearMsg is sent to clear the pull success indicator.
type PullSuccessClearMsg struct{}

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

// loadRecentCommits loads recent commits for the sidebar with push status.
func (p *Plugin) loadRecentCommits() tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		commits, pushStatus, err := GetCommitHistoryWithPushStatus(workDir, commitHistoryPageSize)
		if err != nil {
			return RecentCommitsLoadedMsg{Commits: nil, PushStatus: nil}
		}
		return RecentCommitsLoadedMsg{Commits: commits, PushStatus: pushStatus}
	}
}

// loadMoreCommits fetches the next batch of commits for infinite scroll.
func (p *Plugin) loadMoreCommits() tea.Cmd {
	if p.loadingMoreCommits {
		return nil
	}
	p.loadingMoreCommits = true

	workDir := p.ctx.WorkDir
	skip := len(p.recentCommits)
	return func() tea.Msg {
		commits, pushStatus, err := GetCommitHistoryWithPushStatusOffset(workDir, commitHistoryPageSize, skip)
		if err != nil {
			return MoreCommitsLoadedMsg{Commits: nil, PushStatus: nil}
		}
		return MoreCommitsLoadedMsg{Commits: commits, PushStatus: pushStatus}
	}
}

// loadCommitStats fetches stats for a specific commit (lazy loading).
func (p *Plugin) loadCommitStats(hash string) tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		commit, err := GetCommitDetail(workDir, hash)
		if err != nil || commit == nil {
			return CommitStatsLoadedMsg{Hash: hash, Stats: CommitStats{}}
		}
		return CommitStatsLoadedMsg{Hash: hash, Stats: commit.Stats}
	}
}

// loadFilteredCommits fetches commits with current filter options.
func (p *Plugin) loadFilteredCommits() tea.Cmd {
	workDir := p.ctx.WorkDir
	opts := HistoryFilterOpts{
		Author: p.historyFilterAuthor,
		Path:   p.historyFilterPath,
		Limit:  50,
	}
	return func() tea.Msg {
		commits, pushStatus, err := GetCommitHistoryFilteredWithPushStatus(workDir, opts)
		if err != nil {
			return FilteredCommitsLoadedMsg{Commits: nil, PushStatus: nil}
		}
		return FilteredCommitsLoadedMsg{Commits: commits, PushStatus: pushStatus}
	}
}

// loadFolderDiff loads a concatenated diff for all files in a folder.
func (p *Plugin) loadFolderDiff(entry *FileEntry) tea.Cmd {
	workDir := p.ctx.WorkDir
	folderPath := entry.Path
	children := entry.Children
	return func() tea.Msg {
		rawDiff, err := GetFolderDiff(workDir, children)
		if err != nil {
			return InlineDiffLoadedMsg{File: folderPath, Raw: "", Parsed: nil}
		}
		parsed, _ := ParseUnifiedDiff(rawDiff)
		return InlineDiffLoadedMsg{File: folderPath, Raw: rawDiff, Parsed: parsed}
	}
}

// loadFullFolderDiff loads a concatenated diff for full-screen view.
func (p *Plugin) loadFullFolderDiff(entry *FileEntry) tea.Cmd {
	workDir := p.ctx.WorkDir
	children := entry.Children
	return func() tea.Msg {
		rawDiff, err := GetFolderDiff(workDir, children)
		if err != nil {
			return ErrorMsg{Err: err}
		}

		return DiffLoadedMsg{Content: rawDiff, Raw: rawDiff}
	}
}

// loadCommitFileDiff loads diff for a file in a commit.
func (p *Plugin) loadCommitFileDiff(hash, path string) tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		rawDiff, err := GetCommitDiff(workDir, hash, path)
		if err != nil {
			return ErrorMsg{Err: err}
		}

		return DiffLoadedMsg{Content: rawDiff, Raw: rawDiff}
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
	// Size for modal: modalWidth - 6 (border+padding) - 2 (textarea internal padding)
	textareaWidth := p.commitModalWidth() - 8
	if textareaWidth < 40 {
		textareaWidth = 40
	}
	p.commitMessage.SetWidth(textareaWidth)
	p.commitMessage.SetHeight(4)
	p.commitError = ""
	p.commitButtonFocus = false
	p.commitButtonHover = false
}

// updateCommit handles key events in the commit view.
func (p *Plugin) updateCommit(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel commit, return to status
		p.viewMode = ViewModeStatus
		p.commitError = ""
		return p, nil

	case "ctrl+s":
		// Execute commit if message not empty
		return p, p.tryCommit()

	case "tab", "shift+tab":
		// Toggle focus between textarea and button
		p.commitButtonFocus = !p.commitButtonFocus
		if p.commitButtonFocus {
			p.commitMessage.Blur()
		} else {
			p.commitMessage.Focus()
		}
		return p, nil

	case "enter":
		// If button is focused, execute commit
		if p.commitButtonFocus {
			return p, p.tryCommit()
		}
		// Otherwise let textarea handle it (newline)
	}

	// Pass other keys to textarea (only if textarea is focused)
	if !p.commitButtonFocus {
		var cmd tea.Cmd
		p.commitMessage, cmd = p.commitMessage.Update(msg)
		return p, cmd
	}

	return p, nil
}

// tryCommit attempts to execute the commit if message is valid.
func (p *Plugin) tryCommit() tea.Cmd {
	message := strings.TrimSpace(p.commitMessage.Value())
	if message == "" {
		p.commitError = "Commit message cannot be empty"
		return nil
	}
	p.commitInProgress = true
	return p.doCommit(message)
}

// updatePushMenu handles key events in the push menu.
func (p *Plugin) updatePushMenu(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	const itemCount = 3 // push, force, upstream

	switch msg.String() {
	case "tab", "j", "down":
		p.pushMenuFocus = (p.pushMenuFocus + 1) % itemCount
		return p, nil
	case "shift+tab", "k", "up":
		p.pushMenuFocus = (p.pushMenuFocus - 1 + itemCount) % itemCount
		return p, nil
	case "enter":
		return p.executePushMenuAction(p.pushMenuFocus)
	case "esc", "q":
		p.viewMode = p.pushMenuReturnMode
		p.pushMenuFocus = 0
		return p, nil
	case "p":
		// Regular push (shortcut)
		return p.executePushMenuAction(0)
	case "f":
		// Force push (shortcut)
		return p.executePushMenuAction(1)
	case "u":
		// Push and set upstream (shortcut)
		return p.executePushMenuAction(2)
	}
	return p, nil
}

// executePushMenuAction executes the push menu action at the given index.
func (p *Plugin) executePushMenuAction(idx int) (plugin.Plugin, tea.Cmd) {
	p.viewMode = p.pushMenuReturnMode
	p.pushInProgress = true
	p.pushError = ""
	p.pushSuccess = false
	p.pushMenuFocus = 0

	switch idx {
	case 0:
		return p, p.doPush(false)
	case 1:
		return p, p.doPushForce()
	case 2:
		return p, p.doPushSetUpstream()
	}
	return p, nil
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

// doPush executes a git push asynchronously.
func (p *Plugin) doPush(force bool) tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		output, err := ExecutePush(workDir, force)
		if err != nil {
			return PushErrorMsg{Err: err}
		}
		return PushSuccessMsg{Output: output}
	}
}

// doPushForce executes a force push with lease.
func (p *Plugin) doPushForce() tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		output, err := ExecutePushForce(workDir)
		if err != nil {
			return PushErrorMsg{Err: err}
		}
		return PushSuccessMsg{Output: output}
	}
}

// doPushSetUpstream executes a push with upstream tracking.
func (p *Plugin) doPushSetUpstream() tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		output, err := ExecutePushSetUpstream(workDir)
		if err != nil {
			return PushErrorMsg{Err: err}
		}
		return PushSuccessMsg{Output: output}
	}
}

// clearPushSuccessAfterDelay returns a command that clears the push success indicator after 3 seconds.
func (p *Plugin) clearPushSuccessAfterDelay() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return PushSuccessClearMsg{}
	})
}

// canPush returns true if there are commits that can be pushed.
func (p *Plugin) canPush() bool {
	return p.pushStatus != nil && p.pushStatus.CanPush()
}

// doStashPush stashes all current changes.
func (p *Plugin) doStashPush() tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		err := StashPush(workDir)
		return StashResultMsg{Operation: "push", Err: err}
	}
}

// doStashPop pops the latest stash.
func (p *Plugin) doStashPop() tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		err := StashPop(workDir)
		return StashResultMsg{Operation: "pop", Ref: "stash@{0}", Err: err}
	}
}


// doFetch fetches from remote.
func (p *Plugin) doFetch() tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		output, err := ExecuteFetch(workDir)
		if err != nil {
			return FetchErrorMsg{Err: err}
		}
		return FetchSuccessMsg{Output: output}
	}
}

// doPull pulls from remote.
func (p *Plugin) doPull() tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		output, err := ExecutePull(workDir)
		if err != nil {
			return PullErrorMsg{Err: err}
		}
		return PullSuccessMsg{Output: output}
	}
}

// clearFetchSuccessAfterDelay returns a command that clears the fetch success indicator after 3 seconds.
func (p *Plugin) clearFetchSuccessAfterDelay() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return FetchSuccessClearMsg{}
	})
}

// clearPullSuccessAfterDelay returns a command that clears the pull success indicator after 3 seconds.
func (p *Plugin) clearPullSuccessAfterDelay() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return PullSuccessClearMsg{}
	})
}

// confirmStashPop fetches the latest stash and shows the confirm modal.
func (p *Plugin) confirmStashPop() tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		stashList, err := GetStashList(workDir)
		if err != nil || len(stashList.Stashes) == 0 {
			return StashErrorMsg{Err: fmt.Errorf("no stashes available")}
		}
		return StashPopConfirmMsg{Stash: stashList.Stashes[0]}
	}
}

// StashPopConfirmMsg is sent when the stash pop confirm modal should be shown.
type StashPopConfirmMsg struct {
	Stash *Stash
}

// updateConfirmStashPop handles key events in the confirm stash pop modal.
// handleConfirmKey handles common key input for confirm dialogs with button pairs.
// focusPtr points to the button focus state (1=confirm, 2=cancel).
// onConfirm is called when the user confirms (enter on confirm, y/Y).
// onCancel is called when the user cancels (enter on cancel, esc/n/N/q).
func (p *Plugin) handleConfirmKey(msg tea.KeyMsg, focusPtr *int, onConfirm, onCancel func()) (bool, tea.Cmd) {
	switch msg.String() {
	case "tab":
		if *focusPtr == 1 {
			*focusPtr = 2
		} else {
			*focusPtr = 1
		}
		return true, nil
	case "shift+tab":
		if *focusPtr == 2 {
			*focusPtr = 1
		} else {
			*focusPtr = 2
		}
		return true, nil
	case "enter":
		if *focusPtr == 2 {
			onCancel()
			return true, nil
		}
		onConfirm()
		return true, nil
	case "y", "Y":
		onConfirm()
		return true, nil
	case "esc", "n", "N", "q":
		onCancel()
		return true, nil
	}
	return false, nil
}

func (p *Plugin) updateConfirmStashPop(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	var cmd tea.Cmd
	handled, _ := p.handleConfirmKey(msg, &p.stashPopButtonFocus,
		func() {
			// Confirm
			if p.stashPopItem != nil {
				cmd = p.doStashPop()
			}
			p.viewMode = ViewModeStatus
			p.stashPopItem = nil
			p.stashPopButtonFocus = 1
		},
		func() {
			// Cancel
			p.viewMode = ViewModeStatus
			p.stashPopItem = nil
			p.stashPopButtonFocus = 1
		},
	)
	if handled {
		return p, cmd
	}
	return p, nil
}

// updateConfirmDiscard handles key events in the confirm discard modal.
func (p *Plugin) updateConfirmDiscard(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	var cmd tea.Cmd
	handled, _ := p.handleConfirmKey(msg, &p.discardButtonFocus,
		func() {
			// Confirm
			if p.discardFile != nil {
				cmd = p.doDiscard(p.discardFile)
			}
			p.viewMode = p.discardReturnMode
			p.discardFile = nil
			p.discardButtonFocus = 1
		},
		func() {
			// Cancel
			p.viewMode = p.discardReturnMode
			p.discardFile = nil
			p.discardButtonFocus = 1
		},
	)
	if handled {
		return p, cmd
	}
	return p, nil
}

// doDiscard executes the git discard operation.
func (p *Plugin) doDiscard(entry *FileEntry) tea.Cmd {
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		var err error
		if entry.Status == StatusUntracked {
			// Remove untracked file
			err = DiscardUntracked(workDir, entry.Path)
		} else if entry.Staged {
			// Unstage and restore staged file
			err = DiscardStaged(workDir, entry.Path)
		} else {
			// Restore modified file
			err = DiscardModified(workDir, entry.Path)
		}
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return RefreshDoneMsg{}
	}
}

// clampDiffHorizScroll clamps diffHorizOff to valid range based on content width.
func (p *Plugin) clampDiffHorizScroll() {
	if p.parsedDiff == nil {
		return
	}
	// Calculate content width like the view does
	panelWidth := (p.width - 3) / 2 // -3 for center separator
	lineNoWidth := 5
	contentWidth := panelWidth - lineNoWidth - 2

	clipInfo := GetSideBySideClipInfo(p.parsedDiff, contentWidth, p.diffHorizOff)
	maxScroll := clipInfo.MaxContentWidth - contentWidth
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.diffHorizOff > maxScroll {
		p.diffHorizOff = maxScroll
	}
	if p.diffHorizOff < 0 {
		p.diffHorizOff = 0
	}
}

// clampDiffPaneHorizScroll clamps diffPaneHorizScroll to valid range.
func (p *Plugin) clampDiffPaneHorizScroll() {
	if p.diffPaneParsedDiff == nil {
		return
	}
	// Calculate content width for inline diff pane
	paneWidth := p.width - p.sidebarWidth - 2
	panelWidth := (paneWidth - 3) / 2
	lineNoWidth := 5
	contentWidth := panelWidth - lineNoWidth - 2

	clipInfo := GetSideBySideClipInfo(p.diffPaneParsedDiff, contentWidth, p.diffPaneHorizScroll)
	maxScroll := clipInfo.MaxContentWidth - contentWidth
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.diffPaneHorizScroll > maxScroll {
		p.diffPaneHorizScroll = maxScroll
	}
	if p.diffPaneHorizScroll < 0 {
		p.diffPaneHorizScroll = 0
	}
}
