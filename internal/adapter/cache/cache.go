package cache

import (
	"os"
	"sort"
	"sync"
	"time"
)

// Entry holds cached data with file metadata for invalidation.
type Entry[T any] struct {
	Data       T
	ModTime    time.Time
	Size       int64
	LastAccess time.Time
	ByteOffset int64 // for incremental parsing
}

// Cache is a thread-safe generic cache with LRU eviction.
type Cache[T any] struct {
	entries map[string]Entry[T]
	mu      sync.RWMutex
	maxSize int
}

// New creates a new cache with the specified maximum number of entries.
func New[T any](maxSize int) *Cache[T] {
	return &Cache[T]{
		entries: make(map[string]Entry[T]),
		maxSize: maxSize,
	}
}

// Get returns cached data if the file hasn't changed.
// Returns (data, true) if cache hit, (zero, false) if miss or stale.
func (c *Cache[T]) Get(key string, size int64, modTime time.Time) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		var zero T
		return zero, false
	}

	if entry.Size != size || !entry.ModTime.Equal(modTime) {
		var zero T
		return zero, false
	}

	entry.LastAccess = time.Now()
	c.entries[key] = entry
	return entry.Data, true
}

// GetWithOffset returns cached data and byte offset for incremental parsing.
// Use when file may have grown and you want to resume parsing from the offset.
// Returns (data, offset, true) if cached entry exists with matching key.
// Caller should check if file grew (newSize > cachedSize) to decide on incremental parse.
func (c *Cache[T]) GetWithOffset(key string) (T, int64, int64, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		var zero T
		return zero, 0, 0, time.Time{}, false
	}

	entry.LastAccess = time.Now()
	c.entries[key] = entry
	return entry.Data, entry.ByteOffset, entry.Size, entry.ModTime, true
}

// Set stores data in the cache with file metadata.
func (c *Cache[T]) Set(key string, data T, size int64, modTime time.Time, offset int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = Entry[T]{
		Data:       data,
		ModTime:    modTime,
		Size:       size,
		LastAccess: time.Now(),
		ByteOffset: offset,
	}

	c.evictOldestLocked()
}

// Delete removes an entry from the cache.
func (c *Cache[T]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// DeleteIf removes entries matching the predicate.
func (c *Cache[T]) DeleteIf(pred func(key string, entry Entry[T]) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, entry := range c.entries {
		if pred(key, entry) {
			delete(c.entries, key)
		}
	}
}

// InvalidateIfChanged removes entry if file metadata has changed.
func (c *Cache[T]) InvalidateIfChanged(key string, size int64, modTime time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[key]; ok {
		if entry.Size != size || !entry.ModTime.Equal(modTime) {
			delete(c.entries, key)
		}
	}
}

// Len returns the number of entries in the cache.
func (c *Cache[T]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// evictOldestLocked removes oldest entries when over capacity.
// Must be called with lock held.
func (c *Cache[T]) evictOldestLocked() {
	excess := len(c.entries) - c.maxSize
	if excess <= 0 {
		return
	}

	type keyAccess struct {
		key        string
		lastAccess time.Time
	}
	entries := make([]keyAccess, 0, len(c.entries))
	for key, entry := range c.entries {
		entries = append(entries, keyAccess{key, entry.LastAccess})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastAccess.Before(entries[j].lastAccess)
	})

	for i := range excess {
		delete(c.entries, entries[i].key)
	}
}

// FileChanged checks if a file has changed since a cached entry.
// Returns (changed, grew, info, err).
// - changed: true if size or modTime differs
// - grew: true if file size increased (allows incremental parsing)
// - info: current file info for updating cache
func FileChanged(path string, cachedSize int64, cachedModTime time.Time) (changed, grew bool, info os.FileInfo, err error) {
	info, err = os.Stat(path)
	if err != nil {
		return false, false, nil, err
	}
	if info.Size() == cachedSize && info.ModTime().Equal(cachedModTime) {
		return false, false, info, nil
	}
	return true, info.Size() > cachedSize, info, nil
}
