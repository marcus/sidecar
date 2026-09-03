package filebrowser

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/filefind"
	"github.com/marcus/sidecar/internal/image"
	"github.com/marcus/sidecar/internal/queryfield"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
)

// FocusPane represents which pane is active.
type FocusPane int

const (
	PaneTree FocusPane = iota
	PanePreview
)

// dividerWidth is the width of the draggable divider between panes.
const dividerWidth = 1

// calculatePaneWidths sets the tree and preview pane widths.
// If treeWidth is already set (from drag), only updates previewWidth.
func (p *Plugin) calculatePaneWidths() {
	// RenderPanel handles borders internally, so only subtract divider
	available := p.width - dividerWidth

	// Only set default treeWidth if not yet initialized
	if p.treeWidth == 0 {
		p.treeWidth = available * 30 / 100
	}

	// Clamp treeWidth to valid bounds
	minWidth := 20
	maxWidth := available - 40 // Leave at least 40 for preview
	if maxWidth < minWidth {
		maxWidth = minWidth
	}
	if p.treeWidth < minWidth {
		p.treeWidth = minWidth
	} else if p.treeWidth > maxWidth {
		p.treeWidth = maxWidth
	}

	// Calculate previewWidth from remaining space
	p.previewWidth = available - p.treeWidth
	if p.previewWidth < 40 {
		p.previewWidth = 40
	}
}

// renderView creates the 2-pane layout.
func (p *Plugin) renderView() string {
	// A bound surface paints its reason rather than a tree only when it has
	// one. "Not connected", "too old for the tree contract", and "no bound
	// worktree" are three different things for the user to do next, so they
	// are three different sentences.
	if reason := p.unavailableReason(); reason != "" {
		return lipgloss.NewStyle().Width(p.width).Height(p.height).MaxHeight(p.height).Render(
			styles.Title.Render(pluginName) + "\n\n" + styles.Muted.Render(fmt.Sprintf("%s is unavailable: %s", pluginName, reason)),
		)
	}
	if p.remoteBound() && p.tree == nil {
		return lipgloss.NewStyle().Width(p.width).Height(p.height).MaxHeight(p.height).Render(
			styles.Title.Render(pluginName) + "\n\n" + styles.Muted.Render(fmt.Sprintf("Loading [%s]…", p.ctx.HostID)),
		)
	}
	// Clear mouse hit regions at start of each render
	p.mouseHandler.Clear()

	// NOTE: Inline edit mode is handled within renderPreviewPane(), not here.
	// This allows the tree pane to remain visible during editing.

	// The two search surfaces get one placement, not two. Both act on the whole
	// project rather than on the preview, so both are centred over the whole
	// plugin area with both panes dimmed behind them; the only thing that
	// differs is how wide each box likes to be, which is a property of its rows
	// (paths versus source lines) rather than of where it lives. Anything else
	// reads as two components rather than one component in two modes.
	//
	// This is deliberately not the placement a workspace document pane uses,
	// and the difference is scope rather than style. There, the surface belongs
	// to one pane and is rooted at that pane's directory — two panes can have
	// two different roots on screen at once — so scoping the box to the pane is
	// what says which root it is searching. Here there is one root for the whole
	// plugin, and scoping the box to the preview pane would claim a preview
	// scope the surface does not have. What makes them read as siblings is the
	// box itself: same border, same rows, same elision, same counts row naming
	// the root, whichever host drew it.
	if p.projectSearchMode {
		background := p.renderNormalPanes()
		modal := p.renderProjectSearchModalContent()
		return ui.OverlayModal(background, modal, p.width, p.height)
	}

	if p.quickOpenMode {
		background := p.renderNormalPanes()
		modal := p.renderQuickOpenModalContent()
		return ui.OverlayModal(background, modal, p.width, p.height)
	}

	// Info modal is a full overlay - render modal over dimmed background
	if p.infoMode {
		background := p.renderNormalPanes()
		modal := p.renderInfoModalContent()
		return ui.OverlayModal(background, modal, p.width, p.height)
	}

	// Blame view is a full overlay - render modal over dimmed background
	if p.blameMode {
		background := p.renderNormalPanes()
		modal := p.renderBlameModalContent()
		return ui.OverlayModal(background, modal, p.width, p.height)
	}

	return p.renderNormalPanes()
}

// inputBarHeight returns the number of rows the input bars above the panes
// occupy (content search, file op, line jump). Mouse code needs the same number
// to know where the panes start, so both read it from here.
// Note: the tree search bar is rendered inside the tree pane, not here.
func (p *Plugin) inputBarHeight() int {
	h := 0
	if p.contentSearchMode {
		h += contentSearchBarRows
	}
	if p.fileOpMode != FileOpNone {
		h += inputBarRows
		// One extra line for the file-op error message when present.
		if p.fileOpError != "" {
			h++
		}
		// The move dialog's suggestion dropdown renders below the bar and pushes
		// the panes down with it. Leaving it out shifted every tree hit region
		// up by one row per suggestion, so a drop released on the row the user
		// was looking at moved the file into a different directory entirely.
		h += p.fileOpSuggestionRows()
	}
	if p.lineJumpMode {
		h += inputBarRows
	}
	return h
}

// fileOpSuggestionRows is the number of screen rows the move dialog's path
// suggestion dropdown occupies (0 when it is not showing).
func (p *Plugin) fileOpSuggestionRows() int {
	if p.fileOpMode != FileOpMove || !p.fileOpShowSuggestions {
		return 0
	}
	return len(p.fileOpSuggestions)
}

// fileOpSuggestionsTopY is the screen row the first suggestion is drawn on.
// The dropdown sits underneath everything the bars above it draw, including the
// file-op bar's own input line, its margin row and its optional error line.
func (p *Plugin) fileOpSuggestionsTopY() int {
	y := 0
	if p.contentSearchMode {
		y += contentSearchBarRows
	}
	y += inputBarRows
	if p.fileOpError != "" {
		y++
	}
	return y
}

// inputBarRows is the height of one input bar on screen. The bars render with
// styles.ModalTitle, which carries MarginBottom(1), so each one occupies its
// text line plus a blank line. Counting it as a single row put every tree hit
// region one row above the row it names whenever a bar was open - a click
// selected the wrong file, and a drop moved one into the wrong directory.
const inputBarRows = 2

