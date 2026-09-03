package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// State holds persistent user preferences.
type State struct {
	GitDiffMode       string `json:"gitDiffMode"`                 // "unified" or "side-by-side"
	WorkspaceDiffMode string `json:"workspaceDiffMode,omitempty"` // "unified" or "side-by-side"
	GitGraphEnabled   bool   `json:"gitGraphEnabled,omitempty"`   // Show commit graph in sidebar
	LineWrapEnabled   bool   `json:"lineWrapEnabled,omitempty"`   // Wrap long lines instead of truncating

	// Pane width preferences (percentage of total width, 0 = use default)
	FileBrowserTreeWidth   int `json:"fileBrowserTreeWidth,omitempty"`
	GitStatusSidebarWidth  int `json:"gitStatusSidebarWidth,omitempty"`
	ConversationsSideWidth int `json:"conversationsSideWidth,omitempty"`
	WorkspaceSidebarWidth  int `json:"workspaceSidebarWidth,omitempty"`
	DiffTabFileListWidth   int `json:"diffTabFileListWidth,omitempty"`
	// PluginBrowserSplit is the shared plugin browser's list share, as a
	// percentage of the content box, keyed by plugin instance ID. It is keyed
	// per plugin rather than held globally because two plugins' tables are two
	// different shapes: a split dragged wide for a four-column results table is
	// the wrong one for a two-column source list.
	PluginBrowserSplit map[string]int `json:"pluginBrowserSplit,omitempty"`
	// NotificationCentreWidth is the app-level right panel's width in columns.
	// It belongs to the shell rather than a plugin, but it is the same kind of
	// preference as the pane widths above and is persisted the same way.
	NotificationCentreWidth int `json:"notificationCentreWidth,omitempty"`
	// The three legacy terminal-panel keys below are read once, converted into
	// a pane-tree split, and cleared. Nothing writes them any more: the panel is
	// a Shell leaf of the persisted pane layout, so its side, its size and its
	// presence are the layout's to state.
	LegacyTermPanelSize    int    `json:"termPanelSize,omitempty"`    // Panel's own share (percentage, 0 = 50%)
	LegacyTermPanelSide    string `json:"termPanelLayout,omitempty"`  // "bottom" or "right"
	LegacyTermPanelVisible bool   `json:"termPanelVisible,omitempty"` // Panel was up at exit

	// Plugin-specific state (keyed by working directory path)
	FileBrowser  map[string]FileBrowserState `json:"fileBrowser,omitempty"`
	Workspace    map[string]WorkspaceState   `json:"workspace,omitempty"`
	Notes        map[string]NotesState       `json:"notes,omitempty"`
	ActivePlugin map[string]string           `json:"activePlugin,omitempty"`
	// ContentDeck stores reference-only app-owned passive pane state by
	// working-directory and plugin ID. Loaded bodies never cross this boundary.
	ContentDeck map[string]json.RawMessage `json:"contentDeck,omitempty"`

	// Worktree state: maps main repo path -> last active worktree path
	LastWorktreePath map[string]string `json:"lastWorktreePath,omitempty"`

	// LastRemoteWorktree maps hosts.ScopedKey(HostID, ProjectKey) to the
	// host-side WorktreeKey last entered on that project. GetLastWorktreePath
	// never reads this map: a remote path stored there would be treated as a
	// local restore.
	LastRemoteWorktree map[string]string `json:"lastRemoteWorktree,omitempty"`

	// LastBoundLocation is the last host-qualified destination this TUI bound.
	// It is never a remote filesystem path: restoring LastWorktreePath as a
	// local checkout would follow a remote Root that happens to exist here.
	LastBoundLocation *BoundLocation `json:"lastBoundLocation,omitempty"`

	// Last selected global tab ("agents", "workspaces", or "tasks").
	LastGlobalTab string `json:"lastGlobalTab,omitempty"`

	// LastScope is the top-level space the user was in when Sidecar last left
	// it: "global" or "project". Together with LastGlobalTab it is the whole of
	// the remembered top-level selection — which space, and which tab inside it
	// — so a relaunch reopens where the user was rather than always on the
	// project workspace. Empty (a first run, or an upgrade from a version that
	// never wrote it) means the project workspace, which is the behaviour those
	// users already have.
	//
	// It is deliberately not keyed by working directory. The global space spans
	// every configured project, so "I was in Sessions" is not a fact about the
	// project Sidecar happened to be launched in, and keying it by one would
	// make the answer depend on which checkout the user started from.
	LastScope string `json:"lastScope,omitempty"`

	// ShowIdleWorktrees reveals "no session" rows on the global Workspaces list.
	// Fresh state leaves this off so the list is sessions by default.
	ShowIdleWorktrees bool `json:"showIdleWorktrees,omitempty"`

	// PinnedWorkspaceIDs is the ordered catalog IDs pinned to the top of the
	// global Workspaces list. First-pinned first. Gone IDs are dropped on sync.
	PinnedWorkspaceIDs []string `json:"pinnedWorkspaceIDs,omitempty"`

	// SessionsHiddenHosts are registered host IDs whose rows the global
	// Sessions browser withholds. It is a view filter, not a disable: the host
	// stays connected and its notifications still arrive, so turning it back on
	// is instant. Config's own `disabled` is the other thing, and it is
	// deliberately kept separate — "not this week" is a fact about the machine,
	// "not right now" is a fact about the list.
	SessionsHiddenHosts []string `json:"sessionsHiddenHosts,omitempty"`

	// SessionsSelected is the durable inventory row ID last shown in the
	// global Sessions browser. Empty (fresh profile, or a version that never
	// wrote it) leaves selection to the catalog's default.
	SessionsSelected string `json:"sessionsSelected,omitempty"`
	// SessionsPaneLayouts is the global Sessions browser's per-row pane trees,
	// keyed by the same durable inventory IDs as SessionsSelected. Only
	// composed trees are stored: a bare primary preview writes nothing.
	SessionsPaneLayouts map[string]*PaneLayoutJSON `json:"sessionsPaneLayouts,omitempty"`

	// WorkspaceListSort is the global Workspaces list's chosen order, stored as
	// its display label ("Activity", "Project", "Recent", "Name") rather than
	// an ordinal, so the file reads plainly and the enum can be reordered.
	// Unrecognised or empty falls back to the default. Its per-project
	// counterpart lives on WorkspaceState, because the two scopes answer the
	// question separately.
	WorkspaceListSort string `json:"workspaceListSort,omitempty"`

	// LastCreateAgent is the last agent chosen when creating a worktree.
	LastCreateAgent string `json:"lastCreateAgent,omitempty"`
	// LastGlobalCreateProject is the stable project root last chosen from the
	// cross-project Workspaces create flow.
	LastGlobalCreateProject string `json:"lastGlobalCreateProject,omitempty"`

	// AgentAutoApprove is the last auto-approve checkbox value per agent type.
	// A missing key is treated as false.
	AgentAutoApprove map[string]bool `json:"agentAutoApprove,omitempty"`

	// SeenDefaultThemeNotice records that the one-time "the default theme
	// changed" toast has been shown.
	//
	// It lives here and not in config.json on purpose. Sidecar only writes
	// config.json when a setting changes, and an absent ui.theme block is
	// exactly the signal that identifies a user who is being restyled. Writing
	// the flag into the config would record a theme choice as a side effect and
	// disarm the very mechanism it is flagging.
	SeenDefaultThemeNotice bool `json:"seenDefaultThemeNotice,omitempty"`
}

