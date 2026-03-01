package opencode

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
	_ "github.com/mattn/go-sqlite3"
)

func TestNew(t *testing.T) {
	a := New()
	if a == nil {
		t.Fatal("New() returned nil")
	}
	if a.storageDir == "" {
		t.Error("storageDir should not be empty")
	}
	if a.projectIndex == nil {
		t.Error("projectIndex should be initialized")
	}
}

func TestID(t *testing.T) {
	a := New()
	if got := a.ID(); got != "opencode" {
		t.Errorf("ID() = %q, want %q", got, "opencode")
	}
}

func TestName(t *testing.T) {
	a := New()
	if got := a.Name(); got != "OpenCode" {
		t.Errorf("Name() = %q, want %q", got, "OpenCode")
	}
}

func TestCapabilities(t *testing.T) {
	a := New()
	caps := a.Capabilities()

	if !caps["sessions"] {
		t.Error("expected sessions capability")
	}
	if !caps["messages"] {
		t.Error("expected messages capability")
	}
	if !caps["usage"] {
		t.Error("expected usage capability")
	}
	if !caps["watch"] {
		t.Error("expected watch capability")
	}
}

func createSQLiteFixture(t *testing.T) (dbPath string, projectPath string, sessionID string) {
	t.Helper()

	tmpDir := t.TempDir()
	projectPath = filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	dbPath = filepath.Join(tmpDir, "opencode.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	stmts := []string{
		`CREATE TABLE project (id TEXT PRIMARY KEY, worktree TEXT);`,
		`CREATE TABLE session (id TEXT PRIMARY KEY, project_id TEXT, parent_id TEXT, title TEXT, time_created INTEGER, time_updated INTEGER);`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, time_updated INTEGER, data TEXT);`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, data TEXT);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}

	sessionID = "ses_sqlite_main"
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO project(id, worktree) VALUES(?, ?)`, "proj_sql", projectPath); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO session(id, project_id, parent_id, title, time_created, time_updated) VALUES(?, ?, ?, ?, ?, ?)`,
		sessionID, "proj_sql", "", "SQLite Session", now-5000, now-1000); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	userData, _ := json.Marshal(map[string]any{
		"role": "user",
		"time": map[string]any{"created": now - 5000, "updated": now - 5000},
		"model": map[string]any{
			"providerID": "anthropic",
			"modelID":    "claude-3-5-sonnet",
		},
	})
	assistantData, _ := json.Marshal(map[string]any{
		"role":       "assistant",
		"modelID":    "claude-3-5-sonnet",
		"providerID": "anthropic",
		"time":       map[string]any{"created": now - 3000, "updated": now - 2000},
		"tokens": map[string]any{
			"input":  123,
			"output": 45,
			"cache":  map[string]any{"read": 7, "write": 2},
		},
	})
	if _, err := db.Exec(`INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES(?, ?, ?, ?, ?)`,
		"msg_sql_user", sessionID, now-5000, now-5000, string(userData)); err != nil {
		t.Fatalf("insert user message: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES(?, ?, ?, ?, ?)`,
		"msg_sql_assistant", sessionID, now-3000, now-2000, string(assistantData)); err != nil {
		t.Fatalf("insert assistant message: %v", err)
	}

	partTextUser, _ := json.Marshal(map[string]any{"type": "text", "text": "Hello from sqlite user"})
	partTextAssistant, _ := json.Marshal(map[string]any{"type": "text", "text": "Hello from sqlite assistant"})
	partToolAssistant, _ := json.Marshal(map[string]any{
		"type":   "tool",
		"callID": "call_sql_1",
		"tool":   "bash",
		"state": map[string]any{
			"status": "completed",
			"input":  map[string]any{"cmd": "ls -la"},
			"output": "ok",
		},
	})
	if _, err := db.Exec(`INSERT INTO part(id, message_id, session_id, data) VALUES(?, ?, ?, ?)`,
		"prt_sql_user", "msg_sql_user", sessionID, string(partTextUser)); err != nil {
		t.Fatalf("insert user part: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO part(id, message_id, session_id, data) VALUES(?, ?, ?, ?)`,
		"prt_sql_assistant_text", "msg_sql_assistant", sessionID, string(partTextAssistant)); err != nil {
		t.Fatalf("insert assistant text part: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO part(id, message_id, session_id, data) VALUES(?, ?, ?, ?)`,
		"prt_sql_assistant_tool", "msg_sql_assistant", sessionID, string(partToolAssistant)); err != nil {
		t.Fatalf("insert assistant tool part: %v", err)
	}

	return dbPath, projectPath, sessionID
}

func TestSQLiteStorageMode_DetectSessionsMessages(t *testing.T) {
	dbPath, projectPath, sessionID := createSQLiteFixture(t)
	a := &Adapter{
		storageDir:   filepath.Join(t.TempDir(), "storage"),
		dbPath:       dbPath,
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}

	found, err := a.Detect(projectPath)
	if err != nil {
		t.Fatalf("Detect sqlite error: %v", err)
	}
	if !found {
		t.Fatal("expected sqlite project detection to succeed")
	}

	sessions, err := a.Sessions(projectPath)
	if err != nil {
		t.Fatalf("Sessions sqlite error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 sqlite session, got %d", len(sessions))
	}
	if sessions[0].ID != sessionID {
		t.Fatalf("session id = %q, want %q", sessions[0].ID, sessionID)
	}
	if sessions[0].Path != "" {
		t.Fatalf("sqlite-backed session Path = %q, want empty", sessions[0].Path)
	}

	messages, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages sqlite error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 sqlite messages, got %d", len(messages))
	}
	if messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("unexpected roles: %q, %q", messages[0].Role, messages[1].Role)
	}
	if len(messages[1].ToolUses) == 0 {
		t.Fatal("expected assistant tool uses from sqlite parts")
	}
	if messages[1].InputTokens != 123 || messages[1].OutputTokens != 45 {
		t.Fatalf("unexpected sqlite tokens: in=%d out=%d", messages[1].InputTokens, messages[1].OutputTokens)
	}
}

