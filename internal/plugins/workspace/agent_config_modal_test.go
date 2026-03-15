package workspace

import "testing"

func TestGetAgentConfigPrompt(t *testing.T) {
	tests := []struct {
		name     string
		prompts  []Prompt
		idx      int
		wantNil  bool
		wantName string
	}{
		{"negative index", []Prompt{{Name: "a"}}, -1, true, ""},
		{"out of bounds", []Prompt{{Name: "a"}}, 5, true, ""},
		{"nil prompts", nil, 0, true, ""},
		{"valid index", []Prompt{{Name: "first"}, {Name: "second"}}, 1, false, "second"},
		{"first index", []Prompt{{Name: "only"}}, 0, false, "only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{
				agentConfigPrompts:   tt.prompts,
				agentConfigPromptIdx: tt.idx,
			}
			got := p.getAgentConfigPrompt()
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil prompt")
			}
			if got.Name != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, got.Name)
			}
		})
	}
}

func TestClearAgentConfigModal(t *testing.T) {
	p := &Plugin{
		agentConfigWorktree:  &Worktree{Name: "test"},
		agentConfigIsRestart: true,
		agentConfigAgentType: AgentClaude,
		agentConfigAgentIdx:  3,
		agentConfigSkipPerms: true,
		agentConfigPromptIdx: 2,
		agentConfigPrompts:   []Prompt{{Name: "x"}},
	}
	p.clearAgentConfigModal()

	if p.agentConfigWorktree != nil {
		t.Error("worktree not cleared")
	}
	if p.agentConfigIsRestart {
		t.Error("isRestart not cleared")
	}
	if p.agentConfigAgentType != "" {
		t.Error("agentType not cleared")
	}
	if p.agentConfigAgentIdx != 0 {
		t.Error("agentIdx not cleared")
	}
	if p.agentConfigSkipPerms {
		t.Error("skipPerms not cleared")
	}
	if p.agentConfigPromptIdx != -1 {
		t.Error("promptIdx not cleared")
	}
	if p.agentConfigPrompts != nil {
		t.Error("prompts not cleared")
	}
	if p.agentConfigModal != nil {
		t.Error("modal not cleared")
	}
	if p.agentConfigModalWidth != 0 {
		t.Error("modalWidth not cleared")
	}
	if p.agentConfigFocusSet {
		t.Error("focusSet not cleared")
	}
}

func TestShouldShowAgentConfigSkipPerms(t *testing.T) {
	tests := []struct {
		name      string
		agentType AgentType
		want      bool
	}{
		{"claude has flag", AgentClaude, true},
		{"codex has flag", AgentCodex, true},
		{"none has no flag", AgentNone, false},
		{"opencode has no flag", AgentOpenCode, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{agentConfigAgentType: tt.agentType}
			if got := p.shouldShowAgentConfigSkipPerms(); got != tt.want {
				t.Errorf("shouldShowAgentConfigSkipPerms() = %v, want %v", got, tt.want)
			}
		})
	}
}
