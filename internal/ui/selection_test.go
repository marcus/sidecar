package ui

import (
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/mouse"
)

// --- SelectionPoint tests ---

func TestSelectionPoint_Before(t *testing.T) {
	tests := []struct {
		name     string
		a, b     SelectionPoint
		expected bool
	}{
		{"same line, a before b", SelectionPoint{2, 5}, SelectionPoint{2, 10}, true},
		{"same line, a after b", SelectionPoint{2, 10}, SelectionPoint{2, 5}, false},
		{"same position", SelectionPoint{2, 5}, SelectionPoint{2, 5}, false},
		{"different lines, a before b", SelectionPoint{1, 99}, SelectionPoint{2, 0}, true},
		{"different lines, a after b", SelectionPoint{3, 0}, SelectionPoint{2, 99}, false},
	}

	for _, tt := range tests {
		got := tt.a.Before(tt.b)
		if got != tt.expected {
			t.Errorf("%s: (%+v).Before(%+v) = %v, want %v", tt.name, tt.a, tt.b, got, tt.expected)
		}
	}
}

func TestSelectionPoint_Valid(t *testing.T) {
	tests := []struct {
		name     string
		p        SelectionPoint
		expected bool
	}{
		{"both -1", SelectionPoint{-1, -1}, false},
		{"line -1", SelectionPoint{-1, 0}, false},
		{"col -1", SelectionPoint{0, -1}, false},
		{"both 0", SelectionPoint{0, 0}, true},
		{"positive", SelectionPoint{5, 10}, true},
	}

	for _, tt := range tests {
		got := tt.p.Valid()
		if got != tt.expected {
			t.Errorf("%s: %+v.Valid() = %v, want %v", tt.name, tt.p, got, tt.expected)
		}
	}
}

// --- SelectionState tests ---

func newTestState() *SelectionState {
	s := &SelectionState{}
	s.Clear()
	return s
}

func TestSelectionState_Clear(t *testing.T) {
	s := &SelectionState{
		Active:   true,
		Start:    SelectionPoint{3, 5},
		End:      SelectionPoint{6, 10},
		Anchor:   SelectionPoint{3, 5},
		ViewRect: mouse.Rect{X: 1, Y: 2, W: 80, H: 24},
	}
	s.Clear()

	if s.Active {
		t.Error("Active should be false after Clear")
	}
	if s.Start.Valid() {
		t.Errorf("Start should be invalid, got %+v", s.Start)
	}
	if s.End.Valid() {
		t.Errorf("End should be invalid, got %+v", s.End)
	}
	if s.Anchor.Valid() {
		t.Errorf("Anchor should be invalid, got %+v", s.Anchor)
	}
	if s.ViewRect != (mouse.Rect{}) {
		t.Errorf("ViewRect should be zero, got %+v", s.ViewRect)
	}
}

func TestSelectionState_HasSelection(t *testing.T) {
	s := newTestState()

	if s.HasSelection() {
		t.Error("should return false with sentinel values")
	}

	s.Start = SelectionPoint{3, 0}
	if s.HasSelection() {
		t.Error("should return false with only start set")
	}

	s.End = SelectionPoint{5, 10}
	if !s.HasSelection() {
		t.Error("should return true with both start and end set")
	}
}

func TestSelectionState_IsLineSelected(t *testing.T) {
	s := newTestState()
	s.Start = SelectionPoint{3, 0}
	s.End = SelectionPoint{5, 10}

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
		got := s.IsLineSelected(tt.lineIdx)
		if got != tt.expected {
			t.Errorf("IsLineSelected(%d) = %v, want %v", tt.lineIdx, got, tt.expected)
		}
	}
}

func TestSelectionState_IsLineSelected_NoSelection(t *testing.T) {
	s := newTestState()
	if s.IsLineSelected(0) {
		t.Error("should return false when no selection")
	}
}

