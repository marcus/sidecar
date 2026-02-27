// Package migration handles one-time migration of legacy sidecar state files
// to the centralized XDG-compliant locations introduced in the
// feature/centralized-project-storage PR.
//
// Legacy layout (before centralization):
//
//	<project-root>/.sidecar/shells.json      → project shell manifest
//	<project-root>/.sidecar/config.json      → project prompt config
//	<project-root>/.td-root                  → td database root pointer
//	<worktree>/.sidecar-task                 → linked task ID
//	<worktree>/.sidecar-agent                → agent type
//	<worktree>/.sidecar-pr                   → PR URL
//	<worktree>/.sidecar-base                 → base branch
//	<worktree>/.sidecar-start.sh             → agent launcher script
//
// New layout (after centralization):
//
//	~/.local/state/sidecar/projects/<slug>/shells.json
//	~/.local/state/sidecar/projects/<slug>/config.json
//	~/.local/state/sidecar/projects/<slug>/td-root
//	~/.local/state/sidecar/projects/<slug>/worktrees/<wt-slug>/task
//	~/.local/state/sidecar/projects/<slug>/worktrees/<wt-slug>/agent
//	~/.local/state/sidecar/projects/<slug>/worktrees/<wt-slug>/pr
//	~/.local/state/sidecar/projects/<slug>/worktrees/<wt-slug>/base
//	~/.local/state/sidecar/projects/<slug>/worktrees/<wt-slug>/start.sh
//
// Migration is:
//   - Automatic: called on first use of a project directory
//   - Idempotent: safe to call multiple times, will not overwrite existing data
//   - Non-destructive: legacy files are never deleted
//   - Logged: each migrated file is reported via slog.Info
package migration

import (
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/projectdir"
)

// legacySidecarDir is the legacy per-project directory name.
const legacySidecarDir = ".sidecar"

// Legacy project-level filenames inside .sidecar/.
var legacyProjectFiles = []struct {
	legacy string // filename inside .sidecar/
	new    string // filename inside centralized project dir
}{
	{"shells.json", "shells.json"},
	{"config.json", "config.json"},
}

// Legacy per-worktree dotfiles and their new names in the centralized worktree dir.
var legacyWorktreeFiles = []struct {
	legacy string // filename in worktree root
	new    string // filename in centralized worktree dir
}{
	{".sidecar-task", "task"},
	{".sidecar-agent", "agent"},
	{".sidecar-pr", "pr"},
	{".sidecar-base", "base"},
	{".sidecar-start.sh", "start.sh"},
}

// MigrateProject migrates legacy project-level files from projectRoot to the
// centralized state directory. The worktrees slice lists all worktree paths
// that belong to this project (including the main worktree / projectRoot
// itself). Passing an empty worktrees slice skips worktree migration.
//
// Each file is only copied if the destination does not already exist.
// The originals are never removed.
func MigrateProject(projectRoot string, worktrees []string) error {
	base := config.StateDir()
	return migrateProjectWithBase(base, projectRoot, worktrees)
}

// migrateProjectWithBase is the testable core of MigrateProject.
func migrateProjectWithBase(base, projectRoot string, worktrees []string) error {
	projDir, err := projectdir.ResolveWithBase(base, projectRoot)
	if err != nil {
		return err
	}

	// --- Migrate .sidecar/ project files ---
	sidecarDir := filepath.Join(projectRoot, legacySidecarDir)
	if fi, err := os.Stat(sidecarDir); err == nil && fi.IsDir() {
		for _, f := range legacyProjectFiles {
			src := filepath.Join(sidecarDir, f.legacy)
			dst := filepath.Join(projDir, f.new)
			if err := copyIfNotExists(src, dst); err != nil {
				slog.Warn("migration: failed to migrate project file",
					"src", src, "dst", dst, "err", err)
			}
		}
	}

	// --- Migrate .td-root ---
	legacyTDRoot := filepath.Join(projectRoot, ".td-root")
	newTDRoot := filepath.Join(projDir, "td-root")
	if err := copyIfNotExists(legacyTDRoot, newTDRoot); err != nil {
		slog.Warn("migration: failed to migrate td-root",
			"src", legacyTDRoot, "dst", newTDRoot, "err", err)
	}

	// --- Migrate per-worktree files ---
	for _, wtPath := range worktrees {
		if err := migrateWorktreeWithBase(base, projectRoot, wtPath); err != nil {
			slog.Warn("migration: failed to migrate worktree",
				"worktree", wtPath, "err", err)
		}
	}

	return nil
}

// migrateWorktreeWithBase migrates per-worktree legacy dotfiles.
func migrateWorktreeWithBase(base, projectRoot, worktreePath string) error {
	wtDir, err := projectdir.WorktreeDirWithBase(base, projectRoot, worktreePath)
	if err != nil {
		return err
	}

	for _, f := range legacyWorktreeFiles {
		src := filepath.Join(worktreePath, f.legacy)
		dst := filepath.Join(wtDir, f.new)
		if err := copyIfNotExists(src, dst); err != nil {
			slog.Warn("migration: failed to migrate worktree file",
				"src", src, "dst", dst, "err", err)
		}
	}
	return nil
}

// copyIfNotExists copies src to dst only if:
//   - src exists and is a regular file
//   - dst does not already exist
//
// Returns nil (no-op) if src doesn't exist or dst already exists.
// Returns an error only on unexpected I/O failures.
func copyIfNotExists(src, dst string) error {
	// Check source.
	srcInfo, err := os.Lstat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Nothing to migrate.
		}
		return err
	}

	// Only migrate regular files (skip symlinks, directories, etc.).
	if !srcInfo.Mode().IsRegular() {
		return nil
	}

	// Skip if destination already exists.
	if _, err := os.Lstat(dst); err == nil {
		slog.Debug("migration: destination already exists, skipping",
			"src", src, "dst", dst)
		return nil
	}

	// Ensure destination directory exists.
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	if err := copyFile(src, dst, srcInfo.Mode()); err != nil {
		return err
	}

	slog.Info("migration: migrated legacy file", "src", src, "dst", dst)
	return nil
}

// copyFile copies src to dst, preserving the file mode.
func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode&0777)
	if err != nil {
		if os.IsExist(err) {
			return nil // Raced with another process; dst now exists — that's fine.
		}
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(dst) // Clean up partial write.
		return err
	}
	return out.Close()
}
