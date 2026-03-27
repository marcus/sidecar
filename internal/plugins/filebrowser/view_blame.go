package filebrowser

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/styles"
)

const (
	// blameModalHeaderFooterLines accounts for:
	// - Modal title + border (3 lines)
	// - Modal padding/margins (4 lines)
	// - Section spacing (2 lines)
	blameModalHeaderFooterLines = 9
	// blameMinVisibleLines is the minimum number of blame lines to display.
	blameMinVisibleLines = 5
	// blameMaxVisibleLines is the maximum number of blame lines to display.
	blameMaxVisibleLines = 40

	// Modal element IDs
	blameActionID  = "blame-action"  // Primary action (close on Esc)
	blameContentID = "blame-content" // Content section ID

	// Column widths for blame line rendering
	blameColumnHash      = 8
	blameColumnAuthor    = 12
	blameColumnDate      = 12
	blameColumnLineNo    = 5
	blameColumnSeparator = 3 // " | "

	// Age duration thresholds for color coding
	ageOneDay   = 24 * time.Hour
	ageOneWeek  = 7 * 24 * time.Hour
	ageOneMonth = 30 * 24 * time.Hour
	ageOneYear  = 365 * 24 * time.Hour
)

// ensureBlameModal builds/rebuilds the blame modal.
// CRITICAL: Must be called before each key/mouse event to ensure conditional
// sections evaluate against current state and modal exists for interaction.
func (p *Plugin) ensureBlameModal() {
	if p.blameState == nil {
		return
	}

	// Calculate modal dimensions - use most of the screen
	modalW := p.width - 4
	if modalW > 140 {
		modalW = 140
	}
	if modalW < 60 {
		modalW = 60
	}

	// Only rebuild if modal doesn't exist or width changed
	if p.blameModal != nil && p.blameModalWidth == modalW {
		return
	}
	p.blameModalWidth = modalW

	// Calculate available height for results
	resultsHeight := p.height - blameModalHeaderFooterLines
	if resultsHeight < blameMinVisibleLines {
		resultsHeight = blameMinVisibleLines
	}
	if resultsHeight > blameMaxVisibleLines {
		resultsHeight = blameMaxVisibleLines
	}

	title := fmt.Sprintf("Blame: %s", truncatePath(p.blameState.FilePath, modalW-10))

	p.blameModal = modal.New(title,
		modal.WithWidth(modalW),
		modal.WithPrimaryAction(blameActionID),
		modal.WithHints(false),
	).
		AddSection(p.blameHeaderSection()).
		AddSection(modal.When(func() bool {
			return !p.blameState.IsLoading && p.blameState.Error == nil && len(p.blameState.Lines) > 0
		}, p.blameContentSection(resultsHeight))).
		AddSection(modal.When(func() bool { return p.blameState.IsLoading }, p.blameLoadingSection())).
		AddSection(modal.When(func() bool { return p.blameState.Error != nil }, p.blameErrorSection())).
		AddSection(modal.When(func() bool {
			return !p.blameState.IsLoading && p.blameState.Error == nil && len(p.blameState.Lines) == 0
		}, p.blameEmptySection()))
}

// blameHeaderSection is intentionally empty - title is in modal header
func (p *Plugin) blameHeaderSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		return modal.RenderedSection{Content: ""}
	}, nil)
}

// blameLoadingSection shows loading state.
func (p *Plugin) blameLoadingSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		content := styles.Muted.Render("Loading blame data...")
		return modal.RenderedSection{Content: content}
	}, nil)
}

// blameErrorSection shows error state.
func (p *Plugin) blameErrorSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.blameState == nil || p.blameState.Error == nil {
			return modal.RenderedSection{}
		}
		content := styles.StatusDeleted.Render(fmt.Sprintf("Error: %v", p.blameState.Error))
		return modal.RenderedSection{Content: content}
	}, nil)
}

// blameEmptySection shows empty state.
func (p *Plugin) blameEmptySection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		content := styles.Muted.Render("No blame data available")
		return modal.RenderedSection{Content: content}
	}, nil)
}

// blameContentSection renders the scrollable blame lines.
func (p *Plugin) blameContentSection(resultsHeight int) modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.blameState == nil || len(p.blameState.Lines) == 0 {
			return modal.RenderedSection{}
		}

		state := p.blameState

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

		// Calculate column widths based on contentWidth
		hashWidth := blameColumnHash
		authorWidth := blameColumnAuthor
		dateWidth := blameColumnDate
		lineNoWidth := blameColumnLineNo
		separatorWidth := blameColumnSeparator
		contentW := contentWidth - hashWidth - authorWidth - dateWidth - lineNoWidth - separatorWidth - 2
		if contentW < 20 {
			contentW = 20
		}

		// Render visible lines
		start := state.ScrollOffset
		end := start + resultsHeight
		if end > len(state.Lines) {
			end = len(state.Lines)
		}

		var sb strings.Builder
		for i := start; i < end; i++ {
			line := state.Lines[i]
			isSelected := i == state.Cursor
			lineStr := p.renderBlameLine(line, hashWidth, authorWidth, dateWidth, lineNoWidth, contentW, isSelected)
			sb.WriteString(lineStr)
			if i < end-1 {
				sb.WriteString("\n")
			}
		}

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

// renderBlameModalContent renders the blame view modal.
func (p *Plugin) renderBlameModalContent() string {
	p.ensureBlameModal()
	if p.blameModal == nil {
		return ""
	}

	return p.blameModal.Render(p.width, p.height, p.mouseHandler)
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

	// Truncate content if needed (use runes for unicode safety)
	content := line.Content
	contentRunes := []rune(content)
	if len(contentRunes) > contentW {
		content = string(contentRunes[:contentW-1]) + "…"
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
	case age < ageOneDay:
		return styles.Success
	case age < ageOneWeek:
		return styles.BlameAge1
	case age < ageOneMonth:
		return styles.BlameAge2
	case age < 3*ageOneMonth:
		return styles.BlameAge3
	case age < 6*ageOneMonth:
		return styles.BlameAge4
	case age < ageOneYear:
		return styles.BlameAge5
	default:
		return styles.TextMuted
	}
}

// padOrTruncate ensures a string is exactly the specified width (in runes).
func padOrTruncate(s string, width int) string {
	runes := []rune(s)
	if len(runes) > width {
		return string(runes[:width-1]) + "…"
	}
	return s + strings.Repeat(" ", width-len(runes))
}
