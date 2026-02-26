package workspace

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
	"github.com/marcus/sidecar/internal/state"
)

const (
	pluginID   = "workspace-manager"
	pluginName = "workspaces"
	pluginIcon = "W"

	// Output buffer capacity (lines)
	outputBufferCap = 500

	// Pane layout constants
	dividerWidth    = 1 // Visual divider width
	dividerHitWidth = 3 // Wider hit target for drag

	// Flash effect duration for invalid key interaction
	flashDuration = 1500 * time.Millisecond

	// Hit region IDs
	regionSidebar            = "sidebar"
	regionPreviewPane        = "preview-pane"
	regionPaneDivider        = "pane-divider"
	regionWorktreeItem       = "workspace-item"
	regionPreviewTab         = "preview-tab"
	// Agent choice modal IDs (modal library)
	agentChoiceListID    = "agent-choice-list"
	agentChoiceConfirmID = "agent-choice-confirm"
	agentChoiceCancelID  = "agent-choice-cancel"
	agentChoiceActionID  = "agent-choice-action"

	// Kanban view regions
	regionKanbanCard   = "kanban-card"
	regionKanbanColumn = "kanban-column"
	regionViewToggle   = "view-toggle"

	// Create modal regions
	regionCreateBackdrop    = "create-backdrop"
	regionCreateModalBody   = "create-modal-body"
	regionCreateInput       = "create-input"
	regionCreateDropdown    = "create-dropdown"
	regionCreateButton      = "create-button"
	regionCreateCheckbox    = "create-checkbox"
	regionCreateAgentOption = "create-agent-option"

	// Task Link modal regions
	regionTaskLinkDropdown = "task-link-dropdown"

	// Merge modal element IDs
	mergeMethodListID      = "merge-method-list"
	mergeMethodActionID    = "merge-method-action"
	mergeWaitingDeleteID   = "merge-waiting-delete"
	mergeWaitingKeepID     = "merge-waiting-keep"
	mergeConfirmWorktreeID = "merge-confirm-worktree"
	mergeConfirmBranchID   = "merge-confirm-branch"
	mergeConfirmRemoteID   = "merge-confirm-remote"
	mergeConfirmPullID     = "merge-confirm-pull"
	mergeTargetListID      = "merge-target-list"
	mergeTargetActionID    = "merge-target-action"
	mergeCleanUpButtonID   = "merge-cleanup-btn"
	mergeSkipButtonID      = "merge-skip-btn"

	// Prompt Picker modal regions
	regionPromptItem   = "prompt-item"
	regionPromptFilter = "prompt-filter"

	// Sidebar header regions
	regionCreateWorktreeButton = "create-worktree-button"
	regionShellsPlusButton     = "shells-plus-button"
	regionWorkspacesPlusButton = "workspaces-plus-button"

	// Type selector modal element IDs
	typeSelectorListID       = "type-selector-list"
	typeSelectorInputID      = "type-selector-name-input"
	typeSelectorConfirmID    = "type-selector-confirm"
	typeSelectorCancelID     = "type-selector-cancel"
	typeSelectorAgentListID  = "type-selector-agent-list"  // td-a902fe
	typeSelectorSkipPermsID  = "type-selector-skip-perms"  // td-a902fe
	typeSelectorAgentItemPfx = "ts-agent-"                 // td-a902fe: prefix for agent items

	// Shell delete confirmation modal regions
)

