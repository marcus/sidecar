package configui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/agentintegration"
	"github.com/marcus/sidecar/internal/configchecks"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/version"
)

// Keymap contexts owned by Configuration. They are registered in
// internal/keymap/bindings.go, which is what drives the footer, the help modal,
// and the command palette.
const (
	// ContextConfig is sidebar navigation and page-level actions.
	ContextConfig = "config"
	// ContextConfigEdit is an active editor — Search, or any future field.
	// Registered in the host's isTextInputContext so typing never leaks.
	ContextConfigEdit = "config-edit"
	// ContextConfigConfirm is a consequential change awaiting confirmation.
	ContextConfigConfirm = "config-confirm"
)

// Mouse region IDs. Later phases add their own with the same prefix.
const (
	regionSearch    = "config-search"
	regionNavPrefix = "config-nav-"
	regionResult    = "config-result-"
	regionBack      = "config-back"
)

const (
	sidebarPreferredWidth = 39
	sidebarMinWidth       = 22
)

// Model is the Configuration surface. It owns navigation, search, and the
// per-surface mouse hit map; it owns no configuration data.
type Model struct {
	router  *router
	search  textinput.Model
	focus   focusTarget
	results bool // the detail pane is showing search results

	cursor int // index into visiblePages()

	// Detail-pane state. controls is rebuilt on every render, so the keyboard
	// can only reach what is currently on screen.
	controls    []control
	rowCursor   int
	detailFocus bool
	// flagsScroll is the first painted line in Feature Flags. That registry can
	// grow independently of configui, so the page follows its focused control
	// instead of letting clampLines hide reachable rows below the pane.
	flagsScroll int
	// saved is the parent selection each open child route will return to.
	saved []savedFocus

	// Readiness state. checks is a cache filled by a command; nothing here is
	// computed while rendering.
	checkInput configchecks.Input
	checks     configchecks.Results
	checked    bool
	checking   bool

	// showColorSteps expands the terminal-colors repair's instructions.
	showColorSteps bool

	// repairOpenedOK records that the open repair route's check was already
	// passing when the route opened, so a later Recheck does not read as "the
	// problem is resolved" and close a screen the user asked to read.
	repairOpenedOK bool

	// Environment probe. Like checks, this is a cache filled by a command:
	// whether an integration's command is on PATH is a fact about the machine,
	// never something a render looks up.
	probes    map[string]commandProbe
	brewFound bool
	goFound   bool
	probed    bool

	// pluginDescriptors is the plugin catalog the Panels page renders. It is
	// injected by the host: the plugin packages import internal/app, which owns
	// this surface, so reading the catalog here directly would be a cycle.
	pluginDescriptors []plugin.Descriptor

	// installEnv is the process environment the enable route installs through.
	// Nil means the real one; a test substitutes it so no test ever runs a
	// package manager.
	installEnv *version.Environment

	// restartNote is the inline "takes effect after Sidecar restarts" line a
	// restart-scoped save leaves behind. It belongs to the page that raised it
	// and is dropped when the user moves somewhere else.
	restartNote string

	// host is what the running Sidecar told the surface about itself.
	host HostState
	// editor is the field that owns typed characters, if any.
	editor *editorState
	// dropdown is the select control's list floating over the page, if any. It
	// is the innermost thing on screen while it is open: it owns the arrows,
	// Enter, and Escape, and it is composited over the detail pane rather than
	// pushing the page around.
	dropdown *dropdownState
	// pendingFocus is a control to put the row cursor on as soon as it is
	// rendered.
	pendingFocus string
	// focusedID is the id of the control the row cursor was on when the pane
	// was last rebuilt. It is the cursor's identity across a rebuild, which the
	// row cursor's index alone is not: a list that scrolls or gains a divider
	// declares a different control at the same index on the next frame.
	focusedID string
	// pending holds commands raised outside a key's own return path — a
	// completion request started by a keystroke inside a field, above all.
	pending []tea.Cmd

	// Page state. Each is built lazily against the running configuration and
	// dropped when Configuration closes.
	appearanceState        *appearanceState
	projectsState          *projectsState
	remotesState           *remotesState
	agentsState            *agentsState
	agentIntegrationsState *agentIntegrationsState
	notificationsState     *notificationsState
	terminalState          *terminalState
	panelsState            *panelsState
	advancedState          *advancedState
	aboutState             *aboutState
	enable                 *enableState
	// installing is a confirmed install whose route was left while it ran. The
	// attempt is the user's own, so its outcome is still announced and a
	// successful one still enables the panel.
	installing *enableState
	addProject *projectForm
	// remoteForm is an open Add/Edit host draft.
	remoteForm *remoteForm
	// confirm is a consequential change awaiting an explicit yes.
	confirm *confirmState

	// integrationService overrides the agent-integration application service.
	// It is nil in the running application, which builds the real one on
	// demand; a test injects one over a temporary tree.
	integrationService *agentintegration.Service

	width, height int
	sidebarWidth  int

	mouse   *mouse.Handler
	hoverID string

	// resume is where the user was when Configuration last closed. It lives for
	// the process, not on disk: reopening the surface in the same session should
	// not throw away the section you were reading, and a fresh Sidecar should
	// still start on Setup.
	resume resumeState
}

// resumeState is the position an unnamed open restores. The sidebar cursor is
// deliberately not part of it: it is an index into the list the sidebar was
// showing, which a live query makes a different list, so Reopen derives it from
// the page instead of restoring an index that may point somewhere else.
type resumeState struct {
	page        PageID
	rowCursor   int
	detailFocus bool
	valid       bool
}

// savedFocus is a parent route's detail-pane selection, kept while a child
// route is open.
type savedFocus struct {
	rowCursor   int
	detailFocus bool
}

type focusTarget uint8

