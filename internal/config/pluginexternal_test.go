package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExternalPluginDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.Plugins.External = []PluginInstanceConfig{{
		ID:      "  recall  ",
		Command: []string{" recall ", "sidecar-plugin"},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	got := cfg.Plugins.External[0]
	if got.ID != "recall" {
		t.Fatalf("id = %q, want %q", got.ID, "recall")
	}
	if got.Command[0] != "recall" {
		t.Fatalf("argv[0] = %q, want %q", got.Command[0], "recall")
	}
	if got.Scope != PluginScopeGlobal {
		t.Fatalf("scope = %q, want %q", got.Scope, PluginScopeGlobal)
	}
	// An explicitly configured plugin is one the user asked for, so the
	// default shows it wherever it can be shown.
	if len(got.Placements) != 2 || got.Placements[0] != PluginPlacementTab || got.Placements[1] != PluginPlacementPanes {
		t.Fatalf("placements = %v, want [tab panes]", got.Placements)
	}
	if got.Timeout != DefaultTerminalResourceTimeout {
		t.Fatalf("timeout = %s, want %s", got.Timeout, DefaultTerminalResourceTimeout)
	}
}

func TestExternalPluginValidationRefusals(t *testing.T) {
	tests := []struct {
		name    string
		entries []PluginInstanceConfig
		want    string
	}{
		{
			name:    "project scope",
			entries: []PluginInstanceConfig{{ID: "ongoing", Command: []string{"ongoing"}, Scope: "project"}},
			want:    "does not support",
		},
		{
			name:    "unknown scope",
			entries: []PluginInstanceConfig{{ID: "ongoing", Command: []string{"ongoing"}, Scope: "session"}},
			want:    "the only supported value",
		},
		{
			name:    "unknown placement",
			entries: []PluginInstanceConfig{{ID: "dex", Command: []string{"dex"}, Placements: []string{"sidebar"}}},
			want:    "is not one of",
		},
		{
			name:    "no command",
			entries: []PluginInstanceConfig{{ID: "dex"}},
			want:    "has no command",
		},
		{
			name:    "no id",
			entries: []PluginInstanceConfig{{Command: []string{"dex"}}},
			want:    "has no id",
		},
		{
			name: "duplicate id",
			entries: []PluginInstanceConfig{
				{ID: "dex", Command: []string{"dex"}},
				{ID: "dex", Command: []string{"dex", "other"}},
			},
			want: "configured more than once",
		},
		{
			name:    "collides with an embedded plugin",
			entries: []PluginInstanceConfig{{ID: "notes", Command: []string{"sidecar-notes"}}},
			want:    `plugin id "notes" is already the id of Sidecar's built-in Notes surface`,
		},
		{
			name:    "collides with a global plugin",
			entries: []PluginInstanceConfig{{ID: "tasks", Command: []string{"sidecar-tasks"}}},
			want:    `plugin id "tasks" is already the id of Sidecar's built-in Tasks surface`,
		},
		{
			name:    "collides with an app-owned global tab",
			entries: []PluginInstanceConfig{{ID: " sessions ", Command: []string{"sidecar-sessions"}}},
			want:    `is already the id of Sidecar's built-in Sessions surface`,
		},
		{
			name:    "inline secret in passEnv",
			entries: []PluginInstanceConfig{{ID: "dex", Command: []string{"dex"}, PassEnv: []string{"TOKEN=hunter2"}}},
			want:    "looks like an inline value",
		},
		{
			name:    "claim host with a scheme",
			entries: []PluginInstanceConfig{{ID: "dex", Command: []string{"dex"}, ClaimHosts: []string{"https://example.test"}}},
			want:    "is not a bare hostname",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Plugins.External = tc.entries
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %+v", tc.entries)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// A refused inline secret must not echo the value back into the message: the
// error is read in a terminal, a log, and a screenshot.
func TestExternalPluginPassEnvRefusalHidesTheValue(t *testing.T) {
	cfg := &Config{}
	cfg.Plugins.External = []PluginInstanceConfig{{ID: "dex", Command: []string{"dex"}, PassEnv: []string{"TOKEN=hunter2"}}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted an inline value")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("the refusal echoed the secret: %q", err)
	}
}

func TestExternalPluginCountBound(t *testing.T) {
	cfg := &Config{}
	for i := 0; i <= MaxExternalPlugins; i++ {
		cfg.Plugins.External = append(cfg.Plugins.External, PluginInstanceConfig{
			ID:      string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Command: []string{"plugin"},
		})
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("Validate accepted %d plugins, the limit is %d", len(cfg.Plugins.External), MaxExternalPlugins)
	}
}

func TestPluginInstancesMergesBothSections(t *testing.T) {
	cfg := &Config{}
	cfg.Plugins.External = []PluginInstanceConfig{{ID: "recall", Command: []string{"recall", "sidecar-plugin"}, Enabled: true}}
	cfg.TerminalResources.Providers = []TerminalResourceProviderConfig{
		{ID: "jira", Command: []string{"sidecar-jira"}, Enabled: true, Timeout: 3 * time.Second},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	got := cfg.PluginInstances()
	if len(got) != 2 {
		t.Fatalf("PluginInstances returned %d entries, want 2: %+v", len(got), got)
	}
	// The newer section leads, because order is precedence.
	if got[0].ID != "recall" || got[0].Source != PluginSourceExternal {
		t.Fatalf("first entry = %+v, want recall from %s", got[0], PluginSourceExternal)
	}
	if got[0].IsLegacyResourceProvider() {
		t.Fatal("a plugins.external entry reported itself as a legacy resource provider")
	}
	legacy := got[1]
	if legacy.ID != "jira" || legacy.Source != PluginSourceTerminalResources {
		t.Fatalf("second entry = %+v, want jira from %s", legacy, PluginSourceTerminalResources)
	}
	if !legacy.IsLegacyResourceProvider() {
		t.Fatal("a terminalResources entry did not report itself as a legacy resource provider")
	}
	// A resource provider is panes-only and global: it has no navbar tab and
	// no lifecycle of its own, which is exactly what it was before this plan.
	if legacy.Scope != PluginScopeGlobal {
		t.Fatalf("legacy scope = %q, want %q", legacy.Scope, PluginScopeGlobal)
	}
	if len(legacy.Placements) != 1 || legacy.Placements[0] != PluginPlacementPanes {
		t.Fatalf("legacy placements = %v, want [panes]", legacy.Placements)
	}
	if legacy.Timeout != 3*time.Second {
		t.Fatalf("legacy timeout = %s, want 3s", legacy.Timeout)
	}
}

// One ID configured in both sections is one plugin: plugins.external answers
// and the legacy entry is dropped, so a half-finished migration cannot start
// two child processes under one identity.
func TestPluginInstancesPrefersTheNewSection(t *testing.T) {
	cfg := &Config{}
	cfg.Plugins.External = []PluginInstanceConfig{{ID: "jira", Command: []string{"sidecar-jira", "v2"}, Enabled: true}}
	cfg.TerminalResources.Providers = []TerminalResourceProviderConfig{
		{ID: "jira", Command: []string{"sidecar-jira"}, Enabled: true},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	got := cfg.PluginInstances()
	if len(got) != 1 {
		t.Fatalf("PluginInstances returned %d entries, want 1: %+v", len(got), got)
	}
	if got[0].Source != PluginSourceExternal || len(got[0].Command) != 2 {
		t.Fatalf("entry = %+v, want the plugins.external command", got[0])
	}
}

func TestExternalPluginRoundTripsThroughSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	SetTestConfigPath(path)
	t.Cleanup(ResetTestConfigPath)

	// A section Sidecar does not manage must survive a save that rewrites the
	// plugins block beside it.
	seed := `{"prompts":[{"name":"keep me"}],"plugins":{"external":[{"id":"recall","command":["recall","sidecar-plugin"],"passEnv":["RECALL_PROFILE"],"placements":["tab"],"timeout":"12s"}]}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Plugins.External) != 1 {
		t.Fatalf("loaded %d external plugins, want 1", len(cfg.Plugins.External))
	}
	loaded := cfg.Plugins.External[0]
	if !loaded.Enabled {
		t.Fatal("an entry with no enabled key loaded as disabled")
	}
	if loaded.Timeout != 12*time.Second {
		t.Fatalf("timeout = %s, want 12s", loaded.Timeout)
	}
	if len(loaded.Placements) != 1 || loaded.Placements[0] != PluginPlacementTab {
		t.Fatalf("placements = %v, want [tab]", loaded.Placements)
	}

	if err := SavePlugins(func(p *PluginsConfig) {
		p.External = append(p.External, PluginInstanceConfig{
			ID: "dex", Command: []string{"dex", "sidecar-plugin"}, Enabled: true,
		})
	}); err != nil {
		t.Fatalf("SavePlugins: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(written, &raw); err != nil {
		t.Fatalf("saved config is not JSON: %v\n%s", err, written)
	}
	if _, ok := raw["prompts"]; !ok {
		t.Fatalf("the saver dropped the unmanaged prompts section:\n%s", written)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Plugins.External) != 2 {
		t.Fatalf("reloaded %d external plugins, want 2:\n%s", len(reloaded.Plugins.External), written)
	}
	if reloaded.Plugins.External[1].ID != "dex" {
		t.Fatalf("second entry = %q, want dex", reloaded.Plugins.External[1].ID)
	}
	if reloaded.Plugins.External[0].PassEnv[0] != "RECALL_PROFILE" {
		t.Fatalf("passEnv was not preserved: %+v", reloaded.Plugins.External[0])
	}
}

// Emptying the list must remove the key rather than leave the old entries
// behind. plugins is re-marshalled whole on every save, so this falls out of
// the omitempty rather than needing a delete — and this test is what says so.
func TestRemovingTheLastExternalPluginClearsTheKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	SetTestConfigPath(path)
	t.Cleanup(ResetTestConfigPath)
	seed := `{"plugins":{"external":[{"id":"recall","command":["recall"]}]}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SavePlugins(func(p *PluginsConfig) { p.External = nil }); err != nil {
		t.Fatalf("SavePlugins: %v", err)
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Plugins.External) != 0 {
		t.Fatalf("the removed plugin came back: %+v", reloaded.Plugins.External)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), "recall") {
		t.Fatalf("the emptied section was carried forward:\n%s", written)
	}
}
