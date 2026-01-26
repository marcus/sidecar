package conversations

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/plugin"
)

// Update methods for handling key events in various views

// updateSessions handles key events in session list view.
func (p *Plugin) updateSessions(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	// Handle search mode input
	if p.searchMode {
		return p.updateSearch(msg)
	}

	// Handle filter mode input
	if p.filterMode {
		return p.updateFilter(msg)
	}

	sessions := p.visibleSessions()

	switch msg.String() {
	case "j", "down":
		if p.cursor < len(sessions)-1 {
			p.cursor++
			p.ensureCursorVisible()
			if p.cursor < len(sessions) {
				p.setSelectedSession(sessions[p.cursor].ID)
				return p, p.schedulePreviewLoad(p.selectedSession)
			}
		}

	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisible()
			if p.cursor < len(sessions) {
				p.setSelectedSession(sessions[p.cursor].ID)
				return p, p.schedulePreviewLoad(p.selectedSession)
			}
		}

	case "g":
		p.cursor = 0
		p.scrollOff = 0
		if len(sessions) > 0 {
			p.setSelectedSession(sessions[0].ID)
			return p, p.schedulePreviewLoad(p.selectedSession)
		}

	case "G":
		if len(sessions) > 0 {
			p.cursor = len(sessions) - 1
			p.ensureCursorVisible()
			p.setSelectedSession(sessions[p.cursor].ID)
			return p, p.schedulePreviewLoad(p.selectedSession)
		}

	case "ctrl+d":
		// Page down
		pageSize := 10
		if p.cursor+pageSize < len(sessions) {
			p.cursor += pageSize
		} else {
			p.cursor = len(sessions) - 1
		}
		p.ensureCursorVisible()
		if p.cursor < len(sessions) {
			p.setSelectedSession(sessions[p.cursor].ID)
			return p, p.schedulePreviewLoad(p.selectedSession)
		}

	case "ctrl+u":
		// Page up
		pageSize := 10
		if p.cursor-pageSize >= 0 {
			p.cursor -= pageSize
		} else {
			p.cursor = 0
		}
		p.ensureCursorVisible()
		if p.cursor < len(sessions) {
			p.setSelectedSession(sessions[p.cursor].ID)
			return p, p.schedulePreviewLoad(p.selectedSession)
		}

	case "tab", "shift+tab":
		// Switch focus to messages pane (if sidebar visible)
		if p.selectedSession != "" && p.sidebarVisible {
			p.activePane = PaneMessages
		}

	case "\\":
		// Toggle sidebar visibility
		p.toggleSidebar()

	case "l", "right":
		// Switch focus to messages pane
		if p.selectedSession != "" {
			p.activePane = PaneMessages
		}

	case "enter":
		if len(sessions) > 0 && p.cursor < len(sessions) {
			p.setSelectedSession(sessions[p.cursor].ID)
			p.activePane = PaneMessages
			return p, tea.Batch(
				p.loadMessages(p.selectedSession),
				p.loadUsage(p.selectedSession),
			)
		}

	case "/":
		p.searchMode = true
		p.searchQuery = ""
		p.cursor = 0
		p.scrollOff = 0

	case "f":
		// Open filter menu
		p.filterMode = true
		p.hitRegionsDirty = true // Filter menu replaces session list (td-455e378b)

	case "r":
		return p, p.loadSessions()

	case "U":
		// Toggle global analytics view
		p.view = ViewAnalytics
		return p, nil

	case "y":
		// Yank session details to clipboard
		return p, p.yankSessionDetails()

	case "Y":
		// Yank resume command to clipboard
		return p, p.yankResumeCommand()

	case "R":
		// Open resume modal for workspace
		return p, p.openResumeModal()
	}

	return p, nil
}

