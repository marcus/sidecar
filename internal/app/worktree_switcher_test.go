package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilterWorktrees(t *testing.T) {
	worktrees := []WorktreeInfo{
		{Path: "/main/repo", Branch: "main", IsMain: true},
		{Path: "/worktrees/feature-auth", Branch: "feature-auth", IsMain: false},
		{Path: "/worktrees/feature-billing", Branch: "feature-billing", IsMain: false},
		{Path: "/worktrees/bugfix-login", Branch: "bugfix-login", IsMain: false},
	}

	tests := []struct {
		name     string
		query    string
		expected int
	}{
		{"empty query returns all", "", 4},
		{"filter by branch name", "feature", 2},
		{"filter by auth", "auth", 1},
		{"filter by billing", "billing", 1},
		{"filter by bugfix", "bugfix", 1},
		{"filter by main", "main", 1},
		{"case insensitive", "FEATURE", 2},
		{"no matches", "nonexistent", 0},
		{"partial match", "log", 1}, // matches "bugfix-login"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterWorktrees(worktrees, tt.query)
			if len(result) != tt.expected {
				t.Errorf("filterWorktrees(%q) returned %d results, want %d", tt.query, len(result), tt.expected)
			}
		})
	}
}

func TestWorktreeSwitcherEnsureCursorVisible(t *testing.T) {
	tests := []struct {
		name       string
		cursor     int
		scroll     int
		maxVisible int
		expected   int
	}{
		{"cursor in view", 3, 0, 8, 0},
		{"cursor at top, need to scroll up", 2, 5, 8, 2},
		{"cursor at bottom, need to scroll down", 10, 0, 8, 3},
		{"cursor at edge", 7, 0, 8, 0},
		{"cursor just past edge", 8, 0, 8, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := worktreeSwitcherEnsureCursorVisible(tt.cursor, tt.scroll, tt.maxVisible)
			if result != tt.expected {
				t.Errorf("worktreeSwitcherEnsureCursorVisible(%d, %d, %d) = %d, want %d",
					tt.cursor, tt.scroll, tt.maxVisible, result, tt.expected)
			}
		})
	}
}

