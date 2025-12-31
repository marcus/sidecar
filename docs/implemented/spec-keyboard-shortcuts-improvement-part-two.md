# Keyboard Shortcuts System Overhaul

## Summary of Issues

From screenshot analysis and code review, 4 distinct problems exist:

### Issue 1: Command Palette (?) Doesn't Scroll

**Root cause**: `palette/view.go` renders ALL entries but ignores scroll state (`offset`, `maxVisible`)

- Model has scroll state (lines 25-26, 30 in palette.go)
- View doesn't use it - iterates through ALL entries (view.go:94-113)
- `moveCursor()` correctly updates offset but View ignores it

### Issue 2: Duplicate Entries in Palette

**Root cause**: Same commands declared in multiple contexts aren't deduplicated visually

- Deduplication uses `command:context` key (entries.go:68)
- "scroll" in git-diff, conversation-detail, message-detail = 3 entries
- "search" in conversations, file-browser-tree, file-browser-preview = 3+ entries
- Screenshot shows: multiple Refresh, Scroll, Search, View, Open entries

### Issue 3: Plugin-Specific Shortcuts Not Shown First

**Root cause**: `footerHints()` in view.go adds global hints BEFORE plugin hints

```go
func (m Model) footerHints() []footerHint {
    hints := m.globalFooterHints()           // Global first!
    hints = append(hints, m.pluginFooterHints(...)...)
    return hints
}
```

### Issue 4: Footer Shows Less Relevant Shortcuts

**Root cause**: No priority system - order is declaration order from Commands()

- Commands return in code order, not importance order
- Width truncation cuts off later commands arbitrarily
- No way to mark certain shortcuts as "primary" for footer display

---

## Implementation Plan

### Task 1: Fix Palette Scrolling (P0)

**Files**: `internal/palette/view.go`

1. Modify `View()` to respect `m.offset` and `m.maxVisible`
2. Only render entries within visible range
3. Add scroll indicators (↑/↓) when content exceeds viewport
4. Ensure layer headers are handled correctly with virtual scrolling

**Key changes**:

```go
// In View() - track which entries to show
visibleStart := m.offset
visibleEnd := min(m.offset + m.maxVisible, len(m.filtered))
// Only render entries in range
```

### Task 2: Context Toggle + Smart Deduplication (P1)

**Files**: `internal/palette/palette.go`, `internal/palette/entries.go`, `internal/palette/view.go`

**Approach**: Allow user to toggle between "Current Context" and "All" views

1. Add `showAllContexts bool` to palette Model
2. Add keybinding (tab?) to toggle between modes
3. **Current Context mode** (default):
   - Show only commands for: active context + global
   - No duplicates possible
4. **All Contexts mode**:
   - Show all commands grouped by CommandID
   - Display count indicator: "Scroll (4 contexts)"
   - Store context list in `OtherContexts []string` field

**Implementation**:

1. Add toggle state and keybinding to palette.go
2. In `BuildEntries()`, add `showAll` parameter
3. When showAll=false, filter to current context + global only
4. When showAll=true, group by CommandID and show count
5. Update view to show mode indicator and handle grouped display

### Task 3: Prioritize Plugin Shortcuts in Footer (P1)

**Files**: `internal/app/view.go`

1. Reverse order in `footerHints()`:

```go
func (m Model) footerHints() []footerHint {
    hints := m.pluginFooterHints(p, m.activeContext)  // Plugin first!
    hints = append(hints, m.globalFooterHints()...)
    return hints
}
```

1. Consider limiting global hints in footer to just essentials (?, q)

### Task 4: Add Priority to Footer Hints (P2)

**Files**: `internal/plugin/plugin.go`, `internal/app/view.go`, all plugin Commands()

1. Add `Priority int` field to `plugin.Command` struct:

```go
type Command struct {
    ID          string
    Name        string
    Description string
    Category    Category
    Context     string
    Priority    int  // NEW: Lower = more important, shown first
}
```

1. Sort hints by priority in `pluginFooterHints()` before truncation

2. Update plugin Commands() to set appropriate priorities:
   - Core actions (Stage, Commit, View): Priority 1
   - Secondary actions (Browse, History): Priority 2
   - Navigation (Scroll, Back): Priority 3

---

## Files to Modify

| File                                       | Changes                                                  |
| ------------------------------------------ | -------------------------------------------------------- |
| `internal/palette/palette.go`              | Add showAllContexts toggle, keybinding handler           |
| `internal/palette/view.go`                 | Add virtual scrolling, scroll indicators, mode indicator |
| `internal/palette/entries.go`              | Add context toggle support, grouping by CommandID        |
| `internal/app/view.go`                     | Reorder hints (plugin before global), sort by priority   |
| `internal/plugin/plugin.go`                | Add Priority field to Command struct                     |
| `internal/plugins/gitstatus/plugin.go`     | Set priorities on Commands()                             |
| `internal/plugins/filebrowser/plugin.go`   | Set priorities on Commands()                             |
| `internal/plugins/conversations/plugin.go` | Set priorities on Commands()                             |
| `internal/plugins/tdmonitor/plugin.go`     | Set priorities on Commands()                             |

---

## TD Epic

Create single epic: `td add "Keyboard Shortcuts System Overhaul" --type epic`

Sub-tasks (tracked in epic):

1. Fix palette scrolling (P0)
2. Add context toggle + deduplication (P1)
3. Prioritize plugin shortcuts in footer (P1)
4. Add Priority field + sort footer hints (P2)

---

## Testing Checklist

- [ ] Palette scrolls with j/k/ctrl+d/ctrl+u when entries exceed height
- [ ] Scroll indicators (↑/↓) appear when palette has hidden content
- [ ] Tab toggles between "Current Context" and "All" modes in palette
- [ ] Current Context mode shows only active context + global (no duplicates)
- [ ] All mode groups same commands with "(N contexts)" indicator
- [ ] Footer shows plugin-specific shortcuts BEFORE global ones
- [ ] Most important shortcuts appear first in footer even on narrow terminals
- [ ] Priority field correctly sorts footer hints by importance
- [ ] All plugins have sensible priority values on their Commands()
- [ ] Build passes with no regressions
