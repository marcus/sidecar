package amp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/adapter/cache"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestAdapter creates an Adapter with threadsDir pointing to a temp directory.
func newTestAdapter(t *testing.T, threadsDir string) *Adapter {
	t.Helper()
	return &Adapter{
		threadsDir:   threadsDir,
		sessionIndex: make(map[string]string),
		metaCache:    make(map[string]metaCacheEntry),
		msgCache:     cache.New[msgCacheEntry](msgCacheMaxEntries),
	}
}

// writeThread writes a Thread struct as JSON to threadsDir/T-{id}.json.
func writeThread(t *testing.T, dir string, thread Thread) string {
	t.Helper()
	data, err := json.Marshal(thread)
	if err != nil {
		t.Fatalf("marshal thread: %v", err)
	}
	path := filepath.Join(dir, thread.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write thread: %v", err)
	}
	return path
}

// mustResult marshals a ToolResult to json.RawMessage for test fixtures.
func mustResult(output string, exitCode int) json.RawMessage {
	data, _ := json.Marshal(ToolResult{Output: output, ExitCode: exitCode})
	return data
}

// makeThread builds a realistic Thread with sensible defaults.
func makeThread(id string, projectURI string, msgs []Message, created int64) Thread {
	thread := Thread{
		V:        5,
		ID:       id,
		Created:  created,
		Messages: msgs,
	}
	if projectURI != "" {
		thread.Env = &Env{
			Initial: &EnvInitial{
				Trees: []Tree{
					{DisplayName: "project", URI: projectURI},
				},
			},
		}
	}
	return thread
}

// ts returns a Unix millisecond timestamp for the given offset from a base.
func ts(base int64, offsetSec int) int64 {
	return base + int64(offsetSec)*1000
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const (
	baseTime = int64(1770612513442) // realistic Amp timestamp
)

func fixtureSimpleThread(projectDir string) Thread {
	projectURI := "file://" + projectDir
	return makeThread("T-test-thread-001", projectURI, []Message{
		{
			Role:      "user",
			MessageID: 0,
			Content: []ContentBlock{
				{Type: "text", Text: "Hello, can you help me with this project?"},
			},
			Meta: &MessageMeta{SentAt: baseTime},
		},
		{
			Role:      "assistant",
			MessageID: 1,
			Content: []ContentBlock{
				{Type: "thinking", Thinking: "Let me analyze the request."},
				{Type: "text", Text: "Of course! I'd be happy to help."},
			},
			Usage: &Usage{
				Model:            "claude-opus-4-6",
				InputTokens:      80,
				OutputTokens:     50,
				TotalInputTokens: 100,
				CacheReadInputTokens:     10,
				CacheCreationInputTokens: 5,
			},
			State: &MessageState{Type: "complete", StopReason: "end_turn"},
		},
	}, baseTime)
}

func fixtureToolUseThread(projectDir string) Thread {
	projectURI := "file://" + projectDir
	return makeThread("T-test-thread-002", projectURI, []Message{
		{
			Role:      "user",
			MessageID: 0,
			Content: []ContentBlock{
				{Type: "text", Text: "List the files in this directory"},
			},
			Meta: &MessageMeta{SentAt: baseTime},
		},
		{
			Role:      "assistant",
			MessageID: 1,
			Content: []ContentBlock{
				{Type: "text", Text: "I'll list the files for you."},
				{
					Type:    "tool_use",
					BlockID: "tool_abc123",
					Name:    "Bash",
					Input:   json.RawMessage(`{"command":"ls -la"}`),
				},
			},
			Usage: &Usage{
				Model:            "claude-opus-4-6",
				InputTokens:      120,
				OutputTokens:     60,
				TotalInputTokens: 150,
			},
			State: &MessageState{Type: "complete", StopReason: "tool_use"},
		},
		{
			Role:      "user",
			MessageID: 2,
			Content: []ContentBlock{
				{
					Type:      "tool_result",
					ToolUseID: "tool_abc123",
					Run: &ToolRun{
						Status: "done",
						Result: mustResult("total 8\ndrwxr-xr-x  3 user staff 96 Jan  1 00:00 .\ndrwxr-xr-x  5 user staff 160 Jan  1 00:00 ..", 0),
					},
				},
			},
		},
		{
			Role:      "assistant",
			MessageID: 3,
			Content: []ContentBlock{
				{Type: "text", Text: "The directory listing shows the files."},
			},
			Usage: &Usage{
				Model:            "claude-opus-4-6",
				InputTokens:      200,
				OutputTokens:     40,
				TotalInputTokens: 250,
			},
			State: &MessageState{Type: "complete", StopReason: "end_turn"},
		},
	}, baseTime)
}

func fixtureMultipleToolsThread(projectDir string) Thread {
	projectURI := "file://" + projectDir
	return makeThread("T-test-thread-003", projectURI, []Message{
		{
			Role:      "user",
			MessageID: 0,
			Content: []ContentBlock{
				{Type: "text", Text: "Read file and run test"},
			},
			Meta: &MessageMeta{SentAt: baseTime},
		},
		{
			Role:      "assistant",
			MessageID: 1,
			Content: []ContentBlock{
				{
					Type:    "tool_use",
					BlockID: "tool_read1",
					Name:    "Read",
					Input:   json.RawMessage(`{"file_path":"main.go"}`),
				},
				{
					Type:    "tool_use",
					BlockID: "tool_bash1",
					Name:    "Bash",
					Input:   json.RawMessage(`{"command":"go test ./..."}`),
				},
			},
			Usage: &Usage{
				Model:            "claude-opus-4-6",
				InputTokens:      300,
				OutputTokens:     80,
				TotalInputTokens: 350,
			},
		},
		{
			Role:      "user",
			MessageID: 2,
			Content: []ContentBlock{
				{
					Type:      "tool_result",
					ToolUseID: "tool_read1",
					Run: &ToolRun{
						Status: "done",
						Result: mustResult("package main\n\nfunc main() {}", 0),
					},
				},
				{
					Type:      "tool_result",
					ToolUseID: "tool_bash1",
					Run: &ToolRun{
						Status: "done",
						Result: mustResult("FAIL: TestFoo", 1),
					},
				},
			},
		},
		{
			Role:      "assistant",
			MessageID: 3,
			Content: []ContentBlock{
				{Type: "text", Text: "The test failed. Let me fix it."},
			},
			Usage: &Usage{
				Model:            "claude-opus-4-6",
				InputTokens:      500,
				OutputTokens:     100,
				TotalInputTokens: 600,
			},
		},
	}, baseTime)
}

func fixtureErrorToolThread(projectDir string) Thread {
	projectURI := "file://" + projectDir
	return makeThread("T-test-thread-004", projectURI, []Message{
		{
			Role:      "user",
			MessageID: 0,
			Content: []ContentBlock{
				{Type: "text", Text: "Run failing command"},
			},
			Meta: &MessageMeta{SentAt: baseTime},
		},
		{
			Role:      "assistant",
			MessageID: 1,
			Content: []ContentBlock{
				{
					Type:    "tool_use",
					BlockID: "tool_fail1",
					Name:    "Bash",
					Input:   json.RawMessage(`{"command":"exit 1"}`),
				},
			},
			Usage: &Usage{
				Model:            "claude-opus-4-6",
				InputTokens:      50,
				OutputTokens:     20,
				TotalInputTokens: 60,
			},
		},
		{
			Role:      "user",
			MessageID: 2,
			Content: []ContentBlock{
				{
					Type:      "tool_result",
					ToolUseID: "tool_fail1",
					Run: &ToolRun{
						Status: "done",
						Result: mustResult("error: command failed", 1),
					},
				},
			},
		},
	}, baseTime)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	a := New()
	if a == nil {
		t.Fatal("New() returned nil")
	}
	if a.threadsDir == "" {
		t.Error("threadsDir should not be empty")
	}
	if a.sessionIndex == nil {
		t.Error("sessionIndex should be initialized")
	}
	if a.metaCache == nil {
		t.Error("metaCache should be initialized")
	}
	if a.msgCache == nil {
		t.Error("msgCache should be initialized")
	}
}

func TestID(t *testing.T) {
	a := New()
	if got := a.ID(); got != "amp" {
		t.Errorf("ID() = %q, want %q", got, "amp")
	}
}

func TestName(t *testing.T) {
	a := New()
	if got := a.Name(); got != "Amp" {
		t.Errorf("Name() = %q, want %q", got, "Amp")
	}
}

func TestIcon(t *testing.T) {
	a := New()
	icon := a.Icon()
	if icon == "" {
		t.Error("Icon() should not be empty")
	}
}

func TestCapabilities(t *testing.T) {
	a := New()
	caps := a.Capabilities()

	if !caps[adapter.CapSessions] {
		t.Error("expected sessions capability")
	}
	if !caps[adapter.CapMessages] {
		t.Error("expected messages capability")
	}
	if !caps[adapter.CapUsage] {
		t.Error("expected usage capability")
	}
	if !caps[adapter.CapWatch] {
		t.Error("expected watch capability")
	}
}

// ---------------------------------------------------------------------------
// Detect
// ---------------------------------------------------------------------------

func TestDetect_MatchingProject(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	found, err := a.Detect(projectDir)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !found {
		t.Error("expected Detect to return true for matching project")
	}
}

func TestDetect_NoMatchingProject(t *testing.T) {
	projectDir := t.TempDir()
	otherDir := t.TempDir()
	threadsDir := t.TempDir()

	// Write a thread for projectDir
	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	found, err := a.Detect(otherDir)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Error("expected Detect to return false for non-matching project")
	}
}

