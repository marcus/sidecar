package kiro

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
	_ "github.com/mattn/go-sqlite3"
)

const (
	adapterID    = "kiro"
	adapterName  = "Kiro"
	queryTimeout = 5 * time.Second
)

// Adapter implements the adapter.Adapter interface for Kiro CLI sessions.
type Adapter struct {
	dbPath string
	db     *sql.DB
	dbMu   sync.Mutex
}

// New creates a new Kiro adapter.
func New() *Adapter {
	home, _ := os.UserHomeDir()
	dbPath := findKiroDB(home)
	return &Adapter{
		dbPath: dbPath,
	}
}

// findKiroDB searches candidate paths for the kiro-cli SQLite database.
// Returns the first path that exists, or the primary platform default if none found.
func findKiroDB(home string) string {
	candidates := kiroDBCandidates(home)
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return filepath.Join(home, ".kiro", "data.sqlite3")
}

// kiroDBCandidates returns platform-ordered candidate paths for the kiro-cli database.
func kiroDBCandidates(home string) []string {
	var candidates []string

	// Newer kiro-cli versions (post-.amazonq migration) — all platforms
	candidates = append(candidates, filepath.Join(home, ".kiro", "data.sqlite3"))

	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates, filepath.Join(home, "Library", "Application Support", "kiro-cli", "data.sqlite3"))
	case "linux":
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		candidates = append(candidates, filepath.Join(dataHome, "kiro-cli", "data.sqlite3"))
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		candidates = append(candidates, filepath.Join(configHome, "kiro-cli", "data.sqlite3"))
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			candidates = append(candidates, filepath.Join(appData, "kiro-cli", "data.sqlite3"))
		}
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			candidates = append(candidates, filepath.Join(localAppData, "kiro-cli", "data.sqlite3"))
		}
	}

	// Legacy: ~/.amazonq/data.sqlite3 (pre-migration)
	candidates = append(candidates, filepath.Join(home, ".amazonq", "data.sqlite3"))

	return candidates
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return adapterID }

// Name returns the human-readable adapter name.
func (a *Adapter) Name() string { return adapterName }

// Icon returns the adapter icon for badge display.
func (a *Adapter) Icon() string { return "\u03ba" } // Greek kappa

// Capabilities returns the supported features.
func (a *Adapter) Capabilities() adapter.CapabilitySet {
	return adapter.CapabilitySet{
		adapter.CapSessions: true,
		adapter.CapMessages: true,
		adapter.CapUsage:    true,
		adapter.CapWatch:    true,
	}
}

// Detect checks if Kiro CLI sessions exist for the given project.
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

	query := `SELECT 1 FROM conversations_v2 WHERE key = ? LIMIT 1`
	projectAbs := resolveProjectPath(projectRoot)

	var exists int
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	err = db.QueryRowContext(ctx, query, projectAbs).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, nil
	}

	return true, nil
}

