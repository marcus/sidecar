package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/configui"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/uirequest"
)

// ToastMsg is re-exported from msg package for backward compatibility.
type ToastMsg = msg.ToastMsg

// ShowToast is re-exported from msg package for backward compatibility.
var ShowToast = msg.ShowToast

// FlashMsg is re-exported from the msg package: the status-flash tier beside
// ToastMsg. A flash is transient feedback, never a stored notification.
type FlashMsg = msg.FlashMsg

// ShowFlash and ShowFlashFrom are re-exported for symmetry with ShowToast.
var (
	ShowFlash     = msg.ShowFlash
	ShowFlashFrom = msg.ShowFlashFrom
)

// Alert and Blocked are re-exported: the source-aware notification path, for
// events that should not speak as generic `system` chrome.
var (
	Alert   = msg.Alert
	Blocked = msg.Blocked
)

// ThemeChangedMsg is re-exported from msg package for backward compatibility.
type ThemeChangedMsg = msg.ThemeChangedMsg

// ThemeChanged is re-exported from msg package for backward compatibility.
var ThemeChanged = msg.ThemeChanged

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

// NavigateToFileMsg asks the Files plugin to open a path. Hosts send this
// rather than importing filebrowser.
type NavigateToFileMsg struct {
	Path string // Relative path from workdir
	Line int    // Optional 1-based line to reveal after loading
}

// NavigateToNoteMsg asks Notes to verify and select a stable note identity in
// the named project. Notes focuses itself only after the note is confirmed to
// exist, so a stale or foreign link cannot move the user.
type NavigateToNoteMsg struct {
	ID          string
	ProjectRoot string
}

// ActivateTargetMsg is the one route for "jump to the thing this text names".
// Every surface — a terminal link, the notification centre, a future CLI action
// — sends this rather than reaching into a plugin, because only the app shell
// can both focus plugins and switch projects.
//
// Target is the cross-surface vocabulary (uirequest.Target). Project is an
// optional qualifier — a project path or its base name — and empty means the
// project the user is already in.
type ActivateTargetMsg struct {
	Target  uirequest.Target
	Project string
}

// OpenIssuePaneMsg, OpenDiffPaneMsg, OpenResourcePaneMsg and AttachSessionMsg
// are the public entries for what used to be reachable only through
// workspace-plugin-private methods. They are the surface-parity seam: any
// plugin, the notification centre, or a future CLI action can send them, and
// hosts send them rather than importing workspace.
//
// Each opens against the plugin's currently selected surface, exactly as a
// click on the same link in that surface's terminal does.
type (
	// OpenIssuePaneMsg opens an issue id in an issue pane.
	OpenIssuePaneMsg struct{ Issue string }
	// OpenDiffPaneMsg opens a git spec in a diff pane. The host re-resolves the
	// spec in its own checkout, so a crafted spec cannot skip rev-parse.
	OpenDiffPaneMsg struct{ Spec string }
	// OpenResourcePaneMsg opens one of a Resource leaf's three tab shapes.
	//
	// With Collection empty it is a matched locator: Matcher may be empty, and
	// the host's live matcher snapshot then decides which matcher claims the
	// locator, refusing out loud when none does. With Collection set it is a
	// plugin tab — the collection's list when Locator is empty, and that row's
	// document when it is not — and no matcher is consulted, because a plugin
	// row is addressed by its collection and ID.
	// Filters is a collection tab's applied filter set in
	// resource.EncodeFilters form, which keeps the message a comparable value.
	OpenResourcePaneMsg struct{ Provider, Matcher, Locator, Collection, Query, Filters string }
	// AttachSessionMsg attaches a tmux session by name. The host honours the
	// same full-attach feature gate as every other attach path.
	AttachSessionMsg struct{ Session string }
)

// OpenIssuePane returns a command that opens an issue in an issue pane.
func OpenIssuePane(issue string) tea.Cmd {
	return func() tea.Msg { return OpenIssuePaneMsg{Issue: issue} }
}

// OpenDiffPane returns a command that opens a git spec in a diff pane.
func OpenDiffPane(spec string) tea.Cmd {
	return func() tea.Msg { return OpenDiffPaneMsg{Spec: spec} }
}

