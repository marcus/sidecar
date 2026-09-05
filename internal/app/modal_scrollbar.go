package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/ui"
)

// Interactive list scrollbars for the switcher modals (project, worktree,
// theme). Each list section declares its bar with modal.SectionScrollbar; the
// modal library places the hit regions during render, and the switcher's own
// mouse handler claims the bar's events through modal.SectionBarAt — the same
// shape the notification centre uses for its bar.
//
// The gestures are answered here rather than inside Modal.HandleMouse because
// the app Model is a value that is copied per update: only code running in the
// handler chain rooted at this update's copy may mutate model state, and these
// interceptors do exactly that.

// switcherBarState is one switcher list's scrollbar pointer state: whether the
// pointer hovers the bar, and a snapshot of the live gesture taken at press
// time so re-renders cannot shift the mapping under the pointer.
type switcherBarState struct {
	hovering  bool
	active    bool // press seen, gesture not yet settled
	params    ui.ScrollbarParams
	trackY    int  // absolute row of the track top at press time
	grabDelta int  // track rows between pointer and thumb anchor
	moved     bool // the drag changed the list at least once
}

// dragging reports that this bar's gesture is live on its handler.
func (s *switcherBarState) dragging(h *mouse.Handler) bool {
	if !s.active || h == nil || !h.IsDragging() {
		return false
	}
	switch h.DragRegion() {
	case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
		return true
	}
	return false
}

// style derives the bar's pointer emphasis: hover lights the rail, an active
// drag outranks it (ui.HandleStateFrom precedence).
func (s *switcherBarState) style(h *mouse.Handler) ui.ScrollbarStyle {
	state := ui.HandleStateFrom(s.hovering, s.dragging(h))
	return ui.ScrollbarStyle{Thumb: state, Track: state}
}

// settle ends a gesture. The scroll position it left behind is view state;
// nothing persists.
func (s *switcherBarState) settle() {
	hover := s.hovering
	*s = switcherBarState{hovering: hover}
}

// switcherBarOps adapts the shared scrollbar gesture core to one switcher's
// list: reading its current offset for the grab anchor, applying a dragged
// offset (clamping and cursor-follow are the owner's business), and deciding
// what a release produces. All callbacks run on the live model copy.
type switcherBarOps struct {
	current   func() int
	apply     func(offset int)
	onRelease func(moved bool) tea.Cmd
}

// switcherBarMouseEvent answers the events belonging to a declared list bar:
// presses start thumb grabs or jump-to-spot drags anchored at the grabbed row,
// motions map back through the press-time snapshot (clamped by the shared math
// without losing the gesture), releases settle wherever the pointer stands,
// and a lost release — button-less motion — settles too instead of leaving a
// dead anchor. It reports handled=true only for events it consumed.
func switcherBarMouseEvent(bar *switcherBarState, md *modal.Modal, h *mouse.Handler, msg tea.MouseMsg, ops switcherBarOps) (bool, tea.Cmd) {
	if md == nil || h == nil {
		return false, nil
	}
	mi := msg.Mouse()

	switch msg.(type) {
	case tea.MouseClickMsg:
		if mi.Button != tea.MouseLeft {
			return false, nil
		}
		declared, _, trackY, onThumb, ok := md.SectionBarAt(mi.X, mi.Y, h)
		if !ok {
			return false, nil
		}
		params := ui.ScrollbarParams{
			TotalItems:   declared.TotalItems,
			ScrollOffset: declared.ScrollOffset,
			VisibleItems: declared.VisibleItems,
			TrackHeight:  declared.TrackHeight,
		}
		bar.active = true
		bar.params = params
		bar.trackY = trackY
		bar.moved = false
		if onThumb {
			bar.grabDelta = mi.Y - trackY - ui.RowForOffset(params, ops.current())
			h.StartDrag(mi.X, mi.Y, ui.RegionScrollbarThumb, ops.current())
			return true, nil
		}
		// Jump-to-spot: the grabbed point becomes the thumb anchor, so the
		// continuing drag maps the pointer straight onto track rows.
		bar.grabDelta = 0
		jump := ui.OffsetAtRow(params, mi.Y-trackY)
		ops.apply(jump)
		h.StartDrag(mi.X, mi.Y, ui.RegionScrollbarTrack, jump)
		return true, nil

	case tea.MouseMotionMsg:
		if !bar.active {
			// Hover tracking. A motion over the bar is consumed so rows
			// underneath the column never light up from it.
			if _, _, _, _, onBar := md.SectionBarAt(mi.X, mi.Y, h); onBar {
				bar.hovering = true
				return true, nil
			}
			bar.hovering = false
			return false, nil
		}
		if mi.Button == tea.MouseNone || !h.IsDragging() {
			// Lost release: settle the dead gesture where it stands.
			h.EndDrag()
			wasMoved := bar.moved
			bar.settle()
			return true, ops.onRelease(wasMoved)
		}
		ops.apply(ui.OffsetAtRow(bar.params, mi.Y-bar.trackY-bar.grabDelta))
		return true, nil

	case tea.MouseReleaseMsg:
		if !bar.active {
			return false, nil
		}
		wasMoved := bar.moved
		if h.IsDragging() {
			h.EndDrag()
		}
		bar.settle()
		return true, ops.onRelease(wasMoved)
	}

	return false, nil
}

