package claudecode

import (
	"os"
	"testing"

	"github.com/marcus/sidecar/internal/adapter"
)

func TestDetect(t *testing.T) {
	a := New()

	// Get the current working directory for testing
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Try to detect sessions for current project (may or may not exist)
	found, err := a.Detect(cwd)
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	t.Logf("Claude Code sessions for %s: %v", cwd, found)

	// Should not detect for non-existent project
	found, err = a.Detect("/nonexistent/path")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Error("should not find sessions for nonexistent path")
	}
}

func TestSessions(t *testing.T) {
	a := New()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	sessions, err := a.Sessions(cwd)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) == 0 {
		t.Skip("no sessions found for testing")
	}

	t.Logf("found %d sessions", len(sessions))

	// Check first session has required fields
	s := sessions[0]
	if s.ID == "" {
		t.Error("session ID should not be empty")
	}
	if s.Name == "" {
		t.Error("session Name should not be empty")
	}
	if s.CreatedAt.IsZero() {
		t.Error("session CreatedAt should not be zero")
	}
	if s.UpdatedAt.IsZero() {
		t.Error("session UpdatedAt should not be zero")
	}

	t.Logf("newest session: %s (updated %v)", s.ID, s.UpdatedAt)
}

func TestMessages(t *testing.T) {
	a := New()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	sessions, err := a.Sessions(cwd)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) == 0 {
		t.Skip("no sessions found for testing")
	}

	// Get messages from the most recent session
	messages, err := a.Messages(sessions[0].ID)
	if err != nil {
		t.Fatalf("Messages error: %v", err)
	}

	if len(messages) == 0 {
		t.Skip("no messages in session")
	}

	t.Logf("found %d messages", len(messages))

	// Check first message
	m := messages[0]
	if m.ID == "" {
		t.Error("message ID should not be empty")
	}
	if m.Role != "user" && m.Role != "assistant" {
		t.Errorf("unexpected role: %s", m.Role)
	}
	if m.Timestamp.IsZero() {
		t.Error("message Timestamp should not be zero")
	}

	// Check for tool uses in assistant messages
	toolUseCount := 0
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolUses) > 0 {
			toolUseCount += len(msg.ToolUses)
		}
	}
	t.Logf("found %d tool uses across messages", toolUseCount)
}

func TestUsage(t *testing.T) {
	a := New()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	sessions, err := a.Sessions(cwd)
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) == 0 {
		t.Skip("no sessions found for testing")
	}

	usage, err := a.Usage(sessions[0].ID)
	if err != nil {
		t.Fatalf("Usage error: %v", err)
	}

	t.Logf("usage: input=%d output=%d cache_read=%d cache_write=%d messages=%d",
		usage.TotalInputTokens, usage.TotalOutputTokens,
		usage.TotalCacheRead, usage.TotalCacheWrite,
		usage.MessageCount)

	if usage.MessageCount == 0 {
		t.Error("expected at least one message")
	}
}

func TestCapabilities(t *testing.T) {
	a := New()
	caps := a.Capabilities()

	if !caps["sessions"] {
		t.Error("expected sessions capability")
	}
	if !caps["messages"] {
		t.Error("expected messages capability")
	}
	if !caps["usage"] {
		t.Error("expected usage capability")
	}
	if !caps["watch"] {
		t.Error("expected watch capability")
	}
}

func TestProjectDirPath_RelativePath(t *testing.T) {
	a := New()

	// Get absolute path for comparison
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Test that relative "." gets converted to absolute path
	relPath := a.projectDirPath(".")
	absPath := a.projectDirPath(cwd)

	if relPath != absPath {
		t.Errorf("relative path '.' should resolve to same as absolute path\ngot:  %s\nwant: %s", relPath, absPath)
	}
}

func TestProjectDirPath_AbsolutePath(t *testing.T) {
	a := New()

	// Test known absolute path produces expected result
	path := a.projectDirPath("/Users/test/code/project")

	// Should contain the hashed path
	expected := "-Users-test-code-project"
	if !containsPath(path, expected) {
		t.Errorf("path %q should contain %q", path, expected)
	}
}

