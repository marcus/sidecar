package filebrowser

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/plugin"
)

// createTabTestPlugin creates a test plugin with files for tab testing.
func createTabTestPlugin(t *testing.T, tmpDir string) *Plugin {
	t.Helper()
	// Create directory structure with duplicate filenames for testing
	dirs := []string{"src", "lib", "pkg/util"}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
	}

	// Create files including duplicates in different dirs
	files := map[string]string{
		"main.go":          "package main",
		"README.md":        "# Test",
		"src/main.go":      "package src",    // duplicate filename
		"src/helper.go":    "package src",
		"lib/helper.go":    "package lib",    // duplicate filename
		"pkg/util/util.go": "package util",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, path), []byte(content), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", path, err)
		}
	}

	p := &Plugin{
		ctx: &plugin.Context{
			WorkDir: tmpDir,
			Logger:  slog.New(slog.NewTextHandler(os.Stderr, nil)),
		},
		width:  80,
		height: 24,
	}
	p.tree = NewFileTree(tmpDir)
	if err := p.tree.Build(); err != nil {
		t.Fatalf("failed to build file tree: %v", err)
	}
	return p
}

func TestTabs_OpenTabReplaceCreatesTab(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	cmd := p.openTab("main.go", TabOpenReplace)
	if cmd == nil {
		t.Error("expected LoadPreview command for new tab")
	}
	if len(p.tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(p.tabs))
	}
	if p.activeTab != 0 {
		t.Errorf("expected activeTab 0, got %d", p.activeTab)
	}
	if p.previewFile != "main.go" {
		t.Errorf("expected previewFile main.go, got %s", p.previewFile)
	}
}

func TestTabs_OpenTabNewSavesScroll(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.tabs = []FileTab{{Path: "main.go"}}
	p.activeTab = 0
	p.previewFile = "main.go"
	p.previewScroll = 7

	_ = p.openTab("src/app.go", TabOpenNew)

	if len(p.tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(p.tabs))
	}
	if p.activeTab != 1 {
		t.Errorf("expected activeTab 1, got %d", p.activeTab)
	}
	if p.tabs[0].Scroll != 7 {
		t.Errorf("expected saved scroll 7, got %d", p.tabs[0].Scroll)
	}
}

func TestTabs_CloseTabSelectsNext(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.tabs = []FileTab{
		{Path: "main.go", Loaded: true, Result: PreviewResult{Lines: []string{"a"}}},
		{Path: "src/app.go", Loaded: true, Result: PreviewResult{Lines: []string{"b"}}},
	}
	p.activeTab = 0
	p.previewFile = "main.go"

	_ = p.closeTab(0)

	if len(p.tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(p.tabs))
	}
	if p.previewFile != "src/app.go" {
		t.Errorf("expected previewFile src/app.go, got %s", p.previewFile)
	}
}

func TestTabs_CloseLastTabClearsPreview(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.tabs = []FileTab{{Path: "main.go"}}
	p.activeTab = 0
	p.previewFile = "main.go"

	_ = p.closeTab(0)

	if len(p.tabs) != 0 {
		t.Fatalf("expected 0 tabs, got %d", len(p.tabs))
	}
	if p.previewFile != "" {
		t.Errorf("expected previewFile cleared, got %s", p.previewFile)
	}
}

// TestTabs_CycleWraparound verifies tab cycling wraps from last to first and vice versa.
func TestTabs_CycleWraparound(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	p.tabs = []FileTab{
		{Path: "main.go", Loaded: true},
		{Path: "src/helper.go", Loaded: true},
		{Path: "README.md", Loaded: true},
	}
	p.activeTab = 2
	p.previewFile = "README.md"

	// Cycle forward from last tab should wrap to first
	_ = p.cycleTab(1)
	if p.activeTab != 0 {
		t.Errorf("expected wrap to tab 0, got %d", p.activeTab)
	}

	// Cycle backward from first tab should wrap to last
	_ = p.cycleTab(-1)
	if p.activeTab != 2 {
		t.Errorf("expected wrap to tab 2, got %d", p.activeTab)
	}
}

// TestTabs_CycleSingleTab verifies cycling with one tab does nothing.
func TestTabs_CycleSingleTab(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	p.tabs = []FileTab{{Path: "main.go"}}
	p.activeTab = 0

	cmd := p.cycleTab(1)
	if cmd != nil {
		t.Error("expected nil cmd for single tab")
	}
	if p.activeTab != 0 {
		t.Errorf("expected activeTab to stay 0, got %d", p.activeTab)
	}
}

