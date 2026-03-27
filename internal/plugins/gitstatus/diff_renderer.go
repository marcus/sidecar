package gitstatus

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// DiffViewMode specifies the diff rendering mode.
type DiffViewMode int

const (
	DiffViewUnified    DiffViewMode = iota // Line-by-line unified view
	DiffViewSideBySide                     // Side-by-side split view
)

// Additional styles for enhanced diff rendering
var (
	wordDiffAddStyle = lipgloss.NewStyle().
				Foreground(styles.Success).
				Background(styles.DiffAddBg).
				Bold(true)

	wordDiffRemoveStyle = lipgloss.NewStyle().
				Foreground(styles.Error).
				Background(styles.DiffRemoveBg).
				Bold(true)

	hunkHeaderStyle = lipgloss.NewStyle().
			Foreground(styles.Info).
			Background(styles.BgSecondary).
			Bold(true)

	sideBySideBorder = lipgloss.NewStyle().
				Foreground(styles.BorderNormal)

	fileHeaderStyle = lipgloss.NewStyle().
			Foreground(styles.TextPrimary).
			Background(styles.BgTertiary).
			Bold(true)
)

// RenderLineDiff renders a parsed diff in unified line-by-line format with line numbers.
// horizontalOffset scrolls the content horizontally (0 = no scroll).
// highlighter is optional - if nil, no syntax highlighting is applied.
// wrapEnabled wraps long lines instead of truncating them.
func RenderLineDiff(diff *ParsedDiff, width, startLine, maxLines, horizontalOffset int, highlighter *SyntaxHighlighter, wrapEnabled bool) string {
	if diff == nil || diff.Binary {
		if diff != nil && diff.Binary {
			return styles.Muted.Render(" Binary file differs")
		}
		return styles.Muted.Render(" No diff content")
	}

	var sb strings.Builder
	lineNum := 0
	rendered := 0

	// Calculate line number width based on max line number
	maxLineNo := diff.MaxLineNumber()
	lineNoWidth := len(fmt.Sprintf("%d", maxLineNo))
	if lineNoWidth < 4 {
		lineNoWidth = 4
	}

	lineNoStyle := lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Width(lineNoWidth).
		Align(lipgloss.Right)

	contentWidth := width - (lineNoWidth*2 + 4) // Two line numbers + separators
	isFirstHunk := true

	for _, hunk := range diff.Hunks {
		// Skip until we reach the start line
		if lineNum < startLine {
			lineNum++
			if lineNum > startLine {
				// Add blank line before hunk header (except first)
				if !isFirstHunk && rendered < maxLines {
					sb.WriteString("\n")
					rendered++
				}
				// Render hunk header
				header := truncateLine(fmt.Sprintf("@@ -%d,%d +%d,%d @@%s",
					hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount, hunk.Header), contentWidth)
				sb.WriteString(hunkHeaderStyle.Render(header))
				sb.WriteString("\n")
				rendered++
				isFirstHunk = false
			}
		} else {
			// Add blank line before hunk header (except first)
			if !isFirstHunk && rendered < maxLines {
				sb.WriteString("\n")
				rendered++
			}
			// Render hunk header
			header := truncateLine(fmt.Sprintf("@@ -%d,%d +%d,%d @@%s",
				hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount, hunk.Header), contentWidth)
			sb.WriteString(hunkHeaderStyle.Render(header))
			sb.WriteString("\n")
			rendered++
			isFirstHunk = false
		}

		if rendered >= maxLines {
			break
		}

		for _, line := range hunk.Lines {
			lineNum++
			if lineNum <= startLine {
				continue
			}

			if rendered >= maxLines {
				break
			}

			// Format line numbers
			oldNo := " "
			newNo := " "
			if line.OldLineNo > 0 {
				oldNo = fmt.Sprintf("%d", line.OldLineNo)
			}
			if line.NewLineNo > 0 {
				newNo = fmt.Sprintf("%d", line.NewLineNo)
			}

			lineNos := fmt.Sprintf("%s %s │ ",
				lineNoStyle.Render(oldNo),
				lineNoStyle.Render(newNo))

			// Render content with appropriate style
			var content string
			if wrapEnabled {
				// Pass large width to avoid premature truncation; wrapping is done below
				content = renderDiffContent(line, contentWidth*10, highlighter)
				// Wrap long lines using lipgloss Width
				wrapped := lipgloss.NewStyle().Width(contentWidth).Render(content)
				wrappedLines := strings.Split(wrapped, "\n")
				lineNosPad := strings.Repeat(" ", lineNoWidth*2+4) // blank padding for continuation lines
				for wi, wl := range wrappedLines {
					if rendered >= maxLines {
						break
					}
					if wi == 0 {
						sb.WriteString(lineNos)
					} else {
						sb.WriteString(lineNosPad)
					}
					sb.WriteString(wl)
					sb.WriteString("\n")
					rendered++
				}
			} else {
				content = renderDiffContentWithOffset(line, contentWidth, horizontalOffset, highlighter)
				sb.WriteString(lineNos)
				sb.WriteString(content)
				sb.WriteString("\n")
				rendered++
			}
		}

		if rendered >= maxLines {
			break
		}
	}

	return sb.String()
}