func TestWatchScope_WithSQLiteDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "opencode.db")

	// Without DB file: should be project scope.
	aNoDB := &Adapter{dbPath: dbPath}
	if got := aNoDB.WatchScope(); got != adapter.WatchScopeProject {
		t.Fatalf("WatchScope before db exists = %v, want %v", got, adapter.WatchScopeProject)
	}

	// Create DB file, then build a fresh adapter: useSQLite is cached at first call.
	if err := os.WriteFile(dbPath, []byte("sqlite"), 0644); err != nil {
		t.Fatalf("write db file: %v", err)
	}
	aWithDB := &Adapter{dbPath: dbPath}
	if got := aWithDB.WatchScope(); got != adapter.WatchScopeGlobal {
		t.Fatalf("WatchScope after db exists = %v, want %v", got, adapter.WatchScopeGlobal)
	}
}

func TestWatch_WithSQLiteDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "opencode.db")
	if err := os.WriteFile(dbPath, []byte("sqlite"), 0644); err != nil {
		t.Fatalf("write db file: %v", err)
	}

	a := &Adapter{
		dbPath:       dbPath,
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}
	ch, closer, err := a.Watch(t.TempDir())
	if err != nil {
		t.Fatalf("Watch sqlite error: %v", err)
	}
	defer func() { _ = closer.Close() }()

	if err := os.WriteFile(dbPath+"-wal", []byte("wal-update"), 0644); err != nil {
		t.Fatalf("write wal file: %v", err)
	}

	select {
	case evt := <-ch:
		if evt.Type != adapter.EventSessionCreated && evt.Type != adapter.EventSessionUpdated {
			t.Fatalf("unexpected event type: %v", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected sqlite watcher event")
	}
}

func TestDetect_WithTestdata(t *testing.T) {
	// Create adapter pointing to testdata
	a := newTestAdapter(t)

	// Create temp directory to simulate project root
	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "test-opencode-project")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatalf("failed to create temp project dir: %v", err)
	}

	// Update testdata project to point to our temp dir
	testdataDir := getTestdataDir(t)
	projectJSON := filepath.Join(testdataDir, "project", "test_project.json")
	data, err := os.ReadFile(projectJSON)
	if err != nil {
		t.Fatalf("failed to read test project: %v", err)
	}

	// Create a modified project file pointing to temp dir
	modifiedJSON := `{
  "id": "test_project",
  "worktree": "` + projectPath + `",
  "vcs": "git",
  "time": { "created": 1767000000000, "updated": 1767100000000 }
}`
	if err := os.WriteFile(projectJSON, []byte(modifiedJSON), 0644); err != nil {
		t.Fatalf("failed to write modified project: %v", err)
	}
	defer func() {
		if err := os.WriteFile(projectJSON, data, 0644); err != nil {
			t.Logf("failed to restore original: %v", err)
		}
	}() // Restore original

	// Should detect the project
	found, err := a.Detect(projectPath)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !found {
		t.Error("expected to detect project")
	}

	// Should not detect non-existent project
	found, err = a.Detect("/nonexistent/path")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Error("should not find sessions for nonexistent path")
	}
}

