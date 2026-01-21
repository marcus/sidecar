package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	headerHeight = 2 // header line + spacing
	footerHeight = 1
	minWidth     = 80
	minHeight    = 24
)

// View renders the entire application UI.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Show warning if terminal is too small
	if m.width < minWidth || m.height < minHeight {
		msg := fmt.Sprintf("Terminal too small (%dx%d)\nMinimum: %dx%d",
			m.width, m.height, minWidth, minHeight)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			styles.StatusBlocked.Render(msg))
	}

	// Calculate content area
	contentHeight := m.height - headerHeight
	if m.showFooter {
		contentHeight -= footerHeight
	}
	if contentHeight < 0 {
		contentHeight = 0
	}

	// Build layout
	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString("\n") // spacing between header and content

	// Main content
	content := m.renderContent(m.width, contentHeight)
	b.WriteString(content)

	// Footer (optional)
	if m.showFooter {
		b.WriteString("\n")
		b.WriteString(m.renderFooter())
	}

	// Overlay modals
	if m.showPalette {
		return m.renderPaletteOverlay(b.String())
	}
	if m.showHelp {
		return m.renderHelpOverlay(b.String())
	}
	if m.showDiagnostics {
		return m.renderDiagnosticsOverlay(b.String())
	}
	if m.showQuitConfirm {
		return m.renderQuitConfirmOverlay(b.String())
	}
	if m.showProjectSwitcher {
		return m.renderProjectSwitcherOverlay(b.String())
	}

	return b.String()
}

// renderPaletteOverlay renders the command palette modal.
func (m Model) renderPaletteOverlay(content string) string {
	modal := m.palette.View()
	return ui.OverlayModal(content, modal, m.width, m.height)
}

// renderQuitConfirmOverlay renders the quit confirmation modal.
func (m Model) renderQuitConfirmOverlay(content string) string {
	var b strings.Builder
	b.WriteString(styles.ModalTitle.Render("Quit Sidecar?"))
	b.WriteString("\n\n")
	b.WriteString("Press ")
	b.WriteString(styles.KeyHint.Render("y"))
	b.WriteString(" or ")
	b.WriteString(styles.KeyHint.Render("enter"))
	b.WriteString(" to quit, ")
	b.WriteString(styles.KeyHint.Render("n"))
	b.WriteString(" or ")
	b.WriteString(styles.KeyHint.Render("esc"))
	b.WriteString(" to cancel")

	modal := styles.ModalBox.Render(b.String())
	return ui.OverlayModal(content, modal, m.width, m.height)
}

