package worktree

import (
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
		{AgentCursor, false},   // No known flag
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
