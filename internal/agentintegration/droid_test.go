package agentintegration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Droid adapter suite runs entirely inside t.TempDir with an injected Env.

func droidFixture(t *testing.T, opts ...func(*Env)) (Service, Env, sessionEntryPaths) {
	t.Helper()
	home := t.TempDir()
	env := Env{
		Home: home,
		LookPath: func(file string) (string, error) {
			if file == DroidProvider {
				return filepath.Join(home, "bin", "droid"), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "0.20.0" },
		UID:             os.Getuid(),
	}
	for _, o := range opts {
		o(&env)
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, droidSpec().pathsFor(env)
}

func droidSetUp(t *testing.T, paths sessionEntryPaths) {
	t.Helper()
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func droidStatus(t *testing.T, s Service) Status {
	t.Helper()
	st, err := s.Status(DroidProvider)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

// TestDroidInstallsUnderExactlyTheEventUpstreamNames is the table diff, and the
// interesting half is that there is one row. Upstream withdrew its nine
// lifecycle rows at version 3, so a port that carried them would be reinstating
// something upstream removed.
func TestDroidInstallsUnderExactlyTheEventUpstreamNames(t *testing.T) {
	rows := readProviderHookTable(t, "droid")
	events := droidSpec().eventNames()
	if len(rows) != len(events) {
		t.Fatalf("the fixture records %d events and the port ships %d", len(rows), len(events))
	}
	for i, row := range rows {
		if row[0] != events[i] {
			t.Errorf("event %d: the port installs under %q, the fixture records %q", i, events[i], row[0])
		}
		if row[1] != "-" {
			t.Errorf("event %d: the fixture records matcher %q, but install_droid passes none", i, row[1])
		}
	}
	if droidSpec().matcher != nil {
		t.Fatal("the Droid group carries a matcher; install_droid writes none")
	}
}

func TestDroidBundledEntryMatchesTheRegistry(t *testing.T) {
	asset := NewDroidAdapter().settingsAsset()
	capability, known := agentlifecycle.CapabilityForSource(asset.Source)
	if !known {
		t.Fatalf("no capability entry for %s", asset.Source)
	}
	if capability.AssetVersion != asset.Version {
		t.Fatalf("asset version %q but registry records %q", asset.Version, capability.AssetVersion)
	}
	if capability.Tier != agentlifecycle.TierScreenFallback {
		t.Fatalf("tier %q, want screen-fallback until a released Droid is traced", capability.Tier)
	}
	if capability.Evidence != agentlifecycle.EvidenceDocsOnly {
		t.Fatalf("evidence %q, want docs-only", capability.Evidence)
	}
	if want := "sidecar agent report-session --kind droid --hook-stdin"; reportSessionCommand(DroidProvider) != want {
		t.Fatalf("command %q, want %q", reportSessionCommand(DroidProvider), want)
	}
}

// TestDroidLivesUnderTheFactoryDirectoryWithNoOverride pins the absence.
//
// Every other provider in this group honours a configuration-directory
// variable, and Droid has none: Herdr consults nothing and Factory documents
// nothing. An override invented later would send Sidecar's writes somewhere
// Droid does not read, so the absence is asserted rather than left implicit.
func TestDroidLivesUnderTheFactoryDirectoryWithNoOverride(t *testing.T) {
	home := t.TempDir()
	if droidSpec().dirOverride != nil {
		t.Fatal("the Droid spec consults an environment override; Droid documents none")
	}
	// Neither of the variables that move a neighbouring provider moves this one.
	for _, env := range []Env{
		{Home: home},
		{Home: home, ConfigHome: filepath.Join(t.TempDir(), "xdg")},
	} {
		got := droidSpec().pathsFor(env).Settings
		if want := filepath.Join(home, ".factory", "settings.json"); got != want {
			t.Fatalf("settings path is %q, want %q", got, want)
		}
	}
}

func TestDroidInstallWritesOneEntryAndPreservesTheRest(t *testing.T) {
	svc, _, paths := droidFixture(t)
	droidSetUp(t, paths)
	writeFileForTest(t, paths.Settings, `{"model":"claude-opus","hooksDisabled":false}`)

	applyTo(t, svc, DroidProvider, ActionInstall)

	got := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)
	if got["model"] != "claude-opus" || got["hooksDisabled"] != false {
		t.Fatalf("the user's own settings were lost: %v", got)
	}
	groups := got["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(groups) != 1 {
		t.Fatalf("hooks.SessionStart has %d groups, want one", len(groups))
	}
	group := groups[0].(map[string]any)
	if _, has := group["matcher"]; has {
		t.Fatal("the written group carries a matcher key")
	}
	entry := group["hooks"].([]any)[0].(map[string]any)
	if entry["command"] != reportSessionCommand(DroidProvider) {
		t.Fatalf("command = %v", entry["command"])
	}
	// Seconds, which is what Factory's hooks reference documents (default 60).
	if entry["timeout"] != float64(hookTimeoutSec) {
		t.Fatalf("timeout = %v, want %d seconds", entry["timeout"], hookTimeoutSec)
	}
	if st := droidStatus(t, svc); st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after install = %q (%s)", st.Status, st.Message)
	}

	applyTo(t, svc, DroidProvider, ActionUninstall)
	after := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)
	if _, has := after["hooks"]; has {
		t.Fatalf("uninstall left a hooks key behind: %v", after["hooks"])
	}
	if after["model"] != "claude-opus" {
		t.Fatal("uninstall lost the user's own setting")
	}
}

// TestDroidSaysSoWhenHooksJsonWouldShadowTheEntry is the finding that is not in
// Herdr at all.
//
// Droid reads hook declarations from ~/.factory/hooks.json first and falls back
// to settings.json's hooks key only when that file is absent. Without this the
// status would read `current` for an installation that can never fire, which is
// the most expensive kind of wrong a status surface can be.
func TestDroidSaysSoWhenHooksJsonWouldShadowTheEntry(t *testing.T) {
	svc, _, paths := droidFixture(t)
	droidSetUp(t, paths)
	applyTo(t, svc, DroidProvider, ActionInstall)

	clean := droidStatus(t, svc)
	if strings.Contains(clean.Message, "hooks.json") {
		t.Fatalf("the status warns about hooks.json when there is none: %q", clean.Message)
	}

	writeFileForTest(t, paths.Shadow, `{"hooks":{}}`)
	shadowed := droidStatus(t, svc)

	// The status is unchanged, because the installation is not damaged: it is
	// installed correctly and inert, and repair cannot fix that.
	if shadowed.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status = %q; a shadowed installation is still correctly installed", shadowed.Status)
	}
	if !strings.Contains(shadowed.Message, paths.Shadow) {
		t.Fatalf("the status does not name the file that shadows it: %q", shadowed.Message)
	}
	if !strings.Contains(shadowed.Message, "never fire") {
		t.Fatalf("the status does not say what the consequence is: %q", shadowed.Message)
	}

	// Sidecar reports the file and never offers to touch it.
	var reported bool
	for _, f := range shadowed.Files {
		if f.Path == paths.Shadow {
			reported = true
		}
	}
	if !reported {
		t.Fatal("the shadow file is not among the files the adapter reports inspecting")
	}
	for _, p := range shadowed.TargetPaths {
		if p == paths.Shadow {
			t.Fatal("hooks.json is listed as a file an install would touch; Sidecar never writes it")
		}
	}
	before := readFileForTest(t, paths.Shadow)
	applyTo(t, svc, DroidProvider, ActionUninstall)
	if got := readFileForTest(t, paths.Shadow); got != before {
		t.Fatalf("uninstall edited hooks.json:\n%s\n---\n%s", before, got)
	}
}