func TestDetect_RelativePath(t *testing.T) {
	a := New()

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Both relative and absolute should return same result
	foundRel, errRel := a.Detect(".")
	foundAbs, errAbs := a.Detect(cwd)

	if errRel != nil {
		t.Fatalf("Detect(.) error: %v", errRel)
	}
	if errAbs != nil {
		t.Fatalf("Detect(cwd) error: %v", errAbs)
	}

	if foundRel != foundAbs {
		t.Errorf("Detect('.') = %v, Detect(cwd) = %v - should be equal", foundRel, foundAbs)
	}
}

func containsPath(path, substr string) bool {
	return len(path) > 0 && len(substr) > 0 && path[len(path)-len(substr):] == substr ||
		(len(path) >= len(substr) && path[len(path)-len(substr)-1:len(path)-1] == substr)
}

func TestSessions_RelativePath(t *testing.T) {
	a := New()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Both should return same sessions
	sessionsRel, errRel := a.Sessions(".")
	sessionsAbs, errAbs := a.Sessions(cwd)

	if errRel != nil {
		t.Fatalf("Sessions(.) error: %v", errRel)
	}
	if errAbs != nil {
		t.Fatalf("Sessions(cwd) error: %v", errAbs)
	}

	if len(sessionsRel) != len(sessionsAbs) {
		t.Errorf("Sessions('.') = %d sessions, Sessions(cwd) = %d sessions - should be equal",
			len(sessionsRel), len(sessionsAbs))
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		id       string
		expected string
	}{
		{"12345678", "12345678"},
		{"123456789abcdef", "12345678"},
		{"1234567", "1234567"},
		{"abc", "abc"},
		{"", ""},
	}

	for _, tt := range tests {
		result := shortID(tt.id)
		if result != tt.expected {
			t.Errorf("shortID(%q) = %q, expected %q", tt.id, result, tt.expected)
		}
	}
}

// copyTestdataFile copies a testdata file to the target path
func copyTestdataFile(t *testing.T, testdataFile, targetPath string) {
	t.Helper()
	data, err := os.ReadFile(testdataFile)
	if err != nil {
		t.Fatalf("failed to read testdata file: %v", err)
	}
	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
}

