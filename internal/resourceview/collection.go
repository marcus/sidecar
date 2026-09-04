package resourceview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/pluginbrowser"
	"github.com/marcus/sidecar/internal/resource"
)

// One Resource leaf, two tab shapes.
//
// The Resource leaf is "the single leaf kind every external plugin's content
// shares". A collection tab and a row's document tab are second and third
// shapes of a tab in that leaf, not new leaf kinds — so panelayout, paneframe,
// both pane_host.go files and the layout floors do not learn anything, and both
// workspace projections inherit the shapes by holding the same Model they
// already hold.
//
// The dispatch is here, on one model, rather than at each surface. A Model whose
// reference is a matched document renders the resource card below; a Model whose
// reference names a collection or one of its rows delegates every question —
// render, size, scroll, keys, refresh — to the shared browser in pane mode.
// Anything answered twice is a place the two shapes can drift apart.

// CallsFor is how a surface hands a Model the seam its plugin is reached
// through. It is a function of the instance because one leaf's tabs may belong
// to different plugins, and a Model must not have to know which.
type CallsFor func(instance string) pluginbrowser.Calls

// OpenRow is what Enter on a collection row does: the host opens the row as a
// second tab in the same leaf. A Model does not own the strip, so it asks.
type OpenRow func(ref resource.Reference) tea.Cmd

// OpenRowMsg is the default ask, and the reason both surfaces behave the same:
// a Model has no leaf, no placement and no deck, so Enter on a row emits this
// and whichever surface is showing the pane runs its OWN open journey with it.
// That journey already focuses an open tab rather than fetching it twice, which
// is what makes the second Enter free.
type OpenRowMsg struct{ Ref resource.Reference }

// IsPlugin reports that this tab is one of the plugin shapes, so the host is
// looking at the shared browser rather than the resource card.
func (m *Model) IsPlugin() bool { return m != nil && m.browser != nil }

// Browser is the pane-mode browser behind a plugin-shaped tab, or nil. Hosts
// use it for the few things only the browser can answer — whether it has the
// keyboard, whether an overlay is open — and never to re-render it.
func (m *Model) Browser() *pluginbrowser.Model {
	if m == nil {
		return nil
	}
	return m.browser
}

// SetCallsFor injects the plugin seam. Like SetResolver it may arrive after the
// tab was armed, because the host's describe pass finishes well after restore.
func (m *Model) SetCallsFor(calls CallsFor) {
	if m == nil {
		return
	}
	m.callsFor = calls
	if m.browser != nil && calls != nil {
		m.browser.SetCalls(calls(m.ref.Instance))
	}
}

// SetOpenRow injects what Enter on a row does.
func (m *Model) SetOpenRow(open OpenRow) {
	if m == nil {
		return
	}
	m.openRow = open
	m.bindOpenRow()
}

func (m *Model) bindOpenRow() {
	if m.browser == nil {
		return
	}
	open := m.openRow
	instance := m.ref.Instance
	if open == nil {
		open = func(ref resource.Reference) tea.Cmd {
			return func() tea.Msg { return OpenRowMsg{Ref: ref} }
		}
	}
	m.browser.SetOnOpenRow(func(collection, id string, filters map[string]string) tea.Cmd {
		return open(resource.Reference{
			Instance: instance, Collection: collection, Locator: id,
			// The row's reference carries the scope it was found under, so the
			// tab that opens — and the tab record that outlives this session —
			// expands it under that scope rather than the plugin's defaults.
			Filters: resource.FilterValues(filters),
		})
	})
}

