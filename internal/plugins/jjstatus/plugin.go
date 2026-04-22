package jjstatus

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/filebrowser"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/ui"
	jjvcs "github.com/marcus/sidecar/internal/vcs/jj"
)

const (
	pluginID   = "jj-status"
	pluginName = "jj"
	pluginIcon = "J"
)

type mode int

const (
	modeStatus mode = iota
	modeLog
)

type focusPane int

const (
	paneLeft focusPane = iota
	paneDiff
)

const dividerWidth = 1

// StatusLoadedMsg carries a loaded working-copy status snapshot.
type StatusLoadedMsg struct {
	Epoch    uint64
	Snapshot jjvcs.StatusSnapshot
	Err      error
}

// GetEpoch implements plugin.EpochMessage.
func (m StatusLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// LogLoadedMsg carries loaded commit log entries.
type LogLoadedMsg struct {
	Epoch   uint64
	Commits []jjvcs.StructuredCommit
	Err     error
}

// GetEpoch implements plugin.EpochMessage.
func (m LogLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// DiffLoadedMsg carries loaded diff text.
type DiffLoadedMsg struct {
	Epoch   uint64
	Content string
	Err     error
}

// GetEpoch implements plugin.EpochMessage.
func (m DiffLoadedMsg) GetEpoch() uint64 { return m.Epoch }

// EditDoneMsg carries completion of a jj edit action.
type EditDoneMsg struct {
	Epoch uint64
	Err   error
}

// GetEpoch implements plugin.EpochMessage.
func (m EditDoneMsg) GetEpoch() uint64 { return m.Epoch }

// Plugin provides a read-only jj status/log/diff view.
type Plugin struct {
	ctx    *plugin.Context
	client *jjvcs.Client
	root   string

	focused bool
	width   int
	height  int

	mode       mode
	activePane focusPane

	loadingStatus bool
	loadingLog    bool
	loadingDiff   bool

	status       jjvcs.StatusSnapshot
	statusCursor int
	statusScroll int

	structuredCommits []jjvcs.StructuredCommit
	graphRows         []jjvcs.GraphRow
	logCursor         int
	logScroll         int

	diff       string
	diffScroll int

	leftPaneWidth int
	diffPaneWidth int

	lastErr error

	lastClickCommitID string
	lastClickAt       time.Time
}

// New creates a new jj status plugin.
func New() *Plugin {
	return &Plugin{
		client:     jjvcs.New(),
		mode:       modeStatus,
		activePane: paneLeft,
	}
}

func (p *Plugin) ID() string   { return pluginID }
func (p *Plugin) Name() string { return pluginName }
func (p *Plugin) Icon() string { return pluginIcon }

// Init checks jj workspace availability and initializes plugin state.
func (p *Plugin) Init(ctx *plugin.Context) error {
	p.ctx = ctx
	p.mode = modeStatus
	p.activePane = paneLeft
	p.status = jjvcs.StatusSnapshot{}
	p.statusCursor = 0
	p.statusScroll = 0
	p.structuredCommits = nil
	p.graphRows = nil
	p.logCursor = 0
	p.logScroll = 0
	p.diff = ""
	p.diffScroll = 0
	p.lastErr = nil
	p.loadingStatus = false
	p.loadingLog = false
	p.loadingDiff = false

	root, ok, err := p.client.Detect(ctx.WorkDir)
	if err != nil {
		return fmt.Errorf("jj detect failed: %w", err)
	}
	if !ok {
		return fmt.Errorf("no jj workspace in %s", ctx.WorkDir)
	}
	p.root = root
	return nil
}

func (p *Plugin) Start() tea.Cmd {
	return p.refreshAll()
}

func (p *Plugin) Stop() {}

func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		return p, nil
	case app.PluginFocusedMsg:
		return p, p.refreshAll()
	case StatusLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.loadingStatus = false
		if msg.Err != nil {
			p.lastErr = msg.Err
			return p, nil
		}
		p.lastErr = nil
		p.status = msg.Snapshot
		p.statusCursor = clampCursor(p.statusCursor, len(p.status.Changes))
		if p.mode == modeStatus {
			return p, p.loadSelectedDiff()
		}
		return p, nil
	case LogLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.loadingLog = false
		if msg.Err != nil {
			p.lastErr = msg.Err
			return p, nil
		}
		p.lastErr = nil
		p.structuredCommits = msg.Commits
		p.graphRows = jjvcs.LayoutGraph(msg.Commits)
		p.logCursor = clampCursor(p.logCursor, len(p.structuredCommits))
		if p.mode == modeLog {
			return p, p.loadSelectedDiff()
		}
		return p, nil
	case EditDoneMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.Err != nil {
			p.lastErr = msg.Err
			return p, nil
		}
		p.lastErr = nil
		return p, tea.Batch(
			app.ShowToast("Editing selected commit", 2*time.Second),
			p.refreshAll(),
		)
	case DiffLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.loadingDiff = false
		if msg.Err != nil {
			p.lastErr = msg.Err
			p.diff = ""
			return p, nil
		}
		p.lastErr = nil
		p.diff = msg.Content
		return p, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			return p, p.refreshAll()
		case "tab", "shift+tab":
			p.toggleMode()
			return p, p.loadSelectedDiff()
		case "l", "right":
			if p.activePane == paneLeft {
				p.activePane = paneDiff
				return p, nil
			}
		case "h", "left":
			if p.activePane == paneDiff {
				p.activePane = paneLeft
				return p, nil
			}
		case "\\":
			if p.activePane == paneLeft {
				p.activePane = paneDiff
			} else {
				p.activePane = paneLeft
			}
			return p, nil
		case "j", "down":
			if p.activePane == paneDiff {
				p.diffScroll++
				p.clampDiffScroll()
				return p, nil
			}
			if p.mode == modeStatus && p.statusCursor < len(p.status.Changes)-1 {
				p.statusCursor++
				p.ensureStatusCursorVisible()
				return p, p.loadSelectedDiff()
			}
			if p.mode == modeLog && p.logCursor < len(p.structuredCommits)-1 {
				p.logCursor++
				p.ensureLogCursorVisible()
				return p, p.loadSelectedDiff()
			}
			return p, nil
		case "k", "up":
			if p.activePane == paneDiff {
				p.diffScroll--
				p.clampDiffScroll()
				return p, nil
			}
			if p.mode == modeStatus && p.statusCursor > 0 {
				p.statusCursor--
				p.ensureStatusCursorVisible()
				return p, p.loadSelectedDiff()
			}
			if p.mode == modeLog && p.logCursor > 0 {
				p.logCursor--
				p.ensureLogCursorVisible()
				return p, p.loadSelectedDiff()
			}
			return p, nil
		case "g":
			if p.activePane == paneDiff {
				p.diffScroll = 0
				return p, nil
			}
			if p.mode == modeStatus {
				p.statusCursor = 0
				p.statusScroll = 0
				return p, p.loadSelectedDiff()
			}
			p.logCursor = 0
			p.logScroll = 0
			return p, p.loadSelectedDiff()
		case "G":
			if p.activePane == paneDiff {
				lines := strings.Split(p.diff, "\n")
				p.diffScroll = max(0, len(lines)-p.diffPageSize())
				return p, nil
			}
			if p.mode == modeStatus {
				p.statusCursor = clampCursor(len(p.status.Changes)-1, len(p.status.Changes))
				p.ensureStatusCursorVisible()
				return p, p.loadSelectedDiff()
			}
			p.logCursor = clampCursor(len(p.structuredCommits)-1, len(p.structuredCommits))
			p.ensureLogCursorVisible()
			return p, p.loadSelectedDiff()
		case "d":
			return p, p.loadSelectedDiff()
		case "e":
			if p.mode == modeLog {
				return p, p.editSelectedCommit()
			}
			return p, nil
		case "ctrl+d":
			p.diffScroll += p.diffPageSize()
			p.clampDiffScroll()
			return p, nil
		case "ctrl+u":
			p.diffScroll -= p.diffPageSize()
			p.clampDiffScroll()
			return p, nil
		case "O", "enter":
			if p.mode == modeStatus {
				path := p.selectedPath()
				if path != "" {
					return p, p.openInFileBrowser(path)
				}
			} else if p.mode == modeLog {
				return p, p.editSelectedCommit()
			}
		}
	case tea.MouseMsg:
		return p.handleMouse(msg)
	}
	return p, nil
}

