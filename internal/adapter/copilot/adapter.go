package copilot

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/adapter/cache"
	"gopkg.in/yaml.v3"
)

const (
	adapterID   = "copilot-cli"
	adapterName = "GitHub Copilot CLI"
	adapterIcon = "⋮⋮"

	metaCacheMaxEntries = 2048
	msgCacheMaxEntries  = 128
)

// messageCacheEntry holds cached messages with state for incremental parsing.
type messageCacheEntry struct {
	messages    []adapter.Message
	toolResults map[string]string // toolCallId -> result (for incremental linking)
	byteOffset  int64
}

// metaCacheEntry holds cached workspace.yaml metadata.
type metaCacheEntry struct {
	ws      WorkspaceYAML
	modTime time.Time
	size    int64
}

// Adapter implements the adapter.Adapter interface for GitHub Copilot CLI sessions.
type Adapter struct {
	stateDir     string
	sessionIndex map[string]string // sessionID -> directory path
	metaCache    map[string]metaCacheEntry // workspace.yaml path -> cached metadata
	msgCache     *cache.Cache[messageCacheEntry]
	mu           sync.RWMutex // guards sessionIndex
	metaMu       sync.RWMutex // guards metaCache
}

// New creates a new GitHub Copilot CLI adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		stateDir:     filepath.Join(home, ".copilot", "session-state"),
		sessionIndex: make(map[string]string),
		metaCache:    make(map[string]metaCacheEntry),
		msgCache:     cache.New[messageCacheEntry](msgCacheMaxEntries),
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
	entries, err := os.ReadDir(a.stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		workspaceFile := filepath.Join(a.stateDir, entry.Name(), "workspace.yaml")
		ws, err := a.readWorkspaceCached(workspaceFile)
		if err != nil {
			continue
		}

		if ws.GitRoot == projectRoot || ws.CWD == projectRoot {
			return true, nil
		}
	}

	return false, nil
}

// readWorkspaceCached reads and caches workspace.yaml metadata.
func (a *Adapter) readWorkspaceCached(path string) (WorkspaceYAML, error) {
	info, err := os.Stat(path)
	if err != nil {
		return WorkspaceYAML{}, err
	}

	a.metaMu.RLock()
	cached, ok := a.metaCache[path]
	a.metaMu.RUnlock()

	if ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		return cached.ws, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return WorkspaceYAML{}, err
	}

	var ws WorkspaceYAML
	if err := yaml.Unmarshal(data, &ws); err != nil {
		return WorkspaceYAML{}, err
	}

	a.metaMu.Lock()
	a.metaCache[path] = metaCacheEntry{ws: ws, modTime: info.ModTime(), size: info.Size()}
	// Evict oldest if over limit
	if len(a.metaCache) > metaCacheMaxEntries {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range a.metaCache {
			if oldestKey == "" || v.modTime.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.modTime
			}
		}
		delete(a.metaCache, oldestKey)
	}
	a.metaMu.Unlock()

	return ws, nil
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

		workspaceFile := filepath.Join(sessionDir, "workspace.yaml")
		ws, err := a.readWorkspaceCached(workspaceFile)
		if err != nil {
			continue
		}

		if ws.GitRoot != projectRoot && ws.CWD != projectRoot {
			continue
		}

		eventsFile := filepath.Join(sessionDir, "events.jsonl")
		fileInfo, err := os.Stat(eventsFile)
		if err != nil {
			continue
		}

		// Use cached message count from msgCache if available
		msgCount := a.countMessagesCached(eventsFile, fileInfo)

		isActive := time.Since(ws.UpdatedAt) < 5*time.Minute

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

		a.mu.Lock()
		a.sessionIndex[sessionID] = sessionDir
		a.mu.Unlock()
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// countMessagesCached returns cached message count or counts from file.
func (a *Adapter) countMessagesCached(eventsFile string, info os.FileInfo) int {
	if a.msgCache != nil {
		cached, _, cachedSize, cachedModTime, ok := a.msgCache.GetWithOffset(eventsFile)
		if ok && info.Size() == cachedSize && info.ModTime().Equal(cachedModTime) {
			return len(cached.messages)
		}
	}
	return a.countMessages(eventsFile)
}

