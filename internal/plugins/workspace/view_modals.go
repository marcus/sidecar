package workspace

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// renderCreateModal renders the new worktree modal with dimmed background.
func (p *Plugin) renderCreateModal(width, height int) string {
	background := p.renderListView(width, height)

	p.ensureCreateModal()
	if p.createModal == nil {
		return background
	}
	p.syncCreateModalFocus()

	modalContent := p.createModal.Render(width, height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
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
	searchStyle := inputFocusedStyle()
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
				sb.WriteString(lipgloss.NewStyle().Foreground(styles.Primary).Render(line))
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
				sb.WriteString(lipgloss.NewStyle().Foreground(styles.Primary).Render(line))
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
	modal := modalStyle().Width(modalW).Render(content)

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

	p.ensureConfirmDeleteModal()
	if p.deleteConfirmModal == nil {
		return background
	}

	modalContent := p.deleteConfirmModal.Render(width, height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}

const (
	deleteConfirmLocalID  = "delete-confirm-local-branch"
	deleteConfirmRemoteID = "delete-confirm-remote-branch"
	deleteConfirmDeleteID = "delete-confirm-delete"
	deleteConfirmCancelID = "delete-confirm-cancel"
)

// ensureConfirmDeleteModal builds/rebuilds the delete confirmation modal.
func (p *Plugin) ensureConfirmDeleteModal() {
	if p.deleteConfirmWorktree == nil {
		return
	}

	modalW := 58
	if modalW > p.width-4 {
		modalW = p.width - 4
	}
	if modalW < 20 {
		modalW = 20
	}

	if p.deleteConfirmModal != nil && p.deleteConfirmModalWidth == modalW {
		return
	}
	p.deleteConfirmModalWidth = modalW

	p.deleteConfirmModal = modal.New("Delete Worktree?",
		modal.WithWidth(modalW),
		modal.WithVariant(modal.VariantDanger),
		modal.WithHints(false),
	).
		AddSection(p.deleteConfirmInfoSection()).
		AddSection(modal.Spacer()).
		AddSection(p.deleteConfirmWarningSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.When(func() bool { return !p.deleteIsMainBranch }, p.deleteConfirmBranchHeaderSection())).
		AddSection(modal.When(func() bool { return !p.deleteIsMainBranch }, modal.Checkbox(deleteConfirmLocalID, "Delete local branch", &p.deleteLocalBranchOpt))).
		AddSection(modal.When(func() bool { return !p.deleteIsMainBranch }, p.deleteConfirmLocalHintSection())).
		AddSection(modal.When(func() bool { return !p.deleteIsMainBranch && p.deleteHasRemote }, modal.Checkbox(deleteConfirmRemoteID, "Delete remote branch", &p.deleteRemoteBranchOpt))).
		AddSection(modal.When(func() bool { return !p.deleteIsMainBranch && p.deleteHasRemote }, p.deleteConfirmRemoteHintSection())).
		AddSection(modal.When(func() bool { return !p.deleteIsMainBranch }, modal.Spacer())).
		AddSection(modal.Buttons(
			modal.Btn(" Delete ", deleteConfirmDeleteID, modal.BtnDanger()),
			modal.Btn(" Cancel ", deleteConfirmCancelID),
		))
}

func (p *Plugin) deleteConfirmInfoSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.deleteConfirmWorktree == nil {
			return modal.RenderedSection{}
		}

		wt := p.deleteConfirmWorktree
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Name:   %s\n", lipgloss.NewStyle().Bold(true).Render(wt.Name)))
		sb.WriteString(fmt.Sprintf("Branch: %s\n", wt.Branch))
		sb.WriteString(fmt.Sprintf("Path:   %s", dimText(wt.Path)))

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

func (p *Plugin) deleteConfirmWarningSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		warningStyle := lipgloss.NewStyle().Foreground(styles.Warning)

		var sb strings.Builder
		if p.deleteConfirmWorktree != nil && p.deleteConfirmWorktree.IsMissing {
			sb.WriteString(warningStyle.Render("This will:"))
			sb.WriteString("\n")
			sb.WriteString(dimText("  • Directory already removed"))
			sb.WriteString("\n")
			sb.WriteString(dimText("  • Clean up git worktree metadata"))
		} else {
			sb.WriteString(warningStyle.Render("This will:"))
			sb.WriteString("\n")
			sb.WriteString(dimText("  • Remove the working directory"))
			sb.WriteString("\n")
			sb.WriteString(dimText("  • Uncommitted changes will be lost"))
		}

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

func (p *Plugin) deleteConfirmBranchHeaderSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		content := lipgloss.NewStyle().Bold(true).Render("Branch Cleanup (Optional)")
		return modal.RenderedSection{Content: content}
	}, nil)
}

