package codex

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/marcus/sidecar/internal/adapter"
)

// NewWatcher creates a watcher for Codex session changes.
// Only watches recent month directories to reduce resource usage (td-ae05cd6a).
func NewWatcher(root string) (<-chan adapter.Event, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Watch root for new year/month directories
	if err := watcher.Add(root); err != nil {
		watcher.Close()
		return nil, err
	}

	// Watch only recent month directories (td-ae05cd6a)
	for _, dir := range recentSessionDirs(root) {
		// Ignore errors - directories may not exist yet
		_ = addWatchTree(watcher, dir)
	}

	events := make(chan adapter.Event, 32)

	go func() {
		defer watcher.Close()

		var debounceTimer *time.Timer
		var lastEvent fsnotify.Event
		debounceDelay := 200 * time.Millisecond // Increased from 100ms (td-11c31ccd)

		// Protect against sending to closed channel from timer callback
		var closed bool
		var mu sync.Mutex

		defer func() {
			mu.Lock()
			closed = true
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			mu.Unlock()
			close(events)
		}()

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						_ = addWatchTree(watcher, event.Name)
						continue
					}
				}

				if !strings.HasSuffix(event.Name, ".jsonl") {
					continue
				}

				mu.Lock()
				lastEvent = event
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					mu.Lock()
					defer mu.Unlock()

					if closed {
						return
					}

					sessionID := strings.TrimSuffix(filepath.Base(lastEvent.Name), ".jsonl")
					var eventType adapter.EventType
					switch {
					case lastEvent.Op&fsnotify.Create != 0:
						eventType = adapter.EventSessionCreated
					case lastEvent.Op&fsnotify.Write != 0:
						eventType = adapter.EventMessageAdded
					case lastEvent.Op&fsnotify.Remove != 0:
						return
					default:
						eventType = adapter.EventSessionUpdated
					}

					select {
					case events <- adapter.Event{Type: eventType, SessionID: sessionID}:
					default:
					}
				})
				mu.Unlock()

			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return events, nil
}

// recentSessionDirs returns directories for current and previous months (td-ae05cd6a).
// Codex organizes sessions by date: sessions/YYYY/MM/DD/session.jsonl
func recentSessionDirs(root string) []string {
	now := time.Now()
	dirs := make([]string, 0, 2)

	// Current month
	dirs = append(dirs, filepath.Join(root, now.Format("2006"), now.Format("01")))

	// Previous month (for sessions started last month)
	prev := now.AddDate(0, -1, 0)
	dirs = append(dirs, filepath.Join(root, prev.Format("2006"), prev.Format("01")))

	return dirs
}

func addWatchTree(watcher *fsnotify.Watcher, root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			_ = watcher.Add(path)
		}
		return nil
	})
}
