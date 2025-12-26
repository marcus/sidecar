# TD Plugin Feature Parity Plan

## Overview

Bring sidecar's td-monitor plugin to feature parity with the original td monitor TUI at ~/code/td.

**Design Decisions (confirmed):**

- CLI-based mutations (safer, reuses td logic)
- 3-panel layout matching original td monitor
- Core parity first (monitor doesn't show work sessions/files/deps in panels)
- Include work sessions in scope

---

## Current State Comparison

### Original TD Monitor TUI

3-panel layout with:

- **Current Work Panel**: Focused issue + in-progress issues
- **Task List Panel**: Ready, Reviewable, Blocked, Closed (categories)
- **Activity Log Panel**: Recent activity (logs, actions, comments)

Features:

- Search mode (`/` key) with real-time filtering
- Stats modal (`s` key) with status/type/priority charts
- Issue detail modal with logs, handoffs, dependencies (blocked by/blocks)
- Confirmation dialogs for delete
- Prev/next navigation in modals (`←/→`)
- Mark for review (`r` in Current Work)
- Approve (`a` in Task List for reviewable)
- Delete (`x` with confirmation)
- Toggle closed tasks (`c`)
- Help overlay (`?`)

### Existing Sidecar TD Plugin

Single-panel with 3 collapsible lists:

- In Progress, Ready, Reviewable
- Basic navigation (j/k, tab, enter)
- Basic detail view (title, description only)
- Approve/delete via CLI
- Auto-refresh every 2s

---

## Feature Gap Analysis

| Feature                    | TD Monitor | Sidecar Plugin | Priority |
| -------------------------- | ---------- | -------------- | -------- |
| 3-panel layout             | ✅         | ❌             | P0       |
| Activity panel             | ✅         | ❌             | P0       |
| Blocked/Closed categories  | ✅         | ❌             | P0       |
| Search mode                | ✅         | ❌             | P1       |
| Stats modal                | ✅         | ❌             | P1       |
| Issue modal - logs         | ✅         | ❌             | P1       |
| Issue modal - handoffs     | ✅         | ❌             | P1       |
| Issue modal - dependencies | ✅         | ❌             | P1       |
| Modal prev/next navigation | ✅         | ❌             | P2       |
| Mark for review action     | ✅         | ❌             | P2       |
| Toggle closed              | ✅         | ❌             | P2       |
| Confirmation dialogs       | ✅         | ❌             | P2       |
| Help overlay               | ✅         | ❌             | P3       |

---

## Implementation Plan

### Phase 1: Data Layer (Foundation)

**Files**: `types.go`, `data.go`

#### 1.1 New types (`types.go`)

```go
type Handoff struct {
    ID        string
    IssueID   string
    SessionID string
    Done      []string
    Remaining []string
    Decisions []string
    Uncertain []string
    Timestamp time.Time
}

type Log struct {
    ID        string
    IssueID   string
    SessionID string
    Type      string  // progress, decision, blocker, etc.
    Message   string
    Timestamp time.Time
}

type ActivityItem struct {
    Timestamp time.Time
    SessionID string
    Type      string  // log, action, comment
    IssueID   string
    Message   string
    LogType   string
}

type ExtendedStats struct {
    Total           int
    ByStatus        map[string]int
    ByType          map[string]int
    ByPriority      map[string]int
    TotalPoints     int
    AvgPointsPerTask float64
    CompletionRate  float64
    CreatedToday    int
    CreatedThisWeek int
    TotalLogs       int
    TotalHandoffs   int
}
```

#### 1.2 New queries (`data.go`)

- [ ] `GetLogs(issueID string, limit int) ([]Log, error)`
- [ ] `GetLatestHandoff(issueID string) (*Handoff, error)`
- [ ] `GetDependencies(issueID string) ([]string, error)` - what blocks this
- [ ] `GetBlockedBy(issueID string) ([]string, error)` - what this blocks
- [ ] `GetActivityLog(limit int) ([]ActivityItem, error)` - recent activity
- [ ] `GetBlockedIssues() ([]Issue, error)` - issues with open dependencies
- [ ] `GetClosedIssues(limit int) ([]Issue, error)` - closed issues
- [ ] `GetExtendedStats() (*ExtendedStats, error)` - stats for modal
- [ ] `SearchIssues(query string) ([]Issue, error)` - full-text search

### Phase 2: 3-Panel Layout

**Files**: `plugin.go`, `view.go`

#### 2.1 State restructure (`plugin.go`)

```go
type Panel int
const (
    PanelCurrentWork Panel = iota
    PanelTaskList
    PanelActivity
)

// Add to state:
ActivePanel     Panel
FocusedIssue    *Issue
InProgressIssues []Issue
TaskList struct {
    Ready      []Issue
    Reviewable []Issue
    Blocked    []Issue
    Closed     []Issue
}
Activity        []ActivityItem
Cursor          map[Panel]int
ScrollOffset    map[Panel]int
```

#### 2.2 Panel rendering (`view.go`)

- [ ] `renderCurrentWorkPanel()` - focused + in-progress
- [ ] `renderTaskListPanel()` - categorized with headers (Ready, Reviewable, Blocked, Closed)
- [ ] `renderActivityPanel()` - timestamped activity stream
- [ ] `renderFooter()` - key hints + last refresh time
- [ ] Panel border highlighting for active panel

#### 2.3 Navigation

- [ ] Tab/Shift+Tab to cycle panels
- [ ] 1/2/3 to jump to panel
- [ ] j/k scroll viewport, ↑/↓ move cursor
- [ ] Track cursor per panel

### Phase 3: Enhanced Detail Modal

**Files**: `plugin.go`, `view.go`

#### 3.1 Modal state

```go
ModalOpen       bool
ModalIssueID    string
ModalSourcePanel Panel
ModalScroll     int
ModalIssue      *Issue
ModalHandoff    *Handoff
ModalLogs       []Log
ModalBlockedBy  []Issue
ModalBlocks     []Issue
```

#### 3.2 Modal content

- [ ] Header: ID + title + status/type/priority badges
- [ ] Labels display
- [ ] Session info (implementer, reviewer)
- [ ] Description section
- [ ] Acceptance criteria section
- [ ] Dependencies section (blocked by / blocks)
- [ ] Latest handoff (done/remaining/decisions/uncertain)
- [ ] Recent logs list
- [ ] Scrolling with j/k or ↑/↓

#### 3.3 Modal navigation

- [ ] ←/→ or h/l to prev/next issue in source panel
- [ ] esc to close
- [ ] r to refresh

### Phase 4: Search Mode

**Files**: `plugin.go`, `view.go`

#### 4.1 State

```go
SearchMode  bool
SearchQuery string
```

#### 4.2 Implementation

- [ ] `/` enters search mode
- [ ] Character input appends to query
- [ ] Backspace removes last char
- [ ] Real-time filtering of task list
- [ ] esc exits search mode
- [ ] enter applies search and exits mode
- [ ] Search bar rendering with query + cursor

### Phase 5: Stats Modal

**Files**: `plugin.go`, `view.go`

#### 5.1 State

```go
StatsOpen    bool
StatsLoading bool
StatsData    *ExtendedStats
StatsScroll  int
```

#### 5.2 Content

- [ ] Status breakdown bar chart
- [ ] Type breakdown (compact)
- [ ] Priority breakdown (compact)
- [ ] Summary metrics (total, points, completion %)
- [ ] Timeline (oldest open, last closed, created today/week)
- [ ] Activity counts (logs, handoffs)

### Phase 6: Additional Actions

**Files**: `plugin.go`

#### 6.1 Mark for review

- [ ] `r` key in Current Work panel
- [ ] Only works on in_progress issues
- [ ] Calls `td review <id>` via CLI

#### 6.2 Toggle closed

- [ ] `c` key toggles closed issues visibility
- [ ] Updates data fetch to include/exclude closed

#### 6.3 Confirmation dialogs

- [ ] State: `ConfirmOpen`, `ConfirmAction`, `ConfirmIssueID`
- [ ] Y/N key handling
- [ ] Render centered dialog box

### Phase 7: Polish

- [ ] Help overlay (`?` key)
- [ ] Active sessions indicator in footer
- [ ] Handoff alert if new handoffs occurred
- [ ] Review alert if items pending review
- [ ] Compact view for small terminals

---

## Files to Modify

| File                                        | Changes                                                   |
| ------------------------------------------- | --------------------------------------------------------- |
| `internal/plugins/tdmonitor/types.go`       | Add Handoff, Log, ActivityItem, ExtendedStats, Panel enum |
| `internal/plugins/tdmonitor/data.go`        | Add 9 new query methods                                   |
| `internal/plugins/tdmonitor/view.go`        | Complete rewrite for 3-panel layout, modals               |
| `internal/plugins/tdmonitor/plugin.go`      | Panel state, modal state, search state, new keybindings   |
| `internal/plugins/tdmonitor/plugin_test.go` | Tests for new functionality                               |

---

## Reference Files in ~/code/td

| File                             | Use For                                      |
| -------------------------------- | -------------------------------------------- |
| `internal/tui/monitor/model.go`  | State structure, message types, key handling |
| `internal/tui/monitor/view.go`   | Panel rendering, modal rendering, formatting |
| `internal/tui/monitor/data.go`   | Data fetching patterns                       |
| `internal/tui/monitor/styles.go` | Color schemes, badges                        |
| `internal/db/db.go`              | Query implementations                        |
| `internal/models/models.go`      | Type definitions                             |

---

## Implementation Order

1. **Phase 1** - Data layer (enables everything else)
2. **Phase 2** - 3-panel layout (core structural change)
3. **Phase 3** - Enhanced modal (high value)
4. **Phase 4** - Search mode (quick win)
5. **Phase 5** - Stats modal (quick win)
6. **Phase 6** - Additional actions (incremental)
7. **Phase 7** - Polish (final touches)

---

## Estimated Scope

- ~500 lines new in `data.go`
- ~800 lines rewrite in `view.go`
- ~400 lines new in `plugin.go`
- ~100 lines new in `types.go`
- ~200 lines new tests

Total: ~2000 lines of code changes
