package filebrowser

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/state"
)

// Mouse region identifiers
const (
	regionTreePane    = "tree-pane"    // Overall tree pane for scroll targeting
	regionPreviewPane = "preview-pane" // Overall preview pane for scroll targeting
	regionPaneDivider = "pane-divider" // Border between tree and preview
	regionTreeItem    = "tree-item"    // Individual file/folder (Data: visible index)
	regionQuickOpen   = "quick-open"   // Quick open modal item (Data: match index)
	regionPreviewLine = "preview-line" // Individual preview line (Data: line index)
	regionPreviewTab  = "preview-tab"  // Preview tab (Data: tab index)

	// File operation modal buttons
	regionFileOpConfirm    = "file-op-confirm"    // Confirm/Create/Delete/Yes button
	regionFileOpCancel     = "file-op-cancel"     // Cancel/No button
	regionFileOpSuggestion = "file-op-suggestion" // Path suggestion item (Data: index)
)

// handleMouse processes mouse events and dispatches to appropriate handlers.
func (p *Plugin) handleMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	// Handle exit confirmation dialog if active
	if p.showExitConfirmation {
		return p.handleExitConfirmationMouse(msg)
	}

	// Handle inline edit mode - mouse events for editor and click-away detection
	if p.inlineEditMode && p.inlineEditor != nil && p.inlineEditor.IsActive() {
		action := p.mouseHandler.HandleMouse(msg)

		// Helper to handle click-away: save edit state to tab and detach
		// The tmux session keeps running in background; returning to the tab restores it
		handleClickAway := func(regionID string, regionData interface{}) (*Plugin, tea.Cmd) {
			p.inlineEditorDragging = false // Cancel any drag in progress

			// Check if the editor session is still alive
			if !p.isInlineEditSessionAlive() {
				// Session is dead - just clean up and process click
				p.exitInlineEditMode()
				p.pendingClickRegion = regionID
				p.pendingClickData = regionData
				return p.processPendingClickAction()
			}

			// Session is alive - save state to tab and detach (no confirmation)
			p.saveEditStateToTab()
			p.clearPluginEditState()

			// Process the click directly
			p.pendingClickRegion = regionID
			p.pendingClickData = regionData
			return p.processPendingClickAction()
		}

		// Handle click (mouse press) - start potential drag
		if action.Type == mouse.ActionClick {
			// Check for tab row clicks FIRST (before forwarding to vim)
			// This is needed because regionPreviewPane encompasses tabs
			if len(p.tabs) > 1 {
				inputBarHeight := 0
				if p.contentSearchMode || p.fileOpMode != FileOpNone || p.lineJumpMode {
					inputBarHeight = 1
					if p.fileOpMode != FileOpNone && p.fileOpError != "" {
						inputBarHeight = 2
					}
				}
				tabY := inputBarHeight + 1 // pane border + first content row
				previewX := 0
				if p.treeVisible {
					p.calculatePaneWidths()
					previewX = p.treeWidth + dividerWidth
				}
				// Check if click is in tab row area (allow +/- 1 for tolerance)
				if action.Y >= tabY-1 && action.Y <= tabY+1 && action.X >= previewX {
					// Find which tab was clicked based on X position
					tabX := previewX + 2 // left border + padding
					for _, hit := range p.tabHits {
						hitStart := tabX + hit.X
						hitEnd := hitStart + hit.Width
						if action.X >= hitStart && action.X < hitEnd {
							return handleClickAway(regionPreviewTab, hit.Index)
						}
					}
					// Fallback: find the closest tab based on X position
					if len(p.tabHits) > 0 {
						clickX := action.X - tabX
						bestIdx := p.tabHits[0].Index
						bestDist := -1
						for _, hit := range p.tabHits {
							mid := hit.X + hit.Width/2
							dist := clickX - mid
							if dist < 0 {
								dist = -dist
							}
							if bestDist < 0 || dist < bestDist {
								bestDist = dist
								bestIdx = hit.Index
							}
						}
						return handleClickAway(regionPreviewTab, bestIdx)
					}
					return handleClickAway(regionPreviewTab, nil)
				}
			}

			if action.Region != nil {
				switch action.Region.ID {
				case regionTreePane, regionTreeItem, regionPreviewTab:
					return handleClickAway(action.Region.ID, action.Region.Data)
				case regionPreviewPane, regionPreviewLine:
					// Forward mouse press to vim and start tracking drag
					col, row, ok := p.calculateInlineEditorMouseCoords(action.X, action.Y)
					if ok {
						p.inlineEditorDragging = true
						return p, p.forwardMousePressToInlineEditor(col, row)
					}
					cmd := p.inlineEditor.Update(msg)
					return p, cmd
				}
			}

			// Fallback: use X position to detect tree pane clicks
			if p.treeVisible && action.X < p.treeWidth {
				return handleClickAway(regionTreePane, nil)
			}
		}

		// Handle mouse motion/hover - forward drag events to vim for text selection
		if action.Type == mouse.ActionHover && p.inlineEditorDragging {
			col, row, ok := p.calculateInlineEditorMouseCoords(action.X, action.Y)
			if ok {
				return p, p.forwardMouseDragToInlineEditor(col, row)
			}
		}

		// Handle mouse release - end drag
		if msg.Action == tea.MouseActionRelease {
			if p.inlineEditorDragging {
				p.inlineEditorDragging = false
				col, row, ok := p.calculateInlineEditorMouseCoords(msg.X, msg.Y)
				if ok {
					return p, p.forwardMouseReleaseToInlineEditor(col, row)
				}
			}
			return p, nil
		}

		// Forward other mouse events to tty model
		cmd := p.inlineEditor.Update(msg)
		return p, cmd
	}

	// Handle project search modal first if active
	if p.projectSearchMode {
		return p.handleProjectSearchMouse(msg)
	}

	// Handle quick open modal if active
	if p.quickOpenMode {
		return p.handleQuickOpenMouse(msg)
	}

	// Handle info modal if active
	if p.infoMode {
		return p.handleInfoModalMouse(msg)
	}

	// Handle blame modal if active
	if p.blameMode {
		return p.handleBlameModalMouse(msg)
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
	return p, nil
}

