package projects

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/td/pkg/monitor"
)

const (
	pluginID   = "projects-dashboard"
	pluginName = "projects"
	pluginIcon = "P"

	pollInterval = 5 * time.Second
)

// Plugin displays a dashboard of all configured projects with their td stats.
type Plugin struct {
	ctx     *plugin.Context
	focused bool

	width  int
	height int

	// Project data
	projects []ProjectEntry
	cursor   int
	scroll   int

	// Detail modal state
	showDetail    bool
	detailProject *ProjectEntry

	// Scan state
	scanning    bool
	scanResults []ScanResult
	showScan    bool
	scanCursor  int
}

// New creates a new Projects Dashboard plugin.
func New() *Plugin {
	return &Plugin{}
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return pluginID }

// Name returns the plugin display name.
func (p *Plugin) Name() string { return pluginName }

// Icon returns the plugin icon character.
func (p *Plugin) Icon() string { return pluginIcon }

// Init initializes the plugin with context.
func (p *Plugin) Init(ctx *plugin.Context) error {
	p.ctx = ctx

	// Clear stale state from previous project
	p.projects = nil
	p.cursor = 0
	p.scroll = 0
	p.showDetail = false
	p.detailProject = nil
	p.scanning = false
	p.scanResults = nil
	p.showScan = false

	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	return tea.Batch(
		p.fetchProjects(),
		p.scheduleTick(),
	)
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	p.projects = nil
}

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, nil

	case plugin.PluginFocusedMsg:
		return p, nil

	case tickMsg:
		return p, tea.Batch(p.fetchProjects(), p.scheduleTick())

	case refreshDataMsg:
		p.projects = msg.entries
		return p, nil

	case scanResultMsg:
		p.scanning = false
		p.scanResults = msg.results
		if len(msg.results) == 0 {
			return p, func() tea.Msg {
				return app.ToastMsg{
					Message:  "No new td-initialized projects found",
					Duration: 3 * time.Second,
				}
			}
		}
		p.showScan = true
		p.scanCursor = 0
		return p, nil

	case tea.KeyMsg:
		return p.handleKey(msg)
	}

	return p, nil
}

func (p *Plugin) handleKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	// Handle scan results modal
	if p.showScan {
		return p.handleScanKey(msg)
	}

	// Handle detail modal
	if p.showDetail {
		return p.handleDetailKey(msg)
	}

	key := msg.String()
	switch key {
	case "j", "down":
		if p.cursor < len(p.projects)-1 {
			p.cursor++
			p.ensureCursorVisible()
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisible()
		}
	case "G":
		p.cursor = len(p.projects) - 1
		p.ensureCursorVisible()
	case "enter":
		if p.cursor >= 0 && p.cursor < len(p.projects) {
			entry := p.projects[p.cursor]
			p.detailProject = &entry
			p.showDetail = true
		}
	case "@":
		if p.cursor >= 0 && p.cursor < len(p.projects) {
			path := p.projects[p.cursor].Path
			return p, app.SwitchProject(path)
		}
	case "s":
		if !p.scanning {
			p.scanning = true
			return p, p.scanForProjects()
		}
	case "a":
		return p, func() tea.Msg {
			return app.ToastMsg{
				Message:  "Use @ to open project switcher, then 'a' to add",
				Duration: 3 * time.Second,
			}
		}
	case "r":
		return p, p.fetchProjects()
	case "d":
		if p.cursor >= 0 && p.cursor < len(p.projects) {
			return p, p.removeProject(p.projects[p.cursor])
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(key[0]-'0') - 1
		if idx >= 0 && idx < len(p.projects) {
			path := p.projects[idx].Path
			return p, app.SwitchProject(path)
		}
	}

	return p, nil
}

func (p *Plugin) handleDetailKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		p.showDetail = false
		p.detailProject = nil
	case "@":
		if p.detailProject != nil {
			path := p.detailProject.Path
			p.showDetail = false
			p.detailProject = nil
			return p, app.SwitchProject(path)
		}
	}
	return p, nil
}

func (p *Plugin) handleScanKey(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.showScan = false
		p.scanResults = nil
	case "enter", "y":
		// Add all scan results to config
		return p, p.addScanResults()
	case "j", "down":
		if p.scanCursor < len(p.scanResults)-1 {
			p.scanCursor++
		}
	case "k", "up":
		if p.scanCursor > 0 {
			p.scanCursor--
		}
	}
	return p, nil
}

// View renders the plugin.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height

	if len(p.projects) == 0 {
		content := p.renderEmpty()
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
	}

	var content string
	if p.showScan {
		content = p.renderScanModal()
	} else if p.showDetail && p.detailProject != nil {
		content = p.renderDetail()
	} else {
		content = p.renderList()
	}

	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
}

