package resourceview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/tabs"
)

// MaxTabs bounds one Resource leaf. Clicking keys forever must cost bounded
// memory and a bounded tab strip; past the bound the oldest unfocused tab is
// dropped rather than refusing the user's click.
const MaxTabs = 16

// TabKey is the stable identity of one tab before anything is fetched. The same
// locator from two plugins is two resources, so the instance is always part of
// the key, and the shape is part of it too so a collection and a row of that
// collection can never collide.
//
// A collection tab's identity is deliberately {instance, collection} and
// nothing more: the query, view, sort and cursor are view position, so retyping
// a query re-lists the tab that is already open rather than forking a second.
func TabKey(ref resource.Reference) string {
	switch ref.Shape() {
	case resource.ShapeCollection:
		return ref.Instance + "\x00c\x00" + ref.Collection
	case resource.ShapeItem:
		return ref.Instance + "\x00i\x00" + ref.Collection + "\x00" + ref.Locator
	default:
		return ref.Instance + "\x00" + ref.Matcher + "\x00" + ref.Locator
	}
}

// Tabs is the tabbed set of resource references in one Resource leaf.
//
// Ordering, cycling, close semantics and the overflow window come from
// tabs.Group, the same generic the file and issue strips use. What this type
// adds is only what is specific to resources: keys built from references,
// arming without resolving, routing an answer to the tab that asked, and the
// canonical re-key that merges two tabs naming one resource.
type Tabs struct {
	tabs.Group[*Model]

	renderer *markdown.Renderer
	resolve  Resolver
	// callsFor and openRow are the plugin-shaped tabs' half of the seam. They
	// arrive after construction for the same reason resolve does: the host's
	// describe pass finishes long after a restored tab is armed.
	callsFor CallsFor
	openRow  OpenRow

	// nextModelID hands each model a distinct identity so a late answer can be
	// matched to the tab that asked even after tabs close and indices shift.
	nextModelID int

	width, height int
	epoch         uint64
}

// NewTabs creates an empty tab set.
func NewTabs(renderer *markdown.Renderer, resolve Resolver) *Tabs {
	if renderer == nil {
		renderer, _ = markdown.NewRenderer()
	}
	return &Tabs{renderer: renderer, resolve: resolve}
}

// SetResolver replaces the resolver for this set and every existing tab.
// Provider setup is asynchronous, so resolver injection must not depend on
// whether a restored or clicked tab happened to be constructed first.
func (t *Tabs) SetResolver(resolve Resolver) {
	if t == nil {
		return
	}
	t.resolve = resolve
	for _, item := range t.Items {
		item.Value.SetResolver(resolve)
	}
}

// SetCallsFor injects how a plugin-shaped tab reaches its plugin, for this set
// and every tab already in it.
func (t *Tabs) SetCallsFor(calls CallsFor) {
	if t == nil {
		return
	}
	t.callsFor = calls
	for _, item := range t.Items {
		item.Value.SetCallsFor(calls)
	}
}

// SetOpenRow injects what Enter on a collection row does. The default is this
// set's own: open the row as a second tab in the same leaf, which is the
// behaviour both surfaces owe and neither should re-derive.
func (t *Tabs) SetOpenRow(open OpenRow) {
	if t == nil {
		return
	}
	t.openRow = open
	for _, item := range t.Items {
		item.Value.SetOpenRow(open)
	}
}

// UpdatePlugins routes one of the shared browser's messages to every
// plugin-shaped tab, reporting whether any of them owned it. Each browser
// discards a message for another instance or a superseded generation itself.
func (t *Tabs) UpdatePlugins(msg tea.Msg) (tea.Cmd, bool) {
	if t == nil {
		return nil, false
	}
	var cmds []tea.Cmd
	handled := false
	for i := range t.Items {
		cmd, ok := t.Items[i].Value.ApplyPluginMsg(msg)
		handled = handled || ok
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if ok {
			// A row's document may come back under a canonical identity, so the
			// strip is re-keyed and any tab that now names the same row merges
			// into it — the same rule a resolve already holds for the card.
			t.rekeyAndMerge(i, t.Items[i].Value.Reference().Locator)
		}
	}
	if len(cmds) == 0 {
		return nil, handled
	}
	return tea.Batch(cmds...), handled
}

// Len reports how many tabs are open.
func (t *Tabs) Len() int { return len(t.Items) }

// Empty reports whether the leaf has nothing left in it.
func (t *Tabs) Empty() bool { return len(t.Items) == 0 }

