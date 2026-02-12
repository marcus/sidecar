package workspace

import (
	"fmt"
	"math"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/state"
)

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
	case ViewModeConfirmDeleteShell:
		return p.handleConfirmDeleteShellKeys(msg)
	case ViewModeCommitForMerge:
		return p.handleCommitForMergeKeys(msg)
	case ViewModePromptPicker:
		return p.handlePromptPickerKeys(msg)
	case ViewModeTypeSelector:
		return p.handleTypeSelectorKeys(msg)
	case ViewModeRenameShell:
		return p.handleRenameShellKeys(msg)
	case ViewModeFetchPR:
		return p.handleFetchPRKeys(msg)
	case ViewModeFilePicker:
		return p.handleFilePickerKeys(msg)
	case ViewModeInteractive:
		return p.handleInteractiveKeys(msg)
	}
	return nil
}

// handleTypeSelectorKeys handles keys in the type selector modal.
func (p *Plugin) handleTypeSelectorKeys(msg tea.KeyMsg) tea.Cmd {
	p.ensureTypeSelectorModal()
	if p.typeSelectorModal == nil {
		return nil
	}

	// Track selection before to detect changes
	prevIdx := p.typeSelectorIdx
	prevAgentIdx := p.typeSelectorAgentIdx

	action, cmd := p.typeSelectorModal.HandleKey(msg)

	// Modal width depends on selection - rebuild if type changed
	if p.typeSelectorIdx != prevIdx {
		p.typeSelectorModalWidth = 0 // Force rebuild
	}

	// Sync agent type when agent index changes (td-f42a86)
	// No need to rebuild modal - When sections handle visibility dynamically
	if p.typeSelectorAgentIdx != prevAgentIdx && p.typeSelectorAgentIdx >= 0 && p.typeSelectorAgentIdx < len(ShellAgentOrder) {
		p.typeSelectorAgentType = ShellAgentOrder[p.typeSelectorAgentIdx]
	}

	switch action {
	case "cancel", typeSelectorCancelID:
		p.viewMode = ViewModeList
		p.clearTypeSelectorModal()
		return nil
	case typeSelectorConfirmID, "type-shell", "type-workspace":
		return p.executeTypeSelectorConfirm()
	}

	return cmd
}

// executeTypeSelectorConfirm executes the type selector confirmation.
func (p *Plugin) executeTypeSelectorConfirm() tea.Cmd {
	p.viewMode = ViewModeList
	if p.typeSelectorIdx == 0 {
		// Shell selected - use createShellWithAgent which captures agent info (td-16b2b5)
		cmd := p.createShellWithAgent()
		p.clearTypeSelectorModal()
		return cmd
	}
	// Workspace selected
	p.clearTypeSelectorModal()
	return p.openCreateModal()
}

// handleFetchPRKeys handles keys in the fetch PR modal.
func (p *Plugin) handleFetchPRKeys(msg tea.KeyMsg) tea.Cmd {
	p.ensureFetchPRModal()
	if p.fetchPRModal == nil {
		return nil
	}

	// Intercept custom keys before delegating to modal
	switch msg.String() {
	case "esc":
		p.viewMode = ViewModeList
		p.clearFetchPRState()
		return nil
	case "enter":
		if p.fetchPRLoading || p.fetchPRError != "" {
			return nil
		}
		filtered := p.filteredFetchPRItems()
		if p.fetchPRCursor >= 0 && p.fetchPRCursor < len(filtered) {
			pr := filtered[p.fetchPRCursor]
			p.fetchPRLoading = true // Show loading while creating worktree
			p.clearFetchPRModal()   // Rebuild to show loading state
			return p.fetchAndCreateWorktree(pr)
		}
		return nil
	case "j", "down":
		filtered := p.filteredFetchPRItems()
		if p.fetchPRCursor < len(filtered)-1 {
			p.fetchPRCursor++
			p.adjustFetchPRScroll()
			p.clearFetchPRModal()
		}
		return nil
	case "k", "up":
		if p.fetchPRCursor > 0 {
			p.fetchPRCursor--
			p.adjustFetchPRScroll()
			p.clearFetchPRModal()
		}
		return nil
	case "backspace":
		if len(p.fetchPRFilter) > 0 {
			p.fetchPRFilter = p.fetchPRFilter[:len(p.fetchPRFilter)-1]
			p.fetchPRCursor = 0
			p.fetchPRScrollOffset = 0
			p.clearFetchPRModal() // Rebuild to reflect filter change
		}
		return nil
	default:
		// Treat printable characters as filter input
		if len(msg.String()) == 1 && msg.String()[0] >= 32 && msg.String()[0] < 127 {
			p.fetchPRFilter += msg.String()
			p.fetchPRCursor = 0
			p.fetchPRScrollOffset = 0
			p.clearFetchPRModal() // Rebuild to reflect filter change
		}
		return nil
	}
}

