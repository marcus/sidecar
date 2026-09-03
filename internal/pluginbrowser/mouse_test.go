package pluginbrowser

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/ui"
)

// loadedModel is a described browser with a page of rows already on screen and
// one frame painted, which is what gives the hit map something to answer with.
func loadedModel(t *testing.T, host *fakeHost) *Model {
	t.Helper()
	m := newTestModel(t, host)
	c, _ := m.ActiveCollection()
	s := m.state(c)
	s.query = "rows"
	run(t, m, m.list(c, s, false))
	m.View()
	return m
}

func regionsWithID(m *Model, id string) []mouse.Region {
	var out []mouse.Region
	for _, region := range m.Regions() {
		if region.ID == id {
			out = append(out, region)
		}
	}
	return out
}

func firstRegion(t *testing.T, m *Model, id string) mouse.Region {
	t.Helper()
	regions := regionsWithID(m, id)
	if len(regions) == 0 {
		t.Fatalf("no %q region in the frame; got %s", id, regionIDs(m))
	}
	return regions[0]
}

func rowRegion(t *testing.T, m *Model, index int) mouse.Region {
	t.Helper()
	for _, region := range regionsWithID(m, regionRow) {
		if got, ok := region.Data.(int); ok && got == index {
			return region
		}
	}
	t.Fatalf("no row region for item %d; got %s", index, regionIDs(m))
	return mouse.Region{}
}

func regionIDs(m *Model) string {
	var ids []string
	for _, region := range m.Regions() {
		ids = append(ids, region.ID)
	}
	return strings.Join(ids, ", ")
}

func click(t *testing.T, m *Model, x, y int) {
	t.Helper()
	run(t, m, m.HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}))
	m.View()
}

func clickRegion(t *testing.T, m *Model, region mouse.Region) {
	t.Helper()
	click(t, m, region.Rect.X, region.Rect.Y)
}

// A click on a row selects it. It does not open it: the first Enter selects
// too, and hover selects nothing at all.
func TestClickOnARowSelectsIt(t *testing.T) {
	host := &fakeHost{page: testPage(12)}
	m := loadedModel(t, host)
	row := rowRegion(t, m, 4)
	clickRegion(t, m, row)

	s := m.activeState()
	if s.cursor != 4 {
		t.Fatalf("cursor = %d, want the clicked row", s.cursor)
	}
	if len(host.gets) != 0 {
		t.Fatalf("a first click opened %d documents", len(host.gets))
	}
}

// A second click on the already-selected row is Enter, and so is a double
// click. Both go through the same method the key does.
func TestSecondClickOnTheSelectedRowOpensIt(t *testing.T) {
	host := &fakeHost{page: testPage(12)}
	m := loadedModel(t, host)
	row := rowRegion(t, m, 2)
	clickRegion(t, m, row)
	clickRegion(t, m, row)

	if len(host.gets) != 1 {
		t.Fatalf("get calls = %d, want the second click to open the row", len(host.gets))
	}
	if got := host.gets[0].Params.ID; got != "rc:notes:3" {
		t.Fatalf("opened %q, want the clicked row", got)
	}
}

// A click inside a box focuses it, through the same SetPaneFocus the app's
// focus ring calls.
func TestClickFocusesTheBoxItLandedIn(t *testing.T) {
	host := &fakeHost{page: testPage(12)}
	m := loadedModel(t, host)
	// Open a document so the detail box is a focus stop at all.
	press(t, m, "enter")
	m.View()

	detail := firstRegion(t, m, regionDetail)
	click(t, m, detail.Rect.X+4, detail.Rect.Y+3)
	if m.PaneFocus() != string(FocusDetail) {
		t.Fatalf("focus = %q after a click in the detail box", m.PaneFocus())
	}

	list := firstRegion(t, m, regionList)
	click(t, m, list.Rect.X+2, list.Rect.Y+m.height-2)
	if m.PaneFocus() != string(FocusList) {
		t.Fatalf("focus = %q after a click in the list box", m.PaneFocus())
	}
}

// A click on the query row is `/`.
func TestClickOnTheQueryRowBeginsEditing(t *testing.T) {
	host := &fakeHost{page: testPage(4)}
	m := loadedModel(t, host)
	if m.ConsumesTextInput() {
		t.Fatal("the query line had the keyboard before anything was clicked")
	}
	clickRegion(t, m, firstRegion(t, m, regionQuery))
	if !m.ConsumesTextInput() {
		t.Fatal("a click on the query row did not begin editing")
	}
	view := strip(m.View())
	if !strings.Contains(view, "▌") {
		t.Fatalf("the focused query row has no block caret:\n%s", view)
	}
}

