package worktree

import (
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
	regionSidebar              = "sidebar"
	regionPreviewPane          = "preview-pane"
	regionPaneDivider          = "pane-divider"
	regionWorktreeItem         = "worktree-item"
	regionPreviewTab           = "preview-tab"
	regionAgentChoiceOption    = "agent-choice-option"
	regionAgentChoiceConfirm   = "agent-choice-confirm"
	regionAgentChoiceCancel    = "agent-choice-cancel"
	regionDeleteConfirmDelete  = "delete-confirm-delete"
	regionDeleteConfirmCancel  = "delete-confirm-cancel"

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
	regionMergeRadio = "merge-radio"

	// Prompt Picker modal regions
	regionPromptItem = "prompt-item"
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
	viewMode       ViewMode
	activePane     FocusPane
	previewTab     PreviewTab
	selectedIdx    int
	scrollOffset   int  // Sidebar list scroll offset
	visibleCount   int  // Number of visible list items
	previewOffset      int
	previewHorizOffset int  // Horizontal scroll offset for preview pane
	autoScrollOutput   bool // Auto-scroll output to follow agent (paused when user scrolls up)
	sidebarWidth     int  // Persisted sidebar width
	sidebarVisible bool // Whether sidebar is visible (toggled with \)

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

	// Merge workflow state
	mergeState *MergeWorkflowState

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
	deleteConfirmButtonFocus int       // 0=delete, 1=cancel
	deleteConfirmButtonHover int       // 0=none, 1=delete, 2=cancel

	// Initial reconnection tracking
	initialReconnectDone bool
}

