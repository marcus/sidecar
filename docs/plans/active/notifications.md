# Notifications — toasts, centre, indicator, sources

**Status:** Phase 1 (steel thread) **done**; Phase 1.5 **done**; Phase 2 **done** (both halves — see the two "Phase 2 as built" sections); Phase 3 **done** (see "Phase 3 as built"); Phases 4–7 planned, not started. Next up: `target-activation.md` (separate plan, prerequisite), then Phase 5 — Phase 4 is deliberately deferred behind them
**Created:** 2026-08-18
**Design:** claude.ai/design project `3172ac49-4413-4a60-9235-0afa5c77cf77`, file `Sidecar Notifications.dc.html` (frames 1a–1h). The design is authoritative for visual grammar. Two deliberate deviations, decided by Marcus: the sources config lives on the existing config screen (`internal/configui`), not the design's invented one; and the notification centre is an **app-level right panel that pushes all content left** (see "The centre" below), not the in-pane split the design's frame 1c sketches.

## Ground rules for implementing agents

- **Follow the design as closely as possible** — glyphs, hues, section
  grammar, spacing, the countdown cells, the exact footer key rows. When the
  design and this plan conflict, this plan wins (it encodes later decisions).
- **Respect the existing sidecar UI.** Use shared components (`internal/ui`,
  `internal/modal`, the shared meta-column/section-rule grammar, styles/theme
  keys) and plug into canonical systems (`headerGeometry`, `internal/mouse`
  hit maps, `internal/uirequest`, `internal/terminallink`, `internal/config` +
  `configui`, the 1s heartbeat and tagged ticks). Inventing new infrastructure
  is allowed **only when necessary** — and the necessity should be stated in
  the commit or plan update. A second compositor, border rule, or key-routing
  scheme is a bug.
- **Keyboard shortcuts: the implementing agent has autonomy.** Choose keys by
  availability (check `.claude/skills/keyboard-shortcuts` and the keymap
  registry), with limited permission to rebind an *existing* shortcut when it
  is obscure and the newcomer is clearly more frequently used — note any such
  rebind in the plan update. Keys named in this plan are suggestions:
  lowercase `n` is probably taken; `N` very well may be free and is the
  expected choice for toggling the centre. The design's per-surface key rows
  (`d`, `D`, `m`, `T`, `s`, `tab`, digits) follow the same rule.

## Summary

A real notification system replacing the single-line footer toast:
macOS-style toasts drawn as bordered floating blocks in the top-right of the
content region, a notification centre right panel, an unread indicator in the
header next to the gear, per-source config on the existing config screen, and
an agent-facing `sidecar notify` CLI so agents can post (and dismiss their
own) notifications. Tasks integrate both ways: due tasks post here, and any
notification can be filed back onto a task as a reminder.

**Every current toast becomes a notification.** There is no legacy footer
toast path once this lands, and therefore no double-alerting: the old-style
alerts *are* the new system. The workspace list keeps its attention icons
next to shell names — that is list-view state, not an alert — and the same
underlying event additionally posts a notification. A per-source preference
(1g table) lets users quiet the notification without touching the list icons.

Design frames, for reference while implementing:

- **1a** single toast: bordered block, source-hued border + rule line, title,
  body, key row `enter open · d dismiss · s snooze`, cell-drawn countdown
  `▰▰▰▱▱ 4s`. (Snooze is deferred — render the key row without it until
  Phase 6; tasks handle their own snoozing by re-posting.)
- **1b** stacking: newest on top, max 3 on screen; same-source toasts collapse
  into one block with `×N` and a "▾ 2 more · tab expand" peek line.
- **1c** the centre's *content* grammar: sections per source (`◆ AGENTS`,
  `? WAITING`, `✓ SESSIONS`, `○ TASKS`, `■ TD`), unread `●`, times in the
  shared meta column, "Dismissed items clear after 24h" footer note. Footer
  keys: `j/k move · enter open · d dismiss · D dismiss group · m mute source ·
  T to task · esc close`. (The *container* differs from 1c — see below.)
- **1d** corner indicator next to the gear, ≤5 cells: `·` empty, `●3` unread,
  `?12` agent waiting, red `●99+` on failure/clamp, `◌4` muted, inverted while
  the panel is open. Colour carries the loudest unread source.
- **1e** calls to action: every id in a notification is a project-aware jump —
  td issue → td tab, task → tasks tab, commit → git diff, `file:line` →
  files/$EDITOR, session → attach, web → $BROWSER (confirmed). Numbered 1–N
  for keyboard jumps. Cross-project targets render `repo/td-xxxxxx`.
- **1f** tasks both ways: a due task posts a notification; `T` on any
  notification writes a REMINDERS entry onto a task.
- **1g** sources config: per-source rows with `toast / centre / bell / expiry`
  columns, plus a behaviour block (stacking on/off, max toasts, quiet hours,
  suppress-while-resizing, `t` test toast). "Anything suppressed still lands
  in the centre and still counts in the corner."
- **1h** the reveal spec — the only motion allowed: one whole row per frame,
  ~90ms apart, no subpixel travel, no fades; reveal top-down (4 frames in),
  retract bottom-up (3 out) so the border never redraws twice; countdown
  ticks one cell per second, no tween; skipped entirely on dumb/slow
  terminals — the toast just appears.

## The centre: an app-level right panel

Per Marcus's screenshot (2026-08-18): the centre is a full-height right panel
with its own title ("Notifications") and close affordance, **owned by the app
shell, not by any plugin**, that pushes the active plugin's content to the
left — the same visual family as the workspaces plugin's right panels. It is
not an overlay and not a modal.

Consequences the implementing agents must take seriously:

- **This is the largest architectural piece of the feature.** Today plugins
  receive the full content `(width, height)` in `View`. The app must be able
  to reserve a right-hand column and hand every plugin a narrower width —
  uniformly, for td, git, files, notes, workspaces, and the kanban/task
  views. The correct mechanism is the canonical one: shrink the `width`
  passed to `plugin.View()` and re-emit size updates when the panel opens,
  closes, or resizes, exactly as a terminal resize would. Plugins that
  mishandle a narrow width are pre-existing bugs to fix, not reasons to
  special-case the panel.
- Reuse the shared pane grammar for the panel itself: the resize rail
  (`internal/paneframe` / drag-pane machinery) for width adjustment, shared
  section rules and meta columns for content. Panel width persists in state.
- **No navbar tab.** The centre is reached only via the header indicator
  (click) and its shortcut (likely `N`). `esc` or the close affordance
  closes it. Focus routing follows the existing key-precedence rules
  (`plugin.KeyRouter` docs); while the panel has focus it consumes list keys,
  and clicking back into content returns focus without closing the panel.
- **The panel stays open until the user closes it — across all navigation.**
  Switching plugins/tabs, opening files, changing projects or worktrees,
  entering and leaving modals: none of these close the panel. It is app-shell
  state (like the header), not per-plugin state, and its open/width state
  survives plugin `Reinit` on project/worktree switches. This is a real
  integration cost — every navigation path must re-emit the narrowed size
  rather than resetting to full width, and transitions that rebuild the
  plugin registry must restore the reservation before the next frame — and
  it is part of the plan, not an edge case. Only an explicit close (`esc`
  with panel focus, the close affordance, or the indicator toggle) dismisses
  it. Unread state updates live (the store is in the app model, no polling
  needed).

## Architecture

One core package, thin surfaces — core in a library; every capability has a
non-interactive path; store behind a narrow interface, JSONL first.

- **`internal/notify`** — the model and store. `Notification{ID, Source,
  Severity, Title, Body, Targets []Target, CreatedAt, ReadAt, DismissedAt,
  ExpiresAt, Origin, Sticky}`. Sources are registered (`agent`, `waiting`,
  `session`, `tasks`, `td`, `system`, plus external registrations), each with
  glyph + hue matching 1c/1g. Store is JSONL under the sidecar state dir
  (`notifications.jsonl`, appended events: posted/read/dismissed), compacted
  on load, 24h retention for dismissed items, behind a small `Store`
  interface. State-free resolution logic (what may toast, what counts as
  unread, loudest-source colour, quiet hours) lives here so a headless caller
  could adopt it unchanged.
- **Posting API (in-process):** a `notify.PostMsg` tea.Msg + helper in
  `internal/app/commands.go`, like `ToastMsg` today. `msg.ShowToast` becomes
  a thin adapter posting a `system`-source notification; the footer status
  rendering path (`internal/app/view.go` toast block) is removed in the same
  phase. Callers do not change.
- **Posting API (out-of-process):** a new `uirequest.Action` (`notify`) on
  the existing file-RPC bus (`internal/uirequest`), and `sidecar notify
  post|dismiss|list` in `internal/cli/registry.go` — which lands it in
  `sidecar --agents` automatically. `--json` on `list`. Dismissal via CLI is
  origin-checked: agents may dismiss only notifications they created. `list`
  reads the JSONL directly so it works with no TUI running; `post` falls back
  to a direct JSONL append when no instance is announced, so nothing is lost.
- **Triggers:** the 1s heartbeat already resolves
  `agentstatus.Presentation`; lane *transitions* (working→blocked, →done,
  session ended/failed) post notifications. The tasks source is a polled
  adapter behind the same interface. **No git source and no GitHub polling**
  — deliberately out of scope; the source registry makes it a later add if a
  real local data source ever appears.
- **Rendering (toasts):** composited over the active content region via
  `internal/overlay` (the command palette is the precedent for floating
  bordered surfaces). Toasts render whether or not the centre panel is open.
- **Reveal framework:** a tiny `internal/reveal` package implementing the 1h
  spec — a row-count state machine driven by a tagged tick (~90ms), generic
  over "a block of N rows". This is the one piece of genuinely new
  infrastructure, justified because nothing in sidecar animates today except
  the intro; it becomes the reusable primitive for any future motion.
  Auto-disabled on dumb/slow terminals (toast just appears).
- **Config:** a top-level `Notifications` section in `internal/config`
  (pattern precedent: `Selection`, `TerminalResources`) + a new `configui`
  page rendering the 1g table with the existing form components
  (`toggleRow`, `selectRow`, `FormRow`). Suppressed ≠ dropped: everything
  still lands in the centre and counts in the corner. Includes the per-source
  switch that quiets agent-waiting notifications for users who find the
  workspace list icons sufficient.

## Steel thread (Phase 1)

Smallest end-to-end slice proving the whole pipe: **an agent posts a
notification from a shell; a bordered toast appears top-right; the header
shows `●1`; the centre shortcut opens the right panel listing it (content
pushed left on every plugin); `d` dismisses it; it persists across restart
until dismissed.**

1. `internal/notify`: model, source registry (hardcoded set), JSONL store,
   unread/loudest resolution. Unit tests.
2. `notify.PostMsg` handling in `internal/app/update.go`; store owned by the
   app model; expiry sweep on the 1s tick.
