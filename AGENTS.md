<!-- td-agent-instructions:start -->
<!-- td-agent-instructions:version=3 -->

## Working with td

td keeps task context durable across sessions. In a new context, run `td usage --new-session -q` to see current work.

Use your judgment about how much tracking a task needs. For substantive work: `td start <id>`, record progress with `td log`, hand off with `td handoff <id>`, then `td review <id>`.

Closing needs a review. Say who did it (default trusted mode; delegated/strict allow only the first):

- independent session: `td approve <id> --reason "..."`
- a sub-agent: `td approve <id> --reviewed-by "<who>"`
- you: `td approve <id> --self-review --reason "..."`

Prefer a reviewer with its own `TD_CONTEXT_ID`; never name one who did not review.

Run `td usage` or `td <command> --help`.

<!-- td-agent-instructions:end -->

## Markdown plans

When writing plans or other Markdown documents, do not insert hard line breaks
to keep a column width — let paragraphs flow as single long lines. Hard-wrapped
text renders poorly when the document is viewed at any width other than the one
it was written for.

## Commit completed work by default

Unless the user explicitly asks not to commit, create a focused commit once the
requested work is complete, verified, and reviewed. Stage only files that belong
to the task, preserve unrelated dirty or staged changes, and do not push unless
the user asks.

## Working inside a Sidecar shell

Run `sidecar agents` for what you can do from here, and use it. `sidecar --agents` and `sidecar -a` are equivalent aliases. The two that earn their keep every session: keep the shell's name describing your current task, and put a file or issue in front of the user rather than describing its path (`sidecar open` works from any context, not only a Sidecar shell). Never edit `shells.json` or rename tmux sessions directly.

### The pane layout is readable and writable

Three verbs over one model, all acting on the surface showing this shell (or, with `--sessions`, the global Sessions surface), and none of them ever queues — a request whose destination is off screen declines with the reason instead of arriving late.

- `sidecar layout get --json` reads the grid: every pane's cell, kind, targets, session, geometry, and the caps and floors a write would be held to. Read before you write.
- `sidecar layout apply` composes panes onto it: `--pane` adds without closing anything, `--spec` replaces the whole layout. All-or-nothing, with a per-pane verdict for each requested pane.
- `sidecar layout move` repositions one pane that is already open: `sidecar layout move 2.1 --to 1.2`, `--to 3` to append to a column, or `--focused --to left|right|up|down` for the same direction rule the reposition modal's `h/j/k/l` use. Use this rather than rebuilding a whole `--spec` to move one pane. A move with nothing to do reports `unchanged` and exits 0; a refusal exits 4 with the reason.

From a Sidecar-managed pane whose geometry lease is held by a connected viewer, `sidecar open` and `sidecar layout` are that viewer's screen — not a TUI that may not be running on the host. There is no `sidecar open --host`. If the matching row is not on that viewer's screen, or the lease holder cannot receive pane requests, the command declines (exit 4) rather than queueing.

Full reference: `docs/reference/cli.md`.

### Shells lost to a tmux restart are recoverable, not gone

If a tmux server has died, managed shell records survive it — a server death marks them as restore candidates rather than deleting them. `sidecar session status` prints the ordered plan (read-only, no TUI needed), `sidecar session restore [--dry-run]` performs it, and `sidecar session policy TARGET --shell|--resume|--never|--inherit` decides per shell how far a restore should go. Resuming an agent conversation is a separate, confirmed decision: under the default `ask` policy a non-interactive resume needs `--yes`, and a refusal says so rather than quietly restoring shells only.

## Coordinating another agent

Sidecar can start and drive a second agent in a shell it owns, behind the default-off `agent_control` feature flag. See `.agents/skills/coordinate-agents/SKILL.md` (or `.claude/skills/coordinate-agents/SKILL.md`) for the full contract; the sequence it exists to protect is:

discover (`sidecar agent list --json`) → create the layout separately (`sidecar create shell`, never `agent start`) → start the provider (`sidecar agent start TARGET --kind KIND`, which returns only at ready) → `sidecar agent prompt TARGET TEXT --wait --timeout ...` → **read before you send keys** (`sidecar agent read TARGET --source recent-unwrapped`) → answer with `sidecar agent send-keys TARGET ...`.

