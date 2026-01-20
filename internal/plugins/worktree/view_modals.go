package worktree

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// renderCreateModal renders the new worktree modal with dimmed background.
func (p *Plugin) renderCreateModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	// Modal dimensions - increased for better task suggestion display
	modalW := 70
	if modalW > width-4 {
		modalW = width - 4
	}

	// Calculate input field width:
	// - modalStyle has border (2) + padding (4) = 6 chars
	// - inputStyle has border (2) + padding (2) = 4 chars
	// - So textinput content width = modalW - 6 (modal) - 4 (input style)
	inputW := modalW - 10
	if inputW < 20 {
		inputW = 20
	}

	// Set textinput widths and remove default prompt
	p.createNameInput.Width = inputW
	p.createNameInput.Prompt = ""
	p.createBaseBranchInput.Width = inputW
	p.createBaseBranchInput.Prompt = ""
	p.taskSearchInput.Width = inputW
	p.taskSearchInput.Prompt = ""

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Create New Worktree"))
	sb.WriteString("\n\n")

	// Name field - use textinput.View() for proper cursor rendering
	nameLabel := "Name:"
	nameStyle := inputStyle
	if p.createFocus == 0 {
		nameStyle = inputFocusedStyle
	}

	// Add validation indicator to label
	nameValue := p.createNameInput.Value()
	if nameValue != "" {
		if p.branchNameValid {
			nameLabel = "Name: " + lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("✓")
		} else {
			nameLabel = "Name: " + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗")
		}
	}

	sb.WriteString(nameLabel)
	sb.WriteString("\n")
	sb.WriteString(nameStyle.Render(p.createNameInput.View()))

	// Show validation errors or sanitized suggestion
	if nameValue != "" && !p.branchNameValid {
		sb.WriteString("\n")
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		if len(p.branchNameErrors) > 0 {
			sb.WriteString(errorStyle.Render("  ⚠ " + strings.Join(p.branchNameErrors, ", ")))
		}
		if p.branchNameSanitized != "" && p.branchNameSanitized != nameValue {
			sb.WriteString("\n")
			sb.WriteString(dimText(fmt.Sprintf("  Suggestion: %s", p.branchNameSanitized)))
		}
	}
	sb.WriteString("\n\n")

	// Base branch field with autocomplete
	baseLabel := "Base Branch (default: current):"
	baseStyle := inputStyle
	if p.createFocus == 1 {
		baseStyle = inputFocusedStyle
	}
	sb.WriteString(baseLabel)
	sb.WriteString("\n")
	sb.WriteString(baseStyle.Render(p.createBaseBranchInput.View()))

	// Show branch dropdown when focused and has branches
	if p.createFocus == 1 && len(p.branchFiltered) > 0 {
		maxDropdown := 5
		dropdownCount := min(maxDropdown, len(p.branchFiltered))
		for i := 0; i < dropdownCount; i++ {
			branch := p.branchFiltered[i]
			prefix := "  "
			if i == p.branchIdx {
				prefix = "> "
			}
			// Truncate branch name if needed
			if len(branch) > modalW-10 {
				branch = branch[:modalW-13] + "..."
			}
			line := prefix + branch
			sb.WriteString("\n")
			if i == p.branchIdx {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(line))
			} else {
				sb.WriteString(dimText(line))
			}
		}
		if len(p.branchFiltered) > maxDropdown {
			sb.WriteString("\n")
			sb.WriteString(dimText(fmt.Sprintf("  ... and %d more", len(p.branchFiltered)-maxDropdown)))
		}
	} else if p.createFocus == 1 && len(p.branchAll) == 0 {
		sb.WriteString("\n")
		sb.WriteString(dimText("  Loading branches..."))
	}
	sb.WriteString("\n\n")

	// Prompt field (focus=2)
	promptLabel := "Prompt:"
	promptStyle := inputStyle
	if p.createFocus == 2 {
		promptStyle = inputFocusedStyle
	}
	sb.WriteString(promptLabel)
	sb.WriteString("\n")

	// Get selected prompt
	selectedPrompt := p.getSelectedPrompt()
	if len(p.createPrompts) == 0 {
		// No prompts configured
		sb.WriteString(promptStyle.Render("No prompts configured"))
		sb.WriteString("\n")
		sb.WriteString(dimText("  See: docs/guides/creating-prompts.md"))
	} else if selectedPrompt == nil {
		// None selected
		sb.WriteString(promptStyle.Render("(none)"))
		sb.WriteString("\n")
		sb.WriteString(dimText("  Press Enter to select a prompt template"))
	} else {
		// Show selected prompt with scope indicator
		scopeIndicator := "[G] global"
		if selectedPrompt.Source == "project" {
			scopeIndicator = "[P] project"
		}
		displayText := fmt.Sprintf("%s  %s", selectedPrompt.Name, dimText(scopeIndicator))
		sb.WriteString(promptStyle.Render(displayText))
		sb.WriteString("\n")
		// Show preview of prompt body (first ~60 chars, single line)
		preview := strings.ReplaceAll(selectedPrompt.Body, "\n", " ")
		if runes := []rune(preview); len(runes) > 60 {
			preview = string(runes[:57]) + "..."
		}
		sb.WriteString(dimText(fmt.Sprintf("  Preview: %s", preview)))
	}
	sb.WriteString("\n\n")

	// Task ID field with search dropdown (focus=3)
	// Handle ticketMode: none - show disabled message instead of input
	if selectedPrompt != nil && selectedPrompt.TicketMode == TicketNone {
		sb.WriteString(dimText("Ticket: (not allowed by selected prompt)"))
	} else {
		// Dynamic label based on selected prompt's ticketMode
		var taskLabel string
		if selectedPrompt != nil {
			switch selectedPrompt.TicketMode {
			case TicketRequired:
				taskLabel = "Link Task (required by selected prompt):"
			case TicketOptional:
				taskLabel = "Link Task (optional for selected prompt):"
			default:
				taskLabel = "Link Task (optional):"
			}
		} else {
			taskLabel = "Link Task (optional):"
		}

		taskStyle := inputStyle
		if p.createFocus == 3 {
			taskStyle = inputFocusedStyle
		}
		sb.WriteString(taskLabel)
		sb.WriteString("\n")
		// If task already selected, show ID and title; otherwise show the search input
		if p.createTaskID != "" {
			// Show task ID and title, truncate title if needed
			display := p.createTaskID
			if p.createTaskTitle != "" {
				title := p.createTaskTitle
				maxTitle := inputW - len(p.createTaskID) - 3 // Account for ": " separator
				if maxTitle > 10 && len(title) > maxTitle {
					title = title[:maxTitle-3] + "..."
				}
				if maxTitle > 10 {
					display = fmt.Sprintf("%s: %s", p.createTaskID, title)
				}
			}
			sb.WriteString(taskStyle.Render(display))
		} else {
			sb.WriteString(taskStyle.Render(p.taskSearchInput.View()))
		}

		// Show hint when task is selected and focused
		if p.createFocus == 3 && p.createTaskID != "" {
			sb.WriteString("\n")
			sb.WriteString(dimText("  Backspace to clear"))
		}

		// Show fallback hint for optional tickets
		if selectedPrompt != nil && selectedPrompt.TicketMode == TicketOptional && p.createTaskID == "" {
			fallback := ExtractFallback(selectedPrompt.Body)
			if fallback != "" {
				sb.WriteString("\n")
				sb.WriteString(dimText(fmt.Sprintf("  Default if empty: \"%s\"", fallback)))
			}
		}

		// Show tip for required tickets
		if selectedPrompt != nil && selectedPrompt.TicketMode == TicketRequired {
			sb.WriteString("\n")
			sb.WriteString(dimText("  Tip: ticket is passed as an ID only. The agent fetches via td."))
		}

		// Show task dropdown when focused and has results
		if p.createFocus == 3 && p.createTaskID == "" {
			if p.taskSearchLoading {
				sb.WriteString("\n")
				sb.WriteString(dimText("  Loading tasks..."))
			} else if len(p.taskSearchFiltered) > 0 {
				maxDropdown := 5
				dropdownCount := min(maxDropdown, len(p.taskSearchFiltered))
				for i := 0; i < dropdownCount; i++ {
					task := p.taskSearchFiltered[i]
					prefix := "  "
					if i == p.taskSearchIdx {
						prefix = "> "
					}
					// Truncate title based on available width
					// Account for: prefix(2) + task.ID(~12) + spacing(2) + modal padding(6)
					title := task.Title
					idWidth := len(task.ID)
					maxTitle := modalW - idWidth - 10
					if maxTitle < 10 {
						maxTitle = 10
					}
					if len(title) > maxTitle {
						title = title[:maxTitle-3] + "..."
					}
					line := fmt.Sprintf("%s%s  %s", prefix, task.ID, title)
					sb.WriteString("\n")
					if i == p.taskSearchIdx {
						sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(line))
					} else {
						sb.WriteString(dimText(line))
					}
				}
				if len(p.taskSearchFiltered) > maxDropdown {
					sb.WriteString("\n")
					sb.WriteString(dimText(fmt.Sprintf("  ... and %d more", len(p.taskSearchFiltered)-maxDropdown)))
				}
			} else if p.taskSearchInput.Value() != "" {
				sb.WriteString("\n")
				sb.WriteString(dimText("  No matching tasks"))
			} else if len(p.taskSearchAll) == 0 {
				sb.WriteString("\n")
				sb.WriteString(dimText("  No open tasks found"))
			} else {
				// Show hint when no query
				sb.WriteString("\n")
				sb.WriteString(dimText("  Type to search, ↑/↓ to navigate"))
			}
		}
	}
	sb.WriteString("\n\n")

	// Agent Selection (radio buttons) - focus=4
	sb.WriteString("Agent:")
	sb.WriteString("\n")
	for _, at := range AgentTypeOrder {
		prefix := "  ○ "
		if at == p.createAgentType {
			prefix = "  ● "
		}
		name := AgentDisplayNames[at]
		line := prefix + name

		if p.createFocus == 4 && at == p.createAgentType {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(line))
		} else if at == p.createAgentType {
			sb.WriteString(line)
		} else {
			sb.WriteString(dimText(line))
		}
		sb.WriteString("\n")
	}

	// Skip Permissions Checkbox (only show when agent is selected and supports it) - focus=5
	if p.createAgentType != AgentNone {
		flag := SkipPermissionsFlags[p.createAgentType]
		if flag != "" {
			sb.WriteString("\n")
			checkBox := "[ ]"
			if p.createSkipPermissions {
				checkBox = "[x]"
			}
			skipLine := fmt.Sprintf("  %s Auto-approve all actions", checkBox)

			if p.createFocus == 5 {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(skipLine))
			} else {
				sb.WriteString(skipLine)
			}
			sb.WriteString("\n")
			sb.WriteString(dimText(fmt.Sprintf("      (Adds %s)", flag)))
			sb.WriteString("\n")
		} else {
			sb.WriteString("\n")
			sb.WriteString(dimText("  Skip permissions not available for this agent"))
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")

	// Display error if present
	if p.createError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		sb.WriteString(errStyle.Render("Error: " + p.createError))
		sb.WriteString("\n\n")
	}

	// Buttons - Create and Cancel (focus=6 and focus=7)
	createBtnStyle := styles.Button
	cancelBtnStyle := styles.Button
	if p.createFocus == 6 {
		createBtnStyle = styles.ButtonFocused
	} else if p.createButtonHover == 1 {
		createBtnStyle = styles.ButtonHover
	}
	if p.createFocus == 7 {
		cancelBtnStyle = styles.ButtonFocused
	} else if p.createButtonHover == 2 {
		cancelBtnStyle = styles.ButtonHover
	}
	sb.WriteString(createBtnStyle.Render(" Create "))
	sb.WriteString("  ")
	sb.WriteString(cancelBtnStyle.Render(" Cancel "))

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Calculate modal position for hit regions
	modalH := lipgloss.Height(modal)
	modalX := (width - modalW) / 2
	modalY := (height - modalH) / 2

	// Register hit regions for interactive elements
	// modalStyle has border(1) + padding(1) = 2 rows before content starts
	// Track Y position through modal content structure
	// IMPORTANT: inputStyle/inputFocusedStyle add a border, making inputs 3 lines tall
	// (top border + content + bottom border)
	hitX := modalX + 3 // border + padding for left edge
	hitW := modalW - 6 // width minus border+padding on both sides
	currentY := modalY + 2 // border(1) + padding(1)

	// Title "Create New Worktree" + blank
	currentY += 2

	// Name field (focus=0): label + bordered input (3 lines)
	currentY++ // "Name:" label
	p.mouseHandler.HitMap.AddRect(regionCreateInput, hitX, currentY, hitW, 3, 0)
	currentY += 3 // bordered input (top border + content + bottom border)

	// Validation errors (conditional) + blank line
	// Note: nameValue already declared earlier in the render function
	if nameValue != "" && !p.branchNameValid {
		currentY++ // error line
		if p.branchNameSanitized != "" && p.branchNameSanitized != nameValue {
			currentY++ // suggestion line
		}
	}
	currentY++ // blank line (\n\n after name section)

	// Base Branch field (focus=1): label + bordered input (3 lines)
	currentY++ // "Base Branch..." label
	p.mouseHandler.HitMap.AddRect(regionCreateInput, hitX, currentY, hitW, 3, 1)
	currentY += 3 // bordered input

	// Branch dropdown (if visible)
	if p.createFocus == 1 && len(p.branchFiltered) > 0 {
		maxDropdown := 5
		dropdownCount := min(maxDropdown, len(p.branchFiltered))
		for i := 0; i < dropdownCount; i++ {
			p.mouseHandler.HitMap.AddRect(regionCreateDropdown, hitX, currentY, hitW, 1, dropdownItemData{field: 1, idx: i})
			currentY++
		}
		if len(p.branchFiltered) > maxDropdown {
			currentY++ // "... and N more"
		}
	} else if p.createFocus == 1 && len(p.branchAll) == 0 {
		currentY++ // "Loading branches..."
	}
	currentY++ // blank

	// Prompt field (focus=2): label + bordered display (3 lines) + preview hint + blank
	currentY++ // "Prompt:" label
	p.mouseHandler.HitMap.AddRect(regionCreateInput, hitX, currentY, hitW, 3, 2)
	currentY += 3 // bordered prompt display
	currentY++ // preview/hint line
	currentY++ // blank

	// Task field (focus=3) - only shown when ticketMode allows
	// Note: selectedPrompt already declared at rendering section
	if selectedPrompt == nil || selectedPrompt.TicketMode != TicketNone {
		currentY++ // "Link Task..." label
		p.mouseHandler.HitMap.AddRect(regionCreateInput, hitX, currentY, hitW, 3, 3)
		currentY += 3 // bordered input (3 lines)

		// Task hints (backspace hint, fallback hint, or required hint)
		if p.createFocus == 3 && p.createTaskID != "" {
			currentY++ // "Backspace to clear"
		}
		if selectedPrompt != nil && selectedPrompt.TicketMode == TicketOptional && p.createTaskID == "" {
			fallback := ExtractFallback(selectedPrompt.Body)
			if fallback != "" {
				currentY++ // Default hint
			}
		}
		if selectedPrompt != nil && selectedPrompt.TicketMode == TicketRequired {
			currentY++ // Tip line
		}

		// Task dropdown (if visible)
		if p.createFocus == 3 && p.createTaskID == "" {
			if p.taskSearchLoading {
				currentY++ // "Loading tasks..."
			} else if len(p.taskSearchFiltered) > 0 {
				maxDropdown := 5
				dropdownCount := min(maxDropdown, len(p.taskSearchFiltered))
				for i := 0; i < dropdownCount; i++ {
					p.mouseHandler.HitMap.AddRect(regionCreateDropdown, hitX, currentY, hitW, 1, dropdownItemData{field: 3, idx: i})
					currentY++
				}
				if len(p.taskSearchFiltered) > maxDropdown {
					currentY++ // "... and N more"
				}
			} else if p.taskSearchInput.Value() != "" {
				currentY++ // "No matching tasks"
			} else if len(p.taskSearchAll) == 0 {
				currentY++ // "No open tasks found"
			} else {
				currentY++ // "Type to search..." hint
			}
		}
	} else {
		currentY++ // "Ticket: (not allowed...)"
	}
	currentY += 2 // blank lines

	// Agent options (focus=4): "Agent:" + agent options
	currentY++ // "Agent:" label
	for i := range AgentTypeOrder {
		p.mouseHandler.HitMap.AddRect(regionCreateAgentOption, hitX, currentY, hitW, 1, i)
		currentY++
	}

	// Skip permissions checkbox (focus=5) - only when agent supports it
	if p.createAgentType != AgentNone {
		flag := SkipPermissionsFlags[p.createAgentType]
		if flag != "" {
			currentY++ // blank before checkbox
			p.mouseHandler.HitMap.AddRect(regionCreateCheckbox, hitX, currentY, hitW, 1, 5)
			currentY++ // checkbox line
			currentY++ // "(Adds --dangerously...)" hint
		} else {
			currentY++ // blank
			currentY++ // "Skip permissions not available..."
		}
	}
	currentY++ // blank

	// Error display (if present)
	if p.createError != "" {
		currentY += 2 // error line + blank
	}

	// Buttons (focus=6 create, focus=7 cancel)
	p.mouseHandler.HitMap.AddRect(regionCreateButton, hitX, currentY, 12, 1, 6)
	p.mouseHandler.HitMap.AddRect(regionCreateButton, hitX+14, currentY, 12, 1, 7)

	// Use OverlayModal for dimmed background effect
	return ui.OverlayModal(background, modal, width, height)
}

