package filebrowser

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/tty"
)

// waitForEvent waits for the next coalesced event, failing if none arrives.
func waitForEvent(t *testing.T, w *TreeWatcher) FSEvent {
	t.Helper()
	return waitForEventWithin(t, w, 3*time.Second)
}

// waitForEventWithin is waitForEvent for a watcher built with a non-default
// quiet period, which has to outwait that window before anything can arrive.
func waitForEventWithin(t *testing.T, w *TreeWatcher, budget time.Duration) FSEvent {
	t.Helper()
	select {
	case ev, ok := <-w.Events():
		if !ok {
			t.Fatal("events channel closed while waiting for an event")
		}
		return ev
	case <-time.After(budget):
		t.Fatal("timeout waiting for filesystem event")
		return FSEvent{}
	}
}

// expectNoEvent asserts nothing is reported within the quiet period plus slack.
func expectNoEvent(t *testing.T, w *TreeWatcher) {
	t.Helper()
	select {
	case ev, ok := <-w.Events():
		if ok {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(watchQuietPeriod + 400*time.Millisecond):
	}
}

// expectNoFlaggedEvent asserts nothing within the window reports a tree or
// preview change. A write to any file in a watched directory still names that
// directory, so consumers can drop cached file contents; those events are fine.
func expectNoFlaggedEvent(t *testing.T, w *TreeWatcher) {
	t.Helper()
	deadline := time.After(watchQuietPeriod + 400*time.Millisecond)
	for {
		select {
		case ev, ok := <-w.Events():
			if !ok {
				return
			}
			if ev.TreeChanged || ev.PreviewChanged {
				t.Fatalf("unexpected change event: %+v", ev)
			}
		case <-deadline:
			return
		}
	}
}

func newTestWatcher(t *testing.T) *TreeWatcher {
	t.Helper()
	w, err := NewTreeWatcher()
	if err != nil {
		t.Fatalf("NewTreeWatcher() failed: %v", err)
	}
	t.Cleanup(w.Stop)
	return w
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func TestNewTreeWatcher(t *testing.T) {
	w := newTestWatcher(t)
	if w.fsWatcher == nil {
		t.Error("fsWatcher not initialized")
	}
	if w.events == nil {
		t.Error("events channel not initialized")
	}
	if cap(w.events) != 1 {
		t.Errorf("events channel cap = %d, want 1", cap(w.events))
	}
}

func TestTreeWatcher_SyncDirs_ReportsCreate(t *testing.T) {
	tmpDir := t.TempDir()
	w := newTestWatcher(t)
	w.SyncDirs([]string{tmpDir})

	writeFile(t, filepath.Join(tmpDir, "new.txt"), "hi")

	ev := waitForEvent(t, w)
	if !ev.TreeChanged {
		t.Errorf("expected TreeChanged for a new file, got %+v", ev)
	}
}

func TestTreeWatcher_SyncDirs_ReportsRemove(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "gone.txt")
	writeFile(t, target, "x")

	w := newTestWatcher(t)
	w.SyncDirs([]string{tmpDir})

	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}

	ev := waitForEvent(t, w)
	if !ev.TreeChanged {
		t.Errorf("expected TreeChanged for a deleted file, got %+v", ev)
	}
}

func TestTreeWatcher_SyncDirs_DiffsInsteadOfRewatching(t *testing.T) {
	tmpDir := t.TempDir()
	sub := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}

	w := newTestWatcher(t)
	w.SyncDirs([]string{tmpDir, sub})

	w.mu.Lock()
	first := len(w.watched)
	w.mu.Unlock()
	if first != 2 {
		t.Fatalf("watched %d dirs, want 2", first)
	}

	// Same set again: nothing should change.
	w.SyncDirs([]string{tmpDir, sub})
	w.mu.Lock()
	second := len(w.watched)
	w.mu.Unlock()
	if second != 2 {
		t.Fatalf("watched %d dirs after re-sync, want 2", second)
	}

	// Collapse the subdirectory: it should be dropped.
	w.SyncDirs([]string{tmpDir})
	w.mu.Lock()
	_, stillWatched := w.watched[sub]
	w.mu.Unlock()
	if stillWatched {
		t.Error("collapsed directory is still watched")
	}
}

