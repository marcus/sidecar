package opencode

import (
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

const (
	adapterID           = "opencode"
	adapterName         = "OpenCode"
	metaCacheMaxEntries = 2048
)

// Adapter implements the adapter.Adapter interface for OpenCode sessions.
type Adapter struct {
	storageDir     string
	projectIndex   map[string]*Project // worktree path -> Project
	sessionIndex   map[string]string   // sessionID -> project ID
	projectsLoaded bool                // true after loadProjects populates projectIndex
	metaCache      map[string]sessionMetaCacheEntry
	metaMu         sync.RWMutex // guards metaCache
}

// sessionMetaCacheEntry caches parsed session metadata with validation info.
type sessionMetaCacheEntry struct {
	meta       *SessionMetadata
	modTime    time.Time
	size       int64
	lastAccess time.Time
}

// New creates a new OpenCode adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		storageDir:   filepath.Join(home, ".local", "share", "opencode", "storage"),
		projectIndex: make(map[string]*Project),
		sessionIndex: make(map[string]string),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return adapterID }

// Name returns the human-readable adapter name.
func (a *Adapter) Name() string { return adapterName }

// Icon returns the adapter icon for badge display.
func (a *Adapter) Icon() string { return "◇" }

// Detect checks if OpenCode sessions exist for the given project.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	projectID, err := a.findProjectID(projectRoot)
	if err != nil {
		return false, nil
	}
	if projectID == "" {
		return false, nil
	}

	// Check if there are any sessions for this project
	sessionDir := filepath.Join(a.storageDir, "session", projectID)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
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
	projectID, err := a.findProjectID(projectRoot)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		return nil, nil
	}

	sessionDir := filepath.Join(a.storageDir, "session", projectID)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	sessions := make([]adapter.Session, 0, len(entries))
	seenPaths := make(map[string]struct{}, len(entries))
	a.sessionIndex = make(map[string]string)

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		path := filepath.Join(sessionDir, e.Name())
		seenPaths[path] = struct{}{}

		info, err := e.Info()
		if err != nil {
			continue
		}

		meta, err := a.sessionMetadata(path, info, projectID)
		if err != nil {
			continue
		}

		// Store in index for Messages() lookup
		a.sessionIndex[meta.SessionID] = projectID

		// Determine name - use title, first user message, or short ID
		name := meta.Title
		if name == "" && meta.FirstUserMessage != "" {
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
			EstCost:      meta.EstCost,
			IsSubAgent:   meta.ParentID != "",
			MessageCount: meta.MsgCount,
		})
	}

	// Sort by UpdatedAt descending (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	a.pruneSessionMetaCache(seenPaths)

	return sessions, nil
}

