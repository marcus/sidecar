# Project Switcher Developer Guide

Implementation guide for the project switcher modal (`@` hotkey).

## Architecture Overview

The project switcher uses the app's declarative **modal library** (`internal/modal/`) for rendering and mouse handling. This abstracts away raw coordinate calculations and provides a sectioned builder pattern for content.

| Component | File | Line | Purpose |
|-----------|------|------|---------|
| Model state | `model.go` | 104-112 | Modal visibility, cursor, scroll, filter, modal cache |
| resetProjectSwitcher | `model.go` | 413 | State cleanup when modal closes |
| initProjectSwitcher | `model.go` | 433 | State initialization when modal opens |
| filterProjects | `model.go` | 457 | Case-insensitive project filtering |
| switchProject | `model.go` | 485 | Plugin context reinitialization |
| previewProjectTheme | `model.go` | 580 | Live theme preview during selection |
| ensureProjectSwitcherModal | `view.go` | 114 | Lazy modal building/caching |
| projectSwitcherItemID | `view.go` | 29 | Mouse click ID generation |
| Keyboard handling | `update.go` | 600-690 | Key event processing |
| Mouse handling | `update.go` | 1073-1115 | Mouse event processing via modal library |
| View rendering | `view.go` | 139-358 | Sectioned modal content |

## Modal Library Integration

The project switcher uses `internal/modal/` for declarative modal rendering. Key concepts:

### Modal Builder Pattern

```go
m.projectSwitcherModal = modal.New("Switch Project",
    modal.WithWidth(modalW),
    modal.WithHints(false),
).
    AddSection(m.projectSwitcherInputSection()).   // Filter input
    AddSection(m.projectSwitcherCountSection()).   // "N of M projects"
    AddSection(m.projectSwitcherListSection()).    // Scrollable project list
    AddSection(m.projectSwitcherHintsSection())    // Keyboard hints
```

### Section Types

| Type | Factory | Purpose |
|------|---------|---------|
| Input | `modal.Input()` | Text input with focus handling |
| Custom | `modal.Custom()` | Escape hatch for complex content |
| Text | `modal.Text()` | Static text |
| Buttons | `modal.Buttons()` | Button row |

### Custom Section Pattern

Custom sections receive `contentWidth`, `focusID`, and `hoverID` for context-aware rendering:

```go
modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
    // Render content
    // Return focusables for mouse hit regions
    return modal.RenderedSection{
        Content:    renderedString,
        Focusables: []modal.FocusableInfo{...},
    }
}, updateFn)
```

## Model State

```go
// internal/app/model.go:104-112

// Project switcher modal
showProjectSwitcher         bool
projectSwitcherCursor       int
projectSwitcherScroll       int // scroll offset for list
projectSwitcherInput        textinput.Model
projectSwitcherFiltered     []config.ProjectConfig
projectSwitcherModal        *modal.Modal           // Cached modal instance
projectSwitcherModalWidth   int                    // Width for cache invalidation
projectSwitcherMouseHandler *mouse.Handler         // Mouse hit region handler
```

Note: Hover state is managed by the modal library through `hoverID` string, not a separate field.

## Initialization

### Opening the Modal

When `@` is pressed (`update.go`):

```go
case "@":
    m.showProjectSwitcher = !m.showProjectSwitcher
    if m.showProjectSwitcher {
        m.activeContext = "project-switcher"
        m.initProjectSwitcher()
    } else {
        m.resetProjectSwitcher()
        m.updateContext()
    }
```

### initProjectSwitcher()

`model.go:433-454` - Sets up the modal state:

```go
func (m *Model) initProjectSwitcher() {
    m.clearProjectSwitcherModal()  // Invalidate cached modal
    ti := textinput.New()
    ti.Placeholder = "Filter projects..."
    ti.Focus()
    ti.CharLimit = 50
    ti.Width = 40
    m.projectSwitcherInput = ti
    m.projectSwitcherFiltered = m.cfg.Projects.List
    m.projectSwitcherCursor = 0
    m.projectSwitcherScroll = 0

    // Pre-select current project
    for i, proj := range m.projectSwitcherFiltered {
        if proj.Path == m.ui.WorkDir {
            m.projectSwitcherCursor = i
            break
        }
    }
    // Preview the initially-selected project's theme
    m.previewProjectTheme()
}
```

### resetProjectSwitcher()

`model.go:413-423` - Cleans up when modal closes:

