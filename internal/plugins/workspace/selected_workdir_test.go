package workspace

import (
	"testing"
)

func TestSelectedWorkDir_ReturnsMainWorktreePath(t *testing.T) {
	// SelectedWorkDir should return the path even for the main worktree,
	// so that switching back to main from a feature worktree works correctly.
	// See: https://github.com/marcus/sidecar/issues/143
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "main", Path: "/repo", IsMain: true},
			{Name: "feature", Path: "/repo-feature", IsMain: false},
		},
		selectedIdx: 0, // main selected
	}

	got := p.SelectedWorkDir()
	if got != "/repo" {
		t.Errorf("SelectedWorkDir() with main selected = %q, want %q", got, "/repo")
	}
}

func TestSelectedWorkDir_ReturnsFeatureWorktreePath(t *testing.T) {
	p := &Plugin{
		worktrees: []*Worktree{
			{Name: "main", Path: "/repo", IsMain: true},
			{Name: "feature", Path: "/repo-feature", IsMain: false},
		},
		selectedIdx: 1, // feature selected
	}

	got := p.SelectedWorkDir()
	if got != "/repo-feature" {
		t.Errorf("SelectedWorkDir() with feature selected = %q, want %q", got, "/repo-feature")
	}
}

func TestSelectedWorkDir_ReturnsEmptyWhenNoWorktrees(t *testing.T) {
	p := &Plugin{}

	got := p.SelectedWorkDir()
	if got != "" {
		t.Errorf("SelectedWorkDir() with no worktrees = %q, want empty", got)
	}
}
