package workspacediff

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/textselect"
)

// Text selection in a diff pane.
//
// The binding lives here, once, because one View is the Diff leaf in the
// project workspace, in the global Workspaces browser and in the app's content
// deck. A host says where its leaf was drawn and forwards the pointer and key
// events it already routes; what a gesture means, what is highlighted and what
// reaches the clipboard is decided here and inherited by all three.
//
// The coordinate space is the frame, not the diff. Every other selectable pane
// lays its whole content out and slices it, so a selection can name an absolute
// row and survive scrolling; this one cannot. The right-hand body is painted by
// the host (gitstatus' line-diff and side-by-side renderers reach this view
// through RenderOpts.PaintFile), one window at a time, with line-number gutters
// the raw patch does not carry — so the only rows this view can answer for are
// the rows it just drew. Line therefore reads the last painted frame, Scroll is
// zero, and a selection is dropped as soon as what is on screen changes.
//
// Two consequences are deliberate. A drag past an edge does not scroll the
// pane: the rows it would reveal cannot be selected without dropping the
// selection that asked for them, so the gesture is bounded by the window. And a
// selection covers whatever the frame drew at those cells — the file list as
// well as the patch — which is what a terminal's own selection does, and what
// makes a file's name as copyable as its hunks.

var _ textselect.Pane = (*View)(nil)

// SetSelection binds the host's shared selection settings: the chords the pane
// answers, and whether finishing a drag copies without being asked.
func (v *View) SetSelection(keys textselect.Keys, copyOnSelect bool) {
	if v == nil {
		return
	}
	v.selection.Keys = keys
	v.selection.CopyOnSelect = copyOnSelect
}

// SetOrigin records where the host last drew this pane's body, in the
// coordinate space its mouse events arrive in. A pane that has not been drawn
// has no origin, so it hit-tests as empty rather than as the top-left corner of
// the screen.
func (v *View) SetOrigin(x, y int) {
	if v == nil {
		return
	}
	v.originX, v.originY = x, y
}

// HasSelection reports whether anything in this pane is selected.
func (v *View) HasSelection() bool {
	if v == nil {
		return false
	}
	v.expireSelection()
	return v.selection.HasSelection()
}

// ClearSelection drops the selection and any gesture editing it.
func (v *View) ClearSelection() {
	if v == nil {
		return
	}
	v.selection.Clear()
}

// AbandonSelection ends a gesture whose release never arrived.
func (v *View) AbandonSelection() textselect.Result {
	if v == nil {
		return textselect.Result{}
	}
	return v.selection.Abandon()
}

// SelectionText is the selection as the user sees it: the rows it covers,
// without the styling they were drawn with.
func (v *View) SelectionText() []string {
	if v == nil {
		return nil
	}
	v.expireSelection()
	return v.selection.SelectedText(diffSource{v})
}

// HandleSelectionMouse advances the gesture over the pane and reports what the
// host owes for it. AutoScroll is dropped rather than applied: see the note at
// the top of this file.
func (v *View) HandleSelectionMouse(action mouse.MouseAction) textselect.Result {
	if v == nil {
		return textselect.Result{}
	}
	v.expireSelection()
	result := v.selection.HandleMouse(action, diffSource{v})
	result.AutoScroll = 0
	return result
}

// HandleSelectionKey answers the chords that act on the selection: copy,
// select-all, and escape.
func (v *View) HandleSelectionKey(msg tea.KeyMsg) textselect.Result {
	if v == nil {
		return textselect.Result{}
	}
	v.expireSelection()
	if press, ok := msg.(tea.KeyPressMsg); ok && press.String() == "esc" && v.selection.HasSelection() {
		v.selection.Clear()
		return textselect.Result{Handled: true, Changed: true}
	}
	return v.selection.HandleKey(msg, diffSource{v})
}

// SelectionCopyCmd delivers the copy an engine result asked for, phrased by the
// shared pipeline and wrapped in whatever notification type the host uses.
func (v *View) SelectionCopyCmd(result textselect.Result, wrap func(textselect.CopyNotice) tea.Msg) tea.Cmd {
	if v == nil || !result.CopyAsked {
		return nil
	}
	return v.selection.Keys.CopySelectionCmd(result.Copy, wrap)
}

// paintSelection records the frame the pane just drew and paints the selection
// onto it. It is the last thing Render does, so what a gesture reads and what
// the reader sees are the same rows.
func (v *View) paintSelection(frame string, width, height int) string {
	if v == nil {
		return frame
	}
	v.frameW, v.frameH = width, height
	v.frameRows = strings.Split(frame, "\n")
	v.expireSelection()
	if !v.selection.HasSelection() {
		return frame
	}
	painted := make([]string, len(v.frameRows))
	for i, row := range v.frameRows {
		painted[i] = v.selection.DecorateRow(row, i)
	}
	return strings.Join(painted, "\n")
}

// expireSelection drops a selection whose rows are about to be replaced.
//
// The key is what the frame is of, not the bytes it came out as: hover on the
// divider and the cursor's own highlight change the bytes every frame, and a
// selection that died on a mouse move would be unusable. Everything that
// changes which text is on which row is in it.
func (v *View) expireSelection() {
	key := fmt.Sprintf("%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%s|%s",
		v.frameW, v.frameH, v.State, v.Scope, v.Focus, v.ViewMode,
		v.Cursor, v.CommitFileCursor, v.DiffScroll, v.HorizScroll, v.CommitFileScroll,
		v.Target.TabLabel(), v.selectedFileName())
	if v.selectionKey == key {
		return
	}
	v.selectionKey = key
	v.selection.Clear()
}

// diffSource is the frame as the selection engine reads it.
type diffSource struct{ v *View }

var _ textselect.Source = diffSource{}

func (s diffSource) ContentRect() mouse.Rect {
	v := s.v
	if v.frameW <= 0 || len(v.frameRows) == 0 {
		return mouse.Rect{}
	}
	return mouse.Rect{X: v.originX, Y: v.originY, W: v.frameW, H: len(v.frameRows)}
}

// Line is the drawn row, held to the drawn width so a selection cannot name
// columns the frame truncated away.
func (s diffSource) Line(i int) string {
	if i < 0 || i >= len(s.v.frameRows) {
		return ""
	}
	return ansi.Truncate(s.v.frameRows[i], s.v.frameW, "")
}

func (s diffSource) LineCount() int { return len(s.v.frameRows) }

// Scroll is zero because the rows this source holds are the rows on screen: the
// frame is the coordinate space.
func (s diffSource) Scroll() int { return 0 }

// TabWidth is zero because the rows hold no tabs: every renderer that reaches
// this frame expanded them in the column space they are drawn in.
func (s diffSource) TabWidth() int { return 0 }
