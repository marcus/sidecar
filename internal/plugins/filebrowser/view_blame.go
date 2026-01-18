package filebrowser

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// renderBlameModalContent renders the blame view modal.
func (p *Plugin) renderBlameModalContent() string {
	state := p.blameState
	if state == nil {
		return styles.ModalBox.Render(styles.Muted.Render("No blame data"))
	}

	// Modal dimensions - use most of the screen
	modalWidth := p.width - 4
	if modalWidth > 140 {
		modalWidth = 140
	}
	if modalWidth < 60 {
		modalWidth = 60
	}

	// Calculate available height for results
	resultsHeight := p.height - 10
	if resultsHeight < 5 {
		resultsHeight = 5
	}
	if resultsHeight > 40 {
		resultsHeight = 40
	}

	var sb strings.Builder

	// Header with file path
	header := fmt.Sprintf("Blame: %s", truncatePath(state.FilePath, modalWidth-10))
	sb.WriteString(styles.ModalTitle.Render(header))
	sb.WriteString("\n\n")

	// Loading state
	if state.IsLoading {
		sb.WriteString(styles.Muted.Render("Loading blame data..."))
		return styles.ModalBox.Width(modalWidth).Render(sb.String())
	}

	// Error state
	if state.Error != nil {
		sb.WriteString(styles.StatusDeleted.Render(fmt.Sprintf("Error: %v", state.Error)))
		sb.WriteString("\n\n")
		sb.WriteString(styles.Muted.Render("Press 'esc' or 'q' to close"))
		return styles.ModalBox.Width(modalWidth).Render(sb.String())
	}

	// No lines
	if len(state.Lines) == 0 {
		sb.WriteString(styles.Muted.Render("No blame data available"))
		return styles.ModalBox.Width(modalWidth).Render(sb.String())
	}

	// Ensure cursor is visible
	if state.Cursor >= state.ScrollOffset+resultsHeight {
		state.ScrollOffset = state.Cursor - resultsHeight + 1
	}
	if state.Cursor < state.ScrollOffset {
		state.ScrollOffset = state.Cursor
	}
	if state.ScrollOffset < 0 {
		state.ScrollOffset = 0
	}

	// Calculate column widths
	hashWidth := 8   // Short hash
	authorWidth := 12
	dateWidth := 12
	lineNoWidth := 5
	// Content gets remaining width
	separatorWidth := 3 // " | "
	contentWidth := modalWidth - hashWidth - authorWidth - dateWidth - lineNoWidth - separatorWidth - 6
	if contentWidth < 20 {
		contentWidth = 20
	}

	// Render visible lines
	start := state.ScrollOffset
	end := start + resultsHeight
	if end > len(state.Lines) {
		end = len(state.Lines)
	}

	for i := start; i < end; i++ {
		line := state.Lines[i]
		isSelected := i == state.Cursor

		// Build line components
		lineStr := p.renderBlameLine(line, hashWidth, authorWidth, dateWidth, lineNoWidth, contentWidth, isSelected)
		sb.WriteString(lineStr)

		if i < end-1 {
			sb.WriteString("\n")
		}
	}

	// Footer with position and hints
	sb.WriteString("\n\n")
	position := fmt.Sprintf("%d/%d", state.Cursor+1, len(state.Lines))
	footer := fmt.Sprintf("%s  j/k=scroll  enter=details  y=yank hash  esc=close", position)
	sb.WriteString(styles.Muted.Render(footer))

	return styles.ModalBox.Width(modalWidth).Render(sb.String())
}

// renderBlameLine renders a single blame line with age-based coloring.
func (p *Plugin) renderBlameLine(line BlameLine, hashW, authorW, dateW, lineNoW, contentW int, selected bool) string {
	// Get age-based color for metadata
	metaColor := getBlameAgeColor(line.AuthorTime)

	// Format each component
	hash := padOrTruncate(line.CommitHash, hashW)
	author := padOrTruncate(line.Author, authorW)
	date := padOrTruncate(RelativeTime(line.AuthorTime), dateW)
	lineNo := fmt.Sprintf("%*d", lineNoW, line.LineNo)

	// Truncate content if needed
	content := line.Content
	if len(content) > contentW {
		content = content[:contentW-1] + "…"
	}

	// Style metadata with age color
	metaStyle := lipgloss.NewStyle().Foreground(metaColor)
	lineNoStyle := styles.FileBrowserLineNumber

	// Build the line
	var lineStr string
	if selected {
		// For selected lines, use full background highlight
		fullLine := fmt.Sprintf("%s %s %s %s | %s",
			hash, author, date, lineNo, content)
		// Pad to full width for consistent selection highlight
		if len(fullLine) < hashW+authorW+dateW+lineNoW+contentW+7 {
			fullLine += strings.Repeat(" ", hashW+authorW+dateW+lineNoW+contentW+7-len(fullLine))
		}
		lineStr = styles.ListItemSelected.Render(fullLine)
	} else {
		lineStr = fmt.Sprintf("%s %s %s %s | %s",
			metaStyle.Render(hash),
			metaStyle.Render(author),
			metaStyle.Render(date),
			lineNoStyle.Render(lineNo),
			content)
	}

	return lineStr
}

// getBlameAgeColor returns a color based on commit age.
// Recent commits are brighter, older commits are more muted.
func getBlameAgeColor(commitTime time.Time) lipgloss.Color {
	if commitTime.IsZero() {
		return styles.TextMuted
	}

	age := time.Since(commitTime)

	switch {
	case age < 24*time.Hour:
		// Less than 1 day - bright green
		return lipgloss.Color("#10B981")
	case age < 7*24*time.Hour:
		// Less than 1 week - lighter green
		return lipgloss.Color("#34D399")
	case age < 30*24*time.Hour:
		// Less than 1 month - yellow/green
		return lipgloss.Color("#84CC16")
	case age < 90*24*time.Hour:
		// Less than 3 months - yellow
		return lipgloss.Color("#FBBF24")
	case age < 180*24*time.Hour:
		// Less than 6 months - orange
		return lipgloss.Color("#F97316")
	case age < 365*24*time.Hour:
		// Less than 1 year - muted
		return lipgloss.Color("#9CA3AF")
	default:
		// More than 1 year - very muted
		return lipgloss.Color("#6B7280")
	}
}

// padOrTruncate ensures a string is exactly the specified width.
func padOrTruncate(s string, width int) string {
	if len(s) > width {
		return s[:width-1] + "…"
	}
	return s + strings.Repeat(" ", width-len(s))
}
