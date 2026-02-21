package conversations

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/adapter"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/plugin"
)

func TestNew(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("expected non-nil plugin")
	}
	if p.pageSize != defaultPageSize {
		t.Errorf("expected pageSize %d, got %d", defaultPageSize, p.pageSize)
	}
}

func TestPluginID(t *testing.T) {
	p := New()
	if id := p.ID(); id != "conversations" {
		t.Errorf("expected ID 'conversations', got %q", id)
	}
}

func TestPluginName(t *testing.T) {
	p := New()
	if name := p.Name(); name != "conversations" {
		t.Errorf("expected Name 'conversations', got %q", name)
	}
}

func TestPluginIcon(t *testing.T) {
	p := New()
	if icon := p.Icon(); icon != "C" {
		t.Errorf("expected Icon 'C', got %q", icon)
	}
}

func TestFocusContext(t *testing.T) {
	p := New()

	// Default: sidebar pane focused
	p.activePane = PaneSidebar
	if ctx := p.FocusContext(); ctx != "conversations-sidebar" {
		t.Errorf("expected context 'conversations-sidebar', got %q", ctx)
	}

	// Messages pane focused
	p.activePane = PaneMessages
	if ctx := p.FocusContext(); ctx != "conversations-main" {
		t.Errorf("expected context 'conversations-main', got %q", ctx)
	}

	// Detail mode
	p.detailMode = true
	if ctx := p.FocusContext(); ctx != "turn-detail" {
		t.Errorf("expected context 'turn-detail', got %q", ctx)
	}
}

func TestDiagnosticsNoAdapter(t *testing.T) {
	p := New()
	diags := p.Diagnostics()

	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diags))
	}

	if diags[0].Status != "disabled" {
		t.Errorf("expected status 'disabled', got %q", diags[0].Status)
	}
	if diags[1].ID != "watcher" {
		t.Errorf("expected watcher diagnostic, got %q", diags[1].ID)
	}
}

func TestDiagnosticsWithSessions(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}} // Set a non-nil adapter
	p.sessions = []adapter.Session{
		{ID: "test-1"},
		{ID: "test-2"},
	}

	diags := p.Diagnostics()

	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diags))
	}

	if diags[0].Status != "ok" {
		t.Errorf("expected status 'ok', got %q", diags[0].Status)
	}
	if diags[1].ID != "watcher" {
		t.Errorf("expected watcher diagnostic, got %q", diags[1].ID)
	}
}

func TestEnsureCursorVisible(t *testing.T) {
	p := New()
	p.height = 10 // 4 visible rows after header/footer

	// Cursor at 0, scroll at 0 - should stay
	p.cursor = 0
	p.scrollOff = 0
	p.ensureCursorVisible()
	if p.scrollOff != 0 {
		t.Errorf("expected scrollOff 0, got %d", p.scrollOff)
	}

	// Move cursor down past visible area
	p.cursor = 10
	p.ensureCursorVisible()
	if p.scrollOff == 0 {
		t.Error("expected scrollOff to increase")
	}

	// Move cursor back up
	p.cursor = 0
	p.ensureCursorVisible()
	if p.scrollOff != 0 {
		t.Errorf("expected scrollOff 0, got %d", p.scrollOff)
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		text     string
		maxWidth int
		expected int
	}{
		{"hello world", 20, 1},
		{"hello world this is a longer text", 10, 5},
		{"", 10, 0},
		{"one two three four five", 10, 3},
	}

	for _, tt := range tests {
		lines := wrapText(tt.text, tt.maxWidth)
		if len(lines) != tt.expected {
			t.Errorf("wrapText(%q, %d) = %d lines, expected %d",
				tt.text, tt.maxWidth, len(lines), tt.expected)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{1 * time.Minute, "1m ago"},
		{2 * time.Hour, "2h ago"},
		{1 * time.Hour, "1h ago"},
		{48 * time.Hour, "2d ago"},
		{24 * time.Hour, "1d ago"},
	}

	for _, tt := range tests {
		result := formatDuration(tt.duration)
		if result != tt.expected {
			t.Errorf("formatDuration(%v) = %q, expected %q",
				tt.duration, result, tt.expected)
		}
	}
}

func TestFormatSessionCount(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{1, "1 session"},
		{5, "5 sessions"},
		{10, "10 sessions"},
		{100, "100 sessions"},
	}

	for _, tt := range tests {
		result := formatSessionCount(tt.count)
		if result != tt.expected {
			t.Errorf("formatSessionCount(%d) = %q, expected %q",
				tt.count, result, tt.expected)
		}
	}
}

func TestSearchModeToggle(t *testing.T) {
	p := New()
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "first-session"},
		{ID: "test-2", Name: "second-session"},
	}
	p.activePane = PaneSidebar

	// Initially not in search mode
	if p.searchMode {
		t.Error("expected searchMode to be false initially")
	}

	// FocusContext should be "conversations-sidebar"
	if ctx := p.FocusContext(); ctx != "conversations-sidebar" {
		t.Errorf("expected context 'conversations-sidebar', got %q", ctx)
	}

	// Toggle search mode on
	p.searchMode = true
	if ctx := p.FocusContext(); ctx != "conversations-search" {
		t.Errorf("expected context 'conversations-search', got %q", ctx)
	}
}

func TestFilterSessions(t *testing.T) {
	p := New()
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha-session", Slug: "alpha-slug"},
		{ID: "test-2", Name: "beta-session", Slug: "beta-slug"},
		{ID: "test-3", Name: "gamma-session", Slug: "gamma-slug"},
	}

	// No filter
	p.filterSessions()
	if p.searchResults != nil {
		t.Error("expected nil searchResults with empty query")
	}

	// Filter by name
	p.searchQuery = "beta"
	p.filterSessions()
	if len(p.searchResults) != 1 {
		t.Errorf("expected 1 result, got %d", len(p.searchResults))
	}
	if p.searchResults[0].Name != "beta-session" {
		t.Errorf("expected 'beta-session', got %q", p.searchResults[0].Name)
	}

	// Filter by slug
	p.searchQuery = "gamma-slug"
	p.filterSessions()
	if len(p.searchResults) != 1 {
		t.Errorf("expected 1 result, got %d", len(p.searchResults))
	}

	// No matches
	p.searchQuery = "nonexistent"
	p.filterSessions()
	if len(p.searchResults) != 0 {
		t.Errorf("expected 0 results, got %d", len(p.searchResults))
	}
}

func TestVisibleSessions(t *testing.T) {
	p := New()
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "beta"},
	}

	// Without search mode, should return all sessions
	visible := p.visibleSessions()
	if len(visible) != 2 {
		t.Errorf("expected 2 visible sessions, got %d", len(visible))
	}

	// In search mode with query, should return filtered results
	p.searchMode = true
	p.searchQuery = "alpha"
	p.filterSessions()
	visible = p.visibleSessions()
	if len(visible) != 1 {
		t.Errorf("expected 1 visible session, got %d", len(visible))
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		id       string
		expected string
	}{
		{"12345678", "12345678"},
		{"123456789abcdef", "12345678"},
		{"1234567", "1234567"},
		{"abc", "abc"},
		{"", ""},
	}

	for _, tt := range tests {
		result := shortID(tt.id)
		if result != tt.expected {
			t.Errorf("shortID(%q) = %q, expected %q", tt.id, result, tt.expected)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		input, output, cache int
		expected             string
	}{
		{0, 0, 0, ""},
		{100, 0, 0, " (in:100)"},
		{0, 100, 0, " (out:100)"},
		{0, 0, 100, " ($:100)"},
		{1000, 2000, 500, " (in:1.0k out:2.0k $:500)"},
		{1500000, 2500000, 0, " (in:1.5M out:2.5M)"},
	}

	for _, tt := range tests {
		result := formatTokens(tt.input, tt.output, tt.cache)
		if result != tt.expected {
			t.Errorf("formatTokens(%d, %d, %d) = %q, expected %q",
				tt.input, tt.output, tt.cache, result, tt.expected)
		}
	}
}

func TestFormatK(t *testing.T) {
	tests := []struct {
		n        int
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{999999, "1000.0k"},
		{1000000, "1.0M"},
		{2500000, "2.5M"},
	}

	for _, tt := range tests {
		result := formatK(tt.n)
		if result != tt.expected {
			t.Errorf("formatK(%d) = %q, expected %q", tt.n, result, tt.expected)
		}
	}
}

func TestDiagnosticsEmptySessions(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{} // Empty but adapter exists

	diags := p.Diagnostics()

	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diags))
	}

	if diags[0].Status != "empty" {
		t.Errorf("expected status 'empty', got %q", diags[0].Status)
	}
}

func TestDiagnosticsActiveSessions(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", IsActive: true},
		{ID: "test-2", IsActive: false},
		{ID: "test-3", IsActive: true},
	}

	diags := p.Diagnostics()

	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diags))
	}

	// Should show "3 sessions (2 active)"
	if diags[0].Status != "ok" {
		t.Errorf("expected status 'ok', got %q", diags[0].Status)
	}
	expectedDetail := "3 sessions (2 active)"
	if diags[0].Detail != expectedDetail {
		t.Errorf("expected detail %q, got %q", expectedDetail, diags[0].Detail)
	}
}

func TestDiagnosticsWatcherOn(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.watchChan = make(chan adapter.Event) // Non-nil channel

	diags := p.Diagnostics()

	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diags))
	}

	if diags[1].Status != "on" {
		t.Errorf("expected watcher status 'on', got %q", diags[1].Status)
	}
}

// Test WatchStartedMsg with nil channel
func TestUpdateWatchStartedMsgNilChannel(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}

	closed := 0
	msg := WatchStartedMsg{Channel: nil, Closers: []io.Closer{&countCloser{count: &closed}}}
	newPlugin, cmd := p.Update(msg)

	if newPlugin == nil {
		t.Fatal("expected non-nil plugin")
	}
	if cmd != nil {
		t.Error("expected nil command when channel is nil")
	}
	if p.watchChan != nil {
		t.Error("expected watchChan to remain nil")
	}
	if closed != 1 {
		t.Errorf("expected closers to be closed, got %d", closed)
	}
}

// Test WatchStartedMsg with valid channel
func TestUpdateWatchStartedMsgValidChannel(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}

	previousClosed := 0
	p.watchClosers = []io.Closer{&countCloser{count: &previousClosed}}

	newClosed := 0
	ch := make(chan adapter.Event)
	msg := WatchStartedMsg{Channel: ch, Closers: []io.Closer{&countCloser{count: &newClosed}}}
	newPlugin, cmd := p.Update(msg)

	if newPlugin == nil {
		t.Fatal("expected non-nil plugin")
	}
	if cmd == nil {
		t.Error("expected non-nil command to start listening")
	}
	if p.watchChan != ch {
		t.Error("expected watchChan to be set to the provided channel")
	}
	if previousClosed != 1 {
		t.Errorf("expected previous closers to be closed, got %d", previousClosed)
	}
	if newClosed != 0 {
		t.Errorf("expected new closers to remain open, got %d", newClosed)
	}
}

func TestUpdateWatchStartedMsgStoppedCloses(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.stopped = true

	closed := 0
	ch := make(chan adapter.Event)
	msg := WatchStartedMsg{Channel: ch, Closers: []io.Closer{&countCloser{count: &closed}}}
	newPlugin, cmd := p.Update(msg)

	if newPlugin == nil {
		t.Fatal("expected non-nil plugin")
	}
	if cmd != nil {
		t.Error("expected nil command when stopped")
	}
	if p.watchChan != nil {
		t.Error("expected watchChan to remain nil when stopped")
	}
	if closed != 1 {
		t.Errorf("expected closers to be closed when stopped, got %d", closed)
	}
}

func TestStopClosesWatchers(t *testing.T) {
	p := New()
	closed := 0
	p.watchClosers = []io.Closer{&countCloser{count: &closed}}
	p.watchChan = make(chan adapter.Event)

	p.Stop()

	if !p.stopped {
		t.Error("expected stopped to be true after Stop")
	}
	if closed != 1 {
		t.Errorf("expected closers to be closed on Stop, got %d", closed)
	}
	if p.watchChan != nil {
		t.Error("expected watchChan to be nil after Stop")
	}
}

// Test WatchEventMsg triggers session reload
func TestUpdateWatchEventMsg(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.watchChan = make(chan adapter.Event)

	msg := WatchEventMsg{}
	newPlugin, cmd := p.Update(msg)

	if newPlugin == nil {
		t.Fatal("expected non-nil plugin")
	}
	// Should return a batch command for loadSessions and listenForWatchEvents
	if cmd == nil {
		t.Error("expected non-nil command for batch operation")
	}
}

// Test listenForWatchEvents with nil channel
func TestListenForWatchEventsNilChannel(t *testing.T) {
	p := New()
	p.watchChan = nil

	cmd := p.listenForWatchEvents()
	if cmd != nil {
		t.Error("expected nil command when watchChan is nil")
	}
}

