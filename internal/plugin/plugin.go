package plugin

import tea "github.com/charmbracelet/bubbletea"

// Plugin defines the interface for all sidecar plugins.
type Plugin interface {
	ID() string
	Name() string
	Icon() string
	Init(ctx *Context) error
	Start() tea.Cmd
	Stop()
	Update(msg tea.Msg) (Plugin, tea.Cmd)
	View(width, height int) string
	IsFocused() bool
	SetFocused(bool)
	Commands() []Command
	FocusContext() string
}

// TextInputConsumer is an optional capability for plugins that need
// alphanumeric key input to be forwarded as typed text instead of being
// intercepted by app-level shortcuts.
type TextInputConsumer interface {
	ConsumesTextInput() bool
}

// WorkspaceContextProvider is an optional capability for plugins that manage
// workspace/worktree selection. When the user navigates away from such a plugin,
// the app layer checks if the selected workspace differs from the current WorkDir
// and switches context accordingly. This ensures that other plugins (e.g., git status)
// show the state of the workspace the user was just looking at.
type WorkspaceContextProvider interface {
	// SelectedWorkDir returns the absolute path of the currently selected workspace,
	// or empty string if no workspace is selected or the selection matches the current context.
	SelectedWorkDir() string
}

// Category represents a logical grouping of commands for the command palette.
type Category string

const (
	CategoryNavigation Category = "Navigation"
	CategoryActions    Category = "Actions"
	CategoryView       Category = "View"
	CategorySearch     Category = "Search"
	CategoryEdit       Category = "Edit"
	CategoryGit        Category = "Git"
	CategorySystem     Category = "System"
)

// Command represents a keybinding command exposed by a plugin.
type Command struct {
	ID          string         // Unique identifier (e.g., "stage-file")
	Name        string         // Short name for footer (e.g., "Stage")
	Description string         // Full description for palette
	Category    Category       // Logical grouping for palette display
	Handler     func() tea.Cmd // Action to execute (optional)
	Context     string         // Activation context
	Priority    int            // Footer display priority: 1=highest, 0=default (treated as 99)
}

// DiagnosticProvider is implemented by plugins that expose diagnostics.
type DiagnosticProvider interface {
	Diagnostics() []Diagnostic
}

// Diagnostic represents a health/status check result.
type Diagnostic struct {
	ID     string
	Status string
	Detail string
}

// OpenFileMsg requests opening a file in an external editor.
// Sent by plugins, handled by app to exec the editor process.
type OpenFileMsg struct {
	Editor string // Editor command (e.g., "vim", "code")
	Path   string // File path to open
	LineNo int    // Line number to open at (0 = start of file)
}

// PluginFocusedMsg is sent to a plugin when it becomes the active plugin.
// Plugins can use this to refresh data or update their state on focus.
type PluginFocusedMsg struct{}

// EpochMessage is implemented by async messages that need staleness detection.
// Messages from async operations should embed an Epoch field and implement this interface.
type EpochMessage interface {
	GetEpoch() uint64
}

// IsStale returns true if the message's epoch doesn't match the current context epoch.
// Use this in Update() handlers to discard messages from previous projects:
//
//	if plugin.IsStale(p.ctx, msg) { return p, nil }
func IsStale(ctx *Context, msg EpochMessage) bool {
	return ctx != nil && msg.GetEpoch() != ctx.Epoch
}
