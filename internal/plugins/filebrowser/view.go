package filebrowser

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/image"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// ansiResetRe matches ANSI reset sequences (both \x1b[0m and \x1b[m)
var ansiResetRe = regexp.MustCompile(`\x1b\[0?m`)

// getSelectionBgANSI returns the ANSI 24-bit background code for selection highlight
// based on the current theme's BgTertiary color.
func getSelectionBgANSI() string {
	// Get BgTertiary hex color from theme and convert to ANSI escape
	theme := styles.GetCurrentTheme()
	hex := theme.Colors.BgTertiary
	// Parse hex color (assumes #RRGGBB format)
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		// Fallback to default dark gray background if parsing fails
		r, g, b = 55, 65, 81
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

// injectSelectionBackground injects selection background color into an ANSI-styled string.
// It prepends the background at the start and re-injects after any reset sequences.
func injectSelectionBackground(s string) string {
	selectionBg := getSelectionBgANSI()
	// Prepend background color
	result := selectionBg + s
	// Re-inject background after any reset sequences
	result = ansiResetRe.ReplaceAllString(result, "${0}"+selectionBg)
	// Append reset at end to not bleed into next line
	return result + "\x1b[0m"
}

// FocusPane represents which pane is active.
type FocusPane int

const (
	PaneTree FocusPane = iota
	PanePreview
)

// dividerWidth is the width of the draggable divider between panes.
const dividerWidth = 1

// calculatePaneWidths sets the tree and preview pane widths.
// If treeWidth is already set (from drag), only updates previewWidth.
func (p *Plugin) calculatePaneWidths() {
	// RenderPanel handles borders internally, so only subtract divider
	available := p.width - dividerWidth

	// Only set default treeWidth if not yet initialized
	if p.treeWidth == 0 {
		p.treeWidth = available * 30 / 100
	}

	// Clamp treeWidth to valid bounds
	minWidth := 20
	maxWidth := available - 40 // Leave at least 40 for preview
	if maxWidth < minWidth {
		maxWidth = minWidth
	}
	if p.treeWidth < minWidth {
		p.treeWidth = minWidth
	} else if p.treeWidth > maxWidth {
		p.treeWidth = maxWidth
	}

	// Calculate previewWidth from remaining space
	p.previewWidth = available - p.treeWidth
	if p.previewWidth < 40 {
		p.previewWidth = 40
	}
}

// renderView creates the 2-pane layout.
func (p *Plugin) renderView() string {
	// Clear mouse hit regions at start of each render
	p.mouseHandler.Clear()

	// Project search is a full overlay - render modal over dimmed background
	if p.projectSearchMode {
		background := p.renderNormalPanes()
		modal := p.renderProjectSearchModalContent()
		return ui.OverlayModal(background, modal, p.width, p.height)
	}

	// Quick open is a full overlay - render modal over dimmed background
	if p.quickOpenMode {
		background := p.renderNormalPanes()
		modal := p.renderQuickOpenModalContent()
		return ui.OverlayModal(background, modal, p.width, p.height)
	}

	// Info modal is a full overlay - render modal over dimmed background
	if p.infoMode {
		background := p.renderNormalPanes()
		modal := p.renderInfoModalContent()
		return ui.OverlayModal(background, modal, p.width, p.height)
	}

	return p.renderNormalPanes()
}

// renderNormalPanes renders the standard 2-pane layout without modals.
func (p *Plugin) renderNormalPanes() string {
	// Account for input bar if active (content search or file op)
	// Note: tree search bar is rendered inside the tree pane, not here
	inputBarHeight := 0
	if p.contentSearchMode || p.fileOpMode != FileOpNone {
		inputBarHeight = 1
		// Add extra line for error message if present
		if p.fileOpMode != FileOpNone && p.fileOpError != "" {
			inputBarHeight = 2
		}
	}

	// Pane height for panels (outer dimensions including borders)
	// Note: footer is rendered by the app, not by the plugin
	paneHeight := p.height - inputBarHeight
	if paneHeight < 4 {
		paneHeight = 4
	}

	// Inner content height (excluding borders and header lines)
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Handle collapsed tree - render full-width preview pane
	if !p.treeVisible {
		previewWidth := p.width - 2 // Account for borders
		if previewWidth < 40 {
			previewWidth = 40
		}

		previewContent := p.renderPreviewPane(innerHeight)
		rightPane := styles.RenderPanel(previewContent, previewWidth, paneHeight, true)

		// Build final layout
		var parts []string

		// Add content search bar if in content search mode
		if p.contentSearchMode {
			parts = append(parts, p.renderContentSearchBar())
		}

		// Add file operation bar if in file operation mode
		if p.fileOpMode != FileOpNone {
			parts = append(parts, p.renderFileOpBar())
		}

		parts = append(parts, rightPane)

		// Update hit regions for collapsed state
		p.mouseHandler.Clear()
		p.mouseHandler.HitMap.AddRect(regionPreviewPane, 0, inputBarHeight, previewWidth, paneHeight, nil)

		return lipgloss.JoinVertical(lipgloss.Left, parts...)
	}

	p.calculatePaneWidths()

	// Determine if panes are active based on focus
	treeActive := p.activePane == PaneTree && !p.searchMode && !p.contentSearchMode
	previewActive := p.activePane == PanePreview && !p.searchMode && !p.contentSearchMode

	treeContent := p.renderTreePane(innerHeight)
	previewContent := p.renderPreviewPane(innerHeight)

	// Apply gradient border styles
	leftPane := styles.RenderPanel(treeContent, p.treeWidth, paneHeight, treeActive)
	rightPane := styles.RenderPanel(previewContent, p.previewWidth, paneHeight, previewActive)

	// Render visible divider between panes
	// MarginTop(1) in renderDivider shifts it down, so use paneHeight directly
	divider := p.renderDivider(paneHeight)

	// Join panes horizontally with divider in between
	panes := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, divider, rightPane)

	// Build final layout
	var parts []string

	// Note: tree search bar is rendered inside renderTreePane(), not here

	// Add content search bar if in content search mode
	if p.contentSearchMode {
		parts = append(parts, p.renderContentSearchBar())
	}

	// Add file operation bar if in file operation mode
	if p.fileOpMode != FileOpNone {
		parts = append(parts, p.renderFileOpBar())
	}

	parts = append(parts, panes)

	// Register mouse hit regions for panes
	// Panes start after any input bars
	paneY := inputBarHeight
	treeItemY := paneY + 3 // border(1) + header(2)

	// Register pane regions - tested in reverse order (last added = highest priority)
	// Tree pane region (x=0, full width) - lowest priority fallback
	p.mouseHandler.HitMap.AddRect(regionTreePane, 0, paneY, p.treeWidth, paneHeight, nil)

	// Preview pane region (after divider) - medium priority
	previewX := p.treeWidth + dividerWidth
	p.mouseHandler.HitMap.AddRect(regionPreviewPane, previewX, paneY, p.previewWidth, paneHeight, nil)

	// Pane divider region - HIGH PRIORITY (registered after panes so it wins in overlap)
	// Left pane is Width(treeWidth), so occupies columns 0 to treeWidth-1
	// Divider is at column treeWidth
	// Hit region is wider for easier clicking
	dividerX := p.treeWidth
	dividerHitWidth := 3
	p.mouseHandler.HitMap.AddRect(regionPaneDivider, dividerX, paneY, dividerHitWidth, paneHeight, nil)

	// Register individual tree items LAST (tested first = higher priority)
	// Note: regions are tested in reverse order, so items added last take precedence
	if p.tree != nil && p.tree.Len() > 0 {
		end := p.treeScrollOff + innerHeight
		if end > p.tree.Len() {
			end = p.tree.Len()
		}
		for i := p.treeScrollOff; i < end; i++ {
			itemY := treeItemY + (i - p.treeScrollOff)
			// Register region: x=1 (inside border), width=treeWidth-2, height=1, data=tree index
			p.mouseHandler.HitMap.AddRect(regionTreeItem, 1, itemY, p.treeWidth-2, 1, i)
		}
	}

	// Register individual preview lines for text selection (LAST for highest priority)
	if p.previewFile != "" && !p.isBinary && len(p.previewLines) > 0 {
		previewContentStartY := paneY + 3 // border(1) + header(2 lines: title + metadata)
		contentStart := p.previewScroll
		contentEnd := contentStart + innerHeight
		if contentEnd > len(p.previewLines) {
			contentEnd = len(p.previewLines)
		}
		for i := contentStart; i < contentEnd; i++ {
			lineY := previewContentStartY + (i - contentStart)
			// Region covers content area within preview pane
			p.mouseHandler.HitMap.AddRect(regionPreviewLine, previewX+1, lineY, p.previewWidth-2, 1, i)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Top, parts...)
}

// renderDivider renders the vertical divider between panes.
func (p *Plugin) renderDivider(height int) string {
	// Use a subtle vertical bar as the divider
	// MarginTop(1) shifts it down to align with pane content (below top border)
	dividerStyle := lipgloss.NewStyle().
		Foreground(styles.BorderNormal).
		MarginTop(1)

	// Build vertical bar with exact height
	var sb strings.Builder
	for i := 0; i < height; i++ {
		sb.WriteString("│")
		if i < height-1 {
			sb.WriteString("\n")
		}
	}

	return dividerStyle.Render(sb.String())
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

// renderSearchBar renders the search input bar (legacy - for external use).
func (p *Plugin) renderSearchBar() string {
	return p.renderTreeSearchBar()
}

// renderTreeSearchBar renders the tree search bar inline within the tree pane.
func (p *Plugin) renderTreeSearchBar() string {
	cursor := "█"
	matchInfo := ""
	if len(p.searchMatches) > 0 {
		matchInfo = fmt.Sprintf(" (%d/%d)", p.searchCursor+1, len(p.searchMatches))
	} else if p.searchQuery != "" {
		matchInfo = " (no matches)"
	}

	searchLine := fmt.Sprintf("/%s%s%s", p.searchQuery, cursor, matchInfo)
	// Use a subtle style that fits inside the pane
	return styles.StatusInProgress.Render(searchLine)
}

// renderFileOpBar renders the file operation input bar (move/rename/create/delete).
func (p *Plugin) renderFileOpBar() string {
	// Handle delete confirmation mode
	if p.fileOpConfirmDelete && p.fileOpTarget != nil {
		itemType := "file"
		if p.fileOpTarget.IsDir {
			itemType = "directory"
		}
		return p.renderFileOpConfirmation(fmt.Sprintf("Delete %s '%s'?", itemType, p.fileOpTarget.Name))
	}

	// Handle confirmation mode for directory creation (during move)
	if p.fileOpConfirmCreate {
		return p.renderFileOpConfirmation(fmt.Sprintf("Create '%s'?", p.fileOpConfirmPath))
	}

	var prompt string
	switch p.fileOpMode {
	case FileOpRename:
		prompt = "Rename: "
	case FileOpMove:
		prompt = "Move to: "
	case FileOpCreateFile:
		prompt = "New file: "
	case FileOpCreateDir:
		prompt = "New dir: "
	default:
		return ""
	}

	inputLine := fmt.Sprintf(" %s%s", prompt, p.fileOpTextInput.View())

	var lines []string
	lines = append(lines, styles.ModalTitle.Render(inputLine))

	if p.fileOpError != "" {
		lines = append(lines, styles.StatusDeleted.Render(" "+p.fileOpError))
	}

	// Show suggestion dropdown for move mode
	if p.fileOpMode == FileOpMove && p.fileOpShowSuggestions && len(p.fileOpSuggestions) > 0 {
		lines = append(lines, p.renderFileOpSuggestions())
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderFileOpConfirmation renders a confirmation dialog with Yes/No buttons.
func (p *Plugin) renderFileOpConfirmation(message string) string {
	// Clear existing hit regions for buttons
	p.mouseHandler.HitMap.Clear()

	var sb strings.Builder
	sb.WriteString(" ")
	sb.WriteString(message)
	sb.WriteString("  ")

	// Calculate button positions (approximate, based on rendered text)
	// We use tree pane width as reference since bar is rendered in tree pane
	baseX := 1 + len(message) + 2 // space + message + spacing

	// Yes button
	yesBtn := "Yes"
	if p.fileOpButtonHover == 1 {
		sb.WriteString(styles.ButtonHover.Render(yesBtn))
	} else if p.fileOpButtonFocus == 1 {
		sb.WriteString(styles.ButtonFocused.Render(yesBtn))
	} else {
		sb.WriteString(styles.Button.Render(yesBtn))
	}
	yesWidth := len(yesBtn) + 4 // Padding adds 2 on each side
	p.mouseHandler.HitMap.AddRect(regionFileOpConfirm, baseX, 0, yesWidth, 1, nil)

	sb.WriteString(" ")
	baseX += yesWidth + 1

	// No button
	noBtn := "No"
	if p.fileOpButtonHover == 2 {
		sb.WriteString(styles.ButtonHover.Render(noBtn))
	} else if p.fileOpButtonFocus == 2 {
		sb.WriteString(styles.ButtonFocused.Render(noBtn))
	} else {
		sb.WriteString(styles.Button.Render(noBtn))
	}
	noWidth := len(noBtn) + 4
	p.mouseHandler.HitMap.AddRect(regionFileOpCancel, baseX, 0, noWidth, 1, nil)

	return styles.ModalTitle.Render(sb.String())
}

// renderFileOpSuggestions renders the path suggestion dropdown.
func (p *Plugin) renderFileOpSuggestions() string {
	var lines []string

	for i, suggestion := range p.fileOpSuggestions {
		line := " " + suggestion
		if i == p.fileOpSuggestionIdx {
			line = styles.ListItemSelected.Render(line)
		} else {
			line = styles.Muted.Render(line)
		}
		lines = append(lines, line)

		// Register hit region for this suggestion (Y offset = line index + 1 for input bar)
		p.mouseHandler.HitMap.AddRect(regionFileOpSuggestion, 0, i+1, p.treeWidth, 1, i)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderTreePane renders the file tree in the left pane.
func (p *Plugin) renderTreePane(visibleHeight int) string {
	var sb strings.Builder

	// Header with sort mode indicator
	header := styles.Title.Render("Files")
	sb.WriteString(header)
	if p.tree != nil {
		sb.WriteString("  ")
		sb.WriteString(styles.Muted.Render("[" + p.tree.SortMode.Label() + "]"))
	}
	sb.WriteString("\n")

	// Search bar (if in search mode) - rendered inside the pane like conversations plugin
	if p.searchMode {
		searchLine := p.renderTreeSearchBar()
		sb.WriteString(searchLine)
		sb.WriteString("\n")
	} else {
		sb.WriteString("\n") // Empty line when not searching
	}

	// In search mode, show filtered results instead of full tree
	if p.searchMode {
		if len(p.searchMatches) > 0 {
			return p.renderSearchResults(&sb, visibleHeight)
		} else if p.searchQuery != "" {
			// Show "no matches" when query exists but no results
			sb.WriteString(styles.Muted.Render("No matching files"))
			return sb.String()
		}
		// Empty query - fall through to show full tree
	}

	if p.tree == nil || p.tree.Len() == 0 {
		sb.WriteString(styles.Muted.Render("No files"))
		return sb.String()
	}

	// visibleHeight already accounts for header (2 lines) in renderNormalPanes()
	// So we use it directly - no additional subtraction needed
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

// renderSearchResults renders the filtered search results list.
func (p *Plugin) renderSearchResults(sb *strings.Builder, visibleHeight int) string {
	maxWidth := p.treeWidth - 4

	// Calculate scroll offset for search results
	searchScrollOff := 0
	if p.searchCursor >= visibleHeight {
		searchScrollOff = p.searchCursor - visibleHeight + 1
	}

	end := searchScrollOff + visibleHeight
	if end > len(p.searchMatches) {
		end = len(p.searchMatches)
	}

	for i := searchScrollOff; i < end; i++ {
		node := p.searchMatches[i]
		selected := i == p.searchCursor

		// Show full path for search results (more useful than just name)
		displayPath := node.Path
		if len(displayPath) > maxWidth-2 {
			displayPath = "…" + displayPath[len(displayPath)-maxWidth+3:]
		}

		if selected {
			// Full-width highlight for selected item
			if len(displayPath) < maxWidth {
				displayPath += strings.Repeat(" ", maxWidth-len(displayPath))
			}
			sb.WriteString(styles.ListItemSelected.Render(displayPath))
		} else {
			// Style based on file type
			if node.IsDir {
				sb.WriteString(styles.FileBrowserDir.Render(displayPath))
			} else if node.IsIgnored {
				sb.WriteString(styles.FileBrowserIgnored.Render(displayPath))
			} else {
				sb.WriteString(styles.FileBrowserFile.Render(displayPath))
			}
		}

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
		// Build plain text version for full-width highlight
		plainLine := indent + icon + displayName
		// Pad to full width
		if len(plainLine) < maxWidth {
			plainLine += strings.Repeat(" ", maxWidth-len(plainLine))
		}
		return styles.ListItemSelected.Render(plainLine)
	}
	return line
}

// renderPreviewPane renders the file preview in the right pane.
func (p *Plugin) renderPreviewPane(visibleHeight int) string {
	var sb strings.Builder

	// Header with file path
	header := "Preview"
	if p.previewFile != "" {
		header = truncatePath(p.previewFile, p.previewWidth-4)
		// Add markdown render indicator
		if p.isMarkdownFile() && p.markdownRenderMode {
			header += " [rendered]"
		}
	}
	sb.WriteString(styles.Title.Render(header))

	// Metadata line (size, mod time, permissions)
	if p.previewFile != "" && p.previewSize > 0 {
		meta := fmt.Sprintf("%s  %s  %s",
			formatSize(p.previewSize),
			p.previewModTime.Format("Jan 2 15:04"),
			p.previewMode.String(),
		)
		sb.WriteString("  ")
		sb.WriteString(styles.Muted.Render(meta))
	}
	sb.WriteString("\n\n")

	if p.previewFile == "" {
		sb.WriteString(styles.Muted.Render("Select a file to preview"))
		return sb.String()
	}

	if p.previewError != nil {
		sb.WriteString(styles.StatusDeleted.Render(p.previewError.Error()))
		return sb.String()
	}

	// Handle image preview
	if p.isImage {
		sb.WriteString(p.renderImagePreview())
		return sb.String()
	}

	if p.isBinary {
		sb.WriteString(styles.Muted.Render("Binary file"))
		return sb.String()
	}

	// Determine which lines to display
	var lines []string
	showLineNumbers := true

	// Use markdown-rendered lines if in render mode for markdown files
	if p.markdownRenderMode && p.isMarkdownFile() && len(p.markdownRendered) > 0 {
		lines = p.markdownRendered
		showLineNumbers = false // Glamour output doesn't map 1:1 to source lines
	} else if len(p.previewHighlighted) > 0 {
		lines = p.previewHighlighted
	} else {
		lines = p.previewLines
	}

	start := p.previewScroll
	end := start + visibleHeight
	if end > len(lines) {
		end = len(lines)
	}

	// Calculate max line width (pane width - line number - padding)
	lineNumWidth := 5 // "1234 " = 5 chars
	if !showLineNumbers {
		lineNumWidth = 0
	}
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
		// Check if this line is selected for text selection highlighting
		if p.isLineSelected(i) && showLineNumbers {
			// Line number with selection background
			lineNumStr := fmt.Sprintf("%4d ", i+1)
			sb.WriteString(injectSelectionBackground(lineNumStr))

			// Get syntax-highlighted content and inject selection background
			var lineContent string
			if i < len(lines) {
				lineContent = lines[i]
			}
			// Truncate using lipgloss (handles ANSI codes properly)
			lineContent = lipgloss.NewStyle().MaxWidth(maxLineWidth).Render(lineContent)
			sb.WriteString(injectSelectionBackground(lineContent))

			// Pad remaining width with selection background
			contentWidth := lipgloss.Width(lineNumStr) + lipgloss.Width(lineContent)
			if contentWidth < p.previewWidth-4 {
				padding := strings.Repeat(" ", p.previewWidth-4-contentWidth)
				sb.WriteString(injectSelectionBackground(padding))
			}
		} else {
			// Get line content
			var lineContent string
			if p.contentSearchMode && len(p.contentSearchMatches) > 0 && showLineNumbers {
				// Use raw lines for highlighting (loses syntax highlighting on matched lines)
				lineContent = p.highlightLineMatches(i)
			} else if i < len(lines) {
				lineContent = lines[i]
			}

			line := lineStyle.Render(lineContent)

			// Render with or without line numbers
			if showLineNumbers {
				lineNum := styles.FileBrowserLineNumber.Render(fmt.Sprintf("%4d ", i+1))
				sb.WriteString(lineNum)
			}
			sb.WriteString(line)
		}

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

// truncatePath shortens a path to fit width (rune-based for Unicode safety).
func truncatePath(path string, maxWidth int) string {
	runes := []rune(path)
	if len(runes) <= maxWidth {
		return path
	}
	if maxWidth < 10 {
		return string(runes[:maxWidth])
	}
	// Show ...end of path
	return "..." + string(runes[len(runes)-maxWidth+3:])
}

// formatSize formats a file size in human-readable form.
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// renderQuickOpenModalContent renders the quick open modal box content.
func (p *Plugin) renderQuickOpenModalContent() string {
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

	// Calculate modal position for hit region registration
	hPad := (p.width - modalWidth - 4) / 2
	if hPad < 0 {
		hPad = 0
	}
	modalX := hPad + 1  // +1 for modal border
	modalItemY := 2 + 3 // paddingTop(2) + border(1) + header(2)
	if p.quickOpenError != "" {
		modalItemY++ // Extra line for error message
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

			// Register hit region for this quick open item
			itemY := modalItemY + (i - start)
			p.mouseHandler.HitMap.AddRect(regionQuickOpen, modalX, itemY, modalWidth-2, 1, i)

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

	// Wrap in modal box (centering handled by overlayModal)
	content := sb.String()
	return styles.ModalBox.
		Width(modalWidth).
		Render(content)
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

// renderImagePreview renders image preview or fallback message.
func (p *Plugin) renderImagePreview() string {
	// Calculate available dimensions (subtract border + padding = 4)
	contentHeight := p.height - 4
	contentWidth := p.previewWidth - 4
	if contentWidth < 10 {
		contentWidth = 10
	}
	if contentHeight < 5 {
		contentHeight = 5
	}

	// Get full path for rendering
	fullPath := filepath.Join(p.tree.RootDir, p.previewFile)

	// Render image
	result, err := p.imageRenderer.Render(fullPath, contentWidth, contentHeight)
	if err != nil {
		return styles.Muted.Render(fmt.Sprintf("Image error: %v", err))
	}

	// Cache result for resize detection
	p.imageResult = result

	if result.IsFallback {
		// Show informative fallback message
		ext := filepath.Ext(p.previewFile)
		msg := fmt.Sprintf("Image file (%s)", ext)

		if result.Content != "" {
			// Custom fallback message (e.g., "too large")
			msg = result.Content
		}

		hint := "Preview in: " + image.SupportedTerminals()

		return lipgloss.JoinVertical(lipgloss.Center,
			styles.Muted.Render(msg),
			"",
			styles.Muted.Render(hint),
		)
	}

	return result.Content
}
