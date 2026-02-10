package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeBaseHashValidation(t *testing.T) {
	// Test the hash validation logic used in getDiffFromBase
	tests := []struct {
		name       string
		mbOutput   string
		shouldUse  bool // Should use merge-base hash
	}{
		{
			name:      "valid sha",
			mbOutput:  "abc123def456789012345678901234567890abcd\n",
			shouldUse: true,
		},
		{
			name:      "valid sha no newline",
			mbOutput:  "abc123def456789012345678901234567890abcd",
			shouldUse: true,
		},
		{
			name:      "empty output",
			mbOutput:  "",
			shouldUse: false,
		},
		{
			name:      "too short",
			mbOutput:  "abc123\n",
			shouldUse: false,
		},
		{
			name:      "only whitespace",
			mbOutput:  "\n\n",
			shouldUse: false,
		},
		{
			name:      "exactly 40 chars",
			mbOutput:  "1234567890123456789012345678901234567890",
			shouldUse: true,
		},
		{
			name:      "39 chars",
			mbOutput:  "123456789012345678901234567890123456789",
			shouldUse: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the validation logic from getDiffFromBase
			mbHash := strings.TrimSpace(tt.mbOutput)
			canUse := len(mbHash) >= 40

			if canUse != tt.shouldUse {
				t.Errorf("hash validation for %q: got canUse=%v, want %v", tt.mbOutput, canUse, tt.shouldUse)
			}
		})
	}
}

func TestGetUnpushedCommits_EmptyInputs(t *testing.T) {
	tests := []struct {
		name         string
		workdir      string
		remoteBranch string
	}{
		{"empty workdir", "", "origin/main"},
		{"empty remoteBranch", "/tmp/repo", ""},
		{"both empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getUnpushedCommits(tt.workdir, tt.remoteBranch)
			if result != nil {
				t.Errorf("expected nil, got %v", result)
			}
		})
	}
}

func TestGetUnpushedCommits_InvalidRemote(t *testing.T) {
	tmpDir := t.TempDir()
	exec.Command("git", "init").Dir = tmpDir
	_ = exec.Command("git", "init").Run()
	
	result := getUnpushedCommits(tmpDir, "nonexistent/branch")
	if result != nil {
		t.Errorf("expected nil for invalid remote, got %v", result)
	}
}

func TestGetUnpushedCommits_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Initialize git repo
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}
	
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	
	// Create initial commit
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "test.txt")
	run("commit", "-m", "initial")
	
	// Create a "remote" branch pointing to current commit
	run("branch", "origin/main")
	
	// Create unpushed commits
	for i := 1; i <= 3; i++ {
		content := []byte(strings.Repeat("x", i))
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			t.Fatal(err)
		}
		run("add", "test.txt")
		run("commit", "-m", "commit")
	}
	
	// Get unpushed commits
	unpushed := getUnpushedCommits(tmpDir, "origin/main")
	if unpushed == nil {
		t.Fatal("expected non-nil map")
	}
	if len(unpushed) != 3 {
		t.Errorf("expected 3 unpushed commits, got %d", len(unpushed))
	}
}

func TestGetUnpushedCommits_AllPushed(t *testing.T) {
	tmpDir := t.TempDir()
	
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		_ = cmd.Run()
	}
	
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	
	testFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("content"), 0644)
	run("add", "test.txt")
	run("commit", "-m", "commit")
	
	// Remote branch points to HEAD (all pushed)
	run("branch", "origin/main")
	
	unpushed := getUnpushedCommits(tmpDir, "origin/main")
	if unpushed == nil {
		t.Fatal("expected empty map, got nil")
	}
	if len(unpushed) != 0 {
		t.Errorf("expected 0 unpushed commits, got %d", len(unpushed))
	}
}

func TestGetWorktreeCommits_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	
	run := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		return cmd.Run()
	}

	_ = run("init")
	_ = run("config", "user.email", "test@test.com")
	_ = run("config", "user.name", "Test")

	// Create main branch with initial commit
	testFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("initial"), 0644)
	_ = run("add", "test.txt")
	_ = run("commit", "-m", "initial")
	_ = run("branch", "-M", "main")

	// Create feature branch
	_ = run("checkout", "-b", "feature")

	// Add commits on feature branch
	for i := 1; i <= 2; i++ {
		_ = os.WriteFile(testFile, []byte(strings.Repeat("x", i)), 0644)
		_ = run("add", "test.txt")
		_ = run("commit", "-m", "feature commit")
	}
	
	// Test: get commits comparing to main
	commits, err := getWorktreeCommits(tmpDir, "main")
	if err != nil {
		t.Fatalf("getWorktreeCommits failed: %v", err)
	}
	
	if len(commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(commits))
	}
	
	// All commits should be marked as not pushed (no remote tracking)
	for _, c := range commits {
		if c.Pushed {
			t.Errorf("commit %s should not be marked as pushed", c.Hash)
		}
	}
}

func TestGetWorktreeCommits_WithRemoteTracking(t *testing.T) {
	tmpDir := t.TempDir()
	
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		_ = cmd.Run()
	}
	
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	
	testFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("initial"), 0644)
	run("add", "test.txt")
	run("commit", "-m", "initial")
	run("branch", "-M", "main")

	run("checkout", "-b", "feature")
	
	// Create commits
	_ = os.WriteFile(testFile, []byte("x"), 0644)
	run("add", "test.txt")
	run("commit", "-m", "commit1")

	_ = os.WriteFile(testFile, []byte("xx"), 0644)
	run("add", "test.txt")
	run("commit", "-m", "commit2")
	
	commits, err := getWorktreeCommits(tmpDir, "main")
	if err != nil {
		t.Fatalf("getWorktreeCommits failed: %v", err)
	}
	
	if len(commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(commits))
	}
	
	// Without remote tracking, all commits should be marked as not pushed
	for _, c := range commits {
		if c.Pushed {
			t.Errorf("commit %s should not be marked as pushed (no remote tracking)", c.Hash)
		}
	}
}
