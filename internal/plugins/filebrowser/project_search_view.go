package filebrowser

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// renderProjectSearchModal renders the project search overlay.
func (p *Plugin) renderProjectSearchModal() string {
	state := p.projectSearchState
	if state == nil {
		state = NewProjectSearchState()
	}

	// Clear hit regions for fresh registration
	p.mouseHandler.HitMap.Clear()

	// Modal dimensions - use most of the screen
	modalWidth := p.width - 4
	if modalWidth > 120 {
		modalWidth = 120
	}
	if modalWidth < 40 {
		modalWidth = 40
	}

	// Calculate modal position for hit region registration
	hPad := (p.width - modalWidth - 4) / 2
	if hPad < 0 {
		hPad = 0
	}
	modalX := hPad + 1 // +1 for modal border

	var sb strings.Builder

	// Header with search input
	sb.WriteString(p.renderProjectSearchHeader(modalWidth))
	sb.WriteString("\n")

	// Options bar (with hit regions)
	// Y position: paddingTop(1) + border(1) + header(1) + newline(1) = 4
	optionsY := 4
	sb.WriteString(p.renderProjectSearchOptionsWithHitRegions(modalX, optionsY))
	sb.WriteString("\n\n")

	// Calculate available height for results
	// Total height - header (3 lines) - options (1 line) - footer (2 lines) - padding
	resultsHeight := p.height - 10
	if resultsHeight < 5 {
		resultsHeight = 5
	}
	if resultsHeight > 30 {
		resultsHeight = 30
	}

	// Results Y position: options(4) + options(1) + newlines(2) = 7
	resultsY := 7

	// Results or status message
	if state.IsSearching {
		sb.WriteString(styles.Muted.Render("Searching..."))
	} else if state.Error != "" {
		sb.WriteString(styles.StatusDeleted.Render(state.Error))
	} else if len(state.Results) == 0 {
		if state.Query != "" {
			sb.WriteString(styles.Muted.Render("No matches found"))
		} else {
			sb.WriteString(styles.Muted.Render("Type to search project files..."))
		}
	} else {
		sb.WriteString(p.renderProjectSearchResultsWithHitRegions(modalX, resultsY, modalWidth-4, resultsHeight))
	}

	// Footer with match count
	sb.WriteString("\n\n")
	sb.WriteString(p.renderProjectSearchFooter())

	// Wrap in modal box
	content := sb.String()
	modal := styles.ModalBox.
		Width(modalWidth).
		Render(content)

	centered := lipgloss.NewStyle().
		PaddingLeft(hPad).
		PaddingTop(1).
		Render(modal)

	return centered
}

// renderProjectSearchHeader renders the search input bar.
func (p *Plugin) renderProjectSearchHeader(width int) string {
	state := p.projectSearchState
	cursor := "█"

	// Calculate available width for query display
	prefix := "Search: "
	available := width - len(prefix) - 1 // -1 for cursor

	query := state.Query
	if len(query) > available {
		// Show end of query if too long
		query = "..." + query[len(query)-available+3:]
	}

	header := fmt.Sprintf("%s%s%s", prefix, query, cursor)
	return styles.ModalTitle.Render(header)
}

// renderProjectSearchOptions renders the toggle options bar.
func (p *Plugin) renderProjectSearchOptions() string {
	state := p.projectSearchState

	var opts []string

	// Regex toggle
	if state.UseRegex {
		opts = append(opts, styles.BarChipActive.Render(".*"))
	} else {
		opts = append(opts, styles.BarChip.Render(".*"))
	}

	// Case sensitive toggle
	if state.CaseSensitive {
		opts = append(opts, styles.BarChipActive.Render("Aa"))
	} else {
		opts = append(opts, styles.BarChip.Render("Aa"))
	}

	// Whole word toggle
	if state.WholeWord {
		opts = append(opts, styles.BarChipActive.Render(`\b`))
	} else {
		opts = append(opts, styles.BarChip.Render(`\b`))
	}

	return strings.Join(opts, " ")
}

