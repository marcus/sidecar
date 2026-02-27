package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/sidecar/internal/projectdir"
)

// helpers

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file to exist: %s", path)
	}
}

func assertFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected file to not exist: %s", path)
	}
}

func assertContent(t *testing.T, path, want string) {
	t.Helper()
	got := readFile(t, path)
	if got != want {
		t.Errorf("file %s: got %q, want %q", path, got, want)
	}
}

// projectDir resolves the centralized project dir for projectRoot inside base.
func resolveProjectDir(t *testing.T, base, projectRoot string) string {
	t.Helper()
	dir, err := projectdir.ResolveWithBase(base, projectRoot)
	if err != nil {
		t.Fatalf("ResolveWithBase: %v", err)
	}
	return dir
}

// worktreeDir resolves the centralized worktree dir.
func resolveWorktreeDir(t *testing.T, base, projectRoot, worktreePath string) string {
	t.Helper()
	dir, err := projectdir.WorktreeDirWithBase(base, projectRoot, worktreePath)
	if err != nil {
		t.Fatalf("WorktreeDirWithBase: %v", err)
	}
	return dir
}

// TestFreshProject — no legacy files, migration is a no-op but should not error.
func TestFreshProject(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()

	if err := migrateProjectWithBase(base, project, []string{project}); err != nil {
		t.Fatalf("migrateProjectWithBase: %v", err)
	}

	projDir := resolveProjectDir(t, base, project)
	// No legacy files — nothing should be copied.
	assertFileNotExists(t, filepath.Join(projDir, "shells.json"))
	assertFileNotExists(t, filepath.Join(projDir, "config.json"))
	assertFileNotExists(t, filepath.Join(projDir, "td-root"))
}

// TestFullSidecarDir — project with a full .sidecar/ directory.
func TestFullSidecarDir(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()

	// Create legacy .sidecar/ files.
	writeFile(t, filepath.Join(project, ".sidecar", "shells.json"), `{"version":1,"shells":[]}`)
	writeFile(t, filepath.Join(project, ".sidecar", "config.json"), `{"prompts":[]}`)
	writeFile(t, filepath.Join(project, ".td-root"), "/some/other/project\n")

	if err := migrateProjectWithBase(base, project, []string{project}); err != nil {
		t.Fatalf("migrateProjectWithBase: %v", err)
	}

	projDir := resolveProjectDir(t, base, project)
	assertContent(t, filepath.Join(projDir, "shells.json"), `{"version":1,"shells":[]}`)
	assertContent(t, filepath.Join(projDir, "config.json"), `{"prompts":[]}`)
	assertContent(t, filepath.Join(projDir, "td-root"), "/some/other/project\n")

	// Legacy files must still exist (non-destructive).
	assertFileExists(t, filepath.Join(project, ".sidecar", "shells.json"))
	assertFileExists(t, filepath.Join(project, ".sidecar", "config.json"))
	assertFileExists(t, filepath.Join(project, ".td-root"))
}

// TestPartialLegacyFiles — only some legacy files present.
func TestPartialLegacyFiles(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()

	// Only shells.json, no config.json, no .td-root.
	writeFile(t, filepath.Join(project, ".sidecar", "shells.json"), `{"version":1,"shells":[]}`)

	if err := migrateProjectWithBase(base, project, []string{project}); err != nil {
		t.Fatalf("migrateProjectWithBase: %v", err)
	}

	projDir := resolveProjectDir(t, base, project)
	assertContent(t, filepath.Join(projDir, "shells.json"), `{"version":1,"shells":[]}`)
	assertFileNotExists(t, filepath.Join(projDir, "config.json"))
	assertFileNotExists(t, filepath.Join(projDir, "td-root"))
}

// TestTDRootOnlyNoSidecar — .td-root but no .sidecar/ directory.
func TestTDRootOnlyNoSidecar(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()

	writeFile(t, filepath.Join(project, ".td-root"), "/main/project\n")

	if err := migrateProjectWithBase(base, project, []string{}); err != nil {
		t.Fatalf("migrateProjectWithBase: %v", err)
	}

	projDir := resolveProjectDir(t, base, project)
	assertContent(t, filepath.Join(projDir, "td-root"), "/main/project\n")
	assertFileNotExists(t, filepath.Join(projDir, "shells.json"))
}

