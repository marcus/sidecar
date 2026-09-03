package pluginbrowser

import (
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/state"
)

// QueryDebounce is how long the browser waits after a keystroke before it
// spends a process on the new query. It is the protocol's stated figure, and it
// is a package constant rather than a literal because the conformance story
// ("search as you type costs one process per pause, not one per keystroke")
// depends on exactly this value.
const QueryDebounce = 250 * time.Millisecond

// listLimit is the page size the browser asks for. The host clamps it; asking
// for the clamp explicitly means the request the plugin sees does not change
// when the host's own limit does.
const listLimit = pluginhost.MaxPageItems

// Focus names the two windows inside the browser. They are the browser's own
// focus ring, projected to the host through PaneFocusProvider rather than
// duplicated by it.
type Focus string

const (
	// FocusList is the collection table.
	FocusList Focus = "list"
	// FocusDetail is the document box beside it.
	FocusDetail Focus = "detail"
)

// collectionState is everything one collection remembers while the browser is
// alive: what was asked, what came back, and where the cursor is.
type collectionState struct {
	id string

	query   string
	editing bool
	// debounce is the newest scheduled query tick. A tick whose sequence is not
	// this one is a keystroke the user has already typed past.
	debounce uint64
	// atLimit records that a keystroke was refused because the query is as long
	// as the protocol allows. It is the query row's own feedback, cleared by the
	// next edit that is accepted, because a bound reached silently reads as a
	// wedged pane.
	atLimit bool

	view    string
	sortKey string
	sortDir pluginhost.SortDir
	// filters is the applied set: only values that differ from their filter's
	// declared default. A missing key means the default, exactly as it does on
	// the wire, so there is one spelling of "nothing is applied".
	filters map[string]string

	items      []pluginhost.Item
	outcome    pluginhost.PageOutcome
	notices    []pluginhost.Notice
	omitted    pluginhost.Omitted
	coverage   []pluginhost.Coverage
	total      int
	nextCursor string
	truncated  bool
	// coverageTruncated records that the plugin sent more coverage rows than
	// the host keeps, so the modal can say the table is not the whole ledger.
	coverageTruncated bool
	// unqueried is the browser's own state for a `search: required` collection
	// nobody has typed into. It is NOT `abstained`: nothing was asked, so no
	// claim was made, and the stored outcome of the empty page is an artefact
	// rather than an answer (td-c2dc19). Every surface that would otherwise
	// read the outcome word asks this first.
	unqueried bool

	cursor int
	scroll int

	loading bool
	paging  bool
	loaded  bool
	err     *resource.Error

	// generation stamps every call, so an answer to a superseded query, view or
	// sort is discarded rather than painted over the newer one.
	generation uint64
}

// detailState is the document the browser is showing beside the list.
type detailState struct {
	collection string
	id         string
	loading    bool
	loaded     bool
	doc        resource.Document
	err        *resource.Error
	scroll     int
	generation uint64

	// body is the rendered body cached per width, generation and renderer style
	// key, so a resize during a drag does not re-run the markdown renderer every
	// frame — and a theme change does, because the cached lines carry the old
	// palette in their escape sequences.
	body         []string
	bodyForW     int
	bodyForGen   uint64
	bodyForStyle string
}

