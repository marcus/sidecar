package app

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/community"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/mouse"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/palette"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/theme"
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
		// Reset diagnostics modal on resize (will be rebuilt on next render)
		if m.showDiagnostics {
			m.diagnosticsModalWidth = 0
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
		// Route mouse events to active modal (priority order)
		switch m.activeModal() {
		case ModalPalette:
			var cmd tea.Cmd
			m.palette, cmd = m.palette.Update(msg)
			return m, cmd
		case ModalHelp:
			return m.handleHelpModalMouse(msg)
		case ModalDiagnostics:
			return m.handleDiagnosticsModalMouse(msg)
		case ModalQuitConfirm:
			return m.handleQuitConfirmMouse(msg)
		case ModalProjectSwitcher:
			if m.projectAddMode {
				return m.handleProjectAddModalMouse(msg)
			}
			return m.handleProjectSwitcherMouse(msg)
		case ModalThemeSwitcher:
			if m.showCommunityBrowser {
				return m.handleCommunityBrowserMouse(msg)
			}
			return m.handleThemeSwitcherMouse(msg)
		}

		// Handle header tab clicks (Y < 2 means header area)
		if msg.Y < headerHeight && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if start, end, ok := m.getRepoNameBounds(); ok && !m.intro.Active && msg.X >= start && msg.X < end {
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
			// All animations complete - mark intro as inactive so header clicks work
			m.intro.Active = false
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
	// Close modals with escape (priority order via activeModal)
	if msg.Type == tea.KeyEsc {
		switch m.activeModal() {
		case ModalPalette:
			m.showPalette = false
			m.updateContext()
			return m, nil
		case ModalHelp:
			m.showHelp = false
			m.clearHelpModal()
			return m, nil
		case ModalDiagnostics:
			m.showDiagnostics = false
			return m, nil
		case ModalQuitConfirm:
			m.showQuitConfirm = false
			return m, nil
		case ModalProjectSwitcher:
			// If in add mode, Esc exits back to list
			if m.projectAddMode {
				m.resetProjectAdd()
				return m, nil
			}
			// Esc: clear filter if set, otherwise close
			if m.projectSwitcherInput.Value() != "" {
				m.projectSwitcherInput.SetValue("")
				m.projectSwitcherFiltered = m.cfg.Projects.List
				m.projectSwitcherCursor = 0
				m.projectSwitcherScroll = 0
				return m, nil
			}
			m.resetProjectSwitcher()
			m.updateContext()
			return m, nil
		case ModalThemeSwitcher:
			if m.showCommunityBrowser {
				// Esc in community browser: clear filter or return to built-in view
				if m.communityBrowserInput.Value() != "" {
					m.communityBrowserInput.SetValue("")
					m.communityBrowserFiltered = community.ListSchemes()
					m.communityBrowserCursor = 0
					m.communityBrowserScroll = 0
					return m, nil
				}
				m.applyThemeFromConfig(m.communityBrowserOriginal)
				m.resetCommunityBrowser()
				return m, nil
			}
			// Esc: clear filter if set, otherwise close (restore original)
			if m.themeSwitcherInput.Value() != "" {
				m.themeSwitcherInput.SetValue("")
				m.themeSwitcherFiltered = styles.ListThemes()
				m.themeSwitcherSelectedIdx = 0
				m.themeSwitcherSelectedIdx = 0
				return m, nil
			}
			m.applyThemeFromConfig(m.themeSwitcherOriginal)
			m.resetThemeSwitcher()
			m.updateContext()
			return m, nil
		}
	}

	if m.showQuitConfirm {
		action, cmd := m.quitModal.HandleKey(msg)
		switch action {
		case "quit":
			// Save active plugin before quitting
			if activePlugin := m.ActivePlugin(); activePlugin != nil {
				state.SetActivePlugin(m.ui.WorkDir, activePlugin.ID())
			}
			m.registry.Stop()
			return m, tea.Quit
		case "cancel":
			m.showQuitConfirm = false
			return m, nil
		}
		return m, cmd
	}

	// Interactive mode: forward ALL keys to plugin including ctrl+c
	// This ensures characters like `, ~, ?, !, @, 1-5 reach tmux instead of triggering app shortcuts
	// Ctrl+C is forwarded to tmux (to interrupt running processes) instead of showing quit dialog
	// User can exit interactive mode with Ctrl+\ first, then quit normally
	if m.activeContext == "workspace-interactive" {
		// Forward ALL keys to plugin (exit keys and ctrl+c handled by plugin)
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

	// Text input contexts: forward all keys to plugin except ctrl+c
	// This ensures typing works correctly in commit messages, search boxes, etc.
	if m.activeContext == "git-commit" {
		// ctrl+c shows quit confirmation
		if msg.String() == "ctrl+c" {
			if !m.hasModal() {
				m.initQuitModal()
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
		if !m.hasModal() {
			m.initQuitModal()
			m.showQuitConfirm = true
			return m, nil
		}
	case "q":
		if !m.hasModal() && isRootContext(m.activeContext) {
			m.initQuitModal()
			m.showQuitConfirm = true
			return m, nil
		}
		// Fall through to forward to plugin for navigation (back/escape)
	}

	// Handle palette input when open (Esc handled above)
	if m.showPalette {
		var cmd tea.Cmd
		m.palette, cmd = m.palette.Update(msg)
		return m, cmd
	}

	// Handle diagnostics modal keys
	if m.showDiagnostics {
		m.ensureDiagnosticsModal()
		if m.diagnosticsModal != nil {
			action, cmd := m.diagnosticsModal.HandleKey(msg)
			if cmd != nil {
				return m, cmd
			}
			switch action {
			case "update":
				if !m.updateInProgress && !m.needsRestart {
					m.updateInProgress = true
					m.updateError = ""
					m.updateSpinnerFrame = 0
					return m, tea.Batch(m.doUpdate(), updateSpinnerTick())
				}
			}
		}
		// Handle 'u' shortcut for update
		if msg.String() == "u" && m.hasUpdatesAvailable() && !m.updateInProgress && !m.needsRestart {
			m.updateInProgress = true
			m.updateError = ""
			m.updateSpinnerFrame = 0
			return m, tea.Batch(m.doUpdate(), updateSpinnerTick())
		}
		return m, nil
	}

	// Handle project switcher modal keys (Esc handled above)
	if m.showProjectSwitcher {
		// Handle project add sub-mode keys
		if m.projectAddMode {
			return m.handleProjectAddModalKeys(msg)
		}

		allProjects := m.cfg.Projects.List
		if len(allProjects) == 0 {
			// No projects configured - handle y for LLM prompt, ctrl+a for add, close on q/@
			switch msg.String() {
			case "y":
				return m, m.copyProjectSetupPrompt()
			case "ctrl+a":
				m.initProjectAdd()
				return m, nil
			case "q", "@":
				m.resetProjectSwitcher()
				m.updateContext()
			}
			return m, nil
		}

		projects := m.projectSwitcherFiltered

		switch msg.Type {
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
			m.previewProjectTheme()
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
			m.previewProjectTheme()
			return m, nil
		}

		// Handle non-text shortcuts
		switch msg.String() {
		case "ctrl+n":
			m.projectSwitcherCursor++
			if m.projectSwitcherCursor >= len(projects) {
				m.projectSwitcherCursor = len(projects) - 1
			}
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			m.previewProjectTheme()
			return m, nil

		case "ctrl+p":
			m.projectSwitcherCursor--
			if m.projectSwitcherCursor < 0 {
				m.projectSwitcherCursor = 0
			}
			m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
			m.previewProjectTheme()
			return m, nil

		case "ctrl+a":
			m.initProjectAdd()
			return m, nil

		case "@":
			// Close modal
			m.resetProjectSwitcher()
			m.updateContext()
			return m, nil
		}

		// Forward other keys to text input for filtering
		var cmd tea.Cmd
		m.projectSwitcherInput, cmd = m.projectSwitcherInput.Update(msg)

		// Re-filter on input change
		m.projectSwitcherFiltered = filterProjects(allProjects, m.projectSwitcherInput.Value())
		m.clearProjectSwitcherModal() // Clear modal cache on filter change
		// Reset cursor if it's beyond filtered list
		if m.projectSwitcherCursor >= len(m.projectSwitcherFiltered) {
			m.projectSwitcherCursor = len(m.projectSwitcherFiltered) - 1
		}
		if m.projectSwitcherCursor < 0 {
			m.projectSwitcherCursor = 0
		}
		m.projectSwitcherScroll = 0
		m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
		m.previewProjectTheme()

		return m, cmd
	}

	// Handle theme switcher modal keys (Esc handled above)
	if m.showThemeSwitcher {
		// Tab toggles between built-in and community views
		if msg.String() == "tab" {
			if m.showCommunityBrowser {
				m.applyThemeFromConfig(m.communityBrowserOriginal)
				m.resetCommunityBrowser()
			} else {
				m.initCommunityBrowser()
			}
			return m, nil
		}

		// ctrl+s or left/right toggles scope between global and project
		if m.currentProjectConfig() != nil {
			switch msg.String() {
			case "ctrl+s", "left", "right":
				if m.themeSwitcherScope == "global" {
					m.themeSwitcherScope = "project"
				} else {
					m.themeSwitcherScope = "global"
				}
				return m, nil
			}
		}

		// Community browser sub-mode handles its own keys
		if m.showCommunityBrowser {
			return m.handleCommunityBrowserKey(msg)
		}

		themes := m.themeSwitcherFiltered

		switch msg.Type {
		case tea.KeyEnter:
			// Confirm selection and close
			if m.themeSwitcherSelectedIdx >= 0 && m.themeSwitcherSelectedIdx < len(themes) {
				selectedTheme := themes[m.themeSwitcherSelectedIdx]
				scope := m.themeSwitcherScope
				m.resetThemeSwitcher()
				m.updateContext()
				// Persist to config based on scope
				tc := config.ThemeConfig{Name: selectedTheme}
				if err := m.saveThemeForScope(tc); err != nil {
					return m, func() tea.Msg {
						return ToastMsg{Message: "Theme applied (save failed)", Duration: 3 * time.Second, IsError: true}
					}
				}
				toastMsg := "Theme: " + selectedTheme + " (global)"
				if scope == "project" {
					toastMsg = "Theme: " + selectedTheme + " (project)"
				}
				return m, func() tea.Msg {
					return ToastMsg{Message: toastMsg, Duration: 2 * time.Second}
				}
			}
			return m, nil

		case tea.KeyUp:
			m.themeSwitcherSelectedIdx--
			if m.themeSwitcherSelectedIdx < 0 {
				m.themeSwitcherSelectedIdx = 0
			}
			m.themeSwitcherSelectedIdx = themeSwitcherEnsureCursorVisible(m.themeSwitcherSelectedIdx, m.themeSwitcherSelectedIdx, 8)
			// Live preview
			if m.themeSwitcherSelectedIdx < len(themes) {
				m.applyThemeFromConfig(themes[m.themeSwitcherSelectedIdx])
			}
			return m, nil

		case tea.KeyDown:
			m.themeSwitcherSelectedIdx++
			if m.themeSwitcherSelectedIdx >= len(themes) {
				m.themeSwitcherSelectedIdx = len(themes) - 1
			}
			if m.themeSwitcherSelectedIdx < 0 {
				m.themeSwitcherSelectedIdx = 0
			}
			m.themeSwitcherSelectedIdx = themeSwitcherEnsureCursorVisible(m.themeSwitcherSelectedIdx, m.themeSwitcherSelectedIdx, 8)
			// Live preview
			if m.themeSwitcherSelectedIdx < len(themes) {
				m.applyThemeFromConfig(themes[m.themeSwitcherSelectedIdx])
			}
			return m, nil
		}

		// Handle non-text shortcuts
		switch msg.String() {
		case "ctrl+n":
			m.themeSwitcherSelectedIdx++
			if m.themeSwitcherSelectedIdx >= len(themes) {
				m.themeSwitcherSelectedIdx = len(themes) - 1
			}
			if m.themeSwitcherSelectedIdx < 0 {
				m.themeSwitcherSelectedIdx = 0
			}
			m.themeSwitcherSelectedIdx = themeSwitcherEnsureCursorVisible(m.themeSwitcherSelectedIdx, m.themeSwitcherSelectedIdx, 8)
			if m.themeSwitcherSelectedIdx < len(themes) {
				m.applyThemeFromConfig(themes[m.themeSwitcherSelectedIdx])
			}
			return m, nil

		case "ctrl+p":
			m.themeSwitcherSelectedIdx--
			if m.themeSwitcherSelectedIdx < 0 {
				m.themeSwitcherSelectedIdx = 0
			}
			m.themeSwitcherSelectedIdx = themeSwitcherEnsureCursorVisible(m.themeSwitcherSelectedIdx, m.themeSwitcherSelectedIdx, 8)
			if m.themeSwitcherSelectedIdx < len(themes) {
				m.applyThemeFromConfig(themes[m.themeSwitcherSelectedIdx])
			}
			return m, nil

		case "#":
			// Close modal and restore original
			m.applyThemeFromConfig(m.themeSwitcherOriginal)
			m.resetThemeSwitcher()
			m.updateContext()
			return m, nil
		}

		// Forward other keys to text input for filtering
		var cmd tea.Cmd
		m.themeSwitcherInput, cmd = m.themeSwitcherInput.Update(msg)

		// Re-filter on input change
		m.themeSwitcherFiltered = filterThemes(styles.ListThemes(), m.themeSwitcherInput.Value())
		m.clearThemeSwitcherModal() // Force modal rebuild
		if m.themeSwitcherSelectedIdx >= len(m.themeSwitcherFiltered) {
			m.themeSwitcherSelectedIdx = len(m.themeSwitcherFiltered) - 1
		}
		if m.themeSwitcherSelectedIdx < 0 {
			m.themeSwitcherSelectedIdx = 0
		}

		// Live preview current selection
		if m.themeSwitcherSelectedIdx >= 0 && m.themeSwitcherSelectedIdx < len(m.themeSwitcherFiltered) {
			m.applyThemeFromConfig(m.themeSwitcherFiltered[m.themeSwitcherSelectedIdx])
		}

		return m, cmd
	}

	// If any modal is open, don't process plugin/toggle keys
	if m.hasModal() {
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
			// Force version check in background (bypasses cache)
			return m, tea.Batch(
				version.ForceCheckAsync(m.currentVersion),
				version.ForceCheckTdAsync(),
			)
		}
		m.clearDiagnosticsModal()
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
	case "#":
		// Toggle theme switcher modal
		m.showThemeSwitcher = !m.showThemeSwitcher
		if m.showThemeSwitcher {
			m.activeContext = "theme-switcher"
			m.initThemeSwitcher()
		} else {
			m.applyThemeFromConfig(m.themeSwitcherOriginal)
			m.resetThemeSwitcher()
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
	case "workspace-list", "workspace-preview":
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
		"workspace-create", "workspace-task-link", "workspace-rename-shell",
		"theme-switcher":
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
				if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
					// Click to select and switch
					selectedProject := projects[projectIdx]
					m.resetProjectSwitcher()
					m.updateContext()
					return m, m.switchProject(selectedProject.Path)
				}
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
			m.previewProjectTheme()
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
			m.previewProjectTheme()
		}

		return m, nil
	}

	// Click outside modal - close it
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		m.resetProjectSwitcher()
		m.updateContext()
		return m, nil
	}

	return m, nil
}

// handleThemeSwitcherMouse handles mouse events for the theme switcher modal.
func (m Model) handleThemeSwitcherMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureThemeSwitcherModal()
	if m.themeSwitcherModal == nil {
		return m, nil
	}
	if m.themeSwitcherMouseHandler == nil {
		m.themeSwitcherMouseHandler = mouse.NewHandler()
	}

	action := m.themeSwitcherModal.HandleMouse(msg, m.themeSwitcherMouseHandler)
	switch action {
	case "select":
		themes := m.themeSwitcherFiltered
		if m.themeSwitcherSelectedIdx >= 0 && m.themeSwitcherSelectedIdx < len(themes) {
			selectedTheme := themes[m.themeSwitcherSelectedIdx]
			m.applyThemeFromConfig(selectedTheme)
			m.resetThemeSwitcher()
			m.updateContext()
			// Persist to config based on scope
			tc := config.ThemeConfig{Name: selectedTheme}
			if err := m.saveThemeForScope(tc); err != nil {
				return m, func() tea.Msg {
					return ToastMsg{Message: "Theme applied (save failed)", Duration: 3 * time.Second, IsError: true}
				}
			}
			return m, func() tea.Msg {
				return ToastMsg{Message: "Theme: " + selectedTheme, Duration: 2 * time.Second}
			}
		}
	}
	return m, nil
}

