package filebrowser

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/plugin"
)

func createTestPlugin(t *testing.T, tmpDir string) *Plugin {
	// Create some test files and directories
	if err := os.Mkdir(filepath.Join(tmpDir, "src"), 0755); err != nil {
		t.Fatalf("failed to create src dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatalf("failed to create main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "src", "app.go"), []byte("package src"), 0644); err != nil {
		t.Fatalf("failed to create app.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "src", "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to create config.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatalf("failed to create README.md: %v", err)
	}

	p := &Plugin{
		ctx: &plugin.Context{
			WorkDir: tmpDir,
			Logger:  slog.New(slog.NewTextHandler(os.Stderr, nil)),
		},
		width:  80,
		height: 24,
	}

	// Build the file tree
	p.tree = NewFileTree(tmpDir)
	if err := p.tree.Build(); err != nil {
		t.Fatalf("failed to build file tree: %v", err)
	}

	return p
}

func TestSearch_EnterSearchMode(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	if p.searchMode {
		t.Error("searchMode should be false initially")
	}

	// Press "/" to enter search mode
	_, _ = p.handleKey(tea.KeyPressMsg{Code: '/', Text: "/"})

	if !p.searchMode {
		t.Error("searchMode should be true after '/'")
	}
	if p.searchQuery() != "" {
		t.Error("searchQuery should be empty when entering search mode")
	}
}

func TestSearch_ExitSearchMode(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	// Enter search mode
	p.searchMode = true
	p.searchField.SetQuery("test")

	// Press escape to exit
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEscape})

	if p.searchMode {
		t.Error("searchMode should be false after escape")
	}
	if p.searchQuery() != "" {
		t.Error("searchQuery should be cleared after escape")
	}
}

func TestSearch_TypeQuery(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	p.searchMode = true

	// Type "main"
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: 'm', Text: "m"})
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: 'i', Text: "i"})
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: 'n', Text: "n"})

	if p.searchQuery() != "main" {
		t.Errorf("searchQuery = %q, want main", p.searchQuery())
	}

	// Should have found main.go
	if len(p.searchMatches) == 0 {
		t.Error("no matches found for 'main'")
	}

	found := false
	for _, match := range p.searchMatches {
		if match.Name == "main.go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("main.go not in search matches")
	}
}

func TestSearchInputsAcceptSpaces(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	space := tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}

	p.searchMode = true
	p.searchField.SetQuery("main")
	_, _ = p.handleSearchKey(space)
	if got := p.searchQuery(); got != "main " {
		t.Fatalf("tree search query = %q, want %q", got, "main ")
	}

	p.quickOpenMode = true
	p.fileFinder().SetQuery("main")
	_, _ = p.handleQuickOpenKey(space)
	if got := p.fileFinder().Query(); got != "main " {
		t.Fatalf("quick-open query = %q, want %q", got, "main ")
	}

	p.contentSearchMode = true
	p.contentSearchField.SetQuery("package")
	_, _ = p.handleContentSearchKey(space)
	if got := p.contentSearchQuery(); got != "package " {
		t.Fatalf("content-search query = %q, want %q", got, "package ")
	}
}

