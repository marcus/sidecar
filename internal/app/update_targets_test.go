package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/version"
)

func withTasksFeature(t *testing.T, enabled bool) {
	t.Helper()
	cfg := config.Default()
	cfg.Features.Flags[features.TasksPlugin.Name] = enabled
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })
}

func productIDs(descs []version.Descriptor) []version.ProductID {
	ids := make([]version.ProductID, 0, len(descs))
	for _, d := range descs {
		ids = append(ids, d.Product)
	}
	return ids
}

// Tasks takes part only when the feature flag is effectively enabled, so a
// disabled plugin schedules no Tasks check at all.
func TestUpdateDescriptors_TasksGatedByFeature(t *testing.T) {
	withTasksFeature(t, false)
	got := productIDs(updateDescriptors(nil))
	for _, id := range got {
		if id == version.ProductTasks {
			t.Fatalf("Tasks must not be discovered while disabled: %v", got)
		}
	}

	withTasksFeature(t, true)
	found := false
	for _, id := range productIDs(updateDescriptors(nil)) {
		if id == version.ProductTasks {
			found = true
		}
	}
	if !found {
		t.Fatal("Tasks should be discovered when the feature is enabled")
	}
}

// A CLI override behaves exactly like plugin assembly.
func TestUpdateDescriptors_CLIOverrideWins(t *testing.T) {
	withTasksFeature(t, true)
	features.SetOverride(features.TasksPlugin.Name, false)

	for _, id := range productIDs(updateDescriptors(nil)) {
		if id == version.ProductTasks {
			t.Fatal("a CLI override disabling Tasks must exclude it from discovery")
		}
	}
}

func TestProductCheckCmds_MatchesEnabledProducts(t *testing.T) {
	withTasksFeature(t, false)
	m := &Model{currentVersion: "0.95.0"}
	if got := len(m.productCheckCmds(false)); got != 2 {
		t.Errorf("expected 2 checks with Tasks disabled, got %d", got)
	}

	withTasksFeature(t, true)
	if got := len(m.productCheckCmds(false)); got != 3 {
		t.Errorf("expected 3 checks with Tasks enabled, got %d", got)
	}
}

func target(id version.ProductID, name, cur, latest string, hasUpdate bool) version.Target {
	return version.Target{
		Product: id, DisplayName: name, Enabled: true, Installed: true,
		CurrentVersion: cur, LatestVersion: latest, HasUpdate: hasUpdate,
		Install: version.Installation{
			Method: version.InstallMethodHomebrew, Managed: true,
			ManualCommand: "brew upgrade marcus/tap/" + string(id),
		},
	}
}

func TestSetProductStatus_ReplacesAndOrders(t *testing.T) {
	m := &Model{}
	m.setProductStatus(version.ProductStatusMsg{Target: target(version.ProductTasks, "Tasks", "1.5.0", "1.6.0", true)})
	m.setProductStatus(version.ProductStatusMsg{Target: target(version.ProductSidecar, "Sidecar", "0.95.0", "0.96.0", true)})
	m.setProductStatus(version.ProductStatusMsg{Target: target(version.ProductTd, "td", "1.0.0", "1.0.0", false)})

	if len(m.products) != 3 {
		t.Fatalf("expected 3 products, got %d", len(m.products))
	}
	want := []version.ProductID{version.ProductSidecar, version.ProductTd, version.ProductTasks}
	for i, id := range want {
		if m.products[i].Product != id {
			t.Errorf("products[%d] = %s, want %s", i, m.products[i].Product, id)
		}
	}

	// A later check for the same product replaces, never duplicates.
	m.setProductStatus(version.ProductStatusMsg{Target: target(version.ProductTasks, "Tasks", "1.6.0", "1.6.0", false)})
	if len(m.products) != 3 {
		t.Fatalf("re-check must not duplicate a product: %d", len(m.products))
	}
	if m.availableUpdateCount() != 1 {
		t.Errorf("expected only Sidecar to be updatable, got %d", m.availableUpdateCount())
	}
}

