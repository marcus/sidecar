package workspace

import (
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/ui"
)

// newSelectionTestPlugin creates a Plugin with interactive state configured for selection tests.
// The pane starts at Y=2, has 1 row of content offset (tab bar), and shows lines 0-9.
func newSelectionTestPlugin() *Plugin {
	p := &Plugin{
		viewMode:     ViewModeInteractive,
		mouseHandler: mouse.NewHandler(),
		interactiveState: &InteractiveState{
			Active:           true,
			VisibleStart:     0,
			VisibleEnd:       10,
			ContentRowOffset: 1,
		},
	}
	p.selection.Clear() // initialize sentinels
	return p
}

// actionAt creates a mouse action at the given content column with the preview pane region.
// The x parameter is a content column; panelOverhead/2 is added to simulate the viewport
// X coordinate (accounting for left border + left padding).
func actionAt(x, y int) mouse.MouseAction {
	return mouse.MouseAction{
		Type: mouse.ActionClick,
		X:    x + panelOverhead/2,
		Y:    y,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
}

func TestPrepareInteractiveDrag_NoSelection(t *testing.T) {
	p := newSelectionTestPlugin()

	// Y=6: contentRow = 6-2-1 = 3, outputRow = 3-1 = 2, lineIdx = 0+2 = 2
	action := actionAt(10, 6)
	p.prepareInteractiveDrag(action)

	if p.selection.HasSelection() {
		t.Error("click without drag should not create selection")
	}
	if p.selection.Start.Valid() {
		t.Errorf("start should be invalid, got %+v", p.selection.Start)
	}
	if p.selection.End.Valid() {
		t.Errorf("end should be invalid, got %+v", p.selection.End)
	}
	if p.selection.Anchor.Line != 2 {
		t.Errorf("anchor line should be 2, got %d", p.selection.Anchor.Line)
	}
}

func TestDragAfterClick_CreatesSelection(t *testing.T) {
	p := newSelectionTestPlugin()

	// Click at line 2 (Y=6)
	action := actionAt(10, 6)
	p.prepareInteractiveDrag(action)

	// Drag to line 4 (Y=8)
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    10,
		Y:    8,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	if !p.selection.HasSelection() {
		t.Error("drag should create selection")
	}
	if !p.selection.Active {
		t.Error("selection should be active after drag")
	}
	if p.selection.Start.Line != 2 {
		t.Errorf("start line should be 2, got %d", p.selection.Start.Line)
	}
	if p.selection.End.Line != 4 {
		t.Errorf("end line should be 4, got %d", p.selection.End.Line)
	}
}

func TestDragUpward_FromAnchor(t *testing.T) {
	p := newSelectionTestPlugin()

	// Click at line 4 (Y=8)
	action := actionAt(10, 8)
	p.prepareInteractiveDrag(action)

	// Drag up to line 1 (Y=5)
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    10,
		Y:    5,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	if !p.selection.HasSelection() {
		t.Error("upward drag should create selection")
	}
	if p.selection.Start.Line != 1 {
		t.Errorf("start line should be 1, got %d", p.selection.Start.Line)
	}
	if p.selection.End.Line != 4 {
		t.Errorf("end line should be 4, got %d", p.selection.End.Line)
	}
}

func TestFinishInteractiveSelection_UnstartedClears(t *testing.T) {
	p := newSelectionTestPlugin()

	// Click without drag
	action := actionAt(10, 6)
	p.prepareInteractiveDrag(action)

	// Finish without any drag motion
	p.finishInteractiveSelection()

	if p.selection.HasSelection() {
		t.Error("finish without drag should not leave selection")
	}
	if p.selection.Start.Valid() {
		t.Errorf("start should be invalid after clear, got %+v", p.selection.Start)
	}
	if p.selection.Anchor.Valid() {
		t.Errorf("anchor should be invalid after clear, got %+v", p.selection.Anchor)
	}
}

func TestFinishInteractiveSelection_AfterDrag(t *testing.T) {
	p := newSelectionTestPlugin()

	// Click and drag
	action := actionAt(10, 6)
	p.prepareInteractiveDrag(action)
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    10,
		Y:    8,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	// Finish
	p.finishInteractiveSelection()

	// Selection should persist (active=false but range preserved)
	if !p.selection.HasSelection() {
		t.Error("selection range should persist after finish")
	}
	if p.selection.Active {
		t.Error("active should be false after finish")
	}
	if p.selection.Start.Line != 2 {
		t.Errorf("start line should be 2, got %d", p.selection.Start.Line)
	}
	if p.selection.End.Line != 4 {
		t.Errorf("end line should be 4, got %d", p.selection.End.Line)
	}
}

func TestClearInteractiveSelection_ResetsSentinels(t *testing.T) {
	p := newSelectionTestPlugin()

	// Create a valid selection
	action := actionAt(10, 6)
	p.prepareInteractiveDrag(action)
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    10,
		Y:    8,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	// Clear
	p.selection.Clear()

	if p.selection.Active {
		t.Error("active should be false after clear")
	}
	if p.selection.Start.Valid() {
		t.Errorf("start should be invalid, got %+v", p.selection.Start)
	}
	if p.selection.End.Valid() {
		t.Errorf("end should be invalid, got %+v", p.selection.End)
	}
	if p.selection.Anchor.Valid() {
		t.Errorf("anchor should be invalid, got %+v", p.selection.Anchor)
	}
	if p.selection.HasSelection() {
		t.Error("HasSelection should return false after clear")
	}
}

func TestDragToSameLine_SelectsSingleLine(t *testing.T) {
	p := newSelectionTestPlugin()
	buf := NewOutputBuffer(100)
	buf.Write("line0\nline1\nline2\nline three has enough text to test\nline4")
	p.shellSelected = true
	p.shells = []*ShellSession{{
		Agent: &Agent{OutputBuf: buf},
	}}
	p.selectedShellIdx = 0

	// Click at line 3 (Y=7: contentRow=7-2-1=4, outputRow=4-1=3, lineIdx=3)
	action := actionAt(10, 7)
	p.prepareInteractiveDrag(action)

	// Drag to same line (different X, same Y)
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    30,
		Y:    7,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	if !p.selection.HasSelection() {
		t.Error("drag to same line should create selection")
	}
	if p.selection.Start.Line != 3 {
		t.Errorf("start line should be 3, got %d", p.selection.Start.Line)
	}
	if p.selection.End.Line != 3 {
		t.Errorf("end line should be 3, got %d", p.selection.End.Line)
	}
	// Column should differ between start and end
	if p.selection.Start.Col == p.selection.End.Col {
		t.Error("start and end col should differ for same-line drag with different X")
	}
}

func TestPrepareInteractiveDrag_InvalidY(t *testing.T) {
	p := newSelectionTestPlugin()

	// Click above content area (Y=2 -> border row)
	action := actionAt(10, 2)
	p.prepareInteractiveDrag(action)

	if p.selection.Anchor.Valid() {
		t.Errorf("anchor should be invalid for invalid Y, got %+v", p.selection.Anchor)
	}
}

func TestPrepareInteractiveDrag_NilRegion(t *testing.T) {
	p := &Plugin{
		viewMode:     ViewModeInteractive,
		mouseHandler: mouse.NewHandler(),
		interactiveState: &InteractiveState{
			Active:           true,
			VisibleStart:     0,
			VisibleEnd:       10,
			ContentRowOffset: 1,
		},
	}
	p.selection.Clear()

	action := mouse.MouseAction{
		Type:   mouse.ActionClick,
		X:      10,
		Y:      6,
		Region: nil,
	}
	p.prepareInteractiveDrag(action)

	if p.selection.Anchor.Valid() {
		t.Errorf("anchor should remain invalid for nil region, got %+v", p.selection.Anchor)
	}
}

func TestIsLineSelected(t *testing.T) {
	p := newSelectionTestPlugin()

	// Set up selection range [3, 5]
	p.selection.Start = ui.SelectionPoint{Line: 3, Col: 0}
	p.selection.End = ui.SelectionPoint{Line: 5, Col: 10}

	tests := []struct {
		lineIdx  int
		expected bool
	}{
		{2, false},
		{3, true},
		{4, true},
		{5, true},
		{6, false},
	}

	for _, tt := range tests {
		got := p.selection.IsLineSelected(tt.lineIdx)
		if got != tt.expected {
			t.Errorf("IsLineSelected(%d) = %v, want %v", tt.lineIdx, got, tt.expected)
		}
	}
}

func TestHasSelection_Sentinels(t *testing.T) {
	p := newSelectionTestPlugin()

	// Default: sentinels
	if p.selection.HasSelection() {
		t.Error("should return false with sentinel values")
	}

	// Only start set
	p.selection.Start = ui.SelectionPoint{Line: 3, Col: 0}
	if p.selection.HasSelection() {
		t.Error("should return false with only start set")
	}

	// Both set
	p.selection.End = ui.SelectionPoint{Line: 5, Col: 10}
	if !p.selection.HasSelection() {
		t.Error("should return true with both start and end set")
	}
}

// --- GetLineSelectionCols tests ---

func TestGetLineSelectionCols(t *testing.T) {
	p := newSelectionTestPlugin()

	tests := []struct {
		name        string
		start, end  ui.SelectionPoint
		lineIdx     int
		expectStart int
		expectEnd   int
	}{
		{
			"line before selection",
			ui.SelectionPoint{Line: 3, Col: 5}, ui.SelectionPoint{Line: 6, Col: 10},
			2, -1, -1,
		},
		{
			"line after selection",
			ui.SelectionPoint{Line: 3, Col: 5}, ui.SelectionPoint{Line: 6, Col: 10},
			7, -1, -1,
		},
		{
			"first line of multi-line",
			ui.SelectionPoint{Line: 3, Col: 5}, ui.SelectionPoint{Line: 6, Col: 10},
			3, 5, -1,
		},
		{
			"middle line",
			ui.SelectionPoint{Line: 3, Col: 5}, ui.SelectionPoint{Line: 6, Col: 10},
			4, 0, -1,
		},
		{
			"last line of multi-line",
			ui.SelectionPoint{Line: 3, Col: 5}, ui.SelectionPoint{Line: 6, Col: 10},
			6, 0, 10,
		},
		{
			"single-line selection",
			ui.SelectionPoint{Line: 5, Col: 3}, ui.SelectionPoint{Line: 5, Col: 15},
			5, 3, 15,
		},
	}

	for _, tt := range tests {
		p.selection.Start = tt.start
		p.selection.End = tt.end
		startCol, endCol := p.selection.GetLineSelectionCols(tt.lineIdx)
		if startCol != tt.expectStart || endCol != tt.expectEnd {
			t.Errorf("%s: GetLineSelectionCols(%d) = (%d, %d), want (%d, %d)",
				tt.name, tt.lineIdx, startCol, endCol, tt.expectStart, tt.expectEnd)
		}
	}
}

// --- interactiveColAtX tests ---

func TestInteractiveColAtX_PlainText(t *testing.T) {
	p := newSelectionTestPlugin()
	buf := NewOutputBuffer(100)
	buf.Write("hello world")

	// Set up a shell with the buffer
	p.shellSelected = true
	p.shells = []*ShellSession{{
		Agent: &Agent{OutputBuf: buf},
	}}
	p.selectedShellIdx = 0
	p.selection.ViewRect = mouse.Rect{X: 0, Y: 2, W: 80, H: 12}

	col, ok := p.interactiveColAtX(5+panelOverhead/2, 0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if col != 5 {
		t.Errorf("plain text: col = %d, want 5", col)
	}
}

func TestInteractiveColAtX_BeyondLineEnd(t *testing.T) {
	p := newSelectionTestPlugin()
	buf := NewOutputBuffer(100)
	buf.Write("hello")

	p.shellSelected = true
	p.shells = []*ShellSession{{
		Agent: &Agent{OutputBuf: buf},
	}}
	p.selectedShellIdx = 0
	p.selection.ViewRect = mouse.Rect{X: 0, Y: 2, W: 80, H: 12}

	col, ok := p.interactiveColAtX(100+panelOverhead/2, 0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// Should clamp to last char (col 4)
	if col != 4 {
		t.Errorf("beyond end: col = %d, want 4", col)
	}
}

func TestInteractiveColAtX_WithHorizOffset(t *testing.T) {
	p := newSelectionTestPlugin()
	buf := NewOutputBuffer(100)
	buf.Write("hello world test")

	p.shellSelected = true
	p.shells = []*ShellSession{{
		Agent: &Agent{OutputBuf: buf},
	}}
	p.selectedShellIdx = 0
	p.selection.ViewRect = mouse.Rect{X: 0, Y: 2, W: 80, H: 12}

	// viewport X=4 (content col 2) -> visual col 2
	col, ok := p.interactiveColAtX(2+panelOverhead/2, 0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if col != 2 {
		t.Errorf("col = %d, want 2", col)
	}
}

func TestInteractiveColAtX_EmptyLine(t *testing.T) {
	p := newSelectionTestPlugin()
	buf := NewOutputBuffer(100)
	buf.Write("")

	p.shellSelected = true
	p.shells = []*ShellSession{{
		Agent: &Agent{OutputBuf: buf},
	}}
	p.selectedShellIdx = 0
	p.selection.ViewRect = mouse.Rect{X: 0, Y: 2, W: 80, H: 12}

	col, ok := p.interactiveColAtX(0+panelOverhead/2, 0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if col != 0 {
		t.Errorf("empty line: col = %d, want 0", col)
	}
}

// --- Character-level drag tests ---

func TestCharacterLevelDrag_SameLineRightward(t *testing.T) {
	p := newSelectionTestPlugin()
	buf := NewOutputBuffer(100)
	buf.Write("hello world")

	p.shellSelected = true
	p.shells = []*ShellSession{{
		Agent: &Agent{OutputBuf: buf},
	}}
	p.selectedShellIdx = 0

	// Click at col 5 on line 0 (Y=4: contentRow=4-2-1=1, outputRow=1-1=0, lineIdx=0)
	action := actionAt(5, 4)
	p.prepareInteractiveDrag(action)

	// Drag to col 10
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    10 + panelOverhead/2,
		Y:    4,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	if p.selection.Start.Line != 0 || p.selection.Start.Col != 5 {
		t.Errorf("start = %+v, want {0, 5}", p.selection.Start)
	}
	if p.selection.End.Line != 0 || p.selection.End.Col != 10 {
		t.Errorf("end = %+v, want {0, 10}", p.selection.End)
	}
}

func TestCharacterLevelDrag_SameLineBackward(t *testing.T) {
	p := newSelectionTestPlugin()
	buf := NewOutputBuffer(100)
	buf.Write("hello world")

	p.shellSelected = true
	p.shells = []*ShellSession{{
		Agent: &Agent{OutputBuf: buf},
	}}
	p.selectedShellIdx = 0

	// Click at col 10
	action := actionAt(10, 4)
	p.prepareInteractiveDrag(action)

	// Drag backward to col 3
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    3 + panelOverhead/2,
		Y:    4,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	// Start should be the lesser position
	if p.selection.Start.Col != 3 {
		t.Errorf("start col = %d, want 3", p.selection.Start.Col)
	}
	if p.selection.End.Col != 10 {
		t.Errorf("end col = %d, want 10", p.selection.End.Col)
	}
}

func TestCharacterLevelDrag_MultiLineDown(t *testing.T) {
	p := newSelectionTestPlugin()
	buf := NewOutputBuffer(100)
	buf.Write("line zero\nline one\nline two\nline three")

	p.shellSelected = true
	p.shells = []*ShellSession{{
		Agent: &Agent{OutputBuf: buf},
	}}
	p.selectedShellIdx = 0

	// Click at (5, line 1) -> Y=5: contentRow=5-2-1=2, outputRow=2-1=1, lineIdx=1
	action := actionAt(5, 5)
	p.prepareInteractiveDrag(action)

	// Drag to (3, line 3) -> Y=7
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    3 + panelOverhead/2,
		Y:    7,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	if p.selection.Start.Line != 1 || p.selection.Start.Col != 5 {
		t.Errorf("start = %+v, want {1, 5}", p.selection.Start)
	}
	if p.selection.End.Line != 3 || p.selection.End.Col != 3 {
		t.Errorf("end = %+v, want {3, 3}", p.selection.End)
	}
}

func TestCharacterLevelDrag_DirectionReversal(t *testing.T) {
	p := newSelectionTestPlugin()
	buf := NewOutputBuffer(100)
	buf.Write("abcdefghijklmnop")

	p.shellSelected = true
	p.shells = []*ShellSession{{
		Agent: &Agent{OutputBuf: buf},
	}}
	p.selectedShellIdx = 0

	// Click at col 8
	action := actionAt(8, 4)
	p.prepareInteractiveDrag(action)

	// Drag right to col 12
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    12 + panelOverhead/2,
		Y:    4,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	if p.selection.Start.Col != 8 || p.selection.End.Col != 12 {
		t.Errorf("after right drag: start.col=%d, end.col=%d, want 8, 12",
			p.selection.Start.Col, p.selection.End.Col)
	}

	// Now reverse past anchor to col 3
	dragAction.X = 3 + panelOverhead/2
	p.handleInteractiveSelectionDrag(dragAction)

	if p.selection.Start.Col != 3 || p.selection.End.Col != 8 {
		t.Errorf("after reversal: start.col=%d, end.col=%d, want 3, 8",
			p.selection.Start.Col, p.selection.End.Col)
	}
}

// --- interactiveSelectionLines integration test ---

func TestInteractiveSelectionLines_SingleLine(t *testing.T) {
	p := newSelectionTestPlugin()
	buf := NewOutputBuffer(100)
	buf.Write("hello world foo bar")
	p.shellSelected = true
	p.shells = []*ShellSession{{Agent: &Agent{OutputBuf: buf}}}
	p.selectedShellIdx = 0

	p.selection.Start = ui.SelectionPoint{Line: 0, Col: 6}
	p.selection.End = ui.SelectionPoint{Line: 0, Col: 10}

	lines := p.interactiveSelectionLines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "world") {
		t.Errorf("expected 'world' in selection, got %q", lines[0])
	}
}
