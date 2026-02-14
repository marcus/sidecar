package filebrowser

import (
	"strings"
	"testing"
)

func TestNewProjectSearchState(t *testing.T) {
	state := NewProjectSearchState()
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.Query != "" {
		t.Errorf("expected empty query, got %q", state.Query)
	}
	if len(state.Results) != 0 {
		t.Errorf("expected empty results, got %d", len(state.Results))
	}
	if state.Cursor != 0 {
		t.Errorf("expected cursor 0, got %d", state.Cursor)
	}
}

func TestProjectSearchState_TotalMatches(t *testing.T) {
	state := NewProjectSearchState()
	state.Results = []SearchFileResult{
		{Path: "a.go", Matches: []SearchMatch{{LineNo: 1}, {LineNo: 2}}},
		{Path: "b.go", Matches: []SearchMatch{{LineNo: 5}}},
	}
	if got := state.TotalMatches(); got != 3 {
		t.Errorf("expected 3 matches, got %d", got)
	}
}

func TestProjectSearchState_FileCount(t *testing.T) {
	state := NewProjectSearchState()
	state.Results = []SearchFileResult{
		{Path: "a.go"},
		{Path: "b.go"},
		{Path: "c.go"},
	}
	if got := state.FileCount(); got != 3 {
		t.Errorf("expected 3 files, got %d", got)
	}
}

func TestProjectSearchState_FlatLen(t *testing.T) {
	tests := []struct {
		name     string
		results  []SearchFileResult
		expected int
	}{
		{
			name:     "empty",
			results:  nil,
			expected: 0,
		},
		{
			name: "files only collapsed",
			results: []SearchFileResult{
				{Path: "a.go", Collapsed: true, Matches: []SearchMatch{{LineNo: 1}, {LineNo: 2}}},
				{Path: "b.go", Collapsed: true, Matches: []SearchMatch{{LineNo: 5}}},
			},
			expected: 2, // just the file headers
		},
		{
			name: "files expanded",
			results: []SearchFileResult{
				{Path: "a.go", Collapsed: false, Matches: []SearchMatch{{LineNo: 1}, {LineNo: 2}}},
				{Path: "b.go", Collapsed: false, Matches: []SearchMatch{{LineNo: 5}}},
			},
			expected: 5, // 2 files + 2 matches + 1 match
		},
		{
			name: "mixed collapse state",
			results: []SearchFileResult{
				{Path: "a.go", Collapsed: true, Matches: []SearchMatch{{LineNo: 1}, {LineNo: 2}}},
				{Path: "b.go", Collapsed: false, Matches: []SearchMatch{{LineNo: 5}, {LineNo: 10}}},
			},
			expected: 4, // 2 files + 0 matches (collapsed) + 2 matches
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := NewProjectSearchState()
			state.Results = tc.results
			if got := state.FlatLen(); got != tc.expected {
				t.Errorf("expected FlatLen %d, got %d", tc.expected, got)
			}
		})
	}
}

func TestProjectSearchState_FlatItem(t *testing.T) {
	state := NewProjectSearchState()
	state.Results = []SearchFileResult{
		{Path: "a.go", Collapsed: false, Matches: []SearchMatch{{LineNo: 1}, {LineNo: 2}}},
		{Path: "b.go", Collapsed: false, Matches: []SearchMatch{{LineNo: 5}}},
	}

	tests := []struct {
		idx          int
		wantFileIdx  int
		wantMatchIdx int
		wantIsFile   bool
	}{
		{idx: 0, wantFileIdx: 0, wantMatchIdx: -1, wantIsFile: true}, // a.go header
		{idx: 1, wantFileIdx: 0, wantMatchIdx: 0, wantIsFile: false}, // a.go match 1
		{idx: 2, wantFileIdx: 0, wantMatchIdx: 1, wantIsFile: false}, // a.go match 2
		{idx: 3, wantFileIdx: 1, wantMatchIdx: -1, wantIsFile: true}, // b.go header
		{idx: 4, wantFileIdx: 1, wantMatchIdx: 0, wantIsFile: false}, // b.go match 1
	}

	for _, tc := range tests {
		fileIdx, matchIdx, isFile := state.FlatItem(tc.idx)
		if fileIdx != tc.wantFileIdx || matchIdx != tc.wantMatchIdx || isFile != tc.wantIsFile {
			t.Errorf("FlatItem(%d) = (%d, %d, %v), want (%d, %d, %v)",
				tc.idx, fileIdx, matchIdx, isFile,
				tc.wantFileIdx, tc.wantMatchIdx, tc.wantIsFile)
		}
	}
}

