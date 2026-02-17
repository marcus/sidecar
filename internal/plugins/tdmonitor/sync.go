package tdmonitor

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/integration"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/ui"
)

// SyncModel handles the provider sync modal.
type SyncModel struct {
	provider integration.Provider
	workDir  string
	todosDir string
	width    int
	height   int

	modal        *modal.Modal
	mouseHandler *mouse.Handler

	syncing bool // true while a sync operation is in progress
}

// SyncPullDoneMsg is sent when a pull operation completes.
type SyncPullDoneMsg struct {
	Result *integration.SyncResult
	Err    error
}

// SyncPushDoneMsg is sent when a push operation completes.
type SyncPushDoneMsg struct {
	Result *integration.SyncResult
	Err    error
}

// NewSyncModel creates a new sync modal for a given provider.
func NewSyncModel(workDir string, provider integration.Provider) *SyncModel {
	m := &SyncModel{
		provider:     provider,
		workDir:      workDir,
		todosDir:     filepath.Join(workDir, ".todos"),
		mouseHandler: mouse.NewHandler(),
	}
	m.buildModal()
	return m
}

func (m *SyncModel) buildModal() {
	title := m.provider.Name() + " Sync"
	desc := fmt.Sprintf("Sync issues between td and %s.", m.provider.Name())
	m.modal = modal.New(title, modal.WithWidth(45)).
		AddSection(modal.Text(desc)).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Pull ", "pull"),
			modal.Btn(" Push ", "push"),
			modal.Btn(" Cancel ", "cancel"),
		))
	m.modal.Reset()
}

// Update handles messages for the sync modal.
func (m *SyncModel) Update(msg tea.Msg) tea.Cmd {
	providerName := m.provider.Name()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return nil

	case tea.KeyMsg:
		if m.syncing {
			return nil // Don't allow input while syncing
		}
		action, cmd := m.modal.HandleKey(msg)
		if action != "" {
			return m.handleAction(action)
		}
		return cmd

	case tea.MouseMsg:
		if m.syncing {
			return nil
		}
		action := m.modal.HandleMouse(msg, m.mouseHandler)
		if action != "" {
			return m.handleAction(action)
		}
		return nil

	case SyncPullDoneMsg:
		m.syncing = false
		if msg.Err != nil {
			return tea.Batch(
				func() tea.Msg { return SyncDismissMsg{} },
				func() tea.Msg {
					return app.ToastMsg{
						Message:  fmt.Sprintf("Pull failed: %v", msg.Err),
						Duration: 3 * time.Second,
						IsError:  true,
					}
				},
			)
		}
		return tea.Batch(
			func() tea.Msg { return SyncDismissMsg{} },
			func() tea.Msg {
				message := fmt.Sprintf("Pulled %d issue(s) from %s", msg.Result.Pulled, providerName)
				if len(msg.Result.Errors) > 0 {
					message += fmt.Sprintf(" (%d errors)", len(msg.Result.Errors))
				}
				return app.ToastMsg{
					Message:  message,
					Duration: 3 * time.Second,
				}
			},
		)

	case SyncPushDoneMsg:
		m.syncing = false
		if msg.Err != nil {
			return tea.Batch(
				func() tea.Msg { return SyncDismissMsg{} },
				func() tea.Msg {
					return app.ToastMsg{
						Message:  fmt.Sprintf("Push failed: %v", msg.Err),
						Duration: 3 * time.Second,
						IsError:  true,
					}
				},
			)
		}
		return tea.Batch(
			func() tea.Msg { return SyncDismissMsg{} },
			func() tea.Msg {
				message := fmt.Sprintf("Pushed %d issue(s) to %s", msg.Result.Pushed, providerName)
				if len(msg.Result.Errors) > 0 {
					message += fmt.Sprintf(" (%d errors)", len(msg.Result.Errors))
				}
				return app.ToastMsg{
					Message:  message,
					Duration: 3 * time.Second,
				}
			},
		)
	}
	return nil
}

func (m *SyncModel) handleAction(action string) tea.Cmd {
	switch action {
	case "pull":
		m.syncing = true
		return m.doPull()
	case "push":
		m.syncing = true
		return m.doPush()
	case "cancel":
		return func() tea.Msg { return SyncDismissMsg{} }
	}
	return nil
}

