package gitstatus

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/state"
)

// Hit region IDs
const (
	regionSidebar      = "sidebar"
	regionDiffPane     = "diff-pane"
	regionPaneDivider  = "pane-divider"
	regionFile         = "file"
	regionCommit       = "commit"
	regionCommitFile   = "commit-file"   // Files in commit preview pane
	regionDiffModal    = "diff-modal"    // Full-screen diff view
	regionDiffBack     = "diff-back"     // Back button in diff breadcrumb
	regionCommitButton = "commit-button" // Commit modal button
)

// handleMouse processes mouse events in the status view.
func (p *Plugin) handleMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
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
	}

	return p, nil
}

// handleMouseClick handles single click events.
func (p *Plugin) handleMouseClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if action.Region == nil {
		return p, nil
	}

	switch action.Region.ID {
	case regionSidebar:
		p.activePane = PaneSidebar
		return p, nil

	case regionDiffPane:
		p.activePane = PaneDiff
		return p, nil

	case regionPaneDivider:
		// Start drag for pane resizing
		p.mouseHandler.StartDrag(action.X, action.Y, regionPaneDivider, p.sidebarWidth)
		return p, nil

	case regionFile:
		// Click on file - select it, or toggle folder expansion
		if idx, ok := action.Region.Data.(int); ok {
			entries := p.tree.AllEntries()
			if idx < len(entries) {
				entry := entries[idx]
				// For folders, toggle expansion on click (not just double-click)
				if entry.IsFolder {
					p.cursor = idx
					p.ensureCursorVisible()
					entry.IsExpanded = !entry.IsExpanded
					return p, p.autoLoadDiff()
				}
			}
			// Regular files: just select
			if idx != p.cursor {
				p.cursor = idx
				p.ensureCursorVisible()
				if p.cursorOnCommit() {
					return p, p.autoLoadCommitPreview()
				}
				return p, p.autoLoadDiff()
			}
		}
		return p, nil

	case regionCommit:
		// Click on commit - select it
		// idx is now absolute index into recentCommits
		if idx, ok := action.Region.Data.(int); ok {
			fileCount := len(p.tree.AllEntries())
			newCursor := fileCount + idx
			if newCursor != p.cursor {
				p.cursor = newCursor
				p.ensureCursorVisible()
				p.ensureCommitVisible(idx)
				return p, p.autoLoadCommitPreview()
			}
		}
		return p, nil

	case regionCommitFile:
		// Click on file in commit preview - select it
		if idx, ok := action.Region.Data.(int); ok {
			if p.previewCommit != nil && idx < len(p.previewCommit.Files) {
				p.previewCommitCursor = idx
				p.activePane = PaneDiff
			}
		}
		return p, nil
	}

	return p, nil
}

