package conversations

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/plugins/workspace"
	"github.com/marcus/sidecar/internal/ui"
)

// Resume target type constants
const (
	resumeTypeShell    = 0
	resumeTypeWorktree = 1
)

// Modal field IDs
const (
	resumeTypeListID      = "resume-type-list"
	resumeNameFieldID     = "resume-name"
	resumeBaseFieldID     = "resume-base"
	resumeAgentListID     = "resume-agent-list"
	resumeSkipPermsID     = "resume-skip-perms"
	resumeSubmitID        = "resume-submit"
	resumeCancelID        = "resume-cancel"
	resumeTypeItemPrefix  = "resume-type-"
	resumeAgentItemPrefix = "resume-agent-"
)

// resumeTypeLabels are the options for resume type selection
var resumeTypeLabels = []string{"Shell", "New Worktree"}

// ensureResumeModal builds or caches the resume modal.
func (p *Plugin) ensureResumeModal() {
	if p.resumeSession == nil {
		return
	}

	modalW := 50
	maxW := p.width - 4
	if maxW < 20 {
		maxW = 20
	}
	if modalW > maxW {
		modalW = maxW
	}

	if p.resumeModal != nil && p.resumeModalWidth == modalW {
		return
	}
	p.resumeModalWidth = modalW

	// Build type selection list
	typeItems := make([]modal.ListItem, len(resumeTypeLabels))
	for i, label := range resumeTypeLabels {
		typeItems[i] = modal.ListItem{
			ID:    fmt.Sprintf("%s%d", resumeTypeItemPrefix, i),
			Label: label,
		}
	}

	// Build agent selection list
	agentItems := make([]modal.ListItem, len(workspace.AgentTypeOrder))
	for i, at := range workspace.AgentTypeOrder {
		agentItems[i] = modal.ListItem{
			ID:    fmt.Sprintf("%s%d", resumeAgentItemPrefix, i),
			Label: workspace.AgentDisplayNames[at],
		}
	}

	p.resumeModal = modal.New("Resume in Workspace",
		modal.WithWidth(modalW),
		modal.WithPrimaryAction(resumeSubmitID),
		modal.WithHints(false),
	).
		AddSection(p.resumeSessionInfoSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.Text("Resume in:")).
		AddSection(modal.List(resumeTypeListID, typeItems, &p.resumeType, modal.WithMaxVisible(2))).
		AddSection(modal.Spacer()).
		// Worktree-specific fields (shown when type == worktree)
		AddSection(modal.When(p.isResumeWorktreeMode, modal.Text("Branch name:"))).
		AddSection(modal.When(p.isResumeWorktreeMode, modal.Input(resumeNameFieldID, &p.resumeNameInput, modal.WithSubmitOnEnter(false)))).
		AddSection(modal.When(p.isResumeWorktreeMode, modal.Spacer())).
		AddSection(modal.When(p.isResumeWorktreeMode, modal.Text("Base branch:"))).
		AddSection(modal.When(p.isResumeWorktreeMode, modal.Input(resumeBaseFieldID, &p.resumeBaseBranchInput, modal.WithSubmitOnEnter(false)))).
		AddSection(modal.When(p.isResumeWorktreeMode, modal.Spacer())).
		AddSection(modal.When(p.isResumeWorktreeMode, modal.Text("Agent:"))).
		AddSection(modal.When(p.isResumeWorktreeMode, modal.List(resumeAgentListID, agentItems, &p.resumeAgentIdx, modal.WithMaxVisible(len(agentItems))))).
		AddSection(modal.When(p.shouldShowResumeSkipPerms, modal.Spacer())).
		AddSection(modal.When(p.shouldShowResumeSkipPerms, modal.Checkbox(resumeSkipPermsID, "Auto-approve all actions", &p.resumeSkipPermissions))).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Resume ", resumeSubmitID),
			modal.Btn(" Cancel ", resumeCancelID),
		))
}

// resumeSessionInfoSection renders session info at the top of the modal.
func (p *Plugin) resumeSessionInfoSection() modal.Section {
	return modal.Custom(
		func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
			if p.resumeSession == nil {
				return modal.RenderedSection{Content: ""}
			}
			name := p.resumeSession.Name
			if name == "" {
				name = p.resumeSession.Slug
			}
			if name == "" {
				name = p.resumeSession.ID[:min(12, len(p.resumeSession.ID))]
			}
			adapterName := p.resumeSession.AdapterName
			if adapterName == "" {
				adapterName = p.resumeSession.AdapterID
			}
			content := fmt.Sprintf("Session: %s\nAgent: %s", name, adapterName)
			return modal.RenderedSection{Content: content}
		},
		func(msg tea.Msg, focusID string) (string, tea.Cmd) {
			return "", nil
		},
	)
}

