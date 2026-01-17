package worktree

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/state"
)

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
	default:
		p.createButtonHover = 0
		p.agentChoiceButtonHover = 0
		p.deleteConfirmButtonHover = 0
		p.mergeMethodHover = 0
		p.mergeConfirmCheckboxHover = 0
		p.mergeConfirmButtonHover = 0
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
	case regionDeleteLocalBranchCheck:
		// Click on local branch checkbox
		p.deleteLocalBranchOpt = !p.deleteLocalBranchOpt
		p.deleteConfirmFocus = 0
	case regionDeleteRemoteBranchCheck:
		// Click on remote branch checkbox (only if remote exists)
		if p.deleteHasRemote {
			p.deleteRemoteBranchOpt = !p.deleteRemoteBranchOpt
			p.deleteConfirmFocus = 1
		}
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
	}
	return nil
}

// handleMouseDoubleClick handles double-click events.
func (p *Plugin) handleMouseDoubleClick(action mouse.MouseAction) tea.Cmd {
	if action.Region == nil {
		return nil
	}

	switch action.Region.ID {
	case regionPreviewPane:
		// Double-click in preview pane attaches to tmux session if agent running
		wt := p.selectedWorktree()
		if wt != nil && wt.Agent != nil && wt.Agent.TmuxSession != "" {
			p.attachedSession = wt.Name
			return p.AttachToSession(wt)
		}
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
