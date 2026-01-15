package worktree

import (
	"os"
	"strings"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with.dot", "with-dot"},
		{"with:colon", "with-colon"},
		{"with/slash", "with-slash"},
		{"multi.dot:colon/slash", "multi-dot-colon-slash"},
		{"already-clean", "already-clean"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetAgentCommand(t *testing.T) {
	tests := []struct {
		agentType AgentType
		expected  string
	}{
		{AgentClaude, "claude"},
		{AgentCodex, "codex"},
		{AgentAider, "aider"},
		{AgentGemini, "gemini"},
		{AgentCursor, "cursor-agent"},
		{AgentOpenCode, "opencode"},
		{AgentCustom, "claude"}, // Falls back to claude
	}

	for _, tt := range tests {
		t.Run(string(tt.agentType), func(t *testing.T) {
			result := getAgentCommand(tt.agentType)
			if result != tt.expected {
				t.Errorf("getAgentCommand(%q) = %q, want %q", tt.agentType, result, tt.expected)
			}
		})
	}
}

func TestDetectStatus(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected WorktreeStatus
	}{
		{
			name:     "waiting for y/n prompt",
			output:   "Some output\nDo you want to continue? [y/n]",
			expected: StatusWaiting,
		},
		{
			name:     "waiting for y/n in parentheses",
			output:   "Some output\nProceed? (y/n):",
			expected: StatusWaiting,
		},
		{
			name:     "allow edit prompt",
			output:   "Claude wants to edit file.go\nAllow edit? [y/n]",
			expected: StatusWaiting,
		},
		{
			name:     "allow bash prompt",
			output:   "Command: rm -rf /tmp/foo\nAllow bash? [y/n]",
			expected: StatusWaiting,
		},
		{
			name:     "approve prompt",
			output:   "Please approve this change",
			expected: StatusWaiting,
		},
		{
			name:     "task completed",
			output:   "All changes applied\nTask completed successfully",
			expected: StatusDone,
		},
		{
			name:     "finished",
			output:   "Output\nFinished processing",
			expected: StatusDone,
		},
		{
			name:     "error detected",
			output:   "Error: file not found",
			expected: StatusError,
		},
		{
			name:     "failed",
			output:   "Build failed with 3 errors",
			expected: StatusError,
		},
		{
			name:     "traceback",
			output:   "Traceback (most recent call last):\n  File...",
			expected: StatusError,
		},
		{
			name:     "normal active output",
			output:   "Processing files...\nCompiling main.go",
			expected: StatusActive,
		},
		{
			name:     "empty output",
			output:   "",
			expected: StatusActive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectStatus(tt.output)
			if result != tt.expected {
				t.Errorf("detectStatus(%q) = %v, want %v", tt.output, result, tt.expected)
			}
		})
	}
}

func TestExtractPrompt(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{
			name:     "y/n prompt",
			output:   "Some context\nDo you want to continue? [y/n]",
			expected: "Do you want to continue? [y/n]",
		},
		{
			name:     "allow edit prompt",
			output:   "Multiple lines\nof output\nAllow edit file.go? [y/n]",
			expected: "Allow edit file.go? [y/n]",
		},
		{
			name:     "approve prompt",
			output:   "Changes:\n- foo\n- bar\nApprove these changes?",
			expected: "Approve these changes?",
		},
		{
			name:     "no prompt",
			output:   "Just normal output\nnothing special",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPrompt(tt.output)
			if result != tt.expected {
				t.Errorf("extractPrompt() = %q, want %q", result, tt.expected)
			}
		})
	}
}


func TestDetectStatusPriorityOrder(t *testing.T) {
	// Waiting should take priority over error when both patterns present
	output := "Error occurred\nRetry? [y/n]"
	result := detectStatus(output)
	if result != StatusWaiting {
		t.Errorf("waiting should take priority over error, got %v", result)
	}
}

func TestTmuxSessionPrefix(t *testing.T) {
	// Verify the session prefix constant
	if !strings.HasPrefix(tmuxSessionPrefix, "sidecar-") {
		t.Errorf("tmux session prefix should start with 'sidecar-', got %q", tmuxSessionPrefix)
	}
}

