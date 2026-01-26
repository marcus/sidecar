package keymap

// DefaultBindings returns the default keymap.
func DefaultBindings() []Binding {
	return []Binding{
		// Global context
		{Key: "q", Command: "quit", Context: "global"},
		{Key: "?", Command: "toggle-palette", Context: "global"},
		{Key: "!", Command: "toggle-diagnostics", Context: "global"},
		{Key: "`", Command: "next-plugin", Context: "global"},
		{Key: "~", Command: "prev-plugin", Context: "global"},
		{Key: "@", Command: "switch-project", Context: "global"},
		{Key: "1", Command: "focus-plugin-1", Context: "global"},
		{Key: "2", Command: "focus-plugin-2", Context: "global"},
		{Key: "3", Command: "focus-plugin-3", Context: "global"},
		{Key: "4", Command: "focus-plugin-4", Context: "global"},
		{Key: "5", Command: "focus-plugin-5", Context: "global"},
		{Key: "6", Command: "focus-plugin-6", Context: "global"},
		{Key: "7", Command: "focus-plugin-7", Context: "global"},
		{Key: "8", Command: "focus-plugin-8", Context: "global"},
		{Key: "9", Command: "focus-plugin-9", Context: "global"},

		// Navigation (Global defaults)
		{Key: "j", Command: "cursor-down", Context: "global"},
		{Key: "k", Command: "cursor-up", Context: "global"},
		{Key: "down", Command: "cursor-down", Context: "global"},
		{Key: "up", Command: "cursor-up", Context: "global"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "global"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "global"},
		{Key: "g g", Command: "cursor-top", Context: "global"},
		{Key: "G", Command: "cursor-bottom", Context: "global"},
		{Key: "enter", Command: "select", Context: "global"},
		{Key: "esc", Command: "back", Context: "global"},

		// Project switcher context
		{Key: "@", Command: "toggle", Context: "project-switcher"},
		{Key: "esc", Command: "close", Context: "project-switcher"},
		{Key: "enter", Command: "select", Context: "project-switcher"},
		{Key: "down", Command: "cursor-down", Context: "project-switcher"},
		{Key: "up", Command: "cursor-up", Context: "project-switcher"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "project-switcher"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "project-switcher"},

		// Git status context
		{Key: "j", Command: "cursor-down", Context: "git-status"},
		{Key: "k", Command: "cursor-up", Context: "git-status"},
		{Key: "tab", Command: "switch-pane", Context: "git-status"},
		{Key: "shift+tab", Command: "switch-pane", Context: "git-status"},
		{Key: "s", Command: "stage-file", Context: "git-status"},
		{Key: "u", Command: "unstage-file", Context: "git-status"},
		{Key: "S", Command: "stage-all", Context: "git-status"},
		{Key: "U", Command: "unstage-all", Context: "git-status"},
		{Key: "c", Command: "commit", Context: "git-status"},
		{Key: "A", Command: "amend", Context: "git-status"},
		{Key: "d", Command: "show-diff", Context: "git-status"},
		{Key: "enter", Command: "show-diff", Context: "git-status"},
		{Key: "r", Command: "refresh", Context: "git-status"},
		{Key: "h", Command: "show-history", Context: "git-status"},
		{Key: "P", Command: "push", Context: "git-status"},
		{Key: "f", Command: "fetch", Context: "git-status"},
		{Key: "L", Command: "pull", Context: "git-status"},
		{Key: "b", Command: "branch-picker", Context: "git-status"},
		{Key: "z", Command: "stash", Context: "git-status"},
		{Key: "Z", Command: "stash-pop", Context: "git-status"},
		{Key: "O", Command: "open-in-file-browser", Context: "git-status"},
		{Key: "o", Command: "open-in-github", Context: "git-status"},
		{Key: "y", Command: "yank-file", Context: "git-status"},
		{Key: "Y", Command: "yank-path", Context: "git-status"},
		{Key: "D", Command: "discard-changes", Context: "git-status"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-status"},

		// Git status commits context (sidebar)
		{Key: "j", Command: "cursor-down", Context: "git-status-commits"},
		{Key: "k", Command: "cursor-up", Context: "git-status-commits"},
		{Key: "enter", Command: "view-commit", Context: "git-status-commits"},
		{Key: "d", Command: "view-commit", Context: "git-status-commits"},
		{Key: "h", Command: "show-history", Context: "git-status-commits"},
		{Key: "y", Command: "yank-commit", Context: "git-status-commits"},
		{Key: "Y", Command: "yank-id", Context: "git-status-commits"},
		{Key: "/", Command: "search-history", Context: "git-status-commits"},
		{Key: "f", Command: "filter-author", Context: "git-status-commits"},
		{Key: "p", Command: "filter-path", Context: "git-status-commits"},
		{Key: "F", Command: "clear-filter", Context: "git-status-commits"},
		{Key: "n", Command: "next-match", Context: "git-status-commits"},
		{Key: "N", Command: "prev-match", Context: "git-status-commits"},
		{Key: "o", Command: "open-in-github", Context: "git-status-commits"},
		{Key: "v", Command: "toggle-graph", Context: "git-status-commits"},
		{Key: "P", Command: "push", Context: "git-status-commits"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-status-commits"},

		// Git status diff context (inline)
		{Key: "j", Command: "scroll-down", Context: "git-status-diff"},
		{Key: "k", Command: "scroll-up", Context: "git-status-diff"},
		{Key: "ctrl+d", Command: "page-down", Context: "git-status-diff"},
		{Key: "ctrl+u", Command: "page-up", Context: "git-status-diff"},
		{Key: "enter", Command: "full-diff", Context: "git-status-diff"},
		{Key: "s", Command: "stage-file", Context: "git-status-diff"},
		{Key: "u", Command: "unstage-file", Context: "git-status-diff"},
		{Key: "v", Command: "toggle-diff-view", Context: "git-status-diff"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-status-diff"},

		// Git commit preview context
		{Key: "j", Command: "scroll-down", Context: "git-commit-preview"},
		{Key: "k", Command: "scroll-up", Context: "git-commit-preview"},
		{Key: "d", Command: "view-diff", Context: "git-commit-preview"},
		{Key: "esc", Command: "back", Context: "git-commit-preview"},
		{Key: "y", Command: "yank-commit", Context: "git-commit-preview"},
		{Key: "Y", Command: "yank-id", Context: "git-commit-preview"},
		{Key: "o", Command: "open-in-github", Context: "git-commit-preview"},
		{Key: "b", Command: "open-in-file-browser", Context: "git-commit-preview"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-commit-preview"},

		// Git diff context (full screen)
		{Key: "esc", Command: "close-diff", Context: "git-diff"},
		{Key: "q", Command: "close-diff", Context: "git-diff"},
		{Key: "j", Command: "scroll-down", Context: "git-diff"},
		{Key: "k", Command: "scroll-up", Context: "git-diff"},
		{Key: "down", Command: "scroll-down", Context: "git-diff"},
		{Key: "up", Command: "scroll-up", Context: "git-diff"},
		{Key: "ctrl+d", Command: "page-down", Context: "git-diff"},
		{Key: "ctrl+u", Command: "page-up", Context: "git-diff"},
		{Key: "s", Command: "stage-file", Context: "git-diff"},
		{Key: "u", Command: "unstage-file", Context: "git-diff"},
		{Key: "[", Command: "prev-file", Context: "git-diff"},
		{Key: "]", Command: "next-file", Context: "git-diff"},
		{Key: "y", Command: "yank-diff", Context: "git-diff"},
		{Key: "c", Command: "commit", Context: "git-diff"},
		{Key: "v", Command: "toggle-diff-view", Context: "git-diff"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-diff"},

		// Git push menu context
		{Key: "p", Command: "push", Context: "git-push-menu"},
		{Key: "f", Command: "force-push", Context: "git-push-menu"},
		{Key: "u", Command: "push-upstream", Context: "git-push-menu"},
		{Key: "esc", Command: "cancel", Context: "git-push-menu"},

		// Git pull menu context
		{Key: "p", Command: "pull-merge", Context: "git-pull-menu"},
		{Key: "r", Command: "pull-rebase", Context: "git-pull-menu"},
		{Key: "f", Command: "pull-ff-only", Context: "git-pull-menu"},
		{Key: "a", Command: "pull-autostash", Context: "git-pull-menu"},
		{Key: "esc", Command: "cancel", Context: "git-pull-menu"},

		// Git pull conflict context
		{Key: "a", Command: "abort-pull", Context: "git-pull-conflict"},
		{Key: "esc", Command: "dismiss", Context: "git-pull-conflict"},

		// Git commit context
		{Key: "ctrl+s", Command: "execute-commit", Context: "git-commit"},
		{Key: "ctrl+enter", Command: "execute-commit", Context: "git-commit"},
		{Key: "esc", Command: "cancel", Context: "git-commit"},

		// Git history context
		{Key: "esc", Command: "close-history", Context: "git-history"},
		{Key: "q", Command: "close-history", Context: "git-history"},
		{Key: "enter", Command: "view-commit", Context: "git-history"},

		// Git commit detail context
		{Key: "esc", Command: "close-detail", Context: "git-commit-detail"},
		{Key: "q", Command: "close-detail", Context: "git-commit-detail"},

		// Conversations sidebar context (two-pane mode, left pane focused)
		{Key: "tab", Command: "switch-pane", Context: "conversations-sidebar"},
		{Key: "shift+tab", Command: "switch-pane", Context: "conversations-sidebar"},
		{Key: "a", Command: "new-session", Context: "conversations-sidebar"},
		{Key: "d", Command: "delete-session", Context: "conversations-sidebar"},
		{Key: "r", Command: "rename-session", Context: "conversations-sidebar"},
		{Key: "e", Command: "export-session", Context: "conversations-sidebar"},
		{Key: "c", Command: "copy-session", Context: "conversations-sidebar"},
		{Key: "f", Command: "filter", Context: "conversations-sidebar"},
		{Key: "/", Command: "search", Context: "conversations-sidebar"},
		{Key: "s", Command: "toggle-star", Context: "conversations-sidebar"},
		{Key: "A", Command: "show-analytics", Context: "conversations-sidebar"},
		{Key: "l", Command: "focus-right", Context: "conversations-sidebar"},
		{Key: "right", Command: "focus-right", Context: "conversations-sidebar"},
		{Key: "v", Command: "toggle-view", Context: "conversations-sidebar"},
		{Key: "enter", Command: "select-session", Context: "conversations-sidebar"},
		{Key: "\\", Command: "toggle-sidebar", Context: "conversations-sidebar"},
		{Key: "y", Command: "yank-details", Context: "conversations-sidebar"},
		{Key: "Y", Command: "yank-resume", Context: "conversations-sidebar"},
		{Key: "R", Command: "resume-in-workspace", Context: "conversations-sidebar"},

		// Conversations main context (two-pane mode, right pane focused)
		{Key: "tab", Command: "switch-pane", Context: "conversations-main"},
		{Key: "shift+tab", Command: "switch-pane", Context: "conversations-main"},
		{Key: "esc", Command: "back", Context: "conversations-main"},
		{Key: "j", Command: "scroll", Context: "conversations-main"},
		{Key: "k", Command: "scroll", Context: "conversations-main"},
		{Key: "g", Command: "cursor-top", Context: "conversations-main"},
		{Key: "G", Command: "cursor-bottom", Context: "conversations-main"},
		{Key: "h", Command: "focus-left", Context: "conversations-main"},
		{Key: "left", Command: "focus-left", Context: "conversations-main"},
		{Key: "v", Command: "toggle-view", Context: "conversations-main"},
		{Key: "e", Command: "expand", Context: "conversations-main"},
		{Key: "enter", Command: "detail", Context: "conversations-main"},
		{Key: "\\", Command: "toggle-sidebar", Context: "conversations-main"},
		{Key: "y", Command: "yank-details", Context: "conversations-main"},
		{Key: "Y", Command: "yank-resume", Context: "conversations-main"},
		{Key: "R", Command: "resume-in-workspace", Context: "conversations-main"},

		// File browser tree context
		{Key: "tab", Command: "switch-pane", Context: "file-browser-tree"},
		{Key: "shift+tab", Command: "switch-pane", Context: "file-browser-tree"},
		{Key: "/", Command: "search", Context: "file-browser-tree"},
		{Key: "ctrl+p", Command: "quick-open", Context: "file-browser-tree"},
		{Key: "f", Command: "project-search", Context: "file-browser-tree"},
		{Key: "t", Command: "new-tab", Context: "file-browser-tree"},
		{Key: "[", Command: "prev-tab", Context: "file-browser-tree"},
		{Key: "]", Command: "next-tab", Context: "file-browser-tree"},
		{Key: "x", Command: "close-tab", Context: "file-browser-tree"},
		{Key: "a", Command: "create-file", Context: "file-browser-tree"},
		{Key: "A", Command: "create-dir", Context: "file-browser-tree"},
		{Key: "d", Command: "delete", Context: "file-browser-tree"},
		{Key: "y", Command: "yank", Context: "file-browser-tree"},
		{Key: "Y", Command: "copy-path", Context: "file-browser-tree"},
		{Key: "p", Command: "paste", Context: "file-browser-tree"},
		{Key: "s", Command: "sort", Context: "file-browser-tree"},
		{Key: "r", Command: "refresh", Context: "file-browser-tree"},
		{Key: "m", Command: "move", Context: "file-browser-tree"},
		{Key: "R", Command: "rename", Context: "file-browser-tree"},
		{Key: "ctrl+r", Command: "reveal", Context: "file-browser-tree"},
		{Key: "i", Command: "info", Context: "file-browser-tree"},
		{Key: "e", Command: "edit", Context: "file-browser-tree"},
		{Key: "E", Command: "edit-external", Context: "file-browser-tree"},
		{Key: "B", Command: "blame", Context: "file-browser-tree"},
		{Key: "\\", Command: "toggle-sidebar", Context: "file-browser-tree"},
		{Key: "I", Command: "toggle-ignored", Context: "file-browser-tree"},

		// File browser preview context
		{Key: "tab", Command: "switch-pane", Context: "file-browser-preview"},
		{Key: "shift+tab", Command: "switch-pane", Context: "file-browser-preview"},
		{Key: "/", Command: "search-content", Context: "file-browser-preview"},
		{Key: "ctrl+p", Command: "quick-open", Context: "file-browser-preview"},
		{Key: "f", Command: "project-search", Context: "file-browser-preview"},
		{Key: "[", Command: "prev-tab", Context: "file-browser-preview"},
		{Key: "]", Command: "next-tab", Context: "file-browser-preview"},
		{Key: "x", Command: "close-tab", Context: "file-browser-preview"},
		{Key: "r", Command: "refresh", Context: "file-browser-preview"},
		{Key: "R", Command: "rename", Context: "file-browser-preview"},
		{Key: "ctrl+r", Command: "reveal", Context: "file-browser-preview"},
		{Key: "i", Command: "info", Context: "file-browser-preview"},
		{Key: "e", Command: "edit", Context: "file-browser-preview"},
		{Key: "E", Command: "edit-external", Context: "file-browser-preview"},
		{Key: "B", Command: "blame", Context: "file-browser-preview"},
		{Key: "m", Command: "toggle-markdown", Context: "file-browser-preview"},
		{Key: "esc", Command: "back", Context: "file-browser-preview"},
		{Key: "h", Command: "back", Context: "file-browser-preview"},
		{Key: "y", Command: "yank-contents", Context: "file-browser-preview"},
		{Key: "Y", Command: "yank-path", Context: "file-browser-preview"},
		{Key: "\\", Command: "toggle-sidebar", Context: "file-browser-preview"},

		// File browser tree search context
		{Key: "esc", Command: "cancel", Context: "file-browser-search"},
		{Key: "enter", Command: "confirm", Context: "file-browser-search"},
		{Key: "n", Command: "next-match", Context: "file-browser-search"},
		{Key: "N", Command: "prev-match", Context: "file-browser-search"},

		// File browser content search context
		{Key: "esc", Command: "cancel", Context: "file-browser-content-search"},
		{Key: "enter", Command: "confirm", Context: "file-browser-content-search"},
		{Key: "n", Command: "next-match", Context: "file-browser-content-search"},
		{Key: "N", Command: "prev-match", Context: "file-browser-content-search"},

		// File browser quick open context
		{Key: "esc", Command: "cancel", Context: "file-browser-quick-open"},
		{Key: "enter", Command: "select", Context: "file-browser-quick-open"},
		{Key: "up", Command: "cursor-up", Context: "file-browser-quick-open"},
		{Key: "down", Command: "cursor-down", Context: "file-browser-quick-open"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "file-browser-quick-open"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "file-browser-quick-open"},

		// File browser project search context
		{Key: "esc", Command: "cancel", Context: "file-browser-project-search"},
		{Key: "enter", Command: "select", Context: "file-browser-project-search"},
		{Key: "up", Command: "cursor-up", Context: "file-browser-project-search"},
		{Key: "down", Command: "cursor-down", Context: "file-browser-project-search"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "file-browser-project-search"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "file-browser-project-search"},
		{Key: "tab", Command: "toggle", Context: "file-browser-project-search"},
		{Key: "alt+r", Command: "toggle-regex", Context: "file-browser-project-search"},
		{Key: "alt+c", Command: "toggle-case", Context: "file-browser-project-search"},
		{Key: "alt+w", Command: "toggle-word", Context: "file-browser-project-search"},
		{Key: "ctrl+g", Command: "cursor-top", Context: "file-browser-project-search"},
		{Key: "ctrl+e", Command: "open-in-editor", Context: "file-browser-project-search"},
		{Key: "ctrl+d", Command: "page-down", Context: "file-browser-project-search"},
		{Key: "ctrl+u", Command: "page-up", Context: "file-browser-project-search"},

		// File browser file operation context
		{Key: "esc", Command: "cancel", Context: "file-browser-file-op"},
		{Key: "enter", Command: "confirm", Context: "file-browser-file-op"},
		{Key: "tab", Command: "next-button", Context: "file-browser-file-op"},
		{Key: "shift+tab", Command: "prev-button", Context: "file-browser-file-op"},

		// File browser line jump context
		{Key: "esc", Command: "cancel", Context: "file-browser-line-jump"},
		{Key: "enter", Command: "confirm", Context: "file-browser-line-jump"},

		// Worktree context
		{Key: "n", Command: "new-workspace", Context: "workspace-list"},
		{Key: "v", Command: "toggle-view", Context: "workspace-list"},
		{Key: "r", Command: "refresh", Context: "workspace-list"},
		{Key: "D", Command: "delete-workspace", Context: "workspace-list"},
		{Key: "d", Command: "show-diff", Context: "workspace-list"},
		{Key: "p", Command: "push", Context: "workspace-list"},
		{Key: "m", Command: "merge-workflow", Context: "workspace-list"},
		{Key: "T", Command: "link-task", Context: "workspace-list"},
		{Key: "s", Command: "start-agent", Context: "workspace-list"},
		{Key: "enter", Command: "interactive", Context: "workspace-list"},
		{Key: "t", Command: "attach", Context: "workspace-list"},
		{Key: "S", Command: "stop-agent", Context: "workspace-list"},
		{Key: "y", Command: "approve", Context: "workspace-list"},
		{Key: "Y", Command: "approve-all", Context: "workspace-list"},
		{Key: "N", Command: "reject", Context: "workspace-list"},
		{Key: "K", Command: "kill-shell", Context: "workspace-list"},
		{Key: "O", Command: "open-in-git", Context: "workspace-list"},
		{Key: "l", Command: "focus-right", Context: "workspace-list"},
		{Key: "right", Command: "focus-right", Context: "workspace-list"},
		{Key: "tab", Command: "switch-pane", Context: "workspace-list"},
		{Key: "shift+tab", Command: "switch-pane", Context: "workspace-list"},
		{Key: "\\", Command: "toggle-sidebar", Context: "workspace-list"},
		{Key: "[", Command: "prev-tab", Context: "workspace-list"},
		{Key: "]", Command: "next-tab", Context: "workspace-list"},

		// Workspace preview context
		{Key: "h", Command: "focus-left", Context: "workspace-preview"},
		{Key: "left", Command: "focus-left", Context: "workspace-preview"},
		{Key: "esc", Command: "focus-left", Context: "workspace-preview"},
		{Key: "s", Command: "start-agent", Context: "workspace-preview"},
		{Key: "S", Command: "stop-agent", Context: "workspace-preview"},
		{Key: "y", Command: "approve", Context: "workspace-preview"},
		{Key: "Y", Command: "approve-all", Context: "workspace-preview"},
		{Key: "N", Command: "reject", Context: "workspace-preview"},
		{Key: "v", Command: "toggle-diff-view", Context: "workspace-preview"},
		{Key: "0", Command: "reset-scroll", Context: "workspace-preview"},
		{Key: "tab", Command: "switch-pane", Context: "workspace-preview"},
		{Key: "shift+tab", Command: "switch-pane", Context: "workspace-preview"},
		{Key: "\\", Command: "toggle-sidebar", Context: "workspace-preview"},
		{Key: "[", Command: "prev-tab", Context: "workspace-preview"},
		{Key: "]", Command: "next-tab", Context: "workspace-preview"},
		{Key: "j", Command: "scroll-down", Context: "workspace-preview"},
		{Key: "k", Command: "scroll-up", Context: "workspace-preview"},
		{Key: "ctrl+d", Command: "page-down", Context: "workspace-preview"},
		{Key: "ctrl+u", Command: "page-up", Context: "workspace-preview"},

		// Workspace interactive context bindings are registered dynamically
		// by the workspace plugin Init() to reflect configured keys.
	}
}

// Category represents a command category.
type Category string

const (
	CategoryNavigation Category = "Navigation"
	CategoryActions    Category = "Actions"
	CategoryView       Category = "View"
	CategorySearch     Category = "Search"
	CategorySystem     Category = "System"
)

// RegisterDefaults registers all default bindings with the given registry.
func RegisterDefaults(r *Registry) {
	for _, binding := range DefaultBindings() {
		r.RegisterBinding(binding)
	}
}
