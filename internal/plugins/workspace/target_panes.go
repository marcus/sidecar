package workspace

import (
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/features"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/uirequest"
)

// The public pane entries. Opening an issue, a diff, a resource or a tmux
// session used to be reachable only through this plugin's private methods,
// which is why a third surface could not jump anywhere. These handlers are the
// message-shaped versions of exactly those paths — same surface selection, same
// pane placement, same feature gate — so the app shell, the notification
// centre, or a future CLI action can send one without importing this package.

func (p *Plugin) openIssuePaneMsg(msg app.OpenIssuePaneMsg) tea.Cmd {
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		return nil
	}
	return p.openIssuePaneForSurface(root, surface, msg.Issue)
}

func (p *Plugin) openDiffPaneMsg(msg app.OpenDiffPaneMsg) tea.Cmd {
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		return nil
	}
	if p.paneRoot == nil {
		return appmsg.ShowFlash(features.WorkspaceDocPanesDisabledDiff)
	}
	return p.openDiffPaneForSurface(root, surface, uirequest.DiffTarget(root, msg.Spec))
}

func (p *Plugin) openResourcePaneMsg(msg app.OpenResourcePaneMsg) tea.Cmd {
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		return nil
	}
	if msg.Collection != "" {
		// A plugin tab. No matcher is consulted: a collection and a row are
		// addressed by name, and there is no span for a matcher to have claimed.
		ref := resourceview.Ref{
			Instance: msg.Provider, Collection: msg.Collection,
			Query: msg.Query, Locator: msg.Locator,
			Filters: resource.DecodeFilters(msg.Filters),
		}
		if !ref.Valid() {
			return nil
		}
		return p.openRequestedResourcePaneForSurface(root, surface, ref)
	}
	ref := resourceview.Ref{Instance: msg.Provider, Matcher: msg.Matcher, Locator: msg.Locator}
	if msg.Matcher == "" {
		// No matcher named: only a live snapshot can say which one claims this
		// locator, and it refuses out loud when none does.
		resolved, refusal := resourceview.ReferenceForLocator(p.resourceMatchers, msg.Provider, msg.Locator)
		if refusal != "" {
			return appmsg.ShowFlash(refusal)
		}
		ref = resolved
	}
	if !ref.Valid() {
		return nil
	}
	return p.openRequestedResourcePaneForSurface(root, surface, ref)
}

// attachSessionMsg attaches a tmux session by name. Lookup, not creation: a
// name that matches no shell and no worktree agent attaches nothing, and the
// full-attach gate is honoured by the same helpers the keyboard paths use.
func (p *Plugin) attachSessionMsg(msg app.AttachSessionMsg) tea.Cmd {
	if !fullTmuxAttachEnabled() || msg.Session == "" {
		return nil
	}
	for _, shell := range p.shells {
		if shell != nil && shell.TmuxName == msg.Session {
			return p.ensureShellAndAttach(shell)
		}
	}
	for _, wt := range p.worktrees {
		if wt != nil && wt.Agent != nil && wt.Agent.TmuxSession == msg.Session {
			return p.AttachToSession(wt)
		}
	}
	return nil
}
