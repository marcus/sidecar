package gitstatus

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/plugin"
)

func TestInit_NoRepoKeepsPluginAvailable(t *testing.T) {
	tmp := t.TempDir()

	p := New()
	err := p.Init(&plugin.Context{WorkDir: tmp})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if p.hasRepo {
		t.Fatalf("hasRepo = true, want false")
	}
	if p.tree == nil {
		t.Fatalf("tree is nil, want non-nil")
	}
	if got := p.FocusContext(); got != "git-no-repo" {
		t.Fatalf("FocusContext() = %q, want %q", got, "git-no-repo")
	}
	if cmd := p.Start(); cmd != nil {
		t.Fatalf("Start() should return nil in no-repo mode")
	}
}

func TestInit_SwitchRepoToNoRepoClearsRepoState(t *testing.T) {
	repoDir := t.TempDir()
	initCmd := exec.Command("git", "init")
	initCmd.Dir = repoDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	p := New()
	if err := p.Init(&plugin.Context{WorkDir: repoDir}); err != nil {
		t.Fatalf("Init(repo) error = %v", err)
	}
	if !p.hasRepo {
		t.Fatalf("hasRepo = false after repo init")
	}
	if p.repoRoot == "" {
		t.Fatalf("repoRoot is empty after repo init")
	}

	noRepoDir := t.TempDir()
	if err := p.Init(&plugin.Context{WorkDir: noRepoDir}); err != nil {
		t.Fatalf("Init(no-repo) error = %v", err)
	}
	if p.hasRepo {
		t.Fatalf("hasRepo = true after switching to no-repo dir")
	}
	if p.repoRoot != "" {
		t.Fatalf("repoRoot = %q, want empty", p.repoRoot)
	}
}

func TestEnsureGitignoreEntries_AddAndIdempotent(t *testing.T) {
	tmp := t.TempDir()
	gitignore := filepath.Join(tmp, ".gitignore")

	if err := os.WriteFile(gitignore, []byte("node_modules/\n"), 0644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	if err := ensureGitignoreEntries(tmp, sidecarGitignoreEntries); err != nil {
		t.Fatalf("ensureGitignoreEntries() first call error = %v", err)
	}
	if err := ensureGitignoreEntries(tmp, sidecarGitignoreEntries); err != nil {
		t.Fatalf("ensureGitignoreEntries() second call error = %v", err)
	}

	data, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)
	for _, entry := range sidecarGitignoreEntries {
		if strings.Count(content, entry) != 1 {
			t.Fatalf("%q count = %d, want 1\ncontent:\n%s", entry, strings.Count(content, entry), content)
		}
	}
}

func TestEnsureGitignoreEntries_AllSidecarEntries(t *testing.T) {
	tmp := t.TempDir()

	// Verify all expected sidecar state paths are covered
	expected := []string{
		".todos/",
		".sidecar/",
		".sidecar-agent",
		".sidecar-task",
		".sidecar-pr",
		".sidecar-start.sh",
		".sidecar-base",
		".td-root",
	}
	for _, e := range expected {
		found := false
		for _, s := range sidecarGitignoreEntries {
			if s == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("sidecarGitignoreEntries missing expected entry %q", e)
		}
	}

	// Ensure entries are applied cleanly to an empty .gitignore
	if err := ensureGitignoreEntries(tmp, sidecarGitignoreEntries); err != nil {
		t.Fatalf("ensureGitignoreEntries() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)
	for _, entry := range expected {
		if !strings.Contains(content, entry) {
			t.Errorf(".gitignore missing entry %q\ncontent:\n%s", entry, content)
		}
	}
}

func TestStart_EnsuresGitignoreForExistingRepo(t *testing.T) {
	repoDir := t.TempDir()
	initCmd := exec.Command("git", "init")
	initCmd.Dir = repoDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	// Write a .gitignore that deliberately omits sidecar entries
	gitignore := filepath.Join(repoDir, ".gitignore")
	if err := os.WriteFile(gitignore, []byte("node_modules/\n"), 0644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	p := New()
	if err := p.Init(&plugin.Context{WorkDir: repoDir}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	// Call Start() synchronously — we only care about the side-effect (gitignore update)
	_ = p.Start()

	data, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)
	for _, entry := range sidecarGitignoreEntries {
		if !strings.Contains(content, entry) {
			t.Errorf("after Start(), .gitignore missing entry %q\ncontent:\n%s", entry, content)
		}
	}
}

func TestUpdateNoRepo_InitKeyStartsInit(t *testing.T) {
	tmp := t.TempDir()
	p := New()
	if err := p.Init(&plugin.Context{WorkDir: tmp}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	updatedPlugin, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	updated, ok := updatedPlugin.(*Plugin)
	if !ok {
		t.Fatalf("updated plugin type = %T, want *Plugin", updatedPlugin)
	}
	if !updated.repoInitInProgress {
		t.Fatalf("repoInitInProgress = false, want true")
	}
	if cmd == nil {
		t.Fatalf("expected init command, got nil")
	}
}