// handlePromptPickerKeys handles keys in the prompt picker modal.
func (p *Plugin) handlePromptPickerKeys(msg tea.KeyMsg) tea.Cmd {
	if p.promptPicker == nil {
		return nil
	}

	p.ensurePromptPickerModal()
	if p.promptPickerModal == nil {
		return nil
	}

	pp := p.promptPicker
	key := msg.String()

	if len(pp.prompts) == 0 && key == "d" {
		return func() tea.Msg { return PromptInstallDefaultsMsg{} }
	}

	switch key {
	case "esc", "q":
		return func() tea.Msg { return PromptCancelledMsg{} }
	case "tab", "shift+tab":
		pp.filterFocused = !pp.filterFocused
		p.syncPromptPickerFocus()
		return nil
	}

	before := pp.filterInput.Value()
	action, cmd := p.promptPickerModal.HandleKey(msg)
	if action == "cancel" {
		return func() tea.Msg { return PromptCancelledMsg{} }
	}

	if before != pp.filterInput.Value() {
		pp.applyFilter()
		if !pp.filterFocused {
			p.syncPromptPickerFocus()
		}
	}

	if action != "" {
		if idx, ok := parsePromptPickerItemID(action); ok {
			pp.selectedIdx = idx
			return p.promptPickerSelectCmd()
		}
		if action == promptPickerFilterID {
			return p.promptPickerSelectCmd()
		}
	}

	switch key {
	case "enter":
		return p.promptPickerSelectCmd()

	case "up":
		if pp.selectedIdx > -1 {
			pp.selectedIdx--
		}
		if !pp.filterFocused {
			p.syncPromptPickerFocus()
		}
		return nil

	case "down":
		if pp.selectedIdx < len(pp.filtered)-1 {
			pp.selectedIdx++
		}
		if !pp.filterFocused {
			p.syncPromptPickerFocus()
		}
		return nil
	}

	if !pp.filterFocused {
		switch key {
		case "k":
			if pp.selectedIdx > -1 {
				pp.selectedIdx--
			}
			p.syncPromptPickerFocus()
			return nil
		case "j":
			if pp.selectedIdx < len(pp.filtered)-1 {
				pp.selectedIdx++
			}
			p.syncPromptPickerFocus()
			return nil
		case "home", "g":
			pp.selectedIdx = -1
			p.syncPromptPickerFocus()
			return nil
		case "end", "G":
			if len(pp.filtered) > 0 {
				pp.selectedIdx = len(pp.filtered) - 1
			}
			p.syncPromptPickerFocus()
			return nil
		}
	}

	return cmd
}

// handleAgentChoiceKeys handles keys in agent choice modal.
func (p *Plugin) handleAgentChoiceKeys(msg tea.KeyMsg) tea.Cmd {
	p.ensureAgentChoiceModal()
	if p.agentChoiceModal == nil {
		return nil
	}

	action, cmd := p.agentChoiceModal.HandleKey(msg)

	switch action {
	case "cancel", agentChoiceCancelID:
		p.viewMode = ViewModeList
		p.clearAgentChoiceModal()
		return nil
	case agentChoiceActionID, agentChoiceConfirmID, "agent-choice-attach", "agent-choice-restart":
		return p.executeAgentChoice()
	}

	return cmd
}

