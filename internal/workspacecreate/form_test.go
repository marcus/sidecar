package workspacecreate

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/agentcatalog"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/workspaceops"
)

func TestMain(m *testing.M) {
	loadLastCreateAgent = func() string { return "" }
	saveLastCreateAgent = func(string) error { return nil }
	loadAgentAutoApprove = func(string) bool { return false }
	saveAgentAutoApprove = func(string, bool) error { return nil }
	os.Exit(m.Run())
}

type memState struct {
	last      string
	auto      map[string]bool
	savedLast []string
}

func useMemState(t *testing.T, s *memState) {
	t.Helper()
	if s.auto == nil {
		s.auto = map[string]bool{}
	}
	loadLastCreateAgent = func() string { return s.last }
	saveLastCreateAgent = func(a string) error {
		s.last = a
		s.savedLast = append(s.savedLast, a)
		return nil
	}
	loadAgentAutoApprove = func(a string) bool { return s.auto[a] }
	saveAgentAutoApprove = func(a string, on bool) error {
		s.auto[a] = on
		return nil
	}
	t.Cleanup(func() {
		loadLastCreateAgent = func() string { return "" }
		saveLastCreateAgent = func(string) error { return nil }
		loadAgentAutoApprove = func(string) bool { return false }
		saveAgentAutoApprove = func(string, bool) error { return nil }
	})
}

func testOpts(kind Kind) OpenOpts {
	return OpenOpts{
		Kind:        kind,
		ShowProject: true,
		ProjectKey:  "one",
		Projects: []ProjectItem{
			{Key: "one", Label: "one"},
			{Key: "two", Label: "two"},
		},
		NextShell: "Shell 3",
		Branches:  []string{"main", "dev"},
	}
}

func renderForm(t *testing.T, f *Form) string {
	t.Helper()
	m := f.Build(52)
	if m == nil {
		t.Fatal("Build returned nil")
	}
	view := m.Render(80, 40, mouse.NewHandler())
	return view
}

func focusable(t *testing.T, f *Form, id string) bool {
	t.Helper()
	m := f.Build(52)
	m.Render(80, 40, mouse.NewHandler())
	before := m.FocusedID()
	m.SetFocus(id)
	ok := m.FocusedID() == id
	if before != "" {
		m.SetFocus(before)
	}
	return ok
}

func TestKindSwitchFieldVisibility(t *testing.T) {
	f := Open(testOpts(KindWorktree))
	view := renderForm(t, f)
	if !strings.Contains(view, "Base Branch") {
		t.Fatalf("worktree form missing Base Branch:\n%s", view)
	}
	if !strings.Contains(view, "Create Workspace") {
		t.Fatalf("missing title:\n%s", view)
	}
	if !focusable(t, f, FieldBase) {
		t.Fatal("worktree Base Branch should be focusable")
	}

	f.SetKind(KindShell)
	view = renderForm(t, f)
	if strings.Contains(view, "Base Branch") {
		t.Fatalf("shell form still shows Base Branch:\n%s", view)
	}
	if focusable(t, f, FieldBase) {
		t.Fatal("shell Base Branch should not be focusable")
	}
	if f.nameInput.Placeholder != "Shell 3" {
		t.Fatalf("shell placeholder = %q, want Shell 3", f.nameInput.Placeholder)
	}

	f.SetKind(KindWorktree)
	if f.nameInput.Placeholder != worktreeNamePlaceholder {
		t.Fatalf("worktree placeholder = %q", f.nameInput.Placeholder)
	}
	view = renderForm(t, f)
	if !strings.Contains(view, "Base Branch") {
		t.Fatalf("worktree after toggle missing Base Branch:\n%s", view)
	}
}

func TestKindSwitchKeepsNameAndAgent(t *testing.T) {
	st := &memState{last: "codex"}
	useMemState(t, st)
	f := Open(testOpts(KindWorktree))
	f.nameInput.SetValue("my-feature")
	if f.Agent() != "codex" {
		t.Fatalf("agent = %q, want codex", f.Agent())
	}
	f.SetKind(KindShell)
	if f.Name() != "my-feature" {
		t.Fatalf("name dropped on kind switch: %q", f.Name())
	}
	if f.Agent() != "codex" {
		t.Fatalf("agent dropped on kind switch: %q", f.Agent())
	}
}

