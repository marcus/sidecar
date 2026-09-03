package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/hosts"
	"github.com/marcus/sidecar/internal/layoutapply"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacediff"
	"github.com/marcus/sidecar/internal/workspaceops"
)

type pendingView struct {
	Target    uirequest.Target
	Options   uirequest.Options
	CreatedAt time.Time
	TTLMs     int
}

func hostInstanceID() string {
	return uirequest.InstanceID("workspace")
}

const relayedOpenNotOnScreenReason = "the origin shell is not on screen, and relayed open requests are never queued"

func (p *Plugin) handleUIRequest(req uirequest.Request) tea.Cmd {
	if req.Action == uirequest.ActionRenameWorktree {
		p.applyWorktreeRenameRequest(req)
		return nil
	}
	if req.Action == uirequest.ActionRenameShell {
		p.applyShellRenameRequest(req)
		return nil
	}
	if req.Action == uirequest.ActionCreate {
		return p.applyCreateRequest(req)
	}
	if req.Origin.HostID != "" {
		return p.handleRelayedUIRequest(req)
	}
	if req.Action == uirequest.ActionLayout {
		if req.Origin.Sessions || !tty.ThisInstanceOwnsSession(req.Origin.TmuxSession) {
			return nil
		}
		return p.applyLayoutRequest(req)
	}
	if req.Action != uirequest.ActionOpen {
		return nil
	}
	if req.Origin.Sessions {
		return nil
	}
	if !tty.ThisInstanceOwnsSession(req.Origin.TmuxSession) {
		return nil
	}

	if req.Origin.TmuxSession == "" {
		if req.Origin.ProjectKey == "" || !p.matchesProjectTarget(req) {
			return nil
		}
		return p.openOnSelectedSurface(req)
	}

	// Match against shells in this workspace
	var targetShell *ShellSession
	for _, sh := range p.shells {
		if sh.TmuxName == req.Origin.TmuxSession {
			targetShell = sh
			break
		}
	}
	if targetShell == nil {
		// Not this instance's shell: ignore silently
		return nil
	}

	root, surface, ok := p.selectedTerminalSurface()
	isSelected := ok && surface == "shell:"+targetShell.TmuxName
	if isSelected {
		return p.applyOpenRequest(req, root, surface)
	}

	// Shell is not selected: queue it and write queued ack
	if p.pendingViews == nil {
		p.pendingViews = make(map[string]*pendingView)
	}
	p.pendingViews[targetShell.TmuxName] = &pendingView{
		Target:    req.Target,
		Options:   req.Options,
		CreatedAt: req.CreatedAt,
		TTLMs:     req.TTLMs,
	}

	p.writeLocalAck(req, uirequest.StatusQueued, "", "shell:"+targetShell.TmuxName, 0)
	return nil
}

func (p *Plugin) handleRelayedUIRequest(req uirequest.Request) tea.Cmd {
	if req.Origin.Sessions {
		return nil
	}
	if !p.remoteBound() || p.ctx.HostID != req.Origin.HostID {
		return nil
	}
	// Bound to this host: always ack. Returning nil with no ack while the app
	// has already skipped Sessions is a dropped request the host CLI waits on.
	if req.Action != uirequest.ActionLayout && req.Action != uirequest.ActionOpen {
		return p.ackRelayedDeclined(req, "unsupported relayed action")
	}
	if !p.ownsRelayedOrigin(req) {
		reason := relayedOpenNotOnScreenReason
		if req.Action == uirequest.ActionLayout {
			reason = layoutapply.NotOnScreenReason
		}
		return p.ackRelayedDeclined(req, reason)
	}
	if req.Action == uirequest.ActionLayout {
		return p.applyRelayedLayoutRequest(req)
	}
	return p.applyRelayedOpenRequest(req)
}

func (p *Plugin) ackRelayedDeclined(req uirequest.Request, reason string) tea.Cmd {
	if req.Action == uirequest.ActionLayout {
		return p.ackLayout(req, uirequest.StatusDeclined, reason, nil, nil)
	}
	surface := ""
	if req.Origin.TmuxSession != "" {
		surface = "shell:" + req.Origin.TmuxSession
	}
	p.ackOpen(req, uirequest.StatusDeclined, reason, surface, 0)
	return nil
}