// New creates a new worktree manager plugin.
func New() *Plugin {
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

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case RefreshMsg:
		if !p.refreshing {
			p.refreshing = true
			cmds = append(cmds, p.refreshWorktrees())
		}

	case RefreshDoneMsg:
		p.refreshing = false
		p.lastRefresh = time.Now()
		if msg.Err == nil {
			p.worktrees = msg.Worktrees
			// Preserve agent pointers from existing agents map
			for _, wt := range p.worktrees {
				if agent, ok := p.agents[wt.Name]; ok {
					wt.Agent = agent
				}
			}
			// Load stats, task links, and agent types for each worktree
			for _, wt := range p.worktrees {
				cmds = append(cmds, p.loadStats(wt.Path))
				// Load linked task ID from .sidecar-task file
				wt.TaskID = loadTaskLink(wt.Path)
				// Load chosen agent type from .sidecar-agent file
				wt.ChosenAgentType = loadAgentType(wt.Path)
				// Load PR URL from .sidecar-pr file
				wt.PRURL = loadPRURL(wt.Path)
			}
			// Detect conflicts across worktrees
			cmds = append(cmds, p.loadConflicts())

			// Load diff for the selected worktree so diff tab shows content immediately
			cmds = append(cmds, p.loadSelectedDiff())

			// Reconnect to existing tmux sessions after initial worktree load
			if !p.initialReconnectDone {
				p.initialReconnectDone = true
				cmds = append(cmds, p.reconnectAgents())
			}
		}

	case ConflictsDetectedMsg:
		if msg.Err == nil {
			p.conflicts = msg.Conflicts
		}

	case StatsLoadedMsg:
		for _, wt := range p.worktrees {
			if wt.Name == msg.WorktreeName {
				wt.Stats = msg.Stats
				break
			}
		}

	case DiffLoadedMsg:
		if p.selectedWorktree() != nil && p.selectedWorktree().Name == msg.WorktreeName {
			p.diffContent = msg.Content
			p.diffRaw = msg.Raw
		}

	case CreateDoneMsg:
		if msg.Err != nil {
			p.createError = msg.Err.Error()
			// Stay in ViewModeCreate - don't close modal or clear state
		} else {
			p.viewMode = ViewModeList
			p.worktrees = append(p.worktrees, msg.Worktree)
			p.selectedIdx = len(p.worktrees) - 1
			p.clearCreateModal()

			// Start agent or attach based on selection
			if msg.AgentType != AgentNone && msg.AgentType != "" {
				cmds = append(cmds, p.StartAgentWithOptions(msg.Worktree, msg.AgentType, msg.SkipPerms, msg.Prompt))
			} else {
				// "None" selected - attach to worktree directory
				cmds = append(cmds, p.AttachToWorktreeDir(msg.Worktree))
			}
		}

	case PromptSelectedMsg:
		// Prompt selected from picker
		p.viewMode = ViewModeCreate
		p.promptPicker = nil
		if msg.Prompt != nil {
			// Find index of selected prompt
			for i, pr := range p.createPrompts {
				if pr.Name == msg.Prompt.Name {
					p.createPromptIdx = i
					break
				}
			}
			// If ticketMode is none, skip task field and jump to agent
			if msg.Prompt.TicketMode == TicketNone {
				p.createFocus = 4 // agent field
			} else {
				p.createFocus = 3 // task field
			}
		} else {
			p.createPromptIdx = -1
			p.createFocus = 3 // task field
		}

	case PromptCancelledMsg:
		// Picker cancelled, return to create modal
		p.viewMode = ViewModeCreate
		p.promptPicker = nil

	case DeleteDoneMsg:
		if msg.Err == nil {
			p.removeWorktreeByName(msg.Name)
			if p.selectedIdx >= len(p.worktrees) && p.selectedIdx > 0 {
				p.selectedIdx--
			}
			// Clear preview pane content to ensure old diff doesn't persist
			p.diffContent = ""
			p.diffRaw = ""
			p.cachedTaskID = ""
			p.cachedTask = nil
			// Load diff for newly selected worktree
			cmds = append(cmds, p.loadSelectedDiff())
		}

	case PushDoneMsg:
		// Handle push result notification
		if msg.Err == nil {
			cmds = append(cmds, p.refreshWorktrees())
		}

	// Agent messages
	case AgentStartedMsg:
		if msg.Err == nil {
			// Create agent record
			agent := &Agent{
				Type:        msg.AgentType,
				TmuxSession: msg.SessionName,
				StartedAt:   time.Now(),
				OutputBuf:   NewOutputBuffer(outputBufferCap),
			}

			if wt := p.findWorktree(msg.WorktreeName); wt != nil {
				wt.Agent = agent
				wt.Status = StatusActive
			}
			p.agents[msg.WorktreeName] = agent
			p.managedSessions[msg.SessionName] = true

			// Start polling for output
			cmds = append(cmds, p.scheduleAgentPoll(msg.WorktreeName, 500*time.Millisecond))
		}

	case pollAgentMsg:
		// Skip polling while user is attached to session
		if p.attachedSession == msg.WorktreeName {
			return p, nil
		}
		return p, p.handlePollAgent(msg.WorktreeName)

	case AgentOutputMsg:
		// Update state (content already stored by Update() in handlePollAgent)
		if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil {
			wt.Agent.LastOutput = time.Now()
			wt.Agent.WaitingFor = msg.WaitingFor
			wt.Status = msg.Status
		}
		// Schedule next poll (1 second interval)
		return p, p.scheduleAgentPoll(msg.WorktreeName, 1*time.Second)

	case AgentPollUnchangedMsg:
		// Content unchanged - schedule next poll with delay (no state update needed)
		return p, p.scheduleAgentPoll(msg.WorktreeName, 1*time.Second)

	case AgentStoppedMsg:
		if wt := p.findWorktree(msg.WorktreeName); wt != nil {
			wt.Agent = nil
			wt.Status = StatusPaused
		}
		delete(p.agents, msg.WorktreeName)
		return p, nil

	case restartAgentMsg:
		// Start new agent after stop completed
		if msg.worktree != nil {
			agentType := msg.worktree.ChosenAgentType
			if agentType == "" {
				agentType = AgentClaude
			}
			return p, p.StartAgent(msg.worktree, agentType)
		}
		return p, nil

	case TmuxAttachFinishedMsg:
		// Clear attached state
		p.attachedSession = ""

		// Re-enable mouse after tea.ExecProcess (tmux attach disables it)
		cmds = append(cmds, func() tea.Msg { return tea.EnableMouseAllMotion() })

		// Resume polling and refresh to capture what happened while attached
		if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil {
			// Immediate poll to get current state
			cmds = append(cmds, p.scheduleAgentPoll(msg.WorktreeName, 0))
		}
		cmds = append(cmds, p.refreshWorktrees())

	case ApproveResultMsg:
		if msg.Err == nil {
			// Clear waiting state, force immediate poll
			if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil {
				wt.Agent.WaitingFor = ""
				wt.Status = StatusActive
			}
			cmds = append(cmds, p.scheduleAgentPoll(msg.WorktreeName, 0))
		}

	case RejectResultMsg:
		if msg.Err == nil {
			// Clear waiting state, force immediate poll
			if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil {
				wt.Agent.WaitingFor = ""
				wt.Status = StatusActive
			}
			cmds = append(cmds, p.scheduleAgentPoll(msg.WorktreeName, 0))
		}

	case TaskLinkedMsg:
		if msg.Err == nil {
			if wt := p.findWorktree(msg.WorktreeName); wt != nil {
				wt.TaskID = msg.TaskID
				// Load task details for the newly linked task
				if msg.TaskID != "" {
					cmds = append(cmds, p.loadTaskDetails(msg.TaskID))
				}
			}
		}

	case TaskSearchResultsMsg:
		p.taskSearchLoading = false
		if msg.Err == nil {
			p.taskSearchAll = msg.Tasks
			p.taskSearchFiltered = filterTasks(p.taskSearchInput.Value(), p.taskSearchAll)
			p.taskSearchIdx = 0
		}

	case BranchListMsg:
		if msg.Err == nil {
			p.branchAll = msg.Branches
			p.branchFiltered = filterBranches(p.createBaseBranchInput.Value(), p.branchAll)
			p.branchIdx = 0
		}

	case TaskDetailsLoadedMsg:
		if msg.Err == nil && msg.Details != nil {
			p.cachedTaskID = msg.TaskID
			p.cachedTask = msg.Details
			p.cachedTaskFetched = time.Now()
		}

	case UncommittedChangesCheckMsg:
		if msg.Err != nil {
			// Error checking changes - cancel merge and return to list
			p.viewMode = ViewModeList
		} else if msg.HasChanges {
			// Show commit modal
			wt := p.findWorktree(msg.WorktreeName)
			if wt != nil {
				p.mergeCommitState = &MergeCommitState{
					Worktree:       wt,
					StagedCount:    msg.StagedCount,
					ModifiedCount:  msg.ModifiedCount,
					UntrackedCount: msg.UntrackedCount,
				}
				p.mergeCommitMessageInput = textinput.New()
				p.mergeCommitMessageInput.Placeholder = "Commit message..."
				p.mergeCommitMessageInput.Focus()
				p.mergeCommitMessageInput.CharLimit = 200
				p.viewMode = ViewModeCommitForMerge
			}
		} else {
			// No uncommitted changes, proceed to merge
			wt := p.findWorktree(msg.WorktreeName)
			if wt != nil {
				cmds = append(cmds, p.proceedToMergeWorkflow(wt))
			}
		}

	case MergeCommitDoneMsg:
		if p.mergeCommitState != nil && p.mergeCommitState.Worktree.Name == msg.WorktreeName {
			if msg.Err != nil {
				p.mergeCommitState.Error = msg.Err.Error()
			} else {
				// Commit succeeded, proceed to merge workflow
				wt := p.mergeCommitState.Worktree
				p.mergeCommitState = nil
				p.mergeCommitMessageInput = textinput.Model{}
				cmds = append(cmds, p.proceedToMergeWorkflow(wt))
			}
		}

	case MergeStepCompleteMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorktreeName {
			if msg.Err != nil {
				p.mergeState.Error = msg.Err
				p.mergeState.StepStatus[msg.Step] = "error"
			} else {
				switch msg.Step {
				case MergeStepReviewDiff:
					// ReviewDiff: User manually advances, so mark done here
					p.mergeState.StepStatus[msg.Step] = "done"
					p.mergeState.DiffSummary = msg.Data
				case MergeStepPush:
					// Push complete - advanceMergeStep handles status transition
					cmds = append(cmds, p.advanceMergeStep())
				case MergeStepCreatePR:
					p.mergeState.PRURL = msg.Data
					// Save PR URL to worktree for indicator in list
					if wt := p.mergeState.Worktree; wt != nil && msg.Data != "" {
						wt.PRURL = msg.Data
						savePRURL(wt.Path, msg.Data)
					}
					// PR created - advanceMergeStep handles status transition
					cmds = append(cmds, p.advanceMergeStep())
				case MergeStepCleanup:
					// Cleanup done, mark done and remove from worktree list
					p.mergeState.StepStatus[msg.Step] = "done"
					p.removeWorktreeByName(msg.WorktreeName)
					if p.selectedIdx >= len(p.worktrees) && p.selectedIdx > 0 {
						p.selectedIdx--
					}
					p.mergeState.Step = MergeStepDone
				}
			}
		}

	case CheckPRMergedMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorktreeName {
			if msg.Err != nil {
				// Silently ignore check errors, will retry
				cmds = append(cmds, p.schedulePRCheck(msg.WorktreeName, 30*time.Second))
			} else if msg.Merged {
				// PR was merged! Move to cleanup step
				p.mergeState.StepStatus[MergeStepWaitingMerge] = "done"
				cmds = append(cmds, p.advanceMergeStep())
			} else {
				// Not merged yet, check again later
				cmds = append(cmds, p.schedulePRCheck(msg.WorktreeName, 30*time.Second))
			}
		}

	case checkPRMergeMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorktreeName {
			cmds = append(cmds, p.checkPRMerged(p.mergeState.Worktree))
		}

	case reconnectedAgentsMsg:
		return p, tea.Batch(msg.Cmds...)

	case tea.KeyMsg:
		cmd := p.handleKeyPress(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseMsg:
		cmd := p.handleMouse(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return p, tea.Batch(cmds...)
}

// selectedWorktree returns the currently selected worktree.
func (p *Plugin) selectedWorktree() *Worktree {
	if p.selectedIdx < 0 || p.selectedIdx >= len(p.worktrees) {
		return nil
	}
	return p.worktrees[p.selectedIdx]
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

// handleKeyPress processes key input based on current view mode.
func (p *Plugin) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	switch p.viewMode {
	case ViewModeList, ViewModeKanban:
		return p.handleListKeys(msg)
	case ViewModeCreate:
		return p.handleCreateKeys(msg)
	case ViewModeTaskLink:
		return p.handleTaskLinkKeys(msg)
	case ViewModeMerge:
		return p.handleMergeKeys(msg)
	case ViewModeAgentChoice:
		return p.handleAgentChoiceKeys(msg)
	case ViewModeConfirmDelete:
		return p.handleConfirmDeleteKeys(msg)
	case ViewModeCommitForMerge:
		return p.handleCommitForMergeKeys(msg)
	case ViewModePromptPicker:
		return p.handlePromptPickerKeys(msg)
	}
	return nil
}

// handlePromptPickerKeys handles keys in the prompt picker modal.
func (p *Plugin) handlePromptPickerKeys(msg tea.KeyMsg) tea.Cmd {
	if p.promptPicker == nil {
		return nil
	}
	_, cmd := p.promptPicker.Update(msg)
	return cmd
}

// handleAgentChoiceKeys handles keys in agent choice modal.
func (p *Plugin) handleAgentChoiceKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab":
		// Cycle focus: options(0) -> confirm(1) -> cancel(2) -> options(0)
		p.agentChoiceButtonFocus = (p.agentChoiceButtonFocus + 1) % 3
	case "shift+tab":
		// Reverse cycle
		p.agentChoiceButtonFocus = (p.agentChoiceButtonFocus + 2) % 3
	case "j", "down":
		if p.agentChoiceButtonFocus == 0 && p.agentChoiceIdx < 1 {
			p.agentChoiceIdx++
		}
	case "k", "up":
		if p.agentChoiceButtonFocus == 0 && p.agentChoiceIdx > 0 {
			p.agentChoiceIdx--
		}
	case "enter":
		// If focused on cancel button, cancel
		if p.agentChoiceButtonFocus == 2 {
			p.viewMode = ViewModeList
			p.agentChoiceWorktree = nil
			p.agentChoiceButtonFocus = 0
			return nil
		}
		// Confirm action
		return p.executeAgentChoice()
	case "esc", "q":
		p.viewMode = ViewModeList
		p.agentChoiceWorktree = nil
		p.agentChoiceButtonFocus = 0
	}
	return nil
}

