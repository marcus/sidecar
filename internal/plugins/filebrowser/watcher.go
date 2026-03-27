package filebrowser

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a single file for changes.
// Only watches the currently previewed file, not the entire directory tree.
type Watcher struct {
	fsWatcher   *fsnotify.Watcher
	watchedFile string // Currently watched file (absolute path)
	events      chan struct{}
	stop        chan struct{}
	debounce    *time.Timer
	mu          sync.Mutex
	closed      bool
}

// NewWatcher creates a file watcher. Does not start watching anything until WatchFile is called.
func NewWatcher() (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		fsWatcher: fsw,
		events:    make(chan struct{}, 1),
		stop:      make(chan struct{}),
	}

	go w.run()
	return w, nil
}

// WatchFile starts watching the specified file. Stops watching any previously watched file.
// Pass empty string to stop watching without watching a new file.
func (w *Watcher) WatchFile(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	// Remove old watch if any
	if w.watchedFile != "" {
		// Watch the directory containing the file (fsnotify works better with directories)
		oldDir := filepath.Dir(w.watchedFile)
		_ = w.fsWatcher.Remove(oldDir)
		w.watchedFile = ""
	}

	// Add new watch if path provided
	if path != "" {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		// Watch the directory containing the file (fsnotify is more reliable with directories)
		dir := filepath.Dir(absPath)
		if err := w.fsWatcher.Add(dir); err != nil {
			return err
		}
		w.watchedFile = absPath
	}

	return nil
}

// run processes file system events.
func (w *Watcher) run() {
	defer func() {
		w.mu.Lock()
		w.closed = true
		if w.debounce != nil {
			w.debounce.Stop()
		}
		w.mu.Unlock()
		close(w.events)
	}()

	for {
		select {
		case <-w.stop:
			return
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}

			w.mu.Lock()
			// Only process events for the watched file
			watchedFile := w.watchedFile
			w.mu.Unlock()

			if watchedFile == "" {
				continue
			}

			// Check if event is for our watched file
			eventPath, _ := filepath.Abs(event.Name)
			if eventPath != watchedFile {
				continue
			}

			// Debounce: wait 100ms for more events before signaling
			w.mu.Lock()
			if w.debounce != nil {
				w.debounce.Stop()
			}
			w.debounce = time.AfterFunc(100*time.Millisecond, func() {
				w.mu.Lock()
				defer w.mu.Unlock()

				if w.closed {
					return
				}

				select {
				case w.events <- struct{}{}:
				default: // Channel full, skip
				}
			})
			w.mu.Unlock()

		case _, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			// Ignore errors, continue watching
		}
	}
}

// Events returns a channel that signals when the watched file changes.
func (w *Watcher) Events() <-chan struct{} {
	return w.events
}

// Stop shuts down the watcher.
func (w *Watcher) Stop() {
	close(w.stop)
	_ = w.fsWatcher.Close()
}
