package filebrowser

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// FocusPane represents which pane is active.
type FocusPane int

const (
	PaneTree FocusPane = iota
	PanePreview
)

// calculatePaneWidths sets the tree and preview pane widths.
func (p *Plugin) calculatePaneWidths() {
	// Account for borders (2 chars each pane) and separator
	available := p.width - 6
	p.treeWidth = available * 30 / 100
	if p.treeWidth < 20 {
		p.treeWidth = 20
	}
	p.previewWidth = available - p.treeWidth
	if p.previewWidth < 40 {
		p.previewWidth = 40
	}
}

// renderView creates the 2-pane layout.
func (p *Plugin) renderView() string {
	// Project search is a full overlay - render modal instead of normal panes
	if p.projectSearchMode {
		return p.renderProjectSearchModal()
	}

	// Quick open is a full overlay - render modal instead of normal panes
	if p.quickOpenMode {
		return p.renderQuickOpenModal()
	}

	p.calculatePaneWidths()

	// Determine border styles based on focus
	treeBorder := styles.PanelInactive
	previewBorder := styles.PanelInactive
	if p.activePane == PaneTree && !p.searchMode && !p.contentSearchMode {
		treeBorder = styles.PanelActive
	} else if p.activePane == PanePreview && !p.searchMode && !p.contentSearchMode {
		previewBorder = styles.PanelActive
	}

	// Account for input bar if active (search or file op)
	inputBarHeight := 0
	if p.searchMode || p.contentSearchMode || p.fileOpMode != FileOpNone {
		inputBarHeight = 1
		// Add extra line for error message if present
		if p.fileOpMode != FileOpNone && p.fileOpError != "" {
			inputBarHeight = 2
		}
	}

	// Calculate pane height: total - input bar - pane border (2 lines)
	// Note: footer is rendered by the app, not by the plugin
	paneHeight := p.height - inputBarHeight - 2
	if paneHeight < 4 {
		paneHeight = 4
	}

	// Inner content height = pane height - header lines (2)
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	treeContent := p.renderTreePane(innerHeight)
	previewContent := p.renderPreviewPane(innerHeight)

	// Apply styles
	leftPane := treeBorder.
		Width(p.treeWidth).
		Height(paneHeight).
		Render(treeContent)

	rightPane := previewBorder.
		Width(p.previewWidth).
		Height(paneHeight).
		Render(previewContent)

	// Join panes horizontally
	panes := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	// Build final layout
	var parts []string

	// Add search bar if in tree search mode
	if p.searchMode {
		parts = append(parts, p.renderSearchBar())
	}

	// Add content search bar if in content search mode
	if p.contentSearchMode {
		parts = append(parts, p.renderContentSearchBar())
	}

	// Add file operation bar if in file operation mode
	if p.fileOpMode != FileOpNone {
		parts = append(parts, p.renderFileOpBar())
	}

	parts = append(parts, panes)

	return lipgloss.JoinVertical(lipgloss.Top, parts...)
}

// renderContentSearchBar renders the content search input bar for preview pane.
func (p *Plugin) renderContentSearchBar() string {
	// Show cursor while typing, hide when committed
	cursor := ""
	if !p.contentSearchCommitted {
		cursor = "█"
	}

	matchInfo := ""
	if len(p.contentSearchMatches) > 0 {
		matchInfo = fmt.Sprintf(" (%d/%d)", p.contentSearchCursor+1, len(p.contentSearchMatches))
		if p.contentSearchCommitted {
			matchInfo += " [n/N j/k]" // Hint for navigation
		}
	} else if p.contentSearchQuery != "" {
		matchInfo = " (0 matches)"
	}

	searchLine := fmt.Sprintf(" / %s%s%s", p.contentSearchQuery, cursor, matchInfo)
	return styles.ModalTitle.Render(searchLine)
}

// renderSearchBar renders the search input bar.
func (p *Plugin) renderSearchBar() string {
	cursor := "█"
	matchInfo := ""
	if len(p.searchMatches) > 0 {
		matchInfo = fmt.Sprintf(" (%d/%d)", p.searchCursor+1, len(p.searchMatches))
	} else if p.searchQuery != "" {
		matchInfo = " (no matches)"
	}

	searchLine := fmt.Sprintf(" / %s%s%s", p.searchQuery, cursor, matchInfo)
	return styles.ModalTitle.Render(searchLine)
}

