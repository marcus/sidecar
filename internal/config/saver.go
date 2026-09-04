package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// saveConfig is the JSON-marshaling intermediary that uses string durations.
//
// terminalResources is deliberately absent. It is a read-only alias: Sidecar
// still reads it and still dispatches its entries on the frozen
// sidecar.terminal-resource/v1 identifier, but nothing writes it, so a save
// carries the section forward exactly as the user wrote it (see Save).
type saveConfig struct {
	Projects      saveProjectsConfig  `json:"projects"`
	Plugins       savePluginsConfig   `json:"plugins"`
	Keymap        KeymapConfig        `json:"keymap"`
	UI            UIConfig            `json:"ui"`
	Features      FeaturesConfig      `json:"features,omitempty"`
	Selection     saveSelectionConfig `json:"selection"`
	Notifications NotificationsConfig `json:"notifications"`
	// Hosts is written only when a machine is registered; see Save.
	Hosts HostsConfig `json:"hosts,omitempty"`
}

type saveSelectionConfig struct {
	CopyOnSelect *bool `json:"copyOnSelect,omitempty"`
}

type saveProjectsConfig struct {
	Mode string          `json:"mode,omitempty"`
	Root string          `json:"root,omitempty"`
	List []ProjectConfig `json:"list,omitempty"`
}

type savePluginsConfig struct {
	GitStatus     saveGitStatusConfig     `json:"git-status,omitempty"`
	TDMonitor     saveTDMonitorConfig     `json:"td-monitor,omitempty"`
	FileBrowser   saveFileBrowserConfig   `json:"file-browser,omitempty"`
	Conversations saveConversationsConfig `json:"conversations,omitempty"`
	Workspace     saveWorkspaceConfig     `json:"workspace,omitempty"`
	Notes         saveNotesConfig         `json:"notes,omitempty"`
	Tasks         saveTasksConfig         `json:"tasks,omitempty"`
	// External is written whenever an instance is configured and omitted when
	// the list is empty. It needs no companion delete in Save:
	// mergePluginsSection clears every key named here before writing the fresh
	// ones, so an emptied list disappears rather than being carried forward.
	External []savePluginInstanceConfig `json:"external,omitempty"`
}

type savePluginInstanceConfig struct {
	ID         string   `json:"id"`
	Command    []string `json:"command"`
	PassEnv    []string `json:"passEnv,omitempty"`
	Enabled    bool     `json:"enabled"`
	Scope      string   `json:"scope,omitempty"`
	Placements []string `json:"placements,omitempty"`
	Timeout    string   `json:"timeout,omitempty"`
	ClaimHosts []string `json:"claimHosts,omitempty"`
}

type saveNotesConfig struct {
	Enabled       *bool  `json:"enabled,omitempty"`
	DefaultEditor string `json:"defaultEditor,omitempty"`
}

type saveTasksConfig struct {
	Enabled  *bool  `json:"enabled,omitempty"`
	Position string `json:"position,omitempty"`
}

type saveFileBrowserConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type saveGitStatusConfig struct {
	Enabled         *bool  `json:"enabled,omitempty"`
	RefreshInterval string `json:"refreshInterval,omitempty"`
}

type saveTDMonitorConfig struct {
	Enabled         *bool  `json:"enabled,omitempty"`
	RefreshInterval string `json:"refreshInterval,omitempty"`
	DBPath          string `json:"dbPath,omitempty"`
}

type saveConversationsConfig struct {
	Enabled       *bool  `json:"enabled,omitempty"`
	ClaudeDataDir string `json:"claudeDataDir,omitempty"`
}

type saveWorkspaceConfig struct {
	DirPrefix                 *bool                 `json:"dirPrefix,omitempty"`
	DefaultAgentType          string                `json:"defaultAgentType,omitempty"`
	Agents                    []string              `json:"agents,omitempty"`
	AgentStart                map[string]string     `json:"agentStart,omitempty"`
	TmuxCaptureMaxBytes       *int                  `json:"tmuxCaptureMaxBytes,omitempty"`
	TerminalBackgrounds       string                `json:"terminalBackgrounds,omitempty"`
	TerminalBackgroundSpanMax *int                  `json:"terminalBackgroundSpanMax,omitempty"`
	ResizeDebounceMs          *int                  `json:"resizeDebounceMs,omitempty"`
	AutoCreateShell           *bool                 `json:"autoCreateShell,omitempty"`
	InteractiveExitKey        string                `json:"interactiveExitKey,omitempty"`
	InteractiveAttachKey      string                `json:"interactiveAttachKey,omitempty"`
	InteractiveCopyKey        string                `json:"interactiveCopyKey,omitempty"`
	InteractivePasteKey       string                `json:"interactivePasteKey,omitempty"`
	CopyOnSelect              *bool                 `json:"copyOnSelect,omitempty"`
	OverviewWorktreeScope     string                `json:"overviewWorktreeScope,omitempty"`
	SidebarDisplay            *SidebarDisplayConfig `json:"sidebarDisplay,omitempty"`
	WorktreeSetup             WorktreeSetupConfig   `json:"worktreeSetup"`
	SessionRestore            SessionRestoreConfig  `json:"sessionRestore"`
}

