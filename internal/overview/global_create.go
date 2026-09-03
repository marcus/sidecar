package overview

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/agentcatalog"
	"github.com/marcus/sidecar/internal/agentcontrol"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/hosts"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/shellstate"
	"github.com/marcus/sidecar/internal/termpanes"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspaceinventory"
	"github.com/marcus/sidecar/internal/workspaceops"
)

const (
	globalCreateConfirmID = "global-create-confirm"
	globalCreateRetryID   = "global-create-retry"
	globalCreateOpenID    = "global-create-open"
	globalCreateDeleteID  = "global-create-delete"
	globalCreateCancelID  = "global-create-cancel"
	globalCreateActionID  = "global-create-shell"
)

var (
	createManagedShell    = workspaceops.CreateManagedShell
	resolveGlobalWorktree = workspaceops.ResolveWorktreePlan
	executeGlobalWorktree = workspaceops.ExecuteWorktree
	persistGlobalJournal  = workspaceops.PersistPendingCreation
	persistGlobalIdentity = workspaceops.PersistWorktreeIdentity
	runGlobalSetup        = workspaceops.RunConfiguredSetup
	removeGlobalJournal   = workspaceops.RemovePendingCreation
	deleteGlobalWorktree  = workspaceops.DeleteCreatedWorktree
	launchGlobalSession   = workspaceops.LaunchWorktreeSession
	waitGlobalShellReady  = func(ctx context.Context, target agentcontrol.Target, timeout time.Duration) (agentcontrol.Snapshot, error) {
		return (agentcontrol.Service{Terminal: agentcontrol.NewLocalTerminal()}).WaitShellReady(ctx, target, timeout)
	}
	startGlobalAgent = func(ctx context.Context, req agentcontrol.StartRequest) (agentcontrol.Agent, error) {
		return (agentcontrol.Service{Terminal: agentcontrol.NewLocalTerminal()}).Start(ctx, req)
	}
	listCreateBranches    = workspaceops.ListLocalBranches
	currentCreateBranch   = workspaceops.CurrentBranch
	resolveGlobalAgentCmd = workspaceops.ResolveAgentCommand
)

const globalAgentStartTimeout = 30 * time.Second

// remoteReply is the fence a host's answer carries back.
//
// Empty HostID means the work happened on this machine and nothing needs
// fencing. A remote answer names the host it was addressed to and the
// host-client incarnation that was current when it was sent, so a reply that
// arrives after a config save removed or retargeted that host can be
// recognised and dropped — the same two fences handleHostUpdate applies to the
// snapshot stream, not a parallel mechanism.
type remoteReply struct {
	HostID      string
	Incarnation uint64
}

type globalShellCreatedMsg struct {
	remoteReply
	Project Project
	Tmux    string
	Err     error
}

type globalWorktreePlannedMsg struct {
	remoteReply
	Project Project
	Plan    *workspaceops.WorktreePlan
	Err     error
}

type globalWorktreeCreatedMsg struct {
	remoteReply
	Project  Project
	Plan     *workspaceops.WorktreePlan
	Record   *workspaceops.WorktreeRecord
	Outcomes []workspaceops.SetupOutcome
	Err      error
	// RemotePath and RemoteSession are what a host reported creating. A remote
	// creation produces no local Record on purpose: a WorktreeRecord is a
	// handle onto a checkout this machine can act on, and the recovery flow it
	// feeds — retry setup, open anyway, delete — is entirely local git.
	RemotePath    string
	RemoteSession string
}

type globalWorktreeDeletedMsg struct {
	Project Project
	Err     error
}

type globalWorkspaceLaunchedMsg struct {
	Project Project
	Plan    *workspaceops.WorktreePlan
	Record  *workspaceops.WorktreeRecord
	Result  workspaceops.AgentLaunchResult
	Err     error
}

type projectMutationRefreshMsg struct {
	Project Project
	Result  workspaceinventory.ProjectResult
	Err     error
	// Background marks a refresh nobody asked for — a manifest watcher signal or
	// a sweep tick rather than a create or delete this surface just performed.
	// Its failures must stay silent: raising the create modal's error on a
	// project the user never touched would be an alert about nothing.
	Background bool
	// DispatchedAt dates the durable state this result was built from, so a
	// background refresh that has been overtaken can be recognised and dropped.
	DispatchedAt time.Time
}

type globalCreateBranchesMsg struct {
	ProjectKey string
	Branches   []string
	Current    string
}

func (m *Model) CreateOpen() bool { return m.createOpen }

func (m *Model) OpenCreateShell(projectKey string) tea.Cmd {
	return m.openCreate(projectKey, workspacecreate.KindShell, false, false)
}

func (m *Model) OpenCreateWorktree(projectKey string) tea.Cmd {
	return m.openCreate(projectKey, workspacecreate.KindWorktree, false, false)
}

// OpenCreate opens the shared chooser used by header and section + actions.
// A section supplies the project answer but leaves the capability choice live.
func (m *Model) OpenCreate(projectKey string) tea.Cmd {
	return m.openCreate(projectKey, workspacecreate.KindWorktree, true, false)
}

// OpenPaneSwitcher opens the create modal as the pane switcher: kind list
// focused, on the row it was last left on. It is the global browser's half of
// the entry the project workspace binds — a pane opens beside what you are
// reading without first leaving it — and both surfaces answer the same key in
// the same contexts, which internal/keymap's parity test holds them to.
func (m *Model) OpenPaneSwitcher() tea.Cmd {
	return m.openCreate("", workspacecreate.KindWorktree, true, true)
}

