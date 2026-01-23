package worktree

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
	case ViewModeFilePicker:
		return p.handleFilePickerKeys(msg)
	case ViewModeInteractive:
		return p.handleInteractiveKeys(msg)
	}
	return nil
}

// handleTypeSelectorKeys handles keys in the type selector modal.
func (p *Plugin) handleTypeSelectorKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab":
		// Cycle focus forward: options (0) -> Confirm (1) -> Cancel (2) -> options (0)
		p.typeSelectorFocus = (p.typeSelectorFocus + 1) % 3
		p.typeSelectorHover = -1
		p.typeSelectorButtonHover = 0
	case "shift+tab":
		// Cycle focus backward: options (0) -> Cancel (2) -> Confirm (1) -> options (0)
		p.typeSelectorFocus--
		if p.typeSelectorFocus < 0 {
			p.typeSelectorFocus = 2
		}
		p.typeSelectorHover = -1
		p.typeSelectorButtonHover = 0
	case "j", "down":
		// Only navigate options when focus is on options (focus=0)
		if p.typeSelectorFocus == 0 && p.typeSelectorIdx < 1 {
			p.typeSelectorIdx++
		}
		p.typeSelectorHover = -1 // Clear hover on keyboard nav
	case "k", "up":
		// Only navigate options when focus is on options (focus=0)
		if p.typeSelectorFocus == 0 && p.typeSelectorIdx > 0 {
			p.typeSelectorIdx--
		}
		p.typeSelectorHover = -1 // Clear hover on keyboard nav
	case "enter":
		// Execute based on current focus
		switch p.typeSelectorFocus {
		case 0, 1: // Options or Confirm button
			p.viewMode = ViewModeList
			p.typeSelectorHover = -1
			p.typeSelectorFocus = 0
			p.typeSelectorButtonHover = 0
			if p.typeSelectorIdx == 0 {
				// Create new shell
				return p.createNewShell()
			}
			// Open worktree create modal
			return p.openCreateModal()
		case 2: // Cancel button
			p.viewMode = ViewModeList
			p.typeSelectorIdx = 1    // Reset to default (Worktree)
			p.typeSelectorHover = -1 // Clear hover state
			p.typeSelectorFocus = 0  // Reset focus
			p.typeSelectorButtonHover = 0
		}
	case "esc", "q":
		p.viewMode = ViewModeList
		p.typeSelectorIdx = 1    // Reset to default (Worktree)
		p.typeSelectorHover = -1 // Clear hover state
		p.typeSelectorFocus = 0  // Reset focus
		p.typeSelectorButtonHover = 0
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
	// When the branch is the main branch, no checkboxes are shown.
	// Focus indices: 0=delete btn, 1=cancel btn
	if p.deleteIsMainBranch {
		switch msg.String() {
		case "tab", "j", "down", "l", "right":
			if p.deleteConfirmFocus == 0 {
				p.deleteConfirmFocus = 1
			}
		case "shift+tab", "k", "up", "h", "left":
			if p.deleteConfirmFocus == 1 {
				p.deleteConfirmFocus = 0
			}
		case "enter":
			if p.deleteConfirmFocus == 1 {
				return p.cancelDelete()
			}
			return p.executeDelete()
		case "D":
			return p.executeDelete()
		case "esc", "q":
			return p.cancelDelete()
		}
		return nil
	}

	// Calculate max focus based on whether remote branch exists
	// 0=local checkbox, 1=remote checkbox (if exists), 2 or 1=delete btn, 3 or 2=cancel btn
	maxFocus := 2 // local checkbox + delete btn + cancel btn
	if p.deleteHasRemote {
		maxFocus = 3 // local checkbox + remote checkbox + delete btn + cancel btn
	}

	deleteBtnFocus := 1
	cancelBtnFocus := 2
	if p.deleteHasRemote {
		deleteBtnFocus = 2
		cancelBtnFocus = 3
	}

	switch msg.String() {
	case "tab":
		p.deleteConfirmFocus = (p.deleteConfirmFocus + 1) % (maxFocus + 1)
	case "shift+tab":
		p.deleteConfirmFocus = (p.deleteConfirmFocus + maxFocus) % (maxFocus + 1)
	case "j", "down":
		if p.deleteConfirmFocus < maxFocus {
			p.deleteConfirmFocus++
		}
	case "k", "up":
		if p.deleteConfirmFocus > 0 {
			p.deleteConfirmFocus--
		}
	case " ":
		// Space toggles checkboxes
		if p.deleteConfirmFocus == 0 {
			p.deleteLocalBranchOpt = !p.deleteLocalBranchOpt
		} else if p.deleteHasRemote && p.deleteConfirmFocus == 1 {
			p.deleteRemoteBranchOpt = !p.deleteRemoteBranchOpt
		}
	case "enter":
		if p.deleteConfirmFocus == cancelBtnFocus {
			return p.cancelDelete()
		}
		if p.deleteConfirmFocus == deleteBtnFocus {
			return p.executeDelete()
		}
		// Space-like behavior on checkboxes with Enter
		if p.deleteConfirmFocus == 0 {
			p.deleteLocalBranchOpt = !p.deleteLocalBranchOpt
		} else if p.deleteHasRemote && p.deleteConfirmFocus == 1 {
			p.deleteRemoteBranchOpt = !p.deleteRemoteBranchOpt
		}
	case "D":
		// Power user shortcut - immediate confirm
		return p.executeDelete()
	case "esc", "q":
		return p.cancelDelete()
	case "h", "left":
		// Navigate between buttons when on button row
		if p.deleteConfirmFocus == cancelBtnFocus {
			p.deleteConfirmFocus = deleteBtnFocus
		}
	case "l", "right":
		// Navigate between buttons when on button row
		if p.deleteConfirmFocus == deleteBtnFocus {
			p.deleteConfirmFocus = cancelBtnFocus
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
	branch := wt.Branch
	deleteLocal := p.deleteLocalBranchOpt
	deleteRemote := p.deleteRemoteBranchOpt && p.deleteHasRemote
	workDir := p.ctx.WorkDir

	// Kill tmux session if it exists (before deleting worktree)
	sessionName := tmuxSessionPrefix + sanitizeName(name)
	if sessionExists(sessionName) {
		exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}
	delete(p.managedSessions, sessionName)
	globalPaneCache.remove(sessionName)

	// Clear modal state
	p.viewMode = ViewModeList
	p.deleteConfirmWorktree = nil
	p.deleteConfirmButtonHover = 0
	p.deleteLocalBranchOpt = false
	p.deleteRemoteBranchOpt = false
	p.deleteHasRemote = false
	p.deleteIsMainBranch = false
	p.deleteConfirmFocus = 0

	// Clear preview pane content
	p.diffContent = ""
	p.diffRaw = ""
	p.cachedTaskID = ""
	p.cachedTask = nil

	return func() tea.Msg {
		var warnings []string

		// Delete the worktree first
		err := doDeleteWorktree(workDir, path)
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
	p.deleteConfirmWorktree = nil
	p.deleteConfirmButtonHover = 0
	p.deleteLocalBranchOpt = false
	p.deleteRemoteBranchOpt = false
	p.deleteHasRemote = false
	p.deleteIsMainBranch = false
	p.deleteConfirmFocus = 0
	return nil
}

// handleConfirmDeleteShellKeys handles keys in the shell delete confirmation modal.
func (p *Plugin) handleConfirmDeleteShellKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab", "j", "down", "l", "right":
		// Toggle between Delete (0) and Cancel (1)
		p.deleteShellConfirmFocus = (p.deleteShellConfirmFocus + 1) % 2
	case "shift+tab", "k", "up", "h", "left":
		// Toggle between Delete (0) and Cancel (1)
		p.deleteShellConfirmFocus = (p.deleteShellConfirmFocus + 1) % 2
	case "enter":
		if p.deleteShellConfirmFocus == 1 {
			return p.cancelShellDelete()
		}
		return p.executeShellDelete()
	case "D":
		// Power user shortcut - immediate confirm
		return p.executeShellDelete()
	case "esc", "q":
		return p.cancelShellDelete()
	}
	return nil
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
	p.deleteConfirmShell = nil
	p.deleteShellConfirmFocus = 0
	p.deleteShellConfirmButtonHover = 0

	return p.killShellSessionByName(sessionName)
}

