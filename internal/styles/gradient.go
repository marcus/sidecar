package styles

import (
	"math"
	"strings"
)

// RGB represents a color in RGB space for interpolation.
type RGB struct {
	R, G, B float64
}

// GradientStop defines a color at a position (0.0 to 1.0)
type GradientStop struct {
	Position float64
	Color    RGB
}

// Gradient defines a multi-stop color gradient with angle support.
type Gradient struct {
	Stops []GradientStop
	Angle float64 // degrees (0 = horizontal left-to-right, 90 = vertical top-to-bottom)
}

// DefaultGradientAngle is the default angle for gradient borders (30 degrees).
const DefaultGradientAngle = 30.0

// HexToRGB converts a hex color string (#RRGGBB) to RGB.
func HexToRGB(hex string) RGB {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return RGB{128, 128, 128} // fallback gray
	}

	var r, g, b uint8
	for i, chars := range []string{hex[0:2], hex[2:4], hex[4:6]} {
		val := hexToByte(chars)
		switch i {
		case 0:
			r = val
		case 1:
			g = val
		case 2:
			b = val
		}
	}
	return RGB{float64(r), float64(g), float64(b)}
}

// hexToByte converts a 2-character hex string to a byte.
func hexToByte(s string) uint8 {
	if len(s) != 2 {
		return 0
	}
	high := hexCharToNibble(s[0])
	low := hexCharToNibble(s[1])
	return high<<4 | low
}

// hexCharToNibble converts a single hex character to its value.
func hexCharToNibble(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}

// RGBToHex converts RGB back to a hex color string.
func RGBToHex(c RGB) string {
	r := clampByte(c.R)
	g := clampByte(c.G)
	b := clampByte(c.B)
	const hex = "0123456789abcdef"
	return string([]byte{'#',
		hex[r>>4], hex[r&0xf],
		hex[g>>4], hex[g&0xf],
		hex[b>>4], hex[b&0xf],
	})
}

// clampByte clamps a float64 to a uint8 range (0-255).
func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// ToANSI returns raw ANSI escape code for foreground color.
func (c RGB) ToANSI() string {
	r := clampByte(c.R)
	g := clampByte(c.G)
	b := clampByte(c.B)
	// Build ANSI escape sequence without fmt.Sprintf
	return "\x1b[38;2;" + itoa(int(r)) + ";" + itoa(int(g)) + ";" + itoa(int(b)) + "m"
}

// ANSIReset is the ANSI escape code to reset formatting.
const ANSIReset = "\x1b[0m"

// itoa converts a small integer to string without fmt package.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [3]byte // max 255
	pos := 2
	for i > 0 {
		buf[pos] = byte('0' + i%10)
		i /= 10
		pos--
	}
	return string(buf[pos+1:])
}

// LerpRGB linearly interpolates between two colors.
// t should be in range [0, 1] where 0 = c1 and 1 = c2.
func LerpRGB(c1, c2 RGB, t float64) RGB {
	return RGB{
		R: c1.R + (c2.R-c1.R)*t,
		G: c1.G + (c2.G-c1.G)*t,
		B: c1.B + (c2.B-c1.B)*t,
	}
}

// NewGradient creates a gradient from a slice of hex color strings.
// Colors are evenly distributed from position 0.0 to 1.0.
func NewGradient(hexColors []string, angle float64) Gradient {
	if len(hexColors) == 0 {
		return Gradient{Angle: angle}
	}

	stops := make([]GradientStop, len(hexColors))
	for i, hex := range hexColors {
		var pos float64
		if len(hexColors) == 1 {
			pos = 0.5
		} else {
			pos = float64(i) / float64(len(hexColors)-1)
		}
		stops[i] = GradientStop{
			Position: pos,
			Color:    HexToRGB(hex),
		}
	}

	return Gradient{
		Stops: stops,
		Angle: angle,
	}
}

// ColorAt returns the interpolated color at position t (0.0 to 1.0).
func (g *Gradient) ColorAt(t float64) RGB {
	if len(g.Stops) == 0 {
		return RGB{128, 128, 128}
	}
	if len(g.Stops) == 1 {
		return g.Stops[0].Color
	}

	// Clamp t to [0, 1]
	if t <= 0 {
		return g.Stops[0].Color
	}
	if t >= 1 {
		return g.Stops[len(g.Stops)-1].Color
	}

	// Find the two stops to interpolate between
	var lower, upper GradientStop
	lower = g.Stops[0]
	upper = g.Stops[len(g.Stops)-1]

	for i := 0; i < len(g.Stops)-1; i++ {
		if t >= g.Stops[i].Position && t <= g.Stops[i+1].Position {
			lower = g.Stops[i]
			upper = g.Stops[i+1]
			break
		}
	}

	// Calculate interpolation factor within this segment
	segmentLength := upper.Position - lower.Position
	if segmentLength <= 0 {
		return lower.Color
	}
	localT := (t - lower.Position) / segmentLength

	return LerpRGB(lower.Color, upper.Color, localT)
}

// PositionAt calculates the gradient position for a coordinate given the angle.
// For a 30-degree angle, the gradient flows diagonally from top-left to bottom-right.
// Returns a value in range [0, 1].
func (g *Gradient) PositionAt(x, y, width, height int) float64 {
	if width <= 0 && height <= 0 {
		return 0.5
	}

	// Convert angle to radians
	// We use negative angle because screen Y increases downward
	angleRad := g.Angle * math.Pi / 180.0

	// Calculate the gradient direction vector
	// For 0 degrees: horizontal (1, 0)
	// For 90 degrees: vertical (0, 1)
	// For 30 degrees: diagonal
	dx := math.Cos(angleRad)
	dy := math.Sin(angleRad)

	// Project the point onto the gradient direction
	// Normalize coordinates to [0, 1] range first
	var nx, ny float64
	if width > 1 {
		nx = float64(x) / float64(width-1)
	}
	if height > 1 {
		ny = float64(y) / float64(height-1)
	}

	// Project point onto gradient line
	projection := nx*dx + ny*dy

	// Normalize to [0, 1] based on the maximum possible projection
	maxProjection := math.Abs(dx) + math.Abs(dy)
	if maxProjection > 0 {
		projection = projection / maxProjection
	}

	// Clamp to [0, 1]
	if projection < 0 {
		return 0
	}
	if projection > 1 {
		return 1
	}
	return projection
}

// IsValid returns true if the gradient has at least 2 color stops.
func (g *Gradient) IsValid() bool {
	return len(g.Stops) >= 2
}