func TestDetect_EmptyThreadsDir(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	a := newTestAdapter(t, threadsDir)
	found, err := a.Detect(projectDir)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Error("expected Detect to return false for empty threads directory")
	}
}

func TestDetect_NonexistentThreadsDir(t *testing.T) {
	a := newTestAdapter(t, "/nonexistent/path/to/threads")
	found, err := a.Detect("/some/project")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Error("expected Detect to return false for nonexistent threads directory")
	}
}

func TestDetect_ThreadWithoutEnv(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	// Thread with no env info -- cannot match any project
	thread := makeThread("T-no-env-001", "", []Message{
		{Role: "user", MessageID: 0, Content: []ContentBlock{{Type: "text", Text: "hello"}}},
	}, baseTime)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	found, err := a.Detect(projectDir)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Error("expected Detect to return false for thread without env")
	}
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

func TestSessions_ReturnsMatchingSessions(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread1 := fixtureSimpleThread(projectDir)
	thread2 := fixtureToolUseThread(projectDir)
	writeThread(t, threadsDir, thread1)
	writeThread(t, threadsDir, thread2)

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Check sorting: UpdatedAt descending
	if sessions[0].UpdatedAt.Before(sessions[1].UpdatedAt) {
		t.Error("sessions should be sorted by UpdatedAt descending")
	}
}

