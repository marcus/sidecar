package overview

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
	"github.com/marcus/sidecar/internal/hostproto"
	"github.com/marcus/sidecar/internal/hosts"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacediff"
	"github.com/marcus/sidecar/internal/workspaceinventory"
)

const relayedOpenNotOnScreenReason = "that Sessions row is not on screen, and relayed open requests are never queued"

const sessionsOpenNotOnScreenReason = "the Sessions surface is not on screen, and open --sessions is never queued"

type pendingView struct {
	Target    uirequest.Target
	Options   uirequest.Options
	CreatedAt time.Time
	TTLMs     int
}

func hostInstanceID() string {
	return uirequest.InstanceID("overview")
}

func (m *Model) handleUIRequest(req uirequest.Request) tea.Cmd {
	if req.Action == uirequest.ActionRenameWorktree {
		m.applyWorktreeRenameRequest(req)
		return nil
	}
	if req.Action == uirequest.ActionRenameShell {
		m.applyShellRenameRequest(req)
		return nil
	}
	if req.Action == uirequest.ActionCreate {
		return m.applyCreateRequest(req)
	}
	if req.Origin.HostID != "" && (req.Action == uirequest.ActionLayout || req.Action == uirequest.ActionOpen) {
		if m.RelayedLanding != nil && !m.RelayedLanding(req) {
			return nil
		}
	}
	if req.Action == uirequest.ActionLayout {
		if req.Origin.HostID == "" && !req.Origin.Sessions && !tty.ThisInstanceOwnsSession(req.Origin.TmuxSession) {
			return nil
		}
		return m.applyLayoutRequest(req)
	}
	if req.Action != uirequest.ActionOpen {
		return nil
	}
	if req.Origin.HostID == "" && !req.Origin.Sessions && !tty.ThisInstanceOwnsSession(req.Origin.TmuxSession) {
		return nil
	}

	targetWorkspace, ok := m.bindOpenWorkspace(req)
	if !ok {
		if req.Origin.HostID != "" || req.Origin.Sessions {
			reason := "no Sessions row matches that origin"
			if req.Origin.Sessions {
				if _, _, _, rowReason := m.resolveSessionsLayoutRow(req); rowReason != "" {
					reason = rowReason
				}
			}
			m.ackOpen(req, uirequest.StatusDeclined, reason, "", 0)
		}
		return nil
	}

	neverQueue := req.Origin.HostID != "" || req.Origin.Sessions
	selected, hasSelected := m.SelectedWorkspace()
	isSelected := hasSelected && selected.ID == targetWorkspace.ID

	if neverQueue {
		if !m.preview.visible {
			reason := relayedOpenNotOnScreenReason
			if req.Origin.Sessions && req.Origin.HostID == "" {
				reason = sessionsOpenNotOnScreenReason
			}
			m.ackOpen(req, uirequest.StatusDeclined, reason, openAckSurface(*targetWorkspace), 0)
			return nil
		}
		if req.Origin.Sessions && req.Origin.HostID == "" {
			if cmd := m.focusSessionsLayoutRow(targetWorkspace.ID); cmd != nil {
				_ = cmd
			}
			selected, hasSelected = m.SelectedWorkspace()
			isSelected = hasSelected && selected.ID == targetWorkspace.ID
		}
		if !isSelected {
			m.ackOpen(req, uirequest.StatusDeclined, relayedOpenNotOnScreenReason, openAckSurface(*targetWorkspace), 0)
			return nil
		}
		return m.applyOpenOnPreview(req, *targetWorkspace)
	}

	if isSelected {
		return m.applyOpenOnPreview(req, *targetWorkspace)
	}

	if m.pendingViews == nil {
		m.pendingViews = make(map[string]*pendingView)
	}
	m.pendingViews[targetWorkspace.TmuxName] = &pendingView{
		Target:    req.Target,
		Options:   req.Options,
		CreatedAt: req.CreatedAt,
		TTLMs:     req.TTLMs,
	}

	m.ackOpen(req, uirequest.StatusQueued, "", openAckSurface(*targetWorkspace), 0)
	return nil
}

