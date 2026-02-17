package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/adapter/cache"
)

const (
	adapterID           = "codex"
	adapterName         = "Codex"
	metaCacheMaxEntries = 2048
	msgCacheMaxEntries  = 128                    // fewer entries since messages are larger
	dirCacheTTL         = 500 * time.Millisecond // TTL for directory listing cache (td-c9ff3aac)
	// Two-pass parsing thresholds (td-a2c1dd41)
	metaParseSmallFileThreshold = 16 * 1024 // Files smaller than 16KB use full scan
	metaParseHeadLines          = 100       // Read first N lines for session_meta
	metaParseTailSize           = 8 * 1024  // Read last N bytes for recent tokens
)

// dirCacheEntry caches the directory listing with expiration (td-c9ff3aac).
type dirCacheEntry struct {
	files     []sessionFileEntry
	expiresAt time.Time
}

// Adapter implements the adapter.Adapter interface for Codex CLI sessions.
type Adapter struct {
	sessionsDir     string
	sessionIndex    map[string]string      // sessionID -> file path cache
	totalUsageCache map[string]*TokenUsage // sessionID -> total usage (populated by Messages)
	mu              sync.RWMutex           // guards sessionIndex and totalUsageCache
	metaCache       map[string]sessionMetaCacheEntry
	metaMu          sync.RWMutex                    // guards metaCache
	msgCache        *cache.Cache[messageCacheEntry] // path -> cached messages
	dirCache        *dirCacheEntry
	dirCacheMu      sync.RWMutex // guards dirCache
}

// messageCacheEntry holds cached messages with incremental parsing state.
type messageCacheEntry struct {
	messages        []adapter.Message
	pendingTools    []adapter.ToolUse
	toolIndex       map[string]int
	pendingThinking []adapter.ThinkingBlock
	pendingUsage    *adapter.TokenUsage
	currentModel    string
	totalUsage      *TokenUsage
	lastTimestamp   time.Time
	byteOffset      int64
}

