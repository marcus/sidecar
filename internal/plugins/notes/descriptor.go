package notes

import (
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/panelpref"
	"github.com/marcus/sidecar/internal/plugin"
)

// Descriptor is what Sidecar knows about the Notes panel before it runs.
//
// Enablement is plugins.notes.enabled. The notes_plugin feature flag is a
// read-only alias kept for one minor release: it answers only while the config
// key is absent, and nothing here ever writes it back.
//
// td owns Notes persistence, so the effective answer also requires the td
// panel. That dependency is deliberately not part of the preference: a user who
// asked for Notes and then turned td off has not changed their mind about
// Notes, and the settings page must not rewrite the choice to say they have.
//
// Both answers come from internal/panelpref rather than being written here,
// because the create pickers in internal/overview and
// internal/plugins/workspace need the same answer and cannot import this
// package. One rule, one place.
func Descriptor() plugin.Descriptor {
	return plugin.Descriptor{
		ID:         pluginID,
		Name:       pluginName,
		Icon:       pluginIcon,
		Class:      plugin.ClassEmbedded,
		Scope:      plugin.ScopeProject,
		Placements: []plugin.Placement{plugin.PlacementTab},
		Detail:     "Project notes, kept inside Sidecar",
		Why:        "Notes adds project notes to Sidecar.",
		Enabled:    panelpref.Notes,
		Preference: panelpref.NotesPreference,
		SetEnabled: func(p *config.PluginsConfig, on bool) { p.Notes.Enabled = &on },
		New:        func() plugin.Plugin { return New() },
	}
}
