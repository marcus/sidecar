package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/hosts"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/overview"
	"github.com/marcus/sidecar/internal/projectlist"
	"github.com/marcus/sidecar/internal/state"
)

// switcherTestModel is a switcher opened over a small known collection, with
// state isolated so the remembered sort and view of the developer running the
// tests is neither read nor written.
func projectSwitcherFixture(t *testing.T) Model {
	t.Helper()
	if err := state.InitWithDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.InitWithDir(t.TempDir()) })

	added := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	m := routerTestModel(t, newRouterPlugin())
	m.cfg.Projects.List = []config.ProjectConfig{
		{Name: "tui", Path: "/tmp/tui"},
		{Name: "archive", Path: "/tmp/archive"},
		{Name: "sidecar", Path: "/tmp/sidecar", AddedAt: &added},
	}
	m.ui.WorkDir = "/tmp/sidecar"
	m.showProjectSwitcher = true
	m.initProjectSwitcher()
	return m
}

func projectSwitcherNames(m *Model) []string {
	out := make([]string, len(m.projectSwitcherRows))
	for i, item := range m.projectSwitcherRows {
		out[i] = item.Name
	}
	return out
}

func projectSwitcherFrame(t *testing.T, m *Model) string {
	t.Helper()
	return ansi.Strip(m.renderProjectSwitcherOverlay(""))
}

func projectSwitcherType(m Model, key string) Model {
	next, _ := mustModel(m.Update(tea.KeyPressMsg{Text: key, Code: rune(key[0])}))
	return next
}

func TestProjectSwitcherOpensOnLastActivityWithTheCurrentProjectFirst(t *testing.T) {
	m := projectSwitcherFixture(t)
	if m.projectSwitcherSort != projectlist.SortActivity {
		t.Fatalf("default sort = %q, want last activity", m.projectSwitcherSort.Label())
	}
	if m.projectSwitcherView != projectlist.ViewList {
		t.Fatalf("default view = %q, want the compact list", m.projectSwitcherView.Label())
	}
	// The bound project is active now; the other two have no recorded activity
	// and fall in behind it by name.
	if got := projectSwitcherNames(&m); got[0] != "sidecar" {
		t.Fatalf("collection = %v, want the current project first under last activity", got)
	}
	if got := projectSwitcherNames(&m); got[1] != "archive" || got[2] != "tui" {
		t.Fatalf("collection = %v, want unknown activity ordered by name behind it", got)
	}
}

func TestProjectSwitcherSortChangeReordersAndKeepsTheSelectedProject(t *testing.T) {
	m := projectSwitcherFixture(t)
	m.projectSwitcherCursor = 2 // "tui"
	selected := m.selectedProjectSwitcherID()

	if cmd := m.applyProjectSwitcherSort(projectlist.SortName); cmd != nil {
		cmd()
	}
	if got := projectSwitcherNames(&m); got[0] != "archive" || got[2] != "tui" {
		t.Fatalf("name order = %v, want A–Z", got)
	}
	if m.selectedProjectSwitcherID() != selected {
		t.Fatalf("selection moved to %q; it must ride the destination, not the row index",
			m.selectedProjectSwitcherID())
	}

	// Reversing the direction reverses the collection and still keeps it.
	if cmd := m.toggleProjectSwitcherOrder(); cmd != nil {
		cmd()
	}
	if got := projectSwitcherNames(&m); got[0] != "tui" {
		t.Fatalf("Z–A order = %v", got)
	}
	if m.selectedProjectSwitcherID() != selected {
		t.Fatal("selection did not survive the direction change")
	}
}

func TestProjectSwitcherSortAndViewAreRemembered(t *testing.T) {
	m := projectSwitcherFixture(t)
	if cmd := m.applyProjectSwitcherSort(projectlist.SortAdded); cmd != nil {
		cmd()
	}
	if cmd := m.setProjectSwitcherView(projectlist.ViewGrid); cmd != nil {
		cmd()
	}

	// A later open reads the choice back rather than starting over.
	m.resetProjectSwitcher()
	m.initProjectSwitcher()
	if m.projectSwitcherSort != projectlist.SortAdded {
		t.Fatalf("sort = %q after reopen, want the remembered one", m.projectSwitcherSort.Label())
	}
	if m.projectSwitcherView != projectlist.ViewGrid {
		t.Fatalf("view = %q after reopen, want the remembered one", m.projectSwitcherView.Label())
	}
	if state.GetProjectSwitcherSort() != projectlist.SortAdded.Label() {
		t.Fatalf("state holds %q", state.GetProjectSwitcherSort())
	}
}

