package pluginbrowser

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// Pane mode is the same browser in a pane deck rather than a navbar tab.
//
// A tab placement has room for a list and the document beside it. A pane
// usually does not, so a pane-mode browser shows exactly one of the two at a
// time — which is also what makes a pane a TAB: a collection tab and a document
// tab are two entries in one Resource leaf's strip, and Enter on a row opens
// the second beside the first rather than replacing the list underneath the
// cursor.
//
// Nothing else about the browser changes. The keys, the query debounce, the
// View modal, the action menu, the notices and the narrow reflow are the tab
// placement's, because a surface that answered a key differently in a pane
// would be exactly the drift one browser exists to prevent.

// PaneShape names which of a Resource leaf's two tab shapes a pane-mode browser
// is showing.
type PaneShape int

const (
	// PaneNone is a browser that is not in pane mode.
	PaneNone PaneShape = iota
	// PaneCollection is a collection tab: the list, full width.
	PaneCollection
	// PaneDocument is a document tab: one document, full width.
	PaneDocument
)

// SetPaneCollection puts the browser in pane mode showing one collection.
//
// The collection is pinned: the browser never cycles off it, and a describe
// that stops declaring it leaves the pane saying so rather than quietly showing
// a different collection under the same tab.
func (m *Model) SetPaneCollection(id string) {
	if m == nil {
		return
	}
	m.paneShape = PaneCollection
	m.paneCollection = strings.TrimSpace(id)
	m.focus = FocusList
}

// SetPaneDocument puts the browser in pane mode showing one document, and
// returns the command that fetches it. A document tab has no list.
func (m *Model) SetPaneDocument(collection, id string) tea.Cmd {
	if m == nil {
		return nil
	}
	m.paneShape = PaneDocument
	m.paneCollection = strings.TrimSpace(collection)
	m.focus = FocusDetail
	return m.openDocument(m.paneCollection, strings.TrimSpace(id), false)
}

// ArmPaneDocument points the browser at one row without fetching it. A restored
// tab starts here, so relaunch does not fan out one process per remembered tab
// and the strip still names what each tab is.
func (m *Model) ArmPaneDocument(collection, id string) {
	if m == nil {
		return
	}
	m.paneShape = PaneDocument
	m.paneCollection = strings.TrimSpace(collection)
	m.focus = FocusDetail
	m.detail.collection = m.paneCollection
	m.detail.id = strings.TrimSpace(id)
}

// PaneShape reports which shape the browser is in, if any.
func (m *Model) PaneShape() PaneShape {
	if m == nil {
		return PaneNone
	}
	return m.paneShape
}

// PaneCollection is the pinned collection ID.
func (m *Model) PaneCollection() string {
	if m == nil {
		return ""
	}
	return m.paneCollection
}

// SetOnOpenRow installs what Enter on a row does in pane mode. A pane has no
// room for the document beside the list, so the host opens it as a second tab
// in the same leaf; without a handler Enter does nothing rather than replacing
// the list.
func (m *Model) SetOnOpenRow(open func(collection, id string) tea.Cmd) {
	if m != nil {
		m.onOpenRow = open
	}
}

// paneMode reports whether this browser is bound to a pane leaf.
func (m *Model) paneMode() bool { return m != nil && m.paneShape != PaneNone }

// paneVisibleCollections narrows Collections to the pinned one. A pinned
// collection the newest describe does not declare yields nothing, which renders
// as the "this collection is gone" card rather than as some other collection.
func (m *Model) paneVisibleCollections(all []pluginhost.Collection) []pluginhost.Collection {
	if !m.paneMode() || m.paneShape == PaneDocument {
		if m.paneShape == PaneDocument {
			return nil
		}
		return all
	}
	for _, c := range all {
		if c.ID == m.paneCollection {
			return []pluginhost.Collection{c}
		}
	}
	return nil
}

// PaneQuery, PaneView, PaneSort and PaneCursorID are the collection tab's view
// position, which is what the host persists and restores. They read the live
// state rather than a copy, so a tab saved mid-query saves the query the user
// can see.
func (m *Model) PaneQuery() string {
	if s := m.paneState(); s != nil {
		return s.query
	}
	return ""
}

func (m *Model) PaneView() string {
	if s := m.paneState(); s != nil {
		return s.view
	}
	return ""
}

func (m *Model) PaneSort() string {
	if s := m.paneState(); s != nil {
		return s.sortKey
	}
	return ""
}

// PaneFilters is the applied filter set, as the map the wire and the persisted
// record both use. It is a copy: a tab's state must not be editable through
// what the host saved.
func (m *Model) PaneFilters() map[string]string {
	s := m.paneState()
	if s == nil || len(s.filters) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.filters))
	for k, v := range s.filters {
		out[k] = v
	}
	return out
}

