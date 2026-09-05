package projectlist

import "testing"

func TestGridColumnsKeepWholeCardsAndFallBackToTheList(t *testing.T) {
	tests := []struct {
		width, want int
	}{
		{0, 0},
		{31, 0}, // below one readable card: the caller draws the list
		{32, 1},
		{64, 1}, // one card plus a gap and part of another
		{65, 2}, // exactly two cards and the gap between them
		{98, 3}, // three cards fit beside the scrollbar's column
		{100, 3},
		{400, 3}, // capped, so the collection does not reflow on a wide terminal
	}
	for _, tc := range tests {
		if got := GridColumns(tc.width); got != tc.want {
			t.Fatalf("GridColumns(%d) = %d, want %d", tc.width, got, tc.want)
		}
	}
	if GridAvailable(31) {
		t.Fatal("grid must report unavailable below one card")
	}
	if !GridAvailable(32) {
		t.Fatal("grid must be available at exactly one card")
	}
}

func TestGridRows(t *testing.T) {
	if got := GridRows(7, 3); got != 3 {
		t.Fatalf("GridRows(7,3) = %d, want 3", got)
	}
	if got := GridRows(0, 3); got != 0 {
		t.Fatalf("GridRows(0,3) = %d, want 0", got)
	}
}

func TestGridMoveIsSpatialAndRefusesRatherThanWrapping(t *testing.T) {
	const n, cols = 7, 3 // rows: [0 1 2] [3 4 5] [6]
	tests := []struct {
		name         string
		from, dx, dy int
		want         int
	}{
		{"right along the row", 0, 1, 0, 1},
		{"left along the row", 1, -1, 0, 0},
		{"left at the edge stays", 0, -1, 0, 0},
		{"right at the edge stays", 2, 1, 0, 2},
		{"right into a short row's gap stays", 6, 1, 0, 6},
		{"down keeps the column", 1, 0, 1, 4},
		{"up keeps the column", 4, 0, -1, 1},
		{"up from the first row stays", 2, 0, -1, 2},
		{"down into a short row lands on its last card", 4, 0, 1, 6},
		{"down from the last row stays", 6, 0, 1, 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := GridMove(tc.from, n, cols, tc.dx, tc.dy); got != tc.want {
				t.Fatalf("GridMove(%d,%d,%d,%d,%d) = %d, want %d", tc.from, n, cols, tc.dx, tc.dy, got, tc.want)
			}
		})
	}
}

func TestGridMoveClampsAnOutOfRangeIndex(t *testing.T) {
	if got := GridMove(99, 4, 2, 0, 0); got != 3 {
		t.Fatalf("out-of-range index = %d, want 3", got)
	}
	if got := GridMove(0, 0, 3, 1, 0); got != 0 {
		t.Fatalf("empty collection = %d, want 0", got)
	}
}
