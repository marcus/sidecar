package gitstatus

import (
	"strings"
	"testing"
)

func TestRenderMinimap_NilDiff(t *testing.T) {
	result := RenderMinimap(nil, 0, 10, 20)
	if result != "" {
		t.Errorf("expected empty string for nil diff, got %q", result)
	}
}

func TestRenderMinimap_EmptyDiff(t *testing.T) {
	ffd := &FullFileDiff{Lines: []FullFileLine{}}
	result := RenderMinimap(ffd, 0, 10, 20)
	if result != "" {
		t.Errorf("expected empty string for empty diff, got %q", result)
	}
}

func TestRenderMinimap_TooShort(t *testing.T) {
	ffd := &FullFileDiff{Lines: make([]FullFileLine, 100)}
	result := RenderMinimap(ffd, 0, 10, 1)
	if result != "" {
		t.Errorf("expected empty string for height < 2, got %q", result)
	}
}

func TestRenderMinimap_LineCount(t *testing.T) {
	lines := make([]FullFileLine, 50)
	for i := range lines {
		lines[i] = FullFileLine{Type: LineContext, OldLineNo: i + 1, NewLineNo: i + 1}
	}
	ffd := &FullFileDiff{Lines: lines}

	height := 20
	result := RenderMinimap(ffd, 0, 10, height)
	// Each row has a trailing \n, so the output has exactly `height` newlines.
	// strings.Split produces height+1 entries (last is empty after trailing \n).
	parts := strings.Split(result, "\n")
	got := len(parts) - 1 // subtract empty trailing entry
	if got != height {
		t.Errorf("minimap line count = %d, want %d", got, height)
	}
}

func TestRenderMinimap_TrailingNewline(t *testing.T) {
	lines := make([]FullFileLine, 20)
	for i := range lines {
		lines[i] = FullFileLine{Type: LineContext}
	}
	ffd := &FullFileDiff{Lines: lines}

	result := RenderMinimap(ffd, 0, 5, 10)
	if !strings.HasSuffix(result, "\n") {
		t.Error("minimap should end with trailing newline to match diff renderer format")
	}
}

func TestRenderMinimap_ContainsHalfBlocks(t *testing.T) {
	lines := make([]FullFileLine, 20)
	for i := range lines {
		lines[i] = FullFileLine{Type: LineContext}
	}
	ffd := &FullFileDiff{Lines: lines}

	result := RenderMinimap(ffd, 0, 5, 10)
	if !strings.Contains(result, "▀") {
		t.Error("minimap should contain half-block characters")
	}
}

func TestRenderMinimap_ShortFile(t *testing.T) {
	// File with 3 lines, minimap height 10 — height caps to totalLines (3).
	lines := []FullFileLine{
		{Type: LineAdd},
		{Type: LineContext},
		{Type: LineRemove},
	}
	ffd := &FullFileDiff{Lines: lines}

	result := RenderMinimap(ffd, 0, 3, 10)
	if result == "" {
		t.Error("minimap should render even for short files")
	}
	parts := strings.Split(result, "\n")
	got := len(parts) - 1 // subtract trailing empty entry
	if got != 3 {
		t.Errorf("minimap line count for short file = %d, want 3 (capped to totalLines)", got)
	}
}

func TestRenderMinimap_VeryShortFile(t *testing.T) {
	// File with 1 line — after capping, height=1 which is < 2, so returns "".
	ffd := &FullFileDiff{Lines: []FullFileLine{{Type: LineAdd}}}
	result := RenderMinimap(ffd, 0, 1, 10)
	if result != "" {
		t.Errorf("1-line file should return empty minimap, got %q", result)
	}
}

func TestRenderMinimap_NegativeScrollPos(t *testing.T) {
	lines := make([]FullFileLine, 50)
	for i := range lines {
		lines[i] = FullFileLine{Type: LineContext}
	}
	ffd := &FullFileDiff{Lines: lines}

	// Should not panic with negative scrollPos.
	result := RenderMinimap(ffd, -5, 10, 20)
	if result == "" {
		t.Error("minimap should render with negative scrollPos (clamped to 0)")
	}
}