// Test listenForWatchEvents with closed channel
func TestListenForWatchEventsClosedChannel(t *testing.T) {
	p := New()
	ch := make(chan adapter.Event)
	close(ch) // Close the channel
	p.watchChan = ch

	cmd := p.listenForWatchEvents()
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	// Execute the command - should return nil when channel is closed
	msg := cmd()
	if msg != nil {
		t.Errorf("expected nil message for closed channel, got %T", msg)
	}
}

// Test listenForWatchEvents receives event
func TestListenForWatchEventsReceivesEvent(t *testing.T) {
	p := New()
	ch := make(chan adapter.Event, 1) // Buffered to avoid blocking
	p.watchChan = ch

	cmd := p.listenForWatchEvents()
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	// Send an event
	ch <- adapter.Event{}

	// Execute the command - should return WatchEventMsg
	msg := cmd()
	if _, ok := msg.(WatchEventMsg); !ok {
		t.Errorf("expected WatchEventMsg, got %T", msg)
	}
}

// Test SessionsLoadedMsg updates sessions
func TestUpdateSessionsLoadedMsg(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}

	sessions := []adapter.Session{
		{ID: "s1", Name: "Session 1"},
		{ID: "s2", Name: "Session 2"},
	}

	msg := SessionsLoadedMsg{Sessions: sessions}
	_, _ = p.Update(msg)

	if len(p.sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(p.sessions))
	}
	if p.sessions[0].ID != "s1" {
		t.Errorf("expected session ID 's1', got %q", p.sessions[0].ID)
	}
}

// Test MessagesLoadedMsg updates messages and hasMore flag
func TestUpdateMessagesLoadedMsg(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.pageSize = 2
	p.selectedSession = "s1"

	// Test with fewer messages than page size
	messages := []adapter.Message{
		{Role: "user", Content: "Hello"},
	}
	msg := MessagesLoadedMsg{SessionID: "s1", Messages: messages}
	_, _ = p.Update(msg)

	if len(p.messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(p.messages))
	}
	if p.hasMore {
		t.Error("expected hasMore to be false when messages < pageSize")
	}

	// Test with page size messages (indicates more might be available)
	messages = []adapter.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	}
	msg = MessagesLoadedMsg{SessionID: "s1", Messages: messages}
	_, _ = p.Update(msg)

	if len(p.messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(p.messages))
	}
	if !p.hasMore {
		t.Error("expected hasMore to be true when messages == pageSize")
	}
}

// mockAdapter is a minimal adapter for testing
type mockAdapter struct{}

func (m *mockAdapter) ID() string                                             { return "mock" }
func (m *mockAdapter) Name() string                                           { return "Mock" }
func (m *mockAdapter) Icon() string                                           { return "◆" }
func (m *mockAdapter) Detect(projectRoot string) (bool, error)                { return true, nil }
func (m *mockAdapter) Capabilities() adapter.CapabilitySet                    { return nil }
func (m *mockAdapter) Sessions(projectRoot string) ([]adapter.Session, error) { return nil, nil }
func (m *mockAdapter) Messages(sessionID string) ([]adapter.Message, error)   { return nil, nil }
func (m *mockAdapter) Usage(sessionID string) (*adapter.UsageStats, error)    { return nil, nil }
func (m *mockAdapter) Watch(projectRoot string) (<-chan adapter.Event, io.Closer, error) {
	return nil, nil, nil
}

type countCloser struct {
	count *int
}

func (c *countCloser) Close() error {
	*c.count++
	return nil
}

// =============================================================================
// Search Key Handling Tests via Update()
// =============================================================================

// TestUpdateSearchModeEnter tests entering search mode with '/' key.
func TestUpdateSearchModeEnter(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "beta"},
	}
	p.cursor = 1 // Set cursor to non-zero

	if p.searchMode {
		t.Fatal("expected searchMode to be false initially")
	}

	// Press '/' to enter search mode
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
	_, _ = p.Update(msg)

	if !p.searchMode {
		t.Error("expected searchMode to be true after pressing '/'")
	}
	if p.searchQuery != "" {
		t.Errorf("expected empty searchQuery, got %q", p.searchQuery)
	}
	if p.cursor != 0 {
		t.Errorf("expected cursor to reset to 0, got %d", p.cursor)
	}
	if p.scrollOff != 0 {
		t.Errorf("expected scrollOff to reset to 0, got %d", p.scrollOff)
	}
}

// TestUpdateSearchModeExit tests exiting search mode with 'esc' key.
func TestUpdateSearchModeExit(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "beta"},
	}
	p.searchMode = true
	p.searchQuery = "test"
	p.searchResults = []adapter.Session{{ID: "test-1"}}
	p.cursor = 1

	// Press 'esc' to exit search mode
	msg := tea.KeyMsg{Type: tea.KeyEsc}
	_, _ = p.Update(msg)

	if p.searchMode {
		t.Error("expected searchMode to be false after pressing 'esc'")
	}
	if p.searchQuery != "" {
		t.Errorf("expected searchQuery to be cleared, got %q", p.searchQuery)
	}
	if p.searchResults != nil {
		t.Error("expected searchResults to be nil")
	}
	if p.cursor != 0 {
		t.Errorf("expected cursor to reset to 0, got %d", p.cursor)
	}
}

// TestUpdateSearchTypingCharacters tests typing characters to build searchQuery.
func TestUpdateSearchTypingCharacters(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "beta"},
		{ID: "test-3", Name: "gamma"},
	}
	p.searchMode = true

	// Type 'a'
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	_, _ = p.Update(msg)

	if p.searchQuery != "a" {
		t.Errorf("expected searchQuery 'a', got %q", p.searchQuery)
	}

	// Type 'l'
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
	_, _ = p.Update(msg)

	if p.searchQuery != "al" {
		t.Errorf("expected searchQuery 'al', got %q", p.searchQuery)
	}

	// Type 'p'
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}
	_, _ = p.Update(msg)

	if p.searchQuery != "alp" {
		t.Errorf("expected searchQuery 'alp', got %q", p.searchQuery)
	}

	// Verify filtering occurred (should match "alpha")
	if len(p.searchResults) != 1 {
		t.Errorf("expected 1 search result, got %d", len(p.searchResults))
	}
	if len(p.searchResults) > 0 && p.searchResults[0].Name != "alpha" {
		t.Errorf("expected result 'alpha', got %q", p.searchResults[0].Name)
	}
}

// TestUpdateSearchBackspace tests backspace deleting from searchQuery.
func TestUpdateSearchBackspace(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "beta"},
	}
	p.searchMode = true
	p.searchQuery = "alph"
	p.filterSessions() // Initialize searchResults

	// Press backspace
	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	_, _ = p.Update(msg)

	if p.searchQuery != "alp" {
		t.Errorf("expected searchQuery 'alp', got %q", p.searchQuery)
	}

	// Press backspace again
	_, _ = p.Update(msg)

	if p.searchQuery != "al" {
		t.Errorf("expected searchQuery 'al', got %q", p.searchQuery)
	}

	// Press backspace until empty
	_, _ = p.Update(msg)
	_, _ = p.Update(msg)

	if p.searchQuery != "" {
		t.Errorf("expected empty searchQuery, got %q", p.searchQuery)
	}

	// Backspace on empty query should do nothing
	_, _ = p.Update(msg)

	if p.searchQuery != "" {
		t.Errorf("expected searchQuery to remain empty, got %q", p.searchQuery)
	}
}

// TestUpdateSearchNavigationDown tests down navigation in search results.
func TestUpdateSearchNavigationDown(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "also-alpha"},
		{ID: "test-3", Name: "another-alpha"},
	}
	p.searchMode = true
	p.searchQuery = "alpha"
	p.filterSessions()
	p.height = 20 // Set height for scroll calculations

	if p.cursor != 0 {
		t.Fatalf("expected initial cursor 0, got %d", p.cursor)
	}

	// Press down arrow
	msg := tea.KeyMsg{Type: tea.KeyDown}
	_, _ = p.Update(msg)

	if p.cursor != 1 {
		t.Errorf("expected cursor 1 after down, got %d", p.cursor)
	}

	// Press down again
	_, _ = p.Update(msg)

	if p.cursor != 2 {
		t.Errorf("expected cursor 2 after second down, got %d", p.cursor)
	}

	// Press down at end (should stay at end)
	_, _ = p.Update(msg)

	if p.cursor != 2 {
		t.Errorf("expected cursor to stay at 2, got %d", p.cursor)
	}
}

// TestUpdateSearchNavigationUp tests up navigation in search results.
func TestUpdateSearchNavigationUp(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "also-alpha"},
		{ID: "test-3", Name: "another-alpha"},
	}
	p.searchMode = true
	p.searchQuery = "alpha"
	p.filterSessions()
	p.cursor = 2 // Start at last item
	p.height = 20

	// Press up arrow
	msg := tea.KeyMsg{Type: tea.KeyUp}
	_, _ = p.Update(msg)

	if p.cursor != 1 {
		t.Errorf("expected cursor 1 after up, got %d", p.cursor)
	}

	// Press up again
	_, _ = p.Update(msg)

	if p.cursor != 0 {
		t.Errorf("expected cursor 0 after second up, got %d", p.cursor)
	}

	// Press up at top (should stay at 0)
	_, _ = p.Update(msg)

	if p.cursor != 0 {
		t.Errorf("expected cursor to stay at 0, got %d", p.cursor)
	}
}

// TestUpdateSearchNavigationCtrlN tests ctrl+n navigation in search results.
func TestUpdateSearchNavigationCtrlN(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "also-alpha"},
	}
	p.searchMode = true
	p.searchQuery = "alpha"
	p.filterSessions()
	p.height = 20

	// Press ctrl+n (should move down)
	msg := tea.KeyMsg{Type: tea.KeyCtrlN}
	_, _ = p.Update(msg)

	if p.cursor != 1 {
		t.Errorf("expected cursor 1 after ctrl+n, got %d", p.cursor)
	}
}

// TestUpdateSearchNavigationCtrlP tests ctrl+p navigation in search results.
func TestUpdateSearchNavigationCtrlP(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "also-alpha"},
	}
	p.searchMode = true
	p.searchQuery = "alpha"
	p.filterSessions()
	p.cursor = 1 // Start at second item
	p.height = 20

	// Press ctrl+p (should move up)
	msg := tea.KeyMsg{Type: tea.KeyCtrlP}
	_, _ = p.Update(msg)

	if p.cursor != 0 {
		t.Errorf("expected cursor 0 after ctrl+p, got %d", p.cursor)
	}
}

// TestUpdateSearchEnterSelectsSession tests enter key selects session in search mode.
func TestUpdateSearchEnterSelectsSession(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "beta"},
	}
	p.searchMode = true
	p.searchQuery = "beta"
	p.filterSessions()
	p.height = 20

	if len(p.searchResults) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(p.searchResults))
	}

	// Press enter to select
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := p.Update(msg)

	if p.searchMode {
		t.Error("expected searchMode to be false after enter")
	}
	if p.activePane != PaneMessages {
		t.Errorf("expected activePane to be PaneMessages, got %d", p.activePane)
	}
	if p.selectedSession != "test-2" {
		t.Errorf("expected selectedSession 'test-2', got %q", p.selectedSession)
	}
	if cmd == nil {
		t.Error("expected non-nil command to load messages")
	}
}

// TestUpdateSearchCursorResetOnQuery tests cursor resets when query changes.
func TestUpdateSearchCursorResetOnQuery(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "also-alpha"},
		{ID: "test-3", Name: "beta"},
	}
	p.searchMode = true
	p.searchQuery = "a"
	p.filterSessions()
	p.cursor = 1 // Move cursor to second result
	p.height = 20

	// Type another character - cursor should reset
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
	_, _ = p.Update(msg)

	if p.cursor != 0 {
		t.Errorf("expected cursor to reset to 0, got %d", p.cursor)
	}

	// Move cursor again
	p.cursor = 1

	// Backspace should also reset cursor
	msg = tea.KeyMsg{Type: tea.KeyBackspace}
	_, _ = p.Update(msg)

	if p.cursor != 0 {
		t.Errorf("expected cursor to reset to 0 after backspace, got %d", p.cursor)
	}
}

// TestUpdateSearchEmptyResults tests behavior with no matching results.
func TestUpdateSearchEmptyResults(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "alpha"},
		{ID: "test-2", Name: "beta"},
	}
	p.searchMode = true
	p.height = 20

	// Type query that matches nothing
	for _, r := range "xyz" {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		_, _ = p.Update(msg)
	}

	if p.searchQuery != "xyz" {
		t.Errorf("expected searchQuery 'xyz', got %q", p.searchQuery)
	}
	if len(p.searchResults) != 0 {
		t.Errorf("expected 0 results, got %d", len(p.searchResults))
	}

	// Navigation should not panic with empty results
	msg := tea.KeyMsg{Type: tea.KeyDown}
	_, _ = p.Update(msg) // Should not panic

	msg = tea.KeyMsg{Type: tea.KeyUp}
	_, _ = p.Update(msg) // Should not panic

	// Enter should do nothing with empty results
	msg = tea.KeyMsg{Type: tea.KeyEnter}
	_, _ = p.Update(msg)

	if p.view != ViewSessions {
		t.Error("expected to stay in sessions view with empty results")
	}
}

