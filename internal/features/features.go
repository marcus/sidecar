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

	// TmuxInlineEdit enables inline file editing via tmux in the files plugin.
	TmuxInlineEdit = Feature{
		Name:        "tmux_inline_edit",
		Default:     true,
		Description: "Enable inline file editing via tmux in the files plugin",
	}

	// NotesPlugin enables the notes plugin for capturing quick notes.
	NotesPlugin = Feature{
		Name:        "notes_plugin",
		Default:     false,
		Description: "Enable the notes plugin for capturing quick notes",
	}

	// RunPlugin enables the run plugin for executing project commands.
	RunPlugin = Feature{
		Name:        "run_plugin",
		Default:     false,
		Description: "Enable the run plugin for executing project commands",
	}
)

// allFeatures is the registry of all known features.
var allFeatures = []Feature{
	TmuxInteractiveInput,
	TmuxInlineEdit,
	NotesPlugin,
	RunPlugin,
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
