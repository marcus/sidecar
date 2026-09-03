package pluginbrowser

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/textselect"
	"github.com/marcus/sidecar/internal/ui"
)

// selectableModel is a browser with a document open in the detail box and the
// chords a host binds, which is the state every gesture below acts on.
func selectableModel(t *testing.T, doc resource.Document) *Model {
	t.Helper()
	host := &fakeHost{page: testPage(3), doc: doc}
	m := loadedModel(t, host)
	m.SetSelection(textselect.Keys{Copy: "alt+c", SelectAll: "ctrl+a"}, false)
	press(t, m, "enter")
	m.View()
	if !m.detail.loaded {
		t.Fatal("no document reached the detail box")
	}
	return m
}

// selectionDoc is a card whose rows are two known words, so what a drag covers
// can be named rather than matched by shape.
func selectionDoc() resource.Document {
	return resource.Document{
		Identity: "rc:notes:1",
		Title:    "Fixture row",
		Fields: []resource.Field{
			{Label: "alpha", Value: "alphavalue"},
			{Label: "bravo", Value: "bravovalue"},
		},
	}
}

// detailRowY is the screen row a card line containing text is drawn on.
func detailRowY(t *testing.T, m *Model, text string) int {
	t.Helper()
	rect := m.detailContentRect()
	rows := m.detailRows()
	for i, row := range rows {
		if !strings.Contains(ansi.Strip(row), text) {
			continue
		}
		y := rect.Y + i - m.detail.scroll
		if y < rect.Y || y >= rect.Y+rect.H {
			t.Fatalf("the row holding %q is scrolled out of the box", text)
		}
		return y
	}
	t.Fatalf("no card row holds %q", text)
	return 0
}

func mouseAt(kind string, x, y int) tea.MouseMsg {
	switch kind {
	case "press":
		return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
	case "drag":
		return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
	default:
		return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
	}
}

// clipboardText runs the OSC 52 half of the two-step command clip.Copy builds
// and reports what it wrote. Only that half is run: the other one reaches the
// developer's own system clipboard, which a test must not touch. A command of
// any other shape is not clip.Copy's and fails here.
func clipboardText(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("the copy chord produced no command")
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
	// tea.SetClipboard's message type is unexported and its underlying type is
	// the text, which is the only thing this needs to read.
	written := reflect.ValueOf(osc())
	if written.Kind() != reflect.String {
		t.Fatalf("the OSC 52 step wrote %T, want the selected text", osc())
	}
	return written.String()
}

// A drag over two of the card's rows selects exactly the text they carry, and
// the copy chord puts it on both clipboards through the shared pipeline.
func TestADragOverTheDetailSelectsTwoRowsAndCopiesThem(t *testing.T) {
	m := selectableModel(t, selectionDoc())
	rect := m.detailContentRect()
	top := detailRowY(t, m, "alpha")
	bottom := detailRowY(t, m, "bravo")
	if bottom <= top {
		t.Fatalf("the two rows are drawn at %d and %d, want one above the other", top, bottom)
	}

	m.HandleMouse(mouseAt("press", rect.X, top))
	m.HandleMouse(mouseAt("drag", rect.X+rect.W-1, bottom))
	m.HandleMouse(mouseAt("release", rect.X+rect.W-1, bottom))

	lines := m.SelectionText()
	if len(lines) != bottom-top+1 {
		t.Fatalf("selected %d rows (%q), want the %d the drag covered", len(lines), lines, bottom-top+1)
	}
	if !strings.Contains(ansi.Strip(lines[0]), "alpha") {
		t.Errorf("first selected row = %q, want the row the drag started on", ansi.Strip(lines[0]))
	}
	if last := ansi.Strip(lines[len(lines)-1]); !strings.Contains(last, "bravo") {
		t.Errorf("last selected row = %q, want the row the drag ended on", last)
	}

	cmd, claimed := m.HandleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
	if !claimed {
		t.Fatal("alt+c was not claimed by the detail box")
	}
	copied := clipboardText(t, cmd)
	if !strings.Contains(copied, "alpha") || !strings.Contains(copied, "bravo") {
		t.Errorf("clipboard = %q, want both selected rows", copied)
	}
}

// A double click selects the word under the pointer, not the row and not a
// single cell.
func TestDoubleClickSelectsAWordInTheDetail(t *testing.T) {
	m := selectableModel(t, selectionDoc())
	rect := m.detailContentRect()
	y := detailRowY(t, m, "alpha")

	m.HandleMouse(mouseAt("press", rect.X+1, y))
	m.HandleMouse(mouseAt("press", rect.X+1, y))

	lines := m.SelectionText()
	if len(lines) != 1 {
		t.Fatalf("a double click selected %d rows (%q), want one word", len(lines), lines)
	}
	if got := ansi.Strip(lines[0]); got != "alpha" {
		t.Errorf("double click selected %q, want the word under the pointer", got)
	}
}

