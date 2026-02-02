package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// BrailleSpinner renders an animated braille dot pattern.
// It is a passive component — it does not generate its own ticks.
// Call Tick() from an existing SkeletonTickMsg handler to advance the frame.
type BrailleSpinner struct {
	frame  int
	active bool
}

// Braille animation frames — a rolling wave pattern using braille dot characters.
var brailleFrames = []string{
	"⠋ ⠙ ⠹ ⠸",
	"⠙ ⠹ ⠸ ⠼",
	"⠹ ⠸ ⠼ ⠴",
	"⠸ ⠼ ⠴ ⠦",
	"⠼ ⠴ ⠦ ⠧",
	"⠴ ⠦ ⠧ ⠇",
	"⠦ ⠧ ⠇ ⠏",
	"⠧ ⠇ ⠏ ⠋",
	"⠇ ⠏ ⠋ ⠙",
	"⠏ ⠋ ⠙ ⠹",
}

// NewBrailleSpinner creates a new braille spinner (inactive by default).
func NewBrailleSpinner() BrailleSpinner {
	return BrailleSpinner{}
}

// Start marks the spinner as active.
func (b *BrailleSpinner) Start() {
	b.active = true
	b.frame = 0
}

// Stop halts the animation.
func (b *BrailleSpinner) Stop() {
	b.active = false
}

// IsActive returns whether the spinner is running.
func (b BrailleSpinner) IsActive() bool {
	return b.active
}

// Tick advances the animation frame. Call this from a SkeletonTickMsg handler.
func (b *BrailleSpinner) Tick() {
	if b.active {
		b.frame++
	}
}

// View renders the current spinner frame.
func (b BrailleSpinner) View() string {
	if !b.active {
		return ""
	}
	frame := brailleFrames[b.frame%len(brailleFrames)]
	return lipgloss.NewStyle().Foreground(styles.TextMuted).Render(frame)
}

// ViewFill renders the spinner centered in the given width, with a label.
func (b BrailleSpinner) ViewFill(width int, label string) string {
	if !b.active {
		return ""
	}
	frame := brailleFrames[b.frame%len(brailleFrames)]

	var sb strings.Builder
	if label != "" {
		labelStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
		sb.WriteString(labelStyle.Render(label))
		sb.WriteString("\n")
	}
	dotStyle := lipgloss.NewStyle().Foreground(styles.Accent)
	line := dotStyle.Render(frame)
	lineLen := len(frame)
	pad := (width - lineLen) / 2
	if pad > 0 {
		sb.WriteString(strings.Repeat(" ", pad))
	}
	sb.WriteString(line)
	return sb.String()
}