func TestTreeWatcher_SyncDirs_StopsReportingRemovedDirs(t *testing.T) {
	tmpDir := t.TempDir()
	sub := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}

	w := newTestWatcher(t)
	w.SyncDirs([]string{sub})
	w.SyncDirs(nil)

	writeFile(t, filepath.Join(sub, "new.txt"), "hi")
	expectNoEvent(t, w)
}

func TestTreeWatcher_SyncDirs_CapsWatchedDirs(t *testing.T) {
	tmpDir := t.TempDir()
	dirs := make([]string, 0, maxWatchedDirs+10)
	for i := 0; i < maxWatchedDirs+10; i++ {
		d := filepath.Join(tmpDir, string(rune('a'+i%26))+string(rune('a'+i/26)))
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, d)
	}

	w := newTestWatcher(t)
	w.SyncDirs(dirs)

	w.mu.Lock()
	got := len(w.watched)
	w.mu.Unlock()
	if got != maxWatchedDirs {
		t.Errorf("watched %d dirs, want the cap of %d", got, maxWatchedDirs)
	}
}

func TestTreeWatcher_WriteDoesNotRebuildTree(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "existing.txt")
	writeFile(t, target, "x")

	w := newTestWatcher(t)
	w.SyncDirs([]string{tmpDir})

	// Overwriting an existing file changes no tree entry.
	writeFile(t, target, "changed")

	select {
	case ev := <-w.Events():
		if ev.TreeChanged {
			t.Errorf("write to an existing file reported TreeChanged: %+v", ev)
		}
	case <-time.After(watchQuietPeriod + 400*time.Millisecond):
	}
}

func TestTreeWatcher_WriteReportsChangedDir(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "existing.txt")
	writeFile(t, target, "x")

	w := newTestWatcher(t)
	w.SyncDirs([]string{tmpDir})

	// An in-place rewrite (gofmt -w, sed -i, an agent edit) changes no tree
	// entry, but a tab holding that file is now showing stale content.
	writeFile(t, target, "changed")

	ev := waitForEvent(t, w)
	if ev.TreeChanged {
		t.Errorf("write to an existing file reported TreeChanged: %+v", ev)
	}
	if len(ev.Dirs) != 1 || !sameDir(t, ev.Dirs[0], tmpDir) {
		t.Errorf("Dirs = %v, want [%s]", ev.Dirs, tmpDir)
	}
}

func TestTreeWatcher_RewatchesRecreatedDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	sub := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	w := newTestWatcher(t)
	w.SyncDirs([]string{tmpDir, sub})

	writeFile(t, filepath.Join(sub, "a.txt"), "x")
	waitForEvent(t, w)

	// A delete and recreate inside one debounce window: git checkout, a build
	// tool, or `rm -rf sub && mkdir sub`. fsnotify drops its own watch on the
	// delete, so the watcher has to notice and re-add it.
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, w) // The removal (and recreation) of sub itself

	w.SyncDirs([]string{tmpDir, sub})

	writeFile(t, filepath.Join(sub, "b.txt"), "x")
	if ev := waitForEvent(t, w); !ev.TreeChanged {
		t.Errorf("expected TreeChanged inside the recreated directory, got %+v", ev)
	}
}

func TestTreeWatcher_SetPreviewFile(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "preview.txt")
	writeFile(t, target, "x")

	w := newTestWatcher(t)
	if err := w.SetPreviewFile(target); err != nil {
		t.Fatalf("SetPreviewFile() failed: %v", err)
	}

	writeFile(t, target, "changed")

	ev := waitForEvent(t, w)
	if !ev.PreviewChanged {
		t.Errorf("expected PreviewChanged, got %+v", ev)
	}
}

func TestTreeWatcher_SetPreviewFile_IgnoresOtherFiles(t *testing.T) {
	tmpDir := t.TempDir()
	watched := filepath.Join(tmpDir, "watched.txt")
	other := filepath.Join(tmpDir, "other.txt")
	writeFile(t, watched, "x")
	writeFile(t, other, "x")

	w := newTestWatcher(t)
	if err := w.SetPreviewFile(watched); err != nil {
		t.Fatalf("SetPreviewFile() failed: %v", err)
	}

	// Writing another existing file is neither a preview nor a tree change.
	writeFile(t, other, "changed")
	expectNoFlaggedEvent(t, w)

	writeFile(t, watched, "changed")
	if ev := waitForEvent(t, w); !ev.PreviewChanged {
		t.Errorf("expected PreviewChanged, got %+v", ev)
	}
}