// executeAgentChoice executes the selected agent choice action.
func (p *Plugin) executeAgentChoice() tea.Cmd {
	wt := p.agentChoiceWorktree
	p.viewMode = ViewModeList
	p.agentChoiceWorktree = nil
	p.agentChoiceButtonFocus = 0
	if wt == nil {
		return nil
	}
	if p.agentChoiceIdx == 0 {
		// Attach to existing session
		return p.AttachToSession(wt)
	}
	// Restart agent: stop first, then start
	return tea.Sequence(
		p.StopAgent(wt),
		func() tea.Msg {
			return restartAgentMsg{worktree: wt}
		},
	)
}

// handleConfirmDeleteKeys handles keys in delete confirmation modal.
func (p *Plugin) handleConfirmDeleteKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab":
		// Cycle focus: delete(0) -> cancel(1) -> delete(0)
		p.deleteConfirmButtonFocus = (p.deleteConfirmButtonFocus + 1) % 2
	case "shift+tab":
		// Reverse cycle
		p.deleteConfirmButtonFocus = (p.deleteConfirmButtonFocus + 1) % 2
	case "enter":
		if p.deleteConfirmButtonFocus == 1 {
			// Cancel button focused
			return p.cancelDelete()
		}
		// Delete button focused - execute delete
		return p.executeDelete()
	case "D":
		// Power user shortcut - immediate confirm
		return p.executeDelete()
	case "esc", "q":
		return p.cancelDelete()
	case "h", "left":
		if p.deleteConfirmButtonFocus > 0 {
			p.deleteConfirmButtonFocus--
		}
	case "l", "right":
		if p.deleteConfirmButtonFocus < 1 {
			p.deleteConfirmButtonFocus++
		}
	}
	return nil
}

// executeDelete performs the actual worktree deletion and cleans up state.
func (p *Plugin) executeDelete() tea.Cmd {
	wt := p.deleteConfirmWorktree
	if wt == nil {
		p.viewMode = ViewModeList
		return nil
	}

	name := wt.Name
	path := wt.Path

	// Clear modal state
	p.viewMode = ViewModeList
	p.deleteConfirmWorktree = nil
	p.deleteConfirmButtonFocus = 0
	p.deleteConfirmButtonHover = 0

	// Clear preview pane content
	p.diffContent = ""
	p.diffRaw = ""
	p.cachedTaskID = ""
	p.cachedTask = nil

	return func() tea.Msg {
		err := doDeleteWorktree(path)
		return DeleteDoneMsg{Name: name, Err: err}
	}
}

// cancelDelete closes the delete confirmation modal without deleting.
func (p *Plugin) cancelDelete() tea.Cmd {
	p.viewMode = ViewModeList
	p.deleteConfirmWorktree = nil
	p.deleteConfirmButtonFocus = 0
	p.deleteConfirmButtonHover = 0
	return nil
}

// handleListKeys handles keys in list view (and kanban view).
func (p *Plugin) handleListKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if p.viewMode == ViewModeKanban {
			// Kanban mode: move down within column
			p.moveKanbanRow(1)
			return p.loadSelectedContent()
		}
		if p.activePane == PaneSidebar {
			p.moveCursor(1)
			return p.loadSelectedContent()
		}
		// Scroll down toward newer content (decrease offset from bottom)
		if p.previewOffset > 0 {
			p.previewOffset--
			if p.previewOffset == 0 {
				p.autoScrollOutput = true // Resume auto-scroll when at bottom
			}
		}
	case "k", "up":
		if p.viewMode == ViewModeKanban {
			// Kanban mode: move up within column
			p.moveKanbanRow(-1)
			return p.loadSelectedContent()
		}
		if p.activePane == PaneSidebar {
			p.moveCursor(-1)
			return p.loadSelectedContent()
		}
		// Scroll up toward older content (increase offset from bottom)
		p.autoScrollOutput = false
		p.previewOffset++
	case "g":
		if p.activePane == PaneSidebar {
			p.selectedIdx = 0
			p.scrollOffset = 0
			return p.loadSelectedContent()
		}
		// Go to top (oldest content) - pause auto-scroll
		p.autoScrollOutput = false
		p.previewOffset = 10000 // Large offset, will be clamped in render
	case "G":
		if p.activePane == PaneSidebar {
			p.selectedIdx = len(p.worktrees) - 1
			p.ensureVisible()
			return p.loadSelectedContent()
		}
		// Go to bottom (newest content) - resume auto-scroll
		p.previewOffset = 0
		p.autoScrollOutput = true
	case "n":
		p.viewMode = ViewModeCreate
		// Initialize textinputs for create modal
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
		p.createAgentType = AgentClaude // Default to Claude
		p.createSkipPermissions = false
		p.createFocus = 0
		p.taskSearchLoading = true
		// Load prompts from global and project config
		home, _ := os.UserHomeDir()
		configDir := filepath.Join(home, ".config", "sidecar")
		p.createPrompts = LoadPrompts(configDir, p.ctx.WorkDir)
		p.createPromptIdx = -1 // No prompt selected by default
		p.promptPicker = nil
		p.branchAll = nil
		p.branchFiltered = nil
		p.branchIdx = 0
		return tea.Batch(p.loadOpenTasks(), p.loadBranches())
	case "D":
		wt := p.selectedWorktree()
		if wt == nil {
			return nil
		}
		p.viewMode = ViewModeConfirmDelete
		p.deleteConfirmWorktree = wt
		p.deleteConfirmButtonFocus = 0 // Focus delete button
		p.deleteConfirmButtonHover = 0
		return nil
	case "p":
		return p.pushSelected()
	case "l", "right":
		if p.viewMode == ViewModeKanban {
			// Kanban mode: move to next column
			p.moveKanbanColumn(1)
			return p.loadSelectedContent()
		}
		if p.activePane == PaneSidebar {
			p.activePane = PanePreview
		} else {
			// Horizontal scroll right in preview pane
			p.previewHorizOffset += 10
		}
	case "enter":
		// Attach to tmux session if agent running, otherwise focus preview
		wt := p.selectedWorktree()
		if wt != nil && wt.Agent != nil {
			p.attachedSession = wt.Name
			return p.AttachToSession(wt)
		}
		if p.activePane == PaneSidebar {
			p.activePane = PanePreview
		}
	case "h", "left":
		if p.viewMode == ViewModeKanban {
			// Kanban mode: move to previous column
			p.moveKanbanColumn(-1)
			return p.loadSelectedContent()
		}
		if p.activePane == PanePreview {
			// Horizontal scroll left in preview pane
			if p.previewHorizOffset > 0 {
				p.previewHorizOffset -= 10
				if p.previewHorizOffset < 0 {
					p.previewHorizOffset = 0
				}
			}
		}
	case "esc":
		if p.activePane == PanePreview {
			p.activePane = PaneSidebar
		}
	case "\\":
		p.toggleSidebar()
	case "tab", "shift+tab":
		// Switch focus between panes (consistent with other plugins)
		if p.activePane == PaneSidebar && p.sidebarVisible {
			p.activePane = PanePreview
		} else if p.activePane == PanePreview && p.sidebarVisible {
			p.activePane = PaneSidebar
		}
	case "[":
		return p.cyclePreviewTab(-1)
	case "]":
		return p.cyclePreviewTab(1)
	case "r":
		return func() tea.Msg { return RefreshMsg{} }
	case "v":
		// In sidebar: toggle between list and kanban view
		// In preview pane on diff tab: toggle unified/side-by-side diff view
		if p.activePane == PanePreview && p.previewTab == PreviewTabDiff {
			if p.diffViewMode == DiffViewUnified {
				p.diffViewMode = DiffViewSideBySide
				_ = state.SetWorktreeDiffMode("side-by-side")
			} else {
				p.diffViewMode = DiffViewUnified
				_ = state.SetWorktreeDiffMode("unified")
			}
		} else if p.activePane == PaneSidebar || p.viewMode == ViewModeKanban {
			if p.viewMode == ViewModeList {
				p.viewMode = ViewModeKanban
				p.syncListToKanban()
			} else if p.viewMode == ViewModeKanban {
				p.viewMode = ViewModeList
			}
		}
	case "ctrl+d":
		// Page down in preview pane
		if p.activePane == PanePreview {
			pageSize := p.height / 2
			if pageSize < 5 {
				pageSize = 5
			}
			if p.previewTab == PreviewTabOutput {
				// For output, offset is from bottom
				if p.previewOffset > pageSize {
					p.previewOffset -= pageSize
				} else {
					p.previewOffset = 0
					p.autoScrollOutput = true
				}
			} else {
				p.previewOffset += pageSize
			}
		}
	case "ctrl+u":
		// Page up in preview pane
		if p.activePane == PanePreview {
			pageSize := p.height / 2
			if pageSize < 5 {
				pageSize = 5
			}
			if p.previewTab == PreviewTabOutput {
				// For output, offset is from bottom
				p.autoScrollOutput = false
				p.previewOffset += pageSize
			} else {
				if p.previewOffset > pageSize {
					p.previewOffset -= pageSize
				} else {
					p.previewOffset = 0
				}
			}
		}
	case "0":
		// Reset horizontal scroll
		if p.activePane == PanePreview {
			p.previewHorizOffset = 0
		}

	// Agent control keys
	case "s":
		// Start agent on selected worktree
		wt := p.selectedWorktree()
		if wt == nil {
			return nil
		}
		if wt.Agent == nil {
			// No agent running - start new one
			return p.StartAgent(wt, AgentClaude)
		}
		// Agent exists - show choice modal (attach or restart)
		p.agentChoiceWorktree = wt
		p.agentChoiceIdx = 0           // Default to attach
		p.agentChoiceButtonFocus = 0   // Start with options focused
		p.agentChoiceButtonHover = 0   // Clear hover state
		p.viewMode = ViewModeAgentChoice
		return nil
	case "S":
		// Stop agent on selected worktree
		wt := p.selectedWorktree()
		if wt != nil && wt.Agent != nil {
			return p.StopAgent(wt)
		}
	case "y":
		// Approve pending prompt on selected worktree
		wt := p.selectedWorktree()
		if wt != nil && wt.Status == StatusWaiting && wt.Agent != nil {
			return p.Approve(wt)
		}
	case "Y":
		// Approve all pending prompts
		return p.ApproveAll()
	case "N":
		// Reject pending prompt on selected worktree
		wt := p.selectedWorktree()
		if wt != nil && wt.Status == StatusWaiting && wt.Agent != nil {
			return p.Reject(wt)
		}
	case "t":
		// Link/unlink td task
		wt := p.selectedWorktree()
		if wt != nil {
			if wt.TaskID != "" {
				// Already linked - unlink
				return p.unlinkTask(wt)
			}
			// No task linked - show task link modal
			p.viewMode = ViewModeTaskLink
			p.linkingWorktree = wt
			p.taskSearchInput = textinput.New()
			p.taskSearchInput.Placeholder = "Search tasks..."
			p.taskSearchInput.Focus()
			p.taskSearchInput.CharLimit = 100
			p.taskSearchIdx = 0
			p.taskSearchLoading = true
			return p.loadOpenTasks()
		}
	case "m":
		// Start merge workflow
		wt := p.selectedWorktree()
		if wt != nil {
			return p.startMergeWorkflow(wt)
		}
	}
	return nil
}

