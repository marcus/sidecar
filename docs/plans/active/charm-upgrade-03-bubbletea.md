# Bubble Tea v1 → v2 Upgrade Plan

> Status: **PLAN / not started** · Part of the Phase 1 atomic trio (see [overview](charm-upgrade-00-overview.md)).
> Do this **after** [lipgloss v2](charm-upgrade-02-lipgloss.md), **before** [bubbles v2](charm-upgrade-04-bubbles.md).

## Versions

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/bubbletea` | **`charm.land/bubbletea/v2`** |
| Version | `v1.3.10` | **`v2.0.7`** (re-verify) |

`go.mod`: `charm.land/bubbletea/v2 v2.0.7`. Requires Go 1.25 (satisfied). **Do NOT use `github.com/charmbracelet/bubbletea/v2`** (beta-frozen).

## The four big architectural changes

1. **`View() string` → `View() tea.View`.** Only the model passed to `tea.NewProgram` must change — in sidecar that is the app `Model` only (see "Plugin View is unaffected" below).
2. **Terminal features are declarative.** Alt-screen, mouse, focus-report, bracketed paste, window title, cursor are now **fields on `tea.View`**, not `NewProgram` options or commands. Mouse is **off by default**.
3. **`tea.KeyMsg` is now an interface.** Match `tea.KeyPressMsg`. Field renames: `msg.Type`→`msg.Code`, `msg.Runes`→`msg.Text` (string), `msg.Alt`→`msg.Mod.Contains(tea.ModAlt)`. Paste left `KeyMsg` entirely → `tea.PasteMsg`.
4. **`tea.MouseMsg` is now an interface**, split into `MouseClickMsg`/`MouseReleaseMsg`/`MouseWheelMsg`/`MouseMotionMsg`. Coords via `msg.Mouse()`. Button constants drop the `Button` infix (`MouseButtonLeft`→`MouseLeft`).

What did **NOT** change: `Init() tea.Cmd`, `Update(tea.Msg) (tea.Model, tea.Cmd)`, `tea.Batch`, `tea.Sequence`, `tea.Tick`, `tea.Quit`, `tea.ExecProcess` (signature), `tea.WindowSizeMsg`, `tea.QuitMsg`.

### Plugin `View(width, height int) string` is unaffected

Sidecar's plugins implement a **custom** interface ([internal/plugin/plugin.go:13-14](../../../internal/plugin/plugin.go)):
```go
Update(msg tea.Msg) (Plugin, tea.Cmd)
View(width, height int) string
```
This is sidecar's own contract, not `tea.Model`. Only the top-level app `Model` is handed to `tea.NewProgram`. **So only one `View()` changes return type** (the app's). The embedded sub-models `palette` and `tty` also keep their `string`-returning `View()` — the app calls them and composes strings, then wraps the final string in `tea.NewView(...)`. Their `Update` key/mouse handling DOES change (interface rename), but their `View` does not.

## The work, grounded in the codebase

### Step 1 — Import path (109 files)

```bash
grep -rl '"github.com/charmbracelet/bubbletea"' --include='*.go' . \
  | xargs sed -i '' 's#github.com/charmbracelet/bubbletea#charm.land/bubbletea/v2#g'
```
Keep the `tea` alias (most imports are `tea "github.com/charmbracelet/bubbletea"` or bare). If bare, the package name is still `tea`.

### Step 2 — Program construction → move options into the View

[cmd/sidecar/main.go:218](../../../cmd/sidecar/main.go):
```go
// BEFORE
p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseAllMotion())

