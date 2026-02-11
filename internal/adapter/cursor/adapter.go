package cursor

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/marcus/sidecar/internal/adapter"
)

// sqlitePoolSettings configures connection pool to prevent FD leaks (td-649ba4).
// Read-only queries only need 1 connection; no idle connections prevents FD buildup.
func sqlitePoolSettings(db *sql.DB) {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	db.SetConnMaxLifetime(time.Second)
}

const (
	adapterID   = "cursor-cli"
	adapterName = "Cursor CLI"
)

// xmlTagRegex is pre-compiled for performance in hot path
var xmlTagRegex = regexp.MustCompile(`<[^>]+>`)

// sessionCacheEntry stores cached session metadata to avoid re-parsing (td-107eea24)
type sessionCacheEntry struct {
	size         int64  // file size when cached
	mtime        int64  // modification time when cached (unix nano)
	walSize      int64  // WAL file size when cached (td-8cf39632)
	walMtime     int64  // WAL modification time when cached (td-8cf39632)
	messageCount int    // cached message count
	firstUserMsg string // cached first user message
}

// Adapter implements the adapter.Adapter interface for Cursor CLI sessions.
type Adapter struct {
	chatsDir string

	// Session metadata cache keyed by dbPath (td-107eea24)
	sessionCache   map[string]sessionCacheEntry
	sessionCacheMu sync.RWMutex // protects sessionCache (td-90a73d68)
}

// New creates a new Cursor CLI adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		chatsDir:     filepath.Join(home, ".cursor", "chats"),
		sessionCache: make(map[string]sessionCacheEntry),
	}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return adapterID }

// Name returns the human-readable adapter name.
func (a *Adapter) Name() string { return adapterName }

// Icon returns the adapter icon for badge display.
func (a *Adapter) Icon() string { return "▌" }

// Capabilities returns the supported features.
func (a *Adapter) Capabilities() adapter.CapabilitySet {
	return adapter.CapabilitySet{
		adapter.CapSessions: true,
		adapter.CapMessages: true,
		adapter.CapUsage:    false, // Token usage not available in cursor format
		adapter.CapWatch:    true,
	}
}

// Detect checks if Cursor CLI sessions exist for the given project.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		return false, err
	}

	workspaceDir := a.workspacePath(absPath)
	entries, err := os.ReadDir(workspaceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	// Check if any session directories exist with store.db
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dbPath := filepath.Join(workspaceDir, e.Name(), "store.db")
		if _, err := os.Stat(dbPath); err == nil {
			return true, nil
		}
	}
	return false, nil
}

// Sessions returns all sessions for the given project, sorted by update time.
func (a *Adapter) Sessions(projectRoot string) ([]adapter.Session, error) {
	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}

	workspaceDir := a.workspacePath(absPath)
	entries, err := os.ReadDir(workspaceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []adapter.Session
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		dbPath := filepath.Join(workspaceDir, e.Name(), "store.db")
		meta, err := a.readSessionMeta(dbPath)
		if err != nil {
			continue
		}

		// Get file modification time as UpdatedAt
		info, err := os.Stat(dbPath)
		updatedAt := time.Now()
		if err == nil {
			updatedAt = info.ModTime()
		}

		// Check cache before parsing messages (td-107eea24)
		// Include WAL file stats in cache key (td-8cf39632)
		var walSize, walMtime int64
		walPath := dbPath + "-wal"
		if walInfo, err := os.Stat(walPath); err == nil {
			walSize = walInfo.Size()
			walMtime = walInfo.ModTime().UnixNano()
		}

		var msgCount int
		var firstUserMsg string
		a.sessionCacheMu.RLock()
		cached, cacheHit := a.sessionCache[dbPath]
		a.sessionCacheMu.RUnlock()

		if cacheHit && info != nil &&
			cached.size == info.Size() && cached.mtime == info.ModTime().UnixNano() &&
			cached.walSize == walSize && cached.walMtime == walMtime {
			// Cache hit - reuse cached data
			msgCount = cached.messageCount
			firstUserMsg = cached.firstUserMsg
		} else {
			// Cache miss - parse messages and update cache
			messages, _ := a.parseMessages(dbPath)
			msgCount = len(messages)
			for _, msg := range messages {
				if msg.Role == "user" && msg.Content != "" {
					firstUserMsg = msg.Content
					break
				}
			}
			// Update cache (td-90a73d68: mutex protected)
			if info != nil {
				a.sessionCacheMu.Lock()
				a.sessionCache[dbPath] = sessionCacheEntry{
					size:         info.Size(),
					mtime:        info.ModTime().UnixNano(),
					walSize:      walSize,
					walMtime:     walMtime,
					messageCount: msgCount,
					firstUserMsg: firstUserMsg,
				}
				a.sessionCacheMu.Unlock()
			}
		}

		// Use first user message as name if meta.Name is empty or "New Agent"
		name := meta.Name
		if (name == "" || name == "New Agent") && firstUserMsg != "" {
			name = truncateTitle(firstUserMsg, 50)
		}
		if name == "" || name == "New Agent" {
			name = shortID(meta.AgentID)
		}

		// Calculate file size (db + wal)
		fileSize := int64(0)
		if info != nil {
			fileSize = info.Size() + walSize
		}

		sessions = append(sessions, adapter.Session{
			ID:           meta.AgentID,
			Name:         name,
			Slug:         shortID(meta.AgentID),
			AdapterID:    adapterID,
			AdapterName:  adapterName,
			AdapterIcon:  a.Icon(),
			CreatedAt:    meta.CreatedTime(),
			UpdatedAt:    updatedAt,
			Duration:     updatedAt.Sub(meta.CreatedTime()),
			IsActive:     time.Since(updatedAt) < 5*time.Minute,
			TotalTokens:  0, // Not tracked in cursor format
			EstCost:      0,
			IsSubAgent:   false,
			MessageCount: msgCount,
			FileSize:     fileSize,
		})
	}

	// Sort by UpdatedAt descending (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// Messages returns all messages for the given session.