// renderProjectSwitcherOverlay renders the project switcher modal.
func (m Model) renderProjectSwitcherOverlay(content string) string {
	var b strings.Builder

	// Title
	b.WriteString(styles.ModalTitle.Render("Switch Project"))
	b.WriteString("  ")
	b.WriteString(styles.Muted.Render("@"))
	b.WriteString("\n\n")

	allProjects := m.cfg.Projects.List

	// Empty state (no projects configured at all)
	if len(allProjects) == 0 {
		b.WriteString(styles.Muted.Render("No projects configured."))
		b.WriteString("\n\n")

		// LLM setup prompt - prominent
		b.WriteString(lipgloss.NewStyle().Foreground(styles.Secondary).Bold(true).Render("Quick Setup"))
		b.WriteString("\n")
		b.WriteString(styles.KeyHint.Render("y"))
		b.WriteString(styles.Muted.Render(" copy setup prompt for LLM"))
		b.WriteString("\n\n")

		// Manual config
		b.WriteString(styles.Muted.Render("Or manually edit:\n"))
		b.WriteString(styles.KeyHint.Render("~/.config/sidecar/config.json"))
		b.WriteString("\n\n")
		configExample := `{
  "projects": {
    "list": [
      {"name": "myapp", "path": "~/code/myapp"}
    ]
  }
}`
		b.WriteString(styles.Subtle.Render(configExample))
		b.WriteString("\n\n")
		b.WriteString(styles.KeyHint.Render("esc"))
		b.WriteString(styles.Muted.Render(" close"))

		modal := styles.ModalBox.Render(b.String())
		return ui.OverlayModal(content, modal, m.width, m.height)
	}

	// Render search input
	b.WriteString(m.projectSwitcherInput.View())
	b.WriteString("\n")

	// Show count if filtering
	projects := m.projectSwitcherFiltered
	if m.projectSwitcherInput.Value() != "" {
		b.WriteString(styles.Muted.Render(fmt.Sprintf("%d of %d projects", len(projects), len(allProjects))))
	}
	b.WriteString("\n")

	// Empty filtered state
	if len(projects) == 0 {
		b.WriteString("\n")
		b.WriteString(styles.Muted.Render("No matches"))
		b.WriteString("\n\n")
		b.WriteString(styles.KeyHint.Render("esc"))
		b.WriteString(styles.Muted.Render(" clear filter  "))
		b.WriteString(styles.KeyHint.Render("@"))
		b.WriteString(styles.Muted.Render(" close"))

		modal := styles.ModalBox.Render(b.String())
		return ui.OverlayModal(content, modal, m.width, m.height)
	}

	// Calculate visible window for scrolling
	maxVisible := 8
	visibleCount := len(projects)
	if visibleCount > maxVisible {
		visibleCount = maxVisible
	}

	// Use stored scroll offset
	scrollOffset := m.projectSwitcherScroll

	// Render scroll indicator if needed (top)
	if scrollOffset > 0 {
		b.WriteString(styles.Muted.Render(fmt.Sprintf("  ↑ %d more above\n", scrollOffset)))
	}

	// Styles for project items
	cursorStyle := lipgloss.NewStyle().Foreground(styles.Primary)
	// Normal name: themed color (secondary/blue) for visibility
	nameNormalStyle := lipgloss.NewStyle().Foreground(styles.Secondary)
	// Selected name: brighter primary color + bold
	nameSelectedStyle := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	// Current project: green + bold
	nameCurrentStyle := lipgloss.NewStyle().Foreground(styles.Success).Bold(true)
	// Current + selected: bright green + bold
	nameCurrentSelectedStyle := lipgloss.NewStyle().Foreground(styles.Success).Bold(true)
	// Paths: always muted, never bold (slightly brighter when selected)
	pathNormalStyle := styles.Subtle
	pathSelectedStyle := styles.Muted

	// Render project list
	for i := scrollOffset; i < scrollOffset+visibleCount && i < len(projects); i++ {
		proj := projects[i]
		isCursor := i == m.projectSwitcherCursor
		isHover := i == m.projectSwitcherHover
		isCurrent := proj.Path == m.ui.WorkDir

		// Cursor indicator
		if isCursor {
			b.WriteString(cursorStyle.Render("→ "))
		} else {
			b.WriteString("  ")
		}

		// Project name - always has themed color, bold when selected
		var nameStyle lipgloss.Style
		if isCurrent {
			if isCursor || isHover {
				nameStyle = nameCurrentSelectedStyle
			} else {
				nameStyle = nameCurrentStyle
			}
		} else if isCursor || isHover {
			nameStyle = nameSelectedStyle
		} else {
			nameStyle = nameNormalStyle
		}
		b.WriteString(nameStyle.Render(proj.Name))

		// Current indicator
		if isCurrent {
			b.WriteString(styles.Muted.Render(" (current)"))
		}
		b.WriteString("\n")

		// Project path (always non-bold, slightly brighter when selected)
		pathStyle := pathNormalStyle
		if isCursor || isHover {
			pathStyle = pathSelectedStyle
		}
		b.WriteString("  ")
		b.WriteString(pathStyle.Render(proj.Path))
		b.WriteString("\n")
	}

	// Render scroll indicator if needed (bottom)
	remaining := len(projects) - (scrollOffset + visibleCount)
	if remaining > 0 {
		b.WriteString(styles.Muted.Render(fmt.Sprintf("  ↓ %d more below\n", remaining)))
	}

	b.WriteString("\n")

	// Help text
	b.WriteString(styles.KeyHint.Render("enter"))
	b.WriteString(styles.Muted.Render(" select  "))
	b.WriteString(styles.KeyHint.Render("↑/↓"))
	b.WriteString(styles.Muted.Render(" navigate  "))
	b.WriteString(styles.KeyHint.Render("esc"))
	b.WriteString(styles.Muted.Render(" cancel  "))
	b.WriteString(styles.Subtle.Render("y"))
	b.WriteString(styles.Subtle.Render(" llm setup"))

	modal := styles.ModalBox.Render(b.String())
	return ui.OverlayModal(content, modal, m.width, m.height)
}

