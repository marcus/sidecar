package assembly

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/configui"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/tasks"
)

// The catalog is the tab order, and every entry has to be answerable: an ID, a
// class, a scope, at least one placement, and a constructor.
func TestDescriptorsAreCompleteAndOrdered(t *testing.T) {
	want := []string{
		IDTDMonitor, IDGitStatus, IDFileBrowser, IDConversations,
		IDWorkspace, IDNotes, IDTasks,
	}
	descriptors := Descriptors()
	if len(descriptors) != len(want) {
		t.Fatalf("catalog has %d descriptors, want %d", len(descriptors), len(want))
	}
	seen := map[string]bool{}
	for i, d := range descriptors {
		if d.ID != want[i] {
			t.Fatalf("descriptor %d = %q, want %q", i, d.ID, want[i])
		}
		if seen[d.ID] {
			t.Fatalf("duplicate descriptor ID %q", d.ID)
		}
		seen[d.ID] = true
		if d.Class != plugin.ClassEmbedded {
			t.Errorf("%s class = %q, want embedded", d.ID, d.Class)
		}
		if d.Scope != plugin.ScopeProject && d.Scope != plugin.ScopeGlobal {
			t.Errorf("%s scope = %q", d.ID, d.Scope)
		}
		if len(d.Placements) == 0 {
			t.Errorf("%s declares no placement", d.ID)
		}
		if d.Name == "" || d.Detail == "" {
			t.Errorf("%s is missing a name or detail: %q / %q", d.ID, d.Name, d.Detail)
		}
		if d.New == nil {
			t.Fatalf("%s has no constructor", d.ID)
		}
		if got := d.New().ID(); got != d.ID {
			t.Errorf("descriptor %q constructed plugin with ID %q", d.ID, got)
		}
	}
}

// Tasks is the one global-scope descriptor today, and internal/app keeps its
// own copy of the global list because it cannot import this package. The two
// have to name the same plugins in the same order or a global tab would exist
// in the settings page and not in the header, or the reverse.
func TestGlobalDescriptorsMatchTheAppsOwnList(t *testing.T) {
	cfg := config.Default()
	cfg.Plugins.Tasks.Enabled = nil
	cfg.Features.Flags[features.TasksPlugin.Name] = true
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })

	catalog := GlobalDescriptors(cfg)
	shell := app.GlobalDescriptors()
	if len(catalog) != len(shell) {
		t.Fatalf("assembly lists %d global plugins, the app shell lists %d", len(catalog), len(shell))
	}
	for i := range catalog {
		if catalog[i].ID != shell[i].ID {
			t.Fatalf("global plugin %d: assembly %q, app %q", i, catalog[i].ID, shell[i].ID)
		}
		if catalog[i].Scope != plugin.ScopeGlobal {
			t.Fatalf("%q is in the global list with scope %q", catalog[i].ID, catalog[i].Scope)
		}
	}
	if len(catalog) == 0 || catalog[0].ID != IDTasks {
		t.Fatalf("global catalog = %#v, want Tasks first", catalog)
	}

	// The switch is read, not assumed: with the plugin off, the global list is
	// empty and the header row has no plugin-provided tab to number.
	off := false
	cfg.Plugins.Tasks.Enabled = &off
	if got := GlobalDescriptors(cfg); len(got) != 0 {
		t.Fatalf("disabled Tasks still listed: %#v", got)
	}
}

// plugins.<id>.enabled is the switch for every plugin, and the two deprecated
// feature flags answer only while the key is absent.
func TestUnifiedEnablementPrefersTheConfigKey(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name    string
		flag    string
		flagOn  bool
		key     *bool
		setKey  func(*config.Config, *bool)
		enabled func(*config.Config) bool
		want    bool
	}{
		{"tasks flag alias", features.TasksPlugin.Name, true, nil,
			func(c *config.Config, v *bool) { c.Plugins.Tasks.Enabled = v },
			tasks.Descriptor().IsEnabled, true},
		{"tasks key wins", features.TasksPlugin.Name, true, &off,
			func(c *config.Config, v *bool) { c.Plugins.Tasks.Enabled = v },
			tasks.Descriptor().IsEnabled, false},
		{"notes flag alias", features.NotesPlugin.Name, false, nil,
			func(c *config.Config, v *bool) { c.Plugins.Notes.Enabled = v },
			NotesWanted, false},
		{"notes key wins", features.NotesPlugin.Name, false, &on,
			func(c *config.Config, v *bool) { c.Plugins.Notes.Enabled = v },
			NotesWanted, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Features.Flags[tc.flag] = tc.flagOn
			tc.setKey(cfg, tc.key)
			features.Init(cfg)
			t.Cleanup(func() { features.Init(config.Default()) })
			if got := tc.enabled(cfg); got != tc.want {
				t.Fatalf("enabled = %v, want %v", got, tc.want)
			}
		})
	}
}

// The settings page is one loop over the catalog, and the catalog lives here.
// This is the guard against configui's own fixture drifting from what Sidecar
// actually ships: it renders the real page with the real descriptors.
func TestPanelsPageListsEverySurfaceInTheRealCatalog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	config.SetTestConfigPath(path)
	t.Cleanup(config.ResetTestConfigPath)

	cfg := config.Default()
	features.Init(cfg)
	t.Cleanup(func() { features.Init(config.Default()) })

	m := configui.New()
	m.SetPluginDescriptors(Descriptors())
	m.SetHostState(configui.HostState{Config: cfg})
	m.Open(configui.PagePanels)

	view := ansi.Strip(m.View(160, 45))
	for _, d := range Descriptors() {
		if !d.HasSwitch() {
			// Workspaces has no switch, so it has no row: a control that
			// cannot change anything is worse than no control.
			if strings.Contains(view, d.Detail) {
				t.Fatalf("a switchless plugin has a settings row: %q", d.ID)
			}
			continue
		}
		if !strings.Contains(view, d.Name) {
			t.Fatalf("Panels is missing %q:\n%s", d.Name, view)
		}
		if !strings.Contains(view, d.Detail) {
			t.Fatalf("Panels is missing %q's detail line %q:\n%s", d.ID, d.Detail, view)
		}
	}
}

// internal/config restates the IDs Sidecar's own surfaces answer to, because
// it is a leaf package and cannot read the descriptor catalog. This is the
// test that keeps the restatement true: a plugin added to the catalog, or a
// global tab added to the app shell, without being named there would let an
// external plugin take its id and paint two tabs with one identity.
func TestReservedPluginIDsCoverTheCatalog(t *testing.T) {
	for _, d := range Descriptors() {
		name, ok := config.ReservedPluginID(d.ID)
		if !ok {
			t.Errorf("descriptor %q is not in config's reserved id list; an external plugin could take its id", d.ID)
			continue
		}
		if name != d.Name {
			t.Errorf("reserved id %q is called %q in config and %q in the catalog", d.ID, name, d.Name)
		}
	}
	// The two app-owned global tabs have no descriptor and are just as
	// unavailable as an id.
	for _, id := range []string{app.GlobalSessions, app.GlobalActivity} {
		if _, ok := config.ReservedPluginID(id); !ok {
			t.Errorf("app-owned global surface %q is not in config's reserved id list", id)
		}
	}
}
