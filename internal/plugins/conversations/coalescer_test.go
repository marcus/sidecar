package conversations

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestEventCoalescer_SingleEvent(t *testing.T) {
	// Single event should be flushed after window
	var received CoalescedRefreshMsg
	var wg sync.WaitGroup
	wg.Add(1)

	ch := make(chan CoalescedRefreshMsg, 1)
	go func() {
		received = <-ch
		wg.Done()
	}()

	c := NewEventCoalescer(50*time.Millisecond, ch)
	c.Add("session-123", 0)

	wg.Wait()

	if len(received.SessionIDs) != 1 {
		t.Errorf("expected 1 session ID, got %d", len(received.SessionIDs))
	}
	if received.SessionIDs[0] != "session-123" {
		t.Errorf("expected session-123, got %s", received.SessionIDs[0])
	}
	if received.RefreshAll {
		t.Error("expected RefreshAll=false")
	}
}

func TestEventCoalescer_MultipleEvents(t *testing.T) {
	// Multiple events within window should be batched
	var received CoalescedRefreshMsg
	var wg sync.WaitGroup
	wg.Add(1)

	ch := make(chan CoalescedRefreshMsg, 1)
	go func() {
		received = <-ch
		wg.Done()
	}()

	c := NewEventCoalescer(100*time.Millisecond, ch)
	c.Add("session-1", 0)
	c.Add("session-2", 0)
	c.Add("session-3", 0)

	wg.Wait()

	if len(received.SessionIDs) != 3 {
		t.Errorf("expected 3 session IDs, got %d", len(received.SessionIDs))
	}
}

func TestEventCoalescer_DuplicateSessionIDs(t *testing.T) {
	// Duplicate session IDs should be deduplicated
	var received CoalescedRefreshMsg
	var wg sync.WaitGroup
	wg.Add(1)

	ch := make(chan CoalescedRefreshMsg, 1)
	go func() {
		received = <-ch
		wg.Done()
	}()

	c := NewEventCoalescer(50*time.Millisecond, ch)
	c.Add("session-1", 0)
	c.Add("session-1", 0)
	c.Add("session-1", 0)

	wg.Wait()

	if len(received.SessionIDs) != 1 {
		t.Errorf("expected 1 session ID (deduplicated), got %d", len(received.SessionIDs))
	}
}

func TestEventCoalescer_EmptySessionID(t *testing.T) {
	// Empty session ID should trigger RefreshAll
	var received CoalescedRefreshMsg
	var wg sync.WaitGroup
	wg.Add(1)

	ch := make(chan CoalescedRefreshMsg, 1)
	go func() {
		received = <-ch
		wg.Done()
	}()

	c := NewEventCoalescer(50*time.Millisecond, ch)
	c.Add("", 0)

	wg.Wait()

	if !received.RefreshAll {
		t.Error("expected RefreshAll=true for empty session ID")
	}
}

func TestEventCoalescer_TooManyEvents(t *testing.T) {
	// More than maxPendingSessionIDs should trigger RefreshAll
	var received CoalescedRefreshMsg
	var wg sync.WaitGroup
	wg.Add(1)

	ch := make(chan CoalescedRefreshMsg, 1)
	go func() {
		received = <-ch
		wg.Done()
	}()

	c := NewEventCoalescer(50*time.Millisecond, ch)
	for i := 0; i < 15; i++ { // More than maxPendingSessionIDs (10)
		c.Add(fmt.Sprintf("session-%d", i), 0)
	}

	wg.Wait()

	if !received.RefreshAll {
		t.Error("expected RefreshAll=true when exceeding max pending IDs")
	}
}

func TestEventCoalescer_Stop(t *testing.T) {
	// Stop should cancel pending flush
	ch := make(chan CoalescedRefreshMsg, 1)
	c := NewEventCoalescer(100*time.Millisecond, ch)

	c.Add("session-1", 0)
	c.Stop()

	// Wait longer than coalesce window
	time.Sleep(150 * time.Millisecond)

	select {
	case <-ch:
		t.Error("expected no message after Stop()")
	default:
		// Good - no message sent
	}
}

