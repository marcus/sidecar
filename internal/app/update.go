package app

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/community"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/palette"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/theme"
	"github.com/marcus/sidecar/internal/version"
)

// isMouseEscapeSequence returns true if the key message appears to be
// an unparsed mouse escape sequence (SGR format: [<...M or [<...m)
func isMouseEscapeSequence(msg tea.KeyMsg) bool {
	s := msg.String()
	// SGR mouse sequences contain [< and end with M or m
	if strings.Contains(s, "[<") && (strings.HasSuffix(s, "M") || strings.HasSuffix(s, "m")) {
		return true
	}
	// Check for semicolon-separated coordinate patterns typical of mouse sequences
	if strings.Contains(s, ";") && strings.ContainsAny(s, "0123456789") {
		if strings.HasSuffix(s, "M") || strings.HasSuffix(s, "m") {
			return true
		}
	}
	return false
}

// Update handles all messages and returns the updated model and commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return (&m).handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			// Prime worktree cache before first render
			m.refreshWorktreeCache()
		}
		m.ready = true
		// Reset diagnostics modal on resize (will be rebuilt on next render)
		if m.showDiagnostics {
			m.diagnosticsModalWidth = 0
		}
		// Forward adjusted WindowSizeMsg to all plugins
		// Plugins receive the content area size (minus header and footer)
		// Must match the height passed to Plugin.View() in view.go
		adjustedHeight := msg.Height - headerHeight - footerHeight
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
		case ModalUpdate:
			return m.handleUpdateModalMouse(msg)
		case ModalDiagnostics:
			return m.handleDiagnosticsModalMouse(msg)
		case ModalQuitConfirm:
			return m.handleQuitConfirmMouse(msg)
		case ModalProjectSwitcher:
			if m.projectAddMode {
				return m.handleProjectAddModalMouse(msg)
			}
			return m.handleProjectSwitcherMouse(msg)
		case ModalWorktreeSwitcher:
			return m.handleWorktreeSwitcherMouse(msg)
		case ModalThemeSwitcher:
			return m.handleThemeSwitcherMouse(msg)
		case ModalOpenIn:
			return m.handleOpenInMouse(msg)
		case ModalIssueInput:
			return m.handleIssueInputMouse(msg)
		case ModalIssuePreview:
			return m.handleIssuePreviewMouse(msg)
		}

		// Handle header tab clicks (Y < 2 means header area)
		if msg.Y < headerHeight && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if start, end, ok := m.getRepoNameBounds(); ok && !m.intro.Active && msg.X >= start && msg.X < end {
				m.showProjectSwitcher = true
				m.activeContext = "project-switcher"
				m.initProjectSwitcher()
				return m, nil
			}

			// Check if click is on worktree indicator
			if start, end, ok := m.getWorktreeIndicatorBounds(); ok && !m.intro.Active && msg.X >= start && msg.X < end {
				worktrees := GetWorktrees(m.ui.WorkDir)
				if len(worktrees) > 1 {
					m.showWorktreeSwitcher = true
					m.activeContext = "worktree-switcher"
					m.initWorktreeSwitcher()
					return m, nil
				}
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
		// Eagerly refresh worktree cache (must happen in Update, not View, due to value receiver)
		m.refreshWorktreeCache()
		// Periodically check if current worktree still exists (every 10 seconds)
		m.worktreeCheckCounter++
		if m.worktreeCheckCounter >= 10 {
			m.worktreeCheckCounter = 0
			return m, tea.Batch(tickCmd(), checkWorktreeExists(m.ui.WorkDir))
		}
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
		// Set all phases to done
		m.updatePhaseStatus[PhaseCheckPrereqs] = "done"
		m.updatePhaseStatus[PhaseInstalling] = "done"
		m.updatePhaseStatus[PhaseVerifying] = "done"
		// Update modal state if modal is open
		if m.updateModalState == UpdateModalProgress {
			m.updateModalState = UpdateModalComplete
		}
		if msg.SidecarUpdated {
			m.updateAvailable = nil
		}
		if msg.TdUpdated && m.tdVersionInfo != nil {
			m.tdVersionInfo.HasUpdate = false
		}
		// Only show toast if modal is not open
		if m.updateModalState == UpdateModalClosed {
			m.ShowToast("Update complete! Restart sidecar to use new version", 10*time.Second)
		}
		return m, nil

	case UpdateErrorMsg:
		m.updateInProgress = false
		m.updateError = fmt.Sprintf("Failed to update %s: %s", msg.Step, msg.Err)
		// Mark current phase as error
		m.updatePhaseStatus[m.updatePhase] = "error"
		// Update modal state if modal is open
		if m.updateModalState == UpdateModalProgress {
			m.updateModalState = UpdateModalError
		}
		// Only show toast if modal is not open
		if m.updateModalState == UpdateModalClosed {
			m.ShowToast("Update failed: "+msg.Err.Error(), 5*time.Second)
		}
		m.statusIsError = true
		return m, nil

	case UpdatePhaseChangeMsg:
		m.updatePhaseStatus[msg.Phase] = msg.Status
		if msg.Status == "running" {
			m.updatePhase = msg.Phase
		}
		return m, nil

	case UpdateElapsedTickMsg:
		// Continue timer if update is in progress
		if m.updateInProgress && m.updateModalState == UpdateModalProgress {
			return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
				return UpdateElapsedTickMsg{}
			})
		}
		return m, nil

	case UpdatePrereqsPassedMsg:
		// Prerequisites passed - transition to install phase
		m.updatePhaseStatus[PhaseCheckPrereqs] = "done"
		m.updatePhase = PhaseInstalling
		m.updatePhaseStatus[PhaseInstalling] = "running"
		return m, m.runInstallPhase()

	case UpdateInstallDoneMsg:
		// Install completed - transition to verify phase
		m.updatePhaseStatus[PhaseInstalling] = "done"
		m.updatePhase = PhaseVerifying
		m.updatePhaseStatus[PhaseVerifying] = "running"
		return m, m.runVerifyPhase(msg)

	case ChangelogLoadedMsg:
		if msg.Err != nil {
			m.updateChangelog = "Failed to load changelog: " + msg.Err.Error()
		} else {
			m.updateChangelog = msg.Content
		}
		m.clearChangelogModal() // Force rebuild with new content
		return m, nil

	case FocusPluginByIDMsg:
		// Switch to requested plugin
		return m, m.FocusPluginByID(msg.PluginID)

	case SwitchWorktreeMsg:
		// Switch to the requested worktree
		return m, m.switchWorktree(msg.WorktreePath)

	case WorktreeDeletedMsg:
		// Current worktree was deleted (detected by periodic check) - switch to main
		return m, tea.Batch(
			m.switchWorktree(msg.MainPath),
			ShowToast("Worktree deleted, switched to main", 3*time.Second),
		)

	case SwitchToMainWorktreeMsg:
		// Current worktree was deleted (detected by workspace plugin) - switch to main
		if msg.MainWorktreePath != "" && msg.MainWorktreePath != m.ui.WorkDir {
			return m, tea.Batch(
				m.switchProject(msg.MainWorktreePath),
				func() tea.Msg {
					return ToastMsg{
						Message:  "Worktree deleted, switched to main repo",
						Duration: 3 * time.Second,
					}
				},
			)
		}
		return m, nil

	case plugin.OpenFileMsg:
		// Open file in editor using tea.ExecProcess
		// Most editors support +lineNo syntax for opening at a line
		args := []string{}
		if msg.LineNo > 0 {
			args = append(args, fmt.Sprintf("+%d", msg.LineNo))
		}
		args = append(args, msg.Path)
		c := exec.Command(msg.Editor, args...)
		termState, _ := term.GetState(int(os.Stdout.Fd()))
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			if termState != nil {
				_ = term.Restore(int(os.Stdout.Fd()), termState)
			}
			return EditorReturnedMsg{Err: err}
		})

	case EditorReturnedMsg:
		// After editor exits, re-enable mouse and trigger refresh
		// tea.ExecProcess disables mouse, need to restore it
		cmds := []tea.Cmd{
			func() tea.Msg { return tea.EnableMouseAllMotion() },
		}
		if msg.Err != nil {
			cmds = append(cmds, func() tea.Msg { return ErrorMsg(msg) })
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
		m.updateInstallMethod = msg.InstallMethod
		m.clearDiagnosticsModal() // Force rebuild so modal picks up new update state
		m.ShowToast(
			fmt.Sprintf("Update %s available! Press ! for details", msg.LatestVersion),
			15*time.Second,
		)
		return m, nil

	case version.TdVersionMsg:
		m.tdVersionInfo = &msg
		m.clearDiagnosticsModal() // Force rebuild so modal picks up new version state
		// Show toast if td has an update (only if sidecar doesn't also have one)
		if msg.HasUpdate && m.updateAvailable == nil {
			m.ShowToast(
				fmt.Sprintf("td update %s available! Press ! for details", msg.LatestVersion),
				15*time.Second,
			)
		}
		return m, nil

	case IssuePreviewResultMsg:
		m.issuePreviewLoading = false
		if msg.Error != nil {
			m.issuePreviewError = msg.Error
		} else {
			m.issuePreviewData = msg.Data
		}
		// Clear modal cache to trigger rebuild
		m.issuePreviewModal = nil
		m.issuePreviewModalWidth = 0
		return m, nil

	case IssueSearchResultMsg:
		// Discard stale results
		if msg.Query != m.issueSearchQuery || !m.showIssueInput {
			return m, nil
		}
		m.issueSearchLoading = false
		if msg.Error == nil {
			m.issueSearchResults = msg.Results
		}
		m.issueSearchScrollOffset = 0
		m.issueInputModal = nil
		m.issueInputModalWidth = 0
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
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		case ModalUpdate:
			// Handle Esc in update modal
			if m.changelogVisible {
				// Close changelog overlay, return to preview
				m.changelogVisible = false
				m.changelogScrollOffset = 0
				m.clearChangelogModal()
				return m, nil
			}
			// Close update modal
			m.updateModalState = UpdateModalClosed
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
		case ModalWorktreeSwitcher:
			// Esc: clear filter if set, otherwise close
			if m.worktreeSwitcherInput.Value() != "" {
				m.worktreeSwitcherInput.SetValue("")
				m.worktreeSwitcherFiltered = m.worktreeSwitcherAll
				m.worktreeSwitcherCursor = 0
				m.worktreeSwitcherScroll = 0
				return m, nil
			}
			m.resetWorktreeSwitcher()
			m.updateContext()
			return m, nil
		case ModalIssueInput:
			m.resetIssueInput()
			m.updateContext()
			return m, nil
		case ModalIssuePreview:
			m.resetIssuePreview()
			m.resetIssueInput()
			m.updateContext()
			return m, nil
		case ModalThemeSwitcher:
			// Esc: clear filter if set, otherwise close (restore original)
			if m.themeSwitcherInput.Value() != "" {
				m.themeSwitcherInput.SetValue("")
				m.themeSwitcherFiltered = buildUnifiedThemeList()
				m.themeSwitcherSelectedIdx = 0
				return m, nil
			}
			m.previewThemeEntry(m.themeSwitcherOriginal)
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
				_ = state.SetActivePlugin(m.ui.ProjectRoot, activePlugin.ID())
			}
			m.registry.Stop()
			return m, tea.Quit
		case "cancel":
			m.showQuitConfirm = false
			return m, nil
		}
		return m, cmd
	}

	// Handle update modal keys
	if m.updateModalState != UpdateModalClosed {
		return m.handleUpdateModalKey(msg)
	}

	// Interactive/inline edit mode: forward ALL keys to plugin including ctrl+c
	// This ensures characters like `, ~, ?, !, @, q, 1-5 reach tmux instead of triggering app shortcuts
	// Ctrl+C is forwarded to tmux (to interrupt running processes) instead of showing quit dialog
	// User can exit interactive mode with Ctrl+\ first, then quit normally
	if m.activeContext == "workspace-interactive" || m.activeContext == "file-browser-inline-edit" || m.activeContext == "notes-inline-edit" {
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

	// Text input contexts: forward all keys to plugin except ctrl+c.
	// Uses plugin runtime capability first, then app-level fallback contexts.
	if m.consumesTextInput() {
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
				// Open update modal instead of starting update directly
				if m.hasUpdatesAvailable() && !m.updateInProgress && !m.needsRestart {
					m.updateReleaseNotes = ""
					if m.updateAvailable != nil {
						m.updateReleaseNotes = m.updateAvailable.ReleaseNotes
					}
					m.updateModalState = UpdateModalPreview
					m.showDiagnostics = false
					return m, nil
				}
			}
		}
		// Handle 'u' shortcut for update - open update modal
		if msg.String() == "u" && m.hasUpdatesAvailable() && !m.updateInProgress && !m.needsRestart {
			m.updateReleaseNotes = ""
			if m.updateAvailable != nil {
				m.updateReleaseNotes = m.updateAvailable.ReleaseNotes
			}
			m.updateModalState = UpdateModalPreview
			m.showDiagnostics = false
			return m, nil
		}
		return m, nil
	}

	// Handle worktree switcher modal keys (Esc handled above)
	if m.showWorktreeSwitcher {
		worktrees := m.worktreeSwitcherFiltered

		switch msg.Type {
		case tea.KeyEnter:
			// Select worktree and switch to it
			if m.worktreeSwitcherCursor >= 0 && m.worktreeSwitcherCursor < len(worktrees) {
				selectedPath := worktrees[m.worktreeSwitcherCursor].Path
				m.resetWorktreeSwitcher()
				m.updateContext()
				return m, m.switchWorktree(selectedPath)
			}
			return m, nil

		case tea.KeyUp:
			m.worktreeSwitcherCursor--
			if m.worktreeSwitcherCursor < 0 {
				m.worktreeSwitcherCursor = 0
			}
			m.worktreeSwitcherScroll = worktreeSwitcherEnsureCursorVisible(m.worktreeSwitcherCursor, m.worktreeSwitcherScroll, 8)
			return m, nil

		case tea.KeyDown:
			m.worktreeSwitcherCursor++
			if m.worktreeSwitcherCursor >= len(worktrees) {
				m.worktreeSwitcherCursor = len(worktrees) - 1
			}
			if m.worktreeSwitcherCursor < 0 {
				m.worktreeSwitcherCursor = 0
			}
			m.worktreeSwitcherScroll = worktreeSwitcherEnsureCursorVisible(m.worktreeSwitcherCursor, m.worktreeSwitcherScroll, 8)
			return m, nil
		}

		// Handle non-text shortcuts
		switch msg.String() {
		case "ctrl+n":
			m.worktreeSwitcherCursor++
			if m.worktreeSwitcherCursor >= len(worktrees) {
				m.worktreeSwitcherCursor = len(worktrees) - 1
			}
			if m.worktreeSwitcherCursor < 0 {
				m.worktreeSwitcherCursor = 0
			}
			m.worktreeSwitcherScroll = worktreeSwitcherEnsureCursorVisible(m.worktreeSwitcherCursor, m.worktreeSwitcherScroll, 8)
			return m, nil

		case "ctrl+p":
			m.worktreeSwitcherCursor--
			if m.worktreeSwitcherCursor < 0 {
				m.worktreeSwitcherCursor = 0
			}
			m.worktreeSwitcherScroll = worktreeSwitcherEnsureCursorVisible(m.worktreeSwitcherCursor, m.worktreeSwitcherScroll, 8)
			return m, nil

		case "W":
			// Close modal with same key
			m.resetWorktreeSwitcher()
			m.updateContext()
			return m, nil
		}

		// Filter out unparsed mouse escape sequences
		if isMouseEscapeSequence(msg) {
			return m, nil
		}

		// Forward other keys to text input for filtering
		var cmd tea.Cmd
		m.worktreeSwitcherInput, cmd = m.worktreeSwitcherInput.Update(msg)

		// Re-filter on input change
		m.worktreeSwitcherFiltered = filterWorktrees(m.worktreeSwitcherAll, m.worktreeSwitcherInput.Value())
		m.clearWorktreeSwitcherModal() // Clear modal cache on filter change
		// Reset cursor if it's beyond filtered list
		if m.worktreeSwitcherCursor >= len(m.worktreeSwitcherFiltered) {
			m.worktreeSwitcherCursor = len(m.worktreeSwitcherFiltered) - 1
		}
		if m.worktreeSwitcherCursor < 0 {
			m.worktreeSwitcherCursor = 0
		}
		m.worktreeSwitcherScroll = 0
		m.worktreeSwitcherScroll = worktreeSwitcherEnsureCursorVisible(m.worktreeSwitcherCursor, m.worktreeSwitcherScroll, 8)

		return m, cmd
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

		// Filter out unparsed mouse escape sequences
		if isMouseEscapeSequence(msg) {
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

		themes := m.themeSwitcherFiltered

		switch msg.Type {
		case tea.KeyEnter:
			// Confirm selection and close (ignore separators)
			if m.themeSwitcherSelectedIdx >= 0 && m.themeSwitcherSelectedIdx < len(themes) && !themes[m.themeSwitcherSelectedIdx].IsSeparator {
				entry := themes[m.themeSwitcherSelectedIdx]
				var tc config.ThemeConfig
				if entry.IsBuiltIn {
					tc = config.ThemeConfig{Name: entry.ThemeKey}
				} else {
					tc = config.ThemeConfig{Name: "default", Community: entry.ThemeKey}
				}
				m.previewThemeEntry(entry)
				return m, m.confirmThemeSelection(tc, entry.Name)
			}
			return m, nil

		case tea.KeyUp:
			m.themeSwitcherSelectedIdx--
			if m.themeSwitcherSelectedIdx < 0 {
				m.themeSwitcherSelectedIdx = 0
			}
			// Skip separators
			for m.themeSwitcherSelectedIdx > 0 && themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.themeSwitcherSelectedIdx--
			}
			if m.themeSwitcherSelectedIdx < len(themes) && !themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.previewThemeEntry(themes[m.themeSwitcherSelectedIdx])
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
			// Skip separators
			for m.themeSwitcherSelectedIdx < len(themes)-1 && themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.themeSwitcherSelectedIdx++
			}
			if m.themeSwitcherSelectedIdx < len(themes) && !themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.previewThemeEntry(themes[m.themeSwitcherSelectedIdx])
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
			for m.themeSwitcherSelectedIdx < len(themes)-1 && themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.themeSwitcherSelectedIdx++
			}
			if m.themeSwitcherSelectedIdx < len(themes) && !themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.previewThemeEntry(themes[m.themeSwitcherSelectedIdx])
			}
			return m, nil

		case "ctrl+p":
			m.themeSwitcherSelectedIdx--
			if m.themeSwitcherSelectedIdx < 0 {
				m.themeSwitcherSelectedIdx = 0
			}
			for m.themeSwitcherSelectedIdx > 0 && themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.themeSwitcherSelectedIdx--
			}
			if m.themeSwitcherSelectedIdx < len(themes) && !themes[m.themeSwitcherSelectedIdx].IsSeparator {
				m.previewThemeEntry(themes[m.themeSwitcherSelectedIdx])
			}
			return m, nil

		case "#":
			// Close modal and restore original
			m.previewThemeEntry(m.themeSwitcherOriginal)
			m.resetThemeSwitcher()
			m.updateContext()
			return m, nil
		}

		// Filter out unparsed mouse escape sequences
		if isMouseEscapeSequence(msg) {
			return m, nil
		}

		// Forward other keys to text input for filtering
		var cmd tea.Cmd
		m.themeSwitcherInput, cmd = m.themeSwitcherInput.Update(msg)

		// Re-filter on input change
		m.themeSwitcherFiltered = filterThemeEntries(buildUnifiedThemeList(), m.themeSwitcherInput.Value())
		m.clearThemeSwitcherModal() // Force modal rebuild
		if m.themeSwitcherSelectedIdx >= len(m.themeSwitcherFiltered) {
			m.themeSwitcherSelectedIdx = len(m.themeSwitcherFiltered) - 1
		}
		if m.themeSwitcherSelectedIdx < 0 {
			m.themeSwitcherSelectedIdx = 0
		}

		// Live preview current selection (skip separators)
		if m.themeSwitcherSelectedIdx >= 0 && m.themeSwitcherSelectedIdx < len(m.themeSwitcherFiltered) && !m.themeSwitcherFiltered[m.themeSwitcherSelectedIdx].IsSeparator {
			m.previewThemeEntry(m.themeSwitcherFiltered[m.themeSwitcherSelectedIdx])
		}

		return m, cmd
	}

	// Handle Open In modal keys (Esc handled above)
	if m.showOpenIn {
		m.ensureOpenInModal()
		if m.openInModal != nil {
			action, cmd := m.openInModal.HandleKey(msg)
			switch action {
			case "cancel":
				m.resetOpenIn()
				m.updateContext()
				return m, nil
			case "select":
				return m, m.confirmOpenIn()
			}
			if cmd != nil {
				return m, cmd
			}
		}
		return m, nil
	}

	// Handle issue input modal keys
	if m.showIssueInput {
		// ctrl+x toggles closed issue visibility (before type switch)
		if msg.String() == "ctrl+x" {
			m.issueSearchIncludeClosed = !m.issueSearchIncludeClosed
			m.issueSearchScrollOffset = 0
			m.issueSearchCursor = -1
			m.issueInputModal = nil
			m.issueInputModalWidth = 0
			if len(strings.TrimSpace(m.issueInputInput.Value())) >= 2 {
				m.issueSearchLoading = true
				return m, issueSearchCmd(m.ui.WorkDir, strings.TrimSpace(m.issueInputInput.Value()), m.issueSearchIncludeClosed)
			}
			return m, nil
		}

		switch msg.Type {
		case tea.KeyEnter:
			return m.issueInputSubmit()
		case tea.KeyUp:
			if len(m.issueSearchResults) > 0 {
				m.issueSearchCursor--
				if m.issueSearchCursor < -1 {
					m.issueSearchCursor = -1
				}
				// Keep cursor visible in viewport
				if m.issueSearchCursor >= 0 && m.issueSearchCursor < m.issueSearchScrollOffset {
					m.issueSearchScrollOffset = m.issueSearchCursor
				}
				m.issueInputModal = nil
				m.issueInputModalWidth = 0
				return m, nil
			}
		case tea.KeyDown:
			if len(m.issueSearchResults) > 0 {
				m.issueSearchCursor++
				if m.issueSearchCursor >= len(m.issueSearchResults) {
					m.issueSearchCursor = len(m.issueSearchResults) - 1
				}
				// Keep cursor visible in viewport
				const maxVisible = 10
				if m.issueSearchCursor >= m.issueSearchScrollOffset+maxVisible {
					m.issueSearchScrollOffset = m.issueSearchCursor - maxVisible + 1
				}
				m.issueInputModal = nil
				m.issueInputModalWidth = 0
				return m, nil
			}
		case tea.KeyTab:
			if m.issueSearchCursor >= 0 && m.issueSearchCursor < len(m.issueSearchResults) {
				m.issueInputInput.SetValue(m.issueSearchResults[m.issueSearchCursor].ID)
				m.issueInputInput.CursorEnd()
				m.issueInputModal = nil
				m.issueInputModalWidth = 0
			}
			// Tab is consumed (fill-in or no-op) — don't forward to textinput
			return m, nil
		}

		if isMouseEscapeSequence(msg) {
			return m, nil
		}

		// Forward key to text input, then clear modal cache so it rebuilds
		var cmd tea.Cmd
		m.issueInputInput, cmd = m.issueInputInput.Update(msg)
		m.issueInputModal = nil
		m.issueInputModalWidth = 0

		// Trigger search if input changed (min 2 chars)
		newValue := strings.TrimSpace(m.issueInputInput.Value())
		if newValue != m.issueSearchQuery && len(newValue) >= 2 {
			m.issueSearchQuery = newValue
			m.issueSearchLoading = true
			// Keep previous results visible while loading to avoid modal shrink/grow flicker.
			// Results are replaced when the new IssueSearchResultMsg arrives.
			m.issueSearchCursor = -1
			return m, tea.Batch(cmd, issueSearchCmd(m.ui.WorkDir, newValue, m.issueSearchIncludeClosed))
		}
		if len(newValue) < 2 {
			m.issueSearchResults = nil
			m.issueSearchQuery = ""
			m.issueSearchCursor = -1
		}
		return m, cmd
	}

	// Handle issue preview modal keys
	if m.showIssuePreview {
		m.ensureIssuePreviewModal()
		if m.issuePreviewModal == nil {
			return m, nil
		}

		// Shortcuts before modal.HandleKey (which consumes Enter/Esc/Tab)
		switch msg.String() {
		case "j", "down":
			m.issuePreviewModal.ScrollBy(1)
			return m, nil
		case "k", "up":
			m.issuePreviewModal.ScrollBy(-1)
			return m, nil
		case "ctrl+d":
			m.issuePreviewModal.ScrollBy(10)
			return m, nil
		case "ctrl+u":
			m.issuePreviewModal.ScrollBy(-10)
			return m, nil
		case "g":
			m.issuePreviewModal.ScrollToTop()
			return m, nil
		case "G":
			m.issuePreviewModal.ScrollToBottom()
			return m, nil
		case "o":
			if m.issuePreviewData != nil {
				issueID := m.issuePreviewData.ID
				m.resetIssuePreview()
				m.resetIssueInput()
				m.updateContext()
				return m, tea.Batch(
					FocusPlugin("td-monitor"),
					func() tea.Msg { return OpenFullIssueMsg{IssueID: issueID} },
				)
			}
		case "b":
			m.backToIssueInput()
			return m, nil
		case "y":
			if m.issuePreviewData != nil {
				d := m.issuePreviewData
				text := d.ID + ": " + d.Title + "\n\n" + d.Description
				if err := clipboard.WriteAll(text); err != nil {
					return m, ShowToast("Copy failed: "+err.Error(), 2*time.Second)
				}
				return m, ShowToast("Yanked issue details", 2*time.Second)
			}
		case "Y":
			if m.issuePreviewData != nil {
				id := m.issuePreviewData.ID
				if err := clipboard.WriteAll(id); err != nil {
					return m, ShowToast("Copy failed: "+err.Error(), 2*time.Second)
				}
				return m, ShowToast("Yanked: "+id, 2*time.Second)
			}
		}

		action, cmd := m.issuePreviewModal.HandleKey(msg)
		switch action {
		case "open-in-td":
			issueID := ""
			if m.issuePreviewData != nil {
				issueID = m.issuePreviewData.ID
			}
			m.resetIssuePreview()
			m.resetIssueInput()
			m.updateContext()
			if issueID != "" {
				return m, tea.Batch(
					FocusPlugin("td-monitor"),
					func() tea.Msg { return OpenFullIssueMsg{IssueID: issueID} },
				)
			}
			return m, nil
		case "back":
			m.backToIssueInput()
			return m, nil
		case "cancel":
			m.resetIssuePreview()
			m.resetIssueInput()
			m.updateContext()
			return m, nil
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
		if m.consumesTextInput() {
			break
		}
		return m, m.NextPlugin()
	case "~":
		// Tilde cycles to previous plugin (except in text input contexts)
		if m.consumesTextInput() {
			break
		}
		return m, m.PrevPlugin()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Number keys for direct plugin switching
		// Block in text input contexts (user is typing numbers)
		if m.consumesTextInput() {
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
	case "W":
		// Toggle worktree switcher modal (capital W)
		// Only enable if we're in a git repo with worktrees
		worktrees := GetWorktrees(m.ui.WorkDir)
		if len(worktrees) <= 1 {
			// No worktrees or only main repo - show toast
			return m, func() tea.Msg {
				return ToastMsg{Message: "No worktrees found", Duration: 2 * time.Second}
			}
		}
		m.showWorktreeSwitcher = !m.showWorktreeSwitcher
		if m.showWorktreeSwitcher {
			m.activeContext = "worktree-switcher"
			m.initWorktreeSwitcher()
		} else {
			m.resetWorktreeSwitcher()
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
			m.previewThemeEntry(m.themeSwitcherOriginal)
			m.resetThemeSwitcher()
			m.updateContext()
		}
		return m, nil
	case "O":
		if !m.hasModal() {
			m.showOpenIn = true
			m.activeContext = "open-in"
			m.initOpenIn()
		}
		return m, nil
	case "i":
		if !m.hasModal() {
			m.showIssueInput = true
			m.activeContext = "issue-input"
			m.initIssueInput()
			return m, nil
		}
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

// consumesTextInput returns true when the active context should treat printable
// keys as text input (block app-level navigation shortcuts).
func (m *Model) consumesTextInput() bool {
	if p := m.ActivePlugin(); p != nil {
		if c, ok := p.(plugin.TextInputConsumer); ok && c.ConsumesTextInput() {
			return true
		}
	}
	return isTextInputContext(m.activeContext)
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
	case "notes-list":
		return true
	default:
		return false
	}
}

// isTextInputContext returns true if the context is a text input mode
// where alphanumeric keys should be forwarded to the plugin for typing.
func isTextInputContext(ctx string) bool {
	switch ctx {
	case "td-search", "td-form", "td-board-editor", "td-confirm", "td-close-confirm",
		"theme-switcher",
		"issue-input":
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
func (m *Model) handleProjectSwitcherMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureProjectSwitcherModal()
	if m.projectSwitcherModal == nil {
		return m, nil
	}
	if m.projectSwitcherMouseHandler == nil {
		m.projectSwitcherMouseHandler = mouse.NewHandler()
	}

	// Handle scroll wheel for project list navigation
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.projectSwitcherCursor--
		if m.projectSwitcherCursor < 0 {
			m.projectSwitcherCursor = 0
		}
		m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(
			m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
		m.clearProjectSwitcherModal()
		m.previewProjectTheme()
		return m, nil
	case tea.MouseButtonWheelDown:
		projects := m.projectSwitcherFiltered
		m.projectSwitcherCursor++
		if m.projectSwitcherCursor >= len(projects) {
			m.projectSwitcherCursor = len(projects) - 1
		}
		if m.projectSwitcherCursor < 0 {
			m.projectSwitcherCursor = 0
		}
		m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(
			m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
		m.clearProjectSwitcherModal()
		m.previewProjectTheme()
		return m, nil
	}

	action := m.projectSwitcherModal.HandleMouse(msg, m.projectSwitcherMouseHandler)

	// Check if action is a project item click
	if strings.HasPrefix(action, projectSwitcherItemPrefix) {
		// Extract index from item ID
		var idx int
		if _, err := fmt.Sscanf(action, projectSwitcherItemPrefix+"%d", &idx); err == nil {
			projects := m.projectSwitcherFiltered
			if idx >= 0 && idx < len(projects) {
				selectedProject := projects[idx]
				m.resetProjectSwitcher()
				m.updateContext()
				return m, m.switchProject(selectedProject.Path)
			}
		}
		return m, nil
	}

	switch action {
	case "cancel":
		m.resetProjectSwitcher()
		m.updateContext()
		return m, nil
	case "select":
		projects := m.projectSwitcherFiltered
		if m.projectSwitcherCursor >= 0 && m.projectSwitcherCursor < len(projects) {
			selectedProject := projects[m.projectSwitcherCursor]
			m.resetProjectSwitcher()
			m.updateContext()
			return m, m.switchProject(selectedProject.Path)
		}
		return m, nil
	}

	return m, nil
}

// handleThemeSwitcherMouse handles mouse events for the theme switcher modal.
func (m *Model) handleThemeSwitcherMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
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
			entry := themes[m.themeSwitcherSelectedIdx]
			m.previewThemeEntry(entry)
			var tc config.ThemeConfig
			if entry.IsBuiltIn {
				tc = config.ThemeConfig{Name: entry.ThemeKey}
			} else {
				tc = config.ThemeConfig{Name: "default", Community: entry.ThemeKey}
			}
			return m, m.confirmThemeSelection(tc, entry.Name)
		}
	}
	return m, nil
}

// handleQuitConfirmMouse handles mouse events for the quit confirmation modal.
func (m *Model) handleHelpModalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureHelpModal()
	if m.helpModal == nil {
		return m, nil
	}
	// Info-only modal - no mouse interaction needed beyond ensuring modal exists
	return m, nil
}