// executeAgentChoice executes the selected agent choice action.
func (p *Plugin) executeAgentChoice() tea.Cmd {
	wt := p.agentChoiceWorktree
	idx := p.agentChoiceIdx
	p.viewMode = ViewModeList
	p.clearAgentChoiceModal()
	if wt == nil {
		return nil
	}
	if idx == 0 {
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
	p.ensureConfirmDeleteModal()
	if p.deleteConfirmModal == nil {
		return nil
	}

	switch msg.String() {
	case "D":
		// Power user shortcut - immediate confirm
		return p.executeDelete()
	case "esc", "q":
		return p.cancelDelete()
	case "j", "down", "l", "right":
		p.deleteConfirmModal.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
		return nil
	case "k", "up", "h", "left":
		p.deleteConfirmModal.HandleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
		return nil
	}

	action, cmd := p.deleteConfirmModal.HandleKey(msg)
	switch action {
	case "cancel", deleteConfirmCancelID:
		return p.cancelDelete()
	case deleteConfirmDeleteID:
		return p.executeDelete()
	}
	return cmd
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
	branch := wt.Branch
	isMissing := wt.IsMissing
	deleteLocal := p.deleteLocalBranchOpt
	deleteRemote := p.deleteRemoteBranchOpt && p.deleteHasRemote
	workDir := p.ctx.WorkDir

	// Kill tmux session if it exists (before deleting worktree)
	sessionName := tmuxSessionPrefix + sanitizeName(name)
	if sessionExists(sessionName) {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}
	delete(p.managedSessions, sessionName)
	globalPaneCache.remove(sessionName)

	// Clear modal state
	p.viewMode = ViewModeList
	p.clearConfirmDeleteModal()

	// Clear preview pane content
	p.diffContent = ""
	p.diffRaw = ""
	p.cachedTaskID = ""
	p.cachedTask = nil

	return func() tea.Msg {
		var warnings []string

		// Delete the worktree first
		err := doDeleteWorktree(workDir, path, isMissing)
		if err != nil {
			return DeleteDoneMsg{Name: name, Err: err}
		}

		// Delete local branch if requested
		if deleteLocal {
			if branchErr := deleteBranch(workDir, branch); branchErr != nil {
				warnings = append(warnings, fmt.Sprintf("Local branch: %v", branchErr))
			}
		}

		// Delete remote branch if requested
		if deleteRemote {
			if remoteErr := deleteRemoteBranchCmd(workDir, branch); remoteErr != nil {
				warnings = append(warnings, fmt.Sprintf("Remote branch: %v", remoteErr))
			}
		}

		return DeleteDoneMsg{Name: name, Err: nil, Warnings: warnings}
	}
}

// cancelDelete closes the delete confirmation modal without deleting.
func (p *Plugin) cancelDelete() tea.Cmd {
	p.viewMode = ViewModeList
	p.clearConfirmDeleteModal()
	return nil
}

func (p *Plugin) clearConfirmDeleteModal() {
	p.deleteConfirmWorktree = nil
	p.deleteLocalBranchOpt = false
	p.deleteRemoteBranchOpt = false
	p.deleteHasRemote = false
	p.deleteIsMainBranch = false
	p.deleteConfirmModal = nil
	p.deleteConfirmModalWidth = 0
}

// handleConfirmDeleteShellKeys handles keys in the shell delete confirmation modal.
func (p *Plugin) handleConfirmDeleteShellKeys(msg tea.KeyMsg) tea.Cmd {
	p.ensureConfirmDeleteShellModal()
	if p.deleteShellModal == nil {
		return nil
	}

	switch msg.String() {
	case "D":
		return p.executeShellDelete()
	case "esc", "q":
		return p.cancelShellDelete()
	case "j", "down", "l", "right":
		p.deleteShellModal.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
		return nil
	case "k", "up", "h", "left":
		p.deleteShellModal.HandleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
		return nil
	}

	action, cmd := p.deleteShellModal.HandleKey(msg)
	switch action {
	case "cancel", deleteShellConfirmCancelID:
		return p.cancelShellDelete()
	case deleteShellConfirmDeleteID:
		return p.executeShellDelete()
	}
	return cmd
}

// executeShellDelete performs the shell deletion.
func (p *Plugin) executeShellDelete() tea.Cmd {
	shell := p.deleteConfirmShell
	if shell == nil {
		p.viewMode = ViewModeList
		return nil
	}

	sessionName := shell.TmuxName

	// Clear modal state
	p.viewMode = ViewModeList
	p.clearConfirmDeleteShellModal()

	return p.killShellSessionByName(sessionName)
}

// cancelShellDelete closes the shell delete confirmation modal without deleting.
func (p *Plugin) cancelShellDelete() tea.Cmd {
	p.viewMode = ViewModeList
	p.clearConfirmDeleteShellModal()
	return nil
}

func (p *Plugin) clearConfirmDeleteShellModal() {
	p.deleteConfirmShell = nil
	p.deleteShellModal = nil
	p.deleteShellModalWidth = 0
}

// handleListKeys handles keys in list view (and kanban view).
func (p *Plugin) handleListKeys(msg tea.KeyMsg) tea.Cmd {
	// Clear any deletion warnings on key interaction
	p.deleteWarnings = nil

	switch msg.String() {
	case "j", "down":
		if p.viewMode == ViewModeKanban {
			// Kanban mode: move cursor down within column (no selection change)
			p.moveKanbanRow(1)
			return nil
		}
		if p.activePane == PaneSidebar {
			p.moveCursor(1)
			return p.loadSelectedContent()
		}
		// Scroll down toward newer content (decrease offset from bottom)
		if p.previewOffset > 0 {
			p.previewOffset--
			if p.previewOffset == 0 {
				p.autoScrollOutput = true    // Resume auto-scroll when at bottom
				p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot
			}
		}
	case "k", "up":
		if p.viewMode == ViewModeKanban {
			// Kanban mode: move cursor up within column (no selection change)
			p.moveKanbanRow(-1)
			return nil
		}
		if p.activePane == PaneSidebar {
			p.moveCursor(-1)
			return p.loadSelectedContent()
		}
		// Scroll up toward older content (increase offset from bottom)
		p.autoScrollOutput = false
		p.captureScrollBaseLineCount() // td-f7c8be: prevent bounce on poll
		p.previewOffset++
	case "g":
		if p.viewMode == ViewModeKanban {
			// Kanban mode: jump cursor to top of current column
			p.kanbanRow = 0
			return nil
		}
		if p.activePane == PaneSidebar {
			// Jump to top = select first shell if any, otherwise first worktree
			if len(p.shells) > 0 {
				p.shellSelected = true
				p.selectedShellIdx = 0
				// Exit interactive mode when switching selection (td-fc758e88)
				p.exitInteractiveMode()
				p.saveSelectionState()
			} else if len(p.worktrees) > 0 {
				p.shellSelected = false
				p.selectedIdx = 0
				// Exit interactive mode when switching selection (td-fc758e88)
				p.exitInteractiveMode()
				p.saveSelectionState()
			}
			p.scrollOffset = 0
			return p.loadSelectedContent()
		}
		// Go to top (oldest content) - pause auto-scroll
		p.autoScrollOutput = false
		p.captureScrollBaseLineCount() // td-f7c8be: prevent bounce on poll
		p.previewOffset = math.MaxInt  // Will be clamped in render
	case "G":
		if p.viewMode == ViewModeKanban {
			// Kanban mode: jump cursor to bottom of current column
			columns := p.getKanbanColumns()
			count := p.kanbanColumnItemCount(p.kanbanCol, columns)
			if count > 0 {
				p.kanbanRow = count - 1
			}
			return nil
		}
		if p.activePane == PaneSidebar {
			// Jump to bottom = select last worktree (not shell)
			if len(p.worktrees) > 0 {
				p.shellSelected = false
				p.selectedIdx = len(p.worktrees) - 1
				// Exit interactive mode when switching selection (td-fc758e88)
				p.exitInteractiveMode()
				p.saveSelectionState()
				p.ensureVisible()
				return p.loadSelectedContent()
			}
			// No worktrees, stay on shell
			return nil
		}
		// Go to bottom (newest content) - resume auto-scroll
		p.previewOffset = 0
		p.autoScrollOutput = true
		p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot
	case "n":
		// Open type selector modal to choose between Shell and Worktree
		p.viewMode = ViewModeTypeSelector
		p.typeSelectorIdx = 1 // Default to Worktree (more common)
		p.typeSelectorNameInput = textinput.New()
		p.typeSelectorNameInput.Placeholder = p.nextShellDisplayName()
		p.typeSelectorNameInput.Prompt = ""
		p.typeSelectorNameInput.Width = 30
		p.typeSelectorNameInput.CharLimit = 50
		p.typeSelectorModal = nil    // Force rebuild
		p.typeSelectorModalWidth = 0 // Force rebuild
		return nil
	case "D":
		// Check if deleting a shell session
		if p.shellSelected && p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
			p.viewMode = ViewModeConfirmDeleteShell
			p.deleteConfirmShell = p.shells[p.selectedShellIdx]
			p.deleteShellModal = nil
			p.deleteShellModalWidth = 0
			return nil
		}
		// Otherwise delete worktree
		wt := p.selectedWorktree()
		if wt == nil {
			return nil
		}
		p.viewMode = ViewModeConfirmDelete
		p.deleteConfirmWorktree = wt
		p.deleteLocalBranchOpt = wt.IsMissing // Default ON when folder already gone
		p.deleteRemoteBranchOpt = false
		p.deleteHasRemote = false
		p.deleteIsMainBranch = isMainBranch(p.ctx.WorkDir, wt.Branch)
		p.deleteConfirmModal = nil
		p.deleteConfirmModalWidth = 0
		if p.deleteIsMainBranch {
			// Main branch is protected: skip branch options
			return nil
		}
		// Check for remote branch existence asynchronously
		return p.checkRemoteBranch(wt)
	case "p":
		return p.pushSelected()
	case "l", "right":
		if p.viewMode == ViewModeKanban {
			// Kanban mode: move cursor to next column (no selection change)
			p.moveKanbanColumn(1)
			return nil
		}
		if p.activePane == PaneSidebar {
			p.activePane = PanePreview
		}
	case "enter":
		// Kanban mode: sync cursor to selection, then fall through to activate
		if p.viewMode == ViewModeKanban {
			oldShellSelected := p.shellSelected
			oldShellIdx := p.selectedShellIdx
			oldWorktreeIdx := p.selectedIdx
			p.syncKanbanToList()
			p.applyKanbanSelectionChange(oldShellSelected, oldShellIdx, oldWorktreeIdx)
		}
		// Enter interactive mode (tmux input passthrough) - feature gated
		// Works from sidebar for selected shell/worktree with active session
		// Handle orphaned worktrees: start new agent instead of silently returning nil
		if !p.shellSelected {
			wt := p.selectedWorktree()
			if wt != nil && wt.IsOrphaned && wt.Agent == nil {
				wt.IsOrphaned = false
				agentType := wt.ChosenAgentType
				if agentType == AgentNone || agentType == "" {
					agentType = AgentClaude
				}
				return p.StartAgent(wt, agentType)
			}
		}
		if cmd := p.enterInteractiveMode(); cmd != nil {
			return cmd
		}
		// Interactive mode couldn't start — at least load content for the selection
		return p.loadSelectedContent()
	case "t":
		// Attach to tmux session
		// Shell entry: attach to selected shell session
		if p.shellSelected {
			if p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
				return p.ensureShellAndAttachByIndex(p.selectedShellIdx)
			}
			return nil
		}
		wt := p.selectedWorktree()
		if wt == nil {
			return nil
		}
		// Attach to tmux session if agent running
		if wt.Agent != nil {
			p.attachedSession = wt.Name
			return p.AttachToSession(wt)
		}
		// Orphaned worktree: recover by starting new agent
		if wt.IsOrphaned {
			// Clear flag immediately for UI feedback; also cleared in AgentStartedMsg
			// handler when agent actually starts (StartAgent is async)
			wt.IsOrphaned = false
			agentType := wt.ChosenAgentType
			if agentType == AgentNone || agentType == "" {
				agentType = AgentClaude // Fallback
			}
			return p.StartAgent(wt, agentType)
		}
		// No agent, not orphaned: focus preview
		if p.activePane == PaneSidebar {
			p.activePane = PanePreview
		}
	case "h", "left":
		if p.viewMode == ViewModeKanban {
			// Kanban mode: move cursor to previous column (no selection change)
			p.moveKanbanColumn(-1)
			return nil
		}
		if p.activePane == PanePreview {
			p.activePane = PaneSidebar
		}
	case "esc":
		if !p.sidebarVisible {
			p.toggleSidebar()
			return p.resizeSelectedPaneCmd()
		}
		if p.activePane == PanePreview {
			p.activePane = PaneSidebar
		}
	case "\\":
		p.toggleSidebar()
		if p.viewMode == ViewModeInteractive {
			// Poll captures cursor atomically - no separate query needed
			return tea.Batch(p.resizeInteractivePaneCmd(), p.pollInteractivePaneImmediate())
		}
		if !p.sidebarVisible {
			return tea.Batch(p.resizeSelectedPaneCmd(), appmsg.ShowToast("Sidebar hidden (\\ to restore)", 2*time.Second))
		}
		// Resize pane in background to match new preview width
		return p.resizeSelectedPaneCmd()
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
	case "{":
		// Jump to previous file in diff (when in preview pane on diff tab)
		if p.activePane == PanePreview && p.previewTab == PreviewTabDiff {
			return p.jumpToPrevFile()
		}
	case "}":
		// Jump to next file in diff (when in preview pane on diff tab)
		if p.activePane == PanePreview && p.previewTab == PreviewTabDiff {
			return p.jumpToNextFile()
		}
	case "f":
		// Open file picker (when in preview pane on diff tab with multiple files)
		if p.activePane == PanePreview && p.previewTab == PreviewTabDiff {
			return p.openFilePicker()
		}
	case "r":
		return func() tea.Msg { return RefreshMsg{} }
	case "i":
		// Legacy shortcut for interactive mode (enter is now primary)
		return p.enterInteractiveMode()
	case "v":
		// In sidebar: toggle between list and kanban view
		// In preview pane on diff tab: toggle unified/side-by-side diff view
		if p.activePane == PanePreview && p.previewTab == PreviewTabDiff {
			if p.diffViewMode == DiffViewUnified {
				p.diffViewMode = DiffViewSideBySide
				_ = state.SetWorkspaceDiffMode("side-by-side")
			} else {
				p.diffViewMode = DiffViewUnified
				_ = state.SetWorkspaceDiffMode("unified")
			}
			return nil
		} else if p.activePane == PaneSidebar || p.viewMode == ViewModeKanban {
			switch p.viewMode {
			case ViewModeList:
				p.viewMode = ViewModeKanban
				p.syncListToKanban()
				return p.pollAllAgentStatusesNow()
			case ViewModeKanban:
				p.viewMode = ViewModeList
				return p.pollSelectedAgentNowIfVisible()
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
					p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot
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
				p.captureScrollBaseLineCount() // td-f7c8be: prevent bounce on poll
				p.previewOffset += pageSize
			} else {
				if p.previewOffset > pageSize {
					p.previewOffset -= pageSize
				} else {
					p.previewOffset = 0
				}
			}
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
		p.agentChoiceIdx = 0 // Default to attach
		p.viewMode = ViewModeAgentChoice
		return nil
	case "S":
		// Stop agent on selected worktree
		wt := p.selectedWorktree()
		if wt != nil && wt.Agent != nil {
			return p.StopAgent(wt)
		}
	case "K":
		// Kill selected shell session
		if p.shellSelected && p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
			shell := p.shells[p.selectedShellIdx]
			if shell.Agent != nil {
				return p.killShellSessionByName(shell.TmuxName)
			}
		}
	case "R":
		// Rename selected shell session
		if p.shellSelected && p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
			shell := p.shells[p.selectedShellIdx]
			p.viewMode = ViewModeRenameShell
			p.renameShellSession = shell
			p.renameShellInput = textinput.New()
			p.renameShellInput.SetValue(shell.Name)
			p.renameShellInput.CharLimit = 50
			p.renameShellInput.Width = 30
			p.renameShellInput.Prompt = ""
			p.renameShellError = ""
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
	case "T":
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
	case "F":
		// Fetch remote PR as workspace
		p.viewMode = ViewModeFetchPR
		p.fetchPRLoading = true
		p.fetchPRFilter = ""
		p.fetchPRCursor = 0
		p.fetchPRError = ""
		return p.fetchPRList()
	case "m":
		// In preview pane on task tab: toggle markdown render mode
		// Otherwise: start merge workflow
		if p.activePane == PanePreview && p.previewTab == PreviewTabTask {
			p.taskMarkdownMode = !p.taskMarkdownMode
			// Clear cached render to force re-render on mode change
			p.taskMarkdownRendered = nil
			return nil
		}
		// Start merge workflow
		wt := p.selectedWorktree()
		if wt != nil {
			return p.startMergeWorkflow(wt)
		}
	case "O":
		// Open selected worktree in git tab - switch to worktree and focus git plugin
		wt := p.selectedWorktree()
		if wt != nil {
			return p.openInGitTab(wt)
		}
	default:
		// Unhandled key in preview pane - flash to indicate attach is needed
		// Only flash if there's something to attach to (shell or worktree with agent)
		if p.activePane == PanePreview {
			canAttach := p.shellSelected || (p.selectedWorktree() != nil && p.selectedWorktree().Agent != nil)
			if canAttach {
				p.flashPreviewTime = time.Now()
			}
		}
	}
	return nil
}

