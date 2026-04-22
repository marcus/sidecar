package jjstatus

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	jjvcs "github.com/marcus/sidecar/internal/vcs/jj"
)

func TestRenderGraphPrefix_Linear(t *testing.T) {
	commit := &jjvcs.StructuredCommit{CommitID: "aaa", IsWorkingCopy: true}
	row := jjvcs.GraphRow{
		Commit: commit,
		Column: 0,
		Lanes:  []string{"aaa"},
	}
	prefix := renderGraphPrefix(row, 1)
	stripped := ansi.Strip(prefix)
	if !strings.Contains(stripped, "⬤") {
		t.Fatalf("expected working copy glyph ⬤, got %q", stripped)
	}
}

func TestRenderGraphPrefix_Immutable(t *testing.T) {
	commit := &jjvcs.StructuredCommit{CommitID: "bbb", IsImmutable: true}
	row := jjvcs.GraphRow{
		Commit: commit,
		Column: 0,
		Lanes:  []string{"bbb"},
	}
	prefix := renderGraphPrefix(row, 1)
	stripped := ansi.Strip(prefix)
	if !strings.Contains(stripped, "◆") {
		t.Fatalf("expected immutable glyph ◆, got %q", stripped)
	}
}

func TestRenderGraphPrefix_Conflict(t *testing.T) {
	commit := &jjvcs.StructuredCommit{CommitID: "ccc", IsConflict: true}
	row := jjvcs.GraphRow{
		Commit: commit,
		Column: 0,
		Lanes:  []string{"ccc"},
	}
	prefix := renderGraphPrefix(row, 1)
	stripped := ansi.Strip(prefix)
	if !strings.Contains(stripped, "⊗") {
		t.Fatalf("expected conflict glyph ⊗, got %q", stripped)
	}
}

func TestRenderGraphPrefix_Connector(t *testing.T) {
	row := jjvcs.GraphRow{
		Commit:    nil,
		Column:    0,
		Lanes:     []string{"aaa"},
		MergeFrom: -1,
		ForkTo:    -1,
	}
	prefix := renderGraphPrefix(row, 1)
	stripped := ansi.Strip(prefix)
	if !strings.Contains(stripped, "║") {
		t.Fatalf("expected connector ║, got %q", stripped)
	}
}

func TestRenderCommitMeta_WithBookmarks(t *testing.T) {
	commit := &jjvcs.StructuredCommit{
		Description: "fix: some bug",
		Timestamp:   "2 days ago",
		Bookmarks:   "main feature",
	}
	meta := renderCommitMeta(commit, 60)
	stripped := ansi.Strip(meta)
	if !strings.Contains(stripped, "main") {
		t.Fatalf("expected bookmark main in output, got %q", stripped)
	}
	if !strings.Contains(stripped, "fix: some bug") {
		t.Fatalf("expected description in output, got %q", stripped)
	}
	if !strings.Contains(stripped, "2 days ago") {
		t.Fatalf("expected timestamp in output, got %q", stripped)
	}
}

func TestRenderCommitDescLines_Wraps(t *testing.T) {
	commit := &jjvcs.StructuredCommit{
		Description: "fix: this is a very long description that should wrap to multiple lines when rendered",
	}
	lines := renderCommitDescLines(commit, 30)
	if len(lines) < 2 {
		t.Fatalf("expected description to wrap to 2+ lines at width 30, got %d lines: %v", len(lines), lines)
	}
	// All text should be present across all lines.
	joined := ""
	for _, l := range lines {
		joined += ansi.Strip(l) + " "
	}
	if !strings.Contains(joined, "very long description") {
		t.Fatalf("expected full description text in wrapped output, got %q", joined)
	}
}

func TestRenderCommitDescLines_NoWrapShort(t *testing.T) {
	commit := &jjvcs.StructuredCommit{
		Description: "short desc",
	}
	lines := renderCommitDescLines(commit, 60)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line for short description, got %d", len(lines))
	}
}

func TestRenderCustomLogList_WrapsDescription(t *testing.T) {
	commits := []jjvcs.StructuredCommit{
		{
			ChangeID:      "aabbccdd",
			CommitID:      "112233445566",
			Description:   "feat: implement a feature with a very long description that definitely exceeds the narrow width we are testing with here",
			Author:        "user@test.com",
			Timestamp:     "1 day ago",
			IsWorkingCopy: true,
			Parents:       []string{"parent123456"},
		},
	}
	rows := jjvcs.LayoutGraph(commits)
	// Narrow width (40) should force description wrapping.
	output := renderCustomLogList(rows, 40, 20, 0, 0, false)
	stripped := ansi.Strip(output)
	lineCount := strings.Count(stripped, "\n") + 1
	// Should produce more than 2 lines (header + wrapped desc lines).
	if lineCount < 3 {
		t.Fatalf("expected 3+ lines for wrapped description at width 40, got %d:\n%s", lineCount, stripped)
	}
	// Full description text should be present (not truncated).
	if !strings.Contains(stripped, "exceeds") {
		t.Fatalf("expected full description text to be present, got:\n%s", stripped)
	}
}

func TestRenderCommitHeaderLine_IDHighlighting(t *testing.T) {
	commit := &jjvcs.StructuredCommit{
		ChangeID:      "pnrtzttk",
		ChangeIDShort: "pn",
		Timestamp:     "1 day ago",
	}
	header := renderCommitHeaderLine(commit, 60)
	stripped := ansi.Strip(header)
	// The full change ID should appear in the output.
	if !strings.Contains(stripped, "pnrtzttk") {
		t.Fatalf("expected full change ID in output, got %q", stripped)
	}
	// The raw output (with ANSI) should contain both prefix and suffix portions
	// rendered separately (different escape sequences).
	if !strings.Contains(header, "pn") {
		t.Fatalf("expected highlighted prefix 'pn' in ANSI output, got %q", header)
	}
	if !strings.Contains(header, "rtzttk") {
		t.Fatalf("expected dimmed suffix 'rtzttk' in ANSI output, got %q", header)
	}
}

func TestRenderCommitHeaderLine_IDNoShort(t *testing.T) {
	// When ChangeIDShort is empty, the full ID should still render.
	commit := &jjvcs.StructuredCommit{
		ChangeID:  "abcdefgh",
		Timestamp: "2 days ago",
	}
	header := renderCommitHeaderLine(commit, 60)
	stripped := ansi.Strip(header)
	if !strings.Contains(stripped, "abcdefgh") {
		t.Fatalf("expected full change ID, got %q", stripped)
	}
}

func TestRenderCommitMeta_Empty(t *testing.T) {
	commit := &jjvcs.StructuredCommit{
		Description: "(no description set)",
		IsEmpty:     true,
		Timestamp:   "1 hour ago",
	}
	meta := renderCommitMeta(commit, 60)
	stripped := ansi.Strip(meta)
	if !strings.Contains(stripped, "empty") {
		t.Fatalf("expected empty indicator in output, got %q", stripped)
	}
}