// Plugin implements the worktree manager plugin.
type Plugin struct {
	// Required by plugin.Plugin interface
	ctx     *plugin.Context
	focused bool
	width   int
	height  int

	// Worktree state
	worktrees []*Worktree
	agents    map[string]*Agent

	// Session tracking for safe cleanup
	managedSessions map[string]bool

	// View state
	viewMode         ViewMode
	activePane       FocusPane
	previewTab       PreviewTab
	selectedIdx      int
	scrollOffset     int // Sidebar list scroll offset
	visibleCount     int // Number of visible list items
	previewOffset       int
	autoScrollOutput    bool // Auto-scroll output to follow agent (paused when user scrolls up)
	scrollBaseLineCount int  // Snapshot of lineCount when scroll started (td-f7c8be: prevents bounce on poll)
	sidebarWidth     int       // Persisted sidebar width
	sidebarVisible   bool      // Whether sidebar is visible (toggled with \)
	flashPreviewTime time.Time // When preview flash was triggered
	toastMessage     string    // Temporary toast message to display
	toastTime        time.Time // When toast was triggered

	// Interactive selection state (preview pane)
	selection                     ui.SelectionState
	interactiveCopyPasteHintShown bool

	// Kanban view state
	kanbanCol int // Current column index (0=Shells, 1=Active, 2=Thinking, 3=Waiting, 4=Done, 5=Paused)
	kanbanRow int // Current row within the column

	// Agent state
	attachedSession     string // Name of worktree we're attached to (pauses polling)
	tmuxCaptureMaxBytes int    // Cap for tmux capture output (bytes)

	// Timer leak prevention (td-83dc22): generation counters to invalidate stale timers.
	// When a timer fires, it checks if its captured generation matches the current one.
	// If not, the timer is stale (worktree/shell was removed) and the msg is ignored.
	pollGeneration      map[string]int // Per-worktree/shell poll generation counter
	shellPollGeneration map[string]int // Per-shell poll generation counter

	// Truncation cache to eliminate ANSI parser allocation churn
	truncateCache *ui.TruncateCache

	// Mouse support
	mouseHandler *mouse.Handler

	// Async state
	refreshing  bool
	lastRefresh time.Time

	// Diff state
	diffContent   string
	diffRaw       string
	diffViewMode  DiffViewMode             // Unified or side-by-side
	multiFileDiff *gitstatus.MultiFileDiff // Parsed multi-file diff with positions

	// File picker modal state (gf command)
	filePickerIdx int // Selected file index in picker

	// Commit status header for diff view
	commitStatusList     []CommitStatusInfo
	commitStatusWorktree string // Name of worktree for cached status

	// Conflict detection state
	conflicts []Conflict

	// Create modal state
	createNameInput       textinput.Model
	createBaseBranchInput textinput.Model
	createTaskID          string
	createTaskTitle       string    // Title of selected task for display
	createAgentType       AgentType // Selected agent type (default: AgentClaude)
	createAgentIdx        int       // Selected agent index in AgentTypeOrder
	createSkipPermissions bool      // Skip permissions checkbox
	createFocus           int       // 0=name, 1=base, 2=prompt, 3=task, 4=agent, 5=skipPerms, 6=create, 7=cancel
	createButtonHover     int       // 0=none, 1=create, 2=cancel
	createError           string    // Error message to display in create modal
	createModal           *modal.Modal
	createModalWidth      int

	// Branch name validation state
	branchNameValid     bool     // Is current name valid?
	branchNameErrors    []string // Validation error messages
	branchNameSanitized string   // Suggested sanitized name

	// Prompt state for create modal
	createPrompts          []Prompt      // Available prompts (merged global + project)
	createPromptIdx        int           // Selected prompt index (-1 = none)
	promptPicker           *PromptPicker // Picker modal state (when open)
	promptPickerModal      *modal.Modal
	promptPickerModalWidth int
	promptPickerModalEmpty bool

	// Task search state for create modal
	taskSearchInput    textinput.Model
	taskSearchAll      []Task // All available tasks
	taskSearchFiltered []Task // Filtered based on query
	taskSearchIdx      int    // Selected index in dropdown
	taskSearchLoading  bool

	// Branch autocomplete state for create modal
	branchAll      []string // All available branches
	branchFiltered []string // Filtered based on query
	branchIdx      int      // Selected index in dropdown

	// Task link modal state (for linking to existing worktrees)
	linkingWorktree *Worktree

	// Cached task details for preview pane
	cachedTaskID      string
	cachedTask        *TaskDetails
	cachedTaskFetched time.Time
	taskLoading       bool // True when task fetch is in progress

	// Markdown rendering for task view
	markdownRenderer     *markdown.Renderer
	taskMarkdownMode     bool     // true = rendered, false = raw
	taskMarkdownRendered []string // Cached rendered lines
	taskMarkdownWidth    int      // Width used for cached render

	// Merge workflow state
	mergeState      *MergeWorkflowState
	mergeModal      *modal.Modal // Modal instance for merge workflow
	mergeModalWidth int          // Cached width for rebuild detection
	mergeModalStep  MergeWorkflowStep // Cached step for rebuild detection

	// Commit-before-merge state
	mergeCommitState        *MergeCommitState
	mergeCommitMessageInput textinput.Model
	commitForMergeModal     *modal.Modal // Modal instance
	commitForMergeModalWidth int         // Cached width for rebuild detection

	// Agent choice modal state (attach vs restart)
	agentChoiceWorktree    *Worktree
	agentChoiceIdx         int          // 0=attach, 1=restart
	agentChoiceModal       *modal.Modal // Modal instance
	agentChoiceModalWidth  int          // Cached width for rebuild detection

	// Delete confirmation modal state
	deleteConfirmWorktree   *Worktree // Worktree pending deletion
	deleteLocalBranchOpt    bool      // Checkbox: delete local branch
	deleteRemoteBranchOpt   bool      // Checkbox: delete remote branch
	deleteHasRemote         bool      // Whether remote branch exists
	deleteIsMainBranch      bool      // Whether the worktree branch is the main branch (protected)
	deleteConfirmModal      *modal.Modal
	deleteConfirmModalWidth int
	deleteWarnings          []string // Warnings from last delete operation (e.g., branch deletion failures)

	// Shell delete confirmation modal state
	deleteConfirmShell    *ShellSession // Shell pending deletion
	deleteShellModal      *modal.Modal
	deleteShellModalWidth int

	// Rename shell modal state
	renameShellSession    *ShellSession   // Shell being renamed
	renameShellInput      textinput.Model // Text input for new name
	renameShellModal      *modal.Modal    // Modal instance
	renameShellModalWidth int             // Cached width for rebuild detection
	renameShellError      string          // Validation error message

	// Initial reconnection tracking
	initialReconnectDone bool

	// State restoration tracking (only restore once on startup)
	stateRestored bool

	// Interactive mode state (feature-gated behind tmux_interactive_input)
	interactiveState   *InteractiveState
	lastScrollTime     time.Time // For scroll debouncing (td-e2ce50)
	lastMouseEventTime time.Time // For suppressing split-CSI "[" near mouse activity
	scrollBurstCount   int       // Consecutive scroll events for burst detection
	scrollBurstStarted time.Time // When current burst started

	// Sidebar header hover state
	hoverNewButton            bool
	hoverShellsPlusButton     bool
	hoverWorkspacesPlusButton bool

	// Multiple shell sessions (not tied to git worktrees)
	shells           []*ShellSession // All shell sessions for this project
	selectedShellIdx int             // Currently selected shell index
	shellSelected    bool            // True when any shell is selected (vs a worktree)

	// Type selector modal state (shell vs worktree)
	typeSelectorIdx         int             // 0=Shell, 1=Worktree
	typeSelectorNameInput   textinput.Model // Optional shell name input
	typeSelectorModal       *modal.Modal    // Modal instance
	typeSelectorModalWidth  int             // Cached width for rebuild detection

	// Type selector modal - shell agent selection (td-2bb232)
	typeSelectorAgentIdx   int       // Selected index in agent list (0 = None)
	typeSelectorAgentType  AgentType // The selected agent type
	typeSelectorSkipPerms  bool      // Whether skip permissions is checked
	typeSelectorFocusField int       // Focus: 0=name, 1=agent, 2=skipPerms, 3=buttons

// Resume conversation state (td-aa4136)
	pendingResumeCmd      string // Resume command to inject after shell creation
	pendingResumeWorktree string // Worktree name to enter interactive mode after agent starts

	// Fetch PR modal state
	fetchPRItems        []PRListItem // PRs from gh pr list
	fetchPRFilter       string       // Filter text
	fetchPRCursor       int          // Selected index in filtered list
	fetchPRScrollOffset int          // Scroll offset for PR list
	fetchPRLoading      bool         // True while gh pr list is running
	fetchPRError        string       // Error message from gh CLI
	fetchPRModal        *modal.Modal // Modal instance
	fetchPRModalWidth   int          // Cached width for rebuild detection

	// Shell manifest for persistence and cross-instance sync (td-f88fdd)
	shellManifest *ShellManifest
	shellWatcher  *ShellWatcher
}

