package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/issueview"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/livepanes"
	"github.com/marcus/sidecar/internal/livewatch"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/mouse"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/noteview"
	"github.com/marcus/sidecar/internal/paneframe"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/panereposition"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/tabs"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/terminalperf"
	"github.com/marcus/sidecar/internal/textselect"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacediff"
)

const (
	appDeckLeafRegion    = "app-content-leaf"
	appDeckDividerRegion = "app-content-divider"
	appDeckTabRegion     = "app-content-tab"
	appDeckCloseRegion   = "app-content-close"
	appDeckLayoutRegion  = "app-content-layout"
)

type appContentResolvedMsg struct {
	Key    string
	Result contentlink.ResolutionResult
}

type appContentResolutionKey struct {
	Root      string
	Candidate contentlink.Pending
}

type appContentLinkHit struct {
	Generation uint64
	Ref        contentlink.Ref
	Rect       mouse.Rect
}

type appContentDeck struct {
	key, workdir, stateRoot, pluginID string
	global                            bool
	deck                              *contentpanes.Deck
	root                              *panelayout.Node
	layoutModal                       *panereposition.Controller
	zoom                              panereposition.Zoom
	plugin                            plugin.Plugin
	layout                            panelayout.Layout
	laidOut                           bool
	canvas                            paneframe.Box
	mouse                             *mouse.Handler
	queued                            []tea.Cmd
	// pluginSize is the geometry last announced to plugin, and pluginSized
	// distinguishes "never sized" from a genuine zero-sized frame.
	pluginSize        paneframe.Size
	pluginSized       bool
	primaryInner      paneframe.Box
	links             []appContentLinkHit
	tabHits           []appDeckTabHit
	hoverTabClose     tabs.CloseHover
	hoverLayout       int
	generation        uint64
	press             *appContentLinkHit
	pressX, pressY    int
	dragged           bool
	resolution        *contentlink.ResolutionIndex
	pending           map[appContentResolutionKey]bool
	resourceMatchers  []contentlink.ResourceMatcher
	matcherGeneration uint64
	dragSplit         int
	search            appDeckSearch
	info              *docview.Info
	infoLeaf          int
	live              *livepanes.Set
	suppressRefresh   bool
	edit              *appDeckDocumentEdit
	// wheel holds one flick per scrollable leaf this deck draws, and wheelNow
	// is its clock (nil is the wall clock, replaced by tests). Each leaf
	// scrolls independently, so the delta one of them holds back belongs to it
	// alone; today only the issue card coalesces here.
	wheel    tty.WheelBursts
	wheelNow func() time.Time
	// Pointer state for hosted panes' interactive scrollbars. selectGestureLeaf
	// is the leaf whose HandleSelectionMouse gesture — a document's bar or any
	// selectable pane's text selection — is live; issueScroll* carry an issue
	// card's bar gesture and the absolute
	// Y of its row 0 at press time; noteBar is the host-side bookkeeping a
	// state-free noteview seam leaves to this surface. selKeys and
	// selCopyOnSelect bind the shared selection chords to every selectable
	// pane at render time, the only place both are known.
	selectGestureLeaf int
	issueScrollLeaf   int
	issueScrollTrackY int
	noteBar           appDeckNoteBar
	selKeys           textselect.Keys
	selCopyOnSelect   bool
}

func appDeckKey(workdir, pluginID string) string { return workdir + "\x00" + pluginID }

func (m *Model) appDeckSurfaceContext(workdir, pluginID string, epoch uint64) contentpanes.SurfaceContext {
	return contentpanes.SurfaceContext{
		Root: workdir, DiffRoot: workdir, Surface: pluginID, Epoch: epoch,
		Source: contentpanes.SourceContext{ProjectRoot: m.ui.ProjectRoot, Root: workdir},
	}
}

// globalDeckRoot is the persistence root for a hosted global plugin's content
// deck. It is keyed by plugin ID so a second global plugin gets its own deck
// rather than inheriting the first one's saved layout.
func globalDeckRoot(pluginID string) string { return "@global-" + pluginID }

func (m *Model) contentDeckEligible(p plugin.Plugin) bool {
	if p == nil || p.ID() == workspacePluginID || !features.IsEnabled(features.PluginContentPanes.Name) {
		return false
	}
	_, links := p.(plugin.ContentLinkProvider)
	_, focus := p.(plugin.PaneFocusProvider)
	return links && focus
}

func (m *Model) activeContentDeck() *appContentDeck {
	if m.configOpen() {
		return nil
	}
	p, stateRoot, global := m.contentDeckSurface()
	if !m.contentDeckEligible(p) {
		return nil
	}
	key := appDeckKey(stateRoot, p.ID())
	h := m.contentDecks[key]
	ctx := m.appDeckSurfaceContext(m.ui.WorkDir, p.ID(), m.registry.Context().Epoch)
	if h == nil {
		cfg := contentpanes.Config{ConfigureViewer: configureAppDeckViewer, Source: contentpanes.LocalSource{}}
		if manager := ResourceProviderManager(); manager != nil {
			cfg.ResourceResolver = resourceResolver(manager)
		}
		var saved contentpanes.State
		if raw := state.GetContentDeck(stateRoot, p.ID()); len(raw) > 0 {
			_ = json.Unmarshal(raw, &saved)
		}
		h = &appContentDeck{key: key, workdir: m.ui.WorkDir, stateRoot: stateRoot, pluginID: p.ID(), plugin: p, global: global,
			mouse: mouse.NewHandler(), resolution: contentlink.NewResolutionIndex(contentlink.MaxPendingResolutions),
			pending: make(map[appContentResolutionKey]bool), matcherGeneration: 1}
		h.live = h.newLiveSet()
		if manager := ResourceProviderManager(); manager != nil {
			h.resourceMatchers = manager.Snapshot().TerminalMatchers()
		}
		if saved.Root != nil {
			h.deck = contentpanes.Decode(ctx, cfg, saved)
			h.queued = append(h.queued, h.deck.LoadVisible()...)
		} else {
			h.deck = contentpanes.New(ctx, cfg)
		}
		m.contentDecks[key] = h
	} else {
		h.plugin = p
		h.workdir = m.ui.WorkDir
		h.queued = append(h.queued, h.deck.SetContext(ctx)...)
	}
	h.syncInnerFocus()
	return h
}

func (m Model) currentContentDeck() *appContentDeck {
	if m.configOpen() || m.registry == nil {
		return nil
	}
	p, stateRoot, _ := m.contentDeckSurface()
	if !m.contentDeckEligible(p) {
		return nil
	}
	return m.contentDecks[appDeckKey(stateRoot, p.ID())]
}

// appContentPassiveFocused reports that the visible keyboard focus is in an
// app-owned sibling of the primary plugin leaf. Primary may retain internal
// modal state while focus is away, but that hidden state does not own keys.
func (m Model) appContentPassiveFocused() bool {
	h := m.currentContentDeck()
	if h == nil || h.deck == nil {
		return false
	}
	leaf := panelayout.Find(h.deck.Tree(), h.deck.FocusedLeaf())
	return leaf != nil && leaf.Kind != panelayout.Primary
}

func (m Model) contentDeckSurface() (plugin.Plugin, string, bool) {
	if host := m.focusedGlobalHost(); host != nil {
		return host.plugin, globalDeckRoot(host.id()), true
	}
	if m.inGlobalScope() {
		return nil, "", false
	}
	return m.ActivePlugin(), m.ui.WorkDir, false
}

func appDeckFloors() panelayout.Floors {
	return paneframe.ChromeFloorsFor(panelayout.Floors{
		// Files itself contains a tree and preview split. Below this width its
		// inner minimums wrap despite receiving the correct leaf size, so preserve
		// the useful primary surface and refuse another outer split.
		Primary:  panelayout.Floor{Width: 80, Height: 10},
		Doc:      panelayout.Floor{Width: markdown.MinWidthForMarkdown, Height: 8},
		Issue:    panelayout.Floor{Width: markdown.MinWidthForMarkdown, Height: 8},
		Note:     panelayout.Floor{Width: markdown.MinWidthForMarkdown, Height: 8},
		Diff:     panelayout.Floor{Width: markdown.MinWidthForMarkdown, Height: 8},
		Resource: panelayout.Floor{Width: markdown.MinWidthForMarkdown, Height: 8},
	}, appDeckChromeForKind)
}

func appDeckChromeForKind(kind panelayout.Kind) paneframe.Chrome {
	if kind == panelayout.Primary {
		return paneframe.ChromeNone
	}
	return paneframe.ChromeIdle
}

