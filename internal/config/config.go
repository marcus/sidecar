package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/tty"
)

const (
	OverviewWorktreeScopeProject  = "project"
	OverviewWorktreeScopeWorktree = "worktree"
)

// Config is the root configuration structure.
type Config struct {
	Projects ProjectsConfig `json:"projects"`
	Plugins  PluginsConfig  `json:"plugins"`
	Keymap   KeymapConfig   `json:"keymap"`
	UI       UIConfig       `json:"ui"`
	Features FeaturesConfig `json:"features"`
	// Selection configures text selection wherever it is offered. It is
	// app-level because selection is one behaviour projected onto every
	// surface, not a per-plugin setting.
	Selection SelectionConfig `json:"selection"`
	// TerminalResources configures external terminal resource providers. It is
	// app-level rather than per-plugin because providers serve both workspace
	// projections and are not project-tab plugins.
	TerminalResources TerminalResourcesConfig `json:"terminalResources"`
	// Notifications configures the notification system. It is app-level
	// because the store, the toasts, and the centre are the shell's, not any
	// plugin's.
	Notifications NotificationsConfig `json:"notifications,omitempty"`
	// Shells configures the shell records Sidecar owns in shells.json. It is
	// app-level because every surface writes those records, including
	// `sidecar shell forget` in a process that loads no plugins.
	Shells ShellsConfig `json:"shells,omitempty"`
	// Hosts registers other machines to observe over SSH. App-level because a
	// host contributes rows to the global Sessions browser, which belongs to
	// the shell rather than to any project or plugin.
	Hosts HostsConfig `json:"hosts,omitempty"`
	// Detection configures the agent screen-detection lane. App-level because
	// the detection manifests answer for every surface that shows an agent
	// state, and for `sidecar agent explain` in a process that loads no
	// plugins at all.
	Detection DetectionConfig `json:"detection,omitempty"`
}

// The values detection.remoteManifests takes, besides an arbitrary catalog URL.
const (
	// RemoteManifestsOff is the default. The manifests vendored into the binary
	// are the only upstream source and Sidecar never reaches the network for
	// one.
	RemoteManifestsOff = "off"
	// RemoteManifestsHerdrDev is Herdr's own published catalog.
	RemoteManifestsHerdrDev = "herdr.dev"
)

// HerdrCatalogURL is the index RemoteManifestsHerdrDev resolves to. It is
// Herdr's own DEFAULT_CATALOG_URL (src/detect/manifest_update.rs:17).
const HerdrCatalogURL = "https://herdr.dev/agent-detection/index.toml"

// DetectionConfig configures the screen-detection lane.
type DetectionConfig struct {
	// RemoteManifests names where, if anywhere, Sidecar may fetch newer
	// agent-detection manifests from while it is running: "off" (the default),
	// "herdr.dev", or the http(s) URL of a catalog index.
	//
	// Anything else is refused rather than guessed at. An unrecognised value
	// could plausibly mean "the user wanted this on and mistyped it" or "the
	// user wanted it off and mistyped it", and only one of those two readings
	// can be wrong quietly: silently reading a typo as "on" would put a network
	// fetch behind a setting nobody successfully turned on. So a value this
	// package cannot resolve turns nothing on and is reported. The loader warns
	// and keeps the value verbatim so `sidecar agent manifests` can show it back
	// with the reason it was refused; RemoteCatalogURL is the gate, and it
	// returns an error for such a value, so no caller can fetch on it.
	RemoteManifests string `json:"remoteManifests,omitempty"`
}