// Model is one protocol plugin in one content box.
type Model struct {
	instance string
	name     string
	calls    Calls
	renderer *markdown.Renderer

	width, height int

	focused bool
	focus   Focus
	// paneFocusManaged records that an outer deck composes this browser's pane
	// focus. Until one does, the browser's own focused window draws its own
	// chrome, which is what a surface that is not inside a deck wants.
	paneFocusManaged bool
	paneFocusActive  bool

	described bool
	desc      pluginhost.Description
	status    pluginhost.Status

	states map[string]*collectionState
	active int

	detail detailState

	// grantedKeys maps a plugin-suggested letter onto the action it was granted
	// for. It is rebuilt on every describe and never persisted, because a key
	// the host granted once must not outlive the declaration that asked for it.
	grantedKeys map[string]string
	// reservedKeys are the surface's own bindings, supplied by the host. A
	// plugin-suggested key that collides with one is refused.
	reservedKeys map[string]bool

	overlay overlay

	// flash is the one line an act outcome leaves behind, shown in the host
	// footer rather than in a toast the browser would have to own.
	flash    string
	flashErr bool

	// seq stamps act calls, which are neither cached nor deduplicated.
	seq uint64

	// Pane mode. paneShape is PaneNone in a tab placement; the rest is the one
	// collection or document a Resource leaf's tab is pinned to. See pane.go.
	paneShape       PaneShape
	paneCollection  string
	onOpenRow       func(collection, id string) tea.Cmd
	restore         paneRestore
	pendingCursorID string

	// id distinguishes this browser from every other one in the process. Two
	// browsers of one plugin are two independent readers, and an answer that
	// did not name which asked would land in both.
	id uint64

	// describeGeneration counts adopted describe results, so the live-refresh
	// binding validates its watch set once per generation rather than on every
	// reconcile.
	describeGeneration uint64

	// Pointer. One handler owns the hit map, which View clears and rebuilds in
	// paint order; geom is where the frame's targets ended up, in the
	// coordinates of the box that drew them. See mouse.go.
	mouse     *mouse.Handler
	geom      frameGeom
	listBar   browserBar
	docBar    browserBar
	hoverRail bool
	// railDragged records that a rail drag is in flight, so the split is saved
	// however the gesture ends. A release is not guaranteed: a button-less
	// motion after a lost one ends the drag inside internal/mouse and arrives
	// here as hover.
	railDragged bool
	// share is the list's percentage of the box. Zero until it is read from
	// state, because a browser is built before it knows how wide it will be.
	share int

	// tabViewRestored records that a global tab has already reinstated its
	// remembered query, view, sort and filters. It happens once: a second pass
	// would overwrite what the user has typed since.
	tabViewRestored bool
}

// New builds a browser for one configured instance.
//
// Construction does no I/O and starts nothing: the model renders a loading card
// until the host's describe pass has settled, which is what keeps a protocol
// plugin off the first-frame path.
func New(instance, name string, calls Calls, renderer *markdown.Renderer) *Model {
	if renderer == nil {
		renderer, _ = markdown.NewRenderer()
	}
	if name == "" {
		name = instance
	}
	m := &Model{
		id:           nextBrowserID.Add(1),
		instance:     instance,
		focused:      true,
		name:         name,
		calls:        calls,
		renderer:     renderer,
		focus:        FocusList,
		states:       make(map[string]*collectionState),
		grantedKeys:  make(map[string]string),
		reservedKeys: make(map[string]bool),
		detail:       detailState{bodyForW: -1},
		mouse:        mouse.NewHandler(),
	}
	// Every browser is a Sidecar surface, so every browser refuses the keys
	// Sidecar owns. Seeding it here rather than at one call site is what stops
	// the next placement from forgetting: a pane-mode browser used to grant a
	// plugin `1`, `q` or `i` because only the tab placement asked for the
	// surface's reserved set (td-fcb648).
	m.SetReservedKeys(surfaceReservedKeys())
	return m
}

// nextBrowserID hands each browser a distinct identity for the lifetime of the
// process.
var nextBrowserID atomic.Uint64

// Instance is the configured instance ID this browser renders.
func (m *Model) Instance() string { return m.instance }

// Name is the display name: the plugin's own once describe has landed, and the
// configured ID until then. It never changes to something the settings page and
// the CLI would disagree with, because both read the same describe result.
func (m *Model) Name() string {
	if m.described && m.desc.Info.Name != "" {
		return m.desc.Info.Name
	}
	return m.name
}

// SetCalls replaces the host seam. A browser built before the host had a
// manager observes one appearing this way.
func (m *Model) SetCalls(calls Calls) { m.calls = calls }

// SetReservedKeys records the keys the surface already binds, so a
// plugin-suggested action letter that collides with one is refused rather than
// silently stealing it.
func (m *Model) SetReservedKeys(keys map[string]bool) {
	m.reservedKeys = make(map[string]bool, len(keys))
	for k, v := range keys {
		if v {
			m.reservedKeys[k] = true
		}
	}
	m.grantKeys()
}

// SetSize records the content box.
func (m *Model) SetSize(width, height int) {
	if width == m.width && height == m.height {
		return
	}
	m.width, m.height = width, height
	m.detail.invalidateBody()
}

// SetFocused records whether the browser owns the keyboard.
func (m *Model) SetFocused(focused bool) { m.focused = focused }

// Focused reports whether the browser owns the keyboard.
func (m *Model) Focused() bool { return m.focused }

// Described reports whether a describe result has landed.
func (m *Model) Described() bool { return m.described }

