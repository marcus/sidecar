package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	configDir  = ".config/sidecar"
	configFile = "config.json"
)

// testConfigPath overrides the config path for testing.
var testConfigPath string

// overrideConfigPath is the -config flag value. Unlike testConfigPath it is a
// real user-facing lever: it relocates config.json and everything derived from
// its directory (debug.log, state.json), which is what makes -config a complete
// config-axis isolation switch. ConfigPath() ignores XDG_CONFIG_HOME by design,
// so this flag is the only way to move it.
var overrideConfigPath string

// testStateDir overrides the state directory for testing.
var testStateDir string

// SetTestConfigPath sets a custom config path for testing.
// Call ResetTestConfigPath() in test cleanup to restore default behavior.
func SetTestConfigPath(path string) {
	testConfigPath = path
}

// ResetTestConfigPath clears the test config path override.
func ResetTestConfigPath() {
	testConfigPath = ""
}

// SetConfigPath records the explicit config path (the -config flag) so that
// ConfigPath, and everything that derives a directory from it, follow it.
// Passing "" leaves the default in place.
func SetConfigPath(path string) {
	overrideConfigPath = path
}

// SetTestStateDir sets a custom state directory for testing.
func SetTestStateDir(dir string) {
	testStateDir = dir
}

// ResetTestStateDir clears the test state directory override.
func ResetTestStateDir() {
	testStateDir = ""
}

// rawConfig is the JSON-unmarshaling intermediary.
type rawConfig struct {
	Projects rawProjectsConfig `json:"projects"`
	Plugins  rawPluginsConfig  `json:"plugins"`
	Keymap   KeymapConfig      `json:"keymap"`
	UI       rawUIConfig       `json:"ui"`
	Features FeaturesConfig    `json:"features"`
	// TerminalResources is a pointer so an absent section is distinguishable
	// from an explicitly empty one; both leave the default (no providers) in
	// place, but only the first is silent about it.
	TerminalResources *rawTerminalResourcesConfig `json:"terminalResources"`
	Selection         rawSelectionConfig          `json:"selection"`
	// Notifications is a pointer for the same reason: an absent section leaves
	// internal/notify's registry defaults alone.
	Notifications *rawNotificationsConfig `json:"notifications"`
	// Shells is a pointer for the same reason: an absent section leaves the
	// shell-record defaults (config.DefaultTombstoneRetention) alone.
	Shells *rawShellsConfig `json:"shells"`
	// Hosts is a pointer for the same reason: an absent section means no
	// remote machines, which is different from an explicitly empty list only
	// in what it says, not in what it does.
	//
	// This section exists here as well as on Config because the loader merges
	// a raw document into defaults field by field. A key that is only on
	// Config parses into nothing and is silently ignored — which is exactly
	// what happened to `hosts` the first time, and the symptom was a
	// correctly-written config that produced no hosts and no error.
	Hosts *HostsConfig `json:"hosts"`
	// Detection is a pointer for the same reason: an absent section leaves the
	// default (remoteManifests off) alone, and an explicitly empty one says so.
	Detection *rawDetectionConfig `json:"detection"`
}

type rawDetectionConfig struct {
	RemoteManifests string `json:"remoteManifests"`
}

type rawShellsConfig struct {
	TombstoneRetention string `json:"tombstoneRetention"`
}

type rawNotificationsConfig struct {
	Native     NativeNotificationsConfig              `json:"native"`
	Sound      SoundNotificationsConfig               `json:"sound"`
	QuietHours *QuietHoursConfig                      `json:"quietHours"`
	SSH        *rawSSHNotificationsConfig             `json:"ssh"`
	Sources    map[string]rawNotificationSourceConfig `json:"sources"`
}

// rawSSHNotificationsConfig keeps ManagedHosts a pointer so an absent key
// leaves the default in force rather than reading as an explicit false.
type rawSSHNotificationsConfig struct {
	ManagedHosts *bool            `json:"managedHosts"`
	Terminal     TerminalNotifier `json:"terminal"`
}

type rawNotificationSourceConfig struct {
	Toast  *bool    `json:"toast"`
	Native *bool    `json:"native"`
	Sound  SoundCue `json:"sound"`
	Expiry string   `json:"expiry"`
}

type rawSelectionConfig struct {
	CopyOnSelect *bool `json:"copyOnSelect"`
}

type rawTerminalResourcesConfig struct {
	Providers []rawTerminalResourceProviderConfig `json:"providers"`
}

