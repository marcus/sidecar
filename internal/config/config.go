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
	Mode string          `json:"mode"` // "single" for now
	Root string          `json:"root"` // "." default
	List []ProjectConfig `json:"list"` // list of configured projects for switcher
}

// ProjectConfig represents a single project in the project switcher.
type ProjectConfig struct {
	Name string `json:"name"` // display name for the project
	Path string `json:"path"` // absolute path to project root (supports ~ expansion)
}

// PluginsConfig holds per-plugin configuration.
type PluginsConfig struct {
	GitStatus     GitStatusPluginConfig     `json:"git-status"`
	TDMonitor     TDMonitorPluginConfig     `json:"td-monitor"`
	Conversations ConversationsPluginConfig `json:"conversations"`
	Worktree      WorktreePluginConfig      `json:"worktree"`
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

// WorktreePluginConfig configures the worktree plugin.
type WorktreePluginConfig struct {
	// DirPrefix prefixes worktree directory names with the repo name (e.g., 'myrepo-feature-auth')
	// This helps associate conversations with the repo after worktree deletion. Default: true.
	DirPrefix bool `json:"dirPrefix"`
	// TmuxCaptureMaxBytes caps tmux pane capture size for the preview pane. Default: 2MB.
	TmuxCaptureMaxBytes int `json:"tmuxCaptureMaxBytes"`
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
			Worktree: WorktreePluginConfig{
				DirPrefix:           true,
				TmuxCaptureMaxBytes: 2 * 1024 * 1024,
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
	if c.Plugins.Worktree.TmuxCaptureMaxBytes <= 0 {
		c.Plugins.Worktree.TmuxCaptureMaxBytes = 2 * 1024 * 1024
	}
	return nil
}