func (p *Plugin) ownsRelayedOrigin(req uirequest.Request) bool {
	if !p.remoteBound() || p.ctx.HostID != req.Origin.HostID {
		return false
	}
	if !tty.ThisInstanceOwnsSession(req.Origin.TmuxSession) {
		return false
	}
	if req.Origin.ProjectKey != "" && !hosts.OriginNamesProject(req.Origin.ProjectKey, p.ctx.ProjectKey) {
		return false
	}
	return p.shellByTmux(req.Origin.TmuxSession) != nil || p.worktreeIndexForSession(req.Origin.TmuxSession) >= 0
}

func (p *Plugin) shellByTmux(session string) *ShellSession {
	if session == "" {
		return nil
	}
	for _, sh := range p.shells {
		if sh != nil && sh.TmuxName == session {
			return sh
		}
	}
	return nil
}

func (p *Plugin) applyRelayedOpenRequest(req uirequest.Request) tea.Cmd {
	root, surface, ok := p.selectedRelayedSurface(req.Origin.TmuxSession)
	if !ok {
		surface = "shell:" + req.Origin.TmuxSession
		p.ackOpen(req, uirequest.StatusDeclined, relayedOpenNotOnScreenReason, surface, 0)
		return nil
	}
	return p.applyOpenRequest(req, root, surface)
}

func (p *Plugin) applyRelayedLayoutRequest(req uirequest.Request) tea.Cmd {
	root, surface, ok := p.selectedRelayedSurface(req.Origin.TmuxSession)
	if !ok {
		return p.ackLayout(req, uirequest.StatusDeclined, layoutapply.NotOnScreenReason, nil, nil)
	}
	payload, err := uirequest.DecodeLayoutPayload(req.Payload)
	if err != nil {
		return p.ackLayout(req, uirequest.StatusDeclined, "invalid layout payload: "+err.Error(), nil, nil)
	}
	return p.answerLayout(req, payload, root, surface)
}

func (p *Plugin) selectedRelayedSurface(session string) (root, surface string, ok bool) {
	root, surface, ok = p.selectedTerminalSurface()
	if !ok {
		return "", "", false
	}
	if session == "" {
		return root, surface, true
	}
	if surface == "shell:"+session {
		return root, surface, true
	}
	if p.worktreeIndexForSession(session) >= 0 {
		if wt := p.selectedWorktree(); wt != nil && (worktreeTmuxSession(wt) == session || (wt.Agent != nil && wt.Agent.TmuxSession == session)) {
			return root, surface, true
		}
	}
	return "", "", false
}

func (p *Plugin) applyWorktreeRenameRequest(req uirequest.Request) {
	if req.Target.Kind != uirequest.TargetKindWorktree || req.Origin.WorkDir == "" || req.Target.Value == "" {
		return
	}
	for _, wt := range p.worktrees {
		if sameCanonicalPath(wt.Path, req.Origin.WorkDir) {
			wt.Name = req.Target.Value
			return
		}
	}
}

func (p *Plugin) applyCreateRequest(req uirequest.Request) tea.Cmd {
	payload, err := uirequest.DecodeCreatePayload(req.Payload)
	if err != nil {
		return nil
	}
	if !p.createRequestApplies(req) {
		return nil
	}
	switch payload.Kind {
	case uirequest.CreateKindShell:
		return p.applyCreateShellRequest(req, payload)
	case uirequest.CreateKindWorktree:
		return p.applyCreateWorktreeRequest(req, payload)
	default:
		return nil
	}
}

