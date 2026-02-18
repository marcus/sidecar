package run

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// Status indicator symbols.
const (
	iconIdle    = "○"
	iconRunning = "●"
	iconDone    = "✓"
	iconError   = "✗"
)

// View renders the plugin.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height

	if len(p.commands) == 0 {
		content := p.renderEmpty()
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
	}

	content := p.renderTwoPaneLayout(height)
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
}

// renderEmpty shows when no commands are detected.
func (p *Plugin) renderEmpty() string {
	var sb strings.Builder
	sb.WriteString(styles.Title.Render("Run"))
	sb.WriteString("\n\n")
	sb.WriteString(styles.Muted.Render("No runnable commands detected."))
	sb.WriteString("\n\n")
	sb.WriteString(styles.Subtle.Render("Supports: Makefile, package.json, docker-compose.yml, pyproject.toml"))
	sb.WriteString("\n")
	sb.WriteString(styles.Subtle.Render("Or add commands to "))
	sb.WriteString(styles.Code.Render("~/.config/sidecar/config.json"))
	sb.WriteString(styles.Subtle.Render(" under "))
	sb.WriteString(styles.Code.Render("plugins.run.commands"))
	sb.WriteString("\n\n")
	sb.WriteString(styles.Subtle.Render("Press "))
	sb.WriteString(styles.Code.Render("r"))
	sb.WriteString(styles.Subtle.Render(" to refresh"))
	return sb.String()
}

// renderTwoPaneLayout renders the command list and output panes.
func (p *Plugin) renderTwoPaneLayout(height int) string {
	paneHeight := height
	if paneHeight < 4 {
		paneHeight = 4
	}

	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Calculate sidebar width
	sidebarWidth := p.width * p.sidebarWidth / 100
	if sidebarWidth < 20 {
		sidebarWidth = 20
	}
	outputWidth := p.width - sidebarWidth - dividerWidth

	listActive := p.activePane == PaneList
	outputActive := p.activePane == PaneOutput

	// Render pane contents
	listContent := p.renderListPane(innerHeight, sidebarWidth-4) // -4 for borders+padding
	outputContent := p.renderOutputPane(innerHeight, outputWidth-4)

	// Apply panel styles
	leftPane := styles.RenderPanel(listContent, sidebarWidth, paneHeight, listActive)
	rightPane := styles.RenderPanel(outputContent, outputWidth, paneHeight, outputActive)

	// Render divider
	divider := ui.RenderDivider(paneHeight)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, divider, rightPane)
}

