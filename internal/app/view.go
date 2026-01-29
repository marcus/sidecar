package app

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

const (
	headerHeight = 2 // header line + spacing
	footerHeight = 1
	minWidth     = 80
	minHeight    = 24

	projectSwitcherItemPrefix = "project-switcher-item-"
)

// projectSwitcherItemID returns the ID for a project item at the given index.
func projectSwitcherItemID(idx int) string {
	return fmt.Sprintf("%s%d", projectSwitcherItemPrefix, idx)
}

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

	// Overlay modals (priority order via activeModal)
	bg := b.String()
	switch m.activeModal() {
	case ModalPalette:
		return m.renderPaletteOverlay(bg)
	case ModalHelp:
		return m.renderHelpModal(bg)
	case ModalUpdate:
		// Render update modal, with optional changelog overlay
		updateView := m.renderUpdateModalOverlay(bg)
		if m.changelogVisible {
			return m.renderChangelogOverlay(updateView)
		}
		return updateView
	case ModalDiagnostics:
		return m.renderDiagnosticsModal(bg)
	case ModalQuitConfirm:
		return m.renderQuitConfirmOverlay(bg)
	case ModalProjectSwitcher:
		return m.renderProjectSwitcherOverlay(bg)
	case ModalWorktreeSwitcher:
		return m.renderWorktreeSwitcherModal(bg)
	case ModalThemeSwitcher:
		return m.renderThemeSwitcherModal(bg)
	case ModalIssueInput:
		return m.renderIssueInputOverlay(bg)
	case ModalIssuePreview:
		return m.renderIssuePreviewOverlay(bg)
	}

	return bg
}

// renderPaletteOverlay renders the command palette modal.
func (m Model) renderPaletteOverlay(content string) string {
	modal := m.palette.View()
	return ui.OverlayModal(content, modal, m.width, m.height)
}

// renderQuitConfirmOverlay renders the quit confirmation modal.
func (m Model) renderQuitConfirmOverlay(content string) string {
	// Lazy init modal if needed
	if m.quitModal == nil {
		// This shouldn't happen, but handle gracefully
		return content
	}
	rendered := m.quitModal.Render(m.width, m.height, m.quitMouseHandler)
	return ui.OverlayModal(content, rendered, m.width, m.height)
}

// ensureProjectSwitcherModal builds/rebuilds the project switcher modal.
func (m *Model) ensureProjectSwitcherModal() {
	modalW := 72
	if modalW > m.width-4 {
		modalW = m.width - 4
	}
	if modalW < 20 {
		modalW = 20
	}

	// Only rebuild if modal doesn't exist or width changed
	if m.projectSwitcherModal != nil && m.projectSwitcherModalWidth == modalW {
		return
	}
	m.projectSwitcherModalWidth = modalW

	m.projectSwitcherModal = modal.New("Switch Project",
		modal.WithWidth(modalW),
		modal.WithHints(false),
	).
		AddSection(m.projectSwitcherInputSection()).
		AddSection(m.projectSwitcherCountSection()).
		AddSection(m.projectSwitcherListSection()).
		AddSection(m.projectSwitcherHintsSection())
}

// projectSwitcherInputSection renders the filter input.
func (m *Model) projectSwitcherInputSection() modal.Section {
	return modal.Input("project-filter", &m.projectSwitcherInput)
}

// projectSwitcherCountSection renders the project count.
func (m *Model) projectSwitcherCountSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		allProjects := m.cfg.Projects.List
		projects := m.projectSwitcherFiltered

		var countText string
		if m.projectSwitcherInput.Value() != "" {
			countText = fmt.Sprintf("%d of %d projects", len(projects), len(allProjects))
		} else if len(allProjects) > 0 {
			countText = fmt.Sprintf("%d projects", len(allProjects))
		}
		return modal.RenderedSection{Content: styles.Muted.Render(countText)}
	}, nil)
}

