# Epic: Conversations Plugin UI Regressions & Enhancements

## Overview
The conversations plugin has several regressions and UI improvements needed to restore full functionality and improve the user experience.

**Priority**: High
**Affected Files**:
- `internal/plugins/conversations/plugin.go`
- `internal/plugins/conversations/view.go`

---

## Stories

### BUG-001: Navigation shortcuts (j/k, arrow keys) not working in conversation views

**Type**: Bug
**Priority**: Critical

**Description**:
Arrow keys and j/k vim-style navigation shortcuts have lost functionality in conversation views. Users cannot navigate between conversations or messages.

**Current Behavior**:
Navigation shortcuts do not move the cursor through the conversation list or message list.

**Expected Behavior**:
- `j` / `down` - Move cursor down 1 item
- `k` / `up` - Move cursor up 1 item
- `ctrl+d` - Page down (~10 items)
- `ctrl+u` - Page up (~10 items)
- `g` - Jump to top
- `G` - Jump to bottom

**Investigation Notes**:
The routing logic in `plugin.go:159-180` routes based on `p.view` and `p.activePane`:
- In `ViewSessions`: routes to `updateSessions()` (unless twoPane + PaneMessages)
- In `ViewMessages`: routes to `updateMessages()` (unless twoPane + PaneSidebar)

Key handlers exist in `updateSessions()` (lines 331-406) and `updateMessages()` (lines 723-761).

**Potential Causes to Investigate**:
1. `p.view` not set correctly on plugin initialization or after state transitions
2. `p.activePane` stuck in wrong state preventing key routing
3. `p.searchMode` or `p.filterMode` stuck as true, causing early return (lines 320-327)
4. `visibleSessions()` returning empty, causing cursor bounds to fail
5. Event not reaching plugin (app-level issue with focus)

**Acceptance Criteria**:
- [ ] j/k navigation works in session list view
- [ ] j/k navigation works in message/turn list view
- [ ] Arrow keys work in both views
- [ ] ctrl+d/ctrl+u page navigation works
- [ ] g/G jump to top/bottom works
- [ ] Navigation works in both single-pane and two-pane modes
- [ ] Add test coverage for navigation state transitions

---

### STORY-002: Implement proper two-pane conversation view

**Type**: Enhancement
**Priority**: High

**Description**:
The conversations screen should show a 2-pane view with:
- Left pane: List of conversations
- Right pane: Selected conversation details/messages

As the user navigates between conversations in the left pane, the right pane should update to show the selected conversation.

**Current State**:
Two-pane mode exists (enabled when width >= 120, see `plugin.go:272`). The rendering is handled by:
- `renderTwoPane()` at `view.go:810-863`
- `renderSidebarPane()` at `view.go:866-951` (left pane)
- `renderMainPane()` at `view.go:1093-1201` (right pane)

**Issues to Address**:
1. Verify two-pane mode activates correctly
2. Ensure left pane shows conversation list with proper scrolling
3. Ensure right pane updates when cursor moves in left pane (debounced load at `plugin.go:306-314`)
4. Verify tab/l/h navigation between panes works
5. Ensure proper focus indication on active pane

**Acceptance Criteria**:
- [ ] Two-pane mode activates when terminal width >= 120
- [ ] Left pane shows scrollable conversation list
- [ ] Right pane shows selected conversation details/messages
- [ ] Moving cursor in left pane updates right pane content
- [ ] Tab key toggles focus between panes
- [ ] l/right moves focus to right pane, h/left moves to left pane
- [ ] Active pane has distinct visual indicator (border style)
- [ ] Navigation shortcuts work in both panes based on focus

---

### STORY-003: Fix group header spacing (Yesterday, This Week)

**Type**: Bug/Enhancement
**Priority**: Medium

**Description**:
In the list view, there should be a line of vertical space **above** "Yesterday" and "This Week" group headers, but **not below** them. This helps visually separate time groups.

**Current Implementation** (`view.go:97-123`):
```go
if currentGroup != "" && (sessionGroup == "Yesterday" || sessionGroup == "This Week") {
    sb.WriteString("\n")  // Spacer BEFORE header
    lineCount++
}
// ... render header
sb.WriteString(styles.PanelHeader.Render(groupHeader))
sb.WriteString("\n")  // After header (for next line)
```

**Verification Needed**:
1. Confirm spacer appears above "Yesterday" and "This Week" only
2. Confirm no spacer appears above "Today" or "Older"
3. Confirm no extra spacer appears below headers (the `\n` after header is just the line ending)
4. Check compact sidebar rendering has same behavior

