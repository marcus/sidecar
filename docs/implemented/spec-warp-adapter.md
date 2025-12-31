# Warp Terminal Adapter Implementation Plan

## Executive Summary

Create a new adapter for Warp terminal's AI Agent Mode that reads from Warp's local SQLite database. **Critical limitation**: Warp does not store AI assistant responses locally - only user queries, tool executions, and usage metadata.

## Warp Data Model Analysis

### Database Location

```
~/Library/Group Containers/2BBY89MBSN.dev.warp/Library/Application Support/dev.warp.Warp-Stable/warp.sqlite
```

### Key Tables

| Table                 | Purpose           | Key Fields                                                                     |
| --------------------- | ----------------- | ------------------------------------------------------------------------------ |
| `ai_queries`          | User queries      | `conversation_id`, `input` (JSON), `model_id`, `working_directory`, `start_ts` |
| `agent_conversations` | Usage metadata    | `conversation_id`, `conversation_data` (JSON with tokens/credits)              |
| `agent_tasks`         | Task lists        | `task` (protobuf blob), `conversation_id`                                      |
| `blocks`              | Terminal commands | `ai_metadata` (links to conversation), `stylized_command/output`               |

### Data Available

- User query text and context
- Project/working directory for filtering
- Model used (claude-4-5-opus, gpt-5-1-high-reasoning, etc.)
- Token usage per model with categories
- Tool call counts and commands executed
- Credit/cost spent

### Data NOT Available

- AI assistant text responses (not stored locally)
- Reasoning/thinking content
- Full conversation history

## Implementation Approach

Given the limitation, the adapter will:

1. Show sessions with metadata (project, model, timestamps)
2. Display user queries as messages
3. Synthesize "assistant" messages from tool executions (commands run)
4. Provide accurate token/usage stats

## File Structure

```
internal/adapter/warp/
├── adapter.go      # Main implementation
├── types.go        # SQLite row types, JSON payloads
├── watcher.go      # FSNotify on SQLite-wal
├── register.go     # init() registration
└── warp_test.go    # Unit tests
```

## Critical Implementation Notes

1. **Read-only SQLite**: Use `?mode=ro` connection, copy WAL for consistent reads
2. **Project Filtering**: Match `working_directory` to `projectRoot` (with symlink resolution)
3. **ANSI Stripping**: `stylized_command/output` contain ANSI escape codes
4. **Protobuf Tasks**: Optional - decode for task display or skip initially
5. **Model Names**: Map Warp model IDs (e.g., "claude-4-5-opus-thinking") to display names

## Capabilities

```go
CapSessions:  true  // List conversations by project
CapMessages:  true  // User queries + tool executions
CapUsage:     true  // Token counts from conversation_data
CapWatch:     true  // Watch SQLite WAL for changes
```

---

## Epic: Warp Adapter Implementation

### Story 1: Core Adapter Scaffold

**Files**: `adapter.go`, `types.go`, `register.go`

Create basic adapter structure:

- `ID()` returns "warp"
- `Name()` returns "Warp"
- `Detect()` checks if SQLite exists and has ai_queries for project
- Define SQLite row structs and JSON types

Types needed:

```go
type AIQuery struct {
    ID               int
    ExchangeID       string
    ConversationID   string
    Input            string // JSON
    OutputStatus     string
    ModelID          string
    WorkingDirectory string
    StartTS          time.Time
}

type QueryInput struct {
    Query struct {
        Text    string         `json:"text"`
        Context []QueryContext `json:"context"`
    } `json:"Query"`
}

type ConversationData struct {
    TokenUsage []TokenUsage `json:"token_usage"`
    CreditsSpent float64    `json:"credits_spent"`
}
```

### Story 2: Sessions Implementation

**Files**: `adapter.go`

Implement `Sessions(projectRoot)`:

1. Open SQLite read-only with WAL
2. Query distinct conversations:

   ```sql
   SELECT DISTINCT conversation_id, working_directory, model_id,
          MIN(start_ts) as first_msg, MAX(start_ts) as last_msg,
          COUNT(*) as msg_count
   FROM ai_queries
   WHERE working_directory LIKE ? OR working_directory = ?
   GROUP BY conversation_id
   ORDER BY last_msg DESC
   ```

3. Filter by project path (support subdirectories)
4. Join with `agent_conversations` for token totals
5. Return `[]adapter.Session` sorted by UpdatedAt

