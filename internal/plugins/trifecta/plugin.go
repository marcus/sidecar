package trifecta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/trifectaindex"
)

const (
	pluginID   = "trifecta"
	pluginName = "Trifecta"
	pluginIcon = "T"
)

// Plugin implements plugin.Plugin interface
type Plugin struct {
	ctx     *plugin.Context
	focused bool
	width   int
	height  int

	// Data
	index       *trifectaindex.WOIndex
	indexLoaded bool
	lastError   string
	lastLoad    time.Time

	// View state
	cursor       int
	filterStatus trifectaindex.WOStatus
}

func New() *Plugin {
	return &Plugin{
		filterStatus: trifectaindex.WOStatus(""), // No filter
	}
}

// plugin.Plugin interface
func (p *Plugin) ID() string   { return pluginID }
func (p *Plugin) Name() string { return pluginName }
func (p *Plugin) Icon() string { return pluginIcon }

func (p *Plugin) Init(ctx *plugin.Context) error {
	p.ctx = ctx
	return p.loadIndex()
}

func (p *Plugin) Start() tea.Cmd {
	return nil
}

// statusColorMap maps WO statuses to their display colors
var statusColorMap = map[trifectaindex.WOStatus]lipgloss.Color{
	trifectaindex.WOStatusRunning: lipgloss.Color("blue"),
	trifectaindex.WOStatusDone:    lipgloss.Color("green"),
	trifectaindex.WOStatusFailed:  lipgloss.Color("red"),
	// Default: gray (used for pending)
}

func (p *Plugin) Stop() {}

func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		return p, p.handleKey(m)
	}
	return p, nil
}

func (p *Plugin) View(width, height int) string {
	p.width, p.height = width, height

	if !p.indexLoaded {
		return p.renderError()
	}
	return p.renderList()
}

func (p *Plugin) IsFocused() bool         { return p.focused }
func (p *Plugin) SetFocused(focused bool) { p.focused = focused }

// Commands returns available commands for command palette
func (p *Plugin) Commands() []plugin.Command {
	return nil // No custom commands for now
}

// FocusContext returns context string for focus indication
func (p *Plugin) FocusContext() string {
	return "Trifecta Work Orders"
}

func (p *Plugin) IndexFilePath() string {
	return filepath.Join(p.ctx.WorkDir, "_ctx", "index", trifectaindex.IndexFilename)
}

func (p *Plugin) loadIndex() error {
	path := p.IndexFilePath()
	index, err := trifectaindex.LoadAndValidate(path, p.ctx.WorkDir)
	if err != nil {
		p.lastError = fmt.Sprintf("%v", err)
		return err
	}

	p.index = index
	p.indexLoaded = true
	p.lastError = ""
	p.lastLoad = time.Now()

	// Reset cursor if out of bounds
	p.resetCursor()
	return nil
}

func (p *Plugin) filteredWorkOrders() []trifectaindex.WorkOrder {
	if p.index == nil {
		return nil
	}

	var filtered []trifectaindex.WorkOrder
	for _, wo := range p.index.WorkOrders {
		if p.filterStatus == "" || wo.Status == p.filterStatus {
			filtered = append(filtered, wo)
		}
	}
	return filtered
}

func (p *Plugin) resetCursor() {
	filtered := p.filteredWorkOrders()
	if p.cursor >= len(filtered) {
		p.cursor = len(filtered) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (p *Plugin) selectedWorkOrder() *trifectaindex.WorkOrder {
	filtered := p.filteredWorkOrders()
	if p.cursor >= 0 && p.cursor < len(filtered) {
		return &filtered[p.cursor]
	}
	return nil
}

// Keybindings (sin colisiones)
func (p *Plugin) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "esc":
		return tea.Quit

	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		filtered := p.filteredWorkOrders()
		if p.cursor < len(filtered)-1 {
			p.cursor++
		}

	case "p": // Filter pending
		p.filterStatus = trifectaindex.WOStatusPending
		p.cursor = 0
	case "r": // Filter running (lowercase r)
		p.filterStatus = trifectaindex.WOStatusRunning
		p.cursor = 0
	case "R": // Refresh (uppercase R = Shift+r)
		_ = p.loadIndex()
	case "d": // Filter done
		p.filterStatus = trifectaindex.WOStatusDone
		p.cursor = 0
	case "f": // Filter failed
		p.filterStatus = trifectaindex.WOStatusFailed
		p.cursor = 0
	case "a": // All
		p.filterStatus = trifectaindex.WOStatus("")
		p.cursor = 0

	case "o": // Open YAML in editor
		return p.handleOpenYAML()
	}

	return nil
}

func (p *Plugin) handleOpenYAML() tea.Cmd {
	wo := p.selectedWorkOrder()
	if wo == nil {
		return nil
	}

	yamlAbs := filepath.Join(p.ctx.WorkDir, wo.WOYAMLPath)
	if _, err := os.Stat(yamlAbs); os.IsNotExist(err) {
		p.lastError = fmt.Sprintf("YAML not found: %s", wo.WOYAMLPath)
		return nil
	}

	// TODO: Import enterInlineEditMode from filebrowser for real tmux editor
	// Current implementation just logs/shows YAML path
	p.lastError = fmt.Sprintf("Opening YAML: %s", yamlAbs)
	return nil
}

func (p *Plugin) renderList() string {
	filtered := p.filteredWorkOrders()

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Trifecta Work Orders"))
	sb.WriteString("\n\n")

	if len(filtered) == 0 {
		sb.WriteString("No work orders found.")
		return sb.String()
	}

	for i, wo := range filtered {
		cursor := " "
		if i == p.cursor {
			cursor = ">"
		}

		// Get status color from map (default to gray if not found)
		statusColor, ok := statusColorMap[wo.Status]
		if !ok {
			statusColor = lipgloss.Color("gray")
		}

		line := fmt.Sprintf("%s [%s] %s %s\n",
			cursor,
			lipgloss.NewStyle().Foreground(statusColor).Render(string(wo.Status)),
			wo.ID,
			wo.Title,
		)
		sb.WriteString(line)
	}

	// Footer
	if p.filterStatus != "" {
		sb.WriteString(fmt.Sprintf("\nFilter: %s (a=clear)", p.filterStatus))
	}
	if !p.lastLoad.IsZero() {
		sb.WriteString(fmt.Sprintf("\nLast update: %s", p.lastLoad.Format("15:04:05")))
	}
	sb.WriteString("\n\nKeys: R=refresh, r=running, o=show YAML path, p/d/f=filter")

	return sb.String()
}

func (p *Plugin) renderError() string {
	var sb strings.Builder
	sb.WriteString("Trifecta Work Orders\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("red")).Render("Error: "))
	sb.WriteString(p.lastError)
	sb.WriteString("\n\nPress 'R' to retry loading index")
	return sb.String()
}
