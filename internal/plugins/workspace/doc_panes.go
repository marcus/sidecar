package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/inlineedit"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/panebadge"
	"github.com/marcus/sidecar/internal/panecodec"
	"github.com/marcus/sidecar/internal/paneframe"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/panesearch"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/terminalperf"
	"github.com/marcus/sidecar/internal/ui"
)

// docPane is one document leaf's tab group. The pane tree points at this, not
// at a single model.
type docPane struct {
	leafID  int
	root    string
	surface string
	tabs    docview.Tabs

	// mode is the search surface this pane is showing over its document, or nil.
	// It is rooted at this pane's own root, which is what makes the same code
	// serve project and global Workspaces.
	mode *panesearch.Mode
	// modeRegions are the surface's hit regions from the last render, already at
	// their true positions. They are registered after the pane tree's own, so a
	// click inside the modal is not taken by the leaf drawn under it.
	modeRegions []mouse.Region
	// edit is this leaf's inline editor, created on the first `e`. It is per
	// leaf rather than per plugin: two document panes may hold sessions at
	// once, and only the focused one takes keys. See doc_edit.go.
	edit *inlineedit.Session
	// editW and editH are the viewport the live session was last sized to, so
	// a resize is sent when the box moves and not on every frame.
	editW, editH int
	// pendingEdit is the action an exit confirmation is holding, run once the
	// user says whether to save.
	pendingEdit func() tea.Cmd
	// boxW and boxH are the box the leaf was last given, so a surface that sizes
	// itself on input rather than on render has an answer before the first frame.
	// boxX and boxY place that box, which is what a click-away test needs.
	boxW, boxH, boxX, boxY int
}

// boxContains reports whether a plugin-local point is inside the pane's last
// drawn box. A pane that has not been drawn contains nothing.
func (d *docPane) boxContains(x, y int) bool {
	if d == nil || d.boxW <= 0 || d.boxH <= 0 {
		return false
	}
	return x >= d.boxX && x < d.boxX+d.boxW && y >= d.boxY && y < d.boxY+d.boxH
}

func newDocPane(leafID int, root, surface string, view *docview.Model) *docPane {
	d := &docPane{leafID: leafID, root: root, surface: surface}
	if view != nil {
		d.tabs.Append(view)
	}
	return d
}

func (d *docPane) view() *docview.Model {
	if d == nil {
		return nil
	}
	return d.tabs.ActiveView()
}

func docPaneTarget(path string) bool {
	return strings.TrimSpace(path) != ""
}

// selectedTerminalSurface identifies the actual terminal selection, not only
// its filesystem root. Project shells deliberately share ctx.WorkDir, so the
// tmux name is required to distinguish shell A from shell B.
func (p *Plugin) selectedTerminalSurface() (root, identity string, ok bool) {
	if p.ctx == nil {
		return "", "", false
	}
	root = p.ctx.WorkDir
	if p.selectingShell() {
		shell := p.getSelectedShell()
		if shell == nil || shell.TmuxName == "" {
			return "", "", false
		}
		if shell.WorkDir != "" {
			root = shell.WorkDir
		}
		identity = "shell:" + shell.TmuxName
	} else {
		wt := p.selectedWorktree()
		if wt == nil {
			return "", "", false
		}
		root = wt.Path
		identity = workspaceSurfaceIdentity(wt)
	}
	if p.remoteBound() {
		if identity == "" {
			return "", "", false
		}
		if root != "" {
			root = filepath.Clean(root)
		}
		return root, identity, true
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", false
	}
	return filepath.Clean(resolved), identity, true
}

func workspaceSurfaceIdentity(wt *Worktree) string {
	return panebadge.WorktreeSurface(worktreeSurfaceKey(wt))
}

func legacyWorkspaceSurfaceIdentity(wt *Worktree) string {
	if wt == nil || wt.Path == "" {
		return ""
	}
	return panebadge.WorktreeSurface(stablePathKey(wt.Path))
}

func (p *Plugin) activeDocPane() (*docPane, *PaneNode) {
	for id, doc := range p.docs {
		if doc == nil {
			continue
		}
		if leaf := FindPane(p.paneRoot, doc.leafID); leaf != nil && leaf.Kind == PaneDoc && leaf.ContentID == id {
			return doc, leaf
		}
	}
	return nil, nil
}

func (p *Plugin) activeDocPaneOrNil() *docPane {
	doc, _ := p.activeDocPane()
	return doc
}

func paneTreeFloors() Floors {
	// Floors are stated as the INNER minimum each content needs; the shared
	// frame adds the chrome every leaf spends (4 columns, 2 rows), so this
	// plugin and the global Workspaces browser budget a border identically.
	// Inner markdown still never drops below MinWidthForMarkdown.
	return paneframe.ChromeFloors(Floors{
		Terminal: PaneFloor{Width: termPanelMinBoxCols, Height: termPanelMinBoxRows},
		// A shell leaf is a terminal like the primary one, so it gets the
		// terminal panel's own floors — the same numbers the hand-rolled split
		// clamps to today.
		Shell: PaneFloor{Width: termPanelMinBoxCols, Height: termPanelMinBoxRows},
		Doc:   PaneFloor{Width: markdown.MinWidthForMarkdown, Height: termPanelMinBoxRows},
		// An issue's body is markdown wrapped by the same renderer, so it needs
		// the width that renderer stops being markdown below.
		Issue: PaneFloor{Width: markdown.MinWidthForMarkdown, Height: termPanelMinBoxRows},
		Note:  PaneFloor{Width: markdown.MinWidthForMarkdown, Height: termPanelMinBoxRows},
		Diff:  PaneFloor{Width: markdown.MinWidthForMarkdown, Height: termPanelMinBoxRows},
		// A resource card is a field grid over a markdown body rendered by
		// that same renderer, so it needs the same width and the same rows.
		Resource: PaneFloor{Width: markdown.MinWidthForMarkdown, Height: termPanelMinBoxRows},
	})
}

func (p *Plugin) openDocPane(root, rel string, line int) tea.Cmd {
	_, surface, _ := p.selectedTerminalSurface()
	return p.openDocPaneForSurface(root, surface, rel, line)
}

func (p *Plugin) openDocPaneForSurface(root, surface, rel string, line int) tea.Cmd {
	return p.openDocPaneFileForSurface(root, surface, rel, line, nil)
}

func (p *Plugin) openDocPaneFileForSurface(root, surface, rel string, line int, file *os.File) tea.Cmd {
	if p.paneRoot == nil || p.ctx == nil {
		if file != nil {
			_ = file.Close()
		}
		return nil
	}
	rel = docview.NormalizeTabPath(rel)
	if rel == "" || rel == "." {
		if file != nil {
			_ = file.Close()
		}
		return nil
	}
	cmd := p.openWorkspaceContentFile(root, surface, contentlink.Ref{Kind: contentlink.KindFile, Value: rel, Line: line}, "Document", file)
	if cmd == nil && file != nil {
		_ = file.Close()
	}
	return cmd
}

