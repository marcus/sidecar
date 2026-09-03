package overview

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/issueview"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/tty"
)

const (
	previewIssueRegionKind = "global-preview-issue"
	previewIssueTabKind    = "global-preview-issue-tab"
	// previewIssueScrollbarKind names a drag that began on the issue card's
	// scrollbar. The card arms the gesture in HandleClick (which also does any
	// track-click jump); this ID is what turns the host's StartDrag into
	// motions routed to ScrollbarDrag and a release anywhere that settles it.
	previewIssueScrollbarKind = "global-preview-issue-scrollbar"
)

func isPreviewIssueRegion(kind string) bool {
	return kind == previewIssueRegionKind || kind == previewIssueTabKind
}

// previewIssueTabHit is the tab stored on the issue header region.
type previewIssueTabHit struct {
	Index int
	Close bool
}

// OpenIssueInTDMsg asks the app to leave global and open this issue in td.
// The jump itself belongs to the app — this surface only names the issue.
type OpenIssueInTDMsg struct {
	IssueID string
}

// previewIssue is the memory-only issue pane beside the selected terminal.
// The shared issue group lives here; this wrapper still owns root, workspace
// surface, focus, epoch, and model-ID allocation. paneCache[workspaceID] is
// the lifetime: switching rows restores this value, a restart does not.
type previewIssue struct {
	tabs    issueview.Tabs
	root    string
	surface string
	focused bool
	epoch   uint64
	// scrollTrackY is the absolute Y the card's row 0 sat at when a scrollbar
	// gesture's button went down, so motion maps onto view-local rows without
	// re-deriving the preview box mid-gesture.
	scrollTrackY int
	// wheel coalesces one flick over this pane, exactly as the terminal's own
	// burst does for its surface; the pane dying drops any held delta with it.
	wheel tty.WheelBurst
	// hostNotice is a connected-stale or verb-failure label for a remote
	// issue that is still showing its last good body.
	hostNotice string
}

func (i *previewIssue) view() *issueview.Model {
	if i == nil {
		return nil
	}
	return i.tabs.ActiveView()
}

// previewIssueLoadedMsg adds workspace identity to issueview's own request
// identity. Routing first resolves the workspace cache entry, then the tab
// whose model ID issued the fetch.
type previewIssueLoadedMsg struct {
	issueview.LoadedMsg
	WorkspaceID string
}

// issueFallbackRefs supplies the app-level configured projects to this
// surface's issue cards' cross-project search at click time.
func (m *Model) issueFallbackRefs() []issueview.ProjectRef {
	return issueview.ProjectRefsFromConfig(m.config)
}

func (m *Model) openPreviewIssue(issueID string) tea.Cmd {
	cmd, _ := m.openPreviewIssueResult(issueID)
	return cmd
}

func (m *Model) openPreviewIssueResult(issueID string) (tea.Cmd, error) {
	workspace, ok := m.SelectedWorkspace()
	issueID = issueview.NormalizeID(issueID)
	if !ok || issueID == "" || workspace.Path == "" {
		return nil, nil
	}
	if workspace.Remote() {
		ctx, ok := m.previewDeckContext()
		if !ok {
			return nil, nil
		}
		ref, err := contentpanes.ResolveDocument(m.previewDeckConfig(ctx).Source, ctx.Source, contentlink.Pending{
			Kind: contentlink.KindIssue, Raw: issueID,
		})
		if err != nil || ref.Value == "" {
			if err == nil {
				err = errors.New("issue not found on " + ctx.Source.HostID)
			}
			return remoteContentErrorCmd(err), err
		}
		issueID = ref.Value
	}
	return m.openPreviewContent(contentlink.Ref{Kind: contentlink.KindIssue, Value: issueID}, "Issue"), nil
}

