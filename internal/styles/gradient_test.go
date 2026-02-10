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
		{
			name:     "red",
			hex:      "#FF0000",
			expected: RGB{255, 0, 0},
		},
		{
			name:     "green",
			hex:      "#00FF00",
			expected: RGB{0, 255, 0},
		},
		{
			name:     "blue",
			hex:      "#0000FF",
			expected: RGB{0, 0, 255},
		},
		{
			name:     "white",
			hex:      "#FFFFFF",
			expected: RGB{255, 255, 255},
		},
		{
			name:     "black",
			hex:      "#000000",
			expected: RGB{0, 0, 0},
		},
		{
			name:     "lowercase hex",
			hex:      "#ff00ff",
			expected: RGB{255, 0, 255},
		},
		{
			name:     "mixed case",
			hex:      "#FfFfFf",
			expected: RGB{255, 255, 255},
		},
		{
			name:     "no hash prefix",
			hex:      "FF0000",
			expected: RGB{255, 0, 0},
		},
		{
			name:     "invalid - too short",
			hex:      "#FF00",
			expected: RGB{128, 128, 128}, // fallback gray
		},
		{
			name:     "invalid - empty",
			hex:      "",
			expected: RGB{128, 128, 128}, // fallback gray
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HexToRGB(tt.hex)
			if result.R != tt.expected.R || result.G != tt.expected.G || result.B != tt.expected.B {
				t.Errorf("HexToRGB(%q) = RGB{%v, %v, %v}, want RGB{%v, %v, %v}",
					tt.hex, result.R, result.G, result.B, tt.expected.R, tt.expected.G, tt.expected.B)
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
		{
			name:     "red",
			rgb:      RGB{255, 0, 0},
			expected: "#ff0000",
		},
		{
			name:     "green",
			rgb:      RGB{0, 255, 0},
			expected: "#00ff00",
		},
		{
			name:     "blue",
			rgb:      RGB{0, 0, 255},
			expected: "#0000ff",
		},
		{
			name:     "white",
			rgb:      RGB{255, 255, 255},
			expected: "#ffffff",
		},
		{
			name:     "black",
			rgb:      RGB{0, 0, 0},
			expected: "#000000",
		},
		{
			name:     "gray",
			rgb:      RGB{128, 128, 128},
			expected: "#808080",
		},
		{
			name:     "clamped overflow",
			rgb:      RGB{300, -50, 128},
			expected: "#ff0080",
		},
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

func TestHexRoundTrip(t *testing.T) {
	colors := []string{"#ff0000", "#00ff00", "#0000ff", "#ffffff", "#000000", "#808080"}

	for _, hex := range colors {
		rgb := HexToRGB(hex)
		result := RGBToHex(rgb)
		if result != hex {
			t.Errorf("Hex roundtrip failed for %s: got %s", hex, result)
		}
	}
}

func TestLerpRGB(t *testing.T) {
	tests := []struct {
		name     string
		c1       RGB
		c2       RGB
		t        float64
		expected RGB
		epsilon  float64
	}{
		{
			name:     "t=0 returns c1",
			c1:       RGB{0, 0, 0},
			c2:       RGB{255, 255, 255},
			t:        0,
			expected: RGB{0, 0, 0},
			epsilon:  0.1,
		},
		{
			name:     "t=1 returns c2",
			c1:       RGB{0, 0, 0},
			c2:       RGB{255, 255, 255},
			t:        1,
			expected: RGB{255, 255, 255},
			epsilon:  0.1,
		},
		{
			name:     "t=0.5 returns midpoint",
			c1:       RGB{0, 0, 0},
			c2:       RGB{100, 100, 100},
			t:        0.5,
			expected: RGB{50, 50, 50},
			epsilon:  0.1,
		},
		{
			name:     "partial interpolation",
			c1:       RGB{0, 100, 200},
			c2:       RGB{100, 150, 250},
			t:        0.5,
			expected: RGB{50, 125, 225},
			epsilon:  0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LerpRGB(tt.c1, tt.c2, tt.t)
			if math.Abs(result.R-tt.expected.R) > tt.epsilon ||
				math.Abs(result.G-tt.expected.G) > tt.epsilon ||
				math.Abs(result.B-tt.expected.B) > tt.epsilon {
				t.Errorf("LerpRGB(%+v, %+v, %v) = %+v, want %+v (±%v)",
					tt.c1, tt.c2, tt.t, result, tt.expected, tt.epsilon)
			}
		})
	}
}

func TestNewGradient(t *testing.T) {
	tests := []struct {
		name     string
		colors   []string
		angle    float64
		wantLen  int
		wantStop0 float64
	}{
		{
			name:     "single color",
			colors:   []string{"#FF0000"},
			angle:    30,
			wantLen:  1,
			wantStop0: 0.5,
		},
		{
			name:     "two colors",
			colors:   []string{"#FF0000", "#0000FF"},
			angle:    45,
			wantLen:  2,
			wantStop0: 0,
		},
		{
			name:     "three colors",
			colors:   []string{"#FF0000", "#00FF00", "#0000FF"},
			angle:    90,
			wantLen:  3,
			wantStop0: 0,
		},
		{
			name:     "empty colors",
			colors:   []string{},
			angle:    30,
			wantLen:  0,
			wantStop0: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewGradient(tt.colors, tt.angle)

			if len(result.Stops) != tt.wantLen {
				t.Errorf("NewGradient stops length = %d, want %d", len(result.Stops), tt.wantLen)
			}

			if tt.wantLen > 0 && result.Stops[0].Position != tt.wantStop0 {
				t.Errorf("NewGradient stops[0].Position = %v, want %v", result.Stops[0].Position, tt.wantStop0)
			}

			if result.Angle != tt.angle {
				t.Errorf("NewGradient angle = %v, want %v", result.Angle, tt.angle)
			}
		})
	}
}

