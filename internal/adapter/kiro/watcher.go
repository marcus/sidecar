package kiro

import (
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/marcus/sidecar/internal/adapter"
)

// NewWatcher creates a watcher for Kiro SQLite changes.
// Watches the WAL file for modifications since Kiro uses WAL mode.
func NewWatcher(dbPath string) (<-chan adapter.Event, io.Closer, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, err
	}

	dbDir := filepath.Dir(dbPath)
	if err := watcher.Add(dbDir); err != nil {
		_ = watcher.Close()
		return nil, nil, err
	}

	walFile := dbPath + "-wal"
	events := make(chan adapter.Event, 32)

	go func() {
		var debounceTimer *time.Timer
		debounceDelay := 100 * time.Millisecond

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

				if event.Name != walFile && event.Name != dbPath {
					continue
				}

				if event.Op&fsnotify.Write == 0 {
					continue
				}

				mu.Lock()
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					mu.Lock()
					defer mu.Unlock()

					if closed {
						return
					}

					select {
					case events <- adapter.Event{
						Type: adapter.EventSessionUpdated,
					}:
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

	return events, watcher, nil
}
