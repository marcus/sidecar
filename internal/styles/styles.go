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
	TextPrimary        = lipgloss.Color("#F9FAFB")
	TextSecondary      = lipgloss.Color("#9CA3AF")
	TextMuted          = lipgloss.Color("#6B7280")
	TextSubtle         = lipgloss.Color("#4B5563")
	TextSelectionColor = lipgloss.Color("#F9FAFB") // Text on selection backgrounds (BgTertiary)

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

	// Danger button colors
	DangerLight  = lipgloss.Color("#FCA5A5") // Light red text
	DangerDark   = lipgloss.Color("#7F1D1D") // Dark red background
	DangerBright = lipgloss.Color("#DC2626") // Bright red focused bg
	DangerHover  = lipgloss.Color("#B91C1C") // Darker red hover bg
	TextInverse  = lipgloss.Color("#FFFFFF") // Inverse/contrast text

	// Blame age gradient colors
	BlameAge1 = lipgloss.Color("#34D399") // < 1 week
	BlameAge2 = lipgloss.Color("#84CC16") // < 1 month
	BlameAge3 = lipgloss.Color("#FBBF24") // < 3 months
	BlameAge4 = lipgloss.Color("#F97316") // < 6 months
	BlameAge5 = lipgloss.Color("#9CA3AF") // < 1 year

	// Third-party theme names (updated by ApplyTheme)
	CurrentSyntaxTheme   = "monokai"
	CurrentMarkdownTheme = "dark"
)

// Tab theme state (updated by ApplyTheme)
var (
	CurrentTabStyle  = "rainbow"
	CurrentTabColors = []RGB{{220, 60, 60}, {60, 220, 60}, {60, 60, 220}, {156, 60, 220}} // Default rainbow
)

// PillTabsEnabled enables rounded pill-style tabs (requires Nerd Font)
var PillTabsEnabled = false

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

	// WorktreeIndicator shows the current worktree branch in the header
	WorktreeIndicator = lipgloss.NewStyle().
				Foreground(Warning).
				Bold(true)

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
				Foreground(TextSelectionColor).
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
				Foreground(TextSelectionColor).
				Background(BgTertiary)

	// Palette entry styles (reusable for modals)
	PaletteEntry = lipgloss.NewStyle().
			Foreground(TextPrimary)

	PaletteEntrySelected = lipgloss.NewStyle().
				Foreground(TextSelectionColor).
				Background(BgTertiary)

	PaletteKey = lipgloss.NewStyle().
			Foreground(TextMuted).
			Background(BgTertiary).
			Padding(0, 1)

	// Text selection for preview pane drag selection
	TextSelection = lipgloss.NewStyle().
			Background(BgTertiary).
			Foreground(TextSelectionColor)
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

	// Danger button styles (for destructive actions like delete)
	ButtonDanger = lipgloss.NewStyle().
			Foreground(DangerLight).
			Background(DangerDark).
			Padding(0, 2)

	ButtonDangerFocused = lipgloss.NewStyle().
				Foreground(TextInverse).
				Background(DangerBright).
				Padding(0, 2).
				Bold(true)

	ButtonDangerHover = lipgloss.NewStyle().
				Foreground(TextInverse).
				Background(DangerHover).
				Padding(0, 2)
)

// RenderTab renders a tab label using the current tab theme.
// tabIndex is the 0-based index of this tab, totalTabs is the total count.
// If isPreview is true, the label is rendered in italic to indicate an ephemeral preview tab.
func RenderTab(label string, tabIndex, totalTabs int, isActive bool, isPreview bool) string {
	style := CurrentTabStyle
	colors := CurrentTabColors

	// Check if style is a preset name
	if preset := GetTabPreset(style); preset != nil {
		style = preset.Style
		if len(preset.Colors) > 0 {
			colors = parseTabColors(preset.Colors)
		}
	}

	switch style {
	case "gradient":
		return renderGradientTab(label, tabIndex, totalTabs, isActive, isPreview, colors)
	case "per-tab":
		return renderPerTabColor(label, tabIndex, isActive, isPreview, colors)
	case "solid":
		return renderSolidTab(label, isActive, isPreview)
	case "minimal":
		return renderMinimalTab(label, isActive, isPreview)
	default:
		// Default to gradient
		return renderGradientTab(label, tabIndex, totalTabs, isActive, isPreview, colors)
	}
}

// RenderGradientTab renders a tab label with a gradient background.
// Kept for backwards compatibility - delegates to RenderTab.
func RenderGradientTab(label string, tabIndex, totalTabs int, isActive bool) string {
	return renderGradientTab(label, tabIndex, totalTabs, isActive, false, CurrentTabColors)
}

