package tty

import (
	"hash/maphash"
	"regexp"
	"strings"
	"sync"
)

// Regexes for cleaning terminal output
var (
	// mouseEscapeRegex matches SGR mouse escape sequences like \x1b[<35;192;47M or \x1b[<0;50;20m
	// These can appear in captured tmux output when applications have mouse mode enabled.
	mouseEscapeRegex = regexp.MustCompile(`\x1b\[<\d+;\d+;\d+[Mm]`)

	// terminalModeRegex matches terminal mode escape sequences
	terminalModeRegex = regexp.MustCompile(`\x1b\[\?(?:1000|1002|1003|1005|1006|1015|2004)[hl]`)

	// partialMouseEscapeRegex matches SGR mouse sequences that lost their ESC prefix.
	// This happens when the ESC byte is consumed by readline/ZLE but the rest of the sequence
	// is printed as literal text in the terminal.
	partialMouseEscapeRegex = regexp.MustCompile(`\[<\d+;\d+;\d+[Mm]`)

	// partialMouseSeqRegex matches SGR mouse sequences that lost their ESC prefix
	// due to split-read timing in terminal input.
	PartialMouseSeqRegex = regexp.MustCompile(`^(\[<\d+;\d+;\d+[Mm])+$`)
)

// OutputBuffer is a thread-safe bounded buffer for terminal output.
// Uses maphash for efficient content change detection to avoid duplicate processing.
type OutputBuffer struct {
	mu          sync.Mutex
	lines       []string
	cap         int
	lastHash    uint64       // Hash of cleaned content (after mouse sequence stripping)
	lastRawHash uint64       // Hash of raw content before processing
	lastLen     int          // Length of last content (collision guard)
	hashSeed    maphash.Seed // Seed for stable hashing
}

// NewOutputBuffer creates a new output buffer with the given capacity.
func NewOutputBuffer(capacity int) *OutputBuffer {
	return &OutputBuffer{
		lines:    make([]string, 0, capacity),
		cap:      capacity,
		hashSeed: maphash.MakeSeed(),
	}
}

// Update replaces buffer content if it has changed (detected via hash).
// Returns true if content was updated, false if content was unchanged.
func (b *OutputBuffer) Update(content string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check hash BEFORE expensive regex processing
	// Compute hash of raw content first
	rawHash := maphash.String(b.hashSeed, content)
	if rawHash == b.lastRawHash && len(content) == b.lastLen {
		return false // Content unchanged - skip ALL processing
	}

	// Content changed - now strip mouse escape sequences
	// Fast path: only run regex if mouse sequences are likely present
	if strings.Contains(content, "\x1b[<") {
		content = mouseEscapeRegex.ReplaceAllString(content, "")
	}
	if strings.Contains(content, "\x1b[?") {
		content = terminalModeRegex.ReplaceAllString(content, "")
	}
	// Strip partial mouse sequences (ESC consumed by shell, rest printed as text)
	if strings.Contains(content, "[<") {
		content = partialMouseEscapeRegex.ReplaceAllString(content, "")
	}

	// Store cleaned content hash for future comparisons
	cleanHash := maphash.String(b.hashSeed, content)
	b.lastHash = cleanHash
	b.lastRawHash = rawHash
	b.lastLen = len(content)
	// Trim trailing newline before split to avoid spurious empty element.
	// tmux capture-pane output ends with \n, which would create an extra empty
	// element after split, causing cursor alignment to be off by one line.
	b.lines = strings.Split(strings.TrimSuffix(content, "\n"), "\n")

	// Trim to capacity (keep most recent lines)
	if len(b.lines) > b.cap {
		b.lines = b.lines[len(b.lines)-b.cap:]
	}

	return true
}

// Write replaces content in the buffer (for backward compatibility).
// Prefer Update() for change detection.
func (b *OutputBuffer) Write(content string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Strip mouse escape sequences.
	// Fast path: only run regex if mouse sequences are likely present
	if strings.Contains(content, "\x1b[<") {
		content = mouseEscapeRegex.ReplaceAllString(content, "")
	}
	if strings.Contains(content, "\x1b[?") {
		content = terminalModeRegex.ReplaceAllString(content, "")
	}
	// Strip partial mouse sequences (ESC consumed by shell, rest printed as text)
	if strings.Contains(content, "[<") {
		content = partialMouseEscapeRegex.ReplaceAllString(content, "")
	}

	// Replace instead of append to avoid duplication
	// Trim trailing newline before split (same as Update method)
	b.lines = strings.Split(strings.TrimSuffix(content, "\n"), "\n")

	// Trim to capacity (keep most recent lines)
	if len(b.lines) > b.cap {
		b.lines = b.lines[len(b.lines)-b.cap:]
	}
}

// Lines returns a copy of all lines in the buffer.
func (b *OutputBuffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]string, len(b.lines))
	copy(result, b.lines)
	return result
}

// LinesRange returns a copy of lines in the specified range [start, end).
// This is more efficient than Lines() when only a portion is needed.
func (b *OutputBuffer) LinesRange(start, end int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if start < 0 {
		start = 0
	}
	if end > len(b.lines) {
		end = len(b.lines)
	}
	if start >= end {
		return nil
	}
	result := make([]string, end-start)
	copy(result, b.lines[start:end])
	return result
}

// LineCount returns the number of lines without copying.
func (b *OutputBuffer) LineCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.lines)
}

// String returns the buffer contents as a single string.
func (b *OutputBuffer) String() string {
	return strings.Join(b.Lines(), "\n")
}

// Clear removes all lines from the buffer.
func (b *OutputBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = b.lines[:0]
	b.lastHash = 0
	b.lastRawHash = 0
	b.lastLen = 0
}

// Len returns the number of lines in the buffer.
func (b *OutputBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.lines)
}
