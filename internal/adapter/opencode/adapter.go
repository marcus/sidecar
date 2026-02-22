package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
	_ "github.com/mattn/go-sqlite3"
)

const (
	adapterID           = "opencode"
	adapterName         = "OpenCode"
	metaCacheMaxEntries = 2048
	queryTimeout        = 5 * time.Second
)

// Adapter implements the adapter.Adapter interface for OpenCode sessions.
type Adapter struct {
	storageDir     string
	dbPath         string
	projectIndex   map[string]*Project // worktree path -> Project
	projectsLoaded bool                // true after loadProjects populates projectIndex
	metaCache      map[string]sessionMetaCacheEntry
	metaMu         sync.RWMutex // guards metaCache
	db             *sql.DB
	dbMu           sync.Mutex // guards db
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
	storageDir := findOpenCodeStorageDir(home)
	dbPath := findOpenCodeDBPath(home)
	return &Adapter{
		storageDir:   storageDir,
		dbPath:       dbPath,
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}
}

// findOpenCodeStorageDir searches candidate paths for the OpenCode storage directory.
// Returns the first path that exists, or the primary default if none found.
func findOpenCodeStorageDir(home string) string {
	candidates := openCodeStorageCandidates(home)
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return filepath.Join(home, ".local", "share", "opencode", "storage")
}

// findOpenCodeDBPath searches candidate paths for the OpenCode SQLite database.
// Returns the first path that exists, or the primary default if none found.
func findOpenCodeDBPath(home string) string {
	candidates := openCodeDBCandidates(home)
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

// openCodeStorageCandidates returns platform-ordered candidate paths for OpenCode storage.
// Upstream bug #8235: currently uses ~/.local/share on all platforms. PR #8236 will fix
// to use platform-native paths. We check both to handle pre- and post-fix versions.
func openCodeStorageCandidates(home string) []string {
	var candidates []string

	switch runtime.GOOS {
	case "darwin":
		// Platform-native (post-PR #8236)
		candidates = append(candidates, filepath.Join(home, "Library", "Application Support", "opencode", "storage"))
	case "linux":
		xdgData := os.Getenv("XDG_DATA_HOME")
		if xdgData == "" {
			xdgData = filepath.Join(home, ".local", "share")
		}
		candidates = append(candidates, filepath.Join(xdgData, "opencode", "storage"))
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			candidates = append(candidates, filepath.Join(localAppData, "opencode", "Data", "storage"))
		}
	}

	// Current default (pre-fix): ~/.local/share/opencode/storage on all platforms
	defaultPath := filepath.Join(home, ".local", "share", "opencode", "storage")
	// Only add if not already in candidates (avoid duplicate on Linux with default XDG)
	if len(candidates) == 0 || candidates[len(candidates)-1] != defaultPath {
		candidates = append(candidates, defaultPath)
	}

	return candidates
}

// openCodeDBCandidates returns platform-ordered candidate paths for OpenCode DB.
func openCodeDBCandidates(home string) []string {
	var candidates []string

	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates, filepath.Join(home, "Library", "Application Support", "opencode", "opencode.db"))
	case "linux":
		xdgData := os.Getenv("XDG_DATA_HOME")
		if xdgData == "" {
			xdgData = filepath.Join(home, ".local", "share")
		}
		candidates = append(candidates, filepath.Join(xdgData, "opencode", "opencode.db"))
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			candidates = append(candidates, filepath.Join(localAppData, "opencode", "Data", "opencode.db"))
		}
	}

	defaultPath := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	if len(candidates) == 0 || candidates[len(candidates)-1] != defaultPath {
		candidates = append(candidates, defaultPath)
	}

	return candidates
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return adapterID }

// Name returns the human-readable adapter name.
func (a *Adapter) Name() string { return adapterName }

// Icon returns the adapter icon for badge display.
func (a *Adapter) Icon() string { return "◇" }

func (a *Adapter) useSQLite() bool {
	if strings.TrimSpace(a.dbPath) == "" {
		return false
	}
	info, err := os.Stat(a.dbPath)
	return err == nil && !info.IsDir()
}

