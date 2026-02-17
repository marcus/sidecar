package workspace

import (
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/plugin"
)

// Commands returns the available commands.
func (p *Plugin) Commands() []plugin.Command {
	switch p.viewMode {
	case ViewModeInteractive:
		return []plugin.Command{
			{ID: "exit-interactive", Name: "Exit", Description: "Exit interactive mode (" + p.getInteractiveExitKey() + ")", Context: "workspace-interactive", Priority: 1},
			{ID: "copy", Name: "Copy", Description: "Copy selection (" + p.getInteractiveCopyKey() + ")", Context: "workspace-interactive", Priority: 2},
			{ID: "paste", Name: "Paste", Description: "Paste clipboard (" + p.getInteractivePasteKey() + ")", Context: "workspace-interactive", Priority: 3},
		}
	case ViewModeCreate:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel workspace creation", Context: "workspace-create", Priority: 1},
			{ID: "confirm", Name: "Create", Description: "Create the workspace", Context: "workspace-create", Priority: 2},
		}
	case ViewModeTaskLink:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel task linking", Context: "workspace-task-link", Priority: 1},
			{ID: "select-task", Name: "Select", Description: "Link selected task", Context: "workspace-task-link", Priority: 2},
		}
	case ViewModeMerge:
		if p.mergeState != nil && p.mergeState.Step == MergeStepError {
			return []plugin.Command{
				{ID: "dismiss-merge-error", Name: "Dismiss", Description: "Dismiss error", Context: "workspace-merge-error", Priority: 1},
				{ID: "yank-merge-error", Name: "Yank", Description: "Copy error to clipboard", Context: "workspace-merge-error", Priority: 2},
			}
		}
		cmds := []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel merge workflow", Context: "workspace-merge", Priority: 1},
		}
		if p.mergeState != nil {
			switch p.mergeState.Step {
			case MergeStepReviewDiff:
				cmds = append(cmds, plugin.Command{ID: "continue", Name: "Push", Description: "Push branch", Context: "workspace-merge", Priority: 2})
			case MergeStepWaitingMerge:
				cmds = append(cmds, plugin.Command{ID: "continue", Name: "Check", Description: "Check merge status", Context: "workspace-merge", Priority: 2})
			case MergeStepDone:
				cmds = append(cmds, plugin.Command{ID: "continue", Name: "Done", Description: "Close modal", Context: "workspace-merge", Priority: 2})
			}
		}
		return cmds
	case ViewModeAgentChoice:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel agent choice", Context: "workspace-agent-choice", Priority: 1},
			{ID: "select", Name: "Select", Description: "Choose selected option", Context: "workspace-agent-choice", Priority: 2},
		}
	case ViewModeConfirmDelete:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel deletion", Context: "workspace-confirm-delete", Priority: 1},
			{ID: "delete", Name: "Delete", Description: "Confirm deletion", Context: "workspace-confirm-delete", Priority: 2},
		}
	case ViewModeConfirmDeleteShell:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel deletion", Context: "workspace-confirm-delete-shell", Priority: 1},
			{ID: "delete", Name: "Delete", Description: "Terminate shell", Context: "workspace-confirm-delete-shell", Priority: 2},
		}
	case ViewModeCommitForMerge:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel merge", Context: "workspace-commit-for-merge", Priority: 1},
			{ID: "commit", Name: "Commit", Description: "Commit and continue", Context: "workspace-commit-for-merge", Priority: 2},
		}
	case ViewModeRenameShell:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel rename", Context: "workspace-rename-shell", Priority: 1},
			{ID: "confirm", Name: "Rename", Description: "Confirm new name", Context: "workspace-rename-shell", Priority: 2},
		}
	case ViewModeFetchPR:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel PR fetch", Context: "workspace-fetch-pr", Priority: 1},
			{ID: "fetch", Name: "Fetch", Description: "Fetch selected PR", Context: "workspace-fetch-pr", Priority: 2},
		}
	case ViewModeFilePicker:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Close file picker", Context: "workspace-file-picker", Priority: 1},
			{ID: "select", Name: "Jump", Description: "Jump to selected file", Context: "workspace-file-picker", Priority: 2},
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
				{ID: "switch-pane", Name: "Focus", Description: "Switch to sidebar", Context: "workspace-preview", Priority: 1},
				{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Context: "workspace-preview", Priority: 2},
			}
			// Tab commands only shown when a worktree is selected (not shell)
			// Shell has no tabs - it shows primer/output directly
			if !p.shellSelected {
				cmds = append(cmds,
					plugin.Command{ID: "prev-tab", Name: "Tab←", Description: "Previous preview tab", Context: "workspace-preview", Priority: 3},
					plugin.Command{ID: "next-tab", Name: "Tab→", Description: "Next preview tab", Context: "workspace-preview", Priority: 4},
				)
				// Add diff view toggle when on Diff tab
				if p.previewTab == PreviewTabDiff {
					diffViewName := "Split"
					if p.diffViewMode == DiffViewSideBySide {
						diffViewName = "Unified"
					}
					cmds = append(cmds, plugin.Command{ID: "toggle-diff-view", Name: diffViewName, Description: "Toggle unified/side-by-side diff", Context: "workspace-preview", Priority: 5})
					// Add file navigation commands when viewing diff with multiple files
					if p.multiFileDiff != nil && len(p.multiFileDiff.Files) > 1 {
						cmds = append(cmds,
							plugin.Command{ID: "next-file", Name: "}", Description: "Next file", Context: "workspace-preview", Priority: 6},
							plugin.Command{ID: "prev-file", Name: "{", Description: "Previous file", Context: "workspace-preview", Priority: 7},
							plugin.Command{ID: "file-picker", Name: "Files", Description: "Open file picker", Context: "workspace-preview", Priority: 8},
						)
					}
				}
			}
			// Also show agent commands in preview pane
			wt := p.selectedWorktree()
			if wt != nil {
				if wt.Agent == nil {
					cmds = append(cmds,
						plugin.Command{ID: "start-agent", Name: "Start", Description: "Start agent", Context: "workspace-preview", Priority: 10},
					)
				} else {
					cmds = append(cmds,
						plugin.Command{ID: "start-agent", Name: "Agent", Description: "Agent options (attach/restart)", Context: "workspace-preview", Priority: 9},
						plugin.Command{ID: "attach", Name: "Attach", Description: "Attach to session", Context: "workspace-preview", Priority: 10},
						plugin.Command{ID: "stop-agent", Name: "Stop", Description: "Stop agent", Context: "workspace-preview", Priority: 11},
					)
					if wt.Status == StatusWaiting {
						cmds = append(cmds,
							plugin.Command{ID: "approve", Name: "Approve", Description: "Approve agent prompt", Context: "workspace-preview", Priority: 12},
							plugin.Command{ID: "reject", Name: "Reject", Description: "Reject agent prompt", Context: "workspace-preview", Priority: 13},
						)
					}
				}
			}
			// Show interactive mode hint when feature enabled and session active
			// Workspace: needs agent and Output tab; Shell: always shows output
			if features.IsEnabled(features.TmuxInteractiveInput.Name) {
				hasActiveSession := false
				if p.shellSelected {
					if shell := p.getSelectedShell(); shell != nil && shell.Agent != nil {
						hasActiveSession = true
					}
				} else if wt != nil && wt.Agent != nil && p.previewTab == PreviewTabOutput {
					hasActiveSession = true
				}
				if hasActiveSession {
					cmds = append(cmds,
						plugin.Command{ID: "interactive", Name: "Type", Description: "Enter interactive mode (E)", Context: "workspace-preview", Priority: 15},
					)
				}
			}
			return cmds
		}

		// Sidebar list commands - reorganized with unique priorities
		// Priority 1-4: Base commands (always visible)
		// Priority 5-8: Worktree-specific commands
		// Priority 10-14: Agent commands (highest visibility when applicable)
		cmds := []plugin.Command{
			{ID: "new-workspace", Name: "New", Description: "Create new workspace", Context: "workspace-list", Priority: 1},
			{ID: "fetch-pr", Name: "Fetch", Description: "Fetch remote PR as workspace", Context: "workspace-list", Priority: 2},
			{ID: "toggle-view", Name: viewToggleName, Description: "Toggle list/kanban view", Context: "workspace-list", Priority: 3},
			{ID: "toggle-sidebar", Name: "Sidebar", Description: "Toggle sidebar visibility", Context: "workspace-list", Priority: 4},
			{ID: "refresh", Name: "Refresh", Description: "Refresh workspace list", Context: "workspace-list", Priority: 5},
		}

		// Shell-specific commands when shell is selected
		if p.shellSelected {
			shell := p.getSelectedShell()
			if shell == nil || shell.Agent == nil {
				cmds = append(cmds,
					plugin.Command{ID: "attach-shell", Name: "Attach", Description: "Create and attach to shell", Context: "workspace-list", Priority: 10},
					plugin.Command{ID: "rename-shell", Name: "Rename", Description: "Rename shell", Context: "workspace-list", Priority: 11},
				)
			} else {
				cmds = append(cmds,
					plugin.Command{ID: "attach-shell", Name: "Attach", Description: "Attach to shell", Context: "workspace-list", Priority: 10},
					plugin.Command{ID: "kill-shell", Name: "Kill", Description: "Kill shell session", Context: "workspace-list", Priority: 11},
					plugin.Command{ID: "rename-shell", Name: "Rename", Description: "Rename shell", Context: "workspace-list", Priority: 12},
				)
			}
			return cmds
		}

		wt := p.selectedWorktree()
		if wt != nil {
			// Agent commands first (most context-dependent, highest visibility)
			if wt.Agent == nil {
				cmds = append(cmds,
					plugin.Command{ID: "start-agent", Name: "Start", Description: "Start agent", Context: "workspace-list", Priority: 10},
				)
			} else {
				cmds = append(cmds,
					plugin.Command{ID: "start-agent", Name: "Agent", Description: "Agent options (attach/restart)", Context: "workspace-list", Priority: 9},
					plugin.Command{ID: "attach", Name: "Attach", Description: "Attach to session", Context: "workspace-list", Priority: 10},
					plugin.Command{ID: "stop-agent", Name: "Stop", Description: "Stop agent", Context: "workspace-list", Priority: 11},
				)
				if wt.Status == StatusWaiting {
					cmds = append(cmds,
						plugin.Command{ID: "approve", Name: "Approve", Description: "Approve agent prompt", Context: "workspace-list", Priority: 12},
						plugin.Command{ID: "reject", Name: "Reject", Description: "Reject agent prompt", Context: "workspace-list", Priority: 13},
						plugin.Command{ID: "approve-all", Name: "Approve All", Description: "Approve all agent prompts", Context: "workspace-list", Priority: 14},
					)
				}
			}
			// Workspace commands
			cmds = append(cmds,
				plugin.Command{ID: "delete-workspace", Name: "Delete", Description: "Delete selected workspace", Context: "workspace-list", Priority: 5},
				plugin.Command{ID: "push", Name: "Push", Description: "Push branch to remote", Context: "workspace-list", Priority: 6},
				plugin.Command{ID: "merge-workflow", Name: "Merge", Description: "Start merge workflow", Context: "workspace-list", Priority: 7},
				plugin.Command{ID: "open-in-git", Name: "Git", Description: "Open in Git tab", Context: "workspace-list", Priority: 16},
			)
			// Task linking
			if wt.TaskID != "" {
				cmds = append(cmds,
					plugin.Command{ID: "link-task", Name: "Unlink", Description: "Unlink task", Context: "workspace-list", Priority: 8},
				)
			} else {
				cmds = append(cmds,
					plugin.Command{ID: "link-task", Name: "Task", Description: "Link task", Context: "workspace-list", Priority: 8},
				)
			}
		}
		return cmds
	}
}

