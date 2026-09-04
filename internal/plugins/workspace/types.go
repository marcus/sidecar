package workspace

import (
	"sync"
	"time"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/agentcatalog"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/workspacediff"
	"github.com/marcus/sidecar/internal/workspaceops"
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
	ViewModeConfirmCloseSplit                  // Split-terminal close confirmation modal
	ViewModeCommitForMerge                     // Commit modal before merge workflow
	ViewModeRenameShell                        // Rename shell modal
	ViewModeRenameWorktree                     // Rename worktree display-name modal
	ViewModeFilePicker                         // Diff file picker modal
	ViewModeInteractive                        // Interactive mode (tmux input passthrough)
	ViewModeFetchPR                            // Fetch remote PR modal
	ViewModeAgentConfig                        // Agent config modal (start/restart with options)
)

// FocusPane represents which pane is active in the split view.
type FocusPane int

const (
	PaneSidebar FocusPane = iota // Worktree list
	PanePreview                  // Preview pane (terminal + content leaves)
)

// Diff view types live in workspacediff so the global preview can use the
// same model without constructing a Plugin.
type (
	DiffViewMode = workspacediff.ViewMode
	DiffScope    = workspacediff.Scope
	LoadState    = workspacediff.LoadState
	DiffTabFocus = workspacediff.Focus
)