const (
	focusSidebar focusTarget = iota
	focusSearch
)

// New builds the surface. It performs no I/O, so it is safe on the startup
// path; nothing here runs until the user opens Configuration.
func New() *Model {
	input := textinput.New()
	input.Placeholder = "Find a setting…"
	input.Prompt = ""
	m := &Model{
		router: newRouter(DefaultPage),
		search: input,
		mouse:  mouse.NewHandler(),
	}
	return m
}

// Open resets the surface onto a destination. Configuration always opens with
// the sidebar focused and no query, so Up/Down works immediately.
func (m *Model) Open(page PageID) {
	if PageTitle(page) == "" {
		page = DefaultPage
	}
	// Re-opening Configuration on top of an abandoned preview would strand the
	// previewed theme: the page state that knows how to put it back is dropped
	// below, so the restore happens before anything is dropped.
	m.restoreActivePreview()
	m.router = newRouter(page)
	m.search.SetValue("")
	m.search.Blur()
	m.focus = focusSidebar
	m.results = false
	m.hoverID = ""
	m.cursor = indexOfPage(m.visiblePages(), page)
	m.saved = nil
	m.appearanceState = nil
	m.projectsState = nil
	m.remotesState = nil
	m.agentsState = nil
	m.agentIntegrationsState = nil
	m.notificationsState = nil
	m.terminalState = nil
	m.panelsState = nil
	m.advancedState = nil
	m.aboutState = nil
	m.addProject = nil
	m.remoteForm = nil
	// A confirmed install already in flight belongs to the user, not this
	// Open. Dropping it would swallow the result if Configuration is closed
	// and reopened while Homebrew or go is still working.
	inFlight := m.installing
	if m.enable != nil && m.enable.phase == installRunning {
		inFlight = m.enable
	}
	if inFlight != nil && inFlight.phase != installRunning {
		inFlight = nil
	}
	m.enable = nil
	m.installing = inFlight
	m.resetDetail()
	m.queueNotificationProbe()
}

// Reopen puts the surface back where it was when it last closed: the same
// section, the same place in the sidebar, and the same row in the detail pane.
// It is what the gear and the settings key ask for, so toggling Configuration
// off and on is not a way to lose your place. Nothing to resume — the first open
// of the session — starts on the default page.
func (m *Model) Reopen() {
	if !m.resume.valid {
		m.Open(DefaultPage)
		return
	}
	// Open sets the sidebar cursor from the page, on the unfiltered list it has
	// just restored. That is the only correct index for it, which is why the
	// remembered position never carries one.
	m.Open(m.resume.page)
	// The detail pane's selection is restored together with the focus that makes
	// it mean anything; restoring the row alone would leave a number nothing on
	// screen was using. Both are clamped by the first render, which is when the
	// page's controls exist — a page with none takes the focus back there.
	m.rowCursor = max(0, m.resume.rowCursor)
	m.detailFocus = m.resume.detailFocus
}

// Close records the position Reopen restores. The host calls it as the surface
// goes away, which is the only moment that position is still known.
//
// It also puts back a theme the surface was only previewing. A preview belongs
// to the screen that started it, and closing Configuration is one more way of
// leaving that screen: Escape restores it, moving to another page restores it,
// and closing must too, or a theme nobody saved outlives the surface that could
// have undone it.
func (m *Model) Close() {
	m.restoreActivePreview()
	m.resume = resumeState{
		page:        m.Page(),
		rowCursor:   m.rowCursor,
		detailFocus: m.detailFocus,
		valid:       true,
	}
}

// restoreActivePreview puts back the theme in force whenever a picker is still
// previewing one. It deliberately does not go through activePicker(): a preview
// is a change to the whole surface, and the picker that made it must be found
// even after the route or the page it lives on has been left behind. Every path
// that moves away from a picker calls this, so a preview can never outlive the
// screen it belongs to.
func (m *Model) restoreActivePreview() {
	if state := m.appearanceState; state != nil && state.picker != nil {
		state.picker.restoreTheme()
	}
	if form := m.addProject; form != nil && form.picker != nil {
		form.picker.restoreTheme()
	}
}

// resetDetail returns the detail pane to its opening state: sidebar has the
// keyboard, the row cursor is at the top, and no half-finished repair state
// survives a move to somewhere else.
func (m *Model) resetDetail() {
	m.detailFocus = false
	m.rowCursor = 0
	m.controls = nil
	m.confirm = nil
	m.showColorSteps = false
	// Whatever the route being left knew about its own check goes with it;
	// OpenRepair records it again for the route it opens.
	m.repairOpenedOK = false
	// A live preview belongs to the screen that started it. Moving anywhere else
	// puts the resolved theme back, so navigation can never leave a previewed
	// theme applied with nothing on screen able to undo it.
	m.restoreActivePreview()
	// An enable route belongs to the child route that opened it; leaving that
	// route ends the attempt rather than leaving it half-answered underneath.
	// A confirmed install already running is the exception: Homebrew does not
	// stop because the route was left, so the attempt is kept and its outcome is
	// reported as a notice.
	if m.enable != nil {
		if m.enable.phase == installRunning {
			m.installing = m.enable
		}
		m.enable = nil
	}
	// A restart note answers a change the user just made on the page they made
	// it on; carrying it somewhere else would be a claim about that page.
	m.restartNote = ""
	// A list belongs to the control it hangs from; leaving the page takes it with
	// it rather than leaving one floating over somewhere else.
	m.closeDropdown()
	if state := m.terminalState; state != nil {
		// A refused value is a complaint about the edit that was open; leaving
		// the page ends that conversation.
		state.invalid, state.reason = "", ""
	}
	m.closeEditor()
}

// Page is the destination the sidebar highlights.
func (m *Model) Page() PageID { return m.router.page() }

