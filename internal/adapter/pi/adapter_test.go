package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/adapter"
)

// newTestAdapter creates an Adapter with sessionsDir pointing at a temp dir
// containing copies of the named testdata fixtures.
func newTestAdapter(t *testing.T, fixtures ...string) *Adapter {
	t.Helper()
	dir := t.TempDir()
	a := New()
	a.sessionsDir = dir
	for _, f := range fixtures {
		src := filepath.Join("testdata", f)
		dst := filepath.Join(dir, f)
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read fixture %s: %v", f, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", f, err)
		}
	}
	return a
}

// populateIndex calls Sessions to build the sessionIndex so Messages/Usage work.
func populateIndex(t *testing.T, a *Adapter, projectRoot string) []string {
	t.Helper()
	sessions, err := a.Sessions(projectRoot)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	ids := make([]string, len(sessions))
	for i, s := range sessions {
		ids[i] = s.ID
	}
	return ids
}

func TestDetect(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")

	found, err := a.Detect("/test/project")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !found {
		t.Fatal("Detect should return true for matching CWD")
	}

	found, err = a.Detect("/nonexistent/path")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Fatal("Detect should return false for non-matching CWD")
	}
}

func TestDetectMissingDir(t *testing.T) {
	a := New()
	a.sessionsDir = "/nonexistent/sessions/dir"

	found, err := a.Detect("/test/project")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if found {
		t.Fatal("Detect should return false when sessions dir missing")
	}
}

func TestSessions(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	s := sessions[0]
	if s.ID != "test-basic" {
		t.Errorf("ID = %q, want test-basic", s.ID)
	}
	if s.Name != "Hello, what files are here?" {
		t.Errorf("Name = %q, want first user message", s.Name)
	}
	if s.AdapterID != "pi" {
		t.Errorf("AdapterID = %q, want pi", s.AdapterID)
	}
	if s.AdapterName != "Pi" {
		t.Errorf("AdapterName = %q, want Pi", s.AdapterName)
	}
	if s.AdapterIcon != "\U0001F43E" {
		t.Errorf("AdapterIcon = %q, want paw icon", s.AdapterIcon)
	}
	if s.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if s.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
	if s.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", s.MessageCount)
	}
	if s.FileSize <= 0 {
		t.Errorf("FileSize = %d, want > 0", s.FileSize)
	}
	if s.Path == "" {
		t.Error("Path should be set")
	}
}

func TestSessionsFiltering(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl", "other-project-session.jsonl")

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session for /test/project, got %d", len(sessions))
	}
	if sessions[0].ID != "test-basic" {
		t.Errorf("expected test-basic, got %s", sessions[0].ID)
	}
}

func TestSessionsOrdering(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl", "tool-session.jsonl")

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) < 2 {
		t.Fatalf("expected at least 2 sessions, got %d", len(sessions))
	}

	// Sessions should be sorted by UpdatedAt desc
	for i := 1; i < len(sessions); i++ {
		if sessions[i].UpdatedAt.After(sessions[i-1].UpdatedAt) {
			t.Errorf("sessions not sorted desc by UpdatedAt: [%d]=%v > [%d]=%v",
				i, sessions[i].UpdatedAt, i-1, sessions[i-1].UpdatedAt)
		}
	}
}

func TestMessages(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")
	populateIndex(t, a, "/test/project")

	messages, err := a.Messages("test-basic")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// First message: user
	m0 := messages[0]
	if m0.ID != "m1" {
		t.Errorf("msg[0].ID = %q, want m1", m0.ID)
	}
	if m0.Role != "user" {
		t.Errorf("msg[0].Role = %q, want user", m0.Role)
	}
	if m0.Content != "Hello, what files are here?" {
		t.Errorf("msg[0].Content = %q", m0.Content)
	}
	if m0.Timestamp.IsZero() {
		t.Error("msg[0].Timestamp should not be zero")
	}

	// Second message: assistant
	m1 := messages[1]
	if m1.ID != "m2" {
		t.Errorf("msg[1].ID = %q, want m2", m1.ID)
	}
	if m1.Role != "assistant" {
		t.Errorf("msg[1].Role = %q, want assistant", m1.Role)
	}
	if m1.Content != "Let me check the files." {
		t.Errorf("msg[1].Content = %q", m1.Content)
	}
	if m1.Model != "claude-opus-4-5" {
		t.Errorf("msg[1].Model = %q, want claude-opus-4-5", m1.Model)
	}
	if m1.Timestamp.IsZero() {
		t.Error("msg[1].Timestamp should not be zero")
	}
}