func TestNoneOrderByKind(t *testing.T) {
	shell := Open(testOpts(KindShell)).AgentItems()
	if len(shell) == 0 || shell[0].Data != "" {
		t.Fatalf("shell None first: %+v", shell)
	}
	worktree := Open(testOpts(KindWorktree)).AgentItems()
	if len(worktree) == 0 || worktree[len(worktree)-1].Data != "" {
		t.Fatalf("worktree None last: %+v", worktree)
	}

	f := Open(testOpts(KindWorktree))
	ids := make([]string, len(f.AgentItems()))
	for i, item := range f.AgentItems() {
		id, _ := item.Data.(string)
		ids[i] = id
	}
	want := agentcatalog.ResolvePicker(nil, false)
	if len(ids) != len(want) {
		t.Fatalf("worktree picker %v, catalog %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("worktree picker %v, catalog %v", ids, want)
		}
	}
}

func TestSkipCheckboxShownHidden(t *testing.T) {
	st := &memState{last: "claude"}
	useMemState(t, st)
	f := Open(testOpts(KindWorktree))
	view := renderForm(t, f)
	if !f.ShowSkip() {
		t.Fatal("claude should show skip")
	}
	if !strings.Contains(view, "Auto-approve all actions") {
		t.Fatalf("missing auto-approve:\n%s", view)
	}
	if !strings.Contains(view, workspaceops.AgentSkipFlag("claude")) {
		t.Fatalf("missing skip flag hint:\n%s", view)
	}
	if !focusable(t, f, FieldSkip) {
		t.Fatal("skip checkbox should be focusable for claude")
	}

	f.agentIndex = indexOfString(f.agentIDs(), "")
	f.SyncAfterInput()
	view = renderForm(t, f)
	if f.ShowSkip() {
		t.Fatal("None should hide skip")
	}
	if strings.Contains(view, "Auto-approve all actions") {
		t.Fatalf("None still shows auto-approve:\n%s", view)
	}
	if focusable(t, f, FieldSkip) {
		t.Fatal("skip checkbox should not be focusable for None")
	}

	f.agentIndex = indexOfString(f.agentIDs(), "copilot")
	f.SyncAfterInput()
	if f.ShowSkip() {
		t.Fatal("copilot has no skip flag")
	}
}

func TestNameRequiredVsOptional(t *testing.T) {
	worktree := Open(testOpts(KindWorktree))
	if got := worktree.Validate(); got != "Name is required" {
		t.Fatalf("empty worktree name: %q", got)
	}
	worktree.nameInput.SetValue("   ")
	if got := worktree.Validate(); got != "Name is required" {
		t.Fatalf("blank worktree name: %q", got)
	}
	worktree.nameInput.SetValue("???")
	if got := worktree.Validate(); got != "Name does not produce a valid git branch" {
		t.Fatalf("unslugable worktree name: %q", got)
	}
	worktree.nameInput.SetValue("Auth Refresh")
	if got := worktree.Validate(); got != "" {
		t.Fatalf("valid worktree name: %q", got)
	}

	shell := Open(testOpts(KindShell))
	if got := shell.Validate(); got != "" {
		t.Fatalf("empty shell name should be optional: %q", got)
	}
	shell.nameInput.SetValue("custom")
	if got := shell.Validate(); got != "" {
		t.Fatalf("named shell: %q", got)
	}
}

func TestLastAgentPrefillAndNoPersistOnOpen(t *testing.T) {
	st := &memState{last: "codex", auto: map[string]bool{"codex": true}}
	useMemState(t, st)
	f := Open(testOpts(KindWorktree))
	if f.Agent() != "codex" {
		t.Fatalf("agent = %q, want codex", f.Agent())
	}
	if !f.SkipPerms() {
		t.Fatal("expected persisted auto-approve for last agent")
	}
	if len(st.savedLast) != 0 {
		t.Fatalf("Open persisted last agent: %v", st.savedLast)
	}
	f.PersistLastAgent()
	if len(st.savedLast) != 1 || st.savedLast[0] != "codex" {
		t.Fatalf("PersistLastAgent = %v", st.savedLast)
	}
}

