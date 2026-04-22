package app

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/marcus/sidecar/internal/features"
)

type jjWorkspaceRef struct {
	Name string
}

func useNativeJJWorkspaces(workDir string) bool {
	if !jjWorkspacesEnabled() {
		return false
	}
	return isJJWorkspace(workDir)
}

func jjWorkspacesEnabled() bool {
	return features.IsEnabled(features.JjPlugin.Name)
}

func isJJWorkspace(workDir string) bool {
	cmd := exec.Command("jj", "--no-pager", "--ignore-working-copy", "root")
	cmd.Dir = workDir
	return cmd.Run() == nil
}

func getJJWorkspaces(workDir string) []WorktreeInfo {
	refs, err := listJJWorkspaceRefs(workDir)
	if err != nil || len(refs) == 0 {
		return nil
	}

	root, err := jjWorkspaceRoot(workDir)
	if err != nil || root == "" {
		return nil
	}
	repoPath, err := jjRepoPath(root)
	if err != nil || repoPath == "" {
		return nil
	}

	mainPath := jjMainWorkspacePath(repoPath, root)
	pathsByName := findJJWorkspacePaths(root, repoPath, refs)

	workspaces := make([]WorktreeInfo, 0, len(refs))
	for _, ref := range refs {
		path := pathsByName[ref.Name]
		if path == "" {
			continue
		}
		workspaces = append(workspaces, WorktreeInfo{
			Path:   path,
			Branch: ref.Name,
			IsMain: sameCleanPath(path, mainPath),
			IsJJ:   true,
		})
	}
	return workspaces
}

func listJJWorkspaceRefs(workDir string) ([]jjWorkspaceRef, error) {
	cmd := exec.Command("jj", "--no-pager", "--ignore-working-copy", "workspace", "list", "-T", "name ++ \"\\n\"")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseJJWorkspaceList(string(output)), nil
}

func parseJJWorkspaceList(output string) []jjWorkspaceRef {
	var refs []jjWorkspaceRef
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || seen[name] {
			continue
		}
		refs = append(refs, jjWorkspaceRef{Name: name})
		seen[name] = true
	}
	return refs
}

func jjWorkspaceRoot(workDir string) (string, error) {
	cmd := exec.Command("jj", "--no-pager", "--ignore-working-copy", "workspace", "root")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return filepath.Clean(strings.TrimSpace(string(output))), nil
}

func jjRepoPath(workspaceRoot string) (string, error) {
	repoEntry := filepath.Join(workspaceRoot, ".jj", "repo")
	info, err := os.Stat(repoEntry)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return filepath.Clean(repoEntry), nil
	}

	content, err := os.ReadFile(repoEntry)
	if err != nil {
		return "", err
	}
	repoPath := strings.TrimSpace(string(content))
	if repoPath == "" {
		return "", fmt.Errorf("empty jj repo pointer: %s", repoEntry)
	}
	if !filepath.IsAbs(repoPath) {
		repoPath = filepath.Join(filepath.Dir(repoEntry), repoPath)
	}
	return filepath.Clean(repoPath), nil
}

func jjMainWorkspacePath(repoPath, fallbackRoot string) string {
	cleanRepoPath := filepath.Clean(repoPath)
	if filepath.Base(cleanRepoPath) == "repo" && filepath.Base(filepath.Dir(cleanRepoPath)) == ".jj" {
		return filepath.Clean(filepath.Dir(filepath.Dir(cleanRepoPath)))
	}
	return filepath.Clean(fallbackRoot)
}

func findJJWorkspacePaths(root, repoPath string, refs []jjWorkspaceRef) map[string]string {
	names := make(map[string]bool, len(refs))
	for _, ref := range refs {
		names[ref.Name] = true
	}

	paths := map[string]string{}
	for _, candidate := range jjWorkspaceSearchCandidates(root, repoPath) {
		if len(paths) == len(refs) {
			break
		}
		if !sameJJRepo(candidate, repoPath) {
			continue
		}
		name := readJJWorkspaceName(candidate, names)
		if name == "" {
			continue
		}
		if _, exists := paths[name]; !exists {
			paths[name] = candidate
		}
	}
	return paths
}

func jjWorkspaceSearchCandidates(root, repoPath string) []string {
	mainPath := jjMainWorkspacePath(repoPath, root)
	parentSet := map[string]bool{
		filepath.Dir(root):     true,
		filepath.Dir(mainPath): true,
	}

	candidateSet := map[string]bool{
		filepath.Clean(root):     true,
		filepath.Clean(mainPath): true,
	}
	for parent := range parentSet {
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			candidateSet[filepath.Join(parent, entry.Name())] = true
		}
	}

	candidates := make([]string, 0, len(candidateSet))
	for candidate := range candidateSet {
		candidates = append(candidates, candidate)
	}
	sort.Strings(candidates)
	return candidates
}

func sameJJRepo(candidate, repoPath string) bool {
	candidateRepoPath, err := jjRepoPath(candidate)
	if err != nil {
		return false
	}
	return sameCleanPath(candidateRepoPath, repoPath)
}

func readJJWorkspaceName(workspaceRoot string, knownNames map[string]bool) string {
	data, err := os.ReadFile(filepath.Join(workspaceRoot, ".jj", "working_copy", "checkout"))
	if err != nil {
		return ""
	}

	matches := make([]string, 0, 1)
	for name := range knownNames {
		if bytes.Contains(data, []byte(name)) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool {
		return len(matches[i]) > len(matches[j])
	})
	return matches[0]
}

func sameCleanPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func findMainJJWorkspaceFromDeleted(deletedPath string) string {
	parentDir := filepath.Dir(deletedPath)
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(parentDir, entry.Name())
		repoPath, err := jjRepoPath(candidate)
		if err != nil {
			continue
		}
		mainPath := jjMainWorkspacePath(repoPath, candidate)
		if mainPath != "" {
			return mainPath
		}
	}
	return ""
}
