package conversations

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/adapter"
)

// Session selection and state management methods

// setSelectedSession updates the selected session and resets related state.
func (p *Plugin) setSelectedSession(sessionID string) {
	if sessionID == "" || sessionID == p.selectedSession {
		return
	}
	p.selectedSession = sessionID
	p.loadedSession = ""
	p.messages = nil
	p.turns = nil
	p.turnCursor = 0
	p.turnScrollOff = 0
	p.sessionSummary = nil
	p.showToolSummary = false
	p.detailMode = false
	p.detailTurn = nil
	p.detailScroll = 0
	p.expandedThinking = make(map[string]bool)
	// Reset conversation flow view state
	p.expandedMessages = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)
	p.messageScroll = 0
	p.messageCursor = 0
	p.turnViewMode = false // Start in conversation flow mode
	// Reset pagination state (td-313ea851)
	p.messageOffset = 0
	p.totalMessages = 0
	p.hasOlderMsgs = false
	// Clear render cache on session change (td-8910b218)
	p.clearRenderCache()
	// Mark hit regions dirty (td-ea784b03)
	p.hitRegionsDirty = true
	// Promote selected session to HOT tier for real-time watching (td-dca6fe)
	if p.tieredManager != nil {
		adapterID := ""
		if s := p.findSelectedSession(); s != nil {
			adapterID = s.AdapterID
		}
		// PromoteToHot already clears Frozen flag
		p.tieredManager.PromoteSession(adapterID, sessionID)
	}
	p.updateTieredHotTargets()
}

// findSelectedSession returns the currently selected session.
func (p *Plugin) findSelectedSession() *adapter.Session {
	for i := range p.sessions {
		if p.sessions[i].ID == p.selectedSession {
			return &p.sessions[i]
		}
	}
	return nil
}

// adapterForSession returns the adapter for a given session ID.
func (p *Plugin) adapterForSession(sessionID string) adapter.Adapter {
	for i := range p.sessions {
		if p.sessions[i].ID == sessionID {
			if p.sessions[i].AdapterID == "" {
				return nil
			}
			return p.adapters[p.sessions[i].AdapterID]
		}
	}
	return nil
}

// getSelectedMessage returns the message at the current messageCursor position.
func (p *Plugin) getSelectedMessage() *adapter.Message {
	if len(p.messages) == 0 {
		return nil
	}
	idx := p.messageCursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(p.messages) {
		idx = len(p.messages) - 1
	}
	return &p.messages[idx]
}

// Session filtering methods

// filterSessions filters sessions based on search query.
func (p *Plugin) filterSessions() {
	if p.searchQuery == "" {
		p.searchResults = nil
		return
	}

	query := strings.ToLower(p.searchQuery)
	var results []adapter.Session
	for _, s := range p.sessions {
		if strings.Contains(strings.ToLower(s.Name), query) ||
			strings.Contains(strings.ToLower(s.Slug), query) ||
			strings.Contains(s.ID, query) ||
			strings.Contains(strings.ToLower(s.AdapterName), query) {
			results = append(results, s)
		}
	}
	p.searchResults = results
}

// visibleSessions returns sessions to display (filtered or all).
func (p *Plugin) visibleSessions() []adapter.Session {
	if p.searchMode && p.searchQuery != "" {
		return p.searchResults
	}

	// Apply filters if active
	if p.filterActive && p.filters.IsActive() {
		var filtered []adapter.Session
		for _, s := range p.sessions {
			if p.filters.Matches(s) {
				filtered = append(filtered, s)
			}
		}
		return filtered
	}

	// Apply session pagination (td-7198a5)
	if p.displayedCount > 0 && p.displayedCount < len(p.sessions) {
		return p.sessions[:p.displayedCount]
	}
	return p.sessions
}

// loadMoreSessions increases the displayed session count by one page (td-7198a5).
func (p *Plugin) loadMoreSessions() {
	p.displayedCount += defaultSessionPageSize
	if p.displayedCount > len(p.sessions) {
		p.displayedCount = len(p.sessions)
	}
	p.hasMoreSessions = p.displayedCount < len(p.sessions)
	p.hitRegionsDirty = true
}

// visibleMessageIndices returns indices of messages shown in conversation flow
// (excludes tool_result-only messages which are shown inline).
func (p *Plugin) visibleMessageIndices() []int {
	var indices []int
	for i, msg := range p.messages {
		if !p.isToolResultOnlyMessage(msg) {
			indices = append(indices, i)
		}
	}
	return indices
}

// isToolResultOnlyMessage checks if a message contains only tool_result blocks.
func (p *Plugin) isToolResultOnlyMessage(msg adapter.Message) bool {
	if len(msg.ContentBlocks) == 0 {
		return false
	}
	for _, block := range msg.ContentBlocks {
		if block.Type != "tool_result" {
			return false
		}
	}
	return true
}