func (a *Adapter) Messages(sessionID string) ([]adapter.Message, error) {
	dbPath := a.findSessionDB(sessionID)
	if dbPath == "" {
		return nil, nil
	}

	return a.parseMessages(dbPath)
}

// Usage returns aggregate usage stats for the given session.
// Cursor CLI doesn't track detailed token usage, so we return estimates.
func (a *Adapter) Usage(sessionID string) (*adapter.UsageStats, error) {
	messages, err := a.Messages(sessionID)
	if err != nil {
		return nil, err
	}

	stats := &adapter.UsageStats{
		MessageCount: len(messages),
	}

	// Estimate tokens from content length
	for _, m := range messages {
		chars := len(m.Content)
		stats.TotalInputTokens += chars / 4  // rough estimate
		stats.TotalOutputTokens += chars / 4 // rough estimate
	}

	return stats, nil
}

// Watch returns a channel that emits events when session data changes.
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, io.Closer, error) {
	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, nil, err
	}
	return NewWatcher(a.workspacePath(absPath))
}

// workspacePath returns the path to the workspace directory in ~/.cursor/chats.
// The workspace hash is the MD5 hash of the absolute project path.
func (a *Adapter) workspacePath(projectRoot string) string {
	hash := md5.Sum([]byte(projectRoot))
	return filepath.Join(a.chatsDir, hex.EncodeToString(hash[:]))
}

// readSessionMeta reads the session metadata from store.db.
func (a *Adapter) readSessionMeta(dbPath string) (*SessionMeta, error) {
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	sqlitePoolSettings(db) // Prevent FD leaks (td-649ba4)
	defer func() { _ = db.Close() }()

	var hexValue string
	err = db.QueryRow("SELECT value FROM meta WHERE key = '0'").Scan(&hexValue)
	if err != nil {
		return nil, err
	}

	// Decode hex-encoded JSON
	jsonBytes, err := hex.DecodeString(hexValue)
	if err != nil {
		return nil, err
	}

	var meta SessionMeta
	if err := json.Unmarshal(jsonBytes, &meta); err != nil {
		return nil, err
	}

	return &meta, nil
}

// findSessionDB finds the store.db path for a given session ID.
func (a *Adapter) findSessionDB(sessionID string) string {
	entries, err := os.ReadDir(a.chatsDir)
	if err != nil {
		return ""
	}

	for _, wsDir := range entries {
		if !wsDir.IsDir() {
			continue
		}
		dbPath := filepath.Join(a.chatsDir, wsDir.Name(), sessionID, "store.db")
		if _, err := os.Stat(dbPath); err == nil {
			return dbPath
		}
	}
	return ""
}

