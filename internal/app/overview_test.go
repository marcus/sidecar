package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/overview"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/workspaceinventory"
)

type navigationPlugin struct {
	id        string
	focused   bool
	inits     int
	keyInputs int
	pending   *plugin.PendingWorkspaceSelection
	// terminal lifecycle counters model a project Workspaces Output surface:
	// focus owns the terminal; background size/messages must not reach it.
	terminalOpen     bool
	terminalResizes  int
	terminalMsgs     int
	mouseClicks      int
	focusChanges     []bool
	focusNotices     int
	refreshes        int
	clearFocusOnInit bool
	hostBackground   string
}

func (p *navigationPlugin) ID() string   { return p.id }
func (p *navigationPlugin) Name() string { return p.id }
func (p *navigationPlugin) Icon() string { return "" }
func (p *navigationPlugin) Init(*plugin.Context) error {
	p.inits++
	if p.clearFocusOnInit {
		p.SetFocused(false)
	}
	return nil
}
func (p *navigationPlugin) Start() tea.Cmd {
	p.refreshes++
	return nil
}
func (p *navigationPlugin) Stop() {}
func (p *navigationPlugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	if background, ok := msg.(termpreview.HostBackgroundMsg); ok {
		p.hostBackground = background.ANSI
	}
	if _, ok := msg.(tea.MouseClickMsg); ok {
		p.mouseClicks++
	}
	if _, ok := msg.(tea.KeyPressMsg); ok {
		p.keyInputs++
	}
	if _, ok := msg.(plugin.PluginFocusedMsg); ok {
		p.focusNotices++
		if p.focused {
			p.refreshes++
			p.terminalOpen = true
		}
	}
	if p.terminalOpen {
		if _, ok := msg.(tea.WindowSizeMsg); ok {
			p.terminalResizes++
		}
		if tty.IsTerminalMessage(msg) {
			p.terminalMsgs++
		}
	}
	return p, nil
}
func (p *navigationPlugin) View(int, int) string { return "" }
func (p *navigationPlugin) IsFocused() bool      { return p.focused }
func (p *navigationPlugin) SetFocused(f bool) {
	p.focused = f
	if !f {
		p.terminalOpen = false
	}
	p.focusChanges = append(p.focusChanges, f)
}
func (p *navigationPlugin) Commands() []plugin.Command { return nil }
func (p *navigationPlugin) FocusContext() string       { return p.id }
func (p *navigationPlugin) SetPendingWorkspaceSelection(s plugin.PendingWorkspaceSelection) {
	p.pending = &s
}

type countingOverviewRunner struct{ calls int }

func (r *countingOverviewRunner) Output(context.Context, string, ...string) ([]byte, error) {
	r.calls++
	return nil, nil
}

func TestCrossProjectOverviewFlagOffPreservesSwitcherAndDoesNoWork(t *testing.T) {
	cfg := config.Default()
	if cfg.Features.Flags == nil {
		cfg.Features.Flags = map[string]bool{}
	}
	cfg.Features.Flags[features.CrossProjectOverview.Name] = false
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })
	cfg.Projects.List = []config.ProjectConfig{{Name: "one", Path: "/tmp/one"}}
	m := New(plugin.NewRegistry(nil), keymap.NewRegistry(), cfg, "", "/tmp/one", "/tmp/one", "")
	if m.overview != nil {
		t.Fatal("overview constructed while feature is disabled")
	}
	m.initProjectSwitcher()
	if len(m.projectSwitcherFiltered) != 1 || m.projectSwitcherFiltered[0].Kind != destinationProject {
		t.Fatalf("flag-off destinations = %#v", m.projectSwitcherFiltered)
	}
}

func asAppModel(t *testing.T, model tea.Model) Model {
	t.Helper()
	switch m := model.(type) {
	case Model:
		return m
	case *Model:
		return *m
	default:
		t.Fatalf("unexpected model type %T", model)
		return Model{}
	}
}

func TestLogoClickAndKToggleOverview(t *testing.T) {
	cfg := config.Default()
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })
	cfg.Projects.List = []config.ProjectConfig{{Name: "one", Path: "/tmp/one"}}
	m := New(plugin.NewRegistry(nil), keymap.NewRegistry(), cfg, "", "/tmp/one", "/tmp/one", "")
	if m.overview == nil {
		t.Fatal("overview should be constructed when feature defaults on")
	}
	m.intro.Active = false
	m.intro.Done = true
	m.width, m.height, m.ready = 120, 40, true

	start, end, ok := m.getLogoBounds()
	if !ok || end <= start {
		t.Fatalf("logo bounds = %d-%d ok=%v", start, end, ok)
	}
	// Click in the middle of "Sidecar"
	x := (start + end) / 2
	updated, cmd := m.Update(tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft})
	m = asAppModel(t, updated)
	if !m.inGlobalScope() {
		t.Fatal("logo click did not open overview")
	}
	if cmd == nil {
		t.Fatal("logo click should start overview load")
	}

	// K toggles closed (handleKeyMsg returns *Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: "K", Mod: tea.ModShift})
	m = asAppModel(t, updated)
	if m.inGlobalScope() {
		t.Fatal("K did not close overview")
	}

	// K opens again
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'k', Text: "K", Mod: tea.ModShift})
	m = asAppModel(t, updated)
	if !m.inGlobalScope() || cmd == nil {
		t.Fatal("K did not reopen overview")
	}

	// Clicking logo while open toggles closed
	updated, _ = m.Update(tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft})
	m = asAppModel(t, updated)
	if m.inGlobalScope() {
		t.Fatal("logo click while open should close overview")
	}
}