// RemoteCatalogURL resolves the setting to the catalog index URL to fetch, or
// "" when fetching is off. An unrecognised value is an error, never a URL and
// never a silent "on".
func (c DetectionConfig) RemoteCatalogURL() (string, error) {
	value := strings.TrimSpace(c.RemoteManifests)
	switch {
	case value == "" || strings.EqualFold(value, RemoteManifestsOff):
		return "", nil
	case strings.EqualFold(value, RemoteManifestsHerdrDev):
		return HerdrCatalogURL, nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("detection.remoteManifests %q is not %q, %q, or a URL: %w",
			c.RemoteManifests, RemoteManifestsOff, RemoteManifestsHerdrDev, err)
	}
	// http as well as https, because the only way to point this at a local
	// catalog -- a mirror, a test server, a file being tuned before it is
	// published -- is a plain http URL, and refusing those would leave no way
	// to exercise the feature without a certificate. The default is https and
	// the setting is off unless someone turns it on, so choosing http is a
	// choice the operator makes explicitly about their own catalog.
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("detection.remoteManifests %q is not %q, %q, or an http(s) URL",
			c.RemoteManifests, RemoteManifestsOff, RemoteManifestsHerdrDev)
	}
	return value, nil
}

// RemoteManifestsEnabled reports whether a runtime fetch is configured and
// resolvable.
func (c DetectionConfig) RemoteManifestsEnabled() bool {
	url, err := c.RemoteCatalogURL()
	return err == nil && url != ""
}

// HostsConfig registers remote machines running Sidecar.
//
// Reachability is deliberately not configured here. The target is whatever the
// user's ssh_config already resolves — their keys, their ProxyJump, their
// agent — so Sidecar adds no second place to describe how to reach a machine,
// and anything that works in `ssh <target>` works here.
type HostsConfig struct {
	List []HostConfig `json:"list"`
}

// HostConfig is one registered machine.
type HostConfig struct {
	// ID is the local name shown in the UI and used to scope remote workspace
	// IDs. Defaults to Target when empty.
	ID string `json:"id,omitempty"`
	// Target is the ssh destination.
	Target string `json:"target"`
	// Binary is an explicit path to sidecar on that host. Usually unnecessary:
	// the connection runs through a login shell, which finds a Homebrew or
	// package-managed install. Set it when the host puts sidecar somewhere a
	// login shell does not look.
	Binary string `json:"binary,omitempty"`
	// Config is an optional -config path for the remote sidecar, so a host can
	// be observed against a config other than its user default.
	Config string `json:"config,omitempty"`
	// Env is extra environment for the remote Sidecar, as KEY=VALUE strings.
	//
	// It exists so a proof run can pin a host to an isolated tmux server and
	// state tree (TMUX_TMPDIR, XDG_STATE_HOME, SIDECAR_ISOLATED_STATE) exactly
	// as a local proof run does. Without it the isolation discipline stops at
	// the machine boundary, which is the point at which it matters most — the
	// remote tree belongs to someone else.
	Env []string `json:"env,omitempty"`
	// Disabled keeps a host registered but unconnected, which is what a user
	// wants for a machine that is off this week — deleting the entry to stop
	// the reconnect attempts would lose its settings.
	Disabled bool `json:"disabled,omitempty"`
}

// SelectionConfig configures text selection across surfaces.
type SelectionConfig struct {
	// CopyOnSelect copies a finished selection without a copy chord.
	// Default: false — a selection that silently replaces the clipboard is the
	// single most complained-about behaviour in the editors that ship it, so
	// selecting and copying stay separate acts unless asked otherwise.
	CopyOnSelect bool `json:"copyOnSelect,omitempty"`
}

// FeaturesConfig holds feature flag settings.
type FeaturesConfig struct {
	Flags map[string]bool `json:"flags"`
}

// ProjectsConfig configures project detection and layout.
type ProjectsConfig struct {
	Mode string          `json:"mode"` // "single" for now
	Root string          `json:"root"` // "." default
	List []ProjectConfig `json:"list"` // list of configured projects for switcher
}

