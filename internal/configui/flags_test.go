package configui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
)

// flagsFixture is a model on Feature Flags with a temp config file and the
// feature manager pointed at it.
func flagsFixture(t *testing.T, mutate func(*config.Config)) *Model {
	t.Helper()
	cfg := config.Default()
	if mutate != nil {
		mutate(cfg)
	}
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })
	m, _ := configFixture(t, cfg)
	m.Open(PageFlags)
	return m
}

// Registering a feature is all it takes to make it reachable. Without this, a
// flag added to internal/features is settable only by hand-editing
// config.json — which is how five of them sat unreachable behind a curated
// four-item list.
func TestFlagsPageListsEveryRegisteredFlag(t *testing.T) {
	m := flagsFixture(t, nil)
	// Tall enough that the pane's height clamp cuts nothing: the detail pane
	// truncates rather than scrolling, so a short view would pass this test by
	// hiding the very rows it is meant to check.
	view := ansi.Strip(m.View(160, 200))

	listed := make(map[string]bool, len(previews()))
	for _, item := range previews() {
		listed[item.flag] = true
	}
	for _, feature := range features.ListAll() {
		if !listed[feature.Name] {
			t.Fatalf("%s is registered but Feature Flags does not list it", feature.Name)
		}
		item := previewCopy[feature.Name]
		label := item.label
		if label == "" {
			label = feature.Name
		}
		if !strings.Contains(view, label) {
			t.Fatalf("%s is listed as %q but does not render:\n%s", feature.Name, label, view)
		}
	}
}

// A flag with no hand-written copy still derives a usable row from the registry
// alone. This is the whole premise of deriving the list — registering a feature
// is meant to be enough — so it is exercised with a feature that genuinely has
// no entry rather than by filtering the real ones, all of which are curated.
func TestFlagsPageFallsBackToRegistryCopy(t *testing.T) {
	synthetic := features.Feature{
		Name:        "not_a_registered_flag",
		Description: "Does a thing the registry describes and configui does not.",
	}
	if _, curated := previewCopy[synthetic.Name]; curated {
		t.Fatalf("%s is curated, so this no longer tests the fallback", synthetic.Name)
	}
	item := previewFor(synthetic)
	if item.flag != synthetic.Name {
		t.Fatalf("flag = %q, want %q", item.flag, synthetic.Name)
	}
	if item.label != synthetic.Name {
		t.Fatalf("label = %q, want the flag name", item.label)
	}
	if item.help != synthetic.Description {
		t.Fatalf("help = %q, want the registry description", item.help)
	}
	if item.owner != "" || item.restart {
		t.Fatalf("an uncurated flag invented metadata: %+v", item)
	}
}

// Curated copy wins over the registry's, or the hand-written labels would be
// silently discarded by the same code path.
func TestFlagsPagePrefersCuratedCopy(t *testing.T) {
	item := previewFor(features.Feature{
		Name:        features.CrossProjectOverview.Name,
		Description: "registry text that should not win",
	})
	if item.label != "Cross-project Activity" {
		t.Fatalf("label = %q, want the curated one", item.label)
	}
	if item.help == "registry text that should not win" {
		t.Fatal("registry description overrode curated help")
	}
	if !item.restart {
		t.Fatal("curated restart flag was lost")
	}
}

func TestPaneMoveUsesHumanFacingCopy(t *testing.T) {
	item := previewFor(features.PaneMove)
	if item.label != "Move panes" || strings.Contains(item.label, "pane_move") {
		t.Fatalf("pane_move label = %q, want human-facing copy", item.label)
	}
	if !strings.Contains(item.help, "header control") {
		t.Fatalf("pane_move help does not describe both entry points: %q", item.help)
	}
	m := flagsFixture(t, nil)
	if view := ansi.Strip(m.View(120, 31)); strings.Contains(view, features.PaneMove.Name) {
		t.Fatalf("Feature Flags exposed raw pane_move identifier:\n%s", view)
	}
}

// The registry may outgrow an ordinary terminal, but every row must remain
// reachable: moving the detail cursor scrolls its complete label and help text
// into the pane instead of letting the generic height clamp hide it.
func TestFlagsPageFitsAnOrdinaryTerminal(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 31}, {160, 31}, {160, 45}} {
		m := flagsFixture(t, nil)
		m.View(size.w, size.h)
		m.detailFocus = true
		for i, item := range flagPagePreviews() {
			m.rowCursor = i
			view := ansi.Strip(m.View(size.w, size.h))
			if !strings.Contains(view, item.label) {
				t.Fatalf("%dx%d did not scroll the %q row into view:\n%s", size.w, size.h, item.label, view)
			}
			// Every flag explains itself, whether or not the cursor is on it:
			// half these names do not say what they turn on. A description that
			// no longer appears whole has outgrown the row and wrapped, which
			// costs a line per flag and is how the page overflows again.
			if !strings.Contains(view, item.help) {
				t.Fatalf("%dx%d wrapped %q's description %q — shorten it:\n%s",
					size.w, size.h, item.label, item.help, view)
			}
			if i == len(flagPagePreviews())-1 && size.h == 31 {
				if m.flagsScroll == 0 {
					t.Fatalf("%dx%d reached the last flag without scrolling", size.w, size.h)
				}
				region := regionFor(t, m, regionFlag+item.flag)
				if region.Rect.Y < 1 || region.Rect.Y >= size.h-1 {
					t.Fatalf("scrolled %q region is off-screen: %+v", item.label, region.Rect)
				}
			}
		}
	}
}

