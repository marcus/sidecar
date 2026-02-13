package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupClaudeTestDir creates a temp HOME with Claude project directory structure.
// Uses t.Setenv which auto-restores HOME after the test (parallel-safe).
func setupClaudeTestDir(t *testing.T, worktreePath string) (tmpHome, projectDir string) {
	t.Helper()
	tmpHome = t.TempDir()
	t.Setenv("HOME", tmpHome)

	projectDirName := claudeProjectDirName(worktreePath)
	projectDir = filepath.Join(tmpHome, ".claude", "projects", projectDirName)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}
	return
}

// writeSessionFile creates a session file and optionally sets its mtime to the past.
func writeSessionFile(t *testing.T, dir, name, content string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", name, err)
	}
	if age > 0 {
		old := time.Now().Add(-age)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("failed to set mtime on %s: %v", name, err)
		}
	}
	return path
}

func TestDetectClaudeSessionStatus_MtimeActive(t *testing.T) {
	worktreePath := "/test/project/path"
	_, projectDir := setupClaudeTestDir(t, worktreePath)

	// Session file just written (mtime is now) → should be active
	writeSessionFile(t, projectDir, "test-session.jsonl",
		`{"type":"assistant","message":{"role":"assistant","content":"Done!"}}`, 0)

	status, ok := detectClaudeSessionStatus(worktreePath)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusActive {
		t.Errorf("got %v, want StatusActive (file just written)", status)
	}
}

func TestDetectClaudeSessionStatus_MtimeStaleAssistant(t *testing.T) {
	worktreePath := "/test/project/path"
	_, projectDir := setupClaudeTestDir(t, worktreePath)

	// Session file old + last entry assistant → waiting (JSONL fallback)
	writeSessionFile(t, projectDir, "test-session.jsonl",
		`{"type":"user","message":{"role":"user","content":"hello"}}
{"type":"assistant","message":{"role":"assistant","content":"Done!"}}`,
		2*time.Minute)

	status, ok := detectClaudeSessionStatus(worktreePath)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusWaiting {
		t.Errorf("got %v, want StatusWaiting (stale file, last entry assistant)", status)
	}
}

func TestDetectClaudeSessionStatus_MtimeStaleUser(t *testing.T) {
	worktreePath := "/test/project/path"
	_, projectDir := setupClaudeTestDir(t, worktreePath)

	// Session file old + last entry user → active (agent is thinking, JSONL fallback)
	writeSessionFile(t, projectDir, "test-session.jsonl",
		`{"type":"assistant","message":{"role":"assistant","content":"Hi"}}
{"type":"user","message":{"role":"user","content":"do something"}}`,
		2*time.Minute)

	status, ok := detectClaudeSessionStatus(worktreePath)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusActive {
		t.Errorf("got %v, want StatusActive (stale file, last entry user = thinking)", status)
	}
}

func TestDetectClaudeSessionStatus_SubagentMtime(t *testing.T) {
	worktreePath := "/test/project/path"
	_, projectDir := setupClaudeTestDir(t, worktreePath)

	// Main session is old (stale mtime), last entry is assistant → would be waiting
	writeSessionFile(t, projectDir, "test-session.jsonl",
		`{"type":"assistant","message":{"role":"assistant","content":"Done!"}}`,
		2*time.Minute)

	// But a sub-agent file was just written → should be active
	subagentsDir := filepath.Join(projectDir, "test-session", "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatalf("failed to create subagents dir: %v", err)
	}
	writeSessionFile(t, subagentsDir, "agent-abc123.jsonl",
		`{"type":"progress","data":{"type":"bash_progress","elapsedTimeSeconds":5}}`, 0)

	status, ok := detectClaudeSessionStatus(worktreePath)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusActive {
		t.Errorf("got %v, want StatusActive (sub-agent file recently modified)", status)
	}
}

func TestDetectClaudeSessionStatus_BothStale(t *testing.T) {
	worktreePath := "/test/project/path"
	_, projectDir := setupClaudeTestDir(t, worktreePath)

	// Main session old, last entry assistant
	writeSessionFile(t, projectDir, "test-session.jsonl",
		`{"type":"assistant","message":{"role":"assistant","content":"Done!"}}`,
		2*time.Minute)

	// Sub-agent file also old
	subagentsDir := filepath.Join(projectDir, "test-session", "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatalf("failed to create subagents dir: %v", err)
	}
	writeSessionFile(t, subagentsDir, "agent-abc123.jsonl",
		`{"type":"assistant","message":{"role":"assistant","content":"done"}}`,
		2*time.Minute)

	status, ok := detectClaudeSessionStatus(worktreePath)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusWaiting {
		t.Errorf("got %v, want StatusWaiting (both files stale, last entry assistant)", status)
	}
}