```go
func (m *Model) resetProjectSwitcher() {
    m.showProjectSwitcher = false
    m.projectSwitcherCursor = 0
    m.projectSwitcherScroll = 0
    m.projectSwitcherFiltered = nil
    m.clearProjectSwitcherModal()
    m.resetProjectAdd()
    // Restore current project's theme (undo any live preview)
    resolved := theme.ResolveTheme(m.cfg, m.ui.WorkDir)
    theme.ApplyResolved(resolved)
}
```

## Modal Caching

The modal is lazily built and cached to avoid rebuilding on every render.

### ensureProjectSwitcherModal()

`view.go:114-137` - Builds modal only when needed:

```go
func (m *Model) ensureProjectSwitcherModal() {
    modalW := 60
    if modalW > m.width-4 {
        modalW = m.width - 4
    }
    if modalW < 20 {
        modalW = 20
    }

    // Only rebuild if modal doesn't exist or width changed
    if m.projectSwitcherModal != nil && m.projectSwitcherModalWidth == modalW {
        return
    }
    m.projectSwitcherModalWidth = modalW

    m.projectSwitcherModal = modal.New("Switch Project", ...).
        AddSection(...)
}
```

### clearProjectSwitcherModal()

`model.go:426-430` - Clears cache to force rebuild:

```go
func (m *Model) clearProjectSwitcherModal() {
    m.projectSwitcherModal = nil
    m.projectSwitcherModalWidth = 0
    m.projectSwitcherMouseHandler = nil
}
```

Call this when content changes (e.g., filter input, cursor movement with scroll).

## Mouse Click IDs

### projectSwitcherItemID()

`view.go:29-31` - Generates unique IDs for mouse click detection:

```go
const projectSwitcherItemPrefix = "project-switcher-item-"

func projectSwitcherItemID(idx int) string {
    return fmt.Sprintf("%s%d", projectSwitcherItemPrefix, idx)
}
```

These IDs are registered as focusables in the list section and returned by `modal.HandleMouse()` on click.

## Keyboard Handling

Keyboard logic is in `update.go:600-690`.

### Key Priority

1. **KeyType switch** (`msg.Type`) - Handles special keys:
   - `KeyEsc` - Clear filter or close modal
   - `KeyEnter` - Select project
   - `KeyUp/KeyDown` - Arrow navigation

2. **String switch** (`msg.String()`) - Handles named keys:
   - `ctrl+n/ctrl+p` - Emacs-style navigation

3. **Fallthrough** - All other keys forwarded to textinput

### Esc Behavior

Esc has two behaviors:

```go
case tea.KeyEsc:
    // Clear filter if set
    if m.projectSwitcherInput.Value() != "" {
        m.projectSwitcherInput.SetValue("")
        m.projectSwitcherFiltered = m.cfg.Projects.List
        m.projectSwitcherCursor = 0
        m.projectSwitcherScroll = 0
        return m, nil
    }
    // Otherwise close modal
    m.resetProjectSwitcher()
    m.updateContext()
    return m, nil
```

### Navigation with Scroll

Navigation updates cursor and ensures visibility:

```go
case tea.KeyDown:
    m.projectSwitcherCursor++
    if m.projectSwitcherCursor >= len(projects) {
        m.projectSwitcherCursor = len(projects) - 1
    }
    if m.projectSwitcherCursor < 0 {
        m.projectSwitcherCursor = 0
    }
    m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(
        m.projectSwitcherCursor, m.projectSwitcherScroll, 8)
    m.previewProjectTheme()
    return m, nil
```

### Filter Input

Keys not matching special cases go to textinput:

```go
// Forward other keys to text input for filtering
var cmd tea.Cmd
m.projectSwitcherInput, cmd = m.projectSwitcherInput.Update(msg)

// Re-filter on input change
m.projectSwitcherFiltered = filterProjects(allProjects, m.projectSwitcherInput.Value())
m.clearProjectSwitcherModal() // Clear modal cache on filter change

// Clamp cursor to valid range
if m.projectSwitcherCursor >= len(m.projectSwitcherFiltered) {
    m.projectSwitcherCursor = len(m.projectSwitcherFiltered) - 1
}
if m.projectSwitcherCursor < 0 {
    m.projectSwitcherCursor = 0
}
m.projectSwitcherScroll = 0
m.projectSwitcherScroll = projectSwitcherEnsureCursorVisible(...)
m.previewProjectTheme()
```

## Filtering

### filterProjects()

`model.go:457-470` - Case-insensitive substring match:

