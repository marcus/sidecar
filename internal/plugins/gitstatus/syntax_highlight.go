package gitstatus

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	appstyles "github.com/marcus/sidecar/internal/styles"
)

// SyntaxHighlighter provides syntax highlighting for diff content using Chroma.
type SyntaxHighlighter struct {
	lexer chroma.Lexer
	style *chroma.Style
}

// NewSyntaxHighlighter creates a highlighter for the given filename.
// Returns nil if no lexer is available for the file type.
func NewSyntaxHighlighter(filename string) *SyntaxHighlighter {
	lexer := lexers.Match(filename)
	if lexer == nil {
		// Try by extension
		ext := filepath.Ext(filename)
		if ext != "" {
			lexer = lexers.Get(ext)
		}
	}
	if lexer == nil {
		return nil
	}

	// Use theme-configured syntax style
	style := styles.Get(appstyles.GetSyntaxTheme())
	if style == nil {
		style = styles.Fallback
	}

	return &SyntaxHighlighter{
		lexer: chroma.Coalesce(lexer),
		style: style,
	}
}

// HighlightSegment represents a segment of highlighted text.
type HighlightSegment struct {
	Text  string
	Style lipgloss.Style
}

// Highlight tokenizes and highlights a line of code.
// Returns segments with lipgloss styles applied.
func (h *SyntaxHighlighter) Highlight(line string) []HighlightSegment {
	if h == nil || h.lexer == nil {
		return []HighlightSegment{{Text: line, Style: lipgloss.NewStyle()}}
	}

	iterator, err := h.lexer.Tokenise(nil, line)
	if err != nil {
		return []HighlightSegment{{Text: line, Style: lipgloss.NewStyle()}}
	}

	var segments []HighlightSegment
	for _, token := range iterator.Tokens() {
		// Strip trailing newlines - Chroma adds them to some tokens (like comments)
		// and they cause rendering issues with lipgloss width calculations
		text := strings.TrimSuffix(token.Value, "\n")
		if text == "" {
			continue
		}
		style := h.tokenStyle(token.Type)
		segments = append(segments, HighlightSegment{
			Text:  text,
			Style: style,
		})
	}

	return segments
}

// tokenStyle converts a Chroma token type to a lipgloss style.
func (h *SyntaxHighlighter) tokenStyle(tokenType chroma.TokenType) lipgloss.Style {
	entry := h.style.Get(tokenType)
	style := lipgloss.NewStyle()

	if entry.Colour.IsSet() {
		style = style.Foreground(lipgloss.Color(entry.Colour.String()))
	}
	if entry.Bold == chroma.Yes {
		style = style.Bold(true)
	}
	// Note: Italic is intentionally not applied because it causes
	// width calculation issues in terminal grid layouts (side-by-side diffs).
	// The ANSI italic sequences can affect visual width in some terminals.
	if entry.Underline == chroma.Yes {
		style = style.Underline(true)
	}

	return style
}

// HighlightLine highlights a single line of code, returning styled segments.
func (h *SyntaxHighlighter) HighlightLine(content string) []HighlightSegment {
	return h.Highlight(content)
}
