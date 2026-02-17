package conversations

import (
	"io"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
)

// nopCloser is a no-op io.Closer for mock adapters.
type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// mockSearchAdapter implements both Adapter and MessageSearcher for testing.
type mockSearchAdapter struct {
	id      string
	results map[string][]adapter.MessageMatch
	delay   time.Duration
	err     error
}

func (m *mockSearchAdapter) ID() string                                 { return m.id }
func (m *mockSearchAdapter) Name() string                               { return "Mock" }
func (m *mockSearchAdapter) Icon() string                               { return "M" }
func (m *mockSearchAdapter) Detect(string) (bool, error)                { return true, nil }
func (m *mockSearchAdapter) Capabilities() adapter.CapabilitySet        { return nil }
func (m *mockSearchAdapter) Sessions(string) ([]adapter.Session, error) { return nil, nil }
func (m *mockSearchAdapter) Messages(string) ([]adapter.Message, error) { return nil, nil }
func (m *mockSearchAdapter) Usage(string) (*adapter.UsageStats, error)  { return nil, nil }
func (m *mockSearchAdapter) Watch(string) (<-chan adapter.Event, io.Closer, error) {
	return nil, nopCloser{}, nil
}

func (m *mockSearchAdapter) SearchMessages(sessionID, query string, opts adapter.SearchOptions) ([]adapter.MessageMatch, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.results[sessionID], nil
}

// mockNonSearchAdapter doesn't implement MessageSearcher.
type mockNonSearchAdapter struct {
	id string
}

func (m *mockNonSearchAdapter) ID() string                                 { return m.id }
func (m *mockNonSearchAdapter) Name() string                               { return "NonSearch" }
func (m *mockNonSearchAdapter) Icon() string                               { return "N" }
func (m *mockNonSearchAdapter) Detect(string) (bool, error)                { return true, nil }
func (m *mockNonSearchAdapter) Capabilities() adapter.CapabilitySet        { return nil }
func (m *mockNonSearchAdapter) Sessions(string) ([]adapter.Session, error) { return nil, nil }
func (m *mockNonSearchAdapter) Messages(string) ([]adapter.Message, error) { return nil, nil }
func (m *mockNonSearchAdapter) Usage(string) (*adapter.UsageStats, error)  { return nil, nil }
func (m *mockNonSearchAdapter) Watch(string) (<-chan adapter.Event, io.Closer, error) {
	return nil, nopCloser{}, nil
}

