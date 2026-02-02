package conversations

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)
// renderTwoPane renders the two-pane layout with sessions on the left and messages on the right.
func (p *Plugin) renderTwoPane() string {
	// Check if hit regions need rebuilding (td-ea784b03)
	// Mark dirty if dimensions or scroll positions changed
	if p.width != p.prevWidth || p.height != p.prevHeight {
		p.hitRegionsDirty = true
		p.prevWidth = p.width
		p.prevHeight = p.height
	}
	if p.scrollOff != p.prevScrollOff {
		p.hitRegionsDirty = true
		p.prevScrollOff = p.scrollOff
	}
	if p.messageScroll != p.prevMsgScroll {
		p.hitRegionsDirty = true
		p.prevMsgScroll = p.messageScroll
	}
	if p.turnScrollOff != p.prevTurnScroll {
		p.hitRegionsDirty = true
		p.prevTurnScroll = p.turnScrollOff
	}

	// Pane height for panels (outer dimensions including borders)
	paneHeight := p.height
	if paneHeight < 4 {
		paneHeight = 4
	}

	// Inner content height (excluding borders and header lines)
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Handle collapsed sidebar - render full-width main pane
	if !p.sidebarVisible {
		mainWidth := p.width - 2 // Account for borders
		if mainWidth < 40 {
			mainWidth = 40
		}

		mainContent := p.renderMainPane(mainWidth, innerHeight)
		rightPane := styles.RenderPanel(mainContent, mainWidth, paneHeight, true)

		// Update hit regions for collapsed state
		if p.hitRegionsDirty {
			p.mouseHandler.HitMap.Clear()
			p.mouseHandler.HitMap.AddRect(regionMainPane, 0, 0, mainWidth, p.height, nil)
			p.registerTurnHitRegions(1, mainWidth-2, innerHeight)
			p.hitRegionsDirty = false
		}

		return rightPane
	}

	// RenderPanel handles borders internally, so only subtract divider
	available := p.width - dividerWidth
	sidebarWidth := p.sidebarWidth
	if sidebarWidth == 0 {
		sidebarWidth = available * 30 / 100
	}
	if sidebarWidth < 25 {
		sidebarWidth = 25
	}
	if sidebarWidth > available-40 {
		sidebarWidth = available - 40
	}
	mainWidth := available - sidebarWidth
	if mainWidth < 40 {
		mainWidth = 40
	}

	// Store for use by content renderers
	p.sidebarWidth = sidebarWidth

	// Determine if panes are active based on focus
	sidebarActive := p.activePane == PaneSidebar
	mainActive := p.activePane != PaneSidebar

	// Render sidebar (session list)
	sidebarContent := p.renderSidebarPane(innerHeight)

	// Render main pane (messages)
	mainContent := p.renderMainPane(mainWidth, innerHeight)

	// Apply gradient border styles
	leftPane := styles.RenderPanel(sidebarContent, sidebarWidth, paneHeight, sidebarActive)

	// Render visible divider
	divider := p.renderDivider(paneHeight)

	rightPane := styles.RenderPanel(mainContent, mainWidth, paneHeight, mainActive)

	// Only rebuild hit regions when dirty (td-ea784b03)
	mainX := sidebarWidth + dividerWidth
	if p.hitRegionsDirty {
		// Clear and re-register hit regions
		p.mouseHandler.HitMap.Clear()

		// Register hit regions (order matters: last = highest priority)
		// Sidebar region - lowest priority fallback
		p.mouseHandler.HitMap.AddRect(regionSidebar, 0, 0, sidebarWidth, p.height, nil)
		// Main pane region (after divider) - medium priority
		p.mouseHandler.HitMap.AddRect(regionMainPane, mainX, 0, mainWidth, p.height, nil)
		// Divider region - HIGH PRIORITY (registered after panes so it wins in overlap)
		dividerX := sidebarWidth
		dividerHitWidth := 3
		p.mouseHandler.HitMap.AddRect(regionPaneDivider, dividerX, 0, dividerHitWidth, p.height, nil)

		// Session item regions - HIGH PRIORITY
		p.registerSessionHitRegions(sidebarWidth, innerHeight)

		// Turn item regions - HIGHEST PRIORITY (registered last)
		p.registerTurnHitRegions(mainX+1, mainWidth-2, innerHeight)

		p.hitRegionsDirty = false
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, divider, rightPane)
}

