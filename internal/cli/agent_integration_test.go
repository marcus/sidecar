package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentintegration"
	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The integration CLI tests never touch a real provider configuration
// directory. Every one of them runs against a Service built over a temporary
// HOME, injected through Env.IntegrationService, so the mutating commands can
// be exercised as themselves rather than through a mock of themselves.

func integrationCLI(t *testing.T) (func(args ...string) (int, string, string), string) {
	t.Helper()
	home := t.TempDir()
	config := filepath.Join(home, ".config")
	if err := os.MkdirAll(filepath.Join(config, "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := agentintegration.Service{
		Env: agentintegration.Env{
			Home:       home,
			ConfigHome: config,
			LookPath: func(file string) (string, error) {
				if file == agentintegration.OpenCodeProvider {
					return filepath.Join(home, "bin", "opencode"), nil
				}
				return "", errors.New("not found")
			},
			ProviderVersion: func(string) string { return "1.18.25" },
			UID:             os.Getuid(),
		},
		Adapters: agentintegration.DefaultAdapters(),
	}
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		env := Env{Stdout: &out, Stderr: &errOut, StateDir: t.TempDir(), IntegrationService: &svc}
		cmd := RootCommand().FindSubcommand("agent").FindSubcommand("integration")
		code := cmd.Run(env, args)
		return code, out.String(), errOut.String()
	}
	return run, filepath.Join(config, "opencode", agentintegration.OpenCodeOwnedDir, agentintegration.OpenCodeAssetName)
}

