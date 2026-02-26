package tieredwatcher

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/marcus/sidecar/internal/adapter"
)

const (
	// ColdPollInterval is how often COLD tier sessions are polled for changes.
	ColdPollInterval = 30 * time.Second
	// HotInactivityTimeout demotes sessions to COLD after this period without activity.
	HotInactivityTimeout = 5 * time.Minute
	// FrozenThreshold is the duration after which unchanged COLD sessions stop being polled.
	FrozenThreshold = 24 * time.Hour
)

// SessionInfo tracks a watched session's path and modification time.
type SessionInfo struct {
	ID       string    // Session ID (e.g., filename without extension)
	Path     string    // Full path to session file
	ModTime  time.Time // Last known modification time
	LastHot  time.Time // When this session was last in HOT tier or accessed
	FileSize int64     // Last known file size
	Frozen   bool      // true = unchanged >24h, skip in pollColdSessions
}

// TieredWatcher manages tiered watching for a single adapter's sessions.
type TieredWatcher struct {
	mu sync.Mutex

	// Session tracking
	sessions  map[string]*SessionInfo // session ID -> info
	pathIndex map[string]string       // path -> session ID (for fast lookups)
	hotIDs    []string                // session IDs currently in HOT tier
	hotTarget int                     // desired HOT session count

	// fsnotify watcher for HOT tier (watches directory, not individual files)
	watcher   *fsnotify.Watcher
	watchDirs map[string]bool // directories being watched
	rootDirs  map[string]bool // directories that should stay watched
	knownDirs map[string]bool // directories with registered sessions

	// Polling for COLD tier
	pollTicker *time.Ticker
	pollDone   chan struct{}

	// Output channel
	events chan adapter.Event
	closed bool

	// Configuration
	rootDir     string                                  // Root directory to watch
	filePattern string                                  // File extension pattern (e.g., ".jsonl")
	extractID   func(path string) string                // Extract session ID from path
	scanDir     func(dir string) ([]SessionInfo, error) // Scan directory for sessions
	filter      func(path string) bool                  // Optional filter for watched paths
}

// Config holds configuration for creating a TieredWatcher.
type Config struct {
	// RootDir is the root directory to watch
	RootDir string
	// FilePattern is the file extension to watch (e.g., ".jsonl")
	FilePattern string
	// ExtractID extracts session ID from a file path
	ExtractID func(path string) string
	// ScanDir scans a directory and returns session info (optional, for COLD tier)
	ScanDir func(dir string) ([]SessionInfo, error)
	// Filter optionally filters watched paths (overrides FilePattern if set)
	Filter func(path string) bool
}

// New creates a new TieredWatcher.
func New(cfg Config) (*TieredWatcher, <-chan adapter.Event, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, err
	}

	tw := &TieredWatcher{
		sessions:    make(map[string]*SessionInfo),
		pathIndex:   make(map[string]string),
		hotIDs:      make([]string, 0),
		hotTarget:   0,
		watcher:     watcher,
		watchDirs:   make(map[string]bool),
		rootDirs:    make(map[string]bool),
		knownDirs:   make(map[string]bool),
		events:      make(chan adapter.Event, 32),
		rootDir:     cfg.RootDir,
		filePattern: cfg.FilePattern,
		extractID:   cfg.ExtractID,
		scanDir:     cfg.ScanDir,
		filter:      cfg.Filter,
	}

	// Watch the root directory if provided
	if cfg.RootDir != "" {
		if err := watcher.Add(cfg.RootDir); err != nil {
			_ = watcher.Close()
			return nil, nil, err
		}
		tw.watchDirs[cfg.RootDir] = true
		tw.rootDirs[cfg.RootDir] = true
		tw.knownDirs[cfg.RootDir] = true
	}

	// Start background goroutines
	tw.pollDone = make(chan struct{})
	tw.pollTicker = time.NewTicker(ColdPollInterval)

	go tw.watchLoop()
	go tw.pollLoop()
	go tw.demotionLoop()

	return tw, tw.events, nil
}

// PromoteToHot promotes a session to the HOT tier (e.g., when user selects it).
func (tw *TieredWatcher) PromoteToHot(sessionID string) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	info, ok := tw.sessions[sessionID]
	if !ok {
		return
	}
	info.LastHot = time.Now()
	info.Frozen = false
	if tw.hotTarget < 1 {
		tw.hotTarget = 1
	}

	// Check if already in HOT tier
	if !tw.isHotLocked(sessionID) {
		// Add to HOT tier
		tw.hotIDs = append(tw.hotIDs, sessionID)
	}

	tw.trimToHotTargetLocked()
	tw.syncHotDirsLocked()
}