func (m *SyncModel) doPull() tea.Cmd {
	provider := m.provider
	workDir := m.workDir
	todosDir := m.todosDir
	return func() tea.Msg {
		// Check availability
		if ok, err := provider.Available(workDir); !ok {
			return SyncPullDoneMsg{Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := integration.Pull(ctx, provider, workDir, todosDir)
		return SyncPullDoneMsg{Result: result, Err: err}
	}
}

func (m *SyncModel) doPush() tea.Cmd {
	provider := m.provider
	workDir := m.workDir
	todosDir := m.todosDir
	return func() tea.Msg {
		// Check availability
		if ok, err := provider.Available(workDir); !ok {
			return SyncPushDoneMsg{Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := integration.Push(ctx, provider, workDir, todosDir)
		return SyncPushDoneMsg{Result: result, Err: err}
	}
}

// View renders the sync modal.
func (m *SyncModel) View(background string, width, height int) string {
	m.width = width
	m.height = height

	var modalContent string
	if m.syncing {
		// Show a simple "syncing..." modal
		title := m.provider.Name() + " Sync"
		syncModal := modal.New(title, modal.WithWidth(45)).
			AddSection(modal.Text("Syncing..."))
		syncModal.Reset()
		modalContent = syncModal.Render(width, height, m.mouseHandler)
	} else {
		modalContent = m.modal.Render(width, height, m.mouseHandler)
	}

	return ui.OverlayModal(background, modalContent, width, height)
}

// SyncPushOneDoneMsg is sent when a single-issue push completes.
type SyncPushOneDoneMsg struct {
	IssueID      string
	ProviderName string
	Result       *integration.SyncResult
	Err          error
}

// doPushOne pushes a single td issue to the provider.
func (m *SyncModel) doPushOne(issueID string) tea.Cmd {
	provider := m.provider
	workDir := m.workDir
	todosDir := m.todosDir
	return func() tea.Msg {
		if ok, err := provider.Available(workDir); !ok {
			return SyncPushOneDoneMsg{IssueID: issueID, ProviderName: provider.Name(), Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := integration.PushOne(ctx, provider, workDir, todosDir, issueID)
		return SyncPushOneDoneMsg{IssueID: issueID, ProviderName: provider.Name(), Result: result, Err: err}
	}
}

// SyncDismissMsg is sent when the sync modal should be dismissed.
type SyncDismissMsg struct{}

// --- Provider Picker Modal ---

// ProviderPickerModel shows a modal to choose between available providers.
type ProviderPickerModel struct {
	providers []integration.Provider
	width     int
	height    int

	modal        *modal.Modal
	mouseHandler *mouse.Handler
}

// ProviderPickedMsg is sent when a provider is selected.
type ProviderPickedMsg struct {
	Provider integration.Provider
}

// ProviderPickerDismissMsg is sent when the picker is dismissed.
type ProviderPickerDismissMsg struct{}

// NewProviderPickerModel creates a provider selection modal.
func NewProviderPickerModel(providers []integration.Provider) *ProviderPickerModel {
	m := &ProviderPickerModel{
		providers:    providers,
		mouseHandler: mouse.NewHandler(),
	}
	m.buildModal()
	return m
}

func (m *ProviderPickerModel) buildModal() {
	buttons := make([]modal.ButtonDef, 0, len(m.providers)+1)
	for _, p := range m.providers {
		buttons = append(buttons, modal.Btn(" "+p.Name()+" ", p.ID()))
	}
	buttons = append(buttons, modal.Btn(" Cancel ", "cancel"))

	m.modal = modal.New("Sync Provider", modal.WithWidth(45)).
		AddSection(modal.Text("Select integration:")).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(buttons...))
	m.modal.Reset()
}

// Update handles picker messages.
func (m *ProviderPickerModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return nil

	case tea.KeyMsg:
		action, cmd := m.modal.HandleKey(msg)
		if action != "" {
			return m.handleAction(action)
		}
		return cmd

	case tea.MouseMsg:
		action := m.modal.HandleMouse(msg, m.mouseHandler)
		if action != "" {
			return m.handleAction(action)
		}
		return nil
	}
	return nil
}

func (m *ProviderPickerModel) handleAction(action string) tea.Cmd {
	if action == "cancel" {
		return func() tea.Msg { return ProviderPickerDismissMsg{} }
	}
	for _, p := range m.providers {
		if p.ID() == action {
			provider := p
			return func() tea.Msg { return ProviderPickedMsg{Provider: provider} }
		}
	}
	return nil
}

// View renders the picker modal.
func (m *ProviderPickerModel) View(background string, width, height int) string {
	m.width = width
	m.height = height
	modalContent := m.modal.Render(width, height, m.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}

// --- Jira Setup Modal ---

// JiraSetupModel handles interactive Jira configuration.
type JiraSetupModel struct {
	width  int
	height int

	urlInput        textinput.Model
	emailInput      textinput.Model
	projectKeyInput textinput.Model
	apiTokenInput   textinput.Model

	modal        *modal.Modal
	mouseHandler *mouse.Handler

	testing    bool
	testResult string
	testError  bool
}

// JiraSetupCompleteMsg is sent when Jira setup succeeds.
type JiraSetupCompleteMsg struct {
	URL        string
	Email      string
	APIToken   string
	ProjectKey string
}

// JiraSetupDismissMsg is sent when setup is cancelled.
type JiraSetupDismissMsg struct{}

// JiraSetupTestDoneMsg is sent when the test completes.
type JiraSetupTestDoneMsg struct {
	Err error
}

// NewJiraSetupModel creates a new Jira setup modal.
func NewJiraSetupModel() *JiraSetupModel {
	urlInput := textinput.New()
	urlInput.Placeholder = "https://myteam.atlassian.net"
	urlInput.CharLimit = 256

	emailInput := textinput.New()
	emailInput.Placeholder = "you@company.com"
	emailInput.CharLimit = 256

	projectKeyInput := textinput.New()
	projectKeyInput.Placeholder = "PROJ"
	projectKeyInput.CharLimit = 32

	apiTokenInput := textinput.New()
	apiTokenInput.Placeholder = "your-api-token"
	apiTokenInput.EchoMode = textinput.EchoPassword
	apiTokenInput.CharLimit = 256

	m := &JiraSetupModel{
		urlInput:        urlInput,
		emailInput:      emailInput,
		projectKeyInput: projectKeyInput,
		apiTokenInput:   apiTokenInput,
		mouseHandler:    mouse.NewHandler(),
	}
	m.buildModal()
	return m
}

func (m *JiraSetupModel) buildModal() {
	m.modal = modal.New("Jira Setup", modal.WithWidth(55)).
		AddSection(modal.Text("Configure Jira Cloud integration.")).
		AddSection(modal.Spacer()).
		AddSection(modal.InputWithLabel("url", "Jira URL", &m.urlInput, modal.WithSubmitOnEnter(false))).
		AddSection(modal.InputWithLabel("email", "Email", &m.emailInput, modal.WithSubmitOnEnter(false))).
		AddSection(modal.InputWithLabel("project", "Project Key", &m.projectKeyInput, modal.WithSubmitOnEnter(false))).
		AddSection(modal.InputWithLabel("token", "API Token", &m.apiTokenInput, modal.WithSubmitOnEnter(false))).
		AddSection(modal.Spacer()).
		AddSection(modal.When(
			func() bool { return m.testResult != "" },
			modal.Text(m.testResult),
		)).
		AddSection(modal.Buttons(
			modal.Btn(" Test & Save ", "test"),
			modal.Btn(" Cancel ", "cancel"),
		))
	m.modal.Reset()
}

// Update handles setup modal messages.
func (m *JiraSetupModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return nil

	case tea.KeyMsg:
		if m.testing {
			return nil
		}
		action, cmd := m.modal.HandleKey(msg)
		if action != "" {
			return m.handleAction(action)
		}
		return cmd

	case tea.MouseMsg:
		if m.testing {
			return nil
		}
		action := m.modal.HandleMouse(msg, m.mouseHandler)
		if action != "" {
			return m.handleAction(action)
		}
		return nil

	case JiraSetupTestDoneMsg:
		m.testing = false
		if msg.Err != nil {
			m.testResult = fmt.Sprintf("Connection failed: %v", msg.Err)
			m.testError = true
			m.buildModal() // Rebuild to show error
			return nil
		}
		// Success — save config and dismiss
		m.testResult = ""
		return m.saveConfig()
	}
	return nil
}

func (m *JiraSetupModel) handleAction(action string) tea.Cmd {
	switch action {
	case "test":
		m.testing = true
		m.testResult = "Testing connection..."
		m.testError = false
		m.buildModal() // Rebuild to show testing message
		return m.doTest()
	case "cancel":
		return func() tea.Msg { return JiraSetupDismissMsg{} }
	}
	return nil
}

func (m *JiraSetupModel) doTest() tea.Cmd {
	url := m.urlInput.Value()
	email := m.emailInput.Value()
	token := m.apiTokenInput.Value()
	projectKey := m.projectKeyInput.Value()

	return func() tea.Msg {
		provider := integration.NewJiraProvider(integration.JiraProviderOptions{
			URL:        url,
			ProjectKey: projectKey,
			Email:      email,
			APIToken:   token,
		})
		_, err := provider.Available("")
		return JiraSetupTestDoneMsg{Err: err}
	}
}

func (m *JiraSetupModel) saveConfig() tea.Cmd {
	url := m.urlInput.Value()
	email := m.emailInput.Value()
	token := m.apiTokenInput.Value()
	projectKey := m.projectKeyInput.Value()

	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return app.ToastMsg{
				Message:  fmt.Sprintf("Load config failed: %v", err),
				Duration: 3 * time.Second,
				IsError:  true,
			}
		}

		cfg.Integrations.Jira = config.JiraIntegrationConfig{
			Enabled:    true,
			URL:        url,
			Email:      email,
			APIToken:   token,
			ProjectKey: projectKey,
		}

		if err := config.Save(cfg); err != nil {
			return app.ToastMsg{
				Message:  fmt.Sprintf("Save config failed: %v", err),
				Duration: 3 * time.Second,
				IsError:  true,
			}
		}

		return JiraSetupCompleteMsg{
			URL:        url,
			Email:      email,
			APIToken:   token,
			ProjectKey: projectKey,
		}
	}
}

// View renders the setup modal.
func (m *JiraSetupModel) View(background string, width, height int) string {
	m.width = width
	m.height = height

	if m.testing {
		testModal := modal.New("Jira Setup", modal.WithWidth(55)).
			AddSection(modal.Text("Testing connection..."))
		testModal.Reset()
		modalContent := testModal.Render(width, height, m.mouseHandler)
		return ui.OverlayModal(background, modalContent, width, height)
	}

	modalContent := m.modal.Render(width, height, m.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}
