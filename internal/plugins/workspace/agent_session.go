package workspace

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	sessionStatusTailBytes  = 2 * 1024 * 1024
	codexSessionCacheTTL    = 5 * time.Second
	codexCwdCacheMaxEntries = 2048

	// sessionActivityThreshold is how recently a session file must have been modified
	// to consider the agent "active". Research shows: bash_progress entries write every 1s,
	// agent_progress entries every 0.5-3s, but LLM thinking can produce gaps up to 55s with
	// no writes. When mtime is stale, we fall back to JSONL content parsing (td-2fca7d).
	sessionActivityThreshold = 30 * time.Second
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

// isFileRecentlyModified returns true if the file at path was modified within threshold.
func isFileRecentlyModified(path string, threshold time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < threshold
}

// anyFileRecentlyModified returns true if any file with the given suffix in dir
// was modified within threshold. Used to check sub-agent session files.
func anyFileRecentlyModified(dir, suffix string, threshold time.Duration) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) < threshold {
			return true
		}
	}
	return false
}

// detectAnyAgentSessionStatus tries all known agent types against a worktree path,
// returning the status and agent type for the first match. Used for detecting externally
// started agents (e.g., in the main worktree) where the agent type isn't known in advance.
// Each per-agent detection function short-circuits quickly when session files don't exist.
func detectAnyAgentSessionStatus(worktreePath string) (WorktreeStatus, AgentType, bool) {
	// Order: most common agents first for fast short-circuit
	candidates := []AgentType{AgentClaude, AgentCodex, AgentGemini, AgentOpenCode, AgentPi}
	for _, agent := range candidates {
		if status, ok := detectAgentSessionStatus(agent, worktreePath); ok {
			return status, agent, true
		}
	}
	return 0, "", false
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

// claudeProjectDirName encodes an absolute path into Claude Code's project directory name.
// Claude Code replaces slashes, underscores, and other non-alphanumeric characters with dashes.
// e.g., /Users/foo/my_project becomes -Users-foo-my-project
func claudeProjectDirName(absPath string) string {
	var b strings.Builder
	b.Grow(len(absPath))
	for _, r := range absPath {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// detectClaudeSessionStatus checks Claude session files using mtime + JSONL fallback.
// Claude stores sessions in ~/.claude/projects/{path-with-dashes}/*.jsonl
// Sub-agent sessions in {session-uuid}/subagents/agent-*.jsonl
//
// Detection strategy (td-2fca7d):
//  1. If main session or any sub-agent file was recently modified → active
//  2. Otherwise, fall back to JSONL content: last user entry → active (thinking),
//     last assistant entry → waiting (idle)
func detectClaudeSessionStatus(worktreePath string) (WorktreeStatus, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, false
	}

	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		return 0, false
	}

	// Claude Code encodes the project path by replacing non-alphanumeric chars with dashes (td-2fca7d).
	projectDirName := claudeProjectDirName(absPath)
	projectDir := filepath.Join(home, ".claude", "projects", projectDirName)

	// Get session files sorted by mtime (most recent first).
	// We iterate candidates because the most recent file may be abandoned
	// (e.g., only file-history-snapshot entries with no user/assistant messages).
	sessionFiles, err := findRecentJSONLFiles(projectDir, "agent-")
	if err != nil || len(sessionFiles) == 0 {
		slog.Debug("claude session: no session file found", "projectDir", projectDir, "err", err)
		return 0, false
	}

	for _, sessionFile := range sessionFiles {
		// Fast path: if the main session file was recently modified, agent is active.
		if isFileRecentlyModified(sessionFile, sessionActivityThreshold) {
			slog.Debug("claude session: active (main file mtime)", "file", filepath.Base(sessionFile))
			return StatusActive, true
		}

		// Check sub-agent files: main session stops receiving agent_progress entries
		// 20-45s before sub-agents finish, but sub-agent files continue being written
		// (e.g., bash_progress every 1s during command execution).
		sessionUUID := strings.TrimSuffix(filepath.Base(sessionFile), ".jsonl")
		subagentsDir := filepath.Join(projectDir, sessionUUID, "subagents")
		if anyFileRecentlyModified(subagentsDir, ".jsonl", sessionActivityThreshold) {
			slog.Debug("claude session: active (sub-agent file mtime)", "file", filepath.Base(sessionFile))
			return StatusActive, true
		}

		// Slow path: all files are stale. Fall back to JSONL content to distinguish
		// "thinking" (last entry = user, agent is generating) from "idle" (last entry = assistant).
		status, ok := getLastMessageStatusJSONL(sessionFile, "type", "user", "assistant")
		if ok {
			slog.Debug("claude session: status from JSONL fallback", "status", status, "file", filepath.Base(sessionFile))
			return status, true
		}
		// No user/assistant entry found (abandoned session) — try next candidate (td-2fca7d v8).
		slog.Debug("claude session: skipping abandoned file", "file", filepath.Base(sessionFile))
	}

	slog.Debug("claude session: no valid session file found", "projectDir", projectDir, "candidates", len(sessionFiles))
	return 0, false
}

// detectCodexSessionStatus checks Codex session files using mtime + JSONL fallback.
// Codex stores sessions in ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl with CWD field.
// Codex has no sub-agents — all activity is recorded in one file per session.
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

	// Fast path: recently modified file means agent is active
	if isFileRecentlyModified(sessionFile, sessionActivityThreshold) {
		return StatusActive, true
	}

	// Slow path: fall back to JSONL content parsing
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

	storageDir := findOpenCodeStorage(home)

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
	files, err := findRecentJSONLFiles(dir, excludePrefix)
	if err != nil || len(files) == 0 {
		return "", err
	}
	return files[0], nil
}