// renderHeader renders the top bar with title, tabs, and clock.
func (m Model) renderHeader() string {
	// Calculate final title width (with repo name) - used for tab positioning
	finalTitleWidth := lipgloss.Width(styles.BarTitle.Render(" Sidecar"))
	if m.intro.RepoName != "" {
		finalTitleWidth += lipgloss.Width(styles.Subtitle.Render(" / " + m.intro.RepoName))
	}
	finalTitleWidth += 1 // trailing space

	// Title with optional repo name
	var title string
	if m.intro.Active {
		// During animation, render into fixed-width container to keep tabs stable
		titleContent := styles.BarTitle.Render(" "+m.intro.View()) + m.intro.RepoNameView() + " "
		title = lipgloss.NewStyle().Width(finalTitleWidth).Render(titleContent)
	} else {
		// Static title with repo name
		repoSuffix := ""
		if m.intro.RepoName != "" {
			repoSuffix = styles.Subtitle.Render(" / " + m.intro.RepoName)
		}
		title = styles.BarTitle.Render(" Sidecar") + repoSuffix + " "
	}

	// Plugin tabs (rainbow gradient)
	plugins := m.registry.Plugins()
	var tabs []string
	for i, p := range plugins {
		isActive := i == m.activePlugin
		tab := styles.RenderGradientTab(p.Name(), i, len(plugins), isActive)
		tabs = append(tabs, tab)
	}
	tabBar := strings.Join(tabs, " ")

	// Clock
	clock := styles.BarText.Render(m.ui.Clock.Format("15:04"))

	// Calculate spacing (always use finalTitleWidth so tabs don't shift)
	tabWidth := lipgloss.Width(tabBar)
	clockWidth := lipgloss.Width(clock)
	spacing := m.width - finalTitleWidth - tabWidth - clockWidth

	if spacing < 0 {
		spacing = 0
	}

	// Build header line
	header := title + strings.Repeat(" ", spacing/2) + tabBar + strings.Repeat(" ", spacing-(spacing/2)) + clock

	return styles.Header.Width(m.width).Render(header)
}

// getTabBounds calculates the X position bounds for each tab in the header.
// Used for mouse click detection on tabs.
func (m Model) getTabBounds() []TabBounds {
	// Always use final title width (must match renderHeader logic)
	titleWidth := lipgloss.Width(styles.BarTitle.Render(" Sidecar"))
	if m.intro.RepoName != "" {
		titleWidth += lipgloss.Width(styles.Subtitle.Render(" / " + m.intro.RepoName))
	}
	titleWidth += 1 // trailing space

	// Calculate tab widths (using gradient renderer)
	plugins := m.registry.Plugins()
	var tabWidths []int
	totalTabWidth := 0
	for i, p := range plugins {
		isActive := i == m.activePlugin
		tab := styles.RenderGradientTab(p.Name(), i, len(plugins), isActive)
		w := lipgloss.Width(tab)
		tabWidths = append(tabWidths, w)
		totalTabWidth += w
	}
	// Add spaces between tabs
	if len(plugins) > 1 {
		totalTabWidth += len(plugins) - 1
	}

	// Clock width
	clock := styles.BarText.Render(m.ui.Clock.Format("15:04"))
	clockWidth := lipgloss.Width(clock)

	// Calculate spacing
	spacing := m.width - titleWidth - totalTabWidth - clockWidth
	if spacing < 0 {
		spacing = 0
	}

	// Calculate tab bounds
	// Tabs start after: title + left spacing
	tabStartX := titleWidth + spacing/2
	bounds := make([]TabBounds, len(plugins))
	x := tabStartX
	for i, w := range tabWidths {
		bounds[i] = TabBounds{Start: x, End: x + w}
		x += w + 1 // +1 for space between tabs
	}

	return bounds
}