func wrapPreviewIssueLoad(cmd tea.Cmd, workspaceID string) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if loaded, ok := msg.(issueview.LoadedMsg); ok {
			return previewIssueLoadedMsg{
				LoadedMsg: loaded, WorkspaceID: workspaceID,
			}
		}
		return msg
	}
}

func (m *Model) previewIssueForWorkspace(workspaceID string) *previewIssue {
	if m.preview.issue != nil && m.preview.workspaceID == workspaceID {
		return m.preview.issue
	}
	if cached, ok := m.preview.paneCache[workspaceID]; ok {
		return cached.issue
	}
	return nil
}

func (m *Model) applyPreviewIssueLoaded(msg previewIssueLoadedMsg) {
	issue := m.previewIssueForWorkspace(msg.WorkspaceID)
	if issue == nil || issue.surface != msg.WorkspaceID {
		return
	}
	for _, item := range issue.tabs.Items {
		if item.Value == nil {
			continue
		}
		matched := item.Value.ResultMatches(msg.LoadedMsg)
		if item.Value.SetResult(msg.LoadedMsg) {
			issue.hostNotice = ""
			return
		}
		if !matched {
			continue
		}
		if msg.NotModified {
			issue.hostNotice = ""
			return
		}
		if msg.Refresh && msg.Error != nil {
			issue.hostNotice = remoteDocumentStaleNotice
		}
		return
	}
}