// handleCreateKeys handles keys in create modal.
// createFocus: 0=name, 1=base, 2=prompt, 3=task, 4=agent, 5=skipPerms, 6=create button, 7=cancel button
func (p *Plugin) handleCreateKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		p.viewMode = ViewModeList
		p.clearCreateModal()
		return nil
	case "tab":
		// Blur current, move focus, focus new
		p.blurCreateInputs()
		p.createFocus = (p.createFocus + 1) % 8
		// Skip task field (3) if prompt has ticketMode=none
		if p.createFocus == 3 {
			prompt := p.getSelectedPrompt()
			if prompt != nil && prompt.TicketMode == TicketNone {
				p.createFocus = 4 // Skip to agent
			}
		}
		// Skip state 5 (skipPerms) if checkbox is hidden
		if p.createFocus == 5 && !p.shouldShowSkipPermissions() {
			p.createFocus = 6
		}
		p.focusCreateInput()
		return nil
	case "shift+tab":
		p.blurCreateInputs()
		p.createFocus = (p.createFocus + 7) % 8
		// Skip state 5 (skipPerms) if checkbox is hidden
		if p.createFocus == 5 && !p.shouldShowSkipPermissions() {
			p.createFocus = 4
		}
		// Skip task field (3) if prompt has ticketMode=none
		if p.createFocus == 3 {
			prompt := p.getSelectedPrompt()
			if prompt != nil && prompt.TicketMode == TicketNone {
				p.createFocus = 2 // Back to prompt
			}
		}
		p.focusCreateInput()
		return nil
	case "backspace":
		// Clear selected task and allow searching again (now focus 3)
		if p.createFocus == 3 && p.createTaskID != "" {
			p.createTaskID = ""
			p.createTaskTitle = ""
			p.taskSearchInput.SetValue("")
			p.taskSearchInput.Focus()
			p.taskSearchFiltered = filterTasks("", p.taskSearchAll)
			p.taskSearchIdx = 0
			return nil
		}
	case " ":
		// Toggle skip permissions checkbox (now focus 5)
		if p.createFocus == 5 {
			p.createSkipPermissions = !p.createSkipPermissions
			return nil
		}
	case "up":
		// Navigate branch dropdown
		if p.createFocus == 1 && len(p.branchFiltered) > 0 {
			if p.branchIdx > 0 {
				p.branchIdx--
			}
			return nil
		}
		// Navigate task dropdown (now focus 3)
		if p.createFocus == 3 && len(p.taskSearchFiltered) > 0 {
			if p.taskSearchIdx > 0 {
				p.taskSearchIdx--
			}
			return nil
		}
		// Navigate agent selection (now focus 4)
		if p.createFocus == 4 {
			p.cycleAgentType(false)
			return nil
		}
	case "down":
		// Navigate branch dropdown
		if p.createFocus == 1 && len(p.branchFiltered) > 0 {
			if p.branchIdx < len(p.branchFiltered)-1 {
				p.branchIdx++
			}
			return nil
		}
		// Navigate task dropdown (now focus 3)
		if p.createFocus == 3 && len(p.taskSearchFiltered) > 0 {
			if p.taskSearchIdx < len(p.taskSearchFiltered)-1 {
				p.taskSearchIdx++
			}
			return nil
		}
		// Navigate agent selection (now focus 4)
		if p.createFocus == 4 {
			p.cycleAgentType(true)
			return nil
		}
	case "enter":
		// Select branch from dropdown if in branch field
		if p.createFocus == 1 && len(p.branchFiltered) > 0 {
			selectedBranch := p.branchFiltered[p.branchIdx]
			p.createBaseBranchInput.SetValue(selectedBranch)
			p.createBaseBranchInput.Blur()
			p.createFocus = 2 // Move to prompt field
			p.focusCreateInput()
			return nil
		}
		// Open prompt picker if in prompt field
		if p.createFocus == 2 {
			p.promptPicker = NewPromptPicker(p.createPrompts, p.width, p.height)
			p.viewMode = ViewModePromptPicker
			return nil
		}
		// Select task from dropdown if in task field (now focus 3)
		if p.createFocus == 3 && len(p.taskSearchFiltered) > 0 {
			// Select task and move to next field
			selectedTask := p.taskSearchFiltered[p.taskSearchIdx]
			p.createTaskID = selectedTask.ID
			p.createTaskTitle = selectedTask.Title
			p.taskSearchInput.Blur()
			p.createFocus = 4 // Move to agent field
			return nil
		}
		// Create button (now focus 6)
		if p.createFocus == 6 {
			return p.createWorktree()
		}
		// Cancel button (now focus 7)
		if p.createFocus == 7 {
			p.viewMode = ViewModeList
			p.clearCreateModal()
			return nil
		}
		// From input fields (0-1), move to next field
		if p.createFocus < 2 {
			p.blurCreateInputs()
			p.createFocus++
			p.focusCreateInput()
		}
		return nil
	}

	// Delegate to focused textinput for all other keys
	// Clear error when user types (they're correcting the issue)
	p.createError = ""
	var cmd tea.Cmd
	switch p.createFocus {
	case 0:
		p.createNameInput, cmd = p.createNameInput.Update(msg)
	case 1:
		p.createBaseBranchInput, cmd = p.createBaseBranchInput.Update(msg)
		// Update filtered branches on input change
		p.branchFiltered = filterBranches(p.createBaseBranchInput.Value(), p.branchAll)
		p.branchIdx = 0
	case 3:
		p.taskSearchInput, cmd = p.taskSearchInput.Update(msg)
		// Update filtered results on input change
		p.taskSearchFiltered = filterTasks(p.taskSearchInput.Value(), p.taskSearchAll)
		p.taskSearchIdx = 0
	}
	return cmd
}

