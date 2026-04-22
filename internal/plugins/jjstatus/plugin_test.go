package jjstatus

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	jjvcs "github.com/marcus/sidecar/internal/vcs/jj"
)

func TestRenderCustomLogList_Basic(t *testing.T) {
	commits := []jjvcs.StructuredCommit{
		{
			ChangeID:      "pnrtzttk",
			CommitID:      "cb5b94f6c470",
			Description:   "docs: add design",
			Author:        "user@test.com",
			Timestamp:     "4 days ago",
			Bookmarks:     "main",
			IsWorkingCopy: true,
			Parents:       []string{"8413c957dd52"},
		},
		{
			ChangeID:    "xzkrospr",
			CommitID:    "8413c957dd52",
			Description: "fix: trailing punctuation",
			Timestamp:   "1 week ago",
			Bookmarks:   "main@origin",
			IsImmutable: true,
		},
	}
	rows := jjvcs.LayoutGraph(commits)
	output := renderCustomLogList(rows, 60, 20, 0, 0, false)
	stripped := ansi.Strip(output)
	if !strings.Contains(stripped, "⬤") {
		t.Fatalf("expected working copy glyph, got %q", stripped)
	}
	if !strings.Contains(stripped, "◆") {
		t.Fatalf("expected immutable glyph, got %q", stripped)
	}
	if !strings.Contains(stripped, "main") {
		t.Fatalf("expected bookmark, got %q", stripped)
	}
}