// Description returns the newest describe result.
func (m *Model) Description() pluginhost.Description { return m.desc }

// Status returns the newest describe status.
func (m *Model) Status() pluginhost.Status { return m.status }

// Collections are the collections the browser can show, which is every declared
// one. A collection that narrows itself to project context and has none is
// hidden: showing a tab that can only ever be empty claims a capability the
// surface does not have.
func (m *Model) Collections() []pluginhost.Collection {
	if !m.described {
		return nil
	}
	hasProject := false
	if m.calls.Context != nil {
		if ctx := m.calls.Context(); ctx != nil && ctx.Project != nil {
			hasProject = true
		}
	}
	out := make([]pluginhost.Collection, 0, len(m.desc.Collections))
	for _, c := range m.desc.Collections {
		if !hasProject && collectionNeedsProject(c) {
			continue
		}
		out = append(out, c)
	}
	return m.paneVisibleCollections(out)
}

func collectionNeedsProject(c pluginhost.Collection) bool {
	for _, kind := range c.Context {
		if kind == pluginhost.ContextProject {
			return true
		}
	}
	return false
}

// ActiveCollection is the collection on screen, and whether there is one.
func (m *Model) ActiveCollection() (pluginhost.Collection, bool) {
	cols := m.Collections()
	if len(cols) == 0 {
		return pluginhost.Collection{}, false
	}
	if m.active < 0 || m.active >= len(cols) {
		m.active = 0
	}
	return cols[m.active], true
}

// state returns the remembered state for a collection, creating it with the
// collection's declared default sort the first time.
func (m *Model) state(c pluginhost.Collection) *collectionState {
	if s, ok := m.states[c.ID]; ok {
		return s
	}
	s := &collectionState{id: c.ID}
	// Filters start empty, which IS the declared defaults: a missing key means
	// the default everywhere, so there is nothing to seed.
	for _, key := range c.Sort {
		if key.Default != "" {
			s.sortKey, s.sortDir = key.ID, key.Default
			break
		}
	}
	m.states[c.ID] = s
	return s
}

// activeState is the state of the collection on screen, or nil.
func (m *Model) activeState() *collectionState {
	c, ok := m.ActiveCollection()
	if !ok {
		return nil
	}
	return m.state(c)
}

// Refresh re-reads the host's describe result and re-lists the active
// collection if anything it depends on moved. It is what a DescribedMsg and the
// first focus both run.
func (m *Model) Refresh() tea.Cmd {
	changed := m.readDescription()
	cmd := m.ensureListed()
	if changed && cmd == nil {
		return nil
	}
	return cmd
}

// readDescription pulls the host's cached describe outcome into the model and
// reports whether it changed anything.
func (m *Model) readDescription() bool {
	if m.calls.Describe == nil {
		return false
	}
	desc, status, ok := m.calls.Describe(m.instance)
	if !ok {
		return false
	}
	before := m.described
	beforeState := m.status.State
	m.status = status
	if status.State == pluginhost.StateReady {
		// The generation counts describe snapshots, not reads of one. Refresh()
		// runs on every tab focus and on every Resolve, so bumping it here
		// unconditionally would make "once per describe generation" mean "on
		// every focus" for the live-refresh binding that keys its validated
		// watch set by it — one stat per declared path per focus, and one cache
		// entry per focus for the life of the process.
		if !m.described || !describeEqual(m.desc, desc) {
			m.describeGeneration++
		}
		m.desc = desc
		m.described = true
		m.grantKeys()
		m.pruneStates()
		if s := m.paneState(); s != nil {
			m.applyRestore(s)
		}
		m.restoreTabView()
	}
	return before != m.described || beforeState != status.State
}

// describeEqual reports whether two describe snapshots say the same thing. It
// is a deep comparison of a small, flat, plugin-declared structure — a handful
// of collections, columns, actions and paths — run once per Refresh, not per
// frame.
func describeEqual(a, b pluginhost.Description) bool { return reflect.DeepEqual(a, b) }

// pruneStates drops remembered state for collections the newest describe no
// longer declares. Keeping it would mean a re-describe could resurrect a query
// against a collection that has gone away.
func (m *Model) pruneStates() {
	live := make(map[string]bool, len(m.desc.Collections))
	for _, c := range m.desc.Collections {
		live[c.ID] = true
	}
	for id := range m.states {
		if !live[id] {
			delete(m.states, id)
		}
	}
	if m.detail.collection != "" && !live[m.detail.collection] {
		m.detail = detailState{bodyForW: -1}
	}
}

