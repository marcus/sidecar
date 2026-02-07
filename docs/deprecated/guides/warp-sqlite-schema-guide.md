# Warp Terminal SQLite Schema Guide

Reference for the Warp terminal's local SQLite database structure, focused on AI Agent Mode data.

## Database Location

```
~/Library/Group Containers/2BBY89MBSN.dev.warp/Library/Application Support/dev.warp.Warp-Stable/warp.sqlite
```

**Note**: Database uses WAL mode. For consistent reads, copy all three files:
- `warp.sqlite`
- `warp.sqlite-shm`
- `warp.sqlite-wal`

## Key Tables for AI Conversations

### ai_queries

Stores user queries sent to AI. One row per exchange (turn).

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Primary key |
| `exchange_id` | TEXT | Unique ID for this exchange |
| `conversation_id` | TEXT | Groups exchanges into conversations |
| `start_ts` | DATETIME | Timestamp of query |
| `input` | TEXT | JSON array with Query object |
| `working_directory` | TEXT | Project path (for filtering) |
| `output_status` | TEXT | "Completed", etc. |
| `model_id` | TEXT | Model used (see Model IDs below) |
| `planning_model_id` | TEXT | Planning model if different |
| `coding_model_id` | TEXT | Coding model if different |

**Input JSON Structure**:
```json
[{
  "Query": {
    "text": "user's question here",
    "context": [
      {"Directory": {"pwd": "/path/to/project", "home_dir": "..."}},
      {"Git": {"head": "main"}},
      {"ProjectRules": {"root_path": "...", "active_rules": [...]}},
      {"CurrentTime": {"current_time": "2025-12-27T10:12:11-08:00"}},
      {"ExecutionEnvironment": {"os": {"category": "MacOS"}, "shell_name": "zsh"}},
      {"Codebase": {"path": "/path", "name": "project-name"}}
    ],
    "referenced_attachments": {}
  }
}]
```

**Note**: Only the first exchange in a conversation has the full Query. Subsequent exchanges have empty `[]` input.

### agent_conversations

Stores usage metadata per conversation.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Primary key |
| `conversation_id` | TEXT | Links to ai_queries |
| `conversation_data` | TEXT | JSON with usage stats |
| `last_modified_at` | TIMESTAMP | Auto-updated on change |

**conversation_data JSON Structure**:
```json
{
  "server_conversation_token": "uuid",
  "conversation_usage_metadata": {
    "was_summarized": false,
    "context_window_usage": 0.36727,
    "credits_spent": 126.82,
    "credits_spent_for_last_block": 35.71,
    "token_usage": [
      {
        "model_id": "claude 4.5 opus (thinking)",
        "warp_tokens": 605182,
        "byok_tokens": 0,
        "warp_token_usage_by_category": {
          "primary_agent": 577163,
          "full_terminal_use": 28019
        }
      }
    ],
    "tool_usage_metadata": {
      "run_command_stats": {"count": 36, "commands_executed": 32},
      "read_files_stats": {"count": 6},
      "grep_stats": {"count": 3},
      "file_glob_stats": {"count": 2},
      "apply_file_diff_stats": {"count": 3, "lines_added": 148, "lines_removed": 15},
      "read_shell_command_output_stats": {"count": 15}
    }
  }
}
```

### agent_tasks

Stores task lists created by AI agent. **Protobuf encoded**.

**Note**: The agent_tasks table stores task lists in protobuf format. Currently, the Warp adapter does not decode these blobs. This section is provided as reference for future implementations or direct SQL exploration.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Primary key |
| `conversation_id` | TEXT | Links to conversation |
| `task_id` | TEXT | Unique task ID |
| `task` | BLOB | Protobuf-encoded task data |
| `last_modified_at` | TIMESTAMP | Auto-updated |

