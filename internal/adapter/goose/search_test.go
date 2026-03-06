package goose

import (
	"testing"

	"github.com/marcus/sidecar/internal/adapter"
)

func TestSearchMessages_InterfaceCompliance(t *testing.T) {
	a := New()
	var _ adapter.MessageSearcher = a
}

func TestSearchMessages_NonExistentSession(t *testing.T) {
	dbPath, _, _ := createGooseFixture(t)
	a := &Adapter{dbPath: dbPath}

	results, err := a.SearchMessages("nonexistent-session-xyz", "test", adapter.DefaultSearchOptions())
	if err != nil {
		t.Fatalf("expected no error for nonexistent session, got %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results, got %#v", results)
	}
}

func TestSearchMessages_FindsToolContent(t *testing.T) {
	dbPath, _, sessionID := createGooseFixture(t)
	a := &Adapter{dbPath: dbPath}

	results, err := a.SearchMessages(sessionID, "list_files", adapter.DefaultSearchOptions())
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one match")
	}
	if len(results[0].Matches) == 0 {
		t.Fatal("expected content matches in first result")
	}
}