// shouldShowSkipPermissions returns true if the current agent type supports skip permissions.
func (p *Plugin) shouldShowSkipPermissions() bool {
	if p.createAgentType == AgentNone {
		return false
	}
	flag := SkipPermissionsFlags[p.createAgentType]
	return flag != ""
}

// cycleAgentType cycles through agent types in the selection.
func (p *Plugin) cycleAgentType(forward bool) {
	currentIdx := 0
	for i, at := range AgentTypeOrder {
		if at == p.createAgentType {
			currentIdx = i
			break
		}
	}

	if forward {
		currentIdx = (currentIdx + 1) % len(AgentTypeOrder)
	} else {
		currentIdx = (currentIdx + len(AgentTypeOrder) - 1) % len(AgentTypeOrder)
	}

	p.createAgentType = AgentTypeOrder[currentIdx]
}

// blurCreateInputs blurs all create modal textinputs.
func (p *Plugin) blurCreateInputs() {
	p.createNameInput.Blur()
	p.createBaseBranchInput.Blur()
	p.taskSearchInput.Blur()
}

// focusCreateInput focuses the appropriate textinput based on createFocus.
func (p *Plugin) focusCreateInput() {
	switch p.createFocus {
	case 0:
		p.createNameInput.Focus()
	case 1:
		p.createBaseBranchInput.Focus()
	case 2:
		p.taskSearchInput.Focus()
	}
}

// handleTaskLinkKeys handles keys in task link modal.
func (p *Plugin) handleTaskLinkKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		p.viewMode = ViewModeList
		p.linkingWorktree = nil
		p.taskSearchInput = textinput.Model{}
		p.taskSearchAll = nil
		p.taskSearchFiltered = nil
		p.taskSearchIdx = 0
		return nil
	case "up":
		if len(p.taskSearchFiltered) > 0 && p.taskSearchIdx > 0 {
			p.taskSearchIdx--
		}
		return nil
	case "down":
		if len(p.taskSearchFiltered) > 0 && p.taskSearchIdx < len(p.taskSearchFiltered)-1 {
			p.taskSearchIdx++
		}
		return nil
	case "enter":
		if len(p.taskSearchFiltered) > 0 && p.linkingWorktree != nil {
			selectedTask := p.taskSearchFiltered[p.taskSearchIdx]
			wt := p.linkingWorktree
			p.viewMode = ViewModeList
			p.linkingWorktree = nil
			p.taskSearchInput = textinput.Model{}
			p.taskSearchAll = nil
			p.taskSearchFiltered = nil
			p.taskSearchIdx = 0
			return p.linkTask(wt, selectedTask.ID)
		}
		return nil
	}

	// Delegate to textinput for all other keys (typing, backspace, paste, etc.)
	var cmd tea.Cmd
	p.taskSearchInput, cmd = p.taskSearchInput.Update(msg)
	// Update filtered results on input change
	p.taskSearchFiltered = filterTasks(p.taskSearchInput.Value(), p.taskSearchAll)
	p.taskSearchIdx = 0
	return cmd
}

// handleMergeKeys handles keys in merge workflow modal.
func (p *Plugin) handleMergeKeys(msg tea.KeyMsg) tea.Cmd {
	if p.mergeState == nil {
		p.viewMode = ViewModeList
		return nil
	}

	switch msg.String() {
	case "esc", "q":
		p.cancelMergeWorkflow()
		return nil

	case "enter":
		// Continue to next step based on current step
		switch p.mergeState.Step {
		case MergeStepReviewDiff:
			// User reviewed diff, proceed to push
			return p.advanceMergeStep()
		case MergeStepWaitingMerge:
			// Manual check for merge status
			return p.checkPRMerged(p.mergeState.Worktree)
		case MergeStepDone:
			// Close modal
			p.cancelMergeWorkflow()
		}

	case "up", "k":
		// Select "Delete worktree after merge"
		if p.mergeState.Step == MergeStepWaitingMerge {
			p.mergeState.DeleteAfterMerge = true
		}

	case "down", "j":
		// Select "Keep worktree"
		if p.mergeState.Step == MergeStepWaitingMerge {
			p.mergeState.DeleteAfterMerge = false
		}

	case "s":
		// Skip current step (for pushing, creating PR)
		switch p.mergeState.Step {
		case MergeStepReviewDiff:
			// Skip push step if already pushed
			p.mergeState.StepStatus[MergeStepReviewDiff] = "done"
			p.mergeState.Step = MergeStepPush
			return p.advanceMergeStep()
		}
	}
	return nil
}

// handleCommitForMergeKeys handles keys in the commit-before-merge modal.
func (p *Plugin) handleCommitForMergeKeys(msg tea.KeyMsg) tea.Cmd {
	if p.mergeCommitState == nil {
		p.viewMode = ViewModeList
		return nil
	}

	switch msg.String() {
	case "esc":
		// Cancel - return to list
		p.mergeCommitState = nil
		p.mergeCommitMessageInput = textinput.Model{}
		p.viewMode = ViewModeList
		return nil

	case "enter":
		// Commit and continue
		message := p.mergeCommitMessageInput.Value()
		if message == "" {
			p.mergeCommitState.Error = "Commit message cannot be empty"
			return nil
		}
		p.mergeCommitState.Error = ""
		return p.stageAllAndCommit(p.mergeCommitState.Worktree, message)
	}

	// Delegate to textinput for all other keys
	p.mergeCommitState.Error = "" // Clear error when user types
	var cmd tea.Cmd
	p.mergeCommitMessageInput, cmd = p.mergeCommitMessageInput.Update(msg)
	return cmd
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
	switch p.previewTab {
	case PreviewTabDiff:
		return p.loadSelectedDiff()
	case PreviewTabTask:
		return p.loadTaskDetailsIfNeeded()
	}
	return nil
}

