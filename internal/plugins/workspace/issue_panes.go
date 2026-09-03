package workspace

import (
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/issueview"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/tty"
)

// issuePane is one td issue leaf's tab group. The pane tree points at this,
// not at a single model. The surface is what lets a selection change collapse
// the leaf rather than carry issues from one shell into another.
type issuePane struct {
	leafID  int
	root    string
	surface string
	tabs    issueview.Tabs
	// wheel coalesces one flick over this leaf. A leaf is one scroll surface,
	// so it holds a single burst rather than a keyed set, and dies with the
	// leaf: no held delta can outlive the pane that earned it.
	wheel tty.WheelBurst
}

func (i *issuePane) view() *issueview.Model {
	if i == nil {
		return nil
	}
	return i.tabs.ActiveView()
}

// activeIssuePane returns the first live issue leaf. A second td link click
// opens or focuses a tab on this leaf rather than splitting again, which
// mirrors how a file click retargets the document pane.
func (p *Plugin) activeIssuePane() (*issuePane, *PaneNode) {
	for id, issue := range p.issues {
		if issue == nil {
			continue
		}
		if leaf := FindPane(p.paneRoot, issue.leafID); leaf != nil && leaf.Kind == PaneIssue && leaf.ContentID == id {
			return issue, leaf
		}
	}
	return nil, nil
}

// activateIssueLink opens the clicked td id against the selected terminal
// surface. The surface is the same answer a clicked file is bound to, so an
// issue and a document opened from one terminal are collapsed together when
// the selection moves on.
func (p *Plugin) activateIssueLink(issueID string) (tea.Cmd, bool) {
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		return nil, false
	}
	cmd := p.openIssuePaneForSurface(root, surface, issueID)
	issue, _ := p.activeIssuePane()
	if issue == nil || issue.tabs.Find(issueID) < 0 {
		return nil, false
	}
	p.clearTerminalSelection()
	return cmd, true
}

// openIssuePaneForSurface opens issueID in the pane tree at the place
// planPaneOpen names. The split is trialled on a clone first, exactly as a
// document's is: a box that cannot hold the result leaves the terminal at the
// size it already has rather than reflowing an agent for a pane that will not
// be drawn.
func (p *Plugin) openIssuePaneForSurface(root, surface, issueID string) tea.Cmd {
	issueID = issueview.NormalizeID(issueID)
	if p.paneRoot == nil || p.ctx == nil || issueID == "" {
		return nil
	}
	return p.openWorkspaceContent(root, surface, contentlink.Ref{Kind: contentlink.KindIssue, Value: issueID}, "Issue")
}

// attachIssuePane points the content behind leafID at issueID and returns its
// fetch when a new tab is created or a restored tab still needs one. An
// already-loaded ID is focused and returns nil.
func (p *Plugin) attachIssuePane(leafID int, root, surface, issueID string) tea.Cmd {
	issueID = issueview.NormalizeID(issueID)
	if p.ctx == nil || issueID == "" {
		return nil
	}
	if p.issues == nil {
		p.issues = make(map[int]*issuePane)
	}
	pane := p.issues[leafID]
	if pane == nil {
		pane = &issuePane{leafID: leafID}
		p.issues[leafID] = pane
	}
	pane.root, pane.surface = root, surface
	return p.openOrFocusIssue(pane, issueID)
}

func (p *Plugin) newIssueModel(pane *issuePane) *issueview.Model {
	view := issueview.New(p.markdownRenderer)
	view.OpenHandler = func(id string) tea.Cmd {
		if pane == nil || p.issues[pane.leafID] != pane {
			return nil
		}
		return p.openOrFocusIssue(pane, id)
	}
	// A local miss searches this host's other configured projects before the
	// card declares an issue missing. The handler is read inside the fetch
	// command, so the update goroutine never resolves roots or stats stores.
	view.FallbackRefs = func() []issueview.ProjectRef {
		if p.ctx == nil {
			return nil
		}
		return issueview.ProjectRefsFromConfig(p.ctx.Config)
	}
	// O leaves the pane for td, through the same jump the issue preview modal
	// uses. Nothing about the jump is restated here.
	view.OpenInTDHandler = app.OpenIssueInTD
	return view
}

