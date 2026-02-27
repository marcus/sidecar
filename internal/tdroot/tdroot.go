// Package tdroot provides utilities for resolving td's root directory and database paths.
// It handles the .td-root file mechanism used to share a td database across git worktrees.
package tdroot

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marcus/sidecar/internal/projectdir"
)

const (
	// TDRootFile is the filename used to link a worktree to a shared td root.
	TDRootFile = ".td-root"
	// TodosDir is the directory containing td's database and related files.
	TodosDir = ".todos"
	// DBFile is the filename of td's SQLite database.
	DBFile = "issues.db"
)

// gitMainWorktree returns the main worktree root if dir is an external worktree.
// Returns "" if dir is already the main worktree or on any error.
func gitMainWorktree(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	commonDir := strings.TrimSpace(string(out))
	if commonDir == "" {
		return ""
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(dir, commonDir)
	}
	mainRoot := filepath.Dir(filepath.Clean(commonDir))
	if mainRoot == filepath.Clean(dir) {
		return ""
	}
	return mainRoot
}

// ResolveTDRoot reads .td-root file and returns the resolved root path.
// Returns workDir if no .td-root exists or it's empty.
func ResolveTDRoot(workDir string) string {
	// Check centralized location first
	projDir, err := projectdir.Resolve(workDir)
	if err == nil {
		tdRootPath := filepath.Join(projDir, "td-root")
		if data, err := os.ReadFile(tdRootPath); err == nil {
			rootDir := strings.TrimSpace(string(data))
			if rootDir != "" {
				return filepath.Clean(rootDir)
			}
		}
	}

	// Fallback to legacy .td-root file in project dir
	linkPath := filepath.Join(workDir, TDRootFile)
	data, err := os.ReadFile(linkPath)
	if err != nil {
		// Check main worktree for .td-root or .todos (handles external worktrees)
		if mainRoot := gitMainWorktree(workDir); mainRoot != "" {
			mainLinkPath := filepath.Join(mainRoot, TDRootFile)
			if data, err := os.ReadFile(mainLinkPath); err == nil {
				rootDir := strings.TrimSpace(string(data))
				if rootDir != "" {
					return filepath.Clean(rootDir)
				}
			}
			todosPath := filepath.Join(mainRoot, TodosDir)
			if fi, err := os.Stat(todosPath); err == nil && fi.IsDir() {
				return mainRoot
			}
		}
		return workDir
	}

	rootDir := strings.TrimSpace(string(data))
	if rootDir == "" {
		return workDir
	}

	return filepath.Clean(rootDir)
}

// ResolveDBPath returns the full path to the td database.
// Uses .td-root resolution to find the correct database location.
func ResolveDBPath(workDir string) string {
	root := ResolveTDRoot(workDir)
	return filepath.Join(root, TodosDir, DBFile)
}

// ErrTodosIsFile is returned when .todos exists as a file instead of a directory.
var ErrTodosIsFile = errors.New("found .todos file where a directory is expected")

// CheckTodosConflict checks whether a .todos path exists as a regular file
// instead of a directory. This can happen when an AI agent or other tool
// creates a .todos file, conflicting with td's expected .todos directory.
// Returns ErrTodosIsFile if there's a conflict, nil otherwise.
func CheckTodosConflict(workDir string) error {
	root := ResolveTDRoot(workDir)
	todosPath := filepath.Join(root, TodosDir)
	fi, err := os.Lstat(todosPath)
	if err != nil {
		return nil // doesn't exist — no conflict
	}
	if !fi.IsDir() {
		return ErrTodosIsFile
	}
	return nil
}

// CreateTDRoot writes a td-root file to the centralized project directory pointing to targetRoot.
// Used when creating worktrees to share the td database.
func CreateTDRoot(projectRoot, worktreePath, targetRoot string) error {
	projDir, err := projectdir.Resolve(projectRoot)
	if err != nil {
		return err
	}
	tdRootPath := filepath.Join(projDir, "td-root")
	return os.WriteFile(tdRootPath, []byte(targetRoot+"\n"), 0644)
}
