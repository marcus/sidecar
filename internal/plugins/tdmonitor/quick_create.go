package tdmonitor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/plugins/workspace"
	"github.com/marcus/sidecar/internal/styles"
)

const (
	qcNameID       = "qc-name"
	qcBaseID       = "qc-base"
	qcPromptListID = "qc-prompt-list"
	qcAgentListID  = "qc-agent-list"
	qcSkipPermsID  = "qc-skip-perms"
	qcCreateID     = "qc-create"
	qcCancelID     = "qc-cancel"
)

// QuickCreateModel is a modal for creating workspaces directly from the TD tab.
type QuickCreateModel struct {
	taskID    string
	taskTitle string

	// Name
	nameInput     textinput.Model
	nameValid     bool
	nameErrors    []string
	nameSanitized string

	// Base branch
	baseBranchInput textinput.Model
	branchAll       []string
	branchFiltered  []string
	branchIdx       int

	// Prompt
	prompts       []workspace.Prompt
	promptListIdx int // 0 = none, 1+ = prompt index + 1

	// Agent
	agentIdx  int
	skipPerms bool

	// Error
	createError string

	// Modal
	modal        *modal.Modal
	modalWidth   int
	mouseHandler *mouse.Handler
}

// NewQuickCreateModel creates a new quick-create modal for the given task.
func NewQuickCreateModel(taskID, taskTitle, workDir string) *QuickCreateModel {
	derivedName := deriveBranchName(taskID, taskTitle)

	nameInput := textinput.New()
	nameInput.Placeholder = "feature-name"
	nameInput.Prompt = ""
	nameInput.CharLimit = 100
	nameInput.SetValue(derivedName)

	baseBranchInput := textinput.New()
	baseBranchInput.Placeholder = "HEAD"
	baseBranchInput.Prompt = ""
	baseBranchInput.CharLimit = 100

	nameValid, nameErrors, nameSanitized := workspace.ValidateBranchName(derivedName)

	branches := loadLocalBranches(workDir)

	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".config", "sidecar")
	prompts := workspace.LoadPrompts(configDir, workDir)

	// Auto-select first prompt with ticket mode
	promptListIdx := 0
	for i, p := range prompts {
		if p.TicketMode != workspace.TicketNone {
			promptListIdx = i + 1
			break
		}
	}

	return &QuickCreateModel{
		taskID:          taskID,
		taskTitle:       taskTitle,
		nameInput:       nameInput,
		nameValid:       nameValid,
		nameErrors:      nameErrors,
		nameSanitized:   nameSanitized,
		baseBranchInput: baseBranchInput,
		branchAll:       branches,
		branchFiltered:  branches,
		prompts:         prompts,
		promptListIdx:   promptListIdx,
		mouseHandler:    mouse.NewHandler(),
	}
}

// deriveBranchName derives a branch name from a task ID and title.
func deriveBranchName(taskID, title string) string {
	sanitized := workspace.SanitizeBranchName(title)
	runes := []rune(sanitized)
	if len(runes) > 40 {
		sanitized = strings.TrimSuffix(string(runes[:40]), "-")
	}
	if sanitized == "" {
		return taskID
	}
	return taskID + "-" + sanitized
}

// loadLocalBranches loads branch names from git (synchronous, local operation).
func loadLocalBranches(workDir string) []string {
	cmd := exec.Command("git", "branch", "--format=%(refname:short)")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	var branches []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches
}

