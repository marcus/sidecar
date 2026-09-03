package plugin

import (
	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/contentlink"
)

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

// SelfConstrainedView is an optional presentation capability for plugins whose
// View already returns the exact width and height requested by the app shell.
// Opting in lets the shell skip its defensive Lip Gloss clamp; View is still
// called on every frame so live content and pointer regions remain current.
//
// Implementations must keep every rendered line within width, return no more
// than height lines, and preserve the padding that the shell's Width/Height
// wrapper would otherwise add. Plugins without this capability keep the
// defensive wrapper. The shell asks after View, so an implementation may use
// the dimensions it just rendered to decline the capability at a size where
// its normal layout floor exceeds the requested box.
type SelfConstrainedView interface {
	ViewIsSelfConstrained() bool
}

// AttentionOriginProvider projects the workspace/session actually visible in
// a presentation-only surface. The app owns focus and publishes this identity
// for host-wide background policy.
type AttentionOriginProvider interface {
	AttentionOrigin() (AttentionOrigin, bool)
}

// AttentionOrigin is the app-level identity shared by project and global
// workspace projections. It deliberately contains no notification or
// transport types.
type AttentionOrigin struct {
	TmuxSession string
	ProjectKey  string
	WorkDir     string
	// HostID names the registered remote host the visible workspace lives on,
	// empty for one on this machine. Two machines legitimately share a session
	// name and a checkout path, so without it a local selection would answer
	// "is the user looking at that remote workspace" with yes.
	HostID string
}

// PaneFocusStop names one directly focusable window inside a plugin. IDs are
// stable while visual order is expressed by the provider's returned slice.
type PaneFocusStop struct {
	ID string
}

// PaneFocusProvider is an optional capability for plugins whose own panes join
// an app-owned focus ring. Implementations project existing focus state rather
// than introducing a second focus model.
type PaneFocusProvider interface {
	PaneFocusStops() []PaneFocusStop
	PaneFocus() string
	SetPaneFocus(id string) tea.Cmd
	SetPaneFocusActive(active bool)
}

// PaneFocusRingHost is an optional capability for a plugin that hands Tab and
// Shift+Tab to the app even on a deck holding nothing but the plugin's own
// Primary leaf. The app cycles PaneFocusStops for it there, exactly as it does
// once a second leaf is open.
//
// It is opt-in because Tab is not a spare key. Files, Git and Notes each route
// it themselves and do more with it than move focus — Notes saves the editor's
// buffer on the way out — so a host that took Tab from a Primary-only deck on
// the strength of two projected stops would quietly replace that. Nor can the
// keymap answer the question: several surfaces switch panes on a hard-coded tab
// that is registered nowhere, which internal/app already says out loud where
// the notification centre asks the same thing. So the plugin says so itself,
// and a plugin that says nothing keeps the key.
type PaneFocusRingHost interface {
	PaneFocusProvider

	// HostOwnsPaneFocusRing reports that the plugin binds no Tab of its own,
	// so the app's ring is the whole of what the key does here.
	HostOwnsPaneFocusRing() bool
}

// ContentLinkProvider is an optional capability for plugins that expose exact
// read-only rendered text rectangles for app-owned content-link decoration.
// Hosts read it after View so its geometry describes the frame just rendered.
type ContentLinkProvider interface {
	ContentLinkSurfaces() []contentlink.Surface
}

// TextInputConsumer is an optional capability for plugins that need
// alphanumeric key input to be forwarded as typed text instead of being
// intercepted by app-level shortcuts.
type TextInputConsumer interface {
	ConsumesTextInput() bool
}

// WheelBoundaryConsumer is an optional fast-path for plugins with scrollable
// surfaces. Bubble Tea asks it before Update and View; returning true drops an
// inertia event that cannot move the surface under the pointer. Implementations
// must return false when they are not certain (for example, a terminal
// application that owns mouse reporting).
//
// The message coordinates are local to the plugin content box, after Sidecar's
// header has been removed. Implementations may reset gesture-only coalescing
// state when they report a boundary, but must not change visible content.
type WheelBoundaryConsumer interface {
	WheelAtBoundary(tea.MouseWheelMsg) bool
}

