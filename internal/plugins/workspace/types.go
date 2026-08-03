package workspace

import (
	"sync"
	"time"

	"github.com/marcus/sidecar/internal/tty"
)

// ViewMode represents the current view state.
type ViewMode int

const (
	ViewModeList               ViewMode = iota // List view (default)
	ViewModeKanban                             // Kanban board view
	ViewModeCreate                             // New worktree modal
	ViewModeTaskLink                           // Task link modal (for existing worktrees)
	ViewModeMerge                              // Merge workflow modal
	ViewModeAgentChoice                        // Agent action choice modal (attach/restart)
	ViewModeConfirmDelete                      // Delete confirmation modal
	ViewModeConfirmDeleteShell                 // Shell delete confirmation modal
	ViewModeCommitForMerge                     // Commit modal before merge workflow
	ViewModePromptPicker                       // Prompt template picker modal
	ViewModeTypeSelector                       // Type selector modal (shell vs worktree)
	ViewModeRenameShell                        // Rename shell modal
	ViewModeFilePicker                         // Diff file picker modal
	ViewModeInteractive                        // Interactive mode (tmux input passthrough)
	ViewModeFetchPR                            // Fetch remote PR modal
	ViewModeAgentConfig                        // Agent config modal (start/restart with options)
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
	DiffViewFullFile                       // Full-file side-by-side view (like VS Code diff)
)

// DiffTabFocus represents which sub-pane is focused within the diff tab.
type DiffTabFocus int

const (
	DiffTabFocusFileList    DiffTabFocus = iota // File list navigation (files + commits)
	DiffTabFocusDiff                            // Per-file diff viewing
	DiffTabFocusCommitFiles                     // Commit file list (drilled into a commit)
	DiffTabFocusCommitDiff                      // Commit file diff viewing
)

// TermPanelLayout represents the terminal panel split orientation.
type TermPanelLayout int

const (
	TermPanelBottom TermPanelLayout = iota // Terminal below output (horizontal divider)
	TermPanelRight                         // Terminal to the right of output (vertical divider)
)

// WorktreeStatus represents the current state of a worktree.
type WorktreeStatus int

const (
	StatusPaused   WorktreeStatus = iota // No agent, worktree exists
	StatusActive                         // Agent running, recent output
	StatusThinking                       // Agent is processing/thinking
	StatusWaiting                        // Agent waiting for input
	StatusDone                           // Agent completed task
	StatusError                          // Agent crashed or errored
)

