package filebrowser

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	projectSearchToggleRegexID = "project-search-toggle-regex"
	projectSearchToggleCaseID  = "project-search-toggle-case"
	projectSearchToggleWordID  = "project-search-toggle-word"
	projectSearchOpenActionID  = "project-search-open"
	projectSearchFilePrefix    = "project-search-file-"
	projectSearchMatchPrefix   = "project-search-match-"
)

func projectSearchFileID(fileIdx int) string {
	return fmt.Sprintf("%s%d", projectSearchFilePrefix, fileIdx)
}

func projectSearchMatchID(fileIdx, matchIdx int) string {
	return fmt.Sprintf("%s%d-%d", projectSearchMatchPrefix, fileIdx, matchIdx)
}

func parseProjectSearchFileID(id string) (int, bool) {
	if !strings.HasPrefix(id, projectSearchFilePrefix) {
		return 0, false
	}

	idx, err := strconv.Atoi(strings.TrimPrefix(id, projectSearchFilePrefix))
	if err != nil {
		return 0, false
	}
	return idx, true
}

func parseProjectSearchMatchID(id string) (int, int, bool) {
	if !strings.HasPrefix(id, projectSearchMatchPrefix) {
		return 0, 0, false
	}

	rest := strings.TrimPrefix(id, projectSearchMatchPrefix)
	parts := strings.Split(rest, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}

	fileIdx, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}

	matchIdx, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}

	return fileIdx, matchIdx, true
}

// renderProjectSearchModalContent renders the project search modal box content.
func (p *Plugin) renderProjectSearchModalContent() string {
	p.ensureProjectSearchModal()
	if p.projectSearchModal == nil {
		return ""
	}
	return p.projectSearchModal.Render(p.width, p.height, p.mouseHandler)
}

// ensureProjectSearchModal builds/rebuilds the project search modal.
func (p *Plugin) ensureProjectSearchModal() {
	if p.projectSearchState == nil {
		return
	}

	modalW := p.projectSearchModalWidthForView()
	if p.projectSearchModal != nil && p.projectSearchModalWidth == modalW {
		return
	}
	p.projectSearchModalWidth = modalW

	p.projectSearchModal = modal.New("",
		modal.WithWidth(modalW),
		modal.WithPrimaryAction(projectSearchOpenActionID),
		modal.WithHints(false),
	).
		AddSection(p.projectSearchHeaderSection()).
		AddSection(p.projectSearchOptionsSection()).
		AddSection(modal.Spacer()).
		AddSection(p.projectSearchResultsSection()).
		AddSection(modal.When(p.projectSearchHasResults, modal.Spacer())).
		AddSection(modal.When(p.projectSearchHasResults, p.projectSearchStatsSection()))
}

func (p *Plugin) projectSearchModalWidthForView() int {
	modalW := 120
	maxWidth := p.width - 4
	if maxWidth < 1 {
		maxWidth = 1
	}
	if modalW > maxWidth {
		modalW = maxWidth
	}
	minWidth := 40
	if maxWidth < minWidth {
		minWidth = maxWidth
	}
	if modalW < minWidth {
		modalW = minWidth
	}
	return modalW
}

func (p *Plugin) clearProjectSearchModal() {
	p.projectSearchModal = nil
	p.projectSearchModalWidth = 0
}

func (p *Plugin) projectSearchHasResults() bool {
	state := p.projectSearchState
	return state != nil && len(state.Results) > 0
}

func (p *Plugin) projectSearchHeaderSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.projectSearchState == nil {
			return modal.RenderedSection{}
		}
		return modal.RenderedSection{Content: p.renderProjectSearchHeader(contentWidth)}
	}, nil)
}

func (p *Plugin) projectSearchOptionsSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		state := p.projectSearchState
		if state == nil {
			return modal.RenderedSection{}
		}

		type option struct {
			id     string
			label  string
			active bool
		}

		opts := []option{
			{id: projectSearchToggleRegexID, label: ".*", active: state.UseRegex},
			{id: projectSearchToggleCaseID, label: "Aa", active: state.CaseSensitive},
			{id: projectSearchToggleWordID, label: `\\b`, active: state.WholeWord},
		}

		var sb strings.Builder
		focusables := make([]modal.FocusableInfo, 0, len(opts))
		x := 0

		for i, opt := range opts {
			if i > 0 {
				sb.WriteString(" ")
				x++
			}

			style := styles.BarChip
			if opt.active || opt.id == focusID || opt.id == hoverID {
				style = styles.BarChipActive
			}

			rendered := style.Render(opt.label)
			sb.WriteString(rendered)

			width := ansi.StringWidth(rendered)
			focusables = append(focusables, modal.FocusableInfo{
				ID:      opt.id,
				OffsetX: x,
				OffsetY: 0,
				Width:   width,
				Height:  1,
			})
			x += width
		}

		return modal.RenderedSection{
			Content:    sb.String(),
			Focusables: focusables,
		}
	}, p.projectSearchOptionsUpdate)
}

