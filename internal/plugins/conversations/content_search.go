// Package conversations provides the content search functionality for
// cross-conversation search (searching message content across sessions).
package conversations

import (
	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/ui"
)

// ContentSearchState holds cross-conversation search state.
type ContentSearchState struct {
	Query           string                // Current search query
	Results         []SessionSearchResult // Sessions with matching messages
	UseRegex        bool                  // Treat query as regex
	CaseSensitive   bool                  // Case-sensitive matching
	Cursor          int                   // Flat index into hierarchical results
	ScrollOffset    int                   // Scroll offset for viewport
	IsSearching     bool                  // True while search is in progress
	Error           string                // Error message if search failed
	DebounceVersion int                   // For debouncing search requests
	TotalFound      int                   // Total matches found before truncation (td-8e1a2b)
	Truncated       bool                  // True if results were truncated (td-8e1a2b)
	Skeleton        ui.Skeleton           // Animated skeleton loader for search in progress (td-e740e4)
}

// SessionSearchResult represents a session with matching messages.
type SessionSearchResult struct {
	Session   adapter.Session        // The session containing matches
	Messages  []adapter.MessageMatch // Messages with matches (from adapter search)
	Collapsed bool                   // True if session is collapsed in view
}

// ContentSearchDebounceMsg is sent after debounce delay to trigger search.
type ContentSearchDebounceMsg struct {
	Version int    // Must match DebounceVersion to be valid
	Query   string // Query to search for
}

// ContentSearchResultsMsg carries search results back to the plugin.
type ContentSearchResultsMsg struct {
	Epoch        uint64                // Epoch when request was issued (for stale detection)
	Results      []SessionSearchResult // Search results by session
	Error        error                 // Error if search failed
	Query        string                // The query these results are for (td-5b9928)
	TotalMatches int                   // Total matches found before truncation (td-8e1a2b)
	Truncated    bool                  // True if results were truncated (td-8e1a2b)
}

// GetEpoch implements plugin.EpochMessage.
func (m ContentSearchResultsMsg) GetEpoch() uint64 { return m.Epoch }

// NewContentSearchState creates a new content search state with defaults.
func NewContentSearchState() *ContentSearchState {
	// Skeleton with varied row widths for search results look (td-e740e4)
	skeleton := ui.NewSkeleton(8, []int{90, 70, 65, 85, 75, 60, 80, 55})
	return &ContentSearchState{
		Results:       make([]SessionSearchResult, 0),
		UseRegex:      false,
		CaseSensitive: false,
		Skeleton:      skeleton,
	}
}

// Reset clears the search state.
func (s *ContentSearchState) Reset() {
	s.Query = ""
	s.Results = s.Results[:0]
	s.Cursor = 0
	s.ScrollOffset = 0
	s.IsSearching = false
	s.Error = ""
	// Preserve UseRegex and CaseSensitive as user preferences
}

// TotalMatches returns the total count of content matches across all results.
func (s *ContentSearchState) TotalMatches() int {
	count := 0
	for _, sr := range s.Results {
		for _, mm := range sr.Messages {
			count += len(mm.Matches)
		}
	}
	return count
}

// SessionCount returns the number of sessions with matches.
func (s *ContentSearchState) SessionCount() int {
	return len(s.Results)
}

// FlatLen returns the total number of items in the flattened view.
// The hierarchy is: Session -> Message -> Match
// When a session is collapsed, its messages and matches are hidden.
func (s *ContentSearchState) FlatLen() int {
	count := 0
	for _, sr := range s.Results {
		count++ // Session row
		if !sr.Collapsed {
			for _, mm := range sr.Messages {
				count++                  // Message row
				count += len(mm.Matches) // Match rows
			}
		}
	}
	return count
}

// FlatItem maps a flat index to the hierarchical position.
// Returns (sessionIdx, msgIdx, matchIdx, isSession, isMessage).
// - If isSession is true, this is a session row (msgIdx and matchIdx are -1).
// - If isMessage is true, this is a message row (matchIdx is -1).
// - Otherwise, this is a match row.
// Returns (-1, -1, -1, false, false) if idx is out of range.
func (s *ContentSearchState) FlatItem(idx int) (sessionIdx, msgIdx, matchIdx int, isSession, isMessage bool) {
	if idx < 0 {
		return -1, -1, -1, false, false
	}

	pos := 0
	for si, sr := range s.Results {
		// Session row
		if pos == idx {
			return si, -1, -1, true, false
		}
		pos++

		if !sr.Collapsed {
			for mi, mm := range sr.Messages {
				// Message row
				if pos == idx {
					return si, mi, -1, false, true
				}
				pos++

				// Match rows
				for mti := range mm.Matches {
					if pos == idx {
						return si, mi, mti, false, false
					}
					pos++
				}
			}
		}
	}

	return -1, -1, -1, false, false
}

// flatIdxFor returns the flat index for a given (sessionIdx, msgIdx, matchIdx).
// Use msgIdx=-1 for session row, matchIdx=-1 for message row.
// Returns -1 if the item is not visible (e.g., inside a collapsed session).
func (s *ContentSearchState) flatIdxFor(sessionIdx, msgIdx, matchIdx int) int {
	if sessionIdx < 0 || sessionIdx >= len(s.Results) {
		return -1
	}

	pos := 0
	for si, sr := range s.Results {
		// Session row
		if si == sessionIdx && msgIdx == -1 {
			return pos
		}
		pos++

		if sr.Collapsed {
			// Skip collapsed content - if we're looking for something inside, it's not visible
			if si == sessionIdx && msgIdx >= 0 {
				return -1
			}
			continue
		}

		for mi, mm := range sr.Messages {
			// Message row
			if si == sessionIdx && mi == msgIdx && matchIdx == -1 {
				return pos
			}
			pos++

			// Match rows
			for mti := range mm.Matches {
				if si == sessionIdx && mi == msgIdx && mti == matchIdx {
					return pos
				}
				pos++
			}
		}
	}

	return -1
}