func (p *Plugin) deleteConfirmLocalHintSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.deleteConfirmWorktree == nil {
			return modal.RenderedSection{}
		}
		wt := p.deleteConfirmWorktree
		return modal.RenderedSection{Content: dimText("  Removes '" + wt.Branch + "' locally")}
	}, nil)
}

func (p *Plugin) deleteConfirmRemoteHintSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.deleteConfirmWorktree == nil {
			return modal.RenderedSection{}
		}
		wt := p.deleteConfirmWorktree
		return modal.RenderedSection{Content: dimText("  Removes 'origin/" + wt.Branch + "'")}
	}, nil)
}

const (
	deleteShellConfirmDeleteID = "delete-shell-confirm-delete"
	deleteShellConfirmCancelID = "delete-shell-confirm-cancel"
)

const (
	commitForMergeInputID  = "commit-for-merge-input"
	commitForMergeCommitID = "commit-for-merge-commit"
	commitForMergeCancelID = "commit-for-merge-cancel"
	commitForMergeActionID = "commit-for-merge-action"
)

// renderConfirmDeleteShellModal renders the shell delete confirmation modal.
func (p *Plugin) renderConfirmDeleteShellModal(width, height int) string {
	// Render the background (list view)
	background := p.renderListView(width, height)

	p.ensureConfirmDeleteShellModal()
	if p.deleteShellModal == nil {
		return background
	}

	modalContent := p.deleteShellModal.Render(width, height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}

// ensureConfirmDeleteShellModal builds/rebuilds the shell delete confirmation modal.
func (p *Plugin) ensureConfirmDeleteShellModal() {
	if p.deleteConfirmShell == nil {
		return
	}

	modalW := 50
	if modalW > p.width-4 {
		modalW = p.width - 4
	}
	if modalW < 20 {
		modalW = 20
	}

	if p.deleteShellModal != nil && p.deleteShellModalWidth == modalW {
		return
	}
	p.deleteShellModalWidth = modalW

	p.deleteShellModal = modal.New("Delete Shell?",
		modal.WithWidth(modalW),
		modal.WithVariant(modal.VariantDanger),
		modal.WithHints(false),
	).
		AddSection(p.deleteShellInfoSection()).
		AddSection(modal.Spacer()).
		AddSection(p.deleteShellWarningSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Delete ", deleteShellConfirmDeleteID, modal.BtnDanger()),
			modal.Btn(" Cancel ", deleteShellConfirmCancelID),
		))
}

func (p *Plugin) deleteShellInfoSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.deleteConfirmShell == nil {
			return modal.RenderedSection{}
		}
		shell := p.deleteConfirmShell

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Name:    %s\n", lipgloss.NewStyle().Bold(true).Render(shell.Name)))
		sb.WriteString(fmt.Sprintf("Session: %s", dimText(shell.TmuxName)))

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

func (p *Plugin) deleteShellWarningSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		warningStyle := lipgloss.NewStyle().Foreground(styles.Warning)

		var sb strings.Builder
		sb.WriteString(warningStyle.Render("This will:"))
		sb.WriteString("\n")
		sb.WriteString(dimText("  • Terminate the tmux session"))
		sb.WriteString("\n")
		sb.WriteString(dimText("  • Any running processes will be killed"))

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

const (
	renameShellInputID  = "rename-shell-input"
	renameShellRenameID = "rename-shell-rename"
	renameShellCancelID = "rename-shell-cancel"
	renameShellActionID = "rename-shell-action"
)

// ensureRenameShellModal builds/rebuilds the rename shell modal.
func (p *Plugin) ensureRenameShellModal() {
	if p.renameShellSession == nil {
		return
	}

	modalW := 50
	if modalW > p.width-4 {
		modalW = p.width - 4
	}
	if modalW < 20 {
		modalW = 20
	}

	// Only rebuild if modal doesn't exist or width changed
	if p.renameShellModal != nil && p.renameShellModalWidth == modalW {
		return
	}
	p.renameShellModalWidth = modalW

	p.renameShellModal = modal.New("Rename Shell",
		modal.WithWidth(modalW),
		modal.WithPrimaryAction(renameShellActionID),
		modal.WithHints(false),
	).
		AddSection(p.renameShellInfoSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.InputWithLabel(renameShellInputID, "New Name:", &p.renameShellInput)).
		AddSection(modal.When(func() bool { return p.renameShellError != "" }, p.renameShellErrorSection())).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Rename ", renameShellRenameID),
			modal.Btn(" Cancel ", renameShellCancelID),
		))
}

// renameShellInfoSection renders the shell info section.
func (p *Plugin) renameShellInfoSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.renameShellSession == nil {
			return modal.RenderedSection{}
		}

		shell := p.renameShellSession
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Session: %s\n", dimText(shell.TmuxName)))
		sb.WriteString(fmt.Sprintf("Current: %s", lipgloss.NewStyle().Bold(true).Render(shell.Name)))

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