func (m *Model) openCreate(projectKey string, kind workspacecreate.Kind, focusKind, useLastKind bool) tea.Cmd {
	// A machine with no local projects but a registered host still has
	// somewhere to create; with no hosts this is len(m.projects) exactly as
	// before.
	projectItems := m.createProjectItems()
	if m.PreviewInteractive() || len(projectItems) == 0 {
		return nil
	}
	m.closeViewFlyout()
	m.closeRenameShell()
	key := m.normalizedCreateProjectKey(projectKey)
	agents := []string(nil)
	defaultAgent := ""
	if m.config != nil {
		agents = m.config.Plugins.Workspace.Agents
		defaultAgent = strings.TrimSpace(m.config.Plugins.Workspace.DefaultAgentType)
	}
	allowTerminalSplit := features.IsEnabled(features.WorkspaceTerminalPanel.Name)
	terminalDisabled := ""
	if allowTerminalSplit && panelayout.LiveCapReached(m.preview.paneRoot) {
		terminalDisabled = termpanes.CapDisabledReason
	}
	terminalName := "Terminal"
	if workspace, ok := m.SelectedWorkspace(); ok && strings.TrimSpace(workspace.Name) != "" {
		terminalName = workspace.Name + " Terminal"
	}
	m.createForm = workspacecreate.Open(workspacecreate.OpenOpts{
		Kind:                  kind,
		FocusKind:             focusKind,
		UseLastKind:           useLastKind,
		ShowProject:           true,
		ProjectKey:            key,
		Projects:              projectItems,
		Agents:                agents,
		NextShell:             m.defaultShellDisplayName(key),
		DefaultAgent:          defaultAgent,
		AllowTerminalSplit:    allowTerminalSplit,
		TerminalSplitDisabled: terminalDisabled,
		TerminalName:          terminalName,
		ShowNotes:             m.notesWanted(),
		Providers:             m.configuredProviders(),
	})
	m.createOpen = true
	m.createError = ""
	m.createWarning = ""
	m.createBusy = false
	m.createPlan = nil
	m.createRecord = nil
	m.createModal = nil
	m.createModalWidth = 0
	m.createTargetHost = ""
	return tea.Batch(m.loadCreateBranches(), m.loadCreatePickerData(), m.loadCreateFileCandidates())
}

func (m *Model) normalizedCreateProjectKey(explicit string) string {
	key := m.defaultCreateProject(explicit)
	if target, ok := m.resolveCreateTarget(key); ok {
		return projectKey(target.Project)
	}
	return key
}

func (m *Model) defaultCreateProject(explicit string) string {
	if _, ok := m.resolveCreateTarget(explicit); ok {
		return explicit
	}
	// A remote row selected in the list defaults the form to its own project,
	// the same way a local one does.
	if selected, ok := m.SelectedWorkspace(); ok {
		if _, ok := m.resolveCreateTarget(selected.ProjectKey); ok {
			return selected.ProjectKey
		}
	}
	if last := loadLastGlobalCreateProject(); last != "" {
		if _, ok := m.resolveCreateTarget(last); ok {
			return last
		}
	}
	if len(m.projects) > 0 {
		return projectKey(m.projects[0])
	}
	if items := m.createProjectItems(); len(items) > 0 {
		return items[0].Key
	}
	return ""
}

func (m *Model) projectIndex(key string) int {
	for i, project := range m.projects {
		if projectKey(project) == key || project.Path == key {
			return i
		}
	}
	return -1
}

func (m *Model) selectedCreateProject() (Project, bool) {
	if m.createForm == nil {
		return Project{}, false
	}
	idx := m.projectIndex(m.createForm.ProjectKey())
	if idx < 0 {
		return Project{}, false
	}
	return m.projects[idx], true
}

func (m *Model) shellDefinitions(key string) []shellstate.Definition {
	defs := make([]shellstate.Definition, 0)
	for _, workspace := range m.projectWorkspaces(key) {
		if workspace.Kind != workspaceinventory.KindShell {
			continue
		}
		defs = append(defs, shellstate.Definition{TmuxName: workspace.TmuxName, DisplayName: workspace.Name, Namespace: workspace.Namespace, WorkDir: workspace.Path})
	}
	return defs
}

// defaultShellDisplayName is the placeholder the form shows for the next shell.
//
// It is a placeholder and nothing more: for a remote project it is a guess made
// from the rows the last snapshot carried, and the host decides the real name
// when it creates the shell. ShellNames is pure string work over the path's last
// element, so asking it about a remote path reaches no filesystem.
func (m *Model) defaultShellDisplayName(key string) string {
	target, ok := m.resolveCreateTarget(key)
	if !ok {
		return "Shell 1"
	}
	display, _ := workspaceops.ShellNames(target.Project.Path, m.shellDefinitions(projectKey(target.Project)))
	return display
}

func (m *Model) ensureCreateShellModal() { m.ensureCreateModal() }

func (m *Model) createModalContentWidth() int {
	modalW := 52
	if m.width > 0 && modalW > m.width-4 {
		modalW = m.width - 4
	}
	if modalW < 24 {
		modalW = 24
	}
	return modalW
}

func (m *Model) ensureCreateModal() {
	if !m.createOpen {
		return
	}
	modalW := m.createModalContentWidth()
	if m.createPlan != nil {
		if m.createModal != nil && m.createModalWidth == modalW {
			return
		}
		m.createModalWidth = modalW
		m.ensureCreatePlanModal(modalW)
		return
	}
	if m.createForm == nil {
		return
	}
	m.createForm.Build(modalW)
	if m.createError != "" {
		m.createForm.SetError(m.createError)
	}
}

func (m *Model) activeCreateModal() *modal.Modal {
	if m.createPlan != nil {
		return m.createModal
	}
	if m.createForm != nil {
		return m.createForm.Modal()
	}
	return m.createModal
}