// countMessages counts user and assistant messages in the events file.
func (a *Adapter) countMessages(eventsFile string) int {
	f, err := os.Open(eventsFile)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	scanner, buf := cache.NewScanner(f)
	defer cache.PutScannerBuffer(buf)

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
	a.mu.RLock()
	sessionDir, ok := a.sessionIndex[sessionID]
	a.mu.RUnlock()

	if !ok {
		sessionDir = filepath.Join(a.stateDir, sessionID)
		if _, err := os.Stat(sessionDir); err != nil {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}

		a.mu.Lock()
		a.sessionIndex[sessionID] = sessionDir
		a.mu.Unlock()
	}

	eventsFile := filepath.Join(sessionDir, "events.jsonl")
	info, err := os.Stat(eventsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to stat events file: %w", err)
	}

	// 3-way cache decision
	if a.msgCache != nil {
		cached, offset, cachedSize, cachedModTime, ok := a.msgCache.GetWithOffset(eventsFile)
		if ok {
			// Exact hit: file unchanged
			if info.Size() == cachedSize && info.ModTime().Equal(cachedModTime) {
				return copyMessages(cached.messages), nil
			}

			// File grew: incremental parse from saved offset
			if info.Size() > cachedSize && offset > 0 {
				messages, entry, err := a.parseMessagesIncremental(eventsFile, cached, offset)
				if err == nil {
					a.msgCache.Set(eventsFile, entry, info.Size(), info.ModTime(), entry.byteOffset)
					return copyMessages(messages), nil
				}
				// Fall through to full parse on error
			}
			// File shrank or other change: full re-parse
		}
	}

	// Full parse
	messages, entry, err := a.parseMessagesFull(eventsFile)
	if err != nil {
		return nil, err
	}

	if a.msgCache != nil {
		a.msgCache.Set(eventsFile, entry, info.Size(), info.ModTime(), entry.byteOffset)
	}
	return copyMessages(messages), nil
}

