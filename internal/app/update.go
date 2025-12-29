package app

import (
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sst/sidecar/internal/palette"
	"github.com/sst/sidecar/internal/plugins/filebrowser"
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

	// Global quit - ctrl+c always takes precedence, 'q' only in global/empty context
	switch msg.String() {
	case "ctrl+c":
		if !m.showHelp && !m.showDiagnostics && !m.showPalette {
			m.showQuitConfirm = true
			return m, nil
		}
	case "q":
		// 'q' is used for navigation in plugin contexts (back, escape)
		// Only trigger quit in global context or when no plugin is active
		if (m.activeContext == "global" || m.activeContext == "") &&
			!m.showHelp && !m.showDiagnostics && !m.showPalette {
			m.showQuitConfirm = true
			return m, nil
		}
		// Fall through to forward to plugin
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
	case "tab":
		return m, m.NextPlugin()
	case "shift+tab":
		return m, m.PrevPlugin()
	case "1", "2", "3", "4":
		// Only switch plugins in global context; forward to plugin otherwise
		// (e.g., td-monitor uses 1,2,3 for pane switching)
		if m.activeContext == "global" || m.activeContext == "" {
			idx := int(msg.Runes[0] - '1')
			return m, m.SetActivePlugin(idx)
		}
		// Fall through to forward to plugin
	case "g", "t", "c", "f":
		// Only switch plugins in global context; forward to plugin otherwise
		// Plugin-specific bindings take precedence (e.g., 'c' for commit in git-status)
		if m.activeContext == "global" || m.activeContext == "" {
			switch msg.String() {
			case "g":
				return m, m.FocusPluginByID("git-status")
			case "t":
				return m, m.FocusPluginByID("td-monitor")
			case "c":
				return m, m.FocusPluginByID("conversations")
			case "f":
				return m, m.FocusPluginByID("file-browser")
			}
		}
		// Fall through to forward to plugin
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
		// In td-monitor context, 'r' is for mark-review - forward to plugin
		// In other contexts, 'r' triggers global refresh
		if m.activeContext != "td-monitor" {
			return m, Refresh()
		}
		// Fall through to forward to plugin
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
