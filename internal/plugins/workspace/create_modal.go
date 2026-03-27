package workspace

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/styles"
)

const (
	createNameFieldID       = "create-name"
	createBaseFieldID       = "create-base"
	createPromptFieldID     = "create-prompt"
	createTaskFieldID       = "create-task"
	createAgentListID       = "create-agent-list"
	createSkipPermissionsID = "create-skip-permissions"
	createSubmitID          = "create-submit"
	createCancelID          = "create-cancel"
	createBranchItemPrefix  = "create-branch-"
	createTaskItemPrefix    = "create-task-item-"
	createAgentItemPrefix   = "create-agent-"
)

func createIndexedID(prefix string, idx int) string {
	return fmt.Sprintf("%s%d", prefix, idx)
}

func parseIndexedID(prefix, id string) (int, bool) {
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	idx, err := strconv.Atoi(strings.TrimPrefix(id, prefix))
	if err != nil {
		return 0, false
	}
	return idx, true
}

func (p *Plugin) ensureCreateModal() {
	modalW := 70
	maxW := p.width - 4
	if maxW < 1 {
		maxW = 1
	}
	if modalW > maxW {
		modalW = maxW
	}

	if p.createModal != nil && p.createModalWidth == modalW {
		return
	}
	p.createModalWidth = modalW

	items := make([]modal.ListItem, len(AgentTypeOrder))
	for i, at := range AgentTypeOrder {
		items[i] = modal.ListItem{
			ID:    createIndexedID(createAgentItemPrefix, i),
			Label: AgentDisplayNames[at],
		}
	}

	p.createModal = modal.New("Create New Worktree",
		modal.WithWidth(modalW),
		modal.WithPrimaryAction(createSubmitID),
		modal.WithHints(false),
	).
		AddSection(p.createNameLabelSection()).
		AddSection(modal.Input(createNameFieldID, &p.createNameInput, modal.WithSubmitOnEnter(false))).
		AddSection(p.createNameErrorsSection()).
		AddSection(modal.Spacer()).
		AddSection(p.createBaseLabelSection()).
		AddSection(modal.Input(createBaseFieldID, &p.createBaseBranchInput, modal.WithSubmitOnEnter(false))).
		AddSection(p.createBranchDropdownSection()).
		AddSection(modal.Spacer()).
		AddSection(p.createPromptSection()).
		AddSection(modal.Spacer()).
		AddSection(p.createTaskSection()).
		AddSection(modal.Spacer()).
		AddSection(p.createAgentLabelSection()).
		AddSection(modal.List(createAgentListID, items, &p.createAgentIdx, modal.WithMaxVisible(len(items)))).
		AddSection(p.createSkipPermissionsSpacerSection()).
		AddSection(modal.When(p.shouldShowSkipPermissions, modal.Checkbox(createSkipPermissionsID, "Auto-approve all actions", &p.createSkipPermissions))).
		AddSection(p.createSkipPermissionsHintSection()).
		AddSection(modal.Spacer()).
		AddSection(p.createErrorSection()).
		AddSection(modal.When(func() bool { return p.createError != "" }, modal.Spacer())).
		AddSection(modal.Buttons(
			modal.Btn(" Create ", createSubmitID),
			modal.Btn(" Cancel ", createCancelID),
		))
}

func (p *Plugin) syncCreateModalFocus() {
	if p.createModal == nil {
		return
	}
	p.normalizeCreateFocus()
	p.syncCreateAgentIdx()

	if focusID := p.createFocusID(); focusID != "" {
		p.createModal.SetFocus(focusID)
	}
}

func (p *Plugin) normalizeCreateFocus() {
	if p.createFocus == 3 {
		prompt := p.getSelectedPrompt()
		if prompt != nil && prompt.TicketMode == TicketNone {
			p.createFocus = 4
		}
	}
	if p.createFocus == 5 && !p.shouldShowSkipPermissions() {
		p.createFocus = 6
	}
}

