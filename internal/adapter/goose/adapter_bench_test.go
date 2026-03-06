package goose

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func createGooseFixtureBenchmark(b *testing.B) (dbPath, projectPath, sessionID string) {
	b.Helper()

	tmpDir := b.TempDir()
	projectPath = filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		b.Fatalf("mkdir project: %v", err)
	}
	dbPath = filepath.Join(tmpDir, "sessions.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	stmts := []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, name TEXT, description TEXT, session_type TEXT, working_dir TEXT, created_at TEXT, updated_at TEXT, total_tokens INTEGER, input_tokens INTEGER, output_tokens INTEGER, accumulated_total_tokens INTEGER, accumulated_input_tokens INTEGER, accumulated_output_tokens INTEGER);`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, message_id TEXT, session_id TEXT NOT NULL, role TEXT NOT NULL, content_json TEXT NOT NULL, created_timestamp INTEGER NOT NULL, timestamp TEXT, metadata_json TEXT);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			b.Fatalf("create schema: %v", err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	sessionID = "ses_goose_bench"
	if _, err := db.Exec(`INSERT INTO sessions(id, name, description, session_type, working_dir, created_at, updated_at, total_tokens, input_tokens, output_tokens, accumulated_total_tokens, accumulated_input_tokens, accumulated_output_tokens) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID,
		"Bench Session",
		"",
		"user",
		projectPath,
		now.Add(-2*time.Minute).Format(time.RFC3339),
		now.Format(time.RFC3339),
		120,
		80,
		40,
		120,
		80,
		40,
	); err != nil {
		b.Fatalf("insert session: %v", err)
	}

	userContent := `[{"type":"text","text":"Please list files"}]`
	assistantContent := `[{"type":"text","text":"Sure"},{"type":"toolRequest","id":"tool_1","toolCall":{"status":"success","value":{"name":"list_files","arguments":{"path":"."}}}}, {"type":"toolResponse","id":"tool_1","toolResult":{"status":"success","value":{"content":[{"type":"text","text":"file1.go\nfile2.go"}]}}}]`
	if _, err := db.Exec(`INSERT INTO messages(message_id, session_id, role, content_json, created_timestamp, timestamp, metadata_json) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		"msg_1", sessionID, "user", userContent, now.Add(-90*time.Second).Unix(), now.Add(-90*time.Second).Format(time.RFC3339), `{"model":""}`,
	); err != nil {
		b.Fatalf("insert user message: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO messages(message_id, session_id, role, content_json, created_timestamp, timestamp, metadata_json) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		"msg_2", sessionID, "assistant", assistantContent, now.Add(-60*time.Second).Unix(), now.Add(-60*time.Second).Format(time.RFC3339), `{"model":"claude-sonnet"}`,
	); err != nil {
		b.Fatalf("insert assistant message: %v", err)
	}

	return dbPath, projectPath, sessionID
}

func BenchmarkMessagesCacheHit(b *testing.B) {
	dbPath, _, sessionID := createGooseFixtureBenchmark(b)
	a := &Adapter{dbPath: dbPath}

	_, _ = a.Messages(sessionID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msgs, err := a.Messages(sessionID)
		if err != nil {
			b.Fatalf("Messages: %v", err)
		}
		if len(msgs) == 0 {
			b.Fatal("expected messages")
		}
	}
}

func BenchmarkSessionsSingleProject(b *testing.B) {
	dbPath, projectPath, _ := createGooseFixtureBenchmark(b)
	a := &Adapter{dbPath: dbPath}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sessions, err := a.Sessions(projectPath)
		if err != nil {
			b.Fatalf("Sessions: %v", err)
		}
		if len(sessions) == 0 {
			b.Fatal("expected sessions")
		}
	}
}

func BenchmarkSessionsFiftyProject(b *testing.B) {
	tmpDir := b.TempDir()
	projectPath := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		b.Fatalf("mkdir project: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "sessions.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	stmts := []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, name TEXT, description TEXT, session_type TEXT, working_dir TEXT, created_at TEXT, updated_at TEXT, total_tokens INTEGER, input_tokens INTEGER, output_tokens INTEGER, accumulated_total_tokens INTEGER, accumulated_input_tokens INTEGER, accumulated_output_tokens INTEGER);`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, message_id TEXT, session_id TEXT NOT NULL, role TEXT NOT NULL, content_json TEXT NOT NULL, created_timestamp INTEGER NOT NULL, timestamp TEXT, metadata_json TEXT);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			b.Fatalf("create schema: %v", err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 50; i++ {
		sid := fmt.Sprintf("ses_goose_%03d", i)
		updated := now.Add(-time.Duration(i) * time.Second)
		if _, err := db.Exec(`INSERT INTO sessions(id, name, description, session_type, working_dir, created_at, updated_at, total_tokens, input_tokens, output_tokens, accumulated_total_tokens, accumulated_input_tokens, accumulated_output_tokens) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sid, fmt.Sprintf("Session %d", i), "", "user", projectPath, updated.Add(-time.Minute).Format(time.RFC3339), updated.Format(time.RFC3339), 10, 6, 4, 10, 6, 4,
		); err != nil {
			b.Fatalf("insert session %d: %v", i, err)
		}
		if _, err := db.Exec(`INSERT INTO messages(message_id, session_id, role, content_json, created_timestamp, timestamp, metadata_json) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("msg_%03d", i), sid, "user", `[{"type":"text","text":"hello"}]`, updated.Unix(), updated.Format(time.RFC3339), `{"model":"bench"}`,
		); err != nil {
			b.Fatalf("insert message %d: %v", i, err)
		}
	}

	a := &Adapter{dbPath: dbPath}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sessions, err := a.Sessions(projectPath)
		if err != nil {
			b.Fatalf("Sessions: %v", err)
		}
		if len(sessions) != 50 {
			b.Fatalf("expected 50 sessions, got %d", len(sessions))
		}
	}
}
