# Cursor Agent Database Format Guide

Reference for Cursor's local SQLite database structure for AI agent conversations.

## Database Location

Conversations are stored in per-session SQLite databases:

```
~/.cursor/chats/<workspace-hash>/<session-id>/store.db
```

- **workspace-hash**: MD5 hash of the absolute project path (32 hex chars)
- **session-id**: UUID of the conversation session (the directory name IS the session UUID)

**Example**:
```
~/.cursor/chats/e4e90b7bcae743c9a1a1a045a49e1b1d/ac437fdf-3e7e-4a54-98a4-7bb04fd3efc6/store.db
```

**Note**: Database uses WAL mode. For consistent reads, include:
- `store.db`
- `store.db-shm`
- `store.db-wal`

## Database Schema

### meta Table

Stores session metadata as hex-encoded JSON.

| Column | Type | Description |
|--------|------|-------------|
| `key` | TEXT | Always "0" for session metadata |
| `value` | TEXT | Hex-encoded JSON |

**Decoding**: `hex.DecodeString(value)` → JSON

**Metadata JSON Structure**:
```json
{
  "agentId": "ac437fdf-3e7e-4a54-98a4-7bb04fd3efc6",
  "latestRootBlobId": "110e9817e50cbdb622b9e70f3c054bdffb74b3bad7dbc73e4096d2c7888815ab",
  "name": "Session Name",
  "mode": "auto-run",
  "createdAt": 1768862746153,
  "lastUsedModel": "claude-4.5-opus-high-thinking"
}
```

| Field | Description |
|-------|-------------|
| `agentId` | Session UUID (same as folder name) |
| `latestRootBlobId` | Hash of root blob in message tree |
| `name` | User-visible session name |
| `mode` | Agent mode: "auto-run", "manual", etc. |
| `createdAt` | Unix timestamp in milliseconds |
| `lastUsedModel` | Model identifier string |

### blobs Table

Stores message content as a Merkle tree structure.

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | SHA-256 hash (64 hex chars) |
| `data` | BLOB | Binary data (JSON or linking blob) |

## Blob Types

Blobs are either **message blobs** (JSON) or **linking blobs** (binary tree structure).

### Message Blobs (JSON)

Start with `{` byte. Contain conversation messages.

**User Message**:
```json
{
  "role": "user",
  "content": "<user_info>...</user_info>\n\n<user_query>\nActual question here\n</user_query>"
}
```

**Assistant Message**:
```json
{
  "id": "1",
  "role": "assistant",
  "content": [
    {
      "type": "reasoning",
      "text": "Let me think about this...",
      "providerOptions": {
        "cursor": {
          "modelName": "claude-4.5-opus-high-thinking"
        }
      },
      "signature": "base64-encoded-signature"
    },
    {
      "type": "text",
      "text": "Here's my response..."
    },
    {
      "type": "tool-call",
      "toolCallId": "toolu_01TMU4XLbonJJkR7gx4mxeN5",
      "toolName": "Read",
      "args": {
        "path": "/path/to/file.go"
      },
      "providerOptions": {
        "cursor": {
          "rawToolCallArgs": "{\"path\": \"/path/to/file.go\"}"
        }
      }
    }
  ]
}
```

**Tool Result Message**:
```json
{
  "role": "tool",
  "id": "toolu_01TMU4XLbonJJkR7gx4mxeN5",
  "content": [
    {
      "type": "tool-result",
      "toolName": "Read",
      "toolCallId": "toolu_01TMU4XLbonJJkR7gx4mxeN5",
      "result": "file contents here...",
      "isError": false
    }
  ]
}
```

**System Message**:
```json
{
  "role": "system",
  "content": "You are an AI coding assistant..."
}
```

### Linking Blobs (Binary)

Start with `0x0A 0x20` bytes. Contain references to child blobs.

**Format**:
```
[0x0A][0x20][32-byte SHA-256 hash] repeated
[optional embedded JSON at end]
```

- `0x0A 0x20` = field tag (protobuf-style)
- 32 bytes = child blob ID as raw bytes
- Pattern repeats for each child
- Optional JSON message embedded after all references

## Content Block Types

| Type | Description | Key Fields |
|------|-------------|------------|
| `text` | Text response | `text` |
| `reasoning` | Thinking/reasoning | `text`, `signature` |
| `tool-call` | Tool invocation | `toolCallId`, `toolName`, `args` |
| `tool-result` | Tool output | `toolCallId`, `result`, `isError` |