func (p *Plugin) projectSearchOptionsUpdate(msg tea.Msg, focusID string) (string, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", nil
	}

	if focusID != projectSearchToggleRegexID && focusID != projectSearchToggleCaseID && focusID != projectSearchToggleWordID {
		return "", nil
	}

	// Note: Space is NOT handled here - it should always add to the search query.
	// Options can be toggled via Enter, mouse click, or alt+r/c/w shortcuts.
	switch keyMsg.String() {
	case "enter":
		return focusID, nil
	}

	return "", nil
}

func (p *Plugin) projectSearchResultsSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		state := p.projectSearchState
		if state == nil {
			return modal.RenderedSection{}
		}

		maxVisible := p.projectSearchMaxVisible()

		// Helper to pad content to minimum height so modal doesn't jump in size
		// Uses " " instead of "" so lines aren't trimmed by measureHeight
		padToMinHeight := func(content string) string {
			lines := strings.Split(content, "\n")
			for len(lines) < maxVisible {
				lines = append(lines, " ")
			}
			return strings.Join(lines, "\n")
		}

		if state.IsSearching {
			return modal.RenderedSection{Content: padToMinHeight(styles.Muted.Render("Searching..."))}
		}
		if state.Error != "" {
			return modal.RenderedSection{Content: padToMinHeight(styles.StatusDeleted.Render(state.Error))}
		}
		if len(state.Results) == 0 {
			if state.Query != "" {
				return modal.RenderedSection{Content: padToMinHeight(styles.Muted.Render("No matches found"))}
			}
			return modal.RenderedSection{Content: padToMinHeight(styles.Muted.Render("Type to search project files..."))}
		}

		flatLen := state.FlatLen()
		if flatLen == 0 {
			return modal.RenderedSection{Content: padToMinHeight(styles.Muted.Render("No matches found"))}
		}

		if state.Cursor >= state.ScrollOffset+maxVisible {
			state.ScrollOffset = state.Cursor - maxVisible + 1
		}
		if state.Cursor < state.ScrollOffset {
			state.ScrollOffset = state.Cursor
		}
		if state.ScrollOffset < 0 {
			state.ScrollOffset = 0
		}

		var lines []string
		focusables := make([]modal.FocusableInfo, 0, maxVisible)
		flatIdx := 0
		lineY := 0

		for fi, file := range state.Results {
			if flatIdx >= state.ScrollOffset && len(lines) < maxVisible {
				itemID := projectSearchFileID(fi)
				selected := flatIdx == state.Cursor
				hovered := itemID == hoverID
				line := p.renderSearchFileHeader(file, fi, selected, hovered, contentWidth)

				lines = append(lines, line)
				focusables = append(focusables, modal.FocusableInfo{
					ID:      itemID,
					OffsetX: 0,
					OffsetY: lineY,
					Width:   contentWidth, // Full width for hover detection
					Height:  1,
				})
				lineY++
			}
			flatIdx++

			if !file.Collapsed {
				for mi, match := range file.Matches {
					if flatIdx >= state.ScrollOffset && len(lines) < maxVisible {
						itemID := projectSearchMatchID(fi, mi)
						selected := flatIdx == state.Cursor
						hovered := itemID == hoverID
						line := p.renderSearchMatchLine(match, mi, selected, hovered, contentWidth)

						lines = append(lines, line)
						focusables = append(focusables, modal.FocusableInfo{
							ID:      itemID,
							OffsetX: 0,
							OffsetY: lineY,
							Width:   contentWidth, // Full width for hover detection
							Height:  1,
						})
						lineY++
					}
					flatIdx++
					if len(lines) >= maxVisible {
						break
					}
				}
			}

			if len(lines) >= maxVisible {
				break
			}
		}

		// Pad to minimum height so modal doesn't jump in size
		// Uses " " instead of "" so lines aren't trimmed by measureHeight
		for len(lines) < maxVisible {
			lines = append(lines, " ")
		}
		content := strings.Join(lines, "\n")

		return modal.RenderedSection{
			Content:    content,
			Focusables: focusables,
		}
	}, nil)
}

func (p *Plugin) projectSearchStatsSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		state := p.projectSearchState
		if state == nil || len(state.Results) == 0 {
			return modal.RenderedSection{}
		}

		position := ""
		flatLen := state.FlatLen()
		if flatLen > 0 {
			position = fmt.Sprintf("%d/%d  ", state.Cursor+1, flatLen)
		}
		stats := fmt.Sprintf("%d matches in %d files", state.TotalMatches(), state.FileCount())

		return modal.RenderedSection{Content: styles.Muted.Render(position + stats)}
	}, nil)
}

func (p *Plugin) projectSearchMaxVisible() int {
	height := p.height - 10
	if height < 5 {
		height = 5
	}
	if height > 30 {
		height = 30
	}
	return height
}