// New creates a new Codex adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		sessionsDir:     filepath.Join(home, ".codex", "sessions"),
		sessionIndex:    make(map[string]string),
		totalUsageCache: make(map[string]*TokenUsage),
		metaCache:       make(map[string]sessionMetaCacheEntry),
		msgCache:        cache.New[messageCacheEntry](msgCacheMaxEntries),
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
	for _, f := range files {
		meta, err := a.sessionMetadata(f.path, f.info)
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

	// Pre-resolve projectRoot once (td-6543fee4)
	resolvedProject := newResolvedProjectPath(projectRoot)

	sessions := make([]adapter.Session, 0, len(files))
	seenPaths := make(map[string]struct{}, len(files))
	// Build new index, then swap atomically to avoid race with sessionFilePath()
	newIndex := make(map[string]string, len(files))
	for _, f := range files {
		seenPaths[f.path] = struct{}{}
		meta, err := a.sessionMetadata(f.path, f.info)
		if err != nil {
			continue
		}
		if !resolvedProject.matchesCWD(meta.CWD) {
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
			FileSize:     f.info.Size(),
			Path:         f.path, // td-dca6fe: tiered watching needs session file path
		})

		// Add to new index (will be swapped atomically after loop)
		newIndex[meta.SessionID] = f.path
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
// Uses caching with incremental parsing for append-only growth optimization.
func (a *Adapter) Messages(sessionID string) ([]adapter.Message, error) {
	path := a.sessionFilePath(sessionID)
	if path == "" {
		return nil, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	// Check cache for existing entry (if cache is initialized)
	if a.msgCache != nil {
		cached, offset, cachedSize, cachedModTime, ok := a.msgCache.GetWithOffset(path)
		if ok {
			// Exact cache hit: file unchanged
			if info.Size() == cachedSize && info.ModTime().Equal(cachedModTime) {
				return copyMessages(cached.messages), nil
			}

			// File grew: incremental parse from saved offset
			if info.Size() > cachedSize && offset > 0 {
				messages, entry, err := a.parseMessagesIncremental(path, sessionID, cached, offset, info)
				if err == nil {
					a.msgCache.Set(path, entry, info.Size(), info.ModTime(), entry.byteOffset)
					// Update total usage cache
					if entry.totalUsage != nil {
						a.mu.Lock()
						a.totalUsageCache[sessionID] = entry.totalUsage
						a.mu.Unlock()
					}
					return messages, nil
				}
				// Fall through to full parse on error
			}
			// File shrank or other change: full re-parse
		}
	}

	// Full parse
	messages, entry, err := a.parseMessagesFull(path, sessionID, info)
	if err != nil {
		return nil, err
	}

	if a.msgCache != nil {
		a.msgCache.Set(path, entry, info.Size(), info.ModTime(), entry.byteOffset)
	}

	// Update total usage cache
	if entry.totalUsage != nil {
		a.mu.Lock()
		a.totalUsageCache[sessionID] = entry.totalUsage
		a.mu.Unlock()
	}

	return messages, nil
}

// parseMessagesFull parses all messages from a session file.
func (a *Adapter) parseMessagesFull(path, sessionID string, info os.FileInfo) ([]adapter.Message, messageCacheEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, messageCacheEntry{}, err
	}
	defer func() {
		_ = file.Close()
	}()

	state := newParseState(sessionID)
	var bytesRead int64

	scanner := bufio.NewScanner(file)
	buf := cache.GetScannerBuffer()
	defer cache.PutScannerBuffer(buf)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesRead += int64(len(line)) + 1
		a.processMessageRecord(line, state)
	}

	if err := scanner.Err(); err != nil {
		return nil, messageCacheEntry{}, err
	}

	a.invalidateSessionMetaCacheIfChanged(path, info)

	// Flush any remaining pending state
	state.flushPending()

	entry := messageCacheEntry{
		messages:        copyMessages(state.messages),
		pendingTools:    copyToolUses(state.pendingTools),
		toolIndex:       copyToolIndex(state.toolIndex),
		pendingThinking: copyThinkingBlocks(state.pendingThinking),
		pendingUsage:    state.pendingUsage,
		currentModel:    state.currentModel,
		totalUsage:      state.totalUsage,
		lastTimestamp:   state.lastTimestamp,
		byteOffset:      bytesRead,
	}

	return state.messages, entry, nil
}

// parseMessagesIncremental resumes parsing from a byte offset.
func (a *Adapter) parseMessagesIncremental(path, sessionID string, cached messageCacheEntry, startOffset int64, info os.FileInfo) ([]adapter.Message, messageCacheEntry, error) {
	reader, err := cache.NewIncrementalReader(path, startOffset)
	if err != nil {
		return nil, messageCacheEntry{}, err
	}
	defer func() {
		_ = reader.Close()
	}()

	// Restore state from cached entry
	state := &parseState{
		sessionID:       sessionID,
		messages:        copyMessages(cached.messages),
		pendingTools:    copyToolUses(cached.pendingTools),
		toolIndex:       copyToolIndex(cached.toolIndex),
		pendingThinking: copyThinkingBlocks(cached.pendingThinking),
		pendingUsage:    cached.pendingUsage,
		currentModel:    cached.currentModel,
		totalUsage:      cached.totalUsage,
		lastTimestamp:   cached.lastTimestamp,
	}

	for {
		line, err := reader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, messageCacheEntry{}, err
		}
		a.processMessageRecord(line, state)
	}

	a.invalidateSessionMetaCacheIfChanged(path, info)

	// Flush any remaining pending state
	state.flushPending()

	entry := messageCacheEntry{
		messages:        copyMessages(state.messages),
		pendingTools:    copyToolUses(state.pendingTools),
		toolIndex:       copyToolIndex(state.toolIndex),
		pendingThinking: copyThinkingBlocks(state.pendingThinking),
		pendingUsage:    state.pendingUsage,
		currentModel:    state.currentModel,
		totalUsage:      state.totalUsage,
		lastTimestamp:   state.lastTimestamp,
		byteOffset:      reader.Offset(),
	}

	return state.messages, entry, nil
}