// toSaveConfig converts Config to the JSON-serializable format.
func toSaveConfig(cfg *Config) saveConfig {
	return saveConfig{
		Projects: saveProjectsConfig{
			Mode: cfg.Projects.Mode,
			Root: cfg.Projects.Root,
			List: cfg.Projects.List,
		},
		Plugins: savePluginsConfig{
			GitStatus: saveGitStatusConfig{
				Enabled:         &cfg.Plugins.GitStatus.Enabled,
				RefreshInterval: cfg.Plugins.GitStatus.RefreshInterval.String(),
			},
			TDMonitor: saveTDMonitorConfig{
				Enabled:         &cfg.Plugins.TDMonitor.Enabled,
				RefreshInterval: cfg.Plugins.TDMonitor.RefreshInterval.String(),
				DBPath:          cfg.Plugins.TDMonitor.DBPath,
			},
			FileBrowser: saveFileBrowserConfig{
				Enabled: &cfg.Plugins.FileBrowser.Enabled,
			},
			Conversations: saveConversationsConfig{
				Enabled:       &cfg.Plugins.Conversations.Enabled,
				ClaudeDataDir: cfg.Plugins.Conversations.ClaudeDataDir,
			},
			Tasks: saveTasksConfig{
				Enabled:  cfg.Plugins.Tasks.Enabled,
				Position: cfg.Plugins.Tasks.Position,
			},
			Notes: saveNotesConfig{
				Enabled:       cfg.Plugins.Notes.Enabled,
				DefaultEditor: cfg.Plugins.Notes.DefaultEditor,
			},
			External: toSavePluginInstances(cfg.Plugins.External),
			Workspace: saveWorkspaceConfig{
				DirPrefix:                 &cfg.Plugins.Workspace.DirPrefix,
				DefaultAgentType:          cfg.Plugins.Workspace.DefaultAgentType,
				Agents:                    cfg.Plugins.Workspace.Agents,
				AgentStart:                cfg.Plugins.Workspace.AgentStart,
				TmuxCaptureMaxBytes:       &cfg.Plugins.Workspace.TmuxCaptureMaxBytes,
				TerminalBackgrounds:       cfg.Plugins.Workspace.TerminalBackgrounds,
				TerminalBackgroundSpanMax: &cfg.Plugins.Workspace.TerminalBackgroundSpanMax,
				ResizeDebounceMs:          &cfg.Plugins.Workspace.ResizeDebounceMs,
				AutoCreateShell:           &cfg.Plugins.Workspace.AutoCreateShell,
				InteractiveExitKey:        cfg.Plugins.Workspace.InteractiveExitKey,
				InteractiveAttachKey:      cfg.Plugins.Workspace.InteractiveAttachKey,
				InteractiveCopyKey:        cfg.Plugins.Workspace.InteractiveCopyKey,
				InteractivePasteKey:       cfg.Plugins.Workspace.InteractivePasteKey,
				CopyOnSelect:              &cfg.Plugins.Workspace.CopyOnSelect,
				OverviewWorktreeScope:     cfg.Plugins.Workspace.OverviewWorktreeScope,
				WorktreeSetup:             cfg.Plugins.Workspace.WorktreeSetup,
				SessionRestore:            cfg.Plugins.Workspace.SessionRestore,
				SidebarDisplay:            &cfg.Plugins.Workspace.SidebarDisplay,
			},
		},
		Keymap:        cfg.Keymap,
		UI:            cfg.UI,
		Features:      cfg.Features,
		Selection:     saveSelectionConfig{CopyOnSelect: &cfg.Selection.CopyOnSelect},
		Notifications: cfg.Notifications,
		Hosts:         cfg.Hosts,
	}
}