// loadSelectedContent loads content based on the active preview tab.
// Always loads diff (for preloading), and also loads task if task tab is active.
func (p *Plugin) loadSelectedContent() tea.Cmd {
	switch p.previewTab {
	case PreviewTabTask:
		return tea.Batch(p.loadSelectedDiff(), p.loadTaskDetailsIfNeeded())
	default:
		return p.loadSelectedDiff()
	}
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

// handleMouse processes mouse input.
func (p *Plugin) handleMouse(msg tea.MouseMsg) tea.Cmd {
	action := p.mouseHandler.HandleMouse(msg)

	switch action.Type {
	case mouse.ActionClick:
		return p.handleMouseClick(action)
	case mouse.ActionDoubleClick:
		return p.handleMouseDoubleClick(action)
	case mouse.ActionScrollUp, mouse.ActionScrollDown:
		return p.handleMouseScroll(action)
	case mouse.ActionScrollLeft, mouse.ActionScrollRight:
		return p.handleMouseHorizontalScroll(action)
	case mouse.ActionDrag:
		return p.handleMouseDrag(action)
	case mouse.ActionDragEnd:
		return p.handleMouseDragEnd()
	case mouse.ActionHover:
		return p.handleMouseHover(action)
	}
	return nil
}

// handleMouseHover handles hover events for visual feedback.
func (p *Plugin) handleMouseHover(action mouse.MouseAction) tea.Cmd {
	// Handle hover in modals that have button hover states
	switch p.viewMode {
	case ViewModeCreate:
		if action.Region == nil {
			p.createButtonHover = 0
			return nil
		}
		switch action.Region.ID {
		case regionCreateButton:
			if idx, ok := action.Region.Data.(int); ok {
				if idx == 6 {
					p.createButtonHover = 1 // Create
				} else if idx == 7 {
					p.createButtonHover = 2 // Cancel
				}
			}
		default:
			p.createButtonHover = 0
		}
	case ViewModeAgentChoice:
		if action.Region == nil {
			p.agentChoiceButtonHover = 0
			return nil
		}
		switch action.Region.ID {
		case regionAgentChoiceConfirm:
			p.agentChoiceButtonHover = 1
		case regionAgentChoiceCancel:
			p.agentChoiceButtonHover = 2
		default:
			p.agentChoiceButtonHover = 0
		}
	case ViewModeConfirmDelete:
		if action.Region == nil {
			p.deleteConfirmButtonHover = 0
			return nil
		}
		switch action.Region.ID {
		case regionDeleteConfirmDelete:
			p.deleteConfirmButtonHover = 1
		case regionDeleteConfirmCancel:
			p.deleteConfirmButtonHover = 2
		default:
			p.deleteConfirmButtonHover = 0
		}
	default:
		p.createButtonHover = 0
		p.agentChoiceButtonHover = 0
		p.deleteConfirmButtonHover = 0
	}
	return nil
}

// handleMouseClick handles single click events.
func (p *Plugin) handleMouseClick(action mouse.MouseAction) tea.Cmd {
	if action.Region == nil {
		return nil
	}

	switch action.Region.ID {
	case regionSidebar:
		p.activePane = PaneSidebar
	case regionPreviewPane:
		p.activePane = PanePreview
	case regionPaneDivider:
		// Start drag for pane resizing
		p.mouseHandler.StartDrag(action.X, action.Y, regionPaneDivider, p.sidebarWidth)
	case regionWorktreeItem:
		// Click on worktree - select it
		if idx, ok := action.Region.Data.(int); ok && idx >= 0 && idx < len(p.worktrees) {
			if p.selectedIdx != idx {
				p.selectedIdx = idx
				p.previewOffset = 0
				p.previewHorizOffset = 0
				p.autoScrollOutput = true
			}
			p.ensureVisible()
			p.activePane = PaneSidebar
			return p.loadSelectedContent()
		}
	case regionPreviewTab:
		// Click on preview tab
		if idx, ok := action.Region.Data.(int); ok && idx >= 0 && idx <= 2 {
			p.previewTab = PreviewTab(idx)
			p.previewOffset = 0
			p.previewHorizOffset = 0
			p.autoScrollOutput = true

			// Load content for the selected tab
			switch p.previewTab {
			case PreviewTabDiff:
				return p.loadSelectedDiff()
			case PreviewTabTask:
				return p.loadTaskDetailsIfNeeded()
			}
		}
	case regionAgentChoiceOption:
		// Click on agent choice option
		if idx, ok := action.Region.Data.(int); ok && idx >= 0 && idx <= 1 {
			p.agentChoiceIdx = idx
			p.agentChoiceButtonFocus = 0
		}
	case regionAgentChoiceConfirm:
		// Click confirm button
		return p.executeAgentChoice()
	case regionAgentChoiceCancel:
		// Click cancel button
		p.viewMode = ViewModeList
		p.agentChoiceWorktree = nil
		p.agentChoiceButtonFocus = 0
	case regionDeleteConfirmDelete:
		// Click delete button
		return p.executeDelete()
	case regionDeleteConfirmCancel:
		// Click cancel button
		return p.cancelDelete()
	case regionKanbanCard:
		// Click on kanban card - select it
		if data, ok := action.Region.Data.(kanbanCardData); ok {
			p.kanbanCol = data.col
			p.kanbanRow = data.row
			p.syncKanbanToList()
			return p.loadSelectedContent()
		}
	case regionKanbanColumn:
		// Click on column header - focus that column
		if colIdx, ok := action.Region.Data.(int); ok {
			p.kanbanCol = colIdx
			p.kanbanRow = 0
			p.syncKanbanToList()
		}
	case regionViewToggle:
		// Click on view toggle - switch views
		if idx, ok := action.Region.Data.(int); ok {
			if idx == 0 {
				p.viewMode = ViewModeList
			} else {
				p.viewMode = ViewModeKanban
				p.syncListToKanban()
			}
		}
	case regionCreateInput:
		// Click on input field in create modal
		if focusIdx, ok := action.Region.Data.(int); ok {
			p.blurCreateInputs()
			p.createFocus = focusIdx
			p.focusCreateInput()
		}
	case regionCreateDropdown:
		// Click on dropdown item
		if data, ok := action.Region.Data.(dropdownItemData); ok {
			if data.field == 1 {
				// Branch selection
				if data.idx >= 0 && data.idx < len(p.branchFiltered) {
					p.createBaseBranchInput.SetValue(p.branchFiltered[data.idx])
					p.branchFiltered = nil
				}
			} else if data.field == 3 {
				// Task selection
				if data.idx >= 0 && data.idx < len(p.taskSearchFiltered) {
					task := p.taskSearchFiltered[data.idx]
					p.createTaskID = task.ID
					p.createTaskTitle = task.Title
					p.taskSearchFiltered = nil
				}
			}
		}
	case regionCreateAgentOption:
		// Click on agent option
		if idx, ok := action.Region.Data.(int); ok {
			if idx >= 0 && idx < len(AgentTypeOrder) {
				p.createAgentType = AgentTypeOrder[idx]
			}
		}
	case regionCreateCheckbox:
		// Toggle checkbox
		p.createSkipPermissions = !p.createSkipPermissions
	case regionCreateButton:
		// Click on button
		if idx, ok := action.Region.Data.(int); ok {
			if idx == 6 {
				return p.createWorktree()
			} else if idx == 7 {
				p.viewMode = ViewModeList
				p.clearCreateModal()
			}
		}
	case regionTaskLinkDropdown:
		// Click on task link dropdown item
		if idx, ok := action.Region.Data.(int); ok {
			if idx >= 0 && idx < len(p.taskSearchFiltered) && p.linkingWorktree != nil {
				task := p.taskSearchFiltered[idx]
				wt := p.linkingWorktree
				p.viewMode = ViewModeList
				p.linkingWorktree = nil
				return p.linkTask(wt, task.ID)
			}
		}
	case regionMergeRadio:
		// Click on merge radio option (0=delete, 1=keep)
		if idx, ok := action.Region.Data.(int); ok && p.mergeState != nil {
			p.mergeState.DeleteAfterMerge = (idx == 0)
		}
	case regionPromptItem:
		// Click on prompt item in picker - select it
		if idx, ok := action.Region.Data.(int); ok && p.promptPicker != nil {
			// idx -1 means "none" option, >= 0 means filtered prompts
			p.promptPicker.selectedIdx = idx
			// Trigger selection
			if idx < 0 {
				// "None" selected
				return func() tea.Msg { return PromptSelectedMsg{Prompt: nil} }
			}
			if idx < len(p.promptPicker.filtered) {
				prompt := p.promptPicker.filtered[idx]
				return func() tea.Msg { return PromptSelectedMsg{Prompt: &prompt} }
			}
		}
	}
	return nil
}

// handleMouseDoubleClick handles double-click events.
func (p *Plugin) handleMouseDoubleClick(action mouse.MouseAction) tea.Cmd {
	if action.Region == nil {
		return nil
	}

	switch action.Region.ID {
	case regionWorktreeItem:
		// Double-click on worktree - attach to tmux session if agent running
		if idx, ok := action.Region.Data.(int); ok && idx >= 0 && idx < len(p.worktrees) {
			p.selectedIdx = idx
			wt := p.worktrees[idx]
			if wt.Agent != nil {
				p.attachedSession = wt.Name
				return p.AttachToSession(wt)
			}
			p.activePane = PanePreview
		}
	case regionKanbanCard:
		// Double-click on kanban card - attach to tmux session if agent running
		if data, ok := action.Region.Data.(kanbanCardData); ok {
			p.kanbanCol = data.col
			p.kanbanRow = data.row
			p.syncKanbanToList()
			wt := p.getKanbanWorktree(data.col, data.row)
			if wt != nil && wt.Agent != nil {
				p.attachedSession = wt.Name
				return p.AttachToSession(wt)
			}
		}
	}
	return nil
}

// handleMouseScroll handles scroll wheel events.
func (p *Plugin) handleMouseScroll(action mouse.MouseAction) tea.Cmd {
	delta := action.Delta
	if action.Type == mouse.ActionScrollUp {
		delta = -1
	} else {
		delta = 1
	}

	// Determine which pane based on region or position
	regionID := ""
	if action.Region != nil {
		regionID = action.Region.ID
	}

	switch regionID {
	case regionSidebar, regionWorktreeItem:
		return p.scrollSidebar(delta)
	case regionPreviewPane:
		return p.scrollPreview(delta)
	case regionKanbanCard, regionKanbanColumn:
		// Scroll within Kanban view - navigate rows in current column
		return p.scrollKanban(delta)
	default:
		// Fallback based on X position and view mode
		if p.viewMode == ViewModeKanban {
			return p.scrollKanban(delta)
		}
		sidebarW := (p.width * p.sidebarWidth) / 100
		if action.X < sidebarW {
			return p.scrollSidebar(delta)
		}
		return p.scrollPreview(delta)
	}
}

// handleMouseHorizontalScroll handles horizontal scroll events in the preview pane.
func (p *Plugin) handleMouseHorizontalScroll(action mouse.MouseAction) tea.Cmd {
	// Only horizontal scroll in preview pane
	if action.Region == nil {
		// No hit region - use X position to determine if in preview pane
		sidebarW := (p.width * p.sidebarWidth) / 100
		if action.X >= sidebarW+dividerWidth {
			return p.scrollPreviewHorizontal(action.Delta)
		}
		return nil
	}

	switch action.Region.ID {
	case regionPreviewPane:
		return p.scrollPreviewHorizontal(action.Delta)
	}

	return nil
}

// scrollPreviewHorizontal scrolls the preview pane horizontally.
func (p *Plugin) scrollPreviewHorizontal(delta int) tea.Cmd {
	p.previewHorizOffset += delta
	if p.previewHorizOffset < 0 {
		p.previewHorizOffset = 0
	}
	return nil
}

// scrollSidebar scrolls the sidebar worktree list.
func (p *Plugin) scrollSidebar(delta int) tea.Cmd {
	if len(p.worktrees) == 0 {
		return nil
	}

	newCursor := p.selectedIdx + delta
	if newCursor < 0 {
		newCursor = 0
	}
	if newCursor >= len(p.worktrees) {
		newCursor = len(p.worktrees) - 1
	}

	if newCursor != p.selectedIdx {
		p.selectedIdx = newCursor
		p.ensureVisible()
		return p.loadSelectedDiff()
	}
	return nil
}

// scrollPreview scrolls the preview pane content.
func (p *Plugin) scrollPreview(delta int) tea.Cmd {
	// For output tab with auto-scroll, handle scroll direction correctly:
	// - Scroll UP (delta < 0): show older content (increase offset from bottom)
	// - Scroll DOWN (delta > 0): show newer content (decrease offset from bottom)
	if p.previewTab == PreviewTabOutput {
		if delta < 0 {
			// Scroll UP: pause auto-scroll, show older content
			p.autoScrollOutput = false
			p.previewOffset++
		} else {
			// Scroll DOWN: show newer content
			if p.previewOffset > 0 {
				p.previewOffset--
				if p.previewOffset == 0 {
					p.autoScrollOutput = true // Resume auto-scroll when at bottom
				}
			}
		}
	} else {
		// For other tabs (diff, task), use simple offset
		p.previewOffset += delta
		if p.previewOffset < 0 {
			p.previewOffset = 0
		}
	}
	return nil
}

// scrollKanban scrolls within the current Kanban column.
func (p *Plugin) scrollKanban(delta int) tea.Cmd {
	columns := p.getKanbanColumns()
	if p.kanbanCol < 0 || p.kanbanCol >= len(kanbanColumnOrder) {
		return nil
	}
	status := kanbanColumnOrder[p.kanbanCol]
	items := columns[status]

	if len(items) == 0 {
		return nil
	}

	newRow := p.kanbanRow + delta
	if newRow < 0 {
		newRow = 0
	}
	maxRow := len(items) - 1
	if newRow > maxRow {
		newRow = maxRow
	}

	if newRow != p.kanbanRow {
		p.kanbanRow = newRow
		p.syncKanbanToList()
		return p.loadSelectedContent()
	}
	return nil
}

// handleMouseDrag handles drag motion events.
func (p *Plugin) handleMouseDrag(action mouse.MouseAction) tea.Cmd {
	if p.mouseHandler.DragRegion() == regionPaneDivider {
		// Calculate new sidebar width based on drag
		startValue := p.mouseHandler.DragStartValue()
		newWidth := startValue + (action.DragDX * 100 / p.width) // Convert px delta to %

		// Clamp to reasonable bounds (20% - 60%)
		if newWidth < 20 {
			newWidth = 20
		}
		if newWidth > 60 {
			newWidth = 60
		}
		p.sidebarWidth = newWidth
	}
	return nil
}

// handleMouseDragEnd handles the end of a drag operation.
func (p *Plugin) handleMouseDragEnd() tea.Cmd {
	// Persist sidebar width
	_ = state.SetWorktreeSidebarWidth(p.sidebarWidth)
	return nil
}

// Commands returns the available commands.
func (p *Plugin) Commands() []plugin.Command {
	switch p.viewMode {
	case ViewModeCreate:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel worktree creation", Context: "worktree-create", Priority: 1},
			{ID: "confirm", Name: "Create", Description: "Create the worktree", Context: "worktree-create", Priority: 2},
		}
	case ViewModeTaskLink:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel task linking", Context: "worktree-task-link", Priority: 1},
			{ID: "select-task", Name: "Select", Description: "Link selected task", Context: "worktree-task-link", Priority: 2},
		}
	case ViewModeMerge:
		cmds := []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel merge workflow", Context: "worktree-merge", Priority: 1},
		}
		if p.mergeState != nil {
			switch p.mergeState.Step {
			case MergeStepReviewDiff:
				cmds = append(cmds, plugin.Command{ID: "continue", Name: "Push", Description: "Push branch", Context: "worktree-merge", Priority: 2})
			case MergeStepWaitingMerge:
				cmds = append(cmds, plugin.Command{ID: "continue", Name: "Check", Description: "Check merge status", Context: "worktree-merge", Priority: 2})
			case MergeStepDone:
				cmds = append(cmds, plugin.Command{ID: "continue", Name: "Done", Description: "Close modal", Context: "worktree-merge", Priority: 2})
			}
		}
		return cmds
	case ViewModeAgentChoice:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel agent choice", Context: "worktree-agent-choice", Priority: 1},
			{ID: "select", Name: "Select", Description: "Choose selected option", Context: "worktree-agent-choice", Priority: 2},
		}
	case ViewModeConfirmDelete:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel deletion", Context: "worktree-confirm-delete", Priority: 1},
			{ID: "delete", Name: "Delete", Description: "Confirm deletion", Context: "worktree-confirm-delete", Priority: 2},
		}
	case ViewModeCommitForMerge:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel merge", Context: "worktree-commit-for-merge", Priority: 1},
			{ID: "commit", Name: "Commit", Description: "Commit and continue", Context: "worktree-commit-for-merge", Priority: 2},
		}
	default:
		// View toggle label changes based on current mode
		viewToggleName := "Kanban"
		if p.viewMode == ViewModeKanban {
			viewToggleName = "List"
		}

		// Return different commands based on active pane
		if p.activePane == PanePreview {
			// Preview pane commands
			cmds := []plugin.Command{
				{ID: "switch-pane", Name: "Focus", Description: "Switch to sidebar", Context: "worktree-preview", Priority: 1},
				{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Context: "worktree-preview", Priority: 2},
				{ID: "prev-tab", Name: "Tab←", Description: "Previous preview tab", Context: "worktree-preview", Priority: 3},
				{ID: "next-tab", Name: "Tab→", Description: "Next preview tab", Context: "worktree-preview", Priority: 4},
			}
			// Add diff view toggle when on Diff tab
			if p.previewTab == PreviewTabDiff {
				diffViewName := "Split"
				if p.diffViewMode == DiffViewSideBySide {
					diffViewName = "Unified"
				}
				cmds = append(cmds, plugin.Command{ID: "toggle-diff-view", Name: diffViewName, Description: "Toggle unified/side-by-side diff", Context: "worktree-preview", Priority: 5})
			}
			// Also show agent commands in preview pane
			wt := p.selectedWorktree()
			if wt != nil {
				if wt.Agent == nil {
					cmds = append(cmds,
						plugin.Command{ID: "start-agent", Name: "Start", Description: "Start agent", Context: "worktree-preview", Priority: 10},
					)
				} else {
					cmds = append(cmds,
						plugin.Command{ID: "start-agent", Name: "Agent", Description: "Agent options (attach/restart)", Context: "worktree-preview", Priority: 9},
						plugin.Command{ID: "attach", Name: "Attach", Description: "Attach to session", Context: "worktree-preview", Priority: 10},
						plugin.Command{ID: "stop-agent", Name: "Stop", Description: "Stop agent", Context: "worktree-preview", Priority: 11},
					)
					if wt.Status == StatusWaiting {
						cmds = append(cmds,
							plugin.Command{ID: "approve", Name: "Approve", Description: "Approve agent prompt", Context: "worktree-preview", Priority: 12},
							plugin.Command{ID: "reject", Name: "Reject", Description: "Reject agent prompt", Context: "worktree-preview", Priority: 13},
						)
					}
				}
			}
			return cmds
		}

		// Sidebar list commands - reorganized with unique priorities
		// Priority 1-4: Base commands (always visible)
		// Priority 5-8: Worktree-specific commands
		// Priority 10-14: Agent commands (highest visibility when applicable)
		cmds := []plugin.Command{
			{ID: "new-worktree", Name: "New", Description: "Create new worktree", Context: "worktree-list", Priority: 1},
			{ID: "toggle-view", Name: viewToggleName, Description: "Toggle list/kanban view", Context: "worktree-list", Priority: 2},
			{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Context: "worktree-list", Priority: 3},
			{ID: "refresh", Name: "Refresh", Description: "Refresh worktree list", Context: "worktree-list", Priority: 4},
		}
		wt := p.selectedWorktree()
		if wt != nil {
			// Agent commands first (most context-dependent, highest visibility)
			if wt.Agent == nil {
				cmds = append(cmds,
					plugin.Command{ID: "start-agent", Name: "Start", Description: "Start agent", Context: "worktree-list", Priority: 10},
				)
			} else {
				cmds = append(cmds,
					plugin.Command{ID: "start-agent", Name: "Agent", Description: "Agent options (attach/restart)", Context: "worktree-list", Priority: 9},
					plugin.Command{ID: "attach", Name: "Attach", Description: "Attach to session", Context: "worktree-list", Priority: 10},
					plugin.Command{ID: "stop-agent", Name: "Stop", Description: "Stop agent", Context: "worktree-list", Priority: 11},
				)
				if wt.Status == StatusWaiting {
					cmds = append(cmds,
						plugin.Command{ID: "approve", Name: "Approve", Description: "Approve agent prompt", Context: "worktree-list", Priority: 12},
						plugin.Command{ID: "reject", Name: "Reject", Description: "Reject agent prompt", Context: "worktree-list", Priority: 13},
						plugin.Command{ID: "approve-all", Name: "Approve All", Description: "Approve all agent prompts", Context: "worktree-list", Priority: 14},
					)
				}
			}
			// Worktree commands
			cmds = append(cmds,
				plugin.Command{ID: "delete-worktree", Name: "Delete", Description: "Delete selected worktree", Context: "worktree-list", Priority: 5},
				plugin.Command{ID: "push", Name: "Push", Description: "Push branch to remote", Context: "worktree-list", Priority: 6},
				plugin.Command{ID: "merge-workflow", Name: "Merge", Description: "Start merge workflow", Context: "worktree-list", Priority: 7},
			)
			// Task linking
			if wt.TaskID != "" {
				cmds = append(cmds,
					plugin.Command{ID: "link-task", Name: "Unlink", Description: "Unlink task", Context: "worktree-list", Priority: 8},
				)
			} else {
				cmds = append(cmds,
					plugin.Command{ID: "link-task", Name: "Task", Description: "Link task", Context: "worktree-list", Priority: 8},
				)
			}
		}
		return cmds
	}
}

