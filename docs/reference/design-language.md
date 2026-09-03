# Sidecar design language

This is the document to read before mocking up or building any Sidecar surface. It describes what Sidecar actually looks like, cites the Go that draws each part, and ends with the mapping from those parts to the `.tui.yaml` constructs that reproduce them in the TUI mockup studio. Where a rule here and the code disagree, the code wins and this file is wrong; every claim below is written so you can check it against a named function.

Beside this file, `docs/reference/design-language/` holds five frames captured from the running binary at 160x45 through `scripts/tmux-drive.sh`, each as a PNG and as the same grid with colour stripped. They are the reference for everything that follows.

| Frame | Shows |
| --- | --- |
| `td-board.png` / `.txt` | Three stacked panes on the td tab: pane titles, section-header rules, list rows, the cursor and the selected-row fill, the header and the footer |
| `workspaces-split.png` / `.txt` | Two panes side by side with the drag rail between them, a focused pane in the active gradient beside an unfocused one in the normal gradient, and title rows carrying right-aligned hints |
| `git-split.png` / `.txt` | The same split on the Git tab, with a scrollbar and a commit detail |
| `terminal-leaf.png` / `.txt` | A live terminal leaf: its header row carries identity chips on the left and interactive-mode hints on the right |
| `pane-switcher.png` / `.txt` | The pane switcher modal floating over a pane, showing modal chrome and a segmented control |

## The grid rule

Everything in Sidecar is whole terminal cells. A border is one column wide, never a hairline; a pane title occupies one row, never half of one; padding is counted in columns, not in fractions of the font. Widths are measured with `ansi.StringWidth` or `lipgloss.Width` rather than by counting runes, so a double-width glyph (CJK, most emoji) claims two cells and a zero-width one claims none. `internal/styles/borders.go` has its own `runeWidth` for the same reason, and `internal/paneframe` states every chrome cost as a named integer constant: `BorderWidth` is 2, `PaddingWidth` is 2, `Overhead` is their sum, `ContentInset` is what chrome spends on the left alone, and `HeaderRows` is 1. A leaf's content box is `Inset(outer)`, which subtracts exactly those cells and nothing else.

The practical consequence for a mockup is that a border column must land on the same column on every row of a pane, and a row must be exactly as wide as the viewport. If a mockup's border wobbles by a column between rows, something is measuring width wrong and the mockup is lying about what the app will do.

## Pane chrome

### The title is the first row inside the box, not part of the border

Sidecar never writes a title into a border line. There is no `╭─ Title ───╮` anywhere in the app. Every pane draws an unbroken rounded border and then puts its title on the first row *inside* that border, which is what `termpreview.HeaderRows` reserves and what `paneframe.Inset` accounts for. `termpreview.RenderHeader` and `termpreview.HeaderRow` compose that row: identity on the left, advisory hints right-aligned on the right, filled to exactly the inner width so the content below can never lose a row to overflow.

Truncation on that row is deliberately asymmetric. The left side says what the surface is and carries the row's only hit regions, so a chip either fits whole or is dropped entirely and is never clipped mid-chip. The hints on the right are advisory and give way first. `LayoutChips` is the single authority on which chips were drawn and where, so a chip dropped for want of columns cannot keep a live click target.

The title itself is a chip: one column of padding either side, bold, in `TextPrimary`, on a `BgSecondary` fill, sitting on an otherwise unfilled row. In `workspaces-split.txt` the focused pane's first inner row reads `Workspaces` at the left and `⇅ Manual     +` at the right, and the pane beside it reads `sidecar-herdr-detection-parity` with `Diff` at the right.

### Padding

A framed leaf spends one column of border and one column of padding on each side, and one row of border top and bottom with no vertical padding. Content therefore starts two columns in from the pane's left edge and one row down from its top edge, and the title row is the first of those content rows.

### The blank row under the title row, and under a query bar

Two rows are spent on separation rather than on content, and both are cheap in a way that reads immediately:

- One blank row sits between the title row and the first content row. A leaf whose header row is a tab strip rather than a title counts that strip as the title row and takes the same blank under it.
- A surface that takes a query draws the query bar on a row of its own, and one blank row of padding separates it from whatever comes next — a table, a list, a filter line. The blank row is the separator; a horizontal rule in that position is saying the same thing louder.

These two are the one part of this section that is specified rather than transcribed: they were decided against the plugin-ecosystem mockups, not read out of a function, so no Go names them yet and the usual rule — the code wins — has nothing to point at. `docs/plans/active/plugin-ecosystem/mockups/recall-studio.tui.yaml` is the reference for what they look like, and any surface built to them should keep matching it.

