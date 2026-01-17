package worktree

import (
	"crypto/sha256"
	"strings"
	"sync"
	"time"
)

// ViewMode represents the current view state.
type ViewMode int

const (
	ViewModeList            ViewMode = iota // List view (default)
	ViewModeKanban                          // Kanban board view
	ViewModeCreate                          // New worktree modal
	ViewModeTaskLink                        // Task link modal (for existing worktrees)
	ViewModeMerge                           // Merge workflow modal
	ViewModeAgentChoice                     // Agent action choice modal (attach/restart)
	ViewModeConfirmDelete                   // Delete confirmation modal
	ViewModeCommitForMerge                  // Commit modal before merge workflow
	ViewModePromptPicker                    // Prompt template picker modal
)

// FocusPane represents which pane is active in the split view.
type FocusPane int

const (
	PaneSidebar FocusPane = iota // Worktree list
	PanePreview                  // Preview pane (output/diff/task)
)

// PreviewTab represents the active tab in the preview pane.
type PreviewTab int

const (
	PreviewTabOutput PreviewTab = iota // Agent output
	PreviewTabDiff                     // Git diff
	PreviewTabTask                     // TD task info
)

// DiffViewMode specifies the diff rendering mode.
type DiffViewMode int

const (
	DiffViewUnified    DiffViewMode = iota // Line-by-line unified view
	DiffViewSideBySide                     // Side-by-side split view
)

// WorktreeStatus represents the current state of a worktree.
type WorktreeStatus int

const (
	StatusPaused  WorktreeStatus = iota // No agent, worktree exists
	StatusActive                        // Agent running, recent output
	StatusWaiting                       // Agent waiting for input
	StatusDone                          // Agent completed task
	StatusError                         // Agent crashed or errored
)