3. `uirequest` `notify` action + `sidecar notify post` (title, `--body`,
   `--source`, `--expiry`) and `sidecar notify dismiss <id>`
   (origin-checked), with the no-instance JSONL fallback.
4. Toast rendering: single bordered block, top-right of the content region,
   source hue, key row, cell countdown. No stacking, no reveal yet — it just
   appears (which is also the spec'd degraded mode, so nothing is thrown
   away later).
5. Header indicator: `●N` / `·` next to the gear inside `headerGeometry()`,
   with a degradation rank (outlives the clock, drops before the gear);
   click toggles the centre; inverted style while open.
6. **The right panel**: app-level width reservation, plugin `View` width
   reduction + resize propagation across all plugins, resize rail, persisted
   width; centre content as a flat list grouped by source with unread `●`,
   `j/k`, `d` dismiss, `D` dismiss group, close via `esc`/click. `enter` is
   a no-op for now. This step is the bulk of Phase 1; verify every plugin
   (td, git, files, notes, workspaces, kanban/task views) at narrow widths,
   and verify the panel stays open (and correctly sized) across tab
   switches, project/worktree switches, and modal open/close.
7. Route `ToastMsg` through the new system and delete the footer toast path.
8. Proof run via `scripts/tmux-drive.sh` (isolated state — see AGENTS.md):
   post from a second shell, snap the toast, the indicator, the open panel on
   at least three different plugins, dismiss, restart, verify persistence.

## Phase 1 as built — decisions and deviations

Landed in commits a7bcbe9f, f6540456, 5c3020c2 plus a review/proof fix pass.
Everything in the steel thread above is implemented. What follows is what a
later phase needs to know that the plan above does not already say.

**Read state.** A notification is marked read when it is selected in the centre,
or when a toast that was *actually painted* expires. The "actually painted" gate
matters: expiry alone silently reads what the user never saw — a notification an
agent posted while sidecar was closed arrives already past its countdown, and
with one toast slot a burst would read everything queued behind the newest.

**Keys** (the plan gave the implementing agent autonomy here):

- **`alt+n` always toggles the notification centre.** `N` also does, but it
  yields to any context that rebinds it — git's `N` is prev-match and is worth
  more there than this toggle. Since the centre has no navbar tab, `alt+n` is
  what guarantees a keyboard route on every tab.
- **`N` toggles the notification centre**, as the plan expected. It is also the
  way *back* into an open panel: with the panel open but blurred, `N` refocuses
  it; pressing it again closes. Nothing was rebound to make room.
- **`d` dismisses the toast on screen** — design 1a's key row, and the same key
  the centre uses, so one key means one thing. It is a global fallback and
  yields to any focused context that binds `d` for itself (`git-status`,
  `workspace-list`, `workspace-preview`, `config`, …) via
  `contextRebindsKey`. Precedence level 3 was *not* enough on its own: it only
  covers plugins implementing `plugin.KeyRouter`, which is `tasks` alone, and
  the rest bind `d` after the global switch.
