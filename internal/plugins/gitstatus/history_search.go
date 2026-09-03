package gitstatus

import (
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/queryfield"
	"github.com/marcus/sidecar/internal/styles"
)

// HistorySearchState holds state for commit history search.
//
// The query is the app's shared query field rather than a string, so the modal
// edits like every other `/` bar: the caret moves, alt+backspace deletes a
// word, home and end work, and a paste arrives whole.
type HistorySearchState struct {
	field     queryfield.Field
	Matches   []*Commit // Commits matching the query
	Cursor    int       // Index in matches for n/N navigation
	Committed bool      // True after Enter (enables n/N)

	// Search options
	UseRegex      bool
	CaseSensitive bool
}

// NewHistorySearchState creates a new search state.
func NewHistorySearchState() *HistorySearchState {
	return &HistorySearchState{
		Matches: make([]*Commit, 0),
	}
}

// Query is the text typed into the search modal.
func (s *HistorySearchState) Query() string {
	if s == nil {
		return ""
	}
	return s.field.Query()
}

// SetQuery replaces the text and puts the caret at its end.
func (s *HistorySearchState) SetQuery(query string) {
	if s != nil {
		s.field.SetQuery(query)
	}
}

// Reset clears the search state.
func (s *HistorySearchState) Reset() {
	s.field.Reset()
	s.Matches = nil
	s.Cursor = 0
	s.Committed = false
}

// searchCommits filters commits by query (message or author).
func (p *Plugin) searchCommits(query string, useRegex, caseSensitive bool) []*Commit {
	if query == "" {
		return nil
	}

	var matches []*Commit
	var re *regexp.Regexp

	if useRegex {
		flags := ""
		if !caseSensitive {
			flags = "(?i)"
		}
		var err error
		re, err = regexp.Compile(flags + query)
		if err != nil {
			return nil // Invalid regex
		}
	}

	// Search within active commits (filtered or all)
	commits := p.activeCommits()
	for _, c := range commits {
		var match bool
		if useRegex && re != nil {
			match = re.MatchString(c.Subject) || re.MatchString(c.Author)
		} else {
			subject := c.Subject
			author := c.Author
			q := query
			if !caseSensitive {
				subject = strings.ToLower(subject)
				author = strings.ToLower(author)
				q = strings.ToLower(query)
			}
			match = strings.Contains(subject, q) || strings.Contains(author, q)
		}
		if match {
			matches = append(matches, c)
		}
	}

	return matches
}

// findCommitIndex returns the index of a commit in active commits by hash.
func (p *Plugin) findCommitIndex(hash string) int {
	commits := p.activeCommits()
	for i, c := range commits {
		if c.Hash == hash {
			return i
		}
	}
	return -1
}