// bindPlugin builds or rebinds the pane-mode browser for a plugin-shaped
// reference, and tears it down for a matched one. It is called from every place
// that points the model at a reference, so the browser and the reference can
// never disagree about which shape this tab is.
func (m *Model) bindPlugin(ref resource.Reference) {
	if !ref.IsPlugin() {
		m.browser = nil
		return
	}
	var calls pluginbrowser.Calls
	if m.callsFor != nil {
		calls = m.callsFor(ref.Instance)
	}
	// A rebind of the same instance keeps the browser: its remembered page, its
	// cursor and its in-flight generation are what make a re-arm cheap.
	if m.browser == nil {
		m.browser = pluginbrowser.New(ref.Instance, ref.Instance, calls, m.renderer)
	} else {
		m.browser.SetCalls(calls)
	}
	m.browser.SetSize(m.width, m.height)
	switch ref.Shape() {
	case resource.ShapeCollection:
		m.browser.SetPaneCollection(ref.Collection)
		m.browser.RestorePaneView(ref.Query, ref.View, ref.Sort, ref.CursorID, resource.FilterMap(ref.Filters))
	case resource.ShapeItem:
		// Armed, not fetched: a restored row tab must name the row in the strip
		// before anything is asked for, or the tab reads as its collection until
		// the document lands.
		m.browser.ArmPaneDocument(ref.Collection, ref.Locator, resource.FilterMap(ref.Filters))
	}
	m.bindOpenRow()
}

// pluginArm points a plugin-shaped tab at its reference without fetching
// anything, which is what restore does.
func (m *Model) pluginArm(ref resource.Reference) {
	m.bindPlugin(ref)
	m.state = StateArmed
}

// pluginLoad starts the work a plugin-shaped tab needs: a collection tab reads
// the host's cached describe and lists, and a row's document tab fetches the
// document.
func (m *Model) pluginLoad() tea.Cmd {
	if m.browser == nil {
		return nil
	}
	m.state = StateReady
	if m.ref.Shape() == resource.ShapeItem {
		// Selecting a row tab that is already showing its document costs
		// nothing. That is what makes the second Enter on a row free: the first
		// opened and fetched, the second only focuses.
		if m.browser.PaneDocumentShowing(m.ref.Locator) {
			return m.browser.Refresh()
		}
		return batch(m.browser.Refresh(),
			m.browser.SetPaneDocument(m.ref.Collection, m.ref.Locator, resource.FilterMap(m.ref.Filters)))
	}
	return m.browser.Refresh()
}

// FocusRef points an already-open tab at a reference that names the SAME tab
// with a different view position, and starts whatever that needs.
//
// It is what makes `sidecar open --plugin recall --collection results --query
// dex` mean the same thing whether or not that collection is already open: the
// tab's identity is deliberately just {instance, collection}, so a second open
// focuses it, and without this the query the user asked for would be dropped on
// the floor by the focus.
func (m *Model) FocusRef(ref resource.Reference) tea.Cmd {
	if m == nil {
		return nil
	}
	if !ref.IsPlugin() || m.browser == nil {
		return m.Resolve()
	}
	if !m.viewPositionDiffers(ref) {
		return m.Resolve()
	}
	m.ref = ref
	m.bindPlugin(ref)
	return m.pluginLoad()
}

// viewPositionDiffers reports that a reference asks for something the browser is
// not showing. An empty field asks for nothing: `open` with no --query focuses
// the tab as it is rather than clearing a query the user typed.
func (m *Model) viewPositionDiffers(ref resource.Reference) bool {
	if ref.Shape() != resource.ShapeCollection {
		return false
	}
	if ref.Query != "" && ref.Query != m.browser.PaneQuery() {
		return true
	}
	if ref.View != "" && ref.View != m.browser.PaneView() {
		return true
	}
	if ref.Sort != "" && ref.Sort != m.browser.PaneSort() {
		return true
	}
	// An `open` naming no filters asks for nothing, exactly as one naming no
	// query does: it focuses the tab as it is rather than clearing what the
	// user chose.
	if len(ref.Filters) == 0 {
		return false
	}
	live := m.browser.PaneFilters()
	for _, f := range ref.Filters {
		if live[f.ID] != f.Value {
			return true
		}
	}
	return false
}

// pluginSnapshot folds the browser's live view position back into the
// reference, so what is persisted is what the user can see.
func (m *Model) pluginSnapshot() resource.Reference {
	ref := m.ref
	if m.browser == nil || ref.Shape() != resource.ShapeCollection {
		return ref
	}
	// Before describe has landed the browser has no state for this collection,
	// and reading a position out of it would report an empty query for a tab
	// that has one waiting to be applied. The reference stays authoritative
	// until the browser has actually adopted it.
	if !m.browser.PaneViewApplied() {
		return ref
	}
	ref.Query = m.browser.PaneQuery()
	ref.View = m.browser.PaneView()
	ref.Sort = m.browser.PaneSort()
	ref.CursorID = m.browser.PaneCursorID()
	ref.Filters = resource.FilterValues(m.browser.PaneFilters())
	return ref
}

