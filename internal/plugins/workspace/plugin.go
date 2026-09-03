package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/features"
	boardkanban "github.com/marcus/sidecar/internal/kanban"
	"github.com/marcus/sidecar/internal/livepanes"
	"github.com/marcus/sidecar/internal/livewatch"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/panereposition"
	"github.com/marcus/sidecar/internal/panesearch"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/shellliveness"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/tabs"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/termpanes"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/tmuxserver"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspacediff"
	"github.com/marcus/sidecar/internal/workspaceinventory"
	"github.com/marcus/sidecar/internal/workspacelist"
	"github.com/marcus/sidecar/internal/worktreedelete"
)

const (
	pluginID   = "workspace-manager"
	pluginName = "Workspaces"
	pluginIcon = "W"

	// Output buffer capacity (lines). The live capture remains small; older
	// ranges are fetched lazily as the user reaches the loaded boundary. Both
	// numbers are the shared layer's, so this surface reaches exactly as far
	// back as the global browser does.
	outputBufferCap  = tty.HistoryBufferLines
	historyLoadChunk = tty.HistoryChunkLines

	// Pane layout constants
	dividerWidth    = 1 // Visual divider width
	dividerHitWidth = 3 // Wider hit target for drag

	// Flash effect duration for invalid key interaction
	flashDuration = 1500 * time.Millisecond

	// Hit region IDs
	regionSidebar         = "sidebar"
	regionPreviewPane     = "preview-pane"
	regionPaneDivider     = "pane-divider"
	regionWorktreeItem    = "workspace-item"
	regionPreviewAction   = "preview-action"
	regionListFilter      = "workspace-list-filter"
	regionListFilterClear = "workspace-list-filter-clear"
	// Agent choice modal IDs (modal library)
	agentChoiceListID    = "agent-choice-list"
	agentChoiceConfirmID = "agent-choice-confirm"
	agentChoiceCancelID  = "agent-choice-cancel"
	agentChoiceActionID  = "agent-choice-action"

	// Kanban view regions
	regionKanbanCard   = "kanban-card"
	regionKanbanColumn = "kanban-column"
	// regionKanbanScrollbar names a drag that began on a lane's scrollbar.
	// The board arms the gesture in PressScrollbar (which also does any
	// track-click jump); this ID is what turns the host's StartDrag into
	// motions routed to DragScrollbar and a release anywhere that settles it.
	regionKanbanScrollbar = "kanban-scrollbar"
	regionViewToggle      = "view-toggle"

	// Task Link modal regions
	regionTaskLinkDropdown = "task-link-dropdown"

	// Merge modal element IDs
	mergeMethodListID      = "merge-method-list"
	mergeMethodActionID    = "merge-method-action"
	mergeWaitingDeleteID   = "merge-waiting-delete"
	mergeWaitingKeepID     = "merge-waiting-keep"
	mergeConfirmWorktreeID = "merge-confirm-worktree"
	mergeConfirmBranchID   = "merge-confirm-branch"
	mergeConfirmRemoteID   = "merge-confirm-remote"
	mergeConfirmPullID     = "merge-confirm-pull"
	mergeTargetListID      = "merge-target-list"
	mergeTargetActionID    = "merge-target-action"
	mergeCleanUpButtonID   = "merge-cleanup-btn"
	mergeSkipButtonID      = "merge-skip-btn"
	mergePRURLID           = "merge-pr-url"
	mergeFallbackDraftID   = "merge-fallback-draft"
	mergeAgentDraftID      = "merge-agent-draft"
	mergeCreatePRID        = "merge-create-pr"
	mergeStopWatchingID    = "merge-stop-watching"
	mergeForceBranchID     = "merge-force-branch"

	// Sidebar header regions
	regionCreateWorktreeButton = "create-worktree-button"
	regionShellsPlusButton     = "shells-plus-button"
	regionWorkspacesPlusButton = "workspaces-plus-button"
	regionListSortButton       = "list-sort-button"
	regionStartAgentButton     = "start-agent-button"
	regionOpenCreateButton     = "open-create-button"

	// Diff tab pane divider (for drag-to-resize file list vs diff viewer)
	regionDiffTabDivider = "diff-tab-divider"

	// Diff tab mouse regions
	regionDiffTabFile         = "diff-tab-file"          // File in left pane file list
	regionDiffTabCommit       = "diff-tab-commit"        // Commit in left pane
	regionDiffTabDiffPane     = "diff-tab-diff-pane"     // Right pane diff content area
	regionDiffTabMinimap      = "diff-tab-minimap"       // Minimap in full-file view
	regionCommitFileItem      = "commit-file-item"       // File in commit drill-down list
	regionCommitFileBack      = "commit-file-back"       // Back button in commit drill-down
	regionCommitFileDiffPane  = "commit-file-diff-pane"  // Right pane for commit file diff
	regionDiffTabPreviewFile  = "diff-tab-preview-file"  // File in commit preview (right pane)
	regionDiffTabFileListPane = "diff-tab-filelist-pane" // Left pane catch-all (for click-to-focus)

	// Terminal panel divider (for drag-to-resize output vs terminal panel)
	regionTermPanelContent = "term-panel-content"
	// A terminal surface's scrollbar drag sources, on the same terms as the
	// note pane's: the sidebar list starts its own bar drags under the shared
	// renderer's thumb/track strings in this same hit map, so a drag source
	// here must be unambiguous. The payload names the surface.
	regionTermScrollbarThumb = "term-scrollbar-thumb"
	regionTermScrollbarTrack = "term-scrollbar-track"
	// regionPaneLeaf is any content leaf's body — document or issue. One region
	// for both: the leaf ID it carries is what a click needs, and the tree says
	// what kind of leaf that is, so the arms ask the tree instead of the name.
	regionPaneLeaf = "pane-leaf"
	// regionIssueScrollbar names a drag that began on an issue card's
	// scrollbar. The card arms the gesture in HandleClick (which also does any
	// track-click jump); this ID is what turns the host's StartDrag into
	// motions routed to ScrollbarDrag and a release that settles it.
	regionIssueScrollbar = "issue-scrollbar"
	// A note pane's scrollbar drag sources, on the same terms. They
	// deliberately do not reuse the shared renderer's thumb/track IDs: the
	// sidebar list starts its own bar drags under those exact strings in this
	// same hit map, so a drag source here must be unambiguous.
	regionNoteScrollbarThumb = "note-scrollbar-thumb"
	regionNoteScrollbarTrack = "note-scrollbar-track"
	regionDocLink            = "doc-link"
	regionDocTab             = "doc-tab"
	regionIssueTab           = "issue-tab"
	regionNoteTab            = "note-tab"
	regionResourceTab        = "resource-tab"
	regionDiffTargetTab      = "diff-target-tab"
	regionPaneClose          = "pane-close"
	regionPaneLayout         = "pane-layout"
	// regionPaneTitle is a leaf's header name, which is a click target so a
	// pane with no sidebar row of its own can still be renamed.
	regionPaneTitle       = "pane-title"
	regionPaneTreeDivider = "pane-tree-divider"

	// Shell delete confirmation modal regions
)

