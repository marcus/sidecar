package geminicli

import (
	"crypto/sha256"
	"encoding/hex"
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
	adapterID           = "gemini-cli"
	adapterName         = "Gemini CLI"
	metaCacheMaxEntries = 2048
)

// Adapter implements the adapter.Adapter interface for Gemini CLI sessions.
type Adapter struct {
	tmpDir       string
	sessionIndex map[string]string // sessionID -> file path cache
	indexMu      sync.RWMutex      // protects sessionIndex
	metaCache    map[string]sessionMetaCacheEntry
	metaMu       sync.RWMutex // guards metaCache
}

// sessionMetaCacheEntry caches parsed session metadata with validation info.
type sessionMetaCacheEntry struct {
	meta       *SessionMetadata
	modTime    time.Time
	size       int64
	lastAccess time.Time
}

// New creates a new Gemini CLI adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		tmpDir:       filepath.Join(home, ".gemini", "tmp"),
		sessionIndex: make(map[string]string),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return adapterID }

// Name returns the human-readable adapter name.
func (a *Adapter) Name() string { return adapterName }

// Icon returns the adapter icon for badge display.
func (a *Adapter) Icon() string { return "★" }

// Detect checks if Gemini CLI sessions exist for the given project.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	chatsDir := a.chatsDir(projectRoot)
	entries, err := os.ReadDir(chatsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "session-") && strings.HasSuffix(e.Name(), ".json") {
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
	chatsDir := a.chatsDir(projectRoot)
	entries, err := os.ReadDir(chatsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	sessions := make([]adapter.Session, 0, len(entries))
	seenPaths := make(map[string]struct{}, len(entries))
	// Reset cache on full session enumeration
	a.indexMu.Lock()
	a.sessionIndex = make(map[string]string)
	a.indexMu.Unlock()
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "session-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		path := filepath.Join(chatsDir, e.Name())
		seenPaths[path] = struct{}{}

		info, err := e.Info()
		if err != nil {
			continue
		}

		meta, err := a.sessionMetadata(path, info)
		if err != nil {
			continue
		}

		// Cache session path for fast lookup
		a.indexMu.Lock()
		a.sessionIndex[meta.SessionID] = path
		a.indexMu.Unlock()

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
			Slug:         shortID(meta.SessionID),
			AdapterID:    adapterID,
			AdapterName:  adapterName,
			AdapterIcon:  a.Icon(),
			CreatedAt:    meta.StartTime,
			UpdatedAt:    meta.LastUpdated,
			Duration:     meta.LastUpdated.Sub(meta.StartTime),
			IsActive:     time.Since(meta.LastUpdated) < 5*time.Minute,
			TotalTokens:  meta.TotalTokens,
			EstCost:      meta.EstCost,
			IsSubAgent:   false,
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
func (a *Adapter) Messages(sessionID string) ([]adapter.Message, error) {
	path := a.sessionFilePath(sessionID)
	if path == "" {
		return nil, nil
	}

	session, err := a.parseSessionFile(path)
	if err != nil {
		return nil, err
	}

	var messages []adapter.Message
	for _, msg := range session.Messages {
		// Skip info messages
		if msg.Type == "info" {
			continue
		}

		// Map type to role
		role := msg.Type
		if role == "gemini" {
			role = "assistant"
		}

		m := adapter.Message{
			ID:        msg.ID,
			Role:      role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
			Model:     msg.Model,
		}

		// Parse tokens
		if msg.Tokens != nil {
			m.TokenUsage = adapter.TokenUsage{
				InputTokens:  msg.Tokens.Input + msg.Tokens.Cached,
				OutputTokens: msg.Tokens.Output,
				CacheRead:    msg.Tokens.Cached,
			}
		}

		// Parse tool uses
		for _, tc := range msg.ToolCalls {
			inputStr := ""
			if tc.Args != nil {
				if b, err := json.Marshal(tc.Args); err == nil {
					inputStr = string(b)
				}
			}
			outputStr := ""
			if tc.Result != nil {
				if b, err := json.Marshal(tc.Result); err == nil {
					outputStr = string(b)
				}
			}
			m.ToolUses = append(m.ToolUses, adapter.ToolUse{
				ID:     tc.ID,
				Name:   tc.Name,
				Input:  inputStr,
				Output: outputStr,
			})
		}

		// Parse thinking blocks from thoughts
		for _, t := range msg.Thoughts {
			content := t.Subject
			if t.Description != "" {
				content = fmt.Sprintf("%s: %s", t.Subject, t.Description)
			}
			m.ThinkingBlocks = append(m.ThinkingBlocks, adapter.ThinkingBlock{
				Content:    content,
				TokenCount: len(content) / 4,
			})
		}

		messages = append(messages, m)
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
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, error) {
	return NewWatcher(a.chatsDir(projectRoot))
}

// projectHash computes SHA256 of the absolute project path.
func projectHash(projectRoot string) string {
	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		absPath = projectRoot
	}
	h := sha256.Sum256([]byte(absPath))
	return hex.EncodeToString(h[:])
}

// chatsDir returns the chats directory for a project.
func (a *Adapter) chatsDir(projectRoot string) string {
	return filepath.Join(a.tmpDir, projectHash(projectRoot), "chats")
}

// sessionFilePath finds the session file for a given session ID.
func (a *Adapter) sessionFilePath(sessionID string) string {
	// Check cache first
	a.indexMu.RLock()
	path, ok := a.sessionIndex[sessionID]
	a.indexMu.RUnlock()
	if ok {
		return path
	}

	// Fallback: scan all project directories
	entries, err := os.ReadDir(a.tmpDir)
	if err != nil {
		return ""
	}

	for _, projDir := range entries {
		if !projDir.IsDir() {
			continue
		}
		chatsDir := filepath.Join(a.tmpDir, projDir.Name(), "chats")
		files, err := os.ReadDir(chatsDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasPrefix(f.Name(), "session-") || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			path := filepath.Join(chatsDir, f.Name())
			session, err := a.parseSessionFile(path)
			if err != nil {
				continue
			}
			if session.SessionID == sessionID {
				// Cache for future lookups
				a.indexMu.Lock()
				a.sessionIndex[sessionID] = path
				a.indexMu.Unlock()
				return path
			}
		}
	}
	return ""
}

// sessionMetadata returns cached metadata if valid, otherwise parses the session file.
func (a *Adapter) sessionMetadata(path string, info os.FileInfo) (*SessionMetadata, error) {
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

// parseSessionFile reads and parses a session JSON file.
func (a *Adapter) parseSessionFile(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

// parseSessionMetadata extracts metadata from a session file.
func (a *Adapter) parseSessionMetadata(path string) (*SessionMetadata, error) {
	session, err := a.parseSessionFile(path)
	if err != nil {
		return nil, err
	}

	meta := &SessionMetadata{
		Path:        path,
		SessionID:   session.SessionID,
		ProjectHash: session.ProjectHash,
		StartTime:   session.StartTime,
		LastUpdated: session.LastUpdated,
	}

	modelTokens := make(map[string]struct{ in, out int })

	for _, msg := range session.Messages {
		// Skip info messages
		if msg.Type == "info" {
			continue
		}
		meta.MsgCount++

		// Extract first user message content for title
		if meta.FirstUserMessage == "" && msg.Type == "user" && msg.Content != "" {
			meta.FirstUserMessage = msg.Content
		}

		if msg.Tokens != nil {
			meta.TotalTokens += msg.Tokens.Input + msg.Tokens.Output

			if msg.Model != "" {
				mt := modelTokens[msg.Model]
				mt.in += msg.Tokens.Input
				mt.out += msg.Tokens.Output
				modelTokens[msg.Model] = mt
			}
		}
	}

	// Determine primary model and calculate cost
	var maxTokens int
	for model, mt := range modelTokens {
		total := mt.in + mt.out
		if total > maxTokens {
			maxTokens = total
			meta.PrimaryModel = model
		}

		// Gemini pricing (approximate)
		var inRate, outRate float64
		switch {
		case strings.Contains(model, "pro"):
			inRate, outRate = 1.25, 5.0 // Gemini Pro
		case strings.Contains(model, "flash"):
			inRate, outRate = 0.075, 0.30 // Gemini Flash
		default:
			inRate, outRate = 0.50, 1.50 // Default
		}
		meta.EstCost += float64(mt.in)*inRate/1_000_000 +
			float64(mt.out)*outRate/1_000_000
	}

	return meta, nil
}

// shortID returns the first 8 characters of an ID.
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
