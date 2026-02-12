package mouse

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Rect represents a rectangular region.
type Rect struct {
	X, Y, W, H int
}

// Contains returns true if the point (x, y) is within the rectangle.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Region is a named rectangular hit region with associated data.
type Region struct {
	ID   string
	Rect Rect
	Data any
}

// HitMap tracks hit regions for mouse click detection.
type HitMap struct {
	regions []Region
}

// NewHitMap creates a new empty HitMap.
func NewHitMap() *HitMap {
	return &HitMap{
		regions: make([]Region, 0, 32),
	}
}

// Clear removes all regions from the hit map.
func (h *HitMap) Clear() {
	h.regions = h.regions[:0]
}

// Add adds a new region to the hit map.
func (h *HitMap) Add(id string, rect Rect, data any) {
	h.regions = append(h.regions, Region{
		ID:   id,
		Rect: rect,
		Data: data,
	})
}

// AddRect adds a region using individual coordinates.
func (h *HitMap) AddRect(id string, x, y, w, height int, data any) {
	h.Add(id, Rect{X: x, Y: y, W: w, H: height}, data)
}

// Test returns the first region containing the point, or nil if none.
func (h *HitMap) Test(x, y int) *Region {
	// Test in reverse order so later (topmost) regions take priority
	for i := len(h.regions) - 1; i >= 0; i-- {
		if h.regions[i].Rect.Contains(x, y) {
			return &h.regions[i]
		}
	}
	return nil
}

// Regions returns a copy of all registered regions (for testing).
func (h *HitMap) Regions() []Region {
	return append([]Region(nil), h.regions...)
}

// Handler combines a HitMap with mouse state tracking for drag and double-click detection.
type Handler struct {
	HitMap *HitMap

	// Click tracking for double-click detection
	lastClickX      int
	lastClickY      int
	lastClickTime   time.Time
	lastClickRegion string

	// Drag tracking
	dragging       bool
	dragStartX     int
	dragStartY     int
	dragStartValue int // Initial value when drag started (e.g., sidebar width)
	dragRegion     string
}

// NewHandler creates a new mouse handler.
func NewHandler() *Handler {
	return &Handler{
		HitMap: NewHitMap(),
	}
}

// ClickResult represents the result of processing a click event.
type ClickResult struct {
	Region        *Region
	IsDoubleClick bool
}

// HandleClick processes a mouse click and returns the hit region.
// Tracks click timing for double-click detection.
func (h *Handler) HandleClick(x, y int) ClickResult {
	region := h.HitMap.Test(x, y)

	result := ClickResult{Region: region}

	if region != nil {
		// Check for double-click (same region within 400ms)
		now := time.Now()
		if region.ID == h.lastClickRegion &&
			now.Sub(h.lastClickTime) < 400*time.Millisecond {
			result.IsDoubleClick = true
			// Reset to prevent triple-click counting as double
			h.lastClickRegion = ""
			h.lastClickTime = time.Time{}
		} else {
			h.lastClickRegion = region.ID
			h.lastClickTime = now
			h.lastClickX = x
			h.lastClickY = y
		}
	}

	return result
}

// StartDrag begins tracking a drag operation.
func (h *Handler) StartDrag(x, y int, regionID string, startValue int) {
	h.dragging = true
	h.dragStartX = x
	h.dragStartY = y
	h.dragStartValue = startValue
	h.dragRegion = regionID
}

// IsDragging returns true if a drag operation is in progress.
func (h *Handler) IsDragging() bool {
	return h.dragging
}

// DragRegion returns the region ID being dragged.
func (h *Handler) DragRegion() string {
	return h.dragRegion
}

// DragDelta returns the X and Y movement since drag started.
func (h *Handler) DragDelta(x, y int) (dx, dy int) {
	return x - h.dragStartX, y - h.dragStartY
}

// DragStartValue returns the initial value when the drag started.
func (h *Handler) DragStartValue() int {
	return h.dragStartValue
}

