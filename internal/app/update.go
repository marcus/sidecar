package app

import (
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/palette"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/version"
)

// Update handles all messages and returns the updated model and commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		// Update button bounds if diagnostics is shown
		if m.showDiagnostics {
			m.updateDiagnosticsButtonBounds()
		}
		// Forward adjusted WindowSizeMsg to all plugins
		// Plugins receive the content area size (minus header and footer)
		// Must match the height passed to Plugin.View() in view.go
		adjustedHeight := msg.Height - headerHeight
		if m.showFooter {
			adjustedHeight -= footerHeight
		}
		adjustedMsg := tea.WindowSizeMsg{
			Width:  msg.Width,
			Height: adjustedHeight,
		}
		plugins := m.registry.Plugins()
		var cmds []tea.Cmd
		for i, p := range plugins {
			newPlugin, cmd := p.Update(adjustedMsg)
			plugins[i] = newPlugin
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case tea.MouseMsg:
		// Forward mouse events to palette when it's open
		if m.showPalette {
			var cmd tea.Cmd
			m.palette, cmd = m.palette.Update(msg)
			return m, cmd
		}

		// Handle diagnostics modal mouse events
		if m.showDiagnostics {
			if m.hasUpdatesAvailable() && !m.updateInProgress && !m.needsRestart {
				// Check if click is on update button
				if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
					if m.updateButtonBounds.Contains(msg.X, msg.Y) {
						m.updateInProgress = true
						m.updateError = ""
						m.updateSpinnerFrame = 0
						return m, tea.Batch(m.doUpdate(), updateSpinnerTick())
					}
				}
			}
			return m, nil
		}

		// Handle project switcher modal mouse events
		if m.showProjectSwitcher {
			return m.handleProjectSwitcherMouse(msg)
		}

		// Ignore mouse events for other modals
		if m.showHelp || m.showQuitConfirm {
			return m, nil
		}

		// Handle header tab clicks (Y < 2 means header area)
		if msg.Y < headerHeight && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if start, end, ok := m.getRepoNameBounds(); ok && msg.X >= start && msg.X < end {
				m.showProjectSwitcher = true
				m.activeContext = "project-switcher"
				m.initProjectSwitcher()
				return m, nil
			}

			// Check if click is on a tab
			tabBounds := m.getTabBounds()
			for i, bounds := range tabBounds {
				if msg.X >= bounds.Start && msg.X < bounds.End {
					return m, m.SetActivePlugin(i)
				}
			}
			return m, nil
		}

		// Forward mouse events to active plugin with Y offset for app header (2 lines)
		if p := m.ActivePlugin(); p != nil {
			adjusted := tea.MouseMsg{
				X:      msg.X,
				Y:      msg.Y - headerHeight, // Offset for app header
				Button: msg.Button,
				Action: msg.Action,
				Ctrl:   msg.Ctrl,
				Alt:    msg.Alt,
				Shift:  msg.Shift,
			}
			newPlugin, cmd := p.Update(adjusted)
			plugins := m.registry.Plugins()
			if m.activePlugin < len(plugins) {
				plugins[m.activePlugin] = newPlugin
			}
			m.updateContext()
			return m, cmd
		}
		return m, nil

	case IntroTickMsg:
		if m.intro.Active {
			m.intro.Update(16 * time.Millisecond)
			// Keep ticking until logo done AND repo name fully faded in
			if !m.intro.Done || m.intro.RepoOpacity < 1.0 {
				return m, IntroTick()
			}
			// All animations complete
			return m, Refresh()
		}
		return m, nil

	case TickMsg:
		m.ui.UpdateClock()
		m.ui.ClearExpiredToast()
		m.ClearToast()
		return m, tickCmd()

	case UpdateSpinnerTickMsg:
		if m.updateInProgress {
			m.updateSpinnerFrame = (m.updateSpinnerFrame + 1) % 10
			return m, updateSpinnerTick()
		}
		return m, nil

	case ToastMsg:
		m.ShowToast(msg.Message, msg.Duration)
		m.statusIsError = msg.IsError
		return m, nil

	case appmsg.ToastMsg:
		m.ShowToast(msg.Message, msg.Duration)
		m.statusIsError = msg.IsError
		return m, nil

	case RefreshMsg:
		m.ui.MarkRefresh()
		// Refresh active plugin
		if p := m.ActivePlugin(); p != nil {
			_, cmd := p.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case ErrorMsg:
		m.lastError = msg.Err
		m.ShowToast("Error: "+msg.Err.Error(), 5*time.Second)
		return m, nil

	case UpdateSuccessMsg:
		m.updateInProgress = false
		m.needsRestart = true
		if msg.SidecarUpdated {
			m.updateAvailable = nil
		}
		if msg.TdUpdated && m.tdVersionInfo != nil {
			m.tdVersionInfo.HasUpdate = false
		}
		m.ShowToast("Update complete! Restart sidecar to use new version", 10*time.Second)
		return m, nil

	case UpdateErrorMsg:
		m.updateInProgress = false
		m.updateError = fmt.Sprintf("Failed to update %s: %s", msg.Step, msg.Err)
		m.ShowToast("Update failed: "+msg.Err.Error(), 5*time.Second)
		m.statusIsError = true
		return m, nil

	case FocusPluginByIDMsg:
		// Switch to requested plugin
		return m, m.FocusPluginByID(msg.PluginID)

	case plugin.OpenFileMsg:
		// Open file in editor using tea.ExecProcess
		// Most editors support +lineNo syntax for opening at a line
		args := []string{}
		if msg.LineNo > 0 {
			args = append(args, fmt.Sprintf("+%d", msg.LineNo))
		}
		args = append(args, msg.Path)
		c := exec.Command(msg.Editor, args...)
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return EditorReturnedMsg{Err: err}
		})

	case EditorReturnedMsg:
		// After editor exits, re-enable mouse and trigger refresh
		// tea.ExecProcess disables mouse, need to restore it
		cmds := []tea.Cmd{
			func() tea.Msg { return tea.EnableMouseAllMotion() },
		}
		if msg.Err != nil {
			cmds = append(cmds, func() tea.Msg { return ErrorMsg{Err: msg.Err} })
		} else {
			cmds = append(cmds, func() tea.Msg { return RefreshMsg{} })
		}
		return m, tea.Batch(cmds...)

	case palette.CommandSelectedMsg:
		// Execute the selected command from the palette
		m.showPalette = false
		m.updateContext()
		// Look up and execute the command
		if cmd, ok := m.keymap.GetCommand(msg.CommandID); ok && cmd.Handler != nil {
			return m, cmd.Handler()
		}
		return m, nil

	case version.UpdateAvailableMsg:
		m.updateAvailable = &msg
		m.ShowToast(
			fmt.Sprintf("Update %s available! Press ! for details", msg.LatestVersion),
			15*time.Second,
		)
		return m, nil

	case version.TdVersionMsg:
		m.tdVersionInfo = &msg
		// Show toast if td has an update (only if sidecar doesn't also have one)
		if msg.HasUpdate && m.updateAvailable == nil {
			m.ShowToast(
				fmt.Sprintf("td update %s available! Press ! for details", msg.LatestVersion),
				15*time.Second,
			)
		}
		return m, nil
	}

	// Forward other messages to ALL plugins (not just active)
	// This ensures plugin-specific messages (like SessionsLoadedMsg) reach
	// their target plugin even when another plugin is focused
	plugins := m.registry.Plugins()
	for i, p := range plugins {
		newPlugin, cmd := p.Update(msg)
		plugins[i] = newPlugin
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if !m.showHelp && !m.showDiagnostics {
		m.updateContext()
	}

	return m, tea.Batch(cmds...)
}

