package docview

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/textselect"
)

// Text selection for a document pane.
//
// The binding lives here, once, because one Model is the file pane in the
// project workspace AND in the global Workspaces browser. A pane host says
// where its leaf was drawn and forwards the pointer and key events it already
// routes; everything about what a gesture means, what is highlighted and what
// reaches the clipboard is decided here and inherited by both surfaces. A rule
// written in a host would be a rule the other surface does not have.
//
// The coordinate space is the laid-out visual row: [displayRows] has already
// wrapped and tab-expanded the document, so the engine never sees wrapping and
// never has to guess where a tab stop landed. Rows are numbered absolutely, so a
// selection survives scrolling, and the gutter is not part of a row's text at
// all, so it can be neither selected nor copied.

// SetSelection binds the host's shared selection settings: the chords the pane
// answers, and whether finishing a drag copies without being asked.
func (m *Model) SetSelection(keys textselect.Keys, copyOnSelect bool) {
	if m == nil {
		return
	}
	m.selection.Keys = keys
	m.selection.CopyOnSelect = copyOnSelect
}

// SetOrigin records where the host last drew this document's content box, in
// the coordinate space its mouse events arrive in. A pane that has not been
// drawn has no origin, so it hit-tests as empty rather than as the top-left
// corner of the screen.
func (m *Model) SetOrigin(x, y int) {
	if m == nil {
		return
	}
	m.originX, m.originY = x, y
}

// HasSelection reports whether anything in this document is selected. It is
// asked by hosts outside a render — a key, a click elsewhere — so it settles the
// expiry itself rather than reporting a selection the next frame will drop.
func (m *Model) HasSelection() bool {
	if m == nil {
		return false
	}
	m.expireSelection()
	return m.selection.HasSelection()
}

// ClearSelection drops the selection and any gesture editing it.
func (m *Model) ClearSelection() {
	if m == nil {
		return
	}
	if m.selection.HasSelection() {
		m.selection.Clear()
		m.bumpVisualRevision()
		return
	}
	m.selection.Clear()
}

// ClearSelectionsExcept drops every selection in this group but keep's, which
// is what makes one selection at a time the rule: starting one anywhere drops
// the one before it. A nil keep drops them all.
func (t Tabs) ClearSelectionsExcept(keep *Model) {
	for _, item := range t.Items {
		if item.View != keep {
			item.View.ClearSelection()
		}
	}
}

// AbandonSelection ends a gesture whose release never arrived — the pointer left
// the window, a modal opened, focus moved.
func (m *Model) AbandonSelection() textselect.Result {
	if m == nil {
		return textselect.Result{}
	}
	hadVisualState := m.selection.HasSelection() || m.scrollbarDrag.active || m.scrollbarHover
	m.abandonScrollbarGesture()
	result := m.selection.Abandon()
	if hadVisualState || result.Changed {
		m.bumpVisualRevision()
	}
	return result
}

// SelectionText is the selection as the user sees it: the visible rows, without
// the styling they were drawn with and without the gutter in front of them.
func (m *Model) SelectionText() []string {
	if m == nil {
		return nil
	}
	m.expireSelection()
	return m.selection.SelectedText(selectionSource{m})
}

// HandleSelectionMouse advances the selection gesture and reports what the host
// owes for it.
//
// A drag that has run past an edge is scrolled here rather than by the host:
// the window it is asking for is this model's own, and a host that moved it
// would be the second place that knows how far a document can scroll. The rows
// it reveals are selected by the next motion event, as the engine's contract
// says. AutoScroll is reported back as the rows that were actually applied — a
// document already at its last row moves nothing — so a host that persists the
// offset saves exactly when a drag changed it.
func (m *Model) HandleSelectionMouse(action mouse.MouseAction) textselect.Result {
	if m == nil {
		return textselect.Result{}
	}
	// The scrollbar column is answered here, before the selection engine can
	// see it: a press on the bar must never arm a selection or resolve into a
	// click-through. See scrollbar.go.
	beforeScroll, beforeHover, beforeDrag := m.scroll, m.scrollbarHover, m.scrollbarDrag.active
	if result, handled := m.handleScrollbarMouse(action); handled {
		if result.Changed || m.scroll != beforeScroll || m.scrollbarHover != beforeHover || m.scrollbarDrag.active != beforeDrag {
			m.bumpVisualRevision()
		}
		return result
	}
	result := m.selection.HandleMouse(action, selectionSource{m})
	if result.AutoScroll != 0 {
		before := m.scroll
		m.Scroll(result.AutoScroll)
		result.AutoScroll = m.scroll - before
	}
	if result.Changed || m.scroll != beforeScroll || m.scrollbarHover != beforeHover || m.scrollbarDrag.active != beforeDrag {
		m.bumpVisualRevision()
	}
	return result
}