func TestIntegrationListNamesEveryProviderIncludingTheUnsupportedOnes(t *testing.T) {
	run, _ := integrationCLI(t)

	code, out, errOut := run("list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"PROVIDER", "opencode", "not-installed", "unsupported"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list output does not mention %q:\n%s", want, out)
		}
	}

	code, out, errOut = run("list", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var payload struct {
		SchemaVersion int                       `json:"schemaVersion"`
		Integrations  []agentintegration.Status `json:"integrations"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("stdout is not one JSON document: %q", out)
	}
	if payload.SchemaVersion == 0 || len(payload.Integrations) < 2 {
		t.Fatalf("thin payload: %+v", payload)
	}
	// Nothing but JSON on stdout, which is what makes the output pipeable.
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("--json wrote something other than JSON first: %q", out)
	}
}

func TestIntegrationStatusReportsTheInstalledFilesAndTheOfferedActions(t *testing.T) {
	run, assetPath := integrationCLI(t)

	code, out, errOut := run("status", "opencode")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"not-installed", assetPath, "absent", "install"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status does not mention %q:\n%s", want, out)
		}
	}

	if code, _, errOut = run("install", "opencode"); code != 0 {
		t.Fatalf("install exit %d: %s", code, errOut)
	}

	code, out, errOut = run("status", "opencode", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var st agentintegration.Status
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("stdout is not one JSON document: %q", out)
	}
	if st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status %s after install", st.Status)
	}
	if st.EffectiveTier != agentlifecycle.TierFull {
		t.Fatalf("tier %s (%s)", st.EffectiveTier, st.TierReason)
	}
	var sawOwned bool
	for _, f := range st.Files {
		if f.Path == assetPath && f.Owned && f.Checksum != "" {
			sawOwned = true
		}
	}
	if !sawOwned {
		t.Fatalf("the installed asset is not reported as owned with a checksum: %+v", st.Files)
	}
}

// TestEveryMutationHasAPreviewThatMatchesItByteForByte is the parity gate. A
// preview a caller reads and then acts on has to be the same change, not a
// description of one, and the cheapest way to keep that true is to assert the
// bytes.
func TestEveryMutationHasAPreviewThatMatchesItByteForByte(t *testing.T) {
	for _, tc := range []struct {
		action string
		setup  func(t *testing.T, run func(...string) (int, string, string), asset string)
	}{
		{action: "install"},
		{action: "update", setup: installThenAge},
		{action: "repair", setup: installThenAge},
		{action: "uninstall", setup: func(t *testing.T, run func(...string) (int, string, string), _ string) {
			if code, _, errOut := run("install", "opencode"); code != 0 {
				t.Fatalf("install: %s", errOut)
			}
		}},
	} {
		t.Run(tc.action, func(t *testing.T) {
			t.Run("human", func(t *testing.T) {
				run, asset := integrationCLI(t)
				if tc.setup != nil {
					tc.setup(t, run, asset)
				}
				code, preview, errOut := run(tc.action, "opencode", "--dry-run")
				if code != 0 {
					t.Fatalf("dry run exit %d: %s", code, errOut)
				}
				code, applied, errOut := run(tc.action, "opencode")
				if code != 0 {
					t.Fatalf("apply exit %d: %s", code, errOut)
				}
				// Only the first line may differ: it is the one sentence that
				// says whether anything happened.
				if body(preview) != body(applied) {
					t.Fatalf("the preview and the mutation describe different operations\n--- preview ---\n%s\n--- applied ---\n%s", preview, applied)
				}
				if headline(preview) == headline(applied) {
					t.Fatalf("a dry run and a real run are indistinguishable: %q", headline(preview))
				}
				if !strings.Contains(headline(preview), "nothing was changed") {
					t.Fatalf("the dry run does not say it changed nothing: %q", headline(preview))
				}
			})

			t.Run("json", func(t *testing.T) {
				run, asset := integrationCLI(t)
				if tc.setup != nil {
					tc.setup(t, run, asset)
				}
				code, preview, errOut := run(tc.action, "opencode", "--dry-run", "--json")
				if code != 0 {
					t.Fatalf("dry run exit %d: %s", code, errOut)
				}
				code, applied, errOut := run(tc.action, "opencode", "--json")
				if code != 0 {
					t.Fatalf("apply exit %d: %s", code, errOut)
				}
				a, b := decodePlan(t, preview), decodePlan(t, applied)
				if !a.DryRun || b.DryRun || !b.Applied {
					t.Fatalf("the invocation flags are wrong: preview=%+v applied=%+v", a, b)
				}
				a.DryRun, b.DryRun, a.Applied, b.Applied = false, false, false, false
				x, _ := json.Marshal(a)
				y, _ := json.Marshal(b)
				if string(x) != string(y) {
					t.Fatalf("the preview and the mutation differ:\n%s\n%s", x, y)
				}
			})
		})
	}
}

func installThenAge(t *testing.T, run func(...string) (int, string, string), asset string) {
	t.Helper()
	if code, _, errOut := run("install", "opencode"); code != 0 {
		t.Fatalf("install: %s", errOut)
	}
	old := "// sidecar-integration: id=" + agentintegration.OpenCodeSource + " schema=1 version=0\n"
	if err := os.WriteFile(asset, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
}

func decodePlan(t *testing.T, out string) agentintegration.Plan {
	t.Helper()
	var p agentintegration.Plan
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("stdout is not one JSON document: %q", out)
	}
	return p
}

func headline(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func body(s string) string {
	_, rest, _ := strings.Cut(s, "\n")
	return rest
}

func TestAPreviewNamesEveryPathAndItsOwnershipOnBothSides(t *testing.T) {
	// "Show the exact paths and planned changes before mutation" is a rule, and
	// a preview that says "install" without naming a file does not satisfy it.
	run, asset := integrationCLI(t)
	code, out, errOut := run("install", "opencode", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{asset, "write", "why", "before", "after", "absent", "sidecar-owned", "sha256"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the preview does not show %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(asset); !os.IsNotExist(err) {
		t.Fatalf("the dry run wrote something: %v", err)
	}
}

func TestARefusalIsAValueRejectionWithACodeAndNamesTheVerbThatHelps(t *testing.T) {
	run, asset := integrationCLI(t)
	if code, _, errOut := run("install", "opencode"); code != 0 {
		t.Fatalf("install: %s", errOut)
	}
	installThenAge(t, run, asset)

	code, _, errOut := run("install", "opencode")
	if code != exitInputRejected {
		t.Fatalf("exit %d, want %d", code, exitInputRejected)
	}
	if !strings.Contains(errOut, "update") {
		t.Fatalf("the refusal does not name the verb that helps: %q", errOut)
	}

	code, out, errOut := run("install", "opencode", "--json")
	if code != exitInputRejected {
		t.Fatalf("exit %d, want %d", code, exitInputRejected)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("a refusal wrote to stdout: %q", out)
	}
	var payload struct {
		Code    agentintegration.RefusalCode     `json:"code"`
		Message string                           `json:"message"`
		Status  agentlifecycle.IntegrationStatus `json:"status"`
	}
	if err := json.Unmarshal([]byte(errOut), &payload); err != nil {
		t.Fatalf("stderr is not one JSON document: %q", errOut)
	}
	if payload.Code != agentintegration.RefuseAlreadyInstalled {
		t.Fatalf("code %q", payload.Code)
	}
	// The state that produced the refusal travels with it, so a caller does not
	// need a second command to find out what to do instead.
	if payload.Status != agentlifecycle.StatusOutdated {
		t.Fatalf("status %q", payload.Status)
	}
}

func TestAnUnsupportedOrUnknownProviderIsRefusedNotCrashed(t *testing.T) {
	run, _ := integrationCLI(t)
	for _, tc := range []struct {
		provider string
		want     agentintegration.RefusalCode
	}{
		// amp has a recorded capability but no bundled adapter, which is what
		// keeps the "unsupported" branch honest now that ten providers ship
		// adapters. grok stood here until the session-identity ports gave it
		// one; the branch needs a provider Sidecar has evaluated and
		// deliberately not built for, and that set shrinks with every port.
		// codex covers the third refusal shape: an adapter exists, but the
		// provider CLI is not on this fixture's PATH.
		{"amp", agentintegration.RefuseUnsupported},
		{"codex", agentintegration.RefuseProviderMissing},
		{"not-an-agent", agentintegration.RefuseUnknownProvider},
	} {
		code, _, errOut := run("install", tc.provider, "--json")
		if code != exitInputRejected {
			t.Fatalf("%s: exit %d", tc.provider, code)
		}
		var payload struct {
			Code agentintegration.RefusalCode `json:"code"`
		}
		if err := json.Unmarshal([]byte(errOut), &payload); err != nil {
			t.Fatalf("%s: stderr is not JSON: %q", tc.provider, errOut)
		}
		if payload.Code != tc.want {
			t.Fatalf("%s refused with %q, want %q", tc.provider, payload.Code, tc.want)
		}
	}
}

func TestIntegrationUsageMistakesExitTwo(t *testing.T) {
	run, _ := integrationCLI(t)
	for _, args := range [][]string{
		{"install"},                            // no provider
		{"install", "opencode", "extra"},       // too many
		{"install", "opencode", "--nonsense"},  // unknown flag
		{"list", "opencode"},                   // list takes nothing
		{"list", "--dry-run"},                  // dry-run is for mutations
		{"status", "opencode", "--dry-run"},    // same
		{"nonsense-command"},                   // unknown subcommand
		{"uninstall", "opencode", "--verbose"}, // unknown flag
	} {
		code, _, errOut := run(args...)
		if code != 2 {
			t.Fatalf("%v: exit %d, want 2 (stderr: %s)", args, code, errOut)
		}
	}
}

func TestIntegrationHelpIsAvailableAtEveryLevel(t *testing.T) {
	run, _ := integrationCLI(t)
	for _, args := range [][]string{
		{},
		{"--help"},
		{"list", "--help"},
		{"install", "--help"},
		{"uninstall", "-h"},
	} {
		code, out, errOut := run(args...)
		if code != 0 {
			t.Fatalf("%v: exit %d (%s)", args, code, errOut)
		}
		if !strings.Contains(out, "integration") {
			t.Fatalf("%v: help does not mention the command:\n%s", args, out)
		}
	}
	// The privacy behavior is in the help rather than only in the docs,
	// because it is what someone asks about at exactly the moment they are
	// deciding whether to install.
	_, out, _ := run("install", "--help")
	if !strings.Contains(out, "never sends prompt text") {
		t.Fatalf("install help does not state what is and is not sent:\n%s", out)
	}
}

func TestIntegrationCommandsAreRegisteredAlphabetically(t *testing.T) {
	// The generated CLI doc and RenderHelp both render Sub in slice order.
	cmd := RootCommand().FindSubcommand("agent").FindSubcommand("integration")
	var names []string
	for _, c := range cmd.Sub {
		names = append(names, c.Name)
	}
	want := []string{"install", "list", "repair", "status", "uninstall", "update"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("subcommands are %v, want %v", names, want)
	}
	// Every action in the vocabulary has a command, and every command that
	// mutates is marked as one.
	for _, act := range agentintegration.Actions() {
		sub := cmd.FindSubcommand(string(act))
		if sub == nil {
			t.Fatalf("no command for action %q", act)
		}
		if !sub.Mutates {
			t.Fatalf("%s changes files but is not marked as mutating", act)
		}
	}
}
