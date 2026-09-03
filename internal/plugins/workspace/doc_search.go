package workspace

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/panemodal"
	"github.com/marcus/sidecar/internal/panesearch"
	"github.com/marcus/sidecar/internal/ui"
)

// A document pane can host the same two search surfaces the Files plugin has:
// the fuzzy file finder (ctrl+p) and the project-wide ripgrep search (f). What
// they are lives in internal/panesearch, which the global Workspaces browser
// binds too; what is left here is where they are drawn and what happens to the
// file they pick. Both are rooted at the pane's own doc.root — a pane carries
// the workspace or shell directory it was opened against, and neither surface
// asks anything else about the host.
//
// The surface is drawn as a modal scoped to the pane's box (internal/panemodal),
// below the pane's header row: sized to its own content and centred with the
// pane's document dimmed around it when the pane has room for a readable
// margin, and taking the whole box when it does not. It is never a full-screen
// takeover — a pane on a large monitor is bigger than any picker needs — and
// never a bare inline widget stranded in a large pane. The header row is never
// covered either way, so a pane always says both which file it holds and what
// it is doing.

// docSearchMsg wraps one search surface's own async message on its way back to
// the pane that issued it. The surfaces' messages are broadcast types the Files
// plugin also uses, and a file scan carries no root, so an unwrapped
// filefind.ScannedMsg from another plugin's finder would land in this pane's
// cache as if it described this pane's directory. Wrapping keeps a pane's
// traffic its own, and the leaf ID keeps it the right pane's.
type docSearchMsg struct {
	LeafID int
	Msg    tea.Msg
}

// docSearchCmd tags a surface's command with the pane that issued it.
func docSearchCmd(leafID int, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if msg == nil {
			return nil
		}
		return docSearchMsg{LeafID: leafID, Msg: msg}
	}
}

// focusedDocPane is the document pane that holds the keyboard, or nil. It is
// the narrower question activeDocPane answers loosely: with two document panes
// open, only one of them is taking keys.
func (p *Plugin) focusedDocPane() *docPane {
	if !p.docFocused() {
		return nil
	}
	leaf := FindPane(p.paneRoot, p.paneFocus)
	if leaf == nil || leaf.Kind != PaneDoc {
		return nil
	}
	return p.docs[leaf.ContentID]
}

// docSearchPane is the focused document pane when it is showing a search
// surface.
func (p *Plugin) docSearchPane() *docPane {
	doc := p.focusedDocPane()
	if doc == nil || doc.mode == nil {
		return nil
	}
	return doc
}

// docSearchActive reports whether a pane-scoped search surface owns the
// keyboard. The footer, the focus context, and the global key gate all read it.
func (p *Plugin) docSearchActive() bool {
	return p.docSearchPane() != nil
}

// docFindActive reports whether the focused document is running an in-file
// search (`/`). It is a different surface from docSearchActive's finder overlay
// — the bar belongs to docview and is drawn inside the document — but it makes
// the same claim on the keyboard, so the focus context and the text-input gate
// read it the same way.
func (p *Plugin) docFindActive() bool {
	doc := p.focusedDocPane()
	if doc == nil || doc.mode != nil {
		return false
	}
	return doc.view().SearchActive()
}

// handleDocFindPaste offers a bracketed paste to the in-file search bar of the
// focused document pane. The bar is a text field like any other, so it takes a
// paste exactly as it takes typed characters; docview declines when no search
// is taking text and the paste carries on to the sidebar filter or the
// terminal.
func (p *Plugin) handleDocFindPaste(msg tea.PasteMsg) (bool, tea.Cmd) {
	if !p.docFindActive() {
		return false, nil
	}
	return p.focusedDocPane().view().HandleSearchPaste(msg)
}

// docPaneByLeaf finds a pane by its tree leaf, which is how a wrapped async
// message names the pane that issued it.
func (p *Plugin) docPaneByLeaf(leafID int) *docPane {
	for _, doc := range p.docs {
		if doc != nil && doc.leafID == leafID {
			return doc
		}
	}
	return nil
}

// openDocFinder opens the fuzzy file finder in the focused document pane,
// rooted at that pane's directory.
func (p *Plugin) openDocFinder(doc *docPane) tea.Cmd {
	if doc == nil || p.ctx == nil {
		return nil
	}
	mode, scan := panesearch.NewFinder(&p.docFinderCaches, doc.root, p.ctx.Epoch)
	doc.mode = mode
	return docSearchCmd(doc.leafID, scan)
}

// openDocProjectSearch opens the ripgrep project search in the focused document
// pane, rooted at that pane's directory.
func (p *Plugin) openDocProjectSearch(doc *docPane) tea.Cmd {
	if doc == nil || p.ctx == nil {
		return nil
	}
	mode := panesearch.NewProject(doc.root, p.ctx.Epoch)
	mode.SetSize(doc.boxW, doc.boxH)
	doc.mode = mode
	return nil
}

// closeDocSearch drops the surface and gives the document back the keyboard.
func (p *Plugin) closeDocSearch(doc *docPane) {
	if doc == nil {
		return
	}
	doc.mode.Close()
	doc.mode = nil
	doc.modeRegions = nil
}