// renderListPane renders the command list sidebar.
func (p *Plugin) renderListPane(height, contentWidth int) string {
	var sb strings.Builder

	// Header
	sb.WriteString(styles.Title.Render("Commands"))
	sb.WriteString(styles.Muted.Render(fmt.Sprintf(" (%d)", len(p.commands))))
	sb.WriteString("\n")

	headerLines := 1
	listHeight := height - headerLines
	if listHeight < 1 {
		listHeight = 1
	}

	// Ensure cursor is visible
	p.ensureVisibleForHeight(listHeight)

	// Calculate visible range
	start := p.scrollOff
	end := start + listHeight
	if end > len(p.commands) {
		end = len(p.commands)
	}

	// Track current group for group headers
	lastGroup := ""

	for i := start; i < end; i++ {
		cmd := p.commands[i]

		// Group header
		if cmd.Group != lastGroup {
			lastGroup = cmd.Group
			if i > start {
				// Don't add group header if it would be the only line
			}
		}

		isSelected := i == p.cursor
		sb.WriteString(p.renderCommandRow(cmd, i, isSelected, contentWidth))
		if i < end-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// renderCommandRow renders a single command row with status indicator.
func (p *Plugin) renderCommandRow(cmd RunCommand, idx int, selected bool, maxWidth int) string {
	// Status icon
	icon := iconIdle
	session := p.sessions[idx]
	if session != nil {
		switch session.Status {
		case StatusRunning:
			icon = iconRunning
		case StatusDone:
			icon = iconDone
		case StatusError:
			icon = iconError
		}
	}

	// Build the row
	var prefix string
	if selected {
		prefix = "> "
	} else {
		prefix = "  "
	}

	// Source tag
	sourceTag := "[" + cmd.Source + "]"

	// Calculate available width for name
	nameWidth := maxWidth - len(prefix) - len(icon) - 1 - len(sourceTag) - 1
	if nameWidth < 5 {
		nameWidth = 5
	}

	name := cmd.Name
	runes := []rune(name)
	if len(runes) > nameWidth {
		name = string(runes[:nameWidth-3]) + "..."
	}

	if selected {
		// Full-width highlight for selected row
		row := prefix + icon + " " + name
		// Pad with spaces for source tag alignment
		padding := maxWidth - lipgloss.Width(row) - len(sourceTag)
		if padding > 0 {
			row += strings.Repeat(" ", padding)
		}
		row += sourceTag
		return styles.ListItemSelected.Render(row)
	}

	// Style based on status
	var iconStyled string
	if session != nil {
		switch session.Status {
		case StatusRunning:
			iconStyled = styles.StatusModified.Render(icon)
		case StatusDone:
			iconStyled = styles.StatusStaged.Render(icon)
		case StatusError:
			iconStyled = styles.StatusDeleted.Render(icon)
		default:
			iconStyled = styles.Muted.Render(icon)
		}
	} else {
		iconStyled = styles.Muted.Render(icon)
	}

	return prefix + iconStyled + " " + styles.Body.Render(name) + " " + styles.Muted.Render(sourceTag)
}

// renderOutputPane renders the right pane with command output.
func (p *Plugin) renderOutputPane(height, contentWidth int) string {
	session := p.selectedSession()
	if session == nil {
		return p.renderOutputPlaceholder()
	}

	var sb strings.Builder

	// Header: command name and status
	statusLabel := session.Status.String()
	var statusStyled string
	switch session.Status {
	case StatusRunning:
		statusStyled = styles.StatusModified.Render(statusLabel)
	case StatusDone:
		statusStyled = styles.StatusStaged.Render(statusLabel)
	case StatusError:
		statusStyled = styles.StatusDeleted.Render(statusLabel)
	default:
		statusStyled = styles.Muted.Render(statusLabel)
	}

	sb.WriteString(styles.Title.Render(session.Command.Name))
	sb.WriteString(" ")
	sb.WriteString(statusStyled)
	sb.WriteString("\n")

	// Command line
	sb.WriteString(styles.Muted.Render("$ " + session.Command.Command))
	sb.WriteString("\n")

	headerLines := 2
	outputHeight := height - headerLines
	if outputHeight < 1 {
		outputHeight = 1
	}

	// Render output with scroll
	if session.Output == "" {
		sb.WriteString(styles.Muted.Render("Waiting for output..."))
	} else {
		lines := strings.Split(session.Output, "\n")
		p.outputLines = len(lines)

		// Auto-scroll to bottom if not manually scrolled
		maxScroll := len(lines) - outputHeight
		if maxScroll < 0 {
			maxScroll = 0
		}
		if p.outputScrollOff > maxScroll {
			p.outputScrollOff = maxScroll
		}

		// For running commands, auto-scroll to bottom
		if session.Status == StatusRunning && p.activePane != PaneOutput {
			p.outputScrollOff = maxScroll
		}

		start := p.outputScrollOff
		end := start + outputHeight
		if end > len(lines) {
			end = len(lines)
		}
		if start < 0 {
			start = 0
		}

		for i := start; i < end; i++ {
			line := lines[i]
			// Truncate long lines
			if lipgloss.Width(line) > contentWidth {
				line = line[:contentWidth]
			}
			sb.WriteString(line)
			if i < end-1 {
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// renderOutputPlaceholder shows when no command is selected or running.
func (p *Plugin) renderOutputPlaceholder() string {
	var sb strings.Builder
	sb.WriteString(styles.Muted.Render("No output"))
	sb.WriteString("\n\n")
	sb.WriteString(styles.Subtle.Render("Select a command and press "))
	sb.WriteString(styles.Code.Render("Enter"))
	sb.WriteString(styles.Subtle.Render(" to run"))
	return sb.String()
}

// ensureVisibleForHeight adjusts scroll offset for a given visible height.
func (p *Plugin) ensureVisibleForHeight(visibleHeight int) {
	if p.cursor < 0 {
		p.cursor = 0
	}
	if len(p.commands) > 0 && p.cursor >= len(p.commands) {
		p.cursor = len(p.commands) - 1
	}
	if p.cursor < p.scrollOff {
		p.scrollOff = p.cursor
	}
	if p.cursor >= p.scrollOff+visibleHeight {
		p.scrollOff = p.cursor - visibleHeight + 1
	}
	if p.scrollOff < 0 {
		p.scrollOff = 0
	}
}
