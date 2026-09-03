package overview

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/agentcontrol"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspaceinventory"
	"github.com/marcus/sidecar/internal/workspacelist"
	"github.com/marcus/sidecar/internal/workspaceops"
)

func createActionPoint(m *Model, kind workspacelist.RegionKind) (int, int, string, bool) {
	split := m.previewSplit(previewWide)
	m.syncCreateActions()
	rendered := m.workspaces.Render(workspacelist.RenderOptions{Width: split.SidebarContentWidth, Height: previewTall - 2, Title: "Workspaces", Focused: true})
	for _, region := range rendered.Regions {
		if region.Kind == kind {
			return globalContentInset + region.X, 1 + region.Y, region.ID, true
		}
	}
	return 0, 0, "", false
}

func stubGlobalAgentShellReady(t *testing.T) {
	t.Helper()
	original := waitGlobalShellReady
	t.Cleanup(func() { waitGlobalShellReady = original })
	waitGlobalShellReady = func(_ context.Context, target agentcontrol.Target, _ time.Duration) (agentcontrol.Snapshot, error) {
		return agentcontrol.Snapshot{Target: target}, nil
	}
}

func TestGlobalWorktreePlanUsesOnlyTargetProjectConfig(t *testing.T) {
	m := catalogModel(t)
	m.projects = []Project{{Name: "one", Path: "/tmp/one", Key: "one"}, {Name: "two", Path: "/tmp/two", Key: "two"}}
	m.config = &config.Config{Projects: config.ProjectsConfig{List: []config.ProjectConfig{
		{Name: "one", Path: "/tmp/one", WorktreeSetup: &config.WorktreeSetupConfig{HookPath: "one.sh"}},
		{Name: "two", Path: "/tmp/two", WorktreeSetup: &config.WorktreeSetupConfig{HookPath: "two.sh", RunHook: true}},
	}}}
	original := resolveGlobalWorktree
	defer func() { resolveGlobalWorktree = original }()
	var gotDir string
	var gotSetup config.WorktreeSetupConfig
	resolveGlobalWorktree = func(_ context.Context, workDir, projectRoot, name, base string, dirPrefix bool, setup config.WorktreeSetupConfig) (*workspaceops.WorktreePlan, error) {
		gotDir, gotSetup = workDir, setup
		return &workspaceops.WorktreePlan{SourceWorktree: workDir, MainWorktree: projectRoot, Branch: name, Path: "/tmp/two-feature", SourceRef: "HEAD", SourceOID: "abc"}, nil
	}
	m.OpenCreateWorktree("two")
	typeCreateName(t, m, "feature")
	cmd := m.planCreateWorktree()
	if cmd == nil {
		t.Fatalf("plan cmd is nil, error=%q name=%q", m.createError, m.createForm.Name())
	}
	msg := cmd().(globalWorktreePlannedMsg)
	if gotDir != "/tmp/two" || gotSetup.HookPath != "two.sh" || !gotSetup.RunHook {
		t.Fatalf("target config leaked: dir=%q setup=%+v", gotDir, gotSetup)
	}
	if msg.Plan.RepoKey != "two" || msg.Project.Path != "/tmp/two" {
		t.Fatalf("planned identity = %+v project=%+v", msg.Plan, msg.Project)
	}
}

func TestGlobalWorktreeCancellationBeforeAndAfterMutation(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreateWorktree("")
	m.createPlan = &workspaceops.WorktreePlan{Path: "/tmp/created", Branch: "created"}
	originalExecute, originalRemove := executeGlobalWorktree, removeGlobalJournal
	defer func() { executeGlobalWorktree, removeGlobalJournal = originalExecute, originalRemove }()
	executed := 0
	executeGlobalWorktree = func(context.Context, string, *workspaceops.WorktreePlan) (*workspaceops.WorktreeRecord, error) {
		executed++
		return nil, nil
	}
	m.applyCreateAction(globalCreateCancelID)
	if executed != 0 || m.CreateOpen() {
		t.Fatalf("pre-mutation cancel executed=%d open=%v", executed, m.CreateOpen())
	}

	m.OpenCreateWorktree("")
	plan := &workspaceops.WorktreePlan{Path: "/tmp/created", Branch: "created", AgentType: "codex"}
	record := &workspaceops.WorktreeRecord{Path: plan.Path, Branch: plan.Branch, HEADOID: "abc"}
	m.Update(globalWorktreeCreatedMsg{Project: m.projects[0], Plan: plan, Record: record, Outcomes: []workspaceops.SetupOutcome{{Action: "optional hook", Err: errors.New("boom")}}})
	if !m.CreateOpen() || m.createRecord == nil || m.createWarning == "" {
		t.Fatalf("post-mutation cancel path lost recovery: open=%v record=%+v warning=%q", m.CreateOpen(), m.createRecord, m.createWarning)
	}
	removeGlobalJournal = func(*workspaceops.WorktreePlan) error { return nil }
	if cmd := m.applyCreateAction(globalCreateCancelID); cmd == nil || !m.createBusy || m.createRecord == nil {
		t.Fatalf("post-mutation cancel did not retain and launch created identity: cmd=%v busy=%v record=%+v", cmd, m.createBusy, m.createRecord)
	}
}

func TestGlobalWorktreeRequiredSetupFailureOffersRecovery(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreateWorktree("")
	plan := &workspaceops.WorktreePlan{Path: "/tmp/created", Branch: "created"}
	record := &workspaceops.WorktreeRecord{Path: plan.Path, Branch: plan.Branch, HEADOID: "abc"}
	m.Update(globalWorktreeCreatedMsg{Project: m.projects[0], Plan: plan, Record: record, Outcomes: []workspaceops.SetupOutcome{{Action: "required hook", Required: true, Err: errors.New("boom")}}})
	m.width, m.height = 80, 30
	m.ensureCreateModal()
	if m.createModal == nil || m.createError == "" || m.createRecord == nil {
		t.Fatalf("required failure did not retain recovery state: error=%q record=%+v", m.createError, m.createRecord)
	}
}

func TestGlobalWorktreePartialMutationAndJournalFailuresStayRecoverable(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreateWorktree("")
	plan := &workspaceops.WorktreePlan{MainWorktree: "/tmp/main", Path: "/tmp/created", Branch: "created", AgentType: "codex"}
	record := &workspaceops.WorktreeRecord{Path: plan.Path, Branch: plan.Branch, HEADOID: "abc"}
	originalRemove, originalLaunch, originalExecute := removeGlobalJournal, launchGlobalSession, executeGlobalWorktree
	originalJournal, originalSetup := persistGlobalJournal, runGlobalSetup
	defer func() {
		removeGlobalJournal, launchGlobalSession, executeGlobalWorktree = originalRemove, originalLaunch, originalExecute
		persistGlobalJournal, runGlobalSetup = originalJournal, originalSetup
	}()
	removed, launched := 0, 0
	removeGlobalJournal = func(*workspaceops.WorktreePlan) error { removed++; return nil }
	launchGlobalSession = func(context.Context, workspaceops.AgentLaunchSpec) (workspaceops.AgentLaunchResult, error) {
		launched++
		return workspaceops.AgentLaunchResult{}, nil
	}
	executeGlobalWorktree = func(context.Context, string, *workspaceops.WorktreePlan) (*workspaceops.WorktreeRecord, error) {
		return record, errors.New("repair failed")
	}
	persistGlobalJournal = func(context.Context, *workspaceops.WorktreePlan, *workspaceops.WorktreeRecord) error { return nil }
	setupRuns := 0
	runGlobalSetup = func(context.Context, *workspaceops.WorktreePlan) []workspaceops.SetupOutcome { setupRuns++; return nil }
	m.createPlan = plan
	msg := m.executeCreateWorktree()().(globalWorktreeCreatedMsg)
	if setupRuns != 0 || msg.Record == nil || msg.Err == nil {
		t.Fatalf("partial execute ran setup or lost identity: setup=%d msg=%+v", setupRuns, msg)
	}
	m.Update(msg)
	if m.createError == "" || m.createRecord == nil || removed != 0 || launched != 0 || !m.CreateOpen() {
		t.Fatalf("partial mutation escaped recovery: error=%q record=%+v removed=%d launched=%d open=%v", m.createError, m.createRecord, removed, launched, m.CreateOpen())
	}

	m.createError = ""
	removeGlobalJournal = func(*workspaceops.WorktreePlan) error { removed++; return errors.New("sync failed") }
	m.Update(globalWorktreeCreatedMsg{Project: m.projects[0], Plan: plan, Record: record})
	if !strings.Contains(m.createError, "finalize pending creation journal") || launched != 0 || m.createRecord == nil {
		t.Fatalf("journal failure escaped recovery: error=%q launched=%d record=%+v", m.createError, launched, m.createRecord)
	}
	m.createError = ""
	if cmd := m.openCreatedWorktreeAnyway(); cmd != nil || !strings.Contains(m.createError, "before opening") {
		t.Fatalf("open-anyway ignored journal failure: cmd=%v error=%q", cmd, m.createError)
	}
}

