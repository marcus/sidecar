package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/agentactivity/manifests"
	"github.com/marcus/sidecar/internal/clip"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/configui"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/gitinit"
	"github.com/marcus/sidecar/internal/hostproto"
	"github.com/marcus/sidecar/internal/hosts"
	"github.com/marcus/sidecar/internal/issueview"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/livewatch"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/notifydelivery"
	"github.com/marcus/sidecar/internal/overview"
	"github.com/marcus/sidecar/internal/palette"
	"github.com/marcus/sidecar/internal/panereposition"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/projectlist"
	"github.com/marcus/sidecar/internal/startuptrace"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/theme"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/version"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspaceinventory"
)

// ModalKind identifies an app-level modal with explicit priority ordering.
// Lower values = higher priority (checked first for rendering and input routing).
type ModalKind int

const (
	ModalNone             ModalKind = iota // No modal open
	ModalPalette                           // Command palette (highest priority)
	ModalHelp                              // Help overlay
	ModalUpdate                            // Update modal
	ModalDiagnostics                       // Diagnostics/version info
	ModalQuitConfirm                       // Quit confirmation dialog
	ModalProjectSwitcher                   // Project switcher
	ModalWorktreeSwitcher                  // Worktree switcher
	ModalThemeSwitcher                     // Theme switcher
	ModalOpenIn                            // Open In IDE picker
	ModalIssueInput                        // Issue ID text input
	ModalIssuePreview                      // Issue preview display
	ModalPaneReposition                    // Reposition one app content-deck leaf
	ModalPaneSwitcher                      // Pane switcher over a plugin's content deck (lowest priority)
)

// activeModal returns the highest-priority open modal.
// This is the single source of truth for which modal is currently active.
func (m *Model) activeModal() ModalKind {
	switch {
	case m.showPalette:
		return ModalPalette
	case m.showHelp:
		return ModalHelp
	case m.updateModalState != UpdateModalClosed:
		return ModalUpdate
	case m.showDiagnostics:
		return ModalDiagnostics
	case m.showQuitConfirm:
		return ModalQuitConfirm
	case m.showProjectSwitcher:
		return ModalProjectSwitcher
	case m.showWorktreeSwitcher:
		return ModalWorktreeSwitcher
	case m.showThemeSwitcher:
		return ModalThemeSwitcher
	case m.showOpenIn:
		return ModalOpenIn
	case m.showIssueInput:
		return ModalIssueInput
	case m.showIssuePreview:
		return ModalIssuePreview
	case func() bool { _, controller := m.activePaneLayoutController(); return controller != nil }():
		return ModalPaneReposition
	case m.paneSwitcherOpen:
		return ModalPaneSwitcher
	default:
		return ModalNone
	}
}

// hasModal returns true if any app-level modal is open.
func (m *Model) hasModal() bool {
	return m.activeModal() != ModalNone
}

// modalFocusContext returns the keymap context a modal owns while it is open.
// An open modal holds keyboard focus, so its context must survive plugin
// activity underneath it (an interactive tmux pane, for example, keeps emitting
// messages that would otherwise recompute the context back to the plugin's).
// Modals that intercept keys before context-based routing (quit confirm, the
// update modal) have no context of their own and report false, leaving the
// underlying plugin context in place.
func modalFocusContext(kind ModalKind) (string, bool) {
	switch kind {
	case ModalPalette:
		return "palette", true
	case ModalHelp:
		return "help", true
	case ModalDiagnostics:
		return "diagnostics", true
	case ModalProjectSwitcher:
		return "project-switcher", true
	case ModalWorktreeSwitcher:
		return "worktree-switcher", true
	case ModalThemeSwitcher:
		return "theme-switcher", true
	case ModalOpenIn:
		return "open-in", true
	case ModalIssueInput:
		return "issue-input", true
	case ModalIssuePreview:
		return "issue-preview", true
	case ModalPaneReposition:
		return panereposition.ModalContext, true
	case ModalPaneSwitcher:
		return paneSwitcherContext, true
	}
	return "", false
}

// TabBounds represents the X position range of a tab for mouse hit testing.
// The tab is identified by its typed reference, not by a bare index, so a hit
// region can only ever activate a tab of the scope that painted it.
type TabBounds struct {
	Start, End int
	Tab        tabRef
}

type projectAddState struct {
	nameInput     textinput.Model
	pathInput     textinput.Model
	errorMessage  string
	themeSelected string
}

type projectSwitcherDestination struct {
	Kind           string
	Name           string
	Path           string
	Project        *config.ProjectConfig
	Destination    Destination
	DisabledReason string
}

func (d projectSwitcherDestination) isRemote() bool {
	return d.Destination.HostID != ""
}

func (d projectSwitcherDestination) identityKey() string {
	if d.Kind == destinationOverview {
		return "overview"
	}
	if d.isRemote() {
		return "host\x1f" + d.Destination.HostID + "\x1f" + d.Destination.ProjectKey + "\x1f" + d.Destination.WorktreeKey
	}
	return "local\x1f" + d.Path
}

const (
	destinationProject  = "project"
	destinationOverview = "overview"
	workspacePluginID   = "workspace-manager"
)

