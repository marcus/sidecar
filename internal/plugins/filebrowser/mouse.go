package filebrowser

import (
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/plugin"
	sharedscroll "github.com/marcus/sidecar/internal/scroll"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/ui"
)

// The Files plugin is declared "covered" in assembly.WheelBoundaryRegistry;
// this assertion makes losing the contract a compile error.
var _ plugin.WheelBoundaryConsumer = (*Plugin)(nil)

// WheelAtBoundary implements plugin.WheelBoundaryConsumer. It mirrors the
// routing in handleMouseScroll without opening files, updating selection, or
// rendering. Open overlays are answered by the overlay that owns mouse input,
// in the same precedence handleMouse uses, and never by the panes underneath.
// The inline editor stays unknown: its wheel belongs to the embedded
// application. The file-operation bar does not intercept the wheel, so the
// ordinary panes answer while it is open.
func (p *Plugin) WheelAtBoundary(msg tea.MouseWheelMsg) bool {
	if p == nil || p.mouseHandler == nil || p.tree == nil {
		return false
	}
	if bounded, ok := p.overlayWheelAtBoundary(msg); ok {
		return bounded
	}
	action := p.mouseHandler.HandleMouse(msg)
	if action.Type != mouse.ActionScrollUp && action.Type != mouse.ActionScrollDown {
		return false
	}
	inTreePane := action.X < p.treeWidth
	if action.Region != nil {
		switch action.Region.ID {
		case regionTreePane, regionTreeItem:
			inTreePane = true
		case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
			// The tree column's bars count as tree pane for boundary
			// purposes; the preview's own bar does not.
			inTreePane = scrollbarRegionIsTreeSide(action.Region)
		}
	}
	surface := regionPreviewPane
	var bounds sharedscroll.Bounds
	if inTreePane {
		surface = regionTreePane
		bounds = sharedscroll.Bounds{Position: p.treeCursor, Maximum: p.tree.Len() - 1}
	} else {
		maxScroll := len(p.getPreviewLines()) - p.visibleContentHeight()
		bounds = sharedscroll.Bounds{Position: p.previewScroll, Maximum: maxScroll}
	}
	if !bounds.AtBoundary(action.Delta) {
		return false
	}
	// A held delta must not leak into the next gesture after the filter starts
	// dropping the inertia tail at this boundary.
	p.wheelBursts.For(surface).Reset()
	return true
}

// overlayWheelAtBoundary answers for whichever overlay currently owns mouse
// input, following the same precedence as handleMouse. ok is false when no
// overlay owns the wheel, which lets the ordinary panes answer.
func (p *Plugin) overlayWheelAtBoundary(msg tea.MouseWheelMsg) (bounded, ok bool) {
	switch {
	case p.edit.ShowExitConfirm:
		// handleExitConfirmationMouse consumes every mouse event without
		// touching state: the whole wheel stream is a known no-op.
		return true, true
	case p.edit.Active:
		// The embedded editor owns the wheel.
		return false, true
	case p.projectSearchMode:
		// The search owns its cursor and its in-flight state, so it answers for
		// itself rather than having those rules reproduced here.
		return p.projectSearchSurface().WheelAtBoundary(msg), true
	case p.quickOpenMode:
		// Likewise the finder.
		return p.fileFinder().WheelAtBoundary(msg), true
	case p.infoMode:
		return p.infoModal != nil && p.infoModal.WheelAtBoundary(msg, p.mouseHandler), true
	case p.blameMode:
		return p.blameModal != nil && p.blameModal.WheelAtBoundary(msg, p.mouseHandler), true
	}
	return false, false
}

// dragForwardThrottle is the minimum interval between forwarding mouse drag
// events to the inline editor's tmux session. Without throttling, every mouse
// motion event (~100+/sec) spawns a subprocess, causing 10-30s hangs.
// 16ms (~60fps) matches the workspace plugin's scrollDebounceInterval.
const dragForwardThrottle = 16 * time.Millisecond

