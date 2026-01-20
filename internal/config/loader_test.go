package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Projects.Mode != "single" {
		t.Errorf("got mode %q, want 'single'", cfg.Projects.Mode)
	}
	if !cfg.Plugins.GitStatus.Enabled {
		t.Error("git-status should be enabled by default")
	}
	if cfg.Plugins.GitStatus.RefreshInterval != time.Second {
		t.Errorf("got refresh %v, want 1s", cfg.Plugins.GitStatus.RefreshInterval)
	}
}

func TestLoadFrom_NonExistent(t *testing.T) {
	cfg, err := LoadFrom("/nonexistent/path/config.json")
	if err != nil {
		t.Errorf("should not error on missing file: %v", err)
	}
	if cfg == nil {
		t.Error("should return default config")
	}
}

func TestLoadFrom_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	content := []byte(`{
		"plugins": {
			"git-status": {
				"enabled": false,
				"refreshInterval": "5s"
			}
		},
		"ui": {
			"showFooter": false
		}
	}`)

	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}

	if cfg.Plugins.GitStatus.Enabled {
		t.Error("git-status should be disabled")
	}
	if cfg.Plugins.GitStatus.RefreshInterval != 5*time.Second {
		t.Errorf("got refresh %v, want 5s", cfg.Plugins.GitStatus.RefreshInterval)
	}
	if cfg.UI.ShowFooter {
		t.Error("showFooter should be false")
	}
	// Default values should still be present
	if !cfg.Plugins.TDMonitor.Enabled {
		t.Error("td-monitor should still be enabled (default)")
	}
}

func TestLoadFrom_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := os.WriteFile(path, []byte(`{invalid`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFrom(path)
	if err == nil {
		t.Error("should error on invalid JSON")
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		input  string
		expect string
	}{
		{"~/.claude", filepath.Join(home, ".claude")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tc := range tests {
		got := expandPath(tc.input)
		if got != tc.expect {
			t.Errorf("expandPath(%q) = %q, want %q", tc.input, got, tc.expect)
		}
	}
}

func TestValidate(t *testing.T) {
	cfg := Default()
	cfg.Plugins.GitStatus.RefreshInterval = -1

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate failed: %v", err)
	}

	// Negative values should be corrected
	if cfg.Plugins.GitStatus.RefreshInterval != time.Second {
		t.Errorf("got %v, want 1s after validation", cfg.Plugins.GitStatus.RefreshInterval)
	}
}

func TestLoadFrom_ProjectsList(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	// Create a test project directory
	testProjectDir := filepath.Join(dir, "myproject")
	if err := os.MkdirAll(testProjectDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := []byte(`{
		"projects": {
			"list": [
				{"name": "My Project", "path": "` + testProjectDir + `"},
				{"name": "Tilde Project", "path": "~/code/test"}
			]
		}
	}`)

	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}

	if len(cfg.Projects.List) != 2 {
		t.Errorf("got %d projects, want 2", len(cfg.Projects.List))
	}

	// Check first project
	if cfg.Projects.List[0].Name != "My Project" {
		t.Errorf("got name %q, want 'My Project'", cfg.Projects.List[0].Name)
	}
	if cfg.Projects.List[0].Path != testProjectDir {
		t.Errorf("got path %q, want %q", cfg.Projects.List[0].Path, testProjectDir)
	}

	// Check tilde expansion
	home, _ := os.UserHomeDir()
	expectedPath := filepath.Join(home, "code/test")
	if cfg.Projects.List[1].Path != expectedPath {
		t.Errorf("got path %q, want %q (tilde expanded)", cfg.Projects.List[1].Path, expectedPath)
	}
}

func TestLoadFrom_EmptyProjectsList(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	content := []byte(`{
		"projects": {
			"mode": "single"
		}
	}`)

	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}

	if len(cfg.Projects.List) != 0 {
		t.Errorf("got %d projects, want 0", len(cfg.Projects.List))
	}
}
