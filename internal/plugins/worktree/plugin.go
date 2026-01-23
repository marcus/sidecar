package worktree

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	pluginID   = "worktree-manager"
	pluginName = "worktrees"
	pluginIcon = "W"

	// Refresh interval for worktree list
	refreshInterval = 5 * time.Second

	// Output buffer capacity (lines)
	outputBufferCap = 500

	// Pane layout constants
	dividerWidth    = 1 // Visual divider width
	dividerHitWidth = 3 // Wider hit target for drag

	// Flash effect duration for invalid key interaction
	flashDuration = 1500 * time.Millisecond

	// Hit region IDs
	regionSidebar                 = "sidebar"
	regionPreviewPane             = "preview-pane"
	regionPaneDivider             = "pane-divider"
	regionWorktreeItem            = "worktree-item"
	regionPreviewTab              = "preview-tab"
	regionAgentChoiceOption       = "agent-choice-option"
	regionAgentChoiceConfirm      = "agent-choice-confirm"
	regionAgentChoiceCancel       = "agent-choice-cancel"
	regionDeleteConfirmDelete     = "delete-confirm-delete"
	regionDeleteConfirmCancel     = "delete-confirm-cancel"
	regionDeleteLocalBranchCheck  = "delete-local-branch-check"
	regionDeleteRemoteBranchCheck = "delete-remote-branch-check"

	// Kanban view regions
	regionKanbanCard   = "kanban-card"
	regionKanbanColumn = "kanban-column"
	regionViewToggle   = "view-toggle"

	// Create modal regions
	regionCreateInput       = "create-input"
	regionCreateDropdown    = "create-dropdown"
	regionCreateButton      = "create-button"
	regionCreateCheckbox    = "create-checkbox"
	regionCreateAgentOption = "create-agent-option"

	// Task Link modal regions
	regionTaskLinkDropdown = "task-link-dropdown"

	// Merge modal regions
	regionMergeMethodOption    = "merge-method-option"
	regionMergeRadio           = "merge-radio"
	regionMergeConfirmCheckbox = "merge-confirm-checkbox"
	regionMergeConfirmButton   = "merge-confirm-btn"
	regionMergeSkipButton      = "merge-skip-btn"

	// Prompt Picker modal regions
	regionPromptItem   = "prompt-item"
	regionPromptFilter = "prompt-filter"

	// Sidebar header regions
	regionCreateWorktreeButton = "create-worktree-button"
	regionShellsPlusButton     = "shells-plus-button"
	regionWorktreesPlusButton  = "worktrees-plus-button"

	// Type selector modal regions
	regionTypeSelectorOption = "type-selector-option"
	regionTypeSelectorConfirm = "type-selector-confirm"
	regionTypeSelectorCancel  = "type-selector-cancel"

	// Shell delete confirmation modal regions
	regionDeleteShellConfirmDelete = "delete-shell-confirm-delete"
	regionDeleteShellConfirmCancel = "delete-shell-confirm-cancel"

	// Rename shell modal regions
	regionRenameShellInput   = "rename-shell-input"
	regionRenameShellConfirm = "rename-shell-confirm"
	regionRenameShellCancel  = "rename-shell-cancel"
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
	viewMode           ViewMode
	activePane         FocusPane
	previewTab         PreviewTab
	selectedIdx        int
	scrollOffset       int // Sidebar list scroll offset
	visibleCount       int // Number of visible list items
	previewOffset      int
	previewHorizOffset int  // Horizontal scroll offset for preview pane
	autoScrollOutput   bool // Auto-scroll output to follow agent (paused when user scrolls up)
	sidebarWidth       int       // Persisted sidebar width
	sidebarVisible     bool      // Whether sidebar is visible (toggled with \)
	lastPreviewResizeAt time.Time // Throttle background pane resizing
	flashPreviewTime   time.Time // When preview flash was triggered
	toastMessage       string    // Temporary toast message to display
	toastTime          time.Time // When toast was triggered

	// Kanban view state
	kanbanCol int // Current column index (0=Active, 1=Waiting, 2=Done, 3=Paused)
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
	diffViewMode  DiffViewMode              // Unified or side-by-side
	multiFileDiff *gitstatus.MultiFileDiff  // Parsed multi-file diff with positions

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
	createSkipPermissions bool      // Skip permissions checkbox
	createFocus           int       // 0=name, 1=base, 2=prompt, 3=task, 4=agent, 5=skipPerms, 6=create, 7=cancel
	createButtonHover     int       // 0=none, 1=create, 2=cancel
	createError           string    // Error message to display in create modal

	// Branch name validation state
	branchNameValid     bool     // Is current name valid?
	branchNameErrors    []string // Validation error messages
	branchNameSanitized string   // Suggested sanitized name

	// Prompt state for create modal
	createPrompts   []Prompt      // Available prompts (merged global + project)
	createPromptIdx int           // Selected prompt index (-1 = none)
	promptPicker    *PromptPicker // Picker modal state (when open)

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
	mergeState                *MergeWorkflowState
	mergeMethodHover          int // 0=none, 1=Create PR option, 2=Direct Merge option (for mouse hover)
	mergeConfirmCheckboxHover int // 0=none, 1-4 for cleanup checkboxes (mouse hover)
	mergeConfirmButtonHover   int // 0=none, 1=Clean Up, 2=Skip All (mouse hover)

	// Commit-before-merge state
	mergeCommitState        *MergeCommitState
	mergeCommitMessageInput textinput.Model

	// Agent choice modal state (attach vs restart)
	agentChoiceWorktree    *Worktree
	agentChoiceIdx         int // 0=attach, 1=restart
	agentChoiceButtonFocus int // 0=options, 1=confirm, 2=cancel
	agentChoiceButtonHover int // 0=none, 1=confirm, 2=cancel

	// Delete confirmation modal state
	deleteConfirmWorktree    *Worktree // Worktree pending deletion
	deleteConfirmButtonHover int       // 0=none, 1=delete, 2=cancel (for mouse hover)
	deleteLocalBranchOpt     bool      // Checkbox: delete local branch
	deleteRemoteBranchOpt    bool      // Checkbox: delete remote branch
	deleteHasRemote          bool      // Whether remote branch exists
	deleteIsMainBranch       bool      // Whether the worktree branch is the main branch (protected)
	deleteConfirmFocus       int       // 0=local checkbox, 1=remote checkbox (if exists), then delete/cancel btns
	deleteWarnings           []string  // Warnings from last delete operation (e.g., branch deletion failures)

	// Shell delete confirmation modal state
	deleteConfirmShell            *ShellSession // Shell pending deletion
	deleteShellConfirmFocus       int           // 0=delete button, 1=cancel button
	deleteShellConfirmButtonHover int           // 0=none, 1=delete, 2=cancel (for mouse hover)

	// Rename shell modal state
	renameShellSession     *ShellSession   // Shell being renamed
	renameShellInput       textinput.Model // Text input for new name
	renameShellFocus       int             // 0=input, 1=confirm, 2=cancel
	renameShellButtonHover int             // 0=none, 1=confirm, 2=cancel (for mouse hover)
	renameShellError       string          // Validation error message

	// Initial reconnection tracking
	initialReconnectDone bool

	// State restoration tracking (only restore once on startup)
	stateRestored bool

	// Interactive mode state (feature-gated behind tmux_interactive_input)
	interactiveState *InteractiveState

	// Sidebar header hover state
	hoverNewButton           bool
	hoverShellsPlusButton    bool
	hoverWorktreesPlusButton bool

	// Multiple shell sessions (not tied to git worktrees)
	shells           []*ShellSession // All shell sessions for this project
	selectedShellIdx int             // Currently selected shell index
	shellSelected    bool            // True when any shell is selected (vs a worktree)

	// Type selector modal state (shell vs worktree)
	typeSelectorIdx         int // 0=Shell, 1=Worktree
	typeSelectorHover       int // Mouse hover: -1=none, 0=Shell, 1=Worktree
	typeSelectorFocus       int // Focus: 0=options, 1=Confirm, 2=Cancel
	typeSelectorButtonHover int // Button hover: 0=none, 1=Confirm, 2=Cancel
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
	if ctx.Config != nil && ctx.Config.Plugins.Worktree.TmuxCaptureMaxBytes > 0 {
		p.tmuxCaptureMaxBytes = ctx.Config.Plugins.Worktree.TmuxCaptureMaxBytes
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

	// Discover existing shell sessions for this project
	p.initShellSessions()

	// Register dynamic keybindings
	if ctx.Keymap != nil {
		// Sidebar list context
		ctx.Keymap.RegisterPluginBinding("n", "new-worktree", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("enter", "attach", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("D", "delete-worktree", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("p", "push", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("d", "show-diff", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("v", "toggle-view", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("l", "focus-right", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("right", "focus-right", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("\\", "toggle-sidebar", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("tab", "switch-pane", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("[", "prev-tab", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("]", "next-tab", "worktree-list")

		// Task linking
		ctx.Keymap.RegisterPluginBinding("t", "link-task", "worktree-list")

		// Agent control bindings - register for both sidebar and preview pane contexts
		ctx.Keymap.RegisterPluginBinding("s", "start-agent", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("s", "start-agent", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("S", "stop-agent", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("S", "stop-agent", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("y", "approve", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("y", "approve", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("Y", "approve-all", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("Y", "approve-all", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("N", "reject", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("N", "reject", "worktree-preview")

		// Shell control binding (kill shell session)
		ctx.Keymap.RegisterPluginBinding("K", "kill-shell", "worktree-list")

		// Merge workflow binding
		ctx.Keymap.RegisterPluginBinding("m", "merge-workflow", "worktree-list")

		// Merge modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "worktree-merge")
		ctx.Keymap.RegisterPluginBinding("enter", "continue", "worktree-merge")
		ctx.Keymap.RegisterPluginBinding("up", "select-delete", "worktree-merge")
		ctx.Keymap.RegisterPluginBinding("down", "select-keep", "worktree-merge")

		// Preview pane context
		ctx.Keymap.RegisterPluginBinding("h", "focus-left", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("left", "focus-left", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("esc", "focus-left", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("tab", "switch-pane", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("\\", "toggle-sidebar", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("[", "prev-tab", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("]", "next-tab", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("v", "toggle-diff-view", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("ctrl+d", "page-down", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("ctrl+u", "page-up", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("0", "reset-scroll", "worktree-preview")

		// Create modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "worktree-create")
		ctx.Keymap.RegisterPluginBinding("enter", "confirm", "worktree-create")
		ctx.Keymap.RegisterPluginBinding("tab", "next-field", "worktree-create")
		ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-field", "worktree-create")

		// Task link modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "worktree-task-link")
		ctx.Keymap.RegisterPluginBinding("enter", "select-task", "worktree-task-link")

		// Agent choice modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "worktree-agent-choice")
		ctx.Keymap.RegisterPluginBinding("enter", "select", "worktree-agent-choice")
		ctx.Keymap.RegisterPluginBinding("j", "cursor-down", "worktree-agent-choice")
		ctx.Keymap.RegisterPluginBinding("k", "cursor-up", "worktree-agent-choice")
		ctx.Keymap.RegisterPluginBinding("down", "cursor-down", "worktree-agent-choice")
		ctx.Keymap.RegisterPluginBinding("up", "cursor-up", "worktree-agent-choice")
	}

	// Load saved sidebar width
	if savedWidth := state.GetWorktreeSidebarWidth(); savedWidth > 0 {
		p.sidebarWidth = savedWidth
	}

	// Load saved diff view mode
	if state.GetWorktreeDiffMode() == "side-by-side" {
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

	return tea.Batch(cmds...)
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	// Cleanup managed tmux sessions if needed
}

// saveSelectionState persists the current selection to disk.
func (p *Plugin) saveSelectionState() {
	if p.ctx == nil {
		return
	}

	wtState := state.GetWorktreeState(p.ctx.WorkDir)
	wtState.WorktreeName = ""
	wtState.ShellTmuxName = ""

	if p.shellSelected {
		// Shell is selected - save shell TmuxName
		if p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
			wtState.ShellTmuxName = p.shells[p.selectedShellIdx].TmuxName
		}
	} else {
		// Worktree is selected - save worktree name
		if p.selectedIdx >= 0 && p.selectedIdx < len(p.worktrees) {
			wtState.WorktreeName = p.worktrees[p.selectedIdx].Name
		}
	}

	if len(p.shells) > 0 {
		shellNames := make(map[string]string, len(p.shells))
		for _, shell := range p.shells {
			if shell == nil || shell.TmuxName == "" || shell.Name == "" {
				continue
			}
			// Only persist custom names, not defaults like "Shell 1", "Shell 2"
			if !isDefaultShellName(shell.Name) {
				shellNames[shell.TmuxName] = shell.Name
			}
		}
		if len(shellNames) > 0 {
			wtState.ShellDisplayNames = shellNames
		} else {
			wtState.ShellDisplayNames = nil
		}
	}

	// Only save if we have something selected or display names
	if wtState.WorktreeName != "" || wtState.ShellTmuxName != "" || len(wtState.ShellDisplayNames) > 0 {
		_ = state.SetWorktreeState(p.ctx.WorkDir, wtState)
	}
}

// restoreSelectionState restores selection from saved state.
// Returns true if selection was restored, false if default should be used.
func (p *Plugin) restoreSelectionState() bool {
	if p.ctx == nil {
		return false
	}

	wtState := state.GetWorktreeState(p.ctx.WorkDir)

	// No saved state
	if wtState.WorktreeName == "" && wtState.ShellTmuxName == "" {
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
	if wtState.WorktreeName != "" {
		for i, wt := range p.worktrees {
			if wt.Name == wtState.WorktreeName {
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
	p.createSkipPermissions = false
	p.createFocus = 0
	p.createError = ""
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
}

// initCreateModalBase initializes common create modal state.
func (p *Plugin) initCreateModalBase() {
	p.viewMode = ViewModeCreate

	// Initialize text inputs
	p.createNameInput = textinput.New()
	p.createNameInput.Placeholder = "feature-name"
	p.createNameInput.Focus()
	p.createNameInput.CharLimit = 100

	p.createBaseBranchInput = textinput.New()
	p.createBaseBranchInput.Placeholder = "main"
	p.createBaseBranchInput.CharLimit = 100

	p.taskSearchInput = textinput.New()
	p.taskSearchInput.Placeholder = "Search tasks..."
	p.taskSearchInput.CharLimit = 100

	// Reset all state
	p.createTaskID = ""
	p.createTaskTitle = ""
	p.createAgentType = AgentClaude
	p.createSkipPermissions = false
	p.createFocus = 0
	p.createError = ""
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
		p.previewHorizOffset = 0
		p.autoScrollOutput = true
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
	p.previewTab = PreviewTab((int(p.previewTab) + delta + 3) % 3)
	p.previewOffset = 0
	p.previewHorizOffset = 0
	p.autoScrollOutput = true // Reset auto-scroll when switching tabs

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
