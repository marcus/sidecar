package overview

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/clip"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/mouse"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/paneframe"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/tabs"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/workspacediff"
)

const (
	previewDiffRegionKind = "global-preview-diff"
	previewDiffTabKind    = "global-preview-diff-tab"
)

type previewDiffTabHit struct {
	Index int
	Close bool
}

// previewDiff is the memory-only Diff pane beside the selected terminal.
type previewDiff struct {
	tabs    workspacediff.Group
	root    string
	surface string
	focused bool
	// hostNotice is a connected-stale or verb-failure label for a remote
	// Diff that is still showing its last good body.
	hostNotice string
}

func (d *previewDiff) view() *workspacediff.View {
	if d == nil {
		return nil
	}
	return d.tabs.ActiveView()
}

func (m *Model) openPreviewDiff(target workspacediff.Target) tea.Cmd {
	cmd, _ := m.openPreviewDiffResult(target)
	return cmd
}

func (m *Model) openPreviewDiffResult(target workspacediff.Target) (tea.Cmd, error) {
	workspace, ok := m.SelectedWorkspace()
	if !ok {
		return nil, nil
	}
	if target.Identity() == "" {
		target = workspacediff.WorkingTreeTarget()
	}
	if !features.IsEnabled(features.WorkspaceDocPanes.Name) {
		return appmsg.ShowFlash(features.WorkspaceDocPanesDisabledDiff), errors.New(features.WorkspaceDocPanesDisabledDiff)
	}
	if workspace.Remote() {
		ctx, ok := m.previewDeckContext()
		if !ok {
			return nil, nil
		}
		raw := target.Identity()
		if raw == "" {
			raw = workspacediff.IdentityWorkingTree
		}
		ref, err := contentpanes.ResolveDocument(m.previewDeckConfig(ctx).Source, ctx.Source, contentlink.Pending{
			Kind: contentlink.KindDiff, Raw: raw,
		})
		if err != nil || ref.Value == "" {
			if err == nil {
				err = errors.New("git object not found on " + ctx.Source.HostID)
			}
			return remoteContentErrorCmd(err), err
		}
		return m.openPreviewContent(ref, "Diff"), nil
	}
	return m.openPreviewContent(contentlink.Ref{Kind: contentlink.KindDiff, Value: target.Identity()}, "Diff"), nil
}

func (m *Model) applyPreviewDiffSnapshot(msg workspacediff.SnapshotMsg) tea.Cmd {
	var cmds []tea.Cmd
	apply := func(diff *previewDiff) {
		if diff == nil {
			return
		}
		for _, item := range diff.tabs.Items {
			if item.Value == nil {
				continue
			}
			before := item.Value.Revision
			cmds = append(cmds, item.Value.ApplySnapshotMsg(msg, item.Value.WorkDir, item.Value.WorkspaceID))
			if msg.Refresh {
				if msg.NotModified || (msg.Err == nil && msg.Snapshot != nil) {
					diff.hostNotice = ""
				} else if msg.Err != nil && item.Value.Revision == before {
					diff.hostNotice = remoteDocumentStaleNotice
				}
			} else if msg.Err == nil {
				diff.hostNotice = ""
			}
		}
	}
	apply(m.preview.diff)
	if cached, ok := m.preview.paneCache[msg.WorkspaceID]; ok {
		apply(cached.diff)
	}
	return tea.Batch(cmds...)
}

func (m *Model) applyPreviewDiffRange(msg workspacediff.RangeMsg) tea.Cmd {
	var cmds []tea.Cmd
	apply := func(diff *previewDiff) {
		if diff == nil {
			return
		}
		for _, item := range diff.tabs.Items {
			if item.Value != nil {
				cmds = append(cmds, item.Value.ApplyRangeMsg(msg))
			}
		}
	}
	apply(m.preview.diff)
	if cached, ok := m.preview.paneCache[msg.WorkspaceID]; ok {
		apply(cached.diff)
	}
	return tea.Batch(cmds...)
}

func (m *Model) applyPreviewDiffCommit(msg workspacediff.CommitDetailMsg) tea.Cmd {
	var cmds []tea.Cmd
	apply := func(diff *previewDiff) {
		if diff == nil {
			return
		}
		for _, item := range diff.tabs.Items {
			if item.Value != nil {
				cmds = append(cmds, item.Value.ApplyCommitDetail(msg))
			}
		}
	}
	apply(m.preview.diff)
	if cached, ok := m.preview.paneCache[msg.WorkspaceID]; ok {
		apply(cached.diff)
	}
	return tea.Batch(cmds...)
}

