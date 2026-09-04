package agentintegration

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Qwen Code adapter suite runs entirely inside t.TempDir with an injected Env.

func qwenFixture(t *testing.T, opts ...func(*Env)) (Service, Env, sessionEntryPaths) {
	t.Helper()
	home := t.TempDir()
	env := Env{
		Home: home,
		LookPath: func(file string) (string, error) {
			if file == QwenProvider {
				return filepath.Join(home, "bin", "qwen"), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "0.23.0" },
		UID:             os.Getuid(),
	}
	for _, o := range opts {
		o(&env)
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, qwenSpec().pathsFor(env)
}

func qwenStatus(t *testing.T, s Service) Status {
	t.Helper()
	st, err := s.Status(QwenProvider)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

// TestQwenInstallsUnderExactlyTheEventUpstreamNames is the table diff, and its
// fixture carries a fourth column no other provider's does: the timeout in the
// unit Qwen counts in.
func TestQwenInstallsUnderExactlyTheEventUpstreamNames(t *testing.T) {
	rows := readQwenHookTable(t)
	events := qwenSpec().eventNames()
	if len(rows) != len(events) {
		t.Fatalf("the fixture records %d events and the port ships %d", len(rows), len(events))
	}
	for i, row := range rows {
		if row[0] != events[i] {
			t.Errorf("event %d: the port installs under %q, the fixture records %q", i, events[i], row[0])
		}
		if row[1] != "*" {
			t.Errorf("event %d: the fixture records matcher %q, but install_qwen passes \"*\"", i, row[1])
		}
		if row[3] != "10000" {
			t.Errorf("event %d: the fixture records timeout_ms %q, want 10000", i, row[3])
		}
	}
	if qwenSpec().matcher == nil || *qwenSpec().matcher != "*" {
		t.Fatalf("the Qwen group's matcher is %v, want \"*\"", qwenSpec().matcher)
	}
}

// TestQwenCountsItsTimeoutInMilliseconds is the one number in this group of
// four that is not what it looks like.
//
// Qwen's hooks reference documents a command hook's timeout in milliseconds,
// where Claude, Codex, Droid and Qoder all count seconds. A 10 written here
// would be a ten-millisecond budget: the report process would be killed before
// it opened the store, and silently, because a hook surface fails open. So the
// relationship between the two constants is asserted rather than left to a
// reader to notice.
func TestQwenCountsItsTimeoutInMilliseconds(t *testing.T) {
	if QwenHookTimeoutMillis != hookTimeoutSec*1000 {
		t.Fatalf("QwenHookTimeoutMillis = %d; it is meant to be the same ten seconds every other entry gets, in Qwen's unit",
			QwenHookTimeoutMillis)
	}
	if qwenSpec().timeout == hookTimeoutSec {
		t.Fatal("the Qwen entry carries the seconds constant, which Qwen would read as ten milliseconds")
	}
	if !strings.Contains(string(qwenSpec().content()), `"timeout": 10000`) {
		t.Fatalf("the canonical Qwen entry does not carry a millisecond timeout:\n%s", qwenSpec().content())
	}
	// And every other provider in this group still counts seconds, which is
	// what makes the difference a difference rather than a global change.
	for _, spec := range []sessionEntrySpec{devinSpec(), droidSpec(), qoderCLISpec()} {
		if spec.timeout != hookTimeoutSec {
			t.Errorf("%s carries timeout %d; it counts in seconds", spec.provider, spec.timeout)
		}
	}
}

func TestQwenBundledEntryMatchesTheRegistry(t *testing.T) {
	asset := NewQwenAdapter().settingsAsset()
	capability, known := agentlifecycle.CapabilityForSource(asset.Source)
	if !known {
		t.Fatalf("no capability entry for %s", asset.Source)
	}
	if capability.AssetVersion != asset.Version {
		t.Fatalf("asset version %q but registry records %q", asset.Version, capability.AssetVersion)
	}
	// Traced against a released 0.23.0, so this is the one of the four
	// session-identity ports that reached its tier. It is also that tier's
	// ceiling: the asset installs one entry and reports which conversation
	// occupies the pane, never what state it is in.
	if capability.Tier != agentlifecycle.TierSessionIdentity {
		t.Fatalf("tier %q, want session-identity", capability.Tier)
	}
	if capability.Evidence != agentlifecycle.EvidenceRealTrace {
		t.Fatalf("evidence %q, want real-trace", capability.Evidence)
	}
	if len(capability.Covered) != 1 || !capability.Covers(agentlifecycle.TransitionSessionIdentity) {
		t.Fatalf("covered %v claims more than one SessionStart proves", capability.Covered)
	}
	if want := "sidecar agent report-session --kind qwen --hook-stdin"; reportSessionCommand(QwenProvider) != want {
		t.Fatalf("command %q, want %q", reportSessionCommand(QwenProvider), want)
	}
}

// TestQwenFollowsQwenHome is the config-dir rule. QWEN_HOME is Qwen Code's own
// override, resolved in place of ~/.qwen by getGlobalQwenDir.
func TestQwenFollowsQwenHome(t *testing.T) {
	home := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "qwen-elsewhere")

	unset := qwenSpec().pathsFor(Env{Home: home})
	if want := filepath.Join(home, ".qwen", "settings.json"); unset.Settings != want {
		t.Fatalf("with no override the settings path is %q, want %q", unset.Settings, want)
	}
	set := qwenSpec().pathsFor(Env{Home: home, QwenHome: elsewhere})
	if want := filepath.Join(elsewhere, "settings.json"); set.Settings != want {
		t.Fatalf("with QWEN_HOME the settings path is %q, want %q", set.Settings, want)
	}
	tilde := qwenSpec().pathsFor(Env{Home: home, QwenHome: "~/somewhere"})
	if want := filepath.Join(home, "somewhere", "settings.json"); tilde.Settings != want {
		t.Fatalf("a tilde in QWEN_HOME resolved to %q, want %q", tilde.Settings, want)
	}
	blank := qwenSpec().pathsFor(Env{Home: home, QwenHome: "   "})
	if want := filepath.Join(home, ".qwen", "settings.json"); blank.Settings != want {
		t.Fatalf("a whitespace-only QWEN_HOME resolved to %q, want the default %q", blank.Settings, want)
	}
	// QWEN_HOME must not move any neighbouring provider, and no neighbouring
	// variable may move Qwen. Four adapters reading one Env is exactly where a
	// misrouted write would come from.
	env := Env{Home: home, QwenHome: elsewhere, QoderConfigDir: filepath.Join(t.TempDir(), "qoder")}
	if got := qoderCLISpec().pathsFor(env).Dir; got == elsewhere {
		t.Fatal("QWEN_HOME moved the Qoder directory")
	}
	if got := qwenSpec().pathsFor(env).Dir; got != elsewhere {
		t.Fatalf("the Qwen directory is %q, want %q", got, elsewhere)
	}
	if got := droidSpec().pathsFor(env).Dir; got != filepath.Join(home, ".factory") {
		t.Fatalf("a neighbouring override moved the Droid directory to %q", got)
	}
}

func TestQwenInstallWritesTheEntryAndPreservesTheRest(t *testing.T) {
	svc, _, paths := qwenFixture(t)
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFileForTest(t, paths.Settings, `{"theme":"Default","selectedAuthType":"openai"}`)

	applyTo(t, svc, QwenProvider, ActionInstall)

	got := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)
	if got["theme"] != "Default" || got["selectedAuthType"] != "openai" {
		t.Fatalf("the user's own settings were lost: %v", got)
	}
	groups := got["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(groups) != 1 {
		t.Fatalf("hooks.SessionStart has %d groups, want one", len(groups))
	}
	group := groups[0].(map[string]any)
	if group["matcher"] != "*" {
		t.Fatalf("matcher = %v", group["matcher"])
	}
	entry := group["hooks"].([]any)[0].(map[string]any)
	if entry["command"] != reportSessionCommand(QwenProvider) {
		t.Fatalf("command = %v", entry["command"])
	}
	if entry["timeout"] != float64(QwenHookTimeoutMillis) {
		t.Fatalf("timeout = %v, want %d milliseconds", entry["timeout"], QwenHookTimeoutMillis)
	}
	if st := qwenStatus(t, svc); st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after install = %q (%s)", st.Status, st.Message)
	}

	applyTo(t, svc, QwenProvider, ActionUninstall)
	after := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)
	if _, has := after["hooks"]; has {
		t.Fatalf("uninstall left a hooks key behind: %v", after["hooks"])
	}
	if after["theme"] != "Default" {
		t.Fatal("uninstall lost the user's own setting")
	}
}