// grantKeys resolves each plugin-suggested action letter. A suggestion is a
// request, never a grant: the host refuses anything it owns, anything the
// surface binds, and anything a second action already took.
func (m *Model) grantKeys() {
	m.grantedKeys = make(map[string]string, len(m.desc.Actions))
	for _, action := range m.desc.Actions {
		key := strings.TrimSpace(action.Key)
		if key == "" {
			continue
		}
		if browserOwnedKeys[key] || m.reservedKeys[key] {
			continue
		}
		if _, taken := m.grantedKeys[key]; taken {
			continue
		}
		m.grantedKeys[key] = action.ID
	}
}

// GrantedKey reports the action a plugin-suggested letter was granted for.
func (m *Model) GrantedKey(key string) (string, bool) {
	id, ok := m.grantedKeys[key]
	return id, ok
}

// ensureListed starts the first list for the active collection if it has never
// been listed. It is a no-op for a collection that is loaded, loading, or
// waiting on a required query.
func (m *Model) ensureListed() tea.Cmd {
	c, ok := m.ActiveCollection()
	if !ok {
		return nil
	}
	s := m.state(c)
	if s.loaded || s.loading {
		return nil
	}
	return m.list(c, s, false)
}

// list asks for a page. append extends the current items with the next cursor;
// otherwise the page replaces them.
//
// A required-search collection with an empty query is answered here, without a
// process: that is the protocol's rule, and it is what keeps a search box free
// until there is something to search for.
func (m *Model) list(c pluginhost.Collection, s *collectionState, appendPage bool) tea.Cmd {
	if !appendPage {
		// A new page is a new subject. The line an act left behind described the
		// rows that were on screen when it ran, and standing beside a different
		// query's results it is no longer true of anything (td-c2dc19).
		m.flash, m.flashErr = "", false
	}
	if c.Search == pluginhost.SearchRequired && strings.TrimSpace(s.query) == "" {
		s.items = nil
		// Not abstained: the plugin was never asked, so it claimed nothing.
		s.outcome = ""
		s.unqueried = true
		s.notices = nil
		s.omitted = pluginhost.Omitted{}
		s.coverage = nil
		s.coverageTruncated = false
		s.total = 0
		s.nextCursor = ""
		s.cursor, s.scroll = 0, 0
		s.loading, s.paging = false, false
		s.loaded = true
		s.err = nil
		s.generation++
		return nil
	}
	s.unqueried = false
	if m.calls.List == nil {
		return nil
	}
	s.generation++
	cursor := ""
	if appendPage {
		cursor = s.nextCursor
		if cursor == "" {
			return nil
		}
		s.paging = true
	} else {
		s.loading = true
	}
	m.persistTabView(c, s)
	call := ListCall{
		Instance: m.instance,
		Browser:  m.id,
		PaneKey:  paneKey(m.id, m.instance, c.ID),
		Params: pluginhost.ListParams{
			Collection: c.ID,
			Query:      s.query,
			View:       s.view,
			Sort:       pluginhost.SortOrder{Key: s.sortKey, Dir: string(s.sortDir)},
			Filters:    pluginhost.NormalizeFilters(c, s.filters),
			Cursor:     cursor,
			Limit:      listLimit,
		},
		Context:    m.context(),
		Generation: s.generation,
		Append:     appendPage,
	}
	return m.calls.List(call)
}

func (m *Model) context() *pluginhost.Context {
	if m.calls.Context == nil {
		return nil
	}
	return m.calls.Context()
}

// Update routes one message. It returns only commands; nothing here starts a
// process itself.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case DescribedMsg:
		return m.Refresh()
	case QueryDebouncedMsg:
		return m.applyDebounce(msg)
	case ListedMsg:
		return m.applyListed(msg)
	case GotMsg:
		m.applyGot(msg)
		return nil
	case ActedMsg:
		return m.applyActed(msg)
	case ChangedMsg:
		return m.applyChanged(msg)
	}
	return nil
}

// QueryDebouncedMsg fires QueryDebounce after a keystroke. Only the newest
// sequence for a collection is acted on; every earlier one is a keystroke the
// user typed past.
//
// It is exported because a pane-mode browser's ticks travel the host's message
// bus like every other background answer, and a host cannot route a type it
// cannot name.
type QueryDebouncedMsg struct {
	Instance   string
	Collection string
	Sequence   uint64
}