func (m *Model) renderContentDeck(h *appContentDeck, width, height int) string {
	if h == nil || width <= 0 || height <= 0 {
		return ""
	}
	h.plugin = m.focusedSurface()
	h.suppressRefresh = m.hasModal() || m.configOpen() || (m.inGlobalScope() && !h.global)
	// The chords a document's selection answers are a render-time binding: this
	// is where the deck knows both which surface is drawn and what the user
	// configured.
	terminal := TerminalConfig(m.cfg)
	h.selKeys, h.selCopyOnSelect = terminal.SelectionKeys(), terminal.CopyOnSelect
	for _, other := range m.contentDecks {
		if other != h && other.laidOut {
			other.releaseAppContentInputs()
			other.laidOut = false
			if other.live != nil {
				other.queued = append(other.queued, other.live.Reconcile())
			}
		}
	}
	h.generation++
	h.links = nil
	h.tabHits = nil
	h.mouse.Clear()
	h.canvas = paneframe.Box{W: width, H: height}
	h.syncLayoutProjection()
	zoom := h.zoom.Leaf(h.layoutScope(), h.root)
	layout, ok := panelayout.LayoutTreeWithZoom(h.root, h.canvas, appDeckFloors(), h.deck.FocusedLeaf(), zoom)
	h.layout, h.laidOut = layout, ok
	if !ok {
		return ui.FitBlock(h.plugin.View(width, height), width, height)
	}
	view := paneframe.Compose(appDeckHost{h}, layout, h.canvas, width, height)
	m.adoptAppContentPlugin(h)
	paneframe.RegisterRegions(appDeckRegions{h}, appDeckHost{h}, layout)
	h.registerAppContentSearchRegions()
	if h.info != nil {
		view = ui.OverlayModal(view, h.info.Render(width, height, h.mouse), width, height)
	}
	if h.live != nil {
		h.queued = append(h.queued, h.live.Reconcile())
	}
	return view
}

func (h *appContentDeck) visibleDocument() *docview.Model {
	if h == nil || !h.laidOut || h.deck == nil {
		return nil
	}
	leafID := h.deck.Leaf(panelayout.Document)
	for _, placement := range h.layout.Leaves {
		if placement.Node != nil && placement.Node.ID == leafID {
			view, _ := h.deck.Viewer(leafID).(*docview.Model)
			return view
		}
	}
	return nil
}

func (h *appContentDeck) newLiveSet() *livepanes.Set {
	return livepanes.NewSet("app-content:"+h.key, func() uint64 {
		if h.deck == nil {
			return 0
		}
		return h.deck.Context().Epoch
	}, livepanes.Binding{
		Kind:   "docs",
		Config: livewatch.Config{},
		Targets: func() []livewatch.Target {
			if h.deck != nil && h.deck.Context().Source.Remote() {
				return nil
			}
			view := h.visibleDocument()
			if view == nil {
				return nil
			}
			view.SetRoot(h.workdir)
			if target := view.WatchTarget(); target.Path != "" {
				return []livewatch.Target{target}
			}
			return nil
		},
		Refresh: func() []tea.Cmd {
			view := h.visibleDocument()
			if view == nil {
				return nil
			}
			view.Observe()
			if cmd := view.Refresh(h.suppressRefresh); cmd != nil {
				return []tea.Cmd{cmd}
			}
			return nil
		},
		Owed: func() bool {
			view := h.visibleDocument()
			return view != nil && view.RefreshPending()
		},
	})
}

type appDeckHost struct{ h *appContentDeck }

func (x appDeckHost) Content(n *panelayout.Node) paneframe.Content {
	return &appDeckContent{h: x.h, node: n}
}
func (x appDeckHost) Focus() int { return x.h.deck.FocusedLeaf() }
func (x appDeckHost) SetFocus(n *panelayout.Node) {
	if n != nil && n.Split == nil {
		x.h.deck.FocusLeaf(n.ID)
		x.h.syncInnerFocus()
	}
}
func (x appDeckHost) Layout() (panelayout.Layout, bool) { return x.h.layout, x.h.laidOut }
func (x appDeckHost) HandleState(splitID int) ui.HandleState {
	return paneframe.HandleStateFor(splitID, x.h.mouse.IsDragging(), x.h.dragSplit, false, 0)
}
func (x appDeckHost) QueueSizeCmd(cmd tea.Cmd) { x.h.queued = append(x.h.queued, cmd) }
func (x appDeckHost) Chrome(n *panelayout.Node) paneframe.Chrome {
	if n != nil && n.Kind == panelayout.Primary {
		return paneframe.ChromeNone
	}
	if n != nil && n.ID == x.h.deck.FocusedLeaf() {
		return paneframe.ChromeActive
	}
	return paneframe.ChromeIdle
}

type appDeckContent struct {
	h    *appContentDeck
	node *panelayout.Node
	size paneframe.Size
}

func (c *appDeckContent) Kind() string { return fmt.Sprint(c.node.Kind) }
func (c *appDeckContent) Title() string {
	if c.node.Kind == panelayout.Primary {
		return c.h.plugin.Name()
	}
	if v := c.h.deck.Viewer(c.node.ID); v != nil {
		switch v := v.(type) {
		case *docview.Model:
			return v.Title()
		case *issueview.Model:
			return v.Title()
		case *noteview.Model:
			return v.Title()
		case *workspacediff.View:
			return v.Target.TabLabel()
		case *resourceview.Model:
			return v.Title()
		}
	}
	return ""
}

// SetSize is called on every render, and appDeckContent is rebuilt each time,
// so the geometry the plugin was last told has to be remembered on the deck. A
// resize is a state change, not a frame: re-announcing the same size every
// frame hands the plugin a WindowSizeMsg it has already answered, and any
// plugin that returns a command for one turns a static frame into a message
// loop. An embedded td with an issue modal open re-rendered its description
// markdown ~150 times a second on a terminal nobody had touched (td-fcb03a).
func (c *appDeckContent) SetSize(size paneframe.Size) tea.Cmd {
	c.size = size
	if c.node.Kind == panelayout.Document {
		if view, ok := c.h.deck.Viewer(c.node.ID).(*docview.Model); ok {
			bodyH := max(size.Height-paneframe.HeaderRows, 0)
			view.SetSize(size.Width, bodyH)
			view.SetSelection(c.h.selKeys, c.h.selCopyOnSelect)
			c.h.prepareDocumentLeaf(view)
		}
	}
	if c.node.Kind != panelayout.Primary || c.h.plugin == nil {
		return nil
	}
	if c.h.pluginSized && c.h.pluginSize == size {
		return nil
	}
	c.h.pluginSize, c.h.pluginSized = size, true
	updated, cmd := c.h.plugin.Update(tea.WindowSizeMsg{Width: size.Width, Height: size.Height})
	c.h.plugin = updated
	return cmd
}
func (c *appDeckContent) View(render paneframe.Render) string {
	if c.node.Kind == panelayout.Primary {
		c.h.primaryInner = paneframe.Box(render.Origin)
		// A plugin that owns a selectable box inside its own frame is bound the
		// same way a hosted viewer is: the host resolves the chords from config
		// once, and everything it draws answers them identically.
		if binder, ok := c.h.plugin.(textselect.Binder); ok {
			binder.SetSelection(c.h.selKeys, c.h.selCopyOnSelect)
		}
		frame := c.h.plugin.View(c.size.Width, c.size.Height)
		frame = c.h.scanPrimary(frame, render.Origin)
		return ui.FitBlock(frame, c.size.Width, c.size.Height)
	}
	if c.node.Kind == panelayout.Document {
		if frame, ok := c.h.renderAppContentDocumentEdit(c.node.ID, c.size); ok {
			return ui.FitBlock(frame, c.size.Width, c.size.Height)
		}
	}
	bodyH := max(c.size.Height-paneframe.HeaderRows, 0)
	body := ""
	switch v := c.h.deck.Viewer(c.node.ID).(type) {
	case *docview.Model:
		// The body sits below the leaf's own tab-header row. Recording where
		// it was drawn is what makes the viewer's own bar hit-testing agree
		// with the regions the frame registers for it.
		v.SetOrigin(render.Origin.X, render.Origin.Y+paneframe.HeaderRows)
		body = c.h.preparedDocumentLeaf(v, render.Origin.X, render.Origin.Y+paneframe.HeaderRows)
		body = c.h.renderAppContentSearch(c.node.ID, body,
			mouse.Rect{X: render.Origin.X, Y: render.Origin.Y + paneframe.HeaderRows},
			paneframe.Size{Width: c.size.Width, Height: bodyH})
	// Issue, note, resource, and diff bodies stay unscanned here on purpose:
	// they scan on no surface today — not Workspace, not the global browser —
	// and making them scan is a wider change than closing the gap between a
	// document opened beside Files and the byte-identical Workspace pane.
	//
	// There is no test pinning this. Every one of these viewers needs a live
	// backend (td, a resource provider, git) before it draws a token at all, so
	// a unit test asserting "no hits" here would be green whether or not the
	// body were scanned. Reviewing a change to this switch is the control.
	case *issueview.Model:
		v.SetSize(c.size.Width, bodyH)
		// The body sits below the leaf's own tab-header row; recording where it
		// was drawn is what lets a press map onto the card's own rows.
		v.SetOrigin(render.Origin.X, render.Origin.Y+paneframe.HeaderRows)
		v.SetSelection(c.h.selKeys, c.h.selCopyOnSelect)
		body = v.View()
	case *noteview.Model:
		v.SetSize(c.size.Width, bodyH)
		body = v.View()
	case *workspacediff.View:
		v.SetSize(c.size.Width, bodyH)
		v.SetOrigin(render.Origin.X, render.Origin.Y+paneframe.HeaderRows)
		v.SetSelection(c.h.selKeys, c.h.selCopyOnSelect)
		body = v.Render(c.size.Width, bodyH, workspacediff.RenderOpts{})
	case *resourceview.Model:
		v.SetSize(c.size.Width, bodyH)
		v.SetOrigin(render.Origin.X, render.Origin.Y+paneframe.HeaderRows)
		v.SetSelection(c.h.selKeys, c.h.selCopyOnSelect)
		body = v.View()
	}
	return ui.FitBlock(c.h.tabHeader(c.node.ID, c.size.Width, render.Origin, render.Focused)+"\n"+body, c.size.Width, c.size.Height)
}