// isResumeWorktreeMode returns true when worktree type is selected.
func (p *Plugin) isResumeWorktreeMode() bool {
	return p.resumeType == resumeTypeWorktree
}

// shouldShowResumeSkipPerms returns true when skip permissions checkbox should show.
func (p *Plugin) shouldShowResumeSkipPerms() bool {
	if !p.isResumeWorktreeMode() {
		return false
	}
	if p.resumeAgentIdx < 0 || p.resumeAgentIdx >= len(workspace.AgentTypeOrder) {
		return false
	}
	agentType := workspace.AgentTypeOrder[p.resumeAgentIdx]
	if agentType == workspace.AgentNone {
		return false
	}
	// Only show if agent has a skip permissions flag
	_, hasFlag := workspace.SkipPermissionsFlags[agentType]
	return hasFlag && workspace.SkipPermissionsFlags[agentType] != ""
}

// syncResumeModalFocus syncs modal focus with current state.
func (p *Plugin) syncResumeModalFocus() {
	if p.resumeModal == nil {
		return
	}
	// Focus based on current field
	var focusID string
	switch p.resumeFocus {
	case 0:
		focusID = fmt.Sprintf("%s%d", resumeTypeItemPrefix, p.resumeType)
	case 1:
		focusID = resumeNameFieldID
	case 2:
		focusID = resumeBaseFieldID
	case 3:
		focusID = fmt.Sprintf("%s%d", resumeAgentItemPrefix, p.resumeAgentIdx)
	case 4:
		focusID = resumeSkipPermsID
	case 5:
		focusID = resumeSubmitID
	case 6:
		focusID = resumeCancelID
	}
	if focusID != "" {
		p.resumeModal.SetFocus(focusID)
	}
}

// handleResumeModalKeys handles keyboard input for the resume modal.
func (p *Plugin) handleResumeModalKeys(msg tea.KeyMsg) tea.Cmd {
	p.ensureResumeModal()
	if p.resumeModal == nil {
		return nil
	}

	action, cmd := p.resumeModal.HandleKey(msg)
	switch action {
	case resumeSubmitID:
		return p.executeResume()
	case resumeCancelID, "cancel":
		p.resetResumeModal()
		return nil
	}

	// Handle type item selection
	if strings.HasPrefix(action, resumeTypeItemPrefix) {
		var idx int
		fmt.Sscanf(action, resumeTypeItemPrefix+"%d", &idx)
		if idx >= 0 && idx < len(resumeTypeLabels) {
			p.resumeType = idx
		}
	}

	// Handle agent item selection
	if strings.HasPrefix(action, resumeAgentItemPrefix) {
		var idx int
		fmt.Sscanf(action, resumeAgentItemPrefix+"%d", &idx)
		if idx >= 0 && idx < len(workspace.AgentTypeOrder) {
			p.resumeAgentIdx = idx
		}
	}

	return cmd
}

// handleResumeModalMouse handles mouse input for the resume modal.
func (p *Plugin) handleResumeModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureResumeModal()
	if p.resumeModal == nil {
		return nil
	}

	action := p.resumeModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case resumeSubmitID:
		return p.executeResume()
	case resumeCancelID, "cancel":
		p.resetResumeModal()
		return nil
	}

	// Handle type item selection
	if strings.HasPrefix(action, resumeTypeItemPrefix) {
		var idx int
		fmt.Sscanf(action, resumeTypeItemPrefix+"%d", &idx)
		if idx >= 0 && idx < len(resumeTypeLabels) {
			p.resumeType = idx
		}
	}

	// Handle agent item selection
	if strings.HasPrefix(action, resumeAgentItemPrefix) {
		var idx int
		fmt.Sscanf(action, resumeAgentItemPrefix+"%d", &idx)
		if idx >= 0 && idx < len(workspace.AgentTypeOrder) {
			p.resumeAgentIdx = idx
		}
	}

	return nil
}

// renderResumeModal renders the resume modal over the background.
func (p *Plugin) renderResumeModal(width, height int) string {
	p.ensureResumeModal()
	if p.resumeModal == nil {
		return ""
	}

	// Render background content (uses stored dimensions from View)
	background := p.renderTwoPane()

	// Render modal
	rendered := p.resumeModal.Render(width, height, p.mouseHandler)

	return ui.OverlayModal(background, rendered, width, height)
}

