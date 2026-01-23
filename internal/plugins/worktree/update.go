package worktree

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	app "github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
)

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		if p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active {
			// Poll captures cursor atomically - no separate query needed
			return p, tea.Batch(p.resizeInteractivePaneCmd(), p.pollInteractivePaneImmediate())
		}
		// Resize selected pane in background so capture-pane output matches preview width
		return p, p.resizeSelectedPaneCmd()

	case app.PluginFocusedMsg:
		if p.focused {
			// Poll shell or selected agent when plugin gains focus
			if shell := p.getSelectedShell(); shell != nil {
				return p, p.pollShellSessionByName(shell.TmuxName)
			}
			return p, p.pollSelectedAgentNowIfVisible()
		}

	case RefreshMsg:
		if !p.refreshing {
			p.refreshing = true
			cmds = append(cmds, p.refreshWorktrees())
		}

	case RefreshDoneMsg:
		p.refreshing = false
		p.lastRefresh = time.Now()
		if msg.Err == nil {
			// Preserve selection by name (not index) across refresh
			var selectedName string
			if p.selectedIdx >= 0 && p.selectedIdx < len(p.worktrees) {
				selectedName = p.worktrees[p.selectedIdx].Name
			}

			p.worktrees = msg.Worktrees

			// Restore selection by finding the worktree with the same name
			if selectedName != "" {
				for i, wt := range p.worktrees {
					if wt.Name == selectedName {
						p.selectedIdx = i
						break
					}
				}
			}

			// On first refresh after startup/project-switch, restore saved selection
			if !p.stateRestored {
				p.stateRestored = true
				// Only restore if we don't already have a valid selection from above
				// and if there are items to select
				if selectedName == "" && (len(p.worktrees) > 0 || len(p.shells) > 0) {
					p.restoreSelectionState()
				}
			}

			// Bounds check in case the selected worktree was deleted
			if p.selectedIdx >= len(p.worktrees) && len(p.worktrees) > 0 {
				p.selectedIdx = len(p.worktrees) - 1
			}

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
			// Parse multi-file diff for file headers and navigation
			p.multiFileDiff = gitstatus.ParseMultiFileDiff(msg.Raw)
			// Also load commit status for this worktree
			// Reload if worktree changed OR if cached list is empty (stale/failed previous load)
			if p.commitStatusWorktree != msg.WorktreeName || len(p.commitStatusList) == 0 {
				cmds = append(cmds, p.loadCommitStatus(p.selectedWorktree()))
			}
		}

	case CommitStatusLoadedMsg:
		if msg.Err == nil && p.selectedWorktree() != nil && p.selectedWorktree().Name == msg.WorktreeName {
			p.commitStatusList = msg.Commits
			p.commitStatusWorktree = msg.WorktreeName
		}

	case CreateDoneMsg:
		if msg.Err != nil {
			p.createError = msg.Err.Error()
			// Stay in ViewModeCreate - don't close modal or clear state
		} else {
			p.viewMode = ViewModeList
			p.worktrees = append(p.worktrees, msg.Worktree)

			// Auto-focus newly created worktree (same pattern as click selection)
			p.shellSelected = false
			p.selectedIdx = len(p.worktrees) - 1
			p.previewOffset = 0
			p.previewHorizOffset = 0
			p.autoScrollOutput = true
			p.saveSelectionState()
			p.ensureVisible()

			p.clearCreateModal()

			// Load content for preview pane
			cmds = append(cmds, p.loadSelectedContent())

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
		if msg.Err != nil {
			p.deleteWarnings = []string{fmt.Sprintf("Delete failed: %v", msg.Err)}
			break
		}
		p.removeWorktreeByName(msg.Name)
		if p.selectedIdx >= len(p.worktrees) && p.selectedIdx > 0 {
			p.selectedIdx--
		}
		// Store any warnings for display
		p.deleteWarnings = msg.Warnings
		// Clear preview pane content to ensure old diff doesn't persist
		p.diffContent = ""
		p.diffRaw = ""
		p.cachedTaskID = ""
		p.cachedTask = nil
		// Load diff for newly selected worktree
		cmds = append(cmds, p.loadSelectedDiff())

	case RemoteCheckDoneMsg:
		// Update delete modal with remote branch existence info
		if p.viewMode == ViewModeConfirmDelete && p.deleteConfirmWorktree != nil &&
			p.deleteConfirmWorktree.Name == msg.WorktreeName {
			p.deleteHasRemote = msg.Exists
			// Adjust focus if remote exists (remote checkbox inserts at index 1,
			// so delete button shifts from 1 to 2)
			if msg.Exists && p.deleteConfirmFocus == 1 {
				p.deleteConfirmFocus = 2
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
				TmuxPane:    msg.PaneID, // Store pane ID for interactive mode
				StartedAt:   time.Now(),
				OutputBuf:   NewOutputBuffer(outputBufferCap),
			}

			if wt := p.findWorktree(msg.WorktreeName); wt != nil {
				wt.Agent = agent
				wt.Status = StatusActive
				wt.IsOrphaned = false
			}
			p.agents[msg.WorktreeName] = agent
			p.managedSessions[msg.SessionName] = true

			// Resize pane to match preview width immediately
			if cmd := p.resizeSelectedPaneCmd(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			// Start polling for output
			cmds = append(cmds, p.scheduleAgentPoll(msg.WorktreeName, 500*time.Millisecond))
		}

	case pollAgentMsg:
		// Timer leak prevention (td-83dc22): ignore stale poll messages.
		// If the worktree was removed or reset since this timer was scheduled,
		// the generation won't match and we drop the message.
		if currentGen := p.pollGeneration[msg.WorktreeName]; msg.Generation != currentGen {
			return p, nil // Stale timer, ignore
		}
		// Skip polling while user is attached to session
		if p.attachedSession == msg.WorktreeName {
			return p, nil
		}
		// Always poll for status updates (needed for sidebar indicators),
		// but use longer intervals when output isn't visible
		return p, p.handlePollAgent(msg.WorktreeName)

	case AgentOutputMsg:
		// Update state (content already stored by Update() in handlePollAgent)
		if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil {
			wt.Agent.LastOutput = time.Now()
			wt.Agent.WaitingFor = msg.WaitingFor
			wt.Status = msg.Status
			// Track poll time for runaway detection (td-018f25)
			wt.Agent.RecordPollTime()
		}
		// Update bracketed paste mode and cursor position if in interactive mode (td-79ab6163)
		if p.viewMode == ViewModeInteractive && !p.shellSelected {
			if wt := p.selectedWorktree(); wt != nil && wt.Name == msg.WorktreeName {
				p.updateBracketedPasteMode(msg.Output)
				p.updateMouseReportingMode(msg.Output)
				// Use cursor position captured atomically with output (no separate query needed)
				if msg.HasCursor && p.interactiveState != nil && p.interactiveState.Active {
					p.interactiveState.CursorRow = msg.CursorRow
					p.interactiveState.CursorCol = msg.CursorCol
					p.interactiveState.CursorVisible = msg.CursorVisible
					p.interactiveState.PaneHeight = msg.PaneHeight
					p.interactiveState.PaneWidth = msg.PaneWidth
				}
				if resizeCmd := p.maybeResizeInteractivePane(msg.PaneWidth, msg.PaneHeight); resizeCmd != nil {
					cmds = append(cmds, resizeCmd)
				}
			}
		}
		// Schedule next poll with adaptive interval based on status
		interval := pollIntervalActive
		switch msg.Status {
		case StatusWaiting:
			interval = pollIntervalWaiting
		case StatusDone, StatusError:
			interval = pollIntervalDone
		}
		// Check for runaway session and throttle if needed (td-018f25)
		if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil {
			if wt.Agent.CheckRunaway() {
				interval = pollIntervalThrottled
			}
		}
		// Three visibility states (same as shells):
		// 1. Visible + focused → fast polling
		// 2. Visible + unfocused → medium polling (2s)
		// 3. Not visible → slow polling (10-20s)
		isVisibleOnScreen := p.outputVisibleForUnfocused(msg.WorktreeName)
		if !isVisibleOnScreen {
			background := p.backgroundPollInterval()
			if background > interval {
				interval = background
			}
		} else if !p.focused {
			// Visible but plugin not focused - use medium interval
			if interval < pollIntervalVisibleUnfocused {
				interval = pollIntervalVisibleUnfocused
			}
		}
		// Use interactive polling in interactive mode for fast response
		if p.viewMode == ViewModeInteractive && !p.shellSelected {
			if wt := p.selectedWorktree(); wt != nil && wt.Name == msg.WorktreeName {
				cmds = append(cmds, p.pollInteractivePane())
				return p, tea.Batch(cmds...)
			}
		}
		cmds = append(cmds, p.scheduleAgentPoll(msg.WorktreeName, interval))
		return p, tea.Batch(cmds...)

	case AgentPollUnchangedMsg:
		// Track unchanged poll for throttle reset (td-018f25)
		if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil {
			wt.Agent.RecordUnchangedPoll()
		}
		// Content unchanged - use longer interval based on current status
		interval := pollIntervalIdle
		switch msg.CurrentStatus {
		case StatusWaiting:
			interval = pollIntervalWaiting
		case StatusDone, StatusError:
			interval = pollIntervalDone
		}
		// If still throttled, maintain throttle interval (td-018f25)
		if wt := p.findWorktree(msg.WorktreeName); wt != nil && wt.Agent != nil && wt.Agent.PollsThrottled {
			interval = pollIntervalThrottled
		}
		// Three visibility states (same as AgentOutputMsg)
		isVisibleOnScreen := p.outputVisibleForUnfocused(msg.WorktreeName)
		if !isVisibleOnScreen {
			background := p.backgroundPollInterval()
			if background > interval {
				interval = background
			}
		} else if !p.focused {
			// Visible but plugin not focused - use medium interval
			if interval < pollIntervalVisibleUnfocused {
				interval = pollIntervalVisibleUnfocused
			}
		}
		cmds = append(cmds, p.scheduleAgentPoll(msg.WorktreeName, interval))
		// Use cursor position captured atomically with output (no separate query needed)
		if p.viewMode == ViewModeInteractive && !p.shellSelected {
			if wt := p.selectedWorktree(); wt != nil && wt.Name == msg.WorktreeName {
				if msg.HasCursor && p.interactiveState != nil && p.interactiveState.Active {
					p.interactiveState.CursorRow = msg.CursorRow
					p.interactiveState.CursorCol = msg.CursorCol
					p.interactiveState.CursorVisible = msg.CursorVisible
					p.interactiveState.PaneHeight = msg.PaneHeight
					p.interactiveState.PaneWidth = msg.PaneWidth
				}
				if resizeCmd := p.maybeResizeInteractivePane(msg.PaneWidth, msg.PaneHeight); resizeCmd != nil {
					cmds = append(cmds, resizeCmd)
				}
			}
		}
		return p, tea.Batch(cmds...)

	// Shell session messages
	case ShellCreatedMsg:
		if msg.Err != nil {
			// Creation failed, show error toast
			return p, func() tea.Msg {
				return app.ToastMsg{Message: msg.Err.Error(), Duration: 5 * time.Second, IsError: true}
			}
		}
		// Create new shell session entry
		shell := &ShellSession{
			Name:     msg.DisplayName,
			TmuxName: msg.SessionName,
			Agent: &Agent{
				Type:        AgentShell,
				TmuxSession: msg.SessionName,
				TmuxPane:    msg.PaneID, // Store pane ID for interactive mode
				OutputBuf:   NewOutputBuffer(outputBufferCap),
				StartedAt:   time.Now(),
				Status:      AgentStatusRunning,
			},
			CreatedAt: time.Now(),
		}
		p.shells = append(p.shells, shell)
		p.managedSessions[msg.SessionName] = true

		// Auto-select and focus the new shell
		p.shellSelected = true
		p.selectedShellIdx = len(p.shells) - 1
		p.saveSelectionState()
		p.activePane = PaneSidebar
		p.autoScrollOutput = true

		// Resize pane to match preview width immediately
		if cmd := p.resizeSelectedPaneCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Start polling for output using stable TmuxName
		cmds = append(cmds, p.scheduleShellPollByName(shell.TmuxName, 500*time.Millisecond))

	case ShellDetachedMsg:
		// User detached from shell session - re-enable mouse and resume polling
		cmds = append(cmds, func() tea.Msg { return tea.EnableMouseAllMotion() })
		if shell := p.getSelectedShell(); shell != nil {
			cmds = append(cmds, p.scheduleShellPollByName(shell.TmuxName, 0)) // Immediate poll
		}

	case shellAttachAfterCreateMsg:
		// Attach to shell after it was created
		return p, p.attachToShellByIndex(msg.Index)

	case ShellKilledMsg:
		// Timer leak prevention (td-83dc22): increment generation to invalidate pending timers
		p.shellPollGeneration[msg.SessionName]++
		// Shell session killed, remove from list
		removedIdx := -1
		for i, shell := range p.shells {
			if shell.TmuxName == msg.SessionName {
				removedIdx = i
				// Clean up Agent resources
				if shell.Agent != nil {
					shell.Agent.OutputBuf = nil
					shell.Agent = nil
				}
				p.shells = append(p.shells[:i], p.shells[i+1:]...)
				delete(p.managedSessions, msg.SessionName)
				// Clean up pane cache and active registry (td-018f25)
				globalPaneCache.remove(msg.SessionName)
				globalActiveRegistry.remove(msg.SessionName)
				break
			}
		}
		// Adjust selection if needed
		if p.shellSelected && removedIdx >= 0 {
			if removedIdx < p.selectedShellIdx {
				// Shell before selected one was removed, decrement to stay on same shell
				p.selectedShellIdx--
			} else if p.selectedShellIdx >= len(p.shells) {
				// Selected shell was removed or index is now out of bounds
				if len(p.shells) > 0 {
					p.selectedShellIdx = len(p.shells) - 1
				} else if len(p.worktrees) > 0 {
					p.shellSelected = false
					p.selectedIdx = 0
				}
			}
			p.saveSelectionState()
			// Reload content for the newly selected item
			cmds = append(cmds, p.loadSelectedContent())
		}

	case ShellSessionDeadMsg:
		// Timer leak prevention (td-83dc22): increment generation to invalidate pending timers
		p.shellPollGeneration[msg.TmuxName]++
		// Shell session externally terminated (user typed 'exit' in shell)
		// Remove the dead shell from the list
		removedIdx := -1
		for i, shell := range p.shells {
			if shell.TmuxName == msg.TmuxName {
				removedIdx = i
				// Clean up Agent resources
				if shell.Agent != nil {
					shell.Agent.OutputBuf = nil
					shell.Agent = nil
				}
				p.shells = append(p.shells[:i], p.shells[i+1:]...)
				delete(p.managedSessions, msg.TmuxName)
				// Clean up pane cache and active registry (td-018f25)
				globalPaneCache.remove(msg.TmuxName)
				globalActiveRegistry.remove(msg.TmuxName)
				break
			}
		}
		// Adjust selection if needed
		if p.shellSelected && removedIdx >= 0 {
			if removedIdx < p.selectedShellIdx {
				// Shell before selected one was removed, decrement to stay on same shell
				p.selectedShellIdx--
			} else if p.selectedShellIdx >= len(p.shells) {
				// Selected shell was removed or index is now out of bounds
				if len(p.shells) > 0 {
					p.selectedShellIdx = len(p.shells) - 1
				} else if len(p.worktrees) > 0 {
					p.shellSelected = false
					p.selectedIdx = 0
				}
			}
			p.saveSelectionState()
			// Reload content for the newly selected item
			return p, p.loadSelectedContent()
		}
		return p, nil

	case ShellOutputMsg:
		// Update last output time if content changed
		shell := p.findShellByName(msg.TmuxName)
		if shell != nil && msg.Changed && shell.Agent != nil {
			shell.Agent.LastOutput = time.Now()
		}
		// Update bracketed paste mode and cursor position if in interactive mode (td-79ab6163)
		if p.viewMode == ViewModeInteractive && p.shellSelected {
			if selectedShell := p.getSelectedShell(); selectedShell != nil && selectedShell.TmuxName == msg.TmuxName {
				p.updateBracketedPasteMode(msg.Output)
				p.updateMouseReportingMode(msg.Output)
				// Use cursor position captured atomically with output (no separate query needed)
				if msg.HasCursor && p.interactiveState != nil && p.interactiveState.Active {
					p.interactiveState.CursorRow = msg.CursorRow
					p.interactiveState.CursorCol = msg.CursorCol
					p.interactiveState.CursorVisible = msg.CursorVisible
					p.interactiveState.PaneHeight = msg.PaneHeight
					p.interactiveState.PaneWidth = msg.PaneWidth
				}
				if resizeCmd := p.maybeResizeInteractivePane(msg.PaneWidth, msg.PaneHeight); resizeCmd != nil {
					cmds = append(cmds, resizeCmd)
				}
			}
		}
		// Schedule next poll with adaptive interval.
		// Three visibility states:
		// 1. Visible + focused → fast polling (500ms active, 5s idle)
		// 2. Visible + unfocused → medium polling (2s) - user can see output but clicked elsewhere
		// 3. Not visible → slow polling (10-20s)
		interval := pollIntervalActive
		if !msg.Changed {
			interval = pollIntervalIdle
		}
		selectedShell := p.getSelectedShell()
		isSelectedShell := selectedShell != nil && selectedShell.TmuxName == msg.TmuxName
		isVisibleOnScreen := isSelectedShell && p.shellSelected &&
			(p.viewMode == ViewModeList || p.viewMode == ViewModeInteractive)

		if !isVisibleOnScreen {
			// Not visible - use slow background polling
			background := p.backgroundPollInterval()
			if background > interval {
				interval = background
			}
		} else if !p.focused {
			// Visible but plugin not focused - use medium interval so user sees updates
			if interval < pollIntervalVisibleUnfocused {
				interval = pollIntervalVisibleUnfocused
			}
		}
		// If visible AND focused, keep the fast interval (pollIntervalActive/pollIntervalIdle)
		// Use interactive polling in interactive mode for fast response
		if p.viewMode == ViewModeInteractive && p.shellSelected {
			if selectedShell != nil && selectedShell.TmuxName == msg.TmuxName {
				cmds = append(cmds, p.pollInteractivePane())
				return p, tea.Batch(cmds...)
			}
		}
		cmds = append(cmds, p.scheduleShellPollByName(msg.TmuxName, interval))
		return p, tea.Batch(cmds...)

	case RenameShellDoneMsg:
		// Find shell and update its display name
		for _, shell := range p.shells {
			if shell.TmuxName == msg.TmuxName {
				shell.Name = msg.NewName
				break
			}
		}
		// Persist the selection state
		p.saveSelectionState()

	case pollShellByNameMsg:
		// Timer leak prevention (td-83dc22): ignore stale poll messages.
		// If the shell was removed since this timer was scheduled,
		// the generation won't match and we drop the message.
		if currentGen := p.shellPollGeneration[msg.TmuxName]; msg.Generation != currentGen {
			return p, nil // Stale timer, ignore
		}
		// Poll specific shell session for output by name
		if p.findShellByName(msg.TmuxName) != nil {
			return p, p.pollShellSessionByName(msg.TmuxName)
		}
		return p, nil

	case pollShellMsg:
		// Legacy: poll selected shell session for output
		if shell := p.getSelectedShell(); shell != nil {
			return p, p.pollShellSessionByName(shell.TmuxName)
		}
		return p, nil

	case AgentStoppedMsg:
		// Timer leak prevention (td-83dc22): increment generation to invalidate pending timers
		p.pollGeneration[msg.WorktreeName]++
		if wt := p.findWorktree(msg.WorktreeName); wt != nil {
			// Capture session name before clearing Agent (uses sanitized name like StartAgent)
			sessionName := tmuxSessionPrefix + sanitizeName(wt.Name)
			wt.Agent = nil
			wt.Status = StatusPaused
			// Clean up cache, active registry, and session tracking (td-53e8a023, td-018f25)
			globalPaneCache.remove(sessionName)
			globalActiveRegistry.remove(sessionName)
			delete(p.managedSessions, sessionName)
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
		p.taskLoading = false
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
					p.mergeState.ExistingPR = msg.ExistingPRFound
					// Save PR URL to worktree for indicator in list
					if wt := p.mergeState.Worktree; wt != nil && msg.Data != "" {
						wt.PRURL = msg.Data
						savePRURL(wt.Path, msg.Data)
					}
					// PR created (or existing found) - advanceMergeStep handles status transition
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

	case DirectMergeDoneMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorktreeName {
			if msg.Err != nil {
				p.mergeState.Error = msg.Err
				p.mergeState.StepStatus[MergeStepDirectMerge] = "error"
			} else {
				// Direct merge succeeded, advance to confirmation
				cmds = append(cmds, p.advanceMergeStep())
			}
		}

	case CleanupDoneMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorktreeName {
			if p.mergeState.CleanupResults == nil {
				p.mergeState.CleanupResults = msg.Results
			} else {
				// Merge results from local cleanup
				p.mergeState.CleanupResults.LocalWorktreeDeleted = msg.Results.LocalWorktreeDeleted
				p.mergeState.CleanupResults.LocalBranchDeleted = msg.Results.LocalBranchDeleted
				p.mergeState.CleanupResults.Errors = append(
					p.mergeState.CleanupResults.Errors, msg.Results.Errors...)
			}

			// Remove worktree from list if deleted
			if msg.Results.LocalWorktreeDeleted {
				p.removeWorktreeByName(msg.WorktreeName)
				if p.selectedIdx >= len(p.worktrees) && p.selectedIdx > 0 {
					p.selectedIdx--
				}
			}

			// Check if all cleanup tasks are done
			p.checkCleanupComplete()
		}

	case RemoteBranchDeleteMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorktreeName {
			if p.mergeState.CleanupResults == nil {
				p.mergeState.CleanupResults = &CleanupResults{}
			}
			if msg.Err != nil {
				p.mergeState.CleanupResults.Errors = append(
					p.mergeState.CleanupResults.Errors,
					fmt.Sprintf("Remote branch: %v", msg.Err))
			} else {
				p.mergeState.CleanupResults.RemoteBranchDeleted = true
			}
			// Check if all cleanup tasks are done
			p.checkCleanupComplete()
		}

	case PullAfterMergeMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorktreeName {
			if p.mergeState.CleanupResults == nil {
				p.mergeState.CleanupResults = &CleanupResults{}
			}
			p.mergeState.CleanupResults.PullAttempted = true
			p.mergeState.CleanupResults.PullSuccess = msg.Success
			p.mergeState.CleanupResults.PullError = msg.Err

			// Parse error for summary and divergence detection
			if msg.Err != nil {
				summary, full, diverged := summarizeGitError(msg.Err)
				p.mergeState.CleanupResults.PullErrorSummary = summary
				p.mergeState.CleanupResults.PullErrorFull = full
				p.mergeState.CleanupResults.BranchDiverged = diverged
				p.mergeState.CleanupResults.BaseBranch = msg.Branch
				p.mergeState.CleanupResults.ShowErrorDetails = false
			}

			// Check if all cleanup tasks are done
			p.checkCleanupComplete()
		}

	case RebaseResolutionMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorktreeName {
			if msg.Success {
				// Rebase succeeded - update state
				p.mergeState.CleanupResults.PullSuccess = true
				p.mergeState.CleanupResults.PullError = nil
				p.mergeState.CleanupResults.BranchDiverged = false
				p.mergeState.CleanupResults.PullErrorSummary = ""
				p.mergeState.CleanupResults.PullErrorFull = ""
			} else {
				// Rebase failed - update error state
				p.mergeState.CleanupResults.PullError = msg.Err
				summary, full, diverged := summarizeGitError(msg.Err)
				p.mergeState.CleanupResults.PullErrorSummary = summary
				p.mergeState.CleanupResults.PullErrorFull = full
				p.mergeState.CleanupResults.BranchDiverged = diverged
			}
		}

	case MergeResolutionMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorktreeName {
			if msg.Success {
				// Merge succeeded - update state
				p.mergeState.CleanupResults.PullSuccess = true
				p.mergeState.CleanupResults.PullError = nil
				p.mergeState.CleanupResults.BranchDiverged = false
				p.mergeState.CleanupResults.PullErrorSummary = ""
				p.mergeState.CleanupResults.PullErrorFull = ""
			} else {
				// Merge failed - update error state
				p.mergeState.CleanupResults.PullError = msg.Err
				summary, full, diverged := summarizeGitError(msg.Err)
				p.mergeState.CleanupResults.PullErrorSummary = summary
				p.mergeState.CleanupResults.PullErrorFull = full
				p.mergeState.CleanupResults.BranchDiverged = diverged
			}
		}

	case reconnectedAgentsMsg:
		// After reconnecting to existing sessions, detect orphaned worktrees
		// (worktrees with .sidecar-agent file but no tmux session)
		p.detectOrphanedWorktrees()
		// Start periodic session validation to prevent memory leaks (td-41695b)
		pollingCmds := append(msg.Cmds, p.scheduleSessionValidation(60*time.Second))
		return p, tea.Batch(pollingCmds...)

	case validateManagedSessionsMsg:
		// Trigger validation of managedSessions against actual tmux sessions
		return p, p.validateManagedSessions()

	case validateManagedSessionsResultMsg:
		// Prune managedSessions entries that no longer exist in tmux (td-41695b)
		for session := range p.managedSessions {
			if !msg.ExistingSessions[session] {
				delete(p.managedSessions, session)
			}
		}
		// Schedule next validation in 60 seconds
		return p, p.scheduleSessionValidation(60 * time.Second)

	case OpenCreateModalWithTaskMsg:
		return p, p.openCreateModalWithTask(msg.TaskID, msg.TaskTitle)

	case cursorPositionMsg:
		// Update cached cursor position for interactive mode rendering (td-648af4)
		if p.interactiveState != nil && p.interactiveState.Active {
			p.interactiveState.CursorRow = msg.Row
			p.interactiveState.CursorCol = msg.Col
			p.interactiveState.CursorVisible = msg.Visible
		}

	case escapeTimerMsg:
		// Handle escape delay timer for interactive mode double-escape detection
		if p.viewMode == ViewModeInteractive {
			cmd := p.handleEscapeTimer()
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case InteractiveSessionDeadMsg:
		// Session ended externally - show notification (td-a1c8456f)
		p.exitInteractiveMode()
		p.toastMessage = "Session ended"
		p.toastTime = time.Now()

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
