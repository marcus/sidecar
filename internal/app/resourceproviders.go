package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/pluginbrowser"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/startuptrace"
)

// External plugins — terminal resource providers and protocol plugins alike —
// must never enter the first-frame path. Not plugin.Init, not config loading,
// not app construction, not a render path: no LookPath, no subprocess, no
// plugin config read, no network before the first frame is on screen.
//
// Bubble Tea's command timing is not a strong enough guarantee for that. A
// command returned from Init can start running before the first render, so
// "start it from Init" would mean "start it at some point that is usually, but
// not always, after the frame". The app therefore owns an explicit one-shot
// latch, closes it from the same branch of View that marks `first ready frame`,
// and the describe command's first act is to wait on it.
//
// The consequence is worth stating: a provider that never becomes ready simply
// contributes no matcher. Nothing about startup waits for one.

// readyLatch is closed exactly once, by the first ready frame.
type readyLatch struct {
	once sync.Once
	ch   chan struct{}
}

func newReadyLatch() *readyLatch { return &readyLatch{ch: make(chan struct{})} }

func (l *readyLatch) close() { l.once.Do(func() { close(l.ch) }) }

func (l *readyLatch) wait() <-chan struct{} { return l.ch }

// firstReadyFrameLatch is package-level for the same reason firstReadyFrame is:
// View has a value receiver, so it cannot store anything, and the latch has to
// outlive every copy of the model.
var firstReadyFrameLatch = newReadyLatch()

// resourceProviderHost owns the manager and the lifetime of the work started
// after the latch opens. It is created lazily, inside the command, so nothing
// about it exists during construction or rendering.
var resourceProviderHost struct {
	mu      sync.Mutex
	manager *pluginhost.Manager
	cancel  context.CancelFunc
	// ctx is the lifetime resolves hang off, so shutdown cancels them.
	ctx context.Context
}

// ResourceProvidersDescribedMsg reports the outcome of a describe pass. In M0
// it is diagnostics only: nothing in the TUI changes shape because of it.
type ResourceProvidersDescribedMsg struct {
	Statuses []pluginhost.Status
	// SnapshotError is the error from a refused snapshot replacement, if any.
	// The previous snapshot stays live when this is set.
	SnapshotError error
}

// ResourceProviderManager returns the live manager, or nil before the first
// describe pass has started. M1 injects its read-only snapshot and Resolve into
// both workspace surfaces through this.
func ResourceProviderManager() *pluginhost.Manager {
	resourceProviderHost.mu.Lock()
	defer resourceProviderHost.mu.Unlock()
	return resourceProviderHost.manager
}