// FocusContext returns the current focus context for keybinding dispatch.
func (p *Plugin) FocusContext() string {
	switch p.viewMode {
	case ViewModeCreate:
		return "worktree-create"
	case ViewModeTaskLink:
		return "worktree-task-link"
	case ViewModeMerge:
		return "worktree-merge"
	case ViewModeAgentChoice:
		return "worktree-agent-choice"
	case ViewModeConfirmDelete:
		return "worktree-confirm-delete"
	case ViewModeCommitForMerge:
		return "worktree-commit-for-merge"
	case ViewModePromptPicker:
		return "worktree-prompt-picker"
	default:
		if p.activePane == PanePreview {
			return "worktree-preview"
		}
		return "worktree-list"
	}
}

// kanbanColumnOrder defines the order of columns in kanban view.
var kanbanColumnOrder = []WorktreeStatus{StatusActive, StatusWaiting, StatusDone, StatusPaused}

// getKanbanColumns returns worktrees grouped by status for kanban view.
// StatusError worktrees are grouped with StatusPaused since they require user intervention.
func (p *Plugin) getKanbanColumns() map[WorktreeStatus][]*Worktree {
	columns := map[WorktreeStatus][]*Worktree{
		StatusActive:  {},
		StatusWaiting: {},
		StatusDone:    {},
		StatusPaused:  {},
	}
	for _, wt := range p.worktrees {
		status := wt.Status
		// Group error worktrees with paused since they require user intervention
		if status == StatusError {
			status = StatusPaused
		}
		columns[status] = append(columns[status], wt)
	}
	return columns
}