// RegisterSession adds a session to tracking (starts in COLD tier).
func (tw *TieredWatcher) RegisterSession(id, path string) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.sessions[id] != nil {
		return // Already registered
	}

	info := &SessionInfo{
		ID:   id,
		Path: path,
	}

	// Get initial file info
	if stat, err := os.Stat(path); err == nil {
		info.ModTime = stat.ModTime()
		info.FileSize = stat.Size()
		if time.Since(info.ModTime) > FrozenThreshold {
			info.Frozen = true
		}
	}

	tw.sessions[id] = info
	tw.pathIndex[path] = id
	tw.knownDirs[filepath.Dir(path)] = true
	if tw.hotTarget > 0 {
		tw.rebuildHotSetLocked()
	}
}

// RegisterSessions adds multiple sessions to tracking.
func (tw *TieredWatcher) RegisterSessions(sessions []SessionInfo) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	for _, s := range sessions {
		if tw.sessions[s.ID] != nil {
			continue
		}
		info := &SessionInfo{
			ID:       s.ID,
			Path:     s.Path,
			ModTime:  s.ModTime,
			LastHot:  s.LastHot,
			FileSize: s.FileSize,
		}
		if time.Since(s.ModTime) > FrozenThreshold {
			info.Frozen = true
		}
		tw.sessions[s.ID] = info
		tw.pathIndex[s.Path] = s.ID
		tw.knownDirs[filepath.Dir(s.Path)] = true
	}

	tw.rebuildHotSetLocked()
}

// SetHotTarget sets the desired number of HOT sessions and rebuilds the HOT set.
func (tw *TieredWatcher) SetHotTarget(target int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.hotTarget = target
	tw.rebuildHotSetLocked()
}

// rebuildHotSetLocked rebuilds the HOT set based on recent activity.
// Must be called with tw.mu held.
func (tw *TieredWatcher) rebuildHotSetLocked() {
	if len(tw.sessions) == 0 || tw.hotTarget <= 0 {
		tw.hotIDs = nil
		tw.syncHotDirsLocked()
		return
	}

	target := tw.hotTarget
	if target > len(tw.sessions) {
		target = len(tw.sessions)
	}

	type sessionActivity struct {
		id   string
		when time.Time
	}
	sorted := make([]sessionActivity, 0, len(tw.sessions))
	for id, info := range tw.sessions {
		sorted = append(sorted, sessionActivity{id: id, when: tw.activityTime(info)})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].when.After(sorted[j].when)
	})

	tw.hotIDs = tw.hotIDs[:0]
	for i := 0; i < target && i < len(sorted); i++ {
		id := sorted[i].id
		info := tw.sessions[id]
		if info.LastHot.IsZero() {
			info.LastHot = info.ModTime
		}
		tw.hotIDs = append(tw.hotIDs, id)
	}

	tw.syncHotDirsLocked()
}

// demoteOldestLocked removes the oldest session from HOT tier.
// Must be called with tw.mu held.
func (tw *TieredWatcher) demoteOldestLocked() {
	if len(tw.hotIDs) == 0 {
		return
	}

	// Find oldest by activity time
	oldestIdx := 0
	oldestTime := time.Now()
	for i, id := range tw.hotIDs {
		if info, ok := tw.sessions[id]; ok && tw.activityTime(info).Before(oldestTime) {
			oldestTime = tw.activityTime(info)
			oldestIdx = i
		}
	}

	// Remove from HOT tier
	tw.hotIDs = append(tw.hotIDs[:oldestIdx], tw.hotIDs[oldestIdx+1:]...)
}

func (tw *TieredWatcher) trimToHotTargetLocked() {
	if tw.hotTarget <= 0 {
		tw.hotIDs = nil
		return
	}
	for len(tw.hotIDs) > tw.hotTarget {
		tw.demoteOldestLocked()
	}
}

func (tw *TieredWatcher) isHotLocked(sessionID string) bool {
	for _, id := range tw.hotIDs {
		if id == sessionID {
			return true
		}
	}
	return false
}

func (tw *TieredWatcher) activityTime(info *SessionInfo) time.Time {
	if info.LastHot.IsZero() {
		return info.ModTime
	}
	return info.LastHot
}

