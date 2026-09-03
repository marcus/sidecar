package pluginbrowser

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/textselect"
)

// Text selection in the browser's detail box.
//
// The card is a list of rendered rows the box already lays out and slices at
// its own scroll offset, which is exactly the shape [textselect.Source]
// describes, so nothing here decides what a gesture means: the engine does, and
// this file only says where the rows are drawn, what they say, and how far the
// box is scrolled.
//
// The coordinate space is the card's own visual row, numbered from the top of
// the unscrolled block, so a selection stays on the text it was made over while
// the box scrolls under it. The list beside the detail is not selectable: its
// rows are a table whose cells are targets, and a drag over them is the row
// gesture the pointer model already owns.

// SetSelection binds the host's shared selection settings: the chords the
// detail answers, and whether finishing a drag copies without being asked.
func (m *Model) SetSelection(keys textselect.Keys, copyOnSelect bool) {
	if m == nil {
		return
	}
	m.selection.Keys = keys
	m.selection.CopyOnSelect = copyOnSelect
}

// HasSelection reports whether anything in the detail box is selected. It is
// asked outside a render — by a key, by a click elsewhere — so it settles the
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
	return m.selection.SelectedText(detailSource{m})
}

// HandleSelectionMouse advances the gesture over the detail box and reports
// what the host owes for it.
//
// A drag that has run past an edge scrolls the box here rather than in the
// host: the window it is asking for is this model's own. The rows it reveals
// are selected by the next motion event, as the engine's contract says.
func (m *Model) HandleSelectionMouse(action mouse.MouseAction) textselect.Result {
	if m == nil {
		return textselect.Result{}
	}
	m.expireSelection()
	result := m.selection.HandleMouse(action, detailSource{m})
	if result.AutoScroll != 0 {
		before := m.detail.scroll
		m.detail.scroll += result.AutoScroll
		m.clampDetailScroll()
		result.AutoScroll = m.detail.scroll - before
	}
	return result
}

// HandleSelectionKey answers the chords that act on the selection: copy,
// select-all, and escape.
func (m *Model) HandleSelectionKey(msg tea.KeyMsg) textselect.Result {
	if m == nil {
		return textselect.Result{}
	}
	m.expireSelection()
	if press, ok := msg.(tea.KeyPressMsg); ok && press.String() == "esc" && m.selection.HasSelection() {
		m.selection.Clear()
		return textselect.Result{Handled: true, Changed: true}
	}
	return m.selection.HandleKey(msg, detailSource{m})
}

// SelectionCopyCmd delivers the copy an engine result asked for, phrased by the
// shared pipeline and wrapped in whatever notification type the host uses.
func (m *Model) SelectionCopyCmd(result textselect.Result, wrap func(textselect.CopyNotice) tea.Msg) tea.Cmd {
	if m == nil || !result.CopyAsked {
		return nil
	}
	return m.selection.Keys.CopySelectionCmd(result.Copy, wrap)
}

// expireSelection drops a selection whose rows are about to be replaced.
//
// A selection names visual rows, and everything in the key below changes which
// text is on which row: a new document, a refresh that landed, a width that
// re-wraps the body, a theme that re-renders it. Hanging the check off the same
// inputs the card is rendered from means a new reason to re-lay it out cannot
// forget to clear it.
func (m *Model) expireSelection() {
	key := m.detailLayoutKey()
	if m.selectionKey == key {
		return
	}
	m.selectionKey = key
	m.selection.Clear()
}

// detailLayoutKey identifies the rows the detail box is currently laying out.
func (m *Model) detailLayoutKey() string {
	return fmt.Sprintf("%d|%d|%s|%s|%t|%t|%d",
		m.detailInnerWidth(), m.detail.generation, m.detail.shownID, m.styleKey(),
		m.detail.loaded, m.detail.err != nil, m.height)
}

// detailContentRect is where the card's text is drawn, inside the panel border
// and padding the box spends and outside the column its scrollbar reserves. A
// press on the border, the bar or the list beside it lands outside the surface
// entirely and stays the host's ordinary click.
func (m *Model) detailContentRect() mouse.Rect {
	if m == nil || m.width <= 0 || m.height <= 0 {
		return mouse.Rect{}
	}
	height := m.height - 2
	if height < 1 {
		return mouse.Rect{}
	}
	if m.paneMode() {
		if m.paneShape != PaneDocument {
			return mouse.Rect{}
		}
		return mouse.Rect{X: 2, Y: 1, W: scrolledWidth(m.width - chromeOverhead), H: height}
	}
	listOuter, detailOuter := m.split()
	if detailOuter <= 0 {
		return mouse.Rect{}
	}
	return mouse.Rect{
		X: listOuter + paneGap + 2,
		Y: 1,
		W: scrolledWidth(detailOuter - chromeOverhead),
		H: height,
	}
}

// detailInnerWidth is how many columns of a card row the box draws.
func (m *Model) detailInnerWidth() int {
	return m.detailContentRect().W
}

// detailSource is the card as the selection engine reads it.
type detailSource struct{ m *Model }

var _ textselect.Source = detailSource{}

func (s detailSource) ContentRect() mouse.Rect { return s.m.detailContentRect() }

// Line is the row as it reaches the screen: the card's own line held to exactly
// the drawn width, which is what stops a drag from copying columns the box
// truncated away.
func (s detailSource) Line(i int) string {
	rows := s.m.detailRows()
	if i < 0 || i >= len(rows) {
		return ""
	}
	return fitStyled(rows[i], s.m.detailInnerWidth())
}

func (s detailSource) LineCount() int { return len(s.m.detailRows()) }

func (s detailSource) Scroll() int { return s.m.detail.scroll }

// TabWidth is zero because the card holds no tabs: every row is built from
// wrapped, styled cells in the column space it is drawn in.
func (s detailSource) TabWidth() int { return 0 }

// detailRows is the unscrolled card, which is the coordinate space a selection
// is kept in.
func (m *Model) detailRows() []string {
	width := m.detailInnerWidth()
	if width < 1 {
		return nil
	}
	return m.detailBlock(width)
}
