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

	// Diff foreground colors (also updated by ApplyTheme)
	DiffAddFg    = lipgloss.Color("#10B981")
	DiffRemoveFg = lipgloss.Color("#EF4444")

	// Additional themeable colors
	TextHighlight         = lipgloss.Color("#E5E7EB") // For subtitle, special text
	ButtonHoverColor      = lipgloss.Color("#9D174D") // Button hover background
	TabTextInactiveColor  = lipgloss.Color("#1a1a1a") // Inactive tab text
	LinkColor             = lipgloss.Color("#60A5FA") // Hyperlink color
	ToastSuccessTextColor = lipgloss.Color("#000000") // Toast success foreground
	ToastErrorTextColor   = lipgloss.Color("#FFFFFF") // Toast error foreground

	// Third-party theme names (updated by ApplyTheme)
	CurrentSyntaxTheme   = "monokai"
	CurrentMarkdownTheme = "dark"
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
			Foreground(TextHighlight)

	Body = lipgloss.NewStyle().
		Foreground(TextPrimary)

	Muted = lipgloss.NewStyle().
		Foreground(TextMuted)

	Subtle = lipgloss.NewStyle().
		Foreground(TextSubtle)

	Code = lipgloss.NewStyle().
		Foreground(Accent)

	Link = lipgloss.NewStyle().
		Foreground(LinkColor).
		Underline(true)

	KeyHint = lipgloss.NewStyle().
		Foreground(TextMuted).
		Background(BgTertiary).
		Padding(0, 1)

	Logo = lipgloss.NewStyle().
		Foreground(Primary).
		Bold(true)
)

// Status indicator styles
var (
	StatusStaged = lipgloss.NewStyle().
			Foreground(Success).
			Bold(true)

	StatusModified = lipgloss.NewStyle().
			Foreground(Warning).
			Bold(true)

	// Toast styles for status messages
	ToastSuccess = lipgloss.NewStyle().
			Background(Success).
			Foreground(ToastSuccessTextColor).
			Bold(true).
			Padding(0, 1)

	ToastError = lipgloss.NewStyle().
			Background(Error).
			Foreground(ToastErrorTextColor).
			Bold(true).
			Padding(0, 1)

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

// Bar element styles (shared by header/footer)
var (
	BarTitle = lipgloss.NewStyle().
			Foreground(TextPrimary).
			Bold(true)

	BarText = lipgloss.NewStyle().
		Foreground(TextMuted)

	BarChip = lipgloss.NewStyle().
		Foreground(TextMuted).
		Background(BgTertiary).
		Padding(0, 1)

	BarChipActive = lipgloss.NewStyle().
			Foreground(TextPrimary).
			Background(Primary).
			Padding(0, 1).
			Bold(true)
)

// Tab rainbow colors (left to right: red → green → blue → purple)
var TabRainbowColors = []struct {
	R, G, B uint8
}{
	{220, 60, 60},   // Red
	{60, 180, 80},   // Green
	{60, 130, 220},  // Blue
	{140, 80, 200},  // Purple
}

// TabTextActive is the text color for active tabs
var TabTextActive = lipgloss.NewStyle().
	Foreground(TextPrimary).
	Bold(true)

// TabTextInactive is the text color for inactive tabs
var TabTextInactive = lipgloss.NewStyle().
	Foreground(TabTextInactiveColor)

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

	// Subtle diff backgrounds for syntax-highlighted lines
	DiffAddBg    = lipgloss.Color("#0D2818") // Very subtle dark green
	DiffRemoveBg = lipgloss.Color("#2D1A1A") // Very subtle dark red
)

// File browser styles
var (
	// Directory names - bold blue
	FileBrowserDir = lipgloss.NewStyle().
			Foreground(Secondary).
			Bold(true)

	// Regular file names
	FileBrowserFile = lipgloss.NewStyle().
			Foreground(TextPrimary)

	// Gitignored files - muted/dimmed
	FileBrowserIgnored = lipgloss.NewStyle().
				Foreground(TextSubtle)

	// Line numbers in preview
	FileBrowserLineNumber = lipgloss.NewStyle().
				Foreground(TextMuted).
				Width(5).
				AlignHorizontal(lipgloss.Right)

	// Tree icons (>, +)
	FileBrowserIcon = lipgloss.NewStyle().
			Foreground(TextMuted)

	// Content search match highlighting
	SearchMatch = lipgloss.NewStyle().
			Background(Warning) // Yellow background for all matches

	SearchMatchCurrent = lipgloss.NewStyle().
				Background(Primary). // Purple background for current match
				Foreground(TextPrimary)

	// Fuzzy match character highlighting (bold in result list)
	FuzzyMatchChar = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true)

	// Quick open result row (normal)
	QuickOpenItem = lipgloss.NewStyle().
			Foreground(TextPrimary)

	// Quick open result row (selected)
	QuickOpenItemSelected = lipgloss.NewStyle().
				Foreground(TextPrimary).
				Background(BgTertiary)

	// Palette entry styles (reusable for modals)
	PaletteEntry = lipgloss.NewStyle().
			Foreground(TextPrimary)

	PaletteEntrySelected = lipgloss.NewStyle().
				Foreground(TextPrimary).
				Background(BgTertiary)

	PaletteKey = lipgloss.NewStyle().
			Foreground(TextMuted).
			Background(BgTertiary).
			Padding(0, 1)

	// Text selection for preview pane drag selection
	TextSelection = lipgloss.NewStyle().
			Background(BgTertiary).
			Foreground(TextPrimary)
)

