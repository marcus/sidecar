package claudecode

import (
	"os"
	"testing"
)

func TestDetect(t *testing.T) {
	a := New()

	// Get the current working directory for testing
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Try to detect sessions for current project (may or may not exist)
	found, err := a.Detect(cwd)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	t.Logf("Claude Code sessions for %s: %v", cwd, found)

	// Should not detect for non-existent project
	found, err = a.Detect("/nonexistent/path")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Error("should not find sessions for nonexistent path")
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
		t.Skip("no sessions found for testing")
	}

	t.Logf("found %d sessions", len(sessions))

	// Check first session has required fields
	s := sessions[0]
	if s.ID == "" {
		t.Error("session ID should not be empty")
	}
	if s.Name == "" {
		t.Error("session Name should not be empty")
	}
	if s.CreatedAt.IsZero() {
		t.Error("session CreatedAt should not be zero")
	}
	if s.UpdatedAt.IsZero() {
		t.Error("session UpdatedAt should not be zero")
	}

	t.Logf("newest session: %s (updated %v)", s.ID, s.UpdatedAt)
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
		t.Skip("no sessions found for testing")
	}

	// Get messages from the most recent session
	messages, err := a.Messages(sessions[0].ID)
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	if len(messages) == 0 {
		t.Skip("no messages in session")
	}

	t.Logf("found %d messages", len(messages))

	// Check first message
	m := messages[0]
	if m.ID == "" {
		t.Error("message ID should not be empty")
	}
	if m.Role != "user" && m.Role != "assistant" {
		t.Errorf("unexpected role: %s", m.Role)
	}
	if m.Timestamp.IsZero() {
		t.Error("message Timestamp should not be zero")
	}

	// Check for tool uses in assistant messages
	toolUseCount := 0
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolUses) > 0 {
			toolUseCount += len(msg.ToolUses)
		}
	}
	t.Logf("found %d tool uses across messages", toolUseCount)
}

func TestUsage(t *testing.T) {
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
		t.Skip("no sessions found for testing")
	}

	usage, err := a.Usage(sessions[0].ID)
	if err != nil {
		t.Fatalf("Usage error: %v", err)
	}

	t.Logf("usage: input=%d output=%d cache_read=%d cache_write=%d messages=%d",
		usage.TotalInputTokens, usage.TotalOutputTokens,
		usage.TotalCacheRead, usage.TotalCacheWrite,
		usage.MessageCount)

	if usage.MessageCount == 0 {
		t.Error("expected at least one message")
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