func (a *Adapter) getDB() (*sql.DB, error) {
	a.dbMu.Lock()
	defer a.dbMu.Unlock()

	if a.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		err := a.db.PingContext(ctx)
		cancel()
		if err == nil {
			return a.db, nil
		}
		_ = a.db.Close()
		a.db = nil
	}

	if strings.TrimSpace(a.dbPath) == "" {
		return nil, fmt.Errorf("opencode db path not configured")
	}
	connStr := a.dbPath + "?mode=ro&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Minute)

	a.db = db
	return a.db, nil
}

// Detect checks if OpenCode sessions exist for the given project.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	if a.useSQLite() {
		found, err := a.detectSQLite(projectRoot)
		if err == nil && found {
			return true, nil
		}
		// Fall back to legacy JSON storage if SQLite detection fails or finds no project.
	}
	return a.detectJSON(projectRoot)
}

func (a *Adapter) detectJSON(projectRoot string) (bool, error) {
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
	if a.useSQLite() {
		sessions, err := a.sessionsSQLite(projectRoot)
		if err == nil && len(sessions) > 0 {
			return sessions, nil
		}
		// Keep legacy compatibility when SQLite is present but unreadable/incomplete.
	}
	return a.sessionsJSON(projectRoot)
}

func (a *Adapter) sessionsJSON(projectRoot string) ([]adapter.Session, error) {
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
			FileSize:     info.Size(), // Session metadata file size (OpenCode uses separate message files)
			Path:         path,        // td-dca6fe: tiered watching needs session file path
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
	if a.useSQLite() {
		messages, err := a.messagesSQLite(sessionID)
		if err == nil && len(messages) > 0 {
			return messages, nil
		}
	}
	return a.messagesJSON(sessionID)
}

func (a *Adapter) messagesJSON(sessionID string) ([]adapter.Message, error) {
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
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, io.Closer, error) {
	if a.useSQLite() {
		return NewDBWatcher(a.dbPath)
	}
	projectID, err := a.findProjectID(projectRoot)
	if err != nil {
		return nil, nil, err
	}
	if projectID == "" {
		return nil, nil, fmt.Errorf("no OpenCode project found for %s", projectRoot)
	}

	sessionDir := filepath.Join(a.storageDir, "session", projectID)
	return NewWatcher(sessionDir)
}

// WatchScope returns Global when SQLite storage is used and Project for legacy JSON.
func (a *Adapter) WatchScope() adapter.WatchScope {
	if a.useSQLite() {
		return adapter.WatchScopeGlobal
	}
	return adapter.WatchScopeProject
}

func normalizePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

func sqliteStorageSize(dbPath string) int64 {
	var total int64
	if info, err := os.Stat(dbPath); err == nil {
		total += info.Size()
	}
	if info, err := os.Stat(dbPath + "-wal"); err == nil {
		total += info.Size()
	}
	return total
}

func (a *Adapter) findProjectIDSQLite(projectRoot string) (string, error) {
	projectRoot = normalizePath(projectRoot)
	if projectRoot == "" {
		return "", nil
	}

	db, err := a.getDB()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, `SELECT id, worktree FROM project`)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			id       string
			worktree sql.NullString
		)
		if err := rows.Scan(&id, &worktree); err != nil {
			continue
		}
		if !worktree.Valid || worktree.String == "/" {
			continue
		}
		if normalizePath(worktree.String) == projectRoot {
			return id, nil
		}
	}
	return "", nil
}

