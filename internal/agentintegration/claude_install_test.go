package agentintegration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Claude adapter suite runs entirely inside t.TempDir with an injected
// Env. That is not a nicety here: this machine's real ~/.claude carries live
// hooks another tool installed, and a test that touched it would be rewriting
// the configuration of the very agent running the test.

func claudeFixture(t *testing.T, opts ...func(*Env)) (Service, Env, claudePaths) {
	t.Helper()
	home := t.TempDir()
	env := Env{
		Home:       home,
		ConfigHome: filepath.Join(home, ".config"),
		LookPath: func(file string) (string, error) {
			if file == ClaudeProvider {
				return filepath.Join(home, "bin", "claude"), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "2.1.220" },
		UID:             os.Getuid(),
	}
	for _, o := range opts {
		o(&env)
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, claudePathsFor(env)
}

func withoutClaude(e *Env) {
	e.LookPath = func(string) (string, error) { return "", errors.New("not found") }
}

func claudeStatus(t *testing.T, s Service) Status {
	t.Helper()
	st, err := s.Status(ClaudeProvider)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

func applyTo(t *testing.T, s Service, provider string, act Action) Plan {
	t.Helper()
	p, err := s.Apply(provider, act)
	if err != nil {
		t.Fatalf("%s %s: %v", provider, act, err)
	}
	return p
}

func writeFileForTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// mustParseAny is semantic JSON equality's half: unmarshal or fail.
func mustParseAny(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	return v
}

func TestClaudeBundledEntryMatchesTheRegistry(t *testing.T) {
	asset := (ClaudeAdapter{}).settingsAsset()
	capability, known := agentlifecycle.CapabilityForSource(asset.Source)
	if !known {
		t.Fatalf("no capability entry for %s", asset.Source)
	}
	if capability.AssetVersion != asset.Version {
		t.Fatalf("asset version %q but registry records %q", asset.Version, capability.AssetVersion)
	}
	// Session identity is the only transition the shipped hook exercises, and
	// the tier must never rise above it: lifecycle authority for Claude is
	// Phase D's to earn with traces, not this asset's to claim.
	if capability.Tier != agentlifecycle.TierSessionIdentity {
		t.Fatalf("tier %q, want session-identity", capability.Tier)
	}
	if !reflect.DeepEqual(capability.Covered, []agentlifecycle.Transition{agentlifecycle.TransitionSessionIdentity}) {
		t.Fatalf("covered %v claims more than the shipped hook proves", capability.Covered)
	}
	// The command the entry runs is fixed argv invoking Sidecar's own verb,
	// with the payload arriving on stdin — nothing of the provider's is ever
	// interpolated into it.
	if want := "sidecar agent report-session --kind claude --hook-stdin"; reportSessionCommand(ClaudeProvider) != want {
		t.Fatalf("command %q, want %q", reportSessionCommand(ClaudeProvider), want)
	}
	if !invokesReportSession(reportSessionCommand(ClaudeProvider)) {
		t.Fatal("the canonical command does not satisfy its own ownership test")
	}
}

func TestClaudeInstallIntoACleanTreeIsExplicitAndIdempotent(t *testing.T) {
	svc, _, paths := claudeFixture(t)

	if got := claudeStatus(t, svc).Status; got != agentlifecycle.StatusNotInstalled {
		t.Fatalf("before install: %s", got)
	}

	p := applyTo(t, svc, ClaudeProvider, ActionInstall)
	if p.Unchanged {
		t.Fatal("the first install reported unchanged")
	}
	if p.StatusAfter != agentlifecycle.StatusCurrent {
		t.Fatalf("after install: %s", p.StatusAfter)
	}

	content := readFileForTest(t, paths.Settings)
	want := `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "sidecar agent report-session --kind claude --hook-stdin",
            "timeout": 10
          }
        ]
      }
    ]
  }
}
`
	if content != want {
		t.Fatalf("settings.json:\n%s\nwant:\n%s", content, want)
	}

	st := claudeStatus(t, svc)
	if st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after install: %s (%s)", st.Status, st.Message)
	}
	if st.EffectiveTier != agentlifecycle.TierSessionIdentity {
		t.Fatalf("tier %q, want session-identity", st.EffectiveTier)
	}
	if st.InstalledVersion != ClaudeAssetVersion {
		t.Fatalf("installed version %q", st.InstalledVersion)
	}

	again := applyTo(t, svc, ClaudeProvider, ActionInstall)
	if !again.Unchanged || len(again.Ops) != 0 {
		t.Fatalf("reinstall over a current install was not a visible no-op: %+v", again)
	}
	if readFileForTest(t, paths.Settings) != want {
		t.Fatal("an unchanged reinstall still rewrote the file")
	}
}

func TestClaudeInstallPreservesEveryUnrelatedSettingAndHook(t *testing.T) {
	svc, _, paths := claudeFixture(t)
	original := `{
  "model": "opus",
  "env": {
    "FOO": "bar"
  },
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "my-audit.sh"
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "matcher": "startup",
        "hooks": [
          {
            "type": "command",
            "command": "echo hello",
            "timeout": 5
          }
        ]
      }
    ]
  },
  "permissions": {
    "allow": [
      "Bash(ls:*)"
    ]
  }
}
`
	writeFileForTest(t, paths.Settings, original)

	applyTo(t, svc, ClaudeProvider, ActionInstall)

	after := readFileForTest(t, paths.Settings)
	got := mustParseAny(t, after).(map[string]any)

	// Every unrelated top-level key survives with its value intact.
	wantTop := mustParseAny(t, original).(map[string]any)
	for _, key := range []string{"model", "env", "permissions"} {
		if !reflect.DeepEqual(got[key], wantTop[key]) {
			t.Fatalf("top-level %q changed: %v -> %v", key, wantTop[key], got[key])
		}
	}
	// The user's hooks survive; Sidecar's group is appended after them.
	hooks := got["hooks"].(map[string]any)
	if !reflect.DeepEqual(hooks["PreToolUse"], wantTop["hooks"].(map[string]any)["PreToolUse"]) {
		t.Fatal("the unrelated PreToolUse hook changed")
	}
	sessionStart := hooks["SessionStart"].([]any)
	if len(sessionStart) != 2 {
		t.Fatalf("SessionStart holds %d groups, want the user's and Sidecar's", len(sessionStart))
	}
	if !reflect.DeepEqual(sessionStart[0], wantTop["hooks"].(map[string]any)["SessionStart"].([]any)[0]) {
		t.Fatal("the user's SessionStart group changed")
	}
	sidecarGroup := sessionStart[1].(map[string]any)
	if sidecarGroup["matcher"] != "*" {
		t.Fatalf("Sidecar's group matcher: %v", sidecarGroup["matcher"])
	}
	// Order is preserved too: the file is not alphabetized out from under the
	// user, so "model" still leads and "permissions" still trails.
	if !strings.HasPrefix(after, "{\n  \"model\"") || strings.Index(after, `"permissions"`) < strings.Index(after, `"hooks"`) {
		t.Fatalf("top-level key order was not preserved:\n%s", after)
	}

	// Uninstall returns the file to the original bytes exactly: same keys,
	// same order, same formatting, Sidecar's group gone.
	applyTo(t, svc, ClaudeProvider, ActionUninstall)
	if got := readFileForTest(t, paths.Settings); got != original {
		t.Fatalf("uninstall did not restore the original file:\n%s\nwant:\n%s", got, original)
	}
}

func TestClaudeDryRunAndTheRealRunDescribeTheSameOperations(t *testing.T) {
	svc, _, paths := claudeFixture(t)
	writeFileForTest(t, paths.Settings, `{"model": "opus"}`)

	preview, err := svc.Plan(ClaudeProvider, ActionInstall)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.DryRun {
		t.Fatal("a plan from Plan() is not marked dry-run")
	}
	before := snapshot(t, filepath.Dir(paths.Dir))

	real := applyTo(t, svc, ClaudeProvider, ActionInstall)

	// The preview and the mutation are the same ops byte for byte; only the
	// execution flags differ.
	preview.DryRun, real.Applied, real.StatusAfter = false, false, preview.StatusAfter
	pb, _ := json.Marshal(preview)
	rb, _ := json.Marshal(real)
	if string(pb) != string(rb) {
		t.Fatalf("dry-run and real run describe different plans:\n%s\n%s", pb, rb)
	}
	if reflect.DeepEqual(before, snapshot(t, filepath.Dir(paths.Dir))) {
		t.Fatal("the real run changed nothing")
	}
}

func TestClaudeUninstallRemovesAFileThatHeldOnlySidecarsEntry(t *testing.T) {
	svc, _, paths := claudeFixture(t)
	applyTo(t, svc, ClaudeProvider, ActionInstall)
	p := applyTo(t, svc, ClaudeProvider, ActionUninstall)
	if p.Unchanged {
		t.Fatal("uninstall reported unchanged")
	}
	if _, err := os.Lstat(paths.Settings); !os.IsNotExist(err) {
		t.Fatal("a settings.json that held nothing but Sidecar's entry was left behind")
	}
	if got := claudeStatus(t, svc).Status; got != agentlifecycle.StatusNotInstalled {
		t.Fatalf("after uninstall: %s", got)
	}
	// A second uninstall has nothing to do and says so.
	if p := applyTo(t, svc, ClaudeProvider, ActionUninstall); !p.Unchanged {
		t.Fatal("uninstalling nothing was not a visible no-op")
	}
}

func TestClaudeNeverAdoptsAForeignLookalikeEntry(t *testing.T) {
	svc, _, paths := claudeFixture(t)
	// Each of these mentions or resembles Sidecar's command without being an
	// invocation of it. None may be adopted, rewritten, or deleted.
	original := `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "echo sidecar agent report-session"
          },
          {
            "type": "command",
            "command": "sidecar-helper agent report-session --kind claude --hook-stdin"
          },
          {
            "type": "command",
            "command": "sidecar agent report-session-v2 --kind claude"
          },
          {
            "type": "command",
            "command": "my-wrapper.sh --then sidecar agent report-session"
          }
        ]
      }
    ]
  }
}
`
	writeFileForTest(t, paths.Settings, original)

	if got := claudeStatus(t, svc).Status; got != agentlifecycle.StatusNotInstalled {
		t.Fatalf("lookalikes read as %s, want not-installed", got)
	}

	applyTo(t, svc, ClaudeProvider, ActionInstall)
	after := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)
	groups := after["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(groups) != 2 {
		t.Fatalf("%d SessionStart groups, want the foreign one plus Sidecar's", len(groups))
	}
	if !reflect.DeepEqual(groups[0], mustParseAny(t, original).(map[string]any)["hooks"].(map[string]any)["SessionStart"].([]any)[0]) {
		t.Fatal("a foreign lookalike entry was modified")
	}

	// Uninstall removes only Sidecar's group; the lookalikes stay.
	applyTo(t, svc, ClaudeProvider, ActionUninstall)
	if got := readFileForTest(t, paths.Settings); got != original {
		t.Fatalf("uninstall touched foreign lookalike entries:\n%s", got)
	}
}

func TestClaudeMalformedSettingsRefuseRatherThanClobber(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"invalid json", `{"hooks": {`},
		{"hooks is an array", `{"hooks": []}`},
		{"event is an object", `{"hooks": {"SessionStart": {}}}`},
		{"entry is a string", `{"hooks": {"SessionStart": [{"hooks": ["nope"]}]}}`},
		{"trailing garbage", `{"hooks": {}} trailing`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, paths := claudeFixture(t)
			writeFileForTest(t, paths.Settings, tc.content)

			st := claudeStatus(t, svc)
			if st.Status != agentlifecycle.StatusNeedsRepair {
				t.Fatalf("status %s, want needs-repair (state unknown)", st.Status)
			}

			for _, act := range Actions() {
				_, err := svc.Apply(ClaudeProvider, act)
				r := refusalFrom(t, err)
				if r.Code != RefuseUnreadable && r.Code != RefuseNeedsRepair {
					t.Fatalf("%s refused with %q", act, r.Code)
				}
			}
			if readFileForTest(t, paths.Settings) != tc.content {
				t.Fatal("a refused mutation still changed the file")
			}
		})
	}
}

