# Plan: Add Version Check on Startup

## Summary

Add automatic version checking that queries GitHub releases on startup, compares with the running version, and notifies the user with easy update instructions if outdated.

## Key Files to Modify/Create

### New Files

- `internal/version/version.go` - Core version check logic, GitHub API call
- `internal/version/semver.go` - Semver parsing and comparison
- `internal/version/cache.go` - Cache checks to avoid API rate limits (6h TTL)
- `internal/version/checker.go` - Bubble Tea command for async check
- `internal/version/version_test.go` - Unit tests

### Modified Files

- `cmd/sidecar/main.go` - Pass current version to app model
- `internal/app/model.go` - Add version field, trigger check in `Init()`
- `internal/app/update.go` - Handle `UpdateAvailableMsg`
- `internal/app/view.go` - Render update notification

## Implementation Steps

### 1. Create `internal/version/` Package

**version.go** - Fetch latest release from GitHub API:

```go
// Check fetches latest release and compares versions
func Check(currentVersion string) CheckResult
```

- Uses `https://api.github.com/repos/sst/sidecar/releases/latest`
- 5-second timeout to avoid blocking
- Skips check for dev versions (`devel`, empty, `devel+abc123`)

**semver.go** - Version comparison:

```go
func isNewer(latest, current string) bool
func parseSemver(v string) [3]int  // handles v1.2.3, 1.2.3, v1.2.3-beta
```

**cache.go** - Avoid hitting API every startup:

- Store in `~/.config/sidecar/version_cache.json`
- 6-hour TTL before re-checking
- Silent failure (don't break app if cache fails)

**checker.go** - Bubble Tea integration:

```go
type UpdateAvailableMsg struct {
    CurrentVersion string
    LatestVersion  string
    UpdateCommand  string
}

func CheckAsync(currentVersion string) tea.Cmd
```

- Returns nil if up-to-date or dev version
- Returns `UpdateAvailableMsg` if update available

### 2. Integrate with App

**main.go** changes:

```go
currentVersion := effectiveVersion(Version)
model := app.New(registry, km, currentVersion)
```

**model.go** changes:

- Add `currentVersion string` field
- Add `updateAvailable *version.UpdateAvailableMsg` field
- Modify `New()` to accept version parameter
- Add `version.CheckAsync()` to `Init()` commands

**update.go** changes:

- Handle `version.UpdateAvailableMsg` in switch statement
- Store in `m.updateAvailable` for persistent display

### 3. Display Update Notification (Two-Tier)

**Toast (brief, auto-dismiss)**:

```
Update v0.2.0 available! Press ! for details
```

- Shows for 10-15 seconds on startup
- Brief, non-intrusive

**Diagnostics modal (detailed)**:
Add new "Version" section to `buildDiagnosticsContent()`:

```
Version
  Current: v0.1.0
  Latest:  v0.2.0  ← Update available

  Update command:
  go install -ldflags "-X main.Version=v0.2.0" github.com/sst/sidecar/cmd/sidecar@v0.2.0
```

- Full update instructions in existing modal (press `!`)
- Also show current version even when up-to-date

## Design Decisions

- **Non-blocking**: Async check via `tea.Cmd`, won't slow startup
- **Caching**: 6h TTL respects GitHub API rate limits
- **Skip dev versions**: No checks for `devel`, empty versions
- **Graceful failure**: Network errors silently ignored
- **No config option**: Keep it simple initially (can add later if needed)