type appDeckTabHit struct {
	leafID, index int
	close         bool
	rect          mouse.Rect
}

func (h *appContentDeck) tabHeader(leafID, width int, origin mouse.Rect, focused bool) string {
	items, active := h.deck.Tabs(leafID)
	labels := make([]tabs.Label, 0, len(items))
	for i, item := range items {
		label := item.Ref.Value
		if h.search.mode != nil && h.search.leafID == leafID && i == active {
			label = h.search.mode.HeaderLabel()
		}
		if view, ok := item.Viewer.(*noteview.Model); ok && view.Title() != "" {
			label = view.Title()
		}
		if label == "" {
			label = string(item.Ref.Kind)
		}
		labels = append(labels, tabs.Label{Text: label})
	}
	reserve := h.reserveHeader(width, true)
	strip := tabs.LayoutStrip(labels, active, reserve.TabsWidth, focused, nil)
	strip.RegisterHits(func(col, width, index int, close bool) {
		h.tabHits = append(h.tabHits, appDeckTabHit{
			leafID: leafID, index: index, close: close,
			rect: mouse.Rect{X: origin.X + col, Y: origin.Y, W: width, H: 1},
		})
	})
	return h.composeHeader(strip.HoverClose(h.hoverTabClose.IndexFor(leafID)).Row, width, true, h.hoverLayout == leafID, false)
}

// setTabCloseHover lights the × of the deck tab the pointer is inside. Only
// the × half of a pill hovers; the rest of it selects.
func (h *appContentDeck) setTabCloseHover(x, y int) {
	h.hoverTabClose = tabs.CloseHover{}
	if h.mouse == nil {
		return
	}
	region := h.mouse.HitMap.Test(x, y)
	if region == nil || region.ID != appDeckTabRegion {
		return
	}
	hit, ok := region.Data.(appDeckTabHit)
	if !ok || !hit.close {
		return
	}
	h.hoverTabClose = tabs.CloseHoverAt(hit.leafID, hit.index)
}

func (h *appContentDeck) syncInnerFocus() {
	if h.search.mode != nil && h.search.leafID != h.deck.FocusedLeaf() {
		h.closeAppContentSearch()
	}
	if h.info != nil && h.infoLeaf != h.deck.FocusedLeaf() {
		h.info, h.infoLeaf = nil, 0
	}
	provider, ok := h.plugin.(plugin.PaneFocusProvider)
	if ok {
		provider.SetPaneFocusActive(h.deck.Leaf(panelayout.Primary) == h.deck.FocusedLeaf())
	}
	for _, placement := range h.layout.Leaves {
		if view, ok := h.deck.Viewer(placement.Node.ID).(*issueview.Model); ok {
			focused := placement.Node.ID == h.deck.FocusedLeaf()
			view.SetActive(focused)
			view.SetFocused(focused)
		}
	}
}

func configureAppDeckViewer(kind panelayout.Kind, model any) {
	if kind != panelayout.Issue {
		return
	}
	view, ok := model.(*issueview.Model)
	if !ok {
		return
	}
	view.OpenHandler = func(issueID string) tea.Cmd {
		return func() tea.Msg {
			return ActivateTargetMsg{Target: uirequest.Target{Kind: uirequest.TargetKindIssue, Value: issueID}}
		}
	}
	view.OpenInTDHandler = OpenIssueInTD
}

type appDeckRegions struct{ h *appContentDeck }

func (r appDeckRegions) Leaf(n *panelayout.Node, b paneframe.Box) {
	r.h.mouse.HitMap.AddRect(appDeckLeafRegion, b.X, b.Y, b.W, b.H, n.ID)
}
func (r appDeckRegions) Divider(id int, b paneframe.Box) {
	r.h.mouse.HitMap.AddRect(appDeckDividerRegion, b.X, b.Y, b.W, b.H, id)
}
func (r appDeckRegions) Tabs(n *panelayout.Node, b paneframe.Box) {
	for _, hit := range r.h.tabHits {
		if n != nil && hit.leafID == n.ID {
			r.h.mouse.HitMap.Add(appDeckTabRegion, hit.rect, hit)
		}
	}
}

// Title is the header name's own target. The deck hosts no leaf that is
// renamed from its pane, so it registers nothing and the tab strip under it
// keeps the cells.
func (r appDeckRegions) Title(*panelayout.Node, paneframe.Box) {}

// Layout is wired by the reposition-modal host adapter. The shared frame owns
// its exact precedence between title and close.
func (r appDeckRegions) Layout(n *panelayout.Node, b paneframe.Box) {
	if n == nil || n.Split != nil || n.Kind == panelayout.Primary {
		return
	}
	reserve := r.h.reserveHeader(b.W, true)
	if reserve.LayoutW < 1 {
		return
	}
	r.h.mouse.HitMap.AddRect(appDeckLayoutRegion, b.X+reserve.LayoutCol, b.Y, reserve.LayoutW, 1, n.ID)
}

func (r appDeckRegions) Close(n *panelayout.Node, b paneframe.Box) {
	if n.Kind == panelayout.Primary || b.W <= 0 {
		return
	}
	// The drawn × is the padded three-cell button from ComposeHeaderClose, so
	// the hit rect must be the same reserved geometry. Registering only the
	// last column left the glyph itself dead: clicks had to land one cell to
	// its right to close.
	reserve := r.h.reserveHeader(b.W, true)
	if reserve.CloseW < 1 {
		return
	}
	r.h.mouse.HitMap.AddRect(appDeckCloseRegion, b.X+reserve.CloseCol, b.Y, reserve.CloseW, 1, n.ID)
}

// Body registers what a hosted pane owns inside its chrome-aware box. The
// frame calls it last, so the scrollbar columns registered here win the hit
// test over the leaf body drawn under them while every frame-owned region —
// leaf, divider, tabs, title, close — keeps its established priority.
func (r appDeckRegions) Body(n *panelayout.Node, b paneframe.Box) {
	if n == nil || n.Kind == panelayout.Primary {
		return
	}
	r.h.registerAppContentScrollbars(n, b)
}