// Plugin implements the worktree manager plugin.
type Plugin struct {
	// Required by plugin.Plugin interface
	ctx     *plugin.Context
	focused bool
	// selectionSince timestamps the current selection so acknowledgement can
	// require dwell. Arrowing past a shell is not reading it.
	selectionSince time.Time
	width          int
	height         int
	terminalPanes  *termpanes.Deck

	// Shared terminal components for the selected primary pane and its optional
	// per-worktree/project terminal panel. Workspaces owns target/layout policy;
	// tty.Model owns transport, model presentation, fallback, input, and delivery.
	applicationFocused    bool
	terminalLinks         termpreview.LinkCoordinator
	linkMatcherGeneration uint64

	// Worktree state
	worktrees                  []*Worktree
	agents                     map[string]*Agent
	repoSnapshot               *RepoSnapshot
	operationCtx               context.Context
	operationCancel            context.CancelFunc
	operationSeq               uint64
	refreshOperationID         string
	activeLifecycleOperationID string
	pendingOverviewSelection   *plugin.PendingWorkspaceSelection
	pendingOverviewAction      tea.Cmd

	// Session tracking for safe cleanup
	managedSessions map[string]bool

	// View state
	viewMode     ViewMode
	activePane   FocusPane
	selectedIdx  int
	scrollOffset int // Sidebar list scroll offset
	visibleCount int // Number of visible list items
	// freeScroll latches that the sidebar viewport is where a scrollbar gesture
	// put it rather than where the selection is; any selection move clears it.
	freeScroll bool
	// sidebarBar carries the shared list's bar between the render pass that
	// drew it and the pointer events that answer it.
	sidebarBar sidebarBarState
	// previewOffset is the document tabs' scroll position: an absolute line
	// from the top of the rendered content. The terminal surfaces do not use
	// it — each live terminal leaf keeps its own distance from the live bottom.
	previewOffset  int
	sidebarWidth   int  // Persisted sidebar width
	sidebarVisible bool // Whether sidebar is visible (toggled with \)

	// listFilter is the shared `/` filter over the sidebar list. The component
	// is internal/workspacelist, the same one the global Workspaces browser
	// uses, so both lists agree on matching, counts, and escape behaviour.
	listFilter workspacelist.Filter
	// listSort orders the sidebar. Manual is the default and means the fixed
	// Shells/Worktrees structure with shells nested under their worktree; every
	// other mode is a computed order over one flat list. See sortedNavSections.
	listSort workspacelist.Sort
	// viewFlyout is the sidebar's View surface, open only while non-nil.
	viewFlyout        *modal.Modal
	viewFlyoutMouse   *mouse.Handler
	viewFlyoutWidth   int
	viewFlyoutSortIdx int
	flashPreviewTime  time.Time // When preview flash was triggered
	toastMessage      string    // Temporary toast message to display
	toastTime         time.Time // When toast was triggered

	// Preview pane tree state. A nil root retains the legacy path while the
	// feature is disabled. Phase 1 intentionally creates only one terminal leaf;
	// document registry and load-request state arrive with the open-doc journey.
	paneRoot    *PaneNode
	contentDeck *contentpanes.Deck
	// contentSource, when set, is the Document adapter for bound remote
	// workspaces. Tests inject a fake; production builds RemoteSource from
	// ctx.RemoteRunner and ctx.HostVerbs.
	contentSource   contentpanes.Source
	paneFocus       int
	paneNextID      int
	paneDragSplitID int
	paneLayoutModal *panereposition.Controller
	paneZoom        panereposition.Zoom
	paneRestoreCmd  tea.Cmd
	// paneLayoutSurface is the surface the live tree currently represents.
	// Empty until a restore or save binds it, so an init terminal is not
	// written onto the default selection.
	paneLayoutSurface string
	// hiddenPaneLayout is the last encoded split+tabs for the current surface
	// when the pane is hidden (q). It can be a document set, an issue set, or
	// both. Last-x forgets it. Restoring an Open surface clears it.
	hiddenPaneLayout *state.PaneLayoutJSON
	// paneSizeCmds holds what a leaf's SetSize answered during a render until
	// the next update can dispatch it. A render has no runtime to hand a command
	// to, and dropping one would swallow the exact signal the content contract
	// documents as how a content asserts geometry it owns beyond this process.
	paneSizeCmds []tea.Cmd
	// paneFrame is the tree exactly as the last frame PLACED it, and paneFrameDrawn
	// says a frame placed one at all. Both are cleared with the hit regions at the
	// top of View and re-earned only where the tree is actually composed, so pointer
	// geometry and click targets can never describe different frames — and a view
	// that draws no tree (the kanban board, a zoomed terminal, a preview too small
	// to place) answers no leaf boxes rather than last frame's.
	paneFrame      PaneLayout
	paneFrameDrawn bool
	// projectPreview caches only the expensive composed pane-tree bytes. Layout
	// and mutable pointer regions are still rebuilt on every frame.
	projectPreview projectPreviewCache
	// projectPreviewRevision advances for every application update except the
	// sidebar-only activity animation tick. Leaf-owned output/document
	// revisions remain separate so producer updates are visible even in tests
	// and direct callers that mutate them without routing a Bubble Tea message.
	projectPreviewRevision uint64
	docs                   map[int]*docPane
	// docLinkHits are the content-link targets the last composed document
	// bodies earned. They are registered in the frame's Body slot so tabs and
	// close buttons keep the header row.
	docLinkHits       []docContentLinkHit
	docLinkResolution *contentlink.ResolutionIndex
	// docSelectLeaf is the document leaf a live text-selection drag started in.
	// A drag is answered by where it began, never by where the pointer has since
	// travelled, and the shared pane-leaf region cannot say which leaf that was.
	docSelectLeaf int
	// issueScrollLeaf and issueScrollTrackY carry an issue card's live
	// scrollbar gesture: which leaf it started in and the absolute Y the card's
	// row 0 sat at when the button went down, so motion maps onto view-local
	// rows without re-deriving pane geometry mid-gesture. See mouse.go.
	issueScrollLeaf   int
	issueScrollTrackY int
	// noteBar carries a note pane's live scrollbar gesture. noteview exposes a
	// state-free seam, so the host owns the bookkeeping: the press-time params
	// snapshot keeps a mid-gesture re-render — a live refresh, a resize — from
	// shifting the mapping under the pointer. See mouse.go.
	noteBar noteBarGesture
	issues  map[int]*issuePane
	notes   map[int]*notePane
	diffs   map[int]*diffPane
	// resources are the external-provider leaves. One map for every provider:
	// the extension point is which resource is recognized, not which windows
	// exist, so a Jira ticket and a CI build are tabs in one kind of leaf.
	resources map[int]*resourcePane
	// resourceMatchers is the live matcher snapshot the scanner may run,
	// injected by the host. Empty — the default — means no provider is ready,
	// which must read as ordinary terminal text.
	resourceMatchers []terminallink.ResourceMatcher
	// resolveResource turns a reference into a document. The manager, process,
	// timeout and cancellation are all the host's; this plugin only decides
	// when to ask.
	resolveResource resourceview.Resolver
	// pluginCalls is how a collection or row tab reaches its protocol plugin.
	// It is the collection shape's counterpart to resolveResource and arrives
	// from the same describe pass.
	pluginCalls resourceview.CallsFor
	// pluginWatchTargets caches the expanded, validated watch targets of the
	// plugins behind the visible collection tabs, keyed by the describe
	// generation they came from. Validation is a stat per path, which is why it
	// happens once per generation in Prepare rather than on every reconcile.
	pluginWatchTargets map[string][]livewatch.Target
	// pluginPollTick is the newest poll ticker sequence, so a tick from a pane
	// that has closed or a plugin whose interval changed is discarded rather
	// than left driving a list nobody is looking at.
	pluginPollTick uint64
	// pluginPollArmed is whether a poll tick is already in flight, so a
	// reconcile per message does not arm one ticker per message.
	pluginPollArmed bool
	// resourceRefreshDebt records a watched change that arrived while a modal
	// covered the collection panes, so the reconcile drives it once the veto
	// lifts rather than dropping it. See livepanes.Binding.Owed.
	resourceRefreshDebt bool

	// Live refresh: one filesystem watcher per content-pane kind, created the
	// first time a pane of that kind is on screen and released in Stop. The
	// lifecycle is livepanes'; what this plugin owns is which panes are visible
	// and how each kind re-reads itself. See live_panes.go.
	live *livepanes.Set
	// diffAdminTargets caches git's administrative paths per worktree, because
	// resolving them costs five `git rev-parse` calls and they never move for
	// the life of a worktree.
	diffAdminTargets   map[string][]livewatch.Target
	diffAdminResolving map[string]bool
	// tdStoreTargets caches where td keeps its store per worktree. Resolving it
	// walks parents and can shell out to git, so it must not happen inline.
	tdStoreTargets   map[string][]livewatch.Target
	tdStoreResolving map[string]bool
	// issueModelNextID allocates a unique load identity per issue tab so a
	// late result cannot land on whichever tab is now active.
	issueModelNextID int
	noteModelNextID  int
	// docInfo is the file-info modal over a workspace document tab.
	docInfo *docview.Info
	// docFinderCaches holds one file list per pane root, so the file finder
	// walks a directory tree once for every pane rooted there rather than once
	// per ctrl+p. Dropped on Init: a project switch invalidates every root.
	docFinderCaches panesearch.Caches

	// One shared, demand-driven frame clock animates semantic agent activity.
	// Ordinary running shells never enter this clock.
	activityAnimationFrame      int
	activityAnimationScheduled  bool
	activityAnimationGeneration uint64

	// Interactive selection state (preview pane)
	selection                     ui.SelectionState
	selectionPanel                bool
	pointer                       tty.Pointer // click/drag state machine over the terminal
	interactiveCopyPasteHintShown bool
	terminalHistory               map[string]tty.HistoryReach
	paneGeometry                  map[string]paneGeometry
	paneMouseReports              map[string]bool
	terminalSearch                terminalSearchState

	// Kanban view state
	kanbanCol int // Current column index (0=Shells, 1=Active, 2=Thinking, 3=Waiting, 4=Done, 5=Paused)
	kanbanRow int // Current row within the column
	kanban    boardkanban.Component

	// Agent state
	attachedSession     string // Name of worktree we're attached to (pauses polling)
	tmuxCaptureMaxBytes int    // Cap for tmux capture output (bytes)
	// backgrounds and backgroundSpanMax are plugins.workspace.
	// terminalBackgrounds / terminalBackgroundSpanMax, resolved through
	// app.TerminalConfig's rules so every terminal surface answers alike.
	backgrounds       tty.BackgroundMode
	backgroundSpanMax int
	// terminalDefaultBackground is reported by the terminal hosting Sidecar.
	// It survives project reinitialization because the host did not change.
	terminalDefaultBackground string
	// resizeDebounceDur is plugins.workspace.resizeDebounceMs. A nil pointer
	// means tty.DefaultResizeDebounce; a set 0 is the per-event escape hatch.
	resizeDebounceDur *time.Duration
	// resizeGeneration invalidates leftover deferredPaneResizeMsg ticks after
	// a divider drop so they cannot fire a second SIGWINCH.
	resizeGeneration uint64
	// resizeFlushImmediate is set only while constructing divider-drop resize
	// cmds so owned tty.Models flush via ResizeAndPollImmediate.
	resizeFlushImmediate bool

	// Timer leak prevention (td-83dc22): generation counters to invalidate stale timers.
	// When a timer fires, it checks if its captured generation matches the current one.
	// If not, the timer is stale (worktree/shell was removed) and the msg is ignored.
	pollScheduler tty.KeyedScheduler // Namespaced agent/shell/panel poll generations
	// terminalOwnership is the visibility epoch for this project projection.
	// It is deliberately separate from applicationFocused: app blur changes input
	// policy, while losing project-surface visibility revokes every terminal side
	// effect already queued for the old owner.
	terminalOwnership           *terminalOwnershipLease
	sessionValidationGeneration uint64
	sessionValidationScheduled  bool

	// Truncation cache to eliminate ANSI parser allocation churn
	truncateCache *ui.TruncateCache

	// Mouse support
	mouseHandler *mouse.Handler

	// A held terminal wheel event changes only the shared burst bookkeeping.
	// Reuse the last same-dimension frame exactly once for that event; every
	// other message still takes the ordinary render path.
	reuseHeldWheelViewOnce bool
	wheelViewCache         string
	wheelViewCacheW        int
	wheelViewCacheH        int
	wheelViewCacheOK       bool

	// Async state
	refreshing  bool
	lastRefresh time.Time

	// Diff state lives on the shared viewer. Hosts keep HitMap + drag only.
	diff         workspacediff.View
	fullFileDiff *gitstatus.FullFileDiff // host-owned; workspacediff cannot import gitstatus
	// fullFileKey is the file the painted full-file body belongs to. The slot
	// is shared by every Diff view, so without it a second view paints the
	// first one's file and never issues a load of its own.
	fullFileKey string

	// previewActionPlacements is where this frame's header put the Diff/Task
	// action chips. They are right-aligned against the hints, so their columns
	// are only knowable from the render that drew them.
	previewActionPlacements []headerChipPlacement

	// lastDragRegion is the region ID of the last drag (EndDrag clears the handler before DragEnd).
	lastDragRegion string

	// Terminal panel state (Ctrl+T toggle). The panel is a Shell leaf of the
	// pane tree: where it sits and how big it is are the tree's, and
	shellSplitAxis  SplitAxis // Axis the next shell split opens at
	shellSplitRatio int       // Primary terminal's share of that split
	// shellSplitPlacement is the `--split` placement the create modal asked
	// for, consumed by the open that follows it. Empty means ctrl+t's
	// remembered shape.
	shellSplitPlacement string
	// shellLeafName is the name the create modal gave the shell leaf, shown in
	// its header chip.
	shellLeafName string
	// pendingTermPanelSeed is a --run/--type to send after the split tmux
	// session exists. The create ack does not wait for that session.
	pendingTermPanelSeed *termPanelSeed
	// shellLeafSurface is the workspace the open split terminal belongs to. A
	// split is a peer in one workspace, not a plugin-wide preference, so the
	// selection landing anywhere else releases it — see
	// releaseShellLeafOffSurface.
	shellLeafSurface string
	// restoredShellSession is the durable session selector a restored shell
	// leaf carried, used once instead of re-deriving the name.
	restoredShellSession string
	// legacyTermPanel is one user's pre-split panel preference, held only until
	// the first persisted layout it can be spliced into.
	legacyTermPanel       termPanelPrefs
	legacyTermPanelTaken  bool
	terminalDocProjection terminalDocProjection
	// termBar is a live pointer gesture on one of this plugin's two terminal
	// scrollbars, armed by a press on that surface's bar regions and settled
	// by release or lost-release. See terminal_scrollbar.go.
	termBar         termBarGesture
	hoverTermBar    terminalScrollbarHit
	hoverTermBarSet bool

	// File picker modal state (gf command)
	filePickerIdx int // Selected file index in picker

	commitStatusWorktree string // Name of worktree for cached status

	// Conflict detection state
	conflicts []Conflict

	// Create modal state. The chooser lives in workspacecreate.Form;
	// confirm/recovery still use createOperationModal.
	createForm              *workspacecreate.Form
	createTargetWorktree    *Worktree // KindShell submit starts an agent here instead of a new shell
	createError             string    // Operation errors shown on the form
	createOperationModal    *modal.Modal
	createOperationWidth    int
	createPlan              *CreateOperationPlan
	createSetupResult       *CreateSetupResult
	createDeleteResult      *CreateRecoveryDeleteResult
	createBusyStep          string
	createCopyEnv           bool
	createRunHook           bool
	deferredCreations       []CreateWorktreeAddedMsg         // stale cross-project results retained until matching repo returns
	removePendingCreationFn func(*CreateOperationPlan) error // test seam for durable journal completion failures

	// Task search state for linking an existing worktree
	taskSearchInput    textinput.Model
	taskSearchAll      []Task // All available tasks
	taskSearchFiltered []Task // Filtered based on query
	taskSearchIdx      int    // Selected index in dropdown
	taskSearchScroll   int    // First rendered task row
	taskSearchLoading  bool

	// Branch autocomplete state for create modal
	branchAll      []string // All available branches
	branchFiltered []string // Filtered based on query
	branchIdx      int      // Selected index in dropdown
	branchScroll   int      // First rendered branch row

	// Task link modal state (for linking to existing worktrees)
	linkingWorktree    *Worktree
	taskLinkModal      *modal.Modal
	taskLinkModalWidth int

	// Markdown renderer shared with issue panes.
	markdownRenderer *markdown.Renderer

	// Merge workflow state
	mergeState      *MergeWorkflowState
	mergeModal      *modal.Modal      // Modal instance for merge workflow
	mergeModalWidth int               // Cached width for rebuild detection
	mergeModalStep  MergeWorkflowStep // Cached step for rebuild detection

	// Commit-before-merge state
	mergeCommitState         *MergeCommitState
	mergeCommitMessageInput  textinput.Model
	commitForMergeModal      *modal.Modal // Modal instance
	commitForMergeModalWidth int          // Cached width for rebuild detection

	// Agent choice modal state (attach vs restart)
	agentChoiceWorktree   *Worktree
	agentChoiceIdx        int          // 0=attach, 1=restart
	agentChoiceModal      *modal.Modal // Modal instance
	agentChoiceModalWidth int          // Cached width for rebuild detection

	// Agent config modal state (start/restart with options)
	agentConfigWorktree   *Worktree
	agentConfigIsRestart  bool
	agentConfigAgentType  AgentType
	agentConfigAgentIdx   int
	agentConfigAgentList  []AgentType // Fixed picker list for modal lifetime (includes preferred)
	agentConfigSkipPerms  bool
	agentConfigAgentInput textinput.Model
	agentConfigModal      *modal.Modal
	agentConfigModalWidth int

	// Delete confirmation state. The confirmation itself — its sections, its
	// branch cleanup options, and its key/mouse routing — is
	// internal/worktreedelete, shared with the global Workspaces browser. The
	// plugin keeps only the lifecycle handle it needs afterwards.
	deleteConfirmWorktree *Worktree // Worktree pending deletion
	deleteConfirm         worktreedelete.State
	deleteWarnings        []string // Warnings from last delete operation (e.g., branch deletion failures)

	// Shell delete confirmation modal state
	deleteConfirmShell    *ShellSession // Shell pending deletion
	deleteShellModal      *modal.Modal
	deleteShellModalWidth int
	// closeSplitModal asks before closing a split terminal that is running
	// something other than its own shell; shellCloseCommand is what tmux said
	// that something is.
	closeSplitModal      *modal.Modal
	closeSplitModalWidth int
	shellCloseCommand    string

	// Rename shell modal state
	renameShellSession    *ShellSession   // Shell being renamed
	renameShellLeafID     int             // Shell LEAF being renamed, when the modal was opened from a pane title
	renameShellInput      textinput.Model // Text input for new name
	renameShellModal      *modal.Modal    // Modal instance
	renameShellModalWidth int             // Cached width for rebuild detection
	renameShellError      string          // Validation error message

	// Rename worktree modal state (display-name only; branch and path stay put)
	renameWorktree           *Worktree
	renameWorktreeInput      textinput.Model
	renameWorktreeModal      *modal.Modal
	renameWorktreeModalWidth int
	renameWorktreeError      string

	// Initial reconnection tracking
	initialReconnectDone bool

	// State restoration tracking (only restore once on startup)
	stateRestored   bool
	worktreesLoaded bool

	// Auto-create default shell tracking (evaluated once per project load)
	autoShellChecked bool

	// Interactive mode state (feature-gated behind tmux_interactive_input)
	interactiveState *InteractiveState
	// wheel coalesces a trackpad flick, so this surface takes the same amount of
	// one as every other terminal surface does — one flick per terminal surface,
	// because the preview and the panel scroll independently. clock is the time it
	// and other in-memory transition policy read: a field so behavior can be
	// driven without sleeping. nil is the wall clock.
	wheel tty.WheelBursts
	clock func() time.Time

	// Sidebar header hover state
	hoverNewButton            bool
	hoverSortButton           bool
	hoverShellsPlusButton     bool
	hoverWorkspacesPlusButton bool
	hoverStartAgentButton     bool
	startAgentBtn             startAgentButtonHit
	// hoverPaneClose is the content leaf whose header X is under the pointer.
	hoverPaneClose  int
	hoverPaneLayout int
	// hoverTabClose is the per-tab × under the pointer. It is the pane X's
	// smaller twin: same hover paint, addressed by leaf and tab index.
	hoverTabClose tabs.CloseHover
	// hoverDividerRegion / hoverDividerID are the resizable split under the
	// pointer (tree splits also carry the split id).
	hoverDividerRegion string
	hoverDividerID     int

	// Multiple shell sessions (not tied to git worktrees)
	shells           []*ShellSession // Current workDir shells (top Shells section)
	selectedShellIdx int             // Currently selected top-section shell index
	shellSelected    bool            // True when a top-section shell is selected
	// agentLaneTracker turns agent lane transitions into notifications. It is
	// the plugin's only notification state: the rules live in internal/notify,
	// and this holds the per-workspace history they debounce against.
	agentLaneTracker      notify.LaneTracker
	pendingAgentLaneSeeds []notify.Notification
	// nestedByWorkDir is the nest projection of the full manifest, keyed by
	// parent worktree path. Current workDir shells stay out of this map.
	nestedByWorkDir map[string][]*ShellSession
	// selectedNestedTmux is set when a sibling shell row is selected.
	selectedNestedTmux string
	// attachSession replaces tmux attach when set. Tests use it so Enter never
	// talks to the user's tmux server.
	attachSession func(sessionName, displayName string) tea.Cmd

	// Resume conversation state (td-aa4136)
	// pendingPrefillCmd is the command line typed at a new shell's prompt once
	// it exists. A resume fills it by rendering catalog argv; the Configuration
	// repair flow fills it with a command line it already had.
	pendingPrefillCmd     string
	pendingResumeWorktree string // Worktree name to enter interactive mode after agent starts

	// Fetch PR modal state
	fetchPRItems        []PRListItem // PRs from gh pr list
	fetchPRFilter       string       // Filter text
	fetchPRCursor       int          // Selected index in filtered list
	fetchPRScrollOffset int          // Scroll offset for PR list
	fetchPRLoading      bool         // True while gh pr list is running
	fetchPRError        string       // Error message from gh CLI
	fetchPRModal        *modal.Modal // Modal instance
	fetchPRModalWidth   int          // Cached width for rebuild detection

	// Shell manifest for persistence and cross-instance sync (td-f88fdd)
	shellManifest        *ShellManifest
	shellWatcher         shellManifestWatcher
	shellWatcherMessages <-chan tea.Msg
	shellStartupHooks    shellStartupHooks
	// shellLiveness holds what this surface has observed about each shell's
	// tmux session, so a dead one closes and a hiccup does not (td-6a4100).
	shellLiveness *shellliveness.Tracker
	// restoreMarked remembers which shells have had their cold-restore
	// eligibility recorded under which tmux server, so the marker costs no
	// syscalls once written. restoreServerSocket/restoreServerID cache the one
	// `tmux display-message` needed to qualify the socket identity with a pid.
	restoreMarked       map[string]string
	restoreServerSocket tmuxserver.Incarnation
	restoreServer       tmuxserver.Incarnation
	restoreServerKnown  bool
	shellStartupEpoch   uint64
	shellStartupVersion uint64
	shellStartupLoading bool

	// Pending agent UI requests
	pendingViews map[string]*pendingView
	// openSplit is the request-scoped --split axis override ("right"/"below").
	// Empty or "auto" leaves PlanOpen's axis alone. Set around handleUIRequest
	// and consumePendingView only.
	openSplit string
	// pendingOpenPlan is the batch-scoped planned placement for ONE content
	// open: a layout apply commits the exact plan it fit-tested rather than
	// letting deck.Open re-plan from scratch. Nil for every other caller, and
	// scoped to a single open by performPlannedOpen.
	pendingOpenPlan *panelayout.OpenPlan
	// pendingShellPlan is the same idea for the batch's shell item: set around
	// createTerminalSplit so openShellLeaf splits where the plan said instead
	// of re-deriving an auto placement.
	pendingShellPlan *panelayout.OpenPlan
}