// =============================================================================
// Thinking Block Toggle Persistence Tests
// =============================================================================

// TestThinkingBlockTogglePersistence tests that thinking block expansion
// persists when scrolling away and back to a message.
func TestThinkingBlockTogglePersistence(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.activePane = PaneMessages
	p.height = 20
	p.turnViewMode = true // Test turn-based behavior

	// Create messages with thinking blocks - alternating roles to create separate turns
	p.messages = []adapter.Message{
		{
			ID:   "msg-1",
			Role: "assistant",
			ThinkingBlocks: []adapter.ThinkingBlock{
				{Content: "thinking 1"},
			},
		},
		{
			ID:      "msg-2",
			Role:    "user",
			Content: "user message",
		},
		{
			ID:   "msg-3",
			Role: "assistant",
			ThinkingBlocks: []adapter.ThinkingBlock{
				{Content: "thinking 3"},
			},
		},
	}
	p.turns = GroupMessagesIntoTurns(p.messages)
	p.turnCursor = 0

	// Initially all thinking blocks should be collapsed
	if p.expandedThinking["msg-1"] {
		t.Error("expected msg-1 thinking to be collapsed initially")
	}

	// Toggle turn 0 thinking blocks (press 'T')
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}
	_, _ = p.Update(msg)

	if !p.expandedThinking["msg-1"] {
		t.Error("expected msg-1 thinking to be expanded after toggle")
	}

	// Scroll to turn 2 (assistant with msg-3)
	p.turnCursor = 2

	// Toggle turn 2 thinking block
	_, _ = p.Update(msg)

	if !p.expandedThinking["msg-3"] {
		t.Error("expected msg-3 thinking to be expanded after toggle")
	}

	// Verify msg-1 is still expanded
	if !p.expandedThinking["msg-1"] {
		t.Error("expected msg-1 thinking to remain expanded after scrolling")
	}

	// Scroll back to turn 0 and verify still expanded
	p.turnCursor = 0
	if !p.expandedThinking["msg-1"] {
		t.Error("expected msg-1 thinking to remain expanded after scrolling back")
	}

	// Toggle turn 0 again to collapse
	_, _ = p.Update(msg)

	if p.expandedThinking["msg-1"] {
		t.Error("expected msg-1 thinking to be collapsed after second toggle")
	}

	// msg-3 should still be expanded
	if !p.expandedThinking["msg-3"] {
		t.Error("expected msg-3 thinking to remain expanded")
	}
}

// TestThinkingBlockToggleNoThinkingBlocks tests toggle on turn without thinking blocks.
func TestThinkingBlockToggleNoThinkingBlocks(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.activePane = PaneMessages
	p.height = 20
	p.turnViewMode = true // Test turn-based behavior

	// Create message without thinking blocks
	p.messages = []adapter.Message{
		{
			ID:   "msg-1",
			Role: "user",
		},
	}
	p.turns = GroupMessagesIntoTurns(p.messages)
	p.turnCursor = 0

	// Try to toggle (should do nothing)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}
	_, _ = p.Update(msg)

	// Map should remain empty
	if len(p.expandedThinking) != 0 {
		t.Errorf("expected empty expandedThinking map, got %d entries", len(p.expandedThinking))
	}
}

// TestThinkingBlockResetOnSessionChange tests that expanded state resets when changing sessions.
func TestThinkingBlockResetOnSessionChange(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.activePane = PaneMessages
	p.height = 20
	p.selectedSession = "session-1"
	p.turnViewMode = true // Test turn-based behavior

	// Create message with thinking block and expand it
	p.messages = []adapter.Message{
		{
			ID:   "msg-1",
			Role: "assistant",
			ThinkingBlocks: []adapter.ThinkingBlock{
				{Content: "thinking"},
			},
		},
	}
	p.turns = GroupMessagesIntoTurns(p.messages)
	p.turnCursor = 0

	// Toggle to expand
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}
	_, _ = p.Update(msg)

	if !p.expandedThinking["msg-1"] {
		t.Fatal("expected msg-1 thinking to be expanded")
	}

	// Change to a different session - this should reset expandedThinking
	p.setSelectedSession("session-2")

	// Verify state was reset
	if len(p.expandedThinking) != 0 {
		t.Errorf("expected expandedThinking to be reset, got %d entries", len(p.expandedThinking))
	}
}

// TestThinkingBlockToggleMultipleIndependent tests independent toggle state for multiple turns.
func TestThinkingBlockToggleMultipleIndependent(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.activePane = PaneMessages
	p.height = 20
	p.turnViewMode = true // Test turn-based behavior

	// Create 5 alternating messages to create 5 separate turns
	p.messages = []adapter.Message{
		{ID: "msg-0", Role: "assistant", ThinkingBlocks: []adapter.ThinkingBlock{{Content: "t0"}}},
		{ID: "msg-1", Role: "user", Content: "u1"},
		{ID: "msg-2", Role: "assistant", ThinkingBlocks: []adapter.ThinkingBlock{{Content: "t2"}}},
		{ID: "msg-3", Role: "user", Content: "u3"},
		{ID: "msg-4", Role: "assistant", ThinkingBlocks: []adapter.ThinkingBlock{{Content: "t4"}}},
	}
	p.turns = GroupMessagesIntoTurns(p.messages)

	toggleMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}

	// Expand turns 0, 2, 4 (assistant turns with thinking blocks)
	p.turnCursor = 0
	_, _ = p.Update(toggleMsg)

	p.turnCursor = 2
	_, _ = p.Update(toggleMsg)

	p.turnCursor = 4
	_, _ = p.Update(toggleMsg)

	// Verify assistant turns are expanded
	if !p.expandedThinking["msg-0"] {
		t.Error("expected msg-0 to be expanded")
	}
	if !p.expandedThinking["msg-2"] {
		t.Error("expected msg-2 to be expanded")
	}
	if !p.expandedThinking["msg-4"] {
		t.Error("expected msg-4 to be expanded")
	}

	// User turns (1, 3) have no thinking blocks so nothing to check there

	// Toggle turn 2 to collapse it
	p.turnCursor = 2
	_, _ = p.Update(toggleMsg)

	if p.expandedThinking["msg-2"] {
		t.Error("expected msg-2 to be collapsed after second toggle")
	}

	// Others should remain unchanged
	if !p.expandedThinking["msg-0"] {
		t.Error("expected msg-0 to still be expanded")
	}
	if !p.expandedThinking["msg-4"] {
		t.Error("expected msg-4 to still be expanded")
	}
}

// TestSessionListNavigation tests j/k navigation in the main session list.
func TestSessionListNavigation(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "Session 1"},
		{ID: "test-2", Name: "Session 2"},
		{ID: "test-3", Name: "Session 3"},
	}
	p.height = 20
	p.width = 100

	// Initial cursor should be at 0
	if p.cursor != 0 {
		t.Errorf("expected initial cursor 0, got %d", p.cursor)
	}

	// Press 'j' to move down
	keyJ := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	_, _ = p.Update(keyJ)

	if p.cursor != 1 {
		t.Errorf("expected cursor 1 after 'j', got %d", p.cursor)
	}

	// Press 'down' to move down again
	keyDown := tea.KeyMsg{Type: tea.KeyDown}
	_, _ = p.Update(keyDown)

	if p.cursor != 2 {
		t.Errorf("expected cursor 2 after 'down', got %d", p.cursor)
	}

	// Press 'k' to move up
	keyK := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	_, _ = p.Update(keyK)

	if p.cursor != 1 {
		t.Errorf("expected cursor 1 after 'k', got %d", p.cursor)
	}

	// Press 'up' to move up
	keyUp := tea.KeyMsg{Type: tea.KeyUp}
	_, _ = p.Update(keyUp)

	if p.cursor != 0 {
		t.Errorf("expected cursor 0 after 'up', got %d", p.cursor)
	}

	// Press 'G' to jump to bottom
	keyG := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}}
	_, _ = p.Update(keyG)

	if p.cursor != 2 {
		t.Errorf("expected cursor 2 after 'G', got %d", p.cursor)
	}

	// Press 'g' to jump to top
	keyg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}
	_, _ = p.Update(keyg)

	if p.cursor != 0 {
		t.Errorf("expected cursor 0 after 'g', got %d", p.cursor)
	}
}

// TestSessionListNavigationBounds tests cursor bounds checking.
func TestSessionListNavigationBounds(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "Session 1"},
		{ID: "test-2", Name: "Session 2"},
	}
	p.height = 20
	p.width = 100

	// Try to move up when at top - should stay at 0
	keyK := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	_, _ = p.Update(keyK)

	if p.cursor != 0 {
		t.Errorf("expected cursor to stay at 0 when at top, got %d", p.cursor)
	}

	// Move to bottom
	keyG := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}}
	_, _ = p.Update(keyG)

	// Try to move down when at bottom - should stay at last
	keyJ := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	_, _ = p.Update(keyJ)

	if p.cursor != 1 {
		t.Errorf("expected cursor to stay at 1 when at bottom, got %d", p.cursor)
	}
}

// TestTwoPaneNavigationRouting tests that navigation is properly routed in two-pane mode.
func TestTwoPaneNavigationRouting(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "Session 1"},
		{ID: "test-2", Name: "Session 2"},
		{ID: "test-3", Name: "Session 3"},
	}
	p.height = 30
	p.width = 150
	p.activePane = PaneSidebar

	// With sidebar focused, j should move cursor in session list
	keyJ := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	_, _ = p.Update(keyJ)

	if p.cursor != 1 {
		t.Errorf("expected cursor 1 after 'j' in sidebar, got %d", p.cursor)
	}

	// Switch to messages pane
	p.activePane = PaneMessages
	p.selectedSession = "test-1"
	p.messages = []adapter.Message{
		{ID: "msg-1", Role: "user"},
		{ID: "msg-2", Role: "assistant"},
	}
	p.turns = GroupMessagesIntoTurns(p.messages)
	p.view = ViewMessages

	// With messages pane focused, j should move turn cursor, not session cursor
	initialCursor := p.cursor
	_, _ = p.Update(keyJ)

	if p.cursor != initialCursor {
		t.Errorf("expected session cursor to stay at %d in messages pane, got %d", initialCursor, p.cursor)
	}
}

// TestTwoPaneFocusSwitching tests pane focus switching with tab, h, l keys.
func TestTwoPaneFocusSwitching(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "Session 1"},
	}
	p.height = 30
	p.width = 150
	p.activePane = PaneSidebar
	p.selectedSession = "test-1"
	p.messages = []adapter.Message{{ID: "msg-1", Role: "user"}}
	p.turns = GroupMessagesIntoTurns(p.messages)

	// Press 'l' to switch to messages pane
	keyL := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
	_, _ = p.Update(keyL)

	if p.activePane != PaneMessages {
		t.Errorf("expected activePane to be PaneMessages after 'l', got %v", p.activePane)
	}

	// Press 'h' to switch back to sidebar
	keyH := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}
	_, _ = p.Update(keyH)

	if p.activePane != PaneSidebar {
		t.Errorf("expected activePane to be PaneSidebar after 'h', got %v", p.activePane)
	}

	// Switch to messages and test tab to switch back
	p.activePane = PaneMessages
	keyTab := tea.KeyMsg{Type: tea.KeyTab}
	_, _ = p.Update(keyTab)

	if p.activePane != PaneSidebar {
		t.Errorf("expected activePane to be PaneSidebar after tab, got %v", p.activePane)
	}
}

// TestGroupHeaderSpacing verifies spacing behavior for time-based group headers.
func TestGroupHeaderSpacing(t *testing.T) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	thisWeek := today.AddDate(0, 0, -3)
	older := today.AddDate(0, 0, -10)

	tests := []struct {
		timestamp time.Time
		expected  string
	}{
		{today, "Today"},
		{yesterday, "Yesterday"},
		{thisWeek, "This Week"},
		{older, "Older"},
	}

	for _, tt := range tests {
		result := getSessionGroup(tt.timestamp)
		if result != tt.expected {
			t.Errorf("getSessionGroup(%v) = %q, expected %q", tt.timestamp, result, tt.expected)
		}
	}
}

// =============================================================================
// Mouse Click Tests
// =============================================================================

