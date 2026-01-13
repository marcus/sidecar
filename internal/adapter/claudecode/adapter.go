package claudecode

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
// We use 1MB initial buffer (default is 4KB) to reduce resizing, with 10MB max.
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
	adapterID           = "claude-code"
	adapterName         = "Claude Code"
	metaCacheMaxEntries = 2048
)

// Adapter implements the adapter.Adapter interface for Claude Code sessions.
type Adapter struct {
	projectsDir  string
	sessionIndex map[string]string // sessionID -> file path cache
	metaCache    map[string]sessionMetaCacheEntry
	mu           sync.RWMutex // guards sessionIndex
	metaMu       sync.RWMutex // guards metaCache
}

// New creates a new Claude Code adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		projectsDir:  filepath.Join(home, ".claude", "projects"),
		sessionIndex: make(map[string]string),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return adapterID }

// Name returns the human-readable adapter name.
func (a *Adapter) Name() string { return adapterName }

// Icon returns the adapter icon for badge display.
func (a *Adapter) Icon() string { return "◆" }

// Detect checks if Claude Code sessions exist for the given project.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	dir := a.projectDirPath(projectRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
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
	dir := a.projectDirPath(projectRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	sessions := make([]adapter.Session, 0, len(entries))
	seenPaths := make(map[string]struct{}, len(entries))
	// Build new index, then swap atomically to avoid race with sessionFilePath()
	newIndex := make(map[string]string, len(entries))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}

		path := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		meta, err := a.sessionMetadata(path, info)
		if err != nil {
			continue
		}
		seenPaths[path] = struct{}{}

		// Use first user message as name, with fallbacks
		name := ""
		if meta.FirstUserMessage != "" {
			name = truncateTitle(meta.FirstUserMessage, 50)
		}
		if name == "" && meta.Slug != "" {
			name = meta.Slug
		}
		if name == "" {
			name = shortID(meta.SessionID)
		}

		// Detect sub-agent by filename prefix
		isSubAgent := strings.HasPrefix(e.Name(), "agent-")

		// Add to new index (will be swapped atomically after loop)
		newIndex[meta.SessionID] = path

		sessions = append(sessions, adapter.Session{
			ID:           meta.SessionID,
			Name:         name,
			Slug:         meta.Slug,
			AdapterID:    adapterID,
			AdapterName:  adapterName,
			AdapterIcon:  a.Icon(),
			CreatedAt:    meta.FirstMsg,
			UpdatedAt:    meta.LastMsg,
			Duration:     meta.LastMsg.Sub(meta.FirstMsg),
			IsActive:     time.Since(meta.LastMsg) < 5*time.Minute,
			TotalTokens:  meta.TotalTokens,
			EstCost:      meta.EstCost,
			IsSubAgent:   isSubAgent,
			MessageCount: meta.MsgCount,
		})
	}

	// Atomically swap in the new index
	a.mu.Lock()
	a.sessionIndex = newIndex
	a.mu.Unlock()

	// Sort by UpdatedAt descending (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	a.pruneSessionMetaCache(dir, seenPaths)

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
	// Track tool use locations for deferred result linking: toolUseID -> (message index, tool use index, content block index)
	toolUseRefs := make(map[string]toolUseRef)

	scanner := bufio.NewScanner(file)
	buf := getScannerBuffer()
	defer putScannerBuffer(buf)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		var raw RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}

		// Skip non-message types
		if raw.Type != "user" && raw.Type != "assistant" {
			continue
		}
		if raw.Message == nil {
			continue
		}

		msg := adapter.Message{
			ID:        raw.UUID,
			Role:      raw.Message.Role,
			Timestamp: raw.Timestamp,
			Model:     raw.Message.Model,
		}

		// Parse content (no tool results linking yet)
		content, toolUses, thinkingBlocks, contentBlocks := a.parseContentWithResults(raw.Message.Content, nil)
		msg.Content = content
		msg.ToolUses = toolUses
		msg.ThinkingBlocks = thinkingBlocks
		msg.ContentBlocks = contentBlocks

		// Parse usage
		if raw.Message.Usage != nil {
			msg.TokenUsage = adapter.TokenUsage{
				InputTokens:  raw.Message.Usage.InputTokens,
				OutputTokens: raw.Message.Usage.OutputTokens,
				CacheRead:    raw.Message.Usage.CacheReadInputTokens,
				CacheWrite:   raw.Message.Usage.CacheCreationInputTokens,
			}
		}

		msgIdx := len(messages)
		messages = append(messages, msg)

		// For assistant messages, track tool use references for later linking
		if raw.Type == "assistant" {
			for toolIdx, tu := range messages[msgIdx].ToolUses {
				if tu.ID != "" {
					toolUseRefs[tu.ID] = toolUseRef{msgIdx: msgIdx, toolIdx: toolIdx, contentIdx: -1}
				}
			}
			// Also track in content blocks
			for contentIdx, cb := range messages[msgIdx].ContentBlocks {
				if cb.Type == "tool_use" && cb.ToolUseID != "" {
					if ref, ok := toolUseRefs[cb.ToolUseID]; ok {
						ref.contentIdx = contentIdx
						toolUseRefs[cb.ToolUseID] = ref
					}
				}
			}
		}

		// For user messages, link tool results to previously seen tool uses
		if raw.Type == "user" {
			a.linkToolResults(raw.Message.Content, messages, toolUseRefs)
		}
	}

	if err := scanner.Err(); err != nil {
		return messages, err
	}

	if info, err := file.Stat(); err == nil {
		a.invalidateSessionMetaCacheIfChanged(path, info)
	}

	return messages, nil
}

