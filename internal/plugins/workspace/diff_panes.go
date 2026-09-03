package workspace

import (
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/clip"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/mouse"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/workspacediff"
)

// diffPane is one Diff leaf's tab group. The pane tree points at this,
// not at a single view. The surface is what lets a selection change collapse
// the leaf rather than carry diffs from one shell into another.
type diffPane struct {
	leafID  int
	root    string
	surface string
	tabs    workspacediff.Group
}

func (d *diffPane) view() *workspacediff.View {
	if d == nil {
		return nil
	}
	return d.tabs.ActiveView()
}

// activeDiffPane returns the first live Diff leaf. A second Diff open
// retargets this leaf rather than splitting again.
func (p *Plugin) activeDiffPane() (*diffPane, *PaneNode) {
	for id, diff := range p.diffs {
		if diff == nil {
			continue
		}
		if leaf := FindPane(p.paneRoot, diff.leafID); leaf != nil && leaf.Kind == PaneDiff && leaf.ContentID == id {
			return diff, leaf
		}
	}
	return nil, nil
}

// showDiffCmd opens the working-tree Diff leaf on the selected surface.
func (p *Plugin) showDiffCmd() tea.Cmd {
	if p.paneRoot == nil {
		return appmsg.ShowFlash(features.WorkspaceDocPanesDisabledDiff)
	}
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		return nil
	}
	return p.openDiffPaneForSurface(root, surface, workspacediff.WorkingTreeTarget())
}

// openDiffPaneForSurface opens target in the pane tree at the place
// planPaneOpen names. The split is trialled on a clone first, exactly as a
// document's is: a box that cannot hold the result leaves the terminal at the
// size it already has rather than reflowing an agent for a pane that will not
// be drawn.
func (p *Plugin) openDiffPaneForSurface(root, surface string, target workspacediff.Target) tea.Cmd {
	if p.paneRoot == nil {
		return appmsg.ShowFlash(features.WorkspaceDocPanesDisabledDiff)
	}
	if p.ctx == nil {
		return nil
	}
	if target.Identity() == "" {
		target = workspacediff.WorkingTreeTarget()
	}
	return p.openWorkspaceContent(root, surface, contentlink.Ref{Kind: contentlink.KindDiff, Value: target.Identity()}, "Diff")
}

// attachDiffPane points the content behind leafID at target and returns its
// load when a new tab is created or a restored tab still needs one.
func (p *Plugin) attachDiffPane(leafID int, root, surface string, target workspacediff.Target) tea.Cmd {
	if p.ctx == nil || target.Identity() == "" {
		return nil
	}
	if p.diffs == nil {
		p.diffs = make(map[int]*diffPane)
	}
	pane := p.diffs[leafID]
	if pane == nil {
		pane = &diffPane{leafID: leafID}
		p.diffs[leafID] = pane
	}
	pane.root, pane.surface = root, surface
	return p.openOrFocusDiff(pane, target)
}

func (p *Plugin) newDiffView(target workspacediff.Target) *workspacediff.View {
	view := &workspacediff.View{
		Target:   target,
		ViewMode: p.diff.ViewMode,
		State:    workspacediff.LoadStateLoading,
	}
	if target.Kind == workspacediff.TargetCommit {
		view.Focus = workspacediff.FocusCommitFiles
	}
	if w := state.GetDiffTabFileListWidth(); w > 0 {
		view.SetListWidth(w)
	}
	p.attachDiffPaintTo(view)
	return view
}

func (p *Plugin) diffWorkspaceID(root, surface string) string {
	if wt := p.selectedWorktree(); wt != nil {
		if key := wt.IdentityKey(); key != "" {
			return key
		}
	}
	if surface != "" {
		return surface
	}
	return root
}

func (p *Plugin) selectedDiffBaseRef() string {
	if wt := p.selectedWorktree(); wt != nil {
		return wt.BaseBranch
	}
	return ""
}

// openOrFocusDiff selects an existing tab for target or appends a fresh
// view and loads it.
func (p *Plugin) openOrFocusDiff(pane *diffPane, target workspacediff.Target) tea.Cmd {
	if pane == nil || p.ctx == nil || target.Identity() == "" {
		return nil
	}
	if idx := pane.tabs.Find(target.Identity()); idx >= 0 {
		pane.tabs.Select(idx)
		return p.ensureActiveDiffTabLoaded(pane)
	}
	view := p.newDiffView(target)
	if _, created := pane.tabs.OpenOrFocus(target, view); !created {
		return p.ensureActiveDiffTabLoaded(pane)
	}
	return p.loadDiffView(view, pane.root, pane.surface)
}

