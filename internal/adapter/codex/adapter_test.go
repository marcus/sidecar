package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetect(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}

	path := filepath.Join(sessionsDir, "2025", "11", "20")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	lines := []string{
		`{"timestamp":"2025-11-21T04:13:55.791Z","type":"session_meta","payload":{"id":"id-1","timestamp":"2025-11-21T04:13:55.777Z","cwd":"` + projectDir + `"}}`,
	}
	if err := writeSessionFile(filepath.Join(path, "rollout-1.jsonl"), lines); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	a := New()
	a.sessionsDir = sessionsDir

	found, err := a.Detect(projectDir)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !found {
		t.Fatalf("Detect() = false, want true")
	}
}

func TestSessionsOrdering(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}

	first := filepath.Join(sessionsDir, "2025", "11", "20")
	second := filepath.Join(sessionsDir, "2025", "11", "21")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	linesA := []string{
		`{"timestamp":"2025-11-20T04:13:55.791Z","type":"session_meta","payload":{"id":"id-a","timestamp":"2025-11-20T04:13:55.777Z","cwd":"` + projectDir + `"}}`,
		`{"timestamp":"2025-11-20T04:15:16.710Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}`,
	}
	linesB := []string{
		`{"timestamp":"2025-11-21T04:13:55.791Z","type":"session_meta","payload":{"id":"id-b","timestamp":"2025-11-21T04:13:55.777Z","cwd":"` + projectDir + `"}}`,
		`{"timestamp":"2025-11-21T04:16:16.710Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"later"}]}}`,
	}
	if err := writeSessionFile(filepath.Join(first, "rollout-a.jsonl"), linesA); err != nil {
		t.Fatalf("write session A: %v", err)
	}
	if err := writeSessionFile(filepath.Join(second, "rollout-b.jsonl"), linesB); err != nil {
		t.Fatalf("write session B: %v", err)
	}

	a := New()
	a.sessionsDir = sessionsDir

	sessions, err := a.Sessions(projectDir)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("Sessions() = %d, want 2", len(sessions))
	}
	if sessions[0].ID != "id-b" {
		t.Fatalf("Sessions()[0].ID = %q, want id-b", sessions[0].ID)
	}
}

func TestSessionsRelativePath(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}

	path := filepath.Join(sessionsDir, "2025", "11", "20")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	lines := []string{
		`{"timestamp":"2025-11-21T04:13:55.791Z","type":"session_meta","payload":{"id":"id-1","timestamp":"2025-11-21T04:13:55.777Z","cwd":"` + projectDir + `"}}`,
		`{"timestamp":"2025-11-21T04:15:16.710Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}`,
		`{"timestamp":"2025-11-21T04:15:17.710Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":5,"cached_input_tokens":1,"output_tokens":2,"reasoning_output_tokens":0,"total_tokens":7}}}}`,
	}
	if err := writeSessionFile(filepath.Join(path, "rollout-1.jsonl"), lines); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	a := New()
	a.sessionsDir = sessionsDir

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	sessionsRel, errRel := a.Sessions(".")
	sessionsAbs, errAbs := a.Sessions(projectDir)
	if errRel != nil || errAbs != nil {
		t.Fatalf("Sessions errors: %v %v", errRel, errAbs)
	}
	if len(sessionsRel) != len(sessionsAbs) {
		t.Fatalf("Sessions length mismatch: %d vs %d", len(sessionsRel), len(sessionsAbs))
	}
	if len(sessionsRel) == 0 {
		t.Fatalf("Sessions returned empty")
	}
	if sessionsRel[0].TotalTokens != 7 {
		t.Fatalf("TotalTokens = %d, want 7", sessionsRel[0].TotalTokens)
	}
}

func writeSessionFile(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for i, line := range lines {
		if i > 0 {
			if _, err := f.WriteString("\n"); err != nil {
				return err
			}
		}
		if _, err := f.WriteString(line); err != nil {
			return err
		}
	}
	if _, err := f.WriteString("\n"); err != nil {
		return err
	}
	return nil
}

func TestShortID(t *testing.T) {
	if got := shortID("abcdefghi"); got != "abcdefgh" {
		t.Fatalf("shortID = %q, want abcdefgh", got)
	}
	if got := shortID("short"); got != "short" {
		t.Fatalf("shortID = %q, want short", got)
	}
}