// handleMouseDoubleClick handles double-click events.
func (p *Plugin) handleMouseDoubleClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if action.Region == nil {
		return p, nil
	}

	switch action.Region.ID {
	case regionFile:
		// Double-click on file - open it in editor (folders handled by single-click)
		entries := p.tree.AllEntries()
		if idx, ok := action.Region.Data.(int); ok && idx < len(entries) {
			entry := entries[idx]
			if entry.IsFolder {
				// Folder expansion is handled by single-click, ignore double-click
				return p, nil
			}
			// Open file in editor
			return p, p.openFile(entry.Path)
		}
		return p, nil

	case regionCommit:
		// Double-click on commit - focus preview pane
		// idx is now absolute index into recentCommits
		if idx, ok := action.Region.Data.(int); ok {
			fileCount := len(p.tree.AllEntries())
			p.cursor = fileCount + idx
			p.ensureCursorVisible()
			p.ensureCommitVisible(idx)
			if p.previewCommit != nil {
				p.activePane = PaneDiff
			}
			return p, p.autoLoadCommitPreview()
		}
		return p, nil

	case regionDiffPane:
		// Double-click in diff pane when on a file - open full-screen diff
		if !p.cursorOnCommit() {
			entries := p.tree.AllEntries()
			if p.cursor < len(entries) {
				entry := entries[p.cursor]
				p.diffReturnMode = p.viewMode
				p.viewMode = ViewModeDiff
				p.diffFile = entry.Path
				p.diffCommit = ""
				p.diffCommitSubject = ""
				p.diffCommitShortHash = ""
				p.diffScroll = 0
				p.diffLoaded = false
				if entry.IsFolder {
					return p, p.loadFullFolderDiff(entry)
				}
				return p, p.loadDiff(entry.Path, entry.Staged, entry.Status)
			}
		}
		return p, nil

	case regionCommitFile:
		// Double-click on file in commit preview - open full-screen diff
		if idx, ok := action.Region.Data.(int); ok {
			if p.previewCommit != nil && idx < len(p.previewCommit.Files) {
				file := p.previewCommit.Files[idx]
				p.diffReturnMode = p.viewMode
				p.viewMode = ViewModeDiff
				p.diffFile = file.Path
				p.diffCommit = p.previewCommit.Hash
				p.diffCommitSubject = p.previewCommit.Subject
				p.diffCommitShortHash = p.previewCommit.ShortHash
				p.diffScroll = 0
				p.diffLoaded = false
				parentHash := ""
				if p.previewCommit.IsMerge && len(p.previewCommit.ParentHashes) > 0 {
					parentHash = p.previewCommit.ParentHashes[0]
				}
				return p, p.loadCommitFileDiff(p.previewCommit.Hash, file.Path, parentHash)
			}
		}
		return p, nil
	}

	return p, nil
}

// handleMouseScroll handles scroll wheel events.
func (p *Plugin) handleMouseScroll(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if action.Region == nil {
		// No hit region - scroll based on pane position
		if action.X < p.sidebarWidth+2 {
			return p.scrollSidebar(action.Delta)
		}
		return p.scrollDiffPane(action.Delta)
	}

	switch action.Region.ID {
	case regionSidebar, regionFile, regionCommit:
		return p.scrollSidebar(action.Delta)

	case regionDiffPane, regionCommitFile:
		return p.scrollDiffPane(action.Delta)
	}

	return p, nil
}

// scrollSidebar scrolls the sidebar file list.
func (p *Plugin) scrollSidebar(delta int) (*Plugin, tea.Cmd) {
	totalItems := p.totalSelectableItems()
	if totalItems == 0 {
		return p, nil
	}

	// Move cursor by scroll amount
	newCursor := p.cursor + delta
	if newCursor < 0 {
		newCursor = 0
	}
	if newCursor >= totalItems {
		newCursor = totalItems - 1
	}

	if newCursor != p.cursor {
		p.cursor = newCursor
		p.ensureCursorVisible()
		if p.cursorOnCommit() {
			commitIdx := p.selectedCommitIndex()
			p.ensureCommitVisible(commitIdx)
			// Trigger load-more when within 3 commits of end
			var loadMoreCmd tea.Cmd
			if !p.historyFilterActive && p.moreCommitsAvailable && commitIdx >= len(p.recentCommits)-3 && !p.loadingMoreCommits {
				loadMoreCmd = p.loadMoreCommits()
			}
			return p, tea.Batch(p.autoLoadCommitPreview(), loadMoreCmd)
		}
		return p, p.autoLoadDiff()
	}

	return p, nil
}