func TestProjectSearchState_ToggleFileCollapse(t *testing.T) {
	state := NewProjectSearchState()
	state.Results = []SearchFileResult{
		{Path: "a.go", Collapsed: false, Matches: []SearchMatch{{LineNo: 1}}},
		{Path: "b.go", Collapsed: true, Matches: []SearchMatch{{LineNo: 5}}},
	}

	// Cursor on first file header
	state.Cursor = 0
	state.ToggleFileCollapse()
	if !state.Results[0].Collapsed {
		t.Error("expected first file to be collapsed")
	}

	// Toggle again
	state.ToggleFileCollapse()
	if state.Results[0].Collapsed {
		t.Error("expected first file to be expanded")
	}

	// Move cursor to match line (shouldn't toggle)
	state.Cursor = 1
	state.ToggleFileCollapse()
	if state.Results[0].Collapsed {
		t.Error("toggling on match line should not collapse file")
	}
}

func TestProjectSearchState_GetSelectedFile(t *testing.T) {
	state := NewProjectSearchState()
	state.Results = []SearchFileResult{
		{Path: "a.go", Collapsed: false, Matches: []SearchMatch{
			{LineNo: 10},
			{LineNo: 20},
		}},
		{Path: "b.go", Collapsed: false, Matches: []SearchMatch{
			{LineNo: 5},
		}},
	}

	tests := []struct {
		cursor   int
		wantPath string
		wantLine int
	}{
		{cursor: 0, wantPath: "a.go", wantLine: 0},  // file header
		{cursor: 1, wantPath: "a.go", wantLine: 10}, // first match
		{cursor: 2, wantPath: "a.go", wantLine: 20}, // second match
		{cursor: 3, wantPath: "b.go", wantLine: 0},  // file header
		{cursor: 4, wantPath: "b.go", wantLine: 5},  // match
	}

	for _, tc := range tests {
		state.Cursor = tc.cursor
		path, lineNo := state.GetSelectedFile()
		if path != tc.wantPath || lineNo != tc.wantLine {
			t.Errorf("cursor %d: got (%q, %d), want (%q, %d)",
				tc.cursor, path, lineNo, tc.wantPath, tc.wantLine)
		}
	}
}

func TestBuildRipgrepArgs(t *testing.T) {
	tests := []struct {
		name          string
		state         *ProjectSearchState
		expectContain []string
		expectExclude []string
	}{
		{
			name: "default options",
			state: &ProjectSearchState{
				Query: "test",
			},
			expectContain: []string{"--line-number", "--ignore-case", "--fixed-strings", "--", "test"},
			expectExclude: []string{"--word-regexp"},
		},
		{
			name: "case sensitive",
			state: &ProjectSearchState{
				Query:         "test",
				CaseSensitive: true,
			},
			expectContain: []string{"--line-number", "--fixed-strings"},
			expectExclude: []string{"--ignore-case"},
		},
		{
			name: "regex mode",
			state: &ProjectSearchState{
				Query:    "test.*",
				UseRegex: true,
			},
			expectContain: []string{"--line-number", "--ignore-case"},
			expectExclude: []string{"--fixed-strings"},
		},
		{
			name: "whole word",
			state: &ProjectSearchState{
				Query:     "test",
				WholeWord: true,
			},
			expectContain: []string{"--line-number", "--word-regexp"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := buildRipgrepArgs(tc.state)
			argsStr := strings.Join(args, " ")

			for _, want := range tc.expectContain {
				if !strings.Contains(argsStr, want) {
					t.Errorf("expected args to contain %q, got %v", want, args)
				}
			}

			for _, notWant := range tc.expectExclude {
				if strings.Contains(argsStr, notWant) {
					t.Errorf("expected args to NOT contain %q, got %v", notWant, args)
				}
			}
		})
	}
}