func (h *appContentDeck) scanPrimary(frame string, origin mouse.Rect) string {
	provider, ok := h.plugin.(plugin.ContentLinkProvider)
	if !ok {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for _, surface := range provider.ContentLinkSurfaces() {
		if !surface.ReadOnly || surface.Rect.W <= 0 || surface.Rect.H <= 0 {
			continue
		}
		for row := 0; row < surface.Rect.H && surface.Rect.Y+row < len(lines); row++ {
			y := surface.Rect.Y + row
			segment := ansi.Cut(lines[y], surface.Rect.X, surface.Rect.X+surface.Rect.W)
			allowedKinds := surface.Kinds
			if allowedKinds == nil {
				// Surface.KindSet's zero value is allow-none. ScanFrame keeps nil
				// as allow-all for direct compatibility callers, so adapt the two
				// contracts explicitly at this boundary.
				allowedKinds = contentlink.KindSet{}
			}
			result := contentlink.ScanFrame(segment, contentlink.FrameOptions{Ready: h.resolution.SnapshotForRoot(surface.WorkDir), Matchers: h.resourceMatchers,
				InternalNamespaces: sidecarIntentNamespaces, AllowedKinds: allowedKinds, Decorate: true,
				RendererOwned: surface.RendererOwned})
			for _, span := range result.Spans {
				h.links = append(h.links, appContentLinkHit{Generation: h.generation, Ref: span.Ref(), Rect: mouse.Rect{
					X: origin.X + surface.Rect.X + span.StartCol, Y: origin.Y + y, W: span.EndCol - span.StartCol + 1, H: 1,
				}})
			}
			for _, candidate := range result.Pending {
				h.queueContentLinkResolve(surface.WorkDir, candidate)
			}
			prefix := ansi.Cut(lines[y], 0, surface.Rect.X)
			suffix := ansi.Cut(lines[y], surface.Rect.X+surface.Rect.W, ansi.StringWidth(lines[y]))
			lines[y] = prefix + result.Output + suffix
		}
	}
	return strings.Join(lines, "\n")
}

// prepareDocumentLeaf recognizes tokens in a document leaf the deck drew beside
// its plugin. A file opened as a Document leaf next to Files is the same
// document as the Workspace pane that already scans, so it goes through the
// same docview seam and registers into the deck's own hit list: press/drag/
// release, click-vs-selection, generation invalidation, and openAppContent
// activation are all inherited rather than reimplemented.
//
// docview.ContentLinkRect excludes the gutter, the scrollbar column, and the
// header row, so a hit registered here cannot steal a tab, close, or scrollbar
// click. Coordinates are already absolute: SetOrigin above told the viewer where
// its body was drawn.
func (h *appContentDeck) prepareDocumentLeaf(view *docview.Model) {
	if view == nil {
		return
	}
	frame := view.PrepareFrame(docview.PrepareOptions{
		Root:              h.workdir,
		Resolution:        h.resolution.SnapshotForRoot(h.workdir),
		Matchers:          h.resourceMatchers,
		MatcherGeneration: h.matcherGeneration,
		AllowedKinds:      docview.ContentLinkKinds(),
		Decorate:          true,
		Links:             true,
	})
	docview.BeginResolutions(h.resolution, h.workdir, frame, h.queueContentLinkRequest)
}

func (h *appContentDeck) preparedDocumentLeaf(view *docview.Model, originX, originY int) string {
	if view == nil {
		return ""
	}
	frame := view.PreparedFrame()
	if !frame.Valid() {
		return view.View()
	}
	frame.EachHitAt(originX, originY, func(hit docview.ContentLinkHit) {
		h.links = append(h.links, appContentLinkHit{Generation: h.generation, Ref: hit.Ref, Rect: hit.Rect})
	})
	return frame.Output()
}

// queueContentLinkResolve schedules root- and token-scoped file/diff work for
// the primary plugin surface. Document frames enter through BeginResolutions
// and share queueContentLinkRequest below.
func (h *appContentDeck) queueContentLinkResolve(root string, candidate contentlink.Pending) {
	request, outcome := h.resolution.BeginClassified(root, candidate)
	switch outcome {
	case contentlink.BeginRequested:
		terminalperf.Record(terminalperf.DocumentResolutionRequest)
		h.queueContentLinkRequest(request)
	case contentlink.BeginReady:
		terminalperf.Record(terminalperf.DocumentResolutionCacheHit)
	}
}

func (h *appContentDeck) queueContentLinkRequest(request contentlink.ResolutionRequest) {
	h.pending[appContentResolutionKey{Root: request.Root, Candidate: request.Candidate}] = true
	src := contentpanes.SourceContext{Root: request.Root}
	var source contentpanes.Source = contentpanes.LocalSource{}
	if h.deck != nil {
		ctx := h.deck.Context()
		if ctx.Source.Root != "" {
			src = ctx.Source
		}
		source = h.deck.ContentSource()
	}
	h.queued = append(h.queued, resolveAppContentLink(h.key, source, src, request))
}

func resolveAppContentLink(key string, source contentpanes.Source, src contentpanes.SourceContext, request contentlink.ResolutionRequest) tea.Cmd {
	return func() tea.Msg {
		result := contentlink.ResolutionResult{Request: request}
		switch request.Candidate.Kind {
		case contentlink.KindFile:
			if src.Root == "" {
				src.Root = request.Root
			}
			ref, err := contentpanes.ResolveDocument(source, src, request.Candidate)
			result.Ref, result.Found = ref, err == nil && ref.Value != ""
		case contentlink.KindDiff:
			target, ok := workspacediff.ParseSpec(request.Candidate.Raw)
			if !ok {
				return appContentResolvedMsg{Key: key, Result: result}
			}
			resolved, err := workspacediff.ResolveSpec(context.Background(), request.Root, target)
			result.Ref, result.Found = contentlink.Ref{Kind: contentlink.KindDiff, Value: resolved.Identity()}, err == nil
		}
		return appContentResolvedMsg{Key: key, Result: result}
	}
}

func (h *appContentDeck) takeQueued() tea.Cmd {
	cmds := h.queued
	h.queued = nil
	return tea.Batch(cmds...)
}

func (m *Model) openAppContent(workdir, pluginID string, ref contentlink.Ref) tea.Cmd {
	h := m.activeContentDeck()
	if h == nil || h.workdir != workdir || h.pluginID != pluginID {
		return nil
	}
	if ref.Kind == contentlink.KindURL {
		return terminallink.OpenHTTP(ref.Value)
	}
	if ref.Kind == contentlink.KindInternal && h.pluginID == "notes" {
		cmd, err := sidecarIntents.activate(IntentAppContext{ProjectRoot: m.ui.ProjectRoot}, ref)
		if err != nil {
			return nil
		}
		return cmd
	}
	out := m.openAppContentOutcome(h, ref, "", nil)
	if out.Status == contentpanes.StatusRefused {
		if out.Refusal == contentpanes.RefusalFit {
			return appmsg.ShowToast("Content pane needs a wider window; layout left unchanged", 3*time.Second)
		}
		return nil
	}
	return out.Command
}

func (m *Model) openAppContentOutcome(h *appContentDeck, ref contentlink.Ref, split string, plan *panelayout.OpenPlan) contentpanes.Outcome {
	boxes := make(map[int]panelayout.Box)
	for _, leaf := range h.layout.Leaves {
		boxes[leaf.Node.ID] = leaf.Box
	}
	out := h.deck.Open(m.appDeckSurfaceContext(h.workdir, h.pluginID, m.registry.Context().Epoch), ref,
		contentpanes.Placement{Box: h.canvas, Boxes: boxes, Floors: appDeckFloors(), Split: split, Plan: plan})
	if out.Accepted() {
		h.syncInnerFocus()
		m.persistAppContentDeck(h)
	}
	return out
}

func (m *Model) handleAppContentUIRequest(req uirequest.Request) (tea.Cmd, bool) {
	if req.Action != uirequest.ActionOpen || req.Origin.TmuxSession != "" || !m.appContentRequestMatchesProject(req) {
		return nil, false
	}
	h := m.activeContentDeck()
	if h == nil {
		return nil, false
	}
	ref, refusal, ok := h.contentRefForTarget(req.Target)
	if refusal != "" {
		m.ackAppContentRequest(req, uirequest.StatusDeclined, refusal, 0)
		return nil, true
	}
	if !ok {
		return nil, false
	}
	var plan *panelayout.OpenPlan
	if at := strings.TrimSpace(req.Options.At); at != "" {
		// An explicit cell is a requirement on this surface too: plan it
		// against the deck's own tree and apply it verbatim, refusing rather
		// than landing anywhere else.
		cell, ok := panelayout.ParseCell(at)
		if !ok {
			m.ackAppContentRequest(req, uirequest.StatusDeclined, fmt.Sprintf("cell %q is not a grid address like 2.1", at), 0)
			return nil, true
		}
		kind, ok := appContentKindForTarget(req.Target.Kind)
		if !ok {
			m.ackAppContentRequest(req, uirequest.StatusDeclined, fmt.Sprintf("a %s target has no pane to place at a cell", string(req.Target.Kind)), 0)
			return nil, true
		}
		planned, refusal := panelayout.PlanOpenAt(h.deck.Tree(), kind, 0, cell)
		if refusal != "" {
			m.ackAppContentRequest(req, uirequest.StatusDeclined, refusal, 0)
			return nil, true
		}
		plan = &planned
	}
	out := m.openAppContentOutcome(h, ref, req.Options.Split, plan)
	if !out.Accepted() {
		reason := string(out.Refusal)
		if out.Refusal == contentpanes.RefusalFit || out.Refusal == contentpanes.RefusalPlacement {
			reason = "the window is too small to split"
		}
		m.ackAppContentRequest(req, uirequest.StatusDeclined, reason, 0)
		return nil, true
	}
	status := uirequest.StatusOpened
	if out.Status == contentpanes.StatusFocused || !out.CreatedLeaf {
		status = uirequest.StatusRetargeted
	}
	m.ackAppContentRequest(req, status, "", out.LeafID)
	return out.Command, true
}

// contentRefForTarget maps the cross-surface target vocabulary onto the ref
// this deck opens. It is the whole per-kind body of the open path, shared by
// `sidecar open` and the pane switcher so the two cannot drift: a kind the
// agent can put beside a plugin is a kind the human can, and both land on the
// same leaf with the same value.
//
// The three answers are distinct. A refusal is a target this surface
// understands and declines with a reason the caller reports; ok=false is a
// kind this surface does not carry at all, which leaves the request for
// another handler; otherwise the ref is ready for openAppContentOutcome.
func (h *appContentDeck) contentRefForTarget(target uirequest.Target) (ref contentlink.Ref, refusal string, ok bool) {
	switch target.Kind {
	case uirequest.TargetKindFile:
		// Resolved against the deck's own root, exactly as the link path does
		// (resolveAppContentLink) and as the Workspaces hosts do — the Document
		// leaf wants the workspace-relative display path, and re-resolving here
		// is what admits an absolute or ~-rooted path the CLI accepted.
		src := contentpanes.SourceContext{Root: h.workdir}
		source := contentpanes.Source(contentpanes.LocalSource{})
		if h.deck != nil {
			if h.deck.Context().Source.Root != "" {
				src = h.deck.Context().Source
			}
			source = h.deck.ContentSource()
		}
		ref, err := contentpanes.ResolveDocument(source, src, contentlink.Pending{Kind: contentlink.KindFile, Raw: target.Value})
		if err != nil || ref.Value == "" {
			return contentlink.Ref{}, fmt.Sprintf("file %q is not readable from %s", target.Value, h.workdir), false
		}
		ref.Line = target.Line
		return ref, "", true
	case uirequest.TargetKindIssue:
		return contentlink.Ref{Kind: contentlink.KindIssue, Value: target.Value}, "", true
	case uirequest.TargetKindNote:
		return contentlink.Ref{Kind: contentlink.KindInternal, Namespace: "note", Value: target.Value}, "", true
	case uirequest.TargetKindDiff:
		return contentlink.Ref{Kind: contentlink.KindDiff, Value: target.Value}, "", true
	case uirequest.TargetKindResource:
		resolved, refusal := resourceview.ReferenceForLocator(h.resourceMatchers, target.Provider, target.Value)
		if refusal != "" {
			return contentlink.Ref{}, refusal, false
		}
		return contentlink.Ref{Kind: contentlink.KindResource, Provider: resolved.Instance, Matcher: resolved.Matcher, Value: resolved.Locator}, "", true
	default:
		return contentlink.Ref{}, "", false
	}
}

// appContentKindForTarget maps an open request's wire kind onto its pane kind
// for explicit-cell placement. Only the passive content kinds are placeable.
func appContentKindForTarget(kind uirequest.TargetKind) (panelayout.Kind, bool) {
	switch kind {
	case uirequest.TargetKindFile:
		return panelayout.Document, true
	case uirequest.TargetKindIssue:
		return panelayout.Issue, true
	case uirequest.TargetKindNote:
		return panelayout.Note, true
	case uirequest.TargetKindDiff:
		return panelayout.Diff, true
	case uirequest.TargetKindResource:
		return panelayout.Resource, true
	default:
		return 0, false
	}
}

func (m *Model) appContentRequestMatchesProject(req uirequest.Request) bool {
	if m.ui == nil || req.Origin.ProjectKey == "" {
		return false
	}
	if dir, ok := projectdir.Lookup(m.ui.ProjectRoot); ok {
		return filepath.Base(dir) == req.Origin.ProjectKey
	}
	return sameCanonicalAppPath(m.ui.ProjectRoot, req.Origin.WorkDir)
}

func sameCanonicalAppPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	canonical := func(path string) string {
		path = filepath.Clean(path)
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = filepath.Clean(resolved)
		}
		return path
	}
	return canonical(a) == canonical(b)
}