// renameShellErrorSection renders the error message section.
func (p *Plugin) renameShellErrorSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.renameShellError == "" {
			return modal.RenderedSection{}
		}

		errStyle := lipgloss.NewStyle().Foreground(styles.Error)
		content := errStyle.Render("Error: " + p.renameShellError)

		return modal.RenderedSection{Content: content}
	}, nil)
}

// renderRenameShellModal renders the rename shell modal.
func (p *Plugin) renderRenameShellModal(width, height int) string {
	background := p.renderListView(width, height)

	p.ensureRenameShellModal()
	if p.renameShellModal == nil {
		return background
	}

	modalContent := p.renameShellModal.Render(width, height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}

// renderPromptPickerModal renders the prompt picker modal.
func (p *Plugin) renderPromptPickerModal(width, height int) string {
	// Render the background (create modal behind it)
	background := p.renderCreateModal(width, height)

	p.ensurePromptPickerModal()
	if p.promptPickerModal == nil {
		return background
	}

	modalContent := p.promptPickerModal.Render(width, height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}

// ensureAgentChoiceModal builds/rebuilds the agent choice modal.
func (p *Plugin) ensureAgentChoiceModal() {
	if p.agentChoiceWorktree == nil {
		return
	}

	modalW := 50
	if p.width > 0 && modalW > p.width-4 {
		modalW = p.width - 4
	}
	if modalW < 20 {
		modalW = 20
	}

	// Only rebuild if modal doesn't exist or width changed
	if p.agentChoiceModal != nil && p.agentChoiceModalWidth == modalW {
		return
	}
	p.agentChoiceModalWidth = modalW

	// Build list items for the options
	items := []modal.ListItem{
		{ID: "agent-choice-attach", Label: "Attach to session"},
		{ID: "agent-choice-restart", Label: "Restart agent"},
	}

	title := fmt.Sprintf("Agent Running: %s", p.agentChoiceWorktree.Name)

	p.agentChoiceModal = modal.New(title,
		modal.WithWidth(modalW),
		modal.WithPrimaryAction(agentChoiceActionID),
		modal.WithHints(false),
	).
		AddSection(modal.Text("An agent is already running on this worktree.\nWhat would you like to do?")).
		AddSection(modal.Spacer()).
		AddSection(modal.List(agentChoiceListID, items, &p.agentChoiceIdx, modal.WithMaxVisible(2))).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Confirm ", agentChoiceConfirmID),
			modal.Btn(" Cancel ", agentChoiceCancelID),
		))
}