// Mouse region identifiers
const (
	regionTreePane    = "tree-pane"    // Overall tree pane for scroll targeting
	regionPreviewPane = "preview-pane" // Overall preview pane for scroll targeting
	regionPaneDivider = "pane-divider" // Border between tree and preview
	regionTreeItem    = "tree-item"    // Individual file/folder (Data: visible index)
	regionPreviewLine = "preview-line" // Individual preview line (Data: line index)
	regionPreviewTab  = "preview-tab"  // Preview tab (Data: previewTabHit)

	// File operation modal buttons
	regionFileOpConfirm    = "file-op-confirm"    // Confirm/Create/Delete/Yes button
	regionFileOpCancel     = "file-op-cancel"     // Cancel/No button
	regionFileOpSuggestion = "file-op-suggestion" // Path suggestion item (Data: index)

	regionTreeSearchClear    = "tree-search-clear"    // × on the tree `/` bar
	regionContentSearchClear = "content-search-clear" // × on the preview `/` bar
)

// handleMouse processes mouse events and dispatches to appropriate handlers.
func (p *Plugin) handleMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	// Handle exit confirmation dialog if active
	if p.edit.ShowExitConfirm {
		p.clearDragState()
		return p.handleExitConfirmationMouse(msg)
	}

	// Handle inline edit mode - mouse events for editor and click-away detection
	if p.edit.Active && p.edit.Model != nil && p.edit.Model.IsActive() {
		p.clearDragState()
		action := p.mouseHandler.HandleMouse(msg)

		// Helper to handle click-away: save edit state to tab and detach
		// The tmux session keeps running in background; returning to the tab restores it
		handleClickAway := func(regionID string, regionData interface{}) (*Plugin, tea.Cmd) {
			p.edit.Dragging = false // Cancel any drag in progress

			// Check if the editor session is still alive
			if !p.isInlineEditSessionAlive() {
				// Session is dead - just clean up and process click
				p.exitInlineEditMode()
				p.edit.PendingClickRegion = regionID
				p.edit.PendingClickData = regionData
				return p.processPendingClickAction()
			}

			// Session is alive - save state to tab and detach (no confirmation)
			p.saveEditStateToTab()
			p.clearPluginEditState()

			// Process the click directly
			p.edit.PendingClickRegion = regionID
			p.edit.PendingClickData = regionData
			return p.processPendingClickAction()
		}

		// Handle click (mouse press) - start potential drag
		if action.Type == mouse.ActionClick {
			// Check for tab row clicks FIRST (before forwarding to vim)
			// This is needed because regionPreviewPane encompasses tabs
			if len(p.tabs) > 1 {
				tabY := p.inputBarHeight() + 1 // pane border + first content row
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
						if hit.CloseW < 1 {
							continue
						}
						closeStart := tabX + hit.CloseX
						if action.X >= closeStart && action.X < closeStart+hit.CloseW {
							return handleClickAway(regionPreviewTab, previewTabHit{Index: hit.Index, Close: true})
						}
					}
					for _, hit := range p.tabHits {
						hitStart := tabX + hit.X
						hitEnd := hitStart + hit.Width
						if action.X >= hitStart && action.X < hitEnd {
							return handleClickAway(regionPreviewTab, previewTabHit{Index: hit.Index})
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
						return handleClickAway(regionPreviewTab, previewTabHit{Index: bestIdx})
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
						p.edit.Dragging = true
						p.lastDragForwardTime = time.Time{}
						return p, p.forwardMousePressToInlineEditor(col, row)
					}
					// The click landed on letterbox padding, outside the real
					// pane. Drop it as hover and release do: forwarding to the
					// tty model would send absolute screen coordinates, which
					// the pane never had.
					return p, nil
				}
			}

			// Fallback: use X position to detect tree pane clicks
			if p.treeVisible && action.X < p.treeWidth {
				return handleClickAway(regionTreePane, nil)
			}
		}

		// Handle mouse motion/hover - forward drag events to vim for text selection.
		// Throttled to ~60fps to prevent subprocess spam (each forward spawns tmux send-keys).
		if action.Type == mouse.ActionHover && p.edit.Dragging {
			now := time.Now()
			if now.Sub(p.lastDragForwardTime) < dragForwardThrottle {
				return p, nil
			}
			col, row, ok := p.calculateInlineEditorMouseCoords(action.X, action.Y)
			if ok {
				p.lastDragForwardTime = now
				return p, p.forwardMouseDragToInlineEditor(col, row)
			}
		}

		// Handle mouse release - end drag
		if rel, ok := msg.(tea.MouseReleaseMsg); ok {
			if p.edit.Dragging {
				p.edit.Dragging = false
				p.lastDragForwardTime = time.Time{}
				rm := rel.Mouse()
				col, row, ok := p.calculateInlineEditorMouseCoords(rm.X, rm.Y)
				if ok {
					return p, p.forwardMouseReleaseToInlineEditor(col, row)
				}
			}
			return p, nil
		}

		// Forward other mouse events to tty model
		cmd := p.edit.Model.Update(msg)
		return p, cmd
	}

	// The modal branches below consume the event without ever reaching the
	// drag dispatch, so a gesture in flight must not survive them: a release
	// swallowed by a modal would otherwise leave the drag armed with a stale
	// row index.
	if p.projectSearchMode || p.quickOpenMode || p.infoMode || p.blameMode {
		p.clearDragState()
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

	// A fresh press always supersedes the previous gesture, even one that lands
	// on empty space (which produces no action at all, so handleMouseClick
	// would never run to clear it).
	if _, ok := msg.(tea.MouseClickMsg); ok {
		p.clearDragState()
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
		return p.handleMouseDragEnd(action)
	case mouse.ActionHover:
		return p.handleMouseHover(action)
	}
	return p, nil
}

