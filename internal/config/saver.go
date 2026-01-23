package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// saveConfig is the JSON-marshaling intermediary that uses string durations.
type saveConfig struct {
	Projects saveProjectsConfig `json:"projects"`
	Plugins  savePluginsConfig  `json:"plugins"`
	Keymap   KeymapConfig       `json:"keymap"`
	UI       UIConfig           `json:"ui"`
	Features FeaturesConfig     `json:"features,omitempty"`
}

type saveProjectsConfig struct {
	Mode string          `json:"mode,omitempty"`
	Root string          `json:"root,omitempty"`
	List []ProjectConfig `json:"list,omitempty"`
}

type savePluginsConfig struct {
	GitStatus     saveGitStatusConfig     `json:"git-status,omitempty"`
	TDMonitor     saveTDMonitorConfig     `json:"td-monitor,omitempty"`
	Conversations saveConversationsConfig `json:"conversations,omitempty"`
	Worktree      saveWorktreeConfig      `json:"worktree,omitempty"`
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

type saveWorktreeConfig struct {
	DirPrefix            *bool  `json:"dirPrefix,omitempty"`
	TmuxCaptureMaxBytes  *int   `json:"tmuxCaptureMaxBytes,omitempty"`
	InteractiveExitKey   string `json:"interactiveExitKey,omitempty"`
	InteractiveAttachKey string `json:"interactiveAttachKey,omitempty"`
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
			Conversations: saveConversationsConfig{
				Enabled:       &cfg.Plugins.Conversations.Enabled,
				ClaudeDataDir: cfg.Plugins.Conversations.ClaudeDataDir,
			},
			Worktree: saveWorktreeConfig{
				DirPrefix:            &cfg.Plugins.Worktree.DirPrefix,
				TmuxCaptureMaxBytes:  &cfg.Plugins.Worktree.TmuxCaptureMaxBytes,
				InteractiveExitKey:   cfg.Plugins.Worktree.InteractiveExitKey,
				InteractiveAttachKey: cfg.Plugins.Worktree.InteractiveAttachKey,
			},
		},
		Keymap:   cfg.Keymap,
		UI:       cfg.UI,
		Features: cfg.Features,
	}
}

// Save writes the config to ~/.config/sidecar/config.json
func Save(cfg *Config) error {
	path := ConfigPath()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	sc := toSaveConfig(cfg)
	data, err := json.MarshalIndent(sc, "", "  ")
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
	return Save(cfg)
}
