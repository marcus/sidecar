package copilot

import "time"

// CopilotEvent represents a single event from events.jsonl
type CopilotEvent struct {
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data"`
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	ParentID  *string                `json:"parentId"`
}

// WorkspaceYAML represents the workspace.yaml metadata
type WorkspaceYAML struct {
	ID           string    `yaml:"id"`
	CWD          string    `yaml:"cwd"`
	GitRoot      string    `yaml:"git_root"`
	Branch       string    `yaml:"branch"`
	Summary      string    `yaml:"summary"`
	SummaryCount int       `yaml:"summary_count"`
	CreatedAt    time.Time `yaml:"created_at"`
	UpdatedAt    time.Time `yaml:"updated_at"`
}

// ToolRequest represents a tool call in an assistant message
type ToolRequest struct {
	ToolCallID string                 `json:"toolCallId"`
	Name       string                 `json:"name"`
	Arguments  map[string]interface{} `json:"arguments"`
	Type       string                 `json:"type"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Content         string                 `json:"content"`
	DetailedContent string                 `json:"detailedContent"`
	Extra           map[string]interface{} `json:"-"` // Catch-all for other fields
}
