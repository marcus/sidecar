package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkSessionFiles measures directory walking performance.
func BenchmarkSessionFiles(b *testing.B) {
	root := b.TempDir()
	sessionsDir := filepath.Join(root, "sessions")

	// Create realistic directory structure: YYYY/MM/DD/session.jsonl
	for _, year := range []string{"2024", "2025"} {
		for month := 1; month <= 12; month++ {
			for day := 1; day <= 28; day++ {
				path := filepath.Join(sessionsDir, year, fmt.Sprintf("%02d", month), fmt.Sprintf("%02d", day))
				if err := os.MkdirAll(path, 0o755); err != nil {
					b.Fatalf("mkdir: %v", err)
				}
				// Create a session file
				if err := writeSessionFile(filepath.Join(path, "session.jsonl"), []string{
					`{"timestamp":"2025-01-01T00:00:00Z","type":"session_meta","payload":{"id":"test","cwd":"/tmp"}}`,
				}); err != nil {
					b.Fatalf("write: %v", err)
				}
			}
		}
	}

	a := New()
	a.sessionsDir = sessionsDir

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Clear cache between runs
		a.dirCache = nil
		_, _ = a.sessionFiles()
	}
}

// BenchmarkSessionFilesCached measures directory listing with cache hit.
func BenchmarkSessionFilesCached(b *testing.B) {
	root := b.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	path := filepath.Join(sessionsDir, "2025", "01", "20")
	if err := os.MkdirAll(path, 0o755); err != nil {
		b.Fatalf("mkdir: %v", err)
	}
	if err := writeSessionFile(filepath.Join(path, "session.jsonl"), []string{
		`{"timestamp":"2025-01-01T00:00:00Z","type":"session_meta","payload":{"id":"test","cwd":"/tmp"}}`,
	}); err != nil {
		b.Fatalf("write: %v", err)
	}

	a := New()
	a.sessionsDir = sessionsDir
	// Prime the cache
	_, _ = a.sessionFiles()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = a.sessionFiles()
	}
}

// BenchmarkSessionMetadataSmall measures metadata parsing for small files.
func BenchmarkSessionMetadataSmall(b *testing.B) {
	root := b.TempDir()
	path := filepath.Join(root, "session.jsonl")

	// Create a small session file (~20 lines)
	lines := generateSessionLines(20)
	if err := writeSessionFile(path, lines); err != nil {
		b.Fatalf("write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		b.Fatalf("stat: %v", err)
	}

	a := New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = a.sessionMetadata(path, info)
		// Clear cache between runs
		a.metaCache = make(map[string]sessionMetaCacheEntry)
	}
}

// BenchmarkSessionMetadataLarge measures metadata parsing for large files.
func BenchmarkSessionMetadataLarge(b *testing.B) {
	root := b.TempDir()
	path := filepath.Join(root, "session.jsonl")

	// Create a large session file (~2000 lines, well above 16KB threshold)
	lines := generateSessionLines(2000)
	if err := writeSessionFile(path, lines); err != nil {
		b.Fatalf("write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		b.Fatalf("stat: %v", err)
	}

	a := New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = a.sessionMetadata(path, info)
		// Clear cache between runs
		a.metaCache = make(map[string]sessionMetaCacheEntry)
	}
}

// BenchmarkSessionMetadataCached measures metadata cache hit performance.
func BenchmarkSessionMetadataCached(b *testing.B) {
	root := b.TempDir()
	path := filepath.Join(root, "session.jsonl")

	lines := generateSessionLines(100)
	if err := writeSessionFile(path, lines); err != nil {
		b.Fatalf("write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		b.Fatalf("stat: %v", err)
	}

	a := New()
	// Prime the cache
	_, _ = a.sessionMetadata(path, info)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = a.sessionMetadata(path, info)
	}
}