// renderProjectSearchHeader renders the search input bar.
func (p *Plugin) renderProjectSearchHeader(width int) string {
	state := p.projectSearchState
	// Show block cursor when input focused, thin cursor when results focused
	cursor := "█"
	if state.ResultsFocused {
		cursor = "▏"
	}

	prefix := "Search: "
	available := width - len(prefix) - 1
	if available < 0 {
		available = 0
	}

	query := state.Query
	if len(query) > available {
		query = ui.TruncateStart(query, available)
	}

	header := fmt.Sprintf("%s%s%s", prefix, query, cursor)
	return styles.ModalTitle.Render(header)
}

// renderSearchFileHeader renders a file header line.
func (p *Plugin) renderSearchFileHeader(file SearchFileResult, fileIdx int, selected, hovered bool, width int) string {
	icon := "▼ "
	if file.Collapsed {
		icon = "▶ "
	}

	matchCount := fmt.Sprintf(" (%d)", len(file.Matches))
	availableWidth := width - len(icon) - len(matchCount) - 2

	path := file.Path
	if len(path) > availableWidth {
		path = ui.TruncateStart(path, availableWidth)
	}

	if selected || hovered {
		// Build plain text version for full-width highlight
		plainLine := icon + path + matchCount
		// Pad to full width
		if len(plainLine) < width {
			plainLine += strings.Repeat(" ", width-len(plainLine))
		}
		return styles.ListItemSelected.Render(plainLine)
	}

	return fmt.Sprintf("%s%s%s",
		styles.FileBrowserIcon.Render(icon),
		styles.FileBrowserDir.Render(path),
		styles.Muted.Render(matchCount),
	)
}

// renderSearchMatchLine renders a single match line.
func (p *Plugin) renderSearchMatchLine(match SearchMatch, matchIdx int, selected, hovered bool, width int) string {
	indent := "    "
	lineNum := fmt.Sprintf("%4d: ", match.LineNo)

	availableWidth := width - len(indent) - len(lineNum) - 2
	if availableWidth < 10 {
		availableWidth = 10
	}

	lineText := strings.TrimSpace(match.LineText)

	runeStart := ui.BytePosToRunePos(match.LineText, match.ColStart)
	runeEnd := ui.BytePosToRunePos(match.LineText, match.ColEnd)

	leadingSpaces := len(match.LineText) - len(strings.TrimLeft(match.LineText, " \t"))
	leadingRuneOffset := ui.BytePosToRunePos(match.LineText, leadingSpaces)
	runeStart -= leadingRuneOffset
	runeEnd -= leadingRuneOffset
	if runeStart < 0 {
		runeStart = 0
	}
	if runeEnd < runeStart {
		runeEnd = runeStart
	}

	lineText, hlStart, hlEnd := ui.TruncateMid(lineText, availableWidth, runeStart, runeEnd)

	if selected || hovered {
		// Build plain text for full-width highlight (keeps match visible within selection)
		plainLine := indent + lineNum + lineText
		// Pad to full width
		if len(plainLine) < width {
			plainLine += strings.Repeat(" ", width-len(plainLine))
		}
		// Highlight the match within the plain text
		matchStart := len(indent) + len(lineNum) + hlStart
		matchEnd := len(indent) + len(lineNum) + hlEnd
		return highlightMatchInSelection(plainLine, matchStart, matchEnd)
	}

	highlightedLine := highlightMatchInLineRunes(lineText, hlStart, hlEnd)
	return fmt.Sprintf("%s%s%s",
		indent,
		styles.FileBrowserLineNumber.Render(lineNum),
		highlightedLine,
	)
}

// highlightMatchInSelection applies selection style with embedded match highlight.
func highlightMatchInSelection(line string, matchStart, matchEnd int) string {
	if matchStart < 0 {
		matchStart = 0
	}
	if matchEnd > len(line) {
		matchEnd = len(line)
	}
	if matchStart >= matchEnd || matchStart >= len(line) {
		return styles.ListItemSelected.Render(line)
	}

	// Split the line and apply styles
	before := line[:matchStart]
	match := line[matchStart:matchEnd]
	after := line[matchEnd:]

	return styles.ListItemSelected.Render(before) +
		styles.SearchMatchCurrent.Render(match) +
		styles.ListItemSelected.Render(after)
}

// highlightMatchInLineRunes applies highlighting using rune positions (safe for Unicode).
func highlightMatchInLineRunes(lineText string, runeStart, runeEnd int) string {
	runes := []rune(lineText)

	if runeStart < 0 {
		runeStart = 0
	}
	if runeEnd > len(runes) {
		runeEnd = len(runes)
	}
	if runeStart >= runeEnd || runeStart >= len(runes) {
		return lineText
	}

	var result strings.Builder
	if runeStart > 0 {
		result.WriteString(string(runes[:runeStart]))
	}
	result.WriteString(styles.SearchMatchCurrent.Render(string(runes[runeStart:runeEnd])))
	if runeEnd < len(runes) {
		result.WriteString(string(runes[runeEnd:]))
	}

	return result.String()
}