// getRepoNameBounds returns the X bounds for the repo name in the header.
func (m Model) getRepoNameBounds() (start, end int, ok bool) {
	if m.intro.RepoName == "" {
		return 0, 0, false
	}

	titlePrefix := styles.BarTitle.Render(" Sidecar")
	repoPrefix := styles.Subtitle.Render(" / ")
	repoName := styles.Subtitle.Render(m.intro.RepoName)

	start = lipgloss.Width(titlePrefix) + lipgloss.Width(repoPrefix)
	end = start + lipgloss.Width(repoName)
	return start, end, true
}

// renderContent renders the main content area.
func (m Model) renderContent(width, height int) string {
	p := m.ActivePlugin()
	if p == nil {
		msg := "No plugins loaded"
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, styles.Muted.Render(msg))
	}

	content := p.View(width, height)
	if height == 0 {
		return ""
	}
	// Use MaxHeight to truncate content that exceeds allocated space.
	// Height() only pads short content; MaxHeight() also truncates tall content.
	// This prevents plugin content from pushing the header off-screen.
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
}

// renderFooter renders the bottom bar with key hints and status.
func (m Model) renderFooter() string {
	// Toast/status message
	var status string
	if m.ui.HasToast() {
		status = styles.StatusModified.Render(m.ui.ToastMessage)
	} else if m.statusMsg != "" {
		toastStyle := styles.ToastSuccess
		if m.statusIsError {
			toastStyle = styles.ToastError
		}
		status = toastStyle.Render(m.statusMsg)
	}

	// Last refresh
	refresh := styles.Muted.Render(fmt.Sprintf("↻ %s", m.ui.LastRefresh.Format("15:04:05")))

	// Calculate available width for hints (leave room for status, refresh, and spacing)
	statusWidth := lipgloss.Width(status)
	refreshWidth := lipgloss.Width(refresh)
	minSpacing := 4 // Minimum spacing between elements
	availableForHints := m.width - statusWidth - refreshWidth - minSpacing

	// Key hints (context-aware) - truncate to fit
	hintsStr := renderHintLineTruncated(m.footerHints(), availableForHints)

	// Calculate spacing
	hintsWidth := lipgloss.Width(hintsStr)
	spacing := m.width - hintsWidth - statusWidth - refreshWidth

	if spacing < 0 {
		spacing = 0
	}

	footer := hintsStr + strings.Repeat(" ", spacing/2) + status + strings.Repeat(" ", spacing-(spacing/2)) + refresh

	// Use MaxWidth to prevent wrapping and ensure single line
	return styles.Footer.Width(m.width).MaxWidth(m.width).Render(footer)
}

type footerHint struct {
	keys  string
	label string
}

func (m Model) footerHints() []footerHint {
	// Plugin-specific hints first - they're more contextually relevant
	var hints []footerHint
	if p := m.ActivePlugin(); p != nil {
		hints = m.pluginFooterHints(p, m.activeContext)
	}
	// Then essential global hints
	hints = append(hints, m.globalFooterHints()...)
	return hints
}

