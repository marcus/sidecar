package styles

import (
	"regexp"
	"sort"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// themeMu protects access to themeRegistry and currentTheme for thread safety
var themeMu sync.RWMutex

// hexColorRegex validates hex color codes
var hexColorRegex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// ColorPalette holds all theme colors
type ColorPalette struct {
	// Brand colors
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
	Accent    string `json:"accent"`

	// Status colors
	Success string `json:"success"`
	Warning string `json:"warning"`
	Error   string `json:"error"`
	Info    string `json:"info"`

	// Text colors
	TextPrimary   string `json:"textPrimary"`
	TextSecondary string `json:"textSecondary"`
	TextMuted     string `json:"textMuted"`
	TextSubtle    string `json:"textSubtle"`

	// Background colors
	BgPrimary   string `json:"bgPrimary"`
	BgSecondary string `json:"bgSecondary"`
	BgTertiary  string `json:"bgTertiary"`
	BgOverlay   string `json:"bgOverlay"`

	// Border colors
	BorderNormal string `json:"borderNormal"`
	BorderActive string `json:"borderActive"`
	BorderMuted  string `json:"borderMuted"`

	// Diff colors
	DiffAddFg    string `json:"diffAddFg"`
	DiffAddBg    string `json:"diffAddBg"`
	DiffRemoveFg string `json:"diffRemoveFg"`
	DiffRemoveBg string `json:"diffRemoveBg"`

	// Additional UI colors
	TextHighlight   string `json:"textHighlight"`   // For subtitle, special text
	ButtonHover     string `json:"buttonHover"`     // Button hover state
	TabTextInactive string `json:"tabTextInactive"` // Inactive tab text
	Link            string `json:"link"`            // Hyperlink color
	ToastSuccessText string `json:"toastSuccessText"` // Toast success foreground
	ToastErrorText   string `json:"toastErrorText"`   // Toast error foreground

	// Third-party theme names
	SyntaxTheme   string `json:"syntaxTheme"`   // Chroma theme name
	MarkdownTheme string `json:"markdownTheme"` // Glamour theme name
}

// Theme represents a complete theme configuration
type Theme struct {
	Name        string       `json:"name"`
	DisplayName string       `json:"displayName"`
	Colors      ColorPalette `json:"colors"`
}

// Built-in themes
var (
	// DefaultTheme is the current dark theme (backwards compatible)
	DefaultTheme = Theme{
		Name:        "default",
		DisplayName: "Default Dark",
		Colors: ColorPalette{
			// Brand colors
			Primary:   "#7C3AED", // Purple
			Secondary: "#3B82F6", // Blue
			Accent:    "#F59E0B", // Amber

			// Status colors
			Success: "#10B981", // Green
			Warning: "#F59E0B", // Amber
			Error:   "#EF4444", // Red
			Info:    "#3B82F6", // Blue

			// Text colors
			TextPrimary:   "#F9FAFB",
			TextSecondary: "#9CA3AF",
			TextMuted:     "#6B7280",
			TextSubtle:    "#4B5563",

			// Background colors
			BgPrimary:   "#111827",
			BgSecondary: "#1F2937",
			BgTertiary:  "#374151",
			BgOverlay:   "#00000080",

			// Border colors
			BorderNormal: "#374151",
			BorderActive: "#7C3AED",
			BorderMuted:  "#1F2937",

			// Diff colors
			DiffAddFg:    "#10B981",
			DiffAddBg:    "#0D2818",
			DiffRemoveFg: "#EF4444",
			DiffRemoveBg: "#2D1A1A",

			// Additional UI colors
			TextHighlight:    "#E5E7EB",
			ButtonHover:      "#9D174D",
			TabTextInactive:  "#1a1a1a",
			Link:             "#60A5FA", // Light blue for links
			ToastSuccessText: "#000000", // Black on green
			ToastErrorText:   "#FFFFFF", // White on red

			// Third-party themes
			SyntaxTheme:   "monokai",
			MarkdownTheme: "dark",
		},
	}

	// DraculaTheme is a Dracula-inspired dark theme with vibrant colors
	DraculaTheme = Theme{
		Name:        "dracula",
		DisplayName: "Dracula",
		Colors: ColorPalette{
			// Brand colors - Dracula palette
			Primary:   "#BD93F9", // Purple
			Secondary: "#8BE9FD", // Cyan
			Accent:    "#FFB86C", // Orange

			// Status colors
			Success: "#50FA7B", // Green
			Warning: "#FFB86C", // Orange
			Error:   "#FF5555", // Red
			Info:    "#8BE9FD", // Cyan

			// Text colors
			TextPrimary:   "#F8F8F2", // Foreground
			TextSecondary: "#BFBFBF",
			TextMuted:     "#6272A4", // Comment
			TextSubtle:    "#44475A", // Current Line

			// Background colors
			BgPrimary:   "#282A36", // Background
			BgSecondary: "#343746",
			BgTertiary:  "#44475A", // Current Line
			BgOverlay:   "#00000080",

			// Border colors
			BorderNormal: "#44475A",
			BorderActive: "#BD93F9",
			BorderMuted:  "#343746",

			// Diff colors
			DiffAddFg:    "#50FA7B",
			DiffAddBg:    "#1E3A29",
			DiffRemoveFg: "#FF5555",
			DiffRemoveBg: "#3D2A2A",

			// Additional UI colors
			TextHighlight:    "#F8F8F2",
			ButtonHover:      "#FF79C6", // Pink
			TabTextInactive:  "#282A36",
			Link:             "#8BE9FD", // Cyan for links (Dracula)
			ToastSuccessText: "#282A36", // Dark bg on green
			ToastErrorText:   "#F8F8F2", // Light on red

			// Third-party themes
			SyntaxTheme:   "dracula",
			MarkdownTheme: "dark",
		},
	}
)

// themeRegistry holds all available themes
var themeRegistry = map[string]Theme{
	"default": DefaultTheme,
	"dracula": DraculaTheme,
}

// currentTheme tracks the active theme name
var currentTheme = "default"

// IsValidHexColor checks if a string is a valid hex color code (#RRGGBB)
func IsValidHexColor(hex string) bool {
	return hexColorRegex.MatchString(hex)
}

// IsValidTheme checks if a theme name exists in the registry
func IsValidTheme(name string) bool {
	themeMu.RLock()
	defer themeMu.RUnlock()
	_, ok := themeRegistry[name]
	return ok
}

// GetTheme returns a theme by name, or the default theme if not found
func GetTheme(name string) Theme {
	themeMu.RLock()
	defer themeMu.RUnlock()
	if theme, ok := themeRegistry[name]; ok {
		return theme
	}
	return DefaultTheme
}

// GetCurrentTheme returns the currently active theme
func GetCurrentTheme() Theme {
	themeMu.RLock()
	name := currentTheme
	themeMu.RUnlock()
	return GetTheme(name)
}

// GetCurrentThemeName returns the name of the currently active theme
func GetCurrentThemeName() string {
	themeMu.RLock()
	defer themeMu.RUnlock()
	return currentTheme
}

// ListThemes returns the names of all available themes in sorted order
func ListThemes() []string {
	themeMu.RLock()
	defer themeMu.RUnlock()
	names := make([]string, 0, len(themeRegistry))
	for name := range themeRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RegisterTheme adds a custom theme to the registry
func RegisterTheme(theme Theme) {
	themeMu.Lock()
	defer themeMu.Unlock()
	themeRegistry[theme.Name] = theme
}

// ApplyTheme applies a theme by name, updating all style variables
func ApplyTheme(name string) {
	theme := GetTheme(name)
	ApplyThemeColors(theme)
	themeMu.Lock()
	currentTheme = name
	themeMu.Unlock()
}

// ApplyThemeWithOverrides applies a theme with color overrides from config
func ApplyThemeWithOverrides(name string, overrides map[string]string) {
	theme := GetTheme(name)

	// Apply overrides to the color palette
	if overrides != nil {
		applyOverrides(&theme.Colors, overrides)
	}

	ApplyThemeColors(theme)
	themeMu.Lock()
	currentTheme = name
	themeMu.Unlock()
}

// applyOverrides applies color overrides to a palette
func applyOverrides(palette *ColorPalette, overrides map[string]string) {
	for key, value := range overrides {
		switch key {
		case "primary":
			palette.Primary = value
		case "secondary":
			palette.Secondary = value
		case "accent":
			palette.Accent = value
		case "success":
			palette.Success = value
		case "warning":
			palette.Warning = value
		case "error":
			palette.Error = value
		case "info":
			palette.Info = value
		case "textPrimary":
			palette.TextPrimary = value
		case "textSecondary":
			palette.TextSecondary = value
		case "textMuted":
			palette.TextMuted = value
		case "textSubtle":
			palette.TextSubtle = value
		case "bgPrimary":
			palette.BgPrimary = value
		case "bgSecondary":
			palette.BgSecondary = value
		case "bgTertiary":
			palette.BgTertiary = value
		case "bgOverlay":
			palette.BgOverlay = value
		case "borderNormal":
			palette.BorderNormal = value
		case "borderActive":
			palette.BorderActive = value
		case "borderMuted":
			palette.BorderMuted = value
		case "diffAddFg":
			palette.DiffAddFg = value
		case "diffAddBg":
			palette.DiffAddBg = value
		case "diffRemoveFg":
			palette.DiffRemoveFg = value
		case "diffRemoveBg":
			palette.DiffRemoveBg = value
		case "textHighlight":
			palette.TextHighlight = value
		case "buttonHover":
			palette.ButtonHover = value
		case "tabTextInactive":
			palette.TabTextInactive = value
		case "link":
			palette.Link = value
		case "toastSuccessText":
			palette.ToastSuccessText = value
		case "toastErrorText":
			palette.ToastErrorText = value
		case "syntaxTheme":
			palette.SyntaxTheme = value
		case "markdownTheme":
			palette.MarkdownTheme = value
		}
	}
}

// ApplyThemeColors updates all style package variables from a theme
func ApplyThemeColors(theme Theme) {
	c := theme.Colors

	// Update color variables
	Primary = lipgloss.Color(c.Primary)
	Secondary = lipgloss.Color(c.Secondary)
	Accent = lipgloss.Color(c.Accent)

	Success = lipgloss.Color(c.Success)
	Warning = lipgloss.Color(c.Warning)
	Error = lipgloss.Color(c.Error)
	Info = lipgloss.Color(c.Info)

	TextPrimary = lipgloss.Color(c.TextPrimary)
	TextSecondary = lipgloss.Color(c.TextSecondary)
	TextMuted = lipgloss.Color(c.TextMuted)
	TextSubtle = lipgloss.Color(c.TextSubtle)

	BgPrimary = lipgloss.Color(c.BgPrimary)
	BgSecondary = lipgloss.Color(c.BgSecondary)
	BgTertiary = lipgloss.Color(c.BgTertiary)
	BgOverlay = lipgloss.Color(c.BgOverlay)

	BorderNormal = lipgloss.Color(c.BorderNormal)
	BorderActive = lipgloss.Color(c.BorderActive)
	BorderMuted = lipgloss.Color(c.BorderMuted)

	DiffAddFg = lipgloss.Color(c.DiffAddFg)
	DiffAddBg = lipgloss.Color(c.DiffAddBg)
	DiffRemoveFg = lipgloss.Color(c.DiffRemoveFg)
	DiffRemoveBg = lipgloss.Color(c.DiffRemoveBg)

	TextHighlight = lipgloss.Color(c.TextHighlight)
	ButtonHoverColor = lipgloss.Color(c.ButtonHover)
	TabTextInactiveColor = lipgloss.Color(c.TabTextInactive)
	LinkColor = lipgloss.Color(c.Link)
	ToastSuccessTextColor = lipgloss.Color(c.ToastSuccessText)
	ToastErrorTextColor = lipgloss.Color(c.ToastErrorText)

	// Store syntax/markdown theme names for external use
	CurrentSyntaxTheme = c.SyntaxTheme
	CurrentMarkdownTheme = c.MarkdownTheme

	// Rebuild all styles that depend on these colors
	rebuildStyles()
}

// rebuildStyles recreates all lipgloss styles with current colors
func rebuildStyles() {
	// Panel styles
	PanelActive = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(BorderActive).
		Padding(0, 1)

	PanelInactive = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(BorderNormal).
		Padding(0, 1)

	PanelHeader = lipgloss.NewStyle().
		Bold(true).
		Foreground(TextPrimary).
		MarginBottom(1)

	PanelNoBorder = lipgloss.NewStyle().
		Padding(0, 1)

	// Text styles
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

	// Status indicator styles
	StatusStaged = lipgloss.NewStyle().
		Foreground(Success).
		Bold(true)

	StatusModified = lipgloss.NewStyle().
		Foreground(Warning).
		Bold(true)

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

	// List item styles
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

	// Bar element styles
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

	// Tab styles
	TabTextActive = lipgloss.NewStyle().
		Foreground(TextPrimary).
		Bold(true)

	TabTextInactive = lipgloss.NewStyle().
		Foreground(TabTextInactiveColor)

	// Diff line styles
	DiffAdd = lipgloss.NewStyle().
		Foreground(Success)

	DiffRemove = lipgloss.NewStyle().
		Foreground(Error)

	DiffContext = lipgloss.NewStyle().
		Foreground(TextMuted)

	DiffHeader = lipgloss.NewStyle().
		Foreground(Info).
		Bold(true)

	// File browser styles
	FileBrowserDir = lipgloss.NewStyle().
		Foreground(Secondary).
		Bold(true)

	FileBrowserFile = lipgloss.NewStyle().
		Foreground(TextPrimary)

	FileBrowserIgnored = lipgloss.NewStyle().
		Foreground(TextSubtle)

	FileBrowserLineNumber = lipgloss.NewStyle().
		Foreground(TextMuted).
		Width(5).
		AlignHorizontal(lipgloss.Right)

	FileBrowserIcon = lipgloss.NewStyle().
		Foreground(TextMuted)

	SearchMatch = lipgloss.NewStyle().
		Background(Warning)

	SearchMatchCurrent = lipgloss.NewStyle().
		Background(Primary).
		Foreground(TextPrimary)

	FuzzyMatchChar = lipgloss.NewStyle().
		Foreground(Primary).
		Bold(true)

	QuickOpenItem = lipgloss.NewStyle().
		Foreground(TextPrimary)

	QuickOpenItemSelected = lipgloss.NewStyle().
		Foreground(TextPrimary).
		Background(BgTertiary)

	PaletteEntry = lipgloss.NewStyle().
		Foreground(TextPrimary)

	PaletteEntrySelected = lipgloss.NewStyle().
		Foreground(TextPrimary).
		Background(BgTertiary)

	PaletteKey = lipgloss.NewStyle().
		Foreground(TextMuted).
		Background(BgTertiary).
		Padding(0, 1)

	TextSelection = lipgloss.NewStyle().
		Background(BgTertiary).
		Foreground(TextPrimary)

	// Footer and header
	Footer = lipgloss.NewStyle().
		Foreground(TextMuted).
		Background(BgSecondary)

	Header = lipgloss.NewStyle().
		Background(BgSecondary)

	// Modal styles
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

	// Button styles
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
}

// GetSyntaxTheme returns the current syntax highlighting theme name
func GetSyntaxTheme() string {
	return CurrentSyntaxTheme
}

// GetMarkdownTheme returns the current markdown rendering theme name
func GetMarkdownTheme() string {
	return CurrentMarkdownTheme
}
