package overview

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/contentservice"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/filefind"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/panelpref"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspacediff"
	"github.com/marcus/sidecar/internal/workspaceops"
)

// notesWanted asks the same function the Notes descriptor's own Enabled does,
// so the Note row exists exactly when the Notes plugin does. It used to restate
// the rule here, which is how a surface drifts from the descriptor.
func (m *Model) notesWanted() bool {
	if m.config == nil {
		return false
	}
	return panelpref.Notes(m.config)
}

// configuredProviders is one kind row per enabled terminal-resource provider.
func (m *Model) configuredProviders() []workspacecreate.ProviderItem {
	if m.remoteSelection() {
		return m.remoteCreateProviders()
	}
	if m.config == nil {
		return nil
	}
	var items []workspacecreate.ProviderItem
	for _, provider := range m.config.TerminalResources.EnabledProviders() {
		items = append(items, workspacecreate.ProviderItem{ID: provider.ID})
	}
	return items
}

func (m *Model) remoteCreateProviders() []workspacecreate.ProviderItem {
	ws, ok := m.SelectedWorkspace()
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var items []workspacecreate.ProviderItem
	for _, matcher := range m.remoteMatchersFor(ws) {
		id := strings.TrimSpace(matcher.Provider)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		items = append(items, workspacecreate.ProviderItem{ID: id})
	}
	return items
}

type createPickerDataMsg struct {
	Refs   []workspaceops.DiffRef
	Issues []workspaceops.IssueRef
	Notes  []workspaceops.NoteRef
}

type createHostCatalogMsg struct {
	HostID      string
	WorkspaceID string
	Files       []string
	Refs        []workspaceops.DiffRef
	Issues      []workspaceops.IssueRef
	Notes       []workspaceops.NoteRef
	Err         error
}

func (m *Model) loadCreatePickerData() tea.Cmd {
	if m.remoteSelection() {
		if m.createForm == nil || !m.createForm.NeedsTarget() {
			return nil
		}
		return m.loadRemoteCreateCatalog()
	}
	root := m.localSelectedRoot()
	if root == "" && len(m.projects) > 0 {
		root = m.projects[0].Path
	}
	if root == "" {
		return nil
	}
	wantNotes := m.notesWanted()
	dir := root
	return func() tea.Msg {
		ctx := context.Background()
		msg := createPickerDataMsg{}
		if refs, err := workspaceops.RecentDiffRefs(ctx, dir, 15); err == nil {
			msg.Refs = refs
		}
		if issues, err := workspaceops.RecentIssues(ctx, dir, 30); err == nil {
			msg.Issues = issues
		}
		if wantNotes {
			if notes, err := workspaceops.RecentNotes(ctx, dir, 20); err == nil {
				msg.Notes = notes
			}
		}
		return msg
	}
}

func (m *Model) loadRemoteCreateCatalog() tea.Cmd {
	ws, ok := m.SelectedWorkspace()
	if !ok || !ws.Remote() {
		return nil
	}
	src := sourceContextFromWorkspace(ws, m.hostIncarnationFor(ws.HostID))
	if src.WorkspaceID == "" {
		return nil
	}
	hostID, workspaceID := ws.HostID, src.WorkspaceID
	rowID := ws.ID
	registry := m.hostRegistry
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), remoteQuickTimeout)
		defer cancel()
		var result contentservice.CatalogResult
		err := runRemoteSidecar(ctx, registry, hostID, []string{
			"content", "catalog", "--workspace", workspaceID, "--json",
		}, &result)
		msg := createHostCatalogMsg{HostID: hostID, WorkspaceID: rowID, Err: err}
		if err != nil || !result.ValidRemoteResult() {
			return msg
		}
		msg.Files = result.Files
		msg.Refs = catalogDiffsToRefs(result.Diffs)
		msg.Issues = catalogIssuesToRefs(result.Issues)
		msg.Notes = catalogNotesToRefs(result.Notes)
		return msg
	}
}

func catalogDiffsToRefs(diffs []contentservice.CatalogDiff) []workspaceops.DiffRef {
	out := make([]workspaceops.DiffRef, 0, len(diffs))
	for _, d := range diffs {
		out = append(out, workspaceops.DiffRef{Identity: d.Identity, Label: d.Label})
	}
	return out
}

func catalogIssuesToRefs(issues []contentservice.CatalogIssue) []workspaceops.IssueRef {
	out := make([]workspaceops.IssueRef, 0, len(issues))
	for _, issue := range issues {
		updated, _ := time.Parse(time.RFC3339, issue.Updated)
		out = append(out, workspaceops.IssueRef{ID: issue.ID, Title: issue.Title, Status: issue.Status, Updated: updated})
	}
	return out
}

func catalogNotesToRefs(notes []contentservice.CatalogNote) []workspaceops.NoteRef {
	out := make([]workspaceops.NoteRef, 0, len(notes))
	for _, note := range notes {
		updated, _ := time.Parse(time.RFC3339, note.Updated)
		out = append(out, workspaceops.NoteRef{ID: note.ID, Title: note.Title, Updated: updated})
	}
	return out
}

func (m *Model) applyCreateHostCatalog(msg createHostCatalogMsg) {
	if m.createForm == nil || msg.Err != nil {
		return
	}
	selected, ok := m.SelectedWorkspace()
	if !ok || selected.HostID != msg.HostID || selected.ID != msg.WorkspaceID {
		return
	}
	applyPickerData(m.createForm, createPickerDataMsg{Refs: msg.Refs, Issues: msg.Issues, Notes: msg.Notes})
	m.applyCreateFileCandidates(workspacecreate.FilesScannedMsg{Paths: msg.Files})
}