// New creates a new worktree manager plugin.
func New() *Plugin {
	// Create markdown renderer (ignore error, will fall back to plain text)
	mdRenderer, _ := markdown.NewRenderer()

	return &Plugin{
		worktrees:           make([]*Worktree, 0),
		agents:              make(map[string]*Agent),
		managedSessions:     make(map[string]bool),
		shells:              make([]*ShellSession, 0),
		pollGeneration:      make(map[string]int),
		shellPollGeneration: make(map[string]int),
		viewMode:            ViewModeList,
		activePane:          PaneSidebar,
		previewTab:          PreviewTabOutput,
		mouseHandler:        mouse.NewHandler(),
		sidebarWidth:        40,   // Default 40% sidebar
		sidebarVisible:      true, // Sidebar visible by default
		autoScrollOutput:    true, // Auto-scroll to follow agent output
		tmuxCaptureMaxBytes: defaultTmuxCaptureMaxBytes,
		truncateCache:       ui.NewTruncateCache(1000), // Cache up to 1000 truncations
		markdownRenderer:    mdRenderer,
		taskMarkdownMode:    true,  // Default to rendered mode
		shellSelected:       false, // Start with first worktree selected, not shell
		typeSelectorIdx:     1,     // Default to Worktree option
		taskLoading:         false, // Explicitly initialized (td-3668584f)
	}
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return pluginID }