// ProjectConfig represents a single project in the project switcher.
type ProjectConfig struct {
	Name          string               `json:"name"`                    // display name for the project
	Path          string               `json:"path"`                    // absolute path to project root (supports ~ expansion)
	Theme         *ThemeConfig         `json:"theme,omitempty"`         // per-project theme (nil = use global)
	LastOpenInApp string               `json:"lastOpenInApp,omitempty"` // last app used to open this project (e.g. "vscode", "goland")
	OpenIn        string               `json:"openIn,omitempty"`        // preferred "open in" app for this project; last-used is the fallback
	WorktreeSetup *WorktreeSetupConfig `json:"worktreeSetup,omitempty"` // optional per-project setup policy
	// AddedAt is when this project was registered with Sidecar, written once by
	// AddProject. It is a registration date and nothing else: Sidecar does not
	// know when the directory or the repository came into being, so the project
	// switcher labels it "Date added" rather than "created".
	//
	// It is a pointer so a project registered before Sidecar recorded this
	// serializes no key at all, and absent stays honestly unknown. Nothing
	// backfills it — not the upgrade time, not the directory's birth time (which
	// changes on a clone or a restore), not the first commit (which is
	// repository history, not local registration).
	AddedAt *time.Time `json:"addedAt,omitempty"`
}

// WorktreeSetupForProject returns the project override when present, otherwise
// the workspace-wide default.
func (c *Config) WorktreeSetupForProject(projectPath string) WorktreeSetupConfig {
	if c == nil {
		return WorktreeSetupConfig{}
	}
	for _, project := range c.Projects.List {
		if filepath.Clean(ExpandPath(project.Path)) == filepath.Clean(projectPath) && project.WorktreeSetup != nil {
			return *project.WorktreeSetup
		}
	}
	return c.Plugins.Workspace.WorktreeSetup
}

// PluginsConfig holds per-plugin configuration.
type PluginsConfig struct {
	GitStatus     GitStatusPluginConfig     `json:"git-status"`
	TDMonitor     TDMonitorPluginConfig     `json:"td-monitor"`
	FileBrowser   FileBrowserPluginConfig   `json:"file-browser"`
	Conversations ConversationsPluginConfig `json:"conversations"`
	Workspace     WorkspacePluginConfig     `json:"workspace"`
	Notes         NotesPluginConfig         `json:"notes"`
	Tasks         TasksPluginConfig         `json:"tasks"`
	// External is the ordered list of external plugin instances Sidecar hosts
	// over the plugin protocol. It lives under plugins because the settings
	// page already groups every plugin here and because enablement is
	// plugins.<id>.enabled for every class.
	External []PluginInstanceConfig `json:"external,omitempty"`
}

// Tab positions for the Tasks plugin.
const (
	// TasksPositionAfterWorkspaces places the Tasks tab immediately after the
	// workspaces tab. This is the default.
	TasksPositionAfterWorkspaces = "after-workspaces"
	// TasksPositionAfterNotes places the Tasks tab immediately after the notes
	// tab.
	TasksPositionAfterNotes = "after-notes"
)

// TasksPluginConfig configures the embedded Tasks plugin.
//
// There is deliberately no store/JSONL path: the embedded Tasks package uses
// Tasks' own configuration resolution.
type TasksPluginConfig struct {
	// Enabled is the unified plugins.<id>.enabled switch. See
	// NotesPluginConfig.Enabled for why it is a pointer; the deprecated alias
	// here is the tasks_plugin feature flag.
	Enabled *bool `json:"enabled,omitempty"`
	// Position was the anchor the Tasks tab was inserted after while Tasks was
	// a project plugin. Tasks is now a tab of the global space (Agents,
	// Workspaces, Tasks), whose order is fixed, so the value is accepted,
	// validated, and preserved for older configs but no longer moves anything.
	// One of TasksPositionAfterWorkspaces (default) or TasksPositionAfterNotes;
	// unknown values are coerced back to the default by Validate.
	Position string `json:"position,omitempty"`
}

// GitStatusPluginConfig configures the git status plugin.
type GitStatusPluginConfig struct {
	Enabled         bool          `json:"enabled"`
	RefreshInterval time.Duration `json:"refreshInterval"`
}