// renderAgentChoiceModal renders the agent action choice modal.
func (p *Plugin) renderAgentChoiceModal(width, height int) string {
	background := p.renderListView(width, height)

	p.ensureAgentChoiceModal()
	if p.agentChoiceModal == nil {
		return background
	}

	modalContent := p.agentChoiceModal.Render(width, height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}

// clearAgentChoiceModal clears agent choice modal state.
func (p *Plugin) clearAgentChoiceModal() {
	p.agentChoiceWorktree = nil
	p.agentChoiceIdx = 0
	p.agentChoiceModal = nil
	p.agentChoiceModalWidth = 0
}

// ensureMergeModal builds/rebuilds the merge workflow modal.
func (p *Plugin) ensureMergeModal() {
	if p.mergeState == nil {
		return
	}

	modalW := 70
	if p.width > 0 && modalW > p.width-4 {
		modalW = p.width - 4
	}
	if modalW < 30 {
		modalW = 30
	}

	// Only rebuild if modal doesn't exist, width changed, or step changed
	if p.mergeModal != nil && p.mergeModalWidth == modalW && p.mergeModalStep == p.mergeState.Step {
		return
	}
	p.mergeModalWidth = modalW
	p.mergeModalStep = p.mergeState.Step

	title := fmt.Sprintf("Merge Workflow: %s", p.mergeState.Worktree.Name)

	// Determine primary action based on current step
	var primaryAction string
	switch p.mergeState.Step {
	case MergeStepTargetBranch:
		primaryAction = mergeTargetActionID
	case MergeStepMergeMethod:
		primaryAction = mergeMethodActionID
	case MergeStepPostMergeConfirmation:
		primaryAction = mergeCleanUpButtonID
	}

	// Build modal based on current step
	opts := []modal.Option{
		modal.WithWidth(modalW),
		modal.WithHints(false),
		modal.WithCloseOnBackdropClick(false),
	}
	if primaryAction != "" {
		opts = append(opts, modal.WithPrimaryAction(primaryAction))
	}
	m := modal.New(title, opts...)

	// Add progress indicator section (always shown)
	m.AddSection(p.mergeProgressSection())
	m.AddSection(modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		return modal.RenderedSection{Content: strings.Repeat("─", min(contentWidth, 60))}
	}, nil))
	m.AddSection(modal.Spacer())

	// Add step-specific sections
	switch p.mergeState.Step {
	case MergeStepReviewDiff:
		m.AddSection(p.mergeReviewDiffSection())
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Text(dimText("Press Enter to continue, Esc to cancel")))

	case MergeStepTargetBranch:
		m.AddSection(modal.Text(lipgloss.NewStyle().Bold(true).Render("Target Branch:")))
		m.AddSection(modal.Spacer())
		if len(p.mergeState.TargetBranches) > 0 {
			items := make([]modal.ListItem, len(p.mergeState.TargetBranches))
			for i, b := range p.mergeState.TargetBranches {
				label := b
				if i == 0 {
					label = b + " (default)"
				}
				items[i] = modal.ListItem{ID: "branch-" + b, Label: label}
			}
			maxVis := len(items)
			if maxVis > 8 {
				maxVis = 8
			}
			m.AddSection(modal.List(mergeTargetListID, items, &p.mergeState.TargetBranchOption, modal.WithMaxVisible(maxVis)))
		} else {
			m.AddSection(modal.Text("Loading branches..."))
		}
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Text(dimText("↑/↓: select   Enter: continue   Esc: cancel")))

	case MergeStepMergeMethod:
		m.AddSection(modal.Text(lipgloss.NewStyle().Bold(true).Render("Choose Merge Method:")))
		m.AddSection(modal.Spacer())
		items := []modal.ListItem{
			{ID: "merge-pr", Label: "Create Pull Request (Recommended)"},
			{ID: "merge-direct", Label: "Direct Merge"},
		}
		m.AddSection(modal.List(mergeMethodListID, items, &p.mergeState.MergeMethodOption, modal.WithMaxVisible(2)))
		m.AddSection(modal.Spacer())
		m.AddSection(p.mergeMethodHintsSection())
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Text(dimText("↑/↓: select   Enter: continue   Esc: cancel")))

	case MergeStepDirectMerge:
		m.AddSection(modal.Text("Merging directly to base branch..."))
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Text(dimText(fmt.Sprintf("Merging '%s' into '%s'...", p.mergeState.Worktree.Branch, p.mergeState.TargetBranch))))

	case MergeStepPush:
		m.AddSection(modal.Text("Pushing branch to remote..."))

	case MergeStepCreatePR:
		m.AddSection(modal.Text("Creating pull request..."))

	case MergeStepWaitingMerge:
		m.AddSection(p.mergeWaitingSection())
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Text(dimText("Enter: check now   o: open PR   Esc: exit   ↑/↓: change option")))

	case MergeStepPostMergeConfirmation:
		m.AddSection(p.mergePostMergeHeaderSection())
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Text(lipgloss.NewStyle().Bold(true).Render("Cleanup Options")))
		m.AddSection(modal.Text(dimText("Select what to clean up:")))
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Checkbox(mergeConfirmWorktreeID, "Delete local worktree", &p.mergeState.DeleteLocalWorktree))
		m.AddSection(modal.Text(dimText("  Removes " + p.mergeState.Worktree.Path)))
		m.AddSection(modal.Checkbox(mergeConfirmBranchID, "Delete local branch", &p.mergeState.DeleteLocalBranch))
		m.AddSection(modal.Text(dimText("  Removes '" + p.mergeState.Worktree.Branch + "' locally")))
		m.AddSection(modal.Checkbox(mergeConfirmRemoteID, "Delete remote branch", &p.mergeState.DeleteRemoteBranch))
		m.AddSection(modal.Text(dimText("  Removes from GitHub (often auto-deleted)")))
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
			return modal.RenderedSection{Content: strings.Repeat("─", min(contentWidth, 60))}
		}, nil))
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Text(lipgloss.NewStyle().Bold(true).Render("Sync Local Branch")))
		m.AddSection(modal.Checkbox(mergeConfirmPullID, fmt.Sprintf("Update local '%s' from remote", p.mergeState.TargetBranch), &p.mergeState.PullAfterMerge))
		if p.mergeState.CurrentBranch != "" {
			m.AddSection(modal.Text(dimText(fmt.Sprintf("  Current branch: %s", p.mergeState.CurrentBranch))))
		} else {
			m.AddSection(modal.Text(dimText(fmt.Sprintf("  Updates local %s to include merged PR", p.mergeState.TargetBranch))))
		}
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Buttons(
			modal.Btn(" Clean Up ", mergeCleanUpButtonID),
			modal.Btn(" Skip All ", mergeSkipButtonID),
		))
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Text(dimText("↑/↓: navigate  space: toggle  enter: confirm  esc: cancel")))

	case MergeStepCleanup:
		m.AddSection(modal.Text("Cleaning up worktree and branch..."))

	case MergeStepDone:
		m.AddSection(p.mergeDoneSection())

	case MergeStepError:
		// Override with danger-variant modal for prominent error display
		m = modal.New(p.mergeState.ErrorTitle,
			modal.WithWidth(modalW),
			modal.WithVariant(modal.VariantDanger),
			modal.WithHints(false),
			modal.WithCloseOnBackdropClick(false),
		)
		m.AddSection(p.mergeProgressSection())
		m.AddSection(modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
			return modal.RenderedSection{Content: strings.Repeat("─", min(contentWidth, 60))}
		}, nil))
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Text(lipgloss.NewStyle().Foreground(styles.Error).Bold(true).Render("Error Output:")))
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Text(p.mergeState.ErrorDetail))
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Buttons(modal.Btn(" Dismiss ", "dismiss")))
		m.AddSection(modal.Spacer())
		m.AddSection(modal.Text(dimText("y: copy error   Esc: dismiss")))
	}

	p.mergeModal = m
}