// Route is the visible route, including any focused child.
func (m *Model) Route() Route { return m.router.current() }

// Navigate moves to a sidebar destination, abandoning any child route.
func (m *Model) Navigate(page PageID) {
	m.router.navigate(page)
	m.cursor = indexOfPage(m.visiblePages(), page)
	m.results = false
	m.saved = nil
	m.resetDetail()
	m.queueNotificationProbe()
}

// PushChild opens a focused child route with parent-return behavior. The
// parent's own selection is remembered here, which is what "returning restores
// the parent page" means in practice.
func (m *Model) PushChild(child ChildID, title string) {
	before := m.router.current()
	m.router.push(child, title)
	if m.router.current() == before {
		return
	}
	m.saved = append(m.saved, savedFocus{rowCursor: m.rowCursor, detailFocus: m.detailFocus})
	m.resetDetail()
}

// Back returns from a focused child route to its parent. It reports false when
// there is nothing to return to.
//
// Returning restores the parent page's own state: the sidebar destination never
// moved, and the row cursor starts at the top of the page the user came from
// rather than at whatever index the child happened to leave behind.
func (m *Model) Back() bool {
	if !m.router.back() {
		return false
	}
	m.resetDetail()
	if len(m.saved) > 0 {
		restore := m.saved[len(m.saved)-1]
		m.saved = m.saved[:len(m.saved)-1]
		m.rowCursor = restore.rowCursor
		m.detailFocus = restore.detailFocus
	}
	return true
}

// SearchFocused reports that Search has the keyboard, so every printable key
// belongs to it.
func (m *Model) SearchFocused() bool { return m.focus == focusSearch }

// SearchActive reports that a query is still narrowing the sidebar, whether or
// not the input has focus. Escape clears that before it closes Configuration.
func (m *Model) SearchActive() bool { return strings.TrimSpace(m.search.Value()) != "" }

// Query is the live search query.
func (m *Model) Query() string { return m.search.Value() }

// ClearSearch drops the query and restores the full sidebar.
func (m *Model) ClearSearch() {
	m.search.SetValue("")
	m.search.Blur()
	m.focus = focusSidebar
	m.results = false
	m.cursor = indexOfPage(m.visiblePages(), m.Page())
}

// FocusContext is the keymap context the surface owns right now.
func (m *Model) FocusContext() string {
	if m.confirm != nil {
		return ContextConfigConfirm
	}
	// A field on a page owns typed characters exactly as Search does.
	if m.editing() {
		return ContextConfigEdit
	}
	if m.SearchFocused() {
		return ContextConfigEdit
	}
	return ContextConfig
}

// Commands describes what the footer and palette may advertise here. Keys come
// from the registered bindings, never from this list.
func (m *Model) Commands() []plugin.Command {
	if m.confirm != nil {
		return []plugin.Command{
			{ID: "confirm", Name: "Apply", Category: plugin.CategoryActions, Context: ContextConfigConfirm, Priority: 1},
			{ID: "cancel", Name: "Cancel", Category: plugin.CategoryActions, Context: ContextConfigConfirm, Priority: 2},
		}
	}
	commands := []plugin.Command{
		{ID: "cursor-down", Name: "Sections", Category: plugin.CategoryNavigation, Context: ContextConfig, Priority: 1},
		{ID: "select", Name: "Change", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 2},
		{ID: "search", Name: "Search", Category: plugin.CategorySearch, Context: ContextConfig, Priority: 3},
		{ID: "close-configuration", Name: "Return", Category: plugin.CategoryNavigation, Context: ContextConfig, Priority: 4},
		{ID: "first-result", Name: "First result", Category: plugin.CategoryNavigation, Context: ContextConfigEdit, Priority: 1},
		{ID: "select", Name: "Open setting", Category: plugin.CategoryActions, Context: ContextConfigEdit, Priority: 2},
		{ID: "clear-search", Name: "Clear search", Category: plugin.CategorySearch, Context: ContextConfigEdit, Priority: 3},
	}
	// The page's own actions are advertised from the controls it just rendered,
	// so the footer describes what is actually on screen. Keys still come from
	// the registered bindings.
	seen := map[string]bool{}
	for _, c := range m.controls {
		if seen[c.key] {
			continue
		}
		if command, ok := controlCommand(c.key); ok {
			seen[c.key] = true
			commands = append(commands, command)
		}
	}
	return commands
}

// controlCommand maps a control shortcut onto the command the footer and the
// palette name it by.
func controlCommand(key string) (plugin.Command, bool) {
	switch key {
	case "r":
		return plugin.Command{ID: "recheck", Name: "Recheck", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 5}, true
	case "c":
		return plugin.Command{ID: "copy-guidance", Name: "Copy", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 6}, true
	case "o":
		return plugin.Command{ID: "open-file", Name: "Open", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 7}, true
	case "a":
		return plugin.Command{ID: "add-project", Name: "Add", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 5}, true
	case "i":
		return plugin.Command{ID: "init-repo", Name: "Init", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 5}, true
	case "d":
		return plugin.Command{ID: "remove-project", Name: "Remove", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 6}, true
	case "e":
		return plugin.Command{ID: "edit-host", Name: "Edit", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 5}, true
	case "f":
		return plugin.Command{ID: "enable-remote-hosts", Name: "Turn on", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 7}, true
	case "g":
		return plugin.Command{ID: "use-global-theme", Name: "Use global", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 8}, true
	case "t":
		return plugin.Command{ID: "test-notifications", Name: "Test", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 5}, true
	case keyIntegrationInstall:
		return plugin.Command{ID: "install-integration", Name: "Install", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 5}, true
	case keyIntegrationUpdate:
		return plugin.Command{ID: "update-integration", Name: "Update", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 6}, true
	case keyIntegrationRepair:
		return plugin.Command{ID: "repair-integration", Name: "Repair", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 6}, true
	case keyIntegrationUninstall:
		return plugin.Command{ID: "uninstall-integration", Name: "Remove", Category: plugin.CategoryActions, Context: ContextConfig, Priority: 7}, true
	}
	return plugin.Command{}, false
}

