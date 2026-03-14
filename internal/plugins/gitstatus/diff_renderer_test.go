package gitstatus

import (
	"strings"
	"testing"
)

func TestRenderLineDiff_EmptyDiff(t *testing.T) {
	result := RenderLineDiff(nil, 80, 0, 20, 0, nil, false)
	if !strings.Contains(result, "No diff content") {
		t.Error("expected 'No diff content' message for nil diff")
	}
}

func TestRenderLineDiff_BinaryFile(t *testing.T) {
	diff := &ParsedDiff{Binary: true}
	result := RenderLineDiff(diff, 80, 0, 20, 0, nil, false)
	if !strings.Contains(result, "Binary") {
		t.Error("expected 'Binary' message for binary diff")
	}
}

func TestRenderLineDiff_BasicOutput(t *testing.T) {
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1,
				OldCount: 2,
				NewStart: 1,
				NewCount: 2,
				Lines: []DiffLine{
					{Type: LineContext, OldLineNo: 1, NewLineNo: 1, Content: "context"},
					{Type: LineRemove, OldLineNo: 2, NewLineNo: 0, Content: "old"},
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 2, Content: "new"},
				},
			},
		},
	}

	result := RenderLineDiff(diff, 80, 0, 20, 0, nil, false)

	if result == "" {
		t.Error("RenderLineDiff returned empty string")
	}

	// Should contain hunk header
	if !strings.Contains(result, "@@") {
		t.Error("expected hunk header in output")
	}
}

func TestRenderSideBySide_EmptyDiff(t *testing.T) {
	result := RenderSideBySide(nil, 80, 0, 20, 0, nil, false)
	if !strings.Contains(result, "No diff content") {
		t.Error("expected 'No diff content' message for nil diff")
	}
}

func TestRenderSideBySide_BinaryFile(t *testing.T) {
	diff := &ParsedDiff{Binary: true}
	result := RenderSideBySide(diff, 80, 0, 20, 0, nil, false)
	if !strings.Contains(result, "Binary") {
		t.Error("expected 'Binary' message for binary diff")
	}
}

func TestRenderSideBySide_BasicOutput(t *testing.T) {
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1,
				OldCount: 2,
				NewStart: 1,
				NewCount: 2,
				Lines: []DiffLine{
					{Type: LineContext, OldLineNo: 1, NewLineNo: 1, Content: "context"},
					{Type: LineRemove, OldLineNo: 2, NewLineNo: 0, Content: "old"},
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 2, Content: "new"},
				},
			},
		},
	}

	result := RenderSideBySide(diff, 100, 0, 20, 0, nil, false)

	if result == "" {
		t.Error("RenderSideBySide returned empty string")
	}

	// Should contain separator character
	if !strings.Contains(result, "│") {
		t.Error("expected separator character in side-by-side output")
	}
}

func TestGroupLinesForSideBySide_ContextLines(t *testing.T) {
	lines := []DiffLine{
		{Type: LineContext, Content: "ctx1"},
		{Type: LineContext, Content: "ctx2"},
	}

	pairs := groupLinesForSideBySide(lines)

	if len(pairs) != 2 {
		t.Fatalf("len(pairs) = %d, want 2", len(pairs))
	}

	// Context lines appear on both sides
	for i, p := range pairs {
		if p.left == nil || p.right == nil {
			t.Errorf("pair[%d] has nil side for context line", i)
		}
	}
}

func TestGroupLinesForSideBySide_RemoveAddPair(t *testing.T) {
	lines := []DiffLine{
		{Type: LineRemove, Content: "old"},
		{Type: LineAdd, Content: "new"},
	}

	pairs := groupLinesForSideBySide(lines)

	if len(pairs) != 1 {
		t.Fatalf("len(pairs) = %d, want 1", len(pairs))
	}

	if pairs[0].left == nil || pairs[0].left.Content != "old" {
		t.Error("left side should be 'old'")
	}
	if pairs[0].right == nil || pairs[0].right.Content != "new" {
		t.Error("right side should be 'new'")
	}
}

