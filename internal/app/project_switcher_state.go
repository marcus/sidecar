package app

import (
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/projectlist"
	"github.com/marcus/sidecar/internal/state"
)

// The project switcher's state: which control has focus, what order the
// collection is in, and how a cursor moves through it. The rules themselves
// live in internal/projectlist so a headless caller gets the same answers; what
// is here is the binding between those rules and this modal.

// switcherFocus names the control the keyboard is talking to. The filter is
// first and is where focus returns on any printable key: a collection modal
// whose letters sometimes filter and sometimes trigger a shortcut is a modal
// you cannot type a project name into.
type switcherFocus uint8

const (
	switcherFocusFilter switcherFocus = iota
	switcherFocusSort
	switcherFocusView
	switcherFocusAdd
)

// switcherFocusOrder is what tab walks.
var switcherFocusOrder = []switcherFocus{
	switcherFocusFilter, switcherFocusSort, switcherFocusView, switcherFocusAdd,
}

// cycleProjectSwitcherFocus moves focus by delta through the control order.
func (m *Model) cycleProjectSwitcherFocus(delta int) {
	at := 0
	for i, focus := range switcherFocusOrder {
		if focus == m.projectSwitcherFocus {
			at = i
			break
		}
	}
	next := (at + delta + len(switcherFocusOrder)) % len(switcherFocusOrder)
	m.projectSwitcherFocus = switcherFocusOrder[next]
	m.projectSwitcherSortOpen = false
	m.clearProjectSwitcherModal()
}

// projectSwitcherNow is the clock the metadata column reads. It is one call per
// render rather than one per row, so every row in a frame agrees about "now".
func (m *Model) projectSwitcherNow() time.Time { return time.Now() }

// restoreProjectSwitcherPreferences reads the remembered sort, direction and
// view. An unrecognised value — a state file from a build that offered
// something else — falls back to the default rather than to an arbitrary mode.
func (m *Model) restoreProjectSwitcherPreferences() {
	m.projectSwitcherSort = projectlist.SortActivity
	if mode, ok := projectlist.SortFromLabel(state.GetProjectSwitcherSort(), projectlist.SortModes); ok {
		m.projectSwitcherSort = mode
	}
	m.projectSwitcherOrder = m.projectSwitcherSort.DefaultOrder()
	if order, ok := projectlist.OrderFromLabel(state.GetProjectSwitcherOrder()); ok {
		m.projectSwitcherOrder = order
	}
	m.projectSwitcherView = projectlist.ViewList
	if view, ok := projectlist.ViewFromLabel(state.GetProjectSwitcherView()); ok {
		m.projectSwitcherView = view
	}
}

// setProjectSwitcherCollection rebuilds the collection for a query and puts it
// in the chosen order. The destinations and the presentation items stay
// index-aligned: activation reads the destination, drawing reads the item, and
// neither has to re-derive the other's order.
func (m *Model) setProjectSwitcherCollection(query string) {
	raw := m.projectSwitcherDestinations(query)
	activity := m.projectActivityTimes()

	items := make([]projectlist.Item, len(raw))
	for i, destination := range raw {
		items[i] = m.projectSwitcherItemFor(destination, activity)
		items[i].Data = i
	}
	ordered := projectlist.Sorted(items, m.projectSwitcherSort, m.projectSwitcherOrder)

	destinations := make([]projectSwitcherDestination, len(ordered))
	for i, item := range ordered {
		destinations[i] = raw[item.Data.(int)]
		ordered[i].Data = nil
	}
	m.projectSwitcherFiltered = destinations
	m.projectSwitcherRows = ordered
}

// projectSwitcherItemFor resolves one destination into its presentation form.
//
// Dates are attached only for a local configured project. A remote project's
// metadata belongs to the host that owns it; reading a local record for a path
// that happens to exist here would be answering about the wrong machine, so a
// remote destination stays honestly unknown until its host reports.
func (m *Model) projectSwitcherItemFor(d projectSwitcherDestination, activity map[string]time.Time) projectlist.Item {
	item := projectlist.Item{
		ID:             d.identityKey(),
		Kind:           projectlist.KindProject,
		Name:           d.Name,
		Path:           d.Path,
		Host:           d.Destination.HostID,
		Current:        m.isCurrentSwitcherDestination(d),
		DisabledReason: d.DisabledReason,
	}
	if d.Kind == destinationOverview {
		item.Kind = projectlist.KindOverview
		return item
	}
	if d.isRemote() {
		item.Path = d.Destination.Root
		return item
	}
	if d.Project != nil && d.Project.AddedAt != nil {
		item.AddedAt = *d.Project.AddedAt
	}
	if when, ok := activity[filepath.Clean(config.ExpandPath(d.Path))]; ok {
		item.LastActiveAt = when
	}
	return item
}