// parseState holds mutable state during message parsing.
type parseState struct {
	sessionID       string
	messages        []adapter.Message
	pendingTools    []adapter.ToolUse
	toolIndex       map[string]int
	pendingThinking []adapter.ThinkingBlock
	pendingUsage    *adapter.TokenUsage
	totalUsage      *TokenUsage
	currentModel    string
	lastTimestamp   time.Time
}

func newParseState(sessionID string) *parseState {
	return &parseState{
		sessionID: sessionID,
		toolIndex: make(map[string]int),
	}
}

// flushPending creates a synthetic message for any remaining pending tools/thinking.
func (s *parseState) flushPending() {
	if len(s.pendingTools) == 0 && len(s.pendingThinking) == 0 {
		return
	}
	msg := adapter.Message{
		ID:             "synthetic-" + shortID(s.sessionID) + "-" + fmt.Sprintf("%d", len(s.messages)),
		Role:           "assistant",
		Content:        "tool calls",
		Timestamp:      s.lastTimestamp,
		Model:          s.currentModel,
		ToolUses:       append([]adapter.ToolUse(nil), s.pendingTools...),
		ThinkingBlocks: append([]adapter.ThinkingBlock(nil), s.pendingThinking...),
	}
	if s.pendingUsage != nil {
		msg.TokenUsage = *s.pendingUsage
		s.pendingUsage = nil
	}
	s.messages = append(s.messages, msg)
	s.pendingTools = nil
	s.pendingThinking = nil
	s.toolIndex = make(map[string]int)
}

