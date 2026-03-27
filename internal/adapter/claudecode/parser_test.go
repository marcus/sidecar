package claudecode

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
)

func TestParseContent_String(t *testing.T) {
	a := &Adapter{}

	content := json.RawMessage(`"Hello, world!"`)
	text, toolUses, thinkingBlocks := a.parseContent(content)

	if text != "Hello, world!" {
		t.Errorf("got text %q, want %q", text, "Hello, world!")
	}
	if len(toolUses) != 0 {
		t.Errorf("got %d tool uses, want 0", len(toolUses))
	}
	if len(thinkingBlocks) != 0 {
		t.Errorf("got %d thinking blocks, want 0", len(thinkingBlocks))
	}
}

func TestParseContent_TextBlock(t *testing.T) {
	a := &Adapter{}

	content := json.RawMessage(`[{"type":"text","text":"First line"},{"type":"text","text":"Second line"}]`)
	text, toolUses, _ := a.parseContent(content)

	if text != "First line\nSecond line" {
		t.Errorf("got text %q, want %q", text, "First line\nSecond line")
	}
	if len(toolUses) != 0 {
		t.Errorf("got %d tool uses, want 0", len(toolUses))
	}
}

func TestParseContent_ToolUse(t *testing.T) {
	a := &Adapter{}

	content := json.RawMessage(`[{"type":"text","text":"Let me read that."},{"type":"tool_use","id":"tool-123","name":"Read","input":{"file_path":"/tmp/test.go"}}]`)
	text, toolUses, _ := a.parseContent(content)

	if text != "Let me read that." {
		t.Errorf("got text %q, want %q", text, "Let me read that.")
	}
	if len(toolUses) != 1 {
		t.Fatalf("got %d tool uses, want 1", len(toolUses))
	}
	if toolUses[0].ID != "tool-123" {
		t.Errorf("got tool ID %q, want %q", toolUses[0].ID, "tool-123")
	}
	if toolUses[0].Name != "Read" {
		t.Errorf("got tool name %q, want %q", toolUses[0].Name, "Read")
	}
}

func TestParseContent_Empty(t *testing.T) {
	a := &Adapter{}

	text, toolUses, _ := a.parseContent(nil)
	if text != "" {
		t.Errorf("got text %q, want empty", text)
	}
	if len(toolUses) != 0 {
		t.Errorf("got %d tool uses, want 0", len(toolUses))
	}

	text, _, _ = a.parseContent(json.RawMessage{})
	if text != "" {
		t.Errorf("got text %q, want empty", text)
	}
}

func TestParseContent_InvalidJSON(t *testing.T) {
	a := &Adapter{}

	content := json.RawMessage(`{invalid}`)
	text, toolUses, _ := a.parseContent(content)

	if text != "" {
		t.Errorf("got text %q, want empty", text)
	}
	if len(toolUses) != 0 {
		t.Errorf("got %d tool uses, want 0", len(toolUses))
	}
}

func TestParseContent_ThinkingBlock(t *testing.T) {
	a := &Adapter{}

	content := json.RawMessage(`[{"type":"thinking","thinking":"Let me think..."},{"type":"text","text":"Answer here."}]`)
	text, toolUses, thinkingBlocks := a.parseContent(content)

	// Thinking blocks should not be included in text but should be extracted
	if text != "Answer here." {
		t.Errorf("got text %q, want %q", text, "Answer here.")
	}
	if len(toolUses) != 0 {
		t.Errorf("got %d tool uses, want 0", len(toolUses))
	}
	if len(thinkingBlocks) != 1 {
		t.Fatalf("got %d thinking blocks, want 1", len(thinkingBlocks))
	}
	if thinkingBlocks[0].Content != "Let me think..." {
		t.Errorf("got thinking content %q, want %q", thinkingBlocks[0].Content, "Let me think...")
	}
	if thinkingBlocks[0].TokenCount != 3 { // len("Let me think...") / 4 = 3
		t.Errorf("got token count %d, want 3", thinkingBlocks[0].TokenCount)
	}
}