// mergeProgressSection renders the progress indicators for all steps.
func (p *Plugin) mergeProgressSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.mergeState == nil {
			return modal.RenderedSection{}
		}

		var sb strings.Builder
		// Determine which steps to show based on merge method
		var steps []MergeWorkflowStep
		if p.mergeState.UseDirectMerge {
			steps = []MergeWorkflowStep{
				MergeStepReviewDiff,
				MergeStepTargetBranch,
				MergeStepMergeMethod,
				MergeStepDirectMerge,
				MergeStepPostMergeConfirmation,
				MergeStepCleanup,
			}
		} else {
			steps = []MergeWorkflowStep{
				MergeStepReviewDiff,
				MergeStepTargetBranch,
				MergeStepMergeMethod,
				MergeStepPush,
				MergeStepCreatePR,
				MergeStepWaitingMerge,
				MergeStepPostMergeConfirmation,
				MergeStepCleanup,
			}
		}

		for i, step := range steps {
			status := p.mergeState.StepStatus[step]
			icon := "○" // pending
			color := styles.TextMuted

			switch status {
			case "running":
				icon = "●"
				color = styles.Warning
			case "done":
				icon = "✓"
				color = styles.Success
			case "error":
				icon = "✗"
				color = styles.Error
			case "skipped":
				icon = "○"
				color = styles.TextMuted
			}

			stepName := step.String()
			if step == p.mergeState.Step {
				stepName = lipgloss.NewStyle().Bold(true).Render(stepName)
			}

			stepLine := fmt.Sprintf("  %s %s",
				lipgloss.NewStyle().Foreground(color).Render(icon),
				stepName,
			)
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(stepLine)
		}

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

// mergeReviewDiffSection renders the diff summary for review.
func (p *Plugin) mergeReviewDiffSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.mergeState == nil {
			return modal.RenderedSection{}
		}

		var sb strings.Builder
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Files Changed:"))
		sb.WriteString("\n\n")

		if p.mergeState.StepStatus[MergeStepReviewDiff] == "running" {
			sb.WriteString(dimText("Loading..."))
		} else if p.mergeState.DiffSummary != "" {
			summaryLines := strings.Split(p.mergeState.DiffSummary, "\n")
			maxLines := 15
			if len(summaryLines) > maxLines {
				summaryLines = summaryLines[:maxLines]
				summaryLines = append(summaryLines, fmt.Sprintf("... (%d more files)", len(strings.Split(p.mergeState.DiffSummary, "\n"))-maxLines))
			}
			for _, line := range summaryLines {
				sb.WriteString(p.colorStatLine(line, contentWidth))
				sb.WriteString("\n")
			}
		} else {
			sb.WriteString(dimText("No files changed"))
		}

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

// mergeMethodHintsSection renders hints for the merge method options.
func (p *Plugin) mergeMethodHintsSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.mergeState == nil {
			return modal.RenderedSection{}
		}

		var sb strings.Builder

		if p.mergeState.MergeMethodOption == 0 {
			sb.WriteString(dimText("Push to origin and create a GitHub PR for review"))
		} else {
			sb.WriteString(dimText(fmt.Sprintf("Merge directly to '%s' without PR", p.mergeState.TargetBranch)))
			sb.WriteString("\n")
			sb.WriteString(lipgloss.NewStyle().Foreground(styles.Warning).Render("Warning: Bypasses code review"))
		}

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

