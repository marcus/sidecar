package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestInit(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current

	// Use InitWithDir to avoid reading real user state
	err := InitWithDir(filepath.Join(tmpDir, ".config", "sidecar"))
	if err != nil {
		t.Fatalf("InitWithDir() failed: %v", err)
	}

	if current == nil {
		t.Error("current state should be initialized")
	}
	if current.GitDiffMode != "unified" {
		t.Errorf("default GitDiffMode = %q, want unified", current.GitDiffMode)
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestLoad_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current

	path = filepath.Join(tmpDir, "nonexistent", "state.json")

	err := Load()
	if err != nil {
		t.Fatalf("Load() for non-existent file should return nil, got %v", err)
	}

	if current == nil {
		t.Error("current should be initialized with defaults")
	}
	if current.GitDiffMode != "unified" {
		t.Errorf("default GitDiffMode = %q, want unified", current.GitDiffMode)
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestLoad_ExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current

	stateFile := filepath.Join(tmpDir, "state.json")
	path = stateFile

	// Create a state file
	testState := State{GitDiffMode: "side-by-side"}
	data, _ := json.Marshal(testState)
	if err := os.WriteFile(stateFile, data, 0644); err != nil {
		t.Fatalf("failed to write test state file: %v", err)
	}

	err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if current.GitDiffMode != "side-by-side" {
		t.Errorf("GitDiffMode = %q, want side-by-side", current.GitDiffMode)
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestLoad_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current

	stateFile := filepath.Join(tmpDir, "state.json")
	path = stateFile

	// Create invalid JSON file
	if err := os.WriteFile(stateFile, []byte("invalid json"), 0644); err != nil {
		t.Fatalf("failed to write invalid JSON: %v", err)
	}

	err := Load()
	if err == nil {
		t.Error("Load() should return error for invalid JSON")
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestSave(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current

	stateFile := filepath.Join(tmpDir, "config", "sidecar", "state.json")
	path = stateFile

	current = &State{GitDiffMode: "side-by-side"}

	err := Save()
	if err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	// Verify contents
	data, _ := os.ReadFile(stateFile)
	var loaded State
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal saved state: %v", err)
	}

	if loaded.GitDiffMode != "side-by-side" {
		t.Errorf("saved GitDiffMode = %q, want side-by-side", loaded.GitDiffMode)
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestSave_CreateDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current

	stateFile := filepath.Join(tmpDir, "deep", "nested", "config", "sidecar", "state.json")
	path = stateFile

	current = &State{GitDiffMode: "unified"}

	err := Save()
	if err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestSave_NilCurrent(t *testing.T) {
	originalPath := path
	originalCurrent := current

	current = nil
	path = "/tmp/nonexistent/state.json"

	// Should not error when current is nil
	err := Save()
	if err != nil {
		t.Fatalf("Save() with nil current should not error, got %v", err)
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestGetGitDiffMode_Default(t *testing.T) {
	originalCurrent := current

	current = nil
	mode := GetGitDiffMode()
	if mode != "unified" {
		t.Errorf("GetGitDiffMode() with nil current = %q, want unified", mode)
	}

	// Cleanup
	current = originalCurrent
}

func TestGetGitDiffMode_Set(t *testing.T) {
	originalCurrent := current

	current = &State{GitDiffMode: "side-by-side"}
	mode := GetGitDiffMode()
	if mode != "side-by-side" {
		t.Errorf("GetGitDiffMode() = %q, want side-by-side", mode)
	}

	// Cleanup
	current = originalCurrent
}

func TestSetGitDiffMode(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current

	stateFile := filepath.Join(tmpDir, "state.json")
	path = stateFile

	current = &State{GitDiffMode: "unified"}

	err := SetGitDiffMode("side-by-side")
	if err != nil {
		t.Fatalf("SetGitDiffMode() failed: %v", err)
	}

	// Verify in-memory value
	if current.GitDiffMode != "side-by-side" {
		t.Errorf("current.GitDiffMode = %q, want side-by-side", current.GitDiffMode)
	}

	// Verify saved to disk
	data, _ := os.ReadFile(stateFile)
	var loaded State
	_ = json.Unmarshal(data, &loaded)
	if loaded.GitDiffMode != "side-by-side" {
		t.Errorf("saved GitDiffMode = %q, want side-by-side", loaded.GitDiffMode)
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestSetGitDiffMode_InitializesNilState(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current

	stateFile := filepath.Join(tmpDir, "state.json")
	path = stateFile

	current = nil

	err := SetGitDiffMode("side-by-side")
	if err != nil {
		t.Fatalf("SetGitDiffMode() failed: %v", err)
	}

	if current == nil {
		t.Error("SetGitDiffMode() should initialize current state")
	}
	if current.GitDiffMode != "side-by-side" {
		t.Errorf("GitDiffMode = %q, want side-by-side", current.GitDiffMode)
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestGetGitGraphEnabled_Default(t *testing.T) {
	current = nil
	enabled := GetGitGraphEnabled()
	if enabled {
		t.Errorf("GetGitGraphEnabled() with nil current = %v, want false", enabled)
	}
}

func TestGetGitGraphEnabled_Set(t *testing.T) {
	current = &State{GitGraphEnabled: true}
	enabled := GetGitGraphEnabled()
	if !enabled {
		t.Errorf("GetGitGraphEnabled() = %v, want true", enabled)
	}
}

func TestSetGitGraphEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current

	stateFile := filepath.Join(tmpDir, "state.json")
	path = stateFile
	current = &State{GitGraphEnabled: false}

	err := SetGitGraphEnabled(true)
	if err != nil {
		t.Fatalf("SetGitGraphEnabled() failed: %v", err)
	}

	if !current.GitGraphEnabled {
		t.Errorf("current.GitGraphEnabled = %v, want true", current.GitGraphEnabled)
	}

	// Verify saved to disk
	data, _ := os.ReadFile(stateFile)
	var loaded State
	_ = json.Unmarshal(data, &loaded)
	if !loaded.GitGraphEnabled {
		t.Errorf("saved GitGraphEnabled = %v, want true", loaded.GitGraphEnabled)
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current

	stateFile := filepath.Join(tmpDir, "state.json")
	path = stateFile

	current = &State{GitDiffMode: "unified"}

	// Run concurrent reads and writes
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			mode := "unified"
			if n%2 == 0 {
				mode = "side-by-side"
			}
			if err := SetGitDiffMode(mode); err != nil {
				errors <- err
			}
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = GetGitDiffMode()
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent access error: %v", err)
		}
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current

	stateFile := filepath.Join(tmpDir, "state.json")
	path = stateFile

	// Set and save
	current = &State{GitDiffMode: "side-by-side"}
	if err := Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Load into fresh state
	current = nil
	if err := Load(); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if current.GitDiffMode != "side-by-side" {
		t.Errorf("round-trip GitDiffMode = %q, want side-by-side", current.GitDiffMode)
	}

	// Cleanup
	path = originalPath
	current = originalCurrent
}

func TestGetWorkspaceState_Default(t *testing.T) {
	originalCurrent := current
	defer func() { current = originalCurrent }()

	current = nil
	state := GetWorkspaceState("/path/to/project")
	if state.WorkspaceName != "" || state.ShellTmuxName != "" || len(state.ShellDisplayNames) > 0 {
		t.Errorf("GetWorkspaceState() with nil current should return empty state")
	}
}

func TestGetWorkspaceState_EmptyMap(t *testing.T) {
	originalCurrent := current
	defer func() { current = originalCurrent }()

	current = &State{Workspace: nil}
	state := GetWorkspaceState("/path/to/project")
	if state.WorkspaceName != "" || state.ShellTmuxName != "" || len(state.ShellDisplayNames) > 0 {
		t.Errorf("GetWorkspaceState() with nil map should return empty state")
	}
}

func TestGetWorkspaceState_Found(t *testing.T) {
	originalCurrent := current
	defer func() { current = originalCurrent }()

	current = &State{
		Workspace: map[string]WorkspaceState{
			"/path/to/project": {
				WorkspaceName: "feature-branch",
				ShellTmuxName: "sidecar-sh-project-1",
				ShellDisplayNames: map[string]string{
					"sidecar-sh-project-1": "Backend",
				},
			},
		},
	}
	state := GetWorkspaceState("/path/to/project")
	if state.WorkspaceName != "feature-branch" {
		t.Errorf("WorkspaceName = %q, want feature-branch", state.WorkspaceName)
	}
	if state.ShellTmuxName != "sidecar-sh-project-1" {
		t.Errorf("ShellTmuxName = %q, want sidecar-sh-project-1", state.ShellTmuxName)
	}
	if state.ShellDisplayNames["sidecar-sh-project-1"] != "Backend" {
		t.Errorf("ShellDisplayNames[sidecar-sh-project-1] = %q, want Backend", state.ShellDisplayNames["sidecar-sh-project-1"])
	}
}

func TestSetWorkspaceState(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current
	defer func() {
		path = originalPath
		current = originalCurrent
	}()

	stateFile := filepath.Join(tmpDir, "state.json")
	path = stateFile
	current = &State{}

	wtState := WorkspaceState{
		WorkspaceName: "my-workspace",
		ShellTmuxName: "",
		ShellDisplayNames: map[string]string{
			"sidecar-sh-project-1": "Backend",
		},
	}

	err := SetWorkspaceState("/projects/sidecar", wtState)
	if err != nil {
		t.Fatalf("SetWorkspaceState() failed: %v", err)
	}

	// Verify in memory
	stored := current.Workspace["/projects/sidecar"]
	if stored.WorkspaceName != "my-workspace" {
		t.Errorf("stored WorkspaceName = %q, want my-workspace", stored.WorkspaceName)
	}
	if stored.ShellDisplayNames["sidecar-sh-project-1"] != "Backend" {
		t.Errorf("stored ShellDisplayNames[sidecar-sh-project-1] = %q, want Backend", stored.ShellDisplayNames["sidecar-sh-project-1"])
	}

	// Verify saved to disk
	data, _ := os.ReadFile(stateFile)
	var loaded State
	_ = json.Unmarshal(data, &loaded)
	if loaded.Workspace["/projects/sidecar"].WorkspaceName != "my-workspace" {
		t.Errorf("persisted WorkspaceName = %q, want my-workspace", loaded.Workspace["/projects/sidecar"].WorkspaceName)
	}
	if loaded.Workspace["/projects/sidecar"].ShellDisplayNames["sidecar-sh-project-1"] != "Backend" {
		t.Errorf("persisted ShellDisplayNames[sidecar-sh-project-1] = %q, want Backend", loaded.Workspace["/projects/sidecar"].ShellDisplayNames["sidecar-sh-project-1"])
	}
}

func TestSetWorkspaceState_ShellSelection(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current
	defer func() {
		path = originalPath
		current = originalCurrent
	}()

	path = filepath.Join(tmpDir, "state.json")
	current = &State{}

	// Save shell selection
	wtState := WorkspaceState{
		WorkspaceName: "",
		ShellTmuxName: "sidecar-sh-project-2",
	}

	err := SetWorkspaceState("/projects/myapp", wtState)
	if err != nil {
		t.Fatalf("SetWorkspaceState() failed: %v", err)
	}

	// Verify
	stored := current.Workspace["/projects/myapp"]
	if stored.ShellTmuxName != "sidecar-sh-project-2" {
		t.Errorf("stored ShellTmuxName = %q, want sidecar-sh-project-2", stored.ShellTmuxName)
	}
	if stored.WorkspaceName != "" {
		t.Errorf("stored WorkspaceName = %q, want empty", stored.WorkspaceName)
	}
}

func TestGetLastWorktreePath_Default(t *testing.T) {
	originalCurrent := current
	defer func() { current = originalCurrent }()

	current = nil
	result := GetLastWorktreePath("/main/repo")
	if result != "" {
		t.Errorf("GetLastWorktreePath() with nil current = %q, want empty", result)
	}
}

func TestGetLastWorktreePath_EmptyMap(t *testing.T) {
	originalCurrent := current
	defer func() { current = originalCurrent }()

	current = &State{LastWorktreePath: nil}
	result := GetLastWorktreePath("/main/repo")
	if result != "" {
		t.Errorf("GetLastWorktreePath() with nil map = %q, want empty", result)
	}
}

func TestGetLastWorktreePath_Found(t *testing.T) {
	originalCurrent := current
	defer func() { current = originalCurrent }()

	current = &State{
		LastWorktreePath: map[string]string{
			"/main/repo": "/worktrees/feature-auth",
		},
	}
	result := GetLastWorktreePath("/main/repo")
	if result != "/worktrees/feature-auth" {
		t.Errorf("GetLastWorktreePath() = %q, want /worktrees/feature-auth", result)
	}
}

func TestSetLastWorktreePath(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current
	defer func() {
		path = originalPath
		current = originalCurrent
	}()

	stateFile := filepath.Join(tmpDir, "state.json")
	path = stateFile
	current = &State{}

	err := SetLastWorktreePath("/main/repo", "/worktrees/feature-billing")
	if err != nil {
		t.Fatalf("SetLastWorktreePath() failed: %v", err)
	}

	// Verify in memory
	if current.LastWorktreePath["/main/repo"] != "/worktrees/feature-billing" {
		t.Errorf("stored path = %q, want /worktrees/feature-billing", current.LastWorktreePath["/main/repo"])
	}

	// Verify saved to disk
	data, _ := os.ReadFile(stateFile)
	var loaded State
	_ = json.Unmarshal(data, &loaded)
	if loaded.LastWorktreePath["/main/repo"] != "/worktrees/feature-billing" {
		t.Errorf("persisted path = %q, want /worktrees/feature-billing", loaded.LastWorktreePath["/main/repo"])
	}
}

func TestSetLastWorktreePath_InitializesNilState(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current
	defer func() {
		path = originalPath
		current = originalCurrent
	}()

	path = filepath.Join(tmpDir, "state.json")
	current = nil

	err := SetLastWorktreePath("/main/repo", "/worktrees/feature")
	if err != nil {
		t.Fatalf("SetLastWorktreePath() failed: %v", err)
	}

	if current == nil {
		t.Error("SetLastWorktreePath() should initialize current state")
	}
	if current.LastWorktreePath["/main/repo"] != "/worktrees/feature" {
		t.Errorf("path = %q, want /worktrees/feature", current.LastWorktreePath["/main/repo"])
	}
}

func TestClearLastWorktreePath(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current
	defer func() {
		path = originalPath
		current = originalCurrent
	}()

	path = filepath.Join(tmpDir, "state.json")
	current = &State{
		LastWorktreePath: map[string]string{
			"/main/repo": "/worktrees/feature",
		},
	}

	err := ClearLastWorktreePath("/main/repo")
	if err != nil {
		t.Fatalf("ClearLastWorktreePath() failed: %v", err)
	}

	// Verify removed
	if _, exists := current.LastWorktreePath["/main/repo"]; exists {
		t.Error("ClearLastWorktreePath() should remove the entry")
	}

	// Verify saved to disk
	data, _ := os.ReadFile(path)
	var loaded State
	_ = json.Unmarshal(data, &loaded)
	if _, exists := loaded.LastWorktreePath["/main/repo"]; exists {
		t.Error("ClearLastWorktreePath() should persist removal")
	}
}

func TestClearLastWorktreePath_NilState(t *testing.T) {
	originalCurrent := current
	defer func() { current = originalCurrent }()

	current = nil
	err := ClearLastWorktreePath("/main/repo")
	if err != nil {
		t.Fatalf("ClearLastWorktreePath() with nil state should not error: %v", err)
	}
}

func TestClearLastWorktreePath_NilMap(t *testing.T) {
	originalCurrent := current
	defer func() { current = originalCurrent }()

	current = &State{LastWorktreePath: nil}
	err := ClearLastWorktreePath("/main/repo")
	if err != nil {
		t.Fatalf("ClearLastWorktreePath() with nil map should not error: %v", err)
	}
}

func TestGetLineWrapEnabled_Default(t *testing.T) {
	originalCurrent := current
	defer func() { current = originalCurrent }()

	current = nil
	enabled := GetLineWrapEnabled()
	if enabled {
		t.Errorf("GetLineWrapEnabled() with nil current = %v, want false", enabled)
	}
}

func TestGetLineWrapEnabled_Set(t *testing.T) {
	originalCurrent := current
	defer func() { current = originalCurrent }()

	current = &State{LineWrapEnabled: true}
	enabled := GetLineWrapEnabled()
	if !enabled {
		t.Errorf("GetLineWrapEnabled() = %v, want true", enabled)
	}
}

func TestSetLineWrapEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current
	defer func() {
		path = originalPath
		current = originalCurrent
	}()

	stateFile := filepath.Join(tmpDir, "state.json")
	path = stateFile
	current = &State{LineWrapEnabled: false}

	err := SetLineWrapEnabled(true)
	if err != nil {
		t.Fatalf("SetLineWrapEnabled() failed: %v", err)
	}

	if !current.LineWrapEnabled {
		t.Errorf("current.LineWrapEnabled = %v, want true", current.LineWrapEnabled)
	}

	// Verify saved to disk
	data, _ := os.ReadFile(stateFile)
	var loaded State
	_ = json.Unmarshal(data, &loaded)
	if !loaded.LineWrapEnabled {
		t.Errorf("saved LineWrapEnabled = %v, want true", loaded.LineWrapEnabled)
	}
}

func TestSetLineWrapEnabled_InitializesNilState(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := path
	originalCurrent := current
	defer func() {
		path = originalPath
		current = originalCurrent
	}()

	stateFile := filepath.Join(tmpDir, "state.json")
	path = stateFile
	current = nil

	err := SetLineWrapEnabled(true)
	if err != nil {
		t.Fatalf("SetLineWrapEnabled() failed: %v", err)
	}

	if current == nil {
		t.Error("SetLineWrapEnabled() should initialize current state")
	}
	if !current.LineWrapEnabled {
		t.Errorf("LineWrapEnabled = %v, want true", current.LineWrapEnabled)
	}
}