// handleMouseHover handles mouse hover for visual feedback.
func (p *Plugin) handleMouseHover(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	// Only track hover for file operation modal buttons
	if p.fileOpMode == FileOpNone {
		p.fileOpButtonHover = 0
		return p, nil
	}

	if action.Region == nil {
		p.fileOpButtonHover = 0
		return p, nil
	}

	switch action.Region.ID {
	case regionFileOpConfirm:
		p.fileOpButtonHover = 1
	case regionFileOpCancel:
		p.fileOpButtonHover = 2
	case regionFileOpSuggestion:
		// Highlight suggestion on hover
		if idx, ok := action.Region.Data.(int); ok {
			p.fileOpSuggestionIdx = idx
		}
		p.fileOpButtonHover = 0
	default:
		p.fileOpButtonHover = 0
	}
	return p, nil
}

// handleMouseClick handles single click actions.
func (p *Plugin) handleMouseClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if action.Region == nil {
		return p, nil
	}

	switch action.Region.ID {
	case regionTreeItem:
		idx, ok := action.Region.Data.(int)
		if !ok {
			return p, nil
		}
		p.treeCursor = idx
		p.activePane = PaneTree
		p.ensureTreeCursorVisible()
		return p, p.loadPreviewForCursor()

	case regionTreePane:
		p.activePane = PaneTree
		return p, nil

	case regionPreviewPane:
		p.activePane = PanePreview
		p.clearTextSelection() // Clear selection when clicking empty area
		return p, nil

	case regionPreviewLine:
		if lineIdx, ok := action.Region.Data.(int); ok {
			p.activePane = PanePreview
			// Initialize selection on this single line
			p.textSelectionActive = false
			p.textSelectionStart = lineIdx
			p.textSelectionEnd = lineIdx
			p.textSelectionAnchor = lineIdx
			// Start drag tracking for potential drag-select
			p.mouseHandler.StartDrag(action.X, action.Y, regionPreviewLine, lineIdx)
		}
		return p, nil

	case regionPreviewTab:
		if idx, ok := action.Region.Data.(int); ok {
			p.activePane = PanePreview
			return p, p.switchTab(idx)
		}
		return p, nil

	case regionPaneDivider:
		// Start drag with current tree width
		p.mouseHandler.StartDrag(action.X, action.Y, regionPaneDivider, p.treeWidth)
		return p, nil

	case regionFileOpConfirm:
		// Click on confirm button in file op modal
		if p.fileOpMode != FileOpNone {
			plug, cmd := p.executeFileOp()
			return plug.(*Plugin), cmd
		}
		return p, nil

	case regionFileOpCancel:
		// Click on cancel button in file op modal
		if p.fileOpMode != FileOpNone {
			p.fileOpMode = FileOpNone
			p.fileOpTarget = nil
			p.fileOpError = ""
			p.fileOpShowSuggestions = false
			p.fileOpConfirmDelete = false
			p.fileOpConfirmCreate = false
			return p, nil
		}
		return p, nil

	case regionFileOpSuggestion:
		// Click on a path suggestion item
		if idx, ok := action.Region.Data.(int); ok {
			if idx >= 0 && idx < len(p.fileOpSuggestions) {
				p.fileOpTextInput.SetValue(p.fileOpSuggestions[idx])
				p.fileOpShowSuggestions = false
				p.fileOpSuggestionIdx = -1
			}
		}
		return p, nil
	}

	return p, nil
}

