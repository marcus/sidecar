package gitstatus

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// FileStatus represents the git status of a file.
type FileStatus string

const (
	StatusModified  FileStatus = "M"
	StatusAdded     FileStatus = "A"
	StatusDeleted   FileStatus = "D"
	StatusRenamed   FileStatus = "R"
	StatusCopied    FileStatus = "C"
	StatusUntracked FileStatus = "?"
	StatusIgnored   FileStatus = "!"
	StatusUnmerged  FileStatus = "U"
)

// FileEntry represents a single file or folder in the git status.
type FileEntry struct {
	Path       string
	Status     FileStatus
	Staged     bool
	Unstaged   bool
	OldPath    string // For renames
	DiffStats  DiffStats
	IsExpanded bool
	IsFolder   bool         // True if this represents an untracked folder
	Children   []*FileEntry // Files within this folder (when IsFolder is true)
}

// DiffStats holds addition/deletion counts.
type DiffStats struct {
	Additions int
	Deletions int
}

// FileTree groups files by status category.
type FileTree struct {
	Staged    []*FileEntry
	Modified  []*FileEntry
	Untracked []*FileEntry
	workDir   string
}

// NewFileTree creates an empty file tree for the given work directory.
func NewFileTree(workDir string) *FileTree {
	return &FileTree{workDir: workDir}
}

// Refresh reloads the git status from disk.
func (t *FileTree) Refresh() error {
	// Run git status with porcelain v2 format (null-separated)
	// Use --untracked-files=all to recursively list all files in untracked folders
	cmd := exec.Command("git", "status", "--porcelain=v2", "-z", "--untracked-files=all")
	cmd.Dir = t.workDir
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	// Build new data into temporary tree to avoid flashing during parse
	temp := &FileTree{workDir: t.workDir}
	if err := temp.parseStatus(output); err != nil {
		return err
	}

	// Get diff stats for all files
	_ = temp.loadDiffStats() // Non-fatal: continue without stats

	// Group untracked files by folder
	temp.groupUntrackedFolders()

	// Swap in new data atomically
	t.Staged = temp.Staged
	t.Modified = temp.Modified
	t.Untracked = temp.Untracked

	return nil
}

// parseStatus parses the git status --porcelain=v2 -z output.
func (t *FileTree) parseStatus(output []byte) error {
	// Split on null bytes
	parts := bytes.Split(output, []byte{0})

	i := 0
	for i < len(parts) {
		if len(parts[i]) == 0 {
			i++
			continue
		}

		line := string(parts[i])

		// Porcelain v2 format:
		// 1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>
		// 2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <X><score> <path>\t<origPath>
		// ? <path>

		if strings.HasPrefix(line, "1 ") {
			entry := t.parseOrdinaryEntry(line)
			if entry != nil {
				t.addEntry(entry)
			}
		} else if strings.HasPrefix(line, "2 ") {
			// Renamed/copied entry - next part has old path
			entry := t.parseRenamedEntry(line)
			if entry != nil {
				i++
				if i < len(parts) {
					entry.OldPath = string(parts[i])
				}
				t.addEntry(entry)
			}
		} else if strings.HasPrefix(line, "? ") {
			entry := &FileEntry{
				Path:     strings.TrimPrefix(line, "? "),
				Status:   StatusUntracked,
				Unstaged: true,
			}
			t.Untracked = append(t.Untracked, entry)
		} else if strings.HasPrefix(line, "u ") {
			// Unmerged entry
			entry := t.parseUnmergedEntry(line)
			if entry != nil {
				t.addEntry(entry)
			}
		}

		i++
	}

	// Sort all lists by path
	sort.Slice(t.Staged, func(i, j int) bool { return t.Staged[i].Path < t.Staged[j].Path })
	sort.Slice(t.Modified, func(i, j int) bool { return t.Modified[i].Path < t.Modified[j].Path })
	sort.Slice(t.Untracked, func(i, j int) bool { return t.Untracked[i].Path < t.Untracked[j].Path })

	return nil
}

// parseOrdinaryEntry parses a "1 <XY> ..." line.
func (t *FileTree) parseOrdinaryEntry(line string) *FileEntry {
	// Format: 1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>
	fields := strings.SplitN(line, " ", 9)
	if len(fields) < 9 {
		return nil
	}

	xy := fields[1]
	path := fields[8]

	entry := &FileEntry{
		Path: path,
	}

	// X = index status, Y = worktree status
	if len(xy) >= 2 {
		indexStatus := xy[0]
		worktreeStatus := xy[1]

		// Staged changes
		if indexStatus != '.' && indexStatus != '?' {
			entry.Staged = true
			entry.Status = FileStatus(string(indexStatus))
		}

		// Unstaged changes
		if worktreeStatus != '.' && worktreeStatus != '?' {
			entry.Unstaged = true
			if !entry.Staged {
				entry.Status = FileStatus(string(worktreeStatus))
			}
		}
	}

	return entry
}

