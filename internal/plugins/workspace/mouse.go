package workspace

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/issueview"
	boardkanban "github.com/marcus/sidecar/internal/kanban"
	"github.com/marcus/sidecar/internal/mouse"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/paneframe"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
	sharedscroll "github.com/marcus/sidecar/internal/scroll"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/tabs"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspacediff"
	"github.com/marcus/sidecar/internal/workspacelist"
	"github.com/marcus/sidecar/internal/worktreedelete"
)

// Workspaces is declared "covered" in assembly.WheelBoundaryRegistry; this
// assertion makes losing the contract a compile error.
var _ plugin.WheelBoundaryConsumer = (*Plugin)(nil)

// previewActionHit names a Git-style header action chip (not a tab).
type previewActionHit int

const (
	previewActionDiff previewActionHit = iota
	previewActionTask
)

func (p *Plugin) previewTaskID() string {
	if wt := p.selectedWorktree(); wt != nil {
		return wt.TaskID
	}
	return ""
}

func (p *Plugin) clickPreviewAction(data any) tea.Cmd {
	hit, ok := data.(previewActionHit)
	if !ok {
		return nil
	}
	if p.viewMode == ViewModeInteractive {
		p.exitInteractiveMode()
	}
	if p.paneRoot == nil {
		return appmsg.ShowFlash(features.WorkspaceDocPanesDisabledDiff)
	}
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		return nil
	}
	switch hit {
	case previewActionTask:
		id := p.previewTaskID()
		if id == "" {
			return nil
		}
		return p.openIssuePaneForSurface(root, surface, id)
	default:
		return p.openDiffPaneForSurface(root, surface, workspacediff.WorkingTreeTarget())
	}
}

// WheelAtBoundary implements plugin.WheelBoundaryConsumer for the project
// Workspaces surface. It follows the same hit regions as handleMouseScroll but
// performs no loads or visible mutations, allowing Bubble Tea to discard an
// inertial tail before Update and View.
func (p *Plugin) WheelAtBoundary(msg tea.MouseWheelMsg) bool {
	if p.mouseHandler == nil {
		return false
	}
	if bounded, ok := p.modalWheelAtBoundary(msg); ok {
		return bounded
	}
	if p.isModalViewMode() {
		return false
	}
	action := p.mouseHandler.HandleMouse(msg)
	if action.Type != mouse.ActionScrollUp && action.Type != mouse.ActionScrollDown {
		return false
	}
	if tty.WheelStaysWithPointer(p.viewMode == ViewModeInteractive) {
		return p.terminalWheelAtBoundary(p.interactiveTermPanel(), action)
	}
	regionID := ""
	if action.Region != nil {
		regionID = action.Region.ID
	}
	switch regionID {
	case regionSidebar, regionWorktreeItem,
		string(workspacelist.RegionScrollbarThumb), string(workspacelist.RegionScrollbarTrack):
		// The bar's column sits over the sidebar background, so a notch there
		// scrolls the list exactly as it did before the bar was clickable.
		return (sharedscroll.Bounds{
			Position: p.sharedSidebarSelectionIndex(),
			Maximum:  len(p.visibleSidebarItems()) - 1,
		}).AtBoundary(action.Delta)
	case regionPaneLeaf, regionDocLink, regionDocTab, regionIssueTab, regionNoteTab, regionResourceTab, regionDiffTargetTab, regionPaneClose,
		regionNoteScrollbarThumb, regionNoteScrollbarTrack:
		leafID := 0
		switch data := action.Region.Data.(type) {
		case int:
			leafID = data
		case docTabHit:
			leafID = data.LeafID
		case docContentLinkHit:
			leafID = data.LeafID
		case issueTabHit:
			leafID = data.LeafID
		case noteTabHit:
			leafID = data.LeafID
		case resourceTabHit:
			leafID = data.LeafID
		case diffTabHit:
			leafID = data.LeafID
		case noteScrollbarHit:
			// A notch on a note pane's bar is bounded by that pane, exactly as
			// it was when the bar's column answered as the leaf body.
			leafID = data.LeafID
		}
		leaf := FindPane(p.paneRoot, leafID)
		if leaf == nil {
			return true
		}
		switch leaf.Kind {
		case PaneDoc:
			doc := p.docs[leaf.ContentID]
			return doc == nil || doc.view() == nil || doc.view().ScrollAtBoundary(action.Delta)
		case PaneIssue:
			issue := p.issues[leaf.ContentID]
			bounded := issue == nil || issue.view() == nil || issue.view().ScrollAtBoundary(action.Delta)
			if issue != nil && bounded {
				issue.wheel.Reset()
			}
			return bounded
		case PaneNote:
			note := p.notes[leaf.ContentID]
			return note == nil || note.view() == nil || note.view().ScrollAtBoundary(action.Delta)
		case PaneDiff:
			view := p.activeDiffView()
			return view.ScrollAtBoundary(action.Delta, view.Height())
		case PaneResource:
			res := p.resources[leaf.ContentID]
			return res == nil || res.pane == nil || res.pane.ScrollAtBoundary(action.Delta)
		default:
			return false
		}
	case regionTermPanelContent:
		return p.terminalWheelAtBoundary(true, action)
	case regionDiffTabFile, regionDiffTabCommit, regionDiffTabFileListPane, regionDiffTabPreviewFile,
		regionDiffTabDiffPane, regionDiffTabMinimap, regionCommitFileDiffPane,
		regionCommitFileItem, regionCommitFileBack:
		return p.activeDiffView().WheelAtBoundary(action.Region.ID, action.Delta)
	case regionPreviewPane:
		if p.previewShowsTerminal() {
			return p.terminalWheelAtBoundary(false, action)
		}
		return (sharedscroll.Bounds{Position: p.previewOffset, Maximum: p.getMaxScrollOffset()}).AtBoundary(action.Delta)
	case regionKanbanCard, regionKanbanColumn, regionKanbanScrollbar:
		return false
	}
	if action.Region != nil {
		return false
	}
	if p.viewMode == ViewModeKanban {
		return false
	}
	split := p.previewSplit()
	if p.sidebarVisible && action.X < split.SidebarWidth {
		return (sharedscroll.Bounds{
			Position: p.sharedSidebarSelectionIndex(),
			Maximum:  len(p.visibleSidebarItems()) - 1,
		}).AtBoundary(action.Delta)
	}
	if p.previewShowsTerminal() {
		return p.terminalWheelAtBoundary(false, action)
	}
	return (sharedscroll.Bounds{Position: p.previewOffset, Maximum: p.getMaxScrollOffset()}).AtBoundary(action.Delta)
}

// modalWheelAtBoundary answers for whichever modal overlay currently owns mouse
// input, following the same precedence as handleMouse. ok is false when no
// overlay owns the wheel, which lets the ordinary panes and terminals answer.
//
// Every one of these modals routes the wheel through modal.HandleMouse, which
// only scrolls the modal body, so the shared modal query is the exact answer.
// ViewModeFilePicker stays unknown: it is not one of the modal mouse branches
// and renders its own overlay over the list regions.
func (p *Plugin) modalWheelAtBoundary(msg tea.MouseWheelMsg) (bounded, ok bool) {
	if p.paneLayoutModal != nil {
		return p.paneLayoutModal.Modal().WheelAtBoundary(msg, p.mouseHandler), true
	}
	if p.docInfo != nil {
		return p.docInfo.WheelAtBoundary(msg, p.mouseHandler), true
	}
	switch p.viewMode {
	case ViewModeCreate:
		if p.createBusyStep != "" {
			// A busy create step drops every mouse event untouched.
			return true, true
		}
		if p.createPlan != nil {
			return p.createOperationModal != nil && p.createOperationModal.WheelAtBoundary(msg, p.mouseHandler), true
		}
		m := p.createFormModal()
		return m != nil && m.WheelAtBoundary(msg, p.mouseHandler), true
	case ViewModeTaskLink:
		return p.taskLinkModal != nil && p.taskLinkModal.WheelAtBoundary(msg, p.mouseHandler), true
	case ViewModeRenameShell:
		return p.renameShellModal != nil && p.renameShellModal.WheelAtBoundary(msg, p.mouseHandler), true
	case ViewModeRenameWorktree:
		return p.renameWorktreeModal != nil && p.renameWorktreeModal.WheelAtBoundary(msg, p.mouseHandler), true
	case ViewModeConfirmDelete:
		return p.deleteConfirm.WheelAtBoundary(p.width, msg, p.mouseHandler), true
	case ViewModeConfirmDeleteShell:
		return p.deleteShellModal != nil && p.deleteShellModal.WheelAtBoundary(msg, p.mouseHandler), true
	case ViewModeConfirmCloseSplit:
		return p.closeSplitModal != nil && p.closeSplitModal.WheelAtBoundary(msg, p.mouseHandler), true
	case ViewModeAgentConfig:
		return p.agentConfigModal != nil && p.agentConfigModal.WheelAtBoundary(msg, p.mouseHandler), true
	case ViewModeAgentChoice:
		return p.agentChoiceModal != nil && p.agentChoiceModal.WheelAtBoundary(msg, p.mouseHandler), true
	case ViewModeFetchPR:
		return p.fetchPRModal != nil && p.fetchPRModal.WheelAtBoundary(msg, p.mouseHandler), true
	case ViewModeMerge:
		return p.mergeModal != nil && p.mergeModal.WheelAtBoundary(msg, p.mouseHandler), true
	case ViewModeCommitForMerge:
		return p.commitForMergeModal != nil && p.commitForMergeModal.WheelAtBoundary(msg, p.mouseHandler), true
	}
	return false, false
}

