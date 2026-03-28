package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/modal"
	ui "github.com/marcus/sidecar/internal/ui"
)

const (
	agentConfigPromptFieldID     = "agent-config-prompt"
	agentConfigAgentListID       = "agent-config-agent-list"
	agentConfigSkipPermissionsID = "agent-config-skip-permissions"
	agentConfigSubmitID          = "agent-config-submit"
	agentConfigCancelID          = "agent-config-cancel"
	agentConfigAgentItemPrefix   = "agent-config-agent-"
)

// openAgentConfigModal initializes and opens the agent config modal for a worktree.
func (p *Plugin) openAgentConfigModal(wt *Worktree, isRestart bool) {
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".config", "sidecar")
	p.agentConfigWorktree = wt
	p.agentConfigIsRestart = isRestart
	p.agentConfigAgentOrder = p.filteredAgentOrder()
	p.agentConfigAgentType = p.resolveWorktreeAgentType(wt)
	p.agentConfigAgentIdx = p.agentConfigTypeIndex(p.agentConfigAgentType)
	p.agentConfigSkipPerms = false
	p.agentConfigPromptIdx = -1
	p.agentConfigPrompts = LoadPrompts(configDir, p.ctx.ProjectRoot)
	p.agentConfigModal = nil
	p.agentConfigModalWidth = 0
	p.viewMode = ViewModeAgentConfig
}

// agentConfigTypeIndex returns the index of the agent type in agentConfigAgentOrder.
func (p *Plugin) agentConfigTypeIndex(agentType AgentType) int {
	agentOrder := p.agentConfigAgentOrder
	if len(agentOrder) == 0 {
		agentOrder = AgentTypeOrder
	}
	for i, at := range agentOrder {
		if at == agentType {
			return i
		}
	}
	return 0
}

// clearAgentConfigModal resets all agent config modal state.
func (p *Plugin) clearAgentConfigModal() {
	p.agentConfigWorktree = nil
	p.agentConfigIsRestart = false
	p.agentConfigAgentType = ""
	p.agentConfigAgentIdx = 0
	p.agentConfigAgentOrder = nil
	p.agentConfigSkipPerms = false
	p.agentConfigPromptIdx = -1
	p.agentConfigPrompts = nil
	p.agentConfigModal = nil
	p.agentConfigModalWidth = 0
}

// openPromptPicker opens the prompt picker overlay, routing return to the given mode.
func (p *Plugin) openPromptPicker(prompts []Prompt, returnMode ViewMode) {
	p.promptPickerReturnMode = returnMode
	p.promptPicker = NewPromptPicker(prompts, p.width, p.height)
	p.clearPromptPickerModal()
	p.viewMode = ViewModePromptPicker
}

// getAgentConfigPrompt resolves the selected prompt index to a *Prompt.
func (p *Plugin) getAgentConfigPrompt() *Prompt {
	if p.agentConfigPromptIdx < 0 || p.agentConfigPromptIdx >= len(p.agentConfigPrompts) {
		return nil
	}
	prompt := p.agentConfigPrompts[p.agentConfigPromptIdx]
	return &prompt
}

// shouldShowAgentConfigSkipPerms returns true if the selected agent supports skip permissions.
func (p *Plugin) shouldShowAgentConfigSkipPerms() bool {
	if p.agentConfigAgentType == AgentNone || p.agentConfigAgentType == "" {
		return false
	}
	flag, ok := SkipPermissionsFlags[p.agentConfigAgentType]
	return ok && flag != ""
}

// ensureAgentConfigModal builds or rebuilds the agent config modal.
func (p *Plugin) ensureAgentConfigModal() {
	if p.agentConfigWorktree == nil {
		return
	}

	modalW := 60
	maxW := p.width - 4
	if maxW < 1 {
		maxW = 1
	}
	if modalW > maxW {
		modalW = maxW
	}

	if p.agentConfigModal != nil && p.agentConfigModalWidth == modalW {
		return
	}
	p.agentConfigModalWidth = modalW

	agentOrder := p.agentConfigAgentOrder
	if len(agentOrder) == 0 {
		agentOrder = AgentTypeOrder
	}
	items := make([]modal.ListItem, len(agentOrder))
	for i, at := range agentOrder {
		items[i] = modal.ListItem{
			ID:    fmt.Sprintf("%s%d", agentConfigAgentItemPrefix, i),
			Label: AgentDisplayNames[at],
		}
	}

	title := fmt.Sprintf("Start Agent: %s", p.agentConfigWorktree.Name)
	if p.agentConfigIsRestart {
		title = fmt.Sprintf("Restart Agent: %s", p.agentConfigWorktree.Name)
	}

	p.agentConfigModal = modal.New(title,
		modal.WithWidth(modalW),
		modal.WithPrimaryAction(agentConfigSubmitID),
		modal.WithHints(false),
	).
		AddSection(p.agentConfigPromptSection()).
		AddSection(modal.Spacer()).
		AddSection(p.agentConfigAgentLabelSection()).
		AddSection(modal.List(agentConfigAgentListID, items, &p.agentConfigAgentIdx, modal.WithMaxVisible(len(items)))).
		AddSection(p.agentConfigSkipPermissionsSpacerSection()).
		AddSection(modal.When(p.shouldShowAgentConfigSkipPerms, modal.Checkbox(agentConfigSkipPermissionsID, "Auto-approve all actions", &p.agentConfigSkipPerms))).
		AddSection(p.agentConfigSkipPermissionsHintSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Start ", agentConfigSubmitID),
			modal.Btn(" Cancel ", agentConfigCancelID),
		))

	// Set initial focus when modal is first built
	p.agentConfigModal.SetFocus(agentConfigAgentListID)
}