// handleKeyMsg processes keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Close modals with escape
	if msg.Type == tea.KeyEsc {
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		if m.showDiagnostics {
			m.showDiagnostics = false
			return m, nil
		}
		if m.showQuitConfirm {
			m.showQuitConfirm = false
			return m, nil
		}
		if m.showProjectSwitcher {
			m.resetProjectSwitcher()
			m.updateContext()
			return m, nil
		}
	}

	if m.showQuitConfirm {
		if msg.String() == "y" || msg.Type == tea.KeyEnter {
			// Save active plugin before quitting
			if activePlugin := m.ActivePlugin(); activePlugin != nil {
				state.SetActivePlugin(m.ui.WorkDir, activePlugin.ID())
			}
			m.registry.Stop()
			return m, tea.Quit
		}
		if msg.String() == "n" {
			m.showQuitConfirm = false
			return m, nil
		}
		return m, nil
	}

	// Text input contexts: forward all keys to plugin except ctrl+c
	// This ensures typing works correctly in commit messages, search boxes, etc.
	if m.activeContext == "git-commit" {
		// ctrl+c shows quit confirmation
		if msg.String() == "ctrl+c" {
			if !m.showHelp && !m.showDiagnostics && !m.showPalette {
				m.showQuitConfirm = true
			}
			return m, nil
		}
		// Forward everything else to plugin (esc, alt+enter handled by plugin)
		if p := m.ActivePlugin(); p != nil {
			newPlugin, cmd := p.Update(msg)
			plugins := m.registry.Plugins()
			if m.activePlugin < len(plugins) {
				plugins[m.activePlugin] = newPlugin
			}
			m.updateContext()
			return m, cmd
		}
		return m, nil
	}

	// Global quit - ctrl+c always takes precedence, 'q' in root plugin contexts
	switch msg.String() {
	case "ctrl+c":
		if !m.showHelp && !m.showDiagnostics && !m.showPalette {
			m.showQuitConfirm = true
			return m, nil
		}
	case "q":
		// 'q' triggers quit in root contexts where it's not used for navigation
		// Root contexts: global, or plugin root views (not sub-views like detail/diff)
		if !m.showHelp && !m.showDiagnostics && !m.showPalette && isRootContext(m.activeContext) {
			m.showQuitConfirm = true
			return m, nil
		}
		// Fall through to forward to plugin for navigation (back/escape)
	}

	// Handle palette input when open
	if m.showPalette {
		if msg.Type == tea.KeyEsc {
			m.showPalette = false
			m.updateContext()
			return m, nil
		}
		// Forward to palette
		var cmd tea.Cmd
		m.palette, cmd = m.palette.Update(msg)
		return m, cmd
	}

	// Handle diagnostics modal keys
	if m.showDiagnostics {
		switch msg.String() {
		case "u":
			if m.hasUpdatesAvailable() && !m.updateInProgress && !m.needsRestart {
				m.updateInProgress = true
				m.updateError = ""
				m.updateSpinnerFrame = 0
				return m, tea.Batch(m.doUpdate(), updateSpinnerTick())
			}
		case "tab":
			if m.hasUpdatesAvailable() && !m.updateInProgress && !m.needsRestart {
				m.updateButtonFocus = !m.updateButtonFocus
			}
		case "enter":
			if m.updateButtonFocus && !m.updateInProgress {
				m.updateInProgress = true
				m.updateError = ""
				m.updateSpinnerFrame = 0
				return m, tea.Batch(m.doUpdate(), updateSpinnerTick())
			}
		}
		return m, nil
	}

	// Handle project switcher modal keys
	if m.showProjectSwitcher {
		allProjects := m.cfg.Projects.List
		if len(allProjects) == 0 {
			// No projects configured - handle y for LLM prompt, close on esc/q/@
			switch msg.String() {
			case "y":
				return m, m.copyProjectSetupPrompt()
			case "q", "@":
				m.resetProjectSwitcher()
				m.updateContext()
			}
			if msg.Type == tea.KeyEsc {
				m.resetProjectSwitcher()
				m.updateContext()
			}
			return m, nil
		}

		projects := m.projectSwitcherFiltered

		switch msg.Type {
		case tea.KeyEsc:
			// Esc: clear filter if set, otherwise close modal
			if m.projectSwitcherInput.Value() != "" {
				m.projectSwitcherInput.SetValue("")
				m.projectSwitcherFiltered = allProjects
				m.projectSwitcherCursor = 0
				m.projectSwitcherScroll = 0
				return m, nil
			}
			m.resetProjectSwitcher()
			m.updateContext()
			return m, nil

		case tea.KeyEnter:
			// Select project and switch to it
			if m.projectSwitcherCursor >= 0 && m.projectSwitcherCursor < len(projects) {
				selectedProject := projects[m.projectSwitcherCursor]
				m.resetProjectSwitcher()
				m.updateContext()
				return m, m.switchProject(selectedProject.Path)
			}
			return m, nil

		case tea.KeyUp:
			m.projectSwitcherCursor--
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			return m, nil

		case tea.KeyDown:
			m.projectSwitcherCursor++
			if m.projectSwitcherCursor >= len(projects) {
				m.projectSwitcherCursor = len(projects) - 1
			}
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			return m, nil
		}

		// Handle string-based keys
		switch msg.String() {
		case "j", "ctrl+n":
			m.projectSwitcherCursor++
			if m.projectSwitcherCursor >= len(projects) {
				m.projectSwitcherCursor = len(projects) - 1
			}
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			return m, nil

		case "k", "ctrl+p":
			m.projectSwitcherCursor--
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			return m, nil

		case "g":
			// Go to first project
			m.projectSwitcherCursor = 0
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			return m, nil

		case "G":
			// Go to last project
			m.projectSwitcherCursor = len(projects) - 1
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			return m, nil

		case "@":
			// Close modal
			m.resetProjectSwitcher()
			m.updateContext()
			return m, nil

		case "y":
			// Copy LLM setup prompt (less prominent when projects exist)
			return m, m.copyProjectSetupPrompt()
		}

		// Forward other keys to text input for filtering
		var cmd tea.Cmd
		m.projectSwitcherInput, cmd = m.projectSwitcherInput.Update(msg)

		// Re-filter on input change
		m.projectSwitcherFiltered = filterProjects(allProjects, m.projectSwitcherInput.Value())
		m.projectSwitcherHover = -1 // Clear hover on filter change
		// Reset cursor if it's beyond filtered list
		if m.projectSwitcherCursor >= len(m.projectSwitcherFiltered) {
			m.projectSwitcherCursor = len(m.projectSwitcherFiltered) - 1
		}
		if m.projectSwitcherCursor < 0 {
			m.projectSwitcherCursor = 0
		}
		m.projectSwitcherScroll = 0
		m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)

		return m, cmd
	}

	// If modal is open, don't process other keys
	if m.showHelp || m.showQuitConfirm {
		return m, nil
	}

	// Plugin switching
	switch msg.String() {
	case "`":
		// Backtick cycles to next plugin (except in text input contexts)
		if isTextInputContext(m.activeContext) {
			break
		}
		return m, m.NextPlugin()
	case "~":
		// Tilde cycles to previous plugin (except in text input contexts)
		if isTextInputContext(m.activeContext) {
			break
		}
		return m, m.PrevPlugin()
	case "1", "2", "3", "4", "5":
		// Number keys for direct plugin switching
		// Block in text input contexts (user is typing numbers)
		if isTextInputContext(m.activeContext) {
			break
		}
		idx := int(msg.Runes[0] - '1')
		return m, m.SetActivePlugin(idx)
	}

	// Toggles
	switch msg.String() {
	case "?":
		m.showPalette = !m.showPalette
		if m.showPalette {
			// Open palette with current context
			pluginCtx := "global"
			if p := m.ActivePlugin(); p != nil {
				pluginCtx = p.ID()
			}
			m.palette.SetSize(m.width, m.height)
			m.palette.Open(m.keymap, m.registry.Plugins(), m.activeContext, pluginCtx)
			m.activeContext = "palette"
		} else {
			m.updateContext()
		}
		return m, nil
	case "!":
		m.showDiagnostics = !m.showDiagnostics
		if m.showDiagnostics {
			m.activeContext = "diagnostics"
			m.updateDiagnosticsButtonBounds()
			// Force version check in background (bypasses cache)
			return m, tea.Batch(
				version.ForceCheckAsync(m.currentVersion),
				version.ForceCheckTdAsync(),
			)
		}
		m.updateButtonFocus = false
		m.updateContext()
		return m, nil
	case "@":
		// Toggle project switcher modal
		m.showProjectSwitcher = !m.showProjectSwitcher
		if m.showProjectSwitcher {
			m.activeContext = "project-switcher"
			m.initProjectSwitcher()
		} else {
			m.resetProjectSwitcher()
			m.updateContext()
		}
		return m, nil
	case "ctrl+h":
		m.showFooter = !m.showFooter
		// Notify plugins of changed content area height
		adjustedHeight := m.height - headerHeight
		if m.showFooter {
			adjustedHeight -= footerHeight
		}
		sizeMsg := tea.WindowSizeMsg{Width: m.width, Height: adjustedHeight}
		plugins := m.registry.Plugins()
		var cmds []tea.Cmd
		for i, p := range plugins {
			newPlugin, cmd := p.Update(sizeMsg)
			plugins[i] = newPlugin
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)
	case "r":
		// Forward 'r' to plugin in contexts where it's used for specific actions
		// or where the user is typing text input
		if !isGlobalRefreshContext(m.activeContext) {
			// Fall through to forward to plugin
			break
		}
		return m, Refresh()
	}

	// Try keymap for context-specific bindings
	if cmd := m.keymap.Handle(msg, m.activeContext); cmd != nil {
		return m, cmd
	}

	// Forward to active plugin
	if p := m.ActivePlugin(); p != nil {
		newPlugin, cmd := p.Update(msg)
		plugins := m.registry.Plugins()
		if m.activePlugin < len(plugins) {
			plugins[m.activePlugin] = newPlugin
		}
		m.updateContext()
		return m, cmd
	}

	return m, nil
}