// moveProjectSwitcherCursor moves the selection. In the list only the vertical
// axis moves; in the grid the arrows are spatial, which is what makes a grid
// navigable rather than a list drawn in boxes.
func (m *Model) moveProjectSwitcherCursor(dx, dy int) tea.Cmd {
	n := len(m.projectSwitcherFiltered)
	if n == 0 {
		return nil
	}
	before := m.projectSwitcherCursor
	if m.projectSwitcherEffectiveView() == projectlist.ViewGrid {
		columns := projectlist.GridColumns(m.projectSwitcherCollectionWidth())
		m.projectSwitcherCursor = projectlist.GridMove(before, n, columns, dx, dy)
	} else if dy != 0 {
		next := before + dy
		if next >= 0 && next < n {
			m.projectSwitcherCursor = next
		}
	}
	if m.projectSwitcherCursor == before {
		return nil
	}
	m.projectSwitcherEnsureCursorVisible()
	m.clearProjectSwitcherModal()
	return m.previewProjectTheme()
}

// projectSwitcherEnsureCursorVisible brings the viewport back to the cursor. In
// the grid the window moves a whole row at a time, so a card never lands
// half-drawn against the top of the collection.
func (m *Model) projectSwitcherEnsureCursorVisible() {
	visible := m.projectSwitcherVisibleRows()
	pinned := m.projectSwitcherPinnedCount()
	if m.projectSwitcherCursor < pinned {
		// A pinned row is always on screen, so landing on one is not a reason
		// to move the collection underneath it.
		return
	}
	scroll := projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor-pinned, m.projectSwitcherScroll, visible)
	if m.projectSwitcherEffectiveView() == projectlist.ViewGrid {
		if columns := projectlist.GridColumns(m.projectSwitcherCollectionWidth()); columns > 0 {
			scroll -= scroll % columns
		}
	}
	if scroll < 0 {
		scroll = 0
	}
	m.projectSwitcherScroll = scroll
}

// selectedProjectSwitcherID is the identity the selection rides. A reorder, a
// filter or a change of view moves rows around; the cursor follows the project
// the user chose rather than the position it used to be at.
func (m *Model) selectedProjectSwitcherID() string {
	if m.projectSwitcherCursor < 0 || m.projectSwitcherCursor >= len(m.projectSwitcherRows) {
		return ""
	}
	return m.projectSwitcherRows[m.projectSwitcherCursor].ID
}

// restoreProjectSwitcherSelection puts the cursor back on an identity after the
// collection was rebuilt, falling back to the first row when it is gone.
func (m *Model) restoreProjectSwitcherSelection(id string) {
	if index := projectlist.IndexOf(m.projectSwitcherRows, id); index >= 0 {
		m.projectSwitcherCursor = index
	} else {
		m.projectSwitcherCursor = 0
	}
	m.projectSwitcherEnsureCursorVisible()
}

// applyProjectSwitcherSort changes the order and keeps the cursor on the same
// destination. Choosing a mode adopts that mode's own default direction, so
// picking "Name" does not inherit "newest first" from the mode before it.
func (m *Model) applyProjectSwitcherSort(mode projectlist.Sort) tea.Cmd {
	if m.projectSwitcherSort == mode {
		return nil
	}
	m.projectSwitcherSort = mode
	m.projectSwitcherOrder = mode.DefaultOrder()
	m.projectSwitcherSortIdx = projectlist.SortIndex(mode, projectlist.SortModes)
	return m.reorderProjectSwitcher()
}

// toggleProjectSwitcherOrder flips the direction within the current mode.
func (m *Model) toggleProjectSwitcherOrder() tea.Cmd {
	m.projectSwitcherOrder = m.projectSwitcherOrder.Toggle()
	return m.reorderProjectSwitcher()
}

