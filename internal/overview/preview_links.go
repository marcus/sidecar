package overview

import (
	"errors"
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/inlineedit"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/paneframe"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/panesearch"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/targetactivation"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacediff"
	"github.com/marcus/sidecar/internal/workspacelist"
)

const (
	previewDocRegionKind     = "global-preview-doc"
	previewDocTabKind        = "global-preview-doc-tab"
	previewPaneCloseKind     = "global-preview-pane-close"
	previewSecondaryMinWidth = markdown.MinWidthForMarkdown
	previewTermMinWidth      = 12
)

// previewPaneCloseHit names the content pane whose header X was clicked.
type previewPaneCloseHit struct {
	Kind   panelayout.Kind
	LeafID int
}

func isPreviewDocRegion(kind string) bool {
	return kind == previewDocRegionKind || kind == previewDocTabKind || kind == previewDocLinkKind
}

// previewDocTabHit is the tab stored on the document header region.
type previewDocTabHit struct {
	Index int
	Close bool
}

// previewDoc is the terminal-adjacent file preview on the global surface.
// It reuses docview.Tabs; it is not the issue-preview modal.
type previewDoc struct {
	tabs    docview.Tabs
	root    string
	surface string
	focused bool
	nextID  int
	epoch   uint64
	// mode is the search surface this pane is showing over its document, or
	// nil; modeRegions are the hit regions its last render earned.
	mode        *panesearch.Mode
	modeRegions []mouse.Region
	// edit is this pane's inline editor, created on the first `e`, and box is
	// the rectangle its last render placed it in — the editor's dimension
	// contract answers from it. See preview_doc_edit.go.
	edit         *inlineedit.Session
	box          termpreview.Box
	editW, editH int
	// pendingEdit is the action an exit confirmation is holding.
	pendingEdit func() tea.Cmd
	// hostNotice is a connected-stale or verb-failure label for a remote
	// document that is still showing its last good body.
	hostNotice string
}

func (d *previewDoc) view() *docview.Model {
	if d == nil {
		return nil
	}
	return d.tabs.ActiveView()
}

func (d *previewDoc) allocID() int {
	d.nextID++
	return d.nextID
}

func (m *Model) previewRemoteHostID() string {
	if m.preview.deck != nil {
		if hostID := m.preview.deck.Context().Source.HostID; hostID != "" {
			return hostID
		}
	}
	if ws, ok := m.SelectedWorkspace(); ok {
		return ws.HostID
	}
	return ""
}

type previewDocLoadedMsg struct {
	docview.LoadedMsg
	WorkspaceID string
}

func (m *Model) previewResolveRoot() string {
	workspace, ok := m.SelectedWorkspace()
	if !ok {
		return ""
	}
	return workspace.Path
}

func (m *Model) previewLinkAt(action mouse.MouseAction) (terminallink.Span, bool) {
	geometry, ok := m.previewGeometry()
	if !ok {
		return terminallink.Span{}, false
	}
	buffer := m.previewBuffer()
	cell, ok := tty.CellAt(geometry, buffer, action.X, action.Y)
	if !ok {
		return terminallink.Span{}, false
	}
	line, ok := tty.LineTextAt(buffer, cell.Line)
	if !ok {
		return terminallink.Span{}, false
	}
	line = ui.ExpandTabs(line, tty.DefaultTabWidth)
	span, ok := m.previewTerminalLeaf().LinkState.SpanAt(line, cell.Line, cell.Col)
	return span, ok
}

func (m *Model) activatePreviewLinkAt(action mouse.MouseAction, modified bool) (tea.Cmd, bool) {
	if modified {
		return nil, false
	}
	span, ok := m.previewLinkAt(action)
	if !ok {
		return nil, false
	}
	plan, err := targetactivation.PlanForSpan(span)
	if err != nil {
		return nil, false
	}
	if plan.Kind == targetactivation.PlanOpenFile || plan.Kind == targetactivation.PlanOpenDiff {
		workspace, ok := m.SelectedWorkspace()
		if ok && workspace.Remote() {
			return m.activatePreviewPlan(plan)
		}
		return m.revalidatePreviewLink(span)
	}
	return m.activatePreviewPlan(plan)
}

