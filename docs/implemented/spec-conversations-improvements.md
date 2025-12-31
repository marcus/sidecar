# Conversations Plugin: Vision & Feature Plan

## The Goal

Transform the conversations plugin from a basic session list into **a developer's command center for understanding their AI coding sessions** - what happened, what it cost, what files were touched, and how to find anything.

---

## Current State (Underwhelming)

- Session list with timestamps and slugs
- Message view with truncated content (200 chars)
- Token counts per message (in/out/cache)
- Basic search by session name
- File watcher for live updates

**What's missing**: Everything that makes sessions _understandable_ at a glance.

---

## Feature Vision

### 1. Session Dashboard View (New)

**At-a-glance session intelligence:**

```
┌─ Sessions ────────────────────────────────────────────────────┐
│  Today (3 sessions, 145k tokens, ~$2.40)                      │
│  ────────────────────────────────────────────────────────────│
│  ● 14:23  "Add auth flow"         opus    45k   $0.85   12m  │
│    └─ 8 files touched · 3 commits · 127 msgs                  │
│                                                               │
│    10:15  "Fix build errors"      sonnet  8k    $0.12    4m  │
│    └─ 2 files touched · 1 commit · 23 msgs                    │
│                                                               │
│  Yesterday (5 sessions, 89k tokens, ~$1.20)                   │
│  ────────────────────────────────────────────────────────────│
│    ...                                                        │
└───────────────────────────────────────────────────────────────┘
```

**Features:**

- **Grouped by time period** (Today, Yesterday, This Week, Older)
- **Session summary line**: slug, model, tokens, cost estimate, duration
- **Activity row**: files touched, commits generated, message count
- **Active indicator** (●) for sessions updated < 5 min ago
- **Collapsible groups** to focus on recent work

### 2. Session Detail Header

When viewing a session, show context:

```
┌─ Session: add-auth-flow ──────────────────────────────────────┐
│  claude-opus-4  │  ~/code/myproject  │  main  │  12m 34s      │
│  127 msgs  │  in:23k out:45k cache:12k  │  ~$0.85 est        │
│                                                               │
│  Files: 8 modified · Tools: 45 calls (23 Read, 15 Edit, 7 Bash)│
└───────────────────────────────────────────────────────────────┘
```

**Data sources (already in JSONL):**

- Model name (per message, show primary)
- CWD and git branch (first message)
- Duration (first → last timestamp)
- Token breakdown with cache stats
- Tool call aggregation from ToolUses

### 3. Tool Impact Summary

Show what the agent actually _did_:

```
 Tool Usage                                    [t to toggle]
────────────────────────────────────────────────────────────
 Read (23)    src/auth/*.go, internal/db/user.go, ...
 Edit (15)    src/auth/oauth.go (+145), src/auth/handler.go (+32)
 Bash (7)     go build, go test, git status
 Write (3)    src/auth/tokens.go (new), docs/AUTH.md (new)
────────────────────────────────────────────────────────────
```

**Value**: Instantly see what files were read/modified, what commands ran

### 4. Message Thread View (Enhanced)

```
┌─ Messages ────────────────────────────────────────────────────┐
│  [14:23:45] user                                              │
│  Add user authentication with OAuth support                   │
│                                                               │
│  [14:23:52] assistant                          opus  1.2k→8.1k│
│  I'll implement OAuth authentication. Let me examine...      │
│  ├─ Read: src/auth/handler.go                                 │
│  ├─ Read: go.mod                                              │
│  └─ Edit: src/auth/oauth.go (+45 lines)                       │
│                                                               │
│  [14:24:15] user                                              │
│  Also add refresh token support                               │
│                                                               │
│  [14:24:22] assistant                          opus  0.8k→12k │
│  Adding refresh token handling...                             │
│  ├─ [thinking] Considering token rotation strategies...       │
│  ├─ Edit: src/auth/oauth.go (+23 lines)                       │
│  └─ Bash: go test ./src/auth/...                              │
└───────────────────────────────────────────────────────────────┘
```

**Enhancements:**

- **Model badge** per message (opus/sonnet/haiku)
- **Token flow** (in→out format)
- **Inline tool summary** with file paths and line counts
- **Thinking blocks** (collapsed by default, expandable)
- **Full content** on Enter (modal or expanded view)

### 5. Search & Filter (Enhanced)

**Multi-dimensional search:**

```
 Search: oauth                        [3/15 sessions]
 Filters: [model:opus] [tokens:>10k] [today]
────────────────────────────────────────────────────────────
 ● 14:23  "Add auth flow"         opus    45k   $0.85   ✓
   10:15  "Fix OAuth callback"    sonnet  12k   $0.18   ✓
   09:00  "OAuth debugging"       opus    8k    $0.15   ✓
```

**Filter dimensions:**

- **Text search**: session name, slug, message content
- **Model filter**: opus, sonnet, haiku
- **Token range**: expensive sessions (>50k), cheap (<5k)
- **Time range**: today, this week, date picker
- **Activity**: active only, has commits, has errors
- **File filter**: sessions that touched specific files

