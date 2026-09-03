package workspace

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/marcus/sidecar/internal/agentstatus"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/paneframe"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/terminalperf"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/workspacelist"
)

// Modal style functions - return fresh styles using current theme colors.
func inputStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(styles.BorderNormal).
		Padding(0, 1)
}

func inputFocusedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(styles.Primary).
		Padding(0, 1)
}

// Panel dimension constants for consistent width calculations. They are read
// from the shared frame rather than restated, so this plugin and the global
// Workspaces browser cannot disagree about what a panel border costs.
const (
	panelBorderWidth  = paneframe.BorderWidth  // Left + right border (1 each)
	panelPaddingWidth = paneframe.PaddingWidth // Left + right padding (1 each)
	panelOverhead     = paneframe.Overhead     // Total overhead: 4
)

// View renders the plugin UI.
func (p *Plugin) View(width, height int) string {
	terminalperf.Record(terminalperf.ProjectWorkspaceViewRendered)
	// Clear truncation cache if dimensions changed
	if p.width != width || p.height != height {
		p.truncateCache.Clear()
	}

	p.width = width
	p.height = height
	if p.reuseHeldWheelViewOnce && p.wheelViewCacheOK &&
		p.wheelViewCacheW == width && p.wheelViewCacheH == height {
		p.reuseHeldWheelViewOnce = false
		return p.wheelViewCache
	}
	p.reuseHeldWheelViewOnce = false

	// CRITICAL: Clear hit regions at start of each render
	p.mouseHandler.Clear()
	p.clearDocLinkHits()
	// Pane geometry is re-earned with the regions, and for the same reason: a
	// frame that does not draw the tree — the kanban board, a modal, a preview
	// that could not place one — must not leave last frame's leaf boxes
	// answering pointer hits.
	p.paneFrame, p.paneFrameDrawn = PaneLayout{}, false
	// A modal has taken the keyboard and the pointer. The selection behind it
	// goes with them: the release will never arrive, so the gesture would
	// otherwise answer the next unrelated drag as an extension of a selection
	// the user finished before the modal opened, and the highlight would sit
	// under a card nothing on screen still acts on.
	if p.isModalViewMode() {
		p.abandonPaneSelection()
		p.clearPaneSelectionsExcept(nil)
	}

	var view string
	switch p.viewMode {
	case ViewModeCreate:
		view = p.renderCreateModal(width, height)
	case ViewModeKanban:
		view = p.renderKanbanView(width, height)
	case ViewModeTaskLink:
		view = p.renderTaskLinkModal(width, height)
	case ViewModeMerge:
		view = p.renderMergeModal(width, height)
	case ViewModeAgentConfig:
		view = p.renderAgentConfigModal(width, height)
	case ViewModeAgentChoice:
		view = p.renderAgentChoiceModal(width, height)
	case ViewModeConfirmDelete:
		view = p.renderConfirmDeleteModal(width, height)
	case ViewModeConfirmDeleteShell:
		view = p.renderConfirmDeleteShellModal(width, height)
	case ViewModeConfirmCloseSplit:
		view = p.renderConfirmCloseSplitModal(width, height)
	case ViewModeCommitForMerge:
		view = p.renderCommitForMergeModal(width, height)
	case ViewModeRenameShell:
		view = p.renderRenameShellModal(width, height)
	case ViewModeRenameWorktree:
		view = p.renderRenameWorktreeModal(width, height)
	case ViewModeFetchPR:
		view = p.renderFetchPRModal(width, height)
	case ViewModeFilePicker:
		background := p.renderListView(width, height)
		view = p.renderFilePickerModal(background)
	default:
		view = p.renderListView(width, height)
		if p.docInfo != nil {
			view = ui.OverlayModal(view, p.docInfo.Render(width, height, p.mouseHandler), width, height)
		}
		if p.viewFlyoutActive() {
			view = p.overlayViewFlyout(view, width, height)
		}
	}
	if p.paneLayoutModal != nil {
		view = ui.OverlayModal(view, p.paneLayoutModal.Render(width, height, p.mouseHandler), width, height)
	}
	p.wheelViewCache = view
	p.wheelViewCacheW = width
	p.wheelViewCacheH = height
	p.wheelViewCacheOK = true
	return view
}

