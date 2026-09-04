package features

import (
	"errors"
	"sync"

	"github.com/marcus/sidecar/internal/config"
)

// ErrNotInitialized is returned when the feature manager is not initialized.
var ErrNotInitialized = errors.New("feature manager not initialized")

// Feature represents a known feature flag with its default value.
type Feature struct {
	Name        string
	Default     bool
	Description string
}

// Known feature flags - add new features here.
var (
	// TmuxInteractiveInput enables write support for tmux panes.
	TmuxInteractiveInput = Feature{
		Name:        "tmux_interactive_input",
		Default:     true,
		Description: "Enable write support for tmux panes",
	}

	// TmuxFullAttach suspends Sidecar and runs `tmux attach-session`.
	// Off by default so users stay in the embedded pane.
	TmuxFullAttach = Feature{
		Name:        "tmux_full_attach",
		Default:     false,
		Description: "Suspend Sidecar and attach to the full tmux session",
	}

	// WorkspaceTerminalPanel enables the workspace split terminal pane —
	// live terminals placed beside the session, which is also what
	// `sidecar create shell` defaults to from inside a shell (nt-7c82c9).
	WorkspaceTerminalPanel = Feature{
		Name:        "workspace_terminal_panel",
		Default:     true,
		Description: "Enable the workspace split terminal panel",
	}

	// TmuxInlineEdit enables inline file editing via tmux in the files plugin.
	TmuxInlineEdit = Feature{
		Name:        "tmux_inline_edit",
		Default:     true,
		Description: "Enable inline file editing via tmux in the files plugin",
	}

	// FilesAutoRefresh enables watching expanded directories in the files
	// plugin and refreshing the tree when they change on disk.
	FilesAutoRefresh = Feature{
		Name:        "files_auto_refresh",
		Default:     true,
		Description: "Auto-refresh the files tree when watched directories change on disk",
	}

	// NotesPlugin enables the notes plugin for capturing quick notes. Notes is
	// stable and defaults on; assembly additionally requires the existing td
	// panel preference because td owns its storage.
	NotesPlugin = Feature{
		Name:        "notes_plugin",
		Default:     true,
		Description: "Enable the notes plugin for capturing quick notes",
	}

	// TasksPlugin enables the embedded Tasks plugin tab.
	TasksPlugin = Feature{
		Name:        "tasks_plugin",
		Default:     false,
		Description: "Enable the embedded Tasks plugin tab",
	}

	// ConversationsPlugin enables the multi-agent session history tab.
	// Off by default: history lives in each harness; this is an opt-in viewer.
	// When disabled, Sidecar does not construct history adapters or read
	// agent session stores (see assembly.ConversationsWanted).
	ConversationsPlugin = Feature{
		Name:        "conversations_plugin",
		Default:     false,
		Description: "Enable the Conversations plugin (multi-agent session history)",
	}

	// WorkspaceDocPanes enables document panes beside workspace terminals.
	// When off there is no pane tree, so Diff is also unavailable.
	WorkspaceDocPanes = Feature{
		Name:        "workspace_doc_panes",
		Default:     true,
		Description: "Open documents in panes beside workspace terminals. When disabled, Diff is unavailable (no pane tree).",
	}

	// PluginContentPanes enables the app-owned passive content deck around
	// eligible capability-driven plugins. Files, Git, Notes, and the embedded
	// td/Tasks focus hosts have completed their parity journeys; td/Tasks still
	// expose no link surface until their owning packages publish safe passive
	// text rectangles.
	PluginContentPanes = Feature{
		Name:        "plugin_content_panes",
		Default:     true,
		Description: "Open passive content panes beside eligible project plugins",
	}

	// PaneMove enables pane repositioning: the M key and the header ⊞ opening
	// the shared reposition modal on all three pane hosts, and the `sidecar
	// layout move` verb over the same planner.
	//
	// Default on since the complete journey — keyboard, modal, and agent — is
	// proven together. It remains a flag because the header control is visible
	// to every user of every surface on the first frame after install, and
	// turning it off must remove the whole feature rather than part of it.
	PaneMove = Feature{
		Name:        "pane_move",
		Default:     true,
		Description: "Reposition panes with M, the pane header's layout button, and sidecar layout move",
	}

	// WorkspaceDocPanesDisabledDiff is the toast/no-op copy when Diff is
	// requested while the pane tree is off.
	WorkspaceDocPanesDisabledDiff = "Document panes are disabled; Diff needs the workspace pane tree"

	// TerminalResourceProviders gates external terminal resource providers:
	// the matchers they declare, the panes they open, and the describe pass
	// that asks them what they are. Off by default until the whole journey is
	// proven, and deliberately a flag rather than "is anything configured":
	// a user should be able to write the configuration and turn it on
	// separately, and turning it off should stop every provider process.
	TerminalResourceProviders = Feature{
		Name:        "terminal_resource_providers",
		Default:     false,
		Description: "Recognize and open resources from configured external terminal resource providers",
	}

	// PluginProtocol gates the plugin protocol host: the plugins.external
	// configuration section, the sidecar.plugin/v1 envelope, and every
	// `sidecar plugin` verb that talks to an external plugin process.
	//
	// Off by default while the contract is a draft. Deliberately a flag rather
	// than "is anything configured": a user must be able to write the
	// configuration and turn it on separately, and turning it off must stop
	// every plugin process rather than only hide the surfaces.
	//
	// It does not gate terminal_resource_providers. A provider configured under
	// terminalResources keeps being described and resolved on the frozen
	// sidecar.terminal-resource/v1 identifier whatever this flag says.
	PluginProtocol = Feature{
		Name:        "plugin_protocol",
		Default:     false,
		Description: "Host external plugins that speak the Sidecar plugin protocol",
	}

	// CrossProjectOverview gates the cross-project agent overview.
	CrossProjectOverview = Feature{
		Name:        "cross_project_overview",
		Default:     true,
		Description: "Enable the cross-project agent overview",
	}

	// SidecarRemoteHosts enables viewing and interacting with sessions on other
	// machines that have Sidecar installed, over SSH. It should cost nothing at
	// all until a user asks for it.
	//
	// With the flag off, no host is read from config, no connection is made,
	// and no remote row exists — the local path is byte-identical. With it on
	// but no host registered, the same is true.
	SidecarRemoteHosts = Feature{
		Name:        "sidecar_remote_hosts",
		Default:     false,
		Description: "Use Sidecar sessions on other machines over SSH",
	}

	// AgentControl gates provider-aware agent list/get/start while the start,
	// prompt, and restore journey is being completed. Help and command metadata
	// remain visible when disabled so an agent can discover the capability.
	AgentControl = Feature{
		Name:        "agent_control",
		Default:     false,
		Description: "Control Sidecar-managed agents through provider-aware commands",
	}
)