// A click on the View pill is `v`.
func TestClickOnTheViewPillOpensTheViewModal(t *testing.T) {
	host := &fakeHost{page: testPage(4)}
	m := loadedModel(t, host)
	clickRegion(t, m, firstRegion(t, m, regionPill))
	if m.overlay.kind != overlayView {
		t.Fatalf("overlay = %v, want the View modal", m.overlay.kind)
	}
	// And the pill's rect is the placement, not a guess: its label is on that
	// row at that column.
	pill := firstRegion(t, m, regionPill)
	if pill.Rect.W < 2 {
		t.Fatalf("the pill's hit rect is %d cells wide", pill.Rect.W)
	}
}

// The wheel scrolls the box under the pointer, not the focused one.
func TestWheelScrollsTheBoxUnderThePointer(t *testing.T) {
	host := &fakeHost{page: testPage(40), doc: longDocument()}
	m := loadedModel(t, host)
	press(t, m, "enter")
	press(t, m, "k") // leave the cursor where it started
	m.SetPaneFocus(string(FocusList))
	m.View()

	before := m.detail.scroll
	detail := firstRegion(t, m, regionDetail)
	run(t, m, m.HandleMouse(tea.MouseWheelMsg{
		X: detail.Rect.X + 4, Y: detail.Rect.Y + 3, Button: tea.MouseWheelDown,
	}))
	if m.detail.scroll == before {
		t.Fatalf("a wheel over the unfocused detail box did not scroll it (scroll = %d)", m.detail.scroll)
	}
	if m.PaneFocus() != string(FocusList) {
		t.Fatalf("the wheel moved focus to %q", m.PaneFocus())
	}
	if cursor := m.activeState().cursor; cursor != 0 {
		t.Fatalf("the wheel over the detail box moved the list cursor to %d", cursor)
	}
}

// A press on a scrollbar track jumps to the pressed spot, and never selects the
// row underneath: the bar is registered after the rows precisely so it wins.
func TestScrollbarPressJumps(t *testing.T) {
	host := &fakeHost{page: testPage(200)}
	m := loadedModel(t, host)
	bars := regionsWithID(m, ui.RegionScrollbarTrack)
	if len(bars) == 0 {
		t.Fatalf("no scrollbar over a 200-row page; regions: %s", regionIDs(m))
	}
	track := bars[0]
	press := track.Rect.Y + track.Rect.H - 2
	click(t, m, track.Rect.X, press)

	s := m.activeState()
	if s.scroll == 0 {
		t.Fatalf("a track press did not move the window (scroll = %d)", s.scroll)
	}
	if s.cursor < s.scroll {
		t.Fatalf("the cursor (%d) is above the window (%d)", s.cursor, s.scroll)
	}
	if len(host.gets) != 0 {
		t.Fatal("a scrollbar press opened the row underneath it")
	}
}

// A drag on the rail moves the split, and the value round-trips through state.
func TestRailDragMovesTheSplitAndRoundTrips(t *testing.T) {
	if err := state.InitWithDir(t.TempDir()); err != nil {
		t.Fatalf("state.InitWithDir: %v", err)
	}
	// A dragged split is a preference: leave it behind and every test after
	// this one opens at the width this one chose.
	t.Cleanup(func() { _ = state.InitWithDir(t.TempDir()) })
	host := &fakeHost{page: testPage(12)}
	m := loadedModel(t, host)

	before, _ := m.split()
	rail := firstRegion(t, m, regionRail)
	run(t, m, m.HandleMouse(tea.MouseClickMsg{X: rail.Rect.X + 1, Y: 4, Button: tea.MouseLeft}))
	run(t, m, m.HandleMouse(tea.MouseMotionMsg{X: rail.Rect.X + 1 - 20, Y: 4, Button: tea.MouseLeft}))
	m.View()
	after, _ := m.split()
	if after >= before {
		t.Fatalf("the split did not move left: %d then %d", before, after)
	}
	run(t, m, m.HandleMouse(tea.MouseReleaseMsg{X: rail.Rect.X + 1 - 20, Y: 4, Button: tea.MouseLeft}))

	if got := state.GetPluginBrowserSplit("fixture"); got != m.ListShare() {
		t.Fatalf("saved share = %d, live share = %d", got, m.ListShare())
	}
	// A browser built now reads it back, which is the whole point of saving it.
	next := newTestModel(t, &fakeHost{page: testPage(12)})
	if got := next.ListShare(); got != m.ListShare() {
		t.Fatalf("a new browser opened at %d, want the remembered %d", got, m.ListShare())
	}
	if reopened, _ := next.split(); reopened != after {
		t.Fatalf("a new browser split at %d, want the dragged %d", reopened, after)
	}
}