// handleMouseDoubleClick handles double click actions.
func (p *Plugin) handleMouseDoubleClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if action.Region == nil || action.Region.ID != regionTreeItem {
		return p, nil
	}

	idx, ok := action.Region.Data.(int)
	if !ok {
		return p, nil
	}

	node := p.tree.GetNode(idx)
	if node == nil {
		return p, nil
	}

	if node.IsDir {
		// Toggle folder expand/collapse
		_ = p.tree.Toggle(node)
		p.treeCursor = idx
		p.ensureTreeCursorVisible()
		return p, nil
	}

	// Open file in editor (same as 'e' key)
	return p, p.openFile(node.Path)
}

// handleMouseScroll handles scroll wheel actions.
func (p *Plugin) handleMouseScroll(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	// Determine which pane to scroll based on region or X position
	inTreePane := false
	if action.Region != nil {
		inTreePane = action.Region.ID == regionTreePane || action.Region.ID == regionTreeItem
	} else {
		inTreePane = action.X < p.treeWidth
	}

	delta := 3
	if action.Type == mouse.ActionScrollUp {
		delta = -3
	}

	if inTreePane {
		// Scroll tree by moving cursor
		p.treeCursor += delta
		if p.treeCursor < 0 {
			p.treeCursor = 0
		} else if p.treeCursor >= p.tree.Len() {
			p.treeCursor = p.tree.Len() - 1
		}
		p.ensureTreeCursorVisible()
		return p, p.loadPreviewForCursor()
	}

	// Scroll preview pane
	lines := p.getPreviewLines()
	visibleHeight := p.visibleContentHeight()
	maxScroll := len(lines) - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}

	p.previewScroll += delta
	if p.previewScroll < 0 {
		p.previewScroll = 0
	} else if p.previewScroll > maxScroll {
		p.previewScroll = maxScroll
	}

	return p, nil
}

// handleMouseDrag handles drag actions (pane resizing and text selection).
func (p *Plugin) handleMouseDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	switch p.mouseHandler.DragRegion() {
	case regionPaneDivider:
		return p.handlePaneDividerDrag(action)
	case regionPreviewLine:
		return p.handlePreviewSelectionDrag(action)
	}
	return p, nil
}

// handlePaneDividerDrag handles dragging the pane divider to resize.
func (p *Plugin) handlePaneDividerDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	startValue := p.mouseHandler.DragStartValue()
	newWidth := startValue + action.DragDX

	// Clamp to reasonable bounds (match calculatePaneWidths logic)
	available := p.width - 6 - dividerWidth
	minWidth := 20
	maxWidth := available - 40 // Leave at least 40 for preview
	if maxWidth < minWidth {
		maxWidth = minWidth
	}
	if newWidth < minWidth {
		newWidth = minWidth
	} else if newWidth > maxWidth {
		newWidth = maxWidth
	}

	p.treeWidth = newWidth
	p.previewWidth = available - p.treeWidth

	return p, nil
}

// handlePreviewSelectionDrag handles drag-to-select in the preview pane.
func (p *Plugin) handlePreviewSelectionDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	p.textSelectionActive = true

	// Calculate Y offset to preview content
	inputBarHeight := 0
	if p.searchMode || p.contentSearchMode || p.fileOpMode != FileOpNone {
		inputBarHeight = 1
		if p.fileOpMode != FileOpNone && p.fileOpError != "" {
			inputBarHeight = 2
		}
	}
	previewContentStartY := inputBarHeight + 3 // border + header

	// Map Y coordinate to line index
	currentLine := (action.Y - previewContentStartY) + p.previewScroll

	// Clamp to valid range
	if currentLine < 0 {
		currentLine = 0
	}
	maxLine := len(p.previewLines) - 1
	if maxLine < 0 {
		return p, nil
	}
	if currentLine > maxLine {
		currentLine = maxLine
	}

	// Update selection based on anchor
	if currentLine < p.textSelectionAnchor {
		p.textSelectionStart = currentLine
		p.textSelectionEnd = p.textSelectionAnchor
	} else {
		p.textSelectionStart = p.textSelectionAnchor
		p.textSelectionEnd = currentLine
	}

	return p, nil
}