func TestTreeWatcher_SwitchPreviewFile(t *testing.T) {
	tmpDir := t.TempDir()
	first := filepath.Join(tmpDir, "first.txt")
	second := filepath.Join(tmpDir, "second.txt")
	writeFile(t, first, "x")
	writeFile(t, second, "x")

	w := newTestWatcher(t)
	if err := w.SetPreviewFile(first); err != nil {
		t.Fatal(err)
	}
	if err := w.SetPreviewFile(second); err != nil {
		t.Fatal(err)
	}

	writeFile(t, first, "changed")
	expectNoFlaggedEvent(t, w)

	writeFile(t, second, "changed")
	if ev := waitForEvent(t, w); !ev.PreviewChanged {
		t.Errorf("expected PreviewChanged for the new preview file, got %+v", ev)
	}
}

func TestTreeWatcher_SetPreviewFile_EmptyStopsWatching(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "preview.txt")
	writeFile(t, target, "x")

	w := newTestWatcher(t)
	if err := w.SetPreviewFile(target); err != nil {
		t.Fatal(err)
	}
	if err := w.SetPreviewFile(""); err != nil {
		t.Fatal(err)
	}

	w.mu.Lock()
	watched := len(w.watched)
	w.mu.Unlock()
	if watched != 0 {
		t.Errorf("watched %d dirs after clearing the preview file, want 0", watched)
	}

	writeFile(t, target, "changed")
	expectNoEvent(t, w)
}

func TestTreeWatcher_PreviewFileSurvivesSyncDirs(t *testing.T) {
	tmpDir := t.TempDir()
	sub := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(sub, "preview.txt")
	writeFile(t, target, "x")

	w := newTestWatcher(t)
	if err := w.SetPreviewFile(target); err != nil {
		t.Fatal(err)
	}
	// Collapsing every directory must not drop the preview watch.
	w.SyncDirs([]string{tmpDir})

	writeFile(t, target, "changed")
	if ev := waitForEvent(t, w); !ev.PreviewChanged {
		t.Errorf("expected PreviewChanged after SyncDirs, got %+v", ev)
	}
}

func TestTreeWatcher_DeletePreviewFile(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "preview.txt")
	writeFile(t, target, "x")

	w := newTestWatcher(t)
	w.SyncDirs([]string{tmpDir})
	if err := w.SetPreviewFile(target); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}

	ev := waitForEvent(t, w)
	if !ev.PreviewChanged || !ev.TreeChanged {
		t.Errorf("deleting the preview file should report both flags, got %+v", ev)
	}
}

func TestTreeWatcher_IgnoresGitDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	w := newTestWatcher(t)
	w.SyncDirs([]string{tmpDir, gitDir})

	writeFile(t, filepath.Join(gitDir, "index.lock"), "x")
	expectNoEvent(t, w)
}

func TestTreeWatcher_IgnoresSystemAndEditorFiles(t *testing.T) {
	tmpDir := t.TempDir()
	w := newTestWatcher(t)
	w.SyncDirs([]string{tmpDir})

	for _, name := range []string{".DS_Store", "._resource", "notes.txt.swp", "notes.txt~", ".#lock", "4913"} {
		writeFile(t, filepath.Join(tmpDir, name), "x")
	}
	expectNoEvent(t, w)

	// A real file still gets through afterwards.
	writeFile(t, filepath.Join(tmpDir, "real.txt"), "x")
	if ev := waitForEvent(t, w); !ev.TreeChanged {
		t.Errorf("expected TreeChanged for a real file, got %+v", ev)
	}
}

func TestIsIgnoredWatchPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/repo/.git/index", true},
		{"/repo/.git", true},
		{"/repo/src/.git/HEAD", true},
		{"/repo/.DS_Store", true},
		{"/repo/._file", true},
		{"/repo/file.swp", true},
		{"/repo/file.txt~", true},
		{"/repo/.#file.txt", true},
		{"/repo/4913", true},
		{"/repo/main.go", false},
		{"/repo/gitignore/main.go", false},
		{"/repo/.github/workflows/ci.yml", false},
	}
	for _, tc := range tests {
		if got := isIgnoredWatchPath(tc.path); got != tc.want {
			t.Errorf("isIgnoredWatchPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestTreeWatcher_CoalescesBurst(t *testing.T) {
	// Widened windows, because what is under test is that one burst flushes
	// once - not that 20 writes finish within 150ms. Under the production
	// windows a loaded runner can spend longer than the quiet period inside
	// the write loop, flushing mid-burst and failing the test for being slow
	// rather than for coalescing wrongly.
	const (
		burstQuiet      = 2 * time.Second
		burstMaxLatency = 10 * time.Second
	)
	w, err := newTreeWatcher(burstQuiet, burstMaxLatency)
	if err != nil {
		t.Fatalf("newTreeWatcher() failed: %v", err)
	}
	t.Cleanup(w.Stop)

	tmpDir := t.TempDir()
	preview := filepath.Join(tmpDir, "preview.txt")
	writeFile(t, preview, "x")

	w.SyncDirs([]string{tmpDir})
	if err := w.SetPreviewFile(preview); err != nil {
		t.Fatal(err)
	}

	// A burst of creates plus preview writes, all inside one quiet period.
	start := time.Now()
	for i := 0; i < 10; i++ {
		writeFile(t, filepath.Join(tmpDir, "burst"+string(rune('0'+i))+".txt"), "x")
		writeFile(t, preview, "change")
	}
	if spent := time.Since(start); spent >= burstQuiet {
		t.Fatalf("burst took %v, longer than the %v window it must fit inside", spent, burstQuiet)
	}

	ev := waitForEventWithin(t, w, burstQuiet+3*time.Second)
	if !ev.TreeChanged || !ev.PreviewChanged {
		t.Errorf("coalesced event lost a flag: %+v", ev)
	}

	// The burst must not queue up one event per write.
	select {
	case extra := <-w.Events():
		t.Errorf("burst produced more than one event: %+v", extra)
	case <-time.After(burstQuiet + 400*time.Millisecond):
	}
}

func TestTreeWatcher_ReportsChangedDirs(t *testing.T) {
	tmpDir := t.TempDir()
	sub := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	w := newTestWatcher(t)
	w.SyncDirs([]string{tmpDir, sub})

	// Two writes in the same directory plus one in the other; the coalesced
	// event should name each directory once.
	writeFile(t, filepath.Join(sub, "a.txt"), "x")
	writeFile(t, filepath.Join(sub, "b.txt"), "x")

	ev := waitForEvent(t, w)
	if !ev.TreeChanged {
		t.Fatalf("expected TreeChanged, got %+v", ev)
	}
	if len(ev.Dirs) != 1 {
		t.Fatalf("Dirs = %v, want one entry", ev.Dirs)
	}
	if !sameDir(t, ev.Dirs[0], sub) {
		t.Errorf("Dirs[0] = %q, want %q", ev.Dirs[0], sub)
	}
}

func TestMergeDirs(t *testing.T) {
	if got := mergeDirs([]string{"/a"}, []string{"/a", "/b"}); len(got) != 2 || got[1] != "/b" {
		t.Errorf("mergeDirs deduplicated wrong: %v", got)
	}

	full := make([]string, maxEventDirs)
	for i := range full {
		full[i] = filepath.Join("/dir", string(rune('a'+i%26)), string(rune('0'+i/26)))
	}
	if got := mergeDirs(full, []string{"/overflow"}); len(got) != maxEventDirs {
		t.Errorf("mergeDirs exceeded the cap: %d entries", len(got))
	}
}

// sameDir compares two directory paths, tolerating the symlinked temp dirs
// macOS hands out.
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	resolve := func(path string) string {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		return filepath.Clean(path)
	}
	return resolve(a) == resolve(b)
}

func TestTreeWatcher_MaxLatencyFlush(t *testing.T) {
	tmpDir := t.TempDir()
	w := newTestWatcher(t)
	w.SyncDirs([]string{tmpDir})

	// Keep writing faster than the quiet period so only the max-latency timer
	// can flush.
	stop := make(chan struct{})
	done := make(chan struct{})
	// Wait for the writer to leave the loop: t.TempDir's cleanup would otherwise
	// race a trailing write and fail with ENOTEMPTY.
	defer func() {
		close(stop)
		<-done
	}()
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.WriteFile(filepath.Join(tmpDir, "busy"+string(rune('a'+i%20))+".txt"), []byte("x"), 0644)
			_ = os.Remove(filepath.Join(tmpDir, "busy"+string(rune('a'+i%20))+".txt"))
			time.Sleep(watchQuietPeriod / 4)
		}
	}()

	start := time.Now()
	ev := waitForEvent(t, w)
	if !ev.TreeChanged {
		t.Errorf("expected TreeChanged, got %+v", ev)
	}
	if elapsed := time.Since(start); elapsed > watchMaxLatency+time.Second {
		t.Errorf("max-latency flush took %v, want <= %v", elapsed, watchMaxLatency+time.Second)
	}
}

