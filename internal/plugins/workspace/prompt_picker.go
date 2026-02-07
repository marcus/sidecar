package workspace

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// PromptPicker is a modal for selecting a prompt template.
type PromptPicker struct {
	prompts       []Prompt        // all available prompts
	filtered      []Prompt        // filtered by query
	filterInput   textinput.Model // filter text input
	selectedIdx   int             // highlighted row (0-based into filtered, -1 = none option)
	hoverIdx      int             // hovered row for mouse feedback (-2 = no hover, -1 = none, 0+ = prompt)
	filterFocused bool            // true when filter has keyboard focus (vs item list)
	width         int
	height        int
}

// PromptSelectedMsg is sent when a prompt is selected.
type PromptSelectedMsg struct {
	Prompt *Prompt // nil means "none" was selected
}

// PromptCancelledMsg is sent when the picker is cancelled.
type PromptCancelledMsg struct{}

// PromptInstallDefaultsMsg is sent when user requests installing default prompts.
type PromptInstallDefaultsMsg struct{}

// NewPromptPicker creates a new prompt picker.
func NewPromptPicker(prompts []Prompt, width, height int) *PromptPicker {
	ti := textinput.New()
	ti.Placeholder = "Type to filter..."
	ti.Prompt = ""
	ti.Width = 30
	ti.Focus() // Focus filter input by default when picker opens

	pp := &PromptPicker{
		prompts:       prompts,
		filtered:      prompts,
		filterInput:   ti,
		selectedIdx:   -1,   // Start on "none" option
		hoverIdx:      -2,   // No hover initially
		filterFocused: true, // Start with filter focused
		width:         width,
		height:        height,
	}
	return pp
}

// Update handles input for the prompt picker.
func (pp *PromptPicker) Update(msg tea.Msg) (*PromptPicker, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()

		// When no prompts configured, handle 'd' to install defaults
		if len(pp.prompts) == 0 && key == "d" {
			return pp, func() tea.Msg { return PromptInstallDefaultsMsg{} }
		}

		// Universal keys - always handled regardless of focus
		switch key {
		case "esc", "q":
			return pp, func() tea.Msg { return PromptCancelledMsg{} }

		case "tab", "shift+tab":
			// Toggle between filter focus and item list focus
			pp.filterFocused = !pp.filterFocused
			if pp.filterFocused {
				pp.filterInput.Focus()
			} else {
				pp.filterInput.Blur()
			}
			return pp, nil

		case "enter":
			if pp.selectedIdx < 0 {
				// "None" selected
				return pp, func() tea.Msg { return PromptSelectedMsg{Prompt: nil} }
			}
			if pp.selectedIdx < len(pp.filtered) {
				prompt := pp.filtered[pp.selectedIdx]
				return pp, func() tea.Msg { return PromptSelectedMsg{Prompt: &prompt} }
			}
			return pp, nil

		case "up":
			// Arrow keys always navigate
			if pp.selectedIdx > -1 {
				pp.selectedIdx--
			}
			return pp, nil

		case "down":
			// Arrow keys always navigate
			if pp.selectedIdx < len(pp.filtered)-1 {
				pp.selectedIdx++
			}
			return pp, nil
		}

		// When filter is focused, send most keys to text input
		if pp.filterFocused {
			var cmd tea.Cmd
			pp.filterInput, cmd = pp.filterInput.Update(msg)
			pp.applyFilter()
			return pp, cmd
		}

		// Navigation keys - only when filter NOT focused
		switch key {
		case "k":
			if pp.selectedIdx > -1 {
				pp.selectedIdx--
			}
			return pp, nil

		case "j":
			if pp.selectedIdx < len(pp.filtered)-1 {
				pp.selectedIdx++
			}
			return pp, nil

		case "home", "g":
			pp.selectedIdx = -1
			return pp, nil

		case "end", "G":
			if len(pp.filtered) > 0 {
				pp.selectedIdx = len(pp.filtered) - 1
			}
			return pp, nil
		}
	}
	return pp, nil
}

// applyFilter filters prompts based on the current filter input.
func (pp *PromptPicker) applyFilter() {
	query := strings.ToLower(pp.filterInput.Value())
	if query == "" {
		pp.filtered = pp.prompts
	} else {
		pp.filtered = make([]Prompt, 0)
		for _, p := range pp.prompts {
			if strings.Contains(strings.ToLower(p.Name), query) ||
				strings.Contains(strings.ToLower(p.Body), query) {
				pp.filtered = append(pp.filtered, p)
			}
		}
	}
	// Reset selection if out of bounds
	if pp.selectedIdx >= len(pp.filtered) {
		pp.selectedIdx = len(pp.filtered) - 1
	}
}

