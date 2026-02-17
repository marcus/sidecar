package amp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/adapter/cache"
)

const (
	adapterID           = "amp"
	adapterName         = "Amp"
	metaCacheMaxEntries = 2048
	msgCacheMaxEntries  = 128
)

// Adapter implements the adapter.Adapter interface for Amp Code threads.
type Adapter struct {
	threadsDir   string
	sessionIndex map[string]string // threadID -> file path
	mu           sync.RWMutex      // guards sessionIndex
	metaCache    map[string]metaCacheEntry
	metaMu       sync.RWMutex // guards metaCache
	msgCache     *cache.Cache[msgCacheEntry]
}

// metaCacheEntry caches parsed thread metadata with validation info.
type metaCacheEntry struct {
	meta       *threadMeta
	modTime    time.Time
	size       int64
	lastAccess time.Time
}

// msgCacheEntry holds cached messages for a thread.
type msgCacheEntry struct {
	messages []adapter.Message
}

// New creates a new Amp adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	threadsDir := findAmpThreadsDir(home)
	return &Adapter{
		threadsDir:   threadsDir,
		sessionIndex: make(map[string]string),
		metaCache:    make(map[string]metaCacheEntry),
		msgCache:     cache.New[msgCacheEntry](msgCacheMaxEntries),
	}
}

// findAmpThreadsDir searches candidate paths for the Amp threads directory.
// Returns the first path that exists, or the primary default if none found.
func findAmpThreadsDir(home string) string {
	candidates := ampThreadsDirCandidates(home)
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return filepath.Join(home, ".local", "share", "amp", "threads")
}

// ampThreadsDirCandidates returns candidate paths for the Amp threads directory.
// Follows Amp's path resolution: AMP_DATA_HOME > XDG_DATA_HOME (Linux) > ~/.local/share.
func ampThreadsDirCandidates(home string) []string {
	var candidates []string

	// 1. AMP_DATA_HOME override (all platforms)
	if ampHome := os.Getenv("AMP_DATA_HOME"); ampHome != "" {
		candidates = append(candidates, filepath.Join(ampHome, "amp", "threads"))
	}

	// 2. XDG_DATA_HOME (Linux only)
	if runtime.GOOS == "linux" {
		if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
			candidates = append(candidates, filepath.Join(xdgData, "amp", "threads"))
		}
	}

	// 3. Default: ~/.local/share/amp/threads (all platforms)
	candidates = append(candidates, filepath.Join(home, ".local", "share", "amp", "threads"))

	return candidates
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return adapterID }

// Name returns the human-readable adapter name.
func (a *Adapter) Name() string { return adapterName }

// Icon returns the adapter icon for badge display.
func (a *Adapter) Icon() string { return "\u26a1" }

// Detect checks if Amp threads exist for the given project.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	entries, err := os.ReadDir(a.threadsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, nil
	}

	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return false, nil
	}
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}
	absRoot = filepath.Clean(absRoot)

	for _, e := range entries {
		if !isThreadFile(e.Name()) {
			continue
		}

		path := filepath.Join(a.threadsDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}

		meta, err := a.threadMetadata(path, info)
		if err != nil {
			continue
		}

		// Check project match by reading the full thread
		if a.threadMatchesProject(path, absRoot) {
			_ = meta // valid thread for this project
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
	entries, err := os.ReadDir(a.threadsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}
	absRoot = filepath.Clean(absRoot)

	sessions := make([]adapter.Session, 0, len(entries))
	seenPaths := make(map[string]struct{}, len(entries))
	newIndex := make(map[string]string, len(entries))

	for _, e := range entries {
		if !isThreadFile(e.Name()) {
			continue
		}

		path := filepath.Join(a.threadsDir, e.Name())
		seenPaths[path] = struct{}{}

		info, err := e.Info()
		if err != nil {
			continue
		}

		if !a.threadMatchesProject(path, absRoot) {
			continue
		}

		meta, err := a.threadMetadata(path, info)
		if err != nil {
			continue
		}

		if meta.MsgCount == 0 {
			continue
		}

		name := meta.FirstUserMessage
		if name != "" {
			name = truncateTitle(name, 50)
		}
		if name == "" {
			name = shortID(meta.ThreadID)
		}

		newIndex[meta.ThreadID] = path

		sessions = append(sessions, adapter.Session{
			ID:           meta.ThreadID,
			Name:         name,
			AdapterID:    adapterID,
			AdapterName:  adapterName,
			AdapterIcon:  a.Icon(),
			CreatedAt:    meta.CreatedAt,
			UpdatedAt:    meta.UpdatedAt,
			Duration:     meta.UpdatedAt.Sub(meta.CreatedAt),
			IsActive:     time.Since(meta.UpdatedAt) < 5*time.Minute,
			TotalTokens:  meta.TotalTokens,
			MessageCount: meta.MsgCount,
			FileSize:     info.Size(),
			Path:         path,
		})
	}

	// Atomically swap the index
	a.mu.Lock()
	a.sessionIndex = newIndex
	a.mu.Unlock()

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	a.pruneMetaCache(seenPaths)

	return sessions, nil
}

// Messages returns all messages for the given session.
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

	// Check cache
	if a.msgCache != nil {
		cached, ok := a.msgCache.Get(path, info.Size(), info.ModTime())
		if ok {
			return copyMessages(cached.messages), nil
		}
	}

	// Full parse
	messages, err := a.parseMessages(path)
	if err != nil {
		return nil, err
	}

	if a.msgCache != nil {
		a.msgCache.Set(path, msgCacheEntry{messages: copyMessages(messages)}, info.Size(), info.ModTime(), 0)
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
	for _, m := range messages {
		stats.TotalInputTokens += m.InputTokens
		stats.TotalOutputTokens += m.OutputTokens
		stats.TotalCacheRead += m.CacheRead
		stats.TotalCacheWrite += m.CacheWrite
		stats.MessageCount++
	}

	return stats, nil
}

// Watch returns a channel that emits events when thread data changes.
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, io.Closer, error) {
	return NewWatcher(a.threadsDir)
}

