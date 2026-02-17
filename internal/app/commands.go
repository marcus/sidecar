package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/plugin"
)

// ToastMsg is re-exported from msg package for backward compatibility.
type ToastMsg = msg.ToastMsg

// ShowToast is re-exported from msg package for backward compatibility.
var ShowToast = msg.ShowToast

// Message types for tea.Cmd
type (
	// TickMsg is sent on each clock tick.
	TickMsg time.Time

	// RefreshMsg triggers a full refresh.
	RefreshMsg struct{}

	// ErrorMsg represents an error condition.
	ErrorMsg struct {
		Err error
	}
)

// tickCmd returns a command that ticks every second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// Refresh returns a command to trigger a refresh.
func Refresh() tea.Cmd {
	return func() tea.Msg {
		return RefreshMsg{}
	}
}

// ReportError returns a command to report an error.
func ReportError(err error) tea.Cmd {
	return func() tea.Msg {
		return ErrorMsg{Err: err}
	}
}

// Tick returns a custom tick command with a tag.
func Tick(d time.Duration, tag string) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return TaggedTickMsg{Time: t, Tag: tag}
	})
}

// TaggedTickMsg is a tick with an identifying tag.
type TaggedTickMsg struct {
	Time time.Time
	Tag  string
}

// PluginFocusedMsg is sent to a plugin when it becomes the active plugin.
// Plugins can use this to refresh data or update their state on focus.
// Re-exported from plugin package for backward compatibility.
type PluginFocusedMsg = plugin.PluginFocusedMsg

// PluginFocused returns a command that sends PluginFocusedMsg.
func PluginFocused() tea.Cmd {
	return func() tea.Msg {
		return plugin.PluginFocusedMsg{}
	}
}

// FocusPluginByIDMsg requests focusing a specific plugin by ID.
// Used for cross-plugin navigation (e.g., opening file in file browser from git).
type FocusPluginByIDMsg struct {
	PluginID string
}

// SwitchWorktreeMsg requests switching to a different worktree.
// Used by the worktree switcher modal and workspace plugin "Open in Git Tab" command.
type SwitchWorktreeMsg struct {
	WorktreePath string // Absolute path to the worktree
}

// SwitchWorktree returns a command that requests switching to a worktree by path.
func SwitchWorktree(path string) tea.Cmd {
	return func() tea.Msg {
		return SwitchWorktreeMsg{WorktreePath: path}
	}
}

// WorktreeDeletedMsg is sent when the current worktree has been deleted.
type WorktreeDeletedMsg struct {
	DeletedPath string // Path of the deleted worktree
	MainPath    string // Path to switch to (main worktree)
}

// checkWorktreeExists returns a command that checks if the current worktree still exists.
func checkWorktreeExists(workDir string) tea.Cmd {
	return func() tea.Msg {
		exists, mainPath := CheckCurrentWorktree(workDir)
		if !exists && mainPath != "" {
			return WorktreeDeletedMsg{
				DeletedPath: workDir,
				MainPath:    mainPath,
			}
		}
		return nil
	}
}

// FocusPlugin returns a command that requests focusing a plugin by ID.
func FocusPlugin(pluginID string) tea.Cmd {
	return func() tea.Msg {
		return FocusPluginByIDMsg{PluginID: pluginID}
	}
}

// UpdateSuccessMsg signals that an update completed successfully.
type UpdateSuccessMsg struct {
	SidecarUpdated    bool
	TdUpdated         bool
	NewSidecarVersion string
	NewTdVersion      string
}

// UpdateErrorMsg signals that an update failed.
type UpdateErrorMsg struct {
	Step string // "sidecar", "td", or "check"
	Err  error
}

// UpdateSpinnerTickMsg triggers spinner animation during update.
type UpdateSpinnerTickMsg struct{}

// UpdateModalState represents the current state of the update modal.
type UpdateModalState int

const (
	UpdateModalClosed   UpdateModalState = iota // Modal not visible
	UpdateModalPreview                          // Show release notes before update
	UpdateModalProgress                         // Show multi-phase progress during update
	UpdateModalComplete                         // Show completion message
	UpdateModalError                            // Show error details
)

// UpdatePhase represents a phase of the update process.
type UpdatePhase int

const (
	PhaseCheckPrereqs UpdatePhase = iota // Checking prerequisites (go installed)
	PhaseInstalling                      // Installing via go install
	PhaseVerifying                       // Verifying installation
)

// String returns the display name for an update phase.
func (p UpdatePhase) String() string {
	return p.StringForMethod("")
}

// StringForMethod returns the display name for an update phase,
// customized for the install method.
func (p UpdatePhase) StringForMethod(method string) string {
	switch p {
	case PhaseCheckPrereqs:
		return "Checking prerequisites"
	case PhaseInstalling:
		switch method {
		case "homebrew":
			return "Upgrading via Homebrew"
		case "binary":
			return "Manual download required"
		default:
			return "Installing via go install"
		}
	case PhaseVerifying:
		return "Verifying"
	default:
		return "Unknown"
	}
}

// UpdatePhaseChangeMsg signals a change in update phase status.
type UpdatePhaseChangeMsg struct {
	Phase  UpdatePhase
	Status string // "pending", "running", "done", "error"
}

// UpdateElapsedTickMsg triggers elapsed time update during update.
type UpdateElapsedTickMsg struct{}

// ChangelogLoadedMsg signals that changelog content has been loaded.
type ChangelogLoadedMsg struct {
	Content string
	Err     error
}

// EditorReturnedMsg signals that an external editor process has exited.
// Used to restore terminal state (mouse support) after returning from vim/etc.
type EditorReturnedMsg struct {
	Err error
}

// updateSpinnerTick returns a command that ticks the spinner every 100ms.
func updateSpinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return UpdateSpinnerTickMsg{}
	})
}

// SwitchProjectMsg requests switching to a different project by path.
// Used by the projects dashboard plugin for cross-plugin project switching.
type SwitchProjectMsg struct {
	ProjectPath string
}

// SwitchProject returns a command that requests switching to a project by path.
func SwitchProject(path string) tea.Cmd {
	return func() tea.Msg {
		return SwitchProjectMsg{ProjectPath: path}
	}
}

// SwitchToMainWorktreeMsg requests switching to the main worktree.
// Sent when the current WorkDir (a worktree) has been deleted and sidecar
// should gracefully switch to the main repository.
type SwitchToMainWorktreeMsg struct {
	MainWorktreePath string // Path to the main worktree to switch to
}

// SwitchToMainWorktree returns a command that requests switching to the main worktree.
func SwitchToMainWorktree(mainPath string) tea.Cmd {
	return func() tea.Msg {
		return SwitchToMainWorktreeMsg{MainWorktreePath: mainPath}
	}
}