// parseRenamedEntry parses a "2 <XY> ..." line.
func (t *FileTree) parseRenamedEntry(line string) *FileEntry {
	// Format: 2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <X><score> <path>
	fields := strings.SplitN(line, " ", 10)
	if len(fields) < 10 {
		return nil
	}

	xy := fields[1]
	path := fields[9]

	entry := &FileEntry{
		Path:   path,
		Status: StatusRenamed,
		Staged: true,
	}

	// Check if there are also worktree changes
	if len(xy) >= 2 && xy[1] != '.' {
		entry.Unstaged = true
	}

	return entry
}

// parseUnmergedEntry parses a "u ..." line.
func (t *FileTree) parseUnmergedEntry(line string) *FileEntry {
	// Format: u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>
	fields := strings.SplitN(line, " ", 11)
	if len(fields) < 11 {
		return nil
	}

	return &FileEntry{
		Path:     fields[10],
		Status:   StatusUnmerged,
		Unstaged: true,
	}
}

// addEntry adds an entry to the appropriate list.
func (t *FileTree) addEntry(entry *FileEntry) {
	if entry.Staged {
		t.Staged = append(t.Staged, entry)
	}
	if entry.Unstaged && !entry.Staged {
		t.Modified = append(t.Modified, entry)
	} else if entry.Unstaged && entry.Staged {
		// File has both staged and unstaged changes
		// Add a copy to modified list
		modEntry := &FileEntry{
			Path:     entry.Path,
			Status:   entry.Status,
			Unstaged: true,
		}
		t.Modified = append(t.Modified, modEntry)
	}
}

// loadDiffStats loads +/- counts for all files.
func (t *FileTree) loadDiffStats() error {
	// Get stats for staged changes
	if err := t.loadDiffStatsFor(true); err != nil {
		return err
	}

	// Get stats for unstaged changes
	return t.loadDiffStatsFor(false)
}

