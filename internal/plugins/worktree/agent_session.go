package worktree

import (
	"bufio"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	sessionStatusTailBytes  = 2 * 1024 * 1024
	codexSessionCacheTTL    = 5 * time.Second
	codexCwdCacheMaxEntries = 2048
)

type codexSessionCacheEntry struct {
	sessionPath string
	expiresAt   time.Time
}

type codexSessionCwdCacheEntry struct {
	cwd        string
	modTime    time.Time
	size       int64
	lastAccess time.Time
}

var codexSessionCache = struct {
	mu      sync.Mutex
	entries map[string]codexSessionCacheEntry
}{
	entries: make(map[string]codexSessionCacheEntry),
}

var codexSessionCwdCache = struct {
	mu      sync.Mutex
	entries map[string]codexSessionCwdCacheEntry
}{
	entries: make(map[string]codexSessionCwdCacheEntry),
}

// detectAgentSessionStatus checks agent session files to determine if an agent
// is waiting for user input or actively processing.
// Returns StatusWaiting if last message is from assistant (agent finished, waiting for user).
// Returns StatusActive if last message is from user (agent is processing response).
// Returns (0, false) if unable to determine status.
func detectAgentSessionStatus(agentType AgentType, worktreePath string) (WorktreeStatus, bool) {
	switch agentType {
	case AgentClaude:
		return detectClaudeSessionStatus(worktreePath)
	case AgentCodex:
		return detectCodexSessionStatus(worktreePath)
	case AgentGemini:
		return detectGeminiSessionStatus(worktreePath)
	case AgentOpenCode:
		return detectOpenCodeSessionStatus(worktreePath)
	case AgentCursor:
		return detectCursorSessionStatus(worktreePath)
	default:
		return 0, false
	}
}

// detectClaudeSessionStatus checks Claude session files.
// Claude stores sessions in ~/.claude/projects/{path-with-dashes}/*.jsonl
// Path format: /Users/foo/code/project becomes -Users-foo-code-project
func detectClaudeSessionStatus(worktreePath string) (WorktreeStatus, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, false
	}

	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		return 0, false
	}

	// Claude Code uses path with slashes replaced by dashes
	// e.g., /Users/foo/code/project becomes -Users-foo-code-project
	pathWithDashes := strings.ReplaceAll(absPath, "/", "-")
	projectDir := filepath.Join(home, ".claude", "projects", pathWithDashes)

	// Find most recent main session file (UUID-based, not agent-* prefixed)
	// Agent subprocesses create agent-* files; we want the main session
	sessionFile, err := findMostRecentJSONL(projectDir, "agent-")
	if err != nil || sessionFile == "" {
		return 0, false
	}

	return getLastMessageStatusJSONL(sessionFile, "type", "user", "assistant")
}

// detectCodexSessionStatus checks Codex session files.
// Codex stores sessions in ~/.codex/sessions/*.jsonl with CWD field.
func detectCodexSessionStatus(worktreePath string) (WorktreeStatus, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, false
	}

	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		return 0, false
	}

	sessionsDir := filepath.Join(home, ".codex", "sessions")

	// Find most recent session file that matches the worktree path
	sessionFile, err := findCodexSessionForPath(sessionsDir, absPath)
	if err != nil || sessionFile == "" {
		return 0, false
	}

	return getCodexLastMessageStatus(sessionFile)
}

// detectGeminiSessionStatus checks Gemini CLI session files.
// Gemini stores sessions in ~/.gemini/tmp/{sha256-hash}/chats/session-*.json
func detectGeminiSessionStatus(worktreePath string) (WorktreeStatus, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, false
	}

	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		return 0, false
	}

	// SHA256 hash of absolute path
	hash := sha256.Sum256([]byte(absPath))
	pathHash := hex.EncodeToString(hash[:])
	chatsDir := filepath.Join(home, ".gemini", "tmp", pathHash, "chats")

	sessionFile, err := findMostRecentJSON(chatsDir, "session-")
	if err != nil || sessionFile == "" {
		return 0, false
	}

	return getGeminiLastMessageStatus(sessionFile)
}

