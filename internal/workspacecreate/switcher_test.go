package workspacecreate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/uirequest"
)

// switcherOpts is the catalog a pane-placing host offers today: every row,
// notes included, plus one resource provider.
func switcherOpts(kind Kind) OpenOpts {
	opts := testOpts(kind)
	opts.AllowTerminalSplit = true
	opts.ShowNotes = true
	opts.Providers = []ProviderItem{{ID: "jira-work"}}
	return opts
}

// The full catalog draws as the mockup's vertical list once it outgrows the
// toggle: one row per kind, descriptions aligned, disabled rows visible with
// their reason inline.
func TestSwitcherKindListIsVerticalWithDescriptions(t *testing.T) {
	f := Open(switcherOpts(KindShell))
	// The width the hosts actually give the modal; the narrow default
	// truncates the longest descriptions.
	m := f.Build(70)
	if m == nil {
		t.Fatal("Build returned nil")
	}
	view := ansi.Strip(m.Render(100, 40, mouse.NewHandler()))
	for _, row := range []struct {
		label string
		desc  string
	}{
		{"Shell", "new agent/shell session"},
		{"Worktree", "shell in a new worktree"},
		{"Terminal split", "terminal beside current pane"},
		{"File", "open a file in a split"},
		{"Git diff", "open a diff in a split"},
		{"td issue", "open an issue in a split"},
		{"jira-work", providerDescription},
		{"Note", "open a note in a split"},
	} {
		if !strings.Contains(view, row.label) {
			t.Fatalf("kind list lost the %q row:\n%s", row.label, view)
		}
		if !strings.Contains(view, row.desc) {
			t.Fatalf("kind list lost %q's description %q:\n%s", row.label, row.desc, view)
		}
	}
}

// A host that cannot run a second live terminal loses exactly one row — the
// Terminal split — and keeps every passive one, resource providers included.
// That is the parity rule, stated at the core both hosts share: a provider row
// opens a Resource pane, which any host with a pane tree can place, so gating
// it on the live-terminal capability is what made it vanish from the global
// browser while `sidecar open <locator>` opened it there perfectly well.
func TestSwitcherCatalogDropsOnlyTheLiveTerminalRow(t *testing.T) {
	providers := []ProviderItem{{ID: "jira"}}
	full := kindRowsForOpts(rowOpts{allowTerminalSplit: true, showNotes: true, providers: providers})
	bare := kindRowsForOpts(rowOpts{allowTerminalSplit: false, showNotes: true, providers: providers})
	if len(full)-len(bare) != 1 {
		t.Fatalf("full catalog = %d rows, bare = %d; want exactly the Terminal split row dropped", len(full), len(bare))
	}
	for i, row := range bare {
		if row.NeedsLiveTerminal {
			t.Fatalf("bare catalog kept live-terminal row %+v at %d", row, i)
		}
	}
	if kindLabel(bare, KindResource) != "jira" {
		t.Fatal("a host without a live-terminal peer lost its configured provider row")
	}
}

// Enter on a target-needing kind advances to the picker step instead of
// submitting, and Esc there returns to the kind list instead of closing.
func TestSwitcherStepFlow(t *testing.T) {
	f := Open(switcherOpts(KindShell))
	f.SetKind(KindFile)
	m := f.Build(52)
	m.Render(80, 40, mouse.NewHandler())

	action, _ := f.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action != "" {
		t.Fatalf("Enter on File returned %q, want it consumed by the step advance", action)
	}
	if f.Step() != StepTarget {
		t.Fatalf("step = %d, want the picker", f.Step())
	}
	view := ansi.Strip(renderForm(t, f))
	if !strings.Contains(view, "New · File") {
		t.Fatalf("picker view lost the step title:\n%s", view)
	}
	if got := f.Modal().FocusedID(); got != FieldPickerInput {
		t.Fatalf("focus after advance = %q, want %q", got, FieldPickerInput)
	}

	action, _ = f.HandleKey(keyEsc())
	if action != "" || f.Step() != StepKind {
		t.Fatalf("esc on picker: action=%q step=%d, want back on the kind list", action, f.Step())
	}
	// And esc on the kind list still closes through the host.
	f.Build(52) // BackToKind rebuilt nothing yet; give the form its modal back.
	if action, _ = f.HandleKey(keyEsc()); action != "cancel" {
		t.Fatalf("esc on kind list = %q, want cancel", action)
	}
}