func TestClaudeSymlinkedSettingsAreRefusedUnwritten(t *testing.T) {
	svc, env, paths := claudeFixture(t)
	target := filepath.Join(env.Home, "elsewhere.json")
	writeFileForTest(t, target, `{}`)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, paths.Settings); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Apply(ClaudeProvider, ActionInstall)
	if r := refusalFrom(t, err); r.Code != RefuseUnsafePath && r.Code != RefuseNeedsRepair {
		t.Fatalf("refused with %q", r.Code)
	}
	_, err = svc.Apply(ClaudeProvider, ActionUninstall)
	if r := refusalFrom(t, err); r.Code != RefuseUnsafePath && r.Code != RefuseNeedsRepair {
		t.Fatalf("uninstall refused with %q", r.Code)
	}
	if got := readFileForTest(t, target); got != `{}` {
		t.Fatalf("the symlink's target was written: %q", got)
	}
}

func TestClaudeStatusIsDecidedFromTheFileNotFromAClaim(t *testing.T) {
	svc, _, paths := claudeFixture(t)
	applyTo(t, svc, ClaudeProvider, ActionInstall)

	// Tamper with the installed entry: same command, different timeout.
	content := strings.Replace(readFileForTest(t, paths.Settings), `"timeout": 10`, `"timeout": 600`, 1)
	writeFileForTest(t, paths.Settings, content)

	st := claudeStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("a tampered entry reads as %s, want needs-repair", st.Status)
	}
	if st.EffectiveTier != agentlifecycle.TierScreenFallback {
		t.Fatalf("a tampered install still holds tier %q", st.EffectiveTier)
	}
	// Only repair converges from here; install refuses and names it.
	_, err := svc.Apply(ClaudeProvider, ActionInstall)
	if r := refusalFrom(t, err); r.Code != RefuseNeedsRepair {
		t.Fatalf("install refused with %q", r.Code)
	}
	p := applyTo(t, svc, ClaudeProvider, ActionRepair)
	if p.StatusAfter != agentlifecycle.StatusCurrent {
		t.Fatalf("repair ended at %s", p.StatusAfter)
	}
	if got := claudeStatus(t, svc).Status; got != agentlifecycle.StatusCurrent {
		t.Fatalf("after repair: %s", got)
	}
}

