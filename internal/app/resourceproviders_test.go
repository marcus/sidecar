package app

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/terminallink"
)

func TestReadyLatchOpensOnce(t *testing.T) {
	latch := newReadyLatch()
	select {
	case <-latch.wait():
		t.Fatal("a fresh latch is already open")
	default:
	}
	latch.close()
	latch.close() // must not panic
	select {
	case <-latch.wait():
	default:
		t.Fatal("the latch did not open")
	}
}

func providerConfig(t *testing.T, argv0 string) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.TerminalResources.Providers = []config.TerminalResourceProviderConfig{
		{ID: "fixture", Command: []string{argv0}, Enabled: true},
	}
	return cfg
}

// The command must not exist at all when there is nothing to do, so the
// ordinary startup adds no waiting goroutine.
func TestDescribeResourceProvidersCmdIsAbsentWhenThereIsNothingToDo(t *testing.T) {
	if cmd := describeResourceProvidersCmd(nil); cmd != nil {
		t.Fatal("a nil config produced a command")
	}

	features.Init(config.Default())
	features.SetOverride(features.TerminalResourceProviders.Name, true)
	t.Cleanup(func() { features.SetOverride(features.TerminalResourceProviders.Name, false) })

	if cmd := describeResourceProvidersCmd(config.Default()); cmd != nil {
		t.Fatal("a config with no providers produced a command")
	}
}

// The feature flag is the master switch: with it off, no command exists even
// when providers are configured, so no provider process can start.
func TestDescribeResourceProvidersCmdIsGatedByTheFeatureFlag(t *testing.T) {
	features.Init(config.Default())
	features.SetOverride(features.TerminalResourceProviders.Name, false)

	cfg := providerConfig(t, "/bin/echo")
	if cmd := describeResourceProvidersCmd(cfg); cmd != nil {
		t.Fatal("the disabled feature still produced a describe command")
	}
}

// The command's first act is to wait on the latch. Until the first ready frame
// closes it, the command must not have run a provider — which, for a command
// whose only observable effect in M0 is the manager it publishes, means the
// manager must not exist yet.
func TestDescribeWaitsForTheFirstReadyFrame(t *testing.T) {
	features.Init(config.Default())
	features.SetOverride(features.TerminalResourceProviders.Name, true)
	t.Cleanup(func() { features.SetOverride(features.TerminalResourceProviders.Name, false) })

	// A command that would take a visible amount of time if it ever ran.
	marker := filepath.Join(t.TempDir(), "provider-ran")
	script := filepath.Join(t.TempDir(), "provider.sh")
	body := "#!/bin/sh\ntouch " + marker + "\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	latch := newReadyLatch()
	restore := firstReadyFrameLatch
	firstReadyFrameLatch = latch
	t.Cleanup(func() {
		firstReadyFrameLatch = restore
		ShutdownResourceProviders()
		resourceProviderHost.mu.Lock()
		resourceProviderHost.manager = nil
		resourceProviderHost.mu.Unlock()
	})

	cmd := describeResourceProvidersCmd(providerConfig(t, script))
	if cmd == nil {
		t.Fatal("no describe command was produced")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		cmd()
	}()

	// Give the command every chance to misbehave.
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a provider process ran before the first ready frame")
	}
	if ResourceProviderManager() != nil {
		t.Fatal("the provider manager was built before the first ready frame")
	}

	latch.close()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the describe command never finished after the latch opened")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the provider never ran after the first ready frame")
	}
	if ResourceProviderManager() == nil {
		t.Fatal("the manager was not published")
	}
}

// Shutdown while the command is still waiting must stop it without ever
// starting a provider.
func TestShutdownBeforeTheFirstFrameStartsNothing(t *testing.T) {
	features.Init(config.Default())
	features.SetOverride(features.TerminalResourceProviders.Name, true)
	t.Cleanup(func() { features.SetOverride(features.TerminalResourceProviders.Name, false) })

	marker := filepath.Join(t.TempDir(), "provider-ran")
	script := filepath.Join(t.TempDir(), "provider.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	latch := newReadyLatch()
	restore := firstReadyFrameLatch
	firstReadyFrameLatch = latch
	t.Cleanup(func() {
		firstReadyFrameLatch = restore
		resourceProviderHost.mu.Lock()
		resourceProviderHost.manager = nil
		resourceProviderHost.mu.Unlock()
	})

	cmd := describeResourceProvidersCmd(providerConfig(t, script))
	done := make(chan struct{})
	go func() {
		defer close(done)
		cmd()
	}()

	time.Sleep(100 * time.Millisecond)
	ShutdownResourceProviders()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not release the waiting command")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a provider ran after shutdown")
	}
}

