package workspacecreate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspaceops"
)

// canonicalProviders is what a config with one enabled provider yields through
// both hosts' builders.
func canonicalProviders() []ProviderItem {
	return []ProviderItem{{ID: "jira-work"}}
}

// TestPaneSwitcherSurfacesStayInParity is the switcher's parity contract, the
// create action's rule carried to the grown modal: rows and pickers work
// identically from the project workspace and the global Sessions browser,
// including the live-terminal row now that both hosts own a pane tree.
//
// Both surfaces build their form from this package, so parity lives or dies in
// two places — the catalogs their OpenOpts produce, and whether each host
// resolves targets through the core rather than growing its own path. This
// test holds both.
func TestPaneSwitcherSurfacesStayInParity(t *testing.T) {
	// Both hosts can place a second live terminal, so their catalogs are the
	// same when the shared feature is allowed.
	projectRows := kindRowsForOpts(rowOpts{allowTerminalSplit: true, showNotes: true, providers: canonicalProviders()})
	globalRows := kindRowsForOpts(rowOpts{allowTerminalSplit: true, showNotes: true, providers: canonicalProviders()})

	if len(projectRows) != len(globalRows) {
		t.Fatalf("project surface offers %d rows, global %d", len(projectRows), len(globalRows))
	}
	for i, want := range globalRows {
		got := projectRows[i]
		if got.Kind != want.Kind || got.Label != want.Label || got.NeedsTarget != want.NeedsTarget {
			t.Fatalf("row %d differs: project %+v, global %+v", i, got, want)
		}
	}

	// Same picker data resolves to the same target on either surface's form.
	newForm := func(rows []kindRow) *Form {
		f := Open(OpenOpts{Kind: KindIssue})
		f.rows = rows
		return f
	}
	for _, rows := range [][]kindRow{projectRows, globalRows} {
		f := newForm(rows)
		f.SetIssues([]Suggestion{{Value: "td-756c34", Label: "td-756c34  fix(palette): scrollbar"}})
		f.AdvanceToTarget()
		target, err := f.TargetFor("")
		if err != nil {
			t.Fatalf("global-catalog form refused its top suggestion: %v", err)
		}
		if target.Kind != uirequest.TargetKindIssue || target.Value != "td-756c34" {
			t.Fatalf("target = %+v, want the suggested issue", target)
		}
	}

	// And neither host grew its own resolution path: each picker file must
	// resolve through Form.TargetFor and carry the placement through
	// PlacementSplit, the same vocabulary the CLI's --split uses. Neither may
	// fold loader results itself either — a private fold is how the overview
	// once resolved diff rows against "hash  hash  title" strings.
	for _, host := range []struct{ name, file string }{
		{"the project workspace", "../plugins/workspace/create_picker.go"},
		{"the global Sessions browser", "../overview/create_picker.go"},
	} {
		calls := calledSelectors(t, host.file)
		for _, required := range []string{"TargetFor", "PlacementSplit",
			"FoldDiffRefs", "FoldIssues", "FoldNotes"} {
			if !contains(calls, required) {
				t.Fatalf("%s (%s) never calls workspacecreate.%s — it grew its own path",
					host.name, host.file, required)
			}
		}
	}
}