func TestQwenRefusesWhenItsConfigurationDirectoryIsAbsent(t *testing.T) {
	svc, _, paths := qwenFixture(t)

	_, err := svc.Plan(QwenProvider, ActionInstall)
	r, ok := AsRefusal(err)
	if !ok || r.Code != RefuseNotInstalled {
		t.Fatalf("install refusal = %v, want not_installed", err)
	}
	if !strings.Contains(r.Message, "QWEN_HOME") {
		t.Fatalf("the refusal does not name the override: %q", r.Message)
	}
	if _, err := os.Stat(paths.Dir); !os.IsNotExist(err) {
		t.Fatalf("the refused plan created %s", paths.Dir)
	}
}

func TestQwenNeverAdoptsAnEntryThatMerelyLooksLikeSidecars(t *testing.T) {
	svc, _, paths := qwenFixture(t)
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFileForTest(t, paths.Settings, `{
  "hooks": {
    "SessionStart": [
      {"matcher": "*", "hooks": [{"type": "command", "command": "echo sidecar agent report-session --kind qwen --hook-stdin", "timeout": 10000}]}
    ]
  }
}`)
	before := readFileForTest(t, paths.Settings)
	if got := qwenStatus(t, svc); got.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("status = %q; an echoed command is not Sidecar's entry", got.Status)
	}
	applyTo(t, svc, QwenProvider, ActionInstall)
	applyTo(t, svc, QwenProvider, ActionUninstall)
	if got := readFileForTest(t, paths.Settings); mustParseAny(t, got) == nil || !strings.Contains(got, "echo sidecar") {
		t.Fatalf("uninstall did not leave the user's lookalike entry alone:\nbefore %s\nafter  %s", before, got)
	}
}

// readQwenHookTable reads Qwen's four-column fixture. It is not
// readProviderHookTable because only this provider records a timeout unit, and
// recording it in a column no other file has is the point.
func readQwenHookTable(t *testing.T) [][]string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "qwen", "hook-table.tsv"))
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
		if len(fields) != 4 {
			t.Fatalf("qwen hook-table.tsv row %q has %d columns, want 4", line, len(fields))
		}
		rows = append(rows, fields)
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}
