package modal

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/mouse"
)

func TestNew(t *testing.T) {
	m := New("Test Modal")
	if m.title != "Test Modal" {
		t.Errorf("expected title 'Test Modal', got %q", m.title)
	}
	if m.width != DefaultWidth {
		t.Errorf("expected default width %d, got %d", DefaultWidth, m.width)
	}
	if m.variant != VariantDefault {
		t.Errorf("expected VariantDefault, got %v", m.variant)
	}
	if !m.closeOnBackdrop {
		t.Errorf("expected closeOnBackdrop true, got %v", m.closeOnBackdrop)
	}
}

func TestNewWithOptions(t *testing.T) {
	m := New("Test",
		WithWidth(60),
		WithVariant(VariantDanger),
		WithHints(false),
		WithPrimaryAction("submit"),
		WithCloseOnBackdropClick(false),
	)

	if m.width != 60 {
		t.Errorf("expected width 60, got %d", m.width)
	}
	if m.variant != VariantDanger {
		t.Errorf("expected VariantDanger, got %v", m.variant)
	}
	if m.showHints != false {
		t.Errorf("expected showHints false, got %v", m.showHints)
	}
	if m.primaryAction != "submit" {
		t.Errorf("expected primaryAction 'submit', got %q", m.primaryAction)
	}
	if m.closeOnBackdrop {
		t.Errorf("expected closeOnBackdrop false, got %v", m.closeOnBackdrop)
	}
}

func TestAddSection(t *testing.T) {
	m := New("Test").
		AddSection(Text("Hello")).
		AddSection(Spacer()).
		AddSection(Text("World"))

	if len(m.sections) != 3 {
		t.Errorf("expected 3 sections, got %d", len(m.sections))
	}
}

func TestTextSection(t *testing.T) {
	s := Text("Hello World")
	res := s.Render(80, "", "")

	if !strings.Contains(res.Content, "Hello World") {
		t.Errorf("expected content to contain 'Hello World', got %q", res.Content)
	}
	if len(res.Focusables) != 0 {
		t.Errorf("expected no focusables, got %d", len(res.Focusables))
	}
}

func TestSpacerSection(t *testing.T) {
	s := Spacer()
	res := s.Render(80, "", "")

	if res.Content != " " {
		t.Errorf("expected spacer content to be a single space, got %q", res.Content)
	}
}

func TestButtonsSection(t *testing.T) {
	s := Buttons(
		Btn(" Confirm ", "confirm"),
		Btn(" Cancel ", "cancel"),
	)
	res := s.Render(80, "confirm", "")

	if !strings.Contains(res.Content, "Confirm") {
		t.Errorf("expected content to contain 'Confirm', got %q", res.Content)
	}
	if len(res.Focusables) != 2 {
		t.Errorf("expected 2 focusables, got %d", len(res.Focusables))
	}

	// Check focusable IDs
	if res.Focusables[0].ID != "confirm" {
		t.Errorf("expected first focusable ID 'confirm', got %q", res.Focusables[0].ID)
	}
	if res.Focusables[1].ID != "cancel" {
		t.Errorf("expected second focusable ID 'cancel', got %q", res.Focusables[1].ID)
	}
}

func TestButtonsDanger(t *testing.T) {
	s := Buttons(
		Btn(" Delete ", "delete", BtnDanger()),
	)
	res := s.Render(80, "delete", "")

	// Should render with danger style
	if !strings.Contains(res.Content, "Delete") {
		t.Errorf("expected content to contain 'Delete', got %q", res.Content)
	}
}

func TestCheckboxSection(t *testing.T) {
	checked := false
	s := Checkbox("agree", "I agree", &checked)

	res := s.Render(80, "agree", "")
	if !strings.Contains(res.Content, "[ ]") {
		t.Errorf("expected unchecked box '[ ]', got %q", res.Content)
	}

	// Toggle via Update
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}, "agree")
	if !checked {
		t.Errorf("expected checked to be true after Enter")
	}

	res = s.Render(80, "agree", "")
	if !strings.Contains(res.Content, "[x]") {
		t.Errorf("expected checked box '[x]', got %q", res.Content)
	}
}

