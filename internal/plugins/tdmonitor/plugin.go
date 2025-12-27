package tdmonitor

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/td/pkg/monitor"
	"github.com/sst/sidecar/internal/app"
	"github.com/sst/sidecar/internal/plugin"
)

const (
	pluginID   = "td-monitor"
	pluginName = "td monitor"
	pluginIcon = "T"

	pollInterval = 2 * time.Second
)

// Plugin wraps td's monitor TUI as a sidecar plugin.
// This provides full feature parity with the standalone `td monitor` command.
type Plugin struct {
	ctx     *plugin.Context
	focused bool

	// Embedded td monitor model
	model *monitor.Model

	// View dimensions (passed to model on each render)
	width  int
	height int
}

// New creates a new TD Monitor plugin.
func New() *Plugin {
	return &Plugin{}
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return pluginID }

// Name returns the plugin display name.
func (p *Plugin) Name() string { return pluginName }

// Icon returns the plugin icon character.
func (p *Plugin) Icon() string { return pluginIcon }

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	p.ctx = ctx

	// Try to create embedded monitor - silent degradation if database not found
	model, err := monitor.NewEmbedded(ctx.WorkDir, pollInterval)
	if err != nil {
		// Database not initialized - plugin loads but is non-functional
		p.ctx.Logger.Debug("td monitor: database not found", "error", err)
		return nil
	}

	p.model = model
	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	if p.model == nil {
		return nil
	}
	// Delegate to monitor's Init which starts data fetch and tick
	return p.model.Init()
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	if p.model != nil {
		p.model.Close()
	}
}

// Update handles messages by delegating to the embedded monitor.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	if p.model == nil {
		return p, nil
	}

	// Handle window size - store for View() and forward to monitor
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		p.width = wsm.Width
		p.height = wsm.Height
	}

	// Refresh data when plugin becomes focused
	if _, ok := msg.(app.PluginFocusedMsg); ok {
		return p, p.model.Init()
	}

	// Intercept quit to prevent monitor from exiting the whole app
	if km, ok := msg.(tea.KeyMsg); ok {
		if km.String() == "q" || km.String() == "ctrl+c" {
			// Don't quit the app, just ignore
			return p, nil
		}
	}

	// Delegate to monitor
	newModel, cmd := p.model.Update(msg)

	// Update our reference (monitor uses value semantics)
	if m, ok := newModel.(monitor.Model); ok {
		p.model = &m
	}

	return p, cmd
}

// View renders the plugin by delegating to the embedded monitor.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height

	var content string
	if p.model == nil {
		content = renderNoDatabase()
	} else {
		// Set dimensions on model before rendering
		p.model.Width = width
		p.model.Height = height
		content = p.model.View()
	}

	// Constrain output to allocated height to prevent header scrolling off-screen.
	// MaxHeight truncates content that exceeds the allocated space.
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
}

// IsFocused returns whether the plugin is focused.
func (p *Plugin) IsFocused() bool { return p.focused }

// SetFocused sets the focus state.
func (p *Plugin) SetFocused(f bool) { p.focused = f }

// Commands returns the available commands.
func (p *Plugin) Commands() []plugin.Command {
	if p.model == nil {
		return nil
	}

	// Expose td monitor's key commands
	return []plugin.Command{
		{ID: "open-details", Name: "Open details", Context: "td-monitor"},
		{ID: "search", Name: "Search", Context: "td-monitor"},
		{ID: "toggle-closed", Name: "Toggle closed", Context: "td-monitor"},
		{ID: "approve", Name: "Approve", Context: "td-monitor"},
		{ID: "mark-review", Name: "Review", Context: "td-monitor"},
		{ID: "delete", Name: "Delete", Context: "td-monitor"},
		{ID: "stats", Name: "Stats", Context: "td-monitor"},
		{ID: "help", Name: "Help", Context: "td-monitor"},
		{ID: "close-modal", Name: "Close", Context: "td-modal"},
	}
}

// FocusContext returns the current focus context based on monitor state.
func (p *Plugin) FocusContext() string {
	if p.model == nil {
		return "td-monitor"
	}

	// Check if modal is open
	if p.model.ModalOpen || p.model.StatsOpen || p.model.ConfirmOpen {
		return "td-modal"
	}

	return "td-monitor"
}

// Diagnostics returns plugin health info.
func (p *Plugin) Diagnostics() []plugin.Diagnostic {
	status := "ok"
	detail := ""

	if p.model == nil {
		status = "disabled"
		detail = "no database"
	} else {
		// Count issues across categories
		total := len(p.model.InProgress) +
			len(p.model.TaskList.Ready) +
			len(p.model.TaskList.Reviewable) +
			len(p.model.TaskList.Blocked)
		if total == 1 {
			detail = "1 issue"
		} else {
			detail = formatCount(total, "issue", "issues")
		}
	}

	return []plugin.Diagnostic{
		{ID: "td-monitor", Status: status, Detail: detail},
	}
}

// renderNoDatabase returns a view when no td database is found.
func renderNoDatabase() string {
	return "No td database found.\nRun 'td init' to initialize."
}

// formatCount formats a count with singular/plural forms.
func formatCount(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}
