package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/projectdir"
)

func TestExtractFallback(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "simple fallback",
			body:     "Review {{ticket || 'open reviews'}} and fix issues",
			expected: "open reviews",
		},
		{
			name:     "no fallback",
			body:     "Work on {{ticket}} and submit",
			expected: "",
		},
		{
			name:     "empty fallback",
			body:     "Process {{ticket || ''}} now",
			expected: "",
		},
		{
			name:     "no ticket placeholder",
			body:     "Just some text without placeholders",
			expected: "",
		},
		{
			name:     "fallback with spaces",
			body:     "Start {{ticket || 'all pending tasks'}} immediately",
			expected: "all pending tasks",
		},
		{
			name:     "multiple placeholders takes first",
			body:     "Review {{ticket || 'first'}} then {{ticket || 'second'}}",
			expected: "first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractFallback(tt.body)
			if result != tt.expected {
				t.Errorf("ExtractFallback(%q) = %q, want %q", tt.body, result, tt.expected)
			}
		})
	}
}

func TestExpandPromptTemplate(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		taskID   string
		expected string
	}{
		{
			name:     "simple replacement",
			body:     "Work on {{ticket}} now",
			taskID:   "td-abc123",
			expected: "Work on td-abc123 now",
		},
		{
			name:     "empty taskID simple",
			body:     "Work on {{ticket}} now",
			taskID:   "",
			expected: "Work on  now",
		},
		{
			name:     "with fallback and taskID",
			body:     "Review {{ticket || 'open reviews'}}",
			taskID:   "td-xyz789",
			expected: "Review td-xyz789",
		},
		{
			name:     "with fallback no taskID",
			body:     "Review {{ticket || 'open reviews'}}",
			taskID:   "",
			expected: "Review open reviews",
		},
		{
			name:     "multiple replacements",
			body:     "Start {{ticket}} and finish {{ticket}}",
			taskID:   "td-123",
			expected: "Start td-123 and finish td-123",
		},
		{
			name:     "no placeholders",
			body:     "Plain text without any placeholders",
			taskID:   "td-abc",
			expected: "Plain text without any placeholders",
		},
		{
			name:     "mixed placeholders",
			body:     "Work on {{ticket}} or {{ticket || 'fallback'}}",
			taskID:   "",
			expected: "Work on  or fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandPromptTemplate(tt.body, tt.taskID)
			if result != tt.expected {
				t.Errorf("ExpandPromptTemplate(%q, %q) = %q, want %q",
					tt.body, tt.taskID, result, tt.expected)
			}
		})
	}
}

func TestHasTicketPlaceholder(t *testing.T) {
	tests := []struct {
		body     string
		expected bool
	}{
		{"Work on {{ticket}} now", true},
		{"Review {{ticket || 'default'}}", true},
		{"No placeholder here", false},
		{"{{tickets}} contains ticket prefix", true}, // Note: simple contains check
		{"ticket without braces", false},
	}

	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			result := HasTicketPlaceholder(tt.body)
			if result != tt.expected {
				t.Errorf("HasTicketPlaceholder(%q) = %v, want %v", tt.body, result, tt.expected)
			}
		})
	}
}

