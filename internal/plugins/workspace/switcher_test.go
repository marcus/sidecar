package workspace

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspaceops"
)

// switcherPlugin is a pane-placing workspace with one selected shell, ready to
// open panes beside it.
func switcherPlugin(t *testing.T) *Plugin {
	t.Helper()
	root := t.TempDir()
	writeDocPaneFixture(t, root, "docs/plan.md", "# plan\n")
	p := docPaneTestPlugin(t, root, true)
	return p
}

// o is the preview-context entry into the pane switcher: the grown create
// modal, kind list focused, without moving focus to the sidebar.
func TestOPreviewOpensPaneSwitcherKindFocused(t *testing.T) {
	p := switcherPlugin(t)
	t.Cleanup(p.stopTerminalModels)
	p.activePane = PanePreview

	// The footer hint is advertised while the preview owns the keys — before
	// the modal opens and the context becomes workspace-create.
	if !commandNamedContext(p, "open-pane", "workspace-preview") {
		t.Fatalf("workspace-preview Commands() omitted open-pane: %#v", p.Commands())
	}

	cmd := p.handleListKeys(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if p.viewMode != ViewModeCreate || p.createForm == nil {
		t.Fatal("o did not open the create form")
	}
	if p.createForm.InitialFocusID() != workspacecreate.FieldKind {
		t.Fatalf("initial focus = %q, want the kind list", p.createForm.InitialFocusID())
	}
	if cmd == nil {
		t.Fatal("o returned no load command")
	}
}

func commandNamedContext(p *Plugin, id, context string) bool {
	for _, cmd := range p.Commands() {
		if cmd.ID == id && cmd.Context == context {
			return true
		}
	}
	return false
}

// The project surface's form carries every row its config enables: notes when
// Notes is registered, one resource row per enabled provider.
func TestWorkspaceSwitcherOptsCarryConfig(t *testing.T) {
	p := New()
	p.ctx = &plugin.Context{
		WorkDir: t.TempDir(), ProjectRoot: t.TempDir(),
		Config: &config.Config{
			Plugins: config.PluginsConfig{TDMonitor: config.TDMonitorPluginConfig{Enabled: true}},
			TerminalResources: config.TerminalResourcesConfig{
				Providers: []config.TerminalResourceProviderConfig{
					{ID: "jira-work", Enabled: true}, {ID: "github-broken", Enabled: false},
				},
			},
		},
	}
	opts := p.createOpenOpts(workspacecreate.KindShell, false, "")
	if opts.AllowTerminalSplit != terminalPanelEnabled() {
		t.Fatal("AllowTerminalSplit drifted from terminalPanelEnabled")
	}
	if !opts.ShowNotes {
		t.Fatal("ShowNotes false while Notes is on by default")
	}
	if len(opts.Providers) != 1 || opts.Providers[0].ID != "jira-work" {
		t.Fatalf("providers = %+v, want only the enabled instance", opts.Providers)
	}
	if features.IsEnabled(features.NotesPlugin.Name) && !p.notesPluginPresent() {
		t.Fatal("notes presence predicate disagrees with the feature default")
	}
}

// The File picker's whole journey from inside the modal: Enter advances,
// typing filters, and submitting opens the document pane through the same
// per-surface path `sidecar open` uses — here proved by the pane tree.
func TestFilePickerSubmitOpensDocPaneForSurface(t *testing.T) {
	p := switcherPlugin(t)
	t.Cleanup(p.stopTerminalModels)

	p.openCreate(workspacecreate.KindShell, true, "")
	p.createForm.SetKind(workspacecreate.KindFile)
	p.ensureCreateModal() // build the modal before keys reach it
	action, _ := p.createForm.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action != "" || p.createForm.Step() != workspacecreate.StepTarget {
		t.Fatalf("Enter on File: action=%q step=%d, want advance to picker", action, p.createForm.Step())
	}

	p.applyCreateFileCandidates(workspacecreate.FilesScannedMsg{
		Root: p.ctx.WorkDir, Paths: []string{"README.md", "docs/plan.md"},
	})
	p.createForm.PickerInput().SetValue("plan")
	p.createForm.SyncAfterInput()

	cmd := p.submitCreateForm()
	if cmd == nil {
		t.Fatal("submit produced no command")
	}
	p.Update(cmd())
	if p.contentDeck == nil {
		t.Fatal("submitting the File picker opened no content deck")
	}
	if leaf := p.contentDeck.Leaf(panelayout.Document); leaf == 0 {
		t.Fatal("no document leaf in the pane tree after submit")
	}
}

// A placement click on the picker step creates right now with that placement.
func TestPickerPlacementClickSubmitsWithAxis(t *testing.T) {
	p := switcherPlugin(t)
	t.Cleanup(p.stopTerminalModels)

	p.openCreate(workspacecreate.KindFile, true, "")
	p.createForm.AdvanceToTarget()
	p.createForm.SetIssues(nil)
	p.createForm.SetFileCandidates([]string{"docs/plan.md"})
	p.createForm.PickerInput().SetValue("docs/plan.md")
	p.createForm.SyncAfterInput()

	if got := p.createForm.ApplyPlacementActionStep(workspacecreate.ActionPlaceBelow); got != workspacecreate.PlacementSubmitted {
		t.Fatalf("placement click = %v, want submit", got)
	}
	cmd := p.submitCreateForm()
	if cmd == nil {
		t.Fatal("placement submit produced no command")
	}
	p.Update(cmd())
	if p.contentDeck == nil {
		t.Fatal("placement click opened nothing")
	}
	// Below means the split axis was rows: the deck tree must hold a Rows
	// split between the terminal and the new document leaf.
	sawRows := false
	var walk func(n *PaneNode)
	walk = func(n *PaneNode) {
		if n == nil || n.Split == nil {
			return
		}
		if n.Split.Axis == SplitRows {
			sawRows = true
		}
		walk(n.Split.A)
		walk(n.Split.B)
	}
	walk(p.paneRoot)
	if !sawRows {
		t.Fatal("Below placement did not produce a rows split")
	}
}

// The project surface folds loader results through the same shared folds as
// the Sessions browser. This drives THIS host's real data paths —
// applyCreatePickerData and applyCreateFileCandidates — over what the loaders
// actually return, so a fold that drifts on one surface only fails here.
func TestWorkspaceHostPickerDataResolvesLikeTheCLI(t *testing.T) {
	dir := initTwoCommitRepo(t)
	refs, err := workspaceops.RecentDiffRefs(context.Background(), dir, 15)
	if err != nil {
		t.Fatalf("RecentDiffRefs: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("fixture repo yielded no refs")
	}

	p := &Plugin{ctx: &plugin.Context{WorkDir: dir}}
	p.createForm = workspacecreate.Open(workspacecreate.OpenOpts{AllowTerminalSplit: true, ShowNotes: true})
	p.applyCreatePickerData(createPickerDataMsg{
		Refs:   refs,
		Issues: []workspaceops.IssueRef{{ID: "td-756c34", Title: "fix(palette): scrollbar", Status: "in_progress"}},
		Notes:  []workspaceops.NoteRef{{ID: "nt-4jdj4e", Title: "scratch"}},
	})

	// Diff: the ref row must resolve by identity — RecentDiffRefs embeds the
	// hash in Label, which must never leak into Value.
	p.createForm.SetKind(workspacecreate.KindDiff)
	p.createForm.AdvanceToTarget()
	p.createForm.PickerInput().SetValue(refs[0].Identity)
	p.createForm.SyncAfterInput()
	got, err := p.createForm.TargetFor(dir)
	if err != nil {
		t.Fatalf("diff TargetFor: %v", err)
	}
	want, err := uirequest.ResolveDiffSpec(dir, refs[0].Identity)
	if err != nil {
		t.Fatalf("CLI ResolveDiffSpec: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("workspace diff target = %+v, want the CLI's %+v", got, want)
	}

	// Note: same form, same folds, id resolves as open would classify it.
	p.createForm.SetKind(workspacecreate.KindNote)
	p.createForm.AdvanceToTarget()
	p.createForm.PickerInput().SetValue("nt-4jdj4e")
	p.createForm.SyncAfterInput()
	got, err = p.createForm.TargetFor(dir)
	if err != nil {
		t.Fatalf("note TargetFor: %v", err)
	}
	if got.Kind != uirequest.TargetKindNote || got.Value != "nt-4jdj4e" {
		t.Fatalf("workspace note target = %+v", got)
	}

	// File: this host's recents-first candidate fold feeds the picker.
	p.createForm.SetKind(workspacecreate.KindFile)
	p.createForm.AdvanceToTarget()
	p.applyCreateFileCandidates(workspacecreate.FilesScannedMsg{Root: dir, Paths: []string{"a.go"}})
	p.createForm.PickerInput().SetValue("a.go")
	p.createForm.SyncAfterInput()
	got, err = p.createForm.TargetFor(dir)
	if err != nil {
		t.Fatalf("file TargetFor: %v", err)
	}
	wantFile, err := uirequest.ResolveFileTarget(dir, "a.go", 0)
	if err != nil {
		t.Fatalf("CLI ResolveFileTarget: %v", err)
	}
	if !got.Equal(wantFile) {
		t.Fatalf("workspace file target = %+v, want the CLI's %+v", got, wantFile)
	}
}

// The disabled Terminal-split row keeps its shipped refusal visible in the
// vertical list this surface actually renders.
func TestDisabledTerminalRowVisibleInVerticalList(t *testing.T) {
	reason := "Two terminals are already on screen — close one first"
	p := New()
	p.ctx = &plugin.Context{WorkDir: t.TempDir(), Epoch: 1, Config: &config.Config{}}
	p.width, p.height = 120, 36
	opts := p.createOpenOpts(workspacecreate.KindTerminalSplit, true, "")
	opts.AllowTerminalSplit = true
	opts.TerminalSplitDisabled = reason
	opts.ShowNotes = true
	opts.Providers = []workspacecreate.ProviderItem{{ID: "jira-work"}}
	p.createForm = workspacecreate.Open(opts)
	m := p.createForm.Build(70)
	view := ansi.Strip(m.Render(100, 40, nil))
	if !strings.Contains(view, "Two terminals are already on screen") {
		t.Fatalf("disabled row lost its inline reason:\n%s", view)
	}
	if !strings.Contains(view, "Terminal split") {
		t.Fatalf("disabled row vanished entirely:\n%s", view)
	}
}