type rawTerminalResourceProviderConfig struct {
	ID      string   `json:"id"`
	Command []string `json:"command"`
	PassEnv []string `json:"passEnv"`
	// Enabled is a pointer because a configured instance is on unless it says
	// otherwise; an omitted field must not read as "disabled".
	Enabled *bool `json:"enabled"`
	// ClaimHosts follows the file-wide unknown-field convention: json.Unmarshal
	// into this typed struct silently ignores keys it does not know, so a
	// config written by a newer Sidecar loads on an older one with the newer
	// fields inert. Known-field validation stays strict.
	ClaimHosts []string `json:"claimHosts"`
	Timeout    string   `json:"timeout"`
}

type rawUIConfig struct {
	ShowClock        *bool       `json:"showClock"`
	Theme            ThemeConfig `json:"theme"`
	NerdFontsEnabled *bool       `json:"nerdFontsEnabled"`
	LastOpenInApp    string      `json:"lastOpenInApp,omitempty"`
	// Pointer so that an explicit "" reads as "disabled" rather than "unset".
	TerminalTitle *string `json:"terminalTitle"`
}

type rawProjectsConfig struct {
	Mode string             `json:"mode"`
	Root string             `json:"root"`
	List []rawProjectConfig `json:"list"`
}

type rawProjectConfig struct {
	Name          string               `json:"name"`
	Path          string               `json:"path"`
	Theme         *ThemeConfig         `json:"theme,omitempty"`
	LastOpenInApp string               `json:"lastOpenInApp,omitempty"`
	OpenIn        string               `json:"openIn,omitempty"`
	WorktreeSetup *WorktreeSetupConfig `json:"worktreeSetup,omitempty"`
	AddedAt       *time.Time           `json:"addedAt,omitempty"`
}

type rawPluginsConfig struct {
	GitStatus     rawGitStatusConfig     `json:"git-status"`
	TDMonitor     rawTDMonitorConfig     `json:"td-monitor"`
	FileBrowser   rawFileBrowserConfig   `json:"file-browser"`
	Conversations rawConversationsConfig `json:"conversations"`
	Workspace     rawWorkspaceConfig     `json:"workspace"`
	Notes         rawNotesConfig         `json:"notes"`
	Tasks         rawTasksConfig         `json:"tasks"`
	// External is a pointer so an absent section is distinguishable from an
	// empty one: an absent key leaves whatever the defaults hold, an explicit
	// [] is the user emptying the list.
	External *[]rawPluginInstanceConfig `json:"external"`
}

// rawPluginInstanceConfig is one plugins.external entry as written. Enabled is
// a pointer because a configured instance is on unless it says otherwise, and
// Timeout is a string because the file speaks Go durations.
//
// Unknown keys follow the file-wide convention: json.Unmarshal into this typed
// struct silently ignores what it does not know, so a config written by a newer
// Sidecar loads on an older one with the newer fields inert.
type rawPluginInstanceConfig struct {
	ID         string   `json:"id"`
	Command    []string `json:"command"`
	PassEnv    []string `json:"passEnv"`
	Enabled    *bool    `json:"enabled"`
	Scope      string   `json:"scope"`
	Placements []string `json:"placements"`
	Timeout    string   `json:"timeout"`
	ClaimHosts []string `json:"claimHosts"`
}

type rawNotesConfig struct {
	Enabled       *bool  `json:"enabled"`
	DefaultEditor string `json:"defaultEditor"`
}

type rawTasksConfig struct {
	Enabled  *bool  `json:"enabled"`
	Position string `json:"position"`
}

type rawWorkspaceConfig struct {
	DirPrefix                 *bool                    `json:"dirPrefix"`
	DefaultAgentType          string                   `json:"defaultAgentType"`
	LegacyDefaultAgent        string                   `json:"defaultAgent"` // Backward compatibility
	Agents                    []string                 `json:"agents"`
	AgentStart                json.RawMessage          `json:"agentStart"`
	TmuxCaptureMaxBytes       *int                     `json:"tmuxCaptureMaxBytes"`
	TerminalBackgrounds       string                   `json:"terminalBackgrounds"`
	TerminalBackgroundSpanMax *int                     `json:"terminalBackgroundSpanMax"`
	ResizeDebounceMs          *int                     `json:"resizeDebounceMs"`
	AutoCreateShell           *bool                    `json:"autoCreateShell"`
	InteractiveExitKey        string                   `json:"interactiveExitKey"`
	InteractiveAttachKey      string                   `json:"interactiveAttachKey"`
	InteractiveCopyKey        string                   `json:"interactiveCopyKey"`
	InteractivePasteKey       string                   `json:"interactivePasteKey"`
	CopyOnSelect              *bool                    `json:"copyOnSelect"`
	OverviewWorktreeScope     string                   `json:"overviewWorktreeScope"`
	SidebarDisplay            *rawSidebarDisplayConfig `json:"sidebarDisplay"`
	WorktreeSetup             *rawWorktreeSetupConfig  `json:"worktreeSetup"`
	SessionRestore            *rawSessionRestoreConfig `json:"sessionRestore"`
}