// Active returns the focused tab, or nil.
func (t *Tabs) Active() *Model {
	item, ok := t.ActiveItem()
	if !ok {
		return nil
	}
	return item.Value
}

// ActiveIndex is the focused tab's position, which the host persists.
func (t *Tabs) ActiveIndex() int { return t.Group.Active }

// At returns the tab at index, or nil.
func (t *Tabs) At(i int) *Model {
	if i < 0 || i >= len(t.Items) {
		return nil
	}
	return t.Items[i].Value
}

// All returns the models in strip order.
func (t *Tabs) All() []*Model {
	out := make([]*Model, 0, len(t.Items))
	for _, item := range t.Items {
		out = append(out, item.Value)
	}
	return out
}

// Labels is the tab strip's text, in order.
func (t *Tabs) Labels() []string {
	out := make([]string, 0, len(t.Items))
	for _, item := range t.Items {
		out = append(out, item.Value.TabLabel())
	}
	return out
}

// SetEpoch records the host's scoping value for subsequent loads.
func (t *Tabs) SetEpoch(epoch uint64) { t.epoch = epoch }

// SetSize sizes every tab, because a tab selected later must already know the
// box it will be drawn into.
func (t *Tabs) SetSize(width, height int) {
	t.width, t.height = width, height
	for _, item := range t.Items {
		item.Value.SetSize(width, height)
	}
}

// Open focuses an existing tab for ref, or appends a new one and resolves it.
// It is the single entry point for a terminal click, a restored tab being
// selected, and `sidecar open --provider`, so all three behave identically.
func (t *Tabs) Open(ref resource.Reference) tea.Cmd {
	if i := t.Find(TabKey(ref)); i >= 0 {
		t.Select(i)
		// A tab that was armed rather than resolved starts now.
		return t.Items[i].Value.Resolve()
	}
	t.evictIfFull()
	m := t.newModel()
	t.Append(TabKey(ref), m)
	return m.Load(t.nextModelID, ref, t.epoch)
}

// Arm appends a tab without resolving it. Restore uses this so relaunch does
// not fan out one process per remembered tab.
func (t *Tabs) Arm(ref resource.Reference, scroll int) *Model {
	if i := t.Find(TabKey(ref)); i >= 0 {
		return t.Items[i].Value
	}
	t.evictIfFull()
	m := t.newModel()
	m.Arm(t.nextModelID, ref, t.epoch)
	m.SetPendingScroll(scroll)
	t.Append(TabKey(ref), m)
	return m
}

func (t *Tabs) newModel() *Model {
	m := New(t.renderer, t.resolve)
	m.SetCallsFor(t.callsFor)
	m.SetOpenRow(t.openRow)
	m.SetSize(t.width, t.height)
	t.nextModelID++
	return m
}

// SetActive focuses a tab by index and resolves it if it is still armed.
// Selecting is what turns a restored reference into a request, which is the
// behavior both hosts need and neither should re-derive.
func (t *Tabs) SetActive(i int) tea.Cmd {
	if i < 0 || i >= len(t.Items) {
		return nil
	}
	t.Select(i)
	return t.Items[i].Value.Resolve()
}

// Cycle moves by delta and resolves the tab that lands, wrapping.
func (t *Tabs) Cycle(delta int) tea.Cmd {
	if len(t.Items) < 2 {
		return nil
	}
	t.Group.Cycle(delta)
	return t.ResolveActive()
}

// Next and Prev are the documented { and } bindings.
func (t *Tabs) Next() tea.Cmd { return t.Cycle(1) }
func (t *Tabs) Prev() tea.Cmd { return t.Cycle(-1) }

// ResolveActive starts the active tab's resolve if it is still armed.
func (t *Tabs) ResolveActive() tea.Cmd {
	if m := t.Active(); m != nil {
		return m.Resolve()
	}
	return nil
}

// RefreshActive re-resolves the active tab, bypassing freshness.
func (t *Tabs) RefreshActive() tea.Cmd {
	if m := t.Active(); m != nil {
		return m.Refresh()
	}
	return nil
}

// ReArmPending returns every tab waiting on an answer the host has decided to
// discard back to a resolvable state, and reports how many changed. A host
// calls this when it drops results wholesale — a workspace row switch, a
// project change — so no tab is left on a loading card forever.
func (t *Tabs) ReArmPending() int {
	n := 0
	for _, item := range t.Items {
		if item.Value.ReArm() {
			n++
		}
	}
	return n
}

