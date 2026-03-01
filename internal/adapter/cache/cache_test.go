package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCache_GetSet(t *testing.T) {
	c := New[string](10)

	now := time.Now()

	// Initially empty
	if _, ok := c.Get("key1", 100, now); ok {
		t.Error("expected cache miss for non-existent key")
	}

	// Set and get
	c.Set("key1", "value1", 100, now, 0)
	if val, ok := c.Get("key1", 100, now); !ok || val != "value1" {
		t.Errorf("expected cache hit with value1, got %v, %v", val, ok)
	}

	// Wrong size = miss
	if _, ok := c.Get("key1", 200, now); ok {
		t.Error("expected cache miss for wrong size")
	}

	// Wrong modTime = miss
	if _, ok := c.Get("key1", 100, now.Add(time.Second)); ok {
		t.Error("expected cache miss for wrong modTime")
	}
}

func TestCache_GetWithOffset(t *testing.T) {
	c := New[[]int](10)

	now := time.Now()
	data := []int{1, 2, 3}
	c.Set("key1", data, 100, now, 50)

	gotData, offset, size, modTime, ok := c.GetWithOffset("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(gotData) != 3 || gotData[0] != 1 {
		t.Errorf("unexpected data: %v", gotData)
	}
	if offset != 50 {
		t.Errorf("expected offset 50, got %d", offset)
	}
	if size != 100 {
		t.Errorf("expected size 100, got %d", size)
	}
	if !modTime.Equal(now) {
		t.Errorf("expected modTime %v, got %v", now, modTime)
	}

	// Non-existent key
	_, _, _, _, ok = c.GetWithOffset("missing")
	if ok {
		t.Error("expected miss for non-existent key")
	}
}

func TestCache_GetWithOffset_UpdatesLastAccess(t *testing.T) {
	c := New[int](3)
	baseTime := time.Now()

	// Add 3 entries
	c.Set("key1", 1, 100, baseTime, 0)
	time.Sleep(time.Millisecond)
	c.Set("key2", 2, 100, baseTime, 0)
	time.Sleep(time.Millisecond)
	c.Set("key3", 3, 100, baseTime, 0)

	// Access key1 via GetWithOffset to bump its LRU position
	c.GetWithOffset("key1")
	time.Sleep(time.Millisecond)

	// Add key4, should evict key2 (oldest by lastAccess), NOT key1
	c.Set("key4", 4, 100, baseTime, 0)

	if c.Len() != 3 {
		t.Errorf("expected 3 entries, got %d", c.Len())
	}

	// key2 should be evicted (oldest access)
	_, _, _, _, ok := c.GetWithOffset("key2")
	if ok {
		t.Error("expected key2 to be evicted")
	}

	// key1 should remain (GetWithOffset bumped its access time)
	_, _, _, _, ok = c.GetWithOffset("key1")
	if !ok {
		t.Error("expected key1 to remain after GetWithOffset bumped its access time")
	}
}

func TestCache_Delete(t *testing.T) {
	c := New[string](10)
	now := time.Now()

	c.Set("key1", "value1", 100, now, 0)
	c.Delete("key1")

	if _, ok := c.Get("key1", 100, now); ok {
		t.Error("expected cache miss after delete")
	}
}

func TestCache_DeleteIf(t *testing.T) {
	c := New[int](10)
	now := time.Now()

	c.Set("a1", 1, 100, now, 0)
	c.Set("a2", 2, 100, now, 0)
	c.Set("b1", 3, 100, now, 0)

	// Delete all keys starting with "a"
	c.DeleteIf(func(key string, _ Entry[int]) bool {
		return key[0] == 'a'
	})

	if c.Len() != 1 {
		t.Errorf("expected 1 entry, got %d", c.Len())
	}
	if _, ok := c.Get("b1", 100, now); !ok {
		t.Error("expected b1 to remain")
	}
}

func TestCache_InvalidateIfChanged(t *testing.T) {
	c := New[string](10)
	now := time.Now()

	c.Set("key1", "value1", 100, now, 0)

	// Same metadata = no invalidation
	c.InvalidateIfChanged("key1", 100, now)
	if c.Len() != 1 {
		t.Error("expected entry to remain when metadata unchanged")
	}

	// Different size = invalidate
	c.InvalidateIfChanged("key1", 200, now)
	if c.Len() != 0 {
		t.Error("expected entry to be invalidated when size changed")
	}

	// Test modTime change
	c.Set("key2", "value2", 100, now, 0)
	c.InvalidateIfChanged("key2", 100, now.Add(time.Second))
	if c.Len() != 0 {
		t.Error("expected entry to be invalidated when modTime changed")
	}
}

func TestCache_LRUEviction(t *testing.T) {
	c := New[int](3)
	baseTime := time.Now()

	// Add 3 entries with different access times
	c.Set("key1", 1, 100, baseTime, 0)
	time.Sleep(time.Millisecond)
	c.Set("key2", 2, 100, baseTime, 0)
	time.Sleep(time.Millisecond)
	c.Set("key3", 3, 100, baseTime, 0)

	// Access key1 to make it recent
	c.Get("key1", 100, baseTime)
	time.Sleep(time.Millisecond)

	// Add key4, should evict key2 (oldest by lastAccess)
	c.Set("key4", 4, 100, baseTime, 0)

	if c.Len() != 3 {
		t.Errorf("expected 3 entries, got %d", c.Len())
	}

	// key2 should be evicted
	if _, ok := c.Get("key2", 100, baseTime); ok {
		t.Error("expected key2 to be evicted")
	}

	// key1, key3, key4 should remain
	if _, ok := c.Get("key1", 100, baseTime); !ok {
		t.Error("expected key1 to remain")
	}
	if _, ok := c.Get("key3", 100, baseTime); !ok {
		t.Error("expected key3 to remain")
	}
	if _, ok := c.Get("key4", 100, baseTime); !ok {
		t.Error("expected key4 to remain")
	}
}

func TestFileChanged(t *testing.T) {
	// Create a temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// No change
	changed, grew, newInfo, err := FileChanged(path, info.Size(), info.ModTime())
	if err != nil {
		t.Fatal(err)
	}
	if changed || grew {
		t.Error("expected no change")
	}
	if newInfo == nil {
		t.Error("expected info")
	}

	// Different size
	changed, grew, _, err = FileChanged(path, 0, info.ModTime())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected changed")
	}
	if !grew {
		t.Error("expected grew")
	}

	// Grew check (smaller cached size)
	changed, grew, _, err = FileChanged(path, 3, info.ModTime())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected changed")
	}
	if !grew {
		t.Error("expected grew when current > cached")
	}

	// Shrunk (larger cached size)
	changed, grew, _, err = FileChanged(path, 100, info.ModTime())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected changed")
	}
	if grew {
		t.Error("expected not grew when current < cached")
	}

	// Non-existent file
	_, _, _, err = FileChanged(filepath.Join(dir, "missing.txt"), 0, time.Now())
	if err == nil {
		t.Error("expected error for missing file")
	}
}