// cancelShellDelete closes the shell delete confirmation modal without deleting.
func (p *Plugin) cancelShellDelete() tea.Cmd {
	p.viewMode = ViewModeList
	p.deleteConfirmShell = nil
	p.deleteShellConfirmFocus = 0
	p.deleteShellConfirmButtonHover = 0
	return nil
}

// handleListKeys handles keys in list view (and kanban view).
func (p *Plugin) handleListKeys(msg tea.KeyMsg) tea.Cmd {
	// Clear any deletion warnings on key interaction
	p.deleteWarnings = nil

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
		p.previewOffset = 10000 // Large offset, will be clamped in render
	case "G":
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
	case "n":
		// Open type selector modal to choose between Shell and Worktree
		p.viewMode = ViewModeTypeSelector
		p.typeSelectorIdx = 1         // Default to Worktree (more common)
		p.typeSelectorHover = -1      // No hover initially (0-based: -1 = none)
		p.typeSelectorFocus = 0       // Focus on options by default
		p.typeSelectorButtonHover = 0 // No button hover initially
		return nil
	case "D":
		// Check if deleting a shell session
		if p.shellSelected && p.selectedShellIdx >= 0 && p.selectedShellIdx < len(p.shells) {
			p.viewMode = ViewModeConfirmDeleteShell
			p.deleteConfirmShell = p.shells[p.selectedShellIdx]
			p.deleteShellConfirmFocus = 0       // Focus delete button
			p.deleteShellConfirmButtonHover = 0 // Clear hover
			return nil
		}
		// Otherwise delete worktree
		wt := p.selectedWorktree()
		if wt == nil {
			return nil
		}
		p.viewMode = ViewModeConfirmDelete
		p.deleteConfirmWorktree = wt
		p.deleteConfirmButtonHover = 0
		p.deleteLocalBranchOpt = false // Default: don't delete branches
		p.deleteRemoteBranchOpt = false
		p.deleteHasRemote = false
		p.deleteIsMainBranch = isMainBranch(p.ctx.WorkDir, wt.Branch)
		if p.deleteIsMainBranch {
			// Main branch is protected: skip branch options, focus delete button directly
			p.deleteConfirmFocus = 0 // Delete button is focus 0 when no checkboxes
			return nil
		}
		p.deleteConfirmFocus = 1 // Focus delete button (index 1 when no remote)
		// Check for remote branch existence asynchronously
		return p.checkRemoteBranch(wt)
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
		if p.viewMode == ViewModeInteractive {
			// Poll captures cursor atomically - no separate query needed
			return tea.Batch(p.resizeInteractivePaneCmd(), p.pollInteractivePaneImmediate())
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
		// Enter interactive mode (tmux input passthrough) - feature gated
		// Only available in preview pane with output tab when there's an active session
		if p.activePane == PanePreview && p.previewTab == PreviewTabOutput {
			return p.enterInteractiveMode()
		}
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
			return nil
		} else if p.activePane == PaneSidebar || p.viewMode == ViewModeKanban {
			if p.viewMode == ViewModeList {
				p.viewMode = ViewModeKanban
				p.syncListToKanban()
				return nil
			} else if p.viewMode == ViewModeKanban {
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
		p.agentChoiceIdx = 0         // Default to attach
		p.agentChoiceButtonFocus = 0 // Start with options focused
		p.agentChoiceButtonHover = 0 // Clear hover state
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
			p.renameShellInput.Focus()
			p.renameShellInput.CharLimit = 50
			p.renameShellInput.Width = 30
			p.renameShellFocus = 0
			p.renameShellButtonHover = 0
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
			// Validate name before creating
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
		// Validate branch name in real-time
		name := p.createNameInput.Value()
		p.branchNameValid, p.branchNameErrors, p.branchNameSanitized = ValidateBranchName(name)
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

	switch msg.String() {
	case "esc", "q":
		p.cancelMergeWorkflow()
		return nil

	case "enter":
		// Continue to next step based on current step
		switch p.mergeState.Step {
		case MergeStepReviewDiff:
			// User reviewed diff, proceed to merge method selection
			return p.advanceMergeStep()
		case MergeStepMergeMethod:
			// User selected merge method, proceed
			return p.advanceMergeStep()
		case MergeStepWaitingMerge:
			// Manual check for merge status
			return p.checkPRMerged(p.mergeState.Worktree)
		case MergeStepPostMergeConfirmation:
			// User confirmed cleanup options
			if p.mergeState.ConfirmationFocus == 5 {
				// Skip All button - uncheck everything
				p.mergeState.DeleteLocalWorktree = false
				p.mergeState.DeleteLocalBranch = false
				p.mergeState.DeleteRemoteBranch = false
				p.mergeState.PullAfterMerge = false
			}
			return p.advanceMergeStep()
		case MergeStepDone:
			// Close modal
			p.cancelMergeWorkflow()
		}

	case "up", "k":
		if p.mergeState.Step == MergeStepMergeMethod {
			// Select PR workflow (option 0)
			p.mergeState.MergeMethodOption = 0
		} else if p.mergeState.Step == MergeStepWaitingMerge {
			// Select "Delete worktree after merge"
			p.mergeState.DeleteAfterMerge = true
		} else if p.mergeState.Step == MergeStepPostMergeConfirmation {
			// Navigate checkboxes/buttons
			if p.mergeState.ConfirmationFocus > 0 {
				p.mergeState.ConfirmationFocus--
			}
		}

	case "down", "j":
		if p.mergeState.Step == MergeStepMergeMethod {
			// Select direct merge (option 1)
			p.mergeState.MergeMethodOption = 1
		} else if p.mergeState.Step == MergeStepWaitingMerge {
			// Select "Keep worktree"
			p.mergeState.DeleteAfterMerge = false
		} else if p.mergeState.Step == MergeStepPostMergeConfirmation {
			// Navigate checkboxes/buttons (0-3=checkboxes, 4=confirm, 5=skip)
			if p.mergeState.ConfirmationFocus < 5 {
				p.mergeState.ConfirmationFocus++
			}
		}

	case " ":
		// Space toggles checkboxes in confirmation step
		if p.mergeState.Step == MergeStepPostMergeConfirmation {
			switch p.mergeState.ConfirmationFocus {
			case 0:
				p.mergeState.DeleteLocalWorktree = !p.mergeState.DeleteLocalWorktree
			case 1:
				p.mergeState.DeleteLocalBranch = !p.mergeState.DeleteLocalBranch
			case 2:
				p.mergeState.DeleteRemoteBranch = !p.mergeState.DeleteRemoteBranch
			case 3:
				p.mergeState.PullAfterMerge = !p.mergeState.PullAfterMerge
			}
		}

	case "tab":
		// Tab cycles focus in confirmation step (0-3=checkboxes, 4=confirm, 5=skip)
		if p.mergeState.Step == MergeStepPostMergeConfirmation {
			p.mergeState.ConfirmationFocus = (p.mergeState.ConfirmationFocus + 1) % 6
		}

	case "shift+tab":
		// Shift+Tab reverse cycles focus
		if p.mergeState.Step == MergeStepPostMergeConfirmation {
			p.mergeState.ConfirmationFocus = (p.mergeState.ConfirmationFocus + 5) % 6
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

// handleRenameShellKeys handles keys in the rename shell modal.
func (p *Plugin) handleRenameShellKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		p.viewMode = ViewModeList
		p.clearRenameShellModal()
		return nil
	case "tab":
		p.renameShellInput.Blur()
		p.renameShellFocus = (p.renameShellFocus + 1) % 3
		if p.renameShellFocus == 0 {
			p.renameShellInput.Focus()
		}
		return nil
	case "shift+tab":
		p.renameShellInput.Blur()
		p.renameShellFocus = (p.renameShellFocus + 2) % 3
		if p.renameShellFocus == 0 {
			p.renameShellInput.Focus()
		}
		return nil
	case "enter":
		if p.renameShellFocus == 2 {
			// Cancel button
			p.viewMode = ViewModeList
			p.clearRenameShellModal()
			return nil
		}
		if p.renameShellFocus == 1 || p.renameShellFocus == 0 {
			// Confirm button or input field
			return p.executeRenameShell()
		}
		return nil
	}

	// Delegate to textinput when focused
	if p.renameShellFocus == 0 {
		p.renameShellError = "" // Clear error on typing
		var cmd tea.Cmd
		p.renameShellInput, cmd = p.renameShellInput.Update(msg)
		return cmd
	}
	return nil
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
	p.renameShellFocus = 0
	p.renameShellButtonHover = 0
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
