package gitstatus

import (
	"testing"
)

func TestParseUnifiedDiff_BasicDiff(t *testing.T) {
	diff := `diff --git a/file.go b/file.go
index abc123..def456 100644
--- a/file.go
+++ b/file.go
@@ -1,3 +1,4 @@
 package main
+
 func foo() {
 }
`

	parsed, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.OldFile != "file.go" {
		t.Errorf("OldFile = %q, want %q", parsed.OldFile, "file.go")
	}
	if parsed.NewFile != "file.go" {
		t.Errorf("NewFile = %q, want %q", parsed.NewFile, "file.go")
	}
	if len(parsed.Hunks) != 1 {
		t.Fatalf("len(Hunks) = %d, want 1", len(parsed.Hunks))
	}

	hunk := parsed.Hunks[0]
	if hunk.OldStart != 1 || hunk.OldCount != 3 {
		t.Errorf("old range = %d,%d, want 1,3", hunk.OldStart, hunk.OldCount)
	}
	if hunk.NewStart != 1 || hunk.NewCount != 4 {
		t.Errorf("new range = %d,%d, want 1,4", hunk.NewStart, hunk.NewCount)
	}
}

func TestParseUnifiedDiff_MultipleHunks(t *testing.T) {
	diff := `--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,3 @@
 line1
-old
+new
 line3
@@ -10,3 +10,4 @@
 line10
+added
 line11
 line12
`

	parsed, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(parsed.Hunks) != 2 {
		t.Fatalf("len(Hunks) = %d, want 2", len(parsed.Hunks))
	}

	if parsed.Hunks[0].OldStart != 1 {
		t.Errorf("first hunk OldStart = %d, want 1", parsed.Hunks[0].OldStart)
	}
	if parsed.Hunks[1].OldStart != 10 {
		t.Errorf("second hunk OldStart = %d, want 10", parsed.Hunks[1].OldStart)
	}
}

func TestParseUnifiedDiff_BinaryFile(t *testing.T) {
	diff := `Binary files a/image.png and b/image.png differ`

	parsed, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !parsed.Binary {
		t.Error("expected Binary = true")
	}
}

func TestParseUnifiedDiff_LineTypes(t *testing.T) {
	// Note: no trailing newline to avoid empty context line
	diff := `--- a/file.txt
+++ b/file.txt
@@ -1,4 +1,4 @@
 context
-removed
+added
 more context`

	parsed, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(parsed.Hunks) != 1 {
		t.Fatalf("len(Hunks) = %d, want 1", len(parsed.Hunks))
	}

	lines := parsed.Hunks[0].Lines
	if len(lines) != 4 {
		t.Fatalf("len(Lines) = %d, want 4", len(lines))
	}

	// Check line types
	if lines[0].Type != LineContext {
		t.Errorf("line 0 type = %v, want LineContext", lines[0].Type)
	}
	if lines[1].Type != LineRemove {
		t.Errorf("line 1 type = %v, want LineRemove", lines[1].Type)
	}
	if lines[2].Type != LineAdd {
		t.Errorf("line 2 type = %v, want LineAdd", lines[2].Type)
	}
	if lines[3].Type != LineContext {
		t.Errorf("line 3 type = %v, want LineContext", lines[3].Type)
	}
}

