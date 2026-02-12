package copilot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/sidecar/internal/adapter"
)

func TestAdapterInterface(t *testing.T) {
	a := New()
	var _ adapter.Adapter = a

	if a.ID() != "copilot-cli" {
		t.Errorf("expected ID 'copilot-cli', got %s", a.ID())
	}
	if a.Name() != "GitHub Copilot CLI" {
		t.Errorf("expected name 'GitHub Copilot CLI', got %s", a.Name())
	}
	if a.Icon() != "⋮⋮" {
		t.Errorf("expected icon '⋮⋮', got %s", a.Icon())
	}

	caps := a.Capabilities()
	if !caps[adapter.CapSessions] {
		t.Error("should support sessions capability")
	}
	if !caps[adapter.CapMessages] {
		t.Error("should support messages capability")
	}
	if !caps[adapter.CapWatch] {
		t.Error("should support watch capability")
	}
	if caps[adapter.CapUsage] {
		t.Error("should not support usage capability")
	}

	if scopeProvider, ok := interface{}(a).(adapter.WatchScopeProvider); ok {
		if scopeProvider.WatchScope() != adapter.WatchScopeGlobal {
			t.Error("copilot adapter should have global watch scope")
		}
	} else {
		t.Error("copilot adapter should implement WatchScopeProvider")
	}
}

// setupTestSession creates a temp dir with workspace.yaml and events file for testing.
func setupTestSession(t *testing.T, eventsFixture string) (*Adapter, string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	sessionID := "test-session-001"
	sessionDir := filepath.Join(tmpDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("failed to create session dir: %v", err)
	}

	// Copy workspace.yaml
	wsData, err := os.ReadFile(filepath.Join("testdata", "workspace.yaml"))
	if err != nil {
		t.Fatalf("failed to read workspace fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "workspace.yaml"), wsData, 0644); err != nil {
		t.Fatalf("failed to write workspace.yaml: %v", err)
	}

	// Copy events file
	eventsData, err := os.ReadFile(filepath.Join("testdata", eventsFixture))
	if err != nil {
		t.Fatalf("failed to read events fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "events.jsonl"), eventsData, 0644); err != nil {
		t.Fatalf("failed to write events.jsonl: %v", err)
	}

	a := New()
	a.stateDir = tmpDir

	return a, sessionID, "/home/user/project"
}

func TestDetect(t *testing.T) {
	a, _, projectRoot := setupTestSession(t, "valid_events.jsonl")

	found, err := a.Detect(projectRoot)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !found {
		t.Error("should detect sessions for matching project root")
	}

	found, err = a.Detect("/nonexistent/project")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Error("should not detect sessions for non-matching project root")
	}

	a.stateDir = "/nonexistent/path"
	found, err = a.Detect(projectRoot)
	if err != nil {
		t.Fatalf("Detect error for nonexistent path: %v", err)
	}
	if found {
		t.Error("should not find sessions when state directory doesn't exist")
	}
}

func TestSessions(t *testing.T) {
	a, _, projectRoot := setupTestSession(t, "valid_events.jsonl")

	sessions, err := a.Sessions(projectRoot)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	s := sessions[0]
	if s.ID != "test-session-001" {
		t.Errorf("session ID should be 'test-session-001', got %s", s.ID)
	}
	if s.AdapterID != "copilot-cli" {
		t.Errorf("session AdapterID should be 'copilot-cli', got %s", s.AdapterID)
	}
	if s.AdapterName != "GitHub Copilot CLI" {
		t.Errorf("session AdapterName should be 'GitHub Copilot CLI', got %s", s.AdapterName)
	}
	if s.Name != "Test session for fixture" {
		t.Errorf("session Name should be 'Test session for fixture', got %s", s.Name)
	}
	if s.CreatedAt.IsZero() {
		t.Error("session CreatedAt should not be zero")
	}
	if s.UpdatedAt.IsZero() {
		t.Error("session UpdatedAt should not be zero")
	}
	if s.Slug == "" {
		t.Error("session Slug should not be empty")
	}
	if len(s.Slug) > 12 {
		t.Errorf("session Slug should be <= 12 chars, got %d", len(s.Slug))
	}
	if s.MessageCount != 4 {
		t.Errorf("expected 4 messages, got %d", s.MessageCount)
	}
}

func TestMessages(t *testing.T) {
	a, sessionID, projectRoot := setupTestSession(t, "valid_events.jsonl")

	// Must call Sessions first to populate sessionIndex
	_, err := a.Sessions(projectRoot)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	messages, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}

	// Check user message
	if messages[0].Role != "user" {
		t.Errorf("message 0 role should be 'user', got %s", messages[0].Role)
	}
	if messages[0].Content != "Hello, can you help me with this project?" {
		t.Errorf("message 0 content mismatch: %s", messages[0].Content)
	}
	if messages[0].ID != "msg-001" {
		t.Errorf("message 0 ID should be 'msg-001', got %s", messages[0].ID)
	}

	// Check assistant message
	if messages[1].Role != "assistant" {
		t.Errorf("message 1 role should be 'assistant', got %s", messages[1].Role)
	}
	if messages[1].Model != "claude-sonnet-4-20250514" {
		t.Errorf("message 1 model mismatch: %s", messages[1].Model)
	}

	// Check tool use in message 3
	if len(messages[3].ToolUses) != 1 {
		t.Fatalf("message 3 should have 1 tool use, got %d", len(messages[3].ToolUses))
	}
	if messages[3].ToolUses[0].Name != "view" {
		t.Errorf("tool name should be 'view', got %s", messages[3].ToolUses[0].Name)
	}
	if messages[3].ToolUses[0].ID != "tool-001" {
		t.Errorf("tool ID should be 'tool-001', got %s", messages[3].ToolUses[0].ID)
	}
}