func TestGlobalWorktreeLaunchesConfiguredAgentBeforeRefresh(t *testing.T) {
	stubGlobalAgentShellReady(t)
	m := catalogModel(t)
	m.config = &config.Config{Plugins: config.PluginsConfig{Workspace: config.WorkspacePluginConfig{AgentStart: map[string]string{"codex": "codex-custom"}}}}
	m.OpenCreateWorktree("")
	plan := &workspaceops.WorktreePlan{MainWorktree: t.TempDir(), Path: t.TempDir(), Branch: "created", AgentType: "codex"}
	record := &workspaceops.WorktreeRecord{Path: plan.Path, Name: "Created", Branch: plan.Branch, HEADOID: "abc"}
	originalRemove, originalLaunch, originalStart := removeGlobalJournal, launchGlobalSession, startGlobalAgent
	defer func() {
		removeGlobalJournal, launchGlobalSession, startGlobalAgent = originalRemove, originalLaunch, originalStart
	}()
	removeGlobalJournal = func(*workspaceops.WorktreePlan) error { return nil }
	var spec workspaceops.AgentLaunchSpec
	launchGlobalSession = func(_ context.Context, got workspaceops.AgentLaunchSpec) (workspaceops.AgentLaunchResult, error) {
		spec = got
		return workspaceops.AgentLaunchResult{SessionName: got.SessionName, PaneID: "%1"}, nil
	}
	var request agentcontrol.StartRequest
	startGlobalAgent = func(_ context.Context, got agentcontrol.StartRequest) (agentcontrol.Agent, error) {
		request = got
		return agentcontrol.Agent{Target: got.Target}, nil
	}
	launchCmd := m.update(globalWorktreeCreatedMsg{Project: m.projects[0], Plan: plan, Record: record})
	if launchCmd == nil {
		t.Fatal("successful setup refreshed before launching")
	}
	launchMsg := launchCmd().(globalWorkspaceLaunchedMsg)
	if spec.StartAgent || spec.AgentCommand != "" || spec.WorkDir != plan.Path {
		t.Fatalf("workspace launch spec = %+v", spec)
	}
	if request.Kind != "codex" || request.Target.Project != projectKey(m.projects[0]) || !strings.Contains(strings.Join(request.Argv, " "), "codex-custom") {
		t.Fatalf("agentcontrol request = %+v", request)
	}
	if refresh := m.update(launchMsg); refresh == nil || m.CreateOpen() {
		t.Fatalf("launch success did not close and refresh: refresh=%v open=%v", refresh, m.CreateOpen())
	}
}

func TestGlobalWorktreeWithoutAgentStillLaunchesPlainWorktreeSession(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreateWorktree("")
	plan := &workspaceops.WorktreePlan{MainWorktree: t.TempDir(), Path: t.TempDir(), Branch: "created"}
	record := &workspaceops.WorktreeRecord{Path: plan.Path, Name: "Created", Branch: plan.Branch, HEADOID: "abc"}
	originalRemove, originalLaunch := removeGlobalJournal, launchGlobalSession
	defer func() { removeGlobalJournal, launchGlobalSession = originalRemove, originalLaunch }()
	removeGlobalJournal = func(*workspaceops.WorktreePlan) error { return nil }
	var spec workspaceops.AgentLaunchSpec
	launchGlobalSession = func(_ context.Context, got workspaceops.AgentLaunchSpec) (workspaceops.AgentLaunchResult, error) {
		spec = got
		return workspaceops.AgentLaunchResult{SessionName: got.SessionName, PaneID: "%1"}, nil
	}
	launchCmd := m.update(globalWorktreeCreatedMsg{Project: m.projects[0], Plan: plan, Record: record})
	if launchCmd == nil {
		t.Fatal("plain worktree closed and refreshed without launching its session")
	}
	if _, ok := launchCmd().(globalWorkspaceLaunchedMsg); !ok {
		t.Fatalf("plain worktree launch returned unexpected message")
	}
	if spec.StartAgent || spec.AgentCommand != "" || spec.WorkDir != plan.Path {
		t.Fatalf("plain worktree launch spec = %+v", spec)
	}
}

func TestCreatedWorktreeSelectionFollowsAcrossSortsAndTinyViewport(t *testing.T) {
	for _, sortMode := range workspacelist.SortModes {
		t.Run(sortMode.Label(), func(t *testing.T) {
			m := catalogModel(t)
			m.showIdleWorktrees = true
			m.workspaces.SetSort(sortMode)
			m.pendingCreatedPath = "/tmp/created"
			project := m.projects[0]
			result := m.results[projectKey(project)]
			result.Workspaces = append(result.Workspaces, workspaceinventory.Workspace{
				ID: "created-worktree", ProjectKey: projectKey(project), ProjectName: project.Name, ProjectRoot: project.Path,
				Kind: workspaceinventory.KindWorktree, Key: "created", Path: "/tmp/created", Name: "Created",
			})
			m.applyProjectMutationRefresh(projectMutationRefreshMsg{Project: project, Result: result})
			_ = m.workspaces.Render(workspacelist.RenderOptions{Width: 24, Height: 4, Title: "Workspaces", Focused: true})
			if got := m.workspaces.SelectedID(); got != "created-worktree" {
				t.Fatalf("selected = %q, want created worktree", got)
			}
		})
	}
}

func TestProjectMutationRefreshReplacesOnlyTargetProject(t *testing.T) {
	m := catalogModel(t)
	otherKey := "untouched"
	untouched := workspaceinventory.ProjectResult{ProjectKey: otherKey, ProjectName: "Untouched", Workspaces: []workspaceinventory.Workspace{{ID: "keep-me", ProjectKey: otherKey}}}
	m.results[otherKey] = untouched
	target := m.projects[0]
	replacement := workspaceinventory.ProjectResult{ProjectKey: projectKey(target), ProjectName: target.Name, Workspaces: []workspaceinventory.Workspace{{ID: "replacement", ProjectKey: projectKey(target)}}}
	m.applyProjectMutationRefresh(projectMutationRefreshMsg{Project: target, Result: replacement})
	got := m.results[otherKey]
	if len(got.Workspaces) != 1 || got.Workspaces[0].ID != "keep-me" {
		t.Fatalf("targeted refresh changed unrelated project: %+v", got)
	}
}