// Name returns the plugin display name.
func (p *Plugin) Name() string { return pluginName }

// Icon returns the plugin icon.
func (p *Plugin) Icon() string { return pluginIcon }

// IsFocused returns whether the plugin is focused.
func (p *Plugin) IsFocused() bool { return p.focused }

// SetFocused sets the focus state.
func (p *Plugin) SetFocused(f bool) {
	// Exit interactive mode when plugin loses focus (user switched tabs) (td-efd736)
	if !f && p.viewMode == ViewModeInteractive {
		p.exitInteractiveMode()
	}
	p.focused = f
}

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	p.ctx = ctx
	if ctx.Config != nil && ctx.Config.Plugins.Workspace.TmuxCaptureMaxBytes > 0 {
		p.tmuxCaptureMaxBytes = ctx.Config.Plugins.Workspace.TmuxCaptureMaxBytes
	}

	// Reset agent-related state for clean reinit (important for project switching)
	// Without this, reconnectAgents() won't run again after switching projects
	p.initialReconnectDone = false
	p.agents = make(map[string]*Agent)
	p.managedSessions = make(map[string]bool)
	p.worktrees = make([]*Worktree, 0)
	p.attachedSession = ""

	// Reset poll generation counters (td-83dc22): invalidates any stale timers from previous project
	p.pollGeneration = make(map[string]int)
	p.shellPollGeneration = make(map[string]int)

	// Reset shell state before initializing for new project (critical for project switching)
	p.shells = make([]*ShellSession, 0)
	p.selectedShellIdx = 0
	p.shellSelected = false

	// Reset state restoration flag for project switching
	p.stateRestored = false

	// Load shell manifest for persistence (td-f88fdd)
	manifestPath := filepath.Join(ctx.WorkDir, ".sidecar", "shells.json")
	p.shellManifest, _ = LoadShellManifest(manifestPath)

	// Stop any previous watcher (important for project switching)
	if p.shellWatcher != nil {
		p.shellWatcher.Stop()
		p.shellWatcher = nil
	}

	// Discover existing shell sessions for this project
	p.initShellSessions()

	// Register dynamic keybindings for modal contexts only.
	// Main worktree-list and worktree-preview bindings are in bindings.go.
	if ctx.Keymap != nil {
		// Merge modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-merge")
		ctx.Keymap.RegisterPluginBinding("enter", "continue", "workspace-merge")
		ctx.Keymap.RegisterPluginBinding("up", "select-delete", "workspace-merge")
		ctx.Keymap.RegisterPluginBinding("down", "select-keep", "workspace-merge")

		// Create modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-create")
		ctx.Keymap.RegisterPluginBinding("enter", "confirm", "workspace-create")
		ctx.Keymap.RegisterPluginBinding("tab", "next-field", "workspace-create")
		ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-field", "workspace-create")

		// Task link modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-task-link")
		ctx.Keymap.RegisterPluginBinding("enter", "select-task", "workspace-task-link")

		// Agent choice modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-agent-choice")
		ctx.Keymap.RegisterPluginBinding("enter", "select", "workspace-agent-choice")
		ctx.Keymap.RegisterPluginBinding("j", "cursor-down", "workspace-agent-choice")
		ctx.Keymap.RegisterPluginBinding("k", "cursor-up", "workspace-agent-choice")
		ctx.Keymap.RegisterPluginBinding("down", "cursor-down", "workspace-agent-choice")
		ctx.Keymap.RegisterPluginBinding("up", "cursor-up", "workspace-agent-choice")

		// Interactive mode context - uses configured keys (td-18098d)
		ctx.Keymap.RegisterPluginBinding(p.getInteractiveExitKey(), "exit-interactive", "workspace-interactive")
		ctx.Keymap.RegisterPluginBinding(p.getInteractiveCopyKey(), "copy", "workspace-interactive")
		ctx.Keymap.RegisterPluginBinding(p.getInteractivePasteKey(), "paste", "workspace-interactive")
	}

	// Load saved sidebar width
	if savedWidth := state.GetWorkspaceSidebarWidth(); savedWidth > 0 {
		p.sidebarWidth = savedWidth
	}

	// Load saved diff view mode
	if state.GetWorkspaceDiffMode() == "side-by-side" {
		p.diffViewMode = DiffViewSideBySide
	}

	return nil
}