// mergeWaitingSection renders the waiting for merge step content.
func (p *Plugin) mergeWaitingSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.mergeState == nil {
			return modal.RenderedSection{}
		}

		var sb strings.Builder
		if p.mergeState.ExistingPR {
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.Warning).Render("Using Existing Pull Request"))
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
		sb.WriteString(strings.Repeat("─", min(contentWidth, 60)))
		sb.WriteString("\n\n")

		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("After merge:"))
		sb.WriteString("\n\n")

		// Radio options
		if p.mergeState.DeleteAfterMerge {
			sb.WriteString(lipgloss.NewStyle().Foreground(styles.Primary).Render(" ● Delete worktree after merge"))
		} else {
			sb.WriteString(dimText(" ○ Delete worktree after merge"))
		}
		sb.WriteString("\n")
		if !p.mergeState.DeleteAfterMerge {
			sb.WriteString(lipgloss.NewStyle().Foreground(styles.Primary).Render(" ● Keep worktree"))
		} else {
			sb.WriteString(dimText(" ○ Keep worktree"))
		}
		sb.WriteString("\n\n")
		sb.WriteString(dimText(" (This takes effect only once the PR is merged)"))

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

// mergePostMergeHeaderSection renders the header for post-merge confirmation.
func (p *Plugin) mergePostMergeHeaderSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		mergeMethod := "PR Merged"
		if p.mergeState != nil && p.mergeState.UseDirectMerge {
			mergeMethod = "Direct Merge Complete"
		}
		header := lipgloss.NewStyle().Bold(true).Foreground(styles.Success).Render(mergeMethod + "!")
		separator := strings.Repeat("─", min(contentWidth, 60))
		return modal.RenderedSection{Content: header + "\n\n" + separator}
	}, nil)
}

// mergeDoneSection renders the completion summary.
func (p *Plugin) mergeDoneSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.mergeState == nil {
			return modal.RenderedSection{}
		}

		var sb strings.Builder
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.Success).Render("Merge workflow complete!"))
		sb.WriteString("\n\n")

		if p.mergeState.CleanupResults != nil {
			results := p.mergeState.CleanupResults
			sb.WriteString("Summary:\n")

			successStyle := lipgloss.NewStyle().Foreground(styles.Success)
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
					warnStyle := lipgloss.NewStyle().Foreground(styles.Warning)
					errorStyle := lipgloss.NewStyle().Foreground(styles.Error)

					sb.WriteString(warnStyle.Render("  ⚠ Pull failed: "))
					sb.WriteString(errorStyle.Render(results.PullErrorSummary))
					sb.WriteString("\n")

					if results.ShowErrorDetails {
						sb.WriteString("\n")
						sb.WriteString(dimText("  Details:"))
						sb.WriteString("\n")
						allDetailLines := strings.Split(results.PullErrorFull, "\n")
						maxDetailLines := 10
						detailLines := allDetailLines
						if len(allDetailLines) > maxDetailLines {
							detailLines = allDetailLines[:maxDetailLines]
						}
						for _, line := range detailLines {
							if line = strings.TrimSpace(line); line != "" {
								sb.WriteString(dimText("    " + line))
								sb.WriteString("\n")
							}
						}
						if len(allDetailLines) > maxDetailLines {
							sb.WriteString(dimText(fmt.Sprintf("    ... (%d more lines)", len(allDetailLines)-maxDetailLines)))
							sb.WriteString("\n")
						}
						sb.WriteString("\n")
						sb.WriteString(dimText("  Press 'd' to hide details"))
					} else {
						sb.WriteString(dimText("  Press 'd' for full error details"))
					}
					sb.WriteString("\n")

					if results.BranchDiverged {
						sb.WriteString("\n")
						sb.WriteString(strings.Repeat("─", min(contentWidth, 60)))
						sb.WriteString("\n\n")
						sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Resolution Options"))
						sb.WriteString("\n")
						sb.WriteString(dimText(fmt.Sprintf("  Your local '%s' has diverged from remote.", results.BaseBranch)))
						sb.WriteString("\n\n")
						sb.WriteString(dimText("    [r] Rebase local onto remote"))
						sb.WriteString("\n")
						sb.WriteString(dimText("        Replay your local commits on top of remote changes"))
						sb.WriteString("\n\n")
						sb.WriteString(dimText("    [m] Merge remote into local"))
						sb.WriteString("\n")
						sb.WriteString(dimText("        Creates a merge commit combining both histories"))
						sb.WriteString("\n")
					}
				}
			}

			if len(results.Errors) > 0 {
				sb.WriteString("\n")
				sb.WriteString(lipgloss.NewStyle().Foreground(styles.Warning).Render("Warnings:"))
				sb.WriteString("\n")
				for _, err := range results.Errors {
					sb.WriteString(dimText("  • " + err))
					sb.WriteString("\n")
				}
			}
		} else {
			sb.WriteString("No cleanup performed. Worktree and branches remain.")
		}

		sb.WriteString("\n\n")
		if p.mergeState.CleanupResults != nil && p.mergeState.CleanupResults.BranchDiverged {
			sb.WriteString(dimText("r: rebase  m: merge  d: details  Enter: close"))
		} else if p.mergeState.CleanupResults != nil && p.mergeState.CleanupResults.PullError != nil {
			sb.WriteString(dimText("d: details  Enter: close"))
		} else {
			sb.WriteString(dimText("Press Enter to close"))
		}

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

