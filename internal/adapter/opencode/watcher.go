package opencode

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/marcus/sidecar/internal/adapter"
)

// NewWatcher creates a watcher for OpenCode session changes.
func NewWatcher(sessionDir string) (<-chan adapter.Event, io.Closer, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, err
	}

	if err := watcher.Add(sessionDir); err != nil {
		_ = watcher.Close() // Ignore close error after failed Add
		return nil, nil, err
	}

	events := make(chan adapter.Event, 32)

	go func() {
		// Debounce timer
		var debounceTimer *time.Timer
		var lastEvent fsnotify.Event
		debounceDelay := 100 * time.Millisecond

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

				// Only watch .json files
				if !strings.HasSuffix(event.Name, ".json") {
					continue
				}

				mu.Lock()
				lastEvent = event

				// Debounce rapid events
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					mu.Lock()
					defer mu.Unlock()

					if closed {
						return
					}

					sessionID := strings.TrimSuffix(filepath.Base(lastEvent.Name), ".json")

					var eventType adapter.EventType
					switch {
					case lastEvent.Op&fsnotify.Create != 0:
						eventType = adapter.EventSessionCreated
					case lastEvent.Op&fsnotify.Write != 0:
						eventType = adapter.EventSessionUpdated
					case lastEvent.Op&fsnotify.Remove != 0:
						// Session deleted, skip for now
						return
					default:
						eventType = adapter.EventSessionUpdated
					}

					select {
					case events <- adapter.Event{
						Type:      eventType,
						SessionID: sessionID,
					}:
					default:
						// Channel full, drop event
					}
				})
				mu.Unlock()

			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
				// Log error but continue watching
			}
		}
	}()

	return events, watcher, nil
}

// NewDBWatcher watches opencode.db and opencode.db-wal for SQLite-backed updates.
func NewDBWatcher(dbPath string) (<-chan adapter.Event, io.Closer, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, err
	}

	dbDir := filepath.Dir(dbPath)
	if err := watcher.Add(dbDir); err != nil {
		_ = watcher.Close()
		return nil, nil, err
	}

	walPath := dbPath + "-wal"
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

		emit := func(eventType adapter.EventType) {
			select {
			case events <- adapter.Event{Type: eventType, SessionID: ""}:
			default:
			}
		}

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Name != dbPath && event.Name != walPath {
					continue
				}

				mu.Lock()
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				op := event.Op
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					mu.Lock()
					defer mu.Unlock()
					if closed {
						return
					}
					switch {
					case op&fsnotify.Create != 0:
						emit(adapter.EventSessionCreated)
					case op&fsnotify.Write != 0:
						emit(adapter.EventSessionUpdated)
					case op&fsnotify.Remove != 0:
						emit(adapter.EventSessionUpdated)
					case op&fsnotify.Rename != 0:
						emit(adapter.EventSessionUpdated)
					default:
						emit(adapter.EventSessionUpdated)
					}
				})
				mu.Unlock()

			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
				// Keep watching on transient watcher errors.
			}
		}
	}()

	// Ensure we can stat directory/file immediately to catch obvious path issues.
	if _, err := os.Stat(dbPath); err != nil && !os.IsNotExist(err) {
		_ = watcher.Close()
		return nil, nil, err
	}

	return events, watcher, nil
}