func TestParseRipgrepOutput(t *testing.T) {
	// Sample ripgrep line output (filename:line:column:content)
	lineOutput := `test.go:10:6:func TestSomething() {
test.go:20:4:// Test comment
other.go:5:5:var TestVar = 1`

	reader := strings.NewReader(lineOutput)
	results := parseRipgrepOutput(reader, 100, 4) // query "Test" has length 4

	if len(results) != 2 {
		t.Fatalf("expected 2 files, got %d", len(results))
	}

	// Check first file
	if results[0].Path != "test.go" {
		t.Errorf("expected first file path 'test.go', got %q", results[0].Path)
	}
	if len(results[0].Matches) != 2 {
		t.Errorf("expected 2 matches in first file, got %d", len(results[0].Matches))
	}
	if results[0].Matches[0].LineNo != 10 {
		t.Errorf("expected first match on line 10, got %d", results[0].Matches[0].LineNo)
	}
	// Column is 1-indexed from rg, we convert to 0-indexed: 6-1=5, end=5+4=9
	if results[0].Matches[0].ColStart != 5 || results[0].Matches[0].ColEnd != 9 {
		t.Errorf("expected match columns 5-9, got %d-%d",
			results[0].Matches[0].ColStart, results[0].Matches[0].ColEnd)
	}

	// Check second file
	if results[1].Path != "other.go" {
		t.Errorf("expected second file path 'other.go', got %q", results[1].Path)
	}
	if len(results[1].Matches) != 1 {
		t.Errorf("expected 1 match in second file, got %d", len(results[1].Matches))
	}
}

func TestParseRipgrepOutput_MaxMatches(t *testing.T) {
	// Generate many matches in line format
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("test.go:1:1:line content x\n")
	}

	reader := strings.NewReader(sb.String())
	results := parseRipgrepOutput(reader, 10, 1) // Limit to 10, query length 1

	totalMatches := 0
	for _, f := range results {
		totalMatches += len(f.Matches)
	}

	if totalMatches > 10 {
		t.Errorf("expected at most 10 matches, got %d", totalMatches)
	}
}

func TestProjectSearchState_FirstMatchIndex(t *testing.T) {
	tests := []struct {
		name     string
		results  []SearchFileResult
		expected int
	}{
		{
			name:     "empty results",
			results:  nil,
			expected: 0,
		},
		{
			name: "single file with matches",
			results: []SearchFileResult{
				{Path: "a.go", Matches: []SearchMatch{{LineNo: 1}, {LineNo: 2}}},
			},
			expected: 1, // Skip file header at 0, first match at 1
		},
		{
			name: "multiple files",
			results: []SearchFileResult{
				{Path: "a.go", Matches: []SearchMatch{{LineNo: 1}}},
				{Path: "b.go", Matches: []SearchMatch{{LineNo: 5}}},
			},
			expected: 1, // First match of first file
		},
		{
			name: "first file collapsed",
			results: []SearchFileResult{
				{Path: "a.go", Collapsed: true, Matches: []SearchMatch{{LineNo: 1}}},
				{Path: "b.go", Collapsed: false, Matches: []SearchMatch{{LineNo: 5}}},
			},
			expected: 2, // Skip collapsed file (idx 0), skip second file header (idx 1), first match at 2
		},
		{
			name: "all files collapsed",
			results: []SearchFileResult{
				{Path: "a.go", Collapsed: true, Matches: []SearchMatch{{LineNo: 1}}},
				{Path: "b.go", Collapsed: true, Matches: []SearchMatch{{LineNo: 5}}},
			},
			expected: 0, // No visible matches, fallback to 0
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := NewProjectSearchState()
			state.Results = tc.results
			if got := state.FirstMatchIndex(); got != tc.expected {
				t.Errorf("FirstMatchIndex() = %d, want %d", got, tc.expected)
			}
		})
	}
}

