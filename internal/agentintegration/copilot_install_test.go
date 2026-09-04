package agentintegration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

func copilotFixture(t *testing.T, opts ...func(*Env)) (Service, Env, sessionHookPaths) {
	t.Helper()
	return sessionHookFixture(t, NewCopilotAdapter().sessionHookAdapter, opts...)
}

// TestCopilotWritesHerdrsEntryShapeExactly is the whole of what this port can
// be held to. Copilot is not installed on any machine Sidecar has surveyed, so
// there is no released binary to check the shape against and Herdr's installer
// is the only source. That makes the shape worth pinning precisely rather than
// loosely: three of its fields differ from every other provider in this tree,
// and none of the three was invented here.
func TestCopilotWritesHerdrsEntryShapeExactly(t *testing.T) {
	a := NewCopilotAdapter().sessionHookAdapter
	top, err := parseJSONFile([]byte(a.asset().Content))
	if err != nil {
		t.Fatal(err)
	}
	hooksIdx, ok := lastMember(top, "hooks")
	if !ok {
		t.Fatalf("the asset has no hooks member:\n%s", a.asset().Content)
	}
	events, err := parseJSONObject(top[hooksIdx].val)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].key != "SessionStart" {
		t.Fatalf("the asset registers %d events, want exactly SessionStart:\n%s", len(events), a.asset().Content)
	}
	handlers, err := parseJSONArray(events[0].val)
	if err != nil {
		t.Fatalf("SessionStart does not hold a flat handler array: %v", err)
	}
	if len(handlers) != 1 {
		t.Fatalf("SessionStart holds %d handlers, want one", len(handlers))
	}
	handler, err := parseJSONObject(handlers[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, wrapped := lastMember(handler, "hooks"); wrapped {
		t.Fatal("the handler is wrapped in a matcher group; Copilot's events take a flat handler list")
	}
	if typ, _ := memberString(handler, "type"); typ != "command" {
		t.Fatalf("handler type is %q, want command", typ)
	}
	// The command field is `bash`, not `command`. This is the field a released
	// Copilot would read, per Herdr's direct_command_field.
	if _, ok := memberString(handler, "bash"); !ok {
		t.Fatalf("the handler has no bash field, which is where Copilot reads the command:\n%s", a.asset().Content)
	}
	if _, ok := memberString(handler, "command"); ok {
		t.Fatalf("the handler also carries a command field, which would be a second spelling of the same hook:\n%s", a.asset().Content)
	}
	if _, ok := lastMember(handler, "timeoutSec"); !ok {
		t.Fatalf("the handler has no timeoutSec, which is Copilot's spelling of the timeout:\n%s", a.asset().Content)
	}
	if _, ok := lastMember(handler, "timeout"); ok {
		t.Fatalf("the handler carries a `timeout`, which is not the field Copilot reads:\n%s", a.asset().Content)
	}
	// No Windows spelling is written, because Sidecar has no Windows support
	// and it would be unreachable and untestable.
	if strings.Contains(a.asset().Content, "powershell") {
		t.Fatalf("the asset writes a Windows command field Sidecar cannot reach:\n%s", a.asset().Content)
	}
}

// TestCopilotStillRecognisesTheWindowsSpelling is the other half of not writing
// it. A settings.json synced from a machine where something wrote the
// `powershell` form still carries a Sidecar entry, and an entry the scan cannot
// see is one that keeps reporting while `integration status` says nothing is
// installed and uninstall has nothing to remove.
func TestCopilotStillRecognisesTheWindowsSpelling(t *testing.T) {
	svc, _, paths := copilotFixture(t)
	writeFileForTest(t, paths.File, `{
  "hooks": {
    "SessionStart": [
      {"type": "command", "powershell": "sidecar agent report-session --kind copilot --hook-stdin", "timeoutSec": 10}
    ]
  }
}
`)
	st, err := svc.Status(CopilotProvider)
	if err != nil {
		t.Fatal(err)
	}
	// It is Sidecar's entry, in a spelling this build does not ship, so it is
	// needs-repair rather than not-installed. The direction that matters is
	// that it is not invisible.
	if st.Status == agentlifecycle.StatusNotInstalled {
		t.Fatal("a Sidecar entry in the Windows spelling read as nothing installed")
	}
	applyTo(t, svc, CopilotProvider, ActionRepair)
	after := readFileForTest(t, paths.File)
	if strings.Contains(after, "powershell") {
		t.Fatalf("repair left the other spelling in place, so both would fire:\n%s", after)
	}
	scan := scanHookTree(true, []byte(after), NewCopilotAdapter().integration.spec)
	if len(scan.owned) != 1 {
		t.Fatalf("repair left %d Sidecar entries, want one:\n%s", len(scan.owned), after)
	}
}

// TestCopilotFollowsItsConfigDirOverride pins $COPILOT_HOME, which is honoured
// on Herdr's word rather than on a released binary's. It is a test rather than
// a comment because the decision to trust the variable here and to ignore
// Antigravity's is the kind of asymmetry a later reader will want to see
// asserted.
func TestCopilotFollowsItsConfigDirOverride(t *testing.T) {
	relocated := t.TempDir()
	svc, env, paths := copilotFixture(t, func(e *Env) { e.CopilotHome = relocated })

	if want := filepath.Join(relocated, "settings.json"); paths.File != want {
		t.Fatalf("the adapter targets %s, not the overridden home", paths.File)
	}
	applyTo(t, svc, CopilotProvider, ActionInstall)
	if got := readFileForTest(t, paths.File); !strings.Contains(got, "report-session") {
		t.Fatalf("install wrote nothing recognisable to the overridden home:\n%s", got)
	}
	if _, err := readFileMaybe(filepath.Join(env.Home, ".copilot", "settings.json")); err == nil {
		t.Fatal("install also wrote into ~/.copilot, so the override moved nothing")
	}
}

// TestCopilotConfigHomeExpandsATilde matches Herdr's own reading of every
// provider directory override it honours, which tilde-expands before use.
func TestCopilotConfigHomeExpandsATilde(t *testing.T) {
	home := "/home/someone"
	for _, tc := range []struct{ value, want string }{
		{"", filepath.Join(home, ".copilot")},
		{"   ", filepath.Join(home, ".copilot")},
		{"~", home},
		{"~/copilot-home", filepath.Join(home, "copilot-home")},
		{"/opt/copilot-home", "/opt/copilot-home"},
	} {
		if got := CopilotConfigHome(home, tc.value); got != tc.want {
			t.Fatalf("COPILOT_HOME=%q resolved to %q, want %q", tc.value, got, tc.want)
		}
	}
}

// readFileMaybe reads a file, reporting the error rather than failing, so a
// test can assert a path does NOT exist.
func readFileMaybe(path string) ([]byte, error) { return os.ReadFile(path) }
