package workspace

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/hostproto"
	"github.com/marcus/sidecar/internal/issueview"
	"github.com/marcus/sidecar/internal/noteview"
	"github.com/marcus/sidecar/internal/panecodec"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/panereposition"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/workspacediff"
	"github.com/marcus/sidecar/internal/workspaceinventory"
)

func (p *Plugin) workspaceDeckContext(root, surface string) contentpanes.SurfaceContext {
	epoch := uint64(0)
	if p.ctx != nil {
		epoch = p.ctx.Epoch
	}
	return contentpanes.SurfaceContext{
		Root: root, DiffRoot: root, Surface: surface, DiffSurface: p.diffWorkspaceID(root, surface),
		BaseRef: p.selectedDiffBaseRef(), Epoch: epoch,
		Source: p.workspaceSourceContext(root, surface),
	}
}

func (p *Plugin) workspaceSourceContext(root, surface string) contentpanes.SourceContext {
	src := contentpanes.SourceContext{Root: root, WorkspaceID: surface}
	if p.ctx != nil {
		src.HostID = p.ctx.HostID
		src.HostIncarnation = p.ctx.HostIncarnation
		src.ProjectRoot = p.ctx.ProjectRoot
		src.ProjectKey = p.ctx.ProjectKey
		if src.ProjectKey == "" && !p.remoteBound() {
			src.ProjectKey = workspaceinventory.CanonicalPath(p.ctx.ProjectRoot)
		}
	}
	if p.selectingShell() {
		src.WorkspaceKind = workspaceinventory.KindShell
		if shell := p.getSelectedShell(); shell != nil {
			src.WorkspaceKey = shell.TmuxName
			if p.remoteBound() {
				src.WorkspaceID = remoteWorkspaceID(shell)
			}
		}
	} else if wt := p.selectedWorktree(); wt != nil {
		src.WorkspaceKind = workspaceinventory.KindWorktree
		src.WorkspaceKey = wt.Key
		if p.remoteBound() && p.ctx != nil && p.ctx.ProjectKey != "" && wt.Key != "" {
			src.WorkspaceID = p.ctx.ProjectKey + ":worktree:" + wt.Key
		}
	}
	return src
}

func remoteWorkspaceID(shell *ShellSession) string {
	if shell == nil {
		return ""
	}
	if shell.InventoryID != "" {
		return shell.InventoryID
	}
	return shell.TmuxName
}

func (p *Plugin) workspaceDeckConfig() contentpanes.Config {
	return contentpanes.Config{
		Renderer:         p.markdownRenderer,
		ResourceResolver: p.resolveResource,
		PluginCalls:      p.pluginCalls,
		ConfigureViewer:  p.configureDeckViewer,
		Source:           p.documentSource(),
	}
}

func (p *Plugin) documentSource() contentpanes.Source {
	if p.contentSource != nil {
		return p.contentSource
	}
	if !p.remoteBound() || p.ctx == nil || p.ctx.RemoteRunner == nil {
		return contentpanes.LocalSource{}
	}
	var verbs hostproto.VerbCapabilities
	if p.ctx.HostVerbs != nil {
		verbs = p.ctx.HostVerbs()
	}
	return contentpanes.NewRemoteSource(p.ctx.HostID, verbs, p.ctx.RemoteRunner)
}

func (p *Plugin) configureDeckViewer(kind panelayout.Kind, model any) {
	switch view := model.(type) {
	case *issueview.Model:
		view.OpenHandler = func(id string) tea.Cmd {
			ctx := p.contentDeck.Context()
			return p.openIssuePaneForSurface(ctx.Root, ctx.Surface, id)
		}
		view.OpenInTDHandler = app.OpenIssueInTD
		// Same cross-project fallback as every other host: this project's
		// config, read inside the fetch command.
		view.FallbackRefs = func() []issueview.ProjectRef {
			if p.ctx == nil {
				return nil
			}
			return issueview.ProjectRefsFromConfig(p.ctx.Config)
		}
	case *workspacediff.View:
		view.ViewMode = p.diff.ViewMode
		if w := state.GetDiffTabFileListWidth(); w > 0 {
			view.SetListWidth(w)
		}
		p.attachDiffPaintTo(view)
	}
}