// isModalViewMode returns true when a modal overlay is active (not List, Kanban, or Interactive).
func (p *Plugin) isModalViewMode() bool {
	switch p.viewMode {
	case ViewModeList, ViewModeKanban, ViewModeInteractive:
		return false
	default:
		return true
	}
}

// isDiffTextRegion reports the Diff regions whose cells are text rather than a
// row the pane answers a click on. A file row and a commit row are targets; the
// patch body and the panes' own backgrounds are prose.
func isDiffTextRegion(regionID string) bool {
	switch regionID {
	case regionDiffTabDiffPane, regionCommitFileDiffPane, regionDiffTabFileListPane:
		return true
	default:
		return false
	}
}

// isDiffBodyRegion reports the Diff inner hits that cover the leaf body and
// therefore skip the regionPaneLeaf click arm. Tab chips are not included —
// they already go through selectDiffTab.
func isDiffBodyRegion(regionID string) bool {
	return workspacediff.IsBodyRegion(regionID)
}

// isBackgroundRegion returns true for regions registered by renderListView()
// that should not respond to mouse events when a modal is open.
func isBackgroundRegion(regionID string) bool {
	switch regionID {
	case regionSidebar, regionPreviewPane, regionPaneDivider,
		regionWorktreeItem, regionPreviewAction, regionDiffTargetTab, regionListFilter, regionListFilterClear,
		regionCreateWorktreeButton, regionShellsPlusButton, regionWorkspacesPlusButton, regionListSortButton,
		regionStartAgentButton, regionOpenCreateButton, regionOpenSetupButton,
		regionPaneClose, regionPaneTitle,
		regionKanbanCard, regionKanbanColumn, regionKanbanScrollbar, regionViewToggle,
		regionDiffTabDivider, regionTermPanelContent, regionPaneTreeDivider,
		regionTermScrollbarThumb, regionTermScrollbarTrack,
		regionDiffTabFile, regionDiffTabCommit, regionDiffTabDiffPane, regionDiffTabMinimap,
		regionCommitFileItem, regionCommitFileBack, regionCommitFileDiffPane,
		regionDiffTabPreviewFile, regionDiffTabFileListPane,
		string(workspacelist.RegionScrollbarThumb), string(workspacelist.RegionScrollbarTrack):
		return true
	default:
		return false
	}
}

// handleMouse processes mouse input.
func (p *Plugin) handleMouse(msg tea.MouseMsg) tea.Cmd {
	// Every mouse event counts as activity for the terminals, routed to one of
	// them or not: the shared key gate reads that clock to tell the bracket of a
	// split SGR report from a typed one, and the component owns it.
	p.noteTerminalMouseActivity()
	if p.paneLayoutModal != nil {
		return p.handlePaneLayoutModalMouse(msg)
	}

	// The View surface hit-tests its own regions, so a click inside it cannot
	// reach the list underneath.
	if p.viewFlyoutActive() {
		return p.handleViewFlyoutMouse(msg)
	}

	if p.docInfo != nil {
		if p.docInfo.HandleMouse(msg, p.mouseHandler) {
			p.closeDocInfo()
		}
		return nil
	}

	// A live pane editor takes the pointer before anything else in the split:
	// inside its body the pointer is vim's, and a click outside it is a request
	// to leave a session that has not been saved yet.
	if doc := p.pointerDocEdit(msg); doc != nil {
		if handled, cmd := p.handleDocEditMouse(doc, msg); handled {
			return cmd
		}
	}

	// A pane-scoped search surface takes the pointer the way the file-info modal
	// does: it hit-tests its own regions, which panemodal placed where the pane
	// actually is, so a click inside the modal cannot reach the document under it.
	if doc := p.docSearchPane(); doc != nil {
		return p.handleDocSearchMouse(doc, msg)
	}

	if p.viewMode == ViewModeCreate {
		return p.handleCreateModalMouse(msg)
	}
	if p.viewMode == ViewModeTaskLink {
		return p.handleTaskLinkModalMouse(msg)
	}

	if p.viewMode == ViewModeRenameShell {
		return p.handleRenameShellModalMouse(msg)
	}

	if p.viewMode == ViewModeRenameWorktree {
		return p.handleRenameWorktreeModalMouse(msg)
	}

	if p.viewMode == ViewModeConfirmDelete {
		return p.handleConfirmDeleteModalMouse(msg)
	}

	if p.viewMode == ViewModeConfirmDeleteShell {
		return p.handleConfirmDeleteShellModalMouse(msg)
	}

	if p.viewMode == ViewModeConfirmCloseSplit {
		return p.handleConfirmCloseSplitModalMouse(msg)
	}

	if p.viewMode == ViewModeAgentConfig {
		return p.handleAgentConfigModalMouse(msg)
	}

	if p.viewMode == ViewModeAgentChoice {
		return p.handleAgentChoiceModalMouse(msg)
	}

	if p.viewMode == ViewModeFetchPR {
		return p.handleFetchPRModalMouse(msg)
	}

	if p.viewMode == ViewModeMerge {
		return p.handleMergeModalMouse(msg)
	}

	if p.viewMode == ViewModeCommitForMerge {
		return p.handleCommitForMergeModalMouse(msg)
	}

	wasDragging := p.mouseHandler.IsDragging()
	dragSourceBefore := p.mouseHandler.DragRegion()
	action := p.mouseHandler.HandleMouse(msg)
	// A release can be lost when the pointer leaves the window or focus changes.
	// The mouse handler cancels that stale drag on the next button-less motion;
	// cancel the paired click-to-activate intent at the same boundary.
	if action.Type == mouse.ActionHover && wasDragging && !p.mouseHandler.IsDragging() {
		if dragSourceBefore == regionPaneLeaf {
			p.abandonPaneSelection()
		}
		if dragSourceBefore == regionIssueScrollbar {
			p.finishIssueScrollbarDrag()
		}
		if dragSourceBefore == regionKanbanScrollbar {
			p.kanban.ReleaseScrollbar()
		}
		if isNoteScrollbarDragID(dragSourceBefore) {
			p.finishNoteScrollbarDrag()
		}
		if isTermScrollbarDragID(dragSourceBefore) {
			p.finishTerminalScrollbarDrag()
		}
		if isListScrollbarID(dragSourceBefore) {
			p.sidebarBar.gesture.End()
		}
		// Drop what the press armed and end the gesture: an edge scroll tick still
		// in flight belongs to a gesture that is over, and neither activation nor a
		// forwarded click survives a release the app never saw.
		p.pointer.Abandon()
		p.syncTerminalResizeHold()
		if p.terminalPointerIntent(mouse.ActionHover, "", dragSourceBefore, true) == tty.PointerAbandon &&
			p.selection.Anchor.Valid() {
			// A release outside the window never reaches Bubble Tea. Close the local
			// selection gesture at the same point the shared handler abandons its drag.
			return p.finishInteractiveSelection()
		}
	}

	var cmd tea.Cmd
	switch action.Type {
	case mouse.ActionClick:
		cmd = p.handleMouseClick(action)
	case mouse.ActionDoubleClick:
		cmd = p.handleMouseDoubleClick(action)
	case mouse.ActionTripleClick:
		cmd = p.handleMouseTripleClick(action)
	case mouse.ActionScrollUp, mouse.ActionScrollDown:
		cmd = p.handleMouseScroll(action)
	case mouse.ActionScrollLeft, mouse.ActionScrollRight:
		cmd = p.handleMouseHorizontalScroll(action)
	case mouse.ActionDrag:
		cmd = p.handleMouseDrag(action)
	case mouse.ActionDragEnd:
		cmd = p.handleMouseDragEnd(action)
	case mouse.ActionHover:
		cmd = p.handleMouseHover(action)
	}
	// After click-to-start and after EndDrag, so reconcile in this Update
	// sees the same hold the drop flush already applied.
	p.syncTerminalResizeHold()
	return cmd
}

func (p *Plugin) handleTaskLinkModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureTaskLinkModal()
	if p.taskLinkModal == nil {
		return nil
	}
	action := p.taskLinkModal.HandleMouse(msg, p.mouseHandler)
	if action == "cancel" || action == createCancelID {
		p.closeTaskLinkModal()
		return nil
	}
	if idx, ok := parseIndexedID(taskLinkItemPrefix, action); ok && idx >= 0 && idx < len(p.taskSearchFiltered) && p.linkingWorktree != nil {
		task := p.taskSearchFiltered[idx]
		wt := p.linkingWorktree
		p.closeTaskLinkModal()
		return p.linkTask(wt, task.ID)
	}
	if action == taskLinkFieldID {
		p.taskSearchInput.Focus()
	}
	return nil
}