func TestLastAgentFallbackChain(t *testing.T) {
	st := &memState{last: "claude"}
	useMemState(t, st)

	f := Open(OpenOpts{Kind: KindWorktree, Agents: []string{"grok", "codex"}, PreferredAgent: "codex"})
	if f.Agent() != "codex" {
		t.Fatalf("preferred fallback = %q, want codex", f.Agent())
	}

	f = Open(OpenOpts{Kind: KindWorktree, Agents: []string{"grok", "codex"}, DefaultAgent: "codex"})
	if f.Agent() != "codex" {
		t.Fatalf("default fallback = %q, want codex", f.Agent())
	}

	f = Open(OpenOpts{Kind: KindWorktree, Agents: []string{"grok", "codex"}})
	if f.Agent() != "grok" {
		t.Fatalf("first-real fallback = %q, want grok", f.Agent())
	}

	f = Open(OpenOpts{Kind: KindShell, Agents: []string{"grok", "codex"}})
	if f.Agent() != "" {
		t.Fatalf("shell fallback = %q, want None", f.Agent())
	}
}

func TestAutoApproveLoadOnAgentChangeAndPersistOnToggle(t *testing.T) {
	st := &memState{
		last: "claude",
		auto: map[string]bool{"claude": false, "codex": true},
	}
	useMemState(t, st)
	f := Open(testOpts(KindWorktree))
	if f.SkipPerms() {
		t.Fatal("claude auto-approve should start false")
	}

	f.agentIndex = indexOfString(f.agentIDs(), "codex")
	f.SyncAfterInput()
	if f.Agent() != "codex" {
		t.Fatalf("agent = %q after change", f.Agent())
	}
	if !f.SkipPerms() {
		t.Fatal("expected codex auto-approve after agent change")
	}

	f.skip = false
	f.SyncAfterInput()
	if st.auto["codex"] {
		t.Fatal("toggle off should persist immediately")
	}

	f.skip = true
	f.SyncAfterInput()
	if !st.auto["codex"] {
		t.Fatal("toggle on should persist immediately")
	}
}

func TestFocusKindVsFocusName(t *testing.T) {
	name := Open(testOpts(KindWorktree))
	if name.InitialFocusID() != FieldName {
		t.Fatalf("default focus = %q, want %s", name.InitialFocusID(), FieldName)
	}
	renderForm(t, name)
	if name.Modal().FocusedID() != FieldName {
		t.Fatalf("rendered default focus = %q, want %s", name.Modal().FocusedID(), FieldName)
	}

	kind := Open(OpenOpts{Kind: KindWorktree, FocusKind: true, ShowProject: true, Projects: testOpts(KindWorktree).Projects})
	if kind.InitialFocusID() != FieldKind {
		t.Fatalf("FocusKind = %q, want %s", kind.InitialFocusID(), FieldKind)
	}
	renderForm(t, kind)
	if kind.Modal().FocusedID() != FieldKind {
		t.Fatalf("rendered FocusKind = %q, want %s", kind.Modal().FocusedID(), FieldKind)
	}
}

func TestShowProjectHidesProjectCombo(t *testing.T) {
	shown := Open(testOpts(KindWorktree))
	view := renderForm(t, shown)
	if !strings.Contains(view, "Project") {
		t.Fatalf("ShowProject true missing Project:\n%s", view)
	}
	if !focusable(t, shown, FieldProject) {
		t.Fatal("project combo should be focusable when shown")
	}

	hidden := Open(OpenOpts{Kind: KindWorktree, ShowProject: false, ProjectKey: "one"})
	if focusable(t, hidden, FieldProject) {
		t.Fatal("project combo should not be focusable when hidden")
	}
	if hidden.ProjectKey() != "one" {
		t.Fatalf("hidden project still keeps key = %q", hidden.ProjectKey())
	}
}

