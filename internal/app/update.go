package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	}

	// Forward other messages to active plugin
	if p := m.ActivePlugin(); p != nil {
		newPlugin, cmd := p.Update(msg)
		// Plugin returned - update registry reference
		plugins := m.registry.Plugins()
		if m.activePlugin < len(plugins) {
			plugins[m.activePlugin] = newPlugin
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

// handleKeyMsg processes keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global quit - always takes precedence
	switch msg.String() {
	case "ctrl+c", "q":
		if !m.showHelp && !m.showDiagnostics {
			m.registry.Stop()
			return m, tea.Quit
		}
	}

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
	}

	// If modal is open, don't process other keys
	if m.showHelp || m.showDiagnostics {
		return m, nil
	}

	// Plugin switching
	switch msg.String() {
	case "tab":
		m.NextPlugin()
		return m, nil
	case "shift+tab":
		m.PrevPlugin()
		return m, nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(msg.Runes[0] - '1')
		m.SetActivePlugin(idx)
		return m, nil
	}

	// Toggles
	switch msg.String() {
	case "?":
		m.showHelp = !m.showHelp
		if m.showHelp {
			m.activeContext = "help"
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