func TestToolLinking(t *testing.T) {
	a, sessionID, projectRoot := setupTestSession(t, "tool_linking.jsonl")

	_, err := a.Sessions(projectRoot)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	messages, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	// Assistant message should have 2 tool uses with results linked
	assistantMsg := messages[1]
	if len(assistantMsg.ToolUses) != 2 {
		t.Fatalf("assistant message should have 2 tool uses, got %d", len(assistantMsg.ToolUses))
	}

	// Tool results should be linked from tool.execution_complete events
	if assistantMsg.ToolUses[0].Output != "package test\n\nfunc TestMain() {}" {
		t.Errorf("tool 1 output should be linked, got: %s", assistantMsg.ToolUses[0].Output)
	}
	if assistantMsg.ToolUses[1].Output != "package main\n\nfunc main() {}" {
		t.Errorf("tool 2 output should be linked, got: %s", assistantMsg.ToolUses[1].Output)
	}
}

func TestMalformedEvents(t *testing.T) {
	a := New()

	f, err := os.Open(filepath.Join("testdata", "malformed.jsonl"))
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer f.Close()

	messages, err := a.parseMessages(f)
	if err != nil {
		t.Fatalf("parseMessages error: %v", err)
	}

	// Should skip malformed lines and parse 2 valid messages
	if len(messages) != 2 {
		t.Errorf("expected 2 valid messages from malformed file, got %d", len(messages))
	}
}

func TestEmptyEvents(t *testing.T) {
	a := New()

	f, err := os.Open(filepath.Join("testdata", "empty.jsonl"))
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer f.Close()

	messages, err := a.parseMessages(f)
	if err != nil {
		t.Fatalf("parseMessages error: %v", err)
	}

	if len(messages) != 0 {
		t.Errorf("expected 0 messages from empty file, got %d", len(messages))
	}
}

