package overview

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/inlineedit"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/mouse"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/paneframe"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/panesearch"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/targetactivation"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacediff"
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
	Kind panelayout.Kind
}

func isPreviewDocRegion(kind string) bool {
	return kind == previewDocRegionKind || kind == previewDocTabKind
}

// previewDocTabHit is the tab stored on the document header region.
type previewDocTabHit int

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

func (m *Model) previewLinkSpans(line string) []terminallink.Span {
	root := m.previewResolveRoot()
	// Matchers go through on every scan, including the rootless one: an
	// external resource key is recognized from the line alone, and an empty
	// matcher list — no provider ready — leaves the line as plain text.
	if root == "" {
		return terminallink.ScanWith(line, terminallink.Options{Matchers: m.resourceMatchers})
	}
	return terminallink.ScanWith(line, terminallink.Options{
		Resolve: func(raw string) (string, terminallink.Extra, bool) {
			display, _, ok := terminallink.ResolveFile(root, raw)
			if !ok {
				return "", terminallink.Extra{}, false
			}
			return display, terminallink.Extra{Raw: raw}, true
		},
		ResolveDiff: m.previewDiffResolver(root),
		Matchers:    m.resourceMatchers,
	})
}

func (m *Model) previewDiffResolver(root string) terminallink.DiffResolver {
	if m.preview.paneRoot == nil || root == "" {
		return nil
	}
	buffer := m.previewBuffer()
	if buffer == nil {
		return nil
	}
	memo := m.ensurePreviewLinkMemo(root, buffer)
	return func(raw string) (string, terminallink.Extra, bool) {
		resolution, found := memo.specs[raw]
		if !found {
			if memo.newSpecs >= terminallink.MaxNewDiffResolves {
				return "", terminallink.Extra{}, false
			}
			memo.newSpecs++
			value, ok := m.resolvePreviewSpec(root, raw)
			resolution = previewSpecResolution{value: value, ok: ok}
			memo.specs[raw] = resolution
		}
		if !resolution.ok {
			return "", terminallink.Extra{}, false
		}
		if resolution.value == "" {
			return raw, terminallink.Extra{Raw: raw}, true
		}
		return resolution.value, terminallink.Extra{Raw: raw}, true
	}
}

func (m *Model) ensurePreviewLinkMemo(root string, buffer *tty.OutputBuffer) *previewLinkMemo {
	revision := buffer.Revision()
	memo := &m.preview.linkMemo
	if memo.root != root || memo.buffer != buffer || memo.revision != revision || memo.specs == nil {
		m.preview.linkMemo = previewLinkMemo{
			root: root, buffer: buffer, revision: revision,
			specs: make(map[string]previewSpecResolution),
		}
	}
	return &m.preview.linkMemo
}

func (m *Model) resolvePreviewSpec(root, raw string) (string, bool) {
	if m.previewSpecResolver != nil {
		return m.previewSpecResolver(root, raw)
	}
	value, _, ok := terminallink.ResolveGitSpec(root, raw)
	return value, ok
}

func (m *Model) decoratePreviewLine(line string, _ int) string {
	line = terminallink.StripOSC8(line)
	return terminallink.Decorate(line, m.decoratedPreviewSpans(line))
}