// processMessageRecord parses a single JSONL record and updates parse state.
func (a *Adapter) processMessageRecord(line []byte, state *parseState) {
	var record RawRecord
	if err := json.Unmarshal(line, &record); err != nil {
		return
	}
	if !record.Timestamp.IsZero() {
		state.lastTimestamp = record.Timestamp
	}

	switch record.Type {
	case "turn_context":
		var payload TurnContextPayload
		if err := json.Unmarshal(record.Payload, &payload); err == nil && payload.Model != "" {
			state.currentModel = payload.Model
		}

	case "response_item":
		var base ResponseItemBase
		if err := json.Unmarshal(record.Payload, &base); err != nil {
			return
		}
		switch base.Type {
		case "message":
			var msg ResponseMessagePayload
			if err := json.Unmarshal(record.Payload, &msg); err != nil {
				return
			}
			if msg.Role != "user" && msg.Role != "assistant" {
				return
			}
			if msg.Role == "user" {
				state.flushPending()
			}

			content := contentFromBlocks(msg.Content)
			message := adapter.Message{
				ID:        fmt.Sprintf("%s-%d", state.sessionID, len(state.messages)),
				Role:      msg.Role,
				Content:   content,
				Timestamp: record.Timestamp,
				Model:     state.currentModel,
			}
			if msg.Role == "assistant" {
				message.ToolUses = append(message.ToolUses, state.pendingTools...)
				message.ThinkingBlocks = append(message.ThinkingBlocks, state.pendingThinking...)
				state.pendingTools = nil
				state.pendingThinking = nil
				state.toolIndex = make(map[string]int)
				if state.pendingUsage != nil {
					message.TokenUsage = *state.pendingUsage
					state.pendingUsage = nil
				}
			}
			state.messages = append(state.messages, message)

		case "function_call", "custom_tool_call":
			var call ResponseToolCallPayload
			if err := json.Unmarshal(record.Payload, &call); err != nil {
				return
			}
			input := toolInputString(call.Arguments, call.Input)
			tool := adapter.ToolUse{
				ID:    call.CallID,
				Name:  call.Name,
				Input: input,
			}
			state.toolIndex[call.CallID] = len(state.pendingTools)
			state.pendingTools = append(state.pendingTools, tool)

		case "function_call_output", "custom_tool_call_output":
			var output ResponseToolOutputPayload
			if err := json.Unmarshal(record.Payload, &output); err != nil {
				return
			}
			out := toolOutputString(output.Output)
			if idx, ok := state.toolIndex[output.CallID]; ok && idx < len(state.pendingTools) {
				state.pendingTools[idx].Output = out
			} else {
				state.toolIndex[output.CallID] = len(state.pendingTools)
				state.pendingTools = append(state.pendingTools, adapter.ToolUse{
					ID:     output.CallID,
					Output: out,
				})
			}

		case "reasoning":
			var reason ResponseReasoningPayload
			if err := json.Unmarshal(record.Payload, &reason); err != nil {
				return
			}
			for _, summary := range reason.Summary {
				if strings.TrimSpace(summary.Text) == "" {
					continue
				}
				state.pendingThinking = append(state.pendingThinking, adapter.ThinkingBlock{
					Content:    summary.Text,
					TokenCount: len(summary.Text) / 4,
				})
			}
		}

	case "event_msg":
		var event EventMsgPayload
		if err := json.Unmarshal(record.Payload, &event); err != nil {
			return
		}
		switch event.Type {
		case "agent_reasoning":
			if strings.TrimSpace(event.Text) != "" {
				state.pendingThinking = append(state.pendingThinking, adapter.ThinkingBlock{
					Content:    event.Text,
					TokenCount: len(event.Text) / 4,
				})
			}
		case "token_count":
			if event.Info == nil {
				return
			}
			if event.Info.LastTokenUsage != nil {
				state.pendingUsage = convertUsage(event.Info.LastTokenUsage)
			}
			if event.Info.TotalTokenUsage != nil {
				state.totalUsage = event.Info.TotalTokenUsage
			}
		}
	}
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
		if m.ThinkingBlocks != nil {
			cp[i].ThinkingBlocks = make([]adapter.ThinkingBlock, len(m.ThinkingBlocks))
			copy(cp[i].ThinkingBlocks, m.ThinkingBlocks)
		}
		if m.ContentBlocks != nil {
			cp[i].ContentBlocks = make([]adapter.ContentBlock, len(m.ContentBlocks))
			copy(cp[i].ContentBlocks, m.ContentBlocks)
		}
	}
	return cp
}

// copyToolUses creates a copy of tool uses slice.
func copyToolUses(tools []adapter.ToolUse) []adapter.ToolUse {
	if tools == nil {
		return nil
	}
	cp := make([]adapter.ToolUse, len(tools))
	copy(cp, tools)
	return cp
}

// copyThinkingBlocks creates a copy of thinking blocks slice.
func copyThinkingBlocks(blocks []adapter.ThinkingBlock) []adapter.ThinkingBlock {
	if blocks == nil {
		return nil
	}
	cp := make([]adapter.ThinkingBlock, len(blocks))
	copy(cp, blocks)
	return cp
}

// copyToolIndex creates a copy of tool index map.
func copyToolIndex(idx map[string]int) map[string]int {
	if idx == nil {
		return make(map[string]int)
	}
	cp := make(map[string]int, len(idx))
	for k, v := range idx {
		cp[k] = v
	}
	return cp
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
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, io.Closer, error) {
	return NewWatcher(a.sessionsDir)
}

// WatchScope returns Global because codex watches a global sessions directory (td-7a72b6f7).
func (a *Adapter) WatchScope() adapter.WatchScope {
	return adapter.WatchScopeGlobal
}

// sessionFileEntry holds a session file path with its FileInfo (td-5a1e8104).
type sessionFileEntry struct {
	path string
	info os.FileInfo
}

