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
	DiffViewUnified   DiffViewMode = iota // Line-by-line unified view
	DiffViewSideBySide                     // Side-by-side split view
)

// Additional styles for enhanced diff rendering
var (
	lineNumberStyle = lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Width(4).
			Align(lipgloss.Right)

	lineNumberSeparator = lipgloss.NewStyle().
				Foreground(styles.TextSubtle)

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
)

// RenderLineDiff renders a parsed diff in unified line-by-line format with line numbers.
// horizontalOffset scrolls the content horizontally (0 = no scroll).
// highlighter is optional - if nil, no syntax highlighting is applied.
func RenderLineDiff(diff *ParsedDiff, width, startLine, maxLines, horizontalOffset int, highlighter *SyntaxHighlighter) string {
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

			// Render content with appropriate style and horizontal offset
			content := renderDiffContentWithOffset(line, contentWidth, horizontalOffset, highlighter)

			sb.WriteString(lineNos)
			sb.WriteString(content)
			sb.WriteString("\n")
			rendered++
		}

		if rendered >= maxLines {
			break
		}
	}

	return sb.String()
}

// RenderSideBySide renders a parsed diff in side-by-side format.
// highlighter is optional - if nil, no syntax highlighting is applied.
func RenderSideBySide(diff *ParsedDiff, width, startLine, maxLines, horizontalOffset int, highlighter *SyntaxHighlighter) string {
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
				leftContent := applyHorizontalOffset(pair.left.Content, horizontalOffset)
				leftRendered = renderSideBySideContent(leftContent, pair.left.Type, contentWidth, highlighter)
			}
			leftRendered = padToWidth(leftRendered, contentWidth)

			// Right side (new)
			rightLineNo := " "
			rightRendered := ""
			if pair.right != nil {
				if pair.right.NewLineNo > 0 {
					rightLineNo = fmt.Sprintf("%d", pair.right.NewLineNo)
				}
				rightContent := applyHorizontalOffset(pair.right.Content, horizontalOffset)
				rightRendered = renderSideBySideContent(rightContent, pair.right.Type, contentWidth, highlighter)
			}
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
	// Apply horizontal offset first
	content := line.Content
	if horizontalOffset > 0 {
		runes := []rune(content)
		if horizontalOffset >= len(runes) {
			content = ""
		} else {
			content = string(runes[horizontalOffset:])
		}
		// Create a modified line with offset content
		line = DiffLine{
			Type:      line.Type,
			Content:   content,
			OldLineNo: line.OldLineNo,
			NewLineNo: line.NewLineNo,
			WordDiff:  nil, // Word diff doesn't work well with offset
		}
	}
	return renderDiffContent(line, maxWidth, highlighter)
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

// applyHorizontalOffset removes the first n runes from a string.
func applyHorizontalOffset(s string, offset int) string {
	if offset <= 0 {
		return s
	}
	runes := []rune(s)
	if offset >= len(runes) {
		return ""
	}
	return string(runes[offset:])
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