// detectOpenCodeSessionStatus checks OpenCode session files.
// OpenCode stores in ~/.local/share/opencode/storage/ with project/session/message dirs.
func detectOpenCodeSessionStatus(worktreePath string) (WorktreeStatus, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, false
	}

	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		return 0, false
	}

	storageDir := filepath.Join(home, ".local", "share", "opencode", "storage")

	// Find project matching worktree path
	projectID, err := findOpenCodeProject(storageDir, absPath)
	if err != nil || projectID == "" {
		return 0, false
	}

	// Find most recent session for project
	sessionID, err := findOpenCodeSession(storageDir, projectID)
	if err != nil || sessionID == "" {
		return 0, false
	}

	// Find last message in session
	return getOpenCodeLastMessageStatus(storageDir, sessionID)
}

// detectCursorSessionStatus checks Cursor session files.
// Cursor stores in ~/.cursor/chats/{md5-hash}/{sessionID}/store.db (SQLite).
// For simplicity, we skip SQLite parsing and return false.
func detectCursorSessionStatus(worktreePath string) (WorktreeStatus, bool) {
	// Cursor uses SQLite which requires database/sql and a driver.
	// For now, skip Cursor session detection to avoid adding dependencies.
	// Tmux pattern detection should still work for Cursor.
	return 0, false
}

func codexSessionCacheKey(sessionsDir, worktreePath string) string {
	return sessionsDir + "\n" + worktreePath
}

func cachedCodexSessionPath(sessionsDir, worktreePath string) (string, bool) {
	key := codexSessionCacheKey(sessionsDir, worktreePath)
	now := time.Now()

	codexSessionCache.mu.Lock()
	entry, ok := codexSessionCache.entries[key]
	codexSessionCache.mu.Unlock()

	if !ok {
		return "", false
	}
	if now.After(entry.expiresAt) {
		codexSessionCache.mu.Lock()
		delete(codexSessionCache.entries, key)
		codexSessionCache.mu.Unlock()
		return "", false
	}
	if entry.sessionPath == "" {
		return "", true
	}
	if _, err := os.Stat(entry.sessionPath); err == nil {
		return entry.sessionPath, true
	}
	codexSessionCache.mu.Lock()
	delete(codexSessionCache.entries, key)
	codexSessionCache.mu.Unlock()
	return "", false
}

func setCachedCodexSessionPath(sessionsDir, worktreePath, sessionPath string) {
	key := codexSessionCacheKey(sessionsDir, worktreePath)
	codexSessionCache.mu.Lock()
	codexSessionCache.entries[key] = codexSessionCacheEntry{
		sessionPath: sessionPath,
		expiresAt:   time.Now().Add(codexSessionCacheTTL),
	}
	codexSessionCache.mu.Unlock()
}

func cachedCodexSessionCWD(path string, info os.FileInfo) (string, bool) {
	codexSessionCwdCache.mu.Lock()
	entry, ok := codexSessionCwdCache.entries[path]
	if ok && entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
		entry.lastAccess = time.Now()
		codexSessionCwdCache.entries[path] = entry
		codexSessionCwdCache.mu.Unlock()
		return entry.cwd, true
	}
	if ok {
		delete(codexSessionCwdCache.entries, path)
	}
	codexSessionCwdCache.mu.Unlock()
	return "", false
}

func setCodexSessionCWDCache(path string, info os.FileInfo, cwd string) {
	codexSessionCwdCache.mu.Lock()
	codexSessionCwdCache.entries[path] = codexSessionCwdCacheEntry{
		cwd:        cwd,
		modTime:    info.ModTime(),
		size:       info.Size(),
		lastAccess: time.Now(),
	}
	pruneCodexSessionCWDCacheLocked()
	codexSessionCwdCache.mu.Unlock()
}

