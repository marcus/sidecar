# Bubbles v1 → v2 Upgrade Plan

> Status: **PLAN / not started** · Part of the Phase 1 atomic trio (see [overview](charm-upgrade-00-overview.md)).
> Do this **last** within Phase 1, after [lipgloss](charm-upgrade-02-lipgloss.md) and [bubbletea](charm-upgrade-03-bubbletea.md).

## Versions

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/bubbles` | **`charm.land/bubbles/v2`** |
| Version | `v0.21.1-0.20250623103423-23b8fd6302d7` | **`v2.1.0`** (re-verify) |

`go.mod`: `charm.land/bubbles/v2 v2.1.0`. **`bubbles/v2`'s go.mod hard-requires `bubbletea/v2 v2.0.7` + `lipgloss/v2 v2.0.3`** — this is why all three land together. Do NOT use `github.com/charmbracelet/bubbles/v2` (beta-frozen at `v2.0.0-beta.1`).

## Scope — only three subpackages are used

Confirmed by grep. Sidecar uses **only**:
- `bubbles/textinput` — 17 files
- `bubbles/textarea` — 4 files
- `bubbles/key` — 2 files (**no API change** — path only)

Not used (no migration needed): viewport, spinner, list, table, paginator, help, progress, cursor, filepicker, stopwatch, timer.

## Step 1 — Import paths

```bash
grep -rl '"github.com/charmbracelet/bubbles/' --include='*.go' . \
  | xargs sed -i '' 's#github.com/charmbracelet/bubbles#charm.land/bubbles/v2#g'
```

## Step 2 — `bubbles/key` (2 files) — path only, zero logic change

[internal/plugins/conversations/content_search_input.go](../../../internal/plugins/conversations/content_search_input.go) and [internal/plugins/notes/plugin.go:9,266](../../../internal/plugins/notes/plugin.go).

`key.Binding`, `key.NewBinding`, `key.Matches`, `key.WithKeys`, `key.WithHelp`, `key.WithDisabled` are **unchanged** in v2. `key.Matches(msg, binding)` keeps working because `tea.KeyMsg` is now an interface satisfied by `tea.KeyPressMsg` (which is what you switch on after the [bubbletea](charm-upgrade-03-bubbletea.md) Step 4 rename). No edits beyond the import path.

## Step 3 — `bubbles/textinput` (17 files): `.Width` field → `SetWidth()`

The only breaking change that touches sidecar's textinput usage is **`Width` became a method pair** (`SetWidth(int)` / `Width() int`). Sidecar reads/writes `.Width` as a field at these sites — change each assignment `ti.Width = N` → `ti.SetWidth(N)`:

| File:line |
|-----------|
| [internal/app/worktree_switcher_modal.go:36](../../../internal/app/worktree_switcher_modal.go) `ti.Width = 40` |
| [internal/app/model.go:640](../../../internal/app/model.go) `ti.Width = 40` |
| [internal/app/model.go:912,919](../../../internal/app/model.go) `nameInput.Width = 36`, `pathInput.Width = 36` |
| [internal/app/model.go:941,1108,1164](../../../internal/app/model.go) `ti.Width = 36/50/54` |
| [internal/plugins/workspace/view_modals.go:48](../../../internal/plugins/workspace/view_modals.go) `p.taskSearchInput.Width = inputW` |
| [internal/plugins/workspace/keys.go:726,1086](../../../internal/plugins/workspace/keys.go) `...Input.Width = 30` |
| [internal/plugins/workspace/prompt_picker.go:41](../../../internal/plugins/workspace/prompt_picker.go) `ti.Width = 30` |
| [internal/plugins/workspace/create_modal.go:346](../../../internal/plugins/workspace/create_modal.go) `p.taskSearchInput.Width = inputInnerWidth` |
| [internal/plugins/notes/task_modal.go:111](../../../internal/plugins/notes/task_modal.go) `p.taskModalTitleInput.Width = 40` |
| [internal/modal/input.go:92](../../../internal/modal/input.go) `s.model.Width = inputInnerWidth` |
| [internal/palette/palette.go:52,72](../../../internal/palette/palette.go) `ti.Width = 40`, `m.textInput.Width = min(...)` |

Any place that *reads* `someInput.Width` must become `someInput.Width()` — grep `grep -rn '\.Width\b' --include='*.go' internal | grep -iE 'input|ti\.|palette'` and audit reads vs writes.

Fields that did **not** change and need no edits (confirmed sidecar usage): `.Placeholder`, `.CharLimit`, `.Focus()`, `.Blur()`, `.Value()`, `.SetValue()`, `.View()`. Sidecar does **not** set `PromptStyle/TextStyle/PlaceholderStyle` on textinput (those moved to `Styles.Focused/Blurred` in v2, but sidecar doesn't touch them), and does **not** use `textinput.DefaultKeyMap` (which became a function). So the textinput styling/keymap breaks don't apply here.

> One exception in gitstatus: [internal/plugins/gitstatus/plugin.go:1423](../../../internal/plugins/gitstatus/plugin.go) sets `p.commitMessage.FocusedStyle.Placeholder = ...` — but `commitMessage` is a **textarea**, not a textinput. Handle under Step 4.

## Step 4 — `bubbles/textarea` (4 files)

Breaking changes that hit sidecar:

### 4a. `textarea.Style` type renamed → `textarea.StyleState`; styling moved under `.Styles`

[internal/plugins/notes/plugin.go:254-264](../../../internal/plugins/notes/plugin.go):
```go
// BEFORE
ta.FocusedStyle = textarea.Style{ ... }
ta.BlurredStyle = ta.FocusedStyle