// activatePreviewPlan executes what the shared service resolved. The decision —
// what this text names, whether the URL is safe — is targetactivation's and is
// identical on the workspace surface; only the panes below are this surface's
// own, so a kind that activates here activates there.
func (m *Model) activatePreviewPlan(plan targetactivation.Plan) (tea.Cmd, bool) {
	var cmd tea.Cmd
	switch plan.Kind {
	case targetactivation.PlanOpenURL:
		m.clearPreviewSelection()
		return terminallink.OpenHTTP(plan.URL), true
	case targetactivation.PlanOpenFile:
		cmd = m.openPreviewDocTarget(uirequest.Target{
			Kind:  uirequest.TargetKindFile,
			Value: plan.Path,
			Line:  plan.Line,
		})
	case targetactivation.PlanOpenIssue:
		cmd = m.openPreviewIssue(plan.Issue)
	case targetactivation.PlanOpenNote:
		cmd = m.openPreviewNote(plan.Note)
	case targetactivation.PlanOpenDiff:
		cmd = m.activatePreviewDiff(plan.Spec)
	case targetactivation.PlanAttachSession:
		cmd = m.attachPreviewSession(plan.Session)
	case targetactivation.PlanOpenResource:
		cmd = m.activatePreviewResource(resourceview.Ref{
			Instance:   plan.Provider,
			Matcher:    plan.Matcher,
			Locator:    plan.Locator,
			Collection: plan.Collection,
			Query:      plan.Query,
			Filters:    resource.DecodeFilters(plan.Filters),
		})
	default:
		return nil, false
	}
	if cmd == nil {
		return nil, false
	}
	m.clearPreviewSelection()
	return cmd, true
}

// previewHandlesPlanKind is the parity assertion's other half: every plan kind
// a scanned span can produce must be dispatched above. Its twin lives on the
// workspace surface.
func previewHandlesPlanKind(kind targetactivation.PlanKind) bool {
	switch kind {
	case targetactivation.PlanOpenURL, targetactivation.PlanOpenFile,
		targetactivation.PlanOpenIssue, targetactivation.PlanOpenNote, targetactivation.PlanOpenDiff,
		targetactivation.PlanOpenResource, targetactivation.PlanAttachSession:
		return true
	default:
		return false
	}
}

// attachPreviewSession is this surface's version of "attach": select the
// workspace running that tmux session and hand it the keyboard. The project
// workspace attaches by opening the session in its own terminal; here the
// live pane is already on screen, so attaching means typing into it. Same
// decision (targetactivation), surface-local execution — the rule every kind
// on these two surfaces already follows.
//
// The click carries the source host of the pane it came from. A name in a
// remote pane matches only (that HostID, tmux session); a name in a local
// pane matches a local row only. A same-named twin on the other side of the
// host boundary is not attachable from this click.
//
// A session no row is running is not attachable, and the caller treats a nil
// command as "this click did nothing", which is what an unknown session is.
func (m *Model) attachPreviewSession(session string) tea.Cmd {
	if strings.TrimSpace(session) == "" {
		return nil
	}
	sourceHostID := ""
	if ws, ok := m.SelectedWorkspace(); ok {
		sourceHostID = ws.HostID
	}
	ids := make([]string, 0, len(m.catalog))
	for id := range m.catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ws := m.catalog[id]
		if ws.TmuxName != session || ws.HostID != sourceHostID {
			continue
		}
		if !m.workspaces.SelectID(id) {
			return nil
		}
		return tea.Batch(m.focusList(), m.previewSync(), m.enterPreviewInteractive())
	}
	return nil
}