func (m *Model) PaneCursorID() string {
	s := m.paneState()
	if s == nil || s.cursor < 0 || s.cursor >= len(s.items) {
		return ""
	}
	return s.items[s.cursor].ID
}

// PaneViewApplied reports that a restored view position has been folded into
// live collection state. Until it has, the reference the host holds is still
// the authority on what this tab is showing.
func (m *Model) PaneViewApplied() bool {
	return m != nil && m.paneMode() && !m.restore.pending && m.paneState() != nil
}

// paneState is the pinned collection's state, created on demand once describe
// has declared it. It is nil before that, which is the loading card.
func (m *Model) paneState() *collectionState {
	if !m.paneMode() || m.paneCollection == "" {
		return nil
	}
	if s, ok := m.states[m.paneCollection]; ok {
		return s
	}
	c, ok := m.desc.Collection(m.paneCollection)
	if !ok {
		return nil
	}
	return m.state(c)
}

// RestorePaneView reinstates a persisted collection tab's view position before
// anything is listed, so the first list is the one the user was reading rather
// than the collection's default page followed by a correction.
//
// cursorID is the row the cursor was on. It is applied when the page it names
// arrives, because the row's position in a re-listed page is not knowable here.
func (m *Model) RestorePaneView(query, view, sort, cursorID string, filters map[string]string) {
	if m == nil || m.paneCollection == "" {
		return
	}
	m.restore = paneRestore{query: query, view: view, sort: sort, cursorID: cursorID, filters: filters, pending: true}
	if s, ok := m.states[m.paneCollection]; ok {
		m.applyRestore(s)
	}
}

// paneRestore is a persisted view position waiting for the collection it names
// to be declared.
type paneRestore struct {
	query    string
	view     string
	sort     string
	cursorID string
	filters  map[string]string
	pending  bool
}

// applyRestore folds a persisted view position into a collection state exactly
// once. The sort and view are adopted only when the newest describe still
// declares them: a plugin that dropped a view must not have the host asking for
// it forever.
func (m *Model) applyRestore(s *collectionState) {
	if s == nil || !m.restore.pending || s.id != m.paneCollection {
		return
	}
	m.restore.pending = false
	s.query = m.restore.query
	c, ok := m.desc.Collection(s.id)
	if !ok {
		return
	}
	for _, v := range c.Views {
		if v.ID == m.restore.view {
			s.view = m.restore.view
			break
		}
	}
	for _, key := range c.Sort {
		if key.ID == m.restore.sort {
			s.sortKey = m.restore.sort
			break
		}
	}
	// A restored filter is adopted only where the newest describe still
	// declares it, and only where its value is still one the control can show:
	// a plugin that dropped a filter, or renamed a choice, must not have the
	// host asking for it forever. The host would drop the key at call time
	// anyway; dropping it here is what stops the View modal opening on a value
	// nothing can select.
	s.filters = adoptFilters(c, m.restore.filters)
	m.pendingCursorID = m.restore.cursorID
}