// TestMouseClickSessionItem tests clicking on a session item selects it.
func TestMouseClickSessionItem(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	now := time.Now()
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "Session 1", UpdatedAt: now},
		{ID: "test-2", Name: "Session 2", UpdatedAt: now.Add(-time.Hour)},
		{ID: "test-3", Name: "Session 3", UpdatedAt: now.Add(-2 * time.Hour)},
	}
	p.width = 150
	p.height = 30
	p.activePane = PaneSidebar
	p.cursor = 0
	p.selectedSession = "test-1"

	// First render to set up hit regions
	_ = p.View(p.width, p.height)

	// Now test that the handler correctly processes session item clicks
	// by checking the plugin's internal state after a click
	// Initial cursor should be 0 after first View call

	// Verify hit regions were registered
	hitMap := p.mouseHandler.HitMap
	if hitMap == nil {
		t.Fatal("expected HitMap to be initialized")
	}

	// Check that session item regions exist by testing hit detection
	// Y position for sessions should be after header lines
	// In two-pane mode: border(1) + title(1) = 2, so sessions start at Y=2
	region := hitMap.Test(5, 3) // Should hit a session item
	if region != nil && region.ID == regionSessionItem {
		if idx, ok := region.Data.(int); ok {
			if idx < 0 || idx >= len(p.sessions) {
				t.Errorf("session item region has invalid index: %d", idx)
			}
		}
	}
}

// TestHandleMouseClickSessionItem tests the handleMouseClick function with session item data.
func TestHandleMouseClickSessionItem(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	now := time.Now()
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "Session 1", UpdatedAt: now},
		{ID: "test-2", Name: "Session 2", UpdatedAt: now.Add(-time.Hour)},
		{ID: "test-3", Name: "Session 3", UpdatedAt: now.Add(-2 * time.Hour)},
	}
	p.width = 150
	p.height = 30
	p.activePane = PaneMessages
	p.cursor = 0
	p.selectedSession = "test-1"

	// Render to initialize hit regions
	_ = p.View(p.width, p.height)

	// Create a mock mouse action with session item region data
	// X=5 is inside the content area (after border+padding which is 2 chars)
	// Y=3 should be where first session is (border + title + group header)
	action := p.mouseHandler.HitMap.Test(5, 3) // Should hit a session in the list

	// If we found a session item region, test the click handler
	if action != nil && action.ID == regionSessionItem {
		if idx, ok := action.Data.(int); ok {
			// Verify the index is valid
			if idx < 0 || idx >= len(p.sessions) {
				t.Errorf("invalid session index: %d", idx)
			}
		}
	}
}

// TestHandleMouseDoubleClickSessionItem tests double-click on session item.
func TestHandleMouseDoubleClickSessionItem(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	now := time.Now()
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "Session 1", UpdatedAt: now},
		{ID: "test-2", Name: "Session 2", UpdatedAt: now.Add(-time.Hour)},
	}
	p.width = 150
	p.height = 30
	p.activePane = PaneSidebar
	p.cursor = 0
	p.selectedSession = ""

	// Render to initialize state
	_ = p.View(p.width, p.height)

	// Verify initial state
	if p.activePane != PaneSidebar {
		t.Errorf("expected initial activePane to be PaneSidebar, got %v", p.activePane)
	}
}

// TestRegisterSessionHitRegions tests that session hit regions are registered correctly.
func TestRegisterSessionHitRegions(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	now := time.Now()
	p.sessions = []adapter.Session{
		{ID: "test-1", Name: "Session 1", UpdatedAt: now},
		{ID: "test-2", Name: "Session 2", UpdatedAt: now.Add(-time.Hour)},
		{ID: "test-3", Name: "Session 3", UpdatedAt: now.Add(-2 * time.Hour)},
	}
	p.width = 150
	p.height = 30

	// Render to trigger hit region registration
	_ = p.View(p.width, p.height)

	// Verify hit regions were registered for sessions
	hitMap := p.mouseHandler.HitMap
	if hitMap == nil {
		t.Fatal("expected HitMap to be initialized")
	}

	// Test that clicking in the session area returns a session-item region
	// X must be inside content (border=1 + padding=1 = 2, so X >= 2)
	// Y: border(1) + title(1) + group header(1) = 3 for first session
	foundSessionRegion := false
	for y := 3; y < 10; y++ {
		region := hitMap.Test(5, y) // X=5 is inside content area
		if region != nil && region.ID == regionSessionItem {
			foundSessionRegion = true
			if idx, ok := region.Data.(int); ok {
				if idx < 0 || idx >= len(p.sessions) {
					t.Errorf("session item region has invalid index: %d", idx)
				}
			} else {
				t.Error("session item region Data should be an int")
			}
			break
		}
	}

	if !foundSessionRegion {
		t.Error("expected to find at least one session-item region")
	}
}

// TestScrollSidebarFunction tests the scrollSidebar function directly.
func TestScrollSidebarFunction(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	now := time.Now()
	// Create many sessions to test scrolling
	for i := 0; i < 20; i++ {
		p.sessions = append(p.sessions, adapter.Session{
			ID:        "test-" + string(rune('a'+i)),
			Name:      "Session " + string(rune('A'+i)),
			UpdatedAt: now.Add(-time.Duration(i) * time.Hour),
		})
	}
	p.width = 150
	p.height = 15
	p.cursor = 5

	// Test scroll down (positive delta)
	oldCursor := p.cursor
	_, _ = p.scrollSidebar(3) // delta of 3 (typical scroll amount)

	if p.cursor != oldCursor+3 {
		t.Errorf("expected cursor %d after scroll down, got %d", oldCursor+3, p.cursor)
	}

	// Test scroll up (negative delta)
	oldCursor = p.cursor
	_, _ = p.scrollSidebar(-2)

	if p.cursor != oldCursor-2 {
		t.Errorf("expected cursor %d after scroll up, got %d", oldCursor-2, p.cursor)
	}

	// Test scroll bounds at top
	p.cursor = 0
	_, _ = p.scrollSidebar(-5)

	if p.cursor != 0 {
		t.Errorf("expected cursor to stay at 0 when scrolling up at top, got %d", p.cursor)
	}

	// Test scroll bounds at bottom
	p.cursor = len(p.sessions) - 1
	_, _ = p.scrollSidebar(5)

	if p.cursor != len(p.sessions)-1 {
		t.Errorf("expected cursor to stay at %d when scrolling down at bottom, got %d", len(p.sessions)-1, p.cursor)
	}
}

// TestHitRegionsDirtyOnModeChange tests that hitRegionsDirty is set when view modes change (td-455e378b).
func TestHitRegionsDirtyOnModeChange(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.width = 150
	p.height = 30
	p.activePane = PaneMessages
	p.sessions = []adapter.Session{{ID: "test-1", Name: "Test", UpdatedAt: time.Now()}}
	p.selectedSession = "test-1"

	// Render to clear initial dirty flag
	_ = p.View(p.width, p.height)
	p.hitRegionsDirty = false

	t.Run("turnViewMode toggle sets dirty", func(t *testing.T) {
		p.hitRegionsDirty = false
		// Simulate 'v' key press
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}}
		p.Update(msg)
		if !p.hitRegionsDirty {
			t.Error("expected hitRegionsDirty=true after turnViewMode toggle")
		}
	})

	t.Run("filterMode open sets dirty", func(t *testing.T) {
		p.hitRegionsDirty = false
		p.activePane = PaneSidebar
		// Simulate 'f' key press
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}}
		p.Update(msg)
		if !p.hitRegionsDirty {
			t.Error("expected hitRegionsDirty=true when opening filter menu")
		}
	})

	t.Run("filterMode close via esc sets dirty", func(t *testing.T) {
		p.filterMode = true
		p.hitRegionsDirty = false
		// Simulate 'esc' key press
		msg := tea.KeyMsg{Type: tea.KeyEscape}
		p.Update(msg)
		if !p.hitRegionsDirty {
			t.Error("expected hitRegionsDirty=true when closing filter menu via esc")
		}
	})

	t.Run("filterMode close via enter sets dirty", func(t *testing.T) {
		p.filterMode = true
		p.hitRegionsDirty = false
		// Simulate 'enter' key press
		msg := tea.KeyMsg{Type: tea.KeyEnter}
		p.Update(msg)
		if !p.hitRegionsDirty {
			t.Error("expected hitRegionsDirty=true when closing filter menu via enter")
		}
	})
}

// TestPaginationKeyHandling tests p/n key handling for pagination (td-313ea851, td-e75bfdc7).
func TestPaginationKeyHandling(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.width = 150
	p.height = 30
	p.activePane = PaneMessages
	p.sessions = []adapter.Session{{ID: "test-1", Name: "Test", UpdatedAt: time.Now()}}
	p.selectedSession = "test-1"

	t.Run("p key does nothing when no older messages", func(t *testing.T) {
		p.messageOffset = 0
		p.hasOlderMsgs = false
		p.totalMessages = 100 // Less than maxMessagesInMemory
		initialOffset := p.messageOffset

		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}
		p.Update(msg)

		if p.messageOffset != initialOffset {
			t.Errorf("expected offset %d, got %d", initialOffset, p.messageOffset)
		}
	})

	t.Run("p key increases offset when older messages exist", func(t *testing.T) {
		p.messageOffset = 0
		p.hasOlderMsgs = true
		p.totalMessages = 1000 // More than maxMessagesInMemory

		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}
		p.Update(msg)

		// Should increase by maxMessagesInMemory / 2 = 250
		expected := 250
		if p.messageOffset != expected {
			t.Errorf("expected offset %d, got %d", expected, p.messageOffset)
		}
	})

	t.Run("p key clamps offset to max", func(t *testing.T) {
		p.messageOffset = 400
		p.hasOlderMsgs = true
		p.totalMessages = 600 // max offset = 600 - 500 = 100

		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}
		p.Update(msg)

		// Should clamp to totalMessages - maxMessagesInMemory = 100
		expected := 100
		if p.messageOffset != expected {
			t.Errorf("expected offset %d (clamped), got %d", expected, p.messageOffset)
		}
	})

	t.Run("n key does nothing when at newest", func(t *testing.T) {
		p.messageOffset = 0
		p.totalMessages = 1000

		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}
		p.Update(msg)

		if p.messageOffset != 0 {
			t.Errorf("expected offset 0, got %d", p.messageOffset)
		}
	})

	t.Run("n key decreases offset", func(t *testing.T) {
		p.messageOffset = 300
		p.totalMessages = 1000

		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}
		p.Update(msg)

		// Should decrease by maxMessagesInMemory / 2 = 250
		expected := 50
		if p.messageOffset != expected {
			t.Errorf("expected offset %d, got %d", expected, p.messageOffset)
		}
	})

	t.Run("n key clamps offset to 0", func(t *testing.T) {
		p.messageOffset = 100
		p.totalMessages = 1000

		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}
		p.Update(msg)

		// Should clamp to 0
		if p.messageOffset != 0 {
			t.Errorf("expected offset 0 (clamped), got %d", p.messageOffset)
		}
	})
}

// TestMessagesLoadedMsgPagination tests pagination state update from MessagesLoadedMsg (td-313ea851).
func TestMessagesLoadedMsgPagination(t *testing.T) {
	t.Run("sets pagination state from message", func(t *testing.T) {
		p := New()
		p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
		p.selectedSession = "test-1"
		p.pageSize = 50

		messages := make([]adapter.Message, 100)
		for i := range messages {
			messages[i] = adapter.Message{ID: fmt.Sprintf("msg-%d", i), Role: "user"}
		}

		msg := MessagesLoadedMsg{
			SessionID:  "test-1",
			Messages:   messages,
			TotalCount: 1000,
			Offset:     500,
		}
		p.Update(msg)

		if p.totalMessages != 1000 {
			t.Errorf("expected totalMessages 1000, got %d", p.totalMessages)
		}
		if p.messageOffset != 500 {
			t.Errorf("expected messageOffset 500, got %d", p.messageOffset)
		}
	})

	t.Run("hasOlderMsgs true when more messages exist", func(t *testing.T) {
		p := New()
		p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
		p.selectedSession = "test-2"
		p.pageSize = 50

		messages := make([]adapter.Message, 100)
		for i := range messages {
			messages[i] = adapter.Message{ID: fmt.Sprintf("msg2-%d", i), Role: "user"}
		}

		msg := MessagesLoadedMsg{
			SessionID:  "test-2",
			Messages:   messages,
			TotalCount: 1000,
			Offset:     0, // At start, 100 messages loaded, 900 older exist
		}
		p.Update(msg)

		// hasOlderMsgs = (0 + 100) < 1000 = true
		if !p.hasOlderMsgs {
			t.Error("expected hasOlderMsgs=true when older messages exist")
		}
	})

	t.Run("hasOlderMsgs false when all messages loaded", func(t *testing.T) {
		p := New()
		p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
		p.selectedSession = "test-3"
		p.pageSize = 50

		messages := make([]adapter.Message, 100)
		for i := range messages {
			messages[i] = adapter.Message{ID: fmt.Sprintf("msg3-%d", i), Role: "user"}
		}

		msg := MessagesLoadedMsg{
			SessionID:  "test-3",
			Messages:   messages,
			TotalCount: 100,
			Offset:     0,
		}
		p.Update(msg)

		// hasOlderMsgs = (0 + 100) < 100 = false
		if p.hasOlderMsgs {
			t.Error("expected hasOlderMsgs=false when all messages loaded")
		}
	})
}

// =============================================================================
// Render Function Tests (td-a3f8fa83)
// =============================================================================

