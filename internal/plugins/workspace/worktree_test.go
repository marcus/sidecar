package workspace

import (
	"testing"
)

func TestValidateBranchName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool
		wantErrs  bool // whether errors are expected
	}{
		{"valid simple", "feature-branch", true, false},
		{"valid with numbers", "feature-123", true, false},
		{"valid with underscore", "feature_branch", true, false},
		{"empty", "", false, false},
		{"starts with dash", "-feature", false, true},
		{"starts with dot", ".feature", false, true},
		{"ends with .lock", "feature.lock", false, true},
		{"contains space", "feature branch", false, true},
		{"contains tilde", "feature~branch", false, true},
		{"contains caret", "feature^branch", false, true},
		{"contains colon", "feature:branch", false, true},
		{"contains question", "feature?branch", false, true},
		{"contains asterisk", "feature*branch", false, true},
		{"contains bracket", "feature[branch", false, true},
		{"contains backslash", "feature\\branch", false, true},
		{"contains double dots", "feature..branch", false, true},
		{"contains @{", "feature@{branch", false, true},
		// Slash tests - important for branch prefix feature
		{"valid with single slash", "myrepo/feature", true, false},
		{"valid with multiple slashes", "org/repo/feature", true, false},
		{"starts with slash", "/feature", false, true},
		{"ends with slash", "feature/", false, true},
		{"double slash", "myrepo//feature", false, true},
		{"slash dot", "myrepo/.feature", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, errs, _ := ValidateBranchName(tt.input)
			if valid != tt.wantValid {
				t.Errorf("ValidateBranchName(%q) valid = %v, want %v", tt.input, valid, tt.wantValid)
			}
			hasErrs := len(errs) > 0
			if hasErrs != tt.wantErrs {
				t.Errorf("ValidateBranchName(%q) hasErrors = %v, want %v, errors: %v", tt.input, hasErrs, tt.wantErrs, errs)
			}
		})
	}
}