func (m Model) globalFooterHints() []footerHint {
	bindings := m.keymap.BindingsForContext("global")
	keysByCmd := bindingKeysByCommand(bindings)

	// Only essential global hints - plugin shortcuts are more relevant
	specs := []struct {
		id    string
		label string
	}{
		{id: "toggle-palette", label: "help"},
		{id: "quit", label: "quit"},
	}

	var hints []footerHint

	// Plugin switching hints (consolidated for brevity)
	hints = append(hints, footerHint{keys: "1-5", label: "plugins"})

	for _, spec := range specs {
		keys := keysByCmd[spec.id]
		if len(keys) == 0 {
			continue
		}
		hints = append(hints, footerHint{keys: keys[0], label: spec.label})
	}
	return hints
}

func (m Model) pluginFooterHints(p plugin.Plugin, context string) []footerHint {
	if context == "" || context == "global" {
		return nil
	}

	keysByCmd := bindingKeysByCommand(m.keymap.BindingsForContext(context))

	// Collect commands with their priorities
	type cmdWithPriority struct {
		cmd      plugin.Command
		keys     []string
		priority int
	}

	var cmds []cmdWithPriority
	for _, cmd := range p.Commands() {
		if cmd.Context != context {
			continue
		}
		keys := keysByCmd[cmd.ID]
		if len(keys) == 0 {
			continue
		}
		priority := cmd.Priority
		if priority == 0 {
			priority = 99 // Default to low priority
		}
		cmds = append(cmds, cmdWithPriority{cmd, keys, priority})
	}

	// Sort by priority (lower = more important, shown first)
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].priority < cmds[j].priority
	})

	var hints []footerHint
	for _, c := range cmds {
		hints = append(hints, footerHint{
			keys:  formatBindingKeys(c.keys),
			label: c.cmd.Name,
		})
	}
	return hints
}

func bindingKeysByCommand(bindings []keymap.Binding) map[string][]string {
	keysByCmd := make(map[string][]string, len(bindings))
	for _, b := range bindings {
		keysByCmd[b.Command] = append(keysByCmd[b.Command], b.Key)
	}
	return keysByCmd
}

func renderHintLine(hints []footerHint) string {
	if len(hints) == 0 {
		return ""
	}
	parts := make([]string, 0, len(hints))
	for _, hint := range hints {
		if hint.keys == "" || hint.label == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", styles.KeyHint.Render(hint.keys), hint.label))
	}
	return strings.Join(parts, "  ")
}

// renderHintLineTruncated renders hints but stops adding when maxWidth is exceeded.
func renderHintLineTruncated(hints []footerHint, maxWidth int) string {
	if len(hints) == 0 || maxWidth <= 0 {
		return ""
	}
	var result string
	separator := "  "
	for i, hint := range hints {
		if hint.keys == "" || hint.label == "" {
			continue
		}
		part := fmt.Sprintf("%s %s", styles.KeyHint.Render(hint.keys), hint.label)
		var candidate string
		if i == 0 {
			candidate = part
		} else {
			candidate = result + separator + part
		}
		if lipgloss.Width(candidate) > maxWidth {
			break // Stop adding hints if we exceed available width
		}
		result = candidate
	}
	return result
}

// renderHelpOverlay renders the help modal over content.
func (m Model) renderHelpOverlay(content string) string {
	help := m.buildHelpContent()
	modal := styles.ModalBox.Render(help)
	return ui.OverlayModal(content, modal, m.width, m.height)
}

// buildHelpContent creates the help modal content.
func (m Model) buildHelpContent() string {
	var b strings.Builder

	b.WriteString(styles.ModalTitle.Render("Keyboard Shortcuts"))
	b.WriteString("\n\n")

	// Global bindings
	b.WriteString(styles.Title.Render("Global"))
	b.WriteString("\n")
	m.renderBindingSection(&b, "global")
	b.WriteString("\n")

	// Active plugin bindings
	if p := m.ActivePlugin(); p != nil {
		ctx := p.FocusContext()
		if ctx != "global" && ctx != "" {
			bindings := m.keymap.BindingsForContext(ctx)
			if len(bindings) > 0 {
				b.WriteString(styles.Title.Render(p.Name()))
				b.WriteString("\n")
				m.renderBindingSection(&b, ctx)
				b.WriteString("\n")
			}
		}
	}

	b.WriteString(styles.Subtle.Render("Press ? or esc to close"))

	return b.String()
}

