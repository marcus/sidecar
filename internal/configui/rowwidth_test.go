package configui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A right-aligned control stops at MaxRowWidth instead of following the pane's
// right edge. Widening the terminal used to drag the ON/OFF pill away from the
// label it belongs to until the two read as unrelated columns.
func TestPanelRowControlStopsAtTheRowWidthCap(t *testing.T) {
	widths := []int{60, MaxRowWidth, 100, 160, 300}
	var settled int
	for _, width := range widths {
		row := ansi.Strip(PanelRow("Document panes", "", "", "ON", width, State{}))
		first := strings.Split(row, "\n")[0]
		end := len(strings.TrimRight(first, " "))
		if width <= MaxRowWidth {
			if end != width {
				t.Fatalf("at pane width %d the row ended at %d, want the full %d", width, end, width)
			}
			continue
		}
		if end > MaxRowWidth {
			t.Fatalf("at pane width %d the control reached column %d, past the %d cap", width, end, MaxRowWidth)
		}
		if settled == 0 {
			settled = end
			continue
		}
		// Every pane wider than the cap has to put the control in the same
		// place, or the pill still moves as the window grows — just later.
		if end != settled {
			t.Fatalf("at pane width %d the control ended at %d, but a narrower wide pane put it at %d", width, end, settled)
		}
	}
}

// The cap lines the pill up with where the widest form field already ends, so
// pills and fields share one right edge rather than each finding its own.
func TestRowWidthCapMatchesTheWidestFormField(t *testing.T) {
	const pane = 400
	// The field pads to its full width with its own background, so its right
	// edge is the rendered width — trimming the padding would measure where the
	// text stops, not where the control ends.
	field := ansi.Strip(FormRow("Terminal preview capture", StaticField("2 MB", ControlWidth(pane, MaxControlWidth), State{}), State{}))
	fieldEnd := len(field)

	// The pill is pushed right by padding that precedes it, so its right edge is
	// where the line stops.
	row := ansi.Strip(PanelRow("Document panes", "", "", "ON", pane, State{}))
	pillEnd := len(strings.TrimRight(strings.Split(row, "\n")[0], " "))

	if fieldEnd != pillEnd {
		t.Fatalf("form field ends at %d but the panel pill ends at %d; they should share an edge", fieldEnd, pillEnd)
	}
}

func TestRowWidthNeverExceedsThePane(t *testing.T) {
	for _, inner := range []int{0, 1, 10, 40, MaxRowWidth - 1, MaxRowWidth, MaxRowWidth + 1, 500} {
		if got := RowWidth(inner); got > inner && inner >= 0 {
			t.Fatalf("RowWidth(%d) = %d, wider than the pane it has to fit in", inner, got)
		}
		if got := RowWidth(inner); got > MaxRowWidth {
			t.Fatalf("RowWidth(%d) = %d, past the cap", inner, got)
		}
	}
}

// clampEnd is the one clamp whose argument is not what it looks like.
// ansi.TruncateLeft's count is how many columns to remove from the left, not
// how many to keep, and the ellipsis it prepends occupies one of the columns
// that survive. Passing the target width straight through cut a 43-column path
// to ten rather than to thirty-four, in the Projects page's path column and in
// the Integrations table's files column alike, and nothing failed: both callers
// pad the result, so a result that was far too short still filled its cell.
//
// The property is the whole contract: whatever comes back is exactly the width
// that was asked for, and it is the end of the string, because the end of a
// path is the part that identifies the file.
func TestClampEndKeepsExactlyTheWidthItWasAsked(t *testing.T) {
	const path = "/Users/someone/.config/opencode/plugin/sidecar-lifecycle.js"
	for width := 1; width <= ansi.StringWidth(path)+3; width++ {
		got := clampEnd(path, width)
		want := min(width, ansi.StringWidth(path))
		if w := ansi.StringWidth(got); w != want {
			t.Fatalf("clampEnd(width %d) returned %d columns: %q", width, w, got)
		}
		switch {
		case width >= ansi.StringWidth(path):
			if got != path {
				t.Fatalf("clampEnd(width %d) shortened a path that fits: %q", width, got)
			}
		case width == 1:
			if got != "…" {
				t.Fatalf("clampEnd(width 1) returned %q", got)
			}
		default:
			if !strings.HasPrefix(got, "…") {
				t.Fatalf("clampEnd(width %d) dropped the ellipsis: %q", width, got)
			}
			if !strings.HasSuffix(path, strings.TrimPrefix(got, "…")) {
				t.Fatalf("clampEnd(width %d) kept %q, which is not the end of the path", width, got)
			}
		}
	}
	if got := clampEnd(path, 0); got != "" {
		t.Fatalf("clampEnd(width 0) returned %q", got)
	}
}