// handleQuitConfirmMouse handles mouse events for the quit confirmation modal.
func (m Model) handleHelpModalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureHelpModal()
	if m.helpModal == nil {
		return m, nil
	}
	// Info-only modal - no mouse interaction needed beyond ensuring modal exists
	return m, nil
}

func (m Model) handleQuitConfirmMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	action := m.quitModal.HandleMouse(msg, m.quitMouseHandler)
	switch action {
	case "quit":
		// Save active plugin before quitting
		if activePlugin := m.ActivePlugin(); activePlugin != nil {
			state.SetActivePlugin(m.ui.WorkDir, activePlugin.ID())
		}
		m.registry.Stop()
		return m, tea.Quit
	case "cancel":
		m.showQuitConfirm = false
		return m, nil
	}
	return m, nil
}

// handleProjectAddThemePickerKeys handles keys within the theme picker sub-modal.
func (m Model) handleProjectAddThemePickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.projectAddCommunityMode {
		return m.handleProjectAddCommunityKeys(msg)
	}

	maxVisible := 6
	switch msg.String() {
	case "esc":
		m.resetProjectAddThemePicker()
		// Restore theme
		resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
		theme.ApplyResolved(resolved)
		return m, nil

	case "tab":
		// Switch to community themes
		m.projectAddCommunityMode = true
		m.projectAddCommunityList = community.ListSchemes()
		m.projectAddCommunityCursor = 0
		m.projectAddCommunityScroll = 0
		return m, nil

	case "up", "k":
		if m.projectAddThemeCursor > 0 {
			m.projectAddThemeCursor--
			if m.projectAddThemeCursor < m.projectAddThemeScroll {
				m.projectAddThemeScroll = m.projectAddThemeCursor
			}
			m.previewProjectAddTheme()
		}
		return m, nil

	case "down", "j":
		if m.projectAddThemeCursor < len(m.projectAddThemeFiltered)-1 {
			m.projectAddThemeCursor++
			if m.projectAddThemeCursor >= m.projectAddThemeScroll+maxVisible {
				m.projectAddThemeScroll = m.projectAddThemeCursor - maxVisible + 1
			}
			m.previewProjectAddTheme()
		}
		return m, nil

	case "enter":
		if m.projectAddThemeCursor >= 0 && m.projectAddThemeCursor < len(m.projectAddThemeFiltered) {
			m.projectAddThemeSelected = m.projectAddThemeFiltered[m.projectAddThemeCursor]
		}
		m.resetProjectAddThemePicker()
		// Restore theme
		resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
		theme.ApplyResolved(resolved)
		return m, nil
	}

	// Forward to filter input
	var cmd tea.Cmd
	m.projectAddThemeInput, cmd = m.projectAddThemeInput.Update(msg)
	// Re-filter
	query := m.projectAddThemeInput.Value()
	all := append([]string{"(use global)"}, styles.ListThemes()...)
	if query == "" {
		m.projectAddThemeFiltered = all
	} else {
		var filtered []string
		q := strings.ToLower(query)
		for _, name := range all {
			if strings.Contains(strings.ToLower(name), q) {
				filtered = append(filtered, name)
			}
		}
		m.projectAddThemeFiltered = filtered
	}
	m.projectAddThemeCursor = 0
	m.projectAddThemeScroll = 0
	return m, cmd
}

