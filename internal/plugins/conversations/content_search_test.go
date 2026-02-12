package conversations

import (
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
)

// Helper to create test data
func makeTestSearchState() *ContentSearchState {
	return &ContentSearchState{
		Query: "test",
		Results: []SessionSearchResult{
			{
				Session: adapter.Session{ID: "session1", Name: "Session 1"},
				Messages: []adapter.MessageMatch{
					{
						MessageID:  "msg1",
						MessageIdx: 0,
						Role:       "user",
						Timestamp:  time.Now(),
						Matches: []adapter.ContentMatch{
							{BlockType: "text", LineNo: 1, LineText: "test line 1", ColStart: 0, ColEnd: 4},
							{BlockType: "text", LineNo: 5, LineText: "another test", ColStart: 8, ColEnd: 12},
						},
					},
					{
						MessageID:  "msg2",
						MessageIdx: 1,
						Role:       "assistant",
						Timestamp:  time.Now(),
						Matches: []adapter.ContentMatch{
							{BlockType: "text", LineNo: 10, LineText: "testing here", ColStart: 0, ColEnd: 7},
						},
					},
				},
				Collapsed: false,
			},
			{
				Session: adapter.Session{ID: "session2", Name: "Session 2"},
				Messages: []adapter.MessageMatch{
					{
						MessageID:  "msg3",
						MessageIdx: 0,
						Role:       "user",
						Timestamp:  time.Now(),
						Matches: []adapter.ContentMatch{
							{BlockType: "text", LineNo: 1, LineText: "test in session 2", ColStart: 0, ColEnd: 4},
						},
					},
				},
				Collapsed: false,
			},
		},
	}
}

func TestFlatLen(t *testing.T) {
	s := makeTestSearchState()

	// Expanded: session1 + msg1 + 2 matches + msg2 + 1 match + session2 + msg3 + 1 match
	// = 1 + 1 + 2 + 1 + 1 + 1 + 1 + 1 = 9
	expected := 9
	if got := s.FlatLen(); got != expected {
		t.Errorf("FlatLen() = %d, want %d", got, expected)
	}

	// Collapse session1
	s.Results[0].Collapsed = true
	// session1 + session2 + msg3 + 1 match = 1 + 1 + 1 + 1 = 4
	expected = 4
	if got := s.FlatLen(); got != expected {
		t.Errorf("FlatLen() with collapsed session = %d, want %d", got, expected)
	}

	// Collapse both
	s.Results[1].Collapsed = true
	// session1 + session2 = 2
	expected = 2
	if got := s.FlatLen(); got != expected {
		t.Errorf("FlatLen() with all collapsed = %d, want %d", got, expected)
	}
}

func TestFlatItem(t *testing.T) {
	s := makeTestSearchState()

	tests := []struct {
		idx       int
		wantSess  int
		wantMsg   int
		wantMatch int
		isSess    bool
		isMsg     bool
		desc      string
	}{
		{0, 0, -1, -1, true, false, "session1"},
		{1, 0, 0, -1, false, true, "session1/msg1"},
		{2, 0, 0, 0, false, false, "session1/msg1/match0"},
		{3, 0, 0, 1, false, false, "session1/msg1/match1"},
		{4, 0, 1, -1, false, true, "session1/msg2"},
		{5, 0, 1, 0, false, false, "session1/msg2/match0"},
		{6, 1, -1, -1, true, false, "session2"},
		{7, 1, 0, -1, false, true, "session2/msg3"},
		{8, 1, 0, 0, false, false, "session2/msg3/match0"},
		{9, -1, -1, -1, false, false, "out of range"},
		{-1, -1, -1, -1, false, false, "negative"},
	}

	for _, tc := range tests {
		sessIdx, msgIdx, matchIdx, isSess, isMsg := s.FlatItem(tc.idx)
		if sessIdx != tc.wantSess || msgIdx != tc.wantMsg || matchIdx != tc.wantMatch || isSess != tc.isSess || isMsg != tc.isMsg {
			t.Errorf("FlatItem(%d) [%s] = (%d, %d, %d, %v, %v), want (%d, %d, %d, %v, %v)",
				tc.idx, tc.desc,
				sessIdx, msgIdx, matchIdx, isSess, isMsg,
				tc.wantSess, tc.wantMsg, tc.wantMatch, tc.isSess, tc.isMsg)
		}
	}
}

