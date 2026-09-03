package filebrowser

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/filefind"
	"github.com/marcus/sidecar/internal/plugin"
)

// primeFileCache runs the quick-open scan to completion the way the update loop
// would, so a test can assert against a populated cache.
func primeFileCache(t *testing.T, p *Plugin) {
	t.Helper()
	cmd := p.ensureFileCache()
	if cmd == nil {
		return
	}
	msg := cmd()
	if _, ok := msg.(FileCacheBuiltMsg); !ok {
		t.Fatalf("file cache scan produced %T, want FileCacheBuiltMsg", msg)
	}
	p.Update(msg)
}

var _ plugin.EpochMessage = FileCacheBuiltMsg{}

var _ tea.Msg = FileCacheBuiltMsg{}

// --- Tab invalidation ---

func TestInvalidateTabsInDirs_DropsBackgroundTabInChangedDir(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.tabs = []FileTab{
		{Path: "main.go", Loaded: true, Result: PreviewResult{Lines: []string{"old"}}},
		{Path: filepath.Join("src", "app.go"), Loaded: true, Result: PreviewResult{Lines: []string{"old"}}},
	}
	p.activeTab = 0

	p.invalidateTabsInDirs([]string{filepath.Join(tmpDir, "src")})

	if !p.tabs[0].Loaded {
		t.Error("tab outside the changed directory should stay loaded")
	}
	if p.tabs[1].Loaded {
		t.Error("background tab in the changed directory should be invalidated")
	}
	if p.tabs[1].Result.Lines != nil {
		t.Error("invalidated tab should drop its cached result")
	}
}

func TestInvalidateTabsInDirs_KeepsActiveTab(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.tabs = []FileTab{{Path: "main.go", Loaded: true, Result: PreviewResult{Lines: []string{"visible"}}}}
	p.activeTab = 0

	p.invalidateTabsInDirs([]string{tmpDir})

	if !p.tabs[0].Loaded {
		t.Error("the active tab must keep its content; the preview path reloads it")
	}
}

func TestInvalidateTabsInDirs_IgnoresNestedDirs(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.tabs = []FileTab{
		{Path: "main.go", Loaded: true},
		{Path: filepath.Join("src", "app.go"), Loaded: true},
	}
	p.activeTab = 0

	// Only the root changed; the file one level down is unaffected.
	p.invalidateTabsInDirs([]string{tmpDir})

	if !p.tabs[1].Loaded {
		t.Error("a change in the parent directory should not invalidate a tab in a subdirectory")
	}
}

func TestInvalidateTabsInDirs_NoDirsIsNoop(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.tabs = []FileTab{{Path: "main.go", Loaded: true}, {Path: "README.md", Loaded: true}}
	p.activeTab = 0

	p.invalidateTabsInDirs(nil)

	for i, tab := range p.tabs {
		if !tab.Loaded {
			t.Errorf("tab %d invalidated without any changed directory", i)
		}
	}
}

func TestWatchEvent_TreeChangedMarksCachesDirtyAndInvalidatesTabs(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	p.tabs = []FileTab{
		{Path: "main.go", Loaded: true},
		{Path: filepath.Join("src", "app.go"), Loaded: true},
	}
	p.activeTab = 0

	p.Update(WatchEventMsg{TreeChanged: true, Dirs: []string{filepath.Join(tmpDir, "src")}})

	if !p.quickOpen.Dirty || !p.dirCache.Dirty {
		t.Error("a tree change should mark both quick-open caches dirty")
	}
	if p.tabs[1].Loaded {
		t.Error("a tree change should invalidate background tabs in the changed directory")
	}
	if cmd := p.ensureFileCache(); cmd == nil {
		t.Error("a dirty cache should trigger a rescan on next use")
	}
}

func TestWatchEvent_PreviewOnlyLeavesCachesClean(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	p.previewFile = "main.go"

	p.Update(WatchEventMsg{PreviewChanged: true})

	if p.quickOpen.Dirty || p.dirCache.Dirty {
		t.Error("a preview-only change does not change the directory listing")
	}
}

