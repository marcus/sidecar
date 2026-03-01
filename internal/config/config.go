package config

import "time"

// Config is the root configuration structure.
type Config struct {
	Projects ProjectsConfig `json:"projects"`
	Plugins  PluginsConfig  `json:"plugins"`
	Keymap   KeymapConfig   `json:"keymap"`
	UI       UIConfig       `json:"ui"`
	Features FeaturesConfig `json:"features"`
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
	Name          string       `json:"name"`                    // display name for the project
	Path          string       `json:"path"`                    // absolute path to project root (supports ~ expansion)
	Theme         *ThemeConfig `json:"theme,omitempty"`         // per-project theme (nil = use global)
	LastOpenInApp string       `json:"lastOpenInApp,omitempty"` // last app used to open this project (e.g. "vscode", "goland")
}

// PluginsConfig holds per-plugin configuration.
type PluginsConfig struct {
	GitStatus     GitStatusPluginConfig     `json:"git-status"`
	TDMonitor     TDMonitorPluginConfig     `json:"td-monitor"`
	Conversations ConversationsPluginConfig `json:"conversations"`
	Workspace     WorkspacePluginConfig     `json:"workspace"`
	Notes         NotesPluginConfig         `json:"notes"`
	Run           RunPluginConfig           `json:"run"`
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
	// TmuxCaptureMaxBytes caps tmux pane capture size for the preview pane. Default: 2MB.
	TmuxCaptureMaxBytes int `json:"tmuxCaptureMaxBytes"`
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
}

// NotesPluginConfig configures the notes plugin.
type NotesPluginConfig struct {
	// DefaultEditor sets the default editor mode when pressing Enter on a note.
	// Values: "builtin" (default), "vim", "nvim", or any $EDITOR value.
	// When set to "vim"/"nvim", Enter opens the note in inline vim instead of built-in editor.
	DefaultEditor string `json:"defaultEditor,omitempty"`
}

// RunPluginConfig configures the run plugin.
type RunPluginConfig struct {
	Commands []RunCommandConfig `json:"commands,omitempty"` // Manual commands
}

// RunCommandConfig represents a manually configured run command.
type RunCommandConfig struct {
	Name    string `json:"name"`            // Display name
	Command string `json:"command"`         // Shell command to execute
	Group   string `json:"group,omitempty"` // Optional grouping (e.g., "docker", "npm")
}

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
				RefreshInterval: 2 * time.Second,
				DBPath:          ".todos/issues.db",
			},
			Conversations: ConversationsPluginConfig{
				Enabled:       true,
				ClaudeDataDir: "~/.claude",
			},
			Workspace: WorkspacePluginConfig{
				DirPrefix:           true,
				TmuxCaptureMaxBytes: 2 * 1024 * 1024,
			},
		},
		Keymap: KeymapConfig{
			Overrides: make(map[string]string),
		},
		UI: UIConfig{
			ShowClock:  true,
			Theme: ThemeConfig{
				Name:      "default",
				Overrides: make(map[string]interface{}),
			},
		},
		Features: FeaturesConfig{
			Flags: make(map[string]bool),
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
	if c.Plugins.Workspace.TmuxCaptureMaxBytes <= 0 {
		c.Plugins.Workspace.TmuxCaptureMaxBytes = 2 * 1024 * 1024
	}
	return nil
}
