package workspace

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
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
	// Record the time of every mouse event, including motion. This is used by
	// handleInteractiveKeys to suppress bare "[" runes that arrive shortly after
	// mouse activity — see the split-CSI comment in handleInteractiveKeys.
	p.lastMouseEventTime = time.Now()

	if p.viewMode == ViewModeCreate {
		return p.handleCreateModalMouse(msg)
	}

	if p.viewMode == ViewModeRenameShell {
		return p.handleRenameShellModalMouse(msg)
	}

	if p.viewMode == ViewModeConfirmDelete {
		return p.handleConfirmDeleteModalMouse(msg)
	}

	if p.viewMode == ViewModeConfirmDeleteShell {
		return p.handleConfirmDeleteShellModalMouse(msg)
	}

	if p.viewMode == ViewModePromptPicker {
		return p.handlePromptPickerModalMouse(msg)
	}

	if p.viewMode == ViewModeTypeSelector {
		return p.handleTypeSelectorModalMouse(msg)
	}

	if p.viewMode == ViewModeAgentChoice {
		return p.handleAgentChoiceModalMouse(msg)
	}

	if p.viewMode == ViewModeFetchPR {
		return p.handleFetchPRModalMouse(msg)
	}

	if p.viewMode == ViewModeMerge {
		return p.handleMergeModalMouse(msg)
	}

	if p.viewMode == ViewModeCommitForMerge {
		return p.handleCommitForMergeModalMouse(msg)
	}

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
	case mouse.ActionHover:
		return p.handleMouseHover(action)
	}
	return nil
}

func (p *Plugin) handleCreateModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureCreateModal()
	if p.createModal == nil {
		return nil
	}

	action := p.createModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case createSubmitID:
		return p.validateAndCreateWorktree()
	case createCancelID, "cancel":
		p.viewMode = ViewModeList
		p.clearCreateModal()
		return nil
	case createPromptFieldID:
		p.createFocus = 2
		p.syncCreateModalFocus()
		p.promptPicker = NewPromptPicker(p.createPrompts, p.width, p.height)
		p.clearPromptPickerModal()
		p.viewMode = ViewModePromptPicker
		return nil
	case createNameFieldID:
		p.createFocus = 0
		p.focusCreateInput()
		p.syncCreateModalFocus()
		return nil
	case createBaseFieldID:
		p.createFocus = 1
		p.focusCreateInput()
		p.syncCreateModalFocus()
		return nil
	case createTaskFieldID:
		p.createFocus = 3
		p.focusCreateInput()
		p.syncCreateModalFocus()
		return nil
	case createSkipPermissionsID:
		p.createFocus = 5
		p.createSkipPermissions = !p.createSkipPermissions
		p.syncCreateModalFocus()
		return nil
	}

	if idx, ok := parseIndexedID(createBranchItemPrefix, action); ok && idx < len(p.branchFiltered) {
		p.createBaseBranchInput.SetValue(p.branchFiltered[idx])
		p.branchFiltered = nil
		p.createFocus = 1
		p.syncCreateModalFocus()
		return nil
	}
	if idx, ok := parseIndexedID(createTaskItemPrefix, action); ok && idx < len(p.taskSearchFiltered) {
		task := p.taskSearchFiltered[idx]
		p.createTaskID = task.ID
		p.createTaskTitle = task.Title
		p.createFocus = 3
		p.syncCreateModalFocus()
		return nil
	}
	if idx, ok := parseIndexedID(createAgentItemPrefix, action); ok && idx < len(AgentTypeOrder) {
		p.createAgentIdx = idx
		p.createAgentType = AgentTypeOrder[idx]
		p.createFocus = 4
		p.syncCreateModalFocus()
		return nil
	}

	return nil
}

func (p *Plugin) handleRenameShellModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureRenameShellModal()
	if p.renameShellModal == nil {
		return nil
	}

	action := p.renameShellModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel", renameShellCancelID:
		p.viewMode = ViewModeList
		p.clearRenameShellModal()
		return nil
	case renameShellActionID, renameShellRenameID:
		return p.executeRenameShell()
	}
	return nil
}