func (p *Plugin) handleCreateModalMouse(msg tea.MouseMsg) tea.Cmd {
	if p.createBusyStep != "" {
		return nil
	}
	if p.createPlan != nil {
		p.ensureCreateOperationModal()
		if p.createOperationModal == nil {
			return nil
		}
		action := p.createOperationModal.HandleMouse(msg, p.mouseHandler)
		if action == "cancel" || action == createCancelID {
			if p.createSetupResult != nil {
				return nil
			}
			p.createPlan, p.createOperationModal, p.createSetupResult = nil, nil, nil
			p.createOperationWidth = 0
			return nil
		}
		return p.handleCreateOperationAction(action)
	}
	p.ensureCreateModal()
	m := p.createFormModal()
	if m == nil {
		return nil
	}

	action := m.HandleMouse(msg, p.mouseHandler)
	if picked := p.createForm.TranslateMouseAction(action); picked != action {
		action = picked
	}
	if action == workspacecreate.FieldSkip {
		// Checkbox clicks return the ID without toggling; Space is the toggle.
		_, _ = m.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	}
	p.syncCreateFormAfterInput()

	switch action {
	case "":
		return nil
	case createSubmitID:
		return p.submitCreateForm()
	case createCancelID, "cancel":
		p.viewMode = ViewModeList
		p.clearCreateModal()
		return nil
	}
	if workspacecreate.IsPlacementAction(action) {
		return p.createFormPlacementAction(action)
	}

	return nil
}

func (p *Plugin) handleRenameShellModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureRenameShellModal()
	if p.renameShellModal == nil {
		return nil
	}

	action := p.renameShellModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel", renameShellCancelID:
		p.viewMode = ViewModeList
		p.clearRenameShellModal()
		return nil
	case renameShellActionID, renameShellRenameID:
		return p.executeRenameShell()
	}
	return nil
}

func (p *Plugin) handleRenameWorktreeModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureRenameWorktreeModal()
	if p.renameWorktreeModal == nil {
		return nil
	}

	action := p.renameWorktreeModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel", renameWorktreeCancelID:
		p.viewMode = ViewModeList
		p.clearRenameWorktreeModal()
		return nil
	case renameWorktreeActionID, renameWorktreeRenameID:
		return p.executeRenameWorktree()
	}
	return nil
}

func (p *Plugin) handleConfirmDeleteModalMouse(msg tea.MouseMsg) tea.Cmd {
	switch p.deleteConfirm.HandleMouse(p.width, msg, p.mouseHandler) {
	case worktreedelete.OutcomeCancel:
		return p.cancelDelete()
	case worktreedelete.OutcomeConfirm:
		return p.executeDelete()
	}
	return nil
}

func (p *Plugin) handleConfirmDeleteShellModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureConfirmDeleteShellModal()
	if p.deleteShellModal == nil {
		return nil
	}

	action := p.deleteShellModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel", deleteShellConfirmCancelID:
		return p.cancelShellDelete()
	case deleteShellConfirmDeleteID:
		return p.executeShellDelete()
	}
	return nil
}

func (p *Plugin) handleAgentChoiceModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureAgentChoiceModal()
	if p.agentChoiceModal == nil {
		return nil
	}

	action := p.agentChoiceModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel", agentChoiceCancelID:
		p.viewMode = ViewModeList
		p.clearAgentChoiceModal()
		return nil
	case agentChoiceActionID, agentChoiceConfirmID, "agent-choice-attach", "agent-choice-restart":
		return p.executeAgentChoice()
	}
	return nil
}

func (p *Plugin) handleAgentConfigModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureAgentConfigModal()
	if p.agentConfigModal == nil {
		return nil
	}

	prevAgent := p.agentConfigAgentType
	prevSkip := p.agentConfigSkipPerms
	action := p.agentConfigModal.HandleMouse(msg, p.mouseHandler)
	p.syncAgentConfigFromIdx()
	if p.agentConfigAgentType != prevAgent {
		p.loadAgentConfigAutoApprove()
	} else if p.agentConfigSkipPerms != prevSkip {
		p.persistAgentConfigAutoApprove()
	}

	switch action {
	case "":
		return nil
	case "cancel", agentConfigCancelID:
		p.viewMode = ViewModeList
		p.clearAgentConfigModal()
		return nil
	case agentConfigSubmitID:
		return p.executeAgentConfig()
	case agentConfigSkipPermissionsID:
		p.agentConfigSkipPerms = !p.agentConfigSkipPerms
		p.persistAgentConfigAutoApprove()
		return nil
	}
	return nil
}

func (p *Plugin) handleFetchPRModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureFetchPRModal()
	if p.fetchPRModal == nil {
		return nil
	}

	action := p.fetchPRModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "cancel":
		p.viewMode = ViewModeList
		p.clearFetchPRState()
		return nil
	}
	return nil
}

func (p *Plugin) handleMergeModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureMergeModal()
	if p.mergeModal == nil {
		return nil
	}

	action := p.mergeModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel", "dismiss":
		p.cancelMergeWorkflow()
		p.clearMergeModal()
		return nil
	case "continue", "abort", "retry-push":
		return p.recoverDirectMerge(action)
	case mergePRURLID:
		if p.mergeState != nil && p.mergeState.PRURL != "" {
			return openInBrowser(p.mergeState.PRURL)
		}
		return nil
	case mergeFallbackDraftID:
		return p.startPRDraft(false)
	case mergeAgentDraftID:
		return p.startPRDraft(true)
	case mergeCreatePRID:
		return p.advanceMergeStep()
	case "check-pr":
		if p.mergeState != nil {
			return p.checkPRMerged(p.mergeState.Worktree)
		}
	case mergeStopWatchingID:
		if p.mergeState != nil {
			p.mergeState.PRWatchStopped = true
			p.clearMergeModal()
		}
		return nil
	case mergeMethodActionID, mergeTargetActionID, mergeCleanUpButtonID:
		// Advance to next step
		return p.advanceMergeStep()
	case mergeSkipButtonID:
		// Skip all cleanup
		if p.mergeState != nil {
			p.mergeState.DeleteLocalWorktree = false
			p.mergeState.DeleteLocalBranch = false
			p.mergeState.DeleteRemoteBranch = false
			p.mergeState.PullAfterMerge = false
		}
		return p.advanceMergeStep()
	}
	return nil
}

func (p *Plugin) handleCommitForMergeModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureCommitForMergeModal()
	if p.commitForMergeModal == nil {
		return nil
	}

	action := p.commitForMergeModal.HandleMouse(msg, p.mouseHandler)
	switch action {
	case "":
		return nil
	case "cancel", commitForMergeCancelID:
		p.mergeCommitState = nil
		p.mergeCommitMessageInput = textinput.Model{}
		p.clearCommitForMergeModal()
		p.viewMode = ViewModeList
		return nil
	case commitForMergeActionID, commitForMergeCommitID:
		message := p.mergeCommitMessageInput.Value()
		if message == "" {
			p.mergeCommitState.Error = "Commit message cannot be empty"
			return nil
		}
		p.mergeCommitState.Error = ""
		return p.stageAllAndCommit(p.mergeCommitState.Worktree, message)
	}
	return nil
}