// renderTaskLinkModal renders the task link modal for existing worktrees with dimmed background.
func (p *Plugin) renderTaskLinkModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	// Modal dimensions - increased for better task display
	modalW := 70
	if modalW > width-4 {
		modalW = width - 4
	}

	// Calculate input field width
	// - modalStyle has border (2) + padding (4) = 6 chars
	// - inputStyle has border (2) + padding (2) = 4 chars
	inputW := modalW - 10
	if inputW < 20 {
		inputW = 20
	}

	// Set textinput width and remove default prompt
	p.taskSearchInput.Width = inputW
	p.taskSearchInput.Prompt = ""

	var sb strings.Builder
	title := "Link Task"
	if p.linkingWorktree != nil {
		title = fmt.Sprintf("Link Task to %s", p.linkingWorktree.Name)
	}
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
	sb.WriteString("\n\n")

	// Search field - use textinput.View() for proper cursor rendering
	searchLabel := "Search tasks:"
	searchStyle := inputFocusedStyle
	sb.WriteString(searchLabel)
	sb.WriteString("\n")
	sb.WriteString(searchStyle.Render(p.taskSearchInput.View()))

	// Task dropdown
	if p.taskSearchLoading {
		sb.WriteString("\n")
		sb.WriteString(dimText("  Loading tasks..."))
	} else if len(p.taskSearchFiltered) > 0 {
		maxDropdown := 8
		dropdownCount := min(maxDropdown, len(p.taskSearchFiltered))
		for i := 0; i < dropdownCount; i++ {
			task := p.taskSearchFiltered[i]
			prefix := "  "
			if i == p.taskSearchIdx {
				prefix = "> "
			}
			// Truncate title based on available width
			taskTitle := task.Title
			idWidth := len(task.ID)
			maxTitle := modalW - idWidth - 10
			if maxTitle < 10 {
				maxTitle = 10
			}
			if len(taskTitle) > maxTitle {
				taskTitle = taskTitle[:maxTitle-3] + "..."
			}
			line := fmt.Sprintf("%s%s  %s", prefix, task.ID, taskTitle)
			sb.WriteString("\n")
			if i == p.taskSearchIdx {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(line))
			} else {
				sb.WriteString(dimText(line))
			}
		}
		if len(p.taskSearchFiltered) > maxDropdown {
			sb.WriteString("\n")
			sb.WriteString(dimText(fmt.Sprintf("  ... and %d more", len(p.taskSearchFiltered)-maxDropdown)))
		}
	} else if p.taskSearchInput.Value() != "" {
		sb.WriteString("\n")
		sb.WriteString(dimText("  No matching tasks"))
	} else if len(p.taskSearchAll) == 0 {
		sb.WriteString("\n")
		sb.WriteString(dimText("  No open tasks found"))
	} else {
		// Show all tasks when no query
		maxDropdown := 8
		dropdownCount := min(maxDropdown, len(p.taskSearchAll))
		for i := 0; i < dropdownCount; i++ {
			task := p.taskSearchAll[i]
			prefix := "  "
			if i == p.taskSearchIdx {
				prefix = "> "
			}
			taskTitle := task.Title
			idWidth := len(task.ID)
			maxTitle := modalW - idWidth - 10
			if maxTitle < 10 {
				maxTitle = 10
			}
			if len(taskTitle) > maxTitle {
				taskTitle = taskTitle[:maxTitle-3] + "..."
			}
			line := fmt.Sprintf("%s%s  %s", prefix, task.ID, taskTitle)
			sb.WriteString("\n")
			if i == p.taskSearchIdx {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(line))
			} else {
				sb.WriteString(dimText(line))
			}
		}
		if len(p.taskSearchAll) > maxDropdown {
			sb.WriteString("\n")
			sb.WriteString(dimText(fmt.Sprintf("  ... and %d more", len(p.taskSearchAll)-maxDropdown)))
		}
	}

	sb.WriteString("\n\n")
	sb.WriteString(dimText("↑/↓ navigate  Enter select  Esc cancel"))

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Calculate modal position for hit regions
	modalH := lipgloss.Height(modal)
	modalX := (width - modalW) / 2
	modalY := (height - modalH) / 2

	// Register hit regions for task dropdown items
	// Content: title(1) + blank(1) + label(1) + bordered-input(3) = 6 lines before dropdown
	// Border offset is 2: border(1) + padding(1)
	dropdownStartY := modalY + 2 + 6 // border(1) + padding(1) + content lines to dropdown

	// Determine which list to use for hit regions
	tasks := p.taskSearchFiltered
	if len(tasks) == 0 && p.taskSearchInput.Value() == "" && len(p.taskSearchAll) > 0 {
		tasks = p.taskSearchAll
	}

	if len(tasks) > 0 {
		maxDropdown := 8
		dropdownCount := min(maxDropdown, len(tasks))
		for i := 0; i < dropdownCount; i++ {
			p.mouseHandler.HitMap.AddRect(regionTaskLinkDropdown, modalX+2, dropdownStartY+i, modalW-6, 1, i)
		}
	}

	// Use OverlayModal for dimmed background effect
	return ui.OverlayModal(background, modal, width, height)
}