// AFTER
p := tea.NewProgram(model)   // options move to View fields (Step 3)
```
`p.Run()` at line 220 is unchanged.

### Step 3 — App root `View() string` → `View() tea.View` + set terminal features

[internal/app/view.go:33](../../../internal/app/view.go) currently `func (m Model) View() string`. Change to:
```go
func (m Model) View() tea.View {
    // ... build `content string` exactly as today ...
    v := tea.NewView(content)
    v.AltScreen = true
    v.MouseMode = tea.MouseModeAllMotion   // replaces WithMouseAllMotion()
    return v
}
```
> `MouseModeAllMotion` mirrors the old `WithMouseAllMotion()`. If motion events prove noisy, `MouseModeCellMotion` is the lighter option, but keep parity with v1 first (the app relies on drag + hover; see `internal/mouse/`).

If the app currently overlays its own cursor escape sequences, prefer `v.Cursor = tea.NewCursor(x, y)` instead (see Step 7). The app itself does not appear to position a cursor (the tty plugin does, see Step 7).

### Step 4 — Key messages: `tea.KeyMsg` → `tea.KeyPressMsg` (54 files reference `tea.KeyMsg`)

For every `case tea.KeyMsg:` in an `Update`/handler switch, rename to `case tea.KeyPressMsg:`. Add a `tea.KeyReleaseMsg` arm only where you actually need releases (sidecar does not today). Heaviest non-test sites:

- [internal/app/update.go:48](../../../internal/app/update.go) — main key switch
- [internal/plugins/workspace/keys.go](../../../internal/plugins/workspace/keys.go) — 24 refs
- [internal/plugins/notes/plugin.go](../../../internal/plugins/notes/plugin.go) — 6 refs
- [internal/palette/palette.go:160](../../../internal/palette/palette.go)
- [internal/plugins/conversations/plugin_input.go](../../../internal/plugins/conversations/plugin_input.go) — 6 refs
- [internal/plugins/gitstatus/update_handlers.go](../../../internal/plugins/gitstatus/update_handlers.go), [internal/plugins/filebrowser/handlers.go](../../../internal/plugins/filebrowser/handlers.go), [internal/plugins/filebrowser/plugin.go](../../../internal/plugins/filebrowser/plugin.go)
- [internal/tty/tty.go:176](../../../internal/tty/tty.go), [internal/app/project_add_modal.go](../../../internal/app/project_add_modal.go)
- helper funcs that take a key msg by value: e.g. `isMouseEscapeSequence(msg tea.KeyMsg)` ([app/update.go:28](../../../internal/app/update.go)), `MapKeyToTmux(msg tea.KeyMsg)` ([tty/keymap.go:13](../../../internal/tty/keymap.go)), `IsPasteInput(msg tea.KeyMsg)` ([tty/paste.go](../../../internal/tty/paste.go)). These param types stay `tea.KeyMsg` (the interface) — they accept the interface; callers pass the `KeyPressMsg`. But their *bodies* use `.Type`/`.Runes` and must change (Step 5).

> `msg.String()` still works on key messages. Code that matches `switch msg.String() { case "enter": ... }` (e.g. workspace/keys.go, tty.go:256,265,334) keeps working — **except** `case " "` which is now `case "space"` (grep for it; see Step 8).

### Step 5 — Key field renames + `tea.KeyRunes` removal (concentrated in `internal/tty/`)

The tty package does low-level key→tmux translation and is the hardest file. Map:

| v1 | v2 |
|----|----|
| `msg.Type` (a `KeyType`) | `msg.Code` (a `rune`) |
| `msg.Runes` (`[]rune`) | `msg.Text` (`string`) |
| `msg.Type == tea.KeyRunes` | `len(msg.Text) > 0` (i.e. it's text input) |
| `tea.KeyEnter/KeyEsc/KeyTab/KeySpace/KeyBackspace/KeyDelete/KeyUp/...` | still exist as `Code` rune constants — match `msg.Code == tea.KeyEnter` |
| `tea.KeyCtrlC` and other `KeyCtrl*` | **removed** — use `msg.String() == "ctrl+c"` or `msg.Code=='c' && msg.Mod.Contains(tea.ModCtrl)` |
| `msg.Alt` | `msg.Mod.Contains(tea.ModAlt)` |

Exact sites:
- [internal/tty/keymap.go:156-159](../../../internal/tty/keymap.go): `case tea.KeyRunes:` then `string(msg.Runes)` → restructure: the `KeyRunes` case becomes the default/text case checking `len(msg.Text) > 0`, returning `msg.Text`.
- [internal/tty/keymap.go](../../../internal/tty/keymap.go) also switches on `tea.KeyEnter, tea.KeyEscape, tea.KeyBackspace, tea.KeyDelete, tea.KeyTab/KeyCtrlI, tea.KeySpace, tea.KeyCtrlC, tea.KeyUp` etc. Convert the switch from `switch msg.Type` to `switch msg.Code`, and replace the `KeyCtrl*` arms with `msg.String()` checks (the Ctrl constants are gone). `tea.KeyCtrlI`/Tab dual-mapping: in v2 Tab is `tea.KeyTab`; ctrl+i collides with Tab — match via `String()`.
- [internal/tty/tty.go:300](../../../internal/tty/tty.go): `if msg.Type == tea.KeyRunes && len(msg.Runes) > 0` → `if len(msg.Text) > 0`. Line 301 `LooksLikeMouseFragment(string(msg.Runes))` → `(msg.Text)`.
- [internal/tty/tty.go:315](../../../internal/tty/tty.go): `msg.Type == tea.KeyRunes && string(msg.Runes) == "["` → `msg.Text == "["`.
- [internal/tty/tty.go:346](../../../internal/tty/tty.go): `text := string(msg.Runes)` → `text := msg.Text`.
- [internal/app/update.go:1200](../../../internal/app/update.go): `idx := int(msg.Runes[0] - '1')` → `idx := int(msg.Text[0] - '1')` (note: `msg.Text[0]` is a byte; for ASCII digits this is fine, but for safety use `[]rune(msg.Text)[0]`).

### Step 6 — Paste: `KeyMsg.Paste` → `tea.PasteMsg` (architectural)

v1 delivered pasted text as a `KeyMsg` with `msg.Paste == true`. v2 removed the field and sends a separate `tea.PasteMsg` (with `.Content string`), plus optional `tea.PasteStartMsg`/`tea.PasteEndMsg`.

Sites:
- [internal/tty/paste.go:14-25](../../../internal/tty/paste.go) — `IsPasteInput` checks `msg.Type != tea.KeyRunes` (line 14) and `msg.Paste` (line 17), then heuristically detects paste from rune count/newlines. In v2 this heuristic is largely **obsolete**: real pastes arrive as `tea.PasteMsg`. Recommended rework:
  - Add a `case tea.PasteMsg:` arm in the tty `Update` ([tty.go:176](../../../internal/tty/tty.go) area) that forwards `msg.Content` to tmux as literal text (reuse the existing tmux send-literal path).
  - Keep `IsPasteInput` only if some terminals still deliver multi-rune `KeyPressMsg` without bracketed paste; if so, change `msg.Type != tea.KeyRunes` → `len(msg.Text) == 0` and drop the `msg.Paste` branch.
- [internal/plugins/workspace/interactive.go:376,379](../../../internal/plugins/workspace/interactive.go) — same pattern (`msg.Type != tea.KeyRunes`, `msg.Paste`). Add a `tea.PasteMsg` handler in the interactive-mode key path and forward `msg.Content`.
- **Bracketed paste is on by default** in v2 (disable via `view.DisableBracketedPasteMode`). Keep it enabled so `PasteMsg` fires.

> Manual test paste into a tmux workspace and into the notes/textarea editor after this change — this is the highest-risk behavioral area.

### Step 7 — Mouse: `tea.MouseMsg` interface + struct literals (33 files)

`tea.MouseMsg` is now an interface. Two kinds of breakage:

**(a) Type switches inspecting `Action`/`Button`.** Convert from inspecting `msg.Action` to matching concrete types:
```go
// BEFORE (e.g. internal/tty/tty.go:406, app/update.go:115)
if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
    col := msg.X + 1; row := msg.Y + 1
}
// AFTER
switch msg := msg.(type) {
case tea.MouseClickMsg:
    m := msg.Mouse()              // tea.Mouse{X, Y, Button, Mod}
    if m.Button == tea.MouseLeft {
        col := m.X + 1; row := m.Y + 1
    }
case tea.MouseReleaseMsg: // ...
case tea.MouseWheelMsg:   // wheel; m.Button == tea.MouseWheelUp/Down
case tea.MouseMotionMsg:  // drag/hover
}
```
Button renames: `MouseButtonLeft`→`MouseLeft`, `MouseButtonRight`→`MouseRight`, `MouseButtonMiddle`→`MouseMiddle`, `MouseButtonWheelUp/Down`→`MouseWheelUp/Down`. `msg.X/msg.Y` → `msg.Mouse().X/.Y`.

Heavy sites: [internal/mouse/mouse.go:174-223](../../../internal/mouse/mouse.go) (the hit-test dispatch layer — central; fix here first), [internal/tty/tty.go:399-406](../../../internal/tty/tty.go), [internal/app/update.go:115,1427-1438](../../../internal/app/update.go), [internal/plugins/workspace/mouse.go](../../../internal/plugins/workspace/mouse.go), [internal/plugins/filebrowser/mouse.go:548](../../../internal/plugins/filebrowser/mouse.go), [internal/plugins/notes/mouse.go:109](../../../internal/plugins/notes/mouse.go), [internal/plugins/gitstatus/mouse.go](../../../internal/plugins/gitstatus/mouse.go), [internal/palette/mouse.go](../../../internal/palette/mouse.go).

**(b) `tea.MouseMsg{...}` struct construction (interface — won't compile).** Sidecar reconstructs a mouse msg with an adjusted Y offset:
- [internal/app/update.go:146](../../../internal/app/update.go): `adjusted := tea.MouseMsg{X, Y: msg.Y-headerHeight, Button, Action}` — used to re-dispatch to plugins below the header. In v2 you cannot build the interface. Options:
  1. Don't rebuild the message; instead pass the adjusted `(x, y)` coordinates into the plugin's mouse handler directly (the cleaner fix — `internal/mouse` already abstracts coordinates).
  2. Construct the concrete type, e.g. `tea.MouseClickMsg{Mouse: tea.Mouse{X: m.X, Y: m.Y-headerHeight, Button: m.Button}}` and re-dispatch. (Verify the exact struct shape of `MouseClickMsg` against the v2 source — it embeds/wraps `tea.Mouse`.)
- [internal/palette/mouse.go:38](../../../internal/palette/mouse.go): same `adjusted := tea.MouseMsg{...}` pattern — same fix.
- **Test files** construct `tea.MouseMsg{...}`: [internal/app/view_test.go:108,153,194](../../../internal/app/view_test.go), [internal/modal/modal_test.go:319+](../../../internal/modal/modal_test.go), [internal/plugins/gitstatus/discard_test.go:114](../../../internal/plugins/gitstatus/discard_test.go). Rewrite to construct the concrete v2 type (e.g. `tea.MouseClickMsg{Mouse: tea.Mouse{X:…, Y:…, Button: tea.MouseLeft}}`). Note `HandleMouse(tea.MouseMsg)` signatures can keep the interface param type; only the construction changes.

> Decide a single house style for re-dispatching with offset coordinates (option 1 above is recommended — thread coords, not messages) and apply it in both `app/update.go` and `palette/mouse.go`.

### Step 8 — Misc command/string fixes

- **`tea.EnableMouseAllMotion()` command removed.** Sites: [app/update.go:349](../../../internal/app/update.go), [workspace/update.go:767,1115](../../../internal/plugins/workspace/update.go). These re-enable mouse after `tea.ExecProcess` (tmux attach / `$EDITOR` disable it). In v2, the declarative `view.MouseMode` is re-asserted by the renderer on the next frame, so these manual re-enables should be **removable**. **Verify** by attaching to tmux / opening `$EDITOR` and confirming mouse still works on return; if not, the fallback is to send a one-shot command — check the v2 API for an equivalent (there may be a `tea.EnableMouse`-style helper, or simply trigger a re-render).
- **`case " "` → `case "space"`.** Grep: `grep -rn 'case " "' --include='*.go' .` and any `msg.String() == " "`. (None found in the initial sweep, but re-check after the key changes.)
- **`tea.Printf` / `tea.Println`** at [workspace/interactive.go:782](../../../internal/plugins/workspace/interactive.go) — verify these still exist in v2 (they print above the program). They are expected to remain; confirm against `charm.land/bubbletea/v2` docs. No change anticipated.
- **`tea.QuitMsg`** interception at [tdmonitor/plugin.go:275-278](../../../internal/plugins/tdmonitor/plugin.go) — `tea.QuitMsg` still exists; no change.
- **`isMouseEscapeSequence`** ([app/update.go:28](../../../internal/app/update.go)) and the CSI-fragment suppression in [tty.go:315](../../../internal/tty/tty.go) are workarounds for v1's parser leaking raw mouse bytes as key runes. v2's parser is stricter; after migration, **test whether these are still needed** — they may be removable, but leave them in until proven unnecessary (they're harmless if dead).

### Step 9 — Cursor in the tty plugin

[internal/tty/tty.go:225-233](../../../internal/tty/tty.go) overlays a cursor into the rendered `View()` string via `RenderWithCursor` ([internal/tty/cursor.go](../../../internal/tty/cursor.go)). Because the tty plugin's `View` returns a `string` composed into the app (not a `tea.View`), this overlay approach **still works** — it's just styled text. You do NOT need to convert it to `view.Cursor`. Only if you observe the real terminal cursor fighting the overlay should you consider routing cursor position up to the app's `tea.View.Cursor`. Leave as-is initially; verify the cursor renders correctly in interactive mode.

## Ordered checklist

1. [ ] `go get charm.land/bubbletea/v2@v2.0.7` (re-verify version)
2. [ ] Import path rewrite, 109 files (Step 1)
3. [ ] `NewProgram` options removed (Step 2)
4. [ ] App `View() string` → `View() tea.View` with `AltScreen`/`MouseMode` (Step 3)
5. [ ] `tea.KeyMsg` → `tea.KeyPressMsg` in all switches (Step 4)
6. [ ] Key field renames in `internal/tty/` + `update.go:1200` (Step 5)
7. [ ] Paste → `tea.PasteMsg` in tty + workspace interactive (Step 6)
8. [ ] Mouse interface conversion + struct-literal rewrites incl. tests (Step 7)
9. [ ] Remove `EnableMouseAllMotion`, fix `case " "`, verify `Printf` (Step 8)
10. [ ] `go build ./...` — green only once [bubbles](charm-upgrade-04-bubbles.md) is also done
11. [ ] Update test constructors (`tea.KeyMsg{...}`, `tea.MouseMsg{...}` literals) — see bubbles plan + Step 7

## Gotchas

- **Mouse silently dead** if you forget `v.MouseMode` in `View()` — it's opt-in now.
- **`RequestWindowSize` / `RequestBackgroundColor` are passed uncalled** — they are `func() Msg` which *is* a `Cmd`. `return tea.RequestWindowSize` (no parens). Sidecar doesn't use these today, but if you add the theme handshake, mind this.
- **`msg.Text[0]` is a byte, not a rune** — for the `'1'..'9'` index math at update.go:1200 it's fine, but prefer `[]rune(msg.Text)[0]` if multibyte input is possible.
- **Tests won't compile** until `tea.KeyMsg{Type:…, Runes:…}` literals become `tea.KeyPressMsg{Code:…, Text:…, Mod:…}`. There are ~20 such literals in `internal/plugins/workspace/interactive_test.go` and `internal/tty/keymap_test.go` alone. Expect to spend real time here.
- **Wrong module path** (`github.com/charmbracelet/bubbletea/v2`) resolves to a beta — use `charm.land/...`.
