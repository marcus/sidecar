package tty

import (
	"strings"
	"testing"
)

func TestNewOutputBuffer(t *testing.T) {
	buf := NewOutputBuffer(100)
	if buf == nil {
		t.Fatal("expected non-nil buffer")
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty buffer, got %d lines", buf.Len())
	}
}

func TestOutputBuffer_Write(t *testing.T) {
	buf := NewOutputBuffer(100)
	buf.Write("line1\nline2\nline3")

	if buf.Len() != 3 {
		t.Errorf("expected 3 lines, got %d", buf.Len())
	}

	lines := buf.Lines()
	if lines[0] != "line1" || lines[1] != "line2" || lines[2] != "line3" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestOutputBuffer_Update(t *testing.T) {
	buf := NewOutputBuffer(100)

	// First write should return true
	changed := buf.Update("hello\nworld")
	if !changed {
		t.Error("expected changed=true for initial write")
	}

	// Same content should return false
	changed = buf.Update("hello\nworld")
	if changed {
		t.Error("expected changed=false for same content")
	}

	// Different content should return true
	changed = buf.Update("hello\nuniverse")
	if !changed {
		t.Error("expected changed=true for different content")
	}
}

func TestOutputBuffer_Capacity(t *testing.T) {
	buf := NewOutputBuffer(3)

	// Write more lines than capacity
	buf.Write("line1\nline2\nline3\nline4\nline5")

	if buf.Len() != 3 {
		t.Errorf("expected 3 lines (capacity), got %d", buf.Len())
	}

	lines := buf.Lines()
	// Should keep most recent lines
	if lines[0] != "line3" || lines[1] != "line4" || lines[2] != "line5" {
		t.Errorf("expected most recent lines, got: %v", lines)
	}
}

func TestOutputBuffer_StripMouseSequences(t *testing.T) {
	buf := NewOutputBuffer(100)

	// Content with mouse escape sequences
	content := "hello\x1b[<65;83;33Mworld"
	buf.Write(content)

	// Mouse sequences should be stripped
	result := buf.String()
	if strings.Contains(result, "\x1b[<") {
		t.Error("expected mouse sequences to be stripped")
	}
	if !strings.Contains(result, "hello") || !strings.Contains(result, "world") {
		t.Error("expected content to be preserved")
	}
}

func TestOutputBuffer_StripTerminalModeSequences(t *testing.T) {
	buf := NewOutputBuffer(100)

	// Content with terminal mode sequences
	content := "hello\x1b[?2004hworld"
	buf.Write(content)

	result := buf.String()
	if strings.Contains(result, "\x1b[?2004h") {
		t.Error("expected terminal mode sequences to be stripped")
	}
}

func TestOutputBuffer_LinesRange(t *testing.T) {
	buf := NewOutputBuffer(100)
	buf.Write("line0\nline1\nline2\nline3\nline4")

	lines := buf.LinesRange(1, 3)
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "line1" || lines[1] != "line2" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestOutputBuffer_Clear(t *testing.T) {
	buf := NewOutputBuffer(100)
	buf.Write("hello\nworld")

	if buf.Len() != 2 {
		t.Errorf("expected 2 lines before clear, got %d", buf.Len())
	}

	buf.Clear()

	if buf.Len() != 0 {
		t.Errorf("expected 0 lines after clear, got %d", buf.Len())
	}
}

func TestPartialMouseSeqRegex(t *testing.T) {
	tests := []struct {
		input string
		match bool
	}{
		{"[<65;83;33M", true},   // scroll down
		{"[<64;10;5M", true},    // scroll up
		{"[<0;50;20m", true},    // release
		{"hello", false},        // normal text
		{"[notmouse]", false},   // not a mouse sequence
		{"[<abc;def;ghiM", false}, // invalid format
	}

	for _, tt := range tests {
		if got := PartialMouseSeqRegex.MatchString(tt.input); got != tt.match {
			t.Errorf("PartialMouseSeqRegex.MatchString(%q) = %v, want %v", tt.input, got, tt.match)
		}
	}
}

func TestOutputBuffer_StripTruncatedMouseSequence(t *testing.T) {
	buf := NewOutputBuffer(100)

	// Truncated mouse sequence missing trailing M (captured mid-transmission)
	content := "prompt> [<65;103;31"
	buf.Write(content)

	result := buf.String()
	if strings.Contains(result, "[<65;103;31") {
		t.Error("expected truncated mouse sequence to be stripped")
	}
	if !strings.Contains(result, "prompt> ") {
		t.Error("expected surrounding content to be preserved")
	}
}

func TestOutputBuffer_StripPartialMouseSequenceWithTerminator(t *testing.T) {
	buf := NewOutputBuffer(100)

	// Partial mouse sequence (no ESC) with terminator
	content := "prompt> [<65;103;31M"
	buf.Write(content)

	result := buf.String()
	if strings.Contains(result, "[<65;103;31M") {
		t.Error("expected partial mouse sequence to be stripped")
	}
	if !strings.Contains(result, "prompt> ") {
		t.Error("expected surrounding content to be preserved")
	}
}

// TestContainsMouseSequence tests the lenient mouse sequence detection (td-e2ce50)
func TestContainsMouseSequence(t *testing.T) {
	tests := []struct {
		input string
		want  bool
		desc  string
	}{
		{"[<65;143;8M", true, "complete sequence"},
		{"[<65;143;8M[<64;143;8M", true, "multiple complete sequences"},
		{"[<65;143;", true, "truncated (no M)"},
		{"8M[<65;143;8M", true, "starts mid-sequence"},
		{"[<65", true, "very truncated"},
		{"[<65;183;40M[<64;183;40M", true, "fast scroll sequence"},
		{"hello", false, "normal text"},
		{"[<notanumber", false, "not a sequence (non-numeric)"},
		{"ls -la", false, "command"},
		{"", false, "empty string"},
		{"[]", false, "empty brackets"},
		{"[test]", false, "normal brackets"},
		{"<65;143;8M", false, "missing opening bracket"},
	}

	for _, tt := range tests {
		if got := ContainsMouseSequence(tt.input); got != tt.want {
			t.Errorf("ContainsMouseSequence(%q) = %v, want %v (%s)", tt.input, got, tt.want, tt.desc)
		}
	}
}

// TestLooksLikeMouseFragment tests the very lenient fragment detection (td-e2ce50)
func TestLooksLikeMouseFragment(t *testing.T) {
	tests := []struct {
		input string
		want  bool
		desc  string
	}{
		// Very short fragments (the key improvement)
		{"[<", true, "just start marker"},
		{"[<6", true, "start + one digit"},
		{"[<64", true, "start + two digits"},
		{";1", true, "semicolon + digit (mid-sequence)"},
		{"3M", true, "digit + M (end of sequence)"},
		{"3m", true, "digit + m (end of sequence, release)"},

		// Longer sequences (should also match via ContainsMouseSequence)
		{"[<65;143;8M", true, "complete sequence"},
		{"[<65;143;8M[<64;143;8M", true, "multiple complete sequences"},
		{"[<65;143;", true, "truncated (no M)"},
		{"8M[<65;143;8M", true, "starts mid-sequence"},

		// Concatenated sequences (td-3b15ee: fast trackpad scroll pattern)
		{"[<64;107;16M[<64;107;16M[<64;107;16M", true, "many concatenated scroll events"},
		{"[<65;107;14M[<65;107;14M[<35;111;12M", true, "mixed scroll events"},
		{"M[<64;107;16M", true, "sequence starting with M (split boundary)"},
		{"m[<64;107;16M", true, "sequence starting with m (split boundary)"},

		// Split CSI sequences (just brackets arriving separately)
		{"[", true, "single bracket (CSI start)"},
		{"[[", true, "double brackets"},
		{"[[[", true, "triple brackets"},
		{"[[[[[[[[[[", true, "many brackets (burst of split CSI)"},

		// Semicolon-heavy patterns (coordinate garbage)
		{"64;107;16", true, "raw coordinates (multi-semicolon)"},
		{"65;107;14;65;107;14", true, "multiple coordinate sets"},

		// Non-matches
		{"hello", false, "normal text"},
		{"a", false, "single letter"},
		{"12", false, "just digits (no markers)"},
		{"ls", false, "command"},
		{"", false, "empty string"},
		{"[test]", false, "normal brackets without <"},
		{"M", false, "just M (no digit)"},
		{";", false, "just semicolon (no digit)"},
		{"hello world", false, "text with space"},
		{"foo;bar", false, "semicolon but no digits"},
	}

	for _, tt := range tests {
		if got := LooksLikeMouseFragment(tt.input); got != tt.want {
			t.Errorf("LooksLikeMouseFragment(%q) = %v, want %v (%s)", tt.input, got, tt.want, tt.desc)
		}
	}
}