// reorderProjectSwitcher re-sorts, keeps the selection, and persists the
// choice. The theme preview is refreshed because the selected destination can
// be a different project after a reorder only if its identity moved — it does
// not — but the preview command is the one that keeps preview and selection in
// step, and running it here costs nothing when they already agree.
func (m *Model) reorderProjectSwitcher() tea.Cmd {
	selected := m.selectedProjectSwitcherID()
	m.setProjectSwitcherCollection(m.projectSwitcherInput.Value())
	m.restoreProjectSwitcherSelection(selected)
	m.clearProjectSwitcherModal()
	sortLabel := m.projectSwitcherSort.Label()
	orderLabel := projectlist.OrderLabel(m.projectSwitcherSort, m.projectSwitcherOrder)
	return tea.Batch(
		func() tea.Msg {
			_ = state.SetProjectSwitcherSort(sortLabel, orderLabel)
			return nil
		},
		m.previewProjectTheme(),
	)
}

// setProjectSwitcherView changes the layout. The collection, the filter and the
// selection are untouched: this is a second way to look at one ordered
// collection, not a second collection.
func (m *Model) setProjectSwitcherView(view projectlist.View) tea.Cmd {
	if m.projectSwitcherView == view {
		return nil
	}
	m.projectSwitcherView = view
	m.projectSwitcherEnsureCursorVisible()
	m.clearProjectSwitcherModal()
	label := view.Label()
	return func() tea.Msg {
		_ = state.SetProjectSwitcherView(label)
		return nil
	}
}

// openProjectSwitcherSort opens the sort menu with the cursor on the mode in
// force, so the menu opens saying where the user already is.
func (m *Model) openProjectSwitcherSort() {
	m.projectSwitcherSortIdx = projectlist.SortIndex(m.projectSwitcherSort, projectlist.SortModes)
	m.projectSwitcherSortOpen = true
	m.projectSwitcherFocus = switcherFocusSort
	m.clearProjectSwitcherModal()
}

// closeProjectSwitcherSort puts the menu away without changing anything.
func (m *Model) closeProjectSwitcherSort() {
	m.projectSwitcherSortOpen = false
	m.clearProjectSwitcherModal()
}

// projectSwitcherSortRows is the number of selectable lines in the sort menu:
// the modes plus the direction line under them.
func projectSwitcherSortRows() int { return len(projectlist.SortModes) + 1 }

// handleProjectSwitcherSortKey answers keys while the sort menu is open. It is
// separate from the collection's handler because an open menu owns the arrows:
// moving the list underneath a menu the user is reading is how a selection ends
// up somewhere nobody chose.
func (m *Model) handleProjectSwitcherSortKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if !m.projectSwitcherSortOpen {
		return false, nil
	}
	switch msg.String() {
	case "esc", "escape":
		// Escape dismisses the menu before it does anything to the filter.
		m.closeProjectSwitcherSort()
		return true, nil
	case "up", "ctrl+p":
		if m.projectSwitcherSortIdx > 0 {
			m.projectSwitcherSortIdx--
			m.clearProjectSwitcherModal()
		}
		return true, nil
	case "down", "ctrl+n":
		if m.projectSwitcherSortIdx < projectSwitcherSortRows()-1 {
			m.projectSwitcherSortIdx++
			m.clearProjectSwitcherModal()
		}
		return true, nil
	case "left", "right":
		// The direction line is a toggle wherever the menu cursor is: it is
		// the only horizontal choice the menu offers.
		return true, m.toggleProjectSwitcherOrder()
	case "enter", " ", "space":
		if m.projectSwitcherSortIdx >= len(projectlist.SortModes) {
			return true, m.toggleProjectSwitcherOrder()
		}
		cmd := m.applyProjectSwitcherSort(projectlist.SortModes[m.projectSwitcherSortIdx])
		m.closeProjectSwitcherSort()
		return true, cmd
	case "tab", "shift+tab", "backtab":
		m.closeProjectSwitcherSort()
		return true, nil
	}
	return true, nil
}