// adoptFilters keeps the restored values a live declaration can still express.
func adoptFilters(c pluginhost.Collection, restored map[string]string) map[string]string {
	if len(restored) == 0 {
		return nil
	}
	out := make(map[string]string, len(restored))
	for _, f := range c.Filters {
		value, ok := restored[f.ID]
		if !ok {
			continue
		}
		if f.Kind == pluginhost.FilterChoice {
			if _, known := f.OptionTitle(value); !known {
				continue
			}
		}
		if value == f.Default {
			continue
		}
		out[f.ID] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// restoreCursor puts the cursor back on the row a restored tab was reading, if
// that row is still in the page. A row that is gone leaves the cursor at the
// top, which is what a list the user has not seen yet should show.
func (m *Model) restoreCursor(s *collectionState) {
	if m.pendingCursorID == "" || s == nil {
		return
	}
	want := m.pendingCursorID
	m.pendingCursorID = ""
	for i, item := range s.items {
		if item.ID == want {
			s.cursor = i
			m.clampListScroll(s)
			return
		}
	}
}

// PaneTitle is the leaf header's label: the collection's declared title for a
// collection tab, and the document's own title for a document tab. Neither
// falls back to the plugin's name, because the leaf header already carries it.
func (m *Model) PaneTitle() string {
	if m == nil {
		return ""
	}
	switch m.paneShape {
	case PaneDocument:
		if m.detail.loaded && m.detail.doc.Title != "" {
			return m.detail.doc.Title
		}
		if m.detail.id != "" {
			return m.detail.id
		}
		return m.Name()
	case PaneCollection:
		if c, ok := m.desc.Collection(m.paneCollection); ok && c.Title != "" {
			return c.Title
		}
		if m.paneCollection != "" {
			return m.paneCollection
		}
	}
	return m.Name()
}

// PaneTabLabel is the tab strip's text for this pane. A collection tab is named
// by its collection so two collections of one plugin are told apart; a document
// tab is named by what it is showing.
func (m *Model) PaneTabLabel() string {
	label := m.PaneTitle()
	if label == "" {
		return m.instance
	}
	return label
}

// PaneDocumentIdentity is the provider-stable identity of a document tab's
// document, once it has landed. The host re-keys the tab to it, exactly as a
// resolve already re-keys a resource card.
func (m *Model) PaneDocumentIdentity() string {
	if m == nil || m.paneShape != PaneDocument || !m.detail.loaded {
		return ""
	}
	return m.detail.doc.Identity
}

// PaneDocumentShowing reports that this pane is already showing (or fetching)
// the row named by id, so re-selecting it needs no process.
func (m *Model) PaneDocumentShowing(id string) bool {
	return m != nil && m.paneShape == PaneDocument && m.detail.id == id &&
		(m.detail.loaded || m.detail.loading)
}

// PaneDocumentID is the row ID a document tab was opened for.
func (m *Model) PaneDocumentID() string {
	if m == nil {
		return ""
	}
	return m.detail.id
}

// PaneSourceURL is the validated http(s) URL `o` would open in this pane.
func (m *Model) PaneSourceURL() string {
	if m == nil {
		return ""
	}
	if m.paneShape == PaneDocument {
		if m.detail.loaded {
			return m.detail.doc.SourceURL
		}
		return ""
	}
	if item, ok := m.currentItem(); ok {
		return item.SourceURL
	}
	return ""
}

// PaneRefresh re-reads whatever this pane is showing. It is what the live
// refresh binding drives on both surfaces and what `r` runs, so a watched file
// changing and the user pressing a key do exactly the same thing.
func (m *Model) PaneRefresh() tea.Cmd {
	if m == nil {
		return nil
	}
	if !m.described {
		return m.Refresh()
	}
	switch m.paneShape {
	case PaneDocument:
		if m.detail.id == "" {
			return nil
		}
		return m.openDocument(m.detail.collection, m.detail.id, true)
	case PaneCollection:
		c, ok := m.desc.Collection(m.paneCollection)
		if !ok {
			return nil
		}
		return m.list(c, m.state(c), false)
	}
	return nil
}

// PaneWatchPaths are the declared watch paths of whatever this pane is showing.
//
// It reads the cached describe snapshot and nothing else: it runs on the update
// goroutine on every live-refresh reconcile, so resolving anything here would
// be exactly the hidden per-frame filesystem cost the startup rules exist to
// prevent. A collection with no refresh declaration contributes none, which is
// how a plugin opts out of being watched.
func (m *Model) PaneWatchPaths() []string {
	if m == nil || !m.described || m.paneCollection == "" {
		return nil
	}
	c, ok := m.desc.Collection(m.paneCollection)
	if !ok {
		return nil
	}
	return c.Refresh.Watch
}

// PanePollSeconds is the declared poll interval for what this pane is showing,
// already clamped by describe validation. Zero means the plugin declared none
// and nothing should tick.
func (m *Model) PanePollSeconds() int {
	if m == nil || !m.described || m.paneCollection == "" {
		return 0
	}
	c, ok := m.desc.Collection(m.paneCollection)
	if !ok {
		return 0
	}
	return c.Refresh.EverySeconds
}

// DescribeGeneration counts the describe results this browser has adopted. The
// live-refresh binding validates its watch set once per generation rather than
// on every reconcile.
func (m *Model) DescribeGeneration() uint64 {
	if m == nil {
		return 0
	}
	return m.describeGeneration
}

// paneView renders a pane-mode browser: one box, the whole width, holding
// whichever of the two shapes this tab is.
func (m *Model) paneView() string {
	inner := m.width - chromeOverhead
	active := m.focused && m.innerFocusActive()
	var lines []string
	box := mouse.Rect{W: m.width, H: m.height}
	if m.paneShape == PaneDocument {
		lines = m.detailLines(inner, m.height-2)
		m.registerRegions(mouse.Rect{}, box, mouse.Rect{})
	} else {
		lines = m.listLines(inner, m.height-2)
		m.registerRegions(box, mouse.Rect{}, mouse.Rect{})
	}
	panel := styles.RenderPanel(strings.Join(lines, "\n"), m.width, m.height, active)
	return m.overlayView(ui.FitBlock(panel, m.width, m.height))
}

// paneOpenRow is Enter in pane mode: the host opens the document as a second
// tab in the same leaf, and a second Enter on a row whose tab is already open
// focuses that tab rather than fetching it again.
func (m *Model) paneOpenRow() tea.Cmd {
	c, ok := m.ActiveCollection()
	if !ok || !c.Detail || m.onOpenRow == nil {
		return nil
	}
	item, ok := m.currentItem()
	if !ok {
		return nil
	}
	return m.onOpenRow(c.ID, item.ID)
}

// PaneScroll is the wheel over a pane-mode browser. Over a list it moves the
// cursor, which is what "scrolling a list" means everywhere else in Sidecar;
// over a document it moves the viewport.
func (m *Model) PaneScroll(delta int) bool {
	if m == nil || delta == 0 {
		return false
	}
	if m.paneShape == PaneDocument {
		before := m.detail.scroll
		m.detail.scroll += delta
		m.clampDetailScroll()
		return m.detail.scroll != before
	}
	s := m.activeState()
	if s == nil {
		return false
	}
	before := s.cursor
	m.moveCursor(delta)
	return s.cursor != before
}

// PaneScrollAtBoundary reports whether a scroll would move nothing, so the host
// hands the wheel to whatever is underneath instead of swallowing it.
func (m *Model) PaneScrollAtBoundary(delta int) bool {
	if m == nil {
		return true
	}
	if m.paneShape == PaneDocument {
		if delta < 0 {
			return m.detail.scroll <= 0
		}
		return m.detail.scroll >= m.maxDetailScroll()
	}
	s := m.activeState()
	if s == nil || len(s.items) == 0 {
		return true
	}
	if delta < 0 {
		return s.cursor <= 0
	}
	// A page with more behind it is never at the boundary: reaching for the row
	// past the end is exactly what fetches the next page.
	return s.cursor >= len(s.items)-1 && s.nextCursor == ""
}

// IsBrowserMsg reports whether msg is one of the browser's own answers or
// ticks. Hosts use it to route background traffic to pane-mode browsers without
// keeping their own list of the message types, which would go stale the moment
// one is added.
func IsBrowserMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case DescribedMsg, QueryDebouncedMsg, ListedMsg, GotMsg, ActedMsg, ChangedMsg:
		return true
	}
	return false
}