func (p *Plugin) applyDiffLoadedToLeaves(msg workspacediff.SnapshotMsg) tea.Cmd {
	var cmds []tea.Cmd
	for _, pane := range p.diffs {
		if pane == nil {
			continue
		}
		for _, item := range pane.tabs.Items {
			if item.Value == nil {
				continue
			}
			cmds = append(cmds, item.Value.ApplySnapshotMsg(msg, item.Value.WorkDir, item.Value.WorkspaceID))
		}
	}
	return tea.Batch(cmds...)
}

func (p *Plugin) applyCommitDetailToLeaves(msg workspacediff.CommitDetailMsg) tea.Cmd {
	var cmds []tea.Cmd
	for _, pane := range p.diffs {
		if pane == nil {
			continue
		}
		for _, item := range pane.tabs.Items {
			if item.Value == nil {
				continue
			}
			cmds = append(cmds, item.Value.ApplyCommitDetail(msg))
		}
	}
	return tea.Batch(cmds...)
}

func (p *Plugin) applyRangeToLeaves(msg workspacediff.RangeMsg) tea.Cmd {
	var cmds []tea.Cmd
	for _, pane := range p.diffs {
		if pane == nil {
			continue
		}
		for _, item := range pane.tabs.Items {
			if item.Value == nil {
				continue
			}
			cmds = append(cmds, item.Value.ApplyRangeMsg(msg))
		}
	}
	return tea.Batch(cmds...)
}

func (p *Plugin) applyCommitFileDiffToLeaves(msg workspacediff.CommitFileDiffMsg) tea.Cmd {
	var cmds []tea.Cmd
	for _, pane := range p.diffs {
		if pane == nil {
			continue
		}
		for _, item := range pane.tabs.Items {
			if item.Value == nil {
				continue
			}
			cmds = append(cmds, item.Value.ApplyCommitFileDiff(msg))
		}
	}
	return tea.Batch(cmds...)
}

// diffFocused is the Diff leaf's own version of issueFocused: the focused
// leaf is a Diff. Without this the keys under a highlighted Diff pane are
// still the agent terminal's — q would open the quit confirmation.
func (p *Plugin) diffFocused() bool {
	diff, _ := p.focusedDiffPane()
	return diff != nil
}

func (p *Plugin) focusedDiffPane() (*diffPane, *PaneNode) {
	if !p.previewLeafFocused() {
		return nil, nil
	}
	leaf := FindPane(p.paneRoot, p.paneFocus)
	if leaf == nil || leaf.Kind != PaneDiff {
		return nil, nil
	}
	diff := p.diffs[leaf.ContentID]
	if diff == nil {
		return nil, nil
	}
	return diff, leaf
}

// handleDiffKey is the focused Diff leaf's input context. A key this pane
// does not own must not fall through to the terminal behind it.
func (p *Plugin) handleDiffKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	diff, _ := p.focusedDiffPane()
	if diff == nil {
		return false, nil
	}
	view := diff.view()
	// Before the pane's own keys: esc clears a selection rather than hiding the
	// pane out from under it, and the copy chord must not fall through to a
	// diff key that happens to share it.
	if view != nil {
		if cmd, handled := p.handlePaneSelectionKey(view, msg); handled {
			return true, cmd
		}
	}
	// Ahead of the view's own keys: this pane used to spend `n` on next-change,
	// which now answers to `<` / `>` so the switcher key means one thing in
	// every pane.
	if handled, cmd := p.paneSwitcherKey(msg); handled {
		return true, cmd
	}
	switch msg.String() {
	case "tab", "shift+tab":
		return false, nil
	case "\\":
		return true, p.toggleSidebarCmd()
	case "q", "esc":
		return true, p.hideDiffPane()
	case "x":
		return true, p.closeActiveDiffTab()
	case "{":
		return true, p.cycleActiveDiffTab(-1)
	case "}":
		return true, p.cycleActiveDiffTab(1)
	case "Y", "shift+y":
		return true, p.yankFocusedDiff()
	case "+":
		return true, p.resizeFocusedLeaf(5)
	case "-":
		return true, p.resizeFocusedLeaf(-5)
	case "f":
		return true, p.openFilePicker()
	default:
		if view == nil {
			return true, nil
		}
		if box, ok := p.paneLeafBox(diff.leafID); ok {
			view.SetSize(box.W, maxInt(box.H-terminalHeaderRows, 0))
		}
		p.attachDiffPaintTo(view)
		beforeActive := diff.tabs.Active
		beforeIdent, beforeScroll := view.Target.Identity(), view.Scroll
		cmd, _ := view.HandleKey(msg)
		p.persistDiffViewModeFrom(view)
		after := diff.view()
		if diff.tabs.Active != beforeActive ||
			(after != nil && (after.Target.Identity() != beforeIdent || after.Scroll != beforeScroll)) {
			p.saveSelectionState()
		}
		return true, cmd
	}
}