func (m *Model) applyDebounce(msg QueryDebouncedMsg) tea.Cmd {
	if msg.Instance != m.instance {
		return nil
	}
	c, ok := m.ActiveCollection()
	if !ok || c.ID != msg.Collection {
		return nil
	}
	s := m.state(c)
	if s.debounce != msg.Sequence {
		return nil
	}
	return m.list(c, s, false)
}

func (m *Model) applyListed(msg ListedMsg) tea.Cmd {
	if msg.Instance != m.instance || msg.Browser != m.id {
		return nil
	}
	s, ok := m.states[msg.Collection]
	if !ok || msg.Generation != s.generation {
		// A page for a query, view, sort or collection the user has moved past.
		return nil
	}
	s.loading, s.paging = false, false
	s.loaded = true
	if msg.Err != nil {
		s.err = pluginhost.AsResourceError(msg.Err)
		if !msg.Append {
			s.items = nil
			s.total = 0
			s.nextCursor = ""
			s.outcome = pluginhost.OutcomeDegraded
			s.notices = nil
			s.omitted = pluginhost.Omitted{}
			s.coverage = nil
			s.coverageTruncated = false
			s.cursor, s.scroll = 0, 0
		}
		return nil
	}
	s.err = nil
	if msg.Append {
		s.items = append(s.items, msg.Page.Items...)
	} else {
		s.items = msg.Page.Items
		s.cursor, s.scroll = 0, 0
	}
	s.outcome = msg.Page.Outcome
	s.unqueried = false
	s.notices = msg.Page.Notices
	s.omitted = msg.Page.Omitted
	s.coverage = msg.Page.Coverage
	s.coverageTruncated = msg.Page.CoverageTruncated
	s.total = msg.Page.Total
	s.nextCursor = msg.Page.NextCursor
	s.truncated = msg.Page.Truncated
	m.clampCursor(s)
	if !msg.Append {
		m.restoreCursor(s)
	}
	return nil
}

func (m *Model) applyGot(msg GotMsg) {
	if msg.Instance != m.instance || msg.Browser != m.id || msg.Generation != m.detail.generation {
		return
	}
	m.detail.loading = false
	if msg.Err != nil {
		m.detail.err = pluginhost.AsResourceError(msg.Err)
		// A failed refresh keeps the document it was refreshing, exactly as the
		// resource card does: replacing what the user is reading with an error
		// would lose the thing the refresh was meant to update.
		if !m.detail.loaded {
			m.detail.doc = resource.Document{}
		}
		return
	}
	m.detail.err = nil
	m.detail.doc = msg.Document
	m.detail.loaded = true
	m.detail.invalidateBody()
}