// String returns the display string for a WorktreeStatus.
func (s WorktreeStatus) String() string {
	switch s {
	case StatusPaused:
		return "paused"
	case StatusActive:
		return "active"
	case StatusThinking:
		return "thinking"
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
	case StatusThinking:
		return "◐"
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
	AgentCopilot  AgentType = "copilot"  // GitHub Copilot CLI
	AgentAider    AgentType = "aider"    // Aider
	AgentGemini   AgentType = "gemini"   // Gemini CLI
	AgentCursor   AgentType = "cursor"   // Cursor Agent
	AgentOpenCode AgentType = "opencode" // OpenCode
	AgentGoose    AgentType = "goose"    // Goose CLI
	AgentPi       AgentType = "pi"       // Pi Agent
	AgentAmp      AgentType = "amp"      // Amp
	AgentCustom   AgentType = "custom"   // Custom command
	AgentShell    AgentType = "shell"    // Project shell (not an AI agent)
)

// SkipPermissionsFlags maps agent types to their skip-permissions CLI flags.
var SkipPermissionsFlags = map[AgentType]string{
	AgentClaude:   "--dangerously-skip-permissions",
	AgentCodex:    "--dangerously-bypass-approvals-and-sandbox",
	AgentCopilot:  "", // No known flag
	AgentAider:    "--yes",
	AgentGemini:   "--yolo",
	AgentCursor:   "-f",
	AgentOpenCode: "", // No known flag
	AgentGoose:    "", // No known flag
	AgentPi:       "", // No known flag
	AgentAmp:      "--dangerously-allow-all",
}

// PrintModeArgs maps agent types to their non-interactive/print mode CLI arguments.
// Agents with print mode can generate output to stdout without an interactive session.
// Only agents that support true non-interactive one-shot output are included.
// Values are passed as arguments to exec.Command after the agent binary name.
var PrintModeArgs = map[AgentType][]string{
	AgentClaude: {"-p"},        // claude -p: reads prompt from stdin, prints response to stdout
	AgentCodex:  {"exec", "-"}, // codex exec -: "-" reads prompt from stdin (convention), prints to stdout
}

// AgentDisplayNames provides human-readable names for agent types.
var AgentDisplayNames = map[AgentType]string{
	AgentNone:     "None (attach only)",
	AgentClaude:   "Claude Code",
	AgentCodex:    "Codex CLI",
	AgentCopilot:  "GitHub Copilot CLI",
	AgentGemini:   "Gemini CLI",
	AgentCursor:   "Cursor Agent",
	AgentOpenCode: "OpenCode",
	AgentGoose:    "Goose CLI",
	AgentPi:       "Pi Agent",
	AgentAmp:      "Amp",
	AgentShell:    "Project Shell",
}

// shellAgentAbbreviations provides short labels for agent types in shell entries.
// td-a29b76: Used to show agent type in sidebar without taking too much space.
var shellAgentAbbreviations = map[AgentType]string{
	AgentClaude:   "Claude",
	AgentCodex:    "Codex",
	AgentCopilot:  "Copilot",
	AgentGemini:   "Gemini",
	AgentCursor:   "Cursor",
	AgentOpenCode: "OpenCode",
	AgentGoose:    "Goose",
	AgentPi:       "Pi",
	AgentAmp:      "Amp",
}

// AgentCommands maps agent types to their CLI commands.
var AgentCommands = map[AgentType]string{
	AgentClaude:   "claude",
	AgentCodex:    "codex",
	AgentCopilot:  "copilot",
	AgentAider:    "aider", // Not in UI, but supported for backward compat
	AgentGemini:   "gemini",
	AgentCursor:   "cursor-agent",
	AgentOpenCode: "opencode",
	AgentGoose:    "goose",
	AgentPi:       "pi",
	AgentAmp:      "amp",
}

// AgentTypeOrder defines the order of agents in selection UI.
var AgentTypeOrder = []AgentType{
	AgentClaude,
	AgentCodex,
	AgentCopilot,
	AgentGemini,
	AgentCursor,
	AgentOpenCode,
	AgentGoose,
	AgentPi,
	AgentAmp,
	AgentNone,
}

// ShellAgentOrder defines agent order for shell creation (None first as default).
// td-a902fe: shells default to no agent, so "None" is first.
var ShellAgentOrder = []AgentType{
	AgentNone,
	AgentClaude,
	AgentCodex,
	AgentCopilot,
	AgentGemini,
	AgentCursor,
	AgentOpenCode,
	AgentGoose,
	AgentPi,
	AgentAmp,
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
	TaskTitle       string         // Task title (used as fallback if td show fails)
	PRURL           string         // URL of open PR (if any)
	ChosenAgentType AgentType      // Agent selected at creation (persists even when agent not running)
	Agent           *Agent         // nil if no agent running
	Status          WorktreeStatus // Derived from agent state
	Stats           *GitStats      // +/- line counts
	CreatedAt       time.Time
	UpdatedAt       time.Time
	IsOrphaned      bool // True if agent file exists but tmux session is gone
	IsMain          bool // True if this is the primary/main worktree (project root)
	IsMissing       bool // True if worktree directory no longer exists (detected via os.Stat or git prunable)
}

// ShellSession represents a tmux shell session (not tied to a git worktree).
type ShellSession struct {
	Name        string // Display name (e.g., "Shell 1")
	TmuxName    string // tmux session name (e.g., "sidecar-sh-project-1")
	Agent       *Agent // Reuses Agent struct for tmux state
	CreatedAt   time.Time
	ChosenAgent AgentType // td-317b64: Agent type selected at creation (AgentNone for plain shell)
	SkipPerms   bool      // td-317b64: Whether skip permissions was enabled
	IsOrphaned  bool      // td-f88fdd: True if manifest entry exists but tmux session is gone
}

// Agent represents an AI coding agent process.
type Agent struct {
	Type        AgentType // claude, codex, aider, gemini
	TmuxSession string    // tmux session name
	TmuxPane    string    // Pane identifier (e.g., "%12" - globally unique)
	PID         int       // Process ID (if available)
	StartedAt   time.Time
	LastOutput  time.Time         // Last time output was detected
	OutputBuf   *tty.OutputBuffer // Last N lines of output
	Status      AgentStatus
	WaitingFor  string // Prompt text if waiting

	// Runaway detection fields (td-018f25)
	// Track recent poll times to detect continuous output that would cause CPU spikes.
	RecentPollTimes    []time.Time // Last N poll times for runaway detection
	PollsThrottled     bool        // True if this agent is throttled due to continuous output
	UnchangedPollCount int         // Consecutive unchanged polls (for throttle reset)
}

// InteractiveState tracks state for interactive mode (tmux input passthrough).
// Feature-gated behind tmux_interactive_input feature flag.
type InteractiveState struct {
	// Active indicates whether interactive mode is currently active.
	Active bool

	// TargetPane is the tmux pane ID (e.g., "%12") receiving input.
	TargetPane string

	// TargetSession is the tmux session name for the active pane.
	TargetSession string

	// TermPanel is true when interactive mode targets the terminal panel session
	// (rather than the main agent/shell session).
	TermPanel bool

	// PaneOnEntry is the pane that was active before interactive mode took over,
	// restored on exit. Interactive mode forces the preview pane active so the
	// embedded terminal owns the cursor and mouse mode (td-62b8ab).
	PaneOnEntry FocusPane

	// LastKeyTime tracks when the last key was sent for polling decay.
	LastKeyTime time.Time

	// EscapePressed tracks if a single Escape was recently pressed
	// (for double-escape exit detection with 150ms delay).
	EscapePressed bool

	// EscapeTime is when the first Escape was pressed.
	EscapeTime time.Time

	// CursorRow and CursorCol track the cached cursor position for overlay rendering.
	// Updated asynchronously via cursorPositionMsg from poll handler (td-648af4).
	CursorRow int
	CursorCol int

	// CursorVisible indicates if the cursor should be rendered.
	// Updated asynchronously via cursorPositionMsg from poll handler (td-648af4).
	CursorVisible bool

	// PaneHeight tracks the tmux pane height for cursor offset calculation.
	// Used to adjust cursor_y when display height differs from pane height.
	PaneHeight int

	// PaneWidth tracks the tmux pane width for display width alignment.
	PaneWidth int

	// CursorHistorySize is the tmux history_size captured atomically with the
	// cursor. In tmux's absolute line space the scrollback occupies
	// [0, history_size) and pane row j is history_size+j, so this converts the
	// pane-relative CursorRow into a buffer coordinate. Without it the rendered
	// cursor floats above the live row by however much scrollback the capture
	// included (td-d29821).
	CursorHistorySize int

	// HasCursorHistory reports whether CursorHistorySize is meaningful. Captures
	// that predate the metadata, or that failed to parse it, fall back to the
	// pane-relative placement.
	HasCursorHistory bool

	// VisibleStart and VisibleEnd track the buffer line range currently visible.
	// Used for interactive selection mapping.
	VisibleStart int
	VisibleEnd   int

	// ContentRowOffset is the number of preview content rows before output lines.
	// Used to map mouse coordinates to buffer lines.
	ContentRowOffset int

	// BracketedPasteEnabled tracks whether the target app has enabled
	// bracketed paste mode (ESC[?2004h). Updated from captured output.
	BracketedPasteEnabled bool

	// MouseReportingEnabled tracks whether the target app has enabled
	// mouse reporting (1000/1002/1003/1006/1015). Updated from captured output.
	//
	// Note: captures come from `capture-pane -e`, which emits rendering escapes
	// only, so DECSET mode sequences never appear in them and this stays false in
	// practice. Click handling still reads it, deliberately: flipping clicks from
	// local selection to app forwarding is a separate behaviour change. Wheel
	// routing uses PaneMouseReporting instead.
	MouseReportingEnabled bool

	// PaneMouseReporting is tmux's #{mouse_any_flag} for the target pane: the
	// app has enabled at least one mouse tracking mode and expects wheel notches
	// as mouse reports rather than having the viewer scroll its own scrollback.
	PaneMouseReporting bool

	// EscapeTimerPending tracks if an escape timer is already in flight.
	// Prevents duplicate timers from accumulating (td-83dc22).
	EscapeTimerPending bool

	// LastResizeAt tracks the last time we attempted to resize the tmux pane.
	LastResizeAt time.Time
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

// validateManagedSessionsMsg triggers periodic validation of managedSessions.
type validateManagedSessionsMsg struct{}

// validateManagedSessionsResultMsg delivers validation results.
type validateManagedSessionsResultMsg struct {
	ExistingSessions map[string]bool // Set of actually existing tmux sessions
}

// AsyncCaptureResultMsg delivers async tmux capture results.
// Used to avoid blocking the UI thread on tmux subprocess calls (td-c2961e).
type AsyncCaptureResultMsg struct {
	WorkspaceName string // Worktree this capture is for
	SessionName   string // tmux session name
	Output        string // Captured output (empty on error)
	Err           error  // Non-nil if capture failed
}

// AsyncShellCaptureResultMsg delivers async shell capture results.
type AsyncShellCaptureResultMsg struct {
	TmuxName string // Shell session tmux name
	Output   string // Captured output (empty on error)
	Err      error  // Non-nil if capture failed
}

// paneIDCache provides thread-safe caching of pane IDs.
// Pane IDs rarely change so we cache them to avoid subprocess calls.
type paneIDCache struct {
	mu      sync.RWMutex
	entries map[string]paneIDCacheEntry
}

type paneIDCacheEntry struct {
	paneID   string
	cachedAt time.Time
}

// paneIDCacheTTL is how long pane IDs are cached (they rarely change).
const paneIDCacheTTL = 5 * time.Minute

// globalPaneIDCache caches pane IDs to avoid subprocess calls.
var globalPaneIDCache = &paneIDCache{
	entries: make(map[string]paneIDCacheEntry),
}

// get returns cached pane ID if valid.
func (c *paneIDCache) get(sessionName string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, ok := c.entries[sessionName]; ok {
		if time.Since(entry.cachedAt) < paneIDCacheTTL {
			return entry.paneID, true
		}
	}
	return "", false
}

// set stores a pane ID in the cache.
func (c *paneIDCache) set(sessionName, paneID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[sessionName] = paneIDCacheEntry{
		paneID:   paneID,
		cachedAt: time.Now(),
	}
}
