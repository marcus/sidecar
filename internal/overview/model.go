// Package overview owns the app-level cross-project Overview model.
package overview

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/activitystore"
	"github.com/marcus/sidecar/internal/agentstatus"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/hosts"
	"github.com/marcus/sidecar/internal/inlineedit"
	"github.com/marcus/sidecar/internal/kanban"
	"github.com/marcus/sidecar/internal/livewatch"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/panereposition"
	"github.com/marcus/sidecar/internal/panesearch"
	"github.com/marcus/sidecar/internal/pluginbrowser"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/shellliveness"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/tabs"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/termpanes"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspacediff"
	"github.com/marcus/sidecar/internal/workspaceinventory"
	"github.com/marcus/sidecar/internal/workspacelist"
	"github.com/marcus/sidecar/internal/workspaceops"
	"github.com/marcus/sidecar/internal/worktreedelete"
)

const (
	minColumnWidth = 17
	cardHeight     = 4
	maxProjects    = 4
	maxCaptures    = 4
	livePollEvery  = 5 * time.Second
	readyPollEvery = 10 * time.Second
	idlePollEvery  = 30 * time.Second
)

type Project struct {
	Name, Path, Key string
	Index           int
}

type refreshPhase string

const (
	phaseIdentity  refreshPhase = "identity"
	phaseInventory refreshPhase = "inventory"
	phaseStatus    refreshPhase = "status"
)

type NavigateMsg struct {
	Workspace  workspaceinventory.Workspace
	Action     string
	Generation int
	RequestID  uint64
}
type ValidationMsg struct {
	Workspace  workspaceinventory.Workspace
	Action     string
	Generation int
	RequestID  uint64
	Err        error
}

// RevealMsg asks the host to show the global Workspaces (Sessions) tab with
// this workspace selected. Activating an Activity card stays in the global
// space: the board and the list are two projections of one catalog, so the card
// names a row the global browser already has, not a project to switch to.
type RevealMsg struct {
	Workspace workspaceinventory.Workspace
}

type panesMsg struct {
	Generation int
	Projects   []Project
	Panes      []workspaceinventory.Pane
	LiveOnly   bool
	Err        error
}
type projectMsg struct {
	Generation int
	Project    Project
	Phase      refreshPhase
	Result     workspaceinventory.ProjectResult
}
type pollMsg struct{ Generation int }

func IsAsyncMessage(msg tea.Msg) bool {
	if IsSharedDiffMessage(msg) {
		return true
	}
	// Live-refresh results are background work by definition: a watcher signal
	// is not a user gesture, and a preview left stale because a modal happened
	// to own focus is the defect this exists to fix.
	if isLiveWatchMessage(msg) {
		return true
	}
	// Manifest-watch results are background work for the same reason: a shell
	// another Sidecar created must land whether or not this browser is the
	// visible surface. See live_shells.go.
	if isShellWatchMessage(msg) {
		return true
	}
	switch msg.(type) {
	case hostUpdateMsg, hostStaleTickMsg:
		// A host reporting in is background work by the same rule live-refresh
		// results are: nobody gestured, and a remote row left stale because a
		// modal owned focus is the defect this classification prevents.
		return true
	case panesMsg, projectMsg, pollMsg, previewAutoScrollTickMsg, workspacePulseTickMsg,
		sessionsSelectedTickMsg,
		previewDocLoadedMsg, previewDocSearchMsg, previewIssueLoadedMsg, previewNoteLoadedMsg, previewResourceResolvedMsg, previewHistoryLoadedMsg, previewTerminalSearchLoadedMsg, contentpanes.Result,
		pluginbrowser.ListedMsg, pluginbrowser.GotMsg, pluginbrowser.ActedMsg, pluginbrowser.DescribedMsg, pluginbrowser.QueryDebouncedMsg, pluginbrowser.ChangedMsg, resourceview.OpenRowMsg,
		renameShellDoneMsg, globalShellCreatedMsg, previewTerminalSplitCreatedMsg, previewSplitSeedFailedMsg, previewSplitCloseProbeMsg, projectMutationRefreshMsg, globalCreateBranchesMsg, previewLinkRevalidatedMsg,
		createPickerDataMsg, createHostCatalogMsg, workspacecreate.FilesScannedMsg:
		// creation is a multi-stage async workflow; every result must stay
		// routed to the global host even while its modal owns focus.
		return true
	case globalWorktreePlannedMsg, globalWorktreeCreatedMsg, globalWorktreeDeletedMsg, globalWorkspaceLaunchedMsg:
		return true
	case globalShellDeletedMsg, globalWorktreeDeleteProbeMsg, globalWorktreeDeleteDoneMsg:
		return true
	case shellProbedMsg, shellForgottenMsg:
		// Auto-close of a dead shell is background work; it must land whether
		// or not this browser is the visible surface (td-6a4100).
		return true
	default:
		return false
	}
}

// IsSharedPickerMessage reports pane-switcher suggestion results. Both hosts
// consume them — whichever surface opened its modal fired the loaders — so
// they must reach the plugins by broadcast as well as this browser, and the
// app's async hand-off must not stop there. See internal/app/update.go.
func IsSharedPickerMessage(msg tea.Msg) bool {
	switch msg.(type) {
	case createPickerDataMsg, workspacecreate.FilesScannedMsg:
		return true
	default:
		return false
	}
}

// IsSharedDiffMessage reports whether msg must remain available to project
// workspace plugins after the global browser has inspected it. Deck results
// are shared by construction; raw workspacediff messages predate Deck and are
// shared for the same reason. Routing either family here alone leaves the
// project pane that issued the load waiting forever.
// IsSharedPluginMessage reports a protocol plugin's own answer or tick.
//
// It is background work like a diff result, and it is SHARED for the same
// reason: the project workspace hosts the same browser this surface does, and a
// page claimed here would leave a project pane refreshing forever. The app
// offers it to this surface and then lets it reach the plugins like any other
// broadcast; every host drops what is not addressed to it.
func IsSharedPluginMessage(msg tea.Msg) bool {
	if _, ok := msg.(resourceview.OpenRowMsg); ok {
		return true
	}
	return pluginbrowser.IsBrowserMsg(msg)
}

func IsSharedDiffMessage(msg tea.Msg) bool {
	if _, ok := msg.(contentpanes.Result); ok {
		return true
	}
	switch msg.(type) {
	case workspacediff.SnapshotMsg, workspacediff.CommitDetailMsg, workspacediff.WorkingTreeFileMsg,
		workspacediff.RangeMsg, workspacediff.CommitFileDiffMsg:
		return true
	default:
		return false
	}
}

type previewOwnershipLease struct {
	mu         sync.RWMutex
	generation uint64
	active     bool
}