func TestGetLineSelectionCols(t *testing.T) {
	s := newTestState()

	tests := []struct {
		name        string
		start, end  SelectionPoint
		lineIdx     int
		expectStart int
		expectEnd   int
	}{
		{
			"line before selection",
			SelectionPoint{3, 5}, SelectionPoint{6, 10},
			2, -1, -1,
		},
		{
			"line after selection",
			SelectionPoint{3, 5}, SelectionPoint{6, 10},
			7, -1, -1,
		},
		{
			"first line of multi-line",
			SelectionPoint{3, 5}, SelectionPoint{6, 10},
			3, 5, -1,
		},
		{
			"middle line",
			SelectionPoint{3, 5}, SelectionPoint{6, 10},
			4, 0, -1,
		},
		{
			"last line of multi-line",
			SelectionPoint{3, 5}, SelectionPoint{6, 10},
			6, 0, 10,
		},
		{
			"single-line selection",
			SelectionPoint{5, 3}, SelectionPoint{5, 15},
			5, 3, 15,
		},
	}

	for _, tt := range tests {
		s.Start = tt.start
		s.End = tt.end
		startCol, endCol := s.GetLineSelectionCols(tt.lineIdx)
		if startCol != tt.expectStart || endCol != tt.expectEnd {
			t.Errorf("%s: GetLineSelectionCols(%d) = (%d, %d), want (%d, %d)",
				tt.name, tt.lineIdx, startCol, endCol, tt.expectStart, tt.expectEnd)
		}
	}
}

func TestGetLineSelectionCols_NoSelection(t *testing.T) {
	s := newTestState()
	sc, ec := s.GetLineSelectionCols(0)
	if sc != -1 || ec != -1 {
		t.Errorf("no selection: got (%d, %d), want (-1, -1)", sc, ec)
	}
}

// --- PrepareDrag / HandleDrag / FinishDrag lifecycle ---

func TestPrepareDrag_SetsAnchor(t *testing.T) {
	s := newTestState()
	rect := mouse.Rect{X: 0, Y: 2, W: 80, H: 12}
	s.PrepareDrag(3, 5, rect)

	if s.Active {
		t.Error("should not be active after PrepareDrag")
	}
	if s.Start.Valid() {
		t.Error("start should be invalid after PrepareDrag")
	}
	if s.End.Valid() {
		t.Error("end should be invalid after PrepareDrag")
	}
	if s.Anchor.Line != 3 || s.Anchor.Col != 5 {
		t.Errorf("anchor = %+v, want {3, 5}", s.Anchor)
	}
	if s.ViewRect != rect {
		t.Errorf("ViewRect = %+v, want %+v", s.ViewRect, rect)
	}
}

func TestHandleDrag_ActivatesSelection(t *testing.T) {
	s := newTestState()
	s.PrepareDrag(2, 5, mouse.Rect{})

	// First drag motion
	s.HandleDrag(4, 10)

	if !s.Active {
		t.Error("should be active after HandleDrag")
	}
	if !s.HasSelection() {
		t.Error("should have selection after HandleDrag")
	}
	if s.Start.Line != 2 || s.Start.Col != 5 {
		t.Errorf("start = %+v, want {2, 5}", s.Start)
	}
	if s.End.Line != 4 || s.End.Col != 10 {
		t.Errorf("end = %+v, want {4, 10}", s.End)
	}
}

func TestHandleDrag_UpwardOrdering(t *testing.T) {
	s := newTestState()
	s.PrepareDrag(4, 10, mouse.Rect{})

	// Drag upward
	s.HandleDrag(1, 3)

	if s.Start.Line != 1 || s.Start.Col != 3 {
		t.Errorf("start = %+v, want {1, 3}", s.Start)
	}
	if s.End.Line != 4 || s.End.Col != 10 {
		t.Errorf("end = %+v, want {4, 10}", s.End)
	}
}

func TestHandleDrag_DirectionReversal(t *testing.T) {
	s := newTestState()
	s.PrepareDrag(0, 8, mouse.Rect{})

	// Drag right
	s.HandleDrag(0, 12)
	if s.Start.Col != 8 || s.End.Col != 12 {
		t.Errorf("after right drag: start.Col=%d, end.Col=%d, want 8, 12",
			s.Start.Col, s.End.Col)
	}

	// Reverse past anchor to left
	s.HandleDrag(0, 3)
	if s.Start.Col != 3 || s.End.Col != 8 {
		t.Errorf("after reversal: start.Col=%d, end.Col=%d, want 3, 8",
			s.Start.Col, s.End.Col)
	}
}

func TestFinishDrag_NoDragClears(t *testing.T) {
	s := newTestState()
	s.PrepareDrag(2, 5, mouse.Rect{})

	// Finish without any drag motion
	s.FinishDrag()

	if s.HasSelection() {
		t.Error("finish without drag should not leave selection")
	}
	if s.Start.Valid() {
		t.Errorf("start should be invalid after clear, got %+v", s.Start)
	}
	if s.Anchor.Valid() {
		t.Errorf("anchor should be invalid after clear, got %+v", s.Anchor)
	}
}