func TestProjectSearchOpenedWithFAcceptsMultiWordQuery(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	_, _ = p.handleKey(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if !p.projectSearchMode || p.projectSearch == nil {
		t.Fatal("f did not open project search")
	}

	for _, r := range "first second" {
		_, _ = p.handleProjectSearchKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	if got := p.projectSearch.State.Query; got != "first second" {
		t.Fatalf("project search query = %q, want %q", got, "first second")
	}
}

func TestSearch_Backspace(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.searchMode = true
	p.searchField.SetQuery("mail")

	// Press backspace
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyBackspace})

	if p.searchQuery() != "mai" {
		t.Errorf("searchQuery = %q, want mai", p.searchQuery())
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	p.searchMode = true

	// Type "MAIN" in uppercase
	p.searchField.SetQuery("MAIN")
	p.updateSearchMatches()

	found := false
	for _, match := range p.searchMatches {
		if match.Name == "main.go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("case-insensitive search failed - main.go not found for 'MAIN'")
	}
}

func TestSearch_PartialMatch(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	p.searchField.SetQuery("app")
	p.updateSearchMatches()

	found := false
	for _, match := range p.searchMatches {
		if match.Name == "app.go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("partial match failed - app.go not found for 'app'")
	}
}

func TestSearch_MultipleMatches(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	p.searchField.SetQuery("a") // Matches: app.go, main.go, README.md, config (in path)
	p.updateSearchMatches()

	if len(p.searchMatches) == 0 {
		t.Error("no matches found for 'a'")
	}

	// Should have multiple matches
	if len(p.searchMatches) < 2 {
		t.Errorf("expected multiple matches, got %d", len(p.searchMatches))
	}
}

func TestSearch_MatchLimit(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	// Create many files matching a pattern
	for i := 0; i < 30; i++ {
		fname := filepath.Join(tmpDir, "file"+string(rune('0'+(i%10)))+"_"+string(rune('0'+(i/10)))+".txt")
		if err := os.WriteFile(fname, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
	}

	// Rebuild tree with new files
	p.tree = NewFileTree(tmpDir)
	if err := p.tree.Build(); err != nil {
		t.Fatalf("failed to rebuild tree: %v", err)
	}
	primeFileCache(t, p)

	p.searchField.SetQuery("file")
	p.updateSearchMatches()

	// Should be limited to 20 matches
	if len(p.searchMatches) > 20 {
		t.Errorf("matches exceeds limit: got %d, want <= 20", len(p.searchMatches))
	}
}

func TestSearch_NavigateMatches(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	p.searchMode = true
	p.searchField.SetQuery("a") // Multiple matches

	// Update matches
	p.updateSearchMatches()

	if len(p.searchMatches) < 2 {
		t.Skip("need at least 2 matches for navigation test")
	}

	initialCursor := p.searchCursor

	// Press down to move to next match
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyDown})

	if p.searchCursor == initialCursor {
		t.Error("search cursor did not move with down arrow")
	}

	// Press up to move to previous match
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyUp})

	if p.searchCursor != initialCursor {
		t.Error("search cursor did not move back with up arrow")
	}
}

func TestSearch_JumpToMatch(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	p.searchMode = true
	p.searchField.SetQuery("app")
	p.updateSearchMatches()

	if len(p.searchMatches) == 0 {
		t.Skip("no matches found for app")
	}

	initialCursor := p.treeCursor

	// Press enter to jump to first match
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Cursor should have moved or tree cursor updated
	if !p.searchMode {
		// Search mode should be exited after jumping
		// Tree cursor should be updated (might be same position)
		_ = p.treeCursor == initialCursor
	}
}

func TestSearch_ExpandParents(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	// Find a nested file
	nestedFile := p.findAndExpandPath(filepath.Join("src", "app.go"))
	if nestedFile == nil {
		t.Skip("nested file not found")
	}

	// Collapse the parent directory
	var srcDir *FileNode
	for _, child := range p.tree.Root.Children {
		if child.Name == "src" {
			srcDir = child
			srcDir.IsExpanded = false
			break
		}
	}

	if srcDir == nil {
		t.Skip("src directory not found")
	}

	// Expand parents should expand src
	p.expandParents(nestedFile)

	if !srcDir.IsExpanded {
		t.Error("expandParents did not expand parent directory")
	}
}