func (p *Plugin) nextIssueModelID() int {
	p.issueModelNextID++
	return p.issueModelNextID
}

// openOrFocusIssue selects an existing tab for issueID or appends a fresh
// model and loads it. The returned command is the fetch for a new or still
// unloaded tab, or nil when the ID is already loaded.
func (p *Plugin) openOrFocusIssue(pane *issuePane, issueID string) tea.Cmd {
	issueID = issueview.NormalizeID(issueID)
	if pane == nil || p.ctx == nil || issueID == "" {
		return nil
	}
	if idx := pane.tabs.Find(issueID); idx >= 0 {
		pane.tabs.Select(idx)
		return p.ensureActiveIssueTabLoaded(pane)
	}
	view := p.newIssueModel(pane)
	if _, created := pane.tabs.OpenOrFocus(issueID, view); !created {
		return p.ensureActiveIssueTabLoaded(pane)
	}
	return p.loadIssueView(view, pane.root, issueID)
}

// applyIssueLoaded delivers a fetch to the tab that asked for it. The epoch
// check is the document pane's: a result that outlived its project has
// nowhere to land. Routing is pane then tab-by-model-ID, so a closed tab or
// a different live tab cannot consume the result.
func (p *Plugin) applyIssueLoaded(msg issueview.LoadedMsg) {
	if p.ctx == nil || msg.Epoch != p.ctx.Epoch {
		return
	}
	for _, issue := range p.issues {
		if issue == nil {
			continue
		}
		for _, item := range issue.tabs.Items {
			if item.Value == nil || item.Value.ModelID() != msg.ModelID {
				continue
			}
			item.Value.SetResult(msg)
			return
		}
	}
}

// issueFocused is the issue leaf's own version of docFocused: not "a content
// leaf holds focus" but "the focused leaf is an issue". A leaf drawn as focused
// owns the keyboard, and without an answer here the keys under a highlighted
// issue pane are still the agent terminal's — `q` would open the quit
// confirmation, `enter` would start typing at the agent.
func (p *Plugin) issueFocused() bool {
	issue, _ := p.focusedIssuePane()
	return issue != nil
}

// focusedIssuePane is the issue leaf holding preview focus, if that is what
// holds it. It reads paneFocus rather than the first issue in the tree, so a
// key can only ever reach the leaf the frame drew as focused.
func (p *Plugin) focusedIssuePane() (*issuePane, *PaneNode) {
	if !p.previewLeafFocused() {
		return nil, nil
	}
	leaf := FindPane(p.paneRoot, p.paneFocus)
	if leaf == nil || leaf.Kind != PaneIssue {
		return nil, nil
	}
	issue := p.issues[leaf.ContentID]
	if issue == nil {
		return nil, nil
	}
	return issue, leaf
}