type Model struct {
	collector          workspaceinventory.Collector
	refreshCollector   workspaceinventory.Collector
	projects           []Project
	roots              []string
	generation         int
	requestID          uint64
	loading            bool
	tmuxErr            error
	results            map[string]workspaceinventory.ProjectResult
	projectErrors      map[string]error
	stale              map[string]bool
	completed          map[int]bool
	pending            []Project
	pendingInventory   []Project
	phase              refreshPhase
	identityProjects   map[int]Project
	inventoryOrder     []Project
	inventoryScheduled map[string]bool
	inventoryProjects  map[string]Project
	inventoryResults   map[string]workspaceinventory.ProjectResult
	statusInputs       map[string]workspaceinventory.ProjectResult
	active             int
	currentPanes       []workspaceinventory.Pane
	shellClaims        workspaceinventory.ShellClaims
	liveOnly           bool
	ctx                context.Context
	cancel             context.CancelFunc
	traceWriter        io.Writer
	cycleStart         time.Time
	configured         int
	firstResult        bool
	maxActive          int
	pollScheduled      bool
	configuredPaths    []string
	board              kanban.Component
	cards              map[string]workspaceinventory.Workspace
	agentCount         int
	compactScroll      int
	mouse              *mouse.Handler
	workspaces         workspacelist.Model
	workspacesMouse    *mouse.Handler
	// wsBar is the Sessions list's interactive scrollbar: the bar's last
	// render snapshot, where its track sits on screen, whether the pointer
	// hovers it, and any drag gesture in flight.
	wsBar          workspaceScrollbarState
	hoverTermBar   bool
	sidebarWidth   int
	sidebarVisible bool
	catalog        map[string]workspaceinventory.Workspace
	preview        previewState
	// previewSelectKind is the leaf a live text-selection drag started in. A
	// drag is answered by where it began, never by where the pointer has since
	// travelled. See preview_pane_select.go.
	previewSelectKind         panelayout.Kind
	previewOwnership          *previewOwnershipLease
	diff                      workspacediff.View
	terminalConfig            tty.Config
	terminalDefaultBackground string
	terminalSearch            previewTerminalSearchState
	config                    *config.Config
	width                     int
	height                    int
	terminalLinks             termpreview.LinkCoordinator
	linkMatcherGeneration     uint64
	terminalLinkRoot          terminalLinkRootContext

	// Remote hosts. All nil with features.SidecarRemoteHosts off, or with no
	// host registered — see hosts.go. hostCtx is separate from m.ctx on
	// purpose: m.ctx is cancelled on every refresh generation, and hanging ssh
	// connections off it would reconnect every host on each poll.
	hostRegistry *hosts.Registry
	hostCtx      context.Context
	hostCancel   context.CancelFunc
	hostResults  map[string][]workspaceinventory.ProjectResult
	// hostLastKnown retains the last snapshot that actually showed, so `@`
	// can list unreachable hosts as disabled rows. Sessions still drops
	// hostResults when !Shows() and paints a health row instead.
	hostLastKnown map[string][]workspaceinventory.ProjectResult
	// hostRegistered is the set of host IDs config currently names, so a final
	// update from a client that has just been stopped cannot resurrect a
	// de-registered machine as a permanent error row.
	hostRegistered map[string]bool
	// hostIncarnations fences queued updates across a same-ID transport
	// replacement. Nil means host reconciliation has not run yet.
	hostIncarnations map[string]uint64
	// hostConfigured is the last reconciled transport identity. A selected
	// terminal owns its own control process, not the serve registry's, so a
	// removed or retargeted HostID must close that terminal explicitly.
	hostConfigured map[string]hosts.Host
	hostHealth     map[string]hosts.Health
	hostProjects   map[string][]Project
	// contentSource, when set, is the Document adapter for remote rows. Tests
	// inject a fake; production leaves it nil and documentSource builds one.
	contentSource contentpanes.Source
	// RelayedLanding, when set, reports whether Sessions should handle a
	// relayed open/layout (apply or decline). False means the bound project
	// workspace owns it: handleUIRequest returns without acking. Nil means
	// Sessions always considers itself the screen (existing package tests).
	RelayedLanding func(uirequest.Request) bool

	// docFinderCaches holds one file list per pane root, so the file finder a
	// document pane opens walks a tree once rather than once per ctrl+p.
	docFinderCaches panesearch.Caches

	// External terminal resource providers. Both default to nothing, which is
	// the state a Sidecar with no configured provider must stay in: no
	// matchers means no underline, and no resolver means an opened tab says so
	// instead of spinning. The app supplies both once a provider reports ready.
	resourceMatchers []terminallink.ResourceMatcher
	resolveResource  resourceview.Resolver
	// pluginCalls is how a collection or row tab reaches its protocol plugin,
	// injected by the app from the same describe pass that supplies the
	// resolver above.
	pluginCalls resourceview.CallsFor
	// pluginWatchTargets caches the expanded, validated watch targets of the
	// plugins behind the visible collection tabs, keyed by describe generation.
	pluginWatchTargets map[string][]livewatch.Target
	pluginPollTick     uint64
	pluginPollArmed    bool
	// remoteMatchers is the host-scoped describe cache. Local rows never
	// read it; remote rows never read resourceMatchers.
	remoteMatchers remoteMatcherCache
	// remoteResources is the viewer-side remote resource document cache.
	remoteResources remoteResourceCache

	// Working/blocked markers breathe on their own clock, independent of the
	// refresh poll. The generation lets a tick in flight be discarded.
	pulseFrame      int
	pulseScheduled  bool
	pulseGeneration uint64

	// shellLiveness holds what this surface has observed about each shell's
	// tmux session, so a dead one closes and a hiccup does not (td-6a4100).
	shellLiveness *shellliveness.Tracker

	// Cross-instance freshness. The manifest watcher reports shells another
	// Sidecar on this host created; the sweep cursor rotates a slow
	// re-inventory across the configured projects. See live_shells.go.
	shellWatcher           *livewatch.PathWatcher
	shellWatchStarting     bool
	shellWatchGeneration   uint64
	shellManifestPaths     map[string]string
	shellManifestDigests   map[string]string
	shellManifestResolving bool
	sweepCursor            int
	// lastFullInventory is when durable state was last re-read for every
	// project, as opposed to the tmux-only evidence the poll refreshes.
	lastFullInventory time.Time
	// inventoryStamp dates each project's newest durable read, so a background
	// refresh built from an older one can be recognised as superseded. Only
	// reads that actually touched disk stamp it: a live-only poll re-observes
	// the membership it was given and learns nothing new about it.
	inventoryStamp map[string]time.Time

	// A coalesced terminal wheel event that was held changed no visible state.
	// Reuse the preceding Workspaces frame once rather than rebuilding it.
	reuseWorkspacesViewOnce bool
	workspacesViewCache     string
	workspacesViewCacheW    int
	workspacesViewCacheH    int
	workspacesViewCacheOK   bool
	workspacesViewRegions   []mouse.Region

	// The global list is stable across live terminal frames. Its cache owns
	// only the framed left panel and the regions that panel drew; preview and
	// shared pane composition remain live on every frame.
	workspaceListCache         workspaceListRenderCache
	workspaceListDataRevision  uint64
	workspaceListThemeRevision uint64

	// showIdleWorktrees is the global-list visibility flag. Off by default;
	// the sort/filter fly-out is the only control that turns it on. Creating
	// or targeting one worktree must not flip this — that floods OLDER with
	// every idle checkout. Those paths use revealedIdleWorktrees instead.
	showIdleWorktrees bool
	// revealedIdleWorktrees are host-scoped paths the list shows even while
	// showIdleWorktrees is off. One created or targeted worktree, not the
	// whole idle set.
	revealedIdleWorktrees map[string]struct{}
	viewFlyout            *modal.Modal
	viewFlyoutOpen        bool
	viewFlyoutWidth       int
	viewFlyoutSortIdx     int
	viewFlyoutMouse       *mouse.Handler
	// The remotes section is rebuilt whenever the configured set changes, so
	// its checkboxes are bound to slices the modal owns for the life of one
	// build. viewFlyoutHostIDs is the order they were built in;
	// viewFlyoutHostShow is what each box currently reads.
	viewFlyoutHostIDs   []string
	viewFlyoutHostShow  []bool
	viewFlyoutHostsKey  string
	viewFlyoutShowHosts bool

	// hostConfiguredIDs is the reconciled host set in display order, the one
	// startHosts last saw. It is what the remotes controls list and what a
	// hidden entry is validated against.
	hostConfiguredIDs []string

	// hiddenHosts are registered machines whose rows the browser withholds.
	// A view filter only: the connection stays up, so unhiding is instant and
	// a hidden host's notifications still reach the user.
	hiddenHosts map[string]bool

	pendingViews map[string]*pendingView
	// openSplit is the request-scoped --split axis override ("right"/"below").
	openSplit string
	// pendingOpenPlan is the request-scoped explicit-cell plan (--at): set by
	// the open request handler, applied verbatim by the next placement, and
	// cleared whether or not it was used.
	pendingOpenPlan *panelayout.OpenPlan

	// previewCloseHover is set while the pointer is over a content-pane X.
	previewCloseHover  bool
	hoverPreviewClose  panelayout.Kind
	hoverPreviewLayout int
	paneLayoutModal    *panereposition.Controller
	paneZoom           panereposition.Zoom
	// hoverTabClose is the per-tab × under the pointer, keyed by preview kind.
	hoverTabClose tabs.CloseHover
	// hoverHandleRegion / hoverHandleSplit are the resizable split under the pointer.
	hoverHandleRegion string
	hoverHandleSplit  int

	renameOpen bool
	// renameBusy marks a rename whose persist is in flight. Input is swallowed
	// while it is set — the remote round trip takes long enough for a second
	// Enter, which raced two renames on the host with the loser's reply
	// silently dropped.
	renameBusy           bool
	renameWorkspace      workspaceinventory.Workspace
	renameInput          textinput.Model
	renameError          string
	renameModal          *modal.Modal
	renameModalWidth     int
	renameMouse          *mouse.Handler
	renameTerminalLeafID int

	previewSplitCloseLeaf    int
	previewSplitCloseCommand string
	previewSplitCloseModal   *modal.Modal
	previewSplitCloseModalW  int

	createOpen         bool
	createForm         *workspacecreate.Form
	createError        string
	createWarning      string
	createBusy         bool
	createModal        *modal.Modal
	createModalWidth   int
	createMouse        *mouse.Handler
	pendingSplitSeed   *previewSplitSeed
	pendingCreatedTmux string
	createPlan         *workspaceops.WorktreePlan
	createRecord       *workspaceops.WorktreeRecord
	pendingCreatedPath string
	// pendingCreatedHost scopes the pending selection to the machine the
	// workspace was created on. Empty means this one. Without it a remote
	// creation is answered by a local row that happens to share a path or a
	// session name — see honorPendingCreated.
	//
	// Like createTargetHost it is SET on every path that queues a pending
	// selection, never adjusted on noticing a difference. A path that sets
	// pendingCreatedTmux or pendingCreatedPath and leaves this alone inherits
	// the previous creation's machine and then searches the wrong snapshot.
	pendingCreatedHost string
	// createTargetHost is the host the create flow is currently addressing, or
	// "" for this machine. It is SET on every submission rather than adjusted
	// when a difference is noticed, which is the rule that stops a surface that
	// went remote once from staying remote (Phase A's tty.Model defect).
	createTargetHost string

	deleteOpen      bool
	deleteBusy      bool
	deleteError     string
	deleteWorkspace workspaceinventory.Workspace
	deleteModal     *modal.Modal
	deleteModalW    int
	deleteMouse     *mouse.Handler
	// worktreeDelete is the shared "Delete Worktree?" confirmation
	// (internal/worktreedelete) — the same construction the project surface
	// raises. Only the shell confirmation above is this surface's own.
	worktreeDelete worktreedelete.State

	// pendingRestoreSelected is the Sessions row ID to select once the catalog
	// delivers it. Cleared when the row appears or the inventory cycle finishes
	// without it.
	pendingRestoreSelected string
	// sessionsSelectedPending / sessionsSelectedGen debounce writes so arrowing
	// the sidebar is one save of the last ID, not one per row.
	sessionsSelectedPending string
	sessionsSelectedGen     int
}

// ActivityStorePath is overridable so tests never touch the user's state dir.
var ActivityStorePath = func() string {
	return filepath.Join(config.StateDir(), activitystore.FileName)
}