// rawSessionRestoreConfig keeps RecreateShells a pointer so that "absent" is
// distinguishable from an explicit false: the default is true, and a
// non-pointer bool would silently turn shell restoration off for everyone whose
// config mentions the object at all.
type rawSessionRestoreConfig struct {
	RecreateShells *bool  `json:"recreateShells"`
	ResumeAgents   string `json:"resumeAgents"`
}

type rawWorktreeSetupConfig struct {
	CopyEnvFiles *bool    `json:"copyEnvFiles"`
	EnvFiles     []string `json:"envFiles"`
	RunHook      *bool    `json:"runHook"`
	HookPath     string   `json:"hookPath"`
	HookRequired *bool    `json:"hookRequired"`
}

type rawSidebarDisplayConfig struct {
	HideRepoPrefix *bool `json:"hideRepoPrefix"`
	HideAgent      *bool `json:"hideAgent"`
	HideTask       *bool `json:"hideTask"`
	HideStats      *bool `json:"hideStats"`
}

type rawGitStatusConfig struct {
	Enabled         *bool  `json:"enabled"`
	RefreshInterval string `json:"refreshInterval"`
}

type rawTDMonitorConfig struct {
	Enabled         *bool  `json:"enabled"`
	RefreshInterval string `json:"refreshInterval"`
	DBPath          string `json:"dbPath"`
}

type rawFileBrowserConfig struct {
	Enabled *bool `json:"enabled"`
}

type rawConversationsConfig struct {
	Enabled       *bool  `json:"enabled"`
	ClaudeDataDir string `json:"claudeDataDir"`
}

const (
	envWorkspaceDefaultAgentType = "SIDECAR_WORKSPACE_DEFAULT_AGENT_TYPE"
	envDefaultAgentType          = "SIDECAR_DEFAULT_AGENT_TYPE"
)

// Load loads configuration from the path Sidecar is actually using — the
// -config flag's file, a test's fixture, or the default location. Every
// read-modify-write helper below goes through it, so a save can never merge
// against a different file than the one it is about to write.
func Load() (*Config, error) {
	return LoadFrom(ConfigPath())
}