// RenderSideBySide renders a parsed diff in side-by-side format.
// highlighter is optional - if nil, no syntax highlighting is applied.
// wrapEnabled wraps long lines instead of truncating them.
func RenderSideBySide(diff *ParsedDiff, width, startLine, maxLines, horizontalOffset int, highlighter *SyntaxHighlighter, wrapEnabled bool) string {
	if diff == nil || diff.Binary {
		if diff != nil && diff.Binary {
			return styles.Muted.Render(" Binary file differs")
		}
		return styles.Muted.Render(" No diff content")
	}

	var sb strings.Builder
	lineNum := 0
	rendered := 0

	// Calculate panel widths
	panelWidth := (width - 3) / 2 // -3 for center separator
	lineNoWidth := 5
	contentWidth := panelWidth - lineNoWidth - 2

	lineNoStyle := lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Width(lineNoWidth).
		Align(lipgloss.Right)

	isFirstHunk := true
	for _, hunk := range diff.Hunks {
		if rendered >= maxLines {
			break
		}

		// Render hunk header across both panels
		if lineNum >= startLine {
			// Add blank line before hunk header (except first)
			if !isFirstHunk && rendered < maxLines {
				sb.WriteString("\n")
				rendered++
			}
			header := fmt.Sprintf("@@ -%d,%d +%d,%d @@",
				hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount)
			sb.WriteString(hunkHeaderStyle.Render(padRight(header, width-1)))
			sb.WriteString("\n")
			rendered++
			isFirstHunk = false
		}
		lineNum++

		// Group lines into pairs (remove/add or context)
		pairs := groupLinesForSideBySide(hunk.Lines)

		for _, pair := range pairs {
			if rendered >= maxLines {
				break
			}
			if lineNum <= startLine {
				lineNum++
				continue
			}

			// Left side (old)
			leftLineNo := " "
			leftRendered := ""
			if pair.left != nil {
				if pair.left.OldLineNo > 0 {
					leftLineNo = fmt.Sprintf("%d", pair.left.OldLineNo)
				}
				if wrapEnabled {
					// Pass large width to avoid premature truncation; caller wraps via lipgloss.Width()
					leftRendered = renderSideBySideContent(pair.left.Content, pair.left.Type, contentWidth*10, highlighter)
				} else {
					// Highlight full content first to preserve syntax context, then apply offset
					leftRendered = renderSideBySideContent(pair.left.Content, pair.left.Type, contentWidth+horizontalOffset, highlighter)
					if horizontalOffset > 0 {
						leftRendered = truncateLeftCached(leftRendered, horizontalOffset)
					}
				}
			}

			// Right side (new)
			rightLineNo := " "
			rightRendered := ""
			if pair.right != nil {
				if pair.right.NewLineNo > 0 {
					rightLineNo = fmt.Sprintf("%d", pair.right.NewLineNo)
				}
				if wrapEnabled {
					// Pass large width to avoid premature truncation; caller wraps via lipgloss.Width()
					rightRendered = renderSideBySideContent(pair.right.Content, pair.right.Type, contentWidth*10, highlighter)
				} else {
					// Highlight full content first to preserve syntax context, then apply offset
					rightRendered = renderSideBySideContent(pair.right.Content, pair.right.Type, contentWidth+horizontalOffset, highlighter)
					if horizontalOffset > 0 {
						rightRendered = truncateLeftCached(rightRendered, horizontalOffset)
					}
				}
			}

			if wrapEnabled {
				// Wrap both sides and align heights
				wrapStyle := lipgloss.NewStyle().Width(contentWidth)
				leftWrapped := wrapStyle.Render(leftRendered)
				rightWrapped := wrapStyle.Render(rightRendered)
				leftLines := strings.Split(leftWrapped, "\n")
				rightLines := strings.Split(rightWrapped, "\n")
				maxH := len(leftLines)
				if len(rightLines) > maxH {
					maxH = len(rightLines)
				}
				lineNoPad := strings.Repeat(" ", lineNoWidth)
				sep := sideBySideBorder.Render(" │ ")
				for vi := 0; vi < maxH; vi++ {
					if rendered >= maxLines {
						break
					}
					lLine := ""
					if vi < len(leftLines) {
						lLine = leftLines[vi]
					}
					rLine := ""
					if vi < len(rightLines) {
						rLine = rightLines[vi]
					}
					lLine = padToWidth(lLine, contentWidth)
					rLine = padToWidth(rLine, contentWidth)
					if vi == 0 {
						sb.WriteString(fmt.Sprintf("%s │%s", lineNoStyle.Render(leftLineNo), lLine))
						sb.WriteString(sep)
						sb.WriteString(fmt.Sprintf("%s │%s", lineNoStyle.Render(rightLineNo), rLine))
					} else {
						sb.WriteString(fmt.Sprintf("%s │%s", lineNoPad, lLine))
						sb.WriteString(sep)
						sb.WriteString(fmt.Sprintf("%s │%s", lineNoPad, rLine))
					}
					sb.WriteString("\n")
					rendered++
				}
			} else {
				leftRendered = padToWidth(leftRendered, contentWidth)
				rightRendered = padToWidth(rightRendered, contentWidth)

				leftPanel := fmt.Sprintf("%s │%s",
					lineNoStyle.Render(leftLineNo),
					leftRendered)

				rightPanel := fmt.Sprintf("%s │%s",
					lineNoStyle.Render(rightLineNo),
					rightRendered)

				sb.WriteString(leftPanel)
				sb.WriteString(sideBySideBorder.Render(" │ "))
				sb.WriteString(rightPanel)
				sb.WriteString("\n")
				rendered++
			}
			lineNum++
		}
	}

	return sb.String()
}