// handleCreateKeys handles keys in create modal.
// createFocus: 0=name, 1=base, 2=prompt, 3=task, 4=agent, 5=skipPerms, 6=create button, 7=cancel button
func (p *Plugin) handleCreateKeys(msg tea.KeyMsg) tea.Cmd {
	p.ensureCreateModal()
	if p.createModal == nil {
		return nil
	}

	focusID := p.createModal.FocusedID()

	switch msg.String() {
	case "esc":
		p.viewMode = ViewModeList
		p.clearCreateModal()
		return nil
	case "tab":
		p.blurCreateInputs()
		p.createFocus = (p.createFocus + 1) % 8
		p.normalizeCreateFocus()
		p.focusCreateInput()
		p.syncCreateModalFocus()
		return nil
	case "shift+tab":
		p.blurCreateInputs()
		p.createFocus = (p.createFocus + 7) % 8
		p.normalizeCreateFocus()
		p.focusCreateInput()
		p.syncCreateModalFocus()
		return nil
	case "backspace":
		if p.createFocus == 3 && p.createTaskID != "" {
			p.createTaskID = ""
			p.createTaskTitle = ""
			p.taskSearchInput.SetValue("")
			p.taskSearchInput.Focus()
			p.taskSearchFiltered = filterTasks("", p.taskSearchAll)
			p.taskSearchIdx = 0
			p.syncCreateModalFocus()
			return nil
		}
	case " ":
		if p.createFocus == 5 {
			p.createSkipPermissions = !p.createSkipPermissions
			return nil
		}
	case "up":
		if p.createFocus == 1 && len(p.branchFiltered) > 0 {
			if p.branchIdx > 0 {
				p.branchIdx--
			}
			return nil
		}
		if p.createFocus == 3 && len(p.taskSearchFiltered) > 0 {
			if p.taskSearchIdx > 0 {
				p.taskSearchIdx--
			}
			return nil
		}
	case "down":
		if p.createFocus == 1 && len(p.branchFiltered) > 0 {
			if p.branchIdx < len(p.branchFiltered)-1 {
				p.branchIdx++
			}
			return nil
		}
		if p.createFocus == 3 && len(p.taskSearchFiltered) > 0 {
			if p.taskSearchIdx < len(p.taskSearchFiltered)-1 {
				p.taskSearchIdx++
			}
			return nil
		}
	case "enter":
		if idx, ok := parseIndexedID(createBranchItemPrefix, focusID); ok && idx < len(p.branchFiltered) {
			p.createBaseBranchInput.SetValue(p.branchFiltered[idx])
			p.branchFiltered = nil
			p.syncCreateModalFocus()
			return nil
		}
		if idx, ok := parseIndexedID(createTaskItemPrefix, focusID); ok && idx < len(p.taskSearchFiltered) {
			task := p.taskSearchFiltered[idx]
			p.createTaskID = task.ID
			p.createTaskTitle = task.Title
			p.taskSearchInput.Blur()
			p.createFocus = 4
			p.syncCreateModalFocus()
			return nil
		}
		if focusID == createPromptFieldID {
			p.promptPicker = NewPromptPicker(p.createPrompts, p.width, p.height)
			p.clearPromptPickerModal()
			p.viewMode = ViewModePromptPicker
			return nil
		}
		if focusID == createSubmitID {
			return p.validateAndCreateWorktree()
		}
		if focusID == createCancelID {
			p.viewMode = ViewModeList
			p.clearCreateModal()
			return nil
		}
		if p.createFocus == 1 && len(p.branchFiltered) > 0 {
			selectedBranch := p.branchFiltered[p.branchIdx]
			p.createBaseBranchInput.SetValue(selectedBranch)
			p.createFocus = 2
			p.focusCreateInput()
			p.syncCreateModalFocus()
			return nil
		}
		if p.createFocus == 2 {
			p.promptPicker = NewPromptPicker(p.createPrompts, p.width, p.height)
			p.clearPromptPickerModal()
			p.viewMode = ViewModePromptPicker
			return nil
		}
		if p.createFocus == 3 && len(p.taskSearchFiltered) > 0 {
			selectedTask := p.taskSearchFiltered[p.taskSearchIdx]
			p.createTaskID = selectedTask.ID
			p.createTaskTitle = selectedTask.Title
			p.taskSearchInput.Blur()
			p.createFocus = 4
			p.syncCreateModalFocus()
			return nil
		}
		if p.createFocus == 6 {
			return p.validateAndCreateWorktree()
		}
		if p.createFocus == 7 {
			p.viewMode = ViewModeList
			p.clearCreateModal()
			return nil
		}
		if p.createFocus < 2 {
			p.createFocus++
			p.focusCreateInput()
			p.syncCreateModalFocus()
			return nil
		}
	}

	wasAgentIdx := p.createAgentIdx
	action, cmd := p.createModal.HandleKey(msg)
	if p.createAgentIdx != wasAgentIdx && p.createAgentIdx < len(AgentTypeOrder) {
		p.createAgentType = AgentTypeOrder[p.createAgentIdx]
		p.syncCreateModalFocus()
	}

	if action == createSubmitID && focusID != createSubmitID {
		return cmd
	}
	if action == "cancel" || action == createCancelID {
		p.viewMode = ViewModeList
		p.clearCreateModal()
		return nil
	}

	// Delegate to task input for all other keys.
	p.createError = ""
	switch p.createFocus {
	case 0:
		name := p.createNameInput.Value()
		p.branchNameValid, p.branchNameErrors, p.branchNameSanitized = ValidateBranchName(name)
	case 1:
		p.branchFiltered = filterBranches(p.createBaseBranchInput.Value(), p.branchAll)
		p.branchIdx = 0
	case 3:
		if p.createTaskID == "" {
			p.taskSearchInput, cmd = p.taskSearchInput.Update(msg)
			p.taskSearchFiltered = filterTasks(p.taskSearchInput.Value(), p.taskSearchAll)
			p.taskSearchIdx = 0
		}
	}

	return cmd
}