func TestParseUnifiedDiff_LineNumbers(t *testing.T) {
	diff := `--- a/file.txt
+++ b/file.txt
@@ -5,4 +5,4 @@
 context
-removed
+added
 more context
`

	parsed, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := parsed.Hunks[0].Lines

	// Context line: both line numbers
	if lines[0].OldLineNo != 5 || lines[0].NewLineNo != 5 {
		t.Errorf("context line numbers = %d,%d, want 5,5", lines[0].OldLineNo, lines[0].NewLineNo)
	}

	// Removed line: only old line number
	if lines[1].OldLineNo != 6 || lines[1].NewLineNo != 0 {
		t.Errorf("removed line numbers = %d,%d, want 6,0", lines[1].OldLineNo, lines[1].NewLineNo)
	}

	// Added line: only new line number
	if lines[2].OldLineNo != 0 || lines[2].NewLineNo != 6 {
		t.Errorf("added line numbers = %d,%d, want 0,6", lines[2].OldLineNo, lines[2].NewLineNo)
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"hello world", []string{"hello", " ", "world"}},
		{"a  b", []string{"a", "  ", "b"}},
		{"\tfoo", []string{"\t", "foo"}},
		{"", nil},
		{"   ", []string{"   "}},
		{"word", []string{"word"}},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := tokenize(tc.input)
			if len(got) != len(tc.want) {
				t.Errorf("len = %d, want %d", len(got), len(tc.want))
				t.Errorf("got: %v", got)
				return
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("token[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParsedDiff_TotalLines(t *testing.T) {
	// No trailing newline
	diff := `--- a/file.txt
+++ b/file.txt
@@ -1,2 +1,2 @@
 context
-old
+new`

	parsed, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3 content lines + 1 hunk header
	if parsed.TotalLines() != 4 {
		t.Errorf("TotalLines() = %d, want 4", parsed.TotalLines())
	}
}

func TestParsedDiff_MaxLineNumber(t *testing.T) {
	// No trailing newline
	diff := `--- a/file.txt
+++ b/file.txt
@@ -100,2 +100,3 @@
 context
+added
 more`

	parsed, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Max line number should be 102 (100 + 2 context lines)
	max := parsed.MaxLineNumber()
	if max != 102 {
		t.Errorf("MaxLineNumber() = %d, want 102", max)
	}
}

func TestBuildFullFileDiff_IdenticalFiles(t *testing.T) {
	oldContent := "line1\nline2\nline3\n"
	newContent := "line1\nline2\nline3\n"
	parsed := &ParsedDiff{OldFile: "test.go", NewFile: "test.go"}

	ffd := BuildFullFileDiff(oldContent, newContent, parsed)

	if ffd == nil {
		t.Fatal("BuildFullFileDiff returned nil")
	}
	if len(ffd.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(ffd.Lines))
	}
	for i, fl := range ffd.Lines {
		if fl.Type != LineContext {
			t.Errorf("line %d: expected LineContext, got %v", i, fl.Type)
		}
		if fl.OldLineNo != i+1 {
			t.Errorf("line %d: OldLineNo = %d, want %d", i, fl.OldLineNo, i+1)
		}
		if fl.NewLineNo != i+1 {
			t.Errorf("line %d: NewLineNo = %d, want %d", i, fl.NewLineNo, i+1)
		}
	}
}

func TestBuildFullFileDiff_SimpleChange(t *testing.T) {
	oldContent := "line1\nold\nline3\n"
	newContent := "line1\nnew\nline3\n"
	// Use diff without trailing newline after last context line to avoid
	// the parser generating an extra empty context line.
	diff := `--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,3 @@
 line1
-old
+new
 line3`

	parsed, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	ffd := BuildFullFileDiff(oldContent, newContent, parsed)

	if ffd == nil {
		t.Fatal("BuildFullFileDiff returned nil")
	}
	if len(ffd.Lines) != 4 {
		t.Fatalf("expected 4 lines (context + remove + add + context), got %d", len(ffd.Lines))
	}

	// line1 - context
	if ffd.Lines[0].Type != LineContext || ffd.Lines[0].OldText != "line1" {
		t.Errorf("line 0: type=%v text=%q, want context/line1", ffd.Lines[0].Type, ffd.Lines[0].OldText)
	}
	// old - removed
	if ffd.Lines[1].Type != LineRemove || ffd.Lines[1].OldText != "old" {
		t.Errorf("line 1: type=%v text=%q, want remove/old", ffd.Lines[1].Type, ffd.Lines[1].OldText)
	}
	if ffd.Lines[1].NewLineNo != 0 {
		t.Errorf("line 1: removed line should have NewLineNo=0, got %d", ffd.Lines[1].NewLineNo)
	}
	// new - added
	if ffd.Lines[2].Type != LineAdd || ffd.Lines[2].NewText != "new" {
		t.Errorf("line 2: type=%v text=%q, want add/new", ffd.Lines[2].Type, ffd.Lines[2].NewText)
	}
	if ffd.Lines[2].OldLineNo != 0 {
		t.Errorf("line 2: added line should have OldLineNo=0, got %d", ffd.Lines[2].OldLineNo)
	}
	// line3 - context
	if ffd.Lines[3].Type != LineContext || ffd.Lines[3].OldText != "line3" {
		t.Errorf("line 3: type=%v text=%q, want context/line3", ffd.Lines[3].Type, ffd.Lines[3].OldText)
	}
}

func TestBuildFullFileDiff_NewFile(t *testing.T) {
	oldContent := ""
	newContent := "hello\nworld\n"
	// No trailing newline after last +line to avoid parser empty context line
	diff := `--- /dev/null
+++ b/newfile.txt
@@ -0,0 +1,2 @@
+hello
+world`

	parsed, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	ffd := BuildFullFileDiff(oldContent, newContent, parsed)

	if ffd == nil {
		t.Fatal("BuildFullFileDiff returned nil")
	}
	if len(ffd.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(ffd.Lines))
	}
	for _, fl := range ffd.Lines {
		if fl.Type != LineAdd {
			t.Errorf("expected LineAdd, got %v", fl.Type)
		}
		if fl.OldLineNo != 0 {
			t.Errorf("new file lines should have OldLineNo=0, got %d", fl.OldLineNo)
		}
	}
}