### 6. Cost Tracking

**Estimated cost based on model + tokens:**

```
 Usage This Week                              [u to toggle]
────────────────────────────────────────────────────────────
 opus      │ ████████████░░░░░░░ │  234k tokens  │  $4.20
 sonnet    │ ██████░░░░░░░░░░░░░ │   89k tokens  │  $0.45
 haiku     │ ██░░░░░░░░░░░░░░░░░ │   23k tokens  │  $0.02
────────────────────────────────────────────────────────────
 Total: 346k tokens │ ~$4.67 estimated
```

**Pricing (approximate, configurable):**

- Opus: ~$15/M input, ~$75/M output
- Sonnet: ~$3/M input, ~$15/M output
- Haiku: ~$0.25/M input, ~$1.25/M output

### 7. Global Analytics Dashboard (NEW - from stats-cache.json)

Press `U` (shift+u) for global usage view:

```
┌─ Usage Analytics ─────────────────────────────────────────────┐
│  Since Nov 12  │  766 sessions  │  58,731 messages            │
│                                                               │
│  This Week's Activity                                         │
│  ─────────────────────────────────────────────────────────── │
│  Mon │ ████████████░░░░ │  4,800 msgs  │  16 sessions         │
│  Tue │ ██████████░░░░░░ │  3,200 msgs  │  12 sessions         │
│  Wed │ ████████░░░░░░░░ │  2,100 msgs  │   8 sessions         │
│  ...                                                          │
│                                                               │
│  Model Usage                                                  │
│  ─────────────────────────────────────────────────────────── │
│  opus   │ ████████████░░░ │ 1.6M in  2.5M out │ ~$180         │
│  sonnet │ ██████░░░░░░░░░ │ 890k in  1.2M out │ ~$22          │
│  haiku  │ ██░░░░░░░░░░░░░ │ 120k in  340k out │ ~$0.50        │
│                                                               │
│  Peak Hours: 5-7 PM (38% of activity)                         │
│  Cache Efficiency: 89% (1.2B tokens from cache)               │
│  Longest Session: 26h 45m (td-monitor refactor)               │
└───────────────────────────────────────────────────────────────┘
```

**Data source**: `~/.claude/stats-cache.json` (already computed by Claude Code)

### 8. Quick Actions

| Key     | Action                     |
| ------- | -------------------------- |
| `Enter` | View session messages      |
| `/`     | Search sessions            |
| `f`     | Filter menu                |
| `t`     | Toggle tool summary        |
| `u`     | Session usage stats        |
| `U`     | **Global analytics**       |
| `c`     | Copy session to clipboard  |
| `e`     | Export session as markdown |
| `r`     | Refresh                    |

### 9. Thinking Blocks (Extended Thinking Visualization)

Claude's internal reasoning is captured but currently **filtered out**. Surface it:

```
[14:24:22] assistant                              opus  0.8k→12k
├─ [thinking] 847 tokens                          [expand: T]
│   "I need to consider token rotation strategies. The current
│    implementation doesn't handle refresh tokens, so I'll need
│    to add storage for the refresh token and implement..."
├─ Edit: src/auth/oauth.go (+23 lines)
└─ Bash: go test ./src/auth/...
```

**Collapsed by default** (just shows token count), press `T` to expand.

### 10. Two-Pane Layout (Optional)

For wider terminals, split view:

```
┌─ Sessions (30%) ─────┬─ Messages (70%) ──────────────────────┐
│  ● Add auth flow     │ [14:23:45] user                       │
│    Fix build errors  │ Add user authentication...            │
│    Initial setup     │                                       │
│                      │ [14:23:52] assistant         1.2k→8.1k│
│                      │ I'll implement OAuth...               │
│                      │ ├─ Read: src/auth/handler.go          │
│                      │ └─ Edit: src/auth/oauth.go            │
└──────────────────────┴───────────────────────────────────────┘
```

---

## Data Already Available (Just Need to Surface)

### From JSONL (parsed but not displayed)

- `message.model` - Model name per response
- `cwd`, `gitBranch`, `version` - Session context
- `thinking` blocks - Agent reasoning (currently filtered out!)
- Tool `input` - Full parameters (file paths, content)
- `cache_creation_input_tokens` - Cache write costs
- `toolUseResult` - Structured tool output metadata
- `isSidechain` - Branched conversation paths

### From JSONL (currently skipped)

- `tool_result` entries - Actual tool outputs
- `parentUuid` - Message threading

### From stats-cache.json (NEW - not currently used!)

```
~/.claude/stats-cache.json
```

- **Global totals**: totalSessions, totalMessages, firstSessionDate
- **Daily activity**: messages, sessions, tool calls per day
- **Model breakdown**: tokens by model per day
- **Model totals**: input/output/cache tokens by model
- **Peak hours**: hourCounts (which hours are busiest)
- **Longest session**: sessionId, duration, messageCount

### From history.jsonl (session index)

```
~/.claude/history.jsonl
```

- Session metadata with project paths
- Message preview for quick search
- Timestamp index