// registerPreviewActionRegions puts click targets over the Diff/Task action
// chips, taken from the same layout that drew them. A chip the header dropped
// for want of columns gets no region.
func (p *Plugin) registerPreviewActionRegions(split previewSplit) {
	// The placements come from the header this frame drew, not from a second
	// layout pass: the chips are right-aligned against the hints, so their
	// columns depend on hint text only the renderer has seen.
	placements := p.previewActionPlacements
	if len(placements) == 0 {
		return
	}
	row, originX := p.previewContentY(), split.ContentX
	if p.selectingShell() || p.selectedWorktree() != nil {
		surface := p.terminalSurfaceGeometry(false)
		if surface.OK {
			row, originX = surface.HeaderY, surface.X
		} else {
			// The terminal is not on screen (zoomed document). Do not paint
			// action chips on top of file tabs.
			if p.selectedWorktree() == nil || !p.selectedWorktree().IsMain {
				return
			}
		}
	}

	for i, placement := range placements {
		if !placement.Drawn {
			continue
		}
		hit := previewActionDiff
		if i > 0 {
			hit = previewActionTask
		}
		p.mouseHandler.HitMap.AddRect(regionPreviewAction,
			originX+placement.Col, row, placement.Width, 1, hit)
	}
}

// renderListView renders the main split-pane list view.
func (p *Plugin) renderListView(width, height int) string {
	// Pane height for panels (outer dimensions including borders)
	paneHeight := height
	if paneHeight < 4 {
		paneHeight = 4
	}

	// Inner content height (excluding borders and header lines)
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	// The sidebar/divider/preview column arithmetic lives in one place so the
	// render path, the cursor path and the hit tests cannot drift (td-73fa86).
	split := p.previewSplitFor(width)

	// If sidebar is hidden, show only preview pane at full width
	if !p.sidebarVisible {
		// Register hit region for full-width preview (uses outer dimensions)
		p.mouseHandler.HitMap.AddRect(regionPreviewPane, 0, 0, split.PreviewWidth, paneHeight, nil)
		p.registerTerminalScrollbarHits(false)

		if p.docVisible() {
			// Multi-leaf: each leaf is its own panel. Do not wrap the peer again.
			previewContent := p.renderPreviewContent(split.PreviewWidth, paneHeight)
			p.registerPreviewActionRegions(split)
			p.registerStartAgentButton(split)
			return previewContent
		}

		// Render content using calculated content width (consistent with panel overhead)
		previewContent := p.renderPreviewContent(split.ContentWidth, innerHeight)
		p.registerPreviewActionRegions(split)
		p.registerStartAgentButton(split)

		if p.previewFlashActive() {
			return styles.RenderPanelWithGradient(previewContent, split.PreviewWidth, paneHeight, styles.GetFlashGradient())
		}
		return styles.RenderPanel(previewContent, split.PreviewWidth, paneHeight, true)
	}

	sidebarW := split.SidebarWidth
	previewW := split.PreviewWidth
	sidebarContentW := split.SidebarContentWidth
	previewContentW := split.ContentWidth

	// Determine pane focus state
	sidebarActive := p.activePane == PaneSidebar
	previewActive := p.activePane == PanePreview

	// Register hit regions (order matters: last = highest priority)
	// 1. Pane regions (lowest priority - fallback for scroll)
	p.mouseHandler.HitMap.AddRect(regionSidebar, 0, 0, sidebarW, paneHeight, nil)
	p.mouseHandler.HitMap.AddRect(regionPreviewPane, split.PreviewX, 0, previewW, paneHeight, nil)
	p.registerTerminalScrollbarHits(false)

	// 2. Divider region (high priority - for drag)
	p.mouseHandler.HitMap.AddRect(regionPaneDivider, sidebarW, 0, dividerHitWidth, paneHeight, nil)

	// Render content for each pane using pre-calculated content widths
	sidebarContent := p.renderSidebarContent(sidebarContentW, innerHeight)
	var previewContent string
	if p.docVisible() {
		// Multi-leaf: the preview peer is a canvas of leaf panels, not one
		// outer RenderPanel wrapping inner frames.
		previewContent = p.renderPreviewContent(previewW, paneHeight)
	} else {
		previewContent = p.renderPreviewContent(previewContentW, innerHeight)
	}

	// Preview tabs are registered after document bodies and their divider, so
	// the visible chips remain the highest-priority targets.
	p.registerPreviewActionRegions(split)
	p.registerStartAgentButton(split)

	flashActive := p.previewFlashActive()

	// Apply gradient border styles
	leftPane := styles.RenderPanel(sidebarContent, sidebarW, paneHeight, sidebarActive)

	var rightPane string
	if p.docVisible() {
		rightPane = previewContent
	} else if p.viewMode == ViewModeInteractive {
		// Use interactive gradient when in interactive mode (td-70aed9)
		rightPane = styles.RenderPanelWithGradient(previewContent, previewW, paneHeight, styles.GetInteractiveGradient())
	} else if flashActive && previewActive {
		rightPane = styles.RenderPanelWithGradient(previewContent, previewW, paneHeight, styles.GetFlashGradient())
	} else {
		rightPane = styles.RenderPanel(previewContent, previewW, paneHeight, previewActive)
	}

	divider := ui.RenderHandle(paneHeight, true, p.dividerHandleState(regionPaneDivider, 0))

	// Join horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, divider, rightPane)
}