// loadDiffStatsFor loads diff stats for staged or unstaged changes.
func (t *FileTree) loadDiffStatsFor(staged bool) error {
	args := []string{"diff", "--numstat"}
	if staged {
		args = append(args, "--cached")
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = t.workDir
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	// Parse numstat output: <additions>\t<deletions>\t<path>
	scanner := bufio.NewScanner(bytes.NewReader(output))
	re := regexp.MustCompile(`^(\d+|-)\t(\d+|-)\t(.+)$`)

	for scanner.Scan() {
		matches := re.FindStringSubmatch(scanner.Text())
		if len(matches) != 4 {
			continue
		}

		additions, _ := strconv.Atoi(matches[1])
		deletions, _ := strconv.Atoi(matches[2])
		path := matches[3]

		// Handle renamed files (path\told_path)
		if idx := strings.Index(path, "\t"); idx > 0 {
			path = path[:idx]
		}

		// Find and update the entry
		entries := t.Modified
		if staged {
			entries = t.Staged
		}
		for _, e := range entries {
			if e.Path == path || filepath.Base(e.Path) == filepath.Base(path) {
				e.DiffStats = DiffStats{
					Additions: additions,
					Deletions: deletions,
				}
				break
			}
		}
	}

	return nil
}

// TotalCount returns the total number of changed files.
func (t *FileTree) TotalCount() int {
	return len(t.Staged) + len(t.Modified) + len(t.Untracked)
}

// groupUntrackedFolders groups untracked files that share a common top-level directory.
// Files within a folder are collapsed into a single folder entry with Children.
func (t *FileTree) groupUntrackedFolders() {
	if len(t.Untracked) == 0 {
		return
	}

	// Group files by their top-level directory
	folderMap := make(map[string][]*FileEntry)
	var standaloneFiles []*FileEntry

	for _, entry := range t.Untracked {
		// Check if file is in a subdirectory
		idx := strings.Index(entry.Path, "/")
		if idx > 0 {
			folder := entry.Path[:idx]
			folderMap[folder] = append(folderMap[folder], entry)
		} else {
			standaloneFiles = append(standaloneFiles, entry)
		}
	}

	// Build new untracked list with folder entries
	var newUntracked []*FileEntry

	// Add folder entries (only for folders with multiple files or deep nesting)
	folders := make([]string, 0, len(folderMap))
	for folder := range folderMap {
		folders = append(folders, folder)
	}
	sort.Strings(folders)

	for _, folder := range folders {
		files := folderMap[folder]
		if len(files) >= 2 {
			// Create a folder entry with children
			folderEntry := &FileEntry{
				Path:       folder + "/",
				Status:     StatusUntracked,
				Unstaged:   true,
				IsFolder:   true,
				IsExpanded: false,
				Children:   files,
			}
			newUntracked = append(newUntracked, folderEntry)
		} else {
			// Single file in folder - keep as standalone
			newUntracked = append(newUntracked, files...)
		}
	}

	// Add standalone files
	newUntracked = append(newUntracked, standaloneFiles...)

	// Sort by path
	sort.Slice(newUntracked, func(i, j int) bool {
		return newUntracked[i].Path < newUntracked[j].Path
	})

	t.Untracked = newUntracked
}

// Summary returns a summary string like "2 staged, 3 modified".
func (t *FileTree) Summary() string {
	var parts []string
	if len(t.Staged) > 0 {
		parts = append(parts, strconv.Itoa(len(t.Staged))+" staged")
	}
	if len(t.Modified) > 0 {
		parts = append(parts, strconv.Itoa(len(t.Modified))+" modified")
	}
	if len(t.Untracked) > 0 {
		parts = append(parts, strconv.Itoa(len(t.Untracked))+" untracked")
	}
	if len(parts) == 0 {
		return "clean"
	}
	return strings.Join(parts, ", ")
}

// AllEntries returns all entries in display order.
// Folder entries are included, and if expanded, their children follow.
func (t *FileTree) AllEntries() []*FileEntry {
	var all []*FileEntry
	all = append(all, t.Staged...)
	all = append(all, t.Modified...)

	// For untracked, handle folder expansion
	for _, entry := range t.Untracked {
		all = append(all, entry)
		if entry.IsFolder && entry.IsExpanded {
			all = append(all, entry.Children...)
		}
	}
	return all
}

// StageFile stages a file.
func (t *FileTree) StageFile(path string) error {
	cmd := exec.Command("git", "add", "--", path)
	cmd.Dir = t.workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// UnstageFile unstages a file.
func (t *FileTree) UnstageFile(path string) error {
	cmd := exec.Command("git", "restore", "--staged", "--", path)
	cmd.Dir = t.workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// StageAll stages all modified and untracked files.
func (t *FileTree) StageAll() error {
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = t.workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// UnstageAll unstages all staged files.
func (t *FileTree) UnstageAll() error {
	cmd := exec.Command("git", "reset", "HEAD")
	cmd.Dir = t.workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// HasStagedFiles returns true if there are any staged files.
func (t *FileTree) HasStagedFiles() bool {
	return len(t.Staged) > 0
}

// StagedStats returns total additions and deletions for staged files.
func (t *FileTree) StagedStats() (additions, deletions int) {
	for _, e := range t.Staged {
		additions += e.DiffStats.Additions
		deletions += e.DiffStats.Deletions
	}
	return
}

// parseCommitHash extracts the commit hash from git commit output.
// Format: "[branch hash] message"
func parseCommitHash(output string) string {
	lines := strings.Split(output, "\n")
	if len(lines) > 0 {
		re := regexp.MustCompile(`\[[\w/-]+ ([a-f0-9]+)\]`)
		matches := re.FindStringSubmatch(lines[0])
		if len(matches) > 1 {
			return matches[1]
		}
	}
	return ""
}

// ExecuteCommit executes a git commit with the given message.
// Returns the commit hash on success or an error with git output on failure.
func ExecuteCommit(workDir, message string) (string, error) {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", &CommitError{Output: string(output), Err: err}
	}
	return parseCommitHash(string(output)), nil
}

// ExecuteAmend executes a git commit --amend with the given message.
func ExecuteAmend(workDir, message string) (string, error) {
	cmd := exec.Command("git", "commit", "--amend", "-m", message)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", &CommitError{Output: string(output), Err: err}
	}
	return parseCommitHash(string(output)), nil
}

// getLastCommitMessage returns the message of the most recent commit.
func getLastCommitMessage(workDir string) string {
	cmd := exec.Command("git", "log", "-1", "--format=%B")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(output), "\n")
}

// CommitError wraps a git commit error with its output.
type CommitError struct {
	Output string
	Err    error
}

func (e *CommitError) Error() string {
	return strings.TrimSpace(e.Output)
}

// DiscardModified discards unstaged changes to a modified file.
func DiscardModified(workDir, path string) error {
	cmd := exec.Command("git", "restore", "--", path)
	cmd.Dir = workDir
	return cmd.Run()
}

// DiscardStaged discards staged changes to a file (unstages and restores).
func DiscardStaged(workDir, path string) error {
	// First unstage
	cmd := exec.Command("git", "restore", "--staged", "--", path)
	cmd.Dir = workDir
	if err := cmd.Run(); err != nil {
		return err
	}
	// Then restore working tree
	cmd = exec.Command("git", "restore", "--", path)
	cmd.Dir = workDir
	return cmd.Run()
}

// DiscardUntracked removes an untracked file.
func DiscardUntracked(workDir, path string) error {
	fullPath := filepath.Join(workDir, path)
	return os.Remove(fullPath)
}