// handleMouseDragEnd handles the end of a drag operation.
func (p *Plugin) handleMouseDragEnd() (*Plugin, tea.Cmd) {
	switch p.mouseHandler.DragRegion() {
	case regionPaneDivider:
		// Save the current tree width to state
		_ = state.SetFileBrowserTreeWidth(p.treeWidth)
	case regionPreviewLine:
		// Selection complete - keep selection visible but mark as inactive
		p.textSelectionActive = false
	}
	return p, nil
}

// handleQuickOpenMouse handles mouse events in quick open modal.
func (p *Plugin) handleQuickOpenMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	action := p.mouseHandler.HandleMouse(msg)

	switch action.Type {
	case mouse.ActionClick:
		if action.Region != nil && action.Region.ID == regionQuickOpen {
			if idx, ok := action.Region.Data.(int); ok {
				p.quickOpenCursor = idx
			}
		}
		return p, nil

	case mouse.ActionDoubleClick:
		if action.Region != nil && action.Region.ID == regionQuickOpen {
			if idx, ok := action.Region.Data.(int); ok {
				p.quickOpenCursor = idx
				plug, cmd := p.selectQuickOpenMatch()
				return plug.(*Plugin), cmd
			}
		}
		return p, nil

	case mouse.ActionScrollUp, mouse.ActionScrollDown:
		// Scroll quick open list
		delta := 3
		if action.Type == mouse.ActionScrollUp {
			delta = -3
		}
		p.quickOpenCursor += delta
		if p.quickOpenCursor < 0 {
			p.quickOpenCursor = 0
		} else if p.quickOpenCursor >= len(p.quickOpenMatches) {
			p.quickOpenCursor = len(p.quickOpenMatches) - 1
		}
		return p, nil
	}

	return p, nil
}

// handleProjectSearchMouse handles mouse events in project search modal.
func (p *Plugin) handleProjectSearchMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	p.ensureProjectSearchModal()
	state := p.projectSearchState
	if state == nil || p.projectSearchModal == nil {
		return p, nil
	}

	if msg.Action == tea.MouseActionMotion {
		p.projectSearchModal.HandleMouse(msg, p.mouseHandler)
		return p, nil
	}

	action := p.mouseHandler.HandleMouse(msg)

	switch action.Type {
	case mouse.ActionClick:
		return p.handleProjectSearchClick(action)

	case mouse.ActionDoubleClick:
		return p.handleProjectSearchDoubleClick(action)

	case mouse.ActionScrollUp, mouse.ActionScrollDown:
		// Scroll results list
		delta := 3
		if action.Type == mouse.ActionScrollUp {
			delta = -3
		}
		maxIdx := state.FlatLen() - 1
		state.Cursor += delta
		if state.Cursor < 0 {
			state.Cursor = 0
		} else if state.Cursor > maxIdx {
			state.Cursor = maxIdx
		}
		return p, nil
	}

	return p, nil
}

// handleProjectSearchClick handles single clicks in project search.
func (p *Plugin) handleProjectSearchClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	state := p.projectSearchState
	if action.Region == nil || state == nil {
		return p, nil
	}

	switch action.Region.ID {
	case "modal-backdrop":
		p.projectSearchMode = false
		p.projectSearchState = nil
		p.clearProjectSearchModal()
		return p, nil
	case "modal-body":
		return p, nil
	}

	p.projectSearchModal.SetFocus(action.Region.ID)

	switch action.Region.ID {
	case projectSearchToggleRegexID:
		plug, cmd := p.toggleProjectSearchOption(state, &state.UseRegex)
		return plug.(*Plugin), cmd
	case projectSearchToggleCaseID:
		plug, cmd := p.toggleProjectSearchOption(state, &state.CaseSensitive)
		return plug.(*Plugin), cmd
	case projectSearchToggleWordID:
		plug, cmd := p.toggleProjectSearchOption(state, &state.WholeWord)
		return plug.(*Plugin), cmd
	}

	// Single click on file header: toggle collapse/expand
	if fileIdx, ok := parseProjectSearchFileID(action.Region.ID); ok {
		if flatIdx := p.findFlatIndexForFile(fileIdx); flatIdx >= 0 {
			state.Cursor = flatIdx
		}
		if fileIdx >= 0 && fileIdx < len(state.Results) {
			state.Results[fileIdx].Collapsed = !state.Results[fileIdx].Collapsed
		}
		return p, nil
	}

	// Single click on match: open file in preview
	if fileIdx, matchIdx, ok := parseProjectSearchMatchID(action.Region.ID); ok {
		if flatIdx := p.findFlatIndexForMatch(fileIdx, matchIdx); flatIdx >= 0 {
			state.Cursor = flatIdx
		}
		// Open the result in preview
		plug, cmd := p.openProjectSearchResult()
		return plug.(*Plugin), cmd
	}

	return p, nil
}