// A provider child is spawned with providerWorkingDir as its cwd, and a process
// whose cwd does not exist does not start at all. On a fresh install the config
// directory has not been created yet, so a working directory that is merely
// "where config would live" silently disables every provider on exactly the
// machines least able to diagnose it. The directory handed out must always
// exist.
func TestProviderWorkingDirExistsWithoutAConfigDirectory(t *testing.T) {
	// A home with no Sidecar config directory in it: the fresh-install case.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "absent"))

	dir := providerWorkingDir()
	if dir == "" {
		t.Fatal("no working directory was chosen, so the child inherits Sidecar's cwd — the selected repository")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("provider working directory %q does not exist, so no provider can spawn: %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("provider working directory %q is not a directory", dir)
	}
}

// resourceSurfacePlugin is a plugin that shows resource panes. It reports the
// work injection started, which is the whole contract publishResourceProviders
// depends on.
type resourceSurfacePlugin struct {
	navigationPlugin
	matchers []terminallink.ResourceMatcher
	resolver resourceview.Resolver
	waiting  bool
	resolved bool
}

func (p *resourceSurfacePlugin) SetResourceMatchers(m []terminallink.ResourceMatcher) { p.matchers = m }

func (p *resourceSurfacePlugin) SetResourceResolver(resolve resourceview.Resolver) tea.Cmd {
	p.resolver = resolve
	if !p.waiting {
		return nil
	}
	p.waiting = false
	return func() tea.Msg {
		p.resolved = true
		return nil
	}
}

// Readiness is the only moment a tab armed at restore can finally load, and the
// command that loads it has to be returned all the way to the runtime. Dropping
// it is what left restored ticket panes saying "waiting for a provider" that
// was, by then, ready.
func TestPublishingProvidersReturnsTheWorkItStarted(t *testing.T) {
	surface := &resourceSurfacePlugin{navigationPlugin: navigationPlugin{id: "workspaces"}, waiting: true}
	registry := plugin.NewRegistry(nil)
	if err := registry.Register(surface); err != nil {
		t.Fatal(err)
	}
	m := New(registry, keymap.NewRegistry(), config.Default(), "", "/tmp/one", "/tmp/one", "workspaces")

	manager := pluginhost.NewManager(pluginhost.ManagerOptions{Log: slog.Default()})
	resourceProviderHost.mu.Lock()
	previous := resourceProviderHost.manager
	resourceProviderHost.manager = manager
	resourceProviderHost.mu.Unlock()
	t.Cleanup(func() {
		resourceProviderHost.mu.Lock()
		resourceProviderHost.manager = previous
		resourceProviderHost.mu.Unlock()
	})

	cmd := m.publishResourceProviders()
	if surface.resolver == nil {
		t.Fatal("the surface was never given a resolver")
	}
	if cmd == nil {
		t.Fatal("the surface's waiting tab produced no work, or the work was dropped")
	}
	cmd()
	if !surface.resolved {
		t.Fatal("the returned command did not carry the surface's load")
	}
}

// externalPluginConfig configures one plugins.external entry.
func externalPluginConfig(argv0 string) *config.Config {
	cfg := config.Default()
	cfg.Plugins.External = []config.PluginInstanceConfig{
		{ID: "fixture", Command: []string{argv0}, Enabled: true, Scope: config.PluginScopeGlobal},
	}
	return cfg
}

// Each section has its own flag. Turning the plugin protocol off must not take a
// working terminal resource provider down with it, and turning it on must not
// resurrect providers whose own flag is off.
func TestEachPluginSectionIsGatedByItsOwnFlag(t *testing.T) {
	features.Init(config.Default())
	t.Cleanup(func() {
		features.SetOverride(features.TerminalResourceProviders.Name, false)
		features.SetOverride(features.PluginProtocol.Name, false)
	})

	features.SetOverride(features.TerminalResourceProviders.Name, false)
	features.SetOverride(features.PluginProtocol.Name, false)
	if cmd := describeResourceProvidersCmd(externalPluginConfig("/bin/echo")); cmd != nil {
		t.Fatal("an external plugin produced a describe command with plugin_protocol off")
	}

	features.SetOverride(features.PluginProtocol.Name, true)
	if cmd := describeResourceProvidersCmd(externalPluginConfig("/bin/echo")); cmd == nil {
		t.Fatal("an external plugin produced no describe command with plugin_protocol on")
	}

	// Two distinct IDs: an ID configured in both sections is one plugin, and
	// that collapse is a different rule than this one.
	cfg := providerConfig(t, "/bin/echo")
	cfg.Plugins.External = []config.PluginInstanceConfig{
		{ID: "recall", Command: []string{"/bin/echo"}, Enabled: true, Scope: config.PluginScopeGlobal},
	}
	features.SetOverride(features.TerminalResourceProviders.Name, false)
	got := enabledPluginInstances(cfg, false, true)
	if len(got) != 1 || got[0].Source != config.PluginSourceExternal {
		t.Fatalf("instances = %+v, want only the plugins.external entry", got)
	}
	features.SetOverride(features.TerminalResourceProviders.Name, true)
	got = enabledPluginInstances(cfg, true, false)
	if len(got) != 1 || got[0].Source != config.PluginSourceTerminalResources {
		t.Fatalf("instances = %+v, want only the terminalResources entry", got)
	}
	if got = enabledPluginInstances(cfg, true, true); len(got) != 2 {
		t.Fatalf("instances = %+v, want both sections", got)
	}
}

// An external plugin waits on exactly the same latch a resource provider does:
// nothing about plugins.external may spawn, LookPath, or read the section
// before the first ready frame.
func TestExternalPluginWaitsForTheFirstReadyFrame(t *testing.T) {
	features.Init(config.Default())
	features.SetOverride(features.PluginProtocol.Name, true)
	t.Cleanup(func() { features.SetOverride(features.PluginProtocol.Name, false) })

	marker := filepath.Join(t.TempDir(), "plugin-ran")
	script := filepath.Join(t.TempDir(), "plugin.sh")
	body := "#!/bin/sh\ntouch " + marker + "\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	latch := newReadyLatch()
	restore := firstReadyFrameLatch
	firstReadyFrameLatch = latch
	t.Cleanup(func() {
		firstReadyFrameLatch = restore
		ShutdownResourceProviders()
		resourceProviderHost.mu.Lock()
		resourceProviderHost.manager = nil
		resourceProviderHost.mu.Unlock()
	})

	cmd := describeResourceProvidersCmd(externalPluginConfig(script))
	if cmd == nil {
		t.Fatal("no describe command was produced")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		cmd()
	}()

	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a plugin process ran before the first ready frame")
	}
	if ResourceProviderManager() != nil {
		t.Fatal("the manager was built before the first ready frame")
	}

	latch.close()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the describe command never finished after the latch opened")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the plugin never ran after the first ready frame")
	}
}