func (p *Plugin) validateAndCreateWorktree() tea.Cmd {
	name := p.createNameInput.Value()
	if name == "" {
		p.createError = "Name is required"
		return nil
	}
	if !p.branchNameValid {
		p.createError = "Invalid branch name: " + strings.Join(p.branchNameErrors, ", ")
		return nil
	}
	return p.createWorktree()
}

// shouldShowSkipPermissions returns true if the current agent type supports skip permissions.
func (p *Plugin) shouldShowSkipPermissions() bool {
	if p.createAgentType == AgentNone {
		return false
	}
	flag := SkipPermissionsFlags[p.createAgentType]
	return flag != ""
}

// shouldShowShellSkipPerms returns true if the selected shell agent supports skip permissions.
// td-a902fe: Used in type selector modal when Shell is selected with an agent.
func (p *Plugin) shouldShowShellSkipPerms() bool {
	if p.typeSelectorAgentType == AgentNone {
		return false
	}
	return SkipPermissionsFlags[p.typeSelectorAgentType] != ""
}

func (p *Plugin) agentTypeIndex(agentType AgentType) int {
	for i, at := range AgentTypeOrder {
		if at == agentType {
			return i
		}
	}
	return 0
}

// blurCreateInputs blurs all create modal textinputs.
func (p *Plugin) blurCreateInputs() {
	p.createNameInput.Blur()
	p.createBaseBranchInput.Blur()
	p.taskSearchInput.Blur()
}

