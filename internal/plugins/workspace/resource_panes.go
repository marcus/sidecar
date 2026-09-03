package workspace

import (
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/terminallink"
)

// resourcePane is one Resource leaf on this surface. What a click does, what
// the documented keys do, and when tab state is persisted all live on
// resourceview.Pane; what lives here is only what the project Workspace
// answers differently from the global Workspaces browser — the tree leaf the
// pane occupies and the terminal surface it belongs to, so a selection change
// collapses it rather than carrying a ticket into another shell.
type resourcePane struct {
	leafID  int
	root    string
	surface string
	tabs    *resourceview.Tabs
	pane    *resourceview.Pane
}

func (r *resourcePane) view() *resourceview.Model {
	if r == nil || r.tabs == nil {
		return nil
	}
	return r.tabs.Active()
}

// resourceHost is this surface's half of the shared Resource pane. Each method
// is one of the few places the two workspace projections legitimately differ;
// anything answered here that resourceview could answer instead is drift
// waiting to happen.
type resourceHost struct {
	p   *Plugin
	res *resourcePane
}

var _ resourceview.Host = resourceHost{}

func (h resourceHost) FocusLeaf() { h.p.focusLeaf(h.res.leafID) }

// EnterFromTerminal is the ritual a click in terminal output owes. The
// viewport freeze itself is NOT re-implemented here: activateTerminalLinkAt
// captures the live window before the activation and re-applies it once the
// leaf exists, which is the same capture the document, issue and diff clicks
// get. What is owed here is the rest — dropping a selection the click
// invalidates, ending a gesture whose release will never arrive, and leaving
// interactive input, because a content pane cannot hold the keyboard while the
// terminal still does.
func (h resourceHost) EnterFromTerminal() {
	h.p.clearTerminalSelection()
	h.p.pointer.Abandon()
	if h.p.viewMode == ViewModeInteractive {
		h.p.exitInteractiveMode()
	}
}

func (h resourceHost) Persist() { h.p.saveSelectionState() }

// OpenURL is the same confirmed browser path a clicked URL in terminal output
// takes. A provider names a URL; it never opens one.
func (h resourceHost) OpenURL(url string) tea.Cmd { return openInBrowser(url) }

// The app injects provider state through this interface. Asserting it here
// makes a signature drift a build error rather than a pane that silently never
// appears.
var _ resourceview.Surface = (*Plugin)(nil)

// SetResourceMatchers publishes the live external matcher snapshot the scanner
// may run. The default is empty on purpose: no ready provider means no
// underline and no click target, which is what keeps ordinary terminal output
// ordinary text until a provider has actually described itself.
func (p *Plugin) SetResourceMatchers(matchers []terminallink.ResourceMatcher) {
	p.resourceMatchers = matchers
	p.linkMatcherGeneration++
	p.bumpProjectPreviewRevision()
}

