package agentintegration

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Devin adapter suite runs entirely inside t.TempDir with an injected Env,
// for the reason the Claude suite states: a test that touched the real
// ~/.config/devin would be rewriting a user's provider configuration.

func devinFixture(t *testing.T, opts ...func(*Env)) (Service, Env, sessionEntryPaths) {
	t.Helper()
	home := t.TempDir()
	env := Env{
		Home:       home,
		ConfigHome: filepath.Join(home, ".config"),
		LookPath: func(file string) (string, error) {
			if file == DevinProvider {
				return filepath.Join(home, "bin", "devin"), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "1.4.0" },
		UID:             os.Getuid(),
	}
	for _, o := range opts {
		o(&env)
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, devinSpec().pathsFor(env)
}

// devinSetUp creates the configuration directory, which is what "the Devin CLI
// has been run at least once" looks like on disk and what the adapter refuses
// to proceed without.
func devinSetUp(t *testing.T, paths sessionEntryPaths) {
	t.Helper()
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func withoutDevin(e *Env) {
	e.LookPath = func(string) (string, error) { return "", errors.New("not found") }
}

// --- the fixture, and what it holds the port to ---

// TestDevinInstallsUnderExactlyTheEventsUpstreamNames is the table diff.
//
// The event list is the whole provider half of this port: everything else is
// Sidecar's transport. Both directions are checked, so an event added to the Go
// list without the fixture fails here, and so does a fixture row the port
// dropped.
func TestDevinInstallsUnderExactlyTheEventsUpstreamNames(t *testing.T) {
	rows := readDevinHookTable(t)
	events := devinSpec().eventNames()
	if len(rows) != len(events) {
		t.Fatalf("the fixture records %d events and the port ships %d", len(rows), len(events))
	}
	for i, row := range rows {
		if row[0] != events[i] {
			t.Errorf("event %d: the port installs under %q, the fixture records %q", i, events[i], row[0])
		}
		if row[1] != "-" {
			t.Errorf("event %d: the fixture records matcher %q, but install_devin passes none", i, row[1])
		}
		if row[2] != "report-session" {
			t.Errorf("event %d: the fixture records verb %q, want report-session", i, row[2])
		}
	}
	// The matcher being absent is a property of the written JSON, not only of
	// the fixture's dash, so it is asserted against the bytes as well.
	if devinSpec().matcher != nil {
		t.Fatal("the Devin group carries a matcher; install_devin writes none")
	}
	if strings.Contains(string(devinSpec().content()), `"matcher"`) {
		t.Fatal("the canonical Devin group renders a matcher key")
	}
}

func readDevinHookTable(t *testing.T) [][]string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "devin", "hook-table.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var rows [][]string
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("hook-table.tsv row %q has %d columns, want 3", line, len(fields))
		}
		rows = append(rows, fields)
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestDevinBundledEntryMatchesTheRegistry(t *testing.T) {
	asset := NewDevinAdapter().settingsAsset()
	capability, known := agentlifecycle.CapabilityForSource(asset.Source)
	if !known {
		t.Fatalf("no capability entry for %s", asset.Source)
	}
	if capability.AssetVersion != asset.Version {
		t.Fatalf("asset version %q but registry records %q", asset.Version, capability.AssetVersion)
	}
	// Untraced, so the entry claims nothing. Promoting it to session-identity
	// is a capture's job, not an edit's; see the entry's own first gap.
	if capability.Tier != agentlifecycle.TierScreenFallback {
		t.Fatalf("tier %q, want screen-fallback until a released Devin is traced", capability.Tier)
	}
	if capability.Evidence != agentlifecycle.EvidenceDocsOnly {
		t.Fatalf("evidence %q, want docs-only", capability.Evidence)
	}
	if len(capability.Covered) != 0 {
		t.Fatalf("covered %v, but no trace covers anything", capability.Covered)
	}
	if want := "sidecar agent report-session --kind devin --hook-stdin"; reportSessionCommand(DevinProvider) != want {
		t.Fatalf("command %q, want %q", reportSessionCommand(DevinProvider), want)
	}
	if !invokesReportSession(reportSessionCommand(DevinProvider)) {
		t.Fatal("the canonical command does not satisfy its own ownership test")
	}
}

// --- installing ---

func TestDevinInstallWritesEveryEventAndPreservesTheRest(t *testing.T) {
	svc, _, paths := devinFixture(t)
	devinSetUp(t, paths)
	writeFileForTest(t, paths.Settings, `{"theme_mode":"dark","hooks":{}}`)

	applyTo(t, svc, DevinProvider, ActionInstall)

	got := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)
	if got["theme_mode"] != "dark" {
		t.Fatalf("the user's own setting was lost: %v", got["theme_mode"])
	}
	hooks := got["hooks"].(map[string]any)
	for _, event := range devinSpec().eventNames() {
		groups, ok := hooks[event].([]any)
		if !ok || len(groups) != 1 {
			t.Fatalf("hooks.%s = %v, want exactly one group", event, hooks[event])
		}
		group := groups[0].(map[string]any)
		if _, has := group["matcher"]; has {
			t.Fatalf("hooks.%s group carries a matcher key", event)
		}
		entries := group["hooks"].([]any)
		if len(entries) != 1 {
			t.Fatalf("hooks.%s carries %d entries, want one", event, len(entries))
		}
		entry := entries[0].(map[string]any)
		if entry["type"] != "command" {
			t.Fatalf("hooks.%s entry type = %v", event, entry["type"])
		}
		if entry["command"] != reportSessionCommand(DevinProvider) {
			t.Fatalf("hooks.%s command = %v", event, entry["command"])
		}
		if entry["timeout"] != float64(hookTimeoutSec) {
			t.Fatalf("hooks.%s timeout = %v, want %d", event, entry["timeout"], hookTimeoutSec)
		}
	}
	st := devinStatus(t, svc)
	if st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after install = %q (%s)", st.Status, st.Message)
	}
}

