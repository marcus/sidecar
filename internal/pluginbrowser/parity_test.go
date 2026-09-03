package pluginbrowser

import (
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
)

// A control key with nothing behind it is inert AND unclaimed, so it falls
// through to whatever the host binds instead of being swallowed (td-fcb648).
func TestControlKeysAreUnclaimedWhereTheyDoNothing(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	host.described = true
	host.status = pluginhost.Status{Instance: "fixture", State: pluginhost.StateReady}
	host.desc = pluginhost.Description{
		Info: pluginhost.Info{Kind: "fixture", Name: "Fixture"},
		Collections: []pluginhost.Collection{{
			ID: "plain", Title: "Plain", Search: pluginhost.SearchNone,
			Columns: []pluginhost.Column{{ID: "name", Label: "Name", Primary: true}},
		}},
	}
	m := New("fixture", "fixture", host.calls(), nil)
	m.SetSize(160, 45)
	run(t, m, m.Refresh())

	for _, key := range []string{"/", "v", "a"} {
		if m.ClaimsKey(key) {
			t.Fatalf("the browser claims %q on a collection that declares nothing for it", key)
		}
		if _, consumed := m.HandleKey(keyPress(key)); consumed {
			t.Fatalf("the browser consumed %q with nothing to do", key)
		}
	}
	// And the keys it can act on are still claimed.
	for _, key := range []string{"j", "enter", "r"} {
		if !m.ClaimsKey(key) {
			t.Fatalf("the browser stopped claiming %q", key)
		}
	}
}

// A selection with no applicable action says nothing at all. The Actions hint
// is already absent, and the design language names a "no action here" line as a
// design failure rather than a courtesy.
func TestNoApplicableActionSaysNothing(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	host.described = true
	host.status = pluginhost.Status{Instance: "fixture", State: pluginhost.StateReady}
	host.desc = pluginhost.Description{
		Info: pluginhost.Info{Kind: "fixture", Name: "Fixture"},
		Collections: []pluginhost.Collection{{
			ID: "plain", Title: "Plain",
			Columns: []pluginhost.Column{{ID: "name", Label: "Name", Primary: true}},
		}},
	}
	m := New("fixture", "fixture", host.calls(), nil)
	m.SetSize(160, 45)
	run(t, m, m.Refresh())

	press(t, m, "a")
	if flash, _ := m.Flash(); flash != "" {
		t.Fatalf("the browser announced %q rather than staying quiet", flash)
	}
	if view := strip(m.View()); strings.Contains(view, "no action") {
		t.Fatalf("the frame still says there is no action:\n%s", view)
	}
}

// The line an act leaves is cleared by the next page. Standing beside a
// different query's results it is no longer true of anything (td-c2dc19).
func TestActionFlashIsClearedByANewList(t *testing.T) {
	host := &fakeHost{page: testPage(3), outcome: pluginhost.Outcome{
		Status: pluginhost.ActDone, Message: "Logged a note for rc:notes:1",
	}}
	m := newTestModel(t, host)
	c, _ := m.ActiveCollection()
	s := m.state(c)
	s.query = "dex"
	run(t, m, m.list(c, s, false))
	run(t, m, m.startAction("capture"))
	if flash, _ := m.Flash(); flash == "" {
		t.Fatal("the act left no line at all")
	}

	s.query = "something else"
	run(t, m, m.list(c, s, false))
	if flash, _ := m.Flash(); flash != "" {
		t.Fatalf("the flash %q survived a new page", flash)
	}
	if view := strip(m.View()); strings.Contains(view, "Logged a note") {
		t.Fatalf("the summary row still carries the old act's line:\n%s", view)
	}
}

