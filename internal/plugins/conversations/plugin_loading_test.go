package conversations

import "testing"

func TestDeriveWorktreeNameFromPath(t *testing.T) {
	tests := []struct {
		name     string
		wtPath   string
		mainPath string
		want     string
	}{
		{
			name:     "standard prefixed path",
			wtPath:   "/Users/foo/code/myrepo-feature-auth",
			mainPath: "/Users/foo/code/myrepo",
			want:     "feature-auth",
		},
		{
			name:     "path without prefix",
			wtPath:   "/Users/foo/code/some-other-dir",
			mainPath: "/Users/foo/code/myrepo",
			want:     "some-other-dir",
		},
		{
			name:     "repo name with hyphen",
			wtPath:   "/Users/foo/code/my-repo-feature",
			mainPath: "/Users/foo/code/my-repo",
			want:     "feature",
		},
		{
			name:     "nested paths",
			wtPath:   "/a/b/c/repo-branch",
			mainPath: "/a/b/c/repo",
			want:     "branch",
		},
		{
			name:     "same directory",
			wtPath:   "/Users/foo/code/myrepo",
			mainPath: "/Users/foo/code/myrepo",
			want:     "myrepo",
		},
		{
			name:     "multi-part branch name",
			wtPath:   "/code/sidecar-fix-bug-123",
			mainPath: "/code/sidecar",
			want:     "fix-bug-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveWorktreeNameFromPath(tt.wtPath, tt.mainPath)
			if got != tt.want {
				t.Errorf("deriveWorktreeNameFromPath(%q, %q) = %q, want %q",
					tt.wtPath, tt.mainPath, got, tt.want)
			}
		})
	}
}

func TestSessionLoadGuard_PreventsDuplicateInFlight(t *testing.T) {
	p := New()

	token, ok := p.beginSessionLoad("codex", "/tmp/repo")
	if !ok {
		t.Fatal("first beginSessionLoad should succeed")
	}
	if token == 0 {
		t.Fatal("token should be non-zero")
	}

	if _, ok := p.beginSessionLoad("codex", "/tmp/repo"); ok {
		t.Fatal("duplicate in-flight load should be rejected")
	}

	p.endSessionLoad("codex", "/tmp/repo", token)

	if _, ok := p.beginSessionLoad("codex", "/tmp/repo"); !ok {
		t.Fatal("beginSessionLoad should succeed after endSessionLoad")
	}
}

func TestSessionLoadGuard_IgnoresStaleTokenOnEnd(t *testing.T) {
	p := New()

	oldToken, ok := p.beginSessionLoad("cursor", "/tmp/repo")
	if !ok {
		t.Fatal("initial beginSessionLoad should succeed")
	}

	// Simulate project reset replacing the in-flight map while an old goroutine is still running.
	p.sessionLoadMu.Lock()
	p.sessionLoads = make(map[string]uint64)
	p.sessionLoadMu.Unlock()

	newToken, ok := p.beginSessionLoad("cursor", "/tmp/repo")
	if !ok {
		t.Fatal("new beginSessionLoad should succeed after reset")
	}
	if newToken == oldToken {
		t.Fatal("session load tokens must remain unique across resets")
	}

	// Old goroutine completion should not clear the newer in-flight entry.
	p.endSessionLoad("cursor", "/tmp/repo", oldToken)

	if _, ok := p.beginSessionLoad("cursor", "/tmp/repo"); ok {
		t.Fatal("stale token must not clear current in-flight load")
	}

	p.endSessionLoad("cursor", "/tmp/repo", newToken)
	if _, ok := p.beginSessionLoad("cursor", "/tmp/repo"); !ok {
		t.Fatal("beginSessionLoad should succeed after valid endSessionLoad")
	}
}