func (m *Model) applyPreviewDiffWorkingTreeFile(msg workspacediff.WorkingTreeFileMsg) tea.Cmd {
	var cmds []tea.Cmd
	apply := func(diff *previewDiff) {
		if diff == nil {
			return
		}
		for _, item := range diff.tabs.Items {
			if item.Value != nil {
				cmds = append(cmds, item.Value.ApplyWorkingTreeFile(msg))
			}
		}
	}
	apply(m.preview.diff)
	if cached, ok := m.preview.paneCache[msg.WorkspaceID]; ok {
		apply(cached.diff)
	}
	return tea.Batch(cmds...)
}

func (m *Model) applyPreviewDiffFile(msg workspacediff.CommitFileDiffMsg) tea.Cmd {
	var cmds []tea.Cmd
	apply := func(diff *previewDiff) {
		if diff == nil {
			return
		}
		for _, item := range diff.tabs.Items {
			if item.Value != nil {
				cmds = append(cmds, item.Value.ApplyCommitFileDiff(msg))
			}
		}
	}
	apply(m.preview.diff)
	if cached, ok := m.preview.paneCache[msg.WorkspaceID]; ok {
		apply(cached.diff)
	}
	return tea.Batch(cmds...)
}

func (m *Model) closePreviewDiff() tea.Cmd {
	if m.preview.deck == nil {
		return nil
	}
	m.preview.deck.FocusLeaf(m.preview.deck.Leaf(panelayout.Diff))
	for m.preview.deck.Leaf(panelayout.Diff) != 0 {
		m.preview.deck.CloseActive()
	}
	if ctx, ok := m.previewDeckContext(); ok {
		m.syncPreviewDeckProjection(ctx)
	}
	if m.preview.doc != nil {
		m.focusPreviewPane(panelayout.Document)
		return m.syncTerminalGeometry()
	}
	if m.preview.issue != nil {
		m.focusPreviewPane(panelayout.Issue)
		return m.syncTerminalGeometry()
	}
	if m.preview.resource != nil {
		m.focusPreviewPane(panelayout.Resource)
		return m.syncTerminalGeometry()
	}
	return tea.Batch(m.focusList(), m.syncTerminalGeometry())
}

func (m *Model) closePreviewDiffTab() tea.Cmd {
	if m.preview.diff == nil {
		return nil
	}
	return m.closePreviewDiffTabAt(m.preview.diff.tabs.Active)
}

func (m *Model) closePreviewDiffTabAt(index int) tea.Cmd {
	if m.preview.deck == nil {
		return nil
	}
	m.preview.deck.CloseTab(m.preview.deck.Leaf(panelayout.Diff), index)
	return m.finishPreviewDeckClose()
}

func (m *Model) cyclePreviewDiffTab(delta int) tea.Cmd {
	if m.preview.diff != nil && (m.preview.deck == nil || len(m.preview.diff.tabs.Items) != previewDeckTabCount(m.preview.deck, panelayout.Diff)) {
		m.preview.diff.tabs.Cycle(delta)
		return nil
	}
	if m.preview.deck == nil || !m.preview.deck.FocusLeaf(m.preview.deck.Leaf(panelayout.Diff)) {
		return nil
	}
	cmd := m.preview.deck.CycleTab(delta)
	if ctx, ok := m.previewDeckContext(); ok {
		m.syncPreviewDeckProjection(ctx)
	}
	return cmd
}

func (m *Model) clickPreviewDiffTab(index int) tea.Cmd {
	if m.preview.deck == nil {
		return nil
	}
	cmd := m.preview.deck.SelectTab(m.preview.deck.Leaf(panelayout.Diff), index)
	if ctx, ok := m.previewDeckContext(); ok {
		m.syncPreviewDeckProjection(ctx)
	}
	return cmd
}

func (m *Model) renderPreviewDiff(diff *previewDiff, box termpreview.Box) string {
	view := diff.view()
	contentHeight := max(box.H-termpreview.HeaderRows, 0)
	focused := m.PreviewFocused() && diff.focused
	if view != nil {
		view.SetSize(box.W, contentHeight)
	}
	header := m.composePreviewHeader(m.previewHostHeaderTabs(layoutPreviewDiffStrip(diff.tabs, m.reserveHeader(box.W, true).TabsWidth, focused).HoverClose(m.tabCloseHoverIn(panelayout.Diff)).Row, diff.hostNotice), box.W, panelayout.Diff)
	if contentHeight <= 0 {
		return header
	}
	body := ""
	if view != nil {
		m.bindPreviewPaneSelection(view, box)
		body = view.Render(box.W, contentHeight, workspacediff.RenderOpts{
			Truncate: func(s string, w int, _ string) string { return termpreview.TruncateANSI(s, w) },
			Handle:   m.dividerHandleState(previewDiffDividerKind, 0),
		})
	}
	return header + "\n" + body
}