// handleProjectAddCommunityKeys handles keys in the community sub-browser within add-project.
func (m Model) handleProjectAddCommunityKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxVisible := 6
	switch msg.String() {
	case "esc", "tab":
		// Back to built-in themes
		m.projectAddCommunityMode = false
		// Restore theme
		resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
		theme.ApplyResolved(resolved)
		return m, nil

	case "up", "k":
		if m.projectAddCommunityCursor > 0 {
			m.projectAddCommunityCursor--
			if m.projectAddCommunityCursor < m.projectAddCommunityScroll {
				m.projectAddCommunityScroll = m.projectAddCommunityCursor
			}
			m.previewProjectAddCommunity()
		}
		return m, nil

	case "down", "j":
		if m.projectAddCommunityCursor < len(m.projectAddCommunityList)-1 {
			m.projectAddCommunityCursor++
			if m.projectAddCommunityCursor >= m.projectAddCommunityScroll+maxVisible {
				m.projectAddCommunityScroll = m.projectAddCommunityCursor - maxVisible + 1
			}
			m.previewProjectAddCommunity()
		}
		return m, nil

	case "enter":
		if m.projectAddCommunityCursor >= 0 && m.projectAddCommunityCursor < len(m.projectAddCommunityList) {
			m.projectAddThemeSelected = m.projectAddCommunityList[m.projectAddCommunityCursor]
		}
		m.resetProjectAddThemePicker()
		// Restore theme
		resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
		theme.ApplyResolved(resolved)
		return m, nil
	}

	return m, nil
}