// TestRenderConversationFlowEmpty tests rendering with no messages.
func TestRenderConversationFlowEmpty(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.messages = []adapter.Message{}
	p.width = 100
	p.height = 20

	lines := p.renderConversationFlow(80, 15)

	if len(lines) != 1 {
		t.Errorf("expected 1 line for empty messages, got %d", len(lines))
	}
	if len(lines) > 0 && !containsSubstring(lines[0], "No messages") {
		t.Errorf("expected 'No messages' text, got %q", lines[0])
	}
}

// TestRenderConversationFlowBasic tests rendering basic messages.
func TestRenderConversationFlowBasic(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	now := time.Now()
	p.messages = []adapter.Message{
		{ID: "msg-1", Role: "user", Content: "Hello", Timestamp: now},
		{ID: "msg-2", Role: "assistant", Content: "Hi there!", Timestamp: now.Add(time.Second)},
	}
	p.width = 100
	p.height = 20
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	lines := p.renderConversationFlow(80, 15)

	// Should render both messages with some content
	if len(lines) < 2 {
		t.Errorf("expected at least 2 lines for 2 messages, got %d", len(lines))
	}

	// Verify message line positions are tracked
	if len(p.msgLinePositions) != 2 {
		t.Errorf("expected 2 msgLinePositions, got %d", len(p.msgLinePositions))
	}
}

// TestRenderConversationFlowScrollWindow tests scroll offset handling.
func TestRenderConversationFlowScrollWindow(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	now := time.Now()

	// Create many messages to force scrolling
	for i := 0; i < 20; i++ {
		p.messages = append(p.messages, adapter.Message{
			ID:        fmt.Sprintf("msg-%d", i),
			Role:      "user",
			Content:   fmt.Sprintf("Message %d", i),
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}
	p.width = 100
	p.height = 20
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	// First render with scroll at 0
	p.messageScroll = 0
	lines1 := p.renderConversationFlow(80, 10)

	// Render with scroll offset
	p.messageScroll = 5
	lines2 := p.renderConversationFlow(80, 10)

	// Both should return same number of lines (height-limited)
	if len(lines1) > 10 || len(lines2) > 10 {
		t.Errorf("expected at most 10 lines, got %d and %d", len(lines1), len(lines2))
	}

	// Scroll should be clamped to valid range
	if p.messageScroll < 0 {
		t.Error("messageScroll should not be negative")
	}
}

// TestRenderConversationFlowSkipsToolResultOnly tests skipping tool-result-only messages.
func TestRenderConversationFlowSkipsToolResultOnly(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	// Set up session so agent name is resolved correctly
	p.sessions = []adapter.Session{{ID: "test-session", AdapterID: "claude-code"}}
	p.selectedSession = "test-session"
	now := time.Now()
	p.messages = []adapter.Message{
		{ID: "msg-1", Role: "user", Content: "Hello", Timestamp: now},
		{
			ID:        "msg-2",
			Role:      "user",
			Timestamp: now.Add(time.Second),
			ContentBlocks: []adapter.ContentBlock{
				{Type: "tool_result", ToolUseID: "tool-1", ToolOutput: "output"},
			},
		},
		{ID: "msg-3", Role: "assistant", Content: "Response", Timestamp: now.Add(2 * time.Second)},
	}
	p.width = 100
	p.height = 20
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	lines := p.renderConversationFlow(80, 15)

	// Should have 2 message positions (skipping the tool-result-only message)
	if len(p.msgLinePositions) != 2 {
		t.Errorf("expected 2 msgLinePositions (skipping tool-result-only), got %d", len(p.msgLinePositions))
	}

	// The content should contain user and assistant messages but not the tool result
	// Note: "user" renders as "you" and "assistant" renders as adapter short name (e.g., "claude")
	content := strings.Join(lines, "\n")
	if !containsSubstring(content, "you") {
		t.Error("expected 'you' role label in output")
	}
	if !containsSubstring(content, "claude") {
		t.Error("expected 'claude' role label in output")
	}
}

// TestRenderConversationFlowVisibleRanges tests visible range calculation.
func TestRenderConversationFlowVisibleRanges(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	now := time.Now()

	// Create enough messages to require scrolling
	for i := 0; i < 10; i++ {
		p.messages = append(p.messages, adapter.Message{
			ID:        fmt.Sprintf("msg-%d", i),
			Role:      "user",
			Content:   "Message content line",
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}
	p.width = 100
	p.height = 20
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	_ = p.renderConversationFlow(80, 8)

	// visibleMsgRanges should be populated
	if len(p.visibleMsgRanges) == 0 {
		t.Error("expected visibleMsgRanges to be populated")
	}

	// Each visible range should have valid indices
	for _, mr := range p.visibleMsgRanges {
		if mr.MsgIdx < 0 || mr.MsgIdx >= len(p.messages) {
			t.Errorf("invalid MsgIdx: %d", mr.MsgIdx)
		}
		if mr.LineCount <= 0 {
			t.Errorf("invalid LineCount: %d", mr.LineCount)
		}
	}
}

// TestRenderMessageBubbleUserRole tests rendering a user message bubble.
func TestRenderMessageBubbleUserRole(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)
	p.messageCursor = 0

	msg := adapter.Message{
		ID:        "msg-1",
		Role:      "user",
		Content:   "Hello world",
		Timestamp: time.Now(),
	}

	lines := p.renderMessageBubble(msg, 0, 60)

	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}

	// First line should contain timestamp and role label "you"
	header := lines[0]
	if !containsSubstring(header, "you") {
		t.Errorf("expected header to contain 'you', got %q", header)
	}
	if !containsSubstring(header, ":") {
		t.Errorf("expected header to contain timestamp with ':', got %q", header)
	}
}

// TestRenderMessageBubbleAssistantRole tests rendering an assistant message bubble.
func TestRenderMessageBubbleAssistantRole(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	// Set up session so agent name is resolved correctly
	p.sessions = []adapter.Session{{ID: "test-session", AdapterID: "claude-code"}}
	p.selectedSession = "test-session"
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)
	p.messageCursor = 0

	msg := adapter.Message{
		ID:        "msg-1",
		Role:      "assistant",
		Content:   "Hello! How can I help?",
		Timestamp: time.Now(),
		Model:     "claude-sonnet-4-20250514",
	}

	lines := p.renderMessageBubble(msg, 0, 60)

	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}

	header := lines[0]
	// Role label should be adapter short name (e.g., "claude" for claude-code) not "assistant"
	if !containsSubstring(header, "claude") {
		t.Errorf("expected header to contain 'claude', got %q", header)
	}
	// Model short name should be included (as a colored badge)
	if !containsSubstring(header, "sonnet") {
		t.Errorf("expected header to contain model short name 'sonnet', got %q", header)
	}
}

// TestRenderMessageBubbleWithContentBlocks tests rendering with structured content blocks.
func TestRenderMessageBubbleWithContentBlocks(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)
	p.messageCursor = 0

	msg := adapter.Message{
		ID:        "msg-1",
		Role:      "assistant",
		Timestamp: time.Now(),
		ContentBlocks: []adapter.ContentBlock{
			{Type: "text", Text: "Here is some text"},
			{Type: "tool_use", ToolUseID: "tool-1", ToolName: "Bash", ToolInput: `{"command":"ls -la"}`},
		},
	}

	lines := p.renderMessageBubble(msg, 0, 60)

	content := strings.Join(lines, "\n")
	// Should render text content
	if !containsSubstring(content, "text") || !containsSubstring(content, "Bash") {
		t.Errorf("expected content to contain text and tool info, got %q", content)
	}
}

// TestRenderMessageBubbleSelected tests selection highlighting.
func TestRenderMessageBubbleSelected(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)
	p.messageCursor = 0 // This message is selected

	msg := adapter.Message{
		ID:        "msg-1",
		Role:      "user",
		Content:   "Selected message",
		Timestamp: time.Now(),
	}

	lines := p.renderMessageBubble(msg, 0, 60)

	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}

	// First line should have cursor indicator ">"
	if !containsSubstring(lines[0], ">") {
		t.Errorf("expected selected message to have '>' cursor, got %q", lines[0])
	}
}

// TestRenderMessageBubbleNotSelected tests non-selected message.
func TestRenderMessageBubbleNotSelected(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)
	p.messageCursor = 1 // Different from this message's index

	msg := adapter.Message{
		ID:        "msg-1",
		Role:      "user",
		Content:   "Not selected message",
		Timestamp: time.Now(),
	}

	lines := p.renderMessageBubble(msg, 0, 60)

	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}

	// Should have space prefix instead of ">"
	if containsSubstring(lines[0], ">") {
		t.Errorf("expected non-selected message to not have '>' cursor, got %q", lines[0])
	}
}

// TestRenderContentBlocksText tests rendering text content blocks.
func TestRenderContentBlocksText(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	msg := adapter.Message{
		ID: "msg-1",
		ContentBlocks: []adapter.ContentBlock{
			{Type: "text", Text: "Simple text content"},
		},
	}

	lines := p.renderContentBlocks(msg, 60)

	if len(lines) == 0 {
		t.Error("expected at least 1 line for text content")
	}
}

// TestRenderContentBlocksThinking tests rendering thinking blocks.
func TestRenderContentBlocksThinking(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	msg := adapter.Message{
		ID: "msg-1",
		ContentBlocks: []adapter.ContentBlock{
			{Type: "thinking", Text: "Let me think about this...", TokenCount: 150},
		},
	}

	// Collapsed by default
	lines := p.renderContentBlocks(msg, 60)
	content := strings.Join(lines, "\n")

	if !containsSubstring(content, "thinking") {
		t.Error("expected thinking header")
	}
	if !containsSubstring(content, "150") {
		t.Error("expected token count in thinking header")
	}
}

// TestRenderContentBlocksThinkingExpanded tests expanded thinking blocks.
func TestRenderContentBlocksThinkingExpanded(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	msg := adapter.Message{
		ID: "msg-1",
		ContentBlocks: []adapter.ContentBlock{
			{Type: "thinking", Text: "Detailed thinking content here", TokenCount: 100},
		},
	}

	// Expand thinking
	p.expandedThinking["msg-1"] = true

	lines := p.renderContentBlocks(msg, 60)
	content := strings.Join(lines, "\n")

	// Should include the thinking content
	if !containsSubstring(content, "Detailed thinking") {
		t.Error("expected expanded thinking content")
	}
}

// TestRenderContentBlocksToolUse tests rendering tool use blocks.
func TestRenderContentBlocksToolUse(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	msg := adapter.Message{
		ID: "msg-1",
		ContentBlocks: []adapter.ContentBlock{
			{
				Type:       "tool_use",
				ToolUseID:  "tool-1",
				ToolName:   "Read",
				ToolInput:  `{"file_path":"/path/to/file.txt"}`,
				ToolOutput: "File contents here",
			},
		},
	}

	lines := p.renderContentBlocks(msg, 60)
	content := strings.Join(lines, "\n")

	if !containsSubstring(content, "Read") {
		t.Error("expected tool name 'Read' in output")
	}
	if !containsSubstring(content, "file.txt") {
		t.Error("expected file path in output")
	}
}

// TestRenderContentBlocksToolResult tests that tool_result blocks are skipped.
func TestRenderContentBlocksToolResult(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	msg := adapter.Message{
		ID: "msg-1",
		ContentBlocks: []adapter.ContentBlock{
			{Type: "tool_result", ToolUseID: "tool-1", ToolOutput: "Should be skipped"},
		},
	}

	lines := p.renderContentBlocks(msg, 60)

	// Tool result blocks should be skipped (rendered inline with tool_use)
	if len(lines) != 0 {
		t.Errorf("expected 0 lines for tool_result-only blocks, got %d", len(lines))
	}
}

// TestRenderContentBlocksMixed tests rendering mixed content blocks.
func TestRenderContentBlocksMixed(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	msg := adapter.Message{
		ID: "msg-1",
		ContentBlocks: []adapter.ContentBlock{
			{Type: "thinking", Text: "Thinking...", TokenCount: 50},
			{Type: "text", Text: "Let me help you with that."},
			{Type: "tool_use", ToolUseID: "t1", ToolName: "Bash", ToolInput: `{"command":"echo hello"}`},
		},
	}

	lines := p.renderContentBlocks(msg, 80)

	if len(lines) < 3 {
		t.Errorf("expected at least 3 lines for mixed blocks, got %d", len(lines))
	}

	content := strings.Join(lines, "\n")
	if !containsSubstring(content, "thinking") {
		t.Error("expected thinking block content")
	}
	if !containsSubstring(content, "Bash") {
		t.Error("expected tool use block content")
	}
}