func TestProjectSwitcherUnknownDatesReadUnknownRatherThanGuessing(t *testing.T) {
	m := projectSwitcherFixture(t)
	if cmd := m.applyProjectSwitcherSort(projectlist.SortAdded); cmd != nil {
		cmd()
	}
	frame := projectSwitcherFrame(t, &m)
	if !strings.Contains(frame, "DATE ADDED") {
		t.Fatalf("date-added sort did not label its column:\n%s", frame)
	}
	if !strings.Contains(frame, "2026-01-02") {
		t.Fatalf("the one recorded registration date is missing:\n%s", frame)
	}
	if !strings.Contains(frame, projectlist.UnknownLabel) {
		t.Fatalf("projects with no recorded date must read %q:\n%s", projectlist.UnknownLabel, frame)
	}
	// The two projects registered before Sidecar recorded the date sort last,
	// which is the whole point of the word.
	names := projectSwitcherNames(&m)
	if names[0] != "sidecar" {
		t.Fatalf("date-added order = %v, want the known date first", names)
	}
}

func TestProjectSwitcherCompactListShowsTheColumnsAndTheCurrentBadge(t *testing.T) {
	m := projectSwitcherFixture(t)
	frame := projectSwitcherFrame(t, &m)
	for _, want := range []string{"PROJECT", "PATH", "LAST ACTIVITY", "current"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("compact list is missing %q:\n%s", want, frame)
		}
	}
	// The current project and the highlighted row are separate facts. Moving
	// the cursor off the current project must not take its badge with it.
	m.projectSwitcherCursor = 1
	m.clearProjectSwitcherModal()
	if frame := projectSwitcherFrame(t, &m); !strings.Contains(frame, "current") {
		t.Fatalf("the current project lost its badge when the cursor moved:\n%s", frame)
	}
}

func TestProjectSwitcherRowsAreFullWidthClickTargets(t *testing.T) {
	m := projectSwitcherFixture(t)
	_ = projectSwitcherFrame(t, &m)
	region := projectSwitcherItemRegion(t, m.projectSwitcherMouseHandler, 1)
	if region.Rect.W < m.projectSwitcherContentWidth()-2 {
		t.Fatalf("row hit region is %d wide, want the whole row", region.Rect.W)
	}
	if region.Rect.H != 1 {
		t.Fatalf("row hit region is %d tall, want one row", region.Rect.H)
	}
}

func TestProjectSwitcherGridCardsAreWholeCardClickTargets(t *testing.T) {
	m := projectSwitcherFixture(t)
	if cmd := m.setProjectSwitcherView(projectlist.ViewGrid); cmd != nil {
		cmd()
	}
	if m.projectSwitcherEffectiveView() != projectlist.ViewGrid {
		t.Fatalf("grid is not drawable at width %d", m.projectSwitcherCollectionWidth())
	}
	_ = projectSwitcherFrame(t, &m)
	region := projectSwitcherItemRegion(t, m.projectSwitcherMouseHandler, 0)
	if region.Rect.W != projectlist.CardWidth || region.Rect.H != projectlist.CardHeight {
		t.Fatalf("card hit region = %dx%d, want %dx%d",
			region.Rect.W, region.Rect.H, projectlist.CardWidth, projectlist.CardHeight)
	}
}

func TestProjectSwitcherGridKeepsTheSelectionAndTheFilter(t *testing.T) {
	m := projectSwitcherFixture(t)
	m.projectSwitcherInput.SetValue("t")
	m.setProjectSwitcherCollection("t")
	m.projectSwitcherCursor = 0
	selected := m.selectedProjectSwitcherID()
	filtered := len(m.projectSwitcherFiltered)

	if cmd := m.setProjectSwitcherView(projectlist.ViewGrid); cmd != nil {
		cmd()
	}
	if len(m.projectSwitcherFiltered) != filtered {
		t.Fatalf("changing view changed the collection: %d rows, want %d",
			len(m.projectSwitcherFiltered), filtered)
	}
	if m.selectedProjectSwitcherID() != selected {
		t.Fatal("changing view moved the selection")
	}
}