// parseMessages parses all messages from a session's store.db.
func (a *Adapter) parseMessages(dbPath string) ([]adapter.Message, error) {
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	sqlitePoolSettings(db) // Prevent FD leaks (td-649ba4)
	defer func() { _ = db.Close() }()

	// Read session metadata inline to avoid opening second connection (td-649ba4)
	var hexValue string
	err = db.QueryRow("SELECT value FROM meta WHERE key = '0'").Scan(&hexValue)
	if err != nil {
		return nil, err
	}
	jsonBytes, err := hex.DecodeString(hexValue)
	if err != nil {
		return nil, err
	}
	var meta SessionMeta
	if err := json.Unmarshal(jsonBytes, &meta); err != nil {
		return nil, err
	}

	// Read all blobs into a map
	blobs := make(map[string][]byte)
	rows, err := db.Query("SELECT id, data FROM blobs")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id string
		var data []byte
		if err := rows.Scan(&id, &data); err != nil {
			// Skip corrupt blobs but continue processing others
			continue
		}
		blobs[id] = data
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating blobs: %w", err)
	}

	// Traverse from root blob to collect messages
	var messages []adapter.Message
	a.collectMessages(blobs, meta.LatestRootBlobID, &messages)

	// Interpolate timestamps: use createdAt for first, file mtime for last
	if len(messages) > 0 {
		startTime := meta.CreatedTime()
		endTime := startTime

		// Get file modification time for end timestamp
		if info, err := os.Stat(dbPath); err == nil {
			endTime = info.ModTime()
		}

		// Interpolate timestamps across messages
		if len(messages) == 1 {
			messages[0].Timestamp = startTime
		} else {
			duration := endTime.Sub(startTime)
			for i := range messages {
				progress := float64(i) / float64(len(messages)-1)
				messages[i].Timestamp = startTime.Add(time.Duration(progress * float64(duration)))
			}
		}
	}

	return messages, nil
}

// collectMessages recursively collects messages from a blob tree.
func (a *Adapter) collectMessages(blobs map[string][]byte, blobID string, messages *[]adapter.Message) {
	data, ok := blobs[blobID]
	if !ok || len(data) == 0 {
		return
	}

	// Check if this blob is JSON (starts with '{')
	if data[0] == '{' {
		msg, err := a.parseMessageBlob(data, blobID)
		if err == nil && (msg.Role == "user" || msg.Role == "assistant") {
			// Skip system context messages (have system tags but no user content)
			if msg.Role == "user" && isSystemContextMessage(msg.Content) {
				return
			}
			// Skip empty messages
			if msg.Content == "" && len(msg.ToolUses) == 0 {
				return
			}
			*messages = append(*messages, msg)
		}
		return
	}

	// Otherwise, it's a linking blob with child references
	// Format: 0x0A 0x20 [32 bytes hash] repeated, optionally followed by JSON
	offset := 0
	for offset+34 <= len(data) {
		if data[offset] != 0x0A || data[offset+1] != 0x20 {
			break
		}
		childID := hex.EncodeToString(data[offset+2 : offset+34])
		a.collectMessages(blobs, childID, messages)
		offset += 34
	}

	// Check if there's embedded JSON after the references
	if offset < len(data) {
		// Skip any non-JSON prefix bytes (field tags)
		jsonStart := offset
		for jsonStart < len(data) && data[jsonStart] != '{' {
			jsonStart++
		}
		if jsonStart < len(data) {
			msg, err := a.parseMessageBlob(data[jsonStart:], blobID)
			if err == nil && (msg.Role == "user" || msg.Role == "assistant") {
				// Skip system context messages
				if msg.Role == "user" && isSystemContextMessage(msg.Content) {
					return
				}
				// Skip empty messages
				if msg.Content == "" && len(msg.ToolUses) == 0 {
					return
				}
				*messages = append(*messages, msg)
			}
		}
	}
}

// parseMessageBlob parses a single message blob into an adapter.Message.
// blobID is the database blob hash, used as the message ID for uniqueness.
// (Cursor stores all assistant messages with internal id="1", so we use blob hash instead)
func (a *Adapter) parseMessageBlob(data []byte, blobID string) (adapter.Message, error) {
	var blob MessageBlob
	if err := json.Unmarshal(data, &blob); err != nil {
		return adapter.Message{}, err
	}

	// Use blob hash as message ID for uniqueness
	// Cursor's internal id field is always "1" for assistant messages, causing cache collisions
	msgID := shortID(blobID)

	msg := adapter.Message{
		ID:   msgID,
		Role: blob.Role,
	}

	// Parse content (now returns ContentBlocks as well)
	content, toolUses, thinkingBlocks, contentBlocks, model := a.parseContent(blob.Content)
	msg.Content = content
	msg.ToolUses = toolUses
	msg.ThinkingBlocks = thinkingBlocks
	msg.ContentBlocks = contentBlocks
	msg.Model = model

	return msg, nil
}

