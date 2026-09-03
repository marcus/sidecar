package filebrowser

import (
	"strings"
	"testing"
)

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
	p.contentSearchField.SetQuery("hello")
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
	p.contentSearchField.SetQuery("hello")
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