// New creates a new worktree manager plugin.
func New() *Plugin {
	// Create markdown renderer (ignore error, will fall back to plain text)
	mdRenderer, _ := markdown.NewRenderer()

	p := &Plugin{
		worktrees:           make([]*Worktree, 0),
		agents:              make(map[string]*Agent),
		managedSessions:     make(map[string]bool),
		shells:              make([]*ShellSession, 0),
		viewMode:            ViewModeList,
		listSort:            workspacelist.SortManual,
		activePane:          PaneSidebar,
		mouseHandler:        mouse.NewHandler(),
		docLinkResolution:   contentlink.NewResolutionIndex(contentlink.MaxPendingResolutions),
		sidebarWidth:        40,   // Default 40% sidebar
		sidebarVisible:      true, // Sidebar visible by default
		tmuxCaptureMaxBytes: defaultTmuxCaptureMaxBytes,
		backgrounds:         tty.BackgroundAuto,
		backgroundSpanMax:   tty.DefaultBackgroundSpanMax,
		truncateCache:       ui.NewTruncateCache(1000), // Cache up to 1000 truncations
		terminalHistory:     make(map[string]tty.HistoryReach),
		markdownRenderer:    mdRenderer,
		shellSelected:       false, // Start with first worktree selected, not shell
		applicationFocused:  true,
		terminalOwnership:   &terminalOwnershipLease{},
		terminalPanes:       termpanes.New(),
		shellStartupHooks:   defaultShellStartupHooks(),
	}
	p.primaryTermPane()
	return p
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return pluginID }

