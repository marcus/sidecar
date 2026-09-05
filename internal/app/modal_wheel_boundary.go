package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
)

// Wheel events reaching an app-level overlay are answered by the overlay that
// owns them, using the precedence in activeModal. Every answer here must mirror
// what the matching handler in Update would actually do with the same event:
// true means "certain no-op, safe to drop before Update and View", and false
// means movable or unknown, which is always forwarded.
//
// App modals are drawn as full-screen overlays (ui.OverlayModal) and Update
// routes the untranslated mouse message to them, so these queries work in
// screen coordinates - the -headerHeight translation applies only to the plugin
// and global-scope branches underneath.

// modalWheelLines matches modal.wheelLines: one notch of modal-body scroll.
const modalWheelLines = 3

// activeModalWheelAtBoundary answers for the highest-priority open overlay.
func (m *Model) activeModalWheelAtBoundary(msg tea.MouseWheelMsg) bool {
	switch m.activeModal() {
	case ModalPalette:
		// The palette owns its filtering rules, so it answers for its own
		// cursor rather than having them reproduced here.
		return m.palette.WheelAtBoundary(msg)

	case ModalHelp:
		// handleHelpModalMouse ensures the modal and returns; it never routes a
		// mouse event into it, so every wheel over the help overlay is absorbed
		// and changes nothing.
		return true

	case ModalUpdate:
		return m.updateModalWheelAtBoundary(msg)

	case ModalDiagnostics:
		return modalWheelAtBoundary(m.diagnosticsModal, m.diagnosticsMouseHandler, msg)

	case ModalQuitConfirm:
		return modalWheelAtBoundary(m.quitModal, m.quitMouseHandler, msg)

	case ModalProjectSwitcher:
		// The project-add flow is nested inside the switcher's state rather than
		// being its own ModalKind, and Update gives it the mouse first.
		if m.projectAddMode {
			if m.projectAddThemeMode {
				// handleProjectAddThemePickerMouse has no mouse handling at all.
				return true
			}
			return modalWheelAtBoundary(m.projectAddModal, m.projectAddMouseHandler, msg)
		}
		return m.projectSwitcherWheelAtBoundary(msg)

	case ModalWorktreeSwitcher:
		return modalWheelAtBoundary(m.worktreeSwitcherModal, m.worktreeSwitcherMouseHandler, msg)

	case ModalThemeSwitcher:
		return modalWheelAtBoundary(m.themeSwitcherModal, m.themeSwitcherMouseHandler, msg)

	case ModalOpenIn:
		return modalWheelAtBoundary(m.openInModal, m.openInMouseHandler, msg)

	case ModalIssueInput:
		return modalWheelAtBoundary(m.issueInputModal, m.issueInputMouseHandler, msg)

	case ModalIssuePreview:
		return m.issuePreviewWheelAtBoundary(msg)

	case ModalPaneReposition:
		h, controller := m.activePaneLayoutController()
		return h != nil && controller != nil && controller.Modal().WheelAtBoundary(msg, h.mouse)

	case ModalPaneSwitcher:
		return m.paneSwitcherWheelAtBoundary(msg)
	}
	return false
}

// modalWheelAtBoundary is the shared answer for overlays whose wheel handling is
// exactly modal.Modal.HandleMouse: the body scrolls, everything else (backdrop,
// buttons, inputs) absorbs the event. An unbuilt modal or hit map is unknown.
func modalWheelAtBoundary(md *modal.Modal, h *mouse.Handler, msg tea.MouseWheelMsg) bool {
	if md == nil || h == nil {
		return false
	}
	return md.WheelAtBoundary(msg, h)
}

// updateModalWheelAtBoundary answers for the update overlay. In the Overview
// phase the notes section owns the wheel wherever the pointer is over the
// modal, so it answers from its last-rendered window geometry; other phases
// use the shared body answer on the one persistent modal.
func (m *Model) updateModalWheelAtBoundary(msg tea.MouseWheelMsg) bool {
	if m.updateNotesOwnsScroll() {
		var delta int
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			delta = -modalWheelLines
		case tea.MouseWheelDown:
			delta = modalWheelLines
		default:
			return false
		}
		return m.updateNotesAtBoundary(delta)
	}
	return modalWheelAtBoundary(m.updateModal, m.updateMouseHandler, msg)
}

// projectSwitcherMaxVisible is the visible-row count used when the terminal's
// height is not yet known. The live count comes from
// Model.projectSwitcherVisibleRows, which follows the window.
const projectSwitcherMaxVisible = 8

// projectSwitcherWheelAtBoundary answers for the project switcher, whose wheel
// moves the cursor by one filtered entry rather than scrolling the modal body.
// Dropping the tail here is what keeps a boundary event from rebuilding the
// modal (clearProjectSwitcherModal) and re-previewing the project theme.
func (m *Model) projectSwitcherWheelAtBoundary(msg tea.MouseWheelMsg) bool {
	var delta int
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return false
	}

	cursor := m.projectSwitcherCursor
	next := cursor + delta
	if delta > 0 {
		if next >= len(m.projectSwitcherFiltered) {
			next = len(m.projectSwitcherFiltered) - 1
		}
	}
	if next < 0 {
		next = 0
	}
	if next != cursor {
		return false
	}
	// The cursor cannot move; the list offset must be stable too, otherwise the
	// same event would still scroll the visible window.
	pinned := m.projectSwitcherPinnedCount()
	scrollAfter := projectSwitcherEnsureCursorVisible(max(0, cursor-pinned), m.projectSwitcherScroll, m.projectSwitcherVisibleRows())
	return scrollAfter == m.projectSwitcherScroll
}

// issuePreviewClock is the clock the preview card's burst reads.
func (m *Model) issuePreviewClock() time.Time {
	if m.issuePreviewWheelNow != nil {
		return m.issuePreviewWheelNow()
	}
	return time.Now()
}

// issuePreviewWheelAtBoundary answers for the td issue preview. Its host
// intercepts every wheel event and drives the issue card's own viewport, so the
// card is the scroll owner whenever it exists; the declarative modal body only
// answers before the card is built.
func (m *Model) issuePreviewWheelAtBoundary(msg tea.MouseWheelMsg) bool {
	view := m.issuePreviewView
	if view == nil {
		// ensureIssuePreviewView would build the card from freshly arrived data;
		// building it here is not this query's job, and the modal body answer
		// would not describe what Update does once it exists.
		if m.issuePreviewData != nil {
			return false
		}
		return modalWheelAtBoundary(m.issuePreviewModal, m.issuePreviewMouseHandler, msg)
	}
	// handleIssuePreviewMouse treats every non-down wheel as an upward scroll.
	delta := -modalWheelLines
	if msg.Mouse().Button == tea.MouseWheelDown {
		delta = modalWheelLines
	}
	bounded := view.ScrollAtBoundary(delta)
	if bounded && m.issuePreviewWheel != nil {
		m.issuePreviewWheel.Reset()
	}
	return bounded
}