// calculatePaneWidths sets left and diff pane widths.
func (p *Plugin) calculatePaneWidths() {
	available := p.width - dividerWidth
	if p.leftPaneWidth == 0 {
		p.leftPaneWidth = available * 35 / 100
	}
	minWidth := 25
	maxWidth := available - 40
	if maxWidth < minWidth {
		maxWidth = minWidth
	}
	if p.leftPaneWidth < minWidth {
		p.leftPaneWidth = minWidth
	} else if p.leftPaneWidth > maxWidth {
		p.leftPaneWidth = maxWidth
	}
	p.diffPaneWidth = available - p.leftPaneWidth
	if p.diffPaneWidth < 40 {
		p.diffPaneWidth = 40
	}
}

func (p *Plugin) View(width, height int) string {
	p.width, p.height = width, height
	if width <= 0 || height <= 0 {
		return ""
	}

	p.calculatePaneWidths()

	paneHeight := height
	if paneHeight < 4 {
		paneHeight = 4
	}

	// Inner height for content (borders consume 2 lines)
	innerHeight := paneHeight - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	leftActive := p.activePane == paneLeft
	diffActive := p.activePane == paneDiff

	leftContent := p.renderLeftPaneContent(p.leftPaneWidth-4, innerHeight)
	diffContent := p.renderDiffPaneContent(p.diffPaneWidth-4, innerHeight)

	leftPanel := styles.RenderPanel(leftContent, p.leftPaneWidth, paneHeight, leftActive)
	divider := ui.RenderDivider(paneHeight)
	rightPanel := styles.RenderPanel(diffContent, p.diffPaneWidth, paneHeight, diffActive)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, divider, rightPanel)
}