func TestFlatItemWithCollapsed(t *testing.T) {
	s := makeTestSearchState()
	s.Results[0].Collapsed = true

	// With session1 collapsed:
	// idx 0: session1
	// idx 1: session2
	// idx 2: session2/msg3
	// idx 3: session2/msg3/match0

	tests := []struct {
		idx       int
		wantSess  int
		wantMsg   int
		wantMatch int
		isSess    bool
		isMsg     bool
	}{
		{0, 0, -1, -1, true, false},   // session1
		{1, 1, -1, -1, true, false},   // session2
		{2, 1, 0, -1, false, true},    // session2/msg3
		{3, 1, 0, 0, false, false},    // session2/msg3/match0
		{4, -1, -1, -1, false, false}, // out of range
	}

	for _, tc := range tests {
		sessIdx, msgIdx, matchIdx, isSess, isMsg := s.FlatItem(tc.idx)
		if sessIdx != tc.wantSess || msgIdx != tc.wantMsg || matchIdx != tc.wantMatch || isSess != tc.isSess || isMsg != tc.isMsg {
			t.Errorf("FlatItem(%d) with collapsed = (%d, %d, %d, %v, %v), want (%d, %d, %d, %v, %v)",
				tc.idx,
				sessIdx, msgIdx, matchIdx, isSess, isMsg,
				tc.wantSess, tc.wantMsg, tc.wantMatch, tc.isSess, tc.isMsg)
		}
	}
}

func TestNextMatchIndex(t *testing.T) {
	s := makeTestSearchState()

	tests := []struct {
		from int
		want int
		desc string
	}{
		{0, 2, "from session1 to first match"},       // session -> match
		{1, 2, "from msg1 to first match"},           // msg -> match
		{2, 3, "from match0 to match1"},              // match -> next match
		{3, 5, "from match1 to msg2/match0"},         // skip msg header
		{5, 8, "from msg2/match0 to session2 match"}, // skip session and msg headers
		{8, 2, "wrap around from last match"},        // wrap to first match
	}

	for _, tc := range tests {
		got := s.NextMatchIndex(tc.from)
		if got != tc.want {
			t.Errorf("NextMatchIndex(%d) [%s] = %d, want %d", tc.from, tc.desc, got, tc.want)
		}
	}
}

func TestPrevMatchIndex(t *testing.T) {
	s := makeTestSearchState()

	tests := []struct {
		from int
		want int
		desc string
	}{
		{2, 8, "from first match wraps to last"},
		{3, 2, "from match1 to match0"},
		{5, 3, "from msg2/match0 to match1"},
		{8, 5, "from session2 match to msg2/match0"},
	}

	for _, tc := range tests {
		got := s.PrevMatchIndex(tc.from)
		if got != tc.want {
			t.Errorf("PrevMatchIndex(%d) [%s] = %d, want %d", tc.from, tc.desc, got, tc.want)
		}
	}
}

func TestGetSelectedResult(t *testing.T) {
	s := makeTestSearchState()

	// Test session row
	s.Cursor = 0
	sess, msg, match := s.GetSelectedResult()
	if sess == nil || sess.ID != "session1" {
		t.Errorf("GetSelectedResult at session row: expected session1, got %v", sess)
	}
	if msg != nil || match != nil {
		t.Errorf("GetSelectedResult at session row: expected nil msg/match, got %v, %v", msg, match)
	}

	// Test message row
	s.Cursor = 1
	sess, msg, match = s.GetSelectedResult()
	if sess == nil || sess.ID != "session1" {
		t.Errorf("GetSelectedResult at message row: expected session1, got %v", sess)
	}
	if msg == nil || msg.MessageID != "msg1" {
		t.Errorf("GetSelectedResult at message row: expected msg1, got %v", msg)
	}
	if match != nil {
		t.Errorf("GetSelectedResult at message row: expected nil match, got %v", match)
	}

	// Test match row
	s.Cursor = 2
	sess, msg, match = s.GetSelectedResult()
	if sess == nil || sess.ID != "session1" {
		t.Errorf("GetSelectedResult at match row: expected session1, got %v", sess)
	}
	if msg == nil || msg.MessageID != "msg1" {
		t.Errorf("GetSelectedResult at match row: expected msg1, got %v", msg)
	}
	if match == nil || match.LineNo != 1 {
		t.Errorf("GetSelectedResult at match row: expected match at line 1, got %v", match)
	}

	// Test out of range
	s.Cursor = 100
	sess, msg, match = s.GetSelectedResult()
	if sess != nil || msg != nil || match != nil {
		t.Errorf("GetSelectedResult out of range: expected all nil, got %v, %v, %v", sess, msg, match)
	}
}