// Footer and header
var (
	Footer = lipgloss.NewStyle().
		Foreground(TextMuted).
		Background(BgSecondary)

	Header = lipgloss.NewStyle().
		Background(BgSecondary)
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

// Button styles
var (
	Button = lipgloss.NewStyle().
		Foreground(TextSecondary).
		Background(BgTertiary).
		Padding(0, 2)

	ButtonFocused = lipgloss.NewStyle().
			Foreground(TextPrimary).
			Background(Primary).
			Padding(0, 2).
			Bold(true)

	ButtonHover = lipgloss.NewStyle().
			Foreground(TextPrimary).
			Background(ButtonHoverColor).
			Padding(0, 2)
)

// RenderGradientTab renders a tab label with a gradient background.
// tabIndex is the 0-based index of this tab, totalTabs is the total count.
// The gradient flows across all tabs: red → green → blue → purple.
func RenderGradientTab(label string, tabIndex, totalTabs int, isActive bool) string {
	if totalTabs == 0 {
		totalTabs = 1
	}

	// Calculate position in the rainbow (0.0 to 1.0 across all tabs)
	tabWidth := 1.0 / float64(totalTabs)
	startPos := float64(tabIndex) * tabWidth
	endPos := startPos + tabWidth

	// Add padding to label
	padded := "  " + label + "  "
	chars := []rune(padded)
	result := ""

	for i, ch := range chars {
		// Position within the rainbow for this character
		charPos := startPos + (endPos-startPos)*float64(i)/float64(len(chars))

		// Get interpolated color
		r, g, b := interpolateRainbow(charPos)

		// Mute colors for inactive tabs
		if !isActive {
			r = uint8(float64(r)*0.35 + 30)
			g = uint8(float64(g)*0.35 + 30)
			b = uint8(float64(b)*0.35 + 30)
		}

		// Create style for this character
		bg := lipgloss.Color(sprintf("#%02x%02x%02x", r, g, b))
		var style lipgloss.Style
		if isActive {
			style = lipgloss.NewStyle().Background(bg).Foreground(TextPrimary).Bold(true)
		} else {
			style = lipgloss.NewStyle().Background(bg).Foreground(TextSecondary)
		}
		result += style.Render(string(ch))
	}

	return result
}

// interpolateRainbow returns RGB for a position 0.0-1.0 across the rainbow
func interpolateRainbow(pos float64) (uint8, uint8, uint8) {
	colors := TabRainbowColors
	if len(colors) < 2 {
		return 128, 128, 128
	}

	// Scale position to color index
	scaled := pos * float64(len(colors)-1)
	idx := int(scaled)
	if idx >= len(colors)-1 {
		idx = len(colors) - 2
	}
	frac := scaled - float64(idx)

	// Interpolate between adjacent colors
	c1, c2 := colors[idx], colors[idx+1]
	r := uint8(float64(c1.R) + frac*(float64(c2.R)-float64(c1.R)))
	g := uint8(float64(c1.G) + frac*(float64(c2.G)-float64(c1.G)))
	b := uint8(float64(c1.B) + frac*(float64(c2.B)-float64(c1.B)))

	return r, g, b
}

// sprintf is a local helper to avoid importing fmt just for color formatting
func sprintf(format string, a ...interface{}) string {
	// Simple hex formatter for RGB
	if format == "#%02x%02x%02x" && len(a) == 3 {
		r, g, b := a[0].(uint8), a[1].(uint8), a[2].(uint8)
		const hex = "0123456789abcdef"
		return string([]byte{'#',
			hex[r>>4], hex[r&0xf],
			hex[g>>4], hex[g&0xf],
			hex[b>>4], hex[b&0xf],
		})
	}
	return ""
}