// renderConfirmDeleteModal renders the delete confirmation modal.
func (p *Plugin) renderConfirmDeleteModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	if p.deleteConfirmWorktree == nil {
		return background
	}

	// Modal dimensions
	modalW := 58
	if modalW > width-4 {
		modalW = width - 4
	}

	wt := p.deleteConfirmWorktree

	var sb strings.Builder
	title := "Delete Worktree?"
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render(title))
	sb.WriteString("\n\n")

	// Worktree name
	sb.WriteString(fmt.Sprintf("Name:   %s\n", lipgloss.NewStyle().Bold(true).Render(wt.Name)))
	sb.WriteString(fmt.Sprintf("Branch: %s\n", wt.Branch))
	sb.WriteString(fmt.Sprintf("Path:   %s\n", dimText(wt.Path)))
	sb.WriteString("\n")

	// Warning text
	warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	sb.WriteString(warningStyle.Render("This will:"))
	sb.WriteString("\n")
	sb.WriteString(dimText("  • Remove the working directory"))
	sb.WriteString("\n")
	sb.WriteString(dimText("  • Uncommitted changes will be lost"))
	sb.WriteString("\n\n")

	// Branch deletion options
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Branch Cleanup (Optional)"))
	sb.WriteString("\n")

	// Checkbox options
	type checkboxOpt struct {
		label   string
		checked bool
		hint    string
		focusID int
	}

	opts := []checkboxOpt{
		{"Delete local branch", p.deleteLocalBranchOpt,
			"Removes '" + wt.Branch + "' locally", 0},
	}

	// Only show remote option if remote branch exists
	if p.deleteHasRemote {
		opts = append(opts, checkboxOpt{
			"Delete remote branch", p.deleteRemoteBranchOpt,
			"Removes 'origin/" + wt.Branch + "'", 1,
		})
	}

	checkboxLines := 0
	for _, opt := range opts {
		checkbox := "[ ]"
		if opt.checked {
			checkbox = "[x]"
		}

		line := fmt.Sprintf("  %s %s", checkbox, opt.label)

		if p.deleteConfirmFocus == opt.focusID {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Bold(true).Render("> " + line[2:]))
		} else {
			sb.WriteString(line)
		}
		sb.WriteString("\n")
		sb.WriteString(dimText("      " + opt.hint))
		sb.WriteString("\n")
		checkboxLines += 2
	}

	sb.WriteString("\n")

	// Determine button focus indices based on whether remote is shown
	deleteBtnFocus := 1
	cancelBtnFocus := 2
	if p.deleteHasRemote {
		deleteBtnFocus = 2
		cancelBtnFocus = 3
	}

	// Render buttons with focus/hover states
	deleteStyle := styles.ButtonDanger
	cancelStyle := styles.Button
	if p.deleteConfirmFocus == deleteBtnFocus {
		deleteStyle = styles.ButtonDangerFocused
	} else if p.deleteConfirmButtonHover == 1 {
		deleteStyle = styles.ButtonDangerHover
	}
	if p.deleteConfirmFocus == cancelBtnFocus {
		cancelStyle = styles.ButtonFocused
	} else if p.deleteConfirmButtonHover == 2 {
		cancelStyle = styles.ButtonHover
	}
	sb.WriteString(deleteStyle.Render(" Delete "))
	sb.WriteString("  ")
	sb.WriteString(cancelStyle.Render(" Cancel "))

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Register hit regions for the modal
	// Calculate modal position (centered)
	modalHeight := lipgloss.Height(modal)
	modalStartX := (width - modalW) / 2
	modalStartY := (height - modalHeight) / 2

	// Calculate Y positions for hit regions dynamically
	// modalStyle has border(1) + padding(1) = 2 rows at top before content starts
	// We must also account for text wrapping (e.g., long paths may wrap to multiple lines)
	//
	// Content structure:
	// - Title line
	// - Blank line (\n\n)
	// - Name line
	// - Branch line
	// - Path line(s) - may wrap!
	// - Blank line
	// - "This will:" line
	// - Bullet 1
	// - Bullet 2
	// - Blank line (\n\n)
	// - "Branch Cleanup (Optional)" header
	// - Checkbox lines start here

	// Calculate content width for wrapping (modal width minus border and padding)
	contentWidth := modalW - 6 // border(2) + padding(4) = 6

	// Build up line count dynamically to handle wrapped text
	currentY := modalStartY + 2 // Start after border(1) + padding(1)

	// Title + blank
	currentY += 2

	// Name line (typically doesn't wrap)
	currentY++

	// Branch line (typically doesn't wrap)
	currentY++

	// Path line - may wrap if path is long
	// Use visual width (ansi.StringWidth) for accurate wrapping calculation
	pathLine := fmt.Sprintf("Path:   %s", wt.Path)
	pathVisualWidth := ansi.StringWidth(pathLine)
	pathLineCount := (pathVisualWidth + contentWidth - 1) / contentWidth // Ceiling division
	if pathLineCount < 1 {
		pathLineCount = 1
	}
	currentY += pathLineCount

	// Blank line
	currentY++

	// "This will:" + 2 bullets + blank
	currentY += 4

	// "Branch Cleanup (Optional)" header
	currentY++

	// Checkboxes start here
	checkboxStartY := currentY

	// Hit regions for checkboxes
	hitX := modalStartX + 3 // border(1) + padding(2) = 3
	hitW := modalW - 6      // width minus border+padding on both sides
	p.mouseHandler.HitMap.AddRect(regionDeleteLocalBranchCheck, hitX, checkboxStartY, hitW, 1, 0)
	if p.deleteHasRemote {
		p.mouseHandler.HitMap.AddRect(regionDeleteRemoteBranchCheck, hitX, checkboxStartY+2, hitW, 1, 1)
	}

	// Hit regions for buttons (after checkboxes + blank line)
	buttonY := checkboxStartY + checkboxLines + 1
	p.mouseHandler.HitMap.AddRect(regionDeleteConfirmDelete, hitX, buttonY, 12, 1, nil)
	cancelX := hitX + 12 + 2
	p.mouseHandler.HitMap.AddRect(regionDeleteConfirmCancel, cancelX, buttonY, 12, 1, nil)

	return ui.OverlayModal(background, modal, width, height)
}

