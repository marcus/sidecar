package agentintegration

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Hermes suite.
//
// It is in three halves, because Hermes is the first provider whose integration
// needs both ownership shapes at once. The first drives the gate over the
// fixtures in testdata/hermes and requires the shipped Python and the Go mirror
// to agree. The second is the config.yaml line editor, which is the part that
// touches a file full of the user's own settings and is therefore the part where
// a bug costs somebody their configuration. The third is the installer contract
// every adapter in this package is held to, asserted again here because an
// adapter that satisfies it by accident is one that stops satisfying it on the
// next edit.

// hermesFixture builds a service against a temporary tree with hermes on PATH
// and its home already present, which is the state a machine is in after Hermes
// has been run once.
func hermesFixture(t *testing.T, opts ...func(*Env)) (Service, Env, hermesPaths) {
	t.Helper()
	home := t.TempDir()
	env := Env{
		Home: home,
		LookPath: func(file string) (string, error) {
			if file == HermesCommand {
				return filepath.Join(home, "bin", "hermes"), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "0.17.0" },
		UID:             os.Getuid(),
	}
	for _, o := range opts {
		o(&env)
	}
	paths := hermesPathsFor(env)
	if err := os.MkdirAll(paths.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, paths
}

func withoutHermes(e *Env) {
	e.LookPath = func(string) (string, error) { return "", errors.New("not found") }
}

func hermesStatus(t *testing.T, s Service) Status {
	t.Helper()
	st, err := s.Status(HermesProvider)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

func hermesApply(t *testing.T, s Service, act Action) Plan {
	t.Helper()
	p, err := s.Apply(HermesProvider, act)
	if err != nil {
		t.Fatalf("%s: %v", act, err)
	}
	return p
}

func writeHermesConfig(t *testing.T, paths hermesPaths, content string) {
	t.Helper()
	if err := os.WriteFile(paths.Config, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeForTest(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func writeForTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readHermesFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// requirePython3 mirrors the node rule the JavaScript assets are held to: a
// machine without a Python interpreter skips rather than passing vacuously,
// unless SIDECAR_REQUIRE_PYTHON=1 says the run is one where that would hide a
// regression.
func requirePython3(t *testing.T) string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err == nil {
		return python
	}
	if os.Getenv("SIDECAR_REQUIRE_PYTHON") == "1" {
		t.Fatal("SIDECAR_REQUIRE_PYTHON=1 but python3 is not on PATH; the Hermes asset cannot be driven")
	}
	t.Skip("python3 is not installed; skipping the Hermes asset equivalence check")
	return ""
}

// hermesFixtureRows parses one fixture into gate inputs.
func hermesFixtureRows(t *testing.T, name string) []HermesHook {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hermes", name))
	if err != nil {
		t.Fatal(err)
	}
	value := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "-" {
			return ""
		}
		return s
	}
	var rows []HermesHook
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 {
			t.Fatalf("malformed fixture row in %s: %q", name, line)
		}
		rows = append(rows, HermesHook{Name: value(cols[0]), Platform: value(cols[1]), SessionID: value(cols[2])})
	}
	if len(rows) == 0 {
		t.Fatalf("%s has no rows, so it asserts nothing", name)
	}
	return rows
}

// TestBundledHermesAssetBehavesLikeTheGate is the equivalence test the split
// between the shipped Python and the Go mirror exists for.
//
// A mirror that has drifted is worse than none: it agrees with itself while the
// file a user runs does something else. So the real plugin is loaded, its real
// callbacks are fired with the payload shapes Hermes passes, and the argv it
// would have spawned is compared element for element with what the Go gate
// produces over the same rows.
func TestBundledHermesAssetBehavesLikeTheGate(t *testing.T) {
	python := requirePython3(t)

	entries, err := os.ReadDir(filepath.Join("testdata", "hermes"))
	if err != nil {
		t.Fatal(err)
	}
	drove := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".tsv") {
			continue
		}
		drove++
		t.Run(entry.Name(), func(t *testing.T) {
			var gate HermesGate
			want := [][]string{}
			for _, row := range hermesFixtureRows(t, entry.Name()) {
				if id := gate.Bind(row); id != "" {
					want = append(want, HermesReportArgs(id))
				}
			}

			cmd := exec.Command(python, filepath.Join("assets", "hermes", "replay-harness.py"),
				filepath.Join("testdata", "hermes", entry.Name()))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("running the hermes replay harness: %v\n%s", err, out)
			}
			var result struct {
				Argv  [][]string `json:"argv"`
				Hooks []string   `json:"hooks"`
			}
			if err := json.Unmarshal(out, &result); err != nil {
				t.Fatalf("the hermes replay harness output is not JSON: %q (%v)", out, err)
			}
			if result.Argv == nil {
				result.Argv = [][]string{}
			}
			if !reflect.DeepEqual(result.Argv, want) {
				t.Fatalf("the shipped asset spawns %v; the Go gate says %v", result.Argv, want)
			}
			// The registrations are part of the contract too. A plugin that
			// subscribed to nothing would agree with a gate that bound nothing.
			if got := strings.Join(result.Hooks, ","); got != "on_session_reset,on_session_start,pre_llm_call" {
				t.Fatalf("the asset registered %q; the port subscribes to upstream's three hooks", got)
			}
		})
	}
	if drove == 0 {
		t.Fatal("no fixtures were driven, so this asserts nothing")
	}
}