// handleMouseHover handles hover events for visual feedback.
func (p *Plugin) handleMouseHover(action mouse.MouseAction) tea.Cmd {
	// Guard: absorb background region hovers when a modal is open (td-f63097).
	if p.isModalViewMode() && action.Region != nil && isBackgroundRegion(action.Region.ID) {
		return nil
	}

	// Handle hover in modals that have button hover states
	switch p.viewMode {
	case ViewModeCreate:
		return nil
	case ViewModeAgentConfig:
		// Modal library handles hover state internally
		return nil
	case ViewModeAgentChoice:
		// Modal library handles hover state internally
		return nil
	case ViewModeRenameShell, ViewModeRenameWorktree:
		// Modal library handles hover state internally
		return nil
	case ViewModeMerge:
		// Modal library handles hover state internally
		return nil
	case ViewModeCommitForMerge:
		// Modal library handles hover state internally
		return nil
	default:
		p.kanban.ClearHover()
		// Handle sidebar header button hover
		p.hoverNewButton = false
		p.hoverSortButton = false
		p.hoverShellsPlusButton = false
		p.hoverWorkspacesPlusButton = false
		p.hoverStartAgentButton = false
		p.hoverPaneClose = 0
		p.hoverPaneLayout = 0
		p.hoverTabClose = tabs.CloseHover{}
		p.hoverDividerRegion = ""
		p.hoverDividerID = 0
		p.sidebarBar.hover = false
		p.hoverTermBarSet = false
		if action.Region != nil {
			switch action.Region.ID {
			case regionTermScrollbarThumb, regionTermScrollbarTrack:
				if hit, ok := action.Region.Data.(terminalScrollbarHit); ok {
					p.hoverTermBar = hit
					p.hoverTermBarSet = true
				}
			case regionDocTab, regionIssueTab, regionNoteTab, regionResourceTab, regionDiffTargetTab:
				// Only the × half of a tab hovers. The rest of the pill is a
				// select target, and a select target that lit up would promise
				// a close the click does not do.
				p.setTabCloseHover(action.Region.Data)
				p.clearIssueHover()
			case regionKanbanCard, regionKanbanScrollbar:
				// A card lights the card; a lane bar lights the bar. The
				// component answers by region kind, so one route serves both.
				if region, ok := action.Region.Data.(boardkanban.HitRegion); ok {
					p.kanban.HandlePointer(boardkanban.PointerHover, region)
				}
			case regionCreateWorktreeButton:
				p.hoverNewButton = true
			case regionListSortButton:
				p.hoverSortButton = true
			case regionShellsPlusButton:
				p.hoverShellsPlusButton = true
			case regionWorkspacesPlusButton:
				p.hoverWorkspacesPlusButton = true
			case regionStartAgentButton:
				p.hoverStartAgentButton = true
			case regionPaneDivider, regionDiffTabDivider:
				p.hoverDividerRegion = action.Region.ID
				p.clearIssueHover()
			case regionPaneTreeDivider:
				p.hoverDividerRegion = action.Region.ID
				if id, ok := action.Region.Data.(int); ok {
					p.hoverDividerID = id
				}
				p.clearIssueHover()
			case regionPaneClose:
				if leafID, ok := action.Region.Data.(int); ok {
					p.hoverPaneClose = leafID
				}
				p.clearIssueHover()
			case regionPaneLayout:
				if leafID, ok := action.Region.Data.(int); ok {
					p.hoverPaneLayout = leafID
				}
				p.clearIssueHover()
			case regionPaneLeaf:
				// Only an issue leaf hovers. A document under the pointer
				// leaves the issue's hover behind, the same as any other
				// region does, because both are now one region.
				issue, _ := p.issueLeafAt(action.Region.Data)
				if issue == nil {
					p.clearIssueHover()
					break
				}
				if view := issue.view(); view != nil {
					lx, ly := issueViewLocal(action.X, action.Y, action.Region.Rect)
					view.HandleHover(lx, ly)
				}
			default:
				p.clearIssueHover()
			}
		} else {
			p.clearIssueHover()
		}
		// The list's bar lights under the pointer wherever its regions resolve,
		// and goes dark the moment the pointer leaves them — including a nil
		// region, which the switch above cannot answer.
		p.sidebarBar.hover = action.Region != nil && isListScrollbarID(action.Region.ID)
	}
	return nil
}

func (p *Plugin) clearIssueHover() {
	for _, issue := range p.issues {
		if issue == nil {
			continue
		}
		for _, item := range issue.tabs.Items {
			if item.Value != nil {
				item.Value.HandleHover(-1, -1)
			}
		}
	}
}

// notePressAwayFromTerminal answers a button going down anywhere but a
// terminal: it ends the gesture the press armed and the mode it was armed in,
// so a divider, a row and the sidebar all leave the pane identically. Ending
// only one of them would leave a live pane holding the keyboard behind a
// divider drag, or fire the armed click under a selection the user moved away
// from. Which actions put a button down is the shared layer's, or a surface
// answers a double click differently from a single one.
func (p *Plugin) notePressAwayFromTerminal(action mouse.MouseAction) {
	if action.Region == nil || !tty.PressesTerminal(action.Type) {
		return
	}
	if !tty.PressLeavesTerminal(action.Region.ID, regionPreviewPane, regionTermPanelContent) {
		return
	}
	p.pointer.Abandon()
	if p.viewMode == ViewModeInteractive {
		p.exitInteractiveMode()
	}
}