func (p *Plugin) ensureWorkspaceDeck(root, surface string) (*contentpanes.Deck, []tea.Cmd) {
	ctx := p.workspaceDeckContext(root, surface)
	cfg := p.workspaceDeckConfig()
	hidden := p.hiddenPaneLayout
	if hidden != nil && hidden.Root == root && hidden.Surface == surface && paneLayoutHasRetainedTabs(hidden) {
		// The host's hidden snapshot owns exact wire-compatible geometry. Re-adopt
		// it before reopening so drag ratios and tabs survive both an in-process
		// hide and a relaunch.
		st, _ := panecodec.Decode(hidden, panecodec.Options{})
		p.contentDeck = contentpanes.Decode(ctx, cfg, st)
		return p.contentDeck, p.contentDeck.LoadVisible()
	}
	if p.contentDeck == nil {
		if saved := p.paneLayoutJSON(p.paneRoot); saved != nil {
			st, _ := panecodec.Decode(saved, panecodec.Options{})
			p.contentDeck = contentpanes.Decode(ctx, cfg, st)
			return p.contentDeck, p.contentDeck.LoadVisible()
		}
		p.contentDeck = contentpanes.New(ctx, cfg)
		return p.contentDeck, nil
	}
	return p.contentDeck, p.contentDeck.SetContext(ctx)
}

// paneLayoutJSON is the live tree's PaneLayoutJSON projection. Root, Surface,
// and Open are host policy and are filled by the persist path.
func (p *Plugin) paneLayoutJSON(node *PaneNode) *state.PaneLayoutJSON {
	if node == nil {
		return nil
	}
	st := contentpanes.State{Version: 1, Root: p.nodeState(node)}
	return panecodec.Encode(st, panecodec.Options{Live: p.liveRecordsFor(node)})
}

func (p *Plugin) liveRecordsFor(node *PaneNode) []panecodec.Live {
	live := []panecodec.Live{{Kind: panecodec.KindTerminal}}
	if firstPaneLeafOfKind(node, PaneShell) != nil {
		live = append(live, panecodec.Live{
			Kind:    panecodec.KindShell,
			Session: p.requireShellTermPane().Session,
			Name:    p.shellLeafName,
		})
	}
	return live
}

func (p *Plugin) nodeState(node *PaneNode) *contentpanes.NodeState {
	if node == nil {
		return nil
	}
	if node.Split != nil {
		axis := "columns"
		if node.Split.Axis == SplitRows {
			axis = "rows"
		}
		return &contentpanes.NodeState{
			Axis: axis, Ratio: node.Split.Ratio,
			A: p.nodeState(node.Split.A), B: p.nodeState(node.Split.B),
		}
	}
	switch node.Kind {
	case PaneTerminal:
		return &contentpanes.NodeState{Kind: "primary"}
	case PaneShell:
		return &contentpanes.NodeState{Kind: "shell"}
	case PaneDoc:
		return contentLeafState("document", p.paneState(panelayout.Document))
	case PaneIssue:
		return contentLeafState("issue", p.paneState(panelayout.Issue))
	case PaneNote:
		return contentLeafState("note", p.paneState(panelayout.Note))
	case PaneDiff:
		return contentLeafState("diff", p.paneState(panelayout.Diff))
	case PaneResource:
		return contentLeafState("resource", p.paneState(panelayout.Resource))
	default:
		return nil
	}
}

func contentLeafState(kind string, pane *contentpanes.PaneState) *contentpanes.NodeState {
	if pane == nil || len(pane.Tabs) == 0 {
		return nil
	}
	return &contentpanes.NodeState{Kind: kind, Pane: pane}
}

func (p *Plugin) paneState(kind panelayout.Kind) *contentpanes.PaneState {
	if p.contentDeck != nil {
		if pane := findPaneState(p.contentDeck.Encode().Root, contentKindName(kind)); pane != nil {
			return pane
		}
	}
	return p.paneStateFromMaps(kind)
}