// updateContext sets activeContext based on current state.
func (m *Model) updateContext() {
	if p := m.ActivePlugin(); p != nil {
		m.activeContext = p.FocusContext()
	} else {
		m.activeContext = "global"
	}
}

// isRootContext returns true if the context is a root view where 'q' should quit.
// Root contexts are plugin top-level views (not sub-views like detail/diff/commit).
func isRootContext(ctx string) bool {
	switch ctx {
	case "global", "":
		return true
	// Plugin root contexts where 'q' is not used for navigation
	case "conversations", "conversations-sidebar", "conversations-main":
		return true
	case "git-status", "git-status-commits", "git-status-diff", "git-commit-preview":
		return true
	case "file-browser-tree", "file-browser-preview":
		return true
	case "worktree-list", "worktree-preview":
		return true
	case "td-monitor", "td-board":
		return true
	default:
		return false
	}
}

// isTextInputContext returns true if the context is a text input mode
// where alphanumeric keys should be forwarded to the plugin for typing.
func isTextInputContext(ctx string) bool {
	switch ctx {
	case "git-commit",
		"conversations-search", "conversations-filter",
		"file-browser-search", "file-browser-content-search",
		"file-browser-quick-open", "file-browser-file-op",
		"file-browser-project-search",
		"file-browser-line-jump",
		"td-search",
		"worktree-create", "worktree-task-link":
		return true
	default:
		return false
	}
}