// updateSearch handles key events in search mode.
func (p *Plugin) updateSearch(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.searchMode = false
		p.searchQuery = ""
		p.searchResults = nil
		p.cursor = 0
		p.scrollOff = 0
		if len(p.sessions) > 0 {
			p.setSelectedSession(p.sessions[0].ID)
			return p, p.schedulePreviewLoad(p.selectedSession)
		}

	case "enter":
		sessions := p.visibleSessions()
		if len(sessions) > 0 && p.cursor < len(sessions) {
			p.setSelectedSession(sessions[p.cursor].ID)
			p.activePane = PaneMessages
			p.msgCursor = 0
			p.msgScrollOff = 0
			p.searchMode = false
			return p, tea.Batch(
				p.loadMessages(p.selectedSession),
				p.loadUsage(p.selectedSession),
			)
		}

	case "backspace":
		if len(p.searchQuery) > 0 {
			p.searchQuery = p.searchQuery[:len(p.searchQuery)-1]
			p.filterSessions()
			p.cursor = 0
			p.scrollOff = 0
		}

	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisible()
			sessions := p.visibleSessions()
			if p.cursor < len(sessions) {
				p.setSelectedSession(sessions[p.cursor].ID)
				return p, p.schedulePreviewLoad(p.selectedSession)
			}
		}

	case "down", "ctrl+n", "j":
		sessions := p.visibleSessions()
		if p.cursor < len(sessions)-1 {
			p.cursor++
			p.ensureCursorVisible()
			if p.cursor < len(sessions) {
				p.setSelectedSession(sessions[p.cursor].ID)
				return p, p.schedulePreviewLoad(p.selectedSession)
			}
		}

	case "k":
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisible()
			sessions := p.visibleSessions()
			if p.cursor < len(sessions) {
				p.setSelectedSession(sessions[p.cursor].ID)
				return p, p.schedulePreviewLoad(p.selectedSession)
			}
		}

	case "ctrl+d":
		sessions := p.visibleSessions()
		if len(sessions) == 0 {
			return p, nil
		}
		pageSize := 10
		if p.cursor+pageSize < len(sessions) {
			p.cursor += pageSize
		} else {
			p.cursor = len(sessions) - 1
		}
		p.ensureCursorVisible()
		if p.cursor < len(sessions) {
			p.setSelectedSession(sessions[p.cursor].ID)
			return p, p.schedulePreviewLoad(p.selectedSession)
		}

	case "ctrl+u":
		sessions := p.visibleSessions()
		if len(sessions) == 0 {
			return p, nil
		}
		pageSize := 10
		if p.cursor-pageSize >= 0 {
			p.cursor -= pageSize
		} else {
			p.cursor = 0
		}
		p.ensureCursorVisible()
		if p.cursor < len(sessions) {
			p.setSelectedSession(sessions[p.cursor].ID)
			return p, p.schedulePreviewLoad(p.selectedSession)
		}

	case "g":
		p.cursor = 0
		p.scrollOff = 0
		sessions := p.visibleSessions()
		if len(sessions) > 0 {
			p.setSelectedSession(sessions[0].ID)
			return p, p.schedulePreviewLoad(p.selectedSession)
		}

	case "G":
		sessions := p.visibleSessions()
		if len(sessions) > 0 {
			p.cursor = len(sessions) - 1
			p.ensureCursorVisible()
			p.setSelectedSession(sessions[p.cursor].ID)
			return p, p.schedulePreviewLoad(p.selectedSession)
		}

	default:
		// Add character to search query
		if len(msg.String()) == 1 {
			p.searchQuery += msg.String()
			p.filterSessions()
			p.cursor = 0
			p.scrollOff = 0
			sessions := p.visibleSessions()
			if len(sessions) > 0 {
				p.setSelectedSession(sessions[0].ID)
				return p, p.schedulePreviewLoad(p.selectedSession)
			}
		}
	}

	return p, nil
}

// updateAnalytics handles key events in analytics view.
func (p *Plugin) updateAnalytics(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	// Calculate max scroll based on content
	maxScroll := len(p.analyticsLines) - (p.height - 2)
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch msg.String() {
	case "esc", "q", "U":
		p.view = ViewSessions
		p.analyticsScrollOff = 0

	case "j", "down":
		if p.analyticsScrollOff < maxScroll {
			p.analyticsScrollOff++
		}

	case "k", "up":
		if p.analyticsScrollOff > 0 {
			p.analyticsScrollOff--
		}

	case "g":
		p.analyticsScrollOff = 0

	case "G":
		p.analyticsScrollOff = maxScroll

	case "ctrl+d":
		p.analyticsScrollOff += 10
		if p.analyticsScrollOff > maxScroll {
			p.analyticsScrollOff = maxScroll
		}

	case "ctrl+u":
		p.analyticsScrollOff -= 10
		if p.analyticsScrollOff < 0 {
			p.analyticsScrollOff = 0
		}
	}
	return p, nil
}

