package copilot

import (
	"os"
	"testing"

	"github.com/marcus/sidecar/internal/adapter"
)

func TestSearchMessages(t *testing.T) {
	a := New()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	sessions, err := a.Sessions(cwd)
	if err != nil {
		t.Skip("no copilot sessions found for testing")
	}
	if len(sessions) == 0 {
		t.Skip("no copilot sessions available")
	}

	// Use first session for testing
	sessionID := sessions[0].ID

	// Test basic substring search
	opts := adapter.DefaultSearchOptions()
	results, err := a.SearchMessages(sessionID, "the", opts)
	if err != nil {
		t.Fatalf("SearchMessages failed: %v", err)
	}

	// Should find at least some matches (unless session is tiny)
	// Don't assert specific count since content varies
	t.Logf("Found %d message matches for 'the'", len(results))

	// Test empty query (should work without crashing)
	results, err = a.SearchMessages(sessionID, "", opts)
	if err != nil {
		t.Errorf("SearchMessages with empty query failed: %v", err)
	}

	// Test nonexistent session
	results, err = a.SearchMessages("nonexistent-session-id", "test", opts)
	if err == nil {
		t.Error("Expected error for nonexistent session")
	}
}

func TestSearchMessages_CaseSensitivity(t *testing.T) {
	a := New()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	sessions, err := a.Sessions(cwd)
	if err != nil || len(sessions) == 0 {
		t.Skip("no copilot sessions available")
	}

	sessionID := sessions[0].ID

	// Case-insensitive (default)
	opts := adapter.DefaultSearchOptions()
	results1, err := a.SearchMessages(sessionID, "THE", opts)
	if err != nil {
		t.Fatalf("Case-insensitive search failed: %v", err)
	}

	// Case-sensitive
	opts.CaseSensitive = true
	results2, err := a.SearchMessages(sessionID, "THE", opts)
	if err != nil {
		t.Fatalf("Case-sensitive search failed: %v", err)
	}

	// Case-insensitive should find at least as many matches
	if len(results1) < len(results2) {
		t.Errorf("Case-insensitive found fewer matches (%d) than case-sensitive (%d)",
			len(results1), len(results2))
	}

	t.Logf("Case-insensitive: %d matches, Case-sensitive: %d matches",
		len(results1), len(results2))
}

func TestSearchMessages_MaxResults(t *testing.T) {
	a := New()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	sessions, err := a.Sessions(cwd)
	if err != nil || len(sessions) == 0 {
		t.Skip("no copilot sessions available")
	}

	sessionID := sessions[0].ID

	// Search with limit
	opts := adapter.DefaultSearchOptions()
	opts.MaxResults = 2
	results, err := a.SearchMessages(sessionID, "the", opts)
	if err != nil {
		t.Fatalf("SearchMessages failed: %v", err)
	}

	// Count total matches across all messages
	totalMatches := adapter.TotalMatches(results)
	if totalMatches > opts.MaxResults {
		t.Errorf("Got %d matches, expected max %d", totalMatches, opts.MaxResults)
	}

	t.Logf("Limited search returned %d message matches with %d total content matches",
		len(results), totalMatches)
}