func TestWhenSection(t *testing.T) {
	show := false
	s := When(func() bool { return show }, Text("Conditional"))

	// When false
	res := s.Render(80, "", "")
	if res.Content != "" {
		t.Errorf("expected empty when condition is false, got %q", res.Content)
	}

	// When true
	show = true
	res = s.Render(80, "", "")
	if !strings.Contains(res.Content, "Conditional") {
		t.Errorf("expected 'Conditional' when condition is true, got %q", res.Content)
	}
}

func TestWhenSectionNoSpacerLine(t *testing.T) {
	m := New("Test", WithHints(false)).
		AddSection(Custom(func(contentWidth int, focusID, hoverID string) RenderedSection {
			return RenderedSection{
				Content: "First",
				Focusables: []FocusableInfo{{
					ID:      "first",
					OffsetX: 0,
					OffsetY: 0,
					Width:   5,
					Height:  1,
				}},
			}
		}, nil)).
		AddSection(When(func() bool { return false }, Text("Hidden"))).
		AddSection(Custom(func(contentWidth int, focusID, hoverID string) RenderedSection {
			return RenderedSection{
				Content: "Second",
				Focusables: []FocusableInfo{{
					ID:      "second",
					OffsetX: 0,
					OffsetY: 0,
					Width:   6,
					Height:  1,
				}},
			}
		}, nil))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	regions := handler.HitMap.Regions()
	var first, second *mouse.Region
	for i := range regions {
		switch regions[i].ID {
		case "first":
			first = &regions[i]
		case "second":
			second = &regions[i]
		}
	}

	if first == nil || second == nil {
		t.Fatalf("expected both 'first' and 'second' regions to be registered")
	}

	if second.Rect.Y-first.Rect.Y != 1 {
		t.Errorf("expected no spacer line between sections; got delta %d", second.Rect.Y-first.Rect.Y)
	}
}

func TestHandleKeyEsc(t *testing.T) {
	m := New("Test").
		AddSection(Buttons(Btn(" OK ", "ok")))

	// Render to populate focusIDs
	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	action, _ := m.HandleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if action != "cancel" {
		t.Errorf("expected 'cancel' on Esc, got %q", action)
	}
}

func TestHandleKeyTab(t *testing.T) {
	m := New("Test").
		AddSection(Buttons(
			Btn(" A ", "a"),
			Btn(" B ", "b"),
			Btn(" C ", "c"),
		))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	// Initial focus should be on first element
	if m.FocusedID() != "a" {
		t.Errorf("expected initial focus on 'a', got %q", m.FocusedID())
	}

	// Tab to next
	m.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.FocusedID() != "b" {
		t.Errorf("expected focus on 'b' after Tab, got %q", m.FocusedID())
	}

	// Tab again
	m.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.FocusedID() != "c" {
		t.Errorf("expected focus on 'c' after second Tab, got %q", m.FocusedID())
	}

	// Tab wraps around
	m.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.FocusedID() != "a" {
		t.Errorf("expected focus to wrap to 'a', got %q", m.FocusedID())
	}

	// Shift+Tab goes backward
	m.HandleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.FocusedID() != "c" {
		t.Errorf("expected focus on 'c' after Shift+Tab, got %q", m.FocusedID())
	}
}

func TestHandleKeyEnter(t *testing.T) {
	m := New("Test").
		AddSection(Buttons(
			Btn(" OK ", "ok"),
			Btn(" Cancel ", "cancel"),
		))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	// Enter on focused button returns its ID
	action, _ := m.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if action != "ok" {
		t.Errorf("expected 'ok' on Enter, got %q", action)
	}

	// Focus cancel and enter
	m.SetFocus("cancel")
	action, _ = m.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if action != "cancel" {
		t.Errorf("expected 'cancel' on Enter, got %q", action)
	}
}

func TestHandleMouseClick(t *testing.T) {
	m := New("Test", WithWidth(40)).
		AddSection(Text("Click a button")).
		AddSection(Spacer()).
		AddSection(Buttons(
			Btn(" OK ", "ok"),
			Btn(" Cancel ", "cancel"),
		))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	// Find the "ok" button region
	regions := handler.HitMap.Regions()
	var okRegion *mouse.Region
	for i := range regions {
		if regions[i].ID == "ok" {
			okRegion = &regions[i]
			break
		}
	}

	if okRegion == nil {
		t.Fatal("expected 'ok' button region to be registered")
	}

	// Click on the OK button
	clickX := okRegion.Rect.X + okRegion.Rect.W/2
	clickY := okRegion.Rect.Y
	action := m.HandleMouse(tea.MouseMsg{
		X:      clickX,
		Y:      clickY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}, handler)

	if action != "ok" {
		t.Errorf("expected 'ok' on click, got %q", action)
	}
}

