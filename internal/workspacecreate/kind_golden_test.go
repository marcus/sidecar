package workspacecreate

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/sidecar/internal/mouse"
)

// The kind chooser is the selector the rest of the app is adopting, so it moved
// into internal/modal as modal.Select. That move is a refactor, not a redesign:
// these goldens are the exact bytes the Create Workspace modal drew before it,
// recorded from the pre-change code, and cover both shapes (the segmented
// toggle under five rows, the vertical list at five or more), a disabled row,
// the focused and unfocused frames, and the widths the two hosts use.
//
// Regenerate deliberately, and only with a reason:
//
//	REGEN_KIND_GOLDEN=1 go test ./internal/workspacecreate/ -run TestKindControlRendersAsBefore
var regenKindGolden = flag.Bool("regen-kind-golden", false, "rewrite the kind chooser goldens")

func kindGoldenCases() []struct {
	name  string
	opts  OpenOpts
	width int
	focus string
} {
	paneOnly := func() OpenOpts {
		opts := testOpts(KindFile)
		opts.PaneKindsOnly = true
		return opts
	}
	full := func() OpenOpts {
		opts := testOpts(KindShell)
		opts.AllowTerminalSplit = true
		opts.ShowNotes = true
		return opts
	}
	return []struct {
		name  string
		opts  OpenOpts
		width int
		focus string
	}{
		{name: "segmented-two-rows", opts: testOpts(KindShell), width: 52, focus: FieldKind},
		{name: "segmented-two-rows-unfocused", opts: testOpts(KindShell), width: 52, focus: FieldName},
		{name: "segmented-four-rows", opts: paneOnly(), width: 70, focus: FieldKind},
		{name: "list-six-rows", opts: full(), width: 70, focus: FieldKind},
		{name: "list-six-rows-narrow", opts: full(), width: 52, focus: FieldKind},
		{name: "list-six-rows-unfocused", opts: full(), width: 70, focus: FieldName},
		{
			name: "list-eight-rows-with-providers",
			opts: func() OpenOpts {
				opts := full()
				opts.Providers = []ProviderItem{{ID: "jira-work"}, {ID: "linear-eng"}}
				return opts
			}(),
			width: 70,
			focus: FieldKind,
		},
		{
			name: "list-disabled-row",
			opts: func() OpenOpts {
				opts := full()
				opts.TerminalSplitDisabled = "Two terminals are already on screen — close one first"
				return opts
			}(),
			width: 70,
			focus: FieldKind,
		},
	}
}

func TestKindControlRendersAsBefore(t *testing.T) {
	for _, tc := range kindGoldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := Open(tc.opts)
			m := f.Build(tc.width)
			if m == nil {
				t.Fatal("Build returned nil")
			}
			m.Render(100, 44, mouse.NewHandler())
			m.SetFocus(tc.focus)
			got := m.Render(100, 44, mouse.NewHandler())

			path := filepath.Join("testdata", "kindcontrol", tc.name+".golden")
			if *regenKindGolden || os.Getenv("REGEN_KIND_GOLDEN") == "1" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s", path)
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (regenerate with REGEN_KIND_GOLDEN=1)", err)
			}
			if string(want) != got {
				t.Fatalf("the kind chooser no longer draws what it drew before the modal.Select promotion.\n--- want ---\n%s\n--- got ---\n%s", want, got)
			}
		})
	}
}