func (p *Plugin) createRequestApplies(req uirequest.Request) bool {
	if p.ctx == nil {
		return true
	}
	if req.Origin.ProjectKey != "" && p.matchesProjectTarget(req) {
		return true
	}
	if req.Origin.TmuxSession != "" {
		for _, sh := range p.shells {
			if sh != nil && sh.TmuxName == req.Origin.TmuxSession {
				return true
			}
		}
		if _, sh := p.findNestedShell(req.Origin.TmuxSession); sh != nil {
			return true
		}
		if p.worktreeIndexForSession(req.Origin.TmuxSession) >= 0 {
			return true
		}
	}
	if req.Origin.WorkDir != "" && (sameCanonicalPath(p.ctx.ProjectRoot, req.Origin.WorkDir) || sameCanonicalPath(p.ctx.WorkDir, req.Origin.WorkDir)) {
		return true
	}
	return false
}

func (p *Plugin) applyCreateShellRequest(req uirequest.Request, payload uirequest.CreatePayload) tea.Cmd {
	if split := strings.TrimSpace(req.Options.Split); split != "" {
		if req.Origin.Sessions {
			return nil
		}
		return p.applyCreateShellSplit(req, payload, split)
	}
	if payload.Session == "" {
		return nil
	}
	idx := -1
	for i, sh := range p.shells {
		if sh != nil && sh.TmuxName == payload.Session {
			idx = i
			if payload.DisplayName != "" {
				sh.Name = payload.DisplayName
			}
			break
		}
	}
	if idx < 0 {
		workDir := ""
		if p.ctx != nil {
			workDir = p.ctx.WorkDir
		}
		if req.Origin.WorkDir != "" {
			workDir = req.Origin.WorkDir
		}
		name := payload.DisplayName
		if name == "" {
			name = payload.Session
		}
		p.shells = append(p.shells, &ShellSession{
			Name:     name,
			TmuxName: payload.Session,
			WorkDir:  workDir,
		})
		idx = len(p.shells) - 1
	}
	if payload.ShouldFocus() {
		p.selectTopShellAt(idx)
		p.saveSelectionState()
	}
	p.ackCreate(req, "shell:"+payload.Session)
	if p.shellManifest != nil {
		return p.syncShellsFromManifest(p.currentShellStartupScope())
	}
	return nil
}

func (p *Plugin) applyCreateWorktreeRequest(req uirequest.Request, payload uirequest.CreatePayload) tea.Cmd {
	if payload.Path == "" {
		return nil
	}
	name := payload.DisplayName
	if name == "" {
		name = filepath.Base(payload.Path)
	}
	idx := -1
	for i, existing := range p.worktrees {
		if existing != nil && sameCanonicalPath(existing.Path, payload.Path) {
			idx = i
			existing.Name = name
			if payload.Branch != "" {
				existing.Branch = payload.Branch
			}
			break
		}
	}
	if idx < 0 {
		wt := &Worktree{Name: name, Path: payload.Path, Branch: payload.Branch}
		p.worktrees = append(p.worktrees, wt)
		idx = len(p.worktrees) - 1
	}
	if payload.ShouldFocus() {
		p.selectWorktreeAt(idx)
		p.resetPreviewScroll()
		p.saveSelectionState()
		p.ensureVisible()
	}
	surface := "worktree:" + payload.Path
	if payload.Session != "" {
		surface = "shell:" + payload.Session
	}
	p.ackCreate(req, surface)
	if p.ctx != nil {
		return p.refreshWorktrees()
	}
	return nil
}

func (p *Plugin) applyCreateShellSplit(req uirequest.Request, payload uirequest.CreatePayload, placement string) tea.Cmd {
	if !p.selectCreateSplitOrigin(req.Origin.TmuxSession) {
		return nil
	}
	if !terminalPanelEnabled() {
		p.ackCreateDeclined(req, features.WorkspaceTerminalPanel.Name+" is off")
		return nil
	}
	before := p.shellLeaf()
	session := p.termPanelSessionName()
	if payload.Run != "" || payload.Type != "" {
		p.pendingTermPanelSeed = &termPanelSeed{
			session: session,
			run:     payload.Run,
			typeCmd: payload.Type,
		}
	}
	cmd := p.createTerminalSplit(payload.DisplayName, placement)
	if p.shellLeaf() == nil || p.shellLeaf() == before {
		p.pendingTermPanelSeed = nil
		reason := p.toastMessage
		if reason == "" {
			reason = "the window is too small to split"
		}
		p.ackCreateDeclined(req, reason)
		return nil
	}
	if p.requireShellTermPane().Session != "" {
		session = p.requireShellTermPane().Session
	}
	p.ackCreate(req, "shell:"+session)
	return cmd
}

