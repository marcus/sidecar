---
name: create-adapter
description: >
  Create conversation adapters for importing AI chat history from different
  tools (Claude Code, Cursor, Warp, Codex, etc.). Covers the adapter.Adapter
  interface, caching strategies, incremental parsing, watch/FD management, and
  performance standards. Use when creating a new adapter, modifying adapter
  behavior, or debugging adapter performance issues. See references/ for
  Cursor DB and Warp SQLite schema details.
---

# Create Adapter

## Why Performance Matters

Adapters are the largest performance risk in Sidecar. Conversations refresh on watch events in a hot path that runs continuously during active sessions:

```
watch event -> coalescer -> session refresh -> adapter.Sessions() -> metadata parsing
```

If an adapter does full directory scans and full-file reparses on every change, CPU and FD usage spike quickly.

## Reference Adapters

Study these before writing a new adapter:
- `internal/adapter/claudecode` - Incremental JSONL parsing, targeted refresh
- `internal/adapter/codex` - Directory cache, two-pass metadata parsing, global watch scope
- `internal/adapter/cursor` - SQLite/WAL-aware cache invalidation, FD-safe DB access
- `internal/adapter/pi` - Global scope, JSONL, CWD-based filtering, session classification, message prefix stripping

## Required Interface

All adapters implement `adapter.Adapter`:

```go
type Adapter interface {
    ID() string
    Name() string
    Icon() string
    Detect(projectRoot string) (bool, error)
    Capabilities() CapabilitySet
    Sessions(projectRoot string) ([]Session, error)
    Messages(sessionID string) ([]Message, error)
    Usage(sessionID string) (*UsageStats, error)
    Watch(projectRoot string) (<-chan Event, io.Closer, error)
}
```

### Required Session Fields

Every session from `Sessions()` must set:
- `ID`, `Name`
- `AdapterID`, `AdapterName`, `AdapterIcon`
- `CreatedAt`, `UpdatedAt`
- `MessageCount`, `FileSize`

`FileSize` is used for dynamic debounce and huge-session auto-reload protection.

### Path and Watch Strategy

Set `Session.Path` only when Sidecar should use tiered file watching for that adapter:
- **File-based append-only** (JSONL/log): set `Path` to absolute file path — this opts into TieredWatcher with HOT/COLD/FROZEN tiers
- **DB/WAL adapters** (Cursor, Warp, Kiro): prefer adapter-specific `Watch()` with WAL-aware invalidation; do not set `Path` unless tiered watching covers your write surface

**FROZEN tier**: File-based sessions with `Path` set automatically benefit from the FROZEN tier. Sessions unchanged for 24 hours (`FrozenThreshold`) are excluded from cold polling entirely — zero syscalls. They unfreeze when promoted to HOT (e.g., user selects the session). This is critical for adapters with thousands of session files; without it, `pollColdSessions()` does one `os.Stat()` per file every 30 seconds.

## Performance Standards

### 1) Cache metadata and messages aggressively

Minimum cache keys:
- Metadata: `path + size + modTime`
- Messages: `path + size + modTime`
- SQLite/WAL: include WAL size+mtime in the key

Use bounded LRU behavior. Prune stale paths.

### 2) Incremental parsing for append-only formats

For JSONL/event-log adapters:
- Cache last parsed byte offset
- Parse only appended bytes
- Fall back to full parse on shrink/rotation/corruption
- Preserve immutable head metadata from prior parse

### 3) Two-pass metadata for large files

When incremental metadata parse is impractical:
- Head pass: ID, CWD, first user message, first timestamp
- Tail pass: latest timestamp, token totals
- Skip middle of large files

### 4) Avoid repeated expensive path work

Resolve project path once per `Sessions()` call (`Abs`/`EvalSymlinks`), reuse for all matches.

### 5) Return defensive copies from caches

Never return cache-owned slices/maps directly. Copy message/session structures to avoid mutation bugs.

### 6) Keep DB access FD-safe

For SQLite adapters:
- Open read-only (`mode=ro`)
- `SetMaxOpenConns(1)`, `SetMaxIdleConns(0)`
- Close rows and DB handles promptly
- Avoid multiple DB connections per `Messages()` call

## Watching and FD Management

### 1) Prefer directory-level watches
Do not watch per-session files when directory-level watch gives equivalent signals.

### 2) Implement watch scope
If adapter watches a global path (same location regardless of worktree):
```go
func (a *Adapter) WatchScope() adapter.WatchScope {
    return adapter.WatchScopeGlobal
}
```
This prevents duplicate watchers across worktrees.

### 3) Always emit SessionID when known
Watch events should include session ID for targeted refresh (avoids full reloads).

