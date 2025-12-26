package gitstatus

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors the .git directory for changes.
type Watcher struct {
	fsWatcher *fsnotify.Watcher
	events    chan struct{}
	stop      chan struct{}
	mu        sync.Mutex
	stopped   bool
}

// NewWatcher creates a new git directory watcher.
func NewWatcher(workDir string) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		fsWatcher: fsWatcher,
		events:    make(chan struct{}, 1),
		stop:      make(chan struct{}),
	}

	// Watch .git/index for staging changes
	gitDir := filepath.Join(workDir, ".git")
	indexPath := filepath.Join(gitDir, "index")
	headPath := filepath.Join(gitDir, "HEAD")
	refsDir := filepath.Join(gitDir, "refs")

	// Add watches
	if err := fsWatcher.Add(gitDir); err != nil {
		fsWatcher.Close()
		return nil, err
	}
	// Try to watch index directly (may not exist yet)
	_ = fsWatcher.Add(indexPath)
	_ = fsWatcher.Add(headPath)
	_ = fsWatcher.Add(refsDir)

	go w.run()

	return w, nil
}

// Events returns the channel that receives change notifications.
func (w *Watcher) Events() <-chan struct{} {
	return w.events
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return
	}
	w.stopped = true

	close(w.stop)
	w.fsWatcher.Close()
}

// run processes file system events.
func (w *Watcher) run() {
	// Debounce timer
	var debounceTimer *time.Timer
	debounceDelay := 100 * time.Millisecond

	for {
		select {
		case <-w.stop:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}

			// Only care about relevant files
			name := filepath.Base(event.Name)
			dir := filepath.Dir(event.Name)
			if name != "index" && name != "HEAD" && name != "COMMIT_EDITMSG" && name != "FETCH_HEAD" {
				// Check if it's a refs change
				if !strings.Contains(dir, "refs") {
					continue
				}
			}

			// Debounce rapid events
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounceDelay, func() {
				select {
				case w.events <- struct{}{}:
				default:
					// Channel full, skip
				}
			})

		case _, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			// Log error but continue
		}
	}
}