// Preview scheduling methods

// schedulePreviewLoad schedules a debounced preview load for a session.
func (p *Plugin) schedulePreviewLoad(sessionID string) tea.Cmd {
	if sessionID == "" {
		return nil
	}
	p.previewToken++
	token := p.previewToken
	// Capture epoch for stale detection on project switch
	var epoch uint64
	if p.ctx != nil {
		epoch = p.ctx.Epoch
	}
	return tea.Tick(previewDebounce, func(time.Time) tea.Msg {
		return PreviewLoadMsg{Epoch: epoch, Token: token, SessionID: sessionID}
	})
}

// scheduleMessageReload schedules a debounced message reload for a session.
func (p *Plugin) scheduleMessageReload(sessionID string) tea.Cmd {
	if sessionID == "" {
		return nil
	}
	p.messageReloadToken++
	token := p.messageReloadToken
	// Capture epoch for stale detection on project switch
	var epoch uint64
	if p.ctx != nil {
		epoch = p.ctx.Epoch
	}
	return tea.Tick(watchReloadDebounce, func(time.Time) tea.Msg {
		return MessageReloadMsg{Epoch: epoch, Token: token, SessionID: sessionID}
	})
}

// Sidebar toggle method

// toggleSidebar toggles the sidebar visibility.
func (p *Plugin) toggleSidebar() {
	if p.sidebarVisible {
		p.sidebarRestore = p.activePane
		p.sidebarVisible = false
		if p.activePane == PaneSidebar {
			p.activePane = PaneMessages
		}
		return
	}

	p.sidebarVisible = true
	if p.sidebarRestore == PaneSidebar {
		p.activePane = PaneSidebar
	} else {
		p.activePane = PaneMessages
	}
}

// Cursor visibility methods

// ensureCursorVisible adjusts scroll to keep cursor visible.
func (p *Plugin) ensureCursorVisible() {
	// Pane height - borders(2) - header(1-2)
	paneHeight := p.height - 2
	visibleRows := paneHeight - 3 // -2 for inner height calc, -1 for header
	if visibleRows < 1 {
		visibleRows = 1
	}

	sessions := p.visibleSessions()

	// When not in search mode and we have sessions, account for group headers
	if !p.searchMode && len(sessions) > 0 {
		// Calculate visual lines between scrollOff and cursor (including headers)
		headerLines := p.countHeaderLinesBetween(p.scrollOff, p.cursor)
		visualOffset := (p.cursor - p.scrollOff) + headerLines

		if p.cursor < p.scrollOff {
			p.scrollOff = p.cursor
		} else if visualOffset >= visibleRows {
			// Need to scroll - find scrollOff that puts cursor at bottom
			p.scrollOff = p.findScrollOffForCursor(p.cursor, visibleRows)
		}
	} else {
		// Search mode or no sessions: flat list, no headers
		if p.cursor < p.scrollOff {
			p.scrollOff = p.cursor
		} else if p.cursor >= p.scrollOff+visibleRows {
			p.scrollOff = p.cursor - visibleRows + 1
		}
	}
}

// countHeaderLinesBetween counts header lines (group headers + spacers) between two session indices.
func (p *Plugin) countHeaderLinesBetween(start, end int) int {
	if start >= end {
		return 0
	}
	sessions := p.visibleSessions()
	if len(sessions) == 0 {
		return 0
	}

	headerLines := 0
	currentGroup := ""
	if start > 0 && start < len(sessions) {
		currentGroup = getSessionGroup(sessions[start].UpdatedAt)
	}

	for i := start; i <= end && i < len(sessions); i++ {
		sessionGroup := getSessionGroup(sessions[i].UpdatedAt)
		if sessionGroup != currentGroup {
			// Group header line
			headerLines++
			// Spacer line for Yesterday/This Week (except first visible)
			if currentGroup != "" && (sessionGroup == "Yesterday" || sessionGroup == "This Week") {
				headerLines++
			}
			currentGroup = sessionGroup
		}
	}
	return headerLines
}

// findScrollOffForCursor finds the scrollOff that puts cursor at the bottom of visible area.
func (p *Plugin) findScrollOffForCursor(cursor, visibleRows int) int {
	sessions := p.visibleSessions()
	if len(sessions) == 0 {
		return 0
	}

	// Binary search or iterate backwards to find best scrollOff
	for scrollOff := cursor; scrollOff >= 0; scrollOff-- {
		headerLines := p.countHeaderLinesBetween(scrollOff, cursor)
		visualOffset := (cursor - scrollOff) + headerLines
		if visualOffset < visibleRows {
			return scrollOff
		}
	}
	return 0
}

