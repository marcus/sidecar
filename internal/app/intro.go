package app

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// IntroModel handles the intro animation state.
type IntroModel struct {
	Active    bool
	StartTime time.Time
	Letters   []*IntroLetter
	Done      bool // Set to true when animation is finished

	// Repo name fade-in (starts after logo animation completes)
	RepoName      string
	RepoOpacity   float64   // 0.0 to 1.0
	RepoFadeStart time.Time // When the fade began
}

type IntroLetter struct {
	Char     rune
	TargetX  float64
	CurrentX float64

	// Overshoot logic
	ReachedTarget bool
	OvershootMax  float64

	// Color interpolation
	StartColor   RGB
	EndColor     RGB
	CurrentColor RGB

	Delay time.Duration
}

type RGB struct {
	R, G, B float64
}

func hexToRGB(hex string) RGB {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b uint8
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return RGB{float64(r), float64(g), float64(b)}
}

func (c RGB) toLipgloss() lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", int(c.R), int(c.G), int(c.B)))
}

func NewIntroModel(repoName string) IntroModel {
	text := "Sidecar"
	letters := make([]*IntroLetter, len(text))

	// Get theme colors for the animation
	theme := styles.GetCurrentTheme()

	// Gradient endpoints for the final state - use theme's accent/warning colors
	startGradient := hexToRGB(theme.Colors.Accent)
	endGradient := hexToRGB(theme.Colors.Warning)

	// Use theme colors for the varied start colors
	startColors := []string{
		theme.Colors.Error,     // Red
		theme.Colors.Secondary, // Blue/Cyan
		theme.Colors.Success,   // Green
		theme.Colors.Primary,   // Purple
		theme.Colors.ButtonHover, // Pink
		theme.Colors.Info,      // Cyan/Blue
		theme.Colors.Accent,    // Orange/Amber
	}

	for i, char := range text {
		// Calculate target color for this letter in the gradient
		t := float64(i) / float64(len(text)-1)
		targetColor := RGB{
			R: startGradient.R + t*(endGradient.R-startGradient.R),
			G: startGradient.G + t*(endGradient.G-startGradient.G),
			B: startGradient.B + t*(endGradient.B-startGradient.B),
		}

		letters[i] = &IntroLetter{
			Char:         char,
			CurrentX:     -15.0 - float64(i)*8.0,
			TargetX:      float64(i),
			OvershootMax: float64(i) * 2.5, // Spread out significantly (space between letters)
			StartColor:   hexToRGB(startColors[i%len(startColors)]),
			EndColor:     targetColor,
			CurrentColor: hexToRGB(startColors[i%len(startColors)]),
			Delay:        time.Duration(i) * 80 * time.Millisecond,
		}
	}

	return IntroModel{
		Active:   true,
		Letters:  letters,
		RepoName: repoName,
	}
}

// Update progresses the animation
func (m *IntroModel) Update(dt time.Duration) {
	if !m.Active {
		return
	}

	allSettled := true

	for _, l := range m.Letters {
		// Check delay
		if m.StartTime.IsZero() {
			m.StartTime = time.Now()
		}
		elapsed := time.Since(m.StartTime)
		if elapsed < l.Delay {
			allSettled = false
			continue
		}

		// Animation logic (Overshoot then return)
		// 1. Move towards OvershootMax until reached
		// 2. Then move back to TargetX

		var target float64
		var speed float64

		if !l.ReachedTarget {
			target = l.OvershootMax
			speed = 30.0
			
			if l.CurrentX >= l.OvershootMax - 0.1 {
				l.ReachedTarget = true
			}
		} else {
			target = l.TargetX
			speed = 5.0 // Slower return
		}

		dist := target - l.CurrentX
		move := dist * 6.0 * dt.Seconds()

		// Clamp move to avoid oscillating wildly
		if math.Abs(move) > math.Abs(dist) {
			move = dist
		}
		
		// Ensure minimum movement if far away
		minMove := speed * dt.Seconds()
		if math.Abs(dist) > 0.1 && math.Abs(move) < minMove {
			if dist > 0 {
				move = minMove
			} else {
				move = -minMove
			}
		}

		l.CurrentX += move

		// Color interpolation
		// Interpolate towards EndColor
		colorSpeed := 3.0 * dt.Seconds()
		l.CurrentColor.R += (l.EndColor.R - l.CurrentColor.R) * colorSpeed
		l.CurrentColor.G += (l.EndColor.G - l.CurrentColor.G) * colorSpeed
		l.CurrentColor.B += (l.EndColor.B - l.CurrentColor.B) * colorSpeed

		// Check if settled
		if l.ReachedTarget && 
		   math.Abs(l.TargetX-l.CurrentX) < 0.1 &&
		   math.Abs(l.EndColor.R-l.CurrentColor.R) < 1.0 {
			// Settled
		} else {
			allSettled = false
		}
	}

	if allSettled {
		m.Done = true
	}

	// Repo name fade-in (starts after logo animation completes)
	if m.Done && m.RepoName != "" && m.RepoOpacity < 1.0 {
		if m.RepoFadeStart.IsZero() {
			m.RepoFadeStart = time.Now()
		}
		// Fade duration: 300ms
		elapsed := time.Since(m.RepoFadeStart)
		m.RepoOpacity = math.Min(1.0, elapsed.Seconds()/0.3)
	}
}

