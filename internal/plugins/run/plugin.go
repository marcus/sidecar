package run

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/plugin"
)

const (
	pluginID   = "run"
	pluginName = "run"
	pluginIcon = "R"

	// Pane layout
	dividerWidth = 1
)

// FocusPane represents which pane is active.
type FocusPane int

const (
	PaneList   FocusPane = iota // Left sidebar (command list)
	PaneOutput                  // Right pane (tmux output)
)

// Plugin implements the run command plugin.
type Plugin struct {
	ctx     *plugin.Context
	focused bool
	width   int
	height  int

	// Command state
	commands []RunCommand // All available commands (detected + config)
	cursor   int          // Selected command index
	scrollOff int         // Scroll offset for command list

	// Pane state
	activePane   FocusPane
	sidebarWidth int // Percentage width for sidebar

	// Run session tracking
	sessions map[int]*RunSession // Index → active session

	// Output scroll state
	outputScrollOff int
	outputLines     int // Total lines in current output
}

// New creates a new run plugin.
func New() *Plugin {
	return &Plugin{
		commands:     make([]RunCommand, 0),
		sessions:     make(map[int]*RunSession),
		activePane:   PaneList,
		sidebarWidth: 30, // 30% sidebar
	}
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return pluginID }

// Name returns the plugin display name.
func (p *Plugin) Name() string { return pluginName }

// Icon returns the plugin icon.
func (p *Plugin) Icon() string { return pluginIcon }

// IsFocused returns whether the plugin is focused.
func (p *Plugin) IsFocused() bool { return p.focused }

// SetFocused sets the focus state.
func (p *Plugin) SetFocused(f bool) { p.focused = f }

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	p.ctx = ctx
	p.commands = nil
	p.cursor = 0
	p.scrollOff = 0
	p.sessions = make(map[int]*RunSession)
	p.outputScrollOff = 0
	return nil
}

// Start begins async operations.
func (p *Plugin) Start() tea.Cmd {
	return p.detectCommandsCmd()
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	// Kill all active tmux sessions
	for _, session := range p.sessions {
		if session != nil && session.SessionName != "" {
			_ = stopRunSession(0, session.SessionName)
		}
	}
	p.sessions = make(map[int]*RunSession)
}

// detectCommandsCmd runs auto-detection in the background.
func (p *Plugin) detectCommandsCmd() tea.Cmd {
	workDir := p.ctx.WorkDir
	configCmds := p.getConfigCommands()
	return func() tea.Msg {
		detected := detectCommands(workDir)
		// Merge config commands
		all := append(detected, configCmds...)
		return DetectCommandsMsg{Commands: all}
	}
}

// getConfigCommands converts config commands to RunCommand structs.
func (p *Plugin) getConfigCommands() []RunCommand {
	if p.ctx == nil || p.ctx.Config == nil {
		return nil
	}
	var commands []RunCommand
	for _, cmd := range p.ctx.Config.Plugins.Run.Commands {
		group := cmd.Group
		if group == "" {
			group = "config"
		}
		commands = append(commands, RunCommand{
			Name:    cmd.Name,
			Command: cmd.Command,
			Source:  "config",
			Group:   group,
		})
	}
	return commands
}

// selectedCommand returns the currently selected command, or nil.
func (p *Plugin) selectedCommand() *RunCommand {
	if p.cursor < 0 || p.cursor >= len(p.commands) {
		return nil
	}
	return &p.commands[p.cursor]
}

// selectedSession returns the session for the currently selected command.
func (p *Plugin) selectedSession() *RunSession {
	return p.sessions[p.cursor]
}
