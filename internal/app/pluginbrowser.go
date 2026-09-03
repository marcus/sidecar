package app

import (
	"os"
	"path/filepath"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/pluginbrowser"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/uirequest"
)

// The protocol half of the global tab row. A configured plugin that declares a
// tab placement is a descriptor like any other, and the shell builds the same
// globalPluginHost around it that Tasks gets — which is the whole point of M1's
// generalization: the second global plugin is a config entry, not an enum value.

// pluginBrowserProject is the project context on offer to protocol plugins.
//
// It is package-level for the same reason resourceProviderHost is: a global
// plugin outlives every project switch, so its context cannot be the one it was
// constructed with. The app republishes this from the same place it republishes
// matchers and the resolver, which is the only moment either can change.
var pluginBrowserProject atomic.Pointer[pluginhost.ProjectContext]

// globalProtocolDescriptors is every configured protocol plugin that declares a
// tab placement, in configuration order, behind the plugin_protocol flag.
//
// It reads only the already-parsed config struct and constructs nothing, so it
// is safe on the first-frame path: the browser it names renders a loading state
// until the host's describe pass lands.
func globalProtocolDescriptors(cfg *config.Config) []plugin.Descriptor {
	if !features.IsEnabled(features.PluginProtocol.Name) {
		return nil
	}
	all := pluginbrowser.TabDescriptors(cfg, pluginBrowserCalls)
	out := make([]plugin.Descriptor, 0, len(all))
	for _, d := range all {
		if d.Scope != plugin.ScopeGlobal || !d.IsEnabled(cfg) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// pluginBrowserCalls is the seam the browser asks for work through. Every call
// runs inside the returned command, never in Update or View, which is what
// keeps an external process off the render path.
func pluginBrowserCalls(instance string) pluginbrowser.Calls {
	return pluginbrowser.Calls{
		Describe: func(id string) (pluginhost.Description, pluginhost.Status, bool) {
			manager := ResourceProviderManager()
			if manager == nil {
				return pluginhost.Description{}, pluginhost.Status{}, false
			}
			status, ok := manager.Status(id)
			if !ok {
				return pluginhost.Description{}, pluginhost.Status{}, false
			}
			desc, _ := manager.Description(id)
			return desc, status, true
		},
		List: func(call pluginbrowser.ListCall) tea.Cmd {
			return func() tea.Msg {
				msg := pluginbrowser.ListedMsg{
					Instance:   call.Instance,
					Browser:    call.Browser,
					Collection: call.Params.Collection,
					Generation: call.Generation,
					Append:     call.Append,
				}
				manager := ResourceProviderManager()
				if manager == nil {
					msg.Err = resource.Errorf(resource.CodeUnavailable, "no plugin host is running yet")
					return msg
				}
				page, err := manager.List(resourceProviderContext(), pluginhost.ListRequest{
					Instance: call.Instance,
					Params:   call.Params,
					Context:  call.Context,
					PaneKey:  call.PaneKey,
				})
				msg.Page, msg.Err = page, err
				return msg
			}
		},
		Get: func(call pluginbrowser.GetCall) tea.Cmd {
			return func() tea.Msg {
				msg := pluginbrowser.GotMsg{
					Instance:   call.Instance,
					Browser:    call.Browser,
					Collection: call.Params.Collection,
					ID:         call.Params.ID,
					Generation: call.Generation,
				}
				manager := ResourceProviderManager()
				if manager == nil {
					msg.Err = resource.Errorf(resource.CodeUnavailable, "no plugin host is running yet")
					return msg
				}
				doc, err := manager.Get(resourceProviderContext(), pluginhost.GetRequest{
					Instance: call.Instance,
					Params:   call.Params,
					Context:  call.Context,
					Refresh:  call.Refresh,
				})
				msg.Document, msg.Err = doc, err
				return msg
			}
		},
		Act: func(call pluginbrowser.ActCall) tea.Cmd {
			return func() tea.Msg {
				msg := pluginbrowser.ActedMsg{
					Instance:   call.Instance,
					Browser:    call.Browser,
					Action:     call.Params.Action,
					Generation: call.Generation,
				}
				manager := ResourceProviderManager()
				if manager == nil {
					msg.Err = resource.Errorf(resource.CodeUnavailable, "no plugin host is running yet")
					return msg
				}
				outcome, err := manager.Act(resourceProviderContext(), pluginhost.ActRequest{
					Instance: call.Instance,
					Params:   call.Params,
					Context:  call.Context,
				})
				msg.Outcome, msg.Err = outcome, err
				return msg
			}
		},
		Cancel: func(paneKey string) {
			if manager := ResourceProviderManager(); manager != nil {
				manager.CancelPane(paneKey)
			}
		},
		OpenURL: func(url string) tea.Cmd {
			safe, ok := contentlink.SafeHTTPURL(url)
			if !ok {
				return nil
			}
			return openPathCmd(safe)
		},
		Context: pluginBrowserContext,
	}
}

// pluginBrowserContext is the host context on offer. It carries only kinds the
// host boundary will narrow to what the plugin declared, so offering it is not
// granting it.
func pluginBrowserContext() *pluginhost.Context {
	project := pluginBrowserProject.Load()
	if project == nil {
		return nil
	}
	copied := *project
	return &pluginhost.Context{Project: &copied}
}

// publishPluginBrowserProject records the project every protocol plugin is
// asked about. On a remote-bound surface it carries that host's ID and that
// host's paths, so a plugin that only knows this machine can refuse naming the
// host rather than answer about the wrong checkout.
func (m *Model) publishPluginBrowserProject() {
	if m.registry == nil {
		pluginBrowserProject.Store(nil)
		return
	}
	ctx := m.registry.Context()
	if ctx == nil || (ctx.WorkDir == "" && ctx.ProjectRoot == "") {
		pluginBrowserProject.Store(nil)
		return
	}
	root := ctx.ProjectRoot
	if root == "" {
		root = ctx.WorkDir
	}
	pluginBrowserProject.Store(&pluginhost.ProjectContext{
		Root:    root,
		WorkDir: ctx.WorkDir,
		Name:    filepath.Base(root),
		HostID:  ctx.HostID,
	})
}

// handlePluginChangedRequest answers `sidecar plugin changed` on the request
// bus: every visible tab of the named plugin re-lists.
//
// It broadcasts rather than addressing a surface. A plugin's data does not
// belong to a project or a workspace row, so "which surface" is not a question
// the request can answer; each browser decides whether it was the one being
// talked about, exactly as it does for a describe pass.
func (m *Model) handlePluginChangedRequest(req uirequest.Request) tea.Cmd {
	payload, err := uirequest.DecodePluginChangedPayload(req.Payload)
	status, reason := uirequest.StatusOpened, ""
	if err != nil {
		status, reason = uirequest.StatusDeclined, err.Error()
	} else if !features.IsEnabled(features.PluginProtocol.Name) {
		status, reason = uirequest.StatusDeclined, "plugin_protocol is off in this instance"
	}
	_ = uirequest.WriteAck(config.StateDir(), req.ID, req.Action, uirequest.Ack{
		Instance: uirequest.InstanceID("app"),
		Host:     uirequest.HostName(),
		PID:      os.Getpid(),
		Status:   status,
		Reason:   reason,
		Surface:  "plugins",
	})
	if status != uirequest.StatusOpened {
		return nil
	}
	changed := pluginbrowser.ChangedMsg{Instance: payload.Instance, Collection: payload.Collection}
	cmds := []tea.Cmd{func() tea.Msg { return changed }}
	// The global tabs are hosted here rather than on the message bus, so they
	// are handed the message directly the way a describe pass is.
	if cmd := m.updateGlobalHosts(changed); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}