// TestTabs_OpenExistingSwitchesInsteadOfDuplicating verifies opening an already-open file
// with TabOpenReplace switches to that tab instead of creating a duplicate.
func TestTabs_OpenExistingSwitchesInsteadOfDuplicating(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	p.tabs = []FileTab{
		{Path: "main.go", Loaded: true, Scroll: 5},
		{Path: "src/helper.go", Loaded: true, Scroll: 10},
	}
	p.activeTab = 0
	p.previewFile = "main.go"

	// Try to open src/helper.go which is already open (TabOpenReplace deduplicates)
	_ = p.openTab("src/helper.go", TabOpenReplace)

	// Should switch to existing tab, not create new one
	if len(p.tabs) != 2 {
		t.Fatalf("expected 2 tabs (no duplicate), got %d", len(p.tabs))
	}
	if p.activeTab != 1 {
		t.Errorf("expected switch to tab 1, got %d", p.activeTab)
	}
	if p.previewFile != "src/helper.go" {
		t.Errorf("expected previewFile src/helper.go, got %s", p.previewFile)
	}
}

// TestTabs_TabOpenNewAlwaysCreatesNewTab verifies TabOpenNew creates a new tab
// even if the file is already open (intentional user action).
func TestTabs_TabOpenNewAlwaysCreatesNewTab(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	p.tabs = []FileTab{
		{Path: "main.go", Loaded: true, Scroll: 5},
	}
	p.activeTab = 0
	p.previewFile = "main.go"

	_ = p.openTab("main.go", TabOpenNew)

	if len(p.tabs) != 2 {
		t.Fatalf("expected 2 tabs (TabOpenNew should not deduplicate), got %d", len(p.tabs))
	}
	if p.activeTab != 1 {
		t.Errorf("expected activeTab 1, got %d", p.activeTab)
	}
}

// TestTabs_PreviewTabReplacedOnNavigation verifies that preview tabs are replaced
// when navigating to a different file.
func TestTabs_PreviewTabReplacedOnNavigation(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	// Open first file as preview
	_ = p.openTab("file1.go", TabOpenPreview)
	if len(p.tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(p.tabs))
	}
	if !p.tabs[0].IsPreview {
		t.Error("expected tab to be preview")
	}

	// Navigate to second file — should replace the preview tab
	_ = p.openTab("file2.go", TabOpenPreview)
	if len(p.tabs) != 1 {
		t.Fatalf("expected 1 tab (preview replaced), got %d", len(p.tabs))
	}
	if p.tabs[0].Path != "file2.go" {
		t.Errorf("expected file2.go, got %s", p.tabs[0].Path)
	}
	if !p.tabs[0].IsPreview {
		t.Error("expected tab to still be preview")
	}
}

// TestTabs_PinPromotesPreviewTab verifies that pinning a preview tab makes it permanent.
func TestTabs_PinPromotesPreviewTab(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	_ = p.openTab("file1.go", TabOpenPreview)
	if !p.tabs[0].IsPreview {
		t.Fatal("expected preview tab")
	}

	p.pinTab(0)
	if p.tabs[0].IsPreview {
		t.Error("expected tab to be pinned (not preview)")
	}

	// Navigate to another file — should create new preview tab, not replace pinned
	_ = p.openTab("file2.go", TabOpenPreview)
	if len(p.tabs) != 2 {
		t.Fatalf("expected 2 tabs (pinned + preview), got %d", len(p.tabs))
	}
	if p.tabs[0].IsPreview {
		t.Error("first tab should still be pinned")
	}
	if !p.tabs[1].IsPreview {
		t.Error("second tab should be preview")
	}
}

// TestTabs_CloseMiddleTabAdjustsActiveTab verifies closing a tab before activeTab adjusts index.
func TestTabs_CloseMiddleTabAdjustsActiveTab(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	p.tabs = []FileTab{
		{Path: "main.go", Loaded: true, Result: PreviewResult{Lines: []string{"a"}}},
		{Path: "src/helper.go", Loaded: true, Result: PreviewResult{Lines: []string{"b"}}},
		{Path: "README.md", Loaded: true, Result: PreviewResult{Lines: []string{"c"}}},
	}
	p.activeTab = 2 // viewing README.md
	p.previewFile = "README.md"

	// Close tab 0 (main.go) - before active tab
	_ = p.closeTab(0)

	if len(p.tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(p.tabs))
	}
	// activeTab should adjust from 2 to 1
	if p.activeTab != 1 {
		t.Errorf("expected activeTab 1 (adjusted), got %d", p.activeTab)
	}
	// Should still be viewing README.md
	if p.previewFile != "README.md" {
		t.Errorf("expected previewFile README.md, got %s", p.previewFile)
	}
}