### A query bar shows its focus

A query bar has two visibly different states, and the difference is more than a caret. Idle, the prompt (`/`) and the placeholder are `TextMuted`. Taking text, the prompt swaps to `styles.Title`, the text is `Body`, a `▌` block caret sits at the cursor, and the count of what the query matched against what exists is right-aligned on the same row.

**One type draws and drives every query bar: `internal/queryfield`.** `queryfield.Field` is the state, the keys and the render together, wrapping a bubbles `textinput.Model` behind `queryfield.RenderRow`; `workspacelist.Filter` is that field wearing the workspace sidebar's placeholder and count. A new query bar composes the field rather than imitating the row, because a bar that is only drawn like the others still edits unlike them.

Its keys are a text field's keys, and they are the same on every surface:

- **Editing comes from the text input.** Cursor left and right, word forward and back (`alt+←`/`alt+→`, `alt+b`/`alt+f`), word delete (`alt+backspace`, `ctrl+w`), `ctrl+k`, home and end, and paste — none of it written per surface.
- **`ctrl+a` is unbound in a query bar.** It is select-all wherever text is selectable (`internal/textselect`), and a bar that took it for line-start would make one chord mean two things depending on which row had focus. `home` is line-start.
- **`Esc` clears on the first press and blurs on the second; `Enter` accepts and blurs, keeping the text; `ctrl+u` clears.** A query survives losing focus: filtering, pressing enter and working inside the narrowed list is one gesture.
- **`up`, `down`, `pgup`, `pgdown`, `ctrl+n`, `ctrl+p` and `tab` are left unclaimed**, so the surface beneath answers them while the bar has the keyboard and the list keeps navigating. Suggestions stay off precisely because textinput binds those keys to them — a bar that accepted them would take exactly the keys the list needs, and would turn `tab` into a completion. What the surface does with `tab` is then its own: while a query bar has focus the app forwards every key but `ctrl+c` to it, and every query-bar host today swallows the ones it has no answer for rather than letting a stray key reach a surface command.
- **The arrow hand-off.** Where the bar sits above a list of its own results, `down` hands the keyboard to the list on the row under the cursor — the first row, after any list — and `up` or `k` on the first row hands it back with the text intact. Two rows, one keyboard path.

The caret is drawn where the caret is, so a surface that caches its rendered row must key that cache on the cursor as well as on the text: a key that moved only the caret still changed the picture.

A `×` is the row's right-hand control whenever the query is non-empty, right of the count. It clears the query and leaves the caret where it is, which is what the `filter-clear` command does from the keyboard and the palette, and `RenderRow` hands back the rect so the host can register it (`workspacelist.RegionFilterClear` on both Workspaces surfaces, `plugin-query-clear` in the browser). While the query is empty the control is not drawn and nothing is registered: those columns belong to the query row, where a click is "focus the query". Clicking the row focuses it.

### Gradient borders

The border is not a single colour. `styles.RenderGradientBorder` walks the perimeter and asks the gradient for the colour at each border cell's own coordinates, so the corner glyphs and every dash and pipe between them carry their own hex. `styles.Gradient.PositionAt` normalises the cell into the box, projects it onto the angle's direction vector, and divides by the largest projection the box can produce, which puts the top-left corner exactly at position 0 and the bottom-right corner exactly at 1. `ColorAt` then interpolates linearly between the stops, which `NewGradient` spreads evenly from 0 to 1.

Angles are in screen space with y increasing downward, so 0 runs left to right, 90 runs top to bottom, and 45 is the exact diagonal. Every shipped theme sets 30, which is a shallow top-left to bottom-right flow. The value is `styles.DefaultGradientAngle` and it is what `GradientBorderAngle` carries in each theme.

Four gradients exist, and which one a pane wears is a reading of focus rather than a decision the pane makes:

| Gradient | Function | When | Stops in the default theme |
| --- | --- | --- | --- |
| Active | `styles.GetActiveGradient` | The focused pane | `#c0982f` to `#b85d3b` |
| Normal | `styles.GetNormalGradient` | Every other pane | `#3d444a` to `#272d32` |
| Interactive | `styles.GetInteractiveGradient` | The focused terminal leaf while the user is typing into the live pane | `Warning` to `Success`, so `#c0982f` to `#5b8f63` |
| Flash | `styles.GetFlashGradient` | The focused terminal leaf's attention flash | `Warning` to `Accent`, so gold to gold in this theme |