// handleMouseClick handles single click events.
func (p *Plugin) handleMouseClick(action mouse.MouseAction) tea.Cmd {
	if action.Region == nil {
		return nil
	}

	// Guard: absorb background region clicks when a modal is open (td-f63097).
	// Without this, clicks on empty modal space fall through to background regions
	// registered by renderListView(), causing enterInteractiveMode/pane switches.
	if p.isModalViewMode() && isBackgroundRegion(action.Region.ID) {
		return nil
	}
	// Focus follows the pointer's LEAF before any region handler runs, so the
	// ring lands on what was clicked whether or not that leaf's kind happens to
	// own a click-to-focus region. A terminal leaf owns none — its presses are
	// the live pane's — which is why hanging focus off the region handlers left
	// the ring behind on the neighbour. Moving focus first also means a handler
	// that wants something finer (the terminal panel inside the terminal leaf)
	// still gets the last word. A modal is drawn over the tree and its own
	// targets are not background regions, so it is excluded explicitly: a click
	// on a file-picker row is not a click on the pane behind it.
	if !p.isModalViewMode() {
		paneframe.FocusLeafAt(paneHost{p}, action.X, action.Y)
	}
	p.notePressAwayFromTerminal(action)
	if action.Region.ID == regionPaneLayout {
		if leafID, ok := action.Region.Data.(int); ok {
			return p.openPaneLayoutModal(leafID)
		}
		return nil
	}
	if cmd, ok := p.clickPaneCloseAt(action.X, action.Y); ok {
		return cmd
	}
	if cmd, ok := p.clickDocTabAt(action.X, action.Y); ok {
		return cmd
	}
	if cmd, ok := p.clickIssueTabAt(action.X, action.Y); ok {
		return cmd
	}
	if cmd, ok := p.clickNoteTabAt(action.X, action.Y); ok {
		return cmd
	}

	// Inner Diff regions win the hit test over regionPaneLeaf, so they must
	// take pane-tree focus themselves or keys stay on the previous leaf.
	if isDiffBodyRegion(action.Region.ID) {
		p.focusActiveDiffLeaf()
	}

	// A press on the sidebar list's bar grabs or jumps-to-spot before any row
	// logic can run: the reverse-scanned hit map gave the bar this column, and
	// a scrollbar press never selects a shell or worktree underneath it.
	if isListScrollbarID(action.Region.ID) {
		p.pressListScrollbar(action)
		return nil
	}

	// A press on a terminal surface's bar grabs or jumps-to-spot instead of
	// reaching the pane under it — and never becomes a forwarded click or a
	// text selection, whatever the application in the pane is doing with the
	// mouse (plan rule 4). The double-click case re-grabs exactly like a
	// single press.
	if _, ok := action.Region.Data.(terminalScrollbarHit); ok {
		switch action.Type {
		case mouse.ActionClick, mouse.ActionDoubleClick:
			p.pressTerminalScrollbar(action)
		}
		return nil
	}

	// Interactive mode: seamless pane switching between agent and terminal panel
	if p.viewMode == ViewModeInteractive {
		switch action.Region.ID {
		case regionTermPanelContent:
			p.activePane = PanePreview
			if p.interactiveState != nil && !p.terminalPaneIsPanel(p.interactiveState.LeafID) {
				// Switch from agent pane to terminal panel
				p.exitInteractiveMode()
				return p.enterTermPanelInteractiveMode()
			}
			// Already targeting terminal panel — arm the gesture and let the
			// release decide between the app's click and a local selection.
			if p.interactiveState != nil && p.interactiveState.Active {
				return p.prepareInteractiveTerminalGesture(action)
			}
			return p.forwardClickToTmux(action.X, action.Y)
		case regionPreviewPane:
			p.activePane = PanePreview
			if p.interactiveState != nil && p.terminalPaneIsPanel(p.interactiveState.LeafID) {
				// Switch from terminal panel to agent pane
				p.exitInteractiveMode()
				return p.enterInteractiveMode()
			}
			// Already targeting agent pane — arm the gesture and let the release
			// decide between the app's click and a local selection.
			if p.interactiveState != nil && p.interactiveState.Active {
				return p.prepareInteractiveTerminalGesture(action)
			}
			return p.forwardClickToTmux(action.X, action.Y)
		}
	}

	switch action.Region.ID {
	case regionListSortButton:
		// Click on the [⇅ Sort] pill - open View, the same surface v opens.
		p.openViewFlyout()
		return nil
	case regionCreateWorktreeButton:
		// Header [+] opens the form with Worktree selected and kind focused.
		return p.openCreateModalFocusKind()
	case regionOpenSetupButton:
		// The blocked empty state's pill: the mouse path for the Enter above it.
		return p.openSetupCmd()
	case regionOpenCreateButton:
		return p.openCreateModal()
	case regionStartAgentButton:
		return p.openStartAgentCreate(p.selectedWorktree())
	case regionShellsPlusButton:
		// Click on Shells [+] button - immediately create a new shell
		return p.createNewShell("")
	case regionWorkspacesPlusButton:
		// Worktrees [+] opens the form with Worktree selected and Name focused.
		return p.openCreateModal()
	case regionSidebar:
		p.focusSidebar()
	case regionPreviewPane:
		p.focusLeaf(terminalLeafID(p.paneRoot))
		// Keep the passive viewport stable through the whole pointer gesture.
		// A release without motion makes the terminal live; motion selects text.
		// Entering interactive mode here would resize and reframe the terminal
		// before drag tracking is armed, so an immediate click-drag selection
		// jumps or disappears.
		if !action.Shift && !action.Alt {
			if cmd, ok := p.activateTerminalLink(action); ok {
				return cmd
			}
		}
		p.releaseTerminalDocProjection(false)
		return p.prepareTerminalClickOrDrag(action)
	case regionPaneDivider:
		// Start drag for pane resizing
		p.mouseHandler.StartDrag(action.X, action.Y, regionPaneDivider, p.sidebarWidth)
	case regionDiffTabDivider:
		// Start drag from the width the divider actually occupies in the tab box.
		view := p.activeDiffView()
		leafW := p.width
		if box, ok := p.diffDividerBox(); ok {
			leafW = box.W
		}
		startWidth := view.EffectiveListWidth(leafW)
		view.SetListWidth(startWidth)
		p.mouseHandler.StartDrag(action.X, action.Y, regionDiffTabDivider, startWidth)
	case regionTermPanelContent:
		// The setter thaws the panel's window, which is what this arm did by
		// hand: a click is an explicit navigation of the surface it lands on.
		p.focusTermPanel()
		if !action.Shift && !action.Alt {
			if cmd, ok := p.activateTerminalLink(action); ok {
				return cmd
			}
		}
		return p.prepareTerminalClickOrDrag(action)
	case regionPaneClose:
		return p.clickPaneClose(action.Region.Data)
	case regionPaneTitle:
		// The title of a pane with no sidebar row is where its rename lives.
		// Focus has already moved: FocusLeafAt answers from geometry.
		return p.clickPaneTitle(action.Region.Data)
	case regionDocLink:
		hit, ok := action.Region.Data.(docContentLinkHit)
		if !ok {
			return nil
		}
		p.focusLeaf(hit.LeafID)
		if action.Shift || action.Alt {
			return p.pressPaneSelection(hit.LeafID, action)
		}
		return p.activateDocContentLink(hit)
	case regionDocTab:
		return p.clickDocTab(action.Region.Data)
	case regionIssueTab:
		return p.clickIssueTab(action.Region.Data)
	case regionNoteTab:
		return p.clickNoteTab(action.Region.Data)
	case regionResourceTab:
		return p.clickResourceTab(action.Region.Data)
	case regionDiffTargetTab:
		return p.clickDiffTab(action.Region.Data)
	case regionPaneLeaf:
		leafID, ok := action.Region.Data.(int)
		if !ok {
			return nil
		}
		leaf := FindPane(p.paneRoot, leafID)
		if leaf == nil || leaf.Split != nil || leaf.Kind == PaneTerminal {
			return nil
		}
		p.focusLeaf(leafID)
		if leaf.Kind == PaneDoc {
			// A press over the document's text arms a selection; a release
			// without motion is still the click that just focused the pane.
			return p.pressPaneSelection(leafID, action)
		}
		if leaf.Kind == PaneResource {
			// The card is passive: a provider document has no clickable
			// targets, so the press only arms a selection and the click that
			// just focused the leaf is the whole of what a release without
			// motion means.
			return p.pressPaneSelection(leafID, action)
		}
		if leaf.Kind == PaneDiff {
			return nil
		}
		issue, _ := p.issueLeafAt(leafID)
		if issue == nil {
			return nil
		}
		view := issue.view()
		if view == nil {
			return nil
		}
		lx, ly := issueViewLocal(action.X, action.Y, action.Region.Rect)
		if view.SelectableAt(lx, ly) {
			// The card's own targets — a parent row, a subtask row, its bar —
			// keep their clicks; everything else in the body is text, and a
			// press over it arms a selection.
			return p.pressPaneSelection(leafID, action)
		}
		beforeActive := issue.tabs.Active
		beforeID, beforeScroll := view.IssueID(), view.ScrollOffset()
		kind, cmd := view.HandleClick(lx, ly)
		if kind == issueview.HitScrollbar {
			// The card armed a scrollbar gesture (and did any track-click
			// jump itself). Start the shared handler's drag so motions come
			// back to ScrollbarDrag and the release anywhere settles it.
			p.issueScrollLeaf = leafID
			p.issueScrollTrackY = action.Y - ly
			p.mouseHandler.StartDrag(action.X, action.Y, regionIssueScrollbar, leafID)
		}
		after := issue.view()
		if issue.tabs.Active != beforeActive ||
			(after != nil && (after.IssueID() != beforeID || after.ScrollOffset() != beforeScroll)) {
			p.saveSelectionState()
		}
		return cmd
	case regionPaneTreeDivider:
		if splitID, ok := action.Region.Data.(int); ok {
			if split := FindPane(p.paneRoot, splitID); split != nil && split.Split != nil {
				p.paneDragSplitID = splitID
				p.mouseHandler.StartDrag(action.X, action.Y, regionPaneTreeDivider, split.Split.Ratio)
			}
		}
	case regionListFilter:
		// Clicking the filter row focuses the query, the same as `/`.
		p.focusSidebar()
		p.focusListFilter()
	case regionListFilterClear:
		// The × clears the query and leaves the caret in it, which is what the
		// filter-clear command does. It is registered only where it is drawn,
		// so this arm is unreachable while the query is empty.
		p.focusSidebar()
		p.focusListFilter()
		p.listFilter.Clear()
		return p.clampSelectionToFilter()
	case regionWorktreeItem:
		// Click on worktree or shell entry - select it
		if hit, ok := action.Region.Data.(nestedShellHit); ok {
			parent, shell := p.findNestedShell(hit.TmuxName)
			if shell != nil {
				if p.shellSelected || p.selectedNestedTmux != hit.TmuxName {
					p.selectNestedShell(parent, hit.TmuxName)
					p.resetPreviewScroll()
					p.exitInteractiveMode()
					p.saveSelectionState()
				}
				p.ensureVisible()
				p.focusSidebar()
				return p.loadSelectedContent()
			}
		}
		if idx, ok := action.Region.Data.(int); ok {
			if idx < 0 {
				// Shell entry clicked (negative index: -1 -> shells[0], -2 -> shells[1], etc.)
				shellIdx := -(idx + 1)
				if shellIdx >= 0 && shellIdx < len(p.shells) {
					if !p.shellSelected || p.selectedShellIdx != shellIdx || p.selectedNestedTmux != "" {
						p.selectTopShellAt(shellIdx)
						p.resetPreviewScroll()
						// Exit interactive mode when switching selection (td-fc758e88)
						p.exitInteractiveMode()
						p.saveSelectionState()
					}
					p.focusSidebar()
					return p.loadSelectedContent()
				}
			} else if idx >= 0 && idx < len(p.worktrees) {
				// Worktree clicked
				if p.shellSelected || p.selectedNestedTmux != "" || p.selectedIdx != idx {
					p.selectWorktreeAt(idx)
					p.resetPreviewScroll()
					// Exit interactive mode when switching selection (td-fc758e88)
					p.exitInteractiveMode()
					p.saveSelectionState()
				}
				p.ensureVisible()
				p.focusSidebar()
				return p.loadSelectedContent()
			}
		}
	case regionPreviewAction:
		return p.clickPreviewAction(action.Region.Data)
	case regionNoteScrollbarThumb, regionNoteScrollbarTrack:
		// A press on a note pane's bar grabs or jumps-to-spot before any pane
		// logic can run: the reverse-scanned hit map gave the bar its column,
		// and a scrollbar press is not a click on the pane beneath it.
		if hit, ok := action.Region.Data.(noteScrollbarHit); ok {
			p.focusLeaf(hit.LeafID)
			p.pressNoteScrollbar(hit.LeafID, action)
		}
	case regionKanbanScrollbar:
		// A press on a lane's bar grabs or jumps-to-spot before any card
		// logic can run: the reverse-scanned hit map gave the bar this
		// column, and a scrollbar press never selects a card underneath it.
		p.pressKanbanScrollbar(action)
	case regionKanbanCard:
		// Click on kanban card - select it
		if region, ok := action.Region.Data.(boardkanban.HitRegion); ok {
			p.kanban.HandlePointer(boardkanban.PointerClick, region)
			selection := p.kanban.Selection()
			oldShellSelected, oldShellIdx, oldWorktreeIdx := p.shellSelected, p.selectedShellIdx, p.selectedIdx
			p.kanbanCol, p.kanbanRow = selection.Column, selection.Row
			p.syncKanbanToList()
			p.applyKanbanSelectionChange(oldShellSelected, oldShellIdx, oldWorktreeIdx)
			return p.loadSelectedContent()
		}
	case regionKanbanColumn:
		// Click on column header - focus that column
		if region, ok := action.Region.Data.(boardkanban.HitRegion); ok {
			p.kanban.HandlePointer(boardkanban.PointerClick, region)
			selection := p.kanban.Selection()
			oldShellSelected, oldShellIdx, oldWorktreeIdx := p.shellSelected, p.selectedShellIdx, p.selectedIdx
			p.kanbanCol, p.kanbanRow = selection.Column, selection.Row
			p.syncKanbanToList()
			if p.applyKanbanSelectionChange(oldShellSelected, oldShellIdx, oldWorktreeIdx) {
				return p.loadSelectedContent()
			}
		}
	case regionViewToggle:
		// Click on view toggle - switch views
		if idx, ok := action.Region.Data.(int); ok {
			if idx == 0 {
				p.viewMode = ViewModeList
			} else {
				p.viewMode = ViewModeKanban
				p.syncListToKanban()
			}
		}
	case regionDiffTabFile, regionDiffTabCommit, regionDiffTabDiffPane, regionDiffTabMinimap,
		regionCommitFileBack, regionCommitFileItem, regionCommitFileDiffPane,
		regionDiffTabPreviewFile, regionDiffTabFileListPane:
		p.activePane = PanePreview
		view := p.activeDiffView()
		if view == nil {
			return nil
		}
		cmd := view.HandleClick(action.Region.ID, action.Region.Data)
		if isDiffTextRegion(action.Region.ID) {
			// The patch body and the pane behind it are text. The click that
			// focused the pane has already happened above; the press also arms
			// a selection so the motion after it selects.
			if _, leaf := p.activeDiffPane(); leaf != nil {
				return tea.Batch(cmd, p.pressPaneSelection(leaf.ID, action))
			}
		}
		if action.Region.ID == regionDiffTabMinimap && p.fullFileDiff != nil {
			clickRow := action.Y - action.Region.Rect.Y
			totalLines := p.fullFileDiff.TotalLines()
			contentHeight := view.Height()
			if contentHeight < 1 {
				contentHeight = 1
			}
			mmH := contentHeight
			if totalLines < mmH {
				mmH = totalLines
			}
			view.DiffScroll = gitstatus.MinimapScrollTarget(clickRow, mmH, totalLines, contentHeight)
		}
		return cmd
	case regionTaskLinkDropdown:
		// Click on task link dropdown item
		if idx, ok := action.Region.Data.(int); ok {
			if idx >= 0 && idx < len(p.taskSearchFiltered) && p.linkingWorktree != nil {
				task := p.taskSearchFiltered[idx]
				wt := p.linkingWorktree
				p.viewMode = ViewModeList
				p.linkingWorktree = nil
				return p.linkTask(wt, task.ID)
			}
		}
	}
	return nil
}