// registerSessionHitRegions registers mouse hit regions for visible session items.
// This mirrors the rendering logic in renderSidebarPane/renderGroupedCompactSessions.
func (p *Plugin) registerSessionHitRegions(sidebarWidth, contentHeight int) {
	if p.filterMode {
		return // Filter menu is shown instead of sessions
	}

	sessions := p.visibleSessions()
	if len(sessions) == 0 {
		return
	}

	// Y offset: panel border (1) + title line (1) + optional search/filter line
	headerY := 2 // border + title
	if p.searchMode || p.filterActive {
		headerY = 3 // border + title + search/filter line
	}

	// X offset: panel border (1) + padding (1) = 2
	// The PanelActive/PanelInactive styles have Padding(0, 1) which adds horizontal padding
	hitX := 2

	// Hit region width: sidebarWidth - border(2) - padding(2) = sidebarWidth - 4
	hitWidth := sidebarWidth - 4
	if hitWidth < 10 {
		hitWidth = 10
	}

	// Track visual line position and visible session count
	lineCount := 0
	currentGroup := ""

	for i := p.scrollOff; i < len(sessions) && lineCount < contentHeight; i++ {
		session := sessions[i]

		// In grouped mode (not searching), account for group headers and spacers
		if !p.searchMode {
			sessionGroup := getSessionGroup(session.UpdatedAt)
			if sessionGroup != currentGroup {
				// Spacer before Yesterday/This Week (except first group)
				if currentGroup != "" && (sessionGroup == "Yesterday" || sessionGroup == "This Week") {
					lineCount++
					if lineCount >= contentHeight {
						break
					}
				}
				// Group header line
				currentGroup = sessionGroup
				lineCount++
				if lineCount >= contentHeight {
					break
				}
			}
		}

		// Register hit region for this session
		itemY := headerY + lineCount
		p.mouseHandler.HitMap.AddRect(regionSessionItem, hitX, itemY, hitWidth, 1, i)
		lineCount++
	}
}

// registerTurnHitRegions registers mouse hit regions for visible turn items in the main pane.
func (p *Plugin) registerTurnHitRegions(mainX, contentWidth, contentHeight int) {
	if p.detailMode {
		return
	}

	// Y offset: panel border (1) + header lines (4: title, stats, resume cmd, separator)
	headerY := 5
	currentY := headerY

	if p.turnViewMode {
		// Turn view: register hit regions for turns
		if len(p.turns) == 0 {
			return
		}
		for i := p.turnScrollOff; i < len(p.turns); i++ {
			turn := p.turns[i]
			turnHeight := p.calculateTurnHeight(turn, contentWidth)

			if currentY+turnHeight > contentHeight+headerY {
				break
			}

			p.mouseHandler.HitMap.AddRect(regionTurnItem, mainX, currentY, contentWidth, turnHeight, i)
			currentY += turnHeight
		}
	} else {
		// Conversation flow: register hit regions for messages
		if len(p.messages) == 0 {
			return
		}
		p.registerMessageHitRegions(mainX, contentWidth, contentHeight, headerY)
	}
}

// registerMessageHitRegions registers mouse hit regions for visible messages in conversation flow.
// Uses visibleMsgRanges populated during renderConversationFlow for accurate positioning.
func (p *Plugin) registerMessageHitRegions(mainX, contentWidth, contentHeight, headerY int) {
	for _, mr := range p.visibleMsgRanges {
		// Convert relative line position to screen Y coordinate
		screenY := headerY + mr.StartLine
		if mr.LineCount > 0 && screenY < headerY+contentHeight {
			p.mouseHandler.HitMap.AddRect(regionMessageItem, mainX, screenY, contentWidth, mr.LineCount, mr.MsgIdx)
		}
	}
}

// calculateTurnHeight returns the number of lines a turn will occupy when rendered.
func (p *Plugin) calculateTurnHeight(turn Turn, maxWidth int) int {
	height := 1 // header line always present
	if turn.ThinkingTokens > 0 {
		height++
	}
	content := turn.Preview(maxWidth - 7)
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.TrimSpace(content)
	if content != "" {
		height++
	}
	if turn.ToolCount > 0 {
		height++
	}
	return height
}

// renderDivider renders the visible divider between panes.
func (p *Plugin) renderDivider(height int) string {
	dividerStyle := lipgloss.NewStyle().
		Foreground(styles.BorderNormal).
		MarginTop(1) // Shifts down to align with pane content

	var sb strings.Builder
	for i := 0; i < height; i++ {
		sb.WriteString("│")
		if i < height-1 {
			sb.WriteString("\n")
		}
	}
	return dividerStyle.Render(sb.String())
}

