package projectdir

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/marcus/sidecar/internal/config"
)

// legacyWorktreeFiles maps legacy per-worktree file names to their
// centralized equivalents (stored under projects/<slug>/worktrees/<wt>/).
var legacyWorktreeFiles = map[string]string{
	".sidecar-task":  "task",
	".sidecar-agent": "agent",
	".sidecar-pr":    "pr",
	".sidecar-base":  "base",
}

// legacySidecarDirFiles lists files inside the .sidecar/ directory that
// should be moved to the centralized project directory.
var legacySidecarDirFiles = []string{
	"shells.json",
	"config.json",
}

// legacyTransientFiles lists files inside .sidecar/ that are transient
// and should simply be deleted during migration.
var legacyTransientFiles = []string{
	"shells.json.lock",
	"shells.json.tmp",
}

// Migrate moves legacy project files from project directories to centralized
// storage under ~/.config/sidecar/projects/<slug>/. worktreePaths is the list
// of known worktree paths to check for legacy files. Does nothing if no
// legacy files are found.
func Migrate(projectRoot string, worktreePaths []string) error {
	base := filepath.Dir(config.ConfigPath())
	return migrateWithBase(base, projectRoot, worktreePaths)
}

// migrateWithBase is the testable core of Migrate. It uses base as the
// sidecar config directory instead of deriving it from config.ConfigPath().
func migrateWithBase(base, projectRoot string, worktreePaths []string) error {
	if !hasLegacyFiles(projectRoot, worktreePaths) {
		return nil
	}

	projDir, err := resolveWithBase(base, projectRoot)
	if err != nil {
		return err
	}

	// Migrate .sidecar/ directory contents
	sidecarDir := filepath.Join(projectRoot, ".sidecar")
	for _, name := range legacySidecarDirFiles {
		src := filepath.Join(sidecarDir, name)
		dst := filepath.Join(projDir, name)
		if err := moveFile(src, dst); err != nil {
			log.Printf("sidecar: migrate %s: %v", src, err)
		}
	}

	// Remove transient files
	for _, name := range legacyTransientFiles {
		p := filepath.Join(sidecarDir, name)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			log.Printf("sidecar: remove transient %s: %v", p, err)
		}
	}

	// Remove the .sidecar directory if now empty
	removeIfEmpty(sidecarDir)

	// Migrate .td-root
	tdRootSrc := filepath.Join(projectRoot, ".td-root")
	tdRootDst := filepath.Join(projDir, "td-root")
	if err := moveFile(tdRootSrc, tdRootDst); err != nil {
		log.Printf("sidecar: migrate %s: %v", tdRootSrc, err)
	}

	// Migrate per-worktree files
	for _, wtPath := range worktreePaths {
		wtDir, wtErr := worktreeDirWithBase(base, projectRoot, wtPath)
		if wtErr != nil {
			log.Printf("sidecar: resolve worktree dir for %s: %v", wtPath, wtErr)
			continue
		}
		for legacyName, newName := range legacyWorktreeFiles {
			src := filepath.Join(wtPath, legacyName)
			dst := filepath.Join(wtDir, newName)
			if err := moveFile(src, dst); err != nil {
				log.Printf("sidecar: migrate %s: %v", src, err)
			}
		}
	}

	return nil
}

// hasLegacyFiles performs a quick scan to determine whether any legacy files
// exist that need migration.
func hasLegacyFiles(projectRoot string, worktreePaths []string) bool {
	// Check .sidecar/ directory
	sidecarDir := filepath.Join(projectRoot, ".sidecar")
	if _, err := os.Stat(sidecarDir); err == nil {
		return true
	}

	// Check .td-root
	if _, err := os.Stat(filepath.Join(projectRoot, ".td-root")); err == nil {
		return true
	}

	// Check per-worktree files
	for _, wtPath := range worktreePaths {
		for legacyName := range legacyWorktreeFiles {
			if _, err := os.Stat(filepath.Join(wtPath, legacyName)); err == nil {
				return true
			}
		}
	}

	return false
}

// moveFile moves src to dst. It tries os.Rename first (fast, same filesystem),
// then falls back to copy+delete for cross-device moves. Does nothing if src
// does not exist.
func moveFile(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	// Try rename first (atomic, same filesystem)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Fallback: copy + delete
	return copyAndDelete(src, dst)
}

// copyAndDelete copies src to dst then removes src.
func copyAndDelete(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	info, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}

	_, err = io.Copy(dstFile, srcFile)
	if closeErr := dstFile.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	return os.Remove(src)
}

// removeIfEmpty removes a directory only if it is empty.
func removeIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	if len(entries) == 0 {
		_ = os.Remove(dir)
	}
}
