package tdmonitor

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugins/workspace"
	"github.com/marcus/sidecar/internal/styles"
)

const (
	qcAgentListID = "qc-agent-list"
	qcSkipPermsID = "qc-skip-perms"
	qcCreateID    = "qc-create"
	qcCancelID    = "qc-cancel"
)

// QuickCreateModel is a lightweight modal for creating workspaces directly from the TD tab.
type QuickCreateModel struct {
	taskID    string
	taskTitle string

	agentIdx  int
	skipPerms bool

	modal        *modal.Modal
	modalWidth   int
	mouseHandler *mouse.Handler
}

// NewQuickCreateModel creates a new quick-create modal for the given task.
func NewQuickCreateModel(taskID, taskTitle string) *QuickCreateModel {
	m := &QuickCreateModel{
		taskID:       taskID,
		taskTitle:    taskTitle,
		mouseHandler: mouse.NewHandler(),
	}
	return m
}

func (m *QuickCreateModel) ensureModal(width int) {
	modalW := 50
	maxW := width - 4
	if maxW < 1 {
		maxW = 1
	}
	if modalW > maxW {
		modalW = maxW
	}

	if m.modal != nil && m.modalWidth == modalW {
		return
	}
	m.modalWidth = modalW

	items := make([]modal.ListItem, len(workspace.AgentTypeOrder))
	for i, at := range workspace.AgentTypeOrder {
		items[i] = modal.ListItem{
			ID:    fmt.Sprintf("qc-agent-%d", i),
			Label: workspace.AgentDisplayNames[at],
		}
	}

	// Build info line
	infoLine := m.taskID
	if m.taskTitle != "" {
		title := m.taskTitle
		runes := []rune(title)
		maxTitle := modalW - len(m.taskID) - 6
		if maxTitle > 10 && len(runes) > maxTitle {
			title = string(runes[:maxTitle-3]) + "..."
		}
		if maxTitle > 10 {
			infoLine = fmt.Sprintf("%s: %s", m.taskID, title)
		}
	}

	m.modal = modal.New("Quick Create Workspace",
		modal.WithWidth(modalW),
		modal.WithPrimaryAction(qcCreateID),
		modal.WithHints(false),
	).
		AddSection(modal.Text(styles.Muted.Render(infoLine))).
		AddSection(modal.Spacer()).
		AddSection(modal.Text("Agent:")).
		AddSection(modal.List(qcAgentListID, items, &m.agentIdx, modal.WithMaxVisible(len(items)))).
		AddSection(modal.Spacer()).
		AddSection(modal.When(m.shouldShowSkipPerms, modal.Checkbox(qcSkipPermsID, "Auto-approve all actions", &m.skipPerms))).
		AddSection(m.skipPermsHintSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Create ", qcCreateID),
			modal.Btn(" Cancel ", qcCancelID),
		))
}

func (m *QuickCreateModel) shouldShowSkipPerms() bool {
	if m.agentIdx < 0 || m.agentIdx >= len(workspace.AgentTypeOrder) {
		return false
	}
	at := workspace.AgentTypeOrder[m.agentIdx]
	_, hasFlag := workspace.SkipPermissionsFlags[at]
	return hasFlag && at != workspace.AgentNone
}

func (m *QuickCreateModel) skipPermsHintSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if m.agentIdx < 0 || m.agentIdx >= len(workspace.AgentTypeOrder) {
			return modal.RenderedSection{}
		}
		at := workspace.AgentTypeOrder[m.agentIdx]
		if at == workspace.AgentNone {
			return modal.RenderedSection{}
		}
		flag := workspace.SkipPermissionsFlags[at]
		if flag == "" {
			return modal.RenderedSection{Content: styles.Muted.Render("  Skip permissions not available for this agent")}
		}
		return modal.RenderedSection{Content: styles.Muted.Render(fmt.Sprintf("      (Adds %s)", flag))}
	}, nil)
}

// SelectedAgentType returns the currently selected agent type.
func (m *QuickCreateModel) SelectedAgentType() workspace.AgentType {
	if m.agentIdx < 0 || m.agentIdx >= len(workspace.AgentTypeOrder) {
		return workspace.AgentClaude
	}
	return workspace.AgentTypeOrder[m.agentIdx]
}

// Update processes input for the quick-create modal.
// Returns action: "create" when confirmed, "cancel" on dismiss, "" otherwise.
func (m *QuickCreateModel) Update(msg tea.Msg, width int) (string, tea.Cmd) {
	m.ensureModal(width)
	if m.modal == nil {
		return "", nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		action, cmd := m.modal.HandleKey(msg)
		switch {
		case action == qcCreateID:
			return "create", cmd
		case action == qcCancelID || action == "cancel":
			return "cancel", cmd
		default:
			// Agent selection updates via pointer; When()/Custom() sections
			// re-evaluate dynamically on each render, so no rebuild needed.
			return "", cmd
		}

	case tea.MouseMsg:
		action := m.modal.HandleMouse(msg, m.mouseHandler)
		switch {
		case action == qcCreateID:
			return "create", nil
		case action == qcCancelID || action == "cancel":
			return "cancel", nil
		}
		return "", nil
	}

	return "", nil
}

// View renders the quick-create modal.
func (m *QuickCreateModel) View(width, height int) string {
	m.ensureModal(width)
	if m.modal == nil {
		return ""
	}
	return m.modal.Render(width, height, m.mouseHandler)
}
