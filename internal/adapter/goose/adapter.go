package goose

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
	adapterID    = "goose"
	adapterName  = "Goose"
	queryTimeout = 5 * time.Second
	msgCacheMax  = 128
)

// Adapter implements the adapter.Adapter interface for Goose sessions.
type Adapter struct {
	dbPath string
	db     *sql.DB
	dbMu   sync.Mutex
	msgMu  sync.Mutex
	msgMap map[string]messageCacheEntry
}

// New creates a new Goose adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		dbPath: findGooseDB(home),
		msgMap: make(map[string]messageCacheEntry),
	}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return adapterID }

// Name returns the human-readable adapter name.
func (a *Adapter) Name() string { return adapterName }

// Icon returns the adapter icon for badge display.
func (a *Adapter) Icon() string { return "G" }

// Capabilities returns the supported features.
func (a *Adapter) Capabilities() adapter.CapabilitySet {
	return adapter.CapabilitySet{
		adapter.CapSessions: true,
		adapter.CapMessages: true,
		adapter.CapUsage:    true,
		adapter.CapWatch:    true,
	}
}

// WatchScope returns Global because Goose uses one global SQLite DB.
func (a *Adapter) WatchScope() adapter.WatchScope {
	return adapter.WatchScopeGlobal
}

// Detect checks whether Goose sessions exist for the given project root.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	if _, err := os.Stat(a.dbPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	db, err := a.getDB()
	if err != nil {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, `SELECT DISTINCT working_dir FROM sessions`)
	if err != nil {
		return false, nil
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var workDir string
		if err := rows.Scan(&workDir); err != nil {
			continue
		}
		if cwdMatchesProject(projectRoot, workDir) {
			return true, nil
		}
	}

	return false, nil
}

