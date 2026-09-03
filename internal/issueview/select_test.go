package issueview

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/textselect"
	"github.com/marcus/sidecar/internal/ui"
)

// selectionOrigin is where the fake host draws the card: somewhere that is
// neither the screen's corner nor the pane's, so a coordinate that forgot to
// account for either shows up as a miss.
const selectionOriginX, selectionOriginY = 10, 5

// selectableCard is an issue card drawn at a known origin with the chords a
// host binds, and one frame painted so its hits are answerable.
func selectableCard(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetData(sample())
	m.SetSize(80, 24)
	m.SetOrigin(selectionOriginX, selectionOriginY)
	m.SetSelection(textselect.Keys{Copy: "alt+c", SelectAll: "ctrl+a"}, false)
	m.View()
	return m
}

func selectPress(x, y int) mouse.MouseAction {
	return mouse.MouseAction{Type: mouse.ActionClick, X: x, Y: y}
}

func selectDoubleClick(x, y int) mouse.MouseAction {
	return mouse.MouseAction{Type: mouse.ActionDoubleClick, X: x, Y: y}
}

func selectDrag(x, y int) mouse.MouseAction {
	return mouse.MouseAction{Type: mouse.ActionDrag, X: x, Y: y}
}

func selectRelease(x, y int) mouse.MouseAction {
	return mouse.MouseAction{Type: mouse.ActionDragEnd, X: x, Y: y}
}

// cardRowY is the screen row a card line containing text is drawn on.
func cardRowY(t *testing.T, m *Model, text string) int {
	t.Helper()
	for i, r := range m.ensureRows() {
		if !strings.Contains(ansi.Strip(r.text), text) {
			continue
		}
		y := selectionOriginY + i - m.scroll
		if y < selectionOriginY || y >= selectionOriginY+m.height {
			t.Fatalf("the row holding %q is scrolled out of the card", text)
		}
		return y
	}
	t.Fatalf("no card row holds %q", text)
	return 0
}

// clipboardText runs the OSC 52 half of the two-step command clip.Copy builds
// and reports what it wrote. Only that half is run: the other one reaches the
// developer's own system clipboard, which a test must not touch.
func clipboardText(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("the copy produced no command")
	}
	value := reflect.ValueOf(cmd())
	if value.Kind() != reflect.Slice || value.Type().Elem() != reflect.TypeOf(tea.Cmd(nil)) {
		t.Fatalf("the copy produced %T, want the sequence clip.Copy builds", cmd())
	}
	if value.Len() != 2 {
		t.Fatalf("the copy sequence has %d steps, want the OSC 52 write and the native one", value.Len())
	}
	osc, _ := value.Index(0).Interface().(tea.Cmd)
	if osc == nil {
		t.Fatal("the copy sequence has no OSC 52 step")
	}
	written := reflect.ValueOf(osc())
	if written.Kind() != reflect.String {
		t.Fatalf("the OSC 52 step wrote %T, want the selected text", osc())
	}
	return written.String()
}

func contentX(m *Model, col int) int { return selectionOriginX + m.leftPadding() + col }

// A drag over two of the card's rows selects exactly the text they carry, and
// the copy chord puts it on both clipboards through the shared pipeline.
func TestADragOverTheCardSelectsTwoRowsAndCopiesThem(t *testing.T) {
	m := selectableCard(t)
	top := cardRowY(t, m, "Labels:")
	bottom := top + 1

	m.HandleSelectionMouse(selectPress(contentX(m, 0), top))
	m.HandleSelectionMouse(selectDrag(contentX(m, m.contentWidth()-1), bottom))
	m.HandleSelectionMouse(selectRelease(contentX(m, m.contentWidth()-1), bottom))

	lines := m.SelectionText()
	if len(lines) != 2 {
		t.Fatalf("selected %d rows (%q), want the two the drag covered", len(lines), lines)
	}
	if !strings.Contains(ansi.Strip(lines[0]), "Labels:") {
		t.Errorf("first selected row = %q, want the row the drag started on", ansi.Strip(lines[0]))
	}

	copied := m.HandleSelectionKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
	if !copied.Handled || !copied.CopyAsked {
		t.Fatalf("alt+c = %#v, want a copy", copied)
	}
	text := clipboardText(t, m.SelectionCopyCmd(copied, func(textselect.CopyNotice) tea.Msg { return nil }))
	if !strings.Contains(text, "Labels:") {
		t.Errorf("clipboard = %q, want the selected rows", text)
	}
}

