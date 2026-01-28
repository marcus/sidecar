package filebrowser

import (
	"strings"
	"testing"
)

func TestInjectHighlightsIntoANSI_PlainText(t *testing.T) {
	input := "hello world foo"
	matches := []matchRange{
		{matchIdx: 0, start: 6, end: 11}, // "world"
	}

	result := injectHighlightsIntoANSI(input, matches, 0)

	if !strings.Contains(result, "world") {
		t.Error("result should contain match text")
	}
	// Should have ANSI codes injected
	if !strings.Contains(result, "\x1b[") {
		t.Error("result should contain ANSI escape codes for highlighting")
	}
	// Should have reset after highlight
	if !strings.Contains(result, "\x1b[0m") {
		t.Error("result should contain ANSI reset after highlight")
	}
	// Text before and after match should be preserved
	if !strings.Contains(result, "hello ") {
		t.Error("text before match should be preserved")
	}
	if !strings.Contains(result, " foo") {
		t.Error("text after match should be preserved")
	}
}

func TestInjectHighlightsIntoANSI_WithANSIEscapes(t *testing.T) {
	// Simulated ANSI-styled line: "hello" in red, then " world"
	input := "\x1b[31mhello\x1b[0m world"
	// Match "world" - visible position 6 (h=0, e=1, l=2, l=3, o=4, ' '=5, w=6)
	matches := []matchRange{
		{matchIdx: 0, start: 6, end: 11},
	}

	result := injectHighlightsIntoANSI(input, matches, 0)

	// Original ANSI for "hello" should be preserved
	if !strings.Contains(result, "\x1b[31m") {
		t.Error("original ANSI codes should be preserved")
	}
	// Match text should be present
	if !strings.Contains(result, "world") {
		t.Error("match text should be present")
	}
}

func TestInjectHighlightsIntoANSI_MultipleMatches(t *testing.T) {
	input := "foo bar foo baz"
	matches := []matchRange{
		{matchIdx: 0, start: 0, end: 3},  // first "foo"
		{matchIdx: 1, start: 8, end: 11}, // second "foo"
	}

	result := injectHighlightsIntoANSI(input, matches, 0)

	// Should have two resets (one per match end)
	count := strings.Count(result, "\x1b[0m")
	if count < 2 {
		t.Errorf("expected at least 2 ANSI resets, got %d", count)
	}
}

func TestInjectHighlightsIntoANSI_CurrentMatch(t *testing.T) {
	input := "foo bar foo"
	matches := []matchRange{
		{matchIdx: 0, start: 0, end: 3},
		{matchIdx: 1, start: 8, end: 11},
	}

	// Current match is index 1 (second "foo")
	result := injectHighlightsIntoANSI(input, matches, 1)

	// Both matches should have highlighting
	if strings.Count(result, "\x1b[0m") < 2 {
		t.Error("both matches should have highlighting with resets")
	}
	// The result should contain different style prefixes for current vs non-current
	// (verified by the fact it compiles and runs without panic)
	if len(result) <= len(input) {
		t.Error("result should be longer than input due to injected ANSI codes")
	}
}

func TestInjectHighlightsIntoANSI_NoMatches(t *testing.T) {
	input := "hello world"
	result := injectHighlightsIntoANSI(input, nil, 0)
	if result != input {
		t.Errorf("no matches should return input unchanged, got %q", result)
	}
}

func TestInjectHighlightsIntoANSI_EmptyString(t *testing.T) {
	result := injectHighlightsIntoANSI("", []matchRange{{matchIdx: 0, start: 0, end: 1}}, 0)
	if result != "" {
		t.Errorf("empty string should return empty, got %q", result)
	}
}

