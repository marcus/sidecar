package notes

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/docview"
)

type noteSearchMatch struct {
	Line     int
	StartCol int
	EndCol   int
}

func (p *Plugin) startNoteSearch() {
	p.noteSearchMode = true
	p.noteSearchCommitted = false
	p.noteSearchField.Reset()
	p.noteSearchField.Focus()
	p.noteSearchMatches = nil
	p.noteSearchCursor = 0
}

func (p *Plugin) clearNoteSearch() {
	p.noteSearchMode = false
	p.noteSearchCommitted = false
	p.noteSearchField.Reset()
	p.noteSearchMatches = nil
	p.noteSearchCursor = 0
}

// handleNoteSearchKey answers a key while the in-note search bar owns the
// preview. While the query is being typed everything that is not esc or enter
// is the shared query field's; esc is the one key the field is never offered,
// because this bar is a mode over a note rather than a filter beside a list and
// leaving it stays one press.
func (p *Plugin) handleNoteSearchKey(msg tea.KeyPressMsg) (pluginResult, tea.Cmd) {
	key := msg.String()

	if key == "esc" {
		p.clearNoteSearch()
		return p, nil
	}

	if !p.noteSearchCommitted {
		p.noteSearchField.Focus()
		if key == "enter" {
			if p.noteSearchQuery() != "" {
				p.noteSearchCommitted = true
				p.noteSearchField.Blur()
			}
			return p, nil
		}
		before := p.noteSearchQuery()
		p.noteSearchField.HandleKey(msg)
		if p.noteSearchQuery() != before {
			p.updateNoteSearchMatches()
		}
		return p, nil
	}

	switch key {
	case "n":
		p.cycleNoteSearch(1)
	case "N":
		p.cycleNoteSearch(-1)
	case "enter":
		p.noteSearchMode = false
		p.noteSearchCommitted = false
		p.noteSearchField.Blur()
	case "j", "down", "ctrl+n":
		p.ensureViewSurface()
		if p.previewCursorLine < len(p.viewSurface.Lines)-1 {
			p.previewCursorLine++
		}
		p.ensurePreviewCursorVisible()
	case "k", "up", "ctrl+p":
		if p.previewCursorLine > 0 {
			p.previewCursorLine--
		}
		p.ensurePreviewCursorVisible()
	}
	return p, nil
}

func (p *Plugin) cycleNoteSearch(delta int) {
	if len(p.noteSearchMatches) == 0 {
		return
	}
	n := len(p.noteSearchMatches)
	p.noteSearchCursor = (p.noteSearchCursor + delta + n) % n
	p.scrollToNoteSearchMatch()
}

func (p *Plugin) updateNoteSearchMatches() {
	p.noteSearchMatches = nil
	p.noteSearchCursor = 0
	if p.noteSearchQuery() == "" {
		return
	}
	p.ensureViewSurface()
	query := strings.ToLower(p.noteSearchQuery())
	for lineNo, line := range p.viewSurface.Lines {
		plain := strings.ToLower(ansi.Strip(line))
		start := 0
		for {
			idx := strings.Index(plain[start:], query)
			if idx < 0 {
				break
			}
			abs := start + idx
			p.noteSearchMatches = append(p.noteSearchMatches, noteSearchMatch{
				Line:     lineNo,
				StartCol: abs,
				EndCol:   abs + len(p.noteSearchQuery()),
			})
			start = abs + 1
		}
	}
	if len(p.noteSearchMatches) > 0 {
		p.scrollToNoteSearchMatch()
	}
}

func (p *Plugin) scrollToNoteSearchMatch() {
	if p.noteSearchCursor < 0 || p.noteSearchCursor >= len(p.noteSearchMatches) {
		return
	}
	match := p.noteSearchMatches[p.noteSearchCursor]
	p.previewCursorLine = match.Line
	p.ensurePreviewCursorVisible()
}

func (p *Plugin) highlightNoteSearchLine(lineNo int, line string) string {
	if !p.noteSearchMode || p.noteSearchQuery() == "" {
		return line
	}
	var ranges []docview.MatchRange
	for i, m := range p.noteSearchMatches {
		if m.Line == lineNo {
			ranges = append(ranges, docview.MatchRange{Index: i, Start: m.StartCol, End: m.EndCol})
		}
	}
	if len(ranges) == 0 {
		return line
	}
	return docview.InjectHighlights(line, ranges, p.noteSearchCursor)
}

func (p *Plugin) renderNoteSearchPrompt() string {
	count := ""
	if p.noteSearchQuery() != "" {
		if len(p.noteSearchMatches) == 0 {
			count = " 0/0"
		} else {
			count = " " + strconv.Itoa(p.noteSearchCursor+1) + "/" + strconv.Itoa(len(p.noteSearchMatches))
		}
	}
	// This row is a segment of the preview's status line rather than a full
	// query bar, so it keeps its own shape instead of queryfield.RenderRow —
	// but the caret is drawn where the caret actually is, which is the point of
	// the shared field.
	query := p.noteSearchQuery()
	if p.noteSearchMode && !p.noteSearchCommitted {
		runes := []rune(query)
		caret := min(max(p.noteSearchField.Cursor(), 0), len(runes))
		query = string(runes[:caret]) + "▌" + string(runes[caret:])
	}
	return "/" + query + count
}

// noteSearchQuery is the in-note search bar's text.
func (p *Plugin) noteSearchQuery() string { return p.noteSearchField.Query() }

// handleNoteSearchPaste puts a bracketed paste into the in-note search bar
// while it is taking text. A committed search has no input on screen.
func (p *Plugin) handleNoteSearchPaste(content string) bool {
	if !p.noteSearchMode || p.noteSearchCommitted {
		return false
	}
	p.noteSearchField.Focus()
	before := p.noteSearchQuery()
	p.noteSearchField.HandlePaste(tea.PasteMsg{Content: content})
	if p.noteSearchQuery() != before {
		p.updateNoteSearchMatches()
	}
	return true
}