// updateMessages handles key events in message view (now uses turns).
func (p *Plugin) updateMessages(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	// In detail mode, handle detail-specific navigation
	if p.detailMode {
		return p.updateDetailMode(msg)
	}

	switch msg.String() {
	case "esc":
		// ESC returns focus to sidebar
		p.activePane = PaneSidebar
		return p, nil

	case "h", "left":
		// Return focus to sidebar
		p.activePane = PaneSidebar
		return p, nil

	case "tab", "shift+tab":
		// Switch focus to sidebar (if visible)
		if p.sidebarVisible {
			p.activePane = PaneSidebar
		}
		return p, nil

	case "\\":
		// Toggle sidebar visibility
		p.toggleSidebar()
		return p, nil

	case "j", "down":
		if p.turnViewMode {
			// Turn view navigation
			if p.turnCursor < len(p.turns)-1 {
				p.turnCursor++
				p.ensureTurnCursorVisible()
			}
		} else {
			// Conversation flow cursor navigation
			visibleIndices := p.visibleMessageIndices()
			if len(visibleIndices) > 0 {
				// Find current position and move to next
				found := false
				for i, idx := range visibleIndices {
					if idx == p.messageCursor {
						found = true
						if i < len(visibleIndices)-1 {
							p.messageCursor = visibleIndices[i+1]
							p.ensureMessageCursorVisible()
						}
						break
					}
				}
				// If cursor wasn't found (edge case), snap to first visible message
				if !found && len(visibleIndices) > 0 {
					p.messageCursor = visibleIndices[0]
					p.ensureMessageCursorVisible()
				}
			}
		}

	case "k", "up":
		if p.turnViewMode {
			if p.turnCursor > 0 {
				p.turnCursor--
				p.ensureTurnCursorVisible()
			}
		} else {
			// Conversation flow cursor navigation
			visibleIndices := p.visibleMessageIndices()
			if len(visibleIndices) > 0 {
				// Find current position and move to previous
				found := false
				for i, idx := range visibleIndices {
					if idx == p.messageCursor {
						found = true
						if i > 0 {
							p.messageCursor = visibleIndices[i-1]
							p.ensureMessageCursorVisible()
						}
						break
					}
				}
				// If cursor wasn't found (edge case), snap to first visible message
				if !found && len(visibleIndices) > 0 {
					p.messageCursor = visibleIndices[0]
					p.ensureMessageCursorVisible()
				}
			}
		}

	case "g":
		if p.turnViewMode {
			p.turnCursor = 0
			p.turnScrollOff = 0
		} else {
			visibleIndices := p.visibleMessageIndices()
			if len(visibleIndices) > 0 {
				p.messageCursor = visibleIndices[0]
				p.messageScroll = 0
			}
		}

	case "G":
		if p.turnViewMode {
			if len(p.turns) > 0 {
				p.turnCursor = len(p.turns) - 1
				p.ensureTurnCursorVisible()
			}
		} else {
			visibleIndices := p.visibleMessageIndices()
			if len(visibleIndices) > 0 {
				p.messageCursor = visibleIndices[len(visibleIndices)-1]
				p.messageScroll = 999999 // Will be clamped in renderer
			}
		}

	case "ctrl+d":
		pageSize := 10
		if p.turnViewMode {
			if p.turnCursor+pageSize < len(p.turns) {
				p.turnCursor += pageSize
			} else if len(p.turns) > 0 {
				p.turnCursor = len(p.turns) - 1
			}
			p.ensureTurnCursorVisible()
		} else {
			// Page down in conversation flow - move cursor by pageSize messages
			visibleIndices := p.visibleMessageIndices()
			if len(visibleIndices) > 0 {
				currentPos := 0
				for i, idx := range visibleIndices {
					if idx == p.messageCursor {
						currentPos = i
						break
					}
				}
				newPos := currentPos + pageSize
				if newPos >= len(visibleIndices) {
					newPos = len(visibleIndices) - 1
				}
				p.messageCursor = visibleIndices[newPos]
				p.ensureMessageCursorVisible()
			}
		}

	case "ctrl+u":
		pageSize := 10
		if p.turnViewMode {
			if p.turnCursor-pageSize >= 0 {
				p.turnCursor -= pageSize
			} else {
				p.turnCursor = 0
			}
			p.ensureTurnCursorVisible()
		} else {
			// Page up in conversation flow - move cursor by pageSize messages
			visibleIndices := p.visibleMessageIndices()
			if len(visibleIndices) > 0 {
				currentPos := 0
				for i, idx := range visibleIndices {
					if idx == p.messageCursor {
						currentPos = i
						break
					}
				}
				newPos := currentPos - pageSize
				if newPos < 0 {
					newPos = 0
				}
				p.messageCursor = visibleIndices[newPos]
				p.ensureMessageCursorVisible()
			}
		}

	case "t":
		// Toggle tool impact summary
		p.showToolSummary = !p.showToolSummary

	case "v":
		// Toggle between conversation flow and turn view
		p.turnViewMode = !p.turnViewMode
		p.hitRegionsDirty = true // Different hit regions per view mode (td-455e378b)
		return p, nil

	case "p":
		// Load older messages (td-313ea851)
		if p.hasOlderMsgs && p.totalMessages > maxMessagesInMemory {
			p.messageOffset += maxMessagesInMemory / 2 // Load half a page older
			if p.messageOffset > p.totalMessages-maxMessagesInMemory {
				p.messageOffset = p.totalMessages - maxMessagesInMemory
			}
			return p, p.loadMessages(p.selectedSession)
		}

	case "n":
		// Load newer messages (td-313ea851)
		if p.messageOffset > 0 {
			p.messageOffset -= maxMessagesInMemory / 2 // Load half a page newer
			if p.messageOffset < 0 {
				p.messageOffset = 0
			}
			return p, p.loadMessages(p.selectedSession)
		}

	case "e":
		// Toggle expand for selected message (content, tools, and thinking)
		if p.turnViewMode {
			if p.turnCursor < len(p.turns) {
				turn := &p.turns[p.turnCursor]
				for _, msg := range turn.Messages {
					// Toggle message content
					p.expandedMessages[msg.ID] = !p.expandedMessages[msg.ID]
					// Toggle thinking
					if len(msg.ThinkingBlocks) > 0 {
						p.expandedThinking[msg.ID] = !p.expandedThinking[msg.ID]
					}
					// Toggle tool outputs
					for _, tu := range msg.ToolUses {
						p.expandedToolResults[tu.ID] = !p.expandedToolResults[tu.ID]
					}
					// Invalidate render cache for this message (td-5445abd6)
					p.invalidateCacheForMessage(msg.ID)
				}
				// Mark hit regions dirty since expansion changes layout (td-ea784b03)
				p.hitRegionsDirty = true
			}
		} else {
			// Conversation flow: toggle for current message
			if msg := p.getSelectedMessage(); msg != nil {
				// Toggle message content
				p.expandedMessages[msg.ID] = !p.expandedMessages[msg.ID]
				// Toggle thinking blocks
				for _, block := range msg.ContentBlocks {
					if block.Type == "thinking" {
						p.expandedThinking[msg.ID] = !p.expandedThinking[msg.ID]
						break
					}
				}
				// Toggle tool outputs
				for _, block := range msg.ContentBlocks {
					if block.Type == "tool_use" && block.ToolUseID != "" {
						p.expandedToolResults[block.ToolUseID] = !p.expandedToolResults[block.ToolUseID]
					}
				}
				// Invalidate render cache for this message (td-5445abd6)
				p.invalidateCacheForMessage(msg.ID)
				// Mark hit regions dirty since expansion changes layout (td-ea784b03)
				p.hitRegionsDirty = true
			}
		}

	case "enter":
		// Open turn detail view in right pane
		if p.turnViewMode {
			// Turn view: use turnCursor
			if p.turnCursor < len(p.turns) {
				p.detailTurn = &p.turns[p.turnCursor]
				p.detailScroll = 0
				p.detailMode = true
			}
		} else {
			// Conversation flow: find turn containing selected message
			if msg := p.getSelectedMessage(); msg != nil {
				for i := range p.turns {
					for _, m := range p.turns[i].Messages {
						if m.ID == msg.ID {
							p.detailTurn = &p.turns[i]
							p.detailScroll = 0
							p.detailMode = true
							return p, nil
						}
					}
				}
			}
		}

	case "c":
		// Copy session to clipboard as markdown
		if p.selectedSession != "" {
			return p, p.copySessionToClipboard()
		}

	case "E":
		// Export session to file
		if p.selectedSession != "" {
			return p, p.exportSessionToFile()
		}

	case " ":
		// Load more messages (would need to implement paging in adapter)
		return p, nil

	case "y":
		// Yank current turn content to clipboard
		return p, p.yankTurnContent()

	case "Y":
		// Yank resume command to clipboard
		return p, p.yankResumeCommand()

	case "R":
		// Open resume modal for workspace
		return p, p.openResumeModal()
	}

	return p, nil
}