// parseContent extracts text content, tool uses, thinking blocks, and content blocks from the content field.
// Also extracts model name from providerOptions if present.
// Returns: content string, tool uses, thinking blocks, content blocks, model name
func (a *Adapter) parseContent(rawContent json.RawMessage) (string, []adapter.ToolUse, []adapter.ThinkingBlock, []adapter.ContentBlock, string) {
	if len(rawContent) == 0 {
		return "", nil, nil, nil, ""
	}

	// Try parsing as string first (first user message format)
	var strContent string
	if err := json.Unmarshal(rawContent, &strContent); err == nil {
		// Extract user query from XML tags if present
		text := strContent
		if query := extractUserQuery(strContent); query != "" {
			text = query
		}
		contentBlocks := []adapter.ContentBlock{{Type: "text", Text: text}}
		return text, nil, nil, contentBlocks, ""
	}

	// Parse as array of content blocks (subsequent messages)
	var blocks []ContentBlock
	if err := json.Unmarshal(rawContent, &blocks); err != nil {
		// Skip malformed messages instead of crashing
		return "", nil, nil, nil, ""
	}

	var texts []string
	var toolUses []adapter.ToolUse
	var thinkingBlocks []adapter.ThinkingBlock
	var contentBlocks []adapter.ContentBlock
	var model string
	toolResultCount := 0

	for _, block := range blocks {
		// Extract model from providerOptions
		if block.ProviderOptions != nil && block.ProviderOptions.Cursor != nil {
			if block.ProviderOptions.Cursor.ModelName != "" && model == "" {
				model = block.ProviderOptions.Cursor.ModelName
			}
		}

		switch block.Type {
		case "text":
			// Extract user query from XML tags if present
			text := block.Text
			if query := extractUserQuery(block.Text); query != "" {
				text = query
			}
			texts = append(texts, text)
			contentBlocks = append(contentBlocks, adapter.ContentBlock{
				Type: "text",
				Text: text,
			})

		case "reasoning":
			tokenCount := len(block.Text) / 4 // rough estimate
			thinkingBlocks = append(thinkingBlocks, adapter.ThinkingBlock{
				Content:    block.Text,
				TokenCount: tokenCount,
			})
			contentBlocks = append(contentBlocks, adapter.ContentBlock{
				Type:       "thinking",
				Text:       block.Text,
				TokenCount: tokenCount,
			})

		case "tool-call":
			inputStr := ""
			if len(block.Args) > 0 {
				inputStr = string(block.Args)
			}
			toolUses = append(toolUses, adapter.ToolUse{
				ID:    block.ToolCallID,
				Name:  block.ToolName,
				Input: inputStr,
			})
			contentBlocks = append(contentBlocks, adapter.ContentBlock{
				Type:      "tool_use",
				ToolUseID: block.ToolCallID,
				ToolName:  block.ToolName,
				ToolInput: inputStr,
			})

		case "tool-result":
			toolResultCount++
			// Extract result content from the Result field
			resultContent := extractToolResultContent(block.Result)
			contentBlocks = append(contentBlocks, adapter.ContentBlock{
				Type:       "tool_result",
				ToolUseID:  block.ToolCallID,
				ToolOutput: resultContent,
				IsError:    block.IsError,
			})
		}
	}

	// If we have tool results but no text, show a placeholder
	content := ""
	if len(texts) > 0 {
		content = texts[0]
		for _, t := range texts[1:] {
			content += "\n" + t
		}
	}
	if content == "" && toolResultCount > 0 {
		content = fmt.Sprintf("[%d tool result(s)]", toolResultCount)
	}

	return content, toolUses, thinkingBlocks, contentBlocks, model
}

// extractToolResultContent extracts the content string from a tool result's Result field.
// The result can be a string, array of content blocks, or other JSON structure.
func extractToolResultContent(result json.RawMessage) string {
	if len(result) == 0 {
		return ""
	}

	// Try parsing as string first
	var strResult string
	if err := json.Unmarshal(result, &strResult); err == nil {
		return strResult
	}

	// Try parsing as array of content blocks (common format: [{type: "text", text: "..."}])
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(result, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}

	// Fallback: return raw JSON (truncated if too long)
	raw := string(result)
	if len(raw) > 500 {
		return raw[:497] + "..."
	}
	return raw
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

// extractUserQuery extracts the user's query from XML-tagged content.
// Returns the query text or empty string if no <user_query> tag found.
func extractUserQuery(content string) string {
	start := strings.Index(content, "<user_query>")
	end := strings.Index(content, "</user_query>")
	if start >= 0 && end > start {
		query := content[start+len("<user_query>") : end]
		return strings.TrimSpace(query)
	}
	return ""
}

// isSystemContextMessage returns true if this is the first message with system context only.
// These messages contain OS info, project layout, git status, rules but no user query.
func isSystemContextMessage(content string) bool {
	hasSystemTags := strings.Contains(content, "<user_info>") ||
		strings.Contains(content, "<project_layout>") ||
		strings.Contains(content, "<git_status>")
	hasUserQuery := strings.Contains(content, "<user_query>")
	return hasSystemTags && !hasUserQuery
}

// stripXMLTags removes XML tags from content and extracts user query if present.
func stripXMLTags(s string) string {
	// First try to extract user query
	if query := extractUserQuery(s); query != "" {
		return query
	}
	// Remove all XML tags (using pre-compiled regex for performance)
	return strings.TrimSpace(xmlTagRegex.ReplaceAllString(s, ""))
}