func tabTextColor(isActive bool, backgrounds []RGB) lipgloss.Color {
	minTarget := 3.5
	candidates := []lipgloss.Color{TextSecondary, TextPrimary, TextMuted}
	if isActive {
		minTarget = 4.5
		candidates = []lipgloss.Color{TextPrimary, TextSecondary, TextMuted}
	}

	best := candidates[0]
	bestRatio := minContrastRatio(colorToRGB(best), backgrounds)
	for _, candidate := range candidates[1:] {
		if ratio := minContrastRatio(colorToRGB(candidate), backgrounds); ratio > bestRatio {
			best = candidate
			bestRatio = ratio
		}
	}

	if bestRatio < minTarget {
		for _, candidate := range []lipgloss.Color{lipgloss.Color("#000000"), lipgloss.Color("#ffffff")} {
			if ratio := minContrastRatio(colorToRGB(candidate), backgrounds); ratio > bestRatio {
				best = candidate
				bestRatio = ratio
			}
		}
	}

	return best
}

func colorToRGB(c lipgloss.Color) RGB {
	return HexToRGB(string(c))
}

// Pill tab cap characters (Powerline/Nerd Font)
const (
	pillLeftCap  = "\ue0b6" //
	pillRightCap = "\ue0b4" //
)

// RenderPill renders text with pill-shaped caps when NerdFontsEnabled (PillTabsEnabled) is true.
// fg is the text color, bg is the pill background, outerBg is the surrounding background.
// If outerBg is empty, defaults to BgSecondary.
func RenderPill(text string, fg, bg, outerBg lipgloss.Color) string {
	if outerBg == "" {
		outerBg = BgSecondary
	}

	style := lipgloss.NewStyle().Foreground(fg).Background(bg).Padding(0, 1)
	content := style.Render(text)

	if !PillTabsEnabled {
		return content
	}

	leftCap := lipgloss.NewStyle().Foreground(bg).Background(outerBg).Render(pillLeftCap)
	rightCap := lipgloss.NewStyle().Foreground(bg).Background(outerBg).Render(pillRightCap)

	// Re-render content without horizontal padding since caps provide visual space
	style = lipgloss.NewStyle().Foreground(fg).Background(bg)
	content = style.Render(" " + text + " ")

	return leftCap + content + rightCap
}

// RenderPillWithStyle renders text with pill-shaped caps using the provided lipgloss.Style.
// The style's background color is used for the pill caps.
// outerBg is the surrounding background; if empty, defaults to BgSecondary.
func RenderPillWithStyle(text string, style lipgloss.Style, outerBg lipgloss.Color) string {
	if outerBg == "" {
		outerBg = BgSecondary
	}

	// Get background from style for pill caps
	bg, _ := style.GetBackground().(lipgloss.Color)
	if bg == "" {
		bg = BgTertiary // fallback
	}

	content := style.Render(text)

	if !PillTabsEnabled {
		return content
	}

	leftCap := lipgloss.NewStyle().Foreground(bg).Background(outerBg).Render(pillLeftCap)
	rightCap := lipgloss.NewStyle().Foreground(bg).Background(outerBg).Render(pillRightCap)

	// Re-render content - adjust padding for pill caps
	// Remove existing padding and add single space padding
	innerStyle := style.Padding(0, 0)
	content = innerStyle.Render(" " + text + " ")

	return leftCap + content + rightCap
}

// renderGradientTab renders a tab with per-character gradient coloring.
func renderGradientTab(label string, tabIndex, totalTabs int, isActive bool, isPreview bool, colors []RGB) string {
	if totalTabs == 0 {
		totalTabs = 1
	}
	if tabIndex < 0 {
		tabIndex = 0
	}

	// Calculate position in the gradient (0.0 to 1.0 across all tabs)
	tabWidth := 1.0 / float64(totalTabs)
	startPos := float64(tabIndex) * tabWidth
	endPos := startPos + tabWidth

	// Add padding to label (less if using pill caps since they add visual width)
	padded := " " + label + " "
	if !PillTabsEnabled {
		padded = "  " + label + "  "
	}
	chars := []rune(padded)
	backgrounds := make([]RGB, len(chars))
	result := ""

	for i := range chars {
		// Position within the gradient for this character
		charPos := startPos + (endPos-startPos)*float64(i)/float64(len(chars))

		// Get interpolated color
		r, g, b := interpolateColors(charPos, colors)

		// Mute colors for inactive tabs
		if !isActive {
			r = uint8(float64(r)*0.35 + 30)
			g = uint8(float64(g)*0.35 + 30)
			b = uint8(float64(b)*0.35 + 30)
		}
		backgrounds[i] = RGB{float64(r), float64(g), float64(b)}
	}

	// Left pill cap: foreground = first char's bg, background = header bg
	if PillTabsEnabled {
		leftBg := lipgloss.Color(RGBToHex(backgrounds[0]))
		leftCap := lipgloss.NewStyle().Foreground(leftBg).Background(BgSecondary).Render(pillLeftCap)
		result += leftCap
	}

	textColor := tabTextColor(isActive, backgrounds)
	for i, ch := range chars {
		bg := lipgloss.Color(RGBToHex(backgrounds[i]))
		var style lipgloss.Style
		if isActive {
			style = lipgloss.NewStyle().Background(bg).Foreground(textColor).Bold(true)
		} else {
			style = lipgloss.NewStyle().Background(bg).Foreground(textColor)
		}
		if isPreview {
			style = style.Italic(true)
		}
		result += style.Render(string(ch))
	}

	// Right pill cap: foreground = last char's bg, background = header bg
	if PillTabsEnabled {
		rightBg := lipgloss.Color(RGBToHex(backgrounds[len(backgrounds)-1]))
		rightCap := lipgloss.NewStyle().Foreground(rightBg).Background(BgSecondary).Render(pillRightCap)
		result += rightCap
	}

	return result
}