func TestCompactOverviewKeepsAppHeaderAndFooterAt72x30(t *testing.T) {
	cfg := config.Default()
	registry := plugin.NewRegistry(nil)
	for _, name := range []string{"td", "git", "files", "conversations", "workspaces"} {
		if err := registry.Register(&navigationPlugin{id: name}); err != nil {
			t.Fatal(err)
		}
	}
	m := New(registry, keymap.NewRegistry(), cfg, "", "/tmp/one", "/tmp/one", "git")
	m.overview = overview.New(workspaceinventory.Collector{Runner: &countingOverviewRunner{}})
	m.scope = ScopeGlobal
	m.globalTab = GlobalActivity
	m.intro.Active = false
	m.width, m.height, m.ready = 72, 30, true
	panes := m.overview.Start(nil)()
	_ = m.overview.Update(panes) // build the production five-lane empty board; leave poll timer unexecuted

	view := m.viewContent()
	if got := lipgloss.Height(view); got != 30 {
		t.Fatalf("app height = %d, want 30\n%s", got, view)
	}
	lines := strings.Split(view, "\n")
	first := ansi.Strip(lines[0])
	if !strings.Contains(first, "Sidecar") || !strings.Contains(first, "Activity") || !strings.Contains(first, "Select Project") || !strings.Contains(ansi.Strip(lines[len(lines)-1]), "Open") {
		t.Fatalf("compact viewport hid app chrome: first=%q last=%q", lines[0], lines[len(lines)-1])
	}
	if got := lipgloss.Width(lines[0]); got != 72 {
		t.Fatalf("header width = %d, want 72: %q", got, lines[0])
	}
	plainHeader := ansi.Strip(lines[0])
	// The global space owns the tab row: its own tabs, and only its own. The
	// project plugin tabs belong to the space the user is not in.
	if !strings.Contains(plainHeader, "Activity") || !strings.Contains(plainHeader, "Sessions") {
		t.Fatalf("narrow global header omitted its own tabs: %q", plainHeader)
	}
	for _, projectTab := range []string{"td", "git", "files", "conversations"} {
		if strings.Contains(plainHeader, projectTab) {
			t.Fatalf("global header still shows the project tab %q: %q", projectTab, plainHeader)
		}
	}
	if got := len(m.getTabBounds()); got != 2 {
		t.Fatalf("global tab bounds = %d, want one per global tab: %#v", got, m.getTabBounds())
	}
	// Project space keeps its existing full-tab layout, wide and narrow.
	wide := m
	wide.scope = ScopeProject
	wide.width = 140
	if header := ansi.Strip(wide.renderHeader()); !strings.Contains(header, "workspaces") || len(wide.getTabBounds()) != 7 {
		t.Fatalf("wide header changed existing full-tab layout: %q bounds=%#v", header, wide.getTabBounds())
	}
	if !strings.Contains(ansi.Strip(lines[1]), "Agent Overview") {
		t.Fatalf("content did not begin at global row 1: %q", lines[1])
	}
	// App mouse routing subtracts the one header row; local compact card row 1
	// therefore begins at global row 2.
	adjusted := offsetMouseY(tea.MouseClickMsg{X: 1, Y: 2, Button: tea.MouseLeft}, -headerHeight)
	if got := adjusted.Mouse().Y; got != 1 {
		t.Fatalf("compact content mouse local Y = %d, want 1", got)
	}
	updatedModel, _ := m.Update(tea.MouseClickMsg{X: 1, Y: 2, Button: tea.MouseLeft})
	if !updatedModel.(Model).inGlobalScope() {
		t.Fatal("content-row click was misrouted as wrapped header")
	}
}

func TestOverviewPinnedFilteredAndActivationIsLazy(t *testing.T) {
	cfg := config.Default()
	cfg.Features.Flags[features.CrossProjectOverview.Name] = true
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })
	cfg.Projects.List = []config.ProjectConfig{{Name: "one", Path: "/tmp/one"}, {Name: "two", Path: "/tmp/two"}, {Name: "three", Path: "/tmp/three"}}
	runner := &countingOverviewRunner{}
	m := New(plugin.NewRegistry(nil), keymap.NewRegistry(), cfg, "", "/tmp/one", "/tmp/one", "")
	m.overview = overview.New(workspaceinventory.Collector{Runner: runner})
	m.initProjectSwitcher()
	if got := m.projectSwitcherFiltered[0]; got.Kind != destinationOverview || got.Name != "Overview" {
		t.Fatalf("first destination = %#v", got)
	}
	m.projectSwitcherInput.SetValue("two")
	m.projectSwitcherFiltered = m.projectSwitcherDestinations("two")
	if len(m.projectSwitcherFiltered) != 2 || m.projectSwitcherFiltered[0].Kind != destinationOverview || m.projectSwitcherFiltered[1].Name != "two" {
		t.Fatalf("filtered destinations = %#v", m.projectSwitcherFiltered)
	}
	if runner.calls != 0 {
		t.Fatalf("collector ran before activation: %d", runner.calls)
	}
	if got := m.overviewProjects(); len(got) != 3 {
		t.Fatalf("Overview projects = %#v, want all configured projects", got)
	}
	workDir, projectRoot, pluginIndex := m.ui.WorkDir, m.ui.ProjectRoot, m.activePlugin
	cmd := m.activateProjectSwitcherDestination(m.projectSwitcherFiltered[0])
	if cmd == nil || !m.inGlobalScope() {
		t.Fatal("Overview activation did not start its loading command")
	}
	if runner.calls != 0 {
		t.Fatalf("collector ran synchronously during activation: %d", runner.calls)
	}
	if m.ui.WorkDir != workDir || m.ui.ProjectRoot != projectRoot || m.activePlugin != pluginIndex {
		t.Fatal("Overview activation changed underlying project/plugin state")
	}
	if got := m.activeDestinationName(); got != "Overview" {
		t.Fatalf("header destination = %q", got)
	}
}