// ShutdownResourceProviders cancels any provider work still in flight. Queued
// work is cancellable, and an invocation in progress has its process group
// killed, so nothing survives the app.
func ShutdownResourceProviders() {
	resourceProviderHost.mu.Lock()
	cancel := resourceProviderHost.cancel
	resourceProviderHost.cancel = nil
	resourceProviderHost.ctx = nil
	resourceProviderHost.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// describeResourceProvidersCmd returns the command that describes every
// configured provider — after the first ready frame, never before it.
//
// It returns nil when there is nothing to do, so the ordinary case adds no
// goroutine and no waiting command at all.
func describeResourceProvidersCmd(cfg *config.Config) tea.Cmd {
	if cfg == nil {
		return nil
	}
	resources := features.IsEnabled(features.TerminalResourceProviders.Name)
	protocol := features.IsEnabled(features.PluginProtocol.Name)
	if !resources && !protocol {
		return nil
	}
	// Reading the already-parsed config struct is not I/O, so the decision costs
	// nothing here: this is a walk of at most sixteen entries, and it must be a
	// per-section one rather than "is anything configured at all". Both flags
	// default on, so a user with providers in only one section and that
	// section's flag off would otherwise get a goroutine that waits on the latch
	// to discover it has nothing to run.
	if len(enabledPluginInstances(cfg, resources, protocol)) == 0 {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())

		resourceProviderHost.mu.Lock()
		if prev := resourceProviderHost.cancel; prev != nil {
			prev()
		}
		resourceProviderHost.cancel = cancel
		resourceProviderHost.ctx = ctx
		resourceProviderHost.mu.Unlock()

		select {
		case <-firstReadyFrameLatch.wait():
		case <-ctx.Done():
			return nil
		}

		defer startuptrace.Begin("terminal resource providers: describe")()

		// Each flag gates its own section. terminal_resource_providers keeps
		// answering for the frozen protocol whatever the plugin flag says, so
		// turning the plugin protocol off cannot take a working Jira provider
		// down with it.
		instances := enabledPluginInstances(cfg, resources, protocol)
		if len(instances) == 0 {
			return nil
		}

		providers, disabled, err := pluginhost.FromInstances(instances, pluginhost.Options{
			Dir: providerWorkingDir(),
			Log: slog.Default(),
		})
		if err != nil {
			slog.Warn("external plugins: configuration refused", "error", err)
			return ResourceProvidersDescribedMsg{}
		}

		manager := pluginhost.NewManager(pluginhost.ManagerOptions{Log: slog.Default()})
		manager.SetProviders(providers, disabled)

		resourceProviderHost.mu.Lock()
		resourceProviderHost.manager = manager
		resourceProviderHost.mu.Unlock()

		statuses := manager.DescribeAll(ctx)
		if ctx.Err() != nil {
			return nil
		}
		return ResourceProvidersDescribedMsg{Statuses: statuses, SnapshotError: manager.SnapshotError()}
	}
}

// enabledPluginInstances filters the merged instance list to the sections whose
// feature flag is on. It reads only the already-parsed config struct.
func enabledPluginInstances(cfg *config.Config, resources, protocol bool) []config.PluginInstance {
	all := cfg.PluginInstances()
	out := make([]config.PluginInstance, 0, len(all))
	for _, instance := range all {
		if instance.IsLegacyResourceProvider() {
			if resources {
				out = append(out, instance)
			}
			continue
		}
		if protocol {
			out = append(out, instance)
		}
	}
	return out
}