// TestWorktreeFiles — per-worktree legacy dotfiles.
func TestWorktreeFiles(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()
	worktree := t.TempDir()

	writeFile(t, filepath.Join(worktree, ".sidecar-task"), "td-abc123\n")
	writeFile(t, filepath.Join(worktree, ".sidecar-agent"), "claude\n")
	writeFile(t, filepath.Join(worktree, ".sidecar-pr"), "https://github.com/owner/repo/pull/42\n")
	writeFile(t, filepath.Join(worktree, ".sidecar-base"), "main\n")
	writeFile(t, filepath.Join(worktree, ".sidecar-start.sh"), "#!/bin/bash\necho hello\n")

	if err := migrateProjectWithBase(base, project, []string{worktree}); err != nil {
		t.Fatalf("migrateProjectWithBase: %v", err)
	}

	wtDir := resolveWorktreeDir(t, base, project, worktree)
	assertContent(t, filepath.Join(wtDir, "task"), "td-abc123\n")
	assertContent(t, filepath.Join(wtDir, "agent"), "claude\n")
	assertContent(t, filepath.Join(wtDir, "pr"), "https://github.com/owner/repo/pull/42\n")
	assertContent(t, filepath.Join(wtDir, "base"), "main\n")
	assertContent(t, filepath.Join(wtDir, "start.sh"), "#!/bin/bash\necho hello\n")

	// Originals preserved.
	assertFileExists(t, filepath.Join(worktree, ".sidecar-task"))
	assertFileExists(t, filepath.Join(worktree, ".sidecar-agent"))
}

// TestMultipleWorktreesMixed — multiple worktrees, some with legacy files.
func TestMultipleWorktreesMixed(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()
	wt1 := t.TempDir()
	wt2 := t.TempDir()

	// wt1 has legacy files, wt2 has nothing.
	writeFile(t, filepath.Join(wt1, ".sidecar-task"), "td-111\n")
	writeFile(t, filepath.Join(wt1, ".sidecar-agent"), "cursor\n")

	if err := migrateProjectWithBase(base, project, []string{wt1, wt2}); err != nil {
		t.Fatalf("migrateProjectWithBase: %v", err)
	}

	wt1Dir := resolveWorktreeDir(t, base, project, wt1)
	assertContent(t, filepath.Join(wt1Dir, "task"), "td-111\n")
	assertContent(t, filepath.Join(wt1Dir, "agent"), "cursor\n")
	assertFileNotExists(t, filepath.Join(wt1Dir, "pr"))

	wt2Dir := resolveWorktreeDir(t, base, project, wt2)
	assertFileNotExists(t, filepath.Join(wt2Dir, "task"))
	assertFileNotExists(t, filepath.Join(wt2Dir, "agent"))
}

// TestIdempotent — running migration twice does not overwrite the destination.
func TestIdempotent(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()

	writeFile(t, filepath.Join(project, ".sidecar", "shells.json"), `original`)

	// First run.
	if err := migrateProjectWithBase(base, project, []string{}); err != nil {
		t.Fatalf("first migration: %v", err)
	}

	projDir := resolveProjectDir(t, base, project)
	assertContent(t, filepath.Join(projDir, "shells.json"), `original`)

	// Modify source (simulating user edit of legacy file after migration).
	writeFile(t, filepath.Join(project, ".sidecar", "shells.json"), `modified`)

	// Second run should NOT overwrite.
	if err := migrateProjectWithBase(base, project, []string{}); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	assertContent(t, filepath.Join(projDir, "shells.json"), `original`)
}

// TestAlreadyMigrated — destination files exist, no source files.
func TestAlreadyMigrated(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()

	// Pre-populate centralized location (as if already migrated).
	projDir := resolveProjectDir(t, base, project)
	writeFile(t, filepath.Join(projDir, "shells.json"), `already migrated`)

	// No legacy files.
	if err := migrateProjectWithBase(base, project, []string{}); err != nil {
		t.Fatalf("migrateProjectWithBase: %v", err)
	}

	// Content must be unchanged.
	assertContent(t, filepath.Join(projDir, "shells.json"), `already migrated`)
}