func TestGlobalCreateHeaderAndProjectSectionActionsRouteTypedProjects(t *testing.T) {
	m := catalogModel(t)
	m.width, m.height = previewWide, previewTall
	m.workspaces.SetSort(workspacelist.SortProject)
	_ = m.WorkspacesView(previewWide, previewTall)

	x, y, _, ok := createActionPoint(m, workspacelist.RegionHeaderAction)
	if !ok {
		t.Fatal("header create action was not rendered")
	}
	m.WorkspacesMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !m.CreateOpen() {
		t.Fatal("clicking the rendered header action did not open create")
	}
	selected, _ := m.SelectedWorkspace()
	gotKey := ""
	if m.createForm != nil {
		gotKey = m.createForm.ProjectKey()
	}
	if gotKey != selected.ProjectKey {
		t.Fatalf("header default project = %q, want selected row project %q", gotKey, selected.ProjectKey)
	}
	m.closeCreateShell()
	_ = m.WorkspacesView(previewWide, previewTall)

	x, y, id, ok := createActionPoint(m, workspacelist.RegionSectionAction)
	if !ok {
		t.Fatal("project section create action was not rendered")
	}
	m.WorkspacesMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !m.CreateOpen() {
		t.Fatal("clicking the rendered section action did not open create")
	}
	gotKey = ""
	if m.createForm != nil {
		gotKey = m.createForm.ProjectKey()
	}
	if want := createProjectKeyFromAction(id); gotKey != want {
		t.Fatalf("section preselected %q, want typed action project %q", gotKey, want)
	}
}

func TestGlobalCreateResolvesNamesAndConfigInsideTargetProject(t *testing.T) {
	m := catalogModel(t)
	m.projects = []Project{{Name: "one", Path: "/tmp/one", Key: "one"}, {Name: "two", Path: "/tmp/two", Key: "two"}}
	m.results["one"] = workspaceinventory.ProjectResult{Workspaces: []workspaceinventory.Workspace{
		{Kind: workspaceinventory.KindShell, TmuxName: "sidecar-sh-one-7", Name: "Shell 7"},
	}}
	m.results["two"] = workspaceinventory.ProjectResult{Workspaces: []workspaceinventory.Workspace{
		{Kind: workspaceinventory.KindShell, TmuxName: "sidecar-sh-two-2", Name: "Shell 2"},
	}}

	original := createManagedShell
	defer func() { createManagedShell = original }()
	var got workspaceops.ManagedShellSpec
	createManagedShell = func(spec workspaceops.ManagedShellSpec) (workspaceops.ShellResult, error) {
		got = spec
		return workspaceops.ShellResult{SessionName: spec.SessionName}, nil
	}
	m.OpenCreateShell("two")
	cmd := m.submitCreateShell()
	msg := cmd().(globalShellCreatedMsg)
	if msg.Project.Path != "/tmp/two" || got.ProjectRoot != "/tmp/two" || got.WorkDir != "/tmp/two" {
		t.Fatalf("target project leaked: msg=%+v spec=%+v", msg.Project, got)
	}
	if got.SessionName != "sidecar-sh-two-3" || got.DisplayName != "Shell 3" {
		t.Fatalf("target names = %q/%q, want sidecar-sh-two-3/Shell 3", got.SessionName, got.DisplayName)
	}
}

func TestGlobalShellCreateLaunchesConfiguredAgent(t *testing.T) {
	stubGlobalAgentShellReady(t)
	_ = state.SetLastCreateAgent("")
	m := catalogModel(t)
	m.config = &config.Config{Plugins: config.PluginsConfig{Workspace: config.WorkspacePluginConfig{
		DefaultAgentType: "codex", AgentStart: map[string]string{"codex": "codex-custom"},
	}}}
	originalCreate, originalStart := createManagedShell, startGlobalAgent
	defer func() { createManagedShell, startGlobalAgent = originalCreate, originalStart }()
	createManagedShell = func(spec workspaceops.ManagedShellSpec) (workspaceops.ShellResult, error) {
		return workspaceops.ShellResult{SessionName: spec.SessionName}, nil
	}
	var request agentcontrol.StartRequest
	startGlobalAgent = func(_ context.Context, got agentcontrol.StartRequest) (agentcontrol.Agent, error) {
		request = got
		return agentcontrol.Agent{Target: got.Target}, nil
	}
	m.OpenCreateShell("sidecar")
	msg := m.submitCreateShell()().(globalShellCreatedMsg)
	if msg.Err != nil || request.Target.Session == "" || !strings.Contains(strings.Join(request.Argv, " "), "codex-custom") {
		t.Fatalf("configured agent launch: request=%+v err=%v", request, msg.Err)
	}
}

func TestGlobalCatalogLaunchesUseStructuredAgentControlAcrossShellAndWorktree(t *testing.T) {
	stubGlobalAgentShellReady(t)
	_ = state.SetLastCreateAgent("")
	m := catalogModel(t)
	m.config = &config.Config{Plugins: config.PluginsConfig{Workspace: config.WorkspacePluginConfig{DefaultAgentType: "codex"}}}
	project := m.projects[0]

	originalCreate, originalLaunch, originalStart := createManagedShell, launchGlobalSession, startGlobalAgent
	defer func() {
		createManagedShell, launchGlobalSession, startGlobalAgent = originalCreate, originalLaunch, originalStart
	}()
	createManagedShell = func(spec workspaceops.ManagedShellSpec) (workspaceops.ShellResult, error) {
		return workspaceops.ShellResult{SessionName: spec.SessionName, PaneID: "%shell"}, nil
	}
	launchGlobalSession = func(_ context.Context, spec workspaceops.AgentLaunchSpec) (workspaceops.AgentLaunchResult, error) {
		if spec.StartAgent || spec.AgentCommand != "" {
			t.Fatalf("workspaceops received agent launch fields: %+v", spec)
		}
		return workspaceops.AgentLaunchResult{SessionName: spec.SessionName, PaneID: "%worktree", Reconnected: true}, nil
	}
	var requests []agentcontrol.StartRequest
	startGlobalAgent = func(_ context.Context, request agentcontrol.StartRequest) (agentcontrol.Agent, error) {
		requests = append(requests, request)
		return agentcontrol.Agent{Target: request.Target}, nil
	}

	m.OpenCreateShell(projectKey(project))
	shellMsg := m.submitCreateShell()().(globalShellCreatedMsg)
	if shellMsg.Err != nil {
		t.Fatalf("shell launch failed: %v", shellMsg.Err)
	}
	plan := &workspaceops.WorktreePlan{MainWorktree: project.Path, Path: t.TempDir(), Branch: "feature", AgentType: "codex"}
	record := &workspaceops.WorktreeRecord{Path: plan.Path, Name: "Feature", Branch: plan.Branch}
	worktreeMsg := m.launchCreatedWorktree(project, plan, record)().(globalWorkspaceLaunchedMsg)
	if worktreeMsg.Err != nil {
		t.Fatalf("worktree launch failed: %v", worktreeMsg.Err)
	}
	if len(requests) != 2 {
		t.Fatalf("agentcontrol requests = %d, want shell and worktree: %+v", len(requests), requests)
	}
	for i, request := range requests {
		if request.Kind != "codex" || len(request.Argv) != 1 || request.Argv[0] != "codex" || request.Target.Project != projectKey(project) || request.Timeout != globalAgentStartTimeout {
			t.Fatalf("request[%d] = %+v", i, request)
		}
	}
	if !worktreeMsg.Result.Reconnected {
		t.Fatal("reconnected shell did not retain its result after verified start")
	}
}