// WatchScope returns Global because amp watches a global threads directory.
func (a *Adapter) WatchScope() adapter.WatchScope {
	return adapter.WatchScopeGlobal
}

// SessionByID returns a single session by ID without scanning the directory.
// Implements adapter.TargetedRefresher.
func (a *Adapter) SessionByID(sessionID string) (*adapter.Session, error) {
	path := a.sessionFilePath(sessionID)
	if path == "" {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	meta, err := a.threadMetadata(path, info)
	if err != nil {
		return nil, err
	}

	if meta.MsgCount == 0 {
		return nil, fmt.Errorf("session %s has no messages", sessionID)
	}

	name := meta.FirstUserMessage
	if name != "" {
		name = truncateTitle(name, 120)
	}
	if name == "" {
		name = shortID(meta.ThreadID)
	}

	return &adapter.Session{
		ID:           meta.ThreadID,
		Name:         name,
		AdapterID:    adapterID,
		AdapterName:  adapterName,
		AdapterIcon:  a.Icon(),
		CreatedAt:    meta.CreatedAt,
		UpdatedAt:    meta.UpdatedAt,
		Duration:     meta.UpdatedAt.Sub(meta.CreatedAt),
		IsActive:     time.Since(meta.UpdatedAt) < 5*time.Minute,
		TotalTokens:  meta.TotalTokens,
		MessageCount: meta.MsgCount,
		FileSize:     info.Size(),
		Path:         path,
	}, nil
}

// threadMatchesProject checks if a thread file is associated with the given project root.
func (a *Adapter) threadMatchesProject(path, absRoot string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	// Parse just the env field for efficiency
	var partial struct {
		Env *Env `json:"env,omitempty"`
	}
	if err := json.Unmarshal(data, &partial); err != nil {
		return false
	}

	if partial.Env == nil || partial.Env.Initial == nil {
		return false
	}

	for _, tree := range partial.Env.Initial.Trees {
		treePath := uriToPath(tree.URI)
		if treePath == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(treePath); err == nil {
			treePath = resolved
		}
		treePath = filepath.Clean(treePath)

		if pathMatchesProject(absRoot, treePath) {
			return true
		}
	}

	return false
}

// threadMetadata returns cached metadata if valid, otherwise parses the thread file.
func (a *Adapter) threadMetadata(path string, info os.FileInfo) (*threadMeta, error) {
	now := time.Now()

	a.metaMu.Lock()
	if entry, ok := a.metaCache[path]; ok && entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
		entry.lastAccess = now
		a.metaCache[path] = entry
		metaCopy := *entry.meta
		a.metaMu.Unlock()
		return &metaCopy, nil
	}
	a.metaMu.Unlock()

	meta, err := a.parseThreadMeta(path)
	if err != nil {
		return nil, err
	}

	a.metaMu.Lock()
	a.metaCache[path] = metaCacheEntry{
		meta:       meta,
		modTime:    info.ModTime(),
		size:       info.Size(),
		lastAccess: now,
	}
	a.enforceMetaCacheLimitLocked()
	a.metaMu.Unlock()

	return meta, nil
}

