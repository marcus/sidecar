package workspace

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/filefind"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/panelpref"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspaceops"
)

// notesPluginPresent asks the same function the Notes descriptor's own Enabled
// does, so the Note row exists exactly when the Notes plugin does. It used to
// restate the rule here, which is how a surface drifts from the descriptor.
func (p *Plugin) notesPluginPresent() bool {
	if p.ctx == nil || p.ctx.Config == nil {
		return false
	}
	return panelpref.Notes(p.ctx.Config)
}

// configuredProviders is one kind row per enabled terminal-resource provider.
func (p *Plugin) configuredProviders() []workspacecreate.ProviderItem {
	if p.ctx == nil || p.ctx.Config == nil {
		return nil
	}
	var items []workspacecreate.ProviderItem
	for _, provider := range p.ctx.Config.TerminalResources.EnabledProviders() {
		items = append(items, workspacecreate.ProviderItem{ID: provider.ID})
	}
	return items
}

// createPickerDataMsg lands the switcher's suggestion sources in one round
// trip. Files travel separately: their scan can take whole seconds on a large
// tree, and the refs/issues/notes answers must not wait behind it.
type createPickerDataMsg struct {
	Refs   []workspaceops.DiffRef
	Issues []workspaceops.IssueRef
	Notes  []workspaceops.NoteRef
	Err    error
}

// loadCreatePickerData fetches everything the target pickers offer except the
// file list. It runs in a command, never before the first frame.
func (p *Plugin) loadCreatePickerData() tea.Cmd {
	if p.ctx == nil || p.ctx.WorkDir == "" {
		return nil
	}
	workDir := p.ctx.WorkDir
	wantNotes := p.notesPluginPresent()
	return func() tea.Msg {
		ctx := context.Background()
		msg := createPickerDataMsg{}
		if refs, err := workspaceops.RecentDiffRefs(ctx, workDir, 15); err == nil {
			msg.Refs = refs
		}
		if issues, err := workspaceops.RecentIssues(ctx, workDir, 30); err == nil {
			msg.Issues = issues
		}
		if wantNotes {
			if notes, err := workspaceops.RecentNotes(ctx, workDir, 20); err == nil {
				msg.Notes = notes
			}
		}
		return msg
	}
}

// loadCreateFileCandidates scans the workspace's files for the File picker,
// gitignore-aware, off the update path.
func (p *Plugin) loadCreateFileCandidates() tea.Cmd {
	if p.ctx == nil || p.ctx.WorkDir == "" {
		return nil
	}
	root := p.ctx.WorkDir
	return func() tea.Msg {
		paths, _ := filefind.ScanPaths(root, false)
		return workspacecreate.FilesScannedMsg{Root: root, Paths: paths}
	}
}

// applyCreatePickerData folds loaded suggestions into the form through the
// shared folds, so this surface and the Sessions browser cannot disagree
// about what a suggestion row resolves from.
func (p *Plugin) applyCreatePickerData(msg createPickerDataMsg) {
	if p.createForm == nil {
		return
	}
	p.createForm.SetDiffRefs(workspacecreate.FoldDiffRefs(msg.Refs))
	p.createForm.SetIssues(workspacecreate.FoldIssues(msg.Issues))
	p.createForm.SetNotes(workspacecreate.FoldNotes(msg.Notes))
}

// applyCreateFileCandidates orders the scan so what is already open in a
// document pane leads — the recents the surface genuinely knows about — and
// hands the rest to the picker as-is.
func (p *Plugin) applyCreateFileCandidates(msg workspacecreate.FilesScannedMsg) {
	if p.createForm == nil || p.ctx == nil || msg.Root != p.ctx.WorkDir {
		return
	}
	recent := make([]string, 0, 8)
	seen := make(map[string]bool, len(msg.Paths)+8)
	for _, doc := range p.docs {
		if doc == nil {
			continue
		}
		for _, item := range doc.tabs.Items {
			if item.View == nil {
				continue
			}
			rel := docview.NormalizeTabPath(item.View.Title())
			if rel == "" || seen[rel] {
				continue
			}
			seen[rel] = true
			recent = append(recent, rel)
		}
	}
	candidates := make([]string, 0, len(recent)+len(msg.Paths))
	candidates = append(candidates, recent...)
	for _, path := range msg.Paths {
		if !seen[path] {
			seen[path] = true
			candidates = append(candidates, path)
		}
	}
	p.createForm.SetFileCandidates(candidates)
}

// submitPaneTargetForm resolves the picker step's answer and opens it through
// the same per-surface path `sidecar open` uses, with the clicked placement as
// the axis override. A resolution failure keeps the modal up with the reason;
// an open that was declined after the fact reports through the toast.
func (p *Plugin) submitPaneTargetForm() tea.Cmd {
	if p.createForm == nil || p.ctx == nil {
		return nil
	}
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		p.setCreateError("Select a terminal to open beside")
		return nil
	}
	target, err := p.createForm.TargetFor(p.ctx.WorkDir)
	if err != nil {
		p.setCreateError(err.Error())
		return nil
	}
	placement := p.createForm.PlacementSplit()
	p.viewMode = ViewModeList
	p.clearCreateModal()
	req := uirequest.Request{Target: target, Options: uirequest.Options{Split: placement}}
	outcome, cmd := p.performTargetOpen(req, root, surface)
	if outcome.status == uirequest.StatusDeclined && outcome.reason != "" {
		return appmsg.ShowFlash(outcome.reason)
	}
	return cmd
}