// renderHistorySearchModal renders the search modal overlay.
func (p *Plugin) renderHistorySearchModal(width int) string {
	state := p.historySearchState
	if state == nil {
		state = NewHistorySearchState()
	}

	// Modal dimensions
	modalWidth := width - 4
	if modalWidth > 70 {
		modalWidth = 70
	}
	if modalWidth < 40 {
		modalWidth = 40
	}

	var sb strings.Builder

	// Header: the app's query bar. It draws through queryfield.RenderRow, so
	// this modal's `/` row is the same row the rest of the app shows, caret
	// included. The × is not drawn: a modal overlay registers no hit region
	// here, and a control nothing listens to is worse than no control.
	header, _ := queryfield.RenderRow(modalWidth, queryfield.Row{
		Query:       state.Query(),
		Cursor:      state.field.Cursor(),
		Focused:     true,
		Placeholder: "search commits…",
	})
	sb.WriteString(header)
	sb.WriteString("\n")

	// Options bar
	var opts []string
	if state.UseRegex {
		opts = append(opts, styles.BarChipActive.Render(".*"))
	} else {
		opts = append(opts, styles.BarChip.Render(".*"))
	}
	if state.CaseSensitive {
		opts = append(opts, styles.BarChipActive.Render("Aa"))
	} else {
		opts = append(opts, styles.BarChip.Render("Aa"))
	}
	sb.WriteString(strings.Join(opts, " "))
	sb.WriteString("\n\n")

	// Status line
	if state.Query() == "" {
		sb.WriteString(styles.Muted.Render("Type to search commits..."))
		sb.WriteString("\n")
	} else if len(state.Matches) == 0 {
		sb.WriteString(styles.Muted.Render("No matches found"))
		sb.WriteString("\n")
	} else {
		// Match count header
		var matchInfo string
		if len(state.Matches) == 1 {
			matchInfo = "1 match"
		} else {
			matchInfo = formatInt(len(state.Matches)) + " matches"
		}
		sb.WriteString(styles.Muted.Render(matchInfo))
		sb.WriteString("\n\n")

		// Display matches (up to 8)
		maxVisible := 8
		if len(state.Matches) < maxVisible {
			maxVisible = len(state.Matches)
		}

		// Calculate scroll offset to keep cursor visible
		scrollOff := 0
		if state.Cursor >= maxVisible {
			scrollOff = state.Cursor - maxVisible + 1
		}

		for i := scrollOff; i < scrollOff+maxVisible && i < len(state.Matches); i++ {
			c := state.Matches[i]

			// Cursor indicator
			if i == state.Cursor {
				sb.WriteString(styles.ListCursor.Render("▸ "))
			} else {
				sb.WriteString("  ")
			}

			// Short hash (muted)
			sb.WriteString(styles.Subtle.Render(c.ShortHash))
			sb.WriteString(" ")

			// Subject (truncate to fit)
			subjectWidth := modalWidth - 12 // hash + cursor + padding
			subject := c.Subject
			if len(subject) > subjectWidth {
				subject = subject[:subjectWidth-3] + "..."
			}
			if i == state.Cursor {
				sb.WriteString(styles.ListItemSelected.Render(subject))
			} else {
				sb.WriteString(subject)
			}
			sb.WriteString("\n")
		}

		// Show scroll indicator if more matches
		if len(state.Matches) > maxVisible {
			remaining := len(state.Matches) - scrollOff - maxVisible
			if remaining > 0 {
				sb.WriteString(styles.Muted.Render("  ↓ " + formatInt(remaining) + " more"))
				sb.WriteString("\n")
			}
		}
	}

	sb.WriteString("\n")
	// Hint
	sb.WriteString(styles.Muted.Render("j/k nav · enter select · alt+r regex · esc cancel"))

	content := sb.String()
	return styles.ModalBox.Width(modalWidth).Render(content)
}

// formatInt converts int to string without importing strconv in view logic.
func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + formatInt(-n)
	}

	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// updateHistorySearch handles key events when in search mode.
func (p *Plugin) updateHistorySearch(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	state := p.historySearchState
	if state == nil {
		state = NewHistorySearchState()
		p.historySearchState = state
	}

	// The modal flag is what means "this bar owns the keyboard", so the field's
	// focus is derived from it rather than tracked twice.
	state.field.Focus()

	key := msg.String()

	switch key {
	case "esc":
		// Cancel search, close modal, and clear search state
		p.historySearchMode = false
		p.clearSearchState()
		return p, nil

	case "enter":
		// Select current match and jump to it
		if len(state.Matches) > 0 {
			state.Committed = true
			p.historySearchMode = false
			return p, p.jumpToSearchMatch()
		}
		// No matches, just close and clear
		p.historySearchMode = false
		p.clearSearchState()
		return p, nil

	case "j", "down", "ctrl+n":
		// Navigate down in matches
		if len(state.Matches) > 0 {
			state.Cursor++
			if state.Cursor >= len(state.Matches) {
				state.Cursor = 0 // Wrap around
			}
		}
		return p, nil

	case "k", "up", "ctrl+p":
		// Navigate up in matches
		if len(state.Matches) > 0 {
			state.Cursor--
			if state.Cursor < 0 {
				state.Cursor = len(state.Matches) - 1 // Wrap around
			}
		}
		return p, nil

	case "alt+r":
		// Toggle regex. This modal's two option toggles are checked before the
		// field, so the surface's chord wins whatever the text input would do
		// with it. Neither is one of the field's word keys today (those are
		// alt+b, alt+f, alt+d and alt+backspace), so nothing is lost.
		state.UseRegex = !state.UseRegex
		p.rematchHistorySearch()
		return p, nil

	case "alt+c":
		// Toggle case sensitivity — see alt+r.
		state.CaseSensitive = !state.CaseSensitive
		p.rematchHistorySearch()
		return p, nil

	default:
		// Everything else is the field's: text, the caret keys, word ops, home
		// and end. j/k and the arrows above still walk the match list, so the
		// two letters this modal has always spent on navigation stay spent.
		before := state.Query()
		state.field.HandleKey(msg)
		if state.Query() != before {
			p.rematchHistorySearch()
		}
		return p, nil
	}
}