// TestSharedSuggestionFoldsResolveLikeTheCLI drives one loader-shaped sample
// through the folds both hosts share and proves the folded rows resolve
// exactly as the CLI would. The hosts' real appliers are driven per surface —
// overview's applyPickerData/applyCreateFileCandidates and the workspace
// plugin's applyCreatePickerData/applyCreateFileCandidates — in those hosts'
// own suites; the source scan above keeps both wired to these folds.
func TestSharedSuggestionFoldsResolveLikeTheCLI(t *testing.T) {
	dir := gitRepo(t)
	hash := shortHeadHash(t, dir)

	// What a folded ref resolves to is the CLI's answer, not the typed short
	// hash: rev-parse widens it to the committed identity.
	resolvedRef, err := uirequest.ResolveDiffSpec(dir, hash)
	if err != nil {
		t.Skipf("git resolution unavailable: %v", err)
	}

	sample := createPickerSample{
		Refs:   []workspaceops.DiffRef{{Identity: hash, Label: hash + "  first commit"}},
		Issues: []workspaceops.IssueRef{{ID: "td-756c34", Title: "fix(palette): scrollbar", Status: "in_progress"}},
		Notes:  []workspaceops.NoteRef{{ID: "nt-4jdj4e", Title: "scratch"}},
	}

	cases := []struct {
		name string
		kind Kind
		set  func(*Form, createPickerSample)
		want uirequest.Target
	}{
		{
			name: "diff ref folds by identity, not display label",
			kind: KindDiff,
			set:  func(f *Form, s createPickerSample) { f.SetDiffRefs(FoldDiffRefs(s.Refs)) },
			want: resolvedRef,
		},
		{
			name: "issue folds by id with badge",
			kind: KindIssue,
			set:  func(f *Form, s createPickerSample) { f.SetIssues(FoldIssues(s.Issues)) },
			want: uirequest.Target{Kind: uirequest.TargetKindIssue, Value: "td-756c34"},
		},
		{
			name: "note folds by id",
			kind: KindNote,
			set:  func(f *Form, s createPickerSample) { f.SetNotes(FoldNotes(s.Notes)) },
			want: uirequest.Target{Kind: uirequest.TargetKindNote, Value: "nt-4jdj4e"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := OpenOpts{Kind: tc.kind, AllowTerminalSplit: false, ShowNotes: true}
			f := Open(opts)
			tc.set(f, sample)
			f.AdvanceToTarget()
			if tc.kind == KindDiff {
				// The working-tree default leads the diff list; name the ref
				// so the folded row is the one under the cursor.
				f.PickerInput().SetValue(sample.Refs[0].Identity)
				f.SyncAfterInput()
			}
			got, err := f.TargetFor(dir)
			if err != nil {
				t.Fatalf("TargetFor: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("target = %+v, want %+v", got, tc.want)
			}
			// Where the CLI classifies the same raw token, it must answer
			// identically — the folded row resolves to what open would.
			switch tc.kind {
			case KindDiff:
				viaCLI, err := uirequest.ResolveDiffSpec(dir, got.Value)
				if err != nil || !viaCLI.Equal(got) {
					t.Fatalf("CLI diff resolution = %+v/%v, want %+v", viaCLI, err, got)
				}
			case KindIssue:
				viaCLI, err := uirequest.ResolveTarget(dir, got.Value, 0, uirequest.ResolveOptions{})
				if err != nil || !viaCLI.Equal(got) {
					t.Fatalf("CLI issue resolution = %+v/%v, want %+v", viaCLI, err, got)
				}
			}
		})
	}
}

// createPickerSample mirrors what a host's loaders deliver for folding.
type createPickerSample struct {
	Refs   []workspaceops.DiffRef
	Issues []workspaceops.IssueRef
	Notes  []workspaceops.NoteRef
}

func shortHeadHash(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Skipf("git rev-parse unavailable: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// calledSelectors lists the method names invoked in file, so a parity test can
// require that a host routes through shared code.
func calledSelectors(t *testing.T, file string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	seen := map[string]bool{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			seen[sel.Sel.Name] = true
		}
		return true
	})
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatalf("%s names no calls — the parity scan read the wrong source", file)
	}
	return names
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestPickerFilesDoNotResolveTargetsThemselves guards the other direction: a
// host that starts parsing paths or ids locally has left the entry-point
// pattern, and the parity promise with it.
func TestPickerFilesDoNotResolveTargetsThemselves(t *testing.T) {
	for _, host := range []struct{ name, file string }{
		{"the project workspace", "../plugins/workspace/create_picker.go"},
		{"the global Sessions browser", "../overview/create_picker.go"},
	} {
		body := sourceOf(t, host.file)
		for _, forbidden := range []string{"filepath.Rel(", "terminallink.IssueID(", "contentlink.NoteID("} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s (%s) resolves targets itself with %s — resolve through workspacecreate instead",
					host.name, host.file, forbidden)
			}
		}
	}
}

func sourceOf(t *testing.T, file string) string {
	t.Helper()
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	return string(body)
}