// renderProjectSearchOptionsWithHitRegions renders the toggle options bar and registers hit regions.
func (p *Plugin) renderProjectSearchOptionsWithHitRegions(modalX, y int) string {
	state := p.projectSearchState

	var opts []string
	x := modalX + 1 // +1 for modal padding

	// Regex toggle - width 4 (includes border/padding)
	regexWidth := 4
	p.mouseHandler.HitMap.AddRect(regionSearchToggleRegex, x, y, regexWidth, 1, nil)
	if state.UseRegex {
		opts = append(opts, styles.BarChipActive.Render(".*"))
	} else {
		opts = append(opts, styles.BarChip.Render(".*"))
	}
	x += regexWidth + 1 // +1 for space

	// Case sensitive toggle - width 4
	caseWidth := 4
	p.mouseHandler.HitMap.AddRect(regionSearchToggleCase, x, y, caseWidth, 1, nil)
	if state.CaseSensitive {
		opts = append(opts, styles.BarChipActive.Render("Aa"))
	} else {
		opts = append(opts, styles.BarChip.Render("Aa"))
	}
	x += caseWidth + 1

	// Whole word toggle - width 4
	wordWidth := 4
	p.mouseHandler.HitMap.AddRect(regionSearchToggleWord, x, y, wordWidth, 1, nil)
	if state.WholeWord {
		opts = append(opts, styles.BarChipActive.Render(`\b`))
	} else {
		opts = append(opts, styles.BarChip.Render(`\b`))
	}

	return strings.Join(opts, " ")
}

// renderProjectSearchResultsWithHitRegions renders results and registers hit regions.
func (p *Plugin) renderProjectSearchResultsWithHitRegions(modalX, startY, width, height int) string {
	state := p.projectSearchState
	if len(state.Results) == 0 {
		return ""
	}

	// Calculate visible range
	flatLen := state.FlatLen()
	if flatLen == 0 {
		return ""
	}

	// Ensure cursor is visible
	if state.Cursor >= state.ScrollOffset+height {
		state.ScrollOffset = state.Cursor - height + 1
	}
	if state.Cursor < state.ScrollOffset {
		state.ScrollOffset = state.Cursor
	}
	if state.ScrollOffset < 0 {
		state.ScrollOffset = 0
	}

	var lines []string
	flatIdx := 0
	lineY := startY

	for fi, file := range state.Results {
		// File header line
		if flatIdx >= state.ScrollOffset && len(lines) < height {
			isSelected := flatIdx == state.Cursor

			// Register hit region for file header
			p.mouseHandler.HitMap.AddRect(regionSearchFile, modalX, lineY, width, 1, fi)

			lines = append(lines, p.renderSearchFileHeader(file, fi, isSelected, width))
			lineY++
		}
		flatIdx++

		// Match lines (if not collapsed)
		if !file.Collapsed {
			for mi, match := range file.Matches {
				if flatIdx >= state.ScrollOffset && len(lines) < height {
					isSelected := flatIdx == state.Cursor

					// Register hit region for match line
					p.mouseHandler.HitMap.AddRect(regionSearchMatch, modalX, lineY, width, 1, searchMatchData{
						FileIdx:  fi,
						MatchIdx: mi,
					})

					lines = append(lines, p.renderSearchMatchLine(match, mi, isSelected, width))
					lineY++
				}
				flatIdx++
				if len(lines) >= height {
					break
				}
			}
		}

		if len(lines) >= height {
			break
		}
	}

	return strings.Join(lines, "\n")
}

// renderProjectSearchResults renders the collapsible file/match tree.
func (p *Plugin) renderProjectSearchResults(width, height int) string {
	state := p.projectSearchState
	if len(state.Results) == 0 {
		return ""
	}

	// Calculate visible range
	flatLen := state.FlatLen()
	if flatLen == 0 {
		return ""
	}

	// Ensure cursor is visible
	if state.Cursor >= state.ScrollOffset+height {
		state.ScrollOffset = state.Cursor - height + 1
	}
	if state.Cursor < state.ScrollOffset {
		state.ScrollOffset = state.Cursor
	}
	if state.ScrollOffset < 0 {
		state.ScrollOffset = 0
	}

	var lines []string
	flatIdx := 0

	for fi, file := range state.Results {
		// File header line
		if flatIdx >= state.ScrollOffset && len(lines) < height {
			isSelected := flatIdx == state.Cursor
			lines = append(lines, p.renderSearchFileHeader(file, fi, isSelected, width))
		}
		flatIdx++

		// Match lines (if not collapsed)
		if !file.Collapsed {
			for mi, match := range file.Matches {
				if flatIdx >= state.ScrollOffset && len(lines) < height {
					isSelected := flatIdx == state.Cursor
					lines = append(lines, p.renderSearchMatchLine(match, mi, isSelected, width))
				}
				flatIdx++
				if len(lines) >= height {
					break
				}
			}
		}

		if len(lines) >= height {
			break
		}
	}

	return strings.Join(lines, "\n")
}

