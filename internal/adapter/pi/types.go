package pi

import (
	"encoding/json"
	"time"
)

// RawLine represents any JSONL line from a Pi session file.
// Fields are a superset; only relevant fields are populated per line type.
type RawLine struct {
	Type      string          `json:"type"`              // "session", "message", "model_change", "thinking_level_change", "custom"
	ID        string          `json:"id"`                // line identifier
	ParentID  *string         `json:"parentId"`          // parent line reference (nullable)
	Timestamp time.Time       `json:"timestamp"`         // line timestamp
	Message   *MessageContent `json:"message,omitempty"` // populated for type="message"

	// Session header fields (type="session")
	Version int    `json:"version,omitempty"` // session format version
	CWD     string `json:"cwd,omitempty"`     // working directory

	// Model change fields (type="model_change")
	Provider string `json:"provider,omitempty"` // e.g. "anthropic"
	ModelID  string `json:"modelId,omitempty"`  // e.g. "claude-opus-4-5"

	// Thinking level change fields (type="thinking_level_change")
	ThinkingLevel string `json:"thinkingLevel,omitempty"` // e.g. "low", "medium", "high"

	// Custom event fields (type="custom")
	CustomType string          `json:"customType,omitempty"` // e.g. "model-snapshot"
	Data       json.RawMessage `json:"data,omitempty"`       // arbitrary event data
}

// MessageContent holds the message payload for type="message" lines.
type MessageContent struct {
	Role       string          `json:"role"`                 // "user", "assistant", "toolResult"
	Content    json.RawMessage `json:"content"`              // array of ContentBlock
	Model      string          `json:"model,omitempty"`      // model ID for assistant messages
	Provider   string          `json:"provider,omitempty"`   // e.g. "anthropic"
	API        string          `json:"api,omitempty"`        // e.g. "anthropic-messages"
	Usage      *Usage          `json:"usage,omitempty"`      // token usage for assistant messages
	StopReason string          `json:"stopReason,omitempty"` // e.g. "end_turn"
	ToolCallID string          `json:"toolCallId,omitempty"` // for toolResult: links to toolCall block ID
	ToolName   string          `json:"toolName,omitempty"`   // for toolResult: name of the tool
	Details    *Details        `json:"details,omitempty"`    // for toolResult: extra info (e.g. diff)
}

// Usage tracks token counts and cost for an assistant message.
type Usage struct {
	Input       int   `json:"input"`       // input tokens
	Output      int   `json:"output"`      // output tokens
	CacheRead   int   `json:"cacheRead"`   // cache read tokens
	CacheWrite  int   `json:"cacheWrite"`  // cache write tokens
	TotalTokens int   `json:"totalTokens"` // total tokens
	Cost        *Cost `json:"cost,omitempty"`
}

// Cost holds pre-calculated cost breakdown in dollars.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// Details holds extra information attached to toolResult messages.
type Details struct {
	Diff string `json:"diff,omitempty"`
}

// ContentBlock represents a single block in a message's content array.
type ContentBlock struct {
	Type              string          `json:"type"`                        // "text", "thinking", "toolCall", "image"
	Text              string          `json:"text,omitempty"`              // for text blocks
	Thinking          string          `json:"thinking,omitempty"`          // for thinking blocks
	ThinkingSignature string          `json:"thinkingSignature,omitempty"` // for thinking blocks
	ID                string          `json:"id,omitempty"`                // for toolCall: the tool use ID
	Name              string          `json:"name,omitempty"`              // for toolCall: tool name
	Arguments         json.RawMessage `json:"arguments,omitempty"`         // for toolCall: tool input
}

// SessionMetadata holds extracted metadata about a Pi session file.
type SessionMetadata struct {
	Path             string    // absolute path to session file
	SessionID        string    // unique session ID from header
	CWD              string    // working directory from header
	Version          int       // session format version
	FirstMsg         time.Time // timestamp of first user/assistant message
	LastMsg          time.Time // timestamp of last user/assistant message
	MsgCount         int       // count of user/assistant messages
	TotalTokens      int       // sum of tokens across all assistant messages
	EstCost          float64   // total cost from pre-calculated usage.cost.total
	PrimaryModel     string    // most-used model in session
	FirstUserMessage string    // content of first user message (used as title)
	SessionCategory  string    // "interactive", "cron", "system"
	CronJobName      string    // extracted job name from "[cron:UUID name]" prefix
	SourceChannel    string    // "telegram", "whatsapp", "direct"
}