// updateDetailMode handles key events when in detail mode (two-pane).
func (p *Plugin) updateDetailMode(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		// Exit detail mode, back to turn list
		p.detailMode = false
		p.detailTurn = nil
		p.detailScroll = 0

	case "h", "left":
		// In detail mode, h/left goes back to turn list (same as esc)
		p.detailMode = false
		p.detailTurn = nil
		p.detailScroll = 0

	case "j", "down":
		p.detailScroll++

	case "k", "up":
		if p.detailScroll > 0 {
			p.detailScroll--
		}

	case "g":
		p.detailScroll = 0

	case "G":
		// Scroll to bottom - will be clamped by renderer
		p.detailScroll = 9999

	case "ctrl+d":
		p.detailScroll += 10

	case "ctrl+u":
		p.detailScroll -= 10
		if p.detailScroll < 0 {
			p.detailScroll = 0
		}

	case "y":
		// Yank current turn content to clipboard
		return p, p.yankTurnContent()

	case "Y":
		// Yank resume command to clipboard
		return p, p.yankResumeCommand()
	}

	return p, nil
}

// updateMessageDetail handles key events in the message detail view.
func (p *Plugin) updateMessageDetail(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		p.view = ViewMessages
		p.detailTurn = nil
		p.detailScroll = 0

	case "j", "down":
		p.detailScroll++

	case "k", "up":
		if p.detailScroll > 0 {
			p.detailScroll--
		}

	case "g":
		p.detailScroll = 0

	case "G":
		// Scroll to bottom - calculate based on content
		p.detailScroll = 100 // Placeholder, will be clamped by renderer

	case "ctrl+d":
		p.detailScroll += 10

	case "ctrl+u":
		p.detailScroll -= 10
		if p.detailScroll < 0 {
			p.detailScroll = 0
		}

	case "y":
		// Yank current turn content to clipboard
		return p, p.yankTurnContent()

	case "Y":
		// Yank resume command to clipboard
		return p, p.yankResumeCommand()
	}
	return p, nil
}

