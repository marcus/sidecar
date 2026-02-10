package kiro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
)

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  time.Time
	}{
		{
			name:  "RFC3339 with timezone",
			input: "2026-02-08T20:44:10.757724-08:00",
			want:  time.Date(2026, 2, 8, 20, 44, 10, 757724000, time.FixedZone("", -8*3600)),
		},
		{
			name:  "RFC3339 UTC",
			input: "2026-02-08T04:44:10Z",
			want:  time.Date(2026, 2, 8, 4, 44, 10, 0, time.UTC),
		},
		{
			name:  "empty string",
			input: "",
			want:  time.Time{},
		},
		{
			name:  "invalid string",
			input: "not-a-timestamp",
			want:  time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTimestamp(tt.input)
			if !got.Equal(tt.want) {
				t.Errorf("parseTimestamp(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestMillisToTime(t *testing.T) {
	// 2026-02-08T20:44:10.757Z in milliseconds
	ms := int64(1770612250757)
	got := time.UnixMilli(ms)
	if got.Year() != 2026 {
		t.Errorf("expected year 2026, got %d", got.Year())
	}
	if got.Month() != time.February {
		t.Errorf("expected February, got %s", got.Month())
	}
}

func TestExtractPromptText(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "valid prompt",
			json: `{"Prompt": {"prompt": "hello world"}}`,
			want: "hello world",
		},
		{
			name: "tool use results (not prompt)",
			json: `{"ToolUseResults": {"tool_use_results": []}}`,
			want: "",
		},
		{
			name: "empty",
			json: `{}`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPromptText(json.RawMessage(tt.json))
			if got != tt.want {
				t.Errorf("extractPromptText(%q) = %q, want %q", tt.json, got, tt.want)
			}
		})
	}
}