func TestSetBranchesPrefillsCurrentWithoutClobberingTypedValue(t *testing.T) {
	f := Open(testOpts(KindWorktree))
	f.SetBranches([]string{"main", "dev"}, "main")
	if f.BaseBranch() != "main" {
		t.Fatalf("prefill current = %q, want main", f.BaseBranch())
	}

	f.SetBranches([]string{"main", "dev", "feat"}, "feat")
	if f.BaseBranch() != "main" {
		t.Fatalf("typed value still in list was clobbered: %q", f.BaseBranch())
	}

	f.SetBranches([]string{"dev", "feat"}, "feat")
	if f.BaseBranch() != "feat" {
		t.Fatalf("typed value gone should reset to current = %q, want feat", f.BaseBranch())
	}

	// An empty list clears everything — list and prefill. This is how a form
	// whose project switched to a remote target sheds the LOCAL repository's
	// branches: keeping either would offer a base another machine resolves
	// against a different history.
	f.SetBranches(nil, "")
	if f.BaseBranch() != "" {
		t.Fatalf("clearing the list kept base = %q", f.BaseBranch())
	}
	if len(f.branches) != 0 {
		t.Fatalf("clearing the list kept branches = %v", f.branches)
	}
}

func TestComboDoesNotOverwriteFocusedFilterOnRebuild(t *testing.T) {
	st := &memState{last: "claude"}
	useMemState(t, st)
	f := Open(testOpts(KindWorktree))
	m := f.Build(52)
	m.Render(80, 40, mouse.NewHandler())
	m.SetFocus(FieldAgent)
	f.pendingFocus = FieldAgent
	f.agentInput.SetValue("c")

	f.SetKind(KindShell)
	_ = f.Build(52)
	if got := f.agentInput.Value(); got != "c" {
		t.Fatalf("agent combo query = %q, want c (prefill must not overwrite incremental search)", got)
	}
}

// clickKindRow clicks row idx of the kind chooser exactly as a host does:
// render, find the row's hit region, hand the modal a click on it. Nothing
// between the pointer and the control maps the click — the control resolves it.
func clickKindRow(t *testing.T, f *Form, width, idx int) string {
	t.Helper()
	m := f.Build(width)
	if m == nil {
		t.Fatal("Build returned nil")
	}
	handler := mouse.NewHandler()
	m.Render(100, 44, handler)
	id := kindItemID(FieldKind, idx)
	for _, region := range handler.HitMap.Regions() {
		if region.ID != id {
			continue
		}
		return m.HandleMouse(tea.MouseClickMsg{
			X:      region.Rect.X + 1,
			Y:      region.Rect.Y,
			Button: tea.MouseLeft,
		}, handler)
	}
	t.Fatalf("kind row %d registered no hit region", idx)
	return ""
}

// A click lands on the row it was over, with no host glue in between, and
// reports the control rather than a row ID a host has no branch for. A
// Workspaces catalog is five rows, which draws as the vertical list.
func TestKindClickPicksTheRow(t *testing.T) {
	f := Open(testOpts(KindShell))
	if action := clickKindRow(t, f, 52, 1); action != FieldKind {
		t.Fatalf("clicking a kind row returned %q, want %s", action, FieldKind)
	}
	if f.Kind() != KindWorktree {
		t.Fatalf("click on Worktree = %v, want Worktree", f.Kind())
	}
	if action := clickKindRow(t, f, 52, 0); action != FieldKind {
		t.Fatalf("clicking back returned %q", action)
	}
	if f.Kind() != KindShell {
		t.Fatalf("click on Shell = %v, want Shell", f.Kind())
	}
	// The click also leaves focus on the control, so the arrows that follow
	// steer what the pointer just used.
	if got := f.Build(52).FocusedID(); got != FieldKind {
		t.Fatalf("focus after a kind click = %q, want %s", got, FieldKind)
	}
}