func layoutPreviewDiffStrip(group workspacediff.Group, width int, focused bool) tabs.Strip {
	labels := make([]tabs.Label, len(group.Items))
	for i, item := range group.Items {
		text := item.Key
		if item.Value != nil {
			text = item.Value.Target.TabLabel()
		}
		labels[i] = tabs.Label{Text: text}
	}
	return tabs.LayoutStrip(labels, group.Active, width, focused, func(text string, _, _, maxWidth int, _ bool) string {
		if maxWidth < 1 {
			return ""
		}
		return text
	})
}

// registerPreviewDiffRegion covers the Diff leaf's INNER box, which is the
// lowest-priority target inside it.
func (m *Model) registerPreviewDiffRegion(diffBox termpreview.Box) {
	if m.preview.diff == nil {
		return
	}
	m.workspacesMouse.HitMap.AddRect(
		previewDiffRegionKind,
		diffBox.X, diffBox.Y, diffBox.W, diffBox.H,
		previewDiffRegionKind,
	)
}

func (m *Model) registerPreviewDiffTabRegions(diffBox termpreview.Box) {
	if m.preview.diff == nil {
		return
	}
	focused := m.PreviewFocused() && m.preview.diff.focused
	strip := layoutPreviewDiffStrip(m.preview.diff.tabs, m.reserveHeader(diffBox.W, true).TabsWidth, focused)
	strip.RegisterHits(func(col, width, index int, close bool) {
		m.workspacesMouse.HitMap.AddRect(
			previewDiffTabKind,
			diffBox.X+col, diffBox.Y, width, 1,
			previewDiffTabHit{Index: index, Close: close},
		)
	})
}

// registerPreviewDiffLeafHits registers the targets the diff view owns inside
// its own body: the file rows and its list/hunk divider.
func (m *Model) registerPreviewDiffLeafHits(diffBox termpreview.Box) {
	if m.preview.diff == nil {
		return
	}
	view := m.preview.diff.view()
	if view == nil {
		return
	}
	body, ok := diffLeafBody(diffBox)
	if !ok {
		return
	}
	view.SetSize(body.W, body.H)
	for _, hit := range view.FileHits(body) {
		m.workspacesMouse.HitMap.AddRect(hit.ID, hit.Rect.X, hit.Rect.Y, hit.Rect.W, hit.Rect.H, hit.Data)
	}
	if d := view.DividerHit(body); d.W > 0 && d.H > 0 {
		m.workspacesMouse.HitMap.AddRect(previewDiffDividerKind, d.X, d.Y, d.W, d.H, previewDiffDividerHit{})
	}
}

func (m *Model) previewDiffLeafBody() (mouse.Rect, bool) {
	if m.preview.diff == nil {
		return mouse.Rect{}, false
	}
	peer, ok := m.previewPeerBox()
	if !ok {
		return mouse.Rect{}, false
	}
	diffBox, ok := m.previewPaneBox(panelayout.Diff, peer)
	if !ok {
		return mouse.Rect{}, false
	}
	return diffLeafBody(diffBox)
}

// diffLeafBody is the Diff leaf's box below its header row.
func diffLeafBody(diffBox termpreview.Box) (mouse.Rect, bool) {
	if diffBox.W < 1 {
		return mouse.Rect{}, false
	}
	body := mouse.Rect{
		X: diffBox.X, Y: diffBox.Y + termpreview.HeaderRows,
		W: diffBox.W, H: max(diffBox.H-termpreview.HeaderRows, 0),
	}
	if body.H < 1 {
		return mouse.Rect{}, false
	}
	return body, true
}

func (m *Model) previewDiffDragView() *workspacediff.View {
	if m.preview.diff != nil {
		if view := m.preview.diff.view(); view != nil {
			return view
		}
	}
	return &m.diff
}

func (m *Model) previewDiffDragWidth() int {
	if body, ok := m.previewDiffLeafBody(); ok {
		return body.W
	}
	if peer, ok := m.previewPeerBox(); ok {
		return paneframe.Inset(peer).W
	}
	return m.width
}

