package tdmonitor

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// Stallion ASCII art - a galloping horse
const stallionArt = "" +
	"                 >\\/7\n" +
	"             _.-(6'  \\\n" +
	"            (=___._/` \\\n" +
	"                 )  \\ |\n" +
	"                /   / |\n" +
	"               /    > /\n" +
	"              j    < _\\\n" +
	"          _.-' :      ``.\n" +
	"          \\ r=._\\        `.\n" +
	"         <`\\\\_  \\         . `-. \n" +
	"          \\ r-7  `-. ._  ' .  `\\\n" +
	"           \\`,      `-.`7  7)   )\n" +
	"            \\/         \\|  \\'  / `-._\n" +
	"                       ||    .'\n" +
	"                        \\\\  (\n" +
	"                         >\\  >\n" +
	"                     ,.-' >.'\n" +
	"                    <.'_.''\n" +
	"                      <'\n"

// getThemeAnimationColors returns the current theme's animation colors as RGB.
// Uses Primary (purple), Secondary (blue), and Accent (amber/orange) from the theme.
func getThemeAnimationColors() (RGB, RGB, RGB) {
	theme := styles.GetCurrentTheme()
	return hexToRGB(theme.Colors.Primary),
		hexToRGB(theme.Colors.Secondary),
		hexToRGB(theme.Colors.Accent)
}

// RGB represents a color in RGB space for interpolation.
type RGB struct {
	R, G, B float64
}

// hexToRGB converts a hex color string to RGB.
func hexToRGB(hex string) RGB {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b uint8
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return RGB{float64(r), float64(g), float64(b)}
}

// toLipgloss converts RGB back to a lipgloss Color.
func (c RGB) toLipgloss() lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", int(c.R), int(c.G), int(c.B)))
}

// toANSI returns raw ANSI escape code for the color.
func (c RGB) toANSI() string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", int(c.R), int(c.G), int(c.B))
}

const ansiReset = "\x1b[0m"

// lerpRGB linearly interpolates between two colors.
func lerpRGB(c1, c2 RGB, t float64) RGB {
	return RGB{
		R: c1.R + (c2.R-c1.R)*t,
		G: c1.G + (c2.G-c1.G)*t,
		B: c1.B + (c2.B-c1.B)*t,
	}
}

// NotInstalledModel handles the animated "td not installed" view.
type NotInstalledModel struct {
	startTime time.Time
	width     int
	height    int
}

// NewNotInstalledModel creates a new not-installed view model.
func NewNotInstalledModel() *NotInstalledModel {
	return &NotInstalledModel{
		startTime: time.Now(),
	}
}

// StallionTickMsg is sent to update the animation frame.
type StallionTickMsg time.Time

// StallionTick returns a command that ticks for animation.
func StallionTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return StallionTickMsg(t)
	})
}

// Init returns the initial command (starts animation).
func (m *NotInstalledModel) Init() tea.Cmd {
	return StallionTick()
}

// Update handles messages for the not-installed view.
func (m *NotInstalledModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case StallionTickMsg:
		return StallionTick()
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return nil
}

// gradientColorAt returns the color for a character based on its position and time.
// Creates a smooth rolling wave effect across the image.
func (m *NotInstalledModel) gradientColorAt(charIndex, totalChars int) RGB {
	elapsed := time.Since(m.startTime).Seconds()
	cycleDuration := 8.0 // seconds for one full color cycle

	// Character's position in the art (0 to 1)
	charPos := float64(charIndex) / float64(totalChars)

	// Create a smooth rolling phase based on position and time
	// The wave travels through the art over time
	phase := math.Mod(charPos-elapsed/cycleDuration, 1.0)
	if phase < 0 {
		phase += 1.0
	}

	// Get current theme colors for animation
	colorPrimary, colorSecondary, colorAccent := getThemeAnimationColors()

	// Smooth three-color gradient: primary -> secondary -> accent -> primary
	// Using sine-based interpolation for smoother transitions
	return threewayGradient(phase, colorPrimary, colorSecondary, colorAccent)
}