// TestDevinInstallIsIdempotentAcrossEveryEvent is upstream's own
// install_devin_is_idempotent_for_hook_entries, translated.
func TestDevinInstallIsIdempotentAcrossEveryEvent(t *testing.T) {
	svc, _, paths := devinFixture(t)
	devinSetUp(t, paths)

	applyTo(t, svc, DevinProvider, ActionInstall)
	first := readFileForTest(t, paths.Settings)

	// A second install is a no-op rather than a second set of entries. It is
	// asserted through the plan as well as through the file, because "unchanged"
	// is the claim a dry run makes to a user and an empty op list that still
	// reported a write would be the lie.
	second, err := svc.Plan(DevinProvider, ActionInstall)
	if err != nil {
		t.Fatalf("a second install was refused: %v", err)
	}
	if !second.Unchanged || len(second.Ops) != 0 {
		t.Fatalf("a second install planned %d operations; a converged file needs none", len(second.Ops))
	}
	applyTo(t, svc, DevinProvider, ActionInstall)
	applyTo(t, svc, DevinProvider, ActionRepair)
	if got := readFileForTest(t, paths.Settings); got != first {
		t.Fatalf("converging twice more changed the file:\n%s\n---\n%s", first, got)
	}
	hooks := mustParseAny(t, first).(map[string]any)["hooks"].(map[string]any)
	for _, event := range devinSpec().eventNames() {
		if groups := hooks[event].([]any); len(groups) != 1 {
			t.Fatalf("hooks.%s has %d groups after two converge passes", event, len(groups))
		}
	}
}

// TestDevinRepairsAPartialInstall is the failure mode six events buy that one
// does not: a hand edit that removed some of Sidecar's entries leaves a file
// that binds a session on some turns and silently not on others.
func TestDevinRepairsAPartialInstall(t *testing.T) {
	svc, _, paths := devinFixture(t)
	devinSetUp(t, paths)
	applyTo(t, svc, DevinProvider, ActionInstall)

	// Remove three of the six by hand, the way a user pruning their config would.
	top := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)
	hooks := top["hooks"].(map[string]any)
	for _, event := range []string{"PreToolUse", "PostToolUse", "Stop"} {
		delete(hooks, event)
	}
	edited, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	writeFileForTest(t, paths.Settings, string(edited))

	st := devinStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("status = %q, want needs-repair on a half-installed file", st.Status)
	}
	if !strings.Contains(st.Message, "3 of the 6") {
		t.Fatalf("the message does not say how much is missing: %q", st.Message)
	}
	if _, err := svc.Plan(DevinProvider, ActionInstall); err == nil {
		t.Fatal("install was offered on a damaged installation; repair is the honest verb")
	}

	applyTo(t, svc, DevinProvider, ActionRepair)
	if got := devinStatus(t, svc); got.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after repair = %q (%s)", got.Status, got.Message)
	}
}