// FocusContext returns the current focus context for keybinding dispatch.
func (p *Plugin) FocusContext() string {
	switch p.viewMode {
	case ViewModeInteractive:
		return "workspace-interactive"
	case ViewModeCreate:
		return "workspace-create"
	case ViewModeTaskLink:
		return "workspace-task-link"
	case ViewModeMerge:
		if p.mergeState != nil && p.mergeState.Step == MergeStepError {
			return "workspace-merge-error"
		}
		return "workspace-merge"
	case ViewModeAgentChoice:
		return "workspace-agent-choice"
	case ViewModeConfirmDelete:
		return "workspace-confirm-delete"
	case ViewModeConfirmDeleteShell:
		return "workspace-confirm-delete-shell"
	case ViewModeCommitForMerge:
		return "workspace-commit-for-merge"
	case ViewModePromptPicker:
		return "workspace-prompt-picker"
	case ViewModeRenameShell:
		return "workspace-rename-shell"
	case ViewModeTypeSelector:
		return "workspace-type-selector"
	case ViewModeFetchPR:
		return "workspace-fetch-pr"
	case ViewModeFilePicker:
		return "workspace-file-picker"
	default:
		if p.activePane == PanePreview {
			return "workspace-preview"
		}
		return "workspace-list"
	}
}

// ConsumesTextInput reports whether the workspace plugin is currently in a
// mode that expects typed text input.
func (p *Plugin) ConsumesTextInput() bool {
	switch p.viewMode {
	case ViewModeInteractive,
		ViewModeCreate,
		ViewModeTaskLink,
		ViewModeCommitForMerge,
		ViewModePromptPicker,
		ViewModeRenameShell,
		ViewModeTypeSelector,
		ViewModeFetchPR:
		return true
	default:
		return false
	}
}
