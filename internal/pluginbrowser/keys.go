package pluginbrowser

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
)

// browserOwnedKeys are the keys the browser itself acts on. They are the same
// for every protocol plugin, which is the point of one browser rather than one
// per plugin, and they are what a plugin-suggested action letter is refused
// against first.
//
// `n` and `tab` are deliberately absent. The pane switcher and the focus ring
// are the host's, and a plugin that swallowed either would be answering a
// question the app has already answered for every other surface.
// `+` and `-` are the split's, as they are on every other two-pane surface in
// sidecar. Neither is in keymap.HostReservedKeys or keymap.GlobalKeys, so the
// browser may own them.
var browserOwnedKeys = map[string]bool{
	"j": true, "k": true, "down": true, "up": true,
	"pgdown": true, "pgup": true, "home": true, "end": true,
	"enter": true,
	"/":     true, "v": true, "r": true, "a": true, "o": true, "c": true,
	"+": true, "-": true,
}

// stateBoundKeys are the browser's control keys that depend on what the
// collection declares, or on what is on screen. They are owned — no plugin may
// take one — but claimed only where they do something, so a key that would be
// inert falls through to whatever the host binds instead of being swallowed
// (td-fcb648).
var stateBoundKeys = map[string]bool{
	"/": true, "v": true, "a": true, "c": true,
	"+": true, "-": true,
}

// OwnedKeys reports the keys the browser acts on, for a host that has to answer
// "does this surface claim that key" without keeping its own copy of the list.
func OwnedKeys() map[string]bool {
	out := make(map[string]bool, len(browserOwnedKeys))
	for k, v := range browserOwnedKeys {
		out[k] = v
	}
	return out
}

// ClaimsKey reports whether the browser would act on a key in its current
// state. It is what the host's key ladder asks at precedence level 3.
func (m *Model) ClaimsKey(key string) bool {
	if m.overlay.open() || m.editingQuery() {
		return true
	}
	if stateBoundKeys[key] {
		return m.canAct(key)
	}
	if browserOwnedKeys[key] {
		return true
	}
	_, granted := m.grantedKeys[key]
	return granted
}

// canAct reports whether a state-bound control key would do anything right now.
// It is the one answer both ClaimsKey and HandleKey read, so a key the browser
// declines to claim is exactly a key it declines to act on.
func (m *Model) canAct(key string) bool {
	c, ok := m.ActiveCollection()
	switch key {
	case "/":
		return ok && c.Search != pluginhost.SearchNone
	case "v":
		return ok && m.hasViewControl(c)
	case "a":
		return len(m.applicableActions()) > 0
	case "c":
		return m.hasCoverage()
	case "+", "-":
		return m.canResizeSplit()
	}
	return false
}

// canResizeSplit reports that there are two boxes to move the rail between. A
// pane leaf holds one shape and a box too narrow for a detail gives the whole
// of itself to the list; in both there is no rail, so the keys are inert and
// unclaimed rather than swallowed.
func (m *Model) canResizeSplit() bool {
	if m.paneMode() || m.width <= 0 {
		return false
	}
	_, detail := m.split()
	return detail > 0
}

// ConsumesTextInput reports that the query line has the keyboard, so the host
// forwards alphanumeric keys as typed text instead of running its shortcuts.
func (m *Model) ConsumesTextInput() bool { return m.editingQuery() }

// BlocksGlobalKeys reports that an overlay owns the keyboard.
func (m *Model) BlocksGlobalKeys() bool { return m.overlay.open() }

// QuitKeyExits reports whether `q` should reach sidecar's quit flow. It does
// everywhere except while the query line is taking text, where `q` is a letter.
func (m *Model) QuitKeyExits() bool { return !m.editingQuery() }

func (m *Model) editingQuery() bool {
	s := m.activeState()
	return s != nil && s.editing
}