// Model is the root Bubble Tea model for the sidecar application.
type Model struct {
	// Configuration
	cfg *config.Config

	// Plugin management
	registry     *plugin.Registry
	activePlugin int
	contentDecks map[string]*appContentDeck

	// Keymap
	keymap        *keymap.Registry
	activeContext string

	// pendingActivation is the single hand-off across a project switch — a
	// target to land or a workspace selection to apply. See pending_target.go.
	pendingActivation *pendingActivation

	// UI state
	width, height           int
	showHelp                bool
	helpModal               *modal.Modal
	helpModalWidth          int
	helpMouseHandler        *mouse.Handler
	showDiagnostics         bool
	diagnosticsModal        *modal.Modal
	diagnosticsModalWidth   int
	diagnosticsPrimary      string // primary action currently applied to the modal
	diagnosticsMouseHandler *mouse.Handler
	showClock               bool
	titleTemplate           string // ui.terminalTitle; empty leaves the terminal title alone
	lastTitle               string // last title emitted, so OSC 1 only goes out on change
	titleResyncCounter      int    // ticks since the icon name was last re-asserted
	showPalette             bool
	showQuitConfirm         bool
	quitModal               *modal.Modal
	quitMouseHandler        *mouse.Handler
	palette                 palette.Model

	// Project switcher modal
	showProjectSwitcher         bool
	projectSwitcherCursor       int
	projectSwitcherScroll       int // scroll offset for list
	projectSwitcherInput        textinput.Model
	projectSwitcherFiltered     []projectSwitcherDestination
	projectSwitcherModal        *modal.Modal
	projectSwitcherModalWidth   int
	projectSwitcherMouseHandler *mouse.Handler
	projectSwitcherBar          switcherBarState
	// projectSwitcherRows is the presentation form of projectSwitcherFiltered,
	// index-aligned with it: the destinations answer activation, the items
	// answer drawing and ordering.
	projectSwitcherRows []projectlist.Item
	// The switcher's view controls. Sort and view are remembered across
	// launches; which control has focus is not.
	projectSwitcherSort     projectlist.Sort
	projectSwitcherOrder    projectlist.Order
	projectSwitcherView     projectlist.View
	projectSwitcherFocus    switcherFocus
	projectSwitcherSortOpen bool
	projectSwitcherSortIdx  int
	// projectSwitcherContentW is the width the modal library handed the
	// collection section on its last render. Key handling reads it so the
	// grid's arrows and the list's window agree with what was drawn.
	projectSwitcherContentW int
	// boundDestination is the host-qualified project this TUI is currently
	// bound to. Empty HostID means a local project (today's path identity).
	boundDestination Destination
	// testHostCatalog, when non-nil, replaces overview.HostCatalog in tests.
	testHostCatalog []overview.HostCatalogEntry

	// Project add sub-mode (within project switcher)
	projectAddMode         bool
	projectAdd             *projectAddState
	projectAddModal        *modal.Modal
	projectAddModalWidth   int
	projectAddMouseHandler *mouse.Handler

	// Theme picker within add-project flow
	projectAddThemeMode     bool // is theme picker sub-modal open?
	projectAddThemeCursor   int
	projectAddThemeScroll   int
	projectAddThemeInput    textinput.Model
	projectAddThemeFiltered []string // filtered theme list

	// Worktree switcher modal
	showWorktreeSwitcher         bool
	worktreeSwitcherCursor       int
	worktreeSwitcherScroll       int
	worktreeSwitcherInput        textinput.Model
	worktreeSwitcherFiltered     []worktreeSwitcherRow
	worktreeSwitcherAll          []worktreeSwitcherRow
	worktreeSwitcherModal        *modal.Modal
	worktreeSwitcherModalWidth   int
	worktreeSwitcherMouseHandler *mouse.Handler
	worktreeCheckCounter         int // Counter for periodic worktree existence check
	worktreeInventoryCounter     int // Counter for periodic worktree inventory refresh
	worktreeSwitcherBar          switcherBarState

	// Worktree info cache (avoids git subprocess forks on every View render)
	cachedWorktreeInfo      *WorktreeInfo
	cachedWorktreeInventory []WorktreeInfo
	// localWorktreeCache is last-captured local inventory keyed by normalized
	// main path and by project name. W while bound remotely reads this instead
	// of spawning git worktree list.
	localWorktreeCache map[string][]WorktreeInfo

	// Open In modal
	showOpenIn         bool
	openInCursor       int
	openInScroll       int
	openInApps         []openInApp
	openInLastID       string
	openInModal        *modal.Modal
	openInModalWidth   int
	openInMouseHandler *mouse.Handler

	// Theme switcher modal
	showThemeSwitcher         bool
	themeSwitcherModal        *modal.Modal
	themeSwitcherModalWidth   int
	themeSwitcherMouseHandler *mouse.Handler
	themeSwitcherSelectedIdx  int
	themeSwitcherInput        textinput.Model
	themeSwitcherFiltered     []themeEntry
	themeSwitcherOriginal     themeEntry // original theme to restore on cancel
	themeSwitcherScope        string     // "global" or "project"
	themeSwitcherBar          switcherBarState

	// Issue preview - input phase
	showIssueInput         bool
	issueInputInput        textinput.Model
	issueInputModal        *modal.Modal
	issueInputModalWidth   int
	issueInputMouseHandler *mouse.Handler

	// Issue input auto-complete
	issueSearchResults       []IssueSearchResult
	issueSearchQuery         string // last query sent to td search
	issueSearchLoading       bool
	issueSearchCursor        int  // selected result index (-1 = none/input focused)
	issueSearchScrollOffset  int  // viewport scroll offset for search results
	issueSearchIncludeClosed bool // whether to include closed issues in search

	// Issue preview - preview phase
	showIssuePreview         bool
	issuePreviewView         *issueview.Model
	issuePreviewData         *IssuePreviewData
	issuePreviewLoading      bool
	issuePreviewError        error
	issuePreviewModal        *modal.Modal
	issuePreviewModalWidth   int
	issuePreviewModalHeight  int
	issuePreviewMouseHandler *mouse.Handler
	// issuePreviewWatcher keeps the modal's card in step with the td store. It
	// lives exactly as long as the modal. See issue_preview_live.go.
	issuePreviewWatcher *livewatch.PathWatcher

	// Pane switcher — the shared create form hosted over a plugin's content
	// deck. One binding site for every deck-eligible plugin; see
	// pane_switcher.go.
	paneSwitcherOpen  bool
	paneSwitcher      *workspacecreate.Form
	paneSwitcherMouse *mouse.Handler

	// Header/footer
	ui *UIState

	// Error handling
	lastError error

	// Ready state
	ready              bool
	applicationFocused bool
	attentionPublished attentionSnapshot
	attentionTracking  bool

	// Version info. One discovered target per product (Sidecar, td, and — only
	// when the tasks_plugin feature is effectively enabled — Tasks), rather
	// than parallel per-product fields.
	currentVersion string
	products       []version.Target

	// Update feature state
	updateInProgress bool
	needsRestart     bool
	// updateResultsAcked records that the user closed the Done/Failed surface
	// after seeing it, so a later entry point offers a fresh confirmation
	// rather than a stale result.
	updateResultsAcked bool

	// Confirmed batch. updatePlan is immutable once confirmed; updateResults
	// records the settled outcome of every attempted target so a later failure
	// cannot erase an earlier success. updatePlanID rejects stale async
	// messages from an abandoned or superseded batch.
	updatePlan      []version.Target
	updateResults   []version.Result
	updateCarried   []version.Result // outcomes carried over from an earlier batch
	updatePlanID    int
	updateActiveIdx int

	// Update modal state. One modal object carries every phase (Preview →
	// Installing → Done/Failed); updateModalState selects which When-gated
	// sections render.
	updateModalState UpdateModalState
	updateStartTime  time.Time
	// The single update modal and its input state. Built once per open flow;
	// phase changes re-present it rather than rebuild it.
	updateModal        *modal.Modal
	updateMouseHandler *mouse.Handler
	updateUI           *updateUIState
	// updateBatchRetry records whether the running batch was launched as a
	// retry, so the Installing surface shows its disabled button accordingly.
	updateBatchRetry bool
	// updateNotesBar is the notes section's scrollbar pointer state (hover +
	// press-time gesture snapshot).
	updateNotesBar switcherBarState
	// Intro animation
	intro IntroModel

	// Scope state. The app owns the current project and project plugin; these
	// three fields own the global space that can be shown over it. Overview is
	// an app-owned destination, not a project plugin, and is only constructed
	// while its feature is enabled.
	overview      *overview.Model
	terminalLinks termpreview.LinkCoordinator
	scope         AppScope
	// globalTab is the visible global surface's ID, never an index: a tab that
	// disappears must not slide an index onto a different action.
	globalTab string
	// globalHosts is one host per enabled global-scope plugin descriptor, in
	// descriptor order. The header row is built from it.
	globalHosts []*globalPluginHost
	// pluginDescriptors is the full catalog, injected by the host process. It
	// is what the settings page loops over.
	pluginDescriptors []plugin.Descriptor

	// Configuration surface. Like the Tasks host it is app-owned rather than a
	// registry plugin, so it survives project switches; unlike the global
	// space it covers whatever surface is active rather than replacing it.
	config       *configui.Model
	configActive bool
	configReturn configReturn
	// headerGearHovered styles the gear while the pointer is over it. The header
	// is otherwise geometric and stateless, so this is the whole of its hover
	// state: one bool for the one control that has a hover look.
	headerGearHovered bool
	// startupConfigPage is the destination a launch command asked for. It is
	// honored once, from Init, so Configuration opens over the ordinary startup
	// surface rather than replacing it: escape still returns to the app the
	// user would have had.
	startupConfigPage configui.PageID
	// sessionRestoreStarted makes the cold restore fire on the first
	// WindowSizeMsg and no later one, so a resize does not schedule a second.
	// It lives on the model rather than in a package-level sync.Once so that a
	// second Model in one process still gets its own restore.
	sessionRestoreStarted bool
	// firstRunProbePending is set when launch has no configured projects and
	// no startup destination. A tea.Cmd answers whether cwd is a Git repo;
	// until it does, the content area shows a guided loading state instead of
	// an empty td-monitor.
	firstRunProbePending bool
	// lastNotifiedTheme is the last styles palette delivered to plugins. Theme
	// notifications compare against it so search typing and other Configuration
	// input do not rebuild plugin styles when the resolved colors did not change.
	lastNotifiedTheme styles.Theme

	// UI request watcher for external CLI commands (e.g. sidecar open).
	// The channel is deliberately not cached here: Init takes the model by
	// value, so anything it assigns is discarded, and a cached-and-nil channel
	// silently stops the listener re-arming after the first request.
	uiRequestWatcher *uirequest.Watcher

	// Notification store and its render-side snapshot. The store is app-shell
	// state, like the header: it outlives every plugin, survives project and
	// worktree switches, and is the single writer for this process.
	notifications     notify.Store
	notificationCache []notify.Notification
	// notificationDelivery is the shared app/CLI coordinator. Its production
	// value is lazy: construction performs no state, PATH, cache, or subprocess
	// work on the startup paint path. Commands queued by nested dismissal paths
	// are drained by the Update postlude.
	notificationDelivery     notifydelivery.Coordinator
	notificationDeliveryCmds []tea.Cmd
	// terminalNotifyWriter collects direct-terminal notification bytes so the
	// renderer emits them rather than a delivery goroutine. Nil when delivery
	// was injected by a test.
	terminalNotifyWriter *terminalNotifyWriter
	// notificationCTAs memoizes each notification's reconciled target list by
	// id, so the file-existence check behind a verified underline runs once per
	// record rather than once per frame. See notification_targets.go.
	notificationCTAs map[string][]notify.CallToAction
	// notificationCTARoot is the checkout notificationCTAs was verified
	// against; a project or worktree switch invalidates the whole memo.
	notificationCTARoot string
	// toastPainted records notification ids a toast was actually drawn for, so
	// the expiry sweep only marks read what the user had a chance to see.
	toastPainted map[string]bool
	// toastMouse carries the toast's one pointer target. A toast has no focus
	// context and never takes the keyboard (plan 1.5 item 5): click-to-dismiss
	// and the global `d` fallback are its whole interaction surface, so a hit
	// map is all the pointer state it needs.
	toastMouse *mouse.Handler
	// toastReshow is a notification the user asked to see again from the
	// centre (`enter` = view details). It is a presentation-only copy with its
	// own countdown: re-showing must not re-post, un-dismiss, or reorder
	// anything in the store.
	toastReshow      *notify.Notification
	toastReshowUntil time.Time
	// toastReveals is one reveal state per on-screen block, keyed by the stack
	// key — the same key the stack collapses on, so a block keeps its motion
	// when another copy of the same message joins it. toastColumn is the order
	// they are painted in, and together they are the *only* description of what
	// is on screen: the store feeds them, and never the renderer directly. See
	// internal/reveal and toast_stack.go.
	toastReveals   map[notify.StackKey]*toastReveal
	toastColumn    []notify.StackKey
	toastRevealSeq int
	// toastExpanded opens every collapsed block's hidden members (design 1b's
	// peek line). It is one flag rather than one per block: the affordance is a
	// single global key, and "expand what is on screen" is what a user pressing
	// it means.
	toastExpanded bool
	// notificationCentreOpen is app-shell state, deliberately not per-plugin:
	// the centre stays open across every navigation until the user closes it.
	notificationCentreOpen bool
	// notificationCentreWidth is the panel's own width in columns, persisted in
	// internal/state. Zero means "not resolved yet"; the first frame reads the
	// saved preference and falls back to the default.
	notificationCentreWidth int
	// notificationCentreFocused says the panel owns the keyboard. It is not the
	// same as open: clicking back into content returns focus without closing.
	notificationCentreFocused bool
	// notificationCentreCursor selects a row in the flat, source-grouped list.
	notificationCentreCursor int
	// notificationCentreScroll is the first body row drawn.
	notificationCentreScroll int
	// Pointer state for the panel: the shared drag machinery (internal/mouse
	// hit regions plus ui.RenderHandle) resizes it, exactly as a plugin's pane
	// divider does.
	notificationCentreMouse       *mouse.Handler
	notificationCentreHoverHandle bool
	notificationCentreHoverClose  bool
	// notificationCentreHoverBar lights the body scrollbar's thumb and track
	// under the pointer, and yields to the drag state while a gesture is live.
	notificationCentreHoverBar bool
	// Burst is a pointer so FilterInput's Model copy still shares it: Reset at
	// a boundary must survive the filter not returning a model.
	notificationCentreWheel *tty.WheelBurst
	notificationCentreNow   func() time.Time
	// The issue preview modal coalesces the same way, for the same reason: its
	// boundary answer runs in the input filter's Model copy, and Reset there
	// must reach the burst Update applies through.
	issuePreviewWheel    *tty.WheelBurst
	issuePreviewWheelNow func() time.Time

	// flash is the status-flash tier: one transient line in the content
	// region's top-right, never stored and never counted. See flash.go.
	flash flashState
}

// Option adjusts the model at construction. Options exist for the deliberate,
// caller-supplied startup choices — where Configuration opens, so far — and are
// deliberately not a general settings channel: everything else comes from the
// config file the app was handed.
type Option func(*Model)

// WithStartupConfigPage opens Configuration on a destination as soon as the app
// starts. `sidecar setup` is the only caller today; an unknown page falls back
// to Configuration's own default rather than failing the launch.
func WithStartupConfigPage(page configui.PageID) Option {
	return func(m *Model) { m.startupConfigPage = page }
}

// WithPluginDescriptors hands the shell the full plugin catalog, which it
// passes to the settings page. It is an option rather than a parameter because
// internal/app cannot import internal/plugins/assembly — the plugin packages
// import this one — so the catalog arrives from the process that owns both.
// Without it the shell still hosts the global plugins it knows about; only the
// settings page's loop is empty.
func WithPluginDescriptors(descriptors []plugin.Descriptor) Option {
	return func(m *Model) {
		m.pluginDescriptors = descriptors
		if m.config != nil {
			m.config.SetPluginDescriptors(descriptors)
		}
	}
}

// WithNotificationDelivery injects a coordinator for focused app tests and
// alternate hosts. Production uses the lazy platform coordinator.
func WithNotificationDelivery(delivery notifydelivery.Coordinator) Option {
	return func(m *Model) { m.notificationDelivery = delivery }
}

