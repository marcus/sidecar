package run

import "github.com/marcus/sidecar/internal/plugin"

// Commands returns the available commands for the footer.
func (p *Plugin) Commands() []plugin.Command {
	if p.activePane == PaneOutput {
		cmds := []plugin.Command{
			{ID: "switch-pane", Name: "Focus", Description: "Switch to command list", Context: "run-output", Priority: 1},
		}
		session := p.selectedSession()
		if session != nil && session.Status == StatusRunning {
			cmds = append(cmds, plugin.Command{ID: "stop", Name: "Stop", Description: "Stop running command", Context: "run-output", Priority: 2})
		}
		return cmds
	}

	// List pane commands
	cmds := []plugin.Command{
		{ID: "run", Name: "Run", Description: "Run selected command", Context: "run-list", Priority: 1},
		{ID: "refresh", Name: "Refresh", Description: "Re-detect commands", Context: "run-list", Priority: 2},
	}

	session := p.selectedSession()
	if session != nil && session.Status == StatusRunning {
		cmds = append(cmds, plugin.Command{ID: "stop", Name: "Stop", Description: "Stop running command", Context: "run-list", Priority: 3})
	}

	if session != nil {
		cmds = append(cmds, plugin.Command{ID: "switch-pane", Name: "Output", Description: "View command output", Context: "run-list", Priority: 4})
	}

	return cmds
}

// FocusContext returns the current focus context for keybinding dispatch.
func (p *Plugin) FocusContext() string {
	if p.activePane == PaneOutput {
		return "run-output"
	}
	return "run-list"
}