// HandleKey routes one key press and reports whether the browser consumed it.
func (m *Model) HandleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.overlay.open() {
		return m.overlayKey(msg)
	}
	if m.editingQuery() {
		return m.queryKey(msg)
	}

	key := msg.String()
	if action, ok := m.grantedKeys[key]; ok {
		return m.startAction(action), true
	}
	// A control key with nothing behind it is inert and unclaimed. The footer
	// hint is already absent — the design language says that is the whole of the
	// message — so swallowing the key here would only make the pane look wedged.
	if stateBoundKeys[key] && !m.canAct(key) {
		return nil, false
	}

	switch key {
	case "j", "down":
		return m.moveCursor(1), true
	case "k", "up":
		return m.moveCursor(-1), true
	case "pgdown":
		return m.moveCursor(m.pageStep()), true
	case "pgup":
		return m.moveCursor(-m.pageStep()), true
	case "home":
		return m.moveTo(0), true
	case "end":
		if s := m.activeState(); s != nil {
			return m.moveTo(len(s.items) - 1), true
		}
		return nil, true
	case "enter":
		return m.openCursorRow(), true
	case "/":
		return m.beginQuery(), true
	case "v":
		return m.openViewModal(), true
	case "r":
		return m.refreshActive(), true
	case "a":
		return m.openActionMenu(), true
	case "c":
		return m.openCoverage(), true
	case "o":
		return m.openSource(), true
	case "+":
		return m.resizeSplit(1), true
	case "-":
		return m.resizeSplit(-1), true
	}
	// Esc with nothing open is the host's: it is how the global space is left,
	// and a surface that swallowed it would strand the user on a tab.
	return nil, false
}

// pageStep is one screenful of rows, at least one.
func (m *Model) pageStep() int {
	rows := m.tableRows()
	if rows < 1 {
		return 1
	}
	return rows
}

func (m *Model) moveCursor(delta int) tea.Cmd {
	if m.focus == FocusDetail {
		m.detail.scroll += delta
		m.clampDetailScroll()
		return nil
	}
	s := m.activeState()
	if s == nil {
		return nil
	}
	target := s.cursor + delta
	// Moving off the end of a page that has more behind it is what "paging on
	// demand" means: the next page is fetched because the user reached for it,
	// never eagerly because it existed.
	if target >= len(s.items) && s.nextCursor != "" && !s.paging {
		c, ok := m.ActiveCollection()
		if ok {
			cmd := m.list(c, s, true)
			m.moveTo(len(s.items) - 1)
			return cmd
		}
	}
	return m.moveTo(target)
}

func (m *Model) moveTo(index int) tea.Cmd {
	s := m.activeState()
	if s == nil {
		return nil
	}
	if index < 0 {
		index = 0
	}
	if index >= len(s.items) {
		index = len(s.items) - 1
	}
	if index < 0 {
		index = 0
	}
	s.cursor = index
	m.clampListScroll(s)
	return nil
}

// openCursorRow opens the row under the cursor, or focuses the detail box when
// the row it already shows is the one under the cursor. That is the second
// Enter in the user contract, and it costs no process.
func (m *Model) openCursorRow() tea.Cmd {
	// In a pane there is no room for the document beside the list, so Enter
	// opens it as a second tab in the same leaf and a second Enter on the same
	// row focuses that tab. Both halves live on the host, which owns the strip.
	if m.paneMode() {
		return m.paneOpenRow()
	}
	c, ok := m.ActiveCollection()
	if !ok {
		return nil
	}
	item, ok := m.currentItem()
	if !ok {
		return nil
	}
	if !c.Detail {
		return nil
	}
	if m.detail.collection == c.ID && m.detail.id == item.ID && (m.detail.loaded || m.detail.loading) {
		m.focus = FocusDetail
		return nil
	}
	return m.openDocument(c.ID, item.ID, false)
}

func (m *Model) beginQuery() tea.Cmd {
	c, ok := m.ActiveCollection()
	if !ok || c.Search == pluginhost.SearchNone {
		return nil
	}
	s := m.state(c)
	s.editing = true
	m.focus = FocusList
	return nil
}

