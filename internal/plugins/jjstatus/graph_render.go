package jjstatus

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/styles"
	jjvcs "github.com/marcus/sidecar/internal/vcs/jj"
)

var (
	nodeWorkingCopy = lipgloss.NewStyle().Foreground(styles.Success).Bold(true)
	nodeImmutable   = lipgloss.NewStyle().Foreground(styles.Info).Bold(true)
	nodeConflict    = lipgloss.NewStyle().Foreground(styles.Error).Bold(true)
	nodeNormal      = lipgloss.NewStyle().Foreground(styles.TextSecondary)
	graphLine       = lipgloss.NewStyle().Foreground(styles.TextSubtle)
	changeIDPrefix  = lipgloss.NewStyle().Foreground(styles.Secondary).Bold(true)
	changeIDSuffix  = lipgloss.NewStyle().Foreground(styles.TextSubtle)
	timestampStyle  = lipgloss.NewStyle().Foreground(styles.TextMuted)
	emptyBadge      = lipgloss.NewStyle().Foreground(styles.TextMuted).Italic(true)
	bookmarkStyle   = lipgloss.NewStyle().Foreground(styles.TextPrimary).Background(styles.BgTertiary).Padding(0, 1)
)

// nodeGlyph returns the styled glyph for a commit.
func nodeGlyph(c *jjvcs.StructuredCommit) string {
	switch {
	case c.IsConflict:
		return nodeConflict.Render("⊗")
	case c.IsWorkingCopy:
		return nodeWorkingCopy.Render("⬤")
	case c.IsImmutable:
		return nodeImmutable.Render("◆")
	default:
		return nodeNormal.Render("◯")
	}
}

// renderGraphPrefix renders the graph tree prefix (lanes + node/connector) for a row.
// maxLanes is the maximum number of lanes across all rows (for consistent width).
func renderGraphPrefix(row jjvcs.GraphRow, maxLanes int) string {
	if maxLanes < 1 {
		maxLanes = 1
	}
	var sb strings.Builder
	col := row.Column

	if row.Commit != nil {
		// Commit row: render lanes with node at col position.
		for i := 0; i < maxLanes; i++ {
			if i == col {
				sb.WriteString(nodeGlyph(row.Commit))
			} else if i < len(row.Lanes) && row.Lanes[i] != "" {
				sb.WriteString(graphLine.Render("║"))
			} else {
				sb.WriteString(" ")
			}
			if i < maxLanes-1 {
				sb.WriteString(" ")
			}
		}
	} else {
		// Connector row.
		for i := 0; i < maxLanes; i++ {
			if i < len(row.Lanes) && row.Lanes[i] != "" {
				sb.WriteString(graphLine.Render("║"))
			} else {
				sb.WriteString(" ")
			}
			if i < maxLanes-1 {
				sb.WriteString(" ")
			}
		}
	}

	return sb.String()
}

// renderGraphContinuation renders the continuation prefix for the second line of a commit.
// Shows ║ in all active lanes including the commit's own lane.
func renderGraphContinuation(row jjvcs.GraphRow, maxLanes int, activeLanes []string) string {
	if maxLanes < 1 {
		maxLanes = 1
	}
	var sb strings.Builder
	for i := 0; i < maxLanes; i++ {
		if i < len(activeLanes) && activeLanes[i] != "" {
			sb.WriteString(graphLine.Render("║"))
		} else {
			sb.WriteString(" ")
		}
		if i < maxLanes-1 {
			sb.WriteString(" ")
		}
	}
	return sb.String()
}

// shortAuthor extracts the username part of an email address.
func shortAuthor(email string) string {
	if idx := strings.Index(email, "@"); idx > 0 {
		return email[:idx]
	}
	return email
}

// renderCommitHeaderLine renders the first line of a commit row:
// changeID + author + bookmarks + timestamp.
func renderCommitHeaderLine(c *jjvcs.StructuredCommit, maxWidth int) string {
	var parts []string

	// Change ID with highlighted unique prefix.
	if c.ChangeIDShort != "" && len(c.ChangeIDShort) < len(c.ChangeID) {
		prefixLen := len(c.ChangeIDShort)
		parts = append(parts, changeIDPrefix.Render(c.ChangeID[:prefixLen])+changeIDSuffix.Render(c.ChangeID[prefixLen:]))
	} else {
		parts = append(parts, changeIDPrefix.Render(c.ChangeID))
	}

	// Short author name.
	if c.Author != "" {
		parts = append(parts, timestampStyle.Render(shortAuthor(c.Author)))
	}

	// Bookmarks as styled badges.
	if c.Bookmarks != "" {
		for _, bm := range strings.Fields(c.Bookmarks) {
			parts = append(parts, bookmarkStyle.Render(bm))
		}
	}

	// Empty badge.
	if c.IsEmpty {
		parts = append(parts, emptyBadge.Render("(empty)"))
	}

	// Timestamp.
	if c.Timestamp != "" {
		parts = append(parts, timestampStyle.Render(c.Timestamp))
	}

	return strings.Join(parts, " ")
}