// NextMatchIndex finds the next actual match (not session or message row) from the given index.
// Returns the flat index of the next match, or -1 if none found.
func (s *ContentSearchState) NextMatchIndex(from int) int {
	flatLen := s.FlatLen()
	for i := from + 1; i < flatLen; i++ {
		_, _, matchIdx, isSession, isMessage := s.FlatItem(i)
		if !isSession && !isMessage && matchIdx >= 0 {
			return i
		}
	}
	// Wrap around
	for i := 0; i <= from && i < flatLen; i++ {
		_, _, matchIdx, isSession, isMessage := s.FlatItem(i)
		if !isSession && !isMessage && matchIdx >= 0 {
			return i
		}
	}
	return -1
}

// PrevMatchIndex finds the previous actual match (not session or message row) from the given index.
// Returns the flat index of the previous match, or -1 if none found.
func (s *ContentSearchState) PrevMatchIndex(from int) int {
	// Search backwards from current position
	for i := from - 1; i >= 0; i-- {
		_, _, matchIdx, isSession, isMessage := s.FlatItem(i)
		if !isSession && !isMessage && matchIdx >= 0 {
			return i
		}
	}
	// Wrap around
	flatLen := s.FlatLen()
	for i := flatLen - 1; i >= from && i >= 0; i-- {
		_, _, matchIdx, isSession, isMessage := s.FlatItem(i)
		if !isSession && !isMessage && matchIdx >= 0 {
			return i
		}
	}
	return -1
}

// GetSelectedResult returns the currently selected item based on cursor position.
// Returns (session, messageMatch, contentMatch) where some may be nil depending on selection type:
// - Session row: session is set, others are nil
// - Message row: session and messageMatch are set, contentMatch is nil
// - Match row: all three are set
// Returns (nil, nil, nil) if cursor is out of range.
func (s *ContentSearchState) GetSelectedResult() (*adapter.Session, *adapter.MessageMatch, *adapter.ContentMatch) {
	sessionIdx, msgIdx, matchIdx, isSession, isMessage := s.FlatItem(s.Cursor)
	if sessionIdx < 0 || sessionIdx >= len(s.Results) {
		return nil, nil, nil
	}

	session := &s.Results[sessionIdx].Session

	if isSession {
		return session, nil, nil
	}

	if msgIdx < 0 || msgIdx >= len(s.Results[sessionIdx].Messages) {
		return session, nil, nil
	}

	msg := &s.Results[sessionIdx].Messages[msgIdx]

	if isMessage {
		return session, msg, nil
	}

	if matchIdx < 0 || matchIdx >= len(msg.Matches) {
		return session, msg, nil
	}

	return session, msg, &msg.Matches[matchIdx]
}

// ToggleCollapse toggles the collapsed state of the session at the current cursor.
// Returns true if toggle was performed (cursor was on a session row).
func (s *ContentSearchState) ToggleCollapse() bool {
	sessionIdx, _, _, isSession, _ := s.FlatItem(s.Cursor)
	if !isSession || sessionIdx < 0 || sessionIdx >= len(s.Results) {
		return false
	}
	s.Results[sessionIdx].Collapsed = !s.Results[sessionIdx].Collapsed
	return true
}

// ExpandAll expands all sessions.
func (s *ContentSearchState) ExpandAll() {
	for i := range s.Results {
		s.Results[i].Collapsed = false
	}
}

// CollapseAll collapses all sessions.
func (s *ContentSearchState) CollapseAll() {
	for i := range s.Results {
		s.Results[i].Collapsed = true
	}
}

// MoveToSession moves the cursor to the session containing the current selection.
// Useful for navigating "up" in the hierarchy.
func (s *ContentSearchState) MoveToSession() {
	sessionIdx, _, _, isSession, _ := s.FlatItem(s.Cursor)
	if isSession || sessionIdx < 0 {
		return
	}
	// Find the flat index for this session
	newIdx := s.flatIdxFor(sessionIdx, -1, -1)
	if newIdx >= 0 {
		s.Cursor = newIdx
	}
}

// FirstMatchInSession returns the flat index of the first match in the given session.
// Returns -1 if session is collapsed or has no matches.
func (s *ContentSearchState) FirstMatchInSession(sessionIdx int) int {
	if sessionIdx < 0 || sessionIdx >= len(s.Results) {
		return -1
	}
	sr := &s.Results[sessionIdx]
	if sr.Collapsed || len(sr.Messages) == 0 {
		return -1
	}
	if len(sr.Messages[0].Matches) == 0 {
		return -1
	}
	return s.flatIdxFor(sessionIdx, 0, 0)
}

// EnsureCursorVisible adjusts ScrollOffset to ensure the cursor is visible.
// viewportHeight is the number of visible rows.
func (s *ContentSearchState) EnsureCursorVisible(viewportHeight int) {
	if viewportHeight <= 0 {
		return
	}
	if s.Cursor < s.ScrollOffset {
		s.ScrollOffset = s.Cursor
	} else if s.Cursor >= s.ScrollOffset+viewportHeight {
		s.ScrollOffset = s.Cursor - viewportHeight + 1
	}
}

// ClampCursor ensures cursor is within valid bounds.
func (s *ContentSearchState) ClampCursor() {
	flatLen := s.FlatLen()
	if s.Cursor < 0 {
		s.Cursor = 0
	}
	if flatLen > 0 && s.Cursor >= flatLen {
		s.Cursor = flatLen - 1
	}
	if flatLen == 0 {
		s.Cursor = 0
	}
}