// clearMergeModal clears the merge modal state.
func (p *Plugin) clearMergeModal() {
	p.mergeModal = nil
	p.mergeModalWidth = 0
	p.mergeModalStep = 0
}

// renderMergeModal renders the merge workflow modal with dimmed background.
func (p *Plugin) renderMergeModal(width, height int) string {
	background := p.renderListView(width, height)

	p.ensureMergeModal()
	if p.mergeModal == nil {
		return background
	}

	modalContent := p.mergeModal.Render(width, height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}

// ensureCommitForMergeModal builds/rebuilds the commit-for-merge modal.
func (p *Plugin) ensureCommitForMergeModal() {
	if p.mergeCommitState == nil {
		return
	}

	modalW := 60
	if p.width > 0 && modalW > p.width-4 {
		modalW = p.width - 4
	}
	if modalW < 30 {
		modalW = 30
	}

	// Only rebuild if modal doesn't exist or width changed
	if p.commitForMergeModal != nil && p.commitForMergeModalWidth == modalW {
		return
	}
	p.commitForMergeModalWidth = modalW

	p.commitForMergeModal = modal.New("Uncommitted Changes",
		modal.WithWidth(modalW),
		modal.WithVariant(modal.VariantWarning),
		modal.WithPrimaryAction(commitForMergeActionID),
		modal.WithHints(false),
	).
		AddSection(p.commitForMergeInfoSection()).
		AddSection(modal.Spacer()).
		AddSection(p.commitForMergeChangesSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.Text(dimText("You must commit these changes before creating a PR."))).
		AddSection(modal.Text(dimText("All changes will be staged and committed."))).
		AddSection(modal.Spacer()).
		AddSection(modal.InputWithLabel(commitForMergeInputID, "Commit message:", &p.mergeCommitMessageInput)).
		AddSection(modal.When(func() bool { return p.mergeCommitState != nil && p.mergeCommitState.Error != "" }, p.commitForMergeErrorSection())).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Commit ", commitForMergeCommitID),
			modal.Btn(" Cancel ", commitForMergeCancelID),
		))
}

// commitForMergeInfoSection renders the workspace info section.
func (p *Plugin) commitForMergeInfoSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.mergeCommitState == nil || p.mergeCommitState.Worktree == nil {
			return modal.RenderedSection{}
		}

		wt := p.mergeCommitState.Worktree
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Workspace: %s\n", lipgloss.NewStyle().Bold(true).Render(wt.Name)))
		sb.WriteString(fmt.Sprintf("Branch:    %s", wt.Branch))

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

// commitForMergeChangesSection renders the change counts section.
func (p *Plugin) commitForMergeChangesSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.mergeCommitState == nil {
			return modal.RenderedSection{}
		}

		var sb strings.Builder
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Changes to commit:"))
		if p.mergeCommitState.StagedCount > 0 {
			sb.WriteString(fmt.Sprintf("\n  • %d staged file(s)", p.mergeCommitState.StagedCount))
		}
		if p.mergeCommitState.ModifiedCount > 0 {
			sb.WriteString(fmt.Sprintf("\n  • %d modified file(s)", p.mergeCommitState.ModifiedCount))
		}
		if p.mergeCommitState.UntrackedCount > 0 {
			sb.WriteString(fmt.Sprintf("\n  • %d untracked file(s)", p.mergeCommitState.UntrackedCount))
		}

		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

// commitForMergeErrorSection renders the error message section.
func (p *Plugin) commitForMergeErrorSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.mergeCommitState == nil || p.mergeCommitState.Error == "" {
			return modal.RenderedSection{}
		}

		errStyle := lipgloss.NewStyle().Foreground(styles.Error)
		content := errStyle.Render("Error: " + p.mergeCommitState.Error)

		return modal.RenderedSection{Content: content}
	}, nil)
}

// clearCommitForMergeModal clears commit-for-merge modal state.
func (p *Plugin) clearCommitForMergeModal() {
	p.commitForMergeModal = nil
	p.commitForMergeModalWidth = 0
}