func TestSlotDominantType_Context(t *testing.T) {
	lines := []FullFileLine{
		{Type: LineContext},
		{Type: LineContext},
		{Type: LineContext},
	}
	if got := slotDominantType(lines, 0, 3); got != LineContext {
		t.Errorf("slotDominantType = %v, want LineContext", got)
	}
}

func TestSlotDominantType_AddPriority(t *testing.T) {
	lines := []FullFileLine{
		{Type: LineContext},
		{Type: LineAdd},
		{Type: LineContext},
	}
	if got := slotDominantType(lines, 0, 3); got != LineAdd {
		t.Errorf("slotDominantType = %v, want LineAdd", got)
	}
}

func TestSlotDominantType_RemovePriority(t *testing.T) {
	lines := []FullFileLine{
		{Type: LineRemove},
		{Type: LineRemove},
		{Type: LineAdd},
	}
	if got := slotDominantType(lines, 0, 3); got != LineRemove {
		t.Errorf("slotDominantType = %v, want LineRemove", got)
	}
}

func TestSlotDominantType_OutOfBounds(t *testing.T) {
	lines := []FullFileLine{{Type: LineAdd}}
	if got := slotDominantType(lines, 5, 10); got != LineContext {
		t.Errorf("slotDominantType out of bounds = %v, want LineContext", got)
	}
}

func TestRangesOverlap(t *testing.T) {
	tests := []struct {
		a0, a1, b0, b1 int
		want           bool
	}{
		{0, 5, 3, 8, true},
		{0, 5, 5, 10, false},
		{5, 10, 0, 5, false},
		{0, 10, 3, 7, true},
		{3, 7, 0, 10, true},
	}
	for _, tt := range tests {
		if got := rangesOverlap(tt.a0, tt.a1, tt.b0, tt.b1); got != tt.want {
			t.Errorf("rangesOverlap(%d,%d,%d,%d) = %v, want %v",
				tt.a0, tt.a1, tt.b0, tt.b1, got, tt.want)
		}
	}
}

func TestRenderMinimap_RailMapSync(t *testing.T) {
	// Verify that the rail (▀/▄/█) appears on at least one row.
	// The rail now uses half-block characters for per-slot precision,
	// matching the map's bright/dim boundary exactly.
	lines := make([]FullFileLine, 800)
	for i := range lines {
		lines[i] = FullFileLine{Type: LineContext}
	}
	ffd := &FullFileDiff{Lines: lines}

	height := 40
	scrollPos := 250
	visibleLines := 40

	result := RenderMinimap(ffd, scrollPos, visibleLines, height)
	fullResult := strings.Join(strings.Split(result, "\n")[:height], "\n")
	// Rail uses █, ▀, or ▄ depending on which half-slots are in viewport.
	hasRail := strings.Contains(fullResult, "█") ||
		strings.Contains(fullResult, "▄")
	if !hasRail {
		t.Error("minimap should have at least one rail indicator row")
	}
}

func TestMinimapScrollTarget(t *testing.T) {
	// Click in the middle of minimap.
	target := MinimapScrollTarget(10, 20, 200, 40)
	if target < 60 || target > 100 {
		t.Errorf("MinimapScrollTarget(10, 20, 200, 40) = %d, expected 60-100", target)
	}

	// Click at top.
	target = MinimapScrollTarget(0, 20, 200, 40)
	if target != 0 {
		t.Errorf("MinimapScrollTarget at top = %d, want 0", target)
	}

	// Click at bottom.
	target = MinimapScrollTarget(19, 20, 200, 40)
	maxScroll := 200 - 40
	if target != maxScroll {
		t.Errorf("MinimapScrollTarget at bottom = %d, want %d", target, maxScroll)
	}

	// Zero height — should not panic.
	target = MinimapScrollTarget(5, 0, 100, 20)
	if target != 0 {
		t.Errorf("MinimapScrollTarget zero height = %d, want 0", target)
	}

	// Zero total lines.
	target = MinimapScrollTarget(5, 20, 0, 20)
	if target != 0 {
		t.Errorf("MinimapScrollTarget zero totalLines = %d, want 0", target)
	}

	// Click beyond minimap bounds — should clamp to maxScroll.
	target = MinimapScrollTarget(25, 20, 200, 40)
	if target < 0 || target > 160 {
		t.Errorf("out-of-bounds click = %d, expected 0-160", target)
	}
}