func toSavePluginInstances(entries []PluginInstanceConfig) []savePluginInstanceConfig {
	if len(entries) == 0 {
		return nil
	}
	out := make([]savePluginInstanceConfig, 0, len(entries))
	for _, p := range entries {
		sp := savePluginInstanceConfig{
			ID:         p.ID,
			Command:    append([]string(nil), p.Command...),
			PassEnv:    append([]string(nil), p.PassEnv...),
			Enabled:    p.Enabled,
			Scope:      p.Scope,
			Placements: append([]string(nil), p.Placements...),
			ClaimHosts: append([]string(nil), p.ClaimHosts...),
		}
		if p.Timeout > 0 {
			sp.Timeout = p.Timeout.String()
		}
		out = append(out, sp)
	}
	return out
}

// managedPluginKeys are the subkeys of "plugins" that savePluginsConfig owns.
//
// It is derived from the struct's own tags rather than written out, so adding a
// section to savePluginsConfig cannot leave a stale key behind in
// mergePluginsSection.
func managedPluginKeys() []string {
	t := reflect.TypeOf(savePluginsConfig{})
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		keys = append(keys, name)
	}
	return keys
}

// mergePluginsSection folds the sections Sidecar manages into whatever the file
// already had under "plugins", so a subsection it does not know about — a newer
// release's key, a hand-written one — survives a save the way an unknown
// top-level key does. Save used to rewrite the whole object, which dropped it.
//
// Managed keys are deleted before the fresh ones are written, so a section that
// marshals away (an emptied plugins.external list) disappears rather than being
// resurrected from the old file.
//
// The result is always built from a map, whatever the file held, so the key
// order Save writes does not depend on whether a plugins section was there to
// merge into. Two consecutive saves must produce the same bytes.
func mergePluginsSection(existing json.RawMessage, managed savePluginsConfig) (json.RawMessage, error) {
	encoded, err := json.Marshal(managed)
	if err != nil {
		return nil, err
	}
	merged := make(map[string]json.RawMessage)
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &merged); err != nil {
			slog.Warn("config: plugins section is not an object, unmanaged subkeys will be lost", "error", err)
			merged = make(map[string]json.RawMessage)
		}
	}
	var fresh map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fresh); err != nil {
		return nil, err
	}
	for _, key := range managedPluginKeys() {
		delete(merged, key)
	}
	for key, val := range fresh {
		merged[key] = val
	}
	return json.Marshal(merged)
}

// Save writes the config to ~/.config/sidecar/config.json, preserving
// any keys it doesn't manage (e.g. "prompts").
func Save(cfg *Config) error {
	path := ConfigPath()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Read existing file to preserve unknown keys
	var raw map[string]json.RawMessage
	if existing, err := os.ReadFile(path); err == nil {
		if jsonErr := json.Unmarshal(existing, &raw); jsonErr != nil {
			slog.Warn("config: invalid JSON, unmanaged keys will be lost", "error", jsonErr)
			raw = make(map[string]json.RawMessage)
		}
	} else {
		raw = make(map[string]json.RawMessage)
	}

	// Marshal each known field into the map
	sc := toSaveConfig(cfg)
	fields := map[string]interface{}{
		"projects":  sc.Projects,
		"keymap":    sc.Keymap,
		"ui":        sc.UI,
		"selection": sc.Selection,
	}
	// plugins is merged rather than replaced: the sections listed in
	// savePluginsConfig are rewritten and everything else under the key is
	// carried forward untouched.
	plugins, err := mergePluginsSection(raw["plugins"], sc.Plugins)
	if err != nil {
		return fmt.Errorf("marshal plugins: %w", err)
	}
	raw["plugins"] = plugins
	if len(sc.Features.Flags) > 0 {
		fields["features"] = sc.Features
	}
	// terminalResources is deliberately not in fields and deliberately not
	// deleted. It is a read-only alias of the plugin protocol's own section:
	// Sidecar reads it and dispatches its entries on the frozen resource
	// identifier, and nothing writes it, so Save carries it forward exactly as
	// the user wrote it. Emptying cfg.TerminalResources in memory therefore
	// removes nothing from the file — the way to remove a provider is to edit
	// the section, and the release that retires the alias rewrites the entries
	// into plugins.external.
	//
	// notifications is now a managed key. A targeted source save must preserve
	// the validated global channel and quiet-hours policy it was based on.
	fields["notifications"] = sc.Notifications
	// hosts is managed on exactly the terms terminalResources is: written when
	// a machine is registered, and the key removed when the last one goes.
	//
	// Until the registry became editable, `hosts` was only ever hand-written and
	// Save carried it forward as an unknown key. That was silently wrong the
	// moment anything wrote it: an entry added to cfg.Hosts was dropped on the
	// way out, and — worse in the other direction — unregistering the last host
	// would have resurrected the whole section from the preserved raw block.
	if len(sc.Hosts.List) > 0 {
		fields["hosts"] = sc.Hosts
	} else {
		delete(raw, "hosts")
	}
	for key, val := range fields {
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", key, err)
		}
		raw[key] = b
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// SaveTheme updates only the theme name in config and saves.
func SaveTheme(themeName string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.UI.Theme.Name = themeName
	cfg.UI.Theme.Community = ""
	cfg.UI.Theme.Overrides = nil
	return Save(cfg)
}

// SaveThemeWithOverrides saves a theme name and full overrides map to config.
func SaveThemeWithOverrides(themeName string, overrides map[string]interface{}) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.UI.Theme.Name = themeName
	cfg.UI.Theme.Community = ""
	cfg.UI.Theme.Overrides = overrides
	return Save(cfg)
}