func TestClaudeTamperedMatcherIsDamageNotForeign(t *testing.T) {
	svc, _, paths := claudeFixture(t)
	applyTo(t, svc, ClaudeProvider, ActionInstall)
	content := strings.Replace(readFileForTest(t, paths.Settings), `"matcher": "*"`, `"matcher": "startup"`, 1)
	writeFileForTest(t, paths.Settings, content)

	st := claudeStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("a changed matcher reads as %s, want needs-repair", st.Status)
	}
	applyTo(t, svc, ClaudeProvider, ActionRepair)
	groups := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(groups) != 1 {
		t.Fatalf("repair left %d groups", len(groups))
	}
	if groups[0].(map[string]any)["matcher"] != "*" {
		t.Fatal("repair did not restore the canonical matcher")
	}
}

func TestClaudeDuplicateSidecarEntriesConvergeToExactlyOne(t *testing.T) {
	svc, _, paths := claudeFixture(t)
	applyTo(t, svc, ClaudeProvider, ActionInstall)
	// A second copy of the real entry — the double-reporting damage class.
	content := readFileForTest(t, paths.Settings)
	group := `{"matcher": "*", "hooks": [{"type": "command", "command": "sidecar agent report-session --kind claude --hook-stdin", "timeout": 10}]}`
	content = strings.Replace(content, "\"SessionStart\": [\n", "\"SessionStart\": [\n"+group+",\n", 1)
	writeFileForTest(t, paths.Settings, content)

	st := claudeStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("duplicates read as %s, want needs-repair", st.Status)
	}
	applyTo(t, svc, ClaudeProvider, ActionRepair)
	groups := mustParseAny(t, readFileForTest(t, paths.Settings)).(map[string]any)["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(groups) != 1 {
		t.Fatalf("repair left %d Sidecar groups, want exactly one", len(groups))
	}
}