// TestTabs_CloseActiveMiddleTabSelectsNext verifies closing the active middle tab selects next.
func TestTabs_CloseActiveMiddleTabSelectsNext(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	p.tabs = []FileTab{
		{Path: "main.go", Loaded: true, Result: PreviewResult{Lines: []string{"a"}}},
		{Path: "src/helper.go", Loaded: true, Result: PreviewResult{Lines: []string{"b"}}},
		{Path: "README.md", Loaded: true, Result: PreviewResult{Lines: []string{"c"}}},
	}
	p.activeTab = 1 // viewing src/helper.go (middle)
	p.previewFile = "src/helper.go"

	// Close active tab (middle)
	_ = p.closeTab(1)

	if len(p.tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(p.tabs))
	}
	// Should stay at index 1 (now README.md)
	if p.activeTab != 1 {
		t.Errorf("expected activeTab 1, got %d", p.activeTab)
	}
	if p.previewFile != "README.md" {
		t.Errorf("expected previewFile README.md, got %s", p.previewFile)
	}
}

// TestTabs_ScrollPreservation verifies scroll position is saved and restored on tab switch.
func TestTabs_ScrollPreservation(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	// Create tabs with enough content lines so scroll won't be clamped to 0
	longContent := make([]string, 100)
	for i := range longContent {
		longContent[i] = "line content"
	}
	p.tabs = []FileTab{
		{Path: "main.go", Loaded: true, Scroll: 0, Result: PreviewResult{Lines: longContent}},
		{Path: "README.md", Loaded: true, Scroll: 0, Result: PreviewResult{Lines: longContent}},
	}
	p.activeTab = 0
	p.previewFile = "main.go"
	p.previewScroll = 25 // User scrolled down
	p.previewLines = longContent

	// Switch to tab 1
	_ = p.switchTab(1)

	// Verify scroll was saved for tab 0
	if p.tabs[0].Scroll != 25 {
		t.Errorf("expected tab 0 scroll saved as 25, got %d", p.tabs[0].Scroll)
	}

	// Set new scroll for tab 1
	p.previewScroll = 10

	// Switch back to tab 0
	_ = p.switchTab(0)

	// Verify scroll was saved for tab 1
	if p.tabs[1].Scroll != 10 {
		t.Errorf("expected tab 1 scroll saved as 10, got %d", p.tabs[1].Scroll)
	}

	// Verify tab 0's scroll is restored
	if p.previewScroll != 25 {
		t.Errorf("expected previewScroll restored to 25, got %d", p.previewScroll)
	}
}

// TestTabs_DuplicateFilenames verifies tabs with same filename show parent directory.
func TestTabs_DuplicateFilenames(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	// Open files with duplicate names
	p.tabs = []FileTab{
		{Path: "main.go"},
		{Path: "src/main.go"}, // same filename, different dir
		{Path: "lib/helper.go"},
		{Path: "src/helper.go"}, // same filename, different dir
	}
	p.activeTab = 0

	labels := p.tabLabels(200) // wide enough for full labels

	// main.go and src/main.go should show parent
	if labels[0] == labels[1] {
		t.Errorf("duplicate filenames should have different labels: %q vs %q", labels[0], labels[1])
	}
	// At least one should include parent dir to distinguish from the other
	hasParentDir := false
	for _, label := range labels[:2] {
		if strings.Contains(label, "/") {
			hasParentDir = true
			break
		}
	}
	if !hasParentDir {
		t.Errorf("expected duplicate filenames to show parent dir, got: %v", labels[:2])
	}
}

// TestTabs_ZeroTabsEdgeCase verifies behavior with no tabs.
func TestTabs_ZeroTabsEdgeCase(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	p.tabs = nil
	p.activeTab = 0

	// Operations on empty tabs should not panic
	cmd := p.cycleTab(1)
	if cmd != nil {
		t.Error("cycleTab on empty tabs should return nil")
	}

	cmd = p.switchTab(0)
	if cmd != nil {
		t.Error("switchTab on empty tabs should return nil")
	}

	cmd = p.closeTab(0)
	if cmd != nil {
		t.Error("closeTab on empty tabs should return nil")
	}

	// normalizeActiveTab should handle empty
	p.normalizeActiveTab()
	if p.activeTab != 0 {
		t.Errorf("expected activeTab 0 after normalize, got %d", p.activeTab)
	}
}

