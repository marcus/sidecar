package tdroot

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// CreateTDRoot writes a .td-root file pointing to targetRoot.
// Used when creating worktrees to share the td database.
func CreateTDRoot(worktreePath, targetRoot string) error {
	tdRootPath := filepath.Join(worktreePath, TDRootFile)
	return os.WriteFile(tdRootPath, []byte(targetRoot+"\n"), 0644)
}
