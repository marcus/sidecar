package filebrowser

import (
	"bufio"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// BlameLine represents a single line in git blame output.
type BlameLine struct {
	CommitHash string
	Author     string
	AuthorTime time.Time
	LineNo     int
	Content    string
}

// BlameState holds the state for blame view.
type BlameState struct {
	Lines        []BlameLine
	Cursor       int
	ScrollOffset int
	FilePath     string
	IsLoading    bool
	Error        error
}

// BlameLoadedMsg is sent when blame data is loaded.
type BlameLoadedMsg struct {
	Epoch uint64 // Epoch when request was issued (for stale detection)
	Lines []BlameLine
	Error error
}

// GetEpoch implements plugin.EpochMessage.
func (m BlameLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// RunGitBlame runs git blame and returns the parsed output.
func RunGitBlame(workDir, filePath string, epoch uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "git", "blame", "--line-porcelain", "--", filePath)
		cmd.Dir = workDir
		output, err := cmd.Output()
		if err != nil {
			return BlameLoadedMsg{Epoch: epoch, Error: err}
		}

		lines := parseBlameOutput(string(output))
		return BlameLoadedMsg{Epoch: epoch, Lines: lines}
	}
}

// parseBlameOutput parses git blame --line-porcelain output.
// Format:
//
//	<hash> <orig-line> <final-line> [<num-lines>]
//	author <name>
//	author-mail <email>
//	author-time <timestamp>
//	author-tz <tz>
//	committer <name>
//	committer-mail <email>
//	committer-time <timestamp>
//	committer-tz <tz>
//	summary <message>
//	[previous <hash> <filename>]
//	filename <filename>
//	\t<content>
func parseBlameOutput(output string) []BlameLine {
	var lines []BlameLine
	scanner := bufio.NewScanner(strings.NewReader(output))

	var current BlameLine
	inEntry := false

	for scanner.Scan() {
		line := scanner.Text()

		// Line starting with hash indicates new entry
		if len(line) >= 40 && isHexString(line[:40]) {
			if inEntry {
				// Finalize previous entry if we have one
				lines = append(lines, current)
			}
			// Parse header: <hash> <orig-line> <final-line> [<num-lines>]
			parts := strings.Fields(line)
			current = BlameLine{
				CommitHash: parts[0][:8], // Short hash
			}
			if len(parts) >= 3 {
				current.LineNo, _ = strconv.Atoi(parts[2])
			}
			inEntry = true
			continue
		}

		if !inEntry {
			continue
		}

		// Parse metadata fields
		switch {
		case strings.HasPrefix(line, "author "):
			current.Author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			ts, _ := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64)
			current.AuthorTime = time.Unix(ts, 0)
		case strings.HasPrefix(line, "\t"):
			// Content line (starts with tab)
			current.Content = line[1:] // Remove leading tab
		}
	}

	// Don't forget the last entry
	if inEntry {
		lines = append(lines, current)
	}

	return lines
}

// isHexString checks if a string contains only hex characters.
func isHexString(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// RelativeTime formats a time as a relative duration string.
func RelativeTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return strconv.Itoa(m) + " mins ago"
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return strconv.Itoa(h) + " hours ago"
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return strconv.Itoa(days) + " days ago"
	case d < 30*24*time.Hour:
		weeks := int(d.Hours() / 24 / 7)
		if weeks == 1 {
			return "1 week ago"
		}
		return strconv.Itoa(weeks) + " weeks ago"
	case d < 365*24*time.Hour:
		months := int(d.Hours() / 24 / 30)
		if months == 1 {
			return "1 month ago"
		}
		return strconv.Itoa(months) + " months ago"
	default:
		years := int(d.Hours() / 24 / 365)
		if years == 1 {
			return "1 year ago"
		}
		return strconv.Itoa(years) + " years ago"
	}
}