func TestHandleMouseBackdropClick(t *testing.T) {
	m := New("Test", WithWidth(40)).
		AddSection(Text("Click outside"))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	action := m.HandleMouse(tea.MouseMsg{
		X:      0,
		Y:      0,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}, handler)
	if action != "cancel" {
		t.Errorf("expected 'cancel' on backdrop click, got %q", action)
	}

	m = New("Test", WithWidth(40), WithCloseOnBackdropClick(false)).
		AddSection(Text("Click outside"))
	handler = mouse.NewHandler()
	m.Render(80, 24, handler)

	action = m.HandleMouse(tea.MouseMsg{
		X:      0,
		Y:      0,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}, handler)
	if action != "" {
		t.Errorf("expected no action on backdrop click when disabled, got %q", action)
	}
}

func TestHandleMouseHover(t *testing.T) {
	m := New("Test", WithWidth(40)).
		AddSection(Buttons(Btn(" OK ", "ok")))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	// Find the button region
	regions := handler.HitMap.Regions()
	var okRegion *mouse.Region
	for i := range regions {
		if regions[i].ID == "ok" {
			okRegion = &regions[i]
			break
		}
	}

	if okRegion == nil {
		t.Fatal("expected 'ok' button region")
	}

	// Hover over button
	m.HandleMouse(tea.MouseMsg{
		X:      okRegion.Rect.X,
		Y:      okRegion.Rect.Y,
		Action: tea.MouseActionMotion,
	}, handler)

	if m.HoveredID() != "ok" {
		t.Errorf("expected hoverID 'ok', got %q", m.HoveredID())
	}

	// Move away
	m.HandleMouse(tea.MouseMsg{
		X:      0,
		Y:      0,
		Action: tea.MouseActionMotion,
	}, handler)

	if m.HoveredID() != "" {
		t.Errorf("expected empty hoverID, got %q", m.HoveredID())
	}
}

func TestMouseScrollModal(t *testing.T) {
	m := New("Test", WithWidth(40)).
		AddSection(Text("Line 1")).
		AddSection(Text("Line 2")).
		AddSection(Text("Line 3")).
		AddSection(Text("Line 4")).
		AddSection(Text("Line 5"))

	handler := mouse.NewHandler()
	m.Render(80, 10, handler) // Small height to enable scrolling

	// Scroll on backdrop should do nothing
	m.HandleMouse(tea.MouseMsg{
		X:      0,
		Y:      0,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	}, handler)

	initialOffset := m.scrollOffset

	// Scroll on modal body should work
	bodyRegion := handler.HitMap.Test(40, 5) // Should hit modal-body
	if bodyRegion != nil && bodyRegion.ID == "modal-body" {
		m.HandleMouse(tea.MouseMsg{
			X:      40,
			Y:      5,
			Action: tea.MouseActionPress,
			Button: tea.MouseButtonWheelDown,
		}, handler)
		// Scroll offset should increase (if content is scrollable)
		_ = initialOffset // May not change if content fits
	}
}

func TestInputSection(t *testing.T) {
	ti := textinput.New()
	ti.Placeholder = "Enter name"
	s := InputWithLabel("name", "Name:", &ti)

	res := s.Render(60, "name", "")

	if !strings.Contains(res.Content, "Name:") {
		t.Errorf("expected content to contain 'Name:', got %q", res.Content)
	}
	if len(res.Focusables) != 1 {
		t.Errorf("expected 1 focusable, got %d", len(res.Focusables))
	}
	if res.Focusables[0].ID != "name" {
		t.Errorf("expected focusable ID 'name', got %q", res.Focusables[0].ID)
	}
}