// renderPerTabColor renders a tab with a single solid color from the colors array.
func renderPerTabColor(label string, tabIndex int, isActive bool, isPreview bool, colors []RGB) string {
	if len(colors) == 0 {
		return renderSolidTab(label, isActive, isPreview)
	}

	// Guard against negative tabIndex (Go modulo returns negative for negative input)
	if tabIndex < 0 {
		tabIndex = 0
	}

	// Get color for this tab (cycle through available colors)
	color := colors[tabIndex%len(colors)]
	r, g, b := uint8(color.R), uint8(color.G), uint8(color.B)

	// Mute colors for inactive tabs
	if !isActive {
		r = uint8(float64(r)*0.35 + 30)
		g = uint8(float64(g)*0.35 + 30)
		b = uint8(float64(b)*0.35 + 30)
	}

	bgColor := RGB{float64(r), float64(g), float64(b)}
	textColor := tabTextColor(isActive, []RGB{bgColor})
	bg := lipgloss.Color(RGBToHex(bgColor))

	padded := " " + label + " "
	if !PillTabsEnabled {
		padded = "  " + label + "  "
	}

	var style lipgloss.Style
	if isActive {
		style = lipgloss.NewStyle().Background(bg).Foreground(textColor).Bold(true)
	} else {
		style = lipgloss.NewStyle().Background(bg).Foreground(textColor)
	}
	if isPreview {
		style = style.Italic(true)
	}

	if PillTabsEnabled {
		leftCap := lipgloss.NewStyle().Foreground(bg).Background(BgSecondary).Render(pillLeftCap)
		rightCap := lipgloss.NewStyle().Foreground(bg).Background(BgSecondary).Render(pillRightCap)
		return leftCap + style.Render(padded) + rightCap
	}
	return style.Render(padded)
}

// renderSolidTab renders a tab with the theme's primary/tertiary colors.
func renderSolidTab(label string, isActive bool, isPreview bool) string {
	padded := " " + label + " "
	if !PillTabsEnabled {
		padded = "  " + label + "  "
	}

	var bg lipgloss.Color
	if isActive {
		bg = Primary
	} else {
		bg = BgTertiary
	}

	textColor := tabTextColor(isActive, []RGB{colorToRGB(bg)})
	style := lipgloss.NewStyle().Background(bg).Foreground(textColor)
	if isActive {
		style = style.Bold(true)
	}
	if isPreview {
		style = style.Italic(true)
	}

	if PillTabsEnabled {
		leftCap := lipgloss.NewStyle().Foreground(bg).Background(BgSecondary).Render(pillLeftCap)
		rightCap := lipgloss.NewStyle().Foreground(bg).Background(BgSecondary).Render(pillRightCap)
		return leftCap + style.Render(padded) + rightCap
	}
	return style.Render(padded)
}

// renderMinimalTab renders a tab with no background, using underline for active.
func renderMinimalTab(label string, isActive bool, isPreview bool) string {
	padded := "  " + label + "  "

	var style lipgloss.Style
	if isActive {
		style = lipgloss.NewStyle().Foreground(Primary).Bold(true).Underline(true)
	} else {
		style = lipgloss.NewStyle().Foreground(TextMuted)
	}
	if isPreview {
		style = style.Italic(true)
	}

	return style.Render(padded)
}

// interpolateColors returns RGB for a position 0.0-1.0 across the color array
func interpolateColors(pos float64, colors []RGB) (uint8, uint8, uint8) {
	if len(colors) < 2 {
		if len(colors) == 1 {
			return uint8(colors[0].R), uint8(colors[0].G), uint8(colors[0].B)
		}
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
	r := uint8(c1.R + frac*(c2.R-c1.R))
	g := uint8(c1.G + frac*(c2.G-c1.G))
	b := uint8(c1.B + frac*(c2.B-c1.B))

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

// parseTabColors converts hex color strings to RGB values for tab rendering
func parseTabColors(hexColors []string) []RGB {
	if len(hexColors) == 0 {
		// Return default rainbow colors
		return []RGB{{220, 60, 60}, {60, 220, 60}, {60, 60, 220}, {156, 60, 220}}
	}

	colors := make([]RGB, len(hexColors))
	for i, hex := range hexColors {
		colors[i] = HexToRGB(hex)
	}
	return colors
}