func TestFinishDrag_AfterDragPreservesSelection(t *testing.T) {
	s := newTestState()
	s.PrepareDrag(2, 5, mouse.Rect{})
	s.HandleDrag(4, 10)

	s.FinishDrag()

	if !s.HasSelection() {
		t.Error("selection range should persist after finish")
	}
	if s.Active {
		t.Error("active should be false after finish")
	}
	if s.Start.Line != 2 {
		t.Errorf("start line should be 2, got %d", s.Start.Line)
	}
	if s.End.Line != 4 {
		t.Errorf("end line should be 4, got %d", s.End.Line)
	}
}

// --- SelectedText tests ---

func TestSelectedText_SingleLine(t *testing.T) {
	s := newTestState()
	s.Start = SelectionPoint{0, 6}
	s.End = SelectionPoint{0, 10}

	lines := []string{"hello world"}
	result := s.SelectedText(lines, 0, 8)
	if len(result) != 1 {
		t.Fatalf("expected 1 line, got %d", len(result))
	}
	if result[0] != "world" {
		t.Errorf("got %q, want %q", result[0], "world")
	}
}

func TestSelectedText_MultiLine(t *testing.T) {
	s := newTestState()
	s.Start = SelectionPoint{0, 6}
	s.End = SelectionPoint{2, 4}

	lines := []string{"hello world", "middle line", "final text here"}
	result := s.SelectedText(lines, 0, 8)
	if len(result) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(result))
	}
	// First line: from col 6 to end
	if result[0] != "world" {
		t.Errorf("first line: got %q, want %q", result[0], "world")
	}
	// Middle line: ANSI stripped full text
	if result[1] != "middle line" {
		t.Errorf("middle line: got %q, want %q", result[1], "middle line")
	}
	// Last line: from 0 to col 5 (endCol+1=5, exclusive)
	if result[2] != "final" {
		t.Errorf("last line: got %q, want %q", result[2], "final")
	}
}

func TestSelectedText_NoSelection(t *testing.T) {
	s := newTestState()
	result := s.SelectedText([]string{"hello"}, 0, 8)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestSelectedText_EmptyLines(t *testing.T) {
	s := newTestState()
	s.Start = SelectionPoint{0, 0}
	s.End = SelectionPoint{0, 4}
	result := s.SelectedText(nil, 0, 8)
	if result != nil {
		t.Errorf("expected nil for nil lines, got %v", result)
	}
}

// --- ExpandTabs tests ---

func TestExpandTabs_NoTabs(t *testing.T) {
	input := "hello world"
	got := ExpandTabs(input, 4)
	if got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestExpandTabs_SingleTab(t *testing.T) {
	input := "\thello"
	got := ExpandTabs(input, 4)
	if got != "    hello" {
		t.Errorf("got %q, want %q", got, "    hello")
	}
}

func TestExpandTabs_MidTab(t *testing.T) {
	input := "ab\tcd"
	got := ExpandTabs(input, 4)
	// "ab" is 2 chars, tab expands to 2 spaces (4 - 2%4 = 2)
	if got != "ab  cd" {
		t.Errorf("got %q, want %q", got, "ab  cd")
	}
}

func TestExpandTabs_ZeroWidth(t *testing.T) {
	input := "\thello"
	got := ExpandTabs(input, 0)
	if got != input {
		t.Errorf("zero tabWidth should return unchanged, got %q", got)
	}
}

// --- VisualSubstring tests ---

func TestVisualSubstring_PlainText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		start    int
		end      int
		expected string
	}{
		{"full word", "hello world", 6, 11, "world"},
		{"mid-word", "hello world", 2, 7, "llo w"},
		{"to end", "hello", 2, -1, "llo"},
		{"from start", "hello", 0, 3, "hel"},
		{"single char", "hello", 2, 3, "l"},
		{"empty string", "", 0, 5, ""},
		{"start beyond len", "hello", 10, 15, ""},
	}

	for _, tt := range tests {
		got := VisualSubstring(tt.input, tt.start, tt.end)
		if got != tt.expected {
			t.Errorf("%s: VisualSubstring(%q, %d, %d) = %q, want %q",
				tt.name, tt.input, tt.start, tt.end, got, tt.expected)
		}
	}
}