// --- Cache scan scheduling ---

func TestEnsureFileCache_ScansOnceThenReusesCache(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	cmd := p.ensureFileCache()
	if cmd == nil {
		t.Fatal("first call should start a scan")
	}
	if !p.quickOpen.Scanning {
		t.Error("quickOpenScanning should be set while the scan is in flight")
	}
	p.Update(cmd())

	if p.ensureFileCache() != nil {
		t.Error("a fresh cache should not be rescanned")
	}
}

func TestEnsureFileCache_RescansWhenCachesDirty(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	p.quickOpen.Dirty = true

	if p.ensureFileCache() == nil {
		t.Fatal("a dirty cache should be rescanned")
	}
	if p.quickOpen.Dirty {
		t.Error("the dirty flag should be consumed once a scan starts")
	}
}

func TestEnsureFileCache_NoConcurrentScans(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	if p.ensureFileCache() == nil {
		t.Fatal("first call should start a scan")
	}
	if p.ensureFileCache() != nil {
		t.Error("a second scan should not start while one is in flight")
	}
}

func TestEnsureFileCache_KeepsStaleFilesUntilScanLands(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	before := append([]string(nil), p.quickOpen.Files...)
	p.quickOpen.Dirty = true
	cmd := p.ensureFileCache()
	if cmd == nil {
		t.Fatal("expected a rescan")
	}

	if len(p.quickOpen.Files) != len(before) {
		t.Error("the previous file list should stay visible while the rescan runs")
	}
}

func TestCacheDirtyFlags_AreTrackedPerCache(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	primeFileCache(t, p)

	// Build the directory cache too, then dirty both.
	dirCmd := p.ensureDirCache()
	if dirCmd == nil {
		t.Fatal("expected a directory scan")
	}
	p.Update(dirCmd())
	p.quickOpen.Dirty = true
	p.dirCache.Dirty = true

	// One cache rescanning must not clear the other's flag.
	if p.ensureFileCache() == nil {
		t.Fatal("expected a file rescan")
	}
	if p.ensureDirCache() == nil {
		t.Error("the directory cache should rebuild after a disk change of its own")
	}
}

func TestEnsureDirCache_RescansWhenDirtiedMidScan(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	cmd := p.ensureDirCache()
	if cmd == nil {
		t.Fatal("expected a directory scan")
	}
	// The disk moves while the scan is walking it.
	p.dirCache.Dirty = true
	p.Update(cmd())

	if p.ensureDirCache() == nil {
		t.Error("a scan that started before the change must not be treated as fresh")
	}
}

// --- FileCacheBuiltMsg handling ---

func TestFileCacheBuiltMsg_PopulatesCacheAndMatches(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.quickOpenMode = true
	p.fileFinder().SetQuery("app")
	p.quickOpen.Scanning = true

	p.Update(FileCacheBuiltMsg{Files: []string{"main.go", filepath.Join("src", "app.go")}})

	if p.quickOpen.Scanning {
		t.Error("quickOpenScanning should be cleared when the scan lands")
	}
	if len(p.quickOpen.Files) != 2 {
		t.Fatalf("quickOpenFiles = %v, want 2 entries", p.quickOpen.Files)
	}
	if len(p.fileFinder().Matches()) == 0 {
		t.Error("matches should be recomputed against the new cache")
	}
}

func TestFileCacheBuiltMsg_StaleEpochIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	p.ctx.Epoch = 2
	p.quickOpen.Scanning = true

	p.Update(FileCacheBuiltMsg{Files: []string{"stale.go"}, Epoch: 1})

	if len(p.quickOpen.Files) != 0 {
		t.Errorf("stale scan applied: %v", p.quickOpen.Files)
	}
	if !p.quickOpen.Scanning {
		t.Error("a dropped scan should not clear the in-flight flag for the current epoch")
	}
}