func TestDetectClaudeSessionStatus_StaleWithHookProgress(t *testing.T) {
	worktreePath := "/test/project/path"
	_, projectDir := setupClaudeTestDir(t, worktreePath)

	// Stale file with hook_progress + system entries after assistant → should fall through to assistant → waiting
	writeSessionFile(t, projectDir, "test-session.jsonl",
		`{"type":"user","message":{"role":"user","content":"hello"}}
{"type":"assistant","message":{"role":"assistant","content":"Done!"}}
{"type":"progress","data":{"type":"hook_progress","hookEvent":"Stop"}}
{"type":"system","subtype":"stop_hook_summary"}
{"type":"system","subtype":"turn_duration"}`,
		2*time.Minute)

	status, ok := detectClaudeSessionStatus(worktreePath)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusWaiting {
		t.Errorf("got %v, want StatusWaiting (stale, JSONL scans past system/progress to assistant)", status)
	}
}

func TestDetectClaudeSessionStatus_NoSessionFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	status, ok := detectClaudeSessionStatus("/nonexistent/path")
	if ok {
		t.Errorf("expected ok=false for missing session, got status=%v", status)
	}
}

func TestDetectClaudeSessionStatus_EmptyFile(t *testing.T) {
	worktreePath := "/test/project/path"
	_, projectDir := setupClaudeTestDir(t, worktreePath)

	writeSessionFile(t, projectDir, "test-session.jsonl", "", 2*time.Minute)

	_, ok := detectClaudeSessionStatus(worktreePath)
	if ok {
		t.Error("expected ok=false for empty session file")
	}
}

func TestDetectClaudeSessionStatus_AbandonedSessionSkipped(t *testing.T) {
	worktreePath := "/test/project/path"
	_, projectDir := setupClaudeTestDir(t, worktreePath)

	// Older session with real content (assistant = waiting)
	writeSessionFile(t, projectDir, "real-session.jsonl",
		`{"type":"user","message":{"role":"user","content":"hello"}}
{"type":"assistant","message":{"role":"assistant","content":"Done!"}}`,
		2*time.Minute)

	// Newer abandoned session with only file-history-snapshot (no user/assistant)
	writeSessionFile(t, projectDir, "abandoned-session.jsonl",
		`{"type":"file-history-snapshot","data":{}}
{"type":"file-history-snapshot","data":{}}`,
		1*time.Minute) // More recent than real-session

	status, ok := detectClaudeSessionStatus(worktreePath)
	if !ok {
		t.Fatal("expected ok=true (should skip abandoned, find real session)")
	}
	if status != StatusWaiting {
		t.Errorf("got %v, want StatusWaiting (real session has last entry assistant)", status)
	}
}

func TestFindMostRecentJSONL(t *testing.T) {
	tmpDir := t.TempDir()

	files := []string{"session1.jsonl", "session2.jsonl", "agent-sub.jsonl"}
	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	result, err := findMostRecentJSONL(tmpDir, "agent-")
	if err != nil {
		t.Fatalf("findMostRecentJSONL() error: %v", err)
	}
	if result == "" {
		t.Error("findMostRecentJSONL() returned empty string")
	}
	if filepath.Base(result) == "agent-sub.jsonl" {
		t.Error("findMostRecentJSONL() should skip agent- prefixed files")
	}
	base := filepath.Base(result)
	if base != "session1.jsonl" && base != "session2.jsonl" {
		t.Errorf("findMostRecentJSONL() = %s, want session1.jsonl or session2.jsonl", base)
	}
}