// Start begins async operations.
func (p *Plugin) Start() tea.Cmd {
	var cmds []tea.Cmd

	// Refresh worktrees - reconnectAgents will be called after worktrees are loaded
	cmds = append(cmds, p.refreshWorktrees())

	// Start shell polling for all existing shells (so preview shows content right away)
	for _, shell := range p.shells {
		if shell.Agent != nil {
			cmds = append(cmds, p.pollShellSessionByName(shell.TmuxName))
		}
	}

	// Start shell manifest watcher for cross-instance sync (td-f88fdd)
	cmds = append(cmds, p.startShellWatcher())

	return tea.Batch(cmds...)
}

// startShellWatcher creates and starts the shell manifest file watcher.
func (p *Plugin) startShellWatcher() tea.Cmd {
	if p.shellManifest == nil {
		return nil
	}

	var err error
	p.shellWatcher, err = NewShellWatcher(p.shellManifest.Path())
	if err != nil {
		return nil // Watcher failed, continue without cross-instance sync
	}

	p.shellWatcher.Start()
	return p.listenForShellManifestChanges()
}

// listenForShellManifestChanges waits for manifest file changes.
func (p *Plugin) listenForShellManifestChanges() tea.Cmd {
	if p.shellWatcher == nil {
		return nil
	}
	return func() tea.Msg {
		// Block until watcher signals a change
		// Channel is closed when watcher stops
		if _, ok := <-p.shellWatcher.msgChan; !ok {
			return nil // Watcher stopped
		}
		return ShellManifestChangedMsg{}
	}
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	// Stop shell watcher (td-f88fdd)
	if p.shellWatcher != nil {
		p.shellWatcher.Stop()
		p.shellWatcher = nil
	}
}

// saveSelectionState persists the current selection to disk.
func (p *Plugin) saveSelectionState() {
	if p.ctx == nil {
		return
	}

	wtState := state.GetWorkspaceState(p.ctx.ProjectRoot)
	wtState.WorkspaceName = ""
	wtState.ShellTmuxName = ""

	if p.shellSelected {
		// Shell is selected - save shell TmuxName
		if p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
			wtState.ShellTmuxName = p.shells[p.selectedShellIdx].TmuxName
		}
	} else {
		// Worktree is selected - save worktree name
		if p.selectedIdx >= 0 && p.selectedIdx < len(p.worktrees) {
			wtState.WorkspaceName = p.worktrees[p.selectedIdx].Name
		}
	}

	// td-f88fdd: Shell display names now persisted in .sidecar/shells.json manifest
	// Only save selection state (which worktree/shell is selected)
	if wtState.WorkspaceName != "" || wtState.ShellTmuxName != "" {
		_ = state.SetWorkspaceState(p.ctx.ProjectRoot, wtState)
	}
}

// restoreSelectionState restores selection from saved state.
// Returns true if selection was restored, false if default should be used.
func (p *Plugin) restoreSelectionState() bool {
	if p.ctx == nil {
		return false
	}

	wtState := state.GetWorkspaceState(p.ctx.ProjectRoot)

	// No saved state
	if wtState.WorkspaceName == "" && wtState.ShellTmuxName == "" {
		return false
	}

	// Try to restore shell selection first (if saved)
	if wtState.ShellTmuxName != "" {
		for i, shell := range p.shells {
			if shell.TmuxName == wtState.ShellTmuxName {
				p.shellSelected = true
				p.selectedShellIdx = i
				return true
			}
		}
		// Shell no longer exists, fall through to try worktree
	}

	// Try to restore worktree selection
	if wtState.WorkspaceName != "" {
		for i, wt := range p.worktrees {
			if wt.Name == wtState.WorkspaceName {
				p.shellSelected = false
				p.selectedIdx = i
				return true
			}
		}
	}

	// Saved items no longer exist
	return false
}

// defaultShellNamePattern matches names like "Shell 1", "Shell 2", etc.
var defaultShellNamePattern = regexp.MustCompile(`^Shell \d+$`)

// isDefaultShellName returns true if the name matches the auto-generated pattern "Shell N".
func isDefaultShellName(name string) bool {
	return defaultShellNamePattern.MatchString(name)
}

// selectedWorktree returns the currently selected worktree.
// Returns nil if shell entry is selected (shell is not a worktree).
func (p *Plugin) selectedWorktree() *Worktree {
	if p.shellSelected {
		return nil
	}
	if p.selectedIdx < 0 || p.selectedIdx >= len(p.worktrees) {
		return nil
	}
	return p.worktrees[p.selectedIdx]
}

// outputVisibleFor returns true when a worktree's output is on-screen AND plugin is focused.
func (p *Plugin) outputVisibleFor(worktreeName string) bool {
	if !p.focused {
		return false
	}
	return p.outputVisibleForUnfocused(worktreeName)
}