// localSelectedRoot is the selected row's directory, but only when that
// directory is on this machine.
//
// Everything these pickers do — git log, gh issue list, a filesystem scan —
// runs here. Handed a remote row's path they would walk a same-named directory
// on THIS machine and offer its commits, issues and files as if they belonged
// to the other one.
func (m *Model) localSelectedRoot() string {
	selected, ok := m.SelectedWorkspace()
	if !ok || selected.Remote() {
		return ""
	}
	return selected.Path
}

// remoteSelection reports that the selected row is on another machine.
//
// localSelectedRoot returns "" for both "nothing selected" and "selected
// elsewhere", and those two want opposite answers from a caller that has a
// local fallback: any project will do for the first, and none will do for the
// second.
func (m *Model) remoteSelection() bool {
	selected, ok := m.SelectedWorkspace()
	return ok && selected.Remote()
}

func (m *Model) loadCreateFileCandidates() tea.Cmd {
	if m.remoteSelection() {
		return nil
	}
	root := m.localSelectedRoot()
	if root == "" {
		return nil
	}
	dir := root
	return func() tea.Msg {
		paths, _ := filefind.ScanPaths(dir, false)
		return workspacecreate.FilesScannedMsg{Root: dir, Paths: paths}
	}
}

func applyPickerData(form *workspacecreate.Form, msg createPickerDataMsg) {
	if form == nil {
		return
	}
	// The folds are workspacecreate's, shared with the project surface —
	// Value carries what resolves, Label only what reads.
	form.SetDiffRefs(workspacecreate.FoldDiffRefs(msg.Refs))
	form.SetIssues(workspacecreate.FoldIssues(msg.Issues))
	form.SetNotes(workspacecreate.FoldNotes(msg.Notes))
}

func (m *Model) applyCreateFileCandidates(msg workspacecreate.FilesScannedMsg) {
	if m.createForm == nil {
		return
	}
	recent := make([]string, 0, 8)
	seen := make(map[string]bool)
	if doc := m.preview.doc; doc != nil {
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
	m.createForm.SetFileCandidates(candidates)
}

// openPreviewTarget opens one resolved target on this surface's preview —
// the whole per-kind body of the ui-request open path, minus acknowledgements,
// so the pane switcher cannot drift from what `sidecar open` does here.
// Caller owns m.openSplit. ok reports whether anything is on screen afterwards:
// an open that only focuses an existing tab legitimately returns no command.
func (m *Model) openPreviewTarget(target uirequest.Target) (tea.Cmd, bool) {
	onScreen := func(kind panelayout.Kind) bool {
		return m.preview.deck != nil && m.preview.deck.Leaf(kind) != 0
	}
	var cmd tea.Cmd
	switch target.Kind {
	case uirequest.TargetKindFile:
		cmd = m.openPreviewDocTarget(target)
		shown := false
		if doc := m.preview.doc; doc != nil {
			if view := doc.view(); view != nil {
				shown = docview.NormalizeTabPath(view.Title()) == docview.NormalizeTabPath(target.Value)
			}
		}
		return cmd, shown || onScreen(panelayout.Document)
	case uirequest.TargetKindIssue:
		cmd = m.openPreviewIssue(target.Value)
		return cmd, onScreen(panelayout.Issue)
	case uirequest.TargetKindNote:
		cmd = m.openPreviewNote(target.Value)
		return cmd, onScreen(panelayout.Note)
	case uirequest.TargetKindDiff:
		selected, ok := m.SelectedWorkspace()
		if ok && selected.Remote() {
			spec, parsed := workspacediff.ParseSpec(target.Value)
			if !parsed {
				spec = workspacediff.WorkingTreeTarget()
			}
			cmd = m.openPreviewDiff(spec)
			return cmd, onScreen(panelayout.Diff)
		}
		root := ""
		if ok {
			root = selected.Path
		}
		cmd = m.openPreviewDiff(uirequest.DiffTarget(root, target.Value))
		return cmd, onScreen(panelayout.Diff)
	case uirequest.TargetKindResource:
		ref, refusal := resourceview.ReferenceForLocator(m.previewResourceMatchers(), target.Provider, target.Value)
		if refusal != "" {
			return nil, false
		}
		cmd = m.OpenPreviewResource(ref)
		return cmd, onScreen(panelayout.Resource)
	default:
		return nil, false
	}
}

// submitPaneTargetForm resolves the picker step's answer and opens it through
// this surface's own open paths with the recorded placement as axis override.
func (m *Model) submitPaneTargetForm() tea.Cmd {
	if m.createForm == nil {
		return nil
	}
	selected, ok := m.SelectedWorkspace()
	if !ok {
		m.setCreateError("Select a workspace to open beside")
		return nil
	}
	var target uirequest.Target
	var err error
	if selected.Remote() {
		target, err = m.createForm.TargetForRemote()
	} else {
		target, err = m.createForm.TargetFor(selected.Path)
	}
	if err != nil {
		m.setCreateError(err.Error())
		return nil
	}
	placement := m.createForm.PlacementSplit()
	m.closeCreateShell()
	prevSplit := m.openSplit
	m.openSplit = placement
	defer func() { m.openSplit = prevSplit }()
	cmd, opened := m.openPreviewTarget(target)
	if !opened {
		return appmsg.ShowToast("the window is too small to split", 3*time.Second)
	}
	return cmd
}