// handleMouseHover handles mouse hover for visual feedback.
func (p *Plugin) handleMouseHover(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	// A hover event means no button is held, so any gesture we still think is
	// in flight lost its release (released outside the window, focus stolen).
	// Drop it rather than leaving a drag armed against a stale row.
	if p.dragArmed || p.dragActive {
		p.clearDragState()
	}

	p.hoverDivider = action.Region != nil && action.Region.ID == regionPaneDivider
	p.setTabCloseHover(action)

	// Scrollbar hover highlight, plus recovery from a release that was lost
	// (outside the window, focus stolen): the drag machinery already ended the
	// gesture on the first button-less motion, so drop our half too.
	if p.dragScrollbar != sbNone && !p.mouseHandler.IsDragging() {
		p.dragScrollbar = sbNone
	}
	p.hoverScrollbar = sbNone
	if action.Region != nil {
		switch action.Region.ID {
		case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
			if view, ok := action.Region.Data.(scrollbarView); ok {
				p.hoverScrollbar = view
			}
		}
	}

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
	// A fresh press always supersedes any previous gesture, including a press
	// on empty space.
	p.clearDragState()

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
		// Arm (but do not start) a drag. Until the movement threshold is
		// crossed in handleTreeItemDrag this stays a plain click, so click
		// behavior above is unchanged. Search mode is excluded: the pane then
		// renders the flat match list while these regions still carry tree
		// indices, so a drag there would target a row the user is not looking at.
		if node := p.draggableNode(idx); node != nil && !p.searchMode {
			p.dragArmed = true
			p.dragSourcePath = node.Path
			p.mouseHandler.StartDrag(action.X, action.Y, regionTreeItem, idx)
		}
		return p, p.loadPreviewForCursor()

	case regionTreePane:
		p.activePane = PaneTree
		return p, nil

	case regionPreviewPane:
		p.activePane = PanePreview
		p.selection.Clear() // Clear selection when clicking empty area
		return p, nil

	case regionPreviewLine:
		p.activePane = PanePreview
		lineIdx, col, ok := p.previewSelectionAtXY(action.X, action.Y)
		if !ok {
			return p, nil
		}
		// Prepare drag tracking with character-level anchor
		p.selection.PrepareDrag(lineIdx, col, action.Region.Rect)
		// Start drag tracking for potential drag-select
		p.mouseHandler.StartDrag(action.X, action.Y, regionPreviewLine, lineIdx)
		return p, nil

	case regionPreviewTab:
		p.activePane = PanePreview
		return p, p.clickPreviewTab(action.Region.Data)

	case regionPaneDivider:
		// Start drag with current tree width
		p.mouseHandler.StartDrag(action.X, action.Y, regionPaneDivider, p.treeWidth)
		return p, nil

	case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
		// Grab the thumb, or jump-to-spot on the track and continue dragging
		// from there. Never falls through to the row regions underneath: the
		// bar's rects were registered after them precisely to win.
		return p.handleScrollbarPress(action)

	case regionTreeSearchClear:
		// The × clears the query and leaves the caret where it is, which is
		// what ctrl+u does from the keyboard.
		p.searchField.Clear()
		return p, p.updateSearchMatches()

	case regionContentSearchClear:
		p.contentSearchField.Clear()
		p.updateContentMatches()
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
	if action.Region == nil {
		return p, nil
	}

	switch action.Region.ID {
	case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
		// The second press of a rapid double-press grabs the bar exactly
		// like the first one did (thumb grab continues, track re-jumps)
		// instead of being dropped for not being a tree row.
		return p.handleScrollbarPress(action)
	case regionTreeItem:
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
			p.syncWatcherDirs()
			p.treeCursor = idx
			p.ensureTreeCursorVisible()
			return p, nil
		}

		// Open file in editor (same as 'e' key) and pin the tab
		cmd := p.openTab(node.Path, TabOpenReplace)
		p.pinTab(p.activeTab)
		if p.isInlineEditSupported(node.Path) {
			return p, tea.Batch(cmd, p.enterInlineEditMode(node.Path, 0))
		}
		return p, tea.Batch(cmd, p.openFile(node.Path))
	}
	return p, nil
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

	surface := regionPreviewPane
	if inTreePane {
		surface = regionTreePane
	}
	now := time.Now()
	if p.wheelNow != nil {
		now = p.wheelNow()
	}
	delta, flush := p.wheelBursts.For(surface).Add(action.Delta, now)
	if !flush {
		p.reuseViewOnce = true
		return p, nil
	}

	if inTreePane {
		// Scroll tree by moving cursor
		p.treeCursor, _ = (sharedscroll.Bounds{
			Position: p.treeCursor,
			Maximum:  p.tree.Len() - 1,
		}).Move(delta)
		p.ensureTreeCursorVisible()
		return p, p.schedulePreviewForCursor()
	}

	// Scroll preview pane
	lines := p.getPreviewLines()
	visibleHeight := p.visibleContentHeight()
	maxScroll := len(lines) - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}

	p.previewScroll, _ = (sharedscroll.Bounds{
		Position: p.previewScroll,
		Maximum:  maxScroll,
	}).Move(delta)

	return p, nil
}

