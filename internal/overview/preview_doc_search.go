package overview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/panemodal"
	"github.com/marcus/sidecar/internal/panesearch"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/ui"
)

// The two search surfaces a focused document pane can open on itself — the
// fuzzy file finder (ctrl+p) and the project-wide ripgrep search (f) — drawn
// here exactly as the project workspace draws them, because they are the same
// surfaces: internal/panesearch owns what they are, and this file owns only
// where they are painted and what happens to the file they pick.
//
// The project workspace answered both keys and this browser answered neither,
// which is the one-surface-only gap the parity rule forbids.

// openPreviewDocFinder opens the fuzzy file finder in the focused document
// pane, rooted at that pane's directory.
func (m *Model) openPreviewDocFinder() tea.Cmd {
	if hostID := m.previewRemoteHostID(); hostID != "" {
		return remoteDocumentUnsupported(hostID, "File finding")
	}
	doc := m.preview.doc
	if doc == nil || doc.root == "" {
		return nil
	}
	mode, scan := panesearch.NewFinder(&m.docFinderCaches, doc.root, doc.epoch)
	doc.mode = mode
	return previewDocSearchCmd(scan, doc.surface)
}

// openPreviewDocProjectSearch opens the ripgrep project search in the focused
// document pane, rooted at that pane's directory.
func (m *Model) openPreviewDocProjectSearch() tea.Cmd {
	if hostID := m.previewRemoteHostID(); hostID != "" {
		return remoteDocumentUnsupported(hostID, "Project search")
	}
	doc := m.preview.doc
	if doc == nil || doc.root == "" {
		return nil
	}
	mode := panesearch.NewProject(doc.root, doc.epoch)
	if box, ok := m.previewDocBox(); ok {
		mode.SetSize(box.W, box.H)
	}
	doc.mode = mode
	return nil
}

// previewDocSearchMsg wraps one search surface's own async message on its way
// back to the pane that issued it. The surfaces' messages are broadcast types
// the Files plugin also uses, and a file scan carries neither root nor surface,
// so an unwrapped filefind.ScannedMsg could land in the wrong pane's cache. The
// surface also keeps the result attached to its originating pane while the
// global Sessions selection is visiting another workspace.
type previewDocSearchMsg struct {
	Msg         tea.Msg
	WorkspaceID string
}

func previewDocSearchCmd(cmd tea.Cmd, workspaceID string) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if msg == nil {
			return nil
		}
		return previewDocSearchMsg{Msg: msg, WorkspaceID: workspaceID}
	}
}

// applyPreviewDocSearchMsg delivers a surface's own async result back to the
// pane that issued it. A pane that has since closed its surface drops the
// message, and a stale epoch is dropped inside the surface.
func (m *Model) applyPreviewDocSearchMsg(msg previewDocSearchMsg) tea.Cmd {
	doc := m.previewDocForWorkspace(msg.WorkspaceID)
	if doc == nil || doc.mode == nil {
		return nil
	}
	return previewDocSearchCmd(doc.mode.Update(msg.Msg), msg.WorkspaceID)
}

func (m *Model) previewDocForWorkspace(workspaceID string) *previewDoc {
	if m.preview.doc != nil && m.preview.workspaceID == workspaceID {
		return m.preview.doc
	}
	if cached, ok := m.preview.paneCache[workspaceID]; ok {
		return cached.doc
	}
	return nil
}

// previewDocSearchActive reports whether a pane-scoped search surface owns the
// keyboard. The focus context and the footer read it.
func (m *Model) previewDocSearchActive() bool {
	return m.docPaneFocused() && m.preview.doc.mode != nil
}

// previewDocFindActive reports whether the focused document is running an
// in-file search (`/`). A different surface from the finder above — the bar
// belongs to docview and is drawn inside the document — making the same claim
// on the keyboard.
func (m *Model) previewDocFindActive() bool {
	return m.docPaneFocused() && m.preview.doc.mode == nil && m.preview.doc.view().SearchActive()
}

// WorkspacesDocFindPaste offers a bracketed paste to the in-file search bar of
// the focused preview document. The bar is a text field like any other, so it
// takes a paste exactly as it takes typed characters; docview declines when no
// search is taking text and the paste carries on down the app's routing.
func (m *Model) WorkspacesDocFindPaste(msg tea.PasteMsg) (bool, tea.Cmd) {
	if m == nil || !m.previewDocFindActive() {
		return false, nil
	}
	return m.preview.doc.view().HandleSearchPaste(msg)
}

// closePreviewDocSearch drops the surface and gives the document back the
// keyboard.
func (m *Model) closePreviewDocSearch() {
	doc := m.preview.doc
	if doc == nil {
		return
	}
	doc.mode.Close()
	doc.mode = nil
	doc.modeRegions = nil
}

// cancelPreviewDocSearch drops the surface the way the user dismissing it
// means: with nothing chosen. A pane that never got a file has nothing left to
// be once the search is gone, so it closes with it — the same rule the project
// workspace applies to a pane opened straight into the finder.
func (m *Model) cancelPreviewDocSearch() tea.Cmd {
	doc := m.preview.doc
	if doc == nil {
		return nil
	}
	m.closePreviewDocSearch()
	if len(doc.tabs.Items) > 0 {
		return nil
	}
	return m.closePreviewDoc()
}

// closeUnfocusedPreviewDocSearch is the focus rule both surfaces share: a
// search is a modal scoped to its pane, and a modal that has lost the keyboard
// is dismissed rather than left drawn and inert. In-file search goes with it,
// for the same reason.
func (m *Model) closeUnfocusedPreviewDocSearch() tea.Cmd {
	doc := m.preview.doc
	if doc == nil || doc.focused {
		return nil
	}
	for _, item := range doc.tabs.Items {
		item.View.CloseSearch()
	}
	if doc.mode == nil {
		return nil
	}
	return m.cancelPreviewDocSearch()
}

