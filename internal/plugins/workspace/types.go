package workspace

import (
	"hash/maphash"
	"regexp"
	"strings"
	"sync"
	"time"
)

// mouseEscapeRegex matches SGR mouse escape sequences like \x1b[<35;192;47M or \x1b[<0;50;20m
// These can appear in captured tmux output when applications have mouse mode enabled.
var mouseEscapeRegex = regexp.MustCompile(`\x1b\[<\d+;\d+;\d+[Mm]`)
var terminalModeRegex = regexp.MustCompile(`\x1b\[\?(?:1000|1002|1003|1005|1006|1015|2004)[hl]`)

// partialMouseEscapeRegex matches SGR mouse sequences that lost their ESC prefix (td-791865).
// This happens when the ESC byte is consumed by readline/ZLE but the rest of the sequence
// is printed as literal text in the terminal. Also handles truncated sequences missing
// the trailing M/m (e.g., "[<65;103;31" captured mid-transmission).
var partialMouseEscapeRegex = regexp.MustCompile(`\[<\d+;\d+;\d+[Mm]?`)

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
	AgentAider    AgentType = "aider"    // Aider
	AgentGemini   AgentType = "gemini"   // Gemini CLI
	AgentCursor   AgentType = "cursor"   // Cursor Agent
	AgentOpenCode AgentType = "opencode" // OpenCode
	AgentPi       AgentType = "pi"       // Pi Agent
	AgentCustom   AgentType = "custom"   // Custom command
	AgentShell    AgentType = "shell"    // Project shell (not an AI agent)
)

// SkipPermissionsFlags maps agent types to their skip-permissions CLI flags.
var SkipPermissionsFlags = map[AgentType]string{
	AgentClaude:   "--dangerously-skip-permissions",
	AgentCodex:    "--dangerously-bypass-approvals-and-sandbox",
	AgentAider:    "--yes",
	AgentGemini:   "--yolo",
	AgentCursor:   "-f",
	AgentOpenCode: "", // No known flag
	AgentPi:       "", // No known flag
}

// PlanModeFlags maps agent types to their plan mode CLI flags.
var PlanModeFlags = map[AgentType]string{
	AgentClaude: "--permission-mode plan",
}

// AgentDisplayNames provides human-readable names for agent types.
var AgentDisplayNames = map[AgentType]string{
	AgentNone:     "None (attach only)",
	AgentClaude:   "Claude Code",
	AgentCodex:    "Codex CLI",
	AgentGemini:   "Gemini CLI",
	AgentCursor:   "Cursor Agent",
	AgentOpenCode: "OpenCode",
	AgentPi:       "Pi Agent",
	AgentShell:    "Project Shell",
}

// shellAgentAbbreviations provides short labels for agent types in shell entries.
// td-a29b76: Used to show agent type in sidebar without taking too much space.
var shellAgentAbbreviations = map[AgentType]string{
	AgentClaude:   "Claude",
	AgentCodex:    "Codex",
	AgentGemini:   "Gemini",
	AgentCursor:   "Cursor",
	AgentOpenCode: "OpenCode",
	AgentPi:       "Pi",
}

// AgentCommands maps agent types to their CLI commands.
var AgentCommands = map[AgentType]string{
	AgentClaude:   "claude",
	AgentCodex:    "codex",
	AgentAider:    "aider", // Not in UI, but supported for backward compat
	AgentGemini:   "gemini",
	AgentCursor:   "cursor-agent",
	AgentOpenCode: "opencode",
	AgentPi:       "pi",
}

// AgentTypeOrder defines the order of agents in selection UI.
var AgentTypeOrder = []AgentType{
	AgentClaude,
	AgentCodex,
	AgentGemini,
	AgentCursor,
	AgentOpenCode,
	AgentPi,
	AgentNone,
}

