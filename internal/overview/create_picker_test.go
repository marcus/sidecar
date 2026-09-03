package overview

import (
	"context"
	"testing"

	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspaceops"
)

// The Sessions browser folds loader results through workspacecreate's shared
// folds. This drives THIS host's real data paths — applyPickerData and
// applyCreateFileCandidates — over what the loaders actually return, and
// proves each picker answer resolves exactly as `sidecar open` would from the
// same repo. The diff case is the repro that once failed here:
// RecentDiffRefs already embeds the hash in Label, and this host folding
// Label into Value made every ref row resolve as "hash  hash  title".
func TestOverviewHostPickerDataResolvesLikeTheCLI(t *testing.T) {
	dir := initPreviewTwoCommitRepo(t)
	ctx := context.Background()
	refs, err := workspaceops.RecentDiffRefs(ctx, dir, 15)
	if err != nil {
		t.Fatalf("RecentDiffRefs: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("fixture repo yielded no refs")
	}

	sample := createPickerDataMsg{
		Refs:   refs,
		Issues: []workspaceops.IssueRef{{ID: "td-756c34", Title: "fix(palette): scrollbar", Status: "in_progress"}},
		Notes:  []workspaceops.NoteRef{{ID: "nt-4jdj4e", Title: "scratch"}},
	}

	t.Run("diff ref resolves by identity not display label", func(t *testing.T) {
		f := workspacecreate.Open(workspacecreate.OpenOpts{ShowNotes: true})
		applyPickerData(f, sample)
		f.SetKind(workspacecreate.KindDiff)
		f.AdvanceToTarget()
		f.PickerInput().SetValue(refs[0].Identity)
		f.SyncAfterInput()
		got, err := f.TargetFor(dir)
		if err != nil {
			t.Fatalf("diff TargetFor: %v", err)
		}
		want, err := uirequest.ResolveDiffSpec(dir, refs[0].Identity)
		if err != nil {
			t.Fatalf("CLI ResolveDiffSpec: %v", err)
		}
		if !got.Equal(want) {
			t.Fatalf("overview diff target = %+v, want the CLI's %+v", got, want)
		}
	})

	t.Run("issue row resolves to the id", func(t *testing.T) {
		f := workspacecreate.Open(workspacecreate.OpenOpts{ShowNotes: true})
		applyPickerData(f, sample)
		f.SetKind(workspacecreate.KindIssue)
		f.AdvanceToTarget()
		f.PickerInput().SetValue("td-756c34")
		f.SyncAfterInput()
		got, err := f.TargetFor(dir)
		if err != nil {
			t.Fatalf("issue TargetFor: %v", err)
		}
		want, err := uirequest.ResolveTarget(dir, "td-756c34", 0, uirequest.ResolveOptions{})
		if err != nil {
			t.Fatalf("CLI ResolveTarget: %v", err)
		}
		if !got.Equal(want) {
			t.Fatalf("overview issue target = %+v, want the CLI's %+v", got, want)
		}
	})

	t.Run("note row resolves to the id", func(t *testing.T) {
		f := workspacecreate.Open(workspacecreate.OpenOpts{ShowNotes: true})
		applyPickerData(f, sample)
		f.SetKind(workspacecreate.KindNote)
		f.AdvanceToTarget()
		f.PickerInput().SetValue("nt-4jdj4e")
		f.SyncAfterInput()
		got, err := f.TargetFor(dir)
		if err != nil {
			t.Fatalf("note TargetFor: %v", err)
		}
		if got.Kind != uirequest.TargetKindNote || got.Value != "nt-4jdj4e" {
			t.Fatalf("overview note target = %+v", got)
		}
	})

	t.Run("file candidates resolve against the workspace root", func(t *testing.T) {
		f := workspacecreate.Open(workspacecreate.OpenOpts{ShowNotes: true})
		m := &Model{createForm: f}
		f.SetKind(workspacecreate.KindFile)
		f.AdvanceToTarget()
		m.applyCreateFileCandidates(workspacecreate.FilesScannedMsg{Root: dir, Paths: []string{"a.go"}})
		f.PickerInput().SetValue("a.go")
		f.SyncAfterInput()
		got, err := f.TargetFor(dir)
		if err != nil {
			t.Fatalf("file TargetFor: %v", err)
		}
		want, err := uirequest.ResolveFileTarget(dir, "a.go", 0)
		if err != nil {
			t.Fatalf("CLI ResolveFileTarget: %v", err)
		}
		if !got.Equal(want) {
			t.Fatalf("overview file target = %+v, want the CLI's %+v", got, want)
		}
	})
}
