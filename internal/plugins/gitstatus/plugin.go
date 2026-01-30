package gitstatus

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/filebrowser"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
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
	ViewModePullMenu                        // Pull options popup menu
	ViewModeConfirmDiscard                  // Confirm discard changes modal
	ViewModeBranchPicker                    // Branch selection modal
	ViewModeConfirmStashPop                 // Confirm stash pop modal
	ViewModePullConflict                    // Pull conflict resolution modal
	ViewModeError                           // Generic error modal for git operation failures
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
	ctx      *plugin.Context
	repoRoot string // Resolved git repo root (may differ from ctx.WorkDir if started in subdirectory)
	tree     *FileTree
	focused   bool
	cursor    int
	scrollOff int

	// View mode state machine
	viewMode ViewMode

	// Three-pane layout state
	activePane           FocusPane // Which pane is focused
	sidebarRestore       FocusPane // Tracks pane focused before collapse; restored on expand via toggleSidebar()
	sidebarVisible       bool      // Toggle sidebar with Tab
	sidebarWidth         int       // Calculated width (~30%)
	diffPaneWidth        int       // Calculated width (~70%)
	recentCommits        []*Commit // Cached recent commits for sidebar
	commitScrollOff      int       // Scroll offset for commits section in sidebar
	loadingMoreCommits   bool      // Prevents duplicate load-more requests
	moreCommitsAvailable bool      // Whether more commits are available to load

	// Inline diff state (for three-pane view)
	selectedDiffFile     string       // File being previewed in diff pane
	forceNextDiffReload  bool         // Bypass dedup on next autoLoadDiff call
	diffPaneScroll       int          // Vertical scroll for inline diff
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
	diffLoaded      bool         // True once diff load completes (distinguishes loading vs empty)
	diffWrapEnabled bool         // Wrap long lines instead of truncating

	// Push status state
	pushStatus              *PushStatus
	pushInProgress          bool
	pushError               string
	pushSuccess             bool      // Show success indicator after push
	pushSuccessTime         time.Time // When to auto-clear success
	pushMenuReturnMode      ViewMode  // Mode to return to when push menu closes
	pushMenuFocus           int       // 0=push, 1=force, 2=upstream
	pushMenuModal           *modal.Modal
	pushMenuModalWidth      int
	pushPreservedCommitHash string // Hash of selected commit when push started

	// Pull menu state
	pullMenuReturnMode ViewMode     // Mode to return to when pull menu closes
	pullModal          *modal.Modal // Modal instance for pull menu
	pullModalWidth     int          // Cached modal width
	pullSelectedIdx    int          // 0=merge, 1=rebase, 2=ff-only, 3=autostash

	// Pull conflict state
	pullConflictFiles []string // Conflicted files from failed pull
	pullConflictType  string   // "merge" or "rebase"
	pullConflictModal *modal.Modal
	pullConflictWidth int

	// View dimensions
	width  int
	height int

	// Watcher
	watcher     *Watcher
	lastRefresh time.Time // Debounce rapid refreshes

	// Commit state
	commitMessage         textarea.Model
	commitError           string
	commitInProgress      bool
	commitAmend           bool // true when amending last commit
	commitButtonFocus     bool // true when button is focused instead of textarea
	commitButtonHover     bool // true when mouse is hovering over button
	commitModal           *modal.Modal
	commitModalWidthCache int

	// Mouse support
	mouseHandler *mouse.Handler

	// Error modal state
	errorModal       *modal.Modal
	errorModalWidth  int
	errorModalHeight int
	errorTitle       string // e.g. "Push Failed", "Fetch Failed"
	errorDetail      string // full git command output

	// Discard confirm state
	discardFile       *FileEntry   // File being confirmed for discard
	discardReturnMode ViewMode     // Mode to return to when modal closes
	discardModal      *modal.Modal // Modal instance for discard confirmation

	// Stash pop confirm state
	stashPopItem  *Stash       // Stash being confirmed for pop
	stashPopModal *modal.Modal // Modal instance for stash pop confirmation

	// Syntax highlighting
	syntaxHighlighter     *SyntaxHighlighter // Cached highlighter for current file
	syntaxHighlighterFile string             // File the highlighter was created for

	// Branch picker state
	branches          []*Branch // List of branches
	branchCursor      int       // Current cursor position
	branchReturnMode  ViewMode  // Mode to return to when modal closes
	branchPickerModal *modal.Modal
	branchPickerWidth int

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

	// Truncation cache to eliminate ANSI parser allocation churn
	truncateCache *ui.TruncateCache
}