### 4) Debounce and non-blocking sends
- Debounce bursty write events
- Use buffered channels
- Non-blocking sends: `select { case ch <- evt: default: }`

### 5) Leverage FROZEN tier for file-based adapters
File-based adapters that set `Session.Path` get TieredWatcher's three-tier system (HOT → COLD → FROZEN). Sessions unchanged for 24h are frozen and cost zero polling overhead. This is the primary defense against CPU spikes with thousands of session files. If your adapter has file-based sessions, always set `Path` — the FROZEN tier scales automatically.

### 6) Ensure cleanup
All watcher paths must close cleanly on plugin stop. No goroutine or FD leaks.

## Message and Content Rendering

Adapters must provide rich structured content for Conversation Flow UI.

### Required message mapping
Map source records to:
- `Message.Role`, `Message.Content`, `Message.ContentBlocks`
- `Message.ToolUses` (legacy compatibility)
- `Message.ThinkingBlocks` (if available)
- `Message.Model` when available

### Tool linking rule
Use consistent `ToolUseID` for `tool_use` and `tool_result` blocks. If incremental parsing is used, preserve pending tool-link state across cache updates.

## Optional Interfaces

### TargetedRefresher
```go
type TargetedRefresher interface {
    SessionByID(sessionID string) (*Session, error)
}
```
Reduces refresh from O(N sessions) to O(1). Implement when adapter can resolve a session directly.

### ProjectDiscoverer
Implement when source format allows discovery of sessions beyond current git worktrees.

## Error Handling

- `Detect()`: return `(false, nil)` for missing data directories
- `Sessions()`: skip corrupt/unreadable entries and continue; hard-fail only on systemic errors
- `Messages()`: return `nil, nil` for missing session files; fail on parse errors
- `Watch()`: return `(nil, nil, err)` when watch setup fails

## Benchmark Targets

New adapters should meet these performance targets:
- `Messages()` full parse (~1MB): under 50ms
- `Messages()` incremental append: under 10ms
- `Messages()` cache hit: under 1ms
- `Sessions()` on 50 session files: under 50ms

## Testing Requirements

Required tests for every new adapter:
- Relative vs absolute project path behavior in `Detect()`/`Sessions()`
- `Sessions()` sorted by `UpdatedAt desc`
- Required session fields populated (`Adapter*`, `FileSize`, `Path` when applicable)
- Cache hit behavior (no reparsing on unchanged files)
- File growth behavior (incremental parse path)
- File shrink/rotation behavior (fallback full parse)
- Tool use/result linking (including incremental append cases)
- Watcher event emission includes `SessionID`
- Watcher cleanup (no leaked closers)

Run tests:
```bash
go test ./internal/adapter/<adapter> -run .
go test ./internal/adapter/<adapter> -bench . -benchmem
```

## PR Compliance Checklist

### A) Correctness
- [ ] Full `adapter.Adapter` contract implemented
- [ ] `Sessions()` sets required identity and timestamp fields
- [ ] `FileSize` populated for every session
- [ ] `Path` strategy explicit and correct for adapter type
- [ ] Message role/content mapping correct
- [ ] `ContentBlocks` include text/tool/thinking data
- [ ] Tool result linking correct (`ToolUseID` parity)

### B) Performance
- [ ] Metadata cache implemented and bounded
- [ ] Message cache implemented and bounded
- [ ] Incremental parse or two-pass strategy implemented
- [ ] No repeated `Abs/EvalSymlinks` in per-session loops
- [ ] No duplicate parsing for single-pass data
- [ ] Benchmarks added with realistic fixtures

### C) FD / Watching
- [ ] Directory-level watches preferred
- [ ] Global adapters implement `WatchScopeProvider`
- [ ] Watch events include `SessionID`
- [ ] Debounce + buffered + non-blocking send pattern
- [ ] DB adapters account for WAL in invalidation/watch
- [ ] Watchers and goroutines close cleanly

### D) Integration
- [ ] Adapter registered via `register.go` and main import
- [ ] Search uses adapter `Messages()` path
- [ ] Large-session behavior validated (`FileSize`-driven)

## Session Classification

Adapters can classify sessions by setting `SessionCategory` on `adapter.Session`. The conversations plugin supports category filtering (f menu: i/r/s keys) and a quick toggle (C key).

### Category Constants

Defined in `internal/adapter/adapter.go`:
- `adapter.SessionCategoryInteractive` — user-initiated interactive sessions
- `adapter.SessionCategoryCron` — automated/scheduled sessions
- `adapter.SessionCategorySystem` — system/gateway sessions