func TestGradientColorAt(t *testing.T) {
	gradient := NewGradient([]string{"#000000", "#FFFFFF"}, 0)

	tests := []struct {
		name     string
		t        float64
		wantApprox RGB
		epsilon  float64
	}{
		{
			name:       "t=0",
			t:          0,
			wantApprox: RGB{0, 0, 0},
			epsilon:    1,
		},
		{
			name:       "t=1",
			t:          1,
			wantApprox: RGB{255, 255, 255},
			epsilon:    1,
		},
		{
			name:       "t=0.5",
			t:          0.5,
			wantApprox: RGB{127, 127, 127},
			epsilon:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gradient.ColorAt(tt.t)
			if math.Abs(result.R-tt.wantApprox.R) > tt.epsilon ||
				math.Abs(result.G-tt.wantApprox.G) > tt.epsilon ||
				math.Abs(result.B-tt.wantApprox.B) > tt.epsilon {
				t.Errorf("ColorAt(%v) = %+v, want %+v (±%v)",
					tt.t, result, tt.wantApprox, tt.epsilon)
			}
		})
	}
}

func TestGradientColorAtEdgeCases(t *testing.T) {
	emptyGradient := NewGradient([]string{}, 0)
	singleGradient := NewGradient([]string{"#FF0000"}, 0)

	t.Run("empty gradient", func(t *testing.T) {
		color := emptyGradient.ColorAt(0.5)
		expected := RGB{128, 128, 128}
		if color.R != expected.R || color.G != expected.G || color.B != expected.B {
			t.Errorf("ColorAt on empty gradient = %+v, want %+v", color, expected)
		}
	})

	t.Run("single color gradient", func(t *testing.T) {
		color := singleGradient.ColorAt(0.5)
		expected := HexToRGB("#FF0000")
		if color.R != expected.R || color.G != expected.G || color.B != expected.B {
			t.Errorf("ColorAt on single gradient = %+v, want %+v", color, expected)
		}
	})

	t.Run("out of bounds", func(t *testing.T) {
		gradient := NewGradient([]string{"#000000", "#FFFFFF"}, 0)
		color1 := gradient.ColorAt(-0.5)
		color2 := gradient.ColorAt(1.5)
		expected1 := RGB{0, 0, 0}
		expected2 := RGB{255, 255, 255}
		if color1.R != expected1.R || color2.R != expected2.R {
			t.Errorf("ColorAt with out of bounds t values failed")
		}
	})
}

func TestGradientPositionAt(t *testing.T) {
	gradient := NewGradient([]string{"#000000", "#FFFFFF"}, 0) // horizontal

	tests := []struct {
		name    string
		x, y    int
		width   int
		height  int
		wantMin float64
		wantMax float64
	}{
		{
			name:    "top-left",
			x:       0,
			y:       0,
			width:   100,
			height:  100,
			wantMin: 0,
			wantMax: 0.5,
		},
		{
			name:    "center",
			x:       50,
			y:       50,
			width:   100,
			height:  100,
			wantMin: 0.4,
			wantMax: 0.6,
		},
		{
			name:    "bottom-right",
			x:       99,
			y:       99,
			width:   100,
			height:  100,
			wantMin: 0.5,
			wantMax: 1,
		},
		{
			name:    "zero dimensions",
			x:       0,
			y:       0,
			width:   0,
			height:  0,
			wantMin: 0.4,
			wantMax: 0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gradient.PositionAt(tt.x, tt.y, tt.width, tt.height)
			if result < tt.wantMin || result > tt.wantMax {
				t.Errorf("PositionAt(%d, %d, %d, %d) = %v, want [%v, %v]",
					tt.x, tt.y, tt.width, tt.height, result, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestGradientIsValid(t *testing.T) {
	tests := []struct {
		name    string
		colors  []string
		wantValid bool
	}{
		{
			name:    "two colors",
			colors:  []string{"#000000", "#FFFFFF"},
			wantValid: true,
		},
		{
			name:    "three colors",
			colors:  []string{"#000000", "#808080", "#FFFFFF"},
			wantValid: true,
		},
		{
			name:    "one color",
			colors:  []string{"#000000"},
			wantValid: false,
		},
		{
			name:    "empty",
			colors:  []string{},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gradient := NewGradient(tt.colors, 0)
			result := gradient.IsValid()
			if result != tt.wantValid {
				t.Errorf("IsValid() = %v, want %v", result, tt.wantValid)
			}
		})
	}
}

func TestRGBToANSI(t *testing.T) {
	tests := []struct {
		name     string
		rgb      RGB
		contains string
	}{
		{
			name:     "red",
			rgb:      RGB{255, 0, 0},
			contains: "38;2;255;0;0",
		},
		{
			name:     "green",
			rgb:      RGB{0, 255, 0},
			contains: "38;2;0;255;0",
		},
		{
			name:     "blue",
			rgb:      RGB{0, 0, 255},
			contains: "38;2;0;0;255",
		},
		{
			name:     "with clamping",
			rgb:      RGB{300, -50, 128},
			contains: "38;2;255;0;128",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.rgb.ToANSI()
			if !contains(result, tt.contains) {
				t.Errorf("ToANSI() = %q, should contain %q", result, tt.contains)
			}
		})
	}
}

// Helper function for test
func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