func (p *Plugin) selectDocTab(doc *docPane, modelID, idx, line int, file *os.File) (tea.Cmd, bool) {
	if doc == nil {
		return nil, false
	}
	p.closeDocInfo()
	doc.tabs.Select(idx)
	view := doc.view()
	if view == nil {
		return nil, false
	}
	if !view.NeedsLoad() {
		if line > 0 {
			view.ApplyLine(line)
		}
		return nil, false
	}
	if p.ctx == nil {
		return nil, false
	}
	rel := view.Title()
	rendered := view.Rendered()
	wrap := view.Wrap()
	var cmd tea.Cmd
	consumed := false
	if file != nil {
		cmd = view.LoadFile(modelID, file, rel, line, p.ctx.Epoch)
		consumed = true
	} else {
		cmd = view.Load(modelID, doc.root, rel, line, p.ctx.Epoch)
	}
	if line > 0 {
		applyDocRenderMode(view, rel, line)
	} else {
		view.SetRendered(rendered)
	}
	view.SetWrap(wrap)
	return cmd, consumed
}

func (p *Plugin) closeActiveDocTab() tea.Cmd {
	doc, leaf := p.activeDocPane()
	if doc == nil || leaf == nil {
		return nil
	}
	return p.closeDocTabAt(doc, leaf.ID, doc.tabs.Active)
}

func (p *Plugin) closeDocTabAt(doc *docPane, leafID, index int) tea.Cmd {
	if doc == nil || index < 0 || index >= len(doc.tabs.Items) {
		return nil
	}
	// Closing the tab the editor is holding away; ask first.
	if index == doc.tabs.Active && p.guardDocEdit(doc, func() tea.Cmd { return p.closeDocTabAt(doc, leafID, index) }) {
		return nil
	}
	if p.contentDeck != nil {
		return p.closeWorkspaceDeckTabAt(panelayout.Document, index)
	}
	if len(doc.tabs.Items) <= 1 {
		return p.closeDocPane()
	}
	p.closeDocInfo()
	doc.tabs.CloseAt(index)
	p.saveSelectionState()
	return p.ensureActiveDocTabLoaded(doc)
}

// clickDocTabAt selects a file tab from a pointer position. The Files plugin
// does this by testing the tab row first, because the preview pane region
// covers the header and a one-cell miss becomes a terminal click. The same
// steal happens here (plus the widened pane-tree divider), so a click on the
// exact document header row picks the tab under X, or the closest tab on that
// row. X is constrained to the document leaf so the
// terminal header that shares the row keeps Diff/Task action chips.
func (p *Plugin) clickDocTabAt(x, y int) (tea.Cmd, bool) {
	if !p.docVisible() {
		return nil, false
	}
	var tabs []mouse.Region
	var closeAt *mouse.Region
	for _, region := range p.mouseHandler.HitMap.Regions() {
		if region.ID != regionDocTab {
			continue
		}
		if y != region.Rect.Y {
			continue
		}
		hit, ok := region.Data.(docTabHit)
		if !ok {
			continue
		}
		if hit.Close {
			if x >= region.Rect.X && x < region.Rect.X+region.Rect.W {
				r := region
				closeAt = &r
			}
			continue
		}
		tabs = append(tabs, region)
	}
	if len(tabs) == 0 && closeAt == nil {
		return nil, false
	}
	inDocHeader := false
	for _, region := range p.mouseHandler.HitMap.Regions() {
		if region.ID != regionPaneLeaf {
			continue
		}
		// Content leaves share one region, so the tree says which of them the
		// row belongs to: only a document's header row carries file tabs.
		leafID, ok := region.Data.(int)
		if !ok {
			continue
		}
		if leaf := FindPane(p.paneRoot, leafID); leaf == nil || leaf.Kind != PaneDoc {
			continue
		}
		header := insetPanelChrome(region.Rect)
		if x >= header.X && x < header.X+header.W && y == header.Y {
			inDocHeader = true
			break
		}
	}
	if !inDocHeader {
		return nil, false
	}
	if closeAt != nil {
		return p.clickDocTab(closeAt.Data), true
	}
	best := tabs[0]
	bestDist := tabRowDistance(x, best.Rect)
	for _, region := range tabs[1:] {
		if d := tabRowDistance(x, region.Rect); d < bestDist {
			best, bestDist = region, d
		}
	}
	return p.clickDocTab(best.Data), true
}

func tabRowDistance(x int, r mouse.Rect) int {
	if x < r.X {
		return r.X - x
	}
	if x >= r.X+r.W {
		return x - (r.X + r.W) + 1
	}
	return 0
}

func (p *Plugin) clickDocTab(data any) tea.Cmd {
	hit, ok := data.(docTabHit)
	if !ok {
		return nil
	}
	leaf := FindPane(p.paneRoot, hit.LeafID)
	if leaf == nil || leaf.Kind != PaneDoc {
		return nil
	}
	doc := p.docs[leaf.ContentID]
	if doc == nil {
		return nil
	}
	p.activePane = PanePreview
	p.paneFocus = hit.LeafID
	p.setShellLeafFocused(false)
	p.pointer.Abandon()
	if p.viewMode == ViewModeInteractive {
		p.exitInteractiveMode()
	}
	if hit.Close {
		return p.closeDocTabAt(doc, hit.LeafID, hit.Index)
	}
	if hit.Index == doc.tabs.Active {
		return nil
	}
	if p.contentDeck != nil {
		return p.selectWorkspaceDeckTab(panelayout.Document, hit.Index)
	}
	cmd, _ := p.selectDocTab(doc, leaf.ContentID, hit.Index, 0, nil)
	p.saveSelectionState()
	return cmd
}

func (p *Plugin) cycleActiveDocTab(delta int) tea.Cmd {
	doc, _ := p.activeDocPane()
	if doc == nil || len(doc.tabs.Items) < 2 {
		return nil
	}
	if p.contentDeck != nil {
		return p.cycleWorkspaceDeckTab(panelayout.Document, delta)
	}
	p.closeDocInfo()
	doc.tabs.Cycle(delta)
	p.saveSelectionState()
	return p.ensureActiveDocTabLoaded(doc)
}

func (p *Plugin) ensureActiveDocTabLoaded(doc *docPane) tea.Cmd {
	if doc == nil || p.ctx == nil {
		return nil
	}
	view := doc.view()
	if view == nil || !view.NeedsLoad() {
		return nil
	}
	leaf := FindPane(p.paneRoot, doc.leafID)
	if leaf == nil {
		return nil
	}
	rendered := view.Rendered()
	wrap := view.Wrap()
	cmd := view.Load(leaf.ContentID, doc.root, view.Title(), 0, p.ctx.Epoch)
	view.SetRendered(rendered)
	view.SetWrap(wrap)
	return cmd
}

func applyDocRenderMode(view *docview.Model, path string, line int) {
	if view == nil {
		return
	}
	if !terminallink.Markdown(path) || line > 0 {
		view.SetRendered(false)
		return
	}
	view.SetRendered(true)
}

func clonePaneTree(node *PaneNode) *PaneNode {
	if node == nil {
		return nil
	}
	clone := *node
	if node.Split != nil {
		split := *node.Split
		split.A = clonePaneTree(node.Split.A)
		split.B = clonePaneTree(node.Split.B)
		clone.Split = &split
	}
	return &clone
}

func terminalLeafID(root *PaneNode) int {
	if leaf := firstPaneLeafOfKind(root, PaneTerminal); leaf != nil {
		return leaf.ID
	}
	return 0
}

func (p *Plugin) closeDocPane() tea.Cmd {
	p.closeDocInfo()
	doc, _ := p.activeDocPane()
	if doc == nil {
		return nil
	}
	return p.forgetContentPane(doc.leafID)
}