func TestCreatedShellSelectionFollowsAcrossSortsAndTinyViewport(t *testing.T) {
	for _, sortMode := range workspacelist.SortModes {
		t.Run(sortMode.Label(), func(t *testing.T) {
			m := catalogModel(t)
			m.workspaces.SetSort(sortMode)
			m.pendingCreatedTmux = "sidecar-sh-sidecar-9"
			project := m.projects[0]
			result := m.results[projectKey(project)]
			result.Workspaces = append(result.Workspaces, workspaceinventory.Workspace{
				ID: "created", ProjectKey: projectKey(project), ProjectName: project.Name, ProjectRoot: project.Path,
				Kind: workspaceinventory.KindShell, Key: "sidecar-sh-sidecar-9", TmuxName: "sidecar-sh-sidecar-9", Name: "Shell 9", Live: true,
			})
			m.applyProjectMutationRefresh(projectMutationRefreshMsg{Project: project, Result: result})
			_ = m.workspaces.Render(workspacelist.RenderOptions{Width: 24, Height: 4, Title: "Workspaces", Focused: true})
			if got := m.workspaces.SelectedID(); got != "created" {
				t.Fatalf("selected = %q, want created", got)
			}
		})
	}
}

// runCreateCmd feeds a command's message — and every batched sibling — to the
// model, the way the runtime would.
func runCreateCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			runCreateCmd(t, m, sub)
		}
		return
	}
	m.Update(msg)
}

func renderCreateModal(t *testing.T, m *Model) string {
	t.Helper()
	if m.width < 1 {
		m.width = 80
	}
	if m.height < 1 {
		m.height = 30
	}
	m.ensureCreateModal()
	md := m.activeCreateModal()
	if md == nil {
		t.Fatal("create modal is nil")
	}
	view := md.Render(m.width, m.height, m.createMouse)
	return view
}

func createFormModal(t *testing.T, m *Model) *modal.Modal {
	t.Helper()
	renderCreateModal(t, m)
	md := m.activeCreateModal()
	if md == nil {
		t.Fatal("create modal is nil")
	}
	return md
}

func createAgentIDs(m *Model) []string {
	if m.createForm == nil {
		return nil
	}
	items := m.createForm.AgentItems()
	ids := make([]string, len(items))
	for i, item := range items {
		id, _ := item.Data.(string)
		ids[i] = id
	}
	return ids
}

func selectCreateAgent(t *testing.T, m *Model, agent string) {
	t.Helper()
	md := createFormModal(t, m)
	md.SetFocus(workspacecreate.FieldAgent)
	renderCreateModal(t, m)
	for _, dir := range []string{"up", "down"} {
		for i := 0; i < 24; i++ {
			if m.createForm.Agent() == agent {
				return
			}
			m.handleCreateShellKey(createKey(dir))
		}
	}
	t.Fatalf("could not select agent %q, got %q", agent, m.createForm.Agent())
}

// selectCreateProject arrows the form's project combo to the given key, the
// way selectCreateAgent does for agents, so the switch runs the real
// finishCreateInput path a user's keystroke runs.
func selectCreateProject(t *testing.T, m *Model, key string) {
	t.Helper()
	md := createFormModal(t, m)
	md.SetFocus(workspacecreate.FieldProject)
	renderCreateModal(t, m)
	for _, dir := range []string{"up", "down"} {
		for i := 0; i < 24; i++ {
			if m.createForm.ProjectKey() == key {
				return
			}
			m.handleCreateShellKey(createKey(dir))
		}
	}
	t.Fatalf("could not select project %q, got %q", key, m.createForm.ProjectKey())
}

func clearFocusedCombo(m *Model) {
	for i := 0; i < 80; i++ {
		m.handleCreateShellKey(createKey("backspace"))
	}
}

func typeCreateName(t *testing.T, m *Model, name string) {
	t.Helper()
	md := createFormModal(t, m)
	md.SetFocus(workspacecreate.FieldName)
	renderCreateModal(t, m)
	for _, r := range name {
		m.handleCreateShellKey(createKey(string(r)))
	}
	if got := strings.TrimSpace(m.createForm.Name()); got != name {
		t.Fatalf("name = %q, want %q", got, name)
	}
}

func createKey(k string) tea.KeyPressMsg {
	switch k {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case " ":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	default:
		r := []rune(k)
		if len(r) == 1 {
			return tea.KeyPressMsg{Code: r[0], Text: k}
		}
		return tea.KeyPressMsg{Text: k}
	}
}

func createHit(m *Model, id string) (mouse.Region, bool) {
	for _, region := range m.createMouse.HitMap.Regions() {
		if region.ID == id {
			return region, true
		}
	}
	return mouse.Region{}, false
}

func TestCreateModalProjectComboFiltersAndSubmitUsesProject(t *testing.T) {
	m := catalogModel(t)
	original := createManagedShell
	defer func() { createManagedShell = original }()
	var got workspaceops.ManagedShellSpec
	createManagedShell = func(spec workspaceops.ManagedShellSpec) (workspaceops.ShellResult, error) {
		got = spec
		return workspaceops.ShellResult{SessionName: spec.SessionName}, nil
	}
	m.OpenCreateShell("sidecar")
	md := createFormModal(t, m)
	md.SetFocus(workspacecreate.FieldProject)
	clearFocusedCombo(m)

	m.handleCreateShellKey(createKey("b"))
	if m.createBusy || !m.CreateOpen() {
		t.Fatal("typing a project filter submitted")
	}
	if project, ok := m.selectedCreateProject(); !ok || project.Name != "braid" {
		t.Fatalf("filtered project = %+v ok=%v, want braid", project, ok)
	}

	renderCreateModal(t, m)
	item, ok := createHit(m, workspacecreate.FieldProject+"/item/0")
	if !ok {
		t.Fatal("expected project overlay row")
	}
	if cmd := m.handleCreateShellMouse(tea.MouseClickMsg{X: item.Rect.X + 1, Y: item.Rect.Y, Button: tea.MouseLeft}); cmd != nil {
		if m.createBusy {
			t.Fatal("overlay click submitted")
		}
	}
	if project, ok := m.selectedCreateProject(); !ok || project.Path != "/tmp/braid" {
		t.Fatalf("clicked project = %+v", project)
	}

	cmd := m.submitCreateShell()
	if cmd == nil {
		t.Fatal("expected submit cmd")
	}
	msg := cmd().(globalShellCreatedMsg)
	if msg.Project.Path != "/tmp/braid" || got.ProjectRoot != "/tmp/braid" || got.WorkDir != "/tmp/braid" {
		t.Fatalf("submit used wrong project: msg=%+v spec=%+v", msg.Project, got)
	}
}

