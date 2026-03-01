package copilot

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

// Watch watches for changes to Copilot CLI sessions.
// Since Copilot sessions are global, this watches all session directories
// but filters events by projectRoot.
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, io.Closer, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, err
	}

	// Watch the main session-state directory for new sessions
	if err := watcher.Add(a.stateDir); err != nil {
		_ = watcher.Close()
		return nil, nil, err
	}

	// Watch existing session directories for events.jsonl changes
	sessions, _ := a.Sessions(projectRoot)
	for _, session := range sessions {
		sessionDir := filepath.Dir(session.Path)
		_ = watcher.Add(sessionDir) // Best effort, ignore errors
	}

	events := make(chan adapter.Event, 32)

	go func() {
		var debounceTimer *time.Timer
		var lastEvent fsnotify.Event
		debounceDelay := 150 * time.Millisecond

		// Protect against sending to closed channel
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

				// Handle new session directory creation
				if event.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						_ = watcher.Add(event.Name)
						continue
					}
				}

				// Only watch for events.jsonl or workspace.yaml changes
				baseName := filepath.Base(event.Name)
				if baseName != "events.jsonl" && baseName != "workspace.yaml" {
					continue
				}

				mu.Lock()
				lastEvent = event

				// Debounce rapid events (Copilot writes frequently)
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					mu.Lock()
					defer mu.Unlock()

					if closed {
						return
					}

					// Extract session ID from path
					sessionDir := filepath.Dir(lastEvent.Name)
					sessionID := filepath.Base(sessionDir)

					// Verify this session belongs to the current project
					if !a.sessionMatchesProject(sessionID, projectRoot) {
						return
					}

					var eventType adapter.EventType
					switch {
					case lastEvent.Op&fsnotify.Create != 0:
						if strings.HasSuffix(lastEvent.Name, "workspace.yaml") {
							eventType = adapter.EventSessionCreated
						} else {
							eventType = adapter.EventMessageAdded
						}
					case lastEvent.Op&fsnotify.Write != 0:
						if strings.HasSuffix(lastEvent.Name, "workspace.yaml") {
							eventType = adapter.EventSessionUpdated
						} else {
							eventType = adapter.EventMessageAdded
						}
					default:
						return
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

// sessionMatchesProject checks if a session belongs to the given project.
func (a *Adapter) sessionMatchesProject(sessionID, projectRoot string) bool {
	a.mu.RLock()
	sessionDir, ok := a.sessionIndex[sessionID]
	a.mu.RUnlock()

	if !ok {
		sessionDir = filepath.Join(a.stateDir, sessionID)
	}

	workspaceFile := filepath.Join(sessionDir, "workspace.yaml")
	ws, err := a.readWorkspaceCached(workspaceFile)
	if err != nil {
		return false
	}

	return ws.GitRoot == projectRoot || ws.CWD == projectRoot
}