const (
	DiffViewUnified    = workspacediff.ViewUnified
	DiffViewSideBySide = workspacediff.ViewSideBySide
	DiffViewFullFile   = workspacediff.ViewFullFile

	DiffScopeWorkingTree = workspacediff.ScopeWorkingTree
	DiffScopeCommits     = workspacediff.ScopeCommits
	DiffScopeAggregate   = workspacediff.ScopeAggregate

	LoadStateUnknown   = workspacediff.LoadStateUnknown
	LoadStateLoading   = workspacediff.LoadStateLoading
	LoadStateClean     = workspacediff.LoadStateClean
	LoadStateReady     = workspacediff.LoadStateReady
	LoadStateTruncated = workspacediff.LoadStateTruncated
	LoadStateError     = workspacediff.LoadStateError

	DiffTabFocusFileList    = workspacediff.FocusFileList
	DiffTabFocusDiff        = workspacediff.FocusDiff
	DiffTabFocusCommitFiles = workspacediff.FocusCommitFiles
	DiffTabFocusCommitDiff  = workspacediff.FocusCommitDiff
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

func supportsAgentActivity(agentType AgentType) bool {
	return agentactivity.Supports(string(agentType))
}

const (
	AgentNone        AgentType = ""            // No agent (attach only)
	AgentClaude      AgentType = "claude"      // Claude Code
	AgentCodex       AgentType = "codex"       // Codex CLI
	AgentCopilot     AgentType = "copilot"     // GitHub Copilot CLI
	AgentAider       AgentType = "aider"       // Aider
	AgentAntigravity AgentType = "antigravity" // Antigravity (agy)
	AgentCursor      AgentType = "cursor"      // Cursor Agent
	AgentOpenCode    AgentType = "opencode"    // OpenCode
	AgentPi          AgentType = "pi"          // Pi Agent
	AgentAmp         AgentType = "amp"         // Amp
	AgentGrok        AgentType = "grok"        // Grok Build
	AgentMuse        AgentType = "muse"        // Muse Spark
	AgentCustom      AgentType = "custom"      // Custom command
	AgentShell       AgentType = "shell"       // Project shell (not an AI agent)
)

// SkipPermissionsFlags maps agent types to their skip-permissions CLI flags.
// Derived from workspaceops.AgentSkipFlag so this map cannot drift from the
// one the creation forms use; agents without a known flag resolve to "".
var SkipPermissionsFlags = buildSkipPermissionsFlags()

// The families are read from the catalog rather than listed, because a listed
// set answers "" for anything it forgot and "" is also the honest answer for a
// provider with no such flag. A family missing here is an auto-approve checkbox
// that is offered, ticked, and then does nothing -- the one failure this map
// must not have. Aider is added by hand for the same reason it is launchable at
// all: it is the legacy bucket, which Families() deliberately does not reach.
func buildSkipPermissionsFlags() map[AgentType]string {
	families := append(agentcatalog.Families(), agentcatalog.LegacyFamilies()...) //nolint:gocritic // one pass over both launchable buckets
	flags := make(map[AgentType]string, len(families))
	for _, family := range families {
		flags[AgentType(family.ID)] = workspaceops.AgentSkipFlag(family.ID)
	}
	return flags
}

// SystemPromptAppendFlags maps agent types to the flag that appends text to
// their system prompt for one session. Only harnesses with a documented flag
// appear here: guidance an agent never sees is worse than none, and a guessed
// flag would break the launch outright. Harnesses without one rely on the
// SIDECAR_SHELL_NAME environment cue (and project docs such as AGENTS.md).
//
// Grok: --rules appends session rules (documented alias: --append-system-prompt).
// Claude: --append-system-prompt.
var SystemPromptAppendFlags = map[AgentType]string{
	AgentClaude: "--append-system-prompt",
	AgentGrok:   "--rules",
}

// PrintModeArgs maps agent types to their non-interactive/print mode CLI arguments.
// Agents with print mode can generate output to stdout without an interactive session.
// Only agents that support true non-interactive one-shot output are included.
// Values are passed as arguments to exec.Command after the agent binary name.
var PrintModeArgs = map[AgentType][]string{
	AgentClaude:      {"-p"},        // claude -p: reads prompt from stdin, prints response to stdout
	AgentCodex:       {"exec", "-"}, // codex exec -: "-" reads prompt from stdin (convention), prints to stdout
	AgentAntigravity: {"-p"},        // agy -p: reads prompt from stdin/args, prints to stdout
}

// The selectable agent families — their order, their names, and the command
// each launches — come from internal/agentcatalog, so the creation pickers here
// and the Agents page in Configuration describe the same set. Entries below add
// only what the catalog deliberately does not carry: the non-agent pseudo-types
// and a family kept for backward compatibility.

// AgentDisplayNames provides human-readable names for agent types.
var AgentDisplayNames = buildAgentDisplayNames()

// AgentCommands maps agent types to their CLI commands.
var AgentCommands = buildAgentCommands()

// AgentTypeOrder defines the order of agents in selection UI.
var AgentTypeOrder = append(catalogAgentTypes(), AgentNone)

// ShellAgentOrder defines agent order for shell creation (None first as default).
// td-a902fe: shells default to no agent, so "None" is first.
var ShellAgentOrder = append([]AgentType{AgentNone}, catalogAgentTypes()...)

// catalogAgentTypes is the catalog's families as agent types, in picker order.
func catalogAgentTypes() []AgentType {
	families := agentcatalog.Families()
	out := make([]AgentType, 0, len(families))
	for _, family := range families {
		out = append(out, AgentType(family.ID))
	}
	return out
}

func buildAgentDisplayNames() map[AgentType]string {
	names := map[AgentType]string{
		AgentNone:  "None (attach only)",
		AgentShell: "Project Shell",
	}
	for _, family := range agentcatalog.Families() {
		names[AgentType(family.ID)] = family.Name
	}
	return names
}

func buildAgentCommands() map[AgentType]string {
	commands := map[AgentType]string{
		AgentAider: "aider", // Not in UI, but supported for backward compat
	}
	for _, family := range agentcatalog.Families() {
		commands[AgentType(family.ID)] = family.Command
	}
	return commands
}

// Worktree represents a git worktree with optional agent.
type Worktree struct {
	Key             string           // Stable normalized-path identity; never presentation
	RepoKey         string           // Stable canonical common-dir identity
	Name            string           // e.g., "auth-oauth-flow"
	Path            string           // Absolute path
	Branch          string           // Git branch name
	BaseBranch      string           // Branch worktree was created from
	TaskID          string           // Linked td task (e.g., "td-a1b2")
	TaskTitle       string           // Task title (used as fallback if td show fails)
	PRURL           string           // URL of open PR (if any)
	PRState         string           // open, merged, closed, or unavailable when known
	SetupWarning    string           // Last visible setup warning for this worktree
	ChosenAgentType AgentType        // Agent selected at creation (persists even when agent not running)
	Agent           *Agent           // nil if no agent running
	Status          WorktreeStatus   // Derived from agent state
	Stats           *GitStats        // +/- line counts
	Changes         *WorktreeChanges // One refresh result shared by badges/stats/overlap/gating
	CreatedAt       time.Time
	UpdatedAt       time.Time
	IsOrphaned      bool // True if agent file exists but tmux session is gone
	IsMain          bool // True if this is the primary/main worktree (project root)
	IsMissing       bool // True if worktree directory no longer exists (detected via os.Stat or git prunable)
	IsBare          bool // True if Git reports a bare worktree entry
	IsDetached      bool // True if HEAD is detached
	IsLocked        bool // True if Git reports the worktree as locked
	IsPrunable      bool // True if Git reports the worktree record as prunable
	HEADOID         string
	BaseOID         string
	Remote          string
	Upstream        string
}

func (w *Worktree) IdentityKey() string {
	if w == nil {
		return ""
	}
	if w.Key != "" {
		return w.Key
	}
	// Compatibility for synthetic tests and pre-inventory callers. Real Git
	// worktrees are assigned Key before entering plugin state.
	return w.Name
}

// ShellSession represents a tmux shell session (not tied to a git worktree).
type ShellSession struct {
	Name        string // Display name (e.g., "Shell 1")
	TmuxName    string // tmux session name (e.g., "sidecar-sh-project-1")
	WorkDir     string // Parent worktree path; persisted on the definition
	InventoryID string // unscoped host inventory id; empty for local shells
	Agent       *Agent // Reuses Agent struct for tmux state
	CreatedAt   time.Time
	ChosenAgent AgentType // td-317b64: Agent type selected at creation (AgentNone for plain shell)
	SkipPerms   bool      // td-317b64: Whether skip permissions was enabled
	IsOrphaned  bool      // td-f88fdd: True if manifest entry exists but tmux session is gone
}

// Agent represents an AI coding agent process.
type Agent struct {
	Type               AgentType // claude, codex, aider, gemini
	TmuxSession        string    // tmux session name
	TmuxPane           string    // Pane identifier (e.g., "%12" - globally unique)
	PID                int       // Process ID (if available)
	StartedAt          time.Time
	LastOutput         time.Time         // Last time output was detected
	OutputBuf          *tty.OutputBuffer // Last N lines of output
	WaitingFor         string            // Prompt text if waiting
	Activity           agentactivity.Tracker
	ActivityCapturedAt time.Time

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

	// LeafID is the pane-tree leaf receiving input.
	LeafID int

	// PaneOnEntry is the pane that was active before interactive mode took over.
	// leaveInteractiveMode (Esc) restores it so Enter-from-the-list still
	// returns to the list. Other exits leave the ring on the live pane.
	// Interactive mode forces the preview pane active so the embedded terminal
	// owns the cursor and mouse mode (td-62b8ab).
	PaneOnEntry FocusPane

	// LastKeyTime tracks when the last key was sent for polling decay.
	LastKeyTime time.Time

	// CursorRow and CursorCol track the cached cursor position for overlay rendering.
	// Updated asynchronously via cursorPositionMsg from poll handler (td-648af4).
	CursorRow int
	CursorCol int

	// CursorVisible indicates if the cursor should be rendered.
	// Updated asynchronously via cursorPositionMsg from poll handler (td-648af4).
	CursorVisible bool

	// PaneHeight tracks the tmux pane height for cursor offset calculation.
	// Used to adjust cursor_y when display height differs from pane height, and
	// to find pane row 0 in the buffer: a capture ends at the bottom of the
	// pane, so its last PaneHeight rows are the pane.
	PaneHeight int

	// PaneWidth tracks the tmux pane width for display width alignment.
	PaneWidth int

	// BracketedPasteEnabled tracks whether the target app has enabled
	// bracketed paste mode (ESC[?2004h). Updated from captured output.
	BracketedPasteEnabled bool

	// MouseReportingEnabled tracks whether the application in the target pane has
	// asked for mouse events. It is mirrored from the terminal component and read
	// only to describe the surface: who owns a click or a notch is that
	// component's PaneMouseReporting, so the two can never disagree.
	MouseReportingEnabled bool

	// LastResizeAt tracks the last time we attempted to resize the tmux pane.
	LastResizeAt time.Time

	// ResizeRetryPending records that a deferred assertion of the pane's
	// geometry is already armed. A layout still moving delivers one size per
	// frame; without this each of them would arm its own retry and the burst
	// would become a chain of resizes spaced a debounce window apart.
	ResizeRetryPending bool
}

// GitStats holds file change statistics.
type GitStats struct {
	Additions    int
	Deletions    int
	FilesChanged int
	Ahead        int // Commits ahead of base branch
	Behind       int // Commits behind base branch
}

// WorktreeChanges is the bounded, immutable status result for one refresh.
// Paths are repository-relative and originate from a single porcelain status
// invocation. Stats and dirty overlap are derived from this same value.
type WorktreeChanges struct {
	State          LoadState
	Staged         []string
	Unstaged       []string
	Untracked      []string
	Dirty          []string
	Truncated      bool
	TruncatedFiles int
	TruncatedBytes int64
	Err            error
}

// CommitStatusInfo holds commit information with merge/push status.
type CommitStatusInfo = workspacediff.CommitInfo

// validateManagedSessionsMsg triggers periodic validation of managedSessions.
type validateManagedSessionsMsg struct{ Generation uint64 }

// validateManagedSessionsResultMsg delivers validation results.
type validateManagedSessionsResultMsg struct {
	Generation       uint64
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
