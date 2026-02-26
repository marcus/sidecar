package tdmonitor

import (
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/td/pkg/monitor"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/workspace"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/tdroot"
)

const (
	pluginID   = "td-monitor"
	pluginName = "td"
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

	// Not-installed view (shown when td binary not found on system)
	notInstalled *NotInstalledModel

	// Setup modal (shown when td is on PATH but project not initialized)
	setupModal *SetupModel

	// todosConflict is set when .todos exists as a file instead of a directory
	todosConflict bool

	// tdOnPath tracks whether td binary is available on the system
	tdOnPath bool

	// View dimensions (passed to model on each render)
	width  int
	height int

	// Track StatusMessage changes to surface as sidecar toasts
	lastStatusMessage string

	// started tracks whether Init() has been called to prevent duplicate poll chains (td-023577)
	started bool
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

	// Clear any stale state from previous initialization (important for project switching)
	p.model = nil
	p.notInstalled = nil
	p.setupModal = nil
	p.todosConflict = false
	p.started = false

	// Check if .todos exists as a file instead of a directory (#194).
	// This must happen before attempting to create the monitor or showing
	// the setup modal, since td init will fail in this state.
	if err := tdroot.CheckTodosConflict(ctx.WorkDir); err != nil {
		p.ctx.Logger.Warn("td monitor: .todos path conflict", "error", err)
		p.todosConflict = true
		return nil
	}

	// Check if td binary is available on PATH
	_, err := exec.LookPath("td")
	p.tdOnPath = err == nil

	// Try to create embedded monitor with custom renderers for gradient borders.
	// Version is empty for embedded use (not displayed in this context).
	opts := monitor.EmbeddedOptions{
		BaseDir:       ctx.WorkDir,
		Interval:      pollInterval,
		Version:       "",
		PanelRenderer: styles.CreateTDPanelRenderer(),
		ModalRenderer: styles.CreateTDModalRenderer(),
		MarkdownTheme: buildMarkdownTheme(),
	}
	model, err := monitor.NewEmbeddedWithOptions(opts)
	if err != nil {
		// Database not initialized - decide which view to show
		p.ctx.Logger.Debug("td monitor: database not found", "error", err)
		if p.tdOnPath {
			// td is installed but project not initialized - show setup modal
			p.setupModal = NewSetupModel(ctx.WorkDir)
		} else {
			// td is not installed on system - show not-installed view
			p.notInstalled = NewNotInstalledModel()
		}
		return nil
	}

	p.model = model

	// Register TD bindings with sidecar's keymap (single source of truth)
	if ctx.Keymap != nil && model.Keymap != nil {
		for _, b := range model.Keymap.ExportBindings() {
			ctx.Keymap.RegisterPluginBinding(b.Key, b.Command, b.Context)
		}
	}

	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	if p.model == nil {
		// Start animation for not-installed view
		if p.notInstalled != nil {
			return p.notInstalled.Init()
		}
		// Setup modal doesn't need animation init
		if p.setupModal != nil {
			return p.setupModal.Init()
		}
		return nil
	}
	// Delegate to monitor's Init which starts data fetch and tick
	// Mark as started to prevent duplicate poll chains on focus (td-023577)
	p.started = true
	return p.model.Init()
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	if p.model != nil {
		_ = p.model.Close()
		p.model = nil
	}
	p.notInstalled = nil
	p.setupModal = nil
	p.started = false
}