### Story 3: Messages Implementation

**Files**: `adapter.go`

Implement `Messages(sessionID)`:

1. Query ai_queries for conversation_id
2. Parse input JSON to extract user query text
3. Query blocks with matching conversation_id in ai_metadata
4. Construct messages:
   - User messages from ai_queries.input
   - Tool use messages from blocks (command + output)
5. Sort chronologically

### Story 4: Usage Implementation

**Files**: `adapter.go`

Implement `Usage(sessionID)`:

1. Query agent_conversations for conversation_id
2. Parse conversation_data JSON
3. Extract token_usage array
4. Map to adapter.UsageStats:
   - InputTokens, OutputTokens per model
   - Total cost from credits_spent

### Story 5: Watch Implementation

**Files**: `watcher.go`

Implement `Watch(projectRoot)`:

1. Watch SQLite WAL file for changes
2. Debounce rapid writes (100ms)
3. Re-query for new/updated sessions on change
4. Emit adapter.EventSessionUpdated

### Story 6: UI Integration

**Files**: `internal/plugins/conversations/view.go`

- Add model name mapping for Warp models
- Add resume command (if Warp supports CLI resume)
- Handle limited message content gracefully

### Story 7: Testing

**Files**: `warp_test.go`

- Test Detect with mock SQLite
- Test Sessions project filtering
- Test Messages parsing
- Test Usage calculation
- Test ANSI stripping from blocks

---

## Decisions Made

1. **Message Synthesis**: Synthesize assistant messages from tool calls - show "[Executed: git status]" style
2. **Protobuf Tasks**: Decode agent_tasks to show task titles and subtasks
3. **Resume Support**: TBD - research Warp CLI for resume capability

---

## TD Epic Creation (run after plan approval)

```bash
# Create epic
td create "Warp Terminal Adapter" --type epic --priority P2 \
  --desc "Create adapter for Warp terminal AI Agent Mode. Reads from SQLite at ~/Library/Group Containers/2BBY89MBSN.dev.warp/. Key limitation: Warp doesn't store AI responses locally."

# Get epic ID and create stories as children
EPIC_ID=$(td list --type epic | grep "Warp" | awk '{print $1}')

td create "Core Adapter Scaffold" --parent $EPIC_ID --points 3 \
  --desc "Create adapter.go, types.go, register.go with ID/Name/Detect methods. Define SQLite row structs (AIQuery, ConversationData, Block) and JSON types for input parsing."

td create "Sessions Implementation" --parent $EPIC_ID --points 5 \
  --desc "Implement Sessions(projectRoot): Open SQLite read-only, query distinct conversations by working_directory, join agent_conversations for token totals, filter by project path with symlink resolution."

td create "Messages Implementation" --parent $EPIC_ID --points 5 \
  --desc "Implement Messages(sessionID): Parse ai_queries input JSON for user text, query blocks with ai_metadata for tool executions, synthesize assistant messages like '[Executed: git status]', strip ANSI codes from stylized_command/output."

td create "Usage Implementation" --parent $EPIC_ID --points 2 \
  --desc "Implement Usage(sessionID): Parse agent_conversations.conversation_data JSON, extract token_usage array, map to adapter.UsageStats with InputTokens/OutputTokens per model."

td create "Watch Implementation" --parent $EPIC_ID --points 3 \
  --desc "Implement Watch(projectRoot): Watch SQLite WAL file (warp.sqlite-wal) for changes, debounce 100ms, re-query for updated sessions, emit adapter.EventSessionUpdated."

td create "Protobuf Task Decoding" --parent $EPIC_ID --points 3 \
  --desc "Decode agent_tasks.task protobuf blob. Reverse-engineer schema from hex dump: task_id (UUID), title (string), subtasks (UUIDs), timestamps. Use google.golang.org/protobuf."

td create "UI Integration" --parent $EPIC_ID --points 2 \
  --desc "Update internal/plugins/conversations/view.go: Add modelShortName mapping for Warp models (claude-4-5-opus-thinking, gpt-5-1-high-reasoning). Research Warp CLI for resume command."

td create "Testing" --parent $EPIC_ID --points 3 \
  --desc "Create warp_test.go: Test Detect with mock SQLite, Sessions project filtering, Messages parsing with ANSI stripping, Usage calculation, protobuf decoding."
```