// updateFilter handles key events in filter mode.
func (p *Plugin) updateFilter(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()
	for _, opt := range adapterFilterOptions(p.adapters) {
		if key == opt.key {
			p.filters.ToggleAdapter(opt.id)
			return p, nil
		}
	}

	switch key {
	case "esc":
		p.filterMode = false
		p.hitRegionsDirty = true // Session list returns (td-455e378b)

	case "enter":
		p.filterMode = false
		p.hitRegionsDirty = true // Session list returns (td-455e378b)
		p.filterActive = p.filters.IsActive()
		p.cursor = 0
		p.scrollOff = 0

	case "1":
		// Toggle model filter: opus
		p.filters.ToggleModel("opus")

	case "2":
		// Toggle model filter: sonnet
		p.filters.ToggleModel("sonnet")

	case "3":
		// Toggle model filter: haiku
		p.filters.ToggleModel("haiku")

	case "t":
		// Toggle date filter: today
		p.filters.SetDateRange("today")

	case "y":
		// Toggle date filter: yesterday
		p.filters.SetDateRange("yesterday")

	case "w":
		// Toggle date filter: week
		p.filters.SetDateRange("week")

	case "a":
		// Toggle active only
		p.filters.ActiveOnly = !p.filters.ActiveOnly

	case "x":
		// Clear all filters
		p.filters = SearchFilters{}
	}
	return p, nil
}