func (m *Model) applyOpenOnPreview(req uirequest.Request, targetWorkspace workspaceinventory.Workspace) tea.Cmd {
	prevSplit := m.openSplit
	m.openSplit = req.Options.Split
	// The plan is scoped to THIS request whether or not the open reaches a
	// placement: a resource that no matcher claims, or a kind that opens
	// nothing, returns before previewDeckPlacement consumes it, and a plan
	// left behind would place the NEXT open at a cell nobody asked for.
	defer func() {
		m.openSplit = prevSplit
		m.pendingOpenPlan = nil
	}()

	surface := openAckSurface(targetWorkspace)
	if at := strings.TrimSpace(req.Options.At); at != "" {
		plan, refusal, ok := m.planPreviewOpenAt(req.Target, at)
		if !ok {
			m.ackOpen(req, uirequest.StatusDeclined, refusal, surface, 0)
			return nil
		}
		m.pendingOpenPlan = plan
	}

	var cmd tea.Cmd
	var openErr error
	// Asked before the open, because afterwards the pane exists either way:
	// the planner is what decides between a new split and an existing pane.
	retargeted := false
	switch req.Target.Kind {
	case uirequest.TargetKindFile:
		retargeted = m.willRetargetPreviewPane(panelayout.Document)
		cmd, openErr = m.openPreviewDocTargetResult(req.Target)
	case uirequest.TargetKindIssue:
		retargeted = m.willRetargetPreviewPane(panelayout.Issue)
		cmd, openErr = m.openPreviewIssueResult(req.Target.Value)
	case uirequest.TargetKindNote:
		retargeted = m.willRetargetPreviewPane(panelayout.Note)
		cmd, openErr = m.openPreviewNoteResult(req.Target.Value)
	case uirequest.TargetKindDiff:
		retargeted = m.willRetargetPreviewPane(panelayout.Diff)
		if targetWorkspace.Remote() {
			spec, ok := workspacediff.ParseSpec(req.Target.Value)
			if !ok {
				spec = workspacediff.WorkingTreeTarget()
			}
			cmd, openErr = m.openPreviewDiffResult(spec)
		} else {
			cmd, openErr = m.openPreviewDiffResult(uirequest.DiffTarget(targetWorkspace.Path, req.Target.Value))
		}
	case uirequest.TargetKindResource:
		ref, refusal := resourceview.ReferenceForRequest(m.previewResourceMatchers(),
			req.Target.Provider, req.Target.Matcher, req.Target.Collection, req.Target.Query, req.Target.Value, req.Target.Filters)
		if refusal != "" {
			m.ackOpen(req, uirequest.StatusDeclined, refusal, surface, 0)
			return nil
		}
		retargeted = m.willRetargetPreviewPane(panelayout.Resource)
		cmd, openErr = m.openPreviewResourceRefResult(ref, false)
	}
	if openErr != nil {
		m.ackOpen(req, uirequest.StatusDeclined, openErr.Error(), surface, 0)
		return cmd
	}

	if cmd == nil {
		m.ackOpen(req, uirequest.StatusDeclined, "window too small to split", surface, 0)
		return nil
	}

	status := uirequest.StatusOpened
	if retargeted {
		status = uirequest.StatusRetargeted
	}
	m.ackOpen(req, status, "", surface, m.preview.paneFocus)
	return cmd
}

func (m *Model) bindOpenWorkspace(req uirequest.Request) (*workspaceinventory.Workspace, bool) {
	if req.Origin.HostID != "" {
		// A machine the user has hidden has no row on this screen, and the
		// open contract answers "not on screen" by declining rather than
		// acting on something the user cannot see.
		if !m.hostShown(req.Origin.HostID) {
			return nil, false
		}
		for _, ws := range m.catalog {
			if ws.HostID == req.Origin.HostID && ws.TmuxName == req.Origin.TmuxSession && ws.TmuxName != "" {
				hit := ws
				return &hit, true
			}
		}
		return nil, false
	}
	if req.Origin.Sessions {
		_, ws, ok, _ := m.resolveSessionsLayoutRow(req)
		if !ok {
			return nil, false
		}
		return &ws, true
	}
	if req.Origin.TmuxSession == "" {
		return nil, false
	}
	for _, ws := range m.catalog {
		// Session names are unique per machine, not globally: two machines
		// running Sidecar on the same project produce the same name. A request
		// that originated in a local shell must never bind to a remote row,
		// which unordered map iteration would otherwise do at random.
		// Display-name fallback stays on this skip; a relayed request binds
		// (HostID, TmuxSession) above.
		if ws.Remote() {
			continue
		}
		if ws.TmuxName == req.Origin.TmuxSession {
			hit := ws
			return &hit, true
		}
	}
	return nil, false
}

