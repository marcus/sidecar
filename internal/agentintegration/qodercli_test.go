package agentintegration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Qoder CLI adapter suite runs entirely inside t.TempDir with an injected Env.

func qoderCLIFixture(t *testing.T, opts ...func(*Env)) (Service, Env, sessionHookPaths) {
	t.Helper()
	home := t.TempDir()
	env := Env{
		Home: home,
		LookPath: func(file string) (string, error) {
			if file == QoderCLICommand {
				return filepath.Join(home, "bin", QoderCLICommand), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "1.2.0" },
		UID:             os.Getuid(),
	}
	for _, o := range opts {
		o(&env)
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, qoderCLIIntegration().pathsFor(env)
}

func qoderCLIStatus(t *testing.T, s Service) Status {
	t.Helper()
	st, err := s.Status(QoderCLIProvider)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

// TestQoderCLIInstallsUnderExactlyTheEventUpstreamNames is the table diff. The
// matcher column is the interesting one: this is the first provider in the
// group whose group carries one.
func TestQoderCLIInstallsUnderExactlyTheEventUpstreamNames(t *testing.T) {
	rows := readProviderHookTable(t, "qodercli")
	events := qoderCLIIntegration().spec.eventNames()
	if len(rows) != len(events) {
		t.Fatalf("the fixture records %d events and the port ships %d", len(rows), len(events))
	}
	for i, row := range rows {
		if row[0] != events[i] {
			t.Errorf("event %d: the port installs under %q, the fixture records %q", i, events[i], row[0])
		}
		if row[1] != "*" {
			t.Errorf("event %d: the fixture records matcher %q, but install_qodercli passes \"*\"", i, row[1])
		}
	}
	if qoderCLIIntegration().spec.matcher == nil || *qoderCLIIntegration().spec.matcher != "*" {
		t.Fatalf("the Qoder group's matcher is %v, want \"*\"", qoderCLIIntegration().spec.matcher)
	}
	if !strings.Contains(string(qoderCLIIntegration().canonicalFile()), `"matcher": "*"`) {
		t.Fatalf("the canonical Qoder group does not render the matcher:\n%s", qoderCLIIntegration().canonicalFile())
	}
}

// TestTheProviderIdAndTheCommandDifferOnPurpose pins the split the catalog
// records. `qodercli` is Herdr's label and Sidecar's id everywhere; `qoder` is
// the binary every official example types. Looking the wrong one up on PATH
// would report the provider missing on a machine that has it.
func TestTheProviderIdAndTheCommandDifferOnPurpose(t *testing.T) {
	if QoderCLIProvider == QoderCLICommand {
		t.Fatal("the id and the command are the same; this test is describing something that no longer exists")
	}
	home := t.TempDir()
	// Only the command resolves; the id does not. A status that finds the
	// provider proves the lookup uses the command.
	env := Env{
		Home: home,
		LookPath: func(file string) (string, error) {
			if file == QoderCLICommand {
				return filepath.Join(home, "bin", QoderCLICommand), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "1.2.0" },
		UID:             os.Getuid(),
	}
	st := NewQoderCLIAdapter().Inspect(env)
	if st.ProviderPath == "" {
		t.Fatal("the adapter looked up the catalog id rather than the command, so an installed qoder reads as missing")
	}
	// And the kind the hook claims is the id, because that is what the catalog
	// resolves and what a report carries.
	if !strings.Contains(reportSessionCommand(QoderCLIProvider), "--kind qodercli") {
		t.Fatalf("the hook command claims the wrong kind: %q", reportSessionCommand(QoderCLIProvider))
	}
}

func TestQoderCLIBundledEntryMatchesTheRegistry(t *testing.T) {
	asset := NewQoderCLIAdapter().asset()
	capability, known := agentlifecycle.CapabilityForSource(asset.Source)
	if !known {
		t.Fatalf("no capability entry for %s", asset.Source)
	}
	if capability.AssetVersion != asset.Version {
		t.Fatalf("asset version %q but registry records %q", asset.Version, capability.AssetVersion)
	}
	if capability.Tier != agentlifecycle.TierScreenFallback {
		t.Fatalf("tier %q, want screen-fallback until a released Qoder is traced", capability.Tier)
	}
	if capability.Evidence != agentlifecycle.EvidenceDocsOnly {
		t.Fatalf("evidence %q, want docs-only", capability.Evidence)
	}
}

// TestQoderCLIFollowsItsConfigDirOverride is the config-dir rule.
func TestQoderCLIFollowsItsConfigDirOverride(t *testing.T) {
	home := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "qoder-elsewhere")

	unset := qoderCLIIntegration().pathsFor(Env{Home: home})
	if want := filepath.Join(home, ".qoder", "settings.json"); unset.File != want {
		t.Fatalf("with no override the settings path is %q, want %q", unset.File, want)
	}
	set := qoderCLIIntegration().pathsFor(Env{Home: home, QoderConfigDir: elsewhere})
	if want := filepath.Join(elsewhere, "settings.json"); set.File != want {
		t.Fatalf("with QODER_CONFIG_DIR the settings path is %q, want %q", set.File, want)
	}
	tilde := qoderCLIIntegration().pathsFor(Env{Home: home, QoderConfigDir: "~/somewhere"})
	if want := filepath.Join(home, "somewhere", "settings.json"); tilde.File != want {
		t.Fatalf("a tilde in QODER_CONFIG_DIR resolved to %q, want %q", tilde.File, want)
	}
	blank := qoderCLIIntegration().pathsFor(Env{Home: home, QoderConfigDir: "   "})
	if want := filepath.Join(home, ".qoder", "settings.json"); blank.File != want {
		t.Fatalf("a whitespace-only QODER_CONFIG_DIR resolved to %q, want the default %q", blank.File, want)
	}
	// The override moves the whole directory rather than being joined under a
	// provider-named child, which is Herdr's config_dir_from_env_or_home rule.
	if got := qoderCLIIntegration().pathsFor(Env{Home: home, QoderConfigDir: elsewhere}).Dir; got != elsewhere {
		t.Fatalf("the override resolved to directory %q, want %q", got, elsewhere)
	}
}

func TestQoderCLIInstallWritesTheEntryAndPreservesTheRest(t *testing.T) {
	svc, _, paths := qoderCLIFixture(t)
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFileForTest(t, paths.File, `{
  "theme": "dark",
  "hooks": {
    "SessionStart": [
      {"matcher": "startup", "hooks": [{"type": "command", "command": "my-own-hook", "timeout": 30}]}
    ]
  }
}`)

	applyTo(t, svc, QoderCLIProvider, ActionInstall)

	got := mustParseAny(t, readFileForTest(t, paths.File)).(map[string]any)
	if got["theme"] != "dark" {
		t.Fatal("the user's own setting was lost")
	}
	groups := got["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(groups) != 2 {
		t.Fatalf("hooks.SessionStart has %d groups, want the user's plus Sidecar's", len(groups))
	}
	if first := groups[0].(map[string]any); first["matcher"] != "startup" {
		t.Fatalf("the user's group was rewritten: %v", first)
	}
	ours := groups[1].(map[string]any)
	if ours["matcher"] != "*" {
		t.Fatalf("Sidecar's group matcher = %v, want \"*\"", ours["matcher"])
	}
	entry := ours["hooks"].([]any)[0].(map[string]any)
	if entry["command"] != reportSessionCommand(QoderCLIProvider) {
		t.Fatalf("command = %v", entry["command"])
	}
	// Seconds, which is what Qoder documents (default 600).
	if entry["timeout"] != float64(hookTimeoutSec) {
		t.Fatalf("timeout = %v, want %d seconds", entry["timeout"], hookTimeoutSec)
	}
	if st := qoderCLIStatus(t, svc); st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after install = %q (%s)", st.Status, st.Message)
	}

	applyTo(t, svc, QoderCLIProvider, ActionUninstall)
	after := mustParseAny(t, readFileForTest(t, paths.File)).(map[string]any)
	left := after["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(left) != 1 {
		t.Fatalf("uninstall left %d groups, want only the user's", len(left))
	}
	if left[0].(map[string]any)["matcher"] != "startup" {
		t.Fatalf("uninstall removed the wrong group: %v", left[0])
	}
}

// TestAChangedMatcherReadsAsDamageRatherThanAsCurrent is what the matcher buys.
// An entry moved under a different matcher no longer fires the way Sidecar
// qualified it, and reporting it as current would be reporting an installation
// that behaves differently from the one that earned the entry.
func TestAChangedMatcherReadsAsDamageRatherThanAsCurrent(t *testing.T) {
	svc, _, paths := qoderCLIFixture(t)
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	applyTo(t, svc, QoderCLIProvider, ActionInstall)

	edited := strings.Replace(readFileForTest(t, paths.File), `"matcher": "*"`, `"matcher": "startup"`, 1)
	if edited == readFileForTest(t, paths.File) {
		t.Fatal("the fixture did not change the matcher")
	}
	writeFileForTest(t, paths.File, edited)

	st := qoderCLIStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("status = %q, want needs-repair for an entry under a changed matcher", st.Status)
	}
	applyTo(t, svc, QoderCLIProvider, ActionRepair)
	if got := qoderCLIStatus(t, svc); got.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after repair = %q (%s)", got.Status, got.Message)
	}
}

func TestQoderCLIRefusesWhenItsConfigurationDirectoryIsAbsent(t *testing.T) {
	svc, _, paths := qoderCLIFixture(t)

	_, err := svc.Plan(QoderCLIProvider, ActionInstall)
	r, ok := AsRefusal(err)
	if !ok || r.Code != RefuseNotInstalled {
		t.Fatalf("install refusal = %v, want not_installed", err)
	}
	if !strings.Contains(r.Message, "QODER_CONFIG_DIR") {
		t.Fatalf("the refusal does not name the override: %q", r.Message)
	}
	if _, err := os.Stat(paths.Dir); !os.IsNotExist(err) {
		t.Fatalf("the refused plan created %s", paths.Dir)
	}
}