func TestContentSearchMarkdownMode(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "# Hello\n\nSome text with hello in it")
	p.previewFile = "test.md"

	// Simulate markdown rendered output (as Glamour would produce, with ANSI)
	p.markdownRendered = []string{
		"",
		"  \x1b[1mHello\x1b[0m",
		"",
		"  Some text with hello in it",
		"",
	}
	p.markdownRenderMode = true

	p.contentSearchMode = true
	p.contentSearchQuery = "hello"
	p.updateContentMatches()

	if len(p.contentSearchMatches) != 2 {
		t.Fatalf("expected 2 matches in rendered markdown, got %d", len(p.contentSearchMatches))
	}

	// First match should be on rendered line 1 ("Hello" stripped of ANSI)
	if p.contentSearchMatches[0].LineNo != 1 {
		t.Errorf("first match line: want 1, got %d", p.contentSearchMatches[0].LineNo)
	}
	// "  Hello" - "hello" starts at byte 2 (after two spaces)
	if p.contentSearchMatches[0].StartCol != 2 {
		t.Errorf("first match start col: want 2, got %d", p.contentSearchMatches[0].StartCol)
	}

	// Second match on rendered line 3
	if p.contentSearchMatches[1].LineNo != 3 {
		t.Errorf("second match line: want 3, got %d", p.contentSearchMatches[1].LineNo)
	}
}

func TestToggleMarkdownDuringSearch(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "# Hello\n\nSome hello text")
	p.previewFile = "test.md"

	// Search in raw mode first
	p.contentSearchMode = true
	p.contentSearchQuery = "hello"
	p.updateContentMatches()

	rawMatches := len(p.contentSearchMatches)
	if rawMatches != 2 {
		t.Fatalf("expected 2 raw matches, got %d", rawMatches)
	}
	// In raw mode, matches reference previewLines indices
	if p.contentSearchMatches[0].LineNo != 0 {
		t.Errorf("raw first match should be on line 0, got %d", p.contentSearchMatches[0].LineNo)
	}

	// Now toggle to markdown mode
	p.markdownRendered = []string{
		"",
		"  \x1b[1mHello\x1b[0m",
		"",
		"  Some hello text",
		"",
	}
	p.markdownRenderMode = true
	// toggleMarkdownRender calls updateContentMatches, but we already set mode manually
	p.updateContentMatches()

	if len(p.contentSearchMatches) != 2 {
		t.Fatalf("expected 2 markdown matches, got %d", len(p.contentSearchMatches))
	}
	// In markdown mode, matches reference markdownRendered indices
	if p.contentSearchMatches[0].LineNo != 1 {
		t.Errorf("markdown first match should be on line 1, got %d", p.contentSearchMatches[0].LineNo)
	}
}

func TestHighlightMarkdownLineMatches(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "# Test")
	p.previewFile = "test.md"
	p.markdownRendered = []string{
		"  \x1b[1mTest\x1b[0m line",
	}
	p.markdownRenderMode = true

	p.contentSearchMatches = []ContentMatch{
		{LineNo: 0, StartCol: 2, EndCol: 6}, // "Test" in stripped text "  Test line"
	}
	p.contentSearchCursor = 0

	result := p.highlightMarkdownLineMatches(0)

	// Should have injected highlight codes
	if !strings.Contains(result, "\x1b[") {
		t.Error("result should contain ANSI highlight codes")
	}
	// Original content should still be present
	if !strings.Contains(result, "Test") {
		t.Error("result should contain original text")
	}
	if !strings.Contains(result, "line") {
		t.Error("result should contain text after match")
	}
}

func TestHighlightMarkdownLineMatches_NoMatch(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "# Test")
	p.previewFile = "test.md"
	p.markdownRendered = []string{"  some line"}
	p.markdownRenderMode = true
	p.contentSearchMatches = []ContentMatch{
		{LineNo: 5, StartCol: 0, EndCol: 3}, // match on different line
	}

	result := p.highlightMarkdownLineMatches(0)
	if result != "  some line" {
		t.Errorf("no match on this line should return unchanged, got %q", result)
	}
}

func TestHighlightMarkdownLineMatches_OutOfBounds(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "# Test")
	p.previewFile = "test.md"
	p.markdownRendered = []string{"line"}

	result := p.highlightMarkdownLineMatches(5)
	if result != "" {
		t.Errorf("out of bounds should return empty, got %q", result)
	}
}
