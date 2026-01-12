package conversations

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/styles"
)

// ansiBackgroundRegex matches ANSI background color escape sequences
var ansiBackgroundRegex = regexp.MustCompile(`\x1b\[4[0-9;]*m`)

// stripANSIBackground removes ANSI background color codes from a string
func stripANSIBackground(s string) string {
	return ansiBackgroundRegex.ReplaceAllString(s, "")
}

// renderNoAdapter renders the view when no adapter is available.
func renderNoAdapter() string {
	return styles.Muted.Render(" No AI sessions available")
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

func adapterBreakdown(sessions []adapter.Session) string {
	counts := make(map[string]int)
	for _, session := range sessions {
		icon := session.AdapterIcon
		if icon == "" {
			icon = adapterAbbrev(session)
		}
		if icon == "" {
			continue
		}
		counts[icon]++
	}
	if len(counts) <= 1 {
		return ""
	}
	var keys []string
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%d %s", counts[key], key))
	}
	return strings.Join(parts, ", ")
}

func adapterBadgeText(session adapter.Session) string {
	if session.AdapterIcon != "" {
		return session.AdapterIcon
	}
	// Fallback for sessions without icon
	abbr := adapterAbbrev(session)
	if abbr == "" {
		return "?" // Unknown adapter fallback
	}
	return "●" + abbr
}

func adapterAbbrev(session adapter.Session) string {
	switch session.AdapterID {
	case "claude-code":
		return "CC"
	case "codex":
		return "CX"
	case "opencode":
		return "OC"
	case "gemini-cli":
		return "GC"
	default:
		name := session.AdapterName
		if name == "" {
			name = session.AdapterID
		}
		name = strings.ReplaceAll(name, " ", "")
		if name == "" {
			return ""
		}
		if len(name) <= 2 {
			return strings.ToUpper(name)
		}
		return strings.ToUpper(name[:2])
	}
}

func adapterShortName(session *adapter.Session) string {
	if session == nil {
		return ""
	}
	switch session.AdapterID {
	case "claude-code":
		return "claude"
	case "codex":
		return "codex"
	case "opencode":
		return "opencode"
	case "gemini-cli":
		return "gemini"
	default:
		if session.AdapterName != "" {
			return strings.ToLower(session.AdapterName)
		}
		return session.AdapterID
	}
}

type adapterFilterOption struct {
	key  string
	id   string
	name string
}

func adapterFilterOptions(adapters map[string]adapter.Adapter) []adapterFilterOption {
	if len(adapters) == 0 {
		return nil
	}

	reservedKeys := map[string]bool{
		"1": true,
		"2": true,
		"3": true,
		"t": true,
		"y": true,
		"w": true,
		"a": true,
		"x": true,
	}

	usedKeys := make(map[string]bool)
	var options []adapterFilterOption

	addOption := func(id string, name string, key string) {
		if key == "" || usedKeys[key] || reservedKeys[key] {
			return
		}
		usedKeys[key] = true
		options = append(options, adapterFilterOption{key: key, id: id, name: name})
	}

	if a, ok := adapters["claude-code"]; ok {
		addOption("claude-code", a.Name(), "c")
	}
	if a, ok := adapters["codex"]; ok {
		addOption("codex", a.Name(), "o")
	}
	if a, ok := adapters["opencode"]; ok {
		addOption("opencode", a.Name(), "p")
	}
	if a, ok := adapters["gemini-cli"]; ok {
		addOption("gemini-cli", a.Name(), "g")
	}

	var extra []adapterFilterOption
	for id, a := range adapters {
		if id == "claude-code" || id == "codex" || id == "opencode" || id == "gemini-cli" {
			continue
		}
		name := a.Name()
		if name == "" {
			name = id
		}
		key := ""
		for _, r := range strings.ToLower(name) {
			candidate := string(r)
			if usedKeys[candidate] || reservedKeys[candidate] {
				continue
			}
			key = candidate
			break
		}
		if key != "" {
			usedKeys[key] = true
			extra = append(extra, adapterFilterOption{key: key, id: id, name: name})
		}
	}

	sort.Slice(extra, func(i, j int) bool {
		return extra[i].name < extra[j].name
	})

	options = append(options, extra...)
	return options
}