// renderWorktreeItem renders a single worktree list item.
func (p *Plugin) renderWorktreeItem(wt *Worktree, selected bool, width int) string {
	return p.renderWorktreeItemKind(wt, selected, width, "")
}

func (p *Plugin) renderWorktreeSidebarItem(wt *Worktree, selected bool, width int) string {
	return p.renderWorktreeItemKind(wt, selected, width, workspacelist.KindWorktree)
}

func (p *Plugin) renderWorktreeItemKind(wt *Worktree, selected bool, width int, kind string) string {
	resolvedStatus := agentStatusPresentation(wt)
	activityIcon, activityText, activityStyle, hasActivity := p.animatedActivityPresentation(wt.Agent)
	marker := workspacelist.RowMarker{}
	switch {
	case resolvedStatus.Health:
		marker.Icon, marker.Tone = resolvedStatus.Icon, healthMarkerTone(resolvedStatus.Icon)
	case hasActivity:
		marker.Icon, marker.Style, marker.HasStyle = activityIcon, activityStyle, true
	case wt.IsMain:
		marker.Icon, marker.Tone = "◉", workspacelist.MarkerMain
	default:
		marker.Icon = wt.Status.Icon()
		marker.Tone = worktreeMarkerTone(wt.Status)
	}

	hasConflict := p.hasConflict(wt.IdentityKey(), p.conflicts)
	hasPR := wt.PRURL != ""
	name := wt.Name
	if !wt.IsMain && p.ctx.Config != nil && p.ctx.Config.Plugins.Workspace.SidebarDisplay.HideRepoPrefix {
		repoName := filepath.Base(p.ctx.ProjectRoot)
		if repoName != "" && strings.HasPrefix(name, repoName+"-") {
			name = name[len(repoName)+1:]
		}
	}

	var sdCfg config.SidebarDisplayConfig
	if p.ctx.Config != nil {
		sdCfg = p.ctx.Config.Plugins.Workspace.SidebarDisplay
	}
	statsStr := ""
	if !sdCfg.HideStats && wt.Stats != nil && (wt.Stats.Additions > 0 || wt.Stats.Deletions > 0) {
		statsStr = fmt.Sprintf("+%d -%d", wt.Stats.Additions, wt.Stats.Deletions)
	}

	nameMeta := make([]workspacelist.RowField, 0, 3)
	if hasPR {
		nameMeta = append(nameMeta, workspacelist.RowField{Text: " PR", Rendered: lipgloss.NewStyle().Foreground(styles.Secondary).Render(" PR")})
	}
	if hasConflict {
		nameMeta = append(nameMeta, workspacelist.RowField{Text: " ⚠", Rendered: styles.StatusModified.Render(" ⚠")})
	}
	if wt.IsOrphaned {
		nameMeta = append(nameMeta, workspacelist.RowField{Text: " ⚠", Rendered: styles.StatusModified.Render(" ⚠")})
	}

	before := make([]workspacelist.RowField, 0, 8)
	for _, label := range p.worktreeStateLabels(wt) {
		before = append(before, workspacelist.RowField{Text: label, Rendered: dimText(label)})
	}
	provider := ""
	if wt.IsMain {
		before = append(before, workspacelist.PlainField(wt.Branch))
	} else if !sdCfg.HideAgent {
		if wt.Agent != nil {
			provider = string(wt.Agent.Type)
		} else if wt.ChosenAgentType != "" && wt.ChosenAgentType != AgentNone {
			provider = string(wt.ChosenAgentType)
		} else {
			before = append(before, workspacelist.PlainField("—"))
		}
	}
	after := make([]workspacelist.RowField, 0, 6)
	if hasActivity {
		after = append(after, workspacelist.RowField{Text: activityText, Rendered: dimText(activityText)})
	}
	if !sdCfg.HideTask && wt.TaskID != "" {
		after = append(after, workspacelist.PlainField(wt.TaskID))
	}
	if statsStr != "" {
		after = append(after, workspacelist.PlainField(statsStr))
	}
	if hasConflict {
		conflictFiles := p.getConflictingFiles(wt.IdentityKey(), p.conflicts)
		if len(conflictFiles) > 0 {
			label := fmt.Sprintf("⚠ %d dirty overlaps", len(conflictFiles))
			after = append(after, workspacelist.RowField{Text: label, Rendered: styles.StatusModified.Render(label)})
		}
	}
	if wt.IsOrphaned {
		after = append(after, workspacelist.RowField{Text: "⚠ session ended", Rendered: styles.StatusModified.Render("⚠ session ended")})
	}
	if badge := p.worktreeRowBadge(wt); badge != "" {
		nameMeta = append(nameMeta, workspacelist.RowField{Text: " " + badge, Rendered: styles.Muted.Render(" " + badge)})
	}
	lines := workspacelist.RenderRow(workspacelist.RowPresentation{
		Marker: marker, Kind: kind, Name: name, Age: worktreeAge(wt), NameMeta: nameMeta,
		BeforeProvider: before, Provider: provider, AfterProvider: after,
	}, width, selected, selected && p.activePane == PaneSidebar)
	return strings.Join(lines, "\n")
}