func TestSessions_FieldsPopulated(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	s := sessions[0]

	if s.ID != "T-test-thread-001" {
		t.Errorf("ID = %q, want %q", s.ID, "T-test-thread-001")
	}
	if s.AdapterID != "amp" {
		t.Errorf("AdapterID = %q, want %q", s.AdapterID, "amp")
	}
	if s.AdapterName != "Amp" {
		t.Errorf("AdapterName = %q, want %q", s.AdapterName, "Amp")
	}
	if s.AdapterIcon == "" {
		t.Error("AdapterIcon should not be empty")
	}
	if s.FileSize <= 0 {
		t.Errorf("FileSize should be > 0, got %d", s.FileSize)
	}
	if s.Path == "" {
		t.Error("Path should not be empty")
	}
	if s.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", s.MessageCount)
	}
	if s.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if s.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
	if s.Name == "" {
		t.Error("Name should not be empty")
	}
	if s.TotalTokens <= 0 {
		t.Error("TotalTokens should be > 0")
	}
}

func TestSessions_ExcludesNonMatching(t *testing.T) {
	projectDir := t.TempDir()
	otherDir := t.TempDir()
	threadsDir := t.TempDir()

	// One thread matches, one doesn't
	matching := fixtureSimpleThread(projectDir)
	nonMatching := fixtureToolUseThread(otherDir)
	writeThread(t, threadsDir, matching)
	writeThread(t, threadsDir, nonMatching)

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != "T-test-thread-001" {
		t.Errorf("expected matching session, got ID %q", sessions[0].ID)
	}
}

func TestSessions_ExcludesEmptyThreads(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()
	projectURI := "file://" + projectDir

	// Thread with no messages
	empty := makeThread("T-empty-001", projectURI, nil, baseTime)
	writeThread(t, threadsDir, empty)

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions for empty thread, got %d", len(sessions))
	}
}

func TestSessions_TitleTruncation(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()
	projectURI := "file://" + projectDir

	// Thread with very long first user message
	longMsg := strings.Repeat("word ", 100)
	thread := makeThread("T-long-title-001", projectURI, []Message{
		{Role: "user", MessageID: 0, Content: []ContentBlock{{Type: "text", Text: longMsg}}, Meta: &MessageMeta{SentAt: baseTime}},
		{Role: "assistant", MessageID: 1, Content: []ContentBlock{{Type: "text", Text: "ok"}},
			Usage: &Usage{Model: "claude-opus-4-6", OutputTokens: 1, TotalInputTokens: 1}},
	}, baseTime)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	// Session name should be truncated to 50 chars
	if len(sessions[0].Name) > 50 {
		t.Errorf("session name should be at most 50 chars, got %d: %q", len(sessions[0].Name), sessions[0].Name)
	}
}

func TestSessions_NonexistentThreadsDir(t *testing.T) {
	a := newTestAdapter(t, "/nonexistent/path/to/threads")
	sessions, err := a.Sessions("/some/project")
	if err != nil {
		t.Fatalf("Sessions should not error for nonexistent dir: %v", err)
	}
	if sessions != nil {
		t.Errorf("expected nil, got %v", sessions)
	}
}

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

func TestMessages_SimpleConversation(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	// Populate the session index
	_, _ = a.Sessions(projectDir)

	msgs, err := a.Messages("T-test-thread-001")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// User message
	user := msgs[0]
	if user.Role != "user" {
		t.Errorf("first message role = %q, want %q", user.Role, "user")
	}
	if user.Content != "Hello, can you help me with this project?" {
		t.Errorf("user content = %q, want user prompt text", user.Content)
	}
	if user.ID != "T-test-thread-001-0" {
		t.Errorf("user ID = %q, want %q", user.ID, "T-test-thread-001-0")
	}

	// Assistant message
	asst := msgs[1]
	if asst.Role != "assistant" {
		t.Errorf("second message role = %q, want %q", asst.Role, "assistant")
	}
	if asst.Content != "Of course! I'd be happy to help." {
		t.Errorf("assistant content = %q", asst.Content)
	}
	if asst.Model != "claude-opus-4-6" {
		t.Errorf("assistant model = %q, want %q", asst.Model, "claude-opus-4-6")
	}
}

func TestMessages_ThinkingBlocks(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	msgs, err := a.Messages("T-test-thread-001")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	asst := msgs[1]
	if len(asst.ThinkingBlocks) != 1 {
		t.Fatalf("expected 1 thinking block, got %d", len(asst.ThinkingBlocks))
	}
	if asst.ThinkingBlocks[0].Content != "Let me analyze the request." {
		t.Errorf("thinking content = %q", asst.ThinkingBlocks[0].Content)
	}
	if asst.ThinkingBlocks[0].TokenCount <= 0 {
		t.Error("thinking block should have estimated token count > 0")
	}
}

func TestMessages_ContentBlocks(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	msgs, err := a.Messages("T-test-thread-001")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	asst := msgs[1]
	// Assistant should have thinking + text content blocks
	if len(asst.ContentBlocks) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(asst.ContentBlocks))
	}
	if asst.ContentBlocks[0].Type != "thinking" {
		t.Errorf("first content block type = %q, want %q", asst.ContentBlocks[0].Type, "thinking")
	}
	if asst.ContentBlocks[1].Type != "text" {
		t.Errorf("second content block type = %q, want %q", asst.ContentBlocks[1].Type, "text")
	}
}

func TestMessages_ToolUsePairing(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureToolUseThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	msgs, err := a.Messages("T-test-thread-002")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	// Should have: user msg, assistant msg (with tool), assistant msg (follow-up)
	// Tool-result-only user messages are skipped.
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// Second message is the assistant with tool_use
	toolMsg := msgs[1]
	if len(toolMsg.ToolUses) != 1 {
		t.Fatalf("expected 1 tool use, got %d", len(toolMsg.ToolUses))
	}

	tu := toolMsg.ToolUses[0]
	if tu.ID != "tool_abc123" {
		t.Errorf("tool use ID = %q, want %q", tu.ID, "tool_abc123")
	}
	if tu.Name != "Bash" {
		t.Errorf("tool use name = %q, want %q", tu.Name, "Bash")
	}
	if tu.Input == "" {
		t.Error("tool use input should not be empty")
	}
	if tu.Output == "" {
		t.Error("tool use output should not be empty (should be paired with result)")
	}
}