func TestDetect_SkipsGlobal(t *testing.T) {
	a := newTestAdapter(t)

	// Root path should not be detected (global project is skipped)
	found, err := a.Detect("/")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Error("should not detect global project (worktree=/)")
	}
}

func TestFindProjectID_WithSandboxPaths(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create a project with sandboxes
	worktreePath := filepath.Join(tmpDir, "repos", "core.zdaj.app")
	sandboxPath := filepath.Join(worktreePath, "main")

	projectJSON := fmt.Sprintf(`{
  "id": "test_project_sandbox",
  "worktree": "%s",
  "vcs": "git",
  "sandboxes": ["%s"],
  "time": { "created": 1767000000000, "updated": 1767100000000 }
}`, worktreePath, sandboxPath)

	if err := os.WriteFile(filepath.Join(projectDir, "test_sandbox.json"), []byte(projectJSON), 0644); err != nil {
		t.Fatalf("failed to write project file: %v", err)
	}

	a := &Adapter{
		storageDir:   tmpDir,
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}

	// Test 1: Find project by worktree path
	projectID, err := a.findProjectID(worktreePath)
	if err != nil {
		t.Fatalf("findProjectID error: %v", err)
	}
	if projectID != "test_project_sandbox" {
		t.Errorf("expected project ID %q, got %q", "test_project_sandbox", projectID)
	}

	// Test 2: Find project by sandbox path
	projectID, err = a.findProjectID(sandboxPath)
	if err != nil {
		t.Fatalf("findProjectID error for sandbox: %v", err)
	}
	if projectID != "test_project_sandbox" {
		t.Errorf("expected project ID %q for sandbox path, got %q", "test_project_sandbox", projectID)
	}

	// Test 3: Path not in sandboxes should not be found
	unrelatedPath := filepath.Join(tmpDir, "repos", "other-repo")
	projectID, err = a.findProjectID(unrelatedPath)
	if err != nil {
		t.Fatalf("findProjectID error: %v", err)
	}
	if projectID != "" {
		t.Errorf("expected empty project ID for unrelated path, got %q", projectID)
	}
}