A theme that declares fewer than two stops for a state falls back to a solid border in `BorderActive` or `BorderNormal`, so a gradient border and a solid one are the same code path.

### Focus chrome, and why exactly one pane is ever lit

`paneframe.Chrome` names the five states a leaf can wear: `ChromeIdle`, `ChromeActive`, `ChromeInteractive`, `ChromeFlash` and `ChromeNone`, the last of which leaves the placed box unframed so an already-composed surface does not acquire a second nested perimeter. `WrapLeaf` maps them onto the gradients above.

Exactly one pane in the whole app reads as focused, and that is enforced at the lowest shared seam rather than by a clause in each surface. `styles.SetFocusHeldOutsidePanes` records, for the duration of a content render, that an app-level surface outside every pane tree owns the keyboard; `RenderPanel` and `RenderPanelWithGradient` then draw the normal border instead of the focused one. Every bordered pane in the app goes through those two functions, so a surface added later inherits the rule without knowing the notification centre exists. The flag is a focus signal only: a toast or a pane flash must never set it, because a notification appearing is not the user moving the keyboard.

Content bytes are never dimmed. Only the border changes between states.

## The header

`internal/app/view.go` `headerGeometry` lays out two clusters on a bar filled with `styles.Header`, which is `BgSecondary`. Nothing is centred, and the right cluster's right edge is always the terminal's right edge.

The left cluster is the brand, a divider, then the global tabs. The brand is `" ◱ Sidecar"` in `styles.BrandLogo`, which is `Primary` and bold. The divider is a `│` in `styles.HeaderDivider`, which is `TextMuted`, with one space on each side. Global tabs follow, joined by a single column.

The right cluster is built left to right and then pinned to the right edge: the project tabs joined by a single column, one space, the optional clock, the project selector, one space, the unread indicator, one space, and the configuration gear. The selector is `styles.ProjectSelector`, a pill with one column of padding, `Primary` bold on `SurfaceRaised`, whose label is the project name plus `" ▾"`; in global scope it becomes `styles.GlobalHeaderAction`, which is the same geometry with the fill dropped. The indicator is `·` when there is nothing unread and `●3`, `?12` or `●99+` when there is, coloured by the loudest unread source and inverted while the notification centre is open, and it is capped at five cells. The gear is a plain `⚙` in `styles.ProjectRestore`, one column of padding either side, which takes the `SurfaceRaised` fill on hover.

Both tab groups are drawn by `styles.RenderTab`, so the global tabs and the project tabs are the same control. The default theme selects the `minimal` tab style, which is `"  " + label + "  "` with no fill: the active tab is `Primary`, bold and underlined, and an inactive one is `TextMuted`. The other styles (`gradient`, `per-tab`, `solid`) exist and are selectable per theme, and `styles.PillTabsEnabled` adds Powerline caps when a Nerd Font is available, but a mockup of the shipped default should draw minimal tabs.

Squeeze order matters and is visible at narrow widths. The clock is dropped first because nothing depends on it, then the unread indicator, then project tabs from the right one at a time while never dropping the active one, then global tabs on the same rule, and finally the selector's label is truncated with an ellipsis. The gear never goes, because it is the only pointer route into Configuration. A tab that is fully hidden or partially clipped loses its hit region in the same step, so a click can never land on a tab that is not painted.

Number keys select destinations, and the header does not draw the numbers.

## The footer

The footer is one line, `styles.Footer`, which is `TextMuted` on `BgSecondary`. `renderHintLineTruncated` composes it: for each hint, a `styles.KeyHint` chip carrying the key, then one space, then the plain label, with two spaces between pairs. `KeyHint` is `KeyHintFg` on `SurfaceRaised` with one column of padding, which in the default theme is gold on `#22272c`. A pair that does not fit stops the line; it is never clipped mid-chip, and the leading pair always renders.

Hints are ordered by frequency of use rather than alphabetically, and they come from the active plugin's `Commands()` joined with the app's global bindings. A plugin must not render its own footer; the app renders one unified bar, and a plugin that draws its own produces two.

`internal/ui/keychips.go` is the same construction as an addressable component, used where chips need hover and focus states (modals, palettes). It keeps `KeyHint`'s geometry exactly so a highlight never reflows the line, and swaps only the fill: `Primary` with `OnPrimary` text when focused, `ButtonHover` when hovered. The space between key and label belongs to the label's style run, so a highlighted chip fills as one continuous pill rather than two blocks with a hole punched between them.