// handleUpdateModalKey handles keyboard input for the update modal.
func (m *Model) handleUpdateModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Handle changelog overlay first if visible
	if m.changelogVisible {
		switch key {
		case "j", "down":
			m.changelogScrollOffset++
			m.syncChangelogScroll()
			return m, nil
		case "k", "up":
			if m.changelogScrollOffset > 0 {
				m.changelogScrollOffset--
				m.syncChangelogScroll()
			}
			return m, nil
		case "ctrl+d", "pgdown":
			m.changelogScrollOffset += 10
			m.syncChangelogScroll()
			return m, nil
		case "ctrl+u", "pgup":
			m.changelogScrollOffset -= 10
			if m.changelogScrollOffset < 0 {
				m.changelogScrollOffset = 0
			}
			m.syncChangelogScroll()
			return m, nil
		case "g":
			m.changelogScrollOffset = 0
			m.syncChangelogScroll()
			return m, nil
		case "G":
			m.changelogScrollOffset = 999999 // Will be clamped during render
			m.syncChangelogScroll()
			return m, nil
		case "esc", "c", "q":
			m.changelogVisible = false
			m.changelogScrollOffset = 0
			m.clearChangelogModal()
			return m, nil
		}
		// Route to modal for Enter (close button)
		m.ensureChangelogModal()
		if m.changelogModal != nil {
			action, _ := m.changelogModal.HandleKey(msg)
			if action == "cancel" {
				m.changelogVisible = false
				m.changelogScrollOffset = 0
				m.clearChangelogModal()
				return m, nil
			}
		}
		return m, nil
	}

	// Handle keys based on modal state
	switch m.updateModalState {
	case UpdateModalPreview:
		// Handle special keys first
		switch key {
		case "c":
			// Show changelog
			m.changelogScrollOffset = 0
			if m.updateChangelog == "" {
				m.changelogVisible = true
				return m, fetchChangelog()
			}
			m.changelogVisible = true
			return m, nil
		case "q":
			m.updateModalState = UpdateModalClosed
			return m, nil
		}
		// Route to modal for Tab/Shift+Tab/Enter/Esc
		m.ensureUpdatePreviewModal()
		if m.updatePreviewModal != nil {
			action, cmd := m.updatePreviewModal.HandleKey(msg)
			switch action {
			case "update":
				m.updateModalState = UpdateModalProgress
				m.updateInProgress = true
				m.updateStartTime = time.Now()
				m.initPhaseStatus()
				m.updatePhase = PhaseCheckPrereqs
				m.updatePhaseStatus[PhaseCheckPrereqs] = "running"
				return m, m.startUpdateWithPhases()
			case "cancel":
				m.updateModalState = UpdateModalClosed
				return m, nil
			}
			if cmd != nil {
				return m, cmd
			}
		}

	case UpdateModalProgress:
		// No keys during progress (except Esc handled earlier)
		return m, nil

	case UpdateModalComplete:
		// Handle 'q' specially for quit
		if key == "q" {
			if activePlugin := m.ActivePlugin(); activePlugin != nil {
				_ = state.SetActivePlugin(m.ui.ProjectRoot, activePlugin.ID())
			}
			m.registry.Stop()
			return m, tea.Quit
		}
		// Route to modal for Tab/Shift+Tab/Enter/Esc
		m.ensureUpdateCompleteModal()
		if m.updateCompleteModal != nil {
			action, cmd := m.updateCompleteModal.HandleKey(msg)
			switch action {
			case "quit":
				if activePlugin := m.ActivePlugin(); activePlugin != nil {
					_ = state.SetActivePlugin(m.ui.ProjectRoot, activePlugin.ID())
				}
				m.registry.Stop()
				return m, tea.Quit
			case "cancel":
				m.updateModalState = UpdateModalClosed
				return m, nil
			}
			if cmd != nil {
				return m, cmd
			}
		}

	case UpdateModalError:
		// Handle 'r' for retry and 'q' for close
		switch key {
		case "r":
			m.updateModalState = UpdateModalProgress
			m.updateError = ""
			m.updateStartTime = time.Now()
			m.initPhaseStatus()
			m.updatePhase = PhaseCheckPrereqs
			m.updatePhaseStatus[PhaseCheckPrereqs] = "running"
			return m, m.startUpdateWithPhases()
		case "q":
			m.updateModalState = UpdateModalClosed
			return m, nil
		}
		// Route to modal for Tab/Shift+Tab/Enter/Esc
		m.ensureUpdateErrorModal()
		if m.updateErrorModal != nil {
			action, cmd := m.updateErrorModal.HandleKey(msg)
			switch action {
			case "retry":
				m.updateModalState = UpdateModalProgress
				m.updateError = ""
				m.updateStartTime = time.Now()
				m.initPhaseStatus()
				m.updatePhase = PhaseCheckPrereqs
				m.updatePhaseStatus[PhaseCheckPrereqs] = "running"
				return m, m.startUpdateWithPhases()
			case "cancel":
				m.updateModalState = UpdateModalClosed
				return m, nil
			}
			if cmd != nil {
				return m, cmd
			}
		}
	}

	return m, nil
}

