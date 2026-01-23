package workspace

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/state"
)

// isModalViewMode returns true when a modal overlay is active (not List, Kanban, or Interactive).
func (p *Plugin) isModalViewMode() bool {
	switch p.viewMode {
	case ViewModeList, ViewModeKanban, ViewModeInteractive:
		return false
	default:
		return true
	}
}

// isBackgroundRegion returns true for regions registered by renderListView()
// that should not respond to mouse events when a modal is open.
func isBackgroundRegion(regionID string) bool {
	switch regionID {
	case regionSidebar, regionPreviewPane, regionPaneDivider,
		regionWorktreeItem, regionPreviewTab,
		regionCreateWorktreeButton, regionShellsPlusButton, regionWorkspacesPlusButton,
		regionKanbanCard, regionKanbanColumn, regionViewToggle:
		return true
	default:
		return false
	}
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
	// Guard: absorb background region hovers when a modal is open (td-f63097).
	if p.isModalViewMode() && action.Region != nil && isBackgroundRegion(action.Region.ID) {
		return nil
	}

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
	case ViewModeConfirmDeleteShell:
		if action.Region == nil {
			p.deleteShellConfirmButtonHover = 0
			return nil
		}
		switch action.Region.ID {
		case regionDeleteShellConfirmDelete:
			p.deleteShellConfirmButtonHover = 1
		case regionDeleteShellConfirmCancel:
			p.deleteShellConfirmButtonHover = 2
		default:
			p.deleteShellConfirmButtonHover = 0
		}
	case ViewModeRenameShell:
		if action.Region == nil {
			p.renameShellButtonHover = 0
			return nil
		}
		switch action.Region.ID {
		case regionRenameShellInput:
			p.renameShellButtonHover = 0 // Clear button hover when hovering input
		case regionRenameShellConfirm:
			p.renameShellButtonHover = 1
		case regionRenameShellCancel:
			p.renameShellButtonHover = 2
		default:
			p.renameShellButtonHover = 0
		}
	case ViewModePromptPicker:
		if p.promptPicker == nil {
			return nil
		}
		if action.Region == nil {
			p.promptPicker.ClearHover()
			return nil
		}
		switch action.Region.ID {
		case regionPromptItem:
			if idx, ok := action.Region.Data.(int); ok {
				p.promptPicker.SetHover(idx)
			}
		case regionPromptFilter:
			p.promptPicker.ClearHover()
		default:
			p.promptPicker.ClearHover()
		}
	case ViewModeMerge:
		if action.Region == nil {
			p.mergeMethodHover = 0
			p.mergeConfirmCheckboxHover = 0
			p.mergeConfirmButtonHover = 0
			return nil
		}
		switch action.Region.ID {
		case regionMergeMethodOption:
			if idx, ok := action.Region.Data.(int); ok {
				p.mergeMethodHover = idx + 1 // 1=Create PR, 2=Direct Merge
			}
			p.mergeConfirmCheckboxHover = 0
			p.mergeConfirmButtonHover = 0
		case regionMergeConfirmCheckbox:
			if idx, ok := action.Region.Data.(int); ok {
				p.mergeConfirmCheckboxHover = idx + 1 // 1-4 for checkboxes
			}
			p.mergeMethodHover = 0
			p.mergeConfirmButtonHover = 0
		case regionMergeConfirmButton:
			p.mergeConfirmButtonHover = 1 // Clean Up
			p.mergeMethodHover = 0
			p.mergeConfirmCheckboxHover = 0
		case regionMergeSkipButton:
			p.mergeConfirmButtonHover = 2 // Skip All
			p.mergeMethodHover = 0
			p.mergeConfirmCheckboxHover = 0
		default:
			p.mergeMethodHover = 0
			p.mergeConfirmCheckboxHover = 0
			p.mergeConfirmButtonHover = 0
		}
	case ViewModeTypeSelector:
		if action.Region == nil {
			p.typeSelectorHover = -1 // No hover
			p.typeSelectorButtonHover = 0
			return nil
		}
		switch action.Region.ID {
		case regionTypeSelectorOption:
			if idx, ok := action.Region.Data.(int); ok {
				p.typeSelectorHover = idx // 0=Shell, 1=Worktree (consistent with typeSelectorIdx)
			}
			p.typeSelectorButtonHover = 0
		case regionTypeSelectorConfirm:
			p.typeSelectorButtonHover = 1
			p.typeSelectorHover = -1
		case regionTypeSelectorCancel:
			p.typeSelectorButtonHover = 2
			p.typeSelectorHover = -1
		default:
			p.typeSelectorHover = -1
			p.typeSelectorButtonHover = 0
		}
	default:
		p.createButtonHover = 0
		p.agentChoiceButtonHover = 0
		p.deleteConfirmButtonHover = 0
		p.mergeMethodHover = 0
		p.mergeConfirmCheckboxHover = 0
		p.mergeConfirmButtonHover = 0
		// Handle sidebar header button hover
		p.hoverNewButton = false
		p.hoverShellsPlusButton = false
		p.hoverWorkspacesPlusButton = false
		if action.Region != nil {
			switch action.Region.ID {
			case regionCreateWorktreeButton:
				p.hoverNewButton = true
			case regionShellsPlusButton:
				p.hoverShellsPlusButton = true
			case regionWorkspacesPlusButton:
				p.hoverWorkspacesPlusButton = true
			}
		}
	}
	return nil
}