func (m *Model) previewDiffPaneKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	diff := m.preview.diff
	if diff == nil || !diff.focused || m.PreviewInteractive() {
		return false, nil
	}
	// Before the pane's own keys: esc clears a selection rather than closing
	// the pane out from under it, and the copy chord must not fall through to
	// a diff key that happens to share it.
	if cmd, handled := m.handlePreviewPaneSelectionKey(panelayout.Diff, msg); handled {
		return true, cmd
	}
	switch msg.String() {
	case "q", "esc":
		return true, m.closePreviewDiff()
	case "x":
		return true, m.closePreviewDiffTab()
	case "{":
		return true, m.cyclePreviewDiffTab(-1)
	case "}":
		return true, m.cyclePreviewDiffTab(1)
	case "Y", "shift+y":
		return true, m.yankPreviewDiff()
	}
	view := diff.view()
	if view == nil {
		return true, nil
	}
	cmd, handled := view.HandleKey(msg)
	m.persistDiffViewModeFrom(view)
	return handled, cmd
}

func (m *Model) persistDiffViewModeFrom(view *workspacediff.View) {
	if view == nil {
		return
	}
	m.diff.ViewMode = view.ViewMode
	m.persistDiffViewMode()
}

func (m *Model) yankPreviewDiff() tea.Cmd {
	view := m.preview.diff.view()
	if view == nil {
		return nil
	}
	ident := view.Target.Identity()
	if ident == "" {
		return nil
	}
	return clip.Copy(ident, func(r clip.Result) tea.Msg {
		return appmsg.FlashMsg{Text: r.Message("Yanked: " + ident)}
	})
}

func (m *Model) handlePreviewDiffMouse(action mouse.MouseAction) tea.Cmd {
	if tab, ok := action.Region.Data.(previewDiffTabHit); ok {
		if action.Type == mouse.ActionClick || action.Type == mouse.ActionDoubleClick {
			if tab.Close {
				return m.closePreviewDiffTabAt(tab.Index)
			}
			return m.clickPreviewDiffTab(tab.Index)
		}
		if action.Type == mouse.ActionScrollUp || action.Type == mouse.ActionScrollDown {
			if view := m.preview.diff.view(); view != nil {
				view.ScrollContent(action.Delta, view.Height())
			}
		}
		return nil
	}
	if m.preview.diff == nil {
		return nil
	}
	if workspacediff.IsBodyRegion(action.Region.ID) {
		m.focusPreviewPane(panelayout.Diff)
		view := m.preview.diff.view()
		if view == nil {
			return nil
		}
		switch action.Type {
		case mouse.ActionClick:
			cmd := view.HandleClick(action.Region.ID, action.Region.Data)
			if isPreviewDiffTextRegion(action.Region.ID) {
				// The patch body and the panes' own backgrounds are text. The
				// click that focused the pane has already happened; the press
				// also arms a selection so the motion after it selects.
				return tea.Batch(cmd, m.pressPreviewPaneSelection(panelayout.Diff, action))
			}
			return cmd
		case mouse.ActionDoubleClick:
			if isPreviewDiffTextRegion(action.Region.ID) {
				// Word by double click, line by triple, exactly as the
				// terminal beside it answers the same gesture.
				return m.pressPreviewPaneSelection(panelayout.Diff, action)
			}
			return view.HandleDoubleClick(action.Region.ID, action.Region.Data)
		case mouse.ActionScrollUp, mouse.ActionScrollDown:
			return view.HandleWheel(action.Region.ID, action.Delta)
		}
		return nil
	}
	kind, _ := regionKind(action.Region)
	if kind != previewDiffRegionKind {
		return nil
	}
	switch action.Type {
	case mouse.ActionClick, mouse.ActionDoubleClick, mouse.ActionTripleClick:
		m.focusPreviewPane(panelayout.Diff)
		return m.pressPreviewPaneSelection(panelayout.Diff, action)
	case mouse.ActionScrollUp, mouse.ActionScrollDown:
		if view := m.preview.diff.view(); view != nil {
			view.ScrollContent(action.Delta, view.Height())
		}
	}
	return nil
}

// isPreviewDiffTextRegion reports the Diff regions whose cells are text rather
// than a row the pane answers a click on. A file row and a commit row are
// targets; the patch body and the panes' own backgrounds are prose.
func isPreviewDiffTextRegion(regionID string) bool {
	switch regionID {
	case workspacediff.RegionDiffPane, workspacediff.RegionCommitDiff, workspacediff.RegionFileListPane:
		return true
	default:
		return false
	}
}