func TestMessages_ToolUseContentBlock(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureToolUseThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	msgs, err := a.Messages("T-test-thread-002")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	toolMsg := msgs[1]
	// Should have text + tool_use content blocks
	var toolBlock *adapter.ContentBlock
	for i, cb := range toolMsg.ContentBlocks {
		if cb.Type == "tool_use" {
			toolBlock = &toolMsg.ContentBlocks[i]
			break
		}
	}
	if toolBlock == nil {
		t.Fatal("expected tool_use content block")
	}
	if toolBlock.ToolName != "Bash" {
		t.Errorf("tool content block name = %q, want %q", toolBlock.ToolName, "Bash")
	}
	if toolBlock.ToolOutput == "" {
		t.Error("tool content block should have output")
	}
	if toolBlock.IsError {
		t.Error("successful tool should not be marked as error")
	}
}

func TestMessages_ToolErrorMarked(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureErrorToolThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	msgs, err := a.Messages("T-test-thread-004")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	// Find the assistant message with tool use
	var asstMsg *adapter.Message
	for i, m := range msgs {
		if m.Role == "assistant" && len(m.ToolUses) > 0 {
			asstMsg = &msgs[i]
			break
		}
	}
	if asstMsg == nil {
		t.Fatal("expected assistant message with tool uses")
	}

	// Check tool_use content block has IsError=true
	var toolBlock *adapter.ContentBlock
	for i, cb := range asstMsg.ContentBlocks {
		if cb.Type == "tool_use" {
			toolBlock = &asstMsg.ContentBlocks[i]
			break
		}
	}
	if toolBlock == nil {
		t.Fatal("expected tool_use content block")
	}
	if !toolBlock.IsError {
		t.Error("failed tool should be marked as error")
	}
}

func TestMessages_MultipleToolsPerMessage(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureMultipleToolsThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	msgs, err := a.Messages("T-test-thread-003")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	// Find the assistant message with tools
	var asstMsg *adapter.Message
	for i, m := range msgs {
		if m.Role == "assistant" && len(m.ToolUses) > 0 {
			asstMsg = &msgs[i]
			break
		}
	}
	if asstMsg == nil {
		t.Fatal("expected assistant message with tool uses")
	}
	if len(asstMsg.ToolUses) != 2 {
		t.Fatalf("expected 2 tool uses, got %d", len(asstMsg.ToolUses))
	}

	// Check that both tools have paired output
	for i, tu := range asstMsg.ToolUses {
		if tu.Output == "" {
			t.Errorf("tool use %d (%s) should have output", i, tu.Name)
		}
	}

	// Check the error tool: Bash with exit code 1
	var bashTool *adapter.ToolUse
	for i, tu := range asstMsg.ToolUses {
		if tu.Name == "Bash" {
			bashTool = &asstMsg.ToolUses[i]
			break
		}
	}
	if bashTool == nil {
		t.Fatal("expected Bash tool use")
	}

	// Check content blocks for the error tool
	var bashBlock *adapter.ContentBlock
	for i, cb := range asstMsg.ContentBlocks {
		if cb.Type == "tool_use" && cb.ToolName == "Bash" {
			bashBlock = &asstMsg.ContentBlocks[i]
			break
		}
	}
	if bashBlock == nil {
		t.Fatal("expected Bash tool_use content block")
	}
	if !bashBlock.IsError {
		t.Error("Bash tool with exit code 1 should be marked as error")
	}
}

func TestMessages_SkipsToolResultOnlyUserMessages(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureToolUseThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	msgs, err := a.Messages("T-test-thread-002")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	// Verify no message is a pure tool_result user message
	for _, m := range msgs {
		if m.Role == "user" && m.Content == "" && len(m.ToolUses) == 0 {
			// This would be a tool_result-only message that should have been skipped
			t.Error("found tool_result-only user message that should have been skipped")
		}
	}
}

func TestMessages_TokenUsage(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	msgs, err := a.Messages("T-test-thread-001")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	asst := msgs[1]
	if asst.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100 (TotalInputTokens)", asst.InputTokens)
	}
	if asst.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", asst.OutputTokens)
	}
	if asst.CacheRead != 10 {
		t.Errorf("CacheRead = %d, want 10", asst.CacheRead)
	}
	if asst.CacheWrite != 5 {
		t.Errorf("CacheWrite = %d, want 5", asst.CacheWrite)
	}
}

func TestMessages_UnknownSession(t *testing.T) {
	threadsDir := t.TempDir()

	a := newTestAdapter(t, threadsDir)
	msgs, err := a.Messages("nonexistent-session")
	if err != nil {
		t.Fatalf("Messages should not error for unknown session: %v", err)
	}
	if msgs != nil {
		t.Errorf("expected nil for unknown session, got %v", msgs)
	}
}

// ---------------------------------------------------------------------------
// Usage
// ---------------------------------------------------------------------------