- **The `notification-centre` keymap context** owns `j/k`, `d`, `D`, `enter`
  (inert until Phase 5), `esc` and — since Phase 2 — `tab`/`shift+tab`,
  registered in `internal/keymap/bindings.go` like every other context.
  Navigation keys — the tab digits, `[`/`]`, `` ` ``/`~`, `K`, `@`, `W`, `^`,
  `?`, `,` — are deliberately *not* claimed: they blur the panel and run
  normally, so a keyboard-only user can leave the panel without closing it.
  `tab` was on that list in Phase 1.5 and no longer is: it is now the focus
  cycle's move onward and is consumed rather than released (see Phase 2 as
  built). The panel still closes only on `esc`, the close affordance, or the
  toggle.

**Read semantics.** A notification is marked read when it is selected in the
centre (on open, on cursor move, on click) and when its toast countdown runs
out. Sticky notifications have no countdown and stay unread until answered.
Without this nothing ever set `ReadAt`, the header climbed all session now that
every `ShowToast` is a notification, and unexpired notifications toasted again
after a restart.

**Store concurrency.** `JSONLStore` re-folds the file from disk under an
exclusive `flock` (the same shape as `internal/shellstate`'s manifest lock)
before every write and before every rewrite. Folding once at open and re-emitting
memory silently deleted anything another process had appended — the CLI's
no-instance fallback, or a second Sidecar sharing the global state dir. `Sweep`
(the 1s heartbeat) is also the cross-process read point: a record another
process appended becomes visible to a running instance within a second.

**Cross-project posts — decided.** The store and the centre are **global in
Phase 1, deliberately**. `notify` requests are still routed by the caller's
origin, but that check answers "which instance acknowledges this request" and
"who may dismiss this record", *not* "who may see it". A post from a project no
running instance is showing is declined on the bus, falls back to the direct
JSONL append, and — since `Sweep` re-folds — surfaces in every running instance
on the next heartbeat rather than being delivered nowhere until a restart. That
is the smallest honest behaviour for a global store with no per-project filter
in the centre. `Origin.ProjectKey` is recorded on every record, so per-project
filtering (or a "this project / everything" toggle) is a later view change, not
a data migration. Revisit alongside the Phase 4 config page.

**Dismissal origin.** `sidecar notify dismiss` sends the *caller's* origin in
`Request.Origin` with the target id in `Target.Value`. It used to send the
target's origin, which made the host's `MayDismiss` compare the record against
itself and pass unconditionally — anything able to write a request file could
dismiss anyone's notification.

**`-config` before a subcommand.** `internal/cli.Run` now strips leading global
flags (`-config`, `-project`, `-debug`, `-enable-feature`, `-disable-feature`,
either spelling) before matching a command, and applies `-config`. Without it
`sidecar -config <path> notify post` fell through to TUI startup and died with
"Sidecar requires an interactive terminal" — which is exactly the invocation an
isolated proof run needs.

**Other deviations.** The toast's countdown renders minutes and hours above 60s
rather than raw seconds (`5m`, not `290s`). The toast block's lipgloss `Width`
is its outer width — passing the interior width left every toast's title rule
two cells too wide, wrapping a `──` stub onto the next row. `internal/termpreview`
took a narrow-width fix so the workspace preview lays out correctly inside the
content region the panel leaves behind.

**Deferred from Phase 1.** Dragging the panel's resize rail emits a resize
storm: every drag frame re-sizes every plugin, and live panes re-read on each.
Phase 3 already owns the fix ("suppress-while-pane-resizing guard") and the
config toggle for it (1g); it is not worth a second mechanism here.

## Phase 1.5 — two-tier notifications and centre polish

Driven by first real use of Phase 1 and by the call-site audit at
`docs/reference/audits/notification-inventory.md` (85 toast call sites:
24 keep, 15 consider, 46 remove). All decisions below are settled with Marcus
(2026-08-19).

**1. Two tiers: notifications and status flashes.**

- **Notification** — the Phase 1 artifact, unchanged in kind: bordered toast,
  lands in the centre, counts in the header. For agent events, real errors,
  blocked actions with a reason, and surprising state changes.
- **Status flash** — new, lightweight, **not stored**: a single line at the
  top-right of the content region (same corner as toasts, for spatial
  consistency), starting with a colored source glyph, fading in and out. No
  centre entry, no history, no unread count. This is the home for routine
  confirmations — "Saved", yank/copy, sidebar toggles — that deserve feedback
  but not persistence.
- Call sites choose the tier explicitly: keep `msg.ShowToast` → notification,
  add a parallel flash message/helper. The flash path never touches the store.
- **Flashes replace, never queue:** a new flash immediately supersedes the one
  on screen. (Notification stacking/queueing — max 3 vertical then queue,
  macOS-style — is Phase 3's spec and is not pulled forward.)
- **Fade** is real color interpolation: 2–3 luminance steps in and out using
  theme colors, driven by a tagged tick; degrade to plain appear/disappear on
  dumb/slow terminals. If it proves too costly it can be backed out to
  appear/disappear, but start with the interpolation.

**2. Re-tier the audited call sites.** Work from the audit doc's tables:

- **REMOVE rows (46):** pure no-ops are deleted outright — "Already on this
  project/worktree", "Nothing to undo", "No title/content to copy",
  "Showing all/X sessions", "Nothing to commit". Every other REMOVE row
  (yank/copy confirmations, "Saved", sidebar toggles, "Opened …", move
  success) becomes a status flash.
- **KEEP rows (24):** stay full notifications. Route the blocked-action and
  merge/commit-lifecycle rows (audit rows 32–35, 72, 79) through more specific
  sources than `system` so hue/priority distinguishes "act on this" from FYI.
- **CONSIDER rows (15):** resolve during implementation by reading the actual
  dynamic strings at each site, per the questions in the audit doc; split
  mixed sites (error branch → notification, routine branch → flash or delete).

**3. Centre polish.**

- Gradient border on the centre panel, matching every other content pane
  (shared border styling, not a second border rule).
- Centre entries go to **two lines** (title + body) so the CTA isn't lost.
- **Re-show as detail view:** `enter` on a centre entry re-presents that
  notification as a toast — this is the "view details" action and gives
  `enter` a job before Phase 5 rebinds it to target activation (digit keys
  and target jumps remain Phase 5; re-show stays as a secondary key then).
- **Shared symbol logic:** one helper renders a source's glyph + hue, used by
  toast, flash, and centre alike, so an item looks the same everywhere.

**4. Toast presentation tweaks.**

- Countdown made less prominent: dimmer/smaller cells, same tick behaviour.
- Default expiries lengthened: 12s for `agent`, 10s for `system` / `session` /
  `td` / `tasks`; `waiting` stays sticky. **These live in `internal/config`
  from day one** (a minimal `Notifications` section with per-source expiry),
  not as constants — no configui page yet, but the values must be
  user-editable in the config file, ahead of the full Phase 4 page which
  will render them.

**5. Simplify toast interaction — toasts never take focus.** Drop the
focusable-toast model entirely: toasts have **no focus context** and must
never steal focus from whatever the user is doing, including at the moment
they appear. The only interactions are **click to dismiss** and the global
`d` fallback where the focused context allows it (not while an interactive
terminal or editor owns keys — the existing `contextRebindsKey` yielding
already encodes this). A toast appearing must not change key routing, cursor
position, or the focused context in any way. (`alt+n`/`N` opening the centre
remains the deliberate, user-initiated route to interact further.)

**6. Housekeeping.** Commit `docs/reference/audits/notification-inventory.md`
alongside this plan so the phase references a stable document.

## Phase 1.5 as built — decisions and deviations

Landed in commits 1fe1497b (flash tier, config expiries, shared glyph),
73871d81 (centre polish, quieter countdown, click-to-dismiss) and 6bb0d44c
(re-tiering every audited call site), plus a review fix pass. All six items are
implemented. What follows is what a later phase needs and the plan above does
not say.

**The flash tier.** `msg.FlashMsg` / `msg.ShowFlash` / `msg.ShowFlashFrom`,
re-exported as `app.FlashMsg` / `app.ShowFlash` / `app.ShowFlashFrom`. All flash
state and rendering is `internal/app/flash.go`: one slot, replace-never-queue,
tagged `flashTickMsg{seq}` so a burst cannot leave two animations fighting over
the line. It never touches `notify.Store` — no centre entry, no unread count.
Fade is real sRGB interpolation (3 steps in, ~2s hold, 3 out) at a 90ms cadence,
composited by `internal/overlay` against the *content region*, so the centre
narrowing content moves it too. `flashAnimated()` is the degraded-terminal check
(`TERM` empty/`dumb`, or `SIDECAR_NO_ANIMATION`), resolved once via `sync.Once`;
Phase 3's `internal/reveal` should move that helper somewhere shared rather than
adding a second one.

- **Deviation:** the flash sits one row *below* a toast when both are painted
  rather than in the same cell. The spec's "same corner" is honoured; overlap
  would be unreadable.

**Source-aware notifications.** `notify.Alert(source, severity, title)` builds
the `PostMsg`; `msg.Alert(...)` is the `tea.Cmd`, and `msg.Blocked(reason)` is
the refusal shape — a `waiting`/warning with an explicit 6s lease. Use `Blocked`
for every refusal: `waiting` is sticky by default, so a bare waiting alert on a
keypress-frequency refusal leaves one permanent unread entry per keystroke.
(That was the one real defect the review pass found, in gitstatus'
`writeBusyToast`.)

**Config.** `config.NotificationsConfig{Sources: {id: {Expiry}}}`; `"sticky"` /
`"never"` / `0` mean no countdown; an unparseable value is warned and skipped,
never a load failure. `notify.ApplyConfig` binds it — called in `app.New` before
the store opens, on the config-screen save, and in `runNotifyPost` so the CLI's
fallback path completes records the same way. `notify.ExpiryFor` is now the only
correct way to ask how long a source toasts for; `Source.DefaultExpiry` is the
built-in floor. `config.Save` does not manage the `notifications` key, so it
survives on its unknown-key preservation path. Phase 4's configui page renders
over this struct — extend it, do not add a parallel one.

**Centre.** Panel body goes through `styles.RenderPanel` (the shared gradient
pane border, active while the panel is focused), which costs 2 rows and 4
columns — hence `notificationCentreDefaultWidth` 34 → 38 (min/max unchanged) and
the interior geometry shift the hit regions follow. Entries are two rows (title +
`TextSubtle` body) carrying the same item index, so the cursor spans both and a
click on either selects the entry; an entry with no body stays one row. `enter`
= "view details": `reshowNotification` re-presents the selection as a toast.
That is presentation only — a copy with a fresh countdown, no re-post, no
un-dismiss, no unread change — and a sticky re-show gets a `system`-length lease
so the slot is never held forever. When Phase 5 rebinds `enter` to target
activation, move re-show to a secondary key and update both
`keymap/bindings.go` and `notificationCentreCommands`.

**Toasts.** Countdown cells are `▪`/`▫` at `TextSubtle`/`BorderNormal` (tick
math unchanged). There was **no focusable-toast model to remove** — Phase 1 never
gave toasts a focus or keymap context — so item 5 reduced to adding the missing
click route (`regionToast`, registered and cleared inside `renderToastOverlay`,
tested after the centre's column and before content) and dropping the `enter
open` hint that nothing ever honoured. The key row is now `click or d dismiss`.

**Re-tiering.** Sources chosen for the KEEP rows: "Reviewed source changed" →
`waiting`/warning (sticky — a stale review is a correctness risk); every other
refusal → `msg.Blocked`; agent fallback → `agent`/warning; merge aborted,
catalog drift, session ended → `session`; terminal errors → `session`/error; td
setup/status failures → `td`/error; task created → `td`/info. The routine half
of every mixed site became a flash, and the enumerated pure no-ops ("Already on
this project/worktree", "Nothing to undo", "No title to copy", "Nothing to
commit", filter confirmations) were deleted outright.

- **Deviations worth Marcus's eye:** the once-ever default-theme notice (row 27)
  is now a flash and leaves no trace — a one-line revert if that reads wrong.
  Notes' "Archived/Deleted notes are read-only" is a flash rather than a
  `Blocked` because it fires on every keystroke into a read-only buffer.
  `docview`/`filebrowser` still flash "No content to copy" where notes deletes
  it (rows 43/44): silence on a copy key that did nothing reads as a broken
  key, and a flash costs nothing persistent.
- `internal/msg` now imports `internal/notify`, and `internal/notify` imports
  `internal/config` — both leaves, no cycle — so every plugin can post a
  source-specific notification without importing `internal/app`.

## Polish round 2 — queued behind target-activation

From live use (Marcus, 2026-08-19, nt-9519f6). Runs after
`target-activation.md` completes; before Phase 5.

**Toast redesign** (deviates from 1a/1h; this plan wins):

- Drop the key row ("click or d dismiss") and the countdown row; global `d`
  and click-to-dismiss keep working, only the hints go. Two rows saved.
- An `×` close button top-right of the block; the countdown loader cells move
  to the title row, left of the `×` (no numeric time, cells only). Sticky
  notifications show just the `×`.
- Reveal/retract ~25% faster (90ms → ~67ms per row); flash fade steps
  proportionally.

#### Toast redesign as built (2026-08-19)

- **The block is now border · title row · rule · body · border.** The key row
  and the standalone countdown row are gone from `renderToastBlock`
  (`internal/app/toast_view.go`), and so is the blank spacer that only existed
  to separate the body from the key row — **three** rows saved on a bodyless
  block, not the two the spec counted, because the spacer had nothing left to
  separate. Nothing else needed changing to follow the shorter block: the
  reveal machine takes its row count from `lipgloss.Height(block)`, and
  `syncToastReveal`'s height budget, the read gate and the hit regions all read
  that same rendered block, so the row counts followed by construction.
- **Controls live in the title row, right-aligned:** the countdown cells
  (`toastCountdownMeter`, cells only — `toastRemaining` and its numeric label
  are deleted) then a space then `×`. A sticky notification shows just the `×`.
  The title truncates against whatever the controls leave.
- **The `×` is its own hit region** (`regionToastClose:<stack key>`) registered
  *after* its block's, so the later region wins the press (`HitMap.Test` is
  last-wins), and only once the reveal has released the title row. Both region
  prefixes resolve to the same stack key, so the `×` and a body click run the
  identical `dismissToastStack` — the button dismisses exactly its own block,
  including when that block is not the top one.
- **`reveal.Step` is 67ms**, which speeds both motions in the system because
  `flashStep` is now literally `reveal.Step` rather than a second 90ms
  constant. `flashFadeSteps` went 3 → 4 so the fade lasts the same wall-clock
  quarter-second over one more interpolation step. The countdown is unaffected:
  it ticks off the 1s heartbeat, not the reveal frame.
- Verified in the real app through `scripts/tmux-drive.sh` (isolated tmux +
  state, `paths` checked, stopped after): a live `agent` toast draws
  `◆ Agent finished        ▪▪▪▪▪ ×` with the body below it and no key row; a
  sticky `waiting` block draws the `×` alone.

#### Review of the toast slice (2026-08-19)

- **Fixed: the `×`'s hit region was one cell right of the glyph.** A block is
  border · padding · inner · padding · border, so the interior's last cell is
  `blockWidth-3`, not `blockWidth-2`; the region sat on the right padding cell.
  It was invisible in behaviour — the block's own region covers the glyph and
  runs the same `dismissToastStack` — and would have become a real miss the
  moment the two regions meant different things. The column is now
  `toastCloseCol(blockWidth)`, read by the hit map, and the close-button test
  asserts the region is the cell the glyph is drawn in (mutation-checked: the
  test fails at `-2`).
- Checked and sound: the rendered block, the reveal row count, the height
  budget in `syncToastReveal` and the read gate all derive from
  `lipgloss.Height` of the same string, so the two-row removal moved them
  together; the close region is registered after the block's (last-wins) and
  only once `Rows() > 1`, so it can neither steal a press from another block
  nor exist before the title row is painted; a sticky or expired countdown
  returning `""` changes no row count because the meter lives in the title row.
- Stale `90ms` comments in `toast_stack.go`, `update.go` and `reveal.go` now
  name `reveal.Step` instead of a number that has changed.
- **Deferred nit:** the read gate marks the lead painted at `Rows() > 0`, i.e.
  with only the border row on screen. Unreachable today (the sweep is on the 1s
  heartbeat and a reveal finishes in well under that), but `Rows() > 1` — "the
  title row is legible", the same test the close button uses — is the truer
  gate if the cadence ever slows.

**Centre:**

- Double-click on an entry = `enter` (view details today; activation when
  Phase 5 rebinds it — double-click follows whatever `enter` means).
- An `×` at the right of each group header clears the group (same action
  as `D`): `◆ SYSTEM ───────────── ×`.

#### Centre interactions as built (2026-08-19)

- **`enter` is now one function, `activateSelectedNotification`**
  (`internal/app/notification_centre.go`), and the double-click calls it. The
  key case is a one-liner over it, so when Phase 5 rebinds `enter` to target
  activation the pointer follows by construction — there is no second action
  to keep in step. Single click is unchanged: select + mark read, no toast.
- **Double-click detection is `internal/mouse`'s existing one.** The handler
  already counts successive clicks at a cell and `HandleMouse` already returns
  `ActionDoubleClick` (the file browser is the precedent); the centre's mouse
  switch already listed that action alongside `ActionClick`, so this was a
  branch inside the item case, not a new mechanism or a timer.
- **`D` is now `dismissNotificationGroup(source)`**, and the header `×` runs
  it with the group it was drawn on — so the control clears its own group
  regardless of where the cursor sits.
- **The `×`'s column is decided once**, by `notificationGroupClearCol(inner)`:
  the renderer reserves that cell at the end of the rule and
  `registerNotificationCentreRegions` puts a 1×1 region
  (`notification-centre-group-<source id>`) on exactly it. Below six interior
  columns it returns -1 and the header is a plain rule again. Header rows now
  carry their source in `centreRow.group`, which is also what keeps them out
  of the item-region loop.
- **Known, pre-existing:** the resize rail's widened hit box overlaps the
  panel's first column, so a press on the leftmost cell of a row is a drag,
  not a select. The tests click a few columns in. Unchanged by this slice.
- Verified in the real app through `scripts/tmux-drive.sh` (isolated on both
  axes, `paths` checked, stopped after): the centre draws
  `◆ AGENTS ─────────────────────── ×`.

**Selected-row background consistency (general fix, scoped).** Selected rows
in the notes list and the centre render inconsistent backgrounds depending on
the row's text — pre-styled spans (hues, links) punch holes in the selection
highlight. This is a recurring sidecar problem: fix it as a shared
styles-layer helper that applies a row background across already-styled
content, adopted by notes and the centre in this round; other plugins migrate
opportunistically, not in this round.

#### Selected-row background as built (2026-08-19)

- **The helper is `ui.RowBackground(row, width, bg)`** in
  `internal/ui/row_background.go`, with `RowBackgroundSeq` for callers holding
  an escape sequence already and `SelectedRowBackground` for the theme's
  selection colour. **Deviation from the wording of the spec:** it lives in
  `internal/ui`, not `internal/styles`. `internal/ui` is the established home
  for ANSI row surgery — `injectRangeBackground`, `CarryRowBackground`,
  `ApplyTerminalDefaultBackground` and the `sgrBackground` parameter parser are
  all there — and `ui` imports `styles`, so putting it in `styles` would have
  meant a second copy of that parser. It reuses the existing one instead, which
  is what "no second scheme" asks for.
- **What it does:** truncates the row to width with `ansi.Truncate`, then walks
  it once and re-asserts the background after *anything* that touches the
  background — a bare `\x1b[m` or `\x1b[0m`, a compound reset like
  `0;38;2;…`, an explicit `48;2;…`/`48;5;…` an inner span set for itself, a
  legacy 40-47/100-107 code — then pads to exactly width with
  background-coloured spaces and terminates with a reset so the fill cannot
  bleed. Foreground, bold and underline are left as the row wrote them: the
  selected row is *the same row*, highlighted.
  `styles.FillBackground` is the older, weaker cousin (reset-substitution only,
  no truncation, no explicit-background override); it is untouched here and is
  a candidate to be re-expressed over this walk later.
- **Adopted in two places.** `styleCentreRow`
  (`internal/app/notification_centre.go`) replaces a
  `lipgloss.Background().Render(padNotificationRow(...))` — both rows of an
  entry go through it, so the two-line highlight is one rectangle covering the
  source-hued unread dot, the muted age column and the muted body.
  `renderNoteRow` (`internal/plugins/notes/view.go`) previously **rebuilt the
  selected row as plain text** so a single `Background()` could cover it, which
  is why a selected note lost its status-icon colour, cursor and pin styling;
  it now renders the ordinary styled row and highlights that.
- **Tests** assert the property that matters rather than a byte string: a
  helper walks the rendered row and records the background active at *each
  visible cell*, and the row is only correct if every cell carries the
  selection background and the row is exactly `width` cells wide.
  `internal/ui/row_background_test.go` runs that over adversarial inputs
  (nested lipgloss, both reset spellings, inner 256/truecolour/legacy
  backgrounds, a `38;2;49;0;0` foreground that contains a `49`, wide runes and
  emoji, over-width and under-width rows); `internal/app` and
  `internal/plugins/notes` assert it end-to-end on the real renderers, plus
  that unselected rows carry no background at all.
- Verified in the real app through `scripts/tmux-drive.sh` (isolated on both
  axes, `paths` checked, stopped after): the selected agent entry draws as a
  solid two-row block across the hue glyph, the title, the age column and the
  muted body.
- **Opportunistic follow-ups, deliberately not in this round:** every other
  surface that still highlights by rebuilding a plain row or by wrapping a
  styled one in `Background()` — the overview/workspace lists, git status,
  file browser, conversations, the kanban `CardSelected` path — is one call to
  `ui.RowBackground` each, with no behaviour change beyond the holes closing.

**Tab parity (global).** Every surface's tab cycle must reach the open centre
— notes, git, files, conversations implement `FocusCycler` so tab cycles
their panes then the centre (their two-pane `tab` toggle becomes a ring with
the centre as the extra stop **only while the centre is open**; closed, tab
is exactly today's behaviour). Embedded td is best effort. Centralize in the
existing `FocusCycler`/`panelayout.focusring` machinery — no per-plugin
bespoke cycles.

#### Tab parity as built (2026-08-19)

- **All five surfaces now implement `plugin.FocusCycler`** — `notes`,
  `gitstatus`, `filebrowser`, `conversations` and, yes, the embedded `td`
  monitor — each in a new `focus.go` of its own, each with a compile-time
  `var _ plugin.FocusCycler` assertion. There is no new key routing and no
  per-plugin cycle: the shell's `notificationCentreTabKey` and the ring helpers
  in `panelayout` are the same ones workspace and overview already used.
- **One helper carries the four two-pane surfaces**: `panelayout.TwoPaneRing`
  (plus `panelayout.ContentPaneTarget`) is `Ring` for a layout that is a list
  beside one content pane rather than a tree. `AtRingEnd`/`RingStart` then
  answer both questions exactly as they do for the tree surfaces, so the stop
  lands where the wrap was. A window that is not drawn is not a stop — a hidden
  sidebar, a diff pane with nothing selected, a preview with no file, notes
  with no note open — which means on those the centre becomes the *second* stop
  rather than the third, and `tab` alternates surface ↔ centre.
- **Each surface offers the ring only in the contexts that have one, and it
  answers with its own `FocusContext()`** rather than a second list of booleans
  that could drift from it: `git-status*`/`git-commit-preview`,
  `file-browser-tree`/`-preview`, `conversations-sidebar`/`-main`,
  `notes-list`/`notes-preview`. Everything else declines — searches, filters,
  file-op and resume modals, blame, info, the inline editors, git's full-screen
  diff and its commit/push/pull modes, conversations' turn detail and analytics,
  and above all the notes editing pane, where `tab` saves and leaves the edit
  and is therefore a mode exit, not a focus cycle. This is the same discipline
  workspace uses to decline during a doc or terminal search.
- **Notes moves focus by running the key's own code.** The two halves of what
  `tab` always meant there are now `focusEditorPane` / `focusListPane`, and both
  the key handler and `FocusCycleStart` call them, so the note pane arrives
  resting (not editing) on either route and there is no second copy of the
  action to keep in step.
- **The embedded td monitor is in, not deferred.** It is the one surface whose
  ring is not sidecar's: td owns the panels and the cursor-clamping and scroll
  bookkeeping that go with moving between them. So `AtFocusCycleEnd` reads td's
  exported `Model.ActivePanel` (last panel forward, first panel back, and only
  in td's main context — `tab` is a button cycle in its modals, a section jump
  in an epic, and a completion in its forms), and `FocusCycleStart` **replays
  `tab`/`shift+tab` into the hosted model** rather than assigning a panel, so
  the wrap is td's own `% 3` with its clamp and scroll fix-up intact.
- **One shell change was required, and it is the interesting one:
  `notificationCentreTabKey` now asks the `FocusCycler` BEFORE it consults the
  keymap.** Reading the registry first made the answer depend on the *name* a
  surface gave its own cycle: td registers `next-panel`, which is not
  `next-pane`/`switch-pane`, so the shell read it as "this surface owns tab
  outright" and the centre was unreachable on the td tab even with the ring in
  place — and git's diff pane registers nothing at all. A surface that
  implements `FocusCycler` has declared in code that its `tab` is a ring and is
  the only thing that knows where that ring ends, including in the modes where
  it is not a ring; the registry check stays exactly where it was for everything
  that does not implement it.
- **A one-frame staleness fixed on the way out:** `leaveNotificationCentreFocus`
  blurred (and so re-read the context) *before* the handback moved focus inside
  the surface, so the footer described the window the keyboard had just left
  until the next event redrew it. It now re-reads the context after
  `FocusCycleStart`. Caught in the drive proof, not in a test — there is a test
  for it now.
- **No new keymap bindings.** Registering `tab → switch-pane` for the contexts
  that lack it (`notes-list`, `git-status-diff`, `git-commit-preview`) would
  have been display-only for the footer and risked changing dispatch on
  surfaces that handle the key themselves; the ring answer already runs ahead of
  the registry. Worth revisiting if those footers should advertise the stop.
- **Tests, per surface**: the ring includes the centre at the wrap point and
  only there, in both directions; the handback lands on the window the cycle
  resumes at; a pane that is not drawn is not a stop; every sub-mode keeps
  `tab`; and the surface's own key handler still performs the exact toggle it
  always did (which is the closed-centre behaviour, since the shell never asks
  while the panel is shut). Plus `panelayout.TwoPaneRing`, and two shell tests:
  a ring beats the registered binding whatever it is named, and tabbing out
  re-reads the surface's context.
- Verified in the real app through `scripts/tmux-drive.sh` (isolated tmux +
  state, `paths` checked, stopped after) with a live notification posted through
  the isolated CLI: on the **td** tab `tab` walks Current Work → Task List →
  Activity → **centre** and repeats (period four); on **git status** it walks
  sidebar → diff → **centre** → sidebar; on the **file browser** with no file
  previewed it walks tree → **centre** → tree, which is the "not drawn is not a
  stop" rule doing its job. Notes and conversations are not in this project's
  tab set, so they are covered by their package tests.

#### Review of the centre, row-background and tab slices (2026-08-19)

No defects found in these three; nothing changed. What was checked:

- **Centre pointer.** The group `×` and the entry rows cannot overlap — a
  header row is skipped by the item loop and carries only its own 1×1 region,
  registered after the panel-wide region so it wins the press. Its column comes
  from `notificationGroupClearCol(inner)` with the same `inner = panelWidth-4`
  the renderer uses, and both sides degrade together below six columns.
  Double-click is `internal/mouse`'s 400ms same-region counter, so a slow second
  click is an ordinary click; the single-click path still selects and marks
  read before the branch, so read-marking is unchanged either way.
- **`ui.RowBackground`.** The ANSI walk re-asserts the background after every
  reset, explicit `48;…` and legacy code through the *existing* `sgrBackground`
  parser; non-SGR sequences (OSC 8 hyperlinks) pass through untouched, and
  truncation is `ansi.Truncate`, so no escape is cut mid-sequence. Unselected
  rows do not go through it at all — the old code padded only in the selected
  branch too, so nothing regressed there.
- **Tab parity and the shell reorder.** Asking the `FocusCycler` before the
  keymap is safe for every context that binds `tab` to a non-cycle command:
  `config`/`config-edit` are not cyclers, and `file-browser-project-search`,
  `file-browser-file-op` and `notes-task-modal` are all declined by their
  surface's `FocusContext` switch. Toasts still register no focus and no
  surface gained a focus-stealing path.

#### Final proof run of polish round 2 (2026-08-19)

Driven headlessly through `scripts/tmux-drive.sh` at 200x50 on an isolated tmux
server *and* state tree (`paths` checked first — everything under
`/private/tmp/scproof`, nothing under `~/.local/state/sidecar` or
`~/.config/sidecar` — and `stop` at the end). Notifications were posted through
the same isolated binary (`sidecar -config <run>/config/config.json notify
post`), and pointer input was raw SGR mouse sequences sent to the host pane.
Notes and conversations, which are not in this project's default tab set, were
brought on screen for the run with `{"features":{"flags":{"notes_plugin":true,
"conversations_plugin":true}},"plugins":{"conversations":{"enabled":true}}}` in
the throwaway config. **All four items pass; no code changed.**

- **Toast.** The block is exactly border · title · rule · body · border:
  `│ ◆ Agent finished                 ▪▪▪▪▪ × │` over
  `│ ────…──── │` and `│ Body line for the toast │`. No key row, no countdown
  row; the meter sits in the title row left of the `×`. A sticky `waiting`
  toast draws `│ ? Waiting for input                    × │` — the `×` alone.
  Reveal cadence, sampled by capturing the pane in a tight loop and stamping
  each frame: the block grew 3 → 4 → 5 rows at +67ms and +64ms, i.e. the 67ms
  step, not 90ms. The countdown still burns one cell per second
  (`▪▪▪▪▪` → `▫▪▪▪▪` → `▫▫▪▪▪` over ~2s off the 1s heartbeat).
- **Toast close button, three-stack.** With Charlie/Bravo/Alpha stacked, a
  click on the `×` of the *middle* block (col 197, its own row) removed Bravo
  and left Charlie and Alpha in place, header indicator `●3 → ●2`.
- **Centre.** Single click on an entry selects and marks read and posts no
  toast; a double-click on the same cell re-showed that entry as a toast
  (`◆ Alpha one … ×` appeared above the existing stack). Clicking the `×` on
  the `● SYSTEM` header cleared the SYSTEM group and left every AGENTS entry
  untouched.
- **Row background.** Measured by walking the captured SGR stream and recording
  the background at each visible cell. A selected centre entry carries
  `48;2;34;39;44` continuously across cols 165–198 on *both* of its rows with
  no gaps, and the row above it carries none. A selected notes row carries
  `48;2;23;27;31` continuously across cols 3–56 while keeping its own
  foreground — `\x1b[1m\x1b[38;2;192;152;47m\x1b[48;2;23;27;31m> * \x1b[0m…`,
  the background re-asserted after the inner `[0m`, which is the whole point of
  the fix. Unselected notes rows carry no background at all.
- **Tab parity, centre open** (active pane read from the gold `191;148;47`
  border): notes → list → preview → **centre** → list (period 3); git → sidebar
  → diff → **centre** → sidebar (period 3); files with nothing previewed →
  tree → **centre** → tree (period 2, the "not drawn is not a stop" rule);
  conversations → sidebar → main → **centre** (period 3). With the centre
  closed, conversations alternates sidebar ↔ main exactly as before. With the
  file browser's `/` search open, `tab` changes nothing — the search keeps it.
- **Observed, out of scope, not fixed:** while the centre holds focus, a
  plugin-level key with no centre binding still reaches the plugin — pressing
  `/` on the files tab opens the file browser's search from a focused centre.
  That is app-wide key routing under `SetFocusHeldOutsidePanes`, not a polish
  round 2 behaviour, and changing it is a routing decision rather than a fix.

## Bottom-status sweep + ship — after polish round 2, before Phases 4/5

Marcus (2026-08-19): after polish round 2, ALL remaining bottom-of-screen
transient toasts/flashes are replaced by the notification system (classified
per the Phase 1.5 audit rules), then this branch ships: merge `main` into
`notification-center` (main has unrelated files-pane work), then merge this
branch to `main` and release. Phases 4–7 continue after shipping.

The residual sites (audited 2026-08-19; everything else is clean or is a
legitimate standing indicator):

1. `internal/plugins/notes/plugin.go:1299` — the `"notes: saving…"`
   in-flight branch of `FooterStatus` goes (routine, self-resolving). The
   `saveErr`/`recoveryErr` branches STAY — standing error conditions with no
   other always-on surface are (b)-class, not toasts.
2. `internal/plugins/gitstatus/sidebar_view.go:234-244` — `✓ Pushed` /
   `Fetched` / `Pulled` + their clear-after-delay tick plumbing → flashes,
   exactly like the already-migrated stash path (`plugin.go:872-888`).
3. `internal/plugins/gitstatus/sidebar_view.go:215,249,258,267` —
   `operationError`/`pushError`/`fetchError`/`pullError` one-liners → error
   notifications (source per the re-tier rules); the push-error modal stays,
   the sidebar echo goes.
4. `internal/plugins/gitstatus/commit_view.go:156-170` — decision: the
   modal-local `"Committing…"` progress and `commitError` lines STAY (a
   modal's own status is form feedback, not a screen-bottom toast) unless
   live use says otherwise.
5. Embedded td/tasks footer bands are td's own UI — out of scope here.

Ship checklist: sweep lands with review; merge main → branch, resolve,
full suite + one headless smoke; merge branch → main; release per
`docs/guides/active/releasing.md`.

#### Bottom-status sweep as built (2026-08-19) — **done**

Items 1–3 landed; items 4 and 5 stand as written (untouched).

- **Notes (1).** `FooterStatus` lost only the `saveInFlight ||
  exportSaveInFlight` branch; `saveErr` and `recoveryErr` still claim the
  footer. The editor header's `Saved`/`Unsaved*` is the save indicator.
- **Git success (2).** `Push`/`Fetch`/`Pull` success are now
  `app.ShowFlash("Pushed"/"Fetched"/"Pulled")`, matching the stash path. The
  sidebar status block is **in-flight only** (`activeOperation` progress label,
  then `Pushing…/Fetching…/Pulling…`); the `✓ Pushed/Fetched/Pulled` rows and
  their whole tick plumbing are gone — fields `pushSuccess`/`pushSuccessTime`/
  `fetchSuccess`/`pullSuccess`, the three `*SuccessClearMsg` types and
  handlers, and the three `clear*AfterDelay` helpers.
- **Git errors (3).** The sidebar error one-liners are gone and the failures
  are notifications: `session`/`error`, per the Phase 1.5 re-tier rule for
  terminal errors. The push/fetch/pull error **modals stay** — the notification
  is the record that it happened, the modal is the full text. The old
  `app.ToastMsg` on `operationResultMsg` (which spoke as `system`) became the
  same session/error alert. Dead state removed with the rendering:
  `pushError`/`fetchError`/`pullError` **and** `operationError`, which nothing
  read once the sidebar line went.
- **Two lines, not a transcript.** Git failures file through
  `remoteFailureAlert` (`gitstatus/plugin.go`): short title (`Push failed`),
  body = `firstMeaningfulLine` of git's output, preferring `! …`/`error:`/
  `fatal:` over the `To ../remote.git` preamble. Filing the raw multi-line
  stderr as the *title* was the review fix.
- **Geometry.** `commitSectionCapacity` reserves the status row on the
  in-flight flags alone, so the removed rows' line is genuinely reclaimed.
- **Proof (headless, isolated repo + bare remote).** Stage-all → commit → push:
  `● Pushed` flash top-right, no sidebar line. Rejected push: session/error
  toast (`Push failed` / `! [rejected] HEAD -> main (fetch first)`) plus the
  Push Failed modal, entry under `✓ SESSIONS` in the centre, no sidebar echo.
  Fetch: ahead/behind updates, no `✓ Fetched` row. Notes edit + `ctrl+s`: no
  `notes: saving…` in the footer, header goes `Unsaved*` → `Saved`.

## Enhancements

Ordered so each phase ships something visible and none blocks the next.

**Phase 2 — session & waiting triggers.** Post from `agentstatus` lane
transitions: agent finished (`✓ SESSION`, pass/fail colour), agent waiting
(`? WAITING`, sticky — no countdown, "stays"), session died. Debounce so a
flapping status doesn't spam. Workspace list icons are untouched; the
per-source config (Phase 4) is the off-switch for users who don't want both.
Also in Phase 2 (decided 2026-08-19, **built** — see "Phase 2 as built: tab as
a focus stop"): **`tab` cycles focus through the open centre like any other
pane.** With the centre open, the app-level focus cycle
includes it as a stop — tab into it, tab onward back into content — exactly as
other panes participate. This replaces the Phase 1 decision to leave `tab`
unclaimed; `alt+n`/`N`/click remain as direct routes. (When a collapsed toast
is on screen, its `tab expand` (1b) must not fight the focus cycle — prefer a
different expand key if both can be live at once.)


### Phase 2 as built: tab as a focus stop

Only the tab item; the agentstatus triggers are the section after this one.

- **The mechanism is the surfaces' own ring, extended — not a second cycle.**
  `plugin.FocusCycler` (`internal/plugin/plugin.go`) is a new optional
  capability with `AtFocusCycleEnd(reverse) bool` and
  `FocusCycleStart(reverse) tea.Cmd`. A surface that implements it keeps
  cycling its panes exactly as before; the press that *would have wrapped* its
  ring goes to the centre instead, and the next press hands focus back to the
  window the ring resumes at. `panelayout.AtRingEnd` / `panelayout.RingStart`
  answer both questions from the same ring `CycleTarget` walks, so the stop
  can only land where the wrap was.
- **Implemented for the parity pair**: `internal/plugins/workspace`
  (`focus.go`) and `internal/overview` (`focus.go`), each in the one file that
  already owned focus, with a compile-time `var _ plugin.FocusCycler`
  assertion. Workspace declines while a doc or terminal search input is live —
  that surface uses `tab` to leave the input, and a shell stop taking the key
  would strand a drawn search box.
- **Everything else**: `notificationCentreTabKey` (`internal/app/notification_centre.go`)
  runs in `handleKeyMsg` right after the panel's own key block and before any
  surface sees the key. With no `FocusCycler` it takes `tab` only where the
  focused context has not bound it (`contextRebindsKey` / `pluginClaimsKey`),
  and it always stands aside for text input, blocking overlays, interactive
  panes, and any context that binds `tab` to something that is not
  `next-pane`/`switch-pane`.
  **Known limit, deliberate:** on surfaces that own `tab` for a two-pane
  toggle rather than a ring (`git-status`, `notes`, `file-browser`,
  `conversations`, the hosted `td` tab) the centre is *not* a tab stop — those
  toggles have no wrap point to insert into. `alt+n`, `N` and the pointer are
  the routes there. Turning those toggles into rings is a follow-up, and it is
  one `FocusCycler` implementation each, no shell change.
- Leaving is symmetric and consumed: `notificationCentreKey` answers
  `tab`/`shift+tab` itself (`leaveNotificationCentreFocus`) instead of
  releasing them, so one press = one stop and the surface underneath does not
  also act on the key. The panel is never closed by `tab`; `esc`/close/toggle
  are still the only closes. Focusing by tab is the same focused state as a
  click — gradient border active, list keys live, selection marked read.
- Footer/help: `focus-content` is a real command on the panel
  (`notificationCentreCommands`) bound to `tab`/`shift+tab` in
  `keymap/bindings.go`, so the hint row reads `… esc Close · tab, shift+tab
  Content` and the binding is reboundable like any other.
- Verified in the real app through `scripts/tmux-drive.sh` (isolated tmux +
  state): on Workspaces, `tab` walks sidebar → preview → **centre** → sidebar
  with the panel staying open throughout; on the hosted td tab `tab` still
  drives td's own panels, as designed.

### Phase 2 as built: session & waiting triggers

The agentstatus half of Phase 2. Both halves are now done.

- **The rules live in `internal/notify/triggers.go`** (`LaneTracker`,
  `LaneObservation`, `LaneEvents`) and know nothing about tmux, plugins, or
  Bubble Tea. A caller hands `Observe` the **complete** set of workspaces it can
  speak for plus a clock; it gets back notifications to post and ids to
  withdraw. The package imports `internal/agentstatus` (a pure leaf) rather than
  stringly-typing lanes.
- **Transitions, not states, and only settled ones.** A lane must hold for
  `Debounce` (`DefaultLaneDebounce` = 3s) before it is committed. A flap
  working→blocked→working inside that window posts nothing. A committed lane
  becomes the tracker's truth, so the same logical event cannot post twice: the
  next post needs a *different* settled lane first. That is the debounce and the
  dedupe in one mechanism rather than two.
- **First sight is a baseline, never a notification.** Starting Sidecar beside
  four already-blocked agents must not open with four toasts about states the
  user already knew.
- **What posts.** blocked → `waiting`/warning, **sticky**, "`<name>` needs
  input". working|blocked → done → `session`/info, "`<name>` finished" (the
  pass/fail split the plan asked for: a finish is info). working|blocked|done →
  paused **with `Presentation.Health`** → `session`/error, "`<name>` session
  ended". Plain paused (no health) is not a death. idle→done is the done-TTL
  bookkeeping, not a finish, and posts nothing.
- **Self-dismiss — decided.** The tracker **assigns the notification id itself**
  (the store's `Post` is id-preserving and idempotent) precisely so it can name
  the waiting notification later. Any settled transition *out of* blocked
  withdraws it, and so does the workspace disappearing from the observation set.
  A "needs input" toast that outlives the wait is worse than no toast. A
  workspace that simply vanishes gets its waiting withdrawn but earns **no**
  death notification — a shell the user closed is not an incident, and a session
  that really failed reaches the paused/health lane while still observed.
- **Body/identity.** Title carries the shell or worktree name; body is
  `provider · project[/branch] · evidence`, so one of five agents is
  identifiable from the toast alone. `Origin` is `{TmuxSession, ProjectKey,
  WorkDir}` — the same shape the CLI sends, so an agent's own `sidecar notify
  dismiss` and a lane trigger agree on identity.
- **Wiring — a deviation worth recording.** The plan said "the app wires it to
  the heartbeat". The app shell has no per-shell agent state at all: the only
  place `agentstatus.Presentation` exists per workspace is the workspace plugin,
  which already polls. So the adapter is
  `internal/plugins/workspace/agent_triggers.go`, called from the single sweep
  seam at the bottom of `Plugin.Update` (`terminal_control.go`) beside the focus
  rule and the live-watch reconcile — the three status-apply sites each end in a
  dozen early returns, so hooking them individually would have been three
  chances to miss one. It emits ordinary `notify.PostMsg` / `notify.DismissMsg`
  commands; nothing new was added to the app shell.
- **Only readable agents are observed.** A workspace with no `Agent`, or one
  whose provider `agentactivity` cannot read, produces no observation — its lane
  is a projection of legacy status and would announce transitions nobody made.
  Worktrees, top-level shells, and nested (sibling) shells are all in the set;
  leaving nested shells out would make every sibling look vanished and withdraw
  live notifications.
- **Workspace list icons untouched**, as specified. Phase 4's per-source config
  is the off-switch for users who find the icons sufficient.
- Tested without tmux: `internal/notify/triggers_test.go` (baseline, flap,
  once-only, self-dismiss, vanish, per-workspace independence, the
  finish/death lane rules) and
  `internal/plugins/workspace/agent_triggers_test.go` (observation filtering,
  nested shells, the post→dismiss round trip through real `tea.Cmd`s).

**Phase 3 — stacking + reveal.** Max 3 toasts on screen, newest on top;
posts beyond 3 **queue** (macOS-style, decided 2026-08-19) and surface as
slots free, oldest queued first. Same-source collapse to `×N` with peek line
and expand (1b) — this is also where repeated `waiting` refusals dedupe.
`internal/reveal` row machine per the 1h spec; wire toast entry/exit through
it (adopt `flashAnimated()`'s degraded-terminal check via a shared home).
Suppress-while-pane-resizing guard.

### Phase 3 as built: stacking + reveal

- **The stacking rules are state-free and live in `internal/notify/stack.go`**
  (`Stack`, `Layout`, `StackToasts`). The app shell asks what belongs on screen
  and gets back an answer any surface could have computed, exactly as with
  `Toastable` and the lane triggers.
- **Collapse is per *source*, and the source id is the block's identity** — the
  collapse key, the reveal key, and the pointer target are one string. That is
  what makes a block keep its animation when a second notification joins it, and
  it is the mechanism by which repeated `waiting` refusals dedupe: they are one
  block with `×N`, not a column of near-identical ones. Consequence worth
  knowing: there are six sources, so the queue only engages when more than three
  sources are live at once.
- **Admission is first-come-first-served; display is newest-on-top.** Two
  different orderings on purpose: a stack is admitted by its *oldest* member (so
  a chatty source cannot shove a block off the screen the instant before it is
  read) and the admitted blocks are painted newest first per 1b. A freed slot is
  filled by the heartbeat's sweep, not only by the next post.
- **The read gate survives stacking, which was the risk.** Only what is legible
  is recorded as painted: the lead of each *visible* block, plus the listed
  members when the block is expanded. A queued block, a block that did not fit
  the remaining height, and a collapsed member are all unpainted, so expiry
  cannot read them — they stay unread and wait in the centre.
- **Expand key: `alt+e`.** Design 1b says `tab`; Phase 2 spent `tab` on the
  focus cycle whenever the centre is open, and one key cannot mean two things.
  `alt+e` sits in the same family as the centre's guaranteed `alt+n`, is global
  (a toast can be on screen on any tab and has no focus context of its own), and
  falls through untouched when nothing on screen is collapsed. It is registered
  as `expand-toast` in `keymap/bindings.go` like every other binding. The peek
  line renders the real key, not the design's.
- **`internal/reveal`** is the 1h row machine and nothing else: `New/Advance/
  Leave/Resize/Rows/Clip`, one integer, generic over "a block of N rows".
  Because reveal is top-down and retract is bottom-up, both directions are "how
  many rows from the top are painted", so `Clip` is the whole renderer contract
  and a border is never redrawn mid-motion. `reveal.Animated()` is the **shared
  home** for the degraded-terminal check that Phase 1.5 asked for;
  `flashAnimated()` is now a one-line call into it, so the two motions cannot
  disagree about a dumb terminal. `SetAnimatedForTests` exists because the real
  answer is (deliberately) resolved once per process.
- **Wiring.** `internal/app/toast_stack.go` owns the column: `toastStacks` (the
  store's layout plus the presentation-only re-show slot, which takes over its
  source's block rather than opening a second one), `syncToastReveal` (called on
  every path that can change the column — post, `ToastMsg`, dismiss, re-show,
  the 1s sweep, and the reveal tick itself), and the `revealTickMsg` loop, which
  stops the moment every block has settled rather than holding a 90ms timer over
  a still screen. Blocks are rendered at sync time and cached, so the render
  path stays pure.
- **Dismissal — decided.** Click or `d` on a collapsed block dismisses **all**
  its members, mirroring the centre's `D dismiss group`. Making the user clear
  the same block five times to empty it would be a worse bargain, and the `×N`
  told them what they were clearing. Dismissing a re-shown block clears the
  presentation copy only, never the record behind it.
- **Suppress-while-resizing** (1g, and the storm deferred from Phase 1) is
  `Model.overlaysSuppressed()`: while a resize rail is being dragged neither
  toasts nor flashes paint, nothing is recorded as painted (so nothing is read),
  and everything still lands in the centre and the header count. The app knows
  its own centre rail directly; surfaces report theirs through a new optional
  capability, **`plugin.ResizeDragReporter`** — implemented for the parity pair
  (`workspace`, `overview`) by reusing each surface's existing divider-drag
  predicate. Two-pane plugins with their own rails (`notes`, `conversations`,
  `git-status`) are a one-method follow-up each, the same shape the Phase 2
  `FocusCycler` limit has.
- Toasts still take no focus, still never steal it, and click-to-dismiss/`d`
  work per block. Tested in `internal/reveal/reveal_test.go`,
  `internal/notify/stack_test.go`, `internal/app/toast_stack_test.go`; verified
  in the real app through `scripts/tmux-drive.sh` (isolated tmux + state): six
  notifications posted from a second shell drew three blocks newest-on-top with
  `waiting ×3` and its `▾ 2 more · alt+e expand` peek line, the fourth source
  queued, and `alt+e` listed the hidden members.

### Phases 2–3 review pass — corrections

An independent review of the three Phase 2/3 commits found five real defects.
All are fixed; the notes above stand except where they say otherwise here.

- **The read gate defaulted to "painted" when there was no reveal state.** The
  sweep only skipped a block whose state said invisible, so a content region
  too narrow for a bordered block — or a block clipped off the bottom of a
  short one — was marked painted and read by its own expiry, which is exactly
  the failure the gate exists to prevent. `syncToastReveal` is now the single
  answer to "what is on screen": it stops admitting blocks when the content
  region's remaining height runs out, and the sweep requires a state that is
  present *and* visible. Expanded members are only marked once the block has
  finished arriving (`reveal.Shown`) — mid-reveal its lower rows are not on
  screen. The heartbeat reconciles the column *before* it sweeps, so a block
  that just took a freed slot is read on the frame it appears rather than a
  second later.
- **The cached block outlived its width.** Blocks are rendered at sync time and
  cached, but the content region also moves without a sync — a terminal resize,
  the centre opening or being dragged — and the renderer painted the stale
  block at the old width. It now redraws whenever the cache does not match the
  width it is being asked for.
- **Three interactions dropped the reveal tick.** `d`, a click, and `alt+e`
  changed the column and returned `nil`, so a retraction or an expansion sat
  frozen until the next heartbeat. The key/mouse handlers now return the sync's
  command, and the mutators no longer sync internally (one owner per frame).
- **The tab stop could steal a hard-coded `tab`.** The fallback for surfaces
  without `FocusCycler` only stood aside for a *registered* binding, and
  several surfaces switch panes on a `tab` that is not in the keymap (git's
  diff and commit-preview panes, file-browser sub-modes, the notes list,
  conversations' content search). With no ring on offer the centre now takes
  `tab` only from the shell's own context (`global`/empty). This widens the
  known limit recorded above rather than changing its shape: `alt+n`, `N` and
  the pointer are the routes everywhere else, and any surface becomes a stop by
  implementing `FocusCycler`.
- **`alt+e` collided with conversations' content search**, which expands all
  sessions on that key from a context the keymap did not describe. The binding
  is now registered (`expand-all` in `conversations-content-search`), so the
  global expand stands aside for it — registration is what makes a claim
  visible to the fallback rules.

**Known risk, not fixed (Phase 4 or later).** A sticky `waiting` notification
survives a restart, but the `LaneTracker` does not: after a restart the tracker
re-baselines and no longer knows the id it posted, so that one notification is
never self-dismissed when the agent unblocks. It ages out with the 24h
retention and the user can dismiss it. The fix is a deterministic per-workspace
id for the waiting record, which needs a rule for re-posting after a dismissal
and is not worth inventing here.

### Phases 2–3 proof run (2026-08-19)

A headless real-app run under `scripts/tmux-drive.sh` (isolated tmux socket
*and* isolated state tree; `paths` confirmed nothing resolved under
`~/.local/state/sidecar` or `~/.config/sidecar`), driving a 200×50 Sidecar with
notifications posted from a second shell through the `sidecar notify` CLI
against the same isolated `XDG_STATE_HOME`. All five checks passed; no defects
found, no code changed.

- **Tab as a focus stop.** In a ring surface (Workspaces) `tab` cycles the
  surface's own stops and then the centre; the centre's border turns from grey
  `#3d434a` to the active gold and `j`/`k` move its cursor row. `tab` from the
  centre returns to the content. In a ringless surface (td, Files, Git) `tab`
  stays with the plugin, exactly as the review pass decided — and the centre is
  still keyboard-reachable there because `alt+n` on an *open but unfocused*
  centre **refocuses** it rather than closing it. That is the property that
  keeps the ringless-surface limit a limit and not a trap; it should stay true.
- **Triggers.** A real lane transition needs a live agent in a tmux shell and is
  not drivable headlessly, so the contract was exercised at the seam instead
  (`notify.LaneTracker` directly, with a throwaway test deleted afterwards):
  first sight baselines silently; settled working→blocked posts
  `source=waiting sticky=true severity=warning "Shell 2 needs input"`; settled
  blocked→done posts `source=session "Shell 2 finished"` **and** withdraws the
  waiting id in the same `LaneEvents`. The adapter that carries this into the
  app is one call from the single `Plugin.Update` seam
  (`terminal_control.go:137` → `notifyAgentTransitions`).
- **Stacking.** Five posts from five sources drew three blocks; admission was
  FCFS by oldest and display newest-on-top, so the two newest queued. Dismissing
  a visible block let a queued one take the freed slot on the next sweep, twice
  in a row. A same-source burst collapsed to `◆ Burst agent ×2` with the
  `▾ 1 more ·  alt+e  expand` peek line, and `alt+e` listed the hidden member —
  the key the peek line prints is the key that works.
- **Reveal.** Frame-by-frame `capture-pane` sampling (~4ms/frame) caught the
  entry as six distinct frames: top border, title, rule, body, spacer, key row,
  bottom border. Relaunched with `SIDECAR_NO_ANIMATION=1`, 900 sampled frames
  contained no partial block — the toast appears whole.
- **No focus stealing.** With three toasts on screen, `j` still moved the td
  board cursor, and a synthetic SGR click on the *second* stacked block
  dismissed that block and no other.

### Live-use fix: focus exclusivity (2026-08-19)

Reported from a real session: on the global Workspaces browser with the centre
open, focusing the centre (`tab` or `alt+n`) lit the centre's border **while the
previously focused pane — a file pane, and the same for diff panes — kept its
own focused border**. Two panes read as focused at once.

Root cause is structural, not per-pane: a surface's focus chrome answers "which
of MY panes has the keyboard" and has no way to learn that something outside it
took the keyboard. The centre is the app's first focus stop that is not a pane,
so nothing told the surface underneath.

The fix is one signal at the lowest shared seam — **`internal/styles/focus.go`**,
next to the border rule every bordered pane in the app is painted by. The shell
sets `styles.SetFocusHeldOutsidePanes(m.notificationCentreOwnsKeys())` around the
content render in `internal/app/view.go` and clears it immediately after; while
it is set, `styles.RenderPanel` and `styles.RenderPanelWithGradient` draw the
normal border in place of the active, interactive or flash one.

- **Every surface inherits it**, not just the parity pair: the project workspace,
  the global browser, file browser, git status, notes, conversations, tasks. A
  surface added later inherits it without knowing the centre exists.
- `paneframe.EffectiveChrome` / `SetFocusHeldOutsidePanes` remain as the pane
  tree's reading of the same signal (delegating, not a second flag), so
  `WrapLeaf` still resolves a leaf to `ChromeIdle` and anything reasoning about
  drawn pane chrome reads the rule the renderer used.
- **No surface state is touched**, so when focus returns the surface's focused
  pane re-lights exactly where it was — asserted in the tests.
- **Attention never sets it.** Toasts and the pane flash leave focus chrome
  alone; an open-but-unfocused centre does too. The centre panel, toasts, the
  flash block and modals are all drawn *after* the signal is cleared, so their
  own chrome is never downgraded.
- Regression tests: `internal/styles/focus_test.go` (the rule),
  `internal/paneframe/focus_test.go` (the pane tree's reading),
  `internal/app/focus_exclusivity_test.go` (the shell sets it for exactly the
  span a surface renders in, and attention does not), and the surface twins
  `internal/overview/focus_exclusivity_test.go` +
  `internal/plugins/workspace/focus_exclusivity_test.go` (border chrome changes,
  content bytes do not, and it restores).


### Live-use fix: block identity, the reveal-driven column, and delivery (2026-08-19)

Three defects Marcus hit within a minute of real use. They are one chain: what
counts as a block, who decides a block is on screen, and whether a post reaches
the running app at all.

- **A block's identity is the source *and* the title, not the source.**
  `notify.StackKey` / `StackKeyFor`, and `Stack.Key` beside `Stack.Source`
  (which now means only "what the block looks like"). Phase 3 keyed collapse on
  the source, which read correctly against the six-source registry and wrongly
  against real use: nearly everything an agent or `sidecar notify post` sends is
  `agent`, so three unrelated notifications a second apart became one block and
  each arrival *replaced* the last. Design 1b's collapse exists to stop a source
  repeating *itself* — "a refusal the user is leaning on a key for" — which is a
  repeat of the same message. Repeats still dedupe to `×N` with the peek line;
  distinct messages stack, queue at four, and retract as separate blocks. The
  "there are six sources, so the queue only engages when more than three sources
  are live" note above is superseded.
- **The reveal machine is the only description of what is on screen.**
  `syncToastReveal` now owns an ordered `Model.toastColumn` and each block's
  `toastReveal.stack`; the renderer, the flash's height query, the read gate,
  `d`, the click target and the expand key all read it, and nothing reads the
  store's live stacks. Painting from the store produced both of Marcus's
  animation symptoms: a record that arrived between two syncs was drawn whole
  and then torn down to replay its entry ("all 3 appear, then disappear, then
  stack"), and a record that expired left the store before the machine began
  retracting, so the block blanked for a frame and was re-painted only to play
  its exit. Record-set changes now only ever feed the machine. A block keeps its
  place in the column while it retracts, and stops being a pointer target and a
  `d` target the moment it starts leaving — it has already been answered.
- **A post from inside the project reaches it.** `ownsNotifyRequest` matched the
  caller's working directory by exact equality against the instance's work dir
  and project root, so a post from any subdirectory — where an agent shell
  nearly always is — was disowned. With a single instance running this was
  hidden, because the CLI adopts the unique instance's origin; with two it was
  not, and the post silently took the JSONL fallback and reached the app a
  second later through the sweep. It is containment now (separator-guarded, so a
  sibling named `sidecar-notification-center` does not match `sidecar`).
- **The CLI's fallback message is honest.** Every failure read "no running
  Sidecar instance", which sent the user looking for a process on screen in
  front of them. `deliveryOutcome` distinguishes nothing announced, every
  instance declined, nobody answered in time, and the request could not be
  written, and says which.
- Regression tests: `internal/notify/stack_test.go` (repeats collapse, distinct
  messages from one source stack and queue at four),
  `internal/app/toast_lifecycle_test.go` (arrivals a second apart stack; a
  sweep-discovered record joins without rebuilding the block already on screen;
  a block is never painted ahead of its reveal; an expired block stays painted
  and retracts bottom-up without blinking out),
  `internal/app/notifications_test.go` (ownership from a subdirectory, and not
  from a sibling), `internal/cli/notify_test.go` (the fallback says why).
  Verified in the real app through `scripts/tmux-drive.sh` (isolated tmux +
  state, with a second announced instance so single-instance adoption could not
  mask it): three posts a second apart from a subdirectory shell all reported
  `Posted` and drew three blocks, newest on top.

### Live-use fix: review pass on the two fixes above (2026-08-19)

Two defects the reviewer found in the reveal-driven column, both in the seam
between "what changed the record set" and "what the machine is told":

- **A block leaves from where it stood.** Retracting blocks were appended to the
  end of the column rather than re-inserted at the index they held. That is
  invisible for an expiry — the oldest block is already the bottom one — and
  wrong for every other way a block leaves: dismissing the top block with `d`
  or a click made it jump to the bottom of the column and retract there while
  the blocks below slid up past it. `syncToastReveal` now splices each leaving
  key back in at its previous index.
- **Both arrival paths reach the column on the same frame.** The heartbeat
  synced the column *before* `sweepNotifications`, and the sweep is also where
  the store re-reads the log — so a record another process appended (the CLI's
  fallback, when no instance took the request) had no reveal state when the
  sync ran and waited a further second for one. The heartbeat's notification
  work is now one method, `Model.reconcileNotifications`: sync (so the read
  gate sees blocks that just took a freed slot), sweep, sync again (so
  swept-in records are painted on the heartbeat that discovered them). Reveal
  ticks are sequence-tagged, so the second sync's tick supersedes the first's.
- Regression tests: `TestADismissedBlockRetractsWhereItStood` and
  `TestSweptInRecordJoinsTheColumnOnTheSameHeartbeat` in
  `internal/app/toast_lifecycle_test.go`, both mutation-checked against the
  pre-fix code.

**Proof run** (`scripts/tmux-drive.sh`, isolated tmux + state, `paths` checked
first, session stopped afterwards, the real state tree never written):

- *Focus exclusivity.* On **Sessions** with the centre open, six sampled frames
  walking `tab` forward and `shift+tab` back: every frame had exactly one panel
  wearing the active border — list, preview, then the centre at column 162 —
  and the other two the normal one.
- *Delivery.* `sidecar notify post` from `internal/app` (a subdirectory) against
  the running isolated instance returned `"delivered":true` and printed
  `Posted`, not "appears at next start".
- *Stacking and motion.* Three posts a second apart, sampled with
  `capture-pane` at a 3.5ms median interval for 26s across the full countdown:
  2696 frames held all three blocks, newest on top, **zero** ordering
  violations; each title appeared in exactly one contiguous run of frames; and
  each block's height profile was strictly monotonic, `0→2→…→8` on entry and
  `8→7→…→2→0` on exit. No frame anywhere showed a block whole, then gone, then
  back — neither symptom Marcus reported can occur.

**Phase 4 — config page.** `Notifications` config section + configui page:
per-source `toast/centre/bell/expiry` table, behaviour block, quiet hours,
`t` test toast (1g). Bell column = terminal BEL. Everything suppressed still
lands in the centre and counts in the corner.

**Phase 5 — calls to action.** **Prerequisite:
`docs/plans/active/target-activation.md` must be done first** — it extracts
the twice-implemented jump machinery (overview `preview_links.go`, workspace
`terminal_links.go`) into one app-level activation service with
cross-project landing. This phase assumes that service exists and builds
only the notification side on top of it. Decisions settled with Marcus
(2026-08-19):

- **Activation happens from the centre only.** Toasts keep no focus context,
  steal nothing, and gain no activation affordance — click-to-dismiss and
  `d` stay their whole interaction. Frame 1e's toast key row is superseded.
- Scanning: `terminallink.ScanWith` over title/body (it is pure and returns
  kinded spans — reusable as-is). Add `KindSession` (+ detection pattern)
  and task ids to the scanner; both existing surfaces' kind switches and
  twin tests update with it. Stored `notify.Targets` and scanned spans
  reconcile into one target list — stored targets first, scan fills gaps.
- `sidecar notify post` grows `--target kind:value[:line][@project]`
  (repeatable) so agents can attach precise targets instead of relying on
  scanning; scanning remains the fallback for plain text.
- Centre UI per 1e: targets numbered 1–N, underlined; `enter` activates the
  selected/first target (re-show moves to a secondary key — update
  `keymap/bindings.go` and `notificationCentreCommands` per the Phase 1.5
  note); digit keys jump; cross-project targets render `repo/td-xxxxxx` and
  go through the activation service's pending-target landing.
- Cross-project targets are **not existence-verified at render time** (the
  resolvers are per-checkout; per-notification I/O at render is not worth
  it): they render activatable and fail gracefully on activation via the
  service's error path. Same-project targets keep the verified-underline
  invariant.
- Untrusted text: title/body from the CLI go through `StripOSC8` before
  `Decorate`, and URL activation refuses non-`SafeHTTPURL` values — the
  service's safety rule, applied at the centre like every other consumer.

Slices: **5a** same-project file + td-issue targets end-to-end in the centre
(numbering, enter, digits); **5b** cross-project landing + `--target` CLI;
**5c** session/task kinds + session attach jumps.

**Phase 6 — tasks both ways + td.** `tasks` source adapter posts
due/reminder notifications; `T` files a notification onto a task (REMINDERS
block, 1f) via the tasks CLI. Tasks own snooze semantics on their side —
sidecar never snoozes; a snoozed task simply re-posts later. `td` source for
assigned/reviewable. (No `git` source — see Architecture.)

**Phase 7 — OS integration (optional).** Emit OSC 9 / OSC 777 desktop
notifications for sources that opt in, so tmux/iTerm/Ghostty surface them as
real macOS notifications when sidecar is unfocused. Adapter-shaped, per
source, off by default.

### Phase 5a as built: same-project file and td-issue targets (2026-08-19)

The centre can now act on what a notification names. Scope was 5a only:
cross-project landing and `--target` are 5b, session/task kinds are 5c.

- **Reconciliation is one state-free function, `notify.CallsToAction`**
  (`internal/notify/cta.go`). Stored `notify.Targets` come first in the order
  they were attached; `terminallink.ScanWith` over title then body fills the
  gaps in reading order (`ScanWith` emits kind by kind, so the spans are sorted
  by column before numbering). A scanned span that says the same thing as a
  stored target is **not** a second entry — it becomes that target's location,
  which is what lets a stored target be underlined where it is written.
  Numbering is 1..N over the reconciled list. `notify.CTATitle`/`CTABody` are
  the exact strings the centre renders — `StripOSC8` applied once, whitespace
  collapsed once — so a span's columns are the columns on screen; the centre
  calls them too rather than repeating the normalisation.
- **`notify` now imports `uirequest` and `terminallink`** (no cycle: neither
  imports `notify`). A `CallToAction` carries a `uirequest.Target`, so the
  reconciled list is already in the activation vocabulary and the centre adds
  no mapping of its own. `notify.TargetTask` maps to nothing yet and is
  **dropped rather than numbered** — a digit that cannot jump is worse than no
  digit — which is 5c's first job to reverse.
- **Verification is memoized, not skipped.** `internal/app/notification_targets.go`
  scans with a `Resolve` that stats against the current checkout
  (`terminallink.ResolveFile`), so the plan's verified-underline invariant
  holds for same-project files; the answer is cached per notification id
  (`Model.notificationCTAs`, pruned in `refreshNotifications`, the one seam the
  cache is written at) so it is one stat per record rather than one per frame.
  No diff resolver and no resource matchers: both need a live snapshot the
  centre does not hold, and a git existence check per notification is not worth
  a lit-up commit id — commit targets therefore only appear when a poster
  attaches one.
- **Rendering — decided, and a deviation from "two lines".** Located targets
  are underlined in place in the title and body rows (`terminallink.Decorate`
  over spans shifted to where the row drew them and clipped to what survived
  truncation). The **numbered** list needs its own row, and it is drawn **only
  for the entry under the cursor on a focused panel** — the only entry `enter`
  and the digits can act on. So the list stays the two rows plan 1.5 asked for,
  the selection expands to a third `1 td-4c1f9a · 2 internal/app/model.go:42`,
  and a target that appears nowhere in the text (attached, or in another
  project) still has somewhere to be seen. Decoration passes every span as
  `KindFile` deliberately: in the centre a URL is underlined like everything
  else and activated through the service's `SafeHTTPURL` check, never turned
  into an OSC-8 link the terminal would open unchecked.
- **Keys.** `enter` = activate the first call to action, via
  `app.ActivateTargetIn` — one function, `activateNotificationTarget`, that the
  digits also use, so the double-click keeps following `enter` by construction.
  On an entry with **no** target `enter` falls back to the old detail re-show
  rather than doing nothing (most notifications name nothing activatable).
  Re-show moved to **`v`** (`show-details`), free in every context and the
  conventional "view". Digits **1-9** jump — but only when the selection has a
  target of that number; otherwise the digit stays the project tab it is
  everywhere else and still releases focus, so the panel never eats a
  navigation key to do nothing. `keymap/bindings.go` registers `v` and, of the
  digits, **only `1`** (`jump-target`): the panel answers the whole range
  itself, exactly as the shell does for its own globals, and nine rows in help
  would say nine things about one behaviour.
- **The `path:line` papercut is closed on this route.** A file target from the
  centre lands in the file browser at its line — `RelativeProjectPath` +
  `NavigateToFileMsg{Path, Line}`, which the file browser already honours. The
  terminal surfaces' version of the papercut (their own click path) is
  untouched and still open.
- **Toasts were not touched**, per the phase decision: no focus context, no
  activation affordance.
- Tested: `internal/notify/cta_test.go` (ordering, stored-first, location
  adoption, dedupe, the dropped task kind, StripOSC8 columns, cross-project
  `Display`) and `internal/app/notification_targets_test.go` (enter, digits,
  the unclaimed digit releasing focus, `v`, the file landing at its line, an
  unverified path being neither underlined nor numbered, the memo and its
  pruning). `TestCentreEntriesAreTwoLines` now also asserts the focused
  selection's third row and that blurring takes it away.
- Verified in the real app through `scripts/tmux-drive.sh` (isolated tmux and
  state in a private run dir, `paths` checked first, stopped after): a
  `sidecar notify post` into the isolated store rendered `Review td-4c1f9a now`
  with the id underlined, `Fix internal/app/model.go:42 first` with the path
  underlined, the selection's `1 td-4c1f9a · 2 internal/app/mo…` row, a footer
  reading `enter Open · 1 Target · v Details`, and pressing `2` opened
  `internal/app/model.go` in the Files preview scrolled to line 42.

### Phase 5b as built: cross-project landing and `--target` (2026-08-19)

Most of the cross-project *rendering* and *activation* was already in place
after 5a (`CallToAction.Project` → `Display()`'s `repo/td-xxxxxx`, and
`ActivateTargetIn` → the pending-target slot), so this slice is mainly the
posting surface and the tests that pin the behaviour down.

- **The spec grammar is a model rule, not a CLI rule.**
  `notify.ParseTargetSpec` / `ParseTargetSpecs` (`internal/notify/target_spec.go`)
  own `kind:value[:line][@project]`, so a future API or MCP poster accepts
  exactly the same strings. The two ambiguities in the grammar are resolved by
  kind: `:line` is read **for files only** (a commit sha, a session name and a
  URL can all end in digits), and the text after the last `@` is taken as a
  project qualifier only when it *looks* like one — an absolute path (or `~`),
  or a name containing no `/` and no `:`. That keeps
  `url:https://user@host/path` intact while `issue:td-99aabb@braid` and
  `file:cmd/main.go:12@/Users/x/code/braid` both qualify.
- **Validation is loud, and happens before anything is posted.** An unknown
  kind, an empty value, a URL that is not `SafeHTTPURL`, or an id that is not
  `terminallink.IssueID` is exit code 2 with the valid kinds named; one bad
  spec fails the whole post rather than filing a notification that quietly does
  less than it says. Exact duplicates within one post are dropped (a typo, not
  two calls to action).
- **`--target` is repeatable and order-preserving**, which is the numbering:
  stored targets come first in attach order (5a's reconciliation), the scan
  fills the gaps. Documented in the command's `Long` text and flags, so it
  lands in `sidecar --agents` and `docs/reference/cli.md` (regenerated).
- **Nothing new was needed on the bus.** The `notify` request payload is the
  marshalled `Notification`, so `Targets` already travel over the file-RPC route
  and through the JSONL fallback; the round trip is now covered by a test rather
  than left to inspection.
- **Cross-project targets stay unverified at render**, per the phase decision:
  a stored target with a `Project` never adopts a local span as its location
  (a span is written in this checkout's terms), so it is numbered, prefixed
  with the project, and fails through the activation service's error path if
  the project is gone.
- Tested: `internal/notify/target_spec_test.go` (grammar, the file-only line,
  project qualifiers vs URL userinfo, refusals, order/dedupe, the
  cross-project `Display` prefix), `internal/cli/notify_test.go`
  (`--target` end-to-end into the log and back through `CallsToAction`, and the
  refusal storing nothing), and `internal/app/notification_targets_test.go`
  (`enter` on a foreign target emits `ActivateTargetMsg` with the project).
- Verified in the real app through an isolated `scripts/tmux-drive.sh` run
  (`paths` checked, private run dir, `stop` confirmed): a post carrying
  `--target issue:td-99aabb@braid --target file:internal/app/model.go:42`
  rendered the selection row `1 braid/td-99aabb · 2 internal/…`, and pressing
  `1` refused out loud with "Cannot jump to braid: that project is no longer
  available" rather than doing nothing.
- **Left for 5c:** `notify.TargetTask` still maps to no activation and is
  dropped from the numbered list, so `--target task:...` parses and stores but
  does not yet number — the kind switch in `notify.targetFromStored`, the
  scanner's `KindSession`/task ids, and both terminal surfaces' dispatch are
  5c's five-test recipe.