// handleUpdateModalMouse handles mouse events for the update modal.
func (m *Model) handleUpdateModalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Handle changelog overlay first if visible
	if m.changelogVisible {
		m.ensureChangelogModal()
		if m.changelogMouseHandler == nil {
			m.changelogMouseHandler = mouse.NewHandler()
		}
		// Handle scroll events via shared state pointer (no modal rebuild needed)
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.changelogScrollOffset > 0 {
				m.changelogScrollOffset -= 3
				if m.changelogScrollOffset < 0 {
					m.changelogScrollOffset = 0
				}
				m.syncChangelogScroll()
			}
			return m, nil
		case tea.MouseButtonWheelDown:
			m.changelogScrollOffset += 3
			m.syncChangelogScroll()
			return m, nil
		}
		// Handle modal interaction (close button, backdrop)
		if m.changelogModal != nil {
			action := m.changelogModal.HandleMouse(msg, m.changelogMouseHandler)
			if action == "cancel" {
				m.changelogVisible = false
				m.changelogScrollOffset = 0
				m.clearChangelogModal()
				return m, nil
			}
		}
		return m, nil
	}

	switch m.updateModalState {
	case UpdateModalPreview:
		m.ensureUpdatePreviewModal()
		if m.updatePreviewModal == nil {
			return m, nil
		}
		if m.updatePreviewMouseHandler == nil {
			m.updatePreviewMouseHandler = mouse.NewHandler()
		}
		action := m.updatePreviewModal.HandleMouse(msg, m.updatePreviewMouseHandler)
		switch action {
		case "update":
			m.updateModalState = UpdateModalProgress
			m.updateInProgress = true
			m.updateStartTime = time.Now()
			m.initPhaseStatus()
			m.updatePhase = PhaseCheckPrereqs
			m.updatePhaseStatus[PhaseCheckPrereqs] = "running"
			return m, m.startUpdateWithPhases()
		case "cancel":
			m.updateModalState = UpdateModalClosed
			return m, nil
		}

	case UpdateModalComplete:
		m.ensureUpdateCompleteModal()
		if m.updateCompleteModal == nil {
			return m, nil
		}
		if m.updateCompleteMouseHandler == nil {
			m.updateCompleteMouseHandler = mouse.NewHandler()
		}
		action := m.updateCompleteModal.HandleMouse(msg, m.updateCompleteMouseHandler)
		switch action {
		case "quit":
			if activePlugin := m.ActivePlugin(); activePlugin != nil {
				_ = state.SetActivePlugin(m.ui.ProjectRoot, activePlugin.ID())
			}
			m.registry.Stop()
			return m, tea.Quit
		case "cancel":
			m.updateModalState = UpdateModalClosed
			return m, nil
		}

	case UpdateModalError:
		m.ensureUpdateErrorModal()
		if m.updateErrorModal == nil {
			return m, nil
		}
		if m.updateErrorMouseHandler == nil {
			m.updateErrorMouseHandler = mouse.NewHandler()
		}
		action := m.updateErrorModal.HandleMouse(msg, m.updateErrorMouseHandler)
		switch action {
		case "retry":
			m.updateModalState = UpdateModalProgress
			m.updateError = ""
			m.updateStartTime = time.Now()
			m.initPhaseStatus()
			m.updatePhase = PhaseCheckPrereqs
			m.updatePhaseStatus[PhaseCheckPrereqs] = "running"
			return m, m.startUpdateWithPhases()
		case "cancel":
			m.updateModalState = UpdateModalClosed
			return m, nil
		}
	}

	return m, nil
}