func TestProjectSwitcherLinkedWorktreeCursorFlagParity(t *testing.T) {
	cfg := config.Default()
	cfg.Projects.List = []config.ProjectConfig{{Name: "other", Path: "/repo/other"}, {Name: "main", Path: "/repo/main"}}
	m := Model{cfg: cfg, ui: &UIState{WorkDir: "/repo/worktrees/topic", ProjectRoot: "/repo/main"}}
	m.initProjectSwitcher()
	if m.projectSwitcherCursor != 0 {
		t.Fatalf("flag-off linked-worktree cursor = %d, want pre-feature zero", m.projectSwitcherCursor)
	}
	m.overview = overview.New(workspaceinventory.Collector{Runner: &countingOverviewRunner{}})
	m.initProjectSwitcher()
	// The cursor is asserted by identity, not position: the collection is
	// ordered by the user's chosen sort, so "the configured main checkout" is
	// the fact under test and its row index is not.
	if m.projectSwitcherCursor <= 0 || m.projectSwitcherCursor >= len(m.projectSwitcherFiltered) {
		t.Fatalf("enabled linked-worktree cursor = %d, want a project after pinned Overview", m.projectSwitcherCursor)
	}
	if got := m.projectSwitcherFiltered[m.projectSwitcherCursor]; got.Path != "/repo/main" {
		t.Fatalf("enabled linked-worktree cursor landed on %q, want the configured main checkout", got.Path)
	}
	if m.projectSwitcherFiltered[0].Kind != destinationOverview {
		t.Fatal("Overview must stay pinned ahead of the collection")
	}
	m.scope = ScopeGlobal
	m.initProjectSwitcher()
	if m.projectSwitcherCursor != 0 {
		t.Fatalf("active Overview cursor = %d, want pinned Overview", m.projectSwitcherCursor)
	}
}

func TestValidatedCrossProjectNavigationFocusesWorkspaceWithoutInput(t *testing.T) {
	source := newOverviewGitRepo(t, "source")
	target := newOverviewGitRepo(t, "target")
	stateBase := t.TempDir()
	config.SetTestStateDir(stateBase)
	t.Cleanup(config.ResetTestStateDir)
	worktreeState, err := projectdir.WorktreeDirWithBase(stateBase, target, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeState, "agent"), []byte("codex\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Projects.List = []config.ProjectConfig{{Name: "source", Path: source}, {Name: "target", Path: target}}
	config.SetTestConfigPath(filepath.Join(t.TempDir(), "config.json"))
	t.Cleanup(config.ResetTestConfigPath)
	km := keymap.NewRegistry()
	ctx := &plugin.Context{WorkDir: source, ProjectRoot: source, Config: cfg, Keymap: km}
	reg := plugin.NewRegistry(ctx)
	gitPlugin := &navigationPlugin{id: "git"}
	workspacePlugin := &navigationPlugin{id: workspacePluginID}
	if err := reg.Register(gitPlugin); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(workspacePlugin); err != nil {
		t.Fatal(err)
	}
	m := New(reg, km, cfg, "", source, source, "git")
	m.overview = overview.New(workspaceinventory.Collector{})
	m.scope = ScopeGlobal
	workspace := workspaceinventory.Workspace{ProjectKey: workspaceinventory.CanonicalPath(target), ProjectRoot: target, Kind: workspaceinventory.KindWorktree, Key: workspaceinventory.CanonicalPath(target), Path: target}
	navigation := overviewNavigation(t, m.overview, workspace)
	initialGitInits, initialWorkspaceInits := gitPlugin.inits, workspacePlugin.inits
	updatedModel, validationCmd := m.Update(navigation)
	m = updatedModel.(Model)
	if validationCmd == nil || m.ui.WorkDir != source || gitPlugin.inits != initialGitInits || workspacePlugin.inits != initialWorkspaceInits {
		t.Fatalf("pre-validation state: cmd=%v work=%q gitInits=%d workspaceInits=%d", validationCmd != nil, m.ui.WorkDir, gitPlugin.inits, workspacePlugin.inits)
	}
	validation, ok := validationCmd().(overview.ValidationMsg)
	if !ok || validation.Err != nil {
		t.Fatalf("validation result = %#v", validation)
	}
	updatedModel, cmd := m.Update(validation)
	updated := updatedModel.(Model)
	if cmd == nil || updated.activePlugin != 1 || !workspacePlugin.focused {
		t.Fatalf("navigation focus: cmd=%v active=%d focused=%v", cmd != nil, updated.activePlugin, workspacePlugin.focused)
	}
	if workspacePlugin.pending == nil || workspacePlugin.pending.Path != target {
		t.Fatalf("pending selection = %#v", workspacePlugin.pending)
	}
	if workspacePlugin.keyInputs != 0 {
		t.Fatalf("navigation sent %d key inputs", workspacePlugin.keyInputs)
	}
}

func TestOverviewWorktreeNavigationScope(t *testing.T) {
	tests := []struct {
		name      string
		scope     string
		card      string
		saveTopic bool
		want      string
	}{
		{name: "project root by default", scope: config.OverviewWorktreeScopeProject, card: "worktree", want: "main"},
		{name: "worktree scope when configured", scope: config.OverviewWorktreeScopeWorktree, card: "worktree", want: "worktree"},
		{name: "configured main card ignores saved linked worktree", scope: config.OverviewWorktreeScopeWorktree, card: "main", saveTopic: true, want: "main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := newOverviewGitRepo(t, "source")
			main := newOverviewGitRepo(t, "main")
			commitOverviewFixture(t, main)
			worktree := filepath.Join(t.TempDir(), "topic")
			runOverviewGit(t, main, "worktree", "add", "-q", "-b", "topic", worktree)
			if err := state.InitWithDir(t.TempDir()); err != nil {
				t.Fatal(err)
			}
			if tt.saveTopic {
				if err := state.SetLastWorktreePath(workspaceinventory.CanonicalPath(main), workspaceinventory.CanonicalPath(worktree)); err != nil {
					t.Fatal(err)
				}
			}

			cfg := config.Default()
			cfg.Plugins.Workspace.OverviewWorktreeScope = tt.scope
			km := keymap.NewRegistry()
			ctx := &plugin.Context{WorkDir: source, ProjectRoot: source, Config: cfg, Keymap: km}
			reg := plugin.NewRegistry(ctx)
			workspacePlugin := &navigationPlugin{id: workspacePluginID}
			if err := reg.Register(workspacePlugin); err != nil {
				t.Fatal(err)
			}
			m := New(reg, km, cfg, "", source, source, workspacePluginID)
			m.overview = overview.New(workspaceinventory.Collector{})
			m.scope = ScopeGlobal
			cardPath := worktree
			if tt.card == "main" {
				cardPath = main
			}
			workspace := workspaceinventory.Workspace{
				ProjectKey:  workspaceinventory.CanonicalPath(main),
				ProjectRoot: main,
				Kind:        workspaceinventory.KindWorktree,
				Key:         workspaceinventory.CanonicalPath(cardPath),
				Path:        cardPath,
			}

			cmd := m.navigateFromOverview(workspace)
			if cmd == nil {
				t.Fatal("navigation returned no command")
			}
			wantPath := main
			if tt.want == "worktree" {
				wantPath = worktree
			}
			if workspaceinventory.CanonicalPath(m.ui.WorkDir) != workspaceinventory.CanonicalPath(wantPath) ||
				workspaceinventory.CanonicalPath(m.ui.ProjectRoot) != workspaceinventory.CanonicalPath(main) {
				t.Fatalf("navigation scope: WorkDir=%q ProjectRoot=%q, want %q and %q", m.ui.WorkDir, m.ui.ProjectRoot, wantPath, main)
			}
			if workspacePlugin.pending == nil || workspacePlugin.pending.Kind != plugin.WorkspaceSelectionWorktree || workspacePlugin.pending.Path != cardPath {
				t.Fatalf("pending selection = %#v, want exact worktree %q", workspacePlugin.pending, cardPath)
			}
			if !workspacePlugin.focused {
				t.Fatal("Workspaces plugin was not focused")
			}
		})
	}
}