func pruneCodexSessionCWDCacheLocked() {
	if len(codexSessionCwdCache.entries) <= codexCwdCacheMaxEntries {
		return
	}
	type cacheEntry struct {
		path       string
		lastAccess time.Time
	}
	entries := make([]cacheEntry, 0, len(codexSessionCwdCache.entries))
	for path, entry := range codexSessionCwdCache.entries {
		entries = append(entries, cacheEntry{path: path, lastAccess: entry.lastAccess})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastAccess.Before(entries[j].lastAccess)
	})
	excess := len(entries) - codexCwdCacheMaxEntries
	for i := 0; i < excess; i++ {
		delete(codexSessionCwdCache.entries, entries[i].path)
	}
}

// findMostRecentJSONL finds most recent .jsonl file in dir.
// excludePrefix: if non-empty, files starting with this prefix are skipped.
func findMostRecentJSONL(dir string, excludePrefix string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var mostRecent string
	var mostRecentTime int64

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		// Skip files matching the exclude prefix (e.g., "agent-" subagent files)
		if excludePrefix != "" && strings.HasPrefix(e.Name(), excludePrefix) {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		modTime := info.ModTime().UnixNano()
		if modTime > mostRecentTime {
			mostRecentTime = modTime
			mostRecent = filepath.Join(dir, e.Name())
		}
	}

	return mostRecent, nil
}

// findMostRecentJSON finds most recent .json file with given prefix.
func findMostRecentJSON(dir string, prefix string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var mostRecent string
	var mostRecentTime int64

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if prefix != "" && !strings.HasPrefix(e.Name(), prefix) {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		modTime := info.ModTime().UnixNano()
		if modTime > mostRecentTime {
			mostRecentTime = modTime
			mostRecent = filepath.Join(dir, e.Name())
		}
	}

	return mostRecent, nil
}

// readTailLines reads up to maxBytes from the end of a file and returns lines.
// If the read starts mid-line, the first partial line is dropped.
func readTailLines(path string, maxBytes int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size == 0 {
		return nil, nil
	}

	start := int64(0)
	if size > int64(maxBytes) {
		start = size - int64(maxBytes)
	}
	if start > 0 {
		if _, err := file.Seek(start, io.SeekStart); err != nil {
			return nil, err
		}
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	return lines, nil
}

// getLastMessageStatusJSONL reads JSONL file and returns status based on last message.
func getLastMessageStatusJSONL(path, typeField, userVal, assistantVal string) (WorktreeStatus, bool) {
	lines, err := readTailLines(path, sessionStatusTailBytes)
	if err != nil {
		return 0, false
	}

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msgType, ok := msg[typeField].(string); ok {
			switch msgType {
			case assistantVal:
				return StatusWaiting, true
			case userVal:
				return StatusActive, true
			}
		}
	}
	return 0, false
}

// findCodexSessionForPath finds the most recent Codex session matching CWD.
func findCodexSessionForPath(sessionsDir, worktreePath string) (string, error) {
	if cached, ok := cachedCodexSessionPath(sessionsDir, worktreePath); ok {
		return cached, nil
	}

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return "", err
	}
	var bestPath string
	var bestModTime int64

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}

		path := filepath.Join(sessionsDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}

		// Check if CWD matches
		cwd, err := getCodexSessionCWD(path, info)
		if err != nil || !cwdMatches(cwd, worktreePath) {
			continue
		}

		modTime := info.ModTime().UnixNano()
		if modTime > bestModTime {
			bestModTime = modTime
			bestPath = path
		}
	}

	if bestPath == "" {
		setCachedCodexSessionPath(sessionsDir, worktreePath, "")
		return "", nil
	}

	setCachedCodexSessionPath(sessionsDir, worktreePath, bestPath)
	return bestPath, nil
}