func TestUpdateToastSummary(t *testing.T) {
	m := &Model{}
	if got := m.updateToastSummary(); got != "" {
		t.Errorf("no updates should produce no toast, got %q", got)
	}
	m.setProductStatus(version.ProductStatusMsg{Target: target(version.ProductSidecar, "Sidecar", "0.95.0", "0.96.0", true)})
	if got := m.updateToastSummary(); !strings.Contains(got, "Sidecar 0.96.0") {
		t.Errorf("single update should name the product, got %q", got)
	}
	m.setProductStatus(version.ProductStatusMsg{Target: target(version.ProductTd, "td", "1.0.0", "1.1.0", true)})
	if got := m.updateToastSummary(); !strings.HasPrefix(got, "2 updates available") {
		t.Errorf("multiple updates should be summarized, got %q", got)
	}
}

// modelWithBatch returns a model mid-batch over the given plan.
func modelWithBatch(plan []version.Target) *Model {
	return &Model{
		width: 80, height: 30,
		updatePlan: plan, updatePlanID: 7, updateInProgress: true,
		updateModalState: UpdateModalProgress,
	}
}

// A later failure must not erase an earlier successful upgrade.
func TestHandleUpdateTargetResult_PartialFailureRetainsSuccess(t *testing.T) {
	plan := []version.Target{
		target(version.ProductSidecar, "Sidecar", "0.95.0", "0.96.0", true),
		target(version.ProductTd, "td", "1.0.0", "1.1.0", true),
		target(version.ProductTasks, "Tasks", "1.5.0", "1.6.0", true),
	}
	m := modelWithBatch(plan)

	m.handleUpdateTargetResult(UpdateTargetResultMsg{PlanID: 7, Index: 0,
		Result: version.Result{Target: plan[0], Status: version.StatusUpdated, Version: "0.96.0"}})
	m.handleUpdateTargetResult(UpdateTargetResultMsg{PlanID: 7, Index: 1,
		Result: version.Result{Target: plan[1], Status: version.StatusUpdated, Version: "1.1.0"}})
	m.handleUpdateTargetResult(UpdateTargetResultMsg{PlanID: 7, Index: 2,
		Result: version.Result{Target: plan[2], Status: version.StatusFailed, Err: errors.New("brew exited 1")}})

	if len(m.updateResults) != 3 {
		t.Fatalf("expected 3 settled results, got %d", len(m.updateResults))
	}
	if m.updateResults[0].Status != version.StatusUpdated || m.updateResults[1].Status != version.StatusUpdated {
		t.Error("earlier successes must be retained after a later failure")
	}
	if m.updateModalState != UpdateModalError {
		t.Errorf("a failed target should settle into the error surface, got %v", m.updateModalState)
	}
	if !m.needsRestart {
		t.Error("Sidecar changed, so a restart is required")
	}
	if retry := version.RetryTargets(m.updateResults); len(retry) != 1 || retry[0].Product != version.ProductTasks {
		t.Errorf("retry must select only the failed target, got %v", retry)
	}
}

// A td/Tasks-only batch must not claim Sidecar needs restarting.
func TestHandleUpdateTargetResult_NoRestartForStandaloneOnly(t *testing.T) {
	plan := []version.Target{target(version.ProductTd, "td", "1.0.0", "1.1.0", true)}
	m := modelWithBatch(plan)

	m.handleUpdateTargetResult(UpdateTargetResultMsg{PlanID: 7, Index: 0,
		Result: version.Result{Target: plan[0], Status: version.StatusUpdated, Version: "1.1.0"}})

	if m.needsRestart {
		t.Error("a td-only update must not require restarting Sidecar")
	}
	if m.updateModalState != UpdateModalComplete {
		t.Errorf("expected completion, got %v", m.updateModalState)
	}
	if m.updateInProgress {
		t.Error("batch should be settled")
	}
}