func TestParseSessionMetadata_ValidFile(t *testing.T) {
	a := &Adapter{}
	testFile := filepath.Join("testdata", "valid_session.jsonl")

	meta, err := a.parseSessionMetadata(testFile)
	if err != nil {
		t.Fatalf("parseSessionMetadata failed: %v", err)
	}

	if meta.SessionID != "valid_session" {
		t.Errorf("got SessionID %q, want %q", meta.SessionID, "valid_session")
	}
	if meta.MsgCount != 4 {
		t.Errorf("got MsgCount %d, want 4", meta.MsgCount)
	}
	if meta.CWD != "/home/user/project" {
		t.Errorf("got CWD %q, want %q", meta.CWD, "/home/user/project")
	}
	if meta.Version != "1.0.0" {
		t.Errorf("got Version %q, want %q", meta.Version, "1.0.0")
	}
	if meta.GitBranch != "main" {
		t.Errorf("got GitBranch %q, want %q", meta.GitBranch, "main")
	}
}

func TestParseSessionMetadata_EmptyFile(t *testing.T) {
	a := &Adapter{}
	testFile := filepath.Join("testdata", "empty.jsonl")

	meta, err := a.parseSessionMetadata(testFile)
	if err != nil {
		t.Fatalf("parseSessionMetadata failed: %v", err)
	}

	if meta.MsgCount != 0 {
		t.Errorf("got MsgCount %d, want 0", meta.MsgCount)
	}
	// FirstMsg and LastMsg should be set to current time
	if meta.FirstMsg.IsZero() {
		t.Error("FirstMsg should not be zero")
	}
}

func TestParseSessionMetadata_MalformedFile(t *testing.T) {
	a := &Adapter{}
	testFile := filepath.Join("testdata", "malformed.jsonl")

	meta, err := a.parseSessionMetadata(testFile)
	if err != nil {
		t.Fatalf("parseSessionMetadata should handle malformed lines: %v", err)
	}

	// Should only count the valid line
	if meta.MsgCount != 1 {
		t.Errorf("got MsgCount %d, want 1 (only valid lines)", meta.MsgCount)
	}
}

func TestParseSessionMetadata_NonExistent(t *testing.T) {
	a := &Adapter{}

	_, err := a.parseSessionMetadata("/nonexistent/path.jsonl")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestMessagesFromTestdata(t *testing.T) {
	a := &Adapter{}
	testFile := filepath.Join("testdata", "valid_session.jsonl")

	messages := parseMessagesFromFile(t, a, testFile)

	if len(messages) != 4 {
		t.Fatalf("got %d messages, want 4", len(messages))
	}

	// Check first message (user)
	if messages[0].Role != "user" {
		t.Errorf("msg[0] role = %q, want user", messages[0].Role)
	}
	if messages[0].ID != "msg-001" {
		t.Errorf("msg[0] ID = %q, want msg-001", messages[0].ID)
	}

	// Check second message (assistant with usage)
	if messages[1].Role != "assistant" {
		t.Errorf("msg[1] role = %q, want assistant", messages[1].Role)
	}
	if messages[1].InputTokens != 50 {
		t.Errorf("msg[1] InputTokens = %d, want 50", messages[1].InputTokens)
	}
	if messages[1].OutputTokens != 20 {
		t.Errorf("msg[1] OutputTokens = %d, want 20", messages[1].OutputTokens)
	}
	if messages[1].CacheRead != 10 {
		t.Errorf("msg[1] CacheRead = %d, want 10", messages[1].CacheRead)
	}

	// Check fourth message (assistant with tool use)
	if len(messages[3].ToolUses) != 1 {
		t.Fatalf("msg[3] ToolUses len = %d, want 1", len(messages[3].ToolUses))
	}
	if messages[3].ToolUses[0].Name != "Read" {
		t.Errorf("msg[3] tool name = %q, want Read", messages[3].ToolUses[0].Name)
	}
}

func TestMessagesFromMalformed(t *testing.T) {
	a := &Adapter{}
	testFile := filepath.Join("testdata", "malformed.jsonl")

	messages := parseMessagesFromFile(t, a, testFile)

	// Should parse the one valid line
	if len(messages) != 1 {
		t.Errorf("got %d messages, want 1", len(messages))
	}
}

func TestMessagesFromEmpty(t *testing.T) {
	a := &Adapter{}
	testFile := filepath.Join("testdata", "empty.jsonl")

	messages := parseMessagesFromFile(t, a, testFile)

	if len(messages) != 0 {
		t.Errorf("got %d messages, want 0", len(messages))
	}
}

func TestRawMessageParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType string
		wantUUID string
		wantErr  bool
	}{
		{
			name:     "user message",
			input:    `{"type":"user","uuid":"u-001","timestamp":"2024-01-15T10:00:00Z"}`,
			wantType: "user",
			wantUUID: "u-001",
		},
		{
			name:     "assistant message",
			input:    `{"type":"assistant","uuid":"a-001","timestamp":"2024-01-15T10:00:00Z"}`,
			wantType: "assistant",
			wantUUID: "a-001",
		},
		{
			name:     "tool result skipped",
			input:    `{"type":"tool_result","uuid":"t-001","timestamp":"2024-01-15T10:00:00Z"}`,
			wantType: "tool_result",
			wantUUID: "t-001",
		},
		{
			name:    "invalid json",
			input:   `{not valid json`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var raw RawMessage
			err := json.Unmarshal([]byte(tc.input), &raw)

			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if raw.Type != tc.wantType {
				t.Errorf("got type %q, want %q", raw.Type, tc.wantType)
			}
			if raw.UUID != tc.wantUUID {
				t.Errorf("got uuid %q, want %q", raw.UUID, tc.wantUUID)
			}
		})
	}
}