func TestFileCacheBuiltMsg_DirsPopulateDirCache(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)
	p.dirCache.Scanning = true

	p.Update(FileCacheBuiltMsg{Dirs: true, Files: []string{"src"}})

	if len(p.dirCache.Files) != 1 || p.dirCache.Files[0] != "src" {
		t.Errorf("dirCache = %v, want [src]", p.dirCache.Files)
	}
	if p.dirCache.Scanning {
		t.Error("dirCacheScanning should be cleared when the scan lands")
	}
	if len(p.quickOpen.Files) != 0 {
		t.Error("a directory scan must not touch the file cache")
	}
}

func TestFileCacheBuiltMsg_ErrTextSurfaces(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.Update(FileCacheBuiltMsg{ErrText: "scan timed out"})

	if p.quickOpen.ErrText != "scan timed out" {
		t.Errorf("quickOpenError = %q, want scan timed out", p.quickOpen.ErrText)
	}
}

// --- The scan itself ---

func TestScanFileCache_RunsOffTheUpdateGoroutine(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir) // Creates main.go, src/app.go, ...
	cache := &filefind.Cache{}
	cmd := cache.Ensure(tmpDir, 0)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := cmd().(FileCacheBuiltMsg); !ok {
				t.Error("scan command did not produce a FileCacheBuiltMsg")
			}
		}()
	}

	// Meanwhile the update goroutine keeps working the live tree, whose
	// gitignore matcher the scan must not share.
	for i := 0; i < 50; i++ {
		if node := p.tree.FindByPath("src"); node != nil {
			_ = p.tree.Expand(node)
			p.tree.Collapse(node)
		}
	}

	wg.Wait()
}

// --- Quick open modal ---

func TestQuickOpenModal_ShowsScanningState(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.quickOpenMode = true
	p.quickOpen.Scanning = true

	out := p.renderQuickOpenModalContent()
	if !strings.Contains(out, "Scanning files") {
		t.Errorf("quick open modal should report the in-flight scan, got:\n%s", out)
	}

	p.quickOpen.Scanning = false
	out = p.renderQuickOpenModalContent()
	if strings.Contains(out, "Scanning files") {
		t.Error("scanning state should disappear once the scan lands")
	}
}

func TestFileCacheBuiltMsg_DirScanFillsMoveSuggestions(t *testing.T) {
	tmpDir := t.TempDir()
	mkdirAll(t, filepath.Join(tmpDir, "alpha"))
	p := createTestPlugin(t, tmpDir)

	// First keystroke in the move modal: the cache is empty and its scan has
	// only just started, so nothing can match yet.
	p.fileOpMode = FileOpMove
	p.fileOpTextInput.SetValue("al")
	cmd := p.updateFileOpSuggestions()
	if cmd == nil {
		t.Fatal("expected a directory scan to be started")
	}
	if p.fileOpShowSuggestions {
		t.Fatal("suggestions cannot exist before the scan lands")
	}

	p.Update(cmd())

	if !p.fileOpShowSuggestions || len(p.fileOpSuggestions) == 0 {
		t.Errorf("the landed scan should fill the dropdown, got %v", p.fileOpSuggestions)
	}
}

func TestFileCacheBuiltMsg_SearchKeepsTheSelectedMatch(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	p.searchMode = true
	p.searchField.SetQuery("go")
	cmd := p.updateSearchMatches()
	if cmd == nil {
		t.Fatal("expected a file scan to be started")
	}
	p.Update(cmd())
	if len(p.searchMatches) < 2 {
		t.Fatalf("need at least two matches to test selection, got %v", p.searchMatches)
	}

	// The user moves down, then a later scan lands: the query did not change,
	// so the selection must not jump back to the first match.
	p.searchCursor = 1
	selected := p.searchMatches[1].Path
	p.quickOpen.Dirty = true
	rescan := p.ensureFileCache()
	if rescan == nil {
		t.Fatal("expected a rescan")
	}
	p.Update(rescan())

	if p.searchCursor >= len(p.searchMatches) || p.searchMatches[p.searchCursor].Path != selected {
		t.Errorf("selection moved off %q when the scan landed (cursor %d of %v)",
			selected, p.searchCursor, p.searchMatches)
	}
}