func TestVisualSubstring_WithANSI(t *testing.T) {
	input := "\x1b[31mhello\x1b[0m world"
	got := VisualSubstring(input, 6, 11)
	if got != "world" {
		t.Errorf("ANSI: VisualSubstring = %q, want %q", got, "world")
	}

	got = VisualSubstring(input, 0, 5)
	if got != "hello" {
		t.Errorf("ANSI within: VisualSubstring = %q, want %q", got, "hello")
	}
}

func TestVisualSubstring_MultiWidth(t *testing.T) {
	input := "A\U0001f389B" // A + party popper emoji (2 cols) + B

	got := VisualSubstring(input, 1, 3)
	if got != "\U0001f389" {
		t.Errorf("emoji: VisualSubstring(%q, 1, 3) = %q, want %q", input, got, "\U0001f389")
	}

	got = VisualSubstring(input, 0, -1)
	if got != "A\U0001f389B" {
		t.Errorf("all: VisualSubstring(%q, 0, -1) = %q, want %q", input, got, "A\U0001f389B")
	}
}

// --- InjectCharacterRangeBackground tests ---

func TestInjectCharacterRangeBackground_FullLine(t *testing.T) {
	input := "hello world"
	result := InjectCharacterRangeBackground(input, 0, -1)
	expected := InjectSelectionBackground(input)
	if result != expected {
		t.Errorf("full line: got %q, want %q", result, expected)
	}
}

func TestInjectCharacterRangeBackground_Partial(t *testing.T) {
	input := "hello world"
	result := InjectCharacterRangeBackground(input, 6, 10)

	selBg := GetSelectionBgANSI()
	if !strings.Contains(result, selBg) {
		t.Error("partial: result should contain selection background ANSI")
	}
	if !strings.Contains(result, "\x1b[49m") {
		t.Error("partial: result should contain background-only ANSI reset")
	}
}

func TestInjectCharacterRangeBackground_EmptyString(t *testing.T) {
	result := InjectCharacterRangeBackground("", 0, 5)
	if result != "" {
		t.Errorf("empty: got %q, want empty", result)
	}
}

// --- VisualColAtRelativeX tests ---

func TestVisualColAtRelativeX_PlainText(t *testing.T) {
	col := VisualColAtRelativeX("hello world", 5)
	if col != 5 {
		t.Errorf("plain text: col = %d, want 5", col)
	}
}

func TestVisualColAtRelativeX_BeyondEnd(t *testing.T) {
	col := VisualColAtRelativeX("hello", 100)
	// Should clamp to last char (col 4)
	if col != 4 {
		t.Errorf("beyond end: col = %d, want 4", col)
	}
}

func TestVisualColAtRelativeX_EmptyLine(t *testing.T) {
	col := VisualColAtRelativeX("", 5)
	if col != 0 {
		t.Errorf("empty line: col = %d, want 0", col)
	}
}

func TestVisualColAtRelativeX_NegativeX(t *testing.T) {
	col := VisualColAtRelativeX("hello", -5)
	if col != 0 {
		t.Errorf("negative X: col = %d, want 0", col)
	}
}

func TestVisualColAtRelativeX_MultiWidth(t *testing.T) {
	// "A" at col 0, emoji at cols 1-2, "B" at col 3
	line := "A\U0001f389B"
	expanded := ExpandTabs(line, 4) // no tabs, just use as-is

	// X=1 should snap to col 1 (start of emoji)
	col := VisualColAtRelativeX(expanded, 1)
	if col != 1 {
		t.Errorf("emoji start: col = %d, want 1", col)
	}

	// X=2 should snap to col 1 (within emoji, snaps to start)
	col = VisualColAtRelativeX(expanded, 2)
	if col != 1 {
		t.Errorf("emoji mid: col = %d, want 1", col)
	}

	// X=3 should be col 3 (the "B")
	col = VisualColAtRelativeX(expanded, 3)
	if col != 3 {
		t.Errorf("after emoji: col = %d, want 3", col)
	}
}

func TestVisualColAtRelativeX_ExactEnd(t *testing.T) {
	// "hello" occupies cols 0-4, cumWidth=5
	// X=4 is the last char
	col := VisualColAtRelativeX("hello", 4)
	if col != 4 {
		t.Errorf("exact last char: col = %d, want 4", col)
	}
}