func TestOverviewShellNavigationLeavesLinkedWorktreeScope(t *testing.T) {
	main := newOverviewGitRepo(t, "main")
	commitOverviewFixture(t, main)
	worktree := filepath.Join(t.TempDir(), "topic")
	runOverviewGit(t, main, "worktree", "add", "-q", "-b", "topic", worktree)
	if err := state.InitWithDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	km := keymap.NewRegistry()
	ctx := &plugin.Context{WorkDir: worktree, ProjectRoot: main, Config: cfg, Keymap: km}
	reg := plugin.NewRegistry(ctx)
	workspacePlugin := &navigationPlugin{id: workspacePluginID}
	if err := reg.Register(workspacePlugin); err != nil {
		t.Fatal(err)
	}
	m := New(reg, km, cfg, "", worktree, main, workspacePluginID)
	m.overview = overview.New(workspaceinventory.Collector{})
	m.scope = ScopeGlobal
	workspace := workspaceinventory.Workspace{ProjectRoot: main, Kind: workspaceinventory.KindShell, Key: "shell", Path: main, TmuxName: "shell"}

	if cmd := m.navigateFromOverview(workspace); cmd == nil {
		t.Fatal("navigation returned no command")
	}
	if workspaceinventory.CanonicalPath(m.ui.WorkDir) != workspaceinventory.CanonicalPath(main) {
		t.Fatalf("shell navigation WorkDir = %q, want project root %q", m.ui.WorkDir, main)
	}
	if workspacePlugin.pending == nil || workspacePlugin.pending.Kind != plugin.WorkspaceSelectionShell || workspacePlugin.pending.Key != "shell" {
		t.Fatalf("pending shell selection = %#v", workspacePlugin.pending)
	}
}

