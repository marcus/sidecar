package conversations

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/app"
)

// yankSessionDetails copies session summary to clipboard.
func (p *Plugin) yankSessionDetails() tea.Cmd {
	session := p.getSelectedSession()
	if session == nil {
		return nil
	}

	md := formatSessionSummary(session)
	return func() tea.Msg {
		if err := clipboard.WriteAll(md); err != nil {
			return app.ToastMsg{Message: "Copy failed: " + err.Error(), Duration: 2 * time.Second, IsError: true}
		}
		return app.ToastMsg{Message: "Yanked session details", Duration: 2 * time.Second}
	}
}

// yankTurnContent copies the current turn's content to clipboard.
func (p *Plugin) yankTurnContent() tea.Cmd {
	turn := p.getCurrentTurn()
	if turn == nil {
		return nil
	}

	md := formatTurnAsMarkdown(turn)
	return func() tea.Msg {
		if err := clipboard.WriteAll(md); err != nil {
			return app.ToastMsg{Message: "Copy failed: " + err.Error(), Duration: 2 * time.Second, IsError: true}
		}
		return app.ToastMsg{Message: "Yanked turn content", Duration: 2 * time.Second}
	}
}

// yankResumeCommand copies the CLI resume command to clipboard.
func (p *Plugin) yankResumeCommand() tea.Cmd {
	var session *adapter.Session

	// If in message view, find session by selectedSession ID
	if p.selectedSession != "" {
		for i := range p.sessions {
			if p.sessions[i].ID == p.selectedSession {
				session = &p.sessions[i]
				break
			}
		}
	} else {
		// Use cursor selection from session list
		sessions := p.visibleSessions()
		if p.cursor >= 0 && p.cursor < len(sessions) {
			session = &sessions[p.cursor]
		}
	}

	if session == nil {
		return nil
	}

	cmd := resumeCommand(session)
	if cmd == "" {
		return nil
	}

	return func() tea.Msg {
		if err := clipboard.WriteAll(cmd); err != nil {
			return app.ToastMsg{Message: "Copy failed: " + err.Error(), Duration: 2 * time.Second, IsError: true}
		}
		return app.ToastMsg{Message: "Yanked: " + cmd, Duration: 2 * time.Second}
	}
}

// getSelectedSession returns the session under cursor based on current view mode.
func (p *Plugin) getSelectedSession() *sessionRef {
	sessions := p.visibleSessions()
	if len(sessions) == 0 {
		return nil
	}

	// In session list views, use cursor
	if p.cursor >= 0 && p.cursor < len(sessions) {
		s := sessions[p.cursor]
		return &sessionRef{
			ID:        s.ID,
			Name:      s.Name,
			Slug:      s.Slug,
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
			Duration:  s.Duration,
			Tokens:    s.TotalTokens,
			EstCost:   s.EstCost,
			Adapter:   s.AdapterName,
		}
	}
	return nil
}

// getSelectedSessionID returns the current session ID for resume command.
func (p *Plugin) getSelectedSessionID() string {
	// If we have a selected session (in message views), use that
	if p.selectedSession != "" {
		return p.selectedSession
	}
	// Otherwise, use cursor selection from session list
	sessions := p.visibleSessions()
	if p.cursor >= 0 && p.cursor < len(sessions) {
		return sessions[p.cursor].ID
	}
	return ""
}

// getCurrentTurn returns the turn under cursor based on current view mode.
func (p *Plugin) getCurrentTurn() *Turn {
	// In detail mode, return the detail turn
	if p.detailMode {
		return p.detailTurn
	}
	// When in messages pane, return the selected turn
	if p.activePane == PaneMessages {
		if p.turnViewMode {
			// Turn view: use turnCursor
			if p.turnCursor >= 0 && p.turnCursor < len(p.turns) {
				return &p.turns[p.turnCursor]
			}
		} else {
			// Conversation flow: find turn containing selected message
			if p.messageCursor >= 0 && p.messageCursor < len(p.messages) {
				msgID := p.messages[p.messageCursor].ID
				for i := range p.turns {
					for _, m := range p.turns[i].Messages {
						if m.ID == msgID {
							return &p.turns[i]
						}
					}
				}
			}
		}
	}
	return nil
}