// ctrl+a selects the whole card, which is the key the empty-copy notice names.
func TestSelectAllSelectsTheWholeCard(t *testing.T) {
	m := selectableModel(t, selectionDoc())
	m.SetPaneFocus(string(FocusDetail))

	cmd, claimed := m.HandleKey(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if !claimed {
		t.Fatal("ctrl+a was not claimed by the detail box")
	}
	_ = cmd
	text := strings.Join(m.SelectionText(), "\n")
	if !strings.Contains(text, "Fixture row") || !strings.Contains(text, "bravovalue") {
		t.Fatalf("select-all covered %q, want every row of the card", text)
	}
}

// A modal takes the keyboard, so the selection under it goes: a highlight
// nothing on screen still acts on, and a gesture that would answer the next
// unrelated drag as an extension of it.
func TestOpeningAModalAbandonsTheDetailSelection(t *testing.T) {
	m := selectableModel(t, selectionDoc())
	rect := m.detailContentRect()
	y := detailRowY(t, m, "alpha")
	m.HandleMouse(mouseAt("press", rect.X, y))
	m.HandleMouse(mouseAt("drag", rect.X+6, y))
	if !m.HasSelection() {
		t.Fatal("the drag selected nothing to abandon")
	}

	press(t, m, "v")
	if !m.OverlayOpen() {
		t.Fatal("the View modal did not open")
	}
	if m.HasSelection() {
		t.Fatal("the selection survived the modal that took the keyboard")
	}
}

// The rows a selection names are the card's own. A different document is a
// different set of rows, so the selection goes with the card it was made on.
func TestANewDocumentDropsTheDetailSelection(t *testing.T) {
	m := selectableModel(t, selectionDoc())
	rect := m.detailContentRect()
	y := detailRowY(t, m, "alpha")
	m.HandleMouse(mouseAt("press", rect.X, y))
	m.HandleMouse(mouseAt("drag", rect.X+6, y))
	m.HandleMouse(mouseAt("release", rect.X+6, y))
	if !m.HasSelection() {
		t.Fatal("the drag selected nothing to drop")
	}

	m.detail.doc = resource.Document{Identity: "rc:notes:2", Title: "Another row"}
	m.detail.shownID = "rc:notes:2"
	m.detail.generation++
	m.View()
	if m.HasSelection() {
		t.Fatal("the selection outlived the document it was made over")
	}
}

// The press that arms a selection is still the click that focuses the box. A
// release without motion resolves to a click-through, and the browser's own
// answer to that click — moving the keyboard here — already happened.
func TestTheFocusClickStillFocusesTheDetail(t *testing.T) {
	m := selectableModel(t, selectionDoc())
	m.SetPaneFocus(string(FocusList))
	if m.PaneFocus() != string(FocusList) {
		t.Fatal("the keyboard did not start in the list")
	}
	rect := m.detailContentRect()
	y := detailRowY(t, m, "alpha")

	m.HandleMouse(mouseAt("press", rect.X+2, y))
	m.HandleMouse(mouseAt("release", rect.X+2, y))

	if m.PaneFocus() != string(FocusDetail) {
		t.Fatalf("pane focus = %q, want the box the click landed in", m.PaneFocus())
	}
	if m.HasSelection() {
		t.Fatal("a click that never moved left a selection behind")
	}
}

// The highlight is painted onto the rows the frame draws, at the columns the
// selection names.
func TestTheDetailPaintsTheSelectionItHolds(t *testing.T) {
	m := selectableModel(t, selectionDoc())
	rect := m.detailContentRect()
	y := detailRowY(t, m, "alpha")
	m.HandleMouse(mouseAt("press", rect.X, y))
	m.HandleMouse(mouseAt("drag", rect.X+6, y))

	rows := strings.Split(m.View(), "\n")
	if y >= len(rows) {
		t.Fatalf("the frame has %d rows, no row %d", len(rows), y)
	}
	if !strings.Contains(rows[y], ui.GetSelectionBgANSI()) {
		t.Fatalf("the selected row carries no highlight:\n%q", rows[y])
	}
}

// A press on the list beside the detail is not a selection: the table's rows
// are targets, and a drag over them is the row gesture the pointer model owns.
func TestTheListIsNotSelectable(t *testing.T) {
	m := selectableModel(t, selectionDoc())
	row := rowRegion(t, m, 1)
	m.HandleMouse(mouseAt("press", row.Rect.X, row.Rect.Y))
	m.HandleMouse(mouseAt("drag", row.Rect.X+6, row.Rect.Y))
	if m.HasSelection() {
		t.Fatal("a drag over the table selected text")
	}
}

// A pane-mode browser showing a document is the same selectable box, at the
// pane's own geometry rather than the split's.
func TestAPaneModeDocumentIsSelectable(t *testing.T) {
	host := &fakeHost{page: testPage(3), doc: selectionDoc()}
	m := loadedModel(t, host)
	m.SetSelection(textselect.Keys{Copy: "alt+c", SelectAll: "ctrl+a"}, false)
	press(t, m, "enter")
	m.paneShape = PaneDocument
	m.focus = FocusDetail
	m.SetSize(80, 20)
	m.View()

	rect := m.detailContentRect()
	if rect.W <= 0 {
		t.Fatal("a pane-mode document box reports no content rect")
	}
	y := detailRowY(t, m, "alpha")
	m.HandleMouse(mouseAt("press", rect.X, y))
	m.HandleMouse(mouseAt("drag", rect.X+6, y))
	if !m.HasSelection() {
		t.Fatal("a drag over a pane-mode document selected nothing")
	}
}