// outputVisibleForUnfocused returns true when a worktree's output is on-screen,
// regardless of whether the plugin is focused. Used for "visible but unfocused" polling.
func (p *Plugin) outputVisibleForUnfocused(worktreeName string) bool {
	if p.viewMode != ViewModeList && p.viewMode != ViewModeInteractive {
		return false
	}
	if p.previewTab != PreviewTabOutput {
		return false
	}
	wt := p.selectedWorktree()
	if wt == nil || wt.Name != worktreeName {
		return false
	}
	return true
}

// backgroundPollInterval returns the poll delay when output isn't visible.
func (p *Plugin) backgroundPollInterval() time.Duration {
	if p.focused {
		return pollIntervalBackground
	}
	return pollIntervalUnfocused
}

// captureScrollBaseLineCount snapshots the current line count when scroll starts (td-f7c8be).
// This prevents "bounce-scroll" where polling adds content and shifts the view.
// Only captures if scrollBaseLineCount is not already set (first scroll in session).
func (p *Plugin) captureScrollBaseLineCount() {
	if p.scrollBaseLineCount > 0 {
		return // Already captured
	}

	// Get line count from currently selected worktree or shell
	var lineCount int
	if p.shellSelected {
		if shell := p.getSelectedShell(); shell != nil && shell.Agent != nil && shell.Agent.OutputBuf != nil {
			lineCount = shell.Agent.OutputBuf.LineCount()
		}
	} else {
		if wt := p.selectedWorktree(); wt != nil && wt.Agent != nil && wt.Agent.OutputBuf != nil {
			lineCount = wt.Agent.OutputBuf.LineCount()
		}
	}

	if lineCount > 0 {
		p.scrollBaseLineCount = lineCount
	}
}

// resetScrollBaseLineCount clears the captured line count (td-f7c8be).
// Called when user scrolls back to bottom (autoScrollOutput = true) or changes selection.
func (p *Plugin) resetScrollBaseLineCount() {
	p.scrollBaseLineCount = 0
}

// pollSelectedAgentNowIfVisible triggers an immediate poll for visible output.
func (p *Plugin) pollSelectedAgentNowIfVisible() tea.Cmd {
	wt := p.selectedWorktree()
	if wt == nil || wt.Agent == nil {
		return nil
	}
	if !p.outputVisibleFor(wt.Name) {
		return nil
	}
	return p.scheduleAgentPoll(wt.Name, 0)
}