func TestProjectSwitcherGridArrowsAreSpatial(t *testing.T) {
	m := projectSwitcherFixture(t)
	m.cfg.Projects.List = manyProjects(9)
	m.setProjectSwitcherCollection("")
	if cmd := m.setProjectSwitcherView(projectlist.ViewGrid); cmd != nil {
		cmd()
	}
	columns := projectlist.GridColumns(m.projectSwitcherCollectionWidth())
	if columns < 2 {
		t.Skipf("grid has %d columns at this width", columns)
	}
	m.projectSwitcherCursor = 0
	m.moveProjectSwitcherCursor(1, 0)
	if m.projectSwitcherCursor != 1 {
		t.Fatalf("right moved to %d, want the next card in the row", m.projectSwitcherCursor)
	}
	m.moveProjectSwitcherCursor(0, 1)
	if m.projectSwitcherCursor != 1+columns {
		t.Fatalf("down moved to %d, want the card below (%d)", m.projectSwitcherCursor, 1+columns)
	}
	m.projectSwitcherCursor = 0
	m.moveProjectSwitcherCursor(-1, 0)
	if m.projectSwitcherCursor != 0 {
		t.Fatal("left at the row's edge must stay put rather than wrap")
	}
}

func TestProjectSwitcherNarrowTerminalDrawsTheListRatherThanClippedCards(t *testing.T) {
	m := projectSwitcherFixture(t)
	if cmd := m.setProjectSwitcherView(projectlist.ViewGrid); cmd != nil {
		cmd()
	}
	m.width = 40
	m.clearProjectSwitcherModal()
	m.ensureProjectSwitcherModal()
	if m.projectSwitcherEffectiveView() != projectlist.ViewList {
		t.Fatalf("at width %d the grid must fall back to the list", m.width)
	}
	// The user's choice is not thrown away; only the drawing changes.
	if m.projectSwitcherView != projectlist.ViewGrid {
		t.Fatal("the fallback must not rewrite the remembered view")
	}
	if frame := projectSwitcherFrame(t, &m); strings.Contains(frame, "PATH") {
		t.Fatalf("a narrow modal should drop the path column, not squeeze it:\n%s", frame)
	}
}

func TestProjectSwitcherPrintableKeysAlwaysReachTheFilter(t *testing.T) {
	m := projectSwitcherFixture(t)
	// g and s are the initials of Grid and Sort. Neither may be a shortcut:
	// the filter owns every printable key while the switcher is open.
	for _, key := range []string{"g", "s", "n"} {
		m = projectSwitcherType(m, key)
	}
	if got := m.projectSwitcherInput.Value(); got != "gsn" {
		t.Fatalf("filter = %q, want the typed text; a control stole a key", got)
	}
	if m.projectSwitcherView != projectlist.ViewList || m.projectSwitcherSortOpen {
		t.Fatal("typing changed a control")
	}
}

func TestProjectSwitcherTabWalksTheControlsAndSpaceOpensTheSortMenu(t *testing.T) {
	m := projectSwitcherFixture(t)
	tab := func() { m, _ = mustModel(m.Update(tea.KeyPressMsg{Code: tea.KeyTab})) }

	tab()
	if m.projectSwitcherFocus != switcherFocusSort {
		t.Fatalf("first tab reached %v, want the sort control", m.projectSwitcherFocus)
	}
	m, _ = mustModel(m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}))
	if !m.projectSwitcherSortOpen {
		t.Fatal("space on the sort control did not open its menu")
	}
	if frame := projectSwitcherFrame(t, &m); !strings.Contains(frame, "Sort projects") {
		t.Fatalf("the sort menu is not drawn:\n%s", frame)
	}

	// Escape dismisses the menu before it touches the filter or the modal.
	m, _ = mustModel(m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}))
	if m.projectSwitcherSortOpen {
		t.Fatal("escape did not close the sort menu")
	}
	if !m.showProjectSwitcher {
		t.Fatal("escape closed the switcher instead of the menu it was in")
	}
}

