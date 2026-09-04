package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
)

// pluginTestEnv points the command at a config file of the test's own, so no
// run reads or writes the developer's real configuration.
func pluginTestEnv(t *testing.T, contents string) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	config.SetTestConfigPath(configPath)
	config.SetTestStateDir(filepath.Join(dir, "state"))
	t.Cleanup(config.ResetTestConfigPath)
	t.Cleanup(config.ResetTestStateDir)
	// runPluginList initializes the feature manager from the config it reads;
	// put it back afterwards so no later test inherits this one's flags.
	t.Cleanup(func() { features.Init(config.Default()) })

	var out, errOut bytes.Buffer
	return Env{Stdout: &out, Stderr: &errOut, StateDir: filepath.Join(dir, "state")}, &out, &errOut
}

func TestPluginListReportsEveryDescriptor(t *testing.T) {
	env, out, errOut := pluginTestEnv(t, `{}`)
	if code := runPluginList(env, []string{"--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var result pluginListJSON
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v (%s)", err, out.String())
	}

	want := []string{
		"td-monitor", "git-status", "file-browser", "conversations",
		"workspace-manager", "notes", "tasks",
	}
	if len(result.Plugins) != len(want) {
		t.Fatalf("listed %d plugins, want %d: %+v", len(result.Plugins), len(want), result.Plugins)
	}
	for i, id := range want {
		if result.Plugins[i].ID != id {
			t.Fatalf("plugin %d = %q, want %q", i, result.Plugins[i].ID, id)
		}
		if result.Plugins[i].Class != "embedded" {
			t.Errorf("%s class = %q", id, result.Plugins[i].Class)
		}
		if len(result.Plugins[i].Placements) == 0 {
			t.Errorf("%s reported no placements", id)
		}
	}
	// Scope is the lifecycle answer, and Tasks is the one global plugin today.
	for _, p := range result.Plugins {
		wantScope := "project"
		if p.ID == "tasks" {
			wantScope = "global"
		}
		if p.Scope != wantScope {
			t.Errorf("%s scope = %q, want %q", p.ID, p.Scope, wantScope)
		}
	}
}

// The reported switch is plugins.<id>.enabled, with the deprecated flags
// answering only while the key is absent.
func TestPluginListReadsTheUnifiedSwitch(t *testing.T) {
	enabled := func(t *testing.T, contents, id string) bool {
		t.Helper()
		env, out, errOut := pluginTestEnv(t, contents)
		if code := runPluginList(env, []string{"--json"}); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		var result pluginListJSON
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		for _, p := range result.Plugins {
			if p.ID == id {
				return p.Enabled
			}
		}
		t.Fatalf("%q is not listed", id)
		return false
	}

	if enabled(t, `{}`, "tasks") {
		t.Error("tasks is on with neither the key nor the flag set")
	}
	if !enabled(t, `{"features":{"flags":{"tasks_plugin":true}}}`, "tasks") {
		t.Error("the deprecated tasks_plugin flag was ignored with no config key present")
	}
	if enabled(t, `{"features":{"flags":{"tasks_plugin":true}},"plugins":{"tasks":{"enabled":false}}}`, "tasks") {
		t.Error("plugins.tasks.enabled did not outrank the deprecated flag")
	}
	if !enabled(t, `{"plugins":{"tasks":{"enabled":true}}}`, "tasks") {
		t.Error("plugins.tasks.enabled was not read")
	}
	if enabled(t, `{"plugins":{"git-status":{"enabled":false}}}`, "git-status") {
		t.Error("plugins.git-status.enabled was not read")
	}
}

func TestPluginListHumanOutputAndUsageErrors(t *testing.T) {
	env, out, errOut := pluginTestEnv(t, `{}`)
	if code := runPluginList(env, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	text := out.String()
	for _, want := range []string{"td-monitor", "embedded", "project", "global", "tasks", "tab"} {
		if !strings.Contains(text, want) {
			t.Fatalf("human output is missing %q:\n%s", want, text)
		}
	}

	env, _, errOut = pluginTestEnv(t, `{}`)
	if code := runPluginList(env, []string{"--nope"}); code != 2 {
		t.Fatalf("unknown option exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown option") {
		t.Fatalf("unknown option was not explained: %s", errOut.String())
	}

	env, _, errOut = pluginTestEnv(t, `{}`)
	if code := runPluginList(env, []string{"recall"}); code != 2 {
		t.Fatalf("positional argument exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "takes no positional arguments") {
		t.Fatalf("positional argument was not explained: %s", errOut.String())
	}

	env, _, errOut = pluginTestEnv(t, `{}`)
	if code := runPluginRoot(env, []string{"describe"}); code != 2 {
		t.Fatalf("unknown subcommand exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown plugin command") {
		t.Fatalf("unknown subcommand was not explained: %s", errOut.String())
	}
}

// buildPluginFixture compiles the reference plugin executable used by the
// pluginhost conformance suite. The CLI verbs are exercised against the real
// process, because "starts nothing" and "starts exactly one thing" are the
// properties under test.
func buildPluginFixture(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fixtureplugin")
	build := exec.Command("go", "build", "-o", bin, "./testdata/fixtureprovider")
	build.Dir = filepath.Join("..", "pluginhost")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("building the fixture plugin: %v", err)
	}
	return bin
}

// pluginProtocolEnv points the plugin verbs at a config of their own, with the
// plugin protocol turned on.
func pluginProtocolEnv(t *testing.T, contents string) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	env, out, errOut := pluginTestEnv(t, contents)
	env.FeatureOverrides = map[string]bool{features.PluginProtocol.Name: true}
	return env, out, errOut
}

func externalPluginJSON(bin string) string {
	return `{"plugins":{"external":[{"id":"fixture","command":["` + bin + `"],"enabled":true}]}}`
}

func TestPluginListReportsExternalPlugins(t *testing.T) {
	bin := buildPluginFixture(t)
	env, out, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
	if code := runPluginList(env, []string{"--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var result pluginListJSON
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v (%s)", err, out.String())
	}
	last := result.Plugins[len(result.Plugins)-1]
	if last.ID != "fixture" || last.Class != "protocol" {
		t.Fatalf("last row = %+v, want the configured external plugin", last)
	}
	if last.Source != config.PluginSourceExternal {
		t.Fatalf("source = %q", last.Source)
	}
	if last.Protocol != "sidecar.plugin/v1" {
		t.Fatalf("protocol = %q", last.Protocol)
	}
	if !last.Enabled || !last.Active {
		t.Fatalf("row = %+v, want enabled and active", last)
	}
	if last.Describe != nil {
		t.Fatal("list ran describe without --describe")
	}
}

// A terminalResources entry is one plugin in the same list, dispatched on the
// frozen identifier. This is the promise that a Jira provider keeps working.
func TestPluginListReportsLegacyResourceProviders(t *testing.T) {
	bin := buildPluginFixture(t)
	contents := `{"terminalResources":{"providers":[{"id":"jira","command":["` + bin + `"],"enabled":true}]}}`
	env, out, errOut := pluginProtocolEnv(t, contents)
	if code := runPluginList(env, []string{"--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var result pluginListJSON
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	last := result.Plugins[len(result.Plugins)-1]
	if last.ID != "jira" || last.Source != config.PluginSourceTerminalResources {
		t.Fatalf("row = %+v", last)
	}
	if last.Protocol != "sidecar.terminal-resource/v1" {
		t.Fatalf("protocol = %q, want the frozen identifier", last.Protocol)
	}
	if len(last.Placements) != 1 || last.Placements[0] != "panes" {
		t.Fatalf("placements = %v, want [panes]", last.Placements)
	}
	// terminal_resource_providers is off by default, so an entry that is
	// configured and enabled is still not being hosted, and the row says so
	// rather than claiming it is on.
	if last.Active || last.InactiveReason != features.TerminalResourceProviders.Name {
		t.Fatalf("row = %+v, want inactive naming terminal_resource_providers", last)
	}
}

// An enabled plugin whose flag is off is configured and not hosted, and the row
// must not claim otherwise.
func TestPluginListMarksAnInactivePluginWithItsFlag(t *testing.T) {
	bin := buildPluginFixture(t)
	env, out, errOut := pluginTestEnv(t, externalPluginJSON(bin))
	if code := runPluginList(env, []string{"--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var result pluginListJSON
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	last := result.Plugins[len(result.Plugins)-1]
	if !last.Enabled || last.Active {
		t.Fatalf("row = %+v, want enabled but inactive", last)
	}
	if last.InactiveReason != features.PluginProtocol.Name {
		t.Fatalf("inactiveReason = %q", last.InactiveReason)
	}
}

// list starts nothing without --describe. The marker script proves it from the
// outside rather than by trusting the code path.
func TestPluginListStartsNoProcessWithoutDescribe(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "plugin.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env, _, errOut := pluginProtocolEnv(t, externalPluginJSON(script))
	if code := runPluginList(env, []string{"--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("plugin list started a plugin process without --describe")
	}
}

func TestPluginListDescribeRunsThePlugin(t *testing.T) {
	bin := buildPluginFixture(t)
	env, out, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
	if code := runPluginList(env, []string{"--describe", "--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var result pluginListJSON
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	last := result.Plugins[len(result.Plugins)-1]
	if last.Describe == nil || !last.Describe.OK {
		t.Fatalf("describe = %+v", last.Describe)
	}
	if len(last.Describe.Collections) != 2 || len(last.Describe.Actions) != 4 {
		t.Fatalf("describe = %+v", last.Describe)
	}
	if len(last.Describe.Context) != 1 || last.Describe.Context[0] != "project" {
		t.Fatalf("context = %v", last.Describe.Context)
	}
}

func TestPluginCheckListsAndGets(t *testing.T) {
	bin := buildPluginFixture(t)
	env, out, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
	code := runPluginCheck(env, []string{"fixture", "--list", "results", "--query", "dex", "--get", "results", "rc:notes:1", "--json"})
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
	}
	var report pluginCheckReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v (%s)", err, out.String())
	}
	if report.State != "ready" || report.Describe == nil || !report.Describe.OK {
		t.Fatalf("report = %+v", report)
	}
	if report.List == nil || !report.List.OK || len(report.List.Page.Items) != 3 {
		t.Fatalf("list = %+v", report.List)
	}
	if report.List.Page.Outcome != "answered" {
		t.Fatalf("outcome = %q", report.List.Page.Outcome)
	}
	if report.Get == nil || !report.Get.OK || len(report.Get.Sections) != 4 {
		t.Fatalf("get = %+v", report.Get)
	}
	// The plugin's passEnv values are never printed, and neither is its stdout.
	if strings.Contains(out.String(), "aFieldTheHostHasNeverHeardOf") {
		t.Fatal("the plugin's raw response reached the output")
	}
}

func TestPluginCheckHumanOutputAndRefusals(t *testing.T) {
	bin := buildPluginFixture(t)

	env, out, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
	if code := runPluginCheck(env, []string{"fixture"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{"collection results", "action log-note", "reads context project", "sidecar.plugin/v1"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("human output is missing %q:\n%s", want, out.String())
		}
	}

	env, _, errOut = pluginProtocolEnv(t, externalPluginJSON(bin))
	if code := runPluginCheck(env, []string{"nope"}); code != pluginExitNotFound {
		t.Fatalf("unknown plugin exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "no plugin named") {
		t.Fatalf("unknown plugin was not explained: %s", errOut.String())
	}

	env, _, errOut = pluginProtocolEnv(t, externalPluginJSON(bin))
	if code := runPluginCheck(env, []string{"fixture", "--query", "dex"}); code != pluginExitUsage {
		t.Fatalf("--query without --list exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "applies only to --list") {
		t.Fatalf("stray --query was not explained: %s", errOut.String())
	}

	// A resource provider has no list or get, and says so rather than running.
	contents := `{"terminalResources":{"providers":[{"id":"jira","command":["` + bin + `"],"enabled":true}]}}`
	env, _, errOut = pluginProtocolEnv(t, contents)
	if code := runPluginCheck(env, []string{"jira", "--list", "results"}); code != pluginExitFailed {
		t.Fatalf("list on a resource provider exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "has no list or get") {
		t.Fatalf("the refusal did not explain itself: %s", errOut.String())
	}
}

func TestPluginCallEveryMethod(t *testing.T) {
	bin := buildPluginFixture(t)

	t.Run("list", func(t *testing.T) {
		env, out, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
		code := runPluginCall(env, []string{"fixture", "list", "--params", `{"collection":"results","query":"dex"}`, "--json"})
		if code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		var report pluginCallReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("decode: %v (%s)", err, out.String())
		}
		if !report.OK || report.Page == nil || len(report.Page.Items) != 3 {
			t.Fatalf("report = %+v", report)
		}
	})

	t.Run("get", func(t *testing.T) {
		env, out, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
		code := runPluginCall(env, []string{"fixture", "get", "--params", `{"collection":"results","id":"rc:notes:1"}`, "--json"})
		if code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		var report pluginCallReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if !report.OK || report.Resource == nil || len(report.Sections) != 4 {
			t.Fatalf("report = %+v", report)
		}
	})

	t.Run("act", func(t *testing.T) {
		env, out, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
		code := runPluginCall(env, []string{"fixture", "act", "--params",
			`{"action":"log-note","collection":"results","id":"rc:notes:1","inputs":{"text":"hello"}}`, "--json"})
		if code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		var report pluginCallReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if !report.OK || report.Result == nil || report.Result.Status != "done" {
			t.Fatalf("report = %+v", report)
		}
		if report.Result.Open == nil || report.Result.Open.ID != "rc:notes:1" {
			t.Fatalf("open = %+v", report.Result.Open)
		}
	})

	t.Run("describe", func(t *testing.T) {
		env, out, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
		if code := runPluginCall(env, []string{"fixture", "describe", "--json"}); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		var report pluginCallReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if !report.OK || report.Describe == nil || len(report.Describe.Collections) != 2 {
			t.Fatalf("report = %+v", report)
		}
	})

	t.Run("undeclared collection", func(t *testing.T) {
		env, out, _ := pluginProtocolEnv(t, externalPluginJSON(bin))
		code := runPluginCall(env, []string{"fixture", "list", "--params", `{"collection":"nowhere"}`, "--json"})
		if code != pluginExitFailed {
			t.Fatalf("exit %d, want %d", code, pluginExitFailed)
		}
		if !strings.Contains(out.String(), "declares no collection") {
			t.Fatalf("output = %s", out.String())
		}
	})

	t.Run("unknown method", func(t *testing.T) {
		env, _, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
		if code := runPluginCall(env, []string{"fixture", "explode"}); code != pluginExitUsage {
			t.Fatalf("exit %d", code)
		}
		if !strings.Contains(errOut.String(), "unknown method") {
			t.Fatalf("stderr = %s", errOut.String())
		}
	})
}

// add starts nothing, prints exactly what will run, and needs a confirmation.
func TestPluginAddPrintsThePlanAndStartsNothing(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "plugin.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	env, out, errOut := pluginProtocolEnv(t, `{}`)
	env.Stdin = strings.NewReader("n\n")
	if code := runPluginAdd(env, []string{"recall", "--command", script, "sidecar-plugin"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	text := out.String()
	for _, want := range []string{"argv[0]", script, "argv[1]", "sidecar-plugin", "crash isolation, not a sandbox", "Configure it? [y/N]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the plan is missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "Not configured.") {
		t.Fatalf("a declined confirmation still configured it:\n%s", text)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("plugin add started the plugin")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.External) != 0 {
		t.Fatalf("a declined add still wrote an entry: %+v", cfg.Plugins.External)
	}
}

func TestPluginAddEnableDisableRemove(t *testing.T) {
	script := filepath.Join(t.TempDir(), "plugin.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	env, out, errOut := pluginProtocolEnv(t, `{"prompts":[{"name":"keep me"}]}`)
	code := runPluginAdd(env, []string{"recall", "--pass-env", "RECALL_PROFILE", "--placement", "panes",
		"--timeout", "12s", "--yes", "--json", "--command", script})
	if code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut.String())
	}
	var added pluginConfigReport
	if err := json.Unmarshal(out.Bytes(), &added); err != nil {
		t.Fatalf("decode: %v (%s)", err, out.String())
	}
	if !added.Applied || added.Scope != config.PluginScopeGlobal {
		t.Fatalf("report = %+v", added)
	}
	if len(added.Placements) != 1 || added.Placements[0] != "panes" {
		t.Fatalf("placements = %v", added.Placements)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.External) != 1 || cfg.Plugins.External[0].Timeout != 12*time.Second {
		t.Fatalf("config = %+v", cfg.Plugins.External)
	}

	// A second add under the same id is refused rather than shadowing the first.
	env, _, errOut = pluginProtocolEnvKeepingConfig(t)
	if code := runPluginAdd(env, []string{"recall", "--yes", "--command", script}); code != pluginExitUsage {
		t.Fatalf("a duplicate add exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "already configured") {
		t.Fatalf("the duplicate was not explained: %s", errOut.String())
	}

	env, _, errOut = pluginProtocolEnvKeepingConfig(t)
	if code := runPluginDisable(env, []string{"recall", "--json"}); code != 0 {
		t.Fatalf("disable exit %d: %s", code, errOut.String())
	}
	cfg, _ = config.Load()
	if cfg.Plugins.External[0].Enabled {
		t.Fatal("disable did not turn the entry off")
	}

	env, out, errOut = pluginProtocolEnvKeepingConfig(t)
	if code := runPluginEnable(env, []string{"recall"}); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Restart") {
		t.Fatalf("enable did not say a restart is needed: %s", out.String())
	}
	cfg, _ = config.Load()
	if !cfg.Plugins.External[0].Enabled {
		t.Fatal("enable did not turn the entry on")
	}

	env, _, errOut = pluginProtocolEnvKeepingConfig(t)
	if code := runPluginRemove(env, []string{"recall", "--json"}); code != 0 {
		t.Fatalf("remove exit %d: %s", code, errOut.String())
	}
	cfg, _ = config.Load()
	if len(cfg.Plugins.External) != 0 {
		t.Fatalf("remove left %+v", cfg.Plugins.External)
	}
	raw, err := os.ReadFile(config.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "keep me") {
		t.Fatalf("the unmanaged section was dropped:\n%s", raw)
	}
}

// pluginProtocolEnvKeepingConfig builds a fresh Env against the config path the
// test already set, so a sequence of verbs operates on one file.
func pluginProtocolEnvKeepingConfig(t *testing.T) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	return Env{
		Stdout:           &out,
		Stderr:           &errOut,
		FeatureOverrides: map[string]bool{features.PluginProtocol.Name: true},
	}, &out, &errOut
}

// Every verb that would run or configure a draft-protocol plugin refuses while
// the flag is off, and says how to turn it on.
func TestPluginVerbsRefuseWhileTheFlagIsOff(t *testing.T) {
	bin := buildPluginFixture(t)
	cases := []struct {
		name string
		run  func(Env, []string) int
		args []string
	}{
		{"check", runPluginCheck, []string{"fixture"}},
		{"call", runPluginCall, []string{"fixture", "describe"}},
		{"add", runPluginAdd, []string{"other", "--yes", "--command", bin}},
		{"enable", runPluginEnable, []string{"fixture"}},
		{"disable", runPluginDisable, []string{"fixture"}},
		{"remove", runPluginRemove, []string{"fixture"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, _, errOut := pluginTestEnv(t, externalPluginJSON(bin))
			if code := tc.run(env, tc.args); code != pluginExitRefused {
				t.Fatalf("exit %d, want %d", code, pluginExitRefused)
			}
			if !strings.Contains(errOut.String(), features.PluginProtocol.Name) {
				t.Fatalf("the refusal did not name the flag: %s", errOut.String())
			}
		})
	}
}

// The config verbs do not own terminalResources.providers, and say so instead
// of editing a section that belongs to the frozen protocol's own surface.
func TestPluginConfigVerbsRefuseTheLegacySection(t *testing.T) {
	bin := buildPluginFixture(t)
	contents := `{"terminalResources":{"providers":[{"id":"jira","command":["` + bin + `"],"enabled":true}]}}`
	env, _, errOut := pluginProtocolEnv(t, contents)
	if code := runPluginDisable(env, []string{"jira"}); code != pluginExitRefused {
		t.Fatalf("exit %d, want %d", code, pluginExitRefused)
	}
	if !strings.Contains(errOut.String(), "terminal-links") {
		t.Fatalf("the refusal did not point at the owning surface: %s", errOut.String())
	}
}

// `plugin check --list` prints what the host will actually send, so an author
// can see a key being dropped rather than wondering why their filter did
// nothing.
func TestPluginCheckPrintsTheAppliedFilters(t *testing.T) {
	bin := buildPluginFixture(t)

	env, out, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
	code := runPluginCheck(env, []string{
		"fixture", "--list", "results", "--query", "filters",
		"--filter", "scope=notes", // declared, not the default: sent
		"--filter=source=any",    // declared, but IS the default: dropped
		"--filter", "smuggled=x", // never declared: dropped
		"--json",
	})
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
	}
	var report pluginCheckReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v (%s)", err, out.String())
	}
	if report.List == nil || len(report.List.Filters) != 1 || report.List.Filters["scope"] != "notes" {
		t.Fatalf("applied filters = %v, want only scope=notes", report.List.Filters)
	}
	// The fixture echoes what actually reached it, so this asserts the wire
	// rather than the host's own bookkeeping.
	if got := report.List.Page.Items[0].Cells["title"]; got != "scope=notes" {
		t.Fatalf("the plugin received %q", got)
	}

	// The declaration is printed too, with the scope marked.
	env, out, errOut = pluginProtocolEnv(t, externalPluginJSON(bin))
	if code := runPluginCheck(env, []string{"fixture", "--list", "results", "--query", "filters", "--filter", "scope=notes"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{`filter scope "Scope" kind=choice scope`, "default=everything", "filters   scope=notes"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("human output is missing %q:\n%s", want, out.String())
		}
	}

	// --get carries the same set, because a row is expanded under the scope it
	// was found in, and reports it the same way the list block does.
	env, out, errOut = pluginProtocolEnv(t, externalPluginJSON(bin))
	code = runPluginCheck(env, []string{
		"fixture", "--get", "results", "rc:notes:1",
		"--filter", "scope=notes", // declared, not the default: sent
		"--filter=source=any",    // declared, but IS the default: dropped
		"--filter", "smuggled=x", // never declared: dropped
	})
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "filters   scope=notes") {
		t.Fatalf("the get block did not print the scope it ran under:\n%s", out.String())
	}
	env, out, errOut = pluginProtocolEnv(t, externalPluginJSON(bin))
	code = runPluginCheck(env, []string{
		"fixture", "--get", "results", "rc:notes:1",
		"--filter", "scope=notes", "--filter=source=any", "--filter", "smuggled=x", "--json",
	})
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
	}
	report = pluginCheckReport{}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v (%s)", err, out.String())
	}
	if report.Get == nil || len(report.Get.Filters) != 1 || report.Get.Filters["scope"] != "notes" {
		t.Fatalf("applied get filters = %v, want only scope=notes", report.Get.Filters)
	}
	scope := ""
	for _, f := range report.Get.Resource.Fields {
		if f.Label == "Scope" {
			scope = f.Value
		}
	}
	if scope != "scope=notes" {
		t.Fatalf("the plugin received scope %q on get", scope)
	}

	// A filter with no list or get to apply it to is a usage error, exactly as
	// a stray --query is.
	env, _, errOut = pluginProtocolEnv(t, externalPluginJSON(bin))
	if code := runPluginCheck(env, []string{"fixture", "--filter", "scope=notes"}); code != pluginExitUsage {
		t.Fatalf("stray --filter exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "applies only to --list") {
		t.Fatalf("stray --filter was not explained: %s", errOut.String())
	}

	env, _, errOut = pluginProtocolEnv(t, externalPluginJSON(bin))
	if code := runPluginCheck(env, []string{"fixture", "--list", "results", "--filter", "novalue"}); code != pluginExitUsage {
		t.Fatalf("malformed --filter exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "takes id=value") {
		t.Fatalf("malformed --filter was not explained: %s", errOut.String())
	}
}

// The same shorthand on `plugin call`, which is the raw-method authoring loop.
func TestPluginCallAcceptsFilterFlags(t *testing.T) {
	bin := buildPluginFixture(t)

	env, out, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
	code := runPluginCall(env, []string{
		"fixture", "list",
		"--params", `{"collection":"results","query":"filters","filters":{"since":"2026-08-01"}}`,
		"--filter", "scope=notes",
		"--json",
	})
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
	}
	report := pluginCallReport{}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v (%s)", err, out.String())
	}
	if got := report.Page.Items[0].Cells["title"]; got != "scope=notes;since=2026-08-01" {
		t.Fatalf("the plugin received %q; --filter merges with params.filters", got)
	}

	// get takes them too: a row is expanded under the scope it was found in.
	env, out, errOut = pluginProtocolEnv(t, externalPluginJSON(bin))
	code = runPluginCall(env, []string{
		"fixture", "get",
		"--params", `{"collection":"results","id":"rc:notes:1","filters":{"since":"2026-08-01"}}`,
		"--filter", "scope=notes",
		"--json",
	})
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
	}
	report = pluginCallReport{}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v (%s)", err, out.String())
	}
	scope := ""
	for _, f := range report.Resource.Fields {
		if f.Label == "Scope" {
			scope = f.Value
		}
	}
	if scope != "scope=notes;since=2026-08-01" {
		t.Fatalf("the plugin received scope %q; --filter merges with params.filters on get too", scope)
	}

	// act still refuses them: an action names its own subject, and a filter
	// there would be describing a page nobody is looking at.
	env, _, errOut = pluginProtocolEnv(t, externalPluginJSON(bin))
	if code := runPluginCall(env, []string{"fixture", "act", "--filter", "scope=notes"}); code != pluginExitUsage {
		t.Fatalf("--filter on act exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "applies only to list and get") {
		t.Fatalf("the refusal did not explain itself: %s", errOut.String())
	}
}

// A page's omitted counts and per-source coverage reach the CLI too, so an
// author can prove the shape before the host draws it.
func TestPluginCheckPrintsOmittedAndCoverage(t *testing.T) {
	bin := buildPluginFixture(t)
	env, out, errOut := pluginProtocolEnv(t, externalPluginJSON(bin))
	if code := runPluginCheck(env, []string{"fixture", "--list", "results", "--query", "coverage", "--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var report pluginCheckReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v (%s)", err, out.String())
	}
	page := report.List.Page
	if page.Omitted == nil || page.Omitted.Suppressed != 1 || page.Omitted.Dropped != 6 {
		t.Fatalf("omitted = %+v", page.Omitted)
	}
	if len(page.Coverage) != 13 {
		t.Fatalf("coverage = %d rows, want 13", len(page.Coverage))
	}

	env, out, errOut = pluginProtocolEnv(t, externalPluginJSON(bin))
	if code := runPluginCheck(env, []string{"fixture", "--list", "results", "--query", "coverage"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{"omitted   1 below floor, 6 over budget", "coverage  mail  unhealthy"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("human output is missing %q:\n%s", want, out.String())
		}
	}
}