// HandleSelectionKey answers the chords that act on the selection: copy,
// select-all, and escape.
//
// Escape belongs here rather than in each host: what it means to a live
// selection is the selection's business, and a host that wrote the rule itself
// would be a host the other surface has to remember to match.
func (m *Model) HandleSelectionKey(msg tea.KeyMsg) textselect.Result {
	if m == nil {
		return textselect.Result{}
	}
	if press, ok := msg.(tea.KeyPressMsg); ok && press.String() == "esc" && m.HasSelection() {
		m.ClearSelection()
		return textselect.Result{Handled: true, Changed: true}
	}
	result := m.selection.HandleKey(msg, selectionSource{m})
	if result.Changed {
		m.bumpVisualRevision()
	}
	return result
}

// SelectionCopyCmd delivers the copy an engine result asked for, phrased by the
// shared pipeline and wrapped in whatever notification type the host uses. A
// result that asked for nothing produces no command, so a host can hand every
// result it gets straight here.
func (m *Model) SelectionCopyCmd(result textselect.Result, wrap func(textselect.CopyNotice) tea.Msg) tea.Cmd {
	if m == nil || !result.CopyAsked {
		return nil
	}
	return m.selection.Keys.CopySelectionCmd(result.Copy, wrap)
}

// expireSelection drops a selection whose rows are about to be replaced.
//
// A selection names visual rows, and everything the layout key tracks changes
// which text is on which row: a new document, a live re-read, a wrap toggle, a
// width that re-wraps. Hanging the check off the layout the renderer is already
// keying on means a new reason to re-lay out cannot forget to clear it.
func (m *Model) expireSelection() {
	key := m.currentLayoutKey()
	if m.selectionKey == key {
		return
	}
	m.selectionKey = key
	if m.selection.HasSelection() {
		m.selection.Clear()
		m.bumpVisualRevision()
		return
	}
	m.selection.Clear()
}

// selectionSource is the document as the selection engine reads it: where its
// text is drawn, what its rows say, and how far it is scrolled.
type selectionSource struct{ m *Model }

var _ textselect.Source = selectionSource{}

// ContentRect is the content box minus the gutter, which is what makes the
// gutter unselectable: a press on a line number lands outside the surface
// entirely and stays the host's ordinary click.
func (s selectionSource) ContentRect() mouse.Rect {
	m := s.m
	return mouse.Rect{
		X: m.originX + m.display().gutterWidth,
		Y: m.originY,
		W: s.contentWidth(),
		H: m.contentHeight(),
	}
}

// Line is the row as it reaches the screen, not as the layout holds it: with
// wrap off a long line keeps its whole text and is truncated on the way out
// (fitLine), and the columns past the pane's edge were never drawn. Cutting the
// row to the drawn width here is what stops a drag over the pane beside this one
// from copying text nobody could see, on the row it started on and on every
// whole row a multi-row selection covers.
func (s selectionSource) Line(i int) string {
	rows := s.m.display().rows
	if i < 0 || i >= len(rows) {
		return ""
	}
	return ansi.Truncate(rows[i], s.contentWidth(), "")
}

// contentWidth is how many columns of a row the pane draws, which is its width
// less the gutter in front of it and the scrollbar column at the right.
func (s selectionSource) contentWidth() int {
	return max(s.m.contentWidth()-s.m.display().gutterWidth, 0)
}

func (s selectionSource) LineCount() int { return len(s.m.display().rows) }

func (s selectionSource) Scroll() int { return s.m.scroll }

// TabWidth is zero because the rows hold no tabs: the layout expanded them in
// the column space they are drawn in, which the engine could not have
// reproduced from a tab width alone — it does not know the gutter is in front
// of them.
func (s selectionSource) TabWidth() int { return 0 }

// docview's viewer is one of the panes every selectable-pane host routes
// through the shared interface. The assertion is here so a change to either
// side is a build error rather than a host arm that quietly stops matching.
var _ textselect.Pane = (*Model)(nil)
