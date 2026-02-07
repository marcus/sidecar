# Adapter Creator Guide

This guide defines the implementation and review standard for new Sidecar adapters.

If you are adding an adapter for a new agent, treat this document as the contract.
If you are reviewing an adapter PR, use the checklist section as the approval gate.

## Why This Matters

Adapters are the largest performance risk in Sidecar.

Conversations refresh on watch events, and that hot path can run continuously during active sessions:

`watch event -> coalescer -> session refresh -> adapter.Sessions() -> metadata parsing`

If an adapter does full directory scans and full-file reparses on every change, CPU and FD usage spike quickly.

## Reference Adapters

Use these as implementation references:

- `internal/adapter/claudecode` (incremental JSONL parsing, targeted refresh)
- `internal/adapter/codex` (directory cache, two-pass metadata parsing, global watch scope)
- `internal/adapter/cursor` (SQLite/WAL-aware cache invalidation, FD-safe DB access)

## Required Adapter Contract

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

### Required Session fields

Every session returned from `Sessions()` must set:

- `ID`
- `Name`
- `AdapterID`, `AdapterName`, `AdapterIcon`
- `CreatedAt`, `UpdatedAt`
- `MessageCount`
- `FileSize`

`FileSize` is used for dynamic debounce and huge-session auto-reload protection.

### `Path` and watch strategy

Set `Session.Path` only when Sidecar should use tiered file watching for that adapter.

- File-based append-only adapters (JSONL/log files): set `Path` to absolute file path.
- DB/WAL adapters (like Cursor): prefer adapter-specific `Watch()` and WAL-aware invalidation; do not set `Path` unless tiered watching fully covers your write surface.

## Performance Standards

### 1) Cache metadata and messages aggressively

At minimum:

- Metadata cache key: `path + size + modTime`
- Message cache key: `path + size + modTime`
- For SQLite/WAL: include WAL size+mtime in the key

Avoid unbounded caches. Use bounded LRU behavior and prune stale paths.

### 2) Incremental parsing for append-only formats

For JSONL/event-log adapters, implement incremental parse from byte offset when file grows.

- Cache the last parsed offset.
- Parse only appended bytes.
- Fall back to full parse on shrink/rotation/corruption.
- Preserve immutable head metadata from prior parse.

### 3) Two-pass metadata strategy for large files

If incremental metadata parse is not practical, use head+tail parsing for large files:

- Head pass: ID/CWD/first user message/first timestamp
- Tail pass: latest timestamp/token totals
- Skip middle of large files

### 4) Avoid repeated expensive path work

Resolve project path once per `Sessions()` call (`Abs`/`EvalSymlinks`) and reuse it for all matches.

### 5) Return defensive copies from caches

Do not return cache-owned slices/maps directly. Return copied message/session structures to avoid mutation bugs.

### 6) Keep DB access FD-safe

For SQLite adapters:

- open read-only (`mode=ro`)
- set pool limits (`SetMaxOpenConns(1)`, `SetMaxIdleConns(0)`)
- close rows and DB handles promptly
- avoid opening multiple DB connections per `Messages()` call

## Watching and FD Management Standards

### 1) Prefer directory-level watches

Do not watch per-session files when directory-level watch gives equivalent signals.

### 2) Implement watch scope correctly

If the adapter watches a global path (same location regardless of worktree), implement:

```go
func (a *Adapter) WatchScope() adapter.WatchScope {
    return adapter.WatchScopeGlobal
}
```

This prevents duplicate watchers across worktrees.

### 3) Always emit `SessionID` when known

`Watch()` events should include session ID whenever possible. This enables targeted refresh and avoids full reloads.

### 4) Debounce and non-blocking event sends

- Debounce bursty write events.
- Use buffered channels.
- Use non-blocking sends (`select { case ch <- evt: default: }`) so watchers do not deadlock UI updates.

### 5) Ensure cleanup

All watcher paths must close cleanly on plugin stop. No goroutine or FD leaks.

## Message and Content Rendering Standards

Conversation Flow is the default UI. Adapters must provide rich structured content.

### Required message mapping

Map source records to:

- `Message.Role`
- `Message.Content`
- `Message.ContentBlocks`
- `Message.ToolUses` (legacy compatibility)
- `Message.ThinkingBlocks` (if available)
- `Message.Model` when available

### Tool linking rule