// projectSwitcherListSection renders the project list with scroll.
func (m *Model) projectSwitcherListSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		allProjects := m.cfg.Projects.List
		projects := m.projectSwitcherFiltered

		var b strings.Builder

		// No projects configured
		if len(allProjects) == 0 {
			b.WriteString(styles.Muted.Render("No projects configured"))
			return modal.RenderedSection{Content: b.String()}
		}

		// Empty filtered state
		if len(projects) == 0 {
			b.WriteString(styles.Muted.Render("No matches"))
			return modal.RenderedSection{Content: b.String()}
		}

		maxVisible := 8
		visibleCount := len(projects)
		if visibleCount > maxVisible {
			visibleCount = maxVisible
		}
		scrollOffset := m.projectSwitcherScroll

		// Track focusables and line offset
		focusables := make([]modal.FocusableInfo, 0, visibleCount)
		lineOffset := 0

		if scrollOffset > 0 {
			b.WriteString(styles.Muted.Render(fmt.Sprintf("  ↑ %d more above", scrollOffset)))
			b.WriteString("\n")
			lineOffset++
		}

		cursorStyle := lipgloss.NewStyle().Foreground(styles.Primary)
		nameNormalStyle := lipgloss.NewStyle().Foreground(styles.Secondary)
		nameSelectedStyle := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
		nameCurrentStyle := lipgloss.NewStyle().Foreground(styles.Success).Bold(true)
		nameCurrentSelectedStyle := lipgloss.NewStyle().Foreground(styles.Success).Bold(true)

		for i := scrollOffset; i < scrollOffset+visibleCount && i < len(projects); i++ {
			project := projects[i]
			isCursor := i == m.projectSwitcherCursor
			isCurrent := project.Path == m.ui.WorkDir
			itemID := projectSwitcherItemID(i)
			isHovered := itemID == hoverID

			if isCursor {
				b.WriteString(cursorStyle.Render("> "))
			} else {
				b.WriteString("  ")
			}

			var nameStyle lipgloss.Style
			if isCurrent {
				if isCursor || isHovered {
					nameStyle = nameCurrentSelectedStyle
				} else {
					nameStyle = nameCurrentStyle
				}
			} else if isCursor || isHovered {
				nameStyle = nameSelectedStyle
			} else {
				nameStyle = nameNormalStyle
			}

			b.WriteString(nameStyle.Render(project.Name))
			if isCurrent {
				b.WriteString(styles.Muted.Render(" (current)"))
			}
			b.WriteString("\n")
			pathDisplay := project.Path
			maxPathLen := contentWidth - 4
			if len(pathDisplay) > maxPathLen {
				pathDisplay = "..." + pathDisplay[len(pathDisplay)-maxPathLen+3:]
			}
			b.WriteString(styles.Muted.Render("  " + pathDisplay))
			if i < scrollOffset+visibleCount-1 && i < len(projects)-1 {
				b.WriteString("\n")
			}

			// Each project takes 2 lines (name + path)
			focusables = append(focusables, modal.FocusableInfo{
				ID:      itemID,
				OffsetX: 0,
				OffsetY: lineOffset + (i-scrollOffset)*2,
				Width:   contentWidth,
				Height:  2,
			})
		}

		remaining := len(projects) - (scrollOffset + visibleCount)
		if remaining > 0 {
			b.WriteString("\n")
			b.WriteString(styles.Muted.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		}

		return modal.RenderedSection{Content: b.String(), Focusables: focusables}
	}, m.projectSwitcherListUpdate)
}

// projectSwitcherListUpdate handles key events for the project list.
func (m *Model) projectSwitcherListUpdate(msg tea.Msg, focusID string) (string, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", nil
	}

	projects := m.projectSwitcherFiltered
	if len(projects) == 0 {
		return "", nil
	}

	switch keyMsg.String() {
	case "up", "k", "ctrl+p":
		if m.projectSwitcherCursor > 0 {
			m.projectSwitcherCursor--
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			m.projectSwitcherModalWidth = 0 // Force modal rebuild for scroll
			m.previewProjectTheme()
		}
		return "", nil

	case "down", "j", "ctrl+n":
		if m.projectSwitcherCursor < len(projects)-1 {
			m.projectSwitcherCursor++
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			m.projectSwitcherModalWidth = 0 // Force modal rebuild for scroll
			m.previewProjectTheme()
		}
		return "", nil

	case "enter":
		if m.projectSwitcherCursor >= 0 && m.projectSwitcherCursor < len(projects) {
			return "select", nil
		}
		return "", nil
	}

	return "", nil
}

// projectSwitcherHintsSection renders the keyboard hints.
func (m *Model) projectSwitcherHintsSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		allProjects := m.cfg.Projects.List
		projects := m.projectSwitcherFiltered

		var b strings.Builder
		b.WriteString("\n")

		// No projects configured
		if len(allProjects) == 0 {
			b.WriteString(styles.KeyHint.Render("ctrl+a"))
			b.WriteString(styles.Muted.Render(" add  "))
			b.WriteString(styles.KeyHint.Render("y"))
			b.WriteString(styles.Muted.Render(" copy prompt  "))
			b.WriteString(styles.KeyHint.Render("esc"))
			b.WriteString(styles.Muted.Render(" close"))
			return modal.RenderedSection{Content: b.String()}
		}

		// Empty filtered state
		if len(projects) == 0 {
			b.WriteString(styles.KeyHint.Render("esc"))
			b.WriteString(styles.Muted.Render(" clear filter  "))
			b.WriteString(styles.KeyHint.Render("@"))
			b.WriteString(styles.Muted.Render(" close"))
			return modal.RenderedSection{Content: b.String()}
		}

		// Normal hints
		b.WriteString(styles.KeyHint.Render("enter"))
		b.WriteString(styles.Muted.Render(" switch  "))
		b.WriteString(styles.KeyHint.Render("↑/↓"))
		b.WriteString(styles.Muted.Render(" navigate  "))
		b.WriteString(styles.KeyHint.Render("ctrl+a"))
		b.WriteString(styles.Muted.Render(" add  "))
		b.WriteString(styles.KeyHint.Render("esc"))
		b.WriteString(styles.Muted.Render(" close"))
		return modal.RenderedSection{Content: b.String()}
	}, nil)
}