func (p *Plugin) IsFocused() bool { return p.focused }
func (p *Plugin) SetFocused(f bool) {
	p.focused = f
}

func (p *Plugin) Commands() []plugin.Command {
	return []plugin.Command{
		{
			ID:       "refresh",
			Name:     "Refresh",
			Category: plugin.CategoryGit,
			Context:  "jj-status",
			Priority: 1,
		},
		{
			ID:       "switch-pane",
			Name:     "Mode",
			Category: plugin.CategoryView,
			Context:  "jj-status",
			Priority: 2,
		},
		{
			ID:       "show-diff",
			Name:     "Diff",
			Category: plugin.CategoryView,
			Context:  "jj-status",
			Priority: 3,
		},
		{
			ID:       "open-in-file-browser",
			Name:     "Open",
			Category: plugin.CategoryActions,
			Context:  "jj-status",
			Priority: 4,
		},
		{
			ID:       "page-down",
			Name:     "PgDn",
			Category: plugin.CategoryView,
			Context:  "jj-status",
			Priority: 5,
		},
		{
			ID:       "page-up",
			Name:     "PgUp",
			Category: plugin.CategoryView,
			Context:  "jj-status",
			Priority: 6,
		},
		{
			ID:       "refresh",
			Name:     "Refresh",
			Category: plugin.CategoryGit,
			Context:  "jj-log",
			Priority: 1,
		},
		{
			ID:       "switch-pane",
			Name:     "Mode",
			Category: plugin.CategoryView,
			Context:  "jj-log",
			Priority: 2,
		},
		{
			ID:       "show-diff",
			Name:     "Diff",
			Category: plugin.CategoryView,
			Context:  "jj-log",
			Priority: 3,
		},
		{
			ID:       "edit-commit",
			Name:     "Edit",
			Category: plugin.CategoryActions,
			Context:  "jj-log",
			Priority: 4,
		},
		{
			ID:       "page-down",
			Name:     "PgDn",
			Category: plugin.CategoryView,
			Context:  "jj-log",
			Priority: 5,
		},
		{
			ID:       "page-up",
			Name:     "PgUp",
			Category: plugin.CategoryView,
			Context:  "jj-log",
			Priority: 6,
		},
	}
}