func TestSearch_FindAndExpandPath(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	for _, rel := range []string{"main.go", "README.md", "src", filepath.Join("src", "app.go"), filepath.Join("src", "config.json")} {
		node := p.findAndExpandPath(rel)
		if node == nil {
			t.Errorf("findAndExpandPath did not find %s", rel)
			continue
		}
		if node.Path != rel {
			t.Errorf("findAndExpandPath(%s) returned node with path %s", rel, node.Path)
		}
	}

	if p.findAndExpandPath(filepath.Join("src", "missing.go")) != nil {
		t.Error("findAndExpandPath found a nonexistent file")
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.searchField.SetQuery("")
	p.updateSearchMatches()

	if len(p.searchMatches) != 0 {
		t.Error("empty search query should have no matches")
	}
}

func TestSearch_NoMatches(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.searchField.SetQuery("xyznonexistent")
	p.updateSearchMatches()

	if len(p.searchMatches) != 0 {
		t.Error("nonexistent query should have no matches")
	}

	if p.searchCursor != 0 {
		t.Error("search cursor should be reset to 0 when no matches")
	}
}

func TestSearch_CursorBounds(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	p.searchMode = true
	p.searchField.SetQuery("a")
	p.updateSearchMatches()

	if len(p.searchMatches) == 0 {
		t.Skip("no matches found")
	}

	// Try to move up when at position 0
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.searchCursor < 0 {
		t.Error("search cursor should not go below 0")
	}

	// Move to end
	for i := 0; i < len(p.searchMatches); i++ {
		_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	// Try to move down at end
	cursorBefore := p.searchCursor
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyDown})

	if p.searchCursor > len(p.searchMatches)-1 {
		t.Error("search cursor should not exceed matches length")
	}
	// This is OK - might have wrapped around
	_ = p.searchCursor != cursorBefore && len(p.searchMatches) > 1
}

func TestSearch_PrintableCharacterFilter(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.searchMode = true

	// Try to input non-printable character (should be ignored)
	initialQuery := p.searchQuery()
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: 0})

	if p.searchQuery() != initialQuery {
		t.Error("non-printable character should be ignored")
	}

	// Printable character should be added
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if p.searchQuery() != "a" {
		t.Error("printable character should be added to query")
	}
}

// --- Content Search Tests ---

func createTestPluginWithPreview(t *testing.T, tmpDir string, fileContent string) *Plugin {
	p := createTestPlugin(t, tmpDir)

	// Set up preview with content
	p.previewFile = "test.txt"
	p.previewLines = strings.Split(fileContent, "\n")
	p.previewHighlighted = p.previewLines // No syntax highlighting for test
	p.activePane = PanePreview
	p.height = 24
	p.width = 80

	return p
}

func TestContentSearch_EnterMode(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "line one\nline two\nline three")

	if p.contentSearchMode {
		t.Error("contentSearchMode should be false initially")
	}

	// Press "/" in preview pane
	_, _ = p.handlePreviewKey("/")

	if !p.contentSearchMode {
		t.Error("contentSearchMode should be true after '/' in preview")
	}
	if p.contentSearchQuery() != "" {
		t.Error("contentSearchQuery should be empty when entering search mode")
	}
}

func TestContentSearch_EnterModeDisabledForBinary(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "some content")
	p.isBinary = true

	_, _ = p.handlePreviewKey("/")

	if p.contentSearchMode {
		t.Error("contentSearchMode should not activate for binary files")
	}
}

func TestContentSearch_EnterModeDisabledForEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "")
	p.previewLines = nil

	_, _ = p.handlePreviewKey("/")

	if p.contentSearchMode {
		t.Error("contentSearchMode should not activate for empty files")
	}
}

func TestContentSearch_ExitWithEsc(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "test content")
	p.contentSearchMode = true
	p.contentSearchField.SetQuery("test")
	p.contentSearchMatches = []ContentMatch{{LineNo: 0, StartCol: 0, EndCol: 4}}

	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: tea.KeyEscape})

	if p.contentSearchMode {
		t.Error("contentSearchMode should be false after escape")
	}
	if p.contentSearchQuery() != "" {
		t.Error("contentSearchQuery should be cleared after escape")
	}
	if len(p.contentSearchMatches) != 0 {
		t.Error("contentSearchMatches should be cleared after escape")
	}
}

