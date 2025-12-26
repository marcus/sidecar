package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sst/sidecar/internal/styles"
)

const (
	headerHeight = 1
	footerHeight = 1
	minWidth     = 80
	minHeight    = 24
)

// View renders the entire application UI.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Calculate content area
	contentHeight := m.height - headerHeight
	if m.showFooter {
		contentHeight -= footerHeight
	}

	// Build layout
	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Main content
	content := m.renderContent(m.width, contentHeight)
	b.WriteString(content)

	// Footer (optional)
	if m.showFooter {
		b.WriteString("\n")
		b.WriteString(m.renderFooter())
	}

	// Overlay modals
	if m.showHelp {
		return m.renderHelpOverlay(b.String())
	}
	if m.showDiagnostics {
		return m.renderDiagnosticsOverlay(b.String())
	}

	return b.String()
}

// renderHeader renders the top bar with title, tabs, and clock.
func (m Model) renderHeader() string {
	// Title
	title := styles.Header.Render(" Agent Sidecar ")

	// Plugin tabs
	plugins := m.registry.Plugins()
	var tabs []string
	for i, p := range plugins {
		icon := p.Icon()
		if icon == "" {
			icon = "•"
		}
		label := fmt.Sprintf(" %s %s ", icon, p.Name())
		if i == m.activePlugin {
			tabs = append(tabs, styles.TabActive.Render(label))
		} else {
			tabs = append(tabs, styles.TabInactive.Render(label))
		}
	}
	tabBar := strings.Join(tabs, "")

	// Clock
	clock := styles.Muted.Render(m.ui.Clock.Format("15:04"))

	// Calculate spacing
	titleWidth := lipgloss.Width(title)
	tabWidth := lipgloss.Width(tabBar)
	clockWidth := lipgloss.Width(clock)
	spacing := m.width - titleWidth - tabWidth - clockWidth

	if spacing < 0 {
		spacing = 0
	}

	// Build header line
	header := title + strings.Repeat(" ", spacing/2) + tabBar + strings.Repeat(" ", spacing-(spacing/2)) + clock

	return styles.Header.Width(m.width).Render(header)
}

// renderContent renders the main content area.
func (m Model) renderContent(width, height int) string {
	p := m.ActivePlugin()
	if p == nil {
		msg := "No plugins loaded"
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, styles.Muted.Render(msg))
	}

	content := p.View(width, height)
	return content
}

// renderFooter renders the bottom bar with key hints and status.
func (m Model) renderFooter() string {
	// Key hints (context-aware)
	hints := m.getContextHints()
	hintsStr := styles.KeyHint.Render(hints)

	// Toast/status message
	var status string
	if m.ui.HasToast() {
		status = styles.StatusModified.Render(m.ui.ToastMessage)
	} else if m.statusMsg != "" {
		status = styles.StatusModified.Render(m.statusMsg)
	}

	// Last refresh
	refresh := styles.Muted.Render(fmt.Sprintf("↻ %s", m.ui.LastRefresh.Format("15:04:05")))

	// Calculate spacing
	hintsWidth := lipgloss.Width(hintsStr)
	statusWidth := lipgloss.Width(status)
	refreshWidth := lipgloss.Width(refresh)
	spacing := m.width - hintsWidth - statusWidth - refreshWidth

	if spacing < 0 {
		spacing = 0
	}

	footer := hintsStr + strings.Repeat(" ", spacing/2) + status + strings.Repeat(" ", spacing-(spacing/2)) + refresh

	return styles.Footer.Width(m.width).Render(footer)
}

// getContextHints returns context-appropriate key hints.
func (m Model) getContextHints() string {
	common := "tab switch  ? help  q quit"
	switch m.activeContext {
	case "git-status":
		return common + "  s stage  u unstage  d diff"
	case "td-monitor":
		return common + "  a approve  x delete"
	default:
		return common
	}
}

// renderHelpOverlay renders the help modal over content.
func (m Model) renderHelpOverlay(content string) string {
	help := m.buildHelpContent()
	modal := styles.ModalBox.Render(help)

	// Center the modal
	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		modal,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("#000000")),
	)
}

// buildHelpContent creates the help modal content.
func (m Model) buildHelpContent() string {
	var b strings.Builder

	b.WriteString(styles.ModalTitle.Render("Keyboard Shortcuts"))
	b.WriteString("\n\n")

	// Global
	b.WriteString(styles.Title.Render("Global"))
	b.WriteString("\n")
	b.WriteString(styles.Muted.Render("  q, ctrl+c") + "  quit\n")
	b.WriteString(styles.Muted.Render("  tab      ") + "  next plugin\n")
	b.WriteString(styles.Muted.Render("  shift+tab") + "  prev plugin\n")
	b.WriteString(styles.Muted.Render("  1-9      ") + "  focus plugin\n")
	b.WriteString(styles.Muted.Render("  ?        ") + "  toggle help\n")
	b.WriteString(styles.Muted.Render("  !        ") + "  toggle diagnostics\n")
	b.WriteString(styles.Muted.Render("  ctrl+h   ") + "  toggle footer\n")
	b.WriteString(styles.Muted.Render("  r        ") + "  refresh\n")
	b.WriteString("\n")

	// Navigation
	b.WriteString(styles.Title.Render("Navigation"))
	b.WriteString("\n")
	b.WriteString(styles.Muted.Render("  j/down   ") + "  cursor down\n")
	b.WriteString(styles.Muted.Render("  k/up     ") + "  cursor up\n")
	b.WriteString(styles.Muted.Render("  g g      ") + "  go to top\n")
	b.WriteString(styles.Muted.Render("  G        ") + "  go to bottom\n")
	b.WriteString(styles.Muted.Render("  enter    ") + "  select\n")
	b.WriteString(styles.Muted.Render("  esc      ") + "  back/close\n")
	b.WriteString("\n")
	b.WriteString(styles.Subtle.Render("Press esc to close"))

	return b.String()
}

// renderDiagnosticsOverlay renders the diagnostics modal.
func (m Model) renderDiagnosticsOverlay(content string) string {
	diag := m.buildDiagnosticsContent()
	modal := styles.ModalBox.Render(diag)

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}

// buildDiagnosticsContent creates the diagnostics modal content.
func (m Model) buildDiagnosticsContent() string {
	var b strings.Builder

	b.WriteString(styles.ModalTitle.Render("Diagnostics"))
	b.WriteString("\n\n")

	// Plugins status
	b.WriteString(styles.Title.Render("Plugins"))
	b.WriteString("\n")

	plugins := m.registry.Plugins()
	for _, p := range plugins {
		status := styles.StatusCompleted.Render("✓")
		b.WriteString(fmt.Sprintf("  %s %s\n", status, p.Name()))
	}

	unavail := m.registry.Unavailable()
	for id, reason := range unavail {
		status := styles.StatusBlocked.Render("✗")
		b.WriteString(fmt.Sprintf("  %s %s: %s\n", status, id, reason))
	}

	if len(plugins) == 0 && len(unavail) == 0 {
		b.WriteString(styles.Muted.Render("  No plugins registered\n"))
	}

	b.WriteString("\n")

	// Last error
	if m.lastError != nil {
		b.WriteString(styles.Title.Render("Last Error"))
		b.WriteString("\n")
		b.WriteString(styles.StatusBlocked.Render(fmt.Sprintf("  %s\n", m.lastError.Error())))
		b.WriteString("\n")
	}

	b.WriteString(styles.Subtle.Render("Press esc to close"))

	return b.String()
}
