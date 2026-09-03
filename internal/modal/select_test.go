package modal

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/styles"
)

func selectItems(n int) []SelectItem {
	items := make([]SelectItem, n)
	for i := range items {
		items[i] = SelectItem{
			ID:          fmt.Sprintf("choice-%d", i),
			Label:       fmt.Sprintf("choice %d", i),
			Description: fmt.Sprintf("the %dth thing", i),
		}
	}
	return items
}

// renderSelect renders one Select on its own, the way a section is rendered
// inside a modal body.
func renderSelect(s Section, width int, focusID, hoverID string) RenderedSection {
	return s.Render(width, focusID, hoverID)
}

// listBody is the list shape's own rows with the box taken off: the first and
// last lines are the border, and every line between them carries a border cell
// at either end. Tests about rows read this; tests about the box read the
// content itself.
func listBody(t *testing.T, content string) []string {
	t.Helper()
	lines := strings.Split(ansi.Strip(content), "\n")
	if len(lines) < 3 {
		t.Fatalf("the list shape draws a border around its rows, got:\n%s", ansi.Strip(content))
	}
	if !strings.HasPrefix(lines[0], "╭") || !strings.HasPrefix(lines[len(lines)-1], "╰") {
		t.Fatalf("the list shape is not bordered:\n%s", ansi.Strip(content))
	}
	rows := make([]string, 0, len(lines)-2)
	for _, line := range lines[1 : len(lines)-1] {
		rows = append(rows, strings.TrimSuffix(strings.TrimPrefix(line, "│"), "│"))
	}
	return rows
}

// Four choices are a row of segments; five are a list. The threshold is the
// point at which a row of segments stops being readable, and it is the same
// number wherever a Select is used.
func TestSelectShapeFollowsItemCount(t *testing.T) {
	var idx int
	four := ansi.Strip(renderSelect(Select("s", selectItems(4), &idx), 80, "s", "").Content)
	if !strings.HasPrefix(four, selectFrameOpen) || !strings.Contains(four, selectSeparator) {
		t.Fatalf("four choices did not draw as a segmented toggle:\n%s", four)
	}
	if strings.Contains(four, "\n") {
		t.Fatalf("the segmented shape is one line:\n%s", four)
	}

	five := renderSelect(Select("s", selectItems(5), &idx), 80, "s", "").Content
	lines := listBody(t, five)
	if len(lines) != 5 {
		t.Fatalf("five choices drew %d rows, want one row each:\n%s", len(lines), ansi.Strip(five))
	}
	if !strings.HasPrefix(strings.TrimLeft(lines[0], " "), "❯ ") {
		t.Fatalf("the list shape points at the selection with ❯:\n%s", ansi.Strip(five))
	}
	if !strings.Contains(lines[0], "the 0th thing") {
		t.Fatalf("the list shape draws the description column:\n%s", ansi.Strip(five))
	}
}

// WithShape overrides the count, in both directions.
func TestSelectShapeCanBeForced(t *testing.T) {
	var idx int
	list := renderSelect(Select("s", selectItems(3), &idx, WithShape(ShapeList)), 80, "s", "").Content
	if rows := listBody(t, list); len(rows) != 3 {
		t.Fatalf("WithShape(ShapeList) did not draw a list:\n%s", ansi.Strip(list))
	}
	seg := ansi.Strip(renderSelect(Select("s", selectItems(8), &idx, WithShape(ShapeSegmented)), 200, "s", "").Content)
	if strings.Contains(seg, "\n") {
		t.Fatalf("WithShape(ShapeSegmented) did not draw a toggle:\n%s", seg)
	}
}

// The count rule has a width floor under it: a toggle that cannot fit is a
// truncated stub, and the list always fits.
func TestSelectFallsBackToTheListWhenSegmentsDoNotFit(t *testing.T) {
	var idx int
	narrow := ansi.Strip(renderSelect(Select("s", selectItems(4), &idx), 20, "s", "").Content)
	if !strings.Contains(narrow, "\n") {
		t.Fatalf("a toggle too wide for its column stayed segmented:\n%s", narrow)
	}
}