func (p *Plugin) worktreeStateLabels(wt *Worktree) []string {
	if wt == nil {
		return nil
	}
	labels := make([]string, 0, 8)
	switch {
	case wt.IsMain:
		labels = append(labels, "main")
	case wt.IsBare:
		labels = append(labels, "bare")
	case wt.IsDetached || wt.Branch == "(detached)":
		labels = append(labels, "detached")
	default:
		labels = append(labels, "branch "+wt.Branch)
	}
	if wt.IsLocked {
		labels = append(labels, "locked · actions unavailable")
	}
	if wt.IsMissing {
		labels = append(labels, "folder missing · actions unavailable")
	} else if wt.IsPrunable {
		labels = append(labels, "prunable · actions unavailable")
	}
	if p.activeLifecycleOperationID != "" && ((p.mergeState != nil && p.mergeState.Worktree != nil && p.mergeState.Worktree.IdentityKey() == wt.IdentityKey()) || (p.createPlan != nil && p.createPlan.Path == wt.Path)) {
		labels = append(labels, "operation in progress")
	}
	if wt.SetupWarning != "" {
		labels = append(labels, "setup warning: "+wt.SetupWarning)
	}
	if wt.PRState != "" {
		labels = append(labels, "PR "+wt.PRState)
	} else if wt.PRURL != "" {
		labels = append(labels, "PR unavailable")
	}
	if wt.Changes != nil {
		switch wt.Changes.State {
		case LoadStateError:
			labels = append(labels, "diff error")
		case LoadStateTruncated:
			labels = append(labels, "diff truncated")
		default:
			if wt.Changes.Truncated {
				labels = append(labels, "diff truncated")
			}
		}
	}
	return labels
}

