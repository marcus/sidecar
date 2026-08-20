package overview

import (
	"testing"

	"github.com/marcus/sidecar/internal/parityscan"
	"github.com/marcus/sidecar/internal/targetactivation"
)

// TestPreviewDispatchesEveryPlanKind is one half of the surface parity pair;
// its twin is TestTerminalDispatchesEveryPlanKind in the workspace plugin. A
// kind that activates on one surface must activate on the other, and the shared
// list of plan kinds a scanned span can produce is what both are measured
// against.
func TestPreviewDispatchesEveryPlanKind(t *testing.T) {
	t.Parallel()
	for _, kind := range targetactivation.PlanKindsFromSpans() {
		if !previewHandlesPlanKind(kind) {
			t.Fatalf("the global preview surface does not activate %s", kind)
		}
	}
	if previewHandlesPlanKind(targetactivation.PlanKind("invented")) {
		t.Fatal("an unknown plan kind must not report as handled")
	}
}

// TestPreviewHandledKindsAreTheDispatchedKinds closes the gap the declaration
// above leaves open: previewHandlesPlanKind is hand-written, so a kind added to
// it but never given a branch in activatePreviewPlan would satisfy the parity
// pair while clicking it did nothing. This reads the real switch out of the
// source and requires the two to name exactly the same kinds.
func TestPreviewHandledKindsAreTheDispatchedKinds(t *testing.T) {
	t.Parallel()
	declared := parityscan.HandledKinds(t, "preview_links.go", "previewHandlesPlanKind")
	dispatched := parityscan.HandledKinds(t, "preview_links.go", "activatePreviewPlan")
	parityscan.RequireSameKinds(t, "the global preview surface", declared, dispatched)
}

// TestSessionPlanNeedsARowRunningIt: attaching on this surface means typing
// into the live pane the session is already showing in, so a session no row is
// running is not attachable and the click must report itself unhandled rather
// than pretending.
func TestSessionPlanNeedsARowRunningIt(t *testing.T) {
	t.Parallel()
	m := &Model{}
	if cmd := m.attachPreviewSession("sidecar-ws-nobody"); cmd != nil {
		t.Fatal("attached a session no workspace is running")
	}
	if cmd := m.attachPreviewSession("  "); cmd != nil {
		t.Fatal("attached an empty session name")
	}
	cmd, handled := m.activatePreviewPlan(targetactivation.Plan{
		Kind: targetactivation.PlanAttachSession, Session: "sidecar-ws-nobody",
	})
	if handled || cmd != nil {
		t.Fatalf("unknown session reported handled=%v cmd=%v", handled, cmd != nil)
	}
}
