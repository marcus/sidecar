# Sidecar Keyboard Shortcuts Guide

Comprehensive guidance for plugin and core implementers on defining, wiring, and presenting keyboard shortcuts.

## Core components (where things live)
- Keymap registry: `internal/keymap/registry.go`
- Default bindings: `internal/keymap/bindings.go`
- Plugin command contract: `internal/plugin/plugin.go` (`Commands()`, `FocusContext()`)
- Footer/help rendering: `internal/app/view.go`

## Concepts
- **Command**: A logical action with an ID, human name, and handler. Registered in `keymap.Registry` or exposed by plugins via `Commands()`.
- **Binding**: A key or key sequence mapped to a command within a *context*.
- **Context**: A string namespace (e.g., `global`, `git-status`, `git-diff`, `td-monitor`). Key lookup first checks active context, then global.
- **Sequence**: Multi-key binding (e.g., `g g`) with a 500ms timeout (`sequenceTimeout`).
- **Active context**: Returned by `Plugin.FocusContext()`; must reflect current view mode so lookup and footer hints stay correct.

## Adding or updating shortcuts
1. **Define a command** (if global/core): register in `keymap.Registry` with a stable `ID` and user-facing `Name`. Keep handlers cheap.
2. **Expose plugin commands**: return `[]plugin.Command` from `Plugin.Commands()`, one per action per context.
3. **Bind keys**: add `keymap.Binding` entries in `internal/keymap/bindings.go` for each command/context pair. Prefer mnemonic, conflict-free keys.
4. **Set the focus context**: ensure `FocusContext()` returns the correct context for the current view state; update it when view modes change.
5. **Test footer/help**: with the plugin focused, footer hints and `?` help should show your bindings. If not, check that command IDs + contexts match bindings.

## Context and focus rules
- Global bindings always apply; plugin bindings only apply when that context is active.
- **Numeric keys (1-9) are context-aware**: They switch plugins only in "global" context. In plugin-specific contexts (e.g., `td-monitor`), they are forwarded to the plugin. This allows plugins like td-monitor to use 1, 2, 3 for internal navigation (pane switching) without conflict.
- Switching plugins calls `SetFocused` on the old/new plugin; update any context-dependent state there if needed.
- When inside subviews (e.g., diff modal), return a sub-context (`git-diff`) so only relevant bindings display.

## Sequences
- Register multi-key bindings using space separation (e.g., `g g`).
- Sequences start when the first key matches a binding prefix; if the second key is not pressed within 500ms, the pending state resets.
- Avoid ambiguous prefixes unless intentional (keep the set small to reduce UX confusion).

## Naming conventions
- Command IDs: kebab-case verbs (`open-file`, `toggle-help`, `approve-issue`).
- Contexts: kebab-case matching plugin IDs or view modes (`git-status`, `conversation-detail`).
- Keys: lowercase for letters (`j`, `k`, `g g`), `ctrl+<key>` for control, `shift+tab`, `enter`, `esc`, `up/down/left/right`.

## User overrides
- Users can override bindings via config (`cfg.Keymap.Overrides`). Registry checks overrides before context/global bindings.
- Keep command IDs stable; changing them breaks overrides. If renaming is necessary, add a backwards-compat alias period when feasible.

## Collision avoidance checklist
- Search `bindings.go` for your chosen key/context before adding.
- Avoid stealing widely expected globals (`q`, `ctrl+c`, `?`, tab navigation) unless justified.
- Prefer context-specific bindings over global when the action is view-local.

## Accessibility and ergonomics
- Provide alternatives for critical actions (e.g., arrow keys in addition to `j/k`).
- Keep high-frequency actions on home-row keys; reserve shifted/ctrl combos for less common tasks.
- Ensure commands remain reachable on 60% keyboards (avoid F-keys unless optional).

## Surfacing shortcuts to users
- Footer hints: supplied by `Commands()` + matching bindings in the active context.
- Help overlay (`?`): built from registry bindings; shows global plus active-context bindings.
- Toasts/status: avoid relying solely on hints; confirm important shortcuts in onboarding copy where relevant.

## Testing shortcuts quickly
- Run Sidecar with `--debug` to see key handling logs.
- Verify: key triggers command → state changes → footer/help reflect correct bindings when switching contexts/plugins.
- For sequences, check timeout behavior and that partial sequences don’t block single-key actions.

## Plugin checklist (per context/view)
- Command list covers all user-facing actions.
- Context name returned by `FocusContext()` matches bindings.
- Default bindings added in `bindings.go`.
- No blocking work in command handlers; they should return `tea.Cmd` for async tasks.
- `SetFocused` flips any flags needed for focus-specific behavior (optional).

## Migration tips
- When altering bindings, keep old keys temporarily as secondary bindings if possible to avoid user breakage.
- Document breaking changes in release notes; keep command IDs stable to preserve overrides.
