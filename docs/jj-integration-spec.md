# Jujutsu Integration Spec (LazyJJ-Style)

## Status
- Owner epic: `td-6fa08a`
- Architecture task: `td-a85aaa`
- Date: 2026-02-17

## Goals
- Add first-class Jujutsu (`jj`) support in Sidecar.
- Provide a keyboard-first, split-pane workflow similar to lazyjj.
- Preserve Git support for non-jj repositories.
- Avoid regressions in app shell behavior (header/footer, sizing, keymap contexts, project/worktree switching).

## Non-Goals (Phase 1)
- Full lazyjj feature parity (advanced history surgery can follow).
- Replacing all Git internals with a generic VCS abstraction in one pass.
- Changing existing plugin footer rendering architecture.

## Current Constraints
- Plugin registration order and availability are controlled in `cmd/sidecar/main.go`.
- Plugin context carries workdir/project root and epoch in `internal/plugin/context.go`.
- Stale async message invalidation is handled via `plugin.IsStale()` in `internal/plugin/plugin.go`.
- Existing Git key contexts are centralized in `internal/keymap/bindings.go`.
- Plugins must constrain `View(width, height)` output height and must not render custom footers.

## Architecture Decision
- Implement a new plugin: `internal/plugins/jjstatus`.
- Keep `internal/plugins/gitstatus` intact initially.
- Introduce a lightweight capability detector to choose plugin behavior by repository mode.

Rationale:
- Lowest regression risk.
- Incremental rollout with feature flag and per-plugin config.
- Enables side-by-side migration and testing in mixed environments.

## Repository Mode Model
Three repository modes:
1. `git-only`: `.git` repo, no jj workspace metadata.
2. `jj`: `jj` workspace available; Git may exist under the hood.
3. `mixed/unknown`: both tools detected but capability checks conflict or fail.

Resolution policy (default):
1. Prefer `jj` if `jj root` succeeds.
2. Fall back to Git if jj is unavailable.
3. If neither is available, existing no-repo UX remains.

Config overrides:
- Add `plugins.jj-status.enabled`.
- Add `plugins.vcs.preferred` (`"auto"|"jj"|"git"`).

## Plugin Gating Behavior
Default behavior matrix:
1. `git-only`: show `git-status`, hide `jj-status`.
2. `jj`: show `jj-status`; `git-status` hidden by default.
3. `mixed/unknown`: show `jj-status` if `preferred=auto`; allow override to Git.

Notes:
- Prefer gating via plugin init capability checks + config/feature flags.
- Keep registry degradation model (failed init should not crash app).

## New Backend Package
Create `internal/vcs/jj` as a typed command adapter.

### Responsibilities
- Execute jj commands in a target working directory.
- Parse command output into typed models.
- Return actionable errors (command + stderr summary).
- Avoid UI concerns.

### Initial API (Phase 1)
- `Detect(workDir) (root string, ok bool, err error)`
- `Status(workDir) (StatusSnapshot, error)`
- `Log(workDir, opts) ([]Commit, error)`
- `Diff(workDir, opts) (string, error)`
- `Describe(workDir, opts) error`
- `New(workDir, opts) error`
- `BookmarkSet/List(workDir, opts)`
- `GitPush/GitFetch(workDir, opts)`

### Data Contract (initial)
- `StatusSnapshot`: working-copy changes grouped by path + change kind.
- `Commit`: id, short id, description/subject, author, timestamp, refs/bookmarks.
- `DiffTarget`: working copy, revision, path filter.

## JJ Plugin UX (Phase 1)
Contexts (proposed):
- `jj-status`
- `jj-log`
- `jj-diff`
- `jj-describe`
- `jj-error`

View model:
- Split pane: left selection (changes/revisions), right preview (diff/details).
- Optional sidebar collapse parity with git plugin patterns.
- Modal-driven operations for destructive/long-running actions.

Keymap parity targets:
- Navigation: `j/k`, arrows, `g g`, `G`, `tab`, `shift+tab`.
- Actions: `d` (diff), `c` (describe/commit flow), `r` (refresh), `/` (search/log filter), `O` (open in file browser).
- Keep footer labels short (`Diff`, `Describe`, `Refresh`, etc.).

## Inter-Plugin Integration
- Reuse app/file browser messages for “open selected path” flow.
- Keep project/worktree switch compatibility through existing registry `Reinit()` + epoch bump.
- Ensure async jj load messages include epoch and are dropped when stale.

## Rollout Plan
Phase 0: Spec + backend scaffolding
- Land this spec.
- Add `internal/vcs/jj` command runner + parser tests.

Phase 1: Read-only jj UI
- Add `jjstatus` plugin with status/log/diff read flow.
- Add keymap contexts and footer commands.

Phase 2: Mutating operations
- Add describe/new/bookmark actions and confirmation modals.
- Add git-bridge operations (`jj git push/fetch`) where appropriate.

Phase 3: Mode gating and migration
- Add repository mode detector + config overrides.
- Auto-hide/disable incompatible plugin by mode.

Phase 4: Hardening
- Add integration tests for git-only/jj/mixed repos.
- Docs and migration guidance in README/docs.

## Risks and Mitigations
- Risk: command-output parser drift across jj versions.
  - Mitigation: favor structured templates/JSON-compatible output when available; test fixtures.
- Risk: user confusion in mixed repos.
  - Mitigation: explicit mode indicator and setting to force preferred VCS.
- Risk: keybinding conflicts.
  - Mitigation: isolate jj contexts and keep command IDs distinct.

## Test Strategy
- Unit: parser fixtures + command runner error handling.
- Plugin unit: state transitions, stale message rejection, context changes.
- Integration: repo mode matrix (`git-only`, `jj`, `mixed`), project/worktree switch behavior.
- Render checks: ensure plugin views remain height-constrained and footer-free.

## Implementation Touchpoints
- Registration and startup: `cmd/sidecar/main.go`
- Config schema/defaults: `internal/config/config.go`, loader/saver tests
- Feature flags (optional rollout guard): `internal/features/features.go`
- Keybindings: `internal/keymap/bindings.go`
- New backend: `internal/vcs/jj/*`
- New plugin: `internal/plugins/jjstatus/*`