func TestSlugExtraction(t *testing.T) {
	// Create temp dir mimicking ~/.claude/projects/{hash}/
	tmpDir := t.TempDir()

	// Create adapter with custom projects dir
	a := &Adapter{projectsDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	// Create project hash dir (simulates -home-user-project)
	projectHash := "-test-project"
	projectDir := tmpDir + "/" + projectHash
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Copy testdata file with slug
	testdataPath := "testdata/valid_session_with_slug.jsonl"
	targetPath := projectDir + "/test-session-slug.jsonl"
	copyTestdataFile(t, testdataPath, targetPath)

	// Set projectsDir to point to our temp structure
	// The adapter uses projectDirPath which adds the hash, so we need to
	// directly call parseSessionMetadata or override the path logic

	// Test parseSessionMetadata directly
	meta, err := a.parseSessionMetadata(targetPath)
	if err != nil {
		t.Fatalf("parseSessionMetadata error: %v", err)
	}

	// Verify slug extraction
	if meta.Slug != "implement-feature-xyz" {
		t.Errorf("expected slug 'implement-feature-xyz', got %q", meta.Slug)
	}

	// Verify other metadata
	if meta.SessionID != "test-session-slug" {
		t.Errorf("expected sessionID 'test-session-slug', got %q", meta.SessionID)
	}
	if meta.MsgCount != 3 {
		t.Errorf("expected 3 messages, got %d", meta.MsgCount)
	}
	if meta.CWD != "/home/user/project" {
		t.Errorf("expected CWD '/home/user/project', got %q", meta.CWD)
	}

	t.Logf("slug=%s sessionID=%s msgs=%d", meta.Slug, meta.SessionID, meta.MsgCount)
}

func TestSlugExtraction_NoSlug(t *testing.T) {
	// Create temp dir
	tmpDir := t.TempDir()
	a := &Adapter{projectsDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	projectDir := tmpDir + "/-test-project"
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Copy testdata file without slug
	testdataPath := "testdata/valid_session.jsonl"
	targetPath := projectDir + "/test-session-001.jsonl"
	copyTestdataFile(t, testdataPath, targetPath)

	meta, err := a.parseSessionMetadata(targetPath)
	if err != nil {
		t.Fatalf("parseSessionMetadata error: %v", err)
	}

	// Verify no slug
	if meta.Slug != "" {
		t.Errorf("expected empty slug, got %q", meta.Slug)
	}

	// Verify session ID is extracted from filename
	if meta.SessionID != "test-session-001" {
		t.Errorf("expected sessionID 'test-session-001', got %q", meta.SessionID)
	}

	t.Logf("slug=%q sessionID=%s msgs=%d", meta.Slug, meta.SessionID, meta.MsgCount)
}

func TestSlugExtraction_SessionsIntegration(t *testing.T) {
	// Create temp dir with project structure
	tmpDir := t.TempDir()
	a := &Adapter{projectsDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	// Create project hash dir that matches what projectDirPath would generate
	// For path "/test/project", the hash is "-test-project"
	projectDir := tmpDir + "/-test-project"
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Copy both session files
	copyTestdataFile(t, "testdata/valid_session_with_slug.jsonl", projectDir+"/test-session-slug.jsonl")
	copyTestdataFile(t, "testdata/valid_session.jsonl", projectDir+"/test-session-001.jsonl")

	// Call Sessions with a path that hashes to our project dir
	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions error: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Find session with slug
	var withSlug, withoutSlug bool
	for _, s := range sessions {
		t.Logf("session: id=%s name=%s slug=%q", s.ID, s.Name, s.Slug)
		if s.ID == "test-session-slug" {
			withSlug = true
			if s.Slug != "implement-feature-xyz" {
				t.Errorf("expected slug 'implement-feature-xyz', got %q", s.Slug)
			}
			// Name should be first user message, not slug
			if s.Name != "Hello" {
				t.Errorf("expected name 'Hello' (first user message), got %q", s.Name)
			}
		}
		if s.ID == "test-session-001" {
			withoutSlug = true
			if s.Slug != "" {
				t.Errorf("expected empty slug, got %q", s.Slug)
			}
			// Name should be first user message
			if s.Name != "Hello, can you help me?" {
				t.Errorf("expected name 'Hello, can you help me?' (first user message), got %q", s.Name)
			}
		}
	}

	if !withSlug {
		t.Error("missing session with slug")
	}
	if !withoutSlug {
		t.Error("missing session without slug")
	}
}

func TestSlugExtraction_SlugOnLaterMessage(t *testing.T) {
	// Test that slug is extracted even if it appears on a later message
	tmpDir := t.TempDir()
	a := &Adapter{projectsDir: tmpDir, sessionIndex: make(map[string]string), metaCache: make(map[string]sessionMetaCacheEntry)}

	projectDir := tmpDir + "/-test-project"
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create a session where slug appears on second message
	sessionData := `{"type":"user","uuid":"msg-001","sessionId":"late-slug-session","timestamp":"2024-01-15T10:00:00Z","message":{"role":"user","content":"First message"}}
{"type":"assistant","uuid":"msg-002","sessionId":"late-slug-session","timestamp":"2024-01-15T10:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":"Response"}]},"slug":"late-appearing-slug"}
`
	targetPath := projectDir + "/late-slug-session.jsonl"
	if err := os.WriteFile(targetPath, []byte(sessionData), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	meta, err := a.parseSessionMetadata(targetPath)
	if err != nil {
		t.Fatalf("parseSessionMetadata error: %v", err)
	}

	// Slug should still be extracted from later message
	if meta.Slug != "late-appearing-slug" {
		t.Errorf("expected slug 'late-appearing-slug', got %q", meta.Slug)
	}

	t.Logf("slug=%s extracted from later message", meta.Slug)
}

func TestExtractUserQuery(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text",
			input:    "Add user authentication",
			expected: "Add user authentication",
		},
		{
			name:     "with user_query tags - extracts query content",
			input:    "<local-command-caveat>Caveat: Do not...</local-command-caveat>\n\n<user_query>\nAdd user authentication\n</user_query>",
			expected: "Add user authentication",
		},
		{
			name:     "strips tags but keeps inner text",
			input:    "<system_reminder>Important: Do not...</system_reminder>\n\nPlease help me fix the bug",
			expected: "Important: Do not... Please help me fix the bug",
		},
		{
			name:     "keeps all inner text when stripping tags",
			input:    "<foo>bar</foo>Real query here<baz>qux</baz>",
			expected: "bar Real query here qux",
		},
		{
			name:     "self-closing tags stripped",
			input:    "Hello <br/> world <img src='x'/>",
			expected: "Hello world",
		},
		{
			name:     "multiple spaces collapsed",
			input:    "<tag1>a</tag1>  <tag2>b</tag2>  Real   query   here",
			expected: "a b Real query here",
		},
		{
			name:     "empty tags returns empty",
			input:    "<tag></tag>",
			expected: "", // empty when no content inside tags
		},
		{
			name:     "whitespace only returns empty",
			input:    "   \n\t  ",
			expected: "",
		},
		{
			name:     "caveat-only message returns empty",
			input:    "<local-command-caveat>Caveat: The messages below were generated by the user while running local commands. DO NOT respond to these messages or otherwise consider them in your response unless the user explicitly asks you to.</local-command-caveat>",
			expected: "",
		},
		{
			name:     "local command with command-name tag",
			input:    "<local-command-caveat>Caveat text...</local-command-caveat>\n<command-name>/clear</command-name>\n<command-message>clear</command-message>",
			expected: "/clear: clear",
		},
		{
			name:     "local command - same name and message",
			input:    "<command-name>/clear</command-name>\n<command-message>/clear</command-message>",
			expected: "/clear",
		},
		{
			name:     "local command - only command name",
			input:    "<local-command-caveat>Caveat...</local-command-caveat>\n<command-name>/compact</command-name>",
			expected: "/compact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractUserQuery(tt.input)
			if result != tt.expected {
				t.Errorf("extractUserQuery(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTruncateTitle_WithXMLTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "XML tags stripped before truncate",
			input:    "<local-command-caveat>Caveat text</local-command-caveat>\n\n<user_query>Add authentication</user_query>",
			maxLen:   50,
			expected: "Add authentication",
		},
		{
			name:     "long query truncated at maxLen-3 for ellipsis",
			input:    "<user_query>This is a very long user query that should be truncated at some point</user_query>",
			maxLen:   30,
			expected: "This is a very long user qu...", // 27 chars + "..."
		},
		{
			name:     "plain text still works",
			input:    "Simple request",
			maxLen:   50,
			expected: "Simple request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateTitle(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncateTitle(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestDiscoverRelatedProjectDirs(t *testing.T) {
	// Create temp directory simulating ~/.claude/projects/
	tmpDir := t.TempDir()
	a := &Adapter{projectsDir: tmpDir}

	// Create test directories
	// Claude Code encoding: /Users/test/code/myrepo -> -Users-test-code-myrepo
	// KNOWN LIMITATION: Decoding is lossy - hyphens in original paths become slashes.
	// E.g., worktree at /Users/test/code/myrepo-feature encodes to -Users-test-code-myrepo-feature
	// but decodes to /Users/test/code/myrepo/feature (incorrect, but acceptable for discovery purposes)
	dirs := []string{
		"-Users-test-code-myrepo",         // main repo
		"-Users-test-code-myrepo-feature", // worktree (decodes with slash, not hyphen)
		"-Users-test-code-myrepo-bugfix",  // worktree (decodes with slash, not hyphen)
		"-Users-test-other",               // unrelated project
		"-Users-test-code-myrepo2",        // different repo (myrepo2, not myrepo)
	}
	for _, d := range dirs {
		if err := os.MkdirAll(tmpDir+"/"+d, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
	}

	tests := []struct {
		name     string
		mainPath string
		want     []string
	}{
		{
			name:     "finds related paths",
			mainPath: "/Users/test/code/myrepo",
			// Note: decoded paths have slashes where original had hyphens (known limitation)
			want: []string{"/Users/test/code/myrepo", "/Users/test/code/myrepo/feature", "/Users/test/code/myrepo/bugfix"},
		},
		{
			name:     "empty for invalid main path",
			mainPath: "",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.DiscoverRelatedProjectDirs(tt.mainPath)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			// Check we got expected paths (order may vary)
			if tt.name == "finds related paths" {
				if len(got) != 3 {
					t.Errorf("expected 3 paths, got %d: %v", len(got), got)
				}
				// Verify myrepo2 and other are not included
				for _, p := range got {
					if p == "/Users/test/other" || p == "/Users/test/code/myrepo2" {
						t.Errorf("should not include unrelated path: %s", p)
					}
				}
			}
		})
	}
}

func TestDiscoverRelatedProjectDirs_EmptyProjectsDir(t *testing.T) {
	tmpDir := t.TempDir()
	a := &Adapter{projectsDir: tmpDir}

	got, err := a.DiscoverRelatedProjectDirs("/Users/test/myrepo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestDiscoverRelatedProjectDirs_NonexistentProjectsDir(t *testing.T) {
	a := &Adapter{projectsDir: "/nonexistent/path/should/not/exist"}

	got, err := a.DiscoverRelatedProjectDirs("/Users/test/myrepo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestIncrementalMetadataParsing(t *testing.T) {
	// Create a temp session file
	tmpDir := t.TempDir()
	sessionPath := tmpDir + "/test-session.jsonl"

	// Write initial messages
	initial := `{"type":"user","timestamp":"2024-01-01T10:00:00Z","message":{"role":"user","content":"hello"},"cwd":"/tmp","version":"1.0"}
{"type":"assistant","timestamp":"2024-01-01T10:01:00Z","message":{"role":"assistant","content":"hi","model":"claude-sonnet-4-20250514","usage":{"input_tokens":100,"output_tokens":50}}}
`
	if err := os.WriteFile(sessionPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	a := New()

	// Full parse
	meta1, offset1, mc1, mt1, err := a.parseSessionMetadataFull(sessionPath)
	if err != nil {
		t.Fatalf("full parse: %v", err)
	}
	if meta1.MsgCount != 2 {
		t.Errorf("expected 2 msgs, got %d", meta1.MsgCount)
	}
	if meta1.TotalTokens != 150 {
		t.Errorf("expected 150 tokens, got %d", meta1.TotalTokens)
	}

	// Append new message
	appended := `{"type":"assistant","timestamp":"2024-01-01T10:02:00Z","message":{"role":"assistant","content":"more","model":"claude-sonnet-4-20250514","usage":{"input_tokens":200,"output_tokens":100}}}
`
	f, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(appended)
	f.Close()

	// Incremental parse from saved offset
	meta2, _, _, _, err := a.parseSessionMetadataIncremental(sessionPath, meta1, offset1, mc1, mt1)
	if err != nil {
		t.Fatalf("incremental parse: %v", err)
	}
	if meta2.MsgCount != 3 {
		t.Errorf("expected 3 msgs after incremental, got %d", meta2.MsgCount)
	}
	if meta2.TotalTokens != 450 {
		t.Errorf("expected 450 tokens after incremental, got %d", meta2.TotalTokens)
	}
	// Head fields preserved
	if meta2.CWD != "/tmp" {
		t.Errorf("CWD not preserved: %s", meta2.CWD)
	}
	if meta2.FirstMsg != meta1.FirstMsg {
		t.Error("FirstMsg changed after incremental parse")
	}

	// Full re-parse should give same results
	meta3, _, _, _, err := a.parseSessionMetadataFull(sessionPath)
	if err != nil {
		t.Fatalf("full re-parse: %v", err)
	}
	if meta3.MsgCount != meta2.MsgCount {
		t.Errorf("full vs incremental MsgCount: %d vs %d", meta3.MsgCount, meta2.MsgCount)
	}
	if meta3.TotalTokens != meta2.TotalTokens {
		t.Errorf("full vs incremental TotalTokens: %d vs %d", meta3.TotalTokens, meta2.TotalTokens)
	}
}

func TestSessionMetadataCacheIncremental(t *testing.T) {
	// Test that sessionMetadata uses incremental path on file growth
	tmpDir := t.TempDir()
	sessionPath := tmpDir + "/cached-session.jsonl"

	initial := `{"type":"user","timestamp":"2024-01-01T10:00:00Z","message":{"role":"user","content":"test"},"cwd":"/work"}
{"type":"assistant","timestamp":"2024-01-01T10:01:00Z","message":{"role":"assistant","content":"reply","model":"claude-sonnet-4-20250514","usage":{"input_tokens":50,"output_tokens":25}}}
`
	if err := os.WriteFile(sessionPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	a := New()
	a.metaCache = make(map[string]sessionMetaCacheEntry)

	// First call: full parse, populates cache
	info1, _ := os.Stat(sessionPath)
	meta1, err := a.sessionMetadata(sessionPath, info1)
	if err != nil {
		t.Fatal(err)
	}
	if meta1.MsgCount != 2 {
		t.Errorf("expected 2 msgs, got %d", meta1.MsgCount)
	}

	// Append and re-stat
	f, _ := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"assistant","timestamp":"2024-01-01T10:02:00Z","message":{"role":"assistant","content":"new","model":"claude-sonnet-4-20250514","usage":{"input_tokens":30,"output_tokens":20}}}` + "\n")
	f.Close()

	info2, _ := os.Stat(sessionPath)
	if info2.Size() <= info1.Size() {
		t.Fatal("file didn't grow")
	}

	// Second call: should use incremental path
	meta2, err := a.sessionMetadata(sessionPath, info2)
	if err != nil {
		t.Fatal(err)
	}
	if meta2.MsgCount != 3 {
		t.Errorf("expected 3 msgs after growth, got %d", meta2.MsgCount)
	}
	if meta2.TotalTokens != 125 {
		t.Errorf("expected 125 tokens, got %d", meta2.TotalTokens)
	}
}

func TestSessionByID(t *testing.T) {
	tmpDir := t.TempDir()
	projDir := tmpDir + "/-tmp-myproject"
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionData := `{"type":"user","timestamp":"2024-01-01T10:00:00Z","message":{"role":"user","content":"implement feature X"},"cwd":"/tmp/myproject"}
{"type":"assistant","timestamp":"2024-01-01T10:01:00Z","message":{"role":"assistant","content":"done","model":"claude-sonnet-4-20250514","usage":{"input_tokens":100,"output_tokens":50}}}
`
	sessionID := "test-session-123"
	if err := os.WriteFile(projDir+"/"+sessionID+".jsonl", []byte(sessionData), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &Adapter{
		projectsDir:  tmpDir,
		sessionIndex: map[string]string{sessionID: projDir + "/" + sessionID + ".jsonl"},
		metaCache:    make(map[string]sessionMetaCacheEntry),
	}

	session, err := a.SessionByID(sessionID)
	if err != nil {
		t.Fatalf("SessionByID: %v", err)
	}
	if session.ID != sessionID {
		t.Errorf("expected ID %s, got %s", sessionID, session.ID)
	}
	if session.MessageCount != 2 {
		t.Errorf("expected 2 msgs, got %d", session.MessageCount)
	}
	if session.TotalTokens != 150 {
		t.Errorf("expected 150 tokens, got %d", session.TotalTokens)
	}
	if session.AdapterID != "claude-code" {
		t.Errorf("expected adapter claude-code, got %s", session.AdapterID)
	}

	// Unknown session
	_, err = a.SessionByID("nonexistent")
	if err == nil {
		t.Error("expected error for unknown session")
	}
}

func TestSessionByIDImplementsTargetedRefresher(t *testing.T) {
	var a adapter.TargetedRefresher = New()
	_ = a
}
