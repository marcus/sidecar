package tdmonitor

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/tdroot"
)

func TestNew(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("expected non-nil plugin")
	}
}

func TestPluginID(t *testing.T) {
	p := New()
	if id := p.ID(); id != "td-monitor" {
		t.Errorf("expected ID 'td-monitor', got %q", id)
	}
}

func TestPluginName(t *testing.T) {
	p := New()
	if name := p.Name(); name != "td" {
		t.Errorf("expected Name 'td', got %q", name)
	}
}

func TestPluginIcon(t *testing.T) {
	p := New()
	if icon := p.Icon(); icon != "T" {
		t.Errorf("expected Icon 'T', got %q", icon)
	}
}

func TestFocusContext(t *testing.T) {
	p := New()

	// Without model, should return default
	if ctx := p.FocusContext(); ctx != "td-monitor" {
		t.Errorf("expected context 'td-monitor', got %q", ctx)
	}
}

func TestDiagnosticsNoDatabase(t *testing.T) {
	p := New()
	diags := p.Diagnostics()

	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}

	if diags[0].Status != "disabled" {
		t.Errorf("expected status 'disabled', got %q", diags[0].Status)
	}
}

func TestFormatCount(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{1, "1 issue"},
		{5, "5 issues"},
		{10, "10 issues"},
		{100, "100 issues"},
	}

	for _, tt := range tests {
		result := formatCount(tt.count, "issue", "issues")
		if result != tt.expected {
			t.Errorf("formatCount(%d) = %q, expected %q",
				tt.count, result, tt.expected)
		}
	}
}

func TestInitWithNonExistentDatabase(t *testing.T) {
	p := New()
	ctx := &plugin.Context{
		WorkDir: "/nonexistent/path",
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	// Init should NOT return an error even if database doesn't exist
	// This is silent degradation - plugin loads but shows "no database"
	err := p.Init(ctx)
	if err != nil {
		t.Errorf("Init should not return error for missing database, got: %v", err)
	}

	// Plugin should still be usable but model should be nil
	if p.ctx == nil {
		t.Error("context should be set")
	}
	if p.model != nil {
		t.Error("model should be nil when database not found")
	}
}

// findProjectRootWithDB walks up from cwd to find a directory whose resolved
// td database path (following .td-root) actually exists. Returns the clean
// project root or calls t.Skip if no usable database is found.
func findProjectRootWithDB(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Skip("couldn't get working directory")
	}

	// Walk up to find a directory with .todos/issues.db
	projectRoot := cwd
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(projectRoot + "/.todos/issues.db"); err == nil {
			break
		}
		projectRoot = projectRoot + "/.."
	}
	projectRoot = filepath.Clean(projectRoot)

	// Verify the *resolved* database path exists. The monitor follows .td-root
	// which may redirect to a different directory (e.g., a worktree root on
	// another machine). Skip if the resolved path is unreachable.
	resolvedDBPath := tdroot.ResolveDBPath(projectRoot)
	if _, err := os.Stat(resolvedDBPath); err != nil {
		t.Skipf("resolved td database not accessible: %s", resolvedDBPath)
	}

	return projectRoot
}

func TestInitWithValidDatabase(t *testing.T) {
	projectRoot := findProjectRootWithDB(t)

	p := New()
	ctx := &plugin.Context{
		WorkDir: projectRoot,
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	err := p.Init(ctx)
	if err != nil {
		t.Errorf("Init failed: %v", err)
	}

	// Check if model was created
	if p.model == nil {
		t.Error("model should be created when database exists")
	}

	// Cleanup
	p.Stop()
}

func TestDiagnosticsWithDatabase(t *testing.T) {
	projectRoot := findProjectRootWithDB(t)

	p := New()
	ctx := &plugin.Context{
		WorkDir: projectRoot,
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
	_ = p.Init(ctx)
	defer p.Stop()

	diags := p.Diagnostics()
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}

	// With database, status should be "ok"
	if diags[0].Status != "ok" {
		t.Errorf("expected status 'ok' with database, got %q", diags[0].Status)
	}
}

func TestNotInstalledModel(t *testing.T) {
	m := NewNotInstalledModel()
	if m == nil {
		t.Fatal("expected non-nil model")
	}

	// Test View renders content
	result := m.View(80, 24)
	if result == "" {
		t.Error("expected non-empty view")
	}

	// Check it contains expected content
	if !strings.Contains(result, "External memory") {
		t.Error("expected view to contain pitch text")
	}
}

func TestCommands(t *testing.T) {
	p := New()

	// Without model, should return nil
	cmds := p.Commands()
	if cmds != nil {
		t.Errorf("expected nil commands without model, got %d", len(cmds))
	}
}

func TestStartWithoutModel(t *testing.T) {
	p := New()

	// Start without model should return nil
	cmd := p.Start()
	if cmd != nil {
		t.Error("expected nil command without model")
	}
}

func TestViewWithoutModel(t *testing.T) {
	p := New()

	// View without model should show "no database" message
	view := p.View(80, 24)
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestInitWithTodosFileConflict(t *testing.T) {
	// Create temp directory with .todos as a regular FILE (not directory)
	tmpDir, err := os.MkdirTemp("", "tdmonitor-conflict-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	todosFile := filepath.Join(tmpDir, ".todos")
	if err := os.WriteFile(todosFile, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("failed to write .todos file: %v", err)
	}

	p := New()
	ctx := &plugin.Context{
		WorkDir: tmpDir,
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	// Init should not return an error (silent degradation)
	if err := p.Init(ctx); err != nil {
		t.Errorf("Init should not return error, got: %v", err)
	}

	// Plugin should detect the conflict
	if !p.todosConflict {
		t.Error("expected todosConflict to be true when .todos is a file")
	}

	// Model should be nil (no monitor created)
	if p.model != nil {
		t.Error("model should be nil when .todos is a file")
	}

	// Setup modal should NOT be shown (the conflict takes priority)
	if p.setupModal != nil {
		t.Error("setupModal should be nil when .todos is a file")
	}

	// View should contain the conflict error message
	view := p.View(80, 24)
	if !strings.Contains(view, "file where a directory is expected") {
		t.Errorf("expected conflict error in view, got: %s", view)
	}

	// Diagnostics should report the error
	diags := p.Diagnostics()
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if diags[0].Status != "error" {
		t.Errorf("expected diagnostic status 'error', got %q", diags[0].Status)
	}
	if !strings.Contains(diags[0].Detail, "file, not a directory") {
		t.Errorf("expected diagnostic detail about file conflict, got %q", diags[0].Detail)
	}
}