func TestTreeWatcher_EmitMergesUndrainedEvent(t *testing.T) {
	// emit is only ever called from run(); drive it directly on a watcher with
	// no run goroutine so the test is the sole sender.
	w := &TreeWatcher{events: make(chan FSEvent, 1)}

	w.emit(FSEvent{TreeChanged: true})
	w.emit(FSEvent{PreviewChanged: true})

	ev := <-w.Events()
	if !ev.TreeChanged || !ev.PreviewChanged {
		t.Errorf("emit dropped a flag when the consumer was behind: %+v", ev)
	}
	select {
	case extra := <-w.Events():
		t.Errorf("expected a single merged event, got another: %+v", extra)
	default:
	}
}

func TestTreeWatcher_StopClosesEvents(t *testing.T) {
	w, err := NewTreeWatcher()
	if err != nil {
		t.Fatalf("NewTreeWatcher() failed: %v", err)
	}

	w.Stop()

	// Stop blocks until run() closed the channel, so this must not block.
	select {
	case _, ok := <-w.Events():
		if ok {
			t.Error("received an event after Stop()")
		}
	default:
		t.Error("events channel is still open after Stop() returned")
	}
}

func TestTreeWatcher_StopIsIdempotent(t *testing.T) {
	w, err := NewTreeWatcher()
	if err != nil {
		t.Fatalf("NewTreeWatcher() failed: %v", err)
	}
	w.Stop()
	w.Stop() // must not panic or block
}

func TestTreeWatcher_StopReleasesWatches(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, filepath.Join(tmpDir, "a.txt"), "x")

	w, err := NewTreeWatcher()
	if err != nil {
		t.Fatalf("NewTreeWatcher() failed: %v", err)
	}
	w.SyncDirs([]string{tmpDir})
	w.Stop()

	w.mu.Lock()
	watched := len(w.watched)
	w.mu.Unlock()
	if watched != 0 {
		t.Errorf("%d watches left registered after Stop(), want 0", watched)
	}
}

func TestTreeWatcher_CallsAfterStopAreNoops(t *testing.T) {
	tmpDir := t.TempDir()
	w, err := NewTreeWatcher()
	if err != nil {
		t.Fatalf("NewTreeWatcher() failed: %v", err)
	}
	w.Stop()

	w.SyncDirs([]string{tmpDir})
	if err := w.SetPreviewFile(filepath.Join(tmpDir, "a.txt")); err != nil {
		t.Errorf("SetPreviewFile() after Stop() returned %v, want nil", err)
	}

	w.mu.Lock()
	watched := len(w.watched)
	w.mu.Unlock()
	if watched != 0 {
		t.Errorf("watcher registered %d dirs after Stop(), want 0", watched)
	}
}

// --- plugin wiring ---