func TestTruncateText(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncated", "hello world this is long", 10, "hello w..."},
		{"newlines", "hello\nworld", 50, "hello world"},
		{"empty", "", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateText(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateText(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestShortConversationID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abcdef01-2345-6789-abcd-ef0123456789", "abcdef01"},
		{"short", "short"},
		{"12345678", "12345678"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shortConversationID(tt.input)
			if got != tt.want {
				t.Errorf("shortConversationID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCwdMatchesProject(t *testing.T) {
	tests := []struct {
		name    string
		project string
		cwd     string
		want    bool
	}{
		{"exact match", "/Users/foo/project", "/Users/foo/project", true},
		{"subdirectory", "/Users/foo/project", "/Users/foo/project/src", true},
		{"different project", "/Users/foo/project", "/Users/foo/other", false},
		{"parent dir", "/Users/foo/project/src", "/Users/foo/project", false},
		{"empty project", "", "/Users/foo/project", false},
		{"empty cwd", "/Users/foo/project", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cwdMatchesProject(tt.project, tt.cwd)
			if got != tt.want {
				t.Errorf("cwdMatchesProject(%q, %q) = %v, want %v", tt.project, tt.cwd, got, tt.want)
			}
		})
	}
}

func TestIsPromptEntry(t *testing.T) {
	prompt := HistoryEntry{
		User: &UserMessage{
			Content: json.RawMessage(`{"Prompt": {"prompt": "hello"}}`),
		},
	}
	if !isPromptEntry(prompt) {
		t.Error("expected prompt entry to be detected")
	}

	toolResult := HistoryEntry{
		User: &UserMessage{
			Content: json.RawMessage(`{"ToolUseResults": {"tool_use_results": []}}`),
		},
	}
	if isPromptEntry(toolResult) {
		t.Error("expected tool result entry not to be a prompt")
	}

	nilUser := HistoryEntry{}
	if isPromptEntry(nilUser) {
		t.Error("expected nil user entry not to be a prompt")
	}
}

func TestParseAssistantMessage(t *testing.T) {
	ts := time.Now()

	t.Run("Response", func(t *testing.T) {
		raw := json.RawMessage(`{"Response": {"message_id": "msg-1", "content": "Hello!"}}`)
		msg, toolUses := parseAssistantMessage(raw, "sess-1", 0, ts, "auto")
		if msg == nil {
			t.Fatal("expected message, got nil")
		}
		if msg.Content != "Hello!" {
			t.Errorf("expected content 'Hello!', got %q", msg.Content)
		}
		if msg.ID != "msg-1" {
			t.Errorf("expected ID 'msg-1', got %q", msg.ID)
		}
		if len(toolUses) != 0 {
			t.Errorf("expected no tool uses, got %d", len(toolUses))
		}
	})

	t.Run("ToolUse", func(t *testing.T) {
		raw := json.RawMessage(`{"ToolUse": {"message_id": "msg-2", "content": "Running command", "tool_uses": [{"id": "tooluse_abc", "name": "execute_bash", "args": {"command": "ls"}}]}}`)
		msg, toolUses := parseAssistantMessage(raw, "sess-1", 1, ts, "auto")
		if msg == nil {
			t.Fatal("expected message, got nil")
		}
		if msg.Content != "Running command" {
			t.Errorf("expected content 'Running command', got %q", msg.Content)
		}
		if len(toolUses) != 1 {
			t.Fatalf("expected 1 tool use, got %d", len(toolUses))
		}
		if toolUses[0].Name != "execute_bash" {
			t.Errorf("expected tool name 'execute_bash', got %q", toolUses[0].Name)
		}
		if toolUses[0].ID != "tooluse_abc" {
			t.Errorf("expected tool ID 'tooluse_abc', got %q", toolUses[0].ID)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		raw := json.RawMessage(`{"Unknown": {}}`)
		msg, _ := parseAssistantMessage(raw, "sess-1", 2, ts, "auto")
		if msg != nil {
			t.Error("expected nil message for unknown format")
		}
	})
}

func TestLinkToolResults(t *testing.T) {
	toolUses := []adapter.ToolUse{
		{ID: "tooluse_abc", Name: "execute_bash", Input: `{"command":"ls"}`},
		{ID: "tooluse_def", Name: "read_file", Input: `{"path":"main.go"}`},
	}

	content := json.RawMessage(`{
		"ToolUseResults": {
			"tool_use_results": [
				{
					"tool_use_id": "tooluse_abc",
					"content": [{"Json": {"exit_status": "0", "stdout": "file1.go\nfile2.go", "stderr": ""}}],
					"status": "Success"
				}
			]
		}
	}`)

	linkToolResults(toolUses, content)

	if toolUses[0].Output != "file1.go\nfile2.go" {
		t.Errorf("expected stdout linked to first tool use, got %q", toolUses[0].Output)
	}
	if toolUses[1].Output != "" {
		t.Errorf("expected second tool use output empty, got %q", toolUses[1].Output)
	}
}

func TestConversationValueParsing(t *testing.T) {
	valueJSON := `{
		"conversation_id": "conv-123",
		"history": [
			{
				"user": {
					"content": {"Prompt": {"prompt": "What files are here?"}},
					"timestamp": "2026-02-08T20:44:10.757724-08:00"
				},
				"assistant": {"Response": {"message_id": "msg-1", "content": "Let me check."}},
				"request_metadata": {
					"context_usage_percentage": 7.2,
					"request_start_timestamp_ms": 1770612250759,
					"stream_end_timestamp_ms": 1770612256160
				}
			},
			{
				"user": {
					"content": {"Prompt": {"prompt": "Run ls"}},
					"timestamp": "2026-02-08T20:45:00.000000-08:00"
				},
				"assistant": {
					"ToolUse": {
						"message_id": "msg-2",
						"content": "I'll run ls for you.",
						"tool_uses": [{"id": "tooluse_xyz", "name": "execute_bash", "args": {"command": "ls -la"}}]
					}
				}
			},
			{
				"user": {
					"content": {
						"ToolUseResults": {
							"tool_use_results": [{
								"tool_use_id": "tooluse_xyz",
								"content": [{"Json": {"exit_status": "0", "stdout": "total 8\n-rw-r--r-- 1 user staff 100 main.go", "stderr": ""}}],
								"status": "Success"
							}]
						}
					},
					"timestamp": "2026-02-08T20:45:05.000000-08:00"
				},
				"assistant": {"Response": {"message_id": "msg-3", "content": "Here are your files."}}
			}
		],
		"model_info": {"model_name": "auto", "model_id": "auto"}
	}`

	var conv ConversationValue
	if err := json.Unmarshal([]byte(valueJSON), &conv); err != nil {
		t.Fatalf("failed to parse conversation: %v", err)
	}

	if conv.ConversationID != "conv-123" {
		t.Errorf("expected conversation_id 'conv-123', got %q", conv.ConversationID)
	}
	if len(conv.History) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(conv.History))
	}

	// First entry should be a prompt
	if !isPromptEntry(conv.History[0]) {
		t.Error("expected first entry to be a prompt")
	}
	if extractPromptText(conv.History[0].User.Content) != "What files are here?" {
		t.Error("unexpected prompt text for first entry")
	}

	// Second entry should be a prompt with tool use response
	if !isPromptEntry(conv.History[1]) {
		t.Error("expected second entry to be a prompt")
	}

	// Third entry should be tool results (not a prompt)
	if isPromptEntry(conv.History[2]) {
		t.Error("expected third entry NOT to be a prompt")
	}

	// Count prompts
	promptCount := 0
	for _, entry := range conv.History {
		if isPromptEntry(entry) {
			promptCount++
		}
	}
	if promptCount != 2 {
		t.Errorf("expected 2 prompts, got %d", promptCount)
	}
}

func TestSearchMessages_InterfaceCompliance(t *testing.T) {
	a := New()
	var _ adapter.MessageSearcher = a
}

func TestSearchMessages_NonExistentSession(t *testing.T) {
	a := New()
	_, err := a.SearchMessages("nonexistent-session-xyz", "test", adapter.DefaultSearchOptions())
	// Don't strictly check error since it depends on local Kiro installation
	_ = err
}

func TestKiroDBCandidates(t *testing.T) {
	home := "/Users/testuser"
	candidates := kiroDBCandidates(home)

	// Should always include ~/.kiro path first
	if candidates[0] != filepath.Join(home, ".kiro", "data.sqlite3") {
		t.Errorf("first candidate should be ~/.kiro, got %s", candidates[0])
	}

	// Should include platform-specific path
	if runtime.GOOS == "darwin" {
		found := false
		for _, c := range candidates {
			if strings.Contains(c, "Application Support") {
				found = true
			}
		}
		if !found {
			t.Error("macOS candidates should include Application Support path")
		}
	}

	// Should always include legacy ~/.amazonq path
	last := candidates[len(candidates)-1]
	if !strings.Contains(last, ".amazonq") {
		t.Errorf("last candidate should be legacy .amazonq path, got %s", last)
	}
}

func TestFindKiroDB(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake DB at the ~/.kiro location
	kiroDir := filepath.Join(tmpDir, ".kiro")
	_ = os.MkdirAll(kiroDir, 0755)
	_ = os.WriteFile(filepath.Join(kiroDir, "data.sqlite3"), []byte("fake"), 0644)

	result := findKiroDB(tmpDir)
	expected := filepath.Join(kiroDir, "data.sqlite3")
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestAdapterInterface(t *testing.T) {
	a := New()
	if a.ID() != "kiro" {
		t.Errorf("ID() = %q, want 'kiro'", a.ID())
	}
	if a.Name() != "Kiro" {
		t.Errorf("Name() = %q, want 'Kiro'", a.Name())
	}
	if a.Icon() != "\u03ba" {
		t.Errorf("Icon() = %q, want kappa", a.Icon())
	}

	caps := a.Capabilities()
	for _, cap := range []adapter.Capability{adapter.CapSessions, adapter.CapMessages, adapter.CapUsage, adapter.CapWatch} {
		if !caps[cap] {
			t.Errorf("expected capability %s to be true", cap)
		}
	}

	if a.WatchScope() != adapter.WatchScopeGlobal {
		t.Error("expected WatchScopeGlobal")
	}
}