func TestFindProjectID_SubdirectoryMatch(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Simulate bare-repo layout: OpenCode registers /repo as worktree,
	// but sidecar's ProjectRoot resolves to /repo/.bare (the main worktree).
	worktreePath := filepath.Join(tmpDir, "repos", "core.zdaj.app")
	barePath := filepath.Join(worktreePath, ".bare")
	sandboxPath := filepath.Join(worktreePath, "main")

	projectJSON := fmt.Sprintf(`{
  "id": "test_project_subdir",
  "worktree": "%s",
  "vcs": "git",
  "sandboxes": ["%s"],
  "time": { "created": 1767000000000, "updated": 1767100000000 }
}`, worktreePath, sandboxPath)

	if err := os.WriteFile(filepath.Join(projectDir, "test_subdir.json"), []byte(projectJSON), 0644); err != nil {
		t.Fatalf("failed to write project file: %v", err)
	}

	a := &Adapter{
		storageDir:   tmpDir,
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}

	// Test 1: .bare subdirectory should match via subdirectory fallback
	projectID, err := a.findProjectID(barePath)
	if err != nil {
		t.Fatalf("findProjectID error for .bare path: %v", err)
	}
	if projectID != "test_project_subdir" {
		t.Errorf("expected project ID %q for .bare subdirectory, got %q", "test_project_subdir", projectID)
	}

	// Test 2: Nested subdirectory should also match
	nestedPath := filepath.Join(worktreePath, ".bare", "worktrees", "main")
	projectID, err = a.findProjectID(nestedPath)
	if err != nil {
		t.Fatalf("findProjectID error for nested path: %v", err)
	}
	if projectID != "test_project_subdir" {
		t.Errorf("expected project ID %q for nested subdirectory, got %q", "test_project_subdir", projectID)
	}

	// Test 3: Sibling directory should NOT match (prefix attack prevention)
	siblingPath := filepath.Join(tmpDir, "repos", "core.zdaj.app-other")
	projectID, err = a.findProjectID(siblingPath)
	if err != nil {
		t.Fatalf("findProjectID error for sibling path: %v", err)
	}
	if projectID != "" {
		t.Errorf("expected empty project ID for sibling path, got %q", projectID)
	}
}
func TestFindProjectID_SandboxNotDuplicated(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create a project where sandbox path equals worktree path (should not duplicate)
	worktreePath := filepath.Join(tmpDir, "repos", "myrepo")

	projectJSON := fmt.Sprintf(`{
  "id": "test_project_no_dup",
  "worktree": "%s",
  "vcs": "git",
  "sandboxes": ["%s"],
  "time": { "created": 1767000000000, "updated": 1767100000000 }
}`, worktreePath, worktreePath)

	if err := os.WriteFile(filepath.Join(projectDir, "test_no_dup.json"), []byte(projectJSON), 0644); err != nil {
		t.Fatalf("failed to write project file: %v", err)
	}

	a := &Adapter{
		storageDir:   tmpDir,
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}

	// Should find project by worktree path
	projectID, err := a.findProjectID(worktreePath)
	if err != nil {
		t.Fatalf("findProjectID error: %v", err)
	}
	if projectID != "test_project_no_dup" {
		t.Errorf("expected project ID %q, got %q", "test_project_no_dup", projectID)
	}

	// Verify projectIndex has only one entry for this path (no duplication)
	if len(a.projectIndex) != 1 {
		t.Errorf("expected 1 entry in projectIndex, got %d", len(a.projectIndex))
	}
}