func TestFirstVisitProjectSwitchRefocusesActiveWorkspacesAfterReinit(t *testing.T) {
	source := newOverviewGitRepo(t, "source")
	target := newOverviewGitRepo(t, "target")
	isolateAppState(t)

	cfg := config.Default()
	km := keymap.NewRegistry()
	ctx := &plugin.Context{WorkDir: source, ProjectRoot: source, Config: cfg, Keymap: km}
	reg := plugin.NewRegistry(ctx)
	files := &navigationPlugin{id: "files", clearFocusOnInit: true}
	workspaces := &navigationPlugin{id: workspacePluginID, clearFocusOnInit: true}
	for _, p := range []*navigationPlugin{files, workspaces} {
		if err := reg.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	m := New(reg, km, cfg, "", source, source, workspacePluginID)
	workspaces.SetFocused(true)
	before := len(workspaces.focusChanges)
	beforeRefreshes := workspaces.refreshes

	cmd := m.switchProjectWithInventory(target, nil)
	if cmd == nil {
		t.Fatal("project switch returned no lifecycle commands")
	}
	if m.ActivePlugin() != workspaces || !workspaces.focused {
		t.Fatalf("first target visit left active Workspaces hidden: active=%T focused=%v", m.ActivePlugin(), workspaces.focused)
	}
	trueTransitions := 0
	for _, focused := range workspaces.focusChanges[before:] {
		if focused {
			trueTransitions++
		}
	}
	if trueTransitions != 1 {
		t.Fatalf("project switch focus activations = %d, want exactly one; transitions=%v", trueTransitions, workspaces.focusChanges[before:])
	}
	for _, msg := range collectMsgs(cmd) {
		updated, _ := m.Update(msg)
		m = asAppModel(t, updated)
	}
	if got := workspaces.refreshes - beforeRefreshes; got != 1 {
		t.Fatalf("reinit workspace refreshes = %d, want registry.Start only", got)
	}
}

func TestProjectSwitchFallsBackToActivePluginWhenSavedPluginNoLongerExists(t *testing.T) {
	source := newOverviewGitRepo(t, "source")
	target := newOverviewGitRepo(t, "target")
	isolateAppState(t)
	if err := state.SetActivePlugin(target, "removed-plugin"); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	km := keymap.NewRegistry()
	ctx := &plugin.Context{WorkDir: source, ProjectRoot: source, Config: cfg, Keymap: km}
	reg := plugin.NewRegistry(ctx)
	files := &navigationPlugin{id: "files", clearFocusOnInit: true}
	workspaces := &navigationPlugin{id: workspacePluginID, clearFocusOnInit: true}
	for _, p := range []*navigationPlugin{files, workspaces} {
		if err := reg.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	m := New(reg, km, cfg, "", source, source, workspacePluginID)
	workspaces.SetFocused(true)
	m.switchProjectWithInventory(target, nil)
	if m.ActivePlugin() != workspaces || !workspaces.focused {
		t.Fatalf("invalid saved plugin left no focused fallback: active=%T focused=%v", m.ActivePlugin(), workspaces.focused)
	}
}

func TestOverviewNavigationDoesNotMutateBeforeValidation(t *testing.T) {
	source := newOverviewGitRepo(t, "source")
	target := newOverviewGitRepo(t, "target")
	cfg := config.Default()
	m := New(plugin.NewRegistry(nil), keymap.NewRegistry(), cfg, "", source, source, "")
	m.overview = overview.New(workspaceinventory.Collector{Runner: &countingOverviewRunner{}})
	m.scope = ScopeGlobal
	workspace := workspaceinventory.Workspace{
		ProjectKey:  workspaceinventory.CanonicalPath(target),
		ProjectRoot: target,
		Kind:        workspaceinventory.KindWorktree,
		Key:         workspaceinventory.CanonicalPath(target),
		Path:        target,
	}
	navigation := overviewNavigation(t, m.overview, workspace)
	updatedModel, cmd := m.Update(navigation)
	updated := updatedModel.(Model)
	if cmd == nil {
		t.Fatal("navigation did not schedule validation")
	}
	if updated.ui.WorkDir != source || updated.ui.ProjectRoot != source || !updated.inGlobalScope() {
		t.Fatalf("navigation mutated before validation: work=%q root=%q overview=%v", updated.ui.WorkDir, updated.ui.ProjectRoot, updated.inGlobalScope())
	}
}

func TestStaleValidationDoesNotMutateProjectOrReinit(t *testing.T) {
	source := newOverviewGitRepo(t, "source")
	cfg := config.Default()
	km := keymap.NewRegistry()
	ctx := &plugin.Context{WorkDir: source, ProjectRoot: source, Config: cfg, Keymap: km}
	reg := plugin.NewRegistry(ctx)
	p := &navigationPlugin{id: workspacePluginID}
	if err := reg.Register(p); err != nil {
		t.Fatal(err)
	}
	m := New(reg, km, cfg, "", source, source, workspacePluginID)
	m.overview = overview.New(workspaceinventory.Collector{Runner: &countingOverviewRunner{}})
	m.scope = ScopeGlobal
	initialInits := p.inits
	navigation := overviewNavigation(t, m.overview, workspaceinventory.Workspace{})
	updatedModel, cmd := m.Update(overviewValidation(navigation, errors.New("worktree disappeared")))
	updated := updatedModel.(Model)
	if updated.ui.WorkDir != source || updated.ui.ProjectRoot != source || !updated.inGlobalScope() || p.inits != initialInits {
		t.Fatalf("stale navigation mutated state: work=%q root=%q overview=%v inits=%d", updated.ui.WorkDir, updated.ui.ProjectRoot, updated.inGlobalScope(), p.inits)
	}
	if cmd == nil {
		t.Fatal("stale validation did not return toast")
	}
	if toast, ok := cmd().(ToastMsg); !ok || !toast.IsError {
		t.Fatalf("stale result = %#v", cmd())
	}
}

// Both ways of leaving the Agents board end its lifecycle: returning to the
// project, and switching to another global tab. A card activation in flight
// from the old lifecycle must not act afterwards.
func TestOverviewExitBeforeNavigateMsgIgnoresLateActivation(t *testing.T) {
	cases := []struct {
		name        string
		key         tea.KeyPressMsg
		staysGlobal bool
	}{
		{"esc returns to the project", tea.KeyPressMsg{Code: tea.KeyEsc}, false},
		{"8 switches to another global tab", tea.KeyPressMsg{Code: '8', Text: "8"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, p, source := newOverviewRaceModel(t)
			target := newOverviewGitRepo(t, "target")
			navigation := overviewNavigation(t, m.overview, overviewWorkspace(target))
			initialInits := p.inits

			exitedModel, _ := m.handleKeyMsg(tc.key)
			exited := *exitedModel.(*Model)
			updatedModel, cmd := exited.Update(navigation)
			updated := updatedModel.(Model)
			if cmd != nil || updated.inGlobalScope() != tc.staysGlobal || updated.ui.WorkDir != source || p.inits != initialInits {
				t.Fatalf("late NavigateMsg mutated after exit: cmd=%v global=%v work=%q inits=%d",
					cmd != nil, updated.inGlobalScope(), updated.ui.WorkDir, p.inits)
			}
		})
	}
}

func TestOverviewExitBeforeValidationMsgIgnoresLateResult(t *testing.T) {
	m, p, source := newOverviewRaceModel(t)
	target := newOverviewGitRepo(t, "target")
	navigation := overviewNavigation(t, m.overview, overviewWorkspace(target))
	updatedModel, validationCmd := m.Update(navigation)
	m = updatedModel.(Model)
	if validationCmd == nil {
		t.Fatal("current navigation did not schedule validation")
	}
	initialInits := p.inits

	exitedModel, _ := m.Update(FocusPluginByIDMsg{PluginID: "git"})
	exited := exitedModel.(Model)
	updatedModel, cmd := exited.Update(overviewValidation(navigation, nil))
	updated := updatedModel.(Model)
	if cmd != nil || updated.inGlobalScope() || updated.ui.WorkDir != source || p.inits != initialInits {
		t.Fatalf("late ValidationMsg mutated after tab exit: cmd=%v active=%v work=%q inits=%d", cmd != nil, updated.inGlobalScope(), updated.ui.WorkDir, p.inits)
	}
}

func TestOverviewNewerActivationSupersedesOlderValidation(t *testing.T) {
	m, p, source := newOverviewRaceModel(t)
	firstTarget := newOverviewGitRepo(t, "first")
	secondTarget := newOverviewGitRepo(t, "second")
	first := overviewNavigation(t, m.overview, overviewWorkspace(firstTarget))
	updatedModel, firstValidation := m.Update(first)
	m = updatedModel.(Model)
	if firstValidation == nil {
		t.Fatal("first navigation did not schedule validation")
	}
	second := overviewNavigation(t, m.overview, overviewWorkspace(secondTarget))
	updatedModel, secondValidation := m.Update(second)
	m = updatedModel.(Model)
	if secondValidation == nil {
		t.Fatal("second navigation did not schedule validation")
	}
	initialInits := p.inits

	updatedModel, toastCmd := m.Update(overviewValidation(second, errors.New("second target disappeared")))
	m = updatedModel.(Model)
	if toastCmd == nil {
		t.Fatal("current validation error did not produce a toast")
	}
	updatedModel, cmd := m.Update(overviewValidation(first, nil))
	updated := updatedModel.(Model)
	if cmd != nil || !updated.inGlobalScope() || updated.ui.WorkDir != source || p.inits != initialInits {
		t.Fatalf("older validation won race: cmd=%v active=%v work=%q inits=%d", cmd != nil, updated.inGlobalScope(), updated.ui.WorkDir, p.inits)
	}
}

// Clicking a different global tab in the header ends the board's lifecycle the
// same way a key does, and leaves the project underneath untouched.
func TestOverviewHeaderTabClickInvalidatesPendingNavigation(t *testing.T) {
	m, p, source := newOverviewRaceModel(t)
	target := newOverviewGitRepo(t, "target")
	navigation := overviewNavigation(t, m.overview, overviewWorkspace(target))
	m.width, m.height, m.ready = 120, 40, true
	bounds := m.getTabBounds()
	if len(bounds) < 2 || bounds[0].Tab.scope != ScopeGlobal || bounds[0].Tab.global != GlobalSessions {
		t.Fatalf("expected the global tab row in the header: %#v", bounds)
	}
	initialInits := p.inits
	updatedModel, _ := m.Update(tea.MouseClickMsg{X: bounds[0].Start, Y: 0, Button: tea.MouseLeft})
	exited := updatedModel.(Model)
	if !exited.inGlobalScope() || exited.globalTab != GlobalSessions {
		t.Fatalf("tab click left the global space: global=%v tab=%v", exited.inGlobalScope(), exited.globalTab)
	}
	updatedModel, cmd := exited.Update(navigation)
	updated := updatedModel.(Model)
	if cmd != nil || updated.ui.WorkDir != source || p.inits != initialInits {
		t.Fatalf("late navigation mutated after tab click: cmd=%v work=%q inits=%d", cmd != nil, updated.ui.WorkDir, p.inits)
	}
}

func TestOverviewHeaderMouseOpensSwitcher(t *testing.T) {
	cfg := config.Default()
	m := Model{cfg: cfg, registry: plugin.NewRegistry(nil), keymap: keymap.NewRegistry(), ui: &UIState{}, intro: IntroModel{RepoName: "repo", Done: true}, overview: overview.New(workspaceinventory.Collector{Runner: &countingOverviewRunner{}}), width: 120, height: 40, ready: true}
	start, end, ok := m.getProjectSelectorBounds()
	if !ok {
		t.Fatal("project header has no switcher bounds")
	}
	updatedModel, _ := m.Update(tea.MouseClickMsg{X: (start + end) / 2, Y: 0, Button: tea.MouseLeft})
	updated := updatedModel.(Model)
	if !updated.showProjectSwitcher || updated.inGlobalScope() {
		t.Fatalf("project header click: open=%v global=%v", updated.showProjectSwitcher, updated.inGlobalScope())
	}
}

func TestGlobalProjectSelectorOpensSwitcher(t *testing.T) {
	cfg := config.Default()
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })
	cfg.Projects.List = []config.ProjectConfig{{Name: "one", Path: "/tmp/one"}}
	m := New(plugin.NewRegistry(nil), keymap.NewRegistry(), cfg, "", "/tmp/one", "/tmp/one", "")
	if m.overview == nil {
		t.Fatal("overview should be constructed when feature defaults on")
	}
	m.intro.Active, m.intro.Done = false, true
	m.width, m.height, m.ready = 120, 40, true
	m.scope = ScopeGlobal
	m.updateContext()

	start, end, ok := m.getProjectSelectorBounds()
	if !ok || end <= start {
		t.Fatalf("scope bounds = %d-%d ok=%v", start, end, ok)
	}
	updated, _ := m.Update(tea.MouseClickMsg{X: (start + end) / 2, Y: 0, Button: tea.MouseLeft})
	m = asAppModel(t, updated)
	if !m.inGlobalScope() || !m.showProjectSwitcher {
		t.Fatal("global selector click did not open the project switcher in place")
	}
	m.showProjectSwitcher = false
	m.activeContext = "global-workspaces"

	// Logo and K still toggle the scope.
	logoStart, logoEnd, ok := m.getLogoBounds()
	if !ok {
		t.Fatal("logo bounds vanished in project scope")
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: (logoStart + logoEnd) / 2, Y: 0, Button: tea.MouseLeft})
	m = asAppModel(t, updated)
	if m.inGlobalScope() {
		t.Fatal("logo click did not return to project scope")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: "K", Mod: tea.ModShift})
	m = asAppModel(t, updated)
	if !m.inGlobalScope() {
		t.Fatal("K did not re-enter global scope")
	}
}