// TestSlugCollisionDuringMigration — two projects with same basename both migrate correctly.
func TestSlugCollisionDuringMigration(t *testing.T) {
	base := t.TempDir()

	// Create two project directories with the same basename.
	parent1 := t.TempDir()
	parent2 := t.TempDir()

	// Both are named "myapp" from the slug perspective (basename of dir will vary,
	// so we simulate via subdirs).
	project1 := filepath.Join(parent1, "myapp")
	project2 := filepath.Join(parent2, "myapp")
	if err := os.MkdirAll(project1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project2, 0755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(project1, ".sidecar", "shells.json"), `project1 shells`)
	writeFile(t, filepath.Join(project2, ".sidecar", "shells.json"), `project2 shells`)

	if err := migrateProjectWithBase(base, project1, []string{}); err != nil {
		t.Fatalf("migrate project1: %v", err)
	}
	if err := migrateProjectWithBase(base, project2, []string{}); err != nil {
		t.Fatalf("migrate project2: %v", err)
	}

	dir1 := resolveProjectDir(t, base, project1)
	dir2 := resolveProjectDir(t, base, project2)

	if dir1 == dir2 {
		t.Fatalf("slug collision: both projects resolved to %s", dir1)
	}

	assertContent(t, filepath.Join(dir1, "shells.json"), `project1 shells`)
	assertContent(t, filepath.Join(dir2, "shells.json"), `project2 shells`)
}

// TestPermissionError — read-only destination directory.
func TestPermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, permission test not meaningful")
	}

	base := t.TempDir()
	project := t.TempDir()

	writeFile(t, filepath.Join(project, ".sidecar", "shells.json"), `data`)

	// First resolve to create the project dir.
	projDir := resolveProjectDir(t, base, project)

	// Make the project dir read-only.
	if err := os.Chmod(projDir, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(projDir, 0755) })

	// Migration should not panic or crash; it logs a warning and continues.
	// (We don't assert an error here — the function is designed to be resilient.)
	err := migrateProjectWithBase(base, project, []string{})
	// Either err is nil (warning logged) or non-nil; both are acceptable.
	// The important thing is it doesn't panic.
	_ = err
}

// TestSymlinkSource — source is a symlink; should be skipped (not migrated).
func TestSymlinkSource(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()

	// Create a real file and a symlink pointing to it.
	realFile := filepath.Join(project, "real.json")
	writeFile(t, realFile, `{"data":"real"}`)

	sidecarDir := filepath.Join(project, ".sidecar")
	if err := os.MkdirAll(sidecarDir, 0755); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(sidecarDir, "shells.json")
	if err := os.Symlink(realFile, symlink); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := migrateProjectWithBase(base, project, []string{}); err != nil {
		t.Fatalf("migrateProjectWithBase: %v", err)
	}

	projDir := resolveProjectDir(t, base, project)
	// Symlinks are NOT migrated (only regular files).
	assertFileNotExists(t, filepath.Join(projDir, "shells.json"))
}

// TestEmptyFile — empty legacy file should migrate (empty is valid content).
func TestEmptyFile(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()

	writeFile(t, filepath.Join(project, ".sidecar", "shells.json"), ``)

	if err := migrateProjectWithBase(base, project, []string{}); err != nil {
		t.Fatalf("migrateProjectWithBase: %v", err)
	}

	projDir := resolveProjectDir(t, base, project)
	assertContent(t, filepath.Join(projDir, "shells.json"), ``)
}

// TestMissingSidecarDir — .sidecar dir doesn't exist at all, no error.
func TestMissingSidecarDir(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()

	// No .sidecar dir, no .td-root.
	if err := migrateProjectWithBase(base, project, []string{}); err != nil {
		t.Fatalf("migrateProjectWithBase: %v", err)
	}

	projDir := resolveProjectDir(t, base, project)
	assertFileNotExists(t, filepath.Join(projDir, "shells.json"))
	assertFileNotExists(t, filepath.Join(projDir, "config.json"))
	assertFileNotExists(t, filepath.Join(projDir, "td-root"))
}
