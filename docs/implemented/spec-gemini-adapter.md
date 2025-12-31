## Summary

Implement a Sidecar adapter for **Gemini CLI** (not Antigravity) to display Gemini CLI sessions in the conversations UI.

## Research Findings

### Storage Location

- Sessions: `~/.gemini/tmp/<project_hash>/chats/session-*.json`
- Project hash: `SHA256(absolute_project_path)` (verified)
- File pattern: `session-YYYY-MM-DDTHH-MM-<uuid>.json`

### Session JSON Schema

```json
{
  "sessionId": "UUID",
  "projectHash": "SHA256",
  "startTime": "ISO8601",
  "lastUpdated": "ISO8601",
  "messages": [
    {
      "id": "UUID",
      "timestamp": "ISO8601",
      "type": "user" | "gemini" | "info",
      "content": "string",
      // gemini messages only:
      "model": "gemini-3-pro-preview",
      "tokens": { "input", "output", "cached", "thoughts", "tool", "total" },
      "toolCalls": [{ "id", "name", "args", "result", "status", "displayName" }],
      "thoughts": [{ "subject", "description", "timestamp" }]
    }
  ]
}
```

## Files to Create

### 1. `internal/adapter/geminicli/types.go`

Define Go structs matching the JSON schema:

- `SessionFile` - top-level session document
- `Message` - with type, content, model, tokens, toolCalls, thoughts
- `ToolCall` - tool execution with args/result
- `Thought` - reasoning block
- `TokenStats` - token breakdown
- `SessionMetadata` - internal metadata for Sessions() method

### 2. `internal/adapter/geminicli/adapter.go`

Implement `adapter.Adapter` interface:

```go
const (
    adapterID   = "gemini-cli"
    adapterName = "Gemini CLI"
)
```

**Methods:**

- `ID()` / `Name()` - return constants
- `Detect(projectRoot)` - check if `~/.gemini/tmp/<hash>/chats/` has session files
- `Capabilities()` - return all caps (Sessions, Messages, Usage, Watch)
- `Sessions(projectRoot)` - parse all session files, extract metadata
- `Messages(sessionID)` - parse single session, convert to adapter.Message
- `Usage(sessionID)` - aggregate token stats from messages
- `Watch(projectRoot)` - delegate to watcher

**Key Implementation Details:**

- `projectHash()` - compute SHA256 of absolute path
- `parseSession()` - read JSON file, return SessionFile
- Map `type: "gemini"` to `role: "assistant"`
- Map `type: "user"` to `role: "user"`
- Skip `type: "info"` (system messages like "Request cancelled")
- Extract thinking blocks from `thoughts` array
- Extract tool uses from `toolCalls` array
- Token mapping: `input` + `cached` = InputTokens, `output` = OutputTokens

### 3. `internal/adapter/geminicli/watcher.go`

Watch `~/.gemini/tmp/<hash>/chats/` for changes:

- Use fsnotify (same pattern as claudecode)
- Filter for `.json` files
- Debounce 100ms
- Emit SessionCreated/SessionUpdated/MessageAdded events

### 4. `internal/adapter/geminicli/register.go`

```go
func init() {
    adapter.RegisterFactory(func() adapter.Adapter {
        return New()
    })
}
```

## Files to Modify

### 5. `cmd/sidecar/main.go`

Add blank import:

```go
_ "github.com/sst/sidecar/internal/adapter/geminicli"
```

### 6. `internal/plugins/conversations/view.go`

Add resume command mapping for gemini-cli:

```go
case "gemini-cli":
    return fmt.Sprintf("gemini --resume %s", session.ID)
```

Extend `modelShortName()` for Gemini models:

```go
case strings.Contains(model, "gemini-3-pro"):
    return "Pro"
case strings.Contains(model, "gemini-3-flash"):
    return "Flash"
```

## Cost Calculation

Gemini pricing (per million tokens):

- Gemini 2.0 Flash: $0.10 input / $0.40 output
- Gemini 1.5 Pro: $1.25 input / $5.00 output
- Gemini 1.5 Flash: $0.075 input / $0.30 output

Cache discount: apply 0.25x rate for cached tokens (similar to Claude).

## Testing Checklist

- [ ] Detect() finds sessions for current project
- [ ] Sessions() returns correct count and metadata
- [ ] Messages() parses user/gemini messages correctly
- [ ] Messages() extracts tool calls with inputs/outputs
- [ ] Messages() extracts thinking blocks (thoughts)
- [ ] Usage() aggregates tokens correctly
- [ ] Watch() emits events on session changes
- [ ] Resume command works: `gemini --resume <sessionId>`
