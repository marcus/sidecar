package workspace

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestMergeWorkflowStepString(t *testing.T) {
	tests := []struct {
		step     MergeWorkflowStep
		expected string
	}{
		{MergeStepReviewDiff, "Review Diff"},
		{MergeStepPush, "Push Branch"},
		{MergeStepCreatePR, "Create PR"},
		{MergeStepWaitingMerge, "Waiting for Merge"},
		{MergeStepPostMergeConfirmation, "Confirm Cleanup"},
		{MergeStepCleanup, "Cleanup"},
		{MergeStepDone, "Done"},
		{MergeWorkflowStep(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.step.String()
			if result != tt.expected {
				t.Errorf("MergeWorkflowStep(%d).String() = %q, want %q", tt.step, result, tt.expected)
			}
		})
	}
}

func TestTruncateDiff(t *testing.T) {
	tests := []struct {
		name     string
		diff     string
		maxLines int
		wantLen  int // Expected number of lines
	}{
		{
			name:     "short diff",
			diff:     "line1\nline2\nline3",
			maxLines: 5,
			wantLen:  3,
		},
		{
			name:     "exact limit",
			diff:     "line1\nline2\nline3\nline4\nline5",
			maxLines: 5,
			wantLen:  5,
		},
		{
			name:     "over limit",
			diff:     "line1\nline2\nline3\nline4\nline5\nline6\nline7",
			maxLines: 3,
			wantLen:  4, // 3 lines + truncation message
		},
		{
			name:     "empty diff",
			diff:     "",
			maxLines: 5,
			wantLen:  1, // Just the empty string
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateDiff(tt.diff, tt.maxLines)

			// For short diff, result should equal input
			if tt.name == "short diff" && result != tt.diff {
				t.Errorf("truncateDiff() should not modify short diff")
			}

			// For over limit, result should contain truncation message
			if tt.name == "over limit" && !strings.Contains(result, "more lines") {
				t.Errorf("truncateDiff() over limit should contain truncation message")
			}
		})
	}
}

func TestMergeWorkflowState(t *testing.T) {
	wt := &Worktree{
		Name:       "test-branch",
		Path:       "/tmp/test",
		Branch:     "test-branch",
		BaseBranch: "main",
	}

	state := &MergeWorkflowState{
		Worktree:   wt,
		Step:       MergeStepReviewDiff,
		PRTitle:    "Test PR",
		StepStatus: make(map[MergeWorkflowStep]string),
	}

	// Test initial state
	if state.Worktree != wt {
		t.Error("Worktree not set correctly")
	}
	if state.Step != MergeStepReviewDiff {
		t.Errorf("Step = %v, want MergeStepReviewDiff", state.Step)
	}

	// Test step status
	state.StepStatus[MergeStepReviewDiff] = "done"
	if state.StepStatus[MergeStepReviewDiff] != "done" {
		t.Error("StepStatus not working correctly")
	}
}

func TestCancelMergeWorkflow(t *testing.T) {
	p := &Plugin{
		viewMode: ViewModeMerge,
		mergeState: &MergeWorkflowState{
			Worktree: &Worktree{Name: "test"},
		},
	}

	p.cancelMergeWorkflow()

	if p.mergeState != nil {
		t.Error("mergeState should be nil after cancel")
	}
	if p.viewMode != ViewModeList {
		t.Errorf("viewMode = %v, want ViewModeList", p.viewMode)
	}
}

