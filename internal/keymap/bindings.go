package keymap

// DefaultBindings returns the default key bindings.
func DefaultBindings() []Binding {
	return []Binding{
		// Global bindings
		{Key: "q", Command: "quit", Context: "global"},
		{Key: "ctrl+c", Command: "quit", Context: "global"},
		{Key: "`", Command: "next-plugin", Context: "global"},
		{Key: "~", Command: "prev-plugin", Context: "global"},
		{Key: "1", Command: "focus-plugin-1", Context: "global"},
		{Key: "2", Command: "focus-plugin-2", Context: "global"},
		{Key: "3", Command: "focus-plugin-3", Context: "global"},
		{Key: "4", Command: "focus-plugin-4", Context: "global"},
		{Key: "5", Command: "focus-plugin-5", Context: "global"},
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
		{Key: "tab", Command: "switch-pane", Context: "git-status"},
		{Key: "shift+tab", Command: "switch-pane", Context: "git-status"},
		{Key: "s", Command: "stage-file", Context: "git-status"},
		{Key: "u", Command: "unstage-file", Context: "git-status"},
		{Key: "S", Command: "stage-all", Context: "git-status"},
		{Key: "c", Command: "commit", Context: "git-status"},
		{Key: "d", Command: "show-diff", Context: "git-status"},
		{Key: "v", Command: "toggle-diff-mode", Context: "git-status"},
		{Key: "h", Command: "show-history", Context: "git-status"},
		{Key: "o", Command: "open-file", Context: "git-status"},
		{Key: "O", Command: "open-in-file-browser", Context: "git-status"},
		{Key: "enter", Command: "show-diff", Context: "git-status"},
		{Key: "D", Command: "discard-changes", Context: "git-status"},
		{Key: "z", Command: "stash", Context: "git-status"},
		{Key: "Z", Command: "stash-pop", Context: "git-status"},
		{Key: "b", Command: "branch-picker", Context: "git-status"},
		{Key: "f", Command: "fetch", Context: "git-status"},
		{Key: "p", Command: "pull", Context: "git-status"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-status"},

		// Git Status commits context (recent commits in sidebar)
		{Key: "tab", Command: "switch-pane", Context: "git-status-commits"},
		{Key: "shift+tab", Command: "switch-pane", Context: "git-status-commits"},
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
		{Key: "v", Command: "toggle-graph", Context: "git-status-commits"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-status-commits"},
		{Key: "o", Command: "open-in-github", Context: "git-status-commits"},

		// Git commit preview context (commit preview in right pane)
		{Key: "tab", Command: "switch-pane", Context: "git-commit-preview"},
		{Key: "shift+tab", Command: "switch-pane", Context: "git-commit-preview"},
		{Key: "esc", Command: "back", Context: "git-commit-preview"},
		{Key: "h", Command: "back", Context: "git-commit-preview"},
		{Key: "enter", Command: "view-diff", Context: "git-commit-preview"},
		{Key: "d", Command: "view-diff", Context: "git-commit-preview"},
		{Key: "y", Command: "yank-commit", Context: "git-commit-preview"},
		{Key: "Y", Command: "yank-id", Context: "git-commit-preview"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-commit-preview"},
		{Key: "o", Command: "open-in-github", Context: "git-commit-preview"},
		{Key: "b", Command: "open-in-file-browser", Context: "git-commit-preview"},

		// Git Diff context
		{Key: "esc", Command: "close-diff", Context: "git-diff"},
		{Key: "j", Command: "scroll", Context: "git-diff"},
		{Key: "k", Command: "scroll", Context: "git-diff"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-diff"},
		{Key: "v", Command: "toggle-diff-view", Context: "git-diff"},
		{Key: "O", Command: "open-in-file-browser", Context: "git-diff"},

		// Git Status Diff Pane context (inline diff in three-pane view)
		{Key: "tab", Command: "switch-pane", Context: "git-status-diff"},
		{Key: "shift+tab", Command: "switch-pane", Context: "git-status-diff"},
		{Key: "v", Command: "toggle-diff-view", Context: "git-status-diff"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-status-diff"},

		// TD Monitor bindings are registered dynamically by the TD plugin
		// via ctx.Keymap.RegisterPluginBinding() in Init()
		// This keeps TD as the single source of truth for shortcuts

		// Conversations context
		{Key: "enter", Command: "view-session", Context: "conversations"},
		{Key: "/", Command: "search", Context: "conversations"},
		{Key: "r", Command: "refresh", Context: "conversations"},
		{Key: "U", Command: "analytics", Context: "conversations"},
		{Key: "y", Command: "yank-details", Context: "conversations"},
		{Key: "Y", Command: "yank-resume", Context: "conversations"},

		// Conversations search context
		{Key: "enter", Command: "select", Context: "conversations-search"},
		{Key: "esc", Command: "cancel", Context: "conversations-search"},
		{Key: "up", Command: "cursor-up", Context: "conversations-search"},
		{Key: "down", Command: "cursor-down", Context: "conversations-search"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "conversations-search"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "conversations-search"},

		// Conversation detail context (turn list view)
		{Key: "esc", Command: "back", Context: "conversation-detail"},
		{Key: "q", Command: "back", Context: "conversation-detail"},
		{Key: "j", Command: "scroll", Context: "conversation-detail"},
		{Key: "k", Command: "scroll", Context: "conversation-detail"},
		{Key: "g", Command: "cursor-top", Context: "conversation-detail"},
		{Key: "G", Command: "cursor-bottom", Context: "conversation-detail"},
		{Key: "y", Command: "yank-details", Context: "conversation-detail"},
		{Key: "Y", Command: "yank-resume", Context: "conversation-detail"},

		// Message detail context (single turn detail view - single-pane mode)
		{Key: "esc", Command: "back", Context: "message-detail"},
		{Key: "q", Command: "back", Context: "message-detail"},
		{Key: "j", Command: "scroll", Context: "message-detail"},
		{Key: "k", Command: "scroll", Context: "message-detail"},
		{Key: "g", Command: "cursor-top", Context: "message-detail"},
		{Key: "G", Command: "cursor-bottom", Context: "message-detail"},
		{Key: "ctrl+d", Command: "page-down", Context: "message-detail"},
		{Key: "ctrl+u", Command: "page-up", Context: "message-detail"},
		{Key: "y", Command: "yank-details", Context: "message-detail"},
		{Key: "Y", Command: "yank-resume", Context: "message-detail"},

		// Turn detail context (two-pane mode, detail in right pane)
		{Key: "esc", Command: "back", Context: "turn-detail"},
		{Key: "q", Command: "back", Context: "turn-detail"},
		{Key: "h", Command: "back", Context: "turn-detail"},
		{Key: "left", Command: "back", Context: "turn-detail"},
		{Key: "j", Command: "scroll-down", Context: "turn-detail"},
		{Key: "k", Command: "scroll-up", Context: "turn-detail"},
		{Key: "down", Command: "scroll-down", Context: "turn-detail"},
		{Key: "up", Command: "scroll-up", Context: "turn-detail"},
		{Key: "g", Command: "scroll-top", Context: "turn-detail"},
		{Key: "G", Command: "scroll-bottom", Context: "turn-detail"},
		{Key: "ctrl+d", Command: "page-down", Context: "turn-detail"},
		{Key: "ctrl+u", Command: "page-up", Context: "turn-detail"},
		{Key: "y", Command: "yank", Context: "turn-detail"},

		// Conversations sidebar context (two-pane mode, left pane focused)
		{Key: "tab", Command: "switch-pane", Context: "conversations-sidebar"},
		{Key: "shift+tab", Command: "switch-pane", Context: "conversations-sidebar"},
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
		{Key: "\\", Command: "toggle-sidebar", Context: "conversations-sidebar"},
		{Key: "y", Command: "yank-details", Context: "conversations-sidebar"},
		{Key: "Y", Command: "yank-resume", Context: "conversations-sidebar"},

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

		// File browser tree context
		{Key: "tab", Command: "switch-pane", Context: "file-browser-tree"},
		{Key: "shift+tab", Command: "switch-pane", Context: "file-browser-tree"},
		{Key: "/", Command: "search", Context: "file-browser-tree"},
		{Key: "ctrl+p", Command: "quick-open", Context: "file-browser-tree"},
		{Key: "ctrl+s", Command: "project-search", Context: "file-browser-tree"},
		{Key: "a", Command: "create-file", Context: "file-browser-tree"},
		{Key: "A", Command: "create-dir", Context: "file-browser-tree"},
		{Key: "d", Command: "delete", Context: "file-browser-tree"},
		{Key: "y", Command: "yank", Context: "file-browser-tree"},
		{Key: "Y", Command: "copy-path", Context: "file-browser-tree"},
		{Key: "p", Command: "paste", Context: "file-browser-tree"},
		{Key: "s", Command: "sort", Context: "file-browser-tree"},
		{Key: "r", Command: "rename", Context: "file-browser-tree"},
		{Key: "m", Command: "move", Context: "file-browser-tree"},
		{Key: "R", Command: "reveal", Context: "file-browser-tree"},
		{Key: "i", Command: "info", Context: "file-browser-tree"},
		{Key: "B", Command: "blame", Context: "file-browser-tree"},
		{Key: "\\", Command: "toggle-sidebar", Context: "file-browser-tree"},

		// File browser preview context
		{Key: "tab", Command: "switch-pane", Context: "file-browser-preview"},
		{Key: "shift+tab", Command: "switch-pane", Context: "file-browser-preview"},
		{Key: "/", Command: "search-content", Context: "file-browser-preview"},
		{Key: "ctrl+p", Command: "quick-open", Context: "file-browser-preview"},
		{Key: "ctrl+s", Command: "project-search", Context: "file-browser-preview"},
		{Key: "R", Command: "reveal", Context: "file-browser-preview"},
		{Key: "i", Command: "info", Context: "file-browser-preview"},
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
		{Key: "tab", Command: "toggle", Context: "file-browser-project-search"},
		{Key: " ", Command: "toggle", Context: "file-browser-project-search"},
		{Key: "up", Command: "cursor-up", Context: "file-browser-project-search"},
		{Key: "down", Command: "cursor-down", Context: "file-browser-project-search"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "file-browser-project-search"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "file-browser-project-search"},

		// File browser file operation context (move/rename/create/delete)
		{Key: "esc", Command: "cancel", Context: "file-browser-file-op"},
		{Key: "enter", Command: "confirm", Context: "file-browser-file-op"},

		// File browser blame context
		{Key: "esc", Command: "close", Context: "file-browser-blame"},
		{Key: "q", Command: "close", Context: "file-browser-blame"},
		{Key: "j", Command: "scroll-down", Context: "file-browser-blame"},
		{Key: "k", Command: "scroll-up", Context: "file-browser-blame"},
		{Key: "down", Command: "scroll-down", Context: "file-browser-blame"},
		{Key: "up", Command: "scroll-up", Context: "file-browser-blame"},
		{Key: "g", Command: "scroll-top", Context: "file-browser-blame"},
		{Key: "G", Command: "scroll-bottom", Context: "file-browser-blame"},
		{Key: "ctrl+d", Command: "page-down", Context: "file-browser-blame"},
		{Key: "ctrl+u", Command: "page-up", Context: "file-browser-blame"},
		{Key: "enter", Command: "view-commit", Context: "file-browser-blame"},
		{Key: "y", Command: "yank-hash", Context: "file-browser-blame"},

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