func (p *Plugin) paneStateFromMaps(kind panelayout.Kind) *contentpanes.PaneState {
	switch kind {
	case panelayout.Document:
		for _, doc := range p.docs {
			if pane := docPaneState(doc); pane != nil {
				return pane
			}
		}
	case panelayout.Issue:
		for _, issue := range p.issues {
			if pane := issuePaneState(issue); pane != nil {
				return pane
			}
		}
	case panelayout.Note:
		for _, note := range p.notes {
			if pane := notePaneState(note); pane != nil {
				return pane
			}
		}
	case panelayout.Diff:
		for _, diff := range p.diffs {
			if pane := diffPaneState(diff); pane != nil {
				return pane
			}
		}
	case panelayout.Resource:
		for _, res := range p.resources {
			if pane := resourcePaneState(res); pane != nil {
				return pane
			}
		}
	}
	return nil
}

func docPaneState(doc *docPane) *contentpanes.PaneState {
	if doc == nil {
		return nil
	}
	pane := &contentpanes.PaneState{Kind: "document"}
	for i, item := range doc.tabs.Items {
		view := item.View
		if view == nil || view.Title() == "" {
			continue
		}
		if i == doc.tabs.Active {
			pane.Active = len(pane.Tabs)
		}
		pane.Tabs = append(pane.Tabs, contentpanes.TabState{
			Ref:      contentlink.Ref{Kind: contentlink.KindFile, Value: docview.NormalizeTabPath(view.Title())},
			Scroll:   view.ScrollOffset(),
			Wrap:     view.Wrap(),
			Rendered: view.Rendered(),
		})
	}
	if len(pane.Tabs) == 0 {
		return nil
	}
	return pane
}

func issuePaneState(issue *issuePane) *contentpanes.PaneState {
	if issue == nil {
		return nil
	}
	pane := &contentpanes.PaneState{Kind: "issue"}
	for i, item := range issue.tabs.Items {
		id := issueview.NormalizeID(item.Key)
		if id == "" && item.Value != nil {
			id = issueview.NormalizeID(item.Value.IssueID())
		}
		if id == "" {
			continue
		}
		if i == issue.tabs.Active {
			pane.Active = len(pane.Tabs)
		}
		tab := contentpanes.TabState{Ref: contentlink.Ref{Kind: contentlink.KindIssue, Value: id}}
		if item.Value != nil {
			tab.Scroll = item.Value.ScrollOffset()
			if name, root := item.Value.Owner(); name != "" && root != "" {
				tab.OwnerName, tab.OwnerRoot = name, root
			}
		}
		pane.Tabs = append(pane.Tabs, tab)
	}
	if len(pane.Tabs) == 0 {
		return nil
	}
	return pane
}

func notePaneState(note *notePane) *contentpanes.PaneState {
	if note == nil {
		return nil
	}
	pane := &contentpanes.PaneState{Kind: "note"}
	for i, item := range note.tabs.Items {
		id := noteview.NormalizeID(item.Key)
		if id == "" && item.Value != nil {
			id = noteview.NormalizeID(item.Value.NoteID())
		}
		if id == "" {
			continue
		}
		if i == note.tabs.Active {
			pane.Active = len(pane.Tabs)
		}
		tab := contentpanes.TabState{Ref: contentlink.Ref{Kind: contentlink.KindInternal, Namespace: "note", Value: id}}
		if item.Value != nil {
			tab.Scroll = item.Value.ScrollOffset()
		}
		pane.Tabs = append(pane.Tabs, tab)
	}
	if len(pane.Tabs) == 0 {
		return nil
	}
	return pane
}

func diffPaneState(diff *diffPane) *contentpanes.PaneState {
	if diff == nil {
		return nil
	}
	pane := &contentpanes.PaneState{Kind: "diff"}
	for i, item := range diff.tabs.Items {
		spec := item.Key
		if spec == "" && item.Value != nil {
			spec = item.Value.Target.Identity()
		}
		if spec == "" {
			continue
		}
		if i == diff.tabs.Active {
			pane.Active = len(pane.Tabs)
		}
		tab := contentpanes.TabState{Ref: contentlink.Ref{Kind: contentlink.KindDiff, Value: spec}}
		if item.Value != nil {
			tab.Path = item.Value.SelectedFileName()
			tab.Scope = item.Value.Scope.Persist()
			tab.Mode = item.Value.ViewMode.Persist()
			tab.Scroll = item.Value.Scroll
		}
		pane.Tabs = append(pane.Tabs, tab)
	}
	if len(pane.Tabs) == 0 {
		return nil
	}
	return pane
}