// FileBrowserTabState holds persistent tab state for the file browser.
type FileBrowserTabState struct {
	Path   string `json:"path,omitempty"`   // File path (relative)
	Scroll int    `json:"scroll,omitempty"` // Preview scroll offset
}

// FileBrowserState holds persistent file browser state.
type FileBrowserState struct {
	SelectedFile  string                `json:"selectedFile,omitempty"`  // Currently selected file path (relative)
	TreeScroll    int                   `json:"treeScroll,omitempty"`    // Tree pane scroll offset
	PreviewScroll int                   `json:"previewScroll,omitempty"` // Preview pane scroll offset
	ExpandedDirs  []string              `json:"expandedDirs,omitempty"`  // List of expanded directory paths
	ActivePane    string                `json:"activePane,omitempty"`    // "tree" or "preview"
	PreviewFile   string                `json:"previewFile,omitempty"`   // File being previewed (relative)
	TreeCursor    int                   `json:"treeCursor,omitempty"`    // Tree cursor position
	ShowIgnored   *bool                 `json:"showIgnored,omitempty"`   // Whether to show git-ignored files (nil = default true)
	Tabs          []FileBrowserTabState `json:"tabs,omitempty"`
	ActiveTab     int                   `json:"activeTab,omitempty"`
}

// WorkspaceState holds persistent workspace plugin state.
type WorkspaceState struct {
	WorkspaceName     string                     `json:"workspaceName,omitempty"`     // Name of selected workspace
	ShellTmuxName     string                     `json:"shellTmuxName,omitempty"`     // TmuxName of selected shell (empty = workspace selected)
	ShellDisplayNames map[string]string          `json:"shellDisplayNames,omitempty"` // TmuxName -> display name
	PaneLayout        *PaneLayoutJSON            `json:"paneLayout,omitempty"`        // Read-only migrate into PaneLayouts
	PaneLayouts       map[string]*PaneLayoutJSON `json:"paneLayouts,omitempty"`       // surface → layout
	// ListSort is the sidebar's chosen order, stored as its display label
	// ("Manual", "Activity", "Recent", "Name") rather than an ordinal. A label
	// survives reordering the enum, reads plainly in the state file, and an
	// unrecognised one falls back to the default instead of selecting an
	// arbitrary mode. Empty means the project has never chosen.
	ListSort string `json:"listSort,omitempty"`
}

