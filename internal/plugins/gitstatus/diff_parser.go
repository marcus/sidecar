package gitstatus

import (
	"regexp"
	"strconv"
	"strings"
)

// LineType represents the type of a diff line.
type LineType int

const (
	LineContext LineType = iota
	LineAdd
	LineRemove
)

// WordSegment represents a segment of text with word-level diff highlighting.
type WordSegment struct {
	Text     string
	IsChange bool
}

// DiffLine represents a single line in a diff.
type DiffLine struct {
	Type      LineType
	OldLineNo int // 0 means not applicable
	NewLineNo int // 0 means not applicable
	Content   string
	WordDiff  []WordSegment
}

// Hunk represents a diff hunk.
type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Header   string
	Lines    []DiffLine
}

// ParsedDiff represents a fully parsed diff.
type ParsedDiff struct {
	OldFile string
	NewFile string
	Binary  bool
	Hunks   []Hunk
}

// FileDiffInfo holds a parsed diff with rendering position info.
type FileDiffInfo struct {
	Diff       *ParsedDiff
	StartLine  int // Line position where this file starts in rendered output
	EndLine    int // Line position where this file ends
	Additions  int // Number of added lines
	Deletions  int // Number of deleted lines
}

// MultiFileDiff holds multiple file diffs with navigation info.
type MultiFileDiff struct {
	Files []FileDiffInfo
}

// ParseMultiFileDiff parses a git diff output containing multiple files.
func ParseMultiFileDiff(diff string) *MultiFileDiff {
	result := &MultiFileDiff{}

	// Split diff into individual file diffs
	fileDiffs := splitIntoFileDiffs(diff)

	for _, fileDiff := range fileDiffs {
		parsed, err := ParseUnifiedDiff(fileDiff)
		if err != nil || parsed == nil {
			continue
		}

		// Count additions and deletions
		additions, deletions := 0, 0
		for _, hunk := range parsed.Hunks {
			for _, line := range hunk.Lines {
				switch line.Type {
				case LineAdd:
					additions++
				case LineRemove:
					deletions++
				}
			}
		}

		result.Files = append(result.Files, FileDiffInfo{
			Diff:      parsed,
			Additions: additions,
			Deletions: deletions,
		})
	}

	return result
}

// splitIntoFileDiffs splits a multi-file diff into individual file diffs.
func splitIntoFileDiffs(diff string) []string {
	var fileDiffs []string
	var current strings.Builder

	lines := strings.Split(diff, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Check for start of new file diff
		if strings.HasPrefix(line, "diff --git ") {
			// Save previous file diff if exists
			if current.Len() > 0 {
				fileDiffs = append(fileDiffs, current.String())
				current.Reset()
			}
		}

		current.WriteString(line)
		current.WriteString("\n")
	}

	// Don't forget the last file
	if current.Len() > 0 {
		fileDiffs = append(fileDiffs, current.String())
	}

	return fileDiffs
}

// FileName returns the display filename (prefers NewFile, falls back to OldFile).
func (f *FileDiffInfo) FileName() string {
	if f.Diff.NewFile != "" && f.Diff.NewFile != "/dev/null" {
		return f.Diff.NewFile
	}
	if f.Diff.OldFile != "" && f.Diff.OldFile != "/dev/null" {
		return f.Diff.OldFile
	}
	return "unknown"
}

// ChangeStats returns a formatted string like "+10/-5".
func (f *FileDiffInfo) ChangeStats() string {
	return "+" + itoa(f.Additions) + "/-" + itoa(f.Deletions)
}

// itoa is a simple int to string conversion to avoid fmt import.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

var (
	hunkHeaderRegex = regexp.MustCompile(`^@@\s*-(\d+)(?:,(\d+))?\s*\+(\d+)(?:,(\d+))?\s*@@(.*)$`)
)

