package warp

import (
	"encoding/json"
	"time"
)

// AIQueryRow represents a row from the ai_queries table.
type AIQueryRow struct {
	ID               int
	ExchangeID       string
	ConversationID   string
	Input            string // JSON array
	OutputStatus     string
	ModelID          string
	PlanningModelID  string
	CodingModelID    string
	WorkingDirectory string
	StartTS          time.Time
}

// QueryInput represents the parsed input JSON from ai_queries.
// Format: [{"Query": {...}}]
type QueryInput struct {
	Query struct {
		Text                  string          `json:"text"`
		Context               []QueryContext  `json:"context"`
		ReferencedAttachments json.RawMessage `json:"referenced_attachments"`
	} `json:"Query"`
}

// QueryContext represents context items in the query.
type QueryContext struct {
	Directory            *DirectoryContext            `json:",omitempty"`
	Git                  *GitContext                  `json:",omitempty"`
	ProjectRules         *ProjectRulesContext         `json:",omitempty"`
	CurrentTime          *CurrentTimeContext          `json:",omitempty"`
	ExecutionEnvironment *ExecutionEnvironmentContext `json:",omitempty"`
	Codebase             *CodebaseContext             `json:",omitempty"`
}

// DirectoryContext contains the working directory info.
type DirectoryContext struct {
	PWD     string `json:"pwd"`
	HomeDir string `json:"home_dir"`
}

// GitContext contains git state.
type GitContext struct {
	Head string `json:"head"`
}

// ProjectRulesContext contains project rules.
type ProjectRulesContext struct {
	RootPath    string          `json:"root_path"`
	ActiveRules json.RawMessage `json:"active_rules"` // Complex nested structure, not parsed
}

// CurrentTimeContext contains the timestamp.
type CurrentTimeContext struct {
	CurrentTime string `json:"current_time"`
}

// ExecutionEnvironmentContext contains OS/shell info.
type ExecutionEnvironmentContext struct {
	OS        *OSInfo `json:"os"`
	ShellName string  `json:"shell_name"`
}

// OSInfo contains OS category.
type OSInfo struct {
	Category string `json:"category"`
}

// CodebaseContext contains project info.
type CodebaseContext struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// AgentConversationRow represents a row from agent_conversations.
type AgentConversationRow struct {
	ID               int
	ConversationID   string
	ConversationData string // JSON
	LastModifiedAt   time.Time
}

// ConversationData represents the parsed conversation_data JSON.
type ConversationData struct {
	ServerConversationToken string                     `json:"server_conversation_token"`
	UsageMetadata           *ConversationUsageMetadata `json:"conversation_usage_metadata"`
}

// ConversationUsageMetadata contains usage statistics.
type ConversationUsageMetadata struct {
	WasSummarized         bool               `json:"was_summarized"`
	ContextWindowUsage    float64            `json:"context_window_usage"`
	CreditsSpent          float64            `json:"credits_spent"`
	CreditsSpentLastBlock float64            `json:"credits_spent_for_last_block"`
	TokenUsage            []TokenUsageItem   `json:"token_usage"`
	ToolUsageMetadata     *ToolUsageMetadata `json:"tool_usage_metadata"`
}

// TokenUsageItem contains token usage for a specific model.
type TokenUsageItem struct {
	ModelID              string         `json:"model_id"`
	WarpTokens           int            `json:"warp_tokens"`
	BYOKTokens           int            `json:"byok_tokens"`
	WarpTokensByCategory map[string]int `json:"warp_token_usage_by_category"`
}

// ToolUsageMetadata contains tool call statistics.
type ToolUsageMetadata struct {
	RunCommand      *ToolStats `json:"run_command_stats"`
	ReadFiles       *ToolStats `json:"read_files_stats"`
	Grep            *ToolStats `json:"grep_stats"`
	FileGlob        *ToolStats `json:"file_glob_stats"`
	ApplyFileDiff   *DiffStats `json:"apply_file_diff_stats"`
	ReadShellOutput *ToolStats `json:"read_shell_command_output_stats"`
}

// ToolStats contains basic tool call counts.
type ToolStats struct {
	Count            int `json:"count"`
	CommandsExecuted int `json:"commands_executed,omitempty"`
}

// DiffStats contains file diff statistics.
type DiffStats struct {
	Count        int `json:"count"`
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

// BlockRow represents a row from the blocks table.
type BlockRow struct {
	ID              int
	PaneLeafUUID    []byte
	StylizedCommand []byte
	StylizedOutput  []byte
	PWD             string
	ExitCode        int
	StartTS         time.Time
	CompletedTS     time.Time
	AIMetadata      string // JSON, nullable
}

// BlockAIMetadata represents the parsed ai_metadata JSON from blocks.
type BlockAIMetadata struct {
	ActionID          string          `json:"action_id"`
	ConversationID    string          `json:"conversation_id"`
	ConversationPhase json.RawMessage `json:"conversation_phase"`
}

// ModelDisplayNames maps Warp model IDs to display names.
var ModelDisplayNames = map[string]string{
	"claude-4-5-opus":          "Claude Opus 4.5",
	"claude-4-5-opus-thinking": "Claude Opus 4.5 (Thinking)",
	"gpt-5-1-high-reasoning":   "GPT-5",
}