func (m *Model) ackAppContentRequest(req uirequest.Request, status uirequest.Status, reason string, pane int) {
	surface := ""
	if h := m.currentContentDeck(); h != nil {
		surface = "plugin:" + h.pluginID
	} else if p := m.focusedSurface(); p != nil {
		surface = "plugin:" + p.ID()
	}
	_ = uirequest.WriteAck(config.StateDir(), req.ID, req.Action, uirequest.Ack{
		Instance: uirequest.InstanceID("app-content"), Host: uirequest.HostName(), PID: os.Getpid(),
		Status: status, Reason: reason, Surface: surface, Pane: pane, At: time.Now().UTC(),
	})
}

func (m *Model) adoptAppContentPlugin(h *appContentDeck) {
	if h == nil || h.plugin == nil || m.registry == nil {
		return
	}
	if h.global {
		if host := m.globalHostByID(h.pluginID); host != nil {
			host.plugin = h.plugin
		}
		return
	}
	plugins := m.registry.Plugins()
	for i, candidate := range plugins {
		if candidate.ID() == h.pluginID {
			m.registry.Replace(i, h.plugin)
			return
		}
	}
}

func (m *Model) persistAppContentDeck(h *appContentDeck) {
	if h == nil {
		return
	}
	raw, err := json.Marshal(h.deck.Encode())
	if err == nil {
		_ = state.SetContentDeck(h.stateRoot, h.pluginID, raw)
	}
}

func (m *Model) applyAppContentResult(result contentpanes.Result) tea.Cmd {
	for _, h := range m.contentDecks {
		if cmd, ok := h.deck.Apply(result); ok {
			m.persistAppContentDeck(h)
			return cmd
		}
	}
	return nil
}

func (m *Model) applyAppContentBroadcast(payload any) tea.Cmd {
	var cmds []tea.Cmd
	for _, h := range m.contentDecks {
		cmds = append(cmds, h.deck.ApplyBroadcast(payload))
	}
	return tea.Batch(cmds...)
}

func (m *Model) appContentMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	h := m.activeContentDeck()
	if h == nil || !h.laidOut {
		return nil, false
	}
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
		if region := h.mouse.HitMap.Test(click.X, click.Y); region != nil && region.ID == appDeckLayoutRegion {
			if leafID, ok := region.Data.(int); ok {
				return m.openAppPaneLayoutModal(h, leafID), true
			}
			return nil, true
		}
	}
	if h.info != nil {
		if h.info.HandleMouse(msg, h.mouse) {
			h.info, h.infoLeaf = nil, 0
		}
		return nil, true
	}
	if cmd, handled := m.handleAppContentSearchMouse(msg); handled {
		return cmd, true
	}
	mi := msg.Mouse()
	// A release can be lost when the pointer leaves the window or a modal takes
	// input mid-gesture. The shared handler cancels that stale drag on the next
	// button-less motion; capture what it held before this event so the hosted
	// pane's half of the gesture can be settled at that same boundary.
	wasDragging := h.mouse.IsDragging()
	dragSourceBefore := h.mouse.DragRegion()
	action := h.mouse.HandleMouse(msg)
	if action.Type == mouse.ActionHover {
		h.hoverLayout = 0
		if action.Region != nil && action.Region.ID == appDeckLayoutRegion {
			if leafID, ok := action.Region.Data.(int); ok {
				h.hoverLayout = leafID
			}
		}
	}
	if action.Type == mouse.ActionClick && action.Region != nil && action.Region.ID == appDeckLayoutRegion {
		if leafID, ok := action.Region.Data.(int); ok {
			return m.openAppPaneLayoutModal(h, leafID), true
		}
		return nil, true
	}
	if action.Type == mouse.ActionClick && action.Region != nil && action.Region.ID == appDeckDividerRegion {
		if splitID, ok := action.Region.Data.(int); ok {
			if split := panelayout.Find(h.deck.Tree(), splitID); split != nil && split.Split != nil {
				h.dragSplit = splitID
				h.mouse.StartDrag(mi.X, mi.Y, appDeckDividerRegion, split.Split.Ratio)
				return nil, true
			}
		}
	}
	if action.Type == mouse.ActionDrag && action.DragStartID == appDeckDividerRegion {
		split := panelayout.Find(h.deck.Tree(), h.dragSplit)
		if split != nil && split.Split != nil {
			ratio := h.mouse.DragStartValue()
			if split.Split.Axis == panelayout.Rows && h.canvas.H > 0 {
				ratio += action.DragDY * 100 / h.canvas.H
			} else if split.Split.Axis == panelayout.Columns && h.canvas.W > 0 {
				ratio += action.DragDX * 100 / h.canvas.W
			}
			h.deck.SetRatio(h.dragSplit, ratio)
		}
		return nil, true
	}
	if action.Type == mouse.ActionDragEnd && action.DragStartID == appDeckDividerRegion {
		h.dragSplit = 0
		m.persistAppContentDeck(h)
		return nil, true
	}
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mi.Button == tea.MouseLeft {
			h.press, h.dragged, h.pressX, h.pressY = nil, false, mi.X, mi.Y
			for i := range h.links {
				hit := &h.links[i]
				if hit.Generation == h.generation && hit.Rect.Contains(mi.X, mi.Y) {
					h.press = hit
					break
				}
			}
		}
	case tea.MouseMotionMsg:
		if h.press != nil && (mi.X != h.pressX || mi.Y != h.pressY) {
			h.dragged = true
		}
		// Hover is read here rather than in handlePassiveMouse: motion over a
		// secondary leaf's tabs is routed to the primary plugin while primary
		// holds focus, and the × still has to light under the pointer.
		h.setTabCloseHover(mi.X, mi.Y)
	case tea.MouseReleaseMsg:
		if h.press != nil {
			hit, activate := *h.press, !h.dragged && h.press.Rect.Contains(mi.X, mi.Y)
			h.press = nil
			if activate {
				// A document link and its text occupy the same cells. The body
				// selector was armed on press so dragging can select the label;
				// settle that no-drag gesture before activating the link or it
				// would answer the next unrelated motion as a stale drag.
				var settle tea.Cmd
				focused := panelayout.Find(h.deck.Tree(), h.deck.FocusedLeaf())
				if focused != nil && focused.Kind == panelayout.Primary {
					// Primary rendered surfaces, including Files, also see the
					// press before the host recognizes their link. They must see
					// the matching release before activation for the same reason.
					settle = m.updateAppContentPrimaryMouse(h, msg)
				} else if action.DragStartID == appDeckSelectGestureRegion {
					settle, _ = h.continueAppContentGesture(action)
				}
				return tea.Batch(settle, m.openAppContent(h.workdir, h.pluginID, hit.Ref)), true
			}
		}
	}
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
		if paneframe.FocusLeafAt(appDeckHost{h}, mi.X, mi.Y) {
			h.syncInnerFocus()
		}
	}
	if cmd, claimed := h.routeAppContentGesture(action, wasDragging, dragSourceBefore); claimed {
		m.persistAppContentDeck(h)
		return cmd, true
	}
	leaf := panelayout.Find(h.deck.Tree(), h.deck.FocusedLeaf())
	if leaf == nil || leaf.Kind == panelayout.Primary {
		return m.updateAppContentPrimaryMouse(h, msg), true
	}
	cmd := h.handlePassiveMouse(msg, leaf)
	m.persistAppContentDeck(h)
	return cmd, true
}

func (m *Model) updateAppContentPrimaryMouse(h *appContentDeck, msg tea.MouseMsg) tea.Cmd {
	adjusted := offsetMouse(msg, -h.primaryInner.X, -h.primaryInner.Y)
	held := h.primaryHasSelection()
	newPlugin, cmd := h.plugin.Update(adjusted)
	h.plugin = newPlugin
	m.adoptAppContentPlugin(h)
	// The other half of one selection at a time: a plugin that owns a
	// selectable box inside its own frame answers its own gesture, so the deck
	// learns a selection started there only by noticing that the plugin now
	// holds one. Asked here rather than on every frame because this is the
	// event that could have started it.
	if !held && h.primaryHasSelection() {
		h.clearAppContentSelectionsExcept(nil)
	}
	return cmd
}

func offsetMouse(msg tea.MouseMsg, dx, dy int) tea.MouseMsg {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		msg.X, msg.Y = msg.X+dx, msg.Y+dy
		return msg
	case tea.MouseReleaseMsg:
		msg.X, msg.Y = msg.X+dx, msg.Y+dy
		return msg
	case tea.MouseMotionMsg:
		msg.X, msg.Y = msg.X+dx, msg.Y+dy
		return msg
	case tea.MouseWheelMsg:
		msg.X, msg.Y = msg.X+dx, msg.Y+dy
		return msg
	default:
		return msg
	}
}