// linePair represents a pair of lines for side-by-side view.
type linePair struct {
	left  *DiffLine
	right *DiffLine
}

// groupLinesForSideBySide groups diff lines into pairs for side-by-side display.
func groupLinesForSideBySide(lines []DiffLine) []linePair {
	var pairs []linePair
	i := 0

	for i < len(lines) {
		line := &lines[i]

		switch line.Type {
		case LineContext:
			// Context lines appear on both sides
			pairs = append(pairs, linePair{left: line, right: line})
			i++

		case LineRemove:
			// Check if followed by add lines
			removeStart := i
			for i < len(lines) && lines[i].Type == LineRemove {
				i++
			}
			removeEnd := i

			addStart := i
			for i < len(lines) && lines[i].Type == LineAdd {
				i++
			}
			addEnd := i

			// Pair up removes with adds
			removeCount := removeEnd - removeStart
			addCount := addEnd - addStart
			maxPairs := removeCount
			if addCount > maxPairs {
				maxPairs = addCount
			}

			for j := 0; j < maxPairs; j++ {
				var left, right *DiffLine
				if j < removeCount {
					left = &lines[removeStart+j]
				}
				if j < addCount {
					right = &lines[addStart+j]
				}
				pairs = append(pairs, linePair{left: left, right: right})
			}

		case LineAdd:
			// Orphan add (shouldn't happen if grouping is correct)
			pairs = append(pairs, linePair{left: nil, right: line})
			i++
		}
	}

	return pairs
}