func TestUsage_AggregatesTokens(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureToolUseThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	usage, err := a.Usage("T-test-thread-002")
	if err != nil {
		t.Fatalf("Usage error: %v", err)
	}

	// Thread has two assistant messages with usage:
	// msg1: TotalInputTokens=150, OutputTokens=60
	// msg3: TotalInputTokens=250, OutputTokens=40
	if usage.TotalInputTokens != 400 {
		t.Errorf("TotalInputTokens = %d, want 400", usage.TotalInputTokens)
	}
	if usage.TotalOutputTokens != 100 {
		t.Errorf("TotalOutputTokens = %d, want 100", usage.TotalOutputTokens)
	}
	// 3 real messages (user + 2 assistant), tool_result message is skipped
	if usage.MessageCount != 3 {
		t.Errorf("MessageCount = %d, want 3", usage.MessageCount)
	}
}

func TestUsage_SimpleThread(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	usage, err := a.Usage("T-test-thread-001")
	if err != nil {
		t.Fatalf("Usage error: %v", err)
	}

	if usage.TotalInputTokens != 100 {
		t.Errorf("TotalInputTokens = %d, want 100", usage.TotalInputTokens)
	}
	if usage.TotalOutputTokens != 50 {
		t.Errorf("TotalOutputTokens = %d, want 50", usage.TotalOutputTokens)
	}
	if usage.TotalCacheRead != 10 {
		t.Errorf("TotalCacheRead = %d, want 10", usage.TotalCacheRead)
	}
	if usage.TotalCacheWrite != 5 {
		t.Errorf("TotalCacheWrite = %d, want 5", usage.TotalCacheWrite)
	}
	if usage.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", usage.MessageCount)
	}
}

// ---------------------------------------------------------------------------
// Cache behavior
// ---------------------------------------------------------------------------

func TestMessageCache_HitOnUnchangedFile(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	// First call populates cache
	msgs1, err := a.Messages("T-test-thread-001")
	if err != nil {
		t.Fatalf("first Messages call: %v", err)
	}
	if len(msgs1) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs1))
	}

	// Second call should hit cache
	msgs2, err := a.Messages("T-test-thread-001")
	if err != nil {
		t.Fatalf("second Messages call: %v", err)
	}
	if len(msgs2) != len(msgs1) {
		t.Errorf("cache hit should return same count: %d vs %d", len(msgs2), len(msgs1))
	}

	// Content should match
	for i := range msgs1 {
		if msgs1[i].Content != msgs2[i].Content {
			t.Errorf("message %d content mismatch on cache hit", i)
		}
	}
}

func TestMessageCache_InvalidatedOnFileChange(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	path := writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	// First call populates cache
	msgs1, err := a.Messages("T-test-thread-001")
	if err != nil {
		t.Fatalf("first Messages call: %v", err)
	}
	if len(msgs1) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs1))
	}

	// Modify the file: add another assistant message
	thread.Messages = append(thread.Messages, Message{
		Role:      "assistant",
		MessageID: 2,
		Content: []ContentBlock{
			{Type: "text", Text: "Anything else I can help with?"},
		},
		Usage: &Usage{Model: "claude-opus-4-6", OutputTokens: 10, TotalInputTokens: 200},
	})
	data, _ := json.Marshal(thread)
	// Ensure modtime changes by waiting briefly
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("rewrite thread: %v", err)
	}

	// Third call should detect file change and re-parse
	msgs2, err := a.Messages("T-test-thread-001")
	if err != nil {
		t.Fatalf("second Messages call after modification: %v", err)
	}
	if len(msgs2) != 3 {
		t.Errorf("expected 3 messages after modification, got %d", len(msgs2))
	}
}

func TestMetaCache_HitOnUnchangedFile(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)

	// First call populates meta cache
	sessions1, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("first Sessions call: %v", err)
	}

	// Second call should use cached metadata
	sessions2, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("second Sessions call: %v", err)
	}

	if len(sessions1) != len(sessions2) {
		t.Errorf("cache hit should return same count: %d vs %d", len(sessions1), len(sessions2))
	}
	if len(sessions1) > 0 && sessions1[0].ID != sessions2[0].ID {
		t.Error("cached session ID mismatch")
	}
}

// ---------------------------------------------------------------------------
// Project matching
// ---------------------------------------------------------------------------

func TestProjectMatching_ExactMatch(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()
	projectURI := "file://" + projectDir

	thread := makeThread("T-match-001", projectURI, []Message{
		{Role: "user", MessageID: 0, Content: []ContentBlock{{Type: "text", Text: "test"}}, Meta: &MessageMeta{SentAt: baseTime}},
		{Role: "assistant", MessageID: 1, Content: []ContentBlock{{Type: "text", Text: "ok"}},
			Usage: &Usage{Model: "m", OutputTokens: 1, TotalInputTokens: 1}},
	}, baseTime)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session for exact match, got %d", len(sessions))
	}
}

func TestProjectMatching_SubdirectoryMatch(t *testing.T) {
	projectDir := t.TempDir()
	subDir := filepath.Join(projectDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	threadsDir := t.TempDir()
	subURI := "file://" + subDir

	thread := makeThread("T-sub-001", subURI, []Message{
		{Role: "user", MessageID: 0, Content: []ContentBlock{{Type: "text", Text: "test"}}, Meta: &MessageMeta{SentAt: baseTime}},
		{Role: "assistant", MessageID: 1, Content: []ContentBlock{{Type: "text", Text: "ok"}},
			Usage: &Usage{Model: "m", OutputTokens: 1, TotalInputTokens: 1}},
	}, baseTime)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session for subdirectory match, got %d", len(sessions))
	}
}

func TestProjectMatching_NoMatchDifferentProject(t *testing.T) {
	projectDir := t.TempDir()
	otherDir := t.TempDir()
	threadsDir := t.TempDir()
	otherURI := "file://" + otherDir

	thread := makeThread("T-other-001", otherURI, []Message{
		{Role: "user", MessageID: 0, Content: []ContentBlock{{Type: "text", Text: "test"}}, Meta: &MessageMeta{SentAt: baseTime}},
		{Role: "assistant", MessageID: 1, Content: []ContentBlock{{Type: "text", Text: "ok"}},
			Usage: &Usage{Model: "m", OutputTokens: 1, TotalInputTokens: 1}},
	}, baseTime)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions for different project, got %d", len(sessions))
	}
}