// The width a segmented control reports is the width it draws. It was not:
// styles.Button pads every segment by two columns either side, and leaving that
// out of the measurement is what let the View modal keep a control that did not
// fit and truncate its last segment into "Up…".
func TestSelectSegmentedWidthIsTheWidthItDraws(t *testing.T) {
	var idx int
	items := []SelectItem{
		{ID: "a", Label: "Relevance"},
		{ID: "b", Label: "Source"},
		{ID: "c", Label: "Updated"},
	}
	s := Select("s", items, &idx).(*selectSection)
	natural := s.NaturalWidth()
	if natural != s.segmentedWidth() {
		t.Fatalf("a segmented control's natural width %d is not its drawn width %d", natural, s.segmentedWidth())
	}
	// Rendered with room to spare, so nothing truncates and the measurement is
	// compared against the whole control.
	drawn := ansi.StringWidth(ansi.Strip(renderSelect(s, 200, "s", "").Content))
	if drawn != natural {
		t.Fatalf("the toggle drew %d columns, reported %d", drawn, natural)
	}
	// Given exactly its natural width it stays segmented and whole.
	fitted := ansi.Strip(renderSelect(s, natural, "s", "").Content)
	if strings.Contains(fitted, "…") || strings.Contains(fitted, "\n") {
		t.Fatalf("the toggle at its own natural width %d was not drawn whole:\n%s", natural, fitted)
	}
	if !strings.Contains(fitted, "Updated") {
		t.Fatalf("the last segment is missing at the natural width:\n%s", fitted)
	}
}

// The list shape fills whatever column it is handed, so it asks for no width of
// its own and cannot widen a modal on its own account.
func TestSelectListShapeAsksForNoWidth(t *testing.T) {
	var idx int
	if w := Select("s", selectItems(3), &idx, WithShape(ShapeList)).(*selectSection).NaturalWidth(); w != 0 {
		t.Fatalf("a forced list asked for %d columns", w)
	}
	if w := Select("s", selectItems(9), &idx).(*selectSection).NaturalWidth(); w != 0 {
		t.Fatalf("a nine-choice list asked for %d columns", w)
	}
	if w := Select("s", nil, &idx).(*selectSection).NaturalWidth(); w != 0 {
		t.Fatalf("an empty control asked for %d columns", w)
	}
}

// WidthForSections is what a host sizes a modal by: the widest control, the
// box's border and padding, and the column a scrollbar takes once the body is
// taller than the surface. A When wrapper whose condition is false hides its
// control from the measurement, exactly as it hides it from the box.
func TestWidthForSectionsSizesToTheWidestControl(t *testing.T) {
	var idx int
	narrow := Select("n", []SelectItem{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}, &idx)
	wide := Select("w", []SelectItem{
		{ID: "a", Label: "Relevance"}, {ID: "b", Label: "Source"}, {ID: "c", Label: "Updated"},
	}, &idx)

	want := wide.(*selectSection).segmentedWidth() + ModalPadding + ScrollbarColumns
	if got := WidthForSections(Text("hello"), narrow, Spacer(), wide); got != want {
		t.Fatalf("WidthForSections = %d, want the widest control %d", got, want)
	}
	if got := WidthForSections(Text("hello"), Spacer()); got != 0 {
		t.Fatalf("a modal of sections that ask for nothing wanted %d columns", got)
	}
	hidden := When(func() bool { return false }, wide)
	if got := WidthForSections(narrow, hidden); got >= want {
		t.Fatalf("a hidden control widened the modal to %d", got)
	}
	shown := When(func() bool { return true }, wide)
	if got := WidthForSections(narrow, shown); got != want {
		t.Fatalf("a shown control inside When reported %d, want %d", got, want)
	}
}

