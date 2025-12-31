# Adapter Creator Guide

This guide describes how to add a new AI session adapter to Sidecar.

## Overview

Adapters live in `internal/adapter/<name>` and implement the `adapter.Adapter` interface:

```go
// internal/adapter/adapter.go

type Adapter interface {
	ID() string
	Name() string
	Detect(projectRoot string) (bool, error)
	Capabilities() CapabilitySet
	Sessions(projectRoot string) ([]Session, error)
	Messages(sessionID string) ([]Message, error)
	Usage(sessionID string) (*UsageStats, error)
	Watch(projectRoot string) (<-chan Event, error)
}
```

Sidecar discovers adapters via `adapter.RegisterFactory` and `adapter.DetectAdapters`.

## File Layout

```
internal/adapter/<name>/
  adapter.go       // main implementation
  types.go         // JSONL payload types
  watcher.go       // fsnotify watcher (if supported)
  register.go      // init() registers factory
  *_test.go        // unit tests + fixture parsing
```

## Required Fields

When building sessions, populate adapter identity:

```go
adapter.Session{
	ID:          meta.SessionID,
	Name:        shortID(meta.SessionID),
	Slug:        meta.SessionID, // optional: short display slug if you have one
	AdapterID:   "<your-id>",
	AdapterName: "<Your Name>",
	// ... timestamps, tokens, counts
}
```

These are used for badges, filtering, and resume commands in the conversations UI.

## Step-by-step

### 1) Define adapter constants

```go
const (
	adapterID   = "my-adapter"
	adapterName = "My Adapter"
)
```

Pick a stable `adapterID` (it becomes part of persisted UI state like filters).

### 2) Implement Detect

Detect should return true only when sessions for `projectRoot` exist. Prefer:
- `filepath.Abs` + `filepath.Rel` for stable path matching
- `filepath.EvalSymlinks` to avoid false negatives
- graceful handling when data directories do not exist

### 3) Implement Sessions

Parse all session files, extract:
- `SessionID`
- `FirstMsg` and `LastMsg`
- `MsgCount` (user + assistant messages)
- `TotalTokens` (if available)

Sort by `UpdatedAt` descending.

### 4) Implement Messages

Return ordered `adapter.Message` values with:
- `Role`: user or assistant
- `Content`: concatenated content blocks
- `ToolUses`: tool calls and outputs
- `ThinkingBlocks`: reasoning summaries (if present)
- `TokenUsage`: map token_count events to the next assistant message
- `Model`: from your session metadata

### 5) Implement Usage

Aggregate per-message token usage, and optionally fall back to totals from a session summary record.

### 6) Implement Watch (optional but recommended)

Use `fsnotify` and:
- add watchers for nested directories (fsnotify is non-recursive)
- debounce rapid writes
- map file events to `adapter.Event` types

### 7) Register the adapter

Add a `register.go` with an init hook:

```go
package myadapter

import "github.com/sst/sidecar/internal/adapter"

func init() {
	adapter.RegisterFactory(func() adapter.Adapter {
		return New()
	})
}
```

And ensure the package is imported (blank import) in `cmd/sidecar/main.go`:

```go
import (
	_ "github.com/sst/sidecar/internal/adapter/myadapter"
)
```

## UI Integration Notes

- Conversations view shows adapter badges using `AdapterID`/`AdapterName`.
- `resumeCommand()` is adapter-specific; add a mapping in `internal/plugins/conversations/view.go` if your tool supports resuming sessions.
- `modelShortName()` should be extended if your models are non-Claude.

## Testing Checklist

- Detect() matches both absolute and relative project roots
- Sessions() sorts by UpdatedAt
- Messages() attaches tool uses and token usage correctly
- Usage() matches message totals
- Watch() emits create/write events (if supported)

## Minimal Skeleton

```go
package myadapter

type Adapter struct {
	// data dir, indexes, etc.
}

func New() *Adapter { /* ... */ }
func (a *Adapter) ID() string { return adapterID }
func (a *Adapter) Name() string { return adapterName }
func (a *Adapter) Detect(projectRoot string) (bool, error) { /* ... */ }
func (a *Adapter) Capabilities() adapter.CapabilitySet { /* ... */ }
func (a *Adapter) Sessions(projectRoot string) ([]adapter.Session, error) { /* ... */ }
func (a *Adapter) Messages(sessionID string) ([]adapter.Message, error) { /* ... */ }
func (a *Adapter) Usage(sessionID string) (*adapter.UsageStats, error) { /* ... */ }
func (a *Adapter) Watch(projectRoot string) (<-chan adapter.Event, error) { /* ... */ }
```