// renderDiffContentWithOffset renders line content with horizontal scroll, word-level and syntax highlighting.
func renderDiffContentWithOffset(line DiffLine, maxWidth, horizontalOffset int, highlighter *SyntaxHighlighter) string {
	// Render full line first to preserve syntax highlighting context
	rendered := renderDiffContent(line, maxWidth+horizontalOffset, highlighter)

	// Apply horizontal offset to already-styled output using ANSI-aware truncation
	// Note: ansi.TruncateLeft used directly here - cache would need plugin access
	if horizontalOffset > 0 {
		rendered = truncateLeftCached(rendered, horizontalOffset)
	}
	return rendered
}

// renderDiffContent renders line content with word-level and syntax highlighting.
func renderDiffContent(line DiffLine, maxWidth int, highlighter *SyntaxHighlighter) string {
	var baseStyle lipgloss.Style
	switch line.Type {
	case LineAdd:
		baseStyle = styles.DiffAdd
	case LineRemove:
		baseStyle = styles.DiffRemove
	default:
		baseStyle = styles.DiffContext
	}

	// If we have word diff data, use it (word diff takes priority over syntax)
	if len(line.WordDiff) > 0 {
		var sb strings.Builder
		for _, segment := range line.WordDiff {
			if segment.IsChange {
				if line.Type == LineAdd {
					sb.WriteString(wordDiffAddStyle.Render(segment.Text))
				} else {
					sb.WriteString(wordDiffRemoveStyle.Render(segment.Text))
				}
			} else {
				sb.WriteString(baseStyle.Render(segment.Text))
			}
		}
		content := sb.String()
		// Truncate if needed (accounting for ANSI codes is complex, so just truncate raw)
		if lipgloss.Width(line.Content) > maxWidth && maxWidth > 3 {
			// Re-render truncated
			truncated := truncateLine(line.Content, maxWidth)
			return baseStyle.Render(truncated)
		}
		return content
	}

	// Apply syntax highlighting if available
	if highlighter != nil {
		segments := highlighter.HighlightLine(line.Content)
		if len(segments) > 0 {
			var sb strings.Builder
			for _, seg := range segments {
				// Blend syntax style with diff line type
				style := blendSyntaxWithDiff(seg.Style, line.Type)
				sb.WriteString(style.Render(seg.Text))
			}
			result := sb.String()
			// Truncate if needed
			if lipgloss.Width(line.Content) > maxWidth && maxWidth > 3 {
				truncated := truncateLine(line.Content, maxWidth)
				return renderSyntaxHighlighted(truncated, line.Type, highlighter)
			}
			return result
		}
	}

	content := line.Content
	if lipgloss.Width(content) > maxWidth && maxWidth > 3 {
		content = truncateLine(content, maxWidth)
	}
	// Add background for add/remove lines even without syntax highlighting
	style := baseStyle
	switch line.Type {
	case LineAdd:
		style = style.Background(styles.DiffAddBg)
	case LineRemove:
		style = style.Background(styles.DiffRemoveBg)
	}
	return style.Render(content)
}

// renderSideBySideContent renders content for side-by-side view with syntax highlighting.
// Returns styled content that should then be padded with padToWidth for alignment.
func renderSideBySideContent(content string, lineType LineType, maxWidth int, highlighter *SyntaxHighlighter) string {
	var baseStyle lipgloss.Style
	switch lineType {
	case LineAdd:
		baseStyle = styles.DiffAdd
	case LineRemove:
		baseStyle = styles.DiffRemove
	default:
		baseStyle = styles.DiffContext
	}

	// Truncate content first to fit maxWidth
	if lipgloss.Width(content) > maxWidth && maxWidth > 3 {
		content = truncateLine(content, maxWidth)
	}

	// Apply syntax highlighting if available
	if highlighter != nil {
		highlighted := renderSyntaxHighlighted(content, lineType, highlighter)
		// Wrap with MaxWidth to ensure consistent width handling
		// even though content was pre-truncated
		return lipgloss.NewStyle().MaxWidth(maxWidth).Render(highlighted)
	}

	// Add background for add/remove lines even without syntax highlighting
	style := baseStyle
	switch lineType {
	case LineAdd:
		style = style.Background(styles.DiffAddBg)
	case LineRemove:
		style = style.Background(styles.DiffRemoveBg)
	}
	return style.MaxWidth(maxWidth).Render(content)
}