// decoratedPreviewSpans keeps exactly the kinds this surface activates. The
// answer is terminallink's rather than a list restated here, because the two
// hand-written copies of it — one for drawing, one for hit testing — were one
// new kind away from disagreeing about what is clickable.
func (m *Model) decoratedPreviewSpans(line string) []terminallink.Span {
	spans := m.previewLinkSpans(line)
	bound := make([]terminallink.Span, 0, len(spans))
	for _, span := range spans {
		if terminallink.Activatable(span.Kind) {
			bound = append(bound, span)
		}
	}
	return bound
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
	for _, span := range m.previewLinkSpans(line) {
		if !terminallink.Activatable(span.Kind) {
			continue
		}
		if cell.Col >= span.StartCol && cell.Col <= span.EndCol {
			return span, true
		}
	}
	return terminallink.Span{}, false
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
	case targetactivation.PlanOpenDiff:
		cmd = m.activatePreviewDiff(plan.Spec)
	case targetactivation.PlanAttachSession:
		cmd = m.attachPreviewSession(plan.Session)
	case targetactivation.PlanOpenResource:
		cmd = m.activatePreviewResource(resourceview.Ref{
			Instance: plan.Provider,
			Matcher:  plan.Matcher,
			Locator:  plan.Locator,
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
		targetactivation.PlanOpenIssue, targetactivation.PlanOpenDiff,
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
// A session no row is running is not attachable, and the caller treats a nil
// command as "this click did nothing", which is what an unknown session is.
func (m *Model) attachPreviewSession(session string) tea.Cmd {
	if strings.TrimSpace(session) == "" {
		return nil
	}
	ids := make([]string, 0, len(m.catalog))
	for id := range m.catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if m.catalog[id].TmuxName != session {
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
	target := uirequest.DiffTarget(previewDiffPath(workspace), raw)
	if target.Kind != workspacediff.TargetCommit && target.Kind != workspacediff.TargetRange {
		return nil
	}
	return m.openPreviewDiff(target)
}

// openPreviewDocTarget opens a file target in the preview document pane. The
// target's Value is the token as the text wrote it; it is re-resolved against
// this surface's own root, so a target that names nothing here opens nothing.
func (m *Model) openPreviewDocTarget(target uirequest.Target) tea.Cmd {
	workspace, ok := m.SelectedWorkspace()
	if !ok {
		return nil
	}
	root := workspace.Path
	display, abs, ok := terminallink.ResolveFile(root, target.Value)
	if !ok {
		return nil
	}
	file, err := openPreviewFile(root, display, abs)
	if err != nil {
		return nil
	}
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	leafID, refusal := m.ensurePreviewPane(panelayout.Document, "Document")
	if refusal != nil {
		return refusal
	}
	if leafID == 0 {
		return nil
	}
	wasInteractive := m.PreviewInteractive()
	if m.preview.doc == nil || m.preview.doc.surface != workspace.ID {
		// The pane being replaced may hold a session; it goes with the pane.
		m.preview.doc.releaseEdit()
		m.preview.doc = &previewDoc{epoch: m.nextPreviewContentEpoch()}
	}
	m.preview.doc.root = root
	m.preview.doc.surface = workspace.ID
	m.focusPreviewPane(panelayout.Document)

	var load tea.Cmd
	if idx := m.preview.doc.tabs.IndexOf(display); idx >= 0 {
		load = m.selectPreviewDocTab(idx, target.Line, file)
		file = nil
	} else {
		viewer := docview.New(nil)
		load = viewer.LoadFile(m.preview.doc.allocID(), file, display, target.Line, m.preview.doc.epoch)
		file = nil
		applyPreviewDocRenderMode(viewer, display, target.Line)
		m.preview.doc.tabs.Append(viewer)
	}

	var cmds []tea.Cmd
	if wasInteractive {
		cmds = append(cmds, m.exitPreviewInteractive())
	}
	cmds = append(cmds, wrapPreviewDocLoad(load, workspace.ID), m.syncTerminalGeometry())
	return tea.Batch(cmds...)
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
	if m.preview.doc == nil {
		return nil
	}
	m.focusPreviewPane(panelayout.Document)
	if index == m.preview.doc.tabs.Active {
		return nil
	}
	return wrapPreviewDocLoad(m.selectPreviewDocTab(index, 0, nil), m.preview.doc.surface)
}

func (m *Model) cyclePreviewDocTab(delta int) tea.Cmd {
	if m.preview.doc == nil || len(m.preview.doc.tabs.Items) < 2 {
		return nil
	}
	m.preview.doc.tabs.Cycle(delta)
	return wrapPreviewDocLoad(m.ensurePreviewDocTabLoaded(), m.preview.doc.surface)
}

func (m *Model) closePreviewDocTab() tea.Cmd {
	if m.preview.doc == nil {
		return nil
	}
	// Closing the tab takes the file the editor is holding away; ask first.
	if m.guardPreviewDocEdit(func() tea.Cmd { return m.closePreviewDocTab() }) {
		return nil
	}
	if len(m.preview.doc.tabs.Items) <= 1 {
		return m.closePreviewDoc()
	}
	m.preview.doc.tabs.CloseActive()
	return wrapPreviewDocLoad(m.ensurePreviewDocTabLoaded(), m.preview.doc.surface)
}

func (m *Model) ensurePreviewDocTabLoaded() tea.Cmd {
	doc := m.preview.doc
	if doc == nil {
		return nil
	}
	view := doc.view()
	if view == nil || !view.NeedsLoad() {
		return nil
	}
	rendered := view.Rendered()
	wrap := view.Wrap()
	cmd := view.Load(doc.allocID(), doc.root, view.Title(), 0, doc.epoch)
	view.SetRendered(rendered)
	view.SetWrap(wrap)
	return cmd
}

func openPreviewFile(root, display, abs string) (*os.File, error) {
	if display != "" && !filepath.IsAbs(filepath.FromSlash(display)) {
		return terminallink.OpenRegular(filepath.Join(root, filepath.FromSlash(display)))
	}
	return terminallink.OpenRegular(abs)
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
		if item.View != nil && item.View.SetResult(msg.LoadedMsg) {
			return
		}
	}
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
	m.preview.doc = nil
	if leaf := panelayout.FirstOfKind(m.preview.paneRoot, panelayout.Document); leaf != nil {
		m.preview.paneRoot, m.preview.paneFocus = panelayout.Close(m.preview.paneRoot, leaf.ID)
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
	if leaf.Kind != panelayout.Terminal {
		cmd = m.exitPreviewInteractive()
	}
	m.preview.paneFocus = leaf.ID
	m.preview.focus = focusPreview
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

func (m *Model) ensurePreviewPane(kind panelayout.Kind, name string) (int, tea.Cmd) {
	if m.preview.paneRoot == nil {
		m.resetActivePreviewPanes()
	}
	plan, ok := panelayout.PlanOpen(m.preview.paneRoot, kind, m.lastPreviewBoxes())
	if !ok {
		return 0, nil
	}
	plan = panelayout.ApplyAxisOverride(plan, m.openSplit)
	if plan.Retarget != 0 {
		return plan.Retarget, nil
	}
	peer, ok := m.previewPeerBox()
	if !ok {
		return 0, nil
	}
	id := m.preview.paneNextID
	trial := panelayout.Clone(m.preview.paneRoot)
	trial, focus := panelayout.SplitLeaf(trial, plan.Split, plan.Axis, &panelayout.Node{ID: id, Kind: kind, ContentID: id})
	if focus != id {
		return 0, nil
	}
	if _, _, fits := panelayout.LayoutPanes(trial, peer, previewPaneFloors()); !fits {
		dimension := "wider"
		if plan.Axis == panelayout.Rows {
			dimension = "taller"
		}
		return 0, appmsg.ShowToast(name+" pane needs a "+dimension+" window; layout left unchanged", 3*time.Second)
	}
	m.preview.paneRoot, focus = panelayout.SplitLeaf(m.preview.paneRoot, plan.Split, plan.Axis, &panelayout.Node{ID: id, Kind: kind, ContentID: id})
	if focus != id {
		return 0, nil
	}
	m.preview.paneFocus = focus
	m.preview.paneNextID = panelayout.MaxID(m.preview.paneRoot) + 1
	return id, nil
}

// layoutPreviewPanes places the tree in peer, a surface-local OUTER rectangle.
// Every placement it returns is therefore OUTER: the box a leaf's own border is
// drawn on, not the box its content draws in.
func (m *Model) layoutPreviewPanes(peer termpreview.Box) (panelayout.Layout, bool) {
	if m.preview.paneRoot == nil {
		m.resetActivePreviewPanes()
	}
	return panelayout.LayoutTree(m.preview.paneRoot, peer, previewPaneFloors(), m.preview.paneFocus)
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

func (m *Model) previewTerminalBox() (termpreview.Box, bool) {
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
	for _, tab := range docview.LayoutTabStrip(m.preview.doc.tabs, ui.ReserveHeaderClose(docBox.W).TabsWidth, m.PreviewFocused() && m.preview.doc.focused).Tabs {
		m.workspacesMouse.HitMap.AddRect(previewDocTabKind, docBox.X+tab.Col, docBox.Y, tab.Width, 1, previewDocTabHit(tab.Index))
	}
}

func (m *Model) handlePreviewDocMouse(action mouse.MouseAction) tea.Cmd {
	if tab, ok := action.Region.Data.(previewDocTabHit); ok {
		if action.Type == mouse.ActionClick || action.Type == mouse.ActionDoubleClick {
			return m.clickPreviewDocTab(int(tab))
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
			return true, m.enterPreviewDocEdit()
		case "ctrl+p":
			return true, m.openPreviewDocFinder()
		case "f":
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
		case "r":
			// Refresh rebuilds the preview and would drop this document.
			return true, nil
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
	if view != nil {
		view.SetSize(box.W, contentHeight)
	}
	tabsWidth := ui.ReserveHeaderClose(box.W).TabsWidth
	focused := m.PreviewFocused() && doc.focused
	strip := docview.LayoutTabStrip(doc.tabs, tabsWidth, focused)
	if doc.mode != nil {
		// A pane taking search keystrokes says so where it says which file it
		// holds, exactly as the project workspace does.
		strip = docview.LayoutSearchTabStrip(doc.tabs, doc.mode.HeaderLabel(), tabsWidth, focused)
	}
	header := m.composePreviewHeader(strip.Row, box.W, panelayout.Document)
	body := ""
	if view != nil {
		m.bindPreviewDocSelection(view, box)
		body = view.View()
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
	return ui.ComposeHeaderClose(tabsRow, width, m.previewCloseHover && m.hoverPreviewClose == kind)
}

func (m *Model) registerPreviewCloseRegion(kind panelayout.Kind, box termpreview.Box) {
	reserve := ui.ReserveHeaderClose(box.W)
	if reserve.CloseW < 1 {
		return
	}
	m.workspacesMouse.HitMap.AddRect(
		previewPaneCloseKind,
		box.X+reserve.CloseCol, box.Y, reserve.CloseW, 1,
		previewPaneCloseHit{Kind: kind},
	)
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
