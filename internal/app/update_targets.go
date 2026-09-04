package app

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/plugins/tasks"
	"github.com/marcus/sidecar/internal/version"
)

const (
	// metadataRefreshTimeout bounds the one package-manager metadata refresh.
	metadataRefreshTimeout = 3 * time.Minute
	// targetInstallTimeout bounds installing and verifying one product.
	targetInstallTimeout = 15 * time.Minute
)

// UpdateBatchReadyMsg signals that package-manager metadata was refreshed and
// the confirmed batch may start installing.
type UpdateBatchReadyMsg struct {
	PlanID int
}

// UpdateTargetResultMsg carries the settled result of one target in a batch.
type UpdateTargetResultMsg struct {
	PlanID int
	Index  int
	Result version.Result
}

// updateDescriptors returns the products to discover, in display order.
//
// Tasks takes part only when the Tasks descriptor says it is enabled, so
// plugins.tasks.enabled, the tasks_plugin alias and a CLI override all behave
// exactly as they do in plugin assembly. A disabled plugin adds no startup
// process or network work at all.
func updateDescriptors(cfg *config.Config) []version.Descriptor {
	descs := []version.Descriptor{
		version.SidecarDescriptor(),
		version.TdDescriptor(),
	}
	if tasks.Descriptor().IsEnabled(cfg) {
		descs = append(descs, version.TasksDescriptor())
	}
	return descs
}

// productCheckCmds returns the background release checks for every enabled
// product. force bypasses the per-product cache.
func (m *Model) productCheckCmds(force bool) []tea.Cmd {
	var cmds []tea.Cmd
	for _, d := range updateDescriptors(m.cfg) {
		current := ""
		if d.Product == version.ProductSidecar {
			current = m.currentVersion
		}
		cmds = append(cmds, version.CheckProductAsync(d, current, force))
	}
	return cmds
}

// setProductStatus records a discovered product, replacing any earlier result
// for the same product and keeping the list in display order.
func (m *Model) setProductStatus(msg version.ProductStatusMsg) {
	replaced := false
	for i := range m.products {
		if m.products[i].Product == msg.Target.Product {
			m.products[i] = msg.Target
			replaced = true
			break
		}
	}
	if !replaced {
		m.products = append(m.products, msg.Target)
	}
	m.sortProducts()
}

func (m *Model) sortProducts() {
	order := map[version.ProductID]int{
		version.ProductSidecar: 0,
		version.ProductTd:      1,
		version.ProductTasks:   2,
	}
	sorted := make([]version.Target, 0, len(m.products))
	for rank := 0; rank < 3; rank++ {
		for _, t := range m.products {
			if order[t.Product] == rank {
				sorted = append(sorted, t)
			}
		}
	}
	m.products = sorted
}

// productTarget returns the discovered target for a product, if any.
func (m *Model) productTarget(id version.ProductID) *version.Target {
	for i := range m.products {
		if m.products[i].Product == id {
			return &m.products[i]
		}
	}
	return nil
}

// hasUpdatesAvailable reports whether any discovered product has a real
// available update.
func (m *Model) hasUpdatesAvailable() bool {
	return len(version.SelectPlan(m.products)) > 0
}

// availableUpdateCount is the number of products a confirmation would change.
func (m *Model) availableUpdateCount() int {
	return len(version.SelectPlan(m.products))
}

// startUpdateBatch confirms a plan and begins running it. The plan is captured
// immutably here; later discovery results cannot change what the user
// confirmed.
//
// Results from an earlier batch for products this plan does not touch are
// carried forward, so retrying one failed product neither re-runs nor forgets
// an upgrade that already succeeded.
func (m *Model) startUpdateBatch(plan []version.Target) tea.Cmd {
	// Defense in depth for the sequential-batch design: no entry point may
	// start a second batch while one is in flight, which would bump
	// updatePlanID and orphan the running batch's results while its
	// package-manager subprocess keeps going.
	if m.updateInProgress {
		return nil
	}
	inPlan := make(map[version.ProductID]bool, len(plan))
	for _, t := range plan {
		inPlan[t.Product] = true
	}
	var carried []version.Result
	for _, r := range m.settledResults() {
		if !inPlan[r.Target.Product] {
			carried = append(carried, r)
		}
	}

	m.updatePlanID++
	m.updatePlan = plan
	m.updateCarried = carried
	m.updateResults = nil
	m.updateActiveIdx = 0
	m.updateInProgress = true
	m.updateResultsAcked = false
	m.updateStartTime = time.Now()
	m.updateModalState = UpdateModalProgress

	if len(plan) == 0 {
		m.updateInProgress = false
		m.updateModalState = UpdateModalComplete
		m.ensureUpdateModal()
		return nil
	}

	planID := m.updatePlanID
	m.ensureUpdateModal()
	return tea.Batch(
		m.startElapsedTimer(),
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), metadataRefreshTimeout)
			defer cancel()
			version.RefreshPackageMetadata(ctx, version.DefaultEnvironment(), plan)
			return UpdateBatchReadyMsg{PlanID: planID}
		},
	)
}