// ShellAgentOrder defines agent order for shell creation (None first as default).
// td-a902fe: shells default to no agent, so "None" is first.
var ShellAgentOrder = []AgentType{
	AgentNone,
	AgentClaude,
	AgentCodex,
	AgentGemini,
	AgentCursor,
	AgentOpenCode,
	AgentPi,
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
	LastOutput  time.Time     // Last time output was detected
	OutputBuf   *OutputBuffer // Last N lines of output
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
	MouseReportingEnabled bool

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

// OutputBuffer is a thread-safe bounded buffer for agent output.
// Uses SHA256 hashing to detect content changes and avoid duplicate processing.
type OutputBuffer struct {
	mu          sync.Mutex
	lines       []string
	cap         int
	lastHash    uint64       // Hash of cleaned content (after mouse sequence stripping)
	lastRawHash uint64       // Hash of raw content before processing (td-15cc29)
	lastLen     int          // Length of last content (collision guard)
	hashSeed    maphash.Seed // Seed for stable hashing
}

// NewOutputBuffer creates a new output buffer with the given capacity.
func NewOutputBuffer(capacity int) *OutputBuffer {
	return &OutputBuffer{
		lines:    make([]string, 0, capacity),
		cap:      capacity,
		hashSeed: maphash.MakeSeed(),
	}
}

// Update replaces buffer content if it has changed (detected via SHA256 hash).
// Returns true if content was updated, false if content was unchanged.
func (b *OutputBuffer) Update(content string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check hash BEFORE expensive regex processing (td-15cc29)
	// Compute hash of raw content first
	rawHash := maphash.String(b.hashSeed, content)
	if rawHash == b.lastRawHash && len(content) == b.lastLen {
		return false // Content unchanged - skip ALL processing
	}

	// Content changed - now strip mouse escape sequences
	// Fast path: only run regex if mouse sequences are likely present (td-53e8a023)
	if strings.Contains(content, "\x1b[<") {
		content = mouseEscapeRegex.ReplaceAllString(content, "")
	}
	if strings.Contains(content, "\x1b[?") {
		content = terminalModeRegex.ReplaceAllString(content, "")
	}
	// Strip partial mouse sequences (ESC consumed by shell, rest printed as text) (td-791865)
	if strings.Contains(content, "[<") {
		content = partialMouseEscapeRegex.ReplaceAllString(content, "")
	}

	// Store cleaned content hash for future comparisons
	cleanHash := maphash.String(b.hashSeed, content)
	b.lastHash = cleanHash
	b.lastRawHash = rawHash
	b.lastLen = len(content)
	// Trim trailing newline before split to avoid spurious empty element.
	// tmux capture-pane output ends with \n, which would create an extra empty
	// element after split, causing cursor alignment to be off by one line.
	b.lines = strings.Split(strings.TrimSuffix(content, "\n"), "\n")

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

	// Strip mouse escape sequences.
	// Fast path: only run regex if mouse sequences are likely present (td-53e8a023)
	if strings.Contains(content, "\x1b[<") {
		content = mouseEscapeRegex.ReplaceAllString(content, "")
	}
	if strings.Contains(content, "\x1b[?") {
		content = terminalModeRegex.ReplaceAllString(content, "")
	}
	// Strip partial mouse sequences (ESC consumed by shell, rest printed as text) (td-791865)
	if strings.Contains(content, "[<") {
		content = partialMouseEscapeRegex.ReplaceAllString(content, "")
	}

	// Replace instead of append to avoid duplication
	// Trim trailing newline before split (same as Update method)
	b.lines = strings.Split(strings.TrimSuffix(content, "\n"), "\n")

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

// LinesRange returns a copy of lines in the specified range [start, end).
// This is more efficient than Lines() when only a portion is needed.
func (b *OutputBuffer) LinesRange(start, end int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if start < 0 {
		start = 0
	}
	if end > len(b.lines) {
		end = len(b.lines)
	}
	if start >= end {
		return nil
	}
	result := make([]string, end-start)
	copy(result, b.lines[start:end])
	return result
}

// LineCount returns the number of lines without copying.
func (b *OutputBuffer) LineCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.lines)
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
	b.lastHash = 0
	b.lastLen = 0
}

// Len returns the number of lines in the buffer.
func (b *OutputBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.lines)
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
