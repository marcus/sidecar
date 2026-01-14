package worktree

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
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
	regionSidebar       = "sidebar"
	regionPreviewPane   = "preview-pane"
	regionPaneDivider   = "pane-divider"
	regionWorktreeItem  = "worktree-item"
	regionViewModeTab   = "view-mode-tab"
	regionPreviewTab    = "preview-tab"
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
	viewMode      ViewMode
	activePane    FocusPane
	previewTab    PreviewTab
	selectedIdx   int
	scrollOffset  int // Sidebar list scroll offset
	visibleCount  int // Number of visible list items
	previewOffset int
	sidebarWidth  int // Persisted sidebar width

	// Agent state
	attachedSession string // Name of worktree we're attached to (pauses polling)

	// Mouse support
	mouseHandler *mouse.Handler

	// Async state
	refreshing  bool
	lastRefresh time.Time

	// Diff state
	diffContent string
	diffRaw     string

	// Create modal state
	createName       string
	createBaseBranch string
	createTaskID     string
	createFocus      int // 0=name, 1=base, 2=task, 3=confirm
}

// New creates a new worktree manager plugin.
func New() *Plugin {
	return &Plugin{
		worktrees:       make([]*Worktree, 0),
		agents:          make(map[string]*Agent),
		managedSessions: make(map[string]bool),
		viewMode:        ViewModeList,
		activePane:      PaneSidebar,
		previewTab:      PreviewTabOutput,
		mouseHandler:    mouse.NewHandler(),
		sidebarWidth:    40, // Default 40% sidebar
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

		// Agent control bindings
		ctx.Keymap.RegisterPluginBinding("s", "start-agent", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("S", "stop-agent", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("y", "approve", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("Y", "approve-all", "worktree-list")
		ctx.Keymap.RegisterPluginBinding("N", "reject", "worktree-list")

		// Preview pane context
		ctx.Keymap.RegisterPluginBinding("h", "focus-left", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("left", "focus-left", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("esc", "focus-left", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("tab", "next-tab", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-tab", "worktree-preview")
		ctx.Keymap.RegisterPluginBinding("\\", "toggle-sidebar", "worktree-preview")

		// Create modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "worktree-create")
		ctx.Keymap.RegisterPluginBinding("enter", "confirm", "worktree-create")
		ctx.Keymap.RegisterPluginBinding("tab", "next-field", "worktree-create")
		ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-field", "worktree-create")
	}

	return nil
}

// Start begins async operations.
func (p *Plugin) Start() tea.Cmd {
	return tea.Batch(
		p.refreshWorktrees(),
		p.reconnectAgents(),
	)
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
			// Load stats for each worktree
			for _, wt := range p.worktrees {
				cmds = append(cmds, p.loadStats(wt.Path))
			}
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
		p.viewMode = ViewModeList
		if msg.Err == nil {
			p.worktrees = append(p.worktrees, msg.Worktree)
			p.selectedIdx = len(p.worktrees) - 1
		}
		p.clearCreateModal()

	case DeleteDoneMsg:
		if msg.Err == nil {
			p.removeWorktreeByName(msg.Name)
			if p.selectedIdx >= len(p.worktrees) && p.selectedIdx > 0 {
				p.selectedIdx--
			}
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
		// Update state (safe - we're in Update)
		if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil {
			wt.Agent.OutputBuf.Write(msg.Output)
			wt.Agent.LastOutput = time.Now()
			wt.Agent.WaitingFor = msg.WaitingFor
			wt.Status = msg.Status
		}
		// Schedule next poll (1 second interval)
		return p, p.scheduleAgentPoll(msg.WorktreeName, 1*time.Second)

	case AgentStoppedMsg:
		if wt := p.findWorktree(msg.WorktreeName); wt != nil {
			wt.Agent = nil
			wt.Status = StatusPaused
		}
		delete(p.agents, msg.WorktreeName)
		return p, nil

	case TmuxAttachFinishedMsg:
		// Clear attached state
		p.attachedSession = ""

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
	p.createName = ""
	p.createBaseBranch = ""
	p.createTaskID = ""
	p.createFocus = 0
}

// handleKeyPress processes key input based on current view mode.
func (p *Plugin) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	switch p.viewMode {
	case ViewModeList, ViewModeKanban:
		return p.handleListKeys(msg)
	case ViewModeCreate:
		return p.handleCreateKeys(msg)
	}
	return nil
}

// handleListKeys handles keys in list view (and kanban view).
func (p *Plugin) handleListKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if p.activePane == PaneSidebar {
			p.moveCursor(1)
			return p.loadSelectedDiff()
		}
		p.previewOffset++
	case "k", "up":
		if p.activePane == PaneSidebar {
			p.moveCursor(-1)
			return p.loadSelectedDiff()
		}
		if p.previewOffset > 0 {
			p.previewOffset--
		}
	case "g":
		if p.activePane == PaneSidebar {
			p.selectedIdx = 0
			p.scrollOffset = 0
			return p.loadSelectedDiff()
		}
		p.previewOffset = 0
	case "G":
		if p.activePane == PaneSidebar {
			p.selectedIdx = len(p.worktrees) - 1
			p.ensureVisible()
			return p.loadSelectedDiff()
		}
	case "n":
		p.viewMode = ViewModeCreate
	case "D":
		return p.deleteSelected()
	case "p":
		return p.pushSelected()
	case "l", "right":
		if p.activePane == PaneSidebar {
			p.activePane = PanePreview
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
	case "h", "left", "esc":
		if p.activePane == PanePreview {
			p.activePane = PaneSidebar
		}
	case "tab":
		p.cyclePreviewTab(1)
	case "shift+tab":
		p.cyclePreviewTab(-1)
	case "r":
		return func() tea.Msg { return RefreshMsg{} }
	case "v":
		// Toggle between list and kanban view
		if p.viewMode == ViewModeList {
			p.viewMode = ViewModeKanban
		} else if p.viewMode == ViewModeKanban {
			p.viewMode = ViewModeList
		}

	// Agent control keys
	case "s":
		// Start agent on selected worktree
		wt := p.selectedWorktree()
		if wt != nil && wt.Agent == nil {
			return p.StartAgent(wt, AgentClaude)
		}
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
	}
	return nil
}

// handleCreateKeys handles keys in create modal.
func (p *Plugin) handleCreateKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		p.viewMode = ViewModeList
		p.clearCreateModal()
	case "tab":
		p.createFocus = (p.createFocus + 1) % 4
	case "shift+tab":
		p.createFocus = (p.createFocus + 3) % 4
	case "enter":
		if p.createFocus == 3 {
			return p.createWorktree()
		}
		p.createFocus = (p.createFocus + 1) % 4
	case "backspace":
		switch p.createFocus {
		case 0:
			if len(p.createName) > 0 {
				p.createName = p.createName[:len(p.createName)-1]
			}
		case 1:
			if len(p.createBaseBranch) > 0 {
				p.createBaseBranch = p.createBaseBranch[:len(p.createBaseBranch)-1]
			}
		case 2:
			if len(p.createTaskID) > 0 {
				p.createTaskID = p.createTaskID[:len(p.createTaskID)-1]
			}
		}
	default:
		if len(msg.String()) == 1 {
			switch p.createFocus {
			case 0:
				p.createName += msg.String()
			case 1:
				p.createBaseBranch += msg.String()
			case 2:
				p.createTaskID += msg.String()
			}
		}
	}
	return nil
}

// moveCursor moves the selection cursor.
func (p *Plugin) moveCursor(delta int) {
	p.selectedIdx += delta
	if p.selectedIdx < 0 {
		p.selectedIdx = 0
	}
	if p.selectedIdx >= len(p.worktrees) {
		p.selectedIdx = len(p.worktrees) - 1
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
func (p *Plugin) cyclePreviewTab(delta int) {
	p.previewTab = PreviewTab((int(p.previewTab) + delta + 3) % 3)
	p.previewOffset = 0
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
	case mouse.ActionDrag:
		return p.handleMouseDrag(action)
	case mouse.ActionDragEnd:
		return p.handleMouseDragEnd()
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
			p.selectedIdx = idx
			p.ensureVisible()
			p.activePane = PaneSidebar
			return p.loadSelectedDiff()
		}
	case regionViewModeTab:
		// Click on view mode toggle
		if p.viewMode == ViewModeList {
			p.viewMode = ViewModeKanban
		} else if p.viewMode == ViewModeKanban {
			p.viewMode = ViewModeList
		}
	case regionPreviewTab:
		// Click on preview tab
		if idx, ok := action.Region.Data.(int); ok && idx >= 0 && idx <= 2 {
			p.previewTab = PreviewTab(idx)
			p.previewOffset = 0
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
	default:
		// Fallback based on X position
		sidebarW := (p.width * p.sidebarWidth) / 100
		if action.X < sidebarW {
			return p.scrollSidebar(delta)
		}
		return p.scrollPreview(delta)
	}
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
	p.previewOffset += delta
	if p.previewOffset < 0 {
		p.previewOffset = 0
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
	// Could persist sidebar width to state here
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
	default:
		// View toggle label changes based on current mode
		viewToggleName := "Kanban"
		if p.viewMode == ViewModeKanban {
			viewToggleName = "List"
		}
		cmds := []plugin.Command{
			{ID: "new-worktree", Name: "New", Description: "Create new worktree", Context: "worktree-list", Priority: 1},
			{ID: "toggle-view", Name: viewToggleName, Description: "Toggle list/kanban view", Context: "worktree-list", Priority: 2},
			{ID: "refresh", Name: "Refresh", Description: "Refresh worktree list", Context: "worktree-list", Priority: 3},
		}
		wt := p.selectedWorktree()
		if wt != nil {
			cmds = append(cmds,
				plugin.Command{ID: "delete-worktree", Name: "Delete", Description: "Delete selected worktree", Context: "worktree-list", Priority: 4},
				plugin.Command{ID: "push", Name: "Push", Description: "Push branch to remote", Context: "worktree-list", Priority: 5},
			)
			// Agent commands
			if wt.Agent == nil {
				cmds = append(cmds,
					plugin.Command{ID: "start-agent", Name: "Start", Description: "Start agent", Context: "worktree-list", Priority: 6},
				)
			} else {
				cmds = append(cmds,
					plugin.Command{ID: "attach", Name: "Attach", Description: "Attach to session", Context: "worktree-list", Priority: 6},
					plugin.Command{ID: "stop-agent", Name: "Stop", Description: "Stop agent", Context: "worktree-list", Priority: 7},
				)
				if wt.Status == StatusWaiting {
					cmds = append(cmds,
						plugin.Command{ID: "approve", Name: "Approve", Description: "Approve prompt", Context: "worktree-list", Priority: 8},
						plugin.Command{ID: "reject", Name: "Reject", Description: "Reject prompt", Context: "worktree-list", Priority: 9},
					)
				}
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
	default:
		if p.activePane == PanePreview {
			return "worktree-preview"
		}
		return "worktree-list"
	}
}