// Narrower than that the descriptions wrap, which is fine — but every row must
// still be reachable, because the pane truncates and the cursor walks onto rows
// that were cut.
func TestFlagsPageKeepsEveryRowOnANarrowPane(t *testing.T) {
	m := flagsFixture(t, nil)
	m.View(100, 36)
	m.detailFocus = true
	for i, item := range flagPagePreviews() {
		m.rowCursor = i
		view := ansi.Strip(m.View(100, 36))
		if !strings.Contains(view, item.label) {
			t.Fatalf("100x36 did not scroll the %q row into view:\n%s", item.label, view)
		}
	}
}

// The restart notice is above the list, so it cannot be the first thing the
// pane's height clamp removes.
func TestRestartNoticeSurvivesAShortPane(t *testing.T) {
	m := flagsFixture(t, nil)
	activate(t, m, regionFlag+features.CrossProjectOverview.Name)
	if view := ansi.Strip(m.View(120, 31)); !strings.Contains(view, panelRestartNote) {
		t.Fatalf("a short pane hid the restart notice:\n%s", view)
	}
}

// A flag whose consumer needs a restart *and* has a scope note says both. The
// original switch stopped at the first match and dropped the sentence warning
// that turning terminal_resource_providers off kills running provider
// processes.
func TestFlagRowStatesRestartAndScopeTogether(t *testing.T) {
	m := flagsFixture(t, nil)
	m.View(160, 60)
	m.detailFocus = true
	m.focusControlByID(regionFlag + features.TerminalResourceProviders.Name)
	view := ansi.Strip(m.View(160, 60))
	// Match phrases short enough to survive the detail's wrap. The view is two
	// panes side by side, so the sidebar's own text sits between the detail's
	// wrapped lines and nothing can be matched across one.
	for _, want := range []string{"takes effect after a restart", "turning this off stops every provider"} {
		if !strings.Contains(view, want) {
			t.Fatalf("focused row is missing %q:\n%s", want, view)
		}
	}
}

// The Conversations row reports what Panels reports. Panels pairs the flag with
// the plugin's own enabled key and clears only the key on the way off, so a row
// reading the raw flag renders ON beside a Panels page rendering OFF.
func TestOwnedRowAgreesWithTheOwningPage(t *testing.T) {
	m := flagsFixture(t, func(cfg *config.Config) {
		cfg.Features.Flags = map[string]bool{features.ConversationsPlugin.Name: true}
		cfg.Plugins.Conversations.Enabled = false
	})
	item := previewCopy[features.ConversationsPlugin.Name]
	if got := item.state(m); got != m.conversationsOn() {
		t.Fatalf("Feature Flags says %v, Panels says %v", got, m.conversationsOn())
	}
	if item.state(m) {
		t.Fatal("the row reported ON for a panel Panels shows as OFF")
	}
}

// A flag another page owns is reported here but not settable here: activating
// the row navigates to the owning control instead of writing the flag. Two
// switches over one value is how the pair Panels keeps consistent — the flag
// and the plugin's own enabled key — would start disagreeing.
func TestFlagsPageDefersOwnedFlagsToTheirPage(t *testing.T) {
	owned := 0
	for _, item := range previews() {
		if item.owner == "" {
			continue
		}
		owned++
		t.Run(item.flag, func(t *testing.T) {
			m := flagsFixture(t, nil)
			before := m.flagEnabled(item.flag)
			activate(t, m, regionFlag+item.flag)

			if _, ok := loadSaved(t).Features.Flags[item.flag]; ok {
				t.Fatalf("%s was written from a page that does not own it", item.flag)
			}
			if m.flagEnabled(item.flag) != before {
				t.Fatalf("%s changed on a read-only row", item.flag)
			}
			if m.Route().Page != item.owner {
				t.Fatalf("activating %s landed on %q, want %q", item.flag, m.Route().Page, item.owner)
			}
		})
	}
	if owned == 0 {
		t.Fatal("no owned flags in the list, so the deferral is untested")
	}
}

