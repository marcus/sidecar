package modal

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/mouse"
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
	var seenHover string
	out := ControlRow("caption", []Control{{ID: "a", Label: "Sort ▾"}, {ID: "b", Label: "Grid"}},
		func(anchors []int, hoverID string) *Overlay {
			seen = anchors
			seenHover = hoverID
			return &Overlay{Content: "menu", OffsetX: anchors[0], OffsetY: 1}
		}).Render(50, "", "test-hover")

	if out.Overlay == nil {
		t.Fatal("the overlay was not attached")
	}
	if len(seen) != 2 {
		t.Fatalf("overlay was handed %d anchors, want one per control", len(seen))
	}
	if seenHover != "test-hover" {
		t.Fatalf("overlay was handed hoverID %q, want %q", seenHover, "test-hover")
	}
	if out.Overlay.OffsetX != out.Focusables[0].OffsetX {
		t.Fatalf("overlay at x=%d, want it anchored to its control at x=%d",
			out.Overlay.OffsetX, out.Focusables[0].OffsetX)
	}
}

func TestControlRowOverlayMouseInteractionAndBackdropAbsorption(t *testing.T) {
	m := New("Test Modal", WithWidth(60), WithHints(false)).
		AddSection(ControlRow("caption", []Control{{ID: "sort-btn", Label: "Sort ▾"}},
			func(anchors []int, hoverID string) *Overlay {
				return &Overlay{
					Content: "┌────────────┐\n│ Option 1   │\n│ Option 2   │\n└────────────┘",
					OffsetX: anchors[0],
					OffsetY: 1,
					Focusables: []FocusableInfo{
						{ID: "opt-1", OffsetX: 1, OffsetY: 1, Width: 12, Height: 1, MouseOnly: true},
						{ID: "opt-2", OffsetX: 1, OffsetY: 2, Width: 12, Height: 1, MouseOnly: true},
					},
				}
			})).
		AddSection(Custom(func(contentWidth int, focusID, hoverID string) RenderedSection {
			return RenderedSection{
				Content: "Underlying button content",
				Focusables: []FocusableInfo{
					{ID: "covered-btn", OffsetX: 0, OffsetY: 0, Width: 50, Height: 1},
				},
			}
		}, nil)).
		AddSection(Text("filler-1")).
		AddSection(Text("filler-2")).
		AddSection(Text("filler-3")).
		AddSection(Text("filler-4"))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	regions := handler.HitMap.Regions()
	var backdropRegion, opt1Region, coveredRegion *mouse.Region
	for i := range regions {
		switch regions[i].ID {
		case RegionOverlayBackdrop:
			backdropRegion = &regions[i]
		case "opt-1":
			opt1Region = &regions[i]
		case "covered-btn":
			coveredRegion = &regions[i]
		}
	}
	if backdropRegion == nil {
		t.Fatal("expected RegionOverlayBackdrop region to be registered")
	}
	if opt1Region == nil {
		t.Fatal("expected opt-1 overlay focusable region")
	}
	if coveredRegion == nil {
		t.Fatal("expected covered-btn region")
	}

	// 1. Hover over opt-1: should hover opt-1
	_ = m.HandleMouse(tea.MouseMotionMsg{X: opt1Region.Rect.X, Y: opt1Region.Rect.Y}, handler)
	if m.HoveredID() != "opt-1" {
		t.Fatalf("hover on opt-1 = %q, want opt-1", m.HoveredID())
	}

	// 2. Hover over overlay border (backdrop) where it covers the underlying button:
	// It must NOT hover covered-btn!
	hit := handler.HitMap.Test(backdropRegion.Rect.X, backdropRegion.Rect.Y)
	if hit == nil || hit.ID != RegionOverlayBackdrop {
		t.Fatalf("hit on overlay border = %v, want %s", hit, RegionOverlayBackdrop)
	}
	_ = m.HandleMouse(tea.MouseMotionMsg{X: backdropRegion.Rect.X, Y: backdropRegion.Rect.Y}, handler)
	if m.HoveredID() != "" {
		t.Fatalf("hover on overlay backdrop = %q, want empty (must not bleed through to %s)", m.HoveredID(), coveredRegion.ID)
	}

	// 3. Click on opt-1: must return "opt-1"
	action := m.HandleMouse(tea.MouseClickMsg{
		X:      opt1Region.Rect.X,
		Y:      opt1Region.Rect.Y,
		Button: tea.MouseLeft,
	}, handler)
	if action != "opt-1" {
		t.Fatalf("click on opt-1 action = %q, want opt-1", action)
	}

	// 4. Click on overlay backdrop: must absorb (return "")
	action = m.HandleMouse(tea.MouseClickMsg{
		X:      backdropRegion.Rect.X,
		Y:      backdropRegion.Rect.Y,
		Button: tea.MouseLeft,
	}, handler)
	if action != "" {
		t.Fatalf("click on overlay backdrop action = %q, want empty (absorbed)", action)
	}
}