// renderPromptPickerModal renders the prompt picker modal.
func (p *Plugin) renderPromptPickerModal(width, height int) string {
	// Render the background (create modal behind it)
	background := p.renderCreateModal(width, height)

	if p.promptPicker == nil {
		return background
	}

	// Modal dimensions
	modalW := 80
	if modalW > width-4 {
		modalW = width - 4
	}
	p.promptPicker.width = modalW
	p.promptPicker.height = height

	content := p.promptPicker.View()
	modal := modalStyle.Width(modalW).Render(content)

	// Calculate modal position for hit regions
	modalH := lipgloss.Height(modal)
	modalX := (width - modalW) / 2
	modalY := (height - modalH) / 2

	// Register hit region for filter input
	// Layout from content start (modalY + 2 for border + padding):
	// - header "Select Prompt..." (1 line)
	// - blank from \n\n (1 line)
	// - "Filter:" label (1 line)
	// - bordered filter input (3 lines: border + content + border)
	// Filter input starts at: 1 + 1 + 1 = 3 lines from content start
	filterY := modalY + 2 + 3 // border(1) + padding(1) + header + blank + label
	p.mouseHandler.HitMap.AddRect(regionPromptFilter, modalX+2, filterY, 32, 3, nil) // height 3 for bordered input

	// Register hit regions for prompt items
	// After filter input (3 lines) + blank (1 line) + column headers (1 line) + separator (1 line) = 6 more lines
	// Total from content start: 3 (before filter) + 3 (filter) + 1 (blank) + 1 (headers) + 1 (separator) = 9
	// "None" option is first, then filtered prompts
	itemStartY := modalY + 2 + 9 // border(1) + padding(1) + header + blank + label + bordered-filter + blank + col-headers + separator
	itemHeight := 1              // Each prompt item is 1 line

	// "None" option at index -1
	p.mouseHandler.HitMap.AddRect(regionPromptItem, modalX+2, itemStartY, modalW-6, itemHeight, -1)

	// Prompt items
	maxVisible := 10
	if len(p.promptPicker.filtered) > 0 {
		visibleCount := min(maxVisible, len(p.promptPicker.filtered))
		for i := range visibleCount {
			y := itemStartY + 1 + i // +1 for "none" row
			p.mouseHandler.HitMap.AddRect(regionPromptItem, modalX+2, y, modalW-6, itemHeight, i)
		}
	}

	return ui.OverlayModal(background, modal, width, height)
}

