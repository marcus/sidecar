package worktree

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/state"
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
	sidebarWidth       int  // Persisted sidebar width
	sidebarVisible     bool // Whether sidebar is visible (toggled with \)

	// Kanban view state
	kanbanCol int // Current column index (0=Active, 1=Waiting, 2=Done, 3=Paused)
	kanbanRow int // Current row within the column

	// Agent state
	attachedSession string // Name of worktree we're attached to (pauses polling)

	// Mouse support
	mouseHandler *mouse.Handler

	// Async state
	refreshing  bool
	lastRefresh time.Time

	// Diff state
	diffContent  string
	diffRaw      string
	diffViewMode DiffViewMode // Unified or side-by-side

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

	// Markdown rendering for task view
	markdownRenderer     *markdown.Renderer
	taskMarkdownMode     bool     // true = rendered, false = raw
	taskMarkdownRendered []string // Cached rendered lines
	taskMarkdownWidth    int      // Width used for cached render

	// Merge workflow state
	mergeState               *MergeWorkflowState
	mergeMethodHover         int // 0=none, 1=Create PR option, 2=Direct Merge option (for mouse hover)
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
	deleteConfirmFocus       int       // 0=local checkbox, 1=remote checkbox (if exists), then delete/cancel btns
	deleteWarnings           []string  // Warnings from last delete operation (e.g., branch deletion failures)

	// Initial reconnection tracking
	initialReconnectDone bool
}

// New creates a new worktree manager plugin.
func New() *Plugin {
	// Create markdown renderer (ignore error, will fall back to plain text)
	mdRenderer, _ := markdown.NewRenderer()

	return &Plugin{
		worktrees:        make([]*Worktree, 0),
		agents:           make(map[string]*Agent),
		managedSessions:  make(map[string]bool),
		viewMode:         ViewModeList,
		activePane:       PaneSidebar,
		previewTab:       PreviewTabOutput,
		mouseHandler:     mouse.NewHandler(),
		sidebarWidth:     40,   // Default 40% sidebar
		sidebarVisible:   true, // Sidebar visible by default
		autoScrollOutput: true, // Auto-scroll to follow agent output
		markdownRenderer: mdRenderer,
		taskMarkdownMode: true, // Default to rendered mode
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
func (p *Plugin) SetFocused(f bool) { p.focused = f }

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	p.ctx = ctx

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
	// Only refresh worktrees - reconnectAgents will be called after worktrees are loaded
	return p.refreshWorktrees()
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	// Cleanup managed tmux sessions if needed
}

// selectedWorktree returns the currently selected worktree.
func (p *Plugin) selectedWorktree() *Worktree {
	if p.selectedIdx < 0 || p.selectedIdx >= len(p.worktrees) {
		return nil
	}
	return p.worktrees[p.selectedIdx]
}

// outputVisibleFor returns true when a worktree's output is on-screen.
func (p *Plugin) outputVisibleFor(worktreeName string) bool {
	if !p.focused {
		return false
	}
	if p.viewMode != ViewModeList {
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
func (p *Plugin) moveCursor(delta int) {
	oldIdx := p.selectedIdx
	p.selectedIdx += delta
	if p.selectedIdx < 0 {
		p.selectedIdx = 0
	}
	if p.selectedIdx >= len(p.worktrees) {
		p.selectedIdx = len(p.worktrees) - 1
	}
	// Reset preview scroll state when changing worktree selection
	if p.selectedIdx != oldIdx {
		p.previewOffset = 0
		p.previewHorizOffset = 0
		p.autoScrollOutput = true
	}
	p.ensureVisible()
}

// ensureVisible adjusts scroll to keep selected item visible.
func (p *Plugin) ensureVisible() {
	if p.selectedIdx < p.scrollOffset {
		p.scrollOffset = p.selectedIdx
	}
	if p.visibleCount > 0 && p.selectedIdx >= p.scrollOffset+p.visibleCount {
		p.scrollOffset = p.selectedIdx - p.visibleCount + 1
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
// Always loads diff (for preloading), and also loads task if task tab is active.
func (p *Plugin) loadSelectedContent() tea.Cmd {
	var cmds []tea.Cmd
	switch p.previewTab {
	case PreviewTabTask:
		if cmd := p.loadSelectedDiff(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := p.loadTaskDetailsIfNeeded(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	default:
		if cmd := p.loadSelectedDiff(); cmd != nil {
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

// loadTaskDetailsIfNeeded loads task details if not cached or stale.
func (p *Plugin) loadTaskDetailsIfNeeded() tea.Cmd {
	wt := p.selectedWorktree()
	if wt == nil || wt.TaskID == "" {
		return nil
	}

	// Check if we need to refresh (different task or cache is older than 30 seconds)
	if p.cachedTaskID != wt.TaskID || time.Since(p.cachedTaskFetched) > 30*time.Second {
		return p.loadTaskDetails(wt.TaskID)
	}
	return nil
}
