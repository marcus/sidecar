package worktree

import (
	"github.com/marcus/sidecar/internal/plugin"
)

// Commands returns the available commands.
func (p *Plugin) Commands() []plugin.Command {
	switch p.viewMode {
	case ViewModeCreate:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel worktree creation", Context: "worktree-create", Priority: 1},
			{ID: "confirm", Name: "Create", Description: "Create the worktree", Context: "worktree-create", Priority: 2},
		}
	case ViewModeTaskLink:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel task linking", Context: "worktree-task-link", Priority: 1},
			{ID: "select-task", Name: "Select", Description: "Link selected task", Context: "worktree-task-link", Priority: 2},
		}
	case ViewModeMerge:
		cmds := []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel merge workflow", Context: "worktree-merge", Priority: 1},
		}
		if p.mergeState != nil {
			switch p.mergeState.Step {
			case MergeStepReviewDiff:
				cmds = append(cmds, plugin.Command{ID: "continue", Name: "Push", Description: "Push branch", Context: "worktree-merge", Priority: 2})
			case MergeStepWaitingMerge:
				cmds = append(cmds, plugin.Command{ID: "continue", Name: "Check", Description: "Check merge status", Context: "worktree-merge", Priority: 2})
			case MergeStepDone:
				cmds = append(cmds, plugin.Command{ID: "continue", Name: "Done", Description: "Close modal", Context: "worktree-merge", Priority: 2})
			}
		}
		return cmds
	case ViewModeAgentChoice:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel agent choice", Context: "worktree-agent-choice", Priority: 1},
			{ID: "select", Name: "Select", Description: "Choose selected option", Context: "worktree-agent-choice", Priority: 2},
		}
	case ViewModeConfirmDelete:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel deletion", Context: "worktree-confirm-delete", Priority: 1},
			{ID: "delete", Name: "Delete", Description: "Confirm deletion", Context: "worktree-confirm-delete", Priority: 2},
		}
	case ViewModeCommitForMerge:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel merge", Context: "worktree-commit-for-merge", Priority: 1},
			{ID: "commit", Name: "Commit", Description: "Commit and continue", Context: "worktree-commit-for-merge", Priority: 2},
		}
	default:
		// View toggle label changes based on current mode
		viewToggleName := "Kanban"
		if p.viewMode == ViewModeKanban {
			viewToggleName = "List"
		}

		// Return different commands based on active pane
		if p.activePane == PanePreview {
			// Preview pane commands
			cmds := []plugin.Command{
				{ID: "switch-pane", Name: "Focus", Description: "Switch to sidebar", Context: "worktree-preview", Priority: 1},
				{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Context: "worktree-preview", Priority: 2},
			}
			// Tab commands only shown when a worktree is selected (not shell)
			// Shell has no tabs - it shows primer/output directly
			if !p.shellSelected {
				cmds = append(cmds,
					plugin.Command{ID: "prev-tab", Name: "Tab←", Description: "Previous preview tab", Context: "worktree-preview", Priority: 3},
					plugin.Command{ID: "next-tab", Name: "Tab→", Description: "Next preview tab", Context: "worktree-preview", Priority: 4},
				)
				// Add diff view toggle when on Diff tab
				if p.previewTab == PreviewTabDiff {
					diffViewName := "Split"
					if p.diffViewMode == DiffViewSideBySide {
						diffViewName = "Unified"
					}
					cmds = append(cmds, plugin.Command{ID: "toggle-diff-view", Name: diffViewName, Description: "Toggle unified/side-by-side diff", Context: "worktree-preview", Priority: 5})
				}
			}
			// Also show agent commands in preview pane
			wt := p.selectedWorktree()
			if wt != nil {
				if wt.Agent == nil {
					cmds = append(cmds,
						plugin.Command{ID: "start-agent", Name: "Start", Description: "Start agent", Context: "worktree-preview", Priority: 10},
					)
				} else {
					cmds = append(cmds,
						plugin.Command{ID: "start-agent", Name: "Agent", Description: "Agent options (attach/restart)", Context: "worktree-preview", Priority: 9},
						plugin.Command{ID: "attach", Name: "Attach", Description: "Attach to session", Context: "worktree-preview", Priority: 10},
						plugin.Command{ID: "stop-agent", Name: "Stop", Description: "Stop agent", Context: "worktree-preview", Priority: 11},
					)
					if wt.Status == StatusWaiting {
						cmds = append(cmds,
							plugin.Command{ID: "approve", Name: "Approve", Description: "Approve agent prompt", Context: "worktree-preview", Priority: 12},
							plugin.Command{ID: "reject", Name: "Reject", Description: "Reject agent prompt", Context: "worktree-preview", Priority: 13},
						)
					}
				}
			}
			return cmds
		}

		// Sidebar list commands - reorganized with unique priorities
		// Priority 1-4: Base commands (always visible)
		// Priority 5-8: Worktree-specific commands
		// Priority 10-14: Agent commands (highest visibility when applicable)
		cmds := []plugin.Command{
			{ID: "new-worktree", Name: "New", Description: "Create new worktree", Context: "worktree-list", Priority: 1},
			{ID: "toggle-view", Name: viewToggleName, Description: "Toggle list/kanban view", Context: "worktree-list", Priority: 2},
			{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Context: "worktree-list", Priority: 3},
			{ID: "refresh", Name: "Refresh", Description: "Refresh worktree list", Context: "worktree-list", Priority: 4},
		}

		// Shell-specific commands when shell is selected
		if p.shellSelected {
			shell := p.getSelectedShell()
			if shell == nil || shell.Agent == nil {
				cmds = append(cmds,
					plugin.Command{ID: "attach-shell", Name: "Attach", Description: "Create and attach to shell", Context: "worktree-list", Priority: 10},
				)
			} else {
				cmds = append(cmds,
					plugin.Command{ID: "attach-shell", Name: "Attach", Description: "Attach to shell", Context: "worktree-list", Priority: 10},
					plugin.Command{ID: "kill-shell", Name: "Kill", Description: "Kill shell session", Context: "worktree-list", Priority: 11},
				)
			}
			return cmds
		}

		wt := p.selectedWorktree()
		if wt != nil {
			// Agent commands first (most context-dependent, highest visibility)
			if wt.Agent == nil {
				cmds = append(cmds,
					plugin.Command{ID: "start-agent", Name: "Start", Description: "Start agent", Context: "worktree-list", Priority: 10},
				)
			} else {
				cmds = append(cmds,
					plugin.Command{ID: "start-agent", Name: "Agent", Description: "Agent options (attach/restart)", Context: "worktree-list", Priority: 9},
					plugin.Command{ID: "attach", Name: "Attach", Description: "Attach to session", Context: "worktree-list", Priority: 10},
					plugin.Command{ID: "stop-agent", Name: "Stop", Description: "Stop agent", Context: "worktree-list", Priority: 11},
				)
				if wt.Status == StatusWaiting {
					cmds = append(cmds,
						plugin.Command{ID: "approve", Name: "Approve", Description: "Approve agent prompt", Context: "worktree-list", Priority: 12},
						plugin.Command{ID: "reject", Name: "Reject", Description: "Reject agent prompt", Context: "worktree-list", Priority: 13},
						plugin.Command{ID: "approve-all", Name: "Approve All", Description: "Approve all agent prompts", Context: "worktree-list", Priority: 14},
					)
				}
			}
			// Worktree commands
			cmds = append(cmds,
				plugin.Command{ID: "delete-worktree", Name: "Delete", Description: "Delete selected worktree", Context: "worktree-list", Priority: 5},
				plugin.Command{ID: "push", Name: "Push", Description: "Push branch to remote", Context: "worktree-list", Priority: 6},
				plugin.Command{ID: "merge-workflow", Name: "Merge", Description: "Start merge workflow", Context: "worktree-list", Priority: 7},
			)
			// Task linking
			if wt.TaskID != "" {
				cmds = append(cmds,
					plugin.Command{ID: "link-task", Name: "Unlink", Description: "Unlink task", Context: "worktree-list", Priority: 8},
				)
			} else {
				cmds = append(cmds,
					plugin.Command{ID: "link-task", Name: "Task", Description: "Link task", Context: "worktree-list", Priority: 8},
				)
			}
		}
		return cmds
	}
}

// FocusContext returns the current focus context for keybinding dispatch.
func (p *Plugin) FocusContext() string {
	switch p.viewMode {
	case ViewModeCreate:
		return "worktree-create"
	case ViewModeTaskLink:
		return "worktree-task-link"
	case ViewModeMerge:
		return "worktree-merge"
	case ViewModeAgentChoice:
		return "worktree-agent-choice"
	case ViewModeConfirmDelete:
		return "worktree-confirm-delete"
	case ViewModeCommitForMerge:
		return "worktree-commit-for-merge"
	case ViewModePromptPicker:
		return "worktree-prompt-picker"
	default:
		if p.activePane == PanePreview {
			return "worktree-preview"
		}
		return "worktree-list"
	}
}