// Sidebar preference access is overridable so interaction tests can prove a
// drag release without reading or writing the developer's real state file.
var (
	loadWorkspaceSidebarWidth   = state.GetWorkspaceSidebarWidth
	saveWorkspaceSidebarWidth   = state.SetWorkspaceSidebarWidth
	loadShowIdleWorktrees       = state.GetShowIdleWorktrees
	saveShowIdleWorktrees       = state.SetShowIdleWorktrees
	loadPinnedWorkspaceIDs      = state.GetPinnedWorkspaceIDs
	savePinnedWorkspaceIDs      = state.SetPinnedWorkspaceIDs
	loadWorkspaceListSort       = state.GetWorkspaceListSort
	saveWorkspaceListSort       = state.SetWorkspaceListSort
	loadLastGlobalCreateProject = state.GetLastGlobalCreateProject
	saveLastGlobalCreateProject = state.SetLastGlobalCreateProject
	loadSessionsSelected        = state.GetSessionsSelected
	saveSessionsSelected        = state.SetSessionsSelected
	loadSessionsPaneLayout      = state.GetSessionsPaneLayout
	saveSessionsPaneLayout      = state.SetSessionsPaneLayout
	loadSessionsHiddenHosts     = state.GetSessionsHiddenHosts
	saveSessionsHiddenHosts     = state.SetSessionsHiddenHosts
)

func New(collector workspaceinventory.Collector) *Model {
	collector = collector.WithDefaults()
	// Restored idle state is what lets a card say "idle 3h" instead of "25s"
	// on the first cycle after launch, and lets a turn that finished while
	// Sidecar was closed still land in the done lane.
	if path := ActivityStorePath(); path != "" {
		collector = collector.SeedTrackers(activitystore.Load(path, time.Now()))
	}
	m := &Model{collector: collector, results: make(map[string]workspaceinventory.ProjectResult), projectErrors: make(map[string]error), stale: make(map[string]bool), completed: make(map[int]bool), cards: make(map[string]workspaceinventory.Workspace), catalog: make(map[string]workspaceinventory.Workspace), mouse: mouse.NewHandler(), workspacesMouse: mouse.NewHandler(), viewFlyoutMouse: mouse.NewHandler(), renameMouse: mouse.NewHandler(), createMouse: mouse.NewHandler(), deleteMouse: mouse.NewHandler(), sidebarWidth: defaultWorkspaceSidebarPercent, sidebarVisible: true, showIdleWorktrees: loadShowIdleWorktrees(), previewOwnership: &previewOwnershipLease{}}
	m.preview.terminalPanes = termpanes.New()
	m.previewTerminalLeaf()
	if savedWidth := loadWorkspaceSidebarWidth(); savedWidth > 0 {
		m.sidebarWidth = savedWidth
	}
	m.applyWorkspacesEmptyState(0)
	m.workspaces.SetPinned(loadPinnedWorkspaceIDs())
	m.hiddenHosts = make(map[string]bool)
	for _, id := range loadSessionsHiddenHosts() {
		m.hiddenHosts[id] = true
	}
	// The chosen order is as much a part of "where I left off" as the pins and
	// the sidebar width beside it. Without this the list reshuffled itself on
	// every launch, which is the one moment a user is least able to tell a
	// reset apart from something having actually changed.
	if mode, ok := workspacelist.SortFromLabel(loadWorkspaceListSort(), workspacelist.SortModes); ok {
		m.workspaces.SetSort(mode)
	}
	// In-memory only: state.json is already loaded. Decoding pane trees waits
	// until a row is first shown.
	m.pendingRestoreSelected = loadSessionsSelected()
	if value := os.Getenv("SIDECAR_OVERVIEW_TRACE"); value == "1" || value == "stderr" {
		m.traceWriter = os.Stderr
	}
	return m
}

// SetConfig hands the global host the same app-owned configuration project
// plugins receive, without instantiating or temporarily switching a plugin.
func (m *Model) SetConfig(cfg *config.Config) { m.config = cfg }

// SyncHosts brings remote host connections in line with the current config.
// Returns the command that begins consuming their updates, or nil when the
// feature is off or nothing is registered.
func (m *Model) SyncHosts() tea.Cmd { return m.startHosts() }

// persistActivity writes committed trackers after a completed cycle. Failure
// is silent by design: the store is a convenience, and a state directory that
// cannot be written should not interrupt the board.
func (m *Model) persistActivity() {
	path := ActivityStorePath()
	if path == "" {
		return
	}
	_ = activitystore.Save(path, m.collector.TrackerSnapshot(), time.Now())
}

func (m *Model) Start(projects []Project) tea.Cmd {
	return m.start(projects, "refresh")
}

// Ensure starts collection only when the shared catalog has nothing live
// behind it. The Agents board and the global Workspaces list are two
// projections of one cache: whichever becomes visible first starts the cycle,
// and the other reuses its results, its trackers, and its poll. A second
// collector here would double every project's tmux and Git fan-out for a view
// that already has the data.
// Ensure starts collection when the shared catalog has nothing live behind it,
// and otherwise refreshes only when what it holds could actually be stale.
//
// Opening the tab is the gesture a user makes when they suspect the board is
// out of date, and the poll it would otherwise resume re-reads tmux evidence
// and no durable state at all — so a cold start, a changed project set, or a
// catalog whose last full inventory has aged out all re-read from disk.
//
// What must not trigger one is moving between the two global tabs. They are two
// projections of one catalog and deliberately do not stop collection when
// switching, so refreshing on every toggle would reintroduce exactly the
// duplicated fan-out that sharing the collector exists to avoid.
func (m *Model) Ensure(projects []Project) tea.Cmd {
	if m.loading {
		return nil
	}
	if m.cancel == nil || !sameConfiguredProjects(m.configuredPaths, projects) {
		return m.start(projects, "refresh")
	}
	if m.lastFullInventory.IsZero() || time.Since(m.lastFullInventory) >= inventorySweepEvery {
		return m.start(projects, "refresh")
	}
	return nil
}

func sameConfiguredProjects(paths []string, projects []Project) bool {
	if len(paths) != len(projects) {
		return false
	}
	for i, project := range projects {
		if paths[i] != project.Path {
			return false
		}
	}
	return true
}

// SetProjects applies a changed configured project set immediately, canceling
// any cycle that is still running so the change cannot be silently dropped.
// Ensure defers to an in-flight cycle, which is right for visibility gestures
// but wrong for membership changes: a project added or removed while the
// browser is live must win now, not after the next tab transition.
func (m *Model) SetProjects(projects []Project) tea.Cmd {
	if m.cancel != nil && sameConfiguredProjects(m.configuredPaths, projects) {
		return nil
	}
	return m.start(projects, "refresh")
}

func (m *Model) start(projects []Project, reason string) tea.Cmd {
	if m.cancel != nil {
		if m.pollScheduled {
			m.tracef("cycle generation=%d poll_cancel_requested", m.generation)
			m.pollScheduled = false
		}
		if m.loading || m.active > 0 {
			m.tracef("cycle generation=%d canceled active_projects=%d", m.generation, m.active)
		}
		m.cancel()
	}
	m.generation++
	m.requestID++
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.loading, m.tmuxErr = true, nil
	m.completed = make(map[int]bool)
	if reason != "poll" {
		// Manifest paths are resolved once and then reused, so a project that had
		// no state directory when they were resolved would never be watched — and
		// creating the very first shell in a project is what creates that
		// directory, which is exactly the case this watcher exists to catch. A
		// full cycle is infrequent and user-driven, so it is the right moment to
		// pay for a rescan. Until then such a project degrades to the sweep.
		m.invalidateShellManifestPaths()
	}
	// Keep last-good cards on screen during a poll/refresh cycle. Relative age
	// already communicates freshness; a full-board "refreshing…" rewrite was
	// too noisy on the 5s live poll.
	if len(m.projects) > 0 {
		m.syncBoard()
	}
	m.cycleStart, m.configured, m.firstResult, m.maxActive = time.Now(), len(projects), false, 0
	m.configuredPaths = m.configuredPaths[:0]
	for _, project := range projects {
		m.configuredPaths = append(m.configuredPaths, project.Path)
	}
	m.tracef("cycle generation=%d reason=%s configured=%d start", m.generation, reason, len(projects))
	generation := m.generation
	ctx := m.ctx
	configured := append([]Project(nil), projects...)
	return func() tea.Msg {
		liveOnly := reason == "poll"
		panes, err := m.collector.ListPanes(ctx)
		return panesMsg{Generation: generation, Projects: configured, Panes: panes, LiveOnly: liveOnly, Err: err}
	}
}

// StopHosts disconnects every remote host. It is separate from Stop because
// Stop runs whenever the tab is left, and a host connection must outlive that:
// reconnecting every machine each time a user switches tabs would be both slow
// and, on a flaky link, a source of rows that blink.
func (m *Model) StopHosts() { m.stopHosts() }

func (m *Model) Stop() {
	m.deactivatePreviewOwnership()
	m.pulseGeneration++
	m.pulseScheduled = false
	m.stopLiveWatchers()
	m.stopShellWatch()
	if m.cancel != nil {
		if m.pollScheduled {
			m.tracef("cycle generation=%d poll_cancel_requested", m.generation)
			m.pollScheduled = false
		}
		if m.loading || m.active > 0 {
			m.tracef("cycle generation=%d canceled active_projects=%d", m.generation, m.active)
		}
		m.cancel()
		m.cancel = nil
	}
	m.generation++
	m.requestID++
	m.loading = false
	// Stopping the cycle stops the preview with it: a tab nobody is looking at
	// has no reason to retain a pane's producer or memory-only output.
	m.preview.visible = false
	m.releasePreview()
}

// RequestNavigation binds a card activation to the current Overview lifecycle
// and supersedes any prior in-flight destination validation.
func (m *Model) RequestNavigation(workspace workspaceinventory.Workspace) tea.Cmd {
	return m.RequestNavigationAction(workspace, "")
}