// parseThreadMeta extracts metadata from a thread JSON file.
func (a *Adapter) parseThreadMeta(path string) (*threadMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var thread Thread
	if err := json.Unmarshal(data, &thread); err != nil {
		return nil, err
	}

	meta := &threadMeta{
		ThreadID:  thread.ID,
		Path:      path,
		CreatedAt: thread.CreatedTime(),
	}

	var lastTimestamp time.Time
	var totalInput, totalOutput int

	for _, msg := range thread.Messages {
		// Count user and assistant messages
		if msg.Role == "user" || msg.Role == "assistant" {
			meta.MsgCount++
		}

		// Track timestamps from meta.sentAt for user messages
		if msg.Meta != nil && msg.Meta.SentAt > 0 {
			t := msg.Meta.SentAtTime()
			if t.After(lastTimestamp) {
				lastTimestamp = t
			}
		}

		// Track timestamps from usage for assistant messages
		if msg.Usage != nil && msg.Usage.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, msg.Usage.Timestamp); err == nil {
				t = t.Local()
				if t.After(lastTimestamp) {
					lastTimestamp = t
				}
			}
		}

		// Aggregate usage
		if msg.Usage != nil {
			totalInput += msg.Usage.TotalInputTokens
			totalOutput += msg.Usage.OutputTokens
			if meta.Model == "" {
				meta.Model = msg.Usage.Model
			}
		}

		// Extract first user message text for title
		if meta.FirstUserMessage == "" && msg.Role == "user" {
			for _, block := range msg.Content {
				if block.Type == "text" && block.Text != "" {
					meta.FirstUserMessage = block.Text
					break
				}
			}
		}
	}

	meta.TotalTokens = totalInput + totalOutput

	if lastTimestamp.IsZero() {
		meta.UpdatedAt = meta.CreatedAt
	} else {
		meta.UpdatedAt = lastTimestamp
	}

	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = meta.UpdatedAt
	}

	return meta, nil
}

// parseMessages fully parses all messages from a thread file.
func (a *Adapter) parseMessages(path string) ([]adapter.Message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var thread Thread
	if err := json.Unmarshal(data, &thread); err != nil {
		return nil, err
	}

	// Build tool result index: toolUseID -> ToolRun result
	toolResults := make(map[string]*ToolRun)
	for i := range thread.Messages {
		msg := &thread.Messages[i]
		if msg.Role != "user" {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "tool_result" && block.ToolUseID != "" && block.Run != nil {
				toolResults[block.ToolUseID] = block.Run
			}
		}
	}

	var messages []adapter.Message

	for _, msg := range thread.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}

		// Skip tool_result-only user messages (they're paired with tool_use)
		if msg.Role == "user" && isToolResultOnly(msg) {
			continue
		}

		adapterMsg := adapter.Message{
			ID:   fmt.Sprintf("%s-%d", thread.ID, msg.MessageID),
			Role: msg.Role,
		}

		// Determine timestamp
		if msg.Meta != nil && msg.Meta.SentAt > 0 {
			adapterMsg.Timestamp = msg.Meta.SentAtTime()
		} else if msg.Usage != nil && msg.Usage.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, msg.Usage.Timestamp); err == nil {
				adapterMsg.Timestamp = t.Local()
			}
		}

		// Set model and usage from assistant messages
		if msg.Usage != nil {
			adapterMsg.Model = msg.Usage.Model
			adapterMsg.TokenUsage = adapter.TokenUsage{
				InputTokens:  msg.Usage.TotalInputTokens,
				OutputTokens: msg.Usage.OutputTokens,
				CacheRead:    msg.Usage.CacheReadInputTokens,
				CacheWrite:   msg.Usage.CacheCreationInputTokens,
			}
		}

		// Parse content blocks
		var textParts []string
		for _, block := range msg.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					textParts = append(textParts, block.Text)
					adapterMsg.ContentBlocks = append(adapterMsg.ContentBlocks, adapter.ContentBlock{
						Type: "text",
						Text: block.Text,
					})
				}

			case "thinking":
				thinking := block.Thinking
				if thinking != "" {
					tokenCount := len(thinking) / 4
					adapterMsg.ThinkingBlocks = append(adapterMsg.ThinkingBlocks, adapter.ThinkingBlock{
						Content:    thinking,
						TokenCount: tokenCount,
					})
					adapterMsg.ContentBlocks = append(adapterMsg.ContentBlocks, adapter.ContentBlock{
						Type:       "thinking",
						Text:       thinking,
						TokenCount: tokenCount,
					})
				}

			case "tool_use":
				inputStr := ""
				if len(block.Input) > 0 && string(block.Input) != "null" {
					inputStr = string(block.Input)
				}

				// Look up paired result
				outputStr := ""
				isError := false
				if result, ok := toolResults[block.BlockID]; ok && result != nil && len(result.Result) > 0 {
					output, exitCode := result.ParseResult()
					outputStr = output
					isError = exitCode != 0
				}

				adapterMsg.ToolUses = append(adapterMsg.ToolUses, adapter.ToolUse{
					ID:     block.BlockID,
					Name:   block.Name,
					Input:  inputStr,
					Output: outputStr,
				})

				adapterMsg.ContentBlocks = append(adapterMsg.ContentBlocks, adapter.ContentBlock{
					Type:       "tool_use",
					ToolUseID:  block.BlockID,
					ToolName:   block.Name,
					ToolInput:  inputStr,
					ToolOutput: outputStr,
					IsError:    isError,
				})
			}
		}

		adapterMsg.Content = strings.Join(textParts, "\n")
		messages = append(messages, adapterMsg)
	}

	return messages, nil
}