func openAckSurface(ws workspaceinventory.Workspace) string {
	if ws.Remote() {
		return ws.ID
	}
	if ws.TmuxName != "" {
		return "shell:" + ws.TmuxName
	}
	return ws.ID
}

func (m *Model) ackOpen(req uirequest.Request, status uirequest.Status, reason, surface string, pane int) {
	if req.Origin.HostID == "" {
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
		return
	}
	m.ackRemote(req, status, reason, surface, pane, nil, nil)
}

func (m *Model) ackRemote(req uirequest.Request, status uirequest.Status, reason, surface string, pane int, layout json.RawMessage, items []uirequest.AckItem) {
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
	ctx, cancel := context.WithTimeout(context.Background(), remoteQuickTimeout)
	defer cancel()
	var result uirequest.AckResult
	_ = runRemoteSidecar(ctx, m.hostRegistry, req.Origin.HostID, args, &result)
}

func requestFromAnnouncement(event hostproto.UIRequest) uirequest.Request {
	return uirequest.Request{
		Version:   1,
		ID:        event.ID,
		CreatedAt: event.CreatedAt,
		TTLMs:     event.TTLMs,
		Origin: uirequest.Origin{
			TmuxSession: event.Origin.TmuxSession,
			Namespace:   event.Origin.Namespace,
			ProjectKey:  event.Origin.ProjectKey,
			WorkDir:     event.Origin.WorkDir,
			HostID:      event.Origin.HostID,
			Sessions:    event.Origin.Sessions,
			SessionsRow: event.Origin.SessionsRow,
		},
		Action: uirequest.Action(event.Action),
		Target: uirequest.Target{
			Kind:     uirequest.TargetKind(event.Target.Kind),
			Value:    event.Target.Value,
			Line:     event.Target.Line,
			Provider: event.Target.Provider,
			Matcher:  event.Target.Matcher,
		},
		Options: uirequest.Options{
			Split: event.Options.Split,
			At:    event.Options.At,
		},
		Payload: event.Payload,
	}
}