func TestProjectSwitcherSortMenuAppliesAMode(t *testing.T) {
	m := projectSwitcherFixture(t)
	m.openProjectSwitcherSort()
	m.projectSwitcherSortIdx = projectlist.SortIndex(projectlist.SortName, projectlist.SortModes)
	handled, cmd := m.handleProjectSwitcherSortKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("the open menu did not take enter")
	}
	if cmd != nil {
		cmd()
	}
	if m.projectSwitcherSort != projectlist.SortName {
		t.Fatalf("sort = %q, want Name", m.projectSwitcherSort.Label())
	}
	if m.projectSwitcherSortOpen {
		t.Fatal("applying a mode left the menu open")
	}
	// Picking a mode adopts that mode's own direction rather than inheriting
	// "newest first" from the mode before it.
	if m.projectSwitcherOrder != projectlist.OrderAscending {
		t.Fatalf("name order = %q, want A–Z", projectlist.OrderLabel(projectlist.SortName, m.projectSwitcherOrder))
	}
}

func TestProjectSwitcherNoResultsNamesTheQueryAndKeepsIt(t *testing.T) {
	m := projectSwitcherFixture(t)
	m.projectSwitcherInput.SetValue("sidecart")
	m.setProjectSwitcherCollection("sidecart")
	frame := projectSwitcherFrame(t, &m)
	if !strings.Contains(frame, `No projects match "sidecart"`) {
		t.Fatalf("the empty state does not name the query:\n%s", frame)
	}
	if !strings.Contains(frame, "esc clears the filter") {
		t.Fatalf("the empty state does not name a way back:\n%s", frame)
	}
	if m.projectSwitcherInput.Value() != "sidecart" {
		t.Fatal("the query was cleared out from under the user")
	}
}