func TestMessageCaching(t *testing.T) {
	a, sessionID, projectRoot := setupTestSession(t, "valid_events.jsonl")

	_, err := a.Sessions(projectRoot)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	// First call: full parse
	messages1, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	// Second call: should hit cache
	messages2, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages error on cached call: %v", err)
	}

	if len(messages1) != len(messages2) {
		t.Errorf("cached messages count mismatch: %d vs %d", len(messages1), len(messages2))
	}

	// Verify defensive copies (modifying returned slice shouldn't affect cache)
	if len(messages1) > 0 {
		messages1[0].Content = "MODIFIED"
		messages3, err := a.Messages(sessionID)
		if err != nil {
			t.Fatalf("Messages error: %v", err)
		}
		if messages3[0].Content == "MODIFIED" {
			t.Error("cache should return defensive copies, not references")
		}
	}
}

func TestIncrementalParsing(t *testing.T) {
	a, sessionID, projectRoot := setupTestSession(t, "valid_events.jsonl")

	_, err := a.Sessions(projectRoot)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	// First call: full parse, populates cache
	messages1, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	// Append a new message to the events file
	a.mu.RLock()
	sessionDir := a.sessionIndex[sessionID]
	a.mu.RUnlock()
	eventsFile := filepath.Join(sessionDir, "events.jsonl")

	f, err := os.OpenFile(eventsFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open events file for append: %v", err)
	}
	newEvent := `{"type":"user.message","id":"msg-005","timestamp":"2025-01-15T10:02:00Z","data":{"content":"Thanks for the help!"}}` + "\n"
	if _, err := f.WriteString(newEvent); err != nil {
		f.Close()
		t.Fatalf("failed to append event: %v", err)
	}
	f.Close()

	// Second call: should trigger incremental parse
	messages2, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages error on incremental: %v", err)
	}

	if len(messages2) != len(messages1)+1 {
		t.Errorf("expected %d messages after append, got %d", len(messages1)+1, len(messages2))
	}

	// Verify the new message
	lastMsg := messages2[len(messages2)-1]
	if lastMsg.ID != "msg-005" {
		t.Errorf("last message ID should be 'msg-005', got %s", lastMsg.ID)
	}
	if lastMsg.Content != "Thanks for the help!" {
		t.Errorf("last message content mismatch: %s", lastMsg.Content)
	}
}

func TestUsage(t *testing.T) {
	a := New()

	stats, err := a.Usage("test-session-id")
	if err != nil {
		t.Fatalf("Usage error: %v", err)
	}

	if stats == nil {
		t.Fatal("Usage should return non-nil stats")
	}

	if stats.TotalInputTokens != 0 {
		t.Error("TotalInputTokens should be 0")
	}
	if stats.TotalOutputTokens != 0 {
		t.Error("TotalOutputTokens should be 0")
	}
}

func TestCountMessages(t *testing.T) {
	a := New()

	count := a.countMessages("/nonexistent/file.jsonl")
	if count != 0 {
		t.Errorf("expected 0 messages for nonexistent file, got %d", count)
	}
}

func TestWatch(t *testing.T) {
	a, _, projectRoot := setupTestSession(t, "valid_events.jsonl")

	eventChan, closer, err := a.Watch(projectRoot)
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}
	defer closer.Close()

	if eventChan == nil {
		t.Error("event channel should not be nil")
	}
}

func TestMetadataCaching(t *testing.T) {
	a, _, projectRoot := setupTestSession(t, "valid_events.jsonl")

	// First detect call reads workspace.yaml
	found1, err := a.Detect(projectRoot)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !found1 {
		t.Error("first detect should find session")
	}

	// Second detect should use cached metadata
	found2, err := a.Detect(projectRoot)
	if err != nil {
		t.Fatalf("Detect error on cached call: %v", err)
	}
	if !found2 {
		t.Error("cached detect should find session")
	}

	// Verify metaCache has entries
	a.metaMu.RLock()
	cacheLen := len(a.metaCache)
	a.metaMu.RUnlock()

	if cacheLen == 0 {
		t.Error("metaCache should have entries after Detect calls")
	}
}