// FileBrowserPluginConfig configures the file browser plugin.
type FileBrowserPluginConfig struct {
	// Enabled controls whether the file browser plugin is loaded. Default: true.
	Enabled bool `json:"enabled"`
}

// tdMonitorDefaultRefresh is how often the td panel polls. Each poll forks
// several `git` processes and reads the task database, and it runs whether or
// not the panel is visible, so this is deliberately slow: a Sidecar left open
// all day should not be a battery cost for data that changes a few times an
// hour. Lower it per project with plugins.td-monitor.refreshInterval.
const tdMonitorDefaultRefresh = 10 * time.Second

// TDMonitorPluginConfig configures the TD monitor plugin.
type TDMonitorPluginConfig struct {
	Enabled         bool          `json:"enabled"`
	RefreshInterval time.Duration `json:"refreshInterval"`
	DBPath          string        `json:"dbPath"`
}

// ConversationsPluginConfig configures the conversations plugin.
type ConversationsPluginConfig struct {
	Enabled       bool   `json:"enabled"`
	ClaudeDataDir string `json:"claudeDataDir"`
	// DefaultCategoryFilter sets the default session category filter on startup.
	// Example: ["interactive"] hides cron/system sessions by default.
	// Empty or omitted means show all sessions (no filter).
	DefaultCategoryFilter []string `json:"defaultCategoryFilter,omitempty"`
}

// WorkspacePluginConfig configures the workspace plugin.
type WorkspacePluginConfig struct {
	// DirPrefix prefixes workspace directory names with the repo name (e.g., 'myrepo-feature-auth')
	// This helps associate conversations with the repo after workspace deletion. Default: true.
	DirPrefix bool `json:"dirPrefix"`
	// DefaultAgentType sets the default agent family selected when creating a workspace.
	// Uses workspace.AgentType values (e.g. "claude", "codex", "opencode", "grok").
	DefaultAgentType string `json:"defaultAgentType,omitempty"`
	// Agents is an ordered allowlist of agent type IDs shown in Create Shell, Create Worktree,
	// and Start Agent pickers (e.g. ["claude","codex","grok"]). Empty/omitted shows all
	// built-in UI agents. Unknown IDs are ignored. "None (attach only)" is always offered
	// in pickers regardless of this list. Stored agent types on existing workspaces still
	// resolve even if hidden from pickers.
	Agents []string `json:"agents,omitempty"`
	// AgentStart maps agent family (AgentType string) to default startup command.
	// Example: {"claude":"claude", "opencode":"opencode --profile fast", "grok":"grok"}.
	// Per-workspace .sidecar-agent-start still takes precedence when present.
	AgentStart map[string]string `json:"agentStart,omitempty"`
	// TmuxCaptureMaxBytes caps tmux pane capture size for the preview pane. Default: 2MB.
	TmuxCaptureMaxBytes int `json:"tmuxCaptureMaxBytes"`
	// TerminalBackgrounds controls how background colors carried in captured
	// pane output render:
	//
	//   - "auto" (default) keeps canvas detection: when one background covers
	//     nearly every painted row of the live grid, default-background cells
	//     are filled with it so a fullscreen TUI shows no seams.
	//   - "bounded" renders short background spans (diffs, highlights, a
	//     few-line block) but drops the background of any run longer than
	//     TerminalBackgroundSpanMax consecutive rows. An application that
	//     paints most of its output one color degrades to plain text instead of
	//     repainting the whole pane.
	//   - "never" drops every carried background; rows render as plain text.
	//
	// Unknown values fall back to "auto".
	TerminalBackgrounds string `json:"terminalBackgrounds,omitempty"`
	// TerminalBackgroundSpanMax is the row cap for "bounded" mode. Default: 12.
	TerminalBackgroundSpanMax int `json:"terminalBackgroundSpanMax,omitempty"`
	// ResizeDebounceMs is the shared interval for live-pane SIGWINCH during
	// layout motion (divider drag, interactive correction). Default: 300.
	// 0 restores per-event paint and poll-driven resize. Negative values
	// become 300. Unlike TmuxCaptureMaxBytes, 0 is not treated as unset.
	ResizeDebounceMs int `json:"resizeDebounceMs"`
	// AutoCreateShell creates a shell session the first time the workspaces tab is
	// focused in a session, when no shell sessions exist yet. The shell honors
	// DefaultAgentType; with none set it is a plain shell. Default: false.
	AutoCreateShell bool `json:"autoCreateShell"`
	// InteractiveExitKey is the keybinding to exit interactive mode. Default: "ctrl+\".
	// Examples: "ctrl+]", "ctrl+\\", "ctrl+x"
	InteractiveExitKey string `json:"interactiveExitKey,omitempty"`
	// InteractiveAttachKey is the keybinding to attach from interactive mode. Default: "ctrl+]".
	// When pressed in interactive mode, exits interactive and attaches to the tmux session.
	InteractiveAttachKey string `json:"interactiveAttachKey,omitempty"`
	// InteractiveCopyKey is the keybinding to copy selection in interactive mode. Default: "alt+c".
	InteractiveCopyKey string `json:"interactiveCopyKey,omitempty"`
	// InteractivePasteKey is the keybinding to paste clipboard in interactive mode. Default: "alt+v".
	InteractivePasteKey string `json:"interactivePasteKey,omitempty"`
	// CopyOnSelect copies terminal selections when a drag completes. Default: false.
	CopyOnSelect bool `json:"copyOnSelect,omitempty"`
	// OverviewWorktreeScope controls whether activating a worktree on the cross-project
	// Overview enters the project root or scopes Sidecar to that worktree. Valid values
	// are "project" (the default) and "worktree".
	OverviewWorktreeScope string `json:"overviewWorktreeScope,omitempty"`
	// SidebarDisplay controls what information is shown in the workspace sidebar entries.
	SidebarDisplay SidebarDisplayConfig `json:"sidebarDisplay"`
	// WorktreeSetup controls repository artifacts Sidecar may copy or execute when
	// creating a worktree. The creation confirmation always names the discovered
	// files and hook and requires an explicit per-operation selection.
	WorktreeSetup WorktreeSetupConfig `json:"worktreeSetup"`
	// SessionRestore controls what happens on the first Sidecar start after the
	// tmux server that was hosting the managed shells has been replaced.
	SessionRestore SessionRestoreConfig `json:"sessionRestore"`
}