## Important Quirks

### All Assistant Messages Have id="1"

**Critical**: Cursor stores all assistant messages with `"id": "1"` in the JSON. This is a Cursor-specific quirk - all assistant messages share the same internal ID. This causes cache collisions if used as a unique identifier.

**Solution**: Use the blob hash (database `id` column) as the message ID instead. The codebase works around this by using the blob hash as the message identifier:
```go
// Use blob hash for uniqueness, not internal JSON id
msgID := blobID[:8]  // Short hash for display
```

### Tool Results as Array

Tool results can be a string or array of content blocks:
```json
// String format
"result": "file contents here"

// Array format
"result": [{"type": "text", "text": "line 1"}, {"type": "text", "text": "line 2"}]
```

### System-Context Messages Filtered

During parsing, messages that contain only system context (no user content) are skipped. This filters out messages that are purely metadata or context-setting without meaningful conversation content.

### User Query Extraction

User messages often contain XML-wrapped context. Extract the actual query:
```go
// Look for <user_query>...</user_query> tags
userQueryRegex := regexp.MustCompile(`<user_query>\s*([\s\S]*?)\s*</user_query>`)
```

### Timestamps Not Stored

Individual messages don't have timestamps. Interpolate from:
- `meta.createdAt` for session start
- File modification time (`store.db` mtime) for session end

## Tree Traversal

To read all messages:

1. Get `latestRootBlobId` from meta table
2. Load all blobs into a map: `map[hash]data`
3. Recursively traverse from root:

```go
func collectMessages(blobs map[string][]byte, blobID string, messages *[]Message) {
    data := blobs[blobID]
    
    // JSON blob = message
    if data[0] == '{' {
        msg := parseMessageBlob(data, blobID)
        *messages = append(*messages, msg)
        return
    }
    
    // Linking blob = tree node
    offset := 0
    for offset+34 <= len(data) {
        if data[offset] != 0x0A || data[offset+1] != 0x20 {
            break
        }
        childID := hex.EncodeToString(data[offset+2 : offset+34])
        collectMessages(blobs, childID, messages)
        offset += 34
    }
    
    // Check for embedded JSON after references
    if offset < len(data) {
        // Find JSON start
        for jsonStart := offset; jsonStart < len(data); jsonStart++ {
            if data[jsonStart] == '{' {
                msg := parseMessageBlob(data[jsonStart:], blobID)
                *messages = append(*messages, msg)
                break
            }
        }
    }
}
```

## Model Identifiers

| Cursor Model ID | Display Name |
|-----------------|--------------|
| `claude-4.5-opus-high-thinking` | Claude Opus 4.5 (Thinking) |
| `claude-4-sonnet` | Claude Sonnet 4 |
| `gpt-4o` | GPT-4o |
| `gpt-4.1` | GPT-4.1 |
| `gemini-2.5-pro` | Gemini 2.5 Pro |

## Useful Queries

### List all sessions in a workspace
```sql
-- From parent directory, list session folders
-- Each folder with store.db is a session
```

### Get session metadata
```sql
SELECT hex(value) FROM meta WHERE key = '0';
-- Decode hex to get JSON
```

### Count messages in session
```sql
SELECT COUNT(*) FROM blobs WHERE data LIKE '{%';
```

### Find assistant messages
```sql
SELECT id, substr(data, 1, 200) 
FROM blobs 
WHERE data LIKE '{\"id\":\"%' AND data LIKE '%\"role\":\"assistant\"%';
```

### Find user messages
```sql
SELECT id, substr(data, 1, 200)
FROM blobs
WHERE data LIKE '{\"role\":\"user\"%';
```

## File Watching

Watch `store.db-wal` for changes to detect new messages:
- Debounce rapid writes (100ms recommended)
- Re-read metadata to get updated `latestRootBlobId`
- Traverse tree to find new messages

## Comparison with Other Formats

| Feature | Cursor | Warp | Claude Code |
|---------|--------|------|-------------|
| Storage | Per-session SQLite | Single SQLite | JSON files |
| Messages | Full content stored | Only queries/tools | Full content |
| Thinking | ✓ Stored | ✗ Not local | ✓ Stored |
| Tool results | ✓ Stored | ✓ In blocks table | ✓ Stored |
| Timestamps | Interpolated | Per-message | Per-message |
| Structure | Merkle tree | Flat tables | Flat JSON |
