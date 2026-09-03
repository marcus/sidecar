package pluginbrowser

import (
	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/textselect"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/workspacelist"
)

// The browser's pointer model.
//
// There is one hit map, cleared at the top of every View and rebuilt in paint
// order, and every click resolves through it to the same method a key would
// call. That is the whole rule: a click can never do something a key cannot,
// and nothing the browser can do is reachable only by pointer.
//
// Regions are registered list box, detail box, rows, query row, View pill,
// outcome, notices, both scrollbars, and the rail last, because HitMap.Test
// scans in reverse and the smallest, most specific target has to win.

const (
	regionList    = "plugin-list"
	regionDetail  = "plugin-detail"
	regionRow     = "plugin-row"
	regionQuery   = "plugin-query"
	regionPill    = "plugin-view-pill"
	regionOutcome = "plugin-outcome"
	regionClear   = "plugin-query-clear"
	regionNotice  = "plugin-notice"
	regionRail    = "plugin-rail"
	// regionDetailSelect names a text-selection drag that began in the detail
	// box. It is a gesture source rather than a hit target: the press is
	// answered by regionDetail, and this is what routes every motion and the
	// release back to the selection the press armed, wherever the pointer has
	// since travelled.
	regionDetailSelect = "plugin-detail-select"
)

// barTarget names which box a scrollbar region belongs to. Both bars register
// under the shared ui.RegionScrollbar* IDs — the same strings every other
// surface uses — so the box is carried as the region's data rather than by
// inventing a second pair of IDs.
type barTarget int

const (
	barList barTarget = iota
	barDetail
)

// rowRect is one visible row's box and the item index it stands for.
type rowRect struct {
	index int
	rect  mouse.Rect
}

// barGeom is a rendered scrollbar's placement, in the coordinates of the box
// that drew it.
type barGeom struct {
	has    bool
	track  mouse.Rect
	thumb  mouse.Rect
	params ui.ScrollbarParams
}

// frameGeom is where this frame's targets ended up, in coordinates local to
// the inner content of the box that drew them. View translates it once, so no
// renderer has to know where its box was placed.
type frameGeom struct {
	pill    mouse.Rect
	query   mouse.Rect
	outcome mouse.Rect
	clear   mouse.Rect
	rows    []rowRect
	notices []mouse.Rect
	listBar barGeom
	docBar  barGeom
}

// browserBar is one box's live scrollbar: the last frame's parameters, the
// absolute row its track started on, and the pointer state of both.
type browserBar struct {
	geom    barGeom
	originY int
	hover   bool
	gesture workspacelist.ScrollGesture
}

// snapshot is the shared gesture's view of this bar. Reusing
// workspacelist.ScrollGesture keeps the press-and-drag mapping identical to the
// file browser's and Sessions', rather than a third copy of the same arithmetic.
func (b browserBar) snapshot() workspacelist.SidebarScrollbar {
	return workspacelist.SidebarScrollbar{Params: b.geom.params, Has: b.geom.has}
}

func (b browserBar) style() ui.ScrollbarStyle {
	return ui.ScrollbarStyle{
		Thumb: ui.HandleStateFrom(b.hover, b.gesture.Active()),
		Track: ui.HandleStateFrom(b.hover, b.gesture.Active()),
	}
}

// pointer is the browser's own handler, created on demand so a model built
// before it is ever drawn still has one when the first event arrives.
func (m *Model) pointer() *mouse.Handler {
	if m.mouse == nil {
		m.mouse = mouse.NewHandler()
	}
	return m.mouse
}