func (h *appContentDeck) handlePassiveMouse(msg tea.MouseMsg, leaf *panelayout.Node) tea.Cmd {
	mi := msg.Mouse()
	if _, ok := msg.(tea.MouseClickMsg); ok && mi.Button == tea.MouseLeft {
		region := h.mouse.HitMap.Test(mi.X, mi.Y)
		if region != nil {
			switch region.ID {
			case appDeckCloseRegion:
				h.deck.FocusLeaf(leaf.ID)
				h.deck.CloseActive()
				h.syncInnerFocus()
			case appDeckTabRegion:
				if hit, ok := region.Data.(appDeckTabHit); ok {
					h.deck.FocusLeaf(hit.leafID)
					if hit.close {
						h.deck.CloseTab(hit.leafID, hit.index)
						h.syncInnerFocus()
						return nil
					}
					return h.deck.SelectTab(hit.leafID, hit.index)
				}
			}
		}
	}
	if wheel, ok := msg.(tea.MouseWheelMsg); ok {
		delta := 1
		if wheel.Button == tea.MouseWheelUp {
			delta = -1
		}
		switch v := h.deck.Viewer(leaf.ID).(type) {
		case *docview.Model:
			v.Scroll(delta)
		case *issueview.Model:
			h.scrollIssueByWheel(leaf.ID, v, delta)
		case *noteview.Model:
			v.Scroll(delta)
		case *workspacediff.View:
			v.ScrollContent(delta, v.Height())
		case *resourceview.Model:
			v.ScrollBy(delta)
		}
	}
	return nil
}

// issueWheelSurfaceKey names the flick over one deck leaf's issue card.
func issueWheelSurfaceKey(leafID int) string { return fmt.Sprintf("issue-%d", leafID) }

// scrollIssueByWheel applies one notch to a deck issue card through the shared
// burst guard. A leaf is one scroll surface, so it is named by its leaf ID and
// its held delta dies with the deck rather than crossing to its neighbours.
func (h *appContentDeck) scrollIssueByWheel(leafID int, view *issueview.Model, delta int) {
	flushed, ok := h.wheel.For(issueWheelSurfaceKey(leafID)).Add(delta, h.now())
	if !ok {
		return
	}
	view.Scroll(flushed)
}

func (h *appContentDeck) now() time.Time {
	if h.wheelNow != nil {
		return h.wheelNow()
	}
	return time.Now()
}

// appContentWheelAtBoundary mirrors appContentMouse's pointer ownership before
// Update runs. The pre-update filter must ask the leaf under the pointer, not
// the primary plugin hidden beside it, or a short Files preview can swallow a
// valid wheel intended for a long passive document.
func (m Model) appContentWheelAtBoundary(wheel tea.MouseWheelMsg) (boundary, owned bool) {
	h := m.currentContentDeck()
	if h == nil || !h.laidOut {
		return false, false
	}
	if h.info != nil {
		return h.info.WheelAtBoundary(wheel, h.mouse), true
	}
	delta := 0
	switch wheel.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return false, false
	}
	leaf := paneframe.LeafAtForHost(appDeckHost{h}, h.layout, wheel.X, wheel.Y)
	if leaf == nil {
		return false, false
	}
	if leaf.Kind == panelayout.Primary {
		consumer, ok := h.plugin.(plugin.WheelBoundaryConsumer)
		if !ok {
			return false, true
		}
		adjusted, ok := offsetMouse(wheel, -h.primaryInner.X, -h.primaryInner.Y).(tea.MouseWheelMsg)
		if !ok {
			return false, true
		}
		return consumer.WheelAtBoundary(adjusted), true
	}
	switch v := h.deck.Viewer(leaf.ID).(type) {
	case *docview.Model:
		return v.ScrollAtBoundary(delta), true
	case *issueview.Model:
		bounded := v.ScrollAtBoundary(delta)
		if bounded {
			// A held delta must not leak into the next gesture after the
			// filter starts dropping the inertia tail at this boundary.
			h.wheel.For(issueWheelSurfaceKey(leaf.ID)).Reset()
		}
		return bounded, true
	case *noteview.Model:
		return v.ScrollAtBoundary(delta), true
	case *workspacediff.View:
		return v.ScrollAtBoundary(delta, v.Height()), true
	case *resourceview.Model:
		return v.ScrollAtBoundary(delta), true
	default:
		return false, true
	}
}

func (m *Model) handleAppContentKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	h := m.activeContentDeck()
	if h == nil {
		return nil, false
	}
	if h.deck.Leaf(panelayout.Document)+h.deck.Leaf(panelayout.Issue)+h.deck.Leaf(panelayout.Note)+h.deck.Leaf(panelayout.Diff)+h.deck.Leaf(panelayout.Resource) == 0 {
		// A deck showing only its Primary leaf still has a ring whenever the
		// plugin projects one, and Tab is how the app moves round every other
		// ring it owns. Without this, a surface with two windows of its own —
		// the plugin browser's list and the document box beside it — could be
		// reached only by binding Tab itself, which is the app's key everywhere
		// else. Nothing but Tab is answered here: the plugin owns its keys.
		return m.cyclePrimaryPaneFocus(h, key)
	}
	leaf := panelayout.Find(h.deck.Tree(), h.deck.FocusedLeaf())
	if leaf == nil {
		return nil, false
	}
	if leaf.Kind == panelayout.Primary {
		provider, ok := h.plugin.(plugin.PaneFocusProvider)
		if !ok || len(provider.PaneFocusStops()) == 0 {
			// A primary sub-mode with no projected stops owns its keys. Git's
			// full-screen diff is the important case: Tab returns to its sidebar
			// and must not enter a passive outer leaf left open beside it.
			return nil, false
		}
	}
	if h.info != nil {
		if key.String() == "ctrl+c" {
			return nil, false
		}
		closed, cmd := h.info.HandleKey(key)
		if closed {
			h.info, h.infoLeaf = nil, 0
		}
		return cmd, true
	}
	if h.appContentSearchActive() {
		if key.String() == "ctrl+c" {
			return nil, false
		}
		return m.handleAppContentSearchKey(key), true
	}
	if key.Code == tea.KeyTab {
		cmd := h.cycleCombinedFocus(key.Mod.Contains(tea.ModShift))
		m.persistAppContentDeck(h)
		return cmd, true
	}
	if leaf.Kind == panelayout.Primary {
		return nil, false
	}
	if view, ok := h.deck.Viewer(leaf.ID).(*docview.Model); ok && view.SearchActive() {
		_, cmd := view.HandleSearchKey(key)
		return cmd, true
	}
	// Selection chords belong to the pane before pane-level escape and ordinary
	// viewer keys. In particular, escape clears a selection instead of hiding
	// the pane, matching Files and both Workspace hosts. Every selectable pane
	// answers here, so a copy is a copy whichever kind of leaf has the
	// keyboard.
	if pane, ok := h.deck.Viewer(leaf.ID).(textselect.Pane); ok {
		result := pane.HandleSelectionKey(key)
		if result.Handled {
			return appDeckSelectionCopyCmd(pane, result), true
		}
	}
	// M is the deck's own entry onto the reposition modal, beside the header ⊞.
	// It sits with the structural keys because moving this leaf is structural,
	// and it declines rather than consuming the key when the deck cannot answer.
	if cmd, handled := m.appPaneMoveKey(h, leaf, key); handled {
		return cmd, true
	}
	switch key.String() {
	case "q", "esc":
		h.deck.HideFocused()
		h.syncInnerFocus()
		m.persistAppContentDeck(h)
		return nil, true
	case "x":
		h.deck.CloseActive()
		h.syncInnerFocus()
		m.persistAppContentDeck(h)
		return nil, true
	case "{":
		cmd := h.deck.CycleTab(-1)
		m.persistAppContentDeck(h)
		return cmd, true
	case "}":
		cmd := h.deck.CycleTab(1)
		m.persistAppContentDeck(h)
		return cmd, true
	}
	// Sidecar's own globals outrank a passive leaf. A focused document, note,
	// or diff must not swallow the keys that switch plugins, open the palette,
	// or reach the switchers — those belong to the host's switch, which runs
	// later in the key ladder. Returning false hands them back; everything the
	// deck structurally owns (tab, q/esc, x, tab cycling) was answered above,
	// and an active in-document search consumed its keys even earlier.
	//
	// The pane switcher's entry is one of them for the same reason, and for one
	// more: opening a pane beside the pane you are reading is the whole point of
	// the entry, so the focused leaf that would otherwise absorb the key is
	// exactly where it has to work. Here that key is the `n` this leaf's context
	// binds on all three surfaces, which is why the release is asked of the
	// keymap rather than spelled out. It is not in keymap.GlobalKeys because the
	// host answers it only where a deck can take the result, and a plugin may
	// hold it anywhere else.
	if keymap.GlobalKeys[key.String()] || m.paneSwitcherClaimsKey(key.String()) {
		return nil, false
	}
	switch v := h.deck.Viewer(leaf.ID).(type) {
	case *docview.Model:
		switch key.String() {
		case "/":
			v.StartSearch()
			return nil, true
		case "e":
			return m.enterAppContentDocumentEdit(), true
		case "E":
			return docview.EditExternal(v.Root(), v.Title(), v.ScrollOffset()+1), true
		case "r":
			return h.deck.ReloadFocused(), true
		case "ctrl+p":
			return h.openAppContentFinder(), true
		case "f":
			return h.openAppContentProjectSearch(), true
		case "m":
			v.ToggleRenderMode()
			return nil, true
		case "w":
			v.ToggleWrap()
			return nil, true
		case "ctrl+r":
			return docview.Reveal(v.Root(), v.Title()), true
		case "I":
			return h.openAppContentInfo(v, leaf.ID), true
		case "Y", "shift+y":
			return docview.YankPath(v.Title()), true
		case "y":
			return v.YankSelectionOrContents(), true
		case "+":
			m.resizeAppContentFocused(h, 5)
			return nil, true
		case "-":
			m.resizeAppContentFocused(h, -5)
			return nil, true
		}
		v.HandleKey(key)
		return nil, true
	case *issueview.Model:
		_, cmd := v.HandleKey(key)
		return cmd, true
	case *noteview.Model:
		switch key.String() {
		case "y":
			if data := v.Data(); data != nil {
				return noteview.CopyMarkdown(data), true
			}
		case "Y", "shift+y":
			if data := v.Data(); data != nil {
				return noteview.CopyID(data), true
			}
		}
		_, cmd := v.HandleKey(key)
		return cmd, true
	case *workspacediff.View:
		cmd, _ := v.HandleKey(key)
		return cmd, true
	case *resourceview.Model:
		switch key.String() {
		case "r":
			return v.Refresh(), true
		case "o":
			if safe, ok := contentlink.SafeHTTPURL(v.SourceURL()); ok {
				return openPathCmd(safe), true
			}
			return nil, true
		case "j", "down":
			v.ScrollBy(1)
			return nil, true
		case "k", "up":
			v.ScrollBy(-1)
			return nil, true
		case "pgdown":
			v.ScrollBy(max(v.Height()-1, 1))
			return nil, true
		case "pgup":
			v.ScrollBy(-max(v.Height()-1, 1))
			return nil, true
		}
	}
	return nil, true
}