func newOverviewGitRepo(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("git", "init", "-q", "-b", "main", path).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	// CI runners have no global git identity, so commits here must carry one.
	runOverviewGit(t, path, "config", "user.name", "Sidecar Test")
	runOverviewGit(t, path, "config", "user.email", "sidecar@example.test")
	return path
}

func commitOverviewFixture(t *testing.T, repo string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runOverviewGit(t, repo, "add", "README.md")
	runOverviewGit(t, repo, "-c", "user.name=Sidecar Test", "-c", "user.email=sidecar@example.test", "commit", "-q", "-m", "fixture")
}

func runOverviewGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmdArgs := append([]string{"-C", repo}, args...)
	if out, err := exec.Command("git", cmdArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func newOverviewRaceModel(t *testing.T) (Model, *navigationPlugin, string) {
	t.Helper()
	isolateAppState(t)
	source := newOverviewGitRepo(t, "source")
	cfg := config.Default()
	km := keymap.NewRegistry()
	ctx := &plugin.Context{WorkDir: source, ProjectRoot: source, Config: cfg, Keymap: km}
	reg := plugin.NewRegistry(ctx)
	p := &navigationPlugin{id: "git"}
	if err := reg.Register(p); err != nil {
		t.Fatal(err)
	}
	m := New(reg, km, cfg, "", source, source, "git")
	m.overview = overview.New(workspaceinventory.Collector{Runner: &countingOverviewRunner{}})
	m.scope = ScopeGlobal
	m.globalTab = GlobalActivity
	m.overview.Start(nil)
	return m, p, source
}

func overviewWorkspace(path string) workspaceinventory.Workspace {
	canonical := workspaceinventory.CanonicalPath(path)
	return workspaceinventory.Workspace{ProjectKey: canonical, ProjectRoot: path, Kind: workspaceinventory.KindWorktree, Key: canonical, Path: path}
}

func overviewNavigation(t *testing.T, model *overview.Model, workspace workspaceinventory.Workspace) overview.NavigateMsg {
	t.Helper()
	msg, ok := model.RequestNavigation(workspace)().(overview.NavigateMsg)
	if !ok {
		t.Fatal("request did not return NavigateMsg")
	}
	return msg
}

func overviewValidation(navigation overview.NavigateMsg, err error) overview.ValidationMsg {
	return overview.ValidationMsg{
		Workspace:  navigation.Workspace,
		Action:     navigation.Action,
		Generation: navigation.Generation,
		RequestID:  navigation.RequestID,
		Err:        err,
	}
}

// textInputPlugin mimics a plugin sitting in an interactive/text-input mode
// (an embedded shell, an inline editor) underneath the Overview.
type textInputPlugin struct {
	navigationPlugin
	pastes int
	others int
}

func (p *textInputPlugin) ConsumesTextInput() bool { return true }
func (p *textInputPlugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyPressMsg:
		p.keyInputs++
	case tea.PasteMsg:
		p.pastes++
	case uv.UnknownCsiEvent, uv.UnknownEvent:
		p.others++
	}
	return p, nil
}