func (m *Model) activatePreviewDiff(raw string) tea.Cmd {
	workspace, ok := m.SelectedWorkspace()
	if !ok {
		return nil
	}
	if workspace.Remote() {
		ctx, ok := m.previewDeckContext()
		if !ok {
			return nil
		}
		ref, err := contentpanes.ResolveDocument(m.previewDeckConfig(ctx).Source, ctx.Source, contentlink.Pending{
			Kind: contentlink.KindDiff, Raw: raw,
		})
		if err != nil || ref.Value == "" {
			if err == nil {
				err = errors.New("git object not found on " + ctx.Source.HostID)
			}
			return remoteContentErrorCmd(err)
		}
		target, ok := workspacediff.ParseSpec(ref.Value)
		if !ok || (target.Kind != workspacediff.TargetCommit && target.Kind != workspacediff.TargetRange) {
			return nil
		}
		return m.openPreviewDiff(target)
	}
	target := uirequest.DiffTarget(previewDiffPath(workspace), raw)
	if target.Kind != workspacediff.TargetCommit && target.Kind != workspacediff.TargetRange {
		return nil
	}
	return m.openPreviewDiff(target)
}

// openPreviewDocTarget opens a file target in the preview document pane. The
// target's Value is the token as the text wrote it; it is re-resolved against
// this surface's content source, so a remote row never reads the viewer's twin.
func (m *Model) openPreviewDocTarget(target uirequest.Target) tea.Cmd {
	cmd, _ := m.openPreviewDocTargetResult(target)
	return cmd
}

func (m *Model) openPreviewDocTargetResult(target uirequest.Target) (tea.Cmd, error) {
	_, ok := m.SelectedWorkspace()
	if !ok {
		return nil, nil
	}
	ctx, ok := m.previewDeckContext()
	if !ok {
		return nil, nil
	}
	ref, err := contentpanes.ResolveDocument(m.previewDeckConfig(ctx).Source, ctx.Source, contentlink.Pending{
		Kind: contentlink.KindFile, Raw: target.Value,
	})
	if err != nil || ref.Value == "" {
		if ctx.Source.Remote() {
			if err == nil {
				err = errors.New("file not found on " + ctx.Source.HostID)
			}
			return remoteContentErrorCmd(err), err
		}
		return nil, nil
	}
	ref.Line = target.Line
	return m.openPreviewContent(ref, "Document"), nil
}

func (m *Model) selectPreviewDocTab(idx, line int, file *os.File) tea.Cmd {
	doc := m.preview.doc
	if doc == nil {
		if file != nil {
			_ = file.Close()
		}
		return nil
	}
	doc.tabs.Select(idx)
	view := doc.view()
	if view == nil {
		if file != nil {
			_ = file.Close()
		}
		return nil
	}
	if !view.NeedsLoad() {
		if line > 0 {
			view.ApplyLine(line)
		}
		if file != nil {
			_ = file.Close()
		}
		return nil
	}
	rel := view.Title()
	rendered := view.Rendered()
	wrap := view.Wrap()
	var cmd tea.Cmd
	if file != nil {
		cmd = view.LoadFile(doc.allocID(), file, rel, line, doc.epoch)
	} else {
		cmd = view.Load(doc.allocID(), doc.root, rel, line, doc.epoch)
	}
	if line > 0 {
		applyPreviewDocRenderMode(view, rel, line)
	} else {
		view.SetRendered(rendered)
	}
	view.SetWrap(wrap)
	return cmd
}

func (m *Model) clickPreviewDocTab(index int) tea.Cmd {
	if m.preview.deck == nil {
		return nil
	}
	leaf := m.preview.deck.Leaf(panelayout.Document)
	cmd := m.preview.deck.SelectTab(leaf, index)
	if ctx, ok := m.previewDeckContext(); ok {
		m.syncPreviewDeckProjection(ctx)
	}
	return cmd
}

func (m *Model) cyclePreviewDocTab(delta int) tea.Cmd {
	if m.preview.deck == nil || !m.preview.deck.FocusLeaf(m.preview.deck.Leaf(panelayout.Document)) {
		return nil
	}
	cmd := m.preview.deck.CycleTab(delta)
	if ctx, ok := m.previewDeckContext(); ok {
		m.syncPreviewDeckProjection(ctx)
	}
	return cmd
}