// The File picker filters its candidates and Enter resolves to a target whose
// shape the CLI would produce.
func TestSwitcherFilePickerResolvesSelection(t *testing.T) {
	f := Open(switcherOpts(KindFile))
	f.SetFileCandidates([]string{"internal/palette/list.go", "docs/plan.md"})
	f.AdvanceToTarget()
	f.PickerInput().SetValue("palette")
	f.SyncAfterInput()

	items := f.items()
	if len(items) != 1 || items[0].Value != "internal/palette/list.go" {
		t.Fatalf("filtered items = %+v, want just the palette file", items)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal/palette"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal/palette/list.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := f.TargetFor(dir)
	if err != nil {
		t.Fatalf("TargetFor: %v", err)
	}
	if got.Kind != uirequest.TargetKindFile || got.Value != "internal/palette/list.go" {
		t.Fatalf("target = %+v, want the palette file relative to root", got)
	}
}

// A typed id that matches nothing in the recents still submits — paste an id.
func TestSwitcherIssuePickerAcceptsPastedID(t *testing.T) {
	f := Open(switcherOpts(KindIssue))
	f.SetIssues([]Suggestion{
		{Value: "td-756c34", Label: "td-756c34  fix(palette): scrollbar", Badge: "in progress"},
	})
	f.AdvanceToTarget()
	f.PickerInput().SetValue("td-9d3b09")
	f.SyncAfterInput()
	if items := f.items(); len(items) != 0 {
		t.Fatalf("unrelated id filtered to %d items, want none", len(items))
	}
	target, err := f.TargetFor("")
	if err != nil {
		t.Fatalf("paste-an-id refused: %v", err)
	}
	if target.Value != "td-9d3b09" || target.Kind != uirequest.TargetKindIssue {
		t.Fatalf("target = %+v, want the pasted issue id", target)
	}
	// A picker with no suggestions at all refuses an empty input loudly
	// rather than opening nothing.
	bare := Open(switcherOpts(KindIssue))
	bare.AdvanceToTarget()
	if bare.items() != nil {
		t.Fatal("issue picker has suggestions without data")
	}
	if _, err := bare.TargetFor(""); err == nil {
		t.Fatal("empty issue input resolved")
	}
}

// The placement row shows for every pane kind. Clicking one from the kind list
// of a target-needing kind records it and continues to the picker; clicking on
// the picker step creates with it. Enter stays Auto unless a button was
// clicked earlier in this modal session.
func TestSwitcherPlacementRowOnEveryPaneKind(t *testing.T) {
	for _, kind := range []Kind{KindTerminalSplit, KindFile, KindDiff, KindIssue, KindResource, KindNote} {
		f := Open(switcherOpts(kind))
		if !f.ShowPlacement() {
			t.Fatalf("%v hides the placement row", kind)
		}
	}
	f := Open(switcherOpts(KindShell))
	if f.ShowPlacement() {
		t.Fatal("shell offers a placement row")
	}

	// Kind-list click on Right: advanced, placement carried.
	f = Open(switcherOpts(KindFile))
	if got := f.ApplyPlacementActionStep(ActionPlaceRight); got != PlacementAdvanced {
		t.Fatalf("placement click from kind list = %v, want advance", got)
	}
	if f.PlacementSplit() != "right" || f.Step() != StepTarget {
		t.Fatalf("after click: split=%q step=%d, want right + picker", f.PlacementSplit(), f.Step())
	}
	// Picker-step click on Below: submit now.
	if got := f.ApplyPlacementActionStep(ActionPlaceBelow); got != PlacementSubmitted {
		t.Fatalf("placement click on picker = %v, want submit", got)
	}
	if f.PlacementSplit() != "below" {
		t.Fatalf("placement = %q, want below", f.PlacementSplit())
	}
}

// One row per configured provider, labelled by instance ID, and none where the
// host passes no providers.
func TestSwitcherProviderRows(t *testing.T) {
	f := Open(switcherOpts(KindResource))
	labels := make([]string, 0, len(f.rows))
	for _, row := range f.rows {
		labels = append(labels, row.Label)
	}
	found := false
	for _, label := range labels {
		if label == "jira-work" {
			found = true
		}
	}
	if !found {
		t.Fatalf("provider row missing from %v", labels)
	}
	if f.selectedProviderID() != "jira-work" {
		t.Fatalf("provider ID = %q, want jira-work", f.selectedProviderID())
	}
	none := Open(testOpts(KindShell))
	for _, row := range none.rows {
		if row.Kind == KindResource {
			t.Fatal("resource row offered without providers")
		}
	}
}

// Two provider rows share one Kind, so selection must track the ROW: picking
// the second provider titles, validates, and resolves against that second
// instance, not whichever sorts first.
func TestSwitcherProviderSelectionTracksInstance(t *testing.T) {
	newForm := func() *Form {
		opts := switcherOpts(KindShell)
		opts.Providers = []ProviderItem{{ID: "jira-work"}, {ID: "linear-eng"}}
		return Open(opts)
	}
	resourceRows := func(f *Form) []int {
		var idxs []int
		for i, row := range f.rows {
			if row.Kind == KindResource {
				idxs = append(idxs, i)
			}
		}
		return idxs
	}

	f := newForm()
	idxs := resourceRows(f)
	if len(idxs) != 2 {
		t.Fatalf("resource rows = %v, want two configured providers", idxs)
	}

	// Click the second provider's row.
	click := newForm()
	clickKindRow(t, click, 70, idxs[1])
	if got := click.selectedProviderID(); got != "linear-eng" {
		t.Fatalf("click selected provider = %q, want linear-eng", got)
	}
	if label := click.selectedLabel(); label != "linear-eng" {
		t.Fatalf("selected label = %q, want the picked instance", label)
	}
	// The list highlights exactly the picked ROW — both providers share one
	// Kind, so a Kind-based highlight would light both cursors at once.
	view := ansi.Strip(renderForm(t, click))
	if !strings.Contains(view, "❯ linear-eng") || strings.Contains(view, "❯ jira-work") {
		t.Fatalf("kind list does not point at only the chosen provider:\n%s", view)
	}

	// And resolution validates against the chosen instance.
	click.AdvanceToTarget()
	if view := ansi.Strip(renderForm(t, click)); !strings.Contains(view, "New · linear-eng") {
		t.Fatalf("modal title does not name the chosen instance:\n%s", view)
	}
	click.PickerInput().SetValue("ENG-42")
	target, err := click.TargetFor("")
	if err != nil {
		t.Fatalf("TargetFor on second provider: %v", err)
	}
	if target.Provider != "linear-eng" || target.Value != "ENG-42" {
		t.Fatalf("target = %+v, want locator under linear-eng", target)
	}

	// Arrow-key selection carries the instance too.
	keys := newForm()
	m := keys.Build(70)
	m.Render(100, 40, mouse.NewHandler())
	m.SetFocus(FieldKind) // the kind list owns up/down only while focused
	for step := 0; step < idxs[1]; step++ {
		m.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if got := keys.selectedProviderID(); got != "linear-eng" {
		t.Fatalf("arrow selection provider = %q, want linear-eng", got)
	}
}

// The disabled Terminal-split row stays visible with its reason inline —
// reusing the shipped live-cap string — while the other rows keep their
// descriptions.
func TestSwitcherDisabledRowKeepsReasonInline(t *testing.T) {
	reason := "Two terminals are already on screen — close one first" // the shipped live-cap string
	opts := switcherOpts(KindShell)
	opts.TerminalSplitDisabled = reason
	f := Open(opts)
	m := f.Build(70)
	if m == nil {
		t.Fatal("Build returned nil")
	}
	view := ansi.Strip(m.Render(100, 40, mouse.NewHandler()))
	// The reason rides the same line as the row; a narrow modal may elide its
	// tail, but its head must be readable before the row is entered.
	if !strings.Contains(view, "Two terminals are already on screen") {
		t.Fatalf("disabled reason not inline:\n%s", view)
	}
	// The disabled row shows its reason IN PLACE OF its description; every
	// other row keeps theirs.
	if !strings.Contains(view, "open a file in a split") {
		t.Fatalf("a non-disabled row lost its description while another was disabled:\n%s", view)
	}
	if strings.Contains(view, "terminal beside current pane") {
		t.Fatal("the disabled row kept its description instead of the reason")
	}
	// The disabled kind cannot be advanced into or submitted past.
	f.SetKind(KindTerminalSplit)
	renderForm(t, f) // SetKind invalidated the modal; rebuild before keys.
	action, _ := f.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action != ActionCreate {
		t.Fatalf("disabled kind Enter produced %q, want Create so the host refuses it visibly", action)
	}
	if f.Step() != StepKind {
		t.Fatal("a disabled kind advanced to the picker")
	}
}

// Count lines read like the mockup's state line for each kind.
func TestSwitcherCountLines(t *testing.T) {
	f := Open(switcherOpts(KindDiff))
	f.SetDiffRefs([]Suggestion{
		{Value: "abc1234", Label: "abc1234  first"},
		{Value: "main", Label: "main  (branch)"},
	})
	f.AdvanceToTarget()
	if got := f.countLine(); got != "working tree default · 2 refs" {
		t.Fatalf("diff count = %q", got)
	}
	f.PickerInput().SetValue("abc")
	f.SyncAfterInput()
	// The query is a subsequence match: abc1234 by prefix, main by its
	// "(branch)" suffix. The working tree is not a ref and never counts.
	if got := f.countLine(); got != "2 of 2 refs" {
		t.Fatalf("filtered diff count = %q, want both matching refs over 2", got)
	}
}

func keyEsc() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEscape}
}