// Update handles messages by delegating to the embedded monitor.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	// Handle setup completion - reinitialize to load the monitor
	if _, ok := msg.(SetupCompleteMsg); ok {
		if err := p.Init(p.ctx); err == nil {
			return p, p.Start()
		}
		return p, nil
	}

	// Handle setup error - show error toast
	if errMsg, ok := msg.(SetupErrorMsg); ok {
		return p, func() tea.Msg {
			return app.ToastMsg{
				Message:  errMsg.Error,
				Duration: 5 * time.Second,
				IsError:  true,
			}
		}
	}

	// Handle setup skip - show not-installed view
	if _, ok := msg.(SetupSkippedMsg); ok {
		p.setupModal = nil
		p.notInstalled = NewNotInstalledModel()
		return p, p.notInstalled.Init()
	}

	if p.model == nil {
		// Handle setup modal
		if p.setupModal != nil {
			cmd := p.setupModal.Update(msg)
			return p, cmd
		}
		// Handle not-installed animation
		if p.notInstalled != nil {
			cmd := p.notInstalled.Update(msg)
			return p, cmd
		}
		return p, nil
	}

	// Handle window size - store dimensions and forward to TD
	// The app already adjusts height for the header offset
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		p.width = wsm.Width
		p.height = wsm.Height
		newModel, cmd := p.model.Update(wsm)
		if m, ok := newModel.(monitor.Model); ok {
			p.model = &m
		}
		return p, cmd
	}

	// Skip refresh on focus - the existing poll chain handles periodic updates (td-023577).
	// Calling model.Init() on every focus created duplicate poll chains, causing
	// concurrent adapter.Sessions() calls that accumulated file descriptors.
	if _, ok := msg.(app.PluginFocusedMsg); ok {
		return p, nil
	}

	// Intercept TD's SendTaskToWorktree message and route to workspace plugin
	if sendMsg, ok := msg.(monitor.SendTaskToWorktreeMsg); ok {
		return p, tea.Batch(
			app.FocusPlugin("workspace-manager"),
			func() tea.Msg {
				return workspace.OpenCreateModalWithTaskMsg{
					TaskID:    sendMsg.TaskID,
					TaskTitle: sendMsg.TaskTitle,
				}
			},
		)
	}

	// Handle issue preview "Open in TD" request
	if fullMsg, ok := msg.(app.OpenFullIssueMsg); ok {
		if p.model == nil {
			return p, func() tea.Msg {
				return app.ToastMsg{
					Message:  "TD not initialized",
					Duration: 2 * time.Second,
					IsError:  true,
				}
			}
		}
		newModel, cmd := p.model.Update(monitor.OpenIssueByIDMsg{
			IssueID: fullMsg.IssueID,
		})
		if m, ok := newModel.(monitor.Model); ok {
			p.model = &m
		}
		return p, cmd
	}

	// Delegate to monitor
	newModel, cmd := p.model.Update(msg)

	// Update our reference (monitor uses value semantics)
	if m, ok := newModel.(monitor.Model); ok {
		p.model = &m
	}

	// Intercept tea.Quit to prevent monitor from exiting the whole app.
	// The sidecar app handles quit via quit confirmation modal.
	if cmd != nil {
		originalCmd := cmd
		cmd = func() tea.Msg {
			result := originalCmd()
			if _, isQuit := result.(tea.QuitMsg); isQuit {
				return nil // Suppress quit - let app handle via modal
			}
			return result
		}
	}

	// Surface td toasts to sidecar
	var cmds []tea.Cmd
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Check for StatusMessage changes and emit ToastMsg
	if p.model != nil && p.model.StatusMessage != "" &&
		p.model.StatusMessage != p.lastStatusMessage {
		p.lastStatusMessage = p.model.StatusMessage
		message := p.model.StatusMessage
		isError := p.model.StatusIsError
		cmds = append(cmds, func() tea.Msg {
			return app.ToastMsg{
				Message:  message,
				Duration: 2 * time.Second,
				IsError:  isError,
			}
		})
	} else if p.model != nil && p.model.StatusMessage == "" {
		p.lastStatusMessage = ""
	}

	if len(cmds) == 0 {
		return p, nil
	}
	if len(cmds) == 1 {
		return p, cmds[0]
	}
	return p, tea.Batch(cmds...)
}

