package opencode

import (
	"encoding/json"
	"time"
)

// Project represents an OpenCode project from storage/project/{id}.json.
type Project struct {
	ID       string   `json:"id"`
	Worktree string   `json:"worktree"`
	VCS      string   `json:"vcs,omitempty"`
	Sandboxes []string `json:"sandboxes,omitempty"`
	Time     TimeInfo `json:"time"`
	Icon     *Icon    `json:"icon,omitempty"`
}

// Icon holds project icon settings.
type Icon struct {
	Color string `json:"color,omitempty"`
}

// Session represents an OpenCode session from storage/session/{projectID}/{sessionID}.json.
type Session struct {
	ID        string       `json:"id"`
	Version   string       `json:"version,omitempty"`
	ProjectID string       `json:"projectID"`
	Directory string       `json:"directory"`
	Title     string       `json:"title,omitempty"`
	ParentID  string       `json:"parentID,omitempty"` // Non-empty for sub-agents
	Time      TimeInfo     `json:"time"`
	Summary   *DiffSummary `json:"summary,omitempty"`
}

// DiffSummary holds aggregate diff statistics for a session.
type DiffSummary struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Files     int `json:"files"`
}

// Message represents an OpenCode message from storage/message/{sessionID}/{messageID}.json.
type Message struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"sessionID"`
	Role       string          `json:"role"` // "user" or "assistant"
	Time       TimeInfo        `json:"time"`
	ParentID   string          `json:"parentID,omitempty"`
	ModelID    string          `json:"modelID,omitempty"`
	ProviderID string          `json:"providerID,omitempty"`
	Mode       string          `json:"mode,omitempty"`
	Agent      string          `json:"agent,omitempty"`
	Path       *PathInfo       `json:"path,omitempty"`
	Cost       float64         `json:"cost,omitempty"`
	Tokens     *TokenInfo      `json:"tokens,omitempty"`
	Finish     string          `json:"finish,omitempty"`
	Summary    *MessageSummary `json:"summary,omitempty"`
	Model      *ModelInfo      `json:"model,omitempty"` // Alternative model location for user messages
}

// PathInfo holds working directory information.
type PathInfo struct {
	CWD  string `json:"cwd"`
	Root string `json:"root"`
}

// ModelInfo holds model information (used in user messages).
type ModelInfo struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// MessageSummary holds message summary information.
type MessageSummary struct {
	Title string        `json:"title,omitempty"`
	Diffs []SummaryDiff `json:"diffs,omitempty"`
}

// SummaryDiff represents a diff entry in message summary.
type SummaryDiff struct {
	File      string `json:"file"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// Part represents a content part from storage/part/{messageID}/{partID}.json.
type Part struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionID"`
	MessageID string    `json:"messageID"`
	Type      string    `json:"type"` // text, tool, step-start, step-finish, file, patch, compaction
	Time      *PartTime `json:"time,omitempty"`

	// Text part fields
	Text string `json:"text,omitempty"`

	// Tool part fields
	CallID string     `json:"callID,omitempty"`
	Tool   string     `json:"tool,omitempty"`
	State  *ToolState `json:"state,omitempty"`

	// Step part fields
	Snapshot string     `json:"snapshot,omitempty"`
	Reason   string     `json:"reason,omitempty"`
	Cost     float64    `json:"cost,omitempty"`
	Tokens   *TokenInfo `json:"tokens,omitempty"`

	// File part fields (type: "file")
	Mime     string      `json:"mime,omitempty"`
	Filename string      `json:"filename,omitempty"`
	URL      string      `json:"url,omitempty"`
	Source   *FileSource `json:"source,omitempty"`

	// Patch part fields (type: "patch")
	Hash  string   `json:"hash,omitempty"`
	Files []string `json:"files,omitempty"`
}

// PartTime holds timing info for parts.
type PartTime struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

// FileSource holds file reference source information.
type FileSource struct {
	Type string          `json:"type"`
	Path string          `json:"path"`
	Text *FileSourceText `json:"text,omitempty"`
}

// FileSourceText holds text range for file references.
type FileSourceText struct {
	Value string `json:"value"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// ToolState holds the state of a tool call.
type ToolState struct {
	Status   string         `json:"status"` // "completed", "pending", etc.
	Input    map[string]any `json:"input,omitempty"`
	Output   any            `json:"output,omitempty"` // Can be string or object
	Title    string         `json:"title,omitempty"`
	Metadata *ToolMetadata  `json:"metadata,omitempty"`
	Time     *ToolTime      `json:"time,omitempty"`
}

// ToolMetadata holds additional tool metadata.
type ToolMetadata struct {
	Output      string `json:"output,omitempty"`
	Exit        int    `json:"exit,omitempty"`
	Description string `json:"description,omitempty"`
}

// ToolTime holds timing info for tool execution.
type ToolTime struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

// TimeInfo holds timestamp information in Unix milliseconds.
type TimeInfo struct {
	Created   int64 `json:"created,omitempty"`
	Updated   int64 `json:"updated,omitempty"`
	Completed int64 `json:"completed,omitempty"`
}

// CreatedTime converts created timestamp to time.Time in local timezone.
func (t TimeInfo) CreatedTime() time.Time {
	if t.Created == 0 {
		return time.Time{}
	}
	return time.UnixMilli(t.Created).Local()
}

// UpdatedTime converts updated timestamp to time.Time in local timezone.
func (t TimeInfo) UpdatedTime() time.Time {
	if t.Updated == 0 {
		return t.CreatedTime()
	}
	return time.UnixMilli(t.Updated).Local()
}

// CompletedTime converts completed timestamp to time.Time in local timezone.
func (t TimeInfo) CompletedTime() time.Time {
	if t.Completed == 0 {
		return time.Time{}
	}
	return time.UnixMilli(t.Completed).Local()
}

// TokenInfo holds token usage information.
type TokenInfo struct {
	Input     int        `json:"input,omitempty"`
	Output    int        `json:"output,omitempty"`
	Reasoning int        `json:"reasoning,omitempty"`
	Cache     *CacheInfo `json:"cache,omitempty"`
}

// CacheInfo holds cache-related token information.
type CacheInfo struct {
	Read  int `json:"read,omitempty"`
	Write int `json:"write,omitempty"`
}

// SessionDiffEntry represents a file diff from storage/session_diff/{sessionID}.json.
type SessionDiffEntry struct {
	File      string `json:"file"`
	Before    string `json:"before"`
	After     string `json:"after"`
	Additions *int   `json:"additions"` // nil for binary files
	Deletions *int   `json:"deletions"`
}

// SessionMetadata holds aggregated metadata for adapter.Session mapping.
type SessionMetadata struct {
	Path             string
	SessionID        string
	ProjectID        string
	Title            string
	ParentID         string
	FirstMsg         time.Time
	LastMsg          time.Time
	MsgCount         int
	TotalTokens      int
	EstCost          float64
	PrimaryModel     string
	Additions        int
	Deletions        int
	FileCount        int
	FirstUserMessage string // Content of the first user message (for title fallback)
}

// ToolInputString extracts a string representation of tool input.
func ToolInputString(input map[string]any) string {
	if input == nil {
		return ""
	}
	b, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	return string(b)
}

// ToolOutputString extracts a string representation of tool output.
func ToolOutputString(output any) string {
	if output == nil {
		return ""
	}
	switch v := output.(type) {
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