func (m *Model) handleQuitConfirmMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	action := m.quitModal.HandleMouse(msg, m.quitMouseHandler)
	switch action {
	case "quit":
		// Save active plugin before quitting
		if activePlugin := m.ActivePlugin(); activePlugin != nil {
			_ = state.SetActivePlugin(m.ui.ProjectRoot, activePlugin.ID())
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
func (m *Model) handleProjectAddThemePickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			if m.projectAdd != nil {
				m.projectAdd.themeSelected = m.projectAddThemeFiltered[m.projectAddThemeCursor]
			}
		}
		m.projectAddModalWidth = 0 // Force modal rebuild to show new theme
		m.resetProjectAddThemePicker()
		// Restore theme
		resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
		theme.ApplyResolved(resolved)
		return m, nil
	}

	// Filter out unparsed mouse escape sequences
	if isMouseEscapeSequence(msg) {
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
func (m *Model) handleProjectAddCommunityKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			if m.projectAdd != nil {
				m.projectAdd.themeSelected = m.projectAddCommunityList[m.projectAddCommunityCursor]
			}
		}
		m.projectAddModalWidth = 0 // Force modal rebuild to show new theme
		m.resetProjectAddThemePicker()
		// Restore theme
		resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
		theme.ApplyResolved(resolved)
		return m, nil
	}

	return m, nil
}