// renderSidebarPane renders the session list for the sidebar.
func (p *Plugin) renderSidebarPane(height int) string {
	var sb strings.Builder

	sessions := p.visibleSessions()

	// Content width = sidebar width - border (2) - padding (2) = 4
	contentWidth := p.sidebarWidth - 4
	if contentWidth < 15 {
		contentWidth = 15
	}

	// Header with count
	countStr := fmt.Sprintf("%d", len(p.sessions))
	if p.searchMode && p.searchQuery != "" {
		countStr = fmt.Sprintf("%d/%d", len(sessions), len(p.sessions))
	}
	// Truncate count if needed
	maxCountLen := contentWidth - len("Sessions ")
	if maxCountLen > 0 && len(countStr) > maxCountLen {
		countStr = countStr[:maxCountLen]
	}
	sb.WriteString(styles.Title.Render("Sessions"))
	sb.WriteString(styles.Muted.Render(" " + countStr))
	sb.WriteString("\n")

	linesUsed := 1

	// Search bar (if in search mode)
	if p.searchMode {
		searchLine := fmt.Sprintf("/%s█", p.searchQuery)
		if len(searchLine) > contentWidth {
			searchLine = searchLine[:contentWidth]
		}
		sb.WriteString(styles.StatusInProgress.Render(searchLine))
		sb.WriteString("\n")
		linesUsed++
	} else if p.filterActive {
		filterStr := p.filters.String()
		if len(filterStr) > contentWidth {
			filterStr = filterStr[:contentWidth-3] + "..."
		}
		sb.WriteString(styles.Muted.Render(filterStr))
		sb.WriteString("\n")
		linesUsed++
	}

	// Filter menu (if in filter mode)
	if p.filterMode {
		sb.WriteString(p.renderFilterMenu(height - linesUsed))
		return sb.String()
	}

	// Session list
	if len(sessions) == 0 {
		// Show skeleton while loading, "No sessions" when done (td-6cc19f)
		if !p.initialLoadDone {
			contentHeight := height - linesUsed
			if contentHeight < 1 {
				contentHeight = 1
			}
			sb.WriteString(p.skeleton.View(contentWidth))
			return sb.String()
		}
		if p.searchMode {
			sb.WriteString(styles.Muted.Render("No matching sessions"))
		} else {
			sb.WriteString(styles.Muted.Render("No sessions"))
		}
		return sb.String()
	}

	// Render sessions
	contentHeight := height - linesUsed
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Reserve 1 column for scrollbar
	sessionWidth := contentWidth - 1
	if sessionWidth < 15 {
		sessionWidth = 15
	}

	var sessionSB strings.Builder
	if !p.searchMode {
		groups := GroupSessionsByTime(sessions)
		p.renderGroupedCompactSessions(&sessionSB, groups, contentHeight, sessionWidth)
	} else {
		end := p.scrollOff + contentHeight
		if end > len(sessions) {
			end = len(sessions)
		}

		for i := p.scrollOff; i < end; i++ {
			session := sessions[i]
			selected := i == p.cursor
			sessionSB.WriteString(p.renderCompactSessionRow(session, selected, sessionWidth))
			sessionSB.WriteString("\n")
		}
	}

	sessionContent := strings.TrimRight(sessionSB.String(), "\n")

	// Render scrollbar
	scrollbar := ui.RenderScrollbar(ui.ScrollbarParams{
		TotalItems:   len(sessions),
		ScrollOffset: p.scrollOff,
		VisibleItems: contentHeight,
		TrackHeight:  contentHeight,
	})

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, sessionContent, scrollbar))

	return sb.String()
}

func (p *Plugin) renderGroupedCompactSessions(sb *strings.Builder, groups []SessionGroup, contentHeight int, contentWidth int) {
	sessions := p.visibleSessions()

	lineCount := 0
	currentGroup := ""

	for i := p.scrollOff; i < len(sessions) && lineCount < contentHeight; i++ {
		session := sessions[i]
		sessionGroup := getSessionGroup(session.UpdatedAt)

		if sessionGroup != currentGroup {
			if currentGroup != "" && (sessionGroup == "Yesterday" || sessionGroup == "This Week") {
				sb.WriteString("\n")
				lineCount++
				if lineCount >= contentHeight {
					break
				}
			}

			currentGroup = sessionGroup

			// Find group count
			groupStats := ""
			for _, g := range groups {
				if g.Label == sessionGroup {
					groupStats = fmt.Sprintf(" (%d)", g.Summary.SessionCount)
					break
				}
			}

			groupHeader := sessionGroup + groupStats
			if len(groupHeader) > contentWidth {
				groupHeader = groupHeader[:contentWidth]
			}
			sb.WriteString(styles.Code.Render(groupHeader))
			sb.WriteString("\n")
			lineCount++
			if lineCount >= contentHeight {
				break
			}
		}

		selected := i == p.cursor
		sb.WriteString(p.renderCompactSessionRow(session, selected, contentWidth))
		sb.WriteString("\n")
		lineCount++
	}
}