// findRecentJSONLFiles returns .jsonl files in dir sorted by mtime descending.
// Used to iterate session candidates when the most recent file is abandoned (td-2fca7d).
func findRecentJSONLFiles(dir string, excludePrefix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	type fileEntry struct {
		path    string
		modTime int64
	}
	var files []fileEntry

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if excludePrefix != "" && strings.HasPrefix(e.Name(), excludePrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{
			path:    filepath.Join(dir, e.Name()),
			modTime: info.ModTime().UnixNano(),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime > files[j].modTime
	})

	result := make([]string, len(files))
	for i, f := range files {
		result[i] = f.path
	}
	return result, nil
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
	defer func() { _ = file.Close() }()

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

// getLastMessageStatusJSONL reads JSONL file and returns status based on the last
// user or assistant message. Used as a fallback when mtime-based detection is
// inconclusive (file is stale but agent may be thinking).
// Returns StatusActive if last significant entry is from user (agent is thinking).
// Returns StatusWaiting if last significant entry is from assistant (agent is idle).
// All other entry types (system, progress, file-history-snapshot) are skipped.
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
		msgType, ok := msg[typeField].(string)
		if !ok {
			continue
		}
		switch msgType {
		case userVal:
			return StatusActive, true
		case assistantVal:
			return StatusWaiting, true
		}
	}
	return 0, false
}

// findCodexSessionForPath finds the most recent Codex session matching CWD.
// Codex stores sessions in a YYYY/MM/DD/ date hierarchy under sessionsDir,
// so we walk the directory tree to find all .jsonl files (td-2fca7d).
func findCodexSessionForPath(sessionsDir, worktreePath string) (string, error) {
	if cached, ok := cachedCodexSessionPath(sessionsDir, worktreePath); ok {
		return cached, nil
	}

	var bestPath string
	var bestModTime int64

	_ = filepath.WalkDir(sessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Check if CWD matches
		cwd, err := getCodexSessionCWD(path, info)
		if err != nil || !cwdMatches(cwd, worktreePath) {
			return nil
		}

		modTime := info.ModTime().UnixNano()
		if modTime > bestModTime {
			bestModTime = modTime
			bestPath = path
		}
		return nil
	})

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
	defer func() { _ = file.Close() }()

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

// findOpenCodeStorage searches candidate paths for the OpenCode storage directory.
func findOpenCodeStorage(home string) string {
	var candidates []string

	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates, filepath.Join(home, "Library", "Application Support", "opencode", "storage"))
	case "linux":
		xdgData := os.Getenv("XDG_DATA_HOME")
		if xdgData == "" {
			xdgData = filepath.Join(home, ".local", "share")
		}
		candidates = append(candidates, filepath.Join(xdgData, "opencode", "storage"))
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			candidates = append(candidates, filepath.Join(localAppData, "opencode", "Data", "storage"))
		}
	}

	defaultPath := filepath.Join(home, ".local", "share", "opencode", "storage")
	if len(candidates) == 0 || candidates[len(candidates)-1] != defaultPath {
		candidates = append(candidates, defaultPath)
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return defaultPath
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
