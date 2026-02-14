package gitstatus

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSearchCommits(t *testing.T) {
	// Setup test data
	commits := []*Commit{
		{Subject: "Fix bug in parser", Author: "Alice"},
		{Subject: "Add new feature", Author: "Bob"},
		{Subject: "Refactor database", Author: "Alice"},
		{Subject: "Update documentation", Author: "Charlie"},
		{Subject: "Fix typo", Author: "Bob"},
	}

	p := &Plugin{
		recentCommits: commits,
	}

	tests := []struct {
		name          string
		query         string
		useRegex      bool
		caseSensitive bool
		wantCount     int
		wantSubject   string // Check first match subject if count > 0
	}{
		{
			name:      "Empty query",
			query:     "",
			wantCount: 0,
		},
		{
			name:        "Simple match subject",
			query:       "feature",
			wantCount:   1,
			wantSubject: "Add new feature",
		},
		{
			name:        "Simple match author",
			query:       "Charlie",
			wantCount:   1,
			wantSubject: "Update documentation",
		},
		{
			name:          "Case insensitive match",
			query:         "alice",
			caseSensitive: false,
			wantCount:     2,
			wantSubject:   "Fix bug in parser",
		},
		{
			name:          "Case sensitive match fail",
			query:         "alice",
			caseSensitive: true,
			wantCount:     0,
		},
		{
			name:          "Case sensitive match success",
			query:         "Alice",
			caseSensitive: true,
			wantCount:     2,
			wantSubject:   "Fix bug in parser",
		},
		{
			name:        "Regex match",
			query:       "Fix.*",
			useRegex:    true,
			wantCount:   2,
			wantSubject: "Fix bug in parser",
		},
		{
			name:        "Regex match author",
			query:       "^Bob$",
			useRegex:    true,
			wantCount:   2,
			wantSubject: "Add new feature",
		},
		{
			name:      "Invalid regex",
			query:     "[",
			useRegex:  true,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := p.searchCommits(tt.query, tt.useRegex, tt.caseSensitive)
			if len(matches) != tt.wantCount {
				t.Errorf("got %d matches, want %d", len(matches), tt.wantCount)
			}
			if tt.wantCount > 0 && matches[0].Subject != tt.wantSubject {
				t.Errorf("first match subject = %q, want %q", matches[0].Subject, tt.wantSubject)
			}
		})
	}
}

func TestUpdatePathFilter(t *testing.T) {
	p := &Plugin{
		pathFilterMode:  true,
		pathFilterInput: "src",
	}

	// Test backspace
	p.updatePathFilter(tea.KeyMsg{Type: tea.KeyBackspace})
	if p.pathFilterInput != "sr" {
		t.Errorf("after backspace, input = %q, want %q", p.pathFilterInput, "sr")
	}

	// Test input
	p.updatePathFilter(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if p.pathFilterInput != "src" {
		t.Errorf("after input 'c', input = %q, want %q", p.pathFilterInput, "src")
	}

	// Test escape
	p.updatePathFilter(tea.KeyMsg{Type: tea.KeyEsc})
	if p.pathFilterMode {
		t.Error("after esc, pathFilterMode should be false")
	}
	if p.pathFilterInput != "" {
		t.Error("after esc, pathFilterInput should be empty")
	}
}

func TestUpdateHistorySearch(t *testing.T) {
	p := &Plugin{
		historySearchMode: true,
		historySearchState: &HistorySearchState{
			Query: "foo",
		},
		recentCommits: []*Commit{
			{Subject: "foo bar"},
		},
	}

	// Test backspace
	p.updateHistorySearch(tea.KeyMsg{Type: tea.KeyBackspace})
	if p.historySearchState.Query != "fo" {
		t.Errorf("after backspace, query = %q, want %q", p.historySearchState.Query, "fo")
	}

	// Test input
	p.updateHistorySearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if p.historySearchState.Query != "foo" {
		t.Errorf("after input 'o', query = %q, want %q", p.historySearchState.Query, "foo")
	}

	// Test matches update (searchCommits called internally)
	if len(p.historySearchState.Matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(p.historySearchState.Matches))
	}
}

func TestFocusContextSearchModals(t *testing.T) {
	p := &Plugin{
		tree: &FileTree{},
	}

	p.historySearchMode = true
	if got := p.FocusContext(); got != "git-history-search" {
		t.Fatalf("history search context = %q, want %q", got, "git-history-search")
	}

	p.historySearchMode = false
	p.pathFilterMode = true
	if got := p.FocusContext(); got != "git-path-filter" {
		t.Fatalf("path filter context = %q, want %q", got, "git-path-filter")
	}

	p.historySearchMode = true
	if got := p.FocusContext(); got != "git-history-search" {
		t.Fatalf("expected history search context precedence, got %q", got)
	}
}