// New creates a new application model.
// initialPluginID optionally specifies which plugin to focus on startup (empty = first plugin).
func New(reg *plugin.Registry, km *keymap.Registry, cfg *config.Config, currentVersion, workDir, projectRoot, initialPluginID string, opts ...Option) Model {
	repoName := GetRepoName(workDir)
	ui := NewUIState()
	ui.WorkDir = workDir
	ui.ProjectRoot = projectRoot

	// Determine initial active plugin index
	activeIdx := 0
	if initialPluginID != "" {
		for i, p := range reg.Plugins() {
			if p.ID() == initialPluginID {
				activeIdx = i
				break
			}
		}
	}

	m := Model{
		cfg:                cfg,
		registry:           reg,
		keymap:             km,
		activePlugin:       activeIdx,
		contentDecks:       make(map[string]*appContentDeck),
		activeContext:      "global",
		showClock:          cfg.UI.ShowClock,
		titleTemplate:      cfg.UI.TerminalTitle,
		palette:            palette.New(),
		config:             configui.New(),
		ui:                 ui,
		ready:              false,
		applicationFocused: true,
		intro:              NewIntroModel(repoName),
		currentVersion:     currentVersion,
	}
	m.terminalLinks = newTerminalLinkCoordinator(func(hostID, root string, candidate contentlink.Pending) (contentlink.Ref, bool) {
		if m.overview == nil {
			return contentlink.Ref{}, false
		}
		return m.overview.ResolveRemoteTerminalLink(hostID, root, candidate)
	})
	m.injectTerminalLinkCoordinator()
	if watcher, err := uirequest.NewWatcher(config.StateDir()); err == nil {
		m.uiRequestWatcher = watcher
	}
	// Bind the `notifications` config section before the store opens: the
	// store completes every record it is handed, and completion is where a
	// per-source expiry is applied.
	notify.ApplyConfig(cfg.Notifications)
	m.notifications = openNotificationStore()
	m.refreshNotifications()
	m.terminalNotifyWriter = &terminalNotifyWriter{}
	m.notificationDelivery = notifydelivery.NewDefaultWithTerminalWriter(config.StateDir(), m.terminalNotifyWriter.Write)
	m.notificationCentreMouse = mouse.NewHandler()
	m.notificationCentreWheel = &tty.WheelBurst{}
	m.toastMouse = mouse.NewHandler()
	if tab, ok := parseGlobalTabID(state.GetLastGlobalTab()); ok {
		m.globalTab = tab
	} else {
		m.globalTab = GlobalSessions
	}
	if features.IsEnabled(features.CrossProjectOverview.Name) {
		m.overview = overview.New(workspaceinventory.Collector{})
		m.overview.SetTerminalLinkCoordinator(m.terminalLinks)
		m.overview.SetConfig(cfg)
		// One resolution of the user's terminal settings, handed to every surface
		// that hosts a terminal: the browser's live pane answers the chords the
		// project plugin answers. The bindings are registered here for the same
		// reason the plugin registers its own — the default table cannot read the
		// user's config.
		terminal := TerminalConfig(cfg)
		m.overview.SetTerminalConfig(terminal)
		km.RegisterPluginBinding(terminal.ExitKey, "exit-interactive", "global-workspaces-terminal")
		km.RegisterPluginBinding(terminal.CopyKey, "copy-selection", "global-workspaces-terminal")
		km.RegisterPluginBinding(terminal.PasteKey, "paste", "global-workspaces-terminal")
	}
	m.installPluginHostSeams()
	// Global-scope plugins are tabs of the global space, so their hosts are
	// built here rather than registered as project plugins. Constructing one
	// does no I/O; the model behind it is built by the command start returns.
	m.globalHosts = newGlobalPluginHosts(
		append(GlobalDescriptors(), globalProtocolDescriptors(cfg)...), cfg, reg.Context(), km)
	// Restore the top-level space the user left on. It runs here, after the two
	// fields that decide which global tabs exist are built, and it reads only
	// the state the process already loaded — no extra file is opened on the
	// pre-first-frame path.
	//
	// The persisted scope is a request, not an instruction. Global scope is
	// honored only while the global space still has a tab to show: a user who
	// quit in Sessions and then disabled the cross-project Overview gets the
	// project workspace back rather than an empty surface. ensureVisibleGlobalTab
	// applies the same rule one level down, moving off a remembered tab whose
	// own feature is now off.
	if scope, ok := parseAppScopeID(state.GetLastScope()); ok && scope == ScopeGlobal && m.globalScopeAvailable() {
		m.scope = ScopeGlobal
		m.ensureVisibleGlobalTab()
		// The keymap context is the restored tab's from the first frame, so the
		// footer names the right keys before the first message arrives.
		m.updateContext()
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&m)
		}
	}
	if m.cfg != nil && len(m.cfg.Projects.List) == 0 && m.startupConfigPage == "" {
		m.firstRunProbePending = true
	}
	return m
}

func announceInstanceCmd(workDir, projectRoot, hostID string) tea.Cmd {
	return func() tea.Msg {
		inst := uirequest.Instance{
			PID:       os.Getpid(),
			Host:      uirequest.HostName(),
			HostID:    hostID,
			StartedAt: time.Now().UTC(),
		}
		if hostID == "" {
			inst.WorkDir = workDir
			if projectRoot != "" {
				inst.Project = filepath.Base(projectRoot)
				if dir, ok := projectdir.Lookup(projectRoot); ok {
					inst.ProjectKey = filepath.Base(dir)
				}
				if inst.WorkDir == "" {
					inst.WorkDir = projectRoot
				}
			} else if workDir != "" {
				inst.Project = filepath.Base(workDir)
			}
		}
		_ = uirequest.Announce(config.StateDir(), inst)
		return nil
	}
}

func (m Model) announcePresenceCmd() tea.Cmd {
	if m.boundDestination.HostID != "" {
		return announceInstanceCmd("", "", m.boundDestination.HostID)
	}
	workDir, projectRoot := "", ""
	if m.ui != nil {
		workDir, projectRoot = m.ui.WorkDir, m.ui.ProjectRoot
	}
	return announceInstanceCmd(workDir, projectRoot, "")
}