// hideDocPane collapses the live split and remembers the tab set. q/esc hide;
// last-x forgets through closeDocPane.
func (p *Plugin) hideDocPane() tea.Cmd {
	p.closeDocInfo()
	doc, _ := p.activeDocPane()
	if doc == nil {
		return nil
	}
	// Hiding the split leaves a live editor with no pane to draw in.
	if p.guardDocEdit(doc, func() tea.Cmd { return p.hideDocPane() }) {
		return nil
	}
	return p.hideContentPane(doc.leafID)
}

func paneLayoutHasDocTabs(layout *state.PaneLayoutJSON) bool {
	if layout == nil {
		return false
	}
	if len(layout.Tabs) > 0 {
		return true
	}
	if layout.Split == nil {
		return false
	}
	return paneLayoutHasDocTabs(layout.Split.A) || paneLayoutHasDocTabs(layout.Split.B)
}

func paneLayoutHasIssueTabs(layout *state.PaneLayoutJSON) bool {
	if layout == nil {
		return false
	}
	if len(layout.IssueTabs) > 0 {
		return true
	}
	if terminallink.IssueID(strings.TrimSpace(layout.Issue)) {
		return true
	}
	if layout.Split == nil {
		return false
	}
	return paneLayoutHasIssueTabs(layout.Split.A) || paneLayoutHasIssueTabs(layout.Split.B)
}

func paneLayoutHasDiffTabs(layout *state.PaneLayoutJSON) bool {
	if layout == nil {
		return false
	}
	if len(layout.DiffTabs) > 0 {
		return true
	}
	if layout.Split == nil {
		return false
	}
	return paneLayoutHasDiffTabs(layout.Split.A) || paneLayoutHasDiffTabs(layout.Split.B)
}

// paneLayoutHasRetainedTabs is the hide/reopen predicate: a q-hidden surface
// keeps document tabs, issue tabs, Diff tabs, resource references, or a legacy
// issue leaf.
func paneLayoutHasRetainedTabs(layout *state.PaneLayoutJSON) bool {
	return paneLayoutHasDocTabs(layout) || paneLayoutHasIssueTabs(layout) ||
		paneLayoutHasNoteTabs(layout) || paneLayoutHasDiffTabs(layout) || paneLayoutHasResourceTabs(layout)
}

func paneLayoutHasNoteTabs(layout *state.PaneLayoutJSON) bool {
	if layout == nil {
		return false
	}
	if len(layout.NoteTabs) > 0 {
		return true
	}
	if layout.Split == nil {
		return false
	}
	return paneLayoutHasNoteTabs(layout.Split.A) || paneLayoutHasNoteTabs(layout.Split.B)
}

// rememberHiddenPaneLayout merges the live tree into the surface's hidden
// snapshot so a later q on the remaining sibling keeps the first-hidden tabs.
func (p *Plugin) rememberHiddenPaneLayout(root, surface string) {
	live := p.paneLayoutJSON(p.paneRoot)
	if live == nil {
		return
	}
	live.Root = root
	live.Surface = surface
	live.Open = false
	merged := mergeHiddenPaneLayout(p.hiddenPaneLayout, live)
	if merged == nil {
		return
	}
	merged.Root = root
	merged.Surface = surface
	merged.Open = false
	normalizePersistedIssueLeaves(merged)
	p.hiddenPaneLayout = merged
}

func mergeHiddenPaneLayout(existing, live *state.PaneLayoutJSON) *state.PaneLayoutJSON {
	if existing == nil {
		return clonePaneLayout(live)
	}
	if live == nil {
		return clonePaneLayout(existing)
	}
	kinds := []string{contentKindDoc, contentKindIssue, contentKindNote, contentKindDiff, contentKindResource}
	var contents []*state.PaneLayoutJSON
	for _, kind := range kinds {
		leaf := firstLayoutLeafOfKind(live, kind)
		if leaf == nil {
			leaf = firstLayoutLeafOfKind(existing, kind)
		}
		if leaf != nil {
			contents = append(contents, leaf)
		}
	}
	if len(contents) == 0 {
		return clonePaneLayout(live)
	}
	existCount, liveCount := 0, 0
	for _, kind := range kinds {
		if firstLayoutLeafOfKind(existing, kind) != nil {
			existCount++
		}
		if firstLayoutLeafOfKind(live, kind) != nil {
			liveCount++
		}
	}
	if liveCount < len(contents) && existCount < len(contents) {
		return composeStackedHidden(live, contents...)
	}
	template := existing
	if existCount < len(contents) && liveCount == len(contents) {
		template = live
	}
	out := clonePaneLayout(template)
	for i, kind := range kinds {
		var leaf *state.PaneLayoutJSON
		for _, c := range contents {
			if c.Kind == kind {
				leaf = c
				break
			}
		}
		if leaf != nil {
			if firstLayoutLeafOfKind(out, kind) == nil {
				return composeStackedHidden(out, contents...)
			}
			replaceLayoutLeaf(out, kind, leaf)
		}
		_ = i
	}
	out.Open = false
	return out
}

func composeStackedHidden(template *state.PaneLayoutJSON, contents ...*state.PaneLayoutJSON) *state.PaneLayoutJSON {
	var kept []*state.PaneLayoutJSON
	for _, c := range contents {
		if c != nil {
			kept = append(kept, copyContentLeaf(c))
		}
	}
	if len(kept) == 0 {
		return clonePaneLayout(template)
	}
	cols, rows := 50, 50
	var root, surface string
	if template != nil {
		root, surface = template.Root, template.Surface
		if template.Split != nil {
			cols = template.Split.Ratio
			if inner := template.Split.B; inner != nil && inner.Split != nil {
				rows = inner.Split.Ratio
			} else if inner := template.Split.A; inner != nil && inner.Split != nil {
				rows = inner.Split.Ratio
			}
		}
	}
	right := kept[0]
	for i := 1; i < len(kept); i++ {
		right = &state.PaneLayoutJSON{Split: &state.PaneSplitJSON{
			Axis: "rows", Ratio: rows,
			A: right,
			B: kept[i],
		}}
	}
	return &state.PaneLayoutJSON{
		Root: root, Surface: surface, Open: false,
		Split: &state.PaneSplitJSON{
			Axis: "cols", Ratio: cols,
			A: &state.PaneLayoutJSON{Kind: contentKindTerminal},
			B: right,
		},
	}
}

func clonePaneLayout(src *state.PaneLayoutJSON) *state.PaneLayoutJSON {
	if src == nil {
		return nil
	}
	out := *src
	if src.Tabs != nil {
		out.Tabs = append([]state.PaneDocTabJSON(nil), src.Tabs...)
	}
	if src.IssueTabs != nil {
		out.IssueTabs = append([]state.PaneIssueTabJSON(nil), src.IssueTabs...)
	}
	if src.DiffTabs != nil {
		out.DiffTabs = append([]state.PaneDiffTabJSON(nil), src.DiffTabs...)
	}
	if src.ResourceTabs != nil {
		out.ResourceTabs = append([]state.PaneResourceTabJSON(nil), src.ResourceTabs...)
	}
	if src.NoteTabs != nil {
		out.NoteTabs = append([]state.PaneNoteTabJSON(nil), src.NoteTabs...)
	}
	if src.Split != nil {
		split := *src.Split
		split.A = clonePaneLayout(src.Split.A)
		split.B = clonePaneLayout(src.Split.B)
		out.Split = &split
	}
	return &out
}