// Messages returns all messages for the given session.
// Uses batch reading to minimize file I/O overhead.
func (a *Adapter) Messages(sessionID string) ([]adapter.Message, error) {
	messageDir := filepath.Join(a.storageDir, "message", sessionID)

	// Batch read all message files at once
	msgMap, err := a.batchReadMessages(messageDir)
	if err != nil {
		return nil, err
	}
	if len(msgMap) == 0 {
		return nil, nil
	}

	// Collect all message IDs for batch part loading
	messageIDs := make([]string, 0, len(msgMap))
	for id := range msgMap {
		messageIDs = append(messageIDs, id)
	}

	// Batch load all parts for all messages at once
	partsMap := a.batchLoadAllParts(messageIDs)

	// Build adapter messages using pre-loaded data
	messages := make([]adapter.Message, 0, len(msgMap))
	for _, msg := range msgMap {
		parts := partsMap[msg.ID]

		// Build content string
		var contentParts []string
		if parts.content != "" {
			contentParts = append(contentParts, parts.content)
		}
		if len(parts.fileRefs) > 0 {
			contentParts = append(contentParts, fmt.Sprintf("[files: %s]", strings.Join(parts.fileRefs, ", ")))
		}
		if len(parts.patchFiles) > 0 {
			contentParts = append(contentParts, fmt.Sprintf("[edited: %d files]", len(parts.patchFiles)))
		}

		// Get model from either ModelID field or Model.ModelID
		model := msg.ModelID
		if model == "" && msg.Model != nil {
			model = msg.Model.ModelID
		}

		adapterMsg := adapter.Message{
			ID:             msg.ID,
			Role:           msg.Role,
			Content:        strings.Join(contentParts, "\n"),
			Timestamp:      msg.Time.CreatedTime(),
			Model:          model,
			ToolUses:       parts.toolUses,
			ThinkingBlocks: parts.thinkingBlocks,
		}

		// Add token usage
		if msg.Tokens != nil {
			adapterMsg.TokenUsage = adapter.TokenUsage{
				InputTokens:  msg.Tokens.Input,
				OutputTokens: msg.Tokens.Output,
			}
			if msg.Tokens.Cache != nil {
				adapterMsg.CacheRead = msg.Tokens.Cache.Read
				adapterMsg.CacheWrite = msg.Tokens.Cache.Write
			}
		}

		messages = append(messages, adapterMsg)
	}

	// Sort by timestamp ascending
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

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
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, error) {
	projectID, err := a.findProjectID(projectRoot)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		return nil, fmt.Errorf("no OpenCode project found for %s", projectRoot)
	}

	sessionDir := filepath.Join(a.storageDir, "session", projectID)
	return NewWatcher(sessionDir)
}

// findProjectID finds the OpenCode project ID for the given project root.
func (a *Adapter) findProjectID(projectRoot string) (string, error) {
	// Normalize the project root path
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}
	absRoot = filepath.Clean(absRoot)

	// Load and cache all projects once
	if !a.projectsLoaded {
		if err := a.loadProjects(); err != nil {
			return "", err
		}
	}

	// Lookup in cache
	if proj, ok := a.projectIndex[absRoot]; ok {
		return proj.ID, nil
	}

	return "", nil
}

// loadProjects loads all projects from storage/project/*.json and populates projectIndex.
func (a *Adapter) loadProjects() error {
	projectDir := filepath.Join(a.storageDir, "project")
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		if os.IsNotExist(err) {
			a.projectsLoaded = true
			return nil
		}
		return err
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		path := filepath.Join(projectDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var proj Project
		if err := json.Unmarshal(data, &proj); err != nil {
			continue
		}

		// Skip global project
		if proj.Worktree == "/" {
			continue
		}

		// Normalize and cache worktree path
		worktree := proj.Worktree
		if resolved, err := filepath.EvalSymlinks(worktree); err == nil {
			worktree = resolved
		}
		worktree = filepath.Clean(worktree)

		projCopy := proj // Copy to avoid pointer aliasing
		a.projectIndex[worktree] = &projCopy
	}

	a.projectsLoaded = true
	return nil
}

// DiscoverRelatedProjectDirs scans OpenCode project files for worktree paths
// related to the given main worktree path. This finds conversations from deleted
// worktrees by checking if stored worktree paths share the same repository base name.
func (a *Adapter) DiscoverRelatedProjectDirs(mainWorktreePath string) ([]string, error) {
	absMain, err := filepath.Abs(mainWorktreePath)
	if err != nil {
		return nil, nil
	}
	repoName := filepath.Base(absMain)
	if repoName == "" || repoName == "." || repoName == "/" {
		return nil, nil
	}

	// Load projects if not already loaded
	if !a.projectsLoaded {
		if err := a.loadProjects(); err != nil {
			return nil, nil
		}
	}

	var related []string
	for worktreePath := range a.projectIndex {
		// Check if this worktree path is related to our repo
		base := filepath.Base(worktreePath)
		if base == repoName || strings.HasPrefix(base, repoName+"-") {
			related = append(related, worktreePath)
		}
	}

	return related, nil
}