// View renders the plugin by delegating to the embedded monitor.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height

	var content string
	if p.todosConflict {
		content = renderConflictView(width)
	} else if p.model == nil {
		if p.setupModal != nil {
			content = p.setupModal.View(width, height)
		} else if p.notInstalled != nil {
			content = p.notInstalled.View(width, height)
		} else {
			content = "No td database found.\nRun 'td init' to initialize."
		}
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

// Commands returns the available commands by consuming TD's exported command metadata.
func (p *Plugin) Commands() []plugin.Command {
	if p.model == nil || p.model.Keymap == nil {
		return nil
	}

	// Get exported commands from TD (single source of truth)
	exported := p.model.Keymap.ExportCommands()
	commands := make([]plugin.Command, 0, len(exported))

	for _, cmd := range exported {
		commands = append(commands, plugin.Command{
			ID:          cmd.ID,
			Name:        cmd.Name,
			Description: cmd.Description,
			Context:     cmd.Context,
			Priority:    cmd.Priority,
			Category:    categorizeCommand(cmd.ID),
		})
	}

	return commands
}

// categorizeCommand returns the appropriate category for a command ID.
func categorizeCommand(id string) plugin.Category {
	switch id {
	case "open-details", "toggle-closed", "open-stats", "toggle-help":
		return plugin.CategoryView
	case "search", "search-confirm", "search-cancel", "search-clear":
		return plugin.CategorySearch
	case "approve", "mark-for-review", "delete", "confirm", "cancel", "refresh", "copy-to-clipboard":
		return plugin.CategoryActions
	case "cursor-down", "cursor-up", "cursor-top", "cursor-bottom",
		"half-page-down", "half-page-up", "full-page-down", "full-page-up",
		"scroll-down", "scroll-up", "next-panel", "prev-panel",
		"focus-panel-1", "focus-panel-2", "focus-panel-3",
		"navigate-prev", "navigate-next", "close", "back", "select",
		"focus-task-section", "open-epic-task", "open-parent-epic", "open-handoffs":
		return plugin.CategoryNavigation
	case "quit":
		return plugin.CategorySystem
	default:
		return plugin.CategoryActions
	}
}

// FocusContext returns the current focus context by consuming TD's context state.
func (p *Plugin) FocusContext() string {
	if p.model == nil {
		return "td-monitor"
	}

	// Delegate to TD's context tracking (single source of truth)
	return p.model.CurrentContextString()
}

// ConsumesTextInput reports whether TD monitor is in a text-entry context.
func (p *Plugin) ConsumesTextInput() bool {
	if p.model == nil {
		return false
	}
	switch p.model.CurrentContextString() {
	case "td-search", "td-form", "td-board-editor", "td-confirm", "td-close-confirm":
		return true
	default:
		return false
	}
}

// Diagnostics returns plugin health info.
func (p *Plugin) Diagnostics() []plugin.Diagnostic {
	status := "ok"
	detail := ""

	if p.todosConflict {
		status = "error"
		detail = ".todos is a file, not a directory"
	} else if p.model == nil {
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

// formatCount formats a count with singular/plural forms.
func formatCount(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// renderConflictView renders the error view when .todos is a file instead of a directory.
func renderConflictView(width int) string {
	theme := styles.GetCurrentTheme()

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.Colors.Error)).
		Render("Cannot initialize td")

	body := lipgloss.NewStyle().
		Width(width - 4).
		Render(
			"Found a .todos file where a directory is expected.\n" +
				"This may have been created by another tool or AI agent.\n\n" +
				"To fix, remove or rename the file:\n\n" +
				"  mv .todos .todos.bak\n" +
				"  td init\n\n" +
				"Then restart sidecar.")

	return lipgloss.JoinVertical(lipgloss.Left, "", title, "", body)
}

// buildMarkdownTheme creates a MarkdownThemeConfig from the current sidecar theme.
// This shares sidecar's color palette with td's markdown renderer.
func buildMarkdownTheme() *monitor.MarkdownThemeConfig {
	theme := styles.GetCurrentTheme()
	c := theme.Colors

	return &monitor.MarkdownThemeConfig{
		// Use the theme's Chroma syntax theme (e.g., "monokai", "dracula")
		SyntaxTheme:   c.SyntaxTheme,
		MarkdownTheme: c.MarkdownTheme,
		// Also provide explicit colors for full theme consistency
		Colors: &monitor.MarkdownColorPalette{
			Primary:   c.Primary,
			Secondary: c.Secondary,
			Success:   c.Success,
			Warning:   c.Warning,
			Error:     c.Error,
			Muted:     c.TextMuted,
			Text:      c.TextPrimary,
			BgCode:    c.BgTertiary,
		},
	}
}
