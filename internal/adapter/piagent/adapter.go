package piagent

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
	"github.com/marcus/sidecar/internal/adapter/pi"
)

const (
	adapterID           = "pi-agent"
	adapterName         = "Pi Agent"
	adapterIcon         = "π"
	metaCacheMaxEntries = 2048
	msgCacheMaxEntries  = 128
)

// sessionMetaCacheEntry caches parsed session metadata with file stats.
type sessionMetaCacheEntry struct {
	meta        *pi.SessionMetadata
	modTime     time.Time
	size        int64
	lastAccess  time.Time
	byteOffset  int64
	modelCounts map[string]int
	modelTokens map[string]modelTokenEntry
}

// modelTokenEntry tracks per-model token accumulation.
type modelTokenEntry struct {
	in, out, cacheRead int
}

// messageCacheEntry holds cached messages with incremental parsing state.
type messageCacheEntry struct {
	messages     []adapter.Message
	toolUseRefs  map[string]toolUseRef
	pendingRefs  map[string]toolUseRef
	byteOffset   int64
	messageCount int
}

// toolUseRef tracks location of a tool use for deferred result linking.
type toolUseRef struct {
	msgIdx     int
	toolIdx    int
	contentIdx int
}

// Adapter implements the adapter.Adapter interface for standalone Pi Agent sessions.
type Adapter struct {
	sessionsDir  string
	sessionIndex map[string]string // sessionID -> file path
	metaCache    map[string]sessionMetaCacheEntry
	msgCache     *cache.Cache[messageCacheEntry]
	mu           sync.RWMutex
	metaMu       sync.RWMutex
}

// New creates a new Pi Agent adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		sessionsDir:  filepath.Join(home, ".pi", "agent", "sessions"),
		sessionIndex: make(map[string]string),
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

// Detect checks if Pi Agent sessions exist for the given project.
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

		newIndex[meta.SessionID] = path

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
			FileSize:        info.Size(),
			Path:            path,
			SessionCategory: meta.SessionCategory,
			CronJobName:     meta.CronJobName,
			SourceChannel:   meta.SourceChannel,
		})
	}

	a.mu.Lock()
	a.sessionIndex = newIndex
	a.mu.Unlock()

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
	return NewWatcher(a.projectDirPath(projectRoot))
}

// projectDirPath converts a project root path to the Pi Agent sessions directory path.
// Pi Agent encodes paths as: /home/user/project → --home-user-project--
func (a *Adapter) projectDirPath(projectRoot string) string {
	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		absPath = projectRoot
	}
	// /home/user/project → --home-user-project--
	// Strip leading slash, replace remaining slashes with dashes, wrap in --
	path := strings.TrimPrefix(absPath, "/")
	encoded := strings.ReplaceAll(path, "/", "-")
	return filepath.Join(a.sessionsDir, "--"+encoded+"--")
}

// sessionFilePath finds the JSONL file for a given session ID.
func (a *Adapter) sessionFilePath(sessionID string) string {
	a.mu.RLock()
	if path, ok := a.sessionIndex[sessionID]; ok {
		a.mu.RUnlock()
		return path
	}
	a.mu.RUnlock()

	// Fallback: scan all project directories
	entries, err := os.ReadDir(a.sessionsDir)
	if err != nil {
		return ""
	}

	for _, projDir := range entries {
		if !projDir.IsDir() {
			continue
		}
		projPath := filepath.Join(a.sessionsDir, projDir.Name())
		files, err := os.ReadDir(projPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			// Check if filename contains the session ID
			if strings.Contains(f.Name(), sessionID) {
				path := filepath.Join(projPath, f.Name())
				a.mu.Lock()
				a.sessionIndex[sessionID] = path
				a.mu.Unlock()
				return path
			}
		}
	}
	return ""
}

// --- Session metadata cache ---

