package gitstatus

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Commit represents a git commit.
type Commit struct {
	Hash         string
	ShortHash    string
	Author       string
	AuthorEmail  string
	Date         time.Time
	Subject      string
	Body         string
	Files        []CommitFile
	Stats        CommitStats
	Pushed       bool     // Whether this commit has been pushed to upstream
	ParentHashes []string // Parent commit hashes (empty for root commits)
	IsMerge      bool     // True if commit has multiple parents
}

// CommitFile represents a file changed in a commit.
type CommitFile struct {
	Path      string
	OldPath   string // For renames
	Status    FileStatus
	Additions int
	Deletions int
}

// CommitStats holds aggregate commit statistics.
type CommitStats struct {
	FilesChanged int
	Additions    int
	Deletions    int
}

// GetCommitHistory fetches recent commits.
func GetCommitHistory(workDir string, limit int) ([]*Commit, error) {
	// Format: hash\x00shorthash\x00author\x00email\x00timestamp\x00subject\x00parents
	format := "%H%x00%h%x00%an%x00%ae%x00%at%x00%s%x00%P"
	args := []string{"log", "--format=" + format, "-n", strconv.Itoa(limit)}

	cmd := gitReadOnly(args...)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var commits []*Commit
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 6 {
			continue
		}

		timestamp, _ := strconv.ParseInt(parts[4], 10, 64)

		// Parse parent hashes (space-separated in parts[6])
		var parents []string
		if len(parts) >= 7 && parts[6] != "" {
			parents = strings.Split(parts[6], " ")
		}

		commits = append(commits, &Commit{
			Hash:         parts[0],
			ShortHash:    parts[1],
			Author:       parts[2],
			AuthorEmail:  parts[3],
			Date:         time.Unix(timestamp, 0),
			Subject:      parts[5],
			ParentHashes: parents,
			IsMerge:      len(parents) > 1,
		})
	}

	return commits, nil
}

// GetCommitDetail fetches full commit info including file list.
func GetCommitDetail(workDir, hash string) (*Commit, error) {
	// Get commit metadata (%P = parent hashes for merge detection)
	format := "%H%n%h%n%an%n%ae%n%at%n%P%n%s%n%b"
	cmd := gitReadOnly("show", "--format="+format, "-s", hash)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.SplitN(string(output), "\n", 8)
	if len(lines) < 7 {
		return nil, nil
	}

	timestamp, _ := strconv.ParseInt(strings.TrimSpace(lines[4]), 10, 64)

	// Parse parent hashes (space-separated, empty for root commits)
	var parents []string
	if raw := strings.TrimSpace(lines[5]); raw != "" {
		parents = strings.Fields(raw)
	}

	commit := &Commit{
		Hash:         strings.TrimSpace(lines[0]),
		ShortHash:    strings.TrimSpace(lines[1]),
		Author:       strings.TrimSpace(lines[2]),
		AuthorEmail:  strings.TrimSpace(lines[3]),
		Date:         time.Unix(timestamp, 0),
		Subject:      strings.TrimSpace(lines[6]),
		ParentHashes: parents,
		IsMerge:      len(parents) > 1,
	}
	if len(lines) > 7 {
		commit.Body = strings.TrimSpace(lines[7])
	}

	// Get file stats — for merge commits, diff against first parent to avoid empty combined diff
	if commit.IsMerge && len(commit.ParentHashes) > 0 {
		cmd = gitReadOnly("diff", "--numstat", commit.ParentHashes[0], hash)
	} else {
		cmd = gitReadOnly("show", "--numstat", "--format=", hash)
	}
	cmd.Dir = workDir
	output, err = cmd.Output()
	if err != nil {
		return commit, nil // Return commit without files
	}

	fileLines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range fileLines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		var adds, dels int
		if parts[0] != "-" {
			adds, _ = strconv.Atoi(parts[0])
		}
		if parts[1] != "-" {
			dels, _ = strconv.Atoi(parts[1])
		}

		// Handle renames: path format is "oldpath => newpath" or "{old => new}path"
		path := parts[2]
		oldPath := ""
		status := StatusModified

		if strings.Contains(path, " => ") {
			status = StatusRenamed
			// Could be "a => b" or "{a => b}/path"
			if strings.Contains(path, "{") {
				// Format: prefix/{old => new}/suffix
				// For now, just take the whole thing
				oldPath = path
			} else {
				pathParts := strings.Split(path, " => ")
				if len(pathParts) == 2 {
					oldPath = pathParts[0]
					path = pathParts[1]
				}
			}
		}

		commit.Files = append(commit.Files, CommitFile{
			Path:      path,
			OldPath:   oldPath,
			Status:    status,
			Additions: adds,
			Deletions: dels,
		})

		commit.Stats.FilesChanged++
		commit.Stats.Additions += adds
		commit.Stats.Deletions += dels
	}

	return commit, nil
}