func (tw *TieredWatcher) noteActivityLocked(sessionID string) {
	info := tw.sessions[sessionID]
	if info == nil {
		return
	}
	info.LastHot = time.Now()
	if tw.hotTarget <= 0 {
		return
	}
	if !tw.isHotLocked(sessionID) {
		tw.hotIDs = append(tw.hotIDs, sessionID)
	}
	tw.trimToHotTargetLocked()
	tw.syncHotDirsLocked()
}

// watchLoop handles fsnotify events for HOT tier sessions.
func (tw *TieredWatcher) watchLoop() {
	var debounceTimer *time.Timer
	var lastPath string
	debounceDelay := 100 * time.Millisecond

	var closed bool
	var mu sync.Mutex

	defer func() {
		mu.Lock()
		closed = true
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		mu.Unlock()
	}()

	for {
		select {
		case event, ok := <-tw.watcher.Events:
			if !ok {
				return
			}

			// Check if this is a file we care about
			if tw.filter != nil {
				if !tw.filter(event.Name) {
					continue
				}
			} else if tw.filePattern != "" && filepath.Ext(event.Name) != tw.filePattern {
				continue
			}

			mu.Lock()
			lastPath = event.Name
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			capturedEvent := event
			debounceTimer = time.AfterFunc(debounceDelay, func() {
				mu.Lock()
				defer mu.Unlock()
				if closed {
					return
				}

				tw.mu.Lock()
				sessionID := tw.pathIndex[lastPath]
				if sessionID == "" && tw.extractID != nil {
					sessionID = tw.extractID(lastPath)
				}
				info := tw.sessions[sessionID]

				// Update mod time if this is a known session
				if info != nil {
					if stat, err := os.Stat(lastPath); err == nil {
						info.ModTime = stat.ModTime()
						info.FileSize = stat.Size()
					}
					tw.noteActivityLocked(sessionID)
				}
				tw.mu.Unlock()

				var eventType adapter.EventType
				switch {
				case capturedEvent.Op&fsnotify.Create != 0:
					eventType = adapter.EventSessionCreated
				case capturedEvent.Op&fsnotify.Write != 0:
					eventType = adapter.EventMessageAdded
				case capturedEvent.Op&fsnotify.Remove != 0:
					return // Skip delete events
				default:
					eventType = adapter.EventSessionUpdated
				}

				select {
				case tw.events <- adapter.Event{
					Type:      eventType,
					SessionID: sessionID,
				}:
				default:
					// Channel full
				}
			})
			mu.Unlock()

		case _, ok := <-tw.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// pollLoop periodically checks COLD tier sessions for changes.
func (tw *TieredWatcher) pollLoop() {
	for {
		select {
		case <-tw.pollTicker.C:
			tw.pollColdSessions()
		case <-tw.pollDone:
			return
		}
	}
}

// pollColdSessions checks non-frozen COLD tier sessions for changes using batch ReadDir.
func (tw *TieredWatcher) pollColdSessions() {
	tw.mu.Lock()
	hotSet := make(map[string]bool, len(tw.hotIDs))
	for _, id := range tw.hotIDs {
		hotSet[id] = true
	}

	// Collect non-frozen COLD sessions to check, grouped by directory
	type checkInfo struct {
		id   string
		path string
		prev time.Time
		size int64
	}
	dirSessions := make(map[string][]checkInfo) // dir -> sessions in that dir
	for id, info := range tw.sessions {
		if !hotSet[id] && !info.Frozen {
			dir := filepath.Dir(info.Path)
			dirSessions[dir] = append(dirSessions[dir], checkInfo{
				id:   id,
				path: info.Path,
				prev: info.ModTime,
				size: info.FileSize,
			})
		}
	}
	tw.mu.Unlock()

	// Batch check: one ReadDir per directory instead of one Stat per file
	for dir, sessions := range dirSessions {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		// Build lookup map: filename -> DirEntry
		entryMap := make(map[string]os.DirEntry, len(entries))
		for _, e := range entries {
			entryMap[e.Name()] = e
		}

		for _, c := range sessions {
			entry, ok := entryMap[filepath.Base(c.path)]
			if !ok {
				// File no longer on disk — freeze so we stop polling it
				tw.mu.Lock()
				if info := tw.sessions[c.id]; info != nil {
					info.Frozen = true
				}
				tw.mu.Unlock()
				continue
			}
			fi, err := entry.Info()
			if err != nil {
				continue
			}

			if fi.ModTime().After(c.prev) || fi.Size() != c.size {
				// File changed — update and emit event
				tw.mu.Lock()
				if info := tw.sessions[c.id]; info != nil {
					info.ModTime = fi.ModTime()
					info.FileSize = fi.Size()
					info.Frozen = false
					tw.noteActivityLocked(c.id)
				}
				tw.mu.Unlock()

				select {
				case tw.events <- adapter.Event{
					Type:      adapter.EventSessionUpdated,
					SessionID: c.id,
				}:
				default:
				}
			} else {
				// Unchanged — freeze check uses current info under lock
				tw.mu.Lock()
				if info := tw.sessions[c.id]; info != nil {
					if time.Since(info.ModTime) > FrozenThreshold {
						info.Frozen = true
					}
				}
				tw.mu.Unlock()
			}
		}
	}

	// Look for new sessions in known directories (optional)
	tw.scanForNewSessions()
}

// scanForNewSessions discovers new sessions in known directories.
// It only runs when a scanDir function is provided.
func (tw *TieredWatcher) scanForNewSessions() {
	if tw.scanDir == nil {
		return
	}

	tw.mu.Lock()
	dirs := make([]string, 0, len(tw.knownDirs))
	for dir := range tw.knownDirs {
		dirs = append(dirs, dir)
	}
	tw.mu.Unlock()

	for _, dir := range dirs {
		sessions, err := tw.scanDir(dir)
		if err != nil {
			continue
		}

		var newIDs []string
		needsRebuild := false
		tw.mu.Lock()
		for _, s := range sessions {
			if tw.sessions[s.ID] != nil {
				continue
			}
			info := &SessionInfo{
				ID:       s.ID,
				Path:     s.Path,
				ModTime:  s.ModTime,
				LastHot:  time.Now(),
				FileSize: s.FileSize,
			}
			if time.Since(s.ModTime) > FrozenThreshold {
				info.Frozen = true
			}
			tw.sessions[s.ID] = info
			tw.pathIndex[s.Path] = s.ID
			tw.knownDirs[filepath.Dir(s.Path)] = true
			newIDs = append(newIDs, s.ID)
			needsRebuild = true
		}
		if needsRebuild {
			tw.rebuildHotSetLocked()
		}
		tw.mu.Unlock()

		for _, id := range newIDs {
			select {
			case tw.events <- adapter.Event{
				Type:      adapter.EventSessionCreated,
				SessionID: id,
			}:
			default:
			}
		}
	}
}

// demotionLoop periodically demotes inactive HOT sessions to COLD.
func (tw *TieredWatcher) demotionLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			tw.demoteInactive()
		case <-tw.pollDone:
			return
		}
	}
}

