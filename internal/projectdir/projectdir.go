// Package projectdir resolves project-specific state directories under
// $XDG_STATE_HOME/sidecar/projects/<slug>/ (defaults to
// ~/.local/state/sidecar/projects/<slug>/). Each project root gets a
// unique slug-named directory containing a meta.json that maps back to
// the original project path.
package projectdir

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcus/sidecar/internal/config"
)

// projectMeta is stored as meta.json inside each project slug directory.
type projectMeta struct {
	Path string `json:"path"`
}

// Resolve returns the data directory for the given project root path.
// It creates the directory (with meta.json) if it does not already exist.
// On subsequent calls with the same projectRoot, the existing directory is
// returned.
func Resolve(projectRoot string) (string, error) {
	base := config.StateDir()
	return resolveWithBase(base, projectRoot)
}

// WorktreeDir returns the worktree-specific data directory for a project.
// The directory is created if it does not exist.
func WorktreeDir(projectRoot, worktreePath string) (string, error) {
	base := config.StateDir()
	return worktreeDirWithBase(base, projectRoot, worktreePath)
}

// WorktreeDirWithBase is the exported, testable form of WorktreeDir.
// base overrides the state directory (e.g. a temp dir in tests).
func WorktreeDirWithBase(base, projectRoot, worktreePath string) (string, error) {
	return worktreeDirWithBase(base, projectRoot, worktreePath)
}

// ResolveWithBase is the exported, testable form of Resolve.
// base overrides the state directory (e.g. a temp dir in tests).
func ResolveWithBase(base, projectRoot string) (string, error) {
	return resolveWithBase(base, projectRoot)
}

// worktreeDirWithBase is the testable core of WorktreeDir.
func worktreeDirWithBase(base, projectRoot, worktreePath string) (string, error) {
	projectDir, err := resolveWithBase(base, projectRoot)
	if err != nil {
		return "", err
	}

	wtSlug := sanitizeSlug(filepath.Base(worktreePath))
	dir := filepath.Join(projectDir, "worktrees", wtSlug)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating worktree dir: %w", err)
	}
	return dir, nil
}

// resolveWithBase is the testable core of Resolve. It uses base as the
// sidecar state directory (e.g. ~/.local/state/sidecar) instead of
// deriving it from config.StateDir().
func resolveWithBase(base, projectRoot string) (string, error) {
	projectsDir := filepath.Join(base, "projects")

	// Scan existing project directories for a matching path.
	if dir, found := findByMeta(projectsDir, projectRoot); found {
		return dir, nil
	}

	slug := sanitizeSlug(filepath.Base(projectRoot))

	// Try slug, then slug-2, slug-3, ..., slug-99.
	for i := 1; i <= 99; i++ {
		candidate := slug
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", slug, i)
		}
		dir := filepath.Join(projectsDir, candidate)

		_, err := os.Stat(dir)
		if os.IsNotExist(err) {
			// Slot is free -- claim it.
			return createProjectDir(dir, projectRoot)
		}
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", dir, err)
		}

		// Directory exists. Check if it belongs to the same project.
		meta, readErr := readMeta(dir)
		if readErr != nil {
			// Corrupt or missing meta -- skip to next candidate.
			continue
		}
		if meta.Path == projectRoot {
			return dir, nil
		}
		// Different project owns this slug -- try next suffix.
	}

	return "", fmt.Errorf("could not allocate slug for %q (tried 99 suffixes)", projectRoot)
}

// sanitizeSlug removes characters that are problematic in directory names.
func sanitizeSlug(s string) string {
	// Remove forward and back slashes.
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "\\", "")

	// Remove control characters (0x00-0x1F).
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 {
			b.WriteRune(r)
		}
	}
	s = b.String()

	// Replace empty, ".", ".." with underscore.
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}

// findByMeta scans all subdirectories in projectsDir looking for one
// whose meta.json path matches projectRoot. Returns the directory path
// and true if found.
func findByMeta(projectsDir, projectRoot string) (string, bool) {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(projectsDir, e.Name())
		meta, err := readMeta(dir)
		if err != nil {
			continue
		}
		if meta.Path == projectRoot {
			return dir, true
		}
	}
	return "", false
}

// readMeta reads and parses the meta.json in the given directory.
func readMeta(dir string) (projectMeta, error) {
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return projectMeta{}, err
	}
	var meta projectMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return projectMeta{}, err
	}
	return meta, nil
}

// createProjectDir creates the slug directory and writes its meta.json.
func createProjectDir(dir, projectRoot string) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating project dir: %w", err)
	}

	meta := projectMeta{Path: projectRoot}
	data, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshaling meta: %w", err)
	}

	metaPath := filepath.Join(dir, "meta.json")
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return "", fmt.Errorf("writing meta.json: %w", err)
	}

	return dir, nil
}