// renderCompactSessionRow renders a compact session row for the sidebar.
// Format: [active] [icon] [worktree] Session title...              12m  45k
func (p *Plugin) renderCompactSessionRow(session adapter.Session, selected bool, maxWidth int) string {
	// Get badge text for width calculations (plain text length)
	badgeText := adapterBadgeText(session)

	// Format worktree badge if session is from a different worktree
	worktreeBadge := ""
	if session.WorktreeName != "" {
		// Truncate long worktree names to keep UI clean (rune-safe for Unicode)
		wtName := session.WorktreeName
		runes := []rune(wtName)
		if len(runes) > 12 {
			wtName = string(runes[:9]) + "..."
		}
		worktreeBadge = "[" + wtName + "]"
	}

	// Format duration - only if we have data
	lengthCol := ""
	if session.Duration > 0 {
		lengthCol = formatSessionDuration(session.Duration)
	}

	// Format token count - only if we have data
	tokenCol := ""
	if session.TotalTokens > 0 {
		tokenCol = formatK(session.TotalTokens)
	}

	// Calculate right column width (only for columns that have data)
	rightColWidth := 0
	if lengthCol != "" {
		rightColWidth += len(lengthCol)
	}
	if tokenCol != "" {
		if rightColWidth > 0 {
			rightColWidth += 1 // space between columns
		}
		rightColWidth += len(tokenCol)
	}

	// Calculate prefix length for width calculations
	// active(1) + badge + space + worktree + space (if worktree)
	prefixLen := 1 + len(badgeText) + 1
	if worktreeBadge != "" {
		prefixLen += len(worktreeBadge) + 1 // badge + space
	}
	if session.IsSubAgent {
		prefixLen += 2 // extra indent for sub-agents
	}
	// Add right column width plus spacing if present
	if rightColWidth > 0 {
		prefixLen += rightColWidth + 2 // space before + space after
	}

	// Session name/ID
	name := session.Name
	if name == "" {
		name = shortID(session.ID)
	}

	// Calculate available width for name
	nameWidth := maxWidth - prefixLen
	if nameWidth < 5 {
		nameWidth = 5
	}

	// Truncate name to fit (rune-safe for Unicode)
	if runes := []rune(name); len(runes) > nameWidth {
		name = string(runes[:nameWidth-3]) + "..."
	}

	// Calculate padding for right-aligned stats
	visibleLen := 0
	if session.IsSubAgent {
		visibleLen += 2
	}
	visibleLen += 1                              // indicator
	visibleLen += len(badgeText) + 1 + len(name) // badge + space + name
	if worktreeBadge != "" {
		visibleLen += len(worktreeBadge) + 1 // worktree badge + space
	}
	padding := maxWidth - visibleLen - rightColWidth - 1
	if padding < 0 {
		padding = 0
	}

	// Build the row with styling
	var sb strings.Builder

	// Sub-agent indent
	if session.IsSubAgent {
		sb.WriteString("  ")
	}

	// Activity indicator with colors
	if session.IsActive {
		sb.WriteString(styles.StatusInProgress.Render("●"))
	} else if session.IsSubAgent {
		sb.WriteString(styles.Muted.Render("↳"))
	} else {
		sb.WriteString(" ")
	}

	// Colored adapter icon + worktree badge + name based on session type
	if session.IsSubAgent {
		// Sub-agents: muted styling
		sb.WriteString(styles.Muted.Render(badgeText))
		sb.WriteString(" ")
		if worktreeBadge != "" {
			sb.WriteString(styles.Muted.Render(worktreeBadge))
			sb.WriteString(" ")
		}
		sb.WriteString(styles.Subtitle.Render(name))
	} else {
		// Top-level: use colored adapter icon
		sb.WriteString(renderAdapterIcon(session))
		sb.WriteString(" ")
		if worktreeBadge != "" {
			// Cyan/teal color for worktree badge to stand out
			sb.WriteString(lipgloss.NewStyle().Foreground(styles.Success).Render(worktreeBadge))
			sb.WriteString(" ")
		}
		sb.WriteString(styles.Body.Render(name))
	}

	// Padding and right-aligned stats (only if we have data)
	if rightColWidth > 0 && padding > 0 {
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString(" ")
		if lengthCol != "" {
			if session.IsSubAgent {
				sb.WriteString(styles.Muted.Render(lengthCol))
			} else {
				sb.WriteString(styles.Subtitle.Render(lengthCol))
			}
		}
		if tokenCol != "" {
			if lengthCol != "" {
				sb.WriteString(" ")
			}
			sb.WriteString(styles.Subtle.Render(tokenCol))
		}
	}

	row := sb.String()

	// For selected rows, build plain text version with background highlight
	if selected {
		var plain strings.Builder
		if session.IsSubAgent {
			plain.WriteString("  ")
		}
		if session.IsActive {
			plain.WriteString("●")
		} else if session.IsSubAgent {
			plain.WriteString("↳")
		} else {
			plain.WriteString(" ")
		}
		plain.WriteString(badgeText)
		plain.WriteString(" ")
		if worktreeBadge != "" {
			plain.WriteString(worktreeBadge)
			plain.WriteString(" ")
		}
		plain.WriteString(name)
		if rightColWidth > 0 && padding > 0 {
			plain.WriteString(strings.Repeat(" ", padding))
			plain.WriteString(" ")
			if lengthCol != "" {
				plain.WriteString(lengthCol)
			}
			if tokenCol != "" {
				if lengthCol != "" {
					plain.WriteString(" ")
				}
				plain.WriteString(tokenCol)
			}
		}
		plainRow := plain.String()
		// Pad to full width for proper background highlight
		if len(plainRow) < maxWidth {
			plainRow += strings.Repeat(" ", maxWidth-len(plainRow))
		}
		return styles.ListItemSelected.Render(plainRow)
	}

	return row
}

