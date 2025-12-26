package conversations

import (
	"fmt"
	"strings"
	"time"

	"github.com/sst/sidecar/internal/adapter"
	"github.com/sst/sidecar/internal/styles"
)

// renderNoAdapter renders the view when no adapter is available.
func renderNoAdapter() string {
	return styles.Muted.Render(" Claude Code sessions not available")
}

// renderSessions renders the session list view.
func (p *Plugin) renderSessions() string {
	var sb strings.Builder

	// Header
	header := fmt.Sprintf(" Claude Code Sessions                    %d sessions", len(p.sessions))
	sb.WriteString(styles.PanelHeader.Render(header))
	sb.WriteString("\n")
	sb.WriteString(styles.Muted.Render(strings.Repeat("━", p.width-2)))
	sb.WriteString("\n")

	// Content
	if len(p.sessions) == 0 {
		sb.WriteString(styles.Muted.Render(" No sessions found for this project"))
	} else {
		contentHeight := p.height - 5
		if contentHeight < 1 {
			contentHeight = 1
		}

		end := p.scrollOff + contentHeight
		if end > len(p.sessions) {
			end = len(p.sessions)
		}

		for i := p.scrollOff; i < end; i++ {
			session := p.sessions[i]
			selected := i == p.cursor
			sb.WriteString(p.renderSessionRow(session, selected))
			sb.WriteString("\n")
		}
	}

	// Footer
	sb.WriteString(styles.Muted.Render(strings.Repeat("━", p.width-2)))
	sb.WriteString("\n")
	sb.WriteString(p.renderSessionFooter())

	return sb.String()
}

// renderSessionRow renders a single session row.
func (p *Plugin) renderSessionRow(session adapter.Session, selected bool) string {
	// Cursor
	cursor := "  "
	if selected {
		cursor = styles.ListCursor.Render("> ")
	}

	// Timestamp
	ts := session.UpdatedAt.Local().Format("2006-01-02 15:04")

	// Active indicator
	active := ""
	if session.IsActive {
		active = styles.StatusInProgress.Render(" ●")
	}

	// Session name/ID
	name := session.Name
	if name == "" {
		name = session.ID[:8]
	}

	// Compose line
	lineStyle := styles.ListItemNormal
	if selected {
		lineStyle = styles.ListItemSelected
	}

	// Calculate available width
	maxNameWidth := p.width - 30
	if len(name) > maxNameWidth && maxNameWidth > 3 {
		name = name[:maxNameWidth-3] + "..."
	}

	return lineStyle.Render(fmt.Sprintf("%s%s  %s%s", cursor, ts, name, active))
}

// renderSessionFooter renders the session list footer.
func (p *Plugin) renderSessionFooter() string {
	hints := []string{
		styles.KeyHint.Render("enter") + " view",
		styles.KeyHint.Render("r") + " refresh",
		styles.KeyHint.Render("?") + " help",
	}
	return styles.Muted.Render(" " + strings.Join(hints, "  "))
}

// renderMessages renders the message view.
func (p *Plugin) renderMessages() string {
	var sb strings.Builder

	// Find session name
	sessionName := p.selectedSession[:8]
	for _, s := range p.sessions {
		if s.ID == p.selectedSession {
			sessionName = s.Name
			if sessionName == "" {
				sessionName = s.ID[:8]
			}
			break
		}
	}

	// Header
	header := fmt.Sprintf(" Session: %s                    %d messages", sessionName, len(p.messages))
	sb.WriteString(styles.PanelHeader.Render(header))
	sb.WriteString("\n")
	sb.WriteString(styles.Muted.Render(strings.Repeat("━", p.width-2)))
	sb.WriteString("\n")

	// Content
	if len(p.messages) == 0 {
		sb.WriteString(styles.Muted.Render(" No messages in this session"))
	} else {
		contentHeight := p.height - 5
		if contentHeight < 1 {
			contentHeight = 1
		}

		// Render messages
		lineCount := 0
		for i := p.msgScrollOff; i < len(p.messages) && lineCount < contentHeight; i++ {
			msg := p.messages[i]
			lines := p.renderMessage(msg, p.width-4)
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

	// Footer
	sb.WriteString(styles.Muted.Render(strings.Repeat("━", p.width-2)))
	sb.WriteString("\n")
	sb.WriteString(p.renderMessageFooter())

	return sb.String()
}

// renderMessage renders a single message.
func (p *Plugin) renderMessage(msg adapter.Message, maxWidth int) []string {
	var lines []string

	// Header line: [timestamp] role (tokens)
	ts := msg.Timestamp.Local().Format("15:04:05")
	roleStyle := styles.Muted
	if msg.Role == "user" {
		roleStyle = styles.StatusInProgress
	} else {
		roleStyle = styles.StatusStaged
	}

	tokens := ""
	if msg.OutputTokens > 0 {
		tokens = fmt.Sprintf(" %dk tok", msg.OutputTokens/1000)
	}

	headerLine := fmt.Sprintf(" [%s] %s%s",
		styles.Muted.Render(ts),
		roleStyle.Render(msg.Role),
		styles.Muted.Render(tokens))
	lines = append(lines, headerLine)

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

	// Tool uses
	if len(msg.ToolUses) > 0 {
		for _, tu := range msg.ToolUses {
			toolLine := fmt.Sprintf(" [tool] %s", tu.Name)
			lines = append(lines, styles.Code.Render(toolLine))
		}
	}

	// Empty line between messages
	lines = append(lines, "")

	return lines
}

// renderMessageFooter renders the message view footer.
func (p *Plugin) renderMessageFooter() string {
	hints := []string{
		styles.KeyHint.Render("esc") + " back",
		styles.KeyHint.Render("j/k") + " scroll",
		styles.KeyHint.Render("?") + " help",
	}
	return styles.Muted.Render(" " + strings.Join(hints, "  "))
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
