package pluginbrowser

// The frame is the one promise a hosted surface makes to the shell: exactly the
// box it was given, whatever a plugin sent. These tests hold the browser to it
// with a modal open, with a maximal declaration, and with cells and documents
// written to break it.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
)

func assertBox(t *testing.T, m *Model, label string, w, h int) {
	t.Helper()
	m.SetSize(w, h)
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Fatalf("%s %dx%d: %d lines", label, w, h, len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != w {
			t.Fatalf("%s %dx%d: line %d is %d cells (%q)", label, w, h, i, got, strip(line))
		}
		if strings.ContainsAny(line, "\t\r") {
			t.Fatalf("%s %dx%d: line %d carries a control byte: %q", label, w, h, i, strip(line))
		}
	}
}

// An overlay composited over the browser must not change the box.
func TestOverlaysStayInsideTheBox(t *testing.T) {
	sizes := [][2]int{{160, 45}, {120, 30}, {100, 24}, {80, 20}, {52, 18}, {40, 12}}

	for _, open := range []struct {
		name string
		do   func(*Model)
	}{
		{"view", func(m *Model) { m.openViewModal() }},
		{"actions", func(m *Model) { m.openActionMenu() }},
		{"form", func(m *Model) { m.startAction("log-note") }},
		{"confirm", func(m *Model) {
			m.active = 1
			s := m.activeState()
			s.items = []pluginhost.Item{{ID: "a", Cells: map[string]string{"name": "notes"}}}
			s.loaded = true
			m.startAction("refresh-source")
		}},
	} {
		for _, size := range sizes {
			host := &fakeHost{page: testPage(6)}
			m := newTestModel(t, host)
			s := m.activeState()
			s.setQuery("dex")
			run(t, m, m.list(m.desc.Collections[0], s, false))
			m.SetSize(size[0], size[1])
			open.do(m)
			if !m.OverlayOpen() {
				continue
			}
			assertBox(t, m, "overlay "+open.name, size[0], size[1])
		}
	}
}

// A plugin's own bytes reach the detail box. None of them may change the frame.
func TestHostileDocumentStaysInsideTheBox(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	host.doc = resource.Document{
		Identity: "rc:notes:1",
		Title:    "tab\there\tand\twide 漢字漢字漢字",
		Subtitle: "sub\ttitle",
		Status:   &resource.Status{Label: strings.Repeat("status ", 12), Tone: resource.ToneWarning},
		Fields: []resource.Field{
			{Label: "Tabbed", Value: "a\tb\tc"},
			{Label: strings.Repeat("long label ", 6), Value: strings.Repeat("v", 400)},
		},
		Body: &resource.Body{Format: resource.FormatText, Text: "one\ttwo\tthree\n" + strings.Repeat("x", 300)},
		Sections: []resource.Section{
			{Title: "Tab\tsection", Items: []resource.TimelineItem{{Title: "t\ta", Text: "b\tc"}}},
			{Title: "Fields", Fields: []resource.Field{{Label: "k\t", Value: "v\t"}}},
		},
	}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))
	run(t, m, m.openDocument("results", "rc:notes:1", openReplace))
	m.focus = FocusDetail

	for _, size := range [][2]int{{160, 45}, {120, 30}, {100, 24}, {80, 20}, {52, 18}, {40, 12}} {
		assertBox(t, m, "hostile document", size[0], size[1])
	}
}

// A plugin's own cells reach the table. None of them may change the frame.
func TestHostileRowsStayInsideTheBox(t *testing.T) {
	page := testPage(5)
	page.Items[0].Cells["title"] = "tab\there"
	page.Items[1].Cells["excerpt"] = strings.Repeat("漢", 120)
	page.Items[2].Cells["source"] = strings.Repeat("s", 300)
	page.Items[3].Status = &resource.Status{Label: strings.Repeat("wide ", 20), Tone: resource.ToneDanger}
	host := &fakeHost{page: page}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))

	for _, size := range [][2]int{{160, 45}, {120, 30}, {100, 24}, {80, 20}, {52, 18}, {40, 12}} {
		assertBox(t, m, "hostile rows", size[0], size[1])
	}
}

// A notice the plugin sent is host-styled text on one row, never more.
func TestNoticesStayOnOneRow(t *testing.T) {
	page := testPage(3)
	page.Notices = []pluginhost.Notice{
		{Tone: resource.ToneWarning, Text: strings.Repeat("notice ", 60)},
		{Tone: resource.ToneInfo, Text: "short"},
	}
	host := &fakeHost{page: page}
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))
	for _, size := range [][2]int{{160, 45}, {80, 20}, {52, 18}} {
		assertBox(t, m, "notices", size[0], size[1])
	}
}