// queryKey edits the query line. Every edit schedules one debounce tick; only
// the newest one spends a process, and the manager kills the superseded call's
// process group when it does.
func (m *Model) queryKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	c, ok := m.ActiveCollection()
	if !ok {
		return nil, false
	}
	s := m.state(c)
	switch msg.String() {
	case "esc":
		s.editing, s.atLimit = false, false
		return nil, true
	case "enter":
		s.editing, s.atLimit = false, false
		s.debounce++
		return m.list(c, s, false), true
	case "backspace":
		if s.query == "" {
			return nil, true
		}
		runes := []rune(s.query)
		s.query = string(runes[:len(runes)-1])
		s.atLimit = false
		return m.scheduleQuery(s), true
	case "ctrl+u":
		if s.query == "" {
			return nil, true
		}
		s.query = ""
		s.atLimit = false
		return m.scheduleQuery(s), true
	}
	text := msg.Text
	if text == "" || strings.ContainsAny(text, "\n\r\t") {
		return nil, true
	}
	// The same bound the CLI and resource.Reference.Valid enforce. A query typed
	// past it would persist, decode, and then be refused as invalid when the
	// tabs were rebuilt — the tab would vanish on relaunch. Refusing the
	// keystroke is the honest answer: what is on screen is what is saved — and
	// the row says so, on the row that refused it, rather than leaving the pane
	// looking wedged (td-fcb648).
	if utf8.RuneCountInString(s.query)+utf8.RuneCountInString(text) > resource.MaxQueryChars {
		s.atLimit = true
		return nil, true
	}
	s.atLimit = false
	s.query += text
	return m.scheduleQuery(s), true
}

func (m *Model) scheduleQuery(s *collectionState) tea.Cmd {
	s.debounce++
	seq := s.debounce
	instance, collection := m.instance, s.id
	return tea.Tick(QueryDebounce, func(time.Time) tea.Msg {
		return QueryDebouncedMsg{Instance: instance, Collection: collection, Sequence: seq}
	})
}

// refreshActive re-lists the collection and re-fetches the open document. A
// refresh never blanks what is on screen: the previous page stays until the new
// one lands.
func (m *Model) refreshActive() tea.Cmd {
	if !m.described {
		return m.Refresh()
	}
	c, ok := m.ActiveCollection()
	if !ok {
		return nil
	}
	s := m.state(c)
	var cmds []tea.Cmd
	if cmd := m.list(c, s, false); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if m.detail.loaded && m.detail.id != "" {
		if cmd := m.openDocument(m.detail.collection, m.detail.id, true); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// openSource opens the validated http(s) URL of whatever is in focus, through
// the host's confirmed path. The browser validates before it asks: a plugin
// never gets to hand the host a destination it has not checked.
func (m *Model) openSource() tea.Cmd {
	if m.calls.OpenURL == nil {
		return nil
	}
	raw := ""
	if m.focus == FocusDetail && m.detail.loaded {
		raw = m.detail.doc.SourceURL
	}
	if raw == "" {
		if item, ok := m.currentItem(); ok {
			raw = item.SourceURL
		}
	}
	if raw == "" && m.detail.loaded {
		raw = m.detail.doc.SourceURL
	}
	safe, ok := contentlink.SafeHTTPURL(raw)
	if !ok {
		return nil
	}
	return m.calls.OpenURL(safe)
}

// PaneFocusStops names the browser's two windows for the app-owned focus ring.
// The detail box is a stop only when there is something in it; a ring entry
// that lands on a blank card is a stop the user cannot use.
func (m *Model) PaneFocusStops() []string {
	// A pane leaf holds one shape, so it is one stop. The other shape is a
	// sibling TAB in the same leaf, and the strip is what moves between them.
	if m.paneMode() {
		if m.paneShape == PaneDocument {
			return []string{string(FocusDetail)}
		}
		return []string{string(FocusList)}
	}
	stops := []string{string(FocusList)}
	if m.detail.loaded || m.detail.loading {
		stops = append(stops, string(FocusDetail))
	}
	return stops
}

// PaneFocus is the focused window's ID.
func (m *Model) PaneFocus() string { return string(m.focus) }

// SetPaneFocus moves focus within the browser.
func (m *Model) SetPaneFocus(id string) {
	switch Focus(id) {
	case FocusDetail:
		if m.detail.loaded || m.detail.loading {
			m.focus = FocusDetail
		}
	case FocusList:
		m.focus = FocusList
	}
}

// SetPaneFocusActive records whether the browser's inner focus chrome should be
// drawn, which is false while an app-level surface holds the keyboard.
func (m *Model) SetPaneFocusActive(active bool) {
	m.paneFocusManaged = true
	m.paneFocusActive = active
}

// innerFocusActive reports whether the focused window should wear the active
// border. A browser no deck has claimed answers for itself.
func (m *Model) innerFocusActive() bool {
	return !m.paneFocusManaged || m.paneFocusActive
}