// EndDrag stops tracking the drag operation.
func (h *Handler) EndDrag() {
	h.dragging = false
	h.dragRegion = ""
}

// Clear resets the handler state and clears the hit map.
func (h *Handler) Clear() {
	h.HitMap.Clear()
}

// HandleMouse is a convenience method for processing tea.MouseMsg events.
// Returns the action to take based on the mouse event.
func (h *Handler) HandleMouse(msg tea.MouseMsg) MouseAction {
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			result := h.HandleClick(msg.X, msg.Y)
			if result.Region == nil {
				return MouseAction{Type: ActionNone}
			}
			if result.IsDoubleClick {
				return MouseAction{
					Type:   ActionDoubleClick,
					Region: result.Region,
					X:      msg.X,
					Y:      msg.Y,
				}
			}
			return MouseAction{
				Type:   ActionClick,
				Region: result.Region,
				X:      msg.X,
				Y:      msg.Y,
			}
		}
		if msg.Button == tea.MouseButtonWheelUp {
			region := h.HitMap.Test(msg.X, msg.Y)
			// Shift+scroll = horizontal scroll
			if msg.Shift {
				return MouseAction{
					Type:   ActionScrollLeft,
					Region: region,
					X:      msg.X,
					Y:      msg.Y,
					Delta:  -10,
				}
			}
			return MouseAction{
				Type:   ActionScrollUp,
				Region: region,
				X:      msg.X,
				Y:      msg.Y,
				Delta:  -3,
			}
		}
		if msg.Button == tea.MouseButtonWheelDown {
			region := h.HitMap.Test(msg.X, msg.Y)
			// Shift+scroll = horizontal scroll
			if msg.Shift {
				return MouseAction{
					Type:   ActionScrollRight,
					Region: region,
					X:      msg.X,
					Y:      msg.Y,
					Delta:  10,
				}
			}
			return MouseAction{
				Type:   ActionScrollDown,
				Region: region,
				X:      msg.X,
				Y:      msg.Y,
				Delta:  3,
			}
		}
		// Native horizontal scroll (trackpad) - reversed for Mac natural scrolling
		if msg.Button == tea.MouseButtonWheelLeft {
			region := h.HitMap.Test(msg.X, msg.Y)
			return MouseAction{
				Type:   ActionScrollRight,
				Region: region,
				X:      msg.X,
				Y:      msg.Y,
				Delta:  10,
			}
		}
		if msg.Button == tea.MouseButtonWheelRight {
			region := h.HitMap.Test(msg.X, msg.Y)
			return MouseAction{
				Type:   ActionScrollLeft,
				Region: region,
				X:      msg.X,
				Y:      msg.Y,
				Delta:  -10,
			}
		}

	case tea.MouseActionRelease:
		if h.dragging {
			h.EndDrag()
			return MouseAction{Type: ActionDragEnd}
		}

	case tea.MouseActionMotion:
		if h.dragging {
			dx, dy := h.DragDelta(msg.X, msg.Y)
			return MouseAction{
				Type:   ActionDrag,
				X:      msg.X,
				Y:      msg.Y,
				DragDX: dx,
				DragDY: dy,
			}
		}
		// Track hover for visual feedback
		region := h.HitMap.Test(msg.X, msg.Y)
		return MouseAction{
			Type:   ActionHover,
			Region: region,
			X:      msg.X,
			Y:      msg.Y,
		}
	}

	return MouseAction{Type: ActionNone}
}

// ActionType represents the type of mouse action detected.
type ActionType int

const (
	ActionNone ActionType = iota
	ActionClick
	ActionDoubleClick
	ActionScrollUp
	ActionScrollDown
	ActionScrollLeft  // Shift+scroll up = scroll left
	ActionScrollRight // Shift+scroll down = scroll right
	ActionDrag
	ActionDragEnd
	ActionHover
)

// MouseAction represents a processed mouse event.
type MouseAction struct {
	Type   ActionType
	Region *Region
	X, Y   int
	Delta  int // Scroll delta
	DragDX int // Drag delta X
	DragDY int // Drag delta Y
}