// openResumeModal opens the resume modal for the selected session.
func (p *Plugin) openResumeModal() tea.Cmd {
	session := p.getSessionForResume()
	if session == nil {
		return func() tea.Msg {
			return app.ToastMsg{Message: "No session selected", IsError: true}
		}
	}

	// Check if adapter supports resume
	cmd := resumeCommand(session)
	if cmd == "" {
		return func() tea.Msg {
			return app.ToastMsg{Message: "Resume not supported for " + session.AdapterName, IsError: true}
		}
	}

	// Initialize modal state
	p.resumeSession = session
	p.resumeType = resumeTypeShell // Default to shell
	p.resumeFocus = 0

	// Initialize name input with sanitized session name
	p.resumeNameInput = textinput.New()
	p.resumeNameInput.Placeholder = "branch-name"
	p.resumeNameInput.SetValue("resume-" + sanitizeBranchName(session.Name))
	p.resumeNameInput.CharLimit = 50

	// Initialize base branch input
	p.resumeBaseBranchInput = textinput.New()
	p.resumeBaseBranchInput.Placeholder = "HEAD"
	p.resumeBaseBranchInput.SetValue("HEAD")
	p.resumeBaseBranchInput.CharLimit = 100

	// Set default agent based on adapter
	p.resumeAgentIdx = defaultAgentIdxForAdapter(session.AdapterID)
	p.resumeSkipPermissions = false

	// Clear cached modal to rebuild with new session
	p.resumeModal = nil
	p.resumeModalWidth = 0
	p.showResumeModal = true

	return nil
}

// resetResumeModal closes and resets the resume modal state.
func (p *Plugin) resetResumeModal() {
	p.showResumeModal = false
	p.resumeModal = nil
	p.resumeSession = nil
	p.resumeType = resumeTypeShell
	p.resumeFocus = 0
	p.resumeAgentIdx = 0
	p.resumeSkipPermissions = false
}

// executeResume sends the resume message to workspace plugin.
func (p *Plugin) executeResume() tea.Cmd {
	session := p.resumeSession
	if session == nil {
		return nil
	}

	// Generate resume command
	resumeCmd := resumeCommand(session)
	if resumeCmd == "" {
		return func() tea.Msg {
			return app.ToastMsg{Message: "Resume not supported for " + session.AdapterName, IsError: true}
		}
	}

	// Build message based on type
	msg := workspace.ResumeConversationMsg{
		SessionID: session.ID,
		AdapterID: session.AdapterID,
		ResumeCmd: resumeCmd,
	}

	if p.resumeType == resumeTypeShell {
		msg.Type = "shell"
	} else {
		msg.Type = "worktree"
		msg.WorktreeName = p.resumeNameInput.Value()
		msg.BaseBranch = p.resumeBaseBranchInput.Value()
		if msg.BaseBranch == "" {
			msg.BaseBranch = "HEAD"
		}
		if p.resumeAgentIdx >= 0 && p.resumeAgentIdx < len(workspace.AgentTypeOrder) {
			msg.AgentType = workspace.AgentTypeOrder[p.resumeAgentIdx]
		}
		msg.SkipPerms = p.resumeSkipPermissions
	}

	// Close modal and send message
	p.resetResumeModal()
	return tea.Batch(
		app.FocusPlugin("workspace-manager"),
		func() tea.Msg { return msg },
	)
}

// getSessionForResume returns the session to resume, checking both selectedSession ID and cursor.
func (p *Plugin) getSessionForResume() *adapter.Session {
	// If in message view, find session by selectedSession ID
	if p.selectedSession != "" {
		for i := range p.sessions {
			if p.sessions[i].ID == p.selectedSession {
				return &p.sessions[i]
			}
		}
	}
	// Use cursor selection from session list
	sessions := p.visibleSessions()
	if p.cursor >= 0 && p.cursor < len(sessions) {
		return &sessions[p.cursor]
	}
	return nil
}

// defaultAgentIdxForAdapter returns the index in AgentTypeOrder for the given adapter.
func defaultAgentIdxForAdapter(adapterID string) int {
	var agentType workspace.AgentType
	switch adapterID {
	case "claude-code":
		agentType = workspace.AgentClaude
	case "codex":
		agentType = workspace.AgentCodex
	case "gemini-cli":
		agentType = workspace.AgentGemini
	case "cursor-cli":
		agentType = workspace.AgentCursor
	case "opencode":
		agentType = workspace.AgentOpenCode
	default:
		return 0 // Default to first (Claude)
	}

	// Find index in AgentTypeOrder
	for i, at := range workspace.AgentTypeOrder {
		if at == agentType {
			return i
		}
	}
	return 0
}

// sanitizeBranchName converts a string to a valid git branch name.
var branchNameInvalidChars = regexp.MustCompile(`[^a-zA-Z0-9-]`)

func sanitizeBranchName(name string) string {
	if name == "" {
		return "session"
	}
	// Replace spaces and invalid chars with hyphens
	name = strings.ReplaceAll(name, " ", "-")
	name = branchNameInvalidChars.ReplaceAllString(name, "")
	// Remove leading/trailing hyphens
	name = strings.Trim(name, "-")
	// Collapse multiple hyphens
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	// Truncate if too long
	if len(name) > 40 {
		name = name[:40]
	}
	if name == "" {
		return "session"
	}
	return strings.ToLower(name)
}