func (m IntroModel) View() string {
	if !m.Active {
		return ""
	}

	// Calculate required width based on current positions
	minX := 0
	maxX := len(m.Letters) // Minimum width is the final string length

	for _, l := range m.Letters {
		x := int(math.Round(l.CurrentX))
		if x > maxX {
			maxX = x
		}
	}

	// Add a little padding to the width to avoid clipping the last character during movement
	width := maxX + 1
	buf := make([]string, width)
	for i := range buf {
		buf[i] = " "
	}

	for _, l := range m.Letters {
		x := int(math.Round(l.CurrentX))
		if x >= minX && x < width {
			style := lipgloss.NewStyle().Foreground(l.CurrentColor.toLipgloss()).Bold(true)
			buf[x] = style.Render(string(l.Char))
		}
	}

	return strings.Join(buf, "")
}

// RepoNameView returns the repo name with current fade opacity applied.
// Returns empty string if no repo name or opacity is 0.
func (m IntroModel) RepoNameView() string {
	if m.RepoName == "" || m.RepoOpacity <= 0 {
		return ""
	}

	// Get theme colors for the animation
	theme := styles.GetCurrentTheme()

	// Background color for fade-in interpolation - use theme background
	bgColor := hexToRGB(theme.Colors.BgSecondary)

	// Gradient using theme's highlight/primary colors
	// Light variant to dark variant
	lightColor := hexToRGB(theme.Colors.TextHighlight)
	darkColor := hexToRGB(theme.Colors.Primary)

	// Render " / " prefix in theme's secondary text color
	prefixTarget := hexToRGB(theme.Colors.TextSecondary)
	prefixColor := RGB{
		R: bgColor.R + (prefixTarget.R-bgColor.R)*m.RepoOpacity,
		G: bgColor.G + (prefixTarget.G-bgColor.G)*m.RepoOpacity,
		B: bgColor.B + (prefixTarget.B-bgColor.B)*m.RepoOpacity,
	}
	prefixStyle := lipgloss.NewStyle().Foreground(prefixColor.toLipgloss())
	result := prefixStyle.Render(" / ")

	// Render each character of repo name with gradient
	runes := []rune(m.RepoName)
	for i, r := range runes {
		// Calculate gradient position (0.0 = start/light, 1.0 = end/dark)
		var t float64
		if len(runes) > 1 {
			t = float64(i) / float64(len(runes)-1)
		}

		// Interpolate between light and dark color
		targetColor := RGB{
			R: lightColor.R + t*(darkColor.R-lightColor.R),
			G: lightColor.G + t*(darkColor.G-lightColor.G),
			B: lightColor.B + t*(darkColor.B-lightColor.B),
		}

		// Apply fade-in opacity
		currentColor := RGB{
			R: bgColor.R + (targetColor.R-bgColor.R)*m.RepoOpacity,
			G: bgColor.G + (targetColor.G-bgColor.G)*m.RepoOpacity,
			B: bgColor.B + (targetColor.B-bgColor.B)*m.RepoOpacity,
		}

		style := lipgloss.NewStyle().Foreground(currentColor.toLipgloss())
		result += style.Render(string(r))
	}

	return result
}

// IntroTickMsg is sent to update the animation frame.
type IntroTickMsg time.Time

func IntroTick() tea.Cmd {
	return tea.Tick(time.Millisecond*16, func(t time.Time) tea.Msg {
		return IntroTickMsg(t)
	})
}