func TestTotalMatches(t *testing.T) {
	s := makeTestSearchState()
	// session1: 2 + 1 matches, session2: 1 match = 4 total
	expected := 4
	if got := s.TotalMatches(); got != expected {
		t.Errorf("TotalMatches() = %d, want %d", got, expected)
	}
}

func TestSessionCount(t *testing.T) {
	s := makeTestSearchState()
	expected := 2
	if got := s.SessionCount(); got != expected {
		t.Errorf("SessionCount() = %d, want %d", got, expected)
	}
}

func TestToggleCollapse(t *testing.T) {
	s := makeTestSearchState()

	// On session row
	s.Cursor = 0
	if !s.ToggleCollapse() {
		t.Error("ToggleCollapse should return true on session row")
	}
	if !s.Results[0].Collapsed {
		t.Error("Session should be collapsed after toggle")
	}

	// Toggle again
	if !s.ToggleCollapse() {
		t.Error("ToggleCollapse should return true on session row")
	}
	if s.Results[0].Collapsed {
		t.Error("Session should be expanded after second toggle")
	}

	// On non-session row
	s.Cursor = 2 // match row
	if s.ToggleCollapse() {
		t.Error("ToggleCollapse should return false on non-session row")
	}
}

func TestExpandCollapseAll(t *testing.T) {
	s := makeTestSearchState()

	s.CollapseAll()
	for i, sr := range s.Results {
		if !sr.Collapsed {
			t.Errorf("Session %d should be collapsed after CollapseAll", i)
		}
	}

	s.ExpandAll()
	for i, sr := range s.Results {
		if sr.Collapsed {
			t.Errorf("Session %d should be expanded after ExpandAll", i)
		}
	}
}

func TestMoveToSession(t *testing.T) {
	s := makeTestSearchState()

	// Start at a match row
	s.Cursor = 3 // session1/msg1/match1
	s.MoveToSession()
	if s.Cursor != 0 {
		t.Errorf("MoveToSession from match: cursor = %d, want 0", s.Cursor)
	}

	// Start at session2's match
	s.Cursor = 8 // session2/msg3/match0
	s.MoveToSession()
	if s.Cursor != 6 {
		t.Errorf("MoveToSession from session2 match: cursor = %d, want 6", s.Cursor)
	}

	// Already at session row - should stay
	s.Cursor = 0
	s.MoveToSession()
	if s.Cursor != 0 {
		t.Errorf("MoveToSession from session row: cursor changed to %d", s.Cursor)
	}
}

func TestFirstMatchInSession(t *testing.T) {
	s := makeTestSearchState()

	// First match in session1 is at flat index 2
	if got := s.FirstMatchInSession(0); got != 2 {
		t.Errorf("FirstMatchInSession(0) = %d, want 2", got)
	}

	// First match in session2 is at flat index 8
	if got := s.FirstMatchInSession(1); got != 8 {
		t.Errorf("FirstMatchInSession(1) = %d, want 8", got)
	}

	// Collapse session1 - should return -1
	s.Results[0].Collapsed = true
	if got := s.FirstMatchInSession(0); got != -1 {
		t.Errorf("FirstMatchInSession(0) with collapsed = %d, want -1", got)
	}

	// Out of range
	if got := s.FirstMatchInSession(5); got != -1 {
		t.Errorf("FirstMatchInSession(5) = %d, want -1", got)
	}
}

func TestContentSearchEnsureCursorVisible(t *testing.T) {
	s := makeTestSearchState()
	viewportHeight := 3

	// Cursor at 0, should be visible
	s.Cursor = 0
	s.ScrollOffset = 0
	s.EnsureCursorVisible(viewportHeight)
	if s.ScrollOffset != 0 {
		t.Errorf("EnsureCursorVisible: scroll should stay at 0, got %d", s.ScrollOffset)
	}

	// Cursor at 5, viewport is 0-2, should scroll
	s.Cursor = 5
	s.ScrollOffset = 0
	s.EnsureCursorVisible(viewportHeight)
	if s.ScrollOffset != 3 {
		t.Errorf("EnsureCursorVisible: scroll should be 3, got %d", s.ScrollOffset)
	}

	// Cursor at 2, viewport is 3-5, should scroll back
	s.Cursor = 2
	s.ScrollOffset = 3
	s.EnsureCursorVisible(viewportHeight)
	if s.ScrollOffset != 2 {
		t.Errorf("EnsureCursorVisible: scroll should be 2, got %d", s.ScrollOffset)
	}
}

