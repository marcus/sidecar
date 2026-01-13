package styles

import (
	"math"
	"testing"
)

func TestHexToRGB(t *testing.T) {
	tests := []struct {
		name     string
		hex      string
		expected RGB
	}{
		{"black", "#000000", RGB{0, 0, 0}},
		{"white", "#FFFFFF", RGB{255, 255, 255}},
		{"red", "#FF0000", RGB{255, 0, 0}},
		{"green", "#00FF00", RGB{0, 255, 0}},
		{"blue", "#0000FF", RGB{0, 0, 255}},
		{"purple", "#7C3AED", RGB{124, 58, 237}},
		{"without hash", "3B82F6", RGB{59, 130, 246}},
		{"lowercase", "#abcdef", RGB{171, 205, 239}},
		{"invalid short", "#FFF", RGB{128, 128, 128}},
		{"invalid empty", "", RGB{128, 128, 128}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HexToRGB(tt.hex)
			if result.R != tt.expected.R || result.G != tt.expected.G || result.B != tt.expected.B {
				t.Errorf("HexToRGB(%q) = %+v, want %+v", tt.hex, result, tt.expected)
			}
		})
	}
}

func TestRGBToHex(t *testing.T) {
	tests := []struct {
		name     string
		rgb      RGB
		expected string
	}{
		{"black", RGB{0, 0, 0}, "#000000"},
		{"white", RGB{255, 255, 255}, "#ffffff"},
		{"red", RGB{255, 0, 0}, "#ff0000"},
		{"clamped high", RGB{300, 300, 300}, "#ffffff"},
		{"clamped low", RGB{-10, -10, -10}, "#000000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RGBToHex(tt.rgb)
			if result != tt.expected {
				t.Errorf("RGBToHex(%+v) = %q, want %q", tt.rgb, result, tt.expected)
			}
		})
	}
}

func TestLerpRGB(t *testing.T) {
	black := RGB{0, 0, 0}
	white := RGB{255, 255, 255}

	tests := []struct {
		name     string
		c1, c2   RGB
		t        float64
		expected RGB
	}{
		{"start", black, white, 0, black},
		{"end", black, white, 1, white},
		{"middle", black, white, 0.5, RGB{127.5, 127.5, 127.5}},
		{"quarter", black, white, 0.25, RGB{63.75, 63.75, 63.75}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LerpRGB(tt.c1, tt.c2, tt.t)
			if math.Abs(result.R-tt.expected.R) > 0.01 ||
				math.Abs(result.G-tt.expected.G) > 0.01 ||
				math.Abs(result.B-tt.expected.B) > 0.01 {
				t.Errorf("LerpRGB(%+v, %+v, %v) = %+v, want %+v", tt.c1, tt.c2, tt.t, result, tt.expected)
			}
		})
	}
}

func TestNewGradient(t *testing.T) {
	t.Run("empty colors", func(t *testing.T) {
		g := NewGradient([]string{}, 30)
		if len(g.Stops) != 0 {
			t.Errorf("expected 0 stops, got %d", len(g.Stops))
		}
	})

	t.Run("single color", func(t *testing.T) {
		g := NewGradient([]string{"#FF0000"}, 30)
		if len(g.Stops) != 1 {
			t.Errorf("expected 1 stop, got %d", len(g.Stops))
		}
		if g.Stops[0].Position != 0.5 {
			t.Errorf("expected position 0.5, got %v", g.Stops[0].Position)
		}
	})

	t.Run("two colors", func(t *testing.T) {
		g := NewGradient([]string{"#FF0000", "#0000FF"}, 45)
		if len(g.Stops) != 2 {
			t.Errorf("expected 2 stops, got %d", len(g.Stops))
		}
		if g.Stops[0].Position != 0 {
			t.Errorf("expected first position 0, got %v", g.Stops[0].Position)
		}
		if g.Stops[1].Position != 1 {
			t.Errorf("expected second position 1, got %v", g.Stops[1].Position)
		}
		if g.Angle != 45 {
			t.Errorf("expected angle 45, got %v", g.Angle)
		}
	})

	t.Run("three colors", func(t *testing.T) {
		g := NewGradient([]string{"#FF0000", "#00FF00", "#0000FF"}, 30)
		if len(g.Stops) != 3 {
			t.Errorf("expected 3 stops, got %d", len(g.Stops))
		}
		if g.Stops[1].Position != 0.5 {
			t.Errorf("expected middle position 0.5, got %v", g.Stops[1].Position)
		}
	})
}