func (m *QuickCreateModel) ensureModal(width int) {
	modalW := 60
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

	// Task info line
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

	// Prompt list: (none) + loaded prompts
	promptItems := make([]modal.ListItem, 0, len(m.prompts)+1)
	promptItems = append(promptItems, modal.ListItem{ID: "qc-prompt-none", Label: "(none)"})
	for i, p := range m.prompts {
		source := "[G]"
		if p.Source == "project" {
			source = "[P]"
		}
		promptItems = append(promptItems, modal.ListItem{
			ID:    fmt.Sprintf("qc-prompt-%d", i),
			Label: fmt.Sprintf("%s  %s", p.Name, styles.Muted.Render(source)),
		})
	}

	// Agent list
	agentItems := make([]modal.ListItem, len(workspace.AgentTypeOrder))
	for i, at := range workspace.AgentTypeOrder {
		agentItems[i] = modal.ListItem{
			ID:    fmt.Sprintf("qc-agent-%d", i),
			Label: workspace.AgentDisplayNames[at],
		}
	}

	m.modal = modal.New("Quick Create Workspace",
		modal.WithWidth(modalW),
		modal.WithHints(false),
	).
		AddSection(modal.Text(styles.Muted.Render(infoLine))).
		AddSection(modal.Spacer()).
		AddSection(m.nameLabelSection()).
		AddSection(modal.Input(qcNameID, &m.nameInput, modal.WithSubmitOnEnter(false))).
		AddSection(m.nameErrorsSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.Text("Base Branch (default: HEAD):")).
		AddSection(modal.Input(qcBaseID, &m.baseBranchInput, modal.WithSubmitOnEnter(false))).
		AddSection(m.branchDropdownSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.Text("Prompt:")).
		AddSection(modal.List(qcPromptListID, promptItems, &m.promptListIdx, modal.WithMaxVisible(min(len(promptItems), 4)))).
		AddSection(m.promptPreviewSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.Text("Agent:")).
		AddSection(modal.List(qcAgentListID, agentItems, &m.agentIdx, modal.WithMaxVisible(len(agentItems)))).
		AddSection(modal.Spacer()).
		AddSection(modal.When(m.shouldShowSkipPerms, modal.Checkbox(qcSkipPermsID, "Auto-approve all actions", &m.skipPerms))).
		AddSection(m.skipPermsHintSection()).
		AddSection(modal.Spacer()).
		AddSection(m.errorSection()).
		AddSection(modal.Buttons(
			modal.Btn(" Create ", qcCreateID),
			modal.Btn(" Cancel ", qcCancelID),
		))
}

func (m *QuickCreateModel) nameLabelSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		label := "Name:"
		nameValue := m.nameInput.Value()
		if nameValue != "" {
			if m.nameValid {
				label = "Name: " + lipgloss.NewStyle().Foreground(styles.Success).Render("✓")
			} else {
				label = "Name: " + lipgloss.NewStyle().Foreground(styles.Error).Render("✗")
			}
		}
		return modal.RenderedSection{Content: label}
	}, nil)
}

func (m *QuickCreateModel) nameErrorsSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		nameValue := m.nameInput.Value()
		if nameValue == "" || m.nameValid {
			return modal.RenderedSection{}
		}
		var lines []string
		if len(m.nameErrors) > 0 {
			errorStyle := lipgloss.NewStyle().Foreground(styles.Error)
			lines = append(lines, errorStyle.Render("  ⚠ "+strings.Join(m.nameErrors, ", ")))
		}
		if m.nameSanitized != "" && m.nameSanitized != nameValue {
			lines = append(lines, styles.Muted.Render(fmt.Sprintf("  Suggestion: %s", m.nameSanitized)))
		}
		return modal.RenderedSection{Content: strings.Join(lines, "\n")}
	}, nil)
}

func (m *QuickCreateModel) branchDropdownSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if focusID != qcBaseID {
			return modal.RenderedSection{}
		}
		if len(m.branchFiltered) == 0 {
			if len(m.branchAll) == 0 {
				return modal.RenderedSection{Content: styles.Muted.Render("  No branches found")}
			}
			return modal.RenderedSection{}
		}

		maxDropdown := 5
		count := min(len(m.branchFiltered), maxDropdown)

		lines := make([]string, 0, count+1)
		for i := 0; i < count; i++ {
			branch := m.branchFiltered[i]
			maxWidth := contentWidth - 4
			if maxWidth < 8 {
				maxWidth = 8
			}
			if len(branch) > maxWidth {
				branch = branch[:maxWidth-3] + "..."
			}
			prefix := "  "
			if i == m.branchIdx {
				prefix = "> "
			}
			line := prefix + branch
			if i == m.branchIdx {
				line = lipgloss.NewStyle().Foreground(styles.Primary).Render(line)
			} else {
				line = styles.Muted.Render(line)
			}
			lines = append(lines, line)
		}
		if len(m.branchFiltered) > maxDropdown {
			lines = append(lines, styles.Muted.Render(fmt.Sprintf("  ... and %d more", len(m.branchFiltered)-maxDropdown)))
		}

		return modal.RenderedSection{Content: strings.Join(lines, "\n")}
	}, nil)
}

