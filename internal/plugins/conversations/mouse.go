package conversations

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/state"
)

// handleMouse processes mouse events in the two-pane view.
func (p *Plugin) handleMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	// Handle resume modal first if open (td-aa4136)
	if p.showResumeModal {
		cmd := p.handleResumeModalMouse(msg)
		return p, cmd
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
	}

	return p, nil
}

// handleMouseClick handles single click events.
func (p *Plugin) handleMouseClick(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if action.Region == nil {
		return p, nil
	}

	switch action.Region.ID {
	case regionSessionItem:
		// Click on a session item - select it
		if idx, ok := action.Region.Data.(int); ok {
			sessions := p.visibleSessions()
			if idx >= 0 && idx < len(sessions) {
				p.cursor = idx
				p.activePane = PaneSidebar
				p.setSelectedSession(sessions[idx].ID)
				return p, p.schedulePreviewLoad(p.selectedSession)
			}
		}
		return p, nil

	case regionSidebar:
		p.activePane = PaneSidebar
		return p, nil

	case regionTurnItem:
		// Click on a turn item - select it
		if idx, ok := action.Region.Data.(int); ok {
			if idx >= 0 && idx < len(p.turns) {
				p.turnCursor = idx
				p.activePane = PaneMessages
				p.ensureTurnCursorVisible()
			}
		}
		return p, nil

	case regionMainPane:
		p.activePane = PaneMessages
		return p, nil

	case regionMessageItem:
		// Click on a message in conversation flow - select it
		if idx, ok := action.Region.Data.(int); ok {
			if idx >= 0 && idx < len(p.messages) {
				p.messageCursor = idx
				p.activePane = PaneMessages
			}
		}
		return p, nil

	case regionToolExpand:
		// Click to toggle tool output expand/collapse
		if toolID, ok := action.Region.Data.(string); ok {
			p.expandedToolResults[toolID] = !p.expandedToolResults[toolID]
		}
		return p, nil

	case regionShowMore:
		// Click to expand collapsed message content
		if msgID, ok := action.Region.Data.(string); ok {
			p.expandedMessages[msgID] = !p.expandedMessages[msgID]
		}
		return p, nil

	case regionPaneDivider:
		// Start drag for pane resizing
		p.mouseHandler.StartDrag(action.X, action.Y, regionPaneDivider, p.sidebarWidth)
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
	case regionSessionItem:
		// Double-click on session item: select and focus messages pane
		if idx, ok := action.Region.Data.(int); ok {
			sessions := p.visibleSessions()
			if idx >= 0 && idx < len(sessions) {
				p.cursor = idx
				p.setSelectedSession(sessions[idx].ID)
				p.activePane = PaneMessages
				return p, tea.Batch(
					p.loadMessages(p.selectedSession),
					p.loadUsage(p.selectedSession),
				)
			}
		}
		return p, nil

	case regionSidebar:
		// Double-click in sidebar (fallback): select and focus messages pane
		sessions := p.visibleSessions()
		if p.cursor < len(sessions) {
			p.setSelectedSession(sessions[p.cursor].ID)
			p.activePane = PaneMessages
			return p, tea.Batch(
				p.loadMessages(p.selectedSession),
				p.loadUsage(p.selectedSession),
			)
		}
		return p, nil

	case regionTurnItem:
		// Double-click on turn item: select it and open detail view
		if idx, ok := action.Region.Data.(int); ok {
			if idx >= 0 && idx < len(p.turns) {
				p.turnCursor = idx
				p.detailTurn = &p.turns[idx]
				p.detailScroll = 0
				p.detailMode = true
			}
		}
		return p, nil

	case regionMainPane:
		// Double-click in main pane (fallback): open turn detail view for current cursor
		if p.turnCursor < len(p.turns) {
			p.detailTurn = &p.turns[p.turnCursor]
			p.detailScroll = 0
			p.detailMode = true
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
		if p.detailMode {
			return p.scrollDetailPane(action.Delta)
		}
		return p.scrollMainPane(action.Delta)
	}

	switch action.Region.ID {
	case regionSidebar, regionSessionItem:
		return p.scrollSidebar(action.Delta)

	case regionMainPane, regionTurnItem, regionMessageItem:
		if p.detailMode {
			return p.scrollDetailPane(action.Delta)
		}
		return p.scrollMainPane(action.Delta)
	}

	return p, nil
}

// scrollSidebar scrolls the sidebar session list.
func (p *Plugin) scrollSidebar(delta int) (*Plugin, tea.Cmd) {
	sessions := p.visibleSessions()
	if len(sessions) == 0 {
		return p, nil
	}

	// Move cursor by scroll amount
	newCursor := p.cursor + delta
	if newCursor < 0 {
		newCursor = 0
	}
	if newCursor >= len(sessions) {
		newCursor = len(sessions) - 1
	}

	if newCursor != p.cursor {
		p.cursor = newCursor
		p.ensureCursorVisible()
		p.setSelectedSession(sessions[p.cursor].ID)
		return p, p.schedulePreviewLoad(p.selectedSession)
	}

	return p, nil
}

// scrollMainPane scrolls the main messages pane.
func (p *Plugin) scrollMainPane(delta int) (*Plugin, tea.Cmd) {
	if p.turnViewMode {
		// Turn view: scroll by moving turn cursor
		if len(p.turns) == 0 {
			return p, nil
		}

		newCursor := p.turnCursor + delta
		if newCursor < 0 {
			newCursor = 0
		}
		if newCursor >= len(p.turns) {
			newCursor = len(p.turns) - 1
		}

		if newCursor != p.turnCursor {
			p.turnCursor = newCursor
			p.ensureTurnCursorVisible()
		}
	} else {
		// Conversation flow: scroll by moving message cursor
		visibleIndices := p.visibleMessageIndices()
		if len(visibleIndices) == 0 {
			return p, nil
		}

		// Find current position in visible indices
		currentPos := 0
		for i, idx := range visibleIndices {
			if idx == p.messageCursor {
				currentPos = i
				break
			}
		}

		// Apply delta
		newPos := currentPos + delta
		if newPos < 0 {
			newPos = 0
		}
		if newPos >= len(visibleIndices) {
			newPos = len(visibleIndices) - 1
		}

		if newPos != currentPos {
			p.messageCursor = visibleIndices[newPos]
			p.ensureMessageCursorVisible()
		}
	}

	return p, nil
}

// scrollDetailPane scrolls the detail view content.
func (p *Plugin) scrollDetailPane(delta int) (*Plugin, tea.Cmd) {
	p.detailScroll += delta
	if p.detailScroll < 0 {
		p.detailScroll = 0
	}
	// Max scroll is clamped in renderer (view.go:1587-1591)
	return p, nil
}

// handleMouseDrag handles drag motion events for pane resizing.
func (p *Plugin) handleMouseDrag(action mouse.MouseAction) (*Plugin, tea.Cmd) {
	if p.mouseHandler.DragRegion() != regionPaneDivider {
		return p, nil
	}

	// Calculate new sidebar width based on drag
	startValue := p.mouseHandler.DragStartValue()
	newWidth := startValue + action.DragDX

	// Clamp to reasonable bounds
	available := p.width - 5 - dividerWidth
	minWidth := 25
	maxWidth := available - 40 // Leave at least 40 for main pane
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

	return p, nil
}

// handleMouseDragEnd handles the end of a drag operation (saves pane width).
func (p *Plugin) handleMouseDragEnd() (*Plugin, tea.Cmd) {
	// Save the current sidebar width to state
	_ = state.SetConversationsSideWidth(p.sidebarWidth)
	return p, nil
}
