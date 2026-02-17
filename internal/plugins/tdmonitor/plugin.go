package tdmonitor

import (
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/integration"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/workspace"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/td/pkg/monitor"
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

	// Sync modal (provider-aware)
	syncModal *SyncModel

	// Provider picker modal (shown when multiple providers available)
	providerPicker *ProviderPickerModel

	// Jira setup modal (shown when Jira not yet configured)
	jiraSetupModal *JiraSetupModel

	// Quick-create workspace modal (overlays on TD tab)
	quickCreateModal *QuickCreateModel

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
	p.syncModal = nil
	p.providerPicker = nil
	p.jiraSetupModal = nil
	p.quickCreateModal = nil
	p.started = false

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
		textEntryContexts := map[string]bool{
			"td-search": true, "td-form": true, "td-board-editor": true,
			"td-confirm": true, "td-close-confirm": true,
		}
		registeredN := map[string]bool{}
		for _, b := range model.Keymap.ExportBindings() {
			ctx.Keymap.RegisterPluginBinding(b.Key, b.Command, b.Context)
			// Register N (new workspace from ticket) for non-text-entry TD contexts
			if !textEntryContexts[b.Context] && !registeredN[b.Context] {
				ctx.Keymap.RegisterPluginBinding("N", "new-workspace-from-ticket", b.Context)
				registeredN[b.Context] = true
			}
		}
		// Register sync keybinding
		ctx.Keymap.RegisterPluginBinding("ctrl+g", "sync", "td-monitor")
		// Register push-one keybinding for modal context
		ctx.Keymap.RegisterPluginBinding("ctrl+g", "push-issue", "td-modal")
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
	p.syncModal = nil
	p.providerPicker = nil
	p.jiraSetupModal = nil
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

	// Handle sync modal dismiss
	if _, ok := msg.(SyncDismissMsg); ok {
		p.syncModal = nil
		return p, nil
	}

	// Handle provider picker dismiss
	if _, ok := msg.(ProviderPickerDismissMsg); ok {
		p.providerPicker = nil
		return p, nil
	}

	// Handle Jira setup dismiss
	if _, ok := msg.(JiraSetupDismissMsg); ok {
		p.jiraSetupModal = nil
		return p, nil
	}

	// Handle Jira setup complete — update in-memory config and open sync modal
	if setupMsg, ok := msg.(JiraSetupCompleteMsg); ok {
		p.jiraSetupModal = nil

		// Update in-memory config so subsequent operations use the new values
		if p.ctx.Config != nil {
			p.ctx.Config.Integrations.Jira = config.JiraIntegrationConfig{
				Enabled:    true,
				URL:        setupMsg.URL,
				Email:      setupMsg.Email,
				APIToken:   setupMsg.APIToken,
				ProjectKey: setupMsg.ProjectKey,
			}
		}

		// Build provider from the setup values directly
		provider := integration.NewJiraProvider(integration.JiraProviderOptions{
			URL:        setupMsg.URL,
			ProjectKey: setupMsg.ProjectKey,
			Email:      setupMsg.Email,
			APIToken:   setupMsg.APIToken,
		})
		p.syncModal = NewSyncModel(p.ctx.WorkDir, provider)

		return p, func() tea.Msg {
			return app.ToastMsg{
				Message:  "Jira configured successfully",
				Duration: 2 * time.Second,
			}
		}
	}

	// Handle provider picked from picker modal
	if pickedMsg, ok := msg.(ProviderPickedMsg); ok {
		p.providerPicker = nil
		// If Jira was picked but not configured, show setup
		if pickedMsg.Provider.ID() == "jira" && !p.isJiraConfigured() {
			p.jiraSetupModal = NewJiraSetupModel()
			return p, nil
		}
		p.syncModal = NewSyncModel(p.ctx.WorkDir, pickedMsg.Provider)
		return p, nil
	}

	// Handle single-issue push completion (no modal involved)
	if doneMsg, ok := msg.(SyncPushOneDoneMsg); ok {
		providerName := doneMsg.ProviderName
		if providerName == "" {
			providerName = "provider"
		}
		if doneMsg.Err != nil {
			return p, func() tea.Msg {
				return app.ToastMsg{
					Message:  fmt.Sprintf("Push %s failed: %v", doneMsg.IssueID, doneMsg.Err),
					Duration: 3 * time.Second,
					IsError:  true,
				}
			}
		}
		return p, func() tea.Msg {
			message := fmt.Sprintf("Pushed %s to %s", doneMsg.IssueID, providerName)
			if len(doneMsg.Result.Errors) > 0 {
				message += fmt.Sprintf(" (%d errors)", len(doneMsg.Result.Errors))
			}
			return app.ToastMsg{
				Message:  message,
				Duration: 3 * time.Second,
			}
		}
	}

	// Route sync completion messages to the sync modal
	if p.syncModal != nil {
		switch msg.(type) {
		case SyncPullDoneMsg, SyncPushDoneMsg:
			cmd := p.syncModal.Update(msg)
			return p, cmd
		}
	}

	// Handle Jira setup test result
	if p.jiraSetupModal != nil {
		if _, ok := msg.(JiraSetupTestDoneMsg); ok {
			cmd := p.jiraSetupModal.Update(msg)
			return p, cmd
		}
	}

	// Handle ctrl+g in modal context — push current issue to default provider
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+g" && p.model != nil && p.syncModal == nil && p.model.ModalOpen() {
		if modalInfo := p.model.CurrentModal(); modalInfo != nil && modalInfo.IssueID != "" {
			issueID := modalInfo.IssueID
			provider := p.getDefaultPushProvider()
			if provider == nil {
				return p, func() tea.Msg {
					return app.ToastMsg{
						Message:  "No sync provider available",
						Duration: 2 * time.Second,
						IsError:  true,
					}
				}
			}
			syncModel := NewSyncModel(p.ctx.WorkDir, provider)
			providerName := provider.Name()
			return p, tea.Batch(
				func() tea.Msg {
					return app.ToastMsg{
						Message:  fmt.Sprintf("Pushing %s to %s...", issueID, providerName),
						Duration: 2 * time.Second,
					}
				},
				syncModel.doPushOne(issueID),
			)
		}
	}

	// Handle ctrl+g to open sync — provider picker if multiple, direct modal if one
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+g" && p.model != nil && p.syncModal == nil && p.providerPicker == nil && p.jiraSetupModal == nil {
		return p, p.openSyncFlow()
	}

	// If Jira setup modal is active, route input to it
	if p.jiraSetupModal != nil {
		if _, ok := msg.(tea.KeyMsg); ok {
			cmd := p.jiraSetupModal.Update(msg)
			return p, cmd
		}
		if _, ok := msg.(tea.MouseMsg); ok {
			cmd := p.jiraSetupModal.Update(msg)
			return p, cmd
		}
	}

	// If provider picker is active, route input to it
	if p.providerPicker != nil {
		if _, ok := msg.(tea.KeyMsg); ok {
			cmd := p.providerPicker.Update(msg)
			return p, cmd
		}
		if _, ok := msg.(tea.MouseMsg); ok {
			cmd := p.providerPicker.Update(msg)
			return p, cmd
		}
	}

	// If sync modal is active, route all input to it
	if p.syncModal != nil {
		if _, ok := msg.(tea.KeyMsg); ok {
			cmd := p.syncModal.Update(msg)
			return p, cmd
		}
		if _, ok := msg.(tea.MouseMsg); ok {
			cmd := p.syncModal.Update(msg)
			return p, cmd
		}
	}

	// Intercept TD's SendTaskToWorktree message and show quick-create modal on TD tab
	if sendMsg, ok := msg.(monitor.SendTaskToWorktreeMsg); ok {
		defaultPlanMode := true
		if p.ctx.Config != nil && p.ctx.Config.Plugins.Workspace.DefaultPlanMode != nil {
			defaultPlanMode = *p.ctx.Config.Plugins.Workspace.DefaultPlanMode
		}
		p.quickCreateModal = NewQuickCreateModel(sendMsg.TaskID, sendMsg.TaskTitle, p.ctx.WorkDir, defaultPlanMode)
		return p, nil
	}

	// Route input to quick-create modal when active
	if p.quickCreateModal != nil {
		switch msg.(type) {
		case tea.KeyMsg, tea.MouseMsg:
			action, cmd := p.quickCreateModal.Update(msg, p.width)
			switch action {
			case "create":
				qcm := p.quickCreateModal
				p.quickCreateModal = nil
				name := qcm.nameInput.Value()
				baseBranch := qcm.baseBranchInput.Value()
				prompt := qcm.SelectedPrompt()
				return p, tea.Batch(cmd, func() tea.Msg {
					return workspace.QuickCreateWorkspaceMsg{
						TaskID:     qcm.taskID,
						TaskTitle:  qcm.taskTitle,
						Name:       name,
						BaseBranch: baseBranch,
						AgentType:  qcm.SelectedAgentType(),
						SkipPerms:  qcm.skipPerms,
						PlanMode:   qcm.PlanMode(),
						Prompt:     prompt,
					}
				})
			case "cancel":
				p.quickCreateModal = nil
				return p, cmd
			}
			return p, cmd
		}
	}

	// Handle N key to open quick-create workspace modal for the selected TD task.
	// Forwards as W to the td model which emits SendTaskToWorktreeMsg (intercepted above).
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "N" && p.quickCreateModal == nil {
		// Skip in text-entry contexts where N should be typed as input
		switch p.model.CurrentContextString() {
		case "td-search", "td-form", "td-board-editor", "td-confirm", "td-close-confirm":
			// Fall through to normal model delegation below
		default:
			wKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}}
			newModel, cmd := p.model.Update(wKey)
			if m, ok := newModel.(monitor.Model); ok {
				p.model = &m
			}
			return p, cmd
		}
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
	if p.model == nil {
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

	// Render overlay modals
	if p.providerPicker != nil {
		content = p.providerPicker.View(content, width, height)
	}
	if p.jiraSetupModal != nil {
		content = p.jiraSetupModal.View(content, width, height)
	}
	if p.syncModal != nil {
		content = p.syncModal.View(content, width, height)
	}

	// Constrain output to allocated height to prevent header scrolling off-screen.
	// MaxHeight truncates content that exceeds the allocated space.
	rendered := lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)

	// Overlay quick-create modal when active
	if p.quickCreateModal != nil {
		modalView := p.quickCreateModal.View(width, height)
		return ui.OverlayModal(rendered, modalView, width, height)
	}

	return rendered
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

	// Add sync command
	commands = append(commands, plugin.Command{
		ID:          "sync",
		Name:        "Sync",
		Description: "Sync issues with provider",
		Context:     "td-monitor",
		Priority:    50,
		Category:    plugin.CategoryActions,
	})

	// Add push-one command for modal context
	commands = append(commands, plugin.Command{
		ID:          "push-issue",
		Name:        "Push",
		Description: "Push issue to provider",
		Context:     "td-modal",
		Priority:    50,
		Category:    plugin.CategoryActions,
	})

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
	// Overlay modals with text inputs consume all keystrokes
	if p.jiraSetupModal != nil {
		return true
	}
	if p.syncModal != nil {
		return true
	}
	if p.providerPicker != nil {
		return true
	}
	if p.quickCreateModal != nil {
		return true
	}
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

