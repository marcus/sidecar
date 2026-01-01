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
		idx         int
		wantFileIdx int
		wantMatchIdx int
		wantIsFile  bool
	}{
		{idx: 0, wantFileIdx: 0, wantMatchIdx: -1, wantIsFile: true},  // a.go header
		{idx: 1, wantFileIdx: 0, wantMatchIdx: 0, wantIsFile: false},  // a.go match 1
		{idx: 2, wantFileIdx: 0, wantMatchIdx: 1, wantIsFile: false},  // a.go match 2
		{idx: 3, wantFileIdx: 1, wantMatchIdx: -1, wantIsFile: true},  // b.go header
		{idx: 4, wantFileIdx: 1, wantMatchIdx: 0, wantIsFile: false},  // b.go match 1
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
		{cursor: 0, wantPath: "a.go", wantLine: 0},   // file header
		{cursor: 1, wantPath: "a.go", wantLine: 10},  // first match
		{cursor: 2, wantPath: "a.go", wantLine: 20},  // second match
		{cursor: 3, wantPath: "b.go", wantLine: 0},   // file header
		{cursor: 4, wantPath: "b.go", wantLine: 5},   // match
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
			expectContain: []string{"--json", "--ignore-case", "--fixed-strings", "--", "test"},
			expectExclude: []string{"--word-regexp"},
		},
		{
			name: "case sensitive",
			state: &ProjectSearchState{
				Query:         "test",
				CaseSensitive: true,
			},
			expectContain: []string{"--json", "--fixed-strings"},
			expectExclude: []string{"--ignore-case"},
		},
		{
			name: "regex mode",
			state: &ProjectSearchState{
				Query:    "test.*",
				UseRegex: true,
			},
			expectContain: []string{"--json", "--ignore-case"},
			expectExclude: []string{"--fixed-strings"},
		},
		{
			name: "whole word",
			state: &ProjectSearchState{
				Query:     "test",
				WholeWord: true,
			},
			expectContain: []string{"--json", "--word-regexp"},
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
	// Sample ripgrep JSON output
	jsonOutput := `{"type":"begin","data":{"path":{"text":"test.go"}}}
{"type":"match","data":{"path":{"text":"test.go"},"lines":{"text":"func TestSomething() {\n"},"line_number":10,"submatches":[{"match":{"text":"Test"},"start":5,"end":9}]}}
{"type":"match","data":{"path":{"text":"test.go"},"lines":{"text":"// Test comment\n"},"line_number":20,"submatches":[{"match":{"text":"Test"},"start":3,"end":7}]}}
{"type":"end","data":{"path":{"text":"test.go"},"stats":{}}}
{"type":"begin","data":{"path":{"text":"other.go"}}}
{"type":"match","data":{"path":{"text":"other.go"},"lines":{"text":"var TestVar = 1\n"},"line_number":5,"submatches":[{"match":{"text":"Test"},"start":4,"end":8}]}}
{"type":"end","data":{"path":{"text":"other.go"},"stats":{}}}`

	reader := strings.NewReader(jsonOutput)
	results := parseRipgrepOutput(reader, 100)

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
	// Generate many matches
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString(`{"type":"match","data":{"path":{"text":"test.go"},"lines":{"text":"line content\n"},"line_number":`)
		sb.WriteString(strings.Repeat("1", 1))
		sb.WriteString(`,"submatches":[{"match":{"text":"x"},"start":0,"end":1}]}}`)
		sb.WriteString("\n")
	}

	reader := strings.NewReader(sb.String())
	results := parseRipgrepOutput(reader, 10) // Limit to 10

	totalMatches := 0
	for _, f := range results {
		totalMatches += len(f.Matches)
	}

	if totalMatches > 10 {
		t.Errorf("expected at most 10 matches, got %d", totalMatches)
	}
}
