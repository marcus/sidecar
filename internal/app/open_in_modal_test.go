package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenInItemID(t *testing.T) {
	tests := []struct {
		idx  int
		want string
	}{
		{0, "open-in-item-0"},
		{1, "open-in-item-1"},
		{5, "open-in-item-5"},
		{42, "open-in-item-42"},
	}
	for _, tt := range tests {
		got := openInItemID(tt.idx)
		if got != tt.want {
			t.Errorf("openInItemID(%d) = %q, want %q", tt.idx, got, tt.want)
		}
	}
}

func TestDetectInstalledApps(t *testing.T) {
	// Create a temp directory to simulate /Applications
	tmpDir := t.TempDir()

	// Create some fake .app bundles (directories)
	for _, name := range []string{"Visual Studio Code.app", "GoLand.app", "Zed.app"} {
		err := os.MkdirAll(filepath.Join(tmpDir, name), 0755)
		if err != nil {
			t.Fatalf("failed to create fake bundle %s: %v", name, err)
		}
	}

	registry := []openInApp{
		{ID: "vscode", Name: "VS Code", AppBundles: []string{"Visual Studio Code.app"}},
		{ID: "cursor", Name: "Cursor", AppBundles: []string{"Cursor.app"}},
		{ID: "goland", Name: "GoLand", AppBundles: []string{"GoLand.app"}},
		{ID: "pycharm", Name: "PyCharm", AppBundles: []string{"PyCharm.app", "PyCharm CE.app"}},
		{ID: "zed", Name: "Zed", AppBundles: []string{"Zed.app"}},
		{ID: "finder", Name: "Finder", AlwaysAvailable: true},
	}

	installed := detectInstalledApps(registry, tmpDir)

	// Should find: vscode, goland, zed, finder
	wantIDs := map[string]bool{
		"vscode": true,
		"goland": true,
		"zed":    true,
		"finder": true,
	}

	if len(installed) != len(wantIDs) {
		t.Fatalf("expected %d installed apps, got %d: %v", len(wantIDs), len(installed), installed)
	}

	for _, app := range installed {
		if !wantIDs[app.ID] {
			t.Errorf("unexpected app in installed list: %s", app.ID)
		}
	}
}

func TestDetectInstalledApps_MultipleBundle(t *testing.T) {
	// Test that an app with multiple bundles is detected if any one exists
	tmpDir := t.TempDir()

	// Create only the CE variant
	err := os.MkdirAll(filepath.Join(tmpDir, "PyCharm CE.app"), 0755)
	if err != nil {
		t.Fatalf("failed to create fake bundle: %v", err)
	}

	registry := []openInApp{
		{ID: "pycharm", Name: "PyCharm", AppBundles: []string{"PyCharm.app", "PyCharm CE.app"}},
	}

	installed := detectInstalledApps(registry, tmpDir)

	if len(installed) != 1 {
		t.Fatalf("expected 1 installed app, got %d", len(installed))
	}
	if installed[0].ID != "pycharm" {
		t.Errorf("expected pycharm, got %s", installed[0].ID)
	}
}

func TestDetectInstalledApps_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	registry := []openInApp{
		{ID: "vscode", Name: "VS Code", AppBundles: []string{"Visual Studio Code.app"}},
		{ID: "finder", Name: "Finder", AlwaysAvailable: true},
	}

	installed := detectInstalledApps(registry, tmpDir)

	// Should only find: finder (always available)
	if len(installed) != 1 {
		t.Fatalf("expected 1 installed app, got %d", len(installed))
	}
	if installed[0].ID != "finder" {
		t.Errorf("expected finder, got %s", installed[0].ID)
	}
}

func TestFindLastUsedIndex(t *testing.T) {
	apps := []openInApp{
		{ID: "vscode", Name: "VS Code"},
		{ID: "goland", Name: "GoLand"},
		{ID: "finder", Name: "Finder"},
	}

	tests := []struct {
		lastID string
		want   int
	}{
		{"vscode", 0},
		{"goland", 1},
		{"finder", 2},
		{"unknown", 0},  // fallback to 0
		{"", 0},          // empty string fallback
	}

	for _, tt := range tests {
		got := findLastUsedIndex(apps, tt.lastID)
		if got != tt.want {
			t.Errorf("findLastUsedIndex(apps, %q) = %d, want %d", tt.lastID, got, tt.want)
		}
	}
}

func TestFindLastUsedIndex_EmptyList(t *testing.T) {
	var apps []openInApp
	got := findLastUsedIndex(apps, "vscode")
	if got != 0 {
		t.Errorf("findLastUsedIndex(nil, %q) = %d, want 0", "vscode", got)
	}
}

func TestOpenInEnsureCursorVisible(t *testing.T) {
	tests := []struct {
		name       string
		cursor     int
		scroll     int
		maxVisible int
		want       int
	}{
		{"cursor in view", 3, 0, 10, 0},
		{"cursor at top of view", 0, 0, 10, 0},
		{"cursor at bottom of view", 9, 0, 10, 0},
		{"cursor above scroll", 2, 5, 10, 2},
		{"cursor below scroll", 15, 0, 10, 6},
		{"cursor just below view", 10, 0, 10, 1},
		{"cursor at scroll start", 5, 5, 10, 5},
		{"scroll to beginning", 0, 5, 10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := openInEnsureCursorVisible(tt.cursor, tt.scroll, tt.maxVisible)
			if got != tt.want {
				t.Errorf("openInEnsureCursorVisible(%d, %d, %d) = %d, want %d",
					tt.cursor, tt.scroll, tt.maxVisible, got, tt.want)
			}
		})
	}
}