// isGlobalRefreshContext returns true if 'r' should trigger a global refresh.
// Returns false for contexts where 'r' should be forwarded to the plugin
// (text input modes or plugin-specific 'r' bindings).
func isGlobalRefreshContext(ctx string) bool {
	switch ctx {
	// Global context - 'r' refreshes
	case "global", "":
		return true

	// Git status contexts - 'r' refreshes (no text input, no 'r' binding)
	case "git-status", "git-history", "git-commit-detail", "git-diff":
		return true

	// Conversations list - 'r' refreshes (no text input, no 'r' binding)
	case "conversations", "conversation-detail", "message-detail":
		return true

	// File browser preview - 'r' refreshes (no text input)
	case "file-browser-preview":
		return true

	// Contexts where 'r' should be forwarded to plugin:
	// - td-monitor: 'r' is mark-review
	// - file-browser-tree: 'r' is rename
	// - file-browser-search: text input mode
	// - file-browser-content-search: text input mode
	// - file-browser-quick-open: text input mode
	// - file-browser-file-op: text input mode
	// - conversations-search: text input mode
	// - conversations-filter: text input mode
	// - git-commit: text input mode (commit message)
	// - td-modal: modal view
	// - palette: command palette
	// - diagnostics: diagnostics view
	default:
		return false
	}
}

