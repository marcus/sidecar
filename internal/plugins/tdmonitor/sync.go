package tdmonitor

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/td/pkg/monitor"
	"github.com/marcus/td/pkg/monitor/modal"
	"github.com/marcus/td/pkg/monitor/mouse"

	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/integration"
)

// SyncModel handles the GitHub sync modal.
type SyncModel struct {
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

// NewSyncModel creates a new sync modal.
func NewSyncModel(workDir string) *SyncModel {
	m := &SyncModel{
		workDir:      workDir,
		todosDir:     filepath.Join(workDir, ".todos"),
		mouseHandler: mouse.NewHandler(),
	}
	m.buildModal()
	return m
}

func (m *SyncModel) buildModal() {
	m.modal = modal.New("GitHub Sync", modal.WithWidth(45)).
		AddSection(modal.Text("Sync issues between td and GitHub.")).
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
				message := fmt.Sprintf("Pulled %d issue(s) from GitHub", msg.Result.Pulled)
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
				message := fmt.Sprintf("Pushed %d issue(s) to GitHub", msg.Result.Pushed)
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
	workDir := m.workDir
	todosDir := m.todosDir
	return func() tea.Msg {
		provider := integration.NewGitHubProvider()

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
	workDir := m.workDir
	todosDir := m.todosDir
	return func() tea.Msg {
		provider := integration.NewGitHubProvider()

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
		syncModal := modal.New("GitHub Sync", modal.WithWidth(45)).
			AddSection(modal.Text("Syncing..."))
		syncModal.Reset()
		modalContent = syncModal.Render(width, height, m.mouseHandler)
	} else {
		modalContent = m.modal.Render(width, height, m.mouseHandler)
	}

	return monitor.OverlayModal(background, modalContent, width, height)
}

// SyncPushOneDoneMsg is sent when a single-issue push completes.
type SyncPushOneDoneMsg struct {
	IssueID string
	Result  *integration.SyncResult
	Err     error
}

// doPushOne pushes a single td issue to GitHub.
func (m *SyncModel) doPushOne(issueID string) tea.Cmd {
	workDir := m.workDir
	todosDir := m.todosDir
	return func() tea.Msg {
		provider := integration.NewGitHubProvider()

		if ok, err := provider.Available(workDir); !ok {
			return SyncPushOneDoneMsg{IssueID: issueID, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := integration.PushOne(ctx, provider, workDir, todosDir, issueID)
		return SyncPushOneDoneMsg{IssueID: issueID, Result: result, Err: err}
	}
}

// SyncDismissMsg is sent when the sync modal should be dismissed.
type SyncDismissMsg struct{}
