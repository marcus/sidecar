package worktree

import (
	"sync"
	"testing"
	"time"
)

func TestOutputBuffer(t *testing.T) {
	t.Run("basic write and read", func(t *testing.T) {
		buf := NewOutputBuffer(10)
		buf.Write("line 1\nline 2\nline 3")

		lines := buf.Lines()
		if len(lines) != 3 {
			t.Errorf("got %d lines, want 3", len(lines))
		}
		if lines[0] != "line 1" {
			t.Errorf("first line = %q, want %q", lines[0], "line 1")
		}
	})

	t.Run("capacity limit", func(t *testing.T) {
		buf := NewOutputBuffer(5)
		// Write 10 lines - should be trimmed to last 5
		buf.Write("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")

		if buf.Len() != 5 {
			t.Errorf("buf.Len() = %d, want 5", buf.Len())
		}
		// Should keep the last 5 lines
		lines := buf.Lines()
		if lines[0] != "line6" {
			t.Errorf("first line = %q, want %q", lines[0], "line6")
		}
	})

	t.Run("string output", func(t *testing.T) {
		buf := NewOutputBuffer(10)
		buf.Write("a\nb\nc")

		s := buf.String()
		if s != "a\nb\nc" {
			t.Errorf("String() = %q, want %q", s, "a\nb\nc")
		}
	})

	t.Run("clear", func(t *testing.T) {
		buf := NewOutputBuffer(10)
		buf.Write("a\nb\nc")
		buf.Clear()

		if buf.Len() != 0 {
			t.Errorf("after Clear(), Len() = %d, want 0", buf.Len())
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		buf := NewOutputBuffer(100)
		var wg sync.WaitGroup

		// Concurrent writes (using replace semantics)
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					buf.Write("test line 1\ntest line 2\ntest line 3")
				}
			}()
		}

		// Concurrent reads
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					_ = buf.Lines()
					_ = buf.Len()
				}
			}()
		}

		wg.Wait()
		// Just verify it didn't panic and has valid content
		if buf.Len() > 100 {
			t.Errorf("buffer exceeded capacity: %d", buf.Len())
		}
	})

	t.Run("Update change detection", func(t *testing.T) {
		buf := NewOutputBuffer(100)

		// First update should return true (content changed)
		if !buf.Update("content 1") {
			t.Error("first Update() should return true")
		}

		// Same content should return false (no change)
		if buf.Update("content 1") {
			t.Error("Update() with same content should return false")
		}

		// Different content should return true
		if !buf.Update("content 2") {
			t.Error("Update() with different content should return true")
		}

		// Verify content is replaced
		lines := buf.Lines()
		if len(lines) != 1 || lines[0] != "content 2" {
			t.Errorf("content = %v, want [\"content 2\"]", lines)
		}
	})
}

func TestWorktreeStatusIcon(t *testing.T) {
	tests := []struct {
		status WorktreeStatus
		icon   string
	}{
		{StatusPaused, "⏸"},
		{StatusActive, "●"},
		{StatusWaiting, "⧗"},
		{StatusDone, "✓"},
		{StatusError, "✗"},
	}

	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			if tt.status.Icon() != tt.icon {
				t.Errorf("Icon() = %q, want %q", tt.status.Icon(), tt.icon)
			}
		})
	}
}

func TestWorktreeStatusString(t *testing.T) {
	tests := []struct {
		status WorktreeStatus
		str    string
	}{
		{StatusPaused, "paused"},
		{StatusActive, "active"},
		{StatusWaiting, "waiting"},
		{StatusDone, "done"},
		{StatusError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			if tt.status.String() != tt.str {
				t.Errorf("String() = %q, want %q", tt.status.String(), tt.str)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"a\nb\nc", []string{"a", "b", "c"}},
		{"a\r\nb\r\nc", []string{"a", "b", "c"}},
		{"single", []string{"single"}},
		{"", nil},
		{"trailing\n", []string{"trailing"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitLines(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("splitLines(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitLines(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFormatRelativeTime(t *testing.T) {
	// Just verify it doesn't panic with zero time
	s := formatRelativeTime(time.Time{})
	if s != "" {
		t.Errorf("formatRelativeTime(zero) = %q, want empty", s)
	}
}
