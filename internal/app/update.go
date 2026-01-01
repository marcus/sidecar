package app

import (
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/palette"
	"github.com/marcus/sidecar/internal/plugins/filebrowser"
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
		// Forward adjusted WindowSizeMsg to all plugins
		// Plugins receive the content area size (minus header)
		adjustedMsg := tea.WindowSizeMsg{
			Width:  msg.Width,
			Height: msg.Height - headerHeight, // headerHeight = 2
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
		// Ignore mouse events when modals are open
		if m.showHelp || m.showDiagnostics || m.showQuitConfirm || m.showPalette {
			return m, nil
		}

		// Handle header tab clicks (Y < 2 means header area)
		if msg.Y < headerHeight && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
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
		if m.intro.Active && !m.intro.Done {
			m.intro.Update(16 * time.Millisecond)
			if m.intro.Done {
				// Animation finished - trigger a refresh to ensure final state is rendered
				return m, Refresh()
			}
			return m, IntroTick()
		}
		return m, nil

	case TickMsg:
		m.ui.UpdateClock()
		m.ui.ClearExpiredToast()
		m.ClearToast()
		return m, tickCmd()

	case ToastMsg:
		m.ShowToast(msg.Message, msg.Duration)
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

	case FocusPluginByIDMsg:
		// Switch to requested plugin
		return m, m.FocusPluginByID(msg.PluginID)

	case filebrowser.OpenFileMsg:
		// Open file in editor using tea.ExecProcess
		c := exec.Command(msg.Editor, msg.Path)
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			if err != nil {
				return ErrorMsg{Err: err}
			}
			return RefreshMsg{}
		})

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
	}

	if m.showQuitConfirm {
		if msg.String() == "y" || msg.Type == tea.KeyEnter {
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

	// If modal is open, don't process other keys
	if m.showHelp || m.showDiagnostics || m.showQuitConfirm {
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
	case "1", "2", "3", "4":
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
		} else {
			m.updateContext()
		}
		return m, nil
	case "ctrl+h":
		m.showFooter = !m.showFooter
		return m, nil
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
	case "conversations", "conversations-sidebar":
		return true
	case "git-status", "git-status-commits", "git-status-diff":
		return true
	case "file-browser-tree":
		return true
	case "td-monitor":
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
		"td-search":
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
