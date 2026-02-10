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
	"github.com/marcus/sidecar/internal/ui"
)

// Formatting utilities and content rendering functions

// ansiBackgroundRegex matches ANSI background color escape sequences including:
// - Basic: \x1b[40m through \x1b[49m
// - 256-color: \x1b[48;5;XXXm
// - True color: \x1b[48;2;R;G;Bm
var ansiBackgroundRegex = regexp.MustCompile(`\x1b\[(4[0-9]|48;[0-9;]+)m`)

// stripANSIBackground removes ANSI background color codes from a string
// to allow selection highlighting to show through consistently.
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

// formatCost formats a cost estimate in dollars.
func formatCost(cost float64) string {
	if cost < 0.01 {
		return "<$0.01"
	}
	if cost < 1.0 {
		return fmt.Sprintf("$%.2f", cost)
	}
	return fmt.Sprintf("$%.1f", cost)
}

// renderCategoryBadge returns a dim category badge for non-interactive sessions.
// Interactive sessions return empty string (clean default).
func renderCategoryBadge(session adapter.Session) string {
	switch session.SessionCategory {
	case adapter.SessionCategoryCron:
		return lipgloss.NewStyle().Foreground(styles.TextSubtle).Render("cron")
	case adapter.SessionCategorySystem:
		return lipgloss.NewStyle().Foreground(styles.TextSubtle).Render("sys")
	default:
		return ""
	}
}

// categoryBadgeText returns the plain text for a category badge (for width calculations).
func categoryBadgeText(session adapter.Session) string {
	switch session.SessionCategory {
	case adapter.SessionCategoryCron:
		return "cron"
	case adapter.SessionCategorySystem:
		return "sys"
	default:
		return ""
	}
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

// renderAdapterIcon returns a colorized adapter icon based on the adapter type.
func renderAdapterIcon(session adapter.Session) string {
	icon := session.AdapterIcon
	if icon == "" {
		icon = "◆"
	}

	// Color based on adapter
	switch session.AdapterID {
	case "claude-code":
		// Amber for Claude Code (matches existing StatusModified)
		return styles.StatusModified.Render(icon)
	case "gemini-cli":
		// Google blue
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#4285F4")).Render(icon)
	case "codex":
		// OpenAI green
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#10A37F")).Render(icon)
	case "cursor-cli":
		// Cursor purple
		return lipgloss.NewStyle().Foreground(styles.Primary).Render(icon)
	case "amp":
		// Sourcegraph orange
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5543")).Render(icon)
	default:
		return styles.Muted.Render(icon)
	}
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
	case "amp":
		return "AM"
	default:
		name := session.AdapterName
		if name == "" {
			name = session.AdapterID
		}
		name = strings.ReplaceAll(name, " ", "")
		if name == "" {
			return ""
		}
		if runes := []rune(name); len(runes) <= 2 {
			return strings.ToUpper(name)
		} else {
			return strings.ToUpper(string(runes[:2]))
		}
	}
}