// The pane-only catalog is short enough to draw as the segmented toggle, which
// is the shape a plugin host's Open Pane modal wears. It is a separate case
// from the list because the two shapes register their hit regions differently:
// one region per row against one region per column span, and only this one can
// put two choices on the same screen line.
func TestKindClickPicksTheSegmentInThePaneCatalog(t *testing.T) {
	opts := testOpts(KindFile)
	opts.PaneKindsOnly = true

	shape := Open(opts)
	if len(shape.rows) >= 5 {
		t.Fatalf("the pane catalog has %d rows, which no longer draws segmented", len(shape.rows))
	}
	view := ansi.Strip(shape.Build(70).Render(100, 44, mouse.NewHandler()))
	if !strings.Contains(view, "File") || !strings.Contains(view, "|") || strings.Contains(view, "❯") {
		t.Fatalf("the pane catalog did not draw as a segmented toggle:\n%s", view)
	}

	for i, row := range shape.rows {
		click := Open(opts)
		if action := clickKindRow(t, click, 70, i); action != FieldKind {
			t.Fatalf("clicking segment %d returned %q, want %s", i, action, FieldKind)
		}
		if click.Kind() != row.Kind {
			t.Fatalf("click on segment %d = %v, want %v", i, click.Kind(), row.Kind)
		}
	}
}

func TestKindToggleKeys(t *testing.T) {
	f := Open(OpenOpts{Kind: KindWorktree, FocusKind: true})
	m := f.Build(52)
	m.Render(80, 40, mouse.NewHandler())
	m.HandleKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if f.Kind() != KindShell {
		t.Fatalf("left on kind = %v, want Shell", f.Kind())
	}
	view := renderForm(t, f)
	if strings.Contains(view, "Base Branch") {
		t.Fatalf("after left, still shows Base Branch:\n%s", view)
	}
}

func TestTabAcrossFieldsWithRenders(t *testing.T) {
	f := Open(testOpts(KindWorktree))
	m := f.Build(52)
	_ = m.Render(80, 40, mouse.NewHandler())
	if m.FocusedID() != FieldName {
		t.Fatalf("initial focus = %q, want %s", m.FocusedID(), FieldName)
	}

	wantOrder := []string{
		FieldBase,
		FieldAgent,
		FieldSkip,
		ActionCreate,
		ActionCancel,
		FieldKind,
		FieldProject,
		FieldName,
	}

	for i, want := range wantOrder {
		m.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
		// Simulate frame render between keypresses
		_ = f.Build(52).Render(80, 40, mouse.NewHandler())
		if got := m.FocusedID(); got != want {
			t.Fatalf("after tab %d, focus = %q, want %s", i+1, got, want)
		}
	}
}

func TestSlugHintWhenDisplayDiffers(t *testing.T) {
	f := Open(testOpts(KindWorktree))
	f.nameInput.SetValue("auth-refresh")
	view := renderForm(t, f)
	if strings.Contains(view, "git: ") {
		t.Fatalf("slug hint shown when slug equals name:\n%s", view)
	}
	f.nameInput.SetValue("Auth Refresh")
	view = renderForm(t, f)
	if !strings.Contains(view, "git: auth-refresh") {
		t.Fatalf("expected slug hint:\n%s", view)
	}
	f.SetKind(KindShell)
	view = renderForm(t, f)
	if strings.Contains(view, "git: ") {
		t.Fatalf("slug hint on shell:\n%s", view)
	}
}

func TestErrorSection(t *testing.T) {
	f := Open(testOpts(KindWorktree))
	f.SetError("Name is required")
	view := renderForm(t, f)
	if !strings.Contains(view, "Error: Name is required") {
		t.Fatalf("missing error:\n%s", view)
	}
}

// TestErrorSectionWrapsRatherThanTruncates. A remote failure's message is two
// halves — the host's own sentence, then what to do about it — and the modal is
// narrower than either. Rendered as one unwrapped line the tail was cut, which
// always meant the actionable half: the live two-machine proof saw "Error: the
// remote Sidecar did not accept this…" and nothing else.
func TestErrorSectionWrapsRatherThanTruncates(t *testing.T) {
	f := Open(testOpts(KindWorktree))
	message := `branch "phase-c-wt" already exists — pick another name, or delete that branch on the host`
	f.SetError(message)
	view := renderForm(t, f)
	// Word by word, because wrapping moves the line breaks around: what must
	// not happen is a word going missing.
	for _, word := range strings.Fields(message) {
		if !strings.Contains(view, word) {
			t.Fatalf("the error was truncated before %q:\n%s", word, view)
		}
	}
}