// issueInputSubmit resolves the current issue input (selected result or typed ID)
// and either opens the full issue in TD monitor or shows a lightweight preview.
func (m *Model) issueInputSubmit() (tea.Model, tea.Cmd) {
	var issueID string
	if m.issueSearchCursor >= 0 && m.issueSearchCursor < len(m.issueSearchResults) {
		issueID = m.issueSearchResults[m.issueSearchCursor].ID
	} else {
		issueID = strings.TrimSpace(m.issueInputInput.Value())
	}
	if issueID == "" {
		return m, nil
	}
	// Check if active plugin is TD monitor — go directly to rich modal
	if p := m.ActivePlugin(); p != nil && p.ID() == "td-monitor" {
		m.resetIssueInput()
		m.updateContext()
		return m, tea.Batch(
			func() tea.Msg { return OpenFullIssueMsg{IssueID: issueID} },
		)
	}
	// Hide input modal but preserve search state so "back" can restore it.
	m.showIssueInput = false
	// Show lightweight preview
	m.showIssuePreview = true
	m.activeContext = "issue-preview"
	m.issuePreviewLoading = true
	m.issuePreviewData = nil
	m.issuePreviewError = nil
	m.issuePreviewModal = nil
	m.issuePreviewModalWidth = 0
	m.issuePreviewMouseHandler = mouse.NewHandler()
	return m, fetchIssuePreviewCmd(m.ui.WorkDir, issueID)
}

