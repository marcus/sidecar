package keymap

// DefaultBindings returns the default keymap.
func DefaultBindings() []Binding {
	bindings := []Binding{
		// Global context
		{Key: "q", Command: "quit", Context: "global"},
		{Key: "ctrl+c", Command: "quit", Context: "global"},
		{Key: "?", Command: "toggle-palette", Context: "global"},
		{Key: "!", Command: "toggle-diagnostics", Context: "global"},
		{Key: "`", Command: "next-plugin", Context: "global"},
		{Key: "~", Command: "prev-plugin", Context: "global"},
		{Key: "[", Command: "prev-plugin", Context: "global"},
		{Key: "]", Command: "next-plugin", Context: "global"},
		{Key: "@", Command: "switch-project", Context: "global"},
		{Key: "W", Command: "switch-worktree", Context: "global"},
		{Key: "#", Command: "switch-theme", Context: "global"},
		{Key: "^", Command: "open-in", Context: "global"},
		// K: Overview (Kanban board). Bare O is taken by open-in. Shell delete is D.
		{Key: "K", Command: "toggle-overview", Context: "global"},
		{Key: "i", Command: "open-issue", Context: "global"},
		// N: the notification centre. Bare n is taken (new-worktree, new-note,
		// next-match); N is free globally — the search/diff contexts that bind
		// it answer before the global switch. Handled in handleKeyMsg; the
		// binding is registered so the palette and help can find it.
		{Key: "N", Command: "toggle-notifications", Context: "global"},
		// `N` yields to plugins that bind it (git's prev-match), and the centre
		// has no navbar tab, so alt+n is the route that works on every tab.
		{Key: "alt+n", Command: "toggle-notifications", Context: "global"},
		// alt+e opens a collapsed toast stack (design 1b's peek line). The
		// design's key was tab, which the centre's focus cycle now owns; alt+e
		// keeps the expand affordance in the same family as alt+n and out of
		// every plugin's way. Handled in handleKeyMsg; registered so the
		// palette, help and rebinding find it.
		{Key: "alt+e", Command: "expand-toast", Context: "global"},
		// Comma is the conventional settings key and is otherwise unbound.
		{Key: ",", Command: "open-configuration", Context: "global"},
		{Key: "r", Command: "refresh", Context: "global"},
		{Key: "1", Command: "focus-plugin-1", Context: "global"},
		{Key: "2", Command: "focus-plugin-2", Context: "global"},
		{Key: "3", Command: "focus-plugin-3", Context: "global"},
		{Key: "4", Command: "focus-plugin-4", Context: "global"},
		{Key: "5", Command: "focus-plugin-5", Context: "global"},
		{Key: "6", Command: "focus-plugin-6", Context: "global"},
		{Key: "7", Command: "focus-plugin-7", Context: "global"},
		// 8/9/0 address the header's global entries by name. They are not
		// positional tab shortcuts: the project tabs stop at 7 so that Sessions,
		// Activity and Tasks keep one meaning in every scope, and so a disabled
		// Tasks tab cannot slide another entry onto `0`.
		{Key: "8", Command: "focus-sessions", Context: "global"},
		{Key: "9", Command: "focus-activity", Context: "global"},
		{Key: "0", Command: "focus-tasks", Context: "global"},

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

		// Overview context (cross-project agent board). These keys are handled
		// by the app before keymap dispatch; they are registered so the command
		// palette and footer can discover them.
		{Key: "h", Command: "cursor-left", Context: "overview"},
		{Key: "l", Command: "cursor-right", Context: "overview"},
		{Key: "left", Command: "cursor-left", Context: "overview"},
		{Key: "right", Command: "cursor-right", Context: "overview"},
		{Key: "j", Command: "cursor-down", Context: "overview"},
		{Key: "k", Command: "cursor-up", Context: "overview"},
		{Key: "down", Command: "cursor-down", Context: "overview"},
		{Key: "up", Command: "cursor-up", Context: "overview"},
		{Key: "enter", Command: "select", Context: "overview"},
		{Key: "r", Command: "refresh", Context: "overview"},
		{Key: "esc", Command: "close-overview", Context: "overview"},
		{Key: "K", Command: "toggle-overview", Context: "overview"},

		// Configuration contexts. The surface answers these keys itself before
		// keymap dispatch; they are registered so the footer, the help modal,
		// and the command palette describe the same surface the user sees, and
		// so a user override reaches them.
		//
		// config: sidebar navigation and page-level actions.
		{Key: "up", Command: "cursor-up", Context: "config"},
		{Key: "k", Command: "cursor-up", Context: "config"},
		{Key: "down", Command: "cursor-down", Context: "config"},
		{Key: "j", Command: "cursor-down", Context: "config"},
		{Key: "enter", Command: "select", Context: "config"},
		{Key: "/", Command: "search", Context: "config"},
		{Key: "tab", Command: "focus-search", Context: "config"},
		{Key: "esc", Command: "close-configuration", Context: "config"},
		// q leaves the surface the way it leaves a pager. It is registered
		// rather than hardcoded so it is described everywhere esc is and a
		// rebind of close-configuration takes it with it.
		{Key: "q", Command: "close-configuration", Context: "config"},
		// Page-level actions on Sidecar Setup, Diagnostics, and the focused
		// repair routes. They are registered here so the footer, the help modal,
		// and the palette all name the same keys the pages print on their pills.
		{Key: "r", Command: "recheck", Context: "config"},
		{Key: "c", Command: "copy-guidance", Context: "config"},
		{Key: "o", Command: "open-file", Context: "config"},
		// Projects: add, remove, and reorder the configured list.
		{Key: "a", Command: "add-project", Context: "config"},
		{Key: "d", Command: "remove-project", Context: "config"},
		{Key: "shift+up", Command: "move-project-up", Context: "config"},
		{Key: "shift+down", Command: "move-project-down", Context: "config"},
		{Key: "[", Command: "move-project-up", Context: "config"},
		{Key: "]", Command: "move-project-down", Context: "config"},
		// The theme picker, on Appearance and inline in Add Project.
		{Key: "g", Command: "use-global-theme", Context: "config"},

		// notification-centre: the app-level right panel. Its keys are answered
		// by the app before keymap dispatch (it is the focused surface, not a
		// plugin); they are registered here so help, the palette, and the
		// footer describe it from one source and a user override can rebind it.
		{Key: "j", Command: "cursor-down", Context: "notification-centre"},
		{Key: "down", Command: "cursor-down", Context: "notification-centre"},
		{Key: "k", Command: "cursor-up", Context: "notification-centre"},
		{Key: "up", Command: "cursor-up", Context: "notification-centre"},
		// enter activates the selected notification's first call to action —
		// the jump the notification is about (Phase 5). On an entry with no
		// target it falls back to the detail re-show, which `v` is now the
		// dedicated key for.
		{Key: "enter", Command: "select", Context: "notification-centre"},
		{Key: "v", Command: "show-details", Context: "notification-centre"},
		// Digits 1-9 jump to the numbered target of the selected entry. Only
		// "1" is registered: the panel answers the whole range itself (as the
		// shell does for its own globals), and registering nine rows would say
		// nine things in help about one behaviour. A digit with no target of
		// that number is left alone and stays the project tab it is elsewhere.
		{Key: "1", Command: "jump-target", Context: "notification-centre"},
		{Key: "d", Command: "dismiss", Context: "notification-centre"},
		{Key: "D", Command: "dismiss-group", Context: "notification-centre"},
		{Key: "esc", Command: "close-notification-centre", Context: "notification-centre"},
		// The panel is a stop on the focus cycle, so tab moves on from it the
		// way it moves on from any pane — back to the surface underneath,
		// leaving the panel open.
		{Key: "tab", Command: "focus-content", Context: "notification-centre"},
		{Key: "shift+tab", Command: "focus-content", Context: "notification-centre"},

		// config-edit: an active editor owns typed characters. Registered in
		// the app's isTextInputContext, so no global shortcut can steal them.
		{Key: "down", Command: "first-result", Context: "config-edit"},
		{Key: "up", Command: "focus-search", Context: "config-edit"},
		{Key: "enter", Command: "select", Context: "config-edit"},
		{Key: "esc", Command: "clear-search", Context: "config-edit"},
		// A form field: Tab accepts a completion or moves to the next field,
		// and the arrows walk the candidates under the input.
		{Key: "tab", Command: "accept-completion", Context: "config-edit"},

		// config-confirm: a consequential change has an explicit path out.
		{Key: "enter", Command: "confirm", Context: "config-confirm"},
		{Key: "y", Command: "confirm", Context: "config-confirm"},
		{Key: "esc", Command: "cancel", Context: "config-confirm"},
		{Key: "n", Command: "cancel", Context: "config-confirm"},

		// Global Workspaces context (cross-project shell/worktree browser).
		// Like the Agents board above, these keys are answered by the app before
		// keymap dispatch; they are registered so help, the palette, and the
		// footer can discover them.
		//
		// Creation is hosted globally, but delegates lifecycle work to the same
		// presentation-neutral core as the project surface.
		{Key: "enter", Command: "interactive", Context: "global-workspaces"},
		{Key: "n", Command: "new-worktree", Context: "global-workspaces"},
		{Key: "ctrl+n", Command: "new-shell", Context: "global-workspaces"},
		// D acts on the selection's kind, as it does in the project list: the
		// footer advertises whichever of the two the selected row answers.
		{Key: "D", Command: "delete-shell", Context: "global-workspaces"},
		{Key: "D", Command: "delete-worktree", Context: "global-workspaces"},
		{Key: "m", Command: "merge-workflow", Context: "global-workspaces"},
		{Key: "/", Command: "filter", Context: "global-workspaces"},
		{Key: "s", Command: "sort", Context: "global-workspaces"},
		{Key: "p", Command: "pin", Context: "global-workspaces"},
		{Key: "r", Command: "refresh", Context: "global-workspaces"},
		{Key: "j", Command: "cursor-down", Context: "global-workspaces"},
		{Key: "k", Command: "cursor-up", Context: "global-workspaces"},
		{Key: "down", Command: "cursor-down", Context: "global-workspaces"},
		{Key: "up", Command: "cursor-up", Context: "global-workspaces"},
		{Key: "g", Command: "cursor-top", Context: "global-workspaces"},
		{Key: "G", Command: "cursor-bottom", Context: "global-workspaces"},
		{Key: "h", Command: "focus-list", Context: "global-workspaces"},
		{Key: "left", Command: "focus-list", Context: "global-workspaces"},
		{Key: "ctrl+d", Command: "scroll-preview-down", Context: "global-workspaces"},
		{Key: "ctrl+u", Command: "scroll-preview-up", Context: "global-workspaces"},
		{Key: "esc", Command: "close-overview", Context: "global-workspaces"},
		{Key: "K", Command: "toggle-overview", Context: "global-workspaces"},
		{Key: "\\", Command: "toggle-sidebar", Context: "global-workspaces"},
		// Tab cycles the windows on screen — list, terminal, and any document or
		// issue leaf beside it — as it does on the project surface.
		{Key: "tab", Command: "switch-pane", Context: "global-workspaces"},
		{Key: "shift+tab", Command: "switch-pane", Context: "global-workspaces"},
		// Enter from the list starts typing. E is the remaining explicit
		// alternate. i is Sidecar's find-TD-task shortcut, not a way in.
		{Key: "E", Command: "interactive", Context: "global-workspaces"},
		// Display-name write, not create/destroy. Branch and path stay put.
		{Key: "R", Command: "rename-shell", Context: "global-workspaces"},
		{Key: "R", Command: "rename-worktree", Context: "global-workspaces"},
		// Navigation: leave global and open the project's Git plugin.
		{Key: "O", Command: "open-in-git", Context: "global-workspaces"},

		// Rename prompt. Enter confirms; esc cancels. The query is a text input.
		{Key: "enter", Command: "confirm", Context: "global-workspaces-rename"},
		{Key: "esc", Command: "cancel", Context: "global-workspaces-rename"},
		{Key: "enter", Command: "confirm", Context: "global-workspaces-create"},
		{Key: "esc", Command: "cancel", Context: "global-workspaces-create"},
		{Key: "enter", Command: "confirm-delete", Context: "global-workspaces-delete"},
		{Key: "D", Command: "confirm-delete", Context: "global-workspaces-delete"},
		{Key: "esc", Command: "cancel", Context: "global-workspaces-delete"},

		// Focused document leaf beside the selected terminal. q closes the
		// pane; it must not be a root context or Sidecar would quit instead.
		{Key: "q", Command: "close", Context: "global-workspaces-doc"},
		{Key: "esc", Command: "close", Context: "global-workspaces-doc"},
		{Key: "x", Command: "close-tab", Context: "global-workspaces-doc"},
		{Key: "{", Command: "prev-tab", Context: "global-workspaces-doc"},
		{Key: "}", Command: "next-tab", Context: "global-workspaces-doc"},
		{Key: "m", Command: "render", Context: "global-workspaces-doc"},
		{Key: "Y", Command: "yank-path", Context: "global-workspaces-doc"},
		{Key: "tab", Command: "switch-pane", Context: "global-workspaces-doc"},
		{Key: "shift+tab", Command: "switch-pane", Context: "global-workspaces-doc"},

		// Focused td issue leaf. Tab keys match global-workspaces-doc;
		// y/Y match td monitor and the project issue pane.
		{Key: "enter", Command: "open-item", Context: "global-workspaces-issue"},
		{Key: "O", Command: "open-in-td", Context: "global-workspaces-issue"},
		{Key: "y", Command: "yank-issue", Context: "global-workspaces-issue"},
		{Key: "Y", Command: "yank-issue-key", Context: "global-workspaces-issue"},
		{Key: "q", Command: "close", Context: "global-workspaces-issue"},
		{Key: "esc", Command: "close", Context: "global-workspaces-issue"},
		{Key: "x", Command: "close-tab", Context: "global-workspaces-issue"},
		{Key: "{", Command: "prev-tab", Context: "global-workspaces-issue"},
		{Key: "}", Command: "next-tab", Context: "global-workspaces-issue"},
		{Key: "tab", Command: "switch-pane", Context: "global-workspaces-issue"},
		{Key: "shift+tab", Command: "switch-pane", Context: "global-workspaces-issue"},

		// Focused external-resource leaf. The command IDs after close are
		// resourceview's own key vocabulary, which both terminal surfaces
		// register, so the footer cannot advertise different keys for the
		// same pane. q/esc are this surface's content-pane rule, as above.
		{Key: "q", Command: "close", Context: "global-workspaces-resource"},
		{Key: "esc", Command: "close", Context: "global-workspaces-resource"},
		{Key: "r", Command: "refresh", Context: "global-workspaces-resource"},
		{Key: "o", Command: "open-source", Context: "global-workspaces-resource"},
		{Key: "x", Command: "close-tab", Context: "global-workspaces-resource"},
		{Key: "{", Command: "prev-tab", Context: "global-workspaces-resource"},
		{Key: "}", Command: "next-tab", Context: "global-workspaces-resource"},
		{Key: "tab", Command: "switch-pane", Context: "global-workspaces-resource"},
		{Key: "shift+tab", Command: "switch-pane", Context: "global-workspaces-resource"},

		// The two search surfaces a focused document pane can open on itself.
		// Both are rooted at the pane's own directory; the rest of the pane's
		// keys are registered by the plugin.
		{Key: "ctrl+p", Command: "find-file", Context: "workspace-doc"},
		{Key: "f", Command: "search-project", Context: "workspace-doc"},

		// While one of them is open it owns every key in the pane, so only the
		// ways out and in are named. It is not a root context: esc closes the
		// search rather than leaving the plugin.
		{Key: "esc", Command: "search-cancel", Context: "workspace-doc-search"},
		{Key: "enter", Command: "search-open", Context: "workspace-doc-search"},
		{Key: "shift+enter", Command: "search-open-tab", Context: "workspace-doc-search"},

		// In-file search (`/`) is the third search a document pane can run, and
		// the only one that stays inside the document: its bar is docview's, so
		// both pane surfaces answer the same keys. workspace-doc-find and
		// global-workspaces-doc-find are the two hosts' names for that one bar,
		// which is why they register identically.
		{Key: "/", Command: "search-content", Context: "workspace-doc"},
		{Key: "/", Command: "search-content", Context: "global-workspaces-doc"},
		{Key: "ctrl+p", Command: "find-file", Context: "global-workspaces-doc"},
		{Key: "f", Command: "search-project", Context: "global-workspaces-doc"},

		// Inline edit (`e`) is the Files plugin's key for the same act, and both
		// pane surfaces answer it. While a session is live every key belongs to
		// the editor, so the edit contexts register nothing: the ways out are
		// tmux's own (ctrl+\, esc esc).
		{Key: "e", Command: "edit", Context: "workspace-doc"},
		{Key: "e", Command: "edit", Context: "global-workspaces-doc"},

		{Key: "enter", Command: "confirm", Context: "workspace-doc-find"},
		{Key: "n", Command: "next-match", Context: "workspace-doc-find"},
		{Key: "N", Command: "prev-match", Context: "workspace-doc-find"},
		{Key: "esc", Command: "cancel", Context: "workspace-doc-find"},
		{Key: "enter", Command: "confirm", Context: "global-workspaces-doc-find"},
		{Key: "n", Command: "next-match", Context: "global-workspaces-doc-find"},
		{Key: "N", Command: "prev-match", Context: "global-workspaces-doc-find"},
		{Key: "esc", Command: "cancel", Context: "global-workspaces-doc-find"},

		// The finder and project search a global-Workspaces document pane can
		// open on itself, matching workspace-doc-search above.
		{Key: "esc", Command: "search-cancel", Context: "global-workspaces-doc-search"},
		{Key: "enter", Command: "search-open", Context: "global-workspaces-doc-search"},
		{Key: "shift+enter", Command: "search-open-tab", Context: "global-workspaces-doc-search"},

		{Key: "q", Command: "close", Context: "global-workspaces-diff"},
		{Key: "esc", Command: "close", Context: "global-workspaces-diff"},
		{Key: "x", Command: "close-tab", Context: "global-workspaces-diff"},
		{Key: "{", Command: "prev-tab", Context: "global-workspaces-diff"},
		{Key: "}", Command: "next-tab", Context: "global-workspaces-diff"},
		{Key: ",", Command: "prev-file", Context: "global-workspaces-diff"},
		{Key: ".", Command: "next-file", Context: "global-workspaces-diff"},
		{Key: "Y", Command: "yank-id", Context: "global-workspaces-diff"},
		{Key: "tab", Command: "switch-pane", Context: "global-workspaces-diff"},
		{Key: "shift+tab", Command: "switch-pane", Context: "global-workspaces-diff"},
		{Key: "v", Command: "toggle-diff-view", Context: "global-workspaces-diff"},
		{Key: "z", Command: "toggle-diff-scope", Context: "global-workspaces-diff"},

		// Focused project Workspaces issue leaf. Tab keys match workspace-doc.
		{Key: "enter", Command: "open-item", Context: "workspace-issue"},
		{Key: "O", Command: "open-in-td", Context: "workspace-issue"},
		{Key: "y", Command: "yank-issue", Context: "workspace-issue"},
		{Key: "Y", Command: "yank-issue-key", Context: "workspace-issue"},
		{Key: "q", Command: "close", Context: "workspace-issue"},
		{Key: "esc", Command: "close", Context: "workspace-issue"},
		{Key: "x", Command: "close-tab", Context: "workspace-issue"},
		{Key: "{", Command: "prev-tab", Context: "workspace-issue"},
		{Key: "}", Command: "next-tab", Context: "workspace-issue"},
		{Key: "\\", Command: "toggle-sidebar", Context: "workspace-issue"},
		{Key: "tab", Command: "next-pane", Context: "workspace-issue"},
		{Key: "shift+tab", Command: "prev-pane", Context: "workspace-issue"},

		// The Resource leaf answers the same keys on this surface as it does
		// in the global Workspaces browser. The two blocks are siblings on
		// purpose: a key bound in one and not the other is the parity bug the
		// shared resourceview.Pane exists to prevent, and the footer renders
		// nothing for a command with no bound key.
		{Key: "q", Command: "close", Context: "workspace-resource"},
		{Key: "esc", Command: "close", Context: "workspace-resource"},
		{Key: "r", Command: "refresh", Context: "workspace-resource"},
		{Key: "o", Command: "open-source", Context: "workspace-resource"},
		{Key: "x", Command: "close-tab", Context: "workspace-resource"},
		{Key: "{", Command: "prev-tab", Context: "workspace-resource"},
		{Key: "}", Command: "next-tab", Context: "workspace-resource"},
		{Key: "\\", Command: "toggle-sidebar", Context: "workspace-resource"},
		{Key: "tab", Command: "next-pane", Context: "workspace-resource"},
		{Key: "shift+tab", Command: "prev-pane", Context: "workspace-resource"},

		// Focused project Workspaces Diff leaf. q hides; not a root context.
		{Key: "q", Command: "close", Context: "workspace-diff"},
		{Key: "esc", Command: "close", Context: "workspace-diff"},
		{Key: "x", Command: "close-tab", Context: "workspace-diff"},
		{Key: "{", Command: "prev-tab", Context: "workspace-diff"},
		{Key: "}", Command: "next-tab", Context: "workspace-diff"},
		{Key: ",", Command: "prev-file", Context: "workspace-diff"},
		{Key: ".", Command: "next-file", Context: "workspace-diff"},
		{Key: "Y", Command: "yank-id", Context: "workspace-diff"},
		{Key: "\\", Command: "toggle-sidebar", Context: "workspace-diff"},
		{Key: "tab", Command: "next-pane", Context: "workspace-diff"},
		{Key: "shift+tab", Command: "prev-pane", Context: "workspace-diff"},
		{Key: "+", Command: "resize-pane-grow", Context: "workspace-diff"},
		{Key: "-", Command: "resize-pane-shrink", Context: "workspace-diff"},
		{Key: "f", Command: "file-picker", Context: "workspace-diff"},
		{Key: "v", Command: "toggle-diff-view", Context: "workspace-diff"},
		{Key: "z", Command: "toggle-diff-scope", Context: "workspace-diff"},

		// The preview forwarding keys to a live pane. Almost every key is the
		// pane's, ctrl+c included, so only the acts that belong to the surface
		// around it are listed: the ways out and the terminal's own selection and
		// scrollback. The exit, copy and paste chords are configurable, so the app
		// registers those from config — the default table cannot read it.
		{Key: "ctrl+a", Command: "select-all", Context: "global-workspaces-terminal"},
		{Key: "shift+up", Command: "scrollback", Context: "global-workspaces-terminal"},
		{Key: "shift+down", Command: "scrollback", Context: "global-workspaces-terminal"},
		{Key: "shift+pgup", Command: "scrollback", Context: "global-workspaces-terminal"},
		{Key: "shift+pgdown", Command: "scrollback", Context: "global-workspaces-terminal"},

		// Global Workspaces filter context. While the query owns the keyboard it
		// is a text input: only these keys mean anything else, and navigation
		// stays live so a user can type, arrow onto a match, and press enter.
		{Key: "enter", Command: "filter-accept", Context: "global-workspaces-filter"},
		{Key: "esc", Command: "filter-clear", Context: "global-workspaces-filter"},
		{Key: "down", Command: "cursor-down", Context: "global-workspaces-filter"},
		{Key: "up", Command: "cursor-up", Context: "global-workspaces-filter"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "global-workspaces-filter"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "global-workspaces-filter"},

		// Project switcher context
		{Key: "@", Command: "toggle", Context: "project-switcher"},
		{Key: "esc", Command: "close", Context: "project-switcher"},
		{Key: "enter", Command: "select", Context: "project-switcher"},
		{Key: "down", Command: "cursor-down", Context: "project-switcher"},
		{Key: "up", Command: "cursor-up", Context: "project-switcher"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "project-switcher"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "project-switcher"},

		// Git status context
		{Key: "i", Command: "init-repo", Context: "git-no-repo"},
		{Key: "enter", Command: "init-repo", Context: "git-no-repo"},
		{Key: "r", Command: "refresh", Context: "git-no-repo"},

		{Key: "j", Command: "cursor-down", Context: "git-status"},
		{Key: "k", Command: "cursor-up", Context: "git-status"},
		{Key: "tab", Command: "switch-pane", Context: "git-status"},
		{Key: "shift+tab", Command: "switch-pane", Context: "git-status"},
		{Key: "l", Command: "focus-right", Context: "git-status"},
		{Key: "right", Command: "focus-right", Context: "git-status"},
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
		{Key: "ctrl+z", Command: "stash-apply", Context: "git-status"},
		{Key: "O", Command: "open-in-file-browser", Context: "git-status"},
		{Key: "o", Command: "open-in-github", Context: "git-status"},
		{Key: "y", Command: "yank-file", Context: "git-status"},
		{Key: "Y", Command: "yank-path", Context: "git-status"},
		{Key: "D", Command: "discard-changes", Context: "git-status"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-status"},
		{Key: "+", Command: "resize-pane-grow", Context: "git-status"},
		{Key: "-", Command: "resize-pane-shrink", Context: "git-status"},

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
		{Key: "L", Command: "pull", Context: "git-status-commits"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-status-commits"},
		{Key: "+", Command: "resize-pane-grow", Context: "git-status-commits"},
		{Key: "-", Command: "resize-pane-shrink", Context: "git-status-commits"},

		// Git history search modal context
		{Key: "enter", Command: "select", Context: "git-history-search"},
		{Key: "esc", Command: "cancel", Context: "git-history-search"},
		{Key: "j", Command: "navigate", Context: "git-history-search"},
		{Key: "k", Command: "navigate", Context: "git-history-search"},
		{Key: "down", Command: "navigate", Context: "git-history-search"},
		{Key: "up", Command: "navigate", Context: "git-history-search"},
		{Key: "alt+r", Command: "toggle-regex", Context: "git-history-search"},
		{Key: "alt+c", Command: "toggle-case", Context: "git-history-search"},

		// Git path filter modal context
		{Key: "enter", Command: "apply-filter", Context: "git-path-filter"},
		{Key: "esc", Command: "cancel", Context: "git-path-filter"},

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
		{Key: "w", Command: "toggle-wrap", Context: "git-status-diff"},
		{Key: "|", Command: "reset-hscroll", Context: "git-status-diff"},
		{Key: "+", Command: "resize-pane-grow", Context: "git-status-diff"},
		{Key: "-", Command: "resize-pane-shrink", Context: "git-status-diff"},

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
		{Key: "+", Command: "resize-pane-grow", Context: "git-commit-preview"},
		{Key: "-", Command: "resize-pane-shrink", Context: "git-commit-preview"},

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
		{Key: ",", Command: "prev-file", Context: "git-diff"},
		{Key: ".", Command: "next-file", Context: "git-diff"},
		{Key: "y", Command: "yank-diff", Context: "git-diff"},
		{Key: "c", Command: "commit", Context: "git-diff"},
		{Key: "v", Command: "toggle-diff-view", Context: "git-diff"},
		{Key: "\\", Command: "toggle-sidebar", Context: "git-diff"},
		{Key: "w", Command: "toggle-wrap", Context: "git-diff"},

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

		// Issue preview context
		// Issue input modal context
		{Key: "ctrl+x", Command: "toggle-closed", Context: "issue-input"},

		{Key: "o", Command: "open-in-td", Context: "issue-preview"},
		{Key: "b", Command: "issue-back", Context: "issue-preview"},
		{Key: "y", Command: "yank-issue", Context: "issue-preview"},
		{Key: "Y", Command: "yank-issue-key", Context: "issue-preview"},
		{Key: "esc", Command: "close", Context: "issue-preview"},

		// Git error modal context
		{Key: "L", Command: "pull-from-error", Context: "git-error"},
		{Key: "y", Command: "yank-error", Context: "git-error"},
		{Key: "esc", Command: "dismiss", Context: "git-error"},

		// Git pull conflict context
		{Key: "a", Command: "abort-pull", Context: "git-pull-conflict"},
		{Key: "esc", Command: "dismiss", Context: "git-pull-conflict"},

		// Git stash pop context
		{Key: "y", Command: "confirm-pop", Context: "git-stash-pop"},
		{Key: "esc", Command: "dismiss", Context: "git-stash-pop"},

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
		// Content search expands every session on alt+e. The key is handled in
		// the plugin, but it has to be registered: the global alt+e (expand a
		// collapsed toast stack) stands aside only for a context that has
		// claimed the key, and an unregistered claim is invisible to that rule.
		{Key: "alt+e", Command: "expand-all", Context: "conversations-content-search"},
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
		{Key: "C", Command: "toggle-category", Context: "conversations-sidebar"},
		{Key: "R", Command: "resume-in-workspace", Context: "conversations-sidebar"},
		{Key: "+", Command: "resize-pane-grow", Context: "conversations-sidebar"},
		{Key: "-", Command: "resize-pane-shrink", Context: "conversations-sidebar"},

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
		{Key: "+", Command: "resize-pane-grow", Context: "conversations-main"},
		{Key: "-", Command: "resize-pane-shrink", Context: "conversations-main"},

		// File browser tree context
		{Key: "tab", Command: "switch-pane", Context: "file-browser-tree"},
		{Key: "shift+tab", Command: "switch-pane", Context: "file-browser-tree"},
		{Key: "l", Command: "focus-right", Context: "file-browser-tree"},
		{Key: "right", Command: "focus-right", Context: "file-browser-tree"},
		{Key: "/", Command: "search", Context: "file-browser-tree"},
		// The same two search surfaces a workspace file pane opens, on the same
		// two keys and under the same two names: ctrl+p Find finds a file by
		// name, f Search searches the project's contents. The names say what the
		// feature does, so the two surfaces cannot drift apart again.
		{Key: "ctrl+p", Command: "quick-open", Context: "file-browser-tree"},
		{Key: "f", Command: "project-search", Context: "file-browser-tree"},
		{Key: "t", Command: "new-tab", Context: "file-browser-tree"},
		{Key: "{", Command: "prev-tab", Context: "file-browser-tree"},
		{Key: "}", Command: "next-tab", Context: "file-browser-tree"},
		{Key: "x", Command: "close-tab", Context: "file-browser-tree"},
		{Key: "a", Command: "create-file", Context: "file-browser-tree"},
		{Key: "A", Command: "create-dir", Context: "file-browser-tree"},
		{Key: "D", Command: "delete", Context: "file-browser-tree"},
		{Key: "y", Command: "yank", Context: "file-browser-tree"},
		{Key: "Y", Command: "copy-path", Context: "file-browser-tree"},
		{Key: "p", Command: "paste", Context: "file-browser-tree"},
		{Key: "s", Command: "sort", Context: "file-browser-tree"},
		{Key: "r", Command: "refresh", Context: "file-browser-tree"},
		{Key: "m", Command: "move", Context: "file-browser-tree"},
		{Key: "R", Command: "rename", Context: "file-browser-tree"},
		{Key: "ctrl+r", Command: "reveal", Context: "file-browser-tree"},
		{Key: "I", Command: "info", Context: "file-browser-tree"},
		{Key: "e", Command: "edit", Context: "file-browser-tree"},
		{Key: "E", Command: "edit-external", Context: "file-browser-tree"},
		{Key: "B", Command: "blame", Context: "file-browser-tree"},
		{Key: "\\", Command: "toggle-sidebar", Context: "file-browser-tree"},
		{Key: "H", Command: "toggle-ignored", Context: "file-browser-tree"},
		{Key: "+", Command: "resize-pane-grow", Context: "file-browser-tree"},
		{Key: "-", Command: "resize-pane-shrink", Context: "file-browser-tree"},

		// File browser preview context
		{Key: "tab", Command: "switch-pane", Context: "file-browser-preview"},
		{Key: "shift+tab", Command: "switch-pane", Context: "file-browser-preview"},
		// `/` is InFile: this file's contents, as against f Search's whole
		// project and ctrl+p Find's file names.
		{Key: "/", Command: "search-content", Context: "file-browser-preview"},
		{Key: "ctrl+p", Command: "quick-open", Context: "file-browser-preview"},
		{Key: "f", Command: "project-search", Context: "file-browser-preview"},
		{Key: "{", Command: "prev-tab", Context: "file-browser-preview"},
		{Key: "}", Command: "next-tab", Context: "file-browser-preview"},
		{Key: "x", Command: "close-tab", Context: "file-browser-preview"},
		{Key: "r", Command: "refresh", Context: "file-browser-preview"},
		{Key: "R", Command: "rename", Context: "file-browser-preview"},
		{Key: "ctrl+r", Command: "reveal", Context: "file-browser-preview"},
		{Key: "I", Command: "info", Context: "file-browser-preview"},
		{Key: "e", Command: "edit", Context: "file-browser-preview"},
		{Key: "E", Command: "edit-external", Context: "file-browser-preview"},
		{Key: "B", Command: "blame", Context: "file-browser-preview"},
		{Key: "m", Command: "toggle-markdown", Context: "file-browser-preview"},
		{Key: "esc", Command: "back", Context: "file-browser-preview"},
		{Key: "h", Command: "back", Context: "file-browser-preview"},
		{Key: "y", Command: "yank-contents", Context: "file-browser-preview"},
		{Key: "Y", Command: "yank-path", Context: "file-browser-preview"},
		{Key: "\\", Command: "toggle-sidebar", Context: "file-browser-preview"},
		{Key: "w", Command: "toggle-wrap", Context: "file-browser-preview"},
		{Key: "+", Command: "resize-pane-grow", Context: "file-browser-preview"},
		{Key: "-", Command: "resize-pane-shrink", Context: "file-browser-preview"},

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
		{Key: "j", Command: "cursor-down", Context: "file-browser-project-search"},
		{Key: "k", Command: "cursor-up", Context: "file-browser-project-search"},
		{Key: "g", Command: "cursor-top", Context: "file-browser-project-search"},
		{Key: "G", Command: "cursor-bottom", Context: "file-browser-project-search"},

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
		{Key: "ctrl+n", Command: "new-shell", Context: "workspace-list"},
		// v opens View here as it does in the global browser; the kanban toggle
		// moved to V so the two surfaces spend their obvious "view" key on the
		// same thing.
		{Key: "v", Command: "open-view", Context: "workspace-list"},
		{Key: "V", Command: "toggle-view", Context: "workspace-list"},
		{Key: "r", Command: "refresh", Context: "workspace-list"},
		{Key: "D", Command: "delete-workspace", Context: "workspace-list"},
		{Key: "d", Command: "show-diff", Context: "workspace-list"},
		{Key: "d", Command: "show-diff", Context: "workspace-preview"},
		{Key: "p", Command: "push", Context: "workspace-list"},
		{Key: "m", Command: "merge-workflow", Context: "workspace-list"},
		{Key: "T", Command: "link-task", Context: "workspace-list"},
		{Key: "s", Command: "start-agent", Context: "workspace-list"},
		// E is the explicit type key. i is Sidecar's find-TD-task shortcut.
		{Key: "E", Command: "interactive", Context: "workspace-list"},
		{Key: "t", Command: "attach", Context: "workspace-list"},
		{Key: "S", Command: "stop-agent", Context: "workspace-list"},
		{Key: "O", Command: "open-in-git", Context: "workspace-list"},
		{Key: "l", Command: "focus-right", Context: "workspace-list"},
		{Key: "right", Command: "focus-right", Context: "workspace-list"},
		{Key: "tab", Command: "switch-pane", Context: "workspace-list"},
		{Key: "shift+tab", Command: "switch-pane", Context: "workspace-list"},
		{Key: "\\", Command: "toggle-sidebar", Context: "workspace-list"},
		{Key: "P", Command: "fetch-pr", Context: "workspace-list"},
		{Key: "F", Command: "find-file", Context: "workspace-list"},
		{Key: "R", Command: "rename-shell", Context: "workspace-list"},
		{Key: "R", Command: "rename-worktree", Context: "workspace-list"},
		{Key: "+", Command: "resize-pane-grow", Context: "workspace-list"},
		{Key: "-", Command: "resize-pane-shrink", Context: "workspace-list"},
		{Key: "ctrl+t", Command: "toggle-terminal", Context: "workspace-list"},
		{Key: "alt+t", Command: "switch-terminal-layout", Context: "workspace-list"},

		// Workspace fetch PR context
		{Key: "esc", Command: "cancel", Context: "workspace-fetch-pr"},
		{Key: "enter", Command: "fetch", Context: "workspace-fetch-pr"},

		// Workspace merge/PR lifecycle
		{Key: "esc", Command: "cancel", Context: "workspace-merge"},
		{Key: "enter", Command: "continue", Context: "workspace-merge"},
		{Key: "d", Command: "merge-fallback-draft", Context: "workspace-merge"},
		{Key: "a", Command: "merge-agent-draft", Context: "workspace-merge"},
		{Key: "ctrl+s", Command: "merge-create-pr", Context: "workspace-merge"},
		{Key: "s", Command: "merge-stop-watching", Context: "workspace-merge"},
		{Key: "o", Command: "open-pr", Context: "workspace-merge"},
		{Key: "y", Command: "copy-pr", Context: "workspace-merge"},

		// Workspace preview context
		{Key: "h", Command: "focus-left", Context: "workspace-preview"},
		{Key: "left", Command: "focus-left", Context: "workspace-preview"},
		{Key: "esc", Command: "focus-left", Context: "workspace-preview"},
		{Key: "s", Command: "start-agent", Context: "workspace-preview"},
		{Key: "S", Command: "stop-agent", Context: "workspace-preview"},
		{Key: "E", Command: "interactive", Context: "workspace-preview"},
		// `0` deliberately has no workspace-preview binding. It is the header's
		// Tasks shortcut now, and a context-local binding that shadowed it here
		// would make the same key mean two different things one tab apart. The
		// binding it replaces cost nothing: `reset-scroll` had no handler
		// anywhere in the tree, and the preview already jumps to the top of its
		// scrollback with `g`.
		{Key: "tab", Command: "switch-pane", Context: "workspace-preview"},
		{Key: "shift+tab", Command: "switch-pane", Context: "workspace-preview"},
		{Key: "\\", Command: "toggle-sidebar", Context: "workspace-preview"},
		{Key: "j", Command: "scroll-down", Context: "workspace-preview"},
		{Key: "k", Command: "scroll-up", Context: "workspace-preview"},
		{Key: "ctrl+d", Command: "page-down", Context: "workspace-preview"},
		{Key: "ctrl+u", Command: "page-up", Context: "workspace-preview"},
		{Key: "+", Command: "resize-pane-grow", Context: "workspace-preview"},
		{Key: "-", Command: "resize-pane-shrink", Context: "workspace-preview"},
		{Key: "ctrl+t", Command: "toggle-terminal", Context: "workspace-preview"},
		{Key: "alt+t", Command: "switch-terminal-layout", Context: "workspace-preview"},

		// Workspace merge error context
		{Key: "esc", Command: "dismiss-merge-error", Context: "workspace-merge-error"},
		{Key: "y", Command: "yank-merge-error", Context: "workspace-merge-error"},
		{Key: "c", Command: "continue-merge", Context: "workspace-merge-error"},
		{Key: "a", Command: "abort-merge", Context: "workspace-merge-error"},
		{Key: "r", Command: "retry-push", Context: "workspace-merge-error"},

		// Workspace interactive context bindings are registered dynamically
		// by the workspace plugin Init() to reflect configured keys.

		// Notes list context
		{Key: "j", Command: "cursor-down", Context: "notes-list"},
		{Key: "k", Command: "cursor-up", Context: "notes-list"},
		{Key: "down", Command: "cursor-down", Context: "notes-list"},
		{Key: "up", Command: "cursor-up", Context: "notes-list"},
		{Key: "G", Command: "cursor-bottom", Context: "notes-list"},
		{Key: "n", Command: "new-note", Context: "notes-list"},
		{Key: "X", Command: "delete-note", Context: "notes-list"},
		{Key: "x", Command: "show-deleted", Context: "notes-list"},
		{Key: "p", Command: "toggle-pin", Context: "notes-list"},
		{Key: "A", Command: "archive-note", Context: "notes-list"},
		{Key: "a", Command: "show-archived", Context: "notes-list"},
		{Key: "u", Command: "undo", Context: "notes-list"},
		{Key: "r", Command: "refresh", Context: "notes-list"},
		{Key: "ctrl+s", Command: "save", Context: "notes-list"},
		// enter is the built-in editor; e is vim in the right pane; E leaves
		// for $EDITOR. Three editors, three explicit keys — nothing infers one
		// from a config value.
		{Key: "enter", Command: "edit-note", Context: "notes-list"},
		{Key: "e", Command: "vim-edit", Context: "notes-list"},
		{Key: "E", Command: "external-editor", Context: "notes-list"},
		{Key: "/", Command: "search", Context: "notes-list"},
		{Key: "T", Command: "to-task", Context: "notes-list"},
		{Key: "I", Command: "show-info", Context: "notes-list"},
		{Key: "y", Command: "yank-content", Context: "notes-list"},
		{Key: "Y", Command: "yank-title", Context: "notes-list"},
		{Key: "esc", Command: "back-to-active", Context: "notes-list"},

		// Notes info modal context
		{Key: "esc", Command: "close", Context: "notes-info"},
		{Key: "enter", Command: "close", Context: "notes-info"},

		// Notes search context
		{Key: "esc", Command: "cancel", Context: "notes-search"},
		{Key: "enter", Command: "select", Context: "notes-search"},
		{Key: "down", Command: "cursor-down", Context: "notes-search"},
		{Key: "up", Command: "cursor-up", Context: "notes-search"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "notes-search"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "notes-search"},
		{Key: "ctrl+s", Command: "save", Context: "notes-search"},

		// Notes preview context (read-only view)
		{Key: "alt+c", Command: "copy-note", Context: "notes-preview"},
		{Key: "enter", Command: "edit-note", Context: "notes-preview"},
		{Key: "i", Command: "edit-note", Context: "notes-preview"},
		{Key: "e", Command: "vim-edit", Context: "notes-preview"},
		{Key: "E", Command: "external-editor", Context: "notes-preview"},
		{Key: "m", Command: "toggle-markdown", Context: "notes-preview"},
		{Key: "ctrl+s", Command: "save", Context: "notes-preview"},

		// Notes editor context
		{Key: "tab", Command: "switch-pane", Context: "notes-editor"},
		{Key: "esc", Command: "back", Context: "notes-editor"},
		{Key: "ctrl+s", Command: "save", Context: "notes-editor"},
		{Key: "alt+c", Command: "copy-note", Context: "notes-editor"},
		{Key: "up", Command: "cursor-up", Context: "notes-editor"},
		{Key: "down", Command: "cursor-down", Context: "notes-editor"},
		{Key: "left", Command: "cursor-left", Context: "notes-editor"},
		{Key: "right", Command: "cursor-right", Context: "notes-editor"},
		{Key: "ctrl+n", Command: "cursor-down", Context: "notes-editor"},
		{Key: "ctrl+p", Command: "cursor-up", Context: "notes-editor"},
		{Key: "home", Command: "line-start", Context: "notes-editor"},
		{Key: "end", Command: "line-end", Context: "notes-editor"},
		{Key: "ctrl+a", Command: "line-start", Context: "notes-editor"},
		{Key: "ctrl+e", Command: "line-end", Context: "notes-editor"},
		{Key: "shift+up", Command: "select-up", Context: "notes-editor"},
		{Key: "shift+down", Command: "select-down", Context: "notes-editor"},
		{Key: "shift+left", Command: "select-left", Context: "notes-editor"},
		{Key: "shift+right", Command: "select-right", Context: "notes-editor"},
		{Key: "shift+home", Command: "select-line-start", Context: "notes-editor"},
		{Key: "shift+end", Command: "select-line-end", Context: "notes-editor"},
		{Key: "alt+s", Command: "select-toggle", Context: "notes-editor"},
		{Key: "alt+a", Command: "select-all", Context: "notes-editor"},
		{Key: "alt+x", Command: "cut", Context: "notes-editor"},
		{Key: "ctrl+z", Command: "undo-edit", Context: "notes-editor"},
		{Key: "ctrl+y", Command: "redo-edit", Context: "notes-editor"},
		{Key: "ctrl+shift+z", Command: "redo-edit", Context: "notes-editor"},

		// Notes task modal context
		{Key: "enter", Command: "create-task", Context: "notes-task-modal"},
		{Key: "esc", Command: "cancel", Context: "notes-task-modal"},
		{Key: "tab", Command: "next-field", Context: "notes-task-modal"},
		{Key: "shift+tab", Command: "prev-field", Context: "notes-task-modal"},
	}
	return append(bindings, diffViewerBindings()...)
}

// diffViewerBindings are the keys the shared Diff viewer answers inside its own
// switch (internal/workspacediff/keys.go). Like the Agents board and the global
// Workspaces list above, the viewer handles them before keymap dispatch; they
// are registered so the footer, the help sheet and the command palette can show
// a reader how to get into a diff and move around it.
//
// Both Diff surfaces run the same viewer, so both contexts get the same table
// from one declaration.
func diffViewerBindings() []Binding {
	keys := []struct {
		key     string
		command string
	}{
		{"l", "diff-open"},
		{"right", "diff-open"},
		{"enter", "diff-open"},
		{"j", "diff-down"},
		{"down", "diff-down"},
		{"k", "diff-up"},
		{"up", "diff-up"},
		{"j", "diff-scroll-down"},
		{"down", "diff-scroll-down"},
		{"k", "diff-scroll-up"},
		{"up", "diff-scroll-up"},
		{"h", "diff-back"},
		{"left", "diff-back"},
		{"g", "diff-top"},
		{"G", "diff-bottom"},
		{"ctrl+d", "diff-page-down"},
		{"pgdown", "diff-page-down"},
		{"ctrl+u", "diff-page-up"},
		{"pgup", "diff-page-up"},
		{"n", "diff-next-change"},
		{"N", "diff-next-change"},
	}
	contexts := []string{"workspace-diff", "global-workspaces-diff"}
	out := make([]Binding, 0, len(keys)*len(contexts))
	for _, context := range contexts {
		for _, k := range keys {
			out = append(out, Binding{Key: k.key, Command: k.command, Context: context})
		}
	}
	return out
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
