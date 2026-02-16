package copilot

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
	"gopkg.in/yaml.v3"
)

const (
	adapterID   = "copilot-cli"
	adapterName = "GitHub Copilot CLI"
	adapterIcon = "⋮⋮"
)

// Adapter implements the adapter.Adapter interface for GitHub Copilot CLI sessions.
type Adapter struct {
	stateDir     string
	sessionIndex map[string]string // sessionID -> directory path
	mu           sync.RWMutex      // guards sessionIndex
}

// New creates a new GitHub Copilot CLI adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		stateDir:     filepath.Join(home, ".copilot", "session-state"),
		sessionIndex: make(map[string]string),
	}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return adapterID }

// Name returns the human-readable adapter name.
func (a *Adapter) Name() string { return adapterName }

// Icon returns the adapter icon for badge display.
func (a *Adapter) Icon() string { return adapterIcon }

// Detect checks if Copilot CLI sessions exist for the given project.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	// List all session directories
	entries, err := os.ReadDir(a.stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	// Check if any session matches this project
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Read workspace.yaml to check project root
		workspaceFile := filepath.Join(a.stateDir, entry.Name(), "workspace.yaml")
		data, err := os.ReadFile(workspaceFile)
		if err != nil {
			continue
		}

		var ws WorkspaceYAML
		if err := yaml.Unmarshal(data, &ws); err != nil {
			continue
		}

		// Match by git_root or cwd
		if ws.GitRoot == projectRoot || ws.CWD == projectRoot {
			return true, nil
		}
	}

	return false, nil
}

// Capabilities returns the supported features.
func (a *Adapter) Capabilities() adapter.CapabilitySet {
	return adapter.CapabilitySet{
		adapter.CapSessions: true,
		adapter.CapMessages: true,
		adapter.CapUsage:    false, // Copilot CLI doesn't expose token usage in events
		adapter.CapWatch:    true,
	}
}

// WatchScope returns global since Copilot sessions are stored globally in ~/.copilot
func (a *Adapter) WatchScope() adapter.WatchScope {
	return adapter.WatchScopeGlobal
}

// Sessions returns all sessions for the given project, sorted by update time.
func (a *Adapter) Sessions(projectRoot string) ([]adapter.Session, error) {
	entries, err := os.ReadDir(a.stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []adapter.Session{}, nil
		}
		return nil, fmt.Errorf("failed to read session directory: %w", err)
	}

	var sessions []adapter.Session
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		sessionID := entry.Name()
		sessionDir := filepath.Join(a.stateDir, sessionID)

		// Read workspace.yaml for metadata
		workspaceFile := filepath.Join(sessionDir, "workspace.yaml")
		data, err := os.ReadFile(workspaceFile)
		if err != nil {
			continue // Skip sessions without valid workspace.yaml
		}

		var ws WorkspaceYAML
		if err := yaml.Unmarshal(data, &ws); err != nil {
			continue
		}

		// Filter by project root
		if ws.GitRoot != projectRoot && ws.CWD != projectRoot {
			continue
		}

		// Get events.jsonl file info
		eventsFile := filepath.Join(sessionDir, "events.jsonl")
		fileInfo, err := os.Stat(eventsFile)
		if err != nil {
			continue
		}

		// Count messages
		msgCount := a.countMessages(eventsFile)

		// Check if session is active (updated within last 5 minutes)
		isActive := time.Since(ws.UpdatedAt) < 5*time.Minute

		// Create slug (first 12 chars of UUID)
		slug := sessionID
		if len(slug) > 12 {
			slug = slug[:12]
		}

		session := adapter.Session{
			ID:           sessionID,
			Name:         ws.Summary,
			Slug:         slug,
			AdapterID:    a.ID(),
			AdapterName:  a.Name(),
			AdapterIcon:  a.Icon(),
			CreatedAt:    ws.CreatedAt,
			UpdatedAt:    ws.UpdatedAt,
			Duration:     ws.UpdatedAt.Sub(ws.CreatedAt),
			IsActive:     isActive,
			MessageCount: msgCount,
			FileSize:     fileInfo.Size(),
			Path:         eventsFile,
		}

		sessions = append(sessions, session)

		// Cache the session path
		a.mu.Lock()
		a.sessionIndex[sessionID] = sessionDir
		a.mu.Unlock()
	}

	// Sort by updated time (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// countMessages counts user and assistant messages in the events file.