func (m *Model) RequestNavigationAction(workspace workspaceinventory.Workspace, action string) tea.Cmd {
	// A remote workspace is observation only until Phase C gives the host
	// protocol a request channel. Navigating to one would resolve its path
	// against THIS machine's filesystem — which either fails confusingly or,
	// far worse, succeeds against an unrelated local directory that happens to
	// share the path. Refusing here covers every activation route into
	// navigation: the board, the list, and reveal.
	if workspace.Remote() {
		return m.refuseRemoteAction(workspace, "open")
	}
	m.requestID++
	msg := NavigateMsg{Workspace: workspace, Action: action, Generation: m.generation, RequestID: m.requestID}
	return func() tea.Msg { return msg }
}

// refuseRemoteAction reports a refusal as an ordinary validation failure, so
// every surface that already renders a failed navigation renders this too. The
// sentence is remoteActionRefusal's, not a second wording of the same rule.
func (m *Model) refuseRemoteAction(workspace workspaceinventory.Workspace, verb string) tea.Cmd {
	m.requestID++
	msg := ValidationMsg{
		Workspace:  workspace,
		Generation: m.generation,
		RequestID:  m.requestID,
		Err:        fmt.Errorf("%s", remoteActionRefusal(workspace, verb)),
	}
	return func() tea.Msg { return msg }
}

func (m *Model) IsCurrentNavigation(generation int, requestID uint64) bool {
	return generation == m.generation && requestID == m.requestID
}

// ConsumeValidation accepts a result at most once. A later duplicate or a
// result superseded by another activation cannot navigate.
func (m *Model) ConsumeValidation(generation int, requestID uint64) bool {
	if !m.IsCurrentNavigation(generation, requestID) {
		return false
	}
	m.requestID++
	return true
}

func (m *Model) Validate(msg NavigateMsg) tea.Cmd {
	return func() tea.Msg {
		return ValidationMsg{
			Workspace:  msg.Workspace,
			Action:     msg.Action,
			Generation: msg.Generation,
			RequestID:  msg.RequestID,
			Err:        m.collector.ValidateWorkspace(context.Background(), msg.Workspace),
		}
	}
}

// Update handles one message and, on the way out, keeps the working/blocked
// marker animation armed. Arming here rather than at a single entry point is
// what makes the pulse unconditional: whatever brought a live row on screen —
// a refresh, a filter keystroke, scrolling, opening the tab — re-checks the
// clock instead of leaving the row frozen until the next refresh.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	// Live-refresh messages are handled instead of the product update — a
	// watcher signal is not a gesture and must not be interpreted as one — but
	// they still fall through to the tail below, so queued pane geometry and the
	// pulse are not dropped just because a watcher happened to fire.
	cmd, handled := m.handleLiveWatchMsg(msg)
	if !handled {
		cmd, handled = m.handleShellWatchMsg(msg)
	}
	if !handled {
		cmd = m.update(msg)
	}
	// Geometry a pane content asserted from inside the last render, dispatched
	// on the first update after it. See paneHost.QueueSizeCmd.
	cmds := append([]tea.Cmd{cmd}, m.takePaneSizeCmds()...)
	// Swept once per update: preview panes open, retarget and close from too
	// many places to trust each of them to say so. See live_preview.go.
	if sync := m.reconcileLiveWatches(); sync != nil {
		cmds = append(cmds, sync)
	}
	// Swept alongside the preview watchers, and for the same reason: the
	// configured project set changes from more than one place.
	if sync := m.reconcileShellWatch(); sync != nil {
		cmds = append(cmds, sync)
	}
	if pulse := m.pulseCmd(); pulse != nil {
		cmds = append(cmds, pulse)
	}
	if tick := m.remoteDocumentRefreshCmd(); tick != nil {
		cmds = append(cmds, tick)
	}
	if tick := m.remoteResourceDescribeCmd(); tick != nil {
		cmds = append(cmds, tick)
	}
	if describe := m.ensureRemoteResourceDescribe(); describe != nil {
		cmds = append(cmds, describe)
	}
	if len(cmds) == 1 {
		return cmd
	}
	return tea.Batch(cmds...)
}

// workspacePulseTickMsg advances the shared marker animation by one frame.
type workspacePulseTickMsg struct{ generation uint64 }