// renderMainPane renders the message list for the main pane.
func (p *Plugin) renderMainPane(paneWidth, height int) string {
	// Content width = pane width - border (2) - padding (2) = 4
	contentWidth := paneWidth - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	if p.selectedSession == "" {
		return styles.Muted.Render("Select a session to view messages")
	}

	// If in detail mode, render the turn detail instead of turn list
	if p.detailMode && p.detailTurn != nil {
		return p.renderDetailPaneContent(contentWidth, height)
	}

	var sb strings.Builder

	// Find session info
	var session *adapter.Session
	for i := range p.sessions {
		if p.sessions[i].ID == p.selectedSession {
			session = &p.sessions[i]
			break
		}
	}

	// Header Line 1: Adapter icon + Session name
	sessionName := shortID(p.selectedSession)
	if session != nil && session.Name != "" {
		sessionName = session.Name
	}

	// Calculate max length for session name (leave room for icon)
	maxSessionLen := contentWidth - 4
	if maxSessionLen < 10 {
		maxSessionLen = 10
	}
	if len(sessionName) > maxSessionLen {
		sessionName = sessionName[:maxSessionLen-3] + "..."
	}

	// Build header with colored adapter icon
	if session != nil {
		sb.WriteString(renderAdapterIcon(*session))
		sb.WriteString(" ")
	}
	sb.WriteString(styles.Title.Render(sessionName))
	sb.WriteString("\n")

	// Header Line 2: Model badge │ msgs │ tokens │ cost │ date
	if p.sessionSummary != nil {
		s := p.sessionSummary

		// Build stats with model badge
		var statsParts []string

		// Model badge (colorful)
		if s.PrimaryModel != "" {
			badge := renderModelBadge(s.PrimaryModel)
			if badge != "" {
				statsParts = append(statsParts, badge)
			}
		} else if session != nil {
			// Fallback to adapter short name
			shortName := adapterShortName(session)
			if shortName != "" {
				statsParts = append(statsParts, styles.Code.Render(shortName))
			}
		}

		// Message count
		statsParts = append(statsParts, fmt.Sprintf("%d msgs", s.MessageCount))

		// Token flow
		statsParts = append(statsParts, fmt.Sprintf("%s→%s", formatK(s.TotalTokensIn), formatK(s.TotalTokensOut)))

		// Cost estimate
		if session != nil && session.EstCost > 0 {
			statsParts = append(statsParts, formatCost(session.EstCost))
		}

		// Last updated
		if session != nil && !session.UpdatedAt.IsZero() {
			statsParts = append(statsParts, session.UpdatedAt.Local().Format("Jan 02 15:04"))
		}

		statsLine := strings.Join(statsParts, " │ ")
		// Check if we need to truncate (accounting for ANSI codes in badge)
		if lipgloss.Width(statsLine) > contentWidth {
			// Rebuild without badge for narrow widths
			statsParts = statsParts[1:] // Remove badge
			statsLine = strings.Join(statsParts, " │ ")
		}
		sb.WriteString(styles.Muted.Render(statsLine))
		sb.WriteString("\n")
	}

	// Header Line 3: Resume command with copy hint
	if session != nil {
		resumeCmd := resumeCommand(session)
		if resumeCmd != "" {
			maxCmdLen := contentWidth - 12 // Leave room for copy hint
			if len(resumeCmd) > maxCmdLen {
				resumeCmd = resumeCmd[:maxCmdLen-3] + "..."
			}
			sb.WriteString(styles.Code.Render(resumeCmd))
			sb.WriteString("  ")
			sb.WriteString(styles.Subtle.Render("[Y:copy]"))
			sb.WriteString("\n")
		}
	}

	// Pagination indicator (td-313ea851)
	if p.totalMessages > maxMessagesInMemory {
		startIdx := p.totalMessages - p.messageOffset - len(p.messages) + 1
		endIdx := p.totalMessages - p.messageOffset
		if startIdx < 1 {
			startIdx = 1
		}
		pageInfo := fmt.Sprintf("Showing %d-%d of %d messages", startIdx, endIdx, p.totalMessages)
		if p.hasOlderMsgs {
			pageInfo += " [p:older"
			if p.messageOffset > 0 {
				pageInfo += " n:newer"
			}
			pageInfo += "]"
		} else if p.messageOffset > 0 {
			pageInfo += " [n:newer]"
		}
		if len(pageInfo) > contentWidth {
			pageInfo = pageInfo[:contentWidth-3] + "..."
		}
		sb.WriteString(styles.StatusModified.Render(pageInfo))
		sb.WriteString("\n")
	}

	sepWidth := contentWidth
	if sepWidth > 60 {
		sepWidth = 60
	}
	sb.WriteString(styles.Muted.Render(strings.Repeat("─", sepWidth)))
	sb.WriteString("\n")

	contentHeight := height - 4 // Account for header lines
	// Adjust for pagination indicator if visible (td-313ea851)
	if p.totalMessages > maxMessagesInMemory {
		contentHeight--
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Check for empty/loading state
	if len(p.messages) == 0 && len(p.turns) == 0 {
		if session != nil && session.MessageCount == 0 {
			sb.WriteString(styles.Muted.Render("No messages (metadata only)"))
		} else {
			sb.WriteString(styles.Muted.Render("Loading messages..."))
		}
		return sb.String()
	}

	if p.turnViewMode {
		// Turn-based view (metadata-focused)
		if len(p.turns) == 0 {
			sb.WriteString(styles.Muted.Render("No turns"))
			return sb.String()
		}
		lineCount := 0
		for i := p.turnScrollOff; i < len(p.turns) && lineCount < contentHeight; i++ {
			turn := p.turns[i]
			lines := p.renderCompactTurn(turn, i, contentWidth)
			for _, line := range lines {
				if lineCount >= contentHeight {
					break
				}
				sb.WriteString(line)
				sb.WriteString("\n")
				lineCount++
			}
		}
	} else {
		// Conversation flow view (content-focused, default)
		lines := p.renderConversationFlow(contentWidth, contentHeight)
		for _, line := range lines {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	// Strip explicit background colors so everything falls through to the
	// terminal's default background, preventing splotchy color mismatches
	// from inner styled elements (model badges, glamour markdown, etc.)
	return stripANSIBackground(sb.String())
}

// renderDetailPaneContent renders the turn detail in the right pane (two-pane mode).
func (p *Plugin) renderDetailPaneContent(contentWidth, height int) string {
	var sb strings.Builder

	if p.detailTurn == nil {
		return styles.Muted.Render("No turn selected")
	}

	turn := p.detailTurn
	msgCount := len(turn.Messages)

	// Header: Turn Role (with message count if > 1)
	roleLabel := turn.Role
	if msgCount > 1 {
		roleLabel = fmt.Sprintf("%s (%d messages)", turn.Role, msgCount)
	}
	header := fmt.Sprintf("%s Turn", strings.Title(roleLabel))
	if len(header) > contentWidth-10 {
		header = header[:contentWidth-13] + "..."
	}
	sb.WriteString(styles.Title.Render(header))
	sb.WriteString("  ")
	sb.WriteString(styles.Muted.Render("[esc]"))
	sb.WriteString("\n")

	// Stats line
	var stats []string
	if turn.TotalTokensIn > 0 || turn.TotalTokensOut > 0 {
		stats = append(stats, fmt.Sprintf("%s→%s tokens", formatK(turn.TotalTokensIn), formatK(turn.TotalTokensOut)))
	}
	if turn.ThinkingTokens > 0 {
		stats = append(stats, fmt.Sprintf("%s thinking", formatK(turn.ThinkingTokens)))
	}
	if turn.ToolCount > 0 {
		stats = append(stats, fmt.Sprintf("%d tools", turn.ToolCount))
	}
	if len(stats) > 0 {
		statsLine := strings.Join(stats, " │ ")
		if len(statsLine) > contentWidth {
			statsLine = statsLine[:contentWidth-3] + "..."
		}
		sb.WriteString(styles.Muted.Render(statsLine))
		sb.WriteString("\n")
	}

	// Separator
	sepWidth := contentWidth
	if sepWidth > 60 {
		sepWidth = 60
	}
	sb.WriteString(styles.Muted.Render(strings.Repeat("─", sepWidth)))
	sb.WriteString("\n")

	// Build content lines for all messages in turn
	var contentLines []string

	for msgIdx, msg := range turn.Messages {
		// Message separator (except for first)
		if msgIdx > 0 {
			contentLines = append(contentLines, "")
			contentLines = append(contentLines, styles.Muted.Render(fmt.Sprintf("── Message %d/%d ──", msgIdx+1, msgCount)))
			contentLines = append(contentLines, "")
		}

		// Thinking blocks
		for i, tb := range msg.ThinkingBlocks {
			contentLines = append(contentLines, styles.Code.Render(fmt.Sprintf("Thinking %d (%d tokens)", i+1, tb.TokenCount)))
			// Wrap thinking content
			thinkingLines := wrapText(tb.Content, contentWidth-2)
			for _, line := range thinkingLines {
				contentLines = append(contentLines, styles.Muted.Render(line))
			}
			contentLines = append(contentLines, "")
		}

		// Main content
		if msg.Content != "" {
			// Render markdown content
			msgLines := p.renderContent(msg.Content, contentWidth-2)
			for _, line := range msgLines {
				contentLines = append(contentLines, line) // Glamour already styled
			}
			contentLines = append(contentLines, "")
		}

		// Tool uses
		if len(msg.ToolUses) > 0 {
			contentLines = append(contentLines, styles.Subtitle.Render("Tools:"))
			for _, tu := range msg.ToolUses {
				toolLine := tu.Name
				if filePath := extractFilePath(tu.Input); filePath != "" {
					toolLine += ": " + filePath
				}
				if len(toolLine) > contentWidth-2 {
					toolLine = toolLine[:contentWidth-5] + "..."
				}
				contentLines = append(contentLines, styles.Code.Render("  "+toolLine))
			}
			contentLines = append(contentLines, "")
		}
	}

	// Apply scroll offset
	headerLines := 3 // title + stats + separator
	contentHeight := height - headerLines
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Clamp scroll
	maxScroll := len(contentLines) - contentHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.detailScroll > maxScroll {
		p.detailScroll = maxScroll
	}
	if p.detailScroll < 0 {
		p.detailScroll = 0
	}

	// Reserve space for scroll indicators (up to 2 lines)
	indicatorLines := 0
	if maxScroll > 0 {
		if p.detailScroll > 0 {
			indicatorLines++
		}
		if p.detailScroll < maxScroll {
			indicatorLines++
		}
	}
	displayHeight := contentHeight - indicatorLines
	if displayHeight < 1 {
		displayHeight = 1
	}

	start := p.detailScroll
	end := start + displayHeight
	if end > len(contentLines) {
		end = len(contentLines)
	}

	for i := start; i < end; i++ {
		sb.WriteString(contentLines[i])
		sb.WriteString("\n")
	}

	// Scroll indicators (space already reserved)
	if maxScroll > 0 {
		if p.detailScroll > 0 {
			sb.WriteString(styles.Muted.Render(fmt.Sprintf("↑ %d more above", p.detailScroll)))
			sb.WriteString("\n")
		}
		remaining := len(contentLines) - end
		if remaining > 0 {
			sb.WriteString(styles.Muted.Render(fmt.Sprintf("↓ %d more below", remaining)))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// renderCompactTurn renders a turn (grouped messages) in compact format for two-pane view.
func (p *Plugin) renderCompactTurn(turn Turn, turnIndex int, maxWidth int) []string {
	var lines []string
	selected := turnIndex == p.turnCursor

	// Header line: [timestamp] role (N msgs) tokens
	ts := turn.FirstTimestamp()

	// Build stats string
	msgCount := len(turn.Messages)
	var stats []string
	if msgCount > 1 {
		stats = append(stats, fmt.Sprintf("%d msgs", msgCount))
	}
	if turn.TotalTokensIn > 0 || turn.TotalTokensOut > 0 {
		stats = append(stats, fmt.Sprintf("%s→%s", formatK(turn.TotalTokensIn), formatK(turn.TotalTokensOut)))
	}
	statsStr := ""
	if len(stats) > 0 {
		statsStr = " (" + strings.Join(stats, ", ") + ")"
	}

	// Build header line
	if selected {
		// For selected: plain text with background highlight
		headerContent := fmt.Sprintf("[%s] %s%s", ts, turn.Role, statsStr)
		lines = append(lines, p.styleTurnLine(headerContent, true, maxWidth))
	} else {
		// For unselected: colored role badge with muted styling
		var roleStyle lipgloss.Style
		if turn.Role == "user" {
			roleStyle = styles.StatusInProgress
		} else {
			roleStyle = styles.StatusStaged
		}
		styledHeader := fmt.Sprintf("[%s] %s%s",
			styles.Muted.Render(ts),
			roleStyle.Render(turn.Role),
			styles.Muted.Render(statsStr))
		lines = append(lines, styledHeader)
	}

	// Thinking indicator (aggregate) - indented under header
	if turn.ThinkingTokens > 0 {
		thinkingLine := fmt.Sprintf("   ├─ [thinking] %s tokens", formatK(turn.ThinkingTokens))
		if len(thinkingLine) > maxWidth {
			thinkingLine = thinkingLine[:maxWidth-3] + "..."
		}
		lines = append(lines, p.styleTurnLine(thinkingLine, selected, maxWidth))
	}

	// Content preview from first meaningful message - indented under header
	content := turn.Preview(maxWidth - 5)
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.TrimSpace(content)
	if content != "" {
		contentLine := "   " + content
		lines = append(lines, p.styleTurnLine(contentLine, selected, maxWidth))
	}

	// Tool uses (aggregate) - indented under header
	if turn.ToolCount > 0 {
		toolLine := fmt.Sprintf("   └─ %d tools", turn.ToolCount)
		lines = append(lines, p.styleTurnLine(toolLine, selected, maxWidth))
	}

	return lines
}

// styleTurnLine applies selection highlighting or default muted styling to a turn line.
func (p *Plugin) styleTurnLine(content string, selected bool, maxWidth int) string {
	if selected {
		// Pad to full width for proper background highlighting
		if len(content) < maxWidth {
			content += strings.Repeat(" ", maxWidth-len(content))
		}
		return styles.ListItemSelected.Render(content)
	}
	return styles.Muted.Render(content)
}

// renderCompactMessage renders a message in compact format for two-pane view.
func (p *Plugin) renderCompactMessage(msg adapter.Message, msgIndex int, maxWidth int) []string {
	var lines []string

	// Header line: [timestamp] role  tokens
	ts := msg.Timestamp.Local().Format("15:04")
	var roleStyle lipgloss.Style
	if msg.Role == "user" {
		roleStyle = styles.StatusInProgress
	} else {
		roleStyle = styles.StatusStaged
	}

	// Cursor indicator
	var styledCursor string
	if msgIndex == p.msgCursor {
		styledCursor = styles.ListCursor.Render("> ")
	} else {
		styledCursor = "  "
	}

	// Token info - truncate if needed
	tokens := ""
	if msg.OutputTokens > 0 || msg.InputTokens > 0 {
		tokens = fmt.Sprintf(" (%s→%s)", formatK(msg.InputTokens), formatK(msg.OutputTokens))
	}

	// Calculate if we need to truncate role (rune-safe for Unicode)
	role := msg.Role
	roleRunes := []rune(role)
	// Account for: cursor(2) + [](2) + ts(5) + space(1) + role + tokens
	usedWidth := 2 + 2 + len(ts) + 1 + len(roleRunes) + len(tokens)
	if usedWidth > maxWidth && len(roleRunes) > 4 {
		role = string(roleRunes[:4])
	}

	// Build styled header
	styledHeader := styledCursor + fmt.Sprintf("[%s] %s%s",
		styles.Muted.Render(ts),
		roleStyle.Render(role),
		styles.Muted.Render(tokens))
	lines = append(lines, styledHeader)

	// Thinking indicator
	if len(msg.ThinkingBlocks) > 0 {
		var totalTokens int
		for _, tb := range msg.ThinkingBlocks {
			totalTokens += tb.TokenCount
		}
		thinkingLine := fmt.Sprintf("  ├─ [thinking] %s tokens", formatK(totalTokens))
		if len(thinkingLine) > maxWidth {
			thinkingLine = thinkingLine[:maxWidth-3] + "..."
		}
		lines = append(lines, styles.Muted.Render(thinkingLine))
	}

	// Content preview (truncated, rune-safe for Unicode)
	content := msg.Content
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.TrimSpace(content)
	contentMaxLen := maxWidth - 4 // Account for "   " prefix
	if contentMaxLen < 10 {
		contentMaxLen = 10
	}
	if runes := []rune(content); len(runes) > contentMaxLen {
		content = string(runes[:contentMaxLen-3]) + "..."
	}
	if content != "" {
		lines = append(lines, "  "+styles.Body.Render(content))
	}

	// Tool uses (compact)
	if len(msg.ToolUses) > 0 {
		toolLine := fmt.Sprintf("  └─ %d tools", len(msg.ToolUses))
		lines = append(lines, styles.Code.Render(toolLine))
	}

	return lines
}

// renderConversationFlow renders messages as a scrollable chat thread (Claude Code web UI style).
func (p *Plugin) renderConversationFlow(contentWidth, height int) []string {
	// Clear previous tracking data
	p.visibleMsgRanges = p.visibleMsgRanges[:0]
	p.msgLinePositions = p.msgLinePositions[:0]

	if len(p.messages) == 0 {
		return []string{styles.Muted.Render("No messages")}
	}

	var allLines []string
	prevRole := ""

	for msgIdx, msg := range p.messages {
		// Skip user messages that are just tool results (they'll be shown inline with tool_use)
		if p.isToolResultOnlyMessage(msg) {
			continue
		}

		// Add subtle turn separator when role changes (user ↔ assistant)
		if prevRole != "" && prevRole != msg.Role {
			// Create a subtle visual break between turns
			sepWidth := contentWidth / 3
			if sepWidth > 20 {
				sepWidth = 20
			}
			separator := strings.Repeat("─", sepWidth)
			allLines = append(allLines, styles.Subtle.Render("  "+separator))
		}
		prevRole = msg.Role

		// Track where this message starts
		startLine := len(allLines)

		// Render message bubble
		msgLines := p.renderMessageBubble(msg, msgIdx, contentWidth)
		allLines = append(allLines, msgLines...)

		allLines = append(allLines, "") // Gap between messages

		// Store position info for scroll calculations (include gap line in count)
		p.msgLinePositions = append(p.msgLinePositions, msgLinePos{
			MsgIdx:    msgIdx,
			StartLine: startLine,
			LineCount: len(msgLines) + 1, // +1 for gap line
		})
	}

	// Apply scroll offset
	maxScroll := len(allLines) - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.messageScroll > maxScroll {
		p.messageScroll = maxScroll
	}
	if p.messageScroll < 0 {
		p.messageScroll = 0
	}

	start := p.messageScroll
	end := start + height
	if end > len(allLines) {
		end = len(allLines)
	}

	if start >= len(allLines) {
		return []string{}
	}

	// Calculate visible ranges for hit region registration
	// screenLine is relative to content area (0 = first visible line)
	for _, mp := range p.msgLinePositions {
		msgEnd := mp.StartLine + mp.LineCount
		// Check if message is visible in the scroll window
		if msgEnd <= start {
			continue // Message is entirely before scroll window
		}
		if mp.StartLine >= end {
			break // Message is entirely after scroll window
		}

		// Calculate visible portion
		visibleStart := mp.StartLine - start
		if visibleStart < 0 {
			visibleStart = 0
		}
		visibleEnd := msgEnd - start
		if visibleEnd > height {
			visibleEnd = height
		}

		if visibleEnd > visibleStart {
			p.visibleMsgRanges = append(p.visibleMsgRanges, msgLineRange{
				MsgIdx:    mp.MsgIdx,
				StartLine: visibleStart,
				LineCount: visibleEnd - visibleStart,
			})
		}
	}

	return allLines[start:end]
}

