package gitstatus

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
	p := &Plugin{pathFilterMode: true}
	p.pathFilterField.SetQuery("src")

	// Test backspace
	p.updatePathFilter(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if p.pathFilterInput() != "sr" {
		t.Errorf("after backspace, input = %q, want %q", p.pathFilterInput(), "sr")
	}

	// Test input
	p.updatePathFilter(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if p.pathFilterInput() != "src" {
		t.Errorf("after input 'c', input = %q, want %q", p.pathFilterInput(), "src")
	}
	p.updatePathFilter(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if p.pathFilterInput() != "src " {
		t.Errorf("after space, input = %q, want %q", p.pathFilterInput(), "src ")
	}

	// Test escape
	p.updatePathFilter(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.pathFilterMode {
		t.Error("after esc, pathFilterMode should be false")
	}
	if p.pathFilterInput() != "" {
		t.Error("after esc, pathFilterInput should be empty")
	}
}

func TestUpdateHistorySearch(t *testing.T) {
	p := &Plugin{
		historySearchMode:  true,
		historySearchState: NewHistorySearchState(),
		recentCommits: []*Commit{
			{Subject: "foo bar"},
		},
	}
	p.historySearchState.SetQuery("foo")

	// Test backspace
	p.updateHistorySearch(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if p.historySearchState.Query() != "fo" {
		t.Errorf("after backspace, query = %q, want %q", p.historySearchState.Query(), "fo")
	}

	// Test input
	p.updateHistorySearch(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if p.historySearchState.Query() != "foo" {
		t.Errorf("after input 'o', query = %q, want %q", p.historySearchState.Query(), "foo")
	}
	p.updateHistorySearch(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if p.historySearchState.Query() != "foo " {
		t.Errorf("after space, query = %q, want %q", p.historySearchState.Query(), "foo ")
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

// M4d-d: the history search and the path filter are the shared query field, so
// they edit like every other query bar rather than only appending and
// backspacing.
func TestHistorySearchWordDeleteAndPaste(t *testing.T) {
	p := &Plugin{
		historySearchMode:  true,
		historySearchState: NewHistorySearchState(),
		recentCommits: []*Commit{
			{Subject: "foo bar"},
			{Subject: "foo baz"},
		},
	}
	p.historySearchState.SetQuery("foo bar")
	p.updateHistorySearch(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if got := p.historySearchState.Query(); got != "foo " {
		t.Fatalf("query after alt+backspace = %q, want %q", got, "foo ")
	}
	// A word delete is a query change, so it re-matches.
	if got := len(p.historySearchState.Matches); got != 2 {
		t.Fatalf("matches after the word delete = %d, want 2", got)
	}

	handled, _ := p.handleSearchPaste(tea.PasteMsg{Content: "baz"})
	if !handled {
		t.Fatal("an open history search refused a paste")
	}
	if got := p.historySearchState.Query(); got != "foo baz" {
		t.Fatalf("query after paste = %q, want %q", got, "foo baz")
	}
	if got := len(p.historySearchState.Matches); got != 1 {
		t.Fatalf("matches after paste = %d, want 1", got)
	}
}

// The option toggles are checked before the field, so the modal's chords keep
// working while a query is being typed.
func TestHistorySearchToggleChordsBeatTheField(t *testing.T) {
	p := &Plugin{
		historySearchMode:  true,
		historySearchState: NewHistorySearchState(),
		recentCommits:      []*Commit{{Subject: "Fix bug", Author: "Alice"}},
	}
	p.historySearchState.SetQuery("fix")
	p.updateHistorySearch(tea.KeyPressMsg{Code: 'r', Mod: tea.ModAlt})
	if !p.historySearchState.UseRegex || p.historySearchState.Query() != "fix" {
		t.Fatalf("alt+r regex=%v query=%q", p.historySearchState.UseRegex, p.historySearchState.Query())
	}
	p.updateHistorySearch(tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
	if !p.historySearchState.CaseSensitive || p.historySearchState.Query() != "fix" {
		t.Fatalf("alt+c case=%v query=%q", p.historySearchState.CaseSensitive, p.historySearchState.Query())
	}
	// Case-sensitive regex over "Fix bug" no longer matches the lowercase query.
	if got := len(p.historySearchState.Matches); got != 0 {
		t.Fatalf("matches after both toggles = %d, want 0", got)
	}
}

func TestPathFilterWordDeleteAndPaste(t *testing.T) {
	p := &Plugin{pathFilterMode: true}
	p.pathFilterField.SetQuery("internal/ cmd/")
	p.updatePathFilter(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if got := p.pathFilterInput(); got != "internal/ " {
		t.Fatalf("path after alt+backspace = %q, want %q", got, "internal/ ")
	}
	handled, _ := p.handleSearchPaste(tea.PasteMsg{Content: "docs/"})
	if !handled {
		t.Fatal("an open path filter refused a paste")
	}
	if got := p.pathFilterInput(); got != "internal/ docs/" {
		t.Fatalf("path after paste = %q, want %q", got, "internal/ docs/")
	}
}