// createProjectItems is every project a workspace can be created in: this
// machine's, then each host's.
//
// A host's projects are labelled the way its rows already are — host, then the
// separator, then the project — so the name in the picker is the name on the
// row it will produce. Their keys are host-scoped already (hosts.ScopedKey), so
// two machines with the same checkout path stay two entries.
func (m *Model) createProjectItems() []workspacecreate.ProjectItem {
	items := make([]workspacecreate.ProjectItem, 0, len(m.projects))
	for _, project := range m.projects {
		items = append(items, workspacecreate.ProjectItem{Key: projectKey(project), Label: project.Name})
	}
	// Hidden machines are left out. Creating a workspace whose row the browser
	// would then withhold is a create that appears to have done nothing, and
	// the pending-selection it leaves behind never resolves.
	for _, id := range m.shownHostOrder() {
		for _, project := range m.hostProjects[id] {
			items = append(items, workspacecreate.ProjectItem{
				Key:   projectKey(project),
				Label: id + hostRowPrefix + project.Name,
			})
		}
	}
	return items
}

func (m *Model) setCreateError(msg string) {
	m.createError = msg
	if m.createForm != nil && m.createPlan == nil {
		m.createForm.SetError(msg)
	}
}

func (m *Model) loadCreateBranches() tea.Cmd {
	target, ok := m.selectedCreateTarget()
	if !ok {
		return nil
	}
	if target.Remote() {
		// Running `git branch` here would run it on THIS machine against the
		// host's path, which on a similarly laid out second machine answers
		// with the wrong repository's branches. The host resolves its own
		// default base ref when no --base is passed; that is the honest answer
		// until a verb exists to ask it for the list. And the list a LOCAL
		// project loaded earlier must not survive the switch: its branches and
		// its prefilled base describe this machine's repository, and leaving
		// them in the form offers a --base the host resolves against a
		// different history.
		if m.createForm != nil {
			m.createForm.SetBranches(nil, "")
		}
		return nil
	}
	project := target.Project
	key := projectKey(project)
	dir := project.Path
	return func() tea.Msg {
		branches, err := listCreateBranches(context.Background(), dir)
		if err != nil {
			branches = nil
		}
		current, _ := currentCreateBranch(context.Background(), dir)
		if current == "HEAD" {
			current = ""
		}
		return globalCreateBranchesMsg{ProjectKey: key, Branches: branches, Current: current}
	}
}

func (m *Model) applyCreateBranches(msg globalCreateBranchesMsg) {
	if m.createForm == nil || m.createForm.ProjectKey() != msg.ProjectKey {
		return
	}
	m.createForm.SetBranches(msg.Branches, msg.Current)
}

func (m *Model) ensureCreatePlanModal(modalW int) {
	plan := m.createPlan
	if plan == nil {
		return
	}
	sections := []modal.Section{modal.Text(createPlanSummary(plan, m.createTargetHost))}
	if m.createError != "" {
		sections = append(sections, modal.Spacer(), modal.Text("Error: "+m.createError))
	}
	if m.createWarning != "" {
		sections = append(sections, modal.Spacer(), modal.Text("Warning: "+m.createWarning))
	}
	if m.createBusy {
		sections = append(sections, modal.Spacer(), modal.Text("Creating worktree and running setup…"))
	}
	var buttons modal.Section
	primary := globalCreateConfirmID
	if m.createRecord != nil {
		primary = globalCreateRetryID
		buttons = modal.Buttons(
			modal.Btn(" Retry setup ", globalCreateRetryID, modal.BtnPrimary()),
			modal.Btn(" Open anyway ", globalCreateOpenID),
			modal.Btn(" Delete ", globalCreateDeleteID),
		)
	} else {
		buttons = modal.Buttons(modal.Btn(" Create ", globalCreateConfirmID, modal.BtnPrimary()), modal.Btn(" Cancel ", globalCreateCancelID))
	}
	sections = append(sections, modal.Spacer(), buttons)
	m.createModal = modal.New("Confirm Worktree", modal.WithWidth(modalW), modal.WithPrimaryAction(primary))
	for _, section := range sections {
		m.createModal.AddSection(section)
	}
}

// createPlanSummary is the confirmation's description of a resolved worktree
// plan, for a plan resolved here or on a host.
//
// One renderer, because a plan is a plan: the fields mean the same thing
// whichever machine resolved them, and two renderers is how a local and a
// remote confirmation come to say different things about the same operation.
func createPlanSummary(plan *workspaceops.WorktreePlan, hostID string) string {
	var sb strings.Builder
	if hostID != "" {
		fmt.Fprintf(&sb, "On %s\n\n", hostID)
	}
	fmt.Fprintf(&sb, "Create %s at\n%s\n\nFrom %s (%s)\n%s\n%s",
		plan.Branch, plan.Path, plan.SourceRef, shortCreateOID(plan.SourceOID),
		plan.RemotePolicy, createPlanHookLine(plan))
	return sb.String()
}

// createPlanHookLine states whether creating this worktree will run a setup
// hook, and whether creation depends on it succeeding.
//
// A line of text and not a checkbox, deliberately. This confirmation says what
// is about to happen; choosing whether the hook runs belongs to configuration
// and to the project surface's richer form, and adding a second place to decide
// it would make the two surfaces disagree about the answer.
func createPlanHookLine(plan *workspaceops.WorktreePlan) string {
	if !plan.RunHook {
		return "No setup hook"
	}
	hook := strings.TrimSpace(plan.HookPath)
	if hook == "" {
		hook = "setup hook"
	}
	if plan.HookRequired {
		return "Runs " + hook + " — required, creation fails if it does"
	}
	return "Runs " + hook + " — optional"
}

func shortCreateOID(oid string) string {
	if len(oid) > 8 {
		return oid[:8]
	}
	return oid
}

func (m *Model) overlayCreateShell(background string, width, height int) string {
	m.ensureCreateShellModal()
	md := m.activeCreateModal()
	if md == nil {
		return background
	}
	rendered := md.Render(width, height, m.createMouse)
	return ui.OverlayModal(background, rendered, width, height)
}