// cancelDocSearch drops a pane's search surface the way the user dismissing it
// means: with nothing chosen. A pane that was opened *for* the search and never
// got a file — F splits a new document pane straight into the finder — has
// nothing left to be once the search is gone, so it closes with it. What
// remained instead was a blank pane taking a third of the width, with no
// filename in its header and nothing on screen saying what it was or how to
// get rid of it.
//
// A pane that already holds a file is untouched: cancelling a search there
// means "never mind, I am still reading this", and the file is what the pane
// is for. The question is only ever whether the pane has a document, never how
// the pane came to exist, so every route into an empty pane is covered by the
// one rule.
func (p *Plugin) cancelDocSearch(doc *docPane) tea.Cmd {
	if doc == nil {
		return nil
	}
	p.closeDocSearch(doc)
	if len(doc.tabs.Items) > 0 {
		return nil
	}
	return p.forgetContentPane(doc.leafID)
}

// closeUnfocusedDocSearches drops any pane search whose pane no longer holds
// the keyboard. It is the one rule this surface has about focus: a search is a
// modal scoped to its pane, and a modal that has lost the keyboard is dismissed
// rather than left drawn and inert. Enforcing it at the single focus writer
// (setFocusTarget) means every gesture that moves focus — Tab, a click on the
// sidebar or another leaf, a shortcut that focuses a pane — obeys it without
// each one having to remember to.
//
// Losing focus is a dismissal, so it goes through cancelDocSearch: a pane that
// only ever held a finder goes with it rather than staying on screen blank.
func (p *Plugin) closeUnfocusedDocSearches() tea.Cmd {
	focused := p.focusedDocPane()
	var cmds []tea.Cmd
	for _, doc := range p.docs {
		if doc == nil || doc == focused {
			continue
		}
		// In-file search is dismissed by the same rule, for the same reason: a
		// pane that lost the keyboard must not keep a live search bar drawn.
		for _, item := range doc.tabs.Items {
			item.View.CloseSearch()
		}
		if doc.mode == nil {
			continue
		}
		if cmd := p.cancelDocSearch(doc); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// handleDocSearchKey routes a keypress to the live surface. Every key belongs
// to it while it is open, the same way a focused document absorbs the keys it
// does not use, so nothing leaks to the workspace behind the pane.
func (p *Plugin) handleDocSearchKey(doc *docPane, msg tea.KeyPressMsg) tea.Cmd {
	if doc == nil || doc.mode == nil {
		return nil
	}
	doc.mode.SetSize(doc.boxW, doc.boxH)
	out, cmd := doc.mode.HandleKey(msg)
	return p.applyDocSearchOutcome(doc, out, cmd)
}

// handleDocSearchMouse routes a mouse event to the live surface. The surface
// hit-tests the regions its last render registered, which panemodal placed at
// the pane's true position, so a click inside the modal hits the modal rather
// than the document under it.
func (p *Plugin) handleDocSearchMouse(doc *docPane, msg tea.MouseMsg) tea.Cmd {
	if doc == nil || doc.mode == nil {
		return nil
	}
	// Outside the pane is outside the modal. A press there dismisses the
	// surface, the way a click on a full-screen modal's backdrop does, and the
	// click is spent on the dismissal rather than on whatever it landed over.
	// Every other event — motion, wheel, release — is swallowed: it is over the
	// modal's backdrop, and the file-info modal answers a backdrop event the
	// same way, by consuming it and doing nothing. Routing them into the
	// surface instead made the wheel over a terminal pane scroll the finder's
	// list, which no modal in this app does.
	if msg != nil {
		pos := msg.Mouse()
		// The header X sits above the search modal. It closes the whole leaf,
		// including a finder-only pane that has no file yet.
		if _, onClose := p.paneCloseAt(pos.X, pos.Y); onClose {
			switch msg.(type) {
			case tea.MouseClickMsg:
				cmd, _ := p.clickPaneCloseAt(pos.X, pos.Y)
				return cmd
			case tea.MouseMotionMsg:
				p.setPaneCloseHoverAt(pos.X, pos.Y)
				return nil
			default:
				return nil
			}
		}
		if !doc.boxContains(pos.X, pos.Y) {
			if _, isClick := msg.(tea.MouseClickMsg); isClick {
				return p.cancelDocSearch(doc)
			}
			return nil
		}
	}
	out, cmd := doc.mode.HandleMouse(msg, p.mouseHandler)
	return p.applyDocSearchOutcome(doc, out, cmd)
}

func (p *Plugin) applyDocSearchOutcome(doc *docPane, out panesearch.Outcome, cmd tea.Cmd) tea.Cmd {
	wrapped := docSearchCmd(doc.leafID, cmd)
	switch {
	case out.Cancelled:
		return tea.Batch(wrapped, p.cancelDocSearch(doc))
	case out.Open && out.Path != "":
		p.closeDocSearch(doc)
		return tea.Batch(wrapped, p.loadDocSearchResult(doc, out))
	}
	return wrapped
}

// loadDocSearchResult puts the chosen file into the pane through the pane's own
// tab machinery: an already-open file is focused (and jumps to the line), a
// plain pick replaces the active tab, and shift+enter opens a new one.
func (p *Plugin) loadDocSearchResult(doc *docPane, out panesearch.Outcome) tea.Cmd {
	if doc == nil {
		return nil
	}
	leaf := FindPane(p.paneRoot, doc.leafID)
	if leaf == nil || leaf.Kind != PaneDoc {
		return nil
	}
	ref := contentlink.Ref{Kind: contentlink.KindFile, Value: out.Path, Line: out.Line}
	if out.NewTab {
		return p.openWorkspaceContent(doc.root, doc.surface, ref, "Document")
	}
	return p.replaceWorkspaceContent(doc.root, doc.surface, ref)
}

// applyDocSearchMsg delivers a surface's own async result back to the pane that
// issued it. A pane that has since closed its surface drops the message, and a
// stale epoch is dropped inside the surface.
func (p *Plugin) applyDocSearchMsg(msg docSearchMsg) tea.Cmd {
	doc := p.docPaneByLeaf(msg.LeafID)
	if doc == nil || doc.mode == nil {
		return nil
	}
	return docSearchCmd(doc.leafID, doc.mode.Update(msg.Msg))
}

// renderDocSearchOverlay composites the live surface over the pane's own
// content, inside the pane's box. background is the leaf's rendered body, which
// is already exactly the box; the result is too, which is what keeps the app
// header on screen.
//
// The surface draws into a scratch handler rather than the plugin's, because
// the pane-tree regions are registered after this render and would otherwise
// win the reverse hit-test for the cells the modal is drawn on. The regions are
// kept on the pane and added last (see registerDocSearchRegions).
func (p *Plugin) renderDocSearchOverlay(doc *docPane, background string, origin mouse.Rect, size Size) string {
	if doc == nil || doc.mode == nil || size.Width <= 0 || size.Height <= 0 {
		return background
	}
	box := panemodal.Box{X: origin.X, Y: origin.Y, W: size.Width, H: size.Height}
	scratch := mouse.NewHandler()
	out := panemodal.RenderFunc(box, ui.FitBlock(background, size.Width, size.Height), scratch, doc.mode.View)
	doc.modeRegions = scratch.HitMap.Regions()
	return out
}

// clearDocSearchRegions drops every pane's remembered surface regions at the
// start of a frame. Only a pane that is drawn puts its regions back (see
// renderDocSearchOverlay), which is what keeps a pane the frame did not draw —
// the panes a zoomed leaf hides, above all — from registering hit regions over
// the pane that was drawn — a zoomed leaf hides its siblings, and a pane the
// frame skipped must not keep claiming the cells the drawn one occupies.
func (p *Plugin) clearDocSearchRegions() {
	for _, doc := range p.docs {
		if doc != nil {
			doc.modeRegions = nil
		}
	}
}

// registerDocSearchRegions puts a live surface's hit regions into the plugin's
// hit map last, so they beat the pane-tree leaf region drawn under them.
func (p *Plugin) registerDocSearchRegions() {
	for _, doc := range p.docs {
		if doc == nil || doc.mode == nil {
			continue
		}
		for _, region := range doc.modeRegions {
			p.mouseHandler.HitMap.Add(region.ID, region.Rect, region.Data)
		}
	}
}

// openFinderPane is the workspace list's F: a new document pane, split beside
// the terminal, opened straight into the file finder. A pane that already
// exists is reused rather than doubled, which is the same answer clicking a
// file path gives.
func (p *Plugin) openFinderPane() tea.Cmd {
	// Kanban draws no pane tree, so a pane opened from it would take keys with
	// nothing on screen to take them for.
	if p.paneRoot == nil || p.ctx == nil || p.viewMode != ViewModeList {
		return nil
	}
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		return nil
	}
	if doc, leaf := p.activeDocPane(); doc != nil && leaf != nil {
		p.paneFocus = leaf.ID
		p.activePane = PanePreview
		p.setShellLeafFocused(false)
		return p.openDocFinder(doc)
	}
	plan, planned := planPaneOpen(p.paneRoot, PaneDoc, p.lastPaneBoxes())
	if !planned {
		return p.docPaneToast("Document")
	}
	docID := p.paneNextID
	node := &PaneNode{ID: docID, Kind: PaneDoc, ContentID: docID}
	if !p.splitOnPlannedLeaf(plan, node, "Document") {
		// splitOnPlannedLeaf already set the fit toast when that was the reason.
		if p.toastMessage == "" {
			return p.docPaneToast("Document")
		}
		return nil
	}
	// A pane opened for the finder has no file in it yet: the finder is what
	// chooses the first tab.
	p.docs[docID] = newDocPane(p.paneFocus, root, surface, nil)
	p.activePane = PanePreview
	p.setShellLeafFocused(false)
	p.saveSelectionState()
	return tea.Batch(p.openDocFinder(p.docs[docID]), p.resizeDocTerminalCmd())
}

func (p *Plugin) docPaneToast(name string) tea.Cmd {
	p.toastMessage = paneFitMessage(name, SplitCols)
	p.toastTime = time.Now()
	return nil
}