func TestBuildFullFileDiff_MultipleHunks(t *testing.T) {
	// 10-line file with changes at line 2 and line 8
	oldContent := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"
	newContent := "1\nTWO\n3\n4\n5\n6\n7\nEIGHT\n9\n10\n"
	diff := `--- a/file.txt
+++ b/file.txt
@@ -1,4 +1,4 @@
 1
-2
+TWO
 3
 4
@@ -7,4 +7,4 @@
 7
-8
+EIGHT
 9
 10
`
	parsed, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	ffd := BuildFullFileDiff(oldContent, newContent, parsed)

	if ffd == nil {
		t.Fatal("BuildFullFileDiff returned nil")
	}

	// Count types
	contextCount, removeCount, addCount := 0, 0, 0
	for _, fl := range ffd.Lines {
		switch fl.Type {
		case LineContext:
			contextCount++
		case LineRemove:
			removeCount++
		case LineAdd:
			addCount++
		}
	}

	if removeCount != 2 {
		t.Errorf("expected 2 removes, got %d", removeCount)
	}
	if addCount != 2 {
		t.Errorf("expected 2 adds, got %d", addCount)
	}
	// 10 original lines - 2 removed + 2 removed-as-context-between-hunks = 12 total
	// Actually: context lines from hunks + gap between hunks + removes + adds
	if contextCount+removeCount+addCount != len(ffd.Lines) {
		t.Errorf("line count mismatch: %d context + %d remove + %d add != %d total",
			contextCount, removeCount, addCount, len(ffd.Lines))
	}
}

func TestBuildFullFileDiff_EmptyOldContent(t *testing.T) {
	ffd := BuildFullFileDiff("", "hello\nworld\n", &ParsedDiff{})
	if ffd == nil {
		t.Fatal("BuildFullFileDiff returned nil")
	}
	// No hunks → context lines from new content only
	if len(ffd.Lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(ffd.Lines))
	}
}

func TestFullFileDiff_TotalLines(t *testing.T) {
	ffd := &FullFileDiff{
		Lines: make([]FullFileLine, 42),
	}
	if ffd.TotalLines() != 42 {
		t.Errorf("TotalLines() = %d, want 42", ffd.TotalLines())
	}
}

func TestFullFileDiff_TotalLines_Nil(t *testing.T) {
	var ffd *FullFileDiff
	if ffd.TotalLines() != 0 {
		t.Errorf("TotalLines() on nil = %d, want 0", ffd.TotalLines())
	}
}

func TestBuildFullFileDiff_BinaryFile(t *testing.T) {
	parsed := &ParsedDiff{Binary: true}
	ffd := BuildFullFileDiff("old", "new", parsed)
	if ffd != nil {
		t.Error("BuildFullFileDiff should return nil for binary files")
	}
}

func TestBuildFullFileDiff_NilParsed(t *testing.T) {
	ffd := BuildFullFileDiff("line1\nline2\n", "line1\nline2\n", nil)
	if ffd == nil {
		t.Fatal("BuildFullFileDiff returned nil for nil parsed")
	}
	// Should produce context lines from both files
	if len(ffd.Lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(ffd.Lines))
	}
}

func TestSplitFileLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single line no newline", "hello", 1},
		{"single line with newline", "hello\n", 1},
		{"two lines", "a\nb\n", 2},
		{"three lines no trailing", "a\nb\nc", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitFileLines(tt.input)
			if len(got) != tt.want {
				t.Errorf("splitFileLines(%q) = %d lines, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestSafeIndex(t *testing.T) {
	lines := []string{"a", "b", "c"}
	if safeIndex(lines, 0) != "a" {
		t.Error("safeIndex(0) should be 'a'")
	}
	if safeIndex(lines, 3) != "" {
		t.Error("safeIndex(3) should be ''")
	}
	if safeIndex(lines, -1) != "" {
		t.Error("safeIndex(-1) should be ''")
	}
	if safeIndex(nil, 0) != "" {
		t.Error("safeIndex(nil, 0) should be ''")
	}
}