func TestTimestampParsing(t *testing.T) {
	input := `{"type":"user","uuid":"u-001","timestamp":"2024-01-15T10:30:45Z"}`

	var raw RawMessage
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	expected := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)
	if !raw.Timestamp.Equal(expected) {
		t.Errorf("got timestamp %v, want %v", raw.Timestamp, expected)
	}
}

// testMessage mirrors adapter.Message for test assertions.
type testMessage struct {
	ID           string
	Role         string
	Content      string
	ToolUses     []adapter.ToolUse
	InputTokens  int
	OutputTokens int
	CacheRead    int
}

// TestParseContentWithResults tests the new ContentBlocks parsing.
func TestParseContentWithResults_TextBlock(t *testing.T) {
	a := &Adapter{}
	raw := json.RawMessage(`[{"type":"text","text":"Hello world"}]`)

	content, toolUses, thinkingBlocks, blocks := a.parseContentWithResults(raw, nil)

	if content != "Hello world" {
		t.Errorf("got content %q, want %q", content, "Hello world")
	}
	if len(toolUses) != 0 {
		t.Errorf("got %d tool uses, want 0", len(toolUses))
	}
	if len(thinkingBlocks) != 0 {
		t.Errorf("got %d thinking blocks, want 0", len(thinkingBlocks))
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("got block type %q, want %q", blocks[0].Type, "text")
	}
	if blocks[0].Text != "Hello world" {
		t.Errorf("got block text %q, want %q", blocks[0].Text, "Hello world")
	}
}

func TestParseContentWithResults_ToolUseLinked(t *testing.T) {
	a := &Adapter{}
	toolResults := map[string]toolResultInfo{
		"toolu_123": {content: "file contents here", isError: false},
	}
	raw := json.RawMessage(`[{"type":"tool_use","id":"toolu_123","name":"Read","input":{"file_path":"/test.go"}}]`)

	content, toolUses, _, blocks := a.parseContentWithResults(raw, toolResults)

	if content != "" {
		t.Errorf("got content %q, want empty", content)
	}
	if len(toolUses) != 1 {
		t.Fatalf("got %d tool uses, want 1", len(toolUses))
	}
	if toolUses[0].Output != "file contents here" {
		t.Errorf("got tool output %q, want %q", toolUses[0].Output, "file contents here")
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(blocks))
	}
	if blocks[0].Type != "tool_use" {
		t.Errorf("got block type %q, want %q", blocks[0].Type, "tool_use")
	}
	if blocks[0].ToolUseID != "toolu_123" {
		t.Errorf("got ToolUseID %q, want %q", blocks[0].ToolUseID, "toolu_123")
	}
	if blocks[0].ToolOutput != "file contents here" {
		t.Errorf("got ToolOutput %q, want %q", blocks[0].ToolOutput, "file contents here")
	}
}