func (p *Plugin) handleConfirmDeleteModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureConfirmDeleteModal()
	if p.deleteConfirmModal == nil {
		return nil
	}

	action := p.deleteConfirmModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel", deleteConfirmCancelID:
		return p.cancelDelete()
	case deleteConfirmDeleteID:
		return p.executeDelete()
	case deleteConfirmLocalID:
		if !p.deleteIsMainBranch {
			p.deleteLocalBranchOpt = !p.deleteLocalBranchOpt
		}
	case deleteConfirmRemoteID:
		if !p.deleteIsMainBranch && p.deleteHasRemote {
			p.deleteRemoteBranchOpt = !p.deleteRemoteBranchOpt
		}
	}
	return nil
}

func (p *Plugin) handleConfirmDeleteShellModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureConfirmDeleteShellModal()
	if p.deleteShellModal == nil {
		return nil
	}

	action := p.deleteShellModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel", deleteShellConfirmCancelID:
		return p.cancelShellDelete()
	case deleteShellConfirmDeleteID:
		return p.executeShellDelete()
	}
	return nil
}

func (p *Plugin) handleTypeSelectorModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureTypeSelectorModal()
	if p.typeSelectorModal == nil {
		return nil
	}

	// Track selection before to detect changes
	prevIdx := p.typeSelectorIdx

	action := p.typeSelectorModal.HandleMouse(msg, p.mouseHandler)

	// Modal width depends on selection - rebuild if changed
	if p.typeSelectorIdx != prevIdx {
		p.typeSelectorModalWidth = 0 // Force rebuild
	}

	switch action {
	case "":
		return nil
	case "cancel", typeSelectorCancelID:
		p.viewMode = ViewModeList
		p.clearTypeSelectorModal()
		return nil
	case typeSelectorConfirmID, "type-shell", "type-workspace":
		return p.executeTypeSelectorConfirm()
	}
	return nil
}

func (p *Plugin) handlePromptPickerModalMouse(msg tea.MouseMsg) tea.Cmd {
	if p.promptPicker == nil {
		return nil
	}

	p.ensurePromptPickerModal()
	if p.promptPickerModal == nil {
		return nil
	}

	action := p.promptPickerModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel":
		return func() tea.Msg { return PromptCancelledMsg{} }
	case promptPickerFilterID:
		p.promptPicker.filterFocused = true
		p.syncPromptPickerFocus()
		return nil
	}

	if idx, ok := parsePromptPickerItemID(action); ok {
		p.promptPicker.selectedIdx = idx
		p.promptPicker.filterFocused = false
		p.syncPromptPickerFocus()
		return p.promptPickerSelectCmd()
	}

	return nil
}

func (p *Plugin) handleAgentChoiceModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureAgentChoiceModal()
	if p.agentChoiceModal == nil {
		return nil
	}

	action := p.agentChoiceModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel", agentChoiceCancelID:
		p.viewMode = ViewModeList
		p.clearAgentChoiceModal()
		return nil
	case agentChoiceActionID, agentChoiceConfirmID, "agent-choice-attach", "agent-choice-restart":
		return p.executeAgentChoice()
	}
	return nil
}

func (p *Plugin) handleFetchPRModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureFetchPRModal()
	if p.fetchPRModal == nil {
		return nil
	}

	action := p.fetchPRModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "cancel":
		p.viewMode = ViewModeList
		p.clearFetchPRState()
		return nil
	}
	return nil
}