func adapterShortName(session *adapter.Session) string {
	if session == nil {
		return "assistant"
	}
	switch session.AdapterID {
	case "claude-code":
		return "claude"
	case "cursor-cli":
		return "cursor"
	case "codex":
		return "codex"
	case "opencode":
		return "opencode"
	case "gemini-cli":
		return "gemini"
	case "warp":
		return "warp"
	case "amp":
		return "amp"
	default:
		if session.AdapterName != "" {
			return strings.ToLower(session.AdapterName)
		}
		if session.AdapterID != "" {
			return session.AdapterID
		}
		return "assistant"
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
		"i": true, // category: interactive
		"r": true, // category: cron
		"s": true, // category: system
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
	case "amp":
		return fmt.Sprintf("amp --resume %s", session.ID)
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

// renderModelBadge returns a colorful styled badge for the model name.
// opus=purple, sonnet=green, haiku=blue, others=default code style.
func renderModelBadge(model string) string {
	short := modelShortName(model)
	if short == "" {
		return ""
	}

	var badgeStyle lipgloss.Style
	switch short {
	case "opus":
		// Purple/magenta for opus (premium model)
		badgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C084FC")).
			Background(lipgloss.Color("#3B1F5B")).
			Padding(0, 1)
	case "sonnet", "sonnet4":
		// Green for sonnet (balanced)
		badgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#86EFAC")).
			Background(lipgloss.Color("#14532D")).
			Padding(0, 1)
	case "haiku":
		// Blue for haiku (fast/cheap)
		badgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#93C5FD")).
			Background(lipgloss.Color("#1E3A5F")).
			Padding(0, 1)
	case "gpt4", "gpt4o":
		// Teal for GPT-4
		badgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5EEAD4")).
			Background(lipgloss.Color("#134E4A")).
			Padding(0, 1)
	case "o1", "o3":
		// Orange for reasoning models
		badgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FDBA74")).
			Background(lipgloss.Color("#7C2D12")).
			Padding(0, 1)
	case "gemini", "gemini3", "3Pro", "3Flash", "2Flash", "1.5Pro", "1.5Flash":
		// Google blue for Gemini
		badgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#93C5FD")).
			Background(lipgloss.Color("#1E3A8A")).
			Padding(0, 1)
	default:
		// Default: amber/code style
		badgeStyle = lipgloss.NewStyle().
			Foreground(styles.Accent).
			Padding(0, 1)
	}

	return badgeStyle.Render(short)
}

// renderTokenFlow returns a compact token flow indicator (in:X out:Y).
func renderTokenFlow(in, out int) string {
	if in == 0 && out == 0 {
		return ""
	}
	return styles.Muted.Render(fmt.Sprintf("in:%s out:%s", formatK(in), formatK(out)))
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

	// First try to parse as object
	var data map[string]any
	if err := json.Unmarshal([]byte(input), &data); err == nil {
		var cmd string
		switch toolName {
		case "Bash", "bash", "Shell", "shell":
			if c, ok := data["command"].(string); ok {
				cmd = c
			}
		case "Read", "read":
			if fp, ok := data["file_path"].(string); ok {
				return fp
			}
			if fp, ok := data["path"].(string); ok {
				return fp
			}
		case "Edit", "edit", "StrReplace", "str_replace_editor":
			if fp, ok := data["file_path"].(string); ok {
				return fp
			}
			if fp, ok := data["path"].(string); ok {
				return fp
			}
		case "Write", "write":
			if fp, ok := data["file_path"].(string); ok {
				return fp
			}
			if fp, ok := data["path"].(string); ok {
				return fp
			}
		case "Glob", "glob":
			if p, ok := data["pattern"].(string); ok {
				cmd = p
			}
			if p, ok := data["glob_pattern"].(string); ok {
				cmd = p
			}
		case "Grep", "grep":
			if p, ok := data["pattern"].(string); ok {
				cmd = p
			}
		case "Task", "task", "TodoWrite", "TodoRead":
			// Task tools often have text content
			if t, ok := data["text"].(string); ok {
				cmd = t
			}
			if t, ok := data["content"].(string); ok {
				cmd = t
			}
			if t, ok := data["description"].(string); ok {
				cmd = t
			}
		default:
			// Fallback: try common text fields
			if t, ok := data["text"].(string); ok {
				cmd = t
			} else if t, ok := data["content"].(string); ok {
				cmd = t
			} else if t, ok := data["message"].(string); ok {
				cmd = t
			} else if t, ok := data["query"].(string); ok {
				cmd = t
			}
		}

		if cmd != "" {
			// Clean up: remove newlines, collapse whitespace
			cmd = strings.ReplaceAll(cmd, "\n", " ")
			cmd = strings.Join(strings.Fields(cmd), " ")
			if len(cmd) > maxLen {
				cmd = cmd[:maxLen-3] + "..."
			}
			return cmd
		}
	}

	// Try to parse as array (e.g., [{"text": "..."}])
	var arr []map[string]any
	if err := json.Unmarshal([]byte(input), &arr); err == nil && len(arr) > 0 {
		// Extract text from first element
		if t, ok := arr[0]["text"].(string); ok {
			cmd := strings.ReplaceAll(t, "\n", " ")
			cmd = strings.Join(strings.Fields(cmd), " ")
			if len(cmd) > maxLen {
				cmd = cmd[:maxLen-3] + "..."
			}
			return cmd
		}
	}

	return ""
}

// renderSourceLabel renders a source label with the channel badge dim and the name styled.
// e.g. "[TG] Marcus Vorwaller" -> dim("[TG]") + styled("Marcus Vorwaller")
func renderSourceLabel(label string) string {
	// Split into badge part and name part
	// Labels are like "[TG] Marcus Vorwaller", "[WA]", "[cron] job-name", "[sys]"
	if idx := strings.Index(label, "] "); idx != -1 {
		badge := label[:idx+1]  // "[TG]"
		name := label[idx+2:]   // "Marcus Vorwaller"
		return styles.Muted.Render(badge) + " " + styles.StatusInProgress.Render(name)
	}
	// No name part, just the badge (e.g. "[WA]", "[sys]")
	return styles.Muted.Render(label)
}

// renderMessageBubble renders a single message as a chat bubble with content blocks.
func (p *Plugin) renderMessageBubble(msg adapter.Message, msgIndex int, maxWidth int) []string {
	var lines []string
	selected := msgIndex == p.messageCursor

	// Header: timestamp + role + model badge + token flow
	ts := msg.Timestamp.Local().Format("15:04")

	// Get agent name from session's adapter
	session := p.findSelectedSession()
	agentName := adapterShortName(session)

	// Cursor indicator for selected message
	cursorPrefix := "  "
	if selected {
		cursorPrefix = "> "
	}

	var headerLine string
	if selected {
		// For selected messages, use plain text (no colored backgrounds) for consistent highlighting
		if msg.Role == "user" {
			userLabel := "you"
			if msg.SourceLabel != "" {
				userLabel = msg.SourceLabel
			}
			headerLine = fmt.Sprintf("%s[%s] %s", cursorPrefix, ts, userLabel)
		} else {
			headerLine = fmt.Sprintf("%s[%s] %s", cursorPrefix, ts, agentName)
			// Add plain model name
			if msg.Model != "" {
				short := modelShortName(msg.Model)
				if short != "" {
					headerLine += " " + short
				}
			}
			// Add plain token flow
			if msg.InputTokens > 0 || msg.OutputTokens > 0 {
				headerLine += " " + fmt.Sprintf("in:%s out:%s", formatK(msg.InputTokens), formatK(msg.OutputTokens))
			}
		}
	} else {
		// For non-selected messages, use colorful styling
		if msg.Role == "user" {
			var userLabel string
			if msg.SourceLabel != "" {
				// Show channel badge dim, user name styled
				userLabel = renderSourceLabel(msg.SourceLabel)
			} else {
				userLabel = styles.StatusInProgress.Render("you")
			}
			headerLine = fmt.Sprintf("%s[%s] %s", cursorPrefix, ts, userLabel)
		} else {
			headerLine = fmt.Sprintf("%s[%s] %s", cursorPrefix, ts, styles.StatusStaged.Render(agentName))

			// Add colorful model badge
			if msg.Model != "" {
				badge := renderModelBadge(msg.Model)
				if badge != "" {
					headerLine += " " + badge
				}
			}

			// Add token flow indicator
			tokenFlow := renderTokenFlow(msg.InputTokens, msg.OutputTokens)
			if tokenFlow != "" {
				headerLine += " " + tokenFlow
			}
		}
	}
	lines = append(lines, headerLine)

	// Render content blocks (same for selected and non-selected)
	if len(msg.ContentBlocks) > 0 {
		blockLines := p.renderContentBlocks(msg, maxWidth-4)
		for _, line := range blockLines {
			lines = append(lines, "    "+line)
		}
	} else if msg.Content != "" {
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
		// Collapsed: show preview with toggle hint (rune-safe for Unicode)
		preview := content
		if runes := []rune(preview); len(runes) > CollapsedPreviewChars {
			preview = string(runes[:CollapsedPreviewChars])
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
// Shows preview when collapsed, full content with | prefix when expanded.
func (p *Plugin) renderThinkingBlock(block adapter.ContentBlock, msgID string, maxWidth int) []string {
	expanded := p.expandedThinking[msgID]

	// Use cache key with "thinking_" prefix to distinguish from content cache
	thinkingCacheID := "thinking_" + msgID
	if cached, ok := p.getCachedRender(thinkingCacheID, maxWidth, expanded); ok {
		return strings.Split(cached, "\n")
	}

	var lines []string

	// Light purple style for thinking blocks
	thinkingStyle := lipgloss.NewStyle().
		Foreground(styles.Primary).
		Italic(true)

	thinkingIcon := "◈"
	tokenStr := formatK(block.TokenCount)

	if expanded {
		// Expanded: show ▼ indicator and full content
		header := fmt.Sprintf("%s thinking (%s tokens) ▼", thinkingIcon, tokenStr)
		lines = append(lines, thinkingStyle.Render(header))

		// Render thinking content with | prefix for visual distinction
		thinkingLines := wrapText(block.Text, maxWidth-4)
		for _, line := range thinkingLines {
			lines = append(lines, styles.Muted.Render("  │ "+line))
		}
	} else {
		// Collapsed: show ▶ indicator and preview
		preview := block.Text
		// Clean up preview (remove newlines, collapse spaces)
		preview = strings.ReplaceAll(preview, "\n", " ")
		preview = strings.Join(strings.Fields(preview), " ")

		// Truncate preview to fit (rune-safe for Unicode)
		maxPreviewLen := 60
		if runes := []rune(preview); len(runes) > maxPreviewLen {
			preview = string(runes[:maxPreviewLen-3]) + "..."
		}

		header := fmt.Sprintf("%s thinking (%s tokens) ▶", thinkingIcon, tokenStr)
		if preview != "" {
			// Add preview in subtle style
			lines = append(lines, thinkingStyle.Render(header)+" "+styles.Subtle.Render(preview))
		} else {
			lines = append(lines, thinkingStyle.Render(header))
		}
	}

	// Store in cache (td-8910b218)
	p.setCachedRender(thinkingCacheID, maxWidth, expanded, strings.Join(lines, "\n"))
	return lines
}

// renderToolUseBlock renders a tool use block with its result (expand/collapse).
func (p *Plugin) renderToolUseBlock(block adapter.ContentBlock, maxWidth int) []string {
	var lines []string

	// Tool-specific icons for visual distinction
	icon := "⚙"
	toolName := block.ToolName
	switch strings.ToLower(toolName) {
	case "read":
		icon = "◉" // Filled circle for read
	case "edit", "str_replace_editor":
		icon = "◈" // Diamond for edit
	case "write":
		icon = "◇" // Empty diamond for write (new file)
	case "bash", "shell":
		icon = "$" // Shell prompt
	case "glob", "grep", "search":
		icon = "⊙" // Target/search symbol
	case "list", "ls":
		icon = "▤" // List symbol
	case "todoread", "todowrite":
		icon = "☐" // Checkbox for tasks
	}

	// Build tool header with icon and name
	toolHeader := icon + " " + toolName

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
		toolHeader = ui.TruncateString(toolHeader, maxWidth-2)
	}

	expanded := p.expandedToolResults[block.ToolUseID]

	// Style based on error state
	if block.IsError {
		// Red styling for errors with x indicator
		errorStyle := lipgloss.NewStyle().
			Foreground(styles.Error)
		// Build error header without the original icon (avoid byte slicing Unicode)
		errorHeader := "✗ " + strings.TrimPrefix(toolHeader, icon+" ")
		lines = append(lines, errorStyle.Render(errorHeader))
	} else {
		lines = append(lines, styles.Code.Render(toolHeader))
	}

	// Show result if expanded or if there's an error
	if block.ToolOutput != "" && (expanded || block.IsError) {
		output := block.ToolOutput

		// Truncate before prettifying to prevent memory issues with large outputs
		const maxChars = 10000
		if len([]rune(output)) > maxChars {
			output = string([]rune(output)[:maxChars])
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
				line = ui.TruncateString(line, maxWidth-4)
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
			// Show first meaningful line as preview (rune-safe for Unicode)
			if runes := []rune(preview); len(runes) > maxWidth-6 {
				preview = string(runes[:maxWidth-9]) + "..."
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

	// Category filters
	sb.WriteString(styles.Subtitle.Render("Category:"))
	sb.WriteString("\n")
	categories := []struct {
		key  string
		name string
		cat  string
	}{
		{"i", "Interactive", adapter.SessionCategoryInteractive},
		{"r", "Cron jobs", adapter.SessionCategoryCron},
		{"s", "System", adapter.SessionCategorySystem},
	}
	for _, c := range categories {
		checkbox := "[ ]"
		if p.filters.HasCategory(c.cat) {
			checkbox = "[✓]"
		}
		sb.WriteString(fmt.Sprintf("  %s %s %s\n", styles.Code.Render(c.key), checkbox, c.name))
	}
	sb.WriteString("\n")

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