// focusCreateInput focuses the appropriate textinput based on createFocus.
// createFocus: 0=name, 1=base, 2=prompt (no textinput), 3=task, 4+=non-inputs
func (p *Plugin) focusCreateInput() {
	switch p.createFocus {
	case 0:
		p.createNameInput.Focus()
	case 1:
		p.createBaseBranchInput.Focus()
	// case 2 is prompt field - no textinput to focus (opens picker on Enter)
	case 3:
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

	// Ensure modal is built for key handling
	p.ensureMergeModal()

	// Handle error step — yank, dismiss
	if p.mergeState.Step == MergeStepError {
		switch msg.String() {
		case "y":
			return p.yankMergeErrorToClipboard()
		case "esc", "q", "enter":
			p.cancelMergeWorkflow()
			p.clearMergeModal()
			return nil
		}
		if p.mergeModal != nil {
			action, cmd := p.mergeModal.HandleKey(msg)
			if action == "dismiss" || action == "cancel" {
				p.cancelMergeWorkflow()
				p.clearMergeModal()
				return nil
			}
			return cmd
		}
		return nil
	}

	// For PostMergeConfirmation step, delegate to modal library for Tab/Enter/Space
	if p.mergeState.Step == MergeStepPostMergeConfirmation && p.mergeModal != nil {
		action, cmd := p.mergeModal.HandleKey(msg)
		switch action {
		case "cancel":
			p.cancelMergeWorkflow()
			p.clearMergeModal()
			return nil
		case mergeCleanUpButtonID:
			return p.advanceMergeStep()
		case mergeSkipButtonID:
			p.mergeState.DeleteLocalWorktree = false
			p.mergeState.DeleteLocalBranch = false
			p.mergeState.DeleteRemoteBranch = false
			p.mergeState.PullAfterMerge = false
			return p.advanceMergeStep()
		case "":
			// Modal handled internally (Tab cycling, checkbox toggle, etc.)
			if cmd != nil {
				return cmd
			}
			// Fall through to custom key handling
		default:
			// Unhandled action from modal
			return cmd
		}
	}

	switch msg.String() {
	case "esc", "q":
		p.cancelMergeWorkflow()
		p.clearMergeModal()
		return nil

	case "enter":
		// Continue to next step based on current step
		switch p.mergeState.Step {
		case MergeStepReviewDiff:
			// User reviewed diff, proceed to target branch selection
			return p.advanceMergeStep()
		case MergeStepTargetBranch:
			// User selected target branch, proceed to merge method
			return p.advanceMergeStep()
		case MergeStepMergeMethod:
			// User selected merge method, proceed
			return p.advanceMergeStep()
		case MergeStepWaitingMerge:
			// Manual check for merge status
			return p.checkPRMerged(p.mergeState.Worktree)
		case MergeStepPostMergeConfirmation:
			// Already handled by modal library above
			return nil
		case MergeStepDone:
			// Close modal
			p.cancelMergeWorkflow()
			p.clearMergeModal()
		}

	case "up", "k":
		switch p.mergeState.Step {
		case MergeStepTargetBranch:
			if p.mergeState.TargetBranchOption > 0 {
				p.mergeState.TargetBranchOption--
				p.clearMergeModal()
			}
		case MergeStepMergeMethod:
			// Select PR workflow (option 0)
			p.mergeState.MergeMethodOption = 0
			p.clearMergeModal() // Rebuild with new selection
		case MergeStepWaitingMerge:
			// Select "Delete worktree after merge"
			p.mergeState.DeleteAfterMerge = true
		}

	case "down", "j":
		switch p.mergeState.Step {
		case MergeStepTargetBranch:
			if p.mergeState.TargetBranchOption < len(p.mergeState.TargetBranches)-1 {
				p.mergeState.TargetBranchOption++
				p.clearMergeModal()
			}
		case MergeStepMergeMethod:
			// Select direct merge (option 1)
			p.mergeState.MergeMethodOption = 1
			p.clearMergeModal() // Rebuild with new selection
		case MergeStepWaitingMerge:
			// Select "Keep worktree"
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

	case "o":
		// Open PR in browser (only during WaitingMerge step with a PR URL)
		if p.mergeState.Step == MergeStepWaitingMerge && p.mergeState.PRURL != "" {
			return openInBrowser(p.mergeState.PRURL)
		}

	case "d":
		// Toggle error details in Done step
		if p.mergeState.Step == MergeStepDone &&
			p.mergeState.CleanupResults != nil &&
			p.mergeState.CleanupResults.PullError != nil {
			p.mergeState.CleanupResults.ShowErrorDetails = !p.mergeState.CleanupResults.ShowErrorDetails
			p.clearMergeModal() // Rebuild with toggled details
		}
		return nil

	case "r":
		// Rebase action (only when branch diverged in Done step)
		if p.mergeState.Step == MergeStepDone &&
			p.mergeState.CleanupResults != nil &&
			p.mergeState.CleanupResults.BranchDiverged {
			return p.executeRebaseResolution()
		}
		return nil

	case "m":
		// Merge action (only when branch diverged in Done step)
		// Note: 'm' in list view starts merge workflow, but here we're in MergeStepDone
		if p.mergeState.Step == MergeStepDone &&
			p.mergeState.CleanupResults != nil &&
			p.mergeState.CleanupResults.BranchDiverged {
			return p.executeMergeResolution()
		}
		return nil
	}
	return nil
}

// handleCommitForMergeKeys handles keys in the commit-before-merge modal.
func (p *Plugin) handleCommitForMergeKeys(msg tea.KeyMsg) tea.Cmd {
	p.ensureCommitForMergeModal()
	if p.commitForMergeModal == nil {
		return nil
	}

	// Clear error when input is focused and user types
	if p.commitForMergeModal.FocusedID() == commitForMergeInputID {
		p.mergeCommitState.Error = ""
	}

	action, cmd := p.commitForMergeModal.HandleKey(msg)
	switch action {
	case "cancel", commitForMergeCancelID:
		p.mergeCommitState = nil
		p.mergeCommitMessageInput = textinput.Model{}
		p.clearCommitForMergeModal()
		p.viewMode = ViewModeList
		return nil
	case commitForMergeActionID, commitForMergeCommitID:
		message := p.mergeCommitMessageInput.Value()
		if message == "" {
			p.mergeCommitState.Error = "Commit message cannot be empty"
			return nil
		}
		p.mergeCommitState.Error = ""
		return p.stageAllAndCommit(p.mergeCommitState.Worktree, message)
	}
	return cmd
}

// handleRenameShellKeys handles keys in the rename shell modal.
func (p *Plugin) handleRenameShellKeys(msg tea.KeyMsg) tea.Cmd {
	p.ensureRenameShellModal()
	if p.renameShellModal == nil {
		return nil
	}

	// Clear error on typing when input is focused
	if p.renameShellModal.FocusedID() == renameShellInputID {
		p.renameShellError = ""
	}

	action, cmd := p.renameShellModal.HandleKey(msg)

	switch action {
	case "cancel", renameShellCancelID:
		p.viewMode = ViewModeList
		p.clearRenameShellModal()
		return nil
	case renameShellActionID, renameShellRenameID:
		return p.executeRenameShell()
	}

	return cmd
}

// executeRenameShell performs the rename operation.
func (p *Plugin) executeRenameShell() tea.Cmd {
	newName := strings.TrimSpace(p.renameShellInput.Value())

	// Validation
	if newName == "" {
		p.renameShellError = "Name cannot be empty"
		return nil
	}

	if len(newName) > 50 {
		p.renameShellError = "Name too long (max 50 characters)"
		return nil
	}

	// Check for duplicates
	for _, shell := range p.shells {
		if shell.Name == newName && shell.TmuxName != p.renameShellSession.TmuxName {
			p.renameShellError = "Name already in use"
			return nil
		}
	}

	shell := p.renameShellSession
	tmuxName := shell.TmuxName

	// Clear modal state
	p.viewMode = ViewModeList
	p.clearRenameShellModal()

	return func() tea.Msg {
		// Rename is just a local state change - no tmux operation needed
		return RenameShellDoneMsg{
			TmuxName: tmuxName,
			NewName:  newName,
			Err:      nil,
		}
	}
}

// clearRenameShellModal clears rename modal state.
func (p *Plugin) clearRenameShellModal() {
	p.renameShellSession = nil
	p.renameShellInput = textinput.Model{}
	p.renameShellModal = nil
	p.renameShellModalWidth = 0
	p.renameShellError = ""
}

// handleFilePickerKeys handles keys in the file picker modal.
func (p *Plugin) handleFilePickerKeys(msg tea.KeyMsg) tea.Cmd {
	if p.multiFileDiff == nil || len(p.multiFileDiff.Files) == 0 {
		p.viewMode = ViewModeList
		return nil
	}

	fileCount := len(p.multiFileDiff.Files)

	switch msg.String() {
	case "esc", "q":
		p.viewMode = ViewModeList
		return nil
	case "j", "down":
		p.filePickerIdx++
		if p.filePickerIdx >= fileCount {
			p.filePickerIdx = fileCount - 1
		}
		return nil
	case "k", "up":
		p.filePickerIdx--
		if p.filePickerIdx < 0 {
			p.filePickerIdx = 0
		}
		return nil
	case "g":
		p.filePickerIdx = 0
		return nil
	case "G":
		p.filePickerIdx = fileCount - 1
		return nil
	case "enter":
		// Jump to selected file
		if p.filePickerIdx >= 0 && p.filePickerIdx < fileCount {
			p.previewOffset = p.multiFileDiff.Files[p.filePickerIdx].StartLine
		}
		p.viewMode = ViewModeList
		return nil
	}
	return nil
}

// openFilePicker opens the file picker modal.
func (p *Plugin) openFilePicker() tea.Cmd {
	if p.multiFileDiff == nil || len(p.multiFileDiff.Files) <= 1 {
		return nil
	}

	// Set initial selection to current file
	currentIdx := p.multiFileDiff.FileAtLine(p.previewOffset)
	if currentIdx < 0 {
		currentIdx = 0
	}
	p.filePickerIdx = currentIdx
	p.viewMode = ViewModeFilePicker
	return nil
}