func TestCountMatches(t *testing.T) {
	tests := []struct {
		name     string
		matches  []adapter.MessageMatch
		expected int
	}{
		{
			name:     "empty",
			matches:  nil,
			expected: 0,
		},
		{
			name: "single message single match",
			matches: []adapter.MessageMatch{
				{Matches: []adapter.ContentMatch{{LineNo: 1}}},
			},
			expected: 1,
		},
		{
			name: "multiple messages multiple matches",
			matches: []adapter.MessageMatch{
				{Matches: []adapter.ContentMatch{{LineNo: 1}, {LineNo: 2}}},
				{Matches: []adapter.ContentMatch{{LineNo: 3}}},
			},
			expected: 3,
		},
		{
			name: "message with no matches",
			matches: []adapter.MessageMatch{
				{Matches: []adapter.ContentMatch{}},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countMatches(tt.matches)
			if got != tt.expected {
				t.Errorf("countMatches() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestRunContentSearch_EmptyQuery(t *testing.T) {
	sessions := []adapter.Session{{ID: "s1", AdapterID: "mock"}}
	adapters := map[string]adapter.Adapter{"mock": &mockSearchAdapter{id: "mock"}}

	cmd := RunContentSearch("", sessions, adapters, adapter.SearchOptions{}, 0)
	msg := cmd()

	result, ok := msg.(ContentSearchResultsMsg)
	if !ok {
		t.Fatalf("expected ContentSearchResultsMsg, got %T", msg)
	}
	if result.Results != nil {
		t.Errorf("expected nil results for empty query, got %v", result.Results)
	}
}

func TestRunContentSearch_BasicSearch(t *testing.T) {
	now := time.Now()
	sessions := []adapter.Session{
		{ID: "s1", AdapterID: "mock", UpdatedAt: now, MessageCount: 10},
		{ID: "s2", AdapterID: "mock", UpdatedAt: now.Add(-time.Hour), MessageCount: 10},
	}

	mockAdp := &mockSearchAdapter{
		id: "mock",
		results: map[string][]adapter.MessageMatch{
			"s1": {{MessageID: "m1", Matches: []adapter.ContentMatch{{LineNo: 1}}}},
			"s2": {{MessageID: "m2", Matches: []adapter.ContentMatch{{LineNo: 2}}}},
		},
	}
	adapters := map[string]adapter.Adapter{"mock": mockAdp}

	cmd := RunContentSearch("test", sessions, adapters, adapter.SearchOptions{}, 0)
	msg := cmd()

	result, ok := msg.(ContentSearchResultsMsg)
	if !ok {
		t.Fatalf("expected ContentSearchResultsMsg, got %T", msg)
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
	// Verify sorted by UpdatedAt descending
	if result.Results[0].Session.ID != "s1" {
		t.Errorf("expected s1 first (most recent), got %s", result.Results[0].Session.ID)
	}
}

func TestRunContentSearch_NonSearchAdapter(t *testing.T) {
	sessions := []adapter.Session{
		{ID: "s1", AdapterID: "nonsearch"},
	}
	adapters := map[string]adapter.Adapter{
		"nonsearch": &mockNonSearchAdapter{id: "nonsearch"},
	}

	cmd := RunContentSearch("test", sessions, adapters, adapter.SearchOptions{}, 0)
	msg := cmd()

	result, ok := msg.(ContentSearchResultsMsg)
	if !ok {
		t.Fatalf("expected ContentSearchResultsMsg, got %T", msg)
	}
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for non-search adapter, got %d", len(result.Results))
	}
}

func TestRunContentSearch_MissingAdapter(t *testing.T) {
	sessions := []adapter.Session{
		{ID: "s1", AdapterID: "missing"},
	}
	adapters := map[string]adapter.Adapter{} // No adapters

	cmd := RunContentSearch("test", sessions, adapters, adapter.SearchOptions{}, 0)
	msg := cmd()

	result, ok := msg.(ContentSearchResultsMsg)
	if !ok {
		t.Fatalf("expected ContentSearchResultsMsg, got %T", msg)
	}
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for missing adapter, got %d", len(result.Results))
	}
}

func TestRunContentSearch_NoMatches(t *testing.T) {
	sessions := []adapter.Session{
		{ID: "s1", AdapterID: "mock"},
	}
	mockAdp := &mockSearchAdapter{
		id:      "mock",
		results: map[string][]adapter.MessageMatch{}, // No results
	}
	adapters := map[string]adapter.Adapter{"mock": mockAdp}

	cmd := RunContentSearch("test", sessions, adapters, adapter.SearchOptions{}, 0)
	msg := cmd()

	result, ok := msg.(ContentSearchResultsMsg)
	if !ok {
		t.Fatalf("expected ContentSearchResultsMsg, got %T", msg)
	}
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results when no matches, got %d", len(result.Results))
	}
}

func TestRunContentSearch_SortsByRecency(t *testing.T) {
	now := time.Now()
	sessions := []adapter.Session{
		{ID: "old", AdapterID: "mock", UpdatedAt: now.Add(-2 * time.Hour), MessageCount: 10},
		{ID: "newest", AdapterID: "mock", UpdatedAt: now, MessageCount: 10},
		{ID: "middle", AdapterID: "mock", UpdatedAt: now.Add(-time.Hour), MessageCount: 10},
	}

	mockAdp := &mockSearchAdapter{
		id: "mock",
		results: map[string][]adapter.MessageMatch{
			"old":    {{MessageID: "m1", Matches: []adapter.ContentMatch{{LineNo: 1}}}},
			"newest": {{MessageID: "m2", Matches: []adapter.ContentMatch{{LineNo: 2}}}},
			"middle": {{MessageID: "m3", Matches: []adapter.ContentMatch{{LineNo: 3}}}},
		},
	}
	adapters := map[string]adapter.Adapter{"mock": mockAdp}

	cmd := RunContentSearch("test", sessions, adapters, adapter.SearchOptions{}, 0)
	msg := cmd()

	result, ok := msg.(ContentSearchResultsMsg)
	if !ok {
		t.Fatalf("expected ContentSearchResultsMsg, got %T", msg)
	}
	if len(result.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result.Results))
	}
	// Verify order: newest, middle, old
	expectedOrder := []string{"newest", "middle", "old"}
	for i, expected := range expectedOrder {
		if result.Results[i].Session.ID != expected {
			t.Errorf("results[%d].Session.ID = %s, want %s", i, result.Results[i].Session.ID, expected)
		}
	}
}

func TestRunContentSearch_ConcurrentExecution(t *testing.T) {
	// Create many sessions to test concurrency
	sessions := make([]adapter.Session, 20)
	results := make(map[string][]adapter.MessageMatch)
	now := time.Now()
	for i := 0; i < 20; i++ {
		id := string(rune('a' + i))
		sessions[i] = adapter.Session{ID: id, AdapterID: "mock", UpdatedAt: now.Add(-time.Duration(i) * time.Minute), MessageCount: 10}
		results[id] = []adapter.MessageMatch{{MessageID: "m" + id, Matches: []adapter.ContentMatch{{LineNo: i}}}}
	}

	mockAdp := &mockSearchAdapter{
		id:      "mock",
		results: results,
		delay:   10 * time.Millisecond, // Add small delay to simulate work
	}
	adapters := map[string]adapter.Adapter{"mock": mockAdp}

	start := time.Now()
	cmd := RunContentSearch("test", sessions, adapters, adapter.SearchOptions{}, 0)
	msg := cmd()
	elapsed := time.Since(start)

	result, ok := msg.(ContentSearchResultsMsg)
	if !ok {
		t.Fatalf("expected ContentSearchResultsMsg, got %T", msg)
	}
	if len(result.Results) != 20 {
		t.Fatalf("expected 20 results, got %d", len(result.Results))
	}

	// With 4 concurrent workers and 10ms delay each, 20 sessions should take ~50ms
	// Sequential would take ~200ms. Allow some margin for test timing variance.
	if elapsed > 150*time.Millisecond {
		t.Logf("warning: search took %v, expected concurrent execution to be faster", elapsed)
	}
}

func TestScheduleContentSearch(t *testing.T) {
	query := "test query"
	version := 42

	cmd := scheduleContentSearch(query, version)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	// Execute the command - it will wait for debounce delay
	start := time.Now()
	msg := cmd()
	elapsed := time.Since(start)

	// Verify the message type and contents
	debounceMsg, ok := msg.(ContentSearchDebounceMsg)
	if !ok {
		t.Fatalf("expected ContentSearchDebounceMsg, got %T", msg)
	}
	if debounceMsg.Query != query {
		t.Errorf("Query = %q, want %q", debounceMsg.Query, query)
	}
	if debounceMsg.Version != version {
		t.Errorf("Version = %d, want %d", debounceMsg.Version, version)
	}

	// Verify it waited approximately the debounce delay
	if elapsed < 150*time.Millisecond {
		t.Errorf("elapsed = %v, expected >= 150ms (debounce delay is 200ms)", elapsed)
	}
}