func copyContentLeaf(src *state.PaneLayoutJSON) *state.PaneLayoutJSON {
	if src == nil {
		return nil
	}
	out := &state.PaneLayoutJSON{
		Kind: src.Kind, Active: src.Active,
		Issue: src.Issue, Scroll: src.Scroll,
	}
	if src.Tabs != nil {
		out.Tabs = append([]state.PaneDocTabJSON(nil), src.Tabs...)
	}
	if src.IssueTabs != nil {
		out.IssueTabs = append([]state.PaneIssueTabJSON(nil), src.IssueTabs...)
	}
	if src.DiffTabs != nil {
		out.DiffTabs = append([]state.PaneDiffTabJSON(nil), src.DiffTabs...)
	}
	if src.ResourceTabs != nil {
		out.ResourceTabs = append([]state.PaneResourceTabJSON(nil), src.ResourceTabs...)
	}
	if src.NoteTabs != nil {
		out.NoteTabs = append([]state.PaneNoteTabJSON(nil), src.NoteTabs...)
	}
	return out
}

func firstLayoutLeafOfKind(layout *state.PaneLayoutJSON, kind string) *state.PaneLayoutJSON {
	if layout == nil {
		return nil
	}
	if layout.Split == nil {
		if layout.Kind == kind {
			return layout
		}
		return nil
	}
	if leaf := firstLayoutLeafOfKind(layout.Split.A, kind); leaf != nil {
		return leaf
	}
	return firstLayoutLeafOfKind(layout.Split.B, kind)
}

func replaceLayoutLeaf(tree *state.PaneLayoutJSON, kind string, leaf *state.PaneLayoutJSON) {
	target := firstLayoutLeafOfKind(tree, kind)
	if target == nil || leaf == nil {
		return
	}
	target.Kind = leaf.Kind
	target.Active = leaf.Active
	target.Issue = leaf.Issue
	target.Scroll = leaf.Scroll
	target.Split = nil
	if leaf.Tabs != nil {
		target.Tabs = append([]state.PaneDocTabJSON(nil), leaf.Tabs...)
	} else {
		target.Tabs = nil
	}
	if leaf.IssueTabs != nil {
		target.IssueTabs = append([]state.PaneIssueTabJSON(nil), leaf.IssueTabs...)
	} else {
		target.IssueTabs = nil
	}
	if leaf.DiffTabs != nil {
		target.DiffTabs = append([]state.PaneDiffTabJSON(nil), leaf.DiffTabs...)
	} else {
		target.DiffTabs = nil
	}
	if leaf.ResourceTabs != nil {
		target.ResourceTabs = append([]state.PaneResourceTabJSON(nil), leaf.ResourceTabs...)
	} else {
		target.ResourceTabs = nil
	}
	if leaf.NoteTabs != nil {
		target.NoteTabs = append([]state.PaneNoteTabJSON(nil), leaf.NoteTabs...)
	} else {
		target.NoteTabs = nil
	}
}

func (p *Plugin) splitOnPlannedLeaf(plan paneOpen, node *PaneNode, name string) bool {
	peer, placed := p.previewPeerBox()
	if !placed {
		return false
	}
	trialNode := clonePaneTree(node)
	trial, trialFocus := ApplyPanePlan(clonePaneTree(p.paneRoot), plan, trialNode)
	if trialFocus != trialNode.ID {
		return false
	}
	if _, _, fits := LayoutPanes(trial, peer, paneTreeFloors()); !fits {
		p.toastMessage = paneFitMessage(name, plan.Axis)
		p.toastTime = time.Now()
		return false
	}
	treeRoot, focus := ApplyPanePlan(p.paneRoot, plan, node)
	if focus != node.ID {
		return false
	}
	p.paneRoot, p.paneFocus = treeRoot, focus
	p.paneNextID = maxInt(p.paneNextID, maxPaneID(p.paneRoot)+1)
	return true
}

// closeContentLeaf drops one content leaf's state and collapses its box into
// its sibling. It is the one close path for every non-terminal leaf, so a kind
// added to the tree cannot be closed by a route that forgets to release it.
func (p *Plugin) closeContentLeaf(leafID int) bool {
	leaf := FindPane(p.paneRoot, leafID)
	if leaf == nil || leaf.Split != nil {
		return false
	}
	switch leaf.Kind {
	case PaneDoc:
		// A pane closed with a search up takes the search's work with it: an
		// unclosed project search leaves rg running to its 30s timeout.
		if doc := p.docs[leaf.ContentID]; doc != nil {
			doc.mode.Close()
			// A leaf dropped by a route that did not ask first (a click on the
			// X, a shell switch) still owns a tmux session; releasing the leaf
			// without it leaves an orphan editor holding the file.
			doc.releaseEdit()
		}
		delete(p.docs, leaf.ContentID)
	case PaneIssue:
		delete(p.issues, leaf.ContentID)
	case PaneNote:
		delete(p.notes, leaf.ContentID)
	case PaneDiff:
		delete(p.diffs, leaf.ContentID)
	case PaneResource:
		delete(p.resources, leaf.ContentID)
	default:
		return false
	}
	p.paneRoot, p.paneFocus = ClosePane(p.paneRoot, leaf.ID)
	return true
}

// resetDocPanesForSelection collapses every content leaf that belongs to a
// terminal surface other than the selected one. A leaf is bound to the surface
// its link was clicked in, so a shell switch takes its documents and issues
// with it rather than showing them against the wrong workspace.
func (p *Plugin) resetDocPanesForSelection() bool {
	p.closeDocInfo()
	root, surface, selected := p.selectedTerminalSurface()
	closed := false
	for _, leafID := range p.contentLeafIDs() {
		paneRoot, paneSurface, ok := p.contentLeafSurface(leafID)
		if ok && selected && filepath.Clean(paneRoot) == root && paneSurface == surface {
			continue
		}
		closed = p.closeContentLeaf(leafID) || closed
	}
	return closed
}

// contentLeafIDs lists the non-terminal leaves of the tree in placement order.
// The list is taken before any close, because closing collapses the tree the
// walk would otherwise be reading.
func (p *Plugin) contentLeafIDs() []int {
	var ids []int
	var walk func(node *PaneNode)
	walk = func(node *PaneNode) {
		if node == nil {
			return
		}
		if node.Split != nil {
			walk(node.Split.A)
			walk(node.Split.B)
			return
		}
		if node.Kind != PaneTerminal {
			ids = append(ids, node.ID)
		}
	}
	walk(p.paneRoot)
	return ids
}

// contentLeafSurface reports the terminal surface a content leaf was opened
// against. ok is false for a leaf whose content is gone, which is a leaf
// nothing can still claim belongs to the selection.
func (p *Plugin) contentLeafSurface(leafID int) (root, surface string, ok bool) {
	leaf := FindPane(p.paneRoot, leafID)
	if leaf == nil || leaf.Split != nil {
		return "", "", false
	}
	switch leaf.Kind {
	case PaneShell:
		// A shell leaf belongs to whatever the sidebar has selected: it holds no
		// content of its own to be stale against, and the session it draws is
		// the selection's.
		return p.selectedTerminalSurface()
	case PaneDoc:
		if doc := p.docs[leaf.ContentID]; doc != nil {
			return doc.root, doc.surface, true
		}
	case PaneIssue:
		if issue := p.issues[leaf.ContentID]; issue != nil {
			return issue.root, issue.surface, true
		}
	case PaneNote:
		if note := p.notes[leaf.ContentID]; note != nil {
			return note.root, note.surface, true
		}
	case PaneDiff:
		if diff := p.diffs[leaf.ContentID]; diff != nil {
			return diff.root, diff.surface, true
		}
	case PaneResource:
		if res := p.resources[leaf.ContentID]; res != nil {
			return res.root, res.surface, true
		}
	}
	return "", "", false
}

