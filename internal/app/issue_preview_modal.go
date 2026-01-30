package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// Issue type icons and colors (matching td monitor).
var issueTypeIcons = map[string]string{
	"epic": "◆", "feature": "●", "bug": "✗", "task": "■", "chore": "○",
}
var issueTypeColors = map[string]lipgloss.Color{
	"epic": "212", "feature": "42", "bug": "196", "task": "45", "chore": "241",
}

func formatSearchTypeIcon(t string) string {
	k := strings.ToLower(t)
	icon := issueTypeIcons[k]
	if icon == "" {
		icon = "?"
	}
	c, ok := issueTypeColors[k]
	if !ok {
		return icon
	}
	return lipgloss.NewStyle().Foreground(c).Render(icon)
}

func formatSearchPriority(p string) string {
	var s lipgloss.Style
	switch strings.ToUpper(p) {
	case "P0":
		s = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	case "P1":
		s = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	case "P2":
		s = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))
	default:
		s = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	}
	return s.Render(p)
}

func formatSearchStatusTag(status string) string {
	switch strings.ToLower(status) {
	case "in_review":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Render("[REV]")
	case "open":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("[RDY]")
	case "blocked":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("[BLK]")
	case "in_progress":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("[WIP]")
	case "closed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("[CLS]")
	default:
		abbr := strings.ToUpper(status)
		if len(abbr) > 3 {
			abbr = abbr[:3]
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("[" + abbr + "]")
	}
}

func (m *Model) renderIssueInputOverlay(content string) string {
	m.ensureIssueInputModal()
	if m.issueInputModal == nil {
		return content
	}
	if m.issueInputMouseHandler == nil {
		m.issueInputMouseHandler = mouse.NewHandler()
	}
	rendered := m.issueInputModal.Render(m.width, m.height, m.issueInputMouseHandler)
	return ui.OverlayModal(content, rendered, m.width, m.height)
}

// issueSearchResultPrefix is the hit-region ID prefix for clickable search results.
const issueSearchResultPrefix = "issue-search-"

func (m *Model) ensureIssueInputModal() {
	modalW := 80
	if modalW > m.width-4 {
		modalW = m.width - 4
	}
	if modalW < 20 {
		modalW = 20
	}
	if m.issueInputModal != nil && m.issueInputModalWidth == modalW {
		return
	}
	m.issueInputModalWidth = modalW

	// Build footer hint string (always visible outside viewport)
	var hintBuf strings.Builder
	hasResults := len(m.issueSearchResults) > 0
	if hasResults {
		hintBuf.WriteString(styles.KeyHint.Render("enter"))
		hintBuf.WriteString(styles.Muted.Render(" open  "))
		hintBuf.WriteString(styles.KeyHint.Render("↑↓"))
		hintBuf.WriteString(styles.Muted.Render(" select  "))
		hintBuf.WriteString(styles.KeyHint.Render("tab"))
		hintBuf.WriteString(styles.Muted.Render(" fill  "))
	}
	if m.issueSearchIncludeClosed {
		hintBuf.WriteString(styles.KeyHint.Render("^x"))
		hintBuf.WriteString(styles.Muted.Render(" hide closed  "))
	} else {
		hintBuf.WriteString(styles.KeyHint.Render("^x"))
		hintBuf.WriteString(styles.Muted.Render(" show closed  "))
	}
	if hasResults {
		hintBuf.WriteString(styles.KeyHint.Render("esc"))
		hintBuf.WriteString(styles.Muted.Render(" cancel"))
	}

	b := modal.New("Open Issue",
		modal.WithWidth(modalW),
		modal.WithHints(false),
		modal.WithCustomFooter(hintBuf.String()),
	).
		AddSection(modal.Input("issue-id", &m.issueInputInput))

	// Status line — always present to avoid layout jumps
	if m.issueSearchLoading {
		b = b.AddSection(modal.Text(styles.Muted.Render("Searching...")))
	} else if len(m.issueSearchResults) > 0 {
		countStr := fmt.Sprintf("%d results", len(m.issueSearchResults))
		if !m.issueSearchIncludeClosed {
			countStr += " (excluding closed)"
		}
		b = b.AddSection(modal.Text(styles.Muted.Render(countStr)))
	} else {
		b = b.AddSection(modal.Text(styles.Muted.Render(" ")))
	}

	// Search results dropdown — viewport window over all results
	const maxVisible = 10
	const minResultLines = 5
	if len(m.issueSearchResults) > 0 {
		searchResults := m.issueSearchResults
		searchCursor := m.issueSearchCursor
		searchScrollOffset := m.issueSearchScrollOffset
		b = b.AddSection(modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
			var sb strings.Builder
			total := len(searchResults)
			endIdx := searchScrollOffset + maxVisible
			if endIdx > total {
				endIdx = total
			}
			visibleCount := endIdx - searchScrollOffset
			focusables := make([]modal.FocusableInfo, 0, visibleCount)
			for i := searchScrollOffset; i < endIdx; i++ {
				r := searchResults[i]
				tag := formatSearchStatusTag(r.Status)
				icon := formatSearchTypeIcon(r.Type)
				pri := formatSearchPriority(r.Priority)
				idStr := styles.Muted.Render(r.ID)
				prefix := fmt.Sprintf(" %s %s %s %s ", tag, icon, idStr, pri)
				title := r.Title
				titleWidth := contentWidth - lipgloss.Width(prefix)
				if titleWidth < 10 {
					titleWidth = 10
				}
				if len(title) > titleWidth {
					title = title[:titleWidth-3] + "..."
				}
				line := prefix + title
				itemID := fmt.Sprintf("%s%d", issueSearchResultPrefix, i)
				isHovered := itemID == hoverID
				if i == searchCursor || isHovered {
					sb.WriteString(styles.ListItemSelected.Render(line))
				} else {
					sb.WriteString(styles.ListItemNormal.Render(line))
				}
				if i < endIdx-1 {
					sb.WriteString("\n")
				}
				focusables = append(focusables, modal.FocusableInfo{
					ID:      itemID,
					OffsetX: 0,
					OffsetY: i - searchScrollOffset,
					Width:   contentWidth,
					Height:  1,
				})
			}
			// Pad with empty lines to maintain minimum height
			for i := visibleCount; i < minResultLines; i++ {
				sb.WriteString("\n")
			}
			return modal.RenderedSection{Content: sb.String(), Focusables: focusables}
		}, nil))
	} else {
		// Reserve space for results even when empty
		b = b.AddSection(modal.Custom(func(contentWidth int, _, _ string) modal.RenderedSection {
			var sb strings.Builder
			for i := 0; i < minResultLines; i++ {
				if i > 0 {
					sb.WriteString("\n")
				}
			}
			return modal.RenderedSection{Content: sb.String()}
		}, nil))
	}

	if hasResults {
		b = b.AddSection(modal.Spacer())
		b = b.AddSection(modal.Buttons(
			modal.Btn(" Open ", "open", modal.BtnPrimary()),
			modal.Btn(" Cancel ", "cancel"),
		))
	}

	m.issueInputModal = b
}