func TestGradient_ColorAt(t *testing.T) {
	g := NewGradient([]string{"#000000", "#FFFFFF"}, 30)

	tests := []struct {
		name string
		t    float64
		minR float64
		maxR float64
	}{
		{"at 0", 0, 0, 0},
		{"at 1", 1, 255, 255},
		{"at 0.5", 0.5, 127, 128},
		{"below 0", -0.5, 0, 0},
		{"above 1", 1.5, 255, 255},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := g.ColorAt(tt.t)
			if result.R < tt.minR || result.R > tt.maxR {
				t.Errorf("ColorAt(%v).R = %v, want between %v and %v", tt.t, result.R, tt.minR, tt.maxR)
			}
		})
	}

	t.Run("empty gradient", func(t *testing.T) {
		empty := Gradient{}
		result := empty.ColorAt(0.5)
		if result.R != 128 {
			t.Errorf("empty gradient ColorAt should return gray, got %+v", result)
		}
	})
}

func TestGradient_PositionAt(t *testing.T) {
	g := NewGradient([]string{"#000000", "#FFFFFF"}, 0) // horizontal gradient

	tests := []struct {
		name                 string
		x, y, width, height  int
		angle                float64
		minPos, maxPos       float64
	}{
		{"top-left at 0deg", 0, 0, 10, 10, 0, 0, 0.01},
		{"top-right at 0deg", 9, 0, 10, 10, 0, 0.99, 1},
		{"zero width", 0, 0, 0, 10, 0, 0.49, 0.51},
		{"zero height", 0, 0, 10, 0, 0, 0.49, 0.51},
		{"both zero", 0, 0, 0, 0, 0, 0.49, 0.51},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testG := NewGradient([]string{"#000000", "#FFFFFF"}, tt.angle)
			result := testG.PositionAt(tt.x, tt.y, tt.width, tt.height)
			if result < tt.minPos || result > tt.maxPos {
				t.Errorf("PositionAt(%d, %d, %d, %d) = %v, want between %v and %v",
					tt.x, tt.y, tt.width, tt.height, result, tt.minPos, tt.maxPos)
			}
		})
	}
}

func TestGradient_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		colors   []string
		expected bool
	}{
		{"empty", []string{}, false},
		{"one color", []string{"#FF0000"}, false},
		{"two colors", []string{"#FF0000", "#0000FF"}, true},
		{"three colors", []string{"#FF0000", "#00FF00", "#0000FF"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGradient(tt.colors, 30)
			if g.IsValid() != tt.expected {
				t.Errorf("IsValid() = %v, want %v", g.IsValid(), tt.expected)
			}
		})
	}
}

func TestRGB_ToANSI(t *testing.T) {
	tests := []struct {
		name     string
		rgb      RGB
		expected string
	}{
		{"red", RGB{255, 0, 0}, "\x1b[38;2;255;0;0m"},
		{"black", RGB{0, 0, 0}, "\x1b[38;2;0;0;0m"},
		{"white", RGB{255, 255, 255}, "\x1b[38;2;255;255;255m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.rgb.ToANSI()
			if result != tt.expected {
				t.Errorf("ToANSI() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{255, "255"},
		{100, "100"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := itoa(tt.input)
			if result != tt.expected {
				t.Errorf("itoa(%d) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