func TestDroidRefusesWhenTheFactoryDirectoryIsAbsent(t *testing.T) {
	svc, _, paths := droidFixture(t)

	_, err := svc.Plan(DroidProvider, ActionInstall)
	r, ok := AsRefusal(err)
	if !ok || r.Code != RefuseNotInstalled {
		t.Fatalf("install refusal = %v, want not_installed", err)
	}
	if !strings.Contains(r.Message, "run droid once") {
		t.Fatalf("the refusal does not say how to fix it: %q", r.Message)
	}
	if _, err := os.Stat(paths.Dir); !os.IsNotExist(err) {
		t.Fatalf("the refused plan created %s", paths.Dir)
	}
}

func TestDroidNeverAdoptsAnEntryThatMerelyLooksLikeSidecars(t *testing.T) {
	svc, _, paths := droidFixture(t)
	droidSetUp(t, paths)
	writeFileForTest(t, paths.Settings, `{
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "wrap sidecar agent report-session --kind droid --hook-stdin", "timeout": 10}]}
    ]
  }
}`)
	if got := droidStatus(t, svc); got.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("status = %q; a wrapped command is not Sidecar's entry", got.Status)
	}
	applyTo(t, svc, DroidProvider, ActionInstall)
	groups := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(groups) != 2 {
		t.Fatalf("install left %d groups; the user's own must survive beside Sidecar's", len(groups))
	}
}

// readProviderHookTable reads one provider's checked-in event fixture. It is
// shared by every session-identity port in this lane, so a provider added later
// without a fixture fails on the missing file rather than on a missing test.
func readProviderHookTable(t *testing.T, provider string) [][]string {
	t.Helper()
	return readHookTableAt(t, filepath.Join("testdata", provider, "hook-table.tsv"))
}