func TestSessions_WithTestdata(t *testing.T) {
	a := newTestAdapter(t)
	testdataDir := getTestdataDir(t)

	// Create temp dir and update project to point to it
	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "test-opencode-project")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatalf("failed to create temp project dir: %v", err)
	}

	// Modify project file
	projectJSON := filepath.Join(testdataDir, "project", "test_project.json")
	origData, err := os.ReadFile(projectJSON)
	if err != nil {
		t.Fatalf("failed to read original project: %v", err)
	}
	modifiedJSON := `{
  "id": "test_project",
  "worktree": "` + projectPath + `",
  "vcs": "git",
  "time": { "created": 1767000000000, "updated": 1767100000000 }
}`
	if err := os.WriteFile(projectJSON, []byte(modifiedJSON), 0644); err != nil {
		t.Fatalf("failed to write modified project: %v", err)
	}
	defer func() {
		if err := os.WriteFile(projectJSON, origData, 0644); err != nil {
			t.Logf("failed to restore original: %v", err)
		}
	}()

	sessions, err := a.Sessions(projectPath)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Check sessions are sorted by UpdatedAt descending
	if sessions[0].UpdatedAt.Before(sessions[1].UpdatedAt) {
		t.Error("sessions should be sorted by UpdatedAt descending")
	}

	// Find the main session and subagent
	var mainSession, subAgent *struct {
		ID         string
		Name       string
		IsSubAgent bool
	}
	for i := range sessions {
		s := sessions[i]
		switch s.ID {
		case "ses_test_main":
			mainSession = &struct {
				ID         string
				Name       string
				IsSubAgent bool
			}{s.ID, s.Name, s.IsSubAgent}
		case "ses_subagent":
			subAgent = &struct {
				ID         string
				Name       string
				IsSubAgent bool
			}{s.ID, s.Name, s.IsSubAgent}
		}
	}

	if mainSession == nil {
		t.Error("expected to find main session")
	} else {
		if mainSession.Name != "Test Session Main" {
			t.Errorf("main session name = %q, want %q", mainSession.Name, "Test Session Main")
		}
		if mainSession.IsSubAgent {
			t.Error("main session should not be a subagent")
		}
	}

	if subAgent == nil {
		t.Error("expected to find subagent session")
	} else {
		if !subAgent.IsSubAgent {
			t.Error("subagent session should have IsSubAgent=true")
		}
	}
}

func TestMessages_WithTestdata(t *testing.T) {
	a := newTestAdapter(t)

	messages, err := a.Messages("ses_test_main")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// Messages should be sorted by timestamp ascending
	if messages[0].Timestamp.After(messages[1].Timestamp) {
		t.Error("messages should be sorted by timestamp ascending")
	}

	// Check user message
	userMsg := messages[0]
	if userMsg.Role != "user" {
		t.Errorf("first message role = %q, want %q", userMsg.Role, "user")
	}
	if userMsg.Content == "" {
		t.Error("user message should have content from text part")
	}

	// Check assistant message
	assistantMsg := messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("second message role = %q, want %q", assistantMsg.Role, "assistant")
	}
	if len(assistantMsg.ToolUses) == 0 {
		t.Error("assistant message should have tool uses")
	}
	if assistantMsg.InputTokens == 0 {
		t.Error("assistant message should have input tokens")
	}
	if assistantMsg.OutputTokens == 0 {
		t.Error("assistant message should have output tokens")
	}
}

func TestUsage_WithTestdata(t *testing.T) {
	a := newTestAdapter(t)

	usage, err := a.Usage("ses_test_main")
	if err != nil {
		t.Fatalf("Usage error: %v", err)
	}

	if usage.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", usage.MessageCount)
	}

	// Only assistant message has tokens
	if usage.TotalInputTokens != 1000 {
		t.Errorf("TotalInputTokens = %d, want 1000", usage.TotalInputTokens)
	}
	if usage.TotalOutputTokens != 500 {
		t.Errorf("TotalOutputTokens = %d, want 500", usage.TotalOutputTokens)
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		id       string
		expected string
	}{
		{"123456789012", "123456789012"},
		{"1234567890123456", "123456789012"},
		{"12345678901", "12345678901"},
		{"abc", "abc"},
		{"", ""},
	}

	for _, tt := range tests {
		result := shortID(tt.id)
		if result != tt.expected {
			t.Errorf("shortID(%q) = %q, expected %q", tt.id, result, tt.expected)
		}
	}
}

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		model   string
		input   int
		output  int
		cache   int
		minCost float64
		maxCost float64
	}{
		{"claude-opus-4", 1000, 500, 0, 0.05, 0.06},
		{"claude-sonnet-4", 1000, 500, 0, 0.01, 0.02},
		{"claude-haiku", 1000, 500, 0, 0.0005, 0.001},
		{"gpt-4o", 1000, 500, 0, 0.005, 0.01},
		{"deepseek", 1000, 500, 0, 0.0001, 0.0005},
	}

	for _, tt := range tests {
		cost := calculateCost(tt.model, tt.input, tt.output, tt.cache)
		if cost < tt.minCost || cost > tt.maxCost {
			t.Errorf("calculateCost(%q, %d, %d, %d) = %f, want between %f and %f",
				tt.model, tt.input, tt.output, tt.cache, cost, tt.minCost, tt.maxCost)
		}
	}
}