// renderCommitForMergeModal renders the commit-before-merge modal.
func (p *Plugin) renderCommitForMergeModal(width, height int) string {
	background := p.renderListView(width, height)

	p.ensureCommitForMergeModal()
	if p.commitForMergeModal == nil {
		return background
	}

	modalContent := p.commitForMergeModal.Render(width, height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}

// ensureTypeSelectorModal builds/rebuilds the type selector modal.
func (p *Plugin) ensureTypeSelectorModal() {
	// Wider when Shell selected to fit name input and agent list (td-a902fe)
	modalW := 32
	if p.typeSelectorIdx == 0 {
		modalW = 48 // Wider for agent selection
	}
	if modalW > p.width-4 {
		modalW = p.width - 4
	}
	if modalW < 20 {
		modalW = 20
	}

	// Only rebuild if modal doesn't exist or width changed
	if p.typeSelectorModal != nil && p.typeSelectorModalWidth == modalW {
		return
	}
	p.typeSelectorModalWidth = modalW

	// Set placeholder for name input
	p.typeSelectorNameInput.Placeholder = p.nextShellDisplayName()

	// Build agent list items for shell (td-a902fe)
	agentItems := make([]modal.ListItem, len(ShellAgentOrder))
	for i, at := range ShellAgentOrder {
		agentItems[i] = modal.ListItem{
			ID:    typeSelectorAgentItemPfx + string(at),
			Label: AgentDisplayNames[at],
		}
	}

	p.typeSelectorModal = modal.New("Create New",
		modal.WithWidth(modalW),
		modal.WithHints(false),
	).
		// Type selector: Shell vs Workspace (use singleFocus so Tab moves to next section)
		AddSection(modal.List(typeSelectorListID, []modal.ListItem{
			{ID: "type-shell", Label: "Shell"},
			{ID: "type-workspace", Label: "Workspace"},
		}, &p.typeSelectorIdx, modal.WithMaxVisible(2), modal.WithSingleFocus())).
		// Shell options section (only shown when Shell is selected)
		AddSection(modal.When(p.typeSelectorIsShell, modal.Spacer())).
		AddSection(modal.When(p.typeSelectorIsShell, p.typeSelectorShellHeaderSection())).
		AddSection(modal.When(p.typeSelectorIsShell, p.typeSelectorNameSection())).
		AddSection(modal.When(p.typeSelectorIsShell, modal.Spacer())).
		AddSection(modal.When(p.typeSelectorIsShell, p.typeSelectorAgentLabelSection())).
		AddSection(modal.When(p.typeSelectorIsShell, modal.List(typeSelectorAgentListID, agentItems, &p.typeSelectorAgentIdx, modal.WithMaxVisible(len(agentItems)), modal.WithSingleFocus()))).
		AddSection(modal.When(p.typeSelectorIsShellWithSkipPerms, modal.Spacer())).
		AddSection(modal.When(p.typeSelectorIsShellWithSkipPerms, modal.Checkbox(typeSelectorSkipPermsID, "Auto-approve all actions", &p.typeSelectorSkipPerms))).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Confirm ", typeSelectorConfirmID),
			modal.Btn(" Cancel ", typeSelectorCancelID),
		))
}

// typeSelectorIsShell returns true when Shell is selected (for conditional sections).
func (p *Plugin) typeSelectorIsShell() bool {
	return p.typeSelectorIdx == 0
}

// typeSelectorIsShellWithSkipPerms returns true when Shell is selected AND agent has skip perms flag.
// td-a902fe: Used to conditionally show skip permissions checkbox.
func (p *Plugin) typeSelectorIsShellWithSkipPerms() bool {
	return p.typeSelectorIdx == 0 && p.shouldShowShellSkipPerms()
}

// typeSelectorShellHeaderSection renders a header to separate shell options from the type selector.
func (p *Plugin) typeSelectorShellHeaderSection() modal.Section {
	return modal.Text("── Shell Options ──")
}

// typeSelectorNameSection renders the shell name input.
func (p *Plugin) typeSelectorNameSection() modal.Section {
	return modal.InputWithLabel(typeSelectorInputID, "Name (optional):", &p.typeSelectorNameInput)
}

// typeSelectorAgentLabelSection renders the agent selection label.
// td-a902fe: Shows "Agent (optional):" label above agent list.
func (p *Plugin) typeSelectorAgentLabelSection() modal.Section {
	return modal.Text("Agent (optional):")
}

// clearTypeSelectorModal clears the type selector modal state.
func (p *Plugin) clearTypeSelectorModal() {
	p.typeSelectorIdx = 1 // Reset to Worktree default
	p.typeSelectorNameInput.SetValue("")
	p.typeSelectorNameInput.Blur()
	p.typeSelectorModal = nil
	p.typeSelectorModalWidth = 0
	// Reset shell agent selection state (td-2bb232)
	p.typeSelectorAgentIdx = 0
	p.typeSelectorAgentType = AgentNone
	p.typeSelectorSkipPerms = false
	p.typeSelectorFocusField = 0
}

// renderTypeSelectorModal renders the type selector modal (Shell vs Worktree).
func (p *Plugin) renderTypeSelectorModal(width, height int) string {
	background := p.renderListView(width, height)

	p.ensureTypeSelectorModal()
	if p.typeSelectorModal == nil {
		return background
	}

	modalContent := p.typeSelectorModal.Render(width, height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}