// TestDevinUninstallRemovesOnlySidecarsEntries is upstream's
// uninstall_devin_removes_herdr_hooks_and_preserves_others, translated, with
// Sidecar's stricter rule: Herdr's uninstall deletes by path without checking
// its own marker, and Sidecar only ever removes what invokes report-session.
func TestDevinUninstallRemovesOnlySidecarsEntries(t *testing.T) {
	svc, _, paths := devinFixture(t)
	devinSetUp(t, paths)
	writeFileForTest(t, paths.Settings, `{
  "theme_mode": "dark",
  "hooks": {
    "Stop": [
      {"hooks": [{"type": "command", "command": "notify-send done", "timeout": 5}]}
    ],
    "Notification": [
      {"matcher": "*", "hooks": [{"type": "command", "command": "my-own-hook", "timeout": 2}]}
    ]
  }
}`)

	applyTo(t, svc, DevinProvider, ActionInstall)
	applyTo(t, svc, DevinProvider, ActionUninstall)

	got := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)
	if got["theme_mode"] != "dark" {
		t.Fatal("uninstall lost the user's own setting")
	}
	hooks := got["hooks"].(map[string]any)
	if len(hooks) != 2 {
		t.Fatalf("uninstall left hooks = %v, want only the user's two events", hooks)
	}
	stop := hooks["Stop"].([]any)
	if len(stop) != 1 {
		t.Fatalf("the user's Stop hook did not survive as the only group: %v", stop)
	}
	entry := stop[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if entry["command"] != "notify-send done" {
		t.Fatalf("the user's Stop command was rewritten: %v", entry["command"])
	}
	if _, has := hooks["Notification"]; !has {
		t.Fatal("uninstall removed an event Sidecar never wrote to")
	}
	if got := devinStatus(t, svc); got.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("status after uninstall = %q", got.Status)
	}
}

// TestDevinNeverAdoptsAnEntryThatMerelyLooksLikeSidecars is the ownership rule
// stated at this provider, because uninstall deletes what it owns.
func TestDevinNeverAdoptsAnEntryThatMerelyLooksLikeSidecars(t *testing.T) {
	svc, _, paths := devinFixture(t)
	devinSetUp(t, paths)
	writeFileForTest(t, paths.Settings, `{
  "hooks": {
    "SessionStart": [
      {"hooks": [
        {"type": "command", "command": "echo sidecar agent report-session --kind devin", "timeout": 10},
        {"type": "command", "command": "my-wrapper sidecar agent report-session --kind devin --hook-stdin", "timeout": 10}
      ]}
    ]
  }
}`)
	before := readFileForTest(t, paths.Settings)

	if got := devinStatus(t, svc); got.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("status = %q; neither entry is Sidecar's", got.Status)
	}
	applyTo(t, svc, DevinProvider, ActionInstall)

	after := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)
	entries := after["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)["hooks"].([]any)
	if len(entries) != 2 {
		t.Fatalf("the user's two lookalike entries were not preserved verbatim: %v", entries)
	}

	// And uninstall gives their file back exactly as it was.
	applyTo(t, svc, DevinProvider, ActionUninstall)
	if got := mustParseAny(t, readFileForTest(t, paths.Settings)); !reflect.DeepEqual(got, mustParseAny(t, before)) {
		t.Fatalf("uninstall did not restore the user's file:\nbefore %s\nafter  %s", before, readFileForTest(t, paths.Settings))
	}
}

// --- where Devin's configuration lives ---

