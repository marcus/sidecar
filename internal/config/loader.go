package config

import (
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

// rawConfig is the JSON-unmarshaling intermediary.
type rawConfig struct {
	Projects rawProjectsConfig `json:"projects"`
	Plugins  rawPluginsConfig  `json:"plugins"`
	Keymap   KeymapConfig      `json:"keymap"`
	UI       UIConfig          `json:"ui"`
}

type rawProjectsConfig struct {
	Mode string             `json:"mode"`
	Root string             `json:"root"`
	List []rawProjectConfig `json:"list"`
}

type rawProjectConfig struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type rawPluginsConfig struct {
	GitStatus     rawGitStatusConfig     `json:"git-status"`
	TDMonitor     rawTDMonitorConfig     `json:"td-monitor"`
	Conversations rawConversationsConfig `json:"conversations"`
	Worktree      rawWorktreeConfig      `json:"worktree"`
}

type rawWorktreeConfig struct {
	DirPrefix           *bool `json:"dirPrefix"`
	TmuxCaptureMaxBytes *int  `json:"tmuxCaptureMaxBytes"`
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

type rawConversationsConfig struct {
	Enabled       *bool  `json:"enabled"`
	ClaudeDataDir string `json:"claudeDataDir"`
}

// Load loads configuration from the default location.
func Load() (*Config, error) {
	return LoadFrom("")
}

// LoadFrom loads configuration from a specific path.
// If path is empty, uses ~/.config/sidecar/config.json
func LoadFrom(path string) (*Config, error) {
	cfg := Default()

	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return cfg, nil // Return defaults on error
		}
		path = filepath.Join(home, configDir, configFile)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
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

	// Expand paths
	cfg.Plugins.Conversations.ClaudeDataDir = expandPath(cfg.Plugins.Conversations.ClaudeDataDir)

	// Expand paths in project list and warn if path doesn't exist
	for i := range cfg.Projects.List {
		cfg.Projects.List[i].Path = expandPath(cfg.Projects.List[i].Path)
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
			cfg.Projects.List[i] = ProjectConfig{
				Name: rp.Name,
				Path: rp.Path,
			}
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

	// Conversations
	if raw.Plugins.Conversations.Enabled != nil {
		cfg.Plugins.Conversations.Enabled = *raw.Plugins.Conversations.Enabled
	}
	if raw.Plugins.Conversations.ClaudeDataDir != "" {
		cfg.Plugins.Conversations.ClaudeDataDir = raw.Plugins.Conversations.ClaudeDataDir
	}

	// Worktree
	if raw.Plugins.Worktree.DirPrefix != nil {
		cfg.Plugins.Worktree.DirPrefix = *raw.Plugins.Worktree.DirPrefix
	}
	if raw.Plugins.Worktree.TmuxCaptureMaxBytes != nil {
		cfg.Plugins.Worktree.TmuxCaptureMaxBytes = *raw.Plugins.Worktree.TmuxCaptureMaxBytes
	}

	// Keymap
	if raw.Keymap.Overrides != nil {
		for k, v := range raw.Keymap.Overrides {
			cfg.Keymap.Overrides[k] = v
		}
	}

	// UI
	cfg.UI.ShowFooter = raw.UI.ShowFooter
	cfg.UI.ShowClock = raw.UI.ShowClock
	if raw.UI.Theme.Name != "" {
		cfg.UI.Theme.Name = raw.UI.Theme.Name
	}
	if raw.UI.Theme.Overrides != nil {
		for k, v := range raw.UI.Theme.Overrides {
			cfg.UI.Theme.Overrides[k] = v
		}
	}
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
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
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDir, configFile)
}