func TestGroupLinesForSideBySide_MultipleRemoves(t *testing.T) {
	lines := []DiffLine{
		{Type: LineRemove, Content: "old1"},
		{Type: LineRemove, Content: "old2"},
		{Type: LineAdd, Content: "new1"},
	}

	pairs := groupLinesForSideBySide(lines)

	if len(pairs) != 2 {
		t.Fatalf("len(pairs) = %d, want 2", len(pairs))
	}

	// First pair: old1 -> new1
	if pairs[0].left.Content != "old1" {
		t.Errorf("first left = %q, want 'old1'", pairs[0].left.Content)
	}
	// Second pair: old2 -> nil
	if pairs[1].left.Content != "old2" {
		t.Errorf("second left = %q, want 'old2'", pairs[1].left.Content)
	}
	if pairs[1].right != nil {
		t.Error("second right should be nil")
	}
}

func TestTruncateLine(t *testing.T) {
	tests := []struct {
		input    string
		maxWidth int
		want     string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is..."},
		{"ab", 5, "ab"},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := truncateLine(tc.input, tc.maxWidth)
			if got != tc.want {
				t.Errorf("truncateLine(%q, %d) = %q, want %q", tc.input, tc.maxWidth, got, tc.want)
			}
		})
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		input string
		width int
		want  string
	}{
		{"abc", 5, "abc  "},
		{"abc", 3, "abc"},
		{"abc", 2, "abc"},
		{"", 3, "   "},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := padRight(tc.input, tc.width)
			if got != tc.want {
				t.Errorf("padRight(%q, %d) = %q, want %q", tc.input, tc.width, got, tc.want)
			}
		})
	}
}

func TestDiffViewMode_Constants(t *testing.T) {
	// Verify the constants exist and are distinct
	if DiffViewUnified == DiffViewSideBySide {
		t.Error("DiffViewUnified and DiffViewSideBySide should be different")
	}
}

func TestRenderLineDiff_WithHorizontalOffset(t *testing.T) {
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1,
				OldCount: 1,
				NewStart: 1,
				NewCount: 1,
				Lines: []DiffLine{
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 1, Content: "0123456789ABCDEFGHIJ"},
				},
			},
		},
	}

	// Without offset - should show full content
	result0 := RenderLineDiff(diff, 80, 0, 20, 0, nil, false)
	if !strings.Contains(result0, "0123456789") {
		t.Error("expected full content when offset=0")
	}

	// With offset=5 - should skip first 5 chars
	result5 := RenderLineDiff(diff, 80, 0, 20, 5, nil, false)
	if strings.Contains(result5, "01234") {
		t.Error("offset=5 should hide first 5 chars")
	}
	if !strings.Contains(result5, "56789") {
		t.Error("offset=5 should show chars starting at position 5")
	}

	// With very large offset - should handle gracefully
	result100 := RenderLineDiff(diff, 80, 0, 20, 100, nil, false)
	if result100 == "" {
		t.Error("large offset should not crash, should return something")
	}
}

func TestRenderSideBySide_WithHorizontalOffset(t *testing.T) {
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1,
				OldCount: 1,
				NewStart: 1,
				NewCount: 1,
				Lines: []DiffLine{
					{Type: LineRemove, OldLineNo: 1, NewLineNo: 0, Content: "OLDCONTENT0123456789"},
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 1, Content: "NEWCONTENT0123456789"},
				},
			},
		},
	}

	// Without offset
	result0 := RenderSideBySide(diff, 120, 0, 20, 0, nil, false)
	if !strings.Contains(result0, "OLD") {
		t.Error("expected OLD prefix when offset=0")
	}

	// With offset=3 - should skip first 3 chars
	result3 := RenderSideBySide(diff, 120, 0, 20, 3, nil, false)
	if strings.Contains(result3, "OLD") || strings.Contains(result3, "NEW") {
		t.Error("offset=3 should hide first 3 chars of each side")
	}
}