// SessionRestoreConfig configures cold restore after a tmux server replacement.
//
// The two settings are independent on purpose. Getting a terminal back in the
// right directory costs nothing and loses nothing, so it is on by default.
// Resuming a conversation starts a provider process that can spend money and
// change a repository, so it is a separate decision that defaults to asking.
type SessionRestoreConfig struct {
	// RecreateShells recreates managed shells that were confirmed live in the
	// previous tmux server, under their own names and existing working
	// directories. It never replays an arbitrary --run command. Default: true.
	RecreateShells bool `json:"recreateShells"`
	// ResumeAgents is off | ask | auto. Default: ask, which paints the restored
	// shells and layout and then presents one grouped summary; nothing paid or
	// agent-mutating happens until the user confirms or runs the CLI. auto is
	// explicit standing authorization, and applies only to exact session
	// references an official integration reported.
	ResumeAgents string `json:"resumeAgents,omitempty"`
}

// Session restore resumeAgents values.
const (
	ResumeAgentsOff  = "off"
	ResumeAgentsAsk  = "ask"
	ResumeAgentsAuto = "auto"
)

// WorktreeSetupConfig configures the optional setup phase after git creates a
// worktree. Paths are relative to the canonical main worktree.
type WorktreeSetupConfig struct {
	CopyEnvFiles bool     `json:"copyEnvFiles"`
	EnvFiles     []string `json:"envFiles,omitempty"`
	RunHook      bool     `json:"runHook"`
	HookPath     string   `json:"hookPath,omitempty"`
	HookRequired bool     `json:"hookRequired"`
}