// Paging leaves no collection stuck in a loading state when the user moves on
// before the page lands.
func TestSupersededPageLeavesNoStuckSpinner(t *testing.T) {
	host := &fakeHost{page: testPage(4)}
	host.page.NextCursor = "next"
	m := newTestModel(t, host)
	s := m.activeState()
	s.setQuery("dex")
	run(t, m, m.list(m.desc.Collections[0], s, false))

	c, _ := m.ActiveCollection()
	// Ask for a page, then move the query on before it lands.
	pageCmd := m.list(c, s, true)
	if !s.paging {
		t.Fatal("an append did not mark the collection as paging")
	}
	s.setQuery("dexter")
	run(t, m, m.list(c, s, false))
	run(t, m, pageCmd)
	if s.paging || s.loading {
		t.Fatalf("paging=%v loading=%v after a superseded page", s.paging, s.loading)
	}
}

// A plugin's declarations are bounded by the protocol, but the bounds are
// generous. The modal a maximal declaration opens must still fit the box.
func TestMaximalDeclarationsKeepTheModalInsideTheBox(t *testing.T) {
	host := &fakeHost{page: testPage(6)}
	desc := testDescription()
	for i := 0; i < pluginhost.MaxSortKeys; i++ {
		desc.Collections[0].Sort = append(desc.Collections[0].Sort,
			pluginhost.SortKey{ID: "s" + itoa(i), Label: "Sort key " + itoa(i)})
	}
	for i := 0; i < pluginhost.MaxViews; i++ {
		desc.Collections[0].Views = append(desc.Collections[0].Views,
			pluginhost.View{ID: "v" + itoa(i), Title: "View " + itoa(i)})
	}
	choices := make([]string, 0, pluginhost.MaxActionChoices)
	for i := 0; i < pluginhost.MaxActionChoices; i++ {
		choices = append(choices, "choice-"+itoa(i))
	}
	big := pluginhost.Action{
		ID: "big", Title: "Big", On: pluginhost.ActionOnCollection, Collection: "results", Mutates: true,
	}
	for i := 0; i < pluginhost.MaxActionInputs; i++ {
		big.Inputs = append(big.Inputs, pluginhost.ActionInput{
			ID: "in" + itoa(i), Label: "Input " + itoa(i), Kind: pluginhost.InputMultiline,
		})
	}
	desc.Actions = append(desc.Actions, big)
	for i := 0; i < pluginhost.MaxActions-4; i++ {
		desc.Actions = append(desc.Actions, pluginhost.Action{
			ID: "a" + itoa(i), Title: "Action " + itoa(i),
			On: pluginhost.ActionOnCollection, Collection: "results",
		})
	}
	desc.Actions = append(desc.Actions, pluginhost.Action{
		ID: "choicey", Title: "Choicey", On: pluginhost.ActionOnCollection, Collection: "results",
		Inputs: []pluginhost.ActionInput{{
			ID: "pick", Label: "Pick", Kind: pluginhost.InputChoice, Choices: choices,
		}},
	})
	host.described = true
	host.status = pluginhost.Status{State: pluginhost.StateReady}
	host.desc = desc
	host.project = &pluginhost.ProjectContext{Root: "/tmp/p", WorkDir: "/tmp/p", Name: "p"}

	for _, open := range []struct {
		name string
		do   func(*Model)
	}{
		{"view", func(m *Model) { m.openViewModal() }},
		{"actions", func(m *Model) { m.openActionMenu() }},
		{"form", func(m *Model) { m.startAction("big") }},
		{"choices", func(m *Model) { m.startAction("choicey") }},
	} {
		for _, size := range [][2]int{{160, 45}, {120, 30}, {80, 20}} {
			m := New("fixture", "fixture", host.calls(), nil)
			m.SetSize(size[0], size[1])
			run(t, m, m.Refresh())
			s := m.activeState()
			s.setQuery("dex")
			run(t, m, m.list(m.desc.Collections[0], s, false))
			m.SetSize(size[0], size[1])
			open.do(m)
			if !m.OverlayOpen() {
				t.Fatalf("%s did not open", open.name)
			}
			assertBox(t, m, "maximal "+open.name, size[0], size[1])
		}
	}
}