func TestProjectMatching_MultipleTrees(t *testing.T) {
	projectDir := t.TempDir()
	otherDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := Thread{
		V:       5,
		ID:      "T-multi-tree-001",
		Created: baseTime,
		Messages: []Message{
			{Role: "user", MessageID: 0, Content: []ContentBlock{{Type: "text", Text: "test"}}, Meta: &MessageMeta{SentAt: baseTime}},
			{Role: "assistant", MessageID: 1, Content: []ContentBlock{{Type: "text", Text: "ok"}},
				Usage: &Usage{Model: "m", OutputTokens: 1, TotalInputTokens: 1}},
		},
		Env: &Env{
			Initial: &EnvInitial{
				Trees: []Tree{
					{DisplayName: "other", URI: "file://" + otherDir},
					{DisplayName: "project", URI: "file://" + projectDir},
				},
			},
		},
	}
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session when project is in trees list, got %d", len(sessions))
	}
}

// ---------------------------------------------------------------------------
// SessionByID (TargetedRefresher)
// ---------------------------------------------------------------------------

func TestSessionByID_ReturnsSession(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	// First populate the session index via Sessions()
	_, _ = a.Sessions(projectDir)

	session, err := a.SessionByID("T-test-thread-001")
	if err != nil {
		t.Fatalf("SessionByID error: %v", err)
	}
	if session == nil {
		t.Fatal("SessionByID returned nil")
	}
	if session.ID != "T-test-thread-001" {
		t.Errorf("ID = %q, want %q", session.ID, "T-test-thread-001")
	}
	if session.AdapterID != "amp" {
		t.Errorf("AdapterID = %q, want %q", session.AdapterID, "amp")
	}
	if session.AdapterName != "Amp" {
		t.Errorf("AdapterName = %q, want %q", session.AdapterName, "Amp")
	}
	if session.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", session.MessageCount)
	}
	if session.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if session.TotalTokens <= 0 {
		t.Error("TotalTokens should be > 0")
	}
}

func TestSessionByID_UnknownSession(t *testing.T) {
	threadsDir := t.TempDir()
	a := newTestAdapter(t, threadsDir)

	_, err := a.SessionByID("nonexistent")
	if err == nil {
		t.Error("expected error for unknown session")
	}
}

func TestSessionByID_DirectFileLookup(t *testing.T) {
	threadsDir := t.TempDir()
	projectDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	// Create adapter WITHOUT calling Sessions() first -- tests the direct path fallback
	a := newTestAdapter(t, threadsDir)

	session, err := a.SessionByID("T-test-thread-001")
	if err != nil {
		t.Fatalf("SessionByID (direct lookup) error: %v", err)
	}
	if session == nil {
		t.Fatal("SessionByID returned nil")
	}
	if session.ID != "T-test-thread-001" {
		t.Errorf("ID = %q, want %q", session.ID, "T-test-thread-001")
	}
}

// ---------------------------------------------------------------------------
// TargetedRefresher interface compliance
// ---------------------------------------------------------------------------

func TestTargetedRefresherInterface(t *testing.T) {
	a := New()
	var _ adapter.TargetedRefresher = a
}

// ---------------------------------------------------------------------------
// WatchScope
// ---------------------------------------------------------------------------