// handleProjectSearchDoubleClick handles double clicks in project search.
func (p *Plugin) handleProjectSearchDoubleClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	state := p.projectSearchState
	if action.Region == nil || state == nil {
		return p, nil
	}

	switch action.Region.ID {
	case "modal-backdrop", "modal-body":
		return p, nil
	}

	// Double click on file header: open first match in external editor
	if fileIdx, ok := parseProjectSearchFileID(action.Region.ID); ok {
		if fileIdx >= 0 && fileIdx < len(state.Results) {
			file := state.Results[fileIdx]
			lineNo := 0
			if len(file.Matches) > 0 {
				lineNo = file.Matches[0].LineNo
			}
			p.projectSearchMode = false
			p.projectSearchState = nil
			p.clearProjectSearchModal()
			return p, p.openFileAtLine(file.Path, lineNo)
		}
		return p, nil
	}

	// Double click on match: open in external editor
	if fileIdx, matchIdx, ok := parseProjectSearchMatchID(action.Region.ID); ok {
		if fileIdx >= 0 && fileIdx < len(state.Results) {
			file := state.Results[fileIdx]
			if matchIdx >= 0 && matchIdx < len(file.Matches) {
				match := file.Matches[matchIdx]
				p.projectSearchMode = false
				p.projectSearchState = nil
				p.clearProjectSearchModal()
				return p, p.openFileAtLine(file.Path, match.LineNo)
			}
		}
	}

	return p, nil
}

// findFlatIndexForFile finds the flat index for a file header.
func (p *Plugin) findFlatIndexForFile(fileIdx int) int {
	state := p.projectSearchState
	if state == nil || fileIdx < 0 || fileIdx >= len(state.Results) {
		return -1
	}

	flatIdx := 0
	for fi := range state.Results {
		if fi == fileIdx {
			return flatIdx
		}
		flatIdx++ // file header
		if !state.Results[fi].Collapsed {
			flatIdx += len(state.Results[fi].Matches)
		}
	}
	return -1
}

// findFlatIndexForMatch finds the flat index for a specific match.
func (p *Plugin) findFlatIndexForMatch(fileIdx, matchIdx int) int {
	state := p.projectSearchState
	if state == nil || fileIdx < 0 || fileIdx >= len(state.Results) {
		return -1
	}

	flatIdx := 0
	for fi, file := range state.Results {
		flatIdx++ // file header
		if fi == fileIdx {
			if file.Collapsed || matchIdx < 0 || matchIdx >= len(file.Matches) {
				return -1
			}
			return flatIdx + matchIdx
		}
		if !file.Collapsed {
			flatIdx += len(file.Matches)
		}
	}
	return -1
}

// handleInfoModalMouse handles mouse events in the info modal.
func (p *Plugin) handleInfoModalMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	p.ensureInfoModal()
	if p.infoModal == nil {
		p.infoMode = false
		return p, nil
	}

	action := p.infoModal.HandleMouse(msg, p.mouseHandler)
	if action == "cancel" {
		p.infoMode = false
		p.clearInfoModal()
	}
	return p, nil
}

// handleBlameModalMouse handles mouse events in the blame modal.
func (p *Plugin) handleBlameModalMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	p.ensureBlameModal()
	if p.blameModal == nil {
		return p, nil
	}

	action := p.blameModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return p, nil
	case "cancel", blameActionID:
		// Close blame view
		p.blameMode = false
		p.blameState = nil
		p.blameModal = nil
		p.blameModalWidth = 0
		return p, nil
	}
	return p, nil
}

// handleExitConfirmationMouse handles mouse events in the exit confirmation dialog.
func (p *Plugin) handleExitConfirmationMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	// For now, clicks anywhere in the confirmation just select the option under cursor
	// The keyboard handling does the main interaction
	// We could add clickable option detection here if needed
	return p, nil
}