// Stale results from a superseded batch must not mutate the current one.
func TestHandleUpdateTargetResult_RejectsStaleMessages(t *testing.T) {
	plan := []version.Target{target(version.ProductTd, "td", "1.0.0", "1.1.0", true)}
	m := modelWithBatch(plan)

	m.handleUpdateTargetResult(UpdateTargetResultMsg{PlanID: 6, Index: 0,
		Result: version.Result{Target: plan[0], Status: version.StatusUpdated}})
	if len(m.updateResults) != 0 {
		t.Fatal("a result from an older plan must be ignored")
	}

	// Out-of-order results are ignored too: the batch is strictly sequential.
	m.handleUpdateTargetResult(UpdateTargetResultMsg{PlanID: 7, Index: 3,
		Result: version.Result{Target: plan[0], Status: version.StatusUpdated}})
	if len(m.updateResults) != 0 {
		t.Fatal("an out-of-order result must be ignored")
	}

	if cmd := m.handleUpdateBatchReady(UpdateBatchReadyMsg{PlanID: 6}); cmd != nil {
		t.Error("a stale batch-ready message must not start installing")
	}
}

// Retrying starts a new plan; results from the previous plan are then stale.
func TestStartUpdateBatch_RetryBumpsPlanID(t *testing.T) {
	plan := []version.Target{target(version.ProductTasks, "Tasks", "1.5.0", "1.6.0", true)}
	m := modelWithBatch(plan)
	m.handleUpdateTargetResult(UpdateTargetResultMsg{PlanID: 7, Index: 0,
		Result: version.Result{Target: plan[0], Status: version.StatusFailed, Err: errors.New("boom")}})

	previous := m.updatePlanID
	m.startUpdateBatch(version.RetryTargets(m.updateResults))

	if m.updatePlanID == previous {
		t.Fatal("a retry must start a new plan")
	}
	if len(m.updateResults) != 0 {
		t.Error("a retry starts with no settled results")
	}
	m.handleUpdateTargetResult(UpdateTargetResultMsg{PlanID: previous, Index: 0,
		Result: version.Result{Target: plan[0], Status: version.StatusUpdated}})
	if len(m.updateResults) != 0 {
		t.Error("results from the superseded plan must be ignored")
	}
}

// An empty plan never runs anything.
func TestStartUpdateBatch_EmptyPlan(t *testing.T) {
	m := &Model{width: 80, height: 30}
	if cmd := m.startUpdateBatch(nil); cmd != nil {
		t.Error("an empty plan must not run any command")
	}
	if m.updateInProgress {
		t.Error("an empty plan must not report work in progress")
	}
	if m.updateModalState != UpdateModalComplete {
		t.Errorf("an empty plan settles immediately, got %v", m.updateModalState)
	}
	if out := renderUpdatePhase(m); !strings.Contains(out, "Nothing to update") {
		t.Errorf("completion should say there was nothing to do:\n%s", out)
	}
}

// Every phase of the single modal must stay inside the terminal at small
// sizes, for zero, one, two, and three visible products.
func TestUpdateModals_ConstrainedRendering(t *testing.T) {
	all := []version.Target{
		target(version.ProductSidecar, "Sidecar", "0.95.0", "0.96.0", true),
		target(version.ProductTd, "td", "1.0.0", "1.1.0", true),
		target(version.ProductTasks, "Tasks", "1.5.0", "1.6.0", true),
	}
	sizes := [][2]int{{40, 12}, {60, 20}, {120, 40}}
	phases := []struct {
		name  string
		state UpdateModalState
	}{
		{"preview", UpdateModalPreview},
		{"progress", UpdateModalProgress},
		{"complete", UpdateModalComplete},
		{"error", UpdateModalError},
	}

	for count := 0; count <= 3; count++ {
		for _, size := range sizes {
			m := &Model{width: size[0], height: size[1], products: all[:count]}
			m.updatePlan = version.SelectPlan(m.products)
			for i, tgt := range m.updatePlan {
				m.updateResults = append(m.updateResults, version.Result{
					Target: tgt, Status: version.StatusUpdated, Version: tgt.LatestVersion})
				m.updateActiveIdx = i + 1
			}
			if count == 3 {
				m.updateResults[2] = version.Result{
					Target: all[2], Status: version.StatusFailed,
					Output: "brew upgrade failed with a very long message that must be wrapped",
					Err:    errors.New("brew exited 1"),
				}
				m.updateActiveIdx = 3
			}
			m.needsRestart = count > 0

			for _, phase := range phases {
				m.updateModalState = phase.state
				out := renderUpdatePhase(m)
				if out == "" {
					continue
				}
				if w := lipgloss.Width(out); w > size[0] {
					t.Errorf("%s modal with %d products at %dx%d is %d wide",
						phase.name, count, size[0], size[1], w)
				}
				if h := lipgloss.Height(out); h > size[1] {
					t.Errorf("%s modal with %d products at %dx%d is %d tall",
						phase.name, count, size[0], size[1], h)
				}
			}
			m.updateModal = nil
		}
	}
}