// handleCommunityBrowserKey handles key events in the community browser sub-mode.
func (m Model) handleCommunityBrowserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	schemes := m.communityBrowserFiltered
	const maxVisible = 8

	switch msg.Type {
	case tea.KeyEnter:
		if m.communityBrowserCursor >= 0 && m.communityBrowserCursor < len(schemes) {
			selectedName := schemes[m.communityBrowserCursor]
			scheme := community.GetScheme(selectedName)
			if scheme != nil {
				// Apply immediately for visual effect
				theme.ApplyResolved(theme.ResolvedTheme{
					BaseName:      "default",
					CommunityName: selectedName,
				})
				scope := m.themeSwitcherScope
				m.resetCommunityBrowser()
				m.resetThemeSwitcher()
				m.updateContext()
				// Persist based on scope
				tc := config.ThemeConfig{Name: "default", Community: selectedName}
				if err := m.saveThemeForScope(tc); err != nil {
					return m, func() tea.Msg {
						return ToastMsg{Message: "Theme applied (save failed)", Duration: 3 * time.Second, IsError: true}
					}
				}
				scopeLabel := "global"
				if scope == "project" {
					scopeLabel = "project"
				}
				return m, func() tea.Msg {
					return ToastMsg{Message: "Theme: " + selectedName + " (" + scopeLabel + ")", Duration: 2 * time.Second}
				}
			}
		}
		return m, nil

	case tea.KeyUp:
		m.communityBrowserCursor--
		if m.communityBrowserCursor < 0 {
			m.communityBrowserCursor = 0
		}
		m.communityBrowserScroll = themeSwitcherEnsureCursorVisible(m.communityBrowserCursor, m.communityBrowserScroll, maxVisible)
		m.previewCommunityScheme()
		return m, nil

	case tea.KeyDown:
		m.communityBrowserCursor++
		if m.communityBrowserCursor >= len(schemes) {
			m.communityBrowserCursor = len(schemes) - 1
		}
		if m.communityBrowserCursor < 0 {
			m.communityBrowserCursor = 0
		}
		m.communityBrowserScroll = themeSwitcherEnsureCursorVisible(m.communityBrowserCursor, m.communityBrowserScroll, maxVisible)
		m.previewCommunityScheme()
		return m, nil
	}

	switch msg.String() {
	case "ctrl+n":
		m.communityBrowserCursor++
		if m.communityBrowserCursor >= len(schemes) {
			m.communityBrowserCursor = len(schemes) - 1
		}
		if m.communityBrowserCursor < 0 {
			m.communityBrowserCursor = 0
		}
		m.communityBrowserScroll = themeSwitcherEnsureCursorVisible(m.communityBrowserCursor, m.communityBrowserScroll, maxVisible)
		m.previewCommunityScheme()
		return m, nil

	case "ctrl+p":
		m.communityBrowserCursor--
		if m.communityBrowserCursor < 0 {
			m.communityBrowserCursor = 0
		}
		m.communityBrowserScroll = themeSwitcherEnsureCursorVisible(m.communityBrowserCursor, m.communityBrowserScroll, maxVisible)
		m.previewCommunityScheme()
		return m, nil

	case "#":
		// Close both modals and restore
		m.applyThemeFromConfig(m.communityBrowserOriginal)
		m.resetCommunityBrowser()
		m.resetThemeSwitcher()
		m.updateContext()
		return m, nil
	}

	// Forward other keys to text input for filtering
	var cmd tea.Cmd
	m.communityBrowserInput, cmd = m.communityBrowserInput.Update(msg)

	// Re-filter on input change
	m.communityBrowserFiltered = filterCommunitySchemes(community.ListSchemes(), m.communityBrowserInput.Value())
	m.communityBrowserHover = -1
	if m.communityBrowserCursor >= len(m.communityBrowserFiltered) {
		m.communityBrowserCursor = len(m.communityBrowserFiltered) - 1
	}
	if m.communityBrowserCursor < 0 {
		m.communityBrowserCursor = 0
	}
	m.communityBrowserScroll = themeSwitcherEnsureCursorVisible(m.communityBrowserCursor, 0, maxVisible)
	m.previewCommunityScheme()

	return m, cmd
}