// linkToolResults extracts tool_result blocks and links them to previously seen tool_use blocks.
func (a *Adapter) linkToolResults(rawContent json.RawMessage, messages []adapter.Message, refs map[string]toolUseRef) {
	if len(rawContent) == 0 {
		return
	}

	var blocks []ContentBlock
	if err := json.Unmarshal(rawContent, &blocks); err != nil {
		return
	}

	for _, block := range blocks {
		if block.Type != "tool_result" || block.ToolUseID == "" {
			continue
		}

		ref, ok := refs[block.ToolUseID]
		if !ok {
			continue
		}

		// Extract result content
		content := ""
		if s, ok := block.Content.(string); ok {
			content = s
		} else if block.Content != nil {
			if b, err := json.Marshal(block.Content); err == nil {
				content = string(b)
			}
		}

		// Update the tool use in the message
		if ref.toolIdx >= 0 && ref.toolIdx < len(messages[ref.msgIdx].ToolUses) {
			messages[ref.msgIdx].ToolUses[ref.toolIdx].Output = content
		}

		// Update the content block if tracked
		if ref.contentIdx >= 0 && ref.contentIdx < len(messages[ref.msgIdx].ContentBlocks) {
			messages[ref.msgIdx].ContentBlocks[ref.contentIdx].ToolOutput = content
			messages[ref.msgIdx].ContentBlocks[ref.contentIdx].IsError = block.IsError
		}
	}
}

// toolUseRef tracks location of a tool use for deferred result linking.
type toolUseRef struct {
	msgIdx     int
	toolIdx    int
	contentIdx int
}

// toolResultInfo holds parsed tool result data.
type toolResultInfo struct {
	content string
	isError bool
}

// Usage returns aggregate usage stats for the given session.
func (a *Adapter) Usage(sessionID string) (*adapter.UsageStats, error) {
	messages, err := a.Messages(sessionID)
	if err != nil {
		return nil, err
	}

	stats := &adapter.UsageStats{}
	for _, m := range messages {
		stats.TotalInputTokens += m.InputTokens
		stats.TotalOutputTokens += m.OutputTokens
		stats.TotalCacheRead += m.CacheRead
		stats.TotalCacheWrite += m.CacheWrite
		stats.MessageCount++
	}

	return stats, nil
}

// Watch returns a channel that emits events when session data changes.
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, error) {
	return NewWatcher(a.projectDirPath(projectRoot))
}

// projectDirPath converts a project root path to the Claude Code projects directory path.
// Claude Code uses the path with slashes replaced by dashes.
func (a *Adapter) projectDirPath(projectRoot string) string {
	// Ensure absolute path for consistent hashing
	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		absPath = projectRoot
	}
	// Convert /Users/foo/code/project to -Users-foo-code-project
	hash := strings.ReplaceAll(absPath, "/", "-")
	return filepath.Join(a.projectsDir, hash)
}