func (p *Plugin) FocusContext() string {
	if p.mode == modeLog {
		return "jj-log"
	}
	return "jj-status"
}

func (p *Plugin) refreshAll() tea.Cmd {
	p.loadingStatus = true
	p.loadingLog = true
	p.loadingDiff = true
	return tea.Batch(p.loadStatus(), p.loadLog(), p.loadSelectedDiff())
}

func (p *Plugin) loadStatus() tea.Cmd {
	if p.ctx == nil {
		return nil
	}
	epoch := p.ctx.Epoch
	workDir := p.ctx.WorkDir
	client := p.client
	return func() tea.Msg {
		snapshot, err := client.Status(workDir)
		return StatusLoadedMsg{
			Epoch:    epoch,
			Snapshot: snapshot,
			Err:      err,
		}
	}
}

func (p *Plugin) loadLog() tea.Cmd {
	if p.ctx == nil {
		return nil
	}
	epoch := p.ctx.Epoch
	workDir := p.ctx.WorkDir
	client := p.client
	return func() tea.Msg {
		commits, err := client.LogStructured(workDir, 80)
		return LogLoadedMsg{
			Epoch:   epoch,
			Commits: commits,
			Err:     err,
		}
	}
}

func (p *Plugin) loadSelectedDiff() tea.Cmd {
	if p.ctx == nil {
		return nil
	}
	epoch := p.ctx.Epoch
	workDir := p.ctx.WorkDir
	client := p.client
	opts := jjvcs.DiffOptions{Git: true, Context: 3}
	if p.mode == modeStatus {
		path := p.selectedPath()
		if path != "" {
			opts.Paths = []string{path}
		}
	} else {
		if commit := p.selectedGraphCommit(); commit != nil {
			opts.Revision = commit.CommitID
		}
	}
	p.loadingDiff = true
	return func() tea.Msg {
		diff, err := client.Diff(workDir, opts)
		return DiffLoadedMsg{
			Epoch:   epoch,
			Content: diff,
			Err:     err,
		}
	}
}

func (p *Plugin) selectedPath() string {
	if p.statusCursor < 0 || p.statusCursor >= len(p.status.Changes) {
		return ""
	}
	return p.status.Changes[p.statusCursor].Path
}

func (p *Plugin) selectedGraphCommit() *jjvcs.StructuredCommit {
	if p.logCursor < 0 || p.logCursor >= len(p.structuredCommits) {
		return nil
	}
	return &p.structuredCommits[p.logCursor]
}

// renderLeftPaneContent renders the left pane: header + file list or log graph.
func (p *Plugin) renderLeftPaneContent(contentWidth, innerHeight int) string {
	var sb strings.Builder

	// Header: title + mode badges
	title := styles.Title.Render("Jujutsu") + " " + p.renderModeBadge()
	sb.WriteString(title)
	sb.WriteString("\n")

	// Subtitle with workspace root
	sb.WriteString(styles.Muted.Render(truncateStr(p.root, contentWidth)))
	sb.WriteString("\n")

	// Section header
	if p.mode == modeStatus {
		sectionTitle := fmt.Sprintf("Working Copy (%d)", len(p.status.Changes))
		sb.WriteString(styles.StatusModified.Render(sectionTitle))
	} else {
		sectionTitle := fmt.Sprintf("History (%d)", len(p.structuredCommits))
		sb.WriteString(styles.Subtitle.Render(sectionTitle))
	}
	sb.WriteString("\n")

	// Status/loading indicators
	if p.loadingStatus || p.loadingLog {
		sb.WriteString(styles.StatusInProgress.Render("Loading..."))
		sb.WriteString("\n")
	}
	if p.lastErr != nil {
		sb.WriteString(styles.StatusBlocked.Render(truncateStr(p.lastErr.Error(), contentWidth)))
		sb.WriteString("\n")
	}

	headerLines := lipgloss.Height(sb.String())
	listHeight := innerHeight - headerLines
	if listHeight < 1 {
		listHeight = 1
	}

	// Render file list or log graph
	if p.mode == modeStatus {
		sb.WriteString(p.renderStatusList(contentWidth, listHeight))
	} else {
		sb.WriteString(p.renderLogList(contentWidth, listHeight))
	}

	return sb.String()
}

