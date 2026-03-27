package adapter

import (
	"testing"
	"time"
)

func TestCompileSearchPattern_Literal(t *testing.T) {
	re, err := CompileSearchPattern("test query", SearchOptions{UseRegex: false, CaseSensitive: false})
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	if !re.MatchString("this is a test query here") {
		t.Error("expected match for literal pattern")
	}
	if !re.MatchString("TEST QUERY uppercase") {
		t.Error("expected case-insensitive match")
	}
}

func TestCompileSearchPattern_CaseSensitive(t *testing.T) {
	re, err := CompileSearchPattern("Test", SearchOptions{UseRegex: false, CaseSensitive: true})
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	if !re.MatchString("Test") {
		t.Error("expected match for exact case")
	}
	if re.MatchString("test") {
		t.Error("should not match different case when CaseSensitive=true")
	}
}

func TestCompileSearchPattern_Regex(t *testing.T) {
	re, err := CompileSearchPattern("test.*query", SearchOptions{UseRegex: true, CaseSensitive: false})
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	if !re.MatchString("test some query") {
		t.Error("expected match for regex pattern")
	}
}

func TestCompileSearchPattern_InvalidRegex(t *testing.T) {
	_, err := CompileSearchPattern("[invalid", SearchOptions{UseRegex: true})
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestSearchContent_SingleLine(t *testing.T) {
	re, _ := CompileSearchPattern("hello", SearchOptions{UseRegex: false, CaseSensitive: false})
	matches := SearchContent("say hello world", "text", re)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].LineNo != 1 {
		t.Errorf("expected line 1, got %d", matches[0].LineNo)
	}
	if matches[0].BlockType != "text" {
		t.Errorf("expected block type 'text', got %q", matches[0].BlockType)
	}
	if matches[0].ColStart != 4 || matches[0].ColEnd != 9 {
		t.Errorf("expected col range 4-9, got %d-%d", matches[0].ColStart, matches[0].ColEnd)
	}
}

func TestSearchContent_MultiLine(t *testing.T) {
	re, _ := CompileSearchPattern("test", SearchOptions{UseRegex: false, CaseSensitive: false})
	content := "first line\nsecond test line\nthird line\nfourth test"
	matches := SearchContent(content, "text", re)

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0].LineNo != 2 {
		t.Errorf("expected first match on line 2, got %d", matches[0].LineNo)
	}
	if matches[1].LineNo != 4 {
		t.Errorf("expected second match on line 4, got %d", matches[1].LineNo)
	}
}

func TestSearchContent_MultipleMatchesSameLine(t *testing.T) {
	re, _ := CompileSearchPattern("foo", SearchOptions{UseRegex: false, CaseSensitive: false})
	matches := SearchContent("foo bar foo baz foo", "text", re)

	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}
	// All on line 1
	for _, m := range matches {
		if m.LineNo != 1 {
			t.Errorf("expected all matches on line 1, got %d", m.LineNo)
		}
	}
}

func TestSearchContent_EmptyContent(t *testing.T) {
	re, _ := CompileSearchPattern("test", SearchOptions{})
	matches := SearchContent("", "text", re)

	if matches != nil {
		t.Errorf("expected nil for empty content, got %v", matches)
	}
}

func TestSearchMessage_TextContent(t *testing.T) {
	re, _ := CompileSearchPattern("hello", SearchOptions{UseRegex: false, CaseSensitive: false})
	msg := &Message{
		ID:        "msg-1",
		Role:      "user",
		Content:   "hello world",
		Timestamp: time.Now(),
	}

	match := SearchMessage(msg, 0, re, 50, 0)
	if match == nil {
		t.Fatal("expected match")
	}
	if len(match.Matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(match.Matches))
	}
	if match.MessageID != "msg-1" {
		t.Errorf("expected message ID 'msg-1', got %q", match.MessageID)
	}
}

func TestSearchMessage_ToolUses(t *testing.T) {
	re, _ := CompileSearchPattern("grep", SearchOptions{UseRegex: false, CaseSensitive: false})
	msg := &Message{
		ID:   "msg-2",
		Role: "assistant",
		ToolUses: []ToolUse{
			{ID: "tu-1", Name: "grep", Input: `{"pattern": "test"}`},
		},
	}

	match := SearchMessage(msg, 0, re, 50, 0)
	if match == nil {
		t.Fatal("expected match for tool name")
	}
	if len(match.Matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(match.Matches))
	}
	if match.Matches[0].BlockType != "tool_use" {
		t.Errorf("expected block type 'tool_use', got %q", match.Matches[0].BlockType)
	}
}

