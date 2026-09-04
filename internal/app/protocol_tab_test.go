package app

import (
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/pluginbrowser"
	"github.com/marcus/sidecar/internal/pluginhost"
)

// protocolTabConfig configures one external plugin with a tab placement, behind
// the flag that gates the whole plugin protocol.
func protocolTabConfig(t *testing.T, placements ...string) *config.Config {
	t.Helper()
	if len(placements) == 0 {
		placements = []string{config.PluginPlacementTab, config.PluginPlacementPanes}
	}
	cfg := config.Default()
	cfg.Plugins.External = []config.PluginInstanceConfig{{
		ID:         "recall",
		Command:    []string{"recall", "sidecar-plugin"},
		Enabled:    true,
		Scope:      config.PluginScopeGlobal,
		Placements: placements,
	}}
	cfg.Features.Flags[features.PluginProtocol.Name] = true
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })
	return cfg
}

// A configured protocol plugin that declares a tab placement is a global tab,
// hosted by the same globalPluginHost Tasks gets. That is the whole point of
// M1's generalization: the second global plugin is a config entry.
func TestProtocolPluginBecomesAGlobalTab(t *testing.T) {
	isolateAppState(t)
	cfg := protocolTabConfig(t)

	descriptors := globalProtocolDescriptors(cfg)
	if len(descriptors) != 1 || descriptors[0].ID != "recall" {
		t.Fatalf("descriptors = %+v, want the configured plugin", descriptors)
	}
	if descriptors[0].Class != plugin.ClassProtocol {
		t.Fatalf("class = %q, want protocol", descriptors[0].Class)
	}
	if descriptors[0].New == nil {
		t.Fatal("a tab descriptor with no constructor can never be hosted")
	}

	km := keymap.NewRegistry()
	registry := plugin.NewRegistry(&plugin.Context{Keymap: km, WorkDir: "/tmp/one", ProjectRoot: "/tmp/one"})
	if err := registry.Register(&navigationPlugin{id: "files"}); err != nil {
		t.Fatal(err)
	}
	m := New(registry, km, cfg, "", "/tmp/one", "/tmp/one", "files")

	var found bool
	for _, host := range m.globalHosts {
		if host.id() == "recall" {
			found = true
			if _, ok := host.plugin.(*pluginbrowser.TabPlugin); !ok {
				t.Fatalf("the recall tab is hosted by %T, not the shared browser", host.plugin)
			}
		}
	}
	if !found {
		t.Fatalf("no global host for the configured protocol plugin: %d hosts", len(m.globalHosts))
	}

	m.scope = ScopeGlobal
	m.globalTab = "recall"
	tab, ok := m.globalSurfaceByID("recall")
	if !ok {
		t.Fatal("the protocol tab is not on the global row")
	}
	if tab.name != "recall" {
		t.Fatalf("tab label = %q; the descriptor's name stands until describe lands", tab.name)
	}
	// Sessions and Activity keep their number keys; a plugin-provided tab takes
	// what is left, and never one of theirs.
	for _, surface := range m.globalTabsVisible() {
		if surface.id == "recall" && (surface.key == "8" || surface.key == "9") {
			t.Fatalf("the protocol tab took %q from an app-owned surface", surface.key)
		}
	}
}

// The flag gates the whole thing. With it off there is no descriptor, no host,
// and no tab — and turning it off cannot take a terminal resource provider down
// with it, which is the frozen section's own flag's job.
func TestProtocolTabIsBehindTheFlag(t *testing.T) {
	cfg := protocolTabConfig(t)
	cfg.Features.Flags[features.PluginProtocol.Name] = false
	features.Init(cfg)
	if got := globalProtocolDescriptors(cfg); len(got) != 0 {
		t.Fatalf("descriptors with the flag off = %+v", got)
	}
}

// A plugin that declares only panes has no navbar tab. Placement is what
// decides where content shows, and the header reads it rather than assuming.
func TestPanesOnlyProtocolPluginHasNoTab(t *testing.T) {
	cfg := protocolTabConfig(t, config.PluginPlacementPanes)
	if got := globalProtocolDescriptors(cfg); len(got) != 0 {
		t.Fatalf("a panes-only plugin produced %d tab descriptors", len(got))
	}
}

// Disabling the plugin removes its tab, through the same plugins.<id>.enabled
// switch every other plugin uses.
func TestDisabledProtocolPluginHasNoTab(t *testing.T) {
	cfg := protocolTabConfig(t)
	cfg.Plugins.External[0].Enabled = false
	if got := globalProtocolDescriptors(cfg); len(got) != 0 {
		t.Fatalf("a disabled plugin produced %d tab descriptors", len(got))
	}
}