// contentSearchBarRows is the height of the content search bar, which is one
// row and not two: it draws through queryfield.RenderRow rather than
// styles.ModalTitle, so it carries no bottom margin. It is counted separately
// from inputBarRows for exactly the reason that constant exists — a bar
// measured at the wrong height puts every tree hit region on the wrong row.
const contentSearchBarRows = 1

// paneHeight returns the outer height of the tree/preview panels, borders
// included. renderNormalPanes and the geometry helpers below must agree on it,
// so it lives in one place.
func (p *Plugin) paneHeight() int {
	h := p.height - p.inputBarHeight()
	if h < 4 {
		h = 4
	}
	return h
}

// treeItemRows returns how many tree rows the pane actually draws: the panel
// height minus its two borders and the two header lines renderTreePane emits
// before the first item.
//
// Everything that maps rows to screen coordinates reads this: the render loop,
// the hit-region loop, the scroll clamp and the drag auto-scroll edges. When
// they disagree the tree registers rows it never draws, and a drop released on
// the pane border commits a move into a directory the user never saw.
func (p *Plugin) treeItemRows() int {
	rows := p.paneHeight() - 4
	if rows < 1 {
		rows = 1
	}
	return rows
}

// treeRowVisible reports whether a flat tree index is currently drawn on screen.
func (p *Plugin) treeRowVisible(idx int) bool {
	return idx >= p.treeScrollOff && idx < p.treeScrollOff+p.treeItemRows()
}

// treeRowsViewport returns the screen row of the first tree item and how many
// item rows are visible.
func (p *Plugin) treeRowsViewport() (topY, height int) {
	// Pane border (1) + header (2) sit above the first item, matching the hit
	// regions registered in renderNormalPanes.
	return p.inputBarHeight() + 3, p.treeItemRows()
}

// clampTreeScroll keeps the tree scroll offset inside the scrollable range.
func (p *Plugin) clampTreeScroll() {
	maxOff := 0
	if p.tree != nil {
		maxOff = p.tree.Len() - p.treeItemRows()
	}
	if maxOff < 0 {
		maxOff = 0
	}
	if p.treeScrollOff > maxOff {
		p.treeScrollOff = maxOff
	}
	if p.treeScrollOff < 0 {
		p.treeScrollOff = 0
	}
}

// renderNormalPanes renders the standard 2-pane layout without modals.
func (p *Plugin) renderNormalPanes() string {
	// The scroll offset can go stale between renders: state restored from a
	// session with a different pane height or tree, a rebuild that shrank the
	// flat list, a collapse that removed rows. Clamping here — before both the
	// render loop and the hit-region loop read the offset — guarantees that a
	// tree which fits the viewport draws in full from the top, and that the
	// rows we register for clicks are exactly the rows we draw.
	p.clampTreeScroll()

	inputBarHeight := p.inputBarHeight()

	// Pane height for panels (outer dimensions including borders)
	// Note: footer is rendered by the app, not by the plugin
	paneHeight := p.paneHeight()

	// Inner content height (excluding borders and header lines)
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Tree rows the pane can actually draw. The tree pane spends two of its
	// inner lines on the header, so it shows fewer rows than the preview pane
	// has lines - and the hit regions below must match what is drawn, not the
	// pane's inner height.
	treeRows := p.treeItemRows()

	// Handle collapsed tree - render full-width preview pane
	if !p.treeVisible {
		previewWidth := p.width - 2 // Account for borders
		if previewWidth < 40 {
			previewWidth = 40
		}
		p.previewWidth = previewWidth

		previewContent := p.renderPreviewPane(innerHeight)
		rightPane := styles.RenderPanel(previewContent, previewWidth, paneHeight, p.innerPaneFocusActive())

		// Build final layout
		var parts []string

		// Add content search bar if in content search mode
		if p.contentSearchMode {
			parts = append(parts, p.renderContentSearchBar())
		}

		// Add file operation bar if in file operation mode
		if p.fileOpMode != FileOpNone {
			parts = append(parts, p.renderFileOpBar())
		}

		// Add line jump bar if in line jump mode
		if p.lineJumpMode {
			parts = append(parts, p.renderLineJumpBar())
		}

		parts = append(parts, rightPane)

		// Update hit regions for collapsed state
		p.mouseHandler.Clear()
		p.mouseHandler.HitMap.AddRect(regionPreviewPane, 0, inputBarHeight, previewWidth, paneHeight, nil)
		if len(p.tabHits) > 0 {
			tabY := inputBarHeight + 1
			tabX := 2 // left border + padding
			p.registerPreviewTabHits(tabX, tabY)
		}
		p.registerPreviewSelectionRegions()
		// Scrollbar column last: above the pane region it overlaps.
		p.registerScrollbarRegions()

		return lipgloss.JoinVertical(lipgloss.Left, parts...)
	}

	p.calculatePaneWidths()

	// Determine if panes are active based on focus
	// Content search mode focuses the preview pane since we're searching file content
	innerFocusActive := p.innerPaneFocusActive()
	treeActive := innerFocusActive && p.activePane == PaneTree && !p.searchMode && !p.contentSearchMode
	previewActive := innerFocusActive && (p.activePane == PanePreview && !p.searchMode || p.contentSearchMode)

	treeContent := p.renderTreePane(treeRows)
	previewContent := p.renderPreviewPane(innerHeight)

	// Apply gradient border styles
	leftPane := styles.RenderPanel(treeContent, p.treeWidth, paneHeight, treeActive)

	// Use interactive gradient when in inline edit mode
	var rightPane string
	if p.edit.Active && p.edit.Model != nil && p.edit.Model.IsActive() {
		rightPane = styles.RenderPanelWithGradient(previewContent, p.previewWidth, paneHeight, styles.GetInteractiveGradient())
	} else {
		rightPane = styles.RenderPanel(previewContent, p.previewWidth, paneHeight, previewActive)
	}

	dragging := p.mouseHandler != nil && p.mouseHandler.IsDragging() && p.mouseHandler.DragRegion() == regionPaneDivider
	divider := ui.RenderHandle(paneHeight, true, ui.HandleStateFrom(p.hoverDivider, dragging))

	// Join panes horizontally with divider in between
	panes := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, divider, rightPane)

	// Build final layout
	var parts []string

	// Note: tree search bar is rendered inside renderTreePane(), not here

	// Add content search bar if in content search mode
	if p.contentSearchMode {
		parts = append(parts, p.renderContentSearchBar())
	}

	// Add file operation bar if in file operation mode
	if p.fileOpMode != FileOpNone {
		parts = append(parts, p.renderFileOpBar())
	}

	// Add line jump bar if in line jump mode
	if p.lineJumpMode {
		parts = append(parts, p.renderLineJumpBar())
	}

	parts = append(parts, panes)

	// Register mouse hit regions for panes
	// Panes start after any input bars
	paneY := inputBarHeight
	treeItemY := paneY + 3 // border(1) + header(2)

	// Register pane regions - tested in reverse order (last added = highest priority)
	// Tree pane region (x=0, full width) - lowest priority fallback
	p.mouseHandler.HitMap.AddRect(regionTreePane, 0, paneY, p.treeWidth, paneHeight, nil)

	// Preview pane region (after divider) - medium priority
	previewX := p.treeWidth + dividerWidth
	p.mouseHandler.HitMap.AddRect(regionPreviewPane, previewX, paneY, p.previewWidth, paneHeight, nil)

	// Pane divider region - HIGH PRIORITY (registered after panes so it wins in overlap)
	// Left pane is Width(treeWidth), so occupies columns 0 to treeWidth-1
	// Divider is at column treeWidth
	// Hit region is wider for easier clicking
	dividerX := p.treeWidth
	dividerHitWidth := 3
	p.mouseHandler.HitMap.AddRect(regionPaneDivider, dividerX, paneY, dividerHitWidth, paneHeight, nil)

	// Register individual tree items LAST (tested first = higher priority)
	// Note: regions are tested in reverse order, so items added last take precedence
	if p.tree != nil && p.tree.Len() > 0 {
		end := p.treeScrollOff + treeRows
		if end > p.tree.Len() {
			end = p.tree.Len()
		}
		for i := p.treeScrollOff; i < end; i++ {
			itemY := treeItemY + (i - p.treeScrollOff)
			// Register region: x=1 (inside border), width=treeWidth-3 (exclude scrollbar), height=1, data=tree index
			p.mouseHandler.HitMap.AddRect(regionTreeItem, 1, itemY, p.treeWidth-3, 1, i)
		}
	}

	// Register exactly the visual rows the preview drew. Rendered Markdown can
	// have a different row count from its source, and wrapped rows can occupy
	// several screen lines; previewTextRect is already the geometry authority
	// for content-link scanning, so selection uses the same answer.
	p.registerPreviewSelectionRegions()

	// Register preview tabs (first content row)
	if len(p.tabHits) > 0 {
		tabY := paneY + 1
		tabX := previewX + 2 // left border + padding
		p.registerPreviewTabHits(tabX, tabY)
	}

	// Scrollbar columns LAST (tested first = highest priority): the tree item
	// and preview line rects above reach into the bar's column, and a press on
	// the bar must never select a row or grab a text selection underneath it.
	p.registerScrollbarRegions()

	return lipgloss.JoinVertical(lipgloss.Top, parts...)
}