func TestSearchMessage_ThinkingBlocks(t *testing.T) {
	re, _ := CompileSearchPattern("analyzing", SearchOptions{UseRegex: false, CaseSensitive: false})
	msg := &Message{
		ID:   "msg-3",
		Role: "assistant",
		ThinkingBlocks: []ThinkingBlock{
			{Content: "I am analyzing the code"},
		},
	}

	match := SearchMessage(msg, 0, re, 50, 0)
	if match == nil {
		t.Fatal("expected match in thinking block")
	}
	if match.Matches[0].BlockType != "thinking" {
		t.Errorf("expected block type 'thinking', got %q", match.Matches[0].BlockType)
	}
}

func TestSearchMessage_ContentBlocks(t *testing.T) {
	re, _ := CompileSearchPattern("result", SearchOptions{UseRegex: false, CaseSensitive: false})
	msg := &Message{
		ID:   "msg-4",
		Role: "assistant",
		ContentBlocks: []ContentBlock{
			{Type: "tool_result", ToolOutput: "tool result output"},
		},
	}

	match := SearchMessage(msg, 0, re, 50, 0)
	if match == nil {
		t.Fatal("expected match in content block")
	}
	if match.Matches[0].BlockType != "tool_result" {
		t.Errorf("expected block type 'tool_result', got %q", match.Matches[0].BlockType)
	}
}

func TestSearchMessage_NoMatch(t *testing.T) {
	re, _ := CompileSearchPattern("xyz123", SearchOptions{UseRegex: false, CaseSensitive: false})
	msg := &Message{
		ID:      "msg-5",
		Content: "hello world",
	}

	match := SearchMessage(msg, 0, re, 50, 0)
	if match != nil {
		t.Errorf("expected no match, got %v", match)
	}
}

func TestSearchMessage_MaxResultsLimit(t *testing.T) {
	re, _ := CompileSearchPattern("line", SearchOptions{UseRegex: false, CaseSensitive: false})
	msg := &Message{
		ID:      "msg-6",
		Content: "line one\nline two\nline three\nline four\nline five",
	}

	// Start with currentTotal=3, maxResults=5, so only 2 more allowed
	match := SearchMessage(msg, 0, re, 5, 3)
	if match == nil {
		t.Fatal("expected match")
	}
	if len(match.Matches) != 2 {
		t.Errorf("expected 2 matches (limited), got %d", len(match.Matches))
	}
}

func TestSearchMessagesSlice(t *testing.T) {
	messages := []Message{
		{ID: "m1", Role: "user", Content: "hello world"},
		{ID: "m2", Role: "assistant", Content: "hi there"},
		{ID: "m3", Role: "user", Content: "hello again"},
	}

	opts := SearchOptions{UseRegex: false, CaseSensitive: false, MaxResults: 50}
	results, err := SearchMessagesSlice(messages, "hello", opts)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 message matches, got %d", len(results))
	}
	if results[0].MessageID != "m1" {
		t.Errorf("expected first match in m1, got %s", results[0].MessageID)
	}
	if results[1].MessageID != "m3" {
		t.Errorf("expected second match in m3, got %s", results[1].MessageID)
	}
}

func TestSearchMessagesSlice_MaxResultsAcrossMessages(t *testing.T) {
	messages := []Message{
		{ID: "m1", Content: "match here match"},   // 2 matches
		{ID: "m2", Content: "another match here"}, // 1 match
		{ID: "m3", Content: "match match match"},  // 3 matches
	}

	// Limit to 3 total matches
	opts := SearchOptions{UseRegex: false, CaseSensitive: false, MaxResults: 3}
	results, err := SearchMessagesSlice(messages, "match", opts)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	total := TotalMatches(results)
	if total != 3 {
		t.Errorf("expected 3 total matches, got %d", total)
	}
}

func TestSearchMessagesSlice_InvalidRegex(t *testing.T) {
	messages := []Message{{ID: "m1", Content: "test"}}
	opts := SearchOptions{UseRegex: true}
	_, err := SearchMessagesSlice(messages, "[invalid", opts)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestDeduplicateMatches(t *testing.T) {
	matches := []ContentMatch{
		{BlockType: "text", LineNo: 1, ColStart: 0, ColEnd: 5, LineText: "hello"},
		{BlockType: "text", LineNo: 1, ColStart: 0, ColEnd: 5, LineText: "hello"}, // duplicate
		{BlockType: "text", LineNo: 2, ColStart: 0, ColEnd: 5, LineText: "world"},
	}

	result := deduplicateMatches(matches)
	if len(result) != 2 {
		t.Errorf("expected 2 unique matches, got %d", len(result))
	}
}