func TestMessagesToolLinking(t *testing.T) {
	a := newTestAdapter(t, "tool-session.jsonl")
	populateIndex(t, a, "/test/project")

	messages, err := a.Messages("test-tool")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}

	// Expected messages: user(m1), assistant(m2 with tool call + tool result linked), assistant(m4)
	// toolResult (m3) is linked into m2, not a separate message
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	// Assistant message m2 should have tool use
	assistantMsg := messages[1]
	if assistantMsg.Role != "assistant" {
		t.Fatalf("msg[1] role = %q, want assistant", assistantMsg.Role)
	}
	if len(assistantMsg.ToolUses) != 1 {
		t.Fatalf("expected 1 tool use, got %d", len(assistantMsg.ToolUses))
	}
	tu := assistantMsg.ToolUses[0]
	if tu.ID != "tc1" {
		t.Errorf("tool use ID = %q, want tc1", tu.ID)
	}
	if tu.Name != "exec" {
		t.Errorf("tool use Name = %q, want exec", tu.Name)
	}
	if tu.Output != "file1.go\nfile2.go" {
		t.Errorf("tool use Output = %q, want linked result", tu.Output)
	}

	// Check ContentBlocks for tool_use and tool_result
	var hasToolUse, hasToolResult bool
	for _, cb := range assistantMsg.ContentBlocks {
		if cb.Type == "tool_use" && cb.ToolUseID == "tc1" {
			hasToolUse = true
		}
		if cb.Type == "tool_result" && cb.ToolUseID == "tc1" {
			hasToolResult = true
		}
	}
	if !hasToolUse {
		t.Error("expected tool_use content block")
	}
	if !hasToolResult {
		t.Error("expected tool_result content block linked to assistant message")
	}
}

func TestMessagesThinking(t *testing.T) {
	a := newTestAdapter(t, "tool-session.jsonl")
	populateIndex(t, a, "/test/project")

	messages, err := a.Messages("test-tool")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}

	// msg[1] is assistant with thinking block
	assistantMsg := messages[1]
	if len(assistantMsg.ThinkingBlocks) != 1 {
		t.Fatalf("expected 1 thinking block, got %d", len(assistantMsg.ThinkingBlocks))
	}
	tb := assistantMsg.ThinkingBlocks[0]
	if tb.Content != "Let me check." {
		t.Errorf("thinking content = %q, want 'Let me check.'", tb.Content)
	}
	if tb.TokenCount <= 0 {
		t.Errorf("thinking TokenCount = %d, want > 0", tb.TokenCount)
	}
}

func TestMessagesCacheHit(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")
	populateIndex(t, a, "/test/project")

	msgs1, err := a.Messages("test-basic")
	if err != nil {
		t.Fatalf("first Messages: %v", err)
	}

	msgs2, err := a.Messages("test-basic")
	if err != nil {
		t.Fatalf("second Messages: %v", err)
	}

	if len(msgs1) != len(msgs2) {
		t.Errorf("cache hit returned different count: %d vs %d", len(msgs1), len(msgs2))
	}
	for i := range msgs1 {
		if msgs1[i].ID != msgs2[i].ID {
			t.Errorf("msg[%d] ID mismatch: %s vs %s", i, msgs1[i].ID, msgs2[i].ID)
		}
	}
}