func (p *Plugin) renderEmpty() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).Render("  No projects configured."))
	sb.WriteString("\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).Render("  Press "))
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.Primary).Bold(true).Render("s"))
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).Render(" to scan for td-initialized projects"))
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).Render("  or "))
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.Primary).Bold(true).Render("@"))
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).Render(" to open the project switcher"))
	return sb.String()
}

func (p *Plugin) renderList() string {
	var sb strings.Builder

	// Header row
	headerStyle := lipgloss.NewStyle().Foreground(styles.TextMuted).Bold(true)
	sb.WriteString(headerStyle.Render(fmt.Sprintf("  %2s  %-20s  %-8s  %4s  %4s  %4s  %4s  %s",
		"#", "Project", "Status", "Open", "WIP", "Blkd", "Rev", "Last Activity")))
	sb.WriteString("\n")

	// Separator
	sepStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
	sep := strings.Repeat("─", min(p.width-2, 90))
	sb.WriteString(sepStyle.Render("  " + sep))
	sb.WriteString("\n")

	// Calculate visible rows (height minus header, separator, and some padding)
	visibleRows := p.height - 4
	if visibleRows < 1 {
		visibleRows = 1
	}

	for i := p.scroll; i < len(p.projects) && i < p.scroll+visibleRows; i++ {
		entry := p.projects[i]
		isSelected := i == p.cursor

		// Status indicator
		status, statusColor := p.projectStatus(entry)

		// Format last activity
		lastActivity := p.formatLastActivity(entry.Summary.LastActivity)

		line := fmt.Sprintf("  %2d  %-20s  %s %-6s  %4d  %4d  %4d  %4d  %s",
			entry.Index,
			truncate(entry.Name, 20),
			lipgloss.NewStyle().Foreground(statusColor).Render("●"),
			status,
			entry.Summary.OpenCount,
			entry.Summary.InProgressCount,
			entry.Summary.BlockedCount,
			entry.Summary.ReviewableCount,
			lastActivity,
		)

		if !entry.HasTD {
			line = fmt.Sprintf("  %2d  %-20s  %s %-6s  %4s  %4s  %4s  %4s  %s",
				entry.Index,
				truncate(entry.Name, 20),
				lipgloss.NewStyle().Foreground(styles.TextMuted).Render("○"),
				"no td",
				"-", "-", "-", "-",
				"-",
			)
		}

		if isSelected {
			sb.WriteString(lipgloss.NewStyle().
				Background(styles.BgSecondary).
				Foreground(styles.TextPrimary).
				Bold(true).
				Width(p.width).
				Render(line))
		} else {
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (p *Plugin) renderDetail() string {
	entry := p.detailProject
	if entry == nil {
		return ""
	}

	var sb strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Primary).
		Padding(1, 2)
	sb.WriteString(titleStyle.Render(entry.Name))
	sb.WriteString("\n")

	// Path
	pathStyle := lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Padding(0, 2)
	sb.WriteString(pathStyle.Render(entry.Path))
	sb.WriteString("\n\n")

	if !entry.HasTD {
		sb.WriteString(lipgloss.NewStyle().Padding(0, 2).Foreground(styles.Warning).Render("  td not initialized in this project"))
		sb.WriteString("\n")
	} else {
		s := entry.Summary
		labelStyle := lipgloss.NewStyle().Foreground(styles.TextMuted).Width(16)
		valueStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		activeStyle := lipgloss.NewStyle().Foreground(styles.Success)
		warnStyle := lipgloss.NewStyle().Foreground(styles.Warning)

		sb.WriteString("  " + labelStyle.Render("Open:") + valueStyle.Render(fmt.Sprintf("%d", s.OpenCount)) + "\n")
		sb.WriteString("  " + labelStyle.Render("In Progress:") + activeStyle.Render(fmt.Sprintf("%d", s.InProgressCount)) + "\n")
		sb.WriteString("  " + labelStyle.Render("Blocked:"))
		if s.BlockedCount > 0 {
			sb.WriteString(warnStyle.Render(fmt.Sprintf("%d", s.BlockedCount)))
		} else {
			sb.WriteString(valueStyle.Render("0"))
		}
		sb.WriteString("\n")
		sb.WriteString("  " + labelStyle.Render("In Review:") + valueStyle.Render(fmt.Sprintf("%d", s.ReviewableCount)) + "\n")
		sb.WriteString("  " + labelStyle.Render("Closed:") + valueStyle.Render(fmt.Sprintf("%d", s.ClosedCount)) + "\n")
		sb.WriteString("  " + labelStyle.Render("Total:") + valueStyle.Render(fmt.Sprintf("%d", s.TotalCount)) + "\n")
		sb.WriteString("\n")

		if s.FocusedIssue != nil {
			sb.WriteString("  " + labelStyle.Render("Focused:") + valueStyle.Render(truncate(s.FocusedIssue.Title, 50)) + "\n")
		}

		sb.WriteString("  " + labelStyle.Render("Last Activity:") + valueStyle.Render(p.formatLastActivity(s.LastActivity)) + "\n")
	}

	sb.WriteString("\n")
	hintStyle := lipgloss.NewStyle().Foreground(styles.TextMuted).Padding(0, 2)
	sb.WriteString(hintStyle.Render("esc: back  @: switch to project"))

	return sb.String()
}

func (p *Plugin) renderScanModal() string {
	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Primary).Padding(1, 2)
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Found %d new project(s)", len(p.scanResults))))
	sb.WriteString("\n\n")

	for i, r := range p.scanResults {
		prefix := "  "
		if i == p.scanCursor {
			prefix = "> "
		}
		nameStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		pathStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
		sb.WriteString(prefix + nameStyle.Render(r.Name) + " " + pathStyle.Render(r.Path) + "\n")
	}

	sb.WriteString("\n")
	hintStyle := lipgloss.NewStyle().Foreground(styles.TextMuted).Padding(0, 2)
	sb.WriteString(hintStyle.Render("enter/y: add all to config  esc: cancel"))

	return sb.String()
}

func (p *Plugin) projectStatus(entry ProjectEntry) (string, lipgloss.Color) {
	if !entry.HasTD {
		return "no td", styles.TextMuted
	}
	s := entry.Summary
	if s.BlockedCount > 0 && s.InProgressCount == 0 {
		return "stuck", styles.Error
	}
	if s.BlockedCount > 0 {
		return "warn", styles.Warning
	}
	if s.InProgressCount > 0 {
		return "active", styles.Success
	}
	if s.OpenCount > 0 {
		return "ready", styles.Primary
	}
	if !s.LastActivity.IsZero() {
		age := time.Since(s.LastActivity)
		if age > 7*24*time.Hour {
			return "idle", styles.TextMuted
		}
	}
	return "idle", styles.TextMuted
}

func (p *Plugin) formatLastActivity(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}

func (p *Plugin) ensureCursorVisible() {
	visibleRows := p.height - 4
	if visibleRows < 1 {
		visibleRows = 1
	}
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	}
	if p.cursor >= p.scroll+visibleRows {
		p.scroll = p.cursor - visibleRows + 1
	}
}