// sessionRef holds session data for formatting.
type sessionRef struct {
	ID        string
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Duration  time.Duration
	Tokens    int
	EstCost   float64
	Adapter   string
}

// formatSessionSummary formats session details as markdown.
func formatSessionSummary(s *sessionRef) string {
	var sb strings.Builder

	// Title
	name := s.Name
	if name == "" {
		name = s.Slug
	}
	if name == "" {
		name = s.ID
	}
	sb.WriteString(fmt.Sprintf("# %s\n\n", name))

	// Metadata
	sb.WriteString(fmt.Sprintf("**Session ID:** `%s`\n", s.ID))
	if s.Adapter != "" {
		sb.WriteString(fmt.Sprintf("**Adapter:** %s\n", s.Adapter))
	}
	sb.WriteString(fmt.Sprintf("**Created:** %s\n", s.CreatedAt.Format("2006-01-02 15:04:05")))
	if !s.UpdatedAt.IsZero() && s.UpdatedAt.After(s.CreatedAt) {
		sb.WriteString(fmt.Sprintf("**Updated:** %s\n", s.UpdatedAt.Format("2006-01-02 15:04:05")))
	}
	if s.Duration > 0 {
		sb.WriteString(fmt.Sprintf("**Duration:** %s\n", formatExportDuration(s.Duration)))
	}

	// Stats
	if s.Tokens > 0 || s.EstCost > 0 {
		sb.WriteString("\n## Stats\n\n")
		if s.Tokens > 0 {
			sb.WriteString(fmt.Sprintf("- **Tokens:** %d\n", s.Tokens))
		}
		if s.EstCost > 0 {
			sb.WriteString(fmt.Sprintf("- **Est. Cost:** $%.4f\n", s.EstCost))
		}
	}

	return sb.String()
}

// formatTurnAsMarkdown formats a turn as markdown for clipboard.
func formatTurnAsMarkdown(turn *Turn) string {
	var sb strings.Builder

	// Header
	role := turn.Role
	if runes := []rune(role); len(runes) > 0 {
		role = strings.ToUpper(string(runes[:1])) + string(runes[1:])
	}
	sb.WriteString(fmt.Sprintf("## %s (%s)\n\n", role, turn.FirstTimestamp()))

	// Token info
	if turn.TotalTokensIn > 0 || turn.TotalTokensOut > 0 {
		sb.WriteString(fmt.Sprintf("*Tokens: in=%d, out=%d*\n\n", turn.TotalTokensIn, turn.TotalTokensOut))
	}

	// Content from all messages in turn
	for _, msg := range turn.Messages {
		// Thinking blocks
		if len(msg.ThinkingBlocks) > 0 {
			for _, tb := range msg.ThinkingBlocks {
				sb.WriteString("<details>\n")
				sb.WriteString(fmt.Sprintf("<summary>Thinking (%d tokens)</summary>\n\n", tb.TokenCount))
				sb.WriteString(tb.Content)
				sb.WriteString("\n\n</details>\n\n")
			}
		}

		// Main content
		if msg.Content != "" {
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		}

		// Tool uses
		if len(msg.ToolUses) > 0 {
			sb.WriteString("**Tools:**\n")
			for _, tool := range msg.ToolUses {
				filePath := extractFilePath(tool.Input)
				if filePath != "" {
					sb.WriteString(fmt.Sprintf("- %s: `%s`\n", tool.Name, filePath))
				} else {
					sb.WriteString(fmt.Sprintf("- %s\n", tool.Name))
				}
			}
			sb.WriteString("\n")
		}
	}

	return strings.TrimSpace(sb.String())
}
