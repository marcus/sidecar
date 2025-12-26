package styles

import "github.com/charmbracelet/lipgloss"

// Color palette - default dark theme
var (
	// Primary colors
	Primary   = lipgloss.Color("#7C3AED") // Purple
	Secondary = lipgloss.Color("#3B82F6") // Blue
	Accent    = lipgloss.Color("#F59E0B") // Amber

	// Status colors
	Success = lipgloss.Color("#10B981") // Green
	Warning = lipgloss.Color("#F59E0B") // Amber
	Error   = lipgloss.Color("#EF4444") // Red
	Info    = lipgloss.Color("#3B82F6") // Blue

	// Text colors
	TextPrimary   = lipgloss.Color("#F9FAFB")
	TextSecondary = lipgloss.Color("#9CA3AF")
	TextMuted     = lipgloss.Color("#6B7280")
	TextSubtle    = lipgloss.Color("#4B5563")

	// Background colors
	BgPrimary   = lipgloss.Color("#111827")
	BgSecondary = lipgloss.Color("#1F2937")
	BgTertiary  = lipgloss.Color("#374151")
	BgOverlay   = lipgloss.Color("#00000080")

	// Border colors
	BorderNormal = lipgloss.Color("#374151")
	BorderActive = lipgloss.Color("#7C3AED")
	BorderMuted  = lipgloss.Color("#1F2937")
)

// Panel styles
var (
	// Active panel with highlighted border
	PanelActive = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(BorderActive).
			Padding(0, 1)

	// Inactive panel with subtle border
	PanelInactive = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(BorderNormal).
			Padding(0, 1)

	// Panel header
	PanelHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(TextPrimary).
			MarginBottom(1)

	// Panel with no border
	PanelNoBorder = lipgloss.NewStyle().
			Padding(0, 1)
)

// Text styles
var (
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(TextPrimary)

	Subtitle = lipgloss.NewStyle().
			Foreground(TextSecondary)

	Body = lipgloss.NewStyle().
		Foreground(TextPrimary)

	Muted = lipgloss.NewStyle().
		Foreground(TextMuted)

	Subtle = lipgloss.NewStyle().
		Foreground(TextSubtle)

	Code = lipgloss.NewStyle().
		Foreground(Accent)

	KeyHint = lipgloss.NewStyle().
			Foreground(TextMuted).
			Background(BgTertiary).
			Padding(0, 1)
)

// Status indicator styles
var (
	StatusStaged = lipgloss.NewStyle().
			Foreground(Success).
			Bold(true)

	StatusModified = lipgloss.NewStyle().
			Foreground(Warning).
			Bold(true)

	StatusUntracked = lipgloss.NewStyle().
			Foreground(TextMuted)

	StatusDeleted = lipgloss.NewStyle().
			Foreground(Error).
			Bold(true)

	StatusInProgress = lipgloss.NewStyle().
				Foreground(Info).
				Bold(true)

	StatusCompleted = lipgloss.NewStyle().
			Foreground(Success)

	StatusBlocked = lipgloss.NewStyle().
			Foreground(Error)

	StatusPending = lipgloss.NewStyle().
			Foreground(TextMuted)
)

// List item styles
var (
	ListItemNormal = lipgloss.NewStyle().
			Foreground(TextPrimary)

	ListItemSelected = lipgloss.NewStyle().
				Foreground(TextPrimary).
				Background(BgTertiary)

	ListItemFocused = lipgloss.NewStyle().
			Foreground(TextPrimary).
			Background(Primary)

	ListCursor = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true)
)

// Tab bar styles
var (
	TabActive = lipgloss.NewStyle().
			Foreground(TextPrimary).
			Background(Primary).
			Padding(0, 2).
			Bold(true)

	TabInactive = lipgloss.NewStyle().
			Foreground(TextSecondary).
			Padding(0, 2)
)

// Diff line styles
var (
	DiffAdd = lipgloss.NewStyle().
		Foreground(Success)

	DiffRemove = lipgloss.NewStyle().
			Foreground(Error)

	DiffContext = lipgloss.NewStyle().
			Foreground(TextMuted)

	DiffHeader = lipgloss.NewStyle().
			Foreground(Info).
			Bold(true)
)

// Footer and header
var (
	Footer = lipgloss.NewStyle().
		Foreground(TextMuted).
		Background(BgSecondary)

	Header = lipgloss.NewStyle().
		Foreground(TextPrimary).
		Background(BgSecondary).
		Bold(true)
)

// Modal styles
var (
	ModalOverlay = lipgloss.NewStyle().
			Background(BgOverlay)

	ModalBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Primary).
			Background(BgSecondary).
			Padding(1, 2)

	ModalTitle = lipgloss.NewStyle().
			Foreground(TextPrimary).
			Bold(true).
			MarginBottom(1)
)

// Theme represents a color theme configuration
type Theme struct {
	Name      string
	Colors    ColorPalette
	Overrides map[string]string
}

// ColorPalette holds all theme colors
type ColorPalette struct {
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color
	Success   lipgloss.Color
	Warning   lipgloss.Color
	Error     lipgloss.Color
}

// DefaultTheme returns the default dark theme
func DefaultTheme() Theme {
	return Theme{
		Name: "default",
		Colors: ColorPalette{
			Primary:   Primary,
			Secondary: Secondary,
			Accent:    Accent,
			Success:   Success,
			Warning:   Warning,
			Error:     Error,
		},
		Overrides: make(map[string]string),
	}
}

// LoadTheme loads a theme by name with optional overrides
func LoadTheme(name string, overrides map[string]string) Theme {
	theme := DefaultTheme()
	theme.Name = name
	if overrides != nil {
		theme.Overrides = overrides
	}
	// Future: Load theme from config file by name
	return theme
}
