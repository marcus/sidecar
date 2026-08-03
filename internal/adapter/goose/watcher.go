package goose

import (
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/marcus/sidecar/internal/adapter"
)

// NewWatcher watches Goose's SQLite DB and WAL files.
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
			case events <- adapter.Event{Type: eventType}:
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
					default:
						emit(adapter.EventSessionUpdated)
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