func (p *Plugin) handleMergeModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureMergeModal()
	if p.mergeModal == nil {
		return nil
	}

	action := p.mergeModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel", "dismiss":
		p.cancelMergeWorkflow()
		p.clearMergeModal()
		return nil
	case mergeMethodActionID, mergeTargetActionID, mergeCleanUpButtonID:
		// Advance to next step
		return p.advanceMergeStep()
	case mergeSkipButtonID:
		// Skip all cleanup
		if p.mergeState != nil {
			p.mergeState.DeleteLocalWorktree = false
			p.mergeState.DeleteLocalBranch = false
			p.mergeState.DeleteRemoteBranch = false
			p.mergeState.PullAfterMerge = false
		}
		return p.advanceMergeStep()
	}
	return nil
}

func (p *Plugin) handleCommitForMergeModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureCommitForMergeModal()
	if p.commitForMergeModal == nil {
		return nil
	}

	action := p.commitForMergeModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
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
				switch idx {
				case 6:
					p.createButtonHover = 1 // Create
				case 7:
					p.createButtonHover = 2 // Cancel
				}
			}
		default:
			p.createButtonHover = 0
		}
	case ViewModeAgentChoice:
		// Modal library handles hover state internally
		return nil
	case ViewModeRenameShell:
		// Modal library handles hover state internally
		return nil
	case ViewModeMerge:
		// Modal library handles hover state internally
		return nil
	case ViewModeCommitForMerge:
		// Modal library handles hover state internally
		return nil
	case ViewModeTypeSelector:
		// Modal library handles hover state internally
		return nil
	default:
		p.createButtonHover = 0
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
		return p.createNewShell("")
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
						p.autoScrollOutput = true
						p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot for new selection
						p.taskLoading = false        // Reset task loading on selection change (td-3668584f)
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
					p.autoScrollOutput = true
					p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot for new selection
					p.taskLoading = false        // Reset task loading on selection change (td-3668584f)
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
			p.autoScrollOutput = true
			p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot when switching tabs
			if prevTab == PreviewTabOutput && p.previewTab != PreviewTabOutput {
				p.selection.Clear()
			}

			// Load content for the selected tab
			switch p.previewTab {
			case PreviewTabDiff:
				return p.loadSelectedDiff()
			case PreviewTabTask:
				return p.loadTaskDetailsIfNeeded()
			}
		}
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
				p.clearPromptPickerModal()
				p.viewMode = ViewModePromptPicker
			}
		}
	case regionCreateDropdown:
		// Click on dropdown item
		if data, ok := action.Region.Data.(dropdownItemData); ok {
			switch data.field {
			case 1:
				// Branch selection
				if data.idx >= 0 && data.idx < len(p.branchFiltered) {
					p.createBaseBranchInput.SetValue(p.branchFiltered[data.idx])
					p.branchFiltered = nil
				}
			case 3:
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
			switch idx {
			case 6:
				return p.createWorktree()
			case 7:
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

	var delta int
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
		now := time.Now()

		// Detect and handle scroll bursts (fast trackpad scrolling)
		timeSinceLastScroll := now.Sub(p.lastScrollTime)
		if timeSinceLastScroll < scrollBurstTimeout {
			p.scrollBurstCount++
		} else {
			// Burst ended, reset
			p.scrollBurstCount = 1
			p.scrollBurstStarted = now
		}

		// During burst mode, use more aggressive debouncing
		debounceInterval := scrollDebounceInterval
		if p.scrollBurstCount > scrollBurstThreshold {
			debounceInterval = scrollBurstDebounce
		}

		if timeSinceLastScroll < debounceInterval {
			return nil
		}
		p.lastScrollTime = now

		if delta < 0 {
			// Scroll UP: pause auto-scroll, show older content
			p.autoScrollOutput = false
			p.captureScrollBaseLineCount() // td-f7c8be: prevent bounce on poll
			p.previewOffset++
		} else {
			// Scroll DOWN: show newer content
			if p.previewOffset > 0 {
				p.previewOffset--
				if p.previewOffset == 0 {
					p.autoScrollOutput = true    // Resume auto-scroll when at bottom
					p.resetScrollBaseLineCount() // td-f7c8be: clear snapshot
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

	if p.selection.Active {
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
