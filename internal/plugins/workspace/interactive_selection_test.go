package workspace

import (
	"testing"

	"github.com/marcus/sidecar/internal/mouse"
)

// newSelectionTestPlugin creates a Plugin with interactive state configured for selection tests.
// The pane starts at Y=2, has 1 row of content offset (tab bar), and shows lines 0-9.
func newSelectionTestPlugin() *Plugin {
	p := &Plugin{
		viewMode:     ViewModeInteractive,
		mouseHandler: mouse.NewHandler(),
		interactiveState: &InteractiveState{
			Active:           true,
			VisibleStart:     0,
			VisibleEnd:       10,
			ContentRowOffset: 1,
		},
		interactiveSelectionStart:  -1,
		interactiveSelectionEnd:    -1,
		interactiveSelectionAnchor: -1,
	}
	return p
}

// actionAt creates a mouse action at the given coordinates with the preview pane region.
func actionAt(x, y int) mouse.MouseAction {
	return mouse.MouseAction{
		Type: mouse.ActionClick,
		X:    x,
		Y:    y,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
}

func TestPrepareInteractiveDrag_NoSelection(t *testing.T) {
	p := newSelectionTestPlugin()

	// Y=6: contentRow = 6-2-1 = 3, outputRow = 3-1 = 2, lineIdx = 0+2 = 2
	action := actionAt(10, 6)
	p.prepareInteractiveDrag(action)

	if p.hasInteractiveSelection() {
		t.Error("click without drag should not create selection")
	}
	if p.interactiveSelectionStart != -1 {
		t.Errorf("start should be -1 (sentinel), got %d", p.interactiveSelectionStart)
	}
	if p.interactiveSelectionEnd != -1 {
		t.Errorf("end should be -1 (sentinel), got %d", p.interactiveSelectionEnd)
	}
	if p.interactiveSelectionAnchor != 2 {
		t.Errorf("anchor should be 2, got %d", p.interactiveSelectionAnchor)
	}
}

func TestDragAfterClick_CreatesSelection(t *testing.T) {
	p := newSelectionTestPlugin()

	// Click at line 2 (Y=6)
	action := actionAt(10, 6)
	p.prepareInteractiveDrag(action)

	// Drag to line 4 (Y=8)
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    10,
		Y:    8,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	if !p.hasInteractiveSelection() {
		t.Error("drag should create selection")
	}
	if !p.interactiveSelectionActive {
		t.Error("selection should be active after drag")
	}
	if p.interactiveSelectionStart != 2 {
		t.Errorf("start should be 2, got %d", p.interactiveSelectionStart)
	}
	if p.interactiveSelectionEnd != 4 {
		t.Errorf("end should be 4, got %d", p.interactiveSelectionEnd)
	}
}

func TestDragUpward_FromAnchor(t *testing.T) {
	p := newSelectionTestPlugin()

	// Click at line 4 (Y=8)
	action := actionAt(10, 8)
	p.prepareInteractiveDrag(action)

	// Drag up to line 1 (Y=5)
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    10,
		Y:    5,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	if !p.hasInteractiveSelection() {
		t.Error("upward drag should create selection")
	}
	if p.interactiveSelectionStart != 1 {
		t.Errorf("start should be 1, got %d", p.interactiveSelectionStart)
	}
	if p.interactiveSelectionEnd != 4 {
		t.Errorf("end should be 4, got %d", p.interactiveSelectionEnd)
	}
}

func TestFinishInteractiveSelection_UnstartedClears(t *testing.T) {
	p := newSelectionTestPlugin()

	// Click without drag
	action := actionAt(10, 6)
	p.prepareInteractiveDrag(action)

	// Finish without any drag motion
	p.finishInteractiveSelection()

	if p.hasInteractiveSelection() {
		t.Error("finish without drag should not leave selection")
	}
	if p.interactiveSelectionStart != -1 {
		t.Errorf("start should be -1 after clear, got %d", p.interactiveSelectionStart)
	}
	if p.interactiveSelectionAnchor != -1 {
		t.Errorf("anchor should be -1 after clear, got %d", p.interactiveSelectionAnchor)
	}
}

func TestFinishInteractiveSelection_AfterDrag(t *testing.T) {
	p := newSelectionTestPlugin()

	// Click and drag
	action := actionAt(10, 6)
	p.prepareInteractiveDrag(action)
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    10,
		Y:    8,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	// Finish
	p.finishInteractiveSelection()

	// Selection should persist (active=false but range preserved)
	if !p.hasInteractiveSelection() {
		t.Error("selection range should persist after finish")
	}
	if p.interactiveSelectionActive {
		t.Error("active should be false after finish")
	}
	if p.interactiveSelectionStart != 2 {
		t.Errorf("start should be 2, got %d", p.interactiveSelectionStart)
	}
	if p.interactiveSelectionEnd != 4 {
		t.Errorf("end should be 4, got %d", p.interactiveSelectionEnd)
	}
}

func TestClearInteractiveSelection_ResetsSentinels(t *testing.T) {
	p := newSelectionTestPlugin()

	// Create a valid selection
	action := actionAt(10, 6)
	p.prepareInteractiveDrag(action)
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    10,
		Y:    8,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	// Clear
	p.clearInteractiveSelection()

	if p.interactiveSelectionActive {
		t.Error("active should be false after clear")
	}
	if p.interactiveSelectionStart != -1 {
		t.Errorf("start should be -1, got %d", p.interactiveSelectionStart)
	}
	if p.interactiveSelectionEnd != -1 {
		t.Errorf("end should be -1, got %d", p.interactiveSelectionEnd)
	}
	if p.interactiveSelectionAnchor != -1 {
		t.Errorf("anchor should be -1, got %d", p.interactiveSelectionAnchor)
	}
	if p.hasInteractiveSelection() {
		t.Error("hasInteractiveSelection should return false after clear")
	}
}

func TestDragToSameLine_SelectsSingleLine(t *testing.T) {
	p := newSelectionTestPlugin()

	// Click at line 3 (Y=7)
	action := actionAt(10, 7)
	p.prepareInteractiveDrag(action)

	// Drag to same line (different X, same Y)
	dragAction := mouse.MouseAction{
		Type: mouse.ActionDrag,
		X:    50,
		Y:    7,
		Region: &mouse.Region{
			ID:   regionPreviewPane,
			Rect: mouse.Rect{X: 0, Y: 2, W: 80, H: 12},
		},
	}
	p.handleInteractiveSelectionDrag(dragAction)

	if !p.hasInteractiveSelection() {
		t.Error("drag to same line should create selection")
	}
	if p.interactiveSelectionStart != 3 {
		t.Errorf("start should be 3, got %d", p.interactiveSelectionStart)
	}
	if p.interactiveSelectionEnd != 3 {
		t.Errorf("end should be 3, got %d", p.interactiveSelectionEnd)
	}
}

func TestPrepareInteractiveDrag_InvalidY(t *testing.T) {
	p := newSelectionTestPlugin()

	// Click above content area (Y=2 → border row, contentRow=0 - 1 = -1 invalid)
	action := actionAt(10, 2)
	p.prepareInteractiveDrag(action)

	// Should have cleared selection
	if p.interactiveSelectionAnchor != -1 {
		t.Errorf("anchor should be -1 for invalid Y, got %d", p.interactiveSelectionAnchor)
	}
}

func TestPrepareInteractiveDrag_NilRegion(t *testing.T) {
	p := newSelectionTestPlugin()

	action := mouse.MouseAction{
		Type:   mouse.ActionClick,
		X:      10,
		Y:      6,
		Region: nil,
	}
	p.prepareInteractiveDrag(action)

	if p.interactiveSelectionAnchor != -1 {
		t.Errorf("anchor should remain -1 for nil region, got %d", p.interactiveSelectionAnchor)
	}
}

func TestIsInteractiveLineSelected(t *testing.T) {
	p := newSelectionTestPlugin()

	// Set up selection range [3, 5]
	p.interactiveSelectionStart = 3
	p.interactiveSelectionEnd = 5
	p.interactiveSelectionActive = true

	tests := []struct {
		lineIdx  int
		expected bool
	}{
		{2, false},
		{3, true},
		{4, true},
		{5, true},
		{6, false},
	}

	for _, tt := range tests {
		got := p.isInteractiveLineSelected(tt.lineIdx)
		if got != tt.expected {
			t.Errorf("isInteractiveLineSelected(%d) = %v, want %v", tt.lineIdx, got, tt.expected)
		}
	}
}

func TestHasInteractiveSelection_Sentinels(t *testing.T) {
	p := newSelectionTestPlugin()

	// Default: sentinels
	if p.hasInteractiveSelection() {
		t.Error("should return false with sentinel values")
	}

	// Only start set
	p.interactiveSelectionStart = 3
	if p.hasInteractiveSelection() {
		t.Error("should return false with only start set")
	}

	// Both set
	p.interactiveSelectionEnd = 5
	if !p.hasInteractiveSelection() {
		t.Error("should return true with both start and end set")
	}
}