func (m *Model) forwardHostUIRequests(update hosts.Update) tea.Cmd {
	if len(update.UIRequest) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(update.UIRequest))
	for _, event := range update.UIRequest {
		req := requestFromAnnouncement(event)
		if req.Origin.HostID == "" {
			req.Origin.HostID = update.HostID
		}
		if m.RelayedLanding != nil && !m.RelayedLanding(req) {
			r := req
			cmds = append(cmds, func() tea.Msg { return uirequest.RequestMsg{Request: r} })
			continue
		}
		if cmd := m.handleUIRequest(req); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// RelayedRowOnScreen reports that the Sessions preview is showing the origin's
// matching row — the existing landing gate, asked as a yes/no so the app-level
// decider does not copy it.
func (m *Model) RelayedRowOnScreen(req uirequest.Request) bool {
	if m == nil || !m.preview.visible {
		return false
	}
	bound, ok := m.bindOpenWorkspace(req)
	if !ok {
		return false
	}
	selected, has := m.SelectedWorkspace()
	return has && selected.ID == bound.ID
}

func (m *Model) applyCreateRequest(req uirequest.Request) tea.Cmd {
	payload, err := uirequest.DecodeCreatePayload(req.Payload)
	if err != nil {
		return nil
	}
	if split := strings.TrimSpace(req.Options.Split); split != "" {
		if payload.Kind != uirequest.CreateKindShell {
			return nil
		}
		return m.applyCreateShellSplit(req, payload, split)
	}
	project, key, ok := m.createRequestProject(req)
	if !ok {
		return nil
	}
	switch payload.Kind {
	case uirequest.CreateKindShell:
		return m.applyCreateShellRequest(req, payload, project, key)
	case uirequest.CreateKindWorktree:
		return m.applyCreateWorktreeRequest(req, payload, project, key)
	default:
		return nil
	}
}

func (m *Model) createRequestProject(req uirequest.Request) (Project, string, bool) {
	for _, project := range m.projects {
		if m.originMatchesProject(req, project) {
			return project, projectKey(project), true
		}
	}
	if req.Origin.ProjectKey != "" {
		if _, ok := m.results[req.Origin.ProjectKey]; ok {
			return Project{Key: req.Origin.ProjectKey, Path: req.Origin.WorkDir, Name: req.Origin.ProjectKey}, req.Origin.ProjectKey, true
		}
	}
	if req.Origin.TmuxSession != "" {
		for key, result := range m.results {
			for _, ws := range result.Workspaces {
				if ws.TmuxName == req.Origin.TmuxSession {
					for _, project := range m.projects {
						if projectKey(project) == key {
							return project, key, true
						}
					}
					return Project{Key: key, Path: result.ProjectRoot, Name: result.ProjectName}, key, true
				}
			}
		}
	}
	return Project{}, "", false
}

func (m *Model) originMatchesProject(req uirequest.Request, project Project) bool {
	key := projectKey(project)
	if req.Origin.ProjectKey != "" {
		if req.Origin.ProjectKey == key || req.Origin.ProjectKey == project.Name || req.Origin.ProjectKey == filepath.Base(project.Path) {
			return true
		}
		if dir, ok := projectdir.Lookup(project.Path); ok && filepath.Base(dir) == req.Origin.ProjectKey {
			return true
		}
	}
	if req.Origin.WorkDir == "" {
		return false
	}
	want := workspaceinventory.CanonicalPath(req.Origin.WorkDir)
	if workspaceinventory.CanonicalPath(project.Path) == want || createPathInside(want, project.Path) {
		return true
	}
	result, ok := m.results[key]
	if !ok {
		return false
	}
	for _, ws := range result.Workspaces {
		if ws.Path == "" {
			continue
		}
		if workspaceinventory.CanonicalPath(ws.Path) == want || createPathInside(want, ws.Path) {
			return true
		}
	}
	return false
}

func createPathInside(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(workspaceinventory.CanonicalPath(root), workspaceinventory.CanonicalPath(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (m *Model) applyCreateShellRequest(req uirequest.Request, payload uirequest.CreatePayload, project Project, key string) tea.Cmd {
	if payload.Session == "" {
		return nil
	}
	name := payload.DisplayName
	if name == "" {
		name = payload.Session
	}
	ws := workspaceinventory.Workspace{
		ProjectKey:  key,
		ProjectName: project.Name,
		ProjectRoot: project.Path,
		Kind:        workspaceinventory.KindShell,
		Key:         payload.Session,
		Name:        name,
		Path:        project.Path,
		TmuxName:    payload.Session,
		Live:        true,
	}
	if ws.Path == "" {
		ws.Path = req.Origin.WorkDir
		ws.ProjectRoot = req.Origin.WorkDir
	}
	ws.ID = ws.ProjectKey + ":shell:" + ws.Key
	m.upsertCreateWorkspace(key, ws)
	if payload.ShouldFocus() {
		m.pendingCreatedTmux = payload.Session
		m.pendingCreatedPath = ""
		// A CLI-driven create ran on this machine, by construction: the request
		// file it came from is in this machine's state tree.
		m.pendingCreatedHost = ""
		if !m.workspaces.SelectID(ws.ID) {
			m.honorPendingCreated()
		} else {
			m.clearPendingCreated()
		}
	}
	m.ackCreate(req, "shell:"+payload.Session)
	if project.Path != "" {
		return m.refreshProjectAfterMutation(project)
	}
	return nil
}

func (m *Model) applyCreateWorktreeRequest(req uirequest.Request, payload uirequest.CreatePayload, project Project, key string) tea.Cmd {
	if payload.Path == "" {
		return nil
	}
	path := workspaceinventory.CanonicalPath(payload.Path)
	name := payload.DisplayName
	if name == "" {
		name = filepath.Base(path)
	}
	ws := workspaceinventory.Workspace{
		ProjectKey:  key,
		ProjectName: project.Name,
		ProjectRoot: project.Path,
		Kind:        workspaceinventory.KindWorktree,
		Key:         path,
		Name:        name,
		Path:        path,
		Branch:      payload.Branch,
		TmuxName:    payload.Session,
		// Optimistic: the CLI sends a session name even for --no-launch, so
		// Session is not liveness. Inventory refresh replaces this row.
		Plain: true,
	}
	ws.ID = ws.ProjectKey + ":worktree:" + ws.Key
	// Show this worktree only. Flipping showIdleWorktrees would dump every
	// idle checkout across every project into Sessions.
	m.revealIdleWorktree(path, "")
	m.upsertCreateWorkspace(key, ws)
	if payload.ShouldFocus() {
		m.pendingCreatedPath = path
		m.pendingCreatedTmux = ""
		m.pendingCreatedHost = ""
		if !m.workspaces.SelectID(ws.ID) {
			m.honorPendingCreated()
		} else {
			m.clearPendingCreated()
		}
	}
	surface := "worktree:" + path
	if payload.Session != "" {
		surface = "shell:" + payload.Session
	}
	m.ackCreate(req, surface)
	if project.Path != "" {
		return m.refreshProjectAfterMutation(project)
	}
	return nil
}

func (m *Model) upsertCreateWorkspace(key string, ws workspaceinventory.Workspace) {
	result := m.results[key]
	found := false
	for i, existing := range result.Workspaces {
		sameShell := ws.Kind == workspaceinventory.KindShell && existing.Kind == workspaceinventory.KindShell && existing.TmuxName == ws.TmuxName
		sameWorktree := ws.Kind == workspaceinventory.KindWorktree && existing.Kind == workspaceinventory.KindWorktree && existing.Path == ws.Path
		if !sameShell && !sameWorktree {
			continue
		}
		if ws.Name != "" {
			result.Workspaces[i].Name = ws.Name
		}
		if ws.TmuxName != "" {
			result.Workspaces[i].TmuxName = ws.TmuxName
		}
		if ws.Branch != "" {
			result.Workspaces[i].Branch = ws.Branch
		}
		if ws.Live {
			result.Workspaces[i].Live = true
		}
		found = true
		break
	}
	if !found {
		result.Workspaces = append(result.Workspaces, ws)
	}
	if m.results == nil {
		m.results = make(map[string]workspaceinventory.ProjectResult)
	}
	m.results[key] = result
	m.syncBoard()
}

func (m *Model) ackCreate(req uirequest.Request, surface string) {
	_ = uirequest.WriteAck(config.StateDir(), req.ID, req.Action, uirequest.Ack{
		Instance: hostInstanceID(),
		Host:     uirequest.HostName(),
		PID:      os.Getpid(),
		Status:   uirequest.StatusOpened,
		Surface:  surface,
		At:       time.Now().UTC(),
	})
}

func (m *Model) ackCreateDeclined(req uirequest.Request, reason string) {
	_ = uirequest.WriteAck(config.StateDir(), req.ID, req.Action, uirequest.Ack{
		Instance: hostInstanceID(),
		Host:     uirequest.HostName(),
		PID:      os.Getpid(),
		Status:   uirequest.StatusDeclined,
		Reason:   reason,
		At:       time.Now().UTC(),
	})
}

func (m *Model) applyWorktreeRenameRequest(req uirequest.Request) {
	if req.Target.Kind != uirequest.TargetKindWorktree || req.Origin.WorkDir == "" || req.Target.Value == "" {
		return
	}
	targetPath := workspaceinventory.CanonicalPath(req.Origin.WorkDir)
	changed := false
	for key, result := range m.results {
		resultChanged := false
		for i := range result.Workspaces {
			workspace := &result.Workspaces[i]
			if workspace.Kind == workspaceinventory.KindWorktree && workspace.Path == targetPath {
				workspace.Name = req.Target.Value
				resultChanged = true
			}
		}
		if resultChanged {
			m.results[key] = result
			changed = true
		}
	}
	if changed {
		m.syncBoard()
	}
}

func (m *Model) applyShellRenameRequest(req uirequest.Request) {
	if req.Target.Kind != uirequest.TargetKindShell || req.Origin.TmuxSession == "" || req.Target.Value == "" {
		return
	}
	changed := false
	for key, result := range m.results {
		resultChanged := false
		for i := range result.Workspaces {
			workspace := &result.Workspaces[i]
			if workspace.TmuxName == req.Origin.TmuxSession {
				workspace.Name = req.Target.Value
				resultChanged = true
			}
		}
		if resultChanged {
			m.results[key] = result
			changed = true
		}
	}
	if changed {
		m.syncBoard()
	}
}

// planPreviewOpenAt resolves one open request's explicit cell against this
// preview's pane tree — the same requirement semantics the workspace surface
// runs: an unhonorable cell (out of range, over a cap, or a kind whose open
// would retarget an existing pane) refuses instead of landing elsewhere. ok is
// false with the reason to show.
func (m *Model) planPreviewOpenAt(target uirequest.Target, at string) (*panelayout.OpenPlan, string, bool) {
	cell, ok := panelayout.ParseCell(at)
	if !ok {
		return nil, fmt.Sprintf("cell %q is not a grid address like 2.1", at), false
	}
	kind, ok := previewKindForTarget(target.Kind)
	if !ok {
		return nil, fmt.Sprintf("a %s target has no pane to place at a cell", string(target.Kind)), false
	}
	plan, refusal := panelayout.PlanOpenAt(m.preview.paneRoot, kind, 0, cell)
	if refusal != "" {
		return nil, refusal, false
	}
	return &plan, "", true
}

// previewKindForTarget maps an open request's wire kind onto its pane kind.
func previewKindForTarget(kind uirequest.TargetKind) (panelayout.Kind, bool) {
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

// willRetargetPreviewPane reports whether opening kind would land in a pane
// that is already on screen rather than splitting a new one.
func (m *Model) willRetargetPreviewPane(kind panelayout.Kind) bool {
	plan, ok := panelayout.PlanOpen(m.preview.paneRoot, kind, m.lastPreviewBoxes())
	return ok && plan.Retarget != 0
}

func (m *Model) consumePendingView(tmuxName string) tea.Cmd {
	if m.pendingViews == nil {
		return nil
	}
	pv, ok := m.pendingViews[tmuxName]
	if !ok || pv == nil {
		return nil
	}
	delete(m.pendingViews, tmuxName)

	ttl := time.Duration(pv.TTLMs) * time.Millisecond
	if ttl <= 0 {
		ttl = uirequest.DefaultTTL
	}
	if time.Since(pv.CreatedAt) > ttl {
		return nil
	}

	prevSplit := m.openSplit
	m.openSplit = pv.Options.Split
	defer func() { m.openSplit = prevSplit }()

	switch pv.Target.Kind {
	case uirequest.TargetKindFile:
		return m.openPreviewDocTarget(pv.Target)
	case uirequest.TargetKindIssue:
		return m.openPreviewIssue(pv.Target.Value)
	case uirequest.TargetKindDiff:
		selected, ok := m.SelectedWorkspace()
		if ok && selected.Remote() {
			spec, parsed := workspacediff.ParseSpec(pv.Target.Value)
			if !parsed {
				spec = workspacediff.WorkingTreeTarget()
			}
			return m.openPreviewDiff(spec)
		}
		root := ""
		if ok {
			root = selected.Path
		}
		return m.openPreviewDiff(uirequest.DiffTarget(root, pv.Target.Value))
	case uirequest.TargetKindResource:
		ref, refusal := resourceview.ReferenceForLocator(m.previewResourceMatchers(), pv.Target.Provider, pv.Target.Value)
		if refusal != "" {
			return nil
		}
		return m.OpenPreviewResource(ref)
	}
	return nil
}

func (m *Model) pendingViewBadge(tmuxName string) (string, bool) {
	if m.pendingViews == nil {
		return "", false
	}
	pv, ok := m.pendingViews[tmuxName]
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