// renderShellEntryForSession renders a shell entry for a specific shell session.
func (p *Plugin) renderShellEntryForSession(shell *ShellSession, selected bool, width int) string {
	// Top-level shells carry the same kind glyph as nested ones and as every
	// shell in the global list. Without it a shell row was the only row in
	// either sidebar with no kind, and its second line hung three columns in
	// while every worktree's hung five — two grammars in one list.
	return p.renderShellEntry(shell, selected, width, 0, workspacelist.KindShell, "")
}

// renderNestedShellEntry draws a shell as a child of its worktree. Only the
// structural order does this; see sidebar_sort.go.
func (p *Plugin) renderNestedShellEntry(shell *ShellSession, selected bool, width int) string {
	return p.renderShellEntry(shell, selected, width, 2, workspacelist.KindShell, "")
}

// renderPeerShellEntry draws a shell that lives in a worktree as a peer of it,
// which is what a computed order requires. The worktree name takes the place
// the global list gives the project name: it is the context that stops "Shell
// 2" from being ambiguous once the row no longer sits under its parent.
func (p *Plugin) renderPeerShellEntry(shell *ShellSession, wt *Worktree, selected bool, width int) string {
	prefix := ""
	if wt != nil {
		prefix = wt.Name + " "
	}
	return p.renderShellEntry(shell, selected, width, 0, workspacelist.KindShell, prefix)
}

func (p *Plugin) renderShellEntry(shell *ShellSession, selected bool, width int, indent int, kind, namePrefix string) string {
	if indent > 0 {
		width = max(1, width-indent)
	}
	content := p.renderShellEntryKind(shell, selected, width, kind, namePrefix)
	if indent <= 0 {
		return content
	}
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = pad + line
	}
	return strings.Join(lines, "\n")
}

func (p *Plugin) renderShellEntryKind(shell *ShellSession, selected bool, width int, kind, namePrefix string) string {
	resolvedStatus := shellAgentStatusPresentation(shell)
	activityIcon, activityText, activityStyle, hasActivity := p.animatedActivityPresentation(shell.Agent)
	marker := workspacelist.RowMarker{}
	switch {
	case resolvedStatus.Health:
		marker.Icon, marker.Tone = resolvedStatus.Icon, healthMarkerTone(resolvedStatus.Icon)
	case hasActivity:
		marker.Icon, marker.Style, marker.HasStyle = activityIcon, activityStyle, true
	case shell.Agent != nil:
		marker.Icon, marker.Tone = "◎", workspacelist.MarkerLive
	default:
		marker.Icon, marker.Tone = "○", workspacelist.MarkerMuted
	}

	provider := AgentNone
	status := "no session"
	switch {
	case resolvedStatus.Health:
		provider, status = shell.ChosenAgent, "offline"
	case shell.Agent != nil:
		provider = liveShellProvider(shell)
		if hasActivity {
			status = activityText
		} else {
			status = "live"
		}
	case shell.ChosenAgent != AgentNone && shell.ChosenAgent != "":
		provider, status = shell.ChosenAgent, "stopped"
	}
	before := []workspacelist.RowField(nil)
	if provider == AgentNone || provider == "" {
		before = append(before, workspacelist.RowField{Text: "shell", Rendered: dimText("shell")})
	}
	after := []workspacelist.RowField{{Text: status, Rendered: dimText(status)}}
	var nameMeta []workspacelist.RowField
	if badge, hasBadge := p.pendingViewBadge(shell.TmuxName); hasBadge {
		nameMeta = append(nameMeta, workspacelist.RowField{Text: badge, Rendered: styles.Muted.Render(badge)})
	}
	// The layout glyph joins the metadata the row already carries. A split
	// workspace stays one row: the panes are what the badge is for.
	if badge := p.shellRowBadge(shell); badge != "" {
		nameMeta = append(nameMeta, workspacelist.RowField{Text: " " + badge, Rendered: styles.Muted.Render(" " + badge)})
	}
	prefix := workspacelist.RowField{}
	if namePrefix != "" {
		prefix = workspacelist.RowField{Text: namePrefix, Rendered: styles.Muted.Render(namePrefix)}
	}
	lines := workspacelist.RenderRow(workspacelist.RowPresentation{
		Marker: marker, Kind: kind, Name: shell.Name, NamePrefix: prefix, Age: shellAge(shell),
		NameMeta: nameMeta, BeforeProvider: before,
		Provider: string(provider), AfterProvider: after,
	}, width, selected, selected && p.activePane == PaneSidebar)
	return strings.Join(lines, "\n")
}