// ensureTurnCursorVisible adjusts scroll to keep turn cursor visible.
func (p *Plugin) ensureTurnCursorVisible() {
	// Pane height - borders(2) - header(4-5)
	paneHeight := p.height - 2
	visibleRows := paneHeight - 6 // Account for header, stats, resume cmd, separator
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Each turn takes ~3 lines (header + content/thinking/tools)
	// so divide by 3 to get approximate visible turns
	visibleTurns := visibleRows / 3
	if visibleTurns < 1 {
		visibleTurns = 1
	}

	if p.turnCursor < p.turnScrollOff {
		p.turnScrollOff = p.turnCursor
	} else if p.turnCursor >= p.turnScrollOff+visibleTurns {
		p.turnScrollOff = p.turnCursor - visibleTurns + 1
	}
}

// ensureMessageCursorVisible scrolls to keep the message cursor in view.
func (p *Plugin) ensureMessageCursorVisible() {
	// Use actual line positions if available (populated during render)
	// Otherwise fall back to estimation for initial render

	// Get available height
	viewHeight := p.height - 10 // Account for headers/margins
	if viewHeight < 10 {
		viewHeight = 10
	}

	// Try to use actual line positions from last render
	if len(p.msgLinePositions) > 0 {
		// Find cursor position in msgLinePositions
		for _, pos := range p.msgLinePositions {
			if pos.MsgIdx == p.messageCursor {
				cursorLine := pos.StartLine
				cursorEnd := cursorLine + pos.LineCount

				// Scroll to keep cursor visible with margin
				margin := 2

				// If cursor is above viewport, scroll up
				if cursorLine < p.messageScroll+margin {
					p.messageScroll = cursorLine - margin
				}
				// If cursor is below viewport, scroll down
				if cursorEnd > p.messageScroll+viewHeight-margin {
					p.messageScroll = cursorEnd - viewHeight + margin
				}

				if p.messageScroll < 0 {
					p.messageScroll = 0
				}
				return
			}
		}
	}

	// Fallback: estimate based on message count (for initial render)
	visibleIndices := p.visibleMessageIndices()
	if len(visibleIndices) == 0 {
		return
	}

	cursorPos := 0
	for i, idx := range visibleIndices {
		if idx == p.messageCursor {
			cursorPos = i
			break
		}
	}

	// Rough estimate of 5 lines per message
	estimatedLine := cursorPos * 5

	margin := viewHeight / 4
	if estimatedLine < p.messageScroll+margin {
		p.messageScroll = estimatedLine - margin
	} else if estimatedLine > p.messageScroll+viewHeight-margin {
		p.messageScroll = estimatedLine - viewHeight + margin
	}

	if p.messageScroll < 0 {
		p.messageScroll = 0
	}
}

// Message matching utility

// messagesMatch checks if old messages match the prefix of new messages.
// Used to detect incremental updates (new messages appended).
func (p *Plugin) messagesMatch(old, newPrefix []adapter.Message) bool {
	if len(old) != len(newPrefix) {
		return false
	}
	for i := range old {
		// Compare by ID and timestamp - content may have minor differences
		if old[i].ID != newPrefix[i].ID {
			return false
		}
	}
	return true
}

// Render cache methods (td-8910b218)

// clearRenderCache clears the entire render cache.
func (p *Plugin) clearRenderCache() {
	p.renderCacheMutex.Lock()
	defer p.renderCacheMutex.Unlock()
	p.renderCache = make(map[renderCacheKey]string)
}

// getCachedRender returns cached render content if available.
func (p *Plugin) getCachedRender(msgID string, width int, expanded bool) (string, bool) {
	p.renderCacheMutex.RLock()
	defer p.renderCacheMutex.RUnlock()
	key := renderCacheKey{messageID: msgID, width: width, expanded: expanded}
	cached, ok := p.renderCache[key]
	return cached, ok
}

// setCachedRender stores rendered content in cache with size-based eviction.
func (p *Plugin) setCachedRender(msgID string, width int, expanded bool, content string) {
	p.renderCacheMutex.Lock()
	defer p.renderCacheMutex.Unlock()

	// LRU eviction: limit cache to 100 entries
	const maxCacheSize = 100
	if len(p.renderCache) >= maxCacheSize {
		// Simple eviction: clear half the cache
		count := 0
		for k := range p.renderCache {
			delete(p.renderCache, k)
			count++
			if count >= maxCacheSize/2 {
				break
			}
		}
	}

	key := renderCacheKey{messageID: msgID, width: width, expanded: expanded}
	p.renderCache[key] = content
}

// invalidateCacheForMessage removes cache entries for a specific message.
func (p *Plugin) invalidateCacheForMessage(msgID string) {
	p.renderCacheMutex.Lock()
	defer p.renderCacheMutex.Unlock()
	for k := range p.renderCache {
		if k.messageID == msgID {
			delete(p.renderCache, k)
		}
	}
}
