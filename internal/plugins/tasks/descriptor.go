package tasks

import (
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/panelpref"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/version"
)

// Descriptor is what Sidecar knows about the Tasks tab before it runs.
//
// Tasks is the global-scope embedded plugin: it is built once by the app shell
// rather than registered as a project plugin, because registry.Reinit would
// otherwise close its store and rebuild its agent queue on every project
// switch. Nothing about that is special-cased any more — it is Scope global
// here and the host reads it.
//
// Enablement is plugins.tasks.enabled, with the tasks_plugin feature flag as a
// read-only alias while the config key is absent. The rule lives in
// internal/panelpref so that every reader of it — this descriptor, the update
// checks, the settings pages — gives the same answer.
func Descriptor() plugin.Descriptor {
	return plugin.Descriptor{
		ID:    pluginID,
		Name:  "Tasks",
		Icon:  pluginIcon,
		Class: plugin.ClassEmbedded,
		Scope: plugin.ScopeGlobal,
		// Tasks is one Bubble Tea frame: it has a navbar tab and no pane
		// content of its own.
		Placements:  []plugin.Placement{plugin.PlacementTab},
		Detail:      "Embedded Tasks global tab, backed by the Tasks command",
		Why:         "Tasks adds an embedded task board to Sidecar's global space. It is a beta integration.",
		Beta:        true,
		Integration: version.TasksDescriptor(),
		Enabled:     panelpref.Tasks,
		SetEnabled:  func(p *config.PluginsConfig, on bool) { p.Tasks.Enabled = &on },
		New:         func() plugin.Plugin { return New() },
	}
}
