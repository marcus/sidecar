package workspace

// RefreshMsg triggers a worktree list refresh.
type RefreshMsg struct{}

// RefreshDoneMsg signals that refresh has completed.
type RefreshDoneMsg struct {
	Worktrees []*Worktree
	Err       error
}

// WatchEventMsg signals a filesystem change was detected.
type WatchEventMsg struct {
	Path string
}

// WatcherStartedMsg signals the file watcher is running.
type WatcherStartedMsg struct{}

// WatcherErrorMsg signals a file watcher error.
type WatcherErrorMsg struct {
	Err error
}

// AgentOutputMsg delivers new agent output.
type AgentOutputMsg struct {
	WorkspaceName string
	Output       string
	Status       WorktreeStatus
	WaitingFor   string
	// Cursor position captured atomically with output (only set in interactive mode)
	CursorRow     int
	CursorCol     int
	CursorVisible bool
	HasCursor     bool // True if cursor position was captured
	PaneHeight    int  // Tmux pane height for cursor offset calculation
	PaneWidth     int  // Tmux pane width for display alignment
}

// AgentStoppedMsg signals an agent has stopped.
type AgentStoppedMsg struct {
	WorkspaceName string
	Err          error
}

// TmuxAttachFinishedMsg signals return from tmux attach.
type TmuxAttachFinishedMsg struct {
	WorkspaceName string
	Err          error
}

// DiffLoadedMsg delivers diff content for a worktree.
type DiffLoadedMsg struct {
	WorkspaceName string
	Content      string
	Raw          string
}

// DiffErrorMsg signals diff loading failed.
type DiffErrorMsg struct {
	WorkspaceName string
	Err          error
}

// StatsLoadedMsg delivers git stats for a worktree.
type StatsLoadedMsg struct {
	WorkspaceName string
	Stats        *GitStats
}

// StatsErrorMsg signals stats loading failed.
type StatsErrorMsg struct {
	WorkspaceName string
	Err          error
}

// CreateWorktreeMsg requests worktree creation.
type CreateWorktreeMsg struct {
	Name       string
	BaseBranch string
	TaskID     string
}

// CreateDoneMsg signals worktree creation completed.
type CreateDoneMsg struct {
	Worktree  *Worktree
	AgentType AgentType // Agent selected at creation
	SkipPerms bool      // Whether to skip permissions
	Prompt    *Prompt   // Selected prompt template (nil if none)
	Err       error
}

// DeleteWorktreeMsg requests worktree deletion.
type DeleteWorktreeMsg struct {
	Name  string
	Force bool
}

// DeleteDoneMsg signals worktree deletion completed.
type DeleteDoneMsg struct {
	Name     string
	Err      error
	Warnings []string // Non-fatal warnings (e.g., branch deletion failures)
}

// RemoteCheckDoneMsg signals remote branch existence check completed.
type RemoteCheckDoneMsg struct {
	WorkspaceName string
	Branch       string
	Exists       bool
}

// PushMsg requests pushing a worktree branch.
type PushMsg struct {
	WorkspaceName string
	Force        bool
	SetUpstream  bool
}

// PushDoneMsg signals push operation completed.
type PushDoneMsg struct {
	WorkspaceName string
	Err          error
}

// TaskSearchResultsMsg delivers task search results.
type TaskSearchResultsMsg struct {
	Tasks []Task
	Err   error
}

// BranchListMsg delivers available branches.
type BranchListMsg struct {
	Branches []string
	Err      error
}

// TaskLinkedMsg signals a task was linked to a worktree.
type TaskLinkedMsg struct {
	WorkspaceName string
	TaskID       string
	Err          error
}

// Task represents a TD task for linking.
type Task struct {
	ID          string
	Title       string
	Status      string
	Description string
	EpicTitle   string // Parent epic title for search
}

// TaskDetails contains full task information for preview pane.
type TaskDetails struct {
	ID          string
	Title       string
	Status      string
	Priority    string
	Type        string
	Description string
	Acceptance  string
	CreatedAt   string
	UpdatedAt   string
}

// TaskDetailsLoadedMsg delivers task details for the preview pane.
type TaskDetailsLoadedMsg struct {
	TaskID  string
	Details *TaskDetails
	Err     error
}

// restartAgentMsg signals that an agent should be restarted after stopping.
type restartAgentMsg struct {
	worktree *Worktree
}

// CommitStatusLoadedMsg delivers commit status info for the diff view header.
type CommitStatusLoadedMsg struct {
	WorkspaceName string
	Commits      []CommitStatusInfo
	Err          error
}

// OpenCreateModalWithTaskMsg opens create modal pre-filled with task data.
// Sent from td-monitor plugin when user presses send-to-worktree hotkey.
type OpenCreateModalWithTaskMsg struct {
	TaskID    string
	TaskTitle string
}

// ResumeConversationMsg requests resuming a conversation in a new shell or worktree.
// Sent from conversations plugin when user presses O key.
type ResumeConversationMsg struct {
	SessionID string // Adapter session ID for resume command
	AdapterID string // Adapter type (claude-code, codex, etc.)
	ResumeCmd string // Full resume command (e.g., "claude --resume xyz")
	Type      string // "shell" or "worktree"
	// Worktree-specific fields (only used when Type == "worktree")
	WorktreeName string    // Branch name for new worktree
	BaseBranch   string    // Base branch to create from
	AgentType    AgentType // Agent to start (matches adapter or user selection)
	SkipPerms    bool      // Whether to auto-approve agent actions
}

// cursorPositionMsg delivers async cursor position updates for interactive mode (td-648af4).
// Queried during poll handler when output changes, not during View() rendering.
type cursorPositionMsg struct {
	Row     int  // 0-indexed row in visible pane
	Col     int  // 0-indexed column
	Visible bool // Whether cursor should be rendered
}

// paneResizedMsg signals that a tmux pane was resized to match preview dimensions.
// Triggers a fresh poll so captured content reflects the new width/wrapping.
type paneResizedMsg struct{}

// InteractivePasteResultMsg reports clipboard paste results for interactive mode.
type InteractivePasteResultMsg struct {
	Err         error
	Empty       bool
	SessionDead bool
}