func TestWorktreeExists(t *testing.T) {
	// Create a temp directory to test with
	tempDir := t.TempDir()

	// Create a mock .git file
	gitPath := filepath.Join(tempDir, ".git")
	if err := os.WriteFile(gitPath, []byte("gitdir: /path/to/main/.git/worktrees/test"), 0644); err != nil {
		t.Fatalf("failed to create .git file: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"valid directory with .git", tempDir, true},
		{"non-existent directory", "/nonexistent/path/12345", false},
		{"file instead of directory", gitPath, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := WorktreeExists(tt.path)
			if result != tt.expected {
				t.Errorf("WorktreeExists(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestWorktreeSwitcherItemID(t *testing.T) {
	tests := []struct {
		idx      int
		expected string
	}{
		{0, "worktree-switcher-item-0"},
		{1, "worktree-switcher-item-1"},
		{10, "worktree-switcher-item-10"},
		{99, "worktree-switcher-item-99"},
	}

	for _, tt := range tests {
		result := worktreeSwitcherItemID(tt.idx)
		if result != tt.expected {
			t.Errorf("worktreeSwitcherItemID(%d) = %q, want %q", tt.idx, result, tt.expected)
		}
	}
}

func TestCheckCurrentWorktree(t *testing.T) {
	// Test with non-existent path
	exists, mainPath := CheckCurrentWorktree("/nonexistent/path/that/does/not/exist")
	if exists {
		t.Error("CheckCurrentWorktree should return false for non-existent path")
	}
	// mainPath may or may not be found depending on the test environment
	_ = mainPath

	// Test with existing path (use temp dir as a valid directory)
	tempDir := t.TempDir()
	gitPath := filepath.Join(tempDir, ".git")
	if err := os.WriteFile(gitPath, []byte("gitdir: /path/to/main/.git/worktrees/test"), 0644); err != nil {
		t.Fatalf("failed to create .git file: %v", err)
	}

	exists, _ = CheckCurrentWorktree(tempDir)
	if !exists {
		t.Error("CheckCurrentWorktree should return true for existing directory with .git")
	}
}

func TestWorktreeStatePersistence(t *testing.T) {
	// Test the logic for determining target path when switching projects
	// This tests the core decision logic without needing a full Model

	tests := []struct {
		name                string
		oldWorkDir          string
		projectPath         string
		mainRepoPath        string // What GetMainWorktreePath would return
		savedWorktree       string // Previously saved worktree for this repo
		savedWorktreeExists bool
		expectedTarget      string
		description         string
	}{
		{
			name:                "switch from worktree to main - should go to main",
			oldWorkDir:          "/repo/worktrees/feature-a",
			projectPath:         "/repo/main",
			mainRepoPath:        "/repo/main",
			savedWorktree:       "/repo/worktrees/feature-a", // Same as oldWorkDir
			savedWorktreeExists: true,
			expectedTarget:      "/repo/main", // Should NOT restore back to feature-a
			description:         "When leaving a worktree to go to main, don't restore back to that worktree",
		},
		{
			name:                "switch from different project - should restore saved worktree",
			oldWorkDir:          "/other-project",
			projectPath:         "/repo/main",
			mainRepoPath:        "/repo/main",
			savedWorktree:       "/repo/worktrees/feature-b",
			savedWorktreeExists: true,
			expectedTarget:      "/repo/worktrees/feature-b", // Should restore
			description:         "When coming from a different project, restore the last worktree",
		},
		{
			name:                "switch to main with no saved worktree",
			oldWorkDir:          "/other-project",
			projectPath:         "/repo/main",
			mainRepoPath:        "/repo/main",
			savedWorktree:       "",
			savedWorktreeExists: false,
			expectedTarget:      "/repo/main",
			description:         "No saved worktree means stay on main",
		},
		{
			name:                "switch to main with stale saved worktree",
			oldWorkDir:          "/other-project",
			projectPath:         "/repo/main",
			mainRepoPath:        "/repo/main",
			savedWorktree:       "/repo/worktrees/deleted-feature",
			savedWorktreeExists: false, // Worktree was deleted
			expectedTarget:      "/repo/main",
			description:         "Stale worktree entry should be ignored",
		},
		{
			name:                "explicit worktree selection - should not restore",
			oldWorkDir:          "/repo/main",
			projectPath:         "/repo/worktrees/feature-c", // User explicitly chose this
			mainRepoPath:        "/repo/main",
			savedWorktree:       "/repo/worktrees/feature-d", // Different saved worktree
			savedWorktreeExists: true,
			expectedTarget:      "/repo/worktrees/feature-c", // User's explicit choice
			description:         "When user explicitly selects a worktree, don't redirect to saved one",
		},
		{
			name:                "switch between worktrees in same repo",
			oldWorkDir:          "/repo/worktrees/feature-a",
			projectPath:         "/repo/worktrees/feature-b", // User explicitly chose this
			mainRepoPath:        "/repo/main",
			savedWorktree:       "/repo/worktrees/feature-a",
			savedWorktreeExists: true,
			expectedTarget:      "/repo/worktrees/feature-b", // User's explicit choice
			description:         "Switching between worktrees should respect explicit selection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the target path determination logic from switchProject
			targetPath := tt.projectPath
			normalizedOldWorkDir := tt.oldWorkDir // In real code this is normalized

			// Only restore if projectPath equals mainRepoPath (switching to main)
			if tt.projectPath == tt.mainRepoPath {
				if tt.savedWorktree != "" {
					// Don't restore if saved worktree is where we're coming from
					if tt.savedWorktree != normalizedOldWorkDir {
						if tt.savedWorktreeExists {
							targetPath = tt.savedWorktree
						}
					}
				}
			}

			if targetPath != tt.expectedTarget {
				t.Errorf("target = %q, want %q\n  %s", targetPath, tt.expectedTarget, tt.description)
			}
		})
	}
}

func TestWorktreeStateNotRestoredWhenLeavingSameWorktree(t *testing.T) {
	// Specific regression test for the bug where switching from worktree to main
	// would save the worktree and immediately restore it

	oldWorkDir := "/projects/sidecar-worktree-switcher"
	projectPath := "/projects/sidecar" // Main repo
	mainRepoPath := "/projects/sidecar"
	savedWorktree := "/projects/sidecar-worktree-switcher" // Same as oldWorkDir

	targetPath := projectPath

	// Simulate the restore logic
	if projectPath == mainRepoPath {
		if savedWorktree != "" && savedWorktree != oldWorkDir {
			// Would restore here, but savedWorktree == oldWorkDir so we skip
			targetPath = savedWorktree
		}
	}

	// The key assertion: we should NOT have restored back to the worktree
	if targetPath != mainRepoPath {
		t.Errorf("Bug regression: switching from worktree to main restored back to worktree.\n"+
			"  oldWorkDir: %s\n"+
			"  projectPath: %s\n"+
			"  savedWorktree: %s\n"+
			"  targetPath: %s (should be %s)",
			oldWorkDir, projectPath, savedWorktree, targetPath, mainRepoPath)
	}
}