// Name returns the plugin display name.
func (p *Plugin) Name() string { return pluginName }

// Icon returns the plugin icon.
func (p *Plugin) Icon() string { return pluginIcon }

// ViewIsSelfConstrained reports the View contract the workspace already
// enforces for its panels, boards, and overlays. The list renderer deliberately
// raises smaller heights to its four-row panel floor, and widths below three do
// not have enough columns for its final frame, so those dimensions retain the
// app shell's defensive clamp.
func (p *Plugin) ViewIsSelfConstrained() bool {
	return p.width >= 3 && p.height >= 4
}

var _ plugin.SelfConstrainedView = (*Plugin)(nil)

// IsFocused returns whether the plugin is focused.
func (p *Plugin) IsFocused() bool { return p.focused }

// SetFocused sets the focus state.
func (p *Plugin) SetFocused(f bool) {
	if f && p.focused && p.terminalOwnershipIsActive() {
		return
	}
	if !f && !p.focused && !p.terminalOwnershipIsActive() {
		return
	}
	// Exit interactive mode when plugin loses focus (user switched tabs) (td-efd736).
	// The ring stays on the live pane: covering this plugin is not a navigation
	// back to the sidebar or an extra content leaf.
	if !f && p.viewMode == ViewModeInteractive {
		p.exitInteractiveMode()
	}
	// Focus is also the visibility contract for the project surface. Global
	// Workspaces can watch the same pane with its own tty.Model, so a covered
	// project preview must close its subscriptions synchronously before the
	// global model opens; waiting for another Update leaves both models able to
	// resize and consume frames for the same pane.
	if !f {
		p.focused = false
		p.deactivateTerminalOwnership()
		return
	}
	p.focused = true
	p.activateTerminalOwnership()
	p.setTerminalFocus(p.applicationFocused)
}

func (p *Plugin) SetPendingWorkspaceSelection(selection plugin.PendingWorkspaceSelection) {
	p.pendingOverviewSelection = &selection
	p.applyPendingWorkspaceSelection()
}

func (p *Plugin) applyPendingWorkspaceSelection() bool {
	if p.pendingOverviewSelection == nil {
		return false
	}
	target := p.pendingOverviewSelection
	switch target.Kind {
	case plugin.WorkspaceSelectionWorktree:
		for i, wt := range p.worktrees {
			if workspaceinventory.CanonicalPath(wt.Path) == workspaceinventory.CanonicalPath(target.Path) {
				p.selectWorktreeAt(i)
				action := target.Action
				p.pendingOverviewSelection = nil
				p.selectKanbanFromList()
				p.finishNavigatedSelection()
				p.queuePendingOverviewAction(action, wt)
				return true
			}
		}
	case plugin.WorkspaceSelectionShell:
		for i, shell := range p.shells {
			if shell.TmuxName == target.Key {
				p.selectTopShellAt(i)
				p.pendingOverviewSelection = nil
				p.selectKanbanFromList()
				p.finishNavigatedSelection()
				return true
			}
		}
		if parent, shell := p.findNestedShell(target.Key); shell != nil {
			p.selectNestedShell(parent, shell.TmuxName)
			p.pendingOverviewSelection = nil
			p.selectKanbanFromList()
			p.finishNavigatedSelection()
			return true
		}
	}
	if p.worktreesLoaded && !p.shellStartupLoading {
		p.pendingOverviewSelection = nil
		p.toastMessage = "Overview item is no longer available"
		p.toastTime = time.Now()
	}
	return false
}

func (p *Plugin) queuePendingOverviewAction(action string, wt *Worktree) {
	if action != "merge" || wt == nil {
		return
	}
	if reason := WorktreeActionRefusal(wt, WorktreeActionMerge); reason != "" {
		p.pendingOverviewAction = appmsg.Blocked(reason)
		return
	}
	p.pendingOverviewAction = p.startMergeWorkflow(wt)
}

func (p *Plugin) TakePendingWorkspaceAction() tea.Cmd {
	cmd := p.pendingOverviewAction
	p.pendingOverviewAction = nil
	return cmd
}

