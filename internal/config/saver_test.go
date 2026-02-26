package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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
