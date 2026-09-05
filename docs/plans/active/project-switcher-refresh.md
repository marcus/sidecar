# The Project Switcher Refresh — a compact list, a grid, and dates that mean something

**Status:** Implemented. The `@` switcher is a compact list with column headings and full-row click targets, a three-column card grid, and a shared sort control offering Last activity / Name / Date added with a direction. Sort, direction and view persist. **Tracking:** `td-69e08f`. **Design source:** `~/code/tui/mockups/project-switcher.tui.yaml` states 02 and 03, and `project-switcher-notes.md`.

One sentence: **the switcher should let you find a project by recency, by name, or by when you added it, and let you click any row or card anywhere on it.**

## What the design chose

Marcus picked states 02 (grid) and 03 (compact list) from the studio explorations, with the compact list as the everyday default. His note on state 03 was that "the compact view is slightly cramped but still nice to have more rows on the screen than fewer, esp since they can be clicked." So the density stayed and the breathing room came from horizontal padding, aligned columns and a full-width selection band — not from spending a second row on every project.

## The two date questions, and the honest answers

Nothing in Sidecar recorded either date before this work. `ProjectConfig` had a name, a path, a theme, an open-in preference and a worktree policy; `state.json` had `lastBoundLocation`, a single most-recent destination and nothing per project. Both columns therefore needed a fact to be recorded before they could be shown, and the choice of which fact is the whole design.

**"Date created" is not a fact Sidecar has, so the control says "Date added".** `ProjectConfig.AddedAt` is written once, by `config.AddProject` — the single Load→mutate→Save boundary that both the TUI's add-project flow and `sidecar project add` already go through, so the CLI records it without knowing about it and `sidecar project list --json` reports it. It means *registration with Sidecar*. It is not the directory's birth time, which changes on a clone or a restore, and not the first commit, which is repository history rather than local registration. A project registered before this shipped has no value, reads `Unknown`, and is never backfilled — not from the upgrade time, not from anything else.

**"Last activity" is the latest Sidecar event recorded against the project.** `state.ProjectActivity` maps a cleaned project path to a timestamp, written when Sidecar binds a project: at launch, and on every switch. Because `sidecar project switch` drives the running TUI through a UI request rather than switching by itself, the CLI path records the same event through the same code. Reading it costs nothing at modal-open time — it is already-loaded state, with no filesystem walk, no git, and no tmux — and writing it happens in a `tea.Cmd`, never on the path to a frame. A project Sidecar has not opened reads `Unknown`. The set of recorded events can widen later (managed shell creation is the obvious next one) without the word changing meaning.

A remote destination's dates belong to the host that owns it. Until a host's inventory carries them, remotes are `Unknown` in both columns and sort with everything else that is unknown.

Three ordering rules hold in every mode: Overview is pinned ahead of the collection and is outside the sorting domain; an unknown date sorts last in *both* directions, because "not recorded" is not a date and reversing the order must not promote it; and ties break on the case-insensitive name and then the stable identity, so the same collection always produces the same order.

## Where the code lives

- **`internal/projectlist`** is the state-free core: `Sort`, `Order` and `View` with their labels and action IDs, the `Item` presentation model, `Filter`, `Sorted`, the relative-time and date formatting, and the grid's column and movement geometry. It has no Bubble Tea state and touches no filesystem, so a headless caller gets the same filtering and ordering the modal uses. Its vocabulary deliberately mirrors `internal/workspacelist`, which is the same shape for the Workspaces collection.
- **`internal/modal.ControlRow`** is the caption-plus-pills row: measuring, right-aligning and registering a hit region that is exactly the drawn pill. It is in the modal library rather than in the switcher because that geometry is the half that goes subtly wrong when it is written twice, and the worktree and theme switchers are the obvious next consumers. The sort menu is a `modal.Overlay` hung off the control it belongs to — the existing overlay mechanism, not a second modal.
- **`internal/app/project_switcher_view.go`** draws the list and the grid. **`project_switcher_state.go`** holds focus, ordering, cursor movement and persistence. **`project_activity.go`** records activity. The destination model, activation and refusal rules in `model.go` were left alone: they were already correct.

## Interaction rules worth keeping

Typing always edits the filter. No control claims a bare printable key — `g`, `s` and `n` are filter text, not shortcuts — and Tab is the only way out of the filter, walking filter → sort → view → add. Escape dismisses an open sort menu before it clears the filter, and clears a non-empty filter before it closes the switcher. Selection rides the destination's stable identity through every filter, sort, direction and view change. The current project and the highlighted row stay separate: the bound project wears a `current` badge whether or not the cursor is on it. Grid arrows are spatial and refuse rather than wrap. The whole row and the whole card are the hit target.

Below the width where a card is readable the grid draws the list instead, and the remembered view is not rewritten by that fallback — only the drawing changes. The list drops its metadata column before its path column, and its path column before its name.

## Proof

`internal/projectlist` and `internal/modal` cover the rules directly. `internal/app/project_switcher_test.go` covers the integration: ordering and its unknown-last rule, selection surviving a reorder, persistence round-tripping through `state.json`, full-row and whole-card hit regions, spatial grid arrows, the narrow-terminal fallback, printable keys reaching the filter, the sort menu, the no-results state, and a refused remote destination keeping its reason and not switching.

The real app was driven headless through `scripts/tmux-drive.sh` at 160×45 on `sidecar-modern`, with both the tmux socket and the Sidecar state tree isolated. A click at column 120 — far to the right of a row's text — switched projects and wrote the activity record, which is the full-row target and the activity pipeline proved end to end in one gesture.

## Not done here, deliberately

The remaining reusable components sketched in state 08 of the mockups — a shared `SearchField`, a `DestinationRow`/`DestinationCard` pair, a `CollectionEmptyState` — are not built. A reusable collection is worth having only where a second modal actually needs it, and today the switcher is the only caller. `ControlRow` was promoted because its geometry is genuinely shared work; the rest would be API written on spec.
