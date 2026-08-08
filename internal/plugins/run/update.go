package run

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/plugin"
)

const (
	pollInterval           = 500 * time.Millisecond
	pollIntervalBackground = 2 * time.Second
)

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height

	case DetectCommandsMsg:
		p.commands = msg.Commands
		if p.cursor >= len(p.commands) {
			p.cursor = 0
		}

	case RunSessionStartedMsg:
		if msg.Err != nil {
			if p.ctx != nil && p.ctx.Logger != nil {
				p.ctx.Logger.Error("run: session start failed", "error", msg.Err)
			}
			return p, nil
		}
		if msg.Index >= 0 && msg.Index < len(p.commands) {
			p.sessions[msg.Index] = &RunSession{
				SessionName: msg.SessionName,
				Command:     p.commands[msg.Index],
				Status:      StatusRunning,
				StartedAt:   time.Now(),
			}
			// Start polling for output
			return p, schedulePoll(msg.Index, pollInterval)
		}

	case RunOutputMsg:
		session := p.sessions[msg.Index]
		if session == nil {
			return p, nil
		}
		session.Output = msg.Output
		p.outputLines = len(strings.Split(msg.Output, "\n"))

		// Check if session is still alive
		if !isSessionAlive(session.SessionName) {
			session.Status = StatusDone
			return p, nil
		}

		// Schedule next poll
		delay := pollInterval
		if !p.focused {
			delay = pollIntervalBackground
		}
		return p, schedulePoll(msg.Index, delay)

	case PollRunOutputMsg:
		session := p.sessions[msg.Index]
		if session == nil || session.Status != StatusRunning {
			return p, nil
		}
		return p, captureRunOutput(msg.Index, session.SessionName)

	case RunSessionStoppedMsg:
		if session, ok := p.sessions[msg.Index]; ok {
			session.Status = StatusDone
		}

	case tea.KeyMsg:
		return p.handleKey(msg)
	}

	return p, nil
}

// handleKey processes keyboard input.
func (p *Plugin) handleKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()

	switch p.activePane {
	case PaneList:
		return p.handleListKey(key)
	case PaneOutput:
		return p.handleOutputKey(key)
	}
	return p, nil
}

// handleListKey handles keys when the command list is focused.
func (p *Plugin) handleListKey(key string) (plugin.Plugin, tea.Cmd) {
	switch key {
	case "j", "down":
		if p.cursor < len(p.commands)-1 {
			p.cursor++
			p.outputScrollOff = 0
			p.ensureVisible()
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
			p.outputScrollOff = 0
			p.ensureVisible()
		}
	case "g":
		// Jump to top (simplified - no gg sequence for now)
	case "G":
		if len(p.commands) > 0 {
			p.cursor = len(p.commands) - 1
			p.outputScrollOff = 0
			p.ensureVisible()
		}
	case "enter":
		return p, p.runSelected()
	case "x":
		return p, p.stopSelected()
	case "r":
		return p, p.detectCommandsCmd()
	case "tab":
		if p.selectedSession() != nil {
			p.activePane = PaneOutput
		}
	case "l", "right":
		if p.selectedSession() != nil {
			p.activePane = PaneOutput
		}
	}
	return p, nil
}

// handleOutputKey handles keys when the output pane is focused.
func (p *Plugin) handleOutputKey(key string) (plugin.Plugin, tea.Cmd) {
	switch key {
	case "tab", "h", "left", "esc":
		p.activePane = PaneList
	case "j", "down":
		p.outputScrollOff++
	case "k", "up":
		if p.outputScrollOff > 0 {
			p.outputScrollOff--
		}
	case "ctrl+d":
		p.outputScrollOff += 10
	case "ctrl+u":
		if p.outputScrollOff > 10 {
			p.outputScrollOff -= 10
		} else {
			p.outputScrollOff = 0
		}
	case "x":
		return p, p.stopSelected()
	}
	return p, nil
}

// runSelected starts the currently selected command.
func (p *Plugin) runSelected() tea.Cmd {
	cmd := p.selectedCommand()
	if cmd == nil {
		return nil
	}

	// If already running, stop it first then restart
	if session := p.selectedSession(); session != nil && session.Status == StatusRunning {
		return tea.Sequence(
			stopRunSession(p.cursor, session.SessionName),
			startRunSession(p.cursor, *cmd, p.ctx.WorkDir),
		)
	}

	return startRunSession(p.cursor, *cmd, p.ctx.WorkDir)
}

// stopSelected stops the currently running command.
func (p *Plugin) stopSelected() tea.Cmd {
	session := p.selectedSession()
	if session == nil || session.Status != StatusRunning {
		return nil
	}
	return stopRunSession(p.cursor, session.SessionName)
}

// ensureVisible adjusts scroll to keep selected item visible.
func (p *Plugin) ensureVisible() {
	if p.cursor < p.scrollOff {
		p.scrollOff = p.cursor
	}
	visibleCount := p.listVisibleCount()
	if visibleCount > 0 && p.cursor >= p.scrollOff+visibleCount {
		p.scrollOff = p.cursor - visibleCount + 1
	}
	if p.scrollOff < 0 {
		p.scrollOff = 0
	}
}

// listVisibleCount returns how many list items fit in the sidebar.
func (p *Plugin) listVisibleCount() int {
	// height minus border (2) minus header (1)
	count := p.height - 3
	if count < 1 {
		count = 1
	}
	return count
}