func TestShouldShowSkipPermissions(t *testing.T) {
	tests := []struct {
		agentType AgentType
		expected  bool
	}{
		{AgentNone, false},     // No agent, no checkbox
		{AgentClaude, true},    // Has --dangerously-skip-permissions
		{AgentCodex, true},     // Has --dangerously-bypass-approvals-and-sandbox
		{AgentGemini, true},    // Has --yolo
		{AgentCursor, true},    // Has -f flag
		{AgentOpenCode, false}, // No known flag
	}

	p := &Plugin{}
	for _, tt := range tests {
		t.Run(string(tt.agentType), func(t *testing.T) {
			p.createAgentType = tt.agentType
			result := p.shouldShowSkipPermissions()
			if result != tt.expected {
				t.Errorf("shouldShowSkipPermissions(%q) = %v, want %v", tt.agentType, result, tt.expected)
			}
		})
	}
}

func TestBuildAgentCommand(t *testing.T) {
	tests := []struct {
		name      string
		agentType AgentType
		skipPerms bool
		taskID    string
		wantFlag  string   // Expected skip-perms flag in output
		wantPrompt bool    // Whether prompt should be included
	}{
		// Claude tests
		{
			name:       "claude no skip no task",
			agentType:  AgentClaude,
			skipPerms:  false,
			taskID:     "",
			wantFlag:   "",
			wantPrompt: false,
		},
		{
			name:       "claude with skip no task",
			agentType:  AgentClaude,
			skipPerms:  true,
			taskID:     "",
			wantFlag:   "--dangerously-skip-permissions",
			wantPrompt: false,
		},
		// Codex tests
		{
			name:       "codex no skip no task",
			agentType:  AgentCodex,
			skipPerms:  false,
			taskID:     "",
			wantFlag:   "",
			wantPrompt: false,
		},
		{
			name:       "codex with skip no task",
			agentType:  AgentCodex,
			skipPerms:  true,
			taskID:     "",
			wantFlag:   "--dangerously-bypass-approvals-and-sandbox",
			wantPrompt: false,
		},
		// Gemini tests
		{
			name:       "gemini no skip no task",
			agentType:  AgentGemini,
			skipPerms:  false,
			taskID:     "",
			wantFlag:   "",
			wantPrompt: false,
		},
		{
			name:       "gemini with skip no task",
			agentType:  AgentGemini,
			skipPerms:  true,
			taskID:     "",
			wantFlag:   "--yolo",
			wantPrompt: false,
		},
		// Cursor tests
		{
			name:       "cursor no skip no task",
			agentType:  AgentCursor,
			skipPerms:  false,
			taskID:     "",
			wantFlag:   "",
			wantPrompt: false,
		},
		{
			name:       "cursor with skip no task",
			agentType:  AgentCursor,
			skipPerms:  true,
			taskID:     "",
			wantFlag:   "-f",
			wantPrompt: false,
		},
		// OpenCode tests (no skip flag)
		{
			name:       "opencode no skip no task",
			agentType:  AgentOpenCode,
			skipPerms:  false,
			taskID:     "",
			wantFlag:   "",
			wantPrompt: false,
		},
		{
			name:       "opencode with skip no task (no flag available)",
			agentType:  AgentOpenCode,
			skipPerms:  true,
			taskID:     "",
			wantFlag:   "",
			wantPrompt: false,
		},
		// Aider tests
		{
			name:       "aider no skip no task",
			agentType:  AgentAider,
			skipPerms:  false,
			taskID:     "",
			wantFlag:   "",
			wantPrompt: false,
		},
		{
			name:       "aider with skip no task",
			agentType:  AgentAider,
			skipPerms:  true,
			taskID:     "",
			wantFlag:   "--yes",
			wantPrompt: false,
		},
	}

	// Create a minimal plugin (no ctx needed for these tests without taskID)
	p := &Plugin{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wt := &Worktree{TaskID: tt.taskID}
			result := p.buildAgentCommand(tt.agentType, wt, tt.skipPerms)

			// Check base command
			baseCmd := getAgentCommand(tt.agentType)
			if !strings.HasPrefix(result, baseCmd) {
				t.Errorf("command should start with %q, got %q", baseCmd, result)
			}

			// Check skip permissions flag
			if tt.wantFlag != "" {
				if !strings.Contains(result, tt.wantFlag) {
					t.Errorf("command should contain flag %q, got %q", tt.wantFlag, result)
				}
			} else if tt.skipPerms {
				// If skipPerms but no wantFlag, ensure no flag was added
				for agent, flag := range SkipPermissionsFlags {
					if agent == tt.agentType && flag != "" {
						t.Errorf("command should not contain flag for %s when wantFlag is empty", tt.agentType)
					}
				}
			}
		})
	}
}