// handleIssueInputMouse handles mouse events for the issue input modal.
func (m *Model) handleIssueInputMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureIssueInputModal()
	if m.issueInputModal == nil {
		return m, nil
	}
	if m.issueInputMouseHandler == nil {
		m.issueInputMouseHandler = mouse.NewHandler()
	}
	// Pre-render to sync hit regions and focusIDs on the (potentially rebuilt) modal.
	// The issue input modal is nilled on every keystroke to fix a stale text-input
	// pointer, so the modal object seen here may lack focusIDs from a prior Render.
	m.issueInputModal.Render(m.width, m.height, m.issueInputMouseHandler)
	action := m.issueInputModal.HandleMouse(msg, m.issueInputMouseHandler)
	switch {
	case action == "cancel":
		m.resetIssueInput()
		m.updateContext()
	case action == "open":
		return m.issueInputSubmit()
	case strings.HasPrefix(action, issueSearchResultPrefix):
		// Click on a search result — select it and submit
		idxStr := strings.TrimPrefix(action, issueSearchResultPrefix)
		if idx, err := strconv.Atoi(idxStr); err == nil && idx >= 0 && idx < len(m.issueSearchResults) {
			m.issueSearchCursor = idx
			return m.issueInputSubmit()
		}
	}
	return m, nil
}