**Acceptance Criteria**:
- [ ] Blank line appears above "Yesterday" header (but not if it's first visible group)
- [ ] Blank line appears above "This Week" header
- [ ] No blank line above "Today" header
- [ ] No blank line above "Older" header
- [ ] No blank line below any header (header immediately followed by sessions)
- [ ] Spacing works in both full-width and compact sidebar modes

---

### STORY-004: Improve sub-conversation indentation

**Type**: Enhancement
**Priority**: Medium

**Description**:
Sub-conversations (spawned by agents) are currently shown with a `↳` indicator which is good. However, we also need additional space indentation to clearly delineate actual conversation boundaries.

**Current Implementation**:
- `renderSessionRow()` at `view.go:159-163`: 2-space indent for sub-agents
- `renderCompactSessionRow()` at `view.go:1047-1050`: 4-space indent for sub-agents

Both use `↳` indicator for sub-agents (`view.go:170`, `view.go:1055-1056`).

**Proposed Changes**:
1. Increase base indentation for sub-agents to make hierarchy clearer
2. Consider hierarchical indentation if parent-child relationships are available
3. Add visual separator or grouping to show which sub-conversations belong to which parent

**Visual Example** (current):
```
> ● Build authentication system
  ↳ Research OAuth providers
  ↳ Implement token storage
● Deploy to production
```

**Visual Example** (proposed):
```
> ● Build authentication system
      ↳ Research OAuth providers
      ↳ Implement token storage
● Deploy to production
```

**Acceptance Criteria**:
- [ ] Sub-conversations have increased indentation (suggest 6 spaces before `↳`)
- [ ] Visual hierarchy clearly shows parent-child relationship
- [ ] Indentation works correctly in both full-width and compact modes
- [ ] Cursor selection still aligns properly with indented items
- [ ] Width calculations account for increased indent

---

### STORY-005: Replace date/time column with conversation length in list view

**Type**: Enhancement
**Priority**: Medium

**Description**:
In the conversation list view (left pane), replace any date/time column with conversation length (duration). The date/time should only be shown in the detail view (right pane).

**Current Implementation**:
The code already shows duration in the list view:
- `renderSessionRow()` at `view.go:176-181`: Uses `formatSessionDuration(session.Duration)` with comment "Conversation length (replaces timestamp column in list views)"
- `renderCompactSessionRow()` at `view.go:1008-1012`: Same, shows duration

**Verification Needed**:
1. Confirm no timestamp appears in list view rendering
2. Confirm duration format is appropriate (e.g., "6m25s", "1h20m")
3. Ensure detail view shows full date/time information

**Detail View Date/Time** (`view.go:291-314` in `renderSessionHeader`):
Currently shows `updated 12-25 14:30` format in the stats line.

**Acceptance Criteria**:
- [ ] List view shows only duration (no date/time)
- [ ] Duration format is human-readable (e.g., "5m", "1h20m", "2d")
- [ ] Detail view header shows full timestamp (date and time)
- [ ] Consider showing "started" time in detail view in addition to "updated"

---

## Technical Notes

### State Machine for Views
```
ViewSessions (default)
    ├─ [enter] → ViewMessages
    ├─ [U] → ViewAnalytics
    └─ [width >= 120] → Two-pane mode (auto-loads messages)

ViewMessages
    ├─ [esc/q] → ViewSessions
    ├─ [enter] → ViewMessageDetail
    └─ [h/left in two-pane] → Focus sidebar

ViewMessageDetail
    └─ [esc/q] → ViewMessages

ViewAnalytics
    └─ [esc/q] → ViewSessions
```

### Key Files Reference
| File | Key Functions |
|------|---------------|
| `plugin.go` | `Update()` (routing), `updateSessions()`, `updateMessages()`, `setSelectedSession()` |
| `view.go` | `View()`, `renderTwoPane()`, `renderSidebarPane()`, `renderMainPane()`, `renderGroupedSessions()`, `renderSessionRow()`, `renderCompactSessionRow()` |
| `summary.go` | `GroupSessionsByTime()`, `getSessionGroup()` |
| `turns.go` | `GroupMessagesIntoTurns()` |

### Testing Checklist
- [ ] Test navigation in single-pane mode (width < 120)
- [ ] Test navigation in two-pane mode (width >= 120)
- [ ] Test pane focus switching (tab, h, l)
- [ ] Test cursor visibility after scroll operations
- [ ] Test with 0, 1, and many sessions
- [ ] Test with sub-agent conversations present
- [ ] Test group header spacing with different date distributions