// HandleMouse routes one pointer event. An open overlay wins, exactly as it
// does for keys; everything else resolves through the browser's own hit map.
func (m *Model) HandleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.overlay.open() {
		if m.overlay.box == nil {
			return nil
		}
		if m.overlay.mouse == nil {
			m.overlay.mouse = mouse.NewHandler()
		}
		return m.applyOverlayAction(m.overlay.box.HandleMouse(msg, m.overlay.mouse))
	}
	wasDragging := m.pointer().IsDragging()
	dragSourceBefore := m.pointer().DragRegion()
	action := m.pointer().HandleMouse(msg)
	switch action.Type {
	case mouse.ActionHover:
		if wasDragging && !m.pointer().IsDragging() && dragSourceBefore == regionDetailSelect {
			// A release lost off-window or behind a focus change: the shared
			// handler drops its half on the first button-less motion, and the
			// selection gesture ends at the same boundary.
			m.AbandonSelection()
		}
		m.applyHover(action)
		return nil
	case mouse.ActionClick:
		return m.pointerClick(action, false)
	case mouse.ActionDoubleClick, mouse.ActionTripleClick:
		// A third rapid press is still a press. The browser has no gesture that
		// wants three of them, and dropping it would make the row, the rail or
		// a scrollbar go dead under a fast hand.
		return m.pointerClick(action, true)
	case mouse.ActionScrollUp, mouse.ActionScrollDown:
		return m.pointerWheel(action)
	case mouse.ActionDrag:
		return m.pointerDrag(action)
	case mouse.ActionDragEnd:
		return m.pointerDragEnd(action)
	}
	return nil
}

// applyHover keeps the rail and both bars lit under the pointer, and settles a
// gesture whose release was lost outside the window — the shared handler has
// already dropped its half by the time a button-less motion arrives here.
func (m *Model) applyHover(action mouse.MouseAction) {
	m.hoverRail = action.Region != nil && action.Region.ID == regionRail
	m.listBar.hover, m.docBar.hover = false, false
	if bar, ok := barOf(action.Region); ok {
		switch bar {
		case barList:
			m.listBar.hover = true
		case barDetail:
			m.docBar.hover = true
		}
	}
	if !m.pointer().IsDragging() {
		if m.listBar.gesture.Active() {
			m.listBar.gesture.End()
		}
		if m.docBar.gesture.Active() {
			m.docBar.gesture.End()
		}
		// A rail drag that ended this way never produced a release, so this is
		// the only place its split would be saved.
		m.settleSplit()
	}
}

// pointerClick is every click semantic the browser has, and each one is the
// key's own method rather than a second implementation of it.
func (m *Model) pointerClick(action mouse.MouseAction, double bool) tea.Cmd {
	if action.Region == nil {
		return nil
	}
	if bar, ok := barOf(action.Region); ok {
		m.pressScrollbar(bar, action)
		return nil
	}
	switch action.Region.ID {
	case regionRail:
		m.pointer().StartDrag(action.X, action.Y, regionRail, m.listShare())
		return nil
	case regionRow:
		m.SetPaneFocus(string(FocusList))
		index, _ := action.Region.Data.(int)
		s := m.activeState()
		if s == nil {
			return nil
		}
		// The second click on the already-selected row is Enter, and so is a
		// double click. Both go through openCursorRow, which is what the key
		// runs.
		if double || index == s.cursor {
			cmd := m.moveTo(index)
			return tea.Batch(cmd, m.openCursorRow())
		}
		return m.moveTo(index)
	case regionQuery:
		m.SetPaneFocus(string(FocusList))
		return m.beginQuery()
	case regionPill:
		m.SetPaneFocus(string(FocusList))
		return m.openViewModal()
	case regionClear:
		// The × drops the query, exactly as the filter-clear command does. It
		// is registered only where it is drawn, so this arm is unreachable
		// while the query is empty.
		m.SetPaneFocus(string(FocusList))
		return m.clearQuery()
	case regionOutcome, regionNotice:
		return m.openCoverage()
	case regionList:
		m.SetPaneFocus(string(FocusList))
		return nil
	case regionDetail:
		// The click that focuses the box happens here, as it always did; the
		// press also arms a selection, so the motion after it selects text and
		// a release without motion is still the click that just focused.
		m.SetPaneFocus(string(FocusDetail))
		return m.pressDetailSelection(action)
	}
	return nil
}