// handleMouseDrag handles drag actions (pane resizing and text selection).
// The drag source region comes from action.DragStartID, the same field
// handleMouseDragEnd reads, so both halves of the gesture agree on one source.
func (p *Plugin) handleMouseDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	dragRegion := action.DragStartID
	if dragRegion == "" {
		dragRegion = p.mouseHandler.DragRegion()
	}

	switch dragRegion {
	case regionPaneDivider:
		return p.handlePaneDividerDrag(action)
	case regionPreviewLine:
		return p.handlePreviewSelectionDrag(action)
	case regionTreeItem:
		return p.handleTreeItemDrag(action)
	case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
		return p.handleScrollbarDrag(action)
	}
	return p, nil
}

// dragThresholdDX/DY are the movement required to promote a press on a tree row
// from "click" to "drag". Drag-to-move ships without a feature flag, so this
// threshold is the only thing protecting ordinary clicks: a single cell of
// drift between press and release is common on a trackpad, and promoting that
// to a drag would move a file on what the user meant as a click. Two cells in
// either axis is deliberate motion.
const (
	dragThresholdDX = 2
	dragThresholdDY = 2
)

// draggableNode returns the node at flat tree row idx if it may be dragged, or
// nil. The root is never draggable (it is not in the flat list, but guard
// anyway).
func (p *Plugin) draggableNode(idx int) *FileNode {
	if p.tree == nil {
		return nil
	}
	// Drag-to-move is a write, and there is no host write verb. Refusing at
	// the arm rather than at the drop means the gesture never starts, so the
	// user is not shown a drop target for a move that cannot happen.
	if p.remoteBound() {
		return nil
	}
	node := p.tree.GetNode(idx)
	if node == nil || node == p.tree.Root || node.Path == "" {
		return nil
	}
	return node
}

