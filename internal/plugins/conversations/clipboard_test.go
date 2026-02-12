package conversations

import (
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
)

func TestFormatSessionSummary_NilSession(t *testing.T) {
	// sessionRef is not a pointer receiver for nil, but we test with empty
	s := &sessionRef{}
	result := formatSessionSummary(s)
	if result == "" {
		t.Error("should produce output even for empty session")
	}
}

func TestFormatSessionSummary_FullSession(t *testing.T) {
	s := &sessionRef{
		ID:        "test-123",
		Name:      "Test Session",
		Adapter:   "Claude Code",
		CreatedAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
		Duration:  30 * time.Minute,
		Tokens:    5000,
		EstCost:   0.15,
	}

	result := formatSessionSummary(s)

	checks := []string{"Test Session", "test-123", "Claude Code", "2024-01-15", "30m", "5000", "$0.15"}
	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("expected output to contain %q", c)
		}
	}
}

func TestFormatSessionSummary_FallbackToSlug(t *testing.T) {
	s := &sessionRef{ID: "abc123", Slug: "ses_abc"}
	result := formatSessionSummary(s)
	if !strings.Contains(result, "ses_abc") {
		t.Error("should fall back to slug when name is empty")
	}
}

func TestFormatSessionSummary_FallbackToID(t *testing.T) {
	s := &sessionRef{ID: "abc123"}
	result := formatSessionSummary(s)
	if !strings.Contains(result, "abc123") {
		t.Error("should fall back to ID when name and slug are empty")
	}
}

func TestFormatTurnAsMarkdown_EmptyTurn(t *testing.T) {
	turn := &Turn{Role: "user", Messages: []adapter.Message{}}
	result := formatTurnAsMarkdown(turn)
	if !strings.Contains(result, "User") {
		t.Error("should contain capitalized role")
	}
}

func TestFormatTurnAsMarkdown_UserMessage(t *testing.T) {
	turn := &Turn{
		Role: "user",
		Messages: []adapter.Message{
			{Role: "user", Content: "Hello world", Timestamp: time.Now()},
		},
	}
	result := formatTurnAsMarkdown(turn)
	if !strings.Contains(result, "Hello world") {
		t.Error("should contain message content")
	}
	if !strings.Contains(result, "## User") {
		t.Error("should contain role header")
	}
}

func TestFormatTurnAsMarkdown_WithToolUses(t *testing.T) {
	turn := &Turn{
		Role: "assistant",
		Messages: []adapter.Message{
			{
				Role:    "assistant",
				Content: "I found the file.",
				ToolUses: []adapter.ToolUse{
					{Name: "Read", Input: `{"file_path": "test.go"}`},
				},
				Timestamp: time.Now(),
			},
		},
	}
	result := formatTurnAsMarkdown(turn)
	if !strings.Contains(result, "**Tools:**") {
		t.Error("should contain tools section")
	}
	if !strings.Contains(result, "Read") {
		t.Error("should contain tool name")
	}
}

func TestFormatTurnAsMarkdown_WithThinkingBlocks(t *testing.T) {
	turn := &Turn{
		Role: "assistant",
		Messages: []adapter.Message{
			{
				Role:    "assistant",
				Content: "The answer is 42.",
				ThinkingBlocks: []adapter.ThinkingBlock{
					{Content: "Let me think...", TokenCount: 100},
				},
				Timestamp: time.Now(),
			},
		},
	}
	result := formatTurnAsMarkdown(turn)
	if !strings.Contains(result, "<details>") {
		t.Error("should contain thinking block details tag")
	}
	if !strings.Contains(result, "100 tokens") {
		t.Error("should contain thinking token count")
	}
}

func TestFormatTurnAsMarkdown_WithTokens(t *testing.T) {
	turn := &Turn{
		Role:           "assistant",
		TotalTokensIn:  1000,
		TotalTokensOut: 500,
		Messages: []adapter.Message{
			{Role: "assistant", Content: "response", Timestamp: time.Now()},
		},
	}
	result := formatTurnAsMarkdown(turn)
	if !strings.Contains(result, "in=1000") {
		t.Error("should contain input token count")
	}
	if !strings.Contains(result, "out=500") {
		t.Error("should contain output token count")
	}
}
