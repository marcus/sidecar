package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSave_PreservesUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write a config file that includes a "prompts" key (not managed by Save)
	initial := []byte(`{
  "prompts": [
    {"name": "My Prompt", "ticketMode": "required", "body": "do the thing {{ticket}}"}
  ],
  "customKey": "should survive"
}`)
	if err := os.WriteFile(path, initial, 0644); err != nil {
		t.Fatal(err)
	}

	// Point Save() at our temp file
	SetTestConfigPath(path)
	defer ResetTestConfigPath()

	// Save a default config
	cfg := Default()
	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Read back and verify prompts and customKey still exist
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal saved config: %v", err)
	}

	if _, ok := raw["prompts"]; !ok {
		t.Error("Save() deleted 'prompts' key from config.json")
	}
	if _, ok := raw["customKey"]; !ok {
		t.Error("Save() deleted 'customKey' from config.json")
	}

	// Verify prompts content is intact
	var prompts []map[string]interface{}
	if err := json.Unmarshal(raw["prompts"], &prompts); err != nil {
		t.Fatalf("unmarshal prompts: %v", err)
	}
	if len(prompts) != 1 {
		t.Errorf("got %d prompts, want 1", len(prompts))
	}
	if prompts[0]["name"] != "My Prompt" {
		t.Errorf("got prompt name %q, want 'My Prompt'", prompts[0]["name"])
	}

	// Verify managed keys are also present
	if _, ok := raw["projects"]; !ok {
		t.Error("Save() did not write 'projects' key")
	}
	if _, ok := raw["plugins"]; !ok {
		t.Error("Save() did not write 'plugins' key")
	}
}

func TestSave_LastOpenInApp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	SetTestConfigPath(path)
	defer ResetTestConfigPath()

	// Create a config with UI.LastOpenInApp and a project with LastOpenInApp
	cfg := Default()
	cfg.UI.LastOpenInApp = "vscode"
	cfg.Projects.List = []ProjectConfig{
		{
			Name:          "my-project",
			Path:          "/home/user/my-project",
			LastOpenInApp: "goland",
		},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Reload and verify both values round-trip
	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}

	if loaded.UI.LastOpenInApp != "vscode" {
		t.Errorf("UI.LastOpenInApp = %q, want %q", loaded.UI.LastOpenInApp, "vscode")
	}
	if len(loaded.Projects.List) != 1 {
		t.Fatalf("got %d projects, want 1", len(loaded.Projects.List))
	}
	if loaded.Projects.List[0].LastOpenInApp != "goland" {
		t.Errorf("Projects.List[0].LastOpenInApp = %q, want %q", loaded.Projects.List[0].LastOpenInApp, "goland")
	}
}

func TestSaveLastOpenInApp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	SetTestConfigPath(path)
	defer ResetTestConfigPath()

	// Seed a config with a project
	cfg := Default()
	cfg.Projects.List = []ProjectConfig{
		{Name: "my-project", Path: "/home/user/my-project"},
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Use SaveLastOpenInApp with a matching project path
	if err := SaveLastOpenInApp("/home/user/my-project", "goland"); err != nil {
		t.Fatalf("SaveLastOpenInApp failed: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}

	if loaded.UI.LastOpenInApp != "goland" {
		t.Errorf("UI.LastOpenInApp = %q, want %q", loaded.UI.LastOpenInApp, "goland")
	}
	if loaded.Projects.List[0].LastOpenInApp != "goland" {
		t.Errorf("project LastOpenInApp = %q, want %q", loaded.Projects.List[0].LastOpenInApp, "goland")
	}

	// Use SaveLastOpenInApp with a non-matching path: only global should update
	if err := SaveLastOpenInApp("/nonexistent", "cursor"); err != nil {
		t.Fatalf("SaveLastOpenInApp failed: %v", err)
	}

	loaded, err = LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}

	if loaded.UI.LastOpenInApp != "cursor" {
		t.Errorf("UI.LastOpenInApp = %q, want %q", loaded.UI.LastOpenInApp, "cursor")
	}
	// Project should still have "goland" from previous save
	if loaded.Projects.List[0].LastOpenInApp != "goland" {
		t.Errorf("project LastOpenInApp = %q, want %q (should not change)", loaded.Projects.List[0].LastOpenInApp, "goland")
	}
}

func TestSave_WorksWithNoExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	SetTestConfigPath(path)
	defer ResetTestConfigPath()

	cfg := Default()
	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file was created and is valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := raw["projects"]; !ok {
		t.Error("missing 'projects' key")
	}
}

func TestSave_WritesWorkspaceAgentSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	SetTestConfigPath(path)
	defer ResetTestConfigPath()

	cfg := Default()
	cfg.Plugins.Workspace.DefaultAgentType = "codex"
	cfg.Plugins.Workspace.AgentStart = map[string]string{
		"codex": "codex --dangerously-bypass-approvals-and-sandbox",
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var plugins map[string]json.RawMessage
	if err := json.Unmarshal(raw["plugins"], &plugins); err != nil {
		t.Fatalf("unmarshal plugins: %v", err)
	}

	var workspace map[string]interface{}
	if err := json.Unmarshal(plugins["workspace"], &workspace); err != nil {
		t.Fatalf("unmarshal workspace: %v", err)
	}

	if got := workspace["defaultAgentType"]; got != "codex" {
		t.Errorf("defaultAgentType = %v, want %q", got, "codex")
	}
	agentStart, ok := workspace["agentStart"].(map[string]interface{})
	if !ok {
		t.Fatalf("agentStart type = %T, want object", workspace["agentStart"])
	}
	if got := agentStart["codex"]; got != "codex --dangerously-bypass-approvals-and-sandbox" {
		t.Errorf("agentStart.codex = %v, want %q", got, "codex --dangerously-bypass-approvals-and-sandbox")
	}
}

func TestSave_WorkspaceAutoCreateShellRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	SetTestConfigPath(path)
	defer ResetTestConfigPath()

	cfg := Default()
	cfg.Plugins.Workspace.AutoCreateShell = true

	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}
	if !loaded.Plugins.Workspace.AutoCreateShell {
		t.Error("AutoCreateShell did not survive a save/load round trip")
	}
}

func TestSave_OverviewWorktreeScopeRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	SetTestConfigPath(path)
	defer ResetTestConfigPath()

	cfg := Default()
	cfg.Plugins.Workspace.OverviewWorktreeScope = OverviewWorktreeScopeWorktree
	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}
	if loaded.Plugins.Workspace.OverviewWorktreeScope != OverviewWorktreeScopeWorktree {
		t.Fatalf("OverviewWorktreeScope = %q, want %q", loaded.Plugins.Workspace.OverviewWorktreeScope, OverviewWorktreeScopeWorktree)
	}
}

func TestSave_RoundTripsSelectionCopyOnSelect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	SetTestConfigPath(path)
	defer ResetTestConfigPath()

	cfg := Default()
	cfg.Selection.CopyOnSelect = true
	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	saved, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}
	if !saved.Selection.CopyOnSelect {
		t.Error("Selection.CopyOnSelect did not survive a save")
	}

	// Turning it back off must write the key, not leave the old value behind
	// through Save's unknown-key preservation.
	saved.Selection.CopyOnSelect = false
	if err := Save(saved); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	reloaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}
	if reloaded.Selection.CopyOnSelect {
		t.Error("Selection.CopyOnSelect stayed on after being turned off")
	}
}

func TestSave_RoundTripsNotesDefaultEditor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	SetTestConfigPath(path)
	t.Cleanup(ResetTestConfigPath)

	cfg := Default()
	cfg.Plugins.Notes.DefaultEditor = NotesEditorPane
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Plugins.Notes.DefaultEditor; got != NotesEditorPane {
		t.Fatalf("default editor = %q, want %q", got, NotesEditorPane)
	}
}