// clearDragState resets the drag-to-move state machine, including the mouse
// handler's own gesture when the handler is the one this plugin armed. The two
// halves have to move together: if the plugin flags reset while the handler
// keeps thinking it is dragging, every motion is reported as ActionDrag instead
// of ActionHover and hover-driven UI (file-op modal buttons) goes dead until
// the next press.
func (p *Plugin) clearDragState() {
	p.dragArmed = false
	p.dragActive = false
	p.dragSourcePath = ""
	p.dragDropIdx = -1
	p.dragDropDir = ""
	p.dragHoverIdx = -1
	p.dragHoverSince = time.Time{}
	p.dragHoverGen++ // Any spring-load tick already in flight is now stale.
	p.dragLastScroll = time.Time{}
	if p.mouseHandler != nil {
		switch p.mouseHandler.DragRegion() {
		case regionTreeItem, ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
			// The handler keeps thinking it is dragging otherwise; see above.
			p.mouseHandler.EndDrag()
		}
	}
	p.dragScrollbar = sbNone
}

// handleTreeItemDrag maintains the drag-to-move gesture for tree rows: it
// promotes armed -> active once the threshold is crossed, then on every motion
// auto-scrolls at the pane edges, tracks the hovered row for spring-loading,
// and re-resolves the drop target.
func (p *Plugin) handleTreeItemDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if !p.dragArmed && !p.dragActive {
		return p, nil
	}
	if !p.dragActive {
		if abs(action.DragDY) < dragThresholdDY && abs(action.DragDX) < dragThresholdDX {
			// Still within the click tolerance - remain a click.
			return p, nil
		}
		p.dragArmed = false
		p.dragActive = true
		p.dragHoverIdx = -1
		p.dragHoverSince = time.Now()
	}

	// Auto-scroll first: the hit regions were registered against the previous
	// scroll offset, so a row index read from them has to be shifted by however
	// far the pane just scrolled to still name the row under the cursor.
	scrolled := p.autoScrollForDrag(action.X, action.Y)
	hoverIdx := treeRowIndexOf(action)
	if hoverIdx >= 0 && scrolled != 0 {
		hoverIdx += scrolled
	}

	cmd := p.trackDragHover(hoverIdx)
	p.dragDropDir, p.dragDropIdx = p.resolveDropTarget(hoverIdx)
	return p, cmd
}

// treeRowIndexOf returns the flat tree row under the cursor for a mouse action,
// or -1 when the cursor is not over a tree row at all.
func treeRowIndexOf(action mouse.MouseAction) int {
	if action.Region == nil || action.Region.ID != regionTreeItem {
		return -1
	}
	idx, ok := action.Region.Data.(int)
	if !ok {
		return -1
	}
	return idx
}

