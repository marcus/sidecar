package gitstatus

import (
	"testing"

	"github.com/marcus/sidecar/internal/plugin"
)

func makeCommits(prefix string, count int) []*Commit {
	commits := make([]*Commit, count)
	for i := 0; i < count; i++ {
		commits[i] = &Commit{
			Hash: prefix + string(rune('a'+i)),
		}
	}
	return commits
}

func TestMergeRecentCommitsPreservesTail(t *testing.T) {
	existing := makeCommits("c", 10)
	latest := makeCommits("c", 5)

	merged := mergeRecentCommits(existing, latest)

	if len(merged) != len(existing) {
		t.Fatalf("merged length = %d, want %d", len(merged), len(existing))
	}
	for i := range existing {
		if merged[i].Hash != existing[i].Hash {
			t.Fatalf("merged[%d] = %q, want %q", i, merged[i].Hash, existing[i].Hash)
		}
	}
}

func TestMergeRecentCommitsAddsNewHead(t *testing.T) {
	existing := makeCommits("c", 8)
	latest := append([]*Commit{{Hash: "new"}}, makeCommits("c", 4)...)

	merged := mergeRecentCommits(existing, latest)

	if len(merged) != 9 {
		t.Fatalf("merged length = %d, want %d", len(merged), 9)
	}
	if merged[0].Hash != "new" {
		t.Fatalf("merged[0] = %q, want %q", merged[0].Hash, "new")
	}
	if merged[8].Hash != "ch" {
		t.Fatalf("merged[8] = %q, want %q", merged[8].Hash, "ch")
	}
}

func TestClampCommitScroll(t *testing.T) {
	p := &Plugin{
		tree:            &FileTree{},
		height:          10,
		recentCommits:   makeCommits("c", 5),
		commitScrollOff: 10,
	}

	p.clampCommitScroll()

	if p.commitScrollOff != 3 {
		t.Fatalf("commitScrollOff = %d, want %d", p.commitScrollOff, 3)
	}
}

func TestEnsureCommitListFilled(t *testing.T) {
	p := &Plugin{
		ctx:                  &plugin.Context{WorkDir: "/tmp"},
		tree:                 &FileTree{},
		height:               10,
		recentCommits:        makeCommits("c", 1),
		moreCommitsAvailable: true,
	}

	if cmd := p.ensureCommitListFilled(); cmd == nil {
		t.Fatal("expected loadMoreCommits cmd, got nil")
	}

	p.loadingMoreCommits = true
	if cmd := p.ensureCommitListFilled(); cmd != nil {
		t.Fatal("expected nil cmd while loading more commits")
	}
}