func TestClaudeProviderMissingRefusesInstallButAllowsUninstall(t *testing.T) {
	svc, _, paths := claudeFixture(t)
	applyTo(t, svc, ClaudeProvider, ActionInstall)

	gone := Service{Env: svc.Env, Adapters: svc.Adapters}
	withoutClaude(&gone.Env)

	st, err := gone.Status(ClaudeProvider)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != agentlifecycle.StatusProviderMissing {
		t.Fatalf("status %s, want provider-missing", st.Status)
	}
	if st.EffectiveTier != agentlifecycle.TierScreenFallback {
		t.Fatalf("tier %q with no provider", st.EffectiveTier)
	}
	_, err = gone.Apply(ClaudeProvider, ActionInstall)
	if r := refusalFrom(t, err); r.Code != RefuseProviderMissing {
		t.Fatalf("install refused with %q", r.Code)
	}
	// Uninstall is gated on the files found, not on the provider, so a user
	// who removed claude can still clean up.
	if p := applyTo(t, gone, ClaudeProvider, ActionUninstall); p.Unchanged {
		t.Fatal("uninstall had nothing to remove")
	}
	if _, err := os.Lstat(paths.Settings); !os.IsNotExist(err) {
		t.Fatal("the entry was not removed")
	}
}

func TestClaudeOfferedActionsAreExactlyTheOnesThatWouldNotRefuse(t *testing.T) {
	svc, _, _ := claudeFixture(t)
	for _, step := range []struct {
		name string
		act  Action
	}{
		{"not installed", ""},
		{"installed", ActionInstall},
	} {
		if step.act != "" {
			applyTo(t, svc, ClaudeProvider, step.act)
		}
		st := claudeStatus(t, svc)
		offered := map[Action]bool{}
		for _, a := range st.Offered {
			offered[a] = true
		}
		for _, act := range Actions() {
			_, err := svc.Plan(ClaudeProvider, act)
			if wouldRun := err == nil; wouldRun != offered[act] {
				t.Fatalf("%s: %s offered=%v but planning err=%v", step.name, act, offered[act], err)
			}
		}
	}
}

