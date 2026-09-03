package filebrowser

import (
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/markdown"
)

func newMarkdownPreviewPlugin(t *testing.T, content string) *Plugin {
	t.Helper()
	renderer, err := markdown.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	p := &Plugin{
		previewFile:        "README.md",
		previewLines:       strings.Split(content, "\n"),
		markdownRenderer:   renderer,
		previewWidth:       80,
		markdownRenderMode: true,
	}
	p.renderMarkdownContent()
	if len(p.markdownRendered) == 0 {
		t.Fatal("expected rendered markdown")
	}
	return p
}

// A theme change must discard the cached rendered markdown and rebuild it,
// rather than leaving the previous palette's ANSI on screen.
func TestHandleThemeChangedRerendersPreview(t *testing.T) {
	p := newMarkdownPreviewPlugin(t, "# Title\n\nBody with `code`.\n")
	want := strings.Join(p.markdownRendered, "\n")

	p.markdownRendered = []string{"stale ansi from the previous theme"}
	p.handleThemeChanged()

	got := strings.Join(p.markdownRendered, "\n")
	if got != want {
		t.Fatalf("preview was not rebuilt from the current theme:\n got %q", got)
	}
}

func TestHandleThemeChangedRefreshesSearchAndClampsState(t *testing.T) {
	p := newMarkdownPreviewPlugin(t, "# Title\n\nneedle in the body\n")
	p.contentSearchMode = true
	p.contentSearchField.SetQuery("needle")
	p.updateContentMatches()
	if len(p.contentSearchMatches) == 0 {
		t.Fatal("expected a search match before the theme change")
	}
	p.contentSearchMatches = nil

	// Out-of-range scroll and selection must be clamped/cleared, not kept.
	p.previewScroll = 9999
	p.selection.Active = true
	p.selection.Start.Line = 9999
	p.selection.End.Line = 9999

	p.handleThemeChanged()

	if len(p.contentSearchMatches) == 0 {
		t.Error("search matches were not recomputed after the theme change")
	}
	if p.previewScroll > len(p.markdownRendered)-1 {
		t.Errorf("previewScroll = %d, want clamped to %d", p.previewScroll, len(p.markdownRendered)-1)
	}
	if p.selection.Active {
		t.Error("out-of-range selection should be cleared")
	}
}

func TestHandleThemeChangedPreservesValidScroll(t *testing.T) {
	p := newMarkdownPreviewPlugin(t, strings.Repeat("paragraph text\n\n", 20))
	p.previewScroll = 3
	p.handleThemeChanged()
	if p.previewScroll != 3 {
		t.Errorf("previewScroll = %d, want 3 preserved", p.previewScroll)
	}
}

func TestHandleThemeChangedIgnoresNonMarkdownPreview(t *testing.T) {
	p := &Plugin{previewFile: "main.go", previewLines: []string{"package main"}}
	p.handleThemeChanged()
	if len(p.markdownRendered) != 0 {
		t.Error("non-markdown preview should not render markdown")
	}
}