func TestClampCursor(t *testing.T) {
	s := makeTestSearchState()
	flatLen := s.FlatLen() // 9

	// Negative cursor
	s.Cursor = -5
	s.ClampCursor()
	if s.Cursor != 0 {
		t.Errorf("ClampCursor with negative: got %d, want 0", s.Cursor)
	}

	// Beyond max
	s.Cursor = 100
	s.ClampCursor()
	if s.Cursor != flatLen-1 {
		t.Errorf("ClampCursor beyond max: got %d, want %d", s.Cursor, flatLen-1)
	}

	// Valid cursor - should stay
	s.Cursor = 5
	s.ClampCursor()
	if s.Cursor != 5 {
		t.Errorf("ClampCursor valid: got %d, want 5", s.Cursor)
	}
}

func TestEmptyState(t *testing.T) {
	s := NewContentSearchState()

	if s.FlatLen() != 0 {
		t.Errorf("Empty state FlatLen() = %d, want 0", s.FlatLen())
	}

	if s.TotalMatches() != 0 {
		t.Errorf("Empty state TotalMatches() = %d, want 0", s.TotalMatches())
	}

	if s.SessionCount() != 0 {
		t.Errorf("Empty state SessionCount() = %d, want 0", s.SessionCount())
	}

	sess, msg, match := s.GetSelectedResult()
	if sess != nil || msg != nil || match != nil {
		t.Error("Empty state GetSelectedResult should return all nil")
	}

	// Should not panic
	s.ClampCursor()
	s.EnsureCursorVisible(10)
	s.ToggleCollapse()
	s.ExpandAll()
	s.CollapseAll()
}

func TestReset(t *testing.T) {
	s := makeTestSearchState()
	s.UseRegex = true
	s.CaseSensitive = true
	s.Cursor = 5
	s.ScrollOffset = 3
	s.IsSearching = true
	s.Error = "some error"

	s.Reset()

	if s.Query != "" {
		t.Errorf("Reset: Query = %q, want empty", s.Query)
	}
	if len(s.Results) != 0 {
		t.Errorf("Reset: Results len = %d, want 0", len(s.Results))
	}
	if s.Cursor != 0 {
		t.Errorf("Reset: Cursor = %d, want 0", s.Cursor)
	}
	if s.ScrollOffset != 0 {
		t.Errorf("Reset: ScrollOffset = %d, want 0", s.ScrollOffset)
	}
	if s.IsSearching {
		t.Error("Reset: IsSearching should be false")
	}
	if s.Error != "" {
		t.Errorf("Reset: Error = %q, want empty", s.Error)
	}

	// Preferences should be preserved
	if !s.UseRegex {
		t.Error("Reset: UseRegex should be preserved")
	}
	if !s.CaseSensitive {
		t.Error("Reset: CaseSensitive should be preserved")
	}
}

// View rendering tests

func TestRenderContentSearchModal(t *testing.T) {
	s := makeTestSearchState()

	// Should not panic and produce non-empty output
	output := renderContentSearchModal(s, 100, 40)
	if output == "" {
		t.Error("renderContentSearchModal should produce non-empty output")
	}

	// Test with empty state
	emptyState := NewContentSearchState()
	output = renderContentSearchModal(emptyState, 100, 40)
	if output == "" {
		t.Error("renderContentSearchModal with empty state should produce non-empty output")
	}

	// Test with searching state
	searchingState := NewContentSearchState()
	searchingState.Query = "test"
	searchingState.IsSearching = true
	output = renderContentSearchModal(searchingState, 100, 40)
	if output == "" {
		t.Error("renderContentSearchModal with searching state should produce non-empty output")
	}

	// Test with error state
	errorState := NewContentSearchState()
	errorState.Query = "test"
	errorState.Error = "Search error occurred"
	output = renderContentSearchModal(errorState, 100, 40)
	if output == "" {
		t.Error("renderContentSearchModal with error state should produce non-empty output")
	}
}