func TestTimeInfo(t *testing.T) {
	ti := TimeInfo{
		Created:   1767050000000,
		Updated:   1767060000000,
		Completed: 1767070000000,
	}

	created := ti.CreatedTime()
	if created.IsZero() {
		t.Error("CreatedTime should not be zero")
	}

	updated := ti.UpdatedTime()
	if updated.IsZero() {
		t.Error("UpdatedTime should not be zero")
	}
	if !updated.After(created) {
		t.Error("UpdatedTime should be after CreatedTime")
	}

	completed := ti.CompletedTime()
	if completed.IsZero() {
		t.Error("CompletedTime should not be zero")
	}

	// Test zero values
	zeroTi := TimeInfo{}
	if !zeroTi.CreatedTime().IsZero() {
		t.Error("Zero TimeInfo should return zero CreatedTime")
	}
}

func TestToolInputString(t *testing.T) {
	input := map[string]any{
		"command":     "ls -la",
		"description": "List files",
	}
	result := ToolInputString(input)
	if result == "" {
		t.Error("ToolInputString should return non-empty string")
	}

	// Test nil input
	nilResult := ToolInputString(nil)
	if nilResult != "" {
		t.Error("ToolInputString(nil) should return empty string")
	}
}

func TestToolOutputString(t *testing.T) {
	// Test string output
	strResult := ToolOutputString("hello")
	if strResult != "hello" {
		t.Errorf("ToolOutputString(string) = %q, want %q", strResult, "hello")
	}

	// Test map output
	mapResult := ToolOutputString(map[string]any{"key": "value"})
	if mapResult == "" {
		t.Error("ToolOutputString(map) should return non-empty string")
	}

	// Test nil output
	nilResult := ToolOutputString(nil)
	if nilResult != "" {
		t.Error("ToolOutputString(nil) should return empty string")
	}
}

// Helper functions

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	testdataDir := getTestdataDir(t)
	return &Adapter{
		storageDir:   testdataDir,
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}
}

func getTestdataDir(t *testing.T) string {
	t.Helper()
	// Get the directory of this test file
	_, filename, _, ok := runtimeCaller(0)
	if !ok {
		t.Fatal("failed to get test file location")
	}
	return filepath.Join(filepath.Dir(filename), "testdata")
}

// runtimeCaller is a wrapper for runtime.Caller to make testing easier
func runtimeCaller(skip int) (pc uintptr, file string, line int, ok bool) {
	// In real code, this would call runtime.Caller
	// For testdata, we use a relative path approach
	cwd, _ := os.Getwd()
	return 0, filepath.Join(cwd, "adapter_test.go"), 0, true
}