func (a *Adapter) countMessages(eventsFile string) int {
	f, err := os.Open(eventsFile)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event CopilotEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		if event.Type == "user.message" || event.Type == "assistant.message" {
			count++
		}
	}

	return count
}

// Messages returns all messages for the given session.
func (a *Adapter) Messages(sessionID string) ([]adapter.Message, error) {
	// Get session directory
	a.mu.RLock()
	sessionDir, ok := a.sessionIndex[sessionID]
	a.mu.RUnlock()

	if !ok {
		// Try to find it
		sessionDir = filepath.Join(a.stateDir, sessionID)
		if _, err := os.Stat(sessionDir); err != nil {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}

		a.mu.Lock()
		a.sessionIndex[sessionID] = sessionDir
		a.mu.Unlock()
	}

	eventsFile := filepath.Join(sessionDir, "events.jsonl")
	f, err := os.Open(eventsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open events file: %w", err)
	}
	defer f.Close()

	return a.parseMessages(f)
}

// parseMessages parses messages from events.jsonl
func (a *Adapter) parseMessages(r io.Reader) ([]adapter.Message, error) {
	var messages []adapter.Message
	toolResults := make(map[string]string) // toolCallId -> result content

	scanner := bufio.NewScanner(r)
	// Increase buffer size for large events
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024) // 1MB max token size

	for scanner.Scan() {
		var event CopilotEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue // Skip malformed events
		}

		switch event.Type {
		case "user.message":
			msg := a.parseUserMessage(event)
			messages = append(messages, msg)

		case "assistant.message":
			msg := a.parseAssistantMessage(event, toolResults)
			messages = append(messages, msg)

		case "tool.execution_complete":
			// Store tool results for linking
			if toolCallID, ok := event.Data["toolCallId"].(string); ok {
				if resultData, ok := event.Data["result"].(map[string]interface{}); ok {
					if content, ok := resultData["content"].(string); ok {
						toolResults[toolCallID] = content
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading events: %w", err)
	}

	return messages, nil
}

// parseUserMessage extracts a user message from an event.
func (a *Adapter) parseUserMessage(event CopilotEvent) adapter.Message {
	content := ""
	if c, ok := event.Data["content"].(string); ok {
		content = c
	}

	return adapter.Message{
		ID:        event.ID,
		Role:      "user",
		Content:   content,
		Timestamp: event.Timestamp,
	}
}

// parseAssistantMessage extracts an assistant message with tool calls.
func (a *Adapter) parseAssistantMessage(event CopilotEvent, toolResults map[string]string) adapter.Message {
	content := ""
	if c, ok := event.Data["content"].(string); ok {
		content = c
	}

	msg := adapter.Message{
		ID:        event.ID,
		Role:      "assistant",
		Content:   content,
		Timestamp: event.Timestamp,
	}

	// Extract model if available
	if model, ok := event.Data["model"].(string); ok {
		msg.Model = model
	}

	// Extract tool requests
	if toolReqs, ok := event.Data["toolRequests"].([]interface{}); ok {
		for _, tr := range toolReqs {
			toolMap, ok := tr.(map[string]interface{})
			if !ok {
				continue
			}

			toolCallID, _ := toolMap["toolCallId"].(string)
			toolName, _ := toolMap["name"].(string)

			// Serialize arguments to JSON
			argsJSON := "{}"
			if args, ok := toolMap["arguments"].(map[string]interface{}); ok {
				if data, err := json.Marshal(args); err == nil {
					argsJSON = string(data)
				}
			}

			// Get result if available
			result := toolResults[toolCallID]

			toolUse := adapter.ToolUse{
				ID:     toolCallID,
				Name:   toolName,
				Input:  argsJSON,
				Output: result,
			}
			msg.ToolUses = append(msg.ToolUses, toolUse)
		}
	}

	return msg
}

// Usage returns usage statistics for the session.
// Note: Copilot CLI doesn't expose token usage in events, so this returns empty stats.
func (a *Adapter) Usage(sessionID string) (*adapter.UsageStats, error) {
	// Copilot CLI events.jsonl doesn't include token usage data
	return &adapter.UsageStats{}, nil
}