func (m *Model) closeCreateShell() {
	m.createOpen = false
	m.createBusy = false
	m.createError = ""
	m.createWarning = ""
	m.createForm = nil
	m.createModal = nil
	m.createModalWidth = 0
	m.createPlan = nil
	m.createRecord = nil
	m.createTargetHost = ""
}

func (m *Model) CreatePaste(value string) bool {
	if !m.createOpen || m.createBusy || m.createPlan != nil || m.createForm == nil {
		return false
	}
	m.ensureCreateModal()
	md := m.createForm.Modal()
	if md == nil {
		return false
	}
	prev := md.FocusedID()
	md.SetFocus(workspacecreate.FieldName)
	for _, r := range value {
		_, _ = md.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if prev != "" && prev != workspacecreate.FieldName {
		md.SetFocus(prev)
	}
	m.createForm.SyncAfterInput()
	m.setCreateError("")
	return true
}

func (m *Model) handleCreateShellKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	m.ensureCreateShellModal()
	if m.createBusy {
		return true, nil
	}
	if m.createPlan != nil {
		if m.createModal == nil {
			return true, nil
		}
		action, cmd := m.createModal.HandleKey(msg)
		return true, tea.Batch(cmd, m.applyCreateAction(action))
	}
	md := m.activeCreateModal()
	if md == nil {
		return true, nil
	}
	prevProject := ""
	prevStep := workspacecreate.StepKind
	if m.createForm != nil {
		prevProject = m.createForm.ProjectKey()
		prevStep = m.createForm.Step()
	}
	// The form owns the two-step flow: Esc on the picker step returns to the
	// kind list instead of closing, and Enter on a target-needing kind
	// advances to it. What escapes is an action for this switch.
	var action string
	var cmd tea.Cmd
	if m.createForm != nil {
		action, cmd = m.createForm.HandleKey(msg)
	} else {
		action, cmd = md.HandleKey(msg)
	}
	return true, tea.Batch(cmd, m.finishCreateInput(action, prevProject, prevStep))
}

func (m *Model) handleCreateShellMouse(msg tea.MouseMsg) tea.Cmd {
	m.ensureCreateShellModal()
	if m.createBusy {
		return nil
	}
	if m.createPlan != nil {
		if m.createModal == nil {
			return nil
		}
		action := m.createModal.HandleMouse(msg, m.createMouse)
		return m.applyCreateAction(action)
	}
	md := m.activeCreateModal()
	if md == nil {
		return nil
	}
	prevProject := ""
	prevStep := workspacecreate.StepKind
	if m.createForm != nil {
		prevProject = m.createForm.ProjectKey()
		prevStep = m.createForm.Step()
	}
	action := md.HandleMouse(msg, m.createMouse)
	if action == workspacecreate.FieldSkip {
		_, _ = md.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	}
	if m.createForm != nil {
		action = m.createForm.TranslateMouseAction(action)
	}
	return m.finishCreateInput(action, prevProject, prevStep)
}

func (m *Model) finishCreateInput(action, previousProject string, previousStep workspacecreate.FormStep) tea.Cmd {
	if m.createForm != nil {
		m.createForm.SyncAfterInput()
	}
	var reload tea.Cmd
	if m.createForm != nil && m.createForm.ProjectKey() != previousProject {
		reload = m.loadCreateBranches()
	}
	if m.remoteSelection() && m.createForm != nil && m.createForm.Step() == workspacecreate.StepTarget && previousStep != workspacecreate.StepTarget {
		reload = tea.Batch(reload, m.loadCreatePickerData())
	}
	if action == "" {
		m.setCreateError("")
	}
	return tea.Batch(reload, m.applyCreateAction(action))
}

func (m *Model) applyCreateAction(action string) tea.Cmd {
	if workspacecreate.IsPlacementAction(action) {
		// On the picker step one click creates with that placement; from the
		// kind list of a target-needing kind it continues there instead.
		if m.createForm == nil {
			return nil
		}
		if m.createForm.ApplyPlacementActionStep(action) == workspacecreate.PlacementSubmitted {
			return m.applyCreateAction(workspacecreate.ActionCreate)
		}
		return nil
	}
	switch action {
	case "cancel", workspacecreate.ActionCancel, globalCreateCancelID:
		if m.createRecord != nil {
			// Once Git has mutated, escape/cancel means retain the usable
			// worktree; it must never silently abandon recovery state.
			return m.openCreatedWorktreeAnyway()
		}
		m.clearPendingCreated()
		m.closeCreateShell()
		return nil
	case workspacecreate.ActionCreate:
		if m.createForm == nil {
			return nil
		}
		if m.createForm.Step() == workspacecreate.StepTarget {
			return m.submitPaneTargetForm()
		}
		if m.createForm.Kind() == workspacecreate.KindTerminalSplit {
			return m.createPreviewTerminalSplit()
		}
		if m.createForm.Kind() == workspacecreate.KindWorktree {
			return m.planCreateWorktree()
		}
		return m.submitCreateShell()
	case globalCreateConfirmID:
		return m.executeCreateWorktree()
	case globalCreateRetryID:
		return m.retryCreateSetup()
	case globalCreateOpenID:
		return m.openCreatedWorktreeAnyway()
	case globalCreateDeleteID:
		return m.deleteCreatedWorktree()
	}
	return nil
}

