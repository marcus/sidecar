package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/plugin"
)

func TestGetDefaultCreateAgentType_FromConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Plugins.Workspace.DefaultAgentType = string(AgentOpenCode)

	p := &Plugin{
		ctx: &plugin.Context{
			WorkDir: t.TempDir(),
			Config:  cfg,
		},
	}

	if got := p.getDefaultCreateAgentType(); got != AgentOpenCode {
		t.Errorf("getDefaultCreateAgentType() = %q, want %q", got, AgentOpenCode)
	}
}

func TestGetDefaultCreateAgentType_SidecarAgentPrecedence(t *testing.T) {
	workDir := t.TempDir()
	cfg := config.Default()
	cfg.Plugins.Workspace.DefaultAgentType = string(AgentGemini)

	if err := os.WriteFile(filepath.Join(workDir, sidecarAgentFile), []byte(string(AgentCodex)+"\n"), 0644); err != nil {
		t.Fatalf("write .sidecar-agent: %v", err)
	}

	p := &Plugin{
		ctx: &plugin.Context{
			WorkDir: workDir,
			Config:  cfg,
		},
	}

	if got := p.getDefaultCreateAgentType(); got != AgentCodex {
		t.Errorf("getDefaultCreateAgentType() = %q, want %q", got, AgentCodex)
	}
}

func TestGetDefaultCreateAgentType_InvalidFallback(t *testing.T) {
	workDir := t.TempDir()
	cfg := config.Default()
	cfg.Plugins.Workspace.DefaultAgentType = "not-an-agent"

	p := &Plugin{
		ctx: &plugin.Context{
			WorkDir: workDir,
			Config:  cfg,
		},
	}

	if got := p.getDefaultCreateAgentType(); got != AgentClaude {
		t.Errorf("getDefaultCreateAgentType() = %q, want %q", got, AgentClaude)
	}
}

func TestInitCreateModalBase_UsesConfiguredDefaultAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workDir := t.TempDir()
	cfg := config.Default()
	cfg.Plugins.Workspace.DefaultAgentType = string(AgentPi)

	p := New()
	p.ctx = &plugin.Context{
		WorkDir: workDir,
		Config:  cfg,
	}

	p.initCreateModalBase()
	if p.createAgentType != AgentPi {
		t.Errorf("createAgentType = %q, want %q", p.createAgentType, AgentPi)
	}
}

func TestResolveWorktreeAgentType_UsesConfigWhenNoSidecarFile(t *testing.T) {
	workDir := t.TempDir()
	cfg := config.Default()
	cfg.Plugins.Workspace.DefaultAgentType = string(AgentOpenCode)

	p := &Plugin{
		ctx: &plugin.Context{
			WorkDir: workDir,
			Config:  cfg,
		},
	}
	wt := &Worktree{Path: workDir}

	if got := p.resolveWorktreeAgentType(wt); got != AgentOpenCode {
		t.Errorf("resolveWorktreeAgentType() = %q, want %q", got, AgentOpenCode)
	}
}

func TestResolveWorktreeAgentType_SidecarFilePrecedence(t *testing.T) {
	workDir := t.TempDir()
	cfg := config.Default()
	cfg.Plugins.Workspace.DefaultAgentType = string(AgentGemini)

	if err := os.WriteFile(filepath.Join(workDir, sidecarAgentFile), []byte(string(AgentCodex)+"\n"), 0644); err != nil {
		t.Fatalf("write .sidecar-agent: %v", err)
	}

	p := &Plugin{
		ctx: &plugin.Context{
			WorkDir: workDir,
			Config:  cfg,
		},
	}
	wt := &Worktree{Path: workDir}

	if got := p.resolveWorktreeAgentType(wt); got != AgentCodex {
		t.Errorf("resolveWorktreeAgentType() = %q, want %q", got, AgentCodex)
	}
}

func TestResolveWorktreeAgentType_ClaudeFallback(t *testing.T) {
	workDir := t.TempDir()
	cfg := config.Default()
	cfg.Plugins.Workspace.DefaultAgentType = "not-an-agent"

	p := &Plugin{
		ctx: &plugin.Context{
			WorkDir: workDir,
			Config:  cfg,
		},
	}
	wt := &Worktree{Path: workDir}

	if got := p.resolveWorktreeAgentType(wt); got != AgentClaude {
		t.Errorf("resolveWorktreeAgentType() = %q, want %q", got, AgentClaude)
	}
}