// renderProjectSwitcherOverlay renders the project switcher modal.
func (m *Model) renderProjectSwitcherOverlay(content string) string {
	m.ensureProjectSwitcherModal()
	if m.projectSwitcherModal == nil {
		return content
	}

	if m.projectSwitcherMouseHandler == nil {
		m.projectSwitcherMouseHandler = mouse.NewHandler()
	}
	modalContent := m.projectSwitcherModal.Render(m.width, m.height, m.projectSwitcherMouseHandler)
	base := ui.OverlayModal(content, modalContent, m.width, m.height)

	if m.projectAddMode {
		return m.renderProjectAddModal(base)
	}
	return base
}

// renderProjectAddThemePickerOverlay renders the theme picker within add-project.
func (m Model) renderProjectAddThemePickerOverlay(content string) string {
	var b strings.Builder
	maxVisible := 6
	cursorStyle := lipgloss.NewStyle().Foreground(styles.Primary)
	selectedStyle := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)

	if m.projectAddCommunityMode {
		// Community sub-browser
		b.WriteString(styles.ModalTitle.Render("Community Themes"))
		b.WriteString("\n\n")

		list := m.projectAddCommunityList
		visibleCount := len(list)
		if visibleCount > maxVisible {
			visibleCount = maxVisible
		}

		if m.projectAddCommunityScroll > 0 {
			b.WriteString(styles.Muted.Render("  ↑ more"))
			b.WriteString("\n")
		}

		for i := m.projectAddCommunityScroll; i < m.projectAddCommunityScroll+visibleCount && i < len(list); i++ {
			cursor := "  "
			nameStyle := styles.Muted
			if i == m.projectAddCommunityCursor {
				cursor = cursorStyle.Render("▸ ")
				nameStyle = selectedStyle
			}
			b.WriteString(cursor)
			b.WriteString(nameStyle.Render(list[i]))
			b.WriteString("\n")
		}

		if len(list) > m.projectAddCommunityScroll+visibleCount {
			b.WriteString(styles.Muted.Render("  ↓ more"))
			b.WriteString("\n")
		}

		b.WriteString("\n")
		b.WriteString(styles.KeyHint.Render("enter"))
		b.WriteString(styles.Muted.Render(" select  "))
		b.WriteString(styles.KeyHint.Render("tab"))
		b.WriteString(styles.Muted.Render(" built-in  "))
		b.WriteString(styles.KeyHint.Render("esc"))
		b.WriteString(styles.Muted.Render(" back"))
	} else {
		// Built-in theme list
		b.WriteString(styles.ModalTitle.Render("Pick Theme"))
		b.WriteString("\n\n")
		b.WriteString(m.projectAddThemeInput.View())
		b.WriteString("\n\n")

		list := m.projectAddThemeFiltered
		visibleCount := len(list)
		if visibleCount > maxVisible {
			visibleCount = maxVisible
		}

		if m.projectAddThemeScroll > 0 {
			b.WriteString(styles.Muted.Render("  ↑ more"))
			b.WriteString("\n")
		}

		for i := m.projectAddThemeScroll; i < m.projectAddThemeScroll+visibleCount && i < len(list); i++ {
			cursor := "  "
			nameStyle := styles.Muted
			if i == m.projectAddThemeCursor {
				cursor = cursorStyle.Render("▸ ")
				nameStyle = selectedStyle
			}
			b.WriteString(cursor)
			b.WriteString(nameStyle.Render(list[i]))
			b.WriteString("\n")
		}

		if len(list) > m.projectAddThemeScroll+visibleCount {
			b.WriteString(styles.Muted.Render("  ↓ more"))
			b.WriteString("\n")
		}

		b.WriteString("\n")
		b.WriteString(styles.KeyHint.Render("enter"))
		b.WriteString(styles.Muted.Render(" select  "))
		b.WriteString(styles.KeyHint.Render("tab"))
		b.WriteString(styles.Muted.Render(" community  "))
		b.WriteString(styles.KeyHint.Render("esc"))
		b.WriteString(styles.Muted.Render(" back"))
	}

	modal := styles.ModalBox.Render(b.String())
	return ui.OverlayModal(content, modal, m.width, m.height)
}