### Implementation Guidelines

- Classify during metadata parsing (zero extra I/O) — extract category from the first user message or session header
- Only set `SessionCategory` if the adapter has meaningful categories. Don't set it if all sessions are the same type
- If the category filter is active and `SessionCategory` is empty, sessions pass through (non-breaking for adapters that don't classify)
- Gateway/system messages may need special classification — e.g., "System: WhatsApp gateway connected" is actually interactive, not system. Check for known preamble patterns before defaulting to system category

### Example (from Pi adapter)

```go
func extractSessionMetadata(firstUserMessage string) (category, cronJobName, sourceChannel string) {
    if strings.HasPrefix(firstUserMessage, "[cron:") {
        return adapter.SessionCategoryCron, extractCronJobName(firstUserMessage), ""
    }
    if strings.HasPrefix(firstUserMessage, "System:") {
        if strings.Contains(firstUserMessage, "WhatsApp gateway") {
            return adapter.SessionCategoryInteractive, "", "whatsapp"
        }
        return adapter.SessionCategorySystem, "", ""
    }
    return adapter.SessionCategoryInteractive, "", detectSourceChannel(firstUserMessage)
}
```

## Rich Metadata Fields

Optional fields on `adapter.Session` for richer display and filtering:

- `CronJobName string` — for cron/scheduled sessions; used as session name when set
- `SourceChannel string` — for multi-channel adapters (e.g., "telegram", "whatsapp", "direct")

Optional field on `adapter.Message`:

- `SourceLabel string` — per-message source attribution badge (e.g., "[TG] Marcus", "[WA]", "[cron] job-name")

Set these during parsing when the source format contains channel/origin metadata. The conversations plugin and conversation flow UI use these for display.

## Message Content Cleaning

For adapters whose source format embeds structured prefixes in user messages (e.g., channel tags, cron headers), strip them during parsing to keep the conversation view clean.

### Pattern

1. Extract metadata (source label, channel, category) from the raw message prefix
2. Strip the prefix from `Message.Content` and text `ContentBlocks`
3. Store the extracted label in `Message.SourceLabel` for badge display

```go
// In processMessageLine for user messages:
content, _, _, contentBlocks := parseContent(raw.Message.Content)
sourceLabel := extractSourceLabel(content)    // "[TG] Marcus"
content = stripMessagePrefix(content)          // clean body only
for i := range contentBlocks {
    if contentBlocks[i].Type == "text" {
        contentBlocks[i].Text = stripMessagePrefix(contentBlocks[i].Text)
    }
}
msg := adapter.Message{
    Content:       content,
    ContentBlocks: contentBlocks,
    SourceLabel:   sourceLabel,
}
```

This keeps `Content` human-readable while preserving origin metadata in `SourceLabel`.

## Global Adapter Gotchas

Lessons learned from building global-scope adapters (Pi, Codex):

### CWD-based Project Filtering

Global adapters (`WatchScopeGlobal`) store sessions in a single directory regardless of project. They must filter by CWD matching `projectRoot` in `Sessions()`:

- Resolve `projectRoot` once per `Sessions()` call (Abs + EvalSymlinks)
- Use a fast CWD cache that reads only the first JSONL line (session header) to avoid full-file parses for non-matching sessions
- Match with `filepath.Rel` — a session matches if its CWD is equal to or a subdirectory of the project root

### Category Filter Interaction

- The conversations plugin category filter only filters sessions that HAVE a `SessionCategory` set — empty passes through
- Don't enable category filter by default in the plugin — it breaks non-classifying adapters
- When adding classification to a new adapter, test that existing adapters without categories still display correctly

### Project Switching

Global adapters need to handle project switching gracefully:

- The watcher persists across project switches, but `Sessions()` gets called with a new `projectRoot`
- Directory listing caches with short TTLs (e.g., 500ms) naturally handle this
- CWD caches keyed by file path are project-agnostic and don't need clearing
- Session index maps (`sessionID -> path`) should be rebuilt on each `Sessions()` call to reflect the new project filter

### Watcher Persistence

- Global adapter watchers are created once and shared across project switches (the plugin deduplicates by adapter ID + WatchScope)
- Watch events don't include project context — the coalescer triggers a full `Sessions()` refresh which applies the current project filter
- Ensure watch goroutines don't hold stale project references

## Schema References

See `references/cursor-db-format.md` for Cursor's per-session SQLite database structure (Merkle tree blobs, hex-encoded metadata, WAL considerations).

See `references/warp-sqlite-schema.md` for Warp's single SQLite database structure (ai_queries, agent_conversations, blocks tables, protobuf tasks).