func TestMessagesIncremental(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")
	populateIndex(t, a, "/test/project")

	msgs1, err := a.Messages("test-basic")
	if err != nil {
		t.Fatalf("first Messages: %v", err)
	}
	if len(msgs1) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs1))
	}

	// Append a new message line to the session file
	sessionPath := filepath.Join(a.sessionsDir, "basic-session.jsonl")
	f, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	_, err = f.WriteString(`{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-02-01T00:00:04Z","message":{"role":"user","content":[{"type":"text","text":"What about tests?"}]}}` + "\n")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	msgs2, err := a.Messages("test-basic")
	if err != nil {
		t.Fatalf("second Messages: %v", err)
	}
	if len(msgs2) != 3 {
		t.Fatalf("expected 3 messages after append, got %d", len(msgs2))
	}
	if msgs2[2].ID != "m3" {
		t.Errorf("new message ID = %q, want m3", msgs2[2].ID)
	}
	if msgs2[2].Content != "What about tests?" {
		t.Errorf("new message content = %q", msgs2[2].Content)
	}
}

func TestUsage(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")
	populateIndex(t, a, "/test/project")

	usage, err := a.Usage("test-basic")
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}

	// basic-session has 1 assistant message with usage: input=500, output=100, cacheRead=400, cacheWrite=100
	if usage.TotalInputTokens != 500 {
		t.Errorf("TotalInputTokens = %d, want 500", usage.TotalInputTokens)
	}
	if usage.TotalOutputTokens != 100 {
		t.Errorf("TotalOutputTokens = %d, want 100", usage.TotalOutputTokens)
	}
	if usage.TotalCacheRead != 400 {
		t.Errorf("TotalCacheRead = %d, want 400", usage.TotalCacheRead)
	}
	if usage.TotalCacheWrite != 100 {
		t.Errorf("TotalCacheWrite = %d, want 100", usage.TotalCacheWrite)
	}
	// MessageCount includes user + assistant = 2
	if usage.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", usage.MessageCount)
	}
}

func TestUsageMultipleAssistantMessages(t *testing.T) {
	a := newTestAdapter(t, "tool-session.jsonl")
	populateIndex(t, a, "/test/project")

	usage, err := a.Usage("test-tool")
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}

	// tool-session has 2 assistant messages:
	// m2: input=500, output=100
	// m4: input=600, output=50
	if usage.TotalInputTokens != 1100 {
		t.Errorf("TotalInputTokens = %d, want 1100", usage.TotalInputTokens)
	}
	if usage.TotalOutputTokens != 150 {
		t.Errorf("TotalOutputTokens = %d, want 150", usage.TotalOutputTokens)
	}
	// 3 user/assistant messages (user m1, assistant m2, assistant m4)
	if usage.MessageCount != 3 {
		t.Errorf("MessageCount = %d, want 3", usage.MessageCount)
	}
}

func TestCWDCache(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")
	sessionPath := filepath.Join(a.sessionsDir, "basic-session.jsonl")
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	// First call: populates cache
	cwd1, err := a.sessionCWD(sessionPath, info)
	if err != nil {
		t.Fatalf("first sessionCWD: %v", err)
	}
	if cwd1 != "/test/project" {
		t.Errorf("CWD = %q, want /test/project", cwd1)
	}

	// Second call with same info: should use cache (no error, same result)
	cwd2, err := a.sessionCWD(sessionPath, info)
	if err != nil {
		t.Fatalf("second sessionCWD: %v", err)
	}
	if cwd2 != cwd1 {
		t.Errorf("cached CWD = %q, want %q", cwd2, cwd1)
	}

	// Verify cache entry exists
	a.cwdMu.RLock()
	_, cached := a.cwdCache[sessionPath]
	a.cwdMu.RUnlock()
	if !cached {
		t.Error("expected cwdCache entry to exist")
	}
}

func TestSessionsEmptySession(t *testing.T) {
	a := newTestAdapter(t, "empty-session.jsonl")

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}

	// The empty session has 0 messages, so Sessions() skips it (MsgCount == 0 check)
	// This matches the adapter behavior: sessions with no messages are filtered out
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions (empty sessions skipped), got %d", len(sessions))
	}

	// But the metadata should still be parseable
	sessionPath := filepath.Join(a.sessionsDir, "empty-session.jsonl")
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	meta, err := a.sessionMetadata(sessionPath, info)
	if err != nil {
		t.Fatalf("sessionMetadata: %v", err)
	}
	if meta.SessionID != "test-empty" {
		t.Errorf("SessionID = %q, want test-empty", meta.SessionID)
	}
	if meta.MsgCount != 0 {
		t.Errorf("MsgCount = %d, want 0", meta.MsgCount)
	}
	if meta.CWD != "/test/project" {
		t.Errorf("CWD = %q, want /test/project", meta.CWD)
	}
}