// renderBindingSection renders bindings for a context.
func (m Model) renderBindingSection(b *strings.Builder, context string) {
	bindings := m.keymap.BindingsForContext(context)

	// Group similar bindings
	seen := make(map[string]bool)
	for _, binding := range bindings {
		if seen[binding.Command] {
			continue
		}
		seen[binding.Command] = true

		// Find all keys for this command
		var keys []string
		for _, b2 := range bindings {
			if b2.Command == binding.Command {
				keys = append(keys, b2.Key)
			}
		}

		keyStr := formatBindingKeys(keys)
		cmdName := formatCommandName(binding.Command)

		// Pad key to align commands
		padded := fmt.Sprintf("%-11s", keyStr)
		b.WriteString(fmt.Sprintf("  %s %s\n", styles.Muted.Render(padded), cmdName))
	}
}

// formatBindingKeys formats multiple keys into a display string.
func formatBindingKeys(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	if len(keys) == 1 {
		return keys[0]
	}
	// Show up to 2 keys
	if len(keys) > 2 {
		keys = keys[:2]
	}
	return strings.Join(keys, ", ")
}

// formatCommandName converts a command ID to a display name.
func formatCommandName(cmd string) string {
	// Convert kebab-case to readable format
	name := strings.ReplaceAll(cmd, "-", " ")
	return name
}

// renderDiagnosticsOverlay renders the diagnostics modal.
func (m Model) renderDiagnosticsOverlay(content string) string {
	diag := m.buildDiagnosticsContent()
	modal := styles.ModalBox.Render(diag)
	return ui.OverlayModal(content, modal, m.width, m.height)
}