func (m *Model) pulseCmd() tea.Cmd {
	if !m.preview.visible || m.pulseScheduled || !m.workspaces.NeedsPulse() {
		return nil
	}
	m.pulseScheduled = true
	generation := m.pulseGeneration
	return tea.Tick(workspacelist.PulseInterval, func(time.Time) tea.Msg {
		return workspacePulseTickMsg{generation: generation}
	})
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case termpreview.HostBackgroundMsg:
		m.terminalDefaultBackground = msg.ANSI
		return nil
	case appmsg.ThemeChangedMsg:
		// listItem contains already-styled metadata, so a palette change must
		// rebuild the projection rather than merely dropping the rendered string.
		m.workspaceListThemeRevision++
		m.syncWorkspaces()
		return nil
	case previewDocLinkResolvedMsg:
		m.applyPreviewDocLinkResolved(msg)
		return nil
	case previewLinkRevalidatedMsg:
		return m.applyPreviewLinkRevalidated(msg)
	case workspacePulseTickMsg:
		if msg.generation != m.pulseGeneration {
			return nil
		}
		m.pulseScheduled = false
		m.pulseFrame++
		m.workspaces.SetPulseFrame(m.pulseFrame)
		return nil
	case sessionsSelectedTickMsg:
		m.applySessionsSelectedTick(msg)
		return nil
	case panesMsg:
		if msg.Generation != m.generation {
			return nil
		}
		m.tmuxErr = msg.Err
		m.currentPanes = append(m.currentPanes[:0], msg.Panes...)
		m.liveOnly = msg.LiveOnly
		m.completed = make(map[int]bool, len(msg.Projects))
		m.active = 0
		if msg.LiveOnly {
			m.phase = phaseStatus
			m.pending = indexedProjects(msg.Projects)
			// Membership can change between full cycles — a shell created from
			// this surface, or one another instance wrote to shells.json.
			// Rebuilding from the results we already hold is what lets the
			// live-only pass recognise those sessions instead of marking them
			// orphaned (td-ecb0b8).
			m.syncShellClaims()
			m.refreshCollector = m.collector.ForRefresh(maxCaptures, m.shellClaims)
		} else {
			m.phase = phaseIdentity
			m.pending = indexedProjects(msg.Projects)
			m.pendingInventory = nil
			m.identityProjects = make(map[int]Project, len(msg.Projects))
			m.inventoryOrder = nil
			m.inventoryScheduled = make(map[string]bool, len(msg.Projects))
			m.inventoryProjects = make(map[string]Project, len(msg.Projects))
			m.inventoryResults = make(map[string]workspaceinventory.ProjectResult, len(msg.Projects))
			m.refreshCollector = m.collector.ForRefresh(maxCaptures)
		}
		m.tracef("cycle generation=%d configured=%d tmux_inventories=1 phase=%s", m.generation, m.configured, m.phase)
		if len(m.pending) == 0 {
			return m.finishPhase()
		}
		return m.dispatchProjects()
	case projectMsg:
		if msg.Generation != m.generation {
			m.tracef("cycle generation=%d drained stale_generation=%d", m.generation, msg.Generation)
			return nil
		}
		if m.active > 0 {
			m.active--
		}
		if msg.Phase != phaseIdentity && !m.firstResult {
			m.firstResult = true
			m.tracef("cycle generation=%d first_result_ms=%d", m.generation, time.Since(m.cycleStart).Milliseconds())
		}
		m.completed[msg.Project.Index] = true
		switch msg.Phase {
		case phaseIdentity:
			m.identityProjects[msg.Project.Index] = msg.Project
			if !m.inventoryScheduled[msg.Project.Key] {
				m.inventoryScheduled[msg.Project.Key] = true
				m.pendingInventory = append(m.pendingInventory, msg.Project)
			}
		case phaseInventory:
			m.inventoryProjects[msg.Project.Key] = msg.Project
			m.inventoryResults[msg.Project.Key] = msg.Result
			m.applyInventoryIncrement(msg.Project, msg.Result)
		default:
			m.applyStatusResult(msg.Result)
		}
		m.syncBoard()
		// The list's selection can move when incremental results arrive (the
		// first result selects a row at all), so the preview follows it here
		// rather than waiting for the user to press a key.
		preview := m.previewSync()
		if len(m.pendingInventory) > 0 || len(m.pending) > 0 || m.active > 0 {
			return tea.Batch(m.dispatchProjects(), preview)
		}
		return tea.Batch(m.finishPhase(), preview)
	case previewAutoScrollTickMsg:
		return m.advancePreviewAutoScroll(msg)
	case inlineedit.StartedMsg:
		return m.applyPreviewDocEditStarted(msg)
	case inlineedit.ExitedMsg:
		return m.applyPreviewDocEditExited(msg)
	case previewDocLoadedMsg:
		m.applyPreviewDocLoaded(msg)
		return nil
	case contentpanes.Result:
		return m.applyPreviewDeckResult(msg)
	case previewDocSearchMsg:
		return m.applyPreviewDocSearchMsg(msg)
	case previewIssueLoadedMsg:
		m.applyPreviewIssueLoaded(msg)
		return nil
	case previewNoteLoadedMsg:
		m.applyPreviewNoteLoaded(msg)
		return nil
	case previewResourceResolvedMsg:
		m.applyPreviewResourceResolved(msg)
		return nil
	case resourceview.OpenRowMsg:
		// Enter on a collection row. It travels as a message rather than a
		// direct call so the row opens through this surface's own open journey,
		// which is what focuses a tab that is already there instead of fetching
		// it twice. Its twin is in internal/plugins/workspace.
		return m.OpenPreviewResource(msg.Ref)
	case pluginbrowser.ListedMsg, pluginbrowser.GotMsg, pluginbrowser.ActedMsg,
		pluginbrowser.DescribedMsg, pluginbrowser.QueryDebouncedMsg, pluginbrowser.ChangedMsg:
		// A collection or row tab's own answers, delivered as a broadcast each
		// viewer either owns or does not, so a page for a tab that has closed
		// lands nowhere rather than in the wrong pane.
		return m.applyPluginBrowserMsg(msg)
	case previewHistoryLoadedMsg:
		return m.applyPreviewHistory(msg)
	case previewTerminalSearchLoadedMsg:
		return m.applyPreviewTerminalSearchHistory(msg)
	case workspacediff.SnapshotMsg:
		return m.applyDiffSnapshot(msg)
	case workspacediff.CommitDetailMsg:
		m.applyCommitDetail(msg)
		return nil
	case workspacediff.RangeMsg:
		return m.applyPreviewDiffRange(msg)
	case workspacediff.CommitFileDiffMsg:
		cmd := m.diff.ApplyCommitFileDiff(msg)
		return tea.Batch(cmd, m.applyPreviewDiffFile(msg))
	case workspacediff.WorkingTreeFileMsg:
		return m.applyPreviewDiffWorkingTreeFile(msg)
	case renameShellDoneMsg:
		m.applyRenameShell(msg)
		return nil
	case globalCreateBranchesMsg:
		m.applyCreateBranches(msg)
		return nil
	case createPickerDataMsg:
		applyPickerData(m.createForm, msg)
		return nil
	case createHostCatalogMsg:
		m.applyCreateHostCatalog(msg)
		return nil
	case workspacecreate.FilesScannedMsg:
		m.applyCreateFileCandidates(msg)
		return nil
	case globalShellCreatedMsg:
		if m.hostReplyStale(msg.HostID, msg.Incarnation) {
			return m.dropRemoteCreateReply(msg.HostID)
		}
		m.createBusy = false
		if msg.Err != nil {
			m.createModal = nil
			m.setCreateError(remoteActionError(msg.Err))
			m.clearPendingCreated()
			return nil
		}
		if msg.HostID != "" {
			// The row arrives with that host's next snapshot. Nothing is
			// synthesized here, and no local inventory is taken: a local
			// refresh would answer a question about another machine.
			m.pendingCreatedTmux = msg.Tmux
			m.pendingCreatedPath = ""
			m.pendingCreatedHost = msg.HostID
			m.closeCreateShell()
			return nil
		}
		m.closeCreateShell()
		return m.refreshProjectAfterMutation(msg.Project)
	case previewTerminalSplitCreatedMsg:
		return m.applyPreviewTerminalSplitCreated(msg)
	case previewSplitSeedFailedMsg:
		if msg.Err != nil {
			m.setCreateError(msg.Err.Error())
		}
		return nil
	case previewSplitCloseProbeMsg:
		return m.applyPreviewSplitCloseProbe(msg)
	case globalWorktreePlannedMsg:
		if m.hostReplyStale(msg.HostID, msg.Incarnation) {
			return m.dropRemoteCreateReply(msg.HostID)
		}
		m.createBusy = false
		if msg.Err != nil {
			m.createModal = nil
			m.setCreateError(remoteActionError(msg.Err))
			return nil
		}
		m.createPlan = msg.Plan
		m.createModal = nil
		return nil
	case globalWorktreeCreatedMsg:
		if m.hostReplyStale(msg.HostID, msg.Incarnation) {
			return m.dropRemoteCreateReply(msg.HostID)
		}
		if msg.HostID != "" {
			return m.applyRemoteWorktreeCreated(msg)
		}
		m.createBusy = false
		m.createPlan, m.createRecord = msg.Plan, msg.Record
		if msg.Record == nil {
			m.createError = "Worktree creation failed"
			if msg.Err != nil {
				m.createError = msg.Err.Error()
			}
			m.createModal = nil
			return nil
		}
		if msg.Err != nil {
			m.createError = msg.Err.Error()
			if failed := failedCreateOutcomes(msg.Outcomes, false); len(failed) > 0 {
				m.createError += "; " + summarizeCreateOutcomes(failed)
			}
			m.createModal = nil
			return nil
		}
		if failed := failedCreateOutcomes(msg.Outcomes, true); len(failed) > 0 {
			m.createError = summarizeCreateOutcomes(failed)
			m.createModal = nil
			return nil
		}
		if failed := failedCreateOutcomes(msg.Outcomes, false); len(failed) > 0 {
			m.createWarning = summarizeCreateOutcomes(failed)
			m.createModal = nil
			return nil
		}
		if err := removeGlobalJournal(msg.Plan); err != nil {
			m.createError = "finalize pending creation journal: " + err.Error()
			m.createModal = nil
			return nil
		}
		return m.launchCreatedWorktree(msg.Project, msg.Plan, msg.Record)
	case globalWorkspaceLaunchedMsg:
		m.createBusy = false
		m.createPlan, m.createRecord = msg.Plan, msg.Record
		if msg.Err != nil {
			m.createError = msg.Err.Error()
			m.createModal = nil
			return nil
		}
		m.pendingCreatedPath = msg.Record.Path
		m.pendingCreatedTmux = ""
		// Local launch: SET the machine, do not leave whatever the last remote
		// create put there. This is the field's own rule (see the declaration)
		// and it was obeyed on one activation path in four — a browser that had
		// created remotely once then failed to select anything it created here.
		m.pendingCreatedHost = ""
		m.revealIdleWorktree(msg.Record.Path, "")
		m.closeCreateShell()
		return m.refreshProjectAfterMutation(msg.Project)
	case globalWorktreeDeletedMsg:
		m.createBusy = false
		if msg.Err != nil {
			m.createError = msg.Err.Error()
			m.createModal = nil
			return nil
		}
		m.closeCreateShell()
		return m.refreshProjectAfterMutation(msg.Project)
	case globalWorktreeDeleteProbeMsg:
		return m.applyWorktreeDeleteProbe(msg)
	case globalWorktreeDeleteDoneMsg:
		return m.applyWorktreeDeleteDone(msg)
	case globalShellDeletedMsg:
		if m.hostReplyStale(msg.HostID, msg.Incarnation) {
			return m.dropRemoteDeleteReply(msg)
		}
		m.deleteBusy = false
		if msg.Err != nil {
			m.deleteError = msg.Err.Error()
			m.deleteModal = nil
			return nil
		}
		m.forgetSessionsRow(msg.WorkspaceID)
		m.closeDelete()
		return m.refreshProjectAfterMutation(msg.Project)
	case projectMutationRefreshMsg:
		return m.applyProjectMutationRefresh(msg)
	case shellProbedMsg:
		return m.applyShellProbe(msg)
	case shellForgottenMsg:
		return m.applyShellForgotten(msg)
	case uirequest.RequestMsg:
		return m.handleUIRequest(msg.Request)
	case hostUpdateMsg:
		return m.handleHostUpdate(msg)
	case hostStaleTickMsg:
		return m.handleHostStaleTick(msg)
	case pollMsg:
		if msg.Generation != m.generation || m.ctx == nil {
			m.tracef("cycle generation=%d poll_drained stale_generation=%d", m.generation, msg.Generation)
			return nil
		}
		m.pollScheduled = false
		return m.start(m.projects, "poll")
	case tea.KeyPressMsg:
		switch msg.String() {
		case "left", "h":
			m.board.MoveColumn(-1)
		case "right", "l":
			m.board.MoveColumn(1)
		case "up", "k":
			m.board.MoveRow(-1)
		case "down", "j":
			m.board.MoveRow(1)
		case "enter":
			return m.activate()
		case "r":
			return m.Start(m.projects)
		}
	case tea.MouseMsg:
		wasDragging := m.mouse.IsDragging()
		action := m.mouse.HandleMouse(msg)
		if action.Type == mouse.ActionHover && wasDragging && !m.mouse.IsDragging() {
			// A release lost off-window or behind focus change: the shared
			// handler cancels the stale drag on this first button-less motion,
			// and a live lane-bar gesture settles with it at the same boundary.
			m.board.ReleaseScrollbar()
			return nil
		}
		// A drag and its release belong to the region they started in — the
		// grabbed lane's bar — wherever the pointer has since travelled,
		// including nowhere the board drew at all.
		if isBoardScrollbarDragID(action.DragStartID) {
			switch action.Type {
			case mouse.ActionDrag:
				m.board.DragScrollbar(action.Y)
			case mouse.ActionDragEnd:
				m.board.ReleaseScrollbar()
			}
			return nil
		}
		if action.Region == nil {
			return nil
		}
		region, ok := action.Region.Data.(kanban.HitRegion)
		if !ok {
			return nil
		}
		switch action.Type {
		case mouse.ActionClick:
			if isBoardBarRegion(region) {
				m.pressBoardScrollbar(region, action)
				break
			}
			m.board.HandlePointer(kanban.PointerClick, region)
		case mouse.ActionDoubleClick:
			// Double-press parity: a rapid second press on a bar re-grabs it
			// exactly like the first one did instead of activating a card.
			if isBoardBarRegion(region) {
				m.pressBoardScrollbar(region, action)
				break
			}
			if m.board.HandlePointer(kanban.PointerDoubleClick, region).Kind == kanban.ActionActivated {
				return m.activate()
			}
		case mouse.ActionHover:
			m.board.HandlePointer(kanban.PointerHover, region)
		case mouse.ActionScrollUp, mouse.ActionScrollDown:
			delta := action.Delta
			if delta == 0 {
				if action.Type == mouse.ActionScrollUp {
					delta = -1
				} else {
					delta = 1
				}
			}
			m.board.MoveInColumn(region.Column, delta)
		}
	}
	return nil
}