// dragSpringLoadDelay is how long the cursor must rest over a collapsed
// directory mid-drag before it auto-expands, letting one gesture reach a nested
// destination.
const dragSpringLoadDelay = 600 * time.Millisecond

// dragAutoScrollInterval throttles edge auto-scroll. Motion events arrive at
// well over 100/s in all-motion mode; one row per event would fly past the
// destination before the user could stop.
const dragAutoScrollInterval = 60 * time.Millisecond

// trackDragHover records how long the cursor has rested on one row and drives
// spring-loading. Two paths expand the directory, because neither alone is
// enough: the scheduled tick covers a cursor held perfectly still (no further
// motion events would ever arrive), and the elapsed-time check on motion covers
// a tick that was dropped or arrived while the pointer was elsewhere.
func (p *Plugin) trackDragHover(hoverIdx int) tea.Cmd {
	if hoverIdx != p.dragHoverIdx {
		p.dragHoverIdx = hoverIdx
		p.dragHoverSince = time.Now()
		p.dragHoverGen++ // Invalidate the tick scheduled for the row we left.
		if p.springLoadable(hoverIdx) {
			gen := p.dragHoverGen
			return tea.Tick(dragSpringLoadDelay, func(time.Time) tea.Msg {
				return DragSpringLoadMsg{Gen: gen}
			})
		}
		return nil
	}
	if p.springLoadable(hoverIdx) && time.Since(p.dragHoverSince) >= dragSpringLoadDelay {
		p.springLoadDir(hoverIdx)
	}
	return nil
}

// springLoadable reports whether the row at idx is a collapsed directory that
// spring-loading could open.
func (p *Plugin) springLoadable(idx int) bool {
	if !p.dragActive || p.tree == nil {
		return false
	}
	node := p.tree.GetNode(idx)
	return node != nil && node.IsDir && !node.IsExpanded
}

// springLoadDir expands the hovered directory mid-drag, then re-resolves the
// drop target against the row the cursor is on. Expanding renumbers every row
// below, but not the expanded directory's own row, so the hovered index is
// still valid - and a user holding perfectly still sends no further motion
// events, so without this the affordance would read "can't drop here" while a
// release would happily perform the move.
func (p *Plugin) springLoadDir(idx int) {
	node := p.tree.GetNode(idx)
	if node == nil || !node.IsDir || node.IsExpanded {
		return
	}
	if err := p.tree.Expand(node); err != nil {
		return
	}
	p.syncWatcherDirs()
	p.clampTreeScroll()
	p.dragDropDir, p.dragDropIdx = p.resolveDropTarget(idx)
	p.dragHoverSince = time.Now()
}

// handleDragSpringLoad expands a directory the cursor has rested on. A tick
// whose generation no longer matches was scheduled for a row the cursor has
// since left, and must do nothing.
func (p *Plugin) handleDragSpringLoad(msg DragSpringLoadMsg) (plugin.Plugin, tea.Cmd) {
	if !p.dragActive || msg.Gen != p.dragHoverGen {
		return p, nil
	}
	if !p.springLoadable(p.dragHoverIdx) {
		return p, nil
	}
	p.springLoadDir(p.dragHoverIdx)
	return p, nil
}

