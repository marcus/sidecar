package filebrowser

import (
	"testing"
	"time"
)

func TestParseBlameOutput(t *testing.T) {
	// Sample git blame --line-porcelain output
	// Note: Content lines start with a literal tab character
	output := "abc1234567890abcdef1234567890abcdef12340 1 1 1\n" +
		"author John Doe\n" +
		"author-mail <john@example.com>\n" +
		"author-time 1700000000\n" +
		"author-tz -0800\n" +
		"committer John Doe\n" +
		"committer-mail <john@example.com>\n" +
		"committer-time 1700000000\n" +
		"committer-tz -0800\n" +
		"summary Initial commit\n" +
		"filename test.go\n" +
		"\tpackage main\n" +
		"def45678901234567890123456789012345678901 2 2 1\n" +
		"author Jane Smith\n" +
		"author-mail <jane@example.com>\n" +
		"author-time 1705000000\n" +
		"author-tz -0800\n" +
		"committer Jane Smith\n" +
		"committer-mail <jane@example.com>\n" +
		"committer-time 1705000000\n" +
		"committer-tz -0800\n" +
		"summary Add feature\n" +
		"filename test.go\n" +
		"\tfunc main() {\n"

	lines := parseBlameOutput(output)

	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(lines))
	}

	// Check first line
	if lines[0].CommitHash != "abc12345" {
		t.Errorf("Expected hash 'abc12345', got '%s'", lines[0].CommitHash)
	}
	if lines[0].Author != "John Doe" {
		t.Errorf("Expected author 'John Doe', got '%s'", lines[0].Author)
	}
	if lines[0].Content != "package main" {
		t.Errorf("Expected content 'package main', got '%s'", lines[0].Content)
	}
	if lines[0].LineNo != 1 {
		t.Errorf("Expected line number 1, got %d", lines[0].LineNo)
	}

	// Check second line (different author)
	if lines[1].CommitHash != "def45678" {
		t.Errorf("Expected hash 'def45678', got '%s'", lines[1].CommitHash)
	}
	if lines[1].Author != "Jane Smith" {
		t.Errorf("Expected author 'Jane Smith', got '%s'", lines[1].Author)
	}
	if lines[1].LineNo != 2 {
		t.Errorf("Expected line number 2, got %d", lines[1].LineNo)
	}
}

func TestParseBlameOutputSingleLine(t *testing.T) {
	// Test with a single line
	output := "abc1234567890abcdef1234567890abcdef12340 1 1 1\n" +
		"author John Doe\n" +
		"author-time 1700000000\n" +
		"filename test.go\n" +
		"\tpackage main\n"

	lines := parseBlameOutput(output)

	if len(lines) != 1 {
		t.Fatalf("Expected 1 line, got %d", len(lines))
	}

	if lines[0].CommitHash != "abc12345" {
		t.Errorf("Expected hash 'abc12345', got '%s'", lines[0].CommitHash)
	}
	if lines[0].Author != "John Doe" {
		t.Errorf("Expected author 'John Doe', got '%s'", lines[0].Author)
	}
	if lines[0].Content != "package main" {
		t.Errorf("Expected content 'package main', got '%s'", lines[0].Content)
	}
}

func TestParseBlameOutputEmpty(t *testing.T) {
	lines := parseBlameOutput("")
	if len(lines) != 0 {
		t.Errorf("Expected 0 lines for empty output, got %d", len(lines))
	}
}

func TestGetBlameAgeColor(t *testing.T) {
	tests := []struct {
		name string
		age  time.Duration
	}{
		{"recent (1 hour)", time.Hour},
		{"yesterday", 24 * time.Hour},
		{"last week", 5 * 24 * time.Hour},
		{"two weeks ago", 14 * 24 * time.Hour},
		{"two months ago", 60 * 24 * time.Hour},
		{"six months ago", 180 * 24 * time.Hour},
		{"two years ago", 730 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commitTime := time.Now().Add(-tt.age)
			color := getBlameAgeColor(commitTime)
			if color == "" {
				t.Errorf("Expected non-empty color for %s", tt.name)
			}
		})
	}
}

func TestGetBlameAgeColorZeroTime(t *testing.T) {
	color := getBlameAgeColor(time.Time{})
	// Should return muted color for zero time
	if color == "" {
		t.Error("Expected non-empty color for zero time")
	}
}

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		age      time.Duration
		expected string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5 mins ago"},
		{1 * time.Minute, "1 min ago"},
		{2 * time.Hour, "2 hours ago"},
		{1 * time.Hour, "1 hour ago"},
		{3 * 24 * time.Hour, "3 days ago"},
		{1 * 24 * time.Hour, "1 day ago"},
		{2 * 7 * 24 * time.Hour, "2 weeks ago"},
		{1 * 7 * 24 * time.Hour, "1 week ago"},
		{3 * 30 * 24 * time.Hour, "3 months ago"},
		{1 * 30 * 24 * time.Hour, "1 month ago"},
		{2 * 365 * 24 * time.Hour, "2 years ago"},
		{1 * 365 * 24 * time.Hour, "1 year ago"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			commitTime := time.Now().Add(-tt.age)
			result := RelativeTime(commitTime)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestRelativeTimeZero(t *testing.T) {
	result := RelativeTime(time.Time{})
	if result != "unknown" {
		t.Errorf("Expected 'unknown' for zero time, got '%s'", result)
	}
}

func TestIsHexString(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"abc123", true},
		{"ABC123", true},
		{"0123456789abcdef", true},
		{"ABCDEF", true},
		{"xyz123", false},
		{"abc123!", false},
		{"", true}, // Empty string is technically valid
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isHexString(tt.input)
			if result != tt.expected {
				t.Errorf("isHexString(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPadOrTruncate(t *testing.T) {
	tests := []struct {
		input    string
		width    int
		expected string
	}{
		{"hello", 10, "hello     "},
		{"hello", 5, "hello"},
		{"hello world", 5, "hell…"},
		{"", 3, "   "},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := padOrTruncate(tt.input, tt.width)
			if result != tt.expected {
				t.Errorf("padOrTruncate(%q, %d) = %q, want %q", tt.input, tt.width, result, tt.expected)
			}
		})
	}
}

func TestPadOrTruncateUnicode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		width    int
		expected string
	}{
		{"cjk no truncate", "日本語", 5, "日本語  "},
		{"cjk truncate", "日本語テスト", 4, "日本語…"},
		{"emoji no truncate", "🔥🔥", 5, "🔥🔥   "},
		{"emoji truncate", "🔥🔥🔥🔥", 3, "🔥🔥…"},
		{"mixed ascii and cjk", "Hi日本", 4, "Hi日本"},
		{"mixed truncate", "Hello世界", 6, "Hello…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := padOrTruncate(tt.input, tt.width)
			if result != tt.expected {
				t.Errorf("padOrTruncate(%q, %d) = %q, want %q", tt.input, tt.width, result, tt.expected)
			}
		})
	}
}