func TestClaudeProjectDirName(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "/Users/foo/project",
			expected: "-Users-foo-project",
		},
		{
			name:     "path with underscores",
			path:     "/Users/foo/zenleap_scratch/sidecar",
			expected: "-Users-foo-zenleap-scratch-sidecar",
		},
		{
			name:     "path with dots",
			path:     "/Users/foo/v1.2.3/project",
			expected: "-Users-foo-v1-2-3-project",
		},
		{
			name:     "path with spaces",
			path:     "/Users/foo/My Projects/app",
			expected: "-Users-foo-My-Projects-app",
		},
		{
			name:     "preserves case and dashes",
			path:     "/Users/foo/My-Project/src",
			expected: "-Users-foo-My-Project-src",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := claudeProjectDirName(tt.path)
			if result != tt.expected {
				t.Errorf("claudeProjectDirName(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestDetectClaudeSessionStatus_UnderscoredPath(t *testing.T) {
	worktreePath := "/test/zenleap_scratch/my_project"
	_, projectDir := setupClaudeTestDir(t, worktreePath)

	// Old file so mtime doesn't trigger, falls through to JSONL
	writeSessionFile(t, projectDir, "test-session.jsonl",
		`{"type":"assistant","message":{"role":"assistant","content":"Done!"}}`,
		2*time.Minute)

	status, ok := detectClaudeSessionStatus(worktreePath)
	if !ok {
		t.Fatal("detectClaudeSessionStatus() returned ok=false, expected ok=true")
	}
	if status != StatusWaiting {
		t.Errorf("detectClaudeSessionStatus() = %v, want StatusWaiting", status)
	}
}

func TestDetectAgentSessionStatus(t *testing.T) {
	status, ok := detectAgentSessionStatus(AgentCustom, "/test/path")
	if ok {
		t.Errorf("expected ok=false for unsupported agent type, got status=%v", status)
	}

	status, ok = detectAgentSessionStatus("", "/test/path")
	if ok {
		t.Errorf("expected ok=false for empty agent type, got status=%v", status)
	}
}

// setupPiTestDir creates a temp HOME with Pi Agent project directory structure.
// Pi stores sessions in ~/.pi/agent/sessions/--{path-encoded}--/
func setupPiTestDir(t *testing.T, worktreePath string) (tmpHome, projectDir string) {
	t.Helper()
	tmpHome = t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Pi encodes paths: strip leading slash, replace slashes with dashes, wrap in --
	path := worktreePath[1:] // strip leading /
	encoded := ""
	for _, c := range path {
		if c == '/' {
			encoded += "-"
		} else {
			encoded += string(c)
		}
	}
	projectDir = filepath.Join(tmpHome, ".pi", "agent", "sessions", "--"+encoded+"--")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}
	return
}

func TestDetectPiSessionStatus_MtimeActive(t *testing.T) {
	worktreePath := "/test/project/path"
	_, projectDir := setupPiTestDir(t, worktreePath)

	// Session file just written (mtime is now) → should be active
	writeSessionFile(t, projectDir, "2026-01-01T00-00-00Z_test-session.jsonl",
		`{"type":"message","message":{"role":"assistant","content":"Done!"}}`, 0)

	status, ok := detectPiSessionStatus(worktreePath)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusActive {
		t.Errorf("got %v, want StatusActive (file just written)", status)
	}
}

func TestDetectPiSessionStatus_MtimeStaleAssistant(t *testing.T) {
	worktreePath := "/test/project/path"
	_, projectDir := setupPiTestDir(t, worktreePath)

	// Session file old + last entry assistant → waiting (JSONL fallback)
	writeSessionFile(t, projectDir, "2026-01-01T00-00-00Z_test-session.jsonl",
		`{"type":"message","message":{"role":"user","content":"hello"}}
{"type":"message","message":{"role":"assistant","content":"Done!"}}`,
		2*time.Minute)

	status, ok := detectPiSessionStatus(worktreePath)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusWaiting {
		t.Errorf("got %v, want StatusWaiting (stale file, last entry assistant)", status)
	}
}

func TestDetectPiSessionStatus_MtimeStaleUser(t *testing.T) {
	worktreePath := "/test/project/path"
	_, projectDir := setupPiTestDir(t, worktreePath)

	// Session file old + last entry user → active (agent is thinking, JSONL fallback)
	writeSessionFile(t, projectDir, "2026-01-01T00-00-00Z_test-session.jsonl",
		`{"type":"message","message":{"role":"assistant","content":"Hi"}}
{"type":"message","message":{"role":"user","content":"do something"}}`,
		2*time.Minute)

	status, ok := detectPiSessionStatus(worktreePath)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusActive {
		t.Errorf("got %v, want StatusActive (stale file, last entry user = thinking)", status)
	}
}

func TestDetectPiSessionStatus_NoSession(t *testing.T) {
	worktreePath := "/test/project/path"
	setupPiTestDir(t, worktreePath)
	// No session file created

	_, ok := detectPiSessionStatus(worktreePath)
	if ok {
		t.Error("expected ok=false when no session files exist")
	}
}

func TestDetectPiSessionStatus_NonexistentPath(t *testing.T) {
	_, ok := detectPiSessionStatus("/nonexistent/path")
	if ok {
		t.Error("expected ok=false for nonexistent path")
	}
}

func TestIsFileRecentlyModified(t *testing.T) {
	tmpDir := t.TempDir()

	// Fresh file
	fresh := filepath.Join(tmpDir, "fresh.jsonl")
	if err := os.WriteFile(fresh, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if !isFileRecentlyModified(fresh, 30*time.Second) {
		t.Error("expected fresh file to be recently modified")
	}

	// Old file
	old := filepath.Join(tmpDir, "old.jsonl")
	if err := os.WriteFile(old, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Minute)
	_ = os.Chtimes(old, oldTime, oldTime)
	if isFileRecentlyModified(old, 30*time.Second) {
		t.Error("expected old file to NOT be recently modified")
	}

	// Nonexistent file
	if isFileRecentlyModified(filepath.Join(tmpDir, "missing.jsonl"), 30*time.Second) {
		t.Error("expected missing file to NOT be recently modified")
	}
}

func TestAnyFileRecentlyModified(t *testing.T) {
	tmpDir := t.TempDir()

	// All old files
	for _, name := range []string{"a.jsonl", "b.jsonl"} {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		oldTime := time.Now().Add(-2 * time.Minute)
		_ = os.Chtimes(path, oldTime, oldTime)
	}
	if anyFileRecentlyModified(tmpDir, ".jsonl", 30*time.Second) {
		t.Error("expected no recently modified files")
	}

	// Add one fresh file
	fresh := filepath.Join(tmpDir, "c.jsonl")
	if err := os.WriteFile(fresh, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if !anyFileRecentlyModified(tmpDir, ".jsonl", 30*time.Second) {
		t.Error("expected at least one recently modified file")
	}

	// Nonexistent directory
	if anyFileRecentlyModified(filepath.Join(tmpDir, "nope"), ".jsonl", 30*time.Second) {
		t.Error("expected false for nonexistent directory")
	}
}

func TestFindCodexSessionForPath_DateNested(t *testing.T) {
	tmpDir := t.TempDir()
	worktreePath := "/test/project"

	// Create date-nested directory: YYYY/MM/DD/rollout-*.jsonl
	dateDir := filepath.Join(tmpDir, "2026", "02", "10")
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a session file with matching CWD
	content := `{"type":"session_meta","payload":{"cwd":"/test/project"}}
{"type":"response_item","payload":{"type":"message","role":"user"}}
{"type":"response_item","payload":{"type":"message","role":"assistant"}}`
	sessionFile := filepath.Join(dateDir, "rollout-2026-02-10T09-00-00-abc123.jsonl")
	if err := os.WriteFile(sessionFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := findCodexSessionForPath(tmpDir, worktreePath)
	if err != nil {
		t.Fatalf("findCodexSessionForPath() error: %v", err)
	}
	if result == "" {
		t.Error("findCodexSessionForPath() should find session in date-nested directory")
	}
	if result != sessionFile {
		t.Errorf("findCodexSessionForPath() = %s, want %s", result, sessionFile)
	}
}

func TestFindCodexSessionForPath_NoMatch(t *testing.T) {
	tmpDir := t.TempDir()

	dateDir := filepath.Join(tmpDir, "2026", "02", "10")
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Session with different CWD
	content := `{"type":"session_meta","payload":{"cwd":"/other/project"}}`
	if err := os.WriteFile(filepath.Join(dateDir, "rollout.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := findCodexSessionForPath(tmpDir, "/test/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for non-matching CWD, got %s", result)
	}
}

func TestDetectCodexSessionStatus_MtimeActive(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Create Codex session directory with date hierarchy
	sessionsDir := filepath.Join(tmpDir, ".codex", "sessions", "2026", "02", "10")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Fresh session file (mtime is now) → active via mtime fast path
	content := `{"type":"session_meta","payload":{"cwd":"/test/project"}}
{"type":"response_item","payload":{"type":"message","role":"assistant"}}`
	writeSessionFile(t, sessionsDir, "rollout-test.jsonl", content, 0)

	status, ok := detectCodexSessionStatus("/test/project")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusActive {
		t.Errorf("got %v, want StatusActive (fresh mtime)", status)
	}
}

func TestDetectCodexSessionStatus_StaleAssistant(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	sessionsDir := filepath.Join(tmpDir, ".codex", "sessions", "2026", "02", "10")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Old session file, last message from assistant → waiting via JSONL fallback
	content := `{"type":"session_meta","payload":{"cwd":"/test/project"}}
{"type":"response_item","payload":{"type":"message","role":"user"}}
{"type":"response_item","payload":{"type":"message","role":"assistant"}}`
	writeSessionFile(t, sessionsDir, "rollout-test.jsonl", content, 2*time.Minute)

	status, ok := detectCodexSessionStatus("/test/project")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusWaiting {
		t.Errorf("got %v, want StatusWaiting (stale mtime, last entry assistant)", status)
	}
}

func TestDetectCodexSessionStatus_StaleUser(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	sessionsDir := filepath.Join(tmpDir, ".codex", "sessions", "2026", "02", "10")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Old session file, last message from user → active (agent thinking) via JSONL fallback
	content := `{"type":"session_meta","payload":{"cwd":"/test/project"}}
{"type":"response_item","payload":{"type":"message","role":"assistant"}}
{"type":"response_item","payload":{"type":"message","role":"user"}}`
	writeSessionFile(t, sessionsDir, "rollout-test.jsonl", content, 2*time.Minute)

	status, ok := detectCodexSessionStatus("/test/project")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if status != StatusActive {
		t.Errorf("got %v, want StatusActive (stale mtime, last entry user = thinking)", status)
	}
}
