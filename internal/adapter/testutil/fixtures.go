package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ClaudeCodeMessage represents a JSONL message in Claude Code format.
type ClaudeCodeMessage struct {
	Type      string                `json:"type"`
	UUID      string                `json:"uuid"`
	SessionID string                `json:"sessionId"`
	Timestamp time.Time             `json:"timestamp"`
	Message   *ClaudeCodeMsgContent `json:"message,omitempty"`
	CWD       string                `json:"cwd,omitempty"`
	Version   string                `json:"version,omitempty"`
}

// ClaudeCodeMsgContent holds the actual message content.
type ClaudeCodeMsgContent struct {
	Role    string           `json:"role"`
	Content json.RawMessage  `json:"content"`
	Model   string           `json:"model,omitempty"`
	Usage   *ClaudeCodeUsage `json:"usage,omitempty"`
}

// ClaudeCodeUsage tracks token usage.
type ClaudeCodeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// GenerateClaudeCodeSessionFile creates a JSONL file with realistic message content.
// messageCount determines the number of message pairs (user + assistant).
// avgMessageSize is the approximate size of message content in bytes.
func GenerateClaudeCodeSessionFile(path string, messageCount int, avgMessageSize int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	sessionID := "bench-session-001"
	enc := json.NewEncoder(f)
	baseTime := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// Write summary line
	summary := ClaudeCodeMessage{
		Type:      "summary",
		UUID:      "summary-001",
		SessionID: sessionID,
		Timestamp: baseTime,
		CWD:       "/home/user/project",
		Version:   "0.2.61",
	}
	if err := enc.Encode(summary); err != nil {
		return err
	}

	// Generate message pairs
	for i := 0; i < messageCount; i++ {
		ts := baseTime.Add(time.Duration(i*2) * time.Second)

		// User message
		userContent := generateTextContent(avgMessageSize/2, "user", i)
		user := ClaudeCodeMessage{
			Type:      "user",
			UUID:      fmt.Sprintf("msg-user-%06d", i),
			SessionID: sessionID,
			Timestamp: ts,
			Message: &ClaudeCodeMsgContent{
				Role:    "user",
				Content: userContent,
			},
		}
		if err := enc.Encode(user); err != nil {
			return err
		}

		// Assistant message with tool use and thinking
		assistantContent := generateAssistantContent(avgMessageSize/2, i)
		assistant := ClaudeCodeMessage{
			Type:      "assistant",
			UUID:      fmt.Sprintf("msg-asst-%06d", i),
			SessionID: sessionID,
			Timestamp: ts.Add(time.Second),
			Message: &ClaudeCodeMsgContent{
				Role:    "assistant",
				Content: assistantContent,
				Model:   "claude-sonnet-4-20250514",
				Usage: &ClaudeCodeUsage{
					InputTokens:  500 + (i % 100),
					OutputTokens: 200 + (i % 50),
				},
			},
		}
		if err := enc.Encode(assistant); err != nil {
			return err
		}
	}

	return nil
}

// generateTextContent creates a content block with text of approximately the given size.
func generateTextContent(size int, role string, index int) json.RawMessage {
	text := fmt.Sprintf("%s message #%d: ", role, index)
	padding := make([]byte, size-len(text))
	for i := range padding {
		padding[i] = 'x'
	}
	text += string(padding)

	blocks := []map[string]any{
		{"type": "text", "text": text},
	}
	data, _ := json.Marshal(blocks)
	return data
}

// generateAssistantContent creates assistant content with tool use and thinking blocks.
func generateAssistantContent(size int, index int) json.RawMessage {
	textSize := size / 2
	thinkingSize := size / 4
	toolInputSize := size / 4

	// Create padding
	textPadding := make([]byte, textSize)
	for i := range textPadding {
		textPadding[i] = 'a'
	}

	thinkingPadding := make([]byte, thinkingSize)
	for i := range thinkingPadding {
		thinkingPadding[i] = 't'
	}

	toolInput := make([]byte, toolInputSize)
	for i := range toolInput {
		toolInput[i] = 'i'
	}

	blocks := []map[string]any{
		{"type": "thinking", "thinking": fmt.Sprintf("Thinking about message %d: %s", index, string(thinkingPadding))},
		{"type": "text", "text": fmt.Sprintf("Response %d: %s", index, string(textPadding))},
	}

	// Add tool use every 5 messages
	if index%5 == 0 {
		blocks = append(blocks, map[string]any{
			"type":  "tool_use",
			"id":    fmt.Sprintf("toolu_%06d", index),
			"name":  "Read",
			"input": map[string]any{"file_path": fmt.Sprintf("/path/to/file_%d.go", index), "content": string(toolInput)},
		})
	}

	data, _ := json.Marshal(blocks)
	return data
}