// Even at the width a host could give it, a control whose segments do not fit
// becomes a list rather than a stub — the floor under the count rule, now that
// the measurement it is compared against is the real one.
func TestSelectFallsBackToTheListAtTheCappedWidth(t *testing.T) {
	var idx int
	items := []SelectItem{
		{ID: "a", Label: "Relevance"},
		{ID: "b", Label: "Source"},
		{ID: "c", Label: "Updated"},
	}
	s := Select("s", items, &idx)
	natural := s.(*selectSection).NaturalWidth()

	whole := ansi.Strip(renderSelect(s, natural, "s", "").Content)
	if strings.Contains(whole, "\n") {
		t.Fatalf("the toggle fell back to the list at a width that fits it:\n%s", whole)
	}
	// One column short of what it needs is the case the screenshot showed. It
	// must become a list, and no segment may be truncated on the way.
	capped := ansi.Strip(renderSelect(s, natural-1, "s", "").Content)
	if !strings.Contains(capped, "\n") {
		t.Fatalf("a toggle one column too wide stayed segmented:\n%s", capped)
	}
	if strings.Contains(capped, "…") {
		t.Fatalf("the fallback truncated a label instead of listing it:\n%s", capped)
	}
	for _, label := range []string{"Relevance", "Source", "Updated"} {
		if !strings.Contains(capped, label) {
			t.Fatalf("the list fallback dropped %q:\n%s", label, capped)
		}
	}
}

// The list shape is a bordered control, and the border is the ladder a modal
// input's border uses: idle, hovered, focused. Without it a modal holding three
// selectors paints three gold rows and says nothing about which one the
// keyboard is on.
func TestSelectListBorderFollowsFocus(t *testing.T) {
	if got, want := selectListBorderStyle(true, false).GetBorderTopForeground(), styles.Primary; got != want {
		t.Fatalf("focused border = %v, want Primary %v", got, want)
	}
	if got, want := selectListBorderStyle(false, true).GetBorderTopForeground(), styles.TextMuted; got != want {
		t.Fatalf("hovered border = %v, want TextMuted %v", got, want)
	}
	if got, want := selectListBorderStyle(false, false).GetBorderTopForeground(), styles.BorderNormal; got != want {
		t.Fatalf("idle border = %v, want BorderNormal %v", got, want)
	}

	var idx int
	s := Select("s", selectItems(6), &idx)
	idle := renderSelect(s, 64, "", "").Content
	focused := renderSelect(s, 64, "s", "").Content
	hovered := renderSelect(s, 64, "", "s").Content
	// Hovering one ROW lights the control's border too: the pointer is on the
	// control either way.
	rowHovered := renderSelect(s, 64, "", "choice-3").Content

	firstLine := func(content string) string { return strings.SplitN(content, "\n", 2)[0] }
	if firstLine(idle) == firstLine(focused) {
		t.Fatal("the focused and idle lists drew the same border")
	}
	if firstLine(hovered) == firstLine(idle) {
		t.Fatal("the hovered list drew the idle border")
	}
	if firstLine(rowHovered) != firstLine(hovered) {
		t.Fatal("hovering a row did not light the control's own border")
	}
	// The selected row keeps its fill whether or not the control has focus: the
	// fill says which choice is active, the border says where the keyboard is.
	for name, content := range map[string]string{"idle": idle, "focused": focused} {
		if !strings.Contains(ansi.Strip(content), "❯ choice 0") {
			t.Fatalf("the %s list lost its selection cursor:\n%s", name, ansi.Strip(content))
		}
	}
	if got, want := selectRowStyle(false, true, false).GetBackground(), styles.ButtonFocused.GetBackground(); got != want {
		t.Fatalf("selected row background = %v, want the Primary fill %v regardless of focus", got, want)
	}
}