// The rendered body cache names the renderer's theme identity, or a theme
// change repaints everything except the body (td-83a3fa).
func TestBodyCacheIsKeyedOnTheRendererStyle(t *testing.T) {
	host := &fakeHost{page: testPage(3), doc: resource.Document{
		Title: "Fixture row",
		Body:  &resource.Body{Format: resource.FormatMarkdown, Text: "# Heading\n\nbody text\n"},
	}}
	m := newTestModel(t, host)
	c, _ := m.ActiveCollection()
	s := m.state(c)
	s.query = "dex"
	run(t, m, m.list(c, s, false))
	press(t, m, "enter")
	m.View()

	if m.detail.bodyForStyle != m.styleKey() {
		t.Fatalf("bodyForStyle = %q, want %q", m.detail.bodyForStyle, m.styleKey())
	}
	m.detail.body = []string{"stale ansi"}
	m.detail.bodyForStyle = "stale-theme"
	got := m.renderedBody(m.detail.bodyForW)
	if len(got) == 1 && got[0] == "stale ansi" {
		t.Fatal("a stale body survived a markdown style change")
	}
	if m.detail.bodyForStyle != m.styleKey() {
		t.Fatalf("bodyForStyle = %q after the rebuild, want %q", m.detail.bodyForStyle, m.styleKey())
	}
}

// Every browser refuses the keys Sidecar owns, whatever placement built it. A
// pane-mode browser used to grant a plugin `1`, `q` or `i` because only the tab
// placement asked for the surface's reserved set (td-fcb648).
func TestEveryBrowserRefusesTheHostsKeys(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	host.described = true
	host.status = pluginhost.Status{Instance: "fixture", State: pluginhost.StateReady}
	host.desc = pluginhost.Description{
		Info: pluginhost.Info{Kind: "fixture", Name: "Fixture"},
		Collections: []pluginhost.Collection{{
			ID: "plain", Title: "Plain",
			Columns: []pluginhost.Column{{ID: "name", Label: "Name", Primary: true}},
		}},
		Actions: []pluginhost.Action{
			{ID: "one", Title: "One", On: pluginhost.ActionOnGlobal, Key: "1"},
			{ID: "switch", Title: "Switch", On: pluginhost.ActionOnGlobal, Key: "i"},
			{ID: "quit", Title: "Quit", On: pluginhost.ActionOnGlobal, Key: "q"},
			{ID: "pane", Title: "Pane", On: pluginhost.ActionOnGlobal, Key: "n"},
			{ID: "fine", Title: "Fine", On: pluginhost.ActionOnGlobal, Key: "z"},
		},
	}
	m := New("fixture", "fixture", host.calls(), nil)
	m.SetPaneCollection("plain")
	m.SetSize(80, 24)
	run(t, m, m.Refresh())

	for key := range keymap.GlobalKeys {
		if _, granted := m.GrantedKey(key); granted {
			t.Fatalf("a pane browser granted the plugin %q, which is Sidecar's", key)
		}
	}
	if _, granted := m.GrantedKey("n"); granted {
		t.Fatal("a pane browser granted the plugin the pane switcher's key")
	}
	if _, granted := m.GrantedKey("z"); !granted {
		t.Fatal("a key nobody owns was refused too")
	}
}

// Reaching the query bound says so on the row that refused the keystroke,
// rather than leaving the pane looking wedged (td-fcb648).
func TestQueryLimitSaysSoOnTheQueryRow(t *testing.T) {
	host := &fakeHost{page: testPage(3)}
	m := newTestModel(t, host)
	c, _ := m.ActiveCollection()
	s := m.state(c)
	press(t, m, "/")
	s.query = strings.Repeat("x", resource.MaxQueryChars)

	m.HandleKey(keyPress("y"))
	if !s.atLimit {
		t.Fatal("a refused keystroke left no trace")
	}
	view := strip(m.View())
	if !strings.Contains(view, "as long as Sidecar keeps") {
		t.Fatalf("the query row does not say the bound was reached:\n%s", view)
	}
	if len([]rune(s.query)) != resource.MaxQueryChars {
		t.Fatalf("the query grew past the bound to %d runes", len([]rune(s.query)))
	}

	m.HandleKey(keyPress("backspace"))
	if s.atLimit {
		t.Fatal("the hint outlived the edit that cleared the bound")
	}
}