// handleIssueKey is the focused issue leaf's input context, the counterpart of
// handleDocKey. It closes and scrolls, and absorbs everything else: a key this
// pane does not own must not fall through to the terminal behind it, which is
// the pane the user is not looking at.
func (p *Plugin) handleIssueKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	issue, _ := p.focusedIssuePane()
	if issue == nil {
		return false, nil
	}
	view := issue.view()
	if view != nil {
		// A focused issue pane is the active card: the pane already owns the
		// keyboard, so arrows walk parent/siblings/subtasks instead of waiting
		// for a second enter the way the preview modal must.
		view.SetActive(true)
		view.SetFocused(true)
	}
	// Before the pane's own keys: esc clears a selection rather than hiding the
	// pane out from under it, and the copy chord must not fall through to a
	// card key that happens to share it.
	if view != nil {
		if cmd, handled := p.handlePaneSelectionKey(view, msg); handled {
			return true, cmd
		}
	}
	if handled, cmd := p.paneSwitcherKey(msg); handled {
		return true, cmd
	}
	switch msg.String() {
	case "tab", "shift+tab":
		// Declining Tab is what keeps the issue leaf in the ring: the cycle
		// lives on the list keymap, where sidebar, terminal, doc, issue and
		// the terminal panel are one sequence. Claiming it here made the leaf
		// a dead end.
		return false, nil
	case "\\":
		return true, p.toggleSidebarCmd()
	case "q", "esc":
		return true, p.hideIssuePane()
	case "x":
		return true, p.closeActiveIssueTab()
	case "{":
		return true, p.cycleActiveIssueTab(-1)
	case "}":
		return true, p.cycleActiveIssueTab(1)
	case "y":
		return true, p.yankFocusedIssue(false)
	case "Y", "shift+y":
		return true, p.yankFocusedIssue(true)
	default:
		if view == nil {
			return true, nil
		}
		beforeActive := issue.tabs.Active
		beforeID, beforeScroll := view.IssueID(), view.ScrollOffset()
		_, cmd := view.HandleKey(msg)
		after := issue.view()
		if issue.tabs.Active != beforeActive ||
			(after != nil && (after.IssueID() != beforeID || after.ScrollOffset() != beforeScroll)) {
			p.saveSelectionState()
		}
		return true, cmd
	}
}

func (p *Plugin) cycleActiveIssueTab(delta int) tea.Cmd {
	issue, _ := p.focusedIssuePane()
	if issue == nil || len(issue.tabs.Items) < 2 {
		return nil
	}
	if p.contentDeck != nil && len(issue.tabs.Items) == workspaceDeckTabCount(p.contentDeck, panelayout.Issue) {
		return p.cycleWorkspaceDeckTab(panelayout.Issue, delta)
	}
	issue.tabs.Cycle(delta)
	p.saveSelectionState()
	return p.ensureActiveIssueTabLoaded(issue)
}

func (p *Plugin) closeActiveIssueTab() tea.Cmd {
	issue, leaf := p.focusedIssuePane()
	if issue == nil || leaf == nil {
		return nil
	}
	return p.closeIssueTabAt(issue, leaf.ID, issue.tabs.Active)
}

func (p *Plugin) closeIssueTabAt(issue *issuePane, leafID, index int) tea.Cmd {
	if issue == nil || index < 0 || index >= len(issue.tabs.Items) {
		return nil
	}
	if p.contentDeck != nil && len(issue.tabs.Items) == workspaceDeckTabCount(p.contentDeck, panelayout.Issue) {
		return p.closeWorkspaceDeckTabAt(panelayout.Issue, index)
	}
	if len(issue.tabs.Items) <= 1 {
		return p.closeIssuePane(leafID)
	}
	issue.tabs.CloseAt(index)
	p.saveSelectionState()
	return p.ensureActiveIssueTabLoaded(issue)
}

func (p *Plugin) selectIssueTab(issue *issuePane, leafID, idx int) tea.Cmd {
	if issue == nil {
		return nil
	}
	p.focusLeaf(leafID)
	p.pointer.Abandon()
	if p.viewMode == ViewModeInteractive {
		p.exitInteractiveMode()
	}
	if idx == issue.tabs.Active {
		return p.ensureActiveIssueTabLoaded(issue)
	}
	if p.contentDeck != nil {
		return p.selectWorkspaceDeckTab(panelayout.Issue, idx)
	}
	issue.tabs.Select(idx)
	p.saveSelectionState()
	return p.ensureActiveIssueTabLoaded(issue)
}