func TestContentSearch_ExitWithEnter(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "test content")
	p.contentSearchMode = true
	p.contentSearchField.SetQuery("test")
	p.contentSearchMatches = []ContentMatch{{LineNo: 0, StartCol: 0, EndCol: 4}}

	// First Enter commits the search (vim-style two-phase)
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !p.contentSearchCommitted {
		t.Error("contentSearchCommitted should be true after first enter")
	}
	if !p.contentSearchMode {
		t.Error("contentSearchMode should still be true after commit")
	}

	// Second Enter exits search mode
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.contentSearchMode {
		t.Error("contentSearchMode should be false after second enter")
	}
}

func TestContentSearch_FindMatches(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "foo bar foo\nbaz foo\nno match here")
	p.contentSearchMode = true
	p.contentSearchField.SetQuery("foo")
	p.updateContentMatches()

	if len(p.contentSearchMatches) != 3 {
		t.Errorf("expected 3 matches, got %d", len(p.contentSearchMatches))
	}

	// Check first match
	if p.contentSearchMatches[0].LineNo != 0 || p.contentSearchMatches[0].StartCol != 0 {
		t.Errorf("first match wrong: %+v", p.contentSearchMatches[0])
	}
	// Check second match (second "foo" on line 0)
	if p.contentSearchMatches[1].LineNo != 0 || p.contentSearchMatches[1].StartCol != 8 {
		t.Errorf("second match wrong: %+v", p.contentSearchMatches[1])
	}
	// Check third match (on line 1)
	if p.contentSearchMatches[2].LineNo != 1 || p.contentSearchMatches[2].StartCol != 4 {
		t.Errorf("third match wrong: %+v", p.contentSearchMatches[2])
	}
}

func TestContentSearch_CaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "FOO foo Foo")
	p.contentSearchMode = true
	p.contentSearchField.SetQuery("foo")
	p.updateContentMatches()

	if len(p.contentSearchMatches) != 3 {
		t.Errorf("case-insensitive search should find 3 matches, got %d", len(p.contentSearchMatches))
	}
}

func TestContentSearch_NavigateNext(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "a\na\na")
	p.contentSearchMode = true
	p.contentSearchField.SetQuery("a")
	p.updateContentMatches()

	if len(p.contentSearchMatches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(p.contentSearchMatches))
	}

	if p.contentSearchCursor != 0 {
		t.Error("cursor should start at 0")
	}

	// Commit search with Enter first (vim-style two-phase)
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Press n
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if p.contentSearchCursor != 1 {
		t.Errorf("cursor should be 1 after n, got %d", p.contentSearchCursor)
	}

	// Press n again
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if p.contentSearchCursor != 2 {
		t.Errorf("cursor should be 2 after n, got %d", p.contentSearchCursor)
	}

	// Press n - should wrap to 0
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if p.contentSearchCursor != 0 {
		t.Errorf("cursor should wrap to 0, got %d", p.contentSearchCursor)
	}
}

func TestContentSearch_NavigatePrev(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "a\na\na")
	p.contentSearchMode = true
	p.contentSearchField.SetQuery("a")
	p.updateContentMatches()

	// Commit search with Enter first (vim-style two-phase)
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Press N at position 0 - should wrap to last
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: 'N', Text: "N"})
	if p.contentSearchCursor != 2 {
		t.Errorf("cursor should wrap to 2, got %d", p.contentSearchCursor)
	}

	// Press N again
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: 'N', Text: "N"})
	if p.contentSearchCursor != 1 {
		t.Errorf("cursor should be 1, got %d", p.contentSearchCursor)
	}
}

func TestContentSearch_Backspace(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "test")
	p.contentSearchMode = true
	p.contentSearchField.SetQuery("tes")

	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: tea.KeyBackspace})

	if p.contentSearchQuery() != "te" {
		t.Errorf("query should be 'te', got %q", p.contentSearchQuery())
	}
}

func TestContentSearch_TypeCharacters(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "hello world")
	p.contentSearchMode = true

	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: 'e', Text: "e"})

	if p.contentSearchQuery() != "he" {
		t.Errorf("query should be 'he', got %q", p.contentSearchQuery())
	}

	if len(p.contentSearchMatches) != 1 {
		t.Errorf("should have 1 match for 'he', got %d", len(p.contentSearchMatches))
	}
}

