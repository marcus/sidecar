package claudecode

import (
	"encoding/json"
	"time"
)

// RawMessage represents a raw JSONL line from Claude Code.
type RawMessage struct {
	Type       string          `json:"type"`
	UUID       string          `json:"uuid"`
	ParentUUID *string         `json:"parentUuid"`
	SessionID  string          `json:"sessionId"`
	Timestamp  time.Time       `json:"timestamp"`
	Message    *MessageContent `json:"message,omitempty"`
	CWD        string          `json:"cwd,omitempty"`
	Version    string          `json:"version,omitempty"`
	GitBranch  string          `json:"gitBranch,omitempty"`
	Slug       string          `json:"slug,omitempty"`
}

// MessageContent holds the actual message data.
type MessageContent struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model,omitempty"`
	ID      string          `json:"id,omitempty"`
	Usage   *Usage          `json:"usage,omitempty"`
}

// Usage tracks token usage for a message.
type Usage struct {
	InputTokens              int            `json:"input_tokens"`
	OutputTokens             int            `json:"output_tokens"`
	CacheCreationInputTokens int            `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int            `json:"cache_read_input_tokens"`
	CacheCreation            *CacheCreation `json:"cache_creation,omitempty"`
}

// CacheCreation holds cache-specific token data.
type CacheCreation struct {
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
}

// ContentBlock represents a single block in the content array.
type ContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	ID        string `json:"id,omitempty"`          // tool_use ID
	Name      string `json:"name,omitempty"`        // tool name
	Input     any    `json:"input,omitempty"`       // tool input
	ToolUseID string `json:"tool_use_id,omitempty"` // for tool_result linking
	Content   any    `json:"content,omitempty"`     // tool_result content (string or array)
	IsError   bool   `json:"is_error,omitempty"`    // tool_result error flag
}

// ToolResult represents the result of a tool call.
type ToolResult struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// SessionMetadata holds metadata about a session file.
type SessionMetadata struct {
	Path             string
	SessionID        string
	Slug             string // Short slug from summary line (e.g., "ses_abc123")
	CWD              string
	Version          string
	GitBranch        string
	FirstMsg         time.Time
	LastMsg          time.Time
	MsgCount         int
	TotalTokens      int     // Sum of input + output tokens
	EstCost          float64 // Estimated cost based on model usage
	PrimaryModel     string  // Most used model in session
	FirstUserMessage string  // Content of the first user message (for title)
}