// renderAgentChoiceModal renders the agent action choice modal.
func (p *Plugin) renderAgentChoiceModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	if p.agentChoiceWorktree == nil {
		return background
	}

	// Modal dimensions
	modalW := 50
	if modalW > width-4 {
		modalW = width - 4
	}

	var sb strings.Builder
	title := fmt.Sprintf("Agent Running: %s", p.agentChoiceWorktree.Name)
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
	sb.WriteString("\n\n")
	sb.WriteString("An agent is already running on this worktree.\n")
	sb.WriteString("What would you like to do?\n\n")

	options := []string{"Attach to session", "Restart agent"}
	for i, opt := range options {
		prefix := "  "
		selected := i == p.agentChoiceIdx && p.agentChoiceButtonFocus == 0
		if selected {
			prefix = "> "
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(prefix + opt))
		} else {
			sb.WriteString(dimText(prefix + opt))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	// Render buttons with focus/hover states
	confirmStyle := styles.Button
	cancelStyle := styles.Button
	if p.agentChoiceButtonFocus == 1 {
		confirmStyle = styles.ButtonFocused
	} else if p.agentChoiceButtonHover == 1 {
		confirmStyle = styles.ButtonHover
	}
	if p.agentChoiceButtonFocus == 2 {
		cancelStyle = styles.ButtonFocused
	} else if p.agentChoiceButtonHover == 2 {
		cancelStyle = styles.ButtonHover
	}
	sb.WriteString(confirmStyle.Render(" Confirm "))
	sb.WriteString("  ")
	sb.WriteString(cancelStyle.Render(" Cancel "))

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Register hit regions for the modal
	// Calculate modal position (centered)
	modalHeight := lipgloss.Height(modal)
	modalStartX := (width - modalW) / 2
	modalStartY := (height - modalHeight) / 2

	// Hit regions for options (inside modal content area)
	// Content lines: title(1) + blank(1) + message(2) + blank(1) = 5 lines before options
	// Border offset is 2: border(1) + padding(1)
	optionY := modalStartY + 2 + 5 // border(1) + padding(1) + header lines
	optionX := modalStartX + 3     // border + padding + "  " prefix
	for i := range options {
		p.mouseHandler.HitMap.AddRect(regionAgentChoiceOption, optionX, optionY+i, modalW-6, 1, i)
	}

	// Hit regions for buttons
	// Buttons are after options (2) + empty (1) = 3 more lines
	buttonY := optionY + 3
	confirmX := modalStartX + 3 // border + padding
	// " Confirm " (9) + Padding(0,2) = 13 chars, " Cancel " (8) + Padding(0,2) = 12 chars
	p.mouseHandler.HitMap.AddRect(regionAgentChoiceConfirm, confirmX, buttonY, 13, 1, nil)
	cancelX := confirmX + 13 + 2 // confirm width + spacing
	p.mouseHandler.HitMap.AddRect(regionAgentChoiceCancel, cancelX, buttonY, 12, 1, nil)

	return ui.OverlayModal(background, modal, width, height)
}