// renderFileOpBar renders the file operation input bar (move/rename).
func (p *Plugin) renderFileOpBar() string {
	// Handle confirmation mode for directory creation
	if p.fileOpConfirmCreate {
		confirmLine := fmt.Sprintf(" Create '%s'? [y]es / [n]o", p.fileOpConfirmPath)
		return styles.ModalTitle.Render(confirmLine)
	}

	var prompt string
	switch p.fileOpMode {
	case FileOpRename:
		prompt = "Rename: "
	case FileOpMove:
		prompt = "Move to: "
	default:
		return ""
	}

	inputLine := fmt.Sprintf(" %s%s", prompt, p.fileOpTextInput.View())

	if p.fileOpError != "" {
		errorLine := styles.StatusDeleted.Render(" " + p.fileOpError)
		return lipgloss.JoinVertical(lipgloss.Left,
			styles.ModalTitle.Render(inputLine),
			errorLine,
		)
	}
	return styles.ModalTitle.Render(inputLine)
}

// renderTreePane renders the file tree in the left pane.
func (p *Plugin) renderTreePane(visibleHeight int) string {
	var sb strings.Builder

	// Header
	header := styles.Title.Render("Files")
	sb.WriteString(header)
	sb.WriteString("\n\n")

	if p.tree == nil || p.tree.Len() == 0 {
		sb.WriteString(styles.Muted.Render("No files"))
		return sb.String()
	}

	end := p.treeScrollOff + visibleHeight
	if end > p.tree.Len() {
		end = p.tree.Len()
	}

	for i := p.treeScrollOff; i < end; i++ {
		node := p.tree.GetNode(i)
		if node == nil {
			continue
		}

		selected := i == p.treeCursor
		maxWidth := p.treeWidth - 4 // Account for border padding
		line := p.renderTreeNode(node, selected, maxWidth)

		sb.WriteString(line)
		// Don't add newline after last line
		if i < end-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// renderTreeNode renders a single tree node.
func (p *Plugin) renderTreeNode(node *FileNode, selected bool, maxWidth int) string {
	// Indentation
	indent := strings.Repeat("  ", node.Depth)

	// Icon for directories
	icon := "  "
	if node.IsDir {
		if node.IsExpanded {
			icon = "> "
		} else {
			icon = "+ "
		}
	}

	// Calculate available width for name (after indent and icon)
	prefixLen := len(indent) + len(icon)
	availableWidth := maxWidth - prefixLen
	if availableWidth < 3 {
		availableWidth = 3
	}

	// Truncate name before styling to avoid cutting ANSI escape codes
	displayName := node.Name
	if len(displayName) > availableWidth {
		displayName = displayName[:availableWidth-1] + "…"
	}

	// Name styling
	var name string
	if node.IsDir {
		name = styles.FileBrowserDir.Render(displayName)
	} else if node.IsIgnored {
		name = styles.FileBrowserIgnored.Render(displayName)
	} else {
		name = styles.FileBrowserFile.Render(displayName)
	}

	line := fmt.Sprintf("%s%s%s", indent, styles.FileBrowserIcon.Render(icon), name)

	if selected {
		return styles.ListItemSelected.Render(line)
	}
	return line
}

// renderPreviewPane renders the file preview in the right pane.
func (p *Plugin) renderPreviewPane(visibleHeight int) string {
	var sb strings.Builder

	// Header
	header := "Preview"
	if p.previewFile != "" {
		header = truncatePath(p.previewFile, p.previewWidth-4)
	}
	sb.WriteString(styles.Title.Render(header))
	sb.WriteString("\n\n")

	if p.previewFile == "" {
		sb.WriteString(styles.Muted.Render("Select a file to preview"))
		return sb.String()
	}

	if p.previewError != nil {
		sb.WriteString(styles.StatusDeleted.Render(p.previewError.Error()))
		return sb.String()
	}

	if p.isBinary {
		sb.WriteString(styles.Muted.Render("Binary file"))
		return sb.String()
	}

	// Use highlighted lines if available
	lines := p.previewHighlighted
	if len(lines) == 0 {
		lines = p.previewLines
	}

	start := p.previewScroll
	end := start + visibleHeight
	if end > len(lines) {
		end = len(lines)
	}

	// Calculate max line width (pane width - line number - padding)
	lineNumWidth := 5 // "1234 " = 5 chars
	maxLineWidth := p.previewWidth - lineNumWidth - 4
	if maxLineWidth < 10 {
		maxLineWidth = 10
	}

	// Style for truncating lines with ANSI codes
	lineStyle := lipgloss.NewStyle().MaxWidth(maxLineWidth)

	// Reserve 1 line for truncation message if needed
	contentEnd := end
	if p.isTruncated && end-start > 1 {
		contentEnd = end - 1
	}

	for i := start; i < contentEnd; i++ {
		lineNum := styles.FileBrowserLineNumber.Render(fmt.Sprintf("%4d ", i+1))

		// Get line content - apply match highlighting if in content search mode
		var lineContent string
		if p.contentSearchMode && len(p.contentSearchMatches) > 0 {
			// Use raw lines for highlighting (loses syntax highlighting on matched lines)
			lineContent = p.highlightLineMatches(i)
		} else if i < len(lines) {
			lineContent = lines[i]
		}

		line := lineStyle.Render(lineContent)

		sb.WriteString(lineNum)
		sb.WriteString(line)
		// Don't add newline after last line
		if i < contentEnd-1 || p.isTruncated {
			sb.WriteString("\n")
		}
	}

	if p.isTruncated {
		sb.WriteString(styles.Muted.Render("... (file truncated)"))
	}

	return sb.String()
}

// highlightLineMatches applies search match highlighting to a line.
func (p *Plugin) highlightLineMatches(lineNo int) string {
	// Get raw line (not syntax highlighted)
	if lineNo >= len(p.previewLines) {
		return ""
	}
	rawLine := p.previewLines[lineNo]

	// Find all matches on this line
	type lineMatch struct {
		matchIdx int // Index in contentSearchMatches (for current detection)
		startCol int
		endCol   int
	}
	var lineMatches []lineMatch

	for i, m := range p.contentSearchMatches {
		if m.LineNo == lineNo {
			lineMatches = append(lineMatches, lineMatch{
				matchIdx: i,
				startCol: m.StartCol,
				endCol:   m.EndCol,
			})
		}
	}

	if len(lineMatches) == 0 {
		// No matches on this line, use syntax highlighted version if available
		if lineNo < len(p.previewHighlighted) {
			return p.previewHighlighted[lineNo]
		}
		return rawLine
	}

	// Build highlighted line from raw text
	var result strings.Builder
	lastEnd := 0

	for _, m := range lineMatches {
		if m.startCol > len(rawLine) || m.endCol > len(rawLine) {
			continue
		}
		if m.startCol < lastEnd {
			continue // Overlapping match, skip
		}

		// Add text before match
		if m.startCol > lastEnd {
			result.WriteString(rawLine[lastEnd:m.startCol])
		}

		// Apply highlight style (current match vs other matches)
		matchText := rawLine[m.startCol:m.endCol]
		if m.matchIdx == p.contentSearchCursor {
			result.WriteString(styles.SearchMatchCurrent.Render(matchText))
		} else {
			result.WriteString(styles.SearchMatch.Render(matchText))
		}
		lastEnd = m.endCol
	}

	// Add remaining text
	if lastEnd < len(rawLine) {
		result.WriteString(rawLine[lastEnd:])
	}

	return result.String()
}

// truncatePath shortens a path to fit width.
func truncatePath(path string, maxWidth int) string {
	if len(path) <= maxWidth {
		return path
	}
	if maxWidth < 10 {
		return path[:maxWidth]
	}
	// Show ...end of path
	return "..." + path[len(path)-maxWidth+3:]
}

// renderQuickOpenModal renders the quick open overlay.
func (p *Plugin) renderQuickOpenModal() string {
	// Modal dimensions
	modalWidth := p.width - 4
	if modalWidth > 80 {
		modalWidth = 80
	}
	if modalWidth < 30 {
		modalWidth = 30
	}

	// Calculate max visible items based on available height
	// Leave room for: header (2 lines), footer (2 lines), border (2 lines), some padding
	maxListHeight := p.height - 8
	if maxListHeight < 5 {
		maxListHeight = 5
	}
	if maxListHeight > 20 {
		maxListHeight = 20
	}

	var sb strings.Builder

	// Header with search input
	cursor := "█"
	header := fmt.Sprintf("Quick Open: %s%s", p.quickOpenQuery, cursor)
	sb.WriteString(styles.ModalTitle.Render(header))
	sb.WriteString("\n\n")

	// Error message if scan was limited
	if p.quickOpenError != "" {
		sb.WriteString(styles.Muted.Render("⚠ " + p.quickOpenError))
		sb.WriteString("\n")
	}

	if len(p.quickOpenMatches) == 0 {
		if p.quickOpenQuery != "" {
			sb.WriteString(styles.Muted.Render("No matches"))
		} else {
			sb.WriteString(styles.Muted.Render("Type to search files..."))
		}
	} else {
		// Determine visible range (scroll if cursor out of view)
		listHeight := maxListHeight
		if listHeight > len(p.quickOpenMatches) {
			listHeight = len(p.quickOpenMatches)
		}

		start := 0
		if p.quickOpenCursor >= listHeight {
			start = p.quickOpenCursor - listHeight + 1
		}
		end := start + listHeight
		if end > len(p.quickOpenMatches) {
			end = len(p.quickOpenMatches)
		}

		for i := start; i < end; i++ {
			match := p.quickOpenMatches[i]
			isSelected := i == p.quickOpenCursor

			// Build the display line with highlighted match chars
			line := p.renderQuickOpenMatch(match, modalWidth-4)

			if isSelected {
				sb.WriteString(styles.QuickOpenItemSelected.Render("> " + line))
			} else {
				sb.WriteString(styles.QuickOpenItem.Render("  " + line))
			}

			if i < end-1 {
				sb.WriteString("\n")
			}
		}
	}

	// Footer with match count
	if len(p.quickOpenMatches) > 0 {
		sb.WriteString(fmt.Sprintf("\n\n%s", styles.Muted.Render(fmt.Sprintf("(%d/%d)", p.quickOpenCursor+1, len(p.quickOpenMatches)))))
	} else if len(p.quickOpenFiles) > 0 {
		sb.WriteString(fmt.Sprintf("\n\n%s", styles.Muted.Render(fmt.Sprintf("(%d files)", len(p.quickOpenFiles)))))
	}

	// Wrap in modal box and center horizontally
	content := sb.String()
	modal := styles.ModalBox.
		Width(modalWidth).
		Render(content)

	// Center horizontally, position near top
	hPad := (p.width - modalWidth - 4) / 2
	if hPad < 0 {
		hPad = 0
	}

	centered := lipgloss.NewStyle().
		PaddingLeft(hPad).
		PaddingTop(2).
		Render(modal)

	return centered
}

// renderQuickOpenMatch renders a single match with highlighted chars.
func (p *Plugin) renderQuickOpenMatch(match QuickOpenMatch, maxWidth int) string {
	path := match.Path

	// Truncate path if too long
	if len(path) > maxWidth {
		path = "..." + path[len(path)-maxWidth+3:]
		// Can't highlight properly after truncation, just return
		return path
	}

	// Apply match highlighting
	if len(match.MatchRanges) > 0 {
		return p.highlightFuzzyMatch(path, match.MatchRanges)
	}

	return path
}

// highlightFuzzyMatch applies highlighting to matched character ranges.
func (p *Plugin) highlightFuzzyMatch(text string, ranges []MatchRange) string {
	if len(ranges) == 0 {
		return text
	}

	var result strings.Builder
	lastEnd := 0

	for _, r := range ranges {
		if r.Start > len(text) || r.End > len(text) {
			continue
		}
		if r.Start < lastEnd {
			continue // Skip overlapping
		}

		// Add text before match
		if r.Start > lastEnd {
			result.WriteString(text[lastEnd:r.Start])
		}

		// Add highlighted match
		result.WriteString(styles.FuzzyMatchChar.Render(text[r.Start:r.End]))
		lastEnd = r.End
	}

	// Add remaining text
	if lastEnd < len(text) {
		result.WriteString(text[lastEnd:])
	}

	return result.String()
}