// TestClaudeFollowsItsConfigDirOverride is the CLAUDE_CONFIG_DIR blind spot,
// closed. Claude Code resolves its configuration home as the raw
// CLAUDE_CONFIG_DIR value, falling back to $HOME/.claude, and this installer
// used to read only $HOME. The consequence was not a cosmetic path difference:
// status reported not-installed for a Claude whose settings.json was full of
// hooks, install then wrote Sidecar's entry into a directory that Claude never
// reads, and uninstall could not find its own entry to remove. All three verbs
// are driven here, because a resolution used by one and not the others is the
// same bug with a smaller blast radius.
func TestClaudeFollowsItsConfigDirOverride(t *testing.T) {
	relocated := t.TempDir()
	svc, env, paths := claudeFixture(t, func(e *Env) { e.ClaudeConfigDir = relocated })

	if paths.Settings != filepath.Join(relocated, "settings.json") {
		t.Fatalf("the adapter targets %s, not the overridden configuration home", paths.Settings)
	}

	applyTo(t, svc, ClaudeProvider, ActionInstall)
	if _, err := os.Lstat(paths.Settings); err != nil {
		t.Fatalf("install wrote nothing to the overridden home: %v", err)
	}
	// The default location must be untouched, which is the half a test that
	// only checked the new path would miss.
	if _, err := os.Lstat(filepath.Join(env.Home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatal("install also wrote into ~/.claude, so the override moved nothing")
	}

	if st := claudeStatus(t, svc); st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status %s at the overridden home, want current", st.Status)
	}
	if p := applyTo(t, svc, ClaudeProvider, ActionUninstall); p.Unchanged {
		t.Fatal("uninstall found nothing to remove at the overridden home")
	}
	if _, err := os.Lstat(paths.Settings); !os.IsNotExist(err) {
		t.Fatal("uninstall left the entry behind at the overridden home")
	}
}

// TestClaudeConfigHomeIgnoresAValueClaudeItselfWouldRefuse pins the two
// readings that are not a plain "use the variable". Claude refuses to run at
// all when the resolved home is not absolute, so a relative value names no
// configuration home; honouring it would put an install somewhere relative to
// Sidecar's working directory, which is the one place Claude is guaranteed not
// to look. A whitespace-only value is a variable exported without a value
// rather than a directory whose name is a space, which is the same reading
// agentsession.PiAgentDir already takes of PI_CODING_AGENT_DIR.
func TestClaudeConfigHomeIgnoresAValueClaudeItselfWouldRefuse(t *testing.T) {
	home := "/home/someone"
	def := filepath.Join(home, ".claude")
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{"unset", "", def},
		{"whitespace", "   ", def},
		{"relative", "claude-config", def},
		{"dot relative", "./claude-config", def},
		{"absolute", "/opt/claude-home", "/opt/claude-home"},
		{"absolute with slack", "  /opt/claude-home  ", "/opt/claude-home"},
		{"absolute unnormalized", "/opt/claude-home/", "/opt/claude-home"},
		{"tilde is not absolute", "~/claude-home", def},
	} {
		if got := ClaudeConfigHome(home, tc.value); got != tc.want {
			t.Fatalf("%s: CLAUDE_CONFIG_DIR=%q resolved to %q, want %q", tc.name, tc.value, got, tc.want)
		}
	}
}