// The border is chrome, not a choice: a click on a border cell focuses the
// control and moves nothing. The rows sit inside it, so their hit regions moved
// with them.
func TestSelectListBorderIsNotARow(t *testing.T) {
	idx := 2
	s := Select("s", selectItems(6), &idx)
	m := New("Choose", WithWidth(50)).AddSection(s)
	handler := mouse.NewHandler()
	m.Render(90, 40, handler)

	var section, row mouse.Rect
	for _, region := range handler.HitMap.Regions() {
		switch region.ID {
		case "s":
			section = region.Rect
		case "choice-0":
			row = region.Rect
		}
	}
	if section.W == 0 || row.W == 0 {
		t.Fatal("the control registered no section or row region")
	}
	if row.X != section.X+1 || row.Y != section.Y+1 {
		t.Fatalf("row 0 at (%d,%d), want it inside the border at (%d,%d)", row.X, row.Y, section.X+1, section.Y+1)
	}
	if row.W != section.W-2 {
		t.Fatalf("row width %d, want the box less its two border cells %d", row.W, section.W-2)
	}

	// The top-left border cell belongs to the control's own Tab stop.
	action := m.HandleMouse(tea.MouseClickMsg{X: section.X, Y: section.Y, Button: tea.MouseLeft}, handler)
	if idx != 2 {
		t.Fatalf("a click on the border selected row %d", idx)
	}
	if action != "s" {
		t.Fatalf("a border click returned %q, want the control's own ID", action)
	}
	if m.FocusedID() != "s" {
		t.Fatalf("a border click left focus on %q", m.FocusedID())
	}
}

// Every row of the list reaches the same right edge. The rows are drawn with a
// filled background, so a row sized to its own text leaves the list with an
// edge whose shape is an accident of the longest description.
func TestSelectListRowsFillTheContentColumn(t *testing.T) {
	var idx int
	const contentWidth = 64
	out := renderSelect(Select("s", selectItems(6), &idx), contentWidth, "s", "").Content
	for _, line := range strings.Split(out, "\n") {
		if got := ansi.StringWidth(line); got != contentWidth {
			t.Fatalf("row width = %d, want the full content column %d: %q", got, contentWidth, ansi.Strip(line))
		}
	}
}

// A disabled row stays visible with its reason in place of its description,
// muted on the list's own fill: the list is one block, and a row that dropped
// the fill would read as a hole in it rather than as an unavailable choice.
func TestSelectDisabledRowKeepsItsPlaceAndSaysWhy(t *testing.T) {
	if got, want := selectRowStyle(true, false, false).GetBackground(), styles.Button.GetBackground(); got != want {
		t.Fatalf("disabled row background = %v, want the list's own %v", got, want)
	}
	if got, want := selectRowStyle(true, false, false).GetForeground(), styles.TextMuted; got != want {
		t.Fatalf("disabled row foreground = %v, want muted %v", got, want)
	}
	// A disabled row that is still the selection keeps the selected chrome, or
	// the control would show nothing selected at all.
	if got, want := selectDisabledSelected().GetBackground(), styles.ButtonHover.GetBackground(); got != want {
		t.Fatalf("selected-disabled background = %v, want the selected row's %v", got, want)
	}
	if got, want := selectDisabledSelected().GetForeground(), styles.TextMuted; got != want {
		t.Fatalf("selected-disabled foreground = %v, want muted %v", got, want)
	}

	const reason = "not while two are already on screen"
	const contentWidth = 64
	var idx int
	s := Select("s", selectItems(6), &idx, WithDisabled(func(i int) string {
		if i == 2 {
			return reason
		}
		return ""
	}))
	out := renderSelect(s, contentWidth, "s", "").Content
	if !strings.Contains(ansi.Strip(out), reason) {
		t.Fatalf("the disabled row does not say why:\n%s", ansi.Strip(out))
	}
	if strings.Contains(ansi.Strip(out), "the 2th thing") {
		t.Fatal("the disabled row kept its description instead of the reason")
	}
	for _, line := range strings.Split(out, "\n") {
		if got := ansi.StringWidth(line); got != contentWidth {
			t.Fatalf("row width = %d, want %d even with a disabled row: %q", got, contentWidth, ansi.Strip(line))
		}
	}
}