// renderMergeModal renders the merge workflow modal with dimmed background.
func (p *Plugin) renderMergeModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	if p.mergeState == nil {
		return background
	}

	// Modal dimensions
	modalW := 70
	if modalW > width-4 {
		modalW = width - 4
	}
	modalH := height - 6
	if modalH < 20 {
		modalH = 20
	}

	var sb strings.Builder

	// Title
	title := fmt.Sprintf("Merge Workflow: %s", p.mergeState.Worktree.Name)
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
	sb.WriteString("\n\n")

	// Progress indicators - show different steps based on merge method
	var steps []MergeWorkflowStep
	if p.mergeState.UseDirectMerge {
		// Direct merge path
		steps = []MergeWorkflowStep{
			MergeStepReviewDiff,
			MergeStepMergeMethod,
			MergeStepDirectMerge,
			MergeStepPostMergeConfirmation,
			MergeStepCleanup,
		}
	} else {
		// PR workflow path
		steps = []MergeWorkflowStep{
			MergeStepReviewDiff,
			MergeStepMergeMethod,
			MergeStepPush,
			MergeStepCreatePR,
			MergeStepWaitingMerge,
			MergeStepPostMergeConfirmation,
			MergeStepCleanup,
		}
	}

	for _, step := range steps {
		status := p.mergeState.StepStatus[step]
		icon := "○" // pending
		color := lipgloss.Color("240")

		switch status {
		case "running":
			icon = "●"
			color = lipgloss.Color("214") // yellow
		case "done":
			icon = "✓"
			color = lipgloss.Color("42") // green
		case "error":
			icon = "✗"
			color = lipgloss.Color("196") // red
		case "skipped":
			icon = "○"
			color = lipgloss.Color("240") // gray, same as pending
		}

		// Highlight current step
		stepName := step.String()
		if step == p.mergeState.Step {
			stepName = lipgloss.NewStyle().Bold(true).Render(stepName)
		}

		stepLine := fmt.Sprintf("  %s %s",
			lipgloss.NewStyle().Foreground(color).Render(icon),
			stepName,
		)
		sb.WriteString(stepLine)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("─", min(modalW-4, 60)))
	sb.WriteString("\n\n")

	// Step-specific content
	switch p.mergeState.Step {
	case MergeStepReviewDiff:
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Diff Summary:"))
		sb.WriteString("\n\n")
		if p.mergeState.DiffSummary != "" {
			// Truncate to fit modal
			summaryLines := strings.Split(p.mergeState.DiffSummary, "\n")
			maxLines := modalH - 15 // Account for header, progress, footer
			if maxLines < 5 {
				maxLines = 5
			}
			if len(summaryLines) > maxLines {
				summaryLines = summaryLines[:maxLines]
				summaryLines = append(summaryLines, fmt.Sprintf("... (%d more lines)", len(strings.Split(p.mergeState.DiffSummary, "\n"))-maxLines))
			}
			for _, line := range summaryLines {
				sb.WriteString(p.colorDiffLine(line, modalW-4))
				sb.WriteString("\n")
			}
		} else {
			sb.WriteString(dimText("Loading diff..."))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		sb.WriteString(dimText("Press Enter to continue, Esc to cancel"))

	case MergeStepMergeMethod:
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Choose Merge Method:"))
		sb.WriteString("\n\n")

		baseBranch := resolveBaseBranch(p.mergeState.Worktree)

		// Style helpers for selection and hover states
		selectedStyle := func(s string) string {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(s)
		}
		hoverStyle := func(s string) string {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(s)
		}

		// Option 1: Create PR (default)
		prIcon := "○"
		prStyle := dimText
		if p.mergeState.MergeMethodOption == 0 {
			prIcon = "●"
			prStyle = selectedStyle
		} else if p.mergeMethodHover == 1 {
			prStyle = hoverStyle
		}
		sb.WriteString(prStyle(fmt.Sprintf(" %s Create Pull Request (Recommended)", prIcon)))
		sb.WriteString("\n")
		sb.WriteString(dimText("      Push to origin and create a GitHub PR for review"))
		sb.WriteString("\n\n")

		// Option 2: Direct merge
		directIcon := "○"
		directStyle := dimText
		if p.mergeState.MergeMethodOption == 1 {
			directIcon = "●"
			directStyle = selectedStyle
		} else if p.mergeMethodHover == 2 {
			directStyle = hoverStyle
		}
		sb.WriteString(directStyle(fmt.Sprintf(" %s Direct Merge", directIcon)))
		sb.WriteString("\n")
		sb.WriteString(dimText(fmt.Sprintf("      Merge directly to '%s' without PR", baseBranch)))
		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("      Warning: Bypasses code review"))
		sb.WriteString("\n\n")

		sb.WriteString(dimText("↑/↓: select   Enter: continue   Esc: cancel"))

	case MergeStepDirectMerge:
		sb.WriteString("Merging directly to base branch...")
		sb.WriteString("\n\n")
		baseBranch := resolveBaseBranch(p.mergeState.Worktree)
		sb.WriteString(dimText(fmt.Sprintf("Merging '%s' into '%s'...", p.mergeState.Worktree.Branch, baseBranch)))

	case MergeStepPush:
		sb.WriteString("Pushing branch to remote...")

	case MergeStepCreatePR:
		sb.WriteString("Creating pull request...")

	case MergeStepWaitingMerge:
		if p.mergeState.ExistingPR {
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")).Render("Using Existing Pull Request"))
		} else {
			sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Pull Request Created"))
		}
		sb.WriteString("\n\n")
		if p.mergeState.PRURL != "" {
			sb.WriteString(fmt.Sprintf("URL: %s", p.mergeState.PRURL))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		sb.WriteString("Checking merge status every 30 seconds...")
		sb.WriteString("\n\n")
		sb.WriteString(strings.Repeat("─", min(modalW-4, 60)))
		sb.WriteString("\n\n")

		// Radio button selection
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("After merge:"))
		sb.WriteString("\n\n")

		// Option 1: Delete worktree (default)
		if p.mergeState.DeleteAfterMerge {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(" ● Delete worktree after merge"))
		} else {
			sb.WriteString(dimText(" ○ Delete worktree after merge"))
		}
		sb.WriteString("\n")

		// Option 2: Keep worktree
		if !p.mergeState.DeleteAfterMerge {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(" ● Keep worktree"))
		} else {
			sb.WriteString(dimText(" ○ Keep worktree"))
		}
		sb.WriteString("\n\n")

		sb.WriteString(dimText(" (This takes effect only once the PR is merged)"))
		sb.WriteString("\n\n")
		sb.WriteString(dimText("Enter: check now   o: open PR   Esc: exit   ↑/↓: change option"))

	case MergeStepPostMergeConfirmation:
		// Success header
		mergeMethod := "PR Merged"
		if p.mergeState.UseDirectMerge {
			mergeMethod = "Direct Merge Complete"
		}
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")).Render(mergeMethod + "!"))
		sb.WriteString("\n\n")

		sb.WriteString(strings.Repeat("─", min(modalW-4, 60)))
		sb.WriteString("\n\n")

		// Cleanup options header
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Cleanup Options"))
		sb.WriteString("\n")
		sb.WriteString(dimText("Select what to clean up:"))
		sb.WriteString("\n\n")

		baseBranch := resolveBaseBranch(p.mergeState.Worktree)

		// Checkbox options
		type checkboxOpt struct {
			label   string
			checked bool
			hint    string
		}
		opts := []checkboxOpt{
			{"Delete local worktree", p.mergeState.DeleteLocalWorktree,
				"Removes " + p.mergeState.Worktree.Path},
			{"Delete local branch", p.mergeState.DeleteLocalBranch,
				"Removes '" + p.mergeState.Worktree.Branch + "' locally"},
			{"Delete remote branch", p.mergeState.DeleteRemoteBranch,
				"Removes from GitHub (often auto-deleted)"},
		}

		for i, opt := range opts {
			checkbox := "[ ]"
			if opt.checked {
				checkbox = "[x]"
			}

			line := fmt.Sprintf("  %s %s", checkbox, opt.label)

			if p.mergeState.ConfirmationFocus == i {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Bold(true).Render("> " + line[2:]))
			} else if p.mergeConfirmCheckboxHover == i+1 {
				// Hover state (subtle highlight)
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Render(line))
			} else {
				sb.WriteString(line)
			}
			sb.WriteString("\n")
			sb.WriteString(dimText("      " + opt.hint))
			sb.WriteString("\n")
		}

		sb.WriteString("\n")
		sb.WriteString(strings.Repeat("─", min(modalW-4, 60)))
		sb.WriteString("\n\n")

		// Pull section
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Sync Local Branch"))
		sb.WriteString("\n")

		// Pull checkbox
		pullCheckbox := "[ ]"
		if p.mergeState.PullAfterMerge {
			pullCheckbox = "[x]"
		}
		pullLabel := fmt.Sprintf("Update local '%s' from remote", baseBranch)
		pullLine := fmt.Sprintf("  %s %s", pullCheckbox, pullLabel)
		if p.mergeState.ConfirmationFocus == 3 {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Bold(true).Render("> " + pullLine[2:]))
		} else if p.mergeConfirmCheckboxHover == 4 {
			// Hover state (subtle highlight)
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Render(pullLine))
		} else {
			sb.WriteString(pullLine)
		}
		sb.WriteString("\n")
		if p.mergeState.CurrentBranch != "" {
			sb.WriteString(dimText(fmt.Sprintf("      Current branch: %s", p.mergeState.CurrentBranch)))
		} else {
			sb.WriteString(dimText(fmt.Sprintf("      Updates local %s to include merged PR", baseBranch)))
		}
		sb.WriteString("\n\n")

		// Buttons (now at focus indices 4 and 5)
		confirmLabel := " Clean Up "
		skipLabel := " Skip All "

		confirmStyle := lipgloss.NewStyle().Background(lipgloss.Color("62")).Foreground(lipgloss.Color("0"))
		skipStyle := lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255"))

		if p.mergeState.ConfirmationFocus == 4 {
			confirmStyle = confirmStyle.Bold(true).Background(lipgloss.Color("42"))
		} else if p.mergeConfirmButtonHover == 1 {
			// Hover state for Clean Up button
			confirmStyle = confirmStyle.Background(lipgloss.Color("75"))
		}
		if p.mergeState.ConfirmationFocus == 5 {
			skipStyle = skipStyle.Bold(true).Background(lipgloss.Color("214"))
		} else if p.mergeConfirmButtonHover == 2 {
			// Hover state for Skip All button
			skipStyle = skipStyle.Background(lipgloss.Color("245"))
		}

		sb.WriteString("  ")
		sb.WriteString(confirmStyle.Render(confirmLabel))
		sb.WriteString("  ")
		sb.WriteString(skipStyle.Render(skipLabel))
		sb.WriteString("\n\n")

		sb.WriteString(dimText("↑/↓: navigate  space: toggle  enter: confirm  esc: cancel"))

	case MergeStepCleanup:
		sb.WriteString("Cleaning up worktree and branch...")

	case MergeStepDone:
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")).Render("Merge workflow complete!"))
		sb.WriteString("\n\n")

		// Show cleanup summary
		if p.mergeState.CleanupResults != nil {
			results := p.mergeState.CleanupResults
			sb.WriteString("Summary:\n")

			successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
			if results.LocalWorktreeDeleted {
				sb.WriteString(successStyle.Render("  ✓ Local worktree deleted"))
				sb.WriteString("\n")
			}
			if results.LocalBranchDeleted {
				sb.WriteString(successStyle.Render("  ✓ Local branch deleted"))
				sb.WriteString("\n")
			}
			if results.RemoteBranchDeleted {
				sb.WriteString(successStyle.Render("  ✓ Remote branch deleted"))
				sb.WriteString("\n")
			}
			if results.PullAttempted {
				if results.PullSuccess {
					sb.WriteString(successStyle.Render("  ✓ Pulled latest changes"))
					sb.WriteString("\n")
				} else if results.PullError != nil {
					sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("  ⚠ Pull failed: " + results.PullError.Error()))
					sb.WriteString("\n")
				}
			}

			// Show any errors/warnings
			if len(results.Errors) > 0 {
				sb.WriteString("\n")
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("Warnings:"))
				sb.WriteString("\n")
				for _, err := range results.Errors {
					sb.WriteString(dimText("  • " + err))
					sb.WriteString("\n")
				}
			}
		} else {
			// No cleanup was performed
			sb.WriteString("No cleanup performed. Worktree and branches remain.")
		}

		sb.WriteString("\n\n")
		sb.WriteString(dimText("Press Enter to close"))
	}

	// Show error if any
	if p.mergeState.Error != nil {
		sb.WriteString("\n\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(
			fmt.Sprintf("Error: %s", p.mergeState.Error.Error())))
	}

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Register hit regions for merge method options during MergeStepMergeMethod
	if p.mergeState.Step == MergeStepMergeMethod {
		modalH := lipgloss.Height(modal)
		modalX := (width - modalW) / 2
		modalY := (height - modalH) / 2

		// Calculate position of merge method radio options
		// Border offset is 2: border(1) + padding(1)
		// Content structure before step content:
		// - Title (1 line)
		// - blank (1 line)
		// - Progress steps: 5 if UseDirectMerge, 7 otherwise
		// - blank (1 line)
		// - separator (1 line)
		// - blank (2 lines from \n\n)
		progressSteps := 7
		if p.mergeState.UseDirectMerge {
			progressSteps = 5
		}
		// Lines before step content: 1 + 1 + progressSteps + 1 + 1 + 2 = progressSteps + 6
		// Then "Choose Merge Method:" (1) + blank (1) = 2 more lines before options
		option1Y := modalY + 2 + progressSteps + 6 + 2 // border(1) + padding(1) + header content + step header + blank
		option2Y := option1Y + 3                        // Option 1 text + description + blank

		p.mouseHandler.HitMap.AddRect(regionMergeMethodOption, modalX+2, option1Y, modalW-6, 1, 0) // Create PR
		p.mouseHandler.HitMap.AddRect(regionMergeMethodOption, modalX+2, option2Y, modalW-6, 1, 1) // Direct Merge
	}

	// Register hit regions for radio buttons during MergeStepWaitingMerge
	if p.mergeState.Step == MergeStepWaitingMerge {
		modalH := lipgloss.Height(modal)
		modalX := (width - modalW) / 2
		modalY := (height - modalH) / 2

		// Radio buttons position calculated from bottom of content
		// Border offset is 2: border(1) + padding(1)
		contentLines := strings.Count(content, "\n")
		// Radio buttons are approximately at contentLines - 7 (from bottom)
		// "Delete worktree" and "Keep worktree" options
		radio1Y := modalY + 2 + contentLines - 7
		radio2Y := radio1Y + 1
		p.mouseHandler.HitMap.AddRect(regionMergeRadio, modalX+2, radio1Y, modalW-6, 1, 0) // Delete
		p.mouseHandler.HitMap.AddRect(regionMergeRadio, modalX+2, radio2Y, modalW-6, 1, 1) // Keep
	}

	// Register hit regions for post-merge confirmation step
	if p.mergeState.Step == MergeStepPostMergeConfirmation {
		modalH := lipgloss.Height(modal)
		modalX := (width - modalW) / 2
		modalY := (height - modalH) / 2

		// Calculate positions based on content structure
		// Title(1) + blank(1) + separator(1) + blank(1) + header(1) + subtext(1) + blank(1) = 7 lines before checkboxes
		// Border offset is 2: border(1) + padding(1)
		checkboxBaseY := modalY + 2 + 7 // border(1) + padding(1) + content lines to checkboxes

		// Three cleanup checkbox options (2 lines each: checkbox + hint)
		for i := 0; i < 3; i++ {
			p.mouseHandler.HitMap.AddRect(
				regionMergeConfirmCheckbox,
				modalX+2,
				checkboxBaseY+(i*2),
				modalW-6,
				1,
				i,
			)
		}

		// Pull checkbox (focus index 3)
		// After 3 cleanup checkboxes (6 lines) + blank(1) + separator(1) + blank(1) + header(1) = 10 lines
		pullCheckboxY := checkboxBaseY + 10
		p.mouseHandler.HitMap.AddRect(regionMergeConfirmCheckbox, modalX+2, pullCheckboxY, modalW-6, 1, 3)

		// Button hit regions (after pull checkbox + hint + blank)
		buttonY := pullCheckboxY + 3
		p.mouseHandler.HitMap.AddRect(regionMergeConfirmButton, modalX+4, buttonY, 12, 1, nil)
		p.mouseHandler.HitMap.AddRect(regionMergeSkipButton, modalX+18, buttonY, 12, 1, nil)
	}

	// Use OverlayModal for dimmed background effect
	return ui.OverlayModal(background, modal, width, height)
}