// pressScrollbar begins a bar gesture: a thumb press grabs where it landed, a
// track press jumps so the pressed row becomes the anchor, and the drag
// continues from there. The bars register after the rows precisely so this wins
// them — a scrollbar press never selects the row underneath.
func (m *Model) pressScrollbar(target barTarget, action mouse.MouseAction) {
	bar := m.bar(target)
	if bar == nil || !bar.geom.has || action.Region == nil {
		return
	}
	thumb := action.Region.ID == ui.RegionScrollbarThumb
	switch target {
	case barList:
		m.SetPaneFocus(string(FocusList))
		s := m.activeState()
		if s == nil {
			return
		}
		offset := bar.gesture.Press(bar.snapshot(), bar.originY, action.Y, thumb, s.scroll)
		m.scrollListTo(s, offset)
		m.pointer().StartDrag(action.X, action.Y, action.Region.ID, offset)
	case barDetail:
		m.SetPaneFocus(string(FocusDetail))
		offset := bar.gesture.Press(bar.snapshot(), bar.originY, action.Y, thumb, m.detail.scroll)
		m.detail.scroll = offset
		m.clampDetailScroll()
		m.pointer().StartDrag(action.X, action.Y, action.Region.ID, offset)
	}
}

// pointerWheel scrolls the box under the pointer, not the focused one.
func (m *Model) pointerWheel(action mouse.MouseAction) tea.Cmd {
	switch m.boxAt(action.Region, action.X) {
	case barDetail:
		m.detail.scroll += action.Delta
		m.clampDetailScroll()
		return nil
	default:
		s := m.activeState()
		if s == nil {
			return nil
		}
		return m.moveCursor(action.Delta)
	}
}

func (m *Model) pointerDrag(action mouse.MouseAction) tea.Cmd {
	switch m.pointer().DragRegion() {
	case regionDetailSelect:
		return m.selectionCmd(m.HandleSelectionMouse(action))
	case regionRail:
		m.railDragged = true
		m.setListShare(m.pointer().DragStartValue() + action.DragDX*100/max(m.width, 1))
		return nil
	case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
		if m.listBar.gesture.Active() {
			if s := m.activeState(); s != nil {
				m.scrollListTo(s, m.listBar.gesture.DragTo(action.Y))
			}
			return nil
		}
		if m.docBar.gesture.Active() {
			m.detail.scroll = m.docBar.gesture.DragTo(action.Y)
			m.clampDetailScroll()
		}
		return nil
	}
	return nil
}

// pointerDragEnd settles the gesture. Only the rail persists anything: a scroll
// offset is view state, and a split is a preference.
func (m *Model) pointerDragEnd(action mouse.MouseAction) tea.Cmd {
	switch action.DragStartID {
	case regionDetailSelect:
		return m.selectionCmd(m.HandleSelectionMouse(action))
	case regionRail:
		m.settleSplit()
	case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
		m.listBar.gesture.End()
		m.docBar.gesture.End()
	}
	return nil
}

// settleSplit saves where a rail drag left the split, whichever way the drag
// ended: on a release, or on the first button-less motion after a release the
// window never saw, which internal/mouse ends the gesture on and reports as
// hover. A drag that put the split back where it was writes nothing — a
// preference that did not change is not a preference to save.
func (m *Model) settleSplit() {
	if !m.railDragged {
		return
	}
	m.railDragged = false
	m.persistSplit()
}

// persistSplit is the one write both routes to the rail go through.
func (m *Model) persistSplit() {
	if state.GetPluginBrowserSplit(m.instance) == m.share {
		return
	}
	_ = state.SetPluginBrowserSplit(m.instance, m.share)
}

// scrollListTo moves the table's window to an offset a bar gesture chose, and
// carries the cursor with it. The browser has no free-scroll mode: its cursor
// is what Enter acts on, so a viewport the cursor is not in would make the next
// key jump somewhere the user is not looking.
func (m *Model) scrollListTo(s *collectionState, offset int) {
	visible := m.tableRows()
	if offset < 0 {
		offset = 0
	}
	if max := len(s.items) - visible; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}
	s.scroll = offset
	if s.cursor < offset {
		s.cursor = offset
	}
	if s.cursor > offset+visible-1 {
		s.cursor = offset + visible - 1
	}
	m.clampCursor(s)
}