// A disabled row cannot be reached by key or taken by click: the keyboard
// steps over it, and a click on it leaves the selection where it was.
func TestSelectDisabledRowIsNotSelectable(t *testing.T) {
	idx := 1
	s := Select("s", selectItems(6), &idx, WithDisabled(func(i int) string {
		if i == 2 {
			return "not now"
		}
		return ""
	}))
	s.Render(64, "s", "")
	s.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, "s")
	if idx != 3 {
		t.Fatalf("j from row 1 landed on %d, want it to step over the disabled row onto 3", idx)
	}
	s.Update(tea.KeyPressMsg{Code: 'k', Text: "k"}, "s")
	if idx != 1 {
		t.Fatalf("k from row 3 landed on %d, want 1", idx)
	}
	if action, _ := s.Update(overlayClickMsg{id: "choice-2"}, ""); action != actionOverlayIdle {
		t.Fatalf("clicking a disabled row returned %q, want the idle action", action)
	}
	if idx != 1 {
		t.Fatalf("clicking a disabled row moved the selection to %d", idx)
	}
}

// Movement stops at the ends rather than wrapping: the ends of a list are
// easier to feel than to count, and a wrap reads as a lost keypress.
func TestSelectDoesNotWrapAtTheEnds(t *testing.T) {
	idx := 0
	s := Select("s", selectItems(6), &idx)
	s.Render(64, "s", "")
	s.Update(tea.KeyPressMsg{Code: 'k', Text: "k"}, "s")
	if idx != 0 {
		t.Fatalf("k at the top wrapped to %d", idx)
	}
	s.Update(tea.KeyPressMsg{Code: tea.KeyEnd}, "s")
	if idx != 5 {
		t.Fatalf("end landed on %d, want the last row", idx)
	}
	s.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, "s")
	if idx != 5 {
		t.Fatalf("j at the bottom wrapped to %d", idx)
	}
	s.Update(tea.KeyPressMsg{Code: tea.KeyHome}, "s")
	if idx != 0 {
		t.Fatalf("home landed on %d, want the first row", idx)
	}
	// The segmented shape is steered with the same keys, sideways.
	idx = 0
	seg := Select("s", selectItems(3), &idx)
	seg.Render(64, "s", "")
	seg.Update(tea.KeyPressMsg{Code: 'h', Text: "h"}, "s")
	if idx != 0 {
		t.Fatalf("h at the left end wrapped to %d", idx)
	}
	seg.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}, "s")
	if idx != 1 {
		t.Fatalf("l moved to %d, want 1", idx)
	}
}

// Twenty choices in a window of six scroll, and the markers say which way
// there is more.
func TestSelectScrollsWithMarkers(t *testing.T) {
	idx := 0
	s := Select("s", selectItems(20), &idx, WithMaxVisible(6))
	// The markers are inside the box with the rows they belong to: a control
	// that said "more below" outside its own border would be pointing at the
	// modal rather than at itself.
	out := s.Render(64, "s", "").Content
	lines := listBody(t, out)
	if len(lines) != 7 {
		t.Fatalf("six visible rows plus one marker = 7 rows, got %d:\n%s", len(lines), ansi.Strip(out))
	}
	if !strings.Contains(lines[len(lines)-1], "↓ more below") {
		t.Fatalf("no more-below marker at the top of a 20-row list:\n%s", ansi.Strip(out))
	}
	if strings.Contains(ansi.Strip(out), "↑ more above") {
		t.Fatalf("a more-above marker at the top of the list:\n%s", ansi.Strip(out))
	}

	idx = 12
	out = s.Render(64, "s", "").Content
	lines = listBody(t, out)
	if len(lines) != 8 {
		t.Fatalf("mid-list draws both markers around six rows, got %d rows:\n%s", len(lines), ansi.Strip(out))
	}
	if !strings.Contains(lines[0], "↑ more above") || !strings.Contains(lines[len(lines)-1], "↓ more below") {
		t.Fatalf("mid-list is missing a marker:\n%s", ansi.Strip(out))
	}
	if !strings.Contains(ansi.Strip(out), "choice 12") {
		t.Fatalf("the selection scrolled out of its own window:\n%s", ansi.Strip(out))
	}

	idx = 19
	out = ansi.Strip(s.Render(64, "s", "").Content)
	if strings.Contains(out, "↓ more below") {
		t.Fatalf("a more-below marker at the end of the list:\n%s", out)
	}
	if !strings.Contains(out, "choice 19") {
		t.Fatalf("the last row is not visible at the end of the list:\n%s", out)
	}
}