func (m *Model) applyActed(msg ActedMsg) tea.Cmd {
	if msg.Instance != m.instance || msg.Browser != m.id || msg.Generation != m.seq {
		return nil
	}
	if msg.Err != nil {
		err := pluginhost.AsResourceError(msg.Err)
		m.flash, m.flashErr = actionFailure(err), true
		return nil
	}
	flashErr := msg.Outcome.Status != pluginhost.ActDone
	flash := msg.Outcome.Message
	if flash == "" {
		if flashErr {
			flash = "The action failed."
		} else {
			flash = "Done."
		}
	}
	if flashErr {
		m.flash, m.flashErr = flash, flashErr
		return nil
	}
	// The line is set after the refreshes this outcome asked for, not before:
	// a new page clears the standing line, and the refresh an act triggers is
	// the one page the act's own line is still true of.
	defer func() { m.flash, m.flashErr = flash, flashErr }()

	var cmds []tea.Cmd
	for _, id := range msg.Outcome.Refresh {
		for _, c := range m.Collections() {
			if c.ID != id {
				continue
			}
			s := m.state(c)
			if !s.loaded && !s.loading {
				continue
			}
			if cmd := m.list(c, s, false); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	// A document whose row an action just touched is re-fetched, so the box
	// beside the list is never one action out of date.
	if m.detail.loaded || m.detail.loading {
		for _, id := range msg.Outcome.Refresh {
			if id == m.detail.collection {
				if cmd := m.openDocument(m.detail.collection, m.detail.id, true); cmd != nil {
					cmds = append(cmds, cmd)
				}
				break
			}
		}
	}
	if target := msg.Outcome.Open; target != nil && target.ID != "" {
		if cmd := m.openDocument(target.Collection, target.ID, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func actionFailure(err *resource.Error) string {
	if err == nil {
		return "The action failed."
	}
	if err.Message != "" {
		return err.Message
	}
	return string(err.Code)
}

// openDocument asks for one row's document. A collection the newest describe
// does not declare is refused here rather than sent, which is the same rule the
// manager holds one layer down.
func (m *Model) openDocument(collection, id string, refresh bool) tea.Cmd {
	if m.calls.Get == nil || id == "" {
		return nil
	}
	c, ok := m.desc.Collection(collection)
	if !ok || !c.Detail {
		return nil
	}
	m.detail.generation++
	m.detail.collection = collection
	m.detail.id = id
	m.detail.loading = true
	if !refresh {
		m.detail.loaded = false
		m.detail.doc = resource.Document{}
		m.detail.scroll = 0
	}
	m.detail.err = nil
	m.detail.invalidateBody()
	return m.calls.Get(GetCall{
		Instance:   m.instance,
		Browser:    m.id,
		Params:     pluginhost.GetParams{Collection: collection, ID: id},
		Context:    m.context(),
		Refresh:    refresh,
		Generation: m.detail.generation,
	})
}

// currentItem is the row under the cursor.
func (m *Model) currentItem() (pluginhost.Item, bool) {
	s := m.activeState()
	if s == nil || len(s.items) == 0 || s.cursor < 0 || s.cursor >= len(s.items) {
		return pluginhost.Item{}, false
	}
	return s.items[s.cursor], true
}

func (m *Model) clampCursor(s *collectionState) {
	if s.cursor >= len(s.items) {
		s.cursor = len(s.items) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

func (d *detailState) invalidateBody() {
	d.body = nil
	d.bodyForW = -1
}

// Flash is the standing line an act outcome left, and whether it is an error.
// The host puts it in the footer; the browser never renders a footer of its own.
func (m *Model) Flash() (string, bool) { return m.flash, m.flashErr }

// A global protocol tab has no pane to be persisted with, so it remembers its
// own view position: the query it was last asked, the view and sort that were
// chosen, and the filters that were applied. A pane-mode browser persists
// nothing here — its position rides on the tab record the surface writes, and
// two authorities on one position is how they start disagreeing.

// restoreTabView reinstates a remembered view position for the collection on
// screen, exactly once, before anything is listed. A collection the user has
// already touched this session is left alone: what is on screen wins over what
// was on screen last time.
func (m *Model) restoreTabView() {
	if m.paneMode() || m.tabViewRestored {
		return
	}
	c, ok := m.ActiveCollection()
	if !ok {
		return
	}
	m.tabViewRestored = true
	saved := state.GetPluginBrowserView(m.instance, c.ID)
	if saved.Empty() {
		return
	}
	s := m.state(c)
	if s.loaded || s.loading || s.query != "" {
		return
	}
	s.query = saved.Query
	for _, v := range c.Views {
		if v.ID == saved.View {
			s.view = saved.View
			break
		}
	}
	for _, key := range c.Sort {
		if key.ID == saved.Sort {
			s.sortKey, s.sortDir = saved.Sort, defaultDir(c, saved.Sort)
			break
		}
	}
	s.filters = adoptFilters(c, saved.Filters)
}

// persistTabView writes the position a list is about to be run with. It writes
// only when something moved, for the reason persistSplit does: a save per
// keystroke is a file write per keystroke.
func (m *Model) persistTabView(c pluginhost.Collection, s *collectionState) {
	if m.paneMode() {
		return
	}
	view := state.PluginBrowserViewJSON{
		Query: s.query, View: s.view, Sort: s.sortKey,
		Filters: pluginhost.NormalizeFilters(c, s.filters),
	}
	if tabViewEqual(state.GetPluginBrowserView(m.instance, c.ID), view) {
		return
	}
	_ = state.SetPluginBrowserView(m.instance, c.ID, view)
}

func tabViewEqual(a, b state.PluginBrowserViewJSON) bool {
	if a.Query != b.Query || a.View != b.View || a.Sort != b.Sort || len(a.Filters) != len(b.Filters) {
		return false
	}
	for id, value := range a.Filters {
		// Comma-ok, not a lookup: an absent key reads as "" and would compare
		// equal to a filter deliberately cleared to the empty string, which is
		// what a text filter with a default looks like once it is cleared.
		if v, ok := b.Filters[id]; !ok || v != value {
			return false
		}
	}
	return true
}