func TestBuildAgentCommandSyntax(t *testing.T) {
	// Test expected output format for each agent
	tests := []struct {
		agentType AgentType
		skipPerms bool
		expected  string
	}{
		{AgentClaude, false, "claude"},
		{AgentClaude, true, "claude --dangerously-skip-permissions"},
		{AgentCodex, false, "codex"},
		{AgentCodex, true, "codex --dangerously-bypass-approvals-and-sandbox"},
		{AgentGemini, false, "gemini"},
		{AgentGemini, true, "gemini --yolo"},
		{AgentCursor, false, "cursor-agent"},
		{AgentCursor, true, "cursor-agent -f"},
		{AgentOpenCode, false, "opencode"},
		{AgentOpenCode, true, "opencode"}, // No skip flag
		{AgentAider, false, "aider"},
		{AgentAider, true, "aider --yes"},
	}

	p := &Plugin{}
	for _, tt := range tests {
		name := string(tt.agentType)
		if tt.skipPerms {
			name += "_skip"
		}
		t.Run(name, func(t *testing.T) {
			wt := &Worktree{TaskID: ""} // No task context
			result := p.buildAgentCommand(tt.agentType, wt, tt.skipPerms)
			if result != tt.expected {
				t.Errorf("buildAgentCommand(%s, skipPerms=%v) = %q, want %q",
					tt.agentType, tt.skipPerms, result, tt.expected)
			}
		})
	}
}

func TestWriteAgentLauncher(t *testing.T) {
	// Test that launcher scripts are created correctly for complex prompts
	tmpDir := t.TempDir()

	p := &Plugin{}

	tests := []struct {
		name      string
		agentType AgentType
		baseCmd   string
		prompt    string
		wantCmd   string
	}{
		{
			name:      "claude with simple prompt",
			agentType: AgentClaude,
			baseCmd:   "claude",
			prompt:    "Task: fix bug",
			wantCmd:   "bash " + tmpDir + "/.sidecar-start.sh",
		},
		{
			name:      "claude with complex markdown",
			agentType: AgentClaude,
			baseCmd:   "claude",
			prompt:    "Task: implement feature\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\nDon't break the user's code!",
			wantCmd:   "bash " + tmpDir + "/.sidecar-start.sh",
		},
		{
			name:      "aider uses --message flag",
			agentType: AgentAider,
			baseCmd:   "aider --yes",
			prompt:    "Task: fix bug",
			wantCmd:   "bash " + tmpDir + "/.sidecar-start.sh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := p.writeAgentLauncher(tmpDir, tt.agentType, tt.baseCmd, tt.prompt)
			if err != nil {
				t.Fatalf("writeAgentLauncher failed: %v", err)
			}

			if cmd != tt.wantCmd {
				t.Errorf("command = %q, want %q", cmd, tt.wantCmd)
			}

			// Verify prompt file was created with correct content
			promptContent, err := os.ReadFile(tmpDir + "/.sidecar-prompt")
			if err != nil {
				t.Fatalf("failed to read prompt file: %v", err)
			}
			if string(promptContent) != tt.prompt {
				t.Errorf("prompt content = %q, want %q", string(promptContent), tt.prompt)
			}

			// Verify launcher script exists and is executable
			launcherInfo, err := os.Stat(tmpDir + "/.sidecar-start.sh")
			if err != nil {
				t.Fatalf("launcher script not created: %v", err)
			}
			if launcherInfo.Mode()&0100 == 0 {
				t.Error("launcher script is not executable")
			}

			// Cleanup for next test
			os.Remove(tmpDir + "/.sidecar-prompt")
			os.Remove(tmpDir + "/.sidecar-start.sh")
		})
	}
}
