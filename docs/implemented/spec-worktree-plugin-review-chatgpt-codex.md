# Worktree Plugin Spec Review (Codex)

Review of `docs/spec-worktree-plugin.md` against sidecar and td code patterns.

## Findings

### High

1. Footer and height constraints are missing from the spec.
   - The spec mockups show a plugin footer line, but sidecar renders a unified footer and plugins must not render their own.
   - Plugins must always constrain output height to avoid the header scrolling off-screen.
   - Add an explicit note to follow sidecar's `View(width,height)` rules and avoid plugin footers.
   - References: docs/spec-worktree-plugin.md:236, docs/guides/ui-feature-guide.md, internal/plugins/tdmonitor/plugin.go:191, internal/plugins/tdmonitor/plugin.go:210.

2. Keymap conflicts with sidecar conventions.
   - Spec uses `Tab`/`Shift+Tab` for plugin switching and `Tab` for preview tabs; sidecar uses `` ` `` and `~` for plugin switching and `Tab`/`\` for pane focus.
   - Spec uses `r` for resume and `R` for refresh; sidecar treats `r` as global refresh in some contexts unless opted out.
   - Update spec keymap to align with sidecar's keyboard shortcut guide and update root/non-root contexts.
   - References: docs/spec-worktree-plugin.md:329, docs/spec-worktree-plugin.md:360, internal/keymap/bindings.go:9, internal/app/update.go:492, docs/guides/ui-feature-guide.md.

3. TD worktree sharing is incomplete and DB filename is wrong.
   - Spec uses `.todos/db.sqlite`, but td uses `.todos/issues.db`.
   - td base directory is currently the working directory (`os.Getwd()`), which also drives config/session paths; only changing DB path is insufficient.
   - Add a baseDir resolver in td (e.g., check `.td-root` or an env/flag) and use that baseDir for all td file locations.
   - References: docs/spec-worktree-plugin.md:1149, docs/spec-worktree-plugin.md:1169, /Users/marcusvorwaller/code/td/cmd/root.go:284, /Users/marcusvorwaller/code/td/internal/db/db.go:17.

4. Background goroutines mutate model state directly.
   - `pollAgentOutput` and `Restore` update worktree/agent state outside `Update`, which breaks Bubble Tea's state model and risks data races.
   - Convert polling to `tea.Cmd` that returns typed messages; update state only in `Update`.
   - References: docs/spec-worktree-plugin.md:743, docs/spec-worktree-plugin.md:1361, docs/guides/sidecar-plugin-guide.md:17.

### Medium

1. Config integration is not aligned with sidecar's config system.
   - Spec introduces `plugins.worktree` and `.sidecar/config.json`, but sidecar config structs/loader do not include this plugin.
   - Decide whether to extend `internal/config/config.go`/`internal/config/loader.go` or remove `.sidecar/config.json` from the spec.
   - References: docs/spec-worktree-plugin.md:1396, internal/config/config.go:19, internal/config/loader.go:19.

2. Keymap/contexts wiring and root-context handling are under-specified.
   - Spec lists shortcuts but does not define `Commands()`, `FocusContext()`, `bindings.go` entries, or `isRootContext`/`isTextInputContext` updates for modals and search.
   - Add a section that mirrors the sidecar keymap workflow and contexts.
   - References: docs/guides/sidecar-plugin-guide.md:35, docs/guides/ui-feature-guide.md, internal/app/update.go:492.

3. Metadata paths conflict with sidecar conventions.
   - Spec writes `.sidecar` metadata inside worktrees and `~/.sidecar/agent-events.jsonl`, but sidecar uses `~/.config/sidecar` (via `ctx.ConfigDir` and `internal/state`).
   - Decide whether worktree metadata should live under `ctx.ConfigDir` or inside worktrees, and document `.gitignore` expectations.
   - References: docs/spec-worktree-plugin.md:1317, docs/spec-worktree-plugin.md:1129, internal/state/state.go:41.

4. td CLI integration details are off.
   - `td query` needs `-o json` (no `--json` flag).
   - `td show` supports `--format json` or `--json`; shell substitution into tmux is brittle.
   - Prefer using `td show --format json` and parse in Go, then send literal prompt text.
   - References: docs/spec-worktree-plugin.md:1285, docs/spec-worktree-plugin.md:1258, /Users/marcusvorwaller/code/td/cmd/query.go:313, /Users/marcusvorwaller/code/td/cmd/show.go:136.

5. Git stats assume `main`.
   - `rev-list main...HEAD` should use `wt.BaseBranch` or upstream to avoid incorrect counts for non-main bases.
   - References: docs/spec-worktree-plugin.md:609.

6. TD session identity for agents is missing.
   - td supports explicit session IDs via `TD_SESSION_ID`, which is more reliable than process detection when launching agents via tmux.
   - Add guidance to set `TD_SESSION_ID` when launching agents so logs/handoffs are correctly scoped.
   - References: /Users/marcusvorwaller/code/td/internal/session/agent_fingerprint.go:74.

### Low

1. `DeriveStatus` uses `a.waitingFor` instead of `WaitingFor` (does not compile as shown).
   - Reference: docs/spec-worktree-plugin.md:1082.

2. `ring.Buffer` with `Update` is not a standard type.
   - Either define a buffer type or use a simple bounded slice with locking.
   - Reference: docs/spec-worktree-plugin.md:736.

3. `tmux send-keys` with `"claude --prompt \"$(td show ...)\""` depends on shell expansion.
   - Compute prompt text in Go and send via `tmux send-keys -l` to avoid quoting issues.
   - Reference: docs/spec-worktree-plugin.md:1258.

4. Branch deletion uses `git branch -d` without setting `cmd.Dir`.
   - Should run in repo root to avoid failures in non-repo working dirs.
   - Reference: docs/spec-worktree-plugin.md:525.

## Suggested Spec Additions

1. Plugin architecture alignment:
   - Add a "Sidecar Integration" section covering `Commands()`, `FocusContext()`, and `internal/keymap/bindings.go`.
   - Include root/non-root context updates (`internal/app/update.go`) and any text input contexts for modals.

2. Rendering contract:
   - Explicitly require `View(width,height)` to store dimensions and constrain output height.
   - Explicitly forbid plugin-specific footers; use `Commands()` for hints.

3. TD baseDir resolution:
   - Document how `.td-root` is resolved and that it must redirect all `.todos` paths (db, config, sessions, analytics).
   - Update DB filename to `.todos/issues.db`.

4. Agent session identity:
   - Specify `TD_SESSION_ID` injection in tmux session environment (e.g., `sidecar-wt-<name>`).

5. Diff rendering:
   - Call out reuse of existing diff rendering/paging patterns (gitstatus renderer and td-331dbf19 for paging).

## Open Questions

1. Should the worktree plugin adopt unified sidebar controls (Tab to switch panes, `\` to collapse) or add a new preview-tab system?
2. Do you want `.td-root` discovery (walk up until found) or a new td flag/env (e.g., `TD_BASE_DIR`) for worktrees?
3. Where should worktree metadata live by default: per-worktree files or `ctx.ConfigDir`?
4. Should agent output/diff previews reuse the gitstatus diff parser for consistency, or is a simplified renderer acceptable?
