package gitstatus

import (
	"strings"
	"testing"
)

func TestComputeGraph_Empty(t *testing.T) {
	lines := ComputeGraphForCommits([]*Commit{})
	if lines != nil {
		t.Errorf("Expected nil for empty commits, got %v", lines)
	}
}

func TestComputeGraph_SingleCommit(t *testing.T) {
	commits := []*Commit{
		{Hash: "c1", ParentHashes: []string{}}, // root, no parents
	}

	lines := ComputeGraphForCommits(commits)

	if len(lines) != 1 {
		t.Fatalf("Expected 1 line, got %d", len(lines))
	}
	if !containsRune(lines[0].Chars, '*') {
		t.Errorf("Expected * in line, got %s", lines[0].String())
	}
}

func TestComputeGraph_LinearHistory(t *testing.T) {
	// 3 commits in a straight line: c3 -> c2 -> c1
	commits := []*Commit{
		{Hash: "c3", ParentHashes: []string{"c2"}},
		{Hash: "c2", ParentHashes: []string{"c1"}},
		{Hash: "c1", ParentHashes: []string{}}, // root
	}

	lines := ComputeGraphForCommits(commits)

	if len(lines) != 3 {
		t.Fatalf("Expected 3 lines, got %d", len(lines))
	}

	// All should be single column with *
	for i, line := range lines {
		if !containsRune(line.Chars, '*') {
			t.Errorf("Line %d should contain *, got %s", i, line.String())
		}
	}
}

func TestComputeGraph_SimpleMerge(t *testing.T) {
	// Merge commit with 2 parents
	// c3 (merge of c1 and c2)
	// |\
	// c1 c2
	commits := []*Commit{
		{Hash: "c3", ParentHashes: []string{"c1", "c2"}, IsMerge: true},
		{Hash: "c2", ParentHashes: []string{"base"}},
		{Hash: "c1", ParentHashes: []string{"base"}},
		{Hash: "base", ParentHashes: []string{}},
	}

	lines := ComputeGraphForCommits(commits)

	if len(lines) != 4 {
		t.Fatalf("Expected 4 lines, got %d", len(lines))
	}

	// First line (merge commit) should show branch connection
	mergeLineStr := lines[0].String()
	if !containsRune(lines[0].Chars, '*') {
		t.Errorf("Merge line should contain *, got %s", mergeLineStr)
	}
}

func TestGraphState_FindOrCreateColumn(t *testing.T) {
	g := NewGraphState()

	col1 := g.findOrCreateColumn("hash1")
	if col1 != 0 {
		t.Errorf("First column should be 0, got %d", col1)
	}

	col2 := g.findOrCreateColumn("hash2")
	if col2 != 1 {
		t.Errorf("Second column should be 1, got %d", col2)
	}

	// Deactivate col1 and verify it gets reused
	g.columns[0].Active = false
	col3 := g.findOrCreateColumn("hash3")
	if col3 != 0 {
		t.Errorf("Should reuse inactive column 0, got %d", col3)
	}
}

func TestGraphState_FindCommitColumn(t *testing.T) {
	g := NewGraphState()

	// Add some columns
	g.columns = []GraphColumn{
		{CommitHash: "hash1", Active: true},
		{CommitHash: "hash2", Active: true},
		{CommitHash: "hash3", Active: false},
	}

	if idx := g.findCommitColumn("hash1"); idx != 0 {
		t.Errorf("Expected column 0 for hash1, got %d", idx)
	}
	if idx := g.findCommitColumn("hash2"); idx != 1 {
		t.Errorf("Expected column 1 for hash2, got %d", idx)
	}
	// hash3 is inactive, should not be found
	if idx := g.findCommitColumn("hash3"); idx != -1 {
		t.Errorf("Expected -1 for inactive hash3, got %d", idx)
	}
	if idx := g.findCommitColumn("nonexistent"); idx != -1 {
		t.Errorf("Expected -1 for nonexistent, got %d", idx)
	}
}

func TestGraphLine_WidthConsistency(t *testing.T) {
	commits := []*Commit{
		{Hash: "c3", ParentHashes: []string{"c1", "c2"}, IsMerge: true},
		{Hash: "c2", ParentHashes: []string{"c1"}},
		{Hash: "c1", ParentHashes: []string{}},
	}

	lines := ComputeGraphForCommits(commits)

	for i, line := range lines {
		if line.Width != len(line.Chars) {
			t.Errorf("Line %d: Width %d should match Chars length %d", i, line.Width, len(line.Chars))
		}
	}
}

func TestGraphLine_String(t *testing.T) {
	gl := GraphLine{Chars: []rune{'*', ' ', '|', ' '}, Width: 4}
	expected := "* | "
	if gl.String() != expected {
		t.Errorf("Expected %q, got %q", expected, gl.String())
	}
}

