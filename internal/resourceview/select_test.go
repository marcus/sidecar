package resourceview

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/textselect"
	"github.com/marcus/sidecar/internal/ui"
)

// selectionOrigin is where the fake host draws the card: somewhere that is
// neither the screen's corner nor the pane's, so a coordinate that forgot to
// account for either shows up as a miss.
const selectionOriginX, selectionOriginY = 10, 5

// selectableCard is a resolved resource card drawn at a known origin with the
// chords a host binds.
func selectableCard(t *testing.T) *Model {
	t.Helper()
	rec := &recorder{}
	m := New(nil, rec.resolver())
	m.SetSize(60, 12)
	m.Load(1, ref("CASH-1"), 0)
	modelID, generation, _ := rec.last()
	m.Apply(ResolvedMsg{ModelID: modelID, Generation: generation, Ref: ref("CASH-1"), Document: resource.Document{
		Identity: "CASH-1",
		Title:    "Alpha title",
		Fields: []resource.Field{
			{Label: "bravo", Value: "bravovalue"},
			{Label: "charlie", Value: "charlievalue"},
		},
	}})
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
	for i, line := range m.lines() {
		if !strings.Contains(ansi.Strip(line), text) {
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

// A drag over two of the card's rows selects exactly the text they carry, and
// the copy chord puts it on both clipboards through the shared pipeline.
func TestADragOverTheResourceCardSelectsTwoRowsAndCopiesThem(t *testing.T) {
	m := selectableCard(t)
	top := cardRowY(t, m, "bravo")
	bottom := cardRowY(t, m, "charlie")
	if bottom != top+1 {
		t.Fatalf("the two field rows are at %d and %d, want them adjacent", top, bottom)
	}

	m.HandleSelectionMouse(selectPress(selectionOriginX, top))
	m.HandleSelectionMouse(selectDrag(selectionOriginX+m.width-1, bottom))
	m.HandleSelectionMouse(selectRelease(selectionOriginX+m.width-1, bottom))

	lines := m.SelectionText()
	if len(lines) != 2 {
		t.Fatalf("selected %d rows (%q), want the two the drag covered", len(lines), lines)
	}
	if !strings.Contains(ansi.Strip(lines[0]), "bravo") {
		t.Errorf("first selected row = %q, want the row the drag started on", ansi.Strip(lines[0]))
	}

	copied := m.HandleSelectionKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
	if !copied.Handled || !copied.CopyAsked {
		t.Fatalf("alt+c = %#v, want a copy", copied)
	}
	text := clipboardText(t, m.SelectionCopyCmd(copied, func(textselect.CopyNotice) tea.Msg { return nil }))
	if !strings.Contains(text, "bravo") || !strings.Contains(text, "charlie") {
		t.Errorf("clipboard = %q, want both selected rows", text)
	}
}

// A double click selects the word under the pointer.
func TestDoubleClickSelectsAWordInTheResourceCard(t *testing.T) {
	m := selectableCard(t)
	y := cardRowY(t, m, "bravo")

	m.HandleSelectionMouse(selectDoubleClick(selectionOriginX+1, y))

	lines := m.SelectionText()
	if len(lines) != 1 {
		t.Fatalf("a double click selected %d rows (%q), want one word", len(lines), lines)
	}
	if got := ansi.Strip(lines[0]); got != "bravo" {
		t.Errorf("double click selected %q, want the word under the pointer", got)
	}
}

// ctrl+a selects the whole card, which is the key the empty-copy notice names.
func TestSelectAllCoversTheWholeResourceCard(t *testing.T) {
	m := selectableCard(t)
	if result := m.HandleSelectionKey(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}); !result.Changed {
		t.Fatal("select-all selected nothing")
	}
	text := strings.Join(m.SelectionText(), "\n")
	if !strings.Contains(text, "Alpha title") || !strings.Contains(text, "charlievalue") {
		t.Fatalf("select-all covered %q, want every row of the card", text)
	}
}

// The rows a selection names are the card's own. A new document is a new set of
// rows, so the selection goes with the card it was made on.
func TestANewDocumentDropsTheResourceSelection(t *testing.T) {
	m := selectableCard(t)
	y := cardRowY(t, m, "bravo")
	m.HandleSelectionMouse(selectPress(selectionOriginX, y))
	m.HandleSelectionMouse(selectDrag(selectionOriginX+6, y))
	if !m.HasSelection() {
		t.Fatal("the drag selected nothing to drop")
	}

	rec := &recorder{}
	m.SetResolver(rec.resolver())
	m.Load(1, ref("CASH-2"), 0)
	if m.HasSelection() {
		t.Fatal("the selection outlived the reference it was made over")
	}
}

// A card nobody has touched paints no highlight. An untouched selection state
// reads as a one-cell selection at the top-left corner, which is exactly the
// cell a card's title starts at.
func TestAnUntouchedResourceCardPaintsNoHighlight(t *testing.T) {
	m := selectableCard(t)
	if strings.Contains(m.View(), ui.GetSelectionBgANSI()) {
		t.Fatalf("a card with no selection drew a highlight:\n%q", m.View())
	}
}

// The highlight is painted onto the rows the frame draws.
func TestTheResourceCardPaintsTheSelectionItHolds(t *testing.T) {
	m := selectableCard(t)
	y := cardRowY(t, m, "bravo")
	m.HandleSelectionMouse(selectPress(selectionOriginX, y))
	m.HandleSelectionMouse(selectDrag(selectionOriginX+6, y))

	rows := strings.Split(m.View(), "\n")
	row := y - selectionOriginY
	if row >= len(rows) {
		t.Fatalf("the card has %d rows, no row %d", len(rows), row)
	}
	if !strings.Contains(rows[row], ui.GetSelectionBgANSI()) {
		t.Fatalf("the selected row carries no highlight:\n%q", rows[row])
	}
}