func TestPlugin_WatchStartedAfterStopStopsWatcher(t *testing.T) {
	tmpDir := t.TempDir()
	p := createRefreshTestPlugin(t, tmpDir, "a.txt")
	p.stopped = true

	w, err := NewTreeWatcher()
	if err != nil {
		t.Fatalf("NewTreeWatcher() failed: %v", err)
	}

	_, cmd := p.Update(WatchStartedMsg{Watcher: w})
	if cmd != nil {
		t.Error("expected no command for a watcher that arrived after Stop()")
	}
	if p.watcher != nil {
		t.Error("plugin adopted a watcher after Stop()")
	}
	select {
	case _, ok := <-w.Events():
		if ok {
			t.Error("late watcher was not stopped")
		}
	default:
		t.Error("late watcher was not stopped")
	}
}

func TestPlugin_WatchStartedSyncsDirsAndPreview(t *testing.T) {
	tmpDir := t.TempDir()
	p := createRefreshTestPlugin(t, tmpDir, "sub/a.txt")
	p.previewFile = filepath.Join("sub", "a.txt")

	// Expand the subdirectory so it should be watched too.
	if node := p.tree.FindByPath("sub"); node != nil {
		if err := p.tree.Expand(node); err != nil {
			t.Fatal(err)
		}
	}

	w, err := NewTreeWatcher()
	if err != nil {
		t.Fatalf("NewTreeWatcher() failed: %v", err)
	}
	defer w.Stop()

	p.Update(WatchStartedMsg{Watcher: w})
	if p.watcher != w {
		t.Fatal("plugin did not adopt the watcher")
	}

	w.mu.Lock()
	watched := len(w.watched)
	preview := w.previewFile
	w.mu.Unlock()

	if watched != 2 {
		t.Errorf("watched %d dirs, want 2 (root + expanded sub)", watched)
	}
	if want, _ := filepath.Abs(filepath.Join(tmpDir, "sub", "a.txt")); preview != want {
		t.Errorf("preview file = %q, want %q", preview, want)
	}
}

func TestPlugin_SyncWatcherDirsNilWatcherIsSafe(t *testing.T) {
	tmpDir := t.TempDir()
	p := createRefreshTestPlugin(t, tmpDir, "a.txt")
	p.syncWatcherDirs() // must not panic
}

func TestPlugin_WatchEventTreeChangedRefreshes(t *testing.T) {
	tmpDir := t.TempDir()
	p := createRefreshTestPlugin(t, tmpDir, "a.txt")

	_, cmd := p.Update(WatchEventMsg{TreeChanged: true})
	if cmd == nil {
		t.Fatal("expected a refresh command for a tree change")
	}
	if p.pendingAutoRefresh {
		t.Error("refresh should have run immediately, not been deferred")
	}
	if p.lastRefresh.IsZero() {
		t.Error("lastRefresh not updated")
	}
}

func TestPlugin_WatchEventDeferredWhileSearching(t *testing.T) {
	tmpDir := t.TempDir()
	p := createRefreshTestPlugin(t, tmpDir, "a.txt")
	p.searchMode = true

	p.Update(WatchEventMsg{TreeChanged: true})
	if !p.pendingAutoRefresh {
		t.Fatal("tree change during search should have been deferred")
	}

	// Leaving search mode flushes the deferred refresh on the next message.
	p.searchMode = false
	_, cmd := p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd == nil {
		t.Fatal("expected the deferred refresh to be flushed")
	}
	if p.pendingAutoRefresh {
		t.Error("pendingAutoRefresh not cleared after flushing")
	}
}

func TestPlugin_WatchEventDeferredWhileInlineEditing(t *testing.T) {
	tmpDir := t.TempDir()
	p := createRefreshTestPlugin(t, tmpDir, "a.txt")
	p.edit.Active = true // No inlineEditor: only the deferral is under test

	p.Update(WatchEventMsg{TreeChanged: true})
	if !p.pendingAutoRefresh {
		t.Error("tree change during inline editing should have been deferred")
	}
}

func TestPlugin_WatchEventDeferredWhileBlaming(t *testing.T) {
	tmpDir := t.TempDir()
	p := createRefreshTestPlugin(t, tmpDir, "a.txt")
	p.blameMode = true

	p.Update(WatchEventMsg{TreeChanged: true})
	if !p.pendingAutoRefresh {
		t.Error("tree change during a modal should have been deferred")
	}
}