// LoadFrom loads configuration from a specific path.
// If path is empty, uses ~/.config/sidecar/config.json
func LoadFrom(path string) (*Config, error) {
	cfg := Default()

	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			applyEnvOverrides(cfg)
			return cfg, nil // Return defaults on error
		}
		path = filepath.Join(home, configDir, configFile)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(cfg)
			if err := cfg.Validate(); err != nil {
				return nil, err
			}
			return cfg, nil // Return defaults if no config file
		}
		return nil, err
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	// Merge raw config into defaults
	mergeConfig(cfg, &raw)
	applyEnvOverrides(cfg)

	// Expand paths
	cfg.Plugins.Conversations.ClaudeDataDir = ExpandPath(cfg.Plugins.Conversations.ClaudeDataDir)

	// Expand paths in project list and warn if path doesn't exist
	for i := range cfg.Projects.List {
		cfg.Projects.List[i].Path = ExpandPath(cfg.Projects.List[i].Path)
		if _, err := os.Stat(cfg.Projects.List[i].Path); os.IsNotExist(err) {
			slog.Warn("project path not found", "name", cfg.Projects.List[i].Name, "path", cfg.Projects.List[i].Path)
		}
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// mergeConfig merges raw config values into the config.
func mergeConfig(cfg *Config, raw *rawConfig) {
	// Projects
	if raw.Projects.Mode != "" {
		cfg.Projects.Mode = raw.Projects.Mode
	}
	if raw.Projects.Root != "" {
		cfg.Projects.Root = raw.Projects.Root
	}
	if len(raw.Projects.List) > 0 {
		cfg.Projects.List = make([]ProjectConfig, len(raw.Projects.List))
		for i, rp := range raw.Projects.List {
			cfg.Projects.List[i] = ProjectConfig(rp)
		}
	}

	// Git Status
	if raw.Plugins.GitStatus.Enabled != nil {
		cfg.Plugins.GitStatus.Enabled = *raw.Plugins.GitStatus.Enabled
	}
	if raw.Plugins.GitStatus.RefreshInterval != "" {
		if d, err := time.ParseDuration(raw.Plugins.GitStatus.RefreshInterval); err == nil {
			cfg.Plugins.GitStatus.RefreshInterval = d
		}
	}

	// TD Monitor
	if raw.Plugins.TDMonitor.Enabled != nil {
		cfg.Plugins.TDMonitor.Enabled = *raw.Plugins.TDMonitor.Enabled
	}
	if raw.Plugins.TDMonitor.RefreshInterval != "" {
		if d, err := time.ParseDuration(raw.Plugins.TDMonitor.RefreshInterval); err == nil {
			cfg.Plugins.TDMonitor.RefreshInterval = d
		}
	}
	if raw.Plugins.TDMonitor.DBPath != "" {
		cfg.Plugins.TDMonitor.DBPath = raw.Plugins.TDMonitor.DBPath
	}

	// File Browser
	if raw.Plugins.FileBrowser.Enabled != nil {
		cfg.Plugins.FileBrowser.Enabled = *raw.Plugins.FileBrowser.Enabled
	}

	// Conversations
	if raw.Plugins.Conversations.Enabled != nil {
		cfg.Plugins.Conversations.Enabled = *raw.Plugins.Conversations.Enabled
	}
	if raw.Plugins.Conversations.ClaudeDataDir != "" {
		cfg.Plugins.Conversations.ClaudeDataDir = raw.Plugins.Conversations.ClaudeDataDir
	}

	// Tasks
	if raw.Plugins.Tasks.Enabled != nil {
		enabled := *raw.Plugins.Tasks.Enabled
		cfg.Plugins.Tasks.Enabled = &enabled
	}
	if raw.Plugins.Tasks.Position != "" {
		cfg.Plugins.Tasks.Position = raw.Plugins.Tasks.Position
	}

	// Notes
	if raw.Plugins.Notes.Enabled != nil {
		enabled := *raw.Plugins.Notes.Enabled
		cfg.Plugins.Notes.Enabled = &enabled
	}
	if raw.Plugins.Notes.DefaultEditor != "" {
		cfg.Plugins.Notes.DefaultEditor = raw.Plugins.Notes.DefaultEditor
	}

	// Workspace
	if raw.Plugins.Workspace.DirPrefix != nil {
		cfg.Plugins.Workspace.DirPrefix = *raw.Plugins.Workspace.DirPrefix
	}
	if setup := raw.Plugins.Workspace.WorktreeSetup; setup != nil {
		if setup.CopyEnvFiles != nil {
			cfg.Plugins.Workspace.WorktreeSetup.CopyEnvFiles = *setup.CopyEnvFiles
		}
		if setup.EnvFiles != nil {
			cfg.Plugins.Workspace.WorktreeSetup.EnvFiles = append([]string(nil), setup.EnvFiles...)
		}
		if setup.RunHook != nil {
			cfg.Plugins.Workspace.WorktreeSetup.RunHook = *setup.RunHook
		}
		if setup.HookPath != "" {
			cfg.Plugins.Workspace.WorktreeSetup.HookPath = setup.HookPath
		}
		if setup.HookRequired != nil {
			cfg.Plugins.Workspace.WorktreeSetup.HookRequired = *setup.HookRequired
		}
	}
	if restore := raw.Plugins.Workspace.SessionRestore; restore != nil {
		if restore.RecreateShells != nil {
			cfg.Plugins.Workspace.SessionRestore.RecreateShells = *restore.RecreateShells
		}
		if restore.ResumeAgents != "" {
			cfg.Plugins.Workspace.SessionRestore.ResumeAgents = restore.ResumeAgents
		}
	}
	if raw.Plugins.Workspace.TmuxCaptureMaxBytes != nil {
		cfg.Plugins.Workspace.TmuxCaptureMaxBytes = *raw.Plugins.Workspace.TmuxCaptureMaxBytes
	}
	if raw.Plugins.Workspace.TerminalBackgrounds != "" {
		cfg.Plugins.Workspace.TerminalBackgrounds = raw.Plugins.Workspace.TerminalBackgrounds
	}
	if raw.Plugins.Workspace.TerminalBackgroundSpanMax != nil {
		cfg.Plugins.Workspace.TerminalBackgroundSpanMax = *raw.Plugins.Workspace.TerminalBackgroundSpanMax
	}
	if raw.Plugins.Workspace.ResizeDebounceMs != nil {
		cfg.Plugins.Workspace.ResizeDebounceMs = *raw.Plugins.Workspace.ResizeDebounceMs
	}
	if raw.Plugins.Workspace.AutoCreateShell != nil {
		cfg.Plugins.Workspace.AutoCreateShell = *raw.Plugins.Workspace.AutoCreateShell
	}
	if raw.Plugins.Workspace.DefaultAgentType != "" {
		cfg.Plugins.Workspace.DefaultAgentType = raw.Plugins.Workspace.DefaultAgentType
	}
	if cfg.Plugins.Workspace.DefaultAgentType == "" && raw.Plugins.Workspace.LegacyDefaultAgent != "" {
		cfg.Plugins.Workspace.DefaultAgentType = raw.Plugins.Workspace.LegacyDefaultAgent
	}
	if len(raw.Plugins.Workspace.Agents) > 0 {
		cfg.Plugins.Workspace.Agents = append([]string(nil), raw.Plugins.Workspace.Agents...)
	}
	if agentStart, ok := parseAgentStartOverrides(raw.Plugins.Workspace.AgentStart); ok {
		cfg.Plugins.Workspace.AgentStart = agentStart
	}
	if raw.Plugins.Workspace.InteractiveExitKey != "" {
		cfg.Plugins.Workspace.InteractiveExitKey = raw.Plugins.Workspace.InteractiveExitKey
	}
	if raw.Plugins.Workspace.InteractiveAttachKey != "" {
		cfg.Plugins.Workspace.InteractiveAttachKey = raw.Plugins.Workspace.InteractiveAttachKey
	}
	if raw.Plugins.Workspace.InteractiveCopyKey != "" {
		cfg.Plugins.Workspace.InteractiveCopyKey = raw.Plugins.Workspace.InteractiveCopyKey
	}
	if raw.Plugins.Workspace.InteractivePasteKey != "" {
		cfg.Plugins.Workspace.InteractivePasteKey = raw.Plugins.Workspace.InteractivePasteKey
	}
	if raw.Plugins.Workspace.CopyOnSelect != nil {
		cfg.Plugins.Workspace.CopyOnSelect = *raw.Plugins.Workspace.CopyOnSelect
	}
	if scope := raw.Plugins.Workspace.OverviewWorktreeScope; scope == OverviewWorktreeScopeProject || scope == OverviewWorktreeScopeWorktree {
		cfg.Plugins.Workspace.OverviewWorktreeScope = scope
	}
	if sd := raw.Plugins.Workspace.SidebarDisplay; sd != nil {
		if sd.HideRepoPrefix != nil {
			cfg.Plugins.Workspace.SidebarDisplay.HideRepoPrefix = *sd.HideRepoPrefix
		}
		if sd.HideAgent != nil {
			cfg.Plugins.Workspace.SidebarDisplay.HideAgent = *sd.HideAgent
		}
		if sd.HideTask != nil {
			cfg.Plugins.Workspace.SidebarDisplay.HideTask = *sd.HideTask
		}
		if sd.HideStats != nil {
			cfg.Plugins.Workspace.SidebarDisplay.HideStats = *sd.HideStats
		}
	}

	// Keymap
	if raw.Keymap.Overrides != nil {
		for k, v := range raw.Keymap.Overrides {
			cfg.Keymap.Overrides[k] = v
		}
	}

	// UI
	if raw.UI.ShowClock != nil {
		cfg.UI.ShowClock = *raw.UI.ShowClock
	}
	if raw.UI.NerdFontsEnabled != nil {
		cfg.UI.NerdFontsEnabled = *raw.UI.NerdFontsEnabled
	}
	if raw.UI.LastOpenInApp != "" {
		cfg.UI.LastOpenInApp = raw.UI.LastOpenInApp
	}
	if raw.UI.TerminalTitle != nil {
		cfg.UI.TerminalTitle = *raw.UI.TerminalTitle
	}
	if raw.UI.Theme.Name != "" {
		cfg.UI.Theme.Name = raw.UI.Theme.Name
	}
	if raw.UI.Theme.Community != "" {
		cfg.UI.Theme.Community = raw.UI.Theme.Community
	}
	if raw.UI.Theme.Overrides != nil {
		for k, v := range raw.UI.Theme.Overrides {
			cfg.UI.Theme.Overrides[k] = v
		}
	}
	// Migrate legacy communityName from overrides to Community field
	if cfg.UI.Theme.Community == "" && cfg.UI.Theme.Overrides != nil {
		if name, ok := cfg.UI.Theme.Overrides["communityName"]; ok {
			if s, ok := name.(string); ok && s != "" {
				cfg.UI.Theme.Community = s
				// Clear overrides — colors will be re-derived from community scheme at runtime
				cfg.UI.Theme.Overrides = nil
			}
		}
	}

	// External plugins
	if raw.Plugins.External != nil {
		external := make([]PluginInstanceConfig, 0, len(*raw.Plugins.External))
		for _, rp := range *raw.Plugins.External {
			p := PluginInstanceConfig{
				ID:         rp.ID,
				Command:    append([]string(nil), rp.Command...),
				PassEnv:    append([]string(nil), rp.PassEnv...),
				Enabled:    true,
				Scope:      rp.Scope,
				Placements: append([]string(nil), rp.Placements...),
				ClaimHosts: append([]string(nil), rp.ClaimHosts...),
			}
			if rp.Enabled != nil {
				p.Enabled = *rp.Enabled
			}
			if rp.Timeout != "" {
				if d, err := time.ParseDuration(rp.Timeout); err == nil {
					p.Timeout = d
				}
			}
			external = append(external, p)
		}
		cfg.Plugins.External = external
	}

	// Terminal resources
	if raw.TerminalResources != nil {
		providers := make([]TerminalResourceProviderConfig, 0, len(raw.TerminalResources.Providers))
		for _, rp := range raw.TerminalResources.Providers {
			p := TerminalResourceProviderConfig{
				ID:         rp.ID,
				Command:    append([]string(nil), rp.Command...),
				PassEnv:    append([]string(nil), rp.PassEnv...),
				ClaimHosts: append([]string(nil), rp.ClaimHosts...),
				Enabled:    true,
			}
			if rp.Enabled != nil {
				p.Enabled = *rp.Enabled
			}
			if rp.Timeout != "" {
				if d, err := time.ParseDuration(rp.Timeout); err == nil {
					p.Timeout = d
				}
			}
			providers = append(providers, p)
		}
		cfg.TerminalResources.Providers = providers
	}

	// Selection
	if raw.Selection.CopyOnSelect != nil {
		cfg.Selection.CopyOnSelect = *raw.Selection.CopyOnSelect
	}
	// Copy-on-select was the embedded terminal's setting before it was every
	// surface's. The old key is folded into the general one and cleared, so
	// there is one answer to "does finishing a selection copy it" and the next
	// save retires the key that used to hold it.
	if cfg.Plugins.Workspace.CopyOnSelect {
		cfg.Selection.CopyOnSelect = true
		cfg.Plugins.Workspace.CopyOnSelect = false
	}

	// Notifications
	if raw.Notifications != nil {
		if raw.Notifications.Native.Mode != "" {
			cfg.Notifications.Native.Mode = raw.Notifications.Native.Mode
		}
		if raw.Notifications.Native.Provider != "" {
			cfg.Notifications.Native.Provider = raw.Notifications.Native.Provider
		}
		if raw.Notifications.Sound.Mode != "" {
			cfg.Notifications.Sound.Mode = raw.Notifications.Sound.Mode
		}
		cfg.Notifications.Sound.AttentionPath = raw.Notifications.Sound.AttentionPath
		cfg.Notifications.Sound.DonePath = raw.Notifications.Sound.DonePath
		cfg.Notifications.Sound.FailurePath = raw.Notifications.Sound.FailurePath
		if raw.Notifications.QuietHours != nil {
			cfg.Notifications.QuietHours = *raw.Notifications.QuietHours
		}
		if raw.Notifications.SSH != nil {
			if raw.Notifications.SSH.ManagedHosts != nil {
				cfg.Notifications.SSH.ManagedHosts = *raw.Notifications.SSH.ManagedHosts
			}
			if raw.Notifications.SSH.Terminal != "" {
				cfg.Notifications.SSH.Terminal = raw.Notifications.SSH.Terminal
			}
		}
	}
	if raw.Notifications != nil && len(raw.Notifications.Sources) > 0 {
		if cfg.Notifications.Sources == nil {
			cfg.Notifications.Sources = make(map[string]NotificationSourceConfig, len(raw.Notifications.Sources))
		}
		for id, src := range raw.Notifications.Sources {
			cfg.Notifications.Sources[id] = NotificationSourceConfig(src)
		}
	}

	// Shells
	if raw.Hosts != nil {
		cfg.Hosts = *raw.Hosts
	}
	if raw.Shells != nil && strings.TrimSpace(raw.Shells.TombstoneRetention) != "" {
		cfg.Shells.TombstoneRetention = strings.TrimSpace(raw.Shells.TombstoneRetention)
	}

	// Detection
	//
	// An unrecognised value is never read as "on": RemoteCatalogURL refuses it,
	// so RemoteManifestsEnabled is false and nothing fetches. The value is kept
	// verbatim rather than replaced with the default, which is what lets
	// `sidecar agent manifests` show the user the typo they wrote and the
	// reason it was refused. Replacing it here made that impossible -- the verb
	// could only ever see "off", so its "setting refused" line was unreachable
	// and the only trace of the typo was this log line, which is not somewhere
	// anyone looks.
	if raw.Detection != nil {
		if value := strings.TrimSpace(raw.Detection.RemoteManifests); value != "" {
			if _, err := (DetectionConfig{RemoteManifests: value}).RemoteCatalogURL(); err != nil {
				slog.Warn("config: detection.remoteManifests not understood, runtime manifest fetch stays off",
					"value", value, "error", err)
			}
			cfg.Detection.RemoteManifests = value
		}
	}

	// Features
	if raw.Features.Flags != nil {
		for k, v := range raw.Features.Flags {
			cfg.Features.Flags[k] = v
		}
	}
}

func applyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}

	// SIDECAR_WORKSPACE_DEFAULT_AGENT_TYPE takes precedence over SIDECAR_DEFAULT_AGENT_TYPE,
	// but only when it is set to a non-blank value. A blank value means "unset" so we fall
	// through to the lower-priority env var rather than silently dropping it.
	if v, ok := os.LookupEnv(envWorkspaceDefaultAgentType); ok && strings.TrimSpace(v) != "" {
		cfg.Plugins.Workspace.DefaultAgentType = strings.TrimSpace(v)
		return
	}
	if v, ok := os.LookupEnv(envDefaultAgentType); ok {
		cfg.Plugins.Workspace.DefaultAgentType = strings.TrimSpace(v)
	}
}

