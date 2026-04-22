package jj

import "testing"

func TestParseStatusOutput(t *testing.T) {
	out := `Working copy changes:
A cmd/sidecar/main.go
M internal/plugins/gitstatus/plugin.go
D old/path.txt
R before.go -> after.go
Working copy : qqqqqqqq abcdef01 (no description set)
Parent commit: pppppppp base message`

	s := ParseStatusOutput(out)
	if len(s.Changes) != 4 {
		t.Fatalf("expected 4 changes, got %d", len(s.Changes))
	}

	if s.Changes[0].Kind != ChangeAdded || s.Changes[0].Path != "cmd/sidecar/main.go" {
		t.Fatalf("unexpected first change: %+v", s.Changes[0])
	}
	if s.Changes[1].Kind != ChangeModified || s.Changes[1].Path != "internal/plugins/gitstatus/plugin.go" {
		t.Fatalf("unexpected second change: %+v", s.Changes[1])
	}
	if s.Changes[2].Kind != ChangeDeleted || s.Changes[2].Path != "old/path.txt" {
		t.Fatalf("unexpected third change: %+v", s.Changes[2])
	}
	if s.Changes[3].Kind != ChangeRenamed || s.Changes[3].Path != "before.go -> after.go" {
		t.Fatalf("unexpected fourth change: %+v", s.Changes[3])
	}
}

func TestParseLogOutput(t *testing.T) {
	out := "chg11111\x1fabcdefff\x1ffirst commit\nchg22222\x1f12345678\x1fsecond commit\ninvalid-line\n"
	commits := ParseLogOutput(out)
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[1].ChangeID != "chg22222" || commits[1].CommitID != "12345678" || commits[1].Subject != "second commit" {
		t.Fatalf("unexpected second commit: %+v", commits[1])
	}
}

func TestParseLogGraphOutput(t *testing.T) {
	out := "@  abcdef12 user date abcdef12\n│  subject\n◆  feedbeef user date feedbeef\n~\n"
	graph := ParseLogGraphOutput(out)
	if len(graph.Lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(graph.Lines))
	}
	if len(graph.Commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(graph.Commits))
	}
	if graph.Commits[0].CommitID != "abcdef12" || graph.Commits[0].LineIndex != 0 {
		t.Fatalf("unexpected first graph commit: %+v", graph.Commits[0])
	}
	if graph.Commits[1].CommitID != "feedbeef" || graph.Commits[1].LineIndex != 2 {
		t.Fatalf("unexpected second graph commit: %+v", graph.Commits[1])
	}
}

func TestParseLogGraphOutputWithANSI(t *testing.T) {
	out := "\x1b[31m@\x1b[0m  abcdef12 user date \x1b[32mabcdef12\x1b[0m\n"
	graph := ParseLogGraphOutput(out)
	if len(graph.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(graph.Commits))
	}
	if graph.Commits[0].CommitID != "abcdef12" || graph.Commits[0].LineIndex != 0 {
		t.Fatalf("unexpected graph commit: %+v", graph.Commits[0])
	}
}

func TestParseStructuredLogOutput(t *testing.T) {
	out := "pnrtzttk\x1fcb5b94f6c470\x1fdocs: add design\x1fuser@test.com\x1f4 days ago\x1fmain feature\x1f\x1f\x1f\x1fwc\x1f8413c957dd52\x1fp\n" +
		"xzkrospr\x1faa0dfefc6d5d\x1ffix: trailing punctuation\x1fdev@test.com\x1f1 week ago\x1fmain@origin\x1f\x1f\x1fimmutable\x1f\x1fa5a88f989737\x1fxz\n"
	commits := ParseStructuredLogOutput(out)
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	c0 := commits[0]
	if c0.ChangeID != "pnrtzttk" {
		t.Fatalf("expected ChangeID pnrtzttk, got %q", c0.ChangeID)
	}
	if c0.ChangeIDShort != "p" {
		t.Fatalf("expected ChangeIDShort p, got %q", c0.ChangeIDShort)
	}
	if c0.CommitID != "cb5b94f6c470" {
		t.Fatalf("expected CommitID cb5b94f6c470, got %q", c0.CommitID)
	}
	if c0.Description != "docs: add design" {
		t.Fatalf("expected description, got %q", c0.Description)
	}
	if c0.Author != "user@test.com" {
		t.Fatalf("expected author, got %q", c0.Author)
	}
	if c0.Timestamp != "4 days ago" {
		t.Fatalf("expected timestamp, got %q", c0.Timestamp)
	}
	if c0.Bookmarks != "main feature" {
		t.Fatalf("expected bookmarks, got %q", c0.Bookmarks)
	}
	if !c0.IsWorkingCopy {
		t.Fatal("expected IsWorkingCopy true")
	}
	if c0.IsImmutable {
		t.Fatal("expected IsImmutable false")
	}
	if len(c0.Parents) != 1 || c0.Parents[0] != "8413c957dd52" {
		t.Fatalf("expected parents [8413c957dd52], got %v", c0.Parents)
	}

	c1 := commits[1]
	if !c1.IsImmutable {
		t.Fatal("expected IsImmutable true")
	}
	if c1.IsWorkingCopy {
		t.Fatal("expected IsWorkingCopy false")
	}
	if c1.Bookmarks != "main@origin" {
		t.Fatalf("expected bookmarks main@origin, got %q", c1.Bookmarks)
	}
	if c1.ChangeIDShort != "xz" {
		t.Fatalf("expected ChangeIDShort xz, got %q", c1.ChangeIDShort)
	}
}

func TestParseStructuredLogOutput_EmptyConflict(t *testing.T) {
	out := "aaaaaaaa\x1fbbbbbbbbbbbb\x1f\x1fuser@test.com\x1f1 hour ago\x1f\x1fempty\x1fconflict\x1f\x1f\x1fcccccccccccc\n"
	commits := ParseStructuredLogOutput(out)
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}
	if !commits[0].IsEmpty {
		t.Fatal("expected IsEmpty true")
	}
	if !commits[0].IsConflict {
		t.Fatal("expected IsConflict true")
	}
}
