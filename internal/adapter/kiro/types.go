package kiro

import "encoding/json"

// ConversationRow represents a row from the conversations_v2 table.
type ConversationRow struct {
	Key            string // project path (e.g., /Users/foo/code/project)
	ConversationID string
	Value          string // JSON blob
	CreatedAt      int64  // Unix timestamp in milliseconds
	UpdatedAt      int64  // Unix timestamp in milliseconds
}

// ConversationValue is the parsed JSON value from conversations_v2.
type ConversationValue struct {
	ConversationID string          `json:"conversation_id"`
	History        []HistoryEntry  `json:"history"`
	ModelInfo      *ModelInfo      `json:"model_info"`
	Tools          json.RawMessage `json:"tools"`
	ContextManager *ContextManager `json:"context_manager"`
}

// HistoryEntry is a single turn in the conversation history.
type HistoryEntry struct {
	User            *UserMessage     `json:"user"`
	Assistant       json.RawMessage  `json:"assistant"` // Response or ToolUse, discriminated
	RequestMetadata *RequestMetadata `json:"request_metadata"`
}

// UserMessage is the user side of a history entry.
type UserMessage struct {
	Content    json.RawMessage `json:"content"`    // Prompt or ToolUseResults, discriminated
	Timestamp  string          `json:"timestamp"`  // RFC3339 format
	EnvContext *EnvContext     `json:"env_context"`
}

// EnvContext holds environment info from the user message.
type EnvContext struct {
	EnvState *EnvState `json:"env_state"`
}

// EnvState holds OS and directory info.
type EnvState struct {
	OperatingSystem         string `json:"operating_system"`
	CurrentWorkingDirectory string `json:"current_working_directory"`
}

// PromptContent is the user content when it's a Prompt.
type PromptContent struct {
	Prompt *PromptData `json:"Prompt"`
}

// PromptData contains the actual prompt text.
type PromptData struct {
	Prompt string `json:"prompt"`
}

// ToolUseResultsContent is the user content when it's ToolUseResults.
type ToolUseResultsContent struct {
	ToolUseResults *ToolUseResultsData `json:"ToolUseResults"`
}

// ToolUseResultsData contains the tool use results.
type ToolUseResultsData struct {
	ToolUseResults []ToolUseResult `json:"tool_use_results"`
}

// ToolUseResult is a single tool result.
type ToolUseResult struct {
	ToolUseID string             `json:"tool_use_id"`
	Content   []ToolResultContent `json:"content"`
	Status    string             `json:"status"`
}

// ToolResultContent is a content block in a tool result.
type ToolResultContent struct {
	Json *ToolResultJSON `json:"Json"`
}

// ToolResultJSON contains structured tool output.
type ToolResultJSON struct {
	ExitStatus string `json:"exit_status"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
}

// AssistantResponse is the assistant content when it's a Response.
type AssistantResponse struct {
	Response *ResponseData `json:"Response"`
}

// ResponseData contains the response text.
type ResponseData struct {
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
}

// AssistantToolUse is the assistant content when it's a ToolUse.
type AssistantToolUse struct {
	ToolUse *ToolUseData `json:"ToolUse"`
}

// ToolUseData contains the tool use info.
type ToolUseData struct {
	MessageID string          `json:"message_id"`
	Content   string          `json:"content"`
	ToolUses  []ToolUseEntry  `json:"tool_uses"`
}

// ToolUseEntry is a single tool invocation.
type ToolUseEntry struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// RequestMetadata holds timing and context info for a request.
type RequestMetadata struct {
	ContextUsagePercentage   float64 `json:"context_usage_percentage"`
	RequestStartTimestampMs  int64   `json:"request_start_timestamp_ms"`
	StreamEndTimestampMs     int64   `json:"stream_end_timestamp_ms"`
}

// ModelInfo holds model configuration.
type ModelInfo struct {
	ModelName string `json:"model_name"`
	ModelID   string `json:"model_id"`
}

// ContextManager holds context paths.
type ContextManager struct {
	Paths []string `json:"paths"`
}