// PaneDocument exposes a document tab's loaded document, which is what a host
// asks for to answer "is there anything here to link".
func (m *Model) PaneDocument() (resource.Document, bool) {
	if m == nil || m.paneShape != PaneDocument {
		return resource.Document{}, false
	}
	return m.detail.doc, m.detail.loaded
}

// HandleKeyString routes a key given as the string a host's key ladder already
// works in. It is the same path HandleKey takes, so a pane and a tab cannot
// answer one key differently.
func (m *Model) HandleKeyString(key string) (tea.Cmd, bool) {
	if m == nil || key == "" {
		return nil, false
	}
	return m.HandleKey(keyMsg(key))
}

// keyMsg rebuilds a KeyPressMsg from the string spelling of a key. A single
// printable rune carries its own text, which is what the query line types with;
// everything else is a named key. A key with neither spelling is returned as a
// no-op message rather than guessed at.
func keyMsg(key string) tea.KeyPressMsg {
	runes := []rune(key)
	if len(runes) == 1 {
		return tea.KeyPressMsg{Code: runes[0], Text: key}
	}
	if code, ok := namedKeys[key]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	return tea.KeyPressMsg{}
}

// namedKeys are the non-printable keys the browser acts on. It is deliberately
// only those: a key the browser does not bind has no reason to be reconstructed.
var namedKeys = map[string]rune{
	"enter":     tea.KeyEnter,
	"esc":       tea.KeyEscape,
	"tab":       tea.KeyTab,
	"backspace": tea.KeyBackspace,
	"up":        tea.KeyUp,
	"down":      tea.KeyDown,
	"left":      tea.KeyLeft,
	"right":     tea.KeyRight,
	"home":      tea.KeyHome,
	"end":       tea.KeyEnd,
	"pgup":      tea.KeyPgUp,
	"pgdown":    tea.KeyPgDown,
	"space":     tea.KeySpace,
}

// ChangedMsg says one plugin's data moved and its visible tabs should re-list.
//
// It arrives from `sidecar plugin changed` on the request bus — the poke a
// plugin gives Sidecar when the change is not a file it declared under
// refresh.watch. An empty Collection means every collection of that plugin,
// which is what a tool that does not know what it touched should say.
type ChangedMsg struct {
	Instance   string
	Collection string
}

// applyChanged re-reads if the message names this browser, and does nothing
// otherwise. A browser whose plugin was not named is not "unchanged"; it was
// never being talked about.
func (m *Model) applyChanged(msg ChangedMsg) tea.Cmd {
	if m == nil || msg.Instance != m.instance {
		return nil
	}
	if m.paneMode() {
		if msg.Collection != "" && msg.Collection != m.paneCollection {
			return nil
		}
		return m.PaneRefresh()
	}
	if c, ok := m.ActiveCollection(); ok && msg.Collection != "" && msg.Collection != c.ID {
		return nil
	}
	return m.refreshActive()
}