// finishNavigatedSelection applies this plugin's own selection-change rule to a
// selection that arrived from the global browser, so an arriving destination is
// indistinguishable from one the user picked here.
//
// selectTopShellAt / selectWorktreeAt already wrote the previous surface and
// restored the destination. resetDocPanesForSelection is a safety net for a
// tree that still belongs to someone else. The pending load stays unless that
// reset actually closed the restored leaves.
//
// The rule lives here on purpose: no global code path reads, rewrites, or
// prunes a project's pane layout. It hands over an identity and nothing else.
func (p *Plugin) finishNavigatedSelection() {
	if p.resetDocPanesForSelection() {
		p.paneRestoreCmd = nil
	}
	p.saveSelectionState()
}

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	if p.operationCancel != nil {
		p.operationCancel()
	}
	p.activityAnimationGeneration++
	p.activityAnimationFrame = 0
	p.activityAnimationScheduled = false
	p.invalidateShellStartup()
	p.stopLiveWatchers()
	p.focused = false
	p.deactivateTerminalOwnership()
	p.reuseHeldWheelViewOnce = false
	p.wheelViewCacheOK = false
	p.projectPreview = projectPreviewCache{}
	p.ctx = ctx
	// Filter state is in-memory and per consumer: a project or worktree switch
	// starts from an unfiltered list rather than restoring a query whose origin
	// the user can no longer see.
	p.resetListFilter()
	p.operationCtx, p.operationCancel = context.WithCancel(context.Background())
	p.repoSnapshot = nil
	p.refreshOperationID = ""
	p.activeLifecycleOperationID = ""
	p.resetLifecycleState()
	// Init starts a new pane-tree identity space. Drop the old collection before
	// constructing models so a new primary ID cannot alias an old Shell leaf.
	p.paneRoot = nil
	p.terminalPanes = termpanes.New()
	p.resetTerminalModels()
	p.primaryTermPane().LinkContext = terminalLinkSurfaceContext{}
	p.requireShellTermPane().LinkContext = terminalLinkSurfaceContext{}
	p.primaryTermPane().LinkState = termpreview.LinkState{}
	p.requireShellTermPane().LinkState = termpreview.LinkState{}
	p.applicationFocused = true
	p.contentDeck = nil
	p.paneFocus = 0
	p.paneNextID = 1
	p.paneDragSplitID = 0
	p.paneRestoreCmd = nil
	p.paneLayoutSurface = ""
	p.hiddenPaneLayout = nil
	p.docs = make(map[int]*docPane)
	p.issues = make(map[int]*issuePane)
	p.notes = make(map[int]*notePane)
	p.diffs = make(map[int]*diffPane)
	p.resources = make(map[int]*resourcePane)
	p.issueModelNextID = 0
	p.noteModelNextID = 0
	p.docFinderCaches = panesearch.Caches{}
	p.closeDocInfo()
	p.terminalDocProjection = terminalDocProjection{}
	// The terminal panel is a leaf of this tree now, so it needs one even where
	// document panes are off: with no tree there is nowhere to put a split.
	if features.IsEnabled(features.WorkspaceDocPanes.Name) || terminalPanelEnabled() {
		p.paneRoot = &PaneNode{ID: p.paneNextID, Kind: PaneTerminal}
		p.paneFocus = p.paneNextID
		p.paneNextID++
	}
	if ctx.Config != nil && ctx.Config.Plugins.Workspace.TmuxCaptureMaxBytes > 0 {
		p.tmuxCaptureMaxBytes = ctx.Config.Plugins.Workspace.TmuxCaptureMaxBytes
	}
	if ctx.Config != nil {
		p.backgrounds = tty.NormalizeBackgroundMode(tty.BackgroundMode(ctx.Config.Plugins.Workspace.TerminalBackgrounds))
		if ctx.Config.Plugins.Workspace.TerminalBackgroundSpanMax > 0 {
			p.backgroundSpanMax = ctx.Config.Plugins.Workspace.TerminalBackgroundSpanMax
		} else {
			p.backgroundSpanMax = tty.DefaultBackgroundSpanMax
		}
	}
	if ctx.Config != nil {
		p.setResizeDebounce(time.Duration(ctx.Config.Plugins.Workspace.ResizeDebounceMs) * time.Millisecond)
	}
	p.applyResizeDebounceToTerminals()

	// Reset terminal panel state for reinit (sessions are preserved in tmux)
	p.cleanupTermPanelSession()

	// Reset agent-related state for clean reinit (important for project switching)
	// Without this, reconnectAgents() won't run again after switching projects
	p.initialReconnectDone = false
	p.agentLaneTracker = notify.LaneTracker{}
	p.pendingAgentLaneSeeds = nil
	p.agents = make(map[string]*Agent)
	p.managedSessions = make(map[string]bool)
	p.worktrees = make([]*Worktree, 0)
	// pendingOverviewSelection is deliberately retained across app-owned Reinit.
	p.attachedSession = ""

	// Reset poll generation counters (td-83dc22): invalidates any stale timers from previous project
	p.pollScheduler.Reset()
	p.terminalHistory = make(map[string]tty.HistoryReach)
	p.paneGeometry = make(map[string]paneGeometry)
	p.paneMouseReports = make(map[string]bool)

	// Reset shell state before initializing for new project (critical for project switching)
	p.shells = make([]*ShellSession, 0)
	p.nestedByWorkDir = nil
	p.selectedShellIdx = 0
	p.shellSelected = false
	p.selectedNestedTmux = ""
	// Scroll is not persisted. A leftover offset from the previous project
	// (or a stale visibleCount) would make RenderSidebar / ensureVisible
	// start mid-list even when the restored selection fits from the top.
	p.scrollOffset = 0
	p.visibleCount = 0

	// Reset state restoration flag for project switching
	p.stateRestored = false
	p.worktreesLoaded = false

	// Re-arm default-shell auto-creation for the newly loaded project
	p.autoShellChecked = false

	// Shell manifest I/O, tmux discovery, pane lookup, and watcher construction
	// are deferred to Start's command so Init remains on the first-frame path.
	p.shellManifest = nil
	p.shellStartupEpoch = ctx.Epoch
	p.shellStartupLoading = true

	// Register dynamic keybindings for modal contexts only.
	// Main worktree-list and worktree-preview bindings are in bindings.go.
	if ctx.Keymap != nil {
		// Merge modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-merge")
		ctx.Keymap.RegisterPluginBinding("enter", "continue", "workspace-merge")
		ctx.Keymap.RegisterPluginBinding("up", "select-delete", "workspace-merge")
		ctx.Keymap.RegisterPluginBinding("down", "select-keep", "workspace-merge")

		// Create modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-create")
		ctx.Keymap.RegisterPluginBinding("enter", "confirm", "workspace-create")
		ctx.Keymap.RegisterPluginBinding("tab", "next-field", "workspace-create")
		ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-field", "workspace-create")
		ctx.Keymap.RegisterPluginBinding("up", "navigate-picker", "workspace-create")
		ctx.Keymap.RegisterPluginBinding("down", "navigate-picker", "workspace-create")
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-create-confirm")
		ctx.Keymap.RegisterPluginBinding("enter", createConfirmID, "workspace-create-confirm")
		ctx.Keymap.RegisterPluginBinding("enter", createRetrySetupID, "workspace-create-recovery")

		// Task link modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-task-link")
		ctx.Keymap.RegisterPluginBinding("enter", "select-task", "workspace-task-link")
		ctx.Keymap.RegisterPluginBinding("up", "navigate-picker", "workspace-task-link")
		ctx.Keymap.RegisterPluginBinding("down", "navigate-picker", "workspace-task-link")

		// Agent choice modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-agent-choice")
		ctx.Keymap.RegisterPluginBinding("enter", "select", "workspace-agent-choice")
		ctx.Keymap.RegisterPluginBinding("j", "cursor-down", "workspace-agent-choice")
		ctx.Keymap.RegisterPluginBinding("k", "cursor-up", "workspace-agent-choice")
		ctx.Keymap.RegisterPluginBinding("down", "cursor-down", "workspace-agent-choice")
		ctx.Keymap.RegisterPluginBinding("up", "cursor-up", "workspace-agent-choice")

		// Agent config modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-agent-config")
		ctx.Keymap.RegisterPluginBinding("enter", "confirm", "workspace-agent-config")
		ctx.Keymap.RegisterPluginBinding("tab", "next-field", "workspace-agent-config")
		ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-field", "workspace-agent-config")

		// Interactive mode context - uses configured keys (td-18098d)
		ctx.Keymap.RegisterPluginBinding(p.getInteractiveExitKey(), "exit-interactive", "workspace-interactive")
		ctx.Keymap.RegisterPluginBinding(p.getInteractiveCopyKey(), "copy", "workspace-interactive")
		ctx.Keymap.RegisterPluginBinding(superCopyKey, "copy", "workspace-interactive")
		ctx.Keymap.RegisterPluginBinding(p.getInteractivePasteKey(), "paste", "workspace-interactive")

		// The shared `/` filter is its own text-input context. It is registered
		// beside workspace-doc deliberately: the two contexts are never active
		// at once, so neither claims the other's keys.
		ctx.Keymap.RegisterPluginBinding("/", "filter-list", "workspace-list")
		ctx.Keymap.RegisterPluginBinding("enter", "filter-accept", "workspace-filter")
		ctx.Keymap.RegisterPluginBinding("esc", "filter-clear", "workspace-filter")

		// Document panes are a distinct focus context: their navigation must not
		// fall through to terminal scrolling or workspace refresh.
		ctx.Keymap.RegisterPluginBinding("tab", "next-pane", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-pane", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("q", "close", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("esc", "close", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("x", "close-tab", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("{", "prev-tab", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("}", "next-tab", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("m", "render", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("w", "toggle-wrap", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("I", "info", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("ctrl+r", "reveal", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("Y", "yank-path", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("+", "resize-pane-grow", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("-", "resize-pane-shrink", "workspace-doc")
		ctx.Keymap.RegisterPluginBinding("\\", "toggle-sidebar", "workspace-doc")

		// An issue pane is a focus context of its own for the same reason. It
		// has no render mode and no resize keys, so it binds only what it
		// answers; the rest it absorbs rather than passing to the terminal.
		ctx.Keymap.RegisterPluginBinding("q", "close", "workspace-issue")
		ctx.Keymap.RegisterPluginBinding("esc", "close", "workspace-issue")
		ctx.Keymap.RegisterPluginBinding("x", "close-tab", "workspace-issue")
		ctx.Keymap.RegisterPluginBinding("{", "prev-tab", "workspace-issue")
		ctx.Keymap.RegisterPluginBinding("}", "next-tab", "workspace-issue")
		ctx.Keymap.RegisterPluginBinding("\\", "toggle-sidebar", "workspace-issue")
		ctx.Keymap.RegisterPluginBinding("enter", "open-item", "workspace-issue")
		ctx.Keymap.RegisterPluginBinding("O", "open-in-td", "workspace-issue")
		ctx.Keymap.RegisterPluginBinding("y", "yank-issue", "workspace-issue")
		ctx.Keymap.RegisterPluginBinding("Y", "yank-issue-key", "workspace-issue")
		ctx.Keymap.RegisterPluginBinding("tab", "next-pane", "workspace-issue")
		ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-pane", "workspace-issue")

		ctx.Keymap.RegisterPluginBinding("q", "close", "workspace-note")
		ctx.Keymap.RegisterPluginBinding("esc", "close", "workspace-note")
		ctx.Keymap.RegisterPluginBinding("x", "close-tab", "workspace-note")
		ctx.Keymap.RegisterPluginBinding("{", "prev-tab", "workspace-note")
		ctx.Keymap.RegisterPluginBinding("}", "next-tab", "workspace-note")
		ctx.Keymap.RegisterPluginBinding("\\", "toggle-sidebar", "workspace-note")
		ctx.Keymap.RegisterPluginBinding("y", "yank-note", "workspace-note")
		ctx.Keymap.RegisterPluginBinding("Y", "yank-note-key", "workspace-note")
		ctx.Keymap.RegisterPluginBinding("tab", "next-pane", "workspace-note")
		ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-pane", "workspace-note")

		ctx.Keymap.RegisterPluginBinding("q", "close", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding("esc", "close", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding("x", "close-tab", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding("{", "prev-tab", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding("}", "next-tab", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding(",", "prev-file", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding(".", "next-file", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding("Y", "yank-id", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding("\\", "toggle-sidebar", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding("tab", "next-pane", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-pane", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding("+", "resize-pane-grow", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding("-", "resize-pane-shrink", "workspace-diff")
		ctx.Keymap.RegisterPluginBinding("f", "file-picker", "workspace-diff")
	}

	// Load saved sidebar width
	if savedWidth := state.GetWorkspaceSidebarWidth(); savedWidth > 0 {
		p.sidebarWidth = savedWidth
	}

	// Load saved diff tab file list width
	if savedWidth := state.GetDiffTabFileListWidth(); savedWidth > 0 {
		p.diff.SetListWidth(savedWidth)
	}

	// Convert the pre-split terminal panel preference into the pane tree's
	// vocabulary, once. A previously visible panel must not come back after the
	// default flipped off, so the flag is still gated on the feature; the
	// remembered shape is taken either way, because it is what ctrl+t opens at.
	if prefs := p.takeLegacyTermPanelPrefs(); restoreTermPanelVisible(prefs.Visible) {
		p.legacyTermPanel = prefs
		p.requestShellLeaf()
	}

	// Load saved diff view mode
	switch state.GetWorkspaceDiffMode() {
	case "side-by-side":
		p.diff.ViewMode = DiffViewSideBySide
	case "full-file":
		p.diff.ViewMode = DiffViewFullFile
	}

	return nil
}

// Start begins async operations.
func (p *Plugin) Start() tea.Cmd {
	if p.remoteBound() {
		p.applyHostInventory()
		return tea.Batch(p.reconcileTerminalModels()...)
	}
	// Refresh worktrees - reconnectAgents will be called after worktrees are loaded
	return tea.Batch(
		p.refreshWorktrees(),
		p.loadShellStartup(),
	)
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	// Registry reinitialization calls Stop before replacing the project context,
	// so this is both the quit and project-switch durability boundary.
	p.saveSelectionState()
	if p.operationCancel != nil {
		p.operationCancel()
		p.operationCancel = nil
	}
	p.invalidateShellStartup()
	p.focused = false
	p.deactivateTerminalOwnership()
	// Clean up terminal panel tmux session
	p.cleanupTermPanelSession()
}

func (p *Plugin) resetLifecycleState() {
	if p.mergeState != nil && p.mergeState.PRGenerationCancel != nil {
		p.mergeState.PRGenerationCancel()
	}
	p.activeLifecycleOperationID = ""
	p.mergeState = nil
	p.mergeModal = nil
	p.mergeCommitState = nil
	p.commitForMergeModal = nil
	p.linkingWorktree = nil
	p.deleteConfirmWorktree = nil
	p.deleteConfirm.Clear()
	p.fetchPRItems = nil
	p.fetchPRLoading = false
	p.fetchPRError = ""
	p.fetchPRModal = nil
	if p.viewMode == ViewModeCreate {
		p.clearCreateModal()
	}
	switch p.viewMode {
	case ViewModeCreate, ViewModeTaskLink, ViewModeMerge, ViewModeCommitForMerge,
		ViewModeConfirmDelete, ViewModeFetchPR:
		p.viewMode = ViewModeList
	}
}

func (p *Plugin) newOperationScope(wt *Worktree) (context.Context, OperationScope) {
	p.operationSeq++
	scope := OperationScope{Epoch: p.ctx.Epoch, OperationID: fmt.Sprintf("%d-%d", p.ctx.Epoch, p.operationSeq)}
	if p.repoSnapshot != nil {
		scope.RepoKey = p.repoSnapshot.Key
	}
	if wt != nil {
		scope.WorktreeKey = wt.IdentityKey()
		if scope.RepoKey == "" {
			scope.RepoKey = wt.RepoKey
		}
	}
	ctx := p.operationCtx
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx, scope
}

func (p *Plugin) newContextScope(wt *Worktree) (context.Context, OperationScope) {
	if wt == nil {
		for _, candidate := range p.worktrees {
			if p.ctx != nil && filepath.Clean(candidate.Path) == filepath.Clean(p.ctx.WorkDir) {
				wt = candidate
				break
			}
		}
	}
	ctx, scope := p.newOperationScope(wt)
	if scope.RepoKey == "" && p.ctx != nil {
		scope.RepoKey = stablePathKey(p.ctx.ProjectRoot)
	}
	if scope.WorktreeKey == "" && p.ctx != nil {
		scope.WorktreeKey = stablePathKey(p.ctx.WorkDir)
	}
	return ctx, scope
}

func (p *Plugin) newLifecycleScope(wt *Worktree) (context.Context, OperationScope) {
	ctx, scope := p.newContextScope(wt)
	scope.Lifecycle = true
	p.activeLifecycleOperationID = scope.OperationID
	return ctx, scope
}

func (p *Plugin) lifecycleScope(wt *Worktree) OperationScope {
	if p.mergeState != nil && p.mergeState.Worktree != nil && wt != nil &&
		p.mergeState.Worktree.IdentityKey() == wt.IdentityKey() {
		return p.mergeState.OperationScope
	}
	_, scope := p.newLifecycleScope(wt)
	return scope
}

func (p *Plugin) scopeMatches(scope OperationScope) bool {
	// Zero scope is accepted only for old internal/test-only messages. New
	// lifecycle command constructors always stamp a non-empty operation ID.
	if scope.OperationID == "" {
		return true
	}
	if p.ctx == nil || scope.Epoch != p.ctx.Epoch {
		return false
	}
	if scope.RepoKey != "" && p.repoSnapshot != nil && scope.RepoKey != p.repoSnapshot.Key {
		return false
	}
	if scope.WorktreeKey != "" && p.findWorktree(scope.WorktreeKey) == nil {
		if p.ctx == nil || scope.WorktreeKey != stablePathKey(p.ctx.WorkDir) {
			return false
		}
	}
	if scope.Lifecycle && scope.OperationID != p.activeLifecycleOperationID {
		return false
	}
	return true
}

// saveSelectionState persists the current selection to disk.
func (p *Plugin) saveSelectionState() {
	if p.ctx == nil {
		return
	}

	hooks := p.shellStartupHooks.withDefaults()
	wtState := hooks.getWorkspaceState(p.ctx.ProjectRoot)
	wtState.WorkspaceName = ""
	wtState.ShellTmuxName = ""

	if shell := p.getSelectedShell(); shell != nil {
		wtState.ShellTmuxName = shell.TmuxName
	} else if p.selectedIdx >= 0 && p.selectedIdx < len(p.worktrees) {
		wtState.WorkspaceName = p.worktrees[p.selectedIdx].Name
	}
	// A disabled feature must not consume or overwrite its dormant state. The
	// nil paneRoot is the Init-time feature boundary and preserves PaneLayout
	// and PaneLayouts verbatim through ordinary selection saves.
	if p.paneRoot != nil {
		state.MigratePaneLayouts(&wtState)
		if layout := p.persistedPaneLayout(); layout != nil && layout.Surface != "" {
			if wtState.PaneLayouts == nil {
				wtState.PaneLayouts = make(map[string]*state.PaneLayoutJSON)
			}
			wtState.PaneLayouts[layout.Surface] = layout
			p.paneLayoutSurface = layout.Surface
		}
		wtState.PaneLayout = nil
	}

	// td-f88fdd: Shell display names now persisted in shells.json manifest
	// Only save selection state (which worktree/shell is selected)
	if wtState.WorkspaceName != "" || wtState.ShellTmuxName != "" || wtState.PaneLayout != nil || len(wtState.PaneLayouts) > 0 {
		_ = hooks.setWorkspaceState(p.ctx.ProjectRoot, wtState)
	}
}

// restoreSelectionState restores selection from saved state.
// Returns true if selection was restored, false if default should be used.
func (p *Plugin) restoreSelectionState() bool {
	if p.ctx == nil {
		return false
	}

	wtState := p.shellStartupHooks.withDefaults().getWorkspaceState(p.ctx.ProjectRoot)

	// No saved selection. A layout is meaningful only after its terminal root
	// has been selected, so never restore it independently.
	if wtState.WorkspaceName == "" && wtState.ShellTmuxName == "" {
		return false
	}

	// Try to restore shell selection first (if saved)
	if wtState.ShellTmuxName != "" {
		for i, shell := range p.shells {
			if shell.TmuxName == wtState.ShellTmuxName {
				p.applyTopShellSelection(i)
				p.restoreIncomingPaneLayoutHonoringOpen()
				p.saveSelectionState()
				return true
			}
		}
		if parent, shell := p.findNestedShell(wtState.ShellTmuxName); shell != nil {
			p.applyNestedShellSelection(parent, shell.TmuxName)
			p.restoreIncomingPaneLayoutHonoringOpen()
			p.saveSelectionState()
			return true
		}
		// Shell no longer exists, fall through to try worktree
	}

	// Try to restore worktree selection
	if wtState.WorkspaceName != "" {
		for i, wt := range p.worktrees {
			if wt.Name == wtState.WorkspaceName {
				p.applyWorktreeSelection(i)
				p.restoreIncomingPaneLayoutHonoringOpen()
				p.saveSelectionState()
				return true
			}
		}
	}

	// Saved items no longer exist
	return false
}

// defaultShellNamePattern matches names like "Shell 1", "Shell 2", etc.
var defaultShellNamePattern = regexp.MustCompile(`^Shell \d+$`)

// isDefaultShellName returns true if the name matches the auto-generated pattern "Shell N".
func isDefaultShellName(name string) bool {
	return defaultShellNamePattern.MatchString(name)
}

// selectedWorktree returns the currently selected worktree.
// Returns nil if a shell (top-section or nested) is selected.
func (p *Plugin) selectedWorktree() *Worktree {
	if p.shellSelected {
		return nil
	}
	if p.selectedNestedTmux != "" {
		if _, shell := p.findNestedShell(p.selectedNestedTmux); shell != nil {
			return nil
		}
	}
	if p.selectedIdx < 0 || p.selectedIdx >= len(p.worktrees) {
		return nil
	}
	wt := p.worktrees[p.selectedIdx]
	// A worktree the list does not offer cannot be the selected surface. The
	// index still defaults to zero — the main checkout — before anything has
	// been restored, and without this the preview would open on a workspace
	// that has no row, which reads as a selection the user cannot find.
	if !p.listedWorktree(wt) {
		return nil
	}
	return wt
}

func (p *Plugin) applyTopShellSelection(idx int) {
	p.shellSelected = true
	p.selectedShellIdx = idx
	p.selectedNestedTmux = ""
}

func (p *Plugin) applyWorktreeSelection(idx int) {
	p.shellSelected = false
	p.selectedIdx = idx
	p.selectedNestedTmux = ""
}

func (p *Plugin) applyNestedShellSelection(parentIdx int, tmuxName string) {
	p.shellSelected = false
	p.selectedIdx = parentIdx
	p.selectedNestedTmux = tmuxName
}

func (p *Plugin) selectTopShellAt(idx int) {
	p.changeSelectedSurface(func() { p.applyTopShellSelection(idx) })
}

func (p *Plugin) selectWorktreeAt(idx int) {
	p.changeSelectedSurface(func() { p.applyWorktreeSelection(idx) })
}

func (p *Plugin) selectNestedShell(parentIdx int, tmuxName string) {
	p.changeSelectedSurface(func() { p.applyNestedShellSelection(parentIdx, tmuxName) })
}

// changeSelectedSurface captures the outgoing identity, applies the index
// change, encodes the still-live tree under that captured key, then restores
// the incoming surface. The store key must stay the captured one: using
// selectedTerminalSurface after apply would write the outgoing tree onto B,
// or the safety net would write terminal-only onto B.
func (p *Plugin) changeSelectedSurface(apply func()) {
	oldRoot, oldSurface, oldOK := p.selectedTerminalSurface()
	apply()
	if p.paneRoot == nil {
		return
	}
	_, newSurface, newOK := p.selectedTerminalSurface()
	if oldOK && newOK && oldSurface == newSurface {
		return
	}
	if oldOK && p.liveTreeRepresents(oldSurface) {
		p.storeLivePaneLayout(oldRoot, oldSurface)
	}
	p.restoreIncomingPaneLayout()
}

// retargetAfterSelectedSurfaceGone jumps without storing. The outgoing
// surface no longer exists; a store through persistedPaneLayout would write
// terminal-only onto the incoming key and wipe that surface's set.
func (p *Plugin) retargetAfterSelectedSurfaceGone(apply func()) {
	apply()
	if p.paneRoot == nil {
		return
	}
	p.restoreIncomingPaneLayoutHonoringOpen()
}

func (p *Plugin) retargetAfterKilledTopShell(removedIdx int) {
	if !p.shellSelected {
		return
	}
	if removedIdx < p.selectedShellIdx {
		p.selectedShellIdx--
		return
	}
	if removedIdx != p.selectedShellIdx && p.selectedShellIdx < len(p.shells) {
		return
	}
	if len(p.shells) > 0 {
		dest := p.selectedShellIdx
		if dest >= len(p.shells) {
			dest = len(p.shells) - 1
		}
		p.retargetAfterSelectedSurfaceGone(func() { p.applyTopShellSelection(dest) })
		return
	}
	if len(p.worktrees) > 0 {
		p.retargetAfterSelectedSurfaceGone(func() { p.applyWorktreeSelection(0) })
		return
	}
	p.shellSelected = false
	p.selectedShellIdx = 0
	p.selectedIdx = -1
}

func (p *Plugin) selectingShell() bool {
	return p.getSelectedShell() != nil
}

func (p *Plugin) currentWorkDir() string {
	if p.ctx == nil {
		return ""
	}
	return p.ctx.WorkDir
}

func (p *Plugin) isCurrentWorkDir(path string) bool {
	return sameWorkDir(path, p.currentWorkDir())
}

func (p *Plugin) worktreePaths() []string {
	paths := make([]string, 0, len(p.worktrees))
	for _, wt := range p.worktrees {
		if wt != nil && wt.Path != "" {
			paths = append(paths, wt.Path)
		}
	}
	return paths
}

func (p *Plugin) findNestedShell(tmuxName string) (parentIdx int, shell *ShellSession) {
	if tmuxName == "" {
		return -1, nil
	}
	for i, wt := range p.worktrees {
		for _, candidate := range p.nestedByWorkDir[filepath.Clean(wt.Path)] {
			if candidate != nil && candidate.TmuxName == tmuxName {
				return i, candidate
			}
		}
	}
	return -1, nil
}

func (p *Plugin) rebuildNestedShells(defs []ShellDefinition, paneID func(string) string) {
	current := p.currentWorkDir()
	grouped := groupManifestShellsByWorkDir(defs, p.worktreePaths(), current, paneID)
	nested := make(map[string][]*ShellSession, len(grouped))
	for dir, shells := range grouped {
		if sameWorkDir(dir, current) {
			continue
		}
		nested[filepath.Clean(dir)] = shells
	}
	p.nestedByWorkDir = nested
	if p.selectedNestedTmux != "" {
		if _, shell := p.findNestedShell(p.selectedNestedTmux); shell == nil {
			// The name is gone; selectedTerminalSurface already reports the
			// parent. Restore that surface instead of writing the nested
			// tree onto it.
			p.selectedNestedTmux = ""
			p.restoreIncomingPaneLayoutHonoringOpen()
		}
	}
}

// dropNestedShell forgets a shell that belongs to a sibling worktree rather
// than the current one. The nest is a projection of the same project manifest,
// so removing the manifest entry and rebuilding is the whole of it — there is
// no second list to keep in step. Reports whether the name was a nested row,
// so callers can tell "not mine" from "removed".
func (p *Plugin) dropNestedShell(tmuxName string) bool {
	parent, shell := p.findNestedShell(tmuxName)
	if shell == nil {
		return false
	}
	if shell.Agent != nil {
		shell.Agent.OutputBuf = nil
		shell.Agent = nil
	}
	delete(p.managedSessions, tmuxName)
	globalPaneCache.remove(tmuxName)
	globalActiveRegistry.remove(tmuxName)
	if p.shellManifest != nil {
		_ = p.shellManifest.RemoveShell(tmuxName)
	}
	dir := filepath.Clean(p.worktrees[parent].Path)
	// Jump to the parent while the nested identity is still resolvable so
	// changeSelectedSurface can store the nested set instead of treating
	// old and new as the same worktree.
	if p.selectedNestedTmux == tmuxName {
		p.selectWorktreeAt(parent)
	}
	p.forgetPaneSurfaces("shell:" + tmuxName)
	remaining := make([]*ShellSession, 0, len(p.nestedByWorkDir[dir]))
	for _, candidate := range p.nestedByWorkDir[dir] {
		if candidate.TmuxName != tmuxName {
			remaining = append(remaining, candidate)
		}
	}
	if len(remaining) == 0 {
		delete(p.nestedByWorkDir, dir)
	} else {
		p.nestedByWorkDir[dir] = remaining
	}
	return true
}

func (p *Plugin) rebuildNestedShellsFromState() {
	var defs []ShellDefinition
	if p.shellManifest != nil {
		defs = p.shellManifest.Shells
	}
	p.rebuildNestedShells(defs, nil)
}

func (p *Plugin) backfillWorkDirsCmd() tea.Cmd {
	if p.shellManifest == nil {
		return nil
	}
	inferred := make(map[string]string)
	current := p.currentWorkDir()
	paths := p.worktreePaths()
	var defs []ShellDefinition
	if p.shellManifest != nil {
		defs = p.shellManifest.Shells
	}
	for _, def := range defs {
		if strings.TrimSpace(def.WorkDir) != "" {
			continue
		}
		if dir := inferDefinitionWorkDir(def, paths, current); dir != "" {
			inferred[def.TmuxName] = dir
		}
	}
	if len(inferred) == 0 {
		return nil
	}
	manifest := p.shellManifest
	return func() tea.Msg {
		_ = manifest.BackfillWorkDirs(inferred)
		return nil
	}
}

// AckDwell is how long a session's output must stay selected before its
// completion is treated as read. Short enough to feel immediate when you stop
// on a card, long enough that scrolling through the list clears nothing.
const AckDwell = 2 * time.Second

// dwellSatisfied reports whether the current selection has been held long
// enough to count as having looked at it.
func (p *Plugin) dwellSatisfied(now time.Time) bool {
	if p.selectionSince.IsZero() {
		return true
	}
	return now.Sub(p.selectionSince) >= AckDwell
}

// outputVisibleFor returns true when a worktree's output is actually being
// viewed. App blur keeps observation alive but must not acknowledge activity.
func (p *Plugin) outputVisibleFor(worktreeName string) bool {
	if !p.focused || !p.applicationFocused {
		return false
	}
	return p.outputVisibleForUnfocused(worktreeName)
}

// outputVisibleForUnfocused returns true when a worktree's output is on-screen,
// regardless of whether the plugin is focused. Used for "visible but unfocused" polling.
func (p *Plugin) outputVisibleForUnfocused(worktreeName string) bool {
	if p.viewMode != ViewModeList && p.viewMode != ViewModeInteractive {
		return false
	}
	wt := p.selectedWorktree()
	if wt == nil || wt.IdentityKey() != worktreeName {
		return false
	}
	return true
}

// shellOutputVisibleFor reports whether a shell's live output is actually being
// viewed. Selection alone is insufficient while another plugin is focused.
func (p *Plugin) shellOutputVisibleFor(tmuxName string) bool {
	if !p.focused || !p.applicationFocused || (p.viewMode != ViewModeList && p.viewMode != ViewModeInteractive) {
		return false
	}
	shell := p.getSelectedShell()
	return shell != nil && shell.TmuxName == tmuxName
}

// backgroundPollInterval returns the poll delay when output isn't visible.
func (p *Plugin) backgroundPollInterval() time.Duration {
	if p.focused {
		return pollIntervalBackground
	}
	return pollIntervalUnfocused
}

// getOutputLineCount returns the line count of the currently visible output buffer.
func (p *Plugin) getOutputLineCount() int {
	if p.selectingShell() {
		if shell := p.getSelectedShell(); shell != nil && shell.Agent != nil && shell.Agent.OutputBuf != nil {
			return shell.Agent.OutputBuf.LineCount()
		}
	} else {
		if wt := p.selectedWorktree(); wt != nil && wt.Agent != nil && wt.Agent.OutputBuf != nil {
			return wt.Agent.OutputBuf.LineCount()
		}
	}
	return 0
}

// getPreviewVisibleHeight estimates the visible content height for scroll clamping.
// The exact height is only known during render, but this is close enough for key handling.
func (p *Plugin) getPreviewVisibleHeight() int {
	if p.width > 0 && p.height > 0 {
		var h int
		if p.shellLeafVisible() {
			_, h = p.calculateAgentPaneDimensions()
		} else {
			_, h = p.calculatePreviewDimensions()
		}
		if h > 0 {
			return h
		}
	}
	h := p.height - panelBorderWidth - terminalHeaderRows
	if h < 1 {
		h = 1
	}
	return h
}

// getMaxScrollOffset returns the maximum scroll offset for the current preview
// content: how far a document's absolute offset can move from the top of its
// content, which is the same number of rows a terminal window can sit back from
// its live bottom (previewMaxScroll reads it that way round).
func (p *Plugin) getMaxScrollOffset() int {
	contentHeight := p.getOutputLineCount()
	visibleHeight := p.getPreviewVisibleHeight()
	maxOffset := contentHeight - visibleHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	return maxOffset
}

// previewShowsTerminal reports whether the preview pane is drawing the primary
// terminal rather than a document. Worktree terminals are terminals; documents
// live in their own leaves.
func (p *Plugin) previewShowsTerminal() bool {
	return true
}

// jumpPreviewWindow places the primary terminal's window at an explicit
// distance back from the live bottom, which ends any pin: a jump chooses its
// own window rather than resuming from the one a gesture was reading.
func (p *Plugin) jumpPreviewWindow(offset int) {
	p.releaseTerminalWindowPin(false)
	p.primaryTermPane().Scroll = max(offset, 0)
}

// resetPreviewScroll puts the preview back where a new selection or a newly
// opened tab starts it: documents at the top of their content, the terminal
// window at the live bottom.
func (p *Plugin) resetPreviewScroll() {
	p.previewOffset = 0
	p.jumpPreviewWindow(0)
}

// pollSelectedAgentNowIfVisible triggers an immediate poll for visible output.
func (p *Plugin) pollSelectedAgentNowIfVisible() tea.Cmd {
	wt := p.selectedWorktree()
	if wt == nil || wt.Agent == nil {
		return nil
	}
	if !p.outputVisibleFor(wt.IdentityKey()) {
		return nil
	}
	return p.scheduleAgentPoll(wt.IdentityKey(), 0)
}

// pollAllAgentStatusesNow triggers an immediate poll for every worktree that has
// an active agent. Used when entering kanban view so all statuses are fresh.
func (p *Plugin) pollAllAgentStatusesNow() tea.Cmd {
	var cmds []tea.Cmd
	for _, wt := range p.worktrees {
		if wt.Agent == nil || p.attachedSession == wt.Name {
			continue
		}
		cmds = append(cmds, p.scheduleAgentPoll(wt.IdentityKey(), 0))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// removeWorktreeByName removes a worktree from the list by name.
func (p *Plugin) removeWorktreeByName(name string) {
	for i, wt := range p.worktrees {
		if wt.Name == name {
			p.removeWorktreeAt(i)
			return
		}
	}
}

func (p *Plugin) removeWorktreeByIdentity(key string) {
	for i, wt := range p.worktrees {
		if wt.IdentityKey() == key {
			p.removeWorktreeAt(i)
			return
		}
	}
}

func (p *Plugin) removeWorktreeAt(index int) {
	if index < 0 || index >= len(p.worktrees) {
		return
	}
	wt := p.worktrees[index]
	selectedRemoved := !p.shellSelected && p.selectedIdx == index
	p.forgetWorktreePaneLayout(wt)
	if nested := p.nestedByWorkDir[filepath.Clean(wt.Path)]; len(nested) > 0 {
		for _, shell := range nested {
			if shell != nil {
				p.forgetPaneSurfaces("shell:" + shell.TmuxName)
			}
		}
		delete(p.nestedByWorkDir, filepath.Clean(wt.Path))
	}
	p.worktrees = append(p.worktrees[:index], p.worktrees[index+1:]...)

	if selectedRemoved {
		switch {
		case len(p.worktrees) > 0:
			dest := min(index, len(p.worktrees)-1)
			p.retargetAfterSelectedSurfaceGone(func() { p.applyWorktreeSelection(dest) })
		case len(p.shells) > 0:
			dest := min(p.selectedShellIdx, len(p.shells)-1)
			p.retargetAfterSelectedSurfaceGone(func() { p.applyTopShellSelection(dest) })
		default:
			p.shellSelected = false
			p.selectedNestedTmux = ""
			p.selectedIdx = -1
			if p.paneRoot != nil {
				p.resetPaneTreeToTerminal()
				p.paneLayoutSurface = ""
			}
		}
	} else if p.selectedIdx > index {
		p.selectedIdx--
	}
	p.saveSelectionState()
}

// isKnownAgentType reports whether agentType is a recognized, non-empty agent type.
func isKnownAgentType(agentType AgentType) bool {
	if agentType == "" {
		return false
	}
	for _, at := range AgentTypeOrder {
		if at == agentType {
			return true
		}
	}
	return false
}

func (p *Plugin) getConfigDefaultAgentType() AgentType {
	if p != nil && p.ctx != nil && p.ctx.Config != nil {
		configAgent := AgentType(strings.TrimSpace(p.ctx.Config.Plugins.Workspace.DefaultAgentType))
		if isKnownAgentType(configAgent) {
			return configAgent
		}
	}
	return AgentClaude
}

// resolveWorktreeAgentType returns the agent type to use when starting an agent for a worktree.
// Hierarchy: .sidecar-agent file -> config defaultAgentType -> Claude fallback.
func (p *Plugin) resolveWorktreeAgentType(wt *Worktree) AgentType {
	if wt != nil && p != nil && p.ctx != nil {
		fileAgent := loadAgentType(p.ctx.ProjectRoot, wt.Path)
		if isKnownAgentType(fileAgent) {
			return fileAgent
		}
	}
	return p.getConfigDefaultAgentType()
}

// getDefaultCreateAgentType returns the default agent for create-worktree modal.
// Precedence: persisted last-create agent (if still selectable) → .sidecar-agent
// in the current workspace → config defaultAgentType → Claude. Result is
// clamped to the selectable allowlist when the preferred type is hidden.
func (p *Plugin) getDefaultCreateAgentType() AgentType {
	agents := p.selectableAgentTypes()
	if last := AgentType(strings.TrimSpace(state.GetLastCreateAgent())); last != "" {
		for _, at := range agents {
			if at == last {
				return last
			}
		}
	}
	var preferred AgentType
	if p != nil && p.ctx != nil {
		fileAgent := loadAgentType(p.ctx.ProjectRoot, p.ctx.WorkDir)
		if isKnownAgentType(fileAgent) {
			preferred = fileAgent
		} else {
			preferred = p.getConfigDefaultAgentType()
		}
	} else {
		preferred = p.getConfigDefaultAgentType()
	}
	got, _ := clampAgentSelection(agents, preferred, -1)
	return got
}

func (p *Plugin) preferredCreateAgent() string {
	if p == nil || p.ctx == nil {
		return ""
	}
	fileAgent := loadAgentType(p.ctx.ProjectRoot, p.ctx.WorkDir)
	if isKnownAgentType(fileAgent) {
		return string(fileAgent)
	}
	return ""
}

func (p *Plugin) createOpenOpts(kind workspacecreate.Kind, focusKind bool, name string) workspacecreate.OpenOpts {
	nextShell := "Shell 1"
	if p.ctx != nil {
		nextShell = p.nextShellDisplayName()
	}
	return workspacecreate.OpenOpts{
		Kind:      kind,
		FocusKind: focusKind,
		// This surface has a pane tree, so it can place a terminal split.
		AllowTerminalSplit: terminalPanelEnabled(),
		// The cap is stated in the modal, not discovered after the click: the
		// row and its form render disabled and the create paths refuse.
		TerminalSplitDisabled: p.terminalSplitDisabledReason(),
		TerminalName:          p.terminalSplitAutoName(),
		ShowProject:           false,
		Name:                  name,
		Agents:                p.configAgents(),
		NextShell:             nextShell,
		PreferredAgent:        p.preferredCreateAgent(),
		DefaultAgent:          string(p.getConfigDefaultAgentType()),
		ShowNotes:             p.notesPluginPresent(),
		Providers:             p.configuredProviders(),
	}
}

func (p *Plugin) resetCreateFormState() {
	p.createForm = nil
	p.createTargetWorktree = nil
	p.createError = ""
	p.createOperationModal = nil
	p.createOperationWidth = 0
	p.createPlan = nil
	p.createSetupResult = nil
	p.createDeleteResult = nil
	p.createBusyStep = ""
	p.createCopyEnv = false
	p.createRunHook = false
	p.branchAll = nil
	p.branchFiltered = nil
	p.branchIdx = 0
	p.branchScroll = 0
}

// clearCreateModal resets create modal state.
func (p *Plugin) clearCreateModal() {
	p.resetCreateFormState()
	p.taskSearchInput = textinput.Model{}
	p.taskSearchAll = nil
	p.taskSearchFiltered = nil
	p.taskSearchIdx = 0
	p.taskSearchScroll = 0
	p.taskSearchLoading = false
	p.taskLinkModal = nil
	p.taskLinkModalWidth = 0
}

// openCreate opens the shared Create Workspace form.
func (p *Plugin) openCreate(kind workspacecreate.Kind, focusKind bool, name string) tea.Cmd {
	p.resetCreateFormState()
	p.viewMode = ViewModeCreate
	p.createForm = workspacecreate.Open(p.createOpenOpts(kind, focusKind, name))
	return tea.Batch(p.loadBranches(), p.loadCreatePickerData(), p.loadCreateFileCandidates())
}

// openStartAgentCreate opens the shared create form on Shell so an existing
// worktree with no agent can pick an agent, a name, and skip-permissions the
// same way n/c/[+] already do. Submit starts the agent on this worktree rather
// than creating a new shell in the project root.
func (p *Plugin) openStartAgentCreate(wt *Worktree) tea.Cmd {
	if wt == nil {
		return nil
	}
	p.resetCreateFormState()
	p.createTargetWorktree = wt
	p.viewMode = ViewModeCreate
	opts := p.createOpenOpts(workspacecreate.KindShell, false, "")
	if preferred := p.resolveWorktreeAgentType(wt); preferred != "" && preferred != AgentNone {
		opts.PreferredAgent = string(preferred)
	}
	p.createForm = workspacecreate.Open(opts)
	return tea.Batch(p.loadBranches(), p.loadCreatePickerData(), p.loadCreateFileCandidates())
}

// initCreateModalBase initializes the shared form in Worktree, Name focused.
func (p *Plugin) initCreateModalBase() {
	_ = p.openCreate(workspacecreate.KindWorktree, false, "")
}

func (p *Plugin) initCreateModalNamed(name string) {
	_ = p.openCreate(workspacecreate.KindWorktree, false, name)
}

// openCreateModal opens the create form on the row it was last left on, Name
// focused.
func (p *Plugin) openCreateModal() tea.Cmd {
	return p.openCreateRemembered(false)
}

// openCreateModalFocusKind opens the create form with the kind list focused.
func (p *Plugin) openCreateModalFocusKind() tea.Cmd {
	return p.openCreateRemembered(true)
}

// openCreateRemembered opens the create form on the remembered row: the kind a
// user picked once is the kind they usually want again.
func (p *Plugin) openCreateRemembered(focusKind bool) tea.Cmd {
	p.resetCreateFormState()
	p.viewMode = ViewModeCreate
	opts := p.createOpenOpts(workspacecreate.KindWorktree, focusKind, "")
	opts.UseLastKind = true
	p.createForm = workspacecreate.Open(opts)
	return tea.Batch(p.loadBranches(), p.loadCreatePickerData(), p.loadCreateFileCandidates())
}

// terminalSplitAutoName is the name a terminal split takes when the user types
// none: the workspace's own directory, which is what distinguishes one split
// from another in a header.
func (p *Plugin) terminalSplitAutoName() string {
	dir := strings.TrimSpace(p.termPanelWorkDir())
	if dir == "" {
		return "Terminal"
	}
	return "term · " + filepath.Base(dir)
}

// openCreateModalWithTask opens the create modal with a name derived from the
// task. Linking stays a separate action on the created worktree.
func (p *Plugin) openCreateModalWithTask(taskID, taskTitle string) tea.Cmd {
	return p.openCreate(workspacecreate.KindWorktree, false, p.deriveBranchName(taskID, taskTitle))
}

// deriveBranchName creates a git-safe branch name from task ID and title.
// Format: "<task-id>-<sanitized-title>" e.g., "td-abc123-add-user-auth"
func (p *Plugin) deriveBranchName(taskID, title string) string {
	sanitized := SanitizeBranchName(title)
	// Truncate by runes (not bytes) to avoid corrupting multi-byte Unicode
	runes := []rune(sanitized)
	if len(runes) > 40 {
		sanitized = strings.TrimSuffix(string(runes[:40]), "-")
	}
	if sanitized == "" {
		return taskID
	}
	return taskID + "-" + sanitized
}

// toggleSidebar toggles sidebar visibility.
func (p *Plugin) toggleSidebar() {
	p.sidebarVisible = !p.sidebarVisible
	if !p.sidebarVisible {
		p.activePane = PanePreview
	} else {
		p.activePane = PaneSidebar
	}
}

// moveCursor moves the selection cursor.
// Navigation order: current shells, then each worktree and its nested children.
func (p *Plugin) moveCursor(delta int) {
	oldShellSelected := p.shellSelected
	oldShellIdx := p.selectedShellIdx
	oldWorktreeIdx := p.selectedIdx
	oldNested := p.selectedNestedTmux

	items := p.visibleSidebarItems()
	current := p.sharedSidebarSelectionIndex()
	if current < 0 && len(items) > 0 {
		current = 0
	}
	next := workspacelist.MoveIndex(current, delta, len(items))
	if next >= 0 && next < len(items) {
		p.selectSidebarItem(items[next])
	}

	selectionChanged := p.shellSelected != oldShellSelected ||
		p.selectedNestedTmux != oldNested ||
		(p.shellSelected && p.selectedShellIdx != oldShellIdx) ||
		(!p.shellSelected && p.selectedNestedTmux == "" && p.selectedIdx != oldWorktreeIdx)
	if !selectionChanged {
		// A key or wheel notch the list could not answer — the selection sits
		// against either end — leaves the viewport exactly where it is,
		// including a free-scrolled position: workspacelist.Model.Move returns
		// before its own ensureVisible when the selection did not move, and a
		// clamped press must not drag the view back to the selection.
		return
	}
	p.applySelectionChange()
	p.ensureVisible()
}

// applySelectionChange resets the per-selection preview, diff, and task state.
func (p *Plugin) applySelectionChange() {
	// Selection alone no longer acknowledges: the poll handlers clear the
	// done marker once the selection has been held long enough to read.
	p.selectionSince = time.Now()
	p.resetPreviewScroll()
	p.resetDiffView()
	p.commitStatusWorktree = ""
	// Exit interactive mode when switching selection (td-fc758e88)
	p.exitInteractiveMode()
	// Persist selection to disk
	p.saveSelectionState()
}

// ensureVisible keeps the selection from sitting above the current offset.
// Paging downward by visibleCount is wrong: that count is painted data rows,
// while the sidebar viewport is measured in lines (headings, separators,
// two-line rows). A stale or short count over-scrolls. RenderSidebar is the
// line-aware authority and advances or clamps on the next paint.
func (p *Plugin) ensureVisible() {
	// Whatever moved the selection — a key, the wheel, a click, a refresh that
	// reselected — owns the viewport again: a scrollbar gesture's latch ends
	// here, the way workspacelist.Model.ensureVisible clears its own.
	p.freeScroll = false
	if p.visibleCount <= 0 {
		p.scrollOffset = 0
		return
	}
	position := p.sharedSidebarSelectionIndex()
	if position >= 0 && position < p.scrollOffset {
		p.scrollOffset = position
	}
	p.clampScrollOffset(p.sharedSidebarRowCount())
}

// clampScrollOffset keeps the offset inside the rows currently drawn. A query
// that shrinks the list must never leave the offset past its end, which would
// render an empty sidebar under a filter row still counting matches. Filling
// the pane is RenderSidebar's job: a row-count max (total-visibleCount) is
// not the same as a line-aware last page.
func (p *Plugin) clampScrollOffset(total int) {
	if p.scrollOffset > max(0, total-1) {
		p.scrollOffset = max(0, total-1)
	}
	if p.scrollOffset < 0 {
		p.scrollOffset = 0
	}
}

// loadSelectedContent loads content for the selected surface.
// Diff git runs only while a Diff leaf is showing.
func (p *Plugin) loadSelectedContent() tea.Cmd {
	p.terminalDocProjection = terminalDocProjection{}
	var cmds []tea.Cmd
	if p.resetDocPanesForSelection() {
		// The selection owns the terminal root. Persist the collapsed terminal
		// immediately so an old worktree's document cannot return after restart.
		p.paneRestoreCmd = nil
		p.saveSelectionState()
	}
	if cmd := p.takePaneRestoreCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Resize selected pane to match preview width so capture output is correct
	if cmd := p.resizeSelectedPaneCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// If shell is selected, poll shell output immediately and consume pending views
	if shell := p.getSelectedShell(); shell != nil {
		if shell.Agent != nil {
			cmds = append(cmds, p.pollShellSessionByName(shell.TmuxName))
		}
		if cmd := p.consumePendingView(shell.TmuxName); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if cmd := p.loadSelectedDiff(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if cmd := p.pollSelectedAgentNowIfVisible(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Refresh terminal panel session if selection changed, and resize it
	if cmd := p.refreshTermPanelForSelection(); cmd != nil {
		cmds = append(cmds, cmd)
	} else if p.shellLeafVisible() {
		// Session unchanged — still resize to match current split dimensions
		if cmd := p.resizeTermPanelPaneCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if len(cmds) == 0 {
		return nil
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

// now is the clock for in-memory interaction and transition decisions.
func (p *Plugin) now() time.Time {
	if p.clock != nil {
		return p.clock()
	}
	return time.Now()
}