func (p *Plugin) persistDiffViewModeFrom(view *workspacediff.View) {
	if view == nil {
		return
	}
	p.diff.ViewMode = view.ViewMode
	p.persistDiffViewMode()
}

func (p *Plugin) cycleActiveDiffTab(delta int) tea.Cmd {
	diff, _ := p.focusedDiffPane()
	if diff == nil || len(diff.tabs.Items) < 2 {
		return nil
	}
	if p.contentDeck != nil && len(diff.tabs.Items) == workspaceDeckTabCount(p.contentDeck, panelayout.Diff) {
		return p.cycleWorkspaceDeckTab(panelayout.Diff, delta)
	}
	diff.tabs.Cycle(delta)
	p.saveSelectionState()
	return p.ensureActiveDiffTabLoaded(diff)
}

func (p *Plugin) closeActiveDiffTab() tea.Cmd {
	diff, leaf := p.focusedDiffPane()
	if diff == nil || leaf == nil {
		return nil
	}
	return p.closeDiffTabAt(diff, leaf.ID, diff.tabs.Active)
}

func (p *Plugin) closeDiffTabAt(diff *diffPane, leafID, index int) tea.Cmd {
	if diff == nil || index < 0 || index >= len(diff.tabs.Items) {
		return nil
	}
	if p.contentDeck != nil && len(diff.tabs.Items) == workspaceDeckTabCount(p.contentDeck, panelayout.Diff) {
		return p.closeWorkspaceDeckTabAt(panelayout.Diff, index)
	}
	if len(diff.tabs.Items) <= 1 {
		return p.closeDiffPane(leafID)
	}
	diff.tabs.CloseAt(index)
	p.saveSelectionState()
	return p.ensureActiveDiffTabLoaded(diff)
}

// takeDiffLeafFocus is the click path onto a Diff leaf: pane-tree focus plus
// the same abandon / leave-interactive sequence a tab-chip click already uses.
func (p *Plugin) takeDiffLeafFocus(leafID int) {
	p.focusLeaf(leafID)
	p.pointer.Abandon()
	if p.viewMode == ViewModeInteractive {
		p.exitInteractiveMode()
	}
}

// focusActiveDiffLeaf resolves the one live Diff leaf and takes focus the way
// Tab / a tab-chip click does. Inner Diff hits carry a file index, not a leaf
// id, so they use this rather than inventing a second focus writer.
func (p *Plugin) focusActiveDiffLeaf() {
	_, leaf := p.activeDiffPane()
	if leaf == nil {
		return
	}
	p.takeDiffLeafFocus(leaf.ID)
}

func (p *Plugin) selectDiffTab(diff *diffPane, leafID, idx int) tea.Cmd {
	if diff == nil {
		return nil
	}
	p.takeDiffLeafFocus(leafID)
	if idx == diff.tabs.Active {
		return p.ensureActiveDiffTabLoaded(diff)
	}
	if p.contentDeck != nil {
		return p.selectWorkspaceDeckTab(panelayout.Diff, idx)
	}
	diff.tabs.Select(idx)
	p.saveSelectionState()
	return p.ensureActiveDiffTabLoaded(diff)
}

func (p *Plugin) clickDiffTab(data any) tea.Cmd {
	hit, ok := data.(diffTabHit)
	if !ok {
		return nil
	}
	leaf := FindPane(p.paneRoot, hit.LeafID)
	if leaf == nil || leaf.Kind != PaneDiff {
		return nil
	}
	diff := p.diffs[leaf.ContentID]
	if diff == nil {
		return nil
	}
	if hit.Close {
		return p.closeDiffTabAt(diff, hit.LeafID, hit.Index)
	}
	return p.selectDiffTab(diff, hit.LeafID, hit.Index)
}

func (p *Plugin) hideDiffPane() tea.Cmd {
	diff, leaf := p.focusedDiffPane()
	if diff == nil || leaf == nil {
		return nil
	}
	return p.hideContentPane(leaf.ID)
}

func (p *Plugin) ensureActiveDiffTabLoaded(diff *diffPane) tea.Cmd {
	if diff == nil || p.ctx == nil {
		return nil
	}
	view := diff.view()
	if view == nil {
		return nil
	}
	if view.State != workspacediff.LoadStateUnknown && view.State != workspacediff.LoadStateLoading && view.State != workspacediff.LoadStateError {
		return nil
	}
	return p.loadDiffView(view, diff.root, diff.surface)
}