// projectSwitcherBarEvent routes the project list's bar events. Previews stay
// out of drag motion (one gesture one recolour): the wheel path recolours per
// notch, a drag recolours once, on release, if it moved anything.
func (m *Model) projectSwitcherBarEvent(msg tea.MouseMsg) (bool, tea.Cmd) {
	return switcherBarMouseEvent(&m.projectSwitcherBar, m.projectSwitcherModal,
		m.projectSwitcherMouseHandler, msg,
		switcherBarOps{
			current: func() int { return m.projectSwitcherScroll },
			apply:   func(off int) { m.setProjectSwitcherScroll(off) },
			onRelease: func(moved bool) tea.Cmd {
				if moved {
					return m.previewProjectTheme()
				}
				return nil
			},
		})
}

// worktreeSwitcherBarEvent routes the worktree list's bar events. The list
// counts two rows per entry, so offsets map through doubling; there is no
// preview to fire.
func (m *Model) worktreeSwitcherBarEvent(msg tea.MouseMsg) (bool, tea.Cmd) {
	return switcherBarMouseEvent(&m.worktreeSwitcherBar, m.worktreeSwitcherModal,
		m.worktreeSwitcherMouseHandler, msg,
		switcherBarOps{
			current: func() int { return m.worktreeSwitcherScroll * 2 },
			apply: func(rows int) {
				m.setWorktreeSwitcherScroll((rows + 1) / 2)
			},
			onRelease: func(bool) tea.Cmd { return nil },
		})
}

// themeSwitcherBarEvent routes the theme list's bar events. The window
// position is derived from the selection, so scrolling moves the selection —
// silently during the drag; the preview fires once, on release.
func (m *Model) themeSwitcherBarEvent(msg tea.MouseMsg) (bool, tea.Cmd) {
	return switcherBarMouseEvent(&m.themeSwitcherBar, m.themeSwitcherModal,
		m.themeSwitcherMouseHandler, msg,
		switcherBarOps{
			current: func() int { return m.themeSwitcherDerivedOffset() },
			apply:   func(off int) { m.selectThemeAtWindowBottom(off) },
			onRelease: func(moved bool) tea.Cmd {
				if !moved {
					return nil
				}
				themes := m.themeSwitcherFiltered
				if idx := m.themeSwitcherSelectedIdx; idx >= 0 && idx < len(themes) && !themes[idx].IsSeparator {
					return m.previewThemeEntry(themes[idx])
				}
				return nil
			},
		})
}

// themeSwitcherDerivedOffset mirrors how the theme list's render derives its
// window from the selection.
func (m *Model) themeSwitcherDerivedOffset() int {
	const maxVisible = themeSwitcherMaxVisible
	themes := m.themeSwitcherFiltered
	selectedIdx := m.themeSwitcherSelectedIdx
	if selectedIdx < 0 {
		selectedIdx = 0
	}
	if selectedIdx >= len(themes) {
		selectedIdx = len(themes) - 1
	}
	off := 0
	if selectedIdx >= maxVisible {
		off = selectedIdx - maxVisible + 1
	}
	return max(0, min(off, len(themes)-min(maxVisible, len(themes))))
}

// setProjectSwitcherScroll clamps a viewport scroll and keeps the cursor
// inside the visible window, so Enter after a drag still acts on a shown row.
func (m *Model) setProjectSwitcherScroll(off int) {
	// The bar scrolls the collection, and pinned rows are not part of it, so
	// every offset here is collection-relative and the cursor is converted in
	// and out of that space rather than compared across the two.
	pinned := m.projectSwitcherPinnedCount()
	count := max(0, len(m.projectSwitcherFiltered)-pinned)
	visible := min(m.projectSwitcherVisibleRows(), count)
	maxOff := max(0, count-visible)
	next := min(max(off, 0), maxOff)
	if next != m.projectSwitcherScroll {
		m.projectSwitcherBar.moved = true
		m.projectSwitcherScroll = next
	}
	if count == 0 {
		return
	}
	cursor := max(0, m.projectSwitcherCursor-pinned)
	m.projectSwitcherCursor = keepSwitcherCursorInWindow(cursor, next, visible) + pinned
}

// setWorktreeSwitcherScroll clamps an item-unit scroll and keeps the cursor in
// the visible window.
func (m *Model) setWorktreeSwitcherScroll(off int) {
	worktrees := m.worktreeSwitcherFiltered
	const maxVisible = 8
	visible := min(maxVisible, len(worktrees))
	maxOff := max(0, len(worktrees)-visible)
	next := min(max(off, 0), maxOff)
	if next != m.worktreeSwitcherScroll {
		m.worktreeSwitcherBar.moved = true
		m.worktreeSwitcherScroll = next
	}
	m.worktreeSwitcherCursor = keepSwitcherCursorInWindow(m.worktreeSwitcherCursor, next, visible)
}

// selectThemeAtWindowBottom lands the selection on the last row of the window
// whose top is off — the inverse of how the render derives its window from the
// selection — skipping back over separators.
func (m *Model) selectThemeAtWindowBottom(off int) {
	themes := m.themeSwitcherFiltered
	visible := min(themeSwitcherMaxVisible, len(themes))
	maxOff := max(0, len(themes)-visible)
	off = min(max(off, 0), maxOff)

	sel := off + visible - 1
	sel = min(sel, len(themes)-1)
	for sel > 0 && themes[sel].IsSeparator {
		sel--
	}
	if sel != m.themeSwitcherSelectedIdx && !themes[sel].IsSeparator {
		m.themeSwitcherSelectedIdx = sel
		m.themeSwitcherBar.moved = true
	}
}

// keepSwitcherCursorInWindow moves the cursor so Enter always acts on a row
// the viewport scroll actually shows.
func keepSwitcherCursorInWindow(cursor, scroll, visibleCount int) int {
	return min(max(cursor, scroll), scroll+visibleCount-1)
}