// expectWatchListenerArmed runs cmd, flattening batches, and asserts that one of
// the commands is waiting on the watcher: a real filesystem change has to come
// back as another WatchEventMsg. The listener is one-shot, so re-arming it is
// the only thing that keeps auto-refresh alive.
func expectWatchListenerArmed(t *testing.T, dir string, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("update returned no command, so the watch listener was not re-armed")
	}

	msgs := make(chan tea.Msg, 16)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					run(sub)
				}
				return
			}
			msgs <- msg
		}()
	}
	run(cmd)

	writeFile(t, filepath.Join(dir, "armed.txt"), "x")

	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-msgs:
			if _, ok := msg.(WatchEventMsg); ok {
				return
			}
		case <-deadline:
			t.Fatal("no WatchEventMsg arrived; the watch listener was not re-armed")
		}
	}
}

// The listen loop is one-shot, so an event consumed by an early return in
// update() would kill auto-refresh for the rest of the session.
func TestPlugin_WatchEventAlwaysRearmsListener(t *testing.T) {
	states := map[string]func(*Plugin){
		"exit confirmation": func(p *Plugin) { p.edit.ShowExitConfirm = true },
		"inline editing": func(p *Plugin) {
			// An inactive editor makes update() take its "vim exited" branch,
			// which returns before reaching any message handling.
			p.edit.Active = true
			p.edit.Model = tty.New(nil)
		},
		"blame modal": func(p *Plugin) { p.blameMode = true },
	}

	for name, setup := range states {
		t.Run(name, func(t *testing.T) {
			tmpDir := t.TempDir()
			p := createRefreshTestPlugin(t, tmpDir, "a.txt")

			w, err := NewTreeWatcher()
			if err != nil {
				t.Fatalf("NewTreeWatcher() failed: %v", err)
			}
			defer w.Stop()
			p.watcher = w
			p.syncWatcherDirs()
			setup(p)

			_, cmd := p.Update(WatchEventMsg{TreeChanged: true})
			expectWatchListenerArmed(t, tmpDir, cmd)
		})
	}
}

func TestPlugin_WatchStartedStopsTheWatcherItReplaces(t *testing.T) {
	tmpDir := t.TempDir()
	p := createRefreshTestPlugin(t, tmpDir, "a.txt")

	first, err := NewTreeWatcher()
	if err != nil {
		t.Fatalf("NewTreeWatcher() failed: %v", err)
	}
	second, err := NewTreeWatcher()
	if err != nil {
		t.Fatalf("NewTreeWatcher() failed: %v", err)
	}
	defer second.Stop()

	// Two Start() calls straddling a project switch: the first watcher must not
	// be dropped on the floor with its goroutine and descriptors still live.
	p.Update(WatchStartedMsg{Watcher: first})
	p.Update(WatchStartedMsg{Watcher: second})

	if p.watcher != second {
		t.Fatal("plugin did not adopt the newer watcher")
	}
	select {
	case _, ok := <-first.Events():
		if ok {
			t.Error("the replaced watcher was not stopped")
		}
	default:
		t.Error("the replaced watcher was not stopped")
	}
}

func TestPlugin_WatchEventInvalidatesTabsWithoutTreeChange(t *testing.T) {
	tmpDir := t.TempDir()
	p := createRefreshTestPlugin(t, tmpDir, "a.txt", "sub/b.txt")

	p.tabs = []FileTab{
		{Path: "a.txt", Loaded: true},
		{Path: filepath.Join("sub", "b.txt"), Loaded: true},
	}
	p.activeTab = 0

	// An in-place rewrite of the background tab's file: no tree change, but the
	// tab is now holding pre-edit content.
	p.Update(WatchEventMsg{Dirs: []string{filepath.Join(tmpDir, "sub")}})

	if p.tabs[1].Loaded {
		t.Error("a background tab whose file was rewritten should be invalidated")
	}
}

func TestPlugin_WatchEventPreviewChangedReloadsPreview(t *testing.T) {
	tmpDir := t.TempDir()
	p := createRefreshTestPlugin(t, tmpDir, "a.txt")
	p.previewFile = "a.txt"

	_, cmd := p.Update(WatchEventMsg{PreviewChanged: true})
	if cmd == nil {
		t.Fatal("expected a preview reload command")
	}
	if p.pendingAutoRefresh {
		t.Error("a preview-only change should not schedule a tree refresh")
	}
}
