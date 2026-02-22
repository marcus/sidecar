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