func resumeCommand(session *adapter.Session) string {
	if session == nil || session.ID == "" {
		return ""
	}
	switch session.AdapterID {
	case "claude-code":
		return fmt.Sprintf("claude --resume %s", session.ID)
	case "codex":
		return fmt.Sprintf("codex resume %s", session.ID)
	case "opencode":
		return fmt.Sprintf("opencode --continue -s %s", session.ID)
	case "gemini-cli":
		return fmt.Sprintf("gemini --resume %s", session.ID)
	case "cursor-cli":
		return fmt.Sprintf("cursor-agent --resume %s", session.ID)
	default:
		return ""
	}
}

// modelShortName maps model IDs to short display names.
func modelShortName(model string) string {
	model = strings.ToLower(model)
	switch {
	// Claude models (cursor uses "claude-4.5-opus-high-thinking" etc.)
	case strings.Contains(model, "opus"):
		return "opus"
	case strings.Contains(model, "sonnet-4") || strings.Contains(model, "sonnet4"):
		return "sonnet4"
	case strings.Contains(model, "sonnet"):
		return "sonnet"
	case strings.Contains(model, "haiku"):
		return "haiku"
	// GPT models
	case strings.HasPrefix(model, "gpt-"):
		parts := strings.Split(model, "-")
		if len(parts) > 1 {
			return "gpt" + parts[1]
		}
		return "gpt"
	case strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3"):
		parts := strings.Split(model, "-")
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
		return "o"
	// Gemini models
	case strings.Contains(model, "gemini-3-pro") || strings.Contains(model, "gemini3-pro"):
		return "3Pro"
	case strings.Contains(model, "gemini-3-flash") || strings.Contains(model, "gemini3-flash"):
		return "3Flash"
	case strings.Contains(model, "gemini-3") || strings.Contains(model, "gemini3"):
		return "gemini3"
	case strings.Contains(model, "gemini-2.0-flash"):
		return "2Flash"
	case strings.Contains(model, "gemini-1.5-pro"):
		return "1.5Pro"
	case strings.Contains(model, "gemini-1.5-flash"):
		return "1.5Flash"
	case strings.HasPrefix(model, "gemini"):
		return "gemini"
	// Other models
	case strings.HasPrefix(model, "grok"):
		return "grok"
	case strings.HasPrefix(model, "deepseek"):
		return "deepseek"
	case strings.HasPrefix(model, "mistral"):
		return "mistral"
	case strings.HasPrefix(model, "llama"):
		return "llama"
	case strings.HasPrefix(model, "qwen"):
		return "qwen"
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
	model = strings.ToLower(model)
	switch {
	case strings.Contains(model, "opus"):
		inRate, outRate = 15.0, 75.0
	case strings.Contains(model, "sonnet"):
		inRate, outRate = 3.0, 15.0
	case strings.Contains(model, "haiku"):
		inRate, outRate = 0.25, 1.25
	case strings.Contains(model, "gpt-4o"):
		inRate, outRate = 2.5, 10.0
	case strings.Contains(model, "gpt-4"):
		inRate, outRate = 10.0, 30.0
	case strings.Contains(model, "o1") || strings.Contains(model, "o3"):
		inRate, outRate = 15.0, 60.0
	case strings.Contains(model, "gemini"):
		inRate, outRate = 1.25, 5.0
	case strings.Contains(model, "deepseek"):
		inRate, outRate = 0.14, 0.28
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

// prettifyJSON attempts to format JSON output with indentation.
// Returns the original string if it's not valid JSON.
func prettifyJSON(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return s
	}

	// Check if it looks like JSON (starts with { or [)
	if s[0] != '{' && s[0] != '[' {
		return s
	}

	var data interface{}
	if err := json.Unmarshal([]byte(s), &data); err != nil {
		return s
	}

	pretty, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return s
	}

	return string(pretty)
}

// extractToolCommand extracts a short command preview from tool input.
// Returns a truncated command string for display in tool headers.
func extractToolCommand(toolName, input string, maxLen int) string {
	if input == "" {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(input), &data); err != nil {
		return ""
	}

	var cmd string
	switch toolName {
	case "Bash", "bash":
		if c, ok := data["command"].(string); ok {
			cmd = c
		}
	case "Read", "read":
		if fp, ok := data["file_path"].(string); ok {
			return fp // Already shown via extractFilePath
		}
	case "Edit", "edit":
		if fp, ok := data["file_path"].(string); ok {
			return fp
		}
	case "Write", "write":
		if fp, ok := data["file_path"].(string); ok {
			return fp
		}
	case "Glob", "glob":
		if p, ok := data["pattern"].(string); ok {
			cmd = p
		}
	case "Grep", "grep":
		if p, ok := data["pattern"].(string); ok {
			cmd = p
		}
	}

	if cmd == "" {
		return ""
	}

	// Clean up command: remove newlines, collapse whitespace
	cmd = strings.ReplaceAll(cmd, "\n", " ")
	cmd = strings.Join(strings.Fields(cmd), " ")

	if len(cmd) > maxLen {
		cmd = cmd[:maxLen-3] + "..."
	}
	return cmd
}

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

	// Calculate pane widths - account for borders (2 per pane = 4 total) plus gap and divider
	available := p.width - 5 - dividerWidth
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
	if !p.searchMode {
		groups := GroupSessionsByTime(sessions)
		p.renderGroupedCompactSessions(&sb, groups, contentHeight, contentWidth)
	} else {
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
	}

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
func (p *Plugin) renderCompactSessionRow(session adapter.Session, selected bool, maxWidth int) string {
	// Calculate prefix length for width calculations
	// active(1) + subagent indent(2) + badge + space + length(4) + space
	badgeText := adapterBadgeText(session)
	length := "--"
	if session.Duration > 0 {
		length = formatSessionDuration(session.Duration)
	}
	lengthCol := fmt.Sprintf("%4s", length)

	prefixLen := 1 + len(badgeText) + 1 + len(lengthCol) + 1
	if session.IsSubAgent {
		prefixLen += 2 // extra indent for sub-agents
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

	// Calculate padding for right-aligned time
	visibleLen := 0
	if session.IsSubAgent {
		visibleLen += 2
	}
	visibleLen += 1                              // indicator
	visibleLen += len(badgeText) + 1 + len(name) // badge + space + name
	padding := maxWidth - visibleLen - len(lengthCol) - 1
	if padding < 0 {
		padding = 0
	}

	// Build the row with styling - differentiate top-level vs sub-agents
	var sb strings.Builder

	// Sub-agent indent
	if session.IsSubAgent {
		sb.WriteString("  ")
	}

	// Type indicator with colors
	if session.IsActive {
		sb.WriteString(styles.StatusInProgress.Render("●"))
	} else if session.IsSubAgent {
		sb.WriteString(styles.Muted.Render("↳"))
	} else {
		sb.WriteString(" ")
	}

	// Style based on session type
	if session.IsSubAgent {
		// Sub-agents: muted styling
		sb.WriteString(styles.Muted.Render(badgeText))
		sb.WriteString(" ")
		sb.WriteString(styles.Subtitle.Render(name))
	} else {
		// Top-level: prominent amber icons, bright text
		sb.WriteString(styles.StatusModified.Render(badgeText))
		sb.WriteString(" ")
		sb.WriteString(styles.Body.Render(name))
	}

	// Padding and time
	if padding > 0 {
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString(" ")
		if session.IsSubAgent {
			sb.WriteString(styles.Muted.Render(lengthCol))
		} else {
			sb.WriteString(styles.Subtitle.Render(lengthCol))
		}
	}

	row := sb.String()

	// For selected rows, we need plain text with background
	if selected {
		// Build plain version for selection
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
		plain.WriteString(name)
		if padding > 0 {
			plain.WriteString(strings.Repeat(" ", padding))
			plain.WriteString(" ")
			plain.WriteString(lengthCol)
		}
		plainRow := plain.String()
		// Pad to full width
		if len(plainRow) < maxWidth {
			plainRow += strings.Repeat(" ", maxWidth-len(plainRow))
		}
		return styles.ListItemSelected.Render(plainRow)
	}

	return row
}

// renderMainPane renders the message list for the main pane.
func (p *Plugin) renderMainPane(paneWidth, height int) string {
	// Content width = pane width - padding (2 chars for Padding(0,1))
	contentWidth := paneWidth - 2
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

	// Header: AdapterName - SessionName (with different colors)
	sessionName := shortID(p.selectedSession)
	if session != nil && session.Name != "" {
		sessionName = session.Name
	}
	adapterName := ""
	if session != nil && session.AdapterName != "" {
		adapterName = session.AdapterName
	}

	// Calculate max length for session name
	prefixLen := 0
	if adapterName != "" {
		prefixLen = len(adapterName) + 3 // " - "
	}
	maxSessionLen := contentWidth - prefixLen - 2
	if maxSessionLen < 10 {
		maxSessionLen = 10
	}
	if len(sessionName) > maxSessionLen {
		sessionName = sessionName[:maxSessionLen-3] + "..."
	}

	if adapterName != "" {
		sb.WriteString(styles.Muted.Render(adapterName + " - "))
	}
	sb.WriteString(styles.Title.Render(sessionName))
	sb.WriteString("\n")

	// Stats line
	if p.sessionSummary != nil {
		s := p.sessionSummary
		modelShort := modelShortName(s.PrimaryModel)
		if modelShort == "" {
			modelShort = adapterShortName(session)
		}
		statsLine := fmt.Sprintf("%s │ %d msgs │ %s→%s",
			modelShort,
			s.MessageCount,
			formatK(s.TotalTokensIn),
			formatK(s.TotalTokensOut))
		if session != nil && !session.UpdatedAt.IsZero() {
			statsLine += fmt.Sprintf(" │ updated %s", session.UpdatedAt.Local().Format("01-02 15:04"))
		}
		if len(statsLine) > contentWidth {
			statsLine = statsLine[:contentWidth-3] + "..."
		}
		sb.WriteString(styles.Muted.Render(statsLine))
		sb.WriteString("\n")
	}

	// Resume command
	if session != nil {
		resumeCmd := resumeCommand(session)
		if resumeCmd != "" {
			if len(resumeCmd) > contentWidth {
				resumeCmd = resumeCmd[:contentWidth-3] + "..."
			}
			sb.WriteString(styles.Code.Render(resumeCmd))
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

	return sb.String()
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

	// Calculate if we need to truncate role
	role := msg.Role
	// Account for: cursor(2) + [](2) + ts(5) + space(1) + role + tokens
	usedWidth := 2 + 2 + len(ts) + 1 + len(role) + len(tokens)
	if usedWidth > maxWidth && len(role) > 4 {
		role = role[:4]
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

// renderConversationFlow renders messages as a scrollable chat thread (Claude Code web UI style).
func (p *Plugin) renderConversationFlow(contentWidth, height int) []string {
	// Clear previous tracking data
	p.visibleMsgRanges = p.visibleMsgRanges[:0]
	p.msgLinePositions = p.msgLinePositions[:0]

	if len(p.messages) == 0 {
		return []string{styles.Muted.Render("No messages")}
	}

	var allLines []string

	for msgIdx, msg := range p.messages {
		// Skip user messages that are just tool results (they'll be shown inline with tool_use)
		if p.isToolResultOnlyMessage(msg) {
			continue
		}

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

// renderMessageBubble renders a single message as a chat bubble with content blocks.
func (p *Plugin) renderMessageBubble(msg adapter.Message, msgIndex int, maxWidth int) []string {
	var lines []string
	selected := msgIndex == p.messageCursor

	// Header: timestamp + role
	ts := msg.Timestamp.Local().Format("15:04")
	var roleStyle lipgloss.Style
	roleLabel := msg.Role
	if msg.Role == "user" {
		roleStyle = styles.StatusInProgress
	} else {
		roleStyle = styles.StatusStaged
		roleLabel = "assistant"
	}

	// Cursor indicator for selected message
	cursorPrefix := "  "
	if selected {
		cursorPrefix = "> "
	}

	headerLine := fmt.Sprintf("%s[%s] %s", cursorPrefix, ts, roleStyle.Render(roleLabel))
	if msg.Model != "" {
		shortModel := modelShortName(msg.Model)
		if shortModel != "" {
			headerLine += styles.Muted.Render(" (" + shortModel + ")")
		}
	}
	lines = append(lines, headerLine)

	// Render content blocks if available, otherwise fall back to Content string
	if len(msg.ContentBlocks) > 0 {
		blockLines := p.renderContentBlocks(msg, maxWidth-4) // Indent content under header
		for _, line := range blockLines {
			lines = append(lines, "    "+line) // Indent
		}
	} else if msg.Content != "" {
		// Fallback: render plain content
		contentLines := p.renderMessageContent(msg.Content, msg.ID, maxWidth-4)
		for _, line := range contentLines {
			lines = append(lines, "    "+line)
		}
	}

	// Apply selection highlighting if needed
	if selected {
		var styledLines []string
		for _, line := range lines {
			// Strip any existing background colors so selection bg shows through
			line = stripANSIBackground(line)
			// Use visible width (not byte length) for proper padding
			visibleWidth := lipgloss.Width(line)
			if visibleWidth < maxWidth {
				line += strings.Repeat(" ", maxWidth-visibleWidth)
			}
			styledLines = append(styledLines, styles.ListItemSelected.Render(line))
		}
		return styledLines
	}

	return lines
}

// renderContentBlocks renders the structured content blocks for a message.
func (p *Plugin) renderContentBlocks(msg adapter.Message, maxWidth int) []string {
	var lines []string

	for _, block := range msg.ContentBlocks {
		switch block.Type {
		case "text":
			textLines := p.renderMessageContent(block.Text, msg.ID, maxWidth)
			lines = append(lines, textLines...)

		case "thinking":
			thinkingLines := p.renderThinkingBlock(block, msg.ID, maxWidth)
			lines = append(lines, thinkingLines...)

		case "tool_use":
			toolLines := p.renderToolUseBlock(block, maxWidth)
			lines = append(lines, toolLines...)

		case "tool_result":
			// Tool results are rendered inline with tool_use via ToolOutput
			// Skip standalone tool_result blocks in the flow
			continue
		}
	}

	return lines
}

// renderMessageContent renders text content with expand/collapse for long messages.
// Uses render cache (td-8910b218) to avoid re-rendering unchanged content.
func (p *Plugin) renderMessageContent(content string, msgID string, maxWidth int) []string {
	if content == "" {
		return nil
	}

	// Check if content is "short" (can display inline)
	lineCount := strings.Count(content, "\n") + 1
	isShort := len(content) <= ShortMessageCharLimit && lineCount <= ShortMessageLineLimit

	expanded := p.expandedMessages[msgID]

	// Check cache (td-8910b218)
	if cached, ok := p.getCachedRender(msgID, maxWidth, expanded); ok {
		return strings.Split(cached, "\n")
	}

	var result []string
	if isShort || expanded {
		// Show full content
		result = p.renderContent(content, maxWidth)
	} else {
		// Collapsed: show preview with toggle hint
		preview := content
		if len(preview) > CollapsedPreviewChars {
			preview = preview[:CollapsedPreviewChars]
		}
		// Clean up preview (no partial lines)
		preview = strings.ReplaceAll(preview, "\n", " ")
		preview = strings.TrimSpace(preview)
		if len(preview) < len(content) {
			preview += "..."
		}
		result = wrapText(preview, maxWidth)
	}

	// Store in cache (td-8910b218)
	p.setCachedRender(msgID, maxWidth, expanded, strings.Join(result, "\n"))
	return result
}

// renderThinkingBlock renders a thinking block (collapsed by default).
// Uses render cache (td-8910b218) to avoid re-rendering unchanged content.
func (p *Plugin) renderThinkingBlock(block adapter.ContentBlock, msgID string, maxWidth int) []string {
	expanded := p.expandedThinking[msgID]

	// Use cache key with "thinking_" prefix to distinguish from content cache
	thinkingCacheID := "thinking_" + msgID
	if cached, ok := p.getCachedRender(thinkingCacheID, maxWidth, expanded); ok {
		return strings.Split(cached, "\n")
	}

	var lines []string

	// Header with token count
	headerText := fmt.Sprintf("[thinking] %s tokens", formatK(block.TokenCount))
	lines = append(lines, styles.Code.Render(headerText))

	if expanded {
		// Show thinking content
		thinkingLines := wrapText(block.Text, maxWidth-2)
		for _, line := range thinkingLines {
			lines = append(lines, styles.Muted.Render("  "+line))
		}
	}

	// Store in cache (td-8910b218)
	p.setCachedRender(thinkingCacheID, maxWidth, expanded, strings.Join(lines, "\n"))
	return lines
}

// renderToolUseBlock renders a tool use block with its result (expand/collapse).
func (p *Plugin) renderToolUseBlock(block adapter.ContentBlock, maxWidth int) []string {
	var lines []string

	// Tool header: name and command/file path if available
	toolHeader := "⚙ " + block.ToolName

	// Try to extract a meaningful command preview
	cmdPreview := extractToolCommand(block.ToolName, block.ToolInput, maxWidth-len(toolHeader)-5)
	if cmdPreview == "" {
		// Fall back to file_path extraction
		if filePath := extractFilePath(block.ToolInput); filePath != "" {
			cmdPreview = filePath
		}
	}
	if cmdPreview != "" {
		toolHeader += ": " + cmdPreview
	}

	if len(toolHeader) > maxWidth-2 {
		toolHeader = toolHeader[:maxWidth-5] + "..."
	}

	expanded := p.expandedToolResults[block.ToolUseID]

	// Error indicator
	if block.IsError {
		toolHeader = styles.StatusUntracked.Render(toolHeader + " [error]")
	} else {
		toolHeader = styles.Code.Render(toolHeader)
	}

	lines = append(lines, toolHeader)

	// Show result if expanded or if there's an error
	if block.ToolOutput != "" && (expanded || block.IsError) {
		output := block.ToolOutput

		// Truncate before prettifying to prevent memory issues with large outputs
		const maxChars = 10000
		if len(output) > maxChars {
			output = output[:maxChars]
		}

		// Try to prettify JSON output
		output = prettifyJSON(output)

		maxOutputLines := 20
		outputLines := strings.Split(output, "\n")
		if len(outputLines) > maxOutputLines {
			outputLines = outputLines[:maxOutputLines]
			outputLines = append(outputLines, fmt.Sprintf("... (%d more lines)", len(strings.Split(output, "\n"))-maxOutputLines))
		}
		for _, line := range outputLines {
			if len(line) > maxWidth-4 {
				line = line[:maxWidth-7] + "..."
			}
			lines = append(lines, styles.Muted.Render("  "+line))
		}
	} else if block.ToolOutput != "" {
		// Collapsed: show first meaningful line of output as preview
		// Skip lines that are just JSON structural chars (not informative)
		outputLines := strings.Split(block.ToolOutput, "\n")
		preview := ""
		for _, line := range outputLines {
			trimmed := strings.TrimSpace(line)
			// Skip empty lines and single-char JSON structure
			if trimmed == "" || trimmed == "{" || trimmed == "[" || trimmed == "}" || trimmed == "]" {
				continue
			}
			preview = trimmed
			break
		}
		if preview != "" {
			// Show first meaningful line as preview
			if len(preview) > maxWidth-6 {
				preview = preview[:maxWidth-9] + "..."
			}
			lines = append(lines, styles.Muted.Render("  → "+preview))
		}
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

	// Adapter filters
	adapterOptions := adapterFilterOptions(p.adapters)
	if len(adapterOptions) > 0 {
		sb.WriteString(styles.Subtitle.Render("Adapter:"))
		sb.WriteString("\n")
		for _, opt := range adapterOptions {
			checkbox := "[ ]"
			if p.filters.HasAdapter(opt.id) {
				checkbox = "[✓]"
			}
			sb.WriteString(fmt.Sprintf("  %s %s %s\n", styles.Code.Render(opt.key), checkbox, opt.name))
		}
		sb.WriteString("\n")
	}

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
	sb.WriteString(fmt.Sprintf("  %s Clear all filters\n", styles.Code.Render("x")))

	return sb.String()
}