// pollAllAgentStatusesNow triggers an immediate poll for every worktree that has
// an active agent. Used when entering kanban view so all statuses are fresh.
func (p *Plugin) pollAllAgentStatusesNow() tea.Cmd {
	var cmds []tea.Cmd
	for _, wt := range p.worktrees {
		if wt.Agent == nil || p.attachedSession == wt.Name {
			continue
		}
		cmds = append(cmds, p.scheduleAgentPoll(wt.Name, 0))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// removeWorktreeByName removes a worktree from the list by name.
func (p *Plugin) removeWorktreeByName(name string) {
	for i, wt := range p.worktrees {
		if wt.Name == name {
			p.worktrees = append(p.worktrees[:i], p.worktrees[i+1:]...)
			return
		}
	}
}

// clearCreateModal resets create modal state.
func (p *Plugin) clearCreateModal() {
	p.createNameInput = textinput.Model{}
	p.createBaseBranchInput = textinput.Model{}
	p.createTaskID = ""
	p.createTaskTitle = ""
	p.createAgentType = AgentClaude // Default to Claude
	p.createAgentIdx = p.agentTypeIndex(p.createAgentType)
	p.createSkipPermissions = false
	p.createFocus = 0
	p.createError = ""
	p.createModal = nil
	p.createModalWidth = 0
	p.taskSearchInput = textinput.Model{}
	p.taskSearchAll = nil
	p.taskSearchFiltered = nil
	p.taskSearchIdx = 0
	p.taskSearchLoading = false
	p.branchAll = nil
	p.branchFiltered = nil
	p.branchIdx = 0
	// Clear prompt state
	p.createPrompts = nil
	p.createPromptIdx = -1
	p.promptPicker = nil
	p.clearPromptPickerModal()
}

func (p *Plugin) clearPromptPickerModal() {
	p.promptPickerModal = nil
	p.promptPickerModalWidth = 0
	p.promptPickerModalEmpty = false
}

// initCreateModalBase initializes common create modal state.
func (p *Plugin) initCreateModalBase() {
	p.viewMode = ViewModeCreate

	// Initialize text inputs
	p.createNameInput = textinput.New()
	p.createNameInput.Placeholder = "feature-name"
	p.createNameInput.Prompt = ""
	p.createNameInput.Focus()
	p.createNameInput.CharLimit = 100

	p.createBaseBranchInput = textinput.New()
	p.createBaseBranchInput.Placeholder = "main"
	p.createBaseBranchInput.Prompt = ""
	p.createBaseBranchInput.CharLimit = 100

	p.taskSearchInput = textinput.New()
	p.taskSearchInput.Placeholder = "Search tasks..."
	p.taskSearchInput.Prompt = ""
	p.taskSearchInput.CharLimit = 100

	// Reset all state
	p.createTaskID = ""
	p.createTaskTitle = ""
	p.createAgentType = AgentClaude
	p.createAgentIdx = p.agentTypeIndex(p.createAgentType)
	p.createSkipPermissions = false
	p.createFocus = 0
	p.createError = ""
	p.createModal = nil
	p.createModalWidth = 0
	p.taskSearchAll = nil
	p.taskSearchFiltered = nil
	p.taskSearchIdx = 0
	p.taskSearchLoading = true

	// Load prompts from global and project config
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".config", "sidecar")
	p.createPrompts = LoadPrompts(configDir, p.ctx.WorkDir)
	p.createPromptIdx = -1
	p.promptPicker = nil
	p.clearPromptPickerModal()
	p.branchAll = nil
	p.branchFiltered = nil
	p.branchIdx = 0
}

// openCreateModal opens the create worktree modal and initializes all inputs.
func (p *Plugin) openCreateModal() tea.Cmd {
	p.initCreateModalBase()
	return tea.Batch(p.loadOpenTasks(), p.loadBranches())
}

// openCreateModalWithTask opens the create modal pre-filled with task data.
// Called from td-monitor plugin when user presses send-to-worktree hotkey.
func (p *Plugin) openCreateModalWithTask(taskID, taskTitle string) tea.Cmd {
	p.initCreateModalBase()

	// Pre-fill name from task
	suggestedName := p.deriveBranchName(taskID, taskTitle)
	p.createNameInput.SetValue(suggestedName)
	p.branchNameValid, p.branchNameErrors, p.branchNameSanitized = ValidateBranchName(suggestedName)

	// Pre-fill task link
	p.createTaskID = taskID
	p.createTaskTitle = taskTitle

	return tea.Batch(p.loadOpenTasks(), p.loadBranches())
}

// deriveBranchName creates a git-safe branch name from task ID and title.
// Format: "<task-id>-<sanitized-title>" e.g., "td-abc123-add-user-auth"
func (p *Plugin) deriveBranchName(taskID, title string) string {
	sanitized := SanitizeBranchName(title)
	// Truncate by runes (not bytes) to avoid corrupting multi-byte Unicode
	runes := []rune(sanitized)
	if len(runes) > 40 {
		sanitized = strings.TrimSuffix(string(runes[:40]), "-")
	}
	if sanitized == "" {
		return taskID
	}
	return taskID + "-" + sanitized
}

// getSelectedPrompt returns the currently selected prompt, or nil if none.
func (p *Plugin) getSelectedPrompt() *Prompt {
	if p.createPromptIdx < 0 || p.createPromptIdx >= len(p.createPrompts) {
		return nil
	}
	return &p.createPrompts[p.createPromptIdx]
}

// toggleSidebar toggles sidebar visibility.
func (p *Plugin) toggleSidebar() {
	p.sidebarVisible = !p.sidebarVisible
	if !p.sidebarVisible {
		p.activePane = PanePreview
	} else {
		p.activePane = PaneSidebar
	}
}

// moveCursor moves the selection cursor.
// Navigation order: shells[0], shells[1], ..., worktrees[0], worktrees[1], ...
func (p *Plugin) moveCursor(delta int) {
	oldShellSelected := p.shellSelected
	oldShellIdx := p.selectedShellIdx
	oldWorktreeIdx := p.selectedIdx

	shellCount := len(p.shells)
	worktreeCount := len(p.worktrees)

	if p.shellSelected {
		// Currently on a shell entry
		newShellIdx := p.selectedShellIdx + delta
		if newShellIdx < 0 {
			// Already at first shell, stay there
			newShellIdx = 0
		} else if newShellIdx >= shellCount {
			// Moving past last shell -> go to first worktree (if any)
			if worktreeCount > 0 {
				p.shellSelected = false
				p.selectedIdx = 0
			} else {
				// No worktrees, stay on last shell
				newShellIdx = shellCount - 1
			}
		}
		if p.shellSelected {
			p.selectedShellIdx = newShellIdx
		}
	} else if worktreeCount == 0 {
		// No worktrees exist, select shell if any
		if shellCount > 0 {
			p.shellSelected = true
			p.selectedShellIdx = 0
		}
	} else {
		// Currently on a worktree
		newIdx := p.selectedIdx + delta
		if newIdx < 0 {
			// Moving up from first worktree -> go to last shell (if any)
			if shellCount > 0 {
				p.shellSelected = true
				p.selectedShellIdx = shellCount - 1
			} else {
				// No shells, stay on first worktree
				p.selectedIdx = 0
			}
		} else if newIdx >= worktreeCount {
			// Already at last worktree, stay there
			p.selectedIdx = worktreeCount - 1
		} else {
			// Normal worktree navigation
			p.selectedIdx = newIdx
		}
	}

	// Reset preview scroll state when changing selection
	selectionChanged := p.shellSelected != oldShellSelected ||
		(p.shellSelected && p.selectedShellIdx != oldShellIdx) ||
		(!p.shellSelected && p.selectedIdx != oldWorktreeIdx)
	if selectionChanged {
		p.previewOffset = 0
		p.autoScrollOutput = true
		p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot for new selection
		p.taskLoading = false // Reset task loading state for new selection (td-3668584f)
		// Exit interactive mode when switching selection (td-fc758e88)
		p.exitInteractiveMode()
		// Persist selection to disk
		p.saveSelectionState()
	}
	p.ensureVisible()
}

// ensureVisible adjusts scroll to keep selected item visible.
// Accounts for shells (which appear before worktrees in the sidebar).
func (p *Plugin) ensureVisible() {
	// Calculate effective position in the combined list (shells + worktrees)
	var effectivePos int
	if p.shellSelected {
		effectivePos = p.selectedShellIdx
	} else {
		effectivePos = len(p.shells) + p.selectedIdx
	}

	if effectivePos < p.scrollOffset {
		p.scrollOffset = effectivePos
	}
	if p.visibleCount > 0 && effectivePos >= p.scrollOffset+p.visibleCount {
		p.scrollOffset = effectivePos - p.visibleCount + 1
	}
	// Guard against negative scroll offset (can happen with empty worktree list)
	if p.scrollOffset < 0 {
		p.scrollOffset = 0
	}
}

// cyclePreviewTab cycles through preview tabs.
func (p *Plugin) cyclePreviewTab(delta int) tea.Cmd {
	prevTab := p.previewTab
	p.previewTab = PreviewTab((int(p.previewTab) + delta + 3) % 3)
	p.previewOffset = 0
	p.autoScrollOutput = true // Reset auto-scroll when switching tabs
	p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot when switching tabs

	if prevTab == PreviewTabOutput && p.previewTab != PreviewTabOutput {
		p.selection.Clear()
	}

	// Load content for the active tab
	var cmds []tea.Cmd
	switch p.previewTab {
	case PreviewTabDiff:
		if cmd := p.loadSelectedDiff(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case PreviewTabTask:
		if cmd := p.loadTaskDetailsIfNeeded(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if cmd := p.pollSelectedAgentNowIfVisible(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if len(cmds) == 0 {
		return nil
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

// loadSelectedContent loads content based on the active preview tab.
// Always loads diff (for preloading), and pre-fetches task details for worktrees with linked tasks.
func (p *Plugin) loadSelectedContent() tea.Cmd {
	var cmds []tea.Cmd

	// Resize selected pane to match preview width so capture output is correct
	if cmd := p.resizeSelectedPaneCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// If shell is selected, poll shell output immediately
	if shell := p.getSelectedShell(); shell != nil && shell.Agent != nil && p.previewTab == PreviewTabOutput {
		cmds = append(cmds, p.pollShellSessionByName(shell.TmuxName))
	}

	// Always load diff
	if cmd := p.loadSelectedDiff(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Always pre-fetch task details if worktree has a linked task (eliminates lag when switching to task tab)
	if cmd := p.loadTaskDetailsIfNeeded(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if cmd := p.pollSelectedAgentNowIfVisible(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if len(cmds) == 0 {
		return nil
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

// loadTaskDetailsIfNeeded loads task details if not cached or stale.
// Guards against multiple simultaneous fetches (td-3668584f).
func (p *Plugin) loadTaskDetailsIfNeeded() tea.Cmd {
	wt := p.selectedWorktree()
	if wt == nil || wt.TaskID == "" {
		return nil
	}

	// Don't start a new fetch if already loading (td-3668584f)
	if p.taskLoading {
		return nil
	}

	// Check if we need to refresh (different task or cache is older than 30 seconds)
	if p.cachedTaskID != wt.TaskID || time.Since(p.cachedTaskFetched) > 30*time.Second {
		p.taskLoading = true
		return p.loadTaskDetails(wt.TaskID)
	}
	return nil
}