// The installing surface must not offer a cancel it cannot honour, must say
// that esc only hides, and must name the product being changed.
func TestProgressModal_NoFalseCancelHint(t *testing.T) {
	plan := []version.Target{target(version.ProductTasks, "Tasks", "1.5.0", "1.6.0", true)}
	m := modelWithBatch(plan)

	out := strings.ToLower(renderUpdatePhase(m))
	if strings.Contains(out, "cancel") {
		t.Errorf("progress modal must not claim the update is cancellable:\n%s", out)
	}
	if !strings.Contains(out, "tasks") {
		t.Errorf("progress modal should name the product being changed:\n%s", out)
	}
	if !strings.Contains(out, "continues") {
		t.Errorf("progress modal should say the update continues behind a hidden modal:\n%s", out)
	}
}

// A discovery result arriving while the confirmation is open must not rebuild
// the modal out from under the user. The single modal reads live state through
// its sections, so the same object simply shows the new row on the next frame.
func TestProductStatus_DoesNotRebuildOpenPreview(t *testing.T) {
	m := &Model{width: 80, height: 30}
	m.products = []version.Target{target(version.ProductTd, "td", "1.0.0", "1.1.0", true)}
	m.openUpdateModal()
	renderUpdatePhase(m)
	built := m.updateModal

	updated, _ := m.Update(version.ProductStatusMsg{
		Target: target(version.ProductSidecar, "Sidecar", "0.95.0", "0.96.0", true)})
	after := updated.(Model)

	if after.updateModal != built {
		t.Error("the open preview must not be rebuilt by a late discovery result")
	}
	if after.updateModal.FocusedID() == "" {
		t.Error("the open preview must keep its focus, or Enter would do nothing")
	}
	out := renderUpdatePhase(&after)
	if !strings.Contains(out, "Sidecar") {
		t.Errorf("the confirmation should show the newly discovered target:\n%s", out)
	}
}

// Once closed, a late discovery result neither shows the modal nor moves the
// flow's phase; the next entry point opens it in whatever phase is current.
func TestProductStatus_RebuildsClosedPreview(t *testing.T) {
	m := &Model{width: 80, height: 30, updateModalState: UpdateModalClosed}
	m.products = []version.Target{target(version.ProductTd, "td", "1.0.0", "1.1.0", true)}
	renderUpdatePhase(m)

	updated, _ := m.Update(version.ProductStatusMsg{
		Target: target(version.ProductSidecar, "Sidecar", "0.95.0", "0.96.0", true)})
	after := updated.(Model)

	if after.updateModalState != UpdateModalClosed {
		t.Errorf("a late result must not reopen a closed flow, got %v", after.updateModalState)
	}
	if out := after.renderUpdateModalOverlay("BG"); out != "BG" {
		t.Error("a closed update flow must paint nothing over the background")
	}
}

