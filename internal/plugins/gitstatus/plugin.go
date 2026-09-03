package gitstatus

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/gitinit"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/filebrowser"
	"github.com/marcus/sidecar/internal/queryfield"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	pluginID   = "git-status"
	pluginName = "Git"
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
	ctx                *plugin.Context
	repoRoot           string // Resolved git repo root (may differ from ctx.WorkDir if started in subdirectory)
	hasRepo            bool
	repoInitInProgress bool
	tree               *FileTree
	focused            bool
	cursor             int
	scrollOff          int

	// View mode state machine
	viewMode ViewMode

	// Three-pane layout state
	activePane           FocusPane // Which pane is focused
	sidebarRestore       FocusPane // Tracks pane focused before collapse; restored on expand via toggleSidebar()
	sidebarVisible       bool      // Toggle sidebar with Tab
	paneFocusManaged     bool      // Set once an outer app deck composes Git focus.
	paneFocusActive      bool      // Whether Git's inner active border is visible.
	sidebarWidth         int       // Calculated width (~30%)
	diffPaneWidth        int       // Calculated width (~70%)
	recentCommits        []*Commit // Cached recent commits for sidebar
	commitScrollOff      int       // Scroll offset for commits section in sidebar
	loadingMoreCommits   bool      // Prevents duplicate load-more requests
	moreCommitsAvailable bool      // Whether more commits are available to load
	totalCommitCount     int       // Total commits in repo (from rev-list --count)
	totalCommitCountOK   bool      // True once a successful count has been loaded
	nextCountRequestID   uint64    // Monotonic ID for commit-count loads
	activeCountRequestID uint64    // In-flight count request (0 = idle)
	countRefreshDirty    bool      // Coalesce count reloads while one is in flight

	// Inline diff state (for three-pane view)
	selectedDiffFile     string        // File being previewed in diff pane
	selectedDiffStaged   bool          // Staging side of selectedDiffFile; paths can appear in both groups
	forceNextDiffReload  bool          // Bypass dedup on next autoLoadDiff call
	diffPaneScroll       int           // Vertical scroll for inline diff
	diffPaneHorizScroll  int           // Horizontal scroll for inline diff
	diffPaneParsedDiff   *ParsedDiff   // Parsed diff for inline view
	diffPaneViewMode     DiffViewMode  // Unified, side-by-side, or full-file for inline diff
	diffPaneFullFileDiff *FullFileDiff // Full-file diff for inline view (loaded on demand)
	// diffPaneTruncated marks a patch the source had to cut. A short patch
	// rendered as if it were whole is a lie about the change, so the pane
	// labels it.
	diffPaneTruncated bool

	// Commit preview state (for three-pane view when on commit)
	previewCommit       *Commit // Commit being previewed in right pane
	previewCommitError  string  // Terminal error for the current commit preview request
	previewCommitCursor int     // Cursor for file list in preview
	previewCommitScroll int     // Scroll offset for preview content
	commitBodyExpanded  bool    // Whether full commit message is shown
	commitBodyScroll    int     // Scroll offset within expanded commit body

	// Diff state (for full-screen diff view)
	diffContent         string
	diffFile            string
	diffStaged          bool // Distinguishes staged/unstaged rows with the same path
	diffScroll          int
	diffRaw             string        // Raw diff before delta processing
	diffCommit          string        // Commit hash if viewing commit diff
	diffCommitSubject   string        // Subject of commit being diffed (for breadcrumb)
	diffCommitShortHash string        // Short hash of commit being diffed (for breadcrumb)
	diffViewMode        DiffViewMode  // Unified, side-by-side, or full-file
	diffHorizOff        int           // Horizontal scroll for side-by-side
	parsedDiff          *ParsedDiff   // Parsed diff for enhanced rendering
	diffReturnMode      ViewMode      // View mode to return to on esc
	diffLoaded          bool          // True once diff load completes (distinguishes loading vs empty)
	diffTruncated       bool          // The source cut this patch; the breadcrumb says so
	diffWrapEnabled     bool          // Wrap long lines instead of truncating
	diffBackWidth       int           // Width of back button for hit region (set during render)
	fullFileDiff        *FullFileDiff // Full-file diff for full-screen view (loaded on demand)

	// Push status state
	pushStatus              *PushStatus
	pushInProgress          bool
	pushMenuReturnMode      ViewMode // Mode to return to when push menu closes
	pushMenuFocus           int      // 0=push, 1=force, 2=upstream
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
	watcher      *Watcher
	lastRefresh  time.Time // Debounce rapid refreshes
	watcherError string
	statusError  string
	historyError string

	// repoState names an in-progress operation on the repository being shown
	// (merge, rebase, cherry-pick, revert, bisect). Only a source that reports
	// it fills it; an ordinary working tree leaves it empty.
	repoState string
	// repoRemoteURL is origin's URL for the repository being shown, as the
	// status read answered it. Only a source that returns it fills it; a local
	// project's GitHub link still asks git at the moment it is wanted.
	repoRemoteURL string
	// remoteRefusal is the host's own reason this bound pane has nothing to
	// show, learned from an answer rather than from the connection.
	remoteRefusal string
	// repoSourceOverride lets a test drive the pane from a fixed answer, the
	// way filebrowser's treeSourceOverride does.
	repoSourceOverride RepoSource

	statusLoader           func(string) (*FileTree, error)
	nextStatusRequestID    uint64
	activeStatusRequestID  uint64
	statusRefreshDirty     bool
	historyLoader          func(string, int) ([]*Commit, *PushStatus, error)
	nextHistoryRequestID   uint64
	activeHistoryRequestID uint64
	historyRefreshDirty    bool

	nextPreviewRequestID       uint64
	inlinePreviewRequestID     uint64
	fullScreenPreviewRequestID uint64
	inlineFullFileRequestID    uint64
	fullScreenFileRequestID    uint64
	commitPreviewRequestID     uint64

	// Index write state. Only one write may run at a time; navigation and
	// rendering remain available while its tea.Cmd executes.
	writeExecutor      gitWriteExecutor
	nextOperationID    uint64
	activeOperation    *operationRequest
	operationSelection selectionIdentity
	auxWriteInProgress bool // discard, stash, and branch mutations

	// Commit state
	commitMessage         textarea.Model
	commitError           string
	commitInProgress      bool
	commitAmend           bool // true when amending last commit
	commitButtonFocus     bool // true when button is focused instead of textarea
	commitButtonHover     bool // true when mouse is hovering over button
	commitModal           *modal.Modal
	commitModalWidthCache int
	amendMessageRequestID uint64
	amendMessageLoading   bool

	// Mouse support
	mouseHandler  *mouse.Handler
	hoverDivider  bool
	sidebarScroll sidebarScrollState // interactive scrollbar pointer state

	// Error modal state
	errorModal       *modal.Modal
	errorModalWidth  int
	errorModalHeight int
	errorTitle       string // e.g. "Push Failed", "Fetch Failed"
	errorDetail      string // full git command output
	errorOfferPull   bool   // true when push was rejected due to remote ahead

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

	// History search state (/ in commit section)
	historySearchState *HistorySearchState
	historySearchMode  bool // True when search modal is open

	// History filter state
	historyFilterActive bool   // True when any filter is active
	historyFilterAuthor string // Filter by author name/email
	historyFilterPath   string // Filter by file path
	filteredCommits     []*Commit

	// Path filter input state
	pathFilterMode  bool             // True when path input modal is open
	pathFilterField queryfield.Field // Current path input, as the shared query field

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
	cmd := gitReadOnly("rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (p *Plugin) inNoRepoMode() bool {
	return p.ctx != nil && p.ctx.HostID == "" && !p.hasRepo
}

