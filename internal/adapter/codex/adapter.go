package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
)

// scannerBufPool recycles buffers for bufio.Scanner to reduce allocations.
var scannerBufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 1024*1024)
	},
}

func getScannerBuffer() []byte {
	return scannerBufPool.Get().([]byte)
}

func putScannerBuffer(buf []byte) {
	scannerBufPool.Put(buf)
}

const (
	adapterID           = "codex"
	adapterName         = "Codex"
	metaCacheMaxEntries = 2048
)

// Adapter implements the adapter.Adapter interface for Codex CLI sessions.
type Adapter struct {
	sessionsDir     string
	sessionIndex    map[string]string      // sessionID -> file path cache
	totalUsageCache map[string]*TokenUsage // sessionID -> total usage (populated by Messages)
	mu              sync.RWMutex           // guards sessionIndex and totalUsageCache
	metaCache       map[string]sessionMetaCacheEntry
	metaMu          sync.RWMutex // guards metaCache
}

// New creates a new Codex adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		sessionsDir:     filepath.Join(home, ".codex", "sessions"),
		sessionIndex:    make(map[string]string),
		totalUsageCache: make(map[string]*TokenUsage),
		metaCache:       make(map[string]sessionMetaCacheEntry),
	}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return adapterID }

// Name returns the human-readable adapter name.
func (a *Adapter) Name() string { return adapterName }

// Icon returns the adapter icon for badge display.
func (a *Adapter) Icon() string { return "▶" }

// Detect checks if Codex sessions exist for the given project.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	files, err := a.sessionFiles()
	if err != nil {
		return false, err
	}
	for _, path := range files {
		meta, err := a.sessionMetadata(path)
		if err != nil {
			continue
		}
		if cwdMatchesProject(projectRoot, meta.CWD) {
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
		adapter.CapUsage:    true,
		adapter.CapWatch:    true,
	}
}

// Sessions returns all sessions for the given project, sorted by update time.
func (a *Adapter) Sessions(projectRoot string) ([]adapter.Session, error) {
	files, err := a.sessionFiles()
	if err != nil {
		return nil, err
	}

	sessions := make([]adapter.Session, 0, len(files))
	seenPaths := make(map[string]struct{}, len(files))
	// Build new index, then swap atomically to avoid race with sessionFilePath()
	newIndex := make(map[string]string, len(files))
	for _, path := range files {
		seenPaths[path] = struct{}{}
		meta, err := a.sessionMetadata(path)
		if err != nil {
			continue
		}
		if !cwdMatchesProject(projectRoot, meta.CWD) {
			continue
		}

		// Use first user message as name, fallback to short ID
		name := ""
		if meta.FirstUserMessage != "" {
			name = truncateTitle(meta.FirstUserMessage, 50)
		}
		if name == "" {
			name = shortID(meta.SessionID)
		}
		sessions = append(sessions, adapter.Session{
			ID:           meta.SessionID,
			Name:         name,
			AdapterID:    adapterID,
			AdapterName:  adapterName,
			AdapterIcon:  a.Icon(),
			CreatedAt:    meta.FirstMsg,
			UpdatedAt:    meta.LastMsg,
			Duration:     meta.LastMsg.Sub(meta.FirstMsg),
			IsActive:     time.Since(meta.LastMsg) < 5*time.Minute,
			TotalTokens:  meta.TotalTokens,
			MessageCount: meta.MsgCount,
		})

		// Add to new index (will be swapped atomically after loop)
		newIndex[meta.SessionID] = path
	}

	// Atomically swap in the new index
	a.mu.Lock()
	a.sessionIndex = newIndex
	a.mu.Unlock()

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	a.pruneSessionMetaCache(seenPaths)

	return sessions, nil
}