### Computable

- Session duration (last - first timestamp)
- Token costs (model × tokens × rate)
- File impact (aggregate tool uses by file)
- Cache efficiency (cache_read / total_input)
- Commit correlation (match timestamps to git log)

---

## Implementation Phases (Prioritized)

### Phase 1: Surface Hidden Data (Foundation)

**Goal**: Show what's already parsed but hidden

1. Show model name per message (opus/sonnet/haiku badge)
2. Show session duration (last - first timestamp)
3. Show full tool paths (Read: `/path/to/file.go`, not just `Read`)
4. Show tool line counts from toolUseResult (Edit: +45 lines)
5. Add per-message cost estimates

**Files**:

- `internal/plugins/conversations/view.go` - Rendering changes
- `internal/adapter/claudecode/adapter.go` - Expose model, duration

### Phase 2: Thinking Blocks (Collapsed)

**Goal**: Surface Claude's reasoning, collapsed by default

1. Stop filtering out thinking blocks in adapter
2. Render collapsed by default: `[thinking] 847 tokens`
3. Add `T` key to expand/collapse current thinking block
4. Show first ~100 chars when collapsed

**Files**:

- `internal/adapter/claudecode/adapter.go` - Remove thinking filter
- `internal/plugins/conversations/view.go` - Thinking render
- `internal/plugins/conversations/plugin.go` - Expand state

### Phase 3: Session Intelligence

**Goal**: At-a-glance session understanding

1. Session summary row: files touched, duration, total tokens
2. Tool impact summary (toggle with `t`)
3. Time-grouped session list (Today, Yesterday, This Week)
4. Enhanced session header with context (cwd, branch, model)

**Files**:

- `internal/plugins/conversations/view.go` - Summary rendering
- `internal/plugins/conversations/summary.go` (new) - Aggregation logic

### Phase 4: Global Analytics

**Goal**: Cross-session usage tracking using stats-cache.json

1. Read `~/.claude/stats-cache.json`
2. Global analytics view (`U` key)
3. Model breakdown with cost estimates
4. Daily activity chart
5. Cache efficiency, peak hours, longest session

**Files**:

- `internal/adapter/claudecode/stats.go` (new) - Stats cache parsing
- `internal/plugins/conversations/analytics.go` (new) - Analytics view
- `internal/plugins/conversations/plugin.go` - View mode

### Phase 5: Two-Pane Layout

**Goal**: Side-by-side sessions + messages for wide terminals

1. Detect terminal width (threshold: ~120 cols)
2. Left pane: session list (30%)
3. Right pane: message thread (70%)
4. `Tab` to toggle sidebar, `h/l` to switch focus
5. Auto-load messages when session selected

**Files**:

- `internal/plugins/conversations/view.go` - Layout logic
- `internal/plugins/conversations/plugin.go` - Focus state

### Phase 6: Enhanced Search & Filter

**Goal**: Find any session by any criteria

1. Multi-dimensional filters: model, date, tokens, active
2. Content search across messages (not just session names)
3. File-based search (sessions that touched X file)
4. Search result counter and highlighting

**Files**:

- `internal/plugins/conversations/plugin.go` - Filter state
- `internal/plugins/conversations/search.go` (new) - Search logic

### Phase 7: Actions & Export

**Goal**: Do something with session data

1. Copy session to clipboard (`c`)
2. Export as markdown (`e`)
3. Show resume command for Claude Code
4. Full message detail modal (Enter on message)

**Files**:

- `internal/plugins/conversations/export.go` (new)
- `internal/plugins/conversations/detail.go` (new)

---

## Key Files Summary

**Modify existing**:

- `internal/adapter/claudecode/adapter.go` - Expose model, thinking, tool details
- `internal/adapter/claudecode/types.go` - Add fields for hidden data
- `internal/plugins/conversations/plugin.go` - View modes, focus, state
- `internal/plugins/conversations/view.go` - All rendering changes

**Create new**:

- `internal/adapter/claudecode/stats.go` - Stats cache parsing
- `internal/plugins/conversations/analytics.go` - Global analytics view
- `internal/plugins/conversations/summary.go` - Session aggregation
- `internal/plugins/conversations/search.go` - Enhanced search
- `internal/plugins/conversations/export.go` - Copy/export actions
- `internal/plugins/conversations/detail.go` - Full message modal

---

## Success Criteria

A developer should be able to:

1. **Instantly understand** what happened in any session (files, cost, duration, model)
2. **See Claude's reasoning** - thinking blocks visible (collapsed by default)
3. **Find any session** by content, time, model, cost, or files touched
4. **See tool impact** - what files were read/written with line counts
5. **Track costs** across sessions and time periods (global analytics)
6. **Navigate fluidly** - two-pane layout, keyboard shortcuts, search
7. **Export and share** - copy conversations, export as markdown

---

## Scope Decision (User Confirmed)

- **Priority**: Both session clarity AND usage analytics
- **Thinking blocks**: Yes, collapsed by default
- **Two-pane layout**: Yes, for wide terminals