func (a *Adapter) sessionFiles() ([]sessionFileEntry, error) {
	// Check cache first (td-c9ff3aac)
	a.dirCacheMu.RLock()
	if a.dirCache != nil && time.Now().Before(a.dirCache.expiresAt) {
		files := a.dirCache.files
		a.dirCacheMu.RUnlock()
		return files, nil
	}
	a.dirCacheMu.RUnlock()

	if _, err := os.Stat(a.sessionsDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []sessionFileEntry
	err := filepath.WalkDir(a.sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".jsonl") {
			// Get FileInfo from DirEntry to avoid duplicate stat (td-5a1e8104)
			info, err := d.Info()
			if err != nil {
				return nil
			}
			files = append(files, sessionFileEntry{path: path, info: info})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Cache the result (td-c9ff3aac)
	a.dirCacheMu.Lock()
	a.dirCache = &dirCacheEntry{
		files:     files,
		expiresAt: time.Now().Add(dirCacheTTL),
	}
	a.dirCacheMu.Unlock()

	return files, nil
}

type sessionMetaCacheEntry struct {
	meta       *SessionMetadata
	modTime    time.Time
	size       int64
	lastAccess time.Time
	headParsed bool // true = head data is immutable, can skip Pass 1 on growth (td-56c153)
}

// sessionMetadata returns cached metadata if valid, otherwise parses the file.
// Supports tail-only re-parse when a file grows and head data was previously parsed (td-56c153).
// Accepts FileInfo to avoid duplicate stat calls (td-5a1e8104).
// Uses write lock for cache hits to safely update lastAccess (td-02e326f7).
func (a *Adapter) sessionMetadata(path string, info os.FileInfo) (*SessionMetadata, error) {
	now := time.Now()

	a.metaMu.Lock()
	entry, cached := a.metaCache[path]
	if cached {
		if entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
			// Exact cache hit (unchanged file)
			entry.lastAccess = now
			a.metaCache[path] = entry
			metaCopy := *entry.meta
			a.metaMu.Unlock()
			return &metaCopy, nil
		}
		if entry.headParsed && info.Size() > entry.size {
			// File grew - only re-read tail, reuse head data (td-56c153)
			a.metaMu.Unlock()
			meta, err := a.parseSessionMetadataTailOnly(path, entry.meta, info.Size())
			if err == nil {
				a.metaMu.Lock()
				a.metaCache[path] = sessionMetaCacheEntry{
					meta:       meta,
					modTime:    info.ModTime(),
					size:       info.Size(),
					lastAccess: now,
					headParsed: true,
				}
				a.enforceSessionMetaCacheLimitLocked()
				a.metaMu.Unlock()
				return meta, nil
			}
			// Fall through to full parse on error
		} else {
			a.metaMu.Unlock()
		}
	} else {
		a.metaMu.Unlock()
	}

	meta, err := a.parseSessionMetadata(path)
	if err != nil {
		return nil, err
	}

	// Mark headParsed for files large enough to use two-pass
	isLargeFile := info.Size() >= metaParseSmallFileThreshold
	a.metaMu.Lock()
	a.metaCache[path] = sessionMetaCacheEntry{
		meta:       meta,
		modTime:    info.ModTime(),
		size:       info.Size(),
		lastAccess: now,
		headParsed: isLargeFile,
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
	excess := len(a.metaCache) - metaCacheMaxEntries
	if excess <= 0 {
		return
	}

	// Collect entries for sorting
	type pathAccess struct {
		path       string
		lastAccess time.Time
	}
	entries := make([]pathAccess, 0, len(a.metaCache))
	for path, entry := range a.metaCache {
		entries = append(entries, pathAccess{path, entry.lastAccess})
	}

	// Sort by lastAccess (oldest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastAccess.Before(entries[j].lastAccess)
	})

	// Delete oldest entries
	for i := 0; i < excess; i++ {
		delete(a.metaCache, entries[i].path)
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

// parseSessionMetadata extracts metadata using two-pass parsing for large files (td-a2c1dd41).
func (a *Adapter) parseSessionMetadata(path string) (*SessionMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	// For small files, use full scan
	if stat.Size() < metaParseSmallFileThreshold {
		return a.parseSessionMetadataFull(file, path)
	}

	// Two-pass approach for large files
	return a.parseSessionMetadataTwoPass(file, path, stat.Size())
}

// parseSessionMetadataFull scans the entire file (for small files).
func (a *Adapter) parseSessionMetadataFull(file *os.File, path string) (*SessionMetadata, error) {
	meta := &SessionMetadata{Path: path}
	var sessionTimestamp time.Time
	var lastRecord time.Time
	var totalTokens int

	scanner := bufio.NewScanner(file)
	buf := cache.GetScannerBuffer()
	defer cache.PutScannerBuffer(buf)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		a.processMetadataRecord(scanner.Bytes(), meta, &sessionTimestamp, &lastRecord, &totalTokens)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	a.finalizeMetadata(meta, path, sessionTimestamp, lastRecord, totalTokens)
	return meta, nil
}

// parseSessionMetadataTwoPass reads head and tail of large files (td-a2c1dd41).
func (a *Adapter) parseSessionMetadataTwoPass(file *os.File, path string, fileSize int64) (*SessionMetadata, error) {
	meta := &SessionMetadata{Path: path}
	var sessionTimestamp time.Time
	var lastRecord time.Time
	var totalTokens int

	buf := cache.GetScannerBuffer()
	defer cache.PutScannerBuffer(buf)

	// Pass 1: Read first N lines for session_meta, FirstMsg, FirstUserMessage
	scanner := bufio.NewScanner(file)
	scanner.Buffer(buf, 10*1024*1024)

	headComplete := false
	for i := 0; i < metaParseHeadLines && scanner.Scan(); i++ {
		a.processMetadataRecord(scanner.Bytes(), meta, &sessionTimestamp, &lastRecord, &totalTokens)
		// Stop early if we have all head data
		if meta.SessionID != "" && meta.CWD != "" && !meta.FirstMsg.IsZero() && meta.FirstUserMessage != "" {
			headComplete = true
			break
		}
	}

	// Pass 2: Seek to tail and read last portion for LastMsg, TotalTokens
	tailOffset := fileSize - metaParseTailSize
	if tailOffset > 0 {
		if _, err := file.Seek(tailOffset, io.SeekStart); err != nil {
			// If seek fails, use what we have
			a.finalizeMetadata(meta, path, sessionTimestamp, lastRecord, totalTokens)
			return meta, nil
		}

		scanner = bufio.NewScanner(file)
		scanner.Buffer(buf, 10*1024*1024)

		// Skip first partial line after seek
		scanner.Scan()

		// Read remaining lines in tail
		for scanner.Scan() {
			a.processMetadataRecord(scanner.Bytes(), meta, &sessionTimestamp, &lastRecord, &totalTokens)
		}
	} else if !headComplete {
		// File smaller than tail size but we stopped head scan early - continue
		for scanner.Scan() {
			a.processMetadataRecord(scanner.Bytes(), meta, &sessionTimestamp, &lastRecord, &totalTokens)
		}
	}

	a.finalizeMetadata(meta, path, sessionTimestamp, lastRecord, totalTokens)
	return meta, nil
}

// parseSessionMetadataTailOnly re-reads only the tail of a file, reusing cached head data (td-56c153).
// Skips Pass 1 entirely since head fields (SessionID, CWD, FirstMsg, FirstUserMessage) are immutable.
func (a *Adapter) parseSessionMetadataTailOnly(path string, headMeta *SessionMetadata, fileSize int64) (*SessionMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	// Copy immutable head fields
	meta := &SessionMetadata{
		Path:             headMeta.Path,
		SessionID:        headMeta.SessionID,
		CWD:              headMeta.CWD,
		FirstMsg:         headMeta.FirstMsg,
		FirstUserMessage: headMeta.FirstUserMessage,
	}

	var sessionTimestamp time.Time
	var lastRecord time.Time
	var totalTokens int

	// Only read tail portion
	tailOffset := fileSize - metaParseTailSize
	if tailOffset > 0 {
		if _, err := file.Seek(tailOffset, io.SeekStart); err != nil {
			return nil, err
		}

		buf := cache.GetScannerBuffer()
		defer cache.PutScannerBuffer(buf)

		scanner := bufio.NewScanner(file)
		scanner.Buffer(buf, 10*1024*1024)

		// Skip first partial line after seek
		scanner.Scan()

		for scanner.Scan() {
			a.processMetadataRecord(scanner.Bytes(), meta, &sessionTimestamp, &lastRecord, &totalTokens)
		}
	} else {
		// File smaller than tail size - shouldn't happen since headParsed is only set for large files
		return nil, fmt.Errorf("file too small for tail-only parse")
	}

	a.finalizeMetadata(meta, path, sessionTimestamp, lastRecord, totalTokens)
	return meta, nil
}

// processMetadataRecord parses a single JSONL record and updates metadata.
func (a *Adapter) processMetadataRecord(line []byte, meta *SessionMetadata, sessionTimestamp, lastRecord *time.Time, totalTokens *int) {
	var record RawRecord
	if err := json.Unmarshal(line, &record); err != nil {
		return
	}

	if !record.Timestamp.IsZero() {
		*lastRecord = record.Timestamp
	}

	switch record.Type {
	case "session_meta":
		var payload SessionMetaPayload
		if err := json.Unmarshal(record.Payload, &payload); err != nil {
			return
		}
		if meta.SessionID == "" {
			meta.SessionID = payload.ID
		}
		if meta.CWD == "" {
			meta.CWD = payload.CWD
		}
		if sessionTimestamp.IsZero() && !payload.Timestamp.IsZero() {
			*sessionTimestamp = payload.Timestamp
		}

	case "response_item":
		var base ResponseItemBase
		if err := json.Unmarshal(record.Payload, &base); err != nil {
			return
		}
		if base.Type != "message" {
			return
		}
		var msg ResponseMessagePayload
		if err := json.Unmarshal(record.Payload, &msg); err != nil {
			return
		}
		if msg.Role != "user" && msg.Role != "assistant" {
			return
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
			return
		}
		if event.Type != "token_count" || event.Info == nil {
			return
		}
		usage := event.Info.TotalTokenUsage
		if usage == nil {
			usage = event.Info.LastTokenUsage
		}
		if usage != nil {
			*totalTokens = usage.TotalTokens
			if *totalTokens == 0 {
				*totalTokens = usage.InputTokens + usage.OutputTokens + usage.ReasoningOutputTokens
			}
		}
	}
}

// finalizeMetadata sets default values for missing metadata fields.
func (a *Adapter) finalizeMetadata(meta *SessionMetadata, path string, sessionTimestamp, lastRecord time.Time, totalTokens int) {
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
	for _, f := range files {
		if strings.Contains(filepath.Base(f.path), sessionID) {
			return f.path
		}
	}

	for _, f := range files {
		meta, err := a.sessionMetadata(f.path, f.info)
		if err != nil {
			continue
		}
		if meta.SessionID == sessionID {
			return f.path
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

// resolvedProjectPath holds a pre-resolved project path for efficient matching.
type resolvedProjectPath struct {
	abs string
}

// newResolvedProjectPath creates a resolved path from projectRoot, performing
// expensive filepath.Abs and filepath.EvalSymlinks calls once.
func newResolvedProjectPath(projectRoot string) *resolvedProjectPath {
	if projectRoot == "" {
		return nil
	}
	projectAbs, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(projectAbs); err == nil {
		projectAbs = resolved
	}
	return &resolvedProjectPath{abs: filepath.Clean(projectAbs)}
}

// matchesCWD checks if a session CWD matches this project path.
func (r *resolvedProjectPath) matchesCWD(cwd string) bool {
	if r == nil || cwd == "" {
		return false
	}
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(cwdAbs); err == nil {
		cwdAbs = resolved
	}
	cwdAbs = filepath.Clean(cwdAbs)

	rel, err := filepath.Rel(r.abs, cwdAbs)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..")
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
