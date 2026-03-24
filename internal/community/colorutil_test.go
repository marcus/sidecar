package community

import (
	"math"
	"testing"
)

func TestHexToHSL(t *testing.T) {
	tests := []struct {
		hex       string
		wantH     float64
		wantS     float64
		wantL     float64
		tolerance float64
	}{
		{"#ff0000", 0, 1.0, 0.5, 1.0},   // pure red
		{"#00ff00", 120, 1.0, 0.5, 1.0}, // pure green
		{"#0000ff", 240, 1.0, 0.5, 1.0}, // pure blue
		{"#ffffff", 0, 0, 1.0, 0.01},    // white
		{"#000000", 0, 0, 0, 0.01},      // black
		{"#808080", 0, 0, 0.502, 0.01},  // gray
		{"#ff8000", 30, 1.0, 0.5, 1.0},  // orange
	}

	for _, tt := range tests {
		h, s, l := HexToHSL(tt.hex)
		if math.Abs(h-tt.wantH) > tt.tolerance {
			t.Errorf("HexToHSL(%s) h = %f, want %f", tt.hex, h, tt.wantH)
		}
		if math.Abs(s-tt.wantS) > tt.tolerance {
			t.Errorf("HexToHSL(%s) s = %f, want %f", tt.hex, s, tt.wantS)
		}
		if math.Abs(l-tt.wantL) > tt.tolerance {
			t.Errorf("HexToHSL(%s) l = %f, want %f", tt.hex, l, tt.wantL)
		}
	}
}

func TestHSLToHex(t *testing.T) {
	tests := []struct {
		h, s, l float64
		want    string
	}{
		{0, 1.0, 0.5, "#ff0000"},   // red
		{120, 1.0, 0.5, "#00ff00"}, // green
		{240, 1.0, 0.5, "#0000ff"}, // blue
		{0, 0, 1.0, "#ffffff"},     // white
		{0, 0, 0, "#000000"},       // black
	}

	for _, tt := range tests {
		got := HSLToHex(tt.h, tt.s, tt.l)
		if got != tt.want {
			t.Errorf("HSLToHex(%f, %f, %f) = %s, want %s", tt.h, tt.s, tt.l, got, tt.want)
		}
	}
}

func TestHSLRoundTrip(t *testing.T) {
	colors := []string{"#ff0000", "#00ff00", "#0000ff", "#ff8000", "#8000ff", "#1e1e2e"}
	for _, hex := range colors {
		h, s, l := HexToHSL(hex)
		got := HSLToHex(h, s, l)
		dist := ColorDistance(hex, got)
		if dist > 2.0 { // allow small rounding error
			t.Errorf("HSL round-trip(%s): got %s, distance %f", hex, got, dist)
		}
	}
}

func TestLuminance(t *testing.T) {
	tests := []struct {
		hex  string
		want float64
		tol  float64
	}{
		{"#ffffff", 1.0, 0.01},
		{"#000000", 0.0, 0.01},
		{"#ff0000", 0.2126, 0.01},
		{"#00ff00", 0.7152, 0.01},
		{"#0000ff", 0.0722, 0.01},
	}
	for _, tt := range tests {
		got := Luminance(tt.hex)
		if math.Abs(got-tt.want) > tt.tol {
			t.Errorf("Luminance(%s) = %f, want %f", tt.hex, got, tt.want)
		}
	}
}

func TestBlend(t *testing.T) {
	// 50% blend of black and white = gray
	got := Blend("#000000", "#ffffff", 0.5)
	dist := ColorDistance(got, "#808080")
	if dist > 2.0 {
		t.Errorf("Blend black+white 50%% = %s, want ~#808080", got)
	}

	// 0% blend = first color
	got = Blend("#ff0000", "#0000ff", 0.0)
	if got != "#ff0000" {
		t.Errorf("Blend 0%% = %s, want #ff0000", got)
	}

	// 100% blend = second color
	got = Blend("#ff0000", "#0000ff", 1.0)
	if got != "#0000ff" {
		t.Errorf("Blend 100%% = %s, want #0000ff", got)
	}
}