// A product that is enabled but not installed is never silently installed.
func TestSelectPlan_EnabledButNotInstalledIsNotPlanned(t *testing.T) {
	m := &Model{}
	notInstalled := version.Target{
		Product: version.ProductTasks, DisplayName: "Tasks", Enabled: true, Installed: false,
	}
	m.setProductStatus(version.ProductStatusMsg{Target: notInstalled})
	if m.hasUpdatesAvailable() {
		t.Error("an uninstalled product must never be part of an update plan")
	}
	row := m.diagnosticsProductRow(notInstalled)
	if !strings.Contains(row, "standalone not installed") ||
		!strings.Contains(row, "Panels → Install Tasks") ||
		!strings.Contains(row, "brew install marcus/tap/tasks") {
		t.Errorf("diagnostics should point at Panels install and still name the command:\n%s", row)
	}
}

// Retrying one failed product must neither re-run nor forget an upgrade that
// already succeeded — including the restart claim that depends on it.
func TestRetry_PreservesEarlierSuccess(t *testing.T) {
	plan := []version.Target{
		target(version.ProductSidecar, "Sidecar", "0.95.0", "0.96.0", true),
		target(version.ProductTasks, "Tasks", "1.5.0", "1.6.0", true),
	}
	m := modelWithBatch(plan)
	m.handleUpdateTargetResult(UpdateTargetResultMsg{PlanID: 7, Index: 0,
		Result: version.Result{Target: plan[0], Status: version.StatusUpdated, Version: "0.96.0"}})
	m.handleUpdateTargetResult(UpdateTargetResultMsg{PlanID: 7, Index: 1,
		Result: version.Result{Target: plan[1], Status: version.StatusFailed, Err: errors.New("boom")}})

	retry := version.RetryTargets(m.settledResults())
	m.startUpdateBatch(retry)
	m.updateInProgress = true
	m.handleUpdateTargetResult(UpdateTargetResultMsg{PlanID: m.updatePlanID, Index: 0,
		Result: version.Result{Target: retry[0], Status: version.StatusUpdated, Version: "1.6.0"}})

	settled := m.settledResults()
	if len(settled) != 2 {
		t.Fatalf("expected both products in the settled set, got %d: %+v", len(settled), settled)
	}
	if settled[0].Target.Product != version.ProductSidecar || settled[0].Status != version.StatusUpdated {
		t.Errorf("the earlier Sidecar success must survive the retry: %+v", settled[0])
	}
	if !m.needsRestart {
		t.Error("Sidecar changed in this session, so a restart is still required after the retry")
	}
	if m.updateModalState != UpdateModalComplete {
		t.Errorf("a fully successful retry should complete, got %v", m.updateModalState)
	}

	// The surface the user is actually looking at must say the same thing.
	out := renderUpdatePhase(m)
	if !strings.Contains(out, "Sidecar") || !strings.Contains(out, "Tasks") {
		t.Errorf("completion should list both products:\n%s", out)
	}
	if strings.Contains(out, "no restart needed") {
		t.Errorf("completion must not deny the restart Sidecar's upgrade requires:\n%s", out)
	}
	if !strings.Contains(out, "Quit & Restart") {
		t.Errorf("completion should offer the restart:\n%s", out)
	}
	if strings.Contains(out, "Press q") {
		t.Errorf("completion must not advertise keyboard shortcuts that are not wired; the button row is the affordance:\n%s", out)
	}
}

// Enter confirms the plan. The modal is rendered once first, exactly as a real
// frame would; no priming step exists any more.
func TestPreviewEnterConfirms(t *testing.T) {
	m := &Model{width: 80, height: 30}
	m.products = []version.Target{target(version.ProductTd, "td", "1.0.0", "1.1.0", true)}
	m.openUpdateModal()
	renderUpdatePhase(m)

	m.handleUpdateModalKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.updateModalState != UpdateModalProgress {
		t.Fatalf("Enter should confirm the update, state is %v", m.updateModalState)
	}
	if len(m.updatePlan) != 1 || m.updatePlan[0].Product != version.ProductTd {
		t.Errorf("confirmed plan = %+v", m.updatePlan)
	}
}