func listenForUIRequests(ch <-chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// Init initializes the model and returns initial commands.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tickCmd(),
		IntroTick(),
		m.announcePresenceCmd(),
		func() tea.Msg { return attentionRefreshMsg{} },
		func() tea.Msg {
			return notify.SeedLaneTrackersMsg{Notifications: append([]notify.Notification(nil), m.notificationCache...)}
		},
		tea.RequestBackgroundColor,
	}
	cmds = append(cmds, m.productCheckCmds(false)...)
	// The project Sidecar launched into is being used now. Without this it
	// would stay unrecorded until the user happened to switch back to it, so
	// the switcher would report the project they just left as Unknown. It is a
	// command, not inline work: it writes state.json, and nothing that touches
	// the filesystem belongs on the path to the first frame.
	if !m.inGlobalScope() {
		if cmd := m.recordProjectActivityCmd(m.ui.WorkDir); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if cmd := defaultThemeNoticeCmd(m.cfg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Mark the startup plugin focused. SetActivePlugin only runs when the user
	// switches tabs, so without this the initial tab reports itself unfocused —
	// which, among other things, makes it poll at the slower unfocused interval.
	// Deliberately no PluginFocused() broadcast: several plugins refresh on that
	// message without checking their own focus flag, which would duplicate their
	// Start() work on the startup path.
	//
	// A launch restored into the global space skips it, for the reason
	// enterOverview drops focus on the way in: focus is the visibility contract
	// terminal-owning plugins use, and a project plugin nobody is looking at
	// must not hold a pane. The restored global tab starts its own collection
	// instead — in a command, like every other startup fetch.
	if m.inGlobalScope() {
		if cmd := m.startVisibleGlobalTab(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	} else if p := m.ActivePlugin(); p != nil {
		p.SetFocused(true)
	}

	// Start all registered plugins
	for _, cmd := range m.registry.Start() {
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// The global plugin hosts start alongside them and outlive them: none is in
	// the registry, so a later project switch cannot stop or rebuild one. Each
	// model is built by the returned command, i.e. after the first frame.
	cmds = append(cmds, m.startGlobalHosts()...)

	// Remote hosts connect after the first frame, in a command, like every
	// other startup fetch. Nothing about a host may run before the first
	// render: dialling ssh on the startup path would put a network round trip
	// — and on an unreachable host, a full connect timeout — in front of the
	// user's first frame. With the feature off this returns nil and no
	// connection is ever attempted.
	if m.overview != nil {
		if cmd := m.overview.SyncHosts(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if m.uiRequestWatcher != nil {
		m.uiRequestWatcher.Start()
		cmds = append(cmds, listenForUIRequests(m.uiRequestWatcher.Messages()))
	}

	// Terminal resource providers describe themselves asynchronously. The
	// command waits on the first-ready-frame latch before it touches a
	// provider, so returning it from Init is safe even though an Init command
	// can start before the first render.
	if cmd := describeResourceProvidersCmd(m.cfg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// The opt-in runtime detection-manifest fetch, on the same terms: it waits
	// on the first-ready-frame latch before it does anything at all, and with
	// the setting off — the default — this returns nil and no command, no
	// goroutine, and no HTTP client exists.
	if cmd := fetchDetectionManifestsCmd(m.cfg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Cold restore is deliberately NOT started here. It is kicked off by the
	// first WindowSizeMsg instead (see update.go), because a command returned
	// from Init that parks on the first-ready-frame latch hangs any caller that
	// runs Init's commands synchronously — which is what several tests do, and
	// what an embedding of this model could reasonably do too. Starting it from
	// the update loop means it exists only inside a program that is actually
	// running a UI, which is the only situation in which it can finish.

	// A launch command's destination opens through the same message an empty
	// state sends, so there is one way into Configuration and one way back out.
	if m.startupConfigPage != "" {
		page := m.startupConfigPage
		cmds = append(cmds, func() tea.Msg { return OpenConfigurationMsg{Page: page} })
	} else if m.firstRunProbePending {
		workDir := m.ui.WorkDir
		cmds = append(cmds, func() tea.Msg {
			return firstRunProbeMsg{NeedsSetup: !gitinit.IsRepository(workDir)}
		})
	}

	return tea.Batch(cmds...)
}

// initQuitModal initializes the quit confirmation modal.
func (m *Model) initQuitModal() {
	m.quitModal = modal.New("Quit Sidecar?",
		modal.WithWidth(50),
		modal.WithVariant(modal.VariantDefault),
		modal.WithPrimaryAction("quit"),
	).
		AddSection(modal.Text("Are you sure you want to quit?")).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Quit ", "quit"),
			modal.Btn(" Cancel ", "cancel"),
		))
	m.quitMouseHandler = mouse.NewHandler()
}

// ActivePlugin returns the currently active plugin.
func (m Model) ActivePlugin() plugin.Plugin {
	if m.registry == nil {
		return nil
	}
	plugins := m.registry.Plugins()
	if len(plugins) == 0 {
		return nil
	}
	if m.activePlugin >= len(plugins) {
		m.activePlugin = 0
	}
	return plugins[m.activePlugin]
}

// SetActivePlugin sets the active plugin by index and returns a command
// to notify the plugin it has been focused.
func (m *Model) SetActivePlugin(idx int) tea.Cmd {
	return m.setActivePlugin(idx, true)
}

func (m *Model) setActivePlugin(idx int, notify bool) tea.Cmd {
	plugins := m.registry.Plugins()
	if idx >= 0 && idx < len(plugins) {
		// Unfocus current
		if current := m.ActivePlugin(); current != nil {
			current.SetFocused(false)
		}
		m.activePlugin = idx
		// Focus new
		if next := m.ActivePlugin(); next != nil {
			next.SetFocused(true)
			m.updateContext()
			if notify {
				return PluginFocused()
			}
		}
	}
	return nil
}

// NextPlugin switches to the next plugin.
func (m *Model) NextPlugin() tea.Cmd {
	plugins := m.registry.Plugins()
	if len(plugins) == 0 {
		return nil
	}
	return m.SetActivePlugin((m.activePlugin + 1) % len(plugins))
}

// PrevPlugin switches to the previous plugin.
func (m *Model) PrevPlugin() tea.Cmd {
	plugins := m.registry.Plugins()
	if len(plugins) == 0 {
		return nil
	}
	idx := m.activePlugin - 1
	if idx < 0 {
		idx = len(plugins) - 1
	}
	return m.SetActivePlugin(idx)
}

// FocusPluginByID switches to a plugin by its ID.
func (m *Model) FocusPluginByID(id string) tea.Cmd {
	// Deliberate navigation. Anything still parked for a switch is stale: the
	// user asked for somewhere else. (A landing takes the slot before it emits
	// its own focus, so this never eats the jump it is part of.)
	m.clearPendingActivation()
	plugins := m.registry.Plugins()
	for i, p := range plugins {
		if p.ID() == id {
			return m.SetActivePlugin(i)
		}
	}
	return nil
}

// focusPluginByIDWithoutNotice restores focus-owned resources after registry
// Reinit without broadcasting PluginFocusedMsg. Reinit already ran Start for
// every plugin; broadcasting the ordinary return-to-project notice here would
// make Workspaces start a second concurrent inventory refresh.
func (m *Model) focusPluginByIDWithoutNotice(id string) {
	plugins := m.registry.Plugins()
	for i, p := range plugins {
		if p.ID() == id {
			m.setActivePlugin(i, false)
			return
		}
	}
}

// ShowToast files a transient message as a `system` notification. It keeps the
// old name and signature because dozens of callers use it, but there is no
// footer toast behind it any more: the message becomes a real notification, so
// it floats as a toast for `duration` and then stays in the centre instead of
// vanishing unseen.
func (m *Model) ShowToast(message string, duration time.Duration) {
	m.showToastWithSeverity(message, duration, false)
}

func (m *Model) showToastWithSeverity(message string, duration time.Duration, isError bool) {
	severity := notify.SeverityInfo
	if isError {
		severity = notify.SeverityError
	}
	n := notify.Notification{
		Source:   notify.SourceSystem,
		Severity: severity,
		Title:    message,
	}
	if duration > 0 {
		expires := time.Now().UTC().Add(duration)
		n.ExpiresAt = &expires
	}
	m.postNotification(n)
}

// resetProjectSwitcher resets the project switcher modal state.
func (m *Model) resetProjectSwitcher() tea.Cmd {
	m.showProjectSwitcher = false
	m.projectSwitcherCursor = 0
	m.projectSwitcherScroll = 0
	m.projectSwitcherFiltered = nil
	m.projectSwitcherRows = nil
	m.projectSwitcherFocus = switcherFocusFilter
	m.projectSwitcherSortOpen = false
	m.clearProjectSwitcherModal()
	m.resetProjectAdd()
	// Restore current project's theme (undo any live preview)
	resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
	return m.applyResolvedTheme(resolved)
}

// clearProjectSwitcherModal clears the modal cache. A scrollbar gesture live
// on this modal's handler ends here — closing mid-drag must not leave a dead
// anchor behind (the td-f63097 boundary, from the switcher's side).
func (m *Model) clearProjectSwitcherModal() {
	if m.projectSwitcherMouseHandler != nil && m.projectSwitcherMouseHandler.IsDragging() {
		m.projectSwitcherMouseHandler.EndDrag()
	}
	m.projectSwitcherBar = switcherBarState{}
	m.projectSwitcherModal = nil
	m.projectSwitcherModalWidth = 0
	m.projectSwitcherMouseHandler = nil
}

// initProjectSwitcher initializes the project switcher modal.
func (m *Model) initProjectSwitcher() {
	m.clearProjectSwitcherModal()
	ti := textinput.New()
	ti.Placeholder = "Filter by name or path…"
	ti.Focus()
	ti.CharLimit = 50
	ti.SetWidth(40)
	m.projectSwitcherInput = ti
	m.projectSwitcherFocus = switcherFocusFilter
	m.projectSwitcherSortOpen = false
	m.restoreProjectSwitcherPreferences()
	m.setProjectSwitcherCollection("")
	m.projectSwitcherCursor = 0
	m.projectSwitcherScroll = 0

	// Set cursor to current project if found
	for i, destination := range m.projectSwitcherFiltered {
		if m.inGlobalScope() && destination.Kind == destinationOverview {
			m.projectSwitcherCursor = i
			break
		}
		if !m.inGlobalScope() && destination.Kind == destinationProject && m.isCurrentSwitcherDestination(destination) {
			m.projectSwitcherCursor = i
			break
		}
	}
	m.projectSwitcherEnsureCursorVisible()
	// Preview the initially-selected project's theme
	m.previewProjectTheme()
}

func (m *Model) projectSwitcherDestinations(query string) []projectSwitcherDestination {
	projects := filterProjects(m.cfg.Projects.List, query)
	result := make([]projectSwitcherDestination, 0, len(projects)+1)
	if m.globalScopeAvailable() {
		// Overview is pinned ahead of the collection and stays through a
		// filter: it is the way out of a search that found nothing, so it is
		// the last row that should disappear when nothing matches.
		result = append(result, projectSwitcherDestination{Kind: destinationOverview, Name: "Overview"})
	}
	for i := range projects {
		project := projects[i]
		result = append(result, projectSwitcherDestination{Kind: destinationProject, Name: project.Name, Path: project.Path, Project: &project})
	}
	// Remote destinations require sidecar_remote_hosts AND cross_project_overview
	// (decision 11): the registry lives on the Overview model, so with Overview
	// off there is nothing to read without inventing a second registry.
	if m.overview != nil || m.testHostCatalog != nil {
		result = append(result, m.remoteProjectSwitcherDestinations(query)...)
	}
	return result
}

func (m *Model) currentHostCatalog() []overview.HostCatalogEntry {
	if m.testHostCatalog != nil {
		return m.testHostCatalog
	}
	if m.overview == nil {
		return nil
	}
	return m.overview.HostCatalog()
}

func (m *Model) remoteProjectSwitcherDestinations(query string) []projectSwitcherDestination {
	catalog := m.currentHostCatalog()
	if len(catalog) == 0 {
		return nil
	}
	var result []projectSwitcherDestination
	for _, entry := range catalog {
		reason := destinationDisabledReason(entry.Health)
		for _, project := range entry.Projects {
			dest := Destination{
				HostID:          entry.ID,
				HostIncarnation: entry.Incarnation,
				ProjectKey:      project.Key,
				ProjectName:     project.Name,
				Root:            project.Root,
			}
			if !DestinationMatches(dest, query) {
				continue
			}
			result = append(result, projectSwitcherDestination{
				Kind:           destinationProject,
				Name:           FormatDestination(dest),
				Destination:    dest,
				DisabledReason: reason,
			})
		}
	}
	return result
}

func destinationDisabledReason(health hosts.Health) string {
	if health.State.Healthy() {
		return ""
	}
	if health.State == hosts.StateConnecting {
		if d := strings.TrimSpace(health.Detail); d != "" {
			return "connecting… · " + firstLine(d)
		}
		return "connecting…"
	}
	status := string(health.State)
	if fix := health.Fix(); fix != "" {
		status += " · " + fix
	}
	if d := strings.TrimSpace(health.Detail); d != "" {
		status += " · " + firstLine(d)
	}
	return status
}

func firstLine(s string) string {
	if index := strings.IndexByte(s, '\n'); index >= 0 {
		return strings.TrimSpace(s[:index])
	}
	return s
}

func (m *Model) isCurrentSwitcherDestination(destination projectSwitcherDestination) bool {
	if destination.Kind == destinationOverview {
		return m.inGlobalScope()
	}
	if m.boundDestination.HostID != "" {
		return destination.Destination.HostID == m.boundDestination.HostID &&
			destination.Destination.ProjectKey == m.boundDestination.ProjectKey &&
			destination.Destination.WorktreeKey == m.boundDestination.WorktreeKey
	}
	if destination.isRemote() {
		return false
	}
	matchesCurrent := destination.Path == m.ui.WorkDir
	if m.overview != nil {
		matchesCurrent = matchesCurrent || destination.Path == m.ui.ProjectRoot
	}
	return destination.Kind == destinationProject && matchesCurrent
}

func (m *Model) refreshOpenProjectSwitcher() {
	if !m.showProjectSwitcher {
		return
	}
	selected := m.selectedProjectSwitcherID()
	m.setProjectSwitcherCollection(m.projectSwitcherInput.Value())
	m.restoreProjectSwitcherSelection(selected)
	m.clearProjectSwitcherModal()
}

// filterProjects filters projects by name or path using a case-insensitive substring match.
func filterProjects(all []config.ProjectConfig, query string) []config.ProjectConfig {
	if query == "" {
		return all
	}
	q := strings.ToLower(query)
	var matches []config.ProjectConfig
	for _, p := range all {
		if strings.Contains(strings.ToLower(p.Name), q) ||
			strings.Contains(strings.ToLower(p.Path), q) {
			matches = append(matches, p)
		}
	}
	return matches
}

// updateProjectSwitcherFilter sends text input to the project filter and
// refreshes the derived list state. Mouse-driven modal transitions can leave
// the bubbles input blurred, so filtering always explicitly reclaims focus.
func (m *Model) updateProjectSwitcherFilter(msg tea.Msg) tea.Cmd {
	m.projectSwitcherInput.Focus()

	var cmd tea.Cmd
	m.projectSwitcherInput, cmd = m.projectSwitcherInput.Update(msg)
	m.setProjectSwitcherCollection(m.projectSwitcherInput.Value())
	m.clearProjectSwitcherModal()

	if m.projectSwitcherCursor >= len(m.projectSwitcherFiltered) {
		m.projectSwitcherCursor = len(m.projectSwitcherFiltered) - 1
	}
	if m.projectSwitcherCursor < 0 {
		m.projectSwitcherCursor = 0
	}
	m.projectSwitcherScroll = 0
	m.projectSwitcherEnsureCursorVisible()
	return tea.Batch(cmd, m.previewProjectTheme())
}

// projectSwitcherEnsureCursorVisible adjusts scroll to keep cursor in view.
// Returns the new scroll offset.
func projectSwitcherEnsureCursorVisible(cursor, scroll, maxVisible int) int {
	if cursor < scroll {
		return cursor
	}
	if cursor >= scroll+maxVisible {
		return cursor - maxVisible + 1
	}
	return scroll
}

// switchProject switches all plugins to a new project directory.
func (m *Model) switchProject(projectPath string) tea.Cmd {
	return m.switchProjectWithInventory(projectPath, nil)
}

func (m *Model) activateProjectSwitcherDestination(destination projectSwitcherDestination) tea.Cmd {
	if destination.DisabledReason != "" {
		return func() tea.Msg {
			return ToastMsg{Message: destination.DisabledReason, Duration: 4 * time.Second, IsError: true}
		}
	}
	m.resetProjectSwitcher()
	// The user picked a destination by hand, which outranks any parked jump.
	m.clearPendingActivation()
	if destination.Kind == destinationOverview && m.globalScopeAvailable() {
		return m.enterOverview()
	}
	if destination.isRemote() {
		return m.bindRemoteDestination(destination.Destination)
	}
	// The switcher can name the project already covered by global scope. That is
	// a return, not a switch: switchProjectWithSelection deliberately no-ops for
	// this path, so leaving with restore=false would strand the active plugin
	// unfocused and its terminal closed. Restore through the ordinary scope exit
	// and keep the useful notice without adding a second command beside the one
	// PluginFocused reconciliation the return requires.
	if m.inGlobalScope() && destination.Kind == destinationProject && destination.Path == m.ui.WorkDir {
		// No notice: the user is already looking at the project they picked, so
		// saying nothing changed adds nothing (audit row 4).
		return m.exitOverview()
	}
	m.leaveOverview(false)
	m.updateContext()
	return m.switchProject(destination.Path)
}

func (m *Model) bindRemoteDestination(dest Destination) tea.Cmd {
	if dest.HostID == "" {
		return nil
	}
	if dest.WorktreeKey == "" {
		dest = m.applyLastRemoteWorktree(dest)
	} else {
		_ = state.SetLastRemoteWorktree(dest.HostID, dest.ProjectKey, dest.WorktreeKey)
	}
	m.leaveOverview(false)

	if m.ui != nil && m.ui.WorkDir != "" {
		if main := GetMainWorktreePath(m.ui.WorkDir); main != "" {
			normalizedMain, _ := normalizePath(main)
			normalizedWork, _ := normalizePath(m.ui.WorkDir)
			_ = state.SetLastWorktreePath(normalizedMain, normalizedWork)
		}
		if active := m.ActivePlugin(); active != nil {
			_ = state.SetActivePlugin(m.ui.WorkDir, active.ID())
		}
	}

	m.boundDestination = dest
	m.installPluginHostSeams()
	_ = state.SetLastBoundLocation(state.BoundLocation{
		HostID:      dest.HostID,
		ProjectKey:  dest.ProjectKey,
		WorktreeKey: dest.WorktreeKey,
	})

	if m.ui != nil {
		m.ui.WorkDir = ""
		m.ui.ProjectRoot = ""
	}
	m.cachedWorktreeInventory = nil
	m.cachedWorktreeInfo = nil
	m.intro.RepoName = BoundDestinationNavbarLabel(dest)

	themeCmd := m.applyResolvedTheme(theme.ResolveTheme(m.cfg, ""))
	var startCmds []tea.Cmd
	if m.registry != nil {
		startCmds = m.registry.ReinitHost("", "", plugin.HostBind{
			HostID:      dest.HostID,
			Incarnation: dest.HostIncarnation,
			ProjectKey:  dest.ProjectKey,
			WorktreeKey: dest.WorktreeKey,
		})
		startCmds = append(startCmds, func() tea.Msg {
			return notify.SeedLaneTrackersMsg{Notifications: append([]notify.Notification(nil), m.notificationCache...)}
		})
		if cmd := m.publishResourceProviders(); cmd != nil {
			startCmds = append(startCmds, cmd)
		}
	}
	if themeCmd != nil {
		startCmds = append(startCmds, themeCmd)
	}
	startCmds = append(startCmds, m.emitContentSize()...)
	m.focusPluginByIDWithoutNotice(workspacePluginID)
	m.claimBoundProjectLeases()

	return tea.Batch(
		tea.Batch(startCmds...),
		m.refreshConfigContext(),
		m.syncTerminalTitle(false),
		m.announcePresenceCmd(),
		ShowFlash(fmt.Sprintf("Switched to %s", FormatDestination(dest))),
	)
}

func (m *Model) clearBoundDestination() {
	m.boundDestination = Destination{}
	_ = state.ClearLastBoundLocation()
}

func (m *Model) applyLastRemoteWorktree(dest Destination) Destination {
	saved := state.GetLastRemoteWorktree(dest.HostID, dest.ProjectKey)
	if saved == "" {
		return dest
	}
	name, ok := m.catalogLinkedWorktreeName(dest.HostID, dest.ProjectKey, saved)
	if !ok {
		return dest
	}
	dest.WorktreeKey = saved
	dest.WorktreeName = name
	return dest
}

func (m *Model) catalogLinkedWorktreeName(hostID, projectKey, worktreeKey string) (string, bool) {
	for _, entry := range m.currentHostCatalog() {
		if entry.ID != hostID {
			continue
		}
		for _, project := range entry.Projects {
			if project.Key != projectKey {
				continue
			}
			for _, ws := range project.Workspaces {
				if ws.Kind != workspaceinventory.KindWorktree {
					continue
				}
				key := unscopedWorktreeKey(ws)
				if key != worktreeKey {
					continue
				}
				if ws.IsMain || key == project.Key || (project.Root != "" && key == project.Root) {
					return "", false
				}
				return catalogWorktreeDisplayName(ws), true
			}
		}
	}
	return "", false
}

func (m *Model) installPluginHostSeams() {
	if m.registry == nil {
		return
	}
	ctx := m.registry.Context()
	if ctx == nil {
		return
	}
	ctx.HostWorkspaces = func() []plugin.HostWorkspace {
		return m.boundHostWorkspaces()
	}
	ctx.RemoteControlSpawner = func() tty.ControlSpawner {
		if m.overview == nil || ctx.HostID == "" {
			return nil
		}
		return m.overview.HostControlSpawner(ctx.HostID)
	}
	ctx.RemoteRunner = func(c context.Context, hostID string, args []string, out any) error {
		if m.overview == nil {
			return fmt.Errorf("no host registry")
		}
		return m.overview.RunHostSidecar(c, hostID, args, out)
	}
	ctx.HostVerbs = func() hostproto.VerbCapabilities {
		if m.overview == nil || ctx.HostID == "" {
			return hostproto.VerbCapabilities{}
		}
		return m.overview.HostVerbs(ctx.HostID)
	}
	ctx.HostShows = func() bool {
		if m.overview == nil || ctx.HostID == "" {
			return false
		}
		return m.overview.HostShows(ctx.HostID)
	}
	if m.overview != nil {
		m.overview.RelayedLanding = func(req uirequest.Request) bool {
			return m.uiRequestLanding(req) != uiRequestLandingBoundWorkspace
		}
	}
}

func (m *Model) boundHostWorkspaces() []plugin.HostWorkspace {
	// Read bound identity from the app model, not registry.Context().
	// Workspace Start calls this during ReinitHost, which already holds the
	// registry lock; taking Context()'s RLock on the same goroutine deadlocks
	// (RWMutex is not reentrant) and freezes the TUI on Enter of a remote row.
	hostID := m.boundDestination.HostID
	projectKey := m.boundDestination.ProjectKey
	if hostID == "" {
		return nil
	}
	for _, entry := range m.currentHostCatalog() {
		if entry.ID != hostID {
			continue
		}
		for _, project := range entry.Projects {
			if project.Key == projectKey {
				out := make([]plugin.HostWorkspace, 0, len(project.Workspaces))
				for _, ws := range project.Workspaces {
					out = append(out, plugin.HostWorkspace{
						ID:         unscopedHostWorkspaceID(ws.ID),
						Kind:       string(ws.Kind),
						Name:       ws.Name,
						Key:        ws.Key,
						Path:       ws.Path,
						TmuxName:   ws.TmuxName,
						PaneID:     ws.PaneID,
						Provider:   ws.Provider,
						Branch:     ws.Branch,
						TaskID:     ws.TaskID,
						Live:       ws.Live,
						IsMain:     ws.IsMain,
						IsMissing:  ws.IsMissing,
						IsBare:     ws.IsBare,
						IsDetached: ws.IsDetached,
						IsLocked:   ws.IsLocked,
						IsPrunable: ws.IsPrunable,
						CreatedAt:  ws.CreatedAt,
					})
				}
				return out
			}
		}
	}
	return nil
}

func unscopedHostWorkspaceID(id string) string {
	if _, rest, ok := hosts.SplitScopedKey(id); ok {
		return rest
	}
	return id
}

// claimGeometryLease is the bind-time lease seam. Production claims through
// tty; tests replace it so a bind can be proven without talking to tmux.
var claimGeometryLease = tty.ClaimGeometryLease

func (m *Model) claimBoundProjectLeases() {
	for _, ws := range m.boundHostWorkspaces() {
		if !ws.Live || ws.TmuxName == "" {
			continue
		}
		claimGeometryLease(ws.TmuxName)
	}
}

func (m *Model) syncBoundHostIncarnation() {
	hostID := m.boundDestination.HostID
	if hostID == "" || m.registry == nil {
		return
	}
	var incarnation uint64
	for _, entry := range m.currentHostCatalog() {
		if entry.ID == hostID {
			incarnation = entry.Incarnation
			break
		}
	}
	if incarnation == 0 {
		return
	}
	if m.boundDestination.HostIncarnation == incarnation {
		ctx := m.registry.Context()
		if ctx != nil && ctx.HostIncarnation == incarnation {
			return
		}
	}
	m.boundDestination.HostIncarnation = incarnation
	m.registry.SetHostIncarnation(incarnation)
}

func (m *Model) overviewProjects() []overview.Project {
	if m.cfg == nil {
		return nil
	}
	selected := make([]overview.Project, 0, len(m.cfg.Projects.List))
	for _, project := range m.cfg.Projects.List {
		selected = append(selected, overview.Project{Name: project.Name, Path: project.Path})
	}
	return selected
}

// syncOverviewProjects applies a changed configured project set to the running
// global catalog. When the global space is not live, entry does this through
// startVisibleGlobalTab's Ensure, so there is nothing to do here.
func (m *Model) syncOverviewProjects() tea.Cmd {
	if m.overview == nil || !m.inGlobalScope() {
		return nil
	}
	return m.overview.SetProjects(m.overviewProjects())
}

// enterOverview switches to the global space on its last-used tab. The project,
// worktree, and active plugin identity remain in place, but the covered project
// surface loses focus before a global surface starts. That focus transition is
// the visibility contract terminal-owning plugins use to close their models, so
// only the visible scope can resize or consume a pane.
func (m *Model) enterOverview() tea.Cmd {
	if !m.globalScopeAvailable() {
		return nil
	}
	if m.inGlobalScope() {
		return m.startVisibleGlobalTab()
	}
	if current := m.ActivePlugin(); current != nil {
		current.SetFocused(false)
	}
	var deckCmd tea.Cmd
	if h := m.currentContentDeck(); h != nil {
		h.releaseAppContentInputs()
		h.laidOut = false
		h.links = nil
		h.press = nil
		if h.live != nil {
			deckCmd = h.live.Reconcile()
		}
	}
	m.scope = ScopeGlobal
	m.ensureVisibleGlobalTab()
	m.persistScope()
	m.updateContext()
	return tea.Batch(deckCmd, m.startVisibleGlobalTab())
}

// exitOverview leaves the global space and hands keyboard focus back to the
// project plugin underneath. It restores the context itself so no caller can
// leave the app stuck on a global context after the space is gone.
func (m *Model) exitOverview() tea.Cmd { return m.leaveOverview(true) }

// leaveOverview is the common scope transition. restoreProject focuses the
// covered plugin and emits the ordinary focus notification when the user is
// returning to it. Callers that immediately switch plugin/project pass false so
// the hidden old surface cannot reopen a terminal during the handoff.
func (m *Model) leaveOverview(restoreProject bool) tea.Cmd {
	wasGlobal := m.inGlobalScope()
	var deckCmd tea.Cmd
	if wasGlobal {
		if h := m.currentContentDeck(); h != nil {
			h.releaseAppContentInputs()
			h.laidOut = false
			h.links = nil
			h.press = nil
			if h.live != nil {
				deckCmd = h.live.Reconcile()
			}
		}
	}
	if wasGlobal && m.overview != nil {
		m.overview.Stop()
	}
	m.scope = ScopeProject
	if wasGlobal {
		// Only a real crossing writes. leaveOverview is also the no-op prelude to
		// activating a project tab the user is already on, and a state write per
		// tab press is a cost the remembered scope does not need to charge.
		m.persistScope()
		if current := m.ActivePlugin(); current != nil && restoreProject {
			current.SetFocused(true)
		}
		m.updateContext()
		if restoreProject {
			return tea.Batch(deckCmd, PluginFocused())
		}
	}
	return deckCmd
}

// toggleOverview moves between the global and project spaces. No-op when the
// global space has no tab to show.
func (m *Model) toggleOverview() tea.Cmd {
	if !m.globalScopeAvailable() {
		return nil
	}
	if m.inGlobalScope() {
		return m.exitOverview()
	}
	return m.enterOverview()
}

func (m *Model) switchProjectWithInventory(projectPath string, inventory []WorktreeInfo) tea.Cmd {
	return m.switchProjectWithSelection(projectPath, inventory, nil, true)
}

func (m *Model) switchProjectWithSelection(projectPath string, inventory []WorktreeInfo, pending *plugin.PendingWorkspaceSelection, restoreLastWorktree bool) tea.Cmd {
	// Skip if already on this project. Silently: the user is looking at it
	// (audit row 17).
	if projectPath == m.ui.WorkDir {
		return nil
	}

	// Binding a project is the activity the switcher's "Last activity" column
	// reports. It is recorded here rather than in the switcher so every route
	// in — the modal, a `sidecar project switch` request, a restored target —
	// records the same event once.
	activityCmd := m.recordProjectActivityCmd(projectPath)

	// A caller-supplied selection is a hand-off like any other, so it goes
	// through the one slot rather than beside it. It supersedes a target parked
	// by an earlier jump: this switch is the newer request.
	if pending != nil {
		m.setPendingActivation(pendingActivation{selection: pending})
	}

	m.clearBoundDestination()

	// Save the active plugin state for the old project root
	oldWorkDir := m.ui.WorkDir
	if activePlugin := m.ActivePlugin(); activePlugin != nil {
		_ = state.SetActivePlugin(oldWorkDir, activePlugin.ID())
	}

	// Normalize old workdir for comparisons
	normalizedOldWorkDir, _ := normalizePath(oldWorkDir)

	// Check if target project has a saved worktree we should restore.
	// Only restore if projectPath is the main repo - if user explicitly chose a
	// specific worktree path (via worktree switcher), respect that choice.
	targetPath := projectPath
	targetMainRepo := mainWorktreePath(inventory)
	if targetMainRepo == "" {
		targetMainRepo = GetMainWorktreePath(projectPath)
	}
	if targetMainRepo != "" {
		normalizedProject, _ := normalizePath(projectPath)
		normalizedTargetMain, _ := normalizePath(targetMainRepo)

		// Only restore saved worktree if switching to the main repo path.
		// A pending worktree selection is an exact destination the user picked
		// in the global browser — including the main worktree of a project
		// whose last visit was somewhere else. Restoring that remembered
		// worktree here would open a neighbour of the item they chose, so an
		// explicit destination outranks the memory. A shell selection names no
		// worktree at all: shells are project-scoped and resolve identically
		// from any worktree, so the remembered worktree still wins there.
		if restoreLastWorktree && normalizedProject == normalizedTargetMain &&
			(pending == nil || pending.Kind != plugin.WorkspaceSelectionWorktree) {
			if savedWorktree := state.GetLastWorktreePath(normalizedTargetMain); savedWorktree != "" {
				// Don't restore if the saved worktree is where we're coming FROM
				// (user is explicitly leaving that worktree)
				if savedWorktree != normalizedOldWorkDir {
					// Verify saved worktree still exists
					if WorktreeExists(savedWorktree) {
						targetPath = savedWorktree
					} else {
						// Stale entry - clear it
						_ = state.ClearLastWorktreePath(normalizedTargetMain)
					}
				}
			}
		}

		// Save the final target as last active worktree for this repo
		normalizedTarget, _ := normalizePath(targetPath)
		_ = state.SetLastWorktreePath(normalizedTargetMain, normalizedTarget)
	}

	// Update the UI state
	m.ui.WorkDir = targetPath
	m.intro.RepoName = GetRepoName(targetPath)
	if len(inventory) > 0 {
		m.setWorktreeInventory(inventory, targetPath)
	} else {
		m.refreshWorktreeCache()
	}

	// Resolve project root (main worktree for linked worktrees, same as targetPath otherwise)
	newProjectRoot := mainWorktreePath(inventory)
	if newProjectRoot == "" {
		newProjectRoot = GetMainWorktreePath(targetPath)
	}
	if newProjectRoot == "" {
		newProjectRoot = targetPath
	}
	m.ui.ProjectRoot = newProjectRoot

	// Apply project-specific theme (or global fallback)
	resolved := theme.ResolveTheme(m.cfg, targetPath)
	themeCmd := m.applyResolvedTheme(resolved)

	// Reinitialize all plugins with the new working directory and project root
	// This stops all plugins, updates the context, and starts them again
	startCmds := m.registry.Reinit(targetPath, newProjectRoot)
	// The workspace lane tracker is project-scoped and Reinit deliberately
	// clears it. Re-offer the already-loaded cache to the new project; the
	// plugin filters ownership and waits for its asynchronous inventory before
	// treating absence as a real lane departure.
	startCmds = append(startCmds, func() tea.Msg {
		return notify.SeedLaneTrackersMsg{Notifications: append([]notify.Notification(nil), m.notificationCache...)}
	})
	// Reinit rebuilds every plugin, and a rebuilt surface knows nothing about
	// providers: no matchers, so resource keys stop underlining, and no
	// resolver, so a restored Resource tab waits for a readiness that already
	// happened. A describe pass runs once per process, so republishing here is
	// the only thing that puts the new surfaces back in the loop.
	if cmd := m.publishResourceProviders(); cmd != nil {
		startCmds = append(startCmds, cmd)
	}
	if themeCmd != nil {
		startCmds = append(startCmds, themeCmd)
	}
	// One hand-off slot, one apply site: a workspace selection this call was
	// given, or a target a cross-project jump parked before switching.
	startCmds = append(startCmds, m.applyPendingActivation()...)

	// Send the content box to all plugins so they recalculate layout/bounds.
	// Without this, plugins like td-monitor lose mouse interactivity because
	// their panel bounds are only calculated on WindowSizeMsg receipt.
	//
	// Reinit rebuilds every plugin, so a plugin that came back would otherwise
	// lay out against the full terminal and paint underneath an open
	// notification centre. emitContentSize restores the reservation here,
	// before the next frame — this is the project/worktree-switch half of the
	// promise that the panel survives all navigation.
	startCmds = append(startCmds, m.emitContentSize()...)

	// Reinit deliberately clears every plugin's focus-owned resources. Always
	// hand focus back explicitly, including on a project's first visit where no
	// saved plugin exists yet. Resolve the destination first and focus once so a
	// pending global-Workspaces navigation cannot start two notice/poll chains.
	focusPluginID := ""
	if active := m.ActivePlugin(); active != nil {
		focusPluginID = active.ID()
	}
	if saved := state.GetActivePluginForWorkDir(targetPath, newProjectRoot); saved != "" && m.registry.Get(saved) != nil {
		focusPluginID = saved
	}
	if pending != nil {
		focusPluginID = workspacePluginID
	}
	m.focusPluginByIDWithoutNotice(focusPluginID)

	// Retitle the terminal now rather than waiting for the next tick, so the
	// tab label changes at the same moment the UI does.
	titleCmd := m.syncTerminalTitle(false)
	var inventoryRefresh tea.Cmd
	if len(inventory) > 0 {
		inventoryRefresh = refreshWorktreeInventoryCmd(targetPath)
	}

	// Return batch of start commands plus a toast notification
	return tea.Batch(
		tea.Batch(startCmds...),
		activityCmd,
		// Configuration stays open across a project switch, so it is told where
		// it now is rather than left describing the project the user left.
		m.refreshConfigContext(),
		titleCmd,
		inventoryRefresh,
		m.announcePresenceCmd(),
		// Routine confirmation of a switch the user just made and can see:
		// a flash, not a stored notification (audit row 18).
		ShowFlash(fmt.Sprintf("Switched to %s", GetRepoName(targetPath))),
	)
}

// openInGitSwitchMsg switches to a checkout from the global list without
// using the current project's cached worktree inventory.
type openInGitSwitchMsg struct {
	Path string
}

// openInGitFromOverview leaves global and opens the Git plugin on the
// checkout the mini-diff showed. A missing path stays in global.
func (m *Model) openInGitFromOverview(path string) tea.Cmd {
	if path == "" || !WorktreeExists(path) {
		return func() tea.Msg {
			return ToastMsg{Message: "Worktree no longer exists", Duration: 3 * time.Second, IsError: true}
		}
	}
	normalizedPath, _ := normalizePath(path)
	normalizedWorkDir, _ := normalizePath(m.ui.WorkDir)
	if normalizedPath == normalizedWorkDir {
		return FocusPlugin("git-status")
	}
	// Sequence, not Batch: switch re-inits plugins and deadlocks if
	// FocusPlugin forks beside it. FocusPluginByIDMsg exits global.
	return tea.Sequence(
		func() tea.Msg { return openInGitSwitchMsg{Path: path} },
		FocusPlugin("git-status"),
	)
}

func (m *Model) navigateFromOverview(workspace workspaceinventory.Workspace) tea.Cmd {
	return m.navigateFromOverviewAction(workspace, "")
}

func (m *Model) navigateFromOverviewAction(workspace workspaceinventory.Workspace, action string) tea.Cmd {
	m.leaveOverview(false)
	kind := plugin.WorkspaceSelectionWorktree
	target := workspace.ProjectRoot
	key := workspace.Key
	if target == "" {
		target = workspace.Path
	}
	if workspace.Kind == workspaceinventory.KindWorktree && m.cfg.Plugins.Workspace.OverviewWorktreeScope == config.OverviewWorktreeScopeWorktree {
		target = workspace.Path
	}
	if workspace.Kind == workspaceinventory.KindShell {
		kind = plugin.WorkspaceSelectionShell
		key = workspace.TmuxName
	}
	pending := plugin.PendingWorkspaceSelection{Kind: kind, Key: key, Path: workspace.Path, Action: action}
	if workspaceinventory.CanonicalPath(target) == workspaceinventory.CanonicalPath(m.ui.WorkDir) {
		// No switch, so no Reinit to wait for — but the hand-off still goes
		// through the one slot, applied immediately, so the selection has a
		// single apply site whether or not a project switch is involved.
		m.setPendingActivation(pendingActivation{selection: &pending})
		applyCmds := m.applyPendingActivation()
		m.updateContext()
		// FocusPluginByID clears the slot; it is already empty by here.
		return tea.Batch(append(applyCmds, m.FocusPluginByID(workspacePluginID))...)
	}
	// Worktree cards name an exact destination, so the remembered worktree must
	// not override it. Shells are project-scoped and still open in whichever
	// worktree the project was last visited in.
	return m.switchProjectWithSelection(target, nil, &pending, kind == plugin.WorkspaceSelectionShell)
}

// previewProjectTheme applies the theme for the currently selected project in the switcher.
func (m *Model) previewProjectTheme() tea.Cmd {
	destinations := m.projectSwitcherFiltered
	if m.projectSwitcherCursor >= 0 && m.projectSwitcherCursor < len(destinations) {
		destination := destinations[m.projectSwitcherCursor]
		if destination.Kind == destinationOverview || destination.isRemote() {
			return m.applyResolvedTheme(theme.ResolveTheme(m.cfg, ""))
		}
		return m.applyResolvedTheme(theme.ResolveTheme(m.cfg, destination.Path))
	}
	return nil
}

// currentProjectConfig returns the ProjectConfig for the current workdir, or nil.
// If the current workdir is a worktree, it also checks if the main worktree path
// matches a registered project (so theme scope selector works from worktrees).
func (m *Model) currentProjectConfig() *config.ProjectConfig {
	if m.cfg == nil {
		return nil
	}
	// First, check direct match
	for i := range m.cfg.Projects.List {
		if m.cfg.Projects.List[i].Path == m.ui.WorkDir {
			return &m.cfg.Projects.List[i]
		}
	}

	// If not found, check if we're in a worktree and the main repo is registered
	mainPath := GetMainWorktreePath(m.ui.WorkDir)
	if mainPath != "" && mainPath != m.ui.WorkDir {
		for i := range m.cfg.Projects.List {
			if m.cfg.Projects.List[i].Path == mainPath {
				return &m.cfg.Projects.List[i]
			}
		}
	}

	return nil
}

// confirmThemeSelection saves the theme, reloads config, resets all theme
// switcher state, and returns a toast command. displayName is used in the toast.
func (m *Model) confirmThemeSelection(tc config.ThemeConfig, displayName string) tea.Cmd {
	scope := m.themeSwitcherScope

	// Save before reset clears scope
	if err := m.saveTheme(tc, scope); err != nil {
		m.resetThemeSwitcher()
		m.updateContext()
		return func() tea.Msg {
			return ToastMsg{Message: "Theme applied (save failed)", Duration: 3 * time.Second, IsError: true}
		}
	}
	var themeCmd tea.Cmd
	if cfg, err := config.Load(); err == nil {
		m.cfg = cfg
		themeCmd = m.applyResolvedTheme(theme.ResolveTheme(m.cfg, m.ui.WorkDir))
	}

	m.resetThemeSwitcher()
	m.updateContext()

	toastMsg := "Theme: " + displayName
	if scope == "project" {
		toastMsg += " (project)"
	} else {
		toastMsg += " (global)"
	}
	// The theme change is the confirmation; only the save-failed branch above
	// is worth keeping (audit row 21).
	return tea.Batch(themeCmd, ShowFlash(toastMsg))
}

// saveTheme persists a ThemeConfig based on scope.
func (m *Model) saveTheme(tc config.ThemeConfig, scope string) error {
	if scope == "project" {
		projectPath := m.ui.WorkDir
		if pc := m.currentProjectConfig(); pc != nil {
			projectPath = pc.Path
		}
		return config.SaveProjectTheme(projectPath, &tc)
	}
	return config.SaveGlobalTheme(tc)
}

// copyProjectSetupPrompt copies an LLM-friendly prompt for configuring projects.
func (m *Model) copyProjectSetupPrompt() tea.Cmd {
	prompt := `Configure sidecar projects for me.

Add my code projects to ~/.config/sidecar/config.json using this format:

{
  "projects": {
    "list": [
      {"name": "short-name", "path": "~/code/project-path"}
    ]
  }
}

Rules:
- Use short, memorable names (1-2 words, lowercase, hyphens ok)
- Expand ~ to full home path if needed
- Only add directories that exist and contain code
- Merge with existing config if present

My code is located at: [TELL ME WHERE YOUR CODE DIRECTORIES ARE]`

	return clip.Copy(prompt, func(r clip.Result) tea.Msg {
		return FlashMsg{Text: r.Message("Copied LLM setup prompt")}
	})
}

// initProjectAdd initializes the project add sub-mode.
func (m *Model) initProjectAdd() {
	m.projectAddMode = true
	m.clearProjectAddModal()

	// Opening the sub-flow takes the pointer away from the switcher list
	// (update.go routes every mouse event here once projectAddMode is set), so
	// a list-bar gesture dies at this boundary rather than settling late: real
	// routing never delivers the stray release to the parent handler, and a
	// moved drag must not spend its preview on it (the td-f63097 boundary,
	// parent side). State stays inert until the next genuine press.
	if m.projectSwitcherMouseHandler != nil && m.projectSwitcherMouseHandler.IsDragging() {
		m.projectSwitcherMouseHandler.EndDrag()
	}
	m.projectSwitcherBar = switcherBarState{}

	if m.projectAdd == nil {
		m.projectAdd = &projectAddState{}
	}
	m.projectAdd.errorMessage = ""
	m.projectAdd.themeSelected = ""

	nameInput := textinput.New()
	nameInput.Placeholder = "project-name"
	nameInput.CharLimit = 40
	nameInput.SetWidth(36)
	nameInput.Focus()
	m.projectAdd.nameInput = nameInput

	pathInput := textinput.New()
	pathInput.Placeholder = "~/code/project-path"
	pathInput.CharLimit = 200
	pathInput.SetWidth(36)
	m.projectAdd.pathInput = pathInput
}

// resetProjectAdd resets the project add sub-mode state.
func (m *Model) resetProjectAdd() {
	m.projectAddMode = false
	if m.projectAdd != nil {
		m.projectAdd.errorMessage = ""
		m.projectAdd.themeSelected = ""
	}
	m.clearProjectAddModal()
	m.resetProjectAddThemePicker()
}

// initProjectAddThemePicker opens the theme picker sub-modal.
func (m *Model) initProjectAddThemePicker() {
	m.projectAddThemeMode = true
	ti := textinput.New()
	ti.Placeholder = "Filter themes..."
	ti.Focus()
	ti.CharLimit = 50
	ti.SetWidth(36)
	m.projectAddThemeInput = ti
	m.projectAddThemeFiltered = append([]string{"(use global)"}, styles.ListThemes()...)
	m.projectAddThemeCursor = 0
	m.projectAddThemeScroll = 0
}

// resetProjectAddThemePicker closes the theme picker sub-modal.
func (m *Model) resetProjectAddThemePicker() {
	m.projectAddThemeMode = false
	m.projectAddThemeCursor = 0
	m.projectAddThemeScroll = 0
}

// previewProjectAddTheme previews the currently-selected theme.
func (m *Model) previewProjectAddTheme() tea.Cmd {
	if m.projectAddThemeCursor >= 0 && m.projectAddThemeCursor < len(m.projectAddThemeFiltered) {
		name := m.projectAddThemeFiltered[m.projectAddThemeCursor]
		if name == "(use global)" {
			resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
			return m.applyResolvedTheme(resolved)
		}
		return m.applyResolvedTheme(theme.ResolvedTheme{BaseName: name})
	}
	return nil
}

// validateProjectAdd validates the project add form inputs.
// Returns an error message or empty string if valid.
func (m *Model) validateProjectAdd() string {
	if m.projectAdd == nil {
		return "Name is required"
	}

	// The rules are shared with Configuration's Add Project route, so the two
	// add journeys cannot drift into accepting different things.
	return config.ValidateProject(
		m.cfg.Projects.List,
		m.projectAdd.nameInput.Value(),
		m.projectAdd.pathInput.Value(),
		-1,
	)
}

// saveProjectAdd saves the new project to config and refreshes the list.
func (m *Model) saveProjectAdd() tea.Cmd {
	if m.projectAdd == nil {
		return func() tea.Msg {
			return ToastMsg{Message: "Project add state missing", Duration: 3 * time.Second, IsError: true}
		}
	}

	name := strings.TrimSpace(m.projectAdd.nameInput.Value())
	path := strings.TrimSpace(m.projectAdd.pathInput.Value())

	// Build project config
	proj := config.ProjectConfig{
		Name: name,
		Path: config.ExpandPath(path),
	}

	// Add theme if user selected one
	if m.projectAdd.themeSelected != "" && m.projectAdd.themeSelected != "(use global)" {
		proj.Theme = &config.ThemeConfig{
			Name: m.projectAdd.themeSelected,
		}
	}

	// Reload config from disk to avoid overwriting external changes
	cfg, err := config.Load()
	if err != nil {
		return func() tea.Msg {
			return ToastMsg{Message: "Failed to load config: " + err.Error(), Duration: 3 * time.Second, IsError: true}
		}
	}

	// Add project to fresh config
	cfg.Projects.List = append(cfg.Projects.List, proj)

	// Save to disk
	if err := config.Save(cfg); err != nil {
		return func() tea.Msg {
			return ToastMsg{Message: "Added project (save failed: " + err.Error() + ")", Duration: 3 * time.Second, IsError: true}
		}
	}

	// Update in-memory config
	m.cfg.Projects.List = cfg.Projects.List

	// Refresh the filtered list
	m.setProjectSwitcherCollection("")

	// The new row in the switcher is the confirmation (audit row 26), and a
	// live global catalog picks the project up immediately instead of waiting
	// for a tab transition or a restart.
	return tea.Batch(ShowFlash(fmt.Sprintf("Added project: %s", name)), m.syncOverviewProjects())
}

// resetThemeSwitcher resets the theme switcher modal state.
func (m *Model) resetThemeSwitcher() {
	m.showThemeSwitcher = false
	m.themeSwitcherSelectedIdx = 0
	m.themeSwitcherFiltered = nil
	m.themeSwitcherScope = ""
	m.themeSwitcherOriginal = themeEntry{}
	m.clearThemeSwitcherModal()
}

// clearThemeSwitcherModal clears the theme switcher modal state. A scrollbar
// gesture live on this modal's handler ends here — closing mid-drag must not
// leave a dead anchor behind (the td-f63097 boundary, from the switcher's side).
func (m *Model) clearThemeSwitcherModal() {
	if m.themeSwitcherMouseHandler != nil && m.themeSwitcherMouseHandler.IsDragging() {
		m.themeSwitcherMouseHandler.EndDrag()
	}
	m.themeSwitcherBar = switcherBarState{}
	m.themeSwitcherModal = nil
	m.themeSwitcherModalWidth = 0
	m.themeSwitcherMouseHandler = nil
}

// openIssueInput opens the issue lookup modal and reports that it took the
// keyboard. It is the one entry point, so the palette reaches the same modal as
// the key — which is what keeps the capability reachable from the contexts that
// bind "i" for themselves.
func (m *Model) openIssueInput() bool {
	if m.hasModal() {
		return false
	}
	m.showIssueInput = true
	m.activeContext = "issue-input"
	m.initIssueInput()
	return true
}

// runHostCommand runs a command sidecar's own key handler implements, naming it
// by the ID the default bindings advertise. It returns the command the work
// raised — opening Configuration starts its readiness run in one — and reports
// whether the ID was one of them. A caller that dropped the command would open
// Configuration onto checks that never ran.
func (m *Model) runHostCommand(id string) (tea.Cmd, bool) {
	switch id {
	case "open-issue":
		return nil, m.openIssueInput()
	case "open-configuration":
		// No page named: the palette command toggles and resumes where the user
		// last was, exactly as the gear and `,` do.
		return m.toggleConfiguration(), true
	case "quit":
		m.initQuitModal()
		m.showQuitConfirm = true
		return nil, true
	case "switch-project":
		m.showProjectSwitcher = true
		m.initProjectSwitcher()
		m.activeContext = "project-switcher"
		return nil, true
	case "switch-worktree":
		return m.openWorktreeSwitcher(), true
	case "switch-theme":
		m.showThemeSwitcher = true
		m.initThemeSwitcher()
		m.activeContext = "theme-switcher"
		return nil, true
	case "open-in":
		m.showOpenIn = true
		m.initOpenIn()
		m.activeContext = "open-in"
		return nil, true
	case "toggle-overview":
		return m.toggleOverview(), true
	case "toggle-notifications":
		if m.notificationCentreVisible() && !m.notificationCentreFocused {
			m.focusNotificationCentre()
			m.readSelectedNotification()
			return nil, true
		}
		return m.toggleNotificationCentre(), true
	case "expand-toast":
		if m.toggleToastExpand() {
			return m.syncToastReveal(time.Now()), true
		}
		return nil, true
	case "toggle-diagnostics":
		m.showDiagnostics = true
		m.activeContext = "diagnostics"
		return tea.Batch(m.productCheckCmds(true)...), true
	case "refresh":
		return Refresh(), true
	case "next-plugin":
		return m.cycleTabs(1), true
	case "prev-plugin":
		return m.cycleTabs(-1), true
	case "focus-plugin-1":
		return m.selectProjectTabByNumber(0), true
	case "focus-plugin-2":
		return m.selectProjectTabByNumber(1), true
	case "focus-plugin-3":
		return m.selectProjectTabByNumber(2), true
	case "focus-plugin-4":
		return m.selectProjectTabByNumber(3), true
	case "focus-plugin-5":
		return m.selectProjectTabByNumber(4), true
	case "focus-plugin-6":
		return m.selectProjectTabByNumber(5), true
	case "focus-plugin-7":
		return m.selectProjectTabByNumber(6), true
	case "focus-sessions":
		return m.selectGlobalTab(GlobalSessions), true
	case "focus-activity":
		return m.selectGlobalTab(GlobalActivity), true
	}
	// A hosted global plugin's palette command is focus-<descriptor id>, so a
	// second global plugin needs no new case here — focus-tasks is one value of
	// this rule rather than a special case.
	if surfaceID, ok := strings.CutPrefix(id, "focus-"); ok {
		if _, exists := m.globalSurfaceByID(surfaceID); exists {
			return m.selectGlobalTab(surfaceID), true
		}
	}
	return nil, false
}

// runGlobalWorkspacesCommand runs a palette-selected command the global
// Workspaces list answers itself. Keys are handled in overview.WorkspacesKey;
// the palette has no keymap handler, so it lands here.
func (m *Model) runGlobalWorkspacesCommand(id string) tea.Cmd {
	if !m.globalWorkspacesVisible() || m.overview == nil || m.overview.PreviewInteractive() {
		return nil
	}
	switch id {
	case "confirm-delete", "cancel":
		if m.overview.DeleteOpen() {
			return m.overview.RunDeleteCommand(id)
		}
		return nil
	case "delete-shell":
		return m.overview.OpenDeleteSelectedShell()
	case "delete-worktree":
		return m.overview.OpenDeleteSelectedWorktree()
	case "merge-workflow":
		return m.overview.StartSelectedMerge()
	case "new-worktree":
		return m.overview.OpenCreateWorktree("")
	case "new-shell":
		return m.overview.OpenCreateShell("")
	case "rename-shell":
		return m.overview.OpenRenameShell()
	case "rename-worktree":
		return m.overview.OpenRenameWorktree()
	case "open-in-git":
		return m.overview.OpenSelectedInGit()
	default:
		return nil
	}
}

// initIssueInput initializes the issue input modal.
func (m *Model) initIssueInput() {
	ti := textinput.New()
	ti.Placeholder = "Issue ID or search text"
	ti.Focus()
	ti.CharLimit = 50
	ti.SetWidth(50)
	m.issueInputInput = ti
	m.issueInputModal = nil
	m.issueInputModalWidth = 0
	m.issueInputMouseHandler = mouse.NewHandler()
	m.issueSearchResults = nil
	m.issueSearchQuery = ""
	m.issueSearchLoading = false
	m.issueSearchCursor = -1
	m.issueSearchScrollOffset = 0
	m.issueSearchIncludeClosed = false
}

// resetIssueInput resets the issue input modal state.
func (m *Model) resetIssueInput() {
	m.showIssueInput = false
	m.issueInputModal = nil
	m.issueInputModalWidth = 0
	m.issueInputMouseHandler = nil
	m.issueSearchResults = nil
	m.issueSearchQuery = ""
	m.issueSearchLoading = false
	m.issueSearchCursor = -1
	m.issueSearchScrollOffset = 0
	m.issueSearchIncludeClosed = false
}

// resetIssuePreview resets the issue preview modal state.
func (m *Model) resetIssuePreview() {
	m.stopIssuePreviewWatch()
	m.showIssuePreview = false
	m.issuePreviewView = nil
	m.issuePreviewData = nil
	m.issuePreviewLoading = false
	m.issuePreviewError = nil
	m.issuePreviewModal = nil
	m.issuePreviewModalWidth = 0
	m.issuePreviewModalHeight = 0
	m.issuePreviewMouseHandler = nil
	if m.issuePreviewWheel != nil {
		// A held delta belongs to the issue being closed, never to whatever
		// card the modal shows next.
		m.issuePreviewWheel.Reset()
	}
}

// backToIssueInput closes the preview and returns to the search modal
// with the previous query, results, and cursor intact.
func (m *Model) backToIssueInput() {
	m.resetIssuePreview()
	m.showIssueInput = true
	m.activeContext = "issue-input"
	m.issueInputInput.Focus()
	m.issueInputModal = nil
	m.issueInputModalWidth = 0
	m.issueInputMouseHandler = mouse.NewHandler()
}

// initThemeSwitcher initializes the theme switcher modal.
func (m *Model) initThemeSwitcher() {
	ti := textinput.New()
	ti.Placeholder = "Filter themes..."
	ti.Focus()
	ti.CharLimit = 50
	ti.SetWidth(54)
	m.themeSwitcherInput = ti

	allEntries := buildUnifiedThemeList()
	m.themeSwitcherFiltered = allEntries
	m.themeSwitcherSelectedIdx = 0
	if m.currentProjectConfig() != nil {
		m.themeSwitcherScope = "project"
	} else {
		m.themeSwitcherScope = "global"
	}
	m.clearThemeSwitcherModal()

	// Determine original theme from config
	// With no recorded choice the running theme is the fresh-install one, so
	// that is what the switcher must open on.
	m.themeSwitcherOriginal = themeEntry{
		Name:      styles.GetTheme(styles.FreshInstallTheme).DisplayName,
		IsBuiltIn: true,
		ThemeKey:  styles.FreshInstallTheme,
	}
	if freshCfg, err := config.Load(); err == nil {
		if freshCfg.UI.Theme.Community != "" {
			// Current theme is a community theme
			communityName := freshCfg.UI.Theme.Community
			m.themeSwitcherOriginal = themeEntry{Name: communityName, IsBuiltIn: false, ThemeKey: communityName}
		} else if freshCfg.UI.Theme.Name != "" {
			name := freshCfg.UI.Theme.Name
			displayName := name
			if t := styles.GetTheme(name); t.DisplayName != "" {
				displayName = t.DisplayName
			}
			m.themeSwitcherOriginal = themeEntry{Name: displayName, IsBuiltIn: true, ThemeKey: name}
		}
	}

	// Set cursor to current theme
	for i, entry := range m.themeSwitcherFiltered {
		if entry.IsBuiltIn == m.themeSwitcherOriginal.IsBuiltIn && entry.ThemeKey == m.themeSwitcherOriginal.ThemeKey {
			m.themeSwitcherSelectedIdx = i
			break
		}
	}
}

// previewThemeEntry applies the given theme entry for live preview.
func (m *Model) previewThemeEntry(entry themeEntry) tea.Cmd {
	if entry.IsBuiltIn {
		return m.applyThemeFromConfig(entry.ThemeKey)
	}
	return m.applyResolvedTheme(theme.ResolvedTheme{
		BaseName:      "default",
		CommunityName: entry.ThemeKey,
	})
}

// applyThemeFromConfig applies a theme, using config overrides only if the
// saved config has that theme selected. This means live preview of other themes
// won't include user customizations (which is intentional - you want to see the
// base theme, not your customizations for a different theme).
func (m *Model) applyThemeFromConfig(themeName string) tea.Cmd {
	freshCfg, err := config.Load()
	if err == nil && freshCfg.UI.Theme.Name == themeName {
		// Apply the saved theme with its full config (community + overrides)
		return m.applyResolvedTheme(theme.ResolvedTheme{
			BaseName:      themeName,
			CommunityName: freshCfg.UI.Theme.Community,
			Overrides:     freshCfg.UI.Theme.Overrides,
		})
	}
	return m.applyThemeName(themeName)
}

// applyResolvedTheme applies a resolved theme to the styles system and notifies plugins.
func (m *Model) applyResolvedTheme(resolved theme.ResolvedTheme) tea.Cmd {
	theme.ApplyResolved(resolved)
	return m.notifyThemeChanged()
}

// applyThemeName applies a theme by name to the styles system and notifies plugins.
func (m *Model) applyThemeName(name string) tea.Cmd {
	styles.ApplyTheme(name)
	return m.notifyThemeChanged()
}

// notifyThemeChanged synchronously delivers msg.ThemeChangedMsg to all plugins
// and app-owned global hosts so immediate frames and inactive tabs are up to date.
// It is a no-op when the resolved styles snapshot has not changed.
func (m *Model) notifyThemeChanged() tea.Cmd {
	current := styles.GetCurrentTheme()
	if reflect.DeepEqual(m.lastNotifiedTheme, current) {
		return nil
	}
	m.lastNotifiedTheme = current

	themeMsg := msg.ThemeChangedMsg{}
	var cmds []tea.Cmd
	if m.registry != nil {
		for i, p := range m.registry.Plugins() {
			newPlugin, cmd := p.Update(themeMsg)
			m.registry.Replace(i, newPlugin)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if cmd := m.updateGlobalHosts(themeMsg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if m.overview != nil {
		if cmd := m.overview.Update(themeMsg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// fetchDetectionManifestsCmd returns the command that checks Herdr's published
// catalog for newer agent-detection manifests — after the first ready frame,
// never before it, and at most once a day.
//
// Three rules are load-bearing here and each one is a rule rather than a
// preference:
//
//   - Nothing runs unless detection.remoteManifests names a catalog. With the
//     setting off this returns nil, so there is no command, no goroutine, no
//     state-directory read, and no HTTP client. manifests.HTTPClientsBuilt is
//     the instrument that proves the last of those in a test.
//   - The work waits on firstReadyFrameLatch, for the reason the resource
//     providers do: a command returned from Init can start before the first
//     render, so "start it from Init" is not by itself a promise about
//     ordering. The latch is.
//   - A failure is never user-visible. The command returns no message at all;
//     what a check learned is in the status file, which `sidecar agent
//     manifests` prints and `sidecar agent explain` reflects in its version
//     lines. A pane's badge is never blocked, delayed, or reddened by a catalog
//     being down.
func fetchDetectionManifestsCmd(cfg *config.Config) tea.Cmd {
	if cfg == nil || !cfg.Detection.RemoteManifestsEnabled() {
		return nil
	}
	detection := cfg.Detection
	return func() tea.Msg {
		<-firstReadyFrameLatch.wait()
		defer startuptrace.Begin("detection manifests: catalog fetch")()

		result, err := manifests.FetchFromConfig(context.Background(), detection, manifests.FetchOptions{})
		switch {
		case err != nil:
			slog.Warn("detection manifests: catalog check failed", "error", err)
		case result.Skipped:
			slog.Debug("detection manifests: catalog check skipped", "reason", result.Reason)
		case len(result.Updated) > 0:
			slog.Info("detection manifests: updated from the catalog", "agents", result.Updated)
		}
		// No message. Nothing in the UI is waiting on this, and a message with
		// no handler is a message someone later wires up by accident.
		return nil
	}
}
