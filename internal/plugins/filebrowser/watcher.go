package filebrowser

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// watchQuietPeriod is how long the filesystem has to go quiet before a
	// batch of changes is reported. Editors and build tools write in bursts.
	watchQuietPeriod = 150 * time.Millisecond

	// watchMaxLatency caps how long a change can sit unreported while writes
	// keep arriving, so a continuously busy directory still refreshes.
	watchMaxLatency = 1 * time.Second

	// maxWatchedDirs caps how many directories are watched at once.
	//
	// On macOS fsnotify uses kqueue, which needs one file descriptor per file
	// inside every watched directory (measured: one 500-file directory costs
	// 501 descriptors). The cap is therefore deliberately small.
	maxWatchedDirs = 32

	// maxEventDirs caps how many distinct directories a single coalesced event
	// reports. Consumers use the list to invalidate caches; past this many the
	// batch is broad enough that the exact set stops being interesting.
	maxEventDirs = 64
)

// FSEvent is a coalesced batch of filesystem changes.
type FSEvent struct {
	// TreeChanged is set when entries appeared, disappeared, or were renamed
	// inside a watched directory - i.e. when the tree needs rebuilding.
	TreeChanged bool
	// PreviewChanged is set when the currently previewed file was touched.
	PreviewChanged bool
	// Dirs holds the absolute directories something changed in, deduplicated
	// and capped at maxEventDirs. In-place writes count: consumers cache file
	// contents, not just directory listings.
	Dirs []string
}

// TreeWatcher watches the expanded directories of the file tree plus the
// directory holding the previewed file, coalescing bursts of filesystem
// activity into at most one pending event.
type TreeWatcher struct {
	fsWatcher *fsnotify.Watcher
	events    chan FSEvent
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once

	// Coalescing windows, fixed at construction because run() reads them
	// without the lock. Production always uses the constants; tests that must
	// fit a whole burst inside one window widen them.
	quietPeriod time.Duration
	maxLatency  time.Duration

	mu          sync.Mutex
	closed      bool
	watched     map[string]bool // Directories currently registered with fsnotify
	treeDirs    map[string]bool // Directories requested by SyncDirs
	previewFile string          // Absolute path of the previewed file ("" = none)
	previewDir  string          // Directory holding previewFile
}

// NewTreeWatcher creates a watcher. Nothing is watched until SyncDirs or
// SetPreviewFile is called.
func NewTreeWatcher() (*TreeWatcher, error) {
	return newTreeWatcher(watchQuietPeriod, watchMaxLatency)
}

// newTreeWatcher builds a watcher with explicit coalescing windows. A test that
// asserts a burst of writes produces exactly one event has to fit every write
// inside one quiet period; on a loaded machine the production 150ms window can
// expire mid-burst, which reports the same coalescing behaviour as a failure.
func newTreeWatcher(quiet, maxLatency time.Duration) (*TreeWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &TreeWatcher{
		fsWatcher:   fsw,
		events:      make(chan FSEvent, 1),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
		watched:     make(map[string]bool),
		treeDirs:    make(map[string]bool),
		quietPeriod: quiet,
		maxLatency:  maxLatency,
	}

	go w.run()
	return w, nil
}

// SyncDirs makes the watched directory set match dirs, adding and removing only
// what actually changed. Order matters: dirs beyond maxWatchedDirs are dropped,
// so callers should pass the most interesting directories first.
func (w *TreeWatcher) SyncDirs(dirs []string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return
	}

	next := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		if len(next) >= maxWatchedDirs {
			break
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		next[abs] = true
	}

	w.treeDirs = next
	w.reconcileLocked()
}

// SetPreviewFile points the preview watch at path (absolute or relative to the
// working directory). Pass "" to stop watching a preview file.
func (w *TreeWatcher) SetPreviewFile(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	if path == "" {
		w.previewFile = ""
		w.previewDir = ""
		w.reconcileLocked()
		return nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	w.previewFile = abs
	w.previewDir = filepath.Dir(abs)
	w.reconcileLocked()
	return nil
}

// reconcileLocked brings the fsnotify registrations in line with the desired
// set (tree directories plus the preview file's directory).
func (w *TreeWatcher) reconcileLocked() {
	desired := make(map[string]bool, len(w.treeDirs)+1)
	for dir := range w.treeDirs {
		desired[dir] = true
	}
	if w.previewDir != "" {
		desired[w.previewDir] = true
	}

	for dir := range w.watched {
		if !desired[dir] {
			_ = w.fsWatcher.Remove(dir)
			delete(w.watched, dir)
		}
	}
	for dir := range desired {
		if w.watched[dir] {
			continue
		}
		if err := w.fsWatcher.Add(dir); err != nil {
			continue // Directory vanished or is unreadable; nothing to watch
		}
		w.watched[dir] = true
	}
}

// classify decides what a raw filesystem event means for the UI.
func (w *TreeWatcher) classify(event fsnotify.Event) FSEvent {
	if event.Op == fsnotify.Chmod {
		return FSEvent{} // Permission/timestamp churn only
	}
	if isIgnoredWatchPath(event.Name) {
		return FSEvent{}
	}

	abs, err := filepath.Abs(event.Name)
	if err != nil {
		return FSEvent{}
	}

	var out FSEvent

	w.mu.Lock()
	previewFile := w.previewFile
	watched := w.watched[filepath.Dir(abs)]
	// fsnotify unregisters a watched directory itself once it is removed or
	// renamed, so forget it here too. Otherwise reconcileLocked keeps believing
	// it is registered and never re-adds it when the directory comes back
	// (git checkout, `rm -rf dir && mkdir dir`).
	if w.watched[abs] && event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		delete(w.watched, abs)
	}
	w.mu.Unlock()

	if previewFile != "" && abs == previewFile {
		out.PreviewChanged = true
	}
	if watched {
		// Only structural changes need a tree rebuild; writes to an existing
		// file do not change what the tree displays. Both still name the
		// directory, because consumers also cache file contents.
		if event.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
			out.TreeChanged = true
		}
		out.Dirs = []string{filepath.Dir(abs)}
	}
	return out
}