func (p *Plugin) activateRepo(root string) {
	p.hasRepo = true
	p.repoRoot = root
	p.tree = NewFileTree(root)
}

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	if p.watcher != nil {
		p.watcher.Stop()
	}
	// Preserve resources that are expensive to recreate or have no project-specific state
	mouseHandler := p.mouseHandler
	truncateCache := p.truncateCache
	repoSourceOverride := p.repoSourceOverride
	width, height := p.width, p.height

	// Reset ALL state by zeroing the struct, then restore preserved fields
	// Note: Epoch is now handled by plugin.Context, incremented in Registry.Reinit()
	*p = Plugin{
		mouseHandler:       mouseHandler,
		truncateCache:      truncateCache,
		repoSourceOverride: repoSourceOverride,
		width:              width,
		height:             height,
		sidebarVisible:     true,
		activePane:         PaneSidebar,
		sidebarRestore:     PaneSidebar,
	}

	// Set up context and repo
	p.ctx = ctx

	// Load user preferences from state. These are this viewer's presentation
	// rules and apply to whichever machine owns the project, so they are read
	// before the bound branch returns.
	switch state.GetGitDiffMode() {
	case "side-by-side":
		p.diffViewMode = DiffViewSideBySide
		p.diffPaneViewMode = DiffViewSideBySide
	case "full-file":
		p.diffViewMode = DiffViewFullFile
		p.diffPaneViewMode = DiffViewFullFile
	}
	if saved := state.GetGitStatusSidebarWidth(); saved > 0 {
		p.sidebarWidth = saved
	}
	p.showCommitGraph = state.GetGitGraphEnabled()
	p.diffWrapEnabled = state.GetLineWrapEnabled()

	if p.remoteBound() {
		// repoRoot and hasRepo stay zero: the host's repository has no path on
		// this disk, and leaving them empty means no later call has a directory
		// to run git in. The tree arrives from the first status answer.
		return nil
	}
	p.tree = NewFileTree(ctx.WorkDir)

	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	if p.remoteBound() {
		// A bound pane has no repository to discover: the host owns it, and the
		// only question is whether this Sidecar can read it.
		return p.reload()
	}
	// Repository discovery invokes Git, so it must remain inside the command.
	// Existing repositories are never mutated merely by opening Sidecar.
	return p.detectRepo()
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	if p.watcher != nil {
		p.watcher.Stop()
	}
}

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	if p.remoteBound() {
		return p.updateRemote(msg)
	}
	switch msg := msg.(type) {
	case tea.PasteMsg:
		// A live search or path-filter bar is a text input: a bracketed paste
		// lands in it exactly as typed characters do.
		if handled, cmd := p.handleSearchPaste(msg); handled {
			return p, cmd
		}

	case tea.KeyPressMsg:
		if p.inNoRepoMode() {
			return p.updateNoRepo(msg)
		}
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
		if p.inNoRepoMode() {
			return p.handleNoRepoMouse(msg)
		}
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
		if p.inNoRepoMode() {
			return p, p.detectRepo()
		}
		return p, p.refresh()

	case app.PluginFocusedMsg:
		if p.inNoRepoMode() {
			return p, p.detectRepo()
		}
		// Refresh data when navigating to this plugin
		p.lastRefresh = time.Now()
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case WatchStartedMsg:
		if plugin.IsStale(p.ctx, msg) || msg.RepoRoot != p.repoRoot || p.inNoRepoMode() {
			if msg.Watcher != nil {
				msg.Watcher.Stop()
			}
			return p, nil
		}
		if p.watcher != nil && p.watcher != msg.Watcher {
			p.watcher.Stop()
		}
		p.watcher = msg.Watcher
		p.watcherError = ""
		return p, p.listenForWatchEvents()

	case WatchStartFailedMsg:
		if plugin.IsStale(p.ctx, msg) || msg.RepoRoot != p.repoRoot {
			return p, nil
		}
		slog.Warn("git watcher unavailable", "repo", p.repoRoot, "err", msg.Err)
		p.watcherError = msg.Err.Error()
		return p, func() tea.Msg {
			return app.ToastMsg{Message: "Git watcher unavailable: " + msg.Err.Error(), Duration: 4 * time.Second, IsError: true}
		}

	case WatchEventMsg:
		if plugin.IsStale(p.ctx, msg) || msg.RepoRoot != p.repoRoot || msg.Watcher != p.watcher || p.inNoRepoMode() {
			return p, nil
		}
		// refresh single-flights event bursts and retains one dirty follow-up.
		// Only HEAD/ref-class events invalidate history; index writes do not.
		var history tea.Cmd
		if msg.History {
			history = p.loadRecentCommits()
		}
		return p, tea.Batch(p.refresh(), history, p.listenForWatchEvents())

	case operationResultMsg:
		if plugin.IsStale(p.ctx, msg) || p.activeOperation == nil || p.activeOperation.ID != msg.ID {
			return p, nil
		}
		p.activeOperation = nil
		if msg.Err != nil {
			p.operationSelection = selectionIdentity{}
			return p, remoteFailureAlert(titleCase(string(msg.Kind)), msg.Err)
		}
		return p, p.refresh()

	case DiscardResultMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.auxWriteInProgress = false
		if msg.Err != nil {
			return p, func() tea.Msg {
				return app.ToastMsg{Message: "Discard failed: " + msg.Err.Error(), Duration: 4 * time.Second, IsError: true}
			}
		}
		return p, p.refresh()

	case StatusSnapshotLoadedMsg:
		if p.inNoRepoMode() || plugin.IsStale(p.ctx, msg) || msg.RequestID != p.activeStatusRequestID {
			return p, nil
		}
		return p, p.applyStatusSnapshot(msg)

	case DiffLoadedMsg:
		return p, p.applyDiffLoaded(msg)

	case CommitSuccessMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		// Commit succeeded, return to status view and refresh
		p.viewMode = ViewModeStatus
		p.commitMessage.Reset()
		p.commitInProgress = false
		p.commitAmend = false
		p.commitError = ""
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case AmendMessageLoadedMsg:
		if plugin.IsStale(p.ctx, msg) || msg.RequestID != p.amendMessageRequestID {
			return p, nil
		}
		p.amendMessageLoading = false
		if msg.Err != nil {
			p.commitError = "Load amend message: " + msg.Err.Error()
			return p, nil
		}
		if p.viewMode == ViewModeCommit && p.commitAmend && strings.TrimSpace(p.commitMessage.Value()) == "" {
			p.commitMessage.SetValue(msg.Message)
		}
		return p, nil

	case CommitErrorMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		// Commit failed, show error and keep message for retry
		p.commitError = msg.Err.Error()
		p.commitInProgress = false
		return p, nil

	case InlineDiffLoadedMsg:
		return p, p.applyInlineDiffLoaded(msg)

	case FullFileDiffLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.ForInline && msg.RequestID != p.inlineFullFileRequestID {
			return p, nil
		}
		if !msg.ForInline && msg.RequestID != p.fullScreenFileRequestID {
			return p, nil
		}
		ffd := BuildFullFileDiff(msg.OldContent, msg.NewContent, msg.Parsed)
		if msg.ForInline {
			if msg.File == p.selectedDiffFile && msg.Staged == p.selectedDiffStaged {
				p.diffPaneFullFileDiff = ffd
				// Clamp scroll if new content is shorter
				p.clampDiffPaneScroll()
			}
		} else {
			if msg.File == p.diffFile {
				p.fullFileDiff = ffd
				// Clamp scroll if new content is shorter
				p.clampDiffScroll()
			}
		}
		return p, nil

	case RecentCommitsLoadedMsg:
		return p, p.applyRecentCommits(msg)

	case CommitCountLoadedMsg:
		if plugin.IsStale(p.ctx, msg) || msg.RequestID != p.activeCountRequestID {
			return p, nil
		}
		p.activeCountRequestID = 0
		if msg.OK {
			p.totalCommitCount = msg.Count
			p.totalCommitCountOK = true
		}
		var countFollowUp tea.Cmd
		if p.countRefreshDirty {
			p.countRefreshDirty = false
			countFollowUp = p.loadCommitCount()
		}
		return p, countFollowUp

	case MoreCommitsLoadedMsg:
		return p, p.applyMoreCommits(msg)

	case FilteredCommitsLoadedMsg:
		return p, p.applyFilteredCommits(msg)

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
		return p, p.applyCommitPreview(msg)

	case PushSuccessMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.pushInProgress = false
		// The status view shows the result; the confirmation is a flash.
		// Note: pushPreservedCommitHash will be used by RecentCommitsLoadedMsg to restore cursor
		return p, tea.Batch(p.refresh(), p.loadRecentCommits(), app.ShowFlash("Pushed"))

	case PushErrorMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.pushInProgress = false
		p.pushPreservedCommitHash = "" // Clear stale hash on error
		if isPushRejectedError(msg.Err) {
			p.errorOfferPull = true
		}
		p.showErrorModal("Push Failed", msg.Err)
		return p, tea.Batch(p.loadRecentCommits(), remoteFailureAlert("Push", msg.Err))

	case StashResultMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.auxWriteInProgress = false
		if msg.Err != nil {
			// Show error toast
			toastMsg := "Stash failed: " + msg.Err.Error()
			return p, func() tea.Msg {
				return app.ToastMsg{Message: toastMsg, Duration: 3 * time.Second, IsError: true}
			}
		}
		// Show success toast and refresh
		var toastMsg string
		switch msg.Operation {
		case "push":
			toastMsg = "Stashed changes"
		case "apply":
			toastMsg = "Stash applied"
		default:
			toastMsg = "Stash popped"
		}
		// The status view shows the result; the confirmation is a flash.
		return p, tea.Batch(p.refresh(), app.ShowFlash(toastMsg))

	case BranchListLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil // Ignore stale message from previous project
		}
		p.applyBranchList(msg)
		return p, nil

	case BranchSwitchSuccessMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.auxWriteInProgress = false
		// Branch switched, close picker and refresh
		p.viewMode = p.branchReturnMode
		p.branches = nil
		p.clearBranchPickerModal()
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case BranchErrorMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.auxWriteInProgress = false
		p.showErrorModal("Branch Error", msg.Err)
		return p, nil

	case FetchSuccessMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.fetchInProgress = false
		// Refresh to show updated ahead/behind
		return p, tea.Batch(p.loadRecentCommits(), app.ShowFlash("Fetched"))

	case FetchErrorMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.fetchInProgress = false
		p.showErrorModal("Fetch Failed", msg.Err)
		return p, remoteFailureAlert("Fetch", msg.Err)

	case PullSuccessMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.pullInProgress = false
		return p, tea.Batch(p.refresh(), p.loadRecentCommits(), app.ShowFlash("Pulled"))

	case PullErrorMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
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
		p.showErrorModal("Pull Failed", msg.Err)
		return p, remoteFailureAlert("Pull", msg.Err)

	case StashErrorMsg:
		p.showErrorModal("Stash Failed", msg.Err)
		return p, nil

	case PullAbortedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.pullInProgress = false
		p.pullConflictFiles = nil
		p.pullConflictType = ""
		return p, tea.Batch(p.refresh(), p.loadRecentCommits())

	case gitinit.ReadyMsg:
		if msg.Root == "" {
			return p, nil
		}
		p.repoInitInProgress = false
		if !p.hasRepo || p.repoRoot != msg.Root {
			p.activateRepo(msg.Root)
		}
		return p, tea.Batch(p.refresh(), p.startWatcher(), p.loadRecentCommits())

	case RepoDetectedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.Root == "" {
			return p, nil
		}
		p.activateRepo(msg.Root)
		return p, tea.Batch(p.refresh(), p.startWatcher(), p.loadRecentCommits())

	case RepoInitDoneMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.repoInitInProgress = false
		if msg.Root == "" {
			errMsg := "Failed to initialize repository"
			if msg.Err != nil {
				errMsg = msg.Err.Error()
			}
			return p, func() tea.Msg {
				return app.ToastMsg{Message: errMsg, Duration: 3 * time.Second, IsError: true}
			}
		}
		p.activateRepo(msg.Root)
		ready := func() tea.Msg { return gitinit.ReadyMsg{Root: msg.Root} }
		if msg.Err != nil {
			return p, tea.Batch(
				p.refresh(),
				p.startWatcher(),
				p.loadRecentCommits(),
				ready,
				func() tea.Msg {
					return app.ToastMsg{
						Message:  "Repository initialized; failed to update .gitignore",
						Duration: 3 * time.Second,
						IsError:  true,
					}
				},
			)
		}
		return p, tea.Batch(
			p.refresh(),
			p.startWatcher(),
			p.loadRecentCommits(),
			ready,
			func() tea.Msg {
				return app.ToastMsg{Message: "Initialized git repository on main", Duration: 2 * time.Second}
			},
		)

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
	Epoch     uint64 // Epoch when request was issued (for stale detection)
	RequestID uint64
	Commit    *Commit
	Err       error
}

