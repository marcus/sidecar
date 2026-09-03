package workspacecreate

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/mouse"
)

// renderWide renders at the width the hosts actually give the modal; the
// narrow default truncates the widest row.
func renderWide(t *testing.T, f *Form) string {
	t.Helper()
	m := f.Build(70)
	if m == nil {
		t.Fatal("Build returned nil")
	}
	return m.Render(100, 40, mouse.NewHandler())
}

func terminalOpts() OpenOpts {
	opts := testOpts(KindShell)
	opts.AllowTerminalSplit = true
	opts.TerminalName = "term · sidecar"
	return opts
}

// The row set is a table, so the Terminal split row is offered exactly where a
// host can place one and nowhere else — and the pane kinds that joined it in
// M2 are offered everywhere, being HostScoped=false.
func TestTerminalSplitRowIsOfferedPerHost(t *testing.T) {
	tests := []struct {
		name  string
		rows  []kindRow
		kinds []Kind
	}{
		{
			name:  "host with a pane tree",
			rows:  kindRowsForOpts(rowOpts{allowTerminalSplit: true, showNotes: true}),
			kinds: []Kind{KindShell, KindWorktree, KindTerminalSplit, KindFile, KindDiff, KindIssue, KindNote},
		},
		{
			name:  "host without one",
			rows:  kindRowsForOpts(rowOpts{allowTerminalSplit: false, showNotes: true}),
			kinds: []Kind{KindShell, KindWorktree, KindFile, KindDiff, KindIssue, KindNote},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.rows) != len(tc.kinds) {
				t.Fatalf("rows = %d, want %d", len(tc.rows), len(tc.kinds))
			}
			for i, kind := range tc.kinds {
				if tc.rows[i].Kind != kind || tc.rows[i].Label == "" {
					t.Fatalf("row %d = %+v, want kind %v with a label", i, tc.rows[i], kind)
				}
			}
		})
	}

	view := renderWide(t, Open(terminalOpts()))
	if !strings.Contains(view, "Terminal split") {
		t.Fatalf("row missing from the list:\n%s", view)
	}
	if view := renderWide(t, Open(testOpts(KindShell))); strings.Contains(view, "Terminal split") {
		t.Fatalf("row offered by a host that cannot place it:\n%s", view)
	}
}

// Selecting the row keeps the modal one screen: a name field with an auto-name
// default, and none of the worktree or agent inputs a terminal has no use for.
func TestTerminalSplitRowSelection(t *testing.T) {
	f := Open(terminalOpts())
	f.SetKind(KindTerminalSplit)
	if f.Kind() != KindTerminalSplit {
		t.Fatalf("Kind = %v", f.Kind())
	}
	view := renderWide(t, f)
	for _, absent := range []string{"Base Branch", "Agent"} {
		if strings.Contains(view, absent) {
			t.Fatalf("terminal split shows %q:\n%s", absent, view)
		}
	}
	if !strings.Contains(view, "Name") {
		t.Fatalf("terminal split has no name field:\n%s", view)
	}
	if got := f.Validate(); got != "" {
		t.Fatalf("Validate = %q, want submittable with no name", got)
	}
	if got := f.TerminalName(); got != "term · sidecar" {
		t.Fatalf("TerminalName = %q, want the auto-name", got)
	}
	f.nameInput.SetValue("dev server")
	if got := f.TerminalName(); got != "dev server" {
		t.Fatalf("TerminalName = %q, want the typed name", got)
	}
}