func (p *Plugin) registerPreviewSelectionRegions() {
	if p.previewFile == "" || p.isBinary || p.isImage || p.previewError != nil || len(p.previewDisplayLines()) == 0 {
		return
	}
	rect := p.previewTextRect()
	for row := 0; row < rect.H; row++ {
		p.mouseHandler.HitMap.AddRect(regionPreviewLine, rect.X, rect.Y+row, rect.W, 1, row)
	}
}

// renderContentSearchBar renders the content search input bar for preview pane.
//
// It draws through queryfield.RenderRow, so the preview's search bar is the
// app's query bar; the match count and the navigation hint are this surface's
// own right cell. The × is not drawn: this row sits above the panes and the
// plugin registers no region for it, and a control nothing listens to is worse
// than no control.
func (p *Plugin) renderContentSearchBar() string {
	matchInfo := ""
	if len(p.contentSearchMatches) > 0 {
		matchInfo = fmt.Sprintf("(%d/%d)", p.contentSearchCursor+1, len(p.contentSearchMatches))
		if p.contentSearchCommitted {
			matchInfo += " [n/N j/k]" // Hint for navigation
		}
	} else if p.contentSearchQuery() != "" {
		matchInfo = "(0 matches)"
	}
	if matchInfo != "" {
		matchInfo = styles.Muted.Render(matchInfo)
	}
	row, _ := queryfield.RenderRow(p.width, queryfield.Row{
		Query:       p.contentSearchQuery(),
		Cursor:      p.contentSearchField.Cursor(),
		Focused:     !p.contentSearchCommitted,
		Placeholder: "search…",
		Right:       matchInfo,
	})
	return row
}

// renderTreeSearchBar renders the tree search bar inline within the tree pane.
func (p *Plugin) renderTreeSearchBar() string {
	matchInfo := ""
	if len(p.searchMatches) > 0 {
		matchInfo = fmt.Sprintf("(%d/%d)", p.searchCursor+1, len(p.searchMatches))
	} else if p.searchQuery() != "" {
		matchInfo = "(no matches)"
	}
	if matchInfo != "" {
		matchInfo = styles.Muted.Render(matchInfo)
	}
	row, _ := queryfield.RenderRow(p.treeSearchBarWidth(), queryfield.Row{
		Query:       p.searchQuery(),
		Cursor:      p.searchField.Cursor(),
		Focused:     p.searchMode,
		Placeholder: "filter…",
		Right:       matchInfo,
	})
	return row
}

// treeSearchBarWidth is the row the tree pane has room for: the pane's width
// less its two border columns.
func (p *Plugin) treeSearchBarWidth() int {
	return max(p.treeWidth-2, 1)
}