func TestProjectSearchState_NextMatchIndex(t *testing.T) {
	state := NewProjectSearchState()
	state.Results = []SearchFileResult{
		{Path: "a.go", Matches: []SearchMatch{{LineNo: 1}, {LineNo: 2}}},
		{Path: "b.go", Matches: []SearchMatch{{LineNo: 5}}},
	}
	// Flat structure: 0=a.go, 1=match1, 2=match2, 3=b.go, 4=match3

	tests := []struct {
		cursor   int
		expected int
	}{
		{cursor: 0, expected: 1}, // From file header, next match is 1
		{cursor: 1, expected: 2}, // From match1, next is match2
		{cursor: 2, expected: 4}, // From match2, skip file header at 3, next is match at 4
		{cursor: 3, expected: 4}, // From file header, next match is 4
		{cursor: 4, expected: 4}, // At last match, stay there
	}

	for _, tc := range tests {
		state.Cursor = tc.cursor
		if got := state.NextMatchIndex(); got != tc.expected {
			t.Errorf("cursor %d: NextMatchIndex() = %d, want %d", tc.cursor, got, tc.expected)
		}
	}
}

func TestProjectSearchState_PrevMatchIndex(t *testing.T) {
	state := NewProjectSearchState()
	state.Results = []SearchFileResult{
		{Path: "a.go", Matches: []SearchMatch{{LineNo: 1}, {LineNo: 2}}},
		{Path: "b.go", Matches: []SearchMatch{{LineNo: 5}}},
	}
	// Flat structure: 0=a.go, 1=match1, 2=match2, 3=b.go, 4=match3

	tests := []struct {
		cursor   int
		expected int
	}{
		{cursor: 4, expected: 2}, // From last match, skip file header at 3, prev is match2 at 2
		{cursor: 3, expected: 2}, // From file header, prev match is 2
		{cursor: 2, expected: 1}, // From match2, prev is match1
		{cursor: 1, expected: 1}, // At first match, stay there
		{cursor: 0, expected: 0}, // At file header before any match, stay there
	}

	for _, tc := range tests {
		state.Cursor = tc.cursor
		if got := state.PrevMatchIndex(); got != tc.expected {
			t.Errorf("cursor %d: PrevMatchIndex() = %d, want %d", tc.cursor, got, tc.expected)
		}
	}
}

func TestProjectSearchState_NearestMatchIndex(t *testing.T) {
	state := NewProjectSearchState()
	state.Results = []SearchFileResult{
		{Path: "a.go", Matches: []SearchMatch{{LineNo: 1}, {LineNo: 2}}},
		{Path: "b.go", Matches: []SearchMatch{{LineNo: 5}}},
	}
	// Flat structure: 0=a.go, 1=match1, 2=match2, 3=b.go, 4=match3

	tests := []struct {
		fromIdx  int
		expected int
	}{
		{fromIdx: 0, expected: 1}, // From file header, nearest match is 1
		{fromIdx: 1, expected: 1}, // Already on match
		{fromIdx: 2, expected: 2}, // Already on match
		{fromIdx: 3, expected: 4}, // From file header, nearest match forward is 4
		{fromIdx: 4, expected: 4}, // Already on match
		{fromIdx: 5, expected: 4}, // Beyond end, search backward to 4
	}

	for _, tc := range tests {
		if got := state.NearestMatchIndex(tc.fromIdx); got != tc.expected {
			t.Errorf("NearestMatchIndex(%d) = %d, want %d", tc.fromIdx, got, tc.expected)
		}
	}
}