Preserve the user's focus, and never close a target you did not create. There is no implicit timeout on any wait. A blocked agent is a question for you to read and answer deliberately — Sidecar never auto-answers an approval. Raw terminal work still belongs to tmux, not to `agent send-keys`.

Every verb above also takes `--host ID` to run on a registered remote host (`sidecar host list`), as one invocation over the ssh connection Sidecar already holds open. The sequence and the refusals are identical, because the verb runs on the machine that owns the pane and you get that machine's own answer back. Two differences are worth knowing: a remote verb needs an explicit TARGET, since the omitted-target rule names the shell *you* are in, and remote output tells you whether a shell is bound to a conversation but not which one unless you pass `--include-session-ref`. `sidecar session status --host ID` and `session restore --host ID` work the same way — the host plans and executes, you request and observe.

## Demoing Features

When you finish building or modifying a user-facing feature, make it easy for
the user to try it in an isolated demo environment using `./scripts/demo.sh`.
Demo runs are 100% ephemeral: they automatically compile a fresh binary from the
current working tree, use private tmux sockets and temp state trees, and clean up
on exit.

See `docs/guides/active/demo-environments.md`.

You can present the demo in two ways:

1. **Provide the command to the user**:
   ```bash
   ./scripts/demo.sh                  # Multi-project demo (5 themed sample projects)
   ./scripts/demo.sh single -p <name> # Single project demo (intersections, plastic-pieces, etc.)
   ./scripts/demo.sh fresh            # Clean first-run onboarding (use --no-td/--no-tasks to mask deps)
   ```

2. **Launch a demo shell / split for the user (Sidecar Inception)**:
   When working inside a Sidecar shell, you can launch the demo directly into
   the user's running session using the shell creation CLI (per
   `docs/plans/implemented/agent-shell-create-cli.md` and `docs/plans/implemented/terminal-splits-and-windowing.md`):
   ```bash
   # Create a dedicated workspace demo shell:
   sidecar create shell --name "Demo: <Feature>" --run "./scripts/demo.sh"

   # Or split the agent's current shell to show the demo side-by-side:
   sidecar create shell --split right --run "./scripts/demo.sh"
   ```


## Build & Versioning