func (p *Plugin) syncCreateAgentIdx() {
	if p.createAgentIdx < 0 || p.createAgentIdx >= len(AgentTypeOrder) {
		p.createAgentIdx = p.agentTypeIndex(p.createAgentType)
		return
	}
	if AgentTypeOrder[p.createAgentIdx] != p.createAgentType {
		p.createAgentIdx = p.agentTypeIndex(p.createAgentType)
	}
}

func (p *Plugin) createFocusID() string {
	switch p.createFocus {
	case 0:
		return createNameFieldID
	case 1:
		return createBaseFieldID
	case 2:
		return createPromptFieldID
	case 3:
		return createTaskFieldID
	case 4:
		return createAgentListID // Use list ID for singleFocus mode
	case 5:
		return createSkipPermissionsID
	case 6:
		return createSubmitID
	case 7:
		return createCancelID
	default:
		return ""
	}
}

func (p *Plugin) createNameLabelSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		label := "Name:"
		nameValue := p.createNameInput.Value()
		if nameValue != "" {
			if p.branchNameValid {
				label = "Name: " + lipgloss.NewStyle().Foreground(styles.Success).Render("✓")
			} else {
				label = "Name: " + lipgloss.NewStyle().Foreground(styles.Error).Render("✗")
			}
		}
		return modal.RenderedSection{Content: label}
	}, nil)
}

func (p *Plugin) createNameErrorsSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		nameValue := p.createNameInput.Value()
		if nameValue == "" || p.branchNameValid {
			return modal.RenderedSection{}
		}

		var lines []string
		if len(p.branchNameErrors) > 0 {
			errorStyle := lipgloss.NewStyle().Foreground(styles.Error)
			lines = append(lines, errorStyle.Render("  ⚠ "+strings.Join(p.branchNameErrors, ", ")))
		}
		if p.branchNameSanitized != "" && p.branchNameSanitized != nameValue {
			lines = append(lines, dimText(fmt.Sprintf("  Suggestion: %s", p.branchNameSanitized)))
		}

		return modal.RenderedSection{Content: strings.Join(lines, "\n")}
	}, nil)
}

func (p *Plugin) createBaseLabelSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		return modal.RenderedSection{Content: "Base Branch (default: current):"}
	}, nil)
}

func (p *Plugin) createBranchDropdownSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if !p.branchDropdownVisible(focusID) {
			return modal.RenderedSection{}
		}

		lines := make([]string, 0)
		focusables := make([]modal.FocusableInfo, 0)
		lineY := 0

		if len(p.branchFiltered) > 0 {
			maxDropdown := 5
			dropdownCount := len(p.branchFiltered)
			if dropdownCount > maxDropdown {
				dropdownCount = maxDropdown
			}

			for i := 0; i < dropdownCount; i++ {
				branch := p.branchFiltered[i]
				maxWidth := contentWidth - 4
				if maxWidth < 8 {
					maxWidth = 8
				}
				if len(branch) > maxWidth {
					branch = branch[:maxWidth-3] + "..."
				}
				prefix := "  "
				if i == p.branchIdx {
					prefix = "> "
				}
				line := prefix + branch
				if i == p.branchIdx {
					line = lipgloss.NewStyle().Foreground(styles.Primary).Render(line)
				} else {
					line = dimText(line)
				}
				lines = append(lines, line)
				focusables = append(focusables, modal.FocusableInfo{
					ID:      createIndexedID(createBranchItemPrefix, i),
					OffsetX: 0,
					OffsetY: lineY,
					Width:   ansi.StringWidth(line),
					Height:  1,
				})
				lineY++
			}
			if len(p.branchFiltered) > maxDropdown {
				lines = append(lines, dimText(fmt.Sprintf("  ... and %d more", len(p.branchFiltered)-maxDropdown)))
			}
		} else if len(p.branchAll) == 0 {
			lines = append(lines, dimText("  Loading branches..."))
		}

		return modal.RenderedSection{Content: strings.Join(lines, "\n"), Focusables: focusables}
	}, nil)
}

