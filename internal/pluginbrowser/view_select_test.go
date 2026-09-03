package pluginbrowser

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/pluginhost"
)

// clickOverlay clicks the row a modal.Select registered for id. The region has
// to be the row's own — the section's Tab stop covers the same cells, and a
// click that resolved to it would pick nothing.
func clickOverlay(t *testing.T, m *Model, id string) {
	t.Helper()
	m.View()
	if m.overlay.mouse == nil {
		t.Fatal("the open overlay has no mouse handler")
	}
	for _, region := range m.overlay.mouse.HitMap.Regions() {
		if region.ID != id {
			continue
		}
		hit := m.overlay.mouse.HitMap.Test(region.Rect.X, region.Rect.Y)
		if hit == nil || hit.ID != id {
			t.Fatalf("hit-test on %s = %v, want that row rather than the control", id, hit)
		}
		run(t, m, m.HandleMouse(tea.MouseClickMsg{X: region.Rect.X, Y: region.Rect.Y, Button: tea.MouseLeft}))
		m.View()
		return
	}
	var ids []string
	for _, region := range m.overlay.mouse.HitMap.Regions() {
		ids = append(ids, region.ID)
	}
	t.Fatalf("no hit region for %q in the open modal; got %v", id, ids)
}

// The View modal's selectors are modal.Select: a sort is picked with the
// keyboard, a filter with the pointer, and either way the modal closes having
// applied exactly what was chosen.
func TestViewModalSelectorsAnswerKeyAndClick(t *testing.T) {
	host := &fakeHost{page: testPage(4)}
	m := newFilteredModel(t, host)

	press(t, m, "v")
	if m.overlay.kind != overlayView {
		t.Fatalf("v opened %v, want the View modal", m.overlay.kind)
	}
	m.View()
	if got := m.overlay.box.FocusedID(); got != viewSortListID {
		t.Fatalf("the modal opened with focus on %q, want the sort selector", got)
	}

	// By key: the sort selector moves and Enter applies.
	press(t, m, "j")
	press(t, m, "enter")
	if got := m.activeState().sortKey; got != "recency" {
		t.Fatalf("sort after j+enter = %q, want recency", got)
	}
	if m.overlay.open() {
		t.Fatal("picking a sort left the modal open")
	}

	// By click: a filter choice resolves inside the section, with no host glue.
	press(t, m, "v")
	clickOverlay(t, m, filterChoicePfx+"scope:project")
	if got := lastList(t, host).Params.Filters["scope"]; got != "project" {
		t.Fatalf("filters after the click = %v, want scope=project", lastList(t, host).Params.Filters)
	}
	if m.overlay.open() {
		t.Fatal("picking a filter left the modal open")
	}

	// And Esc closes it having changed nothing further.
	press(t, m, "v")
	before := len(host.lists)
	press(t, m, "esc")
	if m.overlay.open() {
		t.Fatal("esc left the modal open")
	}
	if len(host.lists) != before {
		t.Fatalf("esc relisted %d times", len(host.lists)-before)
	}
}

// A filter with many choices does not push the rest of the modal off the box:
// the selector keeps its own window and scrolls, saying so with the markers,
// and a row below the fold is still reachable once the selection moves onto it.
func TestManyChoiceFilterScrollsInsideItsWindow(t *testing.T) {
	host := &fakeHost{page: testPage(4)}
	m := newFilteredModel(t, host)
	desc := filteredDescription()
	choices := make([]pluginhost.FilterOption, 0, 20)
	for i := 0; i < 20; i++ {
		id := "src" + itoa(i)
		choices = append(choices, pluginhost.FilterOption{ID: id, Title: "source " + itoa(i)})
	}
	desc.Collections[0].Filters[1] = pluginhost.Filter{
		ID: "source", Label: "Source", Kind: pluginhost.FilterChoice,
		Default: "src0", Choices: choices,
	}
	host.desc = desc
	run(t, m, m.Refresh())
	s := m.activeState()
	s.query = "dex"
	run(t, m, m.list(m.desc.Collections[0], s, false))

	press(t, m, "v")
	view := strip(m.View())
	if !strings.Contains(view, "↓ more below") {
		t.Fatalf("a twenty-choice filter did not scroll:\n%s", view)
	}
	if strings.Contains(view, "source 19") {
		t.Fatalf("the twenty-choice filter drew every row:\n%s", view)
	}
	// Only the window's rows are clickable; the rest are not on screen.
	if regionForID(m, filterChoicePfx+"source:src19") != nil {
		t.Fatal("a choice below the fold registered a hit region")
	}

	// Move the selection down into the tail: the window follows and the rows
	// that scrolled in become the clickable ones.
	for i := 0; i < 20 && m.overlay.box.FocusedID() != filterChoicePfx+"source"; i++ {
		press(t, m, "tab")
		m.View()
	}
	if got := m.overlay.box.FocusedID(); got != filterChoicePfx+"source" {
		t.Fatalf("the many-choice filter is not reachable by keyboard; focus = %q", got)
	}
	for i := 0; i < 19; i++ {
		press(t, m, "j")
	}
	view = strip(m.View())
	if !strings.Contains(view, "↑ more above") || strings.Contains(view, "↓ more below") {
		t.Fatalf("at the end of the list the markers are wrong:\n%s", view)
	}
	clickOverlay(t, m, filterChoicePfx+"source:src19")
	if got := lastList(t, host).Params.Filters["source"]; got != "src19" {
		t.Fatalf("filters = %v, want source=src19", lastList(t, host).Params.Filters)
	}
}

func regionForID(m *Model, id string) *mouse.Region {
	if m.overlay.mouse == nil {
		return nil
	}
	for _, region := range m.overlay.mouse.HitMap.Regions() {
		if region.ID == id {
			copied := region
			return &copied
		}
	}
	return nil
}