func (p *Plugin) clearPullConflictModal() {
	p.pullConflictModal = nil
	p.pullConflictWidth = 0
}

func (p *Plugin) clearErrorModal() {
	p.errorModal = nil
	p.errorModalWidth = 0
	p.errorModalHeight = 0
}

// New creates a new git status plugin.
func New() *Plugin {
	return &Plugin{
		sidebarVisible: true,
		activePane:     PaneSidebar,
		sidebarRestore: PaneSidebar,
		mouseHandler:   mouse.NewHandler(),
		truncateCache:  ui.NewTruncateCache(1000), // Cache up to 1000 truncations
	}
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return pluginID }

// Name returns the plugin display name.
func (p *Plugin) Name() string { return pluginName }

// Icon returns the plugin icon character.
func (p *Plugin) Icon() string { return pluginIcon }

// resolveGitRoot returns the top-level directory of the git repository
// containing dir, or an error if dir is not inside a git repo.
func resolveGitRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	// Resolve git repo root (works from any subdirectory)
	root, err := resolveGitRoot(ctx.WorkDir)
	if err != nil {
		return err // Not a git repo, silently degrade
	}

	// Preserve resources that are expensive to recreate or have no project-specific state
	mouseHandler := p.mouseHandler
	truncateCache := p.truncateCache
	width, height := p.width, p.height

	// Reset ALL state by zeroing the struct, then restore preserved fields
	// Note: Epoch is now handled by plugin.Context, incremented in Registry.Reinit()
	*p = Plugin{
		mouseHandler:   mouseHandler,
		truncateCache:  truncateCache,
		width:          width,
		height:         height,
		sidebarVisible: true,
		activePane:     PaneSidebar,
		sidebarRestore: PaneSidebar,
	}

	// Set up context and repo
	p.ctx = ctx
	p.repoRoot = root
	p.tree = NewFileTree(root)

	// Load user preferences from state
	if state.GetGitDiffMode() == "side-by-side" {
		p.diffViewMode = DiffViewSideBySide
	}
	if saved := state.GetGitStatusSidebarWidth(); saved > 0 {
		p.sidebarWidth = saved
	}
	p.showCommitGraph = state.GetGitGraphEnabled()
	p.diffWrapEnabled = state.GetLineWrapEnabled()

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
		case ViewModePullMenu:
			return p.updatePullMenu(msg)
		case ViewModePullConflict:
			return p.updatePullConflict(msg)
		case ViewModeConfirmDiscard:
			return p.updateConfirmDiscard(msg)
		case ViewModeConfirmStashPop:
			return p.updateConfirmStashPop(msg)
		case ViewModeBranchPicker:
			return p.updateBranchPicker(msg)
		case ViewModeError:
			return p.updateErrorModal(msg)
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
		case ViewModePushMenu:
			return p.handlePushMenuMouse(msg)
		case ViewModePullMenu:
			return p.handlePullMenuMouse(msg)
		case ViewModePullConflict:
			return p.handlePullConflictMouse(msg)
		case ViewModeConfirmDiscard:
			return p.handleDiscardMouse(msg)
		case ViewModeConfirmStashPop:
			return p.handleStashPopMouse(msg)
		case ViewModeError:
			return p.handleErrorModalMouse(msg)
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
		// Auto-load preview for current cursor position after refresh
		if p.viewMode == ViewModeStatus {
			return p, p.autoLoadPreview(true)
		}
		return p, nil

	case DiffLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
		p.diffContent = msg.Content
		p.diffRaw = msg.Raw
		p.diffLoaded = true
		// Always parse diff for built-in rendering (even if delta is available)
		// This allows toggling between delta and built-in rendering at runtime
		p.parsedDiff, _ = ParseUnifiedDiff(msg.Raw)
		return p, nil

	case CommitSuccessMsg:
		// Commit succeeded, return to status view and refresh
		p.viewMode = ViewModeStatus
		p.commitMessage.Reset()
		p.commitInProgress = false
		p.commitAmend = false
		p.commitError = ""
		return p, p.refresh()

	case CommitErrorMsg:
		// Commit failed, show error and keep message for retry
		p.commitError = msg.Err.Error()
		p.commitInProgress = false
		return p, nil

	case InlineDiffLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
		// Only update if this is still the selected file
		if msg.File == p.selectedDiffFile {
			p.diffPaneParsedDiff = msg.Parsed
			// Clamp scroll to new content length (diff may have shrunk after stage/unstage)
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
		}
		return p, nil

	case RecentCommitsLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
		if msg.Commits == nil {
			if msg.PushStatus != nil {
				p.pushStatus = msg.PushStatus
				PopulatePushStatus(p.recentCommits, p.pushStatus)
			}
			return p, nil
		}

		p.moreCommitsAvailable = len(msg.Commits) >= commitHistoryPageSize

		// Determine which commit hash to restore cursor to
		// Priority: pushPreservedCommitHash (set before push) > computed from current state
		prevCommitHash := p.pushPreservedCommitHash
		if prevCommitHash == "" && !p.historyFilterActive && p.cursorOnCommit() {
			commits := p.activeCommits()
			commitIdx := p.selectedCommitIndex()
			if commitIdx >= 0 && commitIdx < len(commits) {
				prevCommitHash = commits[commitIdx].Hash
			}
		}
		// Clear the preserved hash after use
		p.pushPreservedCommitHash = ""

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
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
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
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
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
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
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
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
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
		// Note: pushPreservedCommitHash will be used by RecentCommitsLoadedMsg to restore cursor
		return p, tea.Batch(p.refresh(), p.loadRecentCommits(), p.clearPushSuccessAfterDelay())

	case PushErrorMsg:
		p.pushInProgress = false
		p.pushError = msg.Err.Error()
		p.pushPreservedCommitHash = "" // Clear stale hash on error
		p.showErrorModal("Push Failed", msg.Err)
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
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
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
		p.clearBranchPickerModal()
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case BranchErrorMsg:
		p.showErrorModal("Branch Error", msg.Err)
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
		p.showErrorModal("Fetch Failed", msg.Err)
		return p, nil

	case PullSuccessMsg:
		p.pullInProgress = false
		p.pullSuccess = true
		p.pullError = ""
		return p, tea.Batch(p.refresh(), p.loadRecentCommits(), p.clearPullSuccessAfterDelay())

	case PullErrorMsg:
		p.pullInProgress = false
		if IsConflictError(msg.Err) {
			// Detect conflict type from strategy
			if msg.Strategy == "rebase" || msg.Strategy == "autostash" {
				p.pullConflictType = "rebase"
			} else {
				p.pullConflictType = "merge"
			}
			p.pullConflictFiles = GetConflictedFiles(p.repoRoot)
			if len(p.pullConflictFiles) > 0 {
				p.viewMode = ViewModePullConflict
				p.clearPullConflictModal()
				return p, nil
			}
		}
		p.pullError = msg.Err.Error()
		p.showErrorModal("Pull Failed", msg.Err)
		return p, nil

	case StashErrorMsg:
		p.showErrorModal("Stash Failed", msg.Err)
		return p, nil

	case PullAbortedMsg:
		p.pullConflictFiles = nil
		p.pullConflictType = ""
		p.pullError = ""
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case FetchSuccessClearMsg:
		p.fetchSuccess = false
		return p, nil

	case PullSuccessClearMsg:
		p.pullSuccess = false
		return p, nil

	case StashPopConfirmMsg:
		// Show stash pop confirmation modal
		p.stashPopItem = msg.Stash
		p.stashPopModal = nil // Force rebuild with new stash item
		p.viewMode = ViewModeConfirmStashPop
		return p, nil

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, p.ensureCommitListFilled()
	}

	return p, nil
}