// handleProjectSwitcherControlKey answers keys aimed at a focused toolbar
// control. It returns false when the key was not the control's to take, which
// is every printable key: those belong to the filter.
func (m *Model) handleProjectSwitcherControlKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch m.projectSwitcherFocus {
	case switcherFocusSort:
		switch msg.String() {
		case "enter", " ", "space":
			m.openProjectSwitcherSort()
			return true, nil
		case "left", "right":
			return true, m.toggleProjectSwitcherOrder()
		}
	case switcherFocusView:
		switch msg.String() {
		case "enter", " ", "space", "left", "right":
			next := projectlist.ViewGrid
			if m.projectSwitcherView == projectlist.ViewGrid {
				next = projectlist.ViewList
			}
			return true, m.setProjectSwitcherView(next)
		}
	case switcherFocusAdd:
		switch msg.String() {
		case "enter", " ", "space":
			m.projectSwitcherFocus = switcherFocusFilter
			m.initProjectAdd()
			return true, nil
		}
	}
	return false, nil
}

// applyProjectSwitcherToolbarAction answers a pointer press on a toolbar
// control. Mouse and keyboard reach the same functions, so the two cannot drift.
func (m *Model) applyProjectSwitcherToolbarAction(action string) (bool, tea.Cmd) {
	switch action {
	case projectSwitcherSortButtonID:
		if m.projectSwitcherSortOpen {
			m.closeProjectSwitcherSort()
		} else {
			m.openProjectSwitcherSort()
		}
		return true, nil
	case projectSwitcherViewListID:
		m.projectSwitcherFocus = switcherFocusView
		return true, m.setProjectSwitcherView(projectlist.ViewList)
	case projectSwitcherViewGridID:
		m.projectSwitcherFocus = switcherFocusView
		return true, m.setProjectSwitcherView(projectlist.ViewGrid)
	case projectSwitcherSortOrderID:
		return true, m.toggleProjectSwitcherOrder()
	case projectSwitcherAddButtonID:
		m.projectSwitcherFocus = switcherFocusFilter
		m.initProjectAdd()
		return true, nil
	}
	if index, ok := projectSwitcherSortOptionIndex(action); ok {
		cmd := m.applyProjectSwitcherSort(projectlist.SortModes[index])
		m.closeProjectSwitcherSort()
		return true, cmd
	}
	return false, nil
}

// projectSwitcherSortOptionIndex resolves a sort-menu region ID back to its
// mode's position, refusing anything outside the offered set.
func projectSwitcherSortOptionIndex(action string) (int, bool) {
	for i := range projectlist.SortModes {
		if action == projectSwitcherSortOptionIDFor(i) {
			return i, true
		}
	}
	return 0, false
}

func projectSwitcherSortOptionIDFor(i int) string {
	return projectSwitcherSortOptionID + itoa(i)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// wheelProjectSwitcherCursor moves the selection by one entry per wheel notch.
// The wheel walks the collection in its drawn order — in the grid that is the
// next card, not the next row — so the same gesture means the same thing in
// both views.
func (m *Model) wheelProjectSwitcherCursor(delta int) tea.Cmd {
	n := len(m.projectSwitcherFiltered)
	if n == 0 {
		return nil
	}
	next := m.projectSwitcherCursor + delta
	if next < 0 {
		next = 0
	}
	if next >= n {
		next = n - 1
	}
	if next == m.projectSwitcherCursor {
		return nil
	}
	m.projectSwitcherCursor = next
	m.projectSwitcherEnsureCursorVisible()
	m.clearProjectSwitcherModal()
	return m.previewProjectTheme()
}

// projectSwitcherPinnedCount is how many leading rows sit outside the scroll
// window. Sorted keeps Overview first in every mode, so this is 0 or 1 — but it
// is counted rather than assumed, because a pinned row that is actually
// scrollable is an off-by-one in every piece of geometry below it.
func (m *Model) projectSwitcherPinnedCount() int {
	if m.projectSwitcherEffectiveView() == projectlist.ViewGrid {
		// The grid has no pinned row: a card is the same shape whatever the
		// destination is, so Overview is simply the first card.
		return 0
	}
	count := 0
	for _, item := range m.projectSwitcherRows {
		if item.Kind != projectlist.KindOverview {
			break
		}
		count++
	}
	return count
}