func TestGetPathSuggestions_ReturnsScanCommand(t *testing.T) {
	tmpDir := t.TempDir()
	p := createTestPlugin(t, tmpDir)

	suggestions, cmd := p.getPathSuggestions("src")
	if cmd == nil {
		t.Fatal("a missing directory cache should be scanned in the background")
	}
	if suggestions != nil {
		t.Errorf("suggestions before the scan lands = %v, want none", suggestions)
	}

	p.Update(cmd())

	suggestions, cmd = p.getPathSuggestions("src")
	if cmd != nil {
		t.Error("a fresh directory cache should not be rescanned")
	}
	if len(suggestions) == 0 {
		t.Error("expected src to be suggested once the cache is built")
	}
}

// --- Tree expansion reloads from disk ---

func TestTreeExpand_ReloadsChildrenFromDisk(t *testing.T) {
	tmpDir := t.TempDir()
	mkdirAll(t, filepath.Join(tmpDir, "src"))
	writeFile(t, filepath.Join(tmpDir, "src", "app.go"), "package src")

	tree := NewFileTree(tmpDir)
	if err := tree.Build(); err != nil {
		t.Fatal(err)
	}

	node := tree.FindByPath("src")
	if node == nil {
		t.Fatal("src not found")
	}
	if err := tree.Expand(node); err != nil {
		t.Fatal(err)
	}
	tree.Collapse(node)

	// The directory changes while it is collapsed.
	writeFile(t, filepath.Join(tmpDir, "src", "added.go"), "package src")

	if err := tree.Expand(node); err != nil {
		t.Fatal(err)
	}
	if tree.FindByPath(filepath.Join("src", "added.go")) == nil {
		t.Error("re-expanding a directory should pick up files added while it was collapsed")
	}
}

func TestTreeExpand_PreservesExpandedSubtree(t *testing.T) {
	tmpDir := t.TempDir()
	mkdirAll(t, filepath.Join(tmpDir, "src", "inner"))
	writeFile(t, filepath.Join(tmpDir, "src", "inner", "deep.go"), "package inner")

	tree := NewFileTree(tmpDir)
	if err := tree.Build(); err != nil {
		t.Fatal(err)
	}

	src := tree.FindByPath("src")
	if err := tree.Expand(src); err != nil {
		t.Fatal(err)
	}
	inner := tree.FindByPath(filepath.Join("src", "inner"))
	if inner == nil {
		t.Fatal("src/inner not found")
	}
	if err := tree.Expand(inner); err != nil {
		t.Fatal(err)
	}

	// Re-expanding the parent reloads it, but the open subtree stays open.
	if err := tree.Expand(src); err != nil {
		t.Fatal(err)
	}

	reloadedInner := tree.FindByPath(filepath.Join("src", "inner"))
	if reloadedInner == nil {
		t.Fatal("src/inner disappeared after reload")
	}
	if !reloadedInner.IsExpanded {
		t.Error("an expanded subdirectory should stay expanded across a reload")
	}
	if tree.FindByPath(filepath.Join("src", "inner", "deep.go")) == nil {
		t.Error("children of the expanded subdirectory should be visible after a reload")
	}
}

func TestTreeExpand_KeepsChildrenOnReadError(t *testing.T) {
	tmpDir := t.TempDir()
	mkdirAll(t, filepath.Join(tmpDir, "src"))
	writeFile(t, filepath.Join(tmpDir, "src", "app.go"), "package src")

	tree := NewFileTree(tmpDir)
	if err := tree.Build(); err != nil {
		t.Fatal(err)
	}

	node := tree.FindByPath("src")
	if err := tree.Expand(node); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(filepath.Join(tmpDir, "src")); err != nil {
		t.Fatal(err)
	}

	if err := tree.Expand(node); err == nil {
		t.Error("expanding a directory that vanished should report the error")
	}
	if len(node.Children) != 1 {
		t.Errorf("children = %d, want the previously loaded ones kept", len(node.Children))
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("failed to create %s: %v", path, err)
	}
}