// autoScrollForDrag scrolls the tree when the cursor reaches the top or bottom
// row of the pane during a drag, so a destination that is off-screen when the
// gesture starts is still reachable. It returns the number of rows scrolled
// (negative for up), which is 0 for the common case.
func (p *Plugin) autoScrollForDrag(x, y int) int {
	if !p.dragActive || p.tree == nil {
		return 0
	}
	if x >= p.treeWidth {
		return 0 // Dragging over the preview pane must not scroll the tree.
	}
	topY, height := p.treeRowsViewport()
	if height <= 1 {
		// A one-row viewport has no distinct top and bottom edge, so there is no
		// way to tell which direction the user is reaching for.
		return 0
	}
	bottomY := topY + height - 1
	if y < topY || y > bottomY {
		return 0 // Not over the tree rows at all.
	}

	delta := 0
	switch {
	case y <= topY:
		delta = -1
	case y >= bottomY:
		delta = 1
	default:
		return 0
	}

	now := time.Now()
	if !p.dragLastScroll.IsZero() && now.Sub(p.dragLastScroll) < dragAutoScrollInterval {
		return 0
	}

	before := p.treeScrollOff
	p.treeScrollOff += delta
	p.clampTreeScroll()
	applied := p.treeScrollOff - before
	if applied != 0 {
		p.dragLastScroll = now
	}
	return applied
}

// resolveDropTarget turns the row under the cursor into a destination
// directory. It returns the destination path (relative to the project root, ""
// meaning the root itself) and the row to highlight, or -1 when the drop is not
// allowed. Every rejection here is a move that would be a no-op or would
// corrupt the tree, so this is the last line of defence before os.Rename.
func (p *Plugin) resolveDropTarget(hoverIdx int) (string, int) {
	dir, idx, _ := p.resolveDropTargetReason(hoverIdx)
	return dir, idx
}

// resolveDropTargetReason is resolveDropTarget plus a human-readable reason for
// a rejection, so a released drag can say why nothing happened instead of
// vanishing silently. The reason is empty when the cursor is simply not over a
// droppable row (there is nothing to explain) and when the drop is valid.
func (p *Plugin) resolveDropTargetReason(hoverIdx int) (string, int, string) {
	if !p.dragActive || p.tree == nil || hoverIdx < 0 {
		return "", -1, ""
	}
	source := p.tree.FindByPath(p.dragSourcePath)
	if source == nil || source.Path == "" {
		return "", -1, "" // The root is never draggable.
	}
	hovered := p.tree.GetNode(hoverIdx)
	if hovered == nil {
		return "", -1, ""
	}

	// Dropping on a file targets its parent directory, the way Finder and
	// VS Code behave.
	targetDir := hovered.Path
	highlightIdx := hoverIdx
	if !hovered.IsDir {
		targetDir = parentDirPath(hovered.Path)
		// Highlight the directory row itself when it is on screen, so the
		// feedback names the real destination rather than the file the cursor
		// happens to be over. Top-level files resolve to the root, which has no
		// row, so the hovered row stays the highlight.
		if targetDir != "" {
			if idx := p.tree.IndexOfPath(targetDir); idx >= 0 {
				highlightIdx = idx
			}
		}
	}

	// The move rules themselves live in validateMove, shared with the keyboard
	// 'm' dialog: this function only decides which row the gesture is pointing
	// at and which row to highlight.
	if reason := validateMove(source.Path, source.IsDir, filepath.Join(targetDir, source.Name)); reason != "" {
		return "", -1, reason
	}

	return targetDir, highlightIdx, ""
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
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
	lineIdx, col, ok := p.previewSelectionAtXY(action.X, action.Y)
	if !ok {
		return p, nil
	}

	// Update character-level selection via shared package
	p.selection.HandleDrag(lineIdx, col)

	return p, nil
}

// previewColAtScreenX maps a screen X coordinate to a visual column within
// the preview content for the given line index.
func (p *Plugin) previewColAtScreenX(x, lineIdx int) int {
	// Hit testing reads the same text rectangle content-link scanning exports,
	// including the panel's border, padding and mode-dependent gutter.
	relX := x - p.previewTextRect().X
	if relX < 0 {
		relX = 0
	}

	if lineIdx < 0 || lineIdx >= len(p.previewDisplayLines()) {
		return 0
	}
	expanded := ui.ExpandTabs(p.previewSelectionLine(lineIdx), 8)
	return ui.VisualColAtRelativeX(expanded, relX)
}