func TestRenderDiffContentWithOffset_DisablesWordDiff(t *testing.T) {
	// Line with word diff data
	line := DiffLine{
		Type:      LineAdd,
		Content:   "hello world test",
		OldLineNo: 0,
		NewLineNo: 1,
		WordDiff: []WordSegment{
			{Text: "hello ", IsChange: false},
			{Text: "world", IsChange: true},
			{Text: " test", IsChange: false},
		},
	}

	// With offset=0, word diff should be preserved (handled in renderDiffContent)
	result0 := renderDiffContentWithOffset(line, 80, 0, nil)
	if result0 == "" {
		t.Error("expected non-empty result")
	}

	// With offset>0, word diff should be disabled (set to nil internally)
	result5 := renderDiffContentWithOffset(line, 80, 5, nil)
	if result5 == "" {
		t.Error("expected non-empty result with offset")
	}
	// Content should be offset
	if strings.Contains(result5, "hello") {
		t.Error("offset=5 should skip 'hello'")
	}
}

func TestRenderLineDiff_WrapEnabled(t *testing.T) {
	longContent := strings.Repeat("ABCDEFGHIJ", 15) // 150 chars
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1,
				Lines: []DiffLine{
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 1, Content: longContent},
				},
			},
		},
	}

	result := RenderLineDiff(diff, 40, 0, 50, 0, nil, true)
	if result == "" {
		t.Fatal("wrap=true returned empty")
	}
	lines := strings.Split(result, "\n")
	// With wrapping at width 40, a 150-char line should produce multiple lines
	if len(lines) < 3 {
		t.Errorf("expected multiple wrapped lines, got %d", len(lines))
	}
}

func TestRenderLineDiff_WrapVsTruncate(t *testing.T) {
	longContent := strings.Repeat("A", 100)
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1,
				Lines: []DiffLine{
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 1, Content: longContent},
				},
			},
		},
	}

	truncated := RenderLineDiff(diff, 40, 0, 50, 0, nil, false)
	wrapped := RenderLineDiff(diff, 40, 0, 50, 0, nil, true)

	truncLines := strings.Count(truncated, "\n")
	wrapLines := strings.Count(wrapped, "\n")

	if wrapLines <= truncLines {
		t.Errorf("wrapped (%d newlines) should have more lines than truncated (%d newlines)", wrapLines, truncLines)
	}
}

func TestRenderSideBySide_WrapEnabled(t *testing.T) {
	longContent := strings.Repeat("XYZW", 30) // 120 chars
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1,
				Lines: []DiffLine{
					{Type: LineRemove, OldLineNo: 1, NewLineNo: 0, Content: longContent},
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 1, Content: longContent},
				},
			},
		},
	}

	result := RenderSideBySide(diff, 60, 0, 50, 0, nil, true)
	if result == "" {
		t.Fatal("wrap=true side-by-side returned empty")
	}
	if !strings.Contains(result, "│") {
		t.Error("expected separator in side-by-side output")
	}
}

func TestRenderLineDiff_WrapRespectsMaxLines(t *testing.T) {
	veryLong := strings.Repeat("W", 500)
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1,
				Lines: []DiffLine{
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 1, Content: veryLong},
				},
			},
		},
	}

	maxLines := 3
	result := RenderLineDiff(diff, 40, 0, maxLines, 0, nil, true)
	lineCount := strings.Count(result, "\n")
	if lineCount > maxLines {
		t.Errorf("output has %d lines, should not exceed maxLines=%d", lineCount, maxLines)
	}
}

func TestRenderLineDiff_WithWrapEnabled(t *testing.T) {
	// Create a diff with a very long line
	longContent := strings.Repeat("x", 200)
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1,
				OldCount: 1,
				NewStart: 1,
				NewCount: 1,
				Lines: []DiffLine{
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 1, Content: longContent},
				},
			},
		},
	}

	// Without wrap - should truncate
	resultNoWrap := RenderLineDiff(diff, 80, 0, 20, 0, nil, false)
	linesNoWrap := strings.Split(strings.TrimSpace(resultNoWrap), "\n")
	
	// With wrap - should create multiple lines
	resultWrap := RenderLineDiff(diff, 80, 0, 20, 0, nil, true)
	linesWrap := strings.Split(strings.TrimSpace(resultWrap), "\n")
	
	// Wrapped version should have more lines
	if len(linesWrap) <= len(linesNoWrap) {
		t.Errorf("wrapped output should have more lines: got %d vs %d", len(linesWrap), len(linesNoWrap))
	}
}