// handleMouseDoubleClick handles double-click events.
func (p *Plugin) handleMouseDoubleClick(action mouse.MouseAction) tea.Cmd {
	// Guard: ignore double-clicks when a modal is open (td-f63097).
	if p.isModalViewMode() {
		return nil
	}
	if action.Region == nil {
		return nil
	}

	p.notePressAwayFromTerminal(action)
	if cmd, ok := p.clickPaneCloseAt(action.X, action.Y); ok {
		return cmd
	}
	// A rapid second press on the sidebar's bar re-grabs it exactly like the
	// first one did (thumb grab continues, track re-jumps) instead of being
	// dropped for not naming a row.
	if isListScrollbarID(action.Region.ID) {
		p.pressListScrollbar(action)
		return nil
	}
	// The same parity for a note pane's bar: a rapid second press re-arms the
	// gesture instead of being absorbed as a stray activation.
	if isNoteScrollbarDragID(action.Region.ID) {
		if hit, ok := action.Region.Data.(noteScrollbarHit); ok {
			p.focusLeaf(hit.LeafID)
			p.pressNoteScrollbar(hit.LeafID, action)
		}
		return nil
	}
	// The same parity for a terminal surface's bar: a rapid second press
	// re-grabs or re-jumps instead of falling through to the pane under it.
	if isTermScrollbarDragID(action.Region.ID) {
		if _, ok := action.Region.Data.(terminalScrollbarHit); ok {
			p.pressTerminalScrollbar(action)
		}
		return nil
	}
	// The same parity for a lane bar: a rapid second press re-arms the
	// gesture instead of falling through to card activation.
	if action.Region.ID == regionKanbanScrollbar {
		p.pressKanbanScrollbar(action)
		return nil
	}
	if cmd, ok := p.clickDocTabAt(action.X, action.Y); ok {
		return cmd
	}
	if cmd, ok := p.clickIssueTabAt(action.X, action.Y); ok {
		return cmd
	}
	if cmd, ok := p.clickNoteTabAt(action.X, action.Y); ok {
		return cmd
	}

	switch action.Region.ID {
	case regionTermPanelContent:
		p.activePane = PanePreview
		p.setShellLeafFocused(true)
		return p.selectTerminalWord(action)
	case regionPreviewPane:
		p.setShellLeafFocused(false)
		return p.selectTerminalWord(action)
	case regionWorktreeItem:
		// Double-click on worktree or shell - attach to tmux session if exists
		if hit, ok := action.Region.Data.(nestedShellHit); ok {
			parent, shell := p.findNestedShell(hit.TmuxName)
			if shell != nil {
				p.selectNestedShell(parent, hit.TmuxName)
				p.saveSelectionState()
				if !fullTmuxAttachEnabled() {
					return nil
				}
				return p.ensureShellAndAttach(shell)
			}
		}
		if idx, ok := action.Region.Data.(int); ok {
			if idx < 0 {
				// Double-click on shell entry (negative index: -1 -> shells[0], -2 -> shells[1], etc.)
				shellIdx := -(idx + 1)
				if shellIdx >= 0 && shellIdx < len(p.shells) {
					p.selectTopShellAt(shellIdx)
					p.saveSelectionState()
					if !fullTmuxAttachEnabled() {
						return nil
					}
					return p.ensureShellAndAttachByIndex(shellIdx)
				}
			} else if idx >= 0 && idx < len(p.worktrees) {
				p.selectWorktreeAt(idx)
				p.saveSelectionState()
				wt := p.worktrees[idx]
				if wt.Agent != nil && fullTmuxAttachEnabled() {
					p.attachedSession = wt.Name
					return p.AttachToSession(wt)
				}
				p.activePane = PanePreview
			}
		}
	case regionDiffTabFile, regionDiffTabCommit, regionCommitFileItem, regionDiffTabPreviewFile:
		p.activePane = PanePreview
		view := p.activeDiffView()
		if view == nil {
			return nil
		}
		return view.HandleDoubleClick(action.Region.ID, action.Region.Data)
	case regionPaneLeaf:
		if leafID, ok := action.Region.Data.(int); ok && p.docSelectionView(leafID) != nil {
			// Word by double click, line by triple, exactly as the terminal
			// beside it answers the same gesture.
			p.focusLeaf(leafID)
			return p.pressPaneSelection(leafID, action)
		}
		if issue, leaf := p.issueLeafAt(action.Region.Data); leaf != nil {
			p.focusLeaf(leaf.ID)
			// Bubble Tea emits the first click and then a double-click event at
			// the same cell. The first click is the sole issue navigation;
			// replaying it here can open the child and then its newly rendered
			// parent when both rows occupy the same coordinate. A rapid second
			// press on the card's bar re-arms the gesture instead, through the
			// seam that can never reach a nav row.
			if view := issue.view(); view != nil {
				lx, ly := issueViewLocal(action.X, action.Y, action.Region.Rect)
				if view.PressScrollbar(lx, ly) {
					p.issueScrollLeaf = leaf.ID
					p.issueScrollTrackY = action.Y - ly
					p.mouseHandler.StartDrag(action.X, action.Y, regionIssueScrollbar, leaf.ID)
				}
			}
			return nil
		}
	case regionKanbanCard:
		// Double-click on kanban card - attach to tmux session if agent running
		if region, ok := action.Region.Data.(boardkanban.HitRegion); ok {
			event := p.kanban.HandlePointer(boardkanban.PointerDoubleClick, region)
			if event.Kind != boardkanban.ActionActivated {
				return nil
			}
			selection := p.kanban.Selection()
			col, row := selection.Column, selection.Row
			oldShellSelected, oldShellIdx, oldWorktreeIdx := p.shellSelected, p.selectedShellIdx, p.selectedIdx
			p.kanbanCol, p.kanbanRow = col, row
			p.syncKanbanToList()
			p.applyKanbanSelectionChange(oldShellSelected, oldShellIdx, oldWorktreeIdx)
			if !fullTmuxAttachEnabled() {
				return nil
			}
			if shell := p.selectedKanbanShell(); shell != nil {
				return p.ensureShellAndAttachByIndex(p.selectedShellIdx)
			}
			if wt := p.selectedKanbanWorktree(); wt != nil && wt.Agent != nil {
				p.attachedSession = wt.Name
				return p.AttachToSession(wt)
			}
		}
	}
	return nil
}

func (p *Plugin) handleMouseTripleClick(action mouse.MouseAction) tea.Cmd {
	if p.isModalViewMode() || action.Region == nil {
		return nil
	}
	p.notePressAwayFromTerminal(action)
	switch action.Region.ID {
	case regionTermPanelContent:
		p.activePane = PanePreview
		p.setShellLeafFocused(true)
		return p.selectTerminalLine(action)
	case regionPreviewPane:
		p.activePane = PanePreview
		p.setShellLeafFocused(false)
		return p.selectTerminalLine(action)
	}
	return p.handleMouseDoubleClick(action)
}