// TestWithRealData tests against actual OpenCode data if available
func TestWithRealData(t *testing.T) {
	a := New()

	// Check if real OpenCode data exists
	if _, err := os.Stat(a.storageDir); os.IsNotExist(err) {
		t.Skip("no real OpenCode data available")
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Try to detect this project
	found, err := a.Detect(cwd)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	t.Logf("OpenCode sessions for %s: %v", cwd, found)

	if !found {
		t.Skip("no OpenCode sessions for current project")
	}

	// Get sessions
	sessions, err := a.Sessions(cwd)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	t.Logf("found %d sessions", len(sessions))
	if len(sessions) == 0 {
		t.Skip("no sessions to test")
	}

	// Check first session
	s := sessions[0]
	if s.ID == "" {
		t.Error("session ID should not be empty")
	}
	if s.AdapterID != "opencode" {
		t.Errorf("session AdapterID = %q, want %q", s.AdapterID, "opencode")
	}
	if s.CreatedAt.IsZero() {
		t.Error("session CreatedAt should not be zero")
	}
	if s.UpdatedAt.IsZero() {
		t.Error("session UpdatedAt should not be zero")
	}

	// Get messages for first session
	messages, err := a.Messages(s.ID)
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}
	t.Logf("found %d messages in session %s", len(messages), s.ID)

	// Check timestamps are local
	for _, m := range messages {
		if !m.Timestamp.IsZero() {
			_, offset := m.Timestamp.Zone()
			localOffset := time.Now().Local().Sub(time.Now().UTC()).Seconds()
			if float64(offset) != localOffset {
				// This is just a warning - timezone handling is complex
				t.Logf("message timestamp may not be in local timezone: %v", m.Timestamp)
			}
		}
	}
}

func TestDiscoverRelatedProjectDirs(t *testing.T) {
	// Create temp directory simulating OpenCode storage
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create test project files with different worktree paths
	projects := []struct {
		id       string
		worktree string
	}{
		{"proj1", "/Users/test/code/myrepo"},
		{"proj2", "/Users/test/code/myrepo-feature"},
		{"proj3", "/Users/test/code/myrepo-bugfix"},
		{"proj4", "/Users/test/other"},
		{"proj5", "/Users/test/code/myrepo2"}, // Different repo
	}

	for _, p := range projects {
		data := fmt.Sprintf(`{"id":"%s","worktree":"%s"}`, p.id, p.worktree)
		path := filepath.Join(projectDir, p.id+".json")
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatalf("failed to write project file: %v", err)
		}
	}

	a := &Adapter{
		storageDir:   tmpDir,
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}

	// Test discovering related paths
	related, err := a.DiscoverRelatedProjectDirs("/Users/test/code/myrepo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should find 3 related paths (myrepo, myrepo-feature, myrepo-bugfix)
	if len(related) != 3 {
		t.Errorf("expected 3 related paths, got %d: %v", len(related), related)
	}

	// Verify unrelated paths are not included
	for _, p := range related {
		if p == "/Users/test/other" || p == "/Users/test/code/myrepo2" {
			t.Errorf("should not include unrelated path: %s", p)
		}
	}
}

func TestDiscoverRelatedProjectDirs_EmptyStorage(t *testing.T) {
	tmpDir := t.TempDir()
	a := &Adapter{
		storageDir:   tmpDir,
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}

	related, err := a.DiscoverRelatedProjectDirs("/Users/test/myrepo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(related) != 0 {
		t.Errorf("expected empty slice, got %v", related)
	}
}

func TestMalformedProjectJSON(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	validJSON := `{"id":"proj_valid","worktree":"/tmp/valid-project","vcs":"git","time":{"created":1767000000000,"updated":1767100000000}}`
	if err := os.WriteFile(filepath.Join(projectDir, "valid.json"), []byte(validJSON), 0644); err != nil {
		t.Fatalf("failed to write valid project: %v", err)
	}

	malformedJSON := `{invalid json`
	if err := os.WriteFile(filepath.Join(projectDir, "bad.json"), []byte(malformedJSON), 0644); err != nil {
		t.Fatalf("failed to write malformed project: %v", err)
	}

	a := &Adapter{
		storageDir:   tmpDir,
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}

	if err := a.loadProjects(); err != nil {
		t.Fatalf("loadProjects should not error on malformed JSON, got: %v", err)
	}

	if !a.projectsLoaded {
		t.Error("projectsLoaded should be true")
	}

	if len(a.projectIndex) != 1 {
		t.Errorf("expected 1 project in index, got %d", len(a.projectIndex))
	}

	if _, ok := a.projectIndex["/tmp/valid-project"]; !ok {
		t.Error("expected valid project to be in index")
	}
}

