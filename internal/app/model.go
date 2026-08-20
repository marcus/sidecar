package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/clip"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/configui"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/issueview"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/livewatch"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/overview"
	"github.com/marcus/sidecar/internal/palette"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/theme"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/version"
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
	ModalIssuePreview                      // Issue preview display (lowest priority)
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
	Kind    string
	Name    string
	Path    string
	Project *config.ProjectConfig
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
	projectSwitcherAddFocused   bool // the + (add project) button holds focus

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
	worktreeSwitcherFiltered     []WorktreeInfo
	worktreeSwitcherAll          []WorktreeInfo
	worktreeSwitcherModal        *modal.Modal
	worktreeSwitcherModalWidth   int
	worktreeSwitcherMouseHandler *mouse.Handler
	worktreeCheckCounter         int // Counter for periodic worktree existence check
	worktreeInventoryCounter     int // Counter for periodic worktree inventory refresh

	// Worktree info cache (avoids git subprocess forks on every View render)
	cachedWorktreeInfo      *WorktreeInfo
	cachedWorktreeInventory []WorktreeInfo

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

	// Header/footer
	ui *UIState

	// Error handling
	lastError error

	// Ready state
	ready              bool
	applicationFocused bool

	// Version info. One discovered target per product (Sidecar, td, and — only
	// when the tasks_plugin feature is effectively enabled — Tasks), rather
	// than parallel per-product fields.
	currentVersion string
	products       []version.Target
	updateNotes    string // Sidecar release notes; they describe Sidecar only

	// Update feature state
	updateInProgress bool
	needsRestart     bool

	// Confirmed batch. updatePlan is immutable once confirmed; updateResults
	// records the settled outcome of every attempted target so a later failure
	// cannot erase an earlier success. updatePlanID rejects stale async
	// messages from an abandoned or superseded batch.
	updatePlan      []version.Target
	updateResults   []version.Result
	updateCarried   []version.Result // outcomes carried over from an earlier batch
	updatePlanID    int
	updateActiveIdx int

	// Update modal state
	updateModalState      UpdateModalState
	updateStartTime       time.Time
	updateChangelog       string // Full changelog content
	changelogVisible      bool
	changelogScrollOffset int
	changelogScrollState  *changelogViewState // Shared state for modal closure

	// Update modal (declarative)
	updatePreviewModal         *modal.Modal
	updatePreviewModalWidth    int
	updatePreviewMouseHandler  *mouse.Handler
	updateCompleteModal        *modal.Modal
	updateCompleteModalWidth   int
	updateCompleteMouseHandler *mouse.Handler
	updateErrorModal           *modal.Modal
	updateErrorModalWidth      int
	updateErrorMouseHandler    *mouse.Handler
	changelogModal             *modal.Modal
	changelogModalWidth        int
	changelogMouseHandler      *mouse.Handler
	changelogRenderedLines     []string // Cached rendered changelog lines
	changelogMaxVisibleLines   int      // Max lines visible in viewport

	// Intro animation
	intro IntroModel

	// Scope state. The app owns the current project and project plugin; these
	// three fields own the global space that can be shown over it. Overview is
	// an app-owned destination, not a project plugin, and is only constructed
	// while its feature is enabled.
	overview    *overview.Model
	scope       AppScope
	globalTab   GlobalTab
	globalTasks *globalTasksHost

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
	if watcher, err := uirequest.NewWatcher(config.StateDir()); err == nil {
		m.uiRequestWatcher = watcher
	}
	// Bind the `notifications` config section before the store opens: the
	// store completes every record it is handed, and completion is where a
	// per-source expiry is applied.
	notify.ApplyConfig(cfg.Notifications)
	m.notifications = openNotificationStore()
	m.refreshNotifications()
	m.notificationCentreMouse = mouse.NewHandler()
	m.toastMouse = mouse.NewHandler()
	if tab, ok := parseGlobalTabID(state.GetLastGlobalTab()); ok {
		m.globalTab = tab
	}
	if features.IsEnabled(features.CrossProjectOverview.Name) {
		m.overview = overview.New(workspaceinventory.Collector{})
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
	if features.IsEnabled(features.TasksPlugin.Name) {
		// Tasks is a global tab, so its host is built here rather than
		// registered as a project plugin. Constructing it does no I/O.
		m.globalTasks = newGlobalTasksHost(reg.Context(), km)
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&m)
		}
	}
	return m
}

