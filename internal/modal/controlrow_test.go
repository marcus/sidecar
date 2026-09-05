package modal

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestControlRowRightAlignsAndRegistersOneRegionPerControl(t *testing.T) {
	section := ControlRow("8 projects", []Control{
		{ID: "sort", Label: "Sort: Name ▾"},
		{ID: "list", Label: "List", Active: true},
		{ID: "grid", Label: "Grid"},
	}, nil)

	const width = 60
	out := section.Render(width, "", "")
	plain := ansi.Strip(out.Content)

	if got := ansi.StringWidth(plain); got != width {
		t.Fatalf("row is %d wide, want exactly %d", got, width)
	}
	if !strings.HasPrefix(plain, "8 projects") {
		t.Fatalf("caption is not on the left: %q", plain)
	}
	last := out.Focusables[len(out.Focusables)-1]
	if last.OffsetX+last.Width != width {
		t.Fatalf("controls are not right-aligned: last ends at %d, want %d", last.OffsetX+last.Width, width)
	}
	if len(out.Focusables) != 3 {
		t.Fatalf("registered %d regions, want one per control", len(out.Focusables))
	}
	// Each region must cover exactly the drawn pill, or the pointer lands
	// beside the thing it looks like it is pressing.
	for _, f := range out.Focusables {
		drawn := ansi.Strip(ansi.Cut(out.Content, f.OffsetX, f.OffsetX+f.Width))
		if strings.TrimSpace(drawn) == "" {
			t.Fatalf("region %q covers blank space at x=%d", f.ID, f.OffsetX)
		}
	}
	if out.Focusables[0].OffsetX >= out.Focusables[1].OffsetX {
		t.Fatal("regions are not in the order the controls were given")
	}
}

func TestControlRowTrimsTheCaptionRatherThanTheControls(t *testing.T) {
	controls := []Control{{ID: "a", Label: "Sort: Last activity ▾"}, {ID: "b", Label: "Grid"}}
	const width = 40
	out := ControlRow("a very long caption that cannot possibly fit", controls, nil).Render(width, "", "")
	plain := ansi.Strip(out.Content)

	if got := ansi.StringWidth(plain); got != width {
		t.Fatalf("row is %d wide, want %d", got, width)
	}
	for _, want := range []string{"Sort: Last activity ▾", "Grid"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("control %q was dropped to fit the caption: %q", want, plain)
		}
	}
	if len(out.Focusables) != 2 {
		t.Fatalf("registered %d regions, want both controls still pressable", len(out.Focusables))
	}
}

func TestControlRowHangsAnOverlayOffTheControlItIsAnchoredTo(t *testing.T) {
	var seen []int
	out := ControlRow("caption", []Control{{ID: "a", Label: "Sort ▾"}, {ID: "b", Label: "Grid"}},
		func(anchors []int) *Overlay {
			seen = anchors
			return &Overlay{Content: "menu", OffsetX: anchors[0], OffsetY: 1}
		}).Render(50, "", "")

	if out.Overlay == nil {
		t.Fatal("the overlay was not attached")
	}
	if len(seen) != 2 {
		t.Fatalf("overlay was handed %d anchors, want one per control", len(seen))
	}
	if out.Overlay.OffsetX != out.Focusables[0].OffsetX {
		t.Fatalf("overlay at x=%d, want it anchored to its control at x=%d",
			out.Overlay.OffsetX, out.Focusables[0].OffsetX)
	}
}
