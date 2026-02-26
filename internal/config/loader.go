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

// SetTestConfigPath sets a custom config path for testing.
// Call ResetTestConfigPath() in test cleanup to restore default behavior.
func SetTestConfigPath(path string) {
	testConfigPath = path
}

// ResetTestConfigPath clears the test config path override.
func ResetTestConfigPath() {
	testConfigPath = ""
}

// rawConfig is the JSON-unmarshaling intermediary.
type rawConfig struct {
	Projects rawProjectsConfig `json:"projects"`
	Plugins  rawPluginsConfig  `json:"plugins"`
	Keymap   KeymapConfig      `json:"keymap"`
	UI       rawUIConfig       `json:"ui"`
	Features FeaturesConfig    `json:"features"`
}

type rawUIConfig struct {
	ShowClock        *bool       `json:"showClock"`
	Theme            ThemeConfig `json:"theme"`
	NerdFontsEnabled *bool       `json:"nerdFontsEnabled"`
}

type rawProjectsConfig struct {
	Mode string             `json:"mode"`
	Root string             `json:"root"`
	List []rawProjectConfig `json:"list"`
}

type rawProjectConfig struct {
	Name  string       `json:"name"`
	Path  string       `json:"path"`
	Theme *ThemeConfig `json:"theme,omitempty"`
}

type rawPluginsConfig struct {
	GitStatus     rawGitStatusConfig     `json:"git-status"`
	TDMonitor     rawTDMonitorConfig     `json:"td-monitor"`
	Conversations rawConversationsConfig `json:"conversations"`
	Workspace     rawWorkspaceConfig     `json:"workspace"`
}

type rawWorkspaceConfig struct {
	DirPrefix            *bool           `json:"dirPrefix"`
	DefaultAgentType     string          `json:"defaultAgentType"`
	LegacyDefaultAgent   string          `json:"defaultAgent"` // Backward compatibility
	AgentStart           json.RawMessage `json:"agentStart"`
	TmuxCaptureMaxBytes  *int            `json:"tmuxCaptureMaxBytes"`
	InteractiveExitKey   string          `json:"interactiveExitKey"`
	InteractiveAttachKey string          `json:"interactiveAttachKey"`
	InteractiveCopyKey   string          `json:"interactiveCopyKey"`
	InteractivePasteKey  string          `json:"interactivePasteKey"`
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

const (
	envWorkspaceDefaultAgentType = "SIDECAR_WORKSPACE_DEFAULT_AGENT_TYPE"
	envDefaultAgentType          = "SIDECAR_DEFAULT_AGENT_TYPE"
)

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

	// Conversations
	if raw.Plugins.Conversations.Enabled != nil {
		cfg.Plugins.Conversations.Enabled = *raw.Plugins.Conversations.Enabled
	}
	if raw.Plugins.Conversations.ClaudeDataDir != "" {
		cfg.Plugins.Conversations.ClaudeDataDir = raw.Plugins.Conversations.ClaudeDataDir
	}

	// Workspace
	if raw.Plugins.Workspace.DirPrefix != nil {
		cfg.Plugins.Workspace.DirPrefix = *raw.Plugins.Workspace.DirPrefix
	}
	if raw.Plugins.Workspace.TmuxCaptureMaxBytes != nil {
		cfg.Plugins.Workspace.TmuxCaptureMaxBytes = *raw.Plugins.Workspace.TmuxCaptureMaxBytes
	}
	if raw.Plugins.Workspace.DefaultAgentType != "" {
		cfg.Plugins.Workspace.DefaultAgentType = raw.Plugins.Workspace.DefaultAgentType
	}
	if cfg.Plugins.Workspace.DefaultAgentType == "" && raw.Plugins.Workspace.LegacyDefaultAgent != "" {
		cfg.Plugins.Workspace.DefaultAgentType = raw.Plugins.Workspace.LegacyDefaultAgent
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

	if v, ok := os.LookupEnv(envWorkspaceDefaultAgentType); ok {
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
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDir, configFile)
}