func parseAgentStartOverrides(raw json.RawMessage) (map[string]string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}

	var byType map[string]string
	if err := json.Unmarshal(raw, &byType); err == nil {
		out := make(map[string]string, len(byType))
		for k, v := range byType {
			key := strings.TrimSpace(k)
			val := strings.TrimSpace(v)
			if key == "" || val == "" {
				continue
			}
			out[key] = val
		}
		return out, true
	}

	// Backward compatibility: previous schema accepted a single string.
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		single = strings.TrimSpace(single)
		if single == "" {
			return map[string]string{}, true
		}
		return map[string]string{"*": single}, true
	}

	return nil, false
}

// ExpandPath expands ~ to home directory.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// ConfigPath returns the path to the config file.
func ConfigPath() string {
	if testConfigPath != "" {
		return testConfigPath
	}
	if overrideConfigPath != "" {
		return overrideConfigPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDir, configFile)
}

// AgentCatalogDir returns the directory holding the user's agent catalog
// overlay: one TOML file per family, beside the config file.
//
// It is derived from ConfigPath rather than from $HOME so that -config moves it
// with everything else, the way state.json and debug.log already move
// (td-8d18de). agentcatalog is a leaf package and cannot compute this itself,
// so this is the one place it is computed and agentcatalog.LoadOverlay takes it
// as an argument.
func AgentCatalogDir() string {
	path := ConfigPath()
	if path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "agents")
}

// StateDir returns the directory for sidecar state files.
// Follows XDG Base Directory Specification: $XDG_STATE_HOME/sidecar
// (defaults to ~/.local/state/sidecar).
func StateDir() string {
	if testStateDir != "" {
		return testStateDir
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "sidecar")
}
