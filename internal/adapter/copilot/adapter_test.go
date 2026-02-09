package copilot

import (
	"os"
	"testing"

	"github.com/marcus/sidecar/internal/adapter"
)

func TestAdapterInterface(t *testing.T) {
	a := New()

	// Verify adapter implements the interface
	var _ adapter.Adapter = a

	// Check basic properties
	if a.ID() != "copilot-cli" {
		t.Errorf("expected ID 'copilot-cli', got %s", a.ID())
	}
	if a.Name() != "GitHub Copilot CLI" {
		t.Errorf("expected name 'GitHub Copilot CLI', got %s", a.Name())
	}
	if a.Icon() != "⋮⋮" {
		t.Errorf("expected icon '⋮⋮', got %s", a.Icon())
	}

	// Check capabilities
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
		t.Error("should not support usage capability (not available in Copilot CLI)")
	}

	// Check WatchScope
	if scopeProvider, ok := interface{}(a).(adapter.WatchScopeProvider); ok {
		if scopeProvider.WatchScope() != adapter.WatchScopeGlobal {
			t.Error("copilot adapter should have global watch scope")
		}
	} else {
		t.Error("copilot adapter should implement WatchScopeProvider")
	}
}

func TestDetect(t *testing.T) {
	a := New()

	// Get the current working directory for testing
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Try to detect sessions for current project
	// This may or may not find sessions depending on the test environment
	found, err := a.Detect(cwd)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	t.Logf("Copilot CLI sessions for %s: %v", cwd, found)

	// Should not detect for non-existent state directory
	a.stateDir = "/nonexistent/path"
	found, err = a.Detect(cwd)
	if err != nil {
		t.Fatalf("Detect error for nonexistent path: %v", err)
	}
	if found {
		t.Error("should not find sessions when state directory doesn't exist")
	}
}

func TestSessions(t *testing.T) {
	a := New()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	sessions, err := a.Sessions(cwd)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) == 0 {
		t.Skip("no Copilot sessions found for testing")
	}

	t.Logf("found %d Copilot sessions", len(sessions))

	// Check first session has required fields
	s := sessions[0]
	if s.ID == "" {
		t.Error("session ID should not be empty")
	}
	if s.AdapterID != "copilot-cli" {
		t.Errorf("session AdapterID should be 'copilot-cli', got %s", s.AdapterID)
	}
	if s.AdapterName != "GitHub Copilot CLI" {
		t.Errorf("session AdapterName should be 'GitHub Copilot CLI', got %s", s.AdapterName)
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

	t.Logf("newest session: %s (updated %v, %d messages)", s.ID, s.UpdatedAt, s.MessageCount)
}

func TestMessages(t *testing.T) {
	a := New()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	sessions, err := a.Sessions(cwd)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) == 0 {
		t.Skip("no Copilot sessions found for testing")
	}

	// Test messages for the first session
	sessionID := sessions[0].ID
	messages, err := a.Messages(sessionID)
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	t.Logf("session %s has %d messages", sessionID, len(messages))

	if len(messages) == 0 {
		t.Skip("session has no messages")
	}

	// Verify message structure
	for i, msg := range messages {
		if msg.ID == "" {
			t.Errorf("message %d has empty ID", i)
		}
		if msg.Role != "user" && msg.Role != "assistant" {
			t.Errorf("message %d has invalid role: %s", i, msg.Role)
		}
		if msg.Timestamp.IsZero() {
			t.Errorf("message %d has zero timestamp", i)
		}

		// Check tool uses if present
		if len(msg.ToolUses) > 0 {
			t.Logf("message %d has %d tool uses", i, len(msg.ToolUses))
			for j, tool := range msg.ToolUses {
				if tool.ID == "" {
					t.Errorf("message %d tool %d has empty ID", i, j)
				}
				if tool.Name == "" {
					t.Errorf("message %d tool %d has empty Name", i, j)
				}
			}
		}
	}
}

func TestUsage(t *testing.T) {
	a := New()

	// Usage should return empty stats (not implemented for Copilot)
	stats, err := a.Usage("test-session-id")
	if err != nil {
		t.Fatalf("Usage error: %v", err)
	}

	if stats == nil {
		t.Fatal("Usage should return non-nil stats")
	}

	// All fields should be zero since Copilot doesn't expose token usage
	if stats.TotalInputTokens != 0 {
		t.Error("TotalInputTokens should be 0")
	}
	if stats.TotalOutputTokens != 0 {
		t.Error("TotalOutputTokens should be 0")
	}
}

func TestCountMessages(t *testing.T) {
	a := New()

	// Test with nonexistent file
	count := a.countMessages("/nonexistent/file.jsonl")
	if count != 0 {
		t.Errorf("expected 0 messages for nonexistent file, got %d", count)
	}
}