// The rail's target is wider than its paint, one cell into each neighbour's
// border, exactly as the pane tree's is.
func TestRailHitBoxIsWiderThanItsPaint(t *testing.T) {
	host := &fakeHost{page: testPage(12)}
	m := loadedModel(t, host)
	listOuter, _ := m.split()
	rail := firstRegion(t, m, regionRail)
	if rail.Rect.X != listOuter-1 || rail.Rect.W != paneGap+2 {
		t.Fatalf("rail hit box = %+v, want one cell either side of column %d", rail.Rect, listOuter)
	}
	if !strings.Contains(m.View(), "┃") {
		t.Fatal("the gap between the two boxes is still a blank column, not a rail")
	}
}

// The outcome cell and a notice both open the coverage card, and so does `c`.
// Nothing a click does is reachable only by click.
func TestOutcomeAndNoticeOpenCoverage(t *testing.T) {
	host := &fakeHost{page: pluginhost.Page{
		Outcome: pluginhost.OutcomeDegraded,
		Items:   testPage(3).Items,
		Total:   3,
		Notices: []pluginhost.Notice{{Text: "1 of 13 sources did not answer in time"}},
	}}
	m := loadedModel(t, host)

	clickRegion(t, m, firstRegion(t, m, regionOutcome))
	if m.overlay.kind != overlayCoverage {
		t.Fatalf("overlay = %v after a click on the outcome", m.overlay.kind)
	}
	view := strip(m.View())
	for _, want := range []string{"degraded", "Some source that should have", "did not answer in time"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the coverage card is missing %q:\n%s", want, view)
		}
	}
	m.closeOverlay()
	m.View()

	clickRegion(t, m, firstRegion(t, m, regionNotice))
	if m.overlay.kind != overlayCoverage {
		t.Fatalf("overlay = %v after a click on a notice", m.overlay.kind)
	}
	m.closeOverlay()

	press(t, m, "c")
	if m.overlay.kind != overlayCoverage {
		t.Fatalf("overlay = %v after the c key", m.overlay.kind)
	}
}

// An open overlay takes the pointer, exactly as it takes the keyboard.
func TestAnOpenOverlayTakesThePointer(t *testing.T) {
	host := &fakeHost{page: testPage(12)}
	m := loadedModel(t, host)
	run(t, m, m.openViewModal())
	m.View()

	row := rowRegion(t, m, 4)
	before := m.activeState().cursor
	run(t, m, m.HandleMouse(tea.MouseClickMsg{X: row.Rect.X, Y: row.Rect.Y, Button: tea.MouseLeft}))
	if m.activeState().cursor != before {
		t.Fatal("a click under an open modal reached the list")
	}
}

// Every frame answers for itself: the hit map is cleared and rebuilt, so a row
// that scrolled away leaves no target behind.
func TestHitMapIsRebuiltEveryFrame(t *testing.T) {
	host := &fakeHost{page: testPage(12)}
	m := loadedModel(t, host)
	first := len(m.Regions())
	m.View()
	m.View()
	if got := len(m.Regions()); got != first {
		t.Fatalf("regions grew from %d to %d over three frames", first, got)
	}
}

// longDocument is a card taller than the box, so the detail viewport has
// somewhere to scroll to.
func longDocument() resource.Document {
	fields := make([]resource.Field, 0, 60)
	for i := 0; i < 60; i++ {
		fields = append(fields, resource.Field{Label: "field " + itoa(i), Value: "value " + itoa(i)})
	}
	return resource.Document{Title: "Fixture row", Identity: "rc:notes:1", Fields: fields}
}