type appDeckFocusStop struct {
	inner string
	leaf  int
}

func (h *appContentDeck) focusRing() ([]appDeckFocusStop, plugin.PaneFocusProvider) {
	provider, ok := h.plugin.(plugin.PaneFocusProvider)
	if !ok {
		return nil, nil
	}
	var ring []appDeckFocusStop
	primary := h.deck.Leaf(panelayout.Primary)
	for _, s := range provider.PaneFocusStops() {
		ring = append(ring, appDeckFocusStop{inner: s.ID, leaf: primary})
	}
	for _, placement := range h.layout.Leaves {
		if placement.Node.Kind != panelayout.Primary {
			ring = append(ring, appDeckFocusStop{leaf: placement.Node.ID})
		}
	}
	return ring, provider
}

// cyclePrimaryPaneFocus answers Tab for a deck holding nothing but its Primary
// leaf, by cycling the focused plugin's own projected stops.
//
// Only for a plugin that has handed the key over, through
// plugin.PaneFocusRingHost. Every other surface routes Tab itself — Files and
// Git move to their content pane, Notes saves the editor on the way out — and
// taking it here on the strength of two projected stops would quietly replace
// behaviour those plugins still own. Once a second leaf is open the deck's ring
// is larger than any one plugin's and the key is the app's outright, which is
// the rung above this one.
//
// A provider with fewer than two stops declines as well: one stop is not a
// ring, and swallowing Tab there would give the user nowhere to go.
func (m *Model) cyclePrimaryPaneFocus(h *appContentDeck, key tea.KeyPressMsg) (tea.Cmd, bool) {
	if key.Code != tea.KeyTab {
		return nil, false
	}
	if h.info != nil || h.appContentSearchActive() {
		return nil, false
	}
	provider, ok := h.plugin.(plugin.PaneFocusRingHost)
	if !ok || !provider.HostOwnsPaneFocusRing() {
		return nil, false
	}
	if len(provider.PaneFocusStops()) < 2 {
		return nil, false
	}
	if consumer, ok := h.plugin.(plugin.TextInputConsumer); ok && consumer.ConsumesTextInput() {
		// A surface taking text has the key: Tab there is the field's.
		return nil, false
	}
	cmd := h.cycleCombinedFocus(key.Mod.Contains(tea.ModShift))
	m.persistAppContentDeck(h)
	return cmd, true
}

func (h *appContentDeck) cycleCombinedFocus(reverse bool) tea.Cmd {
	ring, provider := h.focusRing()
	if len(ring) == 0 {
		return nil
	}
	current := 0
	for i, s := range ring {
		if s.leaf == h.deck.FocusedLeaf() && (s.leaf != h.deck.Leaf(panelayout.Primary) || s.inner == provider.PaneFocus()) {
			current = i
			break
		}
	}
	delta := 1
	if reverse {
		delta = -1
	}
	next := (current + delta + len(ring)) % len(ring)
	h.deck.FocusLeaf(ring[next].leaf)
	if ring[next].inner != "" {
		_ = provider.SetPaneFocus(ring[next].inner)
	}
	h.syncInnerFocus()
	return nil
}

// resizeAppContentFocused grows or shrinks the focused passive leaf against
// its nearest enclosing split. The sign follows the leaf rather than the
// split's A/B ordering, matching the Workspace document pane's +/- behavior.
func (m *Model) resizeAppContentFocused(h *appContentDeck, delta int) {
	if h == nil || h.deck == nil || delta == 0 {
		return
	}
	root := h.deck.Tree()
	parent, inA := enclosingAppContentSplit(root, h.deck.FocusedLeaf())
	if parent == nil || parent.Split == nil {
		return
	}
	ratio := parent.Split.Ratio
	if inA {
		ratio += delta
	} else {
		ratio -= delta
	}
	if h.deck.SetRatio(parent.ID, ratio) {
		m.persistAppContentDeck(h)
	}
}

func enclosingAppContentSplit(node *panelayout.Node, leafID int) (*panelayout.Node, bool) {
	if node == nil || node.Split == nil {
		return nil, false
	}
	if panelayout.Find(node.Split.A, leafID) != nil {
		if node.Split.A.ID == leafID {
			return node, true
		}
		if parent, inA := enclosingAppContentSplit(node.Split.A, leafID); parent != nil {
			return parent, inA
		}
	}
	if panelayout.Find(node.Split.B, leafID) != nil {
		if node.Split.B.ID == leafID {
			return node, false
		}
		return enclosingAppContentSplit(node.Split.B, leafID)
	}
	return nil, false
}

type appDeckFocusCycler struct{ h *appContentDeck }

func (c appDeckFocusCycler) AtFocusCycleEnd(reverse bool) bool {
	ring, provider := c.h.focusRing()
	if len(ring) == 0 || provider == nil {
		return false
	}
	current := -1
	for i, s := range ring {
		if s.leaf == c.h.deck.FocusedLeaf() && (s.leaf != c.h.deck.Leaf(panelayout.Primary) || s.inner == provider.PaneFocus()) {
			current = i
			break
		}
	}
	if reverse {
		return current == 0
	}
	return current == len(ring)-1
}

func (c appDeckFocusCycler) FocusCycleStart(reverse bool) tea.Cmd {
	ring, provider := c.h.focusRing()
	if len(ring) == 0 || provider == nil {
		return nil
	}
	index := 0
	if reverse {
		index = len(ring) - 1
	}
	stop := ring[index]
	c.h.deck.FocusLeaf(stop.leaf)
	if stop.inner != "" {
		_ = provider.SetPaneFocus(stop.inner)
	}
	c.h.syncInnerFocus()
	return nil
}

func (m Model) appContentContext() (string, bool) {
	h := m.currentContentDeck()
	if h == nil {
		return "", false
	}
	leaf := panelayout.Find(h.deck.Tree(), h.deck.FocusedLeaf())
	if leaf == nil || leaf.Kind == panelayout.Primary {
		return "", false
	}
	switch leaf.Kind {
	case panelayout.Document:
		if h.appContentDocumentEditing() {
			return "workspace-doc-edit", true
		}
		if view, ok := h.deck.Viewer(leaf.ID).(*docview.Model); ok && view.SearchActive() {
			return "workspace-doc-find", true
		}
		if h.appContentSearchActive() {
			return "workspace-doc-search", true
		}
		return "workspace-doc", true
	case panelayout.Issue:
		return "workspace-issue", true
	case panelayout.Note:
		return "workspace-note", true
	case panelayout.Diff:
		return "workspace-diff", true
	case panelayout.Resource:
		return "workspace-resource", true
	default:
		return "", false
	}
}