**Protobuf Schema** (reverse-engineered from hex dump):
```
Field 1 (0x0a): task_id (string, UUID)
Field 2 (0x12): title (string)
Field 5 (0x2a): subtasks (repeated message)
  - Contains nested task_id, title
Field 13 (0x6a): parent_task_id (string, UUID)
Field 14 (0x72): timestamps
```

### blocks

Terminal command blocks. Links to AI via `ai_metadata`.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Primary key |
| `pane_leaf_uuid` | BLOB | Terminal pane reference |
| `stylized_command` | BLOB | ANSI-encoded command text |
| `stylized_output` | BLOB | ANSI-encoded output text |
| `pwd` | TEXT | Working directory |
| `exit_code` | INTEGER | Command exit code |
| `start_ts` | DATETIME | Command start time |
| `completed_ts` | DATETIME | Command completion time |
| `ai_metadata` | TEXT | JSON linking to conversation |

**ai_metadata JSON Structure**:
```json
{
  "action_id": "toolu_01SGHZqqhZ82QCPEjYaRYK1C",
  "conversation_id": "e05f25f4-4e57-411a-b126-f4c9ebca6781",
  "conversation_phase": {
    "interactive": {"must_not_suggest_create_plan": true}
  },
  "long_running_control_state": null,
  "has_agent_written_to_block": false
}
```

**ANSI Stripping**: `stylized_command` and `stylized_output` contain ANSI escape codes (e.g., `[1m`, `[0m`). Strip with regex: `\x1b\[[0-9;]*m`

## Model IDs

**Note**: This is a non-exhaustive list of common models. The adapter uses `ModelDisplayNames` map for display, but Warp may report additional model variants. Unknown models display as-is from the database.

| Warp Model ID | Display Name |
|---------------|--------------|
| `claude-4-5-opus` | Claude Opus 4.5 |
| `claude-4-5-opus-thinking` | Claude Opus 4.5 (Thinking) |
| `gpt-5-1-high-reasoning` | GPT-5 |

## Critical Limitation

**Warp does NOT store AI assistant text responses locally.**

Only available:
- User queries (ai_queries.input)
- Tool executions (blocks with ai_metadata)
- Usage statistics (agent_conversations)

Not stored locally:
- AI reasoning/response text
- Thinking content
- Full conversation transcript

## Useful Queries

### Important Query Notes
- All queries must use read-only access to avoid disrupting the live database
- Join ai_queries with agent_conversations when you need usage metrics
- When querying blocks, note that `stylized_command` and `stylized_output` contain ANSI escape codes (strip with regex `\x1b\[[0-9;]*m`)

### List conversations by project
```sql
SELECT DISTINCT conversation_id, working_directory, model_id,
       MIN(start_ts) as first_msg, MAX(start_ts) as last_msg,
       COUNT(*) as exchange_count
FROM ai_queries
WHERE working_directory LIKE ? OR working_directory = ?
-- With parameters: ('/path/to/project%', '/path/to/project')
GROUP BY conversation_id
ORDER BY last_msg DESC;
```

### Get blocks for a conversation
```sql
SELECT id, stylized_command, stylized_output, exit_code, ai_metadata
FROM blocks
WHERE ai_metadata LIKE '%"conversation_id":"<uuid>"%'
ORDER BY start_ts;
```

### Get usage for a conversation
```sql
SELECT conversation_data
FROM agent_conversations
WHERE conversation_id = '<uuid>';
```

## Other Notable Tables

| Table | Purpose |
|-------|---------|
| `terminal_panes` | Terminal session state, `conversation_ids` field |
| `commands` | Command history (not AI-specific) |
| `projects` | Known project paths |
| `workspace_metadata` | Workspace navigation history |
| `mcp_server_installations` | MCP server configs |

## Limitations

- **No assistant responses stored**: Warp does not store AI response text locally
- **Limited task data**: agent_tasks uses protobuf encoding and is not currently decoded
- **Debounce on watch**: SQLite WAL watch debounces with 100ms delay

## File Watching

Watch `warp.sqlite-wal` for changes. Debounce rapid writes (100ms recommended).