// renderStatusList renders the file change list with colored status indicators.
func (p *Plugin) renderStatusList(width, height int) string {
	if len(p.status.Changes) == 0 {
		return styles.Muted.Render("Working copy clean")
	}

	p.clampStatusScroll(height)
	var lines []string
	end := minInt(len(p.status.Changes), p.statusScroll+height)
	for i := p.statusScroll; i < end; i++ {
		ch := p.status.Changes[i]
		label := kindLabel(ch.Kind)
		kindStyle := kindStyle(ch.Kind)
		badge := kindStyle.Render("[" + label + "]")

		path := truncateStr(ch.Path, width-6)

		if i == p.statusCursor {
			cursor := styles.ListCursor.Render(">")
			line := cursor + " " + badge + " " + styles.ListItemSelected.Render(path)
			lines = append(lines, line)
		} else {
			line := "  " + badge + " " + styles.ListItemNormal.Render(path)
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// renderLogList renders the commit log graph with selection highlighting.
func (p *Plugin) renderLogList(width, height int) string {
	leftFocused := p.activePane == paneLeft && p.focused
	return renderCustomLogList(p.graphRows, width, height, p.logScroll, p.logCursor, leftFocused)
}

// renderDiffPaneContent renders the diff pane with syntax highlighting.
func (p *Plugin) renderDiffPaneContent(contentWidth, innerHeight int) string {
	var sb strings.Builder

	// Diff pane header
	header := "Diff Preview"
	if p.mode == modeStatus {
		path := p.selectedPath()
		if path != "" {
			header = truncateStr(path, contentWidth-2)
		}
	} else if commit := p.selectedGraphCommit(); commit != nil {
		header = truncateStr("Commit "+commit.CommitID[:minInt(12, len(commit.CommitID))], contentWidth-2)
	}
	sb.WriteString(styles.Title.Render(header))
	sb.WriteString("\n")

	if p.loadingDiff {
		sb.WriteString(styles.StatusInProgress.Render("Loading diff..."))
		sb.WriteString("\n")
	}

	headerLines := lipgloss.Height(sb.String())
	diffHeight := innerHeight - headerLines
	if diffHeight < 1 {
		diffHeight = 1
	}

	if p.diff == "" {
		sb.WriteString(styles.Muted.Render("No diff"))
		return sb.String()
	}

	lines := strings.Split(p.diff, "\n")
	p.clampDiffScrollForLen(len(lines), diffHeight)
	end := minInt(len(lines), p.diffScroll+diffHeight)
	for i := p.diffScroll; i < end; i++ {
		line := lines[i]
		styled := styleDiffLine(line, contentWidth)
		sb.WriteString(styled)
		if i < end-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func (p *Plugin) handleMouse(msg tea.MouseMsg) (plugin.Plugin, tea.Cmd) {
	if p.mode != modeLog {
		return p, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return p, nil
	}

	// Heuristic offsets for panel/border/padding in current shell layout.
	for _, bodyStartY := range []int{3, 4, 5} {
		clickedLine := p.logScroll + (msg.Y - bodyStartY)
		if clickedLine < 0 || clickedLine >= len(p.graphRows) {
			continue
		}
		idx, ok := p.commitIndexAtLine(clickedLine)
		if !ok {
			continue
		}
		p.logCursor = idx
		p.ensureLogCursorVisible()
		commit := p.selectedGraphCommit()
		if commit == nil {
			return p, nil
		}
		now := time.Now()
		if p.lastClickCommitID == commit.CommitID && now.Sub(p.lastClickAt) < 450*time.Millisecond {
			p.lastClickCommitID = ""
			p.lastClickAt = time.Time{}
			return p, p.editSelectedCommit()
		}
		p.lastClickCommitID = commit.CommitID
		p.lastClickAt = now
		return p, p.loadSelectedDiff()
	}
	return p, nil
}

func (p *Plugin) openInFileBrowser(path string) tea.Cmd {
	return tea.Batch(
		app.FocusPlugin("file-browser"),
		func() tea.Msg {
			return filebrowser.NavigateToFileMsg{Path: path}
		},
	)
}

func (p *Plugin) toggleMode() {
	if p.mode == modeStatus {
		p.mode = modeLog
		p.diffScroll = 0
		return
	}
	p.mode = modeStatus
	p.diffScroll = 0
}

func (p *Plugin) renderModeBadge() string {
	active := lipgloss.NewStyle().
		Foreground(styles.TextSelectionColor).
		Background(styles.Primary).
		Bold(true).
		Padding(0, 1)
	inactive := lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Background(styles.BgSecondary).
		Padding(0, 1)
	if p.mode == modeStatus {
		return active.Render("STATUS") + " " + inactive.Render("LOG")
	}
	return inactive.Render("STATUS") + " " + active.Render("LOG")
}

func (p *Plugin) editSelectedCommit() tea.Cmd {
	commit := p.selectedGraphCommit()
	if commit == nil || p.ctx == nil {
		return nil
	}
	epoch := p.ctx.Epoch
	workDir := p.ctx.WorkDir
	client := p.client
	commitID := commit.CommitID
	return func() tea.Msg {
		err := client.Edit(workDir, commitID)
		return EditDoneMsg{Epoch: epoch, Err: err}
	}
}

// styleDiffLine applies syntax highlighting to a single diff line.
func styleDiffLine(line string, maxWidth int) string {
	if len([]rune(line)) > maxWidth && maxWidth > 3 {
		line = string([]rune(line)[:maxWidth-3]) + "..."
	}
	switch {
	case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
		return styles.DiffHeader.Render(line)
	case strings.HasPrefix(line, "@@"):
		return styles.DiffHeader.Render(line)
	case strings.HasPrefix(line, "diff "):
		return styles.DiffHeader.Render(line)
	case strings.HasPrefix(line, "+"):
		return styles.DiffAdd.Render(line)
	case strings.HasPrefix(line, "-"):
		return styles.DiffRemove.Render(line)
	default:
		return styles.DiffContext.Render(line)
	}
}

func kindLabel(k jjvcs.ChangeKind) string {
	switch k {
	case jjvcs.ChangeAdded:
		return "A"
	case jjvcs.ChangeModified:
		return "M"
	case jjvcs.ChangeDeleted:
		return "D"
	case jjvcs.ChangeRenamed:
		return "R"
	case jjvcs.ChangeCopied:
		return "C"
	default:
		return "?"
	}
}

// kindStyle returns a colored style for each change kind.
func kindStyle(k jjvcs.ChangeKind) lipgloss.Style {
	switch k {
	case jjvcs.ChangeAdded:
		return styles.StatusStaged
	case jjvcs.ChangeModified:
		return styles.StatusModified
	case jjvcs.ChangeDeleted:
		return styles.StatusDeleted
	case jjvcs.ChangeRenamed:
		return lipgloss.NewStyle().Foreground(styles.Info).Bold(true)
	case jjvcs.ChangeCopied:
		return lipgloss.NewStyle().Foreground(styles.Info).Bold(true)
	default:
		return styles.StatusUntracked
	}
}

func truncateStr(s string, maxWidth int) string {
	r := []rune(s)
	if len(r) > maxWidth && maxWidth > 3 {
		return string(r[:maxWidth-3]) + "..."
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampCursor(cursor, length int) int {
	if length <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= length {
		return length - 1
	}
	return cursor
}

func (p *Plugin) listPageSize() int {
	return max(1, p.height-6)
}

func (p *Plugin) diffPageSize() int {
	return max(1, p.height-4)
}

func (p *Plugin) ensureStatusCursorVisible() {
	page := p.listPageSize()
	if p.statusCursor < p.statusScroll {
		p.statusScroll = p.statusCursor
	}
	if p.statusCursor >= p.statusScroll+page {
		p.statusScroll = p.statusCursor - page + 1
	}
	p.clampStatusScroll(page)
}

// logMaxLanes returns the max lane count across all graph rows.
func (p *Plugin) logMaxLanes() int {
	maxLanes := 0
	for _, r := range p.graphRows {
		if len(r.Lanes) > maxLanes {
			maxLanes = len(r.Lanes)
		}
		if r.Column+1 > maxLanes {
			maxLanes = r.Column + 1
		}
	}
	return maxLanes
}

// logRenderWidth returns the content width used for log rendering.
func (p *Plugin) logRenderWidth() int {
	p.calculatePaneWidths()
	return p.leftPaneWidth - 4
}

func (p *Plugin) ensureLogCursorVisible() {
	page := p.listPageSize()
	w := p.logRenderWidth()
	ml := p.logMaxLanes()
	_, perRow := graphVisualLineCount(p.graphRows, w, ml)

	visIdx := 0
	commitLines := 0
	idx := 0
	for i, r := range p.graphRows {
		if r.Commit != nil {
			if idx == p.logCursor {
				commitLines = perRow[i]
				break
			}
			idx++
		}
		visIdx += perRow[i]
	}

	if visIdx < p.logScroll {
		p.logScroll = visIdx
	}
	if visIdx+commitLines > p.logScroll+page {
		p.logScroll = visIdx + commitLines - page
	}
	p.clampLogScroll(page)
}

// commitVisualLineIndex returns the visual line index for a commit,
// accounting for wrapped description lines.
func (p *Plugin) commitVisualLineIndex(commitIdx int) int {
	w := p.logRenderWidth()
	ml := p.logMaxLanes()
	_, perRow := graphVisualLineCount(p.graphRows, w, ml)

	idx := 0
	visLine := 0
	for i, r := range p.graphRows {
		if r.Commit != nil {
			if idx == commitIdx {
				return visLine
			}
			idx++
		}
		visLine += perRow[i]
	}
	return -1
}

// logVisualLineCount returns the total number of visual lines in the log.
func (p *Plugin) logVisualLineCount() int {
	w := p.logRenderWidth()
	ml := p.logMaxLanes()
	total, _ := graphVisualLineCount(p.graphRows, w, ml)
	return total
}

func (p *Plugin) commitIndexAtLine(rowIdx int) (int, bool) {
	idx := 0
	for i, r := range p.graphRows {
		if r.Commit != nil {
			if i == rowIdx {
				return idx, true
			}
			idx++
		}
	}
	return 0, false
}

func (p *Plugin) clampStatusScroll(page int) {
	maxScroll := max(0, len(p.status.Changes)-page)
	if p.statusScroll < 0 {
		p.statusScroll = 0
	}
	if p.statusScroll > maxScroll {
		p.statusScroll = maxScroll
	}
}

func (p *Plugin) clampLogScroll(page int) {
	maxScroll := max(0, p.logVisualLineCount()-page)
	if p.logScroll < 0 {
		p.logScroll = 0
	}
	if p.logScroll > maxScroll {
		p.logScroll = maxScroll
	}
}

func (p *Plugin) clampDiffScroll() {
	lines := strings.Split(p.diff, "\n")
	p.clampDiffScrollForLen(len(lines), p.diffPageSize())
}

func (p *Plugin) clampDiffScrollForLen(total, page int) {
	maxScroll := max(0, total-page)
	if p.diffScroll < 0 {
		p.diffScroll = 0
	}
	if p.diffScroll > maxScroll {
		p.diffScroll = maxScroll
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