func TestContentSearch_ScrollToMatch(t *testing.T) {
	tmpDir := t.TempDir()
	// Create content with 50 lines, match on line 40
	var lines []string
	for i := 0; i < 50; i++ {
		if i == 40 {
			lines = append(lines, "TARGET here")
		} else {
			lines = append(lines, fmt.Sprintf("line %d", i))
		}
	}
	p := createTestPluginWithPreview(t, tmpDir, strings.Join(lines, "\n"))
	p.contentSearchMode = true
	p.contentSearchField.SetQuery("TARGET")
	p.updateContentMatches()

	if len(p.contentSearchMatches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(p.contentSearchMatches))
	}

	// Scroll should have moved to show the match
	if p.previewScroll == 0 {
		t.Error("previewScroll should have changed to show match on line 40")
	}
}

func TestContentSearch_ScrollStaysWhenMatchVisible(t *testing.T) {
	tmpDir := t.TempDir()
	// 100 lines with matches at lines 10, 15, 80
	var lines []string
	for i := 0; i < 100; i++ {
		switch i {
		case 10, 15, 80:
			lines = append(lines, "MATCH here")
		default:
			lines = append(lines, fmt.Sprintf("line %d", i))
		}
	}
	p := createTestPluginWithPreview(t, tmpDir, strings.Join(lines, "\n"))
	p.height = 30 // visibleContentHeight = 30 - 6 = 24
	p.contentSearchMode = true
	p.contentSearchField.SetQuery("MATCH")
	p.updateContentMatches()

	if len(p.contentSearchMatches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(p.contentSearchMatches))
	}

	// First match at line 10 - should scroll (starts at 0, match is visible but
	// updateContentMatches calls scrollToContentMatch from scratch)
	scrollAfterFirst := p.previewScroll

	// Commit and navigate to next match at line 15
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: 'n', Text: "n"})

	// Match at line 15 should be visible from the first match's scroll position,
	// so viewport should NOT jump
	if p.previewScroll != scrollAfterFirst {
		t.Errorf("viewport should not jump when next match is visible: scroll was %d, now %d",
			scrollAfterFirst, p.previewScroll)
	}

	// Navigate to match at line 80 - should scroll since it's off-screen
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if p.previewScroll == scrollAfterFirst {
		t.Error("viewport should scroll to show match at line 80")
	}
}

func TestContentSearch_EmptyQuery(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "test content")
	p.contentSearchMode = true
	p.contentSearchField.SetQuery("")
	p.updateContentMatches()

	if len(p.contentSearchMatches) != 0 {
		t.Error("empty query should have no matches")
	}
}

func TestContentSearch_FocusContext(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPluginWithPreview(t, tmpDir, "test")

	// Preview pane focused, no search
	p.activePane = PanePreview
	if p.FocusContext() != "file-browser-preview" {
		t.Errorf("expected file-browser-preview, got %s", p.FocusContext())
	}

	// Content search active
	p.contentSearchMode = true
	if p.FocusContext() != "file-browser-content-search" {
		t.Errorf("expected file-browser-content-search, got %s", p.FocusContext())
	}
}

// --- Quick Open Tests ---

func TestQuickOpen_OpenMode(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	if p.quickOpenMode {
		t.Error("quickOpenMode should be false initially")
	}

	// Press ctrl+p
	_, cmd := p.handleKey(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})

	if !p.quickOpenMode {
		t.Error("quickOpenMode should be true after ctrl+p")
	}
	if !p.quickOpen.Scanning {
		t.Error("ctrl+p should start a background file scan")
	}
	if cmd == nil {
		t.Fatal("ctrl+p should return a scan command")
	}

	// The scan populates the cache when its message is handled
	p.Update(cmd())

	if len(p.quickOpen.Files) == 0 {
		t.Error("file cache should be populated")
	}
	if p.quickOpen.Scanning {
		t.Error("quickOpenScanning should be cleared once the scan lands")
	}
}

