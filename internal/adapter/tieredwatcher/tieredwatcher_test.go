package tieredwatcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, ch, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()

	if ch == nil {
		t.Fatal("events channel is nil")
	}
}

func TestRegisterSession(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test session file
	sessionPath := filepath.Join(tmpDir, "test-session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()
	tw.SetHotTarget(3)

	tw.RegisterSession("test-session", sessionPath)

	tw.mu.Lock()
	info, ok := tw.sessions["test-session"]
	tw.mu.Unlock()

	if !ok {
		t.Fatal("session not registered")
	}
	if info.Path != sessionPath {
		t.Errorf("session path = %q, want %q", info.Path, sessionPath)
	}
}

func TestPromoteToHot(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test session files
	for i := 0; i < 5; i++ {
		path := filepath.Join(tmpDir, "session-"+string('a'+byte(i))+".jsonl")
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
	}

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()

	// Register sessions
	for i := 0; i < 5; i++ {
		id := "session-" + string('a'+byte(i))
		path := filepath.Join(tmpDir, id+".jsonl")
		tw.RegisterSession(id, path)
	}

	// Promote more than the hot target
	tw.PromoteToHot("session-a")
	tw.PromoteToHot("session-b")
	tw.PromoteToHot("session-c")
	tw.PromoteToHot("session-d") // This should demote the oldest

	tw.mu.Lock()
	hotCount := len(tw.hotIDs)
	tw.mu.Unlock()

	if hotCount > 3 {
		t.Errorf("hot sessions = %d, want <= 3", hotCount)
	}
}

func TestStats(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()
	tw.SetHotTarget(2)

	// Create and register sessions
	for i := 0; i < 5; i++ {
		id := "session-" + string('a'+byte(i))
		path := filepath.Join(tmpDir, id+".jsonl")
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
		tw.RegisterSession(id, path)
	}

	// Promote some to HOT
	tw.PromoteToHot("session-a")
	tw.PromoteToHot("session-b")

	hot, cold, _, dirs := tw.Stats()

	if hot != 2 {
		t.Errorf("hot = %d, want 2", hot)
	}
	if cold != 3 {
		t.Errorf("cold = %d, want 3", cold)
	}
	if dirs < 1 {
		t.Errorf("watchedDirs = %d, want >= 1", dirs)
	}
}

func TestManager(t *testing.T) {
	tmpDir := t.TempDir()

	manager := NewManager()
	defer func() { _ = manager.Close() }()

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, ch, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	manager.AddWatcher("test-adapter", tw, ch)

	// Create and register a session
	sessionPath := filepath.Join(tmpDir, "test-session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	manager.RegisterSession("test-adapter", "test-session", sessionPath)

	hot, cold, _, _ := manager.Stats()
	if hot+cold != 1 {
		t.Errorf("total sessions = %d, want 1", hot+cold)
	}
}

func TestManagerPromoteSession(t *testing.T) {
	tmpDir := t.TempDir()

	manager := NewManager()
	defer func() { _ = manager.Close() }()

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, ch, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	manager.AddWatcher("test-adapter", tw, ch)

	// Create and register sessions
	for i := 0; i < 3; i++ {
		id := "session-" + string('a'+byte(i))
		path := filepath.Join(tmpDir, id+".jsonl")
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
		tw.RegisterSession(id, path)
	}

	// Promote a session through the manager
	manager.PromoteSession("test-adapter", "session-a")

	tw.mu.Lock()
	found := false
	for _, id := range tw.hotIDs {
		if id == "session-a" {
			found = true
			break
		}
	}
	tw.mu.Unlock()

	if !found {
		t.Error("session-a not found in HOT tier after promotion")
	}
}

func TestRegisterSessions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test session files with different modification times
	sessions := []SessionInfo{}
	for i := 0; i < 5; i++ {
		id := "session-" + string('a'+byte(i))
		path := filepath.Join(tmpDir, id+".jsonl")
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
		// Set modification times to be different
		modTime := time.Now().Add(-time.Duration(5-i) * time.Hour)
		_ = os.Chtimes(path, modTime, modTime)

		stat, _ := os.Stat(path)
		sessions = append(sessions, SessionInfo{
			ID:      id,
			Path:    path,
			ModTime: stat.ModTime(),
		})
	}

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()

	// Register all sessions at once
	tw.RegisterSessions(sessions)
	tw.SetHotTarget(3)

	hot, cold, _, _ := tw.Stats()

	// Should promote most recent sessions to HOT
	if hot != 3 {
		t.Errorf("hot = %d, want 3", hot)
	}
	if cold != 2 {
		t.Errorf("cold = %d, want 2", cold)
	}
}

func TestFrozenOnRegistration(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()

	// Create a session file with old mod time (>24h)
	oldPath := filepath.Join(tmpDir, "old-session.jsonl")
	if err := os.WriteFile(oldPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(oldPath, oldTime, oldTime)

	// Create a session file with recent mod time
	newPath := filepath.Join(tmpDir, "new-session.jsonl")
	if err := os.WriteFile(newPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	tw.RegisterSession("old-session", oldPath)
	tw.RegisterSession("new-session", newPath)

	tw.mu.Lock()
	oldInfo := tw.sessions["old-session"]
	newInfo := tw.sessions["new-session"]
	tw.mu.Unlock()

	if !oldInfo.Frozen {
		t.Error("old session should be frozen on registration")
	}
	if newInfo.Frozen {
		t.Error("new session should not be frozen on registration")
	}
}

func TestFrozenOnBatchRegistration(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()

	// Create files
	oldPath := filepath.Join(tmpDir, "old.jsonl")
	newPath := filepath.Join(tmpDir, "new.jsonl")
	for _, p := range []string{oldPath, newPath} {
		if err := os.WriteFile(p, []byte("{}"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(oldPath, oldTime, oldTime)

	oldStat, _ := os.Stat(oldPath)
	newStat, _ := os.Stat(newPath)

	tw.RegisterSessions([]SessionInfo{
		{ID: "old", Path: oldPath, ModTime: oldStat.ModTime()},
		{ID: "new", Path: newPath, ModTime: newStat.ModTime()},
	})

	tw.mu.Lock()
	oldFrozen := tw.sessions["old"].Frozen
	newFrozen := tw.sessions["new"].Frozen
	tw.mu.Unlock()

	if !oldFrozen {
		t.Error("old session should be frozen after batch registration")
	}
	if newFrozen {
		t.Error("new session should not be frozen after batch registration")
	}
}

func TestFrozenSkippedInPoll(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()

	// Create an old session (frozen) and a recent session (not frozen)
	frozenPath := filepath.Join(tmpDir, "frozen.jsonl")
	activePath := filepath.Join(tmpDir, "active.jsonl")
	for _, p := range []string{frozenPath, activePath} {
		if err := os.WriteFile(p, []byte("{}"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(frozenPath, oldTime, oldTime)

	tw.RegisterSession("frozen", frozenPath)
	tw.RegisterSession("active", activePath)

	// Verify frozen session has Frozen=true
	tw.mu.Lock()
	if !tw.sessions["frozen"].Frozen {
		t.Fatal("frozen session should be frozen")
	}
	tw.mu.Unlock()

	// Modify the frozen file — pollColdSessions should skip it
	if err := os.WriteFile(frozenPath, []byte("{\"updated\":true}"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	tw.pollColdSessions()

	// Frozen session should still be frozen (change not detected)
	tw.mu.Lock()
	info := tw.sessions["frozen"]
	tw.mu.Unlock()

	if !info.Frozen {
		t.Error("frozen session should still be frozen after poll (skipped)")
	}
}

func TestTouchUnfreezes(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()

	path := filepath.Join(tmpDir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(path, oldTime, oldTime)

	tw.RegisterSession("session", path)

	tw.mu.Lock()
	if !tw.sessions["session"].Frozen {
		t.Fatal("session should start frozen")
	}
	tw.mu.Unlock()

	tw.Touch("session")

	tw.mu.Lock()
	if tw.sessions["session"].Frozen {
		t.Error("session should be unfrozen after Touch()")
	}
	tw.mu.Unlock()
}

func TestPromoteToHotUnfreezes(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()

	path := filepath.Join(tmpDir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(path, oldTime, oldTime)

	tw.RegisterSession("session", path)

	tw.mu.Lock()
	if !tw.sessions["session"].Frozen {
		t.Fatal("session should start frozen")
	}
	tw.mu.Unlock()

	tw.PromoteToHot("session")

	tw.mu.Lock()
	frozen := tw.sessions["session"].Frozen
	tw.mu.Unlock()

	if frozen {
		t.Error("session should be unfrozen after PromoteToHot()")
	}
}

func TestStatsFrozenCount(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()
	tw.SetHotTarget(1)

	// Create 3 old sessions and 2 recent sessions
	for i := 0; i < 5; i++ {
		id := "session-" + string('a'+byte(i))
		path := filepath.Join(tmpDir, id+".jsonl")
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
		if i < 3 {
			oldTime := time.Now().Add(-48 * time.Hour)
			_ = os.Chtimes(path, oldTime, oldTime)
		}
		tw.RegisterSession(id, path)
	}

	// Promote one recent session to HOT
	tw.PromoteToHot("session-d")

	hot, cold, frozen, _ := tw.Stats()

	if hot != 1 {
		t.Errorf("hot = %d, want 1", hot)
	}
	if frozen != 3 {
		t.Errorf("frozen = %d, want 3", frozen)
	}
	if cold != 1 {
		t.Errorf("cold = %d, want 1", cold)
	}
}

func TestBatchReadDirPollDetectsChanges(t *testing.T) {
	tmpDir := t.TempDir()
	// Use a subdirectory not watched by fsnotify to avoid race on close
	sessionDir := filepath.Join(tmpDir, "sessions")
	if err := os.Mkdir(sessionDir, 0755); err != nil {
		t.Fatalf("Mkdir error: %v", err)
	}

	cfg := Config{
		RootDir:     tmpDir,
		FilePattern: ".jsonl",
		ExtractID: func(path string) string {
			return strings.TrimSuffix(filepath.Base(path), ".jsonl")
		},
	}

	tw, ch, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = tw.Close() }()

	// Create session files in the subdirectory (not directly watched)
	for i := 0; i < 3; i++ {
		id := "session-" + string('a'+byte(i))
		path := filepath.Join(sessionDir, id+".jsonl")
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
		tw.RegisterSession(id, path)
	}

	// Modify session-b after registration
	time.Sleep(10 * time.Millisecond) // ensure mod time differs
	bPath := filepath.Join(sessionDir, "session-b.jsonl")
	if err := os.WriteFile(bPath, []byte("{\"updated\":true}"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	tw.pollColdSessions()

	// Drain events and check for session-b update
	found := false
	timeout := time.After(100 * time.Millisecond)
	for {
		select {
		case evt := <-ch:
			if evt.SessionID == "session-b" {
				found = true
			}
		case <-timeout:
			goto done
		}
	}
done:
	if !found {
		t.Error("expected update event for session-b after modification")
	}
}
