package workspace

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// loadStats returns a command to load git stats for a worktree.
func (p *Plugin) loadStats(path string) tea.Cmd {
	epoch := p.ctx.Epoch // Capture epoch for stale detection
	return func() tea.Msg {
		name := filepath.Base(path)
		stats, err := computeStats(path)
		if err != nil {
			return StatsErrorMsg{WorkspaceName: name, Err: err}
		}
		return StatsLoadedMsg{Epoch: epoch, WorkspaceName: name, Stats: stats}
	}
}

// computeStats calculates git stats for a worktree.
func computeStats(workdir string) (*GitStats, error) {
	stats := &GitStats{}

	// Get diff stats (uncommitted changes)
	if err := getDiffStats(workdir, stats); err != nil {
		return nil, err
	}

	// Get ahead/behind counts (non-fatal: might not have upstream)
	_ = getAheadBehind(workdir, stats)

	return stats, nil
}

// getDiffStats computes additions/deletions from git diff.
func getDiffStats(workdir string, stats *GitStats) error {
	// Use --numstat for reliable parsing of tracked file changes (staged + unstaged)
	cmd := exec.Command("git", "diff", "--numstat", "HEAD")
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		// No HEAD yet or other error, try without HEAD
		cmd = exec.Command("git", "diff", "--numstat")
		cmd.Dir = workdir
		output, _ = cmd.Output()
	}

	// Parse numstat output: "<additions>\t<deletions>\t<path>"
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		// Binary files show "-" for counts
		if adds, err := strconv.Atoi(parts[0]); err == nil {
			stats.Additions += adds
		}
		if dels, err := strconv.Atoi(parts[1]); err == nil {
			stats.Deletions += dels
		}
		stats.FilesChanged++
	}

	// Also count lines in untracked files (these are all additions)
	untrackedCmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	untrackedCmd.Dir = workdir
	untrackedOutput, err := untrackedCmd.Output()
	if err != nil {
		return nil
	}

	var untrackedPaths []string
	for _, p := range strings.Split(strings.TrimSpace(string(untrackedOutput)), "\n") {
		if p != "" {
			untrackedPaths = append(untrackedPaths, p)
		}
	}

	if len(untrackedPaths) == 0 {
		return nil
	}

	// Count lines in untracked files using wc -l in batches
	const batchSize = 500
	for i := 0; i < len(untrackedPaths); i += batchSize {
		end := i + batchSize
		if end > len(untrackedPaths) {
			end = len(untrackedPaths)
		}
		batch := untrackedPaths[i:end]

		args := append([]string{"-l"}, batch...)
		wcCmd := exec.Command("wc", args...)
		wcCmd.Dir = workdir
		wcOutput, err := wcCmd.Output()
		if err != nil {
			continue
		}

		for _, wcLine := range strings.Split(strings.TrimSpace(string(wcOutput)), "\n") {
			wcLine = strings.TrimSpace(wcLine)
			if wcLine == "" {
				continue
			}
			fields := strings.Fields(wcLine)
			if len(fields) < 2 {
				continue
			}
			// Skip the "total" summary line from wc
			if fields[len(fields)-1] == "total" {
				continue
			}
			if count, err := strconv.Atoi(fields[0]); err == nil {
				stats.Additions += count
				stats.FilesChanged++
			}
		}
	}

	return nil
}

// getAheadBehind computes ahead/behind counts from upstream.
func getAheadBehind(workdir string, stats *GitStats) error {
	cmd := exec.Command("git", "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	parts := strings.Fields(strings.TrimSpace(string(output)))
	if len(parts) == 2 {
		if n, err := strconv.Atoi(parts[0]); err == nil {
			stats.Behind = n
		}
		if n, err := strconv.Atoi(parts[1]); err == nil {
			stats.Ahead = n
		}
	}

	return nil
}