// Close removes the tab at index and reports whether the leaf is now empty.
// Closing is the user's explicit act, and with a confirmed cleanup it is the
// only thing that may drop a reference.
func (t *Tabs) Close(i int) (empty bool) {
	return t.CloseAt(i).Empty
}

// CloseActive removes the focused tab.
func (t *Tabs) CloseActive() (empty bool) {
	return t.Group.CloseActive().Empty
}

// Apply routes a resolve result to the tab that asked for it, applies any
// canonical re-key, and merges a tab that has just become a duplicate.
//
// It returns false when no open tab owns the message, which is the correct
// outcome for a result arriving after its tab was closed.
func (t *Tabs) Apply(msg ResolvedMsg) bool {
	for i, item := range t.Items {
		if !item.Value.Accepts(msg) {
			continue
		}
		if !item.Value.Apply(msg) {
			return false
		}
		if msg.Err == nil {
			t.rekeyAndMerge(i, msg.Document.Identity)
		}
		return true
	}
	return false
}

// rekeyAndMerge adopts the provider's canonical identity for the tab at i. If
// another tab already holds that identity the two are merged: the resolved one
// wins, so clicking a key and then its canonical form leaves one tab.
func (t *Tabs) rekeyAndMerge(i int, identity string) {
	if identity == "" || i < 0 || i >= len(t.Items) {
		return
	}
	m := t.Items[i].Value
	before := m.Reference()
	m.Rekey(identity)
	after := m.Reference()
	if after.Equal(before) {
		return
	}
	key := TabKey(after)
	t.Items[i].Key = key
	// Drop any other tab that now names the same resource. The resolved tab
	// is the one with content, so it is the one that survives.
	survivor := t.Items[i].Value
	t.CloseMatching(func(item tabs.Item[*Model]) bool {
		return item.Key == key && item.Value != survivor
	})
	if j := t.Find(key); j >= 0 {
		t.Select(j)
	}
}

// evictIfFull drops the oldest tab that is not focused once the bound is hit.
func (t *Tabs) evictIfFull() {
	for len(t.Items) >= MaxTabs {
		victim := 0
		if victim == t.Group.Active && len(t.Items) > 1 {
			victim = 1
		}
		t.CloseAt(victim)
	}
}

// References is the reference-only projection the project surface persists:
// no title, field, body, error, URL, or auth state, by construction rather
// than by remembering to strip them.
func (t *Tabs) References() []PersistedTab {
	out := make([]PersistedTab, 0, len(t.Items))
	for _, item := range t.Items {
		ref := item.Value.Reference()
		out = append(out, PersistedTab{
			Provider:   ref.Instance,
			Matcher:    ref.Matcher,
			Locator:    ref.Locator,
			Collection: ref.Collection,
			Query:      ref.Query,
			View:       ref.View,
			Sort:       ref.Sort,
			CursorID:   ref.CursorID,
			Filters:    resource.FilterMap(ref.Filters),
			Scroll:     item.Value.Scroll(),
		})
	}
	return out
}

// PersistedTab is one reference plus its scroll. It mirrors the state
// package's JSON shape without this package depending on state, so the view
// layer stays free of persistence concerns. Collection and the view position
// beside it are set only on a plugin-shaped tab.
type PersistedTab struct {
	Provider   string
	Matcher    string
	Locator    string
	Collection string
	Query      string
	View       string
	Sort       string
	CursorID   string
	Filters    map[string]string
	Scroll     int
}

// equal compares two persisted tabs. It is spelled out because a tab carries an
// applied filter map, which makes the struct uncomparable with ==; every field
// still takes part, so a save that changed only a filter is still a save.
func (t PersistedTab) Equal(other PersistedTab) bool {
	if t.Provider != other.Provider || t.Matcher != other.Matcher || t.Locator != other.Locator ||
		t.Collection != other.Collection || t.Query != other.Query || t.View != other.View ||
		t.Sort != other.Sort || t.CursorID != other.CursorID || t.Scroll != other.Scroll ||
		len(t.Filters) != len(other.Filters) {
		return false
	}
	for id, value := range t.Filters {
		// Comma-ok, not a lookup: an absent key reads as "" and would compare
		// equal to a filter deliberately cleared to the empty string.
		if v, ok := other.Filters[id]; !ok || v != value {
			return false
		}
	}
	return true
}

// View renders the active tab.
func (t *Tabs) View() string {
	if m := t.Active(); m != nil {
		return m.View()
	}
	return ""
}