// Every settable flag round-trips to features.flags under its real name.
func TestFlagsRoundTrip(t *testing.T) {
	for _, item := range previews() {
		if item.owner != "" {
			continue
		}
		t.Run(item.flag, func(t *testing.T) {
			m := flagsFixture(t, nil)
			before := m.flagEnabled(item.flag)
			activate(t, m, regionFlag+item.flag)
			got, ok := loadSaved(t).Features.Flags[item.flag]
			if !ok {
				t.Fatalf("%s was not written to features.flags", item.flag)
			}
			if got == before {
				t.Fatalf("%s did not change (still %v)", item.flag, got)
			}
			if m.flagEnabled(item.flag) != got {
				t.Fatalf("the page disagrees with the file for %s", item.flag)
			}
			// The live answer must move with the file, or a saved flag would do
			// nothing until a restart it does not need.
			if features.IsEnabled(item.flag) != got {
				t.Fatalf("features.IsEnabled(%s) = %v, file says %v", item.flag, features.IsEnabled(item.flag), got)
			}
		})
	}
}

// Only a flag that is genuinely read once at startup claims a restart.
func TestFlagsRestartNoteIsPerFlag(t *testing.T) {
	m := flagsFixture(t, nil)
	activate(t, m, regionFlag+features.WorkspaceDocPanes.Name)
	if view := ansi.Strip(m.View(160, 200)); strings.Contains(view, panelRestartNote) {
		t.Fatalf("a live flag claimed it needed a restart:\n%s", view)
	}

	m = flagsFixture(t, nil)
	activate(t, m, regionFlag+features.CrossProjectOverview.Name)
	if view := ansi.Strip(m.View(160, 200)); !strings.Contains(view, panelRestartNote) {
		t.Fatalf("a startup-scoped flag did not mention the restart:\n%s", view)
	}
}

// Turning on Full tmux attach is what makes the Terminal page's attach chord
// editable — the two pages share one config state, not a copy each.
func TestFullAttachUnlocksTheTerminalAttachControl(t *testing.T) {
	m := flagsFixture(t, func(cfg *config.Config) {
		cfg.Plugins.Workspace.InteractiveAttachKey = "ctrl+]"
	})
	m.Navigate(PageTerminal)
	if view := ansi.Strip(m.View(160, 45)); !strings.Contains(view, "Turn on Full tmux attach under Feature Flags") {
		t.Fatalf("the attach control was not locked to begin with:\n%s", view)
	}

	m.Navigate(PageFlags)
	activate(t, m, regionFlag+features.TmuxFullAttach.Name)
	m.Navigate(PageTerminal)
	view := ansi.Strip(m.View(160, 45))
	if strings.Contains(view, "Turn on Full tmux attach under Feature Flags") {
		t.Fatalf("the attach control stayed locked after enabling the flag:\n%s", view)
	}
	if !strings.Contains(view, "Ctrl+]") {
		t.Fatalf("the attach chord is not editable after enabling the flag:\n%s", view)
	}
}

// Search finds a flag by the name a user reads in config.json, and lands them
// on a page that can actually set it.
func TestSearchFindsFlagsByConfigName(t *testing.T) {
	for _, item := range previews() {
		t.Run(item.flag, func(t *testing.T) {
			matches := Search(item.flag)
			if len(matches) == 0 {
				t.Fatalf("searching %q found nothing", item.flag)
			}
			want := PageFlags
			if item.owner != "" {
				want = item.owner
			}
			for _, match := range matches {
				if match.Label == item.label && match.Page == want {
					return
				}
			}
			t.Fatalf("searching %q did not offer %q on %q: %+v", item.flag, item.label, want, matches)
		})
	}
}

// The Notes and Tasks rows report what Panels reports. Their feature flags are
// read-only aliases now: the config key decides once it is written, and the
// flag answers only while it is absent. A row reading the flag alone rendered
// "Tasks panel OFF" next to a Panels page rendering Tasks ON.
func TestAliasRowsAgreeWithThePanelsPage(t *testing.T) {
	on, off := true, false
	tests := []struct {
		name    string
		panelID string
		flag    string
		mutate  func(*config.Config)
		want    bool
	}{
		{
			name:    "tasks on by config while the flag is off",
			panelID: panelIDTasks,
			flag:    features.TasksPlugin.Name,
			mutate: func(cfg *config.Config) {
				cfg.Features.Flags = map[string]bool{features.TasksPlugin.Name: false}
				cfg.Plugins.Tasks.Enabled = &on
			},
			want: true,
		},
		{
			name:    "notes off by config while the flag is on",
			panelID: panelIDNotes,
			flag:    features.NotesPlugin.Name,
			mutate: func(cfg *config.Config) {
				cfg.Features.Flags = map[string]bool{features.NotesPlugin.Name: true}
				cfg.Plugins.Notes.Enabled = &off
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := flagsFixture(t, tc.mutate)
			d, ok := m.panelDescriptor(tc.panelID)
			if !ok {
				t.Fatalf("no %s descriptor in the catalog", tc.panelID)
			}
			row := previewCopy[tc.flag]
			if got := row.state(m); got != m.panelOn(d) {
				t.Fatalf("Feature Flags says %v, Panels says %v", got, m.panelOn(d))
			}
			if got := row.state(m); got != tc.want {
				t.Fatalf("row state = %v, want %v", got, tc.want)
			}
		})
	}
}