func overviewModelOverTextInput(t *testing.T) (Model, *textInputPlugin) {
	t.Helper()
	isolateAppState(t)
	cfg := config.Default()
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })
	cfg.Projects.List = []config.ProjectConfig{{Name: "one", Path: "/tmp/one"}}
	shell := &textInputPlugin{navigationPlugin: navigationPlugin{id: "workspaces"}}
	registry := plugin.NewRegistry(nil)
	if err := registry.Register(shell); err != nil {
		t.Fatal(err)
	}
	m := New(registry, keymap.NewRegistry(), cfg, "", "/tmp/one", "/tmp/one", "workspaces")
	m.overview = overview.New(workspaceinventory.Collector{Runner: &countingOverviewRunner{}})
	m.intro.Active, m.intro.Done = false, true
	m.width, m.height, m.ready = 120, 40, true
	m.scope = ScopeGlobal
	m.globalTab = GlobalActivity
	m.updateContext()
	return m, shell
}

func TestOverviewExitKeysWorkOverInteractivePlugin(t *testing.T) {
	keys := map[string]tea.KeyPressMsg{
		"esc": {Code: tea.KeyEsc},
		"K":   {Code: 'k', Text: "K", Mod: tea.ModShift},
	}
	for name, key := range keys {
		t.Run(name, func(t *testing.T) {
			m, shell := overviewModelOverTextInput(t)
			updated, _ := m.Update(key)
			m = asAppModel(t, updated)
			if m.inGlobalScope() {
				t.Fatalf("%s did not close the overview", name)
			}
			if m.showQuitConfirm {
				t.Fatalf("%s opened the quit dialog instead of closing the overview", name)
			}
			if shell.keyInputs != 0 {
				t.Fatalf("%s leaked to the covered plugin (%d key inputs)", name, shell.keyInputs)
			}
		})
	}
}