func (m *Model) closePreviewDocTab() tea.Cmd {
	if m.preview.doc == nil {
		return nil
	}
	return m.closePreviewDocTabAt(m.preview.doc.tabs.Active)
}

func (m *Model) closePreviewDocTabAt(index int) tea.Cmd {
	if m.preview.doc == nil || m.preview.deck == nil {
		return nil
	}
	if index < 0 || index >= len(m.preview.doc.tabs.Items) {
		return nil
	}
	// Closing the tab the editor is holding away; ask first.
	if index == m.preview.doc.tabs.Active && m.guardPreviewDocEdit(func() tea.Cmd { return m.closePreviewDocTabAt(index) }) {
		return nil
	}
	m.preview.deck.CloseTab(m.preview.deck.Leaf(panelayout.Document), index)
	return m.finishPreviewDeckClose()
}

func wrapPreviewDocLoad(cmd tea.Cmd, workspaceID string) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if loaded, ok := msg.(docview.LoadedMsg); ok {
			return previewDocLoadedMsg{LoadedMsg: loaded, WorkspaceID: workspaceID}
		}
		return msg
	}
}

func (m *Model) applyPreviewDocLoaded(msg previewDocLoadedMsg) {
	doc := m.preview.doc
	if msg.WorkspaceID != m.preview.workspaceID {
		doc = m.preview.paneCache[msg.WorkspaceID].doc
	}
	if doc == nil || doc.surface != msg.WorkspaceID {
		return
	}
	for _, item := range doc.tabs.Items {
		if item.View == nil {
			continue
		}
		matched := item.View.ResultMatches(msg.LoadedMsg)
		if item.View.SetResult(msg.LoadedMsg) {
			doc.hostNotice = ""
			return
		}
		if !matched {
			continue
		}
		if msg.NotModified {
			doc.hostNotice = ""
			return
		}
		if msg.Refresh && msg.Result.Error != nil {
			doc.hostNotice = remoteDocumentStaleNotice
		}
		return
	}
}

func (m *Model) previewDocHeaderTabs(row string) string {
	notice := ""
	if m.preview.doc != nil {
		notice = m.preview.doc.hostNotice
	}
	return m.previewHostHeaderTabs(row, notice)
}

func (m *Model) previewHostHeaderTabs(row, notice string) string {
	hostID := m.previewRemoteHostID()
	if hostID == "" {
		return row
	}
	label := workspacelist.HostGlyph + " " + hostID
	if notice != "" {
		label += " · " + notice
	}
	return row + " " + styles.Muted.Render(label)
}

func (m *Model) reloadPreviewDoc() tea.Cmd {
	if m.preview.deck != nil {
		if ctx, ok := m.previewDeckContext(); ok {
			m.preview.deck.FocusLeaf(m.preview.deck.Leaf(panelayout.Document))
			return wrapPreviewDeckCmd(m.preview.deck.ReloadFocused(), ctx.Surface)
		}
	}
	doc := m.preview.doc
	if doc == nil || doc.view() == nil || doc.view().Title() == "" {
		return nil
	}
	return wrapPreviewDocLoad(doc.view().Reload(), doc.surface)
}

func applyPreviewDocRenderMode(view *docview.Model, path string, line int) {
	if view == nil {
		return
	}
	if !terminallink.Markdown(path) || line > 0 {
		view.SetRendered(false)
		return
	}
	view.SetRendered(true)
}

func (m *Model) closePreviewDoc() tea.Cmd {
	if m.preview.doc == nil {
		return nil
	}
	// Closing the pane leaves a live editor with nowhere to draw; ask first.
	if m.guardPreviewDocEdit(func() tea.Cmd { return m.closePreviewDoc() }) {
		return nil
	}
	if m.preview.deck == nil {
		return nil
	}
	m.preview.deck.FocusLeaf(m.preview.deck.Leaf(panelayout.Document))
	for m.preview.deck.Leaf(panelayout.Document) != 0 {
		m.preview.deck.CloseActive()
	}
	if ctx, ok := m.previewDeckContext(); ok {
		m.syncPreviewDeckProjection(ctx)
	}
	if m.preview.issue != nil {
		m.focusPreviewPane(panelayout.Issue)
		return m.syncTerminalGeometry()
	}
	if m.preview.diff != nil {
		m.focusPreviewPane(panelayout.Diff)
		return m.syncTerminalGeometry()
	}
	if m.preview.resource != nil {
		m.focusPreviewPane(panelayout.Resource)
		return m.syncTerminalGeometry()
	}
	return tea.Batch(m.focusList(), m.syncTerminalGeometry())
}

