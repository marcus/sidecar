// Package config handles loading, saving, and validating user configuration
// from JSON files, including project settings, plugin options, keymaps, and UI
// preferences.
//
// # Panel enablement
//
// Every embedded panel is turned on and off with its own config key,
// "plugins.<id>.enabled". That is what the Configuration surface writes and
// what the plugin descriptors read:
//
//	{
//	  "plugins": {
//	    "tasks": { "enabled": true },
//	    "notes": { "enabled": false }
//	  }
//	}
//
// The "tasks_plugin" and "notes_plugin" feature flags are read-only aliases of
// those keys, kept for one minor release. A flag answers only while its config
// key is absent, and nothing writes one back: a user who set a flag keeps it,
// and the first time they use the switch the config key becomes the answer.
// The release that retires the aliases removes the flags.
//
// The embedded Tasks tab is off until it is asked for. It is a tab of the
// global space — [Agents] [Workspaces] [Tasks], reached with K or the Sidecar
// brand — not a project plugin, so it is not part of the project tab order and
// "plugins.tasks.position" no longer moves it. The field is still accepted and
// validated so older configs keep loading.
//
// There is deliberately no Tasks store or JSONL path here. The embedded Tasks
// package performs its own normal configuration resolution.
//
// # External plugins
//
// "plugins.external" configures plugins that speak the Sidecar plugin
// protocol. "terminalResources.providers" is the frozen resource protocol's own
// section and is a read-only alias of it: entries there are still loaded and
// still dispatched on the resource identifier, and Save never writes the
// section, so it stays exactly as the user wrote it until the release that
// rewrites those entries into "plugins.external".
package config