// visiblePages is the sidebar's current destination list: everything, or only
// the pages a non-empty query matches.
func (m *Model) visiblePages() []PageID {
	if m.SearchActive() {
		return SearchPages(m.search.Value())
	}
	var pages []PageID
	for _, page := range AllPages() {
		pages = append(pages, page.ID)
	}
	return pages
}

func indexOfPage(pages []PageID, target PageID) int {
	for i, page := range pages {
		if page == target {
			return i
		}
	}
	return 0
}

// Key handles a key press. It reports whether the surface consumed it; the host
// keeps unconsumed keys away from the plugin hidden underneath.
func (m *Model) Key(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	handled, cmd := m.key(msg)
	return handled, m.drain(cmd)
}

// drain batches any command raised while handling an event with the one the
// handler returned.
func (m *Model) drain(cmd tea.Cmd) tea.Cmd {
	if len(m.pending) == 0 {
		return cmd
	}
	cmds := append(m.pending, cmd)
	m.pending = nil
	return tea.Batch(cmds...)
}

// TakePending returns commands queued outside a key handler — a directory
// listing, a git probe — so the host can run them after a programmatic open.
func (m *Model) TakePending() tea.Cmd {
	return m.drain(nil)
}

func (m *Model) key(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	key := msg.String()

	// A field on a page owns every printable key while it is open, so nothing
	// typed into Name, Location, a theme filter, or a title template can reach
	// a page shortcut or a global one.
	if m.editing() {
		return m.editorKey(msg)
	}

	// An open dropdown is the innermost thing on screen: it answers every key,
	// including the ones that would otherwise close Configuration out from under
	// the list the user is choosing from.
	if handled, cmd := m.dropdownKey(key); handled {
		return true, cmd
	}

	if m.SearchFocused() {
		switch key {
		case "down", "ctrl+n", "tab":
			m.focusSidebarList()
			return true, nil
		case "up", "ctrl+p":
			return true, nil
		case "enter":
			m.focusSidebarList()
			m.activateCursor()
			return true, nil
		}
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		// The detail pane follows the query: results while one is being typed,
		// the page again once it is cleared.
		m.results = m.SearchActive()
		m.clampCursor()
		return true, cmd
	}

	// A confirmation owns the keyboard: while one is on screen the only two
	// answers are yes and no, and nothing else may act on the surface behind it.
	if m.confirm != nil {
		if handled, cmd := m.runShortcut(key); handled {
			return true, cmd
		}
		return true, nil
	}

	// Page shortcuts (the mockups' C, R, O) act on the page whether or not the
	// detail pane holds the cursor: the footer advertises them for the page.
	// Enter is deliberately not one of them — it means "the thing I am on",
	// which the detail pane below answers with the focused control.
	if key != "enter" {
		if handled, cmd := m.runShortcut(key); handled {
			return true, cmd
		}
	}

	// A page's own keys are advertised in the page's own copy ("shift+↑/shift+↓
	// reorders the list"), which is on screen whether or not the detail pane
	// holds the cursor. They act on the selection the page is already showing,
	// so they answer from either pane rather than silently doing nothing.
	if handled, cmd := m.pageKey(key); handled {
		return true, cmd
	}

	if m.detailOwnsKeys() {
		// A focused theme list owns the arrows: moving inside it previews, and
		// running off either end is what hands the cursor back to the page.
		if m.pickerOwnsKeys() {
			if handled, cmd := m.pickerKey(key); handled {
				return true, cmd
			}
		}
		switch key {
		case "down", "j", "ctrl+n":
			m.moveRowCursor(1)
			m.syncPickerCursor()
			return true, nil
		case "up", "k", "ctrl+p":
			if m.rowCursor == 0 && !m.Route().IsChild() {
				// Leaving the top of a page's list returns to the navigation
				// the user came from, rather than trapping the cursor.
				m.detailFocus = false
				return true, nil
			}
			m.moveRowCursor(-1)
			m.syncPickerCursor()
			return true, nil
		case "home", "g":
			m.rowCursor = 0
			return true, nil
		case "end", "G":
			m.rowCursor = max(0, len(m.cursorControls())-1)
			return true, nil
		case "enter":
			return true, m.runControl(m.cursorControl())
		case "left", "h", "shift+tab":
			if !m.Route().IsChild() {
				m.detailFocus = false
				return true, nil
			}
		case "tab":
			if m.Route().IsChild() {
				m.moveRowCursor(1)
				return true, nil
			}
			m.focusSearch()
			return true, nil
		}
		if m.Route().IsChild() {
			// A child route is the only thing on screen the user came for;
			// unclaimed keys stop here rather than moving the sidebar behind it.
			return true, nil
		}
	}

	switch key {
	case "/":
		m.focusSearch()
		return true, nil
	case "tab", "right", "l":
		// Tab walks the panes the way the footer says it does: navigation, the
		// page's own controls, then Search.
		if m.focus == focusSidebar && m.hasDetailControls() {
			m.detailFocus = true
			m.rowCursor = 0
			return true, nil
		}
		m.focusSearch()
		return true, nil
	case "down", "j", "ctrl+n":
		pages := m.visiblePages()
		if m.cursor < len(pages)-1 {
			m.moveSidebarCursor(m.cursor + 1)
		}
		return true, nil
	case "up", "k", "ctrl+p":
		if m.cursor == 0 {
			// Mirrors the mockup's search flow: Down from Search reaches the
			// first result, Up from the first result returns to it. With no
			// query there is nothing above the list, so the cursor stays put.
			if m.SearchActive() {
				m.focusSearch()
			}
			return true, nil
		}
		m.moveSidebarCursor(m.cursor - 1)
		return true, nil
	case "home", "g":
		m.moveSidebarCursor(0)
		return true, nil
	case "end", "G":
		m.moveSidebarCursor(max(0, len(m.visiblePages())-1))
		return true, nil
	case "enter":
		m.activateCursor()
		return true, nil
	}
	return false, nil
}

