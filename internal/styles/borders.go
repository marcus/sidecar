package styles

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Border characters for rounded borders (matching lipgloss.RoundedBorder)
const (
	borderCornerTL   = "╭"
	borderCornerTR   = "╮"
	borderCornerBL   = "╰"
	borderCornerBR   = "╯"
	borderHorizontal = "─"
	borderVertical   = "│"
)

// colorChar wraps a character with ANSI foreground color.
func colorChar(char string, color RGB) string {
	return color.ToANSI() + char + ANSIReset
}

// RenderGradientBorder renders content inside a box with gradient-colored borders.
// The gradient flows at the specified angle (typically 30 degrees).
// width and height are the outer dimensions including borders.
func RenderGradientBorder(content string, width, height int, gradient Gradient, padding int) string {
	if width < 3 || height < 3 {
		return content
	}

	// Calculate inner dimensions
	innerWidth := width - 2   // subtract left and right borders
	innerHeight := height - 2 // subtract top and bottom borders

	// Split content into lines
	lines := strings.Split(content, "\n")

	// Pad or truncate lines to fit inner width with padding
	paddedLines := make([]string, innerHeight)
	paddingStr := strings.Repeat(" ", padding)
	contentWidth := innerWidth - (padding * 2)
	if contentWidth < 0 {
		contentWidth = 0
	}

	for i := 0; i < innerHeight; i++ {
		var line string
		if i < len(lines) {
			line = lines[i]
		}

		// Get visual width and truncate/pad as needed
		lineWidth := lipgloss.Width(line)
		if lineWidth > contentWidth {
			line = truncateString(line, contentWidth)
			lineWidth = lipgloss.Width(line)
		}

		// Pad to fill width
		rightPad := contentWidth - lineWidth
		if rightPad < 0 {
			rightPad = 0
		}
		paddedLines[i] = paddingStr + line + strings.Repeat(" ", rightPad) + paddingStr
	}

	var result strings.Builder

	// Render top border
	result.WriteString(renderGradientBorderTop(width, height, gradient))
	result.WriteString("\n")

	// Render content lines with side borders
	for y, line := range paddedLines {
		// Left border (y+1 because top border is y=0)
		leftPos := gradient.PositionAt(0, y+1, width, height)
		result.WriteString(colorChar(borderVertical, gradient.ColorAt(leftPos)))

		// Content
		result.WriteString(line)

		// Right border
		rightPos := gradient.PositionAt(width-1, y+1, width, height)
		result.WriteString(colorChar(borderVertical, gradient.ColorAt(rightPos)))
		result.WriteString("\n")
	}

	// Render bottom border
	result.WriteString(renderGradientBorderBottom(width, height, gradient))

	return result.String()
}

// renderGradientBorderTop renders the top border line with gradient colors.
func renderGradientBorderTop(width, height int, g Gradient) string {
	var sb strings.Builder

	// Top-left corner (position 0, 0)
	pos := g.PositionAt(0, 0, width, height)
	sb.WriteString(colorChar(borderCornerTL, g.ColorAt(pos)))

	// Horizontal line
	for x := 1; x < width-1; x++ {
		pos := g.PositionAt(x, 0, width, height)
		sb.WriteString(colorChar(borderHorizontal, g.ColorAt(pos)))
	}

	// Top-right corner
	pos = g.PositionAt(width-1, 0, width, height)
	sb.WriteString(colorChar(borderCornerTR, g.ColorAt(pos)))

	return sb.String()
}

// renderGradientBorderBottom renders the bottom border line with gradient colors.
func renderGradientBorderBottom(width, height int, g Gradient) string {
	var sb strings.Builder
	y := height - 1

	// Bottom-left corner
	pos := g.PositionAt(0, y, width, height)
	sb.WriteString(colorChar(borderCornerBL, g.ColorAt(pos)))

	// Horizontal line
	for x := 1; x < width-1; x++ {
		pos := g.PositionAt(x, y, width, height)
		sb.WriteString(colorChar(borderHorizontal, g.ColorAt(pos)))
	}

	// Bottom-right corner
	pos = g.PositionAt(width-1, y, width, height)
	sb.WriteString(colorChar(borderCornerBR, g.ColorAt(pos)))

	return sb.String()
}

// truncateString truncates a string to maxWidth visual characters.
// This function is ANSI-aware and will not break escape sequences.
func truncateString(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	// Use lipgloss truncation which handles ANSI sequences correctly
	return lipgloss.NewStyle().MaxWidth(maxWidth).Render(s)
}

// GetActiveGradient returns the gradient for active (focused) panels from current theme.
func GetActiveGradient() Gradient {
	theme := GetCurrentTheme()
	colors := theme.Colors.GradientBorderActive
	angle := theme.Colors.GradientBorderAngle

	if len(colors) < 2 {
		// Fallback to solid color using BorderActive
		return NewGradient([]string{theme.Colors.BorderActive, theme.Colors.BorderActive}, angle)
	}

	if angle == 0 {
		angle = DefaultGradientAngle
	}

	return NewGradient(colors, angle)
}

// GetNormalGradient returns the gradient for inactive panels from current theme.
func GetNormalGradient() Gradient {
	theme := GetCurrentTheme()
	colors := theme.Colors.GradientBorderNormal
	angle := theme.Colors.GradientBorderAngle

	if len(colors) < 2 {
		// Fallback to solid color using BorderNormal
		return NewGradient([]string{theme.Colors.BorderNormal, theme.Colors.BorderNormal}, angle)
	}

	if angle == 0 {
		angle = DefaultGradientAngle
	}

	return NewGradient(colors, angle)
}

// RenderPanel renders content in a panel with gradient borders.
// This is the main function plugins should use for bordered panels.
// active determines whether to use active (focused) or normal gradient.
// width and height are the outer dimensions including borders.
func RenderPanel(content string, width, height int, active bool) string {
	var gradient Gradient
	if active {
		gradient = GetActiveGradient()
	} else {
		gradient = GetNormalGradient()
	}

	// Use padding of 1 to match lipgloss panel padding
	return RenderGradientBorder(content, width, height, gradient, 1)
}

// RenderPanelWithGradient renders content in a panel with a custom gradient.
// Useful for modals or special cases that need different gradient colors.
func RenderPanelWithGradient(content string, width, height int, gradient Gradient) string {
	return RenderGradientBorder(content, width, height, gradient, 1)
}