func (p *Plugin) selectCreateSplitOrigin(session string) bool {
	if session == "" {
		return false
	}
	for i, sh := range p.shells {
		if sh != nil && sh.TmuxName == session {
			p.selectTopShellAt(i)
			return true
		}
	}
	if parent, sh := p.findNestedShell(session); sh != nil {
		p.selectNestedShell(parent, session)
		return true
	}
	if idx := p.worktreeIndexForSession(session); idx >= 0 {
		p.selectWorktreeAt(idx)
		return true
	}
	return false
}

func (p *Plugin) worktreeIndexForSession(session string) int {
	if session == "" {
		return -1
	}
	for i, wt := range p.worktrees {
		if wt == nil {
			continue
		}
		if wt.Agent != nil && wt.Agent.TmuxSession == session {
			return i
		}
		if worktreeTmuxSession(wt) == session {
			return i
		}
		for _, name := range workspaceops.WorktreeSessionNames(wt.Path, wt.Name) {
			if name == session {
				return i
			}
		}
	}
	return -1
}

func (p *Plugin) ackCreate(req uirequest.Request, surface string) {
	_ = uirequest.WriteAck(config.StateDir(), req.ID, req.Action, uirequest.Ack{
		Instance: hostInstanceID(),
		Host:     uirequest.HostName(),
		PID:      os.Getpid(),
		Status:   uirequest.StatusOpened,
		Surface:  surface,
		At:       time.Now().UTC(),
	})
}

func (p *Plugin) ackCreateDeclined(req uirequest.Request, reason string) {
	_ = uirequest.WriteAck(config.StateDir(), req.ID, req.Action, uirequest.Ack{
		Instance: hostInstanceID(),
		Host:     uirequest.HostName(),
		PID:      os.Getpid(),
		Status:   uirequest.StatusDeclined,
		Reason:   reason,
		At:       time.Now().UTC(),
	})
}

func (p *Plugin) applyShellRenameRequest(req uirequest.Request) {
	if req.Target.Kind != uirequest.TargetKindShell || req.Origin.TmuxSession == "" || req.Target.Value == "" {
		return
	}
	for _, shell := range p.shells {
		if shell.TmuxName == req.Origin.TmuxSession {
			shell.Name = req.Target.Value
			p.saveSelectionState()
			return
		}
	}
	for _, shells := range p.nestedByWorkDir {
		for _, shell := range shells {
			if shell.TmuxName == req.Origin.TmuxSession {
				shell.Name = req.Target.Value
				p.saveSelectionState()
				return
			}
		}
	}
}

func (p *Plugin) matchesProjectTarget(req uirequest.Request) bool {
	if p.ctx == nil || req.Origin.ProjectKey == "" {
		return false
	}
	if dir, ok := projectdir.Lookup(p.ctx.ProjectRoot); ok {
		return filepath.Base(dir) == req.Origin.ProjectKey
	}
	return sameCanonicalPath(p.ctx.ProjectRoot, req.Origin.WorkDir)
}

func (p *Plugin) openOnSelectedSurface(req uirequest.Request) tea.Cmd {
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		_ = uirequest.WriteAck(config.StateDir(), req.ID, req.Action, uirequest.Ack{
			Instance: hostInstanceID(),
			Host:     uirequest.HostName(),
			PID:      os.Getpid(),
			Status:   uirequest.StatusDeclined,
			Reason:   "no selected workspace surface",
			At:       time.Now().UTC(),
		})
		return nil
	}
	return p.applyOpenRequest(req, root, surface)
}

func (p *Plugin) applyOpenRequest(req uirequest.Request, root, surface string) tea.Cmd {
	outcome, cmd := p.performTargetOpen(req, root, surface)
	if outcome.status == uirequest.StatusDeclined {
		p.ackOpen(req, uirequest.StatusDeclined, outcome.reason, surface, 0)
		return nil
	}
	p.ackOpen(req, outcome.status, "", surface, p.paneFocus)
	return cmd
}