// String returns the display string for a WorktreeStatus.
func (s WorktreeStatus) String() string {
	switch s {
	case StatusPaused:
		return "paused"
	case StatusActive:
		return "active"
	case StatusWaiting:
		return "waiting"
	case StatusDone:
		return "done"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// Icon returns the status indicator icon for display.
func (s WorktreeStatus) Icon() string {
	switch s {
	case StatusPaused:
		return "⏸"
	case StatusActive:
		return "●"
	case StatusWaiting:
		return "⧗"
	case StatusDone:
		return "✓"
	case StatusError:
		return "✗"
	default:
		return "?"
	}
}

// AgentType represents the type of AI coding agent.
type AgentType string

const (
	AgentNone     AgentType = ""         // No agent (attach only)
	AgentClaude   AgentType = "claude"   // Claude Code
	AgentCodex    AgentType = "codex"    // Codex CLI
	AgentAider    AgentType = "aider"    // Aider
	AgentGemini   AgentType = "gemini"   // Gemini CLI
	AgentCursor   AgentType = "cursor"   // Cursor Agent
	AgentOpenCode AgentType = "opencode" // OpenCode
	AgentCustom   AgentType = "custom"   // Custom command
)

// SkipPermissionsFlags maps agent types to their skip-permissions CLI flags.
var SkipPermissionsFlags = map[AgentType]string{
	AgentClaude:   "--dangerously-skip-permissions",
	AgentCodex:    "--dangerously-bypass-approvals-and-sandbox",
	AgentAider:    "--yes",
	AgentGemini:   "--yolo",
	AgentCursor:   "-f",
	AgentOpenCode: "", // No known flag
}

// AgentDisplayNames provides human-readable names for agent types.
var AgentDisplayNames = map[AgentType]string{
	AgentNone:     "None (attach only)",
	AgentClaude:   "Claude Code",
	AgentCodex:    "Codex CLI",
	AgentGemini:   "Gemini CLI",
	AgentCursor:   "Cursor Agent",
	AgentOpenCode: "OpenCode",
}

// AgentCommands maps agent types to their CLI commands.
var AgentCommands = map[AgentType]string{
	AgentClaude:   "claude",
	AgentCodex:    "codex",
	AgentAider:    "aider", // Not in UI, but supported for backward compat
	AgentGemini:   "gemini",
	AgentCursor:   "cursor-agent",
	AgentOpenCode: "opencode",
}

// AgentTypeOrder defines the order of agents in selection UI.
var AgentTypeOrder = []AgentType{
	AgentClaude,
	AgentCodex,
	AgentGemini,
	AgentCursor,
	AgentOpenCode,
	AgentNone,
}

// kanbanCardData stores column and row for Kanban card hit regions.
type kanbanCardData struct {
	col int
	row int
}

// dropdownItemData stores field ID and item index for dropdown hit regions.
type dropdownItemData struct {
	field int // 1=branch, 3=task
	idx   int // index in filtered list
}

// Worktree represents a git worktree with optional agent.
type Worktree struct {
	Name            string         // e.g., "auth-oauth-flow"
	Path            string         // Absolute path
	Branch          string         // Git branch name
	BaseBranch      string         // Branch worktree was created from
	TaskID          string         // Linked td task (e.g., "td-a1b2")
	PRURL           string         // URL of open PR (if any)
	ChosenAgentType AgentType      // Agent selected at creation (persists even when agent not running)
	Agent           *Agent         // nil if no agent running
	Status          WorktreeStatus // Derived from agent state
	Stats           *GitStats      // +/- line counts
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Agent represents an AI coding agent process.
type Agent struct {
	Type        AgentType     // claude, codex, aider, gemini
	TmuxSession string        // tmux session name
	TmuxPane    string        // Pane identifier
	PID         int           // Process ID (if available)
	StartedAt   time.Time
	LastOutput  time.Time     // Last time output was detected
	OutputBuf   *OutputBuffer // Last N lines of output
	Status      AgentStatus
	WaitingFor  string // Prompt text if waiting
}

// AgentStatus represents the current status of an agent.
type AgentStatus int

const (
	AgentStatusIdle AgentStatus = iota
	AgentStatusRunning
	AgentStatusWaiting
	AgentStatusDone
	AgentStatusError
)

// GitStats holds file change statistics.
type GitStats struct {
	Additions    int
	Deletions    int
	FilesChanged int
	Ahead        int // Commits ahead of base branch
	Behind       int // Commits behind base branch
}

// CommitStatusInfo holds commit information with merge/push status.
type CommitStatusInfo struct {
	Hash    string // Short commit hash
	Subject string // Commit subject line
	Pushed  bool   // Is commit pushed to remote?
	Merged  bool   // Is commit merged to base branch?
}

// OutputBuffer is a thread-safe bounded buffer for agent output.
// Uses SHA256 hashing to detect content changes and avoid duplicate processing.
type OutputBuffer struct {
	mu       sync.Mutex
	lines    []string
	cap      int
	lastHash [32]byte // SHA256 of last content for change detection
}

// NewOutputBuffer creates a new output buffer with the given capacity.
func NewOutputBuffer(capacity int) *OutputBuffer {
	return &OutputBuffer{
		lines: make([]string, 0, capacity),
		cap:   capacity,
	}
}

// Update replaces buffer content if it has changed (detected via SHA256 hash).
// Returns true if content was updated, false if content was unchanged.
func (b *OutputBuffer) Update(content string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Compute hash of new content
	hash := sha256.Sum256([]byte(content))
	if hash == b.lastHash {
		return false // Content unchanged
	}

	// Content changed - update hash and replace lines
	b.lastHash = hash
	b.lines = strings.Split(content, "\n")

	// Trim to capacity (keep most recent lines)
	if len(b.lines) > b.cap {
		b.lines = b.lines[len(b.lines)-b.cap:]
	}

	return true
}

// Write replaces content in the buffer (for backward compatibility).
// Prefer Update() for change detection.
func (b *OutputBuffer) Write(content string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Replace instead of append to avoid duplication
	b.lines = strings.Split(content, "\n")

	// Trim to capacity (keep most recent lines)
	if len(b.lines) > b.cap {
		b.lines = b.lines[len(b.lines)-b.cap:]
	}
}

// Lines returns a copy of all lines in the buffer.
func (b *OutputBuffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]string, len(b.lines))
	copy(result, b.lines)
	return result
}

// String returns the buffer contents as a single string.
func (b *OutputBuffer) String() string {
	return strings.Join(b.Lines(), "\n")
}

// Clear removes all lines from the buffer.
func (b *OutputBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = b.lines[:0]
	b.lastHash = [32]byte{} // Reset hash
}

// Len returns the number of lines in the buffer.
func (b *OutputBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.lines)
}