// wrapText word-wraps plain text to fit within maxWidth, returning one or more lines.
func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 || ansi.StringWidth(text) <= maxWidth {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := words[0]
	for _, w := range words[1:] {
		if ansi.StringWidth(cur)+1+ansi.StringWidth(w) <= maxWidth {
			cur += " " + w
		} else {
			lines = append(lines, cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// renderCommitDescLines renders the description as one or more lines, wrapping to maxWidth.
func renderCommitDescLines(c *jjvcs.StructuredCommit, maxWidth int) []string {
	desc := c.Description
	if desc == "" {
		desc = "(no description set)"
	}
	muted := c.IsEmpty || desc == "(no description set)"
	wrapped := wrapText(desc, maxWidth)
	styled := make([]string, len(wrapped))
	for i, line := range wrapped {
		if muted {
			styled[i] = styles.Muted.Render(line)
		} else {
			styled[i] = styles.ListItemNormal.Render(line)
		}
	}
	return styled
}

// renderCommitDescLine renders the second line of a commit row: the description (single line, for tests).
func renderCommitDescLine(c *jjvcs.StructuredCommit, maxWidth int) string {
	desc := c.Description
	if desc == "" {
		desc = "(no description set)"
	}
	if c.IsEmpty || desc == "(no description set)" {
		return styles.Muted.Render(desc)
	}
	return styles.ListItemNormal.Render(desc)
}

// renderCommitMeta renders commit metadata on a single line (used by tests).
func renderCommitMeta(c *jjvcs.StructuredCommit, maxWidth int) string {
	return renderCommitHeaderLine(c, maxWidth) + " " + renderCommitDescLine(c, maxWidth)
}

// commitVisualLines returns the number of visual lines a commit row will produce
// (1 header + N description lines) given the available text width for wrapping.
func commitVisualLines(c *jjvcs.StructuredCommit, textWidth int) int {
	desc := c.Description
	if desc == "" {
		desc = "(no description set)"
	}
	return 1 + len(wrapText(desc, textWidth))
}

// graphVisualLineCount returns the total visual lines and per-row line counts
// for the graph rows at the given render width and maxLanes.
func graphVisualLineCount(rows []jjvcs.GraphRow, width, maxLanes int) (total int, perRow []int) {
	perRow = make([]int, len(rows))
	for i, r := range rows {
		if r.Commit != nil {
			prefixW := maxLanes*2 + 1 // lane glyphs + spaces + 1 trailing space
			textW := width - prefixW - 2
			if textW < 10 {
				textW = 10
			}
			n := commitVisualLines(r.Commit, textW)
			perRow[i] = n
			total += n
		} else {
			perRow[i] = 1
			total++
		}
	}
	return
}

func padVisualRight(s string, width int) string {
	w := ansi.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// renderCustomLogList renders the full custom graph view from layout rows.
// Each commit renders as two lines: header (changeID, bookmarks, timestamp)
// and description (under a ║ continuation line).
// selectedCommit is the index into the original commits slice (not rows).
func renderCustomLogList(rows []jjvcs.GraphRow, width, height, scroll, selectedCommit int, focused bool) string {
	if len(rows) == 0 {
		return styles.Muted.Render("No commits")
	}

	// Find max lanes across all rows for consistent prefix width.
	maxLanes := 0
	for _, r := range rows {
		if len(r.Lanes) > maxLanes {
			maxLanes = len(r.Lanes)
		}
		if r.Column+1 > maxLanes {
			maxLanes = r.Column + 1
		}
	}

	// Map commit index to the row index of that commit for selection.
	commitIdx := 0
	selectedRowIdx := -1
	for i, r := range rows {
		if r.Commit != nil {
			if commitIdx == selectedCommit {
				selectedRowIdx = i
			}
			commitIdx++
		}
	}

	// Pre-render all visual lines. Each commit row produces two visual lines;
	// connector rows produce one.
	type visualLine struct {
		text      string
		rowIdx    int
		isCommit  bool
	}
	var allLines []visualLine

	for i, row := range rows {
		prefix := renderGraphPrefix(row, maxLanes)

		if row.Commit != nil {
			// Prefix width in visual columns: each lane = 1 glyph + 1 space, plus " " after prefix.
			prefixVisWidth := ansi.StringWidth(ansi.Strip(prefix)) + 1 // +1 for the space after prefix
			metaWidth := width - prefixVisWidth - 2                    // -2 for continuation indent
			if metaWidth < 10 {
				metaWidth = 10
			}
			headerLine := prefix + " " + renderCommitHeaderLine(row.Commit, metaWidth)

			// Build continuation lanes: the commit's own lane is active after it.
			contLanes := make([]string, len(row.Lanes))
			copy(contLanes, row.Lanes)
			if row.Column < len(contLanes) {
				if len(row.Commit.Parents) > 0 {
					contLanes[row.Column] = row.Commit.Parents[0]
				} else {
					contLanes[row.Column] = ""
				}
			}
			contPrefix := renderGraphContinuation(row, maxLanes, contLanes)

			// Wrap description text to fit available width.
			descLines := renderCommitDescLines(row.Commit, metaWidth)
			var commitLines []string
			commitLines = append(commitLines, headerLine)
			for _, dl := range descLines {
				commitLines = append(commitLines, contPrefix+"  "+dl)
			}

			if i == selectedRowIdx {
				for j, cl := range commitLines {
					stripped := ansi.Strip(cl)
					padded := padVisualRight(stripped, width)
					if focused {
						if j == 0 {
							commitLines[j] = styles.ListItemFocused.Copy().Bold(true).Render(padded)
						} else {
							commitLines[j] = styles.ListItemFocused.Copy().Render(padded)
						}
					} else {
						if j == 0 {
							commitLines[j] = styles.ListItemSelected.Copy().Bold(true).Render(padded)
						} else {
							commitLines[j] = styles.ListItemSelected.Copy().Render(padded)
						}
					}
				}
			}

			for _, cl := range commitLines {
				allLines = append(allLines, visualLine{cl, i, true})
			}
		} else {
			allLines = append(allLines, visualLine{prefix, i, false})
		}
	}

	// Apply scroll and height limits to visual lines.
	end := scroll + height
	if end > len(allLines) {
		end = len(allLines)
	}
	if scroll > len(allLines) {
		scroll = len(allLines)
	}
	var out []string
	for i := scroll; i < end; i++ {
		out = append(out, allLines[i].text)
	}
	return strings.Join(out, "\n")
}