// Sessions returns all sessions for the given project, sorted by update time.
func (a *Adapter) Sessions(projectRoot string) ([]adapter.Session, error) {
	db, err := a.getDB()
	if err != nil {
		return nil, err
	}

	projectAbs := resolveProjectPath(projectRoot)

	query := `
		SELECT conversation_id, value, created_at, updated_at
		FROM conversations_v2
		WHERE key = ?
		ORDER BY updated_at DESC
	`

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	rows, err := db.QueryContext(ctx, query, projectAbs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var sessions []adapter.Session
	for rows.Next() {
		var (
			convID    string
			valueJSON string
			createdMs int64
			updatedMs int64
		)

		if err := rows.Scan(&convID, &valueJSON, &createdMs, &updatedMs); err != nil {
			continue
		}

		createdAt := time.UnixMilli(createdMs)
		updatedAt := time.UnixMilli(updatedMs)

		// Parse conversation value for session metadata
		var conv ConversationValue
		name := shortConversationID(convID)
		msgCount := 0
		if err := json.Unmarshal([]byte(valueJSON), &conv); err == nil {
			// Extract first prompt as session name
			if promptText := firstPromptText(conv.History); promptText != "" {
				name = truncateText(promptText, 50)
			}
			// Count user prompt messages (not tool results)
			for _, entry := range conv.History {
				if isPromptEntry(entry) {
					msgCount++
				}
			}
		}

		isActive := time.Since(updatedAt) < 5*time.Minute

		// Path not set: Kiro uses a global SQLite DB, watched via WatchScopeGlobal
		sessions = append(sessions, adapter.Session{
			ID:           convID,
			Name:         name,
			Slug:         shortConversationID(convID),
			AdapterID:    adapterID,
			AdapterName:  adapterName,
			AdapterIcon:  a.Icon(),
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
			Duration:     updatedAt.Sub(createdAt),
			IsActive:     isActive,
			MessageCount: msgCount,
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

// Messages returns all messages for the given session.
func (a *Adapter) Messages(sessionID string) ([]adapter.Message, error) {
	db, err := a.getDB()
	if err != nil {
		return nil, err
	}

	query := `SELECT value FROM conversations_v2 WHERE conversation_id = ? LIMIT 1`
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	var valueJSON string
	err = db.QueryRowContext(ctx, query, sessionID).Scan(&valueJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var conv ConversationValue
	if err := json.Unmarshal([]byte(valueJSON), &conv); err != nil {
		return nil, err
	}

	var messages []adapter.Message
	msgIdx := 0

	for i, entry := range conv.History {
		if entry.User == nil {
			continue
		}

		ts := parseTimestamp(entry.User.Timestamp)

		// Determine model from model_info
		model := ""
		if conv.ModelInfo != nil && conv.ModelInfo.ModelID != "" {
			model = conv.ModelInfo.ModelID
		}

		// Check if user content is a Prompt
		if isPromptEntry(entry) {
			promptText := extractPromptText(entry.User.Content)
			if promptText != "" {
				messages = append(messages, adapter.Message{
					ID:        sessionID + "-user-" + strconv.Itoa(msgIdx),
					Role:      "user",
					Content:   promptText,
					Timestamp: ts,
					Model:     model,
				})
				msgIdx++
			}
		}
		// Skip ToolUseResults user entries - they are continuations

		// Parse assistant response
		if entry.Assistant != nil {
			aMsg, toolUses := parseAssistantMessage(entry.Assistant, sessionID, msgIdx, ts, model)
			if aMsg != nil {
				// If assistant used tools, look ahead for tool results
				if len(toolUses) > 0 && i+1 < len(conv.History) {
					nextEntry := conv.History[i+1]
					if nextEntry.User != nil {
						linkToolResults(toolUses, nextEntry.User.Content)
					}
				}
				aMsg.ToolUses = toolUses
				messages = append(messages, *aMsg)
				msgIdx++
			}
		}
	}

	return messages, nil
}

// Usage returns aggregate usage stats for the given session.
func (a *Adapter) Usage(sessionID string) (*adapter.UsageStats, error) {
	db, err := a.getDB()
	if err != nil {
		return nil, err
	}

	query := `SELECT value FROM conversations_v2 WHERE conversation_id = ? LIMIT 1`
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	var valueJSON string
	err = db.QueryRowContext(ctx, query, sessionID).Scan(&valueJSON)
	if err == sql.ErrNoRows {
		return &adapter.UsageStats{}, nil
	}
	if err != nil {
		return nil, err
	}

	var conv ConversationValue
	if err := json.Unmarshal([]byte(valueJSON), &conv); err != nil {
		return &adapter.UsageStats{}, nil
	}

	stats := &adapter.UsageStats{}
	for _, entry := range conv.History {
		if isPromptEntry(entry) {
			stats.MessageCount++
		}
	}

	return stats, nil
}

// Watch returns a channel that emits events when session data changes.
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, io.Closer, error) {
	return NewWatcher(a.dbPath)
}

// WatchScope returns Global because kiro watches a global database file.
func (a *Adapter) WatchScope() adapter.WatchScope {
	return adapter.WatchScopeGlobal
}

// getDB returns a persistent database connection, creating one if needed.
func (a *Adapter) getDB() (*sql.DB, error) {
	a.dbMu.Lock()
	defer a.dbMu.Unlock()

	if a.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
	db.SetMaxIdleConns(1)

	a.db = db
	return a.db, nil
}

// Close closes the persistent database connection.
func (a *Adapter) Close() error {
	a.dbMu.Lock()
	defer a.dbMu.Unlock()

	if a.db != nil {
		err := a.db.Close()
		a.db = nil
		return err
	}
	return nil
}

// resolveProjectPath returns the absolute, symlink-resolved path.
func resolveProjectPath(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return projectRoot
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs)
}

// cwdMatchesProject checks if the working directory matches the project root.
func cwdMatchesProject(projectRoot, cwd string) bool {
	if projectRoot == "" || cwd == "" {
		return false
	}
	projectAbs := resolveProjectPath(projectRoot)
	cwdAbs := resolveProjectPath(cwd)

	if projectAbs == "" || cwdAbs == "" {
		return false
	}

	rel, err := filepath.Rel(projectAbs, cwdAbs)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..")
}

// firstPromptText returns the text of the first Prompt entry in the history.
func firstPromptText(history []HistoryEntry) string {
	for _, entry := range history {
		if isPromptEntry(entry) {
			return extractPromptText(entry.User.Content)
		}
	}
	return ""
}

// isPromptEntry returns true if the history entry's user content is a Prompt.
func isPromptEntry(entry HistoryEntry) bool {
	if entry.User == nil || entry.User.Content == nil {
		return false
	}
	var pc PromptContent
	if err := json.Unmarshal(entry.User.Content, &pc); err == nil && pc.Prompt != nil {
		return true
	}
	return false
}

// extractPromptText extracts the prompt text from user content JSON.
func extractPromptText(content json.RawMessage) string {
	if content == nil {
		return ""
	}
	var pc PromptContent
	if err := json.Unmarshal(content, &pc); err == nil && pc.Prompt != nil {
		return pc.Prompt.Prompt
	}
	return ""
}

// parseAssistantMessage parses the assistant JSON into a Message and optional ToolUses.
func parseAssistantMessage(raw json.RawMessage, sessionID string, idx int, ts time.Time, model string) (*adapter.Message, []adapter.ToolUse) {
	// Try Response first
	var resp AssistantResponse
	if err := json.Unmarshal(raw, &resp); err == nil && resp.Response != nil {
		msgID := resp.Response.MessageID
		if msgID == "" {
			msgID = sessionID + "-asst-" + strconv.Itoa(idx)
		}
		return &adapter.Message{
			ID:        msgID,
			Role:      "assistant",
			Content:   resp.Response.Content,
			Timestamp: ts,
			Model:     model,
		}, nil
	}

	// Try ToolUse
	var tu AssistantToolUse
	if err := json.Unmarshal(raw, &tu); err == nil && tu.ToolUse != nil {
		msgID := tu.ToolUse.MessageID
		if msgID == "" {
			msgID = sessionID + "-asst-" + strconv.Itoa(idx)
		}

		var toolUses []adapter.ToolUse
		for _, t := range tu.ToolUse.ToolUses {
			inputJSON := ""
			if t.Args != nil {
				inputJSON = string(t.Args)
			}
			toolUses = append(toolUses, adapter.ToolUse{
				ID:    t.ID,
				Name:  t.Name,
				Input: inputJSON,
			})
		}

		return &adapter.Message{
			ID:        msgID,
			Role:      "assistant",
			Content:   tu.ToolUse.Content,
			Timestamp: ts,
			Model:     model,
		}, toolUses
	}

	return nil, nil
}

// linkToolResults links tool outputs from a ToolUseResults entry back to tool uses.
func linkToolResults(toolUses []adapter.ToolUse, content json.RawMessage) {
	if content == nil {
		return
	}
	var trc ToolUseResultsContent
	if err := json.Unmarshal(content, &trc); err != nil || trc.ToolUseResults == nil {
		return
	}

	// Build a map of tool_use_id -> output
	resultMap := make(map[string]string)
	for _, result := range trc.ToolUseResults.ToolUseResults {
		var output string
		for _, c := range result.Content {
			if c.Json != nil {
				if c.Json.Stdout != "" {
					output = truncateOutput(c.Json.Stdout, 1000)
				} else if c.Json.Stderr != "" {
					output = truncateOutput(c.Json.Stderr, 1000)
				}
			}
		}
		if output != "" {
			resultMap[result.ToolUseID] = output
		}
	}

	// Link results to tool uses
	for i := range toolUses {
		if out, ok := resultMap[toolUses[i].ID]; ok {
			toolUses[i].Output = out
		}
	}
}

// parseTimestamp parses an RFC3339 timestamp string.
func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// shortConversationID returns the first 8 characters of a conversation ID.
func shortConversationID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// truncateText truncates text to maxLen, adding "..." if truncated.
func truncateText(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)

	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// truncateOutput truncates command output to maxLen characters.
func truncateOutput(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

