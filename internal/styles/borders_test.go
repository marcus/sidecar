package styles

import (
	"strings"
	"testing"
)

func TestColorChar(t *testing.T) {
	red := RGB{255, 0, 0}
	result := colorChar("X", red)

	if !strings.HasPrefix(result, "\x1b[38;2;255;0;0m") {
		t.Error("colorChar should start with ANSI color code")
	}
	if !strings.Contains(result, "X") {
		t.Error("colorChar should contain the character")
	}
	if !strings.HasSuffix(result, ANSIReset) {
		t.Error("colorChar should end with ANSI reset")
	}
}

func TestRenderGradientBorder_MinimumSize(t *testing.T) {
	g := NewGradient([]string{"#FF0000", "#0000FF"}, 30)

	// Too small - should return content as-is
	result := RenderGradientBorder("test", 2, 2, g, 0)
	if result != "test" {
		t.Errorf("expected content returned for small dimensions, got %q", result)
	}

	result = RenderGradientBorder("test", 1, 5, g, 0)
	if result != "test" {
		t.Errorf("expected content returned for narrow width, got %q", result)
	}
}

func TestRenderGradientBorder_ContainsBorderChars(t *testing.T) {
	g := NewGradient([]string{"#FF0000", "#0000FF"}, 30)
	result := RenderGradientBorder("hello", 20, 5, g, 1)

	// Check for border characters
	if !strings.Contains(result, "╭") {
		t.Error("result should contain top-left corner")
	}
	if !strings.Contains(result, "╮") {
		t.Error("result should contain top-right corner")
	}
	if !strings.Contains(result, "╰") {
		t.Error("result should contain bottom-left corner")
	}
	if !strings.Contains(result, "╯") {
		t.Error("result should contain bottom-right corner")
	}
	if !strings.Contains(result, "─") {
		t.Error("result should contain horizontal border")
	}
	if !strings.Contains(result, "│") {
		t.Error("result should contain vertical border")
	}
}

func TestRenderGradientBorder_ContainsContent(t *testing.T) {
	g := NewGradient([]string{"#FF0000", "#0000FF"}, 30)
	result := RenderGradientBorder("hello world", 20, 5, g, 1)

	if !strings.Contains(result, "hello world") {
		t.Error("result should contain the content")
	}
}

func TestRenderGradientBorder_MultilineContent(t *testing.T) {
	g := NewGradient([]string{"#FF0000", "#0000FF"}, 30)
	content := "line1\nline2\nline3"
	result := RenderGradientBorder(content, 20, 6, g, 1)

	if !strings.Contains(result, "line1") {
		t.Error("result should contain line1")
	}
	if !strings.Contains(result, "line2") {
		t.Error("result should contain line2")
	}
	if !strings.Contains(result, "line3") {
		t.Error("result should contain line3")
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
		wantLen  int // expected max visual width
	}{
		{"empty string", "", 10, 0},
		{"zero width", "hello", 0, 0},
		{"negative width", "hello", -5, 0},
		{"no truncation needed", "hi", 10, 2},
		{"exact fit", "hello", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxWidth)
			if tt.maxWidth <= 0 && result != "" {
				t.Errorf("expected empty string for maxWidth=%d, got %q", tt.maxWidth, result)
			}
		})
	}
}

func TestGetActiveGradient(t *testing.T) {
	// This tests the default theme's active gradient
	g := GetActiveGradient()

	if !g.IsValid() {
		t.Error("active gradient should be valid (have at least 2 stops)")
	}

	if g.Angle == 0 {
		t.Error("active gradient should have non-zero angle")
	}
}

func TestGetNormalGradient(t *testing.T) {
	// This tests the default theme's normal gradient
	g := GetNormalGradient()

	if !g.IsValid() {
		t.Error("normal gradient should be valid (have at least 2 stops)")
	}

	if g.Angle == 0 {
		t.Error("normal gradient should have non-zero angle")
	}
}

func TestRenderPanel(t *testing.T) {
	// Test active panel
	activeResult := RenderPanel("content", 20, 5, true)
	if !strings.Contains(activeResult, "content") {
		t.Error("active panel should contain content")
	}
	if !strings.Contains(activeResult, "╭") {
		t.Error("active panel should have border")
	}

	// Test inactive panel
	inactiveResult := RenderPanel("content", 20, 5, false)
	if !strings.Contains(inactiveResult, "content") {
		t.Error("inactive panel should contain content")
	}
	if !strings.Contains(inactiveResult, "╭") {
		t.Error("inactive panel should have border")
	}
}

func TestRenderPanelWithGradient(t *testing.T) {
	customGradient := NewGradient([]string{"#00FF00", "#FF00FF"}, 45)
	result := RenderPanelWithGradient("test", 15, 4, customGradient)

	if !strings.Contains(result, "test") {
		t.Error("custom gradient panel should contain content")
	}
	if !strings.Contains(result, "╭") {
		t.Error("custom gradient panel should have border")
	}
}

func TestRenderGradientBorderTop(t *testing.T) {
	g := NewGradient([]string{"#FF0000", "#0000FF"}, 30)
	result := renderGradientBorderTop(10, 5, g)

	// Should start with top-left corner and end with top-right corner
	if !strings.Contains(result, "╭") {
		t.Error("top border should contain top-left corner")
	}
	if !strings.Contains(result, "╮") {
		t.Error("top border should contain top-right corner")
	}
	if !strings.Contains(result, "─") {
		t.Error("top border should contain horizontal line")
	}
}

func TestRenderGradientBorderBottom(t *testing.T) {
	g := NewGradient([]string{"#FF0000", "#0000FF"}, 30)
	result := renderGradientBorderBottom(10, 5, g)

	// Should start with bottom-left corner and end with bottom-right corner
	if !strings.Contains(result, "╰") {
		t.Error("bottom border should contain bottom-left corner")
	}
	if !strings.Contains(result, "╯") {
		t.Error("bottom border should contain bottom-right corner")
	}
	if !strings.Contains(result, "─") {
		t.Error("bottom border should contain horizontal line")
	}
}