func resourcePaneState(res *resourcePane) *contentpanes.PaneState {
	if res == nil || res.tabs == nil {
		return nil
	}
	pane := &contentpanes.PaneState{Kind: "resource"}
	for i, ref := range res.tabs.References() {
		tab, ok := resourceTabStateFrom(ref)
		if !ok {
			continue
		}
		if i == res.tabs.ActiveIndex() {
			pane.Active = len(pane.Tabs)
		}
		pane.Tabs = append(pane.Tabs, tab)
	}
	if len(pane.Tabs) == 0 {
		return nil
	}
	return pane
}

// resourceTabStateFrom projects one remembered tab onto the deck's tab state,
// choosing the shape from the reference exactly as the codec does. All three
// shapes are carried: a collection tab dropped here would vanish from the saved
// layout the moment the deck was built from these maps.
func resourceTabStateFrom(ref resourceview.PersistedTab) (contentpanes.TabState, bool) {
	if ref.Provider == "" {
		return contentpanes.TabState{}, false
	}
	link := contentlink.Ref{Kind: contentlink.KindResource, Provider: ref.Provider}
	out := contentpanes.TabState{Scroll: ref.Scroll}
	switch {
	case ref.Collection != "" && ref.Matcher == "" && ref.Locator == "":
		link.Collection, link.Query = ref.Collection, ref.Query
		out.View, out.Sort, out.CursorID = ref.View, ref.Sort, ref.CursorID
		out.Filters = ref.Filters
	case ref.Collection != "" && ref.Matcher == "" && ref.Locator != "":
		link.Collection, link.Value = ref.Collection, ref.Locator
	case ref.Collection == "" && ref.Matcher != "" && ref.Locator != "":
		link.Matcher, link.Value = ref.Matcher, ref.Locator
	default:
		return contentpanes.TabState{}, false
	}
	out.Ref = link
	return out, true
}

func findPaneState(n *contentpanes.NodeState, kind string) *contentpanes.PaneState {
	if n == nil {
		return nil
	}
	if n.Kind == kind && n.Pane != nil {
		return n.Pane
	}
	if pane := findPaneState(n.A, kind); pane != nil {
		return pane
	}
	return findPaneState(n.B, kind)
}

func contentKindName(kind panelayout.Kind) string {
	switch kind {
	case panelayout.Primary:
		return "primary"
	case panelayout.Document:
		return "document"
	case panelayout.Issue:
		return "issue"
	case panelayout.Note:
		return "note"
	case panelayout.Diff:
		return "diff"
	case panelayout.Resource:
		return "resource"
	case panelayout.Shell:
		return "shell"
	default:
		return ""
	}
}

// restoreTree is the live pane tree for a decoded layout: State supplies
// structure (including the one shell), the deck supplies admission. A content
// kind the deck dropped — invalid tabs, a duplicate kind — is not re-created
// as an empty ghost leaf; its split collapses.
func restoreTree(n *contentpanes.NodeState, deck *contentpanes.Deck, nextID *int, seen map[PaneKind]bool) *PaneNode {
	if n == nil {
		return nil
	}
	if n.A != nil || n.B != nil {
		a := restoreTree(n.A, deck, nextID, seen)
		b := restoreTree(n.B, deck, nextID, seen)
		if a == nil {
			return b
		}
		if b == nil {
			return a
		}
		*nextID++
		axis := SplitCols
		if n.Axis == "rows" {
			axis = SplitRows
		}
		ratio := n.Ratio
		if ratio == 0 {
			ratio = 50
		}
		return &PaneNode{ID: *nextID, Split: &PaneSplit{Axis: axis, Ratio: clampPaneRatio(ratio), A: a, B: b}}
	}
	kind, ok := restoreTreeKind(n.Kind)
	if !ok || seen[kind] {
		return nil
	}
	if kind != PaneTerminal && kind != PaneShell {
		if deck == nil || deck.Leaf(kind) == 0 {
			return nil
		}
	}
	seen[kind] = true
	// Passive leaf IDs are owned by Deck. Restore must use those exact IDs,
	// rather than independently counting through the full host tree where a
	// collapsed Shell shifts every later leaf. Host-only Shell and internal
	// nodes allocate above Deck's complete ID space (nextID is initialized to
	// Deck.Tree's MaxID), so neither can alias a passive leaf on first reproject.
	if deck != nil && kind != PaneShell {
		deckKind := kind
		if kind == PaneTerminal {
			deckKind = panelayout.Primary
		}
		if id := deck.Leaf(deckKind); id > 0 {
			return &PaneNode{ID: id, Kind: kind, ContentID: id}
		}
	}
	*nextID++
	return &PaneNode{ID: *nextID, Kind: kind, ContentID: *nextID}
}