func TestCreateModalKindClickChangesKindAndPlaceholder(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreateShell("sidecar")
	view := createFormModal(t, m).Render(m.width, m.height, m.createMouse)
	if m.createForm.Kind() != workspacecreate.KindShell {
		t.Fatalf("kind = %v, want shell", m.createForm.Kind())
	}
	if !strings.Contains(view, "Shell") {
		t.Fatalf("shell placeholder missing from view:\n%s", view)
	}

	region, ok := createHit(m, workspacecreate.FieldKind)
	if !ok {
		t.Fatal("kind control was not hit-tested")
	}
	// The list shape is a bordered control (td-c6904c), so the region's top
	// line is the border and the rows begin one below it. A click on the border
	// is a click on chrome: it focuses the control and moves nothing.
	if cmd := m.handleCreateShellMouse(tea.MouseClickMsg{
		X: region.Rect.X + 2, Y: region.Rect.Y, Button: tea.MouseLeft,
	}); cmd != nil {
		t.Fatalf("border click submitted: %v", cmd)
	}
	if m.createForm.Kind() != workspacecreate.KindShell {
		t.Fatalf("a click on the control's border changed the kind to %v", m.createForm.Kind())
	}
	// The catalog outgrew the horizontal toggle in M2: the kind list is
	// vertical now, so a click picks by row. Row 1 is Worktree.
	if cmd := m.handleCreateShellMouse(tea.MouseClickMsg{
		X: region.Rect.X + 2, Y: region.Rect.Y + 2, Button: tea.MouseLeft,
	}); cmd != nil {
		t.Fatalf("kind click submitted: %v", cmd)
	}
	if m.createForm.Kind() != workspacecreate.KindWorktree {
		t.Fatalf("kind after click = %v, want worktree", m.createForm.Kind())
	}
	view = renderCreateModal(t, m)
	if !strings.Contains(ansi.Strip(view), "feature") {
		t.Fatalf("worktree placeholder missing from view:\n%s", view)
	}

	renderCreateModal(t, m)
	region, ok = createHit(m, workspacecreate.FieldKind)
	if !ok {
		t.Fatal("kind control missing after rebuild")
	}
	m.handleCreateShellMouse(tea.MouseClickMsg{
		X: region.Rect.X + 2, Y: region.Rect.Y + 1, Button: tea.MouseLeft,
	})
	if m.createForm.Kind() != workspacecreate.KindShell {
		t.Fatalf("kind after top-row click = %v, want shell", m.createForm.Kind())
	}
}

func TestCreateModalKindSwitchKeepsChosenAgent(t *testing.T) {
	_ = state.SetLastCreateAgent("")
	m := catalogModel(t)
	m.config = &config.Config{Plugins: config.PluginsConfig{Workspace: config.WorkspacePluginConfig{
		DefaultAgentType: "claude",
		Agents:           []string{"claude", "grok"},
	}}}

	m.OpenCreateShell("sidecar")
	renderCreateModal(t, m)
	if m.createForm.Agent() != "claude" {
		t.Fatalf("default shell agent = %q, want claude", m.createForm.Agent())
	}
	beforeType := m.createForm.Agent()

	createFormModal(t, m).SetFocus(workspacecreate.FieldKind)
	if handled, cmd := m.handleCreateShellKey(createKey("j")); !handled || cmd != nil {
		t.Fatalf("kind j handled=%v cmd=%v", handled, cmd)
	}
	if m.createForm.Kind() != workspacecreate.KindWorktree {
		t.Fatalf("kind after j = %v, want worktree", m.createForm.Kind())
	}
	if got := m.createForm.Agent(); got != beforeType {
		t.Fatalf("kind switch remapped agent %q → %q", beforeType, got)
	}

	renderCreateModal(t, m)
	region, ok := createHit(m, workspacecreate.FieldKind)
	if !ok {
		t.Fatal("kind control missing after rebuild")
	}
	// The first row sits one line inside the control's border (td-c6904c).
	if cmd := m.handleCreateShellMouse(tea.MouseClickMsg{
		X: region.Rect.X + region.Rect.W/4, Y: region.Rect.Y + 1, Button: tea.MouseLeft,
	}); cmd != nil {
		t.Fatalf("kind click submitted: %v", cmd)
	}
	if m.createForm.Kind() != workspacecreate.KindShell {
		t.Fatalf("kind after click = %v, want shell", m.createForm.Kind())
	}
	if got := m.createForm.Agent(); got != "claude" {
		t.Fatalf("click back to shell remapped agent to %q", got)
	}

	selectCreateAgent(t, m, "")
	if m.createForm.Agent() != "" {
		t.Fatalf("None not selected: %q", m.createForm.Agent())
	}
	createFormModal(t, m).SetFocus(workspacecreate.FieldKind)
	m.handleCreateShellKey(createKey("j"))
	if m.createForm.Kind() != workspacecreate.KindWorktree {
		t.Fatalf("kind after None switch = %v, want worktree", m.createForm.Kind())
	}
	if got := m.createForm.Agent(); got != "" {
		t.Fatalf("switching kind remapped None to %q", got)
	}
}

func TestCreateModalAgentComboSelectedAgentIsUsed(t *testing.T) {
	stubGlobalAgentShellReady(t)
	_ = state.SetLastCreateAgent("")
	m := catalogModel(t)
	m.config = &config.Config{Plugins: config.PluginsConfig{Workspace: config.WorkspacePluginConfig{
		DefaultAgentType: "claude",
		Agents:           []string{"claude", "grok"},
		AgentStart:       map[string]string{"grok": "grok-custom"},
	}}}
	originalCreate, originalStart := createManagedShell, startGlobalAgent
	defer func() { createManagedShell, startGlobalAgent = originalCreate, originalStart }()
	var spec workspaceops.ManagedShellSpec
	createManagedShell = func(got workspaceops.ManagedShellSpec) (workspaceops.ShellResult, error) {
		spec = got
		return workspaceops.ShellResult{SessionName: got.SessionName}, nil
	}
	var request agentcontrol.StartRequest
	started := 0
	startGlobalAgent = func(_ context.Context, got agentcontrol.StartRequest) (agentcontrol.Agent, error) {
		started++
		request = got
		return agentcontrol.Agent{Target: got.Target}, nil
	}

	m.OpenCreateShell("sidecar")
	renderCreateModal(t, m)
	if ids := createAgentIDs(m); len(ids) == 0 || ids[0] != "" {
		t.Fatalf("shell agent list should lead with None: %v", ids)
	}
	selectCreateAgent(t, m, "grok")
	if m.createForm.Agent() != "grok" {
		t.Fatalf("selected agent = %q, want grok", m.createForm.Agent())
	}

	msg := m.submitCreateShell()().(globalShellCreatedMsg)
	if msg.Err != nil || spec.AgentType != "grok" || request.Target.Session == "" || !strings.Contains(strings.Join(request.Argv, " "), "grok-custom") {
		t.Fatalf("chosen agent not used: spec=%+v request=%+v err=%v", spec, request, msg.Err)
	}
	if started != 1 {
		t.Fatalf("start count = %d", started)
	}

	m.OpenCreateWorktree("sidecar")
	typeCreateName(t, m, "feature")
	if ids := createAgentIDs(m); len(ids) == 0 || ids[len(ids)-1] != "" {
		t.Fatalf("worktree agent list should end with None: %v", ids)
	}
	originalPlan := resolveGlobalWorktree
	defer func() { resolveGlobalWorktree = originalPlan }()
	resolveGlobalWorktree = func(context.Context, string, string, string, string, bool, config.WorktreeSetupConfig) (*workspaceops.WorktreePlan, error) {
		return &workspaceops.WorktreePlan{Branch: "feature", Path: "/tmp/feature"}, nil
	}
	selectCreateAgent(t, m, "")
	planMsg := m.planCreateWorktree()().(globalWorktreePlannedMsg)
	if planMsg.Plan == nil || planMsg.Plan.AgentType != "" {
		t.Fatalf("None should leave plan.AgentType empty: %+v", planMsg.Plan)
	}
}

func TestCreateModalNoneDoesNotStartAgent(t *testing.T) {
	_ = state.SetLastCreateAgent("")
	m := catalogModel(t)
	m.config = &config.Config{Plugins: config.PluginsConfig{Workspace: config.WorkspacePluginConfig{
		DefaultAgentType: "claude", AgentStart: map[string]string{"claude": "claude-custom"},
	}}}
	originalCreate, originalStart := createManagedShell, startGlobalAgent
	defer func() { createManagedShell, startGlobalAgent = originalCreate, originalStart }()
	var spec workspaceops.ManagedShellSpec
	createManagedShell = func(got workspaceops.ManagedShellSpec) (workspaceops.ShellResult, error) {
		spec = got
		return workspaceops.ShellResult{SessionName: got.SessionName}, nil
	}
	started := 0
	startGlobalAgent = func(_ context.Context, _ agentcontrol.StartRequest) (agentcontrol.Agent, error) {
		started++
		return agentcontrol.Agent{}, nil
	}
	m.OpenCreateShell("sidecar")
	selectCreateAgent(t, m, "")
	msg := m.submitCreateShell()().(globalShellCreatedMsg)
	if msg.Err != nil || spec.AgentType != "" || started != 0 {
		t.Fatalf("None started an agent: spec=%+v started=%d err=%v", spec, started, msg.Err)
	}
}