// fetchProjects loads project data asynchronously.
// It merges projects from config.json with auto-discovered td-initialized
// repos found by scanning configured scanDirs (or the working directory's
// parent if none configured).
func (p *Plugin) fetchProjects() tea.Cmd {
	cfg := p.ctx.Config
	workDir := p.ctx.WorkDir
	return func() tea.Msg {
		// Collect unique projects keyed by absolute path
		seen := make(map[string]bool)
		var entries []ProjectEntry

		// 1. Add projects from config.json
		for _, proj := range cfg.Projects.List {
			absPath, err := filepath.Abs(config.ExpandPath(proj.Path))
			if err != nil {
				absPath = proj.Path
			}
			if seen[absPath] {
				continue
			}
			seen[absPath] = true

			entry := ProjectEntry{
				Name: proj.Name,
				Path: absPath,
			}

			db, err := monitor.OpenDB(absPath)
			if err != nil {
				entry.HasTD = false
			} else {
				entry.HasTD = true
				entry.Summary = monitor.FetchProjectSummary(db)
				_ = monitor.CloseDB(absPath)
			}

			entries = append(entries, entry)
		}

		// 2. Auto-discover td-initialized repos from scan directories
		scanDirs := cfg.Plugins.Projects.ScanDirs
		if len(scanDirs) == 0 {
			// Default: scan parent of working directory
			scanDirs = []string{filepath.Dir(workDir)}
		}
		discovered := ScanForProjects(scanDirs, nil) // pass nil — we dedup via seen map
		for _, d := range discovered {
			if seen[d.Path] {
				continue
			}
			seen[d.Path] = true

			entry := ProjectEntry{
				Name: d.Name,
				Path: d.Path,
			}

			db, err := monitor.OpenDB(d.Path)
			if err != nil {
				entry.HasTD = false
			} else {
				entry.HasTD = true
				entry.Summary = monitor.FetchProjectSummary(db)
				_ = monitor.CloseDB(d.Path)
			}

			entries = append(entries, entry)
		}

		// Assign 1-based indices after merging
		for i := range entries {
			entries[i].Index = i + 1
		}

		return refreshDataMsg{entries: entries}
	}
}

