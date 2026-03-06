package goose

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
	_ "github.com/mattn/go-sqlite3"
)

func createGooseFixture(t *testing.T) (dbPath, projectPath, sessionID string) {
	t.Helper()

	tmpDir := t.TempDir()
	projectPath = filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	dbPath = filepath.Join(tmpDir, "sessions.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	stmts := []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, name TEXT, description TEXT, session_type TEXT, working_dir TEXT, created_at TEXT, updated_at TEXT, total_tokens INTEGER, input_tokens INTEGER, output_tokens INTEGER, accumulated_total_tokens INTEGER, accumulated_input_tokens INTEGER, accumulated_output_tokens INTEGER);`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, message_id TEXT, session_id TEXT NOT NULL, role TEXT NOT NULL, content_json TEXT NOT NULL, created_timestamp INTEGER NOT NULL, timestamp TEXT, metadata_json TEXT);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	sessionID = "ses_goose_001"
	if _, err := db.Exec(`INSERT INTO sessions(id, name, description, session_type, working_dir, created_at, updated_at, total_tokens, input_tokens, output_tokens, accumulated_total_tokens, accumulated_input_tokens, accumulated_output_tokens) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID,
		"", // force fallback to description
		"Implement goose adapter",
		"user",
		projectPath,
		now.Add(-2*time.Minute).Format("2006-01-02 15:04:05"),
		now.Format("2006-01-02 15:04:05"),
		120,
		80,
		40,
		120,
		80,
		40,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	userContent := `[{"type":"text","text":"Please list files"}]`
	assistantContent := `[{"type":"text","text":"Sure"},{"type":"toolRequest","id":"tool_1","toolCall":{"status":"success","value":{"name":"list_files","arguments":{"path":"."}}}},{"type":"toolResponse","id":"tool_1","toolResult":{"status":"success","value":{"content":[{"type":"text","text":"file1.go\nfile2.go"}]}}}]`
	if _, err := db.Exec(`INSERT INTO messages(message_id, session_id, role, content_json, created_timestamp, timestamp, metadata_json) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		"msg_1", sessionID, "user", userContent, now.Add(-90*time.Second).Unix(), now.Add(-90*time.Second).Format(time.RFC3339), `{"model":""}`,
	); err != nil {
		t.Fatalf("insert user message: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO messages(message_id, session_id, role, content_json, created_timestamp, timestamp, metadata_json) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		"msg_2", sessionID, "assistant", assistantContent, now.Add(-60*time.Second).Unix(), now.Add(-60*time.Second).Format(time.RFC3339), `{"model":"claude-sonnet"}`,
	); err != nil {
		t.Fatalf("insert assistant message: %v", err)
	}

	return dbPath, projectPath, sessionID
}

func TestDetectSessionsMessagesUsage(t *testing.T) {
	dbPath, projectPath, sessionID := createGooseFixture(t)
	a := &Adapter{dbPath: dbPath}

	found, err := a.Detect(projectPath)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !found {
		t.Fatal("expected Detect to succeed")
	}

	sessions, err := a.Sessions(projectPath)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.ID != sessionID {
		t.Fatalf("ID = %q, want %q", s.ID, sessionID)
	}
	if s.Name != "Implement goose adapter" {
		t.Fatalf("Name = %q, want fallback description", s.Name)
	}
	if s.AdapterID != "goose" || s.AdapterName != "Goose" {
		t.Fatalf("adapter metadata mismatch: %q/%q", s.AdapterID, s.AdapterName)
	}
	if s.TotalTokens != 120 {
		t.Fatalf("TotalTokens = %d, want 120", s.TotalTokens)
	}
	if s.MessageCount != 2 {
		t.Fatalf("MessageCount = %d, want 2", s.MessageCount)
	}
	if s.FileSize <= 0 {
		t.Fatalf("FileSize = %d, want >0", s.FileSize)
	}

	messages, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("unexpected roles: %q/%q", messages[0].Role, messages[1].Role)
	}
	if len(messages[1].ToolUses) != 1 {
		t.Fatalf("expected 1 tool use, got %d", len(messages[1].ToolUses))
	}
	if messages[1].ToolUses[0].Name != "list_files" {
		t.Fatalf("unexpected tool name: %q", messages[1].ToolUses[0].Name)
	}
	if messages[1].ToolUses[0].Output == "" {
		t.Fatal("expected linked tool output")
	}
	if messages[1].Model != "claude-sonnet" {
		t.Fatalf("model = %q, want claude-sonnet", messages[1].Model)
	}

	usage, err := a.Usage(sessionID)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage == nil {
		t.Fatal("expected usage")
	}
	if usage.TotalInputTokens != 80 || usage.TotalOutputTokens != 40 {
		t.Fatalf("usage token mismatch: in=%d out=%d", usage.TotalInputTokens, usage.TotalOutputTokens)
	}

	single, err := a.SessionByID(sessionID)
	if err != nil {
		t.Fatalf("SessionByID: %v", err)
	}
	if single.ID != sessionID {
		t.Fatalf("SessionByID ID = %q, want %q", single.ID, sessionID)
	}
}

func TestDetectMissingDB(t *testing.T) {
	a := &Adapter{dbPath: filepath.Join(t.TempDir(), "missing.db")}
	found, err := a.Detect(t.TempDir())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if found {
		t.Fatal("Detect should be false when DB is missing")
	}
}

func TestWatchScopeGlobal(t *testing.T) {
	a := New()
	if got := a.WatchScope(); got != adapter.WatchScopeGlobal {
		t.Fatalf("WatchScope = %v, want %v", got, adapter.WatchScopeGlobal)
	}
}

func TestWatcherEmitsOnWALWrite(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sessions.db")
	if err := os.WriteFile(dbPath, []byte("sqlite"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}

	ch, closer, err := NewWatcher(dbPath)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer func() { _ = closer.Close() }()

	if err := os.WriteFile(dbPath+"-wal", []byte("update"), 0o644); err != nil {
		t.Fatalf("write wal: %v", err)
	}

	select {
	case evt := <-ch:
		if evt.Type != adapter.EventSessionCreated && evt.Type != adapter.EventSessionUpdated {
			t.Fatalf("unexpected event type: %v", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected watcher event")
	}
}

func TestBasicAdapterMethods(t *testing.T) {
	a := New()

	if got := a.ID(); got != "goose" {
		t.Fatalf("ID() = %q, want goose", got)
	}
	if got := a.Name(); got != "Goose" {
		t.Fatalf("Name() = %q, want Goose", got)
	}
	if got := a.Icon(); got != "G" {
		t.Fatalf("Icon() = %q, want G", got)
	}

	caps := a.Capabilities()
	if !caps[adapter.CapSessions] || !caps[adapter.CapMessages] || !caps[adapter.CapUsage] || !caps[adapter.CapWatch] {
		t.Fatalf("expected all capabilities true, got: %#v", caps)
	}
}

func TestAdapterWatch(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sessions.db")
	if err := os.WriteFile(dbPath, []byte("sqlite"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}

	a := &Adapter{dbPath: dbPath}
	ch, closer, err := a.Watch(tmpDir)
	if err != nil {
		t.Fatalf("Watch() error: %v", err)
	}
	if ch == nil || closer == nil {
		t.Fatal("Watch() returned nil channel/closer")
	}
	_ = closer.Close()
}

func TestAdapterWatchEnrichesSessionID(t *testing.T) {
	dbPath, _, _ := createGooseFixture(t)
	a := &Adapter{dbPath: dbPath}

	ch, closer, err := a.Watch("")
	if err != nil {
		t.Fatalf("Watch() error: %v", err)
	}
	defer func() { _ = closer.Close() }()

	if err := os.WriteFile(dbPath+"-wal", []byte("update"), 0o644); err != nil {
		t.Fatalf("write wal: %v", err)
	}

	select {
	case evt := <-ch:
		if evt.Type != adapter.EventSessionCreated && evt.Type != adapter.EventSessionUpdated {
			t.Fatalf("unexpected event type: %v", evt.Type)
		}
		if evt.SessionID == "" {
			t.Fatal("expected SessionID enrichment on watch event")
		}
		if evt.SessionID != "ses_goose_001" {
			t.Fatalf("expected enriched SessionID ses_goose_001, got %q", evt.SessionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected watcher event")
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("containsPath", func(t *testing.T) {
		paths := []string{"/a", "/b"}
		if !containsPath(paths, "/a") {
			t.Fatal("containsPath should find existing path")
		}
		if containsPath(paths, "/c") {
			t.Fatal("containsPath should not find missing path")
		}
	})

	t.Run("chooseInt", func(t *testing.T) {
		if got := chooseInt(sql.NullInt64{Valid: true, Int64: 3}, sql.NullInt64{Valid: true, Int64: 9}); got != 3 {
			t.Fatalf("chooseInt(primary) = %d, want 3", got)
		}
		if got := chooseInt(sql.NullInt64{}, sql.NullInt64{Valid: true, Int64: 9}); got != 9 {
			t.Fatalf("chooseInt(fallback) = %d, want 9", got)
		}
		if got := chooseInt(sql.NullInt64{}, sql.NullInt64{}); got != 0 {
			t.Fatalf("chooseInt(empty) = %d, want 0", got)
		}
	})

	t.Run("parseSQLiteTime", func(t *testing.T) {
		if parseSQLiteTime("").IsZero() != true {
			t.Fatal("empty time should parse to zero")
		}
		if parseSQLiteTime("not-a-time").IsZero() != true {
			t.Fatal("invalid time should parse to zero")
		}
		if parseSQLiteTime("2026-01-02 03:04:05").IsZero() {
			t.Fatal("valid sqlite time should parse")
		}
		if parseSQLiteTime(time.Now().UTC().Format(time.RFC3339Nano)).IsZero() {
			t.Fatal("valid RFC3339 time should parse")
		}
	})

	t.Run("role and category mapping", func(t *testing.T) {
		if got := normalizeRole(""); got != "assistant" {
			t.Fatalf("normalizeRole(empty) = %q", got)
		}
		if got := normalizeRole("USER"); got != "user" {
			t.Fatalf("normalizeRole(USER) = %q", got)
		}
		if got := normalizeRole("custom"); got != "custom" {
			t.Fatalf("normalizeRole(custom) = %q", got)
		}

		if got := toSessionCategory("scheduled"); got != adapter.SessionCategoryCron {
			t.Fatalf("toSessionCategory(scheduled) = %q", got)
		}
		if got := toSessionCategory("hidden"); got != adapter.SessionCategorySystem {
			t.Fatalf("toSessionCategory(hidden) = %q", got)
		}
		if got := toSessionCategory("user"); got != adapter.SessionCategoryInteractive {
			t.Fatalf("toSessionCategory(user) = %q", got)
		}
	})

	t.Run("shortConversationID and maxInt", func(t *testing.T) {
		if got := shortConversationID("abcdef012345"); got != "abcdef01" {
			t.Fatalf("shortConversationID = %q", got)
		}
		if got := shortConversationID("abc"); got != "abc" {
			t.Fatalf("shortConversationID short = %q", got)
		}
		if got := maxInt(2, 5); got != 5 {
			t.Fatalf("maxInt = %d, want 5", got)
		}
		if got := maxInt(8, 5); got != 8 {
			t.Fatalf("maxInt = %d, want 8", got)
		}
	})
}

func TestGooseDBCandidatesAndFind(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOOSE_PATH_ROOT", tmp)

	candidates := gooseDBCandidates(tmp)
	if len(candidates) == 0 {
		t.Fatal("gooseDBCandidates returned no paths")
	}
	if !strings.Contains(candidates[0], filepath.Join(tmp, "data", "sessions.db")) {
		t.Fatalf("first candidate should respect GOOSE_PATH_ROOT, got %q", candidates[0])
	}

	switch runtime.GOOS {
	case "darwin":
		found := false
		for _, c := range candidates {
			if strings.Contains(c, "Library/Application Support") && strings.Contains(c, "goose/sessions.db") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected macOS candidates in list: %#v", candidates)
		}
	case "linux":
		found := false
		for _, c := range candidates {
			if strings.Contains(c, "goose/sessions.db") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected linux goose candidate in list: %#v", candidates)
		}
	}

	dbPath := filepath.Join(tmp, "data", "sessions.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir for db path: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("sqlite"), 0o644); err != nil {
		t.Fatalf("write db path: %v", err)
	}
	if got := findGooseDB(tmp); got != dbPath {
		t.Fatalf("findGooseDB = %q, want %q", got, dbPath)
	}
}

func TestInterfacesAndSearch(t *testing.T) {
	var _ adapter.MessageSearcher = New()
	var _ adapter.TargetedRefresher = New()

	dbPath, projectPath, sessionID := createGooseFixture(t)
	a := &Adapter{dbPath: dbPath, msgMap: make(map[string]messageCacheEntry)}

	found, err := a.Detect(projectPath)
	if err != nil || !found {
		t.Fatalf("Detect failed: found=%v err=%v", found, err)
	}

	results, err := a.SearchMessages(sessionID, "list_files", adapter.DefaultSearchOptions())
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search matches for list_files")
	}
}

func TestParseAndPathHelpers(t *testing.T) {
	t.Run("parseGooseBlocks error and success", func(t *testing.T) {
		if _, err := parseGooseBlocks("{"); err == nil {
			t.Fatal("expected parseGooseBlocks error for invalid json")
		}
		blocks, err := parseGooseBlocks(`[{"type":"text","text":"ok"}]`)
		if err != nil {
			t.Fatalf("unexpected parseGooseBlocks error: %v", err)
		}
		if len(blocks) != 1 || blocks[0].Text != "ok" {
			t.Fatalf("unexpected blocks: %#v", blocks)
		}
	})

	t.Run("parseToolCall and parseToolResult branches", func(t *testing.T) {
		name, input := parseToolCall(ToolResultValue{Status: "error"})
		if name != "" || input != "" {
			t.Fatalf("expected empty tool call on error, got %q / %q", name, input)
		}

		name, input = parseToolCall(ToolResultValue{
			Status: "success",
			Value: ToolCallValue{
				Name:      "run",
				Arguments: map[string]any{"cmd": "ls"},
			},
		})
		if name != "run" || !strings.Contains(input, "\"cmd\":\"ls\"") {
			t.Fatalf("unexpected tool call parse result: %q / %q", name, input)
		}

		out, isErr := parseToolResult(ToolResponseValue{Status: "error", Error: "boom"})
		if out != "boom" || !isErr {
			t.Fatalf("expected error result, got out=%q isErr=%v", out, isErr)
		}

		out, isErr = parseToolResult(ToolResponseValue{Status: "unknown"})
		if out != "" || isErr {
			t.Fatalf("expected empty unknown status result, got out=%q isErr=%v", out, isErr)
		}
	})

	t.Run("resolveProjectPath and cwdMatchesProject", func(t *testing.T) {
		project := t.TempDir()
		sub := filepath.Join(project, "subdir")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir subdir: %v", err)
		}

		resolved := resolveProjectPath(project)
		if !filepath.IsAbs(resolved) {
			t.Fatalf("resolveProjectPath should return abs path, got %q", resolved)
		}
		if !cwdMatchesProject(project, project) {
			t.Fatal("cwdMatchesProject should match same path")
		}
		if !cwdMatchesProject(project, sub) {
			t.Fatal("cwdMatchesProject should match subdir")
		}
		if cwdMatchesProject(project, t.TempDir()) {
			t.Fatal("cwdMatchesProject should not match unrelated dir")
		}
	})
}

func TestUsageNoRowsAndWatchError(t *testing.T) {
	a := &Adapter{dbPath: filepath.Join(t.TempDir(), "missing.db")}

	if _, _, err := a.Watch(t.TempDir()); err == nil {
		t.Fatal("Watch should fail when db is missing")
	}

	dbPath, projectPath, _ := createGooseFixture(t)
	a = &Adapter{dbPath: dbPath}

	found, err := a.Detect(filepath.Join(projectPath, "does-not-match"))
	if err != nil {
		t.Fatalf("Detect mismatch path error: %v", err)
	}
	if found {
		t.Fatal("Detect should be false for non-matching project")
	}

	usage, err := a.Usage("missing-session")
	if err != nil {
		t.Fatalf("Usage missing-session error: %v", err)
	}
	if usage != nil {
		t.Fatalf("Usage missing-session should be nil, got %#v", usage)
	}

	if _, err := a.SessionByID("missing-session"); err == nil {
		t.Fatal("SessionByID should error for missing session")
	}
}

func TestMessagesContentVariantsAndSessionFiltering(t *testing.T) {
	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "repo")
	otherPath := filepath.Join(tmpDir, "other")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := os.MkdirAll(otherPath, 0o755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "sessions.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	stmts := []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, name TEXT, description TEXT, session_type TEXT, working_dir TEXT, created_at TEXT, updated_at TEXT, total_tokens INTEGER, input_tokens INTEGER, output_tokens INTEGER, accumulated_total_tokens INTEGER, accumulated_input_tokens INTEGER, accumulated_output_tokens INTEGER);`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, message_id TEXT, session_id TEXT NOT NULL, role TEXT NOT NULL, content_json TEXT NOT NULL, created_timestamp INTEGER NOT NULL, timestamp TEXT, metadata_json TEXT);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.Exec(`INSERT INTO sessions(id, name, description, session_type, working_dir, created_at, updated_at, total_tokens, input_tokens, output_tokens, accumulated_total_tokens, accumulated_input_tokens, accumulated_output_tokens) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"ses_main", "main", "", "scheduled", projectPath, now.Add(-3*time.Minute).Format(time.RFC3339), now.Format(time.RFC3339), nil, nil, nil, 33, 20, 13,
	); err != nil {
		t.Fatalf("insert ses_main: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sessions(id, name, description, session_type, working_dir, created_at, updated_at, total_tokens, input_tokens, output_tokens, accumulated_total_tokens, accumulated_input_tokens, accumulated_output_tokens) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"ses_other", "other", "", "hidden", otherPath, now.Add(-2*time.Minute).Format(time.RFC3339), now.Add(-time.Minute).Format(time.RFC3339), 1, 1, 0, nil, nil, nil,
	); err != nil {
		t.Fatalf("insert ses_other: %v", err)
	}

	assistantMixed := `[
		{"type":"thinking","thinking":"plan first"},
		{"type":"reasoning","text":"reason note"},
		{"type":"frontendToolRequest","id":"tool_f1","toolCall":{"status":"success","value":{"name":"open_file","arguments":{"path":"README.md"}}}},
		{"type":"toolResponse","id":"tool_f1","toolResult":{"status":"error","error":"tool failed"}}
	]`
	if _, err := db.Exec(`INSERT INTO messages(message_id, session_id, role, content_json, created_timestamp, timestamp, metadata_json) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		"m_mix", "ses_main", "assistant", assistantMixed, now.Add(-90*time.Second).Unix(), now.Add(-90*time.Second).Format(time.RFC3339), `{"modelName":"gpt-4.1"}`,
	); err != nil {
		t.Fatalf("insert mixed message: %v", err)
	}

	a := &Adapter{dbPath: dbPath}

	sessions, err := a.Sessions(projectPath)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 filtered session, got %d", len(sessions))
	}
	if sessions[0].SessionCategory != adapter.SessionCategoryCron {
		t.Fatalf("session category = %q, want cron", sessions[0].SessionCategory)
	}
	if sessions[0].TotalTokens != 33 {
		t.Fatalf("total tokens = %d, want 33", sessions[0].TotalTokens)
	}

	msgs, err := a.Messages("ses_main")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 parsed message, got %d", len(msgs))
	}
	m := msgs[0]
	if len(m.ThinkingBlocks) == 0 {
		t.Fatal("expected thinking blocks")
	}
	if len(m.ToolUses) != 1 || m.ToolUses[0].Name != "open_file" {
		t.Fatalf("unexpected tool use parse: %#v", m.ToolUses)
	}
	if m.ToolUses[0].Output != "tool failed" {
		t.Fatalf("expected linked tool error output, got %q", m.ToolUses[0].Output)
	}
	if m.Model != "gpt-4.1" {
		t.Fatalf("model parse mismatch: %q", m.Model)
	}
}

func TestMessagesMalformedContentReturnsError(t *testing.T) {
	dbPath, _, sessionID := createGooseFixture(t)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC().Unix()
	if _, err := db.Exec(`INSERT INTO messages(message_id, session_id, role, content_json, created_timestamp, timestamp, metadata_json) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		"msg_bad", sessionID, "assistant", `{`, now, time.Now().UTC().Format(time.RFC3339), `{}`,
	); err != nil {
		t.Fatalf("insert malformed message: %v", err)
	}

	a := &Adapter{dbPath: dbPath, msgMap: make(map[string]messageCacheEntry)}
	if _, err := a.Messages(sessionID); err == nil {
		t.Fatal("expected parse error for malformed content_json")
	}
}

func TestMessagesCacheDefensiveCopy(t *testing.T) {
	dbPath, _, sessionID := createGooseFixture(t)
	a := &Adapter{dbPath: dbPath, msgMap: make(map[string]messageCacheEntry)}

	msgs1, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages first: %v", err)
	}
	if len(msgs1) == 0 {
		t.Fatal("expected messages")
	}
	orig := msgs1[0].Content
	msgs1[0].Content = "mutated"

	msgs2, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages second: %v", err)
	}
	if len(msgs2) == 0 {
		t.Fatal("expected messages on second call")
	}
	if msgs2[0].Content != orig {
		t.Fatalf("cache returned mutated content = %q, want %q", msgs2[0].Content, orig)
	}
}

func TestMessagesCacheInvalidatesOnAppend(t *testing.T) {
	dbPath, _, sessionID := createGooseFixture(t)
	a := &Adapter{dbPath: dbPath, msgMap: make(map[string]messageCacheEntry)}

	msgs1, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages first: %v", err)
	}
	if len(msgs1) != 2 {
		t.Fatalf("expected 2 messages initially, got %d", len(msgs1))
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO messages(message_id, session_id, role, content_json, created_timestamp, timestamp, metadata_json) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		"msg_3", sessionID, "assistant", `[{"type":"text","text":"New message after cache"}]`, now.Unix(), now.Format(time.RFC3339), `{"model":"claude-sonnet"}`,
	); err != nil {
		t.Fatalf("insert appended message: %v", err)
	}

	msgs2, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages second: %v", err)
	}
	if len(msgs2) != 3 {
		t.Fatalf("expected 3 messages after append, got %d", len(msgs2))
	}
}
