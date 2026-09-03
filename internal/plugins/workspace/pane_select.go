package workspace

import (
	tea "charm.land/bubbletea/v2"

	app "github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/textselect"
)

// Text selection in this surface's content panes.
//
// Everything about what a gesture means belongs to each viewer's own binding —
// internal/docview/select.go, internal/issueview/select.go and the others —
// which the global Workspaces browser and the app's content deck bind to as
// well. What is left here is surface-local routing: where a leaf was drawn, and
// which of this plugin's mouse and key events reach it. A rule added to this
// file rather than to a viewer is a rule the other surfaces will not have.
//
// Every viewer is reached through textselect.Pane, so a pane kind that grows a
// selection joins by implementing that interface rather than by another arm
// here.

// bindPaneSelection tells a viewer where it is on screen and which chords it
// answers. It runs from the leaf's render, which is the only place that knows
// both, and the only place that runs for exactly the panes that are drawn.
func (p *Plugin) bindPaneSelection(pane textselect.Pane, origin Box) {
	if pane == nil {
		return
	}
	config := p.terminalConfig()
	pane.SetSelection(config.SelectionKeys(), config.CopyOnSelect)
	// The body sits below the leaf's own header row, the same subtraction
	// SetSize makes for the viewport it hands the viewer.
	pane.SetOrigin(origin.X, origin.Y+terminalHeaderRows)
}

// selectionPaneAt is the selectable viewer a leaf is showing, or nil for a leaf
// that has none or has nothing in it.
//
// The check is per kind because a typed nil put into an interface is not nil: a
// leaf whose pane exists but whose viewer does not would otherwise answer every
// gesture as a pane with no rows.
func (p *Plugin) selectionPaneAt(leafID int) textselect.Pane {
	leaf := FindPane(p.paneRoot, leafID)
	if leaf == nil || leaf.Split != nil {
		return nil
	}
	switch leaf.Kind {
	case PaneDoc:
		if doc := p.docs[leaf.ContentID]; doc != nil {
			if view := doc.view(); view != nil {
				return view
			}
		}
	case PaneIssue:
		if issue := p.issues[leaf.ContentID]; issue != nil {
			if view := issue.view(); view != nil {
				return view
			}
		}
	}
	return nil
}

// docSelectionView is the document viewer a leaf is showing, or nil. It is what
// the document-only paths — a content-link press, the search surface — ask for.
func (p *Plugin) docSelectionView(leafID int) *docview.Model {
	leaf := FindPane(p.paneRoot, leafID)
	if leaf == nil || leaf.Split != nil || leaf.Kind != PaneDoc {
		return nil
	}
	doc := p.docs[leaf.ContentID]
	if doc == nil {
		return nil
	}
	return doc.view()
}

// clearPaneSelectionsExcept drops every pane selection this surface holds but
// keep's, which is what makes one selection at a time the rule: starting one
// anywhere drops the one before it, including one in another tab of the same
// leaf. A nil keep drops them all.
func (p *Plugin) clearPaneSelectionsExcept(keep textselect.Pane) {
	for _, doc := range p.docs {
		if doc == nil {
			continue
		}
		doc.tabs.ClearSelectionsExcept(docviewKeep(keep))
	}
	for _, issue := range p.issues {
		if issue == nil {
			continue
		}
		if view := issue.view(); view != nil && textselect.Pane(view) != keep {
			view.ClearSelection()
		}
	}
}

// docSelectionPane lifts a document viewer into the shared interface without
// turning a nil viewer into a non-nil interface value.
func docSelectionPane(view *docview.Model) textselect.Pane {
	if view == nil {
		return nil
	}
	return view
}

// docviewKeep is the document viewer to keep, for the tab set's own
// one-at-a-time sweep. A selection held by some other kind of pane keeps
// nothing here: every document's is dropped.
func docviewKeep(keep textselect.Pane) *docview.Model {
	if view, ok := keep.(*docview.Model); ok {
		return view
	}
	return nil
}

// clearDocSelectionsExcept drops every document selection this surface holds
// but view's. It is what a terminal gesture calls, with no document to keep, to
// take the one live selection for itself.
func (p *Plugin) clearDocSelectionsExcept(view *docview.Model) {
	p.clearPaneSelectionsExcept(view)
}

// pressPaneSelection arms a selection gesture in a content leaf.
//
// Focus has already followed the press, and a press that resolves to a click
// still does exactly what it did before — this only decides what the motion
// after it means. The drag is registered with the shared handler because that
// is what turns the release into a drag end the gesture can be finished by.
func (p *Plugin) pressPaneSelection(leafID int, action mouse.MouseAction) tea.Cmd {
	pane := p.selectionPaneAt(leafID)
	if pane == nil {
		return nil
	}
	p.clearPaneSelectionsExcept(pane)
	// One selection at a time means one on this whole surface: the terminal is
	// drawn beside this pane, so its highlight goes with the rest.
	p.clearTerminalSelection()
	result := pane.HandleSelectionMouse(action)
	if !result.Handled {
		// The press landed on the header, the gutter or the padding: not a
		// selection, and not this file's business.
		return nil
	}
	p.mouseHandler.StartDrag(action.X, action.Y, regionPaneLeaf, leafID)
	p.docSelectLeaf = leafID
	return p.paneSelectionResult(pane, result)
}

// dragPaneSelection extends the gesture the press armed. The pointer routinely
// leaves the pane mid-drag; the leaf it started in answers anyway.
func (p *Plugin) dragPaneSelection(action mouse.MouseAction) tea.Cmd {
	pane := p.selectionPaneAt(p.docSelectLeaf)
	if pane == nil {
		return nil
	}
	return p.paneSelectionResult(pane, pane.HandleSelectionMouse(action))
}

// finishPaneSelection resolves the release: a copy under copy-on-select, or a
// click that was never a drag and has already had its effect.
func (p *Plugin) finishPaneSelection(action mouse.MouseAction) tea.Cmd {
	pane := p.selectionPaneAt(p.docSelectLeaf)
	p.docSelectLeaf = 0
	p.lastDragRegion = ""
	if pane == nil {
		return nil
	}
	return p.paneSelectionResult(pane, pane.HandleSelectionMouse(action))
}

// abandonPaneSelection ends a gesture whose release was lost off-window.
func (p *Plugin) abandonPaneSelection() {
	if pane := p.selectionPaneAt(p.docSelectLeaf); pane != nil {
		pane.AbandonSelection()
	}
	p.docSelectLeaf = 0
}

// handlePaneSelectionKey answers the chords that act on a pane's selection —
// including escape, which the viewer answers so it clears a selection before
// the pane's own esc can mean anything else.
func (p *Plugin) handlePaneSelectionKey(pane textselect.Pane, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if pane == nil {
		return nil, false
	}
	result := pane.HandleSelectionKey(msg)
	if !result.Handled {
		return nil, false
	}
	return p.paneSelectionResult(pane, result), true
}

// paneSelectionResult is what this surface owes the engine's answer: a copy,
// delivered as this plugin's own toast, and a drag that scrolled the pane
// persisted the way every other scroll of it is.
func (p *Plugin) paneSelectionResult(pane textselect.Pane, result textselect.Result) tea.Cmd {
	if result.AutoScroll != 0 {
		p.saveSelectionState()
	}
	return pane.SelectionCopyCmd(result, func(notice textselect.CopyNotice) tea.Msg {
		// A copy that worked is a flash; one that failed is a notification.
		if notice.IsError {
			return app.ToastMsg{Message: notice.Message, Duration: notice.Duration, IsError: true}
		}
		return app.FlashMsg{Text: notice.Message}
	})
}
