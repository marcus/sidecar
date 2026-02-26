package piagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	a := New()
	if a == nil {
		t.Fatal("New() returned nil")
	}
	if a.ID() != "pi-agent" {
		t.Errorf("ID() = %q, want %q", a.ID(), "pi-agent")
	}
	if a.Name() != "Pi Agent" {
		t.Errorf("Name() = %q, want %q", a.Name(), "Pi Agent")
	}
	if a.Icon() != "π" {
		t.Errorf("Icon() = %q, want %q", a.Icon(), "π")
	}
}

func TestProjectDirPath(t *testing.T) {
	a := New()

	tests := []struct {
		input    string
		wantSuffix string
	}{
		{"/home/user/project", "--home-user-project--"},
		{"/home/user/my-app", "--home-user-my-app--"},
		{"/", "----"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := a.projectDirPath(tt.input)
			if !strings.HasSuffix(got, tt.wantSuffix) {
				t.Errorf("projectDirPath(%q) = %q, want suffix %q", tt.input, got, tt.wantSuffix)
			}
		})
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

func TestDetect_NoDirectory(t *testing.T) {
	a := New()
	// Point to a non-existent directory
	a.sessionsDir = "/nonexistent/path"

	detected, err := a.Detect("/some/project")
	if err != nil {
		t.Errorf("Detect() error = %v", err)
	}
	if detected {
		t.Error("Detect() = true for non-existent directory")
	}
}

func TestSessions_WithTestData(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	sessionsDir := tmpDir
	projectDir := filepath.Join(sessionsDir, "--test-project--")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Copy test data
	testdata := "testdata/session1.jsonl"
	data, err := os.ReadFile(testdata)
	if err != nil {
		t.Skipf("test data not available: %v", err)
	}
	destPath := filepath.Join(projectDir, "2026-02-01T00-00-00Z_test-session-1.jsonl")
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	a := New()
	a.sessionsDir = sessionsDir

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions() error = %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("Sessions() returned %d sessions, want 1", len(sessions))
	}

	s := sessions[0]
	if s.ID != "test-session-1" {
		t.Errorf("session ID = %q, want %q", s.ID, "test-session-1")
	}
	if s.AdapterID != "pi-agent" {
		t.Errorf("session AdapterID = %q, want %q", s.AdapterID, "pi-agent")
	}
	if s.MessageCount != 3 { // 1 user + 2 assistant (toolResult doesn't count)
		t.Errorf("session MessageCount = %d, want 3", s.MessageCount)
	}
	if s.EstCost == 0 {
		t.Error("session EstCost = 0, want > 0")
	}
}

func TestMessages_WithTestData(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := tmpDir
	projectDir := filepath.Join(sessionsDir, "--test-project--")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	testdata := "testdata/session1.jsonl"
	data, err := os.ReadFile(testdata)
	if err != nil {
		t.Skipf("test data not available: %v", err)
	}
	destPath := filepath.Join(projectDir, "2026-02-01T00-00-00Z_test-session-1.jsonl")
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	a := New()
	a.sessionsDir = sessionsDir

	// First get sessions to populate the index
	_, err = a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions() error = %v", err)
	}

	messages, err := a.Messages("test-session-1")
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	// Should have: 1 user, 1 assistant (with tool call), 1 assistant (final response)
	// toolResult is linked to the assistant message, not a separate message
	if len(messages) != 3 {
		t.Fatalf("Messages() returned %d messages, want 3", len(messages))
	}

	// Check first message (user)
	if messages[0].Role != "user" {
		t.Errorf("messages[0].Role = %q, want %q", messages[0].Role, "user")
	}
	if !strings.Contains(messages[0].Content, "what files") {
		t.Errorf("messages[0].Content = %q, want to contain 'what files'", messages[0].Content)
	}

	// Check second message (assistant with tool call)
	if messages[1].Role != "assistant" {
		t.Errorf("messages[1].Role = %q, want %q", messages[1].Role, "assistant")
	}
	if len(messages[1].ToolUses) != 1 {
		t.Errorf("messages[1].ToolUses length = %d, want 1", len(messages[1].ToolUses))
	}
	if messages[1].ToolUses[0].Name != "bash" {
		t.Errorf("messages[1].ToolUses[0].Name = %q, want %q", messages[1].ToolUses[0].Name, "bash")
	}
	// Tool result should be linked
	if !strings.Contains(messages[1].ToolUses[0].Output, "file1.go") {
		t.Errorf("tool output not linked: %q", messages[1].ToolUses[0].Output)
	}

	// Check thinking block
	if len(messages[1].ThinkingBlocks) != 1 {
		t.Errorf("messages[1].ThinkingBlocks length = %d, want 1", len(messages[1].ThinkingBlocks))
	}

	// Check third message (final assistant response)
	if messages[2].Role != "assistant" {
		t.Errorf("messages[2].Role = %q, want %q", messages[2].Role, "assistant")
	}
	if !strings.Contains(messages[2].Content, "three files") {
		t.Errorf("messages[2].Content = %q, want to contain 'three files'", messages[2].Content)
	}
}

func TestUsage_WithTestData(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := tmpDir
	projectDir := filepath.Join(sessionsDir, "--test-project--")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	testdata := "testdata/session1.jsonl"
	data, err := os.ReadFile(testdata)
	if err != nil {
		t.Skipf("test data not available: %v", err)
	}
	destPath := filepath.Join(projectDir, "2026-02-01T00-00-00Z_test-session-1.jsonl")
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	a := New()
	a.sessionsDir = sessionsDir

	_, err = a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions() error = %v", err)
	}

	usage, err := a.Usage("test-session-1")
	if err != nil {
		t.Fatalf("Usage() error = %v", err)
	}

	if usage.MessageCount != 3 {
		t.Errorf("usage.MessageCount = %d, want 3", usage.MessageCount)
	}
	if usage.TotalInputTokens == 0 {
		t.Error("usage.TotalInputTokens = 0, want > 0")
	}
	if usage.TotalOutputTokens == 0 {
		t.Error("usage.TotalOutputTokens = 0, want > 0")
	}
}

func TestExtractSessionMetadata(t *testing.T) {
	tests := []struct {
		input       string
		wantCat     string
		wantCron    string
		wantChannel string
	}{
		{"Hello world", "interactive", "", "direct"},
		{"[cron:abc123 daily-backup] Run backup", "cron", "daily-backup", ""},
		{"[Telegram User (@handle) id:123] Hi there", "interactive", "", "telegram"},
		{"System: [something] message", "system", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input[:min(20, len(tt.input))], func(t *testing.T) {
			cat, cron, channel := extractSessionMetadata(tt.input)
			if cat != tt.wantCat {
				t.Errorf("category = %q, want %q", cat, tt.wantCat)
			}
			if cron != tt.wantCron {
				t.Errorf("cronJobName = %q, want %q", cron, tt.wantCron)
			}
			if channel != tt.wantChannel {
				t.Errorf("sourceChannel = %q, want %q", channel, tt.wantChannel)
			}
		})
	}
}

func TestStripMessagePrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello world", "Hello world"},
		{"[Telegram User] Hello", "Hello"},
		{"[cron:abc job] Task", "Task"},
		{"System: [x] Message", "Message"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripMessagePrefix(tt.input)
			if got != tt.want {
				t.Errorf("stripMessagePrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"this is a longer title", 10, "this is..."},
		{"with\nnewline", 20, "with newline"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncateTitle(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateTitle(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