func (m *Model) planCreateWorktree() tea.Cmd {
	target, ok := m.selectedCreateTarget()
	if !ok {
		// The user did choose a project; what vanished is the host it lived on.
		// "Choose a project" would blame the choice for a machine going away.
		m.setCreateError(missingCreateTarget(m.createFormHostID()))
		return nil
	}
	if reason := m.remoteHostUnavailable(target.HostID); reason != "" {
		m.setCreateError(reason)
		return nil
	}
	if m.createForm == nil {
		return nil
	}
	if err := m.createForm.Validate(); err != "" {
		m.setCreateError(err)
		return nil
	}
	project := target.Project
	setup := config.WorktreeSetupConfig{}
	dirPrefix := true
	if m.config != nil {
		setup = m.config.WorktreeSetupForProject(project.Path)
		dirPrefix = m.config.Plugins.Workspace.DirPrefix
	}
	name := strings.TrimSpace(m.createForm.Name())
	base := m.createForm.BaseBranch()
	agent := m.createForm.Agent()
	skip := m.createForm.SkipPerms()
	m.createBusy = true
	m.setCreateError("")
	m.createModal = nil
	// Set on every submission, never adjusted on noticing a difference: that is
	// the rule that keeps a surface which went remote once from staying remote
	// for the next local action.
	m.createTargetHost = target.HostID
	_ = saveLastGlobalCreateProject(lastCreateProjectValue(target))
	m.createForm.PersistLastAgent()
	if target.Remote() {
		// The host resolves the plan with its own config, its own setup hook
		// and its own repository. --plan mutates nothing, so a cancelled
		// confirmation leaves that machine untouched.
		return m.planRemoteWorktree(target, name, base, agent, skip)
	}
	return func() tea.Msg {
		plan, err := resolveGlobalWorktree(context.Background(), project.Path, project.Path, name, base, dirPrefix, setup)
		if plan != nil {
			plan.RepoKey = projectKey(project)
			plan.OperationID = fmt.Sprintf("global-%d", time.Now().UnixNano())
			plan.AgentType = agent
			plan.SkipPerms = skip
		}
		return globalWorktreePlannedMsg{Project: project, Plan: plan, Err: err}
	}
}

func (m *Model) executeCreateWorktree() tea.Cmd {
	if m.createPlan == nil {
		return nil
	}
	target, ok := m.selectedCreateTarget()
	if !ok {
		// The project the confirmation was built for is no longer resolvable —
		// a host removed or retargeted between the plan and the Create press.
		// Every other host-removal path in this file says so; a confirmation
		// that answers a keypress with nothing is the one behaviour a user
		// cannot tell from a hang.
		m.createBusy = false
		m.createModal = nil
		m.createPlan = nil
		m.setCreateError(missingCreateTarget(m.createTargetHost))
		return nil
	}
	if reason := m.remoteHostUnavailable(target.HostID); reason != "" {
		// Registered but disabled or not connected: dispatching would fail on
		// ssh and come back through hostReplyStale as the misleading "was
		// removed or retargeted". Refuse up front with the real reason.
		m.createBusy = false
		m.createModal = nil
		m.createPlan = nil
		m.setCreateError(reason)
		return nil
	}
	project := target.Project
	plan := m.createPlan
	m.createBusy = true
	m.createError = ""
	m.createModal = nil
	m.createTargetHost = target.HostID
	if target.Remote() {
		// The host runs its whole create sequence — execute, journal, identity,
		// configured setup, launch — because that sequence is
		// `sidecar create worktree`. It re-resolves the plan from the same
		// arguments the confirmation was built from, pinned to the confirmed
		// plan's SourceOID so a ref that moved on the host in the meantime is
		// refused there, exactly as the local ExecuteWorktree refuses here.
		if m.createForm == nil {
			m.createBusy = false
			m.createPlan = nil
			m.setCreateError(missingCreateTarget(target.HostID))
			return nil
		}
		return m.executeRemoteWorktree(target,
			strings.TrimSpace(m.createForm.Name()), m.createForm.BaseBranch(),
			m.createForm.Agent(), plan.SourceOID, m.createForm.SkipPerms())
	}
	return func() tea.Msg {
		record, err := executeGlobalWorktree(context.Background(), projectKey(project), plan)
		if record == nil {
			return globalWorktreeCreatedMsg{Project: project, Plan: plan, Err: err}
		}
		outcomes := make([]workspaceops.SetupOutcome, 0)
		if journalErr := persistGlobalJournal(context.Background(), plan, record); journalErr != nil {
			outcomes = append(outcomes, workspaceops.SetupOutcome{Kind: "journal", Action: "persist recovery", Required: true, Err: journalErr})
		}
		if err != nil {
			return globalWorktreeCreatedMsg{Project: project, Plan: plan, Record: record, Outcomes: outcomes, Err: err}
		}
		outcomes = append(outcomes, persistGlobalIdentity(context.Background(), plan)...)
		outcomes = append(outcomes, runGlobalSetup(context.Background(), plan)...)
		return globalWorktreeCreatedMsg{Project: project, Plan: plan, Record: record, Outcomes: outcomes, Err: err}
	}
}

func failedCreateOutcomes(outcomes []workspaceops.SetupOutcome, requiredOnly bool) []workspaceops.SetupOutcome {
	var failed []workspaceops.SetupOutcome
	for _, outcome := range outcomes {
		if outcome.Err != nil && (!requiredOnly || outcome.Required) {
			failed = append(failed, outcome)
		}
	}
	return failed
}

func summarizeCreateOutcomes(outcomes []workspaceops.SetupOutcome) string {
	parts := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		parts = append(parts, outcome.Action+": "+outcome.Err.Error())
	}
	return strings.Join(parts, "; ")
}

func (m *Model) retryCreateSetup() tea.Cmd {
	if m.createPlan == nil || m.createRecord == nil {
		return nil
	}
	project, ok := m.selectedCreateProject()
	if !ok {
		return nil
	}
	plan, record := m.createPlan, m.createRecord
	m.createBusy = true
	m.createError, m.createWarning = "", ""
	m.createModal = nil
	return func() tea.Msg {
		outcomes := append(persistGlobalIdentity(context.Background(), plan), runGlobalSetup(context.Background(), plan)...)
		return globalWorktreeCreatedMsg{Project: project, Plan: plan, Record: record, Outcomes: outcomes}
	}
}