// scrollDiffPane scrolls the diff pane content.
func (p *Plugin) scrollDiffPane(delta int) (*Plugin, tea.Cmd) {
	// If showing commit preview, scroll its file list
	if p.previewCommit != nil && p.cursorOnCommit() {
		p.previewCommitScroll += delta
		if p.previewCommitScroll < 0 {
			p.previewCommitScroll = 0
		}
		maxScroll := len(p.previewCommit.Files) - 5
		if maxScroll < 0 {
			maxScroll = 0
		}
		if p.previewCommitScroll > maxScroll {
			p.previewCommitScroll = maxScroll
		}
		return p, nil
	}

	// Otherwise scroll the diff content
	p.diffPaneScroll += delta
	if p.diffPaneScroll < 0 {
		p.diffPaneScroll = 0
	}

	// Clamp to max if we have parsed diff content
	if p.diffPaneParsedDiff != nil {
		lines := countParsedDiffLines(p.diffPaneParsedDiff)
		maxScroll := lines - (p.height - 6)
		if maxScroll < 0 {
			maxScroll = 0
		}
		if p.diffPaneScroll > maxScroll {
			p.diffPaneScroll = maxScroll
		}
	}

	return p, nil
}

// handleMouseHorizontalScroll handles horizontal scroll events in the diff pane.
func (p *Plugin) handleMouseHorizontalScroll(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	// Only horizontal scroll in diff pane regions
	if action.Region == nil {
		// No hit region - use X position to determine if in diff pane
		if action.X >= p.sidebarWidth+2 {
			return p.scrollDiffPaneHorizontal(action.Delta)
		}
		return p, nil
	}

	switch action.Region.ID {
	case regionDiffPane, regionDiffModal:
		return p.scrollDiffPaneHorizontal(action.Delta)
	}

	return p, nil
}

// scrollDiffPaneHorizontal scrolls the diff pane horizontally.
func (p *Plugin) scrollDiffPaneHorizontal(delta int) (*Plugin, tea.Cmd) {
	p.diffPaneHorizScroll += delta
	if p.diffPaneHorizScroll < 0 {
		p.diffPaneHorizScroll = 0
	}
	return p, nil
}

// handleMouseDrag handles drag motion events.
func (p *Plugin) handleMouseDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if p.mouseHandler.DragRegion() == regionPaneDivider {
		// Calculate new sidebar width based on drag
		startValue := p.mouseHandler.DragStartValue()
		newWidth := startValue + action.DragDX

		// Clamp to reasonable bounds (match calculatePaneWidths logic)
		available := p.width - 5 - dividerWidth
		minWidth := 25
		maxWidth := available - 40 // Leave at least 40 for diff
		if maxWidth < minWidth {
			maxWidth = minWidth
		}
		if newWidth < minWidth {
			newWidth = minWidth
		}
		if newWidth > maxWidth {
			newWidth = maxWidth
		}

		p.sidebarWidth = newWidth
		p.diffPaneWidth = available - p.sidebarWidth
		return p, nil
	}

	return p, nil
}

// handleMouseDragEnd handles the end of a drag operation (saves pane width).
func (p *Plugin) handleMouseDragEnd() (*Plugin, tea.Cmd) {
	// Save the current sidebar width to state
	_ = state.SetGitStatusSidebarWidth(p.sidebarWidth)
	return p, nil
}

// handleCommitMouse processes mouse events in the commit modal.
func (p *Plugin) handleCommitMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	p.ensureCommitModal()
	if p.commitModal == nil {
		return p, nil
	}

	action := p.commitModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case commitActionID:
		return p, p.tryCommit()
	case "cancel":
		p.viewMode = ViewModeStatus
		p.commitAmend = false
		p.commitError = ""
		p.commitModal = nil
		p.commitModalWidthCache = 0
		return p, nil
	}

	return p, nil
}

// handleBranchPickerMouse processes mouse events in the branch picker modal.
func (p *Plugin) handleBranchPickerMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	p.ensureBranchPickerModal()
	if p.branchPickerModal == nil {
		return p, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		p.moveBranchCursor(-1)
		return p, nil
	case tea.MouseButtonWheelDown:
		p.moveBranchCursor(1)
		return p, nil
	}

	action := p.branchPickerModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "cancel":
		p.closeBranchPicker()
		return p, nil
	}

	if idx, ok := parseBranchPickerItem(action); ok {
		return p, p.switchBranchByIndex(idx)
	}

	return p, nil
}