// rematchHistorySearch re-runs the search after the query or an option
// changed, and puts the cursor back on the first match.
func (p *Plugin) rematchHistorySearch() {
	state := p.historySearchState
	if state == nil {
		return
	}
	state.Matches = p.searchCommits(state.Query(), state.UseRegex, state.CaseSensitive)
	state.Cursor = 0
}

// handleSearchPaste puts a bracketed paste into whichever of the two git
// history bars is open. A query bar is a text input, so a paste lands in it
// exactly as typed characters do.
func (p *Plugin) handleSearchPaste(msg tea.PasteMsg) (bool, tea.Cmd) {
	switch {
	case p.historySearchMode:
		if p.historySearchState == nil {
			p.historySearchState = NewHistorySearchState()
		}
		state := p.historySearchState
		state.field.Focus()
		before := state.Query()
		state.field.HandlePaste(msg)
		if state.Query() != before {
			p.rematchHistorySearch()
		}
		return true, nil
	case p.pathFilterMode:
		p.pathFilterField.Focus()
		p.pathFilterField.HandlePaste(msg)
		return true, nil
	}
	return false, nil
}

// clearSearchState clears the history search state.
func (p *Plugin) clearSearchState() {
	if p.historySearchState != nil {
		p.historySearchState.Reset()
	}
}

// updatePathFilter handles key events when in path filter mode.
func (p *Plugin) updatePathFilter(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	// The modal flag is what means "this bar owns the keyboard".
	p.pathFilterField.Focus()

	switch msg.String() {
	case "esc":
		// Cancel path filter, close modal
		p.pathFilterMode = false
		p.pathFilterField.Reset()
		return p, nil

	case "enter":
		// Apply path filter
		if p.pathFilterInput() != "" {
			p.historyFilterPath = p.pathFilterInput()
			p.historyFilterActive = true
			p.pathFilterMode = false
			p.pathFilterField.Blur()
			return p, p.loadFilteredCommits()
		}
		// Empty input, just close
		p.pathFilterMode = false
		p.pathFilterField.Blur()
		return p, nil

	default:
		// Everything else is the field's: text, the caret keys, word ops, home
		// and end.
		p.pathFilterField.HandleKey(msg)
		return p, nil
	}
}

// pathFilterInput is the path typed into the filter modal.
func (p *Plugin) pathFilterInput() string { return p.pathFilterField.Query() }

// renderPathFilterModal renders the path filter input modal.
func (p *Plugin) renderPathFilterModal(width int) string {
	// Modal dimensions
	modalWidth := width - 4
	if modalWidth > 60 {
		modalWidth = 60
	}
	if modalWidth < 30 {
		modalWidth = 30
	}

	var sb strings.Builder

	// Title
	sb.WriteString(styles.ModalTitle.Render("Filter by Path"))
	sb.WriteString("\n\n")

	// Input with the caret drawn where the caret is. This row keeps its own
	// `Path:` label rather than the query bar's `/` prompt — it is a labelled
	// input, not a search — but the text and the caret are the shared field's,
	// so a caret that moved is visible where it actually moved to.
	prefix := "Path: "
	available := modalWidth - len(prefix) - 1

	input, caret := p.pathFilterInput(), p.pathFilterField.Cursor()
	if len(input) > available {
		trimmed := len(input) - available + 3
		input = "..." + input[trimmed:]
		caret = max(caret-trimmed+3, 0)
	}
	runes := []rune(input)
	caret = min(max(caret, 0), len(runes))

	sb.WriteString(prefix + string(runes[:caret]) + "▌" + string(runes[caret:]))
	sb.WriteString("\n\n")

	// Hint
	sb.WriteString(styles.Muted.Render("Examples: *.go, internal/, README.md"))
	sb.WriteString("\n")
	sb.WriteString(styles.Muted.Render("enter apply · esc cancel"))

	content := sb.String()
	return styles.ModalBox.Width(modalWidth).Render(content)
}

// jumpToSearchMatch moves cursor to the current search match.
func (p *Plugin) jumpToSearchMatch() tea.Cmd {
	state := p.historySearchState
	if state == nil || len(state.Matches) == 0 {
		return nil
	}

	match := state.Matches[state.Cursor]
	idx := p.findCommitIndex(match.Hash)
	if idx < 0 {
		return nil
	}

	// Move cursor to match (entries count + commit index)
	entries := p.tree.AllEntries()
	p.cursor = len(entries) + idx
	p.ensureCommitVisible(idx)

	return p.autoLoadCommitPreview()
}