```go
func filterProjects(all []config.ProjectConfig, query string) []config.ProjectConfig {
    if query == "" {
        return all
    }
    q := strings.ToLower(query)
    var matches []config.ProjectConfig
    for _, p := range all {
        if strings.Contains(strings.ToLower(p.Name), q) ||
           strings.Contains(strings.ToLower(p.Path), q) {
            matches = append(matches, p)
        }
    }
    return matches
}
```

Searches both `Name` and `Path` fields.

### Scroll Helper

`model.go:474-482` - Keeps cursor in visible window:

```go
func projectSwitcherEnsureCursorVisible(cursor, scroll, maxVisible int) int {
    if cursor < scroll {
        return cursor
    }
    if cursor >= scroll+maxVisible {
        return cursor - maxVisible + 1
    }
    return scroll
}
```

## Mouse Handling

Mouse handling uses the modal library's abstraction (`update.go:1073-1115`).

### Modal Library Integration

```go
func (m Model) handleProjectSwitcherMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
    m.ensureProjectSwitcherModal()
    if m.projectSwitcherModal == nil {
        return m, nil
    }
    if m.projectSwitcherMouseHandler == nil {
        m.projectSwitcherMouseHandler = mouse.NewHandler()
    }

    // Let modal library handle all mouse events
    action := m.projectSwitcherModal.HandleMouse(msg, m.projectSwitcherMouseHandler)

    // Check if action is a project item click
    if strings.HasPrefix(action, projectSwitcherItemPrefix) {
        var idx int
        if _, err := fmt.Sscanf(action, projectSwitcherItemPrefix+"%d", &idx); err == nil {
            projects := m.projectSwitcherFiltered
            if idx >= 0 && idx < len(projects) {
                selectedProject := projects[idx]
                m.resetProjectSwitcher()
                m.updateContext()
                return m, m.switchProject(selectedProject.Path)
            }
        }
    }
    // ... handle other actions
}
```

### Hover State

Hover is managed by the modal library through `hoverID` string, passed to section render functions:

```go
func (m *Model) projectSwitcherListSection() modal.Section {
    return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
        // ...
        itemID := projectSwitcherItemID(i)
        isHovered := itemID == hoverID  // Check hover via hoverID string
        // Apply hover styling...
    }, m.projectSwitcherListUpdate)
}
```

## View Rendering

View logic is in `view.go:139-358`.

### Sectioned Modal Structure

```
+-------------------------------------------+
| Switch Project                            |  <- Title (from modal.New)
|                                           |
| [Filter projects...                    ]  |  <- Input section
| 3 of 10 projects                          |  <- Count section
|   ^ 2 more above                          |  <- List section (scroll indicator)
| > sidecar                                 |  <- List section (cursor item)
|   ~/code/sidecar                          |
|   td (current)                            |  <- List section (current project)
|   ~/code/td                               |
|   v 5 more below                          |  <- List section (scroll indicator)
|                                           |
| enter switch  up/down navigate  esc close |  <- Hints section
+-------------------------------------------+
```

### List Section with Focusables

Each project registers a focusable for mouse hit detection (`view.go:160-257`):

```go
func (m *Model) projectSwitcherListSection() modal.Section {
    return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
        var b strings.Builder
        focusables := make([]modal.FocusableInfo, 0, visibleCount)

        for i := scrollOffset; i < scrollOffset+visibleCount && i < len(projects); i++ {
            itemID := projectSwitcherItemID(i)
            isHovered := itemID == hoverID

            // Render project with hover/cursor styling...

            // Register focusable for mouse clicks
            focusables = append(focusables, modal.FocusableInfo{
                ID:      itemID,
                OffsetX: 0,
                OffsetY: lineOffset + (i-scrollOffset)*2,
                Width:   contentWidth,
                Height:  2,  // name + path = 2 lines
            })
        }

        return modal.RenderedSection{Content: b.String(), Focusables: focusables}
    }, m.projectSwitcherListUpdate)
}
```

### Empty States

Two empty states exist (`view.go:169-178`):

1. **No projects configured** - Shows muted "No projects configured" text
2. **No filter matches** - Shows "No matches"

### Project Item Styling

Each project has conditional styling based on cursor, hover, and current state:

| State | Name Style |
|-------|------------|
| Normal | Secondary (blue) |
| Cursor or Hover | Primary + Bold |
| Current | Success (green) + Bold |

Current project shows "(current)" label.

## Theme Preview

### previewProjectTheme()

`model.go:580-586` - Live preview during selection:

```go
func (m *Model) previewProjectTheme() {
    projects := m.projectSwitcherFiltered
    if m.projectSwitcherCursor >= 0 && m.projectSwitcherCursor < len(projects) {
        resolved := theme.ResolveTheme(m.cfg, projects[m.projectSwitcherCursor].Path)
        theme.ApplyResolved(resolved)
    }
}
```

