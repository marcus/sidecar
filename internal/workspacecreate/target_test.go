package workspacecreate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/marcus/sidecar/internal/uirequest"
)

// gitRepo makes a one-commit repository so diff resolution runs the same
// rev-parse the CLI does. Skips when git is unavailable.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Skipf("git %v unavailable: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("add", ".")
	run("commit", "-q", "-m", "first")
	return dir
}

// TestPickerTargetsMatchCLITargets is the picker-to-target contract: for the
// same input, each kind's picker resolution produces the SAME target shape
// `sidecar open` produces through uirequest.ResolveTarget — not a near miss,
// the same value, line, and provider fields.
func TestPickerTargetsMatchCLITargets(t *testing.T) {
	dir := gitRepo(t)

	fileTarget := func(raw string) uirequest.Target {
		t.Helper()
		want, err := uirequest.ResolveFileTarget(dir, raw, 0)
		if err != nil {
			t.Fatalf("ResolveFileTarget(%q): %v", raw, err)
		}
		got, err := ResolvePickerTarget(dir, KindFile, "", raw)
		if err != nil {
			t.Fatalf("picker File %q: %v", raw, err)
		}
		if !got.Equal(want) {
			t.Fatalf("picker file target = %+v, want the CLI's %+v", got, want)
		}
		return want
	}

	plain := fileTarget("main.go")
	if plain.Kind != uirequest.TargetKindFile || filepath.IsAbs(plain.Value) || plain.Line != 0 {
		t.Fatalf("plain file target = %+v, want a relative file with no line", plain)
	}
	withLine := fileTarget("main.go:12")
	if withLine.Line != 12 {
		t.Fatalf("path:line target = %+v, want line 12", withLine)
	}

	// Diff: empty is the working tree, a commit resolves to its identity —
	// both exactly what `sidecar open --diff` answers.
	for _, raw := range []string{"", "HEAD"} {
		want, err := uirequest.ResolveDiffSpec(dir, raw)
		if err != nil {
			t.Fatalf("ResolveDiffSpec(%q): %v", raw, err)
		}
		got, err := ResolvePickerTarget(dir, KindDiff, "", raw)
		if err != nil {
			t.Fatalf("picker diff %q: %v", raw, err)
		}
		if !got.Equal(want) {
			t.Fatalf("picker diff target = %+v, want the CLI's %+v", got, want)
		}
	}

	// Issue and note ids classify without touching a store, same as open.
	for _, tc := range []struct {
		kind Kind
		raw  string
		want uirequest.Target
	}{
		{KindIssue, "td-756c34", uirequest.Target{Kind: uirequest.TargetKindIssue, Value: "td-756c34"}},
		{KindNote, "nt-4jdj4e", uirequest.Target{Kind: uirequest.TargetKindNote, Value: "nt-4jdj4e"}},
	} {
		got, err := ResolvePickerTarget(dir, tc.kind, "", tc.raw)
		if err != nil {
			t.Fatalf("picker %v %q: %v", tc.kind, tc.raw, err)
		}
		if !got.Equal(tc.want) {
			t.Fatalf("picker target = %+v, want %+v", got, tc.want)
		}
	}

	// Resource locators validate against the same limits as --provider.
	want, err := uirequest.ResolveResourceTarget("jira-work", "PROJ-123")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolvePickerTarget(dir, KindResource, "jira-work", "PROJ-123")
	if err != nil {
		t.Fatalf("picker resource: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("picker resource target = %+v, want %+v", got, want)
	}
	if _, err := ResolvePickerTarget(dir, KindResource, "jira-work", ""); err == nil {
		t.Fatal("empty locator accepted")
	}
	if _, err := ResolvePickerTarget(dir, KindResource, "", "PROJ-123"); err == nil {
		t.Fatal("empty provider accepted")
	}

	// Refusals stay refusals: a non-id in an issue picker does not silently
	// become some other kind's target.
	if _, err := ResolvePickerTarget(dir, KindIssue, "", "not-an-issue"); err == nil {
		t.Fatal("issue picker accepted a non-id")
	}
	if _, err := ResolvePickerTarget(dir, KindNote, "", "nope"); err == nil {
		t.Fatal("note picker accepted a non-id")
	}
	if _, err := ResolvePickerTarget(dir, KindFile, "", "missing-file.go"); err == nil {
		t.Fatal("file picker accepted a missing path")
	}
}