func TestParsePRMergeStatus(t *testing.T) {
	// Test parsing various JSON responses from gh pr view
	tests := []struct {
		name     string
		json     string
		expected bool
	}{
		{
			name:     "merged true",
			json:     `{"state":"MERGED","merged":true}`,
			expected: true,
		},
		{
			name:     "merged true with whitespace",
			json:     `{"state": "MERGED", "merged": true}`,
			expected: true,
		},
		{
			name:     "state MERGED only",
			json:     `{"state":"MERGED","merged":false}`,
			expected: true, // State takes precedence
		},
		{
			name:     "not merged",
			json:     `{"state":"OPEN","merged":false}`,
			expected: false,
		},
		{
			name:     "closed but not merged",
			json:     `{"state":"CLOSED","merged":false}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse using same logic as checkPRMerged
			var prStatus struct {
				State  string `json:"state"`
				Merged bool   `json:"merged"`
			}
			err := json.Unmarshal([]byte(tt.json), &prStatus)
			if err != nil {
				t.Fatalf("failed to parse JSON: %v", err)
			}

			merged := prStatus.Merged || prStatus.State == "MERGED"
			if merged != tt.expected {
				t.Errorf("parsed merged = %v, want %v", merged, tt.expected)
			}
		})
	}
}

func TestCheckCleanupComplete(t *testing.T) {
	tests := []struct {
		name        string
		pendingOps  int
		wantDone    bool
		wantOpsLeft int
	}{
		{
			name:        "last operation completes",
			pendingOps:  1,
			wantDone:    true,
			wantOpsLeft: 0,
		},
		{
			name:        "still waiting for more",
			pendingOps:  3,
			wantDone:    false,
			wantOpsLeft: 2,
		},
		{
			name:        "already zero",
			pendingOps:  0,
			wantDone:    true,
			wantOpsLeft: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{
				mergeState: &MergeWorkflowState{
					Step:              MergeStepCleanup,
					StepStatus:        make(map[MergeWorkflowStep]string),
					PendingCleanupOps: tt.pendingOps,
				},
			}

			done := p.checkCleanupComplete()

			if done != tt.wantDone {
				t.Errorf("checkCleanupComplete() = %v, want %v", done, tt.wantDone)
			}
			if p.mergeState.PendingCleanupOps != tt.wantOpsLeft {
				t.Errorf("PendingCleanupOps = %v, want %v", p.mergeState.PendingCleanupOps, tt.wantOpsLeft)
			}
			if done && p.mergeState.Step != MergeStepDone {
				t.Errorf("Step = %v, want MergeStepDone when done", p.mergeState.Step)
			}
		})
	}
}

func TestDeleteDoneMsgWarnings(t *testing.T) {
	// Test that DeleteDoneMsg properly carries warnings
	msg := DeleteDoneMsg{
		Name:     "test-worktree",
		Err:      nil,
		Warnings: []string{"Local branch: branch 'feature' not found", "Remote branch: not found"},
	}

	if msg.Name != "test-worktree" {
		t.Errorf("Name = %v, want test-worktree", msg.Name)
	}
	if msg.Err != nil {
		t.Errorf("Err = %v, want nil", msg.Err)
	}
	if len(msg.Warnings) != 2 {
		t.Errorf("len(Warnings) = %v, want 2", len(msg.Warnings))
	}
}

func TestParseExistingPRURL(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantURL   string
		wantFound bool
	}{
		{
			name:      "standard error with PR URL",
			output:    `a pull request for branch "workspace-improvements" into branch "main" already exists: https://github.com/marcus/sidecar/pull/30: exit status 1`,
			wantURL:   "https://github.com/marcus/sidecar/pull/30",
			wantFound: true,
		},
		{
			name:      "error without exit status suffix",
			output:    `a pull request for branch "feature" into branch "main" already exists: https://github.com/owner/repo/pull/123`,
			wantURL:   "https://github.com/owner/repo/pull/123",
			wantFound: true,
		},
		{
			name:      "different error message",
			output:    `GraphQL: Could not resolve to a Repository with the name 'owner/repo'.`,
			wantURL:   "",
			wantFound: false,
		},
		{
			name:      "empty output",
			output:    ``,
			wantURL:   "",
			wantFound: false,
		},
		{
			name:      "already exists but no URL",
			output:    `a pull request already exists: `,
			wantURL:   "",
			wantFound: false,
		},
		{
			name:      "URL with trailing newline",
			output:    "a pull request already exists: https://github.com/o/r/pull/1\n",
			wantURL:   "https://github.com/o/r/pull/1",
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotFound := parseExistingPRURL(tt.output)
			if gotURL != tt.wantURL {
				t.Errorf("parseExistingPRURL() url = %q, want %q", gotURL, tt.wantURL)
			}
			if gotFound != tt.wantFound {
				t.Errorf("parseExistingPRURL() found = %v, want %v", gotFound, tt.wantFound)
			}
		})
	}
}