// Arrowing through the list reaches every row and clamps at the ends, and a
// click lands on the row it is over — by Y, now that the catalog outgrew the
// horizontal toggle.
func TestKindListReachesTerminalSplit(t *testing.T) {
	f := Open(OpenOpts{Kind: KindShell, FocusKind: true, AllowTerminalSplit: true})
	m := f.Build(52)
	m.Render(80, 40, mouse.NewHandler())
	rows := kindRowsFor(true)
	for i := 1; i < len(rows); i++ {
		m.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
		if f.Kind() != rows[i].Kind {
			t.Fatalf("down %d = %v, want %v", i, f.Kind(), rows[i].Kind)
		}
	}
	// Down past the end stays on the last row.
	m.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if f.Kind() != rows[len(rows)-1].Kind {
		t.Fatalf("down past the end = %v, want to stay on the last row", f.Kind())
	}

	for i, row := range rows {
		click := Open(terminalOpts())
		clickKindRow(t, click, 70, i)
		if click.Kind() != row.Kind {
			t.Fatalf("click on row %d = %v, want %v", i, click.Kind(), row.Kind)
		}
	}
}

// The placement row is the same vocabulary as `--split`, and each button is a
// create action: one click chooses the placement AND creates.
func TestPlacementButtonsCreateInOneClick(t *testing.T) {
	tests := []struct {
		action string
		want   Placement
		split  string
	}{
		{action: ActionPlaceAuto, want: PlacementAuto, split: "auto"},
		{action: ActionPlaceRight, want: PlacementRight, split: "right"},
		{action: ActionPlaceBelow, want: PlacementBelow, split: "below"},
	}
	for _, tc := range tests {
		t.Run(tc.split, func(t *testing.T) {
			f := Open(terminalOpts())
			f.SetKind(KindTerminalSplit)
			if !IsPlacementAction(tc.action) {
				t.Fatalf("%q is not a placement action", tc.action)
			}
			if !f.ApplyPlacementAction(tc.action) {
				t.Fatal("placement click did not ask for a create")
			}
			if f.Placement() != tc.want || f.PlacementSplit() != tc.split {
				t.Fatalf("placement = %v/%q, want %v/%q", f.Placement(), f.PlacementSplit(), tc.want, tc.split)
			}
		})
	}

	f := Open(terminalOpts())
	f.SetKind(KindTerminalSplit)
	if !f.ShowPlacement() {
		t.Fatal("terminal split hides the placement row")
	}
	view := renderWide(t, f)
	for _, label := range []string{"Auto", "Right", "Below"} {
		if !strings.Contains(view, label) {
			t.Fatalf("placement row missing %q:\n%s", label, view)
		}
	}
	// Enter is Auto: the primary action creates without touching the row.
	if f.PlacementSplit() != "auto" {
		t.Fatalf("default placement = %q, want auto", f.PlacementSplit())
	}
	// A kind that this modal does not place has no placement row.
	f.SetKind(KindShell)
	if f.ShowPlacement() {
		t.Fatal("shell offers a placement row")
	}
	if IsPlacementAction(ActionCreate) {
		t.Fatal("Create is not a placement action")
	}
}

// The list opens on the row it was last left on.
func TestKindListRemembersLastSelection(t *testing.T) {
	lastKind = KindShell
	t.Cleanup(func() { lastKind = KindShell })

	f := Open(terminalOpts())
	f.SetKind(KindTerminalSplit)

	reopened := Open(OpenOpts{Kind: KindWorktree, UseLastKind: true, AllowTerminalSplit: true})
	if reopened.Kind() != KindTerminalSplit {
		t.Fatalf("reopened on %v, want the remembered row", reopened.Kind())
	}
	// A host that does not offer the remembered row falls back to one it does.
	fallback := Open(OpenOpts{Kind: KindWorktree, UseLastKind: true})
	if fallback.Kind() == KindTerminalSplit {
		t.Fatal("remembered a row this host cannot place")
	}
	// Without UseLastKind the caller's kind still wins: a task-named worktree
	// open is not a list selection.
	explicit := Open(OpenOpts{Kind: KindWorktree, AllowTerminalSplit: true})
	if explicit.Kind() != KindWorktree {
		t.Fatalf("explicit open = %v, want Worktree", explicit.Kind())
	}
}