// renderSyntaxHighlighted renders content with syntax highlighting blended with diff style.
func renderSyntaxHighlighted(content string, lineType LineType, highlighter *SyntaxHighlighter) string {
	// Helper to get base diff style with background for add/remove lines
	getBaseStyle := func() lipgloss.Style {
		switch lineType {
		case LineAdd:
			return styles.DiffAdd.Background(styles.DiffAddBg)
		case LineRemove:
			return styles.DiffRemove.Background(styles.DiffRemoveBg)
		default:
			return styles.DiffContext
		}
	}

	if highlighter == nil {
		return getBaseStyle().Render(content)
	}

	segments := highlighter.HighlightLine(content)
	if len(segments) == 0 {
		// Fallback to base diff style with background when no syntax segments
		return getBaseStyle().Render(content)
	}

	var sb strings.Builder
	for _, seg := range segments {
		style := blendSyntaxWithDiff(seg.Style, lineType)
		sb.WriteString(style.Render(seg.Text))
	}
	return sb.String()
}

// blendSyntaxWithDiff blends syntax highlighting style with diff line style.
// Add/remove lines preserve syntax foreground colors with subtle diff backgrounds.
// Context lines use syntax highlighting when available.
func blendSyntaxWithDiff(syntaxStyle lipgloss.Style, lineType LineType) lipgloss.Style {
	switch lineType {
	case LineAdd:
		// Keep syntax foreground, add green background for diff indication
		return syntaxStyle.Background(styles.DiffAddBg)
	case LineRemove:
		// Keep syntax foreground, add red background for diff indication
		return syntaxStyle.Background(styles.DiffRemoveBg)
	default:
		// Context lines: use syntax color if available, otherwise muted
		fg := syntaxStyle.GetForeground()
		_, isNoColor := fg.(lipgloss.NoColor)
		if isNoColor {
			return styles.DiffContext
		}
		return syntaxStyle
	}
}

// truncateLine truncates a line to fit within maxWidth using visual width.
func truncateLine(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		// Just take first few runes
		runes := []rune(s)
		if len(runes) > maxWidth {
			return string(runes[:maxWidth])
		}
		return s
	}
	// Truncate rune by rune until we fit
	runes := []rune(s)
	for i := len(runes); i > 0; i-- {
		candidate := string(runes[:i]) + "..."
		if lipgloss.Width(candidate) <= maxWidth {
			return candidate
		}
	}
	return "..."
}

// padRight pads a string with spaces to reach the desired visual width.
func padRight(s string, width int) string {
	visualWidth := lipgloss.Width(s)
	if visualWidth >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visualWidth)
}

// padToWidth pads a styled string (with ANSI codes) to exact visual width.
func padToWidth(s string, width int) string {
	visualWidth := lipgloss.Width(s)
	if visualWidth >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visualWidth)
}

// SideBySideClipInfo contains information about horizontal clipping state.
type SideBySideClipInfo struct {
	HasMoreLeft     bool // True if scrolled right (horizOffset > 0)
	HasMoreRight    bool // True if content extends beyond visible width
	MaxContentWidth int  // Maximum width of any line in the diff
}

// GetSideBySideClipInfo calculates clipping info for a side-by-side diff.
// contentWidth is the width available for each side's content (after line numbers).
func GetSideBySideClipInfo(diff *ParsedDiff, contentWidth, horizontalOffset int) SideBySideClipInfo {
	if diff == nil || diff.Binary {
		return SideBySideClipInfo{}
	}

	// Calculate max content width across all lines
	maxWidth := 0
	for _, hunk := range diff.Hunks {
		for _, line := range hunk.Lines {
			lineWidth := lipgloss.Width(line.Content)
			if lineWidth > maxWidth {
				maxWidth = lineWidth
			}
		}
	}

	return SideBySideClipInfo{
		HasMoreLeft:     horizontalOffset > 0,
		HasMoreRight:    maxWidth > contentWidth+horizontalOffset,
		MaxContentWidth: maxWidth,
	}
}