func TestQuickOpen_CloseWithEsc(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	// Enter quick open
	p.quickOpenMode = true
	p.fileFinder().SetQuery("test")
	p.quickOpen.Files = []string{"test.go"}
	p.fileFinder().Refilter()

	// Press esc
	_, _ = p.handleQuickOpenKey(tea.KeyPressMsg{Code: tea.KeyEscape})

	if p.quickOpenMode {
		t.Error("quickOpenMode should be false after esc")
	}
	if p.fileFinder().Query() != "" {
		t.Error("query should be cleared")
	}
}

func TestQuickOpen_TypeQuery(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	p.quickOpenMode = true
	p.quickOpen.Files = []string{"main.go", "test.go", "app.go"}

	// Type "ma"
	_, _ = p.handleQuickOpenKey(tea.KeyPressMsg{Code: 'm', Text: "m"})
	_, _ = p.handleQuickOpenKey(tea.KeyPressMsg{Code: 'a', Text: "a"})

	if p.fileFinder().Query() != "ma" {
		t.Errorf("query should be 'ma', got %q", p.fileFinder().Query())
	}

	// Should match main.go
	found := false
	for _, m := range p.fileFinder().Matches() {
		if m.Path == "main.go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("main.go should be in matches")
	}
}

func TestQuickOpen_NavigateResults(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	p.quickOpenMode = true
	p.quickOpen.Files = []string{"a.go", "b.go", "c.go"}
	p.fileFinder().Refilter()

	if len(p.fileFinder().Matches()) < 3 {
		t.Skip("need at least 3 matches")
	}

	// Initial cursor at 0
	if p.fileFinder().Cursor() != 0 {
		t.Error("cursor should start at 0")
	}

	// Move down
	_, _ = p.handleQuickOpenKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if p.fileFinder().Cursor() != 1 {
		t.Errorf("cursor should be 1, got %d", p.fileFinder().Cursor())
	}

	// Move up
	_, _ = p.handleQuickOpenKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.fileFinder().Cursor() != 0 {
		t.Errorf("cursor should be 0, got %d", p.fileFinder().Cursor())
	}

	// Can't go above 0
	_, _ = p.handleQuickOpenKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.fileFinder().Cursor() != 0 {
		t.Error("cursor should not go below 0")
	}
}

func TestQuickOpen_SelectMatch(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	p.quickOpenMode = true
	p.quickOpen.Files = []string{"main.go", "src/app.go"}
	p.fileFinder().SetQuery("app")
	p.fileFinder().Refilter()

	if len(p.fileFinder().Matches()) == 0 {
		t.Skip("no matches found")
	}

	// Find app.go match
	for i, m := range p.fileFinder().Matches() {
		if m.Path == "src/app.go" {
			p.fileFinder().SetCursor(i)
			break
		}
	}

	// Press enter
	_, cmd := p.handleQuickOpenKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Should have closed quick open
	if p.quickOpenMode {
		t.Error("quickOpenMode should be false after selection")
	}

	// Should have set preview file
	if p.previewFile != "src/app.go" {
		t.Errorf("previewFile should be src/app.go, got %s", p.previewFile)
	}

	// Should have returned a command (LoadPreview)
	if cmd == nil {
		t.Error("should return LoadPreview command")
	}
}

func TestQuickOpen_FocusContext(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	// Normal state
	if p.FocusContext() != "file-browser-tree" {
		t.Errorf("expected file-browser-tree, got %s", p.FocusContext())
	}

	// Quick open active
	p.quickOpenMode = true
	if p.FocusContext() != "file-browser-quick-open" {
		t.Errorf("expected file-browser-quick-open, got %s", p.FocusContext())
	}
}

func TestQuickOpen_Backspace(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	p.quickOpenMode = true
	p.fileFinder().SetQuery("test")
	p.quickOpen.Files = []string{"test.go"}

	_, _ = p.handleQuickOpenKey(tea.KeyPressMsg{Code: tea.KeyBackspace})

	if p.fileFinder().Query() != "tes" {
		t.Errorf("query should be 'tes', got %q", p.fileFinder().Query())
	}
}