// sessionFilePath finds the JSONL file for a given session ID.
func (a *Adapter) sessionFilePath(sessionID string) string {
	// Check cache first
	a.mu.RLock()
	if path, ok := a.sessionIndex[sessionID]; ok {
		a.mu.RUnlock()
		return path
	}
	a.mu.RUnlock()

	// Fallback: scan all project directories
	entries, err := os.ReadDir(a.projectsDir)
	if err != nil {
		return ""
	}

	for _, projDir := range entries {
		if !projDir.IsDir() {
			continue
		}
		path := filepath.Join(a.projectsDir, projDir.Name(), sessionID+".jsonl")
		if _, err := os.Stat(path); err == nil {
			// Cache for future lookups
			a.mu.Lock()
			a.sessionIndex[sessionID] = path
			a.mu.Unlock()
			return path
		}
	}
	return ""
}

// parseSessionMetadata extracts metadata from a session file.
func (a *Adapter) parseSessionMetadata(path string) (*SessionMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	meta := &SessionMetadata{
		Path:      path,
		SessionID: strings.TrimSuffix(filepath.Base(path), ".jsonl"),
	}

	scanner := bufio.NewScanner(file)
	buf := getScannerBuffer()
	defer putScannerBuffer(buf)
	scanner.Buffer(buf, 10*1024*1024)

	modelCounts := make(map[string]int)
	modelTokens := make(map[string]struct{ in, out, cache int })

	for scanner.Scan() {
		var raw RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}

		// Skip non-message types
		if raw.Type != "user" && raw.Type != "assistant" {
			continue
		}

		if meta.FirstMsg.IsZero() {
			meta.FirstMsg = raw.Timestamp
			meta.CWD = raw.CWD
			meta.Version = raw.Version
			meta.GitBranch = raw.GitBranch
		}
		// Extract slug from first message that has it
		if meta.Slug == "" && raw.Slug != "" {
			meta.Slug = raw.Slug
		}
		// Extract first user message content for title
		if meta.FirstUserMessage == "" && raw.Type == "user" && raw.Message != nil {
			content, _, _ := a.parseContent(raw.Message.Content)
			if content != "" {
				meta.FirstUserMessage = content
			}
		}
		meta.LastMsg = raw.Timestamp
		meta.MsgCount++

		// Accumulate token usage from assistant messages
		if raw.Message != nil && raw.Message.Usage != nil {
			usage := raw.Message.Usage
			meta.TotalTokens += usage.InputTokens + usage.OutputTokens

			// Track per-model usage for cost calculation
			model := raw.Message.Model
			if model != "" {
				modelCounts[model]++
				mt := modelTokens[model]
				mt.in += usage.InputTokens
				mt.out += usage.OutputTokens
				mt.cache += usage.CacheReadInputTokens
				modelTokens[model] = mt
			}
		}
	}

	// Determine primary model and calculate cost
	var maxCount int
	for model, count := range modelCounts {
		if count > maxCount {
			maxCount = count
			meta.PrimaryModel = model
		}
	}

	// Calculate cost per model
	for model, mt := range modelTokens {
		var inRate, outRate float64
		switch {
		case strings.Contains(model, "opus"):
			inRate, outRate = 15.0, 75.0
		case strings.Contains(model, "sonnet"):
			inRate, outRate = 3.0, 15.0
		case strings.Contains(model, "haiku"):
			inRate, outRate = 0.25, 1.25
		default:
			inRate, outRate = 3.0, 15.0
		}
		regularIn := mt.in - mt.cache
		if regularIn < 0 {
			regularIn = 0
		}
		meta.EstCost += float64(mt.cache)*inRate*0.1/1_000_000 +
			float64(regularIn)*inRate/1_000_000 +
			float64(mt.out)*outRate/1_000_000
	}

	if meta.FirstMsg.IsZero() {
		meta.FirstMsg = time.Now()
		meta.LastMsg = time.Now()
	}

	return meta, nil
}

type sessionMetaCacheEntry struct {
	meta       *SessionMetadata
	modTime    time.Time
	size       int64
	lastAccess time.Time
}