// renderFileOpBar renders the file operation input bar (move/rename/create/delete).
func (p *Plugin) renderFileOpBar() string {
	// Handle delete confirmation mode
	if p.fileOpConfirmDelete && p.fileOpTarget != nil {
		itemType := "file"
		if p.fileOpTarget.IsDir {
			itemType = "directory"
		}
		return p.renderFileOpConfirmation(fmt.Sprintf("Delete %s '%s'?", itemType, p.fileOpTarget.Name))
	}

	// Handle confirmation mode for directory creation (during move)
	if p.fileOpConfirmCreate {
		return p.renderFileOpConfirmation(fmt.Sprintf("Create '%s'?", p.fileOpConfirmPath))
	}

	var prompt string
	switch p.fileOpMode {
	case FileOpRename:
		prompt = "Rename: "
	case FileOpMove:
		prompt = "Move to: "
	case FileOpCreateFile:
		prompt = "New file: "
	case FileOpCreateDir:
		prompt = "New dir: "
	default:
		return ""
	}

	inputLine := fmt.Sprintf(" %s%s", prompt, p.fileOpTextInput.View())

	var lines []string
	lines = append(lines, styles.ModalTitle.Render(inputLine))

	if p.fileOpError != "" {
		lines = append(lines, styles.StatusDeleted.Render(" "+p.fileOpError))
	}

	// Show suggestion dropdown for move mode
	if p.fileOpMode == FileOpMove && p.fileOpShowSuggestions && len(p.fileOpSuggestions) > 0 {
		lines = append(lines, p.renderFileOpSuggestions())
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderFileOpConfirmation renders a confirmation dialog with Yes/No buttons.
func (p *Plugin) renderFileOpConfirmation(message string) string {
	// Clear existing hit regions for buttons
	p.mouseHandler.HitMap.Clear()

	var sb strings.Builder
	sb.WriteString(" ")
	sb.WriteString(message)
	sb.WriteString("  ")

	// Calculate button positions (approximate, based on rendered text)
	// We use tree pane width as reference since bar is rendered in tree pane
	baseX := 1 + len(message) + 2 // space + message + spacing

	// Yes button
	yesBtn := "Yes"
	if p.fileOpButtonHover == 1 {
		sb.WriteString(styles.ButtonHover.Render(yesBtn))
	} else if p.fileOpButtonFocus == 1 {
		sb.WriteString(styles.ButtonFocused.Render(yesBtn))
	} else {
		sb.WriteString(styles.Button.Render(yesBtn))
	}
	yesWidth := len(yesBtn) + 4 // Padding adds 2 on each side
	p.mouseHandler.HitMap.AddRect(regionFileOpConfirm, baseX, 0, yesWidth, 1, nil)

	sb.WriteString(" ")
	baseX += yesWidth + 1

	// No button
	noBtn := "No"
	if p.fileOpButtonHover == 2 {
		sb.WriteString(styles.ButtonHover.Render(noBtn))
	} else if p.fileOpButtonFocus == 2 {
		sb.WriteString(styles.ButtonFocused.Render(noBtn))
	} else {
		sb.WriteString(styles.Button.Render(noBtn))
	}
	noWidth := len(noBtn) + 4
	p.mouseHandler.HitMap.AddRect(regionFileOpCancel, baseX, 0, noWidth, 1, nil)

	return styles.ModalTitle.Render(sb.String())
}

// renderFileOpSuggestions renders the path suggestion dropdown.
func (p *Plugin) renderFileOpSuggestions() string {
	var lines []string

	topY := p.fileOpSuggestionsTopY()
	for i, suggestion := range p.fileOpSuggestions {
		line := " " + suggestion
		if i == p.fileOpSuggestionIdx {
			line = styles.ListItemSelected.Render(line)
		} else {
			line = styles.Muted.Render(line)
		}
		lines = append(lines, line)

		// Register the hit region on the row this suggestion is actually drawn
		// on, which is below the whole input bar - not one row below its first
		// line.
		p.mouseHandler.HitMap.AddRect(regionFileOpSuggestion, 0, topY+i, p.treeWidth, 1, i)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderTreePane renders the file tree in the left pane.
func (p *Plugin) renderTreePane(visibleHeight int) string {
	var sb strings.Builder

	// Whatever this pane draws this pass — tree or search results — is what
	// gets a scrollbar; forget both so an undrawn one can never register.
	p.clearTreeColumnBars()

	// Header with sort mode and ignored indicator
	header := styles.Title.Render("Files")
	sb.WriteString(header)
	if p.tree != nil {
		sb.WriteString("  ")
		sb.WriteString(styles.Muted.Render("[" + p.tree.SortMode.Label() + "]"))
		if !p.showIgnored {
			sb.WriteString(" ")
			sb.WriteString(styles.Muted.Render("[ignored: hidden]"))
		}
	}
	sb.WriteString("\n")

	// Search bar (if in search mode) - rendered inside the pane like conversations plugin
	if p.searchMode {
		searchLine := p.renderTreeSearchBar()
		sb.WriteString(searchLine)
		sb.WriteString("\n")
	} else if dragLine := p.renderDragStatusLine(); dragLine != "" {
		// The row the search bar would use is otherwise blank, so live drag
		// status goes there: it needs no new bar and, crucially, no change in
		// height - a layout shift mid-gesture would move every row out from
		// under the cursor.
		sb.WriteString(dragLine)
		sb.WriteString("\n")
	} else {
		sb.WriteString("\n") // Empty line when not searching
	}

	// In search mode, show filtered results instead of full tree
	if p.searchMode {
		if len(p.searchMatches) > 0 {
			return p.renderSearchResults(&sb, visibleHeight)
		} else if p.searchQuery() != "" {
			// Show "no matches" when query exists but no results
			sb.WriteString(styles.Muted.Render("No matching files"))
			return sb.String()
		}
		// Empty query - fall through to show full tree
	}

	if p.tree == nil || p.tree.Len() == 0 {
		sb.WriteString(styles.Muted.Render("No files"))
		return sb.String()
	}

	// visibleHeight already accounts for header (2 lines) in renderNormalPanes()
	// So we use it directly - no additional subtraction needed
	end := p.treeScrollOff + visibleHeight
	if end > p.tree.Len() {
		end = p.tree.Len()
	}

	maxWidth := treeNodeWidth(p.treeWidth)

	var treeSB strings.Builder
	for i := p.treeScrollOff; i < end; i++ {
		node := p.tree.GetNode(i)
		if node == nil {
			continue
		}

		selected := i == p.treeCursor
		line := p.renderTreeNode(node, selected, maxWidth)

		treeSB.WriteString(line)
		// Don't add newline after last line
		if i < end-1 {
			treeSB.WriteString("\n")
		}
	}

	scrollbar := p.drawScrollbar(sbTree, p.tree.Len(), p.treeScrollOff, visibleHeight)

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, treeColumn(treeSB.String(), maxWidth), scrollbar))
	return sb.String()
}

// renderSearchResults renders the filtered search results list.
func (p *Plugin) renderSearchResults(sb *strings.Builder, visibleHeight int) string {
	maxWidth := treeNodeWidth(p.treeWidth)

	// The scroll offset: a scrollbar gesture pins it; otherwise it follows the
	// cursor. Both cases resolve here, once, so rows and bar always agree.
	searchScrollOff := p.effectiveSearchScrollOff(visibleHeight)

	end := searchScrollOff + visibleHeight
	if end > len(p.searchMatches) {
		end = len(p.searchMatches)
	}

	var resultSB strings.Builder
	for i := searchScrollOff; i < end; i++ {
		match := p.searchMatches[i]
		selected := i == p.searchCursor

		// Show full path for search results with fuzzy match highlighting
		displayPath := match.Path
		if len(displayPath) > maxWidth-2 {
			displayPath = "…" + displayPath[len(displayPath)-maxWidth+3:]
		}

		if selected {
			// Full-width highlight for selected item
			if len(displayPath) < maxWidth {
				displayPath += strings.Repeat(" ", maxWidth-len(displayPath))
			}
			resultSB.WriteString(styles.ListItemSelected.Render(displayPath))
		} else {
			// Render with fuzzy match highlighting (all items are files from cache)
			if len(match.MatchRanges) > 0 && len(match.Path) <= maxWidth-2 {
				resultSB.WriteString(filefind.HighlightMatch(displayPath, match.MatchRanges))
			} else {
				resultSB.WriteString(styles.FileBrowserFile.Render(displayPath))
			}
		}

		if i < end-1 {
			resultSB.WriteString("\n")
		}
	}

	scrollbar := p.drawScrollbar(sbSearch, len(p.searchMatches), searchScrollOff, visibleHeight)

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, treeColumn(resultSB.String(), maxWidth), scrollbar))
	return sb.String()
}