func TestParseContentWithResults_ThinkingBlock(t *testing.T) {
	a := &Adapter{}
	raw := json.RawMessage(`[{"type":"thinking","thinking":"Let me analyze this problem carefully..."}]`)

	_, _, thinkingBlocks, blocks := a.parseContentWithResults(raw, nil)

	if len(thinkingBlocks) != 1 {
		t.Fatalf("got %d thinking blocks, want 1", len(thinkingBlocks))
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(blocks))
	}
	if blocks[0].Type != "thinking" {
		t.Errorf("got block type %q, want %q", blocks[0].Type, "thinking")
	}
	if blocks[0].Text != "Let me analyze this problem carefully..." {
		t.Errorf("got block text %q, want %q", blocks[0].Text, "Let me analyze this problem carefully...")
	}
	if blocks[0].TokenCount <= 0 {
		t.Error("expected positive token count")
	}
}

func TestParseContentWithResults_InterleavedBlocks(t *testing.T) {
	a := &Adapter{}
	raw := json.RawMessage(`[
		{"type":"text","text":"First"},
		{"type":"tool_use","id":"t1","name":"Read","input":{}},
		{"type":"text","text":"Second"}
	]`)

	_, _, _, blocks := a.parseContentWithResults(raw, nil)

	if len(blocks) != 3 {
		t.Fatalf("got %d content blocks, want 3", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("blocks[0].Type = %q, want text", blocks[0].Type)
	}
	if blocks[1].Type != "tool_use" {
		t.Errorf("blocks[1].Type = %q, want tool_use", blocks[1].Type)
	}
	if blocks[2].Type != "text" {
		t.Errorf("blocks[2].Type = %q, want text", blocks[2].Type)
	}
}

func TestParseContentWithResults_StringContent(t *testing.T) {
	a := &Adapter{}
	raw := json.RawMessage(`"Plain string content"`)

	content, _, _, blocks := a.parseContentWithResults(raw, nil)

	if content != "Plain string content" {
		t.Errorf("got content %q, want %q", content, "Plain string content")
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("got block type %q, want text", blocks[0].Type)
	}
}

func TestCollectToolResults_StringContent(t *testing.T) {
	a := &Adapter{}
	results := make(map[string]toolResultInfo)
	raw := json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_abc","content":"result data"}]`)

	a.collectToolResults(raw, results)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results["toolu_abc"].content != "result data" {
		t.Errorf("got result %q, want %q", results["toolu_abc"].content, "result data")
	}
}

func TestCollectToolResults_ArrayContent(t *testing.T) {
	a := &Adapter{}
	results := make(map[string]toolResultInfo)
	raw := json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_xyz","content":[{"type":"text","text":"nested"}]}]`)

	a.collectToolResults(raw, results)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	// Content should be JSON marshaled
	if results["toolu_xyz"].content == "" {
		t.Error("expected non-empty result content")
	}
}

func TestCollectToolResults_ErrorFlag(t *testing.T) {
	a := &Adapter{}
	results := make(map[string]toolResultInfo)
	raw := json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_err","content":"error message","is_error":true}]`)

	a.collectToolResults(raw, results)

	if !results["toolu_err"].isError {
		t.Error("expected isError=true")
	}
}

func TestParseContentWithResults_ToolResultInContentBlocks(t *testing.T) {
	// Test that tool_result blocks appear in ContentBlocks for user messages
	a := &Adapter{}
	raw := json.RawMessage(`[{"type":"tool_result","tool_use_id":"t123","content":"output here"}]`)

	_, _, _, blocks := a.parseContentWithResults(raw, nil)

	if len(blocks) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(blocks))
	}
	if blocks[0].Type != "tool_result" {
		t.Errorf("got block type %q, want tool_result", blocks[0].Type)
	}
	if blocks[0].ToolUseID != "t123" {
		t.Errorf("got ToolUseID %q, want t123", blocks[0].ToolUseID)
	}
	if blocks[0].ToolOutput != "output here" {
		t.Errorf("got ToolOutput %q, want 'output here'", blocks[0].ToolOutput)
	}
}