// GlobalKeyBlocker is an optional capability for plugins with overlays that
// own the keyboard. While it reports true, Sidecar forwards every key except
// its interrupt instead of running host-level shortcuts.
type GlobalKeyBlocker interface {
	BlocksGlobalKeys() bool
}

// KeyRouter is an optional capability for plugins that own keys the host also
// binds globally. It makes sidecar's key precedence explicit instead of
// implicit in the order of a switch statement:
//
//  1. an open sidecar application modal;
//  2. the active plugin's text-input or blocking-overlay context
//     (TextInputConsumer, or BlocksGlobalKeys here);
//  3. an active plugin contextual binding (ClaimsKey here);
//  4. sidecar global bindings;
//  5. unbound input forwarded to the plugin.
//
// Only plugins that implement it participate in levels 2 (overlay) and 3; every
// other plugin keeps the level-4-then-5 behaviour it has always had.
type KeyRouter interface {
	GlobalKeyBlocker

	// BlocksGlobalKeys reports that the plugin is showing an overlay that owns
	// the keyboard. Every key except the host's interrupt is forwarded.
	// ClaimsKey reports that the plugin has a live contextual binding for a
	// key. It is asked only for keys sidecar would otherwise handle globally,
	// and only when no overlay is blocking.
	ClaimsKey(key string) bool

	// QuitKeyExits reports whether `q` in the plugin's current context should
	// reach sidecar's quit flow. It replaces the host's isRootContext guess for
	// plugins that can answer for themselves.
	QuitKeyExits() bool
}

// FooterStatusProvider is an optional capability for plugins with a condition
// that must stay visible even though the host owns the footer. A plugin that
// suppresses its own status line has no other always-on surface.
type FooterStatusProvider interface {
	// FooterStatus returns text for the host footer and whether it is an error.
	// An empty string means "nothing to say".
	FooterStatus() (string, bool)
}

type WorkspaceSelectionKind string

const (
	WorkspaceSelectionWorktree WorkspaceSelectionKind = "worktree"
	WorkspaceSelectionShell    WorkspaceSelectionKind = "shell"
)

// PendingWorkspaceSelection is delivered synchronously after registry.Reinit,
// before the returned async Start commands run.
type PendingWorkspaceSelection struct {
	Kind WorkspaceSelectionKind
	Key  string
	Path string
	// Action asks the owning project surface to continue with one of its own
	// established workflows after the exact identity is selected.
	Action string
}

// FocusCycler is an optional capability for surfaces whose Tab cycle the shell
// may extend with a stop of its own. The notification centre is that stop: when
// it is open it joins the cycle between a surface's last window and its first,
// rather than running a second cycle beside it.
//
// A surface that does not implement this keeps Tab exactly as it always had it;
// the shell then only offers Tab to the centre when nothing else has claimed
// the key in the focused context.
type FocusCycler interface {
	// AtFocusCycleEnd reports that focus is on the last window of the surface's
	// ring in the direction the cycle is about to move, so the next Tab would
	// wrap. That is where a shell-owned stop belongs.
	AtFocusCycleEnd(reverse bool) bool

	// FocusCycleStart puts focus back on the window the cycle resumes at — the
	// first going forward, the last going back — when the shell hands the
	// keyboard back to the surface.
	FocusCycleStart(reverse bool) tea.Cmd
}

// ResizeDragReporter is implemented by a surface whose panes are resized by
// dragging a rail. While that drag is live the shell suppresses the floating
// tiers — toasts and status flashes — because every drag frame re-lays out the
// whole content region and a block composited on top of it both flickers and
// pays for a second full-screen composite on the frame that is already the
// expensive one (design 1g's "suppress while resizing", and the resize storm
// deferred from notifications Phase 1).
//
// Nothing is lost by the suppression: the notification is already in the store,
// the centre and the header count, and it paints on the frame after the drop.
type ResizeDragReporter interface {
	// ResizeDragActive reports that a pane rail is currently being dragged.
	ResizeDragActive() bool
}

type PendingWorkspaceSelector interface {
	SetPendingWorkspaceSelection(PendingWorkspaceSelection)
}

type PendingWorkspaceActionProvider interface {
	TakePendingWorkspaceAction() tea.Cmd
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