Called on:
- Modal initialization (preview current project's theme)
- Cursor movement (preview newly selected project's theme)
- Filter changes (preview first match's theme)

Theme is restored to current project's theme in `resetProjectSwitcher()`.

## Project Switching

### switchProject()

`model.go:485-577` - Handles the actual switch:

```go
func (m *Model) switchProject(projectPath string) tea.Cmd {
    // Skip if same project
    if projectPath == m.ui.WorkDir {
        return func() tea.Msg {
            return ToastMsg{Message: "Already on this project", Duration: 2 * time.Second}
        }
    }

    // Save active plugin state for old workdir
    oldWorkDir := m.ui.WorkDir
    if activePlugin := m.ActivePlugin(); activePlugin != nil {
        state.SetActivePlugin(oldWorkDir, activePlugin.ID())
    }

    // Check for saved worktree to restore
    // ... worktree restoration logic ...

    // Update UI state
    m.ui.WorkDir = targetPath
    m.intro.RepoName = GetRepoName(targetPath)

    // Apply project-specific theme
    resolved := theme.ResolveTheme(m.cfg, targetPath)
    theme.ApplyResolved(resolved)

    // Reinitialize all plugins
    startCmds := m.registry.Reinit(targetPath)

    // Send WindowSizeMsg to plugins for layout recalculation
    // ... size message broadcasting ...

    // Restore active plugin for new workdir
    newActivePluginID := state.GetActivePlugin(targetPath)
    if newActivePluginID != "" {
        m.FocusPluginByID(newActivePluginID)
    }

    // Return batch with toast notification
    return tea.Batch(
        tea.Batch(startCmds...),
        func() tea.Msg {
            return ToastMsg{
                Message:  fmt.Sprintf("Switched to %s", GetRepoName(targetPath)),
                Duration: 3 * time.Second,
            }
        },
    )
}
```

### Plugin Reinitialization

When switching projects, plugins receive a new `Init()` call via `m.registry.Reinit()`. Plugins must reset their state appropriately.

## Adding New Features

### Adding a Keyboard Shortcut

1. Add case in `update.go` string switch (after KeyType switch)
2. Return early to prevent textinput forwarding

```go
case "ctrl+d":
    // Custom action
    return m, nil
```

### Adding Project Metadata

1. Extend `config.ProjectConfig` in `internal/config/types.go`
2. Update `filterProjects()` to search new fields
3. Update `projectSwitcherListSection()` to display new fields

### Changing Filter Algorithm

Replace `filterProjects()` body. Current: substring match. Options:
- Fuzzy matching (like command palette)
- Regex support
- Field-specific search (`name:foo`)

### Adding a New Section

1. Create a section function returning `modal.Section`:

```go
func (m *Model) projectSwitcherNewSection() modal.Section {
    return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
        // Render content
        return modal.RenderedSection{Content: "..."}
    }, nil)
}
```

2. Add to modal builder in `ensureProjectSwitcherModal()`:

```go
m.projectSwitcherModal = modal.New("Switch Project", ...).
    AddSection(m.projectSwitcherInputSection()).
    AddSection(m.projectSwitcherNewSection()).  // New section
    AddSection(m.projectSwitcherListSection()).
    // ...
```

## Testing

Currently no dedicated tests exist for the project switcher. Recommended test coverage:

1. **filterProjects()** - Various query inputs
2. **projectSwitcherEnsureCursorVisible()** - Scroll boundary cases
3. **Keyboard navigation** - Cursor bounds, scroll sync
4. **Mouse click detection** - Via modal library integration

## Common Pitfalls

1. **Forgetting updateContext()** - Call after closing modal to restore app context
2. **Stale modal cache** - Call `clearProjectSwitcherModal()` when content changes
3. **Cursor out of bounds** - Always clamp after filtering
4. **Printable keys vs navigation** - Keep printable characters routed to textinput
5. **Theme preview cleanup** - Always restore theme in `resetProjectSwitcher()`
6. **Focusable coordinates** - Each project takes 2 lines (name + path)

## File Locations

| File | Contents |
|------|----------|
| `internal/app/model.go` | State, init, reset, filter, switch, theme preview |
| `internal/app/update.go` | Keyboard and mouse handlers |
| `internal/app/view.go` | Modal building and section rendering |
| `internal/modal/` | Modal library (builder, sections, layout) |
| `internal/config/types.go` | ProjectConfig struct |
| `internal/config/loader.go` | Config loading with path validation |