// focusPreviewPane focuses the first leaf of a kind. It is a thin wrapper over
// the focus setter so callers that know only "the document pane" share one
// body with the ring, which knows only leaf IDs.
func (m *Model) focusPreviewPane(kind panelayout.Kind) bool {
	leaf := panelayout.FirstOfKind(m.preview.paneRoot, kind)
	if leaf == nil {
		return false
	}
	// The setter's leaf path can hand back the live pane's keyboard; the queue is
	// how that reaches Update from callers that deal in bools rather than
	// commands. Only the sidebar path issues anything else.
	m.queuePreviewCmd(m.setFocusTarget(panelayout.Target{Kind: panelayout.TargetLeaf, Leaf: leaf.ID}))
	return true
}

// focusPreviewLeaf keeps the input owner and the layout tree's focused leaf in
// lockstep. The tree focus also selects the pane rendered in narrow layouts.
func (m *Model) focusPreviewLeaf(leafID int) (bool, tea.Cmd) {
	leaf := panelayout.Find(m.preview.paneRoot, leafID)
	if leaf == nil {
		return false, nil
	}
	// Typing into the live pane is the terminal leaf holding the keyboard, and
	// it is only ever legal on the leaf that has focus. Ending it here, rather
	// than at each site that moves focus, is what keeps the ring honest: a ring
	// drawn on the document while keys land in the shell is exactly what a
	// per-site rule leaks the first time a site forgets.
	var cmd tea.Cmd
	if !panelayout.IsLive(leaf.Kind) {
		cmd = m.exitPreviewInteractive()
	}
	m.preview.paneFocus = leaf.ID
	if m.preview.deck != nil {
		m.preview.deck.FocusLeaf(leaf.ID)
	}
	m.preview.focus = focusPreview
	if previewTreeComposed(m.preview.paneRoot) {
		m.persistSessionsLayout()
	}
	if m.preview.doc != nil {
		m.preview.doc.focused = leaf.Kind == panelayout.Document
	}
	// A pane that has lost the keyboard drops whatever search it was showing,
	// enforced at the single focus writer so every gesture that moves focus
	// obeys it without having to remember to.
	if cmd2 := m.closeUnfocusedPreviewDocSearch(); cmd2 != nil {
		cmd = tea.Batch(cmd, cmd2)
	}
	if m.preview.issue != nil {
		m.preview.issue.focused = leaf.Kind == panelayout.Issue
		if view := m.preview.issue.view(); view != nil {
			view.SetFocused(m.preview.issue.focused)
		}
	}
	if m.preview.note != nil {
		m.preview.note.focused = leaf.Kind == panelayout.Note
		if view := m.preview.note.view(); view != nil {
			view.SetFocused(m.preview.note.focused)
		}
	}
	if m.preview.diff != nil {
		m.preview.diff.focused = leaf.Kind == panelayout.Diff
	}
	if m.preview.resource != nil {
		m.preview.resource.focused = leaf.Kind == panelayout.Resource
	}
	return true, cmd
}

// lastPreviewBoxes is the tiled leaf OUTER geometry for the current preview
// peer. PlanOpen reads areas from these boxes; a tree that does not fit (the
// zoomed LayoutTree case) has no areas to offer.
func (m *Model) lastPreviewBoxes() map[int]panelayout.Box {
	peer, ok := m.previewPeerBox()
	if !ok {
		return nil
	}
	leaves, _, fits := panelayout.LayoutPanes(m.preview.paneRoot, peer, previewPaneFloors())
	if !fits {
		return nil
	}
	boxes := make(map[int]panelayout.Box, len(leaves))
	for _, leaf := range leaves {
		if leaf.Node == nil {
			continue
		}
		boxes[leaf.Node.ID] = leaf.Box
	}
	return boxes
}