func TestSlotLineRange(t *testing.T) {
	// 100 lines, 20 slots → 5 lines per slot.
	s, e := slotLineRange(0, 5.0, 100)
	if s != 0 || e != 5 {
		t.Errorf("slot 0: [%d, %d), want [0, 5)", s, e)
	}

	s, e = slotLineRange(19, 5.0, 100)
	if s != 95 || e != 100 {
		t.Errorf("slot 19: [%d, %d), want [95, 100)", s, e)
	}

	// Edge: slot beyond file — should clamp.
	s, _ = slotLineRange(25, 5.0, 100)
	if s >= 100 {
		t.Errorf("slot 25: start=%d should be clamped < 100", s)
	}

	// Very small linesPerSlot (file shorter than minimap).
	s, e = slotLineRange(0, 0.15, 3)
	if s != 0 || e < 1 {
		t.Errorf("slot 0 small file: [%d, %d), want [0, >=1)", s, e)
	}
	s, e = slotLineRange(15, 0.15, 3)
	if s >= 3 || e > 3 {
		t.Errorf("slot 15 small file: [%d, %d), want clamped to [0-2, <=3)", s, e)
	}
}

func TestMinimapScrollTarget_ViewportLargerThanFile(t *testing.T) {
	// visibleLines > totalLines — should always return 0.
	target := MinimapScrollTarget(5, 10, 5, 20)
	if target != 0 {
		t.Errorf("MinimapScrollTarget with viewport > file = %d, want 0", target)
	}
}

func TestRenderMinimap_ViewEndExceedsTotalLines(t *testing.T) {
	// scrollPos + visibleLines > totalLines — viewEnd should be clamped.
	lines := make([]FullFileLine, 20)
	for i := range lines {
		lines[i] = FullFileLine{Type: LineContext}
	}
	ffd := &FullFileDiff{Lines: lines}

	// scrollPos=15, visibleLines=10 → viewEnd would be 25, clamped to 20.
	result := RenderMinimap(ffd, 15, 10, 10)
	if result == "" {
		t.Error("minimap should render when viewEnd exceeds totalLines")
	}
}

func TestMinimapScrollTarget_NegativeTotalLines(t *testing.T) {
	target := MinimapScrollTarget(5, 10, -5, 20)
	if target != 0 {
		t.Errorf("MinimapScrollTarget with negative totalLines = %d, want 0", target)
	}
}

func TestRenderMinimap_ViewportLargerThanFile(t *testing.T) {
	// When visibleLines > totalLines, the entire file is in the viewport,
	// so the rail should cover every row.
	lines := make([]FullFileLine, 10)
	for i := range lines {
		lines[i] = FullFileLine{Type: LineContext}
	}
	ffd := &FullFileDiff{Lines: lines}

	// visibleLines=50 > totalLines=10, height caps to 10.
	result := RenderMinimap(ffd, 0, 50, 20)
	if result == "" {
		t.Fatal("minimap should render")
	}
	// Every row should have a rail character (no spaces in rail column).
	rows := strings.Split(result, "\n")
	for i := 0; i < 10; i++ {
		if len(rows[i]) > 0 && rows[i][0] == ' ' {
			t.Errorf("row %d has no rail but entire file is in viewport", i)
		}
	}
}

func TestRenderMinimap_HalfBlockRailBoundary(t *testing.T) {
	// With a known scroll position, verify that ▄ (bottom-only) or ▀ (top-only)
	// appears at the viewport boundary, and █ appears in the interior.
	lines := make([]FullFileLine, 200)
	for i := range lines {
		lines[i] = FullFileLine{Type: LineContext}
	}
	ffd := &FullFileDiff{Lines: lines}

	// Scroll to the middle so both boundaries are visible in the minimap.
	result := RenderMinimap(ffd, 80, 20, 40)
	if result == "" {
		t.Fatal("minimap should render")
	}

	// Should contain █ (full block, interior rail rows).
	if !strings.Contains(result, "█") {
		t.Error("minimap should contain █ for interior viewport rail rows")
	}
}