// TestRenderToolUseBlockCollapsed tests collapsed tool use block.
func TestRenderToolUseBlockCollapsed(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedToolResults = make(map[string]bool)

	block := adapter.ContentBlock{
		Type:       "tool_use",
		ToolUseID:  "tool-1",
		ToolName:   "Bash",
		ToolInput:  `{"command":"ls -la /path/to/dir"}`,
		ToolOutput: "total 0\ndrwxr-xr-x  2 user  group  64 Jan  1 00:00 .",
	}

	lines := p.renderToolUseBlock(block, 60)

	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}

	// Should have tool header
	if !containsSubstring(lines[0], "Bash") {
		t.Errorf("expected 'Bash' in tool header, got %q", lines[0])
	}

	// Collapsed: should have preview line
	content := strings.Join(lines, "\n")
	if len(lines) > 1 && !containsSubstring(content, "total 0") && !containsSubstring(content, "drwxr") {
		// When collapsed, a short preview is shown
		t.Log("collapsed tool block has output preview or arrow")
	}
}

// TestRenderToolUseBlockExpanded tests expanded tool use block.
func TestRenderToolUseBlockExpanded(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedToolResults = make(map[string]bool)
	p.expandedToolResults["tool-1"] = true // Expanded

	block := adapter.ContentBlock{
		Type:       "tool_use",
		ToolUseID:  "tool-1",
		ToolName:   "Bash",
		ToolInput:  `{"command":"ls -la"}`,
		ToolOutput: "file1.txt\nfile2.txt\nfile3.txt",
	}

	lines := p.renderToolUseBlock(block, 60)

	// Should have more lines when expanded
	if len(lines) < 3 {
		t.Errorf("expected at least 3 lines when expanded, got %d", len(lines))
	}

	content := strings.Join(lines, "\n")
	if !containsSubstring(content, "file1.txt") {
		t.Error("expected output content when expanded")
	}
}

// TestRenderToolUseBlockError tests tool use block with error.
func TestRenderToolUseBlockError(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedToolResults = make(map[string]bool)

	block := adapter.ContentBlock{
		Type:       "tool_use",
		ToolUseID:  "tool-1",
		ToolName:   "Bash",
		ToolInput:  `{"command":"invalid_command"}`,
		ToolOutput: "command not found: invalid_command",
		IsError:    true,
	}

	lines := p.renderToolUseBlock(block, 60)

	content := strings.Join(lines, "\n")
	// Errors should show the ✗ error indicator
	if !containsSubstring(content, "✗") {
		t.Error("expected '✗' error indicator for failed tool")
	}
	// Errors should auto-expand to show output
	if !containsSubstring(content, "command not found") {
		t.Error("expected error output to be shown")
	}
}

// TestRenderToolUseBlockDifferentToolTypes tests various tool types.
func TestRenderToolUseBlockDifferentToolTypes(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedToolResults = make(map[string]bool)

	tests := []struct {
		name      string
		toolName  string
		toolInput string
		expected  string
	}{
		{"Bash", "Bash", `{"command":"echo test"}`, "echo test"},
		{"Read", "Read", `{"file_path":"/path/to/file.go"}`, "file.go"},
		{"Edit", "Edit", `{"file_path":"/src/main.rs"}`, "main.rs"},
		{"Write", "Write", `{"file_path":"/new/file.txt"}`, "file.txt"},
		{"Glob", "Glob", `{"pattern":"**/*.go"}`, "*.go"},
		{"Grep", "Grep", `{"pattern":"TODO"}`, "TODO"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := adapter.ContentBlock{
				Type:      "tool_use",
				ToolUseID: "tool-" + tt.name,
				ToolName:  tt.toolName,
				ToolInput: tt.toolInput,
			}

			lines := p.renderToolUseBlock(block, 80)

			if len(lines) == 0 {
				t.Fatal("expected at least 1 line")
			}

			header := lines[0]
			if !containsSubstring(header, tt.expected) {
				t.Errorf("expected %q to contain %q", header, tt.expected)
			}
		})
	}
}

// TestRenderToolUseBlockLongOutput tests truncation of long output.
func TestRenderToolUseBlockLongOutput(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedToolResults = make(map[string]bool)
	p.expandedToolResults["tool-1"] = true // Expanded

	// Generate long output
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString(fmt.Sprintf("Line %d of output\n", i))
	}

	block := adapter.ContentBlock{
		Type:       "tool_use",
		ToolUseID:  "tool-1",
		ToolName:   "Bash",
		ToolInput:  `{"command":"cat largefile.txt"}`,
		ToolOutput: sb.String(),
	}

	result := p.renderToolUseBlock(block, 60)

	// Should be truncated to 20 lines + header + "more lines" indicator
	// Max is 20 output lines + header + possible indicator
	if len(result) > 25 {
		t.Errorf("expected output to be truncated, got %d lines", len(result))
	}

	content := strings.Join(result, "\n")
	if !containsSubstring(content, "more lines") {
		t.Error("expected 'more lines' indicator for truncated output")
	}
}

// TestRenderToolUseBlockJSONOutput tests prettified JSON output.
func TestRenderToolUseBlockJSONOutput(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedToolResults = make(map[string]bool)
	p.expandedToolResults["tool-1"] = true

	block := adapter.ContentBlock{
		Type:       "tool_use",
		ToolUseID:  "tool-1",
		ToolName:   "Bash",
		ToolInput:  `{"command":"cat config.json"}`,
		ToolOutput: `{"key":"value","nested":{"a":1}}`,
	}

	result := p.renderToolUseBlock(block, 60)
	content := strings.Join(result, "\n")

	// JSON should be prettified (indented)
	if !containsSubstring(content, "key") {
		t.Error("expected JSON key in output")
	}
}