// focusSearch and focusSidebarList both hand the keyboard to the navigation
// pane, which means taking it away from the detail pane. Leaving detailFocus
// set behind them left the sidebar drawing the cursor while Enter still ran a
// control the user was no longer on — most visibly, Enter on a search result
// did nothing at all.
func (m *Model) focusSearch() {
	m.focus = focusSearch
	m.detailFocus = false
	m.search.Focus()
	m.results = m.SearchActive()
}

func (m *Model) focusSidebarList() {
	m.focus = focusSidebar
	m.detailFocus = false
	m.search.Blur()
	if m.SearchActive() {
		// The results pane says Down moves to the first matching setting, so it
		// does. Clamping the cursor left over from the unfiltered list would
		// instead drop the user wherever the shorter list happened to end.
		m.cursor = 0
		m.results = true
	}
	m.clampCursor()
}

// moveSidebarCursor moves the navigation cursor and opens what it lands on.
// Arrowing the sidebar is the same move as clicking a section, so the detail
// pane follows immediately rather than waiting for Enter. Focus stays in the
// sidebar: the arrows keep walking sections until the user asks to go into one.
//
// A query is the exception. While one is narrowing the list the detail pane
// belongs to the search results, and navigating on every keystroke would replace
// the very list the user is stepping through, so the cursor moves alone and
// Enter still opens the match.
func (m *Model) moveSidebarCursor(cursor int) {
	m.cursor = cursor
	if m.SearchActive() {
		return
	}
	pages := m.visiblePages()
	m.clampCursor()
	if len(pages) == 0 || pages[m.cursor] == m.Page() {
		return
	}
	m.navigateFromSidebar(pages[m.cursor])
}