// TestDevinFollowsXDGConfigHome is the config-dir rule. Herdr's devin_dir reads
// $XDG_CONFIG_HOME and falls back to ~/.config/devin, and honouring it is both
// what finds a relocated Devin and what lets a proof run redirect the provider
// away from the user's real configuration.
func TestDevinFollowsXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "xdg")

	unset := devinSpec().pathsFor(Env{Home: home})
	if want := filepath.Join(home, ".config", "devin", "config.json"); unset.Settings != want {
		t.Fatalf("with no XDG_CONFIG_HOME the settings path is %q, want %q", unset.Settings, want)
	}
	set := devinSpec().pathsFor(Env{Home: home, ConfigHome: elsewhere})
	if want := filepath.Join(elsewhere, "devin", "config.json"); set.Settings != want {
		t.Fatalf("with XDG_CONFIG_HOME the settings path is %q, want %q", set.Settings, want)
	}
	tilde := devinSpec().pathsFor(Env{Home: home, ConfigHome: "~/somewhere"})
	if want := filepath.Join(home, "somewhere", "devin", "config.json"); tilde.Settings != want {
		t.Fatalf("a tilde in XDG_CONFIG_HOME resolved to %q, want %q", tilde.Settings, want)
	}
	blank := devinSpec().pathsFor(Env{Home: home, ConfigHome: "   "})
	if want := filepath.Join(home, ".config", "devin", "config.json"); blank.Settings != want {
		t.Fatalf("a whitespace-only XDG_CONFIG_HOME resolved to %q, want the default %q", blank.Settings, want)
	}
}

// TestDevinRefusesWhenItsConfigurationDirectoryIsAbsent is the rule this shape
// adds over Claude's. Devin's directory can be moved anywhere by a variable, so
// a Sidecar that created it would write a settings file into a typo and then
// report the integration as current forever.
func TestDevinRefusesWhenItsConfigurationDirectoryIsAbsent(t *testing.T) {
	svc, _, paths := devinFixture(t)

	_, err := svc.Plan(DevinProvider, ActionInstall)
	r, ok := AsRefusal(err)
	if !ok {
		t.Fatalf("install was not refused with a Refusal: %v", err)
	}
	if r.Code != RefuseNotInstalled {
		t.Fatalf("refusal code %q, want %q", r.Code, RefuseNotInstalled)
	}
	if !strings.Contains(r.Message, "run devin once") {
		t.Fatalf("the refusal does not say how to fix it: %q", r.Message)
	}
	if _, err := os.Stat(paths.Dir); !os.IsNotExist(err) {
		t.Fatalf("the refused plan created %s", paths.Dir)
	}
}

// TestDevinWithoutTheCLIOffersNothing keeps the provider-missing rule at this
// provider: nothing is written for a CLI that is not installed.
func TestDevinWithoutTheCLIOffersNothing(t *testing.T) {
	svc, _, paths := devinFixture(t, withoutDevin)
	devinSetUp(t, paths)

	st := devinStatus(t, svc)
	if st.Status != agentlifecycle.StatusProviderMissing {
		t.Fatalf("status = %q, want provider-missing", st.Status)
	}
	if st.EffectiveTier != agentlifecycle.TierScreenFallback {
		t.Fatalf("tier = %q with no provider installed", st.EffectiveTier)
	}
	_, err := svc.Plan(DevinProvider, ActionInstall)
	r, ok := AsRefusal(err)
	if !ok || r.Code != RefuseProviderMissing {
		t.Fatalf("install refusal = %v, want provider_missing", err)
	}
}

// TestDevinBacksUpTheUsersFileBeforeRewritingIt is the recoverability rule.
func TestDevinBacksUpTheUsersFileBeforeRewritingIt(t *testing.T) {
	svc, _, paths := devinFixture(t)
	devinSetUp(t, paths)
	original := `{"theme_mode":"dark"}`
	writeFileForTest(t, paths.Settings, original)

	applyTo(t, svc, DevinProvider, ActionInstall)
	if got := readFileForTest(t, paths.Backup); got != original {
		t.Fatalf("the backup holds %q, want the file as it was", got)
	}
}

func devinStatus(t *testing.T, s Service) Status {
	t.Helper()
	st, err := s.Status(DevinProvider)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}