func (a *Adapter) sessionMetadata(path string, info os.FileInfo) (*SessionMetadata, error) {
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

func (a *Adapter) pruneSessionMetaCache(dir string, seenPaths map[string]struct{}) {
	dir = filepath.Clean(dir)
	dirPrefix := dir + string(os.PathSeparator)

	a.metaMu.Lock()
	for path := range a.metaCache {
		if !strings.HasPrefix(path, dirPrefix) {
			continue
		}
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

// shortID returns the first 8 characters of an ID, or the full ID if shorter.
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

// collectToolResults extracts tool_result content from user messages.
func (a *Adapter) collectToolResults(rawContent json.RawMessage, results map[string]toolResultInfo) {
	if len(rawContent) == 0 {
		return
	}

	var blocks []ContentBlock
	if err := json.Unmarshal(rawContent, &blocks); err != nil {
		return
	}

	for _, block := range blocks {
		if block.Type == "tool_result" && block.ToolUseID != "" {
			content := ""
			if s, ok := block.Content.(string); ok {
				content = s
			} else if block.Content != nil {
				if b, err := json.Marshal(block.Content); err == nil {
					content = string(b)
				}
			}
			results[block.ToolUseID] = toolResultInfo{
				content: content,
				isError: block.IsError,
			}
		}
	}
}

// parseContent extracts text content, tool uses, and thinking blocks from the content field.
// This is a simplified version for metadata parsing that doesn't need ContentBlocks.
func (a *Adapter) parseContent(rawContent json.RawMessage) (string, []adapter.ToolUse, []adapter.ThinkingBlock) {
	content, toolUses, thinkingBlocks, _ := a.parseContentWithResults(rawContent, nil)
	return content, toolUses, thinkingBlocks
}

// parseContentWithResults extracts content and builds ContentBlocks with linked tool results.
func (a *Adapter) parseContentWithResults(rawContent json.RawMessage, toolResults map[string]toolResultInfo) (string, []adapter.ToolUse, []adapter.ThinkingBlock, []adapter.ContentBlock) {
	if len(rawContent) == 0 {
		return "", nil, nil, nil
	}

	// Try parsing as string first
	var strContent string
	if err := json.Unmarshal(rawContent, &strContent); err == nil {
		contentBlocks := []adapter.ContentBlock{{Type: "text", Text: strContent}}
		return strContent, nil, nil, contentBlocks
	}

	// Parse as array of content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(rawContent, &blocks); err != nil {
		return "", nil, nil, nil
	}

	texts := make([]string, 0, len(blocks))
	toolUses := make([]adapter.ToolUse, 0, len(blocks))
	thinkingBlocks := make([]adapter.ThinkingBlock, 0, len(blocks))
	contentBlocks := make([]adapter.ContentBlock, 0, len(blocks))
	toolResultCount := 0

	for _, block := range blocks {
		switch block.Type {
		case "text":
			texts = append(texts, block.Text)
			contentBlocks = append(contentBlocks, adapter.ContentBlock{
				Type: "text",
				Text: block.Text,
			})
		case "thinking":
			tokenCount := len(block.Thinking) / 4
			thinkingBlocks = append(thinkingBlocks, adapter.ThinkingBlock{
				Content:    block.Thinking,
				TokenCount: tokenCount,
			})
			contentBlocks = append(contentBlocks, adapter.ContentBlock{
				Type:       "thinking",
				Text:       block.Thinking,
				TokenCount: tokenCount,
			})
		case "tool_use":
			inputStr := ""
			if block.Input != nil {
				if b, err := json.Marshal(block.Input); err == nil {
					inputStr = string(b)
				}
			}
			// Lookup tool result by ID
			var output string
			var isError bool
			if toolResults != nil {
				if result, ok := toolResults[block.ID]; ok {
					output = result.content
					isError = result.isError
				}
			}
			toolUses = append(toolUses, adapter.ToolUse{
				ID:     block.ID,
				Name:   block.Name,
				Input:  inputStr,
				Output: output,
			})
			contentBlocks = append(contentBlocks, adapter.ContentBlock{
				Type:       "tool_use",
				ToolUseID:  block.ID,
				ToolName:   block.Name,
				ToolInput:  inputStr,
				ToolOutput: output,
				IsError:    isError,
			})
		case "tool_result":
			toolResultCount++
			// Add tool_result to content blocks for user messages
			content := ""
			if s, ok := block.Content.(string); ok {
				content = s
			} else if block.Content != nil {
				if b, err := json.Marshal(block.Content); err == nil {
					content = string(b)
				}
			}
			contentBlocks = append(contentBlocks, adapter.ContentBlock{
				Type:       "tool_result",
				ToolUseID:  block.ToolUseID,
				ToolOutput: content,
				IsError:    block.IsError,
			})
		}
	}

	// If we have tool results but no text, show a placeholder
	content := strings.Join(texts, "\n")
	if content == "" && toolResultCount > 0 {
		content = fmt.Sprintf("[%d tool result(s)]", toolResultCount)
	}

	return content, toolUses, thinkingBlocks, contentBlocks
}