Use consistent `ToolUseID` for `tool_use` and `tool_result` blocks.

If incremental parsing is used, preserve pending tool-link state across cache updates so newly appended `tool_result` blocks can link to previously parsed `tool_use` blocks.

## Optional Interfaces You Should Usually Implement

### `TargetedRefresher`

```go
type TargetedRefresher interface {
    SessionByID(sessionID string) (*Session, error)
}
```

Implement this for adapters that can resolve a session directly. It reduces refresh from O(N sessions) to O(1).

### `ProjectDiscoverer`

Implement when the source format allows discovery of related/deleted worktree sessions beyond current git worktrees.

## Error Handling Rules

- `Detect()`: return `(false, nil)` for missing data directories.
- `Sessions()`: skip corrupt/unreadable entries and continue; only hard-fail on systemic errors.
- `Messages()`: return `nil, nil` for missing session files; fail on parse errors.
- `Watch()`: return `(nil, nil, err)` when watch setup fails.

## Testing and Benchmark Expectations

Each new adapter PR must include tests for correctness + performance behavior.

### Required tests

- relative vs absolute project path behavior in `Detect()`/`Sessions()`
- `Sessions()` sorted by `UpdatedAt desc`
- required session fields populated (`Adapter*`, `FileSize`, and `Path` when applicable)
- cache hit behavior (no reparsing on unchanged files)
- file growth behavior (incremental parse path)
- file shrink/rotation behavior (fallback full parse)
- tool use/result linking (including incremental append cases)
- watcher event emission includes `SessionID`
- watcher cleanup (no leaked closers)

### Benchmark targets (new adapters should be in this range)

- `Messages()` full parse (~1MB): target under 50ms
- `Messages()` incremental append: target under 10ms
- `Messages()` cache hit: target under 1ms
- `Sessions()` on 50 session files: target under 50ms

These are practical targets derived from mature adapter behavior (especially Claude/Codex paths).

## PR Compliance Checklist

Use this list directly in PR review.

### A) Correctness

- [ ] Adapter implements full `adapter.Adapter` contract
- [ ] `Sessions()` sets required identity and timestamp fields
- [ ] `FileSize` is populated for every session
- [ ] `Path` strategy is explicit and correct for adapter type
- [ ] Message role/content mapping is correct
- [ ] `ContentBlocks` include text/tool/thinking data where available
- [ ] Tool result linking is correct (`ToolUseID` parity)

### B) Performance

- [ ] Metadata cache implemented and bounded
- [ ] Message cache implemented and bounded
- [ ] Incremental parse or two-pass large-file strategy implemented
- [ ] No repeated `Abs/EvalSymlinks` work inside per-session loops
- [ ] No duplicate parsing for data that can be extracted in one pass
- [ ] Benchmarks added with realistic fixture sizes

### C) FD / Watching

- [ ] Watch strategy avoids per-file watchers when possible
- [ ] Global adapters implement `WatchScopeProvider`
- [ ] Watch events include `SessionID` when possible
- [ ] Debounce + buffered + non-blocking event send pattern used
- [ ] DB adapters account for WAL changes in invalidation and watch logic
- [ ] Watchers and goroutines close cleanly

### D) Integration

- [ ] Adapter registered via `register.go` and main import
- [ ] Search (if supported) uses adapter `Messages()` path
- [ ] Large-session behavior validated (`FileSize`-driven)
- [ ] Docs include format assumptions and known limitations

## Reviewer Command Pack

Run these during adapter review (adjust `<adapter>`):

```bash
go test ./internal/adapter/<adapter> -run .
go test ./internal/adapter/<adapter> -bench . -benchmem
```

For FD observation during manual runs:

```bash
# macOS/Linux process FD count
lsof -p "$(pgrep -x sidecar | head -n1)" | wc -l
```

## Template: Writing an Adapter-Specific Guide

When adding a new adapter, include a short companion guide with these sections:

1. Storage layout and file/db schema assumptions
2. Project detection and path normalization rules
3. Session extraction strategy (including naming/title heuristics)
4. Message parsing model and ContentBlocks mapping
5. Cache keys and invalidation rules
6. Watch strategy (scope, debounce, event mapping, cleanup)
7. Performance characteristics and benchmark output
8. Known limitations and fallback behavior

That document should make it possible for a reviewer to reason about performance and correctness without reading every line of adapter code.