// SidebarDisplayConfig controls visibility of workspace sidebar entry elements.
type SidebarDisplayConfig struct {
	// HideRepoPrefix strips the repo name prefix from worktree names (e.g., "myrepo-feature" → "feature").
	// Default: false (show full name).
	HideRepoPrefix bool `json:"hideRepoPrefix"`
	// HideAgent hides the agent type label (e.g., "claude") on the second line. Default: false.
	HideAgent bool `json:"hideAgent"`
	// HideTask hides the linked task ID (e.g., "td-abc123") on the second line. Default: false.
	HideTask bool `json:"hideTask"`
	// HideStats hides the +/- line change stats on the second line. Default: false.
	HideStats bool `json:"hideStats"`
}

// NotesPluginConfig configures the notes plugin.
type NotesPluginConfig struct {
	// Enabled is the unified plugins.<id>.enabled switch. It is a pointer
	// because "absent" is a third answer: with no key written, the deprecated
	// notes_plugin feature flag still decides, so a config written before this
	// key existed keeps working. Notes additionally requires the td panel,
	// which owns its persistence.
	Enabled *bool `json:"enabled,omitempty"`
	// DefaultEditor sets the default editor mode when pressing Enter on a note.
	// Values: "builtin" (default) or "pane" ($EDITOR in the Notes pane).
	DefaultEditor string `json:"defaultEditor,omitempty"`
}

const (
	NotesEditorBuiltin = "builtin"
	NotesEditorPane    = "pane"
)

// KeymapConfig holds key binding overrides.
type KeymapConfig struct {
	Overrides map[string]string `json:"overrides"`
}

// UIConfig configures UI appearance.
type UIConfig struct {
	ShowClock        bool        `json:"showClock"`
	Theme            ThemeConfig `json:"theme"`
	NerdFontsEnabled bool        `json:"nerdFontsEnabled"`        // enables Nerd Font glyphs (pill tabs, icons, etc.)
	LastOpenInApp    string      `json:"lastOpenInApp,omitempty"` // global fallback for last app used to open projects
	// TerminalTitle templates the terminal window/tab title. Supported
	// variables: {project} {worktree} {plugin} {dir}. Empty disables retitling.
	TerminalTitle string `json:"terminalTitle"`
}