func TestWatchScope(t *testing.T) {
	a := New()
	if got := a.WatchScope(); got != adapter.WatchScopeGlobal {
		t.Errorf("WatchScope() = %v, want WatchScopeGlobal", got)
	}
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestIsThreadFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"T-abc123.json", true},
		{"T-550e8400-e29b-41d4-a716-446655440000.json", true},
		{"T-.json", true},
		{"thread.json", false},
		{"T-abc123.txt", false},
		{"T-abc123", false},
		{".json", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := isThreadFile(tt.name); got != tt.want {
			t.Errorf("isThreadFile(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestIsToolResultOnly(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			name: "tool result only",
			msg:  Message{Content: []ContentBlock{{Type: "tool_result"}}},
			want: true,
		},
		{
			name: "multiple tool results",
			msg:  Message{Content: []ContentBlock{{Type: "tool_result"}, {Type: "tool_result"}}},
			want: true,
		},
		{
			name: "text only",
			msg:  Message{Content: []ContentBlock{{Type: "text", Text: "hello"}}},
			want: false,
		},
		{
			name: "mixed content",
			msg:  Message{Content: []ContentBlock{{Type: "text", Text: "hello"}, {Type: "tool_result"}}},
			want: false,
		},
		{
			name: "empty content",
			msg:  Message{Content: nil},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isToolResultOnly(tt.msg); got != tt.want {
				t.Errorf("isToolResultOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUriToPath(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"file:///Users/test/project", "/Users/test/project"},
		{"file:///tmp/dir", "/tmp/dir"},
		{"https://example.com", ""},
		{"", ""},
		{"not-a-uri", ""},
	}

	for _, tt := range tests {
		if got := uriToPath(tt.uri); got != tt.want {
			t.Errorf("uriToPath(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}

func TestPathMatchesProject(t *testing.T) {
	tests := []struct {
		projectRoot string
		treePath    string
		want        bool
	}{
		{"/Users/test/project", "/Users/test/project", true},
		{"/Users/test/project", "/Users/test/project/subdir", true},
		{"/Users/test/project", "/Users/test/other", false},
		{"/Users/test/project", "/Users/test/project-other", false},
		{"", "/Users/test/project", false},
		{"/Users/test/project", "", false},
	}

	for _, tt := range tests {
		if got := pathMatchesProject(tt.projectRoot, tt.treePath); got != tt.want {
			t.Errorf("pathMatchesProject(%q, %q) = %v, want %v",
				tt.projectRoot, tt.treePath, got, tt.want)
		}
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"T-550e8400-e29b-41d4-a716-446655440000", "T-550e8400-e"},
		{"T-abc", "T-abc"},
		{"123456789012", "123456789012"},
		{"12345678901234567890", "123456789012"},
		{"short", "short"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := shortID(tt.id); got != tt.want {
			t.Errorf("shortID(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short text", 50, "short text"},
		{"exactly fifty chars padded out to fill the space!!", 50, "exactly fifty chars padded out to fill the space!!"},
		{"this is a very long title that should be truncated to fit within the limit", 50, "this is a very long title that should be trunca..."},
		{"multiline\ntext\nhere", 50, "multiline text here"},
		{"  trimmed  ", 50, "trimmed"},
		{"", 50, ""},
	}

	for _, tt := range tests {
		got := truncateTitle(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateTitle(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
		if len(got) > tt.maxLen {
			t.Errorf("truncateTitle(%q, %d) result length %d exceeds max %d", tt.input, tt.maxLen, len(got), tt.maxLen)
		}
	}
}

func TestCopyMessages(t *testing.T) {
	original := []adapter.Message{
		{
			ID:      "1",
			Role:    "assistant",
			Content: "hello",
			ToolUses: []adapter.ToolUse{
				{ID: "t1", Name: "Bash", Input: "{}", Output: "ok"},
			},
			ThinkingBlocks: []adapter.ThinkingBlock{
				{Content: "thinking", TokenCount: 10},
			},
			ContentBlocks: []adapter.ContentBlock{
				{Type: "text", Text: "hello"},
			},
		},
	}

	copied := copyMessages(original)
	if len(copied) != len(original) {
		t.Fatalf("copy length mismatch: %d vs %d", len(copied), len(original))
	}

	// Verify deep copy: mutating copy shouldn't affect original
	copied[0].ToolUses[0].Name = "MUTATED"
	if original[0].ToolUses[0].Name == "MUTATED" {
		t.Error("copyMessages should deep copy tool uses")
	}

	copied[0].ThinkingBlocks[0].Content = "MUTATED"
	if original[0].ThinkingBlocks[0].Content == "MUTATED" {
		t.Error("copyMessages should deep copy thinking blocks")
	}

	copied[0].ContentBlocks[0].Text = "MUTATED"
	if original[0].ContentBlocks[0].Text == "MUTATED" {
		t.Error("copyMessages should deep copy content blocks")
	}
}

func TestCopyMessages_Nil(t *testing.T) {
	if got := copyMessages(nil); got != nil {
		t.Errorf("copyMessages(nil) = %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// Types tests
// ---------------------------------------------------------------------------

func TestThread_CreatedTime(t *testing.T) {
	thread := Thread{Created: baseTime}
	ct := thread.CreatedTime()
	if ct.IsZero() {
		t.Error("CreatedTime should not be zero")
	}
	if ct.UnixMilli() != baseTime {
		t.Errorf("CreatedTime = %d, want %d", ct.UnixMilli(), baseTime)
	}
}

func TestThread_CreatedTime_Zero(t *testing.T) {
	thread := Thread{Created: 0}
	if !thread.CreatedTime().IsZero() {
		t.Error("CreatedTime(0) should be zero")
	}
}

func TestMessageMeta_SentAtTime(t *testing.T) {
	meta := &MessageMeta{SentAt: baseTime}
	sat := meta.SentAtTime()
	if sat.IsZero() {
		t.Error("SentAtTime should not be zero")
	}
	if sat.UnixMilli() != baseTime {
		t.Errorf("SentAtTime = %d, want %d", sat.UnixMilli(), baseTime)
	}
}

func TestMessageMeta_SentAtTime_Nil(t *testing.T) {
	var meta *MessageMeta
	if !meta.SentAtTime().IsZero() {
		t.Error("nil MessageMeta SentAtTime should be zero")
	}
}

func TestMessageMeta_SentAtTime_Zero(t *testing.T) {
	meta := &MessageMeta{SentAt: 0}
	if !meta.SentAtTime().IsZero() {
		t.Error("zero SentAt SentAtTime should be zero")
	}
}

// ---------------------------------------------------------------------------
// Malformed/edge-case JSON
// ---------------------------------------------------------------------------

func TestSessions_MalformedJSON(t *testing.T) {
	threadsDir := t.TempDir()
	projectDir := t.TempDir()

	// Write malformed JSON
	badPath := filepath.Join(threadsDir, "T-bad-001.json")
	if err := os.WriteFile(badPath, []byte("{broken json"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write valid thread
	goodThread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, goodThread)

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions should not fail on malformed JSON: %v", err)
	}

	// Should still return the good session
	if len(sessions) != 1 {
		t.Errorf("expected 1 valid session (skipping malformed), got %d", len(sessions))
	}
}

func TestSessions_NonThreadFiles(t *testing.T) {
	threadsDir := t.TempDir()
	projectDir := t.TempDir()

	// Write files that don't match T-*.json pattern
	for _, name := range []string{"readme.md", "config.json", ".DS_Store", "random.txt"} {
		path := filepath.Join(threadsDir, name)
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Write valid thread
	goodThread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, goodThread)

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session (ignoring non-thread files), got %d", len(sessions))
	}
}

func TestMessages_Timestamp(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)
	_, _ = a.Sessions(projectDir)

	msgs, err := a.Messages("T-test-thread-001")
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	// User message should have timestamp from meta.sentAt
	if msgs[0].Timestamp.IsZero() {
		t.Error("user message timestamp should not be zero")
	}
	if msgs[0].Timestamp.UnixMilli() != baseTime {
		t.Errorf("user message timestamp = %d, want %d", msgs[0].Timestamp.UnixMilli(), baseTime)
	}
}

// ---------------------------------------------------------------------------
// Session index update
// ---------------------------------------------------------------------------

func TestSessionIndex_UpdatedBySessions(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()

	thread := fixtureSimpleThread(projectDir)
	writeThread(t, threadsDir, thread)

	a := newTestAdapter(t, threadsDir)

	// Before Sessions(), index is empty
	a.mu.RLock()
	if len(a.sessionIndex) != 0 {
		t.Error("sessionIndex should be empty before Sessions()")
	}
	a.mu.RUnlock()

	_, _ = a.Sessions(projectDir)

	// After Sessions(), index should contain the thread
	a.mu.RLock()
	path, ok := a.sessionIndex["T-test-thread-001"]
	a.mu.RUnlock()
	if !ok {
		t.Error("sessionIndex should contain T-test-thread-001 after Sessions()")
	}
	if path == "" {
		t.Error("sessionIndex path should not be empty")
	}
}

// ---------------------------------------------------------------------------
// Multiple sessions ordering
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Path resolution: ampThreadsDirCandidates / findAmpThreadsDir
// ---------------------------------------------------------------------------

func TestAmpThreadsDirCandidates(t *testing.T) {
	home := "/Users/testuser"

	t.Run("default candidates", func(t *testing.T) {
		// Unset env vars for clean test
		t.Setenv("AMP_DATA_HOME", "")
		t.Setenv("XDG_DATA_HOME", "")

		candidates := ampThreadsDirCandidates(home)

		// Should always include default path
		found := false
		for _, c := range candidates {
			if strings.Contains(c, filepath.Join(".local", "share", "amp", "threads")) {
				found = true
			}
		}
		if !found {
			t.Error("candidates should include default ~/.local/share/amp/threads path")
		}
	})

	t.Run("AMP_DATA_HOME override", func(t *testing.T) {
		t.Setenv("AMP_DATA_HOME", "/custom/amp/data")
		t.Setenv("XDG_DATA_HOME", "")

		candidates := ampThreadsDirCandidates(home)

		if candidates[0] != filepath.Join("/custom/amp/data", "amp", "threads") {
			t.Errorf("first candidate should use AMP_DATA_HOME, got %s", candidates[0])
		}
	})
}

func TestFindAmpThreadsDir(t *testing.T) {
	t.Run("finds existing directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("AMP_DATA_HOME", "")
		t.Setenv("XDG_DATA_HOME", "")

		// Create the default directory structure
		threadsDir := filepath.Join(tmpDir, ".local", "share", "amp", "threads")
		_ = os.MkdirAll(threadsDir, 0755)

		result := findAmpThreadsDir(tmpDir)
		if result != threadsDir {
			t.Errorf("expected %s, got %s", threadsDir, result)
		}
	})

	t.Run("prefers AMP_DATA_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		customDir := filepath.Join(tmpDir, "custom")
		threadsDir := filepath.Join(customDir, "amp", "threads")
		_ = os.MkdirAll(threadsDir, 0755)

		t.Setenv("AMP_DATA_HOME", customDir)

		result := findAmpThreadsDir(tmpDir)
		if result != threadsDir {
			t.Errorf("expected AMP_DATA_HOME path %s, got %s", threadsDir, result)
		}
	})

	t.Run("falls back when no dir exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("AMP_DATA_HOME", "")
		t.Setenv("XDG_DATA_HOME", "")

		result := findAmpThreadsDir(tmpDir)
		// Should return first candidate (default path) even though it doesn't exist
		expected := filepath.Join(tmpDir, ".local", "share", "amp", "threads")
		if result != expected {
			t.Errorf("expected fallback %s, got %s", expected, result)
		}
	})
}

func TestSessions_OrderedByUpdateTime(t *testing.T) {
	projectDir := t.TempDir()
	threadsDir := t.TempDir()
	projectURI := "file://" + projectDir

	// Create threads with different timestamps
	for i, offset := range []int{0, 3600, 1800} { // 0s, 1h, 30m offsets
		id := fmt.Sprintf("T-order-%03d", i)
		sentAt := ts(baseTime, offset)
		thread := makeThread(id, projectURI, []Message{
			{Role: "user", MessageID: 0, Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("msg %d", i)}},
				Meta: &MessageMeta{SentAt: sentAt}},
			{Role: "assistant", MessageID: 1, Content: []ContentBlock{{Type: "text", Text: "ok"}},
				Usage: &Usage{Model: "m", OutputTokens: 1, TotalInputTokens: 1}},
		}, baseTime)
		writeThread(t, threadsDir, thread)
	}

	a := newTestAdapter(t, threadsDir)
	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}

	// Should be sorted by UpdatedAt descending
	for i := 0; i < len(sessions)-1; i++ {
		if sessions[i].UpdatedAt.Before(sessions[i+1].UpdatedAt) {
			t.Errorf("sessions[%d].UpdatedAt (%v) < sessions[%d].UpdatedAt (%v)",
				i, sessions[i].UpdatedAt, i+1, sessions[i+1].UpdatedAt)
		}
	}

	// First session should be the one with 1h offset (most recent)
	if sessions[0].ID != "T-order-001" {
		t.Errorf("most recent session should be T-order-001, got %s", sessions[0].ID)
	}
}