func (m *Model) openCreatedWorktreeAnyway() tea.Cmd {
	project, ok := m.selectedCreateProject()
	if !ok || m.createPlan == nil || m.createRecord == nil {
		return nil
	}
	if err := removeGlobalJournal(m.createPlan); err != nil {
		m.createError = "finalize pending creation journal before opening: " + err.Error()
		m.createModal = nil
		m.setCreateError(m.createError)
		return nil
	}
	return m.launchCreatedWorktree(project, m.createPlan, m.createRecord)
}

func (m *Model) launchCreatedWorktree(project Project, plan *workspaceops.WorktreePlan, record *workspaceops.WorktreeRecord) tea.Cmd {
	if plan == nil || record == nil {
		return nil
	}
	configured := map[string]string(nil)
	if m.config != nil {
		configured = m.config.Plugins.Workspace.AgentStart
	}
	startAgent := plan.AgentType != ""
	command := ""
	var launchArgv []string
	var launchErr error
	if startAgent {
		command = resolveGlobalAgentCmd(record.Path, plan.AgentType, configured, plan.SkipPerms)
		launchArgv, launchErr = globalAgentLaunchArgv(plan.AgentType, plan.SkipPerms, command, nil)
	}
	spec := workspaceops.AgentLaunchSpec{
		SessionName: workspaceops.WorktreeSessionName(record.Path, record.Name), WorkDir: record.Path,
		TaskID: plan.TaskID, Env: workspaceops.BuildEnvOverrides(plan.MainWorktree), StartAgent: false,
	}
	m.createBusy = true
	m.createModal = nil
	return func() tea.Msg {
		if launchErr != nil {
			return globalWorkspaceLaunchedMsg{Project: project, Plan: plan, Record: record, Err: launchErr}
		}
		result, err := launchGlobalSession(context.Background(), spec)
		if err == nil && startAgent {
			target := agentcontrol.Target{Host: "local", Project: projectKey(project), Session: spec.SessionName, Name: record.Name}
			if !result.Reconnected {
				_, err = waitGlobalShellReady(context.Background(), target, globalAgentStartTimeout)
			}
			if err != nil {
				return globalWorkspaceLaunchedMsg{Project: project, Plan: plan, Record: record, Result: result, Err: fmt.Errorf("prepare agent shell: %w", err)}
			}
			started, startErr := startGlobalAgent(context.Background(), agentcontrol.StartRequest{
				Target: target,
				Kind:   plan.AgentType, Argv: launchArgv, Timeout: globalAgentStartTimeout,
			})
			if startErr != nil {
				err = fmt.Errorf("start agent: %w", startErr)
			} else if started.Target.PaneID != "" {
				result.PaneID = started.Target.PaneID
			}
		}
		return globalWorkspaceLaunchedMsg{Project: project, Plan: plan, Record: record, Result: result, Err: err}
	}
}

func (m *Model) deleteCreatedWorktree() tea.Cmd {
	project, ok := m.selectedCreateProject()
	if !ok || m.createPlan == nil || m.createRecord == nil {
		return nil
	}
	plan, record := m.createPlan, m.createRecord
	m.createBusy = true
	m.createModal = nil
	return func() tea.Msg {
		err := deleteGlobalWorktree(context.Background(), plan, record)
		if err == nil {
			_ = removeGlobalJournal(plan)
		}
		return globalWorktreeDeletedMsg{Project: project, Err: err}
	}
}

func (m *Model) submitCreateShell() tea.Cmd {
	target, ok := m.selectedCreateTarget()
	if !ok {
		// See planCreateWorktree: the project was chosen; its host went away.
		m.setCreateError(missingCreateTarget(m.createFormHostID()))
		return nil
	}
	if reason := m.remoteHostUnavailable(target.HostID); reason != "" {
		m.setCreateError(reason)
		return nil
	}
	project := target.Project
	key := projectKey(project)
	display, session := workspaceops.ShellNames(project.Path, m.shellDefinitions(key))
	custom := ""
	if m.createForm != nil {
		custom = strings.TrimSpace(m.createForm.Name())
	}
	if custom != "" {
		var err error
		display, err = shellstate.NormalizeName(custom)
		if err != nil {
			m.setCreateError(err.Error())
			return nil
		}
	}
	cols, rows := max(20, m.width/2-4), max(5, m.height-4)
	agent := ""
	skip := false
	if m.createForm != nil {
		agent = m.createForm.Agent()
		skip = m.createForm.SkipPerms()
	}
	spec := workspaceops.ManagedShellSpec{
		ShellSpec:   workspaceops.ShellSpec{WorkDir: project.Path, SessionName: session, DisplayName: display, Cols: cols, Rows: rows},
		ProjectRoot: project.Path,
		AgentType:   agent,
		SkipPerms:   skip,
	}
	m.createBusy = true
	m.setCreateError("")
	m.createModal = nil
	// Set unconditionally on every submission — see planCreateWorktree.
	m.createTargetHost = target.HostID
	m.pendingCreatedHost = target.HostID
	m.pendingCreatedPath = ""
	m.pendingCreatedTmux = session
	if target.Remote() {
		// The host names the session from its own manifest, so there is nothing
		// to pend on until it answers.
		m.pendingCreatedTmux = ""
	}
	_ = saveLastGlobalCreateProject(lastCreateProjectValue(target))
	if m.createForm != nil {
		m.createForm.PersistLastAgent()
	}
	if target.Remote() {
		return m.submitRemoteCreateShell(target, remoteShellName(custom, display), agent, m.remoteAgentCommand(agent, skip))
	}
	return func() tea.Msg {
		_, err := createManagedShell(spec)
		if err == nil && agent != "" {
			configured := map[string]string(nil)
			if m.config != nil {
				configured = m.config.Plugins.Workspace.AgentStart
			}
			command := resolveGlobalAgentCmd(project.Path, agent, configured, skip)
			command = withGlobalShellNaming(command, agent)
			extra := globalShellNamingArgv(agent)
			var launchArgv []string
			launchArgv, err = globalAgentLaunchArgv(agent, skip, command, extra)
			if err == nil {
				target := agentcontrol.Target{Host: "local", Project: projectKey(project), Session: session, Name: display}
				_, err = waitGlobalShellReady(context.Background(), target, globalAgentStartTimeout)
				if err != nil {
					return globalShellCreatedMsg{Project: project, Tmux: session, Err: fmt.Errorf("prepare agent shell: %w", err)}
				}
				_, err = startGlobalAgent(context.Background(), agentcontrol.StartRequest{
					Target: target,
					Kind:   agent, Argv: launchArgv, Timeout: globalAgentStartTimeout,
				})
			}
		}
		return globalShellCreatedMsg{Project: project, Tmux: session, Err: err}
	}
}