// OpenResourcePane returns a command that opens a provider locator in a
// resource pane.
func OpenResourcePane(provider, matcher, locator string) tea.Cmd {
	return func() tea.Msg {
		return OpenResourcePaneMsg{Provider: provider, Matcher: matcher, Locator: locator}
	}
}

// OpenPluginPane returns a command that opens a plugin collection tab, or one
// row's document tab when row is non-empty.
func OpenPluginPane(instance, collection, query, row, filters string) tea.Cmd {
	return func() tea.Msg {
		return OpenResourcePaneMsg{
			Provider: instance, Collection: collection, Query: query, Locator: row, Filters: filters,
		}
	}
}

// AttachSession returns a command that attaches a tmux session by name.
func AttachSession(session string) tea.Cmd {
	return func() tea.Msg { return AttachSessionMsg{Session: session} }
}

// ActivateTarget returns a command that activates a target in the current
// project. Use ActivateTargetIn for a cross-project jump.
func ActivateTarget(target uirequest.Target) tea.Cmd {
	return ActivateTargetIn(target, "")
}

// ActivateTargetIn returns a command that activates a target in a named project.
func ActivateTargetIn(target uirequest.Target, project string) tea.Cmd {
	return func() tea.Msg { return ActivateTargetMsg{Target: target, Project: project} }
}

// OpenPrefilledShellMsg asks the Workspaces plugin for an ordinary new shell
// with a command typed into it and left unexecuted. Hosts send this rather than
// importing workspace.
//
// Nothing about it is privileged: it is the same shell the user could create by
// hand, and the command sits at the prompt until the user reads it and presses
// Enter. Sidecar never runs it, and never sends one that needs sudo.
type OpenPrefilledShellMsg struct {
	Command string
}

// OpenConfigurationMsg asks the host to open Configuration on a destination.
// An empty or unknown Page means Configuration's own default, Sidecar Setup.
//
// It is how a surface that is empty because something is not configured yet
// offers a way out of that state — a plugin sends this rather than importing
// the Configuration surface — and it is also how a launch command's startup
// destination is honored. Escape returns to whatever sent it.
//
// AddProject opens the Add Project child of Projects after the page is shown,
// which is the first-run path when Sidecar launches in a non-Git directory
// with no configured projects.
type OpenConfigurationMsg struct {
	Page       configui.PageID
	AddProject bool
}

// OpenNotesPreferencesMsg asks the host to open the one existing Notes
// enablement control and focus it. It is separate from the generic page route
// so a setup dialog does not need to know Configuration's private control ID.
type OpenNotesPreferencesMsg struct{}

// OpenConfiguration returns a command that opens Configuration on a page.
func OpenConfiguration(page configui.PageID) tea.Cmd {
	return func() tea.Msg { return OpenConfigurationMsg{Page: page} }
}

func OpenNotesPreferences() tea.Cmd {
	return func() tea.Msg { return OpenNotesPreferencesMsg{} }
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

// UpdateModalState represents the current state of the update modal.
type UpdateModalState int

const (
	UpdateModalClosed   UpdateModalState = iota // Modal not visible
	UpdateModalPreview                          // Show release notes before update
	UpdateModalProgress                         // Show multi-phase progress during update
	UpdateModalComplete                         // Show completion message
	UpdateModalError                            // Show error details
)

// UpdateElapsedTickMsg triggers elapsed time update during update.
type UpdateElapsedTickMsg struct{}

// updateChangelogState tracks the tag-pinned full-changelog fetch behind the
// expanded notes section.
type updateChangelogState uint8

const (
	changelogIdle updateChangelogState = iota
	changelogLoading
	changelogLoaded
	changelogFailed
)

// EditorReturnedMsg signals that an external editor process has exited.
// Used to restore terminal state (mouse support) after returning from vim/etc.
type EditorReturnedMsg struct {
	Err error
	// Fallback is the direct-exec argv to try when the shell that was asked to
	// load the user's profile never got as far as running the editor. It is
	// empty once the fallback has been used, so a failure can never loop.
	Fallback []string
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