// RenderFileHeader renders a file header bar for the diff.
func RenderFileHeader(filename, stats string, width int) string {
	// Format: "── filename (+N/-M) ────────"
	prefix := "── "
	suffix := " "
	if stats != "" {
		suffix = " (" + stats + ") "
	}

	// Calculate remaining width for fill
	usedWidth := lipgloss.Width(prefix) + lipgloss.Width(filename) + lipgloss.Width(suffix)
	fillWidth := width - usedWidth
	if fillWidth < 0 {
		fillWidth = 0
	}

	fill := strings.Repeat("─", fillWidth)

	header := prefix + filename + suffix + fill
	return fileHeaderStyle.Width(width).Render(header)
}

// RenderMultiFileDiff renders a multi-file diff with file headers.
// Returns the rendered content and updates file position info.
func RenderMultiFileDiff(mfd *MultiFileDiff, mode DiffViewMode, width, startLine, maxLines, horizontalOffset int, wrapEnabled bool) string {
	if mfd == nil || len(mfd.Files) == 0 {
		return styles.Muted.Render(" No diff content")
	}

	var sb strings.Builder
	currentLine := 0
	rendered := 0

	for i := range mfd.Files {
		file := &mfd.Files[i]
		file.StartLine = currentLine

		// Render file header
		if currentLine >= startLine && rendered < maxLines {
			header := RenderFileHeader(file.FileName(), file.ChangeStats(), width)
			sb.WriteString(header)
			sb.WriteString("\n")
			rendered++
		}
		currentLine++

		// Create syntax highlighter for this file
		var highlighter *SyntaxHighlighter
		if file.Diff.NewFile != "" {
			highlighter = NewSyntaxHighlighter(file.Diff.NewFile)
		}

		// Render file's diff content
		fileContent := renderSingleFileDiff(file.Diff, mode, width, startLine-currentLine, maxLines-rendered, horizontalOffset, highlighter, wrapEnabled)
		fileLines := strings.Split(fileContent, "\n")

		for _, line := range fileLines {
			if currentLine >= startLine && rendered < maxLines {
				sb.WriteString(line)
				sb.WriteString("\n")
				rendered++
			}
			currentLine++
			if rendered >= maxLines {
				break
			}
		}

		file.EndLine = currentLine

		// Add blank line between files
		if i < len(mfd.Files)-1 && rendered < maxLines {
			if currentLine >= startLine {
				sb.WriteString("\n")
				rendered++
			}
			currentLine++
		}

		if rendered >= maxLines {
			break
		}
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

// renderSingleFileDiff renders a single file's diff without the file header.
func renderSingleFileDiff(diff *ParsedDiff, mode DiffViewMode, width, startLine, maxLines, horizontalOffset int, highlighter *SyntaxHighlighter, wrapEnabled bool) string {
	if startLine < 0 {
		startLine = 0
	}
	if maxLines <= 0 {
		return ""
	}

	if mode == DiffViewSideBySide {
		return RenderSideBySide(diff, width, startLine, maxLines, horizontalOffset, highlighter, wrapEnabled)
	}
	return RenderLineDiff(diff, width, startLine, maxLines, horizontalOffset, highlighter, wrapEnabled)
}

// TotalLines returns the total number of rendered lines for a multi-file diff.
func (mfd *MultiFileDiff) TotalLines() int {
	if mfd == nil {
		return 0
	}
	total := 0
	for i, file := range mfd.Files {
		total++ // File header
		total += file.Diff.TotalLines()
		if i < len(mfd.Files)-1 {
			total++ // Blank line between files
		}
	}
	return total
}

// FileAtLine returns the file index at the given line position, or -1 if none.
func (mfd *MultiFileDiff) FileAtLine(line int) int {
	if mfd == nil {
		return -1
	}
	for i, file := range mfd.Files {
		if line >= file.StartLine && line < file.EndLine {
			return i
		}
	}
	return -1
}

// FileCount returns the number of files in the diff.
func (mfd *MultiFileDiff) FileCount() int {
	if mfd == nil {
		return 0
	}
	return len(mfd.Files)
}