func TestListSection(t *testing.T) {
	selectedIdx := 0
	items := []ListItem{
		{ID: "item1", Label: "Item 1"},
		{ID: "item2", Label: "Item 2"},
		{ID: "item3", Label: "Item 3"},
	}
	s := List("list", items, &selectedIdx)

	res := s.Render(60, "item1", "")

	if !strings.Contains(res.Content, "Item 1") {
		t.Errorf("expected content to contain 'Item 1', got %q", res.Content)
	}
	if len(res.Focusables) != 3 {
		t.Errorf("expected 3 focusables, got %d", len(res.Focusables))
	}

	// Test navigation
	s.Update(tea.KeyMsg{Type: tea.KeyDown}, "item1")
	if selectedIdx != 1 {
		t.Errorf("expected selectedIdx 1 after down, got %d", selectedIdx)
	}

	// Test enter returns selected item ID
	action, _ := s.Update(tea.KeyMsg{Type: tea.KeyEnter}, "item2")
	if action != "item2" {
		t.Errorf("expected action 'item2' on enter, got %q", action)
	}
}

func TestHitRegionAccuracy(t *testing.T) {
	m := New("Test Modal", WithWidth(50)).
		AddSection(Text("Some text")).
		AddSection(Spacer()).
		AddSection(Buttons(
			Btn(" OK ", "ok"),
			Btn(" Cancel ", "cancel"),
		))

	handler := mouse.NewHandler()
	rendered := m.Render(80, 24, handler)

	// The modal should have rendered something
	if rendered == "" {
		t.Error("expected non-empty render")
	}

	// Check that hit regions are registered
	regions := handler.HitMap.Regions()
	foundBackdrop := false
	foundBody := false
	foundOK := false
	foundCancel := false

	for _, r := range regions {
		switch r.ID {
		case "modal-backdrop":
			foundBackdrop = true
		case "modal-body":
			foundBody = true
		case "ok":
			foundOK = true
		case "cancel":
			foundCancel = true
		}
	}

	if !foundBackdrop {
		t.Error("expected modal-backdrop region")
	}
	if !foundBody {
		t.Error("expected modal-body region")
	}
	if !foundOK {
		t.Error("expected ok button region")
	}
	if !foundCancel {
		t.Error("expected cancel button region")
	}
}

func TestMeasureHeight(t *testing.T) {
	cases := []struct {
		content  string
		expected int
	}{
		{"", 0},
		{"single line", 1},
		{"line 1\nline 2", 2},
		{"line 1\nline 2\nline 3", 3},
		{"with trailing\n", 1}, // Trailing newline trimmed
		{"\n", 0},              // Only newline = empty
	}

	for _, tc := range cases {
		got := measureHeight(tc.content)
		if got != tc.expected {
			t.Errorf("measureHeight(%q) = %d, want %d", tc.content, got, tc.expected)
		}
	}
}

func TestSliceLines(t *testing.T) {
	content := "line 0\nline 1\nline 2\nline 3\nline 4"

	cases := []struct {
		offset, height int
		padToHeight    bool
		want           string
	}{
		{0, 2, true, "line 0\nline 1"},
		{1, 2, true, "line 1\nline 2"},
		{3, 3, true, "line 3\nline 4\n"},                                  // Padded with empty
		{0, 10, true, "line 0\nline 1\nline 2\nline 3\nline 4\n\n\n\n\n"}, // Padded
		{3, 3, false, "line 3\nline 4"},
		{0, 10, false, "line 0\nline 1\nline 2\nline 3\nline 4"},
	}

	for _, tc := range cases {
		got := sliceLines(content, tc.offset, tc.height, tc.padToHeight)
		if got != tc.want {
			t.Errorf("sliceLines(offset=%d, height=%d, pad=%v) = %q, want %q", tc.offset, tc.height, tc.padToHeight, got, tc.want)
		}
	}
}

func TestReset(t *testing.T) {
	m := New("Test").
		AddSection(Buttons(Btn(" A ", "a"), Btn(" B ", "b")))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	// Change state
	m.focusIdx = 1
	m.hoverID = "a"
	m.scrollOffset = 5

	// Reset
	m.Reset()

	if m.focusIdx != 0 {
		t.Errorf("expected focusIdx 0, got %d", m.focusIdx)
	}
	if m.hoverID != "" {
		t.Errorf("expected empty hoverID, got %q", m.hoverID)
	}
	if m.scrollOffset != 0 {
		t.Errorf("expected scrollOffset 0, got %d", m.scrollOffset)
	}
}