func TestCreateModalTabCyclesWithoutTrapping(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreate("")
	md := createFormModal(t, m)
	if md.FocusedID() != workspacecreate.FieldKind {
		t.Fatalf("chooser focus = %q, want kind", md.FocusedID())
	}
	if m.createForm.Kind() != workspacecreate.KindWorktree {
		t.Fatalf("OpenCreate kind = %v, want worktree", m.createForm.Kind())
	}
	want := []string{
		workspacecreate.FieldProject,
		workspacecreate.FieldName,
		workspacecreate.FieldBase,
		workspacecreate.FieldAgent,
		workspacecreate.FieldSkip,
		workspacecreate.ActionCreate,
		workspacecreate.ActionCancel,
		workspacecreate.FieldKind,
	}
	for i, id := range want {
		m.handleCreateShellKey(createKey("tab"))
		renderCreateModal(t, m)
		if got := m.activeCreateModal().FocusedID(); got != id {
			t.Fatalf("tab %d focus = %q, want %q", i+1, got, id)
		}
	}
	m.handleCreateShellKey(createKey("shift+tab"))
	renderCreateModal(t, m)
	if got := m.activeCreateModal().FocusedID(); got != workspacecreate.ActionCancel {
		t.Fatalf("shift+tab focus = %q, want cancel", got)
	}
}

func TestCreateModalEscClosesOverlayThenModal(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreateShell("sidecar")
	createFormModal(t, m).SetFocus(workspacecreate.FieldProject)
	renderCreateModal(t, m)

	handled, cmd := m.handleCreateShellKey(createKey("esc"))
	if !handled || cmd != nil || !m.CreateOpen() {
		t.Fatalf("first esc closed modal: handled=%v cmd=%v open=%v", handled, cmd != nil, m.CreateOpen())
	}
	handled, cmd = m.handleCreateShellKey(createKey("esc"))
	if !handled || cmd != nil || m.CreateOpen() {
		t.Fatalf("second esc should cancel: handled=%v cmd=%v open=%v", handled, cmd != nil, m.CreateOpen())
	}
}

func TestCreateModalLastProjectAndAgentPersist(t *testing.T) {
	_ = state.SetLastCreateAgent("")
	t.Cleanup(func() { _ = state.SetLastCreateAgent("") })
	var lastProject string
	origLoadP, origSaveP := loadLastGlobalCreateProject, saveLastGlobalCreateProject
	defer func() {
		loadLastGlobalCreateProject, saveLastGlobalCreateProject = origLoadP, origSaveP
	}()
	loadLastGlobalCreateProject = func() string { return lastProject }
	saveLastGlobalCreateProject = func(v string) error { lastProject = v; return nil }

	m := catalogModel(t)
	m.config = &config.Config{Plugins: config.PluginsConfig{Workspace: config.WorkspacePluginConfig{
		DefaultAgentType: "claude",
	}}}
	original := createManagedShell
	defer func() { createManagedShell = original }()
	createManagedShell = func(spec workspaceops.ManagedShellSpec) (workspaceops.ShellResult, error) {
		return workspaceops.ShellResult{SessionName: spec.SessionName}, nil
	}
	m.OpenCreateShell("braid")
	selectCreateAgent(t, m, "grok")
	_ = m.submitCreateShell()()
	if lastProject != "/tmp/braid" || state.GetLastCreateAgent() != "grok" {
		t.Fatalf("persisted project=%q agent=%q", lastProject, state.GetLastCreateAgent())
	}

	fresh := catalogModel(t)
	fresh.config = m.config
	fresh.workspaces.SetItems(nil)
	fresh.OpenCreate("")
	if project, ok := fresh.selectedCreateProject(); !ok || project.Name != "braid" {
		t.Fatalf("fresh project = %+v ok=%v, want braid", project, ok)
	}
	if fresh.createForm.Agent() != "grok" {
		t.Fatalf("fresh agent = %q, want grok", fresh.createForm.Agent())
	}
}

func TestCreateModalComboQuerySurvivesUnrelatedRebuild(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreateShell("sidecar")
	md := createFormModal(t, m)
	md.SetFocus(workspacecreate.FieldAgent)
	clearFocusedCombo(m)
	m.handleCreateShellKey(createKey("g"))
	if m.createForm.Agent() == "g" {
		t.Fatal("typing a filter should not commit agent id g")
	}
	m.width = 40
	renderCreateModal(t, m)
	if got := m.activeCreateModal().FocusedID(); got != workspacecreate.FieldAgent {
		t.Fatalf("rebuild dropped agent focus: %q", got)
	}
}

func TestCreateModalOverlayDoesNotChangeHeight(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreateShell("sidecar")
	createFormModal(t, m).SetFocus(workspacecreate.FieldName)
	closed := renderCreateModal(t, m)
	createFormModal(t, m).SetFocus(workspacecreate.FieldProject)
	open := renderCreateModal(t, m)
	if lipgloss.Height(closed) != lipgloss.Height(open) {
		t.Fatalf("combo overlay changed height: closed=%d open=%d", lipgloss.Height(closed), lipgloss.Height(open))
	}
}

func createdShellWorkspace(project Project) workspaceinventory.Workspace {
	return workspaceinventory.Workspace{
		ID: "created", ProjectKey: projectKey(project), ProjectName: project.Name, ProjectRoot: project.Path,
		Kind: workspaceinventory.KindShell, Key: "sidecar-sh-sidecar-9", TmuxName: "sidecar-sh-sidecar-9", Name: "Shell 9", Live: true,
	}
}

func TestPendingCreatedShellSurvivesMissThenHits(t *testing.T) {
	m := catalogModel(t)
	prev := m.workspaces.SelectedID()
	m.pendingCreatedTmux = "sidecar-sh-sidecar-9"
	project := m.projects[0]
	miss := m.results[projectKey(project)]
	m.applyProjectMutationRefresh(projectMutationRefreshMsg{Project: project, Result: miss})
	if m.pendingCreatedTmux != "sidecar-sh-sidecar-9" {
		t.Fatal("pending cleared by a refresh that omitted the session")
	}
	if got := m.workspaces.SelectedID(); got != prev {
		t.Fatalf("missed refresh stole selection: %q -> %q", prev, got)
	}

	hit := miss
	hit.Workspaces = append(append([]workspaceinventory.Workspace(nil), miss.Workspaces...), createdShellWorkspace(project))
	m.applyProjectMutationRefresh(projectMutationRefreshMsg{Project: project, Result: hit})
	if m.pendingCreatedTmux != "" || m.pendingCreatedPath != "" {
		t.Fatalf("pending still set after hit: tmux=%q path=%q", m.pendingCreatedTmux, m.pendingCreatedPath)
	}
	if got := m.workspaces.SelectedID(); got != "created" {
		t.Fatalf("selected = %q, want created", got)
	}
}

