package workspacediff

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

// selectionOrigin is where the fake host draws the pane: somewhere that is
// neither the screen's corner nor the leaf's, so a coordinate that forgot to
// account for either shows up as a miss.
const selectionOriginX, selectionOriginY = 10, 5

const selectionWidth, selectionHeight = 80, 12

// selectablePane is a diff pane with a patch on screen, drawn at a known origin
// with the chords a host binds.
func selectablePane(t *testing.T) *View {
	t.Helper()
	v := &View{
		State: LoadStateReady,
		Focus: FocusDiff,
		Files: []File{{Path: "a.go", Raw: "@@ -1,3 +1,3 @@\n" +
			"-alpha removed\n" +
			"+bravo added\n" +
			" charlie context\n" +
			// Enough tail for the pane to have somewhere to scroll to, which is
			// what makes the frame-drops-the-selection case reachable.
			strings.Repeat(" delta filler\n", 40)}},
	}
	v.SetSize(selectionWidth, selectionHeight)
	v.SetOrigin(selectionOriginX, selectionOriginY)
	v.SetSelection(textselect.Keys{Copy: "alt+c", SelectAll: "ctrl+a"}, false)
	v.Render(selectionWidth, selectionHeight, RenderOpts{})
	return v
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

// paneRow is the screen row and the first text column of a drawn row holding
// text.
func paneRow(t *testing.T, v *View, text string) (int, int) {
	t.Helper()
	for i, row := range v.frameRows {
		plain := ansi.Strip(row)
		idx := strings.Index(plain, text)
		if idx < 0 {
			continue
		}
		return selectionOriginX + idx, selectionOriginY + i
	}
	t.Fatalf("no drawn row holds %q; frame is\n%s", text, strings.Join(v.frameRows, "\n"))
	return 0, 0
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

// A drag over two of the patch's rows selects exactly the text they carry, and
// the copy chord puts it on both clipboards through the shared pipeline.
func TestADragOverTheDiffSelectsTwoRowsAndCopiesThem(t *testing.T) {
	v := selectablePane(t)
	x, top := paneRow(t, v, "alpha removed")
	_, bottom := paneRow(t, v, "bravo added")
	if bottom != top+1 {
		t.Fatalf("the two patch rows are at %d and %d, want them adjacent", top, bottom)
	}

	v.HandleSelectionMouse(selectPress(x, top))
	v.HandleSelectionMouse(selectDrag(x+len("bravo added"), bottom))
	v.HandleSelectionMouse(selectRelease(x+len("bravo added"), bottom))

	lines := v.SelectionText()
	if len(lines) != 2 {
		t.Fatalf("selected %d rows (%q), want the two the drag covered", len(lines), lines)
	}
	if !strings.Contains(ansi.Strip(lines[0]), "alpha removed") {
		t.Errorf("first selected row = %q, want the row the drag started on", ansi.Strip(lines[0]))
	}

	copied := v.HandleSelectionKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
	if !copied.Handled || !copied.CopyAsked {
		t.Fatalf("alt+c = %#v, want a copy", copied)
	}
	text := clipboardText(t, v.SelectionCopyCmd(copied, func(textselect.CopyNotice) tea.Msg { return nil }))
	if !strings.Contains(text, "alpha removed") || !strings.Contains(text, "bravo added") {
		t.Errorf("clipboard = %q, want both selected rows", text)
	}
}

// A double click selects the word under the pointer.
func TestDoubleClickSelectsAWordInTheDiff(t *testing.T) {
	v := selectablePane(t)
	x, y := paneRow(t, v, "charlie context")

	v.HandleSelectionMouse(selectDoubleClick(x+1, y))

	lines := v.SelectionText()
	if len(lines) != 1 {
		t.Fatalf("a double click selected %d rows (%q), want one word", len(lines), lines)
	}
	if got := ansi.Strip(lines[0]); got != "charlie" {
		t.Errorf("double click selected %q, want the word under the pointer", got)
	}
}

// ctrl+a selects the whole frame, which is the key the empty-copy notice names.
func TestSelectAllCoversTheWholeDiffFrame(t *testing.T) {
	v := selectablePane(t)
	if result := v.HandleSelectionKey(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}); !result.Changed {
		t.Fatal("select-all selected nothing")
	}
	text := strings.Join(v.SelectionText(), "\n")
	if !strings.Contains(text, "alpha removed") || !strings.Contains(text, "charlie context") {
		t.Fatalf("select-all covered %q, want every row on screen", text)
	}
}

// The frame is the coordinate space, so a frame showing something else drops
// the selection rather than leaving a highlight over rows it never covered.
func TestScrollingTheDiffDropsTheSelection(t *testing.T) {
	v := selectablePane(t)
	x, y := paneRow(t, v, "alpha removed")
	v.HandleSelectionMouse(selectPress(x, y))
	v.HandleSelectionMouse(selectDrag(x+6, y))
	if !v.HasSelection() {
		t.Fatal("the drag selected nothing to drop")
	}

	v.ScrollContent(1, selectionHeight)
	v.Render(selectionWidth, selectionHeight, RenderOpts{})
	if v.HasSelection() {
		t.Fatal("the selection outlived the frame it was made over")
	}
}

// A drag past the bottom edge does not scroll the pane: the rows it would
// reveal cannot be selected without dropping the selection that asked for them.
func TestADragPastTheEdgeDoesNotScrollTheDiff(t *testing.T) {
	v := selectablePane(t)
	x, y := paneRow(t, v, "alpha removed")
	before := v.DiffScroll

	v.HandleSelectionMouse(selectPress(x, y))
	result := v.HandleSelectionMouse(selectDrag(x, selectionOriginY+selectionHeight+4))
	if result.AutoScroll != 0 {
		t.Fatalf("the drag asked the host for %d rows of scroll", result.AutoScroll)
	}
	if v.DiffScroll != before {
		t.Fatalf("the drag scrolled the pane to %d", v.DiffScroll)
	}
}

// A pane nobody has touched paints no highlight. An untouched selection state
// reads as a one-cell selection at the top-left corner.
func TestAnUntouchedDiffPanePaintsNoHighlight(t *testing.T) {
	v := selectablePane(t)
	frame := v.Render(selectionWidth, selectionHeight, RenderOpts{})
	if strings.Contains(frame, ui.GetSelectionBgANSI()) {
		t.Fatalf("a pane with no selection drew a highlight:\n%q", frame)
	}
}

// The highlight is painted onto the rows the frame draws.
func TestTheDiffPanePaintsTheSelectionItHolds(t *testing.T) {
	v := selectablePane(t)
	x, y := paneRow(t, v, "alpha removed")
	v.HandleSelectionMouse(selectPress(x, y))
	v.HandleSelectionMouse(selectDrag(x+6, y))

	rows := strings.Split(v.Render(selectionWidth, selectionHeight, RenderOpts{}), "\n")
	row := y - selectionOriginY
	if row >= len(rows) {
		t.Fatalf("the frame has %d rows, no row %d", len(rows), row)
	}
	if !strings.Contains(rows[row], ui.GetSelectionBgANSI()) {
		t.Fatalf("the selected row carries no highlight:\n%q", rows[row])
	}
}