func TestMalformedSessionJSON(t *testing.T) {
	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "myproject")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatalf("failed to create project path: %v", err)
	}

	projectDir := filepath.Join(tmpDir, "storage", "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}
	projectJSON := fmt.Sprintf(`{"id":"proj1","worktree":"%s","vcs":"git","time":{"created":1767000000000,"updated":1767100000000}}`, projectPath)
	if err := os.WriteFile(filepath.Join(projectDir, "proj1.json"), []byte(projectJSON), 0644); err != nil {
		t.Fatalf("failed to write project file: %v", err)
	}

	sessionDir := filepath.Join(tmpDir, "storage", "session", "proj1")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("failed to create session dir: %v", err)
	}

	now := time.Now().UnixMilli()
	validSession := fmt.Sprintf(`{"id":"ses_good","title":"Good Session","parentID":"","time":{"created":%d,"updated":%d}}`, now, now)
	if err := os.WriteFile(filepath.Join(sessionDir, "ses_good.json"), []byte(validSession), 0644); err != nil {
		t.Fatalf("failed to write valid session: %v", err)
	}

	malformedSession := `{not valid json!!!`
	if err := os.WriteFile(filepath.Join(sessionDir, "ses_bad.json"), []byte(malformedSession), 0644); err != nil {
		t.Fatalf("failed to write malformed session: %v", err)
	}

	a := &Adapter{
		storageDir:   filepath.Join(tmpDir, "storage"),
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}

	sessions, err := a.Sessions(projectPath)
	if err != nil {
		t.Fatalf("Sessions should not error on malformed JSON, got: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session (malformed skipped), got %d", len(sessions))
	}

	if sessions[0].ID != "ses_good" {
		t.Errorf("expected session ID %q, got %q", "ses_good", sessions[0].ID)
	}

	if sessions[0].Name != "Good Session" {
		t.Errorf("expected session name %q, got %q", "Good Session", sessions[0].Name)
	}
}

func TestMalformedMessageJSON(t *testing.T) {
	tmpDir := t.TempDir()
	messageDir := filepath.Join(tmpDir, "message", "ses_test")
	if err := os.MkdirAll(messageDir, 0755); err != nil {
		t.Fatalf("failed to create message dir: %v", err)
	}

	now := time.Now().UnixMilli()
	validMsg := fmt.Sprintf(`{"id":"msg_good","sessionID":"ses_test","role":"user","time":{"created":%d}}`, now)
	if err := os.WriteFile(filepath.Join(messageDir, "msg_good.json"), []byte(validMsg), 0644); err != nil {
		t.Fatalf("failed to write valid message: %v", err)
	}

	malformedMsg := `{totally broken json`
	if err := os.WriteFile(filepath.Join(messageDir, "msg_bad.json"), []byte(malformedMsg), 0644); err != nil {
		t.Fatalf("failed to write malformed message: %v", err)
	}

	a := &Adapter{
		storageDir:   tmpDir,
		projectIndex: make(map[string]*Project),
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}

	msgMap, err := a.batchReadMessages(messageDir)
	if err != nil {
		t.Fatalf("batchReadMessages should not error on malformed JSON, got: %v", err)
	}

	if len(msgMap) != 1 {
		t.Fatalf("expected 1 message (malformed skipped), got %d", len(msgMap))
	}

	msg, ok := msgMap["msg_good"]
	if !ok {
		t.Fatal("expected msg_good in result map")
	}

	if msg.Role != "user" {
		t.Errorf("expected role %q, got %q", "user", msg.Role)
	}
}
