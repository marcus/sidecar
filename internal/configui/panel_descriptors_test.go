package configui

import (
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/panelpref"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/version"
)

// testPluginDescriptors is the catalog the Panels tests render.
//
// configui cannot import internal/plugins/assembly — the plugin packages import
// internal/app, which owns this surface — so the fixture states the same
// descriptors the real catalog does. The guard against the two drifting is
// TestPanelsPageListsEverySurfaceInTheRealCatalog in internal/plugins/assembly,
// which renders this page with assembly.Descriptors().
func testPluginDescriptors() []plugin.Descriptor {
	return []plugin.Descriptor{
		{
			ID: panelIDTD, Name: "td", Class: plugin.ClassEmbedded, Scope: plugin.ScopeProject,
			Placements: []plugin.Placement{plugin.PlacementTab},
			Detail:     "Issues and task state from the current project",
			Enabled:    func(c *config.Config) bool { return c.Plugins.TDMonitor.Enabled },
			SetEnabled: func(p *config.PluginsConfig, on bool) { p.TDMonitor.Enabled = on },
		},
		{
			ID: panelIDGit, Name: "Git", Class: plugin.ClassEmbedded, Scope: plugin.ScopeProject,
			Placements: []plugin.Placement{plugin.PlacementTab},
			Detail:     "Status, commits, branches, and diffs",
			Enabled:    func(c *config.Config) bool { return c.Plugins.GitStatus.Enabled },
			SetEnabled: func(p *config.PluginsConfig, on bool) { p.GitStatus.Enabled = on },
		},
		{
			ID: panelIDFiles, Name: "Files", Class: plugin.ClassEmbedded, Scope: plugin.ScopeProject,
			Placements: []plugin.Placement{plugin.PlacementTab},
			Detail:     "Project browser and inline editing",
			Enabled:    func(c *config.Config) bool { return c.Plugins.FileBrowser.Enabled },
			SetEnabled: func(p *config.PluginsConfig, on bool) { p.FileBrowser.Enabled = on },
		},
		{
			ID: panelIDConversations, Name: "Conversations", Class: plugin.ClassEmbedded, Scope: plugin.ScopeProject,
			Placements: []plugin.Placement{plugin.PlacementTab},
			Detail:     "Session history from supported agent harnesses",
			Enabled: func(c *config.Config) bool {
				return features.IsEnabled(features.ConversationsPlugin.Name) && c.Plugins.Conversations.Enabled
			},
			SetEnabled: func(p *config.PluginsConfig, on bool) { p.Conversations.Enabled = on },
		},
		{
			ID: "workspace-manager", Name: "Workspaces", Class: plugin.ClassEmbedded, Scope: plugin.ScopeProject,
			Placements: []plugin.Placement{plugin.PlacementTab},
			Detail:     "Shells, worktrees, and agents for the current project",
		},
		{
			ID: panelIDNotes, Name: "Notes", Class: plugin.ClassEmbedded, Scope: plugin.ScopeProject,
			Placements: []plugin.Placement{plugin.PlacementTab},
			Detail:     "Project notes, kept inside Sidecar",
			Why:        "Notes adds project notes to Sidecar.",
			Enabled:    panelpref.Notes,
			Preference: panelpref.NotesPreference,
			SetEnabled: func(p *config.PluginsConfig, on bool) { p.Notes.Enabled = &on },
		},
		{
			ID: panelIDTasks, Name: "Tasks", Class: plugin.ClassEmbedded, Scope: plugin.ScopeGlobal,
			Placements:  []plugin.Placement{plugin.PlacementTab},
			Detail:      "Embedded Tasks global tab, backed by the Tasks command",
			Why:         "Tasks adds an embedded task board to Sidecar's global space. It is a beta integration.",
			Beta:        true,
			Integration: version.TasksDescriptor(),
			Enabled:     panelpref.Tasks,
			SetEnabled:  func(p *config.PluginsConfig, on bool) { p.Tasks.Enabled = &on },
		},
	}
}