// worktreeAge is the freshness a worktree row reports. A live agent's last
// output wins: it moves when work actually happens, including work several
// directories deep that never touches the worktree root's timestamp. Without a
// session there is nothing to observe but the directory itself.
func worktreeAge(wt *Worktree) string { return formatRelativeTime(worktreeChangedAt(wt)) }

// worktreeChangedAt is the timestamp behind the age. It is separate from the
// formatting so the Recent sort orders rows by the same instant the row
// displays — a list sorted by one clock and labelled by another is a list a
// user cannot trust.
func worktreeChangedAt(wt *Worktree) time.Time {
	if wt == nil {
		return time.Time{}
	}
	if wt.Agent != nil && !wt.Agent.LastOutput.IsZero() {
		return wt.Agent.LastOutput
	}
	return wt.UpdatedAt
}

// shellAge is the freshness a shell row reports, in the same column and the
// same units a worktree row uses. Worktree rows have shown an age all along and
// shell rows have not, which left the one list where the two sit together
// answering "how long since anything happened here?" for half its rows.
//
// Last output is the meaningful change: it is recorded only when the capture
// actually differed, so an idle session's age keeps climbing instead of resetting
// on every poll. A shell with no session yet falls back to when it was created.
func shellAge(shell *ShellSession) string { return formatRelativeTime(shellChangedAt(shell)) }

func shellChangedAt(shell *ShellSession) time.Time {
	if shell == nil {
		return time.Time{}
	}
	if shell.Agent != nil && !shell.Agent.LastOutput.IsZero() {
		return shell.Agent.LastOutput
	}
	return shell.CreatedAt
}

func healthMarkerTone(icon string) workspacelist.MarkerTone {
	if icon == "✗" {
		return workspacelist.MarkerError
	}
	if icon == "⚠" {
		return workspacelist.MarkerWarning
	}
	return workspacelist.MarkerMuted
}

func worktreeMarkerTone(status WorktreeStatus) workspacelist.MarkerTone {
	switch status {
	case StatusActive, StatusDone:
		return workspacelist.MarkerLive
	case StatusWaiting:
		return workspacelist.MarkerWarning
	case StatusError:
		return workspacelist.MarkerError
	default:
		return workspacelist.MarkerMuted
	}
}

func activityPresentation(agent *Agent) (icon, text string, style lipgloss.Style, ok bool) {
	if agent == nil || !supportsAgentActivity(agent.Type) {
		return "", "", lipgloss.Style{}, false
	}
	p := agentstatus.Resolve(agentstatus.Input{ProviderSupported: true, Activity: agent.Activity, CapturedAt: agent.ActivityCapturedAt, Now: agent.ActivityCapturedAt, DoneTTL: agentstatus.DefaultDoneTTL})
	switch p.Lane {
	case agentstatus.LaneWorking:
		return p.Icon, p.Label, styles.StatusCompleted, true
	case agentstatus.LaneBlocked:
		return p.Icon, p.Label, styles.StatusModified, true
	case agentstatus.LaneDone:
		return p.Icon, p.Label, styles.StatusCompleted, true
	case agentstatus.LaneIdle:
		return p.Icon, p.Label, styles.Muted, true
	default:
		return p.Icon, p.Label, styles.Muted, true
	}
}
