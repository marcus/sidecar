package geminicli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	a := New()
	if a == nil {
		t.Fatal("New() returned nil")
	}
	if a.tmpDir == "" {
		t.Error("tmpDir should not be empty")
	}
}

func TestID(t *testing.T) {
	a := New()
	if a.ID() != "gemini-cli" {
		t.Errorf("ID() = %q, expected 'gemini-cli'", a.ID())
	}
}

func TestName(t *testing.T) {
	a := New()
	if a.Name() != "Gemini CLI" {
		t.Errorf("Name() = %q, expected 'Gemini CLI'", a.Name())
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

func TestProjectHash(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/Users/test/project", "c14a78844457ae0822661016ee3b5b5111bfa573cb3f17fab89fbabd816d0354"},
	}

	for _, tt := range tests {
		result := projectHash(tt.path)
		if len(result) != 64 {
			t.Errorf("projectHash(%q) length = %d, expected 64", tt.path, len(result))
		}
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		id       string
		expected string
	}{
		{"12345678", "12345678"},
		{"123456789abcdef", "12345678"},
		{"1234567", "1234567"},
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

func TestDetect_NonExistent(t *testing.T) {
	a := New()

	found, err := a.Detect("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Error("should not find sessions for nonexistent path")
	}
}

func TestParseSessionFile(t *testing.T) {
	a := New()

	testdataPath := "testdata/valid_session.json"
	session, err := a.parseSessionFile(testdataPath)
	if err != nil {
		t.Fatalf("parseSessionFile error: %v", err)
	}

	if session.SessionID != "test-session-001" {
		t.Errorf("SessionID = %q, expected 'test-session-001'", session.SessionID)
	}
	if session.ProjectHash != "abc123def456" {
		t.Errorf("ProjectHash = %q, expected 'abc123def456'", session.ProjectHash)
	}
	if len(session.Messages) != 5 {
		t.Errorf("message count = %d, expected 5", len(session.Messages))
	}
}

func TestParseSessionMetadata(t *testing.T) {
	a := New()

	testdataPath := "testdata/valid_session.json"
	meta, err := a.parseSessionMetadata(testdataPath)
	if err != nil {
		t.Fatalf("parseSessionMetadata error: %v", err)
	}

	if meta.SessionID != "test-session-001" {
		t.Errorf("SessionID = %q, expected 'test-session-001'", meta.SessionID)
	}
	// Should skip "info" messages
	if meta.MsgCount != 4 {
		t.Errorf("MsgCount = %d, expected 4 (excluding info message)", meta.MsgCount)
	}
	if meta.TotalTokens == 0 {
		t.Error("TotalTokens should not be zero")
	}
	if meta.PrimaryModel != "gemini-3-flash-preview" {
		t.Errorf("PrimaryModel = %q, expected 'gemini-3-flash-preview'", meta.PrimaryModel)
	}
}

func TestMessages(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	a := &Adapter{tmpDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	// Create project hash dir with chats subdir
	projectHash := "abc123def456"
	chatsDir := filepath.Join(tmpDir, projectHash, "chats")
	if err := os.MkdirAll(chatsDir, 0755); err != nil {
		t.Fatalf("failed to create chats dir: %v", err)
	}

	// Copy testdata file
	testdata, err := os.ReadFile("testdata/valid_session.json")
	if err != nil {
		t.Fatalf("failed to read testdata: %v", err)
	}
	sessionPath := filepath.Join(chatsDir, "session-2024-01-15T10-00-test-001.json")
	if err := os.WriteFile(sessionPath, testdata, 0644); err != nil {
		t.Fatalf("failed to write session: %v", err)
	}

	messages, err := a.Messages("test-session-001")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	// Should skip "info" messages
	if len(messages) != 4 {
		t.Errorf("message count = %d, expected 4", len(messages))
	}

	// Check first message
	if messages[0].Role != "user" {
		t.Errorf("first message role = %q, expected 'user'", messages[0].Role)
	}

	// Check assistant message has proper role mapping
	if messages[1].Role != "assistant" {
		t.Errorf("second message role = %q, expected 'assistant'", messages[1].Role)
	}

	// Check tool uses
	if len(messages[3].ToolUses) != 1 {
		t.Errorf("tool uses = %d, expected 1", len(messages[3].ToolUses))
	}
	if messages[3].ToolUses[0].Name != "read_file" {
		t.Errorf("tool name = %q, expected 'read_file'", messages[3].ToolUses[0].Name)
	}

	// Check thinking blocks
	if len(messages[1].ThinkingBlocks) != 1 {
		t.Errorf("thinking blocks = %d, expected 1", len(messages[1].ThinkingBlocks))
	}
}

func TestSessions(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	a := &Adapter{tmpDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	// Create a project hash matching our test path
	testPath := "/test/project"
	hash := projectHash(testPath)
	chatsDir := filepath.Join(tmpDir, hash, "chats")
	if err := os.MkdirAll(chatsDir, 0755); err != nil {
		t.Fatalf("failed to create chats dir: %v", err)
	}

	// Copy testdata file
	testdata, err := os.ReadFile("testdata/valid_session.json")
	if err != nil {
		t.Fatalf("failed to read testdata: %v", err)
	}
	sessionPath := filepath.Join(chatsDir, "session-2024-01-15T10-00-test-001.json")
	if err := os.WriteFile(sessionPath, testdata, 0644); err != nil {
		t.Fatalf("failed to write session: %v", err)
	}

	sessions, err := a.Sessions(testPath)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("session count = %d, expected 1", len(sessions))
	}

	s := sessions[0]
	if s.ID != "test-session-001" {
		t.Errorf("session ID = %q, expected 'test-session-001'", s.ID)
	}
	if s.AdapterID != "gemini-cli" {
		t.Errorf("AdapterID = %q, expected 'gemini-cli'", s.AdapterID)
	}
	if s.AdapterName != "Gemini CLI" {
		t.Errorf("AdapterName = %q, expected 'Gemini CLI'", s.AdapterName)
	}
}

func TestDetect(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	a := &Adapter{tmpDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	testPath := "/test/project"
	hash := projectHash(testPath)
	chatsDir := filepath.Join(tmpDir, hash, "chats")
	if err := os.MkdirAll(chatsDir, 0755); err != nil {
		t.Fatalf("failed to create chats dir: %v", err)
	}

	// Copy testdata file
	testdata, err := os.ReadFile("testdata/valid_session.json")
	if err != nil {
		t.Fatalf("failed to read testdata: %v", err)
	}
	sessionPath := filepath.Join(chatsDir, "session-2024-01-15T10-00-test-001.json")
	if err := os.WriteFile(sessionPath, testdata, 0644); err != nil {
		t.Fatalf("failed to write session: %v", err)
	}

	found, err := a.Detect(testPath)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !found {
		t.Error("should detect sessions")
	}
}

func TestUsage(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	a := &Adapter{tmpDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	projectHash := "abc123def456"
	chatsDir := filepath.Join(tmpDir, projectHash, "chats")
	if err := os.MkdirAll(chatsDir, 0755); err != nil {
		t.Fatalf("failed to create chats dir: %v", err)
	}

	testdata, err := os.ReadFile("testdata/valid_session.json")
	if err != nil {
		t.Fatalf("failed to read testdata: %v", err)
	}
	sessionPath := filepath.Join(chatsDir, "session-2024-01-15T10-00-test-001.json")
	if err := os.WriteFile(sessionPath, testdata, 0644); err != nil {
		t.Fatalf("failed to write session: %v", err)
	}

	usage, err := a.Usage("test-session-001")
	if err != nil {
		t.Fatalf("Usage error: %v", err)
	}

	if usage.MessageCount != 4 {
		t.Errorf("MessageCount = %d, expected 4", usage.MessageCount)
	}
	if usage.TotalInputTokens == 0 {
		t.Error("TotalInputTokens should not be zero")
	}
	if usage.TotalOutputTokens == 0 {
		t.Error("TotalOutputTokens should not be zero")
	}
}

func TestTypeUnmarshal(t *testing.T) {
	sessionJSON := `{
		"sessionId": "test-123",
		"projectHash": "hash-456",
		"startTime": "2024-01-15T10:00:00.000Z",
		"lastUpdated": "2024-01-15T10:05:00.000Z",
		"messages": [
			{
				"id": "msg-001",
				"timestamp": "2024-01-15T10:00:00.000Z",
				"type": "user",
				"content": "Hello"
			}
		]
	}`

	var session Session
	if err := json.Unmarshal([]byte(sessionJSON), &session); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if session.SessionID != "test-123" {
		t.Errorf("SessionID = %q, expected 'test-123'", session.SessionID)
	}
	if len(session.Messages) != 1 {
		t.Errorf("message count = %d, expected 1", len(session.Messages))
	}
	if session.Messages[0].Type != "user" {
		t.Errorf("message type = %q, expected 'user'", session.Messages[0].Type)
	}
}