// TestTheHermesAssetSpawnsNothingOutsideAManagedShell is the fail-open rule,
// asserted against the shipped file rather than against a description of it.
//
// The asset registers no callback at all outside a Sidecar-managed shell, which
// is stronger than declining inside one: a Hermes running anywhere else carries
// no Sidecar code on any hook path.
func TestTheHermesAssetSpawnsNothingOutsideAManagedShell(t *testing.T) {
	python := requirePython3(t)

	script := `
import importlib.util, json, os, sys
os.environ.pop("SIDECAR_MANAGED_SHELL", None)
os.environ.pop("SIDECAR_BIN", None)
spec = importlib.util.spec_from_file_location("asset", sys.argv[1])
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
class Ctx:
    def __init__(self): self.hooks = {}
    def register_hook(self, name, cb): self.hooks.setdefault(name, []).append(cb)
unmanaged = Ctx(); m.register(unmanaged)
os.environ["SIDECAR_MANAGED_SHELL"] = "1"
no_bin = Ctx(); m.register(no_bin)
os.environ["SIDECAR_BIN"] = "/nonexistent/sidecar"
managed = Ctx(); m.register(managed)
print(json.dumps({"unmanaged": sorted(unmanaged.hooks), "noBin": sorted(no_bin.hooks), "managed": sorted(managed.hooks)}))
`
	cmd := exec.Command(python, "-c", script, filepath.Join("assets", "hermes", "__init__.py"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driving the asset: %v\n%s", err, out)
	}
	var got struct {
		Unmanaged []string `json:"unmanaged"`
		NoBin     []string `json:"noBin"`
		Managed   []string `json:"managed"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not JSON: %q (%v)", out, err)
	}
	if len(got.Unmanaged) != 0 {
		t.Fatalf("the asset registered %v outside a managed shell", got.Unmanaged)
	}
	if len(got.NoBin) != 0 {
		t.Fatalf("the asset registered %v with no SIDECAR_BIN to spawn", got.NoBin)
	}
	if len(got.Managed) != 3 {
		t.Fatalf("the asset registered %v inside a managed shell; the port subscribes to three hooks", got.Managed)
	}
}

// TestHermesInstallsBothFilesAndTheEnableLine is the whole install in one
// assertion, and the enable line is the half that is easy to forget: Hermes
// finds a plugin it has not been told to load and does nothing with it.
func TestHermesInstallsBothFilesAndTheEnableLine(t *testing.T) {
	svc, _, paths := hermesFixture(t)

	if got := hermesStatus(t, svc).Status; got != agentlifecycle.StatusNotInstalled {
		t.Fatalf("before install: %s", got)
	}

	hermesApply(t, svc, ActionInstall)

	if got := readHermesFile(t, paths.Init); got != HermesInitAsset() {
		t.Fatal("the installed plugin is not the bundled asset")
	}
	if got := readHermesFile(t, paths.Manifest); got != HermesManifestAsset() {
		t.Fatal("the installed manifest is not the bundled asset")
	}
	config := readHermesFile(t, paths.Config)
	if !strings.Contains(config, HermesPluginName) {
		t.Fatalf("config.yaml does not enable the plugin:\n%s", config)
	}
	if !strings.Contains(config, markerToken) {
		t.Fatalf("the enable line carries no marker comment:\n%s", config)
	}

	st := hermesStatus(t, svc)
	if st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("after install: %s (%s)", st.Status, st.Message)
	}

	// Idempotent. A second install is a no-op that says so, rather than a
	// second enable line.
	plan, err := svc.Plan(HermesProvider, ActionInstall)
	if err != nil {
		t.Fatalf("re-planning install: %v", err)
	}
	if !plan.Unchanged || len(plan.Ops) != 0 {
		t.Fatalf("a second install planned %d operations", len(plan.Ops))
	}
	if got := readHermesFile(t, paths.Config); got != config {
		t.Fatalf("re-inspecting changed config.yaml:\n%s", got)
	}
}

// TestHermesUninstallGivesTheUsersConfigBackByteForByte is the promise the line
// editor exists to keep.
//
// The file here has comments, a blank line, quoting the editor never chose, a
// plugin of the user's own, and keys on both sides of the one Sidecar touches.
// After an install and an uninstall it must be the same bytes: not equivalent
// YAML, the same bytes, because a user who diffs their own configuration after
// uninstalling a tool is entitled to an empty diff.
func TestHermesUninstallGivesTheUsersConfigBackByteForByte(t *testing.T) {
	svc, _, paths := hermesFixture(t)

	original := `# my hermes configuration
model:
  provider: auto

plugins:
  enabled:
    - "platforms/discord"   # the one I actually use
    - disk-cleanup
  disabled: []

hooks_auto_accept: true
`
	writeHermesConfig(t, paths, original)

	hermesApply(t, svc, ActionInstall)
	installed := readHermesFile(t, paths.Config)
	if !strings.Contains(installed, HermesPluginName) {
		t.Fatalf("install did not add the enable line:\n%s", installed)
	}
	// The user's own lines are untouched, including the comment on one of them.
	for _, line := range []string{`    - "platforms/discord"   # the one I actually use`, "    - disk-cleanup", "  disabled: []", "hooks_auto_accept: true"} {
		if !strings.Contains(installed, line) {
			t.Fatalf("install disturbed %q:\n%s", line, installed)
		}
	}

	hermesApply(t, svc, ActionUninstall)
	if got := readHermesFile(t, paths.Config); got != original {
		t.Fatalf("uninstall did not give the file back byte for byte.\ngot:\n%s\nwant:\n%s", got, original)
	}
	if _, err := os.Stat(paths.PluginDir); !os.IsNotExist(err) {
		t.Fatalf("the plugin directory survived uninstall: %v", err)
	}
}

// TestHermesFindsItsEnableLineAfterHermesStrippedTheComment is the measured
// reason ownership of that line is its value rather than a marker.
//
// hermes config set, hermes plugins enable and the setup wizard all round-trip
// config.yaml through yaml.dump, which drops every comment in the file and
// rewrites the sequence indentless. A rule that needed the marker would leave
// uninstall unable to find its own entry the first time a user ran any of them.
func TestHermesFindsItsEnableLineAfterHermesStrippedTheComment(t *testing.T) {
	svc, _, paths := hermesFixture(t)

	// Exactly what PyYAML writes: no comments anywhere, and sequence items at
	// the same column as the key that holds them.
	rewritten := "model:\n  provider: auto\nplugins:\n  enabled:\n  - disk-cleanup\n  - " + HermesPluginName + "\n"
	writeHermesConfig(t, paths, rewritten)
	if err := os.MkdirAll(paths.PluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Init, []byte(HermesInitAsset()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Manifest, []byte(HermesManifestAsset()), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := hermesStatus(t, svc).Status; got != agentlifecycle.StatusCurrent {
		t.Fatalf("a Hermes-rewritten config reads as %s; the enable line is still Sidecar's", got)
	}

	hermesApply(t, svc, ActionUninstall)
	got := readHermesFile(t, paths.Config)
	if strings.Contains(got, HermesPluginName) {
		t.Fatalf("uninstall left its own enable line behind:\n%s", got)
	}
	if !strings.Contains(got, "  - disk-cleanup") {
		t.Fatalf("uninstall took the user's plugin with it:\n%s", got)
	}
	// The keys stay, because they are the user's. Hermes writes an empty
	// enabled list itself on the first `plugins disable`.
	if !strings.Contains(got, "plugins:") || !strings.Contains(got, "  enabled:") {
		t.Fatalf("uninstall removed keys it did not create:\n%s", got)
	}
}

// TestHermesRefusesAShapeItWouldHaveToRewrite is the one place this port is
// deliberately narrower than Herdr's, and the reason is that Herdr's wider
// handling writes into a key Hermes does not read.
//
// hermes_cli/plugins.py's _get_enabled_plugins requires `plugins` to be a
// mapping with an `enabled` list in it; a `plugins` that is a bare list is
// ignored entirely, and nothing in it is ever enabled. Herdr edits that shape
// anyway. Sidecar refuses both it and a flow sequence, because converting either
// would rewrite bytes outside Sidecar's own entry to produce a key that either
// does nothing or was already the user's to format.
func TestHermesRefusesAShapeItWouldHaveToRewrite(t *testing.T) {
	for name, content := range map[string]string{
		"a flat plugins list Hermes never reads": "plugins:\n  - platforms/discord\n",
		"a flow sequence":                        "plugins: [platforms/discord]\n",
		"an inline enabled list":                 "plugins:\n  enabled: [disk-cleanup]\n",
		"a scalar":                               "plugins: none\n",
	} {
		t.Run(name, func(t *testing.T) {
			svc, _, paths := hermesFixture(t)
			writeHermesConfig(t, paths, content)

			st := hermesStatus(t, svc)
			if st.Status != agentlifecycle.StatusNeedsRepair {
				t.Fatalf("status is %s; a shape Sidecar will not edit is needs-repair", st.Status)
			}
			if !strings.Contains(st.Message, "block") {
				t.Fatalf("the message does not say what shape is wanted: %q", st.Message)
			}
			for _, act := range []Action{ActionInstall, ActionRepair} {
				if _, err := svc.Plan(HermesProvider, act); err == nil {
					t.Fatalf("%s was planned against a shape Sidecar refuses to rewrite", act)
				}
			}
			if got := readHermesFile(t, paths.Config); got != content {
				t.Fatalf("a refused plan changed the file:\n%s", got)
			}
		})
	}
}

// TestHermesRefusesAConfigThatIsNotYAML pins the read-only oracle at the near
// end of the edit. A file that does not parse is not one whose lines can be
// reasoned about, and guessing at it is how a tool destroys a configuration.
func TestHermesRefusesAConfigThatIsNotYAML(t *testing.T) {
	svc, _, paths := hermesFixture(t)
	broken := "plugins:\n  enabled:\n    - one\n   - badly indented\n\tand a tab\n"
	writeHermesConfig(t, paths, broken)

	st := hermesStatus(t, svc)
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("status is %s for a config.yaml that does not parse", st.Status)
	}
	if _, err := svc.Plan(HermesProvider, ActionRepair); err == nil {
		t.Fatal("repair was planned against a file Sidecar cannot parse")
	}
	if got := readHermesFile(t, paths.Config); got != broken {
		t.Fatal("a refused plan changed the file")
	}
}

// TestAHalfInstalledHermesIsNeedsRepairRatherThanCurrent is the consequence of
// Hermes needing all three parts. A plugin directory missing its manifest is
// skipped by the loader with no error, and a plugin absent from plugins.enabled
// is never loaded at all, so either state looks installed and reports nothing.
func TestAHalfInstalledHermesIsNeedsRepairRatherThanCurrent(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, p hermesPaths){
		"the manifest is gone":      func(t *testing.T, p hermesPaths) { removeForTest(t, p.Manifest) },
		"the plugin file is gone":   func(t *testing.T, p hermesPaths) { removeForTest(t, p.Init) },
		"the enable line is gone":   func(t *testing.T, p hermesPaths) { writeForTest(t, p.Config, "plugins:\n  enabled: []\n") },
		"the whole config is gone":  func(t *testing.T, p hermesPaths) { removeForTest(t, p.Config) },
		"the config lost the block": func(t *testing.T, p hermesPaths) { writeForTest(t, p.Config, "model:\n  provider: auto\n") },
	} {
		t.Run(name, func(t *testing.T) {
			svc, _, paths := hermesFixture(t)
			hermesApply(t, svc, ActionInstall)
			damage(t, paths)

			st := hermesStatus(t, svc)
			if st.Status != agentlifecycle.StatusNeedsRepair {
				t.Fatalf("status is %s; half an installation loads nothing", st.Status)
			}
			if !strings.Contains(st.Message, "partly installed") {
				t.Fatalf("the message does not say what is wrong: %q", st.Message)
			}
			if _, err := svc.Plan(HermesProvider, ActionInstall); err == nil {
				t.Fatal("install was offered against a damaged installation; repair is the honest verb")
			}

			hermesApply(t, svc, ActionRepair)
			if got := hermesStatus(t, svc).Status; got != agentlifecycle.StatusCurrent {
				t.Fatalf("repair left the installation at %s", got)
			}
		})
	}
}

// TestHermesNeverAdoptsAForeignPluginFile is the ownership rule at the two paths
// Sidecar writes whole. Herdr's uninstall removes its plugin directory with
// remove_dir_all and no marker check at all.
func TestHermesNeverAdoptsAForeignPluginFile(t *testing.T) {
	for _, path := range []string{"init", "manifest"} {
		t.Run(path, func(t *testing.T) {
			svc, _, paths := hermesFixture(t)
			if err := os.MkdirAll(paths.PluginDir, 0o755); err != nil {
				t.Fatal(err)
			}
			target := paths.Init
			if path == "manifest" {
				target = paths.Manifest
			}
			foreign := "# somebody else's file, with no Sidecar marker\nvalue = 1\n"
			if err := os.WriteFile(target, []byte(foreign), 0o644); err != nil {
				t.Fatal(err)
			}

			st := hermesStatus(t, svc)
			if st.Status != agentlifecycle.StatusNeedsRepair {
				t.Fatalf("status is %s for a foreign file at Sidecar's own path", st.Status)
			}
			for _, act := range Actions() {
				_, err := svc.Plan(HermesProvider, act)
				if err == nil && act != ActionRepair {
					t.Fatalf("%s was planned over a file Sidecar does not own", act)
				}
				if act == ActionRepair && err == nil {
					t.Fatal("repair was planned over a file Sidecar does not own")
				}
				var refusal *Refusal
				if errors.As(err, &refusal) && refusal.Code == RefuseForeignFile && refusal.Path != target {
					t.Fatalf("the refusal names %q, not the file it is about", refusal.Path)
				}
			}
			if got := readHermesFile(t, target); got != foreign {
				t.Fatal("a refused plan modified a file Sidecar does not own")
			}
		})
	}
}

// TestHermesUninstallTakesPythonsCompiledCopyWithIt is the one thing this
// uninstall removes that Sidecar did not write, and it is here because the live
// proof run found it and no unit test could have.
//
// Importing the plugin leaves __pycache__/__init__.cpython-NN.pyc inside the
// directory Sidecar owns. Without this the ownership rule is satisfied and the
// result is still wrong: every file Sidecar wrote is gone, the directory is not
// empty, and what survives an uninstall is a stale compiled copy of the plugin
// that was just removed. A .pyc for anything else is somebody's own and stays,
// along with the directory holding it.
func TestHermesUninstallTakesPythonsCompiledCopyWithIt(t *testing.T) {
	t.Run("its own compiled copy goes", func(t *testing.T) {
		svc, _, paths := hermesFixture(t)
		hermesApply(t, svc, ActionInstall)
		if err := os.MkdirAll(paths.PyCache, 0o755); err != nil {
			t.Fatal(err)
		}
		writeForTest(t, filepath.Join(paths.PyCache, "__init__.cpython-311.pyc"), "compiled bytes")

		hermesApply(t, svc, ActionUninstall)
		if _, err := os.Stat(paths.PluginDir); !os.IsNotExist(err) {
			t.Fatalf("the plugin directory survived with a stale bytecode cache in it: %v", err)
		}
	})

	t.Run("somebody else's compiled copy stays", func(t *testing.T) {
		svc, _, paths := hermesFixture(t)
		hermesApply(t, svc, ActionInstall)
		if err := os.MkdirAll(paths.PyCache, 0o755); err != nil {
			t.Fatal(err)
		}
		theirs := filepath.Join(paths.PyCache, "helper.cpython-311.pyc")
		writeForTest(t, theirs, "not Sidecar's")

		hermesApply(t, svc, ActionUninstall)
		if _, err := os.Stat(theirs); err != nil {
			t.Fatalf("uninstall removed a compiled copy of a module that is not Sidecar's: %v", err)
		}
	})
}

// TestHermesHonoursItsOwnHomeOverride is the rule the lane settled on: follow
// the code path that reads the file. hermes_constants.get_hermes_home reads
// HERMES_HOME and calls itself the single source of truth for that path, and
// every other copy in that codebase imports it.
func TestHermesHonoursItsOwnHomeOverride(t *testing.T) {
	relocated := t.TempDir()
	svc, env, paths := hermesFixture(t, func(e *Env) { e.HermesHome = relocated })

	if !strings.HasPrefix(paths.Init, relocated) {
		t.Fatalf("the plugin would be installed at %s, not under %s", paths.Init, relocated)
	}
	hermesApply(t, svc, ActionInstall)
	if _, err := os.Stat(filepath.Join(relocated, "plugins", HermesPluginName, HermesInitName)); err != nil {
		t.Fatalf("nothing was installed into the relocated home: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".hermes")); !os.IsNotExist(err) {
		t.Fatal("the default home was created even though the override named another one")
	}

	// A variable exported with nothing in it is not a directory named " ".
	blank := hermesPathsFor(Env{Home: env.Home, HermesHome: "   "})
	if blank.Home != filepath.Join(env.Home, ".hermes") {
		t.Fatalf("a blank HERMES_HOME resolved to %q", blank.Home)
	}
}

// TestHermesRefusesToInventAHomeThatHermesHasNeverCreated keeps Herdr's own
// refusal, restated in Sidecar's vocabulary. Hermes creates its home on first
// run, so its absence means Hermes has never run here.
func TestHermesRefusesToInventAHomeThatHermesHasNeverCreated(t *testing.T) {
	home := t.TempDir()
	env := Env{
		Home:            home,
		HermesHome:      filepath.Join(home, "never-created"),
		LookPath:        func(string) (string, error) { return filepath.Join(home, "bin", "hermes"), nil },
		ProviderVersion: func(string) string { return "0.17.0" },
		UID:             os.Getuid(),
	}
	svc := Service{Env: env, Adapters: DefaultAdapters()}

	st := hermesStatus(t, svc)
	if st.Status != agentlifecycle.StatusNotInstalled {
		t.Fatalf("status is %s", st.Status)
	}
	if !strings.Contains(st.Message, "has not been set up") {
		t.Fatalf("the status does not say why no install is offered: %q", st.Message)
	}
	for _, act := range st.Offered {
		if act == ActionInstall {
			t.Fatal("install was offered for a Hermes that has never run")
		}
	}
	if _, err := svc.Plan(HermesProvider, ActionInstall); err == nil {
		t.Fatal("install was planned against a home Hermes has never created")
	}
	if _, err := os.Stat(env.HermesHome); !os.IsNotExist(err) {
		t.Fatal("a refused plan created the provider's home")
	}
}

// TestHermesBacksUpTheConfigBeforeEditingIt is the recoverable copy the Claude
// installer keeps for the same reason: the file being edited is the user's, and
// it holds everything else they have configured.
func TestHermesBacksUpTheConfigBeforeEditingIt(t *testing.T) {
	svc, _, paths := hermesFixture(t)
	original := "model:\n  provider: auto\n"
	writeHermesConfig(t, paths, original)

	hermesApply(t, svc, ActionInstall)
	if got := readHermesFile(t, paths.ConfigBackup); got != original {
		t.Fatalf("the backup is not the file that was edited:\n%s", got)
	}
	// And the backup is never mistaken for the configuration itself.
	if strings.Contains(readHermesFile(t, paths.ConfigBackup), HermesPluginName) {
		t.Fatal("the backup carries the edit it was taken to protect against")
	}
}

// TestAMissingHermesRefusesInstallButStillAllowsCleanup is the same asymmetry
// every adapter here has. Removing the provider must not strand its files.
func TestAMissingHermesRefusesInstallButStillAllowsCleanup(t *testing.T) {
	svc, env, paths := hermesFixture(t)
	hermesApply(t, svc, ActionInstall)

	gone := Service{Env: env, Adapters: DefaultAdapters()}
	withoutHermes(&gone.Env)

	st := hermesStatus(t, gone)
	if st.Status != agentlifecycle.StatusProviderMissing {
		t.Fatalf("status is %s with no hermes on PATH", st.Status)
	}
	if _, err := gone.Plan(HermesProvider, ActionInstall); err == nil {
		t.Fatal("install was planned with no provider to load the plugin")
	}
	hermesApply(t, gone, ActionUninstall)
	if _, err := os.Stat(paths.Init); !os.IsNotExist(err) {
		t.Fatal("uninstall left the plugin behind")
	}
	if strings.Contains(readHermesFile(t, paths.Config), HermesPluginName) {
		t.Fatal("uninstall left the enable line behind")
	}
}

// TestADryRunAndTheRealHermesRunDescribeTheSameOperations is the honesty rule
// for the preview, driven on the adapter that emits the most operations.
func TestADryRunAndTheRealHermesRunDescribeTheSameOperations(t *testing.T) {
	svc, _, paths := hermesFixture(t)
	writeHermesConfig(t, paths, "model:\n  provider: auto\n")

	preview, err := svc.Plan(HermesProvider, ActionInstall)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.DryRun {
		t.Fatal("Plan did not mark its result as a dry run")
	}
	real := hermesApply(t, svc, ActionInstall)
	if len(preview.Ops) != len(real.Ops) {
		t.Fatalf("the preview described %d operations and the run performed %d", len(preview.Ops), len(real.Ops))
	}
	for i := range preview.Ops {
		if !reflect.DeepEqual(preview.Ops[i], real.Ops[i]) {
			t.Fatalf("operation %d differs:\npreview: %+v\nrun:     %+v", i, preview.Ops[i], real.Ops[i])
		}
	}
	if preview.StatusBefore != real.StatusBefore {
		t.Fatalf("statuses before differ: %s and %s", preview.StatusBefore, real.StatusBefore)
	}
}

// TestHermesEnableLineGoesAtTheEndOfTheUsersList pins where the item lands and
// at which indent, because both are visible in a file the user reads.
func TestHermesEnableLineGoesAtTheEndOfTheUsersList(t *testing.T) {
	// wantBack is what uninstall leaves. It is the original bytes wherever the
	// user already had the block, and an inert `enabled: []` stub in the two
	// cases where Sidecar created the keys: "did Sidecar write this key" is not
	// decidable from the file, and leaving two lines a user can delete is the
	// recoverable error where deleting the user's own keys is not.
	for name, tc := range map[string]struct{ before, wantLine, wantBack string }{
		"an indented list":   {"plugins:\n  enabled:\n    - disk-cleanup\n", "    - " + HermesPluginName, "plugins:\n  enabled:\n    - disk-cleanup\n"},
		"an indentless list": {"plugins:\n  enabled:\n  - disk-cleanup\n", "  - " + HermesPluginName, "plugins:\n  enabled:\n  - disk-cleanup\n"},
		"an empty list":      {"plugins:\n  enabled: []\n", "    - " + HermesPluginName, "plugins:\n  enabled: []\n"},
		"no enabled key":     {"plugins:\n  disabled: []\n", "    - " + HermesPluginName, "plugins:\n  enabled: []\n  disabled: []\n"},
		"no plugins key":     {"model:\n  provider: auto\n", "    - " + HermesPluginName, "model:\n  provider: auto\nplugins:\n  enabled: []\n"},
		"an empty file":      {"", "    - " + HermesPluginName, "plugins:\n  enabled: []\n"},
	} {
		t.Run(name, func(t *testing.T) {
			svc, _, paths := hermesFixture(t)
			writeHermesConfig(t, paths, tc.before)
			hermesApply(t, svc, ActionInstall)

			got := readHermesFile(t, paths.Config)
			var found bool
			for _, line := range strings.Split(got, "\n") {
				if strings.HasPrefix(line, tc.wantLine+" ") || line == tc.wantLine {
					found = true
				}
			}
			if !found {
				t.Fatalf("no line starts with %q:\n%s", tc.wantLine, got)
			}
			if strings.Count(got, HermesPluginName) != 1 {
				t.Fatalf("the plugin is named %d times:\n%s", strings.Count(got, HermesPluginName), got)
			}
			if tc.before != "" && strings.Contains(tc.before, "disk-cleanup") {
				// The user's own item keeps its place, which is what "append at
				// the end" is worth having.
				if strings.Index(got, "disk-cleanup") > strings.Index(got, HermesPluginName) {
					t.Fatalf("Sidecar's item was inserted before the user's:\n%s", got)
				}
			}
			hermesApply(t, svc, ActionUninstall)
			if back := readHermesFile(t, paths.Config); back != tc.wantBack {
				t.Fatalf("uninstall did not leave the file it should:\ngot:\n%s\nwant:\n%s", back, tc.wantBack)
			}
		})
	}
}

// TestHermesRecognisesAQuotedEnableLine is one of Herdr's own fixtures brought
// across: a user or another tool may have written the item quoted, with a
// comment after it, and it is still the same item.
func TestHermesRecognisesAQuotedEnableLine(t *testing.T) {
	svc, _, paths := hermesFixture(t)
	original := "plugins:\n  enabled:\n    - \"" + HermesPluginName + "\" # added by hand\n"
	writeHermesConfig(t, paths, original)
	if err := os.MkdirAll(paths.PluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Init, []byte(HermesInitAsset()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Manifest, []byte(HermesManifestAsset()), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := hermesStatus(t, svc).Status; got != agentlifecycle.StatusCurrent {
		t.Fatalf("a quoted enable line reads as %s", got)
	}
	plan, err := svc.Plan(HermesProvider, ActionInstall)
	if err == nil && !plan.Unchanged {
		t.Fatal("install added a second enable line beside a quoted one")
	}
	hermesApply(t, svc, ActionUninstall)
	if strings.Contains(readHermesFile(t, paths.Config), HermesPluginName) {
		t.Fatalf("uninstall left the quoted line behind:\n%s", readHermesFile(t, paths.Config))
	}
}

// TestHermesPathsAreTheOnesTheAdapterReports keeps the three assets and the
// three reported paths in step, which is what a surface renders before asking
// for confirmation.
func TestHermesPathsAreTheOnesTheAdapterReports(t *testing.T) {
	_, env, paths := hermesFixture(t)
	got := HermesPaths(env)
	want := []string{paths.Init, paths.Manifest, paths.Config}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HermesPaths returned %v, want %v", got, want)
	}
}
