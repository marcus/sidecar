package overview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/mouse"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/textselect"
)

// Text selection in the global browser's non-document content panes.
//
// This is the parity half of the workspace plugin's pane_select.go, and the
// sibling of preview_doc_select.go: what a gesture means, what is highlighted
// and what reaches the clipboard is decided in each viewer's own binding, and
// each surface answers only where its leaf was drawn and which of its own
// events reach it.
//
// The document pane keeps its own file because its region is also the target a
// content link is resolved against; every other selectable pane is reached
// through textselect.Pane, so a pane kind that grows a selection joins by
// implementing that interface rather than by another arm here.

// previewPaneSelectKind names a text-selection drag that began in one of these
// panes. It is a gesture source rather than a hit target: the press is answered
// by the leaf's own region, and this is what routes every motion and the
// release back to the pane the press armed.
const previewPaneSelectKind = "global-preview-pane-select"

// bindPreviewPaneSelection tells a viewer where it is on screen and which
// chords it answers. It runs from the leaf's render, the only place that knows
// both and the only place that runs for exactly the pane that is drawn.
func (m *Model) bindPreviewPaneSelection(pane textselect.Pane, box termpreview.Box) {
	if pane == nil {
		return
	}
	config := m.TerminalConfig()
	pane.SetSelection(config.SelectionKeys(), config.CopyOnSelect)
	// The body sits below the leaf's own header row.
	pane.SetOrigin(box.X, box.Y+termpreview.HeaderRows)
}

// previewSelectionPane is the selectable viewer one preview leaf is showing, or
// nil. The check is per kind because a typed nil put into an interface is not
// nil.
func (m *Model) previewSelectionPane(kind panelayout.Kind) textselect.Pane {
	switch kind {
	case panelayout.Issue:
		if view := m.previewIssueView(); view != nil {
			return view
		}
	}
	return nil
}

// clearPreviewPaneSelectionsExcept drops every preview selection this surface
// holds but keep's, which is what makes one selection at a time the rule.
func (m *Model) clearPreviewPaneSelectionsExcept(keep textselect.Pane) {
	doc, _ := keep.(*docview.Model)
	m.clearPreviewDocSelections(doc)
	if view := m.previewIssueView(); view != nil && textselect.Pane(view) != keep {
		view.ClearSelection()
	}
}

// pressPreviewPaneSelection arms a selection gesture over one pane's text. A
// press that resolves to a click still does what it did before; this only
// decides what the motion after it means.
func (m *Model) pressPreviewPaneSelection(kind panelayout.Kind, action mouse.MouseAction) tea.Cmd {
	pane := m.previewSelectionPane(kind)
	if pane == nil {
		return nil
	}
	m.clearPreviewPaneSelectionsExcept(pane)
	// One selection at a time means one on this whole surface: the terminal is
	// drawn beside this pane, so its highlight goes with the rest.
	m.clearPreviewSelection()
	result := pane.HandleSelectionMouse(action)
	if !result.Handled {
		return nil
	}
	// Registered with the shared handler because that is what turns the release
	// into a drag end this gesture can be finished by.
	m.workspacesMouse.StartDrag(action.X, action.Y, previewPaneSelectKind, int(kind))
	m.previewSelectKind = kind
	return m.previewPaneSelectionResult(pane, result)
}

// handlePreviewPaneGesture answers the half of a selection gesture that does
// not arrive as a region hit: the motion, which routinely leaves the pane, and
// the release, which is the drag's rather than the region's under the pointer.
func (m *Model) handlePreviewPaneGesture(action mouse.MouseAction, wasDragging bool, dragSourceBefore string) (tea.Cmd, bool) {
	if action.DragStartID != previewPaneSelectKind && dragSourceBefore != previewPaneSelectKind {
		return nil, false
	}
	pane := m.previewSelectionPane(m.previewSelectKind)
	switch action.Type {
	case mouse.ActionDrag, mouse.ActionDragEnd:
		if pane == nil {
			return nil, true
		}
		return m.previewPaneSelectionResult(pane, pane.HandleSelectionMouse(action)), true
	case mouse.ActionHover:
		// A release lost off-window: the handler ends the drag on the first
		// button-less motion, and the gesture ends with it.
		if wasDragging && !m.workspacesMouse.IsDragging() {
			if pane != nil {
				pane.AbandonSelection()
			}
			return nil, true
		}
	}
	return nil, false
}

// handlePreviewPaneSelectionKey answers the chords that act on one pane's
// selection — including escape, which the viewer answers so it clears a
// selection before the pane's own esc can mean anything else.
func (m *Model) handlePreviewPaneSelectionKey(kind panelayout.Kind, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	pane := m.previewSelectionPane(kind)
	if pane == nil {
		return nil, false
	}
	result := pane.HandleSelectionKey(msg)
	if !result.Handled {
		return nil, false
	}
	return m.previewPaneSelectionResult(pane, result), true
}

// previewPaneSelectionResult is what this surface owes the engine's answer: a
// copy, delivered as this surface's own toast. Nothing persists these panes'
// scroll offsets, so a drag that scrolled one leaves nothing to save.
func (m *Model) previewPaneSelectionResult(pane textselect.Pane, result textselect.Result) tea.Cmd {
	return pane.SelectionCopyCmd(result, func(notice textselect.CopyNotice) tea.Msg {
		// A copy that worked is a flash; one that failed is a notification.
		if notice.IsError {
			return appmsg.ToastMsg{Message: notice.Message, Duration: notice.Duration, IsError: true}
		}
		return appmsg.FlashMsg{Text: notice.Message}
	})
}