func (p *Plugin) resizeDocTerminalCmd() tea.Cmd {
	return tea.Batch(p.docTerminalResizeCmds()...)
}

// docTerminalResizeCmds names the exact resize fan-out for a tree geometry
// change. Keeping it inspectable lets tests prove one command per visible tmux
// surface without executing tmux against the developer's live server.
func (p *Plugin) docTerminalResizeCmds() []tea.Cmd {
	cmds := make([]tea.Cmd, 0, 2)
	if cmd := p.resizeSelectedPaneCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if p.shellLeafVisible() {
		if cmd := p.resizeTermPanelPaneCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

func (p *Plugin) applyDocLoaded(msg docview.LoadedMsg) {
	if p.contentDeck != nil {
		_ = p.applyWorkspaceDeckBroadcast(msg)
		return
	}
	doc := p.docs[msg.ModelID]
	if doc == nil || p.ctx == nil || msg.Epoch != p.ctx.Epoch {
		return
	}
	root, surface, ok := p.selectedTerminalSurface()
	if !ok || filepath.Clean(doc.root) != root || doc.surface != surface {
		return
	}
	for _, item := range doc.tabs.Items {
		if item.View != nil && item.View.SetResult(msg) {
			return
		}
	}
}

// docVisible reports whether the pane split is on screen. It asks the tree for
// any content leaf rather than for a document, because a terminal beside a td
// issue is the same split with the same geometry. Diff and Task replace that
// split without clearing paneFocus, so a still-selected content leaf is not the
// keyboard owner while those tabs are showing.
func (p *Plugin) docVisible() bool {
	live := false
	for _, leafID := range p.contentLeafIDs() {
		if _, _, ok := p.contentLeafSurface(leafID); ok {
			live = true
			break
		}
	}
	return (live || p.shellLeaf() != nil) && p.paneRoot != nil
}

// previewLeafFocused reports whether a visible content leaf holds the preview's
// keyboard focus, whatever it is showing. The frame reads this — zoom, divider
// styling — because those are properties of a leaf being focused, not of it
// being a document.
func (p *Plugin) previewLeafFocused() bool {
	if !p.docVisible() || p.activePane != PanePreview {
		return false
	}
	leaf := FindPane(p.paneRoot, p.paneFocus)
	return leaf != nil && leaf.Split == nil && leaf.Kind != PaneTerminal
}

// docFocused is the narrower question the document's own keys ask: not "a
// content leaf holds focus" but "the focused leaf is a document". An issue leaf
// answers false here, so nothing routes a document key into it.
func (p *Plugin) docFocused() bool {
	if !p.previewLeafFocused() {
		return false
	}
	leaf := FindPane(p.paneRoot, p.paneFocus)
	return leaf != nil && leaf.Kind == PaneDoc && p.docs[leaf.ContentID] != nil
}

func (p *Plugin) handleDocKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if p.docInfo != nil {
		switch msg.String() {
		case "q", "esc", "I", "i":
			p.closeDocInfo()
			return true, nil
		}
		closed, cmd := p.docInfo.HandleKey(msg)
		if closed {
			p.closeDocInfo()
		}
		return true, cmd
	}
	if !p.docFocused() {
		return false, nil
	}
	doc := p.focusedDocPane()
	if doc == nil {
		return false, nil
	}
	// A live editor owns the pane outright, before the search surfaces and
	// before the document's own keys: every key in it is on its way to vim.
	if doc.editing() {
		return p.handleDocEditKey(doc, msg)
	}
	// A live search surface owns every key in the pane, exactly as the document
	// under it owns every key it is handed: esc closes it, and nothing it does
	// not use reaches the workspace behind the pane.
	if doc.mode != nil {
		return true, p.handleDocSearchKey(doc, msg)
	}
	// In-file search is the same rule one level down: while its bar is up it
	// owns every key in the pane, and only esc/enter give the document back.
	// docview answers handled=false when no search is running, so this costs
	// the ordinary key path nothing.
	if view := doc.view(); view != nil {
		if handled, cmd := view.HandleSearchKey(msg); handled {
			return true, cmd
		}
	}
	// Before the pane's own keys: esc clears a selection rather than hiding the
	// pane out from under it, and the copy chord must not fall through to a
	// document key that happens to share it.
	if cmd, handled := p.handlePaneSelectionKey(docSelectionPane(doc.view()), msg); handled {
		return true, cmd
	}
	// Asked here rather than above: the editor, the finder overlay and a
	// committed in-file search all own `n` while they are up, and each has
	// already declined by this point.
	if handled, cmd := p.paneSwitcherKey(msg); handled {
		return true, cmd
	}
	switch msg.String() {
	case "/":
		if view := doc.view(); view != nil {
			view.StartSearch()
		}
		return true, nil
	case "e":
		return true, p.enterDocEdit(doc)
	case "E":
		if view := doc.view(); view != nil {
			return true, docview.EditExternal(view.Root(), view.Title(), view.ScrollOffset()+1)
		}
		return true, nil
	case "r":
		return true, p.reloadFocusedDoc()
	case "ctrl+p":
		return true, p.openDocFinder(doc)
	case "f":
		return true, p.openDocProjectSearch(doc)
	case "\\":
		return true, p.toggleSidebarCmd()
	case "q", "esc":
		return true, p.hideDocPane()
	case "x":
		return true, p.closeActiveDocTab()
	case "{":
		return true, p.cycleActiveDocTab(-1)
	case "}":
		return true, p.cycleActiveDocTab(1)
	case "m":
		p.toggleDocRenderMode()
		return true, nil
	case "w":
		p.toggleDocWrap()
		return true, nil
	case "I":
		return true, p.openDocInfo()
	case "ctrl+r":
		return true, p.revealActiveDoc()
	case "Y":
		return true, p.yankActiveDocPath()
	case "y":
		if view := doc.view(); view != nil {
			return true, view.YankSelectionOrContents()
		}
		return true, nil
	case "+":
		return true, p.resizeFocusedDoc(5)
	case "-":
		return true, p.resizeFocusedDoc(-5)
	default:
		if view := doc.view(); view != nil {
			before := view.ScrollOffset()
			view.HandleKey(msg)
			if view.ScrollOffset() != before {
				p.saveSelectionState()
			}
		}
		// A focused document is its own input context. Absorb keys it does not
		// own so they cannot trigger workspace actions behind the pane.
		return true, nil
	}
}

func (p *Plugin) closeDocInfo() {
	p.docInfo = nil
}

func (p *Plugin) toggleDocWrap() {
	doc, _ := p.activeDocPane()
	if doc == nil || doc.view() == nil {
		return
	}
	doc.view().ToggleWrap()
	p.saveSelectionState()
}

func (p *Plugin) openDocInfo() tea.Cmd {
	doc, _ := p.activeDocPane()
	if doc == nil || doc.view() == nil || doc.view().Title() == "" {
		return nil
	}
	info, cmd := docview.OpenInfo(doc.root, doc.view().Title())
	p.docInfo = info
	return cmd
}

func (p *Plugin) reloadFocusedDoc() tea.Cmd {
	if p.contentDeck != nil {
		p.syncDeckFocus()
		return unwrapWorkspaceDeckLoad(p.contentDeck.ReloadFocused())
	}
	doc, _ := p.activeDocPane()
	if doc == nil || doc.view() == nil || doc.view().Title() == "" {
		return nil
	}
	return doc.view().Reload()
}

func (p *Plugin) revealActiveDoc() tea.Cmd {
	doc, _ := p.activeDocPane()
	if doc == nil || doc.view() == nil || doc.view().Title() == "" {
		return nil
	}
	return docview.Reveal(doc.root, doc.view().Title())
}

func (p *Plugin) yankActiveDocPath() tea.Cmd {
	doc, _ := p.activeDocPane()
	if doc == nil || doc.view() == nil || doc.view().Title() == "" {
		return nil
	}
	return docview.YankPath(doc.view().Title())
}

func (p *Plugin) resizeFocusedDoc(delta int) tea.Cmd {
	_, leaf := p.activeDocPane()
	if leaf == nil {
		return nil
	}
	parent, docInA := enclosingSplit(p.paneRoot, leaf.ID)
	if parent == nil || parent.Split == nil {
		return nil
	}
	if docInA {
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

func enclosingSplit(node *PaneNode, leafID int) (*PaneNode, bool) {
	if node == nil || node.Split == nil {
		return nil, false
	}
	if FindPane(node.Split.A, leafID) != nil {
		if node.Split.A.ID == leafID {
			return node, true
		}
		if parent, inA := enclosingSplit(node.Split.A, leafID); parent != nil {
			return parent, inA
		}
	}
	if node.Split.B.ID == leafID {
		return node, false
	}
	return enclosingSplit(node.Split.B, leafID)
}

func (p *Plugin) persistedPaneLayout() *state.PaneLayoutJSON {
	if p.paneRoot == nil {
		return nil
	}
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		return nil
	}
	// A content leaf still holding another surface's target is a layout about to
	// be collapsed; persist the terminal alone rather than a document or an issue
	// that will come back attached to the wrong workspace. Switch-away writes
	// the previous surface before the index changes, so this is a safety net
	// for a save that still races the selection.
	for _, leafID := range p.contentLeafIDs() {
		paneRoot, paneSurface, ok := p.contentLeafSurface(leafID)
		if ok && (filepath.Clean(paneRoot) != root || paneSurface != surface) {
			return &state.PaneLayoutJSON{Root: root, Surface: surface, Kind: contentKindTerminal, Open: true}
		}
	}
	return p.persistedSurfaceLayout(root, surface)
}

func (p *Plugin) persistedSurfaceLayout(root, surface string) *state.PaneLayoutJSON {
	if p.hiddenPaneLayout != nil && p.hiddenPaneLayout.Surface == surface && paneLayoutHasRetainedTabs(p.hiddenPaneLayout) {
		layout := p.hiddenPaneLayout
		layout.Root = root
		layout.Surface = surface
		layout.Open = false
		normalizePersistedIssueLeaves(layout)
		return layout
	}
	layout := p.paneLayoutJSON(p.paneRoot)
	if layout == nil {
		return nil
	}
	layout.Root = root
	layout.Surface = surface
	layout.Open = true
	return layout
}

func (p *Plugin) readWorkspaceState() state.WorkspaceState {
	if p.ctx == nil {
		return state.WorkspaceState{}
	}
	wt := p.shellStartupHooks.withDefaults().getWorkspaceState(p.ctx.ProjectRoot)
	state.MigratePaneLayouts(&wt)
	return wt
}

func (p *Plugin) savedPaneLayoutForCurrentSurface(surface string) *state.PaneLayoutJSON {
	wtState := p.readWorkspaceState()
	legacy := ""
	if wt := p.selectedWorktree(); wt != nil {
		legacy = legacyWorkspaceSurfaceIdentity(wt)
	}
	layout, changed := state.RekeyPaneLayout(&wtState, legacy, surface)
	if changed {
		wtState.PaneLayout = nil
		p.writeWorkspaceState(wtState)
	}
	return layout
}

func (p *Plugin) forgetPaneSurfaces(surfaces ...string) {
	wtState := p.readWorkspaceState()
	if state.ForgetPaneLayouts(&wtState, surfaces...) {
		p.writeWorkspaceState(wtState)
	}
}

func (p *Plugin) forgetWorktreePaneLayout(wt *Worktree) {
	if wt == nil {
		return
	}
	p.forgetPaneSurfaces(workspaceSurfaceIdentity(wt), legacyWorkspaceSurfaceIdentity(wt))
}

func (p *Plugin) writeWorkspaceState(wt state.WorkspaceState) {
	if p.ctx == nil {
		return
	}
	_ = p.shellStartupHooks.withDefaults().setWorkspaceState(p.ctx.ProjectRoot, wt)
}

func (p *Plugin) liveTreeRepresents(surface string) bool {
	if surface == "" {
		return false
	}
	hasContent := false
	for _, leafID := range p.contentLeafIDs() {
		_, paneSurface, ok := p.contentLeafSurface(leafID)
		if !ok {
			continue
		}
		hasContent = true
		if paneSurface == surface {
			return true
		}
	}
	if hasContent {
		return false
	}
	return p.paneLayoutSurface == surface
}

func (p *Plugin) storeLivePaneLayout(root, surface string) {
	if p.paneRoot == nil || surface == "" {
		return
	}
	layout := p.persistedSurfaceLayout(root, surface)
	if layout == nil {
		return
	}
	wt := p.readWorkspaceState()
	if wt.PaneLayouts == nil {
		wt.PaneLayouts = make(map[string]*state.PaneLayoutJSON)
	}
	wt.PaneLayouts[surface] = layout
	wt.PaneLayout = nil
	p.writeWorkspaceState(wt)
}

func (p *Plugin) restoreIncomingPaneLayout() {
	p.restoreSurfacePaneLayout(false)
}

// restoreIncomingPaneLayoutHonoringOpen is the relaunch/retarget path: a
// hidden set stays in the map and the live tree stays terminal-only. An
// explicit surface switch uses restoreIncomingPaneLayout so q-hidden tabs
// come back.
func (p *Plugin) restoreIncomingPaneLayoutHonoringOpen() {
	p.restoreSurfacePaneLayout(true)
}

func (p *Plugin) restoreSurfacePaneLayout(honorOpen bool) {
	if p.paneRoot == nil {
		return
	}
	_, surface, ok := p.selectedTerminalSurface()
	p.resetPaneTreeToTerminal()
	if !ok {
		p.paneLayoutSurface = ""
		p.paneRestoreCmd = nil
		return
	}
	p.paneLayoutSurface = surface
	layout := p.savedPaneLayoutForCurrentSurface(surface)
	if p.legacyTermPanel.Visible {
		// The pre-split panel preference becomes a split of this layout, once.
		// It is spent on the FIRST surface restored either way: a preference
		// held back until some later workspace happens to have a saved layout
		// would splice a split into a workspace it was never set on.
		if layout != nil {
			layout = migrateTermPanelIntoLayout(layout, p.legacyTermPanel)
		}
		p.legacyTermPanel = termPanelPrefs{}
	}
	if layout == nil {
		p.paneRestoreCmd = nil
		return
	}
	if honorOpen && !state.PaneLayoutOpen(layout) {
		if paneLayoutHasRetainedTabs(layout) {
			p.hiddenPaneLayout = layout
		}
		p.paneRestoreCmd = nil
		return
	}
	p.paneRestoreCmd = p.restorePaneLayout(layout)
}

func (p *Plugin) takePaneRestoreCmd() tea.Cmd {
	cmd := p.paneRestoreCmd
	p.paneRestoreCmd = nil
	return cmd
}

func (p *Plugin) restorePaneLayout(layout *state.PaneLayoutJSON) tea.Cmd {
	if p.paneRoot == nil || layout == nil || p.ctx == nil {
		return nil
	}
	root, surface, ok := p.selectedTerminalSurface()
	if !ok || filepath.Clean(layout.Root) != root || layout.Surface != surface {
		return nil
	}
	oldPaneRoot := p.paneRoot
	p.releaseAllDocEdits()
	p.docs = make(map[int]*docPane)
	p.issues = make(map[int]*issuePane)
	p.notes = make(map[int]*notePane)
	p.diffs = make(map[int]*diffPane)
	p.resources = make(map[int]*resourcePane)
	p.paneNextID = 1

	st, live := panecodec.Decode(layout, panecodec.Options{AcceptTab: p.acceptRestoredTab(root)})
	if liveKindCount(live, panecodec.KindTerminal) != 1 {
		p.resetPaneTreeToTerminal()
		return nil
	}
	ctx := p.workspaceDeckContext(root, surface)
	cfg := p.workspaceDeckConfig()
	deck := contentpanes.Decode(ctx, cfg, st)
	// Deck owns every passive node ID. Host-only Shell leaves and the extra
	// split nodes needed to carry them start above that namespace.
	nextID := panelayout.MaxID(deck.Tree())
	restored := restoreTree(st.Root, deck, &nextID, make(map[PaneKind]bool))
	if restored == nil || !supportedPaneTree(restored) || firstPaneLeafOfKind(restored, PaneTerminal) == nil {
		p.resetPaneTreeToTerminal()
		return nil
	}
	p.contentDeck = deck
	p.rebindTerminalPaneTree(oldPaneRoot, restored)
	p.paneRoot = restored
	if sh := liveOfKind(live, panecodec.KindShell); sh != nil {
		p.restoredShellSession = shellSessionSelector(sh.Session, "")
		p.requestShellLeaf()
		p.shellLeafSurface = surface
		p.rememberShellSplit()
	} else {
		p.releaseShellTermPane()
		p.setShellLeafFocused(false)
		p.shellLeafSurface = ""
	}
	p.syncShellLeaf()
	p.adoptRestoredDeckMaps(root, surface)
	if !supportedPaneTree(p.paneRoot) {
		p.resetPaneTreeToTerminal()
		return nil
	}
	p.paneFocus = terminalLeafID(p.paneRoot)
	p.paneNextID = maxPaneID(p.paneRoot) + 1
	return tea.Batch(unwrapDeckCmds(deck.LoadVisible()...)...)
}

func (p *Plugin) acceptRestoredTab(root string) func(string, contentpanes.TabState) bool {
	return func(kind string, tab contentpanes.TabState) bool {
		if kind != panecodec.KindDoc {
			return true
		}
		rel, _, valid := resolveTerminalPath(root, tab.Ref.Value)
		return valid && !filepath.IsAbs(rel)
	}
}

func liveKindCount(live []panecodec.Live, kind string) int {
	n := 0
	for _, l := range live {
		if l.Kind == kind {
			n++
		}
	}
	return n
}

func liveOfKind(live []panecodec.Live, kind string) *panecodec.Live {
	for i := range live {
		if live[i].Kind == kind {
			return &live[i]
		}
	}
	return nil
}

// supportedPaneTree accepts any tree whose leaves are kinds this build can
// draw, nested to any depth: the compositor places and clips every leaf the
// layout returns, so the old terminal-beside-one-document restriction was
// refusing shapes it could already render — the steel thread's terminal beside
// a stacked document and issue among them. What it still refuses is a leaf of
// an unknown kind, which a forward or hand-edited layout can carry and which
// would drive terminal geometry for a box nothing draws into.
//
// Exactly one terminal leaf remains the decoder's rule, counted there.
func supportedPaneTree(root *PaneNode) bool {
	if root == nil {
		return false
	}
	if root.Split == nil {
		switch root.Kind {
		case PaneTerminal, PaneShell, PaneDoc, PaneIssue, PaneNote, PaneDiff, PaneResource:
			return true
		default:
			return false
		}
	}
	return root.Split.A != nil && root.Split.B != nil &&
		supportedPaneTree(root.Split.A) && supportedPaneTree(root.Split.B)
}

func (p *Plugin) nextPaneID() int {
	id := p.paneNextID
	p.paneNextID++
	return id
}

func (p *Plugin) resetPaneTreeToTerminal() {
	p.closeDocInfo()
	p.releaseAllDocEdits()
	p.docs = make(map[int]*docPane)
	p.issues = make(map[int]*issuePane)
	p.notes = make(map[int]*notePane)
	p.diffs = make(map[int]*diffPane)
	p.resources = make(map[int]*resourcePane)
	p.contentDeck = nil
	p.hiddenPaneLayout = nil
	p.paneNextID = 1
	p.paneRoot = &PaneNode{ID: p.nextPaneID(), Kind: PaneTerminal}
	p.paneFocus = p.paneRoot.ID
	// A split terminal IS owned by a selection, so a tree rebuilt for a new one
	// is rebuilt without it unless the new selection is the split's own
	// workspace — syncShellLeaf reconciles both halves of that.
	p.syncShellLeaf()
}

// docPaneHeaderRow is the doc leaf's header: the tab strip plus the shared X.
func (p *Plugin) docPaneHeaderRow(doc *docPane, width int, focused bool) string {
	return p.composeContentHeader(layoutDocTabStrip(doc, p.reserveHeader(width, true).TabsWidth, focused).HoverClose(p.hoverTabClose.IndexFor(docLeafID(doc))).Row, width, docLeafID(doc), doc != nil && p.hoverPaneClose == doc.leafID)
}

func (p *Plugin) toggleDocRenderMode() {
	doc, _ := p.activeDocPane()
	if doc == nil || doc.view() == nil {
		return
	}
	if !terminallink.Markdown(doc.view().Title()) {
		return
	}
	doc.view().ToggleRenderMode()
	p.saveSelectionState()
}

func (p *Plugin) dividerHandleState(region string, splitID int) ui.HandleState {
	dragging := p.mouseHandler != nil && p.mouseHandler.IsDragging() && p.mouseHandler.DragRegion() == region
	hovering := p.hoverDividerRegion == region
	if region != regionPaneTreeDivider {
		return ui.HandleStateFrom(hovering, dragging)
	}
	return paneframe.HandleStateFor(splitID, dragging, p.paneDragSplitID, hovering, p.hoverDividerID)
}

func (p *Plugin) renderPaneTreeHandle(split Divider) string {
	return paneframe.RenderDividerHandle(split, p.dividerHandleState(regionPaneTreeDivider, split.SplitID))
}

func (p *Plugin) registerDocPaneRegions(doc *docPane, leafID int, box Box) {
	p.mouseHandler.HitMap.AddRect(regionPaneLeaf, box.X, box.Y, box.W, box.H, leafID)
}

func (p *Plugin) registerDocTabRegions(doc *docPane, leafID int, box Box) {
	strip := layoutDocTabStrip(doc, p.reserveHeader(box.W, true).TabsWidth, p.paneFocus == leafID)
	strip.RegisterHits(func(col, width, index int, close bool) {
		p.mouseHandler.HitMap.AddRect(regionDocTab, box.X+col, box.Y, width, 1, docTabHit{LeafID: leafID, Index: index, Close: close})
	})
}

func (p *Plugin) renderDocumentSplit(width, height int) (string, bool) {
	if !p.docVisible() {
		return "", false
	}
	p.clearDocLinkHits()
	// Regions are re-earned every frame: a pane this frame does not draw must
	// not leave last frame's modal regions on screen.
	p.clearDocSearchRegions()
	canvasBox := p.previewLayoutBox(width, height)
	zoom := p.paneZoom.Leaf(p.paneLayoutModalScope(), p.paneRoot)
	layout, ok := LayoutPaneTreeWithZoom(p.paneRoot, canvasBox, paneTreeFloors(), p.paneFocus, zoom)
	if !ok {
		return "", false
	}
	// A zoomed leaf still composes here, including Primary. Routing Primary
	// through the legacy fallback drew its header but skipped RegisterRegions,
	// leaving the visible layout button inert. One shared frame now owns both
	// the cells and their targets in tiled and zoomed states.
	key, cacheable := p.projectPreviewKeyFor(layout, width, height)
	view, hit := "", false
	if cacheable {
		view, hit = p.reuseProjectPreview(key)
	}
	if hit {
		p.replayProjectPreviewRegions(layout)
	} else {
		terminalperf.Record(terminalperf.ProjectPreviewComposed)
		view = paneframe.Compose(paneHost{p}, layout, canvasBox, width, height)
		if cacheable {
			// Compose may settle document state while preparing a frame. Store the
			// resulting identity, not the pre-compose candidate.
			if settled, ok := p.projectPreviewKeyFor(layout, width, height); ok {
				p.storeProjectPreview(settled, view)
			}
		}
	}
	p.registerPaneTreeRegions(layout)
	// The frame a pointer is tested against is THIS frame, recorded beside the
	// regions it earned rather than re-derived later from state that does not
	// know whether a tree was drawn.
	p.paneFrame, p.paneFrameDrawn = layout, true
	// Last, because a live search surface is drawn over its leaf and its regions
	// have to beat the leaf's own.
	p.registerDocSearchRegions()
	return view, true
}

// renderPaneLeaf draws one placed leaf's body, with no chrome around it, at an
// explicit origin. placement.Box is the body's own rectangle.
func (p *Plugin) renderPaneLeaf(placement Placement, origin Box, zoomed bool) string {
	return paneframe.RenderContent(paneHost{p}, placement.Node, Box{
		X: origin.X + placement.Box.X, Y: origin.Y + placement.Box.Y,
		W: placement.Box.W, H: placement.Box.H,
	}, zoomed)
}

// wrapLeafChrome draws one leaf's border. It is a reader of setFocusTarget:
// interactive/active on the focused leaf, muted on neighbours. Content bytes are
// not dimmed. The lone-terminal preview frame is drawn through here too, so the
// zoomed case and the tiled case cannot pick different borders.
func (p *Plugin) wrapLeafChrome(node *PaneNode, content string, outer Box) string {
	return paneframe.WrapLeaf(content, outer, paneHost{p}.Chrome(node))
}

// takePaneSizeCmds empties the queue as it hands it over: a geometry assertion
// dispatched on every update after the render that made it is a resize storm.
func (p *Plugin) takePaneSizeCmds() []tea.Cmd {
	cmds := p.paneSizeCmds
	p.paneSizeCmds = nil
	return cmds
}

func (p *Plugin) renderPaneTreeDivider(split Divider) string {
	return p.renderPaneTreeHandle(split)
}

// registerPaneLeafRegions registers one placed leaf's hit regions, in
// plugin-local coordinates. Tab hits come from the same strip the header
// draws, so a click cannot land on a tab that was never rendered. A terminal
// leaf registers nothing here: its regions belong to the legacy renderer inside it.
func (p *Plugin) registerPaneLeafRegions(node *PaneNode, box Box) {
	if node == nil || node.Split != nil {
		return
	}
	content := p.paneContent(node)
	if content == nil {
		return
	}
	switch node.Kind {
	case PaneShell:
		// The panel's terminal takes clicks over its INNER box — the border is
		// the frame's, not the terminal's — and it is registered here so a click
		// cannot land on a panel the frame did not draw.
		inner := leafGeometry(box).Inner
		p.mouseHandler.HitMap.AddRect(regionTermPanelContent, inner.X, inner.Y, inner.W, inner.H, node.ID)
	case PaneDoc:
		if doc := p.docs[node.ContentID]; doc != nil {
			p.registerDocPaneRegions(doc, node.ID, box)
		}
	case PaneIssue:
		if issue := p.issues[node.ContentID]; issue != nil {
			p.registerIssuePaneRegions(issue, node.ID, box)
		}
	case PaneNote:
		if note := p.notes[node.ContentID]; note != nil {
			p.registerNotePaneRegions(note, node.ID, box)
		}
	case PaneDiff:
		if diff := p.diffs[node.ContentID]; diff != nil {
			p.registerDiffPaneRegions(diff, node.ID, box)
		}
	case PaneResource:
		if res := p.resources[node.ContentID]; res != nil {
			p.registerResourcePaneRegions(res, node.ID, box)
		}
	}
}

func (p *Plugin) registerPaneTabRegions(node *PaneNode, box Box) {
	if node == nil || node.Split != nil {
		return
	}
	switch node.Kind {
	case PaneDoc:
		if doc := p.docs[node.ContentID]; doc != nil {
			p.registerDocTabRegions(doc, node.ID, box)
		}
	case PaneIssue:
		if issue := p.issues[node.ContentID]; issue != nil {
			p.registerIssueTabRegions(issue, node.ID, box)
		}
	case PaneNote:
		if note := p.notes[node.ContentID]; note != nil {
			p.registerNoteTabRegions(note, node.ID, box)
		}
	case PaneDiff:
		if diff := p.diffs[node.ContentID]; diff != nil {
			p.registerDiffTargetTabRegions(diff, node.ID, box)
		}
	case PaneResource:
		if res := p.resources[node.ContentID]; res != nil {
			p.registerResourceTabRegions(res, node.ID, box)
		}
	}
}

func (p *Plugin) registerPaneCloseRegions(node *PaneNode, box Box) {
	if node == nil || node.Split != nil {
		return
	}
	switch node.Kind {
	case PaneDoc, PaneIssue, PaneNote, PaneDiff, PaneResource:
		if p.paneContent(node) == nil {
			return
		}
		p.registerPaneCloseRegion(node.ID, box)
	case PaneShell:
		// A split terminal is closable exactly like any other non-primary leaf.
		// The PRIMARY terminal is not: it is the surface the sidebar selects,
		// and closing it would leave the workspace with nothing to select into.
		p.registerPaneCloseRegion(node.ID, box)
	}
}

// registerPaneTreeRegions registers hit regions from the same placements the
// canvas drew from, so a click cannot land on geometry the frame did not draw.
// The ORDER is the shared frame's, which is what keeps a click on a stacked
// leaf's tab behaving identically here and in the global Workspaces browser.
func (p *Plugin) registerPaneTreeRegions(layout PaneLayout) {
	paneframe.RegisterRegions(paneRegions{p}, paneHost{p}, layout)
}

// paneDividerHitBox widens a divider's one-cell box into the target a pointer is
// tested against, in the tree's own coordinates. A divider is a cell wide and a
// drag has to be startable on it, so the target reaches one cell into the leaf
// before it — and, on a row split, only upward, because the lower leaf starts
// with a header row a click has to be able to reach.
func paneDividerHitBox(split Divider) Box { return paneframe.DividerHitBox(split) }