// PaneLayoutJSON is the persisted, presentation-neutral pane-tree shape. Doc
// tabs are a list from the first version so adding tab UI later is additive.
// Issue tabs are IssueTabs plus Active. Issue and Scroll are read-only legacy
// and are not written after the first save.
type PaneLayoutJSON struct {
	Root    string `json:"root,omitempty"`
	Surface string `json:"surface,omitempty"`
	// HostID and the remaining source-identity fields are non-authoritative
	// hints for a later restore. HostIncarnation is never persisted.
	HostID        string             `json:"hostId,omitempty"`
	ProjectKey    string             `json:"projectKey,omitempty"`
	ProjectRoot   string             `json:"projectRoot,omitempty"`
	WorkspaceID   string             `json:"workspaceId,omitempty"`
	WorkspaceKind string             `json:"workspaceKind,omitempty"`
	WorkspaceKey  string             `json:"workspaceKey,omitempty"`
	Kind          string             `json:"kind,omitempty"`
	Split         *PaneSplitJSON     `json:"split,omitempty"`
	Tabs          []PaneDocTabJSON   `json:"tabs,omitempty"`
	IssueTabs     []PaneIssueTabJSON `json:"issueTabs,omitempty"`
	DiffTabs      []PaneDiffTabJSON  `json:"diffTabs,omitempty"`
	// ResourceTabs are external provider references. One list serves every
	// provider, so a new integration adds no field here.
	ResourceTabs []PaneResourceTabJSON `json:"resourceTabs,omitempty"`
	NoteTabs     []PaneNoteTabJSON     `json:"noteTabs,omitempty"`
	Active       int                   `json:"active,omitempty"`
	// Open is true when restore should rebuild the split. False means this
	// surface still has tabs but the pane is hidden (q). Omitted on a legacy
	// record that still has a split is treated as open by MigratePaneLayouts.
	Open bool `json:"open,omitempty"`
	// Issue and Scroll are the pre-tab issue leaf. Decode treats them as a
	// one-tab list when IssueTabs is absent.
	Issue  string `json:"issue,omitempty"`
	Scroll int    `json:"scroll,omitempty"`
	// Session is a live leaf's durable target selector — the tmux session name
	// it owns. It is never a tmux pane id: pane ids are reassigned by the
	// server and mean nothing after a restart, so a leaf that persisted one
	// would reattach to whatever now holds that id.
	Session string `json:"session,omitempty"`
	// Name is a live leaf's display title (a terminal split's header). Empty
	// on every other kind. Additive: older files omit it.
	Name string `json:"name,omitempty"`
	// FocusKind is the focused leaf in this tree, in PaneLayoutJSON kind
	// vocabulary (terminal|doc|issue|note|diff|resource|shell). Restore that
	// leaf; if it is gone, fall back to the primary. Additive: older files
	// omit it and restore focus to primary.
	FocusKind string `json:"focusKind,omitempty"`
}

// PaneIssueTabJSON is one persisted issue tab. Restore re-fetches the issue
// and applies Scroll; the body is not cached. OwnerName and OwnerRoot are set
// only on a cross-project tab: restore reopens it from its owning store
// without re-running the search, badge intact.
type PaneIssueTabJSON struct {
	Issue     string `json:"issue"`
	Scroll    int    `json:"scroll,omitempty"`
	OwnerName string `json:"ownerName,omitempty"`
	OwnerRoot string `json:"ownerRoot,omitempty"`
}

// PaneNoteTabJSON is one persisted note tab. Restore re-fetches the note
// through `td note`; the body is not cached.
type PaneNoteTabJSON struct {
	Note   string `json:"note"`
	Scroll int    `json:"scroll,omitempty"`
}

