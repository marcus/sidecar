package copilot

import (
	"testing"

	"github.com/marcus/sidecar/internal/adapter"
)

func TestSearchMessages(t *testing.T) {
	a, sessionID, projectRoot := setupTestSession(t, "valid_events.jsonl")

	_, err := a.Sessions(projectRoot)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	opts := adapter.DefaultSearchOptions()
	results, err := a.SearchMessages(sessionID, "help", opts)
	if err != nil {
		t.Fatalf("SearchMessages failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected at least one match for 'help'")
	}
	t.Logf("Found %d message matches for 'help'", len(results))

	// Test empty query
	results, err = a.SearchMessages(sessionID, "", opts)
	if err != nil {
		t.Errorf("SearchMessages with empty query failed: %v", err)
	}

	// Test nonexistent session
	_, err = a.SearchMessages("nonexistent-session-id", "test", opts)
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestSearchMessages_CaseSensitivity(t *testing.T) {
	a, sessionID, projectRoot := setupTestSession(t, "valid_events.jsonl")

	_, err := a.Sessions(projectRoot)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	// Case-insensitive (default)
	opts := adapter.DefaultSearchOptions()
	results1, err := a.SearchMessages(sessionID, "HELLO", opts)
	if err != nil {
		t.Fatalf("Case-insensitive search failed: %v", err)
	}

	// Case-sensitive
	opts.CaseSensitive = true
	results2, err := a.SearchMessages(sessionID, "HELLO", opts)
	if err != nil {
		t.Fatalf("Case-sensitive search failed: %v", err)
	}

	if len(results1) < len(results2) {
		t.Errorf("Case-insensitive found fewer matches (%d) than case-sensitive (%d)",
			len(results1), len(results2))
	}

	t.Logf("Case-insensitive: %d matches, Case-sensitive: %d matches",
		len(results1), len(results2))
}

func TestSearchMessages_MaxResults(t *testing.T) {
	a, sessionID, projectRoot := setupTestSession(t, "valid_events.jsonl")

	_, err := a.Sessions(projectRoot)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	opts := adapter.DefaultSearchOptions()
	opts.MaxResults = 1
	results, err := a.SearchMessages(sessionID, "you", opts)
	if err != nil {
		t.Fatalf("SearchMessages failed: %v", err)
	}

	totalMatches := adapter.TotalMatches(results)
	if totalMatches > opts.MaxResults {
		t.Errorf("Got %d matches, expected max %d", totalMatches, opts.MaxResults)
	}

	t.Logf("Limited search returned %d message matches with %d total content matches",
		len(results), totalMatches)
}