func (m *Model) appContentCommands() []plugin.Command {
	ctx, ok := m.appContentContext()
	if !ok {
		return nil
	}
	command := func(id, name, description string, priority int) plugin.Command {
		return plugin.Command{ID: id, Name: name, Description: description, Context: ctx, Priority: priority,
			Handler: func() tea.Cmd { return m.runAppContentCommand(id) }}
	}
	if ctx == "workspace-doc-find" {
		cmds := docview.SearchCommands(ctx)
		for i := range cmds {
			id := cmds[i].ID
			cmds[i].Handler = func() tea.Cmd { return m.runAppContentCommand(id) }
		}
		return cmds
	}
	if ctx == "workspace-doc-search" {
		return []plugin.Command{
			command("search-open", "Open", "Open the selected file in this pane", 1),
			command("search-open-tab", "Tab+", "Open the selected file in a new tab", 2),
			command("search-cancel", "Cancel", "Close the search and return to the document", 3),
		}
	}
	if ctx == "workspace-doc-edit" {
		return nil
	}
	cmds := []plugin.Command{
		command("close", "Close", "Hide content pane", 1),
		command("close-tab", "Tab×", "Close active content tab", 2),
		command("prev-tab", "Tab←", "Previous content tab", 3),
		command("next-tab", "Tab→", "Next content tab", 4),
		command("next-pane", "Focus", "Focus next pane", 5),
		command("prev-pane", "Back", "Focus previous pane", 6),
	}
	h := m.currentContentDeck()
	leaf := panelayout.Find(h.deck.Tree(), h.deck.FocusedLeaf())
	if leaf == nil {
		return cmds
	}
	if m.appPaneMoveShortcutLeaf(h, leaf) != 0 {
		cmds = append(cmds, plugin.Command{
			ID: panereposition.CommandMove, Name: "Move", Description: "Reposition this pane",
			Context: ctx, Priority: 90,
			Handler: func() tea.Cmd { return m.runAppContentCommand(panereposition.CommandMove) },
		})
	}
	switch v := h.deck.Viewer(leaf.ID).(type) {
	case *docview.Model:
		cmds = append(cmds,
			command("search-content", "InFile", "Search this file's contents", 7),
			command("edit", "Edit", "Edit this file inline", 8),
			command("edit-external", "Editor", "Open this file in the external editor", 9),
			command("reload", "Reload", "Reload this file from disk", 10),
			command("find-file", "Find", "Find a file by name in this pane", 11),
			command("search-project", "Search", "Search the project in this pane", 12),
			command("toggle-wrap", "Wrap", "Toggle line wrapping", 14),
			command("reveal", "Reveal", "Reveal in file manager", 15),
			command("info", "Info", "Show file info", 16),
			command("yank-path", "Path", "Copy the relative path", 17),
			command("yank-contents", "Yank", "Copy file contents", 18),
			command("resize-pane-grow", "Grow", "Grow document pane", 19),
			command("resize-pane-shrink", "Shrink", "Shrink document pane", 20),
		)
		if terminallink.Markdown(v.Title()) {
			name := "Raw"
			if !v.Rendered() {
				name = "Render"
			}
			cmds = append(cmds, command("render", name, "Toggle rendered and raw markdown", 13))
		}
	case *issueview.Model:
		cmds = append(cmds,
			command("open-item", "Open", "Open selected parent or subtask", 7),
			command("open-in-td", "TD", "Open selected issue in td", 8),
			command("yank-issue", "Yank", "Copy issue as markdown", 9),
			command("yank-issue-key", "YankID", "Copy issue ID", 10),
		)
	case *noteview.Model:
		cmds = append(cmds,
			command("yank-note", "Yank", "Copy note as markdown", 7),
			command("yank-note-key", "YankID", "Copy note ID", 8),
		)
	case *workspacediff.View:
		for _, viewerCommand := range v.Commands(ctx) {
			id := viewerCommand.ID
			viewerCommand.Handler = func() tea.Cmd { return m.runAppContentCommand(id) }
			cmds = append(cmds, viewerCommand)
		}
	case *resourceview.Model:
		for i, viewerCommand := range resourceview.Commands() {
			if viewerCommand.ID == resourceview.CommandCloseTab || viewerCommand.ID == resourceview.CommandPrevTab || viewerCommand.ID == resourceview.CommandNextTab {
				continue
			}
			cmds = append(cmds, command(viewerCommand.ID, viewerCommand.Name, viewerCommand.Name+" resource", 7+i))
		}
	}
	return cmds
}

func (m *Model) runAppContentCommand(id string) tea.Cmd {
	h := m.currentContentDeck()
	if h == nil {
		return nil
	}
	switch id {
	case "close":
		h.deck.HideFocused()
		h.syncInnerFocus()
		m.persistAppContentDeck(h)
		m.updateContext()
	case "close-tab":
		h.deck.CloseActive()
		h.syncInnerFocus()
		m.persistAppContentDeck(h)
		m.updateContext()
	case "prev-tab":
		cmd := h.deck.CycleTab(-1)
		m.persistAppContentDeck(h)
		m.updateContext()
		return cmd
	case "next-tab":
		cmd := h.deck.CycleTab(1)
		m.persistAppContentDeck(h)
		m.updateContext()
		return cmd
	case "next-pane":
		cmd := h.cycleCombinedFocus(false)
		m.persistAppContentDeck(h)
		m.updateContext()
		return cmd
	case "prev-pane":
		cmd := h.cycleCombinedFocus(true)
		m.persistAppContentDeck(h)
		m.updateContext()
		return cmd
	case panereposition.CommandMove:
		leaf := panelayout.Find(h.deck.Tree(), h.deck.FocusedLeaf())
		leafID := m.appPaneMoveShortcutLeaf(h, leaf)
		if leafID == 0 {
			return nil
		}
		return m.openAppPaneLayoutModal(h, leafID)
	}
	leaf := panelayout.Find(h.deck.Tree(), h.deck.FocusedLeaf())
	if leaf == nil {
		return nil
	}
	switch view := h.deck.Viewer(leaf.ID).(type) {
	case *docview.Model:
		switch id {
		case "search-content":
			view.StartSearch()
		case "edit":
			return m.enterAppContentDocumentEdit()
		case "edit-external":
			return docview.EditExternal(view.Root(), view.Title(), view.ScrollOffset()+1)
		case "reload":
			return h.deck.ReloadFocused()
		case "find-file":
			return h.openAppContentFinder()
		case "search-project":
			return h.openAppContentProjectSearch()
		case "render":
			view.ToggleRenderMode()
		case "toggle-wrap":
			view.ToggleWrap()
		case "reveal":
			return docview.Reveal(view.Root(), view.Title())
		case "info":
			return h.openAppContentInfo(view, leaf.ID)
		case "yank-path":
			return docview.YankPath(view.Title())
		case "yank-contents":
			return view.YankSelectionOrContents()
		case "resize-pane-grow":
			m.resizeAppContentFocused(h, 5)
		case "resize-pane-shrink":
			m.resizeAppContentFocused(h, -5)
		case "search-open":
			return m.handleAppContentSearchKey(appContentKeyPress("enter"))
		case "search-open-tab":
			return m.handleAppContentSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
		case "search-cancel":
			return m.handleAppContentSearchKey(appContentKeyPress("esc"))
		case "confirm":
			_, cmd := view.HandleSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			return cmd
		case "next-match":
			_, cmd := view.HandleSearchKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
			return cmd
		case "prev-match":
			_, cmd := view.HandleSearchKey(tea.KeyPressMsg{Code: 'N', Text: "N"})
			return cmd
		case "cancel":
			_, cmd := view.HandleSearchKey(tea.KeyPressMsg{Code: tea.KeyEscape})
			return cmd
		}
	case *issueview.Model:
		key := map[string]string{"open-item": "enter", "open-in-td": "O", "yank-issue": "y", "yank-issue-key": "Y"}[id]
		if key != "" {
			_, cmd := view.HandleKey(appContentKeyPress(key))
			return cmd
		}
	case *noteview.Model:
		switch id {
		case "yank-note":
			if data := view.Data(); data != nil {
				return noteview.CopyMarkdown(data)
			}
		case "yank-note-key":
			if data := view.Data(); data != nil {
				return noteview.CopyID(data)
			}
		}
	case *workspacediff.View:
		key := map[string]string{
			"diff-open": "enter", "diff-down": "j", "diff-up": "k", "diff-back": "h",
			"toggle-diff-view": "v", "toggle-diff-scope": "z", "next-file": ".", "prev-file": ",",
			"file-picker": "f", "diff-next-change": ">", "diff-top": "g", "diff-bottom": "G",
			"diff-page-down": "pgdown", "diff-page-up": "pgup", "diff-scroll-down": "j", "diff-scroll-up": "k",
		}[id]
		if key != "" {
			cmd, _ := view.HandleKey(appContentKeyPress(key))
			return cmd
		}
	case *resourceview.Model:
		switch id {
		case resourceview.CommandRefresh:
			return view.Refresh()
		case resourceview.CommandOpenSource:
			if safe, ok := contentlink.SafeHTTPURL(view.SourceURL()); ok {
				return openPathCmd(safe)
			}
		}
	}
	m.persistAppContentDeck(h)
	m.updateContext()
	return nil
}

func appContentKeyPress(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	default:
		runes := []rune(key)
		if len(runes) == 1 {
			return tea.KeyPressMsg{Code: runes[0], Text: key}
		}
		return tea.KeyPressMsg{}
	}
}

type appContentCommandPlugin struct {
	plugin.Plugin
	commands []plugin.Command
}

func (p appContentCommandPlugin) Commands() []plugin.Command { return p.commands }

func (h *appContentDeck) SetResourceMatchers(matchers []contentlink.ResourceMatcher) {
	h.resourceMatchers = append([]contentlink.ResourceMatcher(nil), matchers...)
	h.matcherGeneration++
}

// SetResourceResolver rebinds this deck's Resource viewers and returns the load
// that gives a tab armed before provider readiness its first chance to resolve.
// Going through the deck rather than ConfigureViewers alone is deliberate: the
// deck also holds the factory future Resource tabs are built from.
func (h *appContentDeck) SetResourceResolver(resolve resourceview.Resolver) tea.Cmd {
	h.deck.SetResourceResolver(resolve)
	return tea.Batch(h.deck.LoadVisibleKind(panelayout.Resource)...)
}