// handleProjectSwitcherMouse handles mouse events for the project switcher modal.
func (m Model) handleProjectSwitcherMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	allProjects := m.cfg.Projects.List
	if len(allProjects) == 0 {
		// No projects, close on click
		if msg.Action == tea.MouseActionPress {
			m.resetProjectSwitcher()
			m.updateContext()
		}
		return m, nil
	}

	// Use filtered list
	projects := m.projectSwitcherFiltered

	// Calculate modal dimensions and position
	// This should roughly match the modal rendered in view.go
	maxVisible := 8
	visibleCount := len(projects)
	if visibleCount > maxVisible {
		visibleCount = maxVisible
	}

	// Estimate modal dimensions (title + input + count + projects + help text)
	// Title: 2 lines, Input: 1 line, Count: 1 line, Projects: 2 lines each, Help: 2 lines
	modalContentLines := 2 + 1 + 1 + visibleCount*2 + 2
	if m.projectSwitcherScroll > 0 {
		modalContentLines++ // scroll indicator above
	}
	if len(projects) > m.projectSwitcherScroll+visibleCount {
		modalContentLines++ // scroll indicator below
	}
	// Empty state takes less space
	if len(projects) == 0 {
		modalContentLines = 2 + 1 + 1 + 2 + 2 // title + input + count + "no matches" + help
	}

	// ModalBox adds padding and border (~2 on each side)
	modalHeight := modalContentLines + 4
	modalWidth := 50 // Rough estimate

	modalX := (m.width - modalWidth) / 2
	modalY := (m.height - modalHeight) / 2

	// Check if click is inside modal
	if msg.X >= modalX && msg.X < modalX+modalWidth &&
		msg.Y >= modalY && msg.Y < modalY+modalHeight {

		// If no filtered projects, don't try to select
		if len(projects) == 0 {
			return m, nil
		}

		// Calculate which project was clicked
		// Content starts at modalY + 2 (border + padding)
		// Title: 2 lines, Input: 1 line, Count: 1 line, then scroll indicator (if any), then projects
		contentStartY := modalY + 2 + 2 + 1 + 1 // border/padding + title + input + count
		if m.projectSwitcherScroll > 0 {
			contentStartY++ // scroll indicator
		}

		// Each project takes 2 lines
		relY := msg.Y - contentStartY
		if relY >= 0 && relY < visibleCount*2 {
			projectIdx := m.projectSwitcherScroll + relY/2

			if projectIdx >= 0 && projectIdx < len(projects) {
				switch msg.Action {
				case tea.MouseActionPress:
					if msg.Button == tea.MouseButtonLeft {
						// Click to select and switch
						selectedProject := projects[projectIdx]
						m.resetProjectSwitcher()
						m.updateContext()
						return m, m.switchProject(selectedProject.Path)
					}
				case tea.MouseActionMotion:
					// Hover effect
					m.projectSwitcherHover = projectIdx
				}
			}
		} else {
			// Not hovering over a project
			if msg.Action == tea.MouseActionMotion {
				m.projectSwitcherHover = -1
			}
		}

		// Handle scroll wheel
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.projectSwitcherCursor--
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			// Update scroll if cursor goes above visible area
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, maxVisible)
		case tea.MouseButtonWheelDown:
			m.projectSwitcherCursor++
			if m.projectSwitcherCursor >= len(projects) {
				m.projectSwitcherCursor = len(projects) - 1
			}
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			// Update scroll if cursor goes below visible area
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, maxVisible)
		}

		return m, nil
	}

	// Click outside modal - close it
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		m.resetProjectSwitcher()
		m.updateContext()
		return m, nil
	}

	// Clear hover when outside modal
	if msg.Action == tea.MouseActionMotion {
		m.projectSwitcherHover = -1
	}

	return m, nil
}