// handleMouseScroll handles scroll wheel events.
func (p *Plugin) handleMouseScroll(action mouse.MouseAction) tea.Cmd {
	// Guard: absorb background region scrolls when a modal is open (td-f63097).
	if p.isModalViewMode() && (action.Region == nil || isBackgroundRegion(action.Region.ID)) {
		return nil
	}

	// A wheel action always carries its distance in lines (mouse.WheelScrollLines
	// per notch); a second normalization here would be a second answer to how far
	// one notch travels.
	delta := action.Delta

	// Determine which pane based on region or position
	regionID := ""
	if action.Region != nil {
		regionID = action.Region.ID
	}

	// Whether a notch is placed by region or stays with the pointer is the shared
	// rule's answer, argued there.
	if tty.WheelStaysWithPointer(p.viewMode == ViewModeInteractive) {
		return p.wheelTerminal(p.interactiveTermPanel(), action, delta)
	}

	switch regionID {
	case regionSidebar, regionWorktreeItem:
		return p.scrollSidebar(delta)
	case regionPaneLeaf, regionDocLink, regionDocTab, regionIssueTab, regionNoteTab, regionResourceTab, regionDiffTargetTab, regionPaneClose,
		regionNoteScrollbarThumb, regionNoteScrollbarTrack:
		leafID := 0
		switch data := action.Region.Data.(type) {
		case int:
			leafID = data
		case docTabHit:
			leafID = data.LeafID
		case docContentLinkHit:
			leafID = data.LeafID
		case issueTabHit:
			leafID = data.LeafID
		case noteTabHit:
			leafID = data.LeafID
		case resourceTabHit:
			leafID = data.LeafID
		case diffTabHit:
			leafID = data.LeafID
		case noteScrollbarHit:
			// A notch on a note pane's bar scrolls that pane, exactly as it
			// did when the bar's column answered as the leaf body.
			leafID = data.LeafID
		}
		leaf := FindPane(p.paneRoot, leafID)
		if leaf == nil {
			return nil
		}
		switch leaf.Kind {
		case PaneDoc:
			if doc := p.docs[leaf.ContentID]; doc != nil {
				if view := doc.view(); view != nil {
					before := view.ScrollOffset()
					view.Scroll(delta)
					if view.ScrollOffset() != before {
						p.saveSelectionState()
					}
				}
			}
		case PaneIssue:
			// The issue component scrolls in rendered rows, the same units the
			// document viewer answers a notch in, so the wheel reaches it by
			// the same path rather than a second one — and through the same
			// burst guard, or mid-range inertia would repaint the card once per
			// notch instead of once per earned flush.
			if issue := p.issues[leaf.ContentID]; issue != nil {
				if view := issue.view(); view != nil {
					flushed, ok := issue.wheel.Add(delta, p.now())
					if !ok {
						return nil
					}
					before := view.ScrollOffset()
					view.Scroll(flushed)
					if view.ScrollOffset() != before {
						p.saveSelectionState()
					}
				}
			}
		case PaneNote:
			if note := p.notes[leaf.ContentID]; note != nil {
				if view := note.view(); view != nil {
					before := view.ScrollOffset()
					view.Scroll(delta)
					if view.ScrollOffset() != before {
						p.saveSelectionState()
					}
				}
			}
		case PaneDiff:
			if view := p.activeDiffView(); view != nil {
				view.ScrollContent(delta, view.Height())
			}
		case PaneResource:
			// The shared pane scrolls and persists together, so a notch and
			// the arrow keys leave the same offset behind.
			if res := p.resources[leaf.ContentID]; res != nil {
				res.pane.Scroll(delta)
			}
		}
		return nil
	case regionTermPanelContent, regionPaneTitle:
		// Scroll the panel under the pointer, whether or not it holds focus.
		// The title region sits on the panel's own header row and is a press
		// target only: a notch over it belongs to the pane under it.
		// Who owns the notch — the application in the pane or this window — is
		// the shared rule's answer, and it is the same answer here as when the
		// panel holds the keyboard.
		return p.wheelTerminal(true, action, delta)
	case regionDiffTabFile, regionDiffTabCommit, regionDiffTabFileListPane,
		regionDiffTabPreviewFile, regionDiffTabDiffPane, regionDiffTabMinimap,
		regionCommitFileItem, regionCommitFileBack, regionCommitFileDiffPane:
		if view := p.activeDiffView(); view != nil {
			return view.HandleWheel(regionID, delta)
		}
		return nil
	case regionPreviewPane:
		return p.wheelPreview(action, delta)
	case regionKanbanCard, regionKanbanColumn, regionKanbanScrollbar:
		// Scroll the lane under the pointer, not whichever lane happened to
		// have keyboard focus before the wheel gesture. A notch on a lane's
		// bar names that same lane through its region data.
		if region, ok := action.Region.Data.(boardkanban.HitRegion); ok {
			return p.scrollKanbanColumn(region.Column, delta)
		}
		return p.scrollKanban(delta)
	default:
		// Fallback based on X position and view mode
		if p.viewMode == ViewModeKanban {
			return p.scrollKanban(delta)
		}
		split := p.previewSplit()
		if p.sidebarVisible && action.X < split.SidebarWidth {
			return p.scrollSidebar(delta)
		}
		return p.wheelPreview(action, delta)
	}
}

// wheelPreview places a notch that landed on the preview. A terminal drawn there
// is a pane and answers the pane's rule; anything else the preview shows is a
// document, scrolled by its own offset.
func (p *Plugin) wheelPreview(action mouse.MouseAction, delta int) tea.Cmd {
	p.releaseTerminalDocProjection(false)
	if p.previewShowsTerminal() {
		return p.wheelTerminal(false, action, delta)
	}
	return p.scrollPreview(delta)
}

// scrollSidebar scrolls the sidebar list (shells + worktrees).
func (p *Plugin) scrollSidebar(delta int) tea.Cmd {
	// Check if there's anything to scroll through
	if len(p.shells) == 0 && len(p.worktrees) == 0 {
		return nil
	}

	// Track old selection to detect change
	oldShellSelected := p.shellSelected
	oldShellIdx := p.selectedShellIdx
	oldWorktreeIdx := p.selectedIdx
	oldNested := p.selectedNestedTmux

	// Delegate to moveCursor which handles multi-shell navigation properly
	p.moveCursor(delta)

	// Check if selection actually changed
	selectionChanged := p.shellSelected != oldShellSelected ||
		p.selectedNestedTmux != oldNested ||
		(p.shellSelected && p.selectedShellIdx != oldShellIdx) ||
		(!p.shellSelected && p.selectedNestedTmux == "" && p.selectedIdx != oldWorktreeIdx)

	if selectionChanged {
		return p.loadSelectedContent()
	}
	return nil
}

// isListScrollbarID reports that a hit or drag belongs to the shared sidebar
// list's bar rather than to one of its rows or controls.
func isListScrollbarID(id string) bool {
	return id == string(workspacelist.RegionScrollbarThumb) ||
		id == string(workspacelist.RegionScrollbarTrack)
}

// pressListScrollbar begins the bar's gesture: grab the thumb where it was
// pressed, or jump-to-spot on the track and keep dragging from there. The bar's
// snapshot is what the last render reported and originY is where its content
// began inside the panel, so the pointer maps onto what was actually drawn.
func (p *Plugin) pressListScrollbar(action mouse.MouseAction) {
	bar := p.sidebarBar.bar
	if !bar.Has || action.Region == nil {
		return
	}
	kind := workspacelist.RegionKind(action.Region.ID)
	trackY := p.sidebarBar.originY + bar.TrackTop
	offset := p.sidebarBar.gesture.Press(bar, trackY, action.Y, kind == workspacelist.RegionScrollbarThumb, p.scrollOffset)
	p.setListViewport(offset)
	p.mouseHandler.StartDrag(action.X, action.Y, string(kind), offset)
}

// dragListScrollbar applies the gesture's offset mapping for the pointer row.
func (p *Plugin) dragListScrollbar(action mouse.MouseAction) {
	if !p.sidebarBar.gesture.Active() {
		return
	}
	p.setListViewport(p.sidebarBar.gesture.DragTo(action.Y))
}

// pressKanbanScrollbar begins a lane bar's gesture: grabbing the thumb where it
// was pressed, or jumping-to-spot on the track anchored there so the same
// gesture keeps dragging. The board component owns the mapping — the regions
// are what the last render reported and the lane travels in the region's
// Column — so this is only routing: arm, then start the shared handler's drag.
func (p *Plugin) pressKanbanScrollbar(action mouse.MouseAction) {
	if action.Region == nil {
		return
	}
	region, ok := action.Region.Data.(boardkanban.HitRegion)
	if !ok {
		return
	}
	if p.kanban.PressScrollbar(region, action.Y) {
		p.mouseHandler.StartDrag(action.X, action.Y, regionKanbanScrollbar, 0)
	}
}

// setListViewport pins the sidebar's viewport to an offset a scrollbar gesture
// chose, latching free-scroll mode: renders keep the chosen position even when
// the selected row sits outside it. Any selection move clears the latch through
// ensureVisible, and following resumes.
func (p *Plugin) setListViewport(offset int) {
	p.freeScroll = true
	p.scrollOffset = max(offset, 0)
}

// handleMouseHorizontalScroll handles horizontal scroll events (shift+wheel or trackpad).
func (p *Plugin) handleMouseHorizontalScroll(action mouse.MouseAction) tea.Cmd {
	if action.Region == nil {
		return nil
	}
	delta := action.Delta
	switch action.Region.ID {
	case regionDiffTabDiffPane, regionDiffTabMinimap, regionCommitFileDiffPane:
		p.diff.HorizScroll += delta
		if p.diff.HorizScroll < 0 {
			p.diff.HorizScroll = 0
		}
	}
	return nil
}