// A subsection under "plugins" that this build does not know about — a newer
// release's key, or a hand-written one — must survive a save the way an unknown
// top-level key does. Save used to rewrite the whole plugins object, so a
// plugins.external entry written by a newer Sidecar was silently dropped by an
// older one, which is exactly the case the migration into plugins.external
// makes common.
func TestSavePreservesUnknownPluginsSubkeys(t *testing.T) {
	path := writeConfig(t, `{
	  "plugins": {
	    "td-monitor": {"enabled": true},
	    "recall": {"kept": ["by", "the", "merge"]},
	    "external-experiment": {"enabled": false}
	  }
	}`)
	SetTestConfigPath(path)
	t.Cleanup(ResetTestConfigPath)

	if err := SaveUI(func(ui *UIConfig) { ui.ShowClock = true }); err != nil {
		t.Fatalf("SaveUI: %v", err)
	}

	var plugins map[string]json.RawMessage
	if err := json.Unmarshal(readRawConfig(t, path)["plugins"], &plugins); err != nil {
		t.Fatalf("plugins section: %v", err)
	}
	for _, key := range []string{"recall", "external-experiment"} {
		if _, ok := plugins[key]; !ok {
			t.Fatalf("Save dropped the unmanaged plugins.%s subsection; kept %v", key, sortedKeys(plugins))
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, plugins["recall"]); err != nil {
		t.Fatalf("compact plugins.recall: %v", err)
	}
	if got := compact.String(); got != `{"kept":["by","the","merge"]}` {
		t.Fatalf("plugins.recall after save = %s", got)
	}
	if _, ok := plugins["td-monitor"]; !ok {
		t.Fatal("Save stopped writing the managed td-monitor subsection")
	}
}

// The merge must not resurrect a managed subsection the user just emptied.
// plugins.external is the one that can empty: `sidecar plugin remove` on the
// last instance has to leave the key gone, not carried forward from the old
// file.
func TestSaveDropsAnEmptiedExternalPluginList(t *testing.T) {
	path := writeConfig(t, `{
	  "plugins": {
	    "external": [{"id": "recall", "command": ["sidecar-recall"]}],
	    "unmanaged": {"survives": true}
	  }
	}`)
	SetTestConfigPath(path)
	t.Cleanup(ResetTestConfigPath)

	if err := SavePlugins(func(p *PluginsConfig) { p.External = nil }); err != nil {
		t.Fatalf("SavePlugins: %v", err)
	}

	var plugins map[string]json.RawMessage
	if err := json.Unmarshal(readRawConfig(t, path)["plugins"], &plugins); err != nil {
		t.Fatalf("plugins section: %v", err)
	}
	if _, ok := plugins["external"]; ok {
		t.Fatalf("an emptied plugins.external was resurrected from the old file: %s", plugins["external"])
	}
	if _, ok := plugins["unmanaged"]; !ok {
		t.Fatal("the merge dropped an unmanaged subsection while deleting the emptied one")
	}
}

// terminalResources is a read-only alias: it is still read and still dispatched
// on the frozen resource identifier, and Save must leave the section exactly as
// the user wrote it — including the keys inside an entry that this build does
// not know about, which a re-serialized section would lose.
func TestSaveLeavesTerminalResourcesUntouched(t *testing.T) {
	body := `{
  "terminalResources": {
    "providers": [
      {
        "id": "jira-work",
        "command": [
          "sidecar-jira"
        ],
        "enabled": true,
        "timeout": "12s",
        "somethingNewerSidecarWrote": {
          "kept": true
        }
      }
    ]
  },
  "ui": {
    "showClock": false
  }
}`
	path := writeConfig(t, body)
	SetTestConfigPath(path)
	t.Cleanup(ResetTestConfigPath)

	before := readRawConfig(t, path)["terminalResources"]
	if err := SaveUI(func(ui *UIConfig) { ui.ShowClock = true }); err != nil {
		t.Fatalf("SaveUI: %v", err)
	}
	after := readRawConfig(t, path)["terminalResources"]
	if string(before) != string(after) {
		t.Fatalf("terminalResources changed across a save:\nbefore: %s\nafter:  %s", before, after)
	}

	// Still read, and still the legacy protocol's section.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	instance, ok := cfg.PluginInstance("jira-work")
	if !ok {
		t.Fatal("the preserved provider is no longer loaded")
	}
	if !instance.IsLegacyResourceProvider() {
		t.Fatalf("source after a save = %q", instance.Source)
	}
}

func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
