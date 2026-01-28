package filebrowser

import (
	"strings"

	"github.com/marcus/sidecar/internal/styles"
)

// matchRange represents a highlight range in visible (ANSI-stripped) text coordinates.
type matchRange struct {
	matchIdx int // index in contentSearchMatches (for current match detection)
	start    int // byte offset in stripped text
	end      int // byte offset in stripped text
}

// highlightMarkdownLineMatches injects search highlighting into a Glamour-rendered ANSI line.
func (p *Plugin) highlightMarkdownLineMatches(lineNo int) string {
	if lineNo >= len(p.markdownRendered) {
		return ""
	}
	ansiLine := p.markdownRendered[lineNo]

	var ranges []matchRange
	for i, m := range p.contentSearchMatches {
		if m.LineNo == lineNo {
			ranges = append(ranges, matchRange{
				matchIdx: i,
				start:    m.StartCol,
				end:      m.EndCol,
			})
		}
	}

	if len(ranges) == 0 {
		return ansiLine
	}

	return injectHighlightsIntoANSI(ansiLine, ranges, p.contentSearchCursor)
}

// Pre-render highlight style prefixes by rendering a known marker and extracting the ANSI prefix.
var (
	searchMatchStyle        = styles.SearchMatch.Render
	searchMatchCurrentStyle = styles.SearchMatchCurrent.Render
)

// injectHighlightsIntoANSI walks an ANSI-styled string and injects highlight
// escape sequences at positions corresponding to visible-text byte offsets.
func injectHighlightsIntoANSI(s string, matches []matchRange, currentMatchIdx int) string {
	if len(matches) == 0 {
		return s
	}

	var result strings.Builder
	result.Grow(len(s) + len(matches)*20)

	visiblePos := 0 // byte offset in stripped/visible text
	matchIdx := 0   // index into matches slice
	inHighlight := false

	i := 0
	for i < len(s) {
		// Pass through ANSI escape sequences without counting as visible
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !isANSITerminator(s[j]) {
				j++
			}
			if j < len(s) {
				j++ // include terminator
			}
			result.WriteString(s[i:j])
			i = j
			continue
		}

		// Check: end current highlight before starting a new one
		if inHighlight && matchIdx < len(matches) && visiblePos >= matches[matchIdx].end {
			result.WriteString("\x1b[0m")
			inHighlight = false
			matchIdx++
		}

		// Check: start highlight
		if !inHighlight && matchIdx < len(matches) && visiblePos == matches[matchIdx].start {
			inHighlight = true
			if matches[matchIdx].matchIdx == currentMatchIdx {
				result.WriteString(extractANSIPrefix(searchMatchCurrentStyle))
			} else {
				result.WriteString(extractANSIPrefix(searchMatchStyle))
			}
		}

		result.WriteByte(s[i])
		visiblePos++
		i++
	}

	// Close unclosed highlight
	if inHighlight {
		result.WriteString("\x1b[0m")
	}

	return result.String()
}

// isANSITerminator returns true for bytes that terminate an ANSI CSI sequence.
func isANSITerminator(b byte) bool {
	return b >= 0x40 && b <= 0x7E
}

// extractANSIPrefix renders a space through a lipgloss style function and
// returns just the ANSI prefix codes (everything before the content).
func extractANSIPrefix(styleFn func(strs ...string) string) string {
	const marker = "\x00"
	rendered := styleFn(marker)
	prefix, _, found := strings.Cut(rendered, marker)
	if !found {
		return ""
	}
	return prefix
}