// getCodexSessionCWD extracts CWD from first session_meta record.
func getCodexSessionCWD(path string, info os.FileInfo) (string, error) {
	if cached, ok := cachedCodexSessionCWD(path, info); ok {
		return cached, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var record struct {
			Type    string `json:"type"`
			Payload struct {
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if record.Type == "session_meta" && record.Payload.CWD != "" {
			setCodexSessionCWDCache(path, info, record.Payload.CWD)
			return record.Payload.CWD, nil
		}
	}
	return "", nil
}

// cwdMatches checks if cwd matches or is under worktreePath.
func cwdMatches(cwd, worktreePath string) bool {
	cwd = filepath.Clean(cwd)
	worktreePath = filepath.Clean(worktreePath)
	return cwd == worktreePath || strings.HasPrefix(cwd, worktreePath+string(filepath.Separator))
}

// getCodexLastMessageStatus reads Codex JSONL and finds last message role.
func getCodexLastMessageStatus(path string) (WorktreeStatus, bool) {
	lines, err := readTailLines(path, sessionStatusTailBytes)
	if err != nil {
		return 0, false
	}

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var record struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Role string `json:"role"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		// Codex uses type="response_item" with payload.type="message"
		if record.Type == "response_item" && record.Payload.Type == "message" {
			switch record.Payload.Role {
			case "assistant":
				return StatusWaiting, true
			case "user":
				return StatusActive, true
			}
		}
	}
	return 0, false
}

// getGeminiLastMessageStatus reads Gemini JSON session file.
func getGeminiLastMessageStatus(path string) (WorktreeStatus, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}

	var session struct {
		Messages []struct {
			Type string `json:"type"` // "user", "gemini", "info"
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return 0, false
	}

	// Find last user/gemini message
	var lastType string
	for _, msg := range session.Messages {
		if msg.Type == "user" || msg.Type == "gemini" {
			lastType = msg.Type
		}
	}

	switch lastType {
	case "gemini": // gemini = assistant
		return StatusWaiting, true
	case "user":
		return StatusActive, true
	default:
		return 0, false
	}
}

// findOpenCodeProject finds project ID matching worktree path.
func findOpenCodeProject(storageDir, worktreePath string) (string, error) {
	projectDir := filepath.Join(storageDir, "project")
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		path := filepath.Join(projectDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var project struct {
			ID       string `json:"id"`
			Worktree string `json:"worktree"`
		}
		if err := json.Unmarshal(data, &project); err != nil {
			continue
		}

		if cwdMatches(project.Worktree, worktreePath) {
			return project.ID, nil
		}
	}
	return "", nil
}

// findOpenCodeSession finds most recent session for project.
func findOpenCodeSession(storageDir, projectID string) (string, error) {
	sessionDir := filepath.Join(storageDir, "session", projectID)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return "", err
	}

	var mostRecent string
	var mostRecentTime int64

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		modTime := info.ModTime().UnixNano()
		if modTime > mostRecentTime {
			mostRecentTime = modTime
			mostRecent = strings.TrimSuffix(e.Name(), ".json")
		}
	}

	return mostRecent, nil
}

// getOpenCodeLastMessageStatus finds last message role in OpenCode session.
func getOpenCodeLastMessageStatus(storageDir, sessionID string) (WorktreeStatus, bool) {
	messageDir := filepath.Join(storageDir, "message", sessionID)
	entries, err := os.ReadDir(messageDir)
	if err != nil {
		return 0, false
	}

	// Find most recent message file
	var mostRecent string
	var mostRecentTime int64

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		modTime := info.ModTime().UnixNano()
		if modTime > mostRecentTime {
			mostRecentTime = modTime
			mostRecent = filepath.Join(messageDir, e.Name())
		}
	}

	if mostRecent == "" {
		return 0, false
	}

	data, err := os.ReadFile(mostRecent)
	if err != nil {
		return 0, false
	}

	var msg struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return 0, false
	}

	switch msg.Role {
	case "assistant":
		return StatusWaiting, true
	case "user":
		return StatusActive, true
	default:
		return 0, false
	}
}

// Unused but keeping for potential future Cursor support
var _ = md5.Sum // md5 import used for Cursor hash (currently disabled)