// demoteInactive demotes HOT sessions that have been inactive too long.
func (tw *TieredWatcher) demoteInactive() {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	cutoff := time.Now().Add(-HotInactivityTimeout)
	var remaining []string
	for _, id := range tw.hotIDs {
		if info, ok := tw.sessions[id]; ok && info.LastHot.After(cutoff) {
			remaining = append(remaining, id)
		}
	}
	tw.hotIDs = remaining
	tw.syncHotDirsLocked()
}

// syncHotDirsLocked ensures only directories containing HOT sessions are watched,
// while preserving any root directories configured at creation.
// Must be called with tw.mu held.
func (tw *TieredWatcher) syncHotDirsLocked() {
	desired := make(map[string]bool, len(tw.hotIDs))
	for _, id := range tw.hotIDs {
		if info, ok := tw.sessions[id]; ok {
			desired[filepath.Dir(info.Path)] = true
		}
	}

	// Add missing watches
	for dir := range desired {
		if !tw.watchDirs[dir] {
			if err := tw.watcher.Add(dir); err == nil {
				tw.watchDirs[dir] = true
			}
		}
	}

	// Remove watches no longer needed (except root dirs)
	for dir := range tw.watchDirs {
		if tw.rootDirs[dir] {
			continue
		}
		if !desired[dir] {
			if err := tw.watcher.Remove(dir); err == nil {
				delete(tw.watchDirs, dir)
			}
		}
	}
}

// Close shuts down the watcher.
func (tw *TieredWatcher) Close() error {
	tw.mu.Lock()
	if tw.closed {
		tw.mu.Unlock()
		return nil
	}
	tw.closed = true
	tw.mu.Unlock()

	// Stop polling
	if tw.pollTicker != nil {
		tw.pollTicker.Stop()
	}
	close(tw.pollDone)

	// Close fsnotify watcher
	if tw.watcher != nil {
		_ = tw.watcher.Close()
	}

	// Close events channel
	close(tw.events)
	return nil
}