// handleMouseDragEnd handles the end of a drag operation.
// The action carries the release point and the region under it; the drag source
// region has to come from action.DragStartID because EndDrag has already run by
// the time this is called.
func (p *Plugin) handleMouseDragEnd(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	dragRegion := action.DragStartID
	if dragRegion == "" {
		dragRegion = p.mouseHandler.DragRegion()
	}

	// Whether or not the gesture was a real drag, it is over now.
	defer p.clearDragState()

	switch dragRegion {
	case regionPaneDivider:
		// Save the current tree width to state
		_ = state.SetFileBrowserTreeWidth(p.treeWidth)
	case regionPreviewLine:
		// Selection complete - finalize drag
		p.selection.FinishDrag()
		// Show copy hint on first selection
		if p.selection.HasSelection() && !p.selectionCopyHintShown {
			p.selectionCopyHintShown = true
			return p, msg.ShowFlash("Press alt+c or y to copy selection")
		}
	case regionTreeItem:
		return p.commitTreeItemDrop(action)
	case ui.RegionScrollbarThumb, ui.RegionScrollbarTrack:
		// Settle: the offset was clamped on every assignment and nothing
		// persists; the deferred clear below drops the gesture.
	}
	return p, nil
}

// commitTreeItemDrop performs the move a released drag asked for. The drop
// target is resolved from scratch here rather than trusting dragDropIdx: the
// watcher can rebuild the tree between the last motion event and the release,
// and a stale row index would move the wrong file into the wrong place.
func (p *Plugin) commitTreeItemDrop(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if !p.dragActive || p.ctx == nil {
		return p, nil // Never promoted past a click: nothing to do.
	}
	hoverIdx := treeRowIndexOf(action)
	// Defence in depth against a hit region that outlives the row it names: a
	// row the user cannot see is a row they cannot have aimed at, and there is
	// no undo for a move.
	if hoverIdx >= 0 && !p.treeRowVisible(hoverIdx) {
		return p, nil
	}
	targetDir, idx, reason := p.resolveDropTargetReason(hoverIdx)
	if idx < 0 {
		// A deliberate multi-second gesture that ends in nothing at all is
		// indistinguishable from a dropped event, so say why when there is
		// something to say.
		if reason != "" {
			return p, msg.Blocked(reason)
		}
		return p, nil
	}
	source := p.tree.FindByPath(p.dragSourcePath)
	if source == nil {
		return p, nil
	}

	src := filepath.Join(p.ctx.WorkDir, source.Path)
	dstDir := filepath.Join(p.ctx.WorkDir, targetDir)
	dst := filepath.Join(dstDir, source.Name)
	if err := p.validateDestPath(dst); err != nil {
		return p, msg.ShowToast("Move failed: "+err.Error(), 3*time.Second)
	}
	// The row may be stale: the directory can have been removed on disk since
	// the last tree build. doFileOp would MkdirAll it back into existence, so a
	// directory the user deliberately deleted would return holding one file.
	// The explicit move dialog still prompts before creating parents; a drop
	// has nowhere to prompt.
	if info, err := os.Stat(dstDir); err != nil || !info.IsDir() {
		return p, msg.ShowToast("Move failed: destination no longer exists", 3*time.Second)
	}

	// doFileOp owns the same-path check, the destination-exists check and the
	// rename itself. Its result is rewritten into a DragMoveResultMsg so this
	// move's outcome is reported as a drag move and no other file operation's
	// result can be mistaken for it.
	name := source.Name
	op := p.doFileOp(src, dst)
	return p, func() tea.Msg {
		switch res := op().(type) {
		case FileOpSuccessMsg:
			return DragMoveResultMsg{Name: name, Dir: targetDir}
		case FileOpErrorMsg:
			return DragMoveResultMsg{Name: name, Dir: targetDir, Err: res.Err}
		default:
			return res
		}
	}
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