// ApplyPluginMsg routes one of the browser's own messages to a plugin-shaped
// tab, reporting whether it belonged here. The browser discards a message for
// another instance or a superseded generation itself, which is what makes
// broadcasting them safe.
func (m *Model) ApplyPluginMsg(msg tea.Msg) (tea.Cmd, bool) {
	if m == nil || m.browser == nil || !pluginbrowser.IsBrowserMsg(msg) {
		return nil, false
	}
	cmd := m.browser.Update(msg)
	// A row's document may come back under a canonical identity. Re-keying here
	// is the same act the resource card's resolve already performs, so the tab
	// strip and the persisted reference name the resource the plugin does.
	if m.ref.Shape() == resource.ShapeItem {
		if identity := m.browser.PaneDocumentIdentity(); identity != "" {
			m.ref.Locator = identity
		}
	}
	return cmd, true
}

// HandlePluginKey offers a key to a plugin-shaped tab, reporting whether the
// browser consumed it.
func (m *Model) HandlePluginKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m == nil || m.browser == nil {
		return nil, false
	}
	return m.browser.HandleKey(msg)
}

// ClaimsKey reports whether a plugin-shaped tab would act on a key. A matched
// document claims nothing here: its keys are the Pane's.
func (m *Model) ClaimsKey(key string) bool {
	return m != nil && m.browser != nil && m.browser.ClaimsKey(key)
}

// ConsumesTextInput reports that a plugin tab's query line has the keyboard.
func (m *Model) ConsumesTextInput() bool {
	return m != nil && m.browser != nil && m.browser.ConsumesTextInput()
}

// BlocksGlobalKeys reports that a plugin tab has an overlay open.
func (m *Model) BlocksGlobalKeys() bool {
	return m != nil && m.browser != nil && m.browser.BlocksGlobalKeys()
}

func batch(cmds ...tea.Cmd) tea.Cmd {
	var out []tea.Cmd
	for _, cmd := range cmds {
		if cmd != nil {
			out = append(out, cmd)
		}
	}
	switch len(out) {
	case 0:
		return nil
	case 1:
		return out[0]
	default:
		return tea.Batch(out...)
	}
}

// HandlePluginKey offers a key to the active tab when it is plugin-shaped. It
// takes the key as a string because that is the vocabulary Pane.HandleKey is
// written in and the surfaces call it with.
func (t *Tabs) HandlePluginKey(key string) (tea.Cmd, bool) {
	m := t.Active()
	if m == nil || m.browser == nil {
		return nil, false
	}
	return m.browser.HandleKeyString(key)
}

// ClaimsKey reports whether the active tab would act on a key. It is what a
// host's key ladder asks before running its own shortcut.
func (t *Tabs) ClaimsKey(key string) bool {
	m := t.Active()
	return m != nil && m.ClaimsKey(key)
}

// ConsumesTextInput reports that the active tab's query line has the keyboard.
func (t *Tabs) ConsumesTextInput() bool {
	m := t.Active()
	return m != nil && m.ConsumesTextInput()
}

// BlocksGlobalKeys reports that the active tab has an overlay owning the
// keyboard — the View control or an action form. A host asks it at precedence
// level 2, beside its own modal question, so those keys reach the overlay
// rather than the host's global switch.
func (t *Tabs) BlocksGlobalKeys() bool {
	m := t.Active()
	return m != nil && m.BlocksGlobalKeys()
}

// PluginSurface is what the app injects the protocol-plugin seam into. It is
// declared here rather than in the app, and asserted by each surface, for the
// reason Surface gives: an assertion that quietly fails is a collection pane
// that never lists anything, with nothing to debug.
type PluginSurface interface {
	// SetPluginCalls injects how a collection or row tab reaches its plugin,
	// and returns the work that injection starts. A tab restored before any
	// describe pass had run is armed and waiting for exactly this.
	SetPluginCalls(CallsFor) tea.Cmd
}