// SetResourceResolver injects how a reference becomes a document. The host
// owns the manager, the process and the timeout; this plugin only says when to
// ask. A nil resolver is valid, and existing panes are rebound because provider
// setup may complete after restored tabs have been constructed.
//
// It also asks the visible pane's active tab again. Restore arms tabs, and
// until a provider is ready there is nothing to ask, so this is the first
// moment a remembered ticket can actually load. Without it the pane sits on
// "Waiting for <provider> to be ready" after every relaunch — of a provider
// that already is — until the user refreshes each tab by hand.
func (p *Plugin) SetResourceResolver(resolve resourceview.Resolver) tea.Cmd {
	p.resolveResource = resolve
	if p.contentDeck != nil {
		// The deck owns the models the projection points at, so asking through
		// the deck is asking the same tabs once, with the deck's own result
		// identity on the answer.
		p.contentDeck.SetResourceResolver(resolve)
		return tea.Batch(unwrapDeckCmds(p.contentDeck.LoadVisibleKind(panelayout.Resource)...)...)
	}
	var cmds []tea.Cmd
	for _, res := range p.resources {
		if res == nil {
			continue
		}
		res.tabs.SetResolver(resolve)
		if cmd := res.tabs.ResolveActive(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (p *Plugin) newResourcePane(leafID int, root, surface string) *resourcePane {
	res := &resourcePane{leafID: leafID, root: root, surface: surface}
	res.tabs = resourceview.NewTabs(p.markdownRenderer, p.resolveResource)
	if p.ctx != nil {
		res.tabs.SetEpoch(p.ctx.Epoch)
	}
	res.pane = resourceview.NewPane(res.tabs, resourceHost{p: p, res: res})
	return res
}

// activeResourcePane returns the first live Resource leaf. A second resource
// key click opens or focuses a tab on this leaf rather than splitting again,
// which mirrors how a file click retargets the document pane.
func (p *Plugin) activeResourcePane() (*resourcePane, *PaneNode) {
	for id, res := range p.resources {
		if res == nil {
			continue
		}
		if leaf := FindPane(p.paneRoot, res.leafID); leaf != nil && leaf.Kind == PaneResource && leaf.ContentID == id {
			return res, leaf
		}
	}
	return nil, nil
}

// activateResourceLink opens the clicked reference against the selected
// terminal surface, so a resource and a document opened from one terminal are
// collapsed together when the selection moves on.
func (p *Plugin) activateResourceLink(ref resourceview.Ref) (tea.Cmd, bool) {
	root, surface, ok := p.selectedTerminalSurface()
	if !ok || !ref.Valid() {
		return nil, false
	}
	cmd := p.openResourcePaneForSurface(root, surface, ref)
	res, _ := p.activeResourcePane()
	if res == nil || res.tabs.Find(resourceview.TabKey(ref)) < 0 {
		return nil, false
	}
	return cmd, true
}

// openResourcePaneForSurface opens ref in the pane tree at the place planOpen
// names. The split is trialled on a clone first, exactly as a document's is: a
// box that cannot hold the result leaves the terminal at the size it already
// has rather than reflowing an agent for a pane that will not be drawn.
func (p *Plugin) openResourcePaneForSurface(root, surface string, ref resourceview.Ref) tea.Cmd {
	return p.openResourcePaneForSurfaceMode(root, surface, ref, true)
}

// openRequestedResourcePaneForSurface is the sidecar-open journey. It shares
// placement with terminal activation but deliberately skips the terminal
// selection/freeze/interactive-exit ritual.
func (p *Plugin) openRequestedResourcePaneForSurface(root, surface string, ref resourceview.Ref) tea.Cmd {
	return p.openResourcePaneForSurfaceMode(root, surface, ref, false)
}

func (p *Plugin) openResourcePaneForSurfaceMode(root, surface string, ref resourceview.Ref, fromTerminal bool) tea.Cmd {
	if p.paneRoot == nil || p.ctx == nil || !ref.Valid() {
		return nil
	}
	if fromTerminal {
		p.clearTerminalSelection()
		p.pointer.Abandon()
	}
	if p.viewMode == ViewModeInteractive {
		p.exitInteractiveMode()
	}
	return p.openWorkspaceContent(root, surface, contentlink.Ref{
		Kind: contentlink.KindResource, Provider: ref.Instance, Matcher: ref.Matcher, Value: ref.Locator,
		Collection: ref.Collection, Query: ref.Query, Filters: resource.EncodeFilters(ref.Filters),
	}, "Resource")
}

// applyResourceResolved delivers a resolve to the tab that asked for it. The
// epoch check is the document pane's: a result that outlived its project has
// nowhere to land. Routing past that is the shared pane's model-ID and
// generation check, so a closed or retargeted tab cannot consume the result.
func (p *Plugin) applyResourceResolved(msg resourceview.ResolvedMsg) {
	if p.ctx == nil || msg.Epoch != p.ctx.Epoch {
		return
	}
	for _, res := range p.resources {
		if res == nil {
			continue
		}
		if res.pane.Apply(msg) {
			return
		}
	}
}

// resourceFocused is the Resource leaf's version of issueFocused: not "a
// content leaf holds focus" but "the focused leaf is a Resource". Without an
// answer here the keys under a highlighted Resource pane would still be the
// agent terminal's — `q` would open the quit confirmation.
func (p *Plugin) resourceFocused() bool {
	res, _ := p.focusedResourcePane()
	return res != nil
}

func (p *Plugin) focusedResourcePane() (*resourcePane, *PaneNode) {
	if !p.previewLeafFocused() {
		return nil, nil
	}
	leaf := FindPane(p.paneRoot, p.paneFocus)
	if leaf == nil || leaf.Kind != PaneResource {
		return nil, nil
	}
	res := p.resources[leaf.ContentID]
	if res == nil {
		return nil, nil
	}
	return res, leaf
}

// focusedResourceTabs is the tab set of the focused Resource leaf, or nil when
// the keyboard is somewhere else. It is what this surface reports keyboard
// ownership from: the leaf's own tabs answer whether a query line or an overlay
// has the keys, so the host's ladder holds its globals back for the same
// reasons on this surface as on the global Sessions one.
func (p *Plugin) focusedResourceTabs() *resourceview.Tabs {
	res, _ := p.focusedResourcePane()
	if res == nil {
		return nil
	}
	return res.tabs
}

// resourcePaneConsumesTextInput reports that a focused collection pane's query
// line is taking typed text.
func (p *Plugin) resourcePaneConsumesTextInput() bool {
	tabs := p.focusedResourceTabs()
	return tabs != nil && tabs.ConsumesTextInput()
}

// resourcePaneBlocksGlobalKeys reports that a focused collection pane has an
// overlay — the View control, an action form — owning the keyboard.
func (p *Plugin) resourcePaneBlocksGlobalKeys() bool {
	tabs := p.focusedResourceTabs()
	return tabs != nil && tabs.BlocksGlobalKeys()
}

// handleResourceKey is the focused Resource leaf's input context. The
// documented pane keys are asked first and answered by the shared pane, so
// both surfaces spell them identically; only the close/hide rule, the sidebar
// toggle and the focus ring are this surface's. Everything else is absorbed: a
// key this pane does not own must not fall through to the terminal behind it.
func (p *Plugin) handleResourceKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	res, _ := p.focusedResourcePane()
	if res == nil {
		return false, nil
	}
	// Before the pane's own keys: esc clears a selection rather than hiding the
	// pane out from under it, and the copy chord must not fall through to a
	// card key that happens to share it.
	if view := res.view(); view != nil {
		if cmd, handled := p.handlePaneSelectionKey(view, msg); handled {
			return true, cmd
		}
	}
	if handled, cmd := p.paneSwitcherKey(msg); handled {
		return true, cmd
	}
	if p.contentDeck != nil {
		switch msg.String() {
		case "}":
			return true, p.cycleWorkspaceDeckTab(panelayout.Resource, 1)
		case "{":
			return true, p.cycleWorkspaceDeckTab(panelayout.Resource, -1)
		}
	}
	if handled, cmd := res.pane.HandleKey(msg.String()); handled {
		return true, cmd
	}
	switch msg.String() {
	case "tab", "shift+tab":
		// Declining Tab keeps the leaf in the ring rather than making it a
		// dead end; the cycle lives on the list keymap.
		return false, nil
	case "\\":
		return true, p.toggleSidebarCmd()
	case "q", "esc":
		return true, p.hideResourcePane()
	case "x":
		return true, p.closeActiveResourceTab()
	}
	return true, nil
}

func (p *Plugin) closeActiveResourceTab() tea.Cmd {
	res, leaf := p.focusedResourcePane()
	if res == nil || leaf == nil || res.tabs == nil {
		return nil
	}
	return p.closeResourceTabAt(res, leaf.ID, res.tabs.ActiveIndex())
}

func (p *Plugin) closeResourceTabAt(res *resourcePane, leafID, index int) tea.Cmd {
	if res == nil || res.pane == nil {
		return nil
	}
	if p.contentDeck != nil {
		return p.closeWorkspaceDeckTabAt(panelayout.Resource, index)
	}
	empty, cmd := res.pane.CloseTab(index)
	if !empty {
		return cmd
	}
	return tea.Batch(cmd, p.closeResourcePane(leafID))
}

func (p *Plugin) selectResourceTab(res *resourcePane, idx int) tea.Cmd {
	if res == nil {
		return nil
	}
	p.pointer.Abandon()
	if p.contentDeck != nil {
		return p.selectWorkspaceDeckTab(panelayout.Resource, idx)
	}
	return res.pane.SelectTab(idx)
}

func (p *Plugin) clickResourceTab(data any) tea.Cmd {
	hit, ok := data.(resourceTabHit)
	if !ok {
		return nil
	}
	leaf := FindPane(p.paneRoot, hit.LeafID)
	if leaf == nil || leaf.Kind != PaneResource {
		return nil
	}
	res := p.resources[leaf.ContentID]
	if hit.Close {
		return p.closeResourceTabAt(res, hit.LeafID, hit.Index)
	}
	return p.selectResourceTab(res, hit.Index)
}

// hideResourcePane collapses the live Resource leaf and remembers its
// references. q/esc hide; last-x forgets through closeResourcePane. Nothing
// else may drop a reference — an unready, failed, disabled or removed provider
// keeps its armed tabs.
func (p *Plugin) hideResourcePane() tea.Cmd {
	res, leaf := p.focusedResourcePane()
	if res == nil || leaf == nil {
		return nil
	}
	return p.hideContentPane(leaf.ID)
}

// closeResourcePane removes the Resource leaf and gives its box back to its
// sibling.
func (p *Plugin) closeResourcePane(leafID int) tea.Cmd {
	return p.forgetContentPane(leafID)
}

func paneLayoutHasResourceTabs(layout *state.PaneLayoutJSON) bool {
	if layout == nil {
		return false
	}
	if len(layout.ResourceTabs) > 0 {
		return true
	}
	if layout.Split == nil {
		return false
	}
	return paneLayoutHasResourceTabs(layout.Split.A) || paneLayoutHasResourceTabs(layout.Split.B)
}

// resourceTabHit is a drawn tab's click target. Index is the tab in the group;
// LeafID is the pane-tree leaf, so two Resource panes cannot steal each
// other's click.
type resourceTabHit struct {
	LeafID int
	Index  int
	Close  bool
}

// layoutResourceTabStrip is the Resource leaf's tab strip. The strip is the
// shared component's, and it is the same call the header draws and the hit
// regions are registered from, so a click cannot land on a tab that was never
// rendered.
func layoutResourceTabStrip(res *resourcePane, width int, focused bool) resourceview.TabStrip {
	var tabs *resourceview.Tabs
	if res != nil {
		tabs = res.tabs
	}
	return resourceview.LayoutTabStrip(tabs, width, focused)
}

// resourcePaneHeaderRow is the Resource leaf's header: the tab strip plus the
// shared X.
func (p *Plugin) resourcePaneHeaderRow(res *resourcePane, width int, focused bool) string {
	return p.composeContentHeader(layoutResourceTabStrip(res, p.reserveHeader(width, true).TabsWidth, focused).HoverClose(p.hoverTabClose.IndexFor(resourceLeafID(res))).Row,
		width, resourceLeafID(res), res != nil && p.hoverPaneClose == res.leafID)
}

func (p *Plugin) registerResourcePaneRegions(res *resourcePane, leafID int, box Box) {
	p.mouseHandler.HitMap.AddRect(regionPaneLeaf, box.X, box.Y, box.W, box.H, leafID)
}

func (p *Plugin) registerResourceTabRegions(res *resourcePane, leafID int, box Box) {
	strip := layoutResourceTabStrip(res, p.reserveHeader(box.W, true).TabsWidth, p.paneFocus == leafID)
	strip.RegisterHits(func(col, width, index int, close bool) {
		p.mouseHandler.HitMap.AddRect(regionResourceTab, box.X+col, box.Y, width, 1,
			resourceTabHit{LeafID: leafID, Index: index, Close: close})
	})
}

// The app injects the protocol-plugin seam through this interface. Asserting it
// here makes a signature drift a build error rather than a collection pane that
// silently never lists anything.
var _ resourceview.PluginSurface = (*Plugin)(nil)

// SetPluginCalls injects how a collection or row tab reaches its plugin, and
// asks the visible pane's active tab again.
//
// The second half is the point, exactly as it is for SetResourceResolver.
// Restore arms tabs, and until the host has described a plugin there is nothing
// to list, so this is the first moment a remembered collection tab can load.
// Without it the pane sits on its loading card after every relaunch — of a
// plugin that is ready — until the user refreshes it by hand.
func (p *Plugin) SetPluginCalls(calls resourceview.CallsFor) tea.Cmd {
	p.pluginCalls = calls
	if p.contentDeck != nil {
		p.contentDeck.SetPluginCalls(calls)
		return tea.Batch(unwrapDeckCmds(p.contentDeck.LoadVisibleKind(panelayout.Resource)...)...)
	}
	var cmds []tea.Cmd
	for _, res := range p.resources {
		if res == nil || res.tabs == nil {
			continue
		}
		res.tabs.SetCallsFor(calls)
		if cmd := res.tabs.ResolveActive(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// applyPluginBrowserMsg routes one of the shared browser's answers to the
// collection and row tabs that are open. It is the collection shape's version
// of applyResourceResolved, and it goes through the deck for the same reason:
// the deck owns the models the projection points at.
func (p *Plugin) applyPluginBrowserMsg(msg tea.Msg) tea.Cmd {
	if p.contentDeck != nil {
		return p.applyWorkspaceDeckBroadcast(msg)
	}
	var cmds []tea.Cmd
	for _, res := range p.resources {
		if res == nil || res.tabs == nil {
			continue
		}
		if cmd, ok := res.tabs.UpdatePlugins(msg); ok && cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// visibleResourceTabs is the active tab of every Resource leaf on screen. Live
// refresh and the poll ticker both read it, and both must see only what is
// actually being looked at: a background tab's plugin is a process spent on a
// frame nobody sees.
func (p *Plugin) visibleResourceTabs() []*resourceview.Model {
	visible := p.visibleContentLeaves()
	var out []*resourceview.Model
	for id, res := range p.resources {
		if res == nil || !visible[id] {
			continue
		}
		if view := res.view(); view != nil {
			out = append(out, view)
		}
	}
	return out
}

// openRowInResourceLeaf is Enter on a collection row: the row opens as a
// sibling tab in the same leaf, through the same journey `sidecar open` takes.
// A row whose tab is already open is focused rather than fetched again.
func (p *Plugin) openRowInResourceLeaf(ref resourceview.Ref) tea.Cmd {
	root, surface, ok := p.selectedTerminalSurface()
	if !ok || !ref.Valid() {
		return nil
	}
	return p.openRequestedResourcePaneForSurface(root, surface, ref)
}