// FocusFilter focuses the filter input field.
func (pp *PromptPicker) FocusFilter() {
	pp.filterFocused = true
	pp.filterInput.Focus()
}

// SetHover sets the hover index for visual feedback.
func (pp *PromptPicker) SetHover(idx int) {
	pp.hoverIdx = idx
}

// ClearHover clears the hover state.
func (pp *PromptPicker) ClearHover() {
	pp.hoverIdx = -2
}

// View renders the prompt picker modal.
func (pp *PromptPicker) View() string {
	var sb strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true)
	sb.WriteString(headerStyle.Render("Select Prompt"))
	sb.WriteString(strings.Repeat(" ", max(0, pp.width-30)))
	sb.WriteString(dimText("Esc to cancel"))
	sb.WriteString("\n\n")

	// Handle empty prompts case
	if len(pp.prompts) == 0 {
		sb.WriteString("No prompts configured.\n\n")
		sb.WriteString(dimText("Add prompts to one of these config files:"))
		sb.WriteString("\n")
		sb.WriteString(dimText("  Global:  ~/.config/sidecar/config.json"))
		sb.WriteString("\n")
		sb.WriteString(dimText("  Project: .sidecar/config.json"))
		sb.WriteString("\n\n")
		sb.WriteString(dimText("See: .claude/skills/create-prompt/SKILL.md"))
		sb.WriteString("\n\n")
		sb.WriteString(dimText("d: add defaults   Esc/Enter: continue without prompt"))
		return sb.String()
	}

	// Filter input - show focused style when filter has keyboard focus
	sb.WriteString("Filter:")
	sb.WriteString("\n")
	filterStyle := inputStyle().Width(30)
	if pp.filterFocused {
		filterStyle = inputFocusedStyle().Width(30)
	}
	sb.WriteString(filterStyle.Render(pp.filterInput.View()))
	sb.WriteString("\n\n")

	// Column headers
	colStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
	sb.WriteString(colStyle.Render(fmt.Sprintf("  %-24s %-7s %-10s %s", "Prompt", "Scope", "Ticket", "Preview")))
	sb.WriteString("\n")
	sb.WriteString(colStyle.Render(strings.Repeat("─", min(pp.width-6, 70))))
	sb.WriteString("\n")

	// "None" option - style priority: selected > hover > default
	nonePrefix := "  "
	if pp.selectedIdx == -1 {
		nonePrefix = "▶ "
	}
	noneLine := fmt.Sprintf("%s%-24s %-7s %-10s %s", nonePrefix, "(none)", "", "", "No prompt template")
	if pp.selectedIdx == -1 {
		sb.WriteString(lipgloss.NewStyle().Foreground(styles.Primary).Render(noneLine))
	} else if pp.hoverIdx == -1 {
		sb.WriteString(lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(noneLine))
	} else {
		sb.WriteString(dimText(noneLine))
	}
	sb.WriteString("\n")

	// Prompt rows
	maxVisible := min(10, len(pp.filtered))
	for i := 0; i < maxVisible; i++ {
		p := pp.filtered[i]

		prefix := "  "
		if i == pp.selectedIdx {
			prefix = "▶ "
		}

		// Scope indicator
		scope := "[G]"
		if p.Source == "project" {
			scope = "[P]"
		}

		// Ticket mode
		ticket := string(p.TicketMode)

		// Preview (truncate, rune-safe for Unicode)
		preview := strings.ReplaceAll(p.Body, "\n", " ")
		maxPreview := pp.width - 50
		if maxPreview < 10 {
			maxPreview = 10
		}
		if runes := []rune(preview); len(runes) > maxPreview {
			preview = string(runes[:maxPreview-3]) + "..."
		}

		line := fmt.Sprintf("%s%-24s %-7s %-10s %s", prefix, truncateString(p.Name, 24), scope, ticket, preview)

		// Style priority: selected > hover > default
		if i == pp.selectedIdx {
			sb.WriteString(lipgloss.NewStyle().Foreground(styles.Primary).Render(line))
		} else if i == pp.hoverIdx {
			sb.WriteString(lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(line))
		} else {
			sb.WriteString(dimText(line))
		}
		sb.WriteString("\n")
	}

	if len(pp.filtered) > maxVisible {
		sb.WriteString(dimText(fmt.Sprintf("  ... and %d more", len(pp.filtered)-maxVisible)))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	if pp.filterFocused {
		sb.WriteString(dimText("  Enter: select   ↑/↓: move   Tab: list nav"))
	} else {
		sb.WriteString(dimText("  Enter: select   j/k/↑/↓: move   Tab: filter"))
	}

	return sb.String()
}

// truncateString truncates a string to maxLen runes, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// dimText is defined in view.go