// parseMessagesFromFile is a helper that mimics Messages() but for local files.
func parseMessagesFromFile(t *testing.T, a *Adapter, path string) []testMessage {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	var messages []testMessage
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var raw RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}

		if raw.Type != "user" && raw.Type != "assistant" {
			continue
		}
		if raw.Message == nil {
			continue
		}

		msg := testMessage{
			ID:   raw.UUID,
			Role: raw.Message.Role,
		}

		content, toolUses, _ := a.parseContent(raw.Message.Content)
		msg.Content = content
		msg.ToolUses = toolUses

		if raw.Message.Usage != nil {
			msg.InputTokens = raw.Message.Usage.InputTokens
			msg.OutputTokens = raw.Message.Usage.OutputTokens
			msg.CacheRead = raw.Message.Usage.CacheReadInputTokens
		}

		messages = append(messages, msg)
	}

	return messages
}

// =============================================================================
// Integration tests for two-pass tool linking
// =============================================================================

// TestTwoPassToolLinking_MultipleToolUses verifies that multiple tool_use blocks
// in a single assistant message are correctly linked to their corresponding
// tool_result blocks in the following user message.
func TestTwoPassToolLinking_MultipleToolUses(t *testing.T) {
	// Create temp dir with test session
	tmpDir := t.TempDir()
	a := &Adapter{projectsDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	projectDir := tmpDir + "/-test-project"
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Copy testdata
	copyTestdataFile(t, "testdata/tool_linking.jsonl", projectDir+"/tool-linking-session.jsonl")

	// Populate session index
	a.mu.Lock()
	a.sessionIndex["tool-linking-session"] = projectDir + "/tool-linking-session.jsonl"
	a.mu.Unlock()

	// Get messages
	messages, err := a.Messages("tool-linking-session")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}

	// Find msg-002 (assistant with two Read tool uses)
	var msg002 *adapter.Message
	for i := range messages {
		if messages[i].ID == "msg-002" {
			msg002 = &messages[i]
			break
		}
	}
	if msg002 == nil {
		t.Fatal("msg-002 not found")
	}

	// Verify two tool uses with linked results
	if len(msg002.ToolUses) != 2 {
		t.Fatalf("expected 2 tool uses, got %d", len(msg002.ToolUses))
	}

	// Check first tool use (toolu_read_1)
	tu1 := msg002.ToolUses[0]
	if tu1.ID != "toolu_read_1" {
		t.Errorf("tu1.ID=%q, want toolu_read_1", tu1.ID)
	}
	if tu1.Name != "Read" {
		t.Errorf("tu1.Name=%q, want Read", tu1.Name)
	}
	if tu1.Output != "package test\n\nfunc TestMain() {}" {
		t.Errorf("tu1.Output=%q, want test file content", tu1.Output)
	}

	// Check second tool use (toolu_read_2)
	tu2 := msg002.ToolUses[1]
	if tu2.ID != "toolu_read_2" {
		t.Errorf("tu2.ID=%q, want toolu_read_2", tu2.ID)
	}
	if tu2.Output != "package main\n\nfunc main() {}" {
		t.Errorf("tu2.Output=%q, want main file content", tu2.Output)
	}

	// Verify ContentBlocks also have linked results
	var toolUseBlocks []adapter.ContentBlock
	for _, cb := range msg002.ContentBlocks {
		if cb.Type == "tool_use" {
			toolUseBlocks = append(toolUseBlocks, cb)
		}
	}
	if len(toolUseBlocks) != 2 {
		t.Fatalf("expected 2 tool_use content blocks, got %d", len(toolUseBlocks))
	}
	if toolUseBlocks[0].ToolOutput != "package test\n\nfunc TestMain() {}" {
		t.Errorf("ContentBlock[0].ToolOutput=%q, want test file", toolUseBlocks[0].ToolOutput)
	}
	if toolUseBlocks[1].ToolOutput != "package main\n\nfunc main() {}" {
		t.Errorf("ContentBlock[1].ToolOutput=%q, want main file", toolUseBlocks[1].ToolOutput)
	}
}

