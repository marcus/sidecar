package conversations

import (
	"testing"
	"time"

	"github.com/sst/sidecar/internal/adapter"
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
	if name := p.Name(); name != "Conversations" {
		t.Errorf("expected Name 'Conversations', got %q", name)
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

	// Default view
	if ctx := p.FocusContext(); ctx != "conversations" {
		t.Errorf("expected context 'conversations', got %q", ctx)
	}

	// Message view
	p.view = ViewMessages
	if ctx := p.FocusContext(); ctx != "conversation-detail" {
		t.Errorf("expected context 'conversation-detail', got %q", ctx)
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
	p.adapter = &mockAdapter{} // Set a non-nil adapter
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

	// Initially not in search mode
	if p.searchMode {
		t.Error("expected searchMode to be false initially")
	}

	// FocusContext should be "conversations"
	if ctx := p.FocusContext(); ctx != "conversations" {
		t.Errorf("expected context 'conversations', got %q", ctx)
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
	p.adapter = &mockAdapter{}
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
	p.adapter = &mockAdapter{}
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
	p.adapter = &mockAdapter{}
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
	p.adapter = &mockAdapter{}

	msg := WatchStartedMsg{Channel: nil}
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
}

// Test WatchStartedMsg with valid channel
func TestUpdateWatchStartedMsgValidChannel(t *testing.T) {
	p := New()
	p.adapter = &mockAdapter{}

	ch := make(chan adapter.Event)
	msg := WatchStartedMsg{Channel: ch}
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
}

// Test WatchEventMsg triggers session reload
func TestUpdateWatchEventMsg(t *testing.T) {
	p := New()
	p.adapter = &mockAdapter{}
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
	p.adapter = &mockAdapter{}

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
	p.adapter = &mockAdapter{}
	p.pageSize = 2

	// Test with fewer messages than page size
	messages := []adapter.Message{
		{Role: "user", Content: "Hello"},
	}
	msg := MessagesLoadedMsg{Messages: messages}
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
	msg = MessagesLoadedMsg{Messages: messages}
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
func (m *mockAdapter) Detect(projectRoot string) (bool, error)                { return true, nil }
func (m *mockAdapter) Capabilities() adapter.CapabilitySet                    { return nil }
func (m *mockAdapter) Sessions(projectRoot string) ([]adapter.Session, error) { return nil, nil }
func (m *mockAdapter) Messages(sessionID string) ([]adapter.Message, error)   { return nil, nil }
func (m *mockAdapter) Usage(sessionID string) (*adapter.UsageStats, error)    { return nil, nil }
func (m *mockAdapter) Watch(projectRoot string) (<-chan adapter.Event, error) { return nil, nil }