// Building a protocol tab does no I/O and renders a loading state: a plugin's
// first process runs after the first ready frame, from the host's describe
// pass, and nothing about startup waits for one.
func TestProtocolTabRendersLoadingWithoutTouchingTheHost(t *testing.T) {
	cfg := protocolTabConfig(t)
	descriptors := globalProtocolDescriptors(cfg)
	p := descriptors[0].New()
	if err := p.Init(&plugin.Context{Keymap: keymap.NewRegistry()}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if cmd := p.Start(); cmd != nil {
		t.Fatal("Start returned work; a protocol tab starts nothing of its own")
	}
	view := p.View(120, 30)
	if !strings.Contains(view, "recall") {
		t.Fatalf("the loading card does not name the plugin:\n%s", view)
	}
	if lines := strings.Split(view, "\n"); len(lines) != 30 {
		t.Fatalf("the tab rendered %d lines for a 30-row box", len(lines))
	}
}

// The project on offer to a protocol plugin is republished from the same place
// the matchers are, because a global plugin outlives every project switch.
func TestProjectContextFollowsTheProjectSwitch(t *testing.T) {
	isolateAppState(t)
	cfg := protocolTabConfig(t)
	km := keymap.NewRegistry()
	registry := plugin.NewRegistry(&plugin.Context{Keymap: km, WorkDir: "/tmp/one", ProjectRoot: "/tmp/one"})
	if err := registry.Register(&navigationPlugin{id: "files"}); err != nil {
		t.Fatal(err)
	}
	m := New(registry, km, cfg, "", "/tmp/one", "/tmp/one", "files")
	t.Cleanup(func() { pluginBrowserProject.Store(nil) })

	m.publishResourceProviders()
	ctx := pluginBrowserContext()
	if ctx == nil || ctx.Project == nil || ctx.Project.Root != "/tmp/one" {
		t.Fatalf("context = %+v, want the current project", ctx)
	}

	m.registry.Reinit("/tmp/two", "/tmp/two")
	m.publishResourceProviders()
	ctx = pluginBrowserContext()
	if ctx == nil || ctx.Project == nil || ctx.Project.Root != "/tmp/two" {
		t.Fatalf("context after the switch = %+v", ctx)
	}
	if ctx.Project.Name != "two" {
		t.Fatalf("project name = %q", ctx.Project.Name)
	}
}

// A remote-bound surface carries that host's ID, so a plugin that only knows
// this machine can refuse naming the host rather than answer about the wrong
// checkout. Sidecar never substitutes a local path.
func TestRemoteBoundSurfacePassesTheHostID(t *testing.T) {
	isolateAppState(t)
	cfg := protocolTabConfig(t)
	km := keymap.NewRegistry()
	registry := plugin.NewRegistry(&plugin.Context{
		Keymap: km, WorkDir: "/srv/checkout", ProjectRoot: "/srv/checkout", HostID: "builder",
	})
	m := New(registry, km, cfg, "", "/srv/checkout", "/srv/checkout", "")
	t.Cleanup(func() { pluginBrowserProject.Store(nil) })

	m.publishResourceProviders()
	ctx := pluginBrowserContext()
	if ctx == nil || ctx.Project == nil {
		t.Fatal("no project context on a remote-bound surface")
	}
	if ctx.Project.HostID != "builder" {
		t.Fatalf("hostId = %q, want the bound host", ctx.Project.HostID)
	}
	if ctx.Project.Root != "/srv/checkout" {
		t.Fatalf("root = %q; a remote path must not be replaced by a local one", ctx.Project.Root)
	}
}

// The browser asks the manager for work, and the manager is the same one the
// resource protocol uses. With no manager the calls answer typed, not nil.
func TestPluginBrowserCallsAnswerBeforeTheHostExists(t *testing.T) {
	calls := pluginBrowserCalls("recall")
	if _, _, ok := calls.Describe("recall"); ok {
		t.Fatal("Describe claimed an answer with no manager running")
	}
	msg := calls.List(pluginbrowser.ListCall{
		Instance: "recall",
		Params:   pluginhost.ListParams{Collection: "results"},
	})()
	listed, ok := msg.(pluginbrowser.ListedMsg)
	if !ok || listed.Err == nil {
		t.Fatalf("list before the host exists = %#v", msg)
	}
}

// The header carries the plugin's own display name once describe has landed,
// and the configured instance ID until then. Only the protocol class asks: an
// embedded plugin's label is a Go literal beside the plugin, and the descriptor
// and the plugin cannot disagree about it.
func TestProtocolTabLabelComesFromTheDescribedPlugin(t *testing.T) {
	protocol := &globalPluginHost{
		descriptor: plugin.Descriptor{ID: "recall", Name: "recall", Class: plugin.ClassProtocol},
		plugin:     &hostedTestPlugin{id: "Recall Studio"},
	}
	if got := protocol.label(); got != "Recall Studio" {
		t.Fatalf("protocol label = %q, want the plugin's own name", got)
	}

	// A plugin with nothing to say yet keeps the descriptor's name, which is the
	// configured instance ID.
	silent := &globalPluginHost{
		descriptor: plugin.Descriptor{ID: "recall", Name: "recall", Class: plugin.ClassProtocol},
		plugin:     &hostedTestPlugin{id: ""},
	}
	if got := silent.label(); got != "recall" {
		t.Fatalf("undescribed label = %q, want the configured id", got)
	}

	// The embedded class never asks the plugin, whatever it would have said.
	embedded := &globalPluginHost{
		descriptor: plugin.Descriptor{ID: "tasks", Name: "Tasks", Class: plugin.ClassEmbedded},
		plugin:     &hostedTestPlugin{id: "something else"},
	}
	if got := embedded.label(); got != "Tasks" {
		t.Fatalf("embedded label = %q, want the descriptor's", got)
	}
}