// renderCommitForMergeModal renders the commit-before-merge modal.
func (p *Plugin) renderCommitForMergeModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	if p.mergeCommitState == nil {
		return background
	}

	// Modal dimensions
	modalW := 60
	if modalW > width-4 {
		modalW = width - 4
	}

	// Calculate input field width
	inputW := modalW - 10
	if inputW < 20 {
		inputW = 20
	}

	// Set textinput width and remove default prompt
	p.mergeCommitMessageInput.Width = inputW
	p.mergeCommitMessageInput.Prompt = ""

	wt := p.mergeCommitState.Worktree

	var sb strings.Builder
	title := "Uncommitted Changes"
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")).Render(title))
	sb.WriteString("\n\n")

	// Worktree info
	sb.WriteString(fmt.Sprintf("Worktree: %s\n", lipgloss.NewStyle().Bold(true).Render(wt.Name)))
	sb.WriteString(fmt.Sprintf("Branch:   %s\n", wt.Branch))
	sb.WriteString("\n")

	// Change counts
	sb.WriteString("Changes to commit:\n")
	if p.mergeCommitState.StagedCount > 0 {
		sb.WriteString(fmt.Sprintf("  • %d staged file(s)\n", p.mergeCommitState.StagedCount))
	}
	if p.mergeCommitState.ModifiedCount > 0 {
		sb.WriteString(fmt.Sprintf("  • %d modified file(s)\n", p.mergeCommitState.ModifiedCount))
	}
	if p.mergeCommitState.UntrackedCount > 0 {
		sb.WriteString(fmt.Sprintf("  • %d untracked file(s)\n", p.mergeCommitState.UntrackedCount))
	}
	sb.WriteString("\n")

	// Info message
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(infoStyle.Render("You must commit these changes before creating a PR."))
	sb.WriteString("\n")
	sb.WriteString(infoStyle.Render("All changes will be staged and committed."))
	sb.WriteString("\n\n")

	// Commit message field
	sb.WriteString("Commit message:\n")
	sb.WriteString(inputFocusedStyle.Render(p.mergeCommitMessageInput.View()))
	sb.WriteString("\n")

	// Display error if present
	if p.mergeCommitState.Error != "" {
		sb.WriteString("\n")
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		sb.WriteString(errStyle.Render("Error: " + p.mergeCommitState.Error))
	}

	sb.WriteString("\n\n")
	sb.WriteString(dimText("Enter to commit and continue • Esc to cancel"))

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Use OverlayModal for dimmed background effect
	return ui.OverlayModal(background, modal, width, height)
}