func TestCwdMatchesProject(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	child := filepath.Join(project, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	if !cwdMatchesProject(project, child) {
		t.Fatalf("cwdMatchesProject should match child")
	}
	if cwdMatchesProject(project, filepath.Join(root, "other")) {
		t.Fatalf("cwdMatchesProject should not match other")
	}
	if !cwdMatchesProject(project, project) {
		t.Fatalf("cwdMatchesProject should match same dir")
	}
	if cwdMatchesProject("", project) {
		t.Fatalf("cwdMatchesProject should reject empty project")
	}
	if cwdMatchesProject(project, "") {
		t.Fatalf("cwdMatchesProject should reject empty cwd")
	}
	if !cwdMatchesProject(project, filepath.Clean(project+string(os.PathSeparator))) {
		t.Fatalf("cwdMatchesProject should handle trailing separator")
	}
}

func TestParseSessionMetadataFallbacks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rollout.jsonl")
	lines := []string{
		`{"timestamp":"2025-11-21T04:13:55.791Z","type":"session_meta","payload":{"id":"id-1","timestamp":"2025-11-21T04:13:55.777Z","cwd":"/tmp/project"}}`,
	}
	if err := writeSessionFile(path, lines); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	a := New()
	meta, err := a.parseSessionMetadata(path)
	if err != nil {
		t.Fatalf("parseSessionMetadata: %v", err)
	}
	if meta.SessionID != "id-1" {
		t.Fatalf("SessionID = %q, want id-1", meta.SessionID)
	}
	if meta.FirstMsg.IsZero() || meta.LastMsg.IsZero() {
		t.Fatalf("expected timestamps to be set")
	}
	if !meta.FirstMsg.Equal(meta.LastMsg) {
		t.Fatalf("expected FirstMsg == LastMsg for metadata-only session")
	}
	if meta.MsgCount != 0 {
		t.Fatalf("MsgCount = %d, want 0", meta.MsgCount)
	}
	if meta.TotalTokens != 0 {
		t.Fatalf("TotalTokens = %d, want 0", meta.TotalTokens)
	}
	if meta.LastMsg.Before(time.Date(2025, 11, 21, 4, 13, 55, 0, time.UTC)) {
		t.Fatalf("expected LastMsg >= session_meta timestamp")
	}
}

func TestSessionMetadataTailOnlyOnGrowth(t *testing.T) {
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a session file larger than metaParseSmallFileThreshold (16KB)
	// by padding the initial session_meta with data
	sessionPath := filepath.Join(sessionsDir, "grow-test.jsonl")

	// Build a file > 16KB with session_meta + messages
	f, err := os.Create(sessionPath)
	if err != nil {
		t.Fatal(err)
	}

	// Write session_meta
	f.WriteString(`{"type":"session_meta","timestamp":"2024-01-01T10:00:00Z","payload":{"id":"grow-test","cwd":"/tmp"}}` + "\n")

	// Write a user message
	f.WriteString(`{"type":"response_item","timestamp":"2024-01-01T10:00:01Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello world"}]}}` + "\n")

	// Write an assistant message
	f.WriteString(`{"type":"response_item","timestamp":"2024-01-01T10:00:02Z","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi there"}]}}` + "\n")

	// Pad to exceed 16KB threshold
	padding := make([]byte, 17*1024)
	for i := range padding {
		padding[i] = ' '
	}
	padding[len(padding)-1] = '\n'
	f.Write(padding)

	// Write a token count event (tail data)
	f.WriteString(`{"type":"event_msg","timestamp":"2024-01-01T10:01:00Z","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":50,"total_tokens":150}}}}` + "\n")
	f.Close()

	a := &Adapter{
		sessionsDir: root,
		metaCache:   make(map[string]sessionMetaCacheEntry),
	}

	// First parse: full two-pass, should set headParsed=true
	info1, _ := os.Stat(sessionPath)
	meta1, err := a.sessionMetadata(sessionPath, info1)
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	if meta1.SessionID != "grow-test" {
		t.Errorf("expected SessionID grow-test, got %s", meta1.SessionID)
	}
	// Two-pass skips middle; head stops after finding all head data (after user msg)
	if meta1.MsgCount < 1 {
		t.Errorf("expected at least 1 msg, got %d", meta1.MsgCount)
	}

	// Verify cache has headParsed set
	a.metaMu.RLock()
	entry := a.metaCache[sessionPath]
	a.metaMu.RUnlock()
	if !entry.headParsed {
		t.Error("expected headParsed=true for large file")
	}

	// Append new token usage event (simulating file growth)
	f2, _ := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0o644)
	f2.WriteString(`{"type":"event_msg","timestamp":"2024-01-01T10:02:00Z","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":200,"output_tokens":100,"total_tokens":300}}}}` + "\n")
	f2.Close()

	// Second parse: should use tail-only path
	info2, _ := os.Stat(sessionPath)
	if info2.Size() <= info1.Size() {
		t.Fatal("file didn't grow")
	}
	meta2, err := a.sessionMetadata(sessionPath, info2)
	if err != nil {
		t.Fatalf("tail-only parse: %v", err)
	}

	// Head fields preserved
	if meta2.SessionID != "grow-test" {
		t.Errorf("SessionID not preserved: %s", meta2.SessionID)
	}
	if meta2.CWD != "/tmp" {
		t.Errorf("CWD not preserved: %s", meta2.CWD)
	}
	if meta2.TotalTokens != 300 {
		t.Errorf("expected 300 tokens from tail update, got %d", meta2.TotalTokens)
	}
}