// selectedKanbanWorktree returns the worktree at the current kanban position.
func (p *Plugin) selectedKanbanWorktree() *Worktree {
	columns := p.getKanbanColumns()
	if p.kanbanCol < 0 || p.kanbanCol >= len(kanbanColumnOrder) {
		return nil
	}
	status := kanbanColumnOrder[p.kanbanCol]
	items := columns[status]
	if p.kanbanRow < 0 || p.kanbanRow >= len(items) {
		return nil
	}
	return items[p.kanbanRow]
}

// syncKanbanToList syncs the kanban selection to the list selectedIdx.
func (p *Plugin) syncKanbanToList() {
	wt := p.selectedKanbanWorktree()
	if wt == nil {
		return
	}
	for i, w := range p.worktrees {
		if w.Name == wt.Name {
			p.selectedIdx = i
			return
		}
	}
}

// moveKanbanColumn moves selection to an adjacent column.
func (p *Plugin) moveKanbanColumn(delta int) {
	columns := p.getKanbanColumns()
	newCol := p.kanbanCol + delta

	// Wrap around or clamp
	if newCol < 0 {
		newCol = 0
	}
	if newCol >= len(kanbanColumnOrder) {
		newCol = len(kanbanColumnOrder) - 1
	}

	if newCol != p.kanbanCol {
		p.kanbanCol = newCol
		// Try to preserve row position, but clamp to new column's item count
		status := kanbanColumnOrder[p.kanbanCol]
		items := columns[status]
		if len(items) == 0 {
			p.kanbanRow = 0
		} else if p.kanbanRow >= len(items) {
			p.kanbanRow = len(items) - 1
		}
		p.syncKanbanToList()
	}
}

// moveKanbanRow moves selection within the current column.
func (p *Plugin) moveKanbanRow(delta int) {
	columns := p.getKanbanColumns()
	status := kanbanColumnOrder[p.kanbanCol]
	items := columns[status]

	if len(items) == 0 {
		return
	}

	newRow := p.kanbanRow + delta
	if newRow < 0 {
		newRow = 0
	}
	if newRow >= len(items) {
		newRow = len(items) - 1
	}

	if newRow != p.kanbanRow {
		p.kanbanRow = newRow
		p.syncKanbanToList()
	}
}

// getKanbanWorktree returns the worktree at the given Kanban coordinates.
func (p *Plugin) getKanbanWorktree(col, row int) *Worktree {
	columns := p.getKanbanColumns()
	if col < 0 || col >= len(kanbanColumnOrder) {
		return nil
	}
	status := kanbanColumnOrder[col]
	items := columns[status]
	if row >= 0 && row < len(items) {
		return items[row]
	}
	return nil
}

// syncListToKanban syncs the list selectedIdx to kanban position.
// Called when switching from list to kanban view.
func (p *Plugin) syncListToKanban() {
	wt := p.selectedWorktree()
	if wt == nil {
		p.kanbanCol = 0
		p.kanbanRow = 0
		return
	}

	columns := p.getKanbanColumns()
	for colIdx, status := range kanbanColumnOrder {
		items := columns[status]
		for rowIdx, item := range items {
			if item.Name == wt.Name {
				p.kanbanCol = colIdx
				p.kanbanRow = rowIdx
				return
			}
		}
	}

	// Worktree not found in any column, default to first column
	p.kanbanCol = 0
	p.kanbanRow = 0
}