func restoreTreeKind(kind string) (PaneKind, bool) {
	switch kind {
	case "primary", panecodec.KindTerminal:
		return PaneTerminal, true
	case "document", panecodec.KindDoc:
		return PaneDoc, true
	case panecodec.KindIssue:
		return PaneIssue, true
	case panecodec.KindNote:
		return PaneNote, true
	case panecodec.KindDiff:
		return PaneDiff, true
	case panecodec.KindResource:
		return PaneResource, true
	case panecodec.KindShell:
		return PaneShell, true
	default:
		return 0, false
	}
}

func (p *Plugin) adoptRestoredDeckMaps(root, surface string) {
	deck := p.contentDeck
	if deck == nil {
		return
	}
	p.docs = make(map[int]*docPane)
	p.issues = make(map[int]*issuePane)
	p.notes = make(map[int]*notePane)
	p.diffs = make(map[int]*diffPane)
	p.resources = make(map[int]*resourcePane)
	for _, kind := range []panelayout.Kind{panelayout.Document, panelayout.Issue, panelayout.Note, panelayout.Diff, panelayout.Resource} {
		leaf := firstPaneLeafOfKind(p.paneRoot, kind)
		if leaf == nil {
			continue
		}
		items, active := deck.Tabs(deck.Leaf(kind))
		if len(items) == 0 {
			continue
		}
		leaf.ContentID = leaf.ID
		switch kind {
		case panelayout.Document:
			doc := newDocPane(leaf.ID, root, surface, nil)
			doc.tabs.Active = active
			for _, item := range items {
				if view, ok := item.Viewer.(*docview.Model); ok {
					doc.tabs.Items = append(doc.tabs.Items, docview.Item{View: view})
				}
			}
			p.docs[leaf.ID] = doc
		case panelayout.Issue:
			issue := &issuePane{leafID: leaf.ID, root: root, surface: surface}
			for _, item := range items {
				if view, ok := item.Viewer.(*issueview.Model); ok {
					issue.tabs.Append(item.Ref.Value, view)
				}
			}
			issue.tabs.Active = active
			p.issues[leaf.ID] = issue
		case panelayout.Note:
			note := &notePane{leafID: leaf.ID, root: root, surface: surface}
			for _, item := range items {
				if view, ok := item.Viewer.(*noteview.Model); ok {
					note.tabs.Append(item.Ref.Value, view)
				}
			}
			note.tabs.Active = active
			p.notes[leaf.ID] = note
		case panelayout.Diff:
			diff := &diffPane{leafID: leaf.ID, root: root, surface: surface}
			for _, item := range items {
				if view, ok := item.Viewer.(*workspacediff.View); ok {
					diff.tabs.Append(item.Ref.Value, view)
				}
			}
			diff.tabs.Active = active
			p.diffs[leaf.ID] = diff
		case panelayout.Resource:
			res := p.newResourcePane(leaf.ID, root, surface)
			res.tabs.SetResolver(p.resolveResource)
			for _, item := range items {
				if view, ok := item.Viewer.(*resourceview.Model); ok {
					res.tabs.Append(resourceview.TabKey(view.Reference()), view)
				}
			}
			res.tabs.Group.Active = active
			p.resources[leaf.ID] = res
		}
	}
}

