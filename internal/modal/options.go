package modal

// Variant represents the visual style of the modal.
type Variant int

const (
	VariantDefault Variant = iota // Primary border color
	VariantDanger                 // Red border, danger button styles
	VariantWarning                // Yellow/amber border
	VariantInfo                   // Blue border
)

// Option is a functional option for configuring a Modal.
type Option func(*Modal)

// Apply applies options to an existing modal. It is how a long-lived modal
// changes presentation (title, variant, hints) without being rebuilt, so its
// focus list and hit regions survive the change.
func (m *Modal) Apply(opts ...Option) {
	for _, opt := range opts {
		opt(m)
	}
	m.Invalidate()
}

// WithWidth sets the modal width.
func WithWidth(w int) Option {
	return func(m *Modal) {
		m.width = w
	}
}

// WithTitle sets the modal title.
func WithTitle(t string) Option {
	return func(m *Modal) {
		m.title = t
	}
}

// WithVariant sets the modal visual variant.
func WithVariant(v Variant) Option {
	return func(m *Modal) {
		m.variant = v
	}
}

// WithHints enables the keyboard hint line at the bottom.
func WithHints(show bool) Option {
	return func(m *Modal) {
		m.showHints = show
	}
}

// WithHintText replaces the default keyboard hint line. Use it where one of the
// default verbs is a lie — a modal whose Enter is a no-op should not advertise
// "Enter to confirm". Implies WithHints(true).
func WithHintText(text string) Option {
	return func(m *Modal) {
		m.showHints = true
		m.hintText = text
	}
}

// WithPrimaryAction sets the action ID returned when input submits implicitly.
func WithPrimaryAction(actionID string) Option {
	return func(m *Modal) {
		m.primaryAction = actionID
	}
}

// WithInitialFocus sets the element ID that receives focus on first render.
func WithInitialFocus(id string) Option {
	return func(m *Modal) {
		m.SetFocus(id)
	}
}

// WithCloseOnBackdropClick controls whether clicking the backdrop dismisses the modal.
// Defaults to true.
func WithCloseOnBackdropClick(close bool) Option {
	return func(m *Modal) {
		m.closeOnBackdrop = close
	}
}

// WithCustomFooter sets a fixed footer line rendered outside the scroll viewport.
func WithCustomFooter(footer string) Option {
	return func(m *Modal) {
		m.customFooter = footer
	}
}

// WithMargin sets how much of the surface the modal leaves clear around itself.
// The default keeps the box off the edge of a screen; a modal that is meant to
// own its surface — a pane too small to show anything useful behind the modal —
// passes 0, 0 and is then rendered at exactly the surface's size.
func WithMargin(x, y int) Option {
	return func(m *Modal) {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		m.marginX, m.marginY = x, y
	}
}

// PreferredListRows is the list length a content-sized modal aims for on a
// surface this tall: enough to be worth opening, never so much that a picker
// with three hits reserves most of a large pane. It deliberately does not
// depend on how many rows there are to show — a box that grows as results land
// breathes under the user's hands.
func PreferredListRows(surfaceHeight int) int {
	rows := surfaceHeight / 3
	if rows < MinListRows {
		rows = MinListRows
	}
	if rows > MaxListRows {
		rows = MaxListRows
	}
	return rows
}

// ListRows is how long a content-sized list actually draws itself: the
// preferred length, less what it has never needed, but never below MinListRows.
//
// seen is the most rows the list has wanted since it was opened — a high-water
// mark, not the current count. A box sized to the current count breathes under
// the user's hands as a query is refined; a box sized to the preferred length
// whatever happens reserves two dozen rows for a picker that has one hit and
// reads as broken on a large screen. Growing once into the results and then
// holding still is the balance: refining a query, which is where the jitter was
// unbearable, never resizes anything.
func ListRows(surfaceHeight, seen int) int {
	rows := PreferredListRows(surfaceHeight)
	if seen < rows {
		rows = seen
	}
	if rows < MinListRows {
		rows = MinListRows
	}
	return rows
}

// ListRowsFor is ListRows with the two cases the high-water mark gets wrong.
//
// The mark exists so that refining a query does not resize the box under the
// user's hands, and that is right while the list is answering. It is not right
// once it has stopped:
//
//   - a query that matches nothing gets a one-row list. The mark had a box that
//     grew to fifty hits still reserving a dozen rows under the words "No
//     matches", which reads as broken rather than steady, and nothing is being
//     refined at that point — the query has stopped matching.
//   - a list showing fewer rows than the ordinary floor keeps the floor and no
//     more. One hit in a fourteen-row box is thirteen blank rows; one hit in an
//     eight-row box is the same box an unasked finder draws, so nothing has
//     jittered and nothing is being reserved for results that are not coming.
//
// current is how many rows the list is showing. A caller in a state that has
// not asked anything yet — no query, or a scan still running — passes
// MinListRows rather than zero: an empty box is not a dead end, and a box that
// opens one row tall and grows is the jitter this all exists to avoid.
func ListRowsFor(surfaceHeight, seen, current int) int {
	if current < 1 {
		return 1
	}
	if current < MinListRows {
		return MinListRows
	}
	return ListRows(surfaceHeight, seen)
}

// ContentBoxWidth is the widest a content-sized box may be on a surface this
// wide. It keeps RoomyMarginX clear on each side so a host that dims the
// surface around the box has something left to dim, and falls back to the
// ordinary margin on a surface too narrow for that to leave a usable box.
func ContentBoxWidth(surfaceWidth int) int {
	if roomy := surfaceWidth - 2*RoomyMarginX; roomy >= MinRoomyWidth {
		return roomy
	}
	if w := surfaceWidth - 2*DefaultMarginX; w > 0 {
		return w
	}
	return 1
}

// Default modal dimensions
const (
	// BackdropRegionID names the region a modal registers over the whole
	// surface it was drawn on. A host that composites the box somewhere other
	// than where the modal thought it was — panemodal, placing it in a pane —
	// registers its own under this ID so the surface behind the box still
	// absorbs clicks.
	BackdropRegionID = "modal-backdrop"

	DefaultWidth  = 50
	MinModalWidth = 30
	MaxModalWidth = 120
	ModalPadding  = 6 // border(2) + horizontal padding(4)

	// ScrollbarColumns is the content column the body's scrollbar takes when
	// the content is taller than the viewport — buildLayout re-renders every
	// section at contentWidth-1 once it needs one. A host sizing a modal to its
	// widest control budgets for it, or the control fits until the modal grows
	// a scrollbar and then quietly changes shape.
	ScrollbarColumns = 1

	// DefaultMarginX/Y are the cells left clear around the modal box.
	DefaultMarginX = 2
	DefaultMarginY = 1

	// RoomyMarginX is the surface a content-sized box tries to leave visible on
	// each side of itself, and the width a pane host requires before it will
	// show the surface behind the box at all. The two are the same number on
	// purpose: a box that grows to DefaultMarginX of its surface can never leave
	// a readable ring, so the host would always fall back to a takeover, which
	// is how a three-row picker came to own a whole pane.
	RoomyMarginX = 8

	// MinRoomyWidth is the narrowest box worth keeping a ring around. Below it
	// the rows need the columns more than the surface behind them does.
	MinRoomyWidth = 40

	// MinListRows/MaxListRows bound PreferredListRows.
	MinListRows = 8
	MaxListRows = 14

	// ChromeWidth/ChromeHeight are what the box itself costs: border plus
	// padding. A caller budgeting its own content needs the same numbers.
	ChromeWidth  = 6
	ChromeHeight = 4
)