// startElapsedTimer starts the elapsed time ticker.
func (m *Model) startElapsedTimer() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return UpdateElapsedTickMsg{}
	})
}

// runUpdateTarget installs and verifies one target of the confirmed plan.
// Targets run sequentially to avoid concurrent package-manager locks and
// confusing PATH changes mid-batch.
func (m *Model) runUpdateTarget(planID, idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.updatePlan) {
		return nil
	}
	target := m.updatePlan[idx]
	return func() tea.Msg {
		// The progress modal truthfully says the update cannot be cancelled, so
		// a hung package manager must still end on its own rather than leaving
		// that modal up forever.
		ctx, cancel := context.WithTimeout(context.Background(), targetInstallTimeout)
		defer cancel()
		result := version.Apply(ctx, version.DefaultEnvironment(), target)
		return UpdateTargetResultMsg{PlanID: planID, Index: idx, Result: result}
	}
}

// handleUpdateBatchReady starts the first target once metadata is refreshed.
func (m *Model) handleUpdateBatchReady(msg UpdateBatchReadyMsg) tea.Cmd {
	if msg.PlanID != m.updatePlanID || !m.updateInProgress {
		return nil // stale batch
	}
	return m.runUpdateTarget(m.updatePlanID, 0)
}

// handleUpdateTargetResult records a settled target and advances the batch.
// It continues after a failure so a later target still gets its chance, and a
// failure never erases an earlier success.
func (m *Model) handleUpdateTargetResult(msg UpdateTargetResultMsg) tea.Cmd {
	if msg.PlanID != m.updatePlanID || !m.updateInProgress {
		return nil // stale result from an abandoned or superseded batch
	}
	if msg.Index != len(m.updateResults) {
		return nil // out-of-order result; the batch is strictly sequential
	}

	m.updateResults = append(m.updateResults, msg.Result)

	// Reflect a verified upgrade in the discovered product list so diagnostics
	// stop offering it.
	if msg.Result.Status == version.StatusUpdated {
		if t := m.productTarget(msg.Result.Target.Product); t != nil {
			t.CurrentVersion = msg.Result.Version
			t.HasUpdate = false
		}
		m.clearDiagnosticsModal()
	}

	m.updateActiveIdx = len(m.updateResults)
	if m.updateActiveIdx < len(m.updatePlan) {
		return m.runUpdateTarget(m.updatePlanID, m.updateActiveIdx)
	}
	return m.finishUpdateBatch()
}

// settledResults is every target outcome the user should still be told about:
// this batch's results plus anything carried over from an earlier one.
func (m *Model) settledResults() []version.Result {
	out := make([]version.Result, 0, len(m.updateCarried)+len(m.updateResults))
	order := map[version.ProductID]int{
		version.ProductSidecar: 0,
		version.ProductTd:      1,
		version.ProductTasks:   2,
	}
	for rank := 0; rank < 3; rank++ {
		for _, set := range [][]version.Result{m.updateCarried, m.updateResults} {
			for _, r := range set {
				if order[r.Target.Product] == rank {
					out = append(out, r)
				}
			}
		}
	}
	return out
}

// finishUpdateBatch settles the batch and picks the completion surface.
func (m *Model) finishUpdateBatch() tea.Cmd {
	results := m.settledResults()
	m.updateInProgress = false
	// A Sidecar upgrade from an earlier batch still means this process is
	// stale, so ask about the whole settled set, not just this batch.
	m.needsRestart = version.RestartRequired(results)

	if len(version.RetryTargets(results)) > 0 {
		if m.updateModalState == UpdateModalProgress {
			m.updateModalState = UpdateModalError
		}
	} else if m.updateModalState == UpdateModalProgress {
		m.updateModalState = UpdateModalComplete
	}
	m.ensureUpdateModal()

	if m.updateModalState == UpdateModalClosed {
		m.ShowToast(version.Summarize(results), 10*time.Second)
	}
	return nil
}

// updateToastSummary describes the discovered updates in one line, so async
// per-product checks arriving one after another do not overwrite each other
// with partial claims.
func (m *Model) updateToastSummary() string {
	plan := version.SelectPlan(m.products)
	switch len(plan) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("%s %s available! Press ! for details",
			plan[0].DisplayName, plan[0].LatestVersion)
	default:
		return fmt.Sprintf("%d updates available! Press ! for details", len(plan))
	}
}
