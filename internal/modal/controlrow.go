package modal

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/styles"
)

// A control row is a caption on the left and a row of pill controls on the
// right: the shape a collection modal uses for "N projects" beside its sort,
// view and add controls.
//
// It lives here rather than in one modal because the geometry — measuring
// pills, right-aligning them, spacing them, and registering a hit region whose
// bounds are exactly the drawn pill — is the same work every time, and it is
// the half that goes subtly wrong when it is written twice. What each modal
// keeps for itself is which controls it offers and what they mean.

// Control is one pill in a control row.
type Control struct {
	// ID is the action a press reports and the region the pointer hits.
	ID string
	// Label is the text inside the pill, without its padding.
	Label string
	// Active marks the control's state as the one currently in force — the
	// selected view, an open menu. It is a fact about the surface, not about
	// focus, and the two are drawn differently on purpose.
	Active bool
	// Focused marks the control the keyboard is talking to.
	Focused bool
}

// ControlRowOverlay lets a caller hang a popover off one of the controls. It
// receives each control's x offset within the section, in the order they were
// given, and returns nil when nothing is open.
type ControlRowOverlay func(anchors []int) *Overlay

// ControlRow renders lead on the left and controls on the right, all on one
// line. Controls that do not fit keep their order and their regions; the
// caption is what gives way first, because a caption that is one column short
// still reads and a pill that is one column short does not.
func ControlRow(lead string, controls []Control, overlay ControlRowOverlay) Section {
	return Custom(func(contentWidth int, focusID, hoverID string) RenderedSection {
		const gap = 1

		rendered := make([]string, len(controls))
		widths := make([]int, len(controls))
		total := 0
		for i, c := range controls {
			rendered[i] = controlStyle(c, hoverID == c.ID).Render(" " + c.Label + " ")
			widths[i] = ansi.StringWidth(rendered[i])
			total += widths[i]
		}
		if len(controls) > 0 {
			total += gap * (len(controls) - 1)
		}

		leadWidth := ansi.StringWidth(lead)
		if leadWidth+total+1 > contentWidth {
			// Trim the caption rather than the controls: the controls are the
			// only part of this row anyone can press.
			keep := max(0, contentWidth-total-1)
			lead = ansi.Truncate(lead, keep, "")
			leadWidth = ansi.StringWidth(lead)
		}
		padding := contentWidth - leadWidth - total
		if padding < 0 {
			// The controls alone are wider than the row. They keep their full
			// size and their regions: a half-drawn pill is a control the
			// pointer can still reach but the user cannot read, which is worse
			// than a row that overflows and is clipped by the modal.
			padding = 0
		} else if padding < 1 && leadWidth > 0 {
			padding = 1
		}

		var row strings.Builder
		row.WriteString(lead)
		row.WriteString(strings.Repeat(" ", padding))

		anchors := make([]int, len(controls))
		focusables := make([]FocusableInfo, 0, len(controls))
		x := leadWidth + padding
		for i, r := range rendered {
			if i > 0 {
				row.WriteString(strings.Repeat(" ", gap))
				x += gap
			}
			anchors[i] = x
			row.WriteString(r)
			focusables = append(focusables, FocusableInfo{
				ID: controls[i].ID, OffsetX: x, OffsetY: 0, Width: widths[i], Height: 1,
			})
			x += widths[i]
		}

		section := RenderedSection{Content: row.String(), Focusables: focusables}
		if overlay != nil {
			section.Overlay = overlay(anchors)
		}
		return section
	}, nil)
}

// controlStyle paints one pill in one of four states, which have to stay
// visibly distinct from each other.
//
// Focus outranks active state: the user needs to see where the keyboard is
// before they need to be reminded what is selected. So focus is the filled
// pill, and the active state is the accent in the text on the ordinary chrome —
// which also keeps "this view is selected" from looking like a button that is
// about to be pressed.
func controlStyle(c Control, hovered bool) lipgloss.Style {
	switch {
	case c.Focused:
		return styles.ButtonFocused
	case c.Active:
		return lipgloss.NewStyle().
			Foreground(styles.Accent).
			Background(styles.SurfaceRaised).
			Bold(true).
			Padding(0, 1)
	case hovered:
		return styles.ButtonHover
	}
	return styles.Button
}
