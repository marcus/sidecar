package pi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"maps"
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
	adapterID           = "pi"
	adapterName         = "Pi"
	adapterIcon         = "\U0001F43E" // 🐾
	metaCacheMaxEntries = 2048
	msgCacheMaxEntries  = 128
	dirCacheTTL         = 500 * time.Millisecond
)

// cwdCacheEntry caches the CWD from a session file's first line.
type cwdCacheEntry struct {
	cwd     string
	modTime time.Time
	size    int64
}

// dirCacheEntry caches the directory listing with expiration.
type dirCacheEntry struct {
	files     []sessionFileEntry
	expiresAt time.Time
}

// sessionFileEntry holds a session file path with its FileInfo.
type sessionFileEntry struct {
	path string
	info os.FileInfo
}

// sessionMetaCacheEntry caches parsed session metadata with file stats.
type sessionMetaCacheEntry struct {
	meta        *SessionMetadata
	modTime     time.Time
	size        int64
	lastAccess  time.Time
	byteOffset  int64                      // position after last parsed line (for incremental)
	modelCounts map[string]int             // per-model message counts
	modelTokens map[string]modelTokenEntry // per-model token accumulation
}

// modelTokenEntry tracks per-model token accumulation.
type modelTokenEntry struct {
	in, out, cacheRead int
}

// messageCacheEntry holds cached messages with incremental parsing state.
type messageCacheEntry struct {
	messages     []adapter.Message
	toolUseRefs  map[string]toolUseRef // tool call ID -> location in messages
	pendingRefs  map[string]toolUseRef // unresolved tool uses awaiting results
	byteOffset   int64                 // resume point for incremental parse
	messageCount int                   // for validation
}

// toolUseRef tracks location of a tool use for deferred result linking.
type toolUseRef struct {
	msgIdx     int
	toolIdx    int
	contentIdx int
}

// Adapter implements the adapter.Adapter interface for Pi sessions.
type Adapter struct {
	sessionsDir  string
	sessionIndex map[string]string // sessionID -> file path
	cwdCache     map[string]cwdCacheEntry
	metaCache    map[string]sessionMetaCacheEntry
	msgCache     *cache.Cache[messageCacheEntry]
	dirCache     *dirCacheEntry
	mu           sync.RWMutex // guards sessionIndex
	cwdMu        sync.RWMutex // guards cwdCache
	metaMu       sync.RWMutex // guards metaCache
	dirCacheMu   sync.RWMutex // guards dirCache
}

// New creates a new Pi adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		sessionsDir:  filepath.Join(home, ".openclaw", "agents", "main", "sessions"),
		sessionIndex: make(map[string]string),
		cwdCache:     make(map[string]cwdCacheEntry),
		metaCache:    make(map[string]sessionMetaCacheEntry),
		msgCache:     cache.New[messageCacheEntry](msgCacheMaxEntries),
	}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return adapterID }

// Name returns the human-readable adapter name.
func (a *Adapter) Name() string { return adapterName }

// Icon returns the adapter icon for badge display.
func (a *Adapter) Icon() string { return adapterIcon }