// CodexMessage represents a JSONL message in Codex format.
type CodexMessage struct {
	Type      string    `json:"type"`
	SessionID string    `json:"session_id,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Role      string    `json:"role,omitempty"`
	Content   string    `json:"content,omitempty"`
	Model     string    `json:"model,omitempty"`
	CallID    string    `json:"call_id,omitempty"`
	Name      string    `json:"name,omitempty"`
	Arguments string    `json:"arguments,omitempty"`
	Output    string    `json:"output,omitempty"`
	Input     int       `json:"input,omitempty"`
	Output_   int       `json:"output_,omitempty"` // for usage
}

// GenerateCodexSessionFile creates a JSONL file in Codex format.
func GenerateCodexSessionFile(path string, messageCount int, avgMessageSize int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	sessionID := "codex-bench-001"
	enc := json.NewEncoder(f)
	baseTime := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// Write session start
	start := CodexMessage{
		Type:      "session_start",
		SessionID: sessionID,
		Timestamp: baseTime,
	}
	if err := enc.Encode(start); err != nil {
		return err
	}

	// Generate messages
	for i := 0; i < messageCount; i++ {
		ts := baseTime.Add(time.Duration(i*2) * time.Second)

		// User message
		userContent := generatePaddedString(avgMessageSize/2, fmt.Sprintf("User message %d: ", i))
		user := CodexMessage{
			Type:      "message",
			Role:      "user",
			Content:   userContent,
			Timestamp: ts,
		}
		if err := enc.Encode(user); err != nil {
			return err
		}

		// Assistant message
		assistantContent := generatePaddedString(avgMessageSize/2, fmt.Sprintf("Assistant response %d: ", i))
		assistant := CodexMessage{
			Type:      "message",
			Role:      "assistant",
			Content:   assistantContent,
			Model:     "gpt-4o",
			Timestamp: ts.Add(time.Second),
		}
		if err := enc.Encode(assistant); err != nil {
			return err
		}

		// Add tool call every 5 messages
		if i%5 == 0 {
			toolCall := CodexMessage{
				Type:      "function_call",
				CallID:    fmt.Sprintf("call_%06d", i),
				Name:      "shell",
				Arguments: fmt.Sprintf(`{"cmd": "echo test %d"}`, i),
				Timestamp: ts.Add(500 * time.Millisecond),
			}
			if err := enc.Encode(toolCall); err != nil {
				return err
			}

			toolOutput := CodexMessage{
				Type:      "function_output",
				CallID:    fmt.Sprintf("call_%06d", i),
				Output:    fmt.Sprintf("test %d\n", i),
				Timestamp: ts.Add(600 * time.Millisecond),
			}
			if err := enc.Encode(toolOutput); err != nil {
				return err
			}
		}
	}

	return nil
}

// generatePaddedString creates a string of approximately the given size with a prefix.
func generatePaddedString(size int, prefix string) string {
	if size <= len(prefix) {
		return prefix
	}
	padding := make([]byte, size-len(prefix))
	for i := range padding {
		padding[i] = 'x'
	}
	return prefix + string(padding)
}

// ApproximateMessageCount returns the message count to generate a file of approximately the given size.
// size is in bytes, avgMessageSize is the target size per message pair.
func ApproximateMessageCount(targetSize int64, avgMessageSize int) int {
	// Each message pair is approximately 2 * avgMessageSize + overhead (~100 bytes)
	pairSize := 2*avgMessageSize + 100
	return int(targetSize) / pairSize
}