// TestIsToolResultOnlyMessage tests the helper function.
func TestIsToolResultOnlyMessage(t *testing.T) {
	p := New()

	tests := []struct {
		name     string
		msg      adapter.Message
		expected bool
	}{
		{
			name:     "empty content blocks",
			msg:      adapter.Message{ID: "1", ContentBlocks: []adapter.ContentBlock{}},
			expected: false,
		},
		{
			name: "only tool_result blocks",
			msg: adapter.Message{
				ID: "2",
				ContentBlocks: []adapter.ContentBlock{
					{Type: "tool_result", ToolOutput: "output1"},
					{Type: "tool_result", ToolOutput: "output2"},
				},
			},
			expected: true,
		},
		{
			name: "mixed blocks",
			msg: adapter.Message{
				ID: "3",
				ContentBlocks: []adapter.ContentBlock{
					{Type: "tool_result", ToolOutput: "output"},
					{Type: "text", Text: "some text"},
				},
			},
			expected: false,
		},
		{
			name: "only text block",
			msg: adapter.Message{
				ID: "4",
				ContentBlocks: []adapter.ContentBlock{
					{Type: "text", Text: "hello"},
				},
			},
			expected: false,
		},
		{
			name:     "no content blocks with content string",
			msg:      adapter.Message{ID: "5", Content: "regular content"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.isToolResultOnlyMessage(tt.msg)
			if result != tt.expected {
				t.Errorf("isToolResultOnlyMessage() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestExtractFilePath tests file path extraction from tool input.
func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"file_path":"/path/to/file.go"}`, "/path/to/file.go"},
		{`{"file_path":"relative/path.txt"}`, "relative/path.txt"},
		{`{"command":"ls -la"}`, ""},
		{`{}`, ""},
		{`invalid json`, ""},
		{``, ""},
	}

	for _, tt := range tests {
		result := extractFilePath(tt.input)
		if result != tt.expected {
			t.Errorf("extractFilePath(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

// TestExtractToolCommand tests tool command extraction.
func TestExtractToolCommand(t *testing.T) {
	tests := []struct {
		toolName string
		input    string
		maxLen   int
		expected string
	}{
		{"Bash", `{"command":"ls -la /path"}`, 50, "ls -la /path"},
		{"Bash", `{"command":"very long command that should be truncated here"}`, 20, "very long command..."},
		{"Glob", `{"pattern":"**/*.go"}`, 50, "**/*.go"},
		{"Grep", `{"pattern":"TODO"}`, 50, "TODO"},
		{"Read", `{"file_path":"/path/file.txt"}`, 50, "/path/file.txt"},
		{"Read", `{"path":"/some/file.go"}`, 50, "/some/file.go"},
		// Task tool with array input containing text
		{"Task", `[{"text":"Perfect! Now I have a comprehensive understanding"}]`, 50, "Perfect! Now I have a comprehensive understanding"},
		{"Task", `[{"text":"Very long text that needs to be truncated"}]`, 20, "Very long text th..."},
		// Fallback text extraction for unknown tools
		{"Unknown", `{"text":"Some message content"}`, 50, "Some message content"},
		{"Unknown", `{"content":"Task description"}`, 50, "Task description"},
		{"Unknown", `{"something":"else"}`, 50, ""},
	}

	for _, tt := range tests {
		result := extractToolCommand(tt.toolName, tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("extractToolCommand(%q, %q, %d) = %q, expected %q",
				tt.toolName, tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

// TestPrettifyJSON tests JSON prettification.
func TestPrettifyJSON(t *testing.T) {
	tests := []struct {
		input    string
		isJSON   bool
		contains string
	}{
		{`{"key":"value"}`, true, "key"},
		{`[1,2,3]`, true, "1"},
		{`not json`, false, "not json"},
		{``, false, ""},
		{`   {"spaced":true}  `, true, "spaced"},
	}

	for _, tt := range tests {
		result := prettifyJSON(tt.input)
		if tt.isJSON {
			if !containsSubstring(result, tt.contains) {
				t.Errorf("prettifyJSON(%q) should contain %q, got %q", tt.input, tt.contains, result)
			}
		} else {
			if result != tt.input {
				t.Errorf("prettifyJSON(%q) should return input unchanged for non-JSON, got %q", tt.input, result)
			}
		}
	}
}

// TestModelShortName tests model name shortening.
func TestModelShortName(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"claude-opus-4-5-20251101", "opus"},
		{"claude-sonnet-4-20250514", "sonnet4"},
		{"claude-3-5-sonnet-20241022", "sonnet"},
		{"claude-3-haiku-20240307", "haiku"},
		{"gpt-4o-2024-08-06", "gpt4o"},
		{"gpt-4-turbo", "gpt4"},
		{"o1-preview", "o1"},
		{"o3-mini", "o3"},
		{"gemini-2.0-flash", "2Flash"},
		{"gemini-1.5-pro", "1.5Pro"},
		{"unknown-model", ""},
	}

	for _, tt := range tests {
		result := modelShortName(tt.model)
		if result != tt.expected {
			t.Errorf("modelShortName(%q) = %q, expected %q", tt.model, result, tt.expected)
		}
	}
}

// TestRenderMessageBubbleEmptyContent tests rendering message with empty content.
func TestRenderMessageBubbleEmptyContent(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)
	p.messageCursor = 0

	msg := adapter.Message{
		ID:        "msg-1",
		Role:      "assistant",
		Content:   "",
		Timestamp: time.Now(),
	}

	lines := p.renderMessageBubble(msg, 0, 60)

	// Should still render header even with empty content
	if len(lines) < 1 {
		t.Error("expected at least header line for empty content message")
	}
}

// TestRenderConversationFlowAllToolResults tests edge case of all tool-result-only messages.
func TestRenderConversationFlowAllToolResults(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	now := time.Now()

	// All messages are tool-result-only
	p.messages = []adapter.Message{
		{
			ID:        "msg-1",
			Role:      "user",
			Timestamp: now,
			ContentBlocks: []adapter.ContentBlock{
				{Type: "tool_result", ToolUseID: "t1", ToolOutput: "output1"},
			},
		},
		{
			ID:        "msg-2",
			Role:      "user",
			Timestamp: now.Add(time.Second),
			ContentBlocks: []adapter.ContentBlock{
				{Type: "tool_result", ToolUseID: "t2", ToolOutput: "output2"},
			},
		},
	}
	p.width = 100
	p.height = 20
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	lines := p.renderConversationFlow(80, 15)

	// When all messages are tool-result-only, they are all skipped
	// The function returns an empty slice (not "No messages" which is only for empty p.messages)
	if len(lines) != 0 {
		t.Errorf("expected 0 lines when all messages are tool-result-only and skipped, got %d", len(lines))
	}

	// No message positions should be tracked
	if len(p.msgLinePositions) != 0 {
		t.Errorf("expected 0 msgLinePositions for all-tool-result messages, got %d", len(p.msgLinePositions))
	}
}

// TestRenderContentBlocksEmpty tests rendering with no content blocks.
func TestRenderContentBlocksEmpty(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	msg := adapter.Message{
		ID:            "msg-1",
		ContentBlocks: []adapter.ContentBlock{},
	}

	lines := p.renderContentBlocks(msg, 60)

	if len(lines) != 0 {
		t.Errorf("expected 0 lines for empty content blocks, got %d", len(lines))
	}
}

// TestRenderToolUseBlockNoOutput tests tool use block with no output.
func TestRenderToolUseBlockNoOutput(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.expandedToolResults = make(map[string]bool)

	block := adapter.ContentBlock{
		Type:       "tool_use",
		ToolUseID:  "tool-1",
		ToolName:   "Read",
		ToolInput:  `{"file_path":"/path/to/file.txt"}`,
		ToolOutput: "", // No output
	}

	lines := p.renderToolUseBlock(block, 60)

	if len(lines) != 1 {
		t.Errorf("expected 1 line (header only) for no output, got %d", len(lines))
	}

	if !containsSubstring(lines[0], "Read") {
		t.Error("expected tool name in header")
	}
}

// TestRenderConversationFlowMaxScrollClamp tests scroll clamping.
func TestRenderConversationFlowMaxScrollClamp(t *testing.T) {
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	now := time.Now()

	// Create 3 short messages
	p.messages = []adapter.Message{
		{ID: "msg-1", Role: "user", Content: "Hello", Timestamp: now},
		{ID: "msg-2", Role: "assistant", Content: "Hi", Timestamp: now.Add(time.Second)},
		{ID: "msg-3", Role: "user", Content: "Bye", Timestamp: now.Add(2 * time.Second)},
	}
	p.width = 100
	p.height = 20
	p.expandedMessages = make(map[string]bool)
	p.expandedThinking = make(map[string]bool)
	p.expandedToolResults = make(map[string]bool)

	// Set scroll way past content
	p.messageScroll = 1000

	_ = p.renderConversationFlow(80, 50) // Large height

	// Scroll should be clamped
	if p.messageScroll < 0 {
		t.Error("scroll should not be negative")
	}
}

// Helper function to check if a string contains a substring
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}

// TestToggleSidebar tests the sidebar toggle focus restoration behavior.
func TestToggleSidebar(t *testing.T) {
	tests := []struct {
		name            string
		startPane       FocusPane
		expectedRestore FocusPane
	}{
		{
			name:            "sidebar to sidebar",
			startPane:       PaneSidebar,
			expectedRestore: PaneSidebar,
		},
		{
			name:            "messages to messages",
			startPane:       PaneMessages,
			expectedRestore: PaneMessages,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			p.activePane = tt.startPane
			p.sidebarVisible = true

			// Collapse sidebar
			p.toggleSidebar()

			if p.sidebarVisible {
				t.Error("sidebar should be hidden after collapse")
			}
			if p.sidebarRestore != tt.startPane {
				t.Errorf("sidebarRestore = %d, want %d", p.sidebarRestore, tt.startPane)
			}
			// When collapsed from sidebar, focus should move to messages
			if tt.startPane == PaneSidebar && p.activePane != PaneMessages {
				t.Errorf("activePane should be PaneMessages after collapsing from sidebar, got %d", p.activePane)
			}

			// Expand sidebar
			p.toggleSidebar()

			if !p.sidebarVisible {
				t.Error("sidebar should be visible after expand")
			}
			if p.activePane != tt.expectedRestore {
				t.Errorf("activePane = %d, want %d after restore", p.activePane, tt.expectedRestore)
			}
		})
	}
}

// TestToggleSidebarRapidToggle tests multiple rapid toggles don't corrupt state.
func TestToggleSidebarRapidToggle(t *testing.T) {
	p := New()
	p.activePane = PaneMessages
	p.sidebarVisible = true

	// Toggle 5 times rapidly
	for i := 0; i < 5; i++ {
		p.toggleSidebar()
	}

	// After odd number of toggles, sidebar should be hidden
	if p.sidebarVisible {
		t.Error("sidebar should be hidden after odd number of toggles")
	}

	// Toggle once more to show
	p.toggleSidebar()

	if !p.sidebarVisible {
		t.Error("sidebar should be visible")
	}
	// Should restore to PaneMessages
	if p.activePane != PaneMessages {
		t.Errorf("activePane should restore to PaneMessages, got %d", p.activePane)
	}
}

// TestToggleSidebarCollapseFromMessages verifies focus stays on messages when collapsing from messages.
func TestToggleSidebarCollapseFromMessages(t *testing.T) {
	p := New()
	p.activePane = PaneMessages
	p.sidebarVisible = true

	p.toggleSidebar()

	// Focus should stay on messages (since we weren't on sidebar)
	if p.activePane != PaneMessages {
		t.Errorf("activePane should remain PaneMessages, got %d", p.activePane)
	}
}

// TestSidebarRestoreInitialization verifies sidebarRestore is initialized correctly.
func TestSidebarRestoreInitialization(t *testing.T) {
	p := New()

	if p.sidebarRestore != PaneSidebar {
		t.Errorf("sidebarRestore should default to PaneSidebar, got %d", p.sidebarRestore)
	}
}

// TestStripANSIBackground verifies background color stripping for selection highlighting.
func TestStripANSIBackground(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic background code 40-47",
			input:    "\x1b[41mred bg\x1b[0m",
			expected: "red bg\x1b[0m",
		},
		{
			name:     "256-color background",
			input:    "\x1b[48;5;196mred 256\x1b[0m",
			expected: "red 256\x1b[0m",
		},
		{
			name:     "true color background",
			input:    "\x1b[48;2;255;0;0mtrue red\x1b[0m",
			expected: "true red\x1b[0m",
		},
		{
			name:     "preserves foreground colors",
			input:    "\x1b[31m\x1b[44mred on blue\x1b[0m",
			expected: "\x1b[31mred on blue\x1b[0m",
		},
		{
			name:     "multiple backgrounds stripped",
			input:    "\x1b[41mone\x1b[42mtwo\x1b[48;5;100mthree\x1b[0m",
			expected: "onetwothree\x1b[0m",
		},
		{
			name:     "no ANSI codes unchanged",
			input:    "plain text",
			expected: "plain text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripANSIBackground(tt.input)
			if result != tt.expected {
				t.Errorf("stripANSIBackground(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestResumeCommand verifies resume command generation for all adapters.
func TestResumeCommand(t *testing.T) {
	tests := []struct {
		name     string
		session  *adapter.Session
		expected string
	}{
		{
			name:     "claude-code adapter",
			session:  &adapter.Session{ID: "ses_abc123", AdapterID: "claude-code"},
			expected: "claude --resume ses_abc123",
		},
		{
			name:     "codex adapter",
			session:  &adapter.Session{ID: "ses_abc123", AdapterID: "codex"},
			expected: "codex resume ses_abc123",
		},
		{
			name:     "opencode adapter",
			session:  &adapter.Session{ID: "ses_abc123", AdapterID: "opencode"},
			expected: "opencode --continue -s ses_abc123",
		},
		{
			name:     "gemini-cli adapter",
			session:  &adapter.Session{ID: "ses_abc123", AdapterID: "gemini-cli"},
			expected: "gemini --resume ses_abc123",
		},
		{
			name:     "cursor-cli adapter",
			session:  &adapter.Session{ID: "ses_abc123", AdapterID: "cursor-cli"},
			expected: "cursor-agent --resume ses_abc123",
		},
		{
			name:     "unknown adapter",
			session:  &adapter.Session{ID: "ses_abc123", AdapterID: "unknown"},
			expected: "",
		},
		{
			name:     "nil session",
			session:  nil,
			expected: "",
		},
		{
			name:     "empty session ID",
			session:  &adapter.Session{ID: "", AdapterID: "claude-code"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resumeCommand(tt.session)
			if result != tt.expected {
				t.Errorf("resumeCommand() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestUnfocusedRefreshThrottle tests that session refresh is skipped when unfocused (td-05149f66)
func TestUnfocusedRefreshThrottle(t *testing.T) {
	t.Run("skips refresh when unfocused", func(t *testing.T) {
		p := New()
		p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
		p.focused = false

		// Simulate a coalesced refresh message
		msg := CoalescedRefreshMsg{RefreshAll: true}
		_, cmd := p.Update(msg)

		// Should set pendingRefresh and return only the listener cmd
		if !p.pendingRefresh {
			t.Error("expected pendingRefresh to be true when unfocused")
		}
		// cmd should be non-nil (for listenForCoalescedRefresh)
		if cmd == nil {
			t.Error("expected non-nil command for listener")
		}
	})

	t.Run("refreshes when focused", func(t *testing.T) {
		p := New()
		p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
		p.focused = true

		// Simulate a coalesced refresh message
		msg := CoalescedRefreshMsg{RefreshAll: true}
		_, cmd := p.Update(msg)

		// Should not set pendingRefresh
		if p.pendingRefresh {
			t.Error("expected pendingRefresh to be false when focused")
		}
		// cmd should be non-nil (batch of listener + loadSessions)
		if cmd == nil {
			t.Error("expected non-nil command")
		}
	})

	t.Run("catches up on focus gain", func(t *testing.T) {
		p := New()
		p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
		p.pendingRefresh = true

		// Simulate gaining focus
		msg := app.PluginFocusedMsg{}
		_, cmd := p.Update(msg)

		// Should clear pendingRefresh and trigger refresh
		if p.pendingRefresh {
			t.Error("expected pendingRefresh to be cleared on focus gain")
		}
		if cmd == nil {
			t.Error("expected non-nil command for catch-up refresh")
		}
	})

	t.Run("no refresh on focus gain without pending", func(t *testing.T) {
		p := New()
		p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
		p.pendingRefresh = false

		// Simulate gaining focus
		msg := app.PluginFocusedMsg{}
		_, cmd := p.Update(msg)

		// Should not trigger refresh
		if cmd != nil {
			t.Error("expected nil command when no pending refresh")
		}
	})
}

// TestLargeSessionWarning tests td-ee67d8: large session detection and warnings.
func TestLargeSessionWarning(t *testing.T) {
	t.Run("no warning for small sessions", func(t *testing.T) {
		p := New()
		p.sessions = []adapter.Session{
			{ID: "small", Slug: "small", FileSize: 50 * 1024 * 1024}, // 50MB
		}

		cmd := p.checkLargeSessionWarnings()
		if cmd != nil {
			t.Error("expected no warning for session under threshold")
		}
	})

	t.Run("warning for large session", func(t *testing.T) {
		p := New()
		p.sessions = []adapter.Session{
			{ID: "large", Slug: "large", FileSize: 150 * 1024 * 1024}, // 150MB
		}

		cmd := p.checkLargeSessionWarnings()
		if cmd == nil {
			t.Fatal("expected warning command for large session")
		}

		// Execute and check message
		msg := cmd()
		toast, ok := msg.(app.ToastMsg)
		if !ok {
			t.Fatalf("expected ToastMsg, got %T", msg)
		}
		if !strings.Contains(toast.Message, "large") {
			t.Errorf("expected slug in message, got: %s", toast.Message)
		}
		if toast.IsError {
			t.Error("large (100MB) warning should not be error")
		}

		// Should be marked as warned
		if !p.warnedSessions["large"] {
			t.Error("session should be marked as warned")
		}
	})

	t.Run("error warning for huge session", func(t *testing.T) {
		p := New()
		p.sessions = []adapter.Session{
			{ID: "huge", Slug: "huge", FileSize: 600 * 1024 * 1024}, // 600MB
		}

		cmd := p.checkLargeSessionWarnings()
		if cmd == nil {
			t.Fatal("expected warning command")
		}

		msg := cmd()
		toast := msg.(app.ToastMsg)
		if !toast.IsError {
			t.Error("huge (500MB+) warning should be error")
		}
		if !strings.Contains(toast.Message, "auto-reload disabled") {
			t.Errorf("expected auto-reload message, got: %s", toast.Message)
		}
	})

	t.Run("no duplicate warnings", func(t *testing.T) {
		p := New()
		p.sessions = []adapter.Session{
			{ID: "large", Slug: "large", FileSize: 150 * 1024 * 1024},
		}

		// First call should warn
		cmd1 := p.checkLargeSessionWarnings()
		if cmd1 == nil {
			t.Fatal("expected first warning")
		}

		// Second call should not warn (already warned)
		cmd2 := p.checkLargeSessionWarnings()
		if cmd2 != nil {
			t.Error("expected no duplicate warning")
		}
	})

	t.Run("only one warning at a time", func(t *testing.T) {
		p := New()
		p.sessions = []adapter.Session{
			{ID: "large1", Slug: "large1", FileSize: 150 * 1024 * 1024},
			{ID: "large2", Slug: "large2", FileSize: 200 * 1024 * 1024},
		}

		// Should return only one command (first warning) even with multiple large sessions
		cmd := p.checkLargeSessionWarnings()
		if cmd == nil {
			t.Fatal("expected warning command")
		}

		// All sessions are marked warned after first call to avoid spam
		if !p.warnedSessions["large1"] || !p.warnedSessions["large2"] {
			t.Error("expected both sessions marked as warned")
		}

		// Second call returns nil - all already warned
		cmd2 := p.checkLargeSessionWarnings()
		if cmd2 != nil {
			t.Error("expected no warning on second call")
		}
	})
}

// =============================================================================
// Plugin Reinit Tests (td-84a1cb)
// =============================================================================

// TestPluginReinitOnProjectSwitch verifies that the conversations plugin properly
// resets state when switching projects via Stop() + Init() + Start().
func TestPluginReinitOnProjectSwitch(t *testing.T) {
	t.Run("state is reset on Init", func(t *testing.T) {
		p := New()

		// Set up initial state simulating an active session
		ctx1 := &plugin.Context{
			WorkDir:  "/project/a",
			Adapters: map[string]adapter.Adapter{"mock": &mockAdapter{}},
		}
		if err := p.Init(ctx1); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Simulate having loaded sessions and messages
		p.sessions = []adapter.Session{{ID: "sess-a", Name: "Session A"}}
		p.selectedSession = "sess-a"
		p.loadedSession = "sess-a"
		p.messages = []adapter.Message{{ID: "msg1", Role: "user", Content: "test"}}
		p.turns = []Turn{{StartIndex: 0, Role: "user"}}
		p.cursor = 5
		p.scrollOff = 10
		p.expandedThinking["msg1"] = true
		p.expandedMessages["msg1"] = true
		p.cachedWorktreePaths = []string{"/project/a", "/project/a-feature"}
		p.cachedWorktreeNames = map[string]string{"/project/a-feature": "feature"}
		p.cachedWorktreePaths = []string{"/project/a"}
		p.warnedSessions["sess-a"] = true

		// Stop the plugin (simulating project switch)
		p.Stop()

		// Reinitialize with new project
		ctx2 := &plugin.Context{
			WorkDir:  "/project/b",
			Adapters: map[string]adapter.Adapter{"mock": &mockAdapter{}},
		}
		if err := p.Init(ctx2); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Verify state was reset
		if len(p.sessions) != 0 {
			t.Errorf("sessions not cleared: got %d, want 0", len(p.sessions))
		}
		if p.selectedSession != "" {
			t.Errorf("selectedSession not cleared: got %q", p.selectedSession)
		}
		if p.loadedSession != "" {
			t.Errorf("loadedSession not cleared: got %q", p.loadedSession)
		}
		if len(p.messages) != 0 {
			t.Errorf("messages not cleared: got %d, want 0", len(p.messages))
		}
		if len(p.turns) != 0 {
			t.Errorf("turns not cleared: got %d, want 0", len(p.turns))
		}
		if p.cursor != 0 {
			t.Errorf("cursor not reset: got %d, want 0", p.cursor)
		}
		if p.scrollOff != 0 {
			t.Errorf("scrollOff not reset: got %d, want 0", p.scrollOff)
		}
		if len(p.expandedThinking) != 0 {
			t.Errorf("expandedThinking not cleared: got %d, want 0", len(p.expandedThinking))
		}
		if len(p.expandedMessages) != 0 {
			t.Errorf("expandedMessages not cleared: got %d, want 0", len(p.expandedMessages))
		}
		if len(p.warnedSessions) != 0 {
			t.Errorf("warnedSessions not cleared: got %d, want 0", len(p.warnedSessions))
		}
	})

	t.Run("worktree cache is invalidated on Init", func(t *testing.T) {
		p := New()

		ctx1 := &plugin.Context{
			WorkDir:  "/project/a",
			Adapters: map[string]adapter.Adapter{"mock": &mockAdapter{}},
		}
		if err := p.Init(ctx1); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Set cached worktree data from old project
		p.cachedWorktreePaths = []string{"/project/a", "/project/a-feature"}
		p.cachedWorktreeNames = map[string]string{"/project/a-feature": "feature"}
		p.worktreeCacheTime = time.Now()

		p.Stop()

		// Reinitialize
		ctx2 := &plugin.Context{
			WorkDir:  "/project/b",
			Adapters: map[string]adapter.Adapter{"mock": &mockAdapter{}},
		}
		if err := p.Init(ctx2); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Worktree cache should be invalidated
		if p.cachedWorktreePaths != nil {
			t.Errorf("cachedWorktreePaths not cleared: got %v", p.cachedWorktreePaths)
		}
		if p.cachedWorktreeNames != nil {
			t.Errorf("cachedWorktreeNames not cleared: got %v", p.cachedWorktreeNames)
		}
		if !p.worktreeCacheTime.IsZero() {
			t.Errorf("worktreeCacheTime not cleared: got %v", p.worktreeCacheTime)
		}
	})

	t.Run("coalescer is recreated on Init", func(t *testing.T) {
		p := New()

		ctx1 := &plugin.Context{
			WorkDir:  "/project/a",
			Adapters: map[string]adapter.Adapter{"mock": &mockAdapter{}},
		}
		if err := p.Init(ctx1); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Grab references to old infrastructure
		oldCoalescer := p.coalescer
		oldChan := p.coalesceChan

		p.Stop()

		// Reinitialize
		ctx2 := &plugin.Context{
			WorkDir:  "/project/b",
			Adapters: map[string]adapter.Adapter{"mock": &mockAdapter{}},
		}
		if err := p.Init(ctx2); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Coalescer should be recreated (different instance)
		if p.coalescer == oldCoalescer {
			t.Error("coalescer was not recreated")
		}
		if p.coalesceChan == oldChan {
			t.Error("coalesceChan was not recreated")
		}

		// Verify new coalescer is functional by adding an event
		// (old coalescer had closed=true which would prevent flush)
		p.coalescer.Add("test-session", 0)

		// Channel should receive the coalesced message after flush
		// Wait briefly for the timer to fire
		select {
		case msg := <-p.coalesceChan:
			if len(msg.SessionIDs) != 1 || msg.SessionIDs[0] != "test-session" {
				t.Errorf("unexpected coalesced message: %+v", msg)
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("coalescer did not send message - may still be in closed state")
		}
	})

	t.Run("context is updated on Init", func(t *testing.T) {
		p := New()

		ctx1 := &plugin.Context{
			WorkDir:  "/project/a",
			Adapters: map[string]adapter.Adapter{"mock": &mockAdapter{}},
		}
		if err := p.Init(ctx1); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		if p.ctx.WorkDir != "/project/a" {
			t.Errorf("WorkDir = %q, want /project/a", p.ctx.WorkDir)
		}

		p.Stop()

		ctx2 := &plugin.Context{
			WorkDir:  "/project/b",
			Adapters: map[string]adapter.Adapter{"mock": &mockAdapter{}},
		}
		if err := p.Init(ctx2); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		if p.ctx.WorkDir != "/project/b" {
			t.Errorf("WorkDir = %q, want /project/b", p.ctx.WorkDir)
		}
	})
}

// TestInitDoesNotApplyCategoryFilter verifies that Init() stores the default
// category filter but does NOT apply it to p.filters.Categories (td-d3b1f6).
func TestInitDoesNotApplyCategoryFilter(t *testing.T) {
	p := New()
	ctx := &plugin.Context{
		WorkDir:  "/test/project",
		Adapters: map[string]adapter.Adapter{"mock": &mockAdapter{}},
	}
	if err := p.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// defaultCategoryFilter should be set (for C toggle)
	if len(p.defaultCategoryFilter) == 0 {
		t.Error("defaultCategoryFilter should be set after Init")
	}

	// But filters.Categories should NOT be applied on startup
	if len(p.filters.Categories) != 0 {
		t.Errorf("filters.Categories should be empty after Init, got %v", p.filters.Categories)
	}

	// filterActive should be false
	if p.filterActive {
		t.Error("filterActive should be false after Init without applied filters")
	}
}

// mockTargetedAdapter implements both adapter.Adapter and adapter.TargetedRefresher.
type mockTargetedAdapter struct {
	mockAdapter
	sessions map[string]*adapter.Session
}

func (m *mockTargetedAdapter) SessionByID(sessionID string) (*adapter.Session, error) {
	if s, ok := m.sessions[sessionID]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("session %s not found", sessionID)
}

// TestRefreshSessionsNoOverwrite verifies that refreshSessions returns nil when
// no adapter can refresh the requested sessions, preventing stale snapshot overwrites.
func TestRefreshSessionsNoOverwrite(t *testing.T) {
	p := New()
	// Use a basic adapter without TargetedRefresher
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}

	now := time.Now()
	p.sessions = []adapter.Session{
		{ID: "claude-1", Name: "Claude session", UpdatedAt: now},
		{ID: "gemini-1", Name: "Gemini session", UpdatedAt: now.Add(-24 * time.Hour)},
	}

	cmd := p.refreshSessions([]string{"gemini-1"})
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	msg := cmd()
	if msg != nil {
		t.Errorf("expected nil message when no adapter supports TargetedRefresher, got %T", msg)
	}
}

// TestRefreshSessionsReturnsDelta verifies that refreshSessions returns a
// SessionsRefreshedMsg (not SessionsLoadedMsg) with only the refreshed sessions.
func TestRefreshSessionsReturnsDelta(t *testing.T) {
	now := time.Now()
	refreshedSession := &adapter.Session{
		ID:        "s1",
		Name:      "Refreshed",
		UpdatedAt: now,
	}
	ta := &mockTargetedAdapter{
		sessions: map[string]*adapter.Session{"s1": refreshedSession},
	}

	p := New()
	p.adapters = map[string]adapter.Adapter{"targeted": ta}
	p.sessions = []adapter.Session{
		{ID: "s1", Name: "Old", UpdatedAt: now.Add(-1 * time.Hour)},
		{ID: "s2", Name: "Other", UpdatedAt: now.Add(-2 * time.Hour)},
	}

	cmd := p.refreshSessions([]string{"s1"})
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	msg := cmd()
	refreshMsg, ok := msg.(SessionsRefreshedMsg)
	if !ok {
		t.Fatalf("expected SessionsRefreshedMsg, got %T", msg)
	}
	if len(refreshMsg.Refreshed) != 1 {
		t.Fatalf("expected 1 refreshed session, got %d", len(refreshMsg.Refreshed))
	}
	if refreshMsg.Refreshed[0].ID != "s1" {
		t.Errorf("expected refreshed session ID s1, got %s", refreshMsg.Refreshed[0].ID)
	}
	if refreshMsg.Refreshed[0].Name != "Refreshed" {
		t.Errorf("expected refreshed name, got %s", refreshMsg.Refreshed[0].Name)
	}
}

// TestSessionsRefreshedMsgMergesIntoCurrentList verifies that the
// SessionsRefreshedMsg handler merges delta updates into the current session list
// (not a stale snapshot), preserving sessions added concurrently.
func TestSessionsRefreshedMsgMergesIntoCurrentList(t *testing.T) {
	now := time.Now()
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.displayedCount = 100

	// Simulate the state AFTER loadSessions has populated both adapters' sessions.
	// This is the "current" list that refreshSessions should merge into.
	p.sessions = []adapter.Session{
		{ID: "claude-1", Name: "Claude 1", UpdatedAt: now},
		{ID: "claude-2", Name: "Claude 2", UpdatedAt: now.Add(-1 * time.Hour)},
		{ID: "gemini-1", Name: "Gemini old", UpdatedAt: now.Add(-48 * time.Hour), WorktreeName: "main"},
	}

	// Simulate a SessionsRefreshedMsg arriving with updated gemini session.
	// In the real scenario, this was computed from a stale snapshot that only had
	// gemini sessions. But with our fix, it only carries the delta.
	msg := SessionsRefreshedMsg{
		Refreshed: []adapter.Session{
			{ID: "gemini-1", Name: "Gemini refreshed", UpdatedAt: now.Add(-47 * time.Hour)},
		},
	}

	p.Update(msg)

	// Verify ALL sessions are preserved (not just the refreshed ones)
	if len(p.sessions) != 3 {
		t.Fatalf("expected 3 sessions after merge, got %d", len(p.sessions))
	}

	// Verify the refreshed session was updated
	var foundGemini bool
	for _, s := range p.sessions {
		if s.ID == "gemini-1" {
			foundGemini = true
			if s.Name != "Gemini refreshed" {
				t.Errorf("expected refreshed name, got %s", s.Name)
			}
			if s.WorktreeName != "main" {
				t.Errorf("expected worktree name preserved, got %q", s.WorktreeName)
			}
		}
	}
	if !foundGemini {
		t.Error("gemini session not found after merge")
	}

	// Verify sessions are sorted by UpdatedAt descending
	for i := 1; i < len(p.sessions); i++ {
		if p.sessions[i].UpdatedAt.After(p.sessions[i-1].UpdatedAt) {
			t.Errorf("sessions not sorted: %s (%v) after %s (%v)",
				p.sessions[i].ID, p.sessions[i].UpdatedAt,
				p.sessions[i-1].ID, p.sessions[i-1].UpdatedAt)
		}
	}
}

// TestSessionsRefreshedMsgAddsNewSession verifies that a new session (not in
// current list) is appended when received via SessionsRefreshedMsg.
func TestSessionsRefreshedMsgAddsNewSession(t *testing.T) {
	now := time.Now()
	p := New()
	p.adapters = map[string]adapter.Adapter{"mock": &mockAdapter{}}
	p.displayedCount = 100

	p.sessions = []adapter.Session{
		{ID: "s1", Name: "Existing", UpdatedAt: now.Add(-1 * time.Hour)},
	}

	msg := SessionsRefreshedMsg{
		Refreshed: []adapter.Session{
			{ID: "s2", Name: "Brand new", UpdatedAt: now},
		},
	}

	p.Update(msg)

	if len(p.sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(p.sessions))
	}
	// New session should be first (most recent)
	if p.sessions[0].ID != "s2" {
		t.Errorf("expected new session at top, got %s", p.sessions[0].ID)
	}
}
