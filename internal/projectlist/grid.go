package projectlist

// Grid geometry. The card is a fixed shape rather than a share of the width:
// a project card holds a name, a path and one metadata line, and those need a
// known number of columns to stay readable. Stretching cards to fill a wide
// modal would make three columns of mostly-empty box; shrinking them to fit a
// narrow one would truncate the path that makes a project recognisable. So the
// card keeps its size and the grid keeps whole columns, and below the width
// where even one card is readable the collection falls back to the list.
const (
	// CardWidth is the card's total width including its border.
	CardWidth = 32
	// CardGap is the space between columns. One column rather than two: three
	// cards, two gaps and the scrollbar's column have to fit the modal's
	// content width together, and a second column of gap is what costs the
	// third card.
	CardGap = 1
	// CardHeight is the card's total height including its border. Six rows is
	// what holds a name row, a path row, breathing space and a metadata row.
	CardHeight = 6
	// MaxGridColumns caps the grid. A fourth column is reachable on a very
	// wide terminal but the modal is not that wide, and a collection that
	// reflows between three and four columns as the window moves is harder to
	// re-find a project in than one that does not.
	MaxGridColumns = 3
)

// GridColumns is how many card columns fit in contentWidth. Zero means not
// even one card is readable, which is the caller's signal to draw the list
// instead — the grid is an alternative view of the collection, never a worse
// one.
func GridColumns(contentWidth int) int {
	if contentWidth < CardWidth {
		return 0
	}
	columns := (contentWidth + CardGap) / (CardWidth + CardGap)
	if columns > MaxGridColumns {
		columns = MaxGridColumns
	}
	if columns < 1 {
		return 0
	}
	return columns
}

// GridAvailable reports whether the grid can be drawn at this width at all.
// A view control that offers a layout the surface cannot draw is a control
// that lies, so the caller uses this to fall back rather than to refuse.
func GridAvailable(contentWidth int) bool { return GridColumns(contentWidth) > 0 }

// GridRows is how many card rows a collection of n items occupies.
func GridRows(n, columns int) int {
	if n <= 0 || columns <= 0 {
		return 0
	}
	return (n + columns - 1) / columns
}

// GridMove is the spatial arrow rule. Arrows in a grid move by position, not by
// list index: left and right walk the row, up and down change row keeping the
// column. Movement off the collection is refused rather than wrapped, so a
// press that has nowhere to go leaves the selection where the user put it.
//
// dx and dy are -1, 0 or 1. The returned index is always within [0, n).
func GridMove(index, n, columns, dx, dy int) int {
	if n <= 0 || columns <= 0 {
		return index
	}
	if index < 0 {
		index = 0
	}
	if index >= n {
		index = n - 1
	}
	row, col := index/columns, index%columns
	if dx != 0 {
		next := col + dx
		if next < 0 || next >= columns {
			return index
		}
		candidate := row*columns + next
		if candidate >= n {
			// The last row can be short. Moving right into the gap is a move to
			// nowhere, so it stays put rather than jumping to another row.
			return index
		}
		return candidate
	}
	if dy != 0 {
		next := row + dy
		if next < 0 {
			return index
		}
		candidate := next*columns + col
		if candidate >= n {
			// Down from the second-to-last row into a short last row lands on
			// its final card rather than refusing: the row exists, and that is
			// the card directly below as far as the user is concerned.
			if next*columns < n {
				return n - 1
			}
			return index
		}
		return candidate
	}
	return index
}