func TestRenderLineDiff_WrapWithEmptyLines(t *testing.T) {
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1,
				OldCount: 3,
				NewStart: 1,
				NewCount: 3,
				Lines: []DiffLine{
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 1, Content: "short"},
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 2, Content: ""},
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 3, Content: strings.Repeat("y", 100)},
				},
			},
		},
	}

	result := RenderLineDiff(diff, 80, 0, 20, 0, nil, true)
	if result == "" {
		t.Error("expected non-empty result with wrap enabled")
	}
	
	// Should handle empty lines gracefully
	if !strings.Contains(result, "short") {
		t.Error("should contain short line")
	}
}

func TestRenderSideBySide_WithWrapEnabled(t *testing.T) {
	longOld := strings.Repeat("a", 150)
	longNew := strings.Repeat("b", 150)
	
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1,
				OldCount: 1,
				NewStart: 1,
				NewCount: 1,
				Lines: []DiffLine{
					{Type: LineRemove, OldLineNo: 1, NewLineNo: 0, Content: longOld},
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 1, Content: longNew},
				},
			},
		},
	}

	// Without wrap
	resultNoWrap := RenderSideBySide(diff, 120, 0, 20, 0, nil, false)
	linesNoWrap := strings.Split(strings.TrimSpace(resultNoWrap), "\n")
	
	// With wrap
	resultWrap := RenderSideBySide(diff, 120, 0, 20, 0, nil, true)
	linesWrap := strings.Split(strings.TrimSpace(resultWrap), "\n")
	
	// Wrapped version should have more lines
	if len(linesWrap) <= len(linesNoWrap) {
		t.Errorf("wrapped side-by-side should have more lines: got %d vs %d", len(linesWrap), len(linesNoWrap))
	}
}

func TestRenderLineDiff_WrapVeryLongLine(t *testing.T) {
	// Test with 1000+ character line
	veryLongContent := strings.Repeat("abcdefghij", 150) // 1500 chars
	diff := &ParsedDiff{
		OldFile: "test.go",
		NewFile: "test.go",
		Hunks: []Hunk{
			{
				OldStart: 1,
				OldCount: 1,
				NewStart: 1,
				NewCount: 1,
				Lines: []DiffLine{
					{Type: LineAdd, OldLineNo: 0, NewLineNo: 1, Content: veryLongContent},
				},
			},
		},
	}

	result := RenderLineDiff(diff, 80, 0, 50, 0, nil, true)
	lines := strings.Split(strings.TrimSpace(result), "\n")
	
	// Should wrap into many lines
	if len(lines) < 10 {
		t.Errorf("expected at least 10 wrapped lines for 1500 char content, got %d", len(lines))
	}
}

func TestRenderFullFileSideBySide_Nil(t *testing.T) {
	result := RenderFullFileSideBySide(nil, 80, 0, 20, 0, nil, false)
	if !strings.Contains(result, "No diff content") {
		t.Error("expected 'No diff content' for nil FullFileDiff")
	}
}

func TestRenderFullFileSideBySide_Empty(t *testing.T) {
	ffd := &FullFileDiff{}
	result := RenderFullFileSideBySide(ffd, 80, 0, 20, 0, nil, false)
	if !strings.Contains(result, "No diff content") {
		t.Error("expected 'No diff content' for empty FullFileDiff")
	}
}

func TestRenderFullFileSideBySide_ContextOnly(t *testing.T) {
	ffd := &FullFileDiff{
		Lines: []FullFileLine{
			{Type: LineContext, OldLineNo: 1, NewLineNo: 1, OldText: "same", NewText: "same"},
			{Type: LineContext, OldLineNo: 2, NewLineNo: 2, OldText: "line2", NewText: "line2"},
		},
	}

	result := RenderFullFileSideBySide(ffd, 100, 0, 20, 0, nil, false)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "│") {
		t.Error("expected separator in side-by-side output")
	}
	// Should contain line numbers
	if !strings.Contains(result, "1") || !strings.Contains(result, "2") {
		t.Error("expected line numbers in output")
	}
}