func TestParsePRGenerationOutput(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantTitle string
		wantBody  string
	}{
		{
			name:      "clean output with both markers",
			output:    "PR_TITLE: Fix authentication bug\nPR_BODY:\n## Summary\nFixed the auth flow.",
			wantTitle: "Fix authentication bug",
			wantBody:  "## Summary\nFixed the auth flow.",
		},
		{
			name:      "output with preamble before markers",
			output:    "Here is the PR description:\n\nPR_TITLE: Add user dashboard\nPR_BODY:\nNew dashboard feature.",
			wantTitle: "Add user dashboard",
			wantBody:  "New dashboard feature.",
		},
		{
			name:      "title only no body marker",
			output:    "PR_TITLE: Quick fix\nSome body text here.",
			wantTitle: "Quick fix",
			wantBody:  "Some body text here.",
		},
		{
			name:      "empty output",
			output:    "",
			wantTitle: "",
			wantBody:  "",
		},
		{
			name:      "no markers at all",
			output:    "This is just some random text without markers.",
			wantTitle: "",
			wantBody:  "",
		},
		{
			name:      "title with empty body",
			output:    "PR_TITLE: Some title\nPR_BODY:\n",
			wantTitle: "Some title",
			wantBody:  "",
		},
		{
			name:      "title only on last line",
			output:    "PR_TITLE: Solo title",
			wantTitle: "Solo title",
			wantBody:  "",
		},
		{
			name:      "whitespace around markers",
			output:    "  \n PR_TITLE:   Trimmed title  \n PR_BODY: \n  Body with spaces  \n",
			wantTitle: "Trimmed title",
			wantBody:  "Body with spaces",
		},
		{
			name:      "multiline body",
			output:    "PR_TITLE: Feature X\nPR_BODY:\n## Summary\n- Change 1\n- Change 2\n\n## Details\nMore info here.",
			wantTitle: "Feature X",
			wantBody:  "## Summary\n- Change 1\n- Change 2\n\n## Details\nMore info here.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotBody := parsePRGenerationOutput(tt.output)
			if gotTitle != tt.wantTitle {
				t.Errorf("parsePRGenerationOutput() title = %q, want %q", gotTitle, tt.wantTitle)
			}
			if gotBody != tt.wantBody {
				t.Errorf("parsePRGenerationOutput() body = %q, want %q", gotBody, tt.wantBody)
			}
		})
	}
}

func TestBuildFallbackPRDescription(t *testing.T) {
	tests := []struct {
		name        string
		branch      string
		commitLog   string
		diffStat    string
		wantTitle   string
		wantBodyHas []string
	}{
		{
			name:      "basic branch name cleanup",
			branch:    "feature/add-user-auth",
			commitLog: "abc1234 Add login endpoint\ndef5678 Add logout",
			diffStat:  " 3 files changed, 50 insertions(+)",
			wantTitle: "feature add user auth",
			wantBodyHas: []string{"Add login endpoint", "Add logout", "Files Changed"},
		},
		{
			name:      "underscore and dash cleanup",
			branch:    "fix_the-broken_thing",
			commitLog: "",
			diffStat:  "",
			wantTitle: "fix the broken thing",
			wantBodyHas: []string{"## Summary"},
		},
		{
			name:      "empty everything",
			branch:    "",
			commitLog: "",
			diffStat:  "",
			wantTitle: "",
			wantBodyHas: []string{"## Summary"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotBody := buildFallbackPRDescription(tt.branch, tt.commitLog, tt.diffStat)
			if gotTitle != tt.wantTitle {
				t.Errorf("buildFallbackPRDescription() title = %q, want %q", gotTitle, tt.wantTitle)
			}
			for _, substr := range tt.wantBodyHas {
				if !strings.Contains(gotBody, substr) {
					t.Errorf("buildFallbackPRDescription() body missing %q", substr)
				}
			}
		})
	}
}