func TestRenderSessionHeader(t *testing.T) {
	sr := SessionSearchResult{
		Session: adapter.Session{
			ID:        "test-session",
			Name:      "Test Session",
			AdapterID: "claude-code",
			UpdatedAt: time.Now().Add(-2 * time.Hour),
		},
		Messages: []adapter.MessageMatch{
			{Matches: []adapter.ContentMatch{{}, {}}},
		},
		Collapsed: false,
	}

	// Expanded state
	output := renderSessionHeader(sr, false, 80)
	if output == "" {
		t.Error("renderSessionHeader should produce non-empty output")
	}
	if !containsAny(output, "Test Session", "\u25bc") {
		t.Errorf("renderSessionHeader output missing expected content: %s", output)
	}

	// Collapsed state
	sr.Collapsed = true
	output = renderSessionHeader(sr, false, 80)
	if !containsAny(output, "\u25b6") {
		t.Errorf("renderSessionHeader collapsed output missing chevron: %s", output)
	}

	// Selected state
	output = renderSessionHeader(sr, true, 80)
	if output == "" {
		t.Error("renderSessionHeader selected should produce non-empty output")
	}
}

func TestRenderMessageHeader(t *testing.T) {
	msg := adapter.MessageMatch{
		MessageID: "msg1",
		Role:      "user",
		Timestamp: time.Now(),
		Matches: []adapter.ContentMatch{
			{LineText: "This is a test message"},
		},
	}

	output := renderMessageHeader(msg, false, 80)
	if output == "" {
		t.Error("renderMessageHeader should produce non-empty output")
	}
	if !containsAny(output, "[User]", "test message") {
		t.Errorf("renderMessageHeader output missing expected content: %s", output)
	}

	// Selected state
	output = renderMessageHeader(msg, true, 80)
	if output == "" {
		t.Error("renderMessageHeader selected should produce non-empty output")
	}

	// Assistant role (truncated to 8 chars as "Assistan")
	msg.Role = "assistant"
	output = renderMessageHeader(msg, false, 80)
	if !containsAny(output, "[Assistan]") {
		t.Errorf("renderMessageHeader assistant output missing role: %s", output)
	}
}

func TestRenderMatchLine(t *testing.T) {
	match := adapter.ContentMatch{
		BlockType: "text",
		LineNo:    42,
		LineText:  "This is a test line with match",
		ColStart:  10,
		ColEnd:    14, // "test"
	}

	output := renderMatchLine(match, "test", false, false, 80, 0, 0, 0)
	if output == "" {
		t.Error("renderMatchLine should produce non-empty output")
	}
	if !containsAny(output, "Line 42") {
		t.Errorf("renderMatchLine output missing line number: %s", output)
	}

	// Selected state
	output = renderMatchLine(match, "test", false, true, 80, 0, 0, 0)
	if output == "" {
		t.Error("renderMatchLine selected should produce non-empty output")
	}

	// Long line that needs truncation
	longMatch := adapter.ContentMatch{
		BlockType: "text",
		LineNo:    1,
		LineText:  "This is a very long line that should be truncated to fit within the display width limit for proper rendering in the terminal window",
		ColStart:  30,
		ColEnd:    39, // "truncated"
	}
	output = renderMatchLine(longMatch, "truncated", false, false, 60, 0, 0, 0)
	if output == "" {
		t.Error("renderMatchLine with long text should produce non-empty output")
	}
}

func TestHighlightMatchRunes(t *testing.T) {
	text := "This is a test string"

	// Valid match (rune indices, same as byte indices for ASCII)
	output := highlightMatchRunes(text, 10, 14)
	if output == "" {
		t.Error("highlightMatchRunes should produce non-empty output")
	}

	// Invalid ranges should return styled text without panic
	output = highlightMatchRunes(text, -1, 5)
	if output == "" {
		t.Error("highlightMatchRunes with negative start should produce output")
	}

	output = highlightMatchRunes(text, 0, 100)
	if output == "" {
		t.Error("highlightMatchRunes with end beyond length should produce output")
	}

	output = highlightMatchRunes(text, 10, 5) // start > end
	if output == "" {
		t.Error("highlightMatchRunes with invalid range should produce output")
	}

	// Test with UTF-8 multi-byte characters
	utf8Text := "Hello 世界 emoji 🎉 test"
	// "Hello " = 6 chars, "世界" = 2 chars (but 6 bytes each), " emoji " = 7 chars, "🎉" = 1 char (4 bytes)
	// Rune indices: H=0, e=1, l=2, l=3, o=4, ' '=5, 世=6, 界=7, ' '=8, e=9...
	output = highlightMatchRunes(utf8Text, 6, 8) // should highlight "世界"
	if output == "" {
		t.Error("highlightMatchRunes with UTF-8 should produce non-empty output")
	}
}