// PaneDiffTabJSON is one persisted Diff tab. Restore reloads the target
// spec; the diff body is not cached.
type PaneDiffTabJSON struct {
	Spec   string `json:"spec"`
	Path   string `json:"path,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Scroll int    `json:"scroll,omitempty"`
}

// PaneResourceTabJSON is one persisted external plugin reference. It is
// deliberately plugin-neutral and deliberately reference-only: Sidecar re-asks
// on restore and never writes a returned title, field, body, error, URL, or any
// auth state to disk.
//
// A reference necessarily includes the non-secret locator, such as CASH-1245,
// because that is the minimum needed to restore the pane the user had open.
//
// It carries the Resource leaf's three tab shapes, exactly one per record:
//
//   - MATCHED: Matcher and Locator, Collection empty. The frozen resource
//     protocol's shape, written by every release before the plugin protocol
//     and read back unchanged.
//   - COLLECTION: Collection set, Matcher and Locator empty, plus the view
//     position (Query, View, Sort, CursorID) so relaunch reopens the list the
//     user was reading rather than the collection's default page.
//   - ITEM: Collection and Locator, Matcher empty. One row of a collection,
//     which the plugin's get method addresses by collection and ID.
//
// Decode refuses a record that is more than one shape or none of them. Which
// shape a half-written record meant is not something the host can infer, and
// inferring it is how a restored tab silently becomes a different tab.
type PaneResourceTabJSON struct {
	Provider string `json:"provider"`
	Matcher  string `json:"matcher,omitempty"`
	Locator  string `json:"locator,omitempty"`
	// Collection and the view position beside it are written only for a
	// plugin-shaped tab. A matched document omits all five.
	Collection string `json:"collection,omitempty"`
	Query      string `json:"query,omitempty"`
	View       string `json:"view,omitempty"`
	Sort       string `json:"sort,omitempty"`
	CursorID   string `json:"cursorId,omitempty"`
	Scroll     int    `json:"scroll,omitempty"`
}

// MigratePaneLayouts copies a legacy single-slot PaneLayout into PaneLayouts
// when the map is empty. The legacy field is left for the writer to drop.
func MigratePaneLayouts(s *WorkspaceState) {
	if s == nil || len(s.PaneLayouts) > 0 || s.PaneLayout == nil || s.PaneLayout.Surface == "" {
		return
	}
	// Legacy writes omitted Open. Those records still wanted the split back;
	// hide is a later, explicit Open=false write into the map.
	if s.PaneLayout.Split != nil {
		s.PaneLayout.Open = true
	}
	s.PaneLayouts = map[string]*PaneLayoutJSON{s.PaneLayout.Surface: s.PaneLayout}
}

// PaneLayoutOpen reports whether restore should rebuild the split. Open=true
// restores. Open=false is hide: tabs stay in the map, the live tree does not.
func PaneLayoutOpen(l *PaneLayoutJSON) bool {
	return l != nil && l.Open
}

// PaneLayoutFor returns the layout stored for surface, migrating a legacy
// single-slot record first. The receiver is a copy; the stored state is not written.
func (s WorkspaceState) PaneLayoutFor(surface string) *PaneLayoutJSON {
	MigratePaneLayouts(&s)
	if surface == "" || s.PaneLayouts == nil {
		return nil
	}
	return s.PaneLayouts[surface]
}

// RekeyPaneLayout moves a saved surface to its canonical identity. If both
// identities exist, the canonical record wins and the duplicate legacy key is
// dropped. The returned bool reports whether the state needs writing.
func RekeyPaneLayout(s *WorkspaceState, legacySurface, canonicalSurface string) (*PaneLayoutJSON, bool) {
	if s == nil || canonicalSurface == "" {
		return nil, false
	}
	MigratePaneLayouts(s)
	if s.PaneLayouts == nil {
		return nil, false
	}
	canonical := s.PaneLayouts[canonicalSurface]
	if legacySurface == "" || legacySurface == canonicalSurface {
		return canonical, false
	}
	legacy := s.PaneLayouts[legacySurface]
	if canonical != nil {
		if legacy != nil {
			delete(s.PaneLayouts, legacySurface)
			return canonical, true
		}
		return canonical, false
	}
	if legacy == nil {
		return nil, false
	}
	delete(s.PaneLayouts, legacySurface)
	legacy.Surface = canonicalSurface
	s.PaneLayouts[canonicalSurface] = legacy
	return legacy, true
}

// ForgetPaneLayouts removes only the named surfaces, including a matching
// legacy single-slot record. It reports whether anything changed so callers
// can avoid unrelated state writes while still writing a last-entry removal.
func ForgetPaneLayouts(s *WorkspaceState, surfaces ...string) bool {
	if s == nil || len(surfaces) == 0 {
		return false
	}
	wanted := make(map[string]struct{}, len(surfaces))
	for _, surface := range surfaces {
		if surface != "" {
			wanted[surface] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return false
	}
	MigratePaneLayouts(s)
	changed := false
	for surface := range wanted {
		if _, ok := s.PaneLayouts[surface]; ok {
			delete(s.PaneLayouts, surface)
			changed = true
		}
	}
	if len(s.PaneLayouts) == 0 && s.PaneLayouts != nil {
		s.PaneLayouts = nil
	}
	if s.PaneLayout != nil {
		if _, ok := wanted[s.PaneLayout.Surface]; ok {
			s.PaneLayout = nil
			changed = true
		}
	}
	return changed
}

type PaneSplitJSON struct {
	Axis  string          `json:"axis"`
	Ratio int             `json:"ratio"`
	A     *PaneLayoutJSON `json:"a"`
	B     *PaneLayoutJSON `json:"b"`
}

type PaneDocTabJSON struct {
	Path   string `json:"path"`
	Mode   string `json:"mode,omitempty"`
	Wrap   bool   `json:"wrap,omitempty"`
	Scroll int    `json:"scroll,omitempty"`
}

// NotesState holds persistent notes plugin state.
type NotesState struct {
	ListWidth    int    `json:"listWidth,omitempty"`    // Width of list pane
	LastNoteID   string `json:"lastNoteID,omitempty"`   // Last selected note ID
	ShowArchived bool   `json:"showArchived,omitempty"` // Whether to show archived notes
}

var (
	current *State
	mu      sync.RWMutex
	path    string
)

// Init loads state from the default location.
func Init() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return InitWithDir(filepath.Join(home, ".config", "sidecar"))
}

// InitWithDir loads state from a specified directory.
// This is primarily for testing to avoid reading real user state.
func InitWithDir(dir string) error {
	path = filepath.Join(dir, "state.json")
	return Load()
}

// Load reads state from disk.
func Load() error {
	mu.Lock()
	defer mu.Unlock()

	current = &State{
		GitDiffMode: "unified", // default
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // no state file yet, use defaults
	}
	if err != nil {
		return err
	}

	return json.Unmarshal(data, current)
}

// Save writes state to disk.
func Save() error {
	mu.RLock()
	defer mu.RUnlock()

	if current == nil {
		return nil
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// GetGitDiffMode returns the saved diff mode.
func GetGitDiffMode() string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return "unified"
	}
	return current.GitDiffMode
}

// SetGitDiffMode saves the diff mode preference.
func SetGitDiffMode(mode string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.GitDiffMode = mode
	mu.Unlock()
	return Save()
}

// GetWorkspaceDiffMode returns the saved workspace diff mode.
func GetWorkspaceDiffMode() string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.WorkspaceDiffMode == "" {
		return "unified"
	}
	return current.WorkspaceDiffMode
}

// SetWorkspaceDiffMode saves the workspace diff mode preference.
func SetWorkspaceDiffMode(mode string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.WorkspaceDiffMode = mode
	mu.Unlock()
	return Save()
}

// GetGitGraphEnabled returns whether the commit graph is enabled.
func GetGitGraphEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return false
	}
	return current.GitGraphEnabled
}

// SetGitGraphEnabled saves the commit graph preference.
func SetGitGraphEnabled(enabled bool) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.GitGraphEnabled = enabled
	mu.Unlock()
	return Save()
}

// GetLineWrapEnabled returns whether line wrapping is enabled.
func GetLineWrapEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return false
	}
	return current.LineWrapEnabled
}

// SetLineWrapEnabled saves the line wrap preference.
func SetLineWrapEnabled(enabled bool) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.LineWrapEnabled = enabled
	mu.Unlock()
	return Save()
}

// GetFileBrowserTreeWidth returns the saved file browser tree pane width.
// Returns 0 if no preference is saved (use default).
func GetFileBrowserTreeWidth() int {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return 0
	}
	return current.FileBrowserTreeWidth
}

// SetFileBrowserTreeWidth saves the file browser tree pane width.
func SetFileBrowserTreeWidth(width int) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.FileBrowserTreeWidth = width
	mu.Unlock()
	return Save()
}

// GetGitStatusSidebarWidth returns the saved git status sidebar width.
// Returns 0 if no preference is saved (use default).
func GetGitStatusSidebarWidth() int {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return 0
	}
	return current.GitStatusSidebarWidth
}

// SetGitStatusSidebarWidth saves the git status sidebar width.
func SetGitStatusSidebarWidth(width int) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.GitStatusSidebarWidth = width
	mu.Unlock()
	return Save()
}

// GetPluginBrowserSplit returns the saved list share for one plugin browser,
// as a percentage of its content box. Returns 0 when nothing is saved for that
// instance, which means "use the browser's default".
func GetPluginBrowserSplit(instance string) int {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || instance == "" {
		return 0
	}
	return current.PluginBrowserSplit[instance]
}

// SetPluginBrowserSplit saves one plugin browser's list share.
func SetPluginBrowserSplit(instance string, percent int) error {
	if instance == "" {
		return nil
	}
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.PluginBrowserSplit == nil {
		current.PluginBrowserSplit = make(map[string]int)
	}
	current.PluginBrowserSplit[instance] = percent
	mu.Unlock()
	return Save()
}

// GetNotificationCentreWidth returns the saved notification centre panel width.
// Returns 0 if no preference is saved (use default).
func GetNotificationCentreWidth() int {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return 0
	}
	return current.NotificationCentreWidth
}

// SetNotificationCentreWidth saves the notification centre panel width.
func SetNotificationCentreWidth(width int) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.NotificationCentreWidth = width
	mu.Unlock()
	return Save()
}

// GetConversationsSideWidth returns the saved conversations sidebar width.
// Returns 0 if no preference is saved (use default).
func GetConversationsSideWidth() int {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return 0
	}
	return current.ConversationsSideWidth
}

// SetConversationsSideWidth saves the conversations sidebar width.
func SetConversationsSideWidth(width int) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.ConversationsSideWidth = width
	mu.Unlock()
	return Save()
}

// GetWorkspaceSidebarWidth returns the saved workspace sidebar width.
// Returns 0 if no preference is saved (use default).
func GetWorkspaceSidebarWidth() int {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return 0
	}
	return current.WorkspaceSidebarWidth
}

// SetWorkspaceSidebarWidth saves the workspace sidebar width.
func SetWorkspaceSidebarWidth(width int) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.WorkspaceSidebarWidth = width
	mu.Unlock()
	return Save()
}

// GetDiffTabFileListWidth returns the saved diff tab file list width (in pixels).
// Returns 0 if no preference is saved (use default).
func GetDiffTabFileListWidth() int {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return 0
	}
	return current.DiffTabFileListWidth
}

// SetDiffTabFileListWidth saves the diff tab file list width (in pixels).
func SetDiffTabFileListWidth(width int) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.DiffTabFileListWidth = width
	mu.Unlock()
	return Save()
}

// LegacyTermPanel returns the pre-split terminal panel preferences: whether the
// panel was up at exit, which side it was on ("bottom" or "right"), and the
// percentage it occupied. They exist only to be converted into a pane-tree
// split once; nothing writes them.
func LegacyTermPanel() (visible bool, side string, size int) {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return false, "", 0
	}
	return current.LegacyTermPanelVisible, current.LegacyTermPanelSide, current.LegacyTermPanelSize
}

// ClearLegacyTermPanel drops the legacy keys once they have been converted, so
// the conversion happens exactly once per user.
func ClearLegacyTermPanel() error {
	mu.Lock()
	if current == nil {
		mu.Unlock()
		return nil
	}
	if !current.LegacyTermPanelVisible && current.LegacyTermPanelSide == "" && current.LegacyTermPanelSize == 0 {
		mu.Unlock()
		return nil
	}
	current.LegacyTermPanelVisible = false
	current.LegacyTermPanelSide = ""
	current.LegacyTermPanelSize = 0
	mu.Unlock()
	return Save()
}

// GetFileBrowserState returns the saved file browser state for a given working directory.
func GetFileBrowserState(workdir string) FileBrowserState {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.FileBrowser == nil {
		return FileBrowserState{}
	}
	return current.FileBrowser[workdir]
}

// GetFileBrowserStateForWorkDir returns content state keyed by the concrete
// worktree. Older Sidecar versions keyed this state by the repository root; on
// first access, copy that legacy value forward only when the worktree has no
// value of its own. The root entry is deliberately retained for rollback.
func GetFileBrowserStateForWorkDir(workdir, projectRoot string) FileBrowserState {
	mu.Lock()
	if current == nil || current.FileBrowser == nil {
		mu.Unlock()
		return FileBrowserState{}
	}
	if value, ok := current.FileBrowser[workdir]; ok {
		mu.Unlock()
		return value
	}
	legacy, ok := current.FileBrowser[projectRoot]
	if !ok || workdir == projectRoot {
		mu.Unlock()
		return FileBrowserState{}
	}
	current.FileBrowser[workdir] = legacy
	mu.Unlock()
	_ = Save()
	return legacy
}

// SetFileBrowserState saves the file browser state for a given working directory.
func SetFileBrowserState(workdir string, fbState FileBrowserState) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.FileBrowser == nil {
		current.FileBrowser = make(map[string]FileBrowserState)
	}
	current.FileBrowser[workdir] = fbState
	mu.Unlock()
	return Save()
}

// GetWorkspaceState returns the saved workspace state for a given working directory.
func GetWorkspaceState(workdir string) WorkspaceState {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.Workspace == nil {
		return WorkspaceState{}
	}
	return current.Workspace[workdir]
}

// SetWorkspaceState saves the workspace state for a given working directory.
func SetWorkspaceState(workdir string, wtState WorkspaceState) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.Workspace == nil {
		current.Workspace = make(map[string]WorkspaceState)
	}
	current.Workspace[workdir] = wtState
	mu.Unlock()
	return Save()
}

func contentDeckKey(workdir, pluginID string) string { return workdir + "\x00" + pluginID }

func GetContentDeck(workdir, pluginID string) json.RawMessage {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.ContentDeck == nil {
		return nil
	}
	return append(json.RawMessage(nil), current.ContentDeck[contentDeckKey(workdir, pluginID)]...)
}

func SetContentDeck(workdir, pluginID string, raw json.RawMessage) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.ContentDeck == nil {
		current.ContentDeck = make(map[string]json.RawMessage)
	}
	key := contentDeckKey(workdir, pluginID)
	if len(raw) == 0 {
		delete(current.ContentDeck, key)
	} else {
		current.ContentDeck[key] = append(json.RawMessage(nil), raw...)
	}
	mu.Unlock()
	return Save()
}

// GetActivePlugin returns the saved active plugin ID for a given working directory.
func GetActivePlugin(workdir string) string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.ActivePlugin == nil {
		return ""
	}
	return current.ActivePlugin[workdir]
}

// GetActivePluginForWorkDir performs the additive migration from the former
// repository-root key to a concrete worktree key. It never overwrites an
// existing worktree choice and retains the legacy entry.
func GetActivePluginForWorkDir(workdir, projectRoot string) string {
	mu.Lock()
	if current == nil || current.ActivePlugin == nil {
		mu.Unlock()
		return ""
	}
	if value, ok := current.ActivePlugin[workdir]; ok {
		mu.Unlock()
		return value
	}
	legacy, ok := current.ActivePlugin[projectRoot]
	if !ok || workdir == projectRoot {
		mu.Unlock()
		return ""
	}
	current.ActivePlugin[workdir] = legacy
	mu.Unlock()
	_ = Save()
	return legacy
}

// SetActivePlugin saves the active plugin ID for a given working directory.
func SetActivePlugin(workdir, pluginID string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.ActivePlugin == nil {
		current.ActivePlugin = make(map[string]string)
	}
	current.ActivePlugin[workdir] = pluginID
	mu.Unlock()
	return Save()
}

// BoundLocation is a host-qualified last location. Empty HostID is unused;
// local last-worktree memory stays on LastWorktreePath.
type BoundLocation struct {
	HostID      string `json:"hostId,omitempty"`
	ProjectKey  string `json:"projectKey,omitempty"`
	WorktreeKey string `json:"worktreeKey,omitempty"`
}

// GetLastBoundLocation returns the last host-qualified destination, or false
// when none is stored.
func GetLastBoundLocation() (BoundLocation, bool) {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.LastBoundLocation == nil {
		return BoundLocation{}, false
	}
	return *current.LastBoundLocation, true
}

// SetLastBoundLocation persists a host-qualified destination. It never writes
// a remote path into LastWorktreePath.
func SetLastBoundLocation(loc BoundLocation) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	copied := loc
	current.LastBoundLocation = &copied
	mu.Unlock()
	return Save()
}

// ClearLastBoundLocation drops the stored host-qualified destination.
func ClearLastBoundLocation() error {
	mu.Lock()
	if current == nil {
		mu.Unlock()
		return nil
	}
	current.LastBoundLocation = nil
	mu.Unlock()
	return Save()
}

// GetLastWorktreePath returns the last active worktree path for a main repo.
func GetLastWorktreePath(mainRepoPath string) string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.LastWorktreePath == nil {
		return ""
	}
	return current.LastWorktreePath[mainRepoPath]
}

// SetLastWorktreePath saves the last active worktree path for a main repo.
func SetLastWorktreePath(mainRepoPath, worktreePath string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.LastWorktreePath == nil {
		current.LastWorktreePath = make(map[string]string)
	}
	current.LastWorktreePath[mainRepoPath] = worktreePath
	mu.Unlock()
	return Save()
}

// lastRemoteWorktreeKey is hosts.ScopedKey(HostID, ProjectKey), inlined so
// this package does not import hosts.
func lastRemoteWorktreeKey(hostID, projectKey string) string {
	return hostID + "\x1f" + projectKey
}

// GetLastRemoteWorktree returns the host-side WorktreeKey last bound for
// (hostID, projectKey). Empty when none is stored. It is not visible to
// GetLastWorktreePath.
func GetLastRemoteWorktree(hostID, projectKey string) string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.LastRemoteWorktree == nil {
		return ""
	}
	return current.LastRemoteWorktree[lastRemoteWorktreeKey(hostID, projectKey)]
}

// SetLastRemoteWorktree stores a host-side WorktreeKey for (hostID, projectKey).
// Empty keys are ignored. It never writes into LastWorktreePath.
func SetLastRemoteWorktree(hostID, projectKey, worktreeKey string) error {
	if hostID == "" || projectKey == "" || worktreeKey == "" {
		return nil
	}
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.LastRemoteWorktree == nil {
		current.LastRemoteWorktree = make(map[string]string)
	}
	current.LastRemoteWorktree[lastRemoteWorktreeKey(hostID, projectKey)] = worktreeKey
	mu.Unlock()
	return Save()
}

// GetLastGlobalTab returns the saved global tab ID, or empty if none is saved.
func GetLastGlobalTab() string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return ""
	}
	return current.LastGlobalTab
}

// SetLastGlobalTab saves the last selected global tab ID.
func SetLastGlobalTab(tab string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.LastGlobalTab = tab
	mu.Unlock()
	return Save()
}

// GetLastScope returns the saved top-level scope ID, or empty when none is
// saved.
func GetLastScope() string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return ""
	}
	return current.LastScope
}

// SetLastScope saves the top-level scope the user is in.
func SetLastScope(scope string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.LastScope == scope {
		mu.Unlock()
		return nil
	}
	current.LastScope = scope
	mu.Unlock()
	return Save()
}

// GetWorkspaceListSort returns the global Workspaces list's saved order label,
// or "" when the user has never chosen one.
func GetWorkspaceListSort() string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return ""
	}
	return current.WorkspaceListSort
}

// SetWorkspaceListSort saves the global Workspaces list's chosen order.
func SetWorkspaceListSort(label string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.WorkspaceListSort = label
	mu.Unlock()
	return Save()
}

// GetShowIdleWorktrees reports whether the global list should include idle
// worktrees. A missing or fresh state is off.
func GetShowIdleWorktrees() bool {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return false
	}
	return current.ShowIdleWorktrees
}

// SetShowIdleWorktrees saves the global idle-worktree visibility preference.
func SetShowIdleWorktrees(show bool) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.ShowIdleWorktrees = show
	mu.Unlock()
	return Save()
}

// GetSessionsHiddenHosts returns the host IDs the global browser is currently
// withholding rows for. Fresh state hides nothing.
func GetSessionsHiddenHosts() []string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return nil
	}
	return append([]string(nil), current.SessionsHiddenHosts...)
}

// SetSessionsHiddenHosts saves which hosts the global browser withholds.
func SetSessionsHiddenHosts(ids []string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.SessionsHiddenHosts = append([]string(nil), ids...)
	mu.Unlock()
	return Save()
}

// GetSeenDefaultThemeNotice reports whether the one-time new-default-theme
// toast has already been shown. Fresh state has not seen it.
func GetSeenDefaultThemeNotice() bool {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return false
	}
	return current.SeenDefaultThemeNotice
}

// SetSeenDefaultThemeNotice records that the notice has been shown, so it never
// appears again.
func SetSeenDefaultThemeNotice(seen bool) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.SeenDefaultThemeNotice = seen
	mu.Unlock()
	return Save()
}

// GetPinnedWorkspaceIDs returns the saved global pin order, or nil if none.
func GetPinnedWorkspaceIDs() []string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || len(current.PinnedWorkspaceIDs) == 0 {
		return nil
	}
	return append([]string(nil), current.PinnedWorkspaceIDs...)
}

// SetPinnedWorkspaceIDs saves the global Workspaces pin order.
func SetPinnedWorkspaceIDs(ids []string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.PinnedWorkspaceIDs = uniquePinnedIDs(ids)
	mu.Unlock()
	return Save()
}

// GetSessionsSelected returns the durable Sessions row ID last shown, or empty
// when none is saved.
func GetSessionsSelected() string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return ""
	}
	return current.SessionsSelected
}

// SetSessionsSelected saves the Sessions row last shown. An unchanged value
// is a no-op so arrowing the sidebar can debounce down to one write.
func SetSessionsSelected(id string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.SessionsSelected == id {
		mu.Unlock()
		return nil
	}
	current.SessionsSelected = id
	mu.Unlock()
	return Save()
}

// GetSessionsPaneLayout returns a copy of the persisted Sessions tree for row
// id, or nil when none is stored.
func GetSessionsPaneLayout(id string) *PaneLayoutJSON {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.SessionsPaneLayouts == nil || id == "" {
		return nil
	}
	return clonePaneLayout(current.SessionsPaneLayouts[id])
}

// GetSessionsPaneLayouts returns a copy of every persisted Sessions tree.
func GetSessionsPaneLayouts() map[string]*PaneLayoutJSON {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || len(current.SessionsPaneLayouts) == 0 {
		return nil
	}
	out := make(map[string]*PaneLayoutJSON, len(current.SessionsPaneLayouts))
	for id, layout := range current.SessionsPaneLayouts {
		out[id] = clonePaneLayout(layout)
	}
	return out
}

// SetSessionsPaneLayout stores (or, when layout is nil, deletes) the Sessions
// tree for row id. Unchanged JSON is a no-op.
func SetSessionsPaneLayout(id string, layout *PaneLayoutJSON) error {
	if id == "" {
		return nil
	}
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if layout == nil {
		if current.SessionsPaneLayouts == nil {
			mu.Unlock()
			return nil
		}
		if _, ok := current.SessionsPaneLayouts[id]; !ok {
			mu.Unlock()
			return nil
		}
		delete(current.SessionsPaneLayouts, id)
		if len(current.SessionsPaneLayouts) == 0 {
			current.SessionsPaneLayouts = nil
		}
		mu.Unlock()
		return Save()
	}
	if paneLayoutJSONEqual(current.SessionsPaneLayouts[id], layout) {
		mu.Unlock()
		return nil
	}
	if current.SessionsPaneLayouts == nil {
		current.SessionsPaneLayouts = make(map[string]*PaneLayoutJSON)
	}
	current.SessionsPaneLayouts[id] = clonePaneLayout(layout)
	mu.Unlock()
	return Save()
}

func clonePaneLayout(l *PaneLayoutJSON) *PaneLayoutJSON {
	if l == nil {
		return nil
	}
	raw, err := json.Marshal(l)
	if err != nil {
		return nil
	}
	var out PaneLayoutJSON
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return &out
}

func paneLayoutJSONEqual(a, b *PaneLayoutJSON) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

func uniquePinnedIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// ClearLastWorktreePath removes the saved worktree path for a main repo.
func ClearLastWorktreePath(mainRepoPath string) error {
	mu.Lock()
	if current == nil || current.LastWorktreePath == nil {
		mu.Unlock()
		return nil
	}
	delete(current.LastWorktreePath, mainRepoPath)
	mu.Unlock()
	return Save()
}

// GetNotesState returns the saved notes state for a given working directory.
func GetNotesState(workdir string) NotesState {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.Notes == nil {
		return NotesState{}
	}
	return current.Notes[workdir]
}

// SetNotesState saves the notes state for a given working directory.
func SetNotesState(workdir string, notesState NotesState) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.Notes == nil {
		current.Notes = make(map[string]NotesState)
	}
	current.Notes[workdir] = notesState
	mu.Unlock()
	return Save()
}

// SetNotesListWidth saves just the notes list width for a given working directory.
func SetNotesListWidth(width int) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.Notes == nil {
		current.Notes = make(map[string]NotesState)
	}
	// Use empty workdir as global setting
	notesState := current.Notes[""]
	notesState.ListWidth = width
	current.Notes[""] = notesState
	mu.Unlock()
	return Save()
}

// GetLastCreateAgent returns the last agent chosen when creating a worktree.
func GetLastCreateAgent() string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return ""
	}
	return current.LastCreateAgent
}

// SetLastCreateAgent saves the last agent chosen when creating a worktree.
func SetLastCreateAgent(agent string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.LastCreateAgent = agent
	mu.Unlock()
	return Save()
}

// GetLastGlobalCreateProject returns the last project root chosen in global
// Workspaces creation.
func GetLastGlobalCreateProject() string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return ""
	}
	return current.LastGlobalCreateProject
}

// SetLastGlobalCreateProject persists the last project root chosen in global
// Workspaces creation.
func SetLastGlobalCreateProject(project string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.LastGlobalCreateProject = project
	mu.Unlock()
	return Save()
}

// GetAgentAutoApprove returns the persisted auto-approve preference for agent.
// A missing key is false.
func GetAgentAutoApprove(agent string) bool {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.AgentAutoApprove == nil {
		return false
	}
	return current.AgentAutoApprove[agent]
}

// SetAgentAutoApprove saves the auto-approve preference for agent.
func SetAgentAutoApprove(agent string, on bool) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.AgentAutoApprove == nil {
		current.AgentAutoApprove = make(map[string]bool)
	}
	current.AgentAutoApprove[agent] = on
	mu.Unlock()
	return Save()
}