// handleMouseClick handles single click events.
func (p *Plugin) handleMouseClick(action mouse.MouseAction) tea.Cmd {
	if action.Region == nil {
		return nil
	}

	// Guard: absorb background region clicks when a modal is open (td-f63097).
	// Without this, clicks on empty modal space fall through to background regions
	// registered by renderListView(), causing enterInteractiveMode/pane switches.
	if p.isModalViewMode() && isBackgroundRegion(action.Region.ID) {
		return nil
	}

	// Exit interactive mode when clicking outside preview pane (td-80d96956)
	if p.viewMode == ViewModeInteractive && action.Region.ID != regionPreviewPane {
		p.exitInteractiveMode()
		// Continue to handle the click normally
	}
	if p.viewMode == ViewModeInteractive && action.Region.ID == regionPreviewPane {
		p.activePane = PanePreview
		if p.interactiveState != nil && p.interactiveState.Active && !p.interactiveState.MouseReportingEnabled {
			return p.prepareInteractiveDrag(action)
		}
		return tea.Batch(p.forwardClickToTmux(action.X, action.Y), p.pollInteractivePaneImmediate())
	}

	switch action.Region.ID {
	case regionCreateWorktreeButton:
		// Click on [New] button - open type selector modal
		return p.openCreateModal()
	case regionShellsPlusButton:
		// Click on Shells [+] button - immediately create a new shell
		return p.createNewShell()
	case regionWorkspacesPlusButton:
		// Click on Worktrees [+] button - open new worktree modal directly
		return p.openCreateModal()
	case regionSidebar:
		p.activePane = PaneSidebar
	case regionPreviewPane:
		p.activePane = PanePreview
		// Single click in preview pane: enter interactive mode if Output tab active (td-7c2016)
		// This provides seamless terminal integration - click to interact
		if p.previewTab == PreviewTabOutput {
			// Check for active session (worktree or shell)
			if p.shellSelected {
				shell := p.getSelectedShell()
				if shell != nil && shell.Agent != nil {
					return p.enterInteractiveMode()
				}
			} else {
				wt := p.selectedWorktree()
				if wt != nil && wt.Agent != nil && wt.Agent.TmuxSession != "" {
					return p.enterInteractiveMode()
				}
			}
		}
	case regionPaneDivider:
		// Start drag for pane resizing
		p.mouseHandler.StartDrag(action.X, action.Y, regionPaneDivider, p.sidebarWidth)
	case regionWorktreeItem:
		// Click on worktree or shell entry - select it
		if idx, ok := action.Region.Data.(int); ok {
			if idx < 0 {
				// Shell entry clicked (negative index: -1 -> shells[0], -2 -> shells[1], etc.)
				shellIdx := -(idx + 1)
				if shellIdx >= 0 && shellIdx < len(p.shells) {
					if !p.shellSelected || p.selectedShellIdx != shellIdx {
						p.shellSelected = true
						p.selectedShellIdx = shellIdx
						p.previewOffset = 0
						p.previewHorizOffset = 0
						p.autoScrollOutput = true
						p.taskLoading = false // Reset task loading on selection change (td-3668584f)
						// Exit interactive mode when switching selection (td-fc758e88)
						p.exitInteractiveMode()
						p.saveSelectionState()
					}
					p.activePane = PaneSidebar
					return p.loadSelectedContent()
				}
			} else if idx >= 0 && idx < len(p.worktrees) {
				// Worktree clicked
				if p.shellSelected || p.selectedIdx != idx {
					p.shellSelected = false
					p.selectedIdx = idx
					p.previewOffset = 0
					p.previewHorizOffset = 0
					p.autoScrollOutput = true
					p.taskLoading = false // Reset task loading on selection change (td-3668584f)
					// Exit interactive mode when switching selection (td-fc758e88)
					p.exitInteractiveMode()
					p.saveSelectionState()
				}
				p.ensureVisible()
				p.activePane = PaneSidebar
				return p.loadSelectedContent()
			}
		}
	case regionPreviewTab:
		// Click on preview tab
		if idx, ok := action.Region.Data.(int); ok && idx >= 0 && idx <= 2 {
			prevTab := p.previewTab
			p.previewTab = PreviewTab(idx)
			p.previewOffset = 0
			p.previewHorizOffset = 0
			p.autoScrollOutput = true
			if prevTab == PreviewTabOutput && p.previewTab != PreviewTabOutput {
				p.clearInteractiveSelection()
			}

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
	case regionDeleteLocalBranchCheck:
		// Click on local branch checkbox (disabled for main branch)
		if !p.deleteIsMainBranch {
			p.deleteLocalBranchOpt = !p.deleteLocalBranchOpt
			p.deleteConfirmFocus = 0
		}
	case regionDeleteRemoteBranchCheck:
		// Click on remote branch checkbox (only if remote exists, disabled for main branch)
		if !p.deleteIsMainBranch && p.deleteHasRemote {
			p.deleteRemoteBranchOpt = !p.deleteRemoteBranchOpt
			p.deleteConfirmFocus = 1
		}
	case regionDeleteConfirmDelete:
		// Click delete button
		return p.executeDelete()
	case regionDeleteConfirmCancel:
		// Click cancel button
		return p.cancelDelete()
	case regionDeleteShellConfirmDelete:
		// Click delete button in shell delete modal
		return p.executeShellDelete()
	case regionDeleteShellConfirmCancel:
		// Click cancel button in shell delete modal
		return p.cancelShellDelete()
	case regionRenameShellInput:
		// Click on rename input field
		p.renameShellFocus = 0
		p.renameShellInput.Focus()
	case regionRenameShellConfirm:
		// Click confirm button in rename shell modal
		return p.executeRenameShell()
	case regionRenameShellCancel:
		// Click cancel button in rename shell modal
		p.viewMode = ViewModeList
		p.clearRenameShellModal()
	case regionKanbanCard:
		// Click on kanban card - select it
		if data, ok := action.Region.Data.(kanbanCardData); ok {
			oldShellSelected := p.shellSelected
			oldShellIdx := p.selectedShellIdx
			oldWorktreeIdx := p.selectedIdx
			p.kanbanCol = data.col
			p.kanbanRow = data.row
			p.syncKanbanToList()
			p.applyKanbanSelectionChange(oldShellSelected, oldShellIdx, oldWorktreeIdx)
			return p.loadSelectedContent()
		}
	case regionKanbanColumn:
		// Click on column header - focus that column
		if colIdx, ok := action.Region.Data.(int); ok {
			oldShellSelected := p.shellSelected
			oldShellIdx := p.selectedShellIdx
			oldWorktreeIdx := p.selectedIdx
			p.kanbanCol = colIdx
			p.kanbanRow = 0
			p.syncKanbanToList()
			if p.applyKanbanSelectionChange(oldShellSelected, oldShellIdx, oldWorktreeIdx) {
				return p.loadSelectedContent()
			}
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
	case regionCreateBackdrop:
		// Click outside create modal - close it
		p.viewMode = ViewModeList
		p.clearCreateModal()
	case regionCreateModalBody:
		// Click inside modal but not on a form element - absorb
	case regionCreateInput:
		// Click on input field in create modal
		if focusIdx, ok := action.Region.Data.(int); ok {
			p.blurCreateInputs()
			p.createFocus = focusIdx
			p.focusCreateInput()

			// If clicking prompt field, open the picker
			if focusIdx == 2 {
				p.promptPicker = NewPromptPicker(p.createPrompts, p.width, p.height)
				p.viewMode = ViewModePromptPicker
			}
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
	case regionMergeMethodOption:
		// Click on merge method option (0=Create PR, 1=Direct Merge)
		if idx, ok := action.Region.Data.(int); ok && p.mergeState != nil &&
			p.mergeState.Step == MergeStepMergeMethod {
			p.mergeState.MergeMethodOption = idx
		}
	case regionMergeRadio:
		// Click on merge radio option (0=delete, 1=keep)
		if idx, ok := action.Region.Data.(int); ok && p.mergeState != nil {
			p.mergeState.DeleteAfterMerge = (idx == 0)
		}
	case regionMergeConfirmCheckbox:
		// Click on confirmation checkbox (0-2=cleanup, 3=pull)
		if idx, ok := action.Region.Data.(int); ok && p.mergeState != nil &&
			p.mergeState.Step == MergeStepPostMergeConfirmation {
			switch idx {
			case 0:
				p.mergeState.DeleteLocalWorktree = !p.mergeState.DeleteLocalWorktree
			case 1:
				p.mergeState.DeleteLocalBranch = !p.mergeState.DeleteLocalBranch
			case 2:
				p.mergeState.DeleteRemoteBranch = !p.mergeState.DeleteRemoteBranch
			case 3:
				p.mergeState.PullAfterMerge = !p.mergeState.PullAfterMerge
			}
			p.mergeState.ConfirmationFocus = idx
		}
	case regionMergeConfirmButton:
		// Click on Clean Up button (focus index 4)
		if p.mergeState != nil && p.mergeState.Step == MergeStepPostMergeConfirmation {
			p.mergeState.ConfirmationFocus = 4
			return p.advanceMergeStep()
		}
	case regionMergeSkipButton:
		// Click on Skip All button (focus index 5)
		if p.mergeState != nil && p.mergeState.Step == MergeStepPostMergeConfirmation {
			p.mergeState.DeleteLocalWorktree = false
			p.mergeState.DeleteLocalBranch = false
			p.mergeState.DeleteRemoteBranch = false
			p.mergeState.PullAfterMerge = false
			p.mergeState.ConfirmationFocus = 5
			return p.advanceMergeStep()
		}
	case regionPromptFilter:
		// Click on filter input in prompt picker - focus it
		if p.promptPicker != nil {
			p.promptPicker.FocusFilter()
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
	case regionTypeSelectorOption:
		// Click on type selector option - select it (visual only)
		if idx, ok := action.Region.Data.(int); ok {
			p.typeSelectorIdx = idx
			p.typeSelectorFocus = 0 // Keep focus on options
		}
	case regionTypeSelectorConfirm:
		// Click confirm button - execute selected option
		p.viewMode = ViewModeList
		if p.typeSelectorIdx == 0 {
			// Create new shell
			return p.createNewShell()
		}
		// Open worktree create modal
		return p.openCreateModal()
	case regionTypeSelectorCancel:
		// Click cancel button - close modal
		p.viewMode = ViewModeList
		p.typeSelectorIdx = 1    // Reset to default (Worktree)
		p.typeSelectorHover = -1 // Clear hover state
		p.typeSelectorFocus = 0  // Reset focus
		p.typeSelectorButtonHover = 0
	}
	return nil
}

// handleMouseDoubleClick handles double-click events.
func (p *Plugin) handleMouseDoubleClick(action mouse.MouseAction) tea.Cmd {
	// Guard: ignore double-clicks when a modal is open (td-f63097).
	if p.isModalViewMode() {
		return nil
	}
	if action.Region == nil {
		return nil
	}

	switch action.Region.ID {
	case regionPreviewPane:
		// Double-click in preview pane: enter interactive mode if Output tab active (td-80d96956)
		// This provides seamless terminal integration without detaching from sidecar
		if p.previewTab == PreviewTabOutput {
			// Check for active session (worktree or shell)
			if p.shellSelected {
				shell := p.getSelectedShell()
				if shell != nil && shell.Agent != nil {
					return p.enterInteractiveMode()
				}
			} else {
				wt := p.selectedWorktree()
				if wt != nil && wt.Agent != nil && wt.Agent.TmuxSession != "" {
					return p.enterInteractiveMode()
				}
			}
		}
	case regionWorktreeItem:
		// Double-click on worktree or shell - attach to tmux session if exists
		if idx, ok := action.Region.Data.(int); ok {
			if idx < 0 {
				// Double-click on shell entry (negative index: -1 -> shells[0], -2 -> shells[1], etc.)
				shellIdx := -(idx + 1)
				if shellIdx >= 0 && shellIdx < len(p.shells) {
					p.shellSelected = true
					p.selectedShellIdx = shellIdx
					p.saveSelectionState()
					return p.ensureShellAndAttachByIndex(shellIdx)
				}
			} else if idx >= 0 && idx < len(p.worktrees) {
				p.shellSelected = false
				p.selectedIdx = idx
				p.saveSelectionState()
				wt := p.worktrees[idx]
				if wt.Agent != nil {
					p.attachedSession = wt.Name
					return p.AttachToSession(wt)
				}
				p.activePane = PanePreview
			}
		}
	case regionKanbanCard:
		// Double-click on kanban card - attach to tmux session if agent running
		if data, ok := action.Region.Data.(kanbanCardData); ok {
			oldShellSelected := p.shellSelected
			oldShellIdx := p.selectedShellIdx
			oldWorktreeIdx := p.selectedIdx
			p.kanbanCol = data.col
			p.kanbanRow = data.row
			p.syncKanbanToList()
			p.applyKanbanSelectionChange(oldShellSelected, oldShellIdx, oldWorktreeIdx)
			if data.col == kanbanShellColumnIndex {
				if shell := p.kanbanShellAt(data.row); shell != nil {
					return p.ensureShellAndAttachByIndex(data.row)
				}
			} else {
				wt := p.getKanbanWorktree(data.col, data.row)
				if wt != nil && wt.Agent != nil {
					p.attachedSession = wt.Name
					return p.AttachToSession(wt)
				}
			}
		}
	}
	return nil
}

// handleMouseScroll handles scroll wheel events.
func (p *Plugin) handleMouseScroll(action mouse.MouseAction) tea.Cmd {
	// Guard: absorb background region scrolls when a modal is open (td-f63097).
	if p.isModalViewMode() && (action.Region == nil || isBackgroundRegion(action.Region.ID)) {
		return nil
	}

	delta := action.Delta
	if action.Type == mouse.ActionScrollUp {
		delta = -1
	} else {
		delta = 1
	}

	// In interactive mode, always forward scroll to tmux copy-mode.
	// The user is interacting with the pane; exit interactive mode first to scroll sidebar.
	if p.viewMode == ViewModeInteractive {
		return p.forwardScrollToTmux(delta)
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
	// Guard: absorb horizontal scroll when a modal is open (td-f63097).
	if p.isModalViewMode() {
		return nil
	}

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

// scrollSidebar scrolls the sidebar list (shells + worktrees).
func (p *Plugin) scrollSidebar(delta int) tea.Cmd {
	// Check if there's anything to scroll through
	if len(p.shells) == 0 && len(p.worktrees) == 0 {
		return nil
	}

	// Track old selection to detect change
	oldShellSelected := p.shellSelected
	oldShellIdx := p.selectedShellIdx
	oldWorktreeIdx := p.selectedIdx

	// Delegate to moveCursor which handles multi-shell navigation properly
	p.moveCursor(delta)

	// Check if selection actually changed
	selectionChanged := p.shellSelected != oldShellSelected ||
		(p.shellSelected && p.selectedShellIdx != oldShellIdx) ||
		(!p.shellSelected && p.selectedIdx != oldWorktreeIdx)

	if selectionChanged {
		return p.loadSelectedContent()
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
	if p.kanbanCol < 0 || p.kanbanCol >= kanbanColumnCount() {
		return nil
	}
	count := p.kanbanColumnItemCount(p.kanbanCol, columns)

	if count == 0 {
		return nil
	}

	newRow := p.kanbanRow + delta
	if newRow < 0 {
		newRow = 0
	}
	maxRow := count - 1
	if newRow > maxRow {
		newRow = maxRow
	}

	if newRow != p.kanbanRow {
		oldShellSelected := p.shellSelected
		oldShellIdx := p.selectedShellIdx
		oldWorktreeIdx := p.selectedIdx
		p.kanbanRow = newRow
		p.syncKanbanToList()
		p.applyKanbanSelectionChange(oldShellSelected, oldShellIdx, oldWorktreeIdx)
		return p.loadSelectedContent()
	}
	return nil
}

// handleMouseDrag handles drag motion events.
func (p *Plugin) handleMouseDrag(action mouse.MouseAction) tea.Cmd {
	// Guard: prevent pane resizing while a modal is open (td-f63097).
	if p.isModalViewMode() {
		return nil
	}

	switch p.mouseHandler.DragRegion() {
	case regionPaneDivider:
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
	case regionPreviewPane:
		if p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active &&
			!p.interactiveState.MouseReportingEnabled {
			return p.handleInteractiveSelectionDrag(action)
		}
	}
	return nil
}

// handleMouseDragEnd handles the end of a drag operation.
func (p *Plugin) handleMouseDragEnd() tea.Cmd {
	// Guard: ignore drag-end when a modal is open (td-f63097).
	if p.isModalViewMode() {
		return nil
	}

	if p.interactiveSelectionActive {
		return p.finishInteractiveSelection()
	}

	// Persist sidebar width
	_ = state.SetWorkspaceSidebarWidth(p.sidebarWidth)
	if p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active {
		// Poll captures cursor atomically - no separate query needed
		return tea.Batch(p.resizeInteractivePaneCmd(), p.pollInteractivePaneImmediate())
	}
	return p.resizeSelectedPaneCmd()
}