// layoutPreviewPanes places the tree in peer, a surface-local OUTER rectangle.
// Every placement it returns is therefore OUTER: the box a leaf's own border is
// drawn on, not the box its content draws in.
func (m *Model) layoutPreviewPanes(peer termpreview.Box) (panelayout.Layout, bool) {
	if m.preview.paneRoot == nil {
		m.resetActivePreviewPanes()
	}
	zoom := m.paneZoom.Leaf(m.paneLayoutScope(), m.preview.paneRoot)
	return panelayout.LayoutTreeWithZoom(m.preview.paneRoot, peer, previewPaneFloors(), m.preview.paneFocus, zoom)
}

// previewPaneBox is a kind's INNER box: what its content draws in, what tmux and
// the native cursor are sized against, and what its hit regions are tested in.
func (m *Model) previewPaneBox(kind panelayout.Kind, peer termpreview.Box) (termpreview.Box, bool) {
	layout, ok := m.layoutPreviewPanes(peer)
	if !ok {
		return termpreview.Box{}, false
	}
	for _, leaf := range layout.Leaves {
		if leaf.Node.Kind == kind {
			return paneframe.Inset(leaf.Box), true
		}
	}
	return termpreview.Box{}, false
}

// previewTerminalBox is the on-screen box of the live leaf the resource-bearing
// preview currently means — resolved by the same rule as previewTerminalLeaf,
// so the geometry the native cursor, the scrollbar gesture, and link hits are
// placed by can never name a different pane than the state it is paired with.
func (m *Model) previewTerminalBox() (termpreview.Box, bool) {
	if node := panelayout.Find(m.preview.paneRoot, m.preview.paneFocus); node != nil && node.Split == nil && panelayout.IsLive(node.Kind) {
		return m.terminalLeafBox(node.ID)
	}
	peer, ok := m.previewPeerBox()
	if !ok {
		return termpreview.Box{}, false
	}
	return m.previewPaneBox(panelayout.Terminal, peer)
}

// registerPreviewDocRegion covers the Document leaf's INNER box.
func (m *Model) registerPreviewDocRegion(docBox termpreview.Box) {
	if m.preview.doc == nil {
		return
	}
	m.workspacesMouse.HitMap.AddRect(previewDocRegionKind, docBox.X, docBox.Y, docBox.W, docBox.H, previewDocRegionKind)
}

func (m *Model) registerPreviewDocTabRegions(docBox termpreview.Box) {
	if m.preview.doc == nil {
		return
	}
	strip := docview.LayoutTabStrip(m.preview.doc.tabs, m.reserveHeader(docBox.W, true).TabsWidth, m.PreviewFocused() && m.preview.doc.focused)
	strip.RegisterHits(func(col, width, index int, close bool) {
		m.workspacesMouse.HitMap.AddRect(previewDocTabKind, docBox.X+col, docBox.Y, width, 1, previewDocTabHit{Index: index, Close: close})
	})
}