func (p *Plugin) scheduleTick() tea.Cmd {
	interval := p.ctx.Config.Plugins.Projects.RefreshInterval
	if interval <= 0 {
		interval = pollInterval
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (p *Plugin) scanForProjects() tea.Cmd {
	cfg := p.ctx.Config
	return func() tea.Msg {
		dirs := cfg.Plugins.Projects.ScanDirs
		if len(dirs) == 0 {
			// Default: scan parent of current working directory
			dirs = []string{cfg.Projects.Root}
		}
		results := ScanForProjects(dirs, cfg.Projects.List)
		return scanResultMsg{results: results}
	}
}

func (p *Plugin) addScanResults() tea.Cmd {
	results := p.scanResults
	p.showScan = false
	p.scanResults = nil

	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return app.ToastMsg{
				Message:  "Failed to load config: " + err.Error(),
				Duration: 3 * time.Second,
				IsError:  true,
			}
		}

		for _, r := range results {
			cfg.Projects.List = append(cfg.Projects.List, config.ProjectConfig{
				Name: r.Name,
				Path: r.Path,
			})
		}

		if err := config.Save(cfg); err != nil {
			return app.ToastMsg{
				Message:  "Failed to save config: " + err.Error(),
				Duration: 3 * time.Second,
				IsError:  true,
			}
		}

		return app.ToastMsg{
			Message:  fmt.Sprintf("Added %d project(s) to config", len(results)),
			Duration: 3 * time.Second,
		}
	}
}

func (p *Plugin) removeProject(entry ProjectEntry) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			cfg, err := config.Load()
			if err != nil {
				return app.ToastMsg{
					Message:  "Failed to load config: " + err.Error(),
					Duration: 3 * time.Second,
					IsError:  true,
				}
			}

			// Check if this project is in the config (vs auto-discovered)
			found := false
			newList := make([]config.ProjectConfig, 0, len(cfg.Projects.List))
			for _, proj := range cfg.Projects.List {
				absPath, _ := filepath.Abs(config.ExpandPath(proj.Path))
				if absPath == entry.Path {
					found = true
				} else {
					newList = append(newList, proj)
				}
			}

			if !found {
				return app.ToastMsg{
					Message:  fmt.Sprintf("%s is auto-discovered and can't be removed", entry.Name),
					Duration: 3 * time.Second,
				}
			}

			cfg.Projects.List = newList

			if err := config.Save(cfg); err != nil {
				return app.ToastMsg{
					Message:  "Failed to save config: " + err.Error(),
					Duration: 3 * time.Second,
					IsError:  true,
				}
			}

			return app.ToastMsg{
				Message:  fmt.Sprintf("Removed project: %s", entry.Name),
				Duration: 3 * time.Second,
			}
		},
		p.fetchProjects(),
	)
}

// IsFocused returns whether the plugin is focused.
func (p *Plugin) IsFocused() bool { return p.focused }

// SetFocused sets the focus state.
func (p *Plugin) SetFocused(f bool) { p.focused = f }

// Commands returns the available commands for the footer.
func (p *Plugin) Commands() []plugin.Command {
	return []plugin.Command{
		{ID: "open-details", Name: "Details", Description: "View project details", Context: "projects-dashboard", Priority: 1, Category: plugin.CategoryView},
		{ID: "switch-to-project", Name: "Switch", Description: "Switch to selected project", Context: "projects-dashboard", Priority: 2, Category: plugin.CategoryActions},
		{ID: "scan-projects", Name: "Scan", Description: "Scan for td-initialized projects", Context: "projects-dashboard", Priority: 3, Category: plugin.CategoryActions},
		{ID: "refresh", Name: "Refresh", Description: "Refresh project stats", Context: "projects-dashboard", Priority: 4, Category: plugin.CategoryActions},
		{ID: "remove-project", Name: "Remove", Description: "Remove project from config", Context: "projects-dashboard", Priority: 5, Category: plugin.CategoryActions},
	}
}

// FocusContext returns the current keyboard context.
func (p *Plugin) FocusContext() string {
	if p.showDetail {
		return "projects-detail"
	}
	return "projects-dashboard"
}

// ConsumesTextInput reports whether the plugin is in a text-entry context.
func (p *Plugin) ConsumesTextInput() bool {
	return false
}

// Diagnostics returns plugin health info.
func (p *Plugin) Diagnostics() []plugin.Diagnostic {
	detail := fmt.Sprintf("%d projects", len(p.projects))
	return []plugin.Diagnostic{
		{ID: pluginID, Status: "ok", Detail: detail},
	}
}

// Helper functions

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