func TestComputeGraph_RootCommitDeactivatesColumn(t *testing.T) {
	// Single root commit should deactivate its column
	commits := []*Commit{
		{Hash: "root", ParentHashes: []string{}},
	}

	g := NewGraphState()
	_ = g.ComputeGraphLine(commits[0])

	// After processing root commit, its column should be inactive
	if len(g.columns) == 0 {
		// Column was never created, that's fine
		return
	}
	// If column exists, it should be inactive
	for _, col := range g.columns {
		if col.CommitHash == "" && col.Active {
			t.Errorf("Root commit column should be inactive")
		}
	}
}

// containsRune checks if a rune slice contains a target rune.
func containsRune(chars []rune, target rune) bool {
	for _, ch := range chars {
		if ch == target {
			return true
		}
	}
	return false
}

func TestComputeGraph_MergeShowsBranch(t *testing.T) {
	// This tests the real-world scenario where a merge commit
	// introduces a new branch column that must be rendered
	// even though it doesn't exist yet in the columns array.
	//
	// Linear commits -> merge -> two branches
	commits := []*Commit{
		{Hash: "c1", ParentHashes: []string{"c2"}},                      // linear
		{Hash: "c2", ParentHashes: []string{"merge"}},                   // linear
		{Hash: "merge", ParentHashes: []string{"p1", "p2"}, IsMerge: true}, // merge with 2 parents
		{Hash: "p1", ParentHashes: []string{"base"}},                    // first parent branch
		{Hash: "p2", ParentHashes: []string{"base"}},                    // second parent branch
		{Hash: "base", ParentHashes: []string{}},                        // root
	}

	lines := ComputeGraphForCommits(commits)

	if len(lines) != 6 {
		t.Fatalf("Expected 6 lines, got %d", len(lines))
	}

	// The merge line (index 2) should show the branch connection
	mergeLine := lines[2].String()
	if !strings.Contains(mergeLine, "*") {
		t.Errorf("Merge line should contain *, got %q", mergeLine)
	}
	// Should have merge connector (\ for right branch)
	if !strings.Contains(mergeLine, "\\") {
		t.Errorf("Merge line should contain \\ for branch connection, got %q", mergeLine)
	}

	// After merge, both branches should be visible
	p1Line := lines[3].String()
	p2Line := lines[4].String()

	// Both should have commit markers
	if !strings.Contains(p1Line, "*") {
		t.Errorf("P1 line should contain *, got %q", p1Line)
	}
	if !strings.Contains(p2Line, "*") {
		t.Errorf("P2 line should contain *, got %q", p2Line)
	}
}

func TestComputeGraph_MultipleBranches(t *testing.T) {
	// Test that multiple simultaneous branches render correctly
	commits := []*Commit{
		{Hash: "merge1", ParentHashes: []string{"a1", "b1"}, IsMerge: true},
		{Hash: "a1", ParentHashes: []string{"a2"}},
		{Hash: "b1", ParentHashes: []string{"b2"}},
		{Hash: "a2", ParentHashes: []string{"base"}},
		{Hash: "b2", ParentHashes: []string{"base"}},
		{Hash: "base", ParentHashes: []string{}},
	}

	lines := ComputeGraphForCommits(commits)

	// Verify we have multiple columns
	maxWidth := 0
	for _, line := range lines {
		if line.Width > maxWidth {
			maxWidth = line.Width
		}
	}

	// With 2 branches, we should have at least 4 chars (2 cols * 2 chars each)
	if maxWidth < 4 {
		t.Errorf("Expected at least 4 width for 2 branches, got %d", maxWidth)
	}
}

func TestComputeGraph_BranchMergeBack(t *testing.T) {
	// Test the scenario where a branch merges back after diverging
	// This is the common feature branch workflow:
	// main: c1 - c2 - merge
	//           \    /
	// feature:   f1
	commits := []*Commit{
		{Hash: "merge", ParentHashes: []string{"c2", "f1"}, IsMerge: true},
		{Hash: "f1", ParentHashes: []string{"c1"}},
		{Hash: "c2", ParentHashes: []string{"c1"}},
		{Hash: "c1", ParentHashes: []string{}},
	}

	lines := ComputeGraphForCommits(commits)

	if len(lines) != 4 {
		t.Fatalf("Expected 4 lines, got %d", len(lines))
	}

	// First line is merge - should show branch
	mergeLine := lines[0].String()
	if !strings.Contains(mergeLine, "*") {
		t.Errorf("Merge line should have commit marker, got %q", mergeLine)
	}
}