func globalAgentLaunchArgv(agent string, skip bool, resolvedCommand string, extra []string) ([]string, error) {
	base, err := agentcatalog.BuildLaunch(agent, nil, skip)
	if err != nil {
		return nil, fmt.Errorf("build agent launch: %w", err)
	}
	structured, err := agentcatalog.BuildLaunch(agent, extra, skip)
	if err != nil {
		return nil, fmt.Errorf("build agent launch: %w", err)
	}
	defaultCommand := strings.Join(base, " ")
	if len(extra) > 0 {
		defaultCommand = withGlobalShellNaming(defaultCommand, agent)
	}
	if strings.TrimSpace(resolvedCommand) == defaultCommand {
		return structured, nil
	}
	// User configuration and .sidecar-agent-start are opaque shell snippets.
	// The wrapper preserves their behavior while agentcontrol still verifies
	// the expected live provider and readiness. These argv are not safe launch
	// metadata and must not be persisted or replayed as catalog launches.
	return agentcatalog.OpaqueLaunchArgv(resolvedCommand)
}

func globalShellNamingArgv(agent string) []string {
	flag := ""
	switch agent {
	case "claude":
		flag = "--append-system-prompt"
	case "grok":
		flag = "--rules"
	}
	if flag == "" {
		return nil
	}
	return []string{flag, shellstate.NamingInstruction}
}

// remoteShellName is the display name to send with a remote create: the one
// that was typed, or nothing at all.
//
// Never the locally computed default. That default is "Shell N" counted from
// the rows this viewer last saw, so sending it would name a shell after the
// wrong machine's numbering and race any shell created there since. Sending
// nothing lets the host name it from its own manifest, which is what the local
// path also does.
func remoteShellName(typed, normalized string) string {
	if strings.TrimSpace(typed) == "" {
		return ""
	}
	return normalized
}

// remoteAgentCommand resolves the command that starts the chosen agent in a
// shell on another machine, or "" when no agent was chosen.
//
// Config-only resolution, and the same naming instruction the local path
// appends. See workspaceops.ResolveAgentCommandFromConfig: reading the
// checkout's .sidecar-agent-start for a remote path reads this machine's file.
func (m *Model) remoteAgentCommand(agent string, skip bool) string {
	if strings.TrimSpace(agent) == "" {
		return ""
	}
	configured := map[string]string(nil)
	if m.config != nil {
		configured = m.config.Plugins.Workspace.AgentStart
	}
	return withGlobalShellNaming(resolveRemoteAgentCmd(agent, configured, skip), agent)
}

// lastCreateProjectValue is what "create here again next time" remembers.
//
// A remote project is remembered by its host-scoped key, never by its path: a
// path remembered from another machine would match a local project with the
// same path the next time the form opens, and silently create there instead.
func lastCreateProjectValue(target createTarget) string {
	if target.Remote() {
		return projectKey(target.Project)
	}
	return target.Project.Path
}

func withGlobalShellNaming(command, agent string) string {
	flag := ""
	switch agent {
	case "claude":
		flag = "--append-system-prompt"
	case "grok":
		flag = "--rules"
	}
	if flag == "" {
		return command
	}
	return command + " " + flag + " " + workspaceops.ShellQuote(shellstate.NamingInstruction)
}

func (m *Model) refreshProjectAfterMutation(project Project) tea.Cmd {
	return m.refreshOneProject(project, false)
}

// refreshOneProject re-inventories exactly one project and folds the result
// back into the board. This is the cheap path — one Git worktree listing and
// one tmux inventory — as opposed to a full cycle's fan-out across every
// configured project, and it is what both a local mutation and a cross-instance
// manifest change use.
func (m *Model) refreshOneProject(project Project, background bool) tea.Cmd {
	return m.refreshOneProjectWithPanes(project, background, nil)
}

