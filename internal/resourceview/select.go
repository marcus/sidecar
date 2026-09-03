package resourceview

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/textselect"
)

// Text selection in a resource card.
//
// The binding lives here, once, because one Model is the resource pane in the
// project workspace, in the global Workspaces browser and in the app's content
// deck. A host says where its leaf was drawn and forwards the pointer and key
// events it already routes; everything about what a gesture means, what is
// highlighted and what reaches the clipboard is decided here and inherited by
// all three.
//
// The coordinate space is the card's own visual row — lines() has already
// wrapped and styled the document to the box's width — numbered from the top of
// the card, so a selection stays on the text it was made over while the card
// scrolls under it.
//
// A plugin-shaped tab is not this card at all: it is the shared browser in pane
// mode, which owns its own selectable detail box (internal/pluginbrowser). Only
// the chords are forwarded to it, because everything else about its gesture is
// answered in its own coordinate space.

var _ textselect.Pane = (*Model)(nil)

// SetSelection binds the host's shared selection settings: the chords the card
// answers, and whether finishing a drag copies without being asked.
func (m *Model) SetSelection(keys textselect.Keys, copyOnSelect bool) {
	if m == nil {
		return
	}
	if m.browser != nil {
		m.browser.SetSelection(keys, copyOnSelect)
		return
	}
	m.selection.Keys = keys
	m.selection.CopyOnSelect = copyOnSelect
}

// SetOrigin records where the host last drew this card, in the coordinate space
// its mouse events arrive in. A card that has not been drawn has no origin, so
// it hit-tests as empty rather than as the top-left corner of the screen.
func (m *Model) SetOrigin(x, y int) {
	if m == nil {
		return
	}
	m.originX, m.originY = x, y
}

// HasSelection reports whether anything in this card is selected.
func (m *Model) HasSelection() bool {
	if m == nil || m.browser != nil {
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
	if m == nil || m.browser != nil {
		return nil
	}
	m.expireSelection()
	return m.selection.SelectedText(resourceSource{m})
}

// HandleSelectionMouse advances the gesture over the card and reports what the
// host owes for it. A drag that has run past an edge scrolls the card here
// rather than in the host: the window it is asking for is this model's own.
func (m *Model) HandleSelectionMouse(action mouse.MouseAction) textselect.Result {
	if m == nil || m.browser != nil {
		return textselect.Result{}
	}
	m.expireSelection()
	result := m.selection.HandleMouse(action, resourceSource{m})
	if result.AutoScroll != 0 {
		before := m.scroll
		m.ScrollBy(result.AutoScroll)
		result.AutoScroll = m.scroll - before
	}
	return result
}

// HandleSelectionKey answers the chords that act on the selection: copy,
// select-all, and escape.
func (m *Model) HandleSelectionKey(msg tea.KeyMsg) textselect.Result {
	if m == nil || m.browser != nil {
		return textselect.Result{}
	}
	m.expireSelection()
	if press, ok := msg.(tea.KeyPressMsg); ok && press.String() == "esc" && m.selection.HasSelection() {
		m.selection.Clear()
		return textselect.Result{Handled: true, Changed: true}
	}
	return m.selection.HandleKey(msg, resourceSource{m})
}

// SelectionCopyCmd delivers the copy an engine result asked for, phrased by the
// shared pipeline and wrapped in whatever notification type the host uses.
func (m *Model) SelectionCopyCmd(result textselect.Result, wrap func(textselect.CopyNotice) tea.Msg) tea.Cmd {
	if m == nil || !result.CopyAsked {
		return nil
	}
	return m.selection.Keys.CopySelectionCmd(result.Copy, wrap)
}

// expireSelection drops a selection whose rows are about to be replaced. A
// selection names visual rows, and everything in the key below changes which
// text is on which row: a new reference, a resolve that landed, a width that
// re-wraps the body, a theme that re-renders it.
func (m *Model) expireSelection() {
	key := fmt.Sprintf("%d|%d|%d|%d|%s|%s", m.width, m.height, m.state, m.generation,
		m.ref.Instance+"\x00"+m.ref.Locator, m.renderer.StyleKey())
	if m.selectionKey == key {
		return
	}
	m.selectionKey = key
	m.selection.Clear()
}

// resourceSource is the card as the selection engine reads it.
type resourceSource struct{ m *Model }

var _ textselect.Source = resourceSource{}

func (s resourceSource) ContentRect() mouse.Rect {
	m := s.m
	if m.width <= 0 || m.height <= 0 {
		return mouse.Rect{}
	}
	return mouse.Rect{X: m.originX, Y: m.originY, W: m.width, H: m.height}
}

// Line is the row as it reaches the screen: held to the drawn width the way
// ui.FitBlock holds it, so the columns the selection names are the columns it
// was drawn at.
func (s resourceSource) Line(i int) string {
	rows := s.m.lines()
	if i < 0 || i >= len(rows) {
		return ""
	}
	return ansi.Truncate(rows[i], s.m.width, "")
}

func (s resourceSource) LineCount() int { return len(s.m.lines()) }

func (s resourceSource) Scroll() int { return s.m.scroll }

// TabWidth is zero because the card holds no tabs: every row is built from
// wrapped, styled cells in the column space it is drawn in.
func (s resourceSource) TabWidth() int { return 0 }