// AFTER
ta.Styles.Focused = textarea.StyleState{ ... }
ta.Styles.Blurred = ta.Styles.Focused
```
(The per-state struct literal type `textarea.Style` → `textarea.StyleState`; the model fields `FocusedStyle`/`BlurredStyle` → `Styles.Focused`/`Styles.Blurred`.)

[internal/plugins/gitstatus/plugin.go:1423](../../../internal/plugins/gitstatus/plugin.go):
```go
// BEFORE
p.commitMessage.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(styles.TextSecondary)
// AFTER
p.commitMessage.Styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(styles.TextSecondary)
```

> Verify the field name on `StyleState` for the placeholder (likely `.Placeholder`) against the v2 textarea source. Also note `lipgloss.NewStyle()...` returns the v2 style — already migrated in the lipgloss step.

### 4b. `SetCursor(col)` → `SetCursorColumn(col)`

[internal/plugins/notes/plugin.go:961](../../../internal/plugins/notes/plugin.go): `p.editorTextarea.SetCursor(col)` → `p.editorTextarea.SetCursorColumn(col)`.

### 4c. `KeyMap.CapitalizeWordForward` — verify it still exists

[internal/plugins/notes/plugin.go:266](../../../internal/plugins/notes/plugin.go): `ta.KeyMap.CapitalizeWordForward = key.NewBinding(key.WithDisabled())`. The `KeyMap` field still exists in v2; confirm the `CapitalizeWordForward` binding name is unchanged (it should be). If renamed, find the equivalent in `textarea.DefaultKeyMap()`.

### 4d. Fields that are unchanged (no edits)

Confirmed sidecar usage that stays the same: `textarea.New()`, `.SetValue()`, `.Value()`, `.Focus()`, `.Blur()`, `.SetWidth()`, `.SetHeight()` (already methods in v1), `.ShowLineNumbers`, `.CharLimit`, `.MaxHeight`, `.Prompt`, `.EndOfBufferCharacter`.

> textarea v2 also added `Cursor()` returning `*tea.Cursor`, dynamic vertical auto-resize (v2.1.0), and `Column()/ScrollYOffset()`. Sidecar doesn't need these now, but auto-resize could simplify the notes editor later (out of scope).

## Step 5 — `DefaultKeyMap` as function (verify not used)

In v2, `textinput.DefaultKeyMap` / `textarea.DefaultKeyMap` became **functions** (`DefaultKeyMap()`). Sidecar does not reference either (confirmed) — but re-grep `grep -rn 'DefaultKeyMap' --include='*.go' .` after import rewrite to be safe; if any survive, add `()`.

## Ordered checklist

1. [ ] `go get charm.land/bubbles/v2@v2.1.0` (re-verify; pulls bubbletea/lipgloss v2)
2. [ ] Import path rewrite for textinput/textarea/key (Step 1)
3. [ ] `key` — confirm zero logic edits (Step 2)
4. [ ] textinput `.Width =` → `.SetWidth()`, reads → `.Width()` (Step 3)
5. [ ] textarea `Style`→`StyleState`, `FocusedStyle`→`Styles.Focused`, gitstatus commit field (Step 4a)
6. [ ] textarea `SetCursor`→`SetCursorColumn` (Step 4b)
7. [ ] Verify `CapitalizeWordForward` + no stray `DefaultKeyMap` (Steps 4c, 5)
8. [ ] `go build ./...` — **should now go green** (all three v2 libs in place)
9. [ ] `go test ./...` — fix remaining test constructor breakage (see below)

## Test files to fix

These construct bubbletea key/mouse messages and will fail to compile under v2 (see [bubbletea plan](charm-upgrade-03-bubbletea.md) Steps 4 & 7) — but they live alongside bubbles-using code:
- [internal/plugins/filebrowser/fileops_test.go](../../../internal/plugins/filebrowser/fileops_test.go), [internal/modal/modal_test.go](../../../internal/modal/modal_test.go) (textinput in tests)
- key-msg literal rewrites: `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}` → `tea.KeyPressMsg{Code: 'a', Text: "a"}` across `internal/plugins/workspace/interactive_test.go`, `internal/tty/keymap_test.go`, `internal/palette/palette_test.go`, `internal/keymap/registry_test.go`, etc.

## Gotchas

- **`textarea.Style` vs `textarea.StyleState`** — the struct literal type rename is easy to miss; the compiler will flag `undefined: textarea.Style`.
- **textinput `.Width` read sites** — assignments are obvious, but a read like `if ti.Width > 0` silently means something different until you add `()`. The compiler catches it (`ti.Width` is now a method value), so rely on the build.
- **Don't migrate bubbles before bubbletea/lipgloss** — `charm.land/bubbles/v2` will pull the v2 trio and nothing compiles until their call-sites are migrated too. This is why it's the last step of the atomic Phase 1.