func (m *QuickCreateModel) promptPreviewSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		prompt := m.SelectedPrompt()
		if prompt == nil {
			return modal.RenderedSection{}
		}
		preview := strings.ReplaceAll(prompt.Body, "\n", " ")
		runes := []rune(preview)
		if len(runes) > 50 {
			preview = string(runes[:47]) + "..."
		}
		return modal.RenderedSection{Content: styles.Muted.Render(fmt.Sprintf("  Preview: %s", preview))}
	}, nil)
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

func (m *QuickCreateModel) errorSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if m.createError == "" {
			return modal.RenderedSection{}
		}
		errStyle := lipgloss.NewStyle().Foreground(styles.Error)
		return modal.RenderedSection{Content: errStyle.Render("Error: " + m.createError)}
	}, nil)
}

// SelectedAgentType returns the currently selected agent type.
func (m *QuickCreateModel) SelectedAgentType() workspace.AgentType {
	if m.agentIdx < 0 || m.agentIdx >= len(workspace.AgentTypeOrder) {
		return workspace.AgentClaude
	}
	return workspace.AgentTypeOrder[m.agentIdx]
}

// SelectedPrompt returns the selected prompt, or nil if "(none)" is selected.
func (m *QuickCreateModel) SelectedPrompt() *workspace.Prompt {
	if m.promptListIdx <= 0 || m.promptListIdx > len(m.prompts) {
		return nil
	}
	return &m.prompts[m.promptListIdx-1]
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
		focusID := m.modal.FocusedID()
		key := msg.String()

		// Handle branch dropdown navigation when base branch is focused
		if focusID == qcBaseID {
			switch key {
			case "up", "k":
				if m.branchIdx > 0 {
					m.branchIdx--
				}
				return "", nil
			case "down", "j":
				if m.branchIdx < len(m.branchFiltered)-1 {
					m.branchIdx++
				}
				return "", nil
			case "enter":
				if len(m.branchFiltered) > 0 && m.branchIdx < len(m.branchFiltered) {
					m.baseBranchInput.SetValue(m.branchFiltered[m.branchIdx])
					m.filterBranches()
				}
				return "", nil
			}
		}

		// Delegate to modal
		action, cmd := m.modal.HandleKey(msg)

		// Post-processing: validate name / filter branches after key input
		if focusID == qcNameID {
			m.validateName()
			m.createError = "" // Clear error on edit
		}
		if focusID == qcBaseID {
			m.filterBranches()
		}

		// Handle actions
		switch {
		case action == qcCreateID:
			if err := m.validate(); err != "" {
				m.createError = err
				return "", cmd
			}
			return "create", cmd
		case action == qcCancelID || action == "cancel":
			return "cancel", cmd
		default:
			return "", cmd
		}

	case tea.MouseMsg:
		action := m.modal.HandleMouse(msg, m.mouseHandler)
		switch {
		case action == qcCreateID:
			if err := m.validate(); err != "" {
				m.createError = err
				return "", nil
			}
			return "create", nil
		case action == qcCancelID || action == "cancel":
			return "cancel", nil
		}
		return "", nil
	}

	return "", nil
}

func (m *QuickCreateModel) validate() string {
	name := m.nameInput.Value()
	if name == "" {
		return "Name is required"
	}
	if !m.nameValid {
		return "Invalid branch name"
	}
	return ""
}

func (m *QuickCreateModel) validateName() {
	name := m.nameInput.Value()
	m.nameValid, m.nameErrors, m.nameSanitized = workspace.ValidateBranchName(name)
}

func (m *QuickCreateModel) filterBranches() {
	query := strings.ToLower(m.baseBranchInput.Value())
	if query == "" {
		m.branchFiltered = m.branchAll
	} else {
		var matches []string
		for _, branch := range m.branchAll {
			if strings.Contains(strings.ToLower(branch), query) {
				matches = append(matches, branch)
			}
		}
		m.branchFiltered = matches
	}
	m.branchIdx = 0
}

// View renders the quick-create modal.
func (m *QuickCreateModel) View(width, height int) string {
	m.ensureModal(width)
	if m.modal == nil {
		return ""
	}
	return m.modal.Render(width, height, m.mouseHandler)
}