// sessionFilePath returns the file path for a given session/thread ID.
func (a *Adapter) sessionFilePath(sessionID string) string {
	a.mu.RLock()
	if path, ok := a.sessionIndex[sessionID]; ok && path != "" {
		a.mu.RUnlock()
		return path
	}
	a.mu.RUnlock()

	// Try direct path construction
	path := filepath.Join(a.threadsDir, sessionID+".json")
	if _, err := os.Stat(path); err == nil {
		return path
	}

	// Scan directory
	entries, err := os.ReadDir(a.threadsDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !isThreadFile(e.Name()) {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		if name == sessionID {
			return filepath.Join(a.threadsDir, e.Name())
		}
	}

	return ""
}

// pruneMetaCache removes cache entries for paths no longer in use.
func (a *Adapter) pruneMetaCache(seenPaths map[string]struct{}) {
	a.metaMu.Lock()
	for path := range a.metaCache {
		if _, ok := seenPaths[path]; !ok {
			delete(a.metaCache, path)
		}
	}
	a.enforceMetaCacheLimitLocked()
	a.metaMu.Unlock()
}

// enforceMetaCacheLimitLocked evicts oldest entries when cache exceeds max size.
// Caller must hold metaMu write lock.
func (a *Adapter) enforceMetaCacheLimitLocked() {
	excess := len(a.metaCache) - metaCacheMaxEntries
	if excess <= 0 {
		return
	}

	type pathAccess struct {
		path       string
		lastAccess time.Time
	}
	entries := make([]pathAccess, 0, len(a.metaCache))
	for path, entry := range a.metaCache {
		entries = append(entries, pathAccess{path, entry.lastAccess})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastAccess.Before(entries[j].lastAccess)
	})

	for i := 0; i < excess; i++ {
		delete(a.metaCache, entries[i].path)
	}
}

// isThreadFile checks if a filename matches the T-{uuid}.json pattern.
func isThreadFile(name string) bool {
	return strings.HasPrefix(name, "T-") && strings.HasSuffix(name, ".json")
}

// isToolResultOnly checks if a user message contains only tool_result blocks.
func isToolResultOnly(msg Message) bool {
	if len(msg.Content) == 0 {
		return false
	}
	for _, block := range msg.Content {
		if block.Type != "tool_result" {
			return false
		}
	}
	return true
}

// uriToPath converts a file:// URI to a filesystem path.
func uriToPath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return ""
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return filepath.FromSlash(parsed.Path)
}

// pathMatchesProject checks if a tree path matches or is under the project root.
func pathMatchesProject(projectRoot, treePath string) bool {
	if projectRoot == "" || treePath == "" {
		return false
	}
	if projectRoot == treePath {
		return true
	}
	// Check if treePath is under projectRoot
	rel, err := filepath.Rel(projectRoot, treePath)
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
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

// shortID returns the first 12 characters of an ID, or the full ID if shorter.
func shortID(id string) string {
	if len(id) >= 12 {
		return id[:12]
	}
	return id
}

// truncateTitle truncates text to maxLen, adding "..." if truncated.
func truncateTitle(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)

	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