// clickIssueTabAt selects an issue tab from a pointer position. File tabs
// test the tab row first so a one-cell miss on a widened divider does not
// become a terminal click; issue tabs need the same steal.
func (p *Plugin) clickIssueTabAt(x, y int) (tea.Cmd, bool) {
	if !p.docVisible() {
		return nil, false
	}
	var tabs []mouse.Region
	var closeAt *mouse.Region
	for _, region := range p.mouseHandler.HitMap.Regions() {
		if region.ID != regionIssueTab {
			continue
		}
		if y != region.Rect.Y {
			continue
		}
		hit, ok := region.Data.(issueTabHit)
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
	inIssueHeader := false
	for _, region := range p.mouseHandler.HitMap.Regions() {
		if region.ID != regionPaneLeaf {
			continue
		}
		header := insetPanelChrome(region.Rect)
		if x >= header.X && x < header.X+header.W && y == header.Y {
			inIssueHeader = true
			break
		}
	}
	if !inIssueHeader {
		return nil, false
	}
	if closeAt != nil {
		return p.clickIssueTab(closeAt.Data), true
	}
	best := tabs[0]
	bestDist := tabRowDistance(x, best.Rect)
	for _, region := range tabs[1:] {
		if d := tabRowDistance(x, region.Rect); d < bestDist {
			best, bestDist = region, d
		}
	}
	return p.clickIssueTab(best.Data), true
}

func (p *Plugin) clickIssueTab(data any) tea.Cmd {
	hit, ok := data.(issueTabHit)
	if !ok {
		return nil
	}
	leaf := FindPane(p.paneRoot, hit.LeafID)
	if leaf == nil || leaf.Kind != PaneIssue {
		return nil
	}
	issue := p.issues[leaf.ContentID]
	if issue == nil {
		return nil
	}
	if hit.Close {
		return p.closeIssueTabAt(issue, hit.LeafID, hit.Index)
	}
	return p.selectIssueTab(issue, hit.LeafID, hit.Index)
}

// hideIssuePane collapses the live issue leaf and remembers the tab set. q/esc
// hide; last-x forgets through closeIssuePane.
func (p *Plugin) hideIssuePane() tea.Cmd {
	issue, leaf := p.focusedIssuePane()
	if issue == nil || leaf == nil {
		return nil
	}
	return p.hideContentPane(leaf.ID)
}

func (p *Plugin) ensureActiveIssueTabLoaded(issue *issuePane) tea.Cmd {
	if issue == nil || p.ctx == nil {
		return nil
	}
	view := issue.view()
	if view == nil || !view.NeedsLoad() {
		return nil
	}
	id := issueview.NormalizeID(view.IssueID())
	if id == "" {
		if item, ok := issue.tabs.ActiveItem(); ok {
			id = issueview.NormalizeID(item.Key)
		}
	}
	if id == "" {
		return nil
	}
	return p.loadIssueView(view, issue.root, id)
}

func (p *Plugin) loadIssueView(view *issueview.Model, root, issueID string) tea.Cmd {
	if view == nil || p.ctx == nil {
		return nil
	}
	modelID := view.ModelID()
	if modelID == 0 {
		modelID = p.nextIssueModelID()
	}
	return view.Load(modelID, issueLoadRoot(view, root), issueID, p.ctx.Epoch)
}

// issueLoadRoot picks the directory a card loads from: an adopted cross-
// project card keeps its owning store so restore and reload never re-run the
// search; every other card loads from the pane's own root. State-free.
func issueLoadRoot(view *issueview.Model, root string) string {
	if adopted := view.WorkDir(); adopted != "" {
		return adopted
	}
	return root
}

func persistedIssueTabs(saved *state.PaneLayoutJSON) []state.PaneIssueTabJSON {
	if saved == nil {
		return nil
	}
	if saved.IssueTabs != nil {
		return saved.IssueTabs
	}
	if id := issueview.NormalizeID(saved.Issue); id != "" {
		return []state.PaneIssueTabJSON{{Issue: id, Scroll: saved.Scroll}}
	}
	return nil
}

func normalizePersistedIssueLeaves(layout *state.PaneLayoutJSON) {
	if layout == nil {
		return
	}
	if layout.Split != nil {
		normalizePersistedIssueLeaves(layout.Split.A)
		normalizePersistedIssueLeaves(layout.Split.B)
		return
	}
	if layout.Kind != contentKindIssue {
		return
	}
	raw := persistedIssueTabs(layout)
	wanted := layout.Active
	if layout.IssueTabs == nil || wanted < 0 || wanted >= len(raw) {
		wanted = 0
	}
	tabs := make([]state.PaneIssueTabJSON, 0, len(raw))
	active := 0
	for i, tab := range raw {
		id := issueview.NormalizeID(tab.Issue)
		if id == "" {
			continue
		}
		if i == wanted {
			active = len(tabs)
		}
		// Owner fields ride along so a cross-project tab survives migration.
		tabs = append(tabs, state.PaneIssueTabJSON{Issue: id, Scroll: tab.Scroll,
			OwnerName: tab.OwnerName, OwnerRoot: tab.OwnerRoot})
	}
	if len(tabs) == 0 {
		layout.IssueTabs = nil
	} else {
		layout.IssueTabs = tabs
	}
	layout.Active = active
	layout.Issue = ""
	layout.Scroll = 0
}

// closeIssuePane removes the issue leaf and gives its box back to its sibling.
func (p *Plugin) closeIssuePane(leafID int) tea.Cmd {
	return p.forgetContentPane(leafID)
}

// issuePaneHeaderRow is the issue leaf's header: the tab strip plus the shared X.
func (p *Plugin) issuePaneHeaderRow(issue *issuePane, width int, focused bool) string {
	return p.composeContentHeader(layoutIssueTabStrip(issue, p.reserveHeader(width, true).TabsWidth, focused).HoverClose(p.hoverTabClose.IndexFor(issueLeafID(issue))).Row, width, issueLeafID(issue), issue != nil && p.hoverPaneClose == issue.leafID)
}

func (p *Plugin) registerIssuePaneRegions(issue *issuePane, leafID int, box Box) {
	p.mouseHandler.HitMap.AddRect(regionPaneLeaf, box.X, box.Y, box.W, box.H, leafID)
}

// finishIssueScrollbarDrag settles an issue card's scrollbar gesture and
// forgets which leaf it belonged to. Safe to call with no gesture live.
func (p *Plugin) finishIssueScrollbarDrag() {
	if issue := p.issues[p.issueScrollLeaf]; issue != nil {
		if view := issue.view(); view != nil {
			view.ScrollbarDragEnd()
		}
	}
	p.issueScrollLeaf = 0
}

func (p *Plugin) registerIssueTabRegions(issue *issuePane, leafID int, box Box) {
	strip := layoutIssueTabStrip(issue, p.reserveHeader(box.W, true).TabsWidth, p.paneFocus == leafID)
	strip.RegisterHits(func(col, width, index int, close bool) {
		p.mouseHandler.HitMap.AddRect(regionIssueTab, box.X+col, box.Y, width, 1, issueTabHit{LeafID: leafID, Index: index, Close: close})
	})
}

// issueLeafAt resolves a pane-leaf region's payload to the issue it names. It
// answers nil for a document leaf, which is how the merged region's arms tell
// the two kinds apart: the tree is the answer, not the region's name.
func (p *Plugin) issueLeafAt(data any) (*issuePane, *PaneNode) {
	leafID, ok := data.(int)
	if !ok {
		return nil, nil
	}
	leaf := FindPane(p.paneRoot, leafID)
	if leaf == nil || leaf.Kind != PaneIssue {
		return nil, nil
	}
	issue := p.issues[leaf.ContentID]
	if issue == nil || issue.view() == nil {
		return nil, nil
	}
	return issue, leaf
}

func issueViewLocal(actionX, actionY int, box Box) (int, int) {
	// regionPaneLeaf is the OUTER panel; the viewer lives in the inner box.
	inner := insetPanelChrome(box)
	return actionX - inner.X, actionY - inner.Y - terminalHeaderRows
}

func (p *Plugin) yankFocusedIssue(idOnly bool) tea.Cmd {
	issue, _ := p.focusedIssuePane()
	view := issue.view()
	if view == nil {
		return nil
	}
	data := view.Data()
	if data == nil {
		return nil
	}
	if idOnly {
		return issueview.CopyID(data)
	}
	return issueview.CopyMarkdown(data)
}