// renderTypeSelectorModal renders the type selector modal (Shell vs Worktree).
func (p *Plugin) renderTypeSelectorModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	// Modal dimensions - minimal width for quick selection
	modalW := 32
	if modalW > width-4 {
		modalW = width - 4
	}

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Create New"))
	sb.WriteString("\n\n")

	// Options
	options := []string{"Shell", "Worktree"}
	for i, opt := range options {
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("245")) // dim

		if i == p.typeSelectorIdx {
			prefix = "> "
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Bold(true) // highlight
		} else if i+1 == p.typeSelectorHover {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("252")) // hover
		}

		sb.WriteString(style.Render(prefix + opt))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(dimText("↑/↓ select  Enter confirm  Esc cancel"))

	content := sb.String()
	modal := modalStyle.Width(modalW).Render(content)

	// Calculate modal position for hit regions
	modalHeight := lipgloss.Height(modal)
	modalX := (width - modalW) / 2
	modalY := (height - modalHeight) / 2

	// Register hit regions for options (inside modal content)
	// Content starts at modalY + 2 (border + padding) + 2 (title + blank line)
	optionStartY := modalY + 4
	optionW := modalW - 6 // Subtract border + padding

	for i := range options {
		p.mouseHandler.HitMap.AddRect(regionTypeSelectorOption, modalX+3, optionStartY+i, optionW, 1, i)
	}

	return ui.OverlayModal(background, modal, width, height)
}