// handleIssuePreviewMouse handles mouse events for the issue preview modal.
func (m *Model) handleIssuePreviewMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.ensureIssuePreviewModal()
	if m.issuePreviewModal == nil {
		return m, nil
	}
	if m.issuePreviewMouseHandler == nil {
		m.issuePreviewMouseHandler = mouse.NewHandler()
	}
	// Pre-render to sync hit regions and focusIDs on the modal, which may have
	// been rebuilt (e.g. after data/error arrival cleared the cache).
	m.issuePreviewModal.Render(m.width, m.height, m.issuePreviewMouseHandler)
	action := m.issuePreviewModal.HandleMouse(msg, m.issuePreviewMouseHandler)
	switch action {
	case "cancel":
		m.resetIssuePreview()
		m.resetIssueInput()
		m.updateContext()
	case "back":
		m.backToIssueInput()
		return m, nil
	case "open-in-td":
		issueID := ""
		if m.issuePreviewData != nil {
			issueID = m.issuePreviewData.ID
		}
		m.resetIssuePreview()
		m.resetIssueInput()
		m.updateContext()
		if issueID != "" {
			return m, tea.Batch(
				FocusPlugin("td-monitor"),
				func() tea.Msg { return OpenFullIssueMsg{IssueID: issueID} },
			)
		}
	}
	return m, nil
}