// GetEpoch implements plugin.EpochMessage.
func (m CommitPreviewLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// CountParsedDiffLines counts total lines in a parsed diff (exported for use by workspace plugin).
func CountParsedDiffLines(diff *ParsedDiff) int {
	return countParsedDiffLines(diff)
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

// View renders the plugin.
func (p *Plugin) View(width, height int) string {
	// Clear truncation cache if dimensions changed
	if p.width != width || p.height != height {
		clearTruncCache()
	}
	p.width = width
	p.height = height

	var content string
	if p.remoteBound() {
		content = p.renderBoundView()
	} else if p.inNoRepoMode() {
		content = p.renderNoRepoView()
	} else {
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
	if p.remoteBound() {
		return p.remoteCommands()
	}
	commands := []plugin.Command{
		// git-no-repo context
		{ID: "init-repo", Name: "Init", Description: "Initialize a git repository on main", Category: plugin.CategoryGit, Context: "git-no-repo", Priority: 1},
		{ID: "refresh", Name: "Retry", Description: "Re-check for a git repository", Category: plugin.CategoryActions, Context: "git-no-repo", Priority: 2},
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
		{ID: "stash-apply", Name: "Apply", Description: "Apply latest stash", Category: plugin.CategoryGit, Context: "git-status", Priority: 4},
		{ID: "open-in-file-browser", Name: "Browse", Description: "Open file in file browser", Category: plugin.CategoryNavigation, Context: "git-status", Priority: 4},
		{ID: "open-in-github", Name: "GitHub", Description: "Open commit in GitHub", Category: plugin.CategoryActions, Context: "git-status", Priority: 4},
		{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "git-status", Priority: 5},
		// git-status-commits context (recent commits in sidebar)
		{ID: "view-commit", Name: "View", Description: "View commit details", Category: plugin.CategoryView, Context: "git-status-commits", Priority: 1},
		{ID: "push", Name: "Push", Description: "Push commits to remote", Category: plugin.CategoryGit, Context: "git-status-commits", Priority: 2},
		{ID: "pull", Name: "Pull", Description: "Pull from remote", Category: plugin.CategoryGit, Context: "git-status-commits", Priority: 2},
		{ID: "fetch", Name: "Fetch", Description: "Fetch from remote", Category: plugin.CategoryGit, Context: "git-status-commits", Priority: 3},
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
		// git-history-search context (commit search modal)
		{ID: "select", Name: "Select", Description: "Jump to selected match", Category: plugin.CategoryActions, Context: "git-history-search", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Close search", Category: plugin.CategoryActions, Context: "git-history-search", Priority: 1},
		{ID: "navigate", Name: "Nav", Description: "Move through matches", Category: plugin.CategoryNavigation, Context: "git-history-search", Priority: 2},
		{ID: "toggle-regex", Name: "Regex", Description: "Toggle regex mode", Category: plugin.CategoryView, Context: "git-history-search", Priority: 3},
		{ID: "toggle-case", Name: "Case", Description: "Toggle case sensitivity", Category: plugin.CategoryView, Context: "git-history-search", Priority: 3},
		// git-path-filter context (path filter modal)
		{ID: "apply-filter", Name: "Apply", Description: "Apply path filter", Category: plugin.CategorySearch, Context: "git-path-filter", Priority: 1},
		{ID: "cancel", Name: "Cancel", Description: "Close path filter", Category: plugin.CategoryActions, Context: "git-path-filter", Priority: 1},
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
		{ID: "reset-hscroll", Name: "Col 0", Description: "Snap horizontal scroll back to column 0", Category: plugin.CategoryNavigation, Context: "git-status-diff", Priority: 4},
		{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "git-status-diff", Priority: 3},
		// git-diff context
		{ID: "close-diff", Name: "Close", Description: "Close diff view", Category: plugin.CategoryView, Context: "git-diff", Priority: 1},
		{ID: "scroll", Name: "Scroll", Description: "Scroll diff content", Category: plugin.CategoryNavigation, Context: "git-diff", Priority: 2},
		{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Category: plugin.CategoryView, Context: "git-diff", Priority: 2},
		{ID: "toggle-diff-view", Name: "View", Description: "Toggle unified/split diff view", Category: plugin.CategoryView, Context: "git-diff", Priority: 3},
		{ID: "toggle-wrap", Name: "Wrap", Description: "Toggle line wrapping", Category: plugin.CategoryView, Context: "git-diff", Priority: 3},
		{ID: "prev-file", Name: "Prev", Description: "Previous changed file", Category: plugin.CategoryNavigation, Context: "git-diff", Priority: 4},
		{ID: "next-file", Name: "Next", Description: "Next changed file", Category: plugin.CategoryNavigation, Context: "git-diff", Priority: 4},
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
		{ID: "pull-from-error", Name: "Pull", Description: "Pull from remote", Category: plugin.CategoryGit, Context: "git-error", Priority: 1},
		{ID: "dismiss", Name: "Dismiss", Description: "Dismiss error", Category: plugin.CategoryNavigation, Context: "git-error", Priority: 1},
		{ID: "yank-error", Name: "Yank", Description: "Copy error to clipboard", Category: plugin.CategoryActions, Context: "git-error", Priority: 2},
		// git-stash-pop context (stash pop confirmation modal)
		{ID: "confirm-pop", Name: "Pop", Description: "Confirm stash pop", Category: plugin.CategoryGit, Context: "git-stash-pop", Priority: 1},
		{ID: "dismiss", Name: "Cancel", Description: "Cancel stash pop", Category: plugin.CategoryNavigation, Context: "git-stash-pop", Priority: 2},
	}
	if !p.writeInProgress() {
		return commands
	}
	available := commands[:0]
	for _, command := range commands {
		if !writeBlockedCommand(command.ID) {
			available = append(available, command)
		}
	}
	return available
}

// FocusContext returns the current focus context.
func (p *Plugin) FocusContext() string {
	if p.remoteBound() {
		// Only the reads this build performs are reachable while bound, so the
		// context names one of those rather than falling through to the modes
		// that own a write.
		switch {
		case p.historySearchMode:
			return "git-history-search"
		case p.pathFilterMode:
			return "git-path-filter"
		case p.viewMode == ViewModeDiff:
			return "git-diff"
		case p.activePane == PaneDiff && p.previewCommit != nil && p.cursorOnCommit():
			return "git-commit-preview"
		case p.activePane == PaneDiff && p.selectedDiffFile != "":
			return "git-status-diff"
		case p.hasSelectedCommit():
			// A real commit row, not the empty-list boundary: a bound pane can
			// be on screen before its first answer arrives, and a footer of
			// commit gestures over no commits is a footer that lies.
			return "git-status-commits"
		}
		return "git-status"
	}
	if p.inNoRepoMode() {
		return "git-no-repo"
	}
	if p.historySearchMode {
		return "git-history-search"
	}
	if p.pathFilterMode {
		return "git-path-filter"
	}

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

// ConsumesTextInput reports whether the plugin is currently in a mode where
// printable keys should be treated as text input.
func (p *Plugin) ConsumesTextInput() bool {
	return p.viewMode == ViewModeCommit || p.historySearchMode || p.pathFilterMode
}

// BlocksGlobalKeys reports whether a plugin-owned overlay has keyboard focus.
func (p *Plugin) BlocksGlobalKeys() bool {
	if p.historySearchMode || p.pathFilterMode {
		return true
	}
	return p.viewMode != ViewModeStatus && p.viewMode != ViewModeDiff
}

// Diagnostics returns plugin health info.
func (p *Plugin) Diagnostics() []plugin.Diagnostic {
	if p.remoteBound() {
		if reason := p.unavailableReason(); reason != "" {
			return []plugin.Diagnostic{
				{ID: "git-status", Status: "warn", Detail: pluginName + " is unavailable: " + reason},
			}
		}
		if p.tree == nil {
			return []plugin.Diagnostic{
				{ID: "git-status", Status: "warn", Detail: "Loading [" + p.ctx.HostID + "]…"},
			}
		}
		status := "ok"
		if p.tree.TotalCount() == 0 {
			status = "clean"
		}
		diagnostics := []plugin.Diagnostic{
			{ID: "git-status", Status: status, Detail: "[" + p.ctx.HostID + "] " + p.tree.Summary()},
		}
		if p.historyError != "" {
			// A log the host would not serve is its own row: the sidebar says
			// "No commits" for an empty history and for a failed read alike,
			// and only one of those is the repository's own answer.
			diagnostics = append(diagnostics, plugin.Diagnostic{ID: "git-history", Status: "warn", Detail: p.historyError})
		}
		return diagnostics
	}
	if p.inNoRepoMode() {
		return []plugin.Diagnostic{
			{ID: "git-status", Status: "warn", Detail: "No git repository"},
		}
	}
	if p.tree == nil {
		return []plugin.Diagnostic{
			{ID: "git-status", Status: "warn", Detail: "No git repository"},
		}
	}
	status := "ok"
	detail := p.tree.Summary()
	if p.tree.TotalCount() == 0 {
		status = "clean"
	}
	diagnostics := []plugin.Diagnostic{
		{ID: "git-status", Status: status, Detail: detail},
	}
	if p.statusError != "" {
		diagnostics = append(diagnostics, plugin.Diagnostic{ID: "git-status-refresh", Status: "warn", Detail: p.statusError})
	}
	if p.historyError != "" {
		diagnostics = append(diagnostics, plugin.Diagnostic{ID: "git-history", Status: "warn", Detail: p.historyError})
	}
	if p.watcherError != "" {
		diagnostics = append(diagnostics, plugin.Diagnostic{ID: "git-watcher", Status: "warn", Detail: p.watcherError})
	}
	return diagnostics
}

// refresh reloads repository status through the seam, from whichever machine
// owns this project.
func (p *Plugin) refresh() tea.Cmd {
	source := p.repoSource()
	if source == nil {
		return nil
	}
	if p.activeStatusRequestID != 0 {
		p.statusRefreshDirty = true
		return nil
	}
	p.nextStatusRequestID++
	requestID := p.nextStatusRequestID
	p.activeStatusRequestID = requestID
	epoch := p.ctx.Epoch
	return func() tea.Msg {
		status, err := source.Status(context.Background())
		return StatusSnapshotLoadedMsg{
			Epoch:     epoch,
			RequestID: requestID,
			Tree:      status.Tree,
			Push:      status.Push,
			State:     status.State,
			RemoteURL: status.RemoteURL,
			Err:       err,
		}
	}
}

// reload is one whole read of the project a refresh means: the working tree,
// and the first page of the log above it.
func (p *Plugin) reload() tea.Cmd {
	return tea.Batch(p.refresh(), p.loadRecentCommits())
}

// applyDiffLoaded lands one full-screen patch. Both message loops end here, so
// a bound pane cannot drift into its own rules about what a loaded patch means.
func (p *Plugin) applyDiffLoaded(msg DiffLoadedMsg) tea.Cmd {
	if plugin.IsStale(p.ctx, msg) || msg.RequestID != p.fullScreenPreviewRequestID {
		return nil // Ignore stale message from previous project
	}
	p.diffContent = msg.Content
	p.diffRaw = msg.Raw
	p.diffLoaded = true
	p.diffTruncated = msg.Truncated
	if msg.Err != nil {
		p.diffContent = ""
		p.diffRaw = ""
		p.parsedDiff = nil
		return func() tea.Msg {
			return app.ToastMsg{Message: "Diff load failed: " + msg.Err.Error(), Duration: 4 * time.Second, IsError: true}
		}
	}
	// Always parse diff for built-in rendering (even if delta is available)
	// This allows toggling between delta and built-in rendering at runtime
	p.parsedDiff, _ = ParseUnifiedDiff(msg.Raw)
	// Auto-load full-file content when in full-file view mode
	if p.diffViewMode == DiffViewFullFile && p.diffFile != "" {
		p.fullFileDiff = nil // Invalidate stale data
		if entry := p.currentWorkingTreeDiffEntry(); entry != nil {
			return p.loadFullFileDiff(entry.Path, entry.Staged, entry.Status, p.diffCommit, false)
		}
		if p.diffCommit != "" {
			return p.loadFullFileDiff(p.diffFile, false, "", p.diffCommit, false)
		}
	}
	return nil
}

// applyInlineDiffLoaded lands one inline patch, for whichever machine answered.
func (p *Plugin) applyInlineDiffLoaded(msg InlineDiffLoadedMsg) tea.Cmd {
	if plugin.IsStale(p.ctx, msg) || msg.RequestID != p.inlinePreviewRequestID {
		return nil // Ignore stale message from previous project
	}
	// Only update if this is still the selected file
	if msg.File != p.selectedDiffFile || msg.Staged != p.selectedDiffStaged {
		return nil
	}
	p.diffPaneParsedDiff = msg.Parsed
	p.diffPaneTruncated = msg.Truncated
	// Clamp scroll to new content length (diff may have shrunk after stage/unstage).
	// In full-file view mode, clamp against the full-file line count (which includes
	// all context lines), not the parsed hunk-only count. Otherwise the watcher
	// refresh cycle snaps scroll back to a much smaller value.
	p.clampDiffPaneScroll()
	// Auto-load full-file content when in full-file view mode.
	// Always reload (not just when nil) so content refreshes after stage/unstage/discard.
	// The old diffPaneFullFileDiff is kept until the new one arrives to avoid flicker.
	if p.diffPaneViewMode == DiffViewFullFile {
		for _, entry := range p.treeEntries() {
			if entry.Path == msg.File && entry.Staged == msg.Staged {
				return p.loadFullFileDiff(entry.Path, entry.Staged, entry.Status, "", true)
			}
		}
	}
	return nil
}

// applyStatusSnapshot lands one status answer. Both the local and the bound
// message loops end here, so a bound pane cannot drift into its own rules about
// what a refresh means.
func (p *Plugin) applyStatusSnapshot(msg StatusSnapshotLoadedMsg) tea.Cmd {
	p.activeStatusRequestID = 0
	if msg.Err == nil && msg.Tree != nil {
		p.tree = msg.Tree
		p.statusError = ""
		p.remoteRefusal = ""
		p.repoState = msg.State
		p.repoRemoteURL = msg.RemoteURL
		// A source that answers the branch row in the read it was already
		// making fills it here; locally it still arrives with the history load.
		if msg.Push != nil {
			p.pushStatus = msg.Push
		}
	}
	var followUp tea.Cmd
	if p.statusRefreshDirty {
		p.statusRefreshDirty = false
		followUp = p.refresh()
	}
	if msg.Err != nil {
		if p.remoteBound() {
			// A bound failure is a state of the pane, not an event: the reason
			// replaces the sidebar and stays until a read succeeds. Toasting it
			// on every refresh would be one alert per snapshot generation.
			p.remoteRefusal = msg.Err.Error()
			p.statusError = msg.Err.Error()
			return followUp
		}
		p.statusError = msg.Err.Error()
		return tea.Batch(followUp, func() tea.Msg {
			return app.ToastMsg{Message: "Git status refresh failed: " + msg.Err.Error(), Duration: 4 * time.Second, IsError: true}
		})
	}
	// Clamp cursor to valid range if files changed
	maxCursor := p.totalSelectableItems() - 1
	if maxCursor < 0 {
		maxCursor = 0
	}
	if p.cursor > maxCursor {
		p.cursor = maxCursor
	}
	if p.remoteBound() {
		// The patch or the commit detail for the row the cursor is on comes
		// through the same seam the status did. The write selection restored
		// below belongs to operations a bound pane does not perform.
		if p.viewMode == ViewModeStatus {
			return tea.Batch(p.autoLoadPreview(true), followUp)
		}
		return followUp
	}
	p.restoreOperationSelection()
	// Auto-load preview for current cursor position after refresh
	if p.viewMode == ViewModeStatus {
		return tea.Batch(p.autoLoadPreview(true), followUp)
	}
	return followUp
}

// applyRecentCommits lands one page of history. Both message loops end here,
// for the same reason the status and patch handlers do: which machine answered
// is decided at the seam and nowhere above it.
func (p *Plugin) applyRecentCommits(msg RecentCommitsLoadedMsg) tea.Cmd {
	if plugin.IsStale(p.ctx, msg) || msg.RequestID != p.activeHistoryRequestID {
		return nil // Ignore stale message from previous project
	}
	p.activeHistoryRequestID = 0
	if msg.Err != nil {
		p.historyError = msg.Err.Error()
	} else {
		p.historyError = ""
	}
	var historyFollowUp tea.Cmd
	if p.historyRefreshDirty {
		p.historyRefreshDirty = false
		historyFollowUp = p.loadRecentCommits()
	}
	if msg.Commits == nil {
		if msg.PushStatus != nil {
			p.pushStatus = msg.PushStatus
			PopulatePushStatus(p.recentCommits, p.pushStatus)
		}
		return historyFollowUp
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
	// A source that answered the branch row in this read owns it. One that did
	// not — a host, which answered it with the status instead and stamped each
	// row's own pushed state — must not blank what is already on screen.
	if msg.PushStatus != nil {
		p.pushStatus = msg.PushStatus
		PopulatePushStatus(p.recentCommits, p.pushStatus)
	}
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
	return tea.Batch(p.ensureCommitListFilled(), historyFollowUp)
}

// applyMoreCommits lands the page a scroll past the end asked for.
func (p *Plugin) applyMoreCommits(msg MoreCommitsLoadedMsg) tea.Cmd {
	if plugin.IsStale(p.ctx, msg) {
		return nil // Ignore stale message from previous project
	}
	p.loadingMoreCommits = false
	if len(msg.Commits) > 0 {
		if len(msg.Commits) < commitHistoryPageSize {
			p.moreCommitsAvailable = false
		}
		p.recentCommits = append(p.recentCommits, msg.Commits...)
		// Recompute entire graph when commits are added
		if p.showCommitGraph {
			commits := p.activeCommits()
			p.commitGraphLines = ComputeGraphForCommits(commits)
		}
		return p.ensureCommitListFilled()
	}
	p.moreCommitsAvailable = false
	return nil
}

// applyFilteredCommits lands a page the source narrowed by author or path.
func (p *Plugin) applyFilteredCommits(msg FilteredCommitsLoadedMsg) tea.Cmd {
	if plugin.IsStale(p.ctx, msg) {
		return nil // Ignore stale message from previous project
	}
	if msg.Commits != nil {
		p.filteredCommits = msg.Commits
		if msg.PushStatus != nil {
			p.pushStatus = msg.PushStatus
		}
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
	return nil
}

// applyCommitPreview lands one commit's detail for the right pane.
func (p *Plugin) applyCommitPreview(msg CommitPreviewLoadedMsg) tea.Cmd {
	if plugin.IsStale(p.ctx, msg) || msg.RequestID != p.commitPreviewRequestID {
		return nil // Ignore stale message from previous project
	}
	if msg.Err != nil {
		p.previewCommit = nil
		p.previewCommitError = msg.Err.Error()
		return func() tea.Msg {
			return app.ToastMsg{Message: "Commit preview failed: " + msg.Err.Error(), Duration: 4 * time.Second, IsError: true}
		}
	}
	// Commit preview loaded for right pane (in status view)
	p.previewCommit = msg.Commit
	p.previewCommitError = ""
	p.previewCommitCursor = 0
	p.previewCommitScroll = 0
	p.commitBodyExpanded = false
	p.commitBodyScroll = 0
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
	return nil
}

// applyBranchList lands the branch picker's list and puts the cursor on the
// current branch.
func (p *Plugin) applyBranchList(msg BranchListLoadedMsg) {
	p.branches = msg.Branches
	for i, b := range p.branches {
		if b.IsCurrent {
			p.branchCursor = i
			break
		}
	}
}

// startWatcher starts the file system watcher.
func (p *Plugin) startWatcher() tea.Cmd {
	if !p.hasRepo || p.repoRoot == "" {
		return nil
	}
	epoch := p.currentEpoch()
	repoRoot := p.repoRoot
	return func() tea.Msg {
		watcher, err := NewWatcher(repoRoot)
		if err != nil {
			return WatchStartFailedMsg{Epoch: epoch, RepoRoot: repoRoot, Err: err}
		}
		return WatchStartedMsg{Epoch: epoch, RepoRoot: repoRoot, Watcher: watcher}
	}
}

// listenForWatchEvents waits for the next file system event.
func (p *Plugin) listenForWatchEvents() tea.Cmd {
	// Capture watcher reference to avoid race with Stop()
	w := p.watcher
	if w == nil {
		return nil
	}
	epoch := p.currentEpoch()
	repoRoot := p.repoRoot
	return func() tea.Msg {
		// When watcher is stopped, Events() channel is closed and this returns
		event, ok := <-w.Events()
		if !ok {
			return nil
		}
		return WatchEventMsg{Epoch: epoch, RepoRoot: repoRoot, Watcher: w, History: event.History}
	}
}

// openFile opens a file in the default editor.
func (p *Plugin) openFile(path string) tea.Cmd {
	return func() tea.Msg {
		// Shared resolution: git status honours VISUAL and the rest of the
		// chain exactly as the other launch sites do.
		editor := tty.ResolveEditor()
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
type StatusSnapshotLoadedMsg struct {
	Epoch     uint64
	RequestID uint64
	Tree      *FileTree
	// Push, State, and RemoteURL are set only by a source that answers them in
	// the same read; see RepoStatus.
	Push      *PushStatus
	State     string
	RemoteURL string
	Err       error
}

func (m StatusSnapshotLoadedMsg) GetEpoch() uint64 { return m.Epoch }

type DiscardResultMsg struct {
	Epoch uint64
	Err   error
}

func (m DiscardResultMsg) GetEpoch() uint64 { return m.Epoch }

type WatchEventMsg struct {
	Epoch    uint64
	RepoRoot string
	Watcher  *Watcher
	History  bool
}

func (m WatchEventMsg) GetEpoch() uint64 { return m.Epoch }

type WatchStartedMsg struct {
	Epoch    uint64
	RepoRoot string
	Watcher  *Watcher
}

func (m WatchStartedMsg) GetEpoch() uint64 { return m.Epoch }

type WatchStartFailedMsg struct {
	Epoch    uint64
	RepoRoot string
	Err      error
}

func (m WatchStartFailedMsg) GetEpoch() uint64 { return m.Epoch }

type ErrorMsg struct{ Err error }
type DiffLoadedMsg struct {
	Epoch     uint64 // Epoch when request was issued (for stale detection)
	RequestID uint64
	Content   string // Rendered content (may be from delta)
	Raw       string // Raw diff for built-in rendering
	Truncated bool   // The source cut this patch; the view must say so
	Err       error
}

// GetEpoch implements plugin.EpochMessage.
func (m DiffLoadedMsg) GetEpoch() uint64 { return m.Epoch }

type CommitSuccessMsg struct {
	Epoch   uint64
	Hash    string
	Subject string
}

type AmendMessageLoadedMsg struct {
	Epoch     uint64
	RequestID uint64
	Message   string
	Err       error
}

func (m AmendMessageLoadedMsg) GetEpoch() uint64 { return m.Epoch }

var _ plugin.EpochMessage = AmendMessageLoadedMsg{}

type CommitErrorMsg struct {
	Epoch uint64
	Err   error
}

func (m CommitSuccessMsg) GetEpoch() uint64 { return m.Epoch }
func (m CommitErrorMsg) GetEpoch() uint64   { return m.Epoch }

// FullFileDiffLoadedMsg is sent when full-file content is loaded for the full-file diff view.
type FullFileDiffLoadedMsg struct {
	Epoch      uint64
	RequestID  uint64
	File       string
	Staged     bool
	OldContent string
	NewContent string
	Parsed     *ParsedDiff
	ForInline  bool // True if this is for the inline diff pane, false for full-screen
}

// GetEpoch implements plugin.EpochMessage.
func (m FullFileDiffLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// InlineDiffLoadedMsg is sent when an inline diff finishes loading.
type InlineDiffLoadedMsg struct {
	Epoch     uint64 // Epoch when request was issued (for stale detection)
	RequestID uint64
	File      string
	Staged    bool
	Raw       string
	Parsed    *ParsedDiff
	Truncated bool // The source cut this patch; the view must say so
}

// GetEpoch implements plugin.EpochMessage.
func (m InlineDiffLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// RecentCommitsLoadedMsg is sent when recent commits are loaded for sidebar.
type RecentCommitsLoadedMsg struct {
	Epoch      uint64 // Epoch when request was issued (for stale detection)
	RequestID  uint64
	Commits    []*Commit
	PushStatus *PushStatus
	Err        error
}

// GetEpoch implements plugin.EpochMessage.
func (m RecentCommitsLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// CommitCountLoadedMsg is sent when total repo commit count is available.
// Loaded independently of the commit page so a slow rev-list never delays
// the infinite-scroll list. RequestID matches history single-flight so an
// older in-flight count cannot overwrite a newer one.
type CommitCountLoadedMsg struct {
	Epoch     uint64
	RequestID uint64
	Count     int
	OK        bool
}

// GetEpoch implements plugin.EpochMessage.
func (m CommitCountLoadedMsg) GetEpoch() uint64 { return m.Epoch }

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
	Epoch  uint64
	Output string
}

// PushErrorMsg is sent when a push fails.
type PushErrorMsg struct {
	Epoch uint64
	Err   error
}

func (m PushSuccessMsg) GetEpoch() uint64 { return m.Epoch }
func (m PushErrorMsg) GetEpoch() uint64   { return m.Epoch }

// PushStatusLoadedMsg is sent when push status is loaded.
type PushStatusLoadedMsg struct {
	Status *PushStatus
}

// StashResultMsg is sent when a stash operation completes.
type StashResultMsg struct {
	Epoch     uint64
	Operation string // "push", "pop", or "apply"
	Ref       string // stash ref for display (e.g. "stash@{0}")
	Err       error
}

func (m StashResultMsg) GetEpoch() uint64 { return m.Epoch }

// BranchListLoadedMsg is sent when branch list is loaded.
type BranchListLoadedMsg struct {
	Epoch    uint64 // Epoch when request was issued (for stale detection)
	Branches []*Branch
}

// GetEpoch implements plugin.EpochMessage.
func (m BranchListLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// BranchSwitchSuccessMsg is sent when branch switch succeeds.
type BranchSwitchSuccessMsg struct {
	Epoch  uint64
	Branch string
}

// BranchErrorMsg is sent when a branch operation fails.
type BranchErrorMsg struct {
	Epoch uint64
	Err   error
}

func (m BranchSwitchSuccessMsg) GetEpoch() uint64 { return m.Epoch }
func (m BranchErrorMsg) GetEpoch() uint64         { return m.Epoch }

// FetchSuccessMsg is sent when fetch succeeds.
type FetchSuccessMsg struct {
	Epoch  uint64
	Output string
}

// FetchErrorMsg is sent when fetch fails.
type FetchErrorMsg struct {
	Epoch uint64
	Err   error
}

func (m FetchSuccessMsg) GetEpoch() uint64 { return m.Epoch }
func (m FetchErrorMsg) GetEpoch() uint64   { return m.Epoch }

// PullSuccessMsg is sent when pull succeeds.
type PullSuccessMsg struct {
	Epoch  uint64
	Output string
}

// PullErrorMsg is sent when pull fails.
type PullErrorMsg struct {
	Epoch    uint64
	Err      error
	Strategy string // "merge", "rebase", "ff-only", "autostash"
}

func (m PullSuccessMsg) GetEpoch() uint64 { return m.Epoch }
func (m PullErrorMsg) GetEpoch() uint64   { return m.Epoch }

// PullAbortedMsg is sent when a conflicted pull is aborted.
type PullAbortedMsg struct{ Epoch uint64 }

func (m PullAbortedMsg) GetEpoch() uint64 { return m.Epoch }

// StashErrorMsg is sent when stash operations fail.
type StashErrorMsg struct {
	Err error
}

// initCommitTextarea initializes the commit message textarea.
func (p *Plugin) initCommitTextarea() {
	p.commitMessage = textarea.New()
	p.commitMessage.SetValue("") // Ensure empty
	p.commitMessage.Placeholder = "Type your commit message..."
	// Make placeholder more visible (default color 240 is too dim).
	// v2: styling moved under .Styles, accessed via Styles()/SetStyles().
	cmStyles := p.commitMessage.Styles()
	cmStyles.Focused.Placeholder = lipgloss.NewStyle().Foreground(styles.TextSecondary)
	p.commitMessage.SetStyles(cmStyles)
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

// confirmStashPop fetches the latest stash and shows the confirm modal. The
// list is read through the seam like every other repository read; popping it is
// a write and belongs to whichever machine owns the repository.
func (p *Plugin) confirmStashPop() tea.Cmd {
	fetch := p.fetchRefs()
	return func() tea.Msg {
		refs, err := fetch()
		if err != nil || len(refs.Stashes) == 0 {
			return StashErrorMsg{Err: fmt.Errorf("no stashes available")}
		}
		return StashPopConfirmMsg{Stash: refs.Stashes[0]}
	}
}

// StashPopConfirmMsg is sent when the stash pop confirm modal should be shown.
type StashPopConfirmMsg struct {
	Stash *Stash
}

// updateConfirmStashPop handles key events in the confirm stash pop modal.

// remoteFailureAlert files a git failure as a session-source error
// notification: a short title, with the first meaningful line of git's own
// output as the body. The full text still belongs to the error modal — the
// notification is the record that it happened, not a second dump of it.
func remoteFailureAlert(action string, err error) tea.Cmd {
	return func() tea.Msg {
		post := notify.Alert(notify.SourceSession, notify.SeverityError, action+" failed")
		post.Notification.Body = firstMeaningfulLine(err.Error())
		return post
	}
}

// firstMeaningfulLine picks the line of a git error that actually says what
// went wrong, preferring git's own diagnostic markers over the transport
// preamble ("To ../remote.git"), and falling back to the first non-blank line.
func firstMeaningfulLine(s string) string {
	var first string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if first == "" {
			first = line
		}
		if strings.HasPrefix(line, "! ") || strings.HasPrefix(line, "error:") || strings.HasPrefix(line, "fatal:") {
			return line
		}
	}
	if first != "" {
		return first
	}
	return strings.TrimSpace(s)
}