// handlePreviewDocSearchKey routes a keypress to the live surface. Every key
// belongs to it while it is open, so nothing leaks to the browser behind it.
func (m *Model) handlePreviewDocSearchKey(msg tea.KeyPressMsg) tea.Cmd {
	doc := m.preview.doc
	if doc == nil || doc.mode == nil {
		return nil
	}
	if box, ok := m.previewDocBox(); ok {
		doc.mode.SetSize(box.W, box.H)
	}
	out, cmd := doc.mode.HandleKey(msg)
	return m.applyPreviewDocSearchOutcome(out, cmd)
}

// handlePreviewDocSearchMouse routes a mouse event to the live surface. The
// surface hit-tests the regions its last render registered, which panemodal
// placed at the pane's true position, so a click inside the modal hits the
// modal rather than the document under it. Outside the pane is the modal's
// backdrop: a press there dismisses it, everything else is swallowed.
func (m *Model) handlePreviewDocSearchMouse(msg tea.MouseMsg) tea.Cmd {
	doc := m.preview.doc
	if doc == nil || doc.mode == nil {
		return nil
	}
	if msg != nil {
		pos := msg.Mouse()
		if box, ok := m.previewDocBox(); ok && !boxContains(box, pos.X, pos.Y) {
			if _, isClick := msg.(tea.MouseClickMsg); isClick {
				return m.cancelPreviewDocSearch()
			}
			return nil
		}
	}
	out, cmd := doc.mode.HandleMouse(msg, m.workspacesMouse)
	return m.applyPreviewDocSearchOutcome(out, cmd)
}

func (m *Model) applyPreviewDocSearchOutcome(out panesearch.Outcome, cmd tea.Cmd) tea.Cmd {
	workspaceID := ""
	if m.preview.doc != nil {
		workspaceID = m.preview.doc.surface
	}
	wrapped := previewDocSearchCmd(cmd, workspaceID)
	switch {
	case out.Cancelled:
		return tea.Batch(wrapped, m.cancelPreviewDocSearch())
	case out.Open && out.Path != "":
		m.closePreviewDocSearch()
		return tea.Batch(wrapped, m.loadPreviewDocSearchResult(out))
	}
	return wrapped
}

// loadPreviewDocSearchResult puts the chosen file into the pane through the
// pane's own tab machinery: a plain pick replaces the active tab, shift+enter
// opens a new one.
func (m *Model) loadPreviewDocSearchResult(out panesearch.Outcome) tea.Cmd {
	doc := m.preview.doc
	if doc == nil {
		return nil
	}
	rel := docview.NormalizeTabPath(out.Path)
	if rel == "" || rel == "." {
		return nil
	}
	if idx := doc.tabs.IndexOf(rel); idx >= 0 {
		return wrapPreviewDocLoad(m.selectPreviewDocTab(idx, out.Line, nil), doc.surface)
	}
	viewer := docview.New(nil)
	cmd := viewer.Load(doc.allocID(), doc.root, rel, out.Line, doc.epoch)
	applyPreviewDocRenderMode(viewer, rel, out.Line)
	if !out.NewTab && doc.view() != nil {
		doc.tabs.Items[doc.tabs.Active].View = viewer
	} else {
		doc.tabs.Append(viewer)
	}
	return wrapPreviewDocLoad(cmd, doc.surface)
}

// previewDocBox is the document leaf's inner box in screen coordinates: what
// the modal is scoped to and what a click-away test is measured against.
func (m *Model) previewDocBox() (termpreview.Box, bool) {
	peer, ok := m.previewPeerBox()
	if !ok {
		return termpreview.Box{}, false
	}
	return m.previewPaneBox(panelayout.Document, peer)
}

func boxContains(box termpreview.Box, x, y int) bool {
	return x >= box.X && x < box.X+box.W && y >= box.Y && y < box.Y+box.H
}

// renderPreviewDocSearchOverlay composites the live surface over the pane's own
// body, inside the pane's box. The result is exactly the box, which is what
// keeps the app header on screen.
//
// The surface draws into a scratch handler rather than the browser's, because
// panemodal translates every region in the handler it is given and the pane's
// own regions are already in the browser's. The regions are kept on the pane
// and added last (see registerPreviewDocSearchRegions).
func (m *Model) renderPreviewDocSearchOverlay(doc *previewDoc, background string, box termpreview.Box) string {
	if doc == nil || doc.mode == nil || box.W <= 0 || box.H <= 0 {
		return background
	}
	scratch := mouse.NewHandler()
	out := panemodal.RenderFunc(
		panemodal.Box{X: box.X, Y: box.Y, W: box.W, H: box.H},
		ui.FitBlock(background, box.W, box.H), scratch, doc.mode.View)
	doc.modeRegions = scratch.HitMap.Regions()
	return out
}

// clearPreviewDocSearchRegions drops last frame's surface regions. Only a pane
// that is drawn puts its regions back.
func (m *Model) clearPreviewDocSearchRegions() {
	if m.preview.doc != nil {
		m.preview.doc.modeRegions = nil
	}
}

// registerPreviewDocSearchRegions puts a live surface's hit regions into the
// browser's hit map last, so they beat the pane's own leaf region under them.
func (m *Model) registerPreviewDocSearchRegions() {
	doc := m.preview.doc
	if doc == nil || doc.mode == nil || m.workspacesMouse == nil {
		return
	}
	for _, region := range doc.modeRegions {
		m.workspacesMouse.HitMap.Add(region.ID, region.Rect, region.Data)
	}
}