func (m *Model) clampCursor() {
	pages := m.visiblePages()
	if m.cursor >= len(pages) {
		m.cursor = max(0, len(pages)-1)
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// activateCursor opens the destination under the cursor. With a query active
// this is the "Enter on a search result" path: it navigates to the page and
// leaves the query in place so the user can keep stepping through matches.
//
// Without one the arrows have already opened the section, so Enter on the page
// the detail pane is already showing means the next thing in: the cursor moves
// into the page's own controls, which is what Tab does from here too.
func (m *Model) activateCursor() {
	pages := m.visiblePages()
	if len(pages) == 0 {
		return
	}
	m.clampCursor()
	if !m.SearchActive() && pages[m.cursor] == m.Page() && m.hasDetailControls() {
		m.detailFocus = true
		m.rowCursor = 0
		return
	}
	// Navigate, rather than moving the router directly: choosing a destination
	// from the sidebar is the same move as any other, and it owes the page being
	// left the same teardown — a restored theme preview above all. The query is
	// deliberately left in place so the user can keep stepping through matches.
	m.Navigate(pages[m.cursor])
}

// Mouse handles a mouse event whose coordinates are local to the content area.
func (m *Model) Mouse(msg tea.MouseMsg) tea.Cmd {
	return m.drain(m.handleMouse(msg))
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	action := m.mouse.HandleMouse(msg)
	switch action.Type {
	case mouse.ActionHover:
		m.hoverID = ""
		if action.Region != nil {
			m.hoverID = action.Region.ID
		}
		// A button-less motion means any scrollbar drag we still think is in
		// flight lost its release; drop it rather than leave it armed.
		m.dropThemeScrollbarGesture()
	case mouse.ActionDrag:
		if isThemeScrollbarDrag(m.mouse.DragRegion()) {
			if picker := m.activePicker(); picker != nil {
				m.dragThemeScrollbar(picker, action)
			}
		}
	case mouse.ActionDragEnd:
		if isThemeScrollbarDrag(action.DragStartID) {
			if picker := m.activePicker(); picker != nil {
				m.settleThemeScrollbar(picker)
			}
		}
	case mouse.ActionScrollUp, mouse.ActionScrollDown:
		if m.dropdownOpen() {
			// A list longer than its window scrolls under the wheel exactly as
			// the theme list does. A notch that misses it is swallowed rather
			// than passed on: moving the theme list would move the row cursor
			// off the very control the open list hangs from, leaving two rows
			// painted as focused.
			if action.Region != nil {
				m.scrollDropdown(action.Region.ID, action.Delta)
			}
			return nil
		}
		m.scrollThemeList(action)
	case mouse.ActionClick, mouse.ActionDoubleClick:
		if action.Region == nil {
			// Clicking away from everything dismisses an open list rather than
			// leaving it floating over a page the user has moved on from.
			m.closeDropdown()
			return nil
		}
		if m.dropdownOpen() {
			if id := action.Region.ID; strings.HasPrefix(id, regionDropdownItem) {
				return m.clickDropdownItem(id)
			} else if id == regionDropdownMore {
				return nil
			} else if id != m.dropdown.controlID {
				// The first click outside an open list closes it. It does not
				// also act on what it landed on: the click the user meant was
				// "put this list away".
				m.closeDropdown()
				return nil
			}
		}
		// A control the page just rendered is clicked as itself: the click
		// moves the row cursor there and runs it, so mouse and keyboard leave
		// the surface in the same state.
		for index, c := range m.controls {
			if c.id != action.Region.ID {
				continue
			}
			if m.editing() && m.editingID() != c.id {
				// Clicking something else is leaving the field: the keyboard
				// goes with the click. Left open, the field would take the row
				// cursor straight back off the control just clicked, which is
				// how a clicked theme lit up for one frame and then nothing was
				// selected at all. The typed value stays — clicking away is not
				// Escape.
				m.closeEditor()
			}
			if c.cursor {
				m.detailFocus = true
				m.focusControlIndex(index)
			} else if base, ok := strings.CutSuffix(c.id, toggleSuffix); ok {
				// A click on the pill still puts the keyboard on the row it
				// belongs to, so the next Enter matches what just happened.
				m.detailFocus = true
				m.focusControlByID(base)
			}
			if c.clickless {
				return nil
			}
			return m.runControl(index)
		}
		switch id := action.Region.ID; {
		case id == ui.RegionScrollbarThumb || id == ui.RegionScrollbarTrack:
			// Grab the thumb, or jump-to-spot on the track and keep dragging
			// from there. The bar's rects are registered after the row rects
			// precisely so this wins them. A rapid second press re-grabs
			// exactly like the first one did.
			if picker := m.activePicker(); picker != nil {
				m.pressThemeScrollbar(picker, action)
			}
		case id == regionThemeList:
			// A click on the frame or the scrollbar is still a click on the
			// list: put the keyboard there so Escape restores a preview.
			m.detailFocus = true
			m.focusPickerList()
		case id == regionSearch:
			m.focusSearch()
		case strings.HasPrefix(id, regionNavPrefix):
			m.navigateFromSidebar(PageID(strings.TrimPrefix(id, regionNavPrefix)))
		case strings.HasPrefix(id, regionResult):
			if page, ok := action.Region.Data.(PageID); ok {
				m.navigateFromSidebar(page)
			}
		}
	}
	return nil
}

// scrollThemeList answers a wheel notch over the theme list. The list is the
// one thing in Configuration long enough to need scrolling — hundreds of themes
// behind an eight-row window — and a list that scrolls under the keyboard but
// not under the wheel is a list that looks broken.
//
// A notch moves the window and keeps the highlight on the same visual row, so
// scrolling down and scrolling back up are the same gesture. The theme under
// that row is previewed once, the way a keyboard step previews once.
func (m *Model) scrollThemeList(action mouse.MouseAction) {
	picker := m.activePicker()
	if picker == nil || action.Region == nil {
		return
	}
	if id := action.Region.ID; !strings.HasPrefix(id, regionThemeRow) && id != regionThemeList &&
		id != ui.RegionScrollbarThumb && id != ui.RegionScrollbarTrack {
		return
	}
	if !picker.scrollWindow(action.Delta) {
		return
	}
	if m.editing() {
		// Filtering and then scrolling the results is the obvious next move,
		// and scrolling is passive: the list moves, but the keyboard stays in
		// the field the user is still typing into.
		return
	}
	// The wheel chose a row; the keyboard should be on it if it was already
	// in the list, and should move into the list if it was not.
	m.detailFocus = true
	m.focusPickerList()
}

// WheelAtBoundary reports that a wheel event over Configuration cannot change
// anything currently under the pointer. The theme list and an open dropdown
// are the surfaces that scroll; everything else is unknown.
func (m *Model) WheelAtBoundary(msg tea.MouseWheelMsg) bool {
	region := m.mouse.HitMap.Test(msg.X, msg.Y)
	delta := mouse.WheelScrollLines
	if msg.Button == tea.MouseWheelUp {
		delta = -mouse.WheelScrollLines
	}
	if m.dropdownOpen() {
		if region == nil {
			return true
		}
		id := region.ID
		if !strings.HasPrefix(id, regionDropdownItem) && id != regionDropdownMore {
			// A notch that misses the open list is swallowed rather than
			// passed on, so it is always a no-op.
			return true
		}
		return m.dropdown.atScrollBoundary(delta)
	}
	if region == nil {
		return false
	}
	if id := region.ID; strings.HasPrefix(id, regionThemeRow) || id == regionThemeList ||
		id == ui.RegionScrollbarThumb || id == ui.RegionScrollbarTrack {
		picker := m.activePicker()
		if picker == nil {
			return false
		}
		return picker.atScrollBoundary(delta)
	}
	return false
}

// navigateFromSidebar opens a destination the user selected in the navigation
// pane. It leaves the detail pane unfocused: the click was on navigation, so
// the arrows and Enter belong to navigation until the user asks otherwise.
func (m *Model) navigateFromSidebar(page PageID) {
	m.focus = focusSidebar
	m.detailFocus = false
	m.search.Blur()
	m.cursor = indexOfPage(m.visiblePages(), page)
	m.router.navigate(page)
	m.results = false
	m.resetDetail()
	m.queueNotificationProbe()
}

// View renders the whole Configuration content area. It never returns more rows
// than height: the host's header and footer must stay on screen.
func (m *Model) View(width, height int) string {
	m.width, m.height = width, height
	m.mouse.Clear()
	if width < 8 || height < 3 {
		// Nothing is painted, so nothing can hold an open list: one left behind
		// here would swallow every key with no way for the user to see why.
		m.closeDropdown()
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render("")
	}

	// The navigation pane keeps its mockup width while the terminal can afford
	// it, and gives columns back to the detail pane before it clips: a fixed 39
	// columns is a third of a 100-column terminal, which is what pushed form
	// fields off the right edge there.
	sidebarWidth := sidebarPreferredWidth
	if third := width / 3; sidebarWidth > third {
		sidebarWidth = max(sidebarMinWidth, third)
	}
	if maxSidebar := width / 2; sidebarWidth > maxSidebar {
		sidebarWidth = maxSidebar
	}
	if sidebarWidth < sidebarMinWidth {
		sidebarWidth = min(sidebarMinWidth, width/2)
	}
	if sidebarWidth < 8 {
		sidebarWidth = width
	}
	m.sidebarWidth = sidebarWidth
	detailWidth := width - sidebarWidth

	sidebar := styles.RenderPanel(m.renderSidebar(sidebarWidth, height), sidebarWidth, height, m.SearchFocused())
	if detailWidth < 8 {
		// Too narrow for two panes: the sidebar is the navigation, so it wins
		// and the detail pane is dropped rather than clipped into nonsense —
		// and with it any list that was hanging off a control in it.
		m.closeDropdown()
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(sidebar)
	}
	detail := styles.RenderPanelWithGradient(
		m.renderDetail(detailWidth, height, sidebarWidth),
		detailWidth, height, styles.GetActiveGradient(),
	)
	row := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, detail)
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(row)
}

// paneContentWidth is the writable width inside a panel: two border columns and
// one column of padding on each side.
func paneContentWidth(paneWidth int) int {
	return max(0, paneWidth-4)
}

// renderSidebar paints the navigation pane and registers its hit regions.
// Regions are in content-area coordinates: the host offsets mouse events past
// the header before forwarding them.
func (m *Model) renderSidebar(paneWidth, paneHeight int) string {
	inner := paneContentWidth(paneWidth)
	originX := 2 // border + padding
	var lines []string

	add := func(line string) int {
		lines = append(lines, line)
		return len(lines) - 1
	}

	add(PaneTitle("Configuration"))
	add("")

	m.search.SetWidth(max(1, inner-8))
	inputStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
	if m.SearchActive() {
		inputStyle = lipgloss.NewStyle().
			Foreground(styles.TextPrimary).
			Background(styles.BgTertiary)
	}
	if m.SearchFocused() {
		inputStyle = inputStyle.Foreground(styles.TextPrimary).Background(styles.BgTertiary)
	}
	if m.hoverID == regionSearch && !m.SearchFocused() {
		inputStyle = inputStyle.Background(styles.SurfaceRaised)
	}
	label := lipgloss.NewStyle().Foreground(styles.TextSecondary).Render("Search ")
	searchLine := label + inputStyle.Render(m.search.View())
	searchY := add(searchLine)
	m.mouse.HitMap.AddRect(regionSearch, originX, 1+searchY, inner, 1, nil)

	matches := Search(m.search.Value())
	if m.SearchActive() {
		count := fmt.Sprintf("  %d matching settings", len(matches))
		if len(matches) == 1 {
			count = "  1 matching setting"
		}
		add(lipgloss.NewStyle().Foreground(styles.Warning).Render(count))
	}

	visible := m.visiblePages()
	inList := make(map[PageID]bool, len(visible))
	for _, page := range visible {
		inList[page] = true
	}

	index := 0
	for _, group := range Groups() {
		shown := make([]Page, 0, len(group.Pages))
		for _, page := range group.Pages {
			if inList[page.ID] {
				shown = append(shown, page)
			}
		}
		if len(shown) == 0 {
			continue
		}
		add("")
		add(PaneTitle(group.Title))
		for _, page := range shown {
			state := State{
				Focused: m.focus == focusSidebar && index == m.cursor,
				Hovered: m.hoverID == regionNavPrefix+string(page.ID),
			}
			// The active destination stays visibly highlighted on every page,
			// including while the cursor is elsewhere or Search has focus.
			text := page.Title
			if page.ID == m.Page() && !state.Focused {
				text = lipgloss.NewStyle().Foreground(styles.Primary).Bold(true).Render(page.Title)
			}
			y := add(ListRow(text, inner, state))
			m.mouse.HitMap.AddRect(regionNavPrefix+string(page.ID), originX, 1+y, inner, 1, page.ID)
			index++
		}
	}

	if len(visible) == 0 {
		add("")
		add(Muted("No matching settings"))
	}

	return clampLines(lines, paneHeight-2, inner)
}

// renderDetail paints the detail pane. offsetX is where the pane starts in the
// content area, so its hit regions land where they are painted.
func (m *Model) renderDetail(paneWidth, paneHeight, offsetX int) string {
	inner := paneContentWidth(paneWidth)
	originX := offsetX + 2

	// The pane is painted with the row cursor as it stands, and the cursor can
	// still move while it is being painted: a page that shrank under a stale
	// cursor clamps it at the end, and a focus request for a control that did
	// not exist yet resolves there too. Either way the frame in hand was
	// painted against a cursor that is no longer the cursor, which shows up as
	// a frame with nothing focused — the resolved repair row, the click that
	// lands while a filter still holds the keyboard. So paint it again, once,
	// with the answer. The regions the panes painted before this one are put
	// back first, so the second pass replaces this pane's hit map rather than
	// doubling it.
	outer := m.mouse.HitMap.Regions()
	lines, settled := m.buildDetail(originX, inner, paneHeight)
	if !settled {
		m.mouse.HitMap.Clear()
		for _, region := range outer {
			m.mouse.HitMap.Add(region.ID, region.Rect, region.Data)
		}
		lines, _ = m.buildDetail(originX, inner, paneHeight)
	}
	// The pane has settled, so an open dropdown is painted over it exactly once:
	// its rows float above the page, and its hit regions are registered last, so
	// they sit on top of the controls they cover.
	lines = m.compositeDropdown(lines, originX, inner, paneHeight-2)
	return clampLines(lines, paneHeight-2, inner)
}

// buildDetail paints the pane once. It reports whether the row cursor it
// painted with survived the pass: a false answer means the frame shows a cursor
// that has since moved, and is worth painting again.
func (m *Model) buildDetail(originX, inner, paneHeight int) ([]string, bool) {
	var lines []string

	// Everything below composes through the builder, so a page declares each
	// control once and its look, its hit region, and its key stay in agreement.
	// Building it here also drops the previous frame's controls, which is what
	// keeps the keyboard from reaching a row that is no longer on screen.
	builder := m.newPaneBuilder(originX, inner, paneHeight-2)

	if m.SearchActive() && m.results {
		lines = append(lines, PaneTitle("Search results"), "")
		lines = append(lines, Muted("Use ↓ to move to the first matching setting, or Esc to clear the filter."))
		matches := Search(m.search.Value())
		lastPage := PageID("")
		for i, entry := range matches {
			if entry.Page != lastPage {
				lines = append(lines, "", PaneTitle(PageTitle(entry.Page)))
				lastPage = entry.Page
			}
			id := fmt.Sprintf("%s%d", regionResult, i)
			state := State{Hovered: m.hoverID == id}
			lines = append(lines, ListRow(entry.Label, inner, state))
			m.mouse.HitMap.AddRect(id, originX, 1+len(lines)-1, inner, 1, entry.Page)
		}
		return lines, true
	}

	route := m.Route()
	switch {
	case m.confirm != nil && !route.IsChild():
		// A confirmation owns the pane wherever it was raised, so a page-level
		// consequential change looks and answers like a repair's does.
		builder.text(PaneTitle(m.confirm.title), "")
		m.buildConfirm(builder)
	case route.IsChild():
		m.buildChild(builder, route)
	case route.Page == PageSetup:
		m.buildSetup(builder)
	case route.Page == PageDiagnostics:
		m.buildDiagnostics(builder)
	case route.Page == PageAppearance:
		m.buildAppearance(builder)
	case route.Page == PageProjects:
		m.buildProjects(builder)
	case route.Page == PageWorkspaces:
		m.buildWorkspaces(builder)
	case route.Page == PageRemotes:
		m.buildRemotes(builder)
	case route.Page == PageAgents:
		m.buildAgents(builder)
	case route.Page == PageNotifications:
		m.buildNotifications(builder)
	case route.Page == PageTerminal:
		m.buildTerminal(builder)
	case route.Page == PagePanels:
		m.buildPanels(builder)
	case route.Page == PageFlags:
		m.buildFlags(builder)
	case route.Page == PageAdvanced:
		m.buildAdvanced(builder)
	case route.Page == PageAbout:
		m.buildAbout(builder)
	default:
		builder.text(pageBody(route.Page, inner)...)
	}
	lines = append(lines, builder.lines...)
	// The cursor the controls above were painted against. A page's own list may
	// have claimed it mid-build, which is deliberate and already in the paint.
	painted := m.rowCursor
	// The row cursor follows the field being typed into, so leaving an editor
	// puts the cursor back on the control the user was working in; a focus
	// request for a control that did not exist yet lands here too.
	if m.editor != nil {
		m.focusControlByID(m.editor.id)
	} else if id := m.pendingFocus; id != "" {
		// One retry: a control that still is not on screen is not coming, and a
		// standing request would later steal the cursor from wherever the user
		// has moved it.
		m.pendingFocus = ""
		m.focusRenderedControl(id)
	}
	m.clampRowCursor()
	if route.Page == PageFlags && !route.IsChild() && m.confirm == nil {
		lines = m.scrollFlagsPage(lines, builder.height)
	}
	if route.Child == ChildAgentIntegrations && m.confirm == nil {
		lines = m.windowIntegrationRows(lines, builder.height)
	}
	return lines, m.rowCursor == painted
}

// clampLines enforces the pane's height contract and keeps every line inside
// its width, so a pane can never push the host's header off screen.
func clampLines(lines []string, height, width int) string {
	if height < 0 {
		height = 0
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if width > 0 && ansi.StringWidth(line) > width {
			line = ansi.Truncate(line, width, "…")
		}
		out[i] = line
	}
	return strings.Join(out, "\n")
}

// pageKey answers the keys a page owns beyond its declared controls. Reordering
// is the only one today: it acts on the selected project, which is a page-level
// idea rather than a control's.
func (m *Model) pageKey(key string) (bool, tea.Cmd) {
	if m.Route().IsChild() || m.Page() != PageProjects {
		return false, nil
	}
	switch key {
	case "shift+up", "[":
		return true, m.moveSelectedProject(-1)
	case "shift+down", "]":
		return true, m.moveSelectedProject(1)
	}
	return false, nil
}

// Escape answers esc for everything the surface owns, innermost first: the
// field being typed into, a pending confirmation, an inline picker, the sidebar
// search, then a focused child route. It reports false when nothing on screen
// needed dismissing, which is the host's signal to close Configuration.
func (m *Model) Escape() bool {
	if m.editing() {
		m.cancelEditor()
		return true
	}
	// An open dropdown closes without committing, leaving the setting exactly as
	// it was — the list is dismissed, not the surface behind it.
	if m.closeDropdown() {
		return true
	}
	if m.DismissConfirm() {
		return true
	}
	// An open inline picker collapses back to its field rather than leaving the
	// route it belongs to.
	if m.collapseInlineThemePicker() {
		return true
	}
	// Leaving Appearance without saving puts back the theme that was in force
	// when the page opened, so a preview can never become a silent change.
	if picker := m.activePicker(); picker != nil && picker.previewing {
		picker.restoreTheme()
		return true
	}
	if m.SearchActive() {
		m.ClearSearch()
		return true
	}
	// Search with nothing typed in it still holds the keyboard. Escape hands it
	// back to the sidebar rather than closing Configuration out from under a user
	// who was only leaving the field.
	if m.SearchFocused() {
		m.focusSidebarList()
		return true
	}
	if m.Route().IsChild() {
		m.closeProjectForm()
		m.closeRemoteForm()
		return m.Back()
	}
	return false
}
