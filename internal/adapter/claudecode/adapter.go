package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
)

// xmlTagRegex matches XML/HTML-like tags for stripping from session titles
var xmlTagRegex = regexp.MustCompile(`<[^>]+>`)

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

		// Skip sessions with no messages (metadata-only files)
		if meta.MsgCount == 0 {
			continue
		}

		// Use first user message as name, with fallbacks
		name := ""
		if meta.FirstUserMessage != "" {
			name = truncateTitle(meta.FirstUserMessage, 120)
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

// SessionByID returns a single session by ID without scanning the directory (td-27f6a1).
// Implements adapter.TargetedRefresher for efficient targeted refresh.
func (a *Adapter) SessionByID(sessionID string) (*adapter.Session, error) {
	path := a.sessionFilePath(sessionID)
	if path == "" {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	meta, err := a.sessionMetadata(path, info)
	if err != nil {
		return nil, err
	}
	if meta.MsgCount == 0 {
		return nil, fmt.Errorf("session %s has no messages", sessionID)
	}

	name := ""
	if meta.FirstUserMessage != "" {
		name = truncateTitle(meta.FirstUserMessage, 120)
	}
	if name == "" && meta.Slug != "" {
		name = meta.Slug
	}
	if name == "" {
		name = shortID(meta.SessionID)
	}

	isSubAgent := strings.HasPrefix(filepath.Base(path), "agent-")

	return &adapter.Session{
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
	}, nil
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

// DiscoverRelatedProjectDirs scans ~/.claude/projects/ for directories that appear
// related to the given main worktree path. This finds conversations from deleted
// worktrees that git no longer knows about.
//
// Returns decoded absolute paths (e.g., "/Users/foo/code/myrepo-feature") for
// directories whose encoded name shares the same repository base name.
func (a *Adapter) DiscoverRelatedProjectDirs(mainWorktreePath string) ([]string, error) {
	absMain, err := filepath.Abs(mainWorktreePath)
	if err != nil {
		return nil, nil
	}
	repoName := filepath.Base(absMain)
	if repoName == "" || repoName == "." || repoName == "/" {
		return nil, nil
	}

	entries, err := os.ReadDir(a.projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var related []string
	// Encode the main path to find its pattern in directory names
	// e.g., /Users/foo/code/myrepo -> -Users-foo-code-myrepo
	encodedMain := strings.ReplaceAll(absMain, "/", "-")

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "-") {
			continue
		}

		// Match directories that:
		// 1. Exactly match the main repo encoded path
		// 2. Start with the main repo encoded path followed by hyphen (worktree suffix)
		if name == encodedMain || strings.HasPrefix(name, encodedMain+"-") {
			// Decode: -Users-foo-code-myrepo -> /Users/foo/code/myrepo
			decoded := strings.ReplaceAll(name, "-", "/")
			related = append(related, decoded)
		}
	}

	return related, nil
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

// parseSessionMetadata is a convenience wrapper for full metadata parsing.
func (a *Adapter) parseSessionMetadata(path string) (*SessionMetadata, error) {
	meta, _, _, _, err := a.parseSessionMetadataFull(path)
	return meta, err
}

// parseSessionMetadataFull extracts metadata from a session file, scanning all lines.
// Returns metadata, final byte offset, and per-model tracking for incremental use.
func (a *Adapter) parseSessionMetadataFull(path string) (*SessionMetadata, int64, map[string]int, map[string]modelTokenEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, nil, err
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
	modelTokens := make(map[string]modelTokenEntry)
	var bytesRead int64

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesRead += int64(len(line)) + 1 // +1 for newline

		a.processMetadataLine(line, meta, modelCounts, modelTokens)
	}

	a.finalizeMetadataCost(meta, modelCounts, modelTokens)

	if meta.FirstMsg.IsZero() {
		meta.FirstMsg = time.Now()
		meta.LastMsg = time.Now()
	}

	return meta, bytesRead, modelCounts, modelTokens, nil
}

// parseSessionMetadataIncremental resumes parsing from a byte offset (td-1b774e).
// Reuses cached head metadata (FirstMsg, CWD, etc.) and accumulates new tail data.
func (a *Adapter) parseSessionMetadataIncremental(path string, base *SessionMetadata, offset int64, baseModelCounts map[string]int, baseModelTokens map[string]modelTokenEntry) (*SessionMetadata, int64, map[string]int, map[string]modelTokenEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	defer file.Close()

	if _, err := file.Seek(offset, 0); err != nil {
		return nil, 0, nil, nil, err
	}

	// Copy base metadata (immutable fields preserved)
	meta := &SessionMetadata{
		Path:             base.Path,
		SessionID:        base.SessionID,
		Slug:             base.Slug,
		CWD:              base.CWD,
		Version:          base.Version,
		GitBranch:        base.GitBranch,
		FirstMsg:         base.FirstMsg,
		LastMsg:          base.LastMsg,
		MsgCount:         base.MsgCount,
		TotalTokens:      base.TotalTokens,
		FirstUserMessage: base.FirstUserMessage,
	}

	// Copy model tracking maps
	modelCounts := make(map[string]int, len(baseModelCounts))
	for k, v := range baseModelCounts {
		modelCounts[k] = v
	}
	modelTokens := make(map[string]modelTokenEntry, len(baseModelTokens))
	for k, v := range baseModelTokens {
		modelTokens[k] = v
	}

	scanner := bufio.NewScanner(file)
	buf := getScannerBuffer()
	defer putScannerBuffer(buf)
	scanner.Buffer(buf, 10*1024*1024)

	bytesRead := offset
	for scanner.Scan() {
		line := scanner.Bytes()
		bytesRead += int64(len(line)) + 1

		a.processMetadataLine(line, meta, modelCounts, modelTokens)
	}

	a.finalizeMetadataCost(meta, modelCounts, modelTokens)

	return meta, bytesRead, modelCounts, modelTokens, nil
}

// processMetadataLine parses a single JSONL line and accumulates metadata.
func (a *Adapter) processMetadataLine(line []byte, meta *SessionMetadata, modelCounts map[string]int, modelTokens map[string]modelTokenEntry) {
	var raw RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return
	}

	// Skip non-message types
	if raw.Type != "user" && raw.Type != "assistant" {
		return
	}

	if meta.FirstMsg.IsZero() {
		meta.FirstMsg = raw.Timestamp
		meta.CWD = raw.CWD
		meta.Version = raw.Version
		meta.GitBranch = raw.GitBranch
	}
	if meta.Slug == "" && raw.Slug != "" {
		meta.Slug = raw.Slug
	}
	if meta.FirstUserMessage == "" && raw.Type == "user" && raw.Message != nil {
		content, _, _ := a.parseContent(raw.Message.Content)
		if content != "" {
			extracted := extractUserQuery(content)
			if extracted != "" && !isTrivialCommand(extracted) {
				meta.FirstUserMessage = content
			}
		}
	}
	meta.LastMsg = raw.Timestamp
	meta.MsgCount++

	if raw.Message != nil && raw.Message.Usage != nil {
		usage := raw.Message.Usage
		meta.TotalTokens += usage.InputTokens + usage.OutputTokens

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

// finalizeMetadataCost calculates PrimaryModel and EstCost from per-model tracking.
func (a *Adapter) finalizeMetadataCost(meta *SessionMetadata, modelCounts map[string]int, modelTokens map[string]modelTokenEntry) {
	var maxCount int
	meta.PrimaryModel = ""
	meta.EstCost = 0

	for model, count := range modelCounts {
		if count > maxCount {
			maxCount = count
			meta.PrimaryModel = model
		}
	}

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
}

// modelTokenEntry tracks per-model token accumulation for incremental cost calculation.
type modelTokenEntry struct {
	in, out, cache int
}

type sessionMetaCacheEntry struct {
	meta        *SessionMetadata
	modTime     time.Time
	size        int64
	lastAccess  time.Time
	byteOffset  int64                    // position after last parsed line (for incremental)
	modelCounts map[string]int           // per-model message counts
	modelTokens map[string]modelTokenEntry // per-model token accumulation
}

// sessionMetadata returns cached metadata if valid, otherwise parses the file.
// Supports incremental parsing when a file grows (td-1b774e): reuses cached
// metadata and resumes parsing from the last byte offset.
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
		if info.Size() > entry.size && entry.byteOffset > 0 {
			// File grew - try incremental parse from saved offset (td-1b774e)
			a.metaMu.Unlock()
			meta, newOffset, mc, mt, err := a.parseSessionMetadataIncremental(path, entry.meta, entry.byteOffset, entry.modelCounts, entry.modelTokens)
			if err == nil {
				a.metaMu.Lock()
				a.metaCache[path] = sessionMetaCacheEntry{
					meta:        meta,
					modTime:     info.ModTime(),
					size:        info.Size(),
					lastAccess:  now,
					byteOffset:  newOffset,
					modelCounts: mc,
					modelTokens: mt,
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

	meta, offset, mc, mt, err := a.parseSessionMetadataFull(path)
	if err != nil {
		return nil, err
	}

	a.metaMu.Lock()
	a.metaCache[path] = sessionMetaCacheEntry{
		meta:        meta,
		modTime:     info.ModTime(),
		size:        info.Size(),
		lastAccess:  now,
		byteOffset:  offset,
		modelCounts: mc,
		modelTokens: mt,
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

// shortID returns the first 8 characters of an ID, or the full ID if shorter.
func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// extractUserQuery extracts the actual user query from text that may contain XML tags.
// Claude Code messages often contain system context wrapped in XML tags like:
// <local-command-caveat>, <user_query>, <system-reminder>, etc.
// This function extracts just the user's actual request.
func extractUserQuery(s string) string {
	// First try to extract content from <user_query> tags (common in Claude Code)
	if start := strings.Index(s, "<user_query>"); start >= 0 {
		if end := strings.Index(s, "</user_query>"); end > start {
			extracted := strings.TrimSpace(s[start+len("<user_query>") : end])
			if extracted != "" {
				return extracted
			}
		}
	}

	// Handle local command messages (e.g., /clear, /compact, etc.)
	// These have <command-name> and optionally <command-message> tags
	if strings.Contains(s, "<local-command-caveat>") || strings.Contains(s, "<command-name>") {
		// Try to extract command name
		if start := strings.Index(s, "<command-name>"); start >= 0 {
			if end := strings.Index(s[start:], "</command-name>"); end > 0 {
				cmdName := strings.TrimSpace(s[start+len("<command-name>") : start+end])
				// Try to get command message too
				cmdMsg := ""
				if msgStart := strings.Index(s, "<command-message>"); msgStart >= 0 {
					if msgEnd := strings.Index(s[msgStart:], "</command-message>"); msgEnd > 0 {
						cmdMsg = strings.TrimSpace(s[msgStart+len("<command-message>") : msgStart+msgEnd])
					}
				}
				if cmdMsg != "" && cmdMsg != cmdName {
					return cmdName + ": " + cmdMsg
				}
				return cmdName
			}
		}
	}

	// Strip all XML tags
	cleaned := xmlTagRegex.ReplaceAllString(s, " ")

	// Collapse multiple spaces and clean up
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	cleaned = strings.TrimSpace(cleaned)

	// Skip common caveat/system phrases that aren't useful as titles
	skipPhrases := []string{
		"Caveat: The messages below",
		"Caveat:",
		"DO NOT respond to these messages",
	}
	for _, phrase := range skipPhrases {
		if strings.HasPrefix(cleaned, phrase) {
			// This is a system/caveat message with no real user content
			// Return empty so caller can use fallback (slug, ID, etc.)
			return ""
		}
	}

	// Return the cleaned content (may be empty if input was just XML tags)
	return cleaned
}

// isTrivialCommand returns true if the text is a trivial command that shouldn't
// be used as a session title (like /clear, /compact, empty strings, etc.)
func isTrivialCommand(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return true
	}
	// Skip slash commands
	trivialCommands := []string{
		"/clear", "/compact", "/config", "/help", "/init",
		"/bug", "/cost", "/doctor", "/feedback", "/login", "/logout",
		"clear", "compact", "help",
	}
	for _, cmd := range trivialCommands {
		if s == cmd || strings.HasPrefix(s, cmd+":") || strings.HasPrefix(s, cmd+" ") {
			return true
		}
	}
	return false
}

// truncateTitle truncates text to maxLen, adding "..." if truncated.
// It first extracts the actual user query (stripping XML tags),
// then replaces newlines with spaces for display.
func truncateTitle(s string, maxLen int) string {
	// Extract actual user query first (strips XML tags)
	s = extractUserQuery(s)

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