// The query row's right-hand cell is a target only when there is a coverage
// card behind it. A page that answered with no notices has none, so those
// columns belong to the query row like every other column on it — a region for
// a control that is not there would be a hole a click falls into.
func TestTheOutcomeCellIsNoTargetWithNothingToExplain(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := loadedModel(t, host)
	if m.hasCoverage() {
		t.Fatal("an answered page with no notices claims there is coverage to show")
	}
	if regions := regionsWithID(m, regionOutcome); len(regions) != 0 {
		t.Fatalf("the outcome cell registered %d target(s) with no card behind it: %+v", len(regions), regions)
	}
	// And the cell is still on the row, so this is the click landing on the
	// query row rather than the summary having disappeared.
	view := strip(m.View())
	if !strings.Contains(view, "answered") {
		t.Fatalf("the outcome word left the query row:\n%s", view)
	}
	query := firstRegion(t, m, regionQuery)
	click(t, m, query.Rect.X+query.Rect.W-2, query.Rect.Y)
	if !m.ConsumesTextInput() {
		t.Fatal("a click on the right-hand end of the query row did not begin editing")
	}
}

// The query-bound hint replaces the count while it stands. It is a message the
// row is making, not a control, so it registers nothing.
func TestTheQueryLimitHintIsNoTarget(t *testing.T) {
	host := &fakeHost{page: pluginhost.Page{
		Outcome: pluginhost.OutcomeDegraded,
		Items:   testPage(3).Items,
		Total:   3,
		Notices: []pluginhost.Notice{{Text: "1 of 13 sources did not answer in time"}},
	}}
	m := loadedModel(t, host)
	if len(regionsWithID(m, regionOutcome)) != 1 {
		t.Fatal("a degraded page with a notice offers no outcome target to lose")
	}
	c, _ := m.ActiveCollection()
	s := m.state(c)
	s.editing = true
	s.query = strings.Repeat("a", resource.MaxQueryChars)
	press(t, m, "b")
	if !s.atLimit {
		t.Fatal("the keystroke past the bound was not refused")
	}
	m.View()
	view := strip(m.View())
	if !strings.Contains(view, "query is as long as Sidecar keeps") {
		t.Fatalf("the refused keystroke said nothing:\n%s", view)
	}
	if regions := regionsWithID(m, regionOutcome); len(regions) != 0 {
		t.Fatalf("the limit hint is registered as the outcome control: %+v", regions)
	}
}

// The rail is not pointer-only. `+` and `-` move the split by the same step
// the file browser and Git move theirs by, through the same clamped setter and
// the same store the drag uses.
func TestResizeKeysMoveTheSplitAndPersistIt(t *testing.T) {
	if err := state.InitWithDir(t.TempDir()); err != nil {
		t.Fatalf("state.InitWithDir: %v", err)
	}
	t.Cleanup(func() { _ = state.InitWithDir(t.TempDir()) })
	m := loadedModel(t, &fakeHost{page: testPage(12)})

	before := m.ListShare()
	step := m.shareStep()
	press(t, m, "+")
	if got := m.ListShare(); got != before+step {
		t.Fatalf("`+` left the share at %d, want %d", got, before+step)
	}
	if got := state.GetPluginBrowserSplit("fixture"); got != m.ListShare() {
		t.Fatalf("saved share = %d, live share = %d", got, m.ListShare())
	}
	press(t, m, "-")
	if got := m.ListShare(); got != before {
		t.Fatalf("`-` left the share at %d, want the %d it started from", got, before)
	}
	if got := state.GetPluginBrowserSplit("fixture"); got != before {
		t.Fatalf("the second press saved %d, want %d", got, before)
	}
}

// The keys stop at the same floors the drag does, and a press the floor refuses
// writes nothing: a preference that did not change is not a preference to save.
func TestResizeKeysClampAtTheFloorsAndSaveNothingThere(t *testing.T) {
	if err := state.InitWithDir(t.TempDir()); err != nil {
		t.Fatalf("state.InitWithDir: %v", err)
	}
	t.Cleanup(func() { _ = state.InitWithDir(t.TempDir()) })
	m := loadedModel(t, &fakeHost{page: testPage(12)})

	lo, hi := m.shareBounds()
	for range 200 {
		press(t, m, "+")
	}
	if got := m.ListShare(); got != hi {
		t.Fatalf("`+` ran to %d, want the ceiling %d", got, hi)
	}
	// At the ceiling the key stays claimed — the rail is still there and the
	// other direction still works — and a press that moves nothing leaves the
	// saved preference exactly where it was.
	press(t, m, "+")
	if got := state.GetPluginBrowserSplit("fixture"); got != hi {
		t.Fatalf("a press at the ceiling left %d saved, want %d", got, hi)
	}
	if !m.ClaimsKey("+") {
		t.Fatal("the browser stopped claiming `+` at the ceiling; the rail is still there")
	}
	for range 200 {
		press(t, m, "-")
	}
	if got := m.ListShare(); got != lo {
		t.Fatalf("`-` ran to %d, want the floor %d", got, lo)
	}
}