// threewayGradient smoothly interpolates between three colors in a cycle.
func threewayGradient(t float64, c1, c2, c3 RGB) RGB {
	// t is 0-1, we divide into three segments with smooth transitions
	t = math.Mod(t, 1.0)
	if t < 0 {
		t += 1.0
	}

	// Use cosine interpolation for smoother transitions
	if t < 1.0/3.0 {
		// c1 -> c2
		blend := smoothstep(t * 3.0)
		return lerpRGB(c1, c2, blend)
	} else if t < 2.0/3.0 {
		// c2 -> c3
		blend := smoothstep((t - 1.0/3.0) * 3.0)
		return lerpRGB(c2, c3, blend)
	} else {
		// c3 -> c1
		blend := smoothstep((t - 2.0/3.0) * 3.0)
		return lerpRGB(c3, c1, blend)
	}
}

// smoothstep provides smooth easing (ease-in-out).
func smoothstep(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t * t * (3 - 2*t)
}

// renderStallion returns the stallion art with animated gradient sweep.
func (m *NotInstalledModel) renderStallion() string {
	lines := strings.Split(stallionArt, "\n")

	// Count total visible characters for position calculation
	var totalChars int
	for _, line := range lines {
		for _, ch := range line {
			if ch != ' ' && ch != '\t' {
				totalChars++
			}
		}
	}

	// Render each character with its gradient color using raw ANSI codes
	// (lipgloss per-character styling causes width calculation issues)
	var result strings.Builder
	charIndex := 0

	for _, line := range lines {
		for _, ch := range line {
			if ch == ' ' || ch == '\t' {
				result.WriteRune(ch)
			} else {
				color := m.gradientColorAt(charIndex, totalChars)
				result.WriteString(color.toANSI())
				result.WriteRune(ch)
				result.WriteString(ansiReset)
				charIndex++
			}
		}
		result.WriteRune('\n')
	}

	return result.String()
}

// renderPitch returns the professional pitch copy.
func (m *NotInstalledModel) renderPitch() string {
	// Use theme-aware styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.TextHighlight)

	mutedStyle := styles.Muted

	textStyle := lipgloss.NewStyle().
		Foreground(styles.TextSecondary)

	bulletStyle := lipgloss.NewStyle().
		Foreground(styles.TextPrimary)

	linkStyle := styles.Link

	codeBoxStyle := lipgloss.NewStyle().
		Foreground(styles.Success).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BorderNormal).
		Padding(0, 1)

	// Build content
	var b strings.Builder

	// Explain why they're seeing this screen
	b.WriteString(mutedStyle.Render("td is not installed in this project."))
	b.WriteString("\n\n")

	b.WriteString(titleStyle.Render("External memory for AI sessions"))
	b.WriteString("\n\n")

	b.WriteString(textStyle.Render("td gives each session:"))
	b.WriteString("\n")
	b.WriteString(bulletStyle.Render("  • Current focus and pending work"))
	b.WriteString("\n")
	b.WriteString(bulletStyle.Render("  • Decisions and their reasoning"))
	b.WriteString("\n")
	b.WriteString(bulletStyle.Render("  • Structured handoffs between sessions"))
	b.WriteString("\n\n")

	b.WriteString(mutedStyle.Render("Local SQLite. No cloud. Git-friendly."))
	b.WriteString("\n\n")

	// GitHub link
	b.WriteString(textStyle.Render("Learn more: "))
	b.WriteString(linkStyle.Render("https://github.com/marcus/td"))
	b.WriteString("\n\n")

	installCmd := "go install github.com/marcus/td/cmd/td@latest\ntd init"
	b.WriteString(codeBoxStyle.Render(installCmd))

	return b.String()
}

// View renders the complete not-installed screen.
func (m *NotInstalledModel) View(width, height int) string {
	m.width = width
	m.height = height

	stallion := m.renderStallion()
	pitch := m.renderPitch()

	// Get stallion width to center pitch within it
	stallionWidth := lipgloss.Width(stallion)
	centeredPitch := lipgloss.PlaceHorizontal(stallionWidth, lipgloss.Center, pitch)

	// Combine vertically - use Left to preserve stallion's whitespace alignment
	// (Center causes ANSI width miscalculation issues)
	content := lipgloss.JoinVertical(lipgloss.Left, stallion, centeredPitch)

	// Center in available space
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}