func (a *Adapter) sessionMetadata(path string, info os.FileInfo) (*pi.SessionMetadata, error) {
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

func (a *Adapter) parseSessionMetadataFull(path string) (*pi.SessionMetadata, int64, map[string]int, map[string]modelTokenEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	defer func() { _ = file.Close() }()

	meta := &pi.SessionMetadata{
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

func (a *Adapter) parseSessionMetadataIncremental(path string, base *pi.SessionMetadata, offset int64, baseModelCounts map[string]int, baseModelTokens map[string]modelTokenEntry) (*pi.SessionMetadata, int64, map[string]int, map[string]modelTokenEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Seek(offset, 0); err != nil {
		return nil, 0, nil, nil, err
	}

	meta := &pi.SessionMetadata{
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

func (a *Adapter) processMetadataLine(line []byte, meta *pi.SessionMetadata, modelCounts map[string]int, modelTokens map[string]modelTokenEntry, currentModel *string) {
	var raw pi.RawLine
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
			return
		}

		if meta.FirstMsg.IsZero() {
			meta.FirstMsg = raw.Timestamp
		}
		meta.LastMsg = raw.Timestamp
		meta.MsgCount++

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

			if usage.Cost != nil {
				meta.EstCost += usage.Cost.Total
			}
		}
	}
}

func (a *Adapter) finalizeMetadata(meta *pi.SessionMetadata, modelCounts map[string]int) {
	var maxCount int
	for model, count := range modelCounts {
		if count > maxCount {
			maxCount = count
			meta.PrimaryModel = model
		}
	}
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

// --- Message parsing ---

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

func (a *Adapter) processMessageLine(line []byte, messages *[]adapter.Message, toolUseRefs, pendingRefs map[string]toolUseRef) {
	var raw pi.RawLine
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
		toolCallID := raw.Message.ToolCallID
		if toolCallID == "" {
			return
		}

		resultContent := extractTextContent(raw.Message.Content)

		ref, ok := toolUseRefs[toolCallID]
		if !ok {
			return
		}

		if ref.toolIdx >= 0 && ref.toolIdx < len((*messages)[ref.msgIdx].ToolUses) {
			(*messages)[ref.msgIdx].ToolUses[ref.toolIdx].Output = resultContent
		}

		if ref.contentIdx >= 0 && ref.contentIdx < len((*messages)[ref.msgIdx].ContentBlocks) {
			(*messages)[ref.msgIdx].ContentBlocks[ref.contentIdx].ToolOutput = resultContent
		}

		toolResultBlock := adapter.ContentBlock{
			Type:       "tool_result",
			ToolUseID:  toolCallID,
			ToolName:   raw.Message.ToolName,
			ToolOutput: resultContent,
		}

		if ref.msgIdx < len(*messages) {
			(*messages)[ref.msgIdx].ContentBlocks = append((*messages)[ref.msgIdx].ContentBlocks, toolResultBlock)
		}

		delete(pendingRefs, toolCallID)
	}
}

// parseContent extracts text, tool uses, thinking blocks, and content blocks
// from a Pi message content array.
func parseContent(rawContent json.RawMessage) (string, []adapter.ToolUse, []adapter.ThinkingBlock, []adapter.ContentBlock) {
	if len(rawContent) == 0 {
		return "", nil, nil, nil
	}

	var strContent string
	if err := json.Unmarshal(rawContent, &strContent); err == nil {
		contentBlocks := []adapter.ContentBlock{{Type: "text", Text: strContent}}
		return strContent, nil, nil, contentBlocks
	}

	var blocks []pi.ContentBlock
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

	var blocks []pi.ContentBlock
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

func extractCronJobName(msg string) string {
	closeBracket := strings.Index(msg, "]")
	if closeBracket < 0 {
		return ""
	}
	inner := msg[len("[cron:"):closeBracket]
	spaceIdx := strings.Index(inner, " ")
	if spaceIdx < 0 {
		return ""
	}
	return strings.TrimSpace(inner[spaceIdx+1:])
}

func detectSourceChannel(msg string) string {
	if strings.HasPrefix(msg, "[Telegram") {
		return "telegram"
	}
	if strings.HasPrefix(msg, "[WhatsApp") {
		return "whatsapp"
	}
	return "direct"
}

func stripMessagePrefix(content string) string {
	if strings.HasPrefix(content, "[Telegram") {
		if idx := strings.Index(content, "] "); idx != -1 {
			return content[idx+2:]
		}
	}
	if strings.HasPrefix(content, "[cron:") {
		if idx := strings.Index(content, "] "); idx != -1 {
			return content[idx+2:]
		}
	}
	if strings.HasPrefix(content, "System: [") {
		if idx := strings.Index(content, "] "); idx != -1 {
			return content[idx+2:]
		}
	}
	return content
}

func extractSourceLabel(content string) string {
	if strings.HasPrefix(content, "[Telegram") {
		closeBracket := strings.Index(content, "]")
		if closeBracket < 0 {
			return "[TG]"
		}
		inner := content[len("[Telegram "):closeBracket]
		if parenIdx := strings.Index(inner, " ("); parenIdx > 0 {
			return "[TG] " + inner[:parenIdx]
		}
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

// SessionByID returns a single session by ID without scanning the directory.
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
	if meta.SessionCategory == adapter.SessionCategoryCron && meta.CronJobName != "" {
		name = meta.CronJobName
	} else if meta.FirstUserMessage != "" {
		name = truncateTitle(stripMessagePrefix(meta.FirstUserMessage), 120)
	}
	if name == "" {
		name = shortID(meta.SessionID)
	}

	return &adapter.Session{
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
		FileSize:        info.Size(),
		Path:            path,
		SessionCategory: meta.SessionCategory,
		CronJobName:     meta.CronJobName,
		SourceChannel:   meta.SourceChannel,
	}, nil
}