// Sessions returns sessions for the given project, sorted by update time desc.
func (a *Adapter) Sessions(projectRoot string) ([]adapter.Session, error) {
	db, err := a.getDB()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
		SELECT
			s.id,
			s.name,
			s.description,
			s.session_type,
			s.working_dir,
			s.created_at,
			s.updated_at,
			s.total_tokens,
			s.input_tokens,
			s.output_tokens,
			s.accumulated_total_tokens,
			s.accumulated_input_tokens,
			s.accumulated_output_tokens,
			COUNT(m.id) as message_count
		FROM sessions s
		LEFT JOIN messages m ON m.session_id = s.id
		GROUP BY
			s.id, s.name, s.description, s.session_type, s.working_dir,
			s.created_at, s.updated_at, s.total_tokens, s.input_tokens, s.output_tokens,
			s.accumulated_total_tokens, s.accumulated_input_tokens, s.accumulated_output_tokens
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	sessions := make([]adapter.Session, 0)
	dbSize := sqliteStorageSize(a.dbPath)
	for rows.Next() {
		var (
			id                string
			name              string
			description       string
			sessionType       string
			workingDir        string
			createdAtRaw      string
			updatedAtRaw      string
			totalTokens       sql.NullInt64
			inputTokens       sql.NullInt64
			outputTokens      sql.NullInt64
			accumTotalTokens  sql.NullInt64
			accumInputTokens  sql.NullInt64
			accumOutputTokens sql.NullInt64
			messageCount      int
		)

		if err := rows.Scan(
			&id,
			&name,
			&description,
			&sessionType,
			&workingDir,
			&createdAtRaw,
			&updatedAtRaw,
			&totalTokens,
			&inputTokens,
			&outputTokens,
			&accumTotalTokens,
			&accumInputTokens,
			&accumOutputTokens,
			&messageCount,
		); err != nil {
			continue
		}

		if !cwdMatchesProject(projectRoot, workingDir) {
			continue
		}

		createdAt := parseSQLiteTime(createdAtRaw)
		updatedAt := parseSQLiteTime(updatedAtRaw)
		if createdAt.IsZero() {
			createdAt = updatedAt
		}

		sessionName := strings.TrimSpace(name)
		if sessionName == "" {
			sessionName = strings.TrimSpace(description)
		}
		if sessionName == "" {
			sessionName = shortConversationID(id)
		}

		tokTotal := chooseInt(accumTotalTokens, totalTokens)
		inTok := chooseInt(accumInputTokens, inputTokens)
		outTok := chooseInt(accumOutputTokens, outputTokens)
		if tokTotal == 0 {
			tokTotal = inTok + outTok
		}

		sessions = append(sessions, adapter.Session{
			ID:              id,
			Name:            sessionName,
			Slug:            shortConversationID(id),
			AdapterID:       adapterID,
			AdapterName:     adapterName,
			AdapterIcon:     a.Icon(),
			CreatedAt:       createdAt,
			UpdatedAt:       updatedAt,
			Duration:        updatedAt.Sub(createdAt),
			IsActive:        time.Since(updatedAt) < 5*time.Minute,
			TotalTokens:     tokTotal,
			MessageCount:    messageCount,
			FileSize:        dbSize,
			SessionCategory: toSessionCategory(sessionType),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// SessionByID returns one session without scanning all sessions.
func (a *Adapter) SessionByID(sessionID string) (*adapter.Session, error) {
	db, err := a.getDB()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	row := db.QueryRowContext(ctx, `
		SELECT
			s.id,
			s.name,
			s.description,
			s.session_type,
			s.created_at,
			s.updated_at,
			s.total_tokens,
			s.input_tokens,
			s.output_tokens,
			s.accumulated_total_tokens,
			s.accumulated_input_tokens,
			s.accumulated_output_tokens,
			COUNT(m.id) as message_count
		FROM sessions s
		LEFT JOIN messages m ON m.session_id = s.id
		WHERE s.id = ?
		GROUP BY
			s.id, s.name, s.description, s.session_type, s.created_at, s.updated_at,
			s.total_tokens, s.input_tokens, s.output_tokens,
			s.accumulated_total_tokens, s.accumulated_input_tokens, s.accumulated_output_tokens
		LIMIT 1
	`, sessionID)

	var (
		id                string
		name              string
		description       string
		sessionType       string
		createdAtRaw      string
		updatedAtRaw      string
		totalTokens       sql.NullInt64
		inputTokens       sql.NullInt64
		outputTokens      sql.NullInt64
		accumTotalTokens  sql.NullInt64
		accumInputTokens  sql.NullInt64
		accumOutputTokens sql.NullInt64
		messageCount      int
	)
	if err := row.Scan(
		&id,
		&name,
		&description,
		&sessionType,
		&createdAtRaw,
		&updatedAtRaw,
		&totalTokens,
		&inputTokens,
		&outputTokens,
		&accumTotalTokens,
		&accumInputTokens,
		&accumOutputTokens,
		&messageCount,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session %s not found", sessionID)
		}
		return nil, err
	}

	createdAt := parseSQLiteTime(createdAtRaw)
	updatedAt := parseSQLiteTime(updatedAtRaw)
	if createdAt.IsZero() {
		createdAt = updatedAt
	}

	sessionName := strings.TrimSpace(name)
	if sessionName == "" {
		sessionName = strings.TrimSpace(description)
	}
	if sessionName == "" {
		sessionName = shortConversationID(id)
	}

	tokTotal := chooseInt(accumTotalTokens, totalTokens)
	inTok := chooseInt(accumInputTokens, inputTokens)
	outTok := chooseInt(accumOutputTokens, outputTokens)
	if tokTotal == 0 {
		tokTotal = inTok + outTok
	}

	return &adapter.Session{
		ID:              id,
		Name:            sessionName,
		Slug:            shortConversationID(id),
		AdapterID:       adapterID,
		AdapterName:     adapterName,
		AdapterIcon:     a.Icon(),
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		Duration:        updatedAt.Sub(createdAt),
		IsActive:        time.Since(updatedAt) < 5*time.Minute,
		TotalTokens:     tokTotal,
		MessageCount:    messageCount,
		FileSize:        sqliteStorageSize(a.dbPath),
		SessionCategory: toSessionCategory(sessionType),
	}, nil
}

// Messages returns all messages for a Goose session.
func (a *Adapter) Messages(sessionID string) ([]adapter.Message, error) {
	db, err := a.getDB()
	if err != nil {
		return nil, err
	}

	a.ensureCache()

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	sig, err := a.messageSignature(ctx, db, sessionID)
	if err != nil {
		return nil, err
	}
	if sig.count == 0 {
		return nil, nil
	}

	a.msgMu.Lock()
	if cached, ok := a.msgMap[sessionID]; ok && cached.sig == sig {
		cached.lastAccess = time.Now()
		a.msgMap[sessionID] = cached
		msgs := copyMessages(cached.messages)
		a.msgMu.Unlock()
		return msgs, nil
	}
	a.msgMu.Unlock()

	rows, err := db.QueryContext(ctx, `
		SELECT message_id, role, content_json, created_timestamp, timestamp, metadata_json
		FROM messages
		WHERE session_id = ?
		ORDER BY created_timestamp, id
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	messages := make([]adapter.Message, 0)
	toolRefs := make(map[string]toolUseRef)

	idx := 0
	for rows.Next() {
		var (
			messageID        sql.NullString
			role             string
			contentJSON      string
			createdTimestamp sql.NullInt64
			timestampRaw     string
			metadataJSON     sql.NullString
		)
		if err := rows.Scan(&messageID, &role, &contentJSON, &createdTimestamp, &timestampRaw, &metadataJSON); err != nil {
			return nil, err
		}

		blocks, err := parseGooseBlocks(contentJSON)
		if err != nil {
			return nil, fmt.Errorf("parse goose message %q: %w", messageID.String, err)
		}

		msgID := messageID.String
		if msgID == "" {
			msgID = fmt.Sprintf("%s:%d", sessionID, idx)
		}

		ts := time.Time{}
		if createdTimestamp.Valid {
			ts = time.Unix(createdTimestamp.Int64, 0)
		}
		if ts.IsZero() {
			ts = parseSQLiteTime(timestampRaw)
		}

		contentParts := make([]string, 0)
		contentBlocks := make([]adapter.ContentBlock, 0)
		thinkingBlocks := make([]adapter.ThinkingBlock, 0)
		toolUses := make([]adapter.ToolUse, 0)
		currentToolIdx := make(map[string]int)

		for _, b := range blocks {
			switch strings.ToLower(b.Type) {
			case "text":
				text := strings.TrimSpace(b.Text)
				if text == "" {
					continue
				}
				contentParts = append(contentParts, text)
				contentBlocks = append(contentBlocks, adapter.ContentBlock{Type: "text", Text: text})

			case "thinking", "reasoning":
				text := strings.TrimSpace(b.Thinking)
				if text == "" {
					text = strings.TrimSpace(b.Text)
				}
				if text == "" {
					continue
				}
				thinkingBlocks = append(thinkingBlocks, adapter.ThinkingBlock{
					Content:    text,
					TokenCount: maxInt(1, len(text)/4),
				})
				contentBlocks = append(contentBlocks, adapter.ContentBlock{Type: "thinking", Text: text, TokenCount: maxInt(1, len(text)/4)})

			case "toolrequest", "frontendtoolrequest":
				useID := b.ID
				if useID == "" {
					useID = fmt.Sprintf("tool_%s_%d", msgID, len(toolUses))
				}
				toolName, toolInput := parseToolCall(b.ToolCall)
				if toolName == "" {
					toolName = "tool"
				}
				toolUses = append(toolUses, adapter.ToolUse{ID: useID, Name: toolName, Input: toolInput})
				toolRef := toolUseRef{msgIdx: len(messages), toolIdx: len(toolUses) - 1}
				toolRefs[useID] = toolRef
				currentToolIdx[useID] = len(toolUses) - 1
				contentBlocks = append(contentBlocks, adapter.ContentBlock{
					Type:      "tool_use",
					ToolUseID: useID,
					ToolName:  toolName,
					ToolInput: toolInput,
				})
				if toolName != "" {
					contentParts = append(contentParts, "[Tool: "+toolName+"]")
				}

			case "toolresponse":
				useID := b.ID
				output, isErr := parseToolResult(b.ToolResult)
				contentBlocks = append(contentBlocks, adapter.ContentBlock{
					Type:       "tool_result",
					ToolUseID:  useID,
					ToolOutput: output,
					IsError:    isErr,
				})
				if output != "" {
					contentParts = append(contentParts, "[Tool Result] "+output)
				}
				if idx, ok := currentToolIdx[useID]; ok && idx < len(toolUses) {
					toolUses[idx].Output = output
				}
				if ref, ok := toolRefs[useID]; ok && ref.msgIdx < len(messages) && ref.toolIdx < len(messages[ref.msgIdx].ToolUses) {
					messages[ref.msgIdx].ToolUses[ref.toolIdx].Output = output
					messages[ref.msgIdx].ContentBlocks = append(messages[ref.msgIdx].ContentBlocks, adapter.ContentBlock{
						Type:       "tool_result",
						ToolUseID:  useID,
						ToolOutput: output,
						IsError:    isErr,
					})
				}
			}
		}

		if len(contentParts) == 0 && len(contentBlocks) == 0 {
			continue
		}

		model := ""
		if metadataJSON.Valid {
			model = extractModelFromMetadata(metadataJSON.String)
		}

		messages = append(messages, adapter.Message{
			ID:             msgID,
			Role:           normalizeRole(role),
			Content:        strings.Join(contentParts, "\n"),
			Timestamp:      ts,
			Model:          model,
			ToolUses:       toolUses,
			ThinkingBlocks: thinkingBlocks,
			ContentBlocks:  contentBlocks,
		})
		idx++
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	a.msgMu.Lock()
	a.msgMap[sessionID] = messageCacheEntry{
		sig:        sig,
		messages:   copyMessages(messages),
		lastAccess: time.Now(),
	}
	a.evictCacheLocked()
	a.msgMu.Unlock()

	return messages, nil
}

// Usage returns aggregate usage stats for a session.
func (a *Adapter) Usage(sessionID string) (*adapter.UsageStats, error) {
	db, err := a.getDB()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	query := `
		SELECT
			COALESCE(accumulated_input_tokens, input_tokens, 0),
			COALESCE(accumulated_output_tokens, output_tokens, 0),
			(SELECT COUNT(*) FROM messages WHERE session_id = ?)
		FROM sessions
		WHERE id = ?
		LIMIT 1
	`

	var inTok, outTok, msgCount int
	err = db.QueryRowContext(ctx, query, sessionID, sessionID).Scan(&inTok, &outTok, &msgCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &adapter.UsageStats{
		TotalInputTokens:  inTok,
		TotalOutputTokens: outTok,
		MessageCount:      msgCount,
	}, nil
}

// Watch watches Goose DB and WAL changes.
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, io.Closer, error) {
	if _, err := os.Stat(a.dbPath); err != nil {
		return nil, nil, err
	}
	src, closer, err := NewWatcher(a.dbPath)
	if err != nil {
		return nil, nil, err
	}

	out := make(chan adapter.Event, 32)
	go func() {
		defer close(out)
		for evt := range src {
			if evt.SessionID == "" {
				if id, err := a.latestChangedSessionID(); err == nil {
					evt.SessionID = id
				}
			}
			select {
			case out <- evt:
			default:
			}
		}
	}()
	return out, closer, nil
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

	connStr := a.dbPath + "?mode=ro&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	db.SetConnMaxLifetime(time.Minute)

	a.db = db
	return a.db, nil
}

func findGooseDB(home string) string {
	candidates := gooseDBCandidates(home)
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return filepath.Join(home, ".local", "share", "goose", "sessions.db")
}

func gooseDBCandidates(home string) []string {
	candidates := make([]string, 0, 8)

	if root := strings.TrimSpace(os.Getenv("GOOSE_PATH_ROOT")); root != "" {
		candidates = append(candidates, filepath.Join(root, "data", "sessions.db"))
	}

	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			filepath.Join(home, "Library", "Application Support", "Block", "goose", "sessions.db"),
			filepath.Join(home, "Library", "Application Support", "goose", "sessions.db"),
		)
	case "linux":
		xdgData := os.Getenv("XDG_DATA_HOME")
		if xdgData == "" {
			xdgData = filepath.Join(home, ".local", "share")
		}
		candidates = append(candidates, filepath.Join(xdgData, "goose", "sessions.db"))
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			candidates = append(candidates, filepath.Join(appData, "goose", "sessions.db"))
		}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			candidates = append(candidates, filepath.Join(local, "goose", "sessions.db"))
		}
	}

	fallback := filepath.Join(home, ".local", "share", "goose", "sessions.db")
	if !containsPath(candidates, fallback) {
		candidates = append(candidates, fallback)
	}

	return candidates
}

func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

func chooseInt(primary, fallback sql.NullInt64) int {
	if primary.Valid {
		return int(primary.Int64)
	}
	if fallback.Valid {
		return int(fallback.Int64)
	}
	return 0
}

func parseSQLiteTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

func resolveProjectPath(projectRoot string) string {
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return projectRoot
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs)
}

func cwdMatchesProject(projectRoot, cwd string) bool {
	projectAbs := resolveProjectPath(projectRoot)
	cwdAbs := resolveProjectPath(cwd)

	if cwdAbs == projectAbs {
		return true
	}
	prefix := projectAbs + string(os.PathSeparator)
	return strings.HasPrefix(cwdAbs, prefix)
}

func shortConversationID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func normalizeRole(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	switch r {
	case "assistant", "user", "system", "tool":
		return r
	default:
		if r == "" {
			return "assistant"
		}
		return r
	}
}

func toSessionCategory(sessionType string) string {
	switch strings.ToLower(strings.TrimSpace(sessionType)) {
	case "scheduled":
		return adapter.SessionCategoryCron
	case "hidden", "gateway", "terminal":
		return adapter.SessionCategorySystem
	default:
		return adapter.SessionCategoryInteractive
	}
}

func parseGooseBlocks(contentJSON string) ([]GooseContentBlock, error) {
	var blocks []GooseContentBlock
	if err := json.Unmarshal([]byte(contentJSON), &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

func parseToolCall(call ToolResultValue) (name string, input string) {
	if call.Status != "success" {
		return "", ""
	}
	name = strings.TrimSpace(call.Value.Name)
	if len(call.Value.Arguments) > 0 {
		if raw, err := json.Marshal(call.Value.Arguments); err == nil {
			input = string(raw)
		}
	}
	return name, input
}

func parseToolResult(result ToolResponseValue) (output string, isErr bool) {
	if strings.EqualFold(result.Status, "error") {
		return strings.TrimSpace(result.Error), true
	}
	if !strings.EqualFold(result.Status, "success") {
		return "", false
	}

	parts := make([]string, 0, len(result.Value.Content))
	for _, c := range result.Value.Content {
		text := strings.TrimSpace(c.Text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n"), false
}

func extractModelFromMetadata(metadataJSON string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &raw); err != nil {
		return ""
	}
	candidates := []string{"model", "model_name", "modelName", "provider_model", "providerModel"}
	for _, key := range candidates {
		if v, ok := raw[key].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type toolUseRef struct {
	msgIdx  int
	toolIdx int
}

type messageCacheSignature struct {
	maxID    int64
	count    int64
	maxStamp int64
}

type messageCacheEntry struct {
	sig        messageCacheSignature
	messages   []adapter.Message
	lastAccess time.Time
}

func (a *Adapter) messageSignature(ctx context.Context, db *sql.DB, sessionID string) (messageCacheSignature, error) {
	var sig messageCacheSignature
	err := db.QueryRowContext(ctx, `
		SELECT
			COALESCE(MAX(id), 0),
			COUNT(*),
			COALESCE(MAX(created_timestamp), 0)
		FROM messages
		WHERE session_id = ?
	`, sessionID).Scan(&sig.maxID, &sig.count, &sig.maxStamp)
	return sig, err
}

func copyMessages(src []adapter.Message) []adapter.Message {
	if len(src) == 0 {
		return nil
	}
	out := make([]adapter.Message, len(src))
	for i := range src {
		out[i] = src[i]
		if src[i].ToolUses != nil {
			out[i].ToolUses = append([]adapter.ToolUse(nil), src[i].ToolUses...)
		}
		if src[i].ThinkingBlocks != nil {
			out[i].ThinkingBlocks = append([]adapter.ThinkingBlock(nil), src[i].ThinkingBlocks...)
		}
		if src[i].ContentBlocks != nil {
			out[i].ContentBlocks = append([]adapter.ContentBlock(nil), src[i].ContentBlocks...)
		}
	}
	return out
}

func (a *Adapter) ensureCache() {
	a.msgMu.Lock()
	defer a.msgMu.Unlock()
	if a.msgMap == nil {
		a.msgMap = make(map[string]messageCacheEntry)
	}
}

func (a *Adapter) evictCacheLocked() {
	if len(a.msgMap) <= msgCacheMax {
		return
	}

	type pair struct {
		id string
		at time.Time
	}
	items := make([]pair, 0, len(a.msgMap))
	for id, ent := range a.msgMap {
		items = append(items, pair{id: id, at: ent.lastAccess})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].at.Before(items[j].at)
	})
	excess := len(a.msgMap) - msgCacheMax
	for i := 0; i < excess; i++ {
		delete(a.msgMap, items[i].id)
	}
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

func (a *Adapter) latestChangedSessionID() (string, error) {
	db, err := a.getDB()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var id string
	// Prefer the latest written message's session, which is usually the true changed session.
	err = db.QueryRowContext(ctx, `
		SELECT session_id
		FROM messages
		ORDER BY id DESC
		LIMIT 1
	`).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	// Fallback when there are no messages yet.
	err = db.QueryRowContext(ctx, `
		SELECT id
		FROM sessions
		ORDER BY updated_at DESC
		LIMIT 1
	`).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}