// renderDragStatusLine describes an in-flight drag: what is moving and where it
// would land. A TUI has no drag cursor and the row highlight is subtle in some
// themes, so this text is the primary signal that a drop will (or will not)
// work.
func (p *Plugin) renderDragStatusLine() string {
	if !p.dragActive || p.dragSourcePath == "" {
		return ""
	}

	name := filepath.Base(p.dragSourcePath)
	maxWidth := p.treeWidth - 4
	if maxWidth < 8 {
		maxWidth = 8
	}

	// The pane is narrow, so drop the decoration before the information: the
	// destination (or the refusal) is what the user needs to read.
	var candidates []string
	style := styles.Muted
	if p.dragDropIdx >= 0 {
		dst := displayDropDir(p.dragDropDir)
		candidates = []string{
			"moving " + name + " → " + dst,
			name + " → " + dst,
			"→ " + dst,
		}
	} else {
		style = styles.StatusDeleted
		candidates = []string{
			"moving " + name + " · can't drop here",
			"can't drop here",
		}
	}

	text := candidates[len(candidates)-1]
	for _, c := range candidates {
		if ansi.StringWidth(c) <= maxWidth {
			text = c
			break
		}
	}
	return style.Render(ansi.Truncate(text, maxWidth, "…"))
}

// treeNodeWidth is the cell width a tree row is rendered into, given the pane
// width: the border padding and the scrollbar column come off the top.
func treeNodeWidth(treeWidth int) int {
	w := treeWidth - 4 - 1
	if w < 1 {
		w = 1
	}
	return w
}

// treeColumn pads every line of the rendered tree to a fixed width so the
// scrollbar sits in the same column no matter how the rows are styled.
//
// JoinHorizontal sizes a block to its widest line, so without this the
// scrollbar's position depends on which rows happen to be padded. Only rows
// drawn with a full-width highlight are, which meant the scrollbar slid left to
// hug the longest filename whenever none was on screen: while dragging (the
// dragged row returns early, before the cursor highlight) and, before drag
// existed at all, whenever the cursor was scrolled out of view.
//
// Padding is measured in display cells and the rows are already truncated to
// width, so a CJK or emoji filename cannot push the column out.
func treeColumn(rendered string, width int) string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		if pad := width - ansi.StringWidth(line); pad > 0 {
			lines[i] = line + strings.Repeat(" ", pad)
		}
	}
	return strings.Join(lines, "\n")
}

