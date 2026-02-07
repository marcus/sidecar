package workspace

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/styles"
)

const (
	promptPickerFilterID   = "prompt-picker-filter"
	promptPickerItemPrefix = "prompt-picker-item-"
	promptPickerNoneID     = "prompt-picker-item-none"
)

var (
	promptPickerSelectedStyle = lipgloss.NewStyle().Foreground(styles.Primary)
	promptPickerHoverStyle    = lipgloss.NewStyle().Foreground(styles.TextSecondary)
)

func promptPickerItemID(idx int) string {
	if idx < 0 {
		return promptPickerNoneID
	}
	return fmt.Sprintf("%s%d", promptPickerItemPrefix, idx)
}

func parsePromptPickerItemID(id string) (int, bool) {
	if id == promptPickerNoneID {
		return -1, true
	}
	if !strings.HasPrefix(id, promptPickerItemPrefix) {
		return 0, false
	}
	idx, err := strconv.Atoi(strings.TrimPrefix(id, promptPickerItemPrefix))
	if err != nil {
		return 0, false
	}
	return idx, true
}

func (p *Plugin) ensurePromptPickerModal() {
	if p.promptPicker == nil {
		return
	}

	modalW := 80
	if modalW > p.width-4 {
		modalW = p.width - 4
	}
	if modalW < 40 {
		modalW = 40
	}

	isEmpty := len(p.promptPicker.prompts) == 0
	if p.promptPickerModal != nil && p.promptPickerModalWidth == modalW && p.promptPickerModalEmpty == isEmpty {
		return
	}

	p.promptPickerModalWidth = modalW
	p.promptPickerModalEmpty = isEmpty

	if isEmpty {
		p.promptPickerModal = modal.New("Select Prompt",
			modal.WithWidth(modalW),
			modal.WithHints(false),
		).
			AddSection(p.promptPickerEmptySection())
		return
	}

	p.promptPickerModal = modal.New("Select Prompt",
		modal.WithWidth(modalW),
		modal.WithHints(false),
	).
		AddSection(modal.InputWithLabel(promptPickerFilterID, "Filter:", &p.promptPicker.filterInput, modal.WithSubmitOnEnter(false))).
		AddSection(modal.Spacer()).
		AddSection(p.promptPickerHeaderSection()).
		AddSection(p.promptPickerSeparatorSection()).
		AddSection(p.promptPickerListSection()).
		AddSection(modal.When(p.promptPickerHasMore, p.promptPickerMoreSection()))
}

func (p *Plugin) syncPromptPickerFocus() {
	if p.promptPicker == nil || p.promptPickerModal == nil {
		return
	}

	if p.promptPicker.filterFocused {
		p.promptPickerModal.SetFocus(promptPickerFilterID)
		return
	}

	p.promptPickerModal.SetFocus(promptPickerItemID(p.promptPicker.selectedIdx))
}

func (p *Plugin) promptPickerSelectCmd() tea.Cmd {
	pp := p.promptPicker
	if pp == nil {
		return nil
	}

	if pp.selectedIdx < 0 {
		return func() tea.Msg { return PromptSelectedMsg{Prompt: nil} }
	}
	if pp.selectedIdx < len(pp.filtered) {
		prompt := pp.filtered[pp.selectedIdx]
		return func() tea.Msg { return PromptSelectedMsg{Prompt: &prompt} }
	}
	return nil
}

func (p *Plugin) promptPickerEmptySection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		var sb strings.Builder

		sb.WriteString("No prompts configured.\n\n")
		sb.WriteString(styles.Muted.Render("Add prompts to one of these config files:"))
		sb.WriteString("\n")
		sb.WriteString(styles.Muted.Render("  Global:  ~/.config/sidecar/config.json"))
		sb.WriteString("\n")
		sb.WriteString(styles.Muted.Render("  Project: .sidecar/config.json"))
		sb.WriteString("\n\n")
		sb.WriteString(styles.Muted.Render("See: .claude/skills/create-prompt/SKILL.md"))
		sb.WriteString("\n\n")
		sb.WriteString(styles.Muted.Render("Press d to add defaults, or Esc/Enter to continue without a prompt."))
		return modal.RenderedSection{Content: sb.String()}
	}, nil)
}