// formatCount formats a count with singular/plural forms.
func formatCount(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// configuredProviders returns providers that are fully configured and available.
func (p *Plugin) configuredProviders() []integration.Provider {
	var providers []integration.Provider

	// Check GitHub
	if p.ctx.Config != nil && p.ctx.Config.Integrations.GitHub.Enabled {
		gh := integration.NewGitHubProvider()
		if ok, _ := gh.Available(p.ctx.WorkDir); ok {
			providers = append(providers, gh)
		}
	}

	// Check Jira (fully configured only)
	if p.isJiraConfigured() {
		jiraCfg := p.ctx.Config.Integrations.Jira
		jira := integration.NewJiraProvider(integration.JiraProviderOptions{
			URL:        jiraCfg.URL,
			ProjectKey: jiraCfg.ProjectKey,
			Email:      jiraCfg.Email,
			APIToken:   jiraCfg.APIToken,
		})
		providers = append(providers, jira)
	}

	return providers
}

// offerableProviders returns providers for the picker, including unconfigured
// Jira (selecting it triggers setup). This ensures Jira is always discoverable.
func (p *Plugin) offerableProviders() []integration.Provider {
	providers := p.configuredProviders()

	// Always offer Jira even when not configured — picking it triggers setup
	if !p.isJiraConfigured() {
		// Create a stub provider just for the picker (ID + Name only)
		providers = append(providers, integration.NewJiraProvider(integration.JiraProviderOptions{}))
	}

	return providers
}

// isJiraConfigured returns true if Jira has URL/token configured.
func (p *Plugin) isJiraConfigured() bool {
	if p.ctx.Config == nil {
		return false
	}
	jiraCfg := p.ctx.Config.Integrations.Jira
	return jiraCfg.URL != "" && jiraCfg.APIToken != ""
}

// buildJiraProvider creates a JiraProvider from current config.
func (p *Plugin) buildJiraProvider() integration.Provider {
	if p.ctx.Config == nil {
		return nil
	}
	jiraCfg := p.ctx.Config.Integrations.Jira
	return integration.NewJiraProvider(integration.JiraProviderOptions{
		URL:        jiraCfg.URL,
		ProjectKey: jiraCfg.ProjectKey,
		Email:      jiraCfg.Email,
		APIToken:   jiraCfg.APIToken,
	})
}

// getDefaultPushProvider returns the first configured provider for single-issue push.
func (p *Plugin) getDefaultPushProvider() integration.Provider {
	providers := p.configuredProviders()
	if len(providers) > 0 {
		return providers[0]
	}
	return nil
}

// openSyncFlow determines which modal to show based on available providers.
func (p *Plugin) openSyncFlow() tea.Cmd {
	offerable := p.offerableProviders()

	switch len(offerable) {
	case 0:
		return func() tea.Msg {
			return app.ToastMsg{
				Message:  "No sync providers available",
				Duration: 2 * time.Second,
				IsError:  true,
			}
		}
	case 1:
		provider := offerable[0]
		// If the single provider is unconfigured Jira, show setup
		if provider.ID() == "jira" && !p.isJiraConfigured() {
			p.jiraSetupModal = NewJiraSetupModel()
			return nil
		}
		p.syncModal = NewSyncModel(p.ctx.WorkDir, provider)
		return nil
	default:
		// Multiple options — show picker (always, so Jira setup is discoverable)
		p.providerPicker = NewProviderPickerModel(offerable)
		return nil
	}
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