// ThemeConfig configures the color theme.
type ThemeConfig struct {
	Name      string                 `json:"name"`
	Community string                 `json:"community,omitempty"` // community scheme name (resolved at runtime)
	Overrides map[string]interface{} `json:"overrides,omitempty"` // user customizations on top
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		Projects: ProjectsConfig{
			Mode: "single",
			Root: ".",
		},
		Plugins: PluginsConfig{
			GitStatus: GitStatusPluginConfig{
				Enabled:         true,
				RefreshInterval: time.Second,
			},
			TDMonitor: TDMonitorPluginConfig{
				Enabled:         true,
				RefreshInterval: tdMonitorDefaultRefresh,
				DBPath:          ".todos/issues.db",
			},
			FileBrowser: FileBrowserPluginConfig{
				Enabled: true,
			},
			Conversations: ConversationsPluginConfig{
				Enabled:       true,
				ClaudeDataDir: "~/.claude",
			},
			Notes: NotesPluginConfig{
				DefaultEditor: NotesEditorBuiltin,
			},
			Tasks: TasksPluginConfig{
				Position: TasksPositionAfterWorkspaces,
			},
			Workspace: WorkspacePluginConfig{
				DirPrefix:             true,
				TmuxCaptureMaxBytes:   2 * 1024 * 1024,
				ResizeDebounceMs:      300,
				OverviewWorktreeScope: OverviewWorktreeScopeProject,
				WorktreeSetup: WorktreeSetupConfig{
					CopyEnvFiles: true,
					EnvFiles:     []string{".env", ".env.local", ".env.development", ".env.development.local"},
					RunHook:      true, HookPath: ".worktree-setup.sh", HookRequired: true,
				},
				SessionRestore: SessionRestoreConfig{RecreateShells: true, ResumeAgents: ResumeAgentsAsk},
			},
		},
		Keymap: KeymapConfig{
			Overrides: make(map[string]string),
		},
		UI: UIConfig{
			// Off by default. The header clock has had no renderer for a long
			// time; now that it has one, defaulting it on would put a clock in
			// every existing user's header for a setting they never chose.
			// Appearance is where it gets turned on.
			ShowClock:     false,
			TerminalTitle: "{project}{worktree}",
			// Theme.Name is left empty on purpose: empty means "no recorded
			// choice", which is what lets a fresh install land on
			// styles.FreshInstallTheme while any name written by a user — or
			// by an older Sidecar that wrote "default" — is preserved
			// verbatim. theme.ResolveTheme owns that fallback.
			Theme: ThemeConfig{
				Overrides: make(map[string]interface{}),
			},
		},
		Features: FeaturesConfig{
			Flags: make(map[string]bool),
		},
		Notifications: DefaultNotificationsConfig(),
		Detection:     DetectionConfig{RemoteManifests: RemoteManifestsOff},
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.Plugins.GitStatus.RefreshInterval < 0 {
		c.Plugins.GitStatus.RefreshInterval = time.Second
	}
	if c.Plugins.TDMonitor.RefreshInterval < 0 {
		c.Plugins.TDMonitor.RefreshInterval = tdMonitorDefaultRefresh
	}
	if c.Plugins.Workspace.TmuxCaptureMaxBytes <= 0 {
		c.Plugins.Workspace.TmuxCaptureMaxBytes = 2 * 1024 * 1024
	}
	c.Plugins.Workspace.TerminalBackgrounds = string(tty.NormalizeBackgroundMode(tty.BackgroundMode(c.Plugins.Workspace.TerminalBackgrounds)))
	if c.Plugins.Workspace.TerminalBackgroundSpanMax <= 0 {
		c.Plugins.Workspace.TerminalBackgroundSpanMax = tty.DefaultBackgroundSpanMax
	}
	if c.Plugins.Workspace.ResizeDebounceMs < 0 {
		c.Plugins.Workspace.ResizeDebounceMs = 300
	}
	// An unrecognized resumeAgents value falls back to the safest of the three
	// rather than failing the whole config. "ask" is the right landing place for
	// a typo specifically because it is the option that does not act on its own.
	switch c.Plugins.Workspace.SessionRestore.ResumeAgents {
	case ResumeAgentsOff, ResumeAgentsAsk, ResumeAgentsAuto:
	default:
		c.Plugins.Workspace.SessionRestore.ResumeAgents = ResumeAgentsAsk
	}
	// An unrecognized (or empty) tasks position falls back to the default
	// anchor rather than failing the whole config, which is how the rest of
	// Validate treats out-of-range values.
	switch c.Plugins.Tasks.Position {
	case TasksPositionAfterWorkspaces, TasksPositionAfterNotes:
	default:
		c.Plugins.Tasks.Position = TasksPositionAfterWorkspaces
	}
	switch c.Plugins.Notes.DefaultEditor {
	case NotesEditorBuiltin, NotesEditorPane:
	case "vim", "nvim":
		// These were the documented values on the previously dormant field.
		// They meant an in-pane editor, so preserve that intent when the field
		// becomes active rather than silently switching those users to built-in.
		c.Plugins.Notes.DefaultEditor = NotesEditorPane
	default:
		c.Plugins.Notes.DefaultEditor = NotesEditorBuiltin
	}
	external, err := validatePluginInstances(c.Plugins.External)
	if err != nil {
		return err
	}
	c.Plugins.External = external
	return validateTerminalResources(&c.TerminalResources)
}