// Detect checks if Pi sessions exist for the given project.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	files, err := a.sessionFiles()
	if err != nil {
		return false, err
	}
	resolvedProject := newResolvedProjectPath(projectRoot)
	if resolvedProject == nil {
		return false, nil
	}
	for _, f := range files {
		cwd, err := a.sessionCWD(f.path, f.info)
		if err != nil {
			continue
		}
		if resolvedProject.matchesCWD(cwd) {
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

// WatchScope returns Global because Pi watches a global sessions directory.
func (a *Adapter) WatchScope() adapter.WatchScope {
	return adapter.WatchScopeGlobal
}

// Sessions returns all sessions for the given project, sorted by update time.
func (a *Adapter) Sessions(projectRoot string) ([]adapter.Session, error) {
	files, err := a.sessionFiles()
	if err != nil {
		return nil, err
	}

	resolvedProject := newResolvedProjectPath(projectRoot)

	sessions := make([]adapter.Session, 0, len(files))
	seenPaths := make(map[string]struct{}, len(files))
	newIndex := make(map[string]string, len(files))

	for _, f := range files {
		seenPaths[f.path] = struct{}{}

		// Fast CWD check first (reads only first line)
		cwd, err := a.sessionCWD(f.path, f.info)
		if err != nil {
			continue
		}
		if resolvedProject != nil && !resolvedProject.matchesCWD(cwd) {
			continue
		}

		meta, err := a.sessionMetadata(f.path, f.info)
		if err != nil {
			continue
		}

		// Skip sessions with no messages
		if meta.MsgCount == 0 {
			continue
		}

		name := ""
		if meta.SessionCategory == adapter.SessionCategoryCron && meta.CronJobName != "" {
			name = meta.CronJobName
		} else if meta.FirstUserMessage != "" {
			name = truncateTitle(stripMessagePrefix(meta.FirstUserMessage), 120)
		}
		if name == "" {
			name = shortID(meta.SessionID)
		}

		newIndex[meta.SessionID] = f.path

		sessions = append(sessions, adapter.Session{
			ID:              meta.SessionID,
			Name:            name,
			AdapterID:       adapterID,
			AdapterName:     adapterName,
			AdapterIcon:     adapterIcon,
			CreatedAt:       meta.FirstMsg,
			UpdatedAt:       meta.LastMsg,
			Duration:        meta.LastMsg.Sub(meta.FirstMsg),
			IsActive:        time.Since(meta.LastMsg) < 5*time.Minute,
			TotalTokens:     meta.TotalTokens,
			EstCost:         meta.EstCost,
			MessageCount:    meta.MsgCount,
			FileSize:        f.info.Size(),
			Path:            f.path,
			SessionCategory: meta.SessionCategory,
			CronJobName:     meta.CronJobName,
			SourceChannel:   meta.SourceChannel,
		})
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

	if a.msgCache != nil {
		cached, offset, cachedSize, cachedModTime, ok := a.msgCache.GetWithOffset(path)
		if ok {
			if info.Size() == cachedSize && info.ModTime().Equal(cachedModTime) {
				return copyMessages(cached.messages), nil
			}
			if info.Size() > cachedSize && offset > 0 {
				messages, entry, err := a.parseMessagesIncremental(path, cached, offset, info)
				if err == nil {
					a.msgCache.Set(path, entry, info.Size(), info.ModTime(), entry.byteOffset)
					return messages, nil
				}
				// Fall through to full parse on error
			}
		}
	}

	messages, entry, err := a.parseMessagesFull(path, info)
	if err != nil {
		return nil, err
	}

	if a.msgCache != nil {
		a.msgCache.Set(path, entry, info.Size(), info.ModTime(), entry.byteOffset)
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

// Watch returns a channel that emits events when session data changes.
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, io.Closer, error) {
	return NewWatcher(a.sessionsDir)
}

// --- CWD cache (reads only first JSONL line) ---

// sessionCWD returns the CWD from a session file, using a cache.
func (a *Adapter) sessionCWD(path string, info os.FileInfo) (string, error) {
	a.cwdMu.RLock()
	entry, ok := a.cwdCache[path]
	a.cwdMu.RUnlock()

	if ok && entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
		return entry.cwd, nil
	}

	cwd, err := readSessionCWD(path)
	if err != nil {
		return "", err
	}

	a.cwdMu.Lock()
	a.cwdCache[path] = cwdCacheEntry{
		cwd:     cwd,
		modTime: info.ModTime(),
		size:    info.Size(),
	}
	a.cwdMu.Unlock()

	return cwd, nil
}

// readSessionCWD reads the first line of a JSONL session file to extract CWD.
func readSessionCWD(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	buf := cache.GetScannerBuffer()
	defer cache.PutScannerBuffer(buf)
	scanner.Buffer(buf, 10*1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("empty session file: %s", path)
	}

	var header struct {
		Type string `json:"type"`
		CWD  string `json:"cwd"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return "", err
	}
	if header.Type != "session" {
		return "", fmt.Errorf("first line is not session header: %s", path)
	}
	return header.CWD, nil
}

// --- Directory listing cache ---

func (a *Adapter) sessionFiles() ([]sessionFileEntry, error) {
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

	entries, err := os.ReadDir(a.sessionsDir)
	if err != nil {
		return nil, err
	}

	files := make([]sessionFileEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, sessionFileEntry{
			path: filepath.Join(a.sessionsDir, e.Name()),
			info: info,
		})
	}

	a.dirCacheMu.Lock()
	a.dirCache = &dirCacheEntry{
		files:     files,
		expiresAt: time.Now().Add(dirCacheTTL),
	}
	a.dirCacheMu.Unlock()

	return files, nil
}

// --- Session metadata cache ---

// sessionMetadata returns cached metadata if valid, otherwise parses the file.
// Supports incremental parsing when a file grows.
func (a *Adapter) sessionMetadata(path string, info os.FileInfo) (*SessionMetadata, error) {
	now := time.Now()

	a.metaMu.Lock()
	entry, cached := a.metaCache[path]
	if cached {
		if entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
			entry.lastAccess = now
			a.metaCache[path] = entry
			metaCopy := *entry.meta
			a.metaMu.Unlock()
			return &metaCopy, nil
		}
		if info.Size() > entry.size && entry.byteOffset > 0 {
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

// parseSessionMetadataFull scans the entire session file for metadata.
func (a *Adapter) parseSessionMetadataFull(path string) (*SessionMetadata, int64, map[string]int, map[string]modelTokenEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	defer func() { _ = file.Close() }()

	meta := &SessionMetadata{
		Path:      path,
		SessionID: strings.TrimSuffix(filepath.Base(path), ".jsonl"),
	}

	scanner := bufio.NewScanner(file)
	buf := cache.GetScannerBuffer()
	defer cache.PutScannerBuffer(buf)
	scanner.Buffer(buf, 10*1024*1024)

	modelCounts := make(map[string]int)
	modelTokens := make(map[string]modelTokenEntry)
	var bytesRead int64
	var currentModel string

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesRead += int64(len(line)) + 1

		a.processMetadataLine(line, meta, modelCounts, modelTokens, &currentModel)
	}

	a.finalizeMetadata(meta, modelCounts)

	if meta.FirstMsg.IsZero() {
		meta.FirstMsg = time.Now()
		meta.LastMsg = time.Now()
	}

	return meta, bytesRead, modelCounts, modelTokens, nil
}

// parseSessionMetadataIncremental resumes parsing from a byte offset.
func (a *Adapter) parseSessionMetadataIncremental(path string, base *SessionMetadata, offset int64, baseModelCounts map[string]int, baseModelTokens map[string]modelTokenEntry) (*SessionMetadata, int64, map[string]int, map[string]modelTokenEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Seek(offset, 0); err != nil {
		return nil, 0, nil, nil, err
	}

	meta := &SessionMetadata{
		Path:             base.Path,
		SessionID:        base.SessionID,
		CWD:              base.CWD,
		Version:          base.Version,
		FirstMsg:         base.FirstMsg,
		LastMsg:          base.LastMsg,
		MsgCount:         base.MsgCount,
		TotalTokens:      base.TotalTokens,
		EstCost:          base.EstCost,
		PrimaryModel:     base.PrimaryModel,
		FirstUserMessage: base.FirstUserMessage,
		SessionCategory:  base.SessionCategory,
		CronJobName:      base.CronJobName,
		SourceChannel:    base.SourceChannel,
	}

	modelCounts := make(map[string]int, len(baseModelCounts))
	maps.Copy(modelCounts, baseModelCounts)
	modelTokens := make(map[string]modelTokenEntry, len(baseModelTokens))
	maps.Copy(modelTokens, baseModelTokens)

	scanner := bufio.NewScanner(file)
	buf := cache.GetScannerBuffer()
	defer cache.PutScannerBuffer(buf)
	scanner.Buffer(buf, 10*1024*1024)

	bytesRead := offset
	var currentModel string
	if meta.PrimaryModel != "" {
		currentModel = meta.PrimaryModel
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		bytesRead += int64(len(line)) + 1

		a.processMetadataLine(line, meta, modelCounts, modelTokens, &currentModel)
	}

	a.finalizeMetadata(meta, modelCounts)

	return meta, bytesRead, modelCounts, modelTokens, nil
}

// processMetadataLine parses a single JSONL line and accumulates metadata.
func (a *Adapter) processMetadataLine(line []byte, meta *SessionMetadata, modelCounts map[string]int, modelTokens map[string]modelTokenEntry, currentModel *string) {
	var raw RawLine
	if err := json.Unmarshal(line, &raw); err != nil {
		return
	}

	switch raw.Type {
	case "session":
		if meta.SessionID == "" || meta.SessionID == strings.TrimSuffix(filepath.Base(meta.Path), ".jsonl") {
			meta.SessionID = raw.ID
		}
		if meta.CWD == "" {
			meta.CWD = raw.CWD
		}
		if meta.Version == 0 {
			meta.Version = raw.Version
		}

	case "model_change":
		if raw.ModelID != "" {
			*currentModel = raw.ModelID
		}

	case "message":
		if raw.Message == nil {
			return
		}
		role := raw.Message.Role
		if role != "user" && role != "assistant" {
			// toolResult lines don't count as messages for metadata
			return
		}

		if meta.FirstMsg.IsZero() {
			meta.FirstMsg = raw.Timestamp
		}
		meta.LastMsg = raw.Timestamp
		meta.MsgCount++

		// Extract first user message for title and classify session
		if meta.FirstUserMessage == "" && role == "user" {
			content := extractTextContent(raw.Message.Content)
			if content != "" {
				meta.FirstUserMessage = content
				cat, cronName, srcChannel := extractSessionMetadata(content)
				meta.SessionCategory = cat
				meta.CronJobName = cronName
				meta.SourceChannel = srcChannel
			}
		}

		// Track model usage and tokens from assistant messages
		if role == "assistant" && raw.Message.Usage != nil {
			usage := raw.Message.Usage
			meta.TotalTokens += usage.Input + usage.Output

			model := raw.Message.Model
			if model == "" {
				model = *currentModel
			}
			if model != "" {
				modelCounts[model]++
				mt := modelTokens[model]
				mt.in += usage.Input
				mt.out += usage.Output
				mt.cacheRead += usage.CacheRead
				modelTokens[model] = mt
			}

			// Use pre-calculated cost if available
			if usage.Cost != nil {
				meta.EstCost += usage.Cost.Total
			}
		}
	}
}

// finalizeMetadata determines PrimaryModel from per-model tracking.
func (a *Adapter) finalizeMetadata(meta *SessionMetadata, modelCounts map[string]int) {
	var maxCount int
	for model, count := range modelCounts {
		if count > maxCount {
			maxCount = count
			meta.PrimaryModel = model
		}
	}
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

	for i := range excess {
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

// --- Session file path lookup ---

func (a *Adapter) sessionFilePath(sessionID string) string {
	a.mu.RLock()
	if path, ok := a.sessionIndex[sessionID]; ok && path != "" {
		a.mu.RUnlock()
		return path
	}
	a.mu.RUnlock()

	// Fallback: scan session files
	files, err := a.sessionFiles()
	if err != nil {
		return ""
	}

	// Try filename match first
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f.path), ".jsonl")
		if base == sessionID {
			a.mu.Lock()
			a.sessionIndex[sessionID] = f.path
			a.mu.Unlock()
			return f.path
		}
	}

	// Try parsing session header for ID match
	for _, f := range files {
		cwd, _ := a.sessionCWD(f.path, f.info)
		_ = cwd // We're just populating the cache as a side effect; check header ID
		meta, err := a.sessionMetadata(f.path, f.info)
		if err != nil {
			continue
		}
		if meta.SessionID == sessionID {
			a.mu.Lock()
			a.sessionIndex[sessionID] = f.path
			a.mu.Unlock()
			return f.path
		}
	}

	return ""
}

// --- Message parsing ---

// parseMessagesFull parses all messages from a session file.
func (a *Adapter) parseMessagesFull(path string, info os.FileInfo) ([]adapter.Message, messageCacheEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, messageCacheEntry{}, err
	}
	defer func() { _ = file.Close() }()

	var messages []adapter.Message
	toolUseRefs := make(map[string]toolUseRef)
	pendingRefs := make(map[string]toolUseRef)
	var bytesRead int64

	scanner := bufio.NewScanner(file)
	buf := cache.GetScannerBuffer()
	defer cache.PutScannerBuffer(buf)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesRead += int64(len(line)) + 1

		a.processMessageLine(line, &messages, toolUseRefs, pendingRefs)
	}

	if err := scanner.Err(); err != nil {
		return messages, messageCacheEntry{}, err
	}

	entry := messageCacheEntry{
		messages:     copyMessages(messages),
		toolUseRefs:  toolUseRefs,
		pendingRefs:  pendingRefs,
		byteOffset:   bytesRead,
		messageCount: len(messages),
	}

	a.invalidateSessionMetaCacheIfChanged(path, info)
	return messages, entry, nil
}

// parseMessagesIncremental resumes parsing from a byte offset.
func (a *Adapter) parseMessagesIncremental(path string, cached messageCacheEntry, startOffset int64, info os.FileInfo) ([]adapter.Message, messageCacheEntry, error) {
	reader, err := cache.NewIncrementalReader(path, startOffset)
	if err != nil {
		return nil, messageCacheEntry{}, err
	}
	defer func() { _ = reader.Close() }()

	messages := copyMessages(cached.messages)
	toolUseRefs := copyToolUseRefs(cached.toolUseRefs)
	pendingRefs := copyToolUseRefs(cached.pendingRefs)

	for {
		line, err := reader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, messageCacheEntry{}, err
		}

		a.processMessageLine(line, &messages, toolUseRefs, pendingRefs)
	}

	entry := messageCacheEntry{
		messages:     copyMessages(messages),
		toolUseRefs:  toolUseRefs,
		pendingRefs:  pendingRefs,
		byteOffset:   reader.Offset(),
		messageCount: len(messages),
	}

	a.invalidateSessionMetaCacheIfChanged(path, info)
	return messages, entry, nil
}

// processMessageLine parses a single JSONL line and appends to messages.
func (a *Adapter) processMessageLine(line []byte, messages *[]adapter.Message, toolUseRefs, pendingRefs map[string]toolUseRef) {
	var raw RawLine
	if err := json.Unmarshal(line, &raw); err != nil {
		return
	}

	if raw.Type != "message" || raw.Message == nil {
		return
	}

	switch raw.Message.Role {
	case "user":
		content, _, _, contentBlocks := parseContent(raw.Message.Content)
		sourceLabel := extractSourceLabel(content)
		content = stripMessagePrefix(content)
		// Also strip prefixes from text content blocks
		for i := range contentBlocks {
			if contentBlocks[i].Type == "text" {
				contentBlocks[i].Text = stripMessagePrefix(contentBlocks[i].Text)
			}
		}
		msg := adapter.Message{
			ID:            raw.ID,
			Role:          "user",
			Content:       content,
			Timestamp:     raw.Timestamp,
			ContentBlocks: contentBlocks,
			SourceLabel:   sourceLabel,
		}
		*messages = append(*messages, msg)

	case "assistant":
		content, toolUses, thinkingBlocks, contentBlocks := parseContent(raw.Message.Content)
		msg := adapter.Message{
			ID:             raw.ID,
			Role:           "assistant",
			Content:        content,
			Timestamp:      raw.Timestamp,
			Model:          raw.Message.Model,
			ToolUses:       toolUses,
			ThinkingBlocks: thinkingBlocks,
			ContentBlocks:  contentBlocks,
		}

		if raw.Message.Usage != nil {
			msg.TokenUsage = adapter.TokenUsage{
				InputTokens:  raw.Message.Usage.Input,
				OutputTokens: raw.Message.Usage.Output,
				CacheRead:    raw.Message.Usage.CacheRead,
				CacheWrite:   raw.Message.Usage.CacheWrite,
			}
		}

		msgIdx := len(*messages)
		*messages = append(*messages, msg)

		// Track tool use references for later linking
		for toolIdx, tu := range (*messages)[msgIdx].ToolUses {
			if tu.ID != "" {
				ref := toolUseRef{msgIdx: msgIdx, toolIdx: toolIdx, contentIdx: -1}
				toolUseRefs[tu.ID] = ref
				pendingRefs[tu.ID] = ref
			}
		}
		for contentIdx, cb := range (*messages)[msgIdx].ContentBlocks {
			if cb.Type == "tool_use" && cb.ToolUseID != "" {
				if ref, ok := toolUseRefs[cb.ToolUseID]; ok {
					ref.contentIdx = contentIdx
					toolUseRefs[cb.ToolUseID] = ref
					pendingRefs[cb.ToolUseID] = ref
				}
			}
		}

	case "toolResult":
		// Link tool result back to the assistant message that made the call
		toolCallID := raw.Message.ToolCallID
		if toolCallID == "" {
			return
		}

		resultContent := extractTextContent(raw.Message.Content)

		ref, ok := toolUseRefs[toolCallID]
		if !ok {
			return
		}

		// Update the tool use output in the assistant message
		if ref.toolIdx >= 0 && ref.toolIdx < len((*messages)[ref.msgIdx].ToolUses) {
			(*messages)[ref.msgIdx].ToolUses[ref.toolIdx].Output = resultContent
		}

		// Update the content block if tracked
		if ref.contentIdx >= 0 && ref.contentIdx < len((*messages)[ref.msgIdx].ContentBlocks) {
			(*messages)[ref.msgIdx].ContentBlocks[ref.contentIdx].ToolOutput = resultContent
		}

		// Also add a tool_result ContentBlock to the conversation flow
		// so the UI can render it
		toolResultBlock := adapter.ContentBlock{
			Type:       "tool_result",
			ToolUseID:  toolCallID,
			ToolName:   raw.Message.ToolName,
			ToolOutput: resultContent,
		}

		// Append to the assistant message's content blocks
		if ref.msgIdx < len(*messages) {
			(*messages)[ref.msgIdx].ContentBlocks = append((*messages)[ref.msgIdx].ContentBlocks, toolResultBlock)
		}

		// Remove from pending
		delete(pendingRefs, toolCallID)
	}
}

// parseContent extracts text, tool uses, thinking blocks, and content blocks
// from a Pi message content array.
func parseContent(rawContent json.RawMessage) (string, []adapter.ToolUse, []adapter.ThinkingBlock, []adapter.ContentBlock) {
	if len(rawContent) == 0 {
		return "", nil, nil, nil
	}

	// Try parsing as string first
	var strContent string
	if err := json.Unmarshal(rawContent, &strContent); err == nil {
		contentBlocks := []adapter.ContentBlock{{Type: "text", Text: strContent}}
		return strContent, nil, nil, contentBlocks
	}

	var blocks []ContentBlock
	if err := json.Unmarshal(rawContent, &blocks); err != nil {
		return "", nil, nil, nil
	}

	texts := make([]string, 0, len(blocks))
	toolUses := make([]adapter.ToolUse, 0)
	thinkingBlocks := make([]adapter.ThinkingBlock, 0)
	contentBlocks := make([]adapter.ContentBlock, 0, len(blocks))

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

		case "toolCall":
			inputStr := ""
			if block.Arguments != nil {
				inputStr = string(block.Arguments)
			}
			toolUses = append(toolUses, adapter.ToolUse{
				ID:    block.ID,
				Name:  block.Name,
				Input: inputStr,
			})
			contentBlocks = append(contentBlocks, adapter.ContentBlock{
				Type:      "tool_use",
				ToolUseID: block.ID,
				ToolName:  block.Name,
				ToolInput: inputStr,
			})
		}
	}

	content := strings.Join(texts, "\n")
	return content, toolUses, thinkingBlocks, contentBlocks
}

// extractTextContent extracts concatenated text from a content array.
func extractTextContent(rawContent json.RawMessage) string {
	if len(rawContent) == 0 {
		return ""
	}

	var strContent string
	if err := json.Unmarshal(rawContent, &strContent); err == nil {
		return strContent
	}

	var blocks []ContentBlock
	if err := json.Unmarshal(rawContent, &blocks); err != nil {
		return ""
	}

	var texts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// --- Deep copy helpers ---

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

func copyToolUseRefs(refs map[string]toolUseRef) map[string]toolUseRef {
	if refs == nil {
		return make(map[string]toolUseRef)
	}
	cp := make(map[string]toolUseRef, len(refs))
	maps.Copy(cp, refs)
	return cp
}

// --- Project path matching ---

type resolvedProjectPath struct {
	abs string
}

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

// --- Utility functions ---

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func truncateTitle(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)

	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// classifySession determines the session category from the first user message.
func classifySession(firstUserMessage string) string {
	cat, _, _ := extractSessionMetadata(firstUserMessage)
	return cat
}

// extractSessionMetadata parses the first user message to determine category,
// cron job name, and source channel.
func extractSessionMetadata(firstUserMessage string) (category, cronJobName, sourceChannel string) {
	if strings.HasPrefix(firstUserMessage, "[cron:") {
		category = adapter.SessionCategoryCron
		cronJobName = extractCronJobName(firstUserMessage)
		return
	}
	if strings.HasPrefix(firstUserMessage, "System:") {
		if strings.Contains(firstUserMessage, "WhatsApp gateway") || strings.Contains(firstUserMessage, "WhatsApp") {
			// WhatsApp gateway preamble — real human conversation, not a system command (td-3b259b)
			category = adapter.SessionCategoryInteractive
			sourceChannel = "whatsapp"
		} else {
			category = adapter.SessionCategorySystem
		}
		return
	}
	category = adapter.SessionCategoryInteractive
	sourceChannel = detectSourceChannel(firstUserMessage)
	return
}

// extractCronJobName parses "[cron:UUID job-name]" prefix and returns the job name.
func extractCronJobName(msg string) string {
	// Expected format: "[cron:UUID job-name] ..."
	closeBracket := strings.Index(msg, "]")
	if closeBracket < 0 {
		return ""
	}
	inner := msg[len("[cron:"):closeBracket] // "UUID job-name"
	// Find the first space after the UUID
	spaceIdx := strings.Index(inner, " ")
	if spaceIdx < 0 {
		return ""
	}
	return strings.TrimSpace(inner[spaceIdx+1:])
}

// detectSourceChannel determines the messaging source from the first user message.
func detectSourceChannel(msg string) string {
	if strings.HasPrefix(msg, "[Telegram") {
		return "telegram"
	}
	if strings.HasPrefix(msg, "[WhatsApp") {
		return "whatsapp"
	}
	return "direct"
}

// stripMessagePrefix removes structured channel prefixes from user message content,
// returning the clean message body. Handles Telegram, cron, and System prefixes.
func stripMessagePrefix(content string) string {
	// Strip [Telegram ...] prefix
	if strings.HasPrefix(content, "[Telegram") {
		if idx := strings.Index(content, "] "); idx != -1 {
			return content[idx+2:]
		}
	}
	// Strip [cron:...] prefix
	if strings.HasPrefix(content, "[cron:") {
		if idx := strings.Index(content, "] "); idx != -1 {
			return content[idx+2:]
		}
	}
	// Strip System: [...] prefix
	if strings.HasPrefix(content, "System: [") {
		if idx := strings.Index(content, "] "); idx != -1 {
			return content[idx+2:]
		}
	}
	return content
}

// extractSourceLabel parses a structured message prefix and returns a display label.
// Returns e.g. "[TG] Marcus Vorwaller", "[WA]", "[cron] job-name", or "".
func extractSourceLabel(content string) string {
	if strings.HasPrefix(content, "[Telegram") {
		closeBracket := strings.Index(content, "]")
		if closeBracket < 0 {
			return "[TG]"
		}
		inner := content[len("[Telegram "):closeBracket] // "Marcus Vorwaller (@handle) id:123 +1m 2026-..."
		// Extract the display name: everything before the first " ("
		if parenIdx := strings.Index(inner, " ("); parenIdx > 0 {
			return "[TG] " + inner[:parenIdx]
		}
		// Fallback: use everything before " id:"
		if idIdx := strings.Index(inner, " id:"); idIdx > 0 {
			return "[TG] " + inner[:idIdx]
		}
		return "[TG]"
	}
	if strings.HasPrefix(content, "[cron:") {
		jobName := extractCronJobName(content)
		if jobName != "" {
			return "[cron] " + jobName
		}
		return "[cron]"
	}
	if strings.HasPrefix(content, "System: [") {
		return "[sys]"
	}
	return ""
}