// renderHeader renders the top bar with title, tabs, and clock.
func (m Model) renderHeader() string {
	// Check if we're in a worktree for the indicator
	worktreeIndicator := ""
	if wtInfo := m.currentWorktreeInfo(); wtInfo != nil && !wtInfo.IsMain {
		// Show worktree branch name as indicator
		branchName := wtInfo.Branch
		if branchName == "" {
			branchName = "worktree"
		}
		worktreeIndicator = styles.WorktreeIndicator.Render(" [" + branchName + "]")
	}

	// Calculate final title width (with repo name and worktree indicator) - used for tab positioning
	finalTitleWidth := lipgloss.Width(styles.BarTitle.Render(" Sidecar"))
	if m.intro.RepoName != "" {
		finalTitleWidth += lipgloss.Width(styles.Subtitle.Render(" / " + m.intro.RepoName))
	}
	finalTitleWidth += lipgloss.Width(worktreeIndicator)
	finalTitleWidth += 1 // trailing space

	// Title with optional repo name and worktree indicator
	var title string
	if m.intro.Active {
		// During animation, render into fixed-width container to keep tabs stable
		titleContent := styles.BarTitle.Render(" "+m.intro.View()) + m.intro.RepoNameView() + worktreeIndicator + " "
		title = lipgloss.NewStyle().Width(finalTitleWidth).Render(titleContent)
	} else {
		// Static title with repo name and worktree indicator
		repoSuffix := ""
		if m.intro.RepoName != "" {
			repoSuffix = styles.Subtitle.Render(" / " + m.intro.RepoName)
		}
		title = styles.BarTitle.Render(" Sidecar") + repoSuffix + worktreeIndicator + " "
	}

	// Plugin tabs (themed)
	plugins := m.registry.Plugins()
	var tabs []string
	for i, p := range plugins {
		isActive := i == m.activePlugin
		tab := styles.RenderTab(p.Name(), i, len(plugins), isActive, false)
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
	// Add worktree indicator width if applicable
	if wtInfo := m.currentWorktreeInfo(); wtInfo != nil && !wtInfo.IsMain {
		branchName := wtInfo.Branch
		if branchName == "" {
			branchName = "worktree"
		}
		titleWidth += lipgloss.Width(styles.WorktreeIndicator.Render(" [" + branchName + "]"))
	}
	titleWidth += 1 // trailing space

	// Calculate tab widths (using themed renderer)
	plugins := m.registry.Plugins()
	var tabWidths []int
	totalTabWidth := 0
	for i, p := range plugins {
		isActive := i == m.activePlugin
		tab := styles.RenderTab(p.Name(), i, len(plugins), isActive, false)
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

// ensureHelpModal builds/rebuilds the help modal.
func (m *Model) ensureHelpModal() {
	modalW := 60
	if modalW > m.width-4 {
		modalW = m.width - 4
	}
	if modalW < 20 {
		modalW = 20
	}

	// Only rebuild if modal doesn't exist or width changed
	if m.helpModal != nil && m.helpModalWidth == modalW {
		return
	}
	m.helpModalWidth = modalW

	m.helpModal = modal.New("Keyboard Shortcuts",
		modal.WithWidth(modalW),
		modal.WithHints(false),
	).
		AddSection(m.helpGlobalSection()).
		AddSection(m.helpPluginSection())
}

// clearHelpModal clears the help modal state.
func (m *Model) clearHelpModal() {
	m.helpModal = nil
	m.helpModalWidth = 0
	m.helpMouseHandler = nil
}

// helpGlobalSection renders the global bindings section.
func (m *Model) helpGlobalSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		var b strings.Builder
		b.WriteString(styles.Title.Render("Global"))
		b.WriteString("\n")
		m.renderBindingSection(&b, "global")
		return modal.RenderedSection{Content: b.String()}
	}, nil)
}

// helpPluginSection renders the active plugin bindings section.
func (m *Model) helpPluginSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p := m.ActivePlugin(); p != nil {
			ctx := p.FocusContext()
			if ctx != "global" && ctx != "" {
				bindings := m.keymap.BindingsForContext(ctx)
				if len(bindings) > 0 {
					var b strings.Builder
					b.WriteString(styles.Title.Render(p.Name()))
					b.WriteString("\n")
					m.renderBindingSection(&b, ctx)
					return modal.RenderedSection{Content: b.String()}
				}
			}
		}
		return modal.RenderedSection{}
	}, nil)
}

// renderHelpModal renders the help modal.
func (m *Model) renderHelpModal(content string) string {
	m.ensureHelpModal()
	if m.helpModal == nil {
		return content
	}

	if m.helpMouseHandler == nil {
		m.helpMouseHandler = mouse.NewHandler()
	}
	modalContent := m.helpModal.Render(m.width, m.height, m.helpMouseHandler)
	return ui.OverlayModal(content, modalContent, m.width, m.height)
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