// A pane leaf holds one box, whose width the deck sets. There is no rail there,
// so the keys are inert and unclaimed rather than swallowed.
func TestResizeKeysAreUnclaimedInPaneMode(t *testing.T) {
	m := loadedModel(t, &fakeHost{page: testPage(12)})
	m.SetPaneCollection("results")
	m.View()
	if !m.paneMode() {
		t.Fatal("the browser is not in pane mode; this test is about a pane leaf")
	}

	for _, key := range []string{"+", "-"} {
		if m.ClaimsKey(key) {
			t.Fatalf("a pane browser claims %q with no rail to move", key)
		}
		if _, consumed := m.HandleKey(keyPress(key)); consumed {
			t.Fatalf("a pane browser consumed %q with nothing to do", key)
		}
	}
}

// A third rapid press is still a press. Nothing here wants three of them, and
// swallowing it would make a row go dead under a fast hand.
func TestATripleClickOnARowIsStillAClick(t *testing.T) {
	host := &fakeHost{page: testPage(12)}
	m := loadedModel(t, host)
	row := rowRegion(t, m, 2)

	press := func() {
		run(t, m, m.HandleMouse(tea.MouseClickMsg{X: row.Rect.X, Y: row.Rect.Y, Button: tea.MouseLeft}))
		m.View()
	}
	press()
	press()
	if len(host.gets) == 0 {
		t.Fatal("two presses on a row never opened it")
	}
	if m.focus != FocusList {
		t.Fatalf("focus is already at %q; the third press would prove nothing", m.focus)
	}
	// The third press, immediately: the shared handler reports it as a triple,
	// and it is the press that focuses the document already showing that row.
	press()
	if m.focus != FocusDetail {
		t.Fatalf("the third press left focus at %q; it was swallowed", m.focus)
	}
}

// A frame that paints nothing keeps no targets: a browser collapsed to no width
// must not answer for the frame before it.
func TestAZeroSizedFrameClearsItsTargets(t *testing.T) {
	m := loadedModel(t, &fakeHost{page: testPage(12)})
	if len(m.Regions()) == 0 {
		t.Fatal("the painted frame registered nothing to lose")
	}
	m.SetSize(0, 0)
	m.View()
	if regions := m.Regions(); len(regions) != 0 {
		t.Fatalf("a frame that painted nothing left %d targets behind", len(regions))
	}
}

// A drag whose release the window never saw still saves the split: internal
// mouse ends the gesture on the first button-less motion and reports it as
// hover, and that is the only notice this model gets.
func TestALostReleaseStillSavesTheSplit(t *testing.T) {
	if err := state.InitWithDir(t.TempDir()); err != nil {
		t.Fatalf("state.InitWithDir: %v", err)
	}
	t.Cleanup(func() { _ = state.InitWithDir(t.TempDir()) })
	m := loadedModel(t, &fakeHost{page: testPage(12)})

	rail := firstRegion(t, m, regionRail)
	run(t, m, m.HandleMouse(tea.MouseClickMsg{X: rail.Rect.X + 1, Y: 4, Button: tea.MouseLeft}))
	run(t, m, m.HandleMouse(tea.MouseMotionMsg{X: rail.Rect.X + 1 - 20, Y: 4, Button: tea.MouseLeft}))
	m.View()
	dragged := m.ListShare()
	// No release: the next thing that arrives is plain motion.
	run(t, m, m.HandleMouse(tea.MouseMotionMsg{X: rail.Rect.X + 1 - 20, Y: 4, Button: tea.MouseNone}))

	if got := state.GetPluginBrowserSplit("fixture"); got != dragged {
		t.Fatalf("saved share = %d after a lost release, want the dragged %d", got, dragged)
	}
}

// While an overlay is up the wheel is the overlay's. Answering the boundary
// question for the list underneath would let the host drop a notch the modal
// was going to scroll.
func TestTheWheelIsNeverAtABoundaryUnderAnOverlay(t *testing.T) {
	m := loadedModel(t, &fakeHost{page: testPage(3)})
	if !m.ScrollAtBoundaryAt(4, 4, -1) {
		t.Fatal("a list at its top is not reporting a boundary; this test needs one")
	}
	run(t, m, m.openViewModal())
	m.View()
	if !m.OverlayOpen() {
		t.Fatal("the View modal did not open")
	}
	if m.ScrollAtBoundaryAt(4, 4, -1) {
		t.Fatal("the browser answered for the list while an overlay owned the wheel")
	}
}