func TestEventCoalescer_TimerReset(t *testing.T) {
	// New events should reset the timer
	var received CoalescedRefreshMsg
	var wg sync.WaitGroup
	wg.Add(1)

	start := time.Now()
	ch := make(chan CoalescedRefreshMsg, 1)
	go func() {
		received = <-ch
		wg.Done()
	}()

	c := NewEventCoalescer(50*time.Millisecond, ch)

	// Add event, wait 30ms, add another
	c.Add("session-1", 0)
	time.Sleep(30 * time.Millisecond)
	c.Add("session-2", 0)

	wg.Wait()
	elapsed := time.Since(start)

	// Should take ~80ms (30ms + 50ms), not ~50ms
	if elapsed < 70*time.Millisecond {
		t.Errorf("expected timer to reset, but elapsed was %v", elapsed)
	}

	if len(received.SessionIDs) != 2 {
		t.Errorf("expected 2 session IDs, got %d", len(received.SessionIDs))
	}
}

func TestEventCoalescer_StopWithClosedChannel(t *testing.T) {
	// Regression test: Stop() then close channel should not panic
	// This simulates the project switch scenario where plugin.Stop() is called
	ch := make(chan CoalescedRefreshMsg, 1)
	c := NewEventCoalescer(50*time.Millisecond, ch)

	c.Add("session-1", 0)

	// Stop and close channel (simulates plugin shutdown)
	c.Stop()
	close(ch)

	// Wait longer than coalesce window - should not panic
	time.Sleep(100 * time.Millisecond)
}

func TestEventCoalescer_StopRaceCondition(t *testing.T) {
	// Stress test: rapidly add events and stop to trigger race conditions
	for i := 0; i < 100; i++ {
		ch := make(chan CoalescedRefreshMsg, 1)
		c := NewEventCoalescer(1*time.Millisecond, ch)

		// Add event
		c.Add("session-1", 0)

		// Stop and close immediately (race with timer)
		c.Stop()
		close(ch)

		// Small sleep to let any pending timers fire
		time.Sleep(5 * time.Millisecond)
	}
}

// TestEventCoalescer_DynamicWindow tests td-190095: dynamic debounce windows.
func TestEventCoalescer_DynamicWindow(t *testing.T) {
	t.Run("small session uses base window", func(t *testing.T) {
		ch := make(chan CoalescedRefreshMsg, 1)
		c := NewEventCoalescer(50*time.Millisecond, ch)

		// Small session (10MB - no scaling needed)
		c.UpdateSessionSize("small", 10*1024*1024)

		start := time.Now()
		c.Add("small", 0)
		<-ch
		elapsed := time.Since(start)

		// Should use base window (~50ms)
		if elapsed < 40*time.Millisecond || elapsed > 100*time.Millisecond {
			t.Errorf("expected ~50ms, got %v", elapsed)
		}
	})

	t.Run("100MB session adds 500ms to window", func(t *testing.T) {
		ch := make(chan CoalescedRefreshMsg, 1)
		c := NewEventCoalescer(50*time.Millisecond, ch)

		// 100MB session = 1x scale factor = +500ms
		c.UpdateSessionSize("medium", 100*1024*1024)

		start := time.Now()
		c.Add("medium", 0)
		<-ch
		elapsed := time.Since(start)

		// Should use scaled window: 50ms base + 500ms = 550ms
		if elapsed < 450*time.Millisecond || elapsed > 700*time.Millisecond {
			t.Errorf("expected ~550ms, got %v", elapsed)
		}
	})

	t.Run("window calculation is correct", func(t *testing.T) {
		// Verify the calculation logic without waiting for timers
		c := NewEventCoalescer(250*time.Millisecond, nil)

		testCases := []struct {
			name     string
			size     int64
			expected time.Duration
		}{
			{"0 bytes", 0, 250 * time.Millisecond},
			{"50MB", 50 * 1024 * 1024, 250 * time.Millisecond},    // below 100MB threshold
			{"100MB", 100 * 1024 * 1024, 750 * time.Millisecond},  // 250 + 500
			{"200MB", 200 * 1024 * 1024, 1250 * time.Millisecond}, // 250 + 1000
			{"500MB", 500 * 1024 * 1024, 2750 * time.Millisecond}, // 250 + 2500
			{"1GB", 1024 * 1024 * 1024, 5 * time.Second},          // capped at max
			{"2GB", 2 * 1024 * 1024 * 1024, 5 * time.Second},      // capped at max
		}

		for _, tc := range testCases {
			c.UpdateSessionSize("test", tc.size)
			c.pendingIDs["test"] = struct{}{}
			window := c.maxWindowForPendingLocked()
			delete(c.pendingIDs, "test")

			if window != tc.expected {
				t.Errorf("%s: expected %v, got %v", tc.name, tc.expected, window)
			}
		}
	})

	t.Run("uses largest pending session window", func(t *testing.T) {
		c := NewEventCoalescer(250*time.Millisecond, nil)

		// Mix of session sizes
		c.UpdateSessionSize("small", 10*1024*1024)   // 0 scale
		c.UpdateSessionSize("medium", 150*1024*1024) // 1x scale = +500ms
		c.UpdateSessionSize("large", 350*1024*1024)  // 3x scale = +1500ms

		c.pendingIDs["small"] = struct{}{}
		c.pendingIDs["medium"] = struct{}{}
		c.pendingIDs["large"] = struct{}{}

		window := c.maxWindowForPendingLocked()

		// Should use large session's window: 250 + 1500 = 1750ms
		expected := 1750 * time.Millisecond
		if window != expected {
			t.Errorf("expected %v (largest session), got %v", expected, window)
		}
	})
}