// allFeatures is the registry of all known features.
var allFeatures = []Feature{
	TmuxInteractiveInput,
	TmuxFullAttach,
	TmuxInlineEdit,
	FilesAutoRefresh,
	NotesPlugin,
	TasksPlugin,
	ConversationsPlugin,
	WorkspaceDocPanes,
	PluginContentPanes,
	PaneMove,
	WorkspaceTerminalPanel,
	CrossProjectOverview,
	TerminalResourceProviders,
	PluginProtocol,
	SidecarRemoteHosts,
	AgentControl,
}

// defaultValues provides O(1) lookup for feature defaults.
var defaultValues = buildDefaultMap()

func buildDefaultMap() map[string]bool {
	m := make(map[string]bool, len(allFeatures))
	for _, f := range allFeatures {
		m[f.Name] = f.Default
	}
	return m
}

// IsKnownFeature returns true if the feature name is registered.
func IsKnownFeature(name string) bool {
	_, ok := defaultValues[name]
	return ok
}

// DefaultEnabled reports a feature's built-in default, ignoring configuration
// and CLI overrides. It is what a surface editing the config file needs: a flag
// absent from features.flags is not "off", it is "whatever this build defaults
// to", and a settings page that showed OFF for a default-on flag would be
// lying.
func DefaultEnabled(name string) bool { return getDefault(name) }