```bash
# Build
go build ./...

# Run tests
go test ./...

# Managed install from the canonical main checkout
make install-local

# Deliberately activate the current branch/worktree
make install-worktree

# Managed installs link Homebrew and retarget any other `sidecar` that
# wins PATH (typically ~/go/bin from unmanaged `make install`) so
# `make install-worktree && sidecar` runs this build.
#
# Managed dev installs build through your go.work: sibling modules listed
# there (td, tasks) are compiled in from source, so an installed sidecar
# tracks your newest local td without a release dance. Each activation
# records which sibling revisions were compiled in — `make install-status`
# prints them. Build against the go.mod pins instead (what releases ship)
# with SIDECAR_INSTALL_PINNED=1 make install-local.

# First command in any git worktree: shadow the main checkout's go.work
# (without this, every go command in a worktree fails with "directory ...
# does not contain modules listed in go.work"; idempotent, safe everywhere).
# Worktrees created through Sidecar run this automatically via .worktree-setup.sh.
make worktree-init

# Inspect the managed link and actual shell resolution
make install-status

# Restore the installed Homebrew release
make use-homebrew

# Cut a release: write bullets under `## [Unreleased]` in CHANGELOG.md, then run
BUMP=minor make release
```

See `docs/guides/active/releasing.md` and `.agents/skills/release-sidecar/SKILL.md`.
Version is set via ldflags at build time. Without it, sidecar shows git revision info.
`make install-dev` is a compatibility alias for `make install-local`. Plain
`make install` is an unmanaged `go install` into `GOBIN`; it does not alter
Homebrew links or guarantee which `sidecar` wins PATH precedence.

## Tmux compatibility

Sidecar supports tmux 3.4 and newer and continuously tests the manifest roles `minimum` and `latest`. Versions, roles, and official archive checksums live only in `compat/tmux-versions.tsv`; local helpers and the GitHub Actions matrix consume that file. Run `./scripts/test-tmux-compat-manifest.sh`, build a role into an absolute temporary prefix with `./scripts/build-tmux-compat.sh ROLE PREFIX`, and prove it with `./scripts/test-tmux-compatibility.sh ROLE PREFIX/bin/tmux`. The full contract, latest-client/oldest-server skew command, build prerequisites, and future stable upgrade procedure are in `docs/guides/active/tmux-compatibility.md`.

Prefer capability probes over tmux version branches. Every compatibility proof must use private sockets, and changing or upgrading a client never authorizes restarting the default server.

## Keyboard Shortcut Parity

See .agents/skills/ui-features/SKILL.md

## Project and global workspace parity

The project workspace (`internal/plugins/workspace`) and the global Workspaces
browser shown as **Sessions** in the navbar (`internal/overview`) are two
projections of one model, not two surfaces that resemble each other. A UI
change that lands in one and not the other is a bug.

This is enforced by shared code, not by memory:

- `internal/panelayout` — pane-tree structure and geometry.
- `internal/paneframe` — presentation: chrome geometry, leaf border states, the
  drag handle and its widened hit box, the compositor, chrome-aware floors, and
  the order hit regions are registered in.
- `internal/livepanes` — live refresh: the watcher lifecycle, watching only the
  panes that are on screen, and re-reading a pane when it comes back into view.
  `internal/livewatch` underneath it owns the filesystem signal and the
  no-change gate.

Each surface binds to the frame in exactly one file — `pane_host.go` — which
answers only what is in its own leaves. When adding anything to do with panes,
splits, handles, borders, focus chrome, or pane hit regions, put it in
`paneframe` and let both surfaces inherit it. Do not add a second compositor,
border rule, or divider renderer. See `.agents/skills/drag-pane/SKILL.md`.

Live refresh binds in exactly one file per surface too —
`internal/plugins/workspace/live_panes.go` and
`internal/overview/live_preview.go` — and a new content-pane kind is one
`livepanes.Binding` entry in each. A pane kind that reads something it does not
own and has no binding is a pane that quietly stops being true while an agent
works; that is what the `Resource` leaf is today, deliberately, because its
content is not on the filesystem.

See td-331dbf19 for diff paging implementation.

## Terminal Background Fidelity

Every cell inside an embedded pane takes its colour from tmux and nothing else.
Captures use `capture-pane -e -N` because the trimmed form cannot tell a blank
row in a carried colour from a blank row in the default, and `internal/termpreview.DrawRows`
is the only place a background is decided. Do not add a second decider, and do
not infer a colour from content: a heuristic here fails at one width and passes
at the next, which is how the pane used to flicker and flood.

See `docs/reference/terminal-background-fidelity.md` for the capture semantics,
the failure modes, and how to check a live pane with
`./scripts/terminal-fidelity.sh`.

## Startup Latency

Everything a plugin does in `Init()` — and everything `Start()` does before
returning its `tea.Cmd` — runs before the first frame is painted. Keep that path
free of filesystem walks, database opens, and subprocess spawns; do that work in
a `tea.Cmd` and render a loading state until the result message arrives. This
matters most on machines running an endpoint security agent (e.g. CrowdStrike),
where every file open and process spawn carries a large fixed tax.

To measure:

```bash
SIDECAR_STARTUP_TRACE=stderr sidecar 2> trace.out   # or =1 to log instead
SIDECAR_STARTUP_TRACE_DELAY=10s                     # dump later to catch async work
SIDECAR_DIAG_PATHS=1 sidecar                        # print the state/config/tmux paths resolved
```

The trace lists each phase with its offset from process start and its duration,
ending with the `first ready frame` marker. See `internal/startuptrace`.

To count subprocess spawns, put logging shims for `git`/`tmux`/`td` ahead of the
real binaries on `PATH` — duplicated git invocations are the usual finding.

## Verifying Changes in the Real App

`scripts/tmux-drive.sh` runs sidecar in a headless tmux session, sends it
keystrokes, and captures the screen as text and PNG, so a UI change can be
verified without a human at the keyboard:

```bash
./scripts/tmux-drive.sh paths                       # confirm the run is isolated first
SIDECAR_BIN=$HOME/go/bin/sidecar ./scripts/tmux-drive.sh start 200 50
./scripts/tmux-drive.sh keys 5 && ./scripts/tmux-drive.sh snap workspaces
./scripts/tmux-drive.sh stop
```

Always run `./scripts/tmux-drive.sh stop` when done or on error to avoid leaking background polling instances.

**A proof run must isolate BOTH the tmux server and the Sidecar state tree.**
They are independent axes, and isolating only one is how td-8d18de destroyed six
of a live user's shells: a private tmux socket did nothing to stop the run from
rewriting the real `~/.local/state/sidecar/projects/sidecar/shells.json` that the
developer's running Sidecar was watching. `tmux-drive.sh` now does both by
default — run `./scripts/tmux-drive.sh paths` before you trust it and confirm
that nothing resolves under `~/.local/state/sidecar` or `~/.config/sidecar`.
Never launch sidecar for a proof by hand without `XDG_STATE_HOME`, `TMUX_TMPDIR`,
`-config <temp path>` and `SIDECAR_ISOLATED_STATE=1` (that last one makes the
binary refuse to start rather than touch the real tree). Note that
`XDG_CONFIG_HOME` moves nothing — config and `state.json` are `$HOME`-based, so
`-config` is the only lever for them.

**`$TMUX` overrides `TMUX_TMPDIR`.** Inside a Sidecar shell the environment carries
`TMUX`, and tmux resolves its socket from that variable before it looks at
`TMUX_TMPDIR`. A proof that only sets `TMUX_TMPDIR` is therefore talking to the
default server, and `TMUX_TMPDIR=$SOCK tmux kill-server` kills every live shell
(this happened on 2026-09-04; see
`docs/plans/active/session-restore-resume-conversations.md`). Every tmux command
in a proof runs after `unset TMUX TMUX_PANE`, or passes `-S` with the private
socket path. `tmux-drive.sh` does this for you; a hand-rolled proof must do it
itself, and must do it on the cleanup line too, not only on the setup line.

Note that the embedded terminal's cursor is a **native** cursor drawn by the host
terminal, so `capture-pane` cannot see it; checking cursor placement needs an
attached viewer client. See `docs/guides/active/headless-testing.md` for that, for the
key-pacing rules, and for the tmux coordinate spaces the terminal code works in.

## Plugin View Rendering

**Critical: Always constrain plugin output height.** The app's header/footer are always visible - plugins must not exceed their allocated height or the header will scroll off-screen.

In `View(width, height int)`:

1. Store dimensions: `p.width, p.height = width, height`
2. Calculate internal layout respecting `height` (e.g., `contentHeight := height - headerLines - footerLines`)
3. Either use `lipgloss.Height(height).Render(content)` to enforce height, or manually limit rendered lines
4. Never rely on the app to truncate - it wraps with Height() but edge cases cause rendering bugs

This bug manifests as "top bar disappears" after state transitions (commits, refreshes, mode switches).

## Footer Hints

**Do NOT render footers in plugin View().** The app renders a unified footer bar using `plugin.Commands()` and keymap bindings. Plugins should:

1. Define commands with short names in `Commands()` method
2. Never render their own footer/hint line - this creates duplicate footers

Keep command names short (1 word preferred) to prevent footer wrapping:

- "Stage" not "Stage file"
- "Diff" not "Show diff"
- "History" not "Show history"

The footer auto-truncates hints that exceed available width.

## Inter-Plugin Communication

Plugins communicate via tea.Msg broadcast - all plugins receive all messages.

**App-level messages** (`internal/app/commands.go`):

- `FocusPluginByIDMsg{PluginID}` - switch focus to a plugin by ID
- `app.FocusPlugin(id)` - helper to create the above

**File browser messages** (`internal/plugins/filebrowser/plugin.go`):

- `NavigateToFileMsg{Path}` - navigate to and preview a file (relative path)

**Usage pattern** (e.g., git → file browser):

```go
func (p *Plugin) openInFileBrowser(path string) tea.Cmd {
    return tea.Batch(
        app.FocusPlugin("file-browser"),
        func() tea.Msg { return filebrowser.NavigateToFileMsg{Path: path} },
    )
}
```

Workspace tmux preview capture cap is configurable via `plugins.workspace.tmuxCaptureMaxBytes` in `~/.config/sidecar/config.json`.