func TestPendingCreatedShellSurvivesPollWithoutSession(t *testing.T) {
	m := catalogModel(t)
	m.ctx, m.cancel = context.WithCancel(context.Background())
	t.Cleanup(func() {
		if m.cancel != nil {
			m.cancel()
		}
	})
	m.generation = 3
	prev := m.workspaces.SelectedID()
	m.pendingCreatedTmux = "sidecar-sh-sidecar-9"
	project := m.projects[0]
	without := m.results[projectKey(project)]

	m.Update(pollMsg{Generation: 2})
	if m.pendingCreatedTmux != "sidecar-sh-sidecar-9" {
		t.Fatal("stale poll generation cleared pending")
	}

	_ = m.start(m.projects, "poll")
	if m.pendingCreatedTmux != "sidecar-sh-sidecar-9" {
		t.Fatal("start poll cleared pending before the session existed")
	}
	if got := m.workspaces.SelectedID(); got != prev {
		t.Fatalf("start poll stole selection: %q -> %q", prev, got)
	}

	m.Update(projectMsg{Generation: m.generation, Project: project, Phase: phaseStatus, Result: without})
	if m.pendingCreatedTmux != "sidecar-sh-sidecar-9" {
		t.Fatal("poll status without the session cleared pending")
	}
	if got := m.workspaces.SelectedID(); got != prev {
		t.Fatalf("poll status stole selection: %q -> %q", prev, got)
	}

	with := without
	with.Workspaces = append(append([]workspaceinventory.Workspace(nil), without.Workspaces...), createdShellWorkspace(project))
	m.Update(projectMsg{Generation: m.generation, Project: project, Phase: phaseStatus, Result: with})
	if m.pendingCreatedTmux != "" {
		t.Fatal("pending not cleared after poll presented the session")
	}
	if got := m.workspaces.SelectedID(); got != "created" {
		t.Fatalf("selected = %q, want created", got)
	}
}

func TestFailedShellCreateClearsPendingWithoutMovingSelection(t *testing.T) {
	m := catalogModel(t)
	prev := m.workspaces.SelectedID()
	m.pendingCreatedTmux = "sidecar-sh-sidecar-9"
	m.Update(globalShellCreatedMsg{Project: m.projects[0], Tmux: "sidecar-sh-sidecar-9", Err: errors.New("tmux failed")})
	if m.pendingCreatedTmux != "" {
		t.Fatal("failed create left pending set")
	}
	if got := m.workspaces.SelectedID(); got != prev {
		t.Fatalf("failed create moved selection to %q", got)
	}
}

func TestNewerCreateReplacesPendingTmux(t *testing.T) {
	m := catalogModel(t)
	original := createManagedShell
	defer func() { createManagedShell = original }()
	createManagedShell = func(spec workspaceops.ManagedShellSpec) (workspaceops.ShellResult, error) {
		return workspaceops.ShellResult{SessionName: spec.SessionName}, nil
	}
	m.pendingCreatedTmux = "old-session"
	m.pendingCreatedPath = "/tmp/stale"
	m.OpenCreateShell("sidecar")
	_ = m.submitCreateShell()
	if m.pendingCreatedTmux == "" || m.pendingCreatedTmux == "old-session" {
		t.Fatalf("pending tmux = %q, want the newer session", m.pendingCreatedTmux)
	}
	if m.pendingCreatedPath != "" {
		t.Fatalf("newer shell create left stale path %q", m.pendingCreatedPath)
	}
}

func TestMutationRefreshClaimsTheCreatedShell(t *testing.T) {
	m := catalogModel(t)
	project := m.projects[0]
	result := m.results[projectKey(project)]
	result.Workspaces = append(result.Workspaces, workspaceinventory.Workspace{
		ID: "created", ProjectKey: projectKey(project), ProjectRoot: project.Path,
		Kind: workspaceinventory.KindShell, TmuxName: "sidecar-sh-sidecar-9", Name: "Shell 9", Live: true,
	})
	m.applyProjectMutationRefresh(projectMutationRefreshMsg{Project: project, Result: result})
	if !m.shellClaims.Sessions["sidecar-sh-sidecar-9"] || m.shellClaims.Owners["sidecar-sh-sidecar-9"] != projectKey(project) {
		t.Fatalf("created shell missing from claims: %#v", m.shellClaims)
	}
}

func TestLiveOnlyStatusDoesNotDropAJustCreatedShell(t *testing.T) {
	m := catalogModel(t)
	project := m.projects[0]
	key := projectKey(project)
	m.liveOnly = true
	m.cycleStart = time.Now().Add(-time.Second)
	created := m.results[key]
	created.Workspaces = append(created.Workspaces, workspaceinventory.Workspace{
		ID: "created", ProjectKey: key, ProjectRoot: project.Path,
		Kind: workspaceinventory.KindShell, TmuxName: "sidecar-sh-sidecar-9", Name: "Shell 9", Live: true, PaneID: "%9",
	})
	m.results[key] = created
	m.markInventoryFresh(key)

	stale := created
	stale.Workspaces = append([]workspaceinventory.Workspace(nil), created.Workspaces[:len(created.Workspaces)-1]...)
	m.applyStatusResult(stale)
	if _, ok := workspaceNamed(m.results[key], "sidecar-sh-sidecar-9"); !ok {
		t.Fatal("live-only status dropped a shell created after this cycle started")
	}
}

func TestLiveOnlyStatusStillAppliesWhenInventoryIsOlder(t *testing.T) {
	m := catalogModel(t)
	project := m.projects[0]
	key := projectKey(project)
	m.liveOnly = true
	m.markInventoryFresh(key)
	m.cycleStart = time.Now().Add(time.Second)
	incoming := workspaceinventory.ProjectResult{ProjectKey: key, Workspaces: []workspaceinventory.Workspace{
		{ID: "only", ProjectKey: key, Kind: workspaceinventory.KindShell, TmuxName: "only", Live: true},
	}}
	m.applyStatusResult(incoming)
	if _, ok := workspaceNamed(m.results[key], "only"); !ok {
		t.Fatal("live-only status with older inventory was skipped")
	}
	if _, ok := workspaceNamed(m.results[key], "sidecar-sh-1"); ok {
		t.Fatal("live-only status did not replace older membership")
	}
}

func workspaceNamed(result workspaceinventory.ProjectResult, tmuxName string) (workspaceinventory.Workspace, bool) {
	for _, workspace := range result.Workspaces {
		if workspace.TmuxName == tmuxName {
			return workspace, true
		}
	}
	return workspaceinventory.Workspace{}, false
}

func TestCancelCreateClearsPending(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreateShell("sidecar")
	m.pendingCreatedTmux = "should-clear"
	m.applyCreateAction(globalCreateCancelID)
	if m.pendingCreatedTmux != "" {
		t.Fatal("cancel-without-create left pending set")
	}
}

func TestPendingCreatedWorktreeSurvivesMissThenHits(t *testing.T) {
	m := catalogModel(t)
	m.showIdleWorktrees = true
	prev := m.workspaces.SelectedID()
	m.pendingCreatedPath = "/tmp/created"
	project := m.projects[0]
	miss := m.results[projectKey(project)]
	m.applyProjectMutationRefresh(projectMutationRefreshMsg{Project: project, Result: miss})
	if m.pendingCreatedPath != "/tmp/created" {
		t.Fatal("worktree pending cleared by a miss")
	}
	if got := m.workspaces.SelectedID(); got != prev {
		t.Fatalf("worktree miss stole selection: %q -> %q", prev, got)
	}

	hit := miss
	hit.Workspaces = append(append([]workspaceinventory.Workspace(nil), miss.Workspaces...), workspaceinventory.Workspace{
		ID: "created-worktree", ProjectKey: projectKey(project), ProjectName: project.Name, ProjectRoot: project.Path,
		Kind: workspaceinventory.KindWorktree, Key: "created", Path: "/tmp/created", Name: "Created",
	})
	m.applyProjectMutationRefresh(projectMutationRefreshMsg{Project: project, Result: hit})
	if m.pendingCreatedPath != "" {
		t.Fatal("worktree pending not cleared after hit")
	}
	if got := m.workspaces.SelectedID(); got != "created-worktree" {
		t.Fatalf("selected = %q, want created worktree", got)
	}
}