func (m *Model) renderIssuePreviewOverlay(content string) string {
	m.ensureIssuePreviewModal()
	if m.issuePreviewModal == nil {
		return content
	}
	if m.issuePreviewMouseHandler == nil {
		m.issuePreviewMouseHandler = mouse.NewHandler()
	}
	rendered := m.issuePreviewModal.Render(m.width, m.height, m.issuePreviewMouseHandler)
	return ui.OverlayModal(content, rendered, m.width, m.height)
}

func (m *Model) ensureIssuePreviewModal() {
	// Use 80% of terminal width so the issue is comfortable to read
	modalW := m.width * 4 / 5
	if modalW > m.width-4 {
		modalW = m.width - 4
	}
	if modalW < 30 {
		modalW = 30
	}

	// Cache check -- also invalidate when data/error/loading changes
	cacheKey := modalW
	if m.issuePreviewModal != nil && m.issuePreviewModalWidth == cacheKey {
		return
	}
	m.issuePreviewModalWidth = cacheKey

	if m.issuePreviewLoading {
		m.issuePreviewModal = modal.New("Loading...",
			modal.WithWidth(modalW),
			modal.WithHints(false),
		).
			AddSection(modal.Text("Fetching issue data..."))
		return
	}

	if m.issuePreviewError != nil {
		m.issuePreviewModal = modal.New("Error",
			modal.WithWidth(modalW),
			modal.WithVariant(modal.VariantDanger),
			modal.WithHints(false),
		).
			AddSection(modal.Text(m.issuePreviewError.Error())).
			AddSection(modal.Spacer()).
			AddSection(modal.Buttons(
				modal.Btn(" Close ", "cancel"),
			))
		return
	}

	if m.issuePreviewData == nil {
		m.issuePreviewModal = nil
		return
	}

	data := m.issuePreviewData

	// Build title
	title := data.ID
	if data.Title != "" {
		title += ": " + data.Title
	}

	// Build status line
	var metaParts []string
	if data.Status != "" {
		metaParts = append(metaParts, "["+data.Status+"]")
	}
	if data.Type != "" {
		metaParts = append(metaParts, data.Type)
	}
	if data.Priority != "" {
		metaParts = append(metaParts, data.Priority)
	}
	if data.Points > 0 {
		metaParts = append(metaParts, fmt.Sprintf("%dp", data.Points))
	}
	statusLine := strings.Join(metaParts, "  ")

	// Build fixed footer hint string
	var hintBuf strings.Builder
	hintBuf.WriteString(styles.KeyHint.Render("j/k"))
	hintBuf.WriteString(styles.Muted.Render(" scroll  "))
	hintBuf.WriteString(styles.KeyHint.Render("o"))
	hintBuf.WriteString(styles.Muted.Render(" open  "))
	hintBuf.WriteString(styles.KeyHint.Render("b"))
	hintBuf.WriteString(styles.Muted.Render(" back  "))
	hintBuf.WriteString(styles.KeyHint.Render("y"))
	hintBuf.WriteString(styles.Muted.Render(" yank  "))
	hintBuf.WriteString(styles.KeyHint.Render("Y"))
	hintBuf.WriteString(styles.Muted.Render(" yank key  "))
	hintBuf.WriteString(styles.KeyHint.Render("esc"))
	hintBuf.WriteString(styles.Muted.Render(" close"))

	// Build modal
	b := modal.New(title,
		modal.WithWidth(modalW),
		modal.WithHints(false),
		modal.WithCustomFooter(hintBuf.String()),
	)

	if statusLine != "" {
		b = b.AddSection(modal.Text(statusLine))
	}

	if data.ParentID != "" {
		b = b.AddSection(modal.Text("Parent: " + data.ParentID))
	}

	if len(data.Labels) > 0 {
		b = b.AddSection(modal.Text("Labels: " + strings.Join(data.Labels, ", ")))
	}

	// Description — render as markdown, let modal scroll handle overflow
	if data.Description != "" {
		b = b.AddSection(modal.Spacer())
		desc := data.Description
		if renderer, err := markdown.NewRenderer(); err == nil {
			rendered := renderer.RenderContent(desc, modalW-modal.ModalPadding)
			desc = strings.Join(rendered, "\n")
		}
		b = b.AddSection(modal.Text(desc))
	}

	b = b.AddSection(modal.Spacer())
	b = b.AddSection(modal.Buttons(
		modal.Btn(" Open in TD ", "open-in-td", modal.BtnPrimary()),
		modal.Btn(" Back ", "back"),
		modal.Btn(" Close ", "cancel"),
	))

	m.issuePreviewModal = b
}