func (p *Plugin) workspaceDeckPlacement() (contentpanes.Placement, bool) {
	peer, ok := p.previewPeerBox()
	if !ok {
		return contentpanes.Placement{}, false
	}
	return contentpanes.Placement{
		Box: peer, Boxes: p.lastPaneBoxes(), Floors: paneTreeFloors(),
		Split: p.openSplit,
		Plan:  p.pendingOpenPlan,
	}, true
}

func (p *Plugin) openWorkspaceContent(root, surface string, ref contentlink.Ref, name string) tea.Cmd {
	return p.openWorkspaceContentFile(root, surface, ref, name, nil)
}

func (p *Plugin) openWorkspaceContentFile(root, surface string, ref contentlink.Ref, name string, file *os.File) tea.Cmd {
	wasInteractive := p.viewMode == ViewModeInteractive
	deck, adopt := p.ensureWorkspaceDeck(root, surface)
	placement, ok := p.workspaceDeckPlacement()
	if !ok {
		return nil
	}
	ctx := p.workspaceDeckContext(root, surface)
	var out contentpanes.Outcome
	if file != nil {
		out = deck.OpenDocumentFile(ctx, ref, placement, file)
	} else {
		out = deck.Open(ctx, ref, placement)
	}
	if out.Status == contentpanes.StatusRefused {
		if out.Refusal == contentpanes.RefusalFit {
			dimension := "wider"
			plan, planned := deck.PlanOpen(ref, placement.Boxes)
			if planned && panelayout.ApplyAxisOverride(plan, placement.Split).Axis == panelayout.Rows {
				dimension = "taller"
			}
			p.toastMessage, p.toastTime = name+" pane needs a "+dimension+" window; layout left unchanged", time.Now()
		}
		return nil
	}
	p.syncWorkspaceDeckProjection(root, surface)
	p.hiddenPaneLayout = nil
	if wasInteractive {
		// sidecar-open may split beside a live terminal without taking its input
		// mode away. Record the new leaf for close/hide routing without invoking
		// setFocusTarget, whose deliberate click/key behavior exits interactive.
		p.paneFocus, p.activePane = out.LeafID, PanePreview
	} else {
		p.focusLeaf(out.LeafID)
	}
	p.saveSelectionState()
	cmds := unwrapDeckCmds(append(adopt, out.Command)...)
	if out.CreatedLeaf {
		cmds = append(cmds, p.docTerminalResizeCmds()...)
	}
	return tea.Batch(cmds...)
}

func unwrapWorkspaceDeckLoad(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if result, ok := msg.(contentpanes.Result); ok {
			return result.Payload
		}
		return msg
	}
}

func unwrapDeckCmds(cmds ...tea.Cmd) []tea.Cmd {
	out := make([]tea.Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if unwrapped := unwrapWorkspaceDeckLoad(cmd); unwrapped != nil {
			out = append(out, unwrapped)
		}
	}
	return out
}