func TestLightenDarken(t *testing.T) {
	// Lighten black
	got := Lighten("#000000", 0.5)
	if Luminance(got) < 0.1 {
		t.Errorf("Lighten(black, 0.5) = %s, too dark", got)
	}

	// Darken white
	got = Darken("#ffffff", 0.5)
	if Luminance(got) > 0.9 {
		t.Errorf("Darken(white, 0.5) = %s, too bright", got)
	}

	// Lighten should increase luminance
	base := "#404040"
	lighter := Lighten(base, 0.2)
	if Luminance(lighter) <= Luminance(base) {
		t.Errorf("Lighten(%s) = %s, not brighter", base, lighter)
	}

	// Darken should decrease luminance
	darker := Darken(base, 0.2)
	if Luminance(darker) >= Luminance(base) {
		t.Errorf("Darken(%s) = %s, not darker", base, darker)
	}
}

func TestSaturation(t *testing.T) {
	// Pure colors have saturation 1.0
	if s := Saturation("#ff0000"); math.Abs(s-1.0) > 0.01 {
		t.Errorf("Saturation(red) = %f, want 1.0", s)
	}
	// Grays have saturation 0
	if s := Saturation("#808080"); s > 0.01 {
		t.Errorf("Saturation(gray) = %f, want 0", s)
	}
}

func TestHueDegrees(t *testing.T) {
	tests := []struct {
		hex  string
		want float64
	}{
		{"#ff0000", 0},
		{"#00ff00", 120},
		{"#0000ff", 240},
	}
	for _, tt := range tests {
		got := HueDegrees(tt.hex)
		if math.Abs(got-tt.want) > 1.0 {
			t.Errorf("HueDegrees(%s) = %f, want %f", tt.hex, got, tt.want)
		}
	}
}

func TestColorDistance(t *testing.T) {
	// Same color = 0
	if d := ColorDistance("#ff0000", "#ff0000"); d != 0 {
		t.Errorf("ColorDistance same = %f, want 0", d)
	}
	// Black to white = sqrt(255^2 * 3) ≈ 441.67
	d := ColorDistance("#000000", "#ffffff")
	if math.Abs(d-441.67) > 1.0 {
		t.Errorf("ColorDistance black-white = %f, want ~441.67", d)
	}
}

func TestContrastRatio(t *testing.T) {
	// White on black = 21:1
	ratio := ContrastRatio("#ffffff", "#000000")
	if math.Abs(ratio-21.0) > 0.01 {
		t.Errorf("white/black ratio = %f, want 21.0", ratio)
	}
	// Same color = 1:1
	ratio = ContrastRatio("#808080", "#808080")
	if math.Abs(ratio-1.0) > 0.01 {
		t.Errorf("same color ratio = %f, want 1.0", ratio)
	}
	// Order shouldn't matter
	r1 := ContrastRatio("#ffffff", "#333333")
	r2 := ContrastRatio("#333333", "#ffffff")
	if math.Abs(r1-r2) > 0.001 {
		t.Errorf("ratio not symmetric: %f vs %f", r1, r2)
	}
}

func TestEnsureContrast(t *testing.T) {
	// Already sufficient contrast - returns original
	result := EnsureContrast("#ffffff", "#000000", 3.0)
	if result != "#ffffff" {
		t.Errorf("already sufficient: got %s, want #ffffff", result)
	}

	// Dark grey on dark background - should lighten
	result = EnsureContrast("#333333", "#1a1a1a", 3.0)
	ratio := ContrastRatio(result, "#1a1a1a")
	if ratio < 3.0 {
		t.Errorf("dark bg: ratio %f < 3.0 (result=%s)", ratio, result)
	}

	// Light grey on light background - should darken
	result = EnsureContrast("#cccccc", "#ffffff", 3.0)
	ratio = ContrastRatio(result, "#ffffff")
	if ratio < 3.0 {
		t.Errorf("light bg: ratio %f < 3.0 (result=%s)", ratio, result)
	}
}