// handlePullMenuMouse processes mouse events in the pull menu modal.
func (p *Plugin) handlePullMenuMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	p.ensurePullModal()
	if p.pullModal == nil {
		return p, nil
	}

	action := p.pullModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return p, nil
	case "cancel":
		p.viewMode = p.pullMenuReturnMode
		p.clearPullModal()
		return p, nil
	case pullMenuOptionMerge, pullMenuOptionRebase, pullMenuOptionFFOnly, pullMenuOptionAutostash:
		plug, cmd := p.executePullMenuAction(action)
		return plug.(*Plugin), cmd
	}
	return p, nil
}

// handlePushMenuMouse processes mouse events in the push menu modal.
func (p *Plugin) handlePushMenuMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	p.ensurePushMenuModal()
	if p.pushMenuModal == nil {
		return p, nil
	}

	action := p.pushMenuModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case pushMenuOptionPush:
		plug, cmd := p.executePushMenuAction(0)
		return plug.(*Plugin), cmd
	case pushMenuOptionForce:
		plug, cmd := p.executePushMenuAction(1)
		return plug.(*Plugin), cmd
	case pushMenuOptionUpstream:
		plug, cmd := p.executePushMenuAction(2)
		return plug.(*Plugin), cmd
	case "cancel":
		p.viewMode = p.pushMenuReturnMode
		p.clearPushMenuModal()
		p.pushMenuFocus = 0
		return p, nil
	}
	return p, nil
}

// handlePullConflictMouse processes mouse events in the pull conflict modal.
func (p *Plugin) handlePullConflictMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	p.ensurePullConflictModal()
	if p.pullConflictModal == nil {
		return p, nil
	}

	action := p.pullConflictModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case pullConflictAbortID:
		plug, cmd := p.abortPullConflict()
		return plug.(*Plugin), cmd
	case "cancel", pullConflictDismissID:
		plug, cmd := p.dismissPullConflict()
		return plug.(*Plugin), cmd
	}
	return p, nil
}

// handleDiffMouse processes mouse events in the full-screen diff view.
func (p *Plugin) handleDiffMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	action := p.mouseHandler.HandleMouse(msg)

	switch action.Type {
	case mouse.ActionClick:
		// Handle clicks on sidebar regions - close diff and navigate
		if action.Region != nil {
			switch action.Region.ID {
			case regionCommit:
				// Click on commit in sidebar - close diff and select commit
				if idx, ok := action.Region.Data.(int); ok {
					fileCount := len(p.tree.AllEntries())
					p.cursor = fileCount + idx
					p.ensureCursorVisible()
					p.ensureCommitVisible(idx)
					p.closeDiffView()
					p.viewMode = ViewModeStatus // override: navigate to specific commit
					return p, p.autoLoadCommitPreview()
				}
			case regionFile:
				// Click on file in sidebar - close diff and select file
				if idx, ok := action.Region.Data.(int); ok {
					p.cursor = idx
					p.ensureCursorVisible()
					p.closeDiffView()
					p.viewMode = ViewModeStatus // override: navigate to specific file
					return p, p.autoLoadDiff()
				}
			case regionDiffBack:
				p.closeDiffView()
				return p, nil
			}
		}

	case mouse.ActionScrollUp, mouse.ActionScrollDown:
		p.diffScroll += action.Delta
		if p.diffScroll < 0 {
			p.diffScroll = 0
		}
		// Clamp to max based on content
		lines := countLines(p.diffContent)
		maxScroll := lines - (p.height - 4) // Account for header + border
		if maxScroll < 0 {
			maxScroll = 0
		}
		if p.diffScroll > maxScroll {
			p.diffScroll = maxScroll
		}

	case mouse.ActionScrollLeft, mouse.ActionScrollRight:
		// Horizontal scroll for side-by-side view
		p.diffHorizOff += action.Delta
		p.clampDiffHorizScroll()
	}

	return p, nil
}