// scrollPreview scrolls the document the preview is showing. A document's offset
// is an absolute line from the top, so a notch up is a smaller one; a terminal's
// window counts back from its live bottom and is placed by the wheel rule
// instead.
func (p *Plugin) scrollPreview(delta int) tea.Cmd {
	p.releaseTerminalDocProjection(false)
	maxOffset := p.getMaxScrollOffset()
	p.previewOffset = min(max(p.previewOffset+delta, 0), maxOffset)
	return nil
}

// scrollKanban scrolls within the current Kanban column.
func (p *Plugin) scrollKanban(delta int) tea.Cmd {
	return p.scrollKanbanColumn(p.kanbanCol, delta)
}

func (p *Plugin) scrollKanbanColumn(column, delta int) tea.Cmd {
	p.syncKanbanComponent()
	oldSelection := p.kanban.Selection()
	p.kanban.MoveInColumn(column, delta)
	next := p.kanban.Selection()
	p.kanbanCol, p.kanbanRow = next.Column, next.Row
	if next != oldSelection {
		oldShellSelected := p.shellSelected
		oldShellIdx := p.selectedShellIdx
		oldWorktreeIdx := p.selectedIdx
		p.syncKanbanToList()
		p.applyKanbanSelectionChange(oldShellSelected, oldShellIdx, oldWorktreeIdx)
		return p.loadSelectedContent()
	}
	return nil
}

// handleMouseDrag handles drag motion events.
func (p *Plugin) handleMouseDrag(action mouse.MouseAction) tea.Cmd {
	// Guard: prevent pane resizing while a modal is open (td-f63097).
	if p.isModalViewMode() {
		return nil
	}

	dragRegion := p.mouseHandler.DragRegion()
	p.lastDragRegion = dragRegion // Save for handleMouseDragEnd (EndDrag clears before DragEnd)

	// The sidebar bar's gesture maps the pointer row through its press-time
	// snapshot; the shared core clamps past both ends of the track without
	// ending anything.
	if isListScrollbarID(dragRegion) {
		p.dragListScrollbar(action)
		return nil
	}

	// A lane bar's drag maps through the press-time snapshot the board
	// component holds; only the grabbed lane's offset moves, wherever the
	// pointer has since travelled.
	if dragRegion == regionKanbanScrollbar {
		p.kanban.DragScrollbar(action.Y)
		return nil
	}

	// A note pane's bar drag: the press-time snapshot of its params maps the
	// pointer onto the rows that were actually drawn.
	if isNoteScrollbarDragID(dragRegion) {
		p.dragNoteScrollbar(action.Y)
		return nil
	}

	// A terminal surface's bar drag: its own press-time snapshot maps the
	// pointer onto the window it armed on, wherever the pointer has since
	// travelled.
	if isTermScrollbarDragID(dragRegion) {
		p.dragTerminalScrollbar(action.Y)
		return nil
	}

	switch dragRegion {
	case regionIssueScrollbar:
		// An issue card's scrollbar gesture. The press-time snapshot of the
		// card's top row maps the pointer onto view-local rows; the shared
		// core clamps past both ends of the track.
		if issue := p.issues[p.issueScrollLeaf]; issue != nil {
			if view := issue.view(); view != nil {
				view.ScrollbarDrag(action.Y - p.issueScrollTrackY)
			}
		}
		return nil
	case regionPaneDivider:
		// Calculate new sidebar width based on drag
		startValue := p.mouseHandler.DragStartValue()
		p.sidebarWidth = workspacelist.ResizePercent(startValue, action.DragDX, p.width)
	case regionDiffTabDivider:
		view := p.activeDiffView()
		leafW := p.width
		if box, ok := p.diffDividerBox(); ok {
			leafW = box.W
		}
		view.SetListWidth(p.mouseHandler.DragStartValue())
		view.ApplyListWidthDelta(action.DragDX, leafW)
	case regionPaneTreeDivider:
		split := FindPane(p.paneRoot, p.paneDragSplitID)
		peer, ok := p.previewPeerBox()
		if split == nil || split.Split == nil || !ok {
			return nil
		}
		startValue := p.mouseHandler.DragStartValue()
		newRatio := startValue
		if split.Split.Axis == SplitRows && peer.H > 0 {
			newRatio += action.DragDY * 100 / peer.H
		} else if split.Split.Axis == SplitCols && peer.W > 0 {
			newRatio += action.DragDX * 100 / peer.W
		}
		SetRatio(p.paneRoot, p.paneDragSplitID, newRatio)
		if p.contentDeck != nil {
			p.contentDeck.SetRatio(p.paneDragSplitID, newRatio)
		}
	case regionPaneLeaf:
		// A document selection. The leaf the gesture started in answers it,
		// wherever the pointer has since travelled.
		return p.dragPaneSelection(action)
	case regionPreviewPane, regionTermPanelContent:
		if p.terminalPointerIntent(mouse.ActionDrag, "", dragRegion, false) != tty.PointerDrag {
			return nil
		}
		if !p.selection.Anchor.Valid() && !p.anchorDragFromOrigin(action) {
			return nil
		}
		return p.handleInteractiveSelectionDrag(action)
	}
	return nil
}

// terminalPointerIntent asks the shared layer what a pointer action over this
// surface's terminals means. Which regions draw one is this plugin's to name;
// what the action means over them is not.
func (p *Plugin) terminalPointerIntent(action mouse.ActionType, region, gestureRegion string, lostRelease bool) tty.PointerIntent {
	terminal := func(id string) bool {
		return id == regionPreviewPane || id == regionTermPanelContent
	}
	return tty.PointerIntentFor(tty.PointerIntentInput{
		Action:       action,
		OverTerminal: terminal(region),
		FromTerminal: terminal(gestureRegion),
		LostRelease:  lostRelease,
	})
}

// handleMouseDragEnd handles the end of a drag operation.
func (p *Plugin) handleMouseDragEnd(action mouse.MouseAction) tea.Cmd {
	// Guard: ignore drag-end when a modal is open (td-f63097). The release is
	// swallowed, so drop the click resolution it would have carried out too —
	// the same boundary where the auto-scroll tick abandons its gesture.
	if p.isModalViewMode() {
		p.pointer.Resolution = tty.ClickNone
		// A document gesture is left holding a live drag at the same boundary,
		// and nothing else ends it: the handler has already closed the drag, so
		// the lost-release path never fires either.
		p.abandonPaneSelection()
		p.finishIssueScrollbarDrag()
		p.finishNoteScrollbarDrag()
		p.finishTerminalScrollbarDrag()
		p.sidebarBar.gesture.End()
		return nil
	}

	dragSource := action.DragStartID
	if dragSource == "" {
		dragSource = p.lastDragRegion
	}
	if dragSource == regionIssueScrollbar {
		// An issue scrollbar gesture settles wherever the pointer is; the
		// offset is view state and nothing persists it here.
		p.finishIssueScrollbarDrag()
		return nil
	}
	if dragSource == regionKanbanScrollbar {
		// A lane bar's gesture settles wherever the pointer left it; scroll
		// offsets are ephemeral view state and nothing persists them here.
		p.kanban.ReleaseScrollbar()
		return nil
	}
	if isNoteScrollbarDragID(dragSource) {
		p.finishNoteScrollbarDrag()
		return nil
	}
	if isTermScrollbarDragID(dragSource) {
		p.finishTerminalScrollbarDrag()
		return nil
	}
	if isListScrollbarID(dragSource) {
		// The sidebar bar's gesture settles wherever the pointer left it; the
		// offset is view state and nothing persists it here.
		p.sidebarBar.gesture.End()
		return nil
	}
	if dragSource == regionPaneLeaf {
		// A document selection ends here rather than in the width-persisting
		// switch below: nothing about a pane leaf is a divider.
		return p.finishPaneSelection(action)
	}
	if isDividerDragRegion(dragSource) {
		// Immediate resize on drop. Hold is released first so the flush is not
		// itself gated, then model and workspace retries are cancelled so a
		// leftover tick cannot fire a second SIGWINCH.
		p.setTerminalResizeHold(false)
		p.cancelDeferredPaneResize()
		p.resizeFlushImmediate = true
		defer func() { p.resizeFlushImmediate = false }()
	}
	if p.terminalPointerIntent(mouse.ActionDragEnd, "", dragSource, false) == tty.PointerFinish &&
		(p.selection.Anchor.Valid() || p.pointer.Resolution != tty.ClickNone) {
		return p.finishInteractiveSelection()
	}

	// Persist widths based on what was being dragged
	switch dragSource {
	case regionPaneTreeDivider:
		// A divider the user dragged is a preference: where they left the shell
		// split is where the next ctrl+t opens one.
		p.rememberShellSplit()
		p.saveSelectionState()
		p.paneDragSplitID = 0
		p.lastDragRegion = ""
		return p.resizeDocTerminalCmd()
	case regionDiffTabDivider:
		_ = state.SetDiffTabFileListWidth(p.activeDiffView().ListWidth())
	default:
		_ = state.SetWorkspaceSidebarWidth(p.sidebarWidth)
	}
	p.lastDragRegion = ""
	if p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active {
		// Poll captures cursor atomically - no separate query needed
		return tea.Batch(p.resizeInteractivePaneCmd(), p.pollInteractivePaneImmediate())
	}
	return p.resizeSelectedPaneCmd()
}