func (p *Plugin) syncWorkspaceDeckProjection(root, surface string) {
	deck := p.contentDeck
	if deck == nil {
		return
	}
	oldDocs, oldIssues, oldNotes, oldDiffs, oldResources := p.docs, p.issues, p.notes, p.diffs, p.resources
	keep := p.paneFocus
	// The deck's tree is the passive content panes' — it has never heard of the
	// shell leaf. Its shape is recorded before the projection lands and the leaf
	// is put back after, so a projection cannot quietly close the panel or move
	// its divider back to the default.
	oldTree := p.paneRoot
	shell := p.shellLeaf()
	shellGrafts := panereposition.CaptureLeafGrafts(oldTree, panelayout.Shell)
	p.rememberShellSplit()
	p.paneRoot = reconcileWorkspaceDeckTree(oldTree, deck.Tree())
	if shell != nil && p.terminalPanes != nil {
		if state := p.terminalPanes.Leaf(shell.ID); state != nil && state.Requested {
			for _, graft := range shellGrafts {
				if graft.LeafID == shell.ID {
					p.paneRoot = panereposition.ApplyLeafGraft(p.paneRoot, graft, shell)
					break
				}
			}
		}
	}
	p.syncShellLeaf()
	// setFocusTarget is the sole writer of the ring. A live-refresh broadcast
	// must not walk it to whatever leaf last opened a tab — that is how
	// switching plugins stole the shell whenever a document, issue, or diff
	// pane was open. Keep the current leaf when it still exists; otherwise take
	// the deck's answer, which Close/Open have already pointed at the survivor.
	if FindPane(p.paneRoot, keep) != nil {
		p.paneFocus = keep
		p.syncDeckFocus()
	} else {
		p.paneFocus = deck.FocusedLeaf()
	}
	p.paneNextID = panelayout.MaxID(p.paneRoot) + 1
	p.docs = make(map[int]*docPane)
	p.issues = make(map[int]*issuePane)
	p.notes = make(map[int]*notePane)
	p.diffs = make(map[int]*diffPane)
	p.resources = make(map[int]*resourcePane)
	for _, kind := range []panelayout.Kind{panelayout.Document, panelayout.Issue, panelayout.Note, panelayout.Diff, panelayout.Resource} {
		leafID := deck.Leaf(kind)
		if leafID == 0 {
			continue
		}
		items, active := deck.Tabs(leafID)
		switch kind {
		case panelayout.Document:
			doc := oldDocs[leafID]
			if doc == nil {
				doc = newDocPane(leafID, root, surface, nil)
			}
			doc.leafID, doc.root, doc.surface = leafID, root, surface
			doc.tabs = docview.Tabs{Active: active}
			for _, item := range items {
				if view, ok := item.Viewer.(*docview.Model); ok {
					doc.tabs.Items = append(doc.tabs.Items, docview.Item{View: view})
				}
			}
			p.docs[leafID] = doc
		case panelayout.Issue:
			issue := oldIssues[leafID]
			if issue == nil {
				issue = &issuePane{}
			}
			issue.leafID, issue.root, issue.surface, issue.tabs = leafID, root, surface, issueview.Tabs{}
			for _, item := range items {
				if view, ok := item.Viewer.(*issueview.Model); ok {
					issue.tabs.Append(item.Ref.Value, view)
				}
			}
			issue.tabs.Active = active
			p.issues[leafID] = issue
		case panelayout.Note:
			note := oldNotes[leafID]
			if note == nil {
				note = &notePane{}
			}
			note.leafID, note.root, note.surface, note.tabs = leafID, root, surface, noteview.Tabs{}
			for _, item := range items {
				if view, ok := item.Viewer.(*noteview.Model); ok {
					note.tabs.Append(item.Ref.Value, view)
				}
			}
			note.tabs.Active = active
			p.notes[leafID] = note
		case panelayout.Diff:
			diff := oldDiffs[leafID]
			if diff == nil {
				diff = &diffPane{}
			}
			diff.leafID, diff.root, diff.surface, diff.tabs = leafID, root, surface, workspacediff.Group{}
			for _, item := range items {
				if view, ok := item.Viewer.(*workspacediff.View); ok {
					diff.tabs.Append(item.Ref.Value, view)
				}
			}
			diff.tabs.Active = active
			p.diffs[leafID] = diff
		case panelayout.Resource:
			res := oldResources[leafID]
			if res == nil {
				res = p.newResourcePane(leafID, root, surface)
			}
			res.leafID, res.root, res.surface = leafID, root, surface
			res.tabs.Items = nil
			res.tabs.SetResolver(p.resolveResource)
			for _, item := range items {
				if view, ok := item.Viewer.(*resourceview.Model); ok {
					res.tabs.Append(resourceview.TabKey(view.Reference()), view)
				}
			}
			res.tabs.Group.Active = active
			p.resources[leafID] = res
		}
	}
}