// Touch unfreezes a session so it will be polled again in COLD tier.
func (tw *TieredWatcher) Touch(sessionID string) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if info, ok := tw.sessions[sessionID]; ok {
		info.Frozen = false
	}
}

// Stats returns current watcher statistics.
func (tw *TieredWatcher) Stats() (hotCount, coldCount, frozenCount, watchedDirs int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	hotSet := make(map[string]bool, len(tw.hotIDs))
	for _, id := range tw.hotIDs {
		hotSet[id] = true
	}
	for id, info := range tw.sessions {
		if hotSet[id] {
			hotCount++
		} else if info.Frozen {
			frozenCount++
		} else {
			coldCount++
		}
	}
	watchedDirs = len(tw.watchDirs)
	return
}

// TieredCloser wraps TieredWatcher to implement io.Closer.
type TieredCloser struct {
	tw *TieredWatcher
}

// Close implements io.Closer.
func (tc *TieredCloser) Close() error {
	return tc.tw.Close()
}

// NewCloser returns an io.Closer for the TieredWatcher.
func (tw *TieredWatcher) NewCloser() io.Closer {
	return &TieredCloser{tw: tw}
}

// Manager coordinates tiered watching across multiple adapters.
// It merges events from all adapter watchers into a single channel.
type Manager struct {
	mu       sync.Mutex
	watchers map[string]*TieredWatcher // adapter ID -> watcher
	events   chan adapter.Event
	closers  []io.Closer
	closed   bool
}

// NewManager creates a new tiered watcher manager.
func NewManager() *Manager {
	return &Manager{
		watchers: make(map[string]*TieredWatcher),
		events:   make(chan adapter.Event, 64),
	}
}

// AddWatcher adds a tiered watcher for an adapter and starts forwarding its events.
func (m *Manager) AddWatcher(adapterID string, tw *TieredWatcher, ch <-chan adapter.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}

	m.watchers[adapterID] = tw
	m.closers = append(m.closers, tw.NewCloser())

	// Forward events from this watcher to the merged channel
	go func() {
		for evt := range ch {
			m.mu.Lock()
			closed := m.closed
			m.mu.Unlock()
			if closed {
				return
			}
			select {
			case m.events <- evt:
			default:
			}
		}
	}()
}

// Events returns the merged event channel.
func (m *Manager) Events() <-chan adapter.Event {
	return m.events
}

// PromoteSession promotes a session to HOT tier for a specific adapter.
// If adapterID is empty, promotes across all watchers.
func (m *Manager) PromoteSession(adapterID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if adapterID != "" {
		if tw, ok := m.watchers[adapterID]; ok {
			tw.PromoteToHot(sessionID)
		}
		return
	}

	for _, tw := range m.watchers {
		tw.PromoteToHot(sessionID)
	}
}

// SetHotTarget sets the desired HOT session count for a specific adapter.
func (m *Manager) SetHotTarget(adapterID string, target int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if tw, ok := m.watchers[adapterID]; ok {
		tw.SetHotTarget(target)
	}
}

// RegisterSession registers a session with the appropriate watcher.
func (m *Manager) RegisterSession(adapterID, sessionID, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if tw, ok := m.watchers[adapterID]; ok {
		tw.RegisterSession(sessionID, path)
	}
}

// Touch unfreezes a session for a specific adapter so it will be polled again.
// If adapterID is empty, touches across all watchers.
func (m *Manager) Touch(adapterID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if adapterID != "" {
		if tw, ok := m.watchers[adapterID]; ok {
			tw.Touch(sessionID)
		}
		return
	}

	for _, tw := range m.watchers {
		tw.Touch(sessionID)
	}
}

// Stats returns aggregate statistics across all watchers.
func (m *Manager) Stats() (hotCount, coldCount, frozenCount, watchedDirs int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, tw := range m.watchers {
		h, c, f, w := tw.Stats()
		hotCount += h
		coldCount += c
		frozenCount += f
		watchedDirs += w
	}
	return
}

// Close shuts down all watchers.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	closers := m.closers
	m.mu.Unlock()

	for _, c := range closers {
		_ = c.Close()
	}
	close(m.events)
	return nil
}

// Closers returns all io.Closers for the manager's watchers.
func (m *Manager) Closers() []io.Closer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closers
}
