# Plan: Embed TD Monitor in Sidecar

## Summary

**Approach**: Export td's monitor as a reusable package, wrap in thin sidecar plugin
**Effort**: ~50 lines in td, ~100 lines in sidecar (vs ~2000 lines to rebuild)
**Result**: Full feature parity, single source of truth, sidecar theme integration

---

## Recommendation: **Extract & Wrap** (not rebuild)

Instead of maintaining two separate implementations (~2000 lines in sidecar duplicating ~3000 lines in td), extract td's monitor as a reusable package and wrap it in a thin sidecar plugin.

### Why This Works

Both systems use Bubble Tea with compatible patterns:

| Aspect   | td monitor             | sidecar plugin      |
| -------- | ---------------------- | ------------------- |
| Update   | `(tea.Model, tea.Cmd)` | `(Plugin, tea.Cmd)` |
| View     | `string`               | `string`            |
| Messages | `tea.Msg`              | `tea.Msg`           |
| Styling  | lipgloss               | lipgloss            |

A sidecar plugin can wrap a `tea.Model` internally and delegate to it.

### Benefits

1. **Single source of truth** - no feature drift
2. **Full parity automatically** - 3-panel layout, search, stats, modals all work
3. **Live updates** - td monitor already has tick-based refresh
4. **Maintenance** - fix bugs once, both benefit
5. **~50 lines vs ~2000 lines** in sidecar

---

## Implementation Plan

### Phase 1: Export td monitor as package (in ~/code/td)

**File: `internal/tui/monitor/monitor.go`** (new)

```go
package monitor

// Config allows customization when embedded
type Config struct {
    DB            *db.DB
    RefreshInterval time.Duration
    Width, Height int
}

// New creates an embeddable monitor model
func New(cfg Config) *Model {
    // existing NewModel logic with config
}

// Model is now exported (rename model -> Model)
```

**Changes to `model.go`:**

- Export `Model` struct (capitalize)
- Export key methods: `Init()`, `Update()`, `View()`
- Make `db` field settable via Config
- Keep internal state private

### Phase 2: Thin wrapper plugin in sidecar

**File: `internal/plugins/tdmonitor/plugin.go`** (replace existing)

```go
package tdmonitor

import (
    tea "github.com/charmbracelet/bubbletea"
    "github.com/marcus/td/internal/tui/monitor"
    "github.com/marcus/td/internal/db"
    "github.com/sst/sidecar/internal/plugin"
)

type Plugin struct {
    ctx     *plugin.Context
    monitor *monitor.Model
    focused bool
    width, height int
}

func (p *Plugin) Init(ctx *plugin.Context) error {
    p.ctx = ctx

    // Open td database (same path td uses)
    database, err := db.Open(filepath.Join(ctx.WorkDir, ".todos", "issues.db"))
    if err != nil {
        return err // silent degradation handled by registry
    }

    p.monitor = monitor.New(monitor.Config{
        DB: database,
        RefreshInterval: 2 * time.Second,
    })
    return nil
}

func (p *Plugin) Start() tea.Cmd {
    return p.monitor.Init()
}

func (p *Plugin) Update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
    // Handle window size
    if wsm, ok := msg.(tea.WindowSizeMsg); ok {
        p.width, p.height = wsm.Width, wsm.Height
    }

    // Delegate to monitor
    newModel, cmd := p.monitor.Update(msg)
    p.monitor = newModel.(*monitor.Model)
    return p, cmd
}

func (p *Plugin) View(width, height int) string {
    p.monitor.SetSize(width, height)
    return p.monitor.View()
}

// ... other interface methods (ID, Name, Icon, etc.)
```

### Phase 3: Add go.mod dependency

**In sidecar's `go.mod`:**

```
require github.com/marcus/td v0.x.x
```

Or use replace directive for local development:

```
replace github.com/marcus/td => ../td
```

---

## Required Changes to td

| File                              | Change                                          |
| --------------------------------- | ----------------------------------------------- |
| `internal/tui/monitor/model.go`   | Export `Model`, add `SetSize()` method          |
| `internal/tui/monitor/monitor.go` | New file with `Config` and `New()`              |
| `internal/db/db.go`               | Ensure `Open()` is exported (likely already is) |

Estimated: ~50 lines changed in td

---

## Required Changes to sidecar

| File                                   | Change                                 |
| -------------------------------------- | -------------------------------------- |
| `internal/plugins/tdmonitor/plugin.go` | Replace with thin wrapper (~100 lines) |
| `internal/plugins/tdmonitor/view.go`   | Delete (not needed)                    |
| `internal/plugins/tdmonitor/data.go`   | Delete (not needed)                    |
| `internal/plugins/tdmonitor/types.go`  | Delete (not needed)                    |
| `go.mod`                               | Add td dependency                      |

Estimated: Net reduction of ~400 lines

---

## Alternative Considered: Full Rebuild

The original plan (`docs/spec-td-monitor-plugin.md`) proposed rebuilding all features:

- ~500 lines in data.go
- ~800 lines in view.go
- ~400 lines in plugin.go
- ~100 lines in types.go
- ~200 lines in tests

**Total: ~2000 new lines to maintain separately**

This creates:

- Duplicate code
- Feature drift risk
- Double maintenance burden
- Potential inconsistencies

---

## Decisions

1. **Dependency**: td is published as a Go module - import directly
2. **Styling**: Adapt to sidecar theme via configurable styles (low complexity)

---

## Style Customization Approach

Add a `StyleConfig` to td monitor that defaults to current colors but allows override:

**In td (`internal/tui/monitor/styles.go`):**

```go
type StyleConfig struct {
    Primary    lipgloss.Color
    Secondary  lipgloss.Color
    Success    lipgloss.Color
    Warning    lipgloss.Color
    Error      lipgloss.Color
    Muted      lipgloss.Color
    Background lipgloss.Color
}

func DefaultStyles() StyleConfig {
    return StyleConfig{
        Primary:   "#7C3AED",
        // ... current td colors
    }
}
```

**In Config:**

```go
type Config struct {
    DB              *db.DB
    RefreshInterval time.Duration
    Styles          *StyleConfig // nil = use defaults
}
```

**In sidecar plugin:**

```go
p.monitor = monitor.New(monitor.Config{
    DB: database,
    Styles: &monitor.StyleConfig{
        Primary: styles.Primary,  // from sidecar/internal/styles
        // ... map sidecar colors
    },
})
```

This is ~20 extra lines in td, makes both codebases happy.
