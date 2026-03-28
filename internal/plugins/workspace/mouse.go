package workspace

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
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
		regionKanbanCard, regionKanbanColumn, regionViewToggle,
		regionDiffTabDivider, regionTermPanelDivider, regionTermPanelContent,
		regionDiffTabFile, regionDiffTabCommit, regionDiffTabDiffPane, regionDiffTabMinimap,
		regionCommitFileItem, regionCommitFileBack, regionCommitFileDiffPane,
		regionDiffTabPreviewFile, regionDiffTabFileListPane:
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

	if p.viewMode == ViewModeAgentConfig {
		return p.handleAgentConfigModalMouse(msg)
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
		p.openPromptPicker(p.createPrompts, ViewModeCreate)
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
	agentOrder := p.createAgentOrder
	if len(agentOrder) == 0 {
		agentOrder = AgentTypeOrder
	}
	if idx, ok := parseIndexedID(createAgentItemPrefix, action); ok && idx < len(agentOrder) {
		p.createAgentIdx = idx
		p.createAgentType = agentOrder[idx]
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

func (p *Plugin) handleAgentConfigModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureAgentConfigModal()
	if p.agentConfigModal == nil {
		return nil
	}

	prevAgentIdx := p.agentConfigAgentIdx
	action := p.agentConfigModal.HandleMouse(msg, p.mouseHandler)

	// Sync agent type when list selection changes via mouse
	if p.agentConfigAgentIdx != prevAgentIdx {
		agentOrder := p.agentConfigAgentOrder
		if len(agentOrder) == 0 {
			agentOrder = AgentTypeOrder
		}
		if p.agentConfigAgentIdx >= 0 && p.agentConfigAgentIdx < len(agentOrder) {
			p.agentConfigAgentType = agentOrder[p.agentConfigAgentIdx]
		}
	}

	switch action {
	case "":
		return nil
	case "cancel", agentConfigCancelID:
		p.viewMode = ViewModeList
		p.clearAgentConfigModal()
		return nil
	case agentConfigPromptFieldID:
		p.openPromptPicker(p.agentConfigPrompts, ViewModeAgentConfig)
		return nil
	case agentConfigSubmitID:
		return p.executeAgentConfig()
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
	case mergePRURLID:
		if p.mergeState != nil && p.mergeState.PRURL != "" {
			return openInBrowser(p.mergeState.PRURL)
		}
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
	case ViewModeAgentConfig:
		// Modal library handles hover state internally
		return nil
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

	// Interactive mode: seamless pane switching between agent and terminal panel
	if p.viewMode == ViewModeInteractive {
		switch action.Region.ID {
		case regionTermPanelContent:
			p.activePane = PanePreview
			if p.interactiveState != nil && !p.interactiveState.TermPanel {
				// Switch from agent pane to terminal panel
				p.exitInteractiveMode()
				return p.enterTermPanelInteractiveMode()
			}
			// Already targeting terminal panel — forward click
			if p.interactiveState != nil && p.interactiveState.Active && !p.interactiveState.MouseReportingEnabled {
				return p.prepareInteractiveDrag(action)
			}
			return tea.Batch(p.forwardClickToTmux(action.X, action.Y), p.pollInteractivePaneImmediate())
		case regionPreviewPane:
			p.activePane = PanePreview
			if p.interactiveState != nil && p.interactiveState.TermPanel {
				// Switch from terminal panel to agent pane
				p.exitInteractiveMode()
				return p.enterInteractiveMode()
			}
			// Already targeting agent pane — forward click
			if p.interactiveState != nil && p.interactiveState.Active && !p.interactiveState.MouseReportingEnabled {
				return p.prepareInteractiveDrag(action)
			}
			return tea.Batch(p.forwardClickToTmux(action.X, action.Y), p.pollInteractivePaneImmediate())
		default:
			// Click outside both panes — exit interactive mode
			p.exitInteractiveMode()
			// Continue to handle the click normally
		}
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
		// When terminal panel is visible, single click focuses the agent sub-pane
		// (double-click enters interactive mode instead)
		if p.termPanelVisible {
			p.termPanelFocused = false
			return nil
		}
		// No terminal panel: single click enters interactive mode if Output tab active (td-7c2016)
		if p.previewTab == PreviewTabOutput {
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
	case regionDiffTabDivider:
		// Start drag for diff tab file list resizing (pixel-based width).
		// If no saved width, compute the effective default so drag starts from the actual position.
		startWidth := p.diffTabListWidth
		if startWidth <= 0 {
			startWidth = diffTabFileListWidth(p.width)
		}
		p.mouseHandler.StartDrag(action.X, action.Y, regionDiffTabDivider, startWidth)
	case regionTermPanelContent:
		// Click in terminal panel area - focus the terminal sub-pane (double-click enters interactive)
		p.activePane = PanePreview
		p.termPanelFocused = true
	case regionTermPanelDivider:
		// Start drag for terminal panel resizing (percentage-based).
		startSize := p.termPanelEffectiveSize()
		p.mouseHandler.StartDrag(action.X, action.Y, regionTermPanelDivider, startSize)
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
			p.termPanelFocused = false // Reset terminal panel focus when switching tabs
			p.autoScrollOutput = true
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
	case regionDiffTabFile:
		// Click on file in diff tab file list
		if idx, ok := action.Region.Data.(int); ok {
			p.activePane = PanePreview
			p.diffTabFocus = DiffTabFocusFileList
			if idx != p.diffTabCursor {
				oldCursor := p.diffTabCursor
				p.diffTabCursor = idx
				return p.onDiffTabCursorChanged(oldCursor)
			}
		}
	case regionDiffTabCommit:
		// Click on commit in diff tab
		if idx, ok := action.Region.Data.(int); ok {
			p.activePane = PanePreview
			p.diffTabFocus = DiffTabFocusFileList
			if idx != p.diffTabCursor {
				oldCursor := p.diffTabCursor
				p.diffTabCursor = idx
				return p.onDiffTabCursorChanged(oldCursor)
			}
		}
	case regionDiffTabDiffPane:
		// Click in diff pane - focus it
		p.activePane = PanePreview
		if p.diffTabFocus == DiffTabFocusCommitFiles || p.diffTabFocus == DiffTabFocusCommitDiff {
			p.diffTabFocus = DiffTabFocusCommitDiff
		} else {
			p.diffTabFocus = DiffTabFocusDiff
		}
	case regionDiffTabMinimap:
		// Click on minimap - jump to scroll position
		p.activePane = PanePreview
		ffd := p.fullFileDiff
		if ffd != nil {
			clickRow := action.Y - action.Region.Rect.Y
			totalLines := ffd.TotalLines()
			contentHeight := p.height - 6
			if contentHeight < 1 {
				contentHeight = 1
			}
			mmH := contentHeight
			if totalLines < mmH {
				mmH = totalLines
			}
			p.diffTabDiffScroll = gitstatus.MinimapScrollTarget(clickRow, mmH, totalLines, contentHeight)
		}
	case regionCommitFileBack:
		// Click on back button in commit drill-down
		p.activePane = PanePreview
		p.diffTabFocus = DiffTabFocusFileList
		p.commitDetail = nil
		p.commitFileDiffRaw = ""
		p.commitFileParsed = nil
		p.fullFileDiff = nil
	case regionCommitFileItem:
		// Click on file in commit file list
		if idx, ok := action.Region.Data.(int); ok {
			p.activePane = PanePreview
			p.diffTabFocus = DiffTabFocusCommitFiles
			if idx != p.commitFileCursor {
				p.commitFileCursor = idx
				p.commitFileDiffRaw = ""
				p.commitFileParsed = nil
				p.fullFileDiff = nil
				return p.loadSelectedCommitFileDiff()
			}
		}
	case regionCommitFileDiffPane:
		// Click in commit file diff pane - focus it
		p.activePane = PanePreview
		p.diffTabFocus = DiffTabFocusCommitDiff
	case regionDiffTabPreviewFile:
		// Click on file in commit preview (right pane) — drill into commit files view
		if idx, ok := action.Region.Data.(int); ok {
			p.activePane = PanePreview
			commitIdx := p.diffTabCursor - p.diffTabFileCount()
			if commitIdx >= 0 && commitIdx < len(p.commitStatusList) {
				commit := p.commitStatusList[commitIdx]
				p.diffTabFocus = DiffTabFocusCommitFiles
				p.commitDetail = nil
				p.commitFileCursor = idx
				p.commitFileScroll = 0
				p.commitFileDiffRaw = ""
				p.commitFileParsed = nil
				p.fullFileDiff = nil
				return p.loadCommitDetail(commit.Hash)
			}
		}
	case regionDiffTabFileListPane:
		// Click on empty space in the left pane — switch focus to file list
		p.activePane = PanePreview
		if p.diffTabFocus == DiffTabFocusCommitFiles || p.diffTabFocus == DiffTabFocusCommitDiff {
			p.diffTabFocus = DiffTabFocusCommitFiles
		} else {
			p.diffTabFocus = DiffTabFocusFileList
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
				p.openPromptPicker(p.createPrompts, ViewModeCreate)
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
		agentOrder := p.createAgentOrder
		if len(agentOrder) == 0 {
			agentOrder = AgentTypeOrder
		}
		if idx, ok := action.Region.Data.(int); ok {
			if idx >= 0 && idx < len(agentOrder) {
				p.createAgentType = agentOrder[idx]
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
	case regionTermPanelContent:
		// Double-click in terminal panel: enter terminal panel interactive mode
		p.activePane = PanePreview
		p.termPanelFocused = true
		return p.enterTermPanelInteractiveMode()
	case regionPreviewPane:
		// Double-click in preview pane: enter interactive mode if Output tab active (td-80d96956)
		// This provides seamless terminal integration without detaching from sidecar
		if p.previewTab == PreviewTabOutput {
			p.termPanelFocused = false
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
	case regionDiffTabFile:
		// Double-click on file - drill into diff pane
		if idx, ok := action.Region.Data.(int); ok {
			p.activePane = PanePreview
			oldCursor := p.diffTabCursor
			p.diffTabCursor = idx
			p.diffTabFocus = DiffTabFocusDiff
			p.diffTabDiffScroll = 0
			p.diffTabHorizScroll = 0
			if idx != oldCursor {
				return p.onDiffTabCursorChanged(oldCursor)
			}
		}
	case regionDiffTabCommit:
		// Double-click on commit - drill into commit files
		if idx, ok := action.Region.Data.(int); ok {
			p.activePane = PanePreview
			oldCursor := p.diffTabCursor
			p.diffTabCursor = idx
			commitIdx := idx - p.diffTabFileCount()
			if commitIdx >= 0 && commitIdx < len(p.commitStatusList) {
				commit := p.commitStatusList[commitIdx]
				p.diffTabFocus = DiffTabFocusCommitFiles
				p.commitDetail = nil
				p.commitFileCursor = 0
				p.commitFileScroll = 0
				p.commitFileDiffRaw = ""
				p.commitFileParsed = nil
				p.fullFileDiff = nil
				_ = oldCursor // cursor change handled by loading commit detail
				return p.loadCommitDetail(commit.Hash)
			}
		}
	case regionCommitFileItem:
		// Double-click on commit file - drill into its diff
		if idx, ok := action.Region.Data.(int); ok {
			p.activePane = PanePreview
			p.commitFileCursor = idx
			p.diffTabFocus = DiffTabFocusCommitDiff
			p.diffTabDiffScroll = 0
			p.diffTabHorizScroll = 0
			p.commitFileDiffRaw = ""
			p.commitFileParsed = nil
			p.fullFileDiff = nil
			return p.loadSelectedCommitFileDiff()
		}
	case regionDiffTabPreviewFile:
		// Double-click on preview file — same as single-click (drill into commit)
		if idx, ok := action.Region.Data.(int); ok {
			p.activePane = PanePreview
			commitIdx := p.diffTabCursor - p.diffTabFileCount()
			if commitIdx >= 0 && commitIdx < len(p.commitStatusList) {
				commit := p.commitStatusList[commitIdx]
				p.diffTabFocus = DiffTabFocusCommitFiles
				p.commitDetail = nil
				p.commitFileCursor = idx
				p.commitFileScroll = 0
				p.commitFileDiffRaw = ""
				p.commitFileParsed = nil
				p.fullFileDiff = nil
				return p.loadCommitDetail(commit.Hash)
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
	case regionTermPanelContent:
		// Scroll terminal panel output directly (position-based, not focus-based)
		if delta < 0 {
			p.termPanelScroll++
		} else {
			if p.termPanelScroll > 0 {
				p.termPanelScroll--
			}
		}
		return nil
	case regionDiffTabFile, regionDiffTabCommit, regionDiffTabFileListPane:
		// Scroll file/commit list in diff tab
		return p.scrollDiffTabFileList(delta)
	case regionDiffTabPreviewFile:
		// Scroll in commit preview — scroll the file list cursor
		return p.scrollDiffTabFileList(delta)
	case regionDiffTabDiffPane, regionDiffTabMinimap:
		// Scroll diff content
		p.diffTabDiffScroll += delta
		if p.diffTabDiffScroll < 0 {
			p.diffTabDiffScroll = 0
		}
		return nil
	case regionCommitFileItem, regionCommitFileBack:
		// Scroll commit file list
		return p.scrollDiffTabCommitFileList(delta)
	case regionCommitFileDiffPane:
		// Scroll commit file diff content
		p.diffTabDiffScroll += delta
		if p.diffTabDiffScroll < 0 {
			p.diffTabDiffScroll = 0
		}
		return nil
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

// handleMouseHorizontalScroll handles horizontal scroll events (shift+wheel or trackpad).
func (p *Plugin) handleMouseHorizontalScroll(action mouse.MouseAction) tea.Cmd {
	if action.Region == nil {
		return nil
	}
	delta := action.Delta
	switch action.Region.ID {
	case regionDiffTabDiffPane, regionDiffTabMinimap, regionCommitFileDiffPane:
		p.diffTabHorizScroll += delta
		if p.diffTabHorizScroll < 0 {
			p.diffTabHorizScroll = 0
		}
	}
	return nil
}

// scrollDiffTabFileList scrolls the file+commit list in the diff tab by moving the cursor.
// Uses lightweight sync updates only — no expensive async loads (prevents scroll freeze).
func (p *Plugin) scrollDiffTabFileList(delta int) tea.Cmd {
	totalItems := p.diffTabTotalItems()
	if totalItems == 0 {
		return nil
	}
	newCursor := p.diffTabCursor + delta
	if newCursor < 0 {
		newCursor = 0
	}
	if newCursor >= totalItems {
		newCursor = totalItems - 1
	}
	if newCursor != p.diffTabCursor {
		p.diffTabCursor = newCursor
		p.diffTabDiffScroll = 0
		p.diffTabHorizScroll = 0
		p.fullFileDiff = nil

		fileCount := p.diffTabFileCount()
		if p.diffTabCursor < fileCount {
			// Cursor on a file — sync update the parsed diff (cheap)
			p.diffTabParsedDiff = p.parsedDiffForCurrentFile()
			p.commitDetail = nil
		} else {
			// Cursor on a commit — just clear stale state, no async load
			p.diffTabParsedDiff = nil
		}
	}
	return nil
}

// scrollDiffTabCommitFileList scrolls the commit file list by moving the cursor.
// Uses lightweight sync updates only — no expensive async loads (prevents scroll freeze).
func (p *Plugin) scrollDiffTabCommitFileList(delta int) tea.Cmd {
	if p.commitDetail == nil || len(p.commitDetail.Files) == 0 {
		return nil
	}
	fileCount := len(p.commitDetail.Files)
	newCursor := p.commitFileCursor + delta
	if newCursor < 0 {
		newCursor = 0
	}
	if newCursor >= fileCount {
		newCursor = fileCount - 1
	}
	if newCursor != p.commitFileCursor {
		p.commitFileCursor = newCursor
		// Clear stale diff state — the diff will load on click or enter
		p.commitFileDiffRaw = ""
		p.commitFileParsed = nil
		p.fullFileDiff = nil
	}
	return nil
}

// scrollPreview scrolls the preview pane content.
func (p *Plugin) scrollPreview(delta int) tea.Cmd {
	// Unified scroll: delta < 0 = scroll up (toward top), delta > 0 = scroll down (toward bottom)
	// Output tab uses burst debouncing for trackpad scroll smoothness.
	if p.previewTab == PreviewTabOutput || p.shellSelected {
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
	}

	// Unified offset: 0 = top of content, higher = further down
	maxOffset := p.getMaxScrollOffset()
	if delta < 0 {
		// Scroll UP: move toward top of content
		if p.previewOffset > 0 {
			p.previewOffset--
		}
		if p.previewTab == PreviewTabOutput || p.shellSelected {
			p.autoScrollOutput = false
		}
	} else {
		// Scroll DOWN: move toward bottom of content
		if p.previewOffset < maxOffset {
			p.previewOffset++
		}
		if (p.previewTab == PreviewTabOutput || p.shellSelected) && p.previewOffset >= maxOffset {
			p.autoScrollOutput = true
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

	dragRegion := p.mouseHandler.DragRegion()
	p.lastDragRegion = dragRegion // Save for handleMouseDragEnd (EndDrag clears before DragEnd)

	switch dragRegion {
	case regionPaneDivider:
		// Calculate new sidebar width based on drag
		startValue := p.mouseHandler.DragStartValue()
		newWidth := startValue + (action.DragDX * 100 / p.width) // Convert px delta to %

		// Clamp to reasonable bounds (10% - 60%)
		if newWidth < 10 {
			newWidth = 10
		}
		if newWidth > 60 {
			newWidth = 60
		}
		p.sidebarWidth = newWidth
	case regionDiffTabDivider:
		// Calculate new diff tab file list width based on drag (pixel-based)
		startValue := p.mouseHandler.DragStartValue()
		newWidth := startValue + action.DragDX

		// Clamp to reasonable bounds
		if newWidth < 20 {
			newWidth = 20
		}
		maxW := p.width - 30
		if maxW < 20 {
			maxW = 20
		}
		if newWidth > maxW {
			newWidth = maxW
		}
		p.diffTabListWidth = newWidth
	case regionTermPanelDivider:
		// Calculate new terminal panel size based on drag (percentage-based).
		startValue := p.mouseHandler.DragStartValue()
		if p.termPanelLayout == TermPanelRight && p.width > 0 {
			// Right layout: drag horizontally, delta in X affects width %
			newSize := startValue - (action.DragDX * 100 / p.width)
			if newSize < termPanelMinSize {
				newSize = termPanelMinSize
			}
			if newSize > termPanelMaxSize {
				newSize = termPanelMaxSize
			}
			p.termPanelSize = newSize
		} else if p.termPanelLayout != TermPanelRight && p.height > 0 {
			// Bottom layout: drag vertically, delta in Y affects height %
			newSize := startValue - (action.DragDY * 100 / p.height)
			if newSize < termPanelMinSize {
				newSize = termPanelMinSize
			}
			if newSize > termPanelMaxSize {
				newSize = termPanelMaxSize
			}
			p.termPanelSize = newSize
		}
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

	// Persist widths based on what was being dragged
	switch p.lastDragRegion {
	case regionDiffTabDivider:
		_ = state.SetDiffTabFileListWidth(p.diffTabListWidth)
	case regionTermPanelDivider:
		_ = state.SetTermPanelSize(p.termPanelSize)
		// Resize both panes after drag-to-resize
		return tea.Batch(p.resizeTermPanelPaneCmd(), p.resizeSelectedPaneCmd())
	default:
		_ = state.SetWorkspaceSidebarWidth(p.sidebarWidth)
	}
	p.lastDragRegion = ""
	if p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active {
		// Poll captures cursor atomically - no separate query needed
		return tea.Batch(p.resizeInteractivePaneCmd(), p.pollInteractivePaneImmediate())
	}
	return p.resizeSelectedPaneCmd()
}
