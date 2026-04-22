package jj

import "testing"

func TestLayoutGraph_Linear(t *testing.T) {
	commits := []StructuredCommit{
		{CommitID: "aaa", Parents: []string{"bbb"}},
		{CommitID: "bbb", Parents: []string{"ccc"}},
		{CommitID: "ccc", Parents: nil},
	}
	rows := LayoutGraph(commits)
	// Linear: 3 commit rows + 2 connector rows between them
	commitCount := 0
	for _, r := range rows {
		if r.Commit != nil {
			commitCount++
		}
	}
	if commitCount != 3 {
		t.Fatalf("expected 3 commit rows, got %d", commitCount)
	}
	// All commits should be in column 0
	for _, r := range rows {
		if r.Commit != nil && r.Column != 0 {
			t.Fatalf("expected column 0 for linear graph, got %d for %s", r.Column, r.Commit.CommitID)
		}
	}
}

func TestLayoutGraph_Branch(t *testing.T) {
	// A has parent C, B has parent C -> fork
	commits := []StructuredCommit{
		{CommitID: "aaa", Parents: []string{"ccc"}},
		{CommitID: "bbb", Parents: []string{"ccc"}},
		{CommitID: "ccc", Parents: nil},
	}
	rows := LayoutGraph(commits)
	commitRows := make(map[string]GraphRow)
	for _, r := range rows {
		if r.Commit != nil {
			commitRows[r.Commit.CommitID] = r
		}
	}
	// aaa and bbb should be in different columns
	if commitRows["aaa"].Column == commitRows["bbb"].Column {
		t.Fatal("expected aaa and bbb in different columns")
	}
	// ccc should be in column 0 (primary lane)
	if commitRows["ccc"].Column != 0 {
		t.Fatalf("expected ccc in column 0, got %d", commitRows["ccc"].Column)
	}
}

func TestLayoutGraph_Merge(t *testing.T) {
	// A has parents B and C -> merge
	commits := []StructuredCommit{
		{CommitID: "aaa", Parents: []string{"bbb", "ccc"}},
		{CommitID: "bbb", Parents: []string{"ddd"}},
		{CommitID: "ccc", Parents: []string{"ddd"}},
		{CommitID: "ddd", Parents: nil},
	}
	rows := LayoutGraph(commits)
	commitRows := make(map[string]GraphRow)
	for _, r := range rows {
		if r.Commit != nil {
			commitRows[r.Commit.CommitID] = r
		}
	}
	if len(commitRows) != 4 {
		t.Fatalf("expected 4 commit rows, got %d", len(commitRows))
	}
}

func TestLayoutGraph_Empty(t *testing.T) {
	rows := LayoutGraph(nil)
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
}