// Manager handles feature flag state.
type Manager struct {
	mu        sync.RWMutex
	cfg       *config.Config
	overrides map[string]bool // CLI overrides take precedence
}

// globalManager is the singleton instance.
var globalManager *Manager

// Init initializes the feature flag manager with the given config.
// Should be called once at startup after config is loaded.
func Init(cfg *config.Config) {
	globalManager = &Manager{
		cfg:       cfg,
		overrides: make(map[string]bool),
	}
}

// SetConfig replaces the configuration snapshot while preserving command-line
// overrides. Configuration saves use this so feature-gated runtime
// reconciliation sees the just-written flags without changing override
// precedence.
func SetConfig(cfg *config.Config) {
	if globalManager == nil {
		Init(cfg)
		return
	}
	globalManager.mu.Lock()
	globalManager.cfg = cfg
	globalManager.mu.Unlock()
}

// SetOverride sets a CLI override for a feature flag.
// Overrides take precedence over config values.
func SetOverride(name string, enabled bool) {
	if globalManager == nil {
		return
	}
	globalManager.mu.Lock()
	defer globalManager.mu.Unlock()
	globalManager.overrides[name] = enabled
}

// IsEnabled checks if a feature is enabled.
// Priority: CLI override > config > default.
func IsEnabled(name string) bool {
	if globalManager == nil {
		// Fall back to default if not initialized
		return getDefault(name)
	}

	globalManager.mu.RLock()
	defer globalManager.mu.RUnlock()

	// Check CLI overrides first
	if enabled, ok := globalManager.overrides[name]; ok {
		return enabled
	}

	// Check config
	if globalManager.cfg != nil && globalManager.cfg.Features.Flags != nil {
		if enabled, ok := globalManager.cfg.Features.Flags[name]; ok {
			return enabled
		}
	}

	// Fall back to default
	return getDefault(name)
}

// getDefault returns the default value for a feature.
func getDefault(name string) bool {
	if val, ok := defaultValues[name]; ok {
		return val
	}
	return false // Unknown features default to disabled
}

// List returns all known features with their current enabled state.
func List() map[string]bool {
	result := make(map[string]bool, len(allFeatures))
	if globalManager != nil {
		globalManager.mu.RLock()
		defer globalManager.mu.RUnlock()
		for _, f := range allFeatures {
			result[f.Name] = isEnabledLocked(f.Name)
		}
	} else {
		for _, f := range allFeatures {
			result[f.Name] = getDefault(f.Name)
		}
	}
	return result
}

// isEnabledLocked checks feature state without acquiring locks (caller must hold lock).
func isEnabledLocked(name string) bool {
	// Check CLI overrides first
	if enabled, ok := globalManager.overrides[name]; ok {
		return enabled
	}
	// Check config
	if globalManager.cfg != nil && globalManager.cfg.Features.Flags != nil {
		if enabled, ok := globalManager.cfg.Features.Flags[name]; ok {
			return enabled
		}
	}
	return getDefault(name)
}

// ListAll returns all known features with metadata.
// Returns a copy to prevent mutation of internal state.
func ListAll() []Feature {
	result := make([]Feature, len(allFeatures))
	copy(result, allFeatures)
	return result
}

// SetEnabled updates a feature flag in the config and saves it.
// Returns an error if the config cannot be saved or if the manager is not initialized.
func SetEnabled(name string, enabled bool) error {
	if globalManager == nil {
		return ErrNotInitialized
	}

	globalManager.mu.Lock()
	defer globalManager.mu.Unlock()

	// Reload from disk to avoid overwriting changes made since startup.
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if cfg.Features.Flags == nil {
		cfg.Features.Flags = make(map[string]bool)
	}
	cfg.Features.Flags[name] = enabled

	// Update in-memory config.
	globalManager.cfg.Features.Flags = cfg.Features.Flags

	return config.Save(cfg)
}
