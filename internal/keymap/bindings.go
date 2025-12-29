package keymap

// DefaultBindings returns the default key bindings.
func DefaultBindings() []Binding {
	return []Binding{
		// Global bindings
		{Key: "q", Command: "quit", Context: "global"},
		{Key: "ctrl+c", Command: "quit", Context: "global"},
		{Key: "tab", Command: "next-plugin", Context: "global"},
		{Key: "shift+tab", Command: "prev-plugin", Context: "global"},
		{Key: "1", Command: "focus-plugin-1", Context: "global"},
		{Key: "2", Command: "focus-plugin-2", Context: "global"},
		{Key: "3", Command: "focus-plugin-3", Context: "global"},
		{Key: "4", Command: "focus-plugin-4", Context: "global"},
		{Key: "g", Command: "focus-git-status", Context: "global"},
		{Key: "t", Command: "focus-td-monitor", Context: "global"},
		{Key: "c", Command: "focus-conversations", Context: "global"},
		{Key: "?", Command: "toggle-palette", Context: "global"},
		{Key: "!", Command: "toggle-diagnostics", Context: "global"},
		{Key: "ctrl+h", Command: "toggle-footer", Context: "global"},
		{Key: "r", Command: "refresh", Context: "global"},
		{Key: "j", Command: "cursor-down", Context: "global"},
		{Key: "down", Command: "cursor-down", Context: "global"},
		{Key: "k", Command: "cursor-up", Context: "global"},
		{Key: "up", Command: "cursor-up", Context: "global"},
		{Key: "g g", Command: "cursor-top", Context: "global"},
		{Key: "G", Command: "cursor-bottom", Context: "global"},
		{Key: "enter", Command: "select", Context: "global"},
		{Key: "esc", Command: "back", Context: "global"},

		// Git Status context (files)
		{Key: "s", Command: "stage-file", Context: "git-status"},
		{Key: "u", Command: "unstage-file", Context: "git-status"},
		{Key: "S", Command: "stage-all", Context: "git-status"},
		{Key: "c", Command: "commit", Context: "git-status"},
		{Key: "d", Command: "show-diff", Context: "git-status"},
		{Key: "D", Command: "show-diff-staged", Context: "git-status"},
		{Key: "v", Command: "toggle-diff-mode", Context: "git-status"},
		{Key: "h", Command: "show-history", Context: "git-status"},
		{Key: "o", Command: "open-file", Context: "git-status"},
		{Key: "O", Command: "open-in-file-browser", Context: "git-status"},
		{Key: "enter", Command: "show-diff", Context: "git-status"},

		// Git Status commits context (recent commits in sidebar)
		{Key: "enter", Command: "view-commit", Context: "git-status-commits"},
		{Key: "d", Command: "view-commit", Context: "git-status-commits"},
		{Key: "h", Command: "show-history", Context: "git-status-commits"},

		// Git commit preview context (commit preview in right pane)
		{Key: "esc", Command: "back", Context: "git-commit-preview"},
		{Key: "h", Command: "back", Context: "git-commit-preview"},
		{Key: "enter", Command: "view-diff", Context: "git-commit-preview"},
		{Key: "d", Command: "view-diff", Context: "git-commit-preview"},

		// Git Diff context
		{Key: "esc", Command: "close-diff", Context: "git-diff"},
		{Key: "j", Command: "scroll", Context: "git-diff"},
		{Key: "k", Command: "scroll", Context: "git-diff"},
		{Key: "O", Command: "open-in-file-browser", Context: "git-diff"},

		// TD Monitor context
		{Key: "a", Command: "approve-issue", Context: "td-monitor"},
		{Key: "r", Command: "mark-review", Context: "td-monitor"},
		{Key: "x", Command: "delete-issue", Context: "td-monitor"},
		{Key: "enter", Command: "view-details", Context: "td-monitor"},

		// TD Detail context
		{Key: "esc", Command: "back", Context: "td-detail"},
		{Key: "a", Command: "approve-issue", Context: "td-detail"},
		{Key: "x", Command: "delete-issue", Context: "td-detail"},

		// Conversations context
		{Key: "enter", Command: "view-session", Context: "conversations"},
		{Key: "/", Command: "search", Context: "conversations"},
		{Key: "r", Command: "refresh", Context: "conversations"},
		{Key: "U", Command: "analytics", Context: "conversations"},

		// Conversations search context
		{Key: "enter", Command: "select", Context: "conversations-search"},
		{Key: "esc", Command: "cancel", Context: "conversations-search"},
		{Key: "up", Command: "cursor-up", Context: "conversations-search"},
		{Key: "down", Command: "cursor-down", Context: "conversations-search"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "conversations-search"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "conversations-search"},

		// Conversation detail context
		{Key: "esc", Command: "back", Context: "conversation-detail"},
		{Key: "q", Command: "back", Context: "conversation-detail"},
		{Key: "j", Command: "scroll", Context: "conversation-detail"},
		{Key: "k", Command: "scroll", Context: "conversation-detail"},
		{Key: "g", Command: "cursor-top", Context: "conversation-detail"},
		{Key: "G", Command: "cursor-bottom", Context: "conversation-detail"},

		// Conversations sidebar context (two-pane mode, left pane focused)
		{Key: "enter", Command: "view-session", Context: "conversations-sidebar"},
		{Key: "/", Command: "search", Context: "conversations-sidebar"},
		{Key: "r", Command: "refresh", Context: "conversations-sidebar"},
		{Key: "U", Command: "analytics", Context: "conversations-sidebar"},
		{Key: "j", Command: "cursor-down", Context: "conversations-sidebar"},
		{Key: "k", Command: "cursor-up", Context: "conversations-sidebar"},
		{Key: "down", Command: "cursor-down", Context: "conversations-sidebar"},
		{Key: "up", Command: "cursor-up", Context: "conversations-sidebar"},
		{Key: "g", Command: "cursor-top", Context: "conversations-sidebar"},
		{Key: "G", Command: "cursor-bottom", Context: "conversations-sidebar"},
		{Key: "l", Command: "focus-right", Context: "conversations-sidebar"},
		{Key: "right", Command: "focus-right", Context: "conversations-sidebar"},

		// Conversations main context (two-pane mode, right pane focused)
		{Key: "esc", Command: "back", Context: "conversations-main"},
		{Key: "q", Command: "back", Context: "conversations-main"},
		{Key: "j", Command: "scroll", Context: "conversations-main"},
		{Key: "k", Command: "scroll", Context: "conversations-main"},
		{Key: "g", Command: "cursor-top", Context: "conversations-main"},
		{Key: "G", Command: "cursor-bottom", Context: "conversations-main"},
		{Key: "h", Command: "focus-left", Context: "conversations-main"},
		{Key: "left", Command: "focus-left", Context: "conversations-main"},

		// File browser tree context
		{Key: "/", Command: "search", Context: "file-browser-tree"},

		// File browser preview context
		{Key: "/", Command: "search-content", Context: "file-browser-preview"},
		{Key: "esc", Command: "back", Context: "file-browser-preview"},
		{Key: "h", Command: "back", Context: "file-browser-preview"},

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
		{Key: "ctrl+p", Command: "quick-open", Context: "file-browser-tree"},
		{Key: "ctrl+p", Command: "quick-open", Context: "file-browser-preview"},
		{Key: "esc", Command: "cancel", Context: "file-browser-quick-open"},
		{Key: "enter", Command: "select", Context: "file-browser-quick-open"},
		{Key: "up", Command: "cursor-up", Context: "file-browser-quick-open"},
		{Key: "down", Command: "cursor-down", Context: "file-browser-quick-open"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "file-browser-quick-open"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "file-browser-quick-open"},

		// Git Commit context (commit message editor)
		{Key: "esc", Command: "cancel", Context: "git-commit"},
		{Key: "alt+enter", Command: "execute-commit", Context: "git-commit"},
		{Key: "alt+s", Command: "execute-commit", Context: "git-commit"},
	}
}

// RegisterDefaults registers all default bindings with the registry.
func RegisterDefaults(r *Registry) {
	for _, b := range DefaultBindings() {
		r.RegisterBinding(b)
	}
}