func (m *Model) closePreviewIssue() tea.Cmd {
	if m.preview.deck == nil {
		return nil
	}
	m.preview.deck.FocusLeaf(m.preview.deck.Leaf(panelayout.Issue))
	for m.preview.deck.Leaf(panelayout.Issue) != 0 {
		m.preview.deck.CloseActive()
	}
	if ctx, ok := m.previewDeckContext(); ok {
		m.syncPreviewDeckProjection(ctx)
	}
	if m.preview.doc != nil {
		m.focusPreviewPane(panelayout.Document)
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

func (m *Model) closePreviewIssueTab() tea.Cmd {
	if m.preview.issue == nil {
		return nil
	}
	return m.closePreviewIssueTabAt(m.preview.issue.tabs.Active)
}

func (m *Model) closePreviewIssueTabAt(index int) tea.Cmd {
	if m.preview.deck == nil {
		return nil
	}
	m.preview.deck.CloseTab(m.preview.deck.Leaf(panelayout.Issue), index)
	return m.finishPreviewDeckClose()
}

func (m *Model) cyclePreviewIssueTab(delta int) tea.Cmd {
	if m.preview.deck == nil || !m.preview.deck.FocusLeaf(m.preview.deck.Leaf(panelayout.Issue)) {
		return nil
	}
	cmd := m.preview.deck.CycleTab(delta)
	if ctx, ok := m.previewDeckContext(); ok {
		m.syncPreviewDeckProjection(ctx)
	}
	return cmd
}

func (m *Model) clickPreviewIssueTab(index int) tea.Cmd {
	if m.preview.deck == nil {
		return nil
	}
	cmd := m.preview.deck.SelectTab(m.preview.deck.Leaf(panelayout.Issue), index)
	if ctx, ok := m.previewDeckContext(); ok {
		m.syncPreviewDeckProjection(ctx)
	}
	return cmd
}

func (m *Model) renderPreviewIssue(issue *previewIssue, box termpreview.Box) string {
	view := issue.view()
	contentHeight := max(box.H-termpreview.HeaderRows, 0)
	focused := m.PreviewFocused() && issue.focused
	if view != nil {
		view.SetSize(box.W, contentHeight)
		view.SetFocused(focused)
	}
	header := m.composePreviewHeader(m.previewHostHeaderTabs(issueview.LayoutTabStrip(issue.tabs, m.reserveHeader(box.W, true).TabsWidth, focused).HoverClose(m.tabCloseHoverIn(panelayout.Issue)).Row, issue.hostNotice), box.W, panelayout.Issue)
	if contentHeight <= 0 {
		return header
	}
	body := ""
	if view != nil {
		m.bindPreviewPaneSelection(view, box)
		body = view.View()
	}
	return header + "\n" + body
}

// registerPreviewIssueRegion covers the Issue leaf's INNER box.
func (m *Model) registerPreviewIssueRegion(issueBox termpreview.Box) {
	if m.preview.issue == nil {
		return
	}
	m.workspacesMouse.HitMap.AddRect(
		previewIssueRegionKind,
		issueBox.X, issueBox.Y, issueBox.W, issueBox.H,
		previewIssueRegionKind,
	)
}

func (m *Model) registerPreviewIssueTabRegions(issueBox termpreview.Box) {
	if m.preview.issue == nil {
		return
	}
	focused := m.PreviewFocused() && m.preview.issue.focused
	strip := issueview.LayoutTabStrip(m.preview.issue.tabs, m.reserveHeader(issueBox.W, true).TabsWidth, focused)
	strip.RegisterHits(func(col, width, index int, close bool) {
		m.workspacesMouse.HitMap.AddRect(
			previewIssueTabKind,
			issueBox.X+col, issueBox.Y, width, 1,
			previewIssueTabHit{Index: index, Close: close},
		)
	})
}

func (m *Model) handlePreviewIssueMouse(action mouse.MouseAction) tea.Cmd {
	if tab, ok := action.Region.Data.(previewIssueTabHit); ok {
		if action.Type == mouse.ActionClick || action.Type == mouse.ActionDoubleClick {
			if tab.Close {
				return m.closePreviewIssueTabAt(tab.Index)
			}
			return m.clickPreviewIssueTab(tab.Index)
		}
		if m.preview.issue != nil {
			switch action.Type {
			case mouse.ActionScrollUp, mouse.ActionScrollDown:
				m.scrollPreviewIssueByWheel(action.Delta)
			}
		}
		return nil
	}
	issue := m.preview.issue
	kind, _ := regionKind(action.Region)
	if kind != previewIssueRegionKind || issue == nil {
		return nil
	}
	view := issue.view()
	switch action.Type {
	case mouse.ActionClick:
		m.focusPreviewPane(panelayout.Issue)
		if view == nil {
			return nil
		}
		lx := action.X - action.Region.Rect.X
		ly := action.Y - action.Region.Rect.Y - termpreview.HeaderRows
		if view.SelectableAt(lx, ly) {
			// The card's own targets — a parent row, a subtask row, its bar —
			// keep their clicks; everything else in the body is text, and a
			// press over it arms a selection.
			return m.pressPreviewPaneSelection(panelayout.Issue, action)
		}
		kind, cmd := view.HandleClick(lx, ly)
		if kind == issueview.HitScrollbar {
			// The card armed a scrollbar gesture (and did any track-click
			// jump itself). Start the shared handler's drag so motions come
			// back to ScrollbarDrag and the release anywhere settles it.
			issue.scrollTrackY = action.Y - ly
			m.workspacesMouse.StartDrag(action.X, action.Y, previewIssueScrollbarKind, 0)
		}
		return cmd
	case mouse.ActionDoubleClick:
		// The preceding click already navigated. Consume Bubble Tea's
		// follow-up double event so a child that has just rendered its parent
		// at this cell cannot immediately navigate back — but a rapid second
		// press on the card's bar re-arms the gesture through the seam that
		// can never reach a nav row.
		m.focusPreviewPane(panelayout.Issue)
		if view != nil {
			lx := action.X - action.Region.Rect.X
			ly := action.Y - action.Region.Rect.Y - termpreview.HeaderRows
			if view.SelectableAt(lx, ly) {
				// Word by double click, line by triple, exactly as the
				// terminal beside it answers the same gesture.
				return m.pressPreviewPaneSelection(panelayout.Issue, action)
			}
			if view.PressScrollbar(lx, ly) {
				issue.scrollTrackY = action.Y - ly
				m.workspacesMouse.StartDrag(action.X, action.Y, previewIssueScrollbarKind, 0)
			}
		}
		return nil
	case mouse.ActionScrollUp, mouse.ActionScrollDown:
		m.scrollPreviewIssueByWheel(action.Delta)
	}
	return nil
}

// previewIssueView is the issue card on screen, or nil when the pane holds
// nothing renderable.
func (m *Model) previewIssueView() *issueview.Model {
	if m.preview.issue == nil {
		return nil
	}
	return m.preview.issue.view()
}

// dragPreviewIssueScrollbar extends an issue card's scrollbar gesture from a
// held pointer. The press-time snapshot of the card's top row maps the pointer
// onto view-local rows; the shared core clamps past both ends of the track.
func (m *Model) dragPreviewIssueScrollbar(action mouse.MouseAction) {
	if issue := m.preview.issue; issue != nil {
		if view := issue.view(); view != nil {
			view.ScrollbarDrag(action.Y - issue.scrollTrackY)
		}
	}
}

// endPreviewIssueScrollbarDrag settles an issue card's scrollbar gesture
// wherever the pointer is; the offset is view state and nothing persists it.
func (m *Model) endPreviewIssueScrollbarDrag() {
	if view := m.previewIssueView(); view != nil {
		view.ScrollbarDragEnd()
	}
}

// scrollPreviewIssueByWheel applies one notch to the issue pane through the
// shared burst guard, so a mid-range flick coalesces into the same handful of
// repaints here that it earns on the terminal and Files surfaces. A held-back
// delta is not lost; it rides the next flush.
func (m *Model) scrollPreviewIssueByWheel(delta int) {
	if m.preview.issue == nil {
		return
	}
	flushed, ok := m.preview.issue.wheel.Add(delta, m.now())
	if !ok {
		return
	}
	if view := m.preview.issue.view(); view != nil {
		view.Scroll(flushed)
	}
}

// previewIssueWheelAtBoundary asks the card whether inertia over it is spent,
// and drops any held delta when it is — the same pairing the boundary filter
// and burst keep on every other surface, so a tail dropped at the top cannot
// leak into the next gesture's first flush.
func (m *Model) previewIssueWheelAtBoundary(delta int) bool {
	issue := m.preview.issue
	if issue == nil {
		return true
	}
	view := issue.view()
	if view == nil {
		return true
	}
	bounded := view.ScrollAtBoundary(delta)
	if bounded {
		issue.wheel.Reset()
	}
	return bounded
}

func (m *Model) previewIssueKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	issue := m.preview.issue
	if issue == nil || !issue.focused || m.PreviewInteractive() {
		return false, nil
	}
	// Before the pane's own keys: esc clears a selection rather than closing
	// the pane out from under it, and the copy chord must not fall through to
	// a card key that happens to share it.
	if cmd, handled := m.handlePreviewPaneSelectionKey(panelayout.Issue, msg); handled {
		return true, cmd
	}
	switch msg.String() {
	case "q", "esc":
		return true, m.closePreviewIssue()
	case "x":
		return true, m.closePreviewIssueTab()
	case "{":
		return true, m.cyclePreviewIssueTab(-1)
	case "}":
		return true, m.cyclePreviewIssueTab(1)
	case "y":
		return true, m.yankPreviewIssue(false)
	case "Y", "shift+y":
		return true, m.yankPreviewIssue(true)
	}
	view := issue.view()
	if view == nil {
		return true, nil
	}
	issue.focused = true
	view.SetActive(true)
	view.SetFocused(true)
	handled, cmd := view.HandleKey(msg)
	if handled {
		return true, cmd
	}
	// Unclaimed keys fall through to WorkspacesKey, which lets host globals
	// through and swallows the rest so they cannot drive the list.
	return false, nil
}

func (m *Model) yankPreviewIssue(idOnly bool) tea.Cmd {
	view := m.preview.issue.view()
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
