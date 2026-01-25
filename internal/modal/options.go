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

// WithWidth sets the modal width.
func WithWidth(w int) Option {
	return func(m *Modal) {
		m.width = w
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

// WithPrimaryAction sets the action ID returned when input submits implicitly.
func WithPrimaryAction(actionID string) Option {
	return func(m *Modal) {
		m.primaryAction = actionID
	}
}

// WithCloseOnBackdropClick controls whether clicking the backdrop dismisses the modal.
// Defaults to true.
func WithCloseOnBackdropClick(close bool) Option {
	return func(m *Modal) {
		m.closeOnBackdrop = close
	}
}

// Default modal dimensions
const (
	DefaultWidth  = 50
	MinModalWidth = 30
	MaxModalWidth = 120
	ModalPadding  = 6 // border(2) + horizontal padding(4)
)