func (p *Plugin) promptPickerHeaderSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		header := fmt.Sprintf("  %-24s %-7s %-10s %s", "Prompt", "Scope", "Ticket", "Preview")
		rendered := styles.Muted.Render(header)
		return modal.RenderedSection{Content: ansi.Truncate(rendered, contentWidth, "")}
	}, nil)
}

func (p *Plugin) promptPickerSeparatorSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		line := strings.Repeat("─", min(contentWidth, 70))
		return modal.RenderedSection{Content: styles.Muted.Render(line)}
	}, nil)
}

func (p *Plugin) promptPickerListSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		pp := p.promptPicker
		if pp == nil {
			return modal.RenderedSection{}
		}

		var sb strings.Builder
		focusables := make([]modal.FocusableInfo, 0, 1+len(pp.filtered))

		noneLine := p.promptPickerNoneLine(pp.selectedIdx == -1, hoverID == promptPickerNoneID, contentWidth)
		sb.WriteString(noneLine)
		focusables = append(focusables, modal.FocusableInfo{
			ID:      promptPickerNoneID,
			OffsetX: 0,
			OffsetY: 0,
			Width:   ansi.StringWidth(noneLine),
			Height:  1,
		})

		maxVisible := min(10, len(pp.filtered))
		for i := 0; i < maxVisible; i++ {
			prompt := pp.filtered[i]
			itemID := promptPickerItemID(i)
			selected := i == pp.selectedIdx
			hovered := itemID == hoverID

			line := p.promptPickerPromptLine(prompt, i, selected, hovered, contentWidth)
			sb.WriteString("\n")
			sb.WriteString(line)

			focusables = append(focusables, modal.FocusableInfo{
				ID:      itemID,
				OffsetX: 0,
				OffsetY: i + 1,
				Width:   ansi.StringWidth(line),
				Height:  1,
			})
		}

		return modal.RenderedSection{Content: sb.String(), Focusables: focusables}
	}, p.promptPickerListUpdate)
}

func (p *Plugin) promptPickerListUpdate(msg tea.Msg, focusID string) (string, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", nil
	}

	if keyMsg.String() != "enter" {
		return "", nil
	}

	if _, ok := parsePromptPickerItemID(focusID); ok {
		return focusID, nil
	}
	return "", nil
}

func (p *Plugin) promptPickerHasMore() bool {
	pp := p.promptPicker
	return pp != nil && len(pp.filtered) > 10
}

func (p *Plugin) promptPickerMoreSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		pp := p.promptPicker
		if pp == nil {
			return modal.RenderedSection{}
		}

		extra := len(pp.filtered) - 10
		if extra <= 0 {
			return modal.RenderedSection{}
		}

		line := fmt.Sprintf("  ... and %d more", extra)
		return modal.RenderedSection{Content: styles.Muted.Render(line)}
	}, nil)
}

func (p *Plugin) promptPickerNoneLine(selected, hovered bool, width int) string {
	prefix := "  "
	if selected {
		prefix = "▶ "
	}

	line := fmt.Sprintf("%s%-24s %-7s %-10s %s", prefix, "(none)", "", "", "No prompt template")
	line = ansi.Truncate(line, width, "")

	if selected {
		return promptPickerSelectedStyle.Render(line)
	}
	if hovered {
		return promptPickerHoverStyle.Render(line)
	}
	return styles.Muted.Render(line)
}

func (p *Plugin) promptPickerPromptLine(prompt Prompt, idx int, selected, hovered bool, width int) string {
	prefix := "  "
	if selected {
		prefix = "▶ "
	}

	scope := "[G]"
	if prompt.Source == "project" {
		scope = "[P]"
	}
	ticket := string(prompt.TicketMode)

	preview := strings.ReplaceAll(prompt.Body, "\n", " ")
	baseWidth := 46
	maxPreview := width - baseWidth
	if maxPreview < 5 {
		maxPreview = 5
	}
	if runes := []rune(preview); len(runes) > maxPreview {
		preview = string(runes[:maxPreview-3]) + "..."
	}

	line := fmt.Sprintf("%s%-24s %-7s %-10s %s", prefix, truncateString(prompt.Name, 24), scope, ticket, preview)
	line = ansi.Truncate(line, width, "")

	if selected {
		return promptPickerSelectedStyle.Render(line)
	}
	if hovered {
		return promptPickerHoverStyle.Render(line)
	}
	return styles.Muted.Render(line)
}