// boxAt answers which of the two boxes a pointer event landed in.
func (m *Model) boxAt(region *mouse.Region, x int) barTarget {
	if bar, ok := barOf(region); ok {
		return bar
	}
	if region != nil {
		switch region.ID {
		case regionDetail:
			return barDetail
		case regionList, regionRow, regionQuery, regionPill, regionOutcome, regionClear, regionNotice:
			return barList
		}
	}
	// A point on no region at all is still in one of the boxes, and geometry is
	// the honest answer for which.
	listOuter, detailOuter := m.split()
	if detailOuter > 0 && x >= listOuter+paneGap {
		return barDetail
	}
	if m.paneMode() && m.paneShape == PaneDocument {
		return barDetail
	}
	return barList
}

func (m *Model) bar(target barTarget) *browserBar {
	switch target {
	case barList:
		return &m.listBar
	case barDetail:
		return &m.docBar
	}
	return nil
}

// barOf reports the box a scrollbar region belongs to.
func barOf(region *mouse.Region) (barTarget, bool) {
	if region == nil {
		return 0, false
	}
	if region.ID != ui.RegionScrollbarThumb && region.ID != ui.RegionScrollbarTrack {
		return 0, false
	}
	target, ok := region.Data.(barTarget)
	return target, ok
}

// listShare is the list's percentage of the content box: the dragged value if
// there is one, the remembered one on first use, and the default otherwise.
func (m *Model) listShare() int {
	if m.share == 0 {
		m.share = state.GetPluginBrowserSplit(m.instance)
		if m.share == 0 {
			m.share = listSharePercent
		}
	}
	return m.share
}

// setListShare applies a dragged share inside the floors both boxes need: the
// detail floor host.md names, and the twenty inner columns below which the
// table stops being a table.
func (m *Model) setListShare(share int) {
	lo, hi := m.shareBounds()
	if share < lo {
		share = lo
	}
	if share > hi {
		share = hi
	}
	m.share = share
}

// resizeColumns is how far one press of the resize keys moves the rail. Three
// columns is what the file browser's and Git's `+`/`-` move their sidebars by,
// and a key that moved a different distance on this surface would be a third
// answer to a question sidecar has already settled twice.
const resizeColumns = 3

// shareStep expresses that step in the unit this split is kept in. It is never
// less than one percent, because a step that rounded to nothing would make the
// key look dead on a wide box.
func (m *Model) shareStep() int {
	if m.width <= 0 {
		return 1
	}
	step := resizeColumns * 100 / m.width
	if step < 1 {
		step = 1
	}
	return step
}

// resizeSplit moves the rail one step in the given direction and persists where
// it settled. It goes through setListShare and state.SetPluginBrowserSplit —
// the same clamp and the same store the drag uses — so the two routes cannot
// drift, and a step that the floors refuse writes nothing.
func (m *Model) resizeSplit(direction int) tea.Cmd {
	if !m.canResizeSplit() {
		return nil
	}
	m.setListShare(m.listShare() + direction*m.shareStep())
	m.persistSplit()
	return nil
}

func (m *Model) shareBounds() (int, int) {
	if m.width <= 0 {
		return 1, 99
	}
	minList := chromeOverhead + 20
	maxList := m.width - paneGap - detailFloor
	if maxList < minList {
		return 1, 99
	}
	lo := (minList*100 + m.width - 1) / m.width
	hi := maxList * 100 / m.width
	if hi < lo {
		hi = lo
	}
	return lo, hi
}

// ListShare reports the list's current share of the box, for a test or a host
// that needs to see what a drag left behind.
func (m *Model) ListShare() int { return m.listShare() }

