package workspace

import (
	"testing"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/plugin"
)

func TestFilteredAgentOrder(t *testing.T) {
	tests := []struct {
		name         string
		filter       []string
		wantLen      int
		wantContains []AgentType
		wantMissing  []AgentType
	}{
		{
			name:         "empty filter returns all",
			filter:       nil,
			wantLen:      len(AgentTypeOrder),
			wantContains: []AgentType{AgentClaude, AgentCodex, AgentNone},
		},
		{
			name:         "single agent plus none",
			filter:       []string{"claude"},
			wantLen:      2,
			wantContains: []AgentType{AgentClaude, AgentNone},
			wantMissing:  []AgentType{AgentCodex, AgentCopilot},
		},
		{
			name:         "multiple agents plus none",
			filter:       []string{"claude", "codex", "copilot"},
			wantLen:      4,
			wantContains: []AgentType{AgentClaude, AgentCodex, AgentCopilot, AgentNone},
			wantMissing:  []AgentType{AgentGemini, AgentCursor},
		},
		{
			name:         "invalid agent IDs ignored",
			filter:       []string{"claude", "invalid", "notreal"},
			wantLen:      2,
			wantContains: []AgentType{AgentClaude, AgentNone},
		},
		{
			name:         "none in filter is ignored (always added at end)",
			filter:       []string{"claude", ""},
			wantLen:      2,
			wantContains: []AgentType{AgentClaude, AgentNone},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{
				ctx: &plugin.Context{
					Config: &config.Config{
						Plugins: config.PluginsConfig{
							Workspace: config.WorkspacePluginConfig{
								AgentFilter: tt.filter,
							},
						},
					},
				},
			}

			got := p.filteredAgentOrder()

			if len(got) != tt.wantLen {
				t.Errorf("filteredAgentOrder() len = %d, want %d", len(got), tt.wantLen)
			}

			for _, want := range tt.wantContains {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("filteredAgentOrder() missing %q", want)
				}
			}

			for _, notWant := range tt.wantMissing {
				for _, g := range got {
					if g == notWant {
						t.Errorf("filteredAgentOrder() should not contain %q", notWant)
					}
				}
			}

			// None should always be last
			if len(got) > 0 && got[len(got)-1] != AgentNone {
				t.Errorf("filteredAgentOrder() last element = %q, want AgentNone", got[len(got)-1])
			}
		})
	}
}

func TestFilteredShellAgentOrder(t *testing.T) {
	tests := []struct {
		name         string
		filter       []string
		wantLen      int
		wantContains []AgentType
		wantMissing  []AgentType
	}{
		{
			name:         "empty filter returns all",
			filter:       nil,
			wantLen:      len(ShellAgentOrder),
			wantContains: []AgentType{AgentNone, AgentClaude, AgentCodex},
		},
		{
			name:         "filtered list has none first",
			filter:       []string{"claude", "codex"},
			wantLen:      3,
			wantContains: []AgentType{AgentNone, AgentClaude, AgentCodex},
			wantMissing:  []AgentType{AgentGemini, AgentCopilot},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{
				ctx: &plugin.Context{
					Config: &config.Config{
						Plugins: config.PluginsConfig{
							Workspace: config.WorkspacePluginConfig{
								AgentFilter: tt.filter,
							},
						},
					},
				},
			}

			got := p.filteredShellAgentOrder()

			if len(got) != tt.wantLen {
				t.Errorf("filteredShellAgentOrder() len = %d, want %d", len(got), tt.wantLen)
			}

			for _, want := range tt.wantContains {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("filteredShellAgentOrder() missing %q", want)
				}
			}

			for _, notWant := range tt.wantMissing {
				for _, g := range got {
					if g == notWant {
						t.Errorf("filteredShellAgentOrder() should not contain %q", notWant)
					}
				}
			}

			// None should always be first for shells
			if len(got) > 0 && got[0] != AgentNone {
				t.Errorf("filteredShellAgentOrder() first element = %q, want AgentNone", got[0])
			}
		})
	}
}

func TestFilteredAgentOrder_NilContext(t *testing.T) {
	p := &Plugin{ctx: nil}
	got := p.filteredAgentOrder()
	if len(got) != len(AgentTypeOrder) {
		t.Errorf("with nil ctx, should return full AgentTypeOrder")
	}
}

func TestFilteredAgentOrder_NilConfig(t *testing.T) {
	p := &Plugin{ctx: &plugin.Context{Config: nil}}
	got := p.filteredAgentOrder()
	if len(got) != len(AgentTypeOrder) {
		t.Errorf("with nil config, should return full AgentTypeOrder")
	}
}