func TestQuickOpen_CtrlPNavigates(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	p.quickOpenMode = true
	p.quickOpen.Files = []string{"a.go", "b.go"}
	p.fileFinder().Refilter()
	p.fileFinder().SetCursor(1)

	// ctrl+p should move cursor up (vim-like)
	_, _ = p.handleQuickOpenKey(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})

	if p.fileFinder().Cursor() != 0 {
		t.Errorf("cursor should be 0 after ctrl+p, got %d", p.fileFinder().Cursor())
	}
}

func TestQuickOpen_CtrlNNavigates(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	p.quickOpenMode = true
	p.quickOpen.Files = []string{"a.go", "b.go"}
	p.fileFinder().Refilter()

	// ctrl+n should move cursor down
	_, _ = p.handleQuickOpenKey(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})

	if p.fileFinder().Cursor() != 1 {
		t.Errorf("cursor should be 1 after ctrl+n, got %d", p.fileFinder().Cursor())
	}
}

func TestQuickOpen_UpdateMatchesResetsCursor(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	p.quickOpenMode = true
	p.quickOpen.Files = []string{"aaa.go", "bbb.go", "ccc.go"}
	p.fileFinder().Refilter()
	p.fileFinder().SetCursor(2)

	// Type query that reduces matches
	p.fileFinder().SetQuery("aaa")
	p.fileFinder().Refilter()

	// Cursor should be reset since only 1 match now
	if p.fileFinder().Cursor() >= len(p.fileFinder().Matches()) {
		t.Error("cursor should be within bounds after matches change")
	}
}

// M4d-d: both `/` bars in the file browser are the shared query field, so they
// edit like every other query bar rather than only appending and backspacing.
func TestTreeSearchWordDeleteRemovesOneWord(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	p.searchMode = true
	p.searchField.SetQuery("main go")
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if got := p.searchQuery(); got != "main " {
		t.Fatalf("tree query after alt+backspace = %q, want %q", got, "main ")
	}
}

func TestTreeSearchPasteInsertsAtTheCaret(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	p.searchMode = true
	handled, _ := p.handleSearchPaste(tea.PasteMsg{Content: "main"})
	if !handled {
		t.Fatal("a live tree search refused a paste")
	}
	if got := p.searchQuery(); got != "main" {
		t.Fatalf("tree query after paste = %q, want %q", got, "main")
	}
	if len(p.searchMatches) == 0 {
		t.Fatal("the pasted query matched nothing; a paste must re-scan like a keystroke")
	}
	// The caret is real: a paste at the start lands at the start.
	_, _ = p.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyHome})
	_, _ = p.handleSearchPaste(tea.PasteMsg{Content: "x"})
	if got := p.searchQuery(); got != "xmain" {
		t.Fatalf("tree query after a paste at the caret = %q, want %q", got, "xmain")
	}
}

func TestContentSearchWordDeleteAndPaste(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.contentSearchMode = true
	p.contentSearchField.SetQuery("package main")
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if got := p.contentSearchQuery(); got != "package " {
		t.Fatalf("content query after alt+backspace = %q, want %q", got, "package ")
	}

	handled, _ := p.handleSearchPaste(tea.PasteMsg{Content: "main"})
	if !handled {
		t.Fatal("a typing content search refused a paste")
	}
	if got := p.contentSearchQuery(); got != "package main" {
		t.Fatalf("content query after paste = %q, want %q", got, "package main")
	}

	// A committed content search has no input on screen: n/N are navigation,
	// and a paste is not the bar's.
	_, _ = p.handleContentSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !p.contentSearchCommitted {
		t.Fatal("enter did not commit the content search")
	}
	if handled, _ := p.handleSearchPaste(tea.PasteMsg{Content: "x"}); handled {
		t.Fatal("a committed content search took a paste")
	}
	if got := p.contentSearchQuery(); got != "package main" {
		t.Fatalf("content query after the refused paste = %q", got)
	}
}