func (p *Plugin) loadDiffView(view *workspacediff.View, root, surface string) tea.Cmd {
	if view == nil || p.ctx == nil {
		return nil
	}
	workspaceID := p.diffWorkspaceID(root, surface)
	view.Bind(root, workspaceID, p.ctx.Epoch)
	p.attachDiffPaintTo(view)
	view.State = workspacediff.LoadStateLoading
	switch view.Target.Kind {
	case workspacediff.TargetCommit:
		// A tab with nothing to load must not be left on "Loading" forever.
		if view.Target.A == "" {
			view.State = workspacediff.LoadStateError
			view.Error = "commit tab has no commit"
			return nil
		}
		return view.LoadCommit(view.Target.A)
	case workspacediff.TargetRange:
		if cmd := view.LoadRange(); cmd != nil {
			return cmd
		}
		view.State = workspacediff.LoadStateError
		view.Error = "range tab has no revisions"
		return nil
	default:
		return workspacediff.LoadSnapshotCmdAt(root, p.selectedDiffBaseRef(), workspaceID, p.ctx.Epoch, view.Target.Identity())
	}
}

func (p *Plugin) closeDiffPane(leafID int) tea.Cmd {
	return p.forgetContentPane(leafID)
}

func (p *Plugin) diffPaneHeaderRow(diff *diffPane, width int, focused bool) string {
	return p.composeContentHeader(layoutDiffTabStrip(diff, p.reserveHeader(width, true).TabsWidth, focused).HoverClose(p.hoverTabClose.IndexFor(diffLeafID(diff))).Row, width, diffLeafID(diff), diff != nil && p.hoverPaneClose == diff.leafID)
}

func (p *Plugin) registerDiffPaneRegions(diff *diffPane, leafID int, box Box) {
	p.mouseHandler.HitMap.AddRect(regionPaneLeaf, box.X, box.Y, box.W, box.H, leafID)
}

func (p *Plugin) registerDiffTargetTabRegions(diff *diffPane, leafID int, box Box) {
	strip := layoutDiffTabStrip(diff, p.reserveHeader(box.W, true).TabsWidth, p.paneFocus == leafID)
	strip.RegisterHits(func(col, width, index int, close bool) {
		p.mouseHandler.HitMap.AddRect(regionDiffTargetTab, box.X+col, box.Y, width, 1, diffTabHit{LeafID: leafID, Index: index, Close: close})
	})
}

func (p *Plugin) registerDiffLeafHits(diff *diffPane, box Box) {
	view := diff.view()
	if view == nil {
		return
	}
	body := mouse.Rect{X: box.X, Y: box.Y + terminalHeaderRows, W: box.W, H: maxInt(box.H-terminalHeaderRows, 0)}
	if body.H < 1 {
		return
	}
	view.SetSize(body.W, body.H)
	p.attachDiffPaintTo(view)
	for _, hit := range view.FileHits(body) {
		p.mouseHandler.HitMap.AddRect(hit.ID, hit.Rect.X, hit.Rect.Y, hit.Rect.W, hit.Rect.H, hit.Data)
	}
	if d := view.DividerHit(body); d.W > 0 && d.H > 0 {
		p.mouseHandler.HitMap.AddRect(regionDiffTabDivider, d.X, d.Y, d.W, d.H, nil)
	}
}

func (p *Plugin) yankFocusedDiff() tea.Cmd {
	diff, _ := p.focusedDiffPane()
	view := diff.view()
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

func (p *Plugin) resizeFocusedLeaf(delta int) tea.Cmd {
	leaf := FindPane(p.paneRoot, p.paneFocus)
	if leaf == nil || leaf.Split != nil || leaf.Kind == PaneTerminal {
		return nil
	}
	parent, inA := enclosingSplit(p.paneRoot, leaf.ID)
	if parent == nil || parent.Split == nil {
		return nil
	}
	if inA {
		SetRatio(p.paneRoot, parent.ID, parent.Split.Ratio+delta)
	} else {
		SetRatio(p.paneRoot, parent.ID, parent.Split.Ratio-delta)
	}
	if p.contentDeck != nil {
		p.contentDeck.SetRatio(parent.ID, parent.Split.Ratio)
	}
	p.saveSelectionState()
	return p.resizeDocTerminalCmd()
}

func (p *Plugin) paneLeafBox(leafID int) (Box, bool) {
	geom, ok := p.leafGeometryFor(leafID)
	if !ok {
		return Box{}, false
	}
	return geom.Inner, true
}

func (p *Plugin) activeDiffView() *workspacediff.View {
	if diff, _ := p.activeDiffPane(); diff != nil {
		if view := diff.view(); view != nil {
			return view
		}
	}
	return &p.diff
}

func (p *Plugin) diffLeafShowing() bool {
	diff, _ := p.activeDiffPane()
	return diff != nil && p.paneTreeShowing()
}