func (m *Model) handlePreviewDocMouse(action mouse.MouseAction) tea.Cmd {
	if hit, ok := action.Region.Data.(previewDocLinkHit); ok {
		switch action.Type {
		case mouse.ActionClick, mouse.ActionDoubleClick:
			m.focusPreviewPane(panelayout.Document)
			if action.Shift || action.Alt {
				return m.pressPreviewDocSelection(action)
			}
			return m.activatePreviewDocLink(hit.Ref)
		case mouse.ActionScrollUp, mouse.ActionScrollDown:
			if view := m.preview.doc.view(); view != nil {
				view.Scroll(action.Delta)
			}
		}
		return nil
	}
	if tab, ok := action.Region.Data.(previewDocTabHit); ok {
		if action.Type == mouse.ActionClick || action.Type == mouse.ActionDoubleClick {
			if tab.Close {
				return m.closePreviewDocTabAt(tab.Index)
			}
			return m.clickPreviewDocTab(tab.Index)
		}
		if m.preview.doc != nil && m.preview.doc.view() != nil {
			switch action.Type {
			case mouse.ActionScrollUp, mouse.ActionScrollDown:
				m.preview.doc.view().Scroll(action.Delta)
			}
		}
		return nil
	}
	kind, _ := regionKind(action.Region)
	if kind != previewDocRegionKind || m.preview.doc == nil {
		return nil
	}
	switch action.Type {
	case mouse.ActionClick, mouse.ActionDoubleClick, mouse.ActionTripleClick:
		m.focusPreviewPane(panelayout.Document)
		// A press over the document's text arms a selection; word by double
		// click, line by triple, as the terminal beside it answers them.
		return m.pressPreviewDocSelection(action)
	case mouse.ActionScrollUp, mouse.ActionScrollDown:
		if view := m.preview.doc.view(); view != nil {
			view.Scroll(action.Delta)
		}
	}
	return nil
}

func (m *Model) previewDocKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.preview.doc == nil {
		return false, nil
	}
	// A live editor owns the pane outright, before the search surfaces and
	// before the document's own keys: every key in it is on its way to vim.
	if m.preview.doc.editing() {
		return m.handlePreviewDocEditKey(msg)
	}
	if m.PreviewInteractive() {
		return false, nil
	}
	key := msg.String()
	if m.preview.doc.focused {
		// ctrl+c is the host's, even mid-query — the same rule the focused
		// filter states above. This browser answers before internal/app's
		// text-input level, which is where every other surface's ctrl+c is
		// intercepted, so a search here must hand it back itself or it is the
		// one place the quit confirmation is unreachable.
		if key == "ctrl+c" && (m.preview.doc.mode != nil || m.previewDocFindActive()) {
			return false, nil
		}
		// A live pane search surface owns every key in the pane, before the
		// document's own keys and before the browser's: `/` here is a query
		// character, `q` is a q.
		if m.preview.doc.mode != nil {
			return true, m.handlePreviewDocSearchKey(msg)
		}
		// In-file search is the same rule one level down, and docview answers
		// handled=false when no search is running.
		if view := m.preview.doc.view(); view != nil {
			if handled, cmd := view.HandleSearchKey(msg); handled {
				return true, cmd
			}
		}
		// Before the pane's own keys: esc clears a selection rather than closing
		// the pane out from under it.
		if cmd, handled := m.handlePreviewDocSelectionKey(msg); handled {
			return true, cmd
		}
		switch key {
		case "/":
			if view := m.preview.doc.view(); view != nil {
				view.StartSearch()
			}
			return true, nil
		case "e":
			if hostID := m.previewRemoteHostID(); hostID != "" {
				return true, remoteDocumentUnsupported(hostID, "Inline editing")
			}
			return true, m.enterPreviewDocEdit()
		case "ctrl+p":
			if hostID := m.previewRemoteHostID(); hostID != "" {
				return true, remoteDocumentUnsupported(hostID, "File finding")
			}
			return true, m.openPreviewDocFinder()
		case "f":
			if hostID := m.previewRemoteHostID(); hostID != "" {
				return true, remoteDocumentUnsupported(hostID, "Project search")
			}
			return true, m.openPreviewDocProjectSearch()
		case "q", "esc":
			return true, m.closePreviewDoc()
		case "m":
			if view := m.preview.doc.view(); view != nil {
				view.ToggleRenderMode()
			}
			return true, nil
		case "x":
			return true, m.closePreviewDocTab()
		case "{":
			return true, m.cyclePreviewDocTab(-1)
		case "}":
			return true, m.cyclePreviewDocTab(1)
		case "Y", "shift+y":
			if view := m.preview.doc.view(); view != nil {
				return true, docview.YankPath(view.Title())
			}
			return true, nil
		case "y":
			if view := m.preview.doc.view(); view != nil {
				if m.previewRemoteHostID() != "" {
					return true, view.YankSelectionOrLoaded()
				}
				return true, view.YankSelectionOrContents()
			}
			return true, nil
		case "r":
			return true, m.reloadPreviewDoc()
		case "enter", interactiveEnterKeyAlt:
			m.preview.doc.focused = false
			return true, m.enterPreviewInteractive()
		}
		if view := m.preview.doc.view(); view != nil && view.HandleKey(msg) {
			return true, nil
		}
	}
	return false, nil
}

