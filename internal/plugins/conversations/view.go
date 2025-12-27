package conversations

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sst/sidecar/internal/adapter"
	"github.com/sst/sidecar/internal/styles"
)

// renderNoAdapter renders the view when no adapter is available.
func renderNoAdapter() string {
	return styles.Muted.Render(" Claude Code sessions not available")
}

// renderSessions renders the session list view with time grouping.
func (p *Plugin) renderSessions() string {
	var sb strings.Builder

	sessions := p.visibleSessions()

	// Header with count
	countStr := fmt.Sprintf("%d sessions", len(p.sessions))
	if p.searchMode && p.searchQuery != "" {
		countStr = fmt.Sprintf("%d/%d", len(sessions), len(p.sessions))
	}
	header := fmt.Sprintf(" Claude Code Sessions                    %s", countStr)
	sb.WriteString(styles.PanelHeader.Render(header))
	sb.WriteString("\n")

	// Search bar (if in search mode)
	if p.searchMode {
		searchLine := fmt.Sprintf(" /%s█", p.searchQuery)
		sb.WriteString(styles.StatusInProgress.Render(searchLine))
		sb.WriteString("\n")
	} else {
		sb.WriteString(styles.Muted.Render(strings.Repeat("━", p.width-2)))
		sb.WriteString("\n")
	}

	// Content
	if len(sessions) == 0 {
		if p.searchMode {
			sb.WriteString(styles.Muted.Render(" No matching sessions"))
		} else {
			sb.WriteString(styles.Muted.Render(" No sessions found for this project"))
		}
	} else {
		headerLines := 2
		contentHeight := p.height - headerLines
		if contentHeight < 1 {
			contentHeight = 1
		}

		// Group sessions by time (only when not searching)
		if !p.searchMode {
			groups := GroupSessionsByTime(sessions)
			p.renderGroupedSessions(&sb, groups, contentHeight)
		} else {
			// Flat list when searching
			end := p.scrollOff + contentHeight
			if end > len(sessions) {
				end = len(sessions)
			}
			for i := p.scrollOff; i < end; i++ {
				session := sessions[i]
				selected := i == p.cursor
				sb.WriteString(p.renderSessionRow(session, selected))
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// renderGroupedSessions renders sessions with time group headers.
func (p *Plugin) renderGroupedSessions(sb *strings.Builder, groups []SessionGroup, contentHeight int) {
	sessions := p.visibleSessions()

	lineCount := 0
	currentGroup := ""

	for i := p.scrollOff; i < len(sessions) && lineCount < contentHeight; i++ {
		session := sessions[i]

		// Determine which group this session belongs to
		sessionGroup := getSessionGroup(session.UpdatedAt)

		// Render group header if group changed
		if sessionGroup != currentGroup {
			currentGroup = sessionGroup
			// Find group stats
			var groupStats string
			for _, g := range groups {
				if g.Label == sessionGroup {
					groupStats = fmt.Sprintf("%d sessions", g.Summary.SessionCount)
					break
				}
			}
			groupHeader := fmt.Sprintf(" %s (%s)", sessionGroup, groupStats)
			sb.WriteString(styles.PanelHeader.Render(groupHeader))
			sb.WriteString("\n")
			lineCount++
			if lineCount >= contentHeight {
				break
			}
		}

		selected := i == p.cursor
		sb.WriteString(p.renderSessionRow(session, selected))
		sb.WriteString("\n")
		lineCount++
	}
}

// getSessionGroup returns the time group label for a given timestamp.
func getSessionGroup(t time.Time) string {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	weekAgo := today.AddDate(0, 0, -7)

	switch {
	case t.After(today) || t.Equal(today):
		return "Today"
	case t.After(yesterday) || t.Equal(yesterday):
		return "Yesterday"
	case t.After(weekAgo):
		return "This Week"
	default:
		return "Older"
	}
}

// renderSessionRow renders a single session row with enhanced stats.
func (p *Plugin) renderSessionRow(session adapter.Session, selected bool) string {
	// Cursor
	cursor := "  "
	if selected {
		cursor = styles.ListCursor.Render("> ")
	}

	// Active indicator
	activeIndicator := " "
	if session.IsActive {
		activeIndicator = styles.StatusInProgress.Render("●")
	}

	// Timestamp - just time for today, date otherwise
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var ts string
	if session.UpdatedAt.After(today) {
		ts = session.UpdatedAt.Local().Format("15:04")
	} else {
		ts = session.UpdatedAt.Local().Format("01-02 15:04")
	}

	// Session name/ID
	name := session.Name
	if name == "" {
		name = shortID(session.ID)
	}

	// Duration
	dur := ""
	if session.Duration > 0 {
		dur = formatSessionDuration(session.Duration)
	}

	// Compose line style
	lineStyle := styles.ListItemNormal
	if selected {
		lineStyle = styles.ListItemSelected
	}

	// Build the row: cursor + active + time + name + duration
	// Format: ● 14:23  "Add auth flow"                        12m
	maxNameWidth := p.width - 25
	if len(name) > maxNameWidth && maxNameWidth > 3 {
		name = name[:maxNameWidth-3] + "..."
	}

	// Pad name to align duration
	namePadded := name
	if len(namePadded) < maxNameWidth {
		namePadded = name + strings.Repeat(" ", maxNameWidth-len(name))
	}

	return lineStyle.Render(fmt.Sprintf("%s%s %s  %s  %s",
		cursor,
		activeIndicator,
		styles.Muted.Render(ts),
		namePadded,
		styles.Muted.Render(dur)))
}

// renderMessages renders the message view with enhanced header.
func (p *Plugin) renderMessages() string {
	var sb strings.Builder

	// Find session info
	var session *adapter.Session
	for i := range p.sessions {
		if p.sessions[i].ID == p.selectedSession {
			session = &p.sessions[i]
			break
		}
	}

	sessionName := shortID(p.selectedSession)
	if session != nil && session.Name != "" {
		sessionName = session.Name
	}

	// Enhanced header with stats
	headerLines := p.renderSessionHeader(&sb, sessionName, session)

	// Tool summary panel (if toggled)
	if p.showToolSummary && p.sessionSummary != nil {
		headerLines += p.renderToolSummary(&sb)
	}

	// Content
	if len(p.messages) == 0 {
		sb.WriteString(styles.Muted.Render(" No messages in this session"))
	} else {
		contentHeight := p.height - headerLines
		if contentHeight < 1 {
			contentHeight = 1
		}

		// Render messages
		lineCount := 0
		for i := p.msgScrollOff; i < len(p.messages) && lineCount < contentHeight; i++ {
			msg := p.messages[i]
			lines := p.renderMessage(msg, i, p.width-4)
			for _, line := range lines {
				if lineCount >= contentHeight {
					break
				}
				sb.WriteString(line)
				sb.WriteString("\n")
				lineCount++
			}
		}
	}

	return sb.String()
}

// renderSessionHeader renders the enhanced session header. Returns lines used.
func (p *Plugin) renderSessionHeader(sb *strings.Builder, sessionName string, session *adapter.Session) int {
	lines := 0

	// Line 1: Session name and duration
	dur := ""
	if session != nil && session.Duration > 0 {
		dur = formatSessionDuration(session.Duration)
	}
	header := fmt.Sprintf(" Session: %s", sessionName)
	if dur != "" {
		padding := p.width - len(header) - len(dur) - 4
		if padding > 0 {
			header += strings.Repeat(" ", padding) + dur
		}
	}
	sb.WriteString(styles.PanelHeader.Render(header))
	sb.WriteString("\n")
	lines++

	// Line 2: Stats line (model, tokens, cost, files)
	if p.sessionSummary != nil {
		s := p.sessionSummary
		modelShort := modelShortName(s.PrimaryModel)
		if modelShort == "" {
			modelShort = "claude"
		}

		statsLine := fmt.Sprintf(" %s  │  %d msgs  │  %s in  %s out",
			modelShort,
			s.MessageCount,
			formatK(s.TotalTokensIn),
			formatK(s.TotalTokensOut))

		if s.TotalCost >= 0.01 {
			statsLine += fmt.Sprintf("  │  ~$%.2f", s.TotalCost)
		}
		if s.FileCount > 0 {
			statsLine += fmt.Sprintf("  │  %d files", s.FileCount)
		}

		sb.WriteString(styles.Muted.Render(statsLine))
		sb.WriteString("\n")
		lines++
	}

	sb.WriteString(styles.Muted.Render(strings.Repeat("━", p.width-2)))
	sb.WriteString("\n")
	lines++

	return lines
}

// renderToolSummary renders the tool impact summary panel. Returns lines used.
func (p *Plugin) renderToolSummary(sb *strings.Builder) int {
	s := p.sessionSummary
	if s == nil {
		return 0
	}

	lines := 0

	// Header
	sb.WriteString(styles.PanelHeader.Render(" Tool Usage                                    [t to toggle]"))
	sb.WriteString("\n")
	lines++

	sb.WriteString(styles.Muted.Render(strings.Repeat("─", p.width-2)))
	sb.WriteString("\n")
	lines++

	// Tool counts sorted by usage
	type toolCount struct {
		name  string
		count int
	}
	var counts []toolCount
	for name, count := range s.ToolCounts {
		counts = append(counts, toolCount{name, count})
	}
	// Sort by count descending
	for i := 0; i < len(counts)-1; i++ {
		for j := i + 1; j < len(counts); j++ {
			if counts[j].count > counts[i].count {
				counts[i], counts[j] = counts[j], counts[i]
			}
		}
	}

	// Show top 5 tools
	maxTools := 5
	if len(counts) < maxTools {
		maxTools = len(counts)
	}
	for i := 0; i < maxTools; i++ {
		tc := counts[i]
		toolLine := fmt.Sprintf(" %s (%d)", tc.name, tc.count)
		sb.WriteString(styles.Code.Render(toolLine))
		sb.WriteString("\n")
		lines++
	}

	sb.WriteString(styles.Muted.Render(strings.Repeat("─", p.width-2)))
	sb.WriteString("\n")
	lines++

	return lines
}

// renderMessage renders a single message.
func (p *Plugin) renderMessage(msg adapter.Message, msgIndex int, maxWidth int) []string {
	var lines []string

	// Header line: [timestamp] role  model  tokens  cost
	ts := msg.Timestamp.Local().Format("15:04:05")
	roleStyle := styles.Muted
	if msg.Role == "user" {
		roleStyle = styles.StatusInProgress
	} else {
		roleStyle = styles.StatusStaged
	}

	// Model badge
	modelBadge := ""
	if shortName := modelShortName(msg.Model); shortName != "" {
		modelBadge = "  " + styles.Code.Render(shortName)
	}

	// Enhanced token display: in/out/cache
	tokens := ""
	if msg.OutputTokens > 0 || msg.InputTokens > 0 {
		tokens = formatTokens(msg.InputTokens, msg.OutputTokens, msg.CacheRead)
	}

	// Cost estimate
	costStr := ""
	if msg.OutputTokens > 0 || msg.InputTokens > 0 {
		cost := estimateCost(msg.Model, msg.InputTokens, msg.OutputTokens, msg.CacheRead)
		if cost >= 0.01 {
			costStr = fmt.Sprintf("  ~$%.2f", cost)
		} else if cost > 0 {
			costStr = fmt.Sprintf("  ~$%.3f", cost)
		}
	}

	headerLine := fmt.Sprintf(" [%s] %s%s%s%s",
		styles.Muted.Render(ts),
		roleStyle.Render(msg.Role),
		modelBadge,
		styles.Muted.Render(tokens),
		styles.Muted.Render(costStr))
	lines = append(lines, headerLine)

	// Thinking blocks (collapsed by default)
	if len(msg.ThinkingBlocks) > 0 {
		expanded := p.expandedThinking[msg.ID]
		for _, tb := range msg.ThinkingBlocks {
			if expanded {
				// Show expanded thinking block
				thinkingHeader := fmt.Sprintf(" ├─ [thinking] %d tokens", tb.TokenCount)
				lines = append(lines, styles.Muted.Render(thinkingHeader))
				// Show content (truncated to ~500 chars)
				content := tb.Content
				if len(content) > 500 {
					content = content[:497] + "..."
				}
				thinkingLines := wrapText(content, maxWidth-5)
				for _, tl := range thinkingLines {
					lines = append(lines, styles.Muted.Render(" │   "+tl))
				}
			} else {
				// Show collapsed thinking block
				thinkingLine := fmt.Sprintf(" ├─ [thinking] %d tokens                    [T to expand]", tb.TokenCount)
				lines = append(lines, styles.Muted.Render(thinkingLine))
			}
		}
	}

	// Content (truncated if too long)
	content := msg.Content
	if len(content) > 200 {
		content = content[:197] + "..."
	}

	// Word wrap content
	contentLines := wrapText(content, maxWidth-2)
	for _, cl := range contentLines {
		lines = append(lines, " "+styles.Body.Render(cl))
	}

	// Tool uses with file paths
	if len(msg.ToolUses) > 0 {
		for _, tu := range msg.ToolUses {
			filePath := extractFilePath(tu.Input)
			var toolLine string
			if filePath != "" {
				toolLine = fmt.Sprintf(" %s: %s", tu.Name, filePath)
			} else {
				toolLine = fmt.Sprintf(" [tool] %s", tu.Name)
			}
			lines = append(lines, styles.Code.Render(toolLine))
		}
	}

	// Empty line between messages
	lines = append(lines, "")

	return lines
}

// wrapText wraps text to fit within maxWidth.
func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}

	// Replace newlines with spaces for simpler wrapping
	text = strings.ReplaceAll(text, "\n", " ")

	var lines []string
	words := strings.Fields(text)
	if len(words) == 0 {
		return lines
	}

	currentLine := words[0]
	for _, word := range words[1:] {
		if len(currentLine)+1+len(word) <= maxWidth {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}

// formatDuration formats a duration in human-readable form.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1d ago"
	}
	return fmt.Sprintf("%dd ago", days)
}

// formatTokens formats token counts compactly.
func formatTokens(input, output, cache int) string {
	parts := []string{}

	if input > 0 {
		parts = append(parts, fmt.Sprintf("in:%s", formatK(input)))
	}
	if output > 0 {
		parts = append(parts, fmt.Sprintf("out:%s", formatK(output)))
	}
	if cache > 0 {
		parts = append(parts, fmt.Sprintf("$:%s", formatK(cache)))
	}

	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, " ") + ")"
}

// formatK formats a number with K/M suffix.
func formatK(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// modelShortName maps model IDs to short display names.
func modelShortName(model string) string {
	switch {
	case strings.Contains(model, "opus"):
		return "opus"
	case strings.Contains(model, "sonnet"):
		return "sonnet"
	case strings.Contains(model, "haiku"):
		return "haiku"
	default:
		return ""
	}
}

// formatSessionDuration formats session duration for display.
func formatSessionDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

// estimateCost calculates cost in dollars based on model and tokens.
func estimateCost(model string, inputTokens, outputTokens, cacheRead int) float64 {
	var inRate, outRate float64
	switch {
	case strings.Contains(model, "opus"):
		inRate, outRate = 15.0, 75.0
	case strings.Contains(model, "sonnet"):
		inRate, outRate = 3.0, 15.0
	case strings.Contains(model, "haiku"):
		inRate, outRate = 0.25, 1.25
	default:
		inRate, outRate = 3.0, 15.0 // Default to sonnet rates
	}

	// Cache reads get 90% discount
	regularIn := inputTokens - cacheRead
	if regularIn < 0 {
		regularIn = 0
	}
	cacheInCost := float64(cacheRead) * inRate * 0.1 / 1_000_000
	regularInCost := float64(regularIn) * inRate / 1_000_000
	outCost := float64(outputTokens) * outRate / 1_000_000

	return cacheInCost + regularInCost + outCost
}

// extractFilePath extracts file_path from tool input JSON.
func extractFilePath(input string) string {
	if input == "" {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(input), &data); err != nil {
		return ""
	}
	if fp, ok := data["file_path"].(string); ok {
		return fp
	}
	return ""
}

// renderTwoPane renders the two-pane layout with sessions on the left and messages on the right.
func (p *Plugin) renderTwoPane() string {
	// Calculate pane widths - account for borders (2 per pane = 4 total) plus gap
	available := p.width - 5
	sidebarWidth := available * 30 / 100
	if sidebarWidth < 25 {
		sidebarWidth = 25
	}
	mainWidth := available - sidebarWidth
	if mainWidth < 40 {
		mainWidth = 40
	}

	// Store for use by content renderers
	p.sidebarWidth = sidebarWidth

	// Pane height: total height - 2 for pane borders
	paneHeight := p.height - 2
	if paneHeight < 4 {
		paneHeight = 4
	}

	// Inner content height = pane height - header lines (2)
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Determine border styles based on focus
	sidebarBorder := styles.PanelInactive
	mainBorder := styles.PanelInactive
	if p.activePane == PaneSidebar {
		sidebarBorder = styles.PanelActive
	} else {
		mainBorder = styles.PanelActive
	}

	// Render sidebar (session list)
	sidebarContent := p.renderSidebarPane(innerHeight)

	// Render main pane (messages)
	mainContent := p.renderMainPane(mainWidth, innerHeight)

	leftPane := sidebarBorder.
		Width(sidebarWidth).
		Height(paneHeight).
		Render(sidebarContent)

	rightPane := mainBorder.
		Width(mainWidth).
		Height(paneHeight).
		Render(mainContent)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)
}

// renderSidebarPane renders the session list for the sidebar.
func (p *Plugin) renderSidebarPane(height int) string {
	var sb strings.Builder

	sessions := p.visibleSessions()

	// Content width = sidebar width - padding (2 chars for Padding(0,1))
	contentWidth := p.sidebarWidth - 2
	if contentWidth < 15 {
		contentWidth = 15
	}

	// Header with count
	countStr := fmt.Sprintf("%d", len(p.sessions))
	if p.searchMode && p.searchQuery != "" {
		countStr = fmt.Sprintf("%d/%d", len(sessions), len(p.sessions))
	}
	header := fmt.Sprintf("Sessions %s", countStr)
	if len(header) > contentWidth {
		header = header[:contentWidth]
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
	end := p.scrollOff + contentHeight
	if end > len(sessions) {
		end = len(sessions)
	}

	for i := p.scrollOff; i < end; i++ {
		session := sessions[i]
		selected := i == p.cursor
		sb.WriteString(p.renderCompactSessionRow(session, selected, contentWidth))
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderCompactSessionRow renders a compact session row for the sidebar.
func (p *Plugin) renderCompactSessionRow(session adapter.Session, selected bool, maxWidth int) string {
	// Calculate prefix length for width calculations
	// cursor(2) + active(1) + selected(0-1)
	prefixLen := 3
	if session.ID == p.selectedSession {
		prefixLen++
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

	// Truncate name to fit
	if len(name) > nameWidth {
		name = name[:nameWidth-3] + "..."
	}

	// Build the row with styles
	var sb strings.Builder
	if selected {
		sb.WriteString(styles.ListCursor.Render("> "))
	} else {
		sb.WriteString("  ")
	}

	if session.IsActive {
		sb.WriteString(styles.StatusInProgress.Render("●"))
	} else {
		sb.WriteString(" ")
	}

	if session.ID == p.selectedSession {
		sb.WriteString(styles.StatusStaged.Render("*"))
	}

	sb.WriteString(name)

	result := sb.String()

	// Apply selection background if selected
	if selected {
		return styles.ListItemSelected.Render(result)
	}
	return result
}

// renderMainPane renders the message list for the main pane.
func (p *Plugin) renderMainPane(paneWidth, height int) string {
	var sb strings.Builder

	// Content width = pane width - padding (2 chars for Padding(0,1))
	contentWidth := paneWidth - 2
	if contentWidth < 20 {
		contentWidth = 20
	}

	if p.selectedSession == "" {
		sb.WriteString(styles.Muted.Render("Select a session to view messages"))
		return sb.String()
	}

	// Find session info
	var session *adapter.Session
	for i := range p.sessions {
		if p.sessions[i].ID == p.selectedSession {
			session = &p.sessions[i]
			break
		}
	}

	// Header
	sessionName := shortID(p.selectedSession)
	if session != nil && session.Name != "" {
		sessionName = session.Name
	}
	if len(sessionName) > contentWidth-5 {
		sessionName = sessionName[:contentWidth-8] + "..."
	}

	sb.WriteString(styles.Title.Render(sessionName))
	sb.WriteString("\n")

	// Stats line
	if p.sessionSummary != nil {
		s := p.sessionSummary
		modelShort := modelShortName(s.PrimaryModel)
		if modelShort == "" {
			modelShort = "claude"
		}
		statsLine := fmt.Sprintf("%s │ %d msgs │ %s→%s",
			modelShort,
			s.MessageCount,
			formatK(s.TotalTokensIn),
			formatK(s.TotalTokensOut))
		if len(statsLine) > contentWidth {
			statsLine = statsLine[:contentWidth-3] + "..."
		}
		sb.WriteString(styles.Muted.Render(statsLine))
		sb.WriteString("\n")
	}

	// Resume command
	if session != nil {
		resumeCmd := fmt.Sprintf("claude --resume %s", session.ID)
		if len(resumeCmd) > contentWidth {
			resumeCmd = resumeCmd[:contentWidth-3] + "..."
		}
		sb.WriteString(styles.Code.Render(resumeCmd))
		sb.WriteString("\n")
	}

	sepWidth := contentWidth
	if sepWidth > 60 {
		sepWidth = 60
	}
	sb.WriteString(styles.Muted.Render(strings.Repeat("─", sepWidth)))
	sb.WriteString("\n")

	// Messages
	if len(p.messages) == 0 {
		sb.WriteString(styles.Muted.Render("Loading messages..."))
		return sb.String()
	}

	contentHeight := height - 4 // Account for header lines
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Render messages
	lineCount := 0
	for i := p.msgScrollOff; i < len(p.messages) && lineCount < contentHeight; i++ {
		msg := p.messages[i]
		lines := p.renderCompactMessage(msg, i, contentWidth)
		for _, line := range lines {
			if lineCount >= contentHeight {
				break
			}
			sb.WriteString(line)
			sb.WriteString("\n")
			lineCount++
		}
	}

	return sb.String()
}

// renderCompactMessage renders a message in compact format for two-pane view.
func (p *Plugin) renderCompactMessage(msg adapter.Message, msgIndex int, maxWidth int) []string {
	var lines []string

	// Header line: [timestamp] role  tokens
	ts := msg.Timestamp.Local().Format("15:04")
	roleStyle := styles.Muted
	if msg.Role == "user" {
		roleStyle = styles.StatusInProgress
	} else {
		roleStyle = styles.StatusStaged
	}

	// Cursor indicator
	cursor := "  "
	if msgIndex == p.msgCursor {
		cursor = "> "
	}

	// Token info
	tokens := ""
	if msg.OutputTokens > 0 || msg.InputTokens > 0 {
		tokens = fmt.Sprintf(" (%s→%s)", formatK(msg.InputTokens), formatK(msg.OutputTokens))
	}

	// Build header without styles first to check length
	headerText := fmt.Sprintf("%s[%s] %s%s", cursor, ts, msg.Role, tokens)
	if len(headerText) > maxWidth {
		headerText = headerText[:maxWidth-3] + "..."
	}

	// Apply styles to truncated header
	styledHeader := cursor
	if msgIndex == p.msgCursor {
		styledHeader = styles.ListCursor.Render("> ")
	} else {
		styledHeader = "  "
	}
	styledHeader += fmt.Sprintf("[%s] %s%s",
		styles.Muted.Render(ts),
		roleStyle.Render(msg.Role),
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

	// Content preview (truncated)
	content := msg.Content
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.TrimSpace(content)
	contentMaxLen := maxWidth - 4 // Account for "   " prefix
	if contentMaxLen < 10 {
		contentMaxLen = 10
	}
	if len(content) > contentMaxLen {
		content = content[:contentMaxLen-3] + "..."
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

// renderFilterMenu renders the filter selection menu.
func (p *Plugin) renderFilterMenu(height int) string {
	var sb strings.Builder

	sb.WriteString(styles.Title.Render("Filters"))
	sb.WriteString("                    ")
	sb.WriteString(styles.Muted.Render("[esc to cancel]"))
	sb.WriteString("\n")
	sb.WriteString(styles.Muted.Render(strings.Repeat("─", p.sidebarWidth-4)))
	sb.WriteString("\n\n")

	// Model filters
	sb.WriteString(styles.Subtitle.Render("Model:"))
	sb.WriteString("\n")
	models := []struct {
		key   string
		name  string
		model string
	}{
		{"1", "Opus", "opus"},
		{"2", "Sonnet", "sonnet"},
		{"3", "Haiku", "haiku"},
	}
	for _, m := range models {
		checkbox := "[ ]"
		if p.filters.HasModel(m.model) {
			checkbox = "[✓]"
		}
		sb.WriteString(fmt.Sprintf("  %s %s %s\n", styles.Code.Render(m.key), checkbox, m.name))
	}
	sb.WriteString("\n")

	// Date filters
	sb.WriteString(styles.Subtitle.Render("Date:"))
	sb.WriteString("\n")
	dates := []struct {
		key    string
		name   string
		preset string
	}{
		{"t", "Today", "today"},
		{"y", "Yesterday", "yesterday"},
		{"w", "This Week", "week"},
	}
	for _, d := range dates {
		checkbox := "[ ]"
		if p.filters.DateRange.Preset == d.preset {
			checkbox = "[✓]"
		}
		sb.WriteString(fmt.Sprintf("  %s %s %s\n", styles.Code.Render(d.key), checkbox, d.name))
	}
	sb.WriteString("\n")

	// Active only
	activeCheck := "[ ]"
	if p.filters.ActiveOnly {
		activeCheck = "[✓]"
	}
	sb.WriteString(fmt.Sprintf("  %s %s Active only\n", styles.Code.Render("a"), activeCheck))
	sb.WriteString("\n")

	// Clear filters
	sb.WriteString(fmt.Sprintf("  %s Clear all filters\n", styles.Code.Render("c")))

	return sb.String()
}

// renderMessageDetail renders the full message detail view.
func (p *Plugin) renderMessageDetail() string {
	var sb strings.Builder

	if p.detailMessage == nil {
		return styles.Muted.Render("No message selected")
	}

	msg := p.detailMessage

	// Header
	sb.WriteString(styles.PanelHeader.Render(" Message Detail                               [esc to close]"))
	sb.WriteString("\n")
	sb.WriteString(styles.Muted.Render(strings.Repeat("━", p.width-2)))
	sb.WriteString("\n")

	// Message metadata
	ts := msg.Timestamp.Local().Format("2006-01-02 15:04:05")
	sb.WriteString(fmt.Sprintf(" Role: %s\n", styles.StatusInProgress.Render(msg.Role)))
	sb.WriteString(fmt.Sprintf(" Time: %s\n", ts))
	if msg.Model != "" {
		sb.WriteString(fmt.Sprintf(" Model: %s\n", styles.Code.Render(modelShortName(msg.Model))))
	}
	if msg.InputTokens > 0 || msg.OutputTokens > 0 {
		sb.WriteString(fmt.Sprintf(" Tokens: in=%d, out=%d", msg.InputTokens, msg.OutputTokens))
		if msg.CacheRead > 0 {
			sb.WriteString(fmt.Sprintf(", cache=%d", msg.CacheRead))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(styles.Muted.Render(strings.Repeat("─", p.width-2)))
	sb.WriteString("\n\n")

	// Build content lines
	var contentLines []string

	// Thinking blocks
	for i, tb := range msg.ThinkingBlocks {
		contentLines = append(contentLines, styles.PanelHeader.Render(fmt.Sprintf(" Thinking Block %d (%d tokens)", i+1, tb.TokenCount)))
		contentLines = append(contentLines, styles.Muted.Render(strings.Repeat("─", p.width-4)))
		// Wrap thinking content
		thinkingLines := wrapText(tb.Content, p.width-4)
		for _, line := range thinkingLines {
			contentLines = append(contentLines, " "+styles.Muted.Render(line))
		}
		contentLines = append(contentLines, "")
	}

	// Main content
	if msg.Content != "" {
		contentLines = append(contentLines, styles.PanelHeader.Render(" Content"))
		contentLines = append(contentLines, styles.Muted.Render(strings.Repeat("─", p.width-4)))
		// Wrap content
		msgLines := wrapText(msg.Content, p.width-4)
		for _, line := range msgLines {
			contentLines = append(contentLines, " "+styles.Body.Render(line))
		}
		contentLines = append(contentLines, "")
	}

	// Tool uses
	if len(msg.ToolUses) > 0 {
		contentLines = append(contentLines, styles.PanelHeader.Render(" Tool Uses"))
		contentLines = append(contentLines, styles.Muted.Render(strings.Repeat("─", p.width-4)))
		for _, tu := range msg.ToolUses {
			contentLines = append(contentLines, " "+styles.Code.Render(tu.Name))
			if filePath := extractFilePath(tu.Input); filePath != "" {
				contentLines = append(contentLines, "   Path: "+filePath)
			}
			// Show input preview (truncated)
			if tu.Input != "" && len(tu.Input) < 200 {
				contentLines = append(contentLines, "   Input: "+tu.Input)
			}
			contentLines = append(contentLines, "")
		}
	}

	// Apply scroll offset
	headerLines := 8 // Lines used by header
	contentHeight := p.height - headerLines
	if contentHeight < 1 {
		contentHeight = 1
	}

	start := p.detailScroll
	if start >= len(contentLines) {
		start = len(contentLines) - 1
		if start < 0 {
			start = 0
		}
	}
	end := start + contentHeight
	if end > len(contentLines) {
		end = len(contentLines)
	}

	for i := start; i < end; i++ {
		sb.WriteString(contentLines[i])
		sb.WriteString("\n")
	}

	return sb.String()
}