// GetCommitDiff returns the diff for a specific file in a commit.
// For merge commits, parentHash should be the first parent so we diff against
// it instead of using git show's combined diff (which is empty for clean merges).
func GetCommitDiff(workDir, hash, path string, parentHash string) (string, error) {
	var args []string
	if parentHash != "" {
		args = []string{"diff", parentHash, hash, "--", path}
	} else {
		args = []string{"show", hash, "--", path}
	}

	cmd := gitReadOnly(args...)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return string(output), nil
			}
		}
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// GetCommitFullDiff returns the full diff for a commit.
func GetCommitFullDiff(workDir, hash string) (string, error) {
	cmd := gitReadOnly("show", hash)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// PopulatePushStatus updates the Pushed field for a list of commits
// based on the provided push status.
func PopulatePushStatus(commits []*Commit, pushStatus *PushStatus) {
	if pushStatus == nil {
		return
	}
	for _, c := range commits {
		c.Pushed = pushStatus.IsCommitPushed(c.Hash)
	}
}

// GetCommitHistoryWithPushStatus fetches commits and populates push status.
func GetCommitHistoryWithPushStatus(workDir string, limit int) ([]*Commit, *PushStatus, error) {
	commits, err := GetCommitHistory(workDir, limit)
	if err != nil {
		return nil, nil, err
	}

	pushStatus := GetPushStatus(workDir)
	PopulatePushStatus(commits, pushStatus)

	return commits, pushStatus, nil
}

// GetCommitHistoryWithOffset fetches commits starting from skip, up to limit.
// Uses git log --skip=N to paginate through history.
func GetCommitHistoryWithOffset(workDir string, limit, skip int) ([]*Commit, error) {
	format := "%H%x00%h%x00%an%x00%ae%x00%at%x00%s%x00%P"
	args := []string{"log", "--format=" + format, "-n", strconv.Itoa(limit), "--skip", strconv.Itoa(skip)}

	cmd := gitReadOnly(args...)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var commits []*Commit
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 6 {
			continue
		}

		timestamp, _ := strconv.ParseInt(parts[4], 10, 64)

		// Parse parent hashes (space-separated in parts[6])
		var parents []string
		if len(parts) >= 7 && parts[6] != "" {
			parents = strings.Split(parts[6], " ")
		}

		commits = append(commits, &Commit{
			Hash:         parts[0],
			ShortHash:    parts[1],
			Author:       parts[2],
			AuthorEmail:  parts[3],
			Date:         time.Unix(timestamp, 0),
			Subject:      parts[5],
			ParentHashes: parents,
			IsMerge:      len(parents) > 1,
		})
	}

	return commits, nil
}

// GetCommitHistoryWithPushStatusOffset fetches commits with offset and populates push status.
func GetCommitHistoryWithPushStatusOffset(workDir string, limit, skip int) ([]*Commit, *PushStatus, error) {
	commits, err := GetCommitHistoryWithOffset(workDir, limit, skip)
	if err != nil {
		return nil, nil, err
	}

	pushStatus := GetPushStatus(workDir)
	PopulatePushStatus(commits, pushStatus)

	return commits, pushStatus, nil
}

// HistoryFilterOpts holds options for filtered commit queries.
type HistoryFilterOpts struct {
	Author string // Filter by author (--author)
	Path   string // Filter by file path (-- <path>)
	Limit  int
	Skip   int
}

// GetCommitHistoryFiltered fetches commits with filters applied.
func GetCommitHistoryFiltered(workDir string, opts HistoryFilterOpts) ([]*Commit, error) {
	format := "%H%x00%h%x00%an%x00%ae%x00%at%x00%s%x00%P"
	args := []string{"log", "--format=" + format}

	if opts.Author != "" {
		args = append(args, "--author="+opts.Author)
	}

	if opts.Limit > 0 {
		args = append(args, "-n", strconv.Itoa(opts.Limit))
	}
	if opts.Skip > 0 {
		args = append(args, "--skip", strconv.Itoa(opts.Skip))
	}

	if opts.Path != "" {
		args = append(args, "--", opts.Path)
	}

	cmd := gitReadOnly(args...)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var commits []*Commit
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 6 {
			continue
		}

		timestamp, _ := strconv.ParseInt(parts[4], 10, 64)

		// Parse parent hashes (space-separated in parts[6])
		var parents []string
		if len(parts) >= 7 && parts[6] != "" {
			parents = strings.Split(parts[6], " ")
		}

		commits = append(commits, &Commit{
			Hash:         parts[0],
			ShortHash:    parts[1],
			Author:       parts[2],
			AuthorEmail:  parts[3],
			Date:         time.Unix(timestamp, 0),
			Subject:      parts[5],
			ParentHashes: parents,
			IsMerge:      len(parents) > 1,
		})
	}

	return commits, nil
}

// GetCommitHistoryFilteredWithPushStatus fetches filtered commits and populates push status.
func GetCommitHistoryFilteredWithPushStatus(workDir string, opts HistoryFilterOpts) ([]*Commit, *PushStatus, error) {
	commits, err := GetCommitHistoryFiltered(workDir, opts)
	if err != nil {
		return nil, nil, err
	}

	pushStatus := GetPushStatus(workDir)
	PopulatePushStatus(commits, pushStatus)

	return commits, pushStatus, nil
}

// RelativeTime returns a human-readable relative time string.
func RelativeTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return strconv.Itoa(mins) + " mins ago"
	case diff < 24*time.Hour:
		hrs := int(diff.Hours())
		if hrs == 1 {
			return "1 hour ago"
		}
		return strconv.Itoa(hrs) + " hours ago"
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return strconv.Itoa(days) + " days ago"
	case diff < 30*24*time.Hour:
		weeks := int(diff.Hours() / 24 / 7)
		if weeks == 1 {
			return "1 week ago"
		}
		return strconv.Itoa(weeks) + " weeks ago"
	case diff < 365*24*time.Hour:
		months := int(diff.Hours() / 24 / 30)
		if months == 1 {
			return "1 month ago"
		}
		return strconv.Itoa(months) + " months ago"
	default:
		years := int(diff.Hours() / 24 / 365)
		if years == 1 {
			return "1 year ago"
		}
		return strconv.Itoa(years) + " years ago"
	}
}