func (m *Model) dispatchProjects() tea.Cmd {
	cmds := make([]tea.Cmd, 0, maxProjects)
	for m.active < maxProjects && (len(m.pendingInventory) > 0 || len(m.pending) > 0) {
		phase := m.phase
		var project Project
		if m.phase == phaseIdentity && len(m.pendingInventory) > 0 {
			project = m.pendingInventory[0]
			m.pendingInventory = m.pendingInventory[1:]
			phase = phaseInventory
		} else {
			project = m.pending[0]
			m.pending = m.pending[1:]
		}
		m.active++
		m.maxActive = max(m.maxActive, m.active)
		generation, ctx := m.generation, m.ctx
		roots := append([]string(nil), m.roots...)
		inventory := append([]workspaceinventory.Pane(nil), m.currentPanes...)
		collector := m.refreshCollector
		previous := m.results[projectKey(project)]
		if !m.liveOnly && phase == phaseStatus {
			previous = m.statusInputs[projectKey(project)]
		}
		cmds = append(cmds, func() tea.Msg {
			if phase == phaseIdentity {
				project = normalizeProject(project)
				return projectMsg{Generation: generation, Project: project, Phase: phase, Result: workspaceinventory.ProjectResult{ProjectKey: project.Key, ProjectName: project.Name, ProjectRoot: project.Path}}
			}
			if phase == phaseInventory {
				return projectMsg{Generation: generation, Project: project, Phase: phase, Result: collector.CollectProjectInventory(ctx, project.Name, project.Path)}
			}
			return projectMsg{Generation: generation, Project: project, Phase: phase, Result: collector.RefreshProjectStatus(ctx, previous, roots, inventory)}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) finishPhase() tea.Cmd {
	if m.phase == phaseIdentity {
		seen := make(map[string]bool, len(m.identityProjects))
		m.inventoryOrder = make([]Project, 0, len(m.identityProjects))
		for index := 0; index < m.configured; index++ {
			project, ok := m.identityProjects[index]
			if !ok || seen[project.Key] {
				continue
			}
			seen[project.Key] = true
			project.Index = len(m.inventoryOrder)
			m.inventoryOrder = append(m.inventoryOrder, project)
		}
		m.tracef("cycle generation=%d identities=%d unique=%d inventory_complete", m.generation, m.configured, len(m.inventoryOrder))
		m.phase = phaseInventory
	}
	if m.phase == phaseInventory {
		seen := make(map[string]bool, len(m.inventoryResults))
		projects := make([]Project, 0, len(m.inventoryResults))
		claimResults := make([]workspaceinventory.ProjectResult, 0, len(m.inventoryResults))
		m.statusInputs = make(map[string]workspaceinventory.ProjectResult, len(m.inventoryResults))
		for _, ordered := range m.inventoryOrder {
			if _, ok := m.inventoryProjects[ordered.Key]; !ok {
				continue
			}
			project := ordered
			result := withProjectIdentity(m.inventoryResults[ordered.Key], project)
			if seen[result.ProjectKey] {
				continue
			}
			seen[result.ProjectKey] = true
			projects = append(projects, project)
			m.statusInputs[result.ProjectKey] = result
			if previous, ok := m.results[result.ProjectKey]; ok {
				m.results[result.ProjectKey] = withProjectIdentity(previous, project)
			}
			claimResult := result
			if result.Err != nil {
				if previous, ok := m.results[result.ProjectKey]; ok && len(previous.Workspaces) > 0 {
					claimResult = previous
				}
			}
			claimResults = append(claimResults, claimResult)
		}
		m.projects = projects
		m.roots = m.roots[:0]
		for _, project := range projects {
			m.roots = append(m.roots, project.Path)
		}
		for key := range m.results {
			if !seen[key] {
				delete(m.results, key)
				delete(m.projectErrors, key)
				delete(m.stale, key)
			}
		}
		m.shellClaims = workspaceinventory.BuildShellClaims(claimResults)
		m.refreshCollector = m.refreshCollector.WithShellClaims(m.shellClaims)
		m.phase = phaseStatus
		m.pending = append(m.pending[:0], projects...)
		m.completed = make(map[int]bool, len(projects))
		m.active = 0
		m.tracef("cycle generation=%d deduped=%d phase=status", m.generation, len(projects))
		m.syncBoard()
		if len(m.pending) > 0 {
			return m.dispatchProjects()
		}
	}
	m.loading = false
	if !m.liveOnly {
		m.lastFullInventory = time.Now()
	}
	m.refreshCollector.CommitTrackers()
	m.persistActivity()
	metrics := m.refreshCollector.Metrics()
	m.tracef("cycle generation=%d complete_ms=%d project_ops=%d captures=%d max_project_concurrency=%d max_capture_concurrency=%d", m.generation, time.Since(m.cycleStart).Milliseconds(), metrics.ProjectOps, metrics.Captures, m.maxActive, metrics.MaxCaptures)
	// A completed cycle is the one moment this surface holds a coherent tmux
	// inventory beside the shells the manifests claim, which is exactly what
	// deciding a shell has died requires (td-6a4100).
	reap := m.reapDeadShells()
	m.syncBoard()
	return tea.Batch(m.pollCmd(), reap, m.sweepCmd())
}

func (m *Model) applyInventoryIncrement(project Project, result workspaceinventory.ProjectResult) {
	key := result.ProjectKey
	if !containsProject(m.projects, key) {
		m.projects = append(m.projects, project)
	}
	if result.Err != nil {
		m.applyFailure(key, result, result.Err)
		return
	}
	if previous, ok := m.results[key]; !ok || previous.Err != nil {
		m.results[key] = result
	}
}

func withProjectIdentity(result workspaceinventory.ProjectResult, project Project) workspaceinventory.ProjectResult {
	result.ProjectKey = project.Key
	result.ProjectName = project.Name
	result.ProjectRoot = project.Path
	workspaces := append([]workspaceinventory.Workspace(nil), result.Workspaces...)
	for i := range workspaces {
		workspaces[i].ProjectKey = project.Key
		workspaces[i].ProjectName = project.Name
		workspaces[i].ProjectRoot = project.Path
	}
	result.Workspaces = workspaces
	return result
}

// markInventoryFresh records that this project's durable state was just read
// from disk. See Model.inventoryStamp.
func (m *Model) markInventoryFresh(key string) {
	if m.inventoryStamp == nil {
		m.inventoryStamp = make(map[string]time.Time)
	}
	m.inventoryStamp[key] = time.Now()
}

func (m *Model) applyStatusResult(result workspaceinventory.ProjectResult) {
	key := result.ProjectKey
	if m.tmuxErr != nil {
		m.applyFailure(key, result, m.tmuxErr)
		return
	}
	if result.Err != nil {
		m.applyFailure(key, result, result.Err)
		return
	}
	if !m.liveOnly {
		// A full cycle's status pass carries the inventory phase's fresh read.
		m.markInventoryFresh(key)
	} else if !m.cycleStart.IsZero() {
		// Live-only status is a liveness pass over the membership this cycle
		// snapshotted. A mutation or watcher that re-read the project after
		// that snapshot already holds newer membership; applying this result
		// would drop a shell created a moment ago, or restore one just
		// deleted (td-ecb0b8).
		if stamped, ok := m.inventoryStamp[key]; ok && stamped.After(m.cycleStart) {
			m.tracef("live-only status project=%s skipped — inventory newer than cycle", key)
			return
		}
	}
	m.results[key] = result
	delete(m.projectErrors, key)
	delete(m.stale, key)
}

// syncShellClaims rebuilds the reserved-session map from the results we
// currently hold. Call it whenever membership changes outside a full inventory
// phase — create, delete, a watcher refresh — so the next live-only poll can
// still find those sessions.
func (m *Model) syncShellClaims() {
	inputs := make([]workspaceinventory.ProjectResult, 0, len(m.results))
	for _, result := range m.results {
		inputs = append(inputs, result)
	}
	m.shellClaims = workspaceinventory.BuildShellClaims(inputs)
	m.refreshCollector = m.refreshCollector.WithShellClaims(m.shellClaims)
}

func (m *Model) applyFailure(key string, result workspaceinventory.ProjectResult, err error) {
	if previous, ok := m.results[key]; ok && previous.Err == nil {
		for i := range previous.Workspaces {
			previous.Workspaces[i].Presentation = stalePresentation(previous.Workspaces[i].Presentation)
		}
		m.results[key] = previous
		m.stale[key] = true
	} else {
		m.results[key] = result
		m.stale[key] = false
	}
	m.projectErrors[key] = err
}

func stalePresentation(p agentstatus.Presentation) agentstatus.Presentation {
	p.Freshness = agentstatus.FreshnessStale
	p.Attention = false
	p.Lane = agentstatus.LanePaused
	p.Label = "stale"
	p.Icon = "?"
	return p
}

func indexedProjects(projects []Project) []Project {
	indexed := append([]Project(nil), projects...)
	for i := range indexed {
		indexed[i].Index = i
	}
	return indexed
}

func containsProject(projects []Project, key string) bool {
	for _, project := range projects {
		if project.Key == key {
			return true
		}
	}
	return false
}

func (m *Model) tracef(format string, args ...any) {
	if m.traceWriter != nil {
		_, _ = fmt.Fprintf(m.traceWriter, "overview "+format+"\n", args...)
	}
}

func (m *Model) pollCmd() tea.Cmd {
	m.pollScheduled = true
	generation, ctx, delay := m.generation, m.ctx, m.pollInterval()
	return func() tea.Msg {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			return pollMsg{Generation: generation}
		case <-ctx.Done():
			return pollMsg{Generation: generation}
		}
	}
}

// pollInterval trades refresh cost against how long a stale badge can persist.
// Working and blocked panes change often enough to want the live cadence. An
// idle or done pane is not inert, though: it is an agent sitting at a prompt
// that can start a turn at any moment, and polling those at the quiet cadence
// meant an all-idle board could take a full idlePollEvery to notice work had
// begun. Only a board with nothing live at all earns the quiet cadence.
func (m *Model) pollInterval() time.Duration {
	interval := idlePollEvery
	for _, result := range m.results {
		for _, workspace := range result.Workspaces {
			switch workspace.Presentation.Lane {
			case agentstatus.LaneWorking, agentstatus.LaneBlocked:
				return livePollEvery
			case agentstatus.LaneIdle, agentstatus.LaneDone:
				interval = readyPollEvery
			}
		}
	}
	return interval
}

func (m *Model) activate() tea.Cmd {
	card, ok := m.board.Board().CardAt(m.board.Selection())
	if !ok {
		return nil
	}
	workspace, ok := m.cards[card.ID]
	if !ok {
		return nil
	}
	return m.RequestReveal(workspace)
}

// RequestReveal asks the host to open the selected card in the global
// Workspaces browser. Bumping the request ID supersedes any activation still
// being validated, exactly as a navigation request does.
func (m *Model) RequestReveal(workspace workspaceinventory.Workspace) tea.Cmd {
	m.requestID++
	return func() tea.Msg { return RevealMsg{Workspace: workspace} }
}

// RevealWorkspace selects a workspace in the global Workspaces list. It is the
// host's half of a RevealMsg and runs before the tab becomes visible, so the
// preview binds to the revealed row rather than the previously selected one.
func (m *Model) RevealWorkspace(workspace workspaceinventory.Workspace) tea.Cmd {
	if workspace.ID == "" {
		return nil
	}
	cmds := []tea.Cmd{m.focusList()}
	if m.workspaces.SelectID(workspace.ID) {
		return tea.Batch(cmds...)
	}

	// An idle worktree the list is currently hiding is still a row the user
	// just asked for; show the hidden rows rather than silently landing on
	// someone else's selection. The choice is persisted like every other way of
	// making it, so the fly-out's checkbox tells the truth after a restart.
	if !m.showIdleWorktrees {
		m.showIdleWorktrees = true
		cmds = append(cmds, m.persistIdleAndSync())
		if m.workspaces.SelectID(workspace.ID) {
			return tea.Batch(cmds...)
		}
	}

	// A narrowing query is the other reason the row is not on screen. Landing
	// silently on somebody else's row is the dangerous outcome — D acts on the
	// selection — so the query goes rather than the request.
	if m.workspaces.Filter().Active() {
		m.workspaces.Filter().Reset()
		m.workspaces.Reproject()
		if m.workspaces.SelectID(workspace.ID) {
			return tea.Batch(append(cmds, m.previewSync())...)
		}
	}

	// The row is genuinely not here any more. Say so rather than leaving the
	// previous selection looking like the answer.
	name := workspace.Name
	if name == "" {
		name = workspace.ID
	}
	return tea.Batch(append(cmds, appmsg.Alert(notify.SourceSession, notify.SeverityWarning, name+" is no longer in the catalog"))...)
}

func (m *Model) View(width, height int) string {
	m.width, m.height = width, height
	result := m.board.Render(kanban.RenderOptions{Width: width, Height: height, Header: "Agent Overview", HeaderRight: m.summary(), MinColumnWidth: minColumnWidth, CardHeight: cardHeight})
	m.mouse.Clear()
	if result.Compact {
		return m.renderCompact(width, height)
	}
	for _, region := range result.Regions {
		// Lane bars keep the shared renderer's region IDs so a press can be
		// told from a card at hit-test time; they are emitted after every
		// card region, so the reverse scan gives them the column they share.
		id := "overview-card"
		if region.Kind == kanban.RegionScrollbarThumb || region.Kind == kanban.RegionScrollbarTrack {
			id = string(region.Kind)
		}
		m.mouse.HitMap.AddRect(id, region.X, region.Y, region.W, region.H, region)
	}
	return result.View
}

// summary is the header-right text. Refreshes are frequent and mostly
// instantaneous, so a "Loading n/m" counter there reads as flicker and pulls
// the eye for nothing. Loading is not an abnormal state: while it runs we keep
// showing the last known counts (nothing at all on the very first load), and
// only genuinely abnormal state — tmux being unavailable — replaces them.
func (m *Model) summary() string {
	if m.tmuxErr != nil {
		return "tmux unavailable"
	}
	if m.loading && len(m.results) == 0 {
		return ""
	}
	return fmt.Sprintf("%d projects · %d agents", len(m.results), m.agentCount)
}

// cardOrder is the sort key syncBoard attaches to every card it builds:
// project group in configured project order, then most-recent-first within
// the group. Error cards carry a zero ChangedAt, which sorts them last
// within their project group.
type cardOrder struct {
	project   int
	changedAt time.Time
}

// boardLane is a shared lane as this board draws it: the theme's lane colours,
// and CellReady as a sentinel meaning neither loading nor errored (syncBoard
// converts card-less ready lanes to CellEmpty). Loading and error states are
// set over it per refresh.
func boardLane(lane agentstatus.LaneID) kanban.Lane {
	built := kanban.AgentLane(lane, kanban.ThemeLanePalette)
	built.State = kanban.CellReady
	return built
}

func (m *Model) syncBoard() {
	// The lanes are the shared definition's — the project board draws the same
	// ones — in this board's own colours and cell state. The count is left to
	// the Kanban component, which appends its own.
	lanes := []kanban.Lane{
		boardLane(agentstatus.LaneWorking),
		boardLane(agentstatus.LaneBlocked),
		boardLane(agentstatus.LaneDone),
		boardLane(agentstatus.LaneIdle),
		boardLane(agentstatus.LanePaused),
	}
	m.cards = make(map[string]workspaceinventory.Workspace)
	order := make(map[string]cardOrder)
	now := time.Now()
	// Remote agents share the lanes with local ones. "Is anything blocked?" is
	// a question about every machine at once, so a remote blocked agent
	// belongs in the same column as a local one rather than in a section of
	// its own that a reader has to remember to look at.
	remoteBase := len(m.projects)
	m.eachHostWorkspace(func(ordinal int, label string, workspace workspaceinventory.Workspace, stale bool, shown bool) {
		// The board is a visible projection, like the list: a hidden machine's
		// agents leave the lanes with it.
		if !shown || !workspace.HasAgent() {
			return
		}
		m.cards[workspace.ID] = workspace
		order[workspace.ID] = cardOrder{project: remoteBase + ordinal, changedAt: workspace.Presentation.ChangedAt}
		card := kanban.Card{ID: workspace.ID, Lines: cardLines(workspace, stale, now)}
		for i := range lanes {
			if lanes[i].ID == kanban.LaneID(workspace.Presentation.Lane) {
				lanes[i].Cards = append(lanes[i].Cards, card)
				break
			}
		}
	})
	for i, project := range m.projects {
		key := projectKey(project)
		result, loaded := m.results[key]
		if !loaded {
			if m.loading {
				lanes[4].State, lanes[4].Message = kanban.CellLoading, "Loading "+project.Name+"…"
			}
			continue
		}
		if result.Err != nil && len(result.Workspaces) == 0 {
			id := "error:" + key
			order[id] = cardOrder{project: i}
			card := kanban.Card{ID: id, Lines: errorCardLines(project.Name, result.Err)}
			lanes[4].Cards = append(lanes[4].Cards, card)
			continue
		}
		for _, workspace := range result.Workspaces {
			// The board is the agent-only projection of the shared catalog.
			// Untyped shell definitions are live-discovery candidates, and plain
			// worktrees have no agent semantics at all; both belong to the
			// Workspaces list, not to a Kanban lane.
			if !workspace.HasAgent() {
				continue
			}
			m.cards[workspace.ID] = workspace
			order[workspace.ID] = cardOrder{project: i, changedAt: workspace.Presentation.ChangedAt}
			card := kanban.Card{ID: workspace.ID, Lines: cardLines(workspace, m.stale[key], now)}
			for i := range lanes {
				if lanes[i].ID == kanban.LaneID(workspace.Presentation.Lane) {
					lanes[i].Cards = append(lanes[i].Cards, card)
					break
				}
			}
		}
	}
	m.agentCount = len(m.cards)
	for i := range lanes {
		sort.SliceStable(lanes[i].Cards, func(a, b int) bool {
			left, right := order[lanes[i].Cards[a].ID], order[lanes[i].Cards[b].ID]
			if left.project != right.project {
				return left.project < right.project
			}
			return left.changedAt.After(right.changedAt)
		})
	}
	for i := range lanes {
		if len(lanes[i].Cards) == 0 && lanes[i].State == kanban.CellReady {
			lanes[i].State = kanban.CellEmpty
		}
	}
	m.board.SetBoard(kanban.Board{Lanes: lanes})
	// One collection, two projections: the list is rebuilt from the same
	// results map, in the same pass, so the tabs cannot disagree.
	m.syncWorkspaces()
	m.honorPendingCreated()
}

// spineGlyph is the per-kind left accent every content line carries: solid
// for a worktree, hairline for a shell. Redundant with kindGlyph on purpose —
// colourblind-safe.
func spineGlyph(kind workspaceinventory.Kind) string {
	if kind == workspaceinventory.KindShell {
		return "▏"
	}
	return "▌"
}

func kindGlyph(kind workspaceinventory.Kind) string {
	if kind == workspaceinventory.KindShell {
		return workspacelist.KindGlyph(workspacelist.KindShell)
	}
	return workspacelist.KindGlyph(workspacelist.KindWorktree)
}

// cardLines builds the three styled content rows for a live workspace card.
// stale reflects the owning project's freshness tracker; abnormal
// Presentation.Freshness (e.g. "unavailable") falls through to a plain word
// when the tracker does not apply. Mid-cycle polls no longer rewrite cards
// with a "refreshing…" flash — relative age already communicates freshness.
func cardLines(workspace workspaceinventory.Workspace, stale bool, now time.Time) []kanban.Line {
	hue := styles.ProjectHue(workspace.ProjectKey)
	spine := spineGlyph(workspace.Kind)
	dormant := isDormant(workspace.Presentation, now)
	nameColor := styles.TextPrimary
	if dormant {
		nameColor = styles.TextMuted
	}
	line1 := kanban.Line{Spans: []kanban.Span{{Text: spine, Foreground: hue}}}
	// The board's half of the same provenance the Sessions row carries: the
	// shared glyph, in the shared per-host colour. It goes on line one because
	// that is the line a narrow lane still draws in full at its head, and it is
	// a glyph rather than the host's name because a card is as narrow as 16
	// columns and the name would be spent before the workspace's own. The name
	// itself lands on line three below, where there is room for it.
	if workspace.Remote() {
		hostHue := workspacelist.HostHue(workspace.HostID)
		line1.Spans = append(line1.Spans, kanban.Span{Text: " " + workspacelist.HostGlyph, Foreground: hostHue})
	}
	line1.Spans = append(line1.Spans,
		kanban.Span{Text: " " + workspace.ProjectName, Foreground: hue, Bold: true},
		kanban.Span{Text: " " + kindGlyph(workspace.Kind), Foreground: styles.TextMuted},
		kanban.Span{Text: " " + workspace.Name, Foreground: nameColor},
	)

	status := workspace.Presentation.Label
	if workspace.Presentation.Attention {
		status = "▲ " + status
	}
	// "~" marks an idle this provider inferred from silence rather than read
	// from a completion marker, so a card that never reaches done reads as a
	// known limitation instead of a missing signal.
	if workspace.Presentation.Inferred {
		status += " ~"
	}
	if age := relativeAge(workspace.Presentation.ChangedAt, now); age != "" {
		status += " · " + age
	}
	statusColor := styles.LaneColor(string(workspace.Presentation.Lane))
	if dormant {
		statusColor = styles.TextMuted
	}
	line2 := kanban.Line{Spans: []kanban.Span{
		{Text: spine, Foreground: hue},
		{Text: " " + styles.AgentLabel(workspace.Provider), Foreground: styles.AgentColor(workspace.Provider), Background: styles.AgentChipFill()},
		{Text: " " + status, Foreground: statusColor, Bold: workspace.Presentation.Lane == agentstatus.LaneDone},
	}}

	// A shell has neither task nor branch; its detail line stays empty rather
	// than carrying the tmux session name, which is an identity key only.
	parts := make([]string, 0, 2)
	if detail := choose(workspace.TaskID, workspace.Branch); detail != "" {
		parts = append(parts, detail)
	}
	switch {
	case stale:
		parts = append(parts, "stale")
	case workspace.Presentation.Freshness != "" && workspace.Presentation.Freshness != agentstatus.FreshnessCurrent:
		parts = append(parts, string(workspace.Presentation.Freshness))
	}
	line3 := kanban.Line{Spans: []kanban.Span{{Text: spine, Foreground: hue}}}
	// Which machine, spelled out. A remote card carries only the bare project
	// name — the host is not in it, the way it is in the Sessions row's project
	// label — so without this the board could say a card is remote but never
	// which of two hosts it came from.
	if workspace.Remote() {
		line3.Spans = append(line3.Spans, kanban.Span{Text: " " + workspace.HostID, Foreground: workspacelist.HostHue(workspace.HostID)})
	}
	if len(parts) > 0 {
		line3.Spans = append(line3.Spans, kanban.Span{Text: " " + strings.Join(parts, " · "), Foreground: styles.TextMuted})
	}
	return []kanban.Line{line1, line2, line3}
}

// errorCardLines renders a project-unavailable card with a muted spine —
// there is no live workspace to hang a project hue off of.
func errorCardLines(projectName string, err error) []kanban.Line {
	spine := spineGlyph(workspaceinventory.KindWorktree)
	return []kanban.Line{
		{Spans: []kanban.Span{{Text: spine, Foreground: styles.TextMuted}, {Text: " " + projectName, Foreground: styles.TextPrimary}}},
		{Spans: []kanban.Span{{Text: spine, Foreground: styles.TextMuted}, {Text: " project unavailable", Foreground: styles.TextMuted}}},
		{Spans: []kanban.Span{{Text: spine, Foreground: styles.TextMuted}, {Text: " " + err.Error(), Foreground: styles.TextMuted}}},
	}
}

// DormantAfter is when an idle session stops competing for attention. The idle
// lane holds both "finished a minute ago" and "untouched since Tuesday";
// dimming past this threshold separates them without a sixth lane.
const DormantAfter = time.Hour

func isDormant(p agentstatus.Presentation, now time.Time) bool {
	if p.Lane != agentstatus.LaneIdle || p.ChangedAt.IsZero() {
		return false
	}
	return now.Sub(p.ChangedAt) > DormantAfter
}

// relativeAge formats the gap between changedAt and now as the small units the
// board cards use: "now", "3m", "1h", "2d". A zero changedAt renders nothing.
//
// It defers to the shared formatter rather than keeping a second copy of the
// same ladder: the board and the workspace lists describe the same events, and
// a card that reads "now" beside a list row that reads "12s" is two answers to
// one question.
func relativeAge(changedAt, now time.Time) string {
	return workspacelist.RelativeAge(changedAt, now)
}

func (m *Model) renderCompact(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	type compactCard struct {
		column, row int
		lane        string
		card        kanban.Card
	}
	board := m.board.Board()
	items := make([]compactCard, 0)
	selectedIndex := -1
	selection := m.board.Selection()
	for column, lane := range board.Lanes {
		for row, card := range lane.Cards {
			if column == selection.Column && row == selection.Row {
				selectedIndex = len(items)
			}
			items = append(items, compactCard{column: column, row: row, lane: lane.Label, card: card})
		}
	}
	visibleRows := max(0, height-1)
	if selectedIndex >= 0 && visibleRows > 0 {
		if selectedIndex < m.compactScroll {
			m.compactScroll = selectedIndex
		} else if selectedIndex >= m.compactScroll+visibleRows {
			m.compactScroll = selectedIndex - visibleRows + 1
		}
	}
	maxScroll := max(0, len(items)-visibleRows)
	m.compactScroll = min(max(0, m.compactScroll), maxScroll)

	header := styles.Title.Render("Agent Overview") + "  " + styles.Muted.Render(m.summary())
	lines := []string{fitCompactLine(header, width)}
	end := min(len(items), m.compactScroll+visibleRows)
	for index := m.compactScroll; index < end; index++ {
		item := items[index]
		line := fitCompactLine(compactCardText(item.lane, item.card, m.cards[item.card.ID]), width)
		if index == selectedIndex {
			// Same darker fill as the board kanban: multi-coloured card text
			// washes out on ListItemSelected's BgTertiary lift.
			line = styles.CardSelected.Render(line)
		}
		lines = append(lines, line)
		y := len(lines) - 1
		m.mouse.HitMap.AddRect("overview-card", 0, y, width, 1, kanban.HitRegion{Kind: kanban.RegionCard, Column: item.column, Row: item.row, CardID: item.card.ID, X: 0, Y: y, W: width, H: 1})
	}
	if len(items) == 0 && len(lines) < height {
		lines = append(lines, fitCompactLine(styles.Muted.Render(" No agent-backed workspaces found"), width))
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines[:height], "\n")
}

// compactCardText renders the one-line compact fallback for a card, picking
// up the project hue and agent colour when a live workspace backs the card.
// Cards built outside syncBoard (tests, or a card with no Lines) fall back to
// the plain Title/Subtitle fields.
func compactCardText(lane string, card kanban.Card, workspace workspaceinventory.Workspace) string {
	if workspace.ID == "" {
		return fmt.Sprintf(" %-15s %s  %s", lane, card.Title, card.Subtitle)
	}
	label := workspace.ProjectName + " / " + workspace.Name
	project := lipgloss.NewStyle().Foreground(styles.ProjectHue(workspace.ProjectKey)).Render(label)
	// The compact fallback is the same board at a width that cannot hold lanes,
	// so it owes the same provenance mark. Naming the host here rather than
	// only glyphing it: this line has the whole terminal width to spend.
	if workspace.Remote() {
		host := lipgloss.NewStyle().Foreground(workspacelist.HostHue(workspace.HostID)).
			Render(workspacelist.HostGlyph + " " + workspace.HostID)
		project = host + " " + project
	}
	agent := lipgloss.NewStyle().Foreground(styles.AgentColor(workspace.Provider)).Render(styles.AgentLabel(workspace.Provider))
	return fmt.Sprintf(" %-15s %s  %s · %s", lane, project, agent, workspace.Presentation.Label)
}

func fitCompactLine(line string, width int) string {
	line = ansi.Truncate(line, width, "")
	if gap := width - ansi.StringWidth(line); gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	return line
}

func clean(path string) string { return workspaceinventory.CanonicalPath(path) }

func normalizeProject(project Project) Project {
	root := workspaceinventory.CanonicalProjectPath(project.Path)
	project.Path = root
	project.Key = root
	return project
}

func projectKey(project Project) string {
	if project.Key != "" {
		return project.Key
	}
	return clean(project.Path)
}

func choose(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Keep the shared semantic dependency visible at this boundary: Overview cards
// are projections of agentstatus, not a second status reducer.
var _ agentstatus.LaneID