// agentConfigPromptSection renders the prompt selector field.
func (p *Plugin) agentConfigPromptSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		lines := make([]string, 0, 4)
		focusables := make([]modal.FocusableInfo, 0, 1)

		lines = append(lines, "Prompt:")

		selectedPrompt := p.getAgentConfigPrompt()
		displayText := "(none)"
		if len(p.agentConfigPrompts) == 0 {
			displayText = "No prompts configured"
		} else if selectedPrompt != nil {
			scopeIndicator := "[G] global"
			if selectedPrompt.Source == "project" {
				scopeIndicator = "[P] project"
			}
			displayText = fmt.Sprintf("%s  %s", selectedPrompt.Name, dimText(scopeIndicator))
		}

		promptStyle := inputStyle()
		if focusID == agentConfigPromptFieldID {
			promptStyle = inputFocusedStyle()
		}
		rendered := promptStyle.Render(displayText)
		renderedLines := strings.Split(rendered, "\n")
		displayStartY := len(lines)
		lines = append(lines, renderedLines...)

		focusables = append(focusables, modal.FocusableInfo{
			ID:      agentConfigPromptFieldID,
			OffsetX: 0,
			OffsetY: displayStartY,
			Width:   ansi.StringWidth(rendered),
			Height:  len(renderedLines),
		})

		if len(p.agentConfigPrompts) == 0 {
			lines = append(lines, dimText("  See: .claude/skills/create-prompt/SKILL.md"))
		} else if selectedPrompt == nil {
			lines = append(lines, dimText("  Press Enter to select a prompt template"))
		} else {
			preview := strings.ReplaceAll(selectedPrompt.Body, "\n", " ")
			if runes := []rune(preview); len(runes) > 60 {
				preview = string(runes[:57]) + "..."
			}
			lines = append(lines, dimText(fmt.Sprintf("  Preview: %s", preview)))
		}

		return modal.RenderedSection{Content: strings.Join(lines, "\n"), Focusables: focusables}
	}, nil)
}

// agentConfigAgentLabelSection renders the "Agent:" label.
func (p *Plugin) agentConfigAgentLabelSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		return modal.RenderedSection{Content: "Agent:"}
	}, nil)
}

// agentConfigSkipPermissionsSpacerSection renders a spacer before the checkbox (hidden when agent is None).
func (p *Plugin) agentConfigSkipPermissionsSpacerSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.agentConfigAgentType == AgentNone || p.agentConfigAgentType == "" {
			return modal.RenderedSection{}
		}
		return modal.RenderedSection{Content: " "}
	}, nil)
}

// agentConfigSkipPermissionsHintSection renders the hint showing the actual flag.
func (p *Plugin) agentConfigSkipPermissionsHintSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.agentConfigAgentType == AgentNone || p.agentConfigAgentType == "" {
			return modal.RenderedSection{}
		}
		if p.shouldShowAgentConfigSkipPerms() {
			flag := SkipPermissionsFlags[p.agentConfigAgentType]
			return modal.RenderedSection{Content: dimText(fmt.Sprintf("      (Adds %s)", flag))}
		}
		return modal.RenderedSection{Content: dimText("  Skip permissions not available for this agent")}
	}, nil)
}

// renderAgentConfigModal renders the agent config modal over a dimmed background.
func (p *Plugin) renderAgentConfigModal(width, height int) string {
	background := p.renderListView(width, height)

	p.ensureAgentConfigModal()
	if p.agentConfigModal == nil {
		return background
	}

	modalContent := p.agentConfigModal.Render(width, height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}