// parseMessagesFull parses the entire events.jsonl file.
func (a *Adapter) parseMessagesFull(path string) ([]adapter.Message, messageCacheEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, messageCacheEntry{}, fmt.Errorf("failed to open events file: %w", err)
	}
	defer f.Close()

	var messages []adapter.Message
	toolResults := make(map[string]string)
	// Track tool use locations for retroactive linking: toolCallId -> (msgIdx, toolUseIdx)
	toolUseIndex := make(map[string][2]int)
	var bytesRead int64

	scanner, buf := cache.NewScanner(f)
	defer cache.PutScannerBuffer(buf)

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesRead += int64(len(line)) + 1

		var event CopilotEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		switch event.Type {
		case "user.message":
			messages = append(messages, a.parseUserMessage(event))
		case "assistant.message":
			msg := a.parseAssistantMessage(event, toolResults)
			msgIdx := len(messages)
			// Index tool uses for retroactive linking
			for tuIdx, tu := range msg.ToolUses {
				toolUseIndex[tu.ID] = [2]int{msgIdx, tuIdx}
			}
			messages = append(messages, msg)
		case "tool.execution_complete":
			a.extractToolResult(event, toolResults)
			// Retroactively link result to existing tool use
			if toolCallID, ok := event.Data["toolCallId"].(string); ok {
				if loc, found := toolUseIndex[toolCallID]; found {
					if result, hasResult := toolResults[toolCallID]; hasResult {
						messages[loc[0]].ToolUses[loc[1]].Output = result
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, messageCacheEntry{}, fmt.Errorf("error reading events: %w", err)
	}

	entry := messageCacheEntry{
		messages:    copyMessages(messages),
		toolResults: copyStringMap(toolResults),
		byteOffset:  bytesRead,
	}

	return messages, entry, nil
}

// parseMessagesIncremental parses only new bytes appended to events.jsonl.
func (a *Adapter) parseMessagesIncremental(path string, cached messageCacheEntry, startOffset int64) ([]adapter.Message, messageCacheEntry, error) {
	reader, err := cache.NewIncrementalReader(path, startOffset)
	if err != nil {
		return nil, messageCacheEntry{}, err
	}
	defer func() { _ = reader.Close() }()

	messages := copyMessages(cached.messages)
	toolResults := copyStringMap(cached.toolResults)

	// Rebuild tool use index from existing messages
	toolUseIndex := make(map[string][2]int)
	for msgIdx, msg := range messages {
		for tuIdx, tu := range msg.ToolUses {
			toolUseIndex[tu.ID] = [2]int{msgIdx, tuIdx}
		}
	}

	for {
		line, err := reader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, messageCacheEntry{}, err
		}

		var event CopilotEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		switch event.Type {
		case "user.message":
			messages = append(messages, a.parseUserMessage(event))
		case "assistant.message":
			msg := a.parseAssistantMessage(event, toolResults)
			msgIdx := len(messages)
			for tuIdx, tu := range msg.ToolUses {
				toolUseIndex[tu.ID] = [2]int{msgIdx, tuIdx}
			}
			messages = append(messages, msg)
		case "tool.execution_complete":
			a.extractToolResult(event, toolResults)
			if toolCallID, ok := event.Data["toolCallId"].(string); ok {
				if loc, found := toolUseIndex[toolCallID]; found {
					if result, hasResult := toolResults[toolCallID]; hasResult {
						messages[loc[0]].ToolUses[loc[1]].Output = result
					}
				}
			}
		}
	}

	entry := messageCacheEntry{
		messages:    copyMessages(messages),
		toolResults: copyStringMap(toolResults),
		byteOffset:  reader.Offset(),
	}

	return messages, entry, nil
}

// extractToolResult stores a tool result from a tool.execution_complete event.
func (a *Adapter) extractToolResult(event CopilotEvent, toolResults map[string]string) {
	toolCallID, ok := event.Data["toolCallId"].(string)
	if !ok {
		return
	}
	if resultData, ok := event.Data["result"].(map[string]interface{}); ok {
		if content, ok := resultData["content"].(string); ok {
			toolResults[toolCallID] = content
		}
	}
}

// parseMessages parses messages from a reader (used by tests).
func (a *Adapter) parseMessages(r io.Reader) ([]adapter.Message, error) {
	var messages []adapter.Message
	toolResults := make(map[string]string)
	toolUseIndex := make(map[string][2]int)

	scanner, buf := cache.NewScanner(r)
	defer cache.PutScannerBuffer(buf)

	for scanner.Scan() {
		var event CopilotEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		switch event.Type {
		case "user.message":
			messages = append(messages, a.parseUserMessage(event))
		case "assistant.message":
			msg := a.parseAssistantMessage(event, toolResults)
			msgIdx := len(messages)
			for tuIdx, tu := range msg.ToolUses {
				toolUseIndex[tu.ID] = [2]int{msgIdx, tuIdx}
			}
			messages = append(messages, msg)
		case "tool.execution_complete":
			a.extractToolResult(event, toolResults)
			if toolCallID, ok := event.Data["toolCallId"].(string); ok {
				if loc, found := toolUseIndex[toolCallID]; found {
					if result, hasResult := toolResults[toolCallID]; hasResult {
						messages[loc[0]].ToolUses[loc[1]].Output = result
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
	return &adapter.UsageStats{}, nil
}

// copyMessages creates a deep copy of messages slice.
func copyMessages(msgs []adapter.Message) []adapter.Message {
	if msgs == nil {
		return nil
	}
	cp := make([]adapter.Message, len(msgs))
	for i, m := range msgs {
		cp[i] = m
		if m.ToolUses != nil {
			cp[i].ToolUses = make([]adapter.ToolUse, len(m.ToolUses))
			copy(cp[i].ToolUses, m.ToolUses)
		}
	}
	return cp
}

// copyStringMap creates a copy of a string map.
func copyStringMap(m map[string]string) map[string]string {
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