// A double click selects the word under the pointer.
func TestDoubleClickSelectsAWordInTheCard(t *testing.T) {
	m := selectableCard(t)
	y := cardRowY(t, m, "Labels:")

	m.HandleSelectionMouse(selectDoubleClick(contentX(m, 1), y))

	lines := m.SelectionText()
	if len(lines) != 1 {
		t.Fatalf("a double click selected %d rows (%q), want one word", len(lines), lines)
	}
	// The engine's word runes include the punctuation that holds an identifier
	// together, so the label reads as one word rather than three.
	if got := ansi.Strip(lines[0]); got != "Labels:" {
		t.Errorf("double click selected %q, want the word under the pointer", got)
	}
}

// ctrl+a selects the whole card, which is the key the empty-copy notice names.
func TestSelectAllCoversTheWholeCard(t *testing.T) {
	m := selectableCard(t)
	if result := m.HandleSelectionKey(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}); !result.Changed {
		t.Fatal("select-all selected nothing")
	}
	text := strings.Join(m.SelectionText(), "\n")
	if !strings.Contains(text, "Labels:") || !strings.Contains(text, "SUBTASKS") {
		t.Fatalf("select-all covered %q, want every row of the card", text)
	}
}

// The card's own targets keep their clicks. A press on a subtask row or on the
// scrollbar is that row's gesture, not the start of a selection.
func TestTheCardsOwnTargetsAreNotSelectable(t *testing.T) {
	m := selectableCard(t)
	hits := m.Hits()
	if len(hits) == 0 {
		t.Fatal("the card drew no navigable rows, so there is nothing to protect")
	}
	if m.SelectableAt(hits[0].X, hits[0].Y) {
		t.Error("a press on a navigable row would arm a selection")
	}
	if !m.SelectableAt(m.leftPadding(), 1) {
		t.Error("a press on plain card text would not arm a selection")
	}
}

// A selection is dropped when the rows under it are replaced.
func TestANewIssueDropsTheCardSelection(t *testing.T) {
	m := selectableCard(t)
	y := cardRowY(t, m, "Labels:")
	m.HandleSelectionMouse(selectPress(contentX(m, 0), y))
	m.HandleSelectionMouse(selectDrag(contentX(m, 6), y))
	if !m.HasSelection() {
		t.Fatal("the drag selected nothing to drop")
	}

	next := sample()
	next.ID = "td-other1"
	m.SetData(next)
	if m.HasSelection() {
		t.Fatal("the selection outlived the issue it was made over")
	}
}

// A host that opens a modal, or moves focus, abandons the gesture through the
// same seam every other surface uses.
func TestAbandonEndsTheCardGesture(t *testing.T) {
	m := selectableCard(t)
	y := cardRowY(t, m, "Labels:")
	m.HandleSelectionMouse(selectPress(contentX(m, 0), y))
	m.HandleSelectionMouse(selectDrag(contentX(m, 6), y))

	if result := m.AbandonSelection(); !result.Handled {
		t.Fatal("abandoning a live gesture reported nothing to end")
	}
	m.ClearSelection()
	if m.HasSelection() {
		t.Fatal("the selection survived being cleared")
	}
	// The gesture is over: an unrelated drag does not extend it.
	m.HandleSelectionMouse(selectDrag(contentX(m, 20), y+2))
	if m.HasSelection() {
		t.Fatal("a drag after the gesture ended extended a selection the user had finished")
	}
}

// The highlight is painted onto the rows the frame draws.
func TestTheCardPaintsTheSelectionItHolds(t *testing.T) {
	m := selectableCard(t)
	y := cardRowY(t, m, "Labels:")
	m.HandleSelectionMouse(selectPress(contentX(m, 0), y))
	m.HandleSelectionMouse(selectDrag(contentX(m, 6), y))

	rows := strings.Split(m.View(), "\n")
	row := y - selectionOriginY
	if row >= len(rows) {
		t.Fatalf("the card has %d rows, no row %d", len(rows), row)
	}
	if !strings.Contains(rows[row], ui.GetSelectionBgANSI()) {
		t.Fatalf("the selected row carries no highlight:\n%q", rows[row])
	}
}
