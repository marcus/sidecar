package filebrowser

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	projectSearchMaxResults = 1000           // Max total matches to display
	projectSearchTimeout    = 30 * time.Second // Max time for search
)

// ProjectSearchState holds the state for project-wide search.
type ProjectSearchState struct {
	Query   string
	Results []SearchFileResult

	// Search options (toggle with keyboard shortcuts)
	UseRegex      bool
	CaseSensitive bool
	WholeWord     bool

	// UI state
	Cursor       int  // Index in flattened results (files + matches)
	ScrollOffset int  // For scrolling
	IsSearching  bool // True while ripgrep is running
	Error        string

	// For future: multiple search tabs
	TabID int
}

// SearchFileResult represents a file with search matches.
type SearchFileResult struct {
	Path      string
	Matches   []SearchMatch
	Collapsed bool
}

// SearchMatch represents a single match within a file.
type SearchMatch struct {
	LineNo    int    // 1-indexed line number
	LineText  string // Full line content
	ColStart  int    // Match start column (0-indexed)
	ColEnd    int    // Match end column (0-indexed)
}

// rgMessage represents a ripgrep JSON message.
type rgMessage struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber  int `json:"line_number"`
		Submatches  []struct {
			Match struct {
				Text string `json:"text"`
			} `json:"match"`
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

// ProjectSearchResultsMsg contains results from a search.
type ProjectSearchResultsMsg struct {
	Results []SearchFileResult
	Error   error
}

// NewProjectSearchState creates a new search state.
func NewProjectSearchState() *ProjectSearchState {
	return &ProjectSearchState{
		Cursor:  0,
		Results: make([]SearchFileResult, 0),
	}
}

// TotalMatches returns the total number of matches across all files.
func (s *ProjectSearchState) TotalMatches() int {
	count := 0
	for _, f := range s.Results {
		count += len(f.Matches)
	}
	return count
}

// FileCount returns the number of files with matches.
func (s *ProjectSearchState) FileCount() int {
	return len(s.Results)
}

// FlatLen returns the length of the flattened results list.
// Each file is 1 item, plus its matches if not collapsed.
func (s *ProjectSearchState) FlatLen() int {
	count := 0
	for _, f := range s.Results {
		count++ // File header
		if !f.Collapsed {
			count += len(f.Matches)
		}
	}
	return count
}

// FlatItem returns the item at the given flat index.
// Returns (fileIndex, matchIndex, isFile).
// matchIndex is -1 if this is a file header.
func (s *ProjectSearchState) FlatItem(idx int) (fileIdx int, matchIdx int, isFile bool) {
	pos := 0
	for fi, f := range s.Results {
		if pos == idx {
			return fi, -1, true
		}
		pos++
		if !f.Collapsed {
			for mi := range f.Matches {
				if pos == idx {
					return fi, mi, false
				}
				pos++
			}
		}
	}
	return -1, -1, false
}

// ToggleFileCollapse toggles the collapsed state of the file at cursor.
func (s *ProjectSearchState) ToggleFileCollapse() {
	fileIdx, _, isFile := s.FlatItem(s.Cursor)
	if fileIdx >= 0 && isFile {
		s.Results[fileIdx].Collapsed = !s.Results[fileIdx].Collapsed
	}
}

// GetSelectedFile returns the currently selected file path and line number.
// If a match is selected, returns file path and line number.
// If a file header is selected, returns file path and line 0.
func (s *ProjectSearchState) GetSelectedFile() (path string, lineNo int) {
	fileIdx, matchIdx, isFile := s.FlatItem(s.Cursor)
	if fileIdx < 0 || fileIdx >= len(s.Results) {
		return "", 0
	}

	file := s.Results[fileIdx]
	if isFile {
		return file.Path, 0
	}

	if matchIdx >= 0 && matchIdx < len(file.Matches) {
		return file.Path, file.Matches[matchIdx].LineNo
	}

	return file.Path, 0
}

// RunProjectSearch executes ripgrep and returns results.
func RunProjectSearch(workDir string, state *ProjectSearchState) tea.Cmd {
	return func() tea.Msg {
		if state.Query == "" {
			return ProjectSearchResultsMsg{Results: nil}
		}

		ctx, cancel := context.WithTimeout(context.Background(), projectSearchTimeout)
		defer cancel()

		args := buildRipgrepArgs(state)
		cmd := exec.CommandContext(ctx, "rg", args...)
		cmd.Dir = workDir

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return ProjectSearchResultsMsg{Error: err}
		}

		if err := cmd.Start(); err != nil {
			// Check if rg is not installed
			if strings.Contains(err.Error(), "executable file not found") {
				return ProjectSearchResultsMsg{Error: &ripgrepNotFoundError{}}
			}
			return ProjectSearchResultsMsg{Error: err}
		}

		results := parseRipgrepOutput(stdout, projectSearchMaxResults)

		// Wait for command to finish (ignore exit code - rg returns 1 for no matches)
		_ = cmd.Wait()

		return ProjectSearchResultsMsg{Results: results}
	}
}

// buildRipgrepArgs constructs the ripgrep command arguments.
func buildRipgrepArgs(state *ProjectSearchState) []string {
	args := []string{
		"--json",           // JSON output for structured parsing
		"--max-count=100",  // Limit matches per file
		"--max-filesize=1M", // Skip very large files
	}

	if !state.CaseSensitive {
		args = append(args, "--ignore-case")
	}

	if state.WholeWord {
		args = append(args, "--word-regexp")
	}

	if !state.UseRegex {
		args = append(args, "--fixed-strings")
	}

	args = append(args, "--", state.Query)

	return args
}

// parseRipgrepOutput reads ripgrep JSON output and builds results.
func parseRipgrepOutput(reader interface{ Read([]byte) (int, error) }, maxMatches int) []SearchFileResult {
	scanner := bufio.NewScanner(reader)
	// Increase buffer size for long lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	fileMap := make(map[string]*SearchFileResult)
	var fileOrder []string
	totalMatches := 0

	for scanner.Scan() && totalMatches < maxMatches {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg rgMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if msg.Type != "match" {
			continue
		}

		path := msg.Data.Path.Text
		lineNo := msg.Data.LineNumber
		lineText := strings.TrimRight(msg.Data.Lines.Text, "\n\r")

		// Get or create file result
		file, exists := fileMap[path]
		if !exists {
			file = &SearchFileResult{
				Path:    path,
				Matches: make([]SearchMatch, 0),
			}
			fileMap[path] = file
			fileOrder = append(fileOrder, path)
		}

		// Add matches for each submatch
		for _, sm := range msg.Data.Submatches {
			if totalMatches >= maxMatches {
				break
			}
			file.Matches = append(file.Matches, SearchMatch{
				LineNo:   lineNo,
				LineText: lineText,
				ColStart: sm.Start,
				ColEnd:   sm.End,
			})
			totalMatches++
		}
	}

	// Build ordered results
	results := make([]SearchFileResult, 0, len(fileOrder))
	for _, path := range fileOrder {
		results = append(results, *fileMap[path])
	}

	return results
}

// ripgrepNotFoundError indicates rg is not installed.
type ripgrepNotFoundError struct{}

func (e *ripgrepNotFoundError) Error() string {
	return "ripgrep (rg) not found - install with: brew install ripgrep"
}