func (p *Plugin) branchDropdownVisible(focusID string) bool {
	if focusID == createBaseFieldID || strings.HasPrefix(focusID, createBranchItemPrefix) {
		return len(p.branchFiltered) > 0 || len(p.branchAll) == 0
	}
	return false
}

func (p *Plugin) createPromptSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		lines := make([]string, 0, 4)
		focusables := make([]modal.FocusableInfo, 0, 1)

		lines = append(lines, "Prompt:")

		selectedPrompt := p.getSelectedPrompt()
		displayText := "(none)"
		if len(p.createPrompts) == 0 {
			displayText = "No prompts configured"
		} else if selectedPrompt != nil {
			scopeIndicator := "[G] global"
			if selectedPrompt.Source == "project" {
				scopeIndicator = "[P] project"
			}
			displayText = fmt.Sprintf("%s  %s", selectedPrompt.Name, dimText(scopeIndicator))
		}

		promptStyle := inputStyle()
		if focusID == createPromptFieldID {
			promptStyle = inputFocusedStyle()
		}
		rendered := promptStyle.Render(displayText)
		renderedLines := strings.Split(rendered, "\n")
		displayStartY := len(lines)
		lines = append(lines, renderedLines...)

		focusables = append(focusables, modal.FocusableInfo{
			ID:      createPromptFieldID,
			OffsetX: 0,
			OffsetY: displayStartY,
			Width:   ansi.StringWidth(rendered),
			Height:  len(renderedLines),
		})

		if len(p.createPrompts) == 0 {
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

func (p *Plugin) createTaskSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		selectedPrompt := p.getSelectedPrompt()
		if selectedPrompt != nil && selectedPrompt.TicketMode == TicketNone {
			return modal.RenderedSection{Content: dimText("Ticket: (not allowed by selected prompt)")}
		}

		lines := make([]string, 0, 6)
		focusables := make([]modal.FocusableInfo, 0)
		lineY := 0

		taskLabel := "Link Task (optional):"
		if selectedPrompt != nil {
			switch selectedPrompt.TicketMode {
			case TicketRequired:
				taskLabel = "Link Task (required by selected prompt):"
			case TicketOptional:
				taskLabel = "Link Task (optional for selected prompt):"
			}
		}
		lines = append(lines, taskLabel)
		lineY++

		isFocused := focusID == createTaskFieldID
		if p.createTaskID != "" {
			p.taskSearchInput.Blur()
		} else if isFocused {
			p.taskSearchInput.Focus()
		} else {
			p.taskSearchInput.Blur()
		}
		inputInnerWidth := contentWidth - 4
		if inputInnerWidth < 1 {
			inputInnerWidth = 1
		}
		p.taskSearchInput.Width = inputInnerWidth

		taskStyle := inputStyle()
		if isFocused {
			taskStyle = inputFocusedStyle()
		}

		display := ""
		if p.createTaskID != "" {
			display = p.createTaskID
			if p.createTaskTitle != "" {
				title := p.createTaskTitle
				maxTitle := inputInnerWidth - len(p.createTaskID) - 3
				if maxTitle > 10 && len(title) > maxTitle {
					title = title[:maxTitle-3] + "..."
				}
				if maxTitle > 10 {
					display = fmt.Sprintf("%s: %s", p.createTaskID, title)
				}
			}
		} else {
			display = p.taskSearchInput.View()
		}

		rendered := taskStyle.Render(display)
		renderedLines := strings.Split(rendered, "\n")
		displayStartY := lineY
		lines = append(lines, renderedLines...)
		focusables = append(focusables, modal.FocusableInfo{
			ID:      createTaskFieldID,
			OffsetX: 0,
			OffsetY: displayStartY,
			Width:   ansi.StringWidth(rendered),
			Height:  len(renderedLines),
		})
		lineY += len(renderedLines)

		if isFocused && p.createTaskID != "" {
			lines = append(lines, dimText("  Backspace to clear"))
			lineY++
		}
		if selectedPrompt != nil && selectedPrompt.TicketMode == TicketOptional && p.createTaskID == "" {
			fallback := ExtractFallback(selectedPrompt.Body)
			if fallback != "" {
				lines = append(lines, dimText(fmt.Sprintf("  Default if empty: \"%s\"", fallback)))
				lineY++
			}
		}
		if selectedPrompt != nil && selectedPrompt.TicketMode == TicketRequired {
			lines = append(lines, dimText("  Tip: ticket is passed as an ID only. The agent fetches via td."))
			lineY++
		}

		if p.taskDropdownVisible(focusID) && p.createTaskID == "" {
			if p.taskSearchLoading {
				lines = append(lines, dimText("  Loading tasks..."))
			} else if len(p.taskSearchFiltered) > 0 {
				maxDropdown := 5
				dropdownCount := len(p.taskSearchFiltered)
				if dropdownCount > maxDropdown {
					dropdownCount = maxDropdown
				}
				for i := 0; i < dropdownCount; i++ {
					task := p.taskSearchFiltered[i]
					prefix := "  "
					if i == p.taskSearchIdx {
						prefix = "> "
					}
					title := task.Title
					idWidth := len(task.ID)
					maxTitle := contentWidth - idWidth - 10
					if maxTitle < 10 {
						maxTitle = 10
					}
					if len(title) > maxTitle {
						title = title[:maxTitle-3] + "..."
					}
					line := fmt.Sprintf("%s%s  %s", prefix, task.ID, title)
					if i == p.taskSearchIdx {
						line = lipgloss.NewStyle().Foreground(styles.Primary).Render(line)
					} else {
						line = dimText(line)
					}
					lines = append(lines, line)
					focusables = append(focusables, modal.FocusableInfo{
						ID:      createIndexedID(createTaskItemPrefix, i),
						OffsetX: 0,
						OffsetY: lineY,
						Width:   ansi.StringWidth(line),
						Height:  1,
					})
					lineY++
				}
				if len(p.taskSearchFiltered) > maxDropdown {
					lines = append(lines, dimText(fmt.Sprintf("  ... and %d more", len(p.taskSearchFiltered)-maxDropdown)))
				}
			} else if p.taskSearchInput.Value() != "" {
				lines = append(lines, dimText("  No matching tasks"))
			} else if len(p.taskSearchAll) == 0 {
				lines = append(lines, dimText("  No open tasks found"))
			} else {
				lines = append(lines, dimText("  Type to search, \u2191/\u2193 to navigate"))
			}
		}

		return modal.RenderedSection{Content: strings.Join(lines, "\n"), Focusables: focusables}
	}, nil)
}

func (p *Plugin) taskDropdownVisible(focusID string) bool {
	return focusID == createTaskFieldID || strings.HasPrefix(focusID, createTaskItemPrefix)
}

func (p *Plugin) createAgentLabelSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		return modal.RenderedSection{Content: "Agent:"}
	}, nil)
}

func (p *Plugin) createSkipPermissionsSpacerSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.createAgentType == AgentNone {
			return modal.RenderedSection{}
		}
		return modal.RenderedSection{Content: " "}
	}, nil)
}

func (p *Plugin) createSkipPermissionsHintSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.createAgentType == AgentNone {
			return modal.RenderedSection{}
		}
		if p.shouldShowSkipPermissions() {
			flag := SkipPermissionsFlags[p.createAgentType]
			return modal.RenderedSection{Content: dimText(fmt.Sprintf("      (Adds %s)", flag))}
		}
		return modal.RenderedSection{Content: dimText("  Skip permissions not available for this agent")}
	}, nil)
}

func (p *Plugin) createErrorSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.createError == "" {
			return modal.RenderedSection{}
		}
		errStyle := lipgloss.NewStyle().Foreground(styles.Error)
		return modal.RenderedSection{Content: errStyle.Render("Error: " + p.createError)}
	}, nil)
}