func TestCapabilities(t *testing.T) {
	a := New()
	caps := a.Capabilities()
	if !caps[adapter.CapSessions] {
		t.Error("expected sessions capability")
	}
	if !caps[adapter.CapMessages] {
		t.Error("expected messages capability")
	}
	if !caps[adapter.CapUsage] {
		t.Error("expected usage capability")
	}
	if !caps[adapter.CapWatch] {
		t.Error("expected watch capability")
	}
}

func TestIDNameIcon(t *testing.T) {
	a := New()
	if a.ID() != "pi" {
		t.Errorf("ID = %q, want pi", a.ID())
	}
	if a.Name() != "Pi" {
		t.Errorf("Name = %q, want Pi", a.Name())
	}
	if a.Icon() != "\U0001F43E" {
		t.Errorf("Icon = %q, want paw", a.Icon())
	}
}

func TestWatchScope(t *testing.T) {
	a := New()
	if a.WatchScope() != adapter.WatchScopeGlobal {
		t.Error("expected WatchScopeGlobal")
	}
}

func TestSessionMetadataIncremental(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")
	sessionPath := filepath.Join(a.sessionsDir, "basic-session.jsonl")

	// Full parse
	meta1, offset, mc, mt, err := a.parseSessionMetadataFull(sessionPath)
	if err != nil {
		t.Fatalf("full parse: %v", err)
	}
	if meta1.MsgCount != 2 {
		t.Errorf("full parse MsgCount = %d, want 2", meta1.MsgCount)
	}

	// Append a new assistant message
	f, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = f.WriteString(`{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-02-01T00:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":"More info."}],"model":"claude-opus-4-5","usage":{"input":200,"output":50,"cacheRead":0,"cacheWrite":0,"totalTokens":250,"cost":{"total":0.005}}}}` + "\n")
	_ = f.Close()

	// Incremental parse
	meta2, _, _, _, err := a.parseSessionMetadataIncremental(sessionPath, meta1, offset, mc, mt)
	if err != nil {
		t.Fatalf("incremental parse: %v", err)
	}
	if meta2.MsgCount != 3 {
		t.Errorf("incremental MsgCount = %d, want 3", meta2.MsgCount)
	}
	if meta2.TotalTokens != meta1.TotalTokens+250 {
		t.Errorf("incremental TotalTokens = %d, want %d", meta2.TotalTokens, meta1.TotalTokens+250)
	}
	// Head fields preserved
	if meta2.SessionID != meta1.SessionID {
		t.Error("SessionID should be preserved")
	}
	if meta2.FirstMsg != meta1.FirstMsg {
		t.Error("FirstMsg should be preserved")
	}
	if !meta2.LastMsg.After(meta1.LastMsg) {
		t.Error("LastMsg should be updated")
	}
}