// TestTabs_ManyTabsEdgeCase verifies behavior with many tabs.
func TestTabs_ManyTabsEdgeCase(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	// Create 20 tabs
	for i := 0; i < 20; i++ {
		p.tabs = append(p.tabs, FileTab{Path: filepath.Join("file", string(rune('a'+i))+".go")})
	}
	p.activeTab = 10

	// Cycle should work correctly
	_ = p.cycleTab(1)
	if p.activeTab != 11 {
		t.Errorf("expected activeTab 11, got %d", p.activeTab)
	}

	// Close from middle
	originalLen := len(p.tabs)
	_ = p.closeTab(5)
	if len(p.tabs) != originalLen-1 {
		t.Errorf("expected %d tabs after close, got %d", originalLen-1, len(p.tabs))
	}
	// activeTab should adjust (was 11, now 10 since we closed before it)
	if p.activeTab != 10 {
		t.Errorf("expected activeTab 10 after close, got %d", p.activeTab)
	}
}

// TestTabs_FindTab verifies finding tabs by path with normalization.
func TestTabs_FindTab(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	p.tabs = []FileTab{
		{Path: "main.go"},
		{Path: "src/helper.go"},
		{Path: "./src/../README.md"}, // unnormalized path
	}

	// Exact match
	if idx := p.findTab("main.go"); idx != 0 {
		t.Errorf("expected findTab(main.go) = 0, got %d", idx)
	}

	// Normalized match
	if idx := p.findTab("README.md"); idx != 2 {
		t.Errorf("expected findTab(README.md) to match normalized path, got %d", idx)
	}

	// No match
	if idx := p.findTab("nonexistent.go"); idx != -1 {
		t.Errorf("expected findTab(nonexistent.go) = -1, got %d", idx)
	}
}

// TestTabs_OpenTabAtLine verifies opening tab at specific line sets scroll correctly.
func TestTabs_OpenTabAtLine(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	// Open at line 50
	_ = p.openTabAtLine("main.go", 50, TabOpenNew)

	if len(p.tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(p.tabs))
	}

	// Scroll should be set to line-1 (0-indexed)
	if p.previewScroll != 49 {
		t.Errorf("expected previewScroll 49 (line 50 - 1), got %d", p.previewScroll)
	}

	// Tab should have scroll saved
	if p.tabs[0].Scroll != 49 {
		t.Errorf("expected tab scroll 49, got %d", p.tabs[0].Scroll)
	}
}

// TestTabs_OpenTabAtLineZero verifies line 0 doesn't cause negative scroll.
func TestTabs_OpenTabAtLineZero(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	// Open at line 0 (edge case)
	_ = p.openTabAtLine("main.go", 0, TabOpenNew)

	if p.previewScroll != 0 {
		t.Errorf("expected previewScroll 0, got %d", p.previewScroll)
	}
}

// TestTabs_TabHitRegistration verifies tab hit regions are recorded for mouse clicks.
func TestTabs_TabHitRegistration(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	p.tabs = []FileTab{
		{Path: "main.go"},
		{Path: "README.md"},
		{Path: "src/helper.go"},
	}
	p.activeTab = 0

	// Render tabs to populate hit regions
	_ = p.renderPreviewTabs(80)

	if len(p.tabHits) != 3 {
		t.Fatalf("expected 3 tab hits, got %d", len(p.tabHits))
	}

	// Verify hit regions have valid positions
	for i, hit := range p.tabHits {
		if hit.Index != i {
			t.Errorf("tabHit[%d].Index = %d, expected %d", i, hit.Index, i)
		}
		if hit.Width <= 0 {
			t.Errorf("tabHit[%d].Width = %d, expected > 0", i, hit.Width)
		}
	}

	// Hits should be in left-to-right order
	for i := 1; i < len(p.tabHits); i++ {
		if p.tabHits[i].X <= p.tabHits[i-1].X {
			t.Errorf("tabHits not in left-to-right order: %d at x=%d, %d at x=%d",
				i-1, p.tabHits[i-1].X, i, p.tabHits[i].X)
		}
	}
}

// TestTabs_SyncTreeSelection verifies tab switch syncs tree cursor.
func TestTabs_SyncTreeSelection(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTabTestPlugin(t, tmpDir)

	// Initially cursor at 0
	p.treeCursor = 0

	// Open main.go and verify sync
	p.syncTreeSelection("main.go")

	// Tree should have cursor pointing to main.go
	if node := p.tree.GetNode(p.treeCursor); node == nil || node.Path != "main.go" {
		t.Errorf("expected tree cursor at main.go, got %v", node)
	}
}