// CommitPreviewLoadedMsg is sent when commit preview is loaded.
type CommitPreviewLoadedMsg struct {
	Epoch  uint64 // Epoch when request was issued (for stale detection)
	Commit *Commit
}

// GetEpoch implements plugin.EpochMessage.
func (m CommitPreviewLoadedMsg) GetEpoch() uint64 { return m.Epoch }

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

// View renders the plugin.
func (p *Plugin) View(width, height int) string {
	// Clear truncation cache if dimensions changed
	if p.width != width || p.height != height {
		clearTruncCache()
	}
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
	case ViewModePullMenu:
		content = p.renderPullMenu()
	case ViewModePullConflict:
		content = p.renderPullConflict()
	case ViewModeConfirmDiscard:
		content = p.renderConfirmDiscard()
	case ViewModeConfirmStashPop:
		content = p.renderConfirmStashPop()
	case ViewModeBranchPicker:
		content = p.renderBranchPicker()
	case ViewModeError:
		content = p.renderErrorModal()
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
		{ID: "amend", Name: "Amend", Description: "Amend last commit", Category: plugin.CategoryGit, Context: "git-status", Priority: 3},
		{ID: "show-diff", Name: "Diff", Description: "View file changes", Category: plugin.CategoryView, Context: "git-status", Priority: 2},
		{ID: "stage-all", Name: "Stage all", Description: "Stage all modified files", Category: plugin.CategoryGit, Context: "git-status", Priority: 2},
		{ID: "unstage-all", Name: "Unstage all", Description: "Unstage all files", Category: plugin.CategoryGit, Context: "git-status", Priority: 2},
		{ID: "push", Name: "Push", Description: "Push commits to remote", Category: plugin.CategoryGit, Context: "git-status", Priority: 2},
		{ID: "open-file", Name: "Open", Description: "Open file in editor", Category: plugin.CategoryActions, Context: "git-status", Priority: 3},
		{ID: "discard-changes", Name: "Discard", Description: "Discard changes to file", Category: plugin.CategoryGit, Context: "git-status", Priority: 3},
		{ID: "branch-picker", Name: "Branch", Description: "Switch branch", Category: plugin.CategoryGit, Context: "git-status", Priority: 3},
		{ID: "fetch", Name: "Fetch", Description: "Fetch from remote", Category: plugin.CategoryGit, Context: "git-status", Priority: 3},
		{ID: "pull", Name: "Pull", Description: "Pull from remote", Category: plugin.CategoryGit, Context: "git-status", Priority: 3},
		{ID: "show-history", Name: "History", Description: "Jump to commit history", Category: plugin.CategoryNavigation, Context: "git-status", Priority: 3},
		{ID: "stash", Name: "Stash", Description: "Stash changes", Category: plugin.CategoryGit, Context: "git-status", Priority: 4},
		{ID: "stash-pop", Name: "Pop", Description: "Pop latest stash", Category: plugin.CategoryGit, Context: "git-status", Priority: 4},
		{ID: "open-in-file-browser", Name: "Browse", Description: "Open file in file browser", Category: plugin.CategoryNavigation, Context: "git-status", Priority: 4},
		{ID: "open-in-github", Name: "GitHub", Description: "Open commit in GitHub", Category: plugin.CategoryActions, Context: "git-status", Priority: 4},
		{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "git-status", Priority: 5},
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
		{ID: "open-in-github", Name: "GitHub", Description: "Open commit in GitHub", Category: plugin.CategoryActions, Context: "git-status-commits", Priority: 3},
		{ID: "toggle-graph", Name: "Graph", Description: "Toggle commit graph display", Category: plugin.CategoryView, Context: "git-status-commits", Priority: 2},
		{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "git-status-commits", Priority: 5},
		// git-commit-preview context (commit preview in right pane)
		{ID: "view-diff", Name: "Diff", Description: "View file diff", Category: plugin.CategoryView, Context: "git-commit-preview", Priority: 1},
		{ID: "back", Name: "Back", Description: "Return to sidebar", Category: plugin.CategoryNavigation, Context: "git-commit-preview", Priority: 1},
		{ID: "yank-commit", Name: "Yank", Description: "Copy commit as markdown", Category: plugin.CategoryActions, Context: "git-commit-preview", Priority: 3},
		{ID: "yank-id", Name: "YankID", Description: "Copy commit ID", Category: plugin.CategoryActions, Context: "git-commit-preview", Priority: 3},
		{ID: "open-in-github", Name: "GitHub", Description: "Open commit in GitHub", Category: plugin.CategoryActions, Context: "git-commit-preview", Priority: 3},
		{ID: "open-in-file-browser", Name: "Browse", Description: "Open file in file browser", Category: plugin.CategoryNavigation, Context: "git-commit-preview", Priority: 3},
		{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "git-commit-preview", Priority: 4},
		// git-status-diff context (inline diff pane)
		{ID: "toggle-diff-view", Name: "View", Description: "Toggle unified/split diff view", Category: plugin.CategoryView, Context: "git-status-diff", Priority: 2},
		{ID: "toggle-wrap", Name: "Wrap", Description: "Toggle line wrapping", Category: plugin.CategoryView, Context: "git-status-diff", Priority: 3},
		{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "git-status-diff", Priority: 3},
		// git-diff context
		{ID: "close-diff", Name: "Close", Description: "Close diff view", Category: plugin.CategoryView, Context: "git-diff", Priority: 1},
		{ID: "scroll", Name: "Scroll", Description: "Scroll diff content", Category: plugin.CategoryNavigation, Context: "git-diff", Priority: 2},
		{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "git-diff", Priority: 2},
		{ID: "toggle-diff-view", Name: "View", Description: "Toggle unified/split diff view", Category: plugin.CategoryView, Context: "git-diff", Priority: 3},
		{ID: "toggle-wrap", Name: "Wrap", Description: "Toggle line wrapping", Category: plugin.CategoryView, Context: "git-diff", Priority: 3},
		{ID: "open-in-file-browser", Name: "Browse", Description: "Open file in file browser", Category: plugin.CategoryNavigation, Context: "git-diff", Priority: 4},
		// git-commit context
		{ID: "execute-commit", Name: "Commit", Description: "Create commit with message", Category: plugin.CategoryGit, Context: "git-commit", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Cancel commit", Category: plugin.CategoryActions, Context: "git-commit", Priority: 1},
		// git-push-menu context
		{ID: "push", Name: "Push", Description: "Push to remote", Category: plugin.CategoryGit, Context: "git-push-menu", Priority: 1},
		{ID: "force-push", Name: "Force", Description: "Force push", Category: plugin.CategoryGit, Context: "git-push-menu", Priority: 1},
		{ID: "push-upstream", Name: "Upstream", Description: "Push & set upstream", Category: plugin.CategoryGit, Context: "git-push-menu", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Cancel", Category: plugin.CategoryNavigation, Context: "git-push-menu", Priority: 2},
		// git-pull-menu context
		{ID: "pull-merge", Name: "Merge", Description: "Pull with merge", Category: plugin.CategoryGit, Context: "git-pull-menu", Priority: 1},
		{ID: "pull-rebase", Name: "Rebase", Description: "Pull with rebase", Category: plugin.CategoryGit, Context: "git-pull-menu", Priority: 1},
		{ID: "pull-ff-only", Name: "FF-only", Description: "Pull fast-forward only", Category: plugin.CategoryGit, Context: "git-pull-menu", Priority: 1},
		{ID: "pull-autostash", Name: "Autostash", Description: "Pull rebase + autostash", Category: plugin.CategoryGit, Context: "git-pull-menu", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Cancel", Category: plugin.CategoryNavigation, Context: "git-pull-menu", Priority: 2},
		// git-pull-conflict context
		{ID: "abort-pull", Name: "Abort", Description: "Abort merge/rebase", Category: plugin.CategoryGit, Context: "git-pull-conflict", Priority: 1},
		{ID: "dismiss", Name: "Dismiss", Description: "Dismiss and resolve manually", Category: plugin.CategoryNavigation, Context: "git-pull-conflict", Priority: 2},
		// git-error context (error modal)
		{ID: "dismiss", Name: "Dismiss", Description: "Dismiss error", Category: plugin.CategoryNavigation, Context: "git-error", Priority: 1},
		{ID: "yank-error", Name: "Yank", Description: "Copy error to clipboard", Category: plugin.CategoryActions, Context: "git-error", Priority: 2},
		// git-stash-pop context (stash pop confirmation modal)
		{ID: "confirm-pop", Name: "Pop", Description: "Confirm stash pop", Category: plugin.CategoryGit, Context: "git-stash-pop", Priority: 1},
		{ID: "dismiss", Name: "Cancel", Description: "Cancel stash pop", Category: plugin.CategoryNavigation, Context: "git-stash-pop", Priority: 2},
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
	case ViewModePullMenu:
		return "git-pull-menu"
	case ViewModePullConflict:
		return "git-pull-conflict"
	case ViewModeError:
		return "git-error"
	case ViewModeConfirmStashPop:
		return "git-stash-pop"
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
		watcher, err := NewWatcher(p.repoRoot)
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

// openFile opens a file in the default editor.
func (p *Plugin) openFile(path string) tea.Cmd {
	return func() tea.Msg {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vim"
		}
		fullPath := filepath.Join(p.repoRoot, path)
		return plugin.OpenFileMsg{Editor: editor, Path: fullPath}
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
	Epoch   uint64 // Epoch when request was issued (for stale detection)
	Content string // Rendered content (may be from delta)
	Raw     string // Raw diff for built-in rendering
}

// GetEpoch implements plugin.EpochMessage.
func (m DiffLoadedMsg) GetEpoch() uint64 { return m.Epoch }

type CommitSuccessMsg struct {
	Hash    string
	Subject string
}
type CommitErrorMsg struct {
	Err error
}

// InlineDiffLoadedMsg is sent when an inline diff finishes loading.
type InlineDiffLoadedMsg struct {
	Epoch  uint64 // Epoch when request was issued (for stale detection)
	File   string
	Raw    string
	Parsed *ParsedDiff
}

// GetEpoch implements plugin.EpochMessage.
func (m InlineDiffLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// RecentCommitsLoadedMsg is sent when recent commits are loaded for sidebar.
type RecentCommitsLoadedMsg struct {
	Epoch      uint64 // Epoch when request was issued (for stale detection)
	Commits    []*Commit
	PushStatus *PushStatus
}

// GetEpoch implements plugin.EpochMessage.
func (m RecentCommitsLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// MoreCommitsLoadedMsg is sent when additional commits are fetched for infinite scroll.
type MoreCommitsLoadedMsg struct {
	Epoch      uint64 // Epoch when request was issued (for stale detection)
	Commits    []*Commit
	PushStatus *PushStatus
}

// GetEpoch implements plugin.EpochMessage.
func (m MoreCommitsLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// FilteredCommitsLoadedMsg is sent when filtered commits are fetched.
type FilteredCommitsLoadedMsg struct {
	Epoch      uint64 // Epoch when request was issued (for stale detection)
	Commits    []*Commit
	PushStatus *PushStatus
}

// GetEpoch implements plugin.EpochMessage.
func (m FilteredCommitsLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// CommitStatsLoadedMsg is sent when commit stats are loaded.
type CommitStatsLoadedMsg struct {
	Epoch uint64 // Epoch when request was issued (for stale detection)
	Hash  string
	Stats CommitStats
}

// GetEpoch implements plugin.EpochMessage.
func (m CommitStatsLoadedMsg) GetEpoch() uint64 { return m.Epoch }

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
	Epoch    uint64 // Epoch when request was issued (for stale detection)
	Branches []*Branch
}

// GetEpoch implements plugin.EpochMessage.
func (m BranchListLoadedMsg) GetEpoch() uint64 { return m.Epoch }

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
	Err      error
	Strategy string // "merge", "rebase", "ff-only", "autostash"
}

// PullAbortedMsg is sent when a conflicted pull is aborted.
type PullAbortedMsg struct{}

// StashErrorMsg is sent when stash operations fail.
type StashErrorMsg struct {
	Err error
}

// FetchSuccessClearMsg is sent to clear the fetch success indicator.
type FetchSuccessClearMsg struct{}

// PullSuccessClearMsg is sent to clear the pull success indicator.
type PullSuccessClearMsg struct{}


// initCommitTextarea initializes the commit message textarea.
func (p *Plugin) initCommitTextarea() {
	p.commitMessage = textarea.New()
	p.commitMessage.SetValue("") // Ensure empty
	p.commitMessage.Placeholder = "Type your commit message..."
	// Make placeholder more visible (default color 240 is too dim)
	p.commitMessage.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(styles.TextSecondary)
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
	p.commitModal = nil
	p.commitModalWidthCache = 0
}

// clearPushSuccessAfterDelay returns a command that clears the push success indicator after 3 seconds.
func (p *Plugin) clearPushSuccessAfterDelay() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return PushSuccessClearMsg{}
	})
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
	workDir := p.repoRoot
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