A standing plugin condition, meaning something that stays true until someone fixes it, is right-aligned on the same line as a toast-styled block. Transient messages are not footer material; they are notifications.

Two things the footer does not carry. A condition that belongs to a surface, such as the scope a list is filtered to or the outcome of the page it shows, lives on that surface's own rows as a pill or an indicator beside the control that changes it, so the reader finds the state where they would go to change it; a sentence about it in the footer is a symptom of a surface with nowhere to put its state. And a surface never announces that it has nothing to offer. An action that does not apply is absent from the hints and inert on its key; a line such as "no action here" is a design failure, because the missing hint already said so.

## The tab strip inside a pane leaf

A leaf that holds more than one document does not grow a title. Its header row *is* the tab strip. `internal/tabs/strip.go` `LayoutStrip` packs tabs left to right joined by a single column, gives leftover width to the active tab, and marks overflow with a muted `<` or `>` at the end it ran out on. Each tab is rendered through the same `styles.RenderTab` the header uses, so a leaf tab and a global tab are visibly the same control, and each carries a per-tab close control, `ui.CloseButtonLabel`, which is `×`, appended after a space inside the tab. When a tab is too narrow to hold both a label and a close control, the close control is dropped before the label is squeezed to nothing. Close regions are registered after tab regions so the `×` wins the cells it occupies.

Document tabs truncate from the start rather than the end, because the end of a path is the part that identifies the file. A pane taking search keystrokes renames its active tab to the live search surface and its query, so a pane receiving typing never reads as a pane merely showing a file.

## Dividers and drag rails

A split places its children as outer boxes and leaves a one-column gap between them. `paneframe.RenderDividerHandle` draws the rail in that gap through `ui.RenderHandle`: `┃` for a vertical rail, `━` for a horizontal one, with the visible bar stopped one cell short at each end so it never collides with the neighbouring panes' corners while the divider keeps its full allocated geometry. At rest the rail is `Blend(BorderMuted, BorderNormal, 0.30)`, which is subordinate to an unfocused border without dropping to the muted-border contrast; hovered it is `#00BCD4`, and while dragging `#FF9800`.

The pointer target is wider than the paint. `DividerHitBox` widens the one-cell divider by one cell of the leaf on each side, on both axes. That is safe precisely because the cell either side is a leaf's own border and never its header row, which sits one cell further in, so a tab or close button is never masked. `DividerHitBoxFor` declines to take that cell from a borderless neighbour, which owns every cell in its placement.

## Pointer parity

Every action a key performs is reachable by the pointer, and both routes call the same method on the same model, so a click can never do something a key cannot and a keyboard-only surface is a surface with a bug. The rules a surface follows, with the Go that already implements each:

