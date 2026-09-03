package issueview

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/textselect"
)

// Text selection in an issue card.
//
// The binding lives here, once, because one Model is the issue pane in the
// project workspace, in the global Workspaces browser and in the app's content
// deck. A host says where its leaf was drawn and forwards the pointer and key
// events it already routes; everything about what a gesture means, what is
// highlighted and what reaches the clipboard is decided here and inherited by
// all three. A rule written in a host would be a rule the other surfaces do not
// have.
//
// The coordinate space is the card's own visual row — buildRows has already
// wrapped and styled the issue to the content width, so the engine never sees
// wrapping — numbered from the top of the card, so a selection stays on the
// text it was made over while the card scrolls under it. The left padding and
// the scrollbar column are outside the surface entirely, so neither can be
// selected or copied.

var _ textselect.Pane = (*Model)(nil)

// SetSelection binds the host's shared selection settings: the chords the card
// answers, and whether finishing a drag copies without being asked.
func (m *Model) SetSelection(keys textselect.Keys, copyOnSelect bool) {
	if m == nil {
		return
	}
	m.selection.Keys = keys
	m.selection.CopyOnSelect = copyOnSelect
}

// SetOrigin records where the host last drew this card's body, in the
// coordinate space its mouse events arrive in. A card that has not been drawn
// has no origin, so it hit-tests as empty rather than as the top-left corner of
// the screen.
func (m *Model) SetOrigin(x, y int) {
	if m == nil {
		return
	}
	m.originX, m.originY = x, y
}

// HasSelection reports whether anything in this card is selected. It is asked
// outside a render — by a key, by a click elsewhere — so it settles the expiry
// itself rather than reporting a selection the next frame will drop.
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
	m.selection.Clear()
}

// AbandonSelection ends a gesture whose release never arrived.
func (m *Model) AbandonSelection() textselect.Result {
	if m == nil {
		return textselect.Result{}
	}
	return m.selection.Abandon()
}

// SelectionText is the selection as the user sees it: the rows it covers,
// without the styling they were drawn with.
func (m *Model) SelectionText() []string {
	if m == nil {
		return nil
	}
	m.expireSelection()
	return m.selection.SelectedText(issueSource{m})
}

// SelectableAt reports whether a press at view-local (x, y) is text rather than
// one of the card's own targets. A parent row, a subtask row and the scrollbar
// each answer a click already, and a press on one of them must stay that click
// rather than arming a gesture over it.
func (m *Model) SelectableAt(x, y int) bool {
	if m == nil {
		return false
	}
	if m.scrollbarContains(x, y) {
		return false
	}
	if hit := m.hitAt(x, y); hit != nil {
		return false
	}
	rect := issueSource{m}.ContentRect()
	return rect.Contains(m.originX+x, m.originY+y)
}

// HandleSelectionMouse advances the gesture over the card and reports what the
// host owes for it.
//
// A drag that has run past an edge scrolls the card here rather than in the
// host: the window it is asking for is this model's own, and a host that moved
// it would be the second place that knows how far an issue can scroll. The rows
// it reveals are selected by the next motion event, as the engine's contract
// says.
func (m *Model) HandleSelectionMouse(action mouse.MouseAction) textselect.Result {
	if m == nil {
		return textselect.Result{}
	}
	m.expireSelection()
	result := m.selection.HandleMouse(action, issueSource{m})
	if result.AutoScroll != 0 {
		before := m.scroll
		m.Scroll(result.AutoScroll)
		result.AutoScroll = m.scroll - before
	}
	return result
}

// HandleSelectionKey answers the chords that act on the selection: copy,
// select-all, and escape.
//
// Escape belongs here rather than in each host: what it means to a live
// selection is the selection's business, and a host that wrote the rule itself
// would be a host the other surfaces have to remember to match.
func (m *Model) HandleSelectionKey(msg tea.KeyMsg) textselect.Result {
	if m == nil {
		return textselect.Result{}
	}
	m.expireSelection()
	if press, ok := msg.(tea.KeyPressMsg); ok && press.String() == "esc" && m.selection.HasSelection() {
		m.selection.Clear()
		return textselect.Result{Handled: true, Changed: true}
	}
	return m.selection.HandleKey(msg, issueSource{m})
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

// expireSelection drops a selection whose rows are about to be replaced. A
// selection names visual rows, and everything the render key tracks changes
// which text is on which row: a new issue, a live re-read, a width that
// re-wraps, a theme that re-renders.
func (m *Model) expireSelection() {
	key := fmt.Sprintf("%d|%d|%d|%s|%s", m.width, m.height, m.renderGeneration, m.issueID, m.renderer.StyleKey())
	if m.selectionKey == key {
		return
	}
	m.selectionKey = key
	m.selection.Clear()
}

// issueSource is the card as the selection engine reads it: where its text is
// drawn, what its rows say, and how far it is scrolled.
type issueSource struct{ m *Model }

var _ textselect.Source = issueSource{}

// ContentRect is the body box minus the left padding in front of it and the
// scrollbar column at its right, which is what makes both unselectable.
func (s issueSource) ContentRect() mouse.Rect {
	m := s.m
	if m.width <= 0 || m.height <= 0 {
		return mouse.Rect{}
	}
	return mouse.Rect{
		X: m.originX + m.leftPadding(),
		Y: m.originY,
		W: m.contentWidth(),
		H: m.height,
	}
}

// Line is the row as it reaches the screen: tab-expanded and held to the drawn
// width by the same fitLine the frame paints it through, so the columns the
// selection names are the columns it was drawn at.
func (s issueSource) Line(i int) string {
	rows := s.m.ensureRows()
	if i < 0 || i >= len(rows) {
		return ""
	}
	return fitLine(rows[i].text, s.m.contentWidth())
}

func (s issueSource) LineCount() int { return len(s.m.ensureRows()) }

func (s issueSource) Scroll() int { return s.m.scroll }

// TabWidth is zero because fitLine has already expanded the tabs, in the column
// space the rows are drawn in.
func (s issueSource) TabWidth() int { return 0 }