// TestTwoPassToolLinking_ErrorResult verifies that tool_result with is_error=true
// is correctly linked and the error flag is preserved.
func TestTwoPassToolLinking_ErrorResult(t *testing.T) {
	tmpDir := t.TempDir()
	a := &Adapter{projectsDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	projectDir := tmpDir + "/-test-project"
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	copyTestdataFile(t, "testdata/tool_linking.jsonl", projectDir+"/tool-linking-session.jsonl")

	a.mu.Lock()
	a.sessionIndex["tool-linking-session"] = projectDir + "/tool-linking-session.jsonl"
	a.mu.Unlock()

	messages, err := a.Messages("tool-linking-session")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}

	// Find msg-004 (assistant with Write tool use that fails)
	var msg004 *adapter.Message
	for i := range messages {
		if messages[i].ID == "msg-004" {
			msg004 = &messages[i]
			break
		}
	}
	if msg004 == nil {
		t.Fatal("msg-004 not found")
	}

	if len(msg004.ToolUses) != 1 {
		t.Fatalf("expected 1 tool use, got %d", len(msg004.ToolUses))
	}

	// Verify output is linked
	if msg004.ToolUses[0].Output != "Error: permission denied" {
		t.Errorf("Output=%q, want error message", msg004.ToolUses[0].Output)
	}

	// Verify IsError flag in ContentBlocks
	var writeBlock *adapter.ContentBlock
	for i := range msg004.ContentBlocks {
		if msg004.ContentBlocks[i].Type == "tool_use" {
			writeBlock = &msg004.ContentBlocks[i]
			break
		}
	}
	if writeBlock == nil {
		t.Fatal("tool_use content block not found")
	}
	if !writeBlock.IsError {
		t.Error("expected IsError=true for failed tool")
	}
}

// TestTwoPassToolLinking_OrphanToolResult verifies that tool_result blocks
// with no matching tool_use are gracefully ignored (no crash).
func TestTwoPassToolLinking_OrphanToolResult(t *testing.T) {
	tmpDir := t.TempDir()
	a := &Adapter{projectsDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	projectDir := tmpDir + "/-test-project"
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	copyTestdataFile(t, "testdata/tool_linking_edge_cases.jsonl", projectDir+"/edge-cases-session.jsonl")

	a.mu.Lock()
	a.sessionIndex["edge-cases-session"] = projectDir + "/edge-cases-session.jsonl"
	a.mu.Unlock()

	// Should not panic or error
	messages, err := a.Messages("edge-cases-session")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}

	// Verify we got messages (orphan result should be ignored)
	if len(messages) == 0 {
		t.Error("expected non-empty messages")
	}

	// Find msg-002 which has a tool_use with no result
	var msg002 *adapter.Message
	for i := range messages {
		if messages[i].ID == "msg-002" {
			msg002 = &messages[i]
			break
		}
	}
	if msg002 == nil {
		t.Fatal("msg-002 not found")
	}

	// Tool use should exist but have empty output (no matching result)
	if len(msg002.ToolUses) != 1 {
		t.Fatalf("expected 1 tool use, got %d", len(msg002.ToolUses))
	}
	if msg002.ToolUses[0].ID != "toolu_no_result" {
		t.Errorf("expected toolu_no_result, got %q", msg002.ToolUses[0].ID)
	}
	if msg002.ToolUses[0].Output != "" {
		t.Errorf("expected empty output for unlinked tool, got %q", msg002.ToolUses[0].Output)
	}
}

// TestTwoPassToolLinking_OutOfOrderResults verifies that tool_result blocks
// can appear in a different order than their corresponding tool_use blocks.
func TestTwoPassToolLinking_OutOfOrderResults(t *testing.T) {
	tmpDir := t.TempDir()
	a := &Adapter{projectsDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	projectDir := tmpDir + "/-test-project"
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	copyTestdataFile(t, "testdata/tool_linking_edge_cases.jsonl", projectDir+"/edge-cases-session.jsonl")

	a.mu.Lock()
	a.sessionIndex["edge-cases-session"] = projectDir + "/edge-cases-session.jsonl"
	a.mu.Unlock()

	messages, err := a.Messages("edge-cases-session")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}

	// Find msg-004 which has toolu_nested_1 and toolu_nested_2
	// Results come in reverse order: toolu_nested_2 first, then toolu_nested_1
	var msg004 *adapter.Message
	for i := range messages {
		if messages[i].ID == "msg-004" {
			msg004 = &messages[i]
			break
		}
	}
	if msg004 == nil {
		t.Fatal("msg-004 not found")
	}

	if len(msg004.ToolUses) != 2 {
		t.Fatalf("expected 2 tool uses, got %d", len(msg004.ToolUses))
	}

	// toolu_nested_1 should be first in ToolUses (order of tool_use blocks)
	tu1 := msg004.ToolUses[0]
	if tu1.ID != "toolu_nested_1" {
		t.Errorf("tu1.ID=%q, want toolu_nested_1", tu1.ID)
	}
	if tu1.Output != "file1.txt\nfile2.txt" {
		t.Errorf("tu1.Output=%q, want file listing", tu1.Output)
	}

	// toolu_nested_2 should be second
	tu2 := msg004.ToolUses[1]
	if tu2.ID != "toolu_nested_2" {
		t.Errorf("tu2.ID=%q, want toolu_nested_2", tu2.ID)
	}
	if tu2.Output != "/home/user" {
		t.Errorf("tu2.Output=%q, want /home/user", tu2.Output)
	}
}