// TestEventCoalescer_AutoReload tests td-190095: auto-reload disabled for huge sessions.
func TestEventCoalescer_AutoReload(t *testing.T) {
	t.Run("small session auto-reloads", func(t *testing.T) {
		ch := make(chan CoalescedRefreshMsg, 1)
		c := NewEventCoalescer(50*time.Millisecond, ch)

		// 50MB - under HugeSessionThreshold (500MB) so auto-reload enabled
		// Window: 50ms base (50MB is below 100MB scale threshold)
		c.UpdateSessionSize("small", 50*1024*1024)
		c.Add("small", 0)

		select {
		case msg := <-ch:
			if len(msg.SessionIDs) != 1 || msg.SessionIDs[0] != "small" {
				t.Errorf("expected small session, got %v", msg.SessionIDs)
			}
		case <-time.After(150 * time.Millisecond):
			t.Error("expected message for small session")
		}
	})

	t.Run("huge session skips auto-reload", func(t *testing.T) {
		ch := make(chan CoalescedRefreshMsg, 1)
		c := NewEventCoalescer(50*time.Millisecond, ch)

		c.UpdateSessionSize("huge", 600*1024*1024) // 600MB - over threshold
		c.Add("huge", 0)

		select {
		case msg := <-ch:
			t.Errorf("expected no message for huge session, got %v", msg)
		case <-time.After(100 * time.Millisecond):
			// Good - no message
		}
	})

	t.Run("ShouldAutoReload returns correct value", func(t *testing.T) {
		c := NewEventCoalescer(50*time.Millisecond, nil)

		// Under threshold
		c.UpdateSessionSize("small", 100*1024*1024)
		if !c.ShouldAutoReload("small") {
			t.Error("expected auto-reload for small session")
		}

		// At threshold
		c.UpdateSessionSize("at-threshold", 500*1024*1024)
		if c.ShouldAutoReload("at-threshold") {
			t.Error("expected no auto-reload at threshold")
		}

		// Over threshold
		c.UpdateSessionSize("huge", 600*1024*1024)
		if c.ShouldAutoReload("huge") {
			t.Error("expected no auto-reload for huge session")
		}

		// Unknown session (no size info)
		if !c.ShouldAutoReload("unknown") {
			t.Error("expected auto-reload for unknown session (0 size)")
		}
	})
}