// SaveCommunityTheme saves a community theme reference with optional user overrides.
// Only the scheme name is stored — the full palette is computed at runtime.
func SaveCommunityTheme(communityName string, userOverrides map[string]interface{}) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.UI.Theme.Name = "default"
	cfg.UI.Theme.Community = communityName
	cfg.UI.Theme.Overrides = userOverrides
	return Save(cfg)
}

// SaveProjectTheme updates a specific project's theme in config and saves.
func SaveProjectTheme(projectPath string, theme *ThemeConfig) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	for i, proj := range cfg.Projects.List {
		if proj.Path == projectPath {
			cfg.Projects.List[i].Theme = theme
			return Save(cfg)
		}
	}
	return fmt.Errorf("project not found: %s", projectPath)
}

// SaveGlobalTheme saves a ThemeConfig as the global UI theme.
func SaveGlobalTheme(tc ThemeConfig) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.UI.Theme = tc
	return Save(cfg)
}

// SaveLastOpenInApp persists the last-used "open in" app ID.
// If projectPath matches a configured project, that project's LastOpenInApp is set.
// The global UI.LastOpenInApp is always set as a fallback.
func SaveLastOpenInApp(projectPath, appID string) error {
	cfg, err := LoadFrom(ConfigPath())
	if err != nil {
		return err
	}
	for i, proj := range cfg.Projects.List {
		if proj.Path == projectPath {
			cfg.Projects.List[i].LastOpenInApp = appID
			break
		}
	}
	cfg.UI.LastOpenInApp = appID
	return Save(cfg)
}

// SaveWorkspace applies a change to the plugins.workspace section and writes
// it. Like SaveUI it reloads first, so a setting changed in Configuration never
// overwrites an edit made to the file since Sidecar started.
func SaveWorkspace(mutate func(*WorkspacePluginConfig)) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	mutate(&cfg.Plugins.Workspace)
	if err := cfg.Validate(); err != nil {
		return err
	}
	return Save(cfg)
}

// SaveNotifications reloads immediately before applying a targeted mutation,
// validates the complete section, and writes only after validation succeeds.
func SaveNotifications(mutate func(*NotificationsConfig)) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	mutate(&cfg.Notifications)
	if err := ValidateNotifications(cfg.Notifications, ConfigPath()); err != nil {
		return err
	}
	return Save(cfg)
}

// SaveSelection applies a change to the selection section — how text selection
// behaves in every surface that offers it — and writes it. Like the others it
// reloads first, so a setting changed in Configuration never overwrites an edit
// made to the file since Sidecar started.
func SaveSelection(mutate func(*SelectionConfig)) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	mutate(&cfg.Selection)
	if err := cfg.Validate(); err != nil {
		return err
	}
	return Save(cfg)
}

// SaveUI applies a change to the ui section and writes it. It reloads first, so
// a setting changed in Configuration never overwrites an edit made to the file
// since Sidecar started.
func SaveUI(mutate func(*UIConfig)) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	mutate(&cfg.UI)
	return Save(cfg)
}

// SavePlugins applies a change to the plugins section and writes it. It is the
// panel-enablement path: which surfaces Sidecar assembles, and the handful of
// inputs those surfaces read. Like the other helpers it reloads first, so a
// setting changed in Configuration never overwrites an edit made to the file
// since Sidecar started, and it validates before writing so an out-of-range
// interval cannot reach disk.
func SavePlugins(mutate func(*PluginsConfig)) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	mutate(&cfg.Plugins)
	if err := cfg.Validate(); err != nil {
		return err
	}
	return Save(cfg)
}