func TestProjectSwitcherRemoteRefusalKeepsItsReasonAndDoesNotSwitch(t *testing.T) {
	m := projectSwitcherFixture(t)
	m.testHostCatalog = []overview.HostCatalogEntry{{
		ID:     "beta",
		Health: hosts.Health{State: hosts.StateUnreachable, Detail: "ssh: connect: host is down"},
		Projects: []overview.HostCatalogProject{{
			Key: "/srv/api", Name: "api", Root: "/srv/api",
		}},
	}}
	m.setProjectSwitcherCollection("")

	index := -1
	for i, item := range m.projectSwitcherRows {
		if item.Host == "beta" {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("the remote destination is missing from the collection")
	}
	item := m.projectSwitcherRows[index]
	if !item.Disabled() {
		t.Fatal("an unreachable host's project must stay refused")
	}
	if !item.LastActiveAt.IsZero() || !item.AddedAt.IsZero() {
		t.Fatal("a remote project's dates belong to its own host, never to a local record")
	}
	if frame := projectSwitcherFrame(t, &m); !strings.Contains(frame, "unreachable") {
		t.Fatalf("the refusal reason is not visible on the row:\n%s", frame)
	}

	workDir := m.ui.WorkDir
	m.projectSwitcherCursor = index
	if cmd := m.activateProjectSwitcherDestination(m.projectSwitcherFiltered[index]); cmd == nil {
		t.Fatal("activating a refused destination reported nothing")
	} else if _, ok := cmd().(ToastMsg); !ok {
		t.Fatal("activating a refused destination did not explain itself")
	}
	if m.ui.WorkDir != workDir || !m.showProjectSwitcher {
		t.Fatal("a refused destination must not switch or close the switcher")
	}
}

// switcherItemRegion finds the registered hit region for one collection entry.
func projectSwitcherItemRegion(t *testing.T, h *mouse.Handler, index int) mouse.Region {
	t.Helper()
	return sectionBarRegion(t, h, projectSwitcherItemID(index))
}

func mustModel(out tea.Model, cmd tea.Cmd) (Model, tea.Cmd) {
	switch v := out.(type) {
	case Model:
		return v, cmd
	case *Model:
		return *v, cmd
	}
	panic("unexpected model type")
}

func TestProjectSwitcherSortDropdownMouseInteraction(t *testing.T) {
	m := projectSwitcherFixture(t)
	m.openProjectSwitcherSort()

	// Initial render registers hit regions into handler
	_ = m.renderProjectSwitcherOverlay("")
	h := m.projectSwitcherMouseHandler
	if h == nil {
		t.Fatal("mouse handler not initialized")
	}

	// Locate regions: backdrop, sort options, order option, and first project card
	var backdropRegion, opt0Region, opt1Region, orderRegion, card0Region *mouse.Region
	for _, reg := range h.HitMap.Regions() {
		switch {
		case reg.ID == modal.RegionOverlayBackdrop:
			r := reg
			backdropRegion = &r
		case reg.ID == projectSwitcherSortOptionIDFor(0):
			r := reg
			opt0Region = &r
		case reg.ID == projectSwitcherSortOptionIDFor(1):
			r := reg
			opt1Region = &r
		case reg.ID == projectSwitcherSortOrderID:
			r := reg
			orderRegion = &r
		case reg.ID == projectSwitcherItemID(0):
			r := reg
			card0Region = &r
		}
	}

	if backdropRegion == nil {
		t.Fatal("missing RegionOverlayBackdrop hit region")
	}
	if opt0Region == nil {
		t.Fatal("missing sort option 0 hit region")
	}
	if opt1Region == nil {
		t.Fatal("missing sort option 1 hit region")
	}
	if orderRegion == nil {
		t.Fatal("missing sort order hit region")
	}
	if card0Region == nil {
		t.Fatal("missing card 0 hit region")
	}

	// 1. Hover on overlay backdrop (the title / border row of the sort popover):
	// Must NOT hover card 0 (no hover bleed-through!)
	m.handleProjectSwitcherMouse(tea.MouseMotionMsg{X: backdropRegion.Rect.X, Y: backdropRegion.Rect.Y})
	if m.projectSwitcherModal.HoveredID() != "" {
		t.Fatalf("hover on overlay backdrop = %q, want empty (must not bleed to background items)",
			m.projectSwitcherModal.HoveredID())
	}

	// 2. Hover on sort option 1:
	m.handleProjectSwitcherMouse(tea.MouseMotionMsg{X: opt1Region.Rect.X + 1, Y: opt1Region.Rect.Y})
	if got := m.projectSwitcherModal.HoveredID(); got != opt1Region.ID {
		t.Fatalf("hover on sort option 1 = %q, want %q", got, opt1Region.ID)
	}

	// 3. Click sort option 1:
	// Applies sort mode 1 (Name), closes the sort dropdown.
	_, cmd := m.handleProjectSwitcherMouse(tea.MouseClickMsg{
		X:      opt1Region.Rect.X + 1,
		Y:      opt1Region.Rect.Y,
		Button: tea.MouseLeft,
	})
	if cmd != nil {
		cmd()
	}
	if m.projectSwitcherSort != projectlist.SortModes[1] {
		t.Fatalf("sort = %v, want %v", m.projectSwitcherSort, projectlist.SortModes[1])
	}
	if m.projectSwitcherSortOpen {
		t.Fatal("clicking sort option should close the sort popover")
	}

	// 4. Reopen sort dropdown and click order toggle:
	m.openProjectSwitcherSort()
	_ = m.renderProjectSwitcherOverlay("")
	orderRegion = nil
	for _, reg := range m.projectSwitcherMouseHandler.HitMap.Regions() {
		if reg.ID == projectSwitcherSortOrderID {
			r := reg
			orderRegion = &r
			break
		}
	}
	if orderRegion == nil {
		t.Fatal("missing sort order region after reopening sort")
	}
	orderBefore := m.projectSwitcherOrder
	_, cmd = m.handleProjectSwitcherMouse(tea.MouseClickMsg{
		X:      orderRegion.Rect.X + 1,
		Y:      orderRegion.Rect.Y,
		Button: tea.MouseLeft,
	})
	if cmd != nil {
		cmd()
	}
	if m.projectSwitcherOrder == orderBefore {
		t.Fatalf("clicking order did not toggle order from %v", orderBefore)
	}
}

func TestProjectSwitcherSortDropdownClickOutsideDismisses(t *testing.T) {
	m := projectSwitcherFixture(t)
	m.openProjectSwitcherSort()
	_ = m.renderProjectSwitcherOverlay("")

	// Click at (0, 0) outside the sort popover
	m.handleProjectSwitcherMouse(tea.MouseClickMsg{
		X:      0,
		Y:      0,
		Button: tea.MouseLeft,
	})
	if m.projectSwitcherSortOpen {
		t.Fatal("clicking outside sort popover should dismiss it")
	}
}