- **Clicking a pane focuses it.** A click anywhere inside a pane's box moves focus there before anything else happens, through the same call the focus ring uses (`paneframe.FocusLeafAt` for a pane tree, a surface's own `SetPaneFocus` for the boxes inside one plugin). Focus is a reading of where the pointer went, so the border chrome above follows it.
- **The wheel scrolls the box under the pointer**, not the focused one, by `mouse.WheelScrollLines` per notch. A wheel at a box's boundary is offered to the host through `WheelBoundaryConsumer` rather than lost.
- **A scrollbar appears whenever content overflows** and is a drag target. `ui.RenderScrollbarWithState` draws it, `ui.RegionScrollbarThumb` and `ui.RegionScrollbarTrack` are its regions, and a press on the track jumps while a drag on the thumb follows, as the file browser and the Sessions surface already do.
- **A list row selects on click and opens on a second click** on the already-selected row or on a double click, which is exactly `Enter` twice. Hover does not select. Where a detail box sits beside the list, selecting also loads it — the detail follows the cursor however the cursor moved, after a quiet period, and what is on screen stays there until the new document lands — so the second click and `Enter` move the keyboard into a document that is already there rather than spending a process (`pluginbrowser.DetailQuiet`, `filebrowser`'s `schedulePreviewForCursor`).
- **A text field focuses on click** and shows it as the query-bar rule above describes. A query bar's `×` is its own target, registered after the row it sits on so it wins the hit test, and registered only where it is drawn.
- **A pill or control on a header row is a hit target.** It is placed through `ui.ReserveHeaderControls`, which is what lets it be dropped whole rather than clipped, and its region is registered from the placement so a dropped control cannot keep a live target.
- **Two boxes inside one surface are separated by a drag rail**, drawn with `ui.RenderHandle`, widened as `paneframe.DividerHitBox` widens the pane-tree rail, and persisted through `internal/state` so the split survives a relaunch. A blank column between two boxes is a rail that has not been drawn yet.
- **Regions are registered in paint order through `mouse.HitMap`, with the rail last**, because `HitMap.Test` scans in reverse and the smallest, most specific target must win.

This section is specified, like the blank-row rule above: it was decided against the plugin browser's parity work (`docs/plans/active/plugin-ecosystem/browser-parity-and-scope.md`) rather than transcribed from one function, and the functions it names are the ones a new surface composes rather than a single one it calls.

## List rows

A list row is composed from pre-styled spans and then, when selected, painted with a background. The pieces:

| Element | Style | Default theme |
| --- | --- | --- |
| Cursor | `styles.ListCursor`, a `❯` in `Primary`, bold | `#c0982f` |
| Selected row | `styles.ListItemSelected` | `TextSelection` on `BgTertiary`, so `#ffffff` on `#171b1f` |
| Focused row | `styles.ListItemFocused` | `OnPrimary` on `Primary` |
| Selected card (multi-coloured kanban and overview cards) | `styles.CardSelected` | `TextSelection` on `CardSelectedBg`, deliberately darker than the board so project hues stay readable |
| Secondary line | `TextMuted` or `TextSubtle` | `#858e95` or `#697177` |
| Badge or pill | A filled span, one column of padding, semantic background with `OnPrimary` or `OnWarning` text | varies |

Non-selected rows keep a two-column gutter where the cursor would be, so text never shifts as the cursor moves.

Painting the background is not a matter of wrapping the row in a lipgloss background. A row's inner spans each close themselves with an SGR reset, which clears the background as well as the foreground, so wrapping leaves holes. `ui.RowBackground` walks the row once and re-asserts the background after anything that touches it, leaving foreground, bold and underline exactly as the row wrote them, then truncates and pads to exactly the width. The selected row is therefore the same row, highlighted, rather than a differently styled row.

A section header is a rule, not a box: a bold label, then a `─` fill in `BorderNormal`, then a right-aligned count in `TextMuted`. The active section's label takes a semantic colour; see `td-board.txt`, where `IN PROGRESS` is teal and its count sits at the right end of the fill.

## Status tones

Colour is earned by semantics, never used for decoration. Gold is the only strong accent in chrome, and everything else says something.

| Tone | Palette key | Default theme | Means |
| --- | --- | --- | --- |
| Neutral | `TextMuted`, `TextSubtle` | `#858e95`, `#697177` | Metadata, counts, inactive labels |
| Info | `Info`, which is the same as `Secondary` | `#4a8f8f` | Identifiers, open state, in progress |
| Success | `Success` | `#5b8f63` | Done, live, healthy |
| Warning | `Warning`, which is the same as `Primary` and `Accent` | `#c0982f` | Attention, blocked, P2, the focus accent |
| Danger | `Error` | `#c06c64` | Failing, P1, destructive |
| Link | `Link` | `#4b8fd6` | Hyperlinks and markdown headings |

Text drawn on a filled span uses `OnPrimary` or `OnWarning` rather than the body foreground, because the body colour on a gold fill is close to illegible. `styles.NormalizePalette` derives those roles and `internal/styles/contrast.go` picks tab text by measured contrast rather than by assumption, so a custom theme cannot produce an unreadable chip.

Structure is carried by a seven-step neutral ramp rather than by saturation. The default theme's backgrounds are `BgPrimary` `#0f1113` for the canvas, `BgSecondary` `#131619` for bars and title chips, `BgTertiary` `#171b1f` for a selected row, `SurfaceRaised` `#22272c` for key chips and bar pills, and `ButtonHover` `#2f3438`. Borders are `BorderNormal` `#3d444a`, `BorderActive` `#c0982f` and `BorderMuted` `#272d32`.

## Modals

`styles.ModalBox` is a rounded border in `Primary` on a `BgSecondary` fill with `Padding(1, 2)`, which is one blank row top and bottom and two columns each side. There is no drop shadow. The border line is unbroken, like every other border in the app: `styles.ModalTitle` puts the title *inside* the box, bold in `TextPrimary`, with one blank row under it. `internal/modal` composes sections (text, buttons, inputs, lists, combos) into that box, and `ui.OverlayModal` places it over the content.

Buttons come from `styles.Button`, `ButtonFocused` and `ButtonHover`, each with two columns of padding; destructive actions use the `ButtonDanger` family. A modal's own hint line is the same key-chip row the footer uses. See `pane-switcher.png` for a live one, including the segmented control whose active segment takes the `Primary` fill.

## Empty and loading states

An empty surface says what is true and offers the next action. It does not disappear, and it does not show a spinner for something that is not loading. `workspaces-split.txt` is the pattern: a short statement of the state, one line naming the way out, and the action itself as a button. An empty section within a list also says so rather than vanishing, because a section that disappears is indistinguishable from a section that failed to load.

Loading is a different claim from empty, and Sidecar distinguishes them. `ui.Skeleton` draws animated placeholder rows with a shimmer band at roughly 12 frames per second, and `ui.BrailleSpinner` is a passive spinner that advances on the skeleton's tick rather than generating its own. The reason both exist is startup latency: anything a plugin does in `Init()`, and anything `Start()` does before returning its command, runs before the first frame is painted, so filesystem walks and subprocess spawns belong in a command with a loading state rendered until the result arrives. A mockup that shows a populated pane where the real thing will show a skeleton is misleading about the surface's first second.

## Mapping to the mockup studio

The TUI mockup studio at `~/code/tui` reproduces this chrome rather than approximating it. The Sidecar-specific constructs are transcriptions of the Go named above, so a mockup lands on the same columns and the same hex values as a capture of the running binary; `npm test` in that repo asserts exactly that against values read out of `design-language/workspaces-split.txt`.

Start every Sidecar mockup with `theme: "sidecar-modern"`, which is `SidecarModernTheme` from `internal/styles/themes.go` transcribed key for key. The older `theme: "sidecar"` is an unrelated blue-accented palette kept for the mockups that already use it.

| Element | `.tui.yaml` |
| --- | --- |
| The header bar | `- type: sidecar_header` with `global_tabs`, `project_tabs`, `active_global` or `active_project`, `project`, and optional `global`, `clock`, `indicator`, `gear` |
| The footer bar | `- type: sidecar_footer` with `keys: [{ key, label }, ...]` and an optional `status` |
| A pane's title as its first inner row | `title_position: inside` on a `pane`, with `padding: [0, 1]` for Sidecar's inner padding, plus `title_style` and `title_right` / `title_right_style` for the right-aligned hints |
| A gradient border | `border_gradient: active` (or `normal`, `interactive`, `flash`), which expands to the theme's own stops, or an explicit list such as `["#c0982f", "#b85d3b"]`, with `border_gradient_angle` defaulting to 30 |
| The drag rail between two panes | `- type: sidecar_rail`, optionally `state: hover` or `state: drag` |
| A leaf's tab strip | `- tabs: { sidecar: true, close: true, items: [...], active: 0 }` as the leaf's first content item, with no `title` on the pane |
| An inline key chip | `[key:Enter]` in any text, which draws `styles.KeyHint`; the older `[kbd:Enter]` draws a bracketed form instead |
| A selected list row | `[bg:surface0 fg:accent bold]❯[/bg][bg:surface0 fg:selection-fg]…[/bg]`, which is `ListCursor` over `ListItemSelected` |
| A modal | A `modals` entry with `background: "surface"`, `shadow: false`, no `title`, and the title as the first content lines: a blank row, then `"  [bold]Title[/bold]"`, then a blank row |
| Status tones | The theme roles `accent`, `secondary`, `success`, `warning`, `error`, `link`, `muted`, `subtle`, `raised`, `rail`, `on-accent`, `selection-fg` |

The default for `title_position` stays `border`, so mockups written before these constructs existed render unchanged.

Two things the studio still cannot draw. It has no per-cell background gradient, so anything that fades a fill rather than a border has to be approximated with flat spans; nothing in Sidecar's chrome needs one today. And it has no live cursor: a text cursor in a mockup is a reverse-video cell, whereas the app places a native terminal cursor that `capture-pane` cannot see at all, which is why cursor placement is checked with an attached viewer client rather than in a mockup. See `docs/guides/active/headless-testing.md`.

## Related documents

`docs/guides/active/launch-visual-language.md` records the launch design study the current theme was transcribed from, including the palette with its roles. `docs/reference/terminal-background-fidelity.md` covers how an embedded terminal's cells take their colour, which is a different problem from the chrome described here. `.agents/skills/create-theme/SKILL.md` and its palette reference cover building a theme against these roles.