// sessionMetadata returns cached metadata if valid, otherwise parses the session file.
func (a *Adapter) sessionMetadata(path string, info os.FileInfo, projectID string) (*SessionMetadata, error) {
	now := time.Now()

	a.metaMu.RLock()
	if entry, ok := a.metaCache[path]; ok && entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
		// Return a copy to prevent caller mutations affecting cache
		metaCopy := *entry.meta
		a.metaMu.RUnlock()

		// Update last access time
		a.metaMu.Lock()
		if entry, ok := a.metaCache[path]; ok {
			entry.lastAccess = now
			a.metaCache[path] = entry
		}
		a.metaMu.Unlock()
		return &metaCopy, nil
	}
	a.metaMu.RUnlock()

	meta, err := a.parseSessionFile(path, projectID)
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

// pruneSessionMetaCache removes cache entries for paths no longer in use.
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

// enforceSessionMetaCacheLimitLocked evicts oldest entries when cache exceeds max size.
// Caller must hold metaMu write lock.
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

// parseSessionFile parses a session JSON file and returns metadata.
// For performance, this only reads the session file and counts message files
// without reading their contents. Token counts and costs are populated
// when Messages() is called.
func (a *Adapter) parseSessionFile(path, projectID string) (*SessionMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}

	meta := &SessionMetadata{
		Path:      path,
		SessionID: sess.ID,
		ProjectID: projectID,
		Title:     sess.Title,
		ParentID:  sess.ParentID,
		FirstMsg:  sess.Time.CreatedTime(),
		LastMsg:   sess.Time.UpdatedTime(),
	}

	// Get summary stats from session if available
	if sess.Summary != nil {
		meta.Additions = sess.Summary.Additions
		meta.Deletions = sess.Summary.Deletions
		meta.FileCount = sess.Summary.Files
	}
	// Skip session_diff fallback for performance - diff stats are less critical

	// Count messages by counting files without reading them (O(1) per file)
	messageDir := filepath.Join(a.storageDir, "message", sess.ID)
	if entries, err := os.ReadDir(messageDir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				meta.MsgCount++
			}
		}
	}

	// Note: FirstUserMessage, TotalTokens, EstCost, PrimaryModel are left empty
	// for Sessions() list view. They will be populated when Messages() is called
	// and the user views a specific session.

	return meta, nil
}

// parseMessageFile parses a message JSON file.
func (a *Adapter) parseMessageFile(path string) (*Message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	return &msg, nil
}

// loadParts loads all parts for a message and returns aggregated content.
func (a *Adapter) loadParts(messageID string) (content string, toolUses []adapter.ToolUse, thinkingBlocks []adapter.ThinkingBlock, fileRefs []string, patchFiles []string) {
	partDir := filepath.Join(a.storageDir, "part", messageID)
	entries, err := os.ReadDir(partDir)
	if err != nil {
		return
	}

	var textParts []string

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		path := filepath.Join(partDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var part Part
		if err := json.Unmarshal(data, &part); err != nil {
			continue
		}

		switch part.Type {
		case "text":
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}

		case "tool":
			tu := adapter.ToolUse{
				ID:   part.CallID,
				Name: part.Tool,
			}
			if part.State != nil {
				tu.Input = ToolInputString(part.State.Input)
				tu.Output = ToolOutputString(part.State.Output)
			}
			toolUses = append(toolUses, tu)

		case "file":
			if part.Filename != "" {
				fileRefs = append(fileRefs, part.Filename)
			}

		case "patch":
			patchFiles = append(patchFiles, part.Files...)

		case "step-finish":
			// Could extract thinking tokens here if needed
			// For now, we skip step markers

		case "step-start", "compaction":
			// Skip these types
		}
	}

	content = strings.Join(textParts, "\n")
	return
}

// parsedParts holds the aggregated parts data for a message.
type parsedParts struct {
	content        string
	toolUses       []adapter.ToolUse
	thinkingBlocks []adapter.ThinkingBlock
	fileRefs       []string
	patchFiles     []string
}