func (m *Model) renderPreviewDoc(doc *previewDoc, box termpreview.Box) string {
	// Where the box is, not only how big it is: the editor's origin and the PTY
	// size are both read back from it.
	doc.box = box
	if cmd := doc.resizePreviewDocEdit(); cmd != nil {
		m.queuePreviewCmd(cmd)
	}
	if doc.editing() {
		return m.renderPreviewDocEdit(doc, box)
	}
	view := doc.view()
	contentHeight := max(box.H-termpreview.HeaderRows, 0)
	tabsWidth := m.reserveHeader(box.W, true).TabsWidth
	focused := m.PreviewFocused() && doc.focused
	strip := docview.LayoutTabStrip(doc.tabs, tabsWidth, focused)
	if doc.mode != nil {
		// A pane taking search keystrokes says so where it says which file it
		// holds, exactly as the project workspace does.
		strip = docview.LayoutSearchTabStrip(doc.tabs, doc.mode.HeaderLabel(), tabsWidth, focused)
	}
	header := m.composePreviewHeader(m.previewDocHeaderTabs(strip.HoverClose(m.tabCloseHoverIn(panelayout.Document)).Row), box.W, panelayout.Document)
	body := ""
	if view != nil {
		m.bindPreviewDocSelection(view, box)
		body = m.preparedPreviewDocBody(doc, box.X, box.Y+termpreview.HeaderRows)
	}
	if contentHeight <= 0 {
		return header
	}
	// A live search surface is composited over the body as a modal scoped to
	// that box, leaving the pane's own header row uncovered: it is where the
	// pane says it is in Find or Search mode.
	if doc.mode != nil {
		body = m.renderPreviewDocSearchOverlay(doc, body, termpreview.Box{
			X: box.X, Y: box.Y + termpreview.HeaderRows, W: box.W, H: contentHeight,
		})
	}
	return header + "\n" + body
}

func (m *Model) composePreviewHeader(tabsRow string, width int, kind panelayout.Kind) string {
	leaf := panelayout.FirstOfKind(m.preview.paneRoot, kind)
	leafID := 0
	if leaf != nil {
		leafID = leaf.ID
	}
	return m.composeHeader(tabsRow, width, kind != panelayout.Terminal,
		leafID != 0 && m.hoverPreviewLayout == leafID,
		m.previewCloseHover && m.hoverPreviewClose == kind)
}

func (m *Model) registerPreviewCloseRegionFor(leafID int, kind panelayout.Kind, box termpreview.Box) {
	reserve := m.reserveHeader(box.W, true)
	if reserve.CloseW < 1 {
		return
	}
	m.workspacesMouse.HitMap.AddRect(
		previewPaneCloseKind,
		box.X+reserve.CloseCol, box.Y, reserve.CloseW, 1,
		previewPaneCloseHit{Kind: kind, LeafID: leafID},
	)
}

func (m *Model) closePreviewPaneHit(hit previewPaneCloseHit) tea.Cmd {
	if hit.Kind == panelayout.Shell {
		return m.requestClosePreviewShellLeaf(hit.LeafID)
	}
	return m.closePreviewPane(hit.Kind)
}

func (m *Model) closePreviewPane(kind panelayout.Kind) tea.Cmd {
	switch kind {
	case panelayout.Document:
		return m.closePreviewDoc()
	case panelayout.Issue:
		return m.closePreviewIssue()
	case panelayout.Diff:
		return m.closePreviewDiff()
	case panelayout.Resource:
		return m.closePreviewResource()
	default:
		return nil
	}
}