// renderTreeNode renders a single tree node.
func (p *Plugin) renderTreeNode(node *FileNode, selected bool, maxWidth int) string {
	// Indentation
	indent := strings.Repeat("  ", node.Depth)

	// Icon for directories
	icon := "  "
	if node.IsDir {
		if node.IsExpanded {
			icon = "> "
		} else {
			icon = "+ "
		}
	}

	// Calculate available width for name (after indent and icon).
	// Widths are measured in display cells, not bytes: a CJK or emoji filename
	// is wider than its byte length suggests, and slicing it by bytes cuts
	// mid-rune.
	prefixLen := ansi.StringWidth(indent) + ansi.StringWidth(icon)
	availableWidth := maxWidth - prefixLen
	if availableWidth < 3 {
		availableWidth = 3
	}

	// Truncate name before styling to avoid cutting ANSI escape codes
	displayName := node.Name
	if ansi.StringWidth(displayName) > availableWidth {
		displayName = ansi.Truncate(displayName, availableWidth, "…")
	}

	// Name styling
	var name string
	if node.IsDir {
		name = styles.FileBrowserDir.Render(displayName)
	} else if node.IsIgnored {
		name = styles.FileBrowserIgnored.Render(displayName)
	} else {
		name = styles.FileBrowserFile.Render(displayName)
	}

	line := fmt.Sprintf("%s%s%s", indent, styles.FileBrowserIcon.Render(icon), name)

	// Plain text version, padded, for any full-width highlight.
	fullWidth := func() string {
		plainLine := indent + icon + displayName
		if w := ansi.StringWidth(plainLine); w < maxWidth {
			plainLine += strings.Repeat(" ", maxWidth-w)
		}
		return plainLine
	}

	if p.dragActive {
		// The drop target gets its own full-width highlight, distinct from the
		// cursor highlight, because during a drag both are on screen at once
		// and they mean different things.
		//
		// The highlight is only drawn on the destination directory's own row.
		// Dropping on a top-level file resolves to the project root, which has
		// no row: highlighting the hovered file there would claim the file is
		// the destination. The status line carries that case instead.
		if p.dragDropIdx >= 0 && node.IsDir && node.Path == p.dragDropDir &&
			p.tree != nil && p.tree.GetNode(p.dragDropIdx) == node {
			return styles.FileBrowserDropTarget.Render(fullWidth())
		}
		// The row being dragged is dimmed. This check comes before the cursor
		// highlight on purpose: the source row is almost always the cursor row,
		// so the normal highlight would otherwise hide the fact that it is the
		// thing in flight.
		if p.dragSourcePath != "" && node.Path == p.dragSourcePath {
			return styles.FileBrowserDragSource.Render(indent + icon + displayName)
		}
	}

	if selected {
		return styles.ListItemSelected.Render(fullWidth())
	}
	return line
}

// renderPreviewPane renders the file preview in the right pane.
func (p *Plugin) renderPreviewPane(visibleHeight int) string {
	// The bar is only drawn when source lines are; forget the previous pass
	// before any early return so a stale bar can never register regions.
	p.bars[sbPreview] = scrollbarBar{}

	// Handle inline edit mode - render editor within preview pane
	if p.edit.Active && p.edit.Model != nil && p.edit.Model.IsActive() {
		return p.renderInlineEditorContent(visibleHeight)
	}
	// The caller passes the panel's full inner height. Source rows begin only
	// after this view's two header rows, so use the same capacity exported to
	// the content-link host instead of relying on the panel to clip overflow.
	visibleHeight = p.previewSourceRowCapacity()

	var sb strings.Builder

	// Tab line (replaces the blank spacer line when multiple tabs are open)
	tabLine := ""
	if len(p.tabs) > 1 {
		tabLine = p.renderPreviewTabs(p.previewWidth - 4)
	} else {
		p.tabHits = nil
	}
	if tabLine != "" {
		sb.WriteString(tabLine)
		sb.WriteString("\n")
	}

	// Header with file path
	header := "Preview"
	if p.previewFile != "" {
		header = truncatePath(p.previewFile, p.previewWidth-4)
		// Add markdown render indicator
		if p.isMarkdownFile() && p.markdownRenderMode {
			header += " [rendered]"
		}
	}
	sb.WriteString(styles.Title.Render(header))

	// Metadata line (size, mod time, permissions)
	if p.previewFile != "" && p.previewSize > 0 {
		meta := fmt.Sprintf("%s  %s  %s",
			formatSize(p.previewSize),
			p.previewModTime.Format("Jan 2 15:04"),
			p.previewMode.String(),
		)
		sb.WriteString("  ")
		sb.WriteString(styles.Muted.Render(meta))
	}
	if tabLine == "" {
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("\n")
	}

	if p.previewFile == "" {
		sb.WriteString(styles.Muted.Render("Select a file to preview"))
		return sb.String()
	}

	if p.previewError != nil {
		sb.WriteString(styles.StatusDeleted.Render(p.previewError.Error()))
		return sb.String()
	}

	// Handle image preview
	if p.isImage {
		sb.WriteString(p.renderImagePreview())
		return sb.String()
	}

	if p.isBinary {
		sb.WriteString(styles.Muted.Render("Binary file"))
		return sb.String()
	}

	// The rows being drawn and the geometry exported to the content-link host
	// read the same accessor, so they cannot disagree about what is on screen.
	// Glamour output does not map 1:1 to source lines, so it is not numbered.
	lines, showLineNumbers := p.previewRenderLines()

	start := max(p.previewScroll, 0)
	end := start + visibleHeight
	if end > len(lines) {
		end = len(lines)
	}

	// The gutter sizes itself to the line count, so a file past 9999 lines
	// gets a column wide enough to hold its numbers instead of clipping them.
	// A disabled gutter (rendered markdown) renders nothing and measures zero,
	// which is why the branches below can just ask it for a cell.
	gutter := docview.NewGutter(len(lines))
	if !showLineNumbers {
		gutter = docview.Gutter{}
	}
	contentWidth := p.previewContentWidth()
	_, maxLineWidth := p.previewTextWidths()

	// Style for truncating lines with ANSI codes
	lineStyle := lipgloss.NewStyle().MaxWidth(maxLineWidth)

	// Reserve 1 line for truncation message if needed
	contentEnd := end
	if p.isTruncated && end-start > 1 {
		contentEnd = end - 1
	}

	var contentSB strings.Builder
	visualLinesRendered := 0
	renderedAll := false
	for i := start; i < contentEnd; i++ {
		if p.previewWrapEnabled && visualLinesRendered >= visibleHeight {
			break
		}

		// Check if this line is selected for text selection highlighting
		startCol, endCol := p.selection.GetLineSelectionCols(i)
		if startCol >= 0 {
			// Get syntax-highlighted content and inject character-level selection background
			var lineContent string
			if i < len(lines) {
				lineContent = lines[i]
			}

			if p.previewWrapEnabled {
				wrappedLines := p.wrapPreviewLine(lineContent, maxLineWidth)

				// Track visual column offset into the original (expanded) line.
				// endCol == -1 means "to end of line".
				selStart := startCol
				selEnd := endCol
				if selEnd == -1 {
					selEnd = int(^uint(0) >> 1) // MaxInt
				}

				offset := 0
				for wi, wl := range wrappedLines {
					if visualLinesRendered >= visibleHeight {
						renderedAll = true
						break
					}

					segWidth := ansi.StringWidth(wl)
					segStart := offset
					segEnd := offset + segWidth - 1

					// Apply selection only if this wrapped segment overlaps. A
					// selected empty row still paints one cell so a multi-paragraph
					// selection reads as one continuous block.
					if segWidth == 0 && selStart == 0 {
						wl = ui.InjectCharacterRangeBackground(" ", 0, 0)
					} else if selStart <= segEnd && selEnd >= segStart && segWidth > 0 {
						localStart := selStart - segStart
						if localStart < 0 {
							localStart = 0
						}
						localEnd := selEnd - segStart
						if localEnd >= segWidth {
							localEnd = segWidth - 1
						}
						wl = ui.InjectCharacterRangeBackground(wl, localStart, localEnd)
					}

					// Raw source keeps its historical selected gutter. Rendered
					// Markdown has no gutter, so only the selected text is written.
					if wi == 0 {
						lineNumber := gutter.Number(i + 1)
						if showLineNumbers {
							lineNumber = ui.InjectSelectionBackground(lineNumber)
						}
						contentSB.WriteString(lineNumber)
					} else {
						contentSB.WriteString(gutter.Blank())
					}
					contentSB.WriteString(wl)
					if visualLinesRendered < visibleHeight-1 || p.isTruncated {
						contentSB.WriteString("\n")
					}
					visualLinesRendered++
					offset += segWidth
				}

				if renderedAll {
					break
				}
			} else {
				lineContent = ui.ExpandTabs(lineContent, 8)
				if ansi.StringWidth(lineContent) == 0 {
					lineContent = ui.InjectCharacterRangeBackground(" ", 0, 0)
				} else {
					lineContent = ui.InjectCharacterRangeBackground(lineContent, startCol, endCol)
				}
				// Truncate using lipgloss (handles ANSI codes properly)
				lineNumStr := gutter.Number(i + 1)
				if showLineNumbers {
					lineNumStr = ui.InjectSelectionBackground(lineNumStr)
				}
				contentSB.WriteString(lineNumStr)
				lineContent = lipgloss.NewStyle().MaxWidth(maxLineWidth).Render(lineContent)
				contentSB.WriteString(lineContent)

				// Pad remaining width with selection background if full-line selection
				if startCol == 0 && endCol == -1 {
					cw := lipgloss.Width(lineNumStr) + lipgloss.Width(lineContent)
					if cw < contentWidth {
						padding := strings.Repeat(" ", contentWidth-cw)
						contentSB.WriteString(ui.InjectSelectionBackground(padding))
					}
				}
				visualLinesRendered++
			}
		} else {
			// Get line content
			var lineContent string
			if p.contentSearchMode && len(p.contentSearchMatches) > 0 {
				if p.markdownRenderMode && p.isMarkdownFile() && len(p.markdownRendered) > 0 {
					lineContent = p.highlightMarkdownLineMatches(i)
				} else if showLineNumbers {
					// Use raw lines for highlighting (loses syntax highlighting on matched lines)
					lineContent = p.highlightLineMatches(i)
				} else if i < len(lines) {
					lineContent = lines[i]
				}
			} else if i < len(lines) {
				lineContent = lines[i]
			}

			if p.previewWrapEnabled {
				wrappedLines := p.wrapPreviewLine(lineContent, maxLineWidth)
				for wi, wl := range wrappedLines {
					if visualLinesRendered >= visibleHeight {
						break
					}
					if wi == 0 {
						contentSB.WriteString(gutter.Number(i + 1))
					} else {
						contentSB.WriteString(gutter.Blank())
					}
					contentSB.WriteString(wl)
					if visualLinesRendered < visibleHeight-1 || p.isTruncated {
						contentSB.WriteString("\n")
					}
					visualLinesRendered++
				}
			} else {
				lineContent = ui.ExpandTabs(lineContent, 8)
				line := lineStyle.Render(lineContent)

				// Render with or without line numbers
				contentSB.WriteString(gutter.Number(i + 1))
				contentSB.WriteString(line)
				visualLinesRendered++
			}
		}

		// Don't add newline after last line (non-wrap path)
		if !p.previewWrapEnabled {
			if i < contentEnd-1 || p.isTruncated {
				contentSB.WriteString("\n")
			}
		}
	}

	if p.isTruncated {
		contentSB.WriteString(styles.Muted.Render("... (file truncated)"))
	}

	scrollbar := p.drawScrollbar(sbPreview, len(lines), p.previewScroll, visibleHeight)

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, treeColumn(contentSB.String(), contentWidth), scrollbar))
	return sb.String()
}

