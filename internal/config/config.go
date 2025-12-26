package config

import "time"

// Config is the root configuration structure.
type Config struct {
	Projects ProjectsConfig `json:"projects"`
	Plugins  PluginsConfig  `json:"plugins"`
	Keymap   KeymapConfig   `json:"keymap"`
	UI       UIConfig       `json:"ui"`
}

// ProjectsConfig configures project detection and layout.
type ProjectsConfig struct {
	Mode string `json:"mode"` // "single" for now
	Root string `json:"root"` // "." default
}

// PluginsConfig holds per-plugin configuration.
type PluginsConfig struct {
	GitStatus     GitStatusPluginConfig     `json:"git-status"`
	TDMonitor     TDMonitorPluginConfig     `json:"td-monitor"`
	Conversations ConversationsPluginConfig `json:"conversations"`
}

// GitStatusPluginConfig configures the git status plugin.
type GitStatusPluginConfig struct {
	Enabled         bool          `json:"enabled"`
	RefreshInterval time.Duration `json:"refreshInterval"`
}

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
}

// KeymapConfig holds key binding overrides.
type KeymapConfig struct {
	Overrides map[string]string `json:"overrides"`
}

// UIConfig configures UI appearance.
type UIConfig struct {
	ShowFooter bool        `json:"showFooter"`
	ShowClock  bool        `json:"showClock"`
	Theme      ThemeConfig `json:"theme"`
}

// ThemeConfig configures the color theme.
type ThemeConfig struct {
	Name      string            `json:"name"`
	Overrides map[string]string `json:"overrides"`
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
				RefreshInterval: 2 * time.Second,
				DBPath:          ".todos/issues.db",
			},
			Conversations: ConversationsPluginConfig{
				Enabled:       true,
				ClaudeDataDir: "~/.claude",
			},
		},
		Keymap: KeymapConfig{
			Overrides: make(map[string]string),
		},
		UI: UIConfig{
			ShowFooter: true,
			ShowClock:  true,
			Theme: ThemeConfig{
				Name:      "default",
				Overrides: make(map[string]string),
			},
		},
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.Plugins.GitStatus.RefreshInterval < 0 {
		c.Plugins.GitStatus.RefreshInterval = time.Second
	}
	if c.Plugins.TDMonitor.RefreshInterval < 0 {
		c.Plugins.TDMonitor.RefreshInterval = 2 * time.Second
	}
	return nil
}