func (a *Adapter) detectSQLite(projectRoot string) (bool, error) {
	projectID, err := a.findProjectIDSQLite(projectRoot)
	if err != nil || projectID == "" {
		return false, err
	}

	db, err := a.getDB()
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	var exists int
	err = db.QueryRowContext(ctx, `SELECT 1 FROM session WHERE project_id = ? LIMIT 1`, projectID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (a *Adapter) sessionsSQLite(projectRoot string) ([]adapter.Session, error) {
	projectID, err := a.findProjectIDSQLite(projectRoot)
	if err != nil || projectID == "" {
		return nil, err
	}

	db, err := a.getDB()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
		SELECT
			s.id,
			s.title,
			s.parent_id,
			s.time_created,
			s.time_updated,
			(SELECT COUNT(*) FROM message m WHERE m.session_id = s.id) AS msg_count
		FROM session s
		WHERE s.project_id = ?
		ORDER BY s.time_updated DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	fileSize := sqliteStorageSize(a.dbPath)
	var sessions []adapter.Session
	for rows.Next() {
		var (
			id        string
			title     sql.NullString
			parentID  sql.NullString
			createdMS int64
			updatedMS int64
			msgCount  int
		)
		if err := rows.Scan(&id, &title, &parentID, &createdMS, &updatedMS, &msgCount); err != nil {
			continue
		}

		name := strings.TrimSpace(title.String)
		if name == "" {
			name = shortID(id)
		}

		createdAt := time.UnixMilli(createdMS).Local()
		updatedAt := time.UnixMilli(updatedMS).Local()
		if createdMS == 0 {
			createdAt = time.Time{}
		}
		if updatedMS == 0 {
			updatedAt = createdAt
		}

		duration := time.Duration(0)
		if !createdAt.IsZero() && !updatedAt.IsZero() && updatedAt.After(createdAt) {
			duration = updatedAt.Sub(createdAt)
		}

		sessions = append(sessions, adapter.Session{
			ID:           id,
			Name:         name,
			AdapterID:    adapterID,
			AdapterName:  adapterName,
			AdapterIcon:  a.Icon(),
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
			Duration:     duration,
			IsActive:     !updatedAt.IsZero() && time.Since(updatedAt) < 5*time.Minute,
			IsSubAgent:   parentID.Valid && strings.TrimSpace(parentID.String) != "",
			MessageCount: msgCount,
			FileSize:     fileSize,
		})
	}
	return sessions, nil
}

func appendParsedPart(parts parsedParts, part Part) parsedParts {
	switch part.Type {
	case "text":
		if part.Text != "" {
			parts.content = strings.TrimSpace(strings.Join([]string{parts.content, part.Text}, "\n"))
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
		parts.toolUses = append(parts.toolUses, tu)
	case "file":
		if part.Filename != "" {
			parts.fileRefs = append(parts.fileRefs, part.Filename)
		}
	case "patch":
		parts.patchFiles = append(parts.patchFiles, part.Files...)
	}
	return parts
}

func (a *Adapter) messagesSQLite(sessionID string) ([]adapter.Message, error) {
	db, err := a.getDB()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	type messageRow struct {
		ID        string
		SessionID string
		CreatedMS int64
		UpdatedMS int64
		Data      []byte
	}

	msgRows, err := db.QueryContext(ctx, `
		SELECT id, session_id, time_created, time_updated, data
		FROM message
		WHERE session_id = ?
		ORDER BY time_created ASC, id ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = msgRows.Close() }()

	rows := make([]messageRow, 0, 64)
	for msgRows.Next() {
		var row messageRow
		if err := msgRows.Scan(&row.ID, &row.SessionID, &row.CreatedMS, &row.UpdatedMS, &row.Data); err != nil {
			continue
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	partRows, err := db.QueryContext(ctx, `
		SELECT id, message_id, session_id, data
		FROM part
		WHERE session_id = ?
		ORDER BY message_id ASC, id ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = partRows.Close() }()

	partsMap := make(map[string]parsedParts)
	for partRows.Next() {
		var (
			partID    string
			messageID string
			partSID   string
			data      []byte
		)
		if err := partRows.Scan(&partID, &messageID, &partSID, &data); err != nil {
			continue
		}

		var part Part
		if err := json.Unmarshal(data, &part); err != nil {
			continue
		}
		part.ID = partID
		part.MessageID = messageID
		part.SessionID = partSID

		current := partsMap[messageID]
		partsMap[messageID] = appendParsedPart(current, part)
	}

	messages := make([]adapter.Message, 0, len(rows))
	for _, row := range rows {
		var msg Message
		if err := json.Unmarshal(row.Data, &msg); err != nil {
			continue
		}
		msg.ID = row.ID
		if msg.SessionID == "" {
			msg.SessionID = row.SessionID
		}
		if msg.Time.Created == 0 {
			msg.Time.Created = row.CreatedMS
		}
		if msg.Time.Updated == 0 {
			msg.Time.Updated = row.UpdatedMS
		}

		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}

		parts := partsMap[msg.ID]
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
		if adapterMsg.Timestamp.IsZero() && row.CreatedMS > 0 {
			adapterMsg.Timestamp = time.UnixMilli(row.CreatedMS).Local()
		}
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

	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})
	return messages, nil
}

func (a *Adapter) discoverRelatedProjectDirsSQLite(mainWorktreePath string) ([]string, error) {
	absMain := normalizePath(mainWorktreePath)
	repoName := filepath.Base(absMain)
	if repoName == "" || repoName == "." || repoName == "/" {
		return nil, nil
	}

	db, err := a.getDB()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, `SELECT worktree FROM project`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var related []string
	seen := make(map[string]struct{})
	for rows.Next() {
		var worktree sql.NullString
		if err := rows.Scan(&worktree); err != nil || !worktree.Valid {
			continue
		}
		cleaned := normalizePath(worktree.String)
		if cleaned == "" || cleaned == "/" {
			continue
		}
		base := filepath.Base(cleaned)
		if base == repoName || strings.HasPrefix(base, repoName+"-") {
			if _, ok := seen[cleaned]; !ok {
				seen[cleaned] = struct{}{}
				related = append(related, cleaned)
			}
		}
	}
	return related, nil
}

// findProjectID finds the OpenCode project ID for the given project root.
// It tries an exact match first, then checks if projectRoot is a subdirectory
// of any known project worktree. This handles bare-repo layouts where sidecar's
// ProjectRoot may be .bare/ inside the OpenCode-registered worktree directory.
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
	if !a.projectsLoaded {
		if err := a.loadProjects(); err != nil {
			return "", err
		}
	}

	// Exact match (covers worktree path and sandbox paths)
	if proj, ok := a.projectIndex[absRoot]; ok {
		return proj.ID, nil
	}

	// Subdirectory match: check if absRoot is inside a known project directory.
	// This handles bare-repo worktree layouts where git reports the main worktree
	// as .bare/ (e.g., /repo/.bare) but OpenCode registers the parent (/repo).
	for indexedPath, proj := range a.projectIndex {
		if strings.HasPrefix(absRoot, indexedPath+string(filepath.Separator)) {
			return proj.ID, nil
		}
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

		// Index sandbox paths as well
		for _, sandbox := range proj.Sandboxes {
			sbNorm := sandbox
			if resolved, err := filepath.EvalSymlinks(sbNorm); err == nil {
				sbNorm = resolved
			}
			sbNorm = filepath.Clean(sbNorm)
			if sbNorm != worktree {
				a.projectIndex[sbNorm] = &projCopy
			}
		}
	}

	a.projectsLoaded = true
	return nil
}

// DiscoverRelatedProjectDirs scans OpenCode project files for worktree paths
// related to the given main worktree path. This finds conversations from deleted
// worktrees by checking if stored worktree paths share the same repository base name.
func (a *Adapter) DiscoverRelatedProjectDirs(mainWorktreePath string) ([]string, error) {
	if a.useSQLite() {
		related, err := a.discoverRelatedProjectDirsSQLite(mainWorktreePath)
		if err == nil && len(related) > 0 {
			return related, nil
		}
		// Fall back to legacy project files if DB discovery fails.
	}

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
// Uses write lock for cache hits to safely update lastAccess (td-fdc81225).
func (a *Adapter) sessionMetadata(path string, info os.FileInfo, projectID string) (*SessionMetadata, error) {
	now := time.Now()

	// Use write lock since we update lastAccess on cache hit (td-fdc81225)
	a.metaMu.Lock()
	if entry, ok := a.metaCache[path]; ok && entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
		// Update lastAccess and return copy to prevent caller mutations
		entry.lastAccess = now
		a.metaCache[path] = entry
		metaCopy := *entry.meta
		a.metaMu.Unlock()
		return &metaCopy, nil
	}
	a.metaMu.Unlock()

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