// BenchmarkSessions measures the full Sessions() call.
func BenchmarkSessions(b *testing.B) {
	for _, numSessions := range []int{10, 50, 100} {
		b.Run(fmt.Sprintf("n=%d", numSessions), func(b *testing.B) {
			root := b.TempDir()
			sessionsDir := filepath.Join(root, "sessions")
			projectDir := filepath.Join(root, "project")
			if err := os.MkdirAll(projectDir, 0o755); err != nil {
				b.Fatalf("mkdir project: %v", err)
			}

			// Create sessions
			for i := 0; i < numSessions; i++ {
				day := (i % 28) + 1
				month := (i / 28 % 12) + 1
				path := filepath.Join(sessionsDir, "2025", fmt.Sprintf("%02d", month), fmt.Sprintf("%02d", day))
				if err := os.MkdirAll(path, 0o755); err != nil {
					b.Fatalf("mkdir: %v", err)
				}

				lines := []string{
					fmt.Sprintf(`{"timestamp":"2025-%02d-%02dT00:00:00Z","type":"session_meta","payload":{"id":"sess-%d","cwd":"%s"}}`, month, day, i, projectDir),
					fmt.Sprintf(`{"timestamp":"2025-%02d-%02dT00:01:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}`, month, day),
					fmt.Sprintf(`{"timestamp":"2025-%02d-%02dT00:02:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}}`, month, day),
				}
				if err := writeSessionFile(filepath.Join(path, fmt.Sprintf("session-%d.jsonl", i)), lines); err != nil {
					b.Fatalf("write: %v", err)
				}
			}

			a := New()
			a.sessionsDir = sessionsDir

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Clear caches between runs
				a.dirCache = nil
				a.metaCache = make(map[string]sessionMetaCacheEntry)
				_, _ = a.Sessions(projectDir)
			}
		})
	}
}

// BenchmarkCwdMatchesProject measures path matching performance.
func BenchmarkCwdMatchesProject(b *testing.B) {
	root := b.TempDir()
	projectDir := filepath.Join(root, "projects", "myrepo")
	cwdDir := filepath.Join(projectDir, "src", "internal")
	if err := os.MkdirAll(cwdDir, 0o755); err != nil {
		b.Fatalf("mkdir: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cwdMatchesProject(projectDir, cwdDir)
	}
}

// BenchmarkResolvedProjectPath measures optimized path matching (td-6543fee4).
func BenchmarkResolvedProjectPath(b *testing.B) {
	root := b.TempDir()
	projectDir := filepath.Join(root, "projects", "myrepo")
	cwdDir := filepath.Join(projectDir, "src", "internal")
	if err := os.MkdirAll(cwdDir, 0o755); err != nil {
		b.Fatalf("mkdir: %v", err)
	}

	// Pre-resolve project path
	resolved := newResolvedProjectPath(projectDir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = resolved.matchesCWD(cwdDir)
	}
}

// generateSessionLines creates realistic session JSONL lines.
func generateSessionLines(count int) []string {
	lines := make([]string, 0, count+2)

	// Session metadata
	lines = append(lines, `{"timestamp":"2025-01-20T12:00:00Z","type":"session_meta","payload":{"id":"bench-session","timestamp":"2025-01-20T12:00:00Z","cwd":"/tmp/project"}}`)

	// User/assistant message pairs
	for i := 0; i < count/2; i++ {
		lines = append(lines, fmt.Sprintf(`{"timestamp":"2025-01-20T12:%02d:%02dZ","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"User message %d with some content to make it realistic"}]}}`, i%60, i%60, i))
		lines = append(lines, fmt.Sprintf(`{"timestamp":"2025-01-20T12:%02d:%02dZ","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Assistant response %d with detailed explanation and code examples to simulate real usage"}]}}`, i%60, (i+1)%60, i))
	}

	// Token count event at end
	lines = append(lines, `{"timestamp":"2025-01-20T13:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":500,"output_tokens":2000,"reasoning_output_tokens":100,"total_tokens":3000}}}}`)

	return lines
}