// batchReadMessages reads all message files from a directory and parses them.
// Returns a map of messageID -> Message for efficient lookup.
func (a *Adapter) batchReadMessages(messageDir string) (map[string]*Message, error) {
	entries, err := os.ReadDir(messageDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	result := make(map[string]*Message, len(entries))

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		path := filepath.Join(messageDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		// Only keep user/assistant messages
		if msg.Role == "user" || msg.Role == "assistant" {
			result[msg.ID] = &msg
		}
	}

	return result, nil
}

// batchLoadAllParts reads all parts for all given message IDs in a single pass.
// Returns a map of messageID -> parsedParts.
func (a *Adapter) batchLoadAllParts(messageIDs []string) map[string]parsedParts {
	result := make(map[string]parsedParts, len(messageIDs))
	partBaseDir := filepath.Join(a.storageDir, "part")

	for _, msgID := range messageIDs {
		partDir := filepath.Join(partBaseDir, msgID)
		entries, err := os.ReadDir(partDir)
		if err != nil {
			result[msgID] = parsedParts{}
			continue
		}

		var textParts []string
		var toolUses []adapter.ToolUse
		var thinkingBlocks []adapter.ThinkingBlock
		var fileRefs []string
		var patchFiles []string

		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}

			path := filepath.Join(partDir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			var part Part
			if err := json.Unmarshal(data, &part); err != nil {
				continue
			}

			switch part.Type {
			case "text":
				if part.Text != "" {
					textParts = append(textParts, part.Text)
				}
			case "tool":
				tu := adapter.ToolUse{
					ID:   part.CallID,
					Name: part.Tool,
				}
				if part.State != nil {
					tu.Input = ToolInputString(part.State.Input)
					tu.Output = ToolOutputString(part.State.Output)
				}
				toolUses = append(toolUses, tu)
			case "file":
				if part.Filename != "" {
					fileRefs = append(fileRefs, part.Filename)
				}
			case "patch":
				patchFiles = append(patchFiles, part.Files...)
			}
		}

		result[msgID] = parsedParts{
			content:        strings.Join(textParts, "\n"),
			toolUses:       toolUses,
			thinkingBlocks: thinkingBlocks,
			fileRefs:       fileRefs,
			patchFiles:     patchFiles,
		}
	}

	return result
}

// calculateCost estimates cost based on model and token usage.
func calculateCost(model string, inputTokens, outputTokens, cacheRead int) float64 {
	var inRate, outRate float64
	model = strings.ToLower(model)

	switch {
	case strings.Contains(model, "opus"):
		inRate, outRate = 15.0, 75.0
	case strings.Contains(model, "sonnet"):
		inRate, outRate = 3.0, 15.0
	case strings.Contains(model, "haiku"):
		inRate, outRate = 0.25, 1.25
	case strings.Contains(model, "gpt-4o"):
		inRate, outRate = 2.5, 10.0
	case strings.Contains(model, "gpt-4"):
		inRate, outRate = 10.0, 30.0
	case strings.Contains(model, "o1"):
		inRate, outRate = 15.0, 60.0
	case strings.Contains(model, "gemini"):
		inRate, outRate = 1.25, 5.0
	case strings.Contains(model, "deepseek"):
		inRate, outRate = 0.14, 0.28
	default:
		// Default to sonnet rates
		inRate, outRate = 3.0, 15.0
	}

	// Cache reads get 90% discount
	regularIn := inputTokens - cacheRead
	if regularIn < 0 {
		regularIn = 0
	}
	cacheInCost := float64(cacheRead) * inRate * 0.1 / 1_000_000
	regularInCost := float64(regularIn) * inRate / 1_000_000
	outCost := float64(outputTokens) * outRate / 1_000_000

	return cacheInCost + regularInCost + outCost
}

// shortID returns the first 12 characters of an ID, or the full ID if shorter.
func shortID(id string) string {
	if len(id) >= 12 {
		return id[:12]
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