// refreshOneProjectWithPanes is refreshOneProject with the tmux inventory
// supplied. A nil panes slice means "collect a fresh one", which is what a
// mutation needs: a shell created a moment ago is not in any inventory taken
// before it. A caller holding panes from a just-completed cycle passes them
// instead, saving a subprocess spawn per project.
func (m *Model) refreshOneProjectWithPanes(project Project, background bool, panes []workspaceinventory.Pane) tea.Cmd {
	// A host-scoped key names a project on another machine (hosts.ScopedKey),
	// and everything below answers a question about THIS one: it stats a path,
	// runs git in it, asks the local tmux server about it, and folds the answer
	// back under that key. That is rule 1 of remote_actions.go — a remote path
	// is never resolved here — and on two machines with the same checkout
	// layout it does not fail, it succeeds against the wrong repository.
	//
	// Refused at the funnel rather than at each call site because the remote
	// mutation replies land in handlers shared with the local ones: a reply that
	// closes a modal must not have to remember which machine it came from to be
	// safe. A remote change becomes visible when its host's next snapshot
	// carries it, which is the only source of truth this surface has for it.
	if _, _, scoped := hosts.SplitScopedKey(projectKey(project)); scoped {
		return nil
	}
	collector := m.collector.ForRefresh(maxCaptures, m.shellClaims)
	roots := append([]string(nil), m.roots...)
	// Snapshot the other projects' results here, on the update goroutine. The
	// command below runs on its own, and ranging over m.results from there would
	// race every write the next cycle makes.
	key := projectKey(project)
	dispatchedAt := time.Now()
	others := make([]workspaceinventory.ProjectResult, 0, len(m.results))
	for existingKey, existing := range m.results {
		if existingKey != key {
			others = append(others, existing)
		}
	}
	return func() tea.Msg {
		ctx := context.Background()
		inventory := collector.CollectProjectInventory(ctx, project.Name, project.Path)
		if inventory.Err != nil {
			return projectMutationRefreshMsg{Project: project, Result: inventory, Err: inventory.Err, Background: background, DispatchedAt: dispatchedAt}
		}
		claimsInputs := append(others, inventory)
		collector = collector.WithShellClaims(workspaceinventory.BuildShellClaims(claimsInputs))
		if panes == nil {
			collected, err := collector.ListPanes(ctx)
			if err != nil {
				return projectMutationRefreshMsg{Project: project, Result: inventory, Err: err, Background: background, DispatchedAt: dispatchedAt}
			}
			panes = collected
		}
		result := collector.RefreshProjectStatus(ctx, inventory, roots, panes)
		return projectMutationRefreshMsg{Project: project, Result: withProjectIdentity(result, project), Err: result.Err, Background: background, DispatchedAt: dispatchedAt}
	}
}

func (m *Model) applyProjectMutationRefresh(msg projectMutationRefreshMsg) tea.Cmd {
	if msg.Err != nil && msg.Background {
		// A background refresh that failed leaves the last good cards alone. The
		// next sweep tick retries, and the full cycle behind it still reports a
		// project that has genuinely gone away.
		m.tracef("background refresh project=%s failed: %v", projectKey(msg.Project), msg.Err)
		return nil
	}
	if msg.Err != nil {
		m.createOpen = true
		m.createBusy = false
		m.createModal = nil
		m.setCreateError(msg.Err.Error())
		return nil
	}
	key := projectKey(msg.Project)
	// A background result replaces the whole project, so one that has been
	// overtaken does not merely show stale data — it removes workspaces a newer
	// read had already found. That does not heal: the live-only poll re-observes
	// the membership it is given and never re-reads durable state, and the
	// manifest digest already matches, so the watcher will not fire again. The
	// project would stay wrong until its next sweep rotation, which is minutes
	// on a large set. Dropping the superseded result is the whole fix.
	//
	// Dated rather than generation-fenced on purpose: m.generation advances on
	// every poll, so fencing on it would discard the watcher refreshes this
	// feature exists to deliver.
	if msg.Background && msg.DispatchedAt.Before(m.inventoryStamp[key]) {
		m.tracef("background refresh project=%s superseded — dropping", key)
		return nil
	}
	m.results[key] = msg.Result
	m.markInventoryFresh(key)
	delete(m.projectErrors, key)
	// The live-only poll keys shell liveness off this map. Without a rebuild
	// here the next pass treats the shell we just created as unclaimed and
	// paints it dead (td-ecb0b8).
	m.syncShellClaims()
	m.syncBoard()
	return m.previewSync()
}

func (m *Model) clearPendingCreated() {
	m.pendingCreatedTmux = ""
	m.pendingCreatedPath = ""
	m.pendingCreatedHost = ""
}

// honorPendingCreated selects a still-pending created workspace once it is
// present in results and visible. Pending stays set until that happens.
//
// The match is scoped to the machine the workspace was created on. Without
// that, a remote worktree at /home/me/api-feature would be answered by a LOCAL
// worktree at the same path, and a tmux session name derived from a directory
// name is even easier to collide on — the row would be selected, the preview
// would open, and everything after it would be about the wrong machine.
func (m *Model) honorPendingCreated() bool {
	if m.pendingCreatedTmux == "" && m.pendingCreatedPath == "" {
		return false
	}
	matches := func(workspace workspaceinventory.Workspace) bool {
		if workspace.HostID != m.pendingCreatedHost {
			return false
		}
		createdShell := m.pendingCreatedTmux != "" && workspace.Kind == workspaceinventory.KindShell && workspace.TmuxName == m.pendingCreatedTmux
		createdWorktree := m.pendingCreatedPath != "" && workspace.Kind == workspaceinventory.KindWorktree && workspace.Path == m.pendingCreatedPath
		return createdShell || createdWorktree
	}
	results := m.results
	if m.pendingCreatedHost != "" {
		results = nil
	}
	for _, result := range results {
		for _, workspace := range result.Workspaces {
			if !matches(workspace) {
				continue
			}
			if m.workspaces.SelectID(workspace.ID) {
				m.clearPendingCreated()
				return true
			}
			return false
		}
	}
	for _, result := range m.hostResults[m.pendingCreatedHost] {
		for _, workspace := range result.Workspaces {
			if !matches(workspace) {
				continue
			}
			if m.workspaces.SelectID(workspace.ID) {
				m.clearPendingCreated()
				return true
			}
			return false
		}
	}
	return false
}

func (m *Model) createWheelAtBoundary(msg tea.MouseWheelMsg) bool {
	if !m.createOpen {
		return false
	}
	md := m.activeCreateModal()
	if md == nil {
		return false
	}
	return md.WheelAtBoundary(msg, m.createMouse)
}

func createProjectKeyFromAction(id string) string {
	return strings.TrimPrefix(id, globalCreateActionID+":")
}