// wrapPreviewLine wraps a single line to width using plain-text breakpoints,
// then slices the original ANSI line to preserve styling.
func (p *Plugin) wrapPreviewLine(line string, width int) []string {
	if width < 1 {
		return []string{""}
	}

	expanded := ui.ExpandTabs(line, 8)
	plain := ansi.Strip(expanded)
	wrappedPlain := cellbuf.Wrap(plain, width, "")
	plainSegments := strings.Split(wrappedPlain, "\n")

	wrapped := make([]string, 0, len(plainSegments))
	offset := 0
	for _, seg := range plainSegments {
		segWidth := ansi.StringWidth(seg)
		if segWidth == 0 {
			wrapped = append(wrapped, "")
			continue
		}
		slice := ansi.TruncateLeft(expanded, offset, "")
		slice = ansi.Truncate(slice, segWidth, "")
		wrapped = append(wrapped, slice)
		offset += segWidth
	}

	return wrapped
}

func (p *Plugin) previewSelectionAtXY(x, y int) (int, int, bool) {
	lines := p.previewDisplayLines()
	rect := p.previewTextRect()
	if len(lines) == 0 || rect.W <= 0 || rect.H <= 0 {
		return 0, 0, false
	}

	row := y - rect.Y
	if row < 0 {
		return 0, 0, false
	}
	// Motions keep extending when the pointer leaves the bottom edge; a press
	// below the body never reaches this function because no line region exists
	// there. Clamp to the last row that was actually drawn, not the last source
	// row, so padding cannot select content.
	if row >= rect.H {
		row = rect.H - 1
	}

	_, maxLineWidth := p.previewTextWidths()

	if !p.previewWrapEnabled {
		lineIdx := p.previewScroll + row
		if lineIdx < 0 || lineIdx >= len(lines) {
			return 0, 0, false
		}
		col := p.previewColAtScreenX(x, lineIdx)
		return lineIdx, col, true
	}

	remainingRow := row
	lineIdx := p.previewScroll
	for lineIdx < len(lines) {
		lineContent := p.previewSelectionLine(lineIdx)
		segments := p.wrapPreviewLine(lineContent, maxLineWidth)
		if len(segments) == 0 {
			segments = []string{""}
		}
		if remainingRow < len(segments) {
			segIdx := remainingRow
			segStart := 0
			for i := 0; i < segIdx; i++ {
				segStart += ansi.StringWidth(segments[i])
			}

			relX := x - rect.X
			if relX < 0 {
				relX = 0
			}

			expanded := ui.ExpandTabs(lineContent, 8)
			segmentText := ui.VisualSubstring(expanded, segStart, -1)
			colInSeg := ui.VisualColAtRelativeX(segmentText, relX)
			col := segStart + colInSeg

			lineWidth := ansi.StringWidth(ansi.Strip(expanded))
			if lineWidth <= 0 {
				col = 0
			} else if col > lineWidth-1 {
				col = lineWidth - 1
			}

			return lineIdx, col, true
		}
		remainingRow -= len(segments)
		lineIdx++
	}

	return 0, 0, false
}