// mergeDirs appends the entries of src that dst does not already have, up to
// maxEventDirs.
func mergeDirs(dst, src []string) []string {
	for _, dir := range src {
		if len(dst) >= maxEventDirs {
			return dst
		}
		if slices.Contains(dst, dir) {
			continue
		}
		dst = append(dst, dir)
	}
	return dst
}

// isIgnoredWatchPath reports whether a path is noise the file browser never shows.
func isIgnoredWatchPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".git" {
			return true
		}
	}
	base := filepath.Base(path)
	if isSystemFile(base) {
		return true
	}
	// Editor scratch files: vim swap/backup, emacs lock files, vim's probe file.
	if strings.HasPrefix(base, ".#") || strings.HasSuffix(base, "~") || base == "4913" {
		return true
	}
	switch filepath.Ext(base) {
	case ".swp", ".swx", ".swo", ".tmp":
		return true
	}
	return false
}

// run processes filesystem events, coalescing bursts into a single pending event.
func (w *TreeWatcher) run() {
	defer func() {
		close(w.events)
		close(w.done)
	}()

	quiet := time.NewTimer(time.Hour)
	stopTimer(quiet)
	maxLatency := time.NewTimer(time.Hour)
	stopTimer(maxLatency)

	var (
		pending  FSEvent
		quietC   <-chan time.Time
		maxLatC  <-chan time.Time
		hasEvent bool
	)

	flush := func() {
		stopTimer(quiet)
		stopTimer(maxLatency)
		quietC, maxLatC = nil, nil
		if !hasEvent {
			return
		}
		w.emit(pending)
		pending = FSEvent{}
		hasEvent = false
	}

	for {
		select {
		case <-w.stop:
			return

		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			change := w.classify(event)
			if !change.TreeChanged && !change.PreviewChanged && len(change.Dirs) == 0 {
				continue
			}
			pending.TreeChanged = pending.TreeChanged || change.TreeChanged
			pending.PreviewChanged = pending.PreviewChanged || change.PreviewChanged
			pending.Dirs = mergeDirs(pending.Dirs, change.Dirs)

			// Restart the quiet period on every change; start the max-latency
			// timer only once per batch so a busy directory still reports.
			stopTimer(quiet)
			quiet.Reset(w.quietPeriod)
			quietC = quiet.C
			if !hasEvent {
				maxLatency.Reset(w.maxLatency)
				maxLatC = maxLatency.C
			}
			hasEvent = true

		case <-quietC:
			flush()

		case <-maxLatC:
			flush()

		case _, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			// Ignore errors and keep watching.
		}
	}
}

// emit delivers ev on the cap-1 events channel, merging with an event the
// consumer has not picked up yet. Only run() sends, so the drain-then-send is
// guaranteed to have room.
func (w *TreeWatcher) emit(ev FSEvent) {
	select {
	case old := <-w.events:
		ev.TreeChanged = ev.TreeChanged || old.TreeChanged
		ev.PreviewChanged = ev.PreviewChanged || old.PreviewChanged
		ev.Dirs = mergeDirs(ev.Dirs, old.Dirs)
	default:
	}
	w.events <- ev
}

func stopTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

// Events returns the channel of coalesced filesystem events. It is closed once
// the watcher stops.
func (w *TreeWatcher) Events() <-chan FSEvent {
	return w.events
}

// Stop shuts the watcher down and blocks until the event channel is closed, so
// a caller that has stopped a watcher can never see another event from it.
func (w *TreeWatcher) Stop() {
	w.stopOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		// fsnotify's kqueue backend marks itself closed before unregistering,
		// so Close() alone leaks one descriptor per watched file on macOS.
		// Remove the directories first.
		for dir := range w.watched {
			_ = w.fsWatcher.Remove(dir)
		}
		w.watched = make(map[string]bool)
		w.treeDirs = make(map[string]bool)
		w.mu.Unlock()

		close(w.stop)
		_ = w.fsWatcher.Close()
	})
	<-w.done
}