func TestOpenCreateIsWorktreeWithKindFocused(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreate("")
	md := createFormModal(t, m)
	if m.createForm.Kind() != workspacecreate.KindWorktree {
		t.Fatalf("kind = %v, want worktree", m.createForm.Kind())
	}
	if md.FocusedID() != workspacecreate.FieldKind {
		t.Fatalf("focus = %q, want kind", md.FocusedID())
	}
}

func TestOpenCreateShellIsShellWithNameFocused(t *testing.T) {
	m := catalogModel(t)
	m.OpenCreateShell("sidecar")
	md := createFormModal(t, m)
	if m.createForm.Kind() != workspacecreate.KindShell {
		t.Fatalf("kind = %v, want shell", m.createForm.Kind())
	}
	if md.FocusedID() != workspacecreate.FieldName {
		t.Fatalf("focus = %q, want name", md.FocusedID())
	}
	if m.createBusy {
		t.Fatal("ctrl+n/OpenCreateShell must stay a modal, not instant create")
	}
}

func TestPlanCreateWorktreePassesFormBaseNotHEAD(t *testing.T) {
	m := catalogModel(t)
	var gotBase string
	original := resolveGlobalWorktree
	defer func() { resolveGlobalWorktree = original }()
	resolveGlobalWorktree = func(_ context.Context, _, _, _, base string, _ bool, _ config.WorktreeSetupConfig) (*workspaceops.WorktreePlan, error) {
		gotBase = base
		return &workspaceops.WorktreePlan{Branch: "feature", Path: "/tmp/feature"}, nil
	}
	m.OpenCreateWorktree("sidecar")
	typeCreateName(t, m, "feature")
	m.createForm.SetBranches([]string{"main", "develop"}, "develop")
	msg := m.planCreateWorktree()().(globalWorktreePlannedMsg)
	if gotBase == "HEAD" {
		t.Fatal("ResolveWorktreePlan was called with hardcoded HEAD")
	}
	if gotBase != "develop" {
		t.Fatalf("base = %q, want develop", gotBase)
	}
	if msg.Plan == nil {
		t.Fatal("expected a plan")
	}
}

func TestSkipPermsReachesShellAndWorktree(t *testing.T) {
	stubGlobalAgentShellReady(t)
	_ = state.SetLastCreateAgent("")
	_ = state.SetAgentAutoApprove("claude", false)
	m := catalogModel(t)
	m.config = &config.Config{Plugins: config.PluginsConfig{Workspace: config.WorkspacePluginConfig{
		DefaultAgentType: "claude",
		AgentStart:       map[string]string{"claude": "claude-custom"},
	}}}
	originalCreate, originalStart, originalAgent := createManagedShell, startGlobalAgent, resolveGlobalAgentCmd
	defer func() {
		createManagedShell, startGlobalAgent, resolveGlobalAgentCmd = originalCreate, originalStart, originalAgent
	}()
	var spec workspaceops.ManagedShellSpec
	createManagedShell = func(got workspaceops.ManagedShellSpec) (workspaceops.ShellResult, error) {
		spec = got
		return workspaceops.ShellResult{SessionName: got.SessionName}, nil
	}
	var agentSkip bool
	resolveGlobalAgentCmd = func(path, agent string, configured map[string]string, skip bool) string {
		agentSkip = skip
		return originalAgent(path, agent, configured, skip)
	}
	startGlobalAgent = func(_ context.Context, req agentcontrol.StartRequest) (agentcontrol.Agent, error) {
		return agentcontrol.Agent{Target: req.Target}, nil
	}

	m.OpenCreateShell("sidecar")
	md := createFormModal(t, m)
	md.SetFocus(workspacecreate.FieldSkip)
	renderCreateModal(t, m)
	if !m.createForm.ShowSkip() {
		t.Fatal("claude should show auto-approve")
	}
	m.handleCreateShellKey(createKey(" "))
	if !m.createForm.SkipPerms() {
		t.Fatal("space should enable auto-approve")
	}
	msg := m.submitCreateShell()().(globalShellCreatedMsg)
	if msg.Err != nil || !spec.SkipPerms || !agentSkip {
		t.Fatalf("shell skip not applied: spec=%+v agentSkip=%v err=%v", spec, agentSkip, msg.Err)
	}

	originalPlan := resolveGlobalWorktree
	defer func() { resolveGlobalWorktree = originalPlan }()
	resolveGlobalWorktree = func(context.Context, string, string, string, string, bool, config.WorktreeSetupConfig) (*workspaceops.WorktreePlan, error) {
		return &workspaceops.WorktreePlan{Branch: "feature", Path: "/tmp/feature"}, nil
	}
	m.OpenCreateWorktree("sidecar")
	typeCreateName(t, m, "feature")
	md = createFormModal(t, m)
	md.SetFocus(workspacecreate.FieldSkip)
	renderCreateModal(t, m)
	if m.createForm.SkipPerms() {
		m.handleCreateShellKey(createKey(" "))
	}
	if m.createForm.SkipPerms() {
		t.Fatal("expected skip off before toggling on")
	}
	m.handleCreateShellKey(createKey(" "))
	if !m.createForm.SkipPerms() {
		t.Fatal("expected skip on for worktree")
	}
	planMsg := m.planCreateWorktree()().(globalWorktreePlannedMsg)
	if planMsg.Plan == nil || !planMsg.Plan.SkipPerms {
		t.Fatalf("worktree SkipPerms not set: %+v", planMsg.Plan)
	}
}

func TestProjectChangeReloadsBranches(t *testing.T) {
	m := catalogModel(t)
	origList, origCurrent := listCreateBranches, currentCreateBranch
	defer func() {
		listCreateBranches, currentCreateBranch = origList, origCurrent
	}()
	var dirs []string
	listCreateBranches = func(_ context.Context, dir string) ([]string, error) {
		dirs = append(dirs, dir)
		if dir == "/tmp/braid" {
			return []string{"braid-main"}, nil
		}
		return []string{"sidecar-main"}, nil
	}
	currentCreateBranch = func(_ context.Context, dir string) (string, error) {
		if dir == "/tmp/braid" {
			return "braid-main", nil
		}
		return "sidecar-main", nil
	}

	if cmd := m.OpenCreateWorktree("sidecar"); cmd != nil {
		runCreateCmd(t, m, cmd)
	}
	if m.createForm.BaseBranch() != "sidecar-main" {
		t.Fatalf("initial base = %q, want sidecar-main", m.createForm.BaseBranch())
	}
	if len(dirs) != 1 || dirs[0] != "/tmp/sidecar" {
		t.Fatalf("open loaded dirs = %v, want [/tmp/sidecar]", dirs)
	}

	md := createFormModal(t, m)
	md.SetFocus(workspacecreate.FieldProject)
	_, cmd := m.handleCreateShellKey(createKey("down"))
	if project, ok := m.selectedCreateProject(); !ok || project.Name != "braid" {
		t.Fatalf("project after down = %+v ok=%v, want braid", project, ok)
	}
	if cmd == nil {
		t.Fatal("project change should reload branches")
	}
	runCreateCmd(t, m, cmd)
	if m.createForm.BaseBranch() != "braid-main" {
		t.Fatalf("base after project change = %q, want braid-main", m.createForm.BaseBranch())
	}
	if len(dirs) != 2 || dirs[1] != "/tmp/braid" {
		t.Fatalf("reload dirs = %v, want sidecar then braid", dirs)
	}
}
