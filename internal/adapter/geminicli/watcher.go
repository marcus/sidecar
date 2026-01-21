package geminicli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/marcus/sidecar/internal/adapter"
)

// sessionIDPattern extracts sessionId field from partial JSON
var sessionIDPattern = regexp.MustCompile(`"sessionId"\s*:\s*"([^"]+)"`)

// NewWatcher creates a watcher for Gemini CLI session changes.
func NewWatcher(chatsDir string) (<-chan adapter.Event, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := watcher.Add(chatsDir); err != nil {
		watcher.Close()
		return nil, err
	}

	events := make(chan adapter.Event, 32)

	go func() {
		defer watcher.Close()

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

				// Only watch session-*.json files
				name := filepath.Base(event.Name)
				if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".json") {
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

					sessionID := extractSessionID(lastEvent.Name)
					if sessionID == "" {
						return
					}

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
			}
		}
	}()

	return events, nil
}

// extractSessionID reads only the first 512 bytes of the session file
// and extracts the sessionId field using regex. This avoids reading
// the entire file during watch events.
func extractSessionID(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	// Read only the first 512 bytes - sessionId is always near the start
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil || n == 0 {
		return ""
	}

	// Extract sessionId using regex
	if match := sessionIDPattern.FindSubmatch(buf[:n]); match != nil {
		return string(match[1])
	}
	return ""
}