// registerRegions rebuilds the hit map from the geometry this frame recorded.
// listBox and detailBox are outer boxes; rail is already the widened target.
func (m *Model) registerRegions(listBox, detailBox, rail mouse.Rect) {
	hits := m.pointer().HitMap
	if listBox.W > 0 && listBox.H > 0 {
		hits.Add(regionList, listBox, nil)
	}
	if detailBox.W > 0 && detailBox.H > 0 {
		hits.Add(regionDetail, detailBox, nil)
	}
	// The inner content of a framed box starts one border and one padding
	// column in, and one border row down.
	lx, ly := listBox.X+2, listBox.Y+1
	dx, dy := detailBox.X+2, detailBox.Y+1
	for _, row := range m.geom.rows {
		hits.Add(regionRow, offsetRect(row.rect, lx, ly), row.index)
	}
	if m.geom.query.W > 0 {
		hits.Add(regionQuery, offsetRect(m.geom.query, lx, ly), nil)
	}
	if m.geom.pill.W > 0 {
		hits.Add(regionPill, offsetRect(m.geom.pill, lx, ly), nil)
	}
	if m.geom.outcome.W > 0 {
		hits.Add(regionOutcome, offsetRect(m.geom.outcome, lx, ly), nil)
	}
	// The × is registered after the query row it sits on, so it wins the hit
	// test: HitMap.Test scans in reverse and the smaller target has to be
	// found first.
	if m.geom.clear.W > 0 {
		hits.Add(regionClear, offsetRect(m.geom.clear, lx, ly), nil)
	}
	for i, notice := range m.geom.notices {
		hits.Add(regionNotice, offsetRect(notice, lx, ly), i)
	}
	m.listBar.geom = m.geom.listBar
	m.listBar.originY = ly + m.geom.listBar.track.Y
	m.docBar.geom = m.geom.docBar
	m.docBar.originY = dy + m.geom.docBar.track.Y
	registerBar(hits, m.geom.listBar, lx, ly, barList)
	registerBar(hits, m.geom.docBar, dx, dy, barDetail)
	if rail.W > 0 && rail.H > 0 {
		hits.Add(regionRail, rail, nil)
	}
}

func registerBar(hits *mouse.HitMap, geom barGeom, x, y int, target barTarget) {
	if !geom.has {
		return
	}
	hits.Add(ui.RegionScrollbarTrack, offsetRect(geom.track, x, y), target)
	hits.Add(ui.RegionScrollbarThumb, offsetRect(geom.thumb, x, y), target)
}

func offsetRect(r mouse.Rect, dx, dy int) mouse.Rect {
	return mouse.Rect{X: r.X + dx, Y: r.Y + dy, W: r.W, H: r.H}
}

// Regions exposes the hit map's regions, for tests that assert what a frame
// made clickable.
func (m *Model) Regions() []mouse.Region { return m.pointer().HitMap.Regions() }

// ScrollAtBoundaryAt reports whether a wheel notch at a point would move
// nothing, answered for the box under the pointer rather than the focused one,
// so the host can hand the notch to whatever is underneath.
func (m *Model) ScrollAtBoundaryAt(x, y, delta int) bool {
	// An open overlay is what the notch would scroll, and the list underneath
	// is not the box being asked about. Answering for the list would let the
	// host drop a wheel the modal was going to use.
	if m.overlay.open() {
		return false
	}
	region := m.pointer().HitMap.Test(x, y)
	if m.boxAt(region, x) == barDetail {
		if delta < 0 {
			return m.detail.scroll <= 0
		}
		return m.detail.scroll >= m.maxDetailScroll()
	}
	s := m.activeState()
	if s == nil {
		return true
	}
	if delta < 0 {
		return s.cursor <= 0
	}
	return s.cursor >= len(s.items)-1 && s.nextCursor == ""
}

// pressDetailSelection arms a selection gesture over the detail box's text. The
// drag is registered with the shared handler because that is what turns the
// release into a drag end the gesture can be finished by.
func (m *Model) pressDetailSelection(action mouse.MouseAction) tea.Cmd {
	result := m.HandleSelectionMouse(action)
	if !result.Handled {
		// The press landed on the box's chrome or the padding below the last
		// row: not a selection, and the focus it moved is the whole of what the
		// click meant.
		return nil
	}
	m.pointer().StartDrag(action.X, action.Y, regionDetailSelect, 0)
	return m.selectionCmd(result)
}

// selectionCmd is what the browser owes an engine result: a copy, phrased by
// the shared pipeline and carried by the app's own alert types. A result that
// asked for nothing produces no command.
func (m *Model) selectionCmd(result textselect.Result) tea.Cmd {
	return m.SelectionCopyCmd(result, func(notice textselect.CopyNotice) tea.Msg {
		if notice.IsError {
			return msg.ToastMsg{Message: notice.Message, Duration: notice.Duration, IsError: true}
		}
		return msg.FlashMsg{Text: notice.Message}
	})
}