func reconcileWorkspaceDeckTree(current, fresh *panelayout.Node) *panelayout.Node {
	byID := make(map[int]*panelayout.Node)
	var index func(*panelayout.Node)
	index = func(n *panelayout.Node) {
		if n == nil {
			return
		}
		// Shell is host-owned and must retain its exact node object for the
		// graft replay. A passive leaf that happens to reuse its numeric ID must
		// never repurpose that live object during reconciliation.
		if n.Split != nil || n.Kind != panelayout.Shell {
			byID[n.ID] = n
		}
		if n.Split != nil {
			index(n.Split.A)
			index(n.Split.B)
		}
	}
	index(current)
	var adopt func(*panelayout.Node) *panelayout.Node
	adopt = func(n *panelayout.Node) *panelayout.Node {
		if n == nil {
			return nil
		}
		out := byID[n.ID]
		if out == nil {
			out = &panelayout.Node{}
		}
		out.ID, out.Kind, out.ContentID = n.ID, n.Kind, n.ContentID
		if n.Split == nil {
			out.Split = nil
			return out
		}
		out.Split = &panelayout.Split{Axis: n.Split.Axis, Ratio: n.Split.Ratio, A: adopt(n.Split.A), B: adopt(n.Split.B)}
		return out
	}
	return adopt(fresh)
}

func (p *Plugin) replaceWorkspaceContent(root, surface string, ref contentlink.Ref) tea.Cmd {
	deck, adopt := p.ensureWorkspaceDeck(root, surface)
	out := deck.ReplaceActive(p.workspaceDeckContext(root, surface), ref)
	if !out.Accepted() {
		return nil
	}
	p.syncWorkspaceDeckProjection(root, surface)
	p.focusLeaf(out.LeafID)
	p.saveSelectionState()
	return tea.Batch(unwrapDeckCmds(append(adopt, out.Command)...)...)
}

func (p *Plugin) applyWorkspaceDeckBroadcast(msg any) tea.Cmd {
	if p.contentDeck == nil {
		return nil
	}
	cmd := p.contentDeck.ApplyBroadcast(msg)
	ctx := p.contentDeck.Context()
	p.syncWorkspaceDeckProjection(ctx.Root, ctx.Surface)
	p.saveSelectionState()
	return cmd
}

func workspaceDeckTabCount(deck *contentpanes.Deck, kind panelayout.Kind) int {
	if deck == nil {
		return 0
	}
	items, _ := deck.Tabs(deck.Leaf(kind))
	return len(items)
}

func (p *Plugin) applyWorkspaceDeckResult(result contentpanes.Result) tea.Cmd {
	if p.contentDeck == nil {
		return nil
	}
	cmd, applied := p.contentDeck.Apply(result)
	if applied {
		ctx := p.contentDeck.Context()
		p.syncWorkspaceDeckProjection(ctx.Root, ctx.Surface)
		p.saveSelectionState()
	}
	return cmd
}

func (p *Plugin) selectWorkspaceDeckTab(kind panelayout.Kind, index int) tea.Cmd {
	if p.contentDeck == nil {
		return nil
	}
	cmd := p.contentDeck.SelectTab(p.contentDeck.Leaf(kind), index)
	ctx := p.contentDeck.Context()
	p.syncWorkspaceDeckProjection(ctx.Root, ctx.Surface)
	p.saveSelectionState()
	return cmd
}

func (p *Plugin) cycleWorkspaceDeckTab(kind panelayout.Kind, delta int) tea.Cmd {
	if p.contentDeck == nil || !p.contentDeck.FocusLeaf(p.contentDeck.Leaf(kind)) {
		return nil
	}
	cmd := p.contentDeck.CycleTab(delta)
	ctx := p.contentDeck.Context()
	p.syncWorkspaceDeckProjection(ctx.Root, ctx.Surface)
	p.saveSelectionState()
	return cmd
}

func (p *Plugin) closeWorkspaceDeckTabAt(kind panelayout.Kind, index int) tea.Cmd {
	if p.contentDeck == nil {
		return nil
	}
	leaf := p.contentDeck.Leaf(kind)
	if leaf == 0 {
		return nil
	}
	p.contentDeck.CloseTab(leaf, index)
	return p.finishWorkspaceDeckClose(kind)
}

func (p *Plugin) finishWorkspaceDeckClose(kind panelayout.Kind) tea.Cmd {
	leafClosed := p.contentDeck.Leaf(kind) == 0
	ctx := p.contentDeck.Context()
	p.syncWorkspaceDeckProjection(ctx.Root, ctx.Surface)
	p.saveSelectionState()
	if leafClosed {
		return p.resizeDocTerminalCmd()
	}
	return nil
}