func TestRenderFullFileSideBySide_WithChanges(t *testing.T) {
	ffd := &FullFileDiff{
		Lines: []FullFileLine{
			{Type: LineContext, OldLineNo: 1, NewLineNo: 1, OldText: "same", NewText: "same"},
			{Type: LineRemove, OldLineNo: 2, OldText: "old"},
			{Type: LineAdd, NewLineNo: 2, NewText: "new"},
			{Type: LineContext, OldLineNo: 3, NewLineNo: 3, OldText: "end", NewText: "end"},
		},
	}

	result := RenderFullFileSideBySide(ffd, 100, 0, 20, 0, nil, false)
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	lines := strings.Split(result, "\n")
	// Should have 4 rendered lines (+ trailing empty from final \n)
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	if nonEmpty != 4 {
		t.Errorf("expected 4 non-empty lines, got %d", nonEmpty)
	}
}

func TestRenderFullFileSideBySide_Scrolling(t *testing.T) {
	var lines []FullFileLine
	for i := 0; i < 50; i++ {
		lines = append(lines, FullFileLine{
			Type:      LineContext,
			OldLineNo: i + 1,
			NewLineNo: i + 1,
			OldText:   "line",
			NewText:   "line",
		})
	}
	ffd := &FullFileDiff{Lines: lines}

	// Render from line 10, max 5 lines
	result := RenderFullFileSideBySide(ffd, 100, 10, 5, 0, nil, false)
	rendered := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	if len(rendered) != 5 {
		t.Errorf("expected 5 lines with startLine=10, maxLines=5, got %d", len(rendered))
	}
}

func TestRenderFullFileSideBySide_WrapEnabled(t *testing.T) {
	ffd := &FullFileDiff{
		Lines: []FullFileLine{
			{Type: LineContext, OldLineNo: 1, NewLineNo: 1,
				OldText: strings.Repeat("X", 200), NewText: strings.Repeat("Y", 200)},
		},
	}

	result := RenderFullFileSideBySide(ffd, 80, 0, 50, 0, nil, true)
	if result == "" {
		t.Fatal("expected non-empty result with wrap enabled")
	}
	lines := strings.Split(result, "\n")
	// Wrapped long lines should produce multiple rendered lines
	if len(lines) < 2 {
		t.Error("expected multiple wrapped lines")
	}
}

func TestDiffViewMode_FullFileConstant(t *testing.T) {
	// Verify the three constants are distinct
	modes := []DiffViewMode{DiffViewUnified, DiffViewSideBySide, DiffViewFullFile}
	seen := make(map[DiffViewMode]bool)
	for _, m := range modes {
		if seen[m] {
			t.Errorf("duplicate DiffViewMode value: %d", m)
		}
		seen[m] = true
	}
}

func TestRenderFullFileSideBySide_HorizontalOffset(t *testing.T) {
	ffd := &FullFileDiff{
		Lines: []FullFileLine{
			{Type: LineContext, OldLineNo: 1, NewLineNo: 1,
				OldText: "0123456789ABCDEF", NewText: "0123456789ABCDEF"},
		},
	}

	result0 := RenderFullFileSideBySide(ffd, 100, 0, 20, 0, nil, false)
	result5 := RenderFullFileSideBySide(ffd, 100, 0, 20, 5, nil, false)

	// Both should produce output
	if result0 == "" || result5 == "" {
		t.Fatal("expected non-empty results")
	}
	// With offset, content should differ
	if result0 == result5 {
		t.Error("horizontal offset should produce different output")
	}
}

func TestFullFileDiff_NextChange(t *testing.T) {
	ffd := &FullFileDiff{
		Lines: []FullFileLine{
			{Type: LineContext, OldLineNo: 1, NewLineNo: 1},  // 0
			{Type: LineContext, OldLineNo: 2, NewLineNo: 2},  // 1
			{Type: LineRemove, OldLineNo: 3},                 // 2
			{Type: LineAdd, NewLineNo: 3},                    // 3
			{Type: LineContext, OldLineNo: 4, NewLineNo: 4},  // 4
			{Type: LineContext, OldLineNo: 5, NewLineNo: 5},  // 5
			{Type: LineAdd, NewLineNo: 6},                    // 6
			{Type: LineContext, OldLineNo: 6, NewLineNo: 7},  // 7
		},
	}

	// From start, should find first change at line 2
	if got := ffd.NextChange(0); got != 2 {
		t.Errorf("NextChange(0) = %d, want 2", got)
	}

	// From within first change block, should skip to next change at line 6
	if got := ffd.NextChange(2); got != 6 {
		t.Errorf("NextChange(2) = %d, want 6", got)
	}

	// From last change, no more changes
	if got := ffd.NextChange(6); got != -1 {
		t.Errorf("NextChange(6) = %d, want -1", got)
	}

	// Nil diff
	var nilFfd *FullFileDiff
	if got := nilFfd.NextChange(0); got != -1 {
		t.Errorf("NextChange on nil = %d, want -1", got)
	}
}