// A click resolves to the row it was over, inside the section, with no help
// from the host — including after the list has scrolled.
func TestSelectClickResolvesToARow(t *testing.T) {
	idx := 0
	items := selectItems(20)
	s := Select("s", items, &idx, WithMaxVisible(6))
	m := New("Choose", WithWidth(50)).AddSection(s)
	handler := mouse.NewHandler()
	m.Render(90, 40, handler)

	click := func(t *testing.T, id string) string {
		t.Helper()
		for _, region := range handler.HitMap.Regions() {
			if region.ID != id {
				continue
			}
			return m.HandleMouse(tea.MouseClickMsg{X: region.Rect.X + 1, Y: region.Rect.Y, Button: tea.MouseLeft}, handler)
		}
		t.Fatalf("no hit region for %q", id)
		return ""
	}

	if got := click(t, "choice-3"); got != "choice-3" {
		t.Fatalf("clicking row 3 returned %q", got)
	}
	if idx != 3 {
		t.Fatalf("clicking row 3 selected %d", idx)
	}
	// A click on a row also focuses the control it belongs to, so the arrows
	// that follow steer what the pointer just used.
	if got := m.FocusedID(); got != "s" {
		t.Fatalf("focus after a row click = %q, want the control", got)
	}

	// Scroll the window down and click a row that only exists there.
	idx = 19
	handler = mouse.NewHandler()
	m.Render(90, 40, handler)
	if got := click(t, "choice-16"); got != "choice-16" {
		t.Fatalf("clicking a scrolled row returned %q", got)
	}
	if idx != 16 {
		t.Fatalf("clicking a scrolled row selected %d, want 16", idx)
	}
	// The rows that scrolled out of the window own nothing on screen.
	for _, region := range handler.HitMap.Regions() {
		if region.ID == "choice-0" {
			t.Fatal("a row outside the visible window still registered a hit region")
		}
	}
}

// The segmented shape resolves a click the same way: each segment owns its own
// columns, the separator belongs to the segment on its left, and the ends
// reach the frame and the right edge so no click near the control misses.
func TestSelectSegmentedClickResolvesToASegment(t *testing.T) {
	idx := 0
	s := Select("s", selectItems(3), &idx)
	m := New("Choose", WithWidth(60)).AddSection(s)
	handler := mouse.NewHandler()
	m.Render(90, 40, handler)

	regions := map[string]mouse.Rect{}
	for _, region := range handler.HitMap.Regions() {
		regions[region.ID] = region.Rect
	}
	first, ok := regions["choice-0"]
	if !ok {
		t.Fatal("no hit region for the first segment")
	}
	section, ok := regions["s"]
	if !ok {
		t.Fatal("the control registered no Tab stop of its own")
	}
	if first.X != section.X {
		t.Fatalf("the first segment starts at %d, want the control's own left edge %d so a click on the frame keeps it", first.X, section.X)
	}
	last := regions["choice-2"]
	if last.X+last.W != section.X+section.W {
		t.Fatalf("the last segment ends at %d, want the content edge %d", last.X+last.W, section.X+section.W)
	}

	action := m.HandleMouse(tea.MouseClickMsg{X: last.X + 1, Y: last.Y, Button: tea.MouseLeft}, handler)
	if action != "choice-2" || idx != 2 {
		t.Fatalf("clicking the last segment = %q / index %d, want choice-2 / 2", action, idx)
	}
}