func TestQOpensQuitModalFromTheAgentsBoard(t *testing.T) {
	m, shell := overviewModelOverTextInput(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = asAppModel(t, updated)
	if !m.inGlobalScope() {
		t.Fatal("q left the global space instead of opening the quit modal")
	}
	if !m.showQuitConfirm {
		t.Fatal("q did not open the quit modal from the Agents board")
	}
	if shell.keyInputs != 0 {
		t.Fatalf("q leaked to the covered plugin (%d key inputs)", shell.keyInputs)
	}
}

func TestOverviewGlobalShortcutsWorkOverInteractivePlugin(t *testing.T) {
	t.Run("project-switcher", func(t *testing.T) {
		m, shell := overviewModelOverTextInput(t)
		updated, _ := m.Update(tea.KeyPressMsg{Code: '@', Text: "@", Mod: tea.ModShift})
		m = asAppModel(t, updated)
		if !m.showProjectSwitcher {
			t.Fatal("@ did not open the project switcher")
		}
		if shell.keyInputs != 0 {
			t.Fatalf("@ leaked to the covered plugin (%d key inputs)", shell.keyInputs)
		}
	})
	t.Run("theme-switcher", func(t *testing.T) {
		m, _ := overviewModelOverTextInput(t)
		updated, _ := m.Update(tea.KeyPressMsg{Code: '#', Text: "#", Mod: tea.ModShift})
		m = asAppModel(t, updated)
		if !m.showThemeSwitcher {
			t.Fatal("# did not open the theme switcher")
		}
	})
	t.Run("palette", func(t *testing.T) {
		m, _ := overviewModelOverTextInput(t)
		updated, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?", Mod: tea.ModShift})
		m = asAppModel(t, updated)
		if !m.showPalette {
			t.Fatal("? did not open the command palette")
		}
	})
	t.Run("8 and 9 move between global tabs", func(t *testing.T) {
		m, shell := overviewModelOverTextInput(t)
		updated, _ := m.Update(tea.KeyPressMsg{Code: '9', Text: "9"})
		m = asAppModel(t, updated)
		if !m.inGlobalScope() || m.globalTab != GlobalActivity {
			t.Fatalf("9 left the global space: global=%v tab=%v", m.inGlobalScope(), m.globalTab)
		}
		updated, _ = m.Update(tea.KeyPressMsg{Code: '8', Text: "8"})
		m = asAppModel(t, updated)
		if !m.inGlobalScope() || m.globalTab != GlobalSessions {
			t.Fatalf("8 left the global space: global=%v tab=%v", m.inGlobalScope(), m.globalTab)
		}
		if shell.keyInputs != 0 {
			t.Fatalf("global tab numbers leaked to the covered plugin (%d key inputs)", shell.keyInputs)
		}
	})
}

func TestOverviewSwallowsUnhandledKeys(t *testing.T) {
	m, shell := overviewModelOverTextInput(t)
	for _, key := range []tea.KeyPressMsg{
		{Code: 'x', Text: "x"},
		{Code: 's', Text: "s"},
		{Code: tea.KeyTab},
	} {
		updated, _ := m.Update(key)
		m = asAppModel(t, updated)
	}
	if shell.keyInputs != 0 {
		t.Fatalf("overview leaked %d keys to the covered plugin", shell.keyInputs)
	}
	if !m.inGlobalScope() {
		t.Fatal("unhandled keys should not close the overview")
	}
}

func TestOverviewSwallowsPasteAndUnknownSequences(t *testing.T) {
	m, shell := overviewModelOverTextInput(t)

	updated, _ := m.Update(tea.PasteMsg{Content: "rm -rf /\n"})
	m = asAppModel(t, updated)
	updated, _ = m.Update(uv.UnknownCsiEvent("\x1b[13;2u"))
	m = asAppModel(t, updated)

	if shell.pastes != 0 {
		t.Fatalf("paste leaked to the covered plugin (%d pastes)", shell.pastes)
	}
	if shell.others != 0 {
		t.Fatalf("unknown CSI sequence leaked to the covered plugin (%d messages)", shell.others)
	}
	if !m.inGlobalScope() {
		t.Fatal("paste/sequence handling should not close the overview")
	}
}

func TestExitOverviewRestoresPluginContext(t *testing.T) {
	m, _ := overviewModelOverTextInput(t)
	if m.activeContext != "overview" {
		t.Fatalf("activeContext = %q, want overview", m.activeContext)
	}
	// A number with no global tab behind it does nothing at all: it must not
	// fall through to the project's plugin list.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '9', Text: "9"})
	m = asAppModel(t, updated)
	if !m.inGlobalScope() || m.activeContext != "overview" {
		t.Fatalf("out-of-range number left the global space: global=%v context=%q", m.inGlobalScope(), m.activeContext)
	}
	// Leaving the space restores the covered plugin's own context.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = asAppModel(t, updated)
	if m.inGlobalScope() {
		t.Fatal("esc should leave the global space")
	}
	if m.activeContext == "overview" {
		t.Fatal("activeContext still overview after the board closed")
	}
}

// The chord that leaves interactive mode is the user's configured one on both
// surfaces, and it is registered so help and the palette name the key the
// browser actually answers.
func TestConfiguredInteractiveExitKeyReachesTheWorkspacesBrowser(t *testing.T) {
	cfg := config.Default()
	cfg.Features.Flags[features.CrossProjectOverview.Name] = true
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })
	cfg.Plugins.Workspace.InteractiveExitKey = "ctrl+q"

	km := keymap.NewRegistry()
	m := New(plugin.NewRegistry(nil), km, cfg, "", "/tmp/one", "/tmp/one", "")

	if got := m.overview.InteractiveExitKey(); got != "ctrl+q" {
		t.Fatalf("browser exit key = %q, want the configured chord", got)
	}
	var bound []string
	for _, binding := range km.BindingsForContext("global-workspaces-terminal") {
		if binding.Command == "exit-interactive" {
			bound = append(bound, binding.Key)
		}
	}
	if len(bound) != 1 || bound[0] != "ctrl+q" {
		t.Fatalf("exit-interactive is bound to %v, want only the configured chord", bound)
	}
}

// The two surfaces that host a terminal resolve one configuration, so the chords
// they answer for the same act cannot drift apart. Everything below reads the
// same TerminalConfig the project Workspaces plugin reads, which it calls
// directly from p.terminalConfig.
func TestBothTerminalSurfacesAnswerOneResolvedConfiguration(t *testing.T) {
	cfg := config.Default()
	cfg.Features.Flags[features.CrossProjectOverview.Name] = true
	cfg.Features.Flags[features.TmuxFullAttach.Name] = true
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })
	cfg.Plugins.Workspace.InteractiveExitKey = "ctrl+q"
	cfg.Plugins.Workspace.InteractiveCopyKey = "alt+y"
	cfg.Plugins.Workspace.InteractivePasteKey = "alt+p"
	cfg.Plugins.Workspace.InteractiveAttachKey = "ctrl+g"
	cfg.Plugins.Workspace.CopyOnSelect = true

	shared := TerminalConfig(cfg)
	m := New(plugin.NewRegistry(nil), keymap.NewRegistry(), cfg, "", "/tmp/one", "/tmp/one", "")
	browser := m.overview.TerminalConfig()

	for _, tc := range []struct{ act, want, got string }{
		{"exit", shared.ExitKey, browser.ExitKey},
		{"copy", shared.CopyKey, browser.CopyKey},
		{"paste", shared.PasteKey, browser.PasteKey},
	} {
		if tc.got != tc.want {
			t.Errorf("the browser answers %q for %s, want the resolved %q", tc.got, tc.act, tc.want)
		}
	}
	if !browser.CopyOnSelect {
		t.Error("copy-on-select is configured but the browser does not honour it")
	}
	// The one act the two surfaces deliberately differ on, and the reason: the
	// browser has no attach path, so the chord stays the pane's input.
	if browser.AttachKey != "" || shared.AttachKey != "ctrl+g" {
		t.Errorf("attach chords = browser %q, plugin %q", browser.AttachKey, shared.AttachKey)
	}
}
