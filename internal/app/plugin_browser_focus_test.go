package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/pluginbrowser"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/state"
)

// browserDeckModel registers one real plugin browser as the active plugin and
// gives it a deck, so what is under test is the app's key ladder over the
// surface that actually ships rather than a stand-in with the same shape.
func browserDeckModel(t *testing.T, root string, p plugin.Plugin) *Model {
	t.Helper()
	cfg := config.Default()
	cfg.Features.Flags = map[string]bool{features.PluginContentPanes.Name: true}
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })
	if err := state.InitWithDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.InitWithDir(t.TempDir()) })
	reg := plugin.NewRegistry(&plugin.Context{WorkDir: root, ProjectRoot: root, Epoch: 9, Config: cfg})
	if err := reg.Register(p); err != nil {
		t.Fatal(err)
	}
	km := keymap.NewRegistry()
	keymap.RegisterDefaults(km)
	p.SetFocused(true)
	return &Model{
		registry: reg, keymap: km, activePlugin: 0, contentDecks: make(map[string]*appContentDeck),
		ui: &UIState{WorkDir: root, ProjectRoot: root}, ready: true, applicationFocused: true,
		width: 200, height: 50, cfg: cfg,
	}
}

// browserTabPlugin is a tab-placed plugin browser over an in-memory host: one
// collection, one page, one document.
func browserTabPlugin() *pluginbrowser.TabPlugin {
	desc := pluginhost.Description{
		Info: pluginhost.Info{Kind: "fixture", Name: "Fixture"},
		Collections: []pluginhost.Collection{{
			ID: "results", Title: "Results", Detail: true,
			Columns: []pluginhost.Column{{ID: "title", Label: "Title", Primary: true}},
		}},
	}
	page := pluginhost.Page{
		Outcome: pluginhost.OutcomeAnswered,
		Items: []pluginhost.Item{
			{ID: "one", Cells: map[string]string{"title": "Row one"}},
			{ID: "two", Cells: map[string]string{"title": "Row two"}},
		},
		Total: 2,
	}
	doc := resource.Document{Title: "Row one", Identity: "one"}
	return pluginbrowser.NewTabPlugin("fixture", "fixture", func(instance string) pluginbrowser.Calls {
		return pluginbrowser.Calls{
			Describe: func(string) (pluginhost.Description, pluginhost.Status, bool) {
				return desc, pluginhost.Status{Instance: instance, State: pluginhost.StateReady}, true
			},
			List: func(call pluginbrowser.ListCall) tea.Cmd {
				return func() tea.Msg {
					return pluginbrowser.ListedMsg{
						Instance: call.Instance, Browser: call.Browser,
						Collection: call.Params.Collection, Generation: call.Generation, Page: page,
					}
				}
			},
			Get: func(call pluginbrowser.GetCall) tea.Cmd {
				return func() tea.Msg {
					return pluginbrowser.GotMsg{
						Instance: call.Instance, Browser: call.Browser,
						Collection: call.Params.Collection, ID: call.Params.ID,
						Generation: call.Generation, Document: doc,
					}
				}
			},
		}
	})
}

// drivePlugin runs a command and feeds what it produced back through the
// plugin, which is what the Bubble Tea runtime does.
func drivePlugin(p plugin.Plugin, cmd tea.Cmd) {
	for i := 0; cmd != nil && i < 8; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			var next []tea.Cmd
			for _, c := range batch {
				if c == nil {
					continue
				}
				if sub := c(); sub != nil {
					_, follow := p.Update(sub)
					next = append(next, follow)
				}
			}
			cmd = tea.Batch(next...)
			continue
		}
		_, cmd = p.Update(msg)
	}
}

// Tab reaches a plugin's own focus ring on a deck that holds nothing but its
// Primary leaf. The browser's list and the document box beside it are exactly
// such a ring, and until this the app's focus ring engaged only once a
// secondary leaf was open — so the browser's stops were correct and
// unreachable.
func TestAppContentTabCyclesPrimaryOnlyBrowserStops(t *testing.T) {
	root := t.TempDir()
	p := browserTabPlugin()
	m := browserDeckModel(t, root, p)
	m.renderContent(200, 40)

	drivePlugin(p, p.Model().Refresh())
	// Open a document, so the browser projects both of its windows.
	drivePlugin(p, func() tea.Msg { return tea.KeyPressMsg{Code: tea.KeyEnter} })
	m.renderContent(200, 40)

	h := m.currentContentDeck()
	if h == nil {
		t.Fatal("the browser got no content deck")
	}
	if got := h.deck.Leaf(panelayout.Document) + h.deck.Leaf(panelayout.Issue) +
		h.deck.Leaf(panelayout.Note) + h.deck.Leaf(panelayout.Diff) + h.deck.Leaf(panelayout.Resource); got != 0 {
		t.Fatalf("the deck holds a passive leaf (%d); this test is about a Primary-only one", got)
	}
	if stops := p.PaneFocusStops(); len(stops) != 2 {
		t.Fatalf("the browser projects %d stops, want its list and its document box", len(stops))
	}
	if got := p.PaneFocus(); got != "list" {
		t.Fatalf("focus started at %q", got)
	}

	if _, handled := m.handleAppContentKey(tea.KeyPressMsg{Code: tea.KeyTab}); !handled {
		t.Fatal("Tab was not answered on a Primary-only deck")
	}
	if got := p.PaneFocus(); got != "detail" {
		t.Fatalf("Tab left focus at %q, want the document box", got)
	}
	if _, handled := m.handleAppContentKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}); !handled {
		t.Fatal("Shift+Tab was not answered on a Primary-only deck")
	}
	if got := p.PaneFocus(); got != "list" {
		t.Fatalf("Shift+Tab left focus at %q, want the list", got)
	}

	// Every other key on that deck is still the plugin's own.
	if _, handled := m.handleAppContentKey(tea.KeyPressMsg{Code: 'j', Text: "j"}); handled {
		t.Fatal("a Primary-only deck swallowed a key the plugin binds")
	}
}

// A surface with one stop is not a ring, and Tab there stays with whatever the
// plugin binds it to rather than being swallowed for nothing.
func TestAppContentTabDeclinesASingleStopSurface(t *testing.T) {
	root := t.TempDir()
	p := browserTabPlugin()
	m := browserDeckModel(t, root, p)
	m.renderContent(200, 40)
	drivePlugin(p, p.Model().Refresh())
	m.renderContent(200, 40)

	if stops := p.PaneFocusStops(); len(stops) != 1 {
		t.Fatalf("a browser with nothing open projects %d stops, want one", len(stops))
	}
	if _, handled := m.handleAppContentKey(tea.KeyPressMsg{Code: tea.KeyTab}); handled {
		t.Fatal("Tab was swallowed by a surface with nowhere to go")
	}
}