// WithSelectAction lets a selector inside a form report the control rather
// than the row, which is what a host with no branch per row needs.
func TestSelectActionOverridesTheRowID(t *testing.T) {
	idx := 0
	var changed []int
	s := Select("s", selectItems(3), &idx,
		WithSelectAction("the-control"),
		WithOnSelect(func(i int) { changed = append(changed, i) }))
	s.Render(60, "s", "")
	action, _ := s.Update(overlayClickMsg{id: "choice-1"}, "")
	if action != "the-control" {
		t.Fatalf("click action = %q, want the control's own", action)
	}
	if idx != 1 || len(changed) != 1 || changed[0] != 1 {
		t.Fatalf("selection = %d, changes = %v, want row 1 reported once", idx, changed)
	}
	// A key that moves the selection reports it too.
	s.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}, "s")
	if idx != 2 || len(changed) != 2 {
		t.Fatalf("selection = %d, changes = %v, want the key change reported", idx, changed)
	}
	// A key that cannot move reports nothing.
	s.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}, "s")
	if len(changed) != 2 {
		t.Fatalf("a blocked move reported a change: %v", changed)
	}
}

// The frame says whether the control has focus, in the same colours a modal
// input border uses, so "this control is active" is not the same signal as
// "this choice is selected".
func TestSelectFrameFollowsFocus(t *testing.T) {
	if got, want := selectFrameStyle(true, false).GetForeground(), styles.Primary; got != want {
		t.Fatalf("focused frame = %v, want Primary %v", got, want)
	}
	if got, want := selectFrameStyle(true, true).GetForeground(), styles.Primary; got != want {
		t.Fatalf("focused+hovered frame = %v, want Primary still %v", got, want)
	}
	if got, want := selectFrameStyle(false, true).GetForeground(), styles.TextMuted; got != want {
		t.Fatalf("hovered frame = %v, want TextMuted %v", got, want)
	}
	if got, want := selectFrameStyle(false, false).GetForeground(), styles.BorderNormal; got != want {
		t.Fatalf("idle frame = %v, want BorderNormal %v", got, want)
	}

	var idx int
	s := Select("s", selectItems(3), &idx)
	idleFrame := renderSelect(s, 80, "", "").Content
	focusedFrame := renderSelect(s, 80, "s", "").Content
	if idleFrame == focusedFrame {
		t.Fatal("focused and idle toggles rendered identically")
	}
	if !strings.Contains(idleFrame, selectFrameStyle(false, false).Render(selectFrameOpen)) {
		t.Fatalf("idle toggle missing the idle frame:\n%s", idleFrame)
	}
	if !strings.Contains(focusedFrame, selectFrameStyle(true, false).Render(selectFrameOpen)) {
		t.Fatalf("focused toggle missing the focused frame:\n%s", focusedFrame)
	}
	// The selection is lit whether or not the control has focus.
	if !strings.Contains(idleFrame, styles.ButtonFocused.Render(" choice 0 ")) {
		t.Fatalf("the unfocused toggle dropped the selected segment:\n%s", idleFrame)
	}
}

// The row styles are the segmented control's: selected wins over hover, and an
// idle row is the plain button fill.
func TestSelectRowStyles(t *testing.T) {
	assertSelectStyle(t, "selected", selectRowStyle(false, true, false), styles.ButtonFocused)
	assertSelectStyle(t, "idle", selectRowStyle(false, false, false), styles.Button)
	assertSelectStyle(t, "selected while hovered", selectRowStyle(false, true, true), styles.ButtonFocused)
	assertSelectStyle(t, "hover", selectRowStyle(false, false, true), styles.ButtonHover)
}

func assertSelectStyle(t *testing.T, name string, got, want lipgloss.Style) {
	t.Helper()
	if fmt.Sprint(got.GetBackground()) != fmt.Sprint(want.GetBackground()) || got.GetBold() != want.GetBold() {
		t.Fatalf("%s: background/bold = %v/%v, want %v/%v",
			name, got.GetBackground(), got.GetBold(), want.GetBackground(), want.GetBold())
	}
}