// previewGutter is the line-number gutter the preview is currently rendered
// with. The hit-test geometry reads it too, so a click lands on the column it
// looks like it lands on however wide the numbers have grown.
func (p *Plugin) previewGutter() docview.Gutter {
	lines, showLineNumbers := p.previewRenderLines()
	if !showLineNumbers {
		return docview.Gutter{}
	}
	return docview.NewGutter(len(lines))
}

func (p *Plugin) previewRenderLines() ([]string, bool) {
	showLineNumbers := !p.markdownRenderMode || !p.isMarkdownFile() || len(p.markdownRendered) == 0

	if showLineNumbers {
		if len(p.previewHighlighted) > 0 {
			return p.previewHighlighted, showLineNumbers
		}
		return p.previewLines, showLineNumbers
	}
	return p.markdownRendered, showLineNumbers
}

// highlightLineMatches applies search match highlighting to a line.
func (p *Plugin) highlightLineMatches(lineNo int) string {
	// Get raw line (not syntax highlighted)
	if lineNo >= len(p.previewLines) {
		return ""
	}
	rawLine := p.previewLines[lineNo]

	// Find all matches on this line
	type lineMatch struct {
		matchIdx int // Index in contentSearchMatches (for current detection)
		startCol int
		endCol   int
	}
	var lineMatches []lineMatch

	for i, m := range p.contentSearchMatches {
		if m.LineNo == lineNo {
			lineMatches = append(lineMatches, lineMatch{
				matchIdx: i,
				startCol: m.StartCol,
				endCol:   m.EndCol,
			})
		}
	}

	if len(lineMatches) == 0 {
		// No matches on this line, use syntax highlighted version if available
		if lineNo < len(p.previewHighlighted) {
			return p.previewHighlighted[lineNo]
		}
		return rawLine
	}

	// Build highlighted line from raw text
	var result strings.Builder
	lastEnd := 0

	for _, m := range lineMatches {
		if m.startCol > len(rawLine) || m.endCol > len(rawLine) {
			continue
		}
		if m.startCol < lastEnd {
			continue // Overlapping match, skip
		}

		// Add text before match
		if m.startCol > lastEnd {
			result.WriteString(rawLine[lastEnd:m.startCol])
		}

		// Apply highlight style (current match vs other matches)
		matchText := rawLine[m.startCol:m.endCol]
		if m.matchIdx == p.contentSearchCursor {
			result.WriteString(styles.SearchMatchCurrent.Render(matchText))
		} else {
			result.WriteString(styles.SearchMatch.Render(matchText))
		}
		lastEnd = m.endCol
	}

	// Add remaining text
	if lastEnd < len(rawLine) {
		result.WriteString(rawLine[lastEnd:])
	}

	return result.String()
}

// truncatePath shortens a path to fit width (rune-based for Unicode safety).
func truncatePath(path string, maxWidth int) string {
	runes := []rune(path)
	if len(runes) <= maxWidth {
		return path
	}
	if maxWidth < 10 {
		return string(runes[:maxWidth])
	}
	// Show ...end of path
	return "..." + string(runes[len(runes)-maxWidth+3:])
}

func formatSize(bytes int64) string {
	return docview.FormatSize(bytes)
}

// renderImagePreview renders image preview or fallback message.
func (p *Plugin) renderImagePreview() string {
	// Calculate available dimensions (subtract border + padding = 4)
	contentHeight := p.height - 4
	contentWidth := p.previewWidth - 4
	if contentWidth < 10 {
		contentWidth = 10
	}
	if contentHeight < 5 {
		contentHeight = 5
	}

	// Get full path for rendering
	fullPath := filepath.Join(p.tree.RootDir, p.previewFile)

	// Render image
	result, err := p.imageRenderer.Render(fullPath, contentWidth, contentHeight)
	if err != nil {
		return styles.Muted.Render(fmt.Sprintf("Image error: %v", err))
	}

	// Cache result for resize detection
	p.imageResult = result

	if result.IsFallback {
		// Show informative fallback message
		ext := filepath.Ext(p.previewFile)
		msg := fmt.Sprintf("Image file (%s)", ext)

		if result.Content != "" {
			// Custom fallback message (e.g., "too large")
			msg = result.Content
		}

		hint := "Preview in: " + image.SupportedTerminals()

		return lipgloss.JoinVertical(lipgloss.Center,
			styles.Muted.Render(msg),
			"",
			styles.Muted.Render(hint),
		)
	}

	return result.Content
}

// renderLineJumpBar renders the line jump input bar.
func (p *Plugin) renderLineJumpBar() string {
	cursor := "█"
	inputLine := fmt.Sprintf(" :%s%s", p.lineJumpBuffer, cursor)
	return styles.ModalTitle.Render(inputLine)
}