// ParseUnifiedDiff parses a unified diff format string.
func ParseUnifiedDiff(diff string) (*ParsedDiff, error) {
	lines := strings.Split(diff, "\n")
	parsed := &ParsedDiff{}

	var currentHunk *Hunk
	oldLineNo := 0
	newLineNo := 0

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "Binary files"):
			parsed.Binary = true
			return parsed, nil

		case strings.HasPrefix(line, "--- "):
			parsed.OldFile = strings.TrimPrefix(line, "--- ")
			if strings.HasPrefix(parsed.OldFile, "a/") {
				parsed.OldFile = parsed.OldFile[2:]
			}

		case strings.HasPrefix(line, "+++ "):
			parsed.NewFile = strings.TrimPrefix(line, "+++ ")
			if strings.HasPrefix(parsed.NewFile, "b/") {
				parsed.NewFile = parsed.NewFile[2:]
			}

		case strings.HasPrefix(line, "@@"):
			match := hunkHeaderRegex.FindStringSubmatch(line)
			if match != nil {
				oldStart, _ := strconv.Atoi(match[1])
				oldCount := 1
				if match[2] != "" {
					oldCount, _ = strconv.Atoi(match[2])
				}
				newStart, _ := strconv.Atoi(match[3])
				newCount := 1
				if match[4] != "" {
					newCount, _ = strconv.Atoi(match[4])
				}

				currentHunk = &Hunk{
					OldStart: oldStart,
					OldCount: oldCount,
					NewStart: newStart,
					NewCount: newCount,
					Header:   match[5],
				}
				parsed.Hunks = append(parsed.Hunks, *currentHunk)
				currentHunk = &parsed.Hunks[len(parsed.Hunks)-1]
				oldLineNo = oldStart
				newLineNo = newStart
			}

		case currentHunk != nil:
			if len(line) == 0 {
				// Empty context line
				diffLine := DiffLine{
					Type:      LineContext,
					OldLineNo: oldLineNo,
					NewLineNo: newLineNo,
					Content:   "",
				}
				currentHunk.Lines = append(currentHunk.Lines, diffLine)
				oldLineNo++
				newLineNo++
				continue
			}

			prefix := line[0]
			content := ""
			if len(line) > 1 {
				content = line[1:]
			}

			switch prefix {
			case '+':
				diffLine := DiffLine{
					Type:      LineAdd,
					OldLineNo: 0,
					NewLineNo: newLineNo,
					Content:   content,
				}
				currentHunk.Lines = append(currentHunk.Lines, diffLine)
				newLineNo++

			case '-':
				diffLine := DiffLine{
					Type:      LineRemove,
					OldLineNo: oldLineNo,
					NewLineNo: 0,
					Content:   content,
				}
				currentHunk.Lines = append(currentHunk.Lines, diffLine)
				oldLineNo++

			case ' ':
				diffLine := DiffLine{
					Type:      LineContext,
					OldLineNo: oldLineNo,
					NewLineNo: newLineNo,
					Content:   content,
				}
				currentHunk.Lines = append(currentHunk.Lines, diffLine)
				oldLineNo++
				newLineNo++

			case '\\':
				// "\ No newline at end of file" - skip

			default:
				// Treat as context if unrecognized
				diffLine := DiffLine{
					Type:      LineContext,
					OldLineNo: oldLineNo,
					NewLineNo: newLineNo,
					Content:   line,
				}
				currentHunk.Lines = append(currentHunk.Lines, diffLine)
				oldLineNo++
				newLineNo++
			}
		}
	}

	// Compute word-level diffs for consecutive add/remove pairs
	for i := range parsed.Hunks {
		computeWordDiffs(&parsed.Hunks[i])
	}

	return parsed, nil
}

// computeWordDiffs computes word-level diffs for a hunk.
func computeWordDiffs(hunk *Hunk) {
	lines := hunk.Lines
	for i := 0; i < len(lines); i++ {
		if lines[i].Type == LineRemove && i+1 < len(lines) && lines[i+1].Type == LineAdd {
			// Found a remove/add pair - compute word diff
			oldWords := tokenize(lines[i].Content)
			newWords := tokenize(lines[i+1].Content)

			// Simple word-by-word comparison
			lines[i].WordDiff = computeWordSegments(oldWords, newWords, false)
			lines[i+1].WordDiff = computeWordSegments(newWords, oldWords, true)
			i++ // Skip the add line
		}
	}
}

// tokenize splits a line into words and whitespace tokens.
func tokenize(s string) []string {
	var tokens []string
	var current strings.Builder
	inWord := false

	for _, r := range s {
		isWord := r != ' ' && r != '\t'
		if isWord {
			if !inWord && current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			current.WriteRune(r)
			inWord = true
		} else {
			if inWord && current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			current.WriteRune(r)
			inWord = false
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// computeWordSegments computes word segments by comparing tokens.
func computeWordSegments(source, target []string, isAdd bool) []WordSegment {
	if len(source) == 0 {
		return nil
	}

	// Build a set of target words for quick lookup
	targetSet := make(map[string]bool)
	for _, t := range target {
		targetSet[t] = true
	}

	var segments []WordSegment
	for _, word := range source {
		isChange := !targetSet[word]
		// Whitespace is never highlighted as changed
		if strings.TrimSpace(word) == "" {
			isChange = false
		}
		segments = append(segments, WordSegment{
			Text:     word,
			IsChange: isChange,
		})
	}

	return segments
}

// TotalLines returns the total number of content lines in the diff.
func (p *ParsedDiff) TotalLines() int {
	total := 0
	for _, hunk := range p.Hunks {
		total += len(hunk.Lines) + 1 // +1 for hunk header
	}
	return total
}

// MaxLineNumber returns the maximum line number in the diff.
func (p *ParsedDiff) MaxLineNumber() int {
	max := 0
	for _, hunk := range p.Hunks {
		for _, line := range hunk.Lines {
			if line.OldLineNo > max {
				max = line.OldLineNo
			}
			if line.NewLineNo > max {
				max = line.NewLineNo
			}
		}
	}
	return max
}