func TestSessionMetadataPrimaryModel(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")
	sessionPath := filepath.Join(a.sessionsDir, "basic-session.jsonl")

	meta, _, _, _, err := a.parseSessionMetadataFull(sessionPath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if meta.PrimaryModel != "claude-opus-4-5" {
		t.Errorf("PrimaryModel = %q, want claude-opus-4-5", meta.PrimaryModel)
	}
}

func TestSessionTimestamps(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	s := sessions[0]
	expected := time.Date(2026, 2, 1, 0, 0, 2, 0, time.UTC)
	if !s.CreatedAt.Equal(expected) {
		t.Errorf("CreatedAt = %v, want %v", s.CreatedAt, expected)
	}
	expectedUpdated := time.Date(2026, 2, 1, 0, 0, 3, 0, time.UTC)
	if !s.UpdatedAt.Equal(expectedUpdated) {
		t.Errorf("UpdatedAt = %v, want %v", s.UpdatedAt, expectedUpdated)
	}
}

func TestSessionEstCost(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	// basic-session has cost.total = 0.015
	if sessions[0].EstCost != 0.015 {
		t.Errorf("EstCost = %f, want 0.015", sessions[0].EstCost)
	}
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 50, "short"},
		{"a long title that exceeds the limit", 20, "a long title that..."},
		{"has\nnewlines\r\nin it", 50, "has newlines in it"},
		{"  spaces  ", 50, "spaces"},
	}
	for _, tt := range tests {
		got := truncateTitle(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateTitle(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"12345678", "12345678"},
		{"123456789abcdef", "12345678"},
		{"short", "short"},
		{"", ""},
	}
	for _, tt := range tests {
		got := shortID(tt.id)
		if got != tt.want {
			t.Errorf("shortID(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestClassifySession(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{"cron prefix", "[cron:abc123 daily-backup] Run the daily backup job", adapter.SessionCategoryCron},
		{"system prefix", "System: check disk usage", adapter.SessionCategorySystem},
		{"whatsapp gateway system", "System: [2026-02-01 14:02:05 PST] WhatsApp gateway connected.", adapter.SessionCategoryInteractive},
		{"telegram prefix", "[Telegram user123] Hello there", adapter.SessionCategoryInteractive},
		{"whatsapp prefix", "[WhatsApp user456] Hi", adapter.SessionCategoryInteractive},
		{"plain message", "Hello, what files are here?", adapter.SessionCategoryInteractive},
		{"empty message", "", adapter.SessionCategoryInteractive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifySession(tt.message)
			if got != tt.want {
				t.Errorf("classifySession(%q) = %q, want %q", tt.message, got, tt.want)
			}
		})
	}
}

func TestSessionCategoryInMetadata(t *testing.T) {
	a := newTestAdapter(t, "cron-session.jsonl")
	sessionPath := filepath.Join(a.sessionsDir, "cron-session.jsonl")
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	meta, err := a.sessionMetadata(sessionPath, info)
	if err != nil {
		t.Fatalf("sessionMetadata: %v", err)
	}
	if meta.SessionCategory != adapter.SessionCategoryCron {
		t.Errorf("SessionCategory = %q, want %q", meta.SessionCategory, adapter.SessionCategoryCron)
	}
}

func TestSessionCategoryInSessions(t *testing.T) {
	a := newTestAdapter(t, "cron-session.jsonl")

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionCategory != adapter.SessionCategoryCron {
		t.Errorf("SessionCategory = %q, want %q", sessions[0].SessionCategory, adapter.SessionCategoryCron)
	}
}

func TestExtractCronJobName(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"standard cron", "[cron:9ed048b3-6de7-49f4-8042-7106d14c0fb7 GitHub Issues Check (Sidecar + td)] Check for new open issues", "GitHub Issues Check (Sidecar + td)"},
		{"short cron", "[cron:abc123 daily-backup] Run backup", "daily-backup"},
		{"no closing bracket", "[cron:abc123 daily-backup Run backup", ""},
		{"no space after UUID", "[cron:abc123] Run backup", ""},
		{"empty job name", "[cron:abc123 ] Run backup", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCronJobName(tt.msg)
			if got != tt.want {
				t.Errorf("extractCronJobName(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

func TestDetectSourceChannel(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"telegram", "[Telegram Marcus Vorwaller (@theinfinitecool) id:6776951004 +1m] hello", "telegram"},
		{"whatsapp", "[WhatsApp user456] Hi", "whatsapp"},
		{"direct plain", "Hello, what files are here?", "direct"},
		{"direct empty", "", "direct"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectSourceChannel(tt.msg)
			if got != tt.want {
				t.Errorf("detectSourceChannel(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

func TestExtractSessionMetadata(t *testing.T) {
	tests := []struct {
		name        string
		msg         string
		wantCat     string
		wantCron    string
		wantChannel string
	}{
		{
			"cron session",
			"[cron:9ed048b3 GitHub Issues Check (Sidecar + td)] Check for new issues",
			adapter.SessionCategoryCron, "GitHub Issues Check (Sidecar + td)", "",
		},
		{
			"whatsapp gateway",
			"System: [2026-02-01 14:02:05 PST] WhatsApp gateway connected.",
			adapter.SessionCategoryInteractive, "", "whatsapp",
		},
		{
			"system non-whatsapp",
			"System: check disk usage",
			adapter.SessionCategorySystem, "", "",
		},
		{
			"telegram interactive",
			"[Telegram Marcus (@cool) id:123] hello",
			adapter.SessionCategoryInteractive, "", "telegram",
		},
		{
			"direct interactive",
			"Hello there",
			adapter.SessionCategoryInteractive, "", "direct",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat, cronName, srcChannel := extractSessionMetadata(tt.msg)
			if cat != tt.wantCat {
				t.Errorf("category = %q, want %q", cat, tt.wantCat)
			}
			if cronName != tt.wantCron {
				t.Errorf("cronJobName = %q, want %q", cronName, tt.wantCron)
			}
			if srcChannel != tt.wantChannel {
				t.Errorf("sourceChannel = %q, want %q", srcChannel, tt.wantChannel)
			}
		})
	}
}

func TestCronSessionName(t *testing.T) {
	a := newTestAdapter(t, "cron-session.jsonl")

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	// Cron session name should be the job name, not the raw message
	if sessions[0].Name != "daily-backup" {
		t.Errorf("Name = %q, want %q", sessions[0].Name, "daily-backup")
	}
	if sessions[0].CronJobName != "daily-backup" {
		t.Errorf("CronJobName = %q, want %q", sessions[0].CronJobName, "daily-backup")
	}
}

func TestWhatsAppSessionMetadata(t *testing.T) {
	a := newTestAdapter(t, "whatsapp-session.jsonl")

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SourceChannel != "whatsapp" {
		t.Errorf("SourceChannel = %q, want %q", sessions[0].SourceChannel, "whatsapp")
	}
	if sessions[0].SessionCategory != adapter.SessionCategoryInteractive {
		t.Errorf("SessionCategory = %q, want %q", sessions[0].SessionCategory, adapter.SessionCategoryInteractive)
	}
}

func TestDirectSessionMetadata(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SourceChannel != "direct" {
		t.Errorf("SourceChannel = %q, want %q", sessions[0].SourceChannel, "direct")
	}
}

func TestStripMessagePrefix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"telegram prefix",
			"[Telegram Marcus Vorwaller (@theinfinitecool) id:6776951004 +1m 2026-02-01 14:03 PST] hello from telegram?",
			"hello from telegram?",
		},
		{
			"cron prefix",
			"[cron:9ed048b3-6de7 GitHub Issues Check] Check for new issues",
			"Check for new issues",
		},
		{
			"system prefix",
			"System: [2026-02-01 14:02:05 PST] WhatsApp gateway connected.",
			"WhatsApp gateway connected.",
		},
		{
			"plain message",
			"Hello, what files are here?",
			"Hello, what files are here?",
		},
		{
			"empty message",
			"",
			"",
		},
		{
			"bracket but not matching prefix",
			"[Something else] hello",
			"[Something else] hello",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMessagePrefix(tt.input)
			if got != tt.want {
				t.Errorf("stripMessagePrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractSourceLabel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"telegram with full prefix",
			"[Telegram Marcus Vorwaller (@theinfinitecool) id:6776951004 +1m 2026-02-01 14:03 PST] hello",
			"[TG] Marcus Vorwaller",
		},
		{
			"telegram without parens",
			"[Telegram SomeUser id:123] hello",
			"[TG] SomeUser",
		},
		{
			"cron with job name",
			"[cron:9ed048b3 GitHub Issues Check] Check for new issues",
			"[cron] GitHub Issues Check",
		},
		{
			"cron without job name",
			"[cron:abc123] Run backup",
			"[cron]",
		},
		{
			"system prefix",
			"System: [2026-02-01 14:02:05 PST] WhatsApp gateway connected.",
			"[sys]",
		},
		{
			"plain message",
			"Hello, what files are here?",
			"",
		},
		{
			"empty message",
			"",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSourceLabel(tt.input)
			if got != tt.want {
				t.Errorf("extractSourceLabel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTelegramMessageStripping(t *testing.T) {
	a := newTestAdapter(t, "telegram-session.jsonl")
	populateIndex(t, a, "/test/project")

	messages, err := a.Messages("test-telegram")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// User message should have prefix stripped and source label set
	userMsg := messages[0]
	if userMsg.Role != "user" {
		t.Fatalf("msg[0].Role = %q, want user", userMsg.Role)
	}
	// Content should have the Telegram prefix stripped
	if !strings.HasPrefix(userMsg.Content, "hello from telegram?") {
		t.Errorf("msg[0].Content = %q, want prefix stripped", userMsg.Content)
	}
	if strings.Contains(userMsg.Content, "[Telegram") {
		t.Errorf("msg[0].Content still contains [Telegram prefix: %q", userMsg.Content)
	}
	// SourceLabel should be set
	if userMsg.SourceLabel != "[TG] Marcus Vorwaller" {
		t.Errorf("msg[0].SourceLabel = %q, want %q", userMsg.SourceLabel, "[TG] Marcus Vorwaller")
	}
	// ContentBlocks text should also be stripped
	for _, cb := range userMsg.ContentBlocks {
		if cb.Type == "text" && strings.Contains(cb.Text, "[Telegram") {
			t.Errorf("ContentBlock text still contains prefix: %q", cb.Text)
		}
	}
}

func TestCronMessageStripping(t *testing.T) {
	a := newTestAdapter(t, "cron-session.jsonl")
	populateIndex(t, a, "/test/project")

	messages, err := a.Messages("test-cron")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(messages) < 1 {
		t.Fatal("expected at least 1 message")
	}

	userMsg := messages[0]
	// Content should have the cron prefix stripped
	if strings.HasPrefix(userMsg.Content, "[cron:") {
		t.Errorf("msg[0].Content still has cron prefix: %q", userMsg.Content)
	}
	if userMsg.Content != "Run the daily backup job" {
		t.Errorf("msg[0].Content = %q, want %q", userMsg.Content, "Run the daily backup job")
	}
	// SourceLabel should be set
	if userMsg.SourceLabel != "[cron] daily-backup" {
		t.Errorf("msg[0].SourceLabel = %q, want %q", userMsg.SourceLabel, "[cron] daily-backup")
	}
}

func TestSystemMessageStripping(t *testing.T) {
	a := newTestAdapter(t, "whatsapp-session.jsonl")
	populateIndex(t, a, "/test/project")

	messages, err := a.Messages("test-whatsapp")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(messages) < 1 {
		t.Fatal("expected at least 1 message")
	}

	userMsg := messages[0]
	// Content should have the System: [...] prefix stripped
	if strings.HasPrefix(userMsg.Content, "System: [") {
		t.Errorf("msg[0].Content still has System prefix: %q", userMsg.Content)
	}
	// SourceLabel should be [sys]
	if userMsg.SourceLabel != "[sys]" {
		t.Errorf("msg[0].SourceLabel = %q, want %q", userMsg.SourceLabel, "[sys]")
	}
}

func TestPlainMessageNoStripping(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")
	populateIndex(t, a, "/test/project")

	messages, err := a.Messages("test-basic")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(messages) < 1 {
		t.Fatal("expected at least 1 message")
	}

	userMsg := messages[0]
	// Content should be unchanged for plain messages
	if userMsg.Content != "Hello, what files are here?" {
		t.Errorf("msg[0].Content = %q, want %q", userMsg.Content, "Hello, what files are here?")
	}
	// SourceLabel should be empty for direct messages
	if userMsg.SourceLabel != "" {
		t.Errorf("msg[0].SourceLabel = %q, want empty", userMsg.SourceLabel)
	}
}

func TestSessionCategoryInteractiveDefault(t *testing.T) {
	a := newTestAdapter(t, "basic-session.jsonl")

	sessions, err := a.Sessions("/test/project")
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionCategory != adapter.SessionCategoryInteractive {
		t.Errorf("SessionCategory = %q, want %q", sessions[0].SessionCategory, adapter.SessionCategoryInteractive)
	}
}