func (p *Plugin) ackOpen(req uirequest.Request, status uirequest.Status, reason, surface string, pane int) {
	if req.Origin.HostID == "" {
		p.writeLocalAck(req, status, reason, surface, pane)
		return
	}
	p.ackRemote(req, status, reason, surface, pane, nil, nil)
}

func (p *Plugin) writeLocalAck(req uirequest.Request, status uirequest.Status, reason, surface string, pane int) {
	_ = uirequest.WriteAck(config.StateDir(), req.ID, req.Action, uirequest.Ack{
		Instance: hostInstanceID(),
		Host:     uirequest.HostName(),
		PID:      os.Getpid(),
		Status:   status,
		Reason:   reason,
		Surface:  surface,
		Pane:     pane,
		At:       time.Now().UTC(),
	})
}

func (p *Plugin) ackRemote(req uirequest.Request, status uirequest.Status, reason, surface string, pane int, layout json.RawMessage, items []uirequest.AckItem) {
	if p.ctx == nil || p.ctx.RemoteRunner == nil || req.Origin.HostID == "" {
		return
	}
	args := []string{"request", "ack", "--id", req.ID, "--action", string(req.Action), "--status", string(status), "--json"}
	if reason != "" {
		args = append(args, "--reason", reason)
	}
	if surface != "" {
		args = append(args, "--surface", surface)
	}
	if pane != 0 {
		args = append(args, "--pane", strconv.Itoa(pane))
	}
	if len(layout) > 0 {
		args = append(args, "--layout", string(layout))
	}
	if len(items) > 0 {
		if raw, err := json.Marshal(items); err == nil {
			args = append(args, "--items", string(raw))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result uirequest.AckResult
	_ = p.ctx.RemoteRunner(ctx, req.Origin.HostID, args, &result)
}

// openOutcome is what one target's open earned. The pane tree, not the returned
// command, is the honest witness of an open: a split that did not fit still
// returns the reopen command, and re-opening a file already on screen
// legitimately returns none.
type openOutcome struct {
	status uirequest.Status
	reason string
}

// performTargetOpen opens one resolved target on the given surface — the whole
// per-kind body of `sidecar open`, minus acknowledgement writing. Both the
// single-open ack path and the layout batch commit call it, so the two cannot
// drift apart about what opening a target means. A request carrying an
// explicit cell (Options.At) takes the PlanOpenAt path instead: placement is a
// requirement there, planned before anything opens.
func (p *Plugin) performTargetOpen(req uirequest.Request, root, surface string) (openOutcome, tea.Cmd) {
	prevSplit := p.openSplit
	p.openSplit = req.Options.Split
	defer func() { p.openSplit = prevSplit }()

	if at := strings.TrimSpace(req.Options.At); at != "" {
		return p.performAtCellOpen(req.Target, at, root, surface)
	}

	var cmd tea.Cmd
	opened := false
	// Asked before the open, because afterwards the pane exists either way:
	// the planner is what decides between a new split and an existing pane.
	retargeted := false
	switch req.Target.Kind {
	case uirequest.TargetKindFile:
		retargeted = p.willRetargetPane(PaneDoc)
		cmd = p.openDocPaneForSurface(root, surface, req.Target.Value, req.Target.Line)
		// A document open is not reported by its command: a split that did
		// not fit still returns the reopen command, and re-opening a file
		// already on screen legitimately returns none. The pane tree is the
		// only honest witness.
		opened = p.docPaneShows(req.Target.Value)
	case uirequest.TargetKindIssue:
		retargeted = p.willRetargetPane(PaneIssue)
		cmd = p.openIssuePaneForSurface(root, surface, req.Target.Value)
		opened = p.contentPaneOnScreen(PaneIssue)
	case uirequest.TargetKindNote:
		retargeted = p.willRetargetPane(PaneNote)
		cmd = p.openNotePaneForSurface(root, surface, req.Target.Value)
		opened = p.contentPaneOnScreen(PaneNote)
	case uirequest.TargetKindDiff:
		if p.paneRoot == nil {
			return openOutcome{status: uirequest.StatusDeclined, reason: features.WorkspaceDocPanesDisabledDiff}, appmsg.ShowFlash(features.WorkspaceDocPanesDisabledDiff)
		}
		retargeted = p.willRetargetPane(PaneDiff)
		spec := uirequest.DiffTarget(root, req.Target.Value)
		cmd = p.openDiffPaneForSurface(root, surface, spec)
		opened = p.diffPaneShows(spec)
	case uirequest.TargetKindResource:
		ref, refusal := resourceview.ReferenceForRequest(p.resourceMatchers,
			req.Target.Provider, req.Target.Matcher, req.Target.Collection, req.Target.Query, req.Target.Value, req.Target.Filters)
		if refusal != "" {
			return openOutcome{status: uirequest.StatusDeclined, reason: refusal}, nil
		}
		retargeted = p.willRetargetPane(PaneResource)
		cmd = p.openRequestedResourcePaneForSurface(root, surface, ref)
		res, _ := p.activeResourcePane()
		opened = res != nil && res.tabs.Find(resourceview.TabKey(ref)) >= 0
	default:
		return openOutcome{status: uirequest.StatusDeclined, reason: "unsupported target kind " + string(req.Target.Kind)}, nil
	}

	// Nothing on screen: the split did not fit, or the target could not be
	// loaded. Say so rather than claiming an open the user cannot see; the
	// toast, when there is one, carries the reason.
	if !opened {
		reason := p.toastMessage
		if reason == "" {
			reason = "the window is too small to split"
		}
		return openOutcome{status: uirequest.StatusDeclined, reason: reason}, nil
	}
	status := uirequest.StatusOpened
	if retargeted {
		status = uirequest.StatusRetargeted
	}
	return openOutcome{status: status}, cmd
}

func sameCanonicalPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return canonicalOpenPath(a) == canonicalOpenPath(b)
}

// performAtCellOpen is the single-open explicit-cell path: the same plumbing
// the layout batch's at-cells run — the SCREEN grid answers ranges, caps, and
// retarget conflicts first (an --at that cannot land exactly where asked
// declines; it is a requirement, never a preference), the cell then translates
// onto the deck's own grid, and the plan that was validated is what commit
// applies verbatim. The deck planner's refusal, if one ever comes back,
// surfaces word for word rather than being re-guessed.
func (p *Plugin) performAtCellOpen(target uirequest.Target, at, root, surface string) (openOutcome, tea.Cmd) {
	cell, ok := panelayout.ParseCell(at)
	if !ok {
		return openOutcome{status: uirequest.StatusDeclined, reason: fmt.Sprintf("cell %q is not a grid address like 2.1", at)}, nil
	}
	kind, ok := paneKindForTarget(target.Kind)
	if !ok {
		return openOutcome{status: uirequest.StatusDeclined, reason: fmt.Sprintf("a %s target has no pane to place at a cell", string(target.Kind))}, nil
	}
	if _, refusal := panelayout.PlanOpenAt(p.paneRoot, kind, 0, cell); refusal != "" {
		return openOutcome{status: uirequest.StatusDeclined, reason: refusal}, nil
	}
	p.ensureWorkspaceDeck(root, surface)
	translated, refusal := deckCellFor(p.paneRoot, cell)
	if refusal != "" {
		return openOutcome{status: uirequest.StatusDeclined, reason: refusal}, nil
	}
	plan, deckRefusal := panelayout.PlanOpenAt(p.contentDeck.Tree(), kind, 0, translated)
	if deckRefusal != "" {
		return openOutcome{status: uirequest.StatusDeclined, reason: deckRefusal}, nil
	}
	return p.performPlannedOpen(target, root, surface, plan)
}

// paneKindForTarget maps an open request's wire kind onto its pane kind. Only
// the passive content kinds are placeable: shells never arrive through `open`.
func paneKindForTarget(kind uirequest.TargetKind) (panelayout.Kind, bool) {
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

func canonicalOpenPath(path string) string {
	path = filepath.Clean(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(resolved)
	}
	return path
}

// willRetargetPane reports whether opening kind would land in a pane that is
// already on screen rather than splitting a new one.
func (p *Plugin) willRetargetPane(kind PaneKind) bool {
	plan, ok := planPaneOpen(p.paneRoot, kind, p.lastPaneBoxes())
	return ok && plan.Retarget != 0
}

// contentPaneOnScreen reports whether kind's content leaf is in the live pane
// tree. An open that only focuses an already-loaded tab returns no command,
// so the command cannot witness that anything happened — the pane tree is
// (the same rule the file branch follows with docPaneShows).
func (p *Plugin) contentPaneOnScreen(kind panelayout.Kind) bool {
	if p.contentDeck == nil {
		return false
	}
	leafID := p.contentDeck.Leaf(kind)
	return leafID != 0 && FindPane(p.paneRoot, leafID) != nil
}

// docPaneShows reports whether the live document pane is showing rel.
func (p *Plugin) docPaneShows(rel string) bool {
	doc, leaf := p.activeDocPane()
	if doc == nil || leaf == nil {
		return false
	}
	view := doc.view()
	if view == nil {
		return false
	}
	return docview.NormalizeTabPath(view.Title()) == docview.NormalizeTabPath(rel)
}

func (p *Plugin) consumePendingView(tmuxName string) tea.Cmd {
	if p.pendingViews == nil {
		return nil
	}
	pv, ok := p.pendingViews[tmuxName]
	if !ok || pv == nil {
		return nil
	}
	delete(p.pendingViews, tmuxName)

	ttl := time.Duration(pv.TTLMs) * time.Millisecond
	if ttl <= 0 {
		ttl = uirequest.DefaultTTL
	}
	if time.Since(pv.CreatedAt) > ttl {
		return nil
	}

	root, surface, ok := p.selectedTerminalSurface()
	if !ok || surface != "shell:"+tmuxName {
		return nil
	}

	prevSplit := p.openSplit
	p.openSplit = pv.Options.Split
	defer func() { p.openSplit = prevSplit }()

	// An explicit cell queued with an off-screen shell is re-planned at
	// selection time, exactly as --split is re-applied there.
	if at := strings.TrimSpace(pv.Options.At); at != "" {
		_, cmd := p.performAtCellOpen(pv.Target, at, root, surface)
		return cmd
	}

	switch pv.Target.Kind {
	case uirequest.TargetKindFile:
		return p.openDocPaneForSurface(root, surface, pv.Target.Value, pv.Target.Line)
	case uirequest.TargetKindIssue:
		return p.openIssuePaneForSurface(root, surface, pv.Target.Value)
	case uirequest.TargetKindNote:
		return p.openNotePaneForSurface(root, surface, pv.Target.Value)
	case uirequest.TargetKindDiff:
		return p.openDiffPaneForSurface(root, surface, uirequest.DiffTarget(root, pv.Target.Value))
	case uirequest.TargetKindResource:
		ref, refusal := resourceview.ReferenceForLocator(p.resourceMatchers, pv.Target.Provider, pv.Target.Value)
		if refusal != "" {
			return nil
		}
		return p.openRequestedResourcePaneForSurface(root, surface, ref)
	}
	return nil
}

func (p *Plugin) diffPaneShows(target workspacediff.Target) bool {
	diff, leaf := p.activeDiffPane()
	if diff == nil || leaf == nil {
		return false
	}
	return diff.tabs.Find(target.Identity()) >= 0
}

func (p *Plugin) pendingViewBadge(tmuxName string) (string, bool) {
	if p.pendingViews == nil {
		return "", false
	}
	pv, ok := p.pendingViews[tmuxName]
	if !ok || pv == nil {
		return "", false
	}
	ttl := time.Duration(pv.TTLMs) * time.Millisecond
	if ttl <= 0 {
		ttl = uirequest.DefaultTTL
	}
	if time.Since(pv.CreatedAt) > ttl {
		return "", false
	}
	return " ◫", true
}