func announceInstanceCmd(workDir, projectRoot string) tea.Cmd {
	return func() tea.Msg {
		inst := uirequest.Instance{
			PID:       os.Getpid(),
			Host:      uirequest.HostName(),
			WorkDir:   workDir,
			StartedAt: time.Now().UTC(),
		}
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
		_ = uirequest.Announce(config.StateDir(), inst)
		return nil
	}
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
		announceInstanceCmd(m.ui.WorkDir, m.ui.ProjectRoot),
	}
	cmds = append(cmds, m.productCheckCmds(false)...)
	if cmd := defaultThemeNoticeCmd(m.cfg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Mark the startup plugin focused. SetActivePlugin only runs when the user
	// switches tabs, so without this the initial tab reports itself unfocused —
	// which, among other things, makes it poll at the slower unfocused interval.
	// Deliberately no PluginFocused() broadcast: several plugins refresh on that
	// message without checking their own focus flag, which would duplicate their
	// Start() work on the startup path.
	if p := m.ActivePlugin(); p != nil {
		p.SetFocused(true)
	}

	// Start all registered plugins
	for _, cmd := range m.registry.Start() {
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// The global Tasks host starts alongside them and outlives them: it is not
	// in the registry, so a later project switch cannot stop or rebuild it. Its
	// model is built by the returned command, i.e. after the first frame.
	if cmd := m.globalTasks.start(); cmd != nil {
		cmds = append(cmds, cmd)
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

	// A launch command's destination opens through the same message an empty
	// state sends, so there is one way into Configuration and one way back out.
	if m.startupConfigPage != "" {
		page := m.startupConfigPage
		cmds = append(cmds, func() tea.Msg { return OpenConfigurationMsg{Page: page} })
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
	m.projectSwitcherAddFocused = false
	m.clearProjectSwitcherModal()
	m.resetProjectAdd()
	// Restore current project's theme (undo any live preview)
	resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
	return m.applyResolvedTheme(resolved)
}

// clearProjectSwitcherModal clears the modal cache.
func (m *Model) clearProjectSwitcherModal() {
	m.projectSwitcherModal = nil
	m.projectSwitcherModalWidth = 0
	m.projectSwitcherMouseHandler = nil
}

// initProjectSwitcher initializes the project switcher modal.
func (m *Model) initProjectSwitcher() {
	m.clearProjectSwitcherModal()
	ti := textinput.New()
	ti.Placeholder = "Filter projects..."
	ti.Focus()
	ti.CharLimit = 50
	ti.SetWidth(40)
	m.projectSwitcherInput = ti
	m.projectSwitcherFiltered = m.projectSwitcherDestinations("")
	m.projectSwitcherCursor = 0
	m.projectSwitcherScroll = 0
	m.projectSwitcherAddFocused = false

	// Set cursor to current project if found
	for i, destination := range m.projectSwitcherFiltered {
		if m.inGlobalScope() && destination.Kind == destinationOverview {
			m.projectSwitcherCursor = i
			break
		}
		matchesCurrent := destination.Path == m.ui.WorkDir
		if m.overview != nil {
			matchesCurrent = matchesCurrent || destination.Path == m.ui.ProjectRoot
		}
		if !m.inGlobalScope() && destination.Kind == destinationProject && matchesCurrent {
			m.projectSwitcherCursor = i
			break
		}
	}
	// Preview the initially-selected project's theme
	m.previewProjectTheme()
}

func (m *Model) projectSwitcherDestinations(query string) []projectSwitcherDestination {
	projects := filterProjects(m.cfg.Projects.List, query)
	result := make([]projectSwitcherDestination, 0, len(projects)+1)
	if m.globalScopeAvailable() {
		result = append(result, projectSwitcherDestination{Kind: destinationOverview, Name: "Overview"})
	}
	for i := range projects {
		project := projects[i]
		result = append(result, projectSwitcherDestination{Kind: destinationProject, Name: project.Name, Path: project.Path, Project: &project})
	}
	return result
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
	m.projectSwitcherFiltered = m.projectSwitcherDestinations(m.projectSwitcherInput.Value())
	m.clearProjectSwitcherModal()

	if m.projectSwitcherCursor >= len(m.projectSwitcherFiltered) {
		m.projectSwitcherCursor = len(m.projectSwitcherFiltered) - 1
	}
	if m.projectSwitcherCursor < 0 {
		m.projectSwitcherCursor = 0
	}
	m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, 0, 8)
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
	m.resetProjectSwitcher()
	// The user picked a destination by hand, which outranks any parked jump.
	m.clearPendingActivation()
	if destination.Kind == destinationOverview && m.globalScopeAvailable() {
		return m.enterOverview()
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
	m.scope = ScopeGlobal
	m.ensureVisibleGlobalTab()
	m.updateContext()
	return m.startVisibleGlobalTab()
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
	if wasGlobal && m.overview != nil {
		m.overview.Stop()
	}
	m.scope = ScopeProject
	if wasGlobal {
		if current := m.ActivePlugin(); current != nil && restoreProject {
			current.SetFocused(true)
		}
		m.updateContext()
		if restoreProject {
			return PluginFocused()
		}
	}
	return nil
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

	// A caller-supplied selection is a hand-off like any other, so it goes
	// through the one slot rather than beside it. It supersedes a target parked
	// by an earlier jump: this switch is the newer request.
	if pending != nil {
		m.setPendingActivation(pendingActivation{selection: pending})
	}

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
		// Configuration stays open across a project switch, so it is told where
		// it now is rather than left describing the project the user left.
		m.refreshConfigContext(),
		titleCmd,
		inventoryRefresh,
		announceInstanceCmd(m.ui.WorkDir, m.ui.ProjectRoot),
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
		if destination.Kind == destinationOverview {
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
	m.projectSwitcherFiltered = m.projectSwitcherDestinations("")

	// The new row in the switcher is the confirmation (audit row 26).
	return ShowFlash(fmt.Sprintf("Added project: %s", name))
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

// clearThemeSwitcherModal clears the theme switcher modal state.
func (m *Model) clearThemeSwitcherModal() {
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
// and the global tasks host so immediate frames and inactive tabs are up to date.
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
	if cmd := m.globalTasks.update(themeMsg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}
