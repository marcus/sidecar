package projects

import (
	"os"
	"path/filepath"

	"github.com/marcus/sidecar/internal/config"
)

// ScanForProjects walks the given directories looking for subdirectories
// that contain a .todos/ directory (indicating td is initialized).
// It returns only projects not already in the existing list.
func ScanForProjects(dirs []string, existing []config.ProjectConfig) []ScanResult {
	existingPaths := make(map[string]bool, len(existing))
	for _, p := range existing {
		abs, err := filepath.Abs(config.ExpandPath(p.Path))
		if err == nil {
			existingPaths[abs] = true
		}
	}

	var results []ScanResult
	for _, dir := range dirs {
		expanded := config.ExpandPath(dir)
		entries, err := os.ReadDir(expanded)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			// Skip hidden directories
			if entry.Name()[0] == '.' {
				continue
			}
			dirPath := filepath.Join(expanded, entry.Name())
			todosPath := filepath.Join(dirPath, ".todos")
			info, err := os.Stat(todosPath)
			if err != nil || !info.IsDir() {
				continue
			}
			absPath, err := filepath.Abs(dirPath)
			if err != nil {
				absPath = dirPath
			}
			if existingPaths[absPath] {
				continue
			}
			results = append(results, ScanResult{
				Name: entry.Name(),
				Path: absPath,
			})
		}
	}
	return results
}