func TestSanitizeBranchName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"spaces to dashes", "my feature", "my-feature"},
		{"removes tilde", "feature~1", "feature1"},
		{"removes caret", "feature^2", "feature2"},
		{"removes leading dash", "-feature", "feature"},
		{"removes leading dot", ".feature", "feature"},
		{"removes trailing .lock", "feature.lock", "feature"},
		{"removes trailing dot", "feature.", "feature"},
		{"removes trailing dash", "feature-", "feature"},
		{"lowercase", "MyFeature", "myfeature"},
		{"already clean", "feature-branch", "feature-branch"},
		{"complex", "My Feature~1^2", "my-feature12"},
		// Regression tests: .lock suffix exposed after trailing character cleanup
		{"lock-with-trailing-dash", "foo.lock-", "foo"},
		{"lock-with-trailing-dashes", "bar.lock--", "bar"},
		{"lock-with-trailing-slash", "branch.lock/", "branch"},
		{"lock-trailing-dash-multiple", "test.lock.lock-", "test"},
		// Slash handling for branch prefix feature
		{"preserves single slash", "myrepo/feature", "myrepo/feature"},
		{"removes leading slash", "/feature", "feature"},
		{"removes trailing slash", "feature/", "feature"},
		{"collapses double slash", "myrepo//feature", "myrepo/feature"},
		{"removes slash-dot", "myrepo/.feature", "myrepo/feature"},
		{"spaces in path with slash", "my repo/my feature", "my-repo/my-feature"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeBranchName(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeBranchName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseWorktreeList(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		mainWorkdir string
		wantCount   int
		wantNames   []string
		wantBranch  []string
		wantIsMain    []bool // Track which worktrees should be marked as main
		wantIsMissing []bool // Track which worktrees should be marked as missing
	}{
		{
			name: "single worktree",
			output: `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/project-feature
HEAD def456
branch refs/heads/feature
`,
			mainWorkdir: "/home/user/project",
			wantCount:   2, // Main + 1 worktree
			wantNames:   []string{"project", "project-feature"},
			wantBranch:  []string{"main", "feature"},
			wantIsMain:  []bool{true, false},
		},
		{
			name: "multiple worktrees",
			output: `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/feature-a
HEAD def456
branch refs/heads/feature-a

worktree /home/user/feature-b
HEAD ghi789
branch refs/heads/feature-b
`,
			mainWorkdir: "/home/user/project",
			wantCount:   3, // Main + 2 worktrees
			wantNames:   []string{"project", "feature-a", "feature-b"},
			wantBranch:  []string{"main", "feature-a", "feature-b"},
			wantIsMain:  []bool{true, false, false},
		},
		{
			name: "detached head",
			output: `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/detached
HEAD def456
detached
`,
			mainWorkdir: "/home/user/project",
			wantCount:   2, // Main + 1 worktree
			wantNames:   []string{"project", "detached"},
			wantBranch:  []string{"main", "(detached)"},
			wantIsMain:  []bool{true, false},
		},
		{
			name:        "empty output",
			output:      "",
			mainWorkdir: "/home/user/project",
			wantCount:   0,
			wantNames:   nil,
			wantBranch:  nil,
			wantIsMain:  nil,
		},
		// Branch prefix tests - branch name has repo prefix, directory name does not
		{
			name: "prefixed branch name",
			output: `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/feature-auth
HEAD def456
branch refs/heads/project/feature-auth
`,
			mainWorkdir: "/home/user/project",
			wantCount:   2, // Main + 1 worktree
			wantNames:   []string{"project", "feature-auth"},
			wantBranch:  []string{"main", "project/feature-auth"},
			wantIsMain:  []bool{true, false},
		},
		{
			name: "multiple prefixed branches",
			output: `worktree /home/user/sidecar
HEAD abc123
branch refs/heads/main

worktree /home/user/fix-bug
HEAD def456
branch refs/heads/sidecar/fix-bug

worktree /home/user/add-feature
HEAD ghi789
branch refs/heads/sidecar/add-feature
`,
			mainWorkdir: "/home/user/sidecar",
			wantCount:   3, // Main + 2 worktrees
			wantNames:   []string{"sidecar", "fix-bug", "add-feature"},
			wantBranch:  []string{"main", "sidecar/fix-bug", "sidecar/add-feature"},
			wantIsMain:  []bool{true, false, false},
		},
		// Nested worktree directories - when branch name contains '/' and creates nested dirs
		{
			name: "nested worktree directory",
			output: `worktree /home/user/sidecar
HEAD abc123
branch refs/heads/main

worktree /home/user/sidecar-prefix/nested-branch
HEAD def456
branch refs/heads/nested-branch
`,
			mainWorkdir: "/home/user/sidecar",
			wantCount:   2, // Main + 1 worktree
			// Main uses basename, worktrees use full relative path
			wantNames:  []string{"sidecar", "sidecar-prefix/nested-branch"},
			wantBranch: []string{"main", "nested-branch"},
			wantIsMain: []bool{true, false},
		},
		{
			name: "deeply nested worktree directory",
			output: `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/project-td-123/feature/auth/login
HEAD def456
branch refs/heads/feature/auth/login
`,
			mainWorkdir: "/home/user/project",
			wantCount:   2, // Main + 1 worktree
			// Full relative path from parent dir for worktrees
			wantNames:  []string{"project", "project-td-123/feature/auth/login"},
			wantBranch: []string{"main", "feature/auth/login"},
			wantIsMain: []bool{true, false},
		},
		{
			name: "prunable worktree (folder missing)",
			output: `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/feature-deleted
HEAD def456
branch refs/heads/feature-deleted
prunable gitdir file points to non-existent location
`,
			mainWorkdir:   "/home/user/project",
			wantCount:     2,
			wantNames:     []string{"project", "feature-deleted"},
			wantBranch:    []string{"main", "feature-deleted"},
			wantIsMain:    []bool{true, false},
			wantIsMissing: []bool{false, true},
		},
		{
			name: "prunable with detached head",
			output: `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/old-checkout
HEAD def456
detached
prunable gitdir file points to non-existent location
`,
			mainWorkdir:   "/home/user/project",
			wantCount:     2,
			wantNames:     []string{"project", "old-checkout"},
			wantBranch:    []string{"main", "(detached)"},
			wantIsMain:    []bool{true, false},
			wantIsMissing: []bool{false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worktrees, err := parseWorktreeList(tt.output, tt.mainWorkdir)
			if err != nil {
				t.Fatalf("parseWorktreeList() error = %v", err)
			}

			if len(worktrees) != tt.wantCount {
				t.Errorf("got %d worktrees, want %d", len(worktrees), tt.wantCount)
			}

			for i, wt := range worktrees {
				if i < len(tt.wantNames) && wt.Name != tt.wantNames[i] {
					t.Errorf("worktree[%d].Name = %q, want %q", i, wt.Name, tt.wantNames[i])
				}
				if i < len(tt.wantBranch) && wt.Branch != tt.wantBranch[i] {
					t.Errorf("worktree[%d].Branch = %q, want %q", i, wt.Branch, tt.wantBranch[i])
				}
				if i < len(tt.wantIsMain) && wt.IsMain != tt.wantIsMain[i] {
					t.Errorf("worktree[%d].IsMain = %v, want %v", i, wt.IsMain, tt.wantIsMain[i])
				}
				if i < len(tt.wantIsMissing) && wt.IsMissing != tt.wantIsMissing[i] {
					t.Errorf("worktree[%d].IsMissing = %v, want %v", i, wt.IsMissing, tt.wantIsMissing[i])
				}
			}
		})
	}
}

func TestFilterTasks(t *testing.T) {
	allTasks := []Task{
		{ID: "td-abc123", Title: "Fix login bug", EpicTitle: "Authentication"},
		{ID: "td-def456", Title: "Add dashboard widget", EpicTitle: "Dashboard"},
		{ID: "td-ghi789", Title: "Refactor auth middleware", EpicTitle: "Authentication"},
		{ID: "td-jkl012", Title: "Update README", EpicTitle: "Documentation"},
	}

	t.Run("empty query returns all tasks", func(t *testing.T) {
		result := filterTasks("", allTasks)
		if len(result) != len(allTasks) {
			t.Errorf("empty query: got %d tasks, want %d", len(result), len(allTasks))
		}
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		result := filterTasks("zzzzz", allTasks)
		if len(result) != 0 {
			t.Errorf("no match query: got %d tasks, want 0", len(result))
		}
	})

	t.Run("fuzzy match on title", func(t *testing.T) {
		result := filterTasks("lgn", allTasks)
		if len(result) == 0 {
			t.Fatal("expected at least one result for 'lgn'")
		}
		if result[0].ID != "td-abc123" {
			t.Errorf("expected 'Fix login bug' first, got %q", result[0].Title)
		}
	})

	t.Run("match on task ID", func(t *testing.T) {
		result := filterTasks("def456", allTasks)
		if len(result) == 0 {
			t.Fatal("expected match on task ID")
		}
		if result[0].ID != "td-def456" {
			t.Errorf("expected task td-def456 first, got %q", result[0].ID)
		}
	})

	t.Run("match on epic title", func(t *testing.T) {
		result := filterTasks("auth", allTasks)
		if len(result) < 2 {
			t.Errorf("expected at least 2 results for 'auth', got %d", len(result))
		}
	})

	t.Run("results sorted by relevance", func(t *testing.T) {
		result := filterTasks("dashboard", allTasks)
		if len(result) == 0 {
			t.Fatal("expected at least one result for 'dashboard'")
		}
		if result[0].ID != "td-def456" {
			t.Errorf("expected 'Add dashboard widget' first, got %q", result[0].Title)
		}
	})

	t.Run("title match ranked higher than epic match", func(t *testing.T) {
		result := filterTasks("readme", allTasks)
		if len(result) == 0 {
			t.Fatal("expected at least one result for 'readme'")
		}
		if result[0].ID != "td-jkl012" {
			t.Errorf("expected 'Update README' first, got %q", result[0].Title)
		}
	})
}

