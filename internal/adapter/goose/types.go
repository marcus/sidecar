package goose

import "encoding/json"

// GooseContentBlock represents one item in Goose's messages.content_json array.
type GooseContentBlock struct {
	Type       string            `json:"type"`
	Text       string            `json:"text,omitempty"`
	Thinking   string            `json:"thinking,omitempty"`
	ID         string            `json:"id,omitempty"`
	ToolCall   ToolResultValue   `json:"toolCall,omitempty"`
	ToolResult ToolResponseValue `json:"toolResult,omitempty"`
}

// ToolResultValue matches Goose's serialized tool request format.
type ToolResultValue struct {
	Status string        `json:"status"`
	Value  ToolCallValue `json:"value"`
	Error  string        `json:"error,omitempty"`
}

// ToolCallValue contains tool name + arguments for toolRequest blocks.
type ToolCallValue struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolResponseValue matches Goose's serialized tool response format.
type ToolResponseValue struct {
	Status string            `json:"status"`
	Value  ToolResponseInner `json:"value"`
	Error  string            `json:"error,omitempty"`
}

// ToolResponseInner contains response content for toolResponse blocks.
type ToolResponseInner struct {
	Content []ToolResponseContent `json:"content"`
}

// ToolResponseContent captures text from rmcp Content values.
type ToolResponseContent struct {
	Type string          `json:"type"`
	Text string          `json:"text"`
	Raw  json.RawMessage `json:"-"`
}