// Messages returns all messages for the given session.
func (a *Adapter) Messages(sessionID string) ([]adapter.Message, error) {
	path := a.sessionFilePath(sessionID)
	if path == "" {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var messages []adapter.Message
	var pendingTools []adapter.ToolUse
	toolIndex := make(map[string]int)
	var pendingThinking []adapter.ThinkingBlock
	var pendingUsage *adapter.TokenUsage
	var totalUsage *TokenUsage // Captured for Usage() to avoid re-scan
	var currentModel string
	var lastTimestamp time.Time

	flushPending := func(ts time.Time) {
		if len(pendingTools) == 0 && len(pendingThinking) == 0 {
			return
		}
		msg := adapter.Message{
			ID:             "synthetic-" + shortID(sessionID) + "-" + fmt.Sprintf("%d", len(messages)),
			Role:           "assistant",
			Content:        "tool calls",
			Timestamp:      ts,
			Model:          currentModel,
			ToolUses:       append([]adapter.ToolUse(nil), pendingTools...),
			ThinkingBlocks: append([]adapter.ThinkingBlock(nil), pendingThinking...),
		}
		if pendingUsage != nil {
			msg.TokenUsage = *pendingUsage
			pendingUsage = nil
		}
		messages = append(messages, msg)
		pendingTools = nil
		pendingThinking = nil
		toolIndex = make(map[string]int)
	}

	scanner := bufio.NewScanner(file)
	buf := getScannerBuffer()
	defer putScannerBuffer(buf)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		var record RawRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if !record.Timestamp.IsZero() {
			lastTimestamp = record.Timestamp
		}

		switch record.Type {
		case "turn_context":
			var payload TurnContextPayload
			if err := json.Unmarshal(record.Payload, &payload); err == nil && payload.Model != "" {
				currentModel = payload.Model
			}

		case "response_item":
			var base ResponseItemBase
			if err := json.Unmarshal(record.Payload, &base); err != nil {
				continue
			}
			switch base.Type {
			case "message":
				var msg ResponseMessagePayload
				if err := json.Unmarshal(record.Payload, &msg); err != nil {
					continue
				}
				if msg.Role != "user" && msg.Role != "assistant" {
					continue
				}
				if msg.Role == "user" {
					flushPending(record.Timestamp)
				}

				content := contentFromBlocks(msg.Content)
				message := adapter.Message{
					ID:        fmt.Sprintf("%s-%d", sessionID, len(messages)),
					Role:      msg.Role,
					Content:   content,
					Timestamp: record.Timestamp,
					Model:     currentModel,
				}
				if msg.Role == "assistant" {
					message.ToolUses = append(message.ToolUses, pendingTools...)
					message.ThinkingBlocks = append(message.ThinkingBlocks, pendingThinking...)
					pendingTools = nil
					pendingThinking = nil
					toolIndex = make(map[string]int)
					if pendingUsage != nil {
						message.TokenUsage = *pendingUsage
						pendingUsage = nil
					}
				}
				messages = append(messages, message)

			case "function_call", "custom_tool_call":
				var call ResponseToolCallPayload
				if err := json.Unmarshal(record.Payload, &call); err != nil {
					continue
				}
				input := toolInputString(call.Arguments, call.Input)
				tool := adapter.ToolUse{
					ID:    call.CallID,
					Name:  call.Name,
					Input: input,
				}
				toolIndex[call.CallID] = len(pendingTools)
				pendingTools = append(pendingTools, tool)

			case "function_call_output", "custom_tool_call_output":
				var output ResponseToolOutputPayload
				if err := json.Unmarshal(record.Payload, &output); err != nil {
					continue
				}
				out := toolOutputString(output.Output)
				if idx, ok := toolIndex[output.CallID]; ok && idx < len(pendingTools) {
					pendingTools[idx].Output = out
				} else {
					toolIndex[output.CallID] = len(pendingTools)
					pendingTools = append(pendingTools, adapter.ToolUse{
						ID:     output.CallID,
						Output: out,
					})
				}

			case "reasoning":
				var reason ResponseReasoningPayload
				if err := json.Unmarshal(record.Payload, &reason); err != nil {
					continue
				}
				for _, summary := range reason.Summary {
					if strings.TrimSpace(summary.Text) == "" {
						continue
					}
					pendingThinking = append(pendingThinking, adapter.ThinkingBlock{
						Content:    summary.Text,
						TokenCount: len(summary.Text) / 4,
					})
				}
			}

		case "event_msg":
			var event EventMsgPayload
			if err := json.Unmarshal(record.Payload, &event); err != nil {
				continue
			}
			switch event.Type {
			case "agent_reasoning":
				if strings.TrimSpace(event.Text) != "" {
					pendingThinking = append(pendingThinking, adapter.ThinkingBlock{
						Content:    event.Text,
						TokenCount: len(event.Text) / 4,
					})
				}
			case "token_count":
				if event.Info == nil {
					continue
				}
				if event.Info.LastTokenUsage != nil {
					pendingUsage = convertUsage(event.Info.LastTokenUsage)
				}
				if event.Info.TotalTokenUsage != nil {
					totalUsage = event.Info.TotalTokenUsage
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if info, err := file.Stat(); err == nil {
		a.invalidateSessionMetaCacheIfChanged(path, info)
	}

	flushPending(lastTimestamp)

	// Cache total usage for Usage() to avoid re-scanning
	if totalUsage != nil {
		a.mu.Lock()
		a.totalUsageCache[sessionID] = totalUsage
		a.mu.Unlock()
	}

	return messages, nil
}

// Usage returns aggregate usage stats for the given session.
func (a *Adapter) Usage(sessionID string) (*adapter.UsageStats, error) {
	messages, err := a.Messages(sessionID)
	if err != nil {
		return nil, err
	}

	stats := &adapter.UsageStats{}
	for _, msg := range messages {
		stats.TotalInputTokens += msg.InputTokens
		stats.TotalOutputTokens += msg.OutputTokens
		stats.TotalCacheRead += msg.CacheRead
		stats.TotalCacheWrite += msg.CacheWrite
		stats.MessageCount++
	}

	// If per-message stats are zero, use cached total from Messages() scan
	if stats.TotalInputTokens == 0 && stats.TotalOutputTokens == 0 && stats.TotalCacheRead == 0 {
		a.mu.RLock()
		usage := a.totalUsageCache[sessionID]
		a.mu.RUnlock()
		if usage != nil {
			stats.TotalInputTokens = usage.InputTokens
			stats.TotalOutputTokens = usage.OutputTokens + usage.ReasoningOutputTokens
			stats.TotalCacheRead = usage.CachedInputTokens
		}
	}

	return stats, nil
}

// Watch returns a channel that emits events when session data changes.
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, error) {
	return NewWatcher(a.sessionsDir)
}

func (a *Adapter) sessionFiles() ([]string, error) {
	if _, err := os.Stat(a.sessionsDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []string
	err := filepath.WalkDir(a.sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

type sessionMetaCacheEntry struct {
	meta       *SessionMetadata
	modTime    time.Time
	size       int64
	lastAccess time.Time
}

func (a *Adapter) sessionMetadata(path string) (*SessionMetadata, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	a.metaMu.RLock()
	if entry, ok := a.metaCache[path]; ok && entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
		// Return a copy to prevent caller mutations affecting cache
		metaCopy := *entry.meta
		a.metaMu.RUnlock()

		a.metaMu.Lock()
		if entry, ok := a.metaCache[path]; ok {
			entry.lastAccess = now
			a.metaCache[path] = entry
		}
		a.metaMu.Unlock()
		return &metaCopy, nil
	}
	a.metaMu.RUnlock()

	meta, err := a.parseSessionMetadata(path)
	if err != nil {
		return nil, err
	}

	a.metaMu.Lock()
	a.metaCache[path] = sessionMetaCacheEntry{
		meta:       meta,
		modTime:    info.ModTime(),
		size:       info.Size(),
		lastAccess: now,
	}
	a.enforceSessionMetaCacheLimitLocked()
	a.metaMu.Unlock()
	return meta, nil
}

func (a *Adapter) pruneSessionMetaCache(seenPaths map[string]struct{}) {
	a.metaMu.Lock()
	for path := range a.metaCache {
		if _, ok := seenPaths[path]; !ok {
			delete(a.metaCache, path)
		}
	}
	a.enforceSessionMetaCacheLimitLocked()
	a.metaMu.Unlock()
}

func (a *Adapter) enforceSessionMetaCacheLimitLocked() {
	for len(a.metaCache) > metaCacheMaxEntries {
		var oldestPath string
		var oldestAccess time.Time
		for path, entry := range a.metaCache {
			if oldestPath == "" || entry.lastAccess.Before(oldestAccess) {
				oldestPath = path
				oldestAccess = entry.lastAccess
			}
		}
		if oldestPath == "" {
			return
		}
		delete(a.metaCache, oldestPath)
	}
}

func (a *Adapter) invalidateSessionMetaCacheIfChanged(path string, info os.FileInfo) {
	if info == nil {
		return
	}
	a.metaMu.Lock()
	if entry, ok := a.metaCache[path]; ok {
		if entry.size != info.Size() || !entry.modTime.Equal(info.ModTime()) {
			delete(a.metaCache, path)
		}
	}
	a.metaMu.Unlock()
}

func (a *Adapter) parseSessionMetadata(path string) (*SessionMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	meta := &SessionMetadata{Path: path}
	var sessionTimestamp time.Time
	var lastRecord time.Time
	var totalTokens int

	scanner := bufio.NewScanner(file)
	buf := getScannerBuffer()
	defer putScannerBuffer(buf)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		var record RawRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}

		if !record.Timestamp.IsZero() {
			lastRecord = record.Timestamp
		}

		switch record.Type {
		case "session_meta":
			var payload SessionMetaPayload
			if err := json.Unmarshal(record.Payload, &payload); err != nil {
				continue
			}
			if meta.SessionID == "" {
				meta.SessionID = payload.ID
			}
			if meta.CWD == "" {
				meta.CWD = payload.CWD
			}
			if sessionTimestamp.IsZero() && !payload.Timestamp.IsZero() {
				sessionTimestamp = payload.Timestamp
			}

		case "response_item":
			var base ResponseItemBase
			if err := json.Unmarshal(record.Payload, &base); err != nil {
				continue
			}
			if base.Type != "message" {
				continue
			}
			var msg ResponseMessagePayload
			if err := json.Unmarshal(record.Payload, &msg); err != nil {
				continue
			}
			if msg.Role != "user" && msg.Role != "assistant" {
				continue
			}
			if meta.FirstMsg.IsZero() {
				meta.FirstMsg = record.Timestamp
			}
			// Extract first user message content for title
			if meta.FirstUserMessage == "" && msg.Role == "user" {
				if content := contentFromBlocks(msg.Content); content != "" {
					meta.FirstUserMessage = content
				}
			}
			meta.LastMsg = record.Timestamp
			meta.MsgCount++

		case "event_msg":
			var event EventMsgPayload
			if err := json.Unmarshal(record.Payload, &event); err != nil {
				continue
			}
			if event.Type != "token_count" || event.Info == nil {
				continue
			}
			usage := event.Info.TotalTokenUsage
			if usage == nil {
				usage = event.Info.LastTokenUsage
			}
			if usage != nil {
				totalTokens = usage.TotalTokens
				if totalTokens == 0 {
					totalTokens = usage.InputTokens + usage.OutputTokens + usage.ReasoningOutputTokens
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if meta.SessionID == "" {
		meta.SessionID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}

	if meta.FirstMsg.IsZero() {
		meta.FirstMsg = sessionTimestamp
	}
	if meta.LastMsg.IsZero() {
		meta.LastMsg = sessionTimestamp
	}
	if meta.FirstMsg.IsZero() && !lastRecord.IsZero() {
		meta.FirstMsg = lastRecord
		meta.LastMsg = lastRecord
	}
	if meta.FirstMsg.IsZero() {
		if info, err := os.Stat(path); err == nil {
			meta.FirstMsg = info.ModTime()
			meta.LastMsg = info.ModTime()
		} else {
			meta.FirstMsg = time.Now()
			meta.LastMsg = meta.FirstMsg
		}
	}
	if meta.LastMsg.IsZero() {
		meta.LastMsg = meta.FirstMsg
	}

	meta.TotalTokens = totalTokens

	return meta, nil
}

func (a *Adapter) sessionFilePath(sessionID string) string {
	a.mu.RLock()
	if path, ok := a.sessionIndex[sessionID]; ok && path != "" {
		a.mu.RUnlock()
		return path
	}
	a.mu.RUnlock()

	files, err := a.sessionFiles()
	if err != nil {
		return ""
	}
	for _, path := range files {
		if strings.Contains(filepath.Base(path), sessionID) {
			return path
		}
	}

	for _, path := range files {
		meta, err := a.sessionMetadata(path)
		if err != nil {
			continue
		}
		if meta.SessionID == sessionID {
			return path
		}
	}

	return ""
}

func contentFromBlocks(blocks []ContentBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	var parts []string
	for _, block := range blocks {
		if block.Text == "" {
			continue
		}
		parts = append(parts, block.Text)
	}
	return strings.Join(parts, "\n")
}

func toolInputString(arguments, input json.RawMessage) string {
	if len(arguments) > 0 && string(arguments) != "null" {
		return rawToString(arguments)
	}
	if len(input) > 0 && string(input) != "null" {
		return rawToString(input)
	}
	return ""
}

func toolOutputString(output json.RawMessage) string {
	if len(output) == 0 || string(output) == "null" {
		return ""
	}
	return rawToString(output)
}

func rawToString(raw json.RawMessage) string {
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}
	return string(raw)
}

func convertUsage(usage *TokenUsage) *adapter.TokenUsage {
	if usage == nil {
		return nil
	}
	return &adapter.TokenUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens + usage.ReasoningOutputTokens,
		CacheRead:    usage.CachedInputTokens,
	}
}

func cwdMatchesProject(projectRoot, cwd string) bool {
	if projectRoot == "" || cwd == "" {
		return false
	}
	projectAbs, err := filepath.Abs(projectRoot)
	if err != nil {
		return false
	}
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(projectAbs); err == nil {
		projectAbs = resolved
	}
	if resolved, err := filepath.EvalSymlinks(cwdAbs); err == nil {
		cwdAbs = resolved
	}
	projectAbs = filepath.Clean(projectAbs)
	cwdAbs = filepath.Clean(cwdAbs)

	rel, err := filepath.Rel(projectAbs, cwdAbs)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..")
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// truncateTitle truncates text to maxLen, adding "..." if truncated.
// It also replaces newlines with spaces for display.
func truncateTitle(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)

	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
