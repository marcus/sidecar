package adapter

import "time"

// Adapter provides access to AI session data from various sources.
type Adapter interface {
	ID() string
	Name() string
	Icon() string
	Detect(projectRoot string) (bool, error)
	Capabilities() CapabilitySet
	Sessions(projectRoot string) ([]Session, error)
	Messages(sessionID string) ([]Message, error)
	Usage(sessionID string) (*UsageStats, error)
	Watch(projectRoot string) (<-chan Event, error)
}

// ProjectDiscoverer is an optional interface for adapters that can discover
// related project directories (e.g., from deleted worktrees).
type ProjectDiscoverer interface {
	// DiscoverRelatedProjectDirs returns paths to project directories that
	// appear related to the given main worktree path. Used to find conversations
	// from deleted worktrees that git no longer knows about.
	DiscoverRelatedProjectDirs(mainWorktreePath string) ([]string, error)
}

// TargetedRefresher is an optional interface for adapters that support refreshing
// a single session by ID without scanning the full directory (td-2b8ebe).
type TargetedRefresher interface {
	SessionByID(sessionID string) (*Session, error)
}

// WatchScope indicates whether an adapter watches global or per-project paths.
type WatchScope int

const (
	// WatchScopeProject indicates the adapter watches a unique path per project.
	WatchScopeProject WatchScope = iota
	// WatchScopeGlobal indicates the adapter watches a global path regardless of project.
	WatchScopeGlobal
)

// WatchScopeProvider is an optional interface for adapters to indicate their watch scope (td-7a72b6f7).
// Adapters that watch global paths (like codex, warp) should implement this to avoid duplicate watchers
// when the plugin iterates over multiple worktree paths.
type WatchScopeProvider interface {
	// WatchScope returns the scope of this adapter's Watch method.
	// Global-scoped adapters only need one watcher regardless of worktree paths.
	WatchScope() WatchScope
}

// Capability represents a feature supported by an adapter.
type Capability string

const (
	CapSessions Capability = "sessions"
	CapMessages Capability = "messages"
	CapUsage    Capability = "usage"
	CapWatch    Capability = "watch"
)

// CapabilitySet tracks which features an adapter supports.
type CapabilitySet map[Capability]bool

// Session represents an AI coding session.
type Session struct {
	ID           string
	Name         string
	Slug         string // Short identifier for display (e.g., "ses_abc123")
	AdapterID    string // Adapter identifier (e.g., "claude-code", "codex")
	AdapterName  string // Human-readable adapter name
	AdapterIcon  string // Single character icon for badge display
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Duration     time.Duration
	IsActive     bool
	TotalTokens  int     // Sum of input + output tokens
	EstCost      float64 // Estimated cost in dollars
	IsSubAgent   bool    // True if this is a sub-agent spawned by another session
	MessageCount int     // Number of user/assistant messages (0 = metadata-only)

	// Worktree fields - populated when session is from a different worktree
	WorktreeName string // Branch name or directory name of the worktree (empty if main or non-worktree)
	WorktreePath string // Absolute path to the worktree (empty if same as current workdir)
}

// ThinkingBlock represents Claude's extended thinking content.
type ThinkingBlock struct {
	Content    string
	TokenCount int // Estimated from len(Content)/4
}

// ContentBlock represents a single block in structured message content.
type ContentBlock struct {
	Type       string // "text", "tool_use", "tool_result", "thinking"
	Text       string // For text/thinking blocks
	ToolUseID  string // For tool_use and tool_result linking
	ToolName   string // For tool_use
	ToolInput  string // For tool_use (JSON string)
	ToolOutput string // For tool_result
	IsError    bool   // For tool_result errors
	TokenCount int    // For thinking blocks
}

// Message represents a message in a session.
type Message struct {
	ID        string
	Role      string
	Content   string
	Timestamp time.Time
	Model     string // Model ID (e.g., "claude-opus-4-5-20251101")
	TokenUsage
	ToolUses       []ToolUse
	ThinkingBlocks []ThinkingBlock
	ContentBlocks  []ContentBlock // Structured content for rich display
}

// TokenUsage tracks token counts for a message or session.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheWrite   int
}

// ToolUse represents a tool call made by the AI.
type ToolUse struct {
	ID     string
	Name   string
	Input  string
	Output string
}

// UsageStats provides aggregate usage statistics.
type UsageStats struct {
	TotalInputTokens  int
	TotalOutputTokens int
	TotalCacheRead    int
	TotalCacheWrite   int
	MessageCount      int
}

// Event represents a change in session data.
type Event struct {
	Type      EventType
	SessionID string
	Data      any
}

// EventType identifies the kind of adapter event.
type EventType string

const (
	EventSessionCreated EventType = "session_created"
	EventSessionUpdated EventType = "session_updated"
	EventMessageAdded   EventType = "message_added"
)