// TestTwoPassToolLinking_ComplexResultContent verifies handling of tool_result
// with array content (nested content blocks) instead of simple string.
func TestTwoPassToolLinking_ComplexResultContent(t *testing.T) {
	tmpDir := t.TempDir()
	a := &Adapter{projectsDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	projectDir := tmpDir + "/-test-project"
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	copyTestdataFile(t, "testdata/tool_linking_edge_cases.jsonl", projectDir+"/edge-cases-session.jsonl")

	a.mu.Lock()
	a.sessionIndex["edge-cases-session"] = projectDir + "/edge-cases-session.jsonl"
	a.mu.Unlock()

	messages, err := a.Messages("edge-cases-session")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}

	// Find msg-006 which has toolu_complex_result with array content
	var msg006 *adapter.Message
	for i := range messages {
		if messages[i].ID == "msg-006" {
			msg006 = &messages[i]
			break
		}
	}
	if msg006 == nil {
		t.Fatal("msg-006 not found")
	}

	if len(msg006.ToolUses) != 1 {
		t.Fatalf("expected 1 tool use, got %d", len(msg006.ToolUses))
	}

	// Output should be JSON-marshaled array content
	if msg006.ToolUses[0].Output == "" {
		t.Error("expected non-empty output for complex content")
	}
	// Should contain the serialized JSON
	if !strings.Contains(msg006.ToolUses[0].Output, "Complex content structure") {
		t.Errorf("output should contain 'Complex content structure', got %q", msg006.ToolUses[0].Output)
	}
}

// TestTwoPassToolLinking_InterleavedTextAndTools verifies correct handling when
// text blocks are interleaved with tool_use blocks.
func TestTwoPassToolLinking_InterleavedTextAndTools(t *testing.T) {
	tmpDir := t.TempDir()
	a := &Adapter{projectsDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	projectDir := tmpDir + "/-test-project"
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	copyTestdataFile(t, "testdata/tool_linking_edge_cases.jsonl", projectDir+"/edge-cases-session.jsonl")

	a.mu.Lock()
	a.sessionIndex["edge-cases-session"] = projectDir + "/edge-cases-session.jsonl"
	a.mu.Unlock()

	messages, err := a.Messages("edge-cases-session")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}

	// Find msg-004 which has: tool_use, text, tool_use
	var msg004 *adapter.Message
	for i := range messages {
		if messages[i].ID == "msg-004" {
			msg004 = &messages[i]
			break
		}
	}
	if msg004 == nil {
		t.Fatal("msg-004 not found")
	}

	// Verify content blocks preserve order
	if len(msg004.ContentBlocks) != 3 {
		t.Fatalf("expected 3 content blocks, got %d", len(msg004.ContentBlocks))
	}
	if msg004.ContentBlocks[0].Type != "tool_use" {
		t.Errorf("blocks[0].Type=%q, want tool_use", msg004.ContentBlocks[0].Type)
	}
	if msg004.ContentBlocks[1].Type != "text" {
		t.Errorf("blocks[1].Type=%q, want text", msg004.ContentBlocks[1].Type)
	}
	if msg004.ContentBlocks[2].Type != "tool_use" {
		t.Errorf("blocks[2].Type=%q, want tool_use", msg004.ContentBlocks[2].Type)
	}

	// Both tool_use blocks should have their outputs linked
	if msg004.ContentBlocks[0].ToolOutput == "" {
		t.Error("blocks[0].ToolOutput should be non-empty")
	}
	if msg004.ContentBlocks[2].ToolOutput == "" {
		t.Error("blocks[2].ToolOutput should be non-empty")
	}
}