// buildDiagnosticsContent creates the diagnostics modal content.
func (m Model) buildDiagnosticsContent() string {
	var b strings.Builder

	logo := `
   _____ _     __                    
  / ___/(_)___/ /__  _________ ______
  \__ \/ / __  / _ \/ ___/ __ \/ ___/
 ___/ / / /_/ /  __/ /__/ /_/ / /    
/____/_/\__,_/\___/\___/\__,_/_/     
`
	b.WriteString(styles.Logo.Render(logo))
	b.WriteString("\n\n")

	// Plugins status
	b.WriteString(styles.Title.Render("Plugins"))
	b.WriteString("\n")

	plugins := m.registry.Plugins()
	for _, p := range plugins {
		status := styles.StatusCompleted.Render("✓")
		b.WriteString(fmt.Sprintf("  %s %s: active\n", status, p.Name()))

		// Check for plugin-specific diagnostics
		if dp, ok := p.(plugin.DiagnosticProvider); ok {
			for _, d := range dp.Diagnostics() {
				statusIcon := "•"
				switch d.Status {
				case "ok":
					statusIcon = styles.StatusCompleted.Render("•")
				case "warning":
					statusIcon = styles.StatusModified.Render("•")
				case "error":
					statusIcon = styles.StatusBlocked.Render("•")
				default:
					statusIcon = styles.Muted.Render("•")
				}
				b.WriteString(fmt.Sprintf("    %s %s\n", statusIcon, d.Detail))
			}
		}
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

	// System info
	b.WriteString(styles.Title.Render("System"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  WorkDir: %s\n", styles.Muted.Render(m.ui.WorkDir)))
	b.WriteString(fmt.Sprintf("  Refresh: %s\n", styles.Muted.Render(m.ui.LastRefresh.Format("15:04:05"))))
	b.WriteString("\n")

	// Version info
	b.WriteString(styles.Title.Render("Version"))
	b.WriteString("\n")

	// Sidecar version
	if m.updateAvailable != nil {
		b.WriteString(fmt.Sprintf("  sidecar: %s → %s ",
			styles.Muted.Render(m.currentVersion),
			m.updateAvailable.LatestVersion))
		b.WriteString(styles.StatusModified.Render("available"))
		b.WriteString("\n")
	} else {
		b.WriteString(fmt.Sprintf("  sidecar: %s ", styles.Muted.Render(m.currentVersion)))
		b.WriteString(styles.StatusCompleted.Render("✓"))
		b.WriteString("\n")
	}

	// td version
	if m.tdVersionInfo != nil {
		if !m.tdVersionInfo.Installed {
			b.WriteString(fmt.Sprintf("  td:      %s\n", styles.Muted.Render("not installed")))
		} else if m.tdVersionInfo.HasUpdate {
			b.WriteString(fmt.Sprintf("  td:      %s → %s ",
				styles.Muted.Render(m.tdVersionInfo.CurrentVersion),
				m.tdVersionInfo.LatestVersion))
			b.WriteString(styles.StatusModified.Render("available"))
			b.WriteString("\n")
		} else {
			b.WriteString(fmt.Sprintf("  td:      %s ", styles.Muted.Render(m.tdVersionInfo.CurrentVersion)))
			b.WriteString(styles.StatusCompleted.Render("✓"))
			b.WriteString("\n")
		}
	}

	// Show update controls if any updates available
	if m.updateAvailable != nil || (m.tdVersionInfo != nil && m.tdVersionInfo.HasUpdate) {
		b.WriteString("\n")

		if m.updateInProgress {
			spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
			spinner := spinnerFrames[m.updateSpinnerFrame%len(spinnerFrames)]
			b.WriteString("  ")
			b.WriteString(styles.StatusInProgress.Render(spinner + " "))
			b.WriteString("Installing update...")
			b.WriteString("\n")
		} else if m.needsRestart {
			b.WriteString("  ")
			b.WriteString(styles.StatusCompleted.Render("✓ "))
			b.WriteString("Update complete. ")
			b.WriteString(styles.StatusModified.Render("Restart sidecar to use new version"))
			b.WriteString("\n")
		} else {
			// Show Update button (click or press u)
			buttonStyle := styles.Button
			if m.updateButtonFocus {
				buttonStyle = styles.ButtonFocused
			}
			label := m.buildUpdateLabel()
			b.WriteString("  ")
			b.WriteString(buttonStyle.Render(" Update "))
			b.WriteString("  ")
			b.WriteString(styles.Muted.Render(label))
			b.WriteString("  ")
			b.WriteString(styles.KeyHint.Render("u"))
			b.WriteString("\n")
		}

		if m.updateError != "" {
			b.WriteString("  ")
			b.WriteString(styles.StatusBlocked.Render("✗ " + m.updateError))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	// Last error
	if m.lastError != nil {
		b.WriteString(styles.Title.Render("Last Error"))
		b.WriteString("\n")
		b.WriteString(styles.StatusBlocked.Render(fmt.Sprintf("  %s\n", m.lastError.Error())))
		b.WriteString("\n")
	}

	b.WriteString(styles.Subtle.Render("Press ! or esc to close"))

	return b.String()
}

// buildUpdateLabel returns a description of what will be updated.
func (m Model) buildUpdateLabel() string {
	var parts []string
	if m.updateAvailable != nil {
		parts = append(parts, "sidecar "+m.updateAvailable.LatestVersion)
	}
	if m.tdVersionInfo != nil && m.tdVersionInfo.HasUpdate && m.tdVersionInfo.Installed {
		parts = append(parts, "td "+m.tdVersionInfo.LatestVersion)
	}
	return strings.Join(parts, " + ")
}