// publishResourceProviders hands every surface the live matcher snapshot and a
// resolver over the manager. It is called after a describe pass, which is the
// only moment either can change.
//
// Matchers are published even when empty: a provider that stopped declaring a
// matcher must stop underlining it, and an empty snapshot is how ordinary
// terminal output goes back to being ordinary text.
//
// The returned command is what a restored tab has been waiting for. Restore
// arms references without resolving them, and until this runs there is no
// resolver to ask, so the surfaces hand back the first load for whatever
// Resource pane is on screen.
func (m *Model) publishResourceProviders() tea.Cmd {
	// The project every protocol plugin is asked about is republished here for
	// the same reason the matchers are: a global plugin outlives every project
	// switch, so the context it was constructed with is the wrong answer the
	// moment the user moves. It is published even when there is no manager,
	// because the browser reads it on its first list rather than at
	// construction.
	m.publishPluginBrowserProject()

	manager := ResourceProviderManager()
	if manager == nil {
		return nil
	}
	matchers := manager.Snapshot().TerminalMatchers()
	resolve := resourceResolver(manager)

	var cmds []tea.Cmd
	for _, surface := range m.resourceSurfaces() {
		surface.SetResourceMatchers(matchers)
		if cmd := surface.SetResourceResolver(resolve); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// The collection shape's half of the same injection. It is behind the
		// flag rather than the manager: a Resource leaf must keep resolving
		// matched documents when the plugin protocol is off, and a collection tab
		// that could not exist has nothing to bind.
		plugins, ok := surface.(resourceview.PluginSurface)
		if !ok || !features.IsEnabled(features.PluginProtocol.Name) {
			continue
		}
		if cmd := plugins.SetPluginCalls(pluginBrowserCalls); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Every protocol browser is waiting on exactly this: a describe pass has
	// settled, so its own snapshot is worth re-reading. It is delivered here
	// rather than returned as a message, so the batch grows only when a browser
	// actually has work to do — a surface with no protocol plugin configured
	// still hands back exactly the one command its waiting tab produced.
	if cmd := m.updateGlobalHosts(pluginbrowser.DescribedMsg{}); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// resourceSurfaces is every surface currently able to show a resource pane.
func (m *Model) resourceSurfaces() []resourceview.Surface {
	var out []resourceview.Surface
	for _, deck := range m.contentDecks {
		out = append(out, deck)
	}
	if m.registry != nil {
		for _, p := range m.registry.Plugins() {
			if surface, ok := p.(resourceview.Surface); ok {
				out = append(out, surface)
			}
		}
	}
	if m.overview != nil {
		out = append(out, m.overview)
	}
	return out
}

// resourceResolver turns a reference into a command that resolves it through
// the manager. The manager owns dedupe, caching, concurrency limits, the
// per-provider timeout, and the process group; this only carries the identity
// fields back so a late answer can be matched to the tab that asked.
//
// The work happens inside the returned command, never in Update or View: that
// is what keeps an external process off the render path.
func resourceResolver(manager *pluginhost.Manager) resourceview.Resolver {
	return func(modelID int, generation, epoch uint64, ref resource.Reference, refresh bool) tea.Cmd {
		return func() tea.Msg {
			msg := resourceview.ResolvedMsg{
				ModelID: modelID, Generation: generation, Epoch: epoch,
				Ref: ref, Refresh: refresh,
			}
			ctx := resourceProviderContext()
			doc, err := manager.Resolve(ctx, ref, refresh)
			if err != nil {
				msg.Err = err
				return msg
			}
			msg.Document = doc
			return msg
		}
	}
}

// resourceProviderContext is the lifetime every resolve hangs off, so app
// shutdown cancels in-flight provider work rather than leaving a child behind.
func resourceProviderContext() context.Context {
	resourceProviderHost.mu.Lock()
	defer resourceProviderHost.mu.Unlock()
	if resourceProviderHost.ctx != nil {
		return resourceProviderHost.ctx
	}
	return context.Background()
}

// logResourceProviderStatuses records one metadata-only line per instance.
func logResourceProviderStatuses(msg ResourceProvidersDescribedMsg) {
	if msg.SnapshotError != nil {
		// A refused replacement keeps the previous snapshot live, so this is a
		// warning about what did not change, not about links disappearing.
		slog.Warn("terminal resource providers: matcher snapshot refused, keeping the previous one",
			"error", msg.SnapshotError)
	}
	for _, st := range msg.Statuses {
		attrs := []any{
			"instance", st.Instance,
			"state", string(st.State),
			"matchers", st.MatcherCount,
			"duration_ms", st.Duration.Milliseconds(),
			"outcome", st.LastOutcome,
		}
		if st.LastError != nil {
			attrs = append(attrs, "code", string(st.LastError.Code))
		}
		slog.Debug("terminal resource provider status", attrs...)
	}
}

// providerWorkingDir is the neutral directory every provider child runs in: the
// Sidecar config directory, never the selected repository. It is read, not
// created — making directories from a background command is not this command's
// job.
//
// But "not created" cannot mean "not usable". Until config has been saved at
// least once the config directory does not exist yet, and a child whose cwd is
// missing does not merely report a problem — it fails to spawn at all, which
// would silently disable every provider on a fresh install. So a missing
// directory falls back to the system temp directory, which is equally neutral,
// is never the selected repository, and always exists.
func providerWorkingDir() string {
	path := config.ConfigPath()
	if path == "" {
		return os.TempDir()
	}
	dir := filepath.Dir(path)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return os.TempDir()
	}
	return dir
}
