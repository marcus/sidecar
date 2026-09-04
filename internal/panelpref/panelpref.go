// Package panelpref answers "does the user want this embedded panel", for the
// two panels whose feature flag is now a read-only alias of their config key.
//
// The answer has several readers and only some of them can import the plugin
// package that owns the descriptor: internal/plugins/notes imports
// internal/app, which imports internal/overview, so the two create pickers
// cannot import the descriptor back. Rather than let each of them restate the
// rule — which is how the Flags page and the Panels page came to disagree about
// Notes and Tasks — every reader binds here, the descriptors included. There is
// one rule, in one place, and a leaf package can read it.
//
// The feature flags are read-only aliases: they answer only while the config
// key is absent, and nothing here ever writes one.
package panelpref

import (
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
)

// NotesPreference is what the user chose for the Notes panel: plugins.notes.enabled
// when it is set, the notes_plugin flag while it is absent.
//
// It is the preference and not the effective answer, because the settings page
// renders this: a Notes row reading OFF because td is off would be Sidecar
// claiming a choice the user never made.
func NotesPreference(cfg *config.Config) bool {
	return preference(cfg, notesEnabled, features.NotesPlugin.Name)
}

// Notes is whether the Notes surface actually exists. td owns Notes
// persistence, so the effective answer also requires the td panel. That
// dependency is deliberately not part of the preference.
func Notes(cfg *config.Config) bool {
	if cfg == nil {
		cfg = config.Default()
	}
	return cfg.Plugins.TDMonitor.Enabled && NotesPreference(cfg)
}

// Tasks is whether the embedded Tasks tab is wanted: plugins.tasks.enabled when
// it is set, the tasks_plugin flag while it is absent. Tasks depends on no
// other panel, so this is both the preference and the effective answer.
func Tasks(cfg *config.Config) bool {
	return preference(cfg, tasksEnabled, features.TasksPlugin.Name)
}

func notesEnabled(cfg *config.Config) *bool { return cfg.Plugins.Notes.Enabled }
func tasksEnabled(cfg *config.Config) *bool { return cfg.Plugins.Tasks.Enabled }

// preference is the alias rule itself: the config key decides whenever it is
// present, and the flag answers only while it is absent.
func preference(cfg *config.Config, key func(*config.Config) *bool, flag string) bool {
	if cfg == nil {
		cfg = config.Default()
	}
	if enabled := key(cfg); enabled != nil {
		return *enabled
	}
	return features.IsEnabled(flag)
}