// previewCommunityScheme applies the currently selected community scheme for live preview.
func (m *Model) previewCommunityScheme() {
	schemes := m.communityBrowserFiltered
	if m.communityBrowserCursor >= 0 && m.communityBrowserCursor < len(schemes) {
		theme.ApplyResolved(theme.ResolvedTheme{
			BaseName:      "default",
			CommunityName: schemes[m.communityBrowserCursor],
		})
	}
}

// handleCommunityBrowserMouse handles mouse events for the community browser modal.
func (m Model) handleCommunityBrowserMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	schemes := m.communityBrowserFiltered
	maxVisible := 8
	visibleCount := len(schemes)
	if visibleCount > maxVisible {
		visibleCount = maxVisible
	}

	// Modal dimensions matching view rendering
	modalContentLines := 2 + 1 + 1 + visibleCount + 2
	if m.communityBrowserScroll > 0 {
		modalContentLines++
	}
	if len(schemes) > m.communityBrowserScroll+visibleCount {
		modalContentLines++
	}
	if len(schemes) == 0 {
		modalContentLines = 2 + 1 + 1 + 2 + 2
	}

	modalHeight := modalContentLines + 4
	modalWidth := 50
	modalX := (m.width - modalWidth) / 2
	modalY := (m.height - modalHeight) / 2

	// Inside modal
	if msg.X >= modalX && msg.X < modalX+modalWidth &&
		msg.Y >= modalY && msg.Y < modalY+modalHeight {

		if len(schemes) == 0 {
			return m, nil
		}

		// Calculate scheme list start Y
		contentStartY := modalY + 2 + 2 + 1 + 1
		if m.communityBrowserScroll > 0 {
			contentStartY++
		}

		relY := msg.Y - contentStartY
		if relY >= 0 && relY < visibleCount {
			schemeIdx := m.communityBrowserScroll + relY
			if schemeIdx >= 0 && schemeIdx < len(schemes) {
				switch msg.Action {
				case tea.MouseActionPress:
					if msg.Button == tea.MouseButtonLeft {
						selectedName := schemes[schemeIdx]
						scheme := community.GetScheme(selectedName)
						if scheme != nil {
							theme.ApplyResolved(theme.ResolvedTheme{
								BaseName:      "default",
								CommunityName: selectedName,
							})
							scope := m.themeSwitcherScope
							m.resetCommunityBrowser()
							m.resetThemeSwitcher()
							m.updateContext()
							tc := config.ThemeConfig{Name: "default", Community: selectedName}
							if err := m.saveThemeForScope(tc); err != nil {
								return m, func() tea.Msg {
									return ToastMsg{Message: "Theme applied (save failed)", Duration: 3 * time.Second, IsError: true}
								}
							}
							scopeLabel := "global"
							if scope == "project" {
								scopeLabel = "project"
							}
							return m, func() tea.Msg {
								return ToastMsg{Message: "Theme: " + selectedName + " (" + scopeLabel + ")", Duration: 2 * time.Second}
							}
						}
					}
				case tea.MouseActionMotion:
					m.communityBrowserHover = schemeIdx
					theme.ApplyResolved(theme.ResolvedTheme{
						BaseName:      "default",
						CommunityName: schemes[schemeIdx],
					})
				}
			}
		} else {
			if msg.Action == tea.MouseActionMotion {
				m.communityBrowserHover = -1
			}
		}

		// Scroll wheel
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.communityBrowserCursor--
			if m.communityBrowserCursor < 0 {
				m.communityBrowserCursor = 0
			}
			m.communityBrowserScroll = themeSwitcherEnsureCursorVisible(m.communityBrowserCursor, m.communityBrowserScroll, maxVisible)
			m.previewCommunityScheme()
		case tea.MouseButtonWheelDown:
			m.communityBrowserCursor++
			if m.communityBrowserCursor >= len(schemes) {
				m.communityBrowserCursor = len(schemes) - 1
			}
			if m.communityBrowserCursor < 0 {
				m.communityBrowserCursor = 0
			}
			m.communityBrowserScroll = themeSwitcherEnsureCursorVisible(m.communityBrowserCursor, m.communityBrowserScroll, maxVisible)
			m.previewCommunityScheme()
		}

		return m, nil
	}

	// Click outside modal - cancel
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		m.applyThemeFromConfig(m.communityBrowserOriginal)
		m.resetCommunityBrowser()
		m.resetThemeSwitcher()
		m.updateContext()
		return m, nil
	}

	if msg.Action == tea.MouseActionMotion {
		m.communityBrowserHover = -1
	}

	return m, nil
}