func TestLoadPrompts(t *testing.T) {
	// Create temp directories for global and project configs
	globalDir := t.TempDir()
	projectRoot := t.TempDir()

	// Set up config path so projectdir resolves to our temp dir
	configBase := t.TempDir()
	config.SetTestConfigPath(filepath.Join(configBase, "config.json"))
	t.Cleanup(config.ResetTestConfigPath)

	// Create global config with prompts
	globalConfig := `{
  "prompts": [
    {
      "name": "global-prompt",
      "ticketMode": "required",
      "body": "Global prompt body with {{ticket}}"
    },
    {
      "name": "shared-prompt",
      "ticketMode": "optional",
      "body": "Global shared body"
    }
  ]
}`
	err := os.WriteFile(filepath.Join(globalDir, "config.json"), []byte(globalConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write global config: %v", err)
	}

	// Resolve project dir so we know where to write project config
	projDir, err := projectdir.Resolve(projectRoot)
	if err != nil {
		t.Fatalf("Failed to resolve project dir: %v", err)
	}
	projectConfig := `{
  "prompts": [
    {
      "name": "project-prompt",
      "ticketMode": "none",
      "body": "Project-only prompt"
    },
    {
      "name": "shared-prompt",
      "ticketMode": "required",
      "body": "Project override body"
    }
  ]
}`
	err = os.WriteFile(filepath.Join(projDir, "config.json"), []byte(projectConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write project config: %v", err)
	}

	// Load prompts
	prompts := LoadPrompts(globalDir, projectRoot)

	// Verify count (3 unique prompts: global-prompt, project-prompt, shared-prompt)
	if len(prompts) != 3 {
		t.Errorf("Expected 3 prompts, got %d", len(prompts))
	}

	// Find and verify each prompt
	promptMap := make(map[string]Prompt)
	for _, p := range prompts {
		promptMap[p.Name] = p
	}

	// Check global-only prompt
	if gp, ok := promptMap["global-prompt"]; !ok {
		t.Error("Missing global-prompt")
	} else {
		if gp.Source != "global" {
			t.Errorf("global-prompt Source = %q, want 'global'", gp.Source)
		}
		if gp.TicketMode != TicketRequired {
			t.Errorf("global-prompt TicketMode = %q, want 'required'", gp.TicketMode)
		}
	}

	// Check project-only prompt
	if pp, ok := promptMap["project-prompt"]; !ok {
		t.Error("Missing project-prompt")
	} else {
		if pp.Source != "project" {
			t.Errorf("project-prompt Source = %q, want 'project'", pp.Source)
		}
		if pp.TicketMode != TicketNone {
			t.Errorf("project-prompt TicketMode = %q, want 'none'", pp.TicketMode)
		}
	}

	// Check overridden prompt (project should override global)
	if sp, ok := promptMap["shared-prompt"]; !ok {
		t.Error("Missing shared-prompt")
	} else {
		if sp.Source != "project" {
			t.Errorf("shared-prompt Source = %q, want 'project' (should be overridden)", sp.Source)
		}
		if sp.TicketMode != TicketRequired {
			t.Errorf("shared-prompt TicketMode = %q, want 'required'", sp.TicketMode)
		}
		if sp.Body != "Project override body" {
			t.Errorf("shared-prompt Body = %q, want 'Project override body'", sp.Body)
		}
	}
}

func TestLoadPromptsJSON(t *testing.T) {
	// Test JSON config loading
	globalDir := t.TempDir()

	jsonConfig := `{
  "prompts": [
    {
      "name": "json-prompt",
      "ticketMode": "optional",
      "body": "JSON body with {{ticket || 'default'}}"
    }
  ]
}`
	err := os.WriteFile(filepath.Join(globalDir, "config.json"), []byte(jsonConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write JSON config: %v", err)
	}

	prompts := LoadPrompts(globalDir, t.TempDir())

	if len(prompts) != 1 {
		t.Fatalf("Expected 1 prompt, got %d", len(prompts))
	}

	if prompts[0].Name != "json-prompt" {
		t.Errorf("Name = %q, want 'json-prompt'", prompts[0].Name)
	}
	if prompts[0].TicketMode != TicketOptional {
		t.Errorf("TicketMode = %q, want 'optional'", prompts[0].TicketMode)
	}
}

func TestLoadPromptsDefaultTicketMode(t *testing.T) {
	// Test that ticketMode defaults to optional when not specified
	globalDir := t.TempDir()

	config := `{
  "prompts": [
    {
      "name": "no-mode-prompt",
      "body": "Body without ticketMode"
    }
  ]
}`
	err := os.WriteFile(filepath.Join(globalDir, "config.json"), []byte(config), 0644)
	if err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	prompts := LoadPrompts(globalDir, t.TempDir())

	if len(prompts) != 1 {
		t.Fatalf("Expected 1 prompt, got %d", len(prompts))
	}

	if prompts[0].TicketMode != TicketOptional {
		t.Errorf("TicketMode = %q, want 'optional' (default)", prompts[0].TicketMode)
	}
}

func TestLoadPromptsEmptyDirsCreatesDefaults(t *testing.T) {
	// Test that empty dirs trigger default prompt creation
	globalDir := t.TempDir()
	projectDir := t.TempDir()

	prompts := LoadPrompts(globalDir, projectDir)

	// Should create 5 default prompts
	if len(prompts) != 5 {
		t.Errorf("Expected 5 default prompts, got %d", len(prompts))
	}

	// Verify config file was created
	configPath := filepath.Join(globalDir, "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Expected config.json to be created in global dir")
	}

	// Verify prompts have correct source
	for _, p := range prompts {
		if p.Source != "global" {
			t.Errorf("Default prompt %q Source = %q, want 'global'", p.Name, p.Source)
		}
	}
}

func TestDefaultPrompts(t *testing.T) {
	prompts := DefaultPrompts()

	if len(prompts) != 5 {
		t.Fatalf("Expected 5 default prompts, got %d", len(prompts))
	}

	// Verify expected prompt names exist
	expectedNames := map[string]bool{
		"Begin Work on Ticket":    false,
		"Code Review Ticket":      false,
		"Plan to Epic (No Impl)":  false,
		"Plan to Epic + Implement": false,
		"TD Review Session":       false,
	}

	for _, p := range prompts {
		if _, ok := expectedNames[p.Name]; ok {
			expectedNames[p.Name] = true
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("Missing default prompt: %q", name)
		}
	}

	// Verify ticketMode settings
	for _, p := range prompts {
		switch p.Name {
		case "Begin Work on Ticket", "Code Review Ticket":
			if p.TicketMode != TicketRequired {
				t.Errorf("Prompt %q TicketMode = %q, want 'required'", p.Name, p.TicketMode)
			}
		default:
			if p.TicketMode != TicketNone {
				t.Errorf("Prompt %q TicketMode = %q, want 'none'", p.Name, p.TicketMode)
			}
		}
	}
}

func TestEnsureDefaultPromptsDoesNotOverwrite(t *testing.T) {
	// Test that existing config is not overwritten
	globalDir := t.TempDir()

	// Create existing config
	existingConfig := `{
  "prompts": [
    {
      "name": "existing-prompt",
      "body": "Existing body"
    }
  ]
}`
	err := os.WriteFile(filepath.Join(globalDir, "config.json"), []byte(existingConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write existing config: %v", err)
	}

	// EnsureDefaultPrompts should return false (no changes)
	if EnsureDefaultPrompts(globalDir) {
		t.Error("EnsureDefaultPrompts should not overwrite existing config")
	}

	// Load and verify existing config is preserved
	prompts := LoadPrompts(globalDir, t.TempDir())
	if len(prompts) != 1 {
		t.Errorf("Expected 1 existing prompt, got %d", len(prompts))
	}
	if prompts[0].Name != "existing-prompt" {
		t.Errorf("Prompt name = %q, want 'existing-prompt'", prompts[0].Name)
	}
}

func TestWriteDefaultPromptsToConfig_NewFile(t *testing.T) {
	dir := t.TempDir()

	if !WriteDefaultPromptsToConfig(dir) {
		t.Fatal("WriteDefaultPromptsToConfig returned false")
	}

	prompts := LoadPrompts(dir, t.TempDir())
	if len(prompts) != 5 {
		t.Errorf("Expected 5 prompts, got %d", len(prompts))
	}
}

func TestWriteDefaultPromptsToConfig_MergesExisting(t *testing.T) {
	dir := t.TempDir()

	// Write existing config with other fields but no prompts
	existing := `{"plugins": {"git-status": {"enabled": true}}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if !WriteDefaultPromptsToConfig(dir) {
		t.Fatal("WriteDefaultPromptsToConfig returned false")
	}

	// Verify prompts were added
	prompts := LoadPrompts(dir, t.TempDir())
	if len(prompts) != 5 {
		t.Errorf("Expected 5 prompts, got %d", len(prompts))
	}

	// Verify other fields preserved
	data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if !strings.Contains(string(data), "plugins") {
		t.Error("Expected plugins field to be preserved")
	}
}

func TestWriteDefaultPromptsToConfig_OverwritesPrompts(t *testing.T) {
	dir := t.TempDir()

	// Write config with a single custom prompt
	existing := `{"prompts": [{"name": "Custom", "body": "custom body"}]}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if !WriteDefaultPromptsToConfig(dir) {
		t.Fatal("WriteDefaultPromptsToConfig returned false")
	}

	// Should now have 5 default prompts (custom was replaced)
	prompts := LoadPrompts(dir, t.TempDir())
	if len(prompts) != 5 {
		t.Errorf("Expected 5 prompts, got %d", len(prompts))
	}
}

func TestWriteDefaultPromptsToConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()

	// Write invalid JSON to config
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("not valid json{{{"), 0644); err != nil {
		t.Fatal(err)
	}

	if !WriteDefaultPromptsToConfig(dir) {
		t.Fatal("WriteDefaultPromptsToConfig returned false for invalid JSON")
	}

	// Should recover and write valid config with defaults
	prompts := LoadPrompts(dir, t.TempDir())
	if len(prompts) != 5 {
		t.Errorf("Expected 5 prompts after invalid JSON recovery, got %d", len(prompts))
	}
}

func TestWriteDefaultPromptsToConfig_CreatesDirectory(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "config", "dir")

	if !WriteDefaultPromptsToConfig(dir) {
		t.Fatal("WriteDefaultPromptsToConfig returned false for nested dir")
	}

	prompts := LoadPrompts(dir, t.TempDir())
	if len(prompts) != 5 {
		t.Errorf("Expected 5 prompts, got %d", len(prompts))
	}
}