func TestPrintModeArgsEntries(t *testing.T) {
	// Verify all PrintModeArgs entries have corresponding AgentCommands
	for agentType, args := range PrintModeArgs {
		if len(args) == 0 {
			t.Errorf("PrintModeArgs[%q] is empty", agentType)
		}
		cmd, ok := AgentCommands[agentType]
		if !ok || cmd == "" {
			t.Errorf("PrintModeArgs has %q but AgentCommands does not", agentType)
		}
	}
}

func TestSummarizeGitError(t *testing.T) {
	tests := []struct {
		name         string
		errMsg       string
		wantSummary  string
		wantDiverged bool
	}{
		{
			name:         "nil error",
			errMsg:       "",
			wantSummary:  "",
			wantDiverged: false,
		},
		{
			name:         "fast-forward not possible",
			errMsg:       "pull: fatal: Not possible to fast-forward, aborting.: exit status 128",
			wantSummary:  "Local and remote branches have diverged",
			wantDiverged: true,
		},
		{
			name:         "cannot fast-forward",
			errMsg:       "pull: error: cannot fast-forward your working tree",
			wantSummary:  "Local and remote branches have diverged",
			wantDiverged: true,
		},
		{
			name:         "branches have diverged",
			errMsg:       "pull: hint: Diverging branches have diverged and must be merged",
			wantSummary:  "Local and remote branches have diverged",
			wantDiverged: true,
		},
		{
			name:         "rebase conflict",
			errMsg:       "rebase failed: CONFLICT (content): Merge conflict in file.go",
			wantSummary:  "Conflicts detected - resolve manually",
			wantDiverged: false,
		},
		{
			name:         "merge conflict",
			errMsg:       "merge failed: Automatic merge failed; fix conflicts and then commit",
			wantSummary:  "Conflicts detected - resolve manually",
			wantDiverged: false,
		},
		{
			name:         "unmerged files",
			errMsg:       "error: you have unmerged files in the working tree",
			wantSummary:  "Unmerged files - resolve conflicts manually",
			wantDiverged: false,
		},
		{
			name:         "local changes blocking",
			errMsg:       "error: Your local changes to the following files would be overwritten",
			wantSummary:  "Uncommitted local changes blocking pull",
			wantDiverged: false,
		},
		{
			name:         "network error",
			errMsg:       "fatal: Could not resolve host: github.com",
			wantSummary:  "Network error - unable to reach remote",
			wantDiverged: false,
		},
		{
			name:         "permission denied",
			errMsg:       "Permission denied (publickey)",
			wantSummary:  "Authentication failed",
			wantDiverged: false,
		},
		{
			name:         "not a git repository",
			errMsg:       "fatal: not a git repository",
			wantSummary:  "Git repository not found",
			wantDiverged: false,
		},
		{
			name:         "unknown error truncated",
			errMsg:       "some very long error message that exceeds sixty characters and should be truncated properly",
			wantSummary:  "some very long error message that exceeds sixty character...",
			wantDiverged: false,
		},
		{
			name:         "multiline error uses first line",
			errMsg:       "first line of error\nsecond line\nthird line",
			wantSummary:  "first line of error",
			wantDiverged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = fmt.Errorf("%s", tt.errMsg)
			}

			gotSummary, gotFull, gotDiverged := summarizeGitError(err)

			if gotSummary != tt.wantSummary {
				t.Errorf("summarizeGitError() summary = %q, want %q", gotSummary, tt.wantSummary)
			}
			if gotDiverged != tt.wantDiverged {
				t.Errorf("summarizeGitError() diverged = %v, want %v", gotDiverged, tt.wantDiverged)
			}
			// Full error should preserve original message
			if err != nil && gotFull != tt.errMsg {
				t.Errorf("summarizeGitError() full = %q, want %q", gotFull, tt.errMsg)
			}
		})
	}
}