func TestFullFileDiff_PrevChange(t *testing.T) {
	ffd := &FullFileDiff{
		Lines: []FullFileLine{
			{Type: LineContext, OldLineNo: 1, NewLineNo: 1},  // 0
			{Type: LineRemove, OldLineNo: 2},                 // 1
			{Type: LineAdd, NewLineNo: 2},                    // 2
			{Type: LineContext, OldLineNo: 3, NewLineNo: 3},  // 3
			{Type: LineContext, OldLineNo: 4, NewLineNo: 4},  // 4
			{Type: LineRemove, OldLineNo: 5},                 // 5
			{Type: LineAdd, NewLineNo: 5},                    // 6
			{Type: LineContext, OldLineNo: 6, NewLineNo: 6},  // 7
		},
	}

	// From after second change block, should find it at line 5
	if got := ffd.PrevChange(7); got != 5 {
		t.Errorf("PrevChange(7) = %d, want 5", got)
	}

	// From second change block, should find first at line 1
	if got := ffd.PrevChange(5); got != 1 {
		t.Errorf("PrevChange(5) = %d, want 1", got)
	}

	// From first change, no previous
	if got := ffd.PrevChange(1); got != -1 {
		t.Errorf("PrevChange(1) = %d, want -1", got)
	}

	// Nil diff
	var nilFfd *FullFileDiff
	if got := nilFfd.PrevChange(5); got != -1 {
		t.Errorf("PrevChange on nil = %d, want -1", got)
	}
}

func TestFullFileDiff_FullFileLineToHunkLine(t *testing.T) {
	// Build a simple parsed diff with one hunk at old line 3
	parsed := &ParsedDiff{
		Hunks: []Hunk{
			{
				OldStart: 3, OldCount: 2, NewStart: 3, NewCount: 2,
				Lines: []DiffLine{
					{Type: LineRemove, OldLineNo: 3, Content: "old"},
					{Type: LineAdd, NewLineNo: 3, Content: "new"},
					{Type: LineContext, OldLineNo: 4, NewLineNo: 4, Content: "ctx"},
				},
			},
		},
	}

	ffd := &FullFileDiff{
		Lines: []FullFileLine{
			{Type: LineContext, OldLineNo: 1, NewLineNo: 1}, // 0
			{Type: LineContext, OldLineNo: 2, NewLineNo: 2}, // 1
			{Type: LineRemove, OldLineNo: 3},                // 2
			{Type: LineAdd, NewLineNo: 3},                   // 3
			{Type: LineContext, OldLineNo: 4, NewLineNo: 4}, // 4
			{Type: LineContext, OldLineNo: 5, NewLineNo: 5}, // 5
		},
	}

	// Line 0 (context, old line 1): before any hunk, should map to 0
	if got := ffd.FullFileLineToHunkLine(0, parsed); got != 0 {
		t.Errorf("FullFileLineToHunkLine(0) = %d, want 0", got)
	}

	// Line 2 (remove, old line 3): should map to hunk header (0) + 1 = line 1
	if got := ffd.FullFileLineToHunkLine(2, parsed); got != 1 {
		t.Errorf("FullFileLineToHunkLine(2) = %d, want 1", got)
	}

	// Line 3 (add, new line 3): should map to hunk header (0) + 2 = line 2
	if got := ffd.FullFileLineToHunkLine(3, parsed); got != 2 {
		t.Errorf("FullFileLineToHunkLine(3) = %d, want 2", got)
	}

	// Nil full-file diff
	var nilFfd *FullFileDiff
	if got := nilFfd.FullFileLineToHunkLine(0, parsed); got != 0 {
		t.Errorf("FullFileLineToHunkLine on nil = %d, want 0", got)
	}
}