// TestHighlightAllMatches tests the multi-occurrence highlight function (td-c24c84).
func TestHighlightAllMatches(t *testing.T) {
	// Multiple occurrences - verify function runs without error
	text := "test case: test passed, test failed"
	output := highlightAllMatches(text, "test", false)
	if output == "" {
		t.Error("highlightAllMatches should produce non-empty output")
	}
	// Output should contain the original text (with or without styling)
	if !strings.Contains(output, "case:") {
		t.Error("highlightAllMatches output should contain parts of original text")
	}

	// Case insensitive matching
	output = highlightAllMatches("Test TEST test TeSt", "test", false)
	if output == "" {
		t.Error("highlightAllMatches case-insensitive should produce output")
	}

	// Case sensitive matching - should only highlight "test" not "Test", "TEST", etc.
	output = highlightAllMatches("Test TEST test TeSt", "test", true)
	if output == "" {
		t.Error("highlightAllMatches case-sensitive should produce output")
	}

	// No matches
	output = highlightAllMatches("hello world", "xyz", false)
	if output == "" {
		t.Error("highlightAllMatches with no matches should produce output")
	}

	// Empty query
	output = highlightAllMatches("hello world", "", false)
	if output == "" {
		t.Error("highlightAllMatches with empty query should produce output")
	}

	// UTF-8 support
	output = highlightAllMatches("世界 hello 世界 world 世界", "世界", false)
	if output == "" {
		t.Error("highlightAllMatches with UTF-8 should produce output")
	}
	// Verify UTF-8 characters are preserved
	if !strings.Contains(output, "hello") {
		t.Error("highlightAllMatches UTF-8 output should preserve ASCII parts")
	}

	// Query longer than text
	output = highlightAllMatches("hi", "hello world", false)
	if output == "" {
		t.Error("highlightAllMatches with query longer than text should produce output")
	}
}

func TestFormatTimeAgo(t *testing.T) {
	now := time.Now()

	tests := []struct {
		time     time.Time
		expected string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-1 * time.Minute), "1m ago"},
		{now.Add(-2 * time.Hour), "2h ago"},
		{now.Add(-1 * time.Hour), "1h ago"},
		{now.Add(-36 * time.Hour), "yesterday"},
		{now.Add(-3 * 24 * time.Hour), "3d ago"},
		{now.Add(-2 * 7 * 24 * time.Hour), "2w ago"},
		{time.Time{}, ""}, // Zero time
	}

	for _, tc := range tests {
		got := formatTimeAgo(tc.time)
		if tc.expected != "" && got != tc.expected {
			t.Errorf("formatTimeAgo(%v) = %q, want %q", tc.time, got, tc.expected)
		}
		if tc.expected == "" && got != "" {
			t.Errorf("formatTimeAgo(zero) = %q, want empty", got)
		}
	}
}

// Helper to check if string contains any of the given substrings
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestFlatIdxFor(t *testing.T) {
	s := makeTestSearchState()

	tests := []struct {
		sessIdx  int
		msgIdx   int
		matchIdx int
		want     int
		desc     string
	}{
		{0, -1, -1, 0, "session1"},
		{0, 0, -1, 1, "session1/msg1"},
		{0, 0, 0, 2, "session1/msg1/match0"},
		{0, 0, 1, 3, "session1/msg1/match1"},
		{0, 1, -1, 4, "session1/msg2"},
		{0, 1, 0, 5, "session1/msg2/match0"},
		{1, -1, -1, 6, "session2"},
		{1, 0, -1, 7, "session2/msg3"},
		{1, 0, 0, 8, "session2/msg3/match0"},
		{2, -1, -1, -1, "out of range session"},
		{0, 5, -1, -1, "out of range msg"},
	}

	for _, tc := range tests {
		got := s.flatIdxFor(tc.sessIdx, tc.msgIdx, tc.matchIdx)
		if got != tc.want {
			t.Errorf("flatIdxFor(%d, %d, %d) [%s] = %d, want %d",
				tc.sessIdx, tc.msgIdx, tc.matchIdx, tc.desc, got, tc.want)
		}
	}

	// Test with collapsed session
	s.Results[0].Collapsed = true
	// Items inside collapsed session should return -1
	if got := s.flatIdxFor(0, 0, 0); got != -1 {
		t.Errorf("flatIdxFor inside collapsed session = %d, want -1", got)
	}
	// Session itself should still be reachable
	if got := s.flatIdxFor(0, -1, -1); got != 0 {
		t.Errorf("flatIdxFor collapsed session = %d, want 0", got)
	}
}