// renderSearchFileHeader renders a file header line.
func (p *Plugin) renderSearchFileHeader(file SearchFileResult, fileIdx int, selected bool, width int) string {
	// Icon for expand/collapse
	icon := "▼ "
	if file.Collapsed {
		icon = "▶ "
	}

	// File path with match count
	matchCount := fmt.Sprintf(" (%d)", len(file.Matches))
	availableWidth := width - len(icon) - len(matchCount) - 2

	path := file.Path
	if len(path) > availableWidth {
		path = "..." + path[len(path)-availableWidth+3:]
	}

	// Build line
	line := fmt.Sprintf("%s%s%s",
		styles.FileBrowserIcon.Render(icon),
		styles.FileBrowserDir.Render(path),
		styles.Muted.Render(matchCount),
	)

	if selected {
		return styles.ListItemSelected.Render(line)
	}
	return line
}

// renderSearchMatchLine renders a single match line.
func (p *Plugin) renderSearchMatchLine(match SearchMatch, matchIdx int, selected bool, width int) string {
	// Indentation for match under file
	indent := "    "

	// Line number
	lineNum := fmt.Sprintf("%4d: ", match.LineNo)

	// Calculate available width for line text
	availableWidth := width - len(indent) - len(lineNum) - 2
	if availableWidth < 10 {
		availableWidth = 10
	}

	// Trim and truncate line text
	lineText := strings.TrimSpace(match.LineText)
	if len(lineText) > availableWidth {
		// Try to center the match in the visible portion
		matchCenter := (match.ColStart + match.ColEnd) / 2
		start := matchCenter - availableWidth/2
		if start < 0 {
			start = 0
		}
		end := start + availableWidth
		if end > len(lineText) {
			end = len(lineText)
			start = end - availableWidth
			if start < 0 {
				start = 0
			}
		}

		if start > 0 {
			lineText = "..." + lineText[start+3:]
		}
		if len(lineText) > availableWidth {
			lineText = lineText[:availableWidth-3] + "..."
		}
	}

	// Highlight the match within the line
	highlightedLine := highlightMatchInLine(lineText, match, match.ColStart, match.ColEnd)

	// Build line
	line := fmt.Sprintf("%s%s%s",
		indent,
		styles.FileBrowserLineNumber.Render(lineNum),
		highlightedLine,
	)

	if selected {
		return styles.ListItemSelected.Render(line)
	}
	return line
}

// highlightMatchInLine applies highlighting to the matched portion.
func highlightMatchInLine(lineText string, match SearchMatch, colStart, colEnd int) string {
	// Adjust for any trimming/truncation that happened
	// Find the match text within the line
	if colStart < 0 {
		colStart = 0
	}
	if colEnd > len(lineText) {
		colEnd = len(lineText)
	}
	if colStart >= colEnd || colStart >= len(lineText) {
		return lineText
	}

	// Find match text in the (possibly truncated) line
	originalMatchText := match.LineText[match.ColStart:match.ColEnd]
	matchStart := strings.Index(lineText, originalMatchText)
	if matchStart == -1 {
		// Match not found in truncated text, just return plain
		return lineText
	}
	matchEnd := matchStart + len(originalMatchText)

	// Build highlighted line
	var result strings.Builder
	if matchStart > 0 {
		result.WriteString(lineText[:matchStart])
	}
	result.WriteString(styles.SearchMatchCurrent.Render(lineText[matchStart:matchEnd]))
	if matchEnd < len(lineText) {
		result.WriteString(lineText[matchEnd:])
	}

	return result.String()
}

// renderProjectSearchFooter renders the footer with counts and hints.
func (p *Plugin) renderProjectSearchFooter() string {
	state := p.projectSearchState

	if len(state.Results) == 0 {
		return styles.Muted.Render("alt+r=regex  alt+c=case  alt+w=word  esc=close")
	}

	// Show match count and position
	position := ""
	flatLen := state.FlatLen()
	if flatLen > 0 {
		position = fmt.Sprintf("%d/%d  ", state.Cursor+1, flatLen)
	}

	stats := fmt.Sprintf("%d matches in %d files", state.TotalMatches(), state.FileCount())

	return styles.Muted.Render(position + stats + "  enter=open  esc=close")
}
