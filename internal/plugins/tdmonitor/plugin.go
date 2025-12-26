package tdmonitor

import (
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sst/sidecar/internal/plugin"
)

const (
	pluginID   = "td-monitor"
	pluginName = "TD Monitor"
	pluginIcon = "T"

	pollInterval = 2 * time.Second
	maxReadyIssues = 10
)

// View represents the current view mode.
type View int

const (
	ViewList View = iota
	ViewDetail
)

// Plugin implements the TD Monitor plugin.
type Plugin struct {
	ctx     *plugin.Context
	data    *DataProvider
	focused bool

	// Current view
	view View

	// Session info
	session *Session

	// Issue lists
	inProgress []Issue
	ready      []Issue
	reviewable []Issue

	// UI state
	cursor      int
	scrollOff   int
	activeList  string // "in_progress", "ready", "reviewable"
	showDetail  bool
	detailIssue *Issue

	// View dimensions
	width  int
	height int
}

// New creates a new TD Monitor plugin.
func New() *Plugin {
	return &Plugin{
		activeList: "ready",
	}
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
	p.data = NewDataProvider(ctx.WorkDir)

	if err := p.data.Open(); err != nil {
		return err // No .todos database, silent degradation
	}

	return nil
}

// Start begins plugin operation.
func (p *Plugin) Start() tea.Cmd {
	return tea.Batch(
		p.refresh(),
		p.tickCmd(),
	)
}

// Stop cleans up plugin resources.
func (p *Plugin) Stop() {
	if p.data != nil {
		p.data.Close()
	}
}

// Update handles messages.
func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if p.showDetail {
			return p.updateDetail(msg)
		}
		return p.updateList(msg)

	case RefreshMsg:
		return p, p.refresh()

	case TickMsg:
		return p, tea.Batch(p.refresh(), p.tickCmd())

	case DataLoadedMsg:
		p.session = msg.Session
		p.inProgress = msg.InProgress
		p.ready = msg.Ready
		p.reviewable = msg.Reviewable
		return p, nil

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
	}

	return p, nil
}

// updateList handles key events in the list view.
func (p *Plugin) updateList(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	list := p.activeListData()

	switch msg.String() {
	case "j", "down":
		if p.cursor < len(list)-1 {
			p.cursor++
			p.ensureCursorVisible()
		}

	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
			p.ensureCursorVisible()
		}

	case "g":
		p.cursor = 0
		p.scrollOff = 0

	case "G":
		if len(list) > 0 {
			p.cursor = len(list) - 1
			p.ensureCursorVisible()
		}

	case "tab":
		// Cycle through lists
		switch p.activeList {
		case "in_progress":
			p.activeList = "ready"
		case "ready":
			p.activeList = "reviewable"
		case "reviewable":
			p.activeList = "in_progress"
		}
		p.cursor = 0
		p.scrollOff = 0

	case "enter":
		if len(list) > 0 && p.cursor < len(list) {
			issue, err := p.data.IssueByID(list[p.cursor].ID)
			if err == nil && issue != nil {
				p.detailIssue = issue
				p.showDetail = true
			}
		}

	case "a":
		// Approve issue via td CLI
		if len(list) > 0 && p.cursor < len(list) {
			return p, p.runTDCommand("approve", list[p.cursor].ID)
		}

	case "x":
		// Delete issue via td CLI
		if len(list) > 0 && p.cursor < len(list) {
			return p, p.runTDCommand("delete", list[p.cursor].ID)
		}

	case "r":
		return p, p.refresh()
	}

	return p, nil
}

// updateDetail handles key events in the detail view.
func (p *Plugin) updateDetail(msg tea.KeyMsg) (plugin.Plugin, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		p.showDetail = false
		p.detailIssue = nil

	case "a":
		if p.detailIssue != nil {
			return p, p.runTDCommand("approve", p.detailIssue.ID)
		}

	case "x":
		if p.detailIssue != nil {
			return p, p.runTDCommand("delete", p.detailIssue.ID)
		}
	}

	return p, nil
}

// View renders the plugin.
func (p *Plugin) View(width, height int) string {
	p.width = width
	p.height = height

	if p.data == nil || p.data.db == nil {
		return renderNoDatabase()
	}

	if p.showDetail && p.detailIssue != nil {
		return p.renderDetail()
	}

	return p.renderList()
}

// IsFocused returns whether the plugin is focused.
func (p *Plugin) IsFocused() bool { return p.focused }

// SetFocused sets the focus state.
func (p *Plugin) SetFocused(f bool) { p.focused = f }

// Commands returns the available commands.
func (p *Plugin) Commands() []plugin.Command {
	return []plugin.Command{
		{ID: "approve-issue", Name: "Approve issue", Context: "td-monitor"},
		{ID: "delete-issue", Name: "Delete issue", Context: "td-monitor"},
	}
}

// FocusContext returns the current focus context.
func (p *Plugin) FocusContext() string {
	if p.showDetail {
		return "td-detail"
	}
	return "td-monitor"
}

// Diagnostics returns plugin health info.
func (p *Plugin) Diagnostics() []plugin.Diagnostic {
	status := "ok"
	detail := ""
	if p.data == nil || p.data.db == nil {
		status = "disabled"
		detail = "no database"
	} else {
		total := len(p.inProgress) + len(p.ready) + len(p.reviewable)
		detail = formatIssueCount(total)
	}
	return []plugin.Diagnostic{
		{ID: "td-monitor", Status: status, Detail: detail},
	}
}

// activeListData returns the currently active list.
func (p *Plugin) activeListData() []Issue {
	switch p.activeList {
	case "in_progress":
		return p.inProgress
	case "reviewable":
		return p.reviewable
	default:
		return p.ready
	}
}

// refresh reloads data from the database.
func (p *Plugin) refresh() tea.Cmd {
	return func() tea.Msg {
		if p.data == nil {
			return DataLoadedMsg{}
		}

		session, _ := p.data.CurrentSession()
		inProgress, _ := p.data.InProgressIssues()
		ready, _ := p.data.ReadyIssues(maxReadyIssues)
		reviewable, _ := p.data.ReviewableIssues()

		return DataLoadedMsg{
			Session:    session,
			InProgress: inProgress,
			Ready:      ready,
			Reviewable: reviewable,
		}
	}
}

// tickCmd returns a command that triggers periodic refresh.
func (p *Plugin) tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return TickMsg{}
	})
}

// runTDCommand executes a td CLI command.
func (p *Plugin) runTDCommand(action, issueID string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("td", action, issueID)
		cmd.Dir = p.ctx.WorkDir
		if err := cmd.Run(); err != nil {
			return ErrorMsg{Err: err}
		}
		return RefreshMsg{}
	}
}

// ensureCursorVisible adjusts scroll to keep cursor visible.
func (p *Plugin) ensureCursorVisible() {
	visibleRows := p.height - 8
	if visibleRows < 1 {
		visibleRows = 1
	}

	if p.cursor < p.scrollOff {
		p.scrollOff = p.cursor
	} else if p.cursor >= p.scrollOff+visibleRows {
		p.scrollOff = p.cursor - visibleRows + 1
	}
}

// formatIssueCount formats an issue count.
func formatIssueCount(n int) string {
	if n == 1 {
		return "1 issue"
	}
	return fmt.Sprintf("%d issues", n)
}

// Message types
type RefreshMsg struct{}
type TickMsg struct{}
type ErrorMsg struct{ Err error }

type DataLoadedMsg struct {
	Session    *Session
	InProgress []Issue
	Ready      []Issue
	Reviewable []Issue
}
