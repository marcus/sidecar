# Changelog

All notable changes to sidecar are documented here.

## [Unreleased]

### Features

- **The plugin browser answers the pointer everywhere.** Every protocol plugin's tab and pane now has the interactions the rest of Sidecar has: clicking a pane focuses it, clicking a row selects it and a second click opens it, the query row and the View control are click targets, the wheel scrolls the box under the pointer, both the table and the detail have draggable scrollbars, the gap between them is a drag rail whose split is remembered per plugin, and `+`/`-` resize it from the keyboard. Tab and Shift+Tab reach the browser through an opt-in focus-ring capability, so Files, Git and Notes keep their own Tab behaviour. The "no action here" flash is gone: an action that does not apply is absent from the hints and inert on its key. The rules are in the design language under Pointer parity. (td-62b81c)

- **Plugins declare filters, and a page that could not answer everything says why.** A collection can declare up to eight choosers or text filters; the host draws them in the View modal, folds the applied scope into the sort pill, persists them with the tab, and sends `list` only the filters that differ from their defaults. A page may report what it omitted, a `failed` outcome, and per-source coverage rows; clicking the outcome word or a notice, or pressing `c`, opens a coverage modal with a Source, State, Elapsed and Reason table and a Retry button. `outcome` describes only the page's own rows. Recall is global first with a profile chooser over every configured profile, and no longer narrows to the Sidecar project on its own, which is what had made every documents source answer empty. `--filter ID=VALUE` reaches the same filters from `sidecar plugin check`, `plugin call`, `sidecar open --plugin` and the layout spec. (td-9ca6a7, td-786e42)

- **Query bars are one field with real editing keys.** Every `/` search bar in Sidecar — the plugin browser, both workspace sidebars, doc search, the file browser's tree and content search, git history search and path filter, both notes searches, Sessions search and both terminal searches — is the same field, backed by the text input the modals use, so option-arrow, option-delete, home, end, cursor movement and paste work in all of them, a clickable × clears the query wherever the row has a hit map, Esc clears then blurs, and the arrow keys hand the keyboard to the list beneath. In the plugin browser, down from the field moves to row one, `k` on row one returns to the field with a key-repeat guard, and the detail follows the cursor after a short quiet period with a superseded load killed rather than left running — the rule the Files plugin already followed, now named in the design language. (td-e8cceb, td-45898b)

- **One selector control, and selectable text in every detail pane.** The Create Workspace kind chooser is now `modal.Select` in the modal library: segmented under five choices while they fit, a bordered `❯`-cursor list at five or more, scrolling past a visible cap, disabled rows kept visible with their reason, and a border that follows focus like an input's; every View modal uses it, and a modal sizes to its widest control. The plugin browser's detail, td issue cards, resource cards and diff panes are selectable with the pointer — drag, double-click a word, triple-click a line — and `alt+c` or `super+c` copies to both the system clipboard and the terminal over OSC 52, through the same engine file panes already used; the help modal names the shift or option bypass for the terminal's own selection. (td-21aafa, td-c6904c, td-2b8f79)

- **A plugin authoring guide and a Plugins page.** `docs/guides/active/creating-plugins.md` takes an author from a CLI to a plugin that passes `sidecar plugin check`, with a runnable Python example under `docs/guides/examples/hello-plugin/` that a test keeps honest; `docs/reference/plugin-protocol.md` is the single authority for the frozen contract; and the documentation site's Plugins page replaces the Terminal Resources page, which described a configuration shape that never existed, with the `sidecar plugin` verbs added to the site's CLI reference. (td-40eb97, td-ade3a3)

- **Configuration -> Agents -> Integrations is a table.** One line per agent Sidecar can install for, with its name, its status, and Install, Update, Repair and Remove always in the same column, painted dimmed on the rows where the service would refuse them. Moving the cursor changes only which row is highlighted, and every action that is on offer can be clicked on any row without selecting that row first. Wide terminals also get the authority tier and the file the integration lives in; narrow ones keep the actions and drop the rest. Underneath the table, a fixed-height detail box follows the cursor with that agent's files, tier and demotion reason, CLI path and version, last lifecycle report, and diagnostic. Agents Sidecar has surveyed and ships nothing for collapse into one line instead of taking a row each. The count of an agent's known gaps is gone: they are gaps in that agent's own hook contract rather than faults in Sidecar, and the page names `sidecar agent integration status <agent>`, which lists them. (td-20e857)

- **External plugins are hosted without turning a flag on first.** `plugin_protocol` and `terminal_resource_providers` both default on. An install with nothing configured still starts nothing — no describe pass is even scheduled when no plugin or provider is configured for the section a flag governs — and turning a flag off still stops every one of that section's child processes while leaving the configuration in place. `terminalResources.providers` is now a read-only alias of the plugin configuration: it is still read and still dispatched on the frozen `sidecar.terminal-resource/v1` identifier, and Sidecar no longer writes the section, so it stays exactly as you wrote it. `plugins.<id>.enabled` is the documented switch for the embedded panels, with the `tasks_plugin` and `notes_plugin` feature flags as read-only aliases for one more minor release: each answers only while its config key is absent, and the Feature Flags page now reports the panel's own answer rather than the flag, so it can no longer disagree with the Panels page. (td-944274)

- **An empty plugin detail box shows the plugin's next collection.** In a Tab placement, where the box stands beside the list, a plugin with a second collection gets that collection drawn there — its title, what it currently says, and its first rows — instead of a card of help text. A plugin's next collection is usually the ledger that explains the list, so "no matches" and "why" are on screen together and an `abstained` page can be checked where it is read. It is listed once, without a query, and never when its search is required; a plugin with one collection keeps the help line, and a pane, which shows one shape at a time, is unchanged. (td-6c49c5)

- **A narrow plugin list keeps its rank beside the name.** Below the table floor a row still reflows onto two lines, but the columns declared before the primary — a rank, an index — now stay with the primary on line one, and the remaining short columns, the status label and the secondary text fold into line two indented under the name. A list of ranked results reads down its names with the numbers still attached, which is what the M0 mockup drew. (td-6c49c5)

- **`status.label` has a stated render bound.** The reserved status column never grows past 24 characters, and the plugin protocol reference now says so in its Limits table as the protocol's own bound, beside the frozen 64-character wire bound it does not replace. A plugin author can pick a label that reads rather than discovering the truncation in a pane. (td-6c49c5)

- **A plugin row now expands under the scope it was found in.** `get` carries `params.filters` — the applied filter set of the list that produced the row, sent exactly as that `list` sent it, narrowed against the collection's declaration at the same process boundary. Enter on a row hands the scope to the tab that opens, a restored row tab carries the scope it recorded, `sidecar open --plugin ID --collection C ROW --filter id=value` and a layout spec row carry theirs, and `sidecar plugin check --get` and `plugin call get` take `--filter` too. The host's get cache is keyed by the applied set, so the same row under two scopes is two questions rather than one cached answer. Without this a row found under a raised-sensitivity profile expanded under the plugin's default profile, which could be a different document or a refusal. (td-6c49c5)

- **The plugin protocol is frozen as `sidecar.plugin/v1`.** Sidecar sends that identifier and validates the answer against it strictly: there is no alias, and a plugin still answering the pre-freeze `sidecar.plugin/v1-draft` is a protocol failure with a named reason rather than a silent downgrade. Tolerance belongs on the plugin side — a plugin that accepts either identifier on a request and answers with whichever it was asked keeps working with a Sidecar released before the freeze and with every one after it. The canonical request and response JSON under `internal/pluginhost/testdata/protocol/`, the reference fixture, the reference, the authoring guide, the runnable example, and the generated CLI reference all name the frozen identifier. (td-6c49c5)

- **Every agent Sidecar can recognise is now an agent it can start, and you can add your own without waiting for a release.** Twelve families gained a launch command: Cline, Devin, Droid, Hermes, Kilo, Kimi, Kiro, Maki, Qoder, Qwen, plus OMP and Mastra Code, which have their first Sidecar identity here. Each one's command, auto-approve flag and resume arguments were read from the provider's own documentation or `--help`, and where a provider has no auto-approve flag the catalog says so instead of guessing one. The catalog itself moved from Go code to one TOML file per family, embedded in the binary, so a family is a file rather than a rebuild-shaped change.

- **You can override an agent or add one of your own.** Drop a `.toml` file into `agents/` beside your config file (`~/.config/sidecar/agents/` by default) and it joins the catalog at startup. A file named after a family Sidecar ships overrides only the fields it states, so `command = "claude-next"` is a complete file and Claude keeps everything else; a file with a new name adds a whole family, launchable and resumable, at the end of the creation picker. A malformed file is reported and skipped rather than stopping Sidecar. See `docs/reference/agent-catalog.md`.

- **The creation pickers offer the agents you actually have.** Create Workspace, Create Shell and the Sessions create now list a family when its command is on your `PATH`, or when you have named it in `plugins.workspace.agents`. Naming one still offers it whether or not it is installed. Configuration → Agents keeps listing every agent Sidecar knows, marking the ones that are not installed, so nothing is hidden from you, and the CLI still launches any family by name, installed or not. The `PATH` lookup happens once per process, off every render path.

- **Kilo Code reports its own lifecycle to Sidecar.** `sidecar agent integration install kilo` writes one Sidecar-owned plugin into Kilo's config directory (`$XDG_CONFIG_HOME/kilo/plugin`, or `$KILO_CONFIG_DIR/plugin`), and from then on a Kilo pane's working, blocked, unblocked, idle and session-identity transitions come from Kilo itself rather than from reading its screen. It installs at the advisory tier on real traces of kilo 7.5.9, which is the ceiling this integration can reach: a user interrupt and a provider failure reach Kilo's bus as the same event with a different name, so a cancelled turn is not distinguishable from a failed one, and nothing releases the lane on exit. Kilo loads both `plugin/` and `plugins/`, so a copy in each would report every event twice; Sidecar owns one of them and reports anything with its asset's name in the other as needing repair. Install, inspect, repair and uninstall go through the same adapter contract as the other integrations, so `--dry-run` shows the exact operations and uninstall removes only what Sidecar owns. It reports lifecycle facts only: never prompts, responses, tool data, paths or credentials.
- **Kimi Code CLI reports its own lifecycle to Sidecar.** `sidecar agent integration install kimi` adds twelve hook entries to Kimi's own `config.toml` (`$KIMI_CODE_HOME/config.toml`, or `~/.kimi-code/config.toml`), and from then on a Kimi pane's working, blocked and idle transitions come from Kimi itself rather than from reading its screen. It installs at the advisory tier on real traces of kimi-code 0.40.1, covering work start, tool use, blocking, unblocking, turn completion and cancellation. Blocking is genuinely first-class here: one `PermissionResult` event fires whether you approve or deny, so a Kimi pane cannot get stuck showing blocked the way an agent with no denial event can. Two things are deliberately not claimed — process exit, because Sidecar already owns process liveness, and session binding, which needs Kimi to become a launchable agent family first. Sidecar owns only the block between its own two marker comments: it refuses to touch a hook of yours placed inside that block, refuses to leave a stray copy of its own outside it, and an uninstall leaves the rest of your `config.toml` byte-identical. It reports lifecycle facts only: never prompts, responses, tool data, paths or credentials. (td-b0e19a)

### Bug Fixes

- **The Integrations table's rows give way before its detail box does.** The Configuration detail pane truncates at its bottom edge, so a page taller than the pane lost whatever was last -- which on this route was the detail box the cursor drives, the outcome of the last install, and the closing note. The rows now window instead: the column header above them and everything below them stays pinned, the rows scroll to keep the cursor's own row on screen, and a row the window hid takes its action pills' click targets with it. The window is only taken where it makes the whole page fit, so a pane too short for either truncates exactly as it did before. (td-20e857)

- **A truncated path in Configuration keeps its filename.** `clampEnd` passed the target width to `ansi.TruncateLeft`, whose count is how many columns to remove rather than how many to keep, so a 43-column path in the Projects page's path column came back as ten columns rather than the thirty-four it had room for. A column exactly one wide came back empty for the same reason, because the ellipsis was TruncateLeft's own prefix and it writes that only when the string is longer than the count. (td-20e857)

- **Saving no longer drops a plugin section Sidecar does not manage.** A key under `plugins` that this build does not know — one written by a newer Sidecar, or by hand — used to be discarded the next time anything saved the configuration. It is now merged, the way an unknown top-level key already was. The first save after upgrading sorts the subkeys under `plugins` once, the way the top level of the file has always been sorted, so expect one cosmetic diff there and no lost settings. An external plugin id that one of Sidecar's own surfaces already answers to (`tasks`, `notes`, `td-monitor`, `git-status`, `file-browser`, `conversations`, `workspace-manager`, `sessions`, `activity`) is refused by `sidecar plugin add` and by configuration validation, naming the surface, instead of painting two tabs with one identity. (td-944274)

- **An integration for an agent Sidecar can recognise but not start can now bind the conversation in its pane.** `sidecar agent report-session --kind KIND` resolved the kind through the launchable agent families only, so a hook for a detection-only family was refused as an agent kind Sidecar does not know, even when Sidecar itself had installed the integration sending it. The lookup now also consults the families Sidecar recognises in a pane, which is the right question for a verb that reports about a pane rather than about a launch.

- **Antigravity panes bind to their own conversation.** `sidecar agent integration install antigravity` writes one `PreInvocation` entry into `~/.gemini/config/hooks.json` under a `sidecar` hook block, so a managed shell running `agy` records exactly which Antigravity conversation is in it and a cold restore can offer that one back. Antigravity has no session-start event, so the binding lands on the session's first model call rather than at startup. Lifecycle state still comes from screen detection, which is what the `session-identity` tier means. Uninstall removes only Sidecar's own entry, wherever in the file it ended up, and leaves every other named hook block untouched. (td-73c4ff)

- **GitHub Copilot CLI panes bind to their own session.** `sidecar agent integration install copilot` writes one `SessionStart` entry into `~/.copilot/settings.json` (or `$COPILOT_HOME/settings.json`), so a managed shell running `copilot` records which session is in it. Copilot is not installed on any machine Sidecar has surveyed, so this port is built entirely from Herdr's own installer and is untraced; `sidecar agent integration status copilot` says so in its known gaps rather than implying more confidence than there is. (td-73c4ff)

- **Cursor Agent panes bind to their own session.** `sidecar agent integration install cursor` writes one `sessionStart` entry into `~/.cursor/hooks.json`, so a managed shell running `cursor-agent` records which session is in it. The entry goes to `~/.cursor` and not to `CURSOR_CONFIG_DIR`, because cursor-agent's hook loader reads the former and never consults the latter. A `version` header is written only into a hooks.json Sidecar creates, so uninstall gives a file you already had back exactly as it was. (td-73c4ff)

- **grok panes bind to their own session.** `sidecar agent integration install grok` writes one `SessionStart` entry into `~/.grok/hooks/sidecar.json` (or under `$GROK_HOME`), a directory grok merges every `.json` from, so a managed shell running `grok` records which session is in it. grok also loads hooks from `~/.claude/settings.json` and `~/.cursor/hooks.json`, so all three Sidecar entries fire inside one grok session and exactly one of them binds: a report whose claimed agent is not the one in the pane is refused rather than recorded. Uninstall removes only Sidecar's entry, so a hook you added to that file yourself is kept. (td-73c4ff)

- **A relocated Claude Code is found by the integration installer.** `sidecar agent integration status|install|uninstall claude` now resolves Claude's configuration home the way Claude itself does, from `CLAUDE_CONFIG_DIR` when that names an absolute directory and `~/.claude` otherwise. Before this, a user who moved that directory saw `not-installed` for a Claude full of hooks, an install that wrote its entry where Claude never reads, and an uninstall that could not find its own entry. A relative or whitespace-only value keeps the default, because Claude refuses to run at all when the resolved home is not absolute. (td-73c4ff)

- **A worktree created non-interactively can now be deleted non-interactively through the same lifecycle as the TUI.** `sidecar worktree delete TARGET --plan --json` reports the exact checkout, dirtiness, remote availability, branch-cleanup choices, and pinned branch and HEAD without changing anything; using the returned absolute path and re-running with `--expect-branch BRANCH --expect-head-oid OID --yes` closes its Sidecar worktree session and rooted managed shells before removing the directory. Local and remote branch cleanup remain explicit flags, as the confirmation's unchecked boxes are, and a failed create's exact pending-creation journal is cleared only after deletion succeeds. A rooted shell that refuses teardown is reported as a warning after the checkout is removed, while requested branch and journal cleanup still finish. The shared refusal rules still protect main, bare, detached, locked, missing, and prunable worktrees. (td-85b0c4)

## [v1.13.0] - 2026-09-02

### Features

- **Pi reports its own lifecycle to Sidecar.** `sidecar agent integration install pi` writes one Sidecar-owned extension into Pi's user-level extension directory (`$PI_CODING_AGENT_DIR/extensions`, or `~/.pi/agent/extensions`), and from then on a Pi pane's working, idle and session-identity transitions come from Pi itself rather than from reading its screen. It installs at the advisory tier on real traces of pi 0.84.3, which is the ceiling Pi can reach: it ships no permission system, so a blocked lane does not exist to be reported. Turn completion is taken from `agent_settled` and never from `agent_end`, because Pi can follow `agent_end` with an automatic retry or a compaction; tool use and process exit are deliberately not claimed. Install, inspect, repair and uninstall go through the same adapter contract as the other integrations, so `--dry-run` shows the exact ops and uninstall removes only the file Sidecar owns. It reports lifecycle facts only: never prompts, responses, tool data, paths or credentials.

- **An agent installed as a plain `#!/usr/bin/env node` shim now gets a state badge.** Detection used to match the pane's `argv[0]` basename only, so an npm-installed CLI left the interpreter in `argv[0]`, tmux reported `node`, and neither identity input named the agent — the pane was never claimed at all. Sidecar now scores the whole foreground process group the way Herdr does: it knows the generic runtimes (`sh bash zsh fish tmux node bun cmd powershell pwsh`, plus `python[3[.N]]`), unwraps one using its own argv, prefers the process group leader, and ranks a non-runtime match above a runtime one. A pane running `node /usr/local/bin/qwen` is Qwen. Node package layouts upstream recognises by path are recognised too, so a Pi or Qwen CLI launched by its `dist/cli.js` is named without guessing from screen text.

- **`sidecar agent start --kind pi` no longer times out.** Pi rewrites its own process title, so tmux reports the pane as `node` while the process is really named `pi`; Pi's process check accepted only the literal `pi` and refused its own correctly identified pane before a single detection rule ran. A resolved process identity now settles which provider a pane is, and it settles it in both directions — the same check stops one agent's detection rules being evaluated against another agent's screen, which a bare `node` allowance previously permitted. Pi deliberately gains no bare-`node` allowance of its own: its published rules are a single literal "Working…", which on any Node pane would be a wrong answer rather than a missing one.

- **`SIDECAR_AGENT` names the agent in a pane where the process cannot be seen.** Export it on a wrapper command — a container, a sandbox, anything that hides the real process — and Sidecar uses the named provider's detection rules for that pane. It is a hint and nothing more: real process evidence always wins over it, and it can never grant or revoke an agent's lifecycle authority, so setting it in someone else's pane cannot switch off their session binding. One bound on macOS: a wrapper that is a SIP-protected system binary, such as `sandbox-exec` or `ssh`, publishes no readable environment to anyone, so it cannot be hinted through; a wrapper you installed yourself can.

## [v1.12.0] - 2026-09-02

### Features

- **The Git tab reads a remote-bound project's repository.** Bound to `[host] Project` from `@`, Git shows that machine's changed files with their staged, unstaged and untracked state, its branch and upstream, its ahead/behind counts, its repository state (merging, rebasing, detached), its commit list and graph, its commit details, and its stash and branch lists. Patches come from the host and render through this machine's own diff parser and viewer, so side-by-side, wrap, the minimap and the full-screen diff are unchanged. Author and path filters run on the host, over its whole log; a subject search runs here over the rows already loaded, the same split a local project makes. Every write refuses by name and changes nothing on either machine: stage, unstage, commit, amend, discard, push, pull, fetch, branch switch, stash, init and open-in-editor all name the host, and the footer offers only what the surface can do. Open in GitHub, yank commit and yank id work, because the remote URL is the host's fact and the browser and clipboard are this machine's. There is no watcher across the boundary: status refreshes on `r` and on the host's own snapshot signal. A bound workspace the host cannot resolve as a worktree says so instead of falling through to this machine's offer to run `git init`. Hosts serve it with the new read-only `sidecar repo status|diff|history|commit|refs`, advertised as `repoReadV1`; a host too old for it says so and names the machine to update. Writing to a bound repository is not part of this: it is a refusal table with a row per gesture, waiting for host verbs that do not exist yet. (td-7a1393)

- **The Files tab browses a remote-bound project.** Selecting `[host] Project` from `@` already bound this TUI to that host; Files now shows that machine's tree and that machine's file bytes, never a same-named checkout on this disk. Directories expand against the host, and opening the tree costs one round trip for the root plus everything you had expanded rather than one per level. `ctrl+p` finds by name from the host's own catalog. Writes, git blame, file info, project search, and reveal-in-file-manager have no host verb behind them and refuse naming the host rather than silently doing nothing — the footer offers only what the surface can actually do. There is no filesystem watcher across the boundary: the tree refreshes on the host's own snapshot and on `r`, and claims nothing more. Hosts serve it with the new read-only `sidecar content tree`, advertised as `contentTreeV1`; a host too old for it says so and names the machine to update. (td-bc57bb)

- **A bound `@` destination is the screen for relayed `sidecar open` / `layout`.** Selecting `[host] Project` already bound this TUI as that host's project workspace; a host agent that runs `sidecar open` or `sidecar layout` now lands on that workspace when this instance holds the geometry lease, not only on Sessions and never on a same-named local twin. Sessions landing is unchanged when you are actually looking at that row. Off-screen still declines and never queues. (td-af932a)

- **Sessions can hide a remote's rows, and says so on the control that brings them back.** The View flyout grows a `⇅ show remotes` toggle with a checkbox per registered machine, so a busy host can be set aside without disconnecting it or editing config — the connection stays up and its notifications still arrive. A hidden machine leaves both projections at once, health row included, and the sort pill carries a struck host glyph (with a count once more than one machine is hidden) so missing rows are never a mystery. The choice persists, and it is dropped for a machine that is later de-registered. Hiding is not deleting: the machine's rows stay in the catalog, so pins, live terminal splits and the remembered selection all survive being hidden and come back with it — while its rows, cards, create targets and relayed pane requests are all off-screen for as long as it is. The flyout's `Filter: none` line is gone from both the global and project surfaces; the filter is reported only when a query is actually doing something.

- **Every agent's state detection now runs Herdr's published detection manifests instead of Sidecar's hand-written rules.** All ten providers Sidecar identifies — Claude Code, Codex, OpenCode, Cursor, Grok, Pi, Copilot, Amp, Antigravity and Muse — are classified by the vendored manifests, so an upstream rule fix arrives without anyone translating a regex. Claude's half-circle busy spinner (2.1.228 and newer) is recognised, so a working pane no longer has to be inferred from the absence of an idle prompt; MCP elicitation dialogs, the `/btw` overlay, "Waiting for N background agents" and "N MCP tasks still running" read as work in progress; a Muse pane sitting on an unanswered "Do you trust this workspace?" prompt reads as blocked instead of as a finished turn; a Copilot pane waiting for background agents reads as working; Cursor's approval prompts are recognised by the control lines every one of them renders rather than by a regex per prompt shape, so a prompt Cursor adds next release is already covered; and Codex's first-run "Do you trust the contents of this directory?" prompt, a Claude "Allow Claude to use …?" permission prompt, and a Codex approval prompt whose option line carries the composer glyph all read as blocked rather than idle. The things Sidecar knows that upstream does not are kept as data overlays with a fixture each — Claude's model picker still holds the prior state instead of reading as a finished turn, a Codex turn parked on a background terminal or in the middle of a tool call still reads as working, and an Antigravity pane on an unanswered "requesting permission for:" prompt reads as blocked — and the pane's status is still gated by Sidecar's stricter process check before any rule is evaluated.

- **From a Sidecar-managed pane on a host you are viewing, `sidecar open` and `sidecar layout` land on this machine.** The geometry lease names the screen: if you are looking at that Sessions row from here, the pane opens here; if you walked over to the host's own Sidecar, the open stays there. Relayed open never queues — a row that is not on screen, or a lease holder that is disconnected or too old, exits 4 with the reason rather than applying later against whatever is selected. There is still no `sidecar open --host`; an agent on the host runs the same command it runs locally. (td-3d2e0d, td-4971ac, td-716ba6, td-15344e, td-4c955f)

- **`n` on a remote Sessions row lists that host's files, diffs, issues, and notes, and a Terminal split creates the tmux session on the host.** Pickers stay empty until the host catalog arrives rather than filling from a same-named local twin. Resource rows come from the host's `content describe` matchers, not this machine's provider snapshot. `layout apply` of a new `kind: shell` pane uses the same host tmux path. (td-3fe778)

- **A click in a remote Sessions pane opens that host's file, issue, note, diff, or resource, not a same-named local twin.** Selecting a registered host's row and clicking a reference resolves and loads on the machine that owns the workspace, then renders here in the same Document, Issue, Note, Diff, and Resource panes local workspaces use. Nested links stay on that host; a failed remote read never falls back to this filesystem; HTTP(S) still opens in the local browser. The host must advertise `ContentReadV1` — an older Sidecar still streams its terminals, and the click names the machine to update. There is no `sidecar open --host`; `sidecar content resolve|read|describe` is the internal transport over the SSH connection already held. Inline edit, file finder, and project search stay unavailable on a remote source, because those would walk this machine. (td-89c1cb, td-87358d, td-925cf9)

### Bug Fixes

- **A host whose login shell prints one line to stdout is no longer stuck at "not-protocol".** The serve stream refused the first non-JSON line, so a motd, a version nag, or a wrapper's log line meant no hello, no snapshot, no Sessions rows and no `@` destinations for that machine — with an error naming the exact cause it would not handle. Output before the first protocol message is now skipped, bounded, including JSON that is not a protocol message so a profile logging `{"level":"info"}` cannot be mistaken for the stream starting. Nothing is skipped after the stream has proven itself, and a stream that is simply not this protocol still fails, quoting what the host actually wrote. A version this viewer refuses still reports as a version mismatch. (td-055768)

- **A Claude pane waiting on background subagents no longer announces that the turn is done.** Claude Code 2.1.257 reports that state in two places and Sidecar was reading neither: the `Waiting for N background agents to finish` row is no longer the last line above the prompt box once an `Update installed · Restart to update` banner sits between them, and the footer that used to say `· N shells ·` now says `· ← N agents ·`. With both signals missed, the prompt box alone read as a finished turn. Both are read now, and a waiting row left in the scrollback by an earlier turn does not hold the pane on the working lane.

- **A pane's detection window no longer drops its topmost visible row.** Detection reads the last N rows of a capture where N is the pane's height, and the newline that terminates a capture was being counted as a row — so on a real tmux capture, which pads the visible region to the full pane height and *then* terminates with a newline, the window started one row too low. Rules anchored on the first row of the screen, such as Codex's first-run trust prompt, could not match.

- **The agent verbs resolve a sibling worktree from a managed shell or worktree session without flags, and refusals name the fix.** An explicit target is still searched across every registered project, but when the same name exists in several, the caller's own project breaks the tie — the one `SIDECAR_SHELL` belongs to, or the one owning the worktree session tmux reports; outside a Sidecar session the ambiguity lists the projects and names `--project` / `--shell`. `--project` now also accepts what `create worktree --json` hands back — the worktree's path or basename resolves to the project that created it — and the create results carry `project`, the slug every verb accepts. A read-only lookup with `--project` no longer refuses because several Sidecar instances are showing that project; that check belongs to `open`, which has to land on one of them. (td-c906c1)

- **`sidecar agent list` reports each live pane once, under the project that owns it.** Every registered project that could see a worktree's checkout used to emit it — a worktree opened as its own project, a subdirectory of the repository registered as a project, a state directory with no checkout path at all — so one pane appeared under six project keys, twice with no name, and an explicit worktree target was "ambiguous across 3 Sidecar sessions". Each worktree root now has one owner: the project that created it, then the project whose checkout it is, then a project that merely discovered it through Git; a project with no checkout path owns no worktree rather than claiming the working directory. (td-ebd72c)

- **`create worktree` and `create shell` start a catalog family with provider arguments, and `create worktree --agent` with `--run` records the family.** Arguments after `--` follow the family's launch command, as `agent start -- ARGS` takes them: `sidecar create worktree orchestrate --agent claude -- --model fable`; the name comes before `--`, and a configured launch override that contains shell syntax refuses them rather than handing them to the wrong process. `--agent` with `--run` on a worktree is now the layering `create shell` had — the family is recorded, the caller's command owns the launch — instead of a refusal that left `--run "claude --model fable"` with no `agentType` on record. Usage refusals under `--json` are `{"error":{"code":"usage",...}}` on stderr rather than a prose line and the full help text. (td-a658ed)

- **`sidecar agent read` without `--source` now reads the visible screen, matching its documented default.** Omitting the flag previously failed with `source "" is not a terminal capture`. (td-152978)

- **Sessions no longer lists a remote project's main checkout just because Sidecar is running there.** `sidecar host serve` cannot exclude the host's own TUI pane the way a local collector excludes `TMUX_PANE`, so that pane's cwd used to mark the main worktree LIVE. The TUI is chrome, not a session, and a remote main checkout with no agent is hidden the same way the project sidebar already hides it locally. Linked worktrees, managed shells, and an agent actually running on main still appear.

### Changed

- **The `manifest_detection` flag is gone.** It ran Herdr's rules in shadow beside Sidecar's own and logged the differences; with every provider now classified by the manifests there is no second lane to compare against, so the flag, its entry on the flags page, and the shadow log it wrote have all been removed.

- **Tmux 3.4 is now Sidecar's explicit compatibility floor, with 3.7c continuously tested.** One checksum-pinned manifest drives local source builds and an oldest/latest CI matrix, including real private-server coverage for control mode, terminal rendering, paste, metadata and shell lifecycle. A separate latest-client/minimum-server proof models a Homebrew upgrade without touching the live default server and verifies capture fallback when tmux explicitly declines cross-version control mode. Future stable upgrades are one manifest change plus the same repeatable proof. (td-22399a)

### Dependencies

- **Tasks moves to v1.17.0**, which gives a project its own open/closed lifecycle: `project drop` and `project reopen` join `project complete`, a closed project is stamped on the section itself, and the Tasks tab hides closed projects until `C` reveals them.

## [v1.11.2] - 2026-08-31

### Bug Fixes

- **The read-only worktree planning proof now ignores Git's transient bookkeeping wherever the fixture repository sits beneath its snapshot root.** Background maintenance can create or remove lock files without Sidecar doing anything; the proof continues to compare every path Sidecar owns while no longer treating Git's internal locks as product mutations.

## [v1.11.1] - 2026-08-31

### Bug Fixes

- **Rendered Markdown in Files can be selected and copied as rendered text.** Drag selection now works in both the primary Files preview and file panes opened beside it, including rows whose layout differs from the Markdown source. Copying sends ANSI-free visible text to the native and terminal clipboards, while link clicks, drag-over-link selection, scrollbars, raw previews, and collapsed-tree previews keep their existing behavior. (td-a2b617)

- **Release verification no longer intermittently loses its private tmux server between agent-control integration tests.** The suite now keeps one inert session alive for the package lifetime, so one test cleaning up its last working session cannot race the next test's server startup. The package still uses its own socket and tears down only that isolated server.

- **The remote-host reconnect test now observes a stable recovered connection instead of a transient state.** Its successful fake stream stays open like the real host protocol, so loaded CI cannot miss the online state between an immediate end-of-stream and the following reconnect.

## [v1.11.0] - 2026-08-31

### Features

- **Shells lost to a tmux restart come back.** After a reboot or a tmux server crash, Sidecar recreates the managed shells that were running, under their own names and in their own working directories, once the first frame is on screen. `sidecar session status` shows what it would do before it does anything — every shell named as reattach, recreate-shell, resume-agent, manual, skip or refuse, with the reason and whether it would run an agent — and `sidecar session restore` performs exactly that plan, with `--dry-run`, `--shell`, `--agents` and `--yes`. `sidecar session policy` sets it per shell (`--inherit`, `--shell`, `--resume`, `--never`), so a long-running server, a disposable helper and a sensitive agent session can differ without changing the machine default. Nothing arbitrary is replayed: a `--run` command, dev server or test watcher is never restarted, a working directory that no longer exists is a refusal rather than a fallback to some other directory, and a tmux session name held by something else is a refusal rather than something Sidecar closes to take the name. Conversations are a separate decision from terminals: `plugins.workspace.sessionRestore.resumeAgents` defaults to `ask`, so a reboot restores your shells and then asks once, in one grouped summary, before resuming anything that can spend money or change a repository. (td-e78e17)

- **A Sidecar-managed shell can be bound to the exact agent conversation running in it.** A provider's own hook calls `sidecar agent report-session --kind KIND (--id ID | --path ABS_PATH)`, and Sidecar records which native conversation that pane is in. That binding is what makes a cold restart able to offer to resume *that* conversation, and what makes `sidecar agent read --source transcript` return it. Sidecar never guesses one: an unbound shell gets `transcript_unavailable`, because "the newest conversation in this directory" is wrong often enough to matter and looks identical to being right. Session values are redacted by default — `agent list` and `agent get` report only whether a shell is bound and whether an official integration vouched for it, and the value appears for your own shell or with `--include-session-ref`, since list output routinely lands in logs and CI artifacts. `shells.json` moves to schema version 3 to hold the binding, additively: a record that has never run an agent serializes exactly as version 2 wrote it. (td-8ec2cc)

- **Codex and Claude Code can tell Sidecar which conversation they are in.** `sidecar agent integration install codex` (or `claude`) adds one Sidecar-owned entry to that provider's own hook configuration, and from then on each new session reports its identity to the shell it is running in. They install at session-identity tier and stay there: these hooks say *which* conversation is running, never what state it is in, so screen and process detection remain the only authority for whether an agent is working, blocked, or done. Installation preserves every unrelated hook and every unrelated setting, `--dry-run` shows the exact ops, and uninstall removes only Sidecar's entry. Codex needs its hook trusted before it will run: Sidecar writes the trust record itself, and if that ever stops matching, the failure is Codex's own visible one-time "Hooks need review" prompt rather than a hook that silently never fires. (td-8ec2cc)

- **Every agent resume command now comes from one registry.** The Conversations plugin had the only table of how to resume each provider, as a switch building shell strings. It now lives in `agentcatalog` as structured argv, so the Conversations UI, the CLI, and cold restore share it and cannot drift, and a session identifier is an argument vector entry rather than text spliced into a command line. A reported identifier that would read as a flag is refused outright, both when it is validated and again when a resume is built from it. The UI keeps its current user-confirmed behavior and the command you see is unchanged. (td-8ec2cc)

- **Agent state can come from deterministic provider integrations instead of screen inference alone.** The new lifecycle contract records working, idle, blocked and terminal outcomes in a bounded JSONL store, resolves competing sources through one authority policy, and exposes the result through `sidecar agent report`, `end`, `release` and `explain`. The bundled OpenCode integration reports full lifecycle state; Codex and Claude Code report session identity. Install, inspect, update, repair and remove integrations from Configuration or the matching `sidecar agent integration` commands, all through the same service. Integrations report lifecycle facts only: they never send prompts, responses, tool data, paths, credentials, or notification policy. (td-43a93f)

- **A managed agent can be driven without taking over its terminal.** Behind the `agent_control` feature flag, `sidecar agent start`, `prompt`, `wait`, `read` and `send-keys` operate on one pinned shell, validate the complete request before writing anything, and refuse when the pane is busy, replaced, blocked, or owned by something else. Prompt delivery uses the same ordered paste and key encoding as the embedded terminal, waits use a pooled tmux control client with bounded polling fallback, and `read` offers visible, recent, unwrapped, detection and transcript sources. This is the non-interactive counterpart to watching and answering an agent in the TUI. (td-7de1af)

- **A pane you have opened can be moved.** `M` from any pane — or the new `⊞` button on the pane header, left of the close `×` — opens one reposition modal: `h/j/k/l` move a draft of the layout, `z` zooms, `enter` commits the whole sequence atomically, `esc` discards it. The pane is pulled out and grafted back, so its tabs, scroll position, selection and any live terminal travel with it, and so does the share of the box you dragged it to. All three pane hosts have it: project Workspaces, the global Sessions browser, and the content decks beside Files, Git and Notes. `M` was chosen over `m`, which is already `render` and `merge-workflow` in two of the twelve pane contexts, and one key must mean one thing in every pane. (td-2ec104)

- **`sidecar layout move` gives agents the same capability in one call.** `sidecar layout move 2.1 --to 1.2` moves by cell, `--to 3` appends to a column (opening one past the last), and `--focused --to left|right|up|down` uses the identical direction rule the modal's keys compile through, so the CLI and the keyboard cannot drift. It was always possible to rearrange a layout with `layout get` plus `layout apply --spec`, but that means reconstructing every pane on screen to move one of them, and every pane you reconstruct is a pane you can get wrong. Like get and apply it never queues, refuses rather than squeezing, and reports a move with nothing to do as `unchanged` (exit 0) rather than calling it moved. With `--sessions` it changes this machine's viewer tree even for a row whose workspace is on another host, and sends no layout mutation to that host. (td-2ec104)

- **Three shell verbs stopped being things only the TUI could do.** `sidecar shell rename --target <session>` renames a shell you are not sitting in — until now rename resolved "which shell am I" from the ambient tmux environment, so there was no way to rename any other one. `sidecar shell send --target <session> --run/--type <command>` sends a command into an existing shell, matching the `--run`/`--type` split `create shell` already had. And `sidecar create worktree --plan` resolves a worktree plan and prints it as JSON without creating anything, so a caller can show branch, path, source OID and whether a setup hook will run before committing to it. Sidecar owns `shells.json`, so a capability reachable only through the TUI was a gap for every agent, not only for remote hosts. `shell send` guards its own boundary: the underlying send-keys has no protection beyond a blank-command check, so the verb refuses a session that is not a live record or a registered worktree session for the resolved project, and refuses one recorded on a different tmux server rather than typing into whatever answers that name on this one. (td-677dde)

- **A configured project can be used before it has ever been opened.** `--project` resolved only through project state directories that already existed on disk, so a project listed in `config.projects.list` but never opened returned `unknown project`. It now falls back to the configured list and registers the project the same way first-open does. (td-677dde)

- **Sidecar can watch and drive sessions on another machine over SSH.** Behind the `sidecar_remote_hosts` feature flag, installing Sidecar on the host and registering it with `sidecar host add` makes that machine's projects, shells, worktrees, agent states and live panes appear in Sessions beside local ones. Selecting a row opens the real remote tmux pane with history, search, selection and ordered input intact; cross-host geometry leases let either machine take back its own viewport by typing. There is no daemon or listening port: Sidecar starts an ephemeral host process over SSH stdio, reuses tmux control mode for pane traffic, and names unreachable, missing-binary, missing-tmux, login-output and version-skew failures with their fixes. The Remote Hosts configuration page and `host add/list/set/remove/probe` commands share one validated registry and apply changes without an app restart. (td-f10d6f, td-998e58, td-42b724, td-141917)

- **Notifications can reach you inside Sidecar and outside the terminal.** Alerts now land in a persistent JSONL-backed notification centre with stacked toasts, an unread header indicator, keyboard and mouse actions, target links, per-source rules and a `sidecar notify` CLI for posting, listing, dismissing and configuring them. Native macOS and Linux notifications, built-in or custom sounds, quiet hours, background-only delivery and live config reload all run behind delivery adapters. A Sidecar running through SSH can forward a remote transition to the viewing machine or emit through the outer terminal, while deduplication and claim rules prevent the same event from alerting twice. (td-7b9ccc, td-eb6475, td-58679c)

- **Agents on another machine can be driven the same way as agents on this one.** Every `sidecar agent` verb — `list`, `get`, `start`, `prompt`, `wait`, `read`, `send-keys` — plus `session status` and `session restore` now take `--host ID` and run on that registered host, as one `sidecar <verb> --json` invocation over the ssh connection Sidecar already keeps open. There is no second protocol and no new daemon: the verbs were headless, target-taking and `--json` from the start, so carrying them to another machine is transport rather than design. What you get back is the host's own answer, including its refusals — a blocked agent on another machine reports `agent_blocked` with the host's own sentence, not a local approximation of it, because the rules run on the machine that owns the pane. Two failures the local vocabulary could not describe honestly get their own codes: `host_unavailable` when the machine could not be reached at all (nothing was attempted, so trying later is the fix) and `version_skew` when the host's Sidecar does not know the verb (update one of the two binaries). Conversation identifiers stay where they belong: a remote `agent list` or `agent get` tells you whether a shell is bound and whether an official integration vouched for it, never what it is bound to, unless you ask with `--include-session-ref`. Nothing remote is ever written into this machine's `shells.json`. A remote verb requires an explicit target, because the "current shell" shorthand names a shell on *this* machine and two machines running a same-named project generate the same tmux session names. Cold restore runs on the host too — a viewer asks for it and watches, and never rebuilds another machine's state locally. Behind the existing `sidecar_remote_hosts` and `agent_control` flags. (td-a55114)

### Bug Fixes

- **A tmux server crash no longer erases your shell records.** When a tmux server dies, every managed session disappears in the same instant. Sidecar's liveness check asked, once per shell, "is this one gone?" — got a true-looking answer every time, and tombstoned the whole file. That is how a crashed server took a user's `sidecar` and `braid` shell lists with it. The liveness path no longer asks for a deletion in that situation at all: when no tmux server is running, or when a shell's last confirmed server is not the one running now, the record is kept and marked as something a restore can bring back. A shell that exits inside a server that is still up is still tombstoned, and still recoverable with `sidecar shell restore`, because that is a terminal you closed. Both paths that could delete were fixed, including the project workspace one, which reaped per shell with no protection at all. (td-e78e17)

- **A late report from an agent that has already exited can no longer overwrite its successor's session binding.** This one was found by building the proof rather than by a test. The generation a reporting hook derives for itself falls back to the pane's root process when it cannot trace its own ancestry back to the pane, and the independently-derived "what occupies this pane now" falls back to the pane's root process when nothing is running there. Those are exactly the two conditions that hold for a hook left behind by a provider that has exited: it has been reparented away from the pane, and the pane is empty. Both sides fell back, compared equal, and the stale report was accepted — in precisely the case the check exists to reject. The reporting side now has no fallback: a hook that cannot prove which provider it belongs to is refused rather than believed. Two fallbacks agreeing is not evidence. (td-8ec2cc)

- **`agent prompt` with the text left off no longer prompts your own shell with a shell's name.** `sidecar agent prompt reviewer` read `reviewer` as the prompt text and typed it into the shell the caller was sitting in, because one positional argument means "prompt this shell". A lone argument that resolves to a managed target is now a usage error naming what is missing. Empty or blank prompt text is a usage error too — it exited 5, the code for a semantic refusal, where every other malformed command line exits 2, and a caller could not tell "I built this wrong" from "the agent would not take it". (td-2cea15)

- **`layout apply --json` and `layout move --json` write only JSON to stdout.** Both wrote the structured result object and then appended the human per-pane lines after it, so `sidecar layout move 2.1 --to 1.2 --json | jq` failed on the trailing text — which is the only reason to ask for `--json` at all. The flag's own help already promised "one structured result object to stdout", and `layout get --json` already kept that promise. The two projections are alternatives now: without `--json` you get the human lines and nothing else, with it you get the object and nothing else. Both carry the same per-item verdicts, cells and reasons, so nothing is lost by choosing one. (td-0e2d12)

- **The reposition modal asks before discarding a live inline edit.** Opening it released every input surface the content deck owns, and for an inline edit that release killed the tmux session holding an unsaved buffer without a word. Every other caller of that release is a surface teardown — a plugin switch, a scope change, shutdown — where the editor has nowhere left to be drawn; the modal is the one caller that keeps the deck on screen and returns to it, so nothing forced the buffer to die. It now raises the editor's existing Save/Discard/Cancel dialog, the same one clicking away from the editor raises, and opens the modal on the pane you asked for once you have answered. Both doors onto the modal — `M` and the header `⊞` — inherit it. (td-0e2d12)

- **A power loss during an agent lifecycle report no longer costs two reports instead of one.** The lifecycle log is append-only JSONL, and a machine dying mid-append leaves a final line with no newline. The next report was appended straight onto that fragment, welding two records into one line that neither could be read from — so the crash cost the report it interrupted *and* the next healthy one, while the write reported success. A pane that had reached its third report came back believing it was on its first. The log is now re-framed when it does not end where a line should. (td-b2370e)

- **Validation errors no longer exit with the code that means "you passed a bad flag."** `create worktree`, `create shell --name` and `shell rename --target` returned exit 2 both for an unusable command line and for a perfectly well-formed one whose *value* was rejected — a branch that already exists, a display name already in use. A caller cannot tell those apart, and the remote-host viewer read the second as version skew and told the user to upgrade Sidecar. Input rejection is now exit 5; exit 2 keeps meaning a usage error. Documented in each command's exit-code table. (td-677dde)

- **A display name or worktree name starting with `-` works.** `shellstate.NormalizeName` accepts a leading dash, so `-wip` was a legal name that the argument parser then read as an unknown option. `shell rename --target`, `shell send` and `create worktree` now stop flag parsing at `--`. (td-677dde)

- **`notify config set` is inside the isolation gate.** It rewrites the whole config file but carried no mutating mark, leaving it the one mutating verb a misconfigured proof run could still drive against the real `~/.config/sidecar/config.json`. (td-677dde)

- **Embedded terminal backgrounds no longer flicker, truncate, or flood at particular widths.** Captures now preserve blank rows with `capture-pane -N`, every drawn cell takes its background from tmux, and Sidecar paints a row's carried background through its trailing cells without letting pane padding leak into the child's pen. The old content-based canvas heuristic is gone, so a terminal's colour no longer changes because its text or width happened to trigger a guess.

### Remote hosts (behind `features.SidecarRemoteHosts`, default off)

- **Phase C: a remote host can be changed, not only watched.** Creating a shell, creating a worktree through its confirmation, seeding an agent, and renaming either kind now work on a remote row from the Sessions browser. Mutations run as one-shot `sidecar <verb> --json` invocations over the ssh connection that already carries the observation stream, so the serve protocol stays one-directional and read-only by construction. Rows arrive through the host's next snapshot rather than being invented locally. Delete, merge and navigation still refuse — their implementations resolve paths against the local filesystem. That was also a live bug in `O` (open in Git), which had no guard at all and sent a remote path into a local worktree switch; on a machine with the same checkout layout that succeeds against the wrong repository. (td-677dde)

- **A confirmed worktree plan is pinned to its commit.** `create worktree --expect-source-oid OID` refuses with exit 5 when the base ref no longer resolves to the confirmed commit, and the remote confirmation passes its plan's OID back on Create — so an agent pushing to the branch while the user reads the modal yields a refusal naming both commits, not a worktree silently built from the new head. The local modal already had this guard from executing its stored plan; the remote path re-ran the command from raw arguments and did not. Also from the pre-merge review: a setup hook failing with its own `command not found` is no longer misread as an uninstalled Sidecar, a login profile emitting 32+ structured-log lines can no longer push a successful result out of the decode window, host-derived error text is stripped of terminal escape bytes before display, disabled hosts refuse mutations up front by name instead of failing as "removed or retargeted", the rename modal shows an in-flight state and swallows the double Enter that raced two renames on the host, a stale reply from one host no longer clears another host's pending selection, switching the create form from a local to a remote project clears the local repo's branch list, and Merge is hidden on remote rows rather than offered and then refused. (td-677dde)

## [v1.10.0] - 2026-08-28

### Features

- **The workspace sidebar reads as a list of cards instead of a wall of text.** Two-line workspace entries sat flush against each other, so line 2 of one row and line 1 of the next were adjacent and the eye had nothing to anchor on; section headers were dim muted text with no glyph and no divider, which made a section boundary the hardest transition on the pane to see. Rows now carry a blank line between them and one before each heading, and headings render flush-left and uppercase with a category glyph — `📌 PINNED`, `● LIVE`, `○ IDLE`, `◆ NEEDS ATTENTION`, `● WORKING` — followed by a horizontal rule that runs to the section's action button. A project-grouped heading takes that project's stable theme hue for both glyph and title, while category and time headings stay neutral. The selected row keeps a full-width fill on both surfaces and changes only its text hierarchy when focus leaves, so a selection is never ambiguous. All of it lives in `internal/workspacelist`, so the project Workspaces sidebar and the global Sessions browser inherit the same presentation, including the scroll, wheel and scrollbar math that had to learn about the new row heights. (td-fd674e)

- **`sidecar --version` now reports the commit, build date, and build profile.** A released binary previously printed only `sidecar version v1.9.0`, which is the one build that cannot be traced back to source from the outside, so a bug report from a Homebrew install gave no way to tell which commit it came from. Release builds now carry `ShortCommit` and the build date from the release pipeline, `make build` and `make install` stamp the commit and dirty state from the working tree, and anything built without ldflags falls back to `debug.ReadBuildInfo`. The first line of the output is unchanged and stays a single `sidecar version <v>` line, because `scripts/dev-install.sh` matches it by prefix and `scripts/verify-release-archives.sh` compares it exactly; the new detail sits on indented lines below it. Unmanaged `make install` builds deliberately still do not stamp a release version, so the update checker keeps offering updates on locally built binaries. Based on the work of [@justin13888](https://github.com/justin13888) in [#228](https://github.com/marcus/sidecar/pull/228). ([#227](https://github.com/marcus/sidecar/issues/227))

### Bug Fixes

- **A file opened beside Files keeps the shortcuts it has in Files.** A document pane composed next to a plugin answered a thin subset of the document keys: `E` external editor, `ctrl+p` Find, `f` project Search, `I` Info, `y` contents, `Y` path, selection copy and select-all, and `+` / `-` resize all did nothing, and `esc` hid the pane instead of clearing a selection. The pane now uses the shared `workspace-doc` context and answers every file-facing key the primary preview does wherever the shared viewer owns the capability, with Find and Search built on the same `internal/panesearch` surfaces the project and global Workspace document panes use, rooted at that deck's project and loading their result back into the focused pane. Because the finder, ripgrep and git-info messages are broadcast types Files also consumes, each is now tagged with the deck and leaf that issued it, so one pane can no longer swallow another's scan. `y` copies the visible selection when there is one and the file otherwise — the Files rule, now implemented once in `docview` rather than decided separately by each host. Files-only tree operations such as rename and full-screen blame stay with the primary surface and are deliberately not forwarded to a different file behind the focused pane.

- **A file finder opened in the global Sessions preview no longer loses its results when the selection moves.** The scan and search messages carry no root and no surface identity, so a result could land in whichever pane happened to be current — and moving the Sessions selection to another workspace while a scan was in flight left the originating pane with nothing. Each search command now carries its workspace ID and is routed back to that workspace's pane, live or cached, and `previewDocSearchMsg` joins the async set the model keeps delivering while the selection is elsewhere.

- **An apostrophe in a task description no longer breaks agent launch on macOS.** The generated `start.sh` passed the prompt through a heredoc nested inside `$(...)`, and bash 3.2 — still what `/bin/bash` is on macOS — mis-tracks single-quote state while scanning for the closing paren. An odd number of apostrophes in the prompt turned the whole script into a syntax error, so `fix today's bug` failed to launch while `don't break the user's code` worked, which is why this survived so long: an even count accidentally re-balances the lexer, including in the test that was supposed to cover it. A `"` or a `)` in the prompt was broken by the same pattern, with `)` silently truncating the prompt and leaking the heredoc delimiter into the agent's argv. The prompt is now staged in a tmpfile written by a top-level heredoc and read back with a plain `cat`, which parses correctly on every bash. Thanks to [@imsickofmaps](https://github.com/imsickofmaps) for the diagnosis and the fix, and to [@jennings](https://github.com/jennings), who reported the same bug and sent an equivalent fix in [#225](https://github.com/marcus/sidecar/pull/225) back in March, months before this landed. ([#262](https://github.com/marcus/sidecar/pull/262))

### Dependencies

- **Tasks moves to v1.16.0, and the Tasks tab can show finished work.** `C` on the Outline reveals DONE and CANCELLED rows in place instead of hiding them, and the same key on the Projects tab nests closed children under their project rather than pruning them and hoisting the open ones. Hidden rows stay honest either way: section badges read `4 · 1 closed`, a search whose matches are all closed says so instead of rendering blank, and reordering anchors on the nearest *visible* sibling so a keypress can't rewrite the file without moving anything on screen. Delegation stamps — `tasks show`'s `(since …)`, the TUI detail pane, the claim-conflict message — now render in the configured timezone and time format, with the year shown only when it isn't the reader's own, so an old handoff can't read as yesterday's. Storage and `--json` keep the exact UTC instant.

- **td moves to v0.65.0, which lets a hosted session approve again.** Hosted `td-sync` handlers have no on-disk `BaseDir`, so review-policy resolution skipped the resolver and fell back to strict mode: an implementation-involved browser session was offered only `reject` in `available_transitions` even though td's documented default is trusted and an attributed approval is valid there. Policy resolution is now centralized across review, approve, close, and transition discovery, and honors process-wide environment overrides in hosted contexts. The release also fixes td's own deploy pipeline — a failed remote build used to be masked into a green health check against the container it had failed to replace — and adopts Sidecar's blocking golangci-lint gate.

## [v1.9.0] - 2026-08-27

### Features

- **Embedded agent terminals now keep the host terminal's background and honor synchronized redraws.** Default-background cells no longer fall through to Sidecar's own canvas, which made Codex look like a dark rectangle compared with the same session in Ghostty; Sidecar asks the host for its real background and uses it only where the child selected the terminal default, while explicit Claude, Grok, and Cursor canvases still win. Canvas inference now also requires vertical reach through the live content, so a localized Codex or Cursor composer cannot become the whole-pane background merely because a one-column rewrap moved another painted row across the viewport edge. Cursor's CLI also brackets composer updates with DEC mode 2026, and Sidecar now holds intermediate emulator frames until that transaction closes instead of flashing partially cleared status and input rows on each keystroke, with a one-second fail-safe for a malformed child. On macOS, Cursor launched as `agent` is identified from its foreground process group's symlink-resolved `argv[0]` even when tmux reports the shared runtime `node`, using the same process-first rule as Herdr without reopening the screen-text false positives. ([#313](https://github.com/marcus/sidecar/issues/313), td-3b8972)

- **A shell record that was written by a newer Sidecar is no longer quietly rewritten without the parts this build could not read.** `shells.json` has carried a `version` field since the beginning and nothing ever read it, so a file from a newer binary would parse into the fields this one knows, lose the rest on the next write, and look fine doing it — the same failure the shell-record durability work is about, one level up in the format itself. The version is now 2 and every writer checks it: a manifest from the future is refused with a message naming both versions, and the file is left byte-identical. A version 1 file upgrades in place on its first write, keeping every field. Reads are unaffected at any version, because a read cannot lose anything. (td-362a41)

- **Forgotten shell records stop accumulating forever.** Forgetting a shell moves its definition to a tombstone so `sidecar shell restore` can put it back, and until now nothing ever removed one — a long-lived project's `shells.json` grew by a record for every shell ever forgotten. Tombstones now expire after `shells.tombstoneRetention` in `config.json`, which takes a Go duration, a day count (`"30d"`), or `"forever"`, and defaults to 14 days. Expiry runs at the writer boundary, so the file is bounded without a background sweeper. One deliberate consequence: once a record's window passes, Sidecar no longer remembers the forget, so a tmux session of that name that is still running becomes an ordinary adoptable row again rather than staying invisible. (td-362a41)

### Dependencies

- **Tasks moves to v1.15.0, and the Tasks tab gains its new triage surface.** `N` marks the selected open task as a GTD next action in one keypress from the list or detail view, and it refuses politely on proposed and done tasks rather than mutating from an input context. An empty Next tab now explains itself — how many dated items are waiting on Agenda, counted by the same query the Agenda tab paints, and how to mark one — instead of rendering a blank pane beside a full Agenda. The Inbox tab buckets approvals and accepted rows under project headings so a triage pass stays inside one theme, with headings as unselectable chrome the a/r walk steps past. `tasks move` files a PROPOSED task into a project without the reject-and-re-propose dance that used to mint a new id.

## [v1.8.0] - 2026-08-26

### Features

- **Quit on Sessions, and the panes come back.** The global Sessions browser already restored the tab, but the selected row and anything composed beside it died with the process. Those now live on global `state.json`: the last row, each composed pane tree, and which leaf had focus. A terminal split reattaches to its still-running tmux session with scrollback intact; a row you only previewed writes nothing. A missing document, issue, unknown kind, or dead split drops that leaf and collapses its split rather than failing the restore. (td-e5a987)

- **`sidecar layout get` and `apply` answer for Sessions.** `--sessions [ROW]` targets the global surface — the selected row by default, or a durable inventory ID / display name with the same ambiguity rules as `--shell`. Off-screen is exit 4. Get and apply share the project workspace's report shape and all-or-nothing verdict path, so a tree an agent cannot read or compose is no longer a Sessions-only hole. (td-b6178f)

- **The Tasks tab keeps up with work done anywhere else.** A Sidecar left open all day only ever reread the local task database, so a task created in the hosted browser, or by a teammate, or on another machine, stayed invisible until some unrelated `td` command happened to pull it in. td v0.64.0 moves that responsibility into the monitor itself: it subscribes to the server's event stream and applies changes within about a second, falling back to cheap status probes and then to timed sync where a stream cannot be held. Work done in the tab — creating, starting, approving, closing, logging — now pushes as soon as it commits instead of waiting for something else to flush it. Sidecar wires none of this; it comes with the dependency.

- **The create modal is steerable with the arrow keys alone.** Choosing a pane kind used to mean Shift+Tab up to the list, arrowing to the row, then Tab back down to the Name field — three gestures before the one that mattered. Up and down now reach the kind list from wherever focus sits, so `n`, a couple of arrows and Enter is the whole interaction, and the fields that give the arrows a meaning of their own — the Project, Base Branch and Agent combos — still keep them for their dropdowns. The hint line says so.
- **The global Sessions browser offers its configured resource providers.** Its preview has placed Resource panes all along — `sidecar open <locator>` opened one there, and the surface resolves the target through the same core the project workspace does — but the create modal never listed the provider rows. They shared a flag with Terminal split, which this surface genuinely cannot offer (one terminal producer, bound to the selected row), so a passive row was gated on a live-terminal capability it does not need. The flag now means only what its name says, and the two surfaces' catalogs differ by exactly the Terminal split row.
- **The kind list reads as one block.** Its rows were chrome-sized to their own text, so the fill ended wherever that row's description happened to, and the list showed a ragged right edge whose shape was an accident of the longest line. Every row now spans the modal's whole content column, and a disabled row keeps the list's fill under its muted text rather than punching a hole in it.

## [v1.7.0] - 2026-08-25

### Features

- **Agents compose pane layouts in one call: `sidecar layout get` and `sidecar layout apply`.** Opening a working set of panes used to mean a sequence of `sidecar open` calls, each placed by a policy the agent could not see and could not address, with no way to read back what was on screen first. `layout get --json` now answers with the current layout — a columns-of-rows grid projection, every pane's kind, targets, tabs and tmux session, the geometry, and the caps and floors an apply will be held to. `layout apply` composes onto it, either additively (repeatable `--pane` descriptors) or as a full layout (`--spec`, or `-` for stdin) that replaces the screen. Apply is all-or-nothing: the host resolves every descriptor, builds the whole trial tree, fit-tests it against the pane floors, and either commits atomically or declines with the reason naming the first violation and leaves the layout byte-identical. The ack's `items` array carries a verdict per requested pane — `opened`, `retargeted`, `carried`, or `declined` with its own reason — so one round trip shows everything wrong with a refused spec rather than one error at a time. A spec must account for every live terminal by name and is declined if it omits one: apply never destroys a live session implicitly. Unlike `open`, layout requests never queue — a queued atomic apply would validate against a tree that no longer exists, and a stale `get` answer is worse than a refusal. (td-e9a089, td-89033c)
- **`sidecar open <target> --at <col>[.<row>]` places a pane at an explicit grid cell.** `--split` expresses a preference and silently no-ops when the open retargets an existing pane; `--at` expresses a requirement, so a cell that cannot be honored exactly declines rather than landing the pane somewhere else. The two are mutually exclusive. Cells are 1-based `col.row` against the same columns-of-rows vocabulary `layout get` prints, and the same requirement semantics apply on all three surfaces that host panes. (td-89033c)
- **The create modal is now a pane switcher.** Pressing `n` used to offer Shell, Worktree and Terminal split; opening a file, a diff, a td issue, a note or a configured resource provider beside your work had no keyboard path at all. The modal now lists every pane kind — growing from a horizontal toggle into the vertical list with aligned descriptions once the row count earns it — and continues to a target picker for the kinds that need one: fuzzy path match for files, recent commits and refs for diffs, in-progress and recent issues from td, recent notes, and a locator field for each configured provider instance. Every pane kind shows the placement row, so one click both chooses where the pane goes and creates it. Disabled rows stay visible with the reason inline instead of vanishing. The pickers resolve to exactly the target shape the CLI produces, so the modal is an entry point rather than a second implementation, and the rows work identically from the project workspace and the global Sessions browser. (td-2962e3)
- **The pane switcher opens from whatever pane you are reading.** Every content pane absorbs the keys it does not own — that is what stops a stray key firing a workspace action behind it — so the switcher used to be reachable only from the sidebar or the terminal, and putting a second pane beside the one you were reading meant leaving it first. `n` now opens it from a focused Document, Issue, Note, Diff or Resource pane, on both the project workspace and the global Sessions browser. It is the same key the sidebar and the terminal preview already answer with "make me a new thing", so the answer no longer changes with focus. Two consequences, both deliberate: the Diff pane's `n` / `N` next-change pair moved to `>` / `<` — the shifted forms of its `,` / `.` file steps, so the pair reads as one hierarchy — and a live input surface inside a pane still wins, so a committed in-file search keeps `n` for its next-match while it is up. The terminal preview keeps `o`, because `n` there belongs to the list's create. Extending the same entry to Notes, Files and Git under `ctrl+n` is planned separately (`docs/plans/active/pane-switcher-everywhere.md`). (td-18e1c1)
- **The fourth pane forms a 2×2.** Auto placement used to stack a third row in the right column once two content panes were open, which is where the floors start refusing on ordinary terminals. It now fills the emptiest column instead — so the fourth pane opens beside the primary terminal — then opens a new column when every column is full, and refuses at the caps (4 columns, 4 panes per column) with a message naming the rule. `LiveLeafCap` is unchanged at two live terminals. (td-afa959)

### Developer
- **A release is now changelog + one command.** `BUMP=major|minor|patch make release` derives the next version from the latest tag, stamps `## [Unreleased]` to `## [vX.Y.Z] - <today>`, commits `release: prepare vX.Y.Z`, pushes `main`, and publishes — the version is stated exactly once, or zero times when `BUMP` implies it. Previously the operator had to hand-edit the changelog heading to a specific version and then repeat that same version on the command line via `RELEASE_VERSION`, and nothing caught the two disagreeing. `scripts/release.sh` now refuses an empty `[Unreleased]` section, a tree dirty beyond `CHANGELOG.md`, a tag that already exists, and a `RELEASE_VERSION` that contradicts an already-stamped heading, naming both versions in that last case. `make release-dry-run` prints the derived plan and exits before any mutation. `RELEASE_VERSION=vX.Y.Z make release` still works unchanged as the explicit-version path. Ported from `tasks`, which already carried this flow. (td-0dda74)

## [v1.6.0] - 2026-08-24

### Features

- **Shell records survive their tmux server.** A tmux server that died or
  restarted used to empty a project's `shells.json`, taking every shell's
  display name, working directory, agent type and skip-perms flag with it — and
  those records are the only thing that can rebuild a shell, so deleting them
  deleted the recovery path. On 2026-08-22 that emptied five projects at once.
  Sidecar now models the tmux server as an identity of its own (socket
  inode+ctime and the server `#{pid}`), because liveness is a joint property of
  a shell and the server it was observed on: when the server is replaced, every
  signal changes at once for a reason that has nothing to do with any one shell.
  "No server running" is read as a fact about the server rather than a listing
  of zero sessions, startup reconciliation is additive-only, and a shell that
  is not running degrades to an offline row you can press Enter on to recreate.
  No code path can now write a manifest with fewer entries than it read except
  the two single-identity removals, and that is enforced by a test at the writer
  boundary rather than by convention.
- **`sidecar shell list`, `forget`, and `restore`.** Forgetting a shell record
  was previously reachable only by pressing a key in the TUI, and Sidecar owns
  these records outright — nothing underneath it can reconstruct a display name
  or agent type — so the capability is owed a non-interactive path. `forget`
  moves a record to a tombstone rather than dropping it, `restore` puts it back
  with its display name, agent type, skip-perms and working directory intact,
  and `list` shows live and forgotten records on both the human and `--json`
  surfaces. The tmux session itself is never started or killed; this is the
  record, which is the part Sidecar owns.
- **A visible terminal no longer costs most of a CPU core.** Leaving an agent
  actively rendering in a visible Sidecar terminal profiled at 53% CPU against
  a 12.7% hidden baseline — 40 points of overhead for having the pane on
  screen. Filesystem and Git resolution are out of `View()`, project Workspace
  and global Sessions share one bounded content-link resolution and row
  analysis path, ordinary frames no longer build diagnostic cell grids,
  presentations with no consumer-visible change are suppressed, and global
  terminal updates no longer rebuild the workspace list. Where that still
  missed the budget, per-feed publication became adaptive: every byte is still
  consumed immediately, with an immediate leading frame, at most 30 sustained
  frames per second, and a guaranteed trailing frame. The isolated fixture
  measures 8.7% visible against 0.4% hidden with output-to-frame p95 of 34 ms.
  Scrollback, selection, links, cursor and mouse modes, resize and input
  latency are unchanged.

### Bug Fixes

- **A terminal whose control client dies is told why.** When a tmux control
  client closed, the notice that a pane's live model had died could be dropped:
  the close tore down the subscriber state that the notice needed, and whichever
  of the two paths got there second found nothing to deliver to and returned
  silently. The consumer was told to fall back to capture — that report goes out
  on a different path — but never told its model was gone, so it had nothing to
  re-subscribe from and a pane could sit on capture fallback indefinitely. The
  closing path now reports any death the ordered actor has not already claimed,
  with the claim taken under the client mutex in both places so exactly one of
  them reports each one.
- **The td tab survives a `td sync`.** `td sync` installs snapshots by
  atomically replacing `.todos/issues.db`, and the embedded monitor's SQLite
  connection kept pointing at the unlinked inode, where every later write failed
  with `SQLITE_READONLY` — correct file permissions, unwritable database.
  Sidecar now notices when `issues.db` names a different file, closes the stale
  handle, reopens against the current one, and replays the keypress that
  triggered the check. Thanks to @gvorwaller (#308).
- **Restored resource tabs load themselves again.** With a provider configured
  and tickets open in panes, every tab came back after a relaunch saying
  "Waiting for `<provider>` to be ready" for a provider that already was, and
  each one had to be refreshed by hand. Restore now asks for the tab that is on
  screen — one call, not one per remembered tab — and a request made before any
  provider is wired up leaves the tab armed instead of failing it, so provider
  readiness resolves it without the user touching anything. A project or
  worktree switch republishes providers to the rebuilt surfaces too, which had
  been silently dropping both the matchers and the resolver.
- **Website**: the navbar theme switcher's dropdown and caret render correctly,
  the homepage gained a terminal compatibility section, and the advertised
  release version and commit count are derived at build time instead of being
  edited by hand.

### Dependencies

- `github.com/marcus/tasks` v1.13.0 -> v1.14.0 (Sidecar-style modals in the
  Tasks TUI: boxed fields, real buttons, fixed footer, draggable scrollbars).
- `github.com/marcus/td` stays at v0.63.0.

## [v1.5.0] - 2026-08-24

### Features

- **The Files readable view recognizes content links.** Rendered Markdown
  (`m` / Render) was the one Sidecar reading surface where a `td-*` id, a
  `path/file.go:42` reference, a commit hash, a URL, a `sidecar://` intent, or a
  provider key went dead — toggling Render removed the links you were about to
  click, while the byte-identical Workspace document pane kept them live. It now
  scans what was drawn, the same way Notes and the document viewer already do.
- **A file opened beside Files is live too.** Document panes in the app content
  deck scan their bodies through the same seam Workspace uses, so a token inside
  one activates and stacks a tab in the deck it was drawn in. Issue, note,
  resource, and diff bodies stay unscanned, matching every other surface.
- **Provider keys inside Markdown links open their card.**
  `[ZMS-37161](https://<site>.atlassian.net/browse/ZMS-37161)` — how a ticket
  normally appears in a brief — could never reach a provider: the key is only in
  the label, and an issue-key matcher can never match a whole browse URL. With
  the destination's host listed in that instance's `claimHosts`, the label now
  opens the Resource card and keeps its browser hyperlink, so cmd-click still
  reaches the ticket. Only frames Sidecar's own Markdown renderer drew are
  eligible; a program writing to a terminal still means what its destination
  says. `claimHosts` is now documented in the provider protocol reference.
- **`./scripts/demo.sh` ships a sample document** exercising every content-link
  kind that works without an external provider.

### Bug Fixes

- **Files preview geometry followed the source line count while reading rendered
  rows.** Harmless while rendered Markdown opted out of scanning; the drawn rows
  and the exported geometry now read one accessor, and a truncated file no
  longer exports one row more than it draws.

## [v1.4.0] - 2026-08-22

### Features

- **Scrollbars you can grab, everywhere they appear.** A shared interactive
  scrollbar core with one geometry and inverse mapping now backs the palette,
  notification centre, file browser (tree, search, and preview), git,
  conversations, notes, the doc and issue viewers, kanban lanes, the sessions
  sidebar, the project workspace sidebar, the theme picker, modal viewports,
  and the project switcher. Bars drag from anywhere on the track, hosted views
  receive the gesture through the content deck, and a sub-modal opening cancels
  the parent's in-flight drag.
- **Terminal panes scroll interactively too**, with the scrollbar wired through
  both workspace projections rather than reimplemented per surface.
- **Cross-project td issue links resolve on a local miss.** An issue ID that
  does not exist in the current project is looked up across the other
  configured projects instead of dead-ending.
- **Carried terminal backgrounds are span-capped.** `bounded` renders short
  runs — diff hunks, highlighted notices — and drops the background of any run
  longer than the cap, so an application that paints most of its output one
  colour degrades to plain text instead of repainting the pane. `never`
  suppresses carried backgrounds entirely; `auto` keeps the previous
  canvas-detection behaviour. Configurable per surface as
  `plugins.workspace.terminalBackgrounds` and `terminalBackgroundSpanMax`.
- **24-bit colour is advertised to tmux.** A fresh tmux server carries no
  direct-colour capability, so tmux quantized truecolour output to the 256 cube
  inside sidecar panes. Sidecar now appends one `terminal-overrides` entry,
  idempotently. The claim names sidecar's own `TERM` rather than every terminal
  type, and is only made when `COLORTERM` says the terminal renders direct
  colour — `terminal-overrides` is a server option that outlives sidecar and
  applies to every session on the server, so a blanket claim would corrupt
  colour in terminals sidecar never opened. Failure is never fatal: a colour
  hint cannot block opening a shell.
- **A Feature Flags page listing the whole registry.** Configuration → System →
  Feature Flags shows every flag Sidecar knows about with its real state.
  Previously only four hand-picked flags had a control, so `tmux_interactive_input`,
  `tmux_inline_edit`, `files_auto_refresh`, `plugin_content_panes`, and
  `terminal_resource_providers` were settable only by hand-editing
  `config.json`. Flags that Panels & Integrations already owns are shown
  read-only with a jump to the control that owns them, so the flag and the
  plugin's own enabled key cannot drift apart.
- **The split workspace terminal ships on by default.**

### Bug Fixes

- **Relaunching returns to the global tab you left on.** Quitting from
  Sessions, Activity, or Tasks reopened the project workspace: the last global
  tab was remembered but the space itself was not, so the remembered tab only
  applied the next time you pressed `K`. A remembered space whose tabs are no
  longer available falls back rather than showing an empty surface.
- **Sidecar never resizes the tmux pane hosting itself.** Scrolling the global
  Sessions list to its bottom could shrink Sidecar's own screen (td-9cddeb).
- **Settings toggles stop drifting right on a wide window.** Panel rows
  right-aligned their ON/OFF pill to the pane's full width, so widening the
  terminal pulled every toggle away from its label. Rows now stop at a shared
  cap that lines the pills up with where the widest form field ends.
- **Background SGRs are stripped without nesting escape sequences.**
- **The global Sessions list sees new projects and non-Git projects' shells.**
- **The files tree shows the full list from the top when it fits the viewport.**
- **Repeat pane opens stop lying**, and `create shell` opens beside the session.
- **The workspace sidebar's free-scroll clears on selection change only.**
- **`g` / `G` can be typed into the conversations session search box** instead
  of jumping the list.

### Build

- **Go 1.27**, matching td and tasks. `encoding/json/v2` is the default
  implementation; sidecar's adapter parsing, config, and state round-trip
  unchanged.
- **`make lint` works in a go.work checkout again.** It computed `GOTOOLCHAIN`
  from a `go list -m` that did not set `GOWORK=off`, so the workspace answered
  for tasks and td as well and the recipe expanded to three version words.
- **Test binaries no longer inherit the developer's `TMUX_PANE`**, which could
  silently drop a scripted pane from an inventory assertion after a tmux server
  restart renumbered panes.

## [v1.3.0] - 2026-08-21

### Features

- **First-run onboarding that names the next step.** Launching in a non-git
  directory with no configured projects opens Configuration → Add Project
  instead of an empty td monitor. Initialize this directory (or the Git tab)
  runs `git init -b main` after a click or `i`, then reloads git context
  without a restart.
- **In-app install for td and Tasks.** The td uninstalled view and
  Configuration → Panels offer a confirmed Install button (Homebrew, or
  `go install` if brew is missing). Sidecar never uses sudo and does not
  treat a package-manager exit as success. Diagnostics for missing standalone
  Tasks points at Panels rather than claiming Sidecar will not install it.
- **Empty states that point at the next action.** TD monitor empty panes
  explain how to create work; embedded Sidecar adds a Workspaces hint when
  there are no tasks yet. Workspaces first-run copy covers `n` / `+` once
  inventory has loaded. An unattached worktree preview is padded and has a
  Start Agent button that opens the create-workspace form.

### Bug Fixes

- **Create Workspace kind toggle shows the selected type.** Opening the form in
  Shell mode (or leaving focus on Name) no longer leaves both Shell and Worktree
  looking unselected; the current kind stays highlighted.
- **Note activity in the td monitor says "created note" / "updated note"**
  instead of treating notes as issues. (Requires a td build that includes the
  matching monitor change.)
- **Create Workspace kind row shows when it has keyboard focus.** The chosen
  kind stays filled; `[ ]` around the row uses the same Primary colour as a
  focused input so Tab on the toggle is visible without stealing the selection.

### Workspaces and panes

- **The terminal panel is a Shell leaf of the pane tree.** The panel's second,
  parallel split system is gone; shells, files, notes, issues, and resources are
  all leaves of one tree with one compositor, one border rule, and one divider
  renderer. Shell placement is duplicable, respects a live cap, and the sidebar
  shows layout badges. Pane titles are clickable to rename.
- **Split terminals belong to their own workspace.** A split's close ends its
  session and its confirmation says what will actually happen; a wedged split can
  be closed, a reused session honours `--run`, and split names are scoped to the
  owning workspace.
- **`sidecar create` CLI.** `sidecar create shell`, `sidecar create shell --split
  <dir>`, and `sidecar create worktree` let an agent open shells, splits, and
  worktrees in the running app, with worktree context, journalling, and ack
  matching.
- **Notes open as workspace content panes.** Notes lists, archive, search,
  formatting, and content links were polished together; the archive reloads the
  right pane with the selected note, and clicking below a note's last line lands
  the caret at the end of the text.
- **Per-tab close buttons** with a hover state and tighter tab labels.
- **Clickable links in Files panes**, symbolic git ranges recognized as diff
  links, and `claimHosts` so resource providers can claim built-in URL spans
  while keeping their OSC-8 hyperlinks.
- **Scrollbars** in Files fileview panes, workspace document panes, and the
  switcher/palette modals, which also gained stable height and mouse support.
- **opencode auto-approve** support in shell creation via `--auto`, derived from
  workspaceops rather than hardcoded per launcher.
- **Session copy ring** — paste-recent over ssh, plus undo aliases in notes.
- **Refreshed diagnostics info modal** layout, plugin table, and close button.

### Bug Fixes — panes, rendering, and input

- **Pane geometry is announced to plugins on resize, not every frame**
  (td-fcb03a), and wheel bursts are coalesced and paced across issue viewer
  surfaces and forwarded pane applications.
- **Canvas detection** survives the screen model's serialization, abstains on
  untouched default-background rows, and breaks ties on row starts
  (td-fb5a9d, td-b8c54e); Grok terminal backgrounds fill on large canvases.
- **Issue and document panes render markdown at full pane width** via the
  compact renderer contract, and padded viewers are paired with compact-document
  renderers (td-85a2be, td-65095b).
- **Clicking a commit tab's own header no longer strands it loading**, file panes
  no longer stick on Loading after a split, and plugin-switch keys stay live over
  a focused content pane.
- **Notes preview clicks are only stolen for real content links** (td-b57215),
  and the content pane header × hit rect covers the glyph.
- **Footer** drops the refresh stamp and aligns shortcut help (td-f6ad90,
  td-70f800).

### Build and tooling

- **Managed dev installs track workspace siblings** (td, tasks) through
  `go.work`, and `make install-status` reports the sibling revisions compiled in.
- **`scripts/demo.sh`** — modular, fully ephemeral demo environments (multi-project,
  single-project, and fresh onboarding presets) that build a fresh binary from the
  working tree and clean up on exit.

### Dependencies

- td v0.61.0 → v0.62.0 (monitor empty states, note activity verbs, embedded
  next-step, and the issue-modal CPU fixes).

## [v1.2.2] - 2026-08-20

### Bug Fixes

- **Create Workspace modal retains keyboard focus across fields.** Tabbing or
  clicking away from the Name field no longer snaps focus back to Name on intermediate
  view renders. Initial focus resolution now handles declarative modals before and
  after first paint without overwriting active field selection.
- **Retain workspace shell focus when switching plugins.**

## [v1.2.1] - 2026-08-20

### Bug Fixes

- **Text selection is easier to find against the canvas.** The highlight sat
  at 1.2–1.5:1 on every theme — a selected-row fill, not a span — so a drag
  in Notes, Files, or a document pane disappeared into the shell background.
  Selection now lifts to at least 2.65:1 against the canvas and stays on the
  same ink pole, so body text, headings, and syntax colours are not inverted
  or washed out. Curated tinted selections bring vibrant, theme-matched highlights
  while supporting chromatic contrast in normalizer calculations.

## [v1.2.0] - 2026-08-20

The footer toast is gone. Notifications stack in the corner, live in a centre
you can open from any surface, and take numbered jumps to the thing they name.
Clicking a file, a commit, a td id, or a note link opens a pane beside the
plugin you were already looking at, instead of yanking you away. Markdown
follows the active Sidecar theme. Notes is no longer a beta.

### Features

- **One Create Workspace form on both surfaces.** Project Workspaces and
  global Sessions share the same Shell | Worktree modal, with auto-approve
  and base branch on global as well as project. Instant-create shortcuts are
  unchanged: project `ctrl+n`, Shells `[+]`, and `autoCreateShell` still skip
  the form.
- **A notification system instead of a one-line footer echo.** Toasts are
  bordered blocks in the top-right of the content region: they stack, collapse
  by source, reveal a row at a time, and dismiss on click or `d`. The centre is
  an app-level right panel (`N` / `alt+n`) with per-source sections, unread
  dots, wheel scrolling, and a header indicator next to the gear. Settled agent
  lane transitions post here; so does anything that used to flash the footer.
  A quieter status-flash tier exists for things that should not take a toast
  slot. Expiry, stacking, and per-source behaviour live on the existing config
  screen.
- **`sidecar notify` for agents.** `post`, `list`, and `dismiss` write the same
  log the UI reads. A post appears as a toast in the running instance and stays
  in the centre until dismissed; with no instance running it is stored and
  shown at the next start. `--target kind:value[:line][@project]` attaches
  numbered calls to action (issue, task, commit, file, session, url), including
  a jump into another checkout.
- **Content links and passive panes on Files, Git, Notes, td, and Tasks.** A
  file path, `path:line`, td id, commit hash, or `sidecar://…` link underlines
  only when it can be activated in the current project. Clicking it keeps the
  active plugin on screen and opens the same Document, Issue, Diff, or Resource
  pane Workspaces already uses: first pane to the right, another kind stacked
  in that column, the same kind as a tab. Plugin content panes are on by
  default. Tab walks the plugin, then the panes, in visual order; `q`/`esc`
  hides the focused pane and `x` closes a tab.
- **Tab walks into the embedded td and Tasks panels.** Those two surfaces now
  name their own focus stops, so the app's tab ring can enter the monitor's
  current-work / list / activity panels and Tasks' list and detail regions
  instead of treating the whole plugin as one opaque box.
- **In-file search and inline edit in document panes**, on both the project
  workspace and the global Sessions browser. `/` searches the focused file
  (incremental, `n`/`N`, wrap-aware highlights); the same tmux-PTY editor the
  Files plugin uses now edits a doc pane in place. `ctrl+p` / `ctrl+f` reach
  the file finder from the browser as well.
- **Markdown follows the active Sidecar theme.** Headings, links, inline code,
  rules, and fenced blocks are derived from the current palette rather than
  Glamour's generic `dark` preset. Fenced code uses the same Chroma style as
  file previews. Switching a theme restyles Markdown that is already on screen
  without discarding scroll, selection, tabs, or search.
- **Notes is a stable project surface.** A project that has not run `td init`
  gets a setup path rather than a dead screen. Create and delete are optimistic,
  mouse editing is native (multi-click, click-after-EOL, pane-local `$EDITOR`
  forwarding), and the built-in editor honors Mac/Emacs keys plus a persisted
  Built-in / `$EDITOR` preference. Layout, filter, and save chrome are tighter;
  informal numbered outlines render as lists; edit padding matches the
  markdown view.

### Bug Fixes

- **`make install-worktree` / `make install-local` make `sidecar` on PATH
  run the build they just activated.** Homebrew is still the managed
  link; copies that win PATH (typically `~/go/bin` from unmanaged
  `make install`) are pointed at the same artifact so
  `make install-worktree && sidecar` is one build. `make install-status`
  still reports resolution without mutating it.
- **Unsafe internal link labels stay inert**, and only rectangles that are
  actually rendered as links are decorated — so a label that looks like a path
  or a td id inside chrome, search, or an editor is not clickable.
- **Absolute content paths survive a project switch**, so a pane opened by
  full path does not resolve against the wrong checkout.
- **Git full-file staging keeps its identity** when a content pane is open
  beside the Git plugin, instead of staging the wrong file because the pane
  and the list had drifted apart.
- **A path qualifier on a jump names a checkout, not a project**, so
  cross-project notification targets and `sidecar open` land in the worktree
  they named.
- **Git failure alerts are a headline**, not a dump, and the last bottom-status
  echoes in Notes and git-status are gone — those events are notifications.

### Internal

- `make lint` is the GitHub lint job: full codebase, `GOOS=linux`,
  `GOWORK=off`, golangci-lint v2.12.2. The old `--new-from-merge-base`
  gate missed unused leftovers whose function bodies were not edited.
- `internal/contentlink` owns link recognition and `sidecar://` routing;
  `internal/passivedeck` owns the app-level content deck both plugin hosts and
  the workspace surfaces bind to. There is still one compositor and one pane
  frame.
- Document search lives in `internal/docview`. Inline edit for Files, Notes,
  and doc panes shares `internal/inlineedit`.
- Cross-surface jumps (notification centre, content links, `sidecar open`) go
  through one state-free activation service, with a single pending-target slot
  so a project switch and a landing cannot race.

### Dependencies

- td v0.60.0 → **v0.61.0**. The embedded monitor exposes its three root panels
  as focus stops the host tab ring can enter.
- tasks v1.11.0 → **v1.12.0**. Same contract for Tasks' list and detail
  regions.

## [v1.1.1] - 2026-08-19

### Dependencies

- **`tasks` moved to v1.11.0**, two releases on from the v1.9.0 v1.1.0 shipped
  against. It brings three-part delegation (mode and note) through the data
  model, the CLI, the HTTP surface, and a delegate modal; a user-configurable
  delegation vocabulary; approving and completing a proposal in one step; and
  expanded relative date input.

### Fixes

- **A release can no longer ship against a stale sibling module.** v1.1.0 was
  cut with `tasks` pinned two minors behind and nothing noticed, because
  `go.work` resolves `td` and `tasks` to the local checkouts — so the pins in
  `go.mod` are the one thing a local build never exercises. The release
  preflight now reads those requirements with `GOWORK=off`, compares each
  `github.com/marcus/*` module against its newest published tag, and refuses to
  tag when any of them is behind. `make sync-deps` is the fix it points at: it
  pins every sibling to its latest release and tidies, so the correction is one
  command rather than a per-module `go get` the operator has to remember.

## [v1.1.0] - 2026-08-18

Text you can see is now text you can select and copy — on every surface, into
every clipboard — and the machinery behind panes gained two shared seams so
project and global workspaces stay one model rather than two lookalikes.

### Features

- **Text in a document pane is selectable on both surfaces.** A file pane could
  be read but not copied from. It is one `docview.Model` in two places — the
  project workspace and the global Workspaces browser — so the selection binds
  to the viewer once and both surfaces inherit it, the way live refresh already
  does. Rows are wrapped and tab-expanded at layout time, in the column space
  they are actually drawn in, so the columns a selection names are the columns
  on screen and the selection engine never sees wrapping. The line-number
  gutter is kept beside the text rather than inside it, so it can never be
  selected or copied.
- **Copies reach both clipboards, and copy-on-select is a choice.** Every copy
  used to end in a local clipboard utility, which does not exist over SSH or
  inside the tmux control-mode sessions the workspace surfaces run on — so
  copying put the text nowhere and reported success anyway. `internal/clip` is
  now the single path text takes: the system clipboard natively, and the
  terminal's own over OSC 52. Copy-on-select is a setting rather than an
  assumption, and one control owns it.
- **A `sidecar-modern` Chroma syntax theme**, with red keywords and teal types,
  plus easier-to-see text selection in that theme.

### Bug Fixes

- **The global Diff pane accepts file clicks and host shortcuts** (td-efbaa9).
  The Sessions Diff leaf registered file-row hits but never dispatched them,
  and always claimed leftover keys, so `@` never reached the project switcher.
  Click and wheel rules moved onto the shared `workspacediff.View`, and host
  globals now pass through when a content leaf is focused.
- **A shell created from the global browser stays live.** Creating one wrote
  the session and manifest, then the next live-only poll treated it as
  unclaimed and painted the preview "session ended" while tmux was still
  running (td-ecb0b8). Unclaimed sessions now fall through to path ownership,
  and an in-flight status pass cannot drop a project re-inventoried mid-cycle.
- **Live watches the kernel drops are re-added**, and a view that can never
  perform a re-read is no longer owed one.

### Internal

- `internal/livepanes` owns the live-refresh watcher lifecycle that had been
  written out once per pane kind per surface — six near-identical copies, and
  six chances for a new pane kind to silently never refresh. Adding a kind is
  now one `Binding` entry per surface.
- The selection engine lives outside the terminal package, so surfaces that are
  not a PTY can use it.
- `make worktree-init` lets any harness make a git worktree Go-buildable, and
  Sidecar-created worktrees run it automatically.

### Dependencies

- td v0.59.0 → **v0.60.0**. The embedded monitor's list, board, and swimlane
  panes now share one row layout, so keys and columns line up and selecting a
  row no longer shifts it.

## [v1.0.3] - 2026-08-18

Mostly internal groundwork — the embedded td monitor, the global Workspaces
browser, and the Notes editor — plus the fixes that fell out of it.

### Features

- **The embedded td monitor wears Sidecar's theme** (td-8d698b). The td tab used
  to render in td's own colors regardless of the active Sidecar theme. It now
  takes the host theme across every surface — lists, kanban and swimlanes,
  modals, forms, markdown, and the select filters — and follows a theme change
  live, without losing selection, scroll position, or filter text. Requires td
  v0.59.0.

### Bug Fixes

- **Shells another Sidecar created now appear on their own.** The global
  Workspaces poll is deliberately live-only: it refreshes tmux evidence for
  workspaces it already knows about and reads no durable state. A second Sidecar
  on the same host — a session over SSH or mosh — writes into the same state
  tree, so its shells simply did not show up until something forced a full
  cycle. Same host means same filesystem, so each configured project's
  `shells.json` is now watched rather than polled, and a signal re-fingerprints
  the manifests by content so only projects that actually changed are re-read.
- **Clicking a diff pane in global Workspaces no longer hands the keyboard
  back.** The click-away rule enumerated which leaf kinds count as a click
  *inside* the preview, and that list named only the doc and issue leaves. A
  press on a diff or resource leaf moved the focus ring onto it and was then
  treated as a click away: the diff drew idle, the list drew focused, and `j`/`k`
  moved rows. Tab-cycling never went through that path, so only the mouse was
  affected. The rule now asks the drawn pane tree — a press inside any
  non-terminal leaf belongs to that leaf — which cannot fall behind a leaf kind
  added later, the way this regressed twice before.
- **Shells are no longer mislabeled as Cursor.** Identification followed
  activity phrases rather than identity, and Codex's npm launcher is `node`, so
  prompts like "ctrl+c to stop" stole those panes. `defaultAgentType=cursor`
  compounded it: a plain zsh or a Grok session still read as Cursor. Live chips
  and Overview now show what the process actually is; the launch preference
  stays the restart default.
- **Terminal resource providers work on a fresh install.** A provider child was
  spawned with the Sidecar config directory as its cwd — a directory that is
  read but never created, so before config had been saved once it did not exist.
  A process whose cwd is missing does not fail diagnosably; it fails to spawn at
  all, so every terminal resource provider was silently dead. It now falls back
  to the system temp directory, which is equally neutral and always present.
- **A wrapped click in Notes' view mode lands on the word clicked.** Glamour's
  wrap breakpoints are not the textarea's, so adding the visual column to the
  wrap-segment start could put the caret on a different token. Paragraphs and
  headings now snap to the clicked word; fences stay at the top of the block and
  raw view is unchanged (td-28409a).

### Notes plugin

- **The built-in editor now has real selection and undo.** Shift-arrows (and
  `alt+s` when the terminal swallows shift) extend a source-coordinate
  selection that survives wrap and resize. `alt+a` selects all, `alt+c` copies
  the selection or the whole note, `alt+x` cuts, and typing, paste, or
  backspace replace the range as one undo unit. `ctrl+z` undoes and
  `ctrl+y` / `ctrl+shift+z` redo; short typing bursts coalesce. Bare `v` and
  `u` still type. The tmux editor keeps vim's own undo.
- **Saving no longer freezes Notes.** Autosave, leaving edit, navigation, and
  editor handoffs keep the UI responsive while td is busy. Slow and failed
  saves stay visible and retryable, content saves use one td command without a
  full-list reload, and shutdown checkpoints unsaved text before flushing it.

## [v1.0.2] - 2026-08-17

### Bug Fixes

- **A failed update now says why.** The failure modal rendered only the error,
  truncated to a single line, and discarded the failing command's output
  entirely — so a failed `go install` showed the command and nothing else, with
  even `exit status 1` cut off. A toolchain error, a network failure, and a
  checksum mismatch were indistinguishable. The modal now shows the error plus
  the tail of the command's output, where the compiler or toolchain message
  lives, wrapped to the modal width rather than truncated.

  Note for anyone updating from v1.0.0: the `go install` environment fix in
  v1.0.1 is run *by* the newer binary, so the v1.0.0 → v1.0.1 update could still
  fail. Install v1.0.1 or later once by hand and automated updates work from
  there on.

## [v1.0.1] - 2026-08-17

A performance and install-reliability patch on top of 1.0, plus a smoother
shell-rename flow.

### Performance

- **An idle Sidecar no longer burns a quarter of a core.** The git watcher was
  driving itself: the `git status` a refresh runs touches the index's attributes,
  which kqueue reported back as a change on `.git/index`, scheduling the next
  refresh forever. Attribute-only events are dropped now; real index writes
  (which arrive as a rename) still land in the panel within a second or two. On
  top of that, `git worktree list` no longer runs inline on the render goroutine
  every tick, and td's monitor honors `plugins.td-monitor.refreshInterval` — a
  setting that existed, was validated, and was never read — now defaulting to
  10s instead of polling every 2s. Idle subprocess spawns went from 1259/min to
  30/min and idle CPU from ~24% to unmeasurable.

### Bug Fixes

- **The in-app updater's `go install` now matches the manual one.** It inherited
  Sidecar's environment, so from a checkout with an active `go.work` it picked up
  local `replace` directives, and it carried whatever `CGO_CFLAGS` the machine
  had — either of which could fail the cgo build. It now runs with `GOWORK=off`
  and appends the warning suppressions to the user's own `CGO_CFLAGS`, and the
  manual command shown on screen reflects the same environment.
- **Renaming a shell applies live** rather than waiting for a restart, and the
  agent instruction copy describing it was updated to match.

## [v1.0.0] - 2026-08-17

Sidecar 1.0. The surfaces that were converging over the 0.9x series — the
project workspace and the global Sessions browser, the pane frame under both,
the CLI agents reach Sidecar through — are now one model with one set of rules,
and the configuration you needed a text editor for lives in the app.

### Features

- **Configuration, in the app.** A full Configuration surface with its own
  chrome, navigation, and search: Sidecar Setup, Diagnostics, Appearance,
  Projects, Workspaces, Agents, Terminal, Panels & Integrations, Advanced, and
  About. Readiness checks sit behind Setup and Diagnostics, each with a focused
  repair. Controls are real dropdowns rather than values that cycle, the theme
  picker previews live, and startup recovery routes a broken install to the page
  that fixes it.
- **Themes unified.** Sidecar Modern is the default, alongside 20 curated
  palettes. The theme system is one registry the app, the picker, and the
  website all read.
- **Global Workspaces reaches parity with the project view.** Create project
  shells and worktrees from the global browser, delete and merge from it, reveal
  from it, and get the same launch lifecycle, shell-liveness handling, and
  refusal rules. Both sidebars share one header (`[sort] [+]`), one age
  formatter, and a sort — by activity, recency, or name — that is remembered per
  project and reachable from the keyboard with `v`.
- **One pane frame under both surfaces.** `internal/panelayout` and
  `internal/paneframe` own pane structure, geometry, chrome, borders, the
  compositor, and hit regions; each surface binds to them in a single file. Drag
  handles work on every resizable split, live panes debounce their resize until
  the divider drops, and a unified focus ring follows the pointer and takes the
  keyboard.
- **Panes hold content, not modes.** Diff, Task, issue, and file leaves open
  beside the live terminal instead of replacing it, each with its own tab strip:
  click a tab, `{` / `}` to cycle, `x` to close. Project tabs persist per
  terminal surface; global tabs stay in memory for the selected row.
- **`sidecar open` works from anywhere.** Outside a Sidecar shell, against the
  workspace root, for multiple requests in one run, and into a Diff leaf
  (`--diff`, or by clicking an underlined git hash or range). It reports when an
  open was retargeted into an existing pane. `sidecar --agents` documents the
  whole surface for agents.
- **Live panes refresh themselves.** `internal/livewatch` keeps issue, document,
  and diff panes current without a keypress.
- **The file finder and project search moved into shared packages** and are now
  available inside workspace file panes, sized to the widths a real pane gives
  them.
- **Diff viewing grew up:** an extracted `workspacediff` viewer with its own keys
  and paging, commit-as-root tabs, section line totals, and a total commit count
  in Git.
- **Conversations gained a Grok adapter** for sessions and resume.
- **Notes runs on td's public `pkg/notes` API** rather than its own database
  handling.
- **A new homepage.** The marketing site is rebuilt around the app it is
  selling, with Sidecar's real chrome re-created as browser components and all
  21 themes live from the same data the binary uses.

### Changed

- Create New Worktree is a stable form: type a natural name, press Enter. Base
  branch, task, and agent are one-line Combo fields that overlay instead of
  stretching the modal. Git uses a slug of the display name, kept separate from
  the display name you typed.
- The header's hierarchy was redesigned, and cycling now works across the whole
  header.
- Inertial wheel scrolling has exact boundary answers on every surface and every
  modal kind, instead of forwarding past the edge.

### Bug Fixes

- Worktree tmux sessions key on path identity, so renaming a worktree no longer
  strands its session.
- Workspace terminal ownership is synchronized, closing a race that let a stale
  pane paint over a live one.
- Shell liveness is hardened against resurrection and wedged tmux.
- Clicked git specs resolve so a click and the CLI land on the same tab.
- Stale full-file paint is cleared on scope and file change; Diff tabs repaint
  through the host adapter.
- Files keeps its preview modes when closing tabs for a path, and closes the
  right ones.
- Worktree removal has one path, with force as an explicit caller intent, and
  probe deadlines are deterministic.

### Removed

- Prompt templates and the prompt picker. Task-linked agent launch still injects
  task context automatically.
- The worktree Output / Diff / Task tab row. Diff and Task are header action
  chips that insert a leaf; `d`, the chip, and `sidecar open --diff` are the
  paths into Diff.
- Link Task from the Create Worktree modal.

### Dependencies

- `github.com/marcus/td` v0.58.0 (public `pkg/notes` API)
- `github.com/marcus/tasks` v1.8.3

## [v0.99.1] - 2026-08-14

### Features

- Worktree terminals are terminals. The Output / Diff / Task tab row is gone.
  **Diff** and **Task** stay as header action chips that insert a leaf beside
  the live terminal. `,` / `.` no longer cycle worktree views; they cycle Diff
  target tabs only while a Diff leaf is focused. `d`, the Diff chip, and
  `sidecar open --diff` are the paths into Diff.
- `--disable-feature=workspace_doc_panes` now means no Diff: there is no pane
  tree, so `d` / the Diff chip / `sidecar open --diff` toast or no-op
  ("Document panes are disabled; Diff needs the workspace pane tree").
- File preview tabs are now clickable in both project and global Workspaces,
  with global previews retaining multiple open files instead of replacing the
  current one.
- Issue panes in project and global Workspaces keep one tab per open td id
  instead of replacing the current issue. Click a drawn tab to select it;
  `{` / `}` cycle; `x` closes the active tab. Project tabs persist per
  terminal surface (`q` hides, last `x` forgets). Global tabs stay in
  memory for the selected row and are not written to disk.

### Bug Fixes

- Project workspace tab clicks now select the intended file without being
  stolen by the terminal preview or pane divider.
- Global and project Workspaces once again share pane placement and resizing
  behavior, including stacked file and issue panes, draggable nested dividers,
  and per-workspace restoration of pane layouts and scroll positions.

## [v0.99.0] - 2026-08-14

### Features

- **Workspace previews are now a real pane system.** A terminal can sit beside
  nested document and td-issue panes, each placed and composed from one shared
  rectangle model. Clicking a file or issue opens it in that tree, repeated
  documents collect into tabs, focused panes can be closed or zoomed, and the
  full in-workspace layout survives restart (external files remain
  session-only). Small windows degrade to one focused pane without corrupting
  the saved tree.
- **Issues are useful inside the workspace, not only in a modal.** Issue panes
  share the existing fetch and rendering core, show the full issue card, scroll
  under mouse or keyboard control, and retarget in place when another td id is
  clicked.
- **Editing stays in Sidecar while each surface keeps the right editor.** Notes
  regain their lightweight editor, workspace documents can edit notes in their
  pane, and the file browser keeps its tmux-backed inline editor. Full tmux
  attach and the experimental terminal split are now explicit opt-ins instead
  of escape hatches that unexpectedly replace the Sidecar experience.

### Bug Fixes

- **Terminal scrolling now follows one rule everywhere.** Watched and live
  panes share scrollback keys, wheel routing, history access, gesture pinning,
  flick cadence, and shifted-key handling across project and global workspace
  surfaces. The reader's viewport is preserved when interaction ends.
- Pane geometry now reaches the tmux window even when a control-mode transport
  owns it, so opening a document or dragging a divider resizes and redraws the
  terminal immediately instead of waiting for a later click.
- Notes editing no longer leaks its command into other palettes, file-browser
  inline editing keeps its own input ownership, and preview/tab cleanup removes
  stale panes without leaving the selection on missing content.

### Dependencies

- Tasks embedding updated to v1.8.2, adding bounded store-lock waits and
  verifiable public-install provenance.

## [v0.98.0] - 2026-08-13

### Features

- **A global scope that spans every project.** Sidecar now has a second space
  above the per-project one, with its own tabs — Overview, Workspaces, and a
  hosted Tasks — reached without switching projects first. One shared workspace
  catalog feeds it, so every project's worktrees and shells appear in a single
  list that filters, sorts, and groups. Sorts carry section headers (Project /
  Recent), rows show kind, project, name, and age on one line, idle worktrees
  hide by default behind a sort/filter fly-out, chosen rows pin to the top, and
  the last global tab survives a restart. The header breadcrumb names the global
  Overview so the current space is never ambiguous.
- **Global workspaces are live, not just listed.** Selecting one previews its
  terminal read-only; entering hands it the keyboard, so an agent running in
  another project can be watched and driven from one place. Each row carries
  Output, Diff, and Task tabs, shells can be renamed in place, and Diff jumps
  to the target project's Git plugin with that project's checkout selected.
- **Document panes ship enabled.** Markdown mentioned by an agent opens beside
  the terminal in a real pane tree, with panes that persist and resize across
  restarts, `m` (and a clickable chip) toggling rendered against raw, and any
  local file an agent prints — not only markdown, not only paths inside the
  worktree — openable straight from the terminal by clicking its link.
- **Child shells nest under their parent worktree in the project sidebar**, so a
  worktree and the shells opened inside it read as one group.

### Bug Fixes

- **Terminal windowing is now one rule instead of several.** Placement,
  freezing, wheel notches, and selection geometry moved onto a single shared
  bottom-relative window model in `internal/tty`, fixing a family of drift bugs:
  panels that resized to the wrong height, a pin left frozen by an empty
  gesture, a notch that thawed a window mid-selection, clipped primary previews,
  and term-panel or editor chrome being mistaken for a worktree pane.
- Backslash hides the sidebar without dropping into the watched preview, `q`
  types normally in the global Rename Shell modal, Enter closes the View
  fly-out after applying a sort, and the Task tab's `m` no longer fires while a
  hidden doc pane holds focus.
- Workspace diffs load the first commit without moving the cursor, compare short
  against full hashes correctly when skipping commits, and no longer gate the
  global preview on capture freshness.

### Dependencies

- td updated to v0.57.0: reopening an issue now supersedes the stale approval,
  the review buckets that were classified but never drawn are painted, and the
  monitor's kanban stops crushing card width with empty columns.
- Tasks embedding updated to v1.8.0, bringing the redesigned Projects view,
  urgency bands, and the shared row vocabulary.

## [v0.97.0] - 2026-08-11

### Features
- **Conversations is opt-in via the `conversations_plugin` feature flag (default off).** The multi-agent session history tab no longer ships enabled. When the flag is off (the default), Sidecar does not register the plugin, does not construct history adapters, and does not read agent session stores — so there is no Detect/watch/load cost for users who do not need the tab. Re-enable with `"features": { "flags": { "conversations_plugin": true } }` in `~/.config/sidecar/config.json`, or `sidecar --enable-feature=conversations_plugin`. With the flag on, `plugins.conversations.enabled: false` remains a hard off-switch. Tab shortcut numbers for plugins after Conversations shift left when it is off.
- **The embedded Tasks tab is framed like every other plugin.** Sidecar draws
  the shared gradient panel around Tasks and renders the embedded model at the
  panel's interior size, shifting mouse coordinates past the frame so clicks,
  drags, and wheel events land where Tasks expects them. Unavailable and
  loading states stay unframed.
- **`sidecar shell name` reports the current shell's name from the manifest**,
  so an agent can check whether a stale name still describes its work. Naming
  guidance now asks agents to rename whenever a previous task's name no longer
  fits, and Grok launches receive the same rules.
- **Codex status detection is more accurate.**

### Performance
- **Git diff rendering cost no longer scales with file size.** Line truncation
  scanned a line rune-by-rune from the end and re-measured the whole candidate
  at each step, so a single 10KB minified JSON line took 577ms to truncate and a
  100KB line took about a minute — repeated for every visible line, every frame.
  Truncation now scans forward against a width budget (577ms → 1.4µs for a 10KB
  line; 2.5µs at 100KB, output unchanged across widths, CJK, emoji, and
  combining marks), truncation happens before syntax highlighting so chroma no
  longer tokenizes discarded bytes, max line number and side-by-side content
  width are memoized on the parsed diff, and side-by-side pair grouping is cached
  per hunk with above-viewport hunks skipped.

### Dependencies
- Tasks embedding updated to v1.7.0, adding ordered labelled task links and the
  multi-link picker.

## [v0.96.0] - 2026-08-11

### Features
- **The embedded Tasks tab adopts Tasks v1.6.0's refreshed visual system.** Every
  list now shares aligned priority and metadata columns, section rules and
  counts; Agenda uses calendar groups; and the detail rail adds clearer state,
  labels, actions, links, and subtask progress.
- **Agents can rename their current shell through a deterministic CLI.**
  `sidecar shell rename [--json] <name>` updates the Sidecar-owned display name
  without renaming the tmux session or terminal title. New shells publish their
  name through `SIDECAR_SHELL` / `SIDECAR_SHELL_NAME`, and supported harnesses
  are prompted to replace generated names with meaningful ones.
- **Overview completion means recently finished work.** Sidecar persists
  semantic activity transitions so newly completed agents stay visible as done
  without classifying every old idle shell as a fresh completion.
- **The Git sidebar shows section line totals and the repository's total commit
  count.** Async loads are request-ID guarded so stale history results cannot
  overwrite a newer repository view.
- **Brackets cycle top-level tabs consistently.** `[` and `]` now move through
  the assembled plugin order, with contextual plugin commands retaining
  precedence where they own those keys.
- **The update journey now covers every product Sidecar knows about, and tells the truth about each one.** Pressing `u` from diagnostics shows one confirmation naming every product that will change, with its current and target version and the install method Sidecar will use — `Sidecar 0.95.0 → 0.96.0 · Homebrew`. When the `tasks_plugin` feature is effectively enabled (config or CLI override, exactly as plugin assembly resolves it) and the standalone `tasks` command is installed, the Tasks suite joins Sidecar and td in that list. With the feature disabled, no Tasks check, process, or network request happens at all.

  Sidecar never installs Tasks just because the plugin is enabled: if the standalone commands are missing, diagnostics show `embedded only · standalone not installed` with the supported `brew install marcus/tap/tasks` command, and updating Sidecar refreshes only its embedded Tasks tab. A confirmation updates; it does not install a new product.

### Fixes
- **Terminal selections preserve application-painted background colours.** The
  overlay now composites selection styling without leaving harness-rendered
  backgrounds in the wrong state.
- **Each product now uses its own install provenance.** Sidecar previously reused its own detected install method to update td, which could run the wrong command on a mixed installation. Provenance is now resolved per product by checking whether the executable you actually run belongs to the formula (`brew --cellar`) or to your active Go bin directory — so a Homebrew Sidecar alongside a `go install`ed td, or an active local Tasks development selector, is handled correctly. An unrecognised install is reported with a manual command rather than overwritten.
- **A failure no longer erases earlier successes.** Targets run sequentially and each one's outcome is retained, so completion distinguishes `updated`, `already current`, `needs a manual update`, and `failed`, with a per-target manual command for anything that failed. Retry runs only the failed products.
- **Restart is only claimed when Sidecar itself changed.** A td- or Tasks-only update no longer asks you to quit Sidecar.
- **Verification compares exact versions, asking each binary in its own dialect.** Every binary a release ships must report the released version afterward — for Tasks that means `tasks`, `tasks-tui`, and `tasks-api` together, so a partially updated suite is a failure rather than a silent success. Success is never inferred from exit status or Homebrew wording alone.
- **The progress modal no longer offers a cancel it cannot honour.** It now shows which product is being changed and what already settled, and says `Update in progress`; the running package manager was never cancellable.

### Dependencies
- Updated the embedded Tasks TUI from v1.5.0 to v1.6.0.

## [v0.95.0] - 2026-08-10

### Features
- **Tasks tab (Beta), off by default.** Sidecar can now embed the [Tasks](https://github.com/marcus/tasks) TUI as a tab, sharing one application rather than reimplementing a second task tool. Tasks keeps ownership of its storage, rendering, overlays, and agent queue; Sidecar owns placement, lifecycle, keys, help, and the footer. Sidecar never reads or writes `tasks.jsonl` directly. Requires a configured Tasks install — an unconfigured or unreadable store shows a diagnostic and a setup hint rather than an empty list that looks authoritative.

  Enable it by adding the feature flag to `~/.config/sidecar/config.json`:

  ```json
  {
    "features": { "flags": { "tasks_plugin": true } }
  }
  ```

  The tab appears after Workspaces by default. To place it after Notes instead:

  ```json
  {
    "plugins": { "tasks": { "position": "after-notes" } }
  }
  ```

  Beta: the tab is usable day to day, but per-project context following, work search across td and Tasks, and workspace/capture links are still to come.

- **Contextual plugin keys take precedence over global keys.** Key input now resolves through a documented ladder — Sidecar modal, plugin text-input or blocking overlay, plugin contextual binding, Sidecar global, then unbound forwarding — so a plugin showing its own list decides what a key means while it is focused. Driven by two opt-in interfaces, `KeyRouter` and `FooterStatusProvider`; plugins that do not implement them behave exactly as before. `ctrl+c`, `q`, and `?` are host-reserved and can never be captured by a plugin. Recorded in `docs/adr/0001-contextual-plugin-keys-take-precedence.md`.

- **Tab order is data, not statement order.** Plugin registration moved out of `main.go` into `internal/plugins/assembly`, whose `Plan` is a pure function of config plus feature flags. Tab shortcut numbers are derived from the resulting order; nothing assumes a fixed number when preceding plugins are disabled.

- **The command palette carries commands that have no key.** A plugin can publish an invocable command without binding a key, and it still appears in `?` and the merged help. This is what lets Sidecar decline a key without making the underlying command unreachable.

### Bug Fixes
- **The footer no longer advertises keys that do something else.** Only bindings Sidecar will actually honour are registered, so a plugin key the host reserves or refuses cannot appear in the footer or merged help labelled as if it worked.
- **Registering a plugin binding twice is a no-op.** Binding tables grew on every project switch, since plugins re-register on model adoption while the keymap lives for the process.
- **A transient toast is no longer hidden by a persistent plugin status.** A plugin reporting a long-lived condition previously masked every toast for that tab, including update-and-restart notices.

### Dependencies
- Adds `github.com/marcus/tasks v1.5.0`, the embeddable Tasks TUI.
- Raises the `go` directive to 1.26.0 to match that module.
- `github.com/marcus/td` stays at v0.56.0 (already latest).

## [v0.94.2] - 2026-08-10

### Features
- **Copy terminal selections with `cmd+c`.** On terminals that deliver the macOS shortcut to Sidecar, `cmd+c` now copies selected text in both read and interactive modes; `alt+c` remains the portable and configurable alternative. Copying with no selection leaves the clipboard untouched and points to select-all. Super, Meta, and Hyper chords that tmux cannot faithfully encode are swallowed instead of typing stray characters into the pane.

### Bug Fixes
- **Terminal selection behaves like a native terminal.** A drag that begins on a passive terminal now selects immediately without activating, reframing, or jumping to the top of scrollback; a motionless click still activates on release. Double-click-and-drag extends by words, triple-click-and-drag extends by lines, shift-click extends to padding and chrome, and dragging beyond an edge continues through scrollback with bounded auto-scroll. Selection remains local even when the program inside the pane enables mouse tracking, while motionless clicks still reach that program.
- **Selection highlighting remains visible over application-painted backgrounds.** Multi-line selections no longer disappear on rows whose own ANSI styling sets or resets the background, including pinned-panel layouts, and the original row background is restored after the selected span.

## [v0.94.1] - 2026-08-09

### Bug Fixes
- **The Agent Overview owns the keyboard while it is open.** Opening the board over a workspace left in interactive mode no longer routes your keys into the hidden tmux pane: `esc`, `q`, and `K` close the board (`q` closes it rather than quitting sidecar), global shortcuts (`@`, `#`, `W`, `?`, `!`, `` ` ``, `~`, `1-9`) keep working over an embedded shell, and unhandled keys — including bracketed paste and unparsed CSI u sequences, which previously typed straight into the covered pane and could run on newline — stop at the Overview instead of reaching a plugin you cannot see. The `overview` context bindings are registered so `?` lists them, and the footer shows the board's own hints.
- **Option/Alt keys reach the pane as real terminal input.** Bubble Tea decodes a terminal's ESC prefix into ModAlt, so alt-modified keys arrived as a bare character and common bindings did nothing; the prefix is restored, making Option+Left/Right word motion (alt+b / alt+f) and Option+Backspace word deletion behave as they do in any terminal. Ctrl is preserved as the meta-control byte, and alt+shift+f stays `F` instead of collapsing to the base rune.
- **The terminal scrollbar column is reserved unconditionally**, so crossing the scrollback threshold no longer changes the width tmux wraps at — a frame published mid-resize rendered the pane one column too wide, clipping its final column and reflowing it as a blank wrapped line. (td-0818ef)
- **The commit-for-merge modal no longer dead-ends on a clean tree.** When an agent had just committed in the workspace, the lagging status snapshot let you press Commit against a clean index and hit git's "nothing to commit" as an unrecoverable modal error; the merge/PR workflow now proceeds with a toast explaining that nothing needed recording.
- **Quiet live shells read as `◎ live` rather than `● running`**, so a plain shell with a process but no agent activity no longer looks like busy work.

### Features
- **Agent chips match across surfaces.** The workspaces list and kanban reuse the Overview's themed agent presentation — icon, colour, raised fill — and Overview shell cards drop the redundant `tmux` prefix in favour of the session name.
- The marketing front page was updated to cover recent features.

## [v0.94.0] - 2026-08-09

### Features
- **The cross-project Agent Overview is on by default.** A single board shows every agent workspace across all your projects, not just the one you have open — click the logo or press `K` to reach it. Rows interleave workspace identity (project, branch, agent) with live inventory (activity state, diff size, PR status), and the board is colour-encoded by state so a glance tells you what is working, blocked, or waiting on you. Refreshes are request-ID'd and per-project trackers are isolated, so one slow project can no longer stall or corrupt another's row, stale navigation results are rejected instead of jumping you to the wrong pane, and sibling worktrees are matched to their real panes. The compact/narrow layouts keep the app header and active tab on one row. (td-1983cb, td-d2e415, td-5feb84, td-a2b217, td-2fb16f, td-a2ec37, td-897e31, td-47321d, td-20f583, td-7e8d0d, td-6b4657)
- **Terminals across the app share one model-first component.** Workspace panes, shell panes, and the files-plugin inline editor all run on the same terminal model instead of three divergent capture paths: absolute scrollback history is preserved, terminal generation is invalidated on resize, a blank pane falls back to a rendered snapshot rather than flickering empty during reseed, and pane input routing is consistent everywhere. The inline editor inherits all of it, and rejects stale async save results instead of applying them over newer edits. (td-b49d63, td-14a1df, td-d5df75, td-76f993, td-d267fe)
- **Workspace creation, merge, and PR lifecycles are recoverable and safe to interrupt.** A creation that fails partway preserves its partial state and can be resumed rather than leaving an orphaned worktree; merge preflight, post-create discovery, and other long git operations are cancelled cleanly when you move on, instead of landing their results on a workspace you have already left. Cleanup refuses unsafe targets, merge recovery is bound to the identity it started with, pull revalidates its target first, fork base topology resolves correctly, and containment refusals say plainly why they refused. (td-24d130, td-75360a, td-9ce7db, td-c90334, td-9ce314)
- **Diff status is authoritative.** The changed-file counts and diff strategy shown on a workspace card come from a captured strategy rather than being recomputed against whatever the tree looks like at render time, so branch commits show correctly and a phantom diff entry no longer appears.
- **Agent activity states animate**, and each provider gets its own spinner set, so a working agent reads as working at a glance instead of as a frozen frame.
- **Grok detection**, plus a round of general agent-detection accuracy fixes.

### Bug Fixes
- **Theme contrast is enforced on every surface text is drawn on**, so no theme can produce unreadable text on a coloured background; search match highlights are guaranteed a legible foreground.
- **Passive terminal follow is preserved on the live grid** — the Overview grid no longer steals follow from a terminal you are watching.
- The workspace terminal cursor is placed from a carried pane split, and the preview header collapses to a single row.
- Panel shortcut input is routed to the right panel; model-owned panel capture is guarded. (td-14a1df)
- Visible terminals are kept current rather than going stale behind the active pane.

### Developer
- **Releases fail closed on red Go CI** instead of relying on a manual checklist item.
- A byte-fed screen-model adapter, tmux fidelity harness, and shadow-comparison canary were built and evaluated against the tmux-owned path; the gate outcome is recorded as HOLD, and tmux retains ownership of bracketed paste at the input seam. (td-b7aa77, td-d7a24f)
- Isolated terminal proof harness hardened: bare control-client socket identity is verified, and the proof driver refuses to run against a shared server. (td-d7a24f)
- A Codex adapter, plus harness-change detection per terminal/workspace.

## [v0.93.0] - 2026-08-08

### Features
- **Agent activity detection is semantic, not just pattern-matched, and it now drives the kanban board.** Working/idle/blocked/done for Claude, Codex, Gemini, Grok, and Antigravity is classified by a real per-provider rules engine (`internal/agentactivity`) that reads pane titles, the current foreground process, and a bounded "current bottom" window of the screen — not raw scrollback, so resolved historical UI (an old prompt scrolled past) can no longer masquerade as live state. This activity tracker is now authoritative over the legacy tmux-render heuristic for every provider it supports, and the kanban board's lanes and card health/status text are driven from the same presentation function as the list view, so the two can no longer disagree. A worktree's health (missing folder, orphaned session, error, paused) always wins over activity, and a completed run is flagged **unseen** ("done") only on a genuine transition from working — restart or initial idle stays quiet, and idle observed only because a positively-identified process produced no rule match ("fallback idle") never itself creates an unseen-done badge. (td-8ac0a8, td-e48244, td-eef998, td-defc9c, td-725a7e, td-495065, td-f1b868, td-6e56a6, td-4954e5, td-22b195)
- **Five more agents get the same live activity detection: Pi, GitHub Copilot CLI, Cursor Agent, OpenCode, and Amp.** Each ships its own rule set (compatibility-tested against Herdr provider manifests) for working/blocked/idle, including recognizing that a help/history/settings overlay or an interrupted turn should retain the agent's last real state rather than reporting idle. Fallback idle — no rule matched a known-live process — no longer downgrades to a "done" completion badge for these five, only for the original four. Terminal-control snapshots (tmux control-mode) now carry pane title and current-command metadata end-to-end, so these probes get the identity they need without falling back to slower polling; the "fallback idle can never announce completion" rule stays scoped to expanded agents, and a plugin-wide fallback idle default is scoped to only the panes actually expanded on screen so a collapsed row can't borrow another row's read. (td-30281f, td-31ab2b, td-52abdf, td-a5670d, td-8625a6)
- **`make install-local` / `make install-worktree` / `make use-homebrew` manage a Homebrew-linked development binary safely.** `scripts/dev-install.sh` builds and swaps the linked `sidecar` binary in place, refuses to run `install-local` from a branch or a linked worktree (use `install-worktree` when that's deliberate) so an incidental checkout can't silently become "the" dev binary, and `make install-status` / `make test-dev-install` report and prove the switching logic against an isolated fake Homebrew prefix. `make install` remains the old unmanaged `go install` path. Mirrors the switching tool td just shipped. (td-d2466a)

### Bug Fixes
- **Git status parsing is hardened for large trees and copy/rename cases.** `git diff --numstat` is now parsed as NUL-delimited (`-z`) output instead of scanning tab-delimited lines with a regex, which broke on filenames containing tabs or newlines and matched staged/unstaged diff stats to entries by filename instead of by request; renamed-and-copied entries (`C`) are now distinguished from pure renames and staged/unstaged flags are read from both status columns instead of assuming a rename is always fully staged. Untracked-file line counting (a separate batched `wc` pass) was removed as redundant. Proven at scale against large working trees. (td-07bb70)
- **Git status snapshots can no longer be torn or stale by the time they render.** Every status/history/diff/preview load is now a request-ID'd, immutable snapshot: a load in flight when another refresh arrives no longer overwrites in-place fields the UI is mid-render against, a stale response is dropped instead of applied, and a refresh that arrives while one is already in flight is coalesced into a single dirty flag rather than double-fetching. Errors from a failed refresh surface as a toast instead of silently leaving the old tree on screen with no indication it's out of date. (td-403712)
- **The git file-system watcher tracks the actual administrative files git is touching, not a hardcoded `.git/` layout.** Paths are resolved with `git rev-parse --git-path` so linked worktrees (whose HEAD/index live outside the common `.git` directory) are watched correctly, `refs/` is walked recursively so nested ref directories are covered, and index-only churn versus HEAD/ref changes (which invalidate commit history) are classified precisely instead of both triggering a full refresh-and-reload. Stage/unstage operations are serialized through a single write executor with proper async result handling, replacing a debounce-and-batch shortcut. (td-ce6c26, td-ee1242)
- **Stage-all/unstage-all and other bulk git write operations run asynchronously** instead of blocking the UI thread, coalescing rapid repeated calls and reporting failures without corrupting the file list. (td-ee1242)
- **Stale startup and dead-code debt removed from the git plugin.** Sidecar's `.gitignore` scaffolding entries are now written only when the user explicitly initializes a new repo from Sidecar, not merely when opening an existing one; a stash-index placeholder that called `exec.Command("echo")` purely to satisfy an unused import was replaced with real index parsing; several unused stash helper functions were deleted. (td-c35648)
- **A second sidecar can no longer make your running shells disappear.** The shell manifest is a file every sidecar instance on the machine shares, and startup used to prune every entry whose tmux session it could not personally see — so a sibling worktree, or a test run on its own tmux socket, would quietly rewrite `shells.json` down to its own sessions. A live instance watching that file then dropped six shells from the Workspaces sidebar whose tmux sessions were still running, and `r` did not bring them back. Persisted definitions now record which tmux server owns them, absence is only treated as death when this instance could actually have discovered the session, in-memory shells that are alive locally survive any manifest another instance writes (and are healed back into the file), and `r` re-runs discovery so surviving sessions come back without restarting the app or hand-editing JSON. (td-8d18de)

- **A shell that comes back to life is usable again.** When a session that had gone missing reappeared, the sidebar row switched from offline to live but never regained its terminal, so opening it silently did nothing until the app was restarted. Reviving a shell now rebuilds its session and starts polling it. Related: a sibling worktree's shells are no longer listed in your sidebar as offline rows you could never open (they are still preserved in the shared file), a failed `tmux list-sessions` is no longer read as "nothing is running" — nothing is pruned when tmux cannot be reached — and shell manifest edits merge with what is on disk instead of overwriting a rename another instance just made. (td-8d18de)

### Developer
- **`-config` now also relocates `state.json` and `debug.log`.** They live next to `config.json`, so `sidecar -config ~/somewhere/sidecar.json` reads and writes its persisted UI state and debug log from `~/somewhere/` rather than `~/.config/sidecar/`. This is what makes `-config` a real isolation lever for proof runs. If you pass `-config` in normal use and want your existing preferences, move `state.json` next to the config file you point at. (td-8d18de)
- **`go test` is isolated from the real state tree by default.** Any test binary is treated as asserting isolation, so a package that forgets to redirect `XDG_STATE_HOME` now gets a refusal instead of quietly creating directories in `~/.local/state/sidecar/projects`. A test that deliberately exercises the ordinary unisolated path sets `SIDECAR_ALLOW_REAL_STATE=1` (with `HOME` pointed at a temp dir). (td-8d18de)
- **Proof runs are isolated on both axes, and fail closed if they are not.** "Never restart the default tmux server" turned out to be necessary but not sufficient: the run that caused the bug above held a private tmux socket and still resolved the developer's real `~/.local/state/sidecar`. Sidecar now refuses to read or write anything under the real state or config tree when `SIDECAR_ISOLATED_STATE=1` is set, exiting at startup rather than touching a byte; `SIDECAR_DIAG_PATHS=1` (implied under asserted isolation) prints the resolved state, config, tmux socket and manifest paths. `scripts/tmux-drive.sh` now isolates `TMUX_TMPDIR`, `XDG_STATE_HOME`, `XDG_CACHE_HOME` and `-config` under one run root, adds a `paths` subcommand, and tears the inner server down by explicit socket path; `scripts/tmux-screenshot.sh` refuses to run un-isolated, and `tmux-drive.sh start` refuses to take over a run root another driver is already using (set `SIDECAR_DRIVE_RUN_DIR` per agent, or `SIDECAR_DRIVE_FORCE=1`). A cross-instance regression suite proves an isolated instance cannot evict a peer's live shells or write the real manifest, and the headless-testing guide, `AGENTS.md` and `PRIVACY.md` now document both isolation axes (including that `XDG_CONFIG_HOME` moves nothing). (td-8d18de)

### Dependencies
- Updated td to v0.56.0. Review stamps cleared by superseded-approval handling now sync between machines instead of being stranded on the clearing client. The monitor displays timestamps and due/defer dates in local time with civil-day math, so a DST transition can no longer mislabel tomorrow as today. Sidecar needed no change for this.

## [v0.92.0] - 2026-08-06

### Features
- **Grok is a first-class workspace agent.** Create and start workspaces with Grok (`grok`, skip-permissions via `--always-approve`) alongside Claude, Codex, and the rest. Configure which agents appear and in what order with `plugins.workspace.agents` (for example `["grok", "claude"]` to hide Copilot and put Grok first); create/start pickers, defaults, and shell lists all honor the allowlist, and an unknown default falls back to the first listed agent.
- **Embedded terminals render at their true pane size.** Tmux panes are sized and mapped through hit testing so the program inside lays out for the visible area rather than a mismatched rectangle; letterbox padding no longer steals editor clicks.
- **Focus-gated ownership lease for shared tmux geometry.** When several sidecar instances (or a sidecar and an attached client) share a session, geometry ownership follows where the user is — focus and attach hold ticks with bounded refresh, dead leases are reclaimed, and an attach no longer claims permanent authority. Held sessions read input back out of tmux so a background holder does not starve the active client.

### Bug Fixes
- **Global shortcuts show up in the command palette.** Hard-coded app bindings (`i`, `W`, `#`, `r`, `ctrl+c`) are registered in the keymap so discovery matches what the app actually handles.
- **Open Issue Enter uses the sole search result** when the cursor has not been moved, instead of submitting a partial typed query.
- **The agent config picker list is pinned for the modal lifetime** so a config reload cannot reshuffle options under the cursor mid-choice.
- **Modals take keyboard focus from a live terminal** so typing goes to the dialog rather than the session underneath.
- Terminal view and cursor stay on one pane-row anchor; geometry decisions use user presence rather than focus alone; failed marker reads are treated as absence of evidence rather than proof of a hold.

### Developer
- **Streamlined releases.** `RELEASE_VERSION=vX.Y.Z make release` fail-closed-preflights (clean tree, live `main`, changelog entry, no `replace`, tag absence), pushes an annotated tag, waits for CI, and verifies or resumes the Homebrew formula. CI runs verify → GoReleaser → template-rendered `Formula/sidecar.rb` with downgrade/idempotency/race guards. See `docs/releasing.md`.

## [v0.91.0] - 2026-08-01

### Features
- **Clicking the shell or agent output makes the terminal live.** A click focused the pane but left it inert — entering interactive mode needed a separate `enter`/`i`/`E` press, so the pane looked active while typing went nowhere, and the wheel paged sidecar's own capture buffer instead of reaching the running app, which meant scrollback in TUIs like the Claude Code harness ignored it entirely. A plain click now enters interactive mode, matching `enter`; shift and alt clicks still fall through to read-mode drag selection, and the click is a no-op when there is no live session.
- **Drag a row in the files tree to move the file or folder.** Press on a row and move two cells in any direction to pick it up; drop it on a directory row to move it there, or on a file row to move it alongside that file, the way Finder and VS Code behave. Resting over a collapsed folder for a moment springs it open so one gesture can reach a nested destination, dragging to the top or bottom of the pane scrolls the tree, and `esc` cancels a gesture in flight. Moves that would corrupt the tree — a folder into itself or into its own subtree — and moves that would do nothing are refused with a toast saying why, and the same rules now back the keyboard `m` dialog, which previously reached `os.Rename` and reported a raw `invalid argument`. Drag-to-move is enabled for everyone: a press that moves less than two cells is still an ordinary click. There is no undo, so nothing moves unless the row was on screen when the button was released.

### Bug Fixes
- **Clicks in the files plugin land on the row you are looking at while an input bar is open.** The tree's clickable rows were registered one row too high per open bar, and the move dialog's path-suggestion dropdown was not counted at all — so with three suggestions showing, every row was off by five. The same stale measurement shifted click and drag-selection in the preview pane by a line, and put the suggestion list's own click targets a row above the suggestions they named. Keyboard tree scrolling used a third, different measurement, which could leave the cursor on a row that was never drawn.
- **Mouse scrolling and clicks work in the td plugin again.** td's monitor computes its panel bounds only when it receives a window-size message, and since the monitor model is now built asynchronously it is adopted after the app has already broadcast that size — so the new model never saw one. Panel bounds stayed empty, every hit test missed, and the wheel and clicks silently did nothing until the terminal was resized.
- **The files tree scrollbar stays in its own column.** It jumped left to hug the longest filename whenever no full-width highlighted row was on screen — during a drag, and any time the cursor was scrolled out of view.
- **Manual search inputs accept spaces.**

### Dependencies
- Updated td to v0.55.0. Log rows written as side effects — the progress note `td unstart` records when releasing a claim, and the auto-cascade and auto-unblock notes — now sync between machines instead of being stranded forever on the machine that created them. A transition to `open` releases the implementer claim on every surface, including the API, which had never received the earlier reopen and unblock fixes. `ERROR:` and `Warning:` diagnostics moved from stdout to stderr so `--json` output is parseable. Sidecar needed no change for this and quietly benefits: the issue preview and search commands unmarshal td's stdout directly, which an error line used to corrupt, and their error extraction already fell back to stderr.

## [v0.90.0] - 2026-07-31

### Features
- **The file browser now tracks filesystem changes as they happen.** Sidecar watches the project root and expanded directories, coalesces bursts of creates, deletes, renames, and writes, and refreshes the tree without waiting for the tab to lose and regain focus. Changes also invalidate background file previews, quick-open results, and path-completion data so every file-browser surface stays aligned with the disk. The watcher is bounded to avoid excessive file descriptors on macOS and is enabled by default behind the `files_auto_refresh` feature flag.

### Performance
- Tree rebuilds, quick-open scans, and path-completion scans now run outside the UI update path while the previous results remain visible. Navigating to a known file descends only through the directories in that path instead of loading the entire project tree.

### Bug Fixes
- Async tree refreshes preserve directories expanded and files selected while a scan is running, reject stale results, and continue listening while the inline editor, search, or a modal is open. Recreated directories are re-watched, project switches stop the old watcher, and cache scans can no longer clear one another's dirty state.

### Dependencies
- Updated td to v0.54.0, adding review attribution and sync support along with monitor input, CLI error reporting, and terminal-output sanitization fixes.

## [v0.89.0] - 2026-07-30

### Features
- **The terminal window and tab are named after the active project.** With several sidecars open at once, the tab bar now says which is which — `sidecar`, `td [charm]` — instead of leaving every tab labelled by the shell, and the name follows along when you switch projects or worktrees from inside sidecar. The format is configurable through the new `ui.terminalTitle` template (`{project}`, `{worktree}`, `{plugin}`, `{dir}`); set it to `""` to leave the title untouched, and outside a git repository it falls back to the directory name rather than clearing what your shell set. The previous title is restored on exit in terminals that support the title stack, and a title set by an editor or an attached session is taken back when you return. Under tmux this sets the pane title — see the docs for surfacing it in the status line.

## [v0.88.2] - 2026-07-28

### Bug Fixes
- **Scrolling an embedded terminal scrolls the program running in it.** When the program has enabled mouse tracking — Claude Code, vim with `mouse=a`, htop — wheel notches now reach it as SGR mouse reports, the way any other terminal emulator delivers them. Previously every notch scrolled sidecar's own view of the captured pane instead, which was worst for full-screen programs: they draw their own scrollback inside the pane and leave tmux's history empty, so scrolling slid the viewport across the live frame and left the layout looking torn. Programs that track no mouse are unaffected and keep scrolling the captured scrollback, and `alt`+wheel still scrolls it for programs that do.

## [v0.88.1] - 2026-07-27

### Bug Fixes
- **`E` enters interactive mode again.** The key was listed in the keymap and advertised by both the preview hint line and the command palette, but nothing handled it — so it did nothing, and because the workspaces tab kept interpreting keys, whatever you typed next was read as workspace shortcuts (typing `whoami` after it opened the issue picker). `enter` was and remains the primary binding.

### Dependencies
- Updated td to v0.53.0, which fixes several issue-query bugs.

## [v0.88.0] - 2026-07-27

### Features
- **Embedded terminals are driven by tmux control mode.** Sidecar keeps a control connection to the visible terminal and re-renders when tmux says the screen changed, instead of capturing it on a timer. The polling path is retained and resumes automatically whenever control mode cannot start or its client dies.
- **Embedded terminals use the host terminal's own cursor** rather than a drawn block, so shape, blink, and hidden-cursor states match what the program running inside actually asked for.
- **Scroll back through the full tmux scrollback.** Older ranges are fetched on demand as you scroll past what is loaded; the hint line reports how many older lines remain and how far back you are from the live edge.
- **Search terminal scrollback.** Press `/` (or `ctrl+shift+f` while interactive) to search, `n`/`N` to step through matches, `esc` to clear. Search covers the complete tmux history, not only the ranges you happen to have scrolled through.
- **Select terminal output with the mouse.** Drag to select, hold alt for a rectangular selection, double-click for a word, triple-click for a line, `ctrl+a` for everything. Set `plugins.workspace.copyOnSelect` to copy as soon as a drag ends.
- **URLs in terminal output are underlined and clickable.** Hyperlink escape sequences present in the output itself are stripped and re-synthesized only for URLs that pass a safety check, so untrusted output cannot make a link point somewhere other than the text it displays.
- **`ctrl+n` creates a shell session directly from the workspaces tab**, skipping the type-selector modal. It uses `plugins.workspace.defaultAgentType` when one is configured and creates a plain shell otherwise; it never passes an agent's skip-permissions flag, so use `n` → Shell when you want that. The hint appears in the workspaces footer next to **New**.
- **New `plugins.workspace.autoCreateShell` setting (default off).** With it enabled, the first time you focus the workspaces tab in a session sidecar creates a shell for you, so a terminal is ready for quick tasks without going through the create modal. It only fires when no shell sessions exist — shells whose tmux sessions survived a restart are reattached as before — and it leaves the sidebar selection where it was rather than jumping to the new shell. Nothing is created until the tab is focused, so startup is unaffected when you open on another tab.

### Bug Fixes
- **Typing fast in an embedded terminal no longer transposes characters.** Every keypress returned its own command, and those run concurrently — so two keys typed in quick succession raced into tmux and could arrive out of order (`whoami` landing as `whoaim`). Keystrokes now go through a per-session queue that fixes their order where the key is handled, not where its command happens to be scheduled. Pastes share the queue, so pasted text can no longer interleave with surrounding keys.
- **The terminal cursor is drawn on the line you are typing on.** The rendered buffer is scrollback followed by the pane's rows, but the cursor's row was mapped straight onto a display row as if the buffer's tail were the pane — so the cursor floated above the live line by however much scrollback the capture carried. On a fresh shell that was one row; after quitting a full-screen program like vim it was many, which is the "cursor stays at the top of the terminal while I type further down" report.
- **The terminal scrollbar sits at the right edge of the pane.** It was aligned to the longest rendered line instead, so on a mostly-empty terminal it appeared right after the shell prompt and crept rightwards as you typed. Full-width lines could also push the joined block past the pane and wrap it, shifting every row down by one.
- **Interactive mode shows a cursor when entered from the sidebar.** The native cursor and cell-motion mouse reporting were gated on the preview pane being active, and entering interactive mode left the sidebar active — so the session ran with no visible cursor at all. The previous pane is restored on exit.
- **Editors opened in a workspace shell get the full pane.** Shell sessions were created without a size, so tmux used its 80x24 default and anything started before the follow-up resize laid itself out for 24 rows.
- **The inline file editor gets the height it is displayed in.** Both the file browser's and the notes' inline editors created their tmux session at the whole plugin rect while rendering into a viewport four to five rows shorter, pushing vim's status and command lines out of view.
- Terminal selection highlights and search matches stay on their lines once older scrollback has been loaded. The renderer could be handed a base of 0 for a buffer that actually started further down tmux's history, offsetting every highlight by the amount of loaded scrollback.
- The plugin active at startup is now marked focused. `SetFocused` only ran when switching tabs, so whichever tab sidecar opened on reported itself unfocused — which, among other things, made it poll its tmux sessions at the slower background interval until you navigated away and back.
- Footer hints with equal priority no longer sort nondeterministically; they now follow the order the plugin declares them in.

### Developer
- Added `scripts/tmux-drive.sh`, which runs sidecar on its own tmux socket, sends it keystrokes, and captures the screen as text and PNG — the terminal fixes above were all reproduced and verified this way, without a human at the keyboard. `docs/guides/headless-testing.md` covers it, including how to see the native cursor (which `capture-pane` cannot) and the tmux coordinate spaces the terminal code works in. `AGENTS.md` is now the single entry point for agent instructions.

### Interface
- Removed the ASCII tree from the main worktree preview. The pane now leads with the text and carries hints for both `n` (new workspace) and `ctrl+n` (new shell).

## [v0.87.1] - 2026-07-24

### Bug Fixes
- **Claude Code and Cursor sessions no longer go missing when the project sits under a symlinked path.** Both adapters locate sessions by deriving a directory name from the project path — Claude Code by substituting characters, Cursor by hashing the string — and neither resolved symlinks first. Sidecar passes the main worktree path reported by `git worktree list`, which *is* resolved, while both tools record the working directory as the shell saw it. Where those disagree the lookup landed on a name that does not exist and the conversations tab reported no sessions at all. Both adapters now try the path as given and its resolved form, matching what the Warp, Kiro, Pi, and Pi Agent adapters already did.
- The Claude Code adapter now honors `CLAUDE_CONFIG_DIR`. Claude Code uses it to relocate its config directory; sidecar only looked in `~/.config/claude/projects` and `~/.claude/projects`, so a relocated install appeared to have no sessions.

## [v0.87.0] - 2026-07-24

### Performance
- **Startup is roughly 30x faster.** Time to a usable first frame went from ~4.3s to ~0.1s on the machine this was profiled on. The gain is larger on machines running an endpoint security agent (CrowdStrike and similar), where every file open and process spawn carries a fixed tax and the old startup path did a great many of both.
  - Conversation adapter detection no longer runs before the first frame. It used to probe all twelve adapters serially inside plugin init, and probing Codex alone walks every rollout file in `~/.codex/sessions` — 3.5s against a few thousand sessions. Detection now runs asynchronously and probes adapters concurrently, so it costs the slowest adapter rather than the sum, and the tab shows its loading skeleton until results arrive.
  - The Codex, Pi, and Amp adapters probe session files newest-first and stop at the first match, so opening a recently used project reads a handful of files instead of the whole history.
  - The td tab builds its embedded monitor (which opens the task database) asynchronously instead of during init.
  - Terminal graphics-protocol detection no longer queries the terminal at startup. The queries cost ~0.5s in escape-sequence round-trips and two tmux subprocess spawns, and nothing consumed the result — image previews always render with Halfblocks. The protocol is now read from environment variables.
  - Dropped redundant `git rev-parse --git-dir` prechecks before `git worktree list` and `git remote get-url`, halving the subprocess spawns on those paths.

### Features
- Added `SIDECAR_STARTUP_TRACE` for diagnosing slow launches. Set it to `stderr` to print a phase-by-phase startup timeline (or `1` to write it to the debug log), and `SIDECAR_STARTUP_TRACE_DELAY` to dump the trace later so asynchronous startup work is included.

### Bug Fixes
- Fixed a potential crash from concurrent access to the OpenCode adapter's project index, which was reachable whenever session loading and file watching ran at the same time.
- A panicking conversation adapter now disables just that adapter instead of taking down the application.

### Dependencies
- bump td to v0.51.2 — local session state store, worktree identity on sessions, session keying by context ID for sub-agent independence, `--json` output across the CLI mutation commands, project slugs and invitations, plus fixes for sync conflict false-positives, handoff autosync stalls, and SQLite timestamp serialization.

## [v0.86.0] - 2026-06-17

### Features
- Add an Oh My Pi (OMP) conversation adapter for sessions stored under `~/.omp/agent/sessions`, reusing the Pi Agent JSONL parser with OMP's current project-path encoding.

### Bug Fixes
- Preserve custom Pi Agent-compatible adapter identity on returned sessions, so OMP sessions are labeled as OMP instead of Pi Agent in conversation lists and targeted refreshes.
- Resolve symlinks when matching Pi Agent project session directories, fixing macOS paths such as `/tmp` resolving to `/private/tmp`.

### Dependencies
- bump td to v0.47.2 — normalizes issue IDs before FK-constrained writes.

## [v0.85.1] - 2026-06-17

### Bug Fixes
- **td monitor: capital-letter keyboard shortcuts now work again.** After the v0.85.0 Charmbracelet v2 upgrade, `Y` (copy task ID) and every other shift-bound shortcut in the embedded td monitor silently did nothing. In Bubble Tea v2 a shifted printable key arrives as the unshifted code plus a shift modifier, so td's keymap matched against `"shift+y"` instead of `"Y"`. Fixed upstream in td and pulled in via the dependency bump below.

### Dependencies
- bump td to v0.47.1 — fixes the capital-letter shortcut regression in the monitor keymap (`KeyToString` now uses `Key.String()`, mirroring sidecar's own keymap fix).

## [v0.85.0] - 2026-06-08

### Dependencies
- **Charmbracelet v2 migration**: upgraded to the `charm.land` v2 stack — lipgloss v2.0.3, bubbletea v2.0.7, bubbles v2.1.0, glamour v2.0.0 (huh v2.0.3 transitively, via td). Bubble Tea v2 ships a faster renderer and a declarative `tea.View`; lipgloss colors moved to `image/color.Color`; key/mouse handling moved to the v2 message model (`KeyPressMsg`, `MouseClickMsg`/`MouseWheelMsg`). No intended behavior change — a mechanical migration verified against the full test suite.
- bump td to v0.45.0 — itself rebuilt on the Charmbracelet v2 stack, so sidecar's embedded td monitor sub-model now shares the same v2 `tea.*`/`lipgloss.*` types. The local development `replace` directive was removed.

## [v0.84.0] - 2026-04-18

### Dependencies
- bump td to v0.44.0 — SQLite configuration stabilization: `foreign_keys=ON` enforced on CLI `issues.db` with migration 30 orphan cleanup + schema `ON DELETE CASCADE` on child relations; centralized SQLite opener (`OpenSQLite`) with uniform pragmas; PASSIVE WAL checkpoints on `Close`; removed redundant manual cascade emulation in `internal/sync/events.go`; new `td doctor fk` orphan audit (gated by `TD_FEATURE_SYNC_CLI=1`); new `TD_MONITOR_DBPOOL_DEBUG` env var for monitor connection-leak tracing; `td import` tolerates forward-referencing deps via per-transaction FK toggle

## [v0.83.0] - 2026-03-24

### Features

- feat: full-file diff view with minimap (#232)
- feat: two-pane diff tab with drag-to-resize (#232)
- feat: git sidebar line stats (#232)
- feat: split terminal panel with Ctrl+T toggle (#232)

## [v0.82.0] - 2026-03-24

### Dependencies
- bump td to v0.43.0 — 24 bug fixes: atomic lossless import, UpdateIssue missing fields, timezone-aware defer/due filtering, RemoveDependencyLogged wrong depID, DeleteBoardLogged atomicity, RateLimiter goroutine leak, CORS missing methods, snapshot stat error, DB connection leaks, form scroll over-run, modal click detection, in-progress panel header count, RFC3339Nano sync parsing, sess nil guard, escapeJSON completeness, stdin pipe size guard, trusted proxy XFF spoofing, CreateUser admin TOCTOU race, backfill false positive, autosync tx leak, singleflight snapshot dedup

## [v0.80.0] - 2026-03-21

### Dependencies
- bump td to v0.42.2 — 14 bug fixes: SSE nil-validator panic, work_session_issues sync, non-atomic undo, rows.Err() propagation, timestamp parse, non-transactional migration, snapshot race, StatusFilter/board editor data races, stale syncState, CLI reject validation, HelpFilter UTF-8, preview count, copyFile durability

## [v0.79.0] - 2026-03-20

### Dependencies
- chore: bump td to v0.42.1 — fixes lossless import round-trip for all issue fields and associated data (#64)

## [v0.78.0] - 2026-03-09

### Features

- Agent-powered PR description generation in merge workflow (#167)
- Configurable default agent type and `.sidecar-agent-start` override per worktree (#198)
- OpenCode adapter backward-compatible conversation updates (#199)

### Bug Fixes

- Prevent tmux server exit when all sessions are killed
- Fix centralized agent file tests to use correct storage path

### Dependencies

- Bump td to v0.42.0

## [v0.77.0] - 2026-02-28

### Features

- Add GitHub Copilot CLI adapter for conversations plugin

### Bug Fixes

- Restore mobile sidebar nav visibility (#220)
- Resolve worktree path from ProjectRoot, not WorkDir (#174) (#218)
- Fix task title truncation in td view (#215) (#217)

### Documentation

- Worktree setup hooks (#219)

### Developer

- Add pre-commit hooks for gofmt/vet/build (#216)

## [v0.76.0] - 2026-02-27

### Features

- Add Amp agent support for workspaces (#195)
- Make PR URL clickable and add yank shortcut in merge modal (#164)

### Bug Fixes

- Resolve CI lint and test failures (#214)
- Remove stale sessionIndex references in opencode adapter tests (#212)
- Add all sidecar state files to .gitignore on init and startup (#211)

### Dependencies

- Updated td to v0.39.0

## [v0.75.0] - 2026-02-26

### Features

- Open In modal to open projects in IDEs (#200)
- Centralized project data storage under `~/.local/state/sidecar` with migration (#197)
- Detect `.todos` file vs directory conflict in onboarding (#206)
- Agent restart uses chosen agent rather than defaulting to Claude (#192)

### Bug Fixes

- Use standard diagnostic status strings for fsnotify watcher (#158)
- Hierarchical branch crash, git hangs, and adapter timeouts (#136)
- Git init bug in test, remove dead kiro code (#148)
- Opencode sandbox path matching for bare-repo worktrees (#204)

## [v0.74.1] - 2026-02-19

### Bug Fixes

- Fix git locking errors in background operations by using `--no-optional-locks` (PR #186 by @borisvu)
- Fix tmux agent reconnection type handling (PR #150 by @jacola)

### Added

- MIT LICENSE file (Issue #182)

### Dependencies

- Updated td to v0.38.0 — approval workflow fix

## [v0.73.1] - 2026-02-15

### Bug Fixes

- Shift+Enter and semicolon key fix for Ghostty terminal (PR #171 by @boozedog)

## [v0.73.0] - 2026-02-15

### Features

- Kanban board view — press V in board context for overlay, f to toggle fullscreen
- 7 status columns: Review, Rework, WIP, Ready, P.Review, Blocked, Closed
- Per-column independent scrolling with scroll indicators
- Form autofill/autocomplete for Parent Epic and Dependencies fields
- InProgress and PendingReview now have distinct categories (previously lumped into Ready)

### Dependencies

- Updated td to v0.37.0

## [v0.72.0] - 2026-02-14

### Dependencies

- Updated td to v0.35.0 — adds GTD-style deferral (td defer, td due, temporal list filters, monitor modal defer/due display)

## [v0.71.1] - 2026-02-10

### Bug Fixes

- Prevent refreshSessions from overwriting concurrent session list updates
- Improve update error modal UX with actionable info
- Detect brew upgrade false-positive "already installed" response
- Add `brew update` before `brew upgrade` in self-update flow

## [v0.71.0] - 2026-02-09

### Features

- **Pi Adapter**: View Pi AI agent sessions (OpenClaw) in the conversations plugin, with session classification (interactive, cron, system), source channel detection, and CWD-based project matching

### Documentation

- Add Pi to supported agents on website homepage, conversations plugin docs, and intro page

### Dependencies

- Update td to v0.33.0

## [v0.70.0] - 2026-02-08

### Features

- **Amp Code Adapter**: View Amp Code IDE threads in the conversations plugin, with token usage, tool calls, and project matching
- **Kiro CLI Adapter**: View Kiro CLI conversations from SQLite storage, with message parsing and project detection

## [v0.69.1] - 2026-02-08

### Bug Fixes

- Fix file search (`/`) hanging on large projects by reusing Ctrl+P file cache with fuzzy matching (#107)

### Dependencies

- Update td to v0.32.0

## [v0.69.0] - 2026-02-08

### Features

- **Homebrew Formula**: Install via `brew install marcus/tap/sidecar` — builds from source for native Apple Silicon performance

### Improvements

- Better PR fetching in git plugin

## [v0.68.0] - 2026-02-07

### Features

- Add ProjectRoot to plugin.Context for worktree-aware shared state
- Convert 17 docs/guides into .claude/skills for AI agent discovery
- Focus files pane when clicking markdown links

### Bug Fixes

- Fix splitFirst OOB panic by using strings.SplitN
- Resolve td root via git-common-dir for external worktrees

### Improvements

- Resolve all 360 golangci-lint issues across codebase
- Make lint-all CI-blocking

### Dependencies

- Update td to v0.31.0

## [v0.67.0] - 2026-02-06

### Features

- Merge notes plugin behind feature flag
- Add nightshift to sister projects section

### Bug Fixes

- Fix git search modal shortcut scoping while typing
- Fix for adding projects

### Improvements

- Refactor text input shortcut gating to plugin capability
- Show git plugin no repo state

### Dependencies

- Update td to v0.30.0

### Documentation

- Update UI guide for plugin text input capability
- Updated adapter guide

## [v0.66.0] - 2026-02-05

### Features

- Offer pull when push is rejected because remote is ahead (Pull button in error modal)
- Detect and display missing worktrees with pruning support
- Add IsMissing field to worktree struct for missing worktree detection

### Bug Fixes

- Show pull/fetch commands in git-status-commits context (footer and command palette)
- Consistent role names and clearer token display in conversations
- Update file delete shortcuts

### Documentation

- Add package-level doc comments for all internal packages

## [v0.65.1] - 2026-02-04

### Bug Fixes

- Fix Claude Code adapter not detecting sessions in paths with dots or underscores (#96)
  - Paths like `/home/user/github.com/project` now correctly match
  - Paths like `/home/user/my_project` now correctly match

## [v0.65.0] - 2026-02-03

### Performance

- Further FD tuning with codex integration

## [v0.64.0] - 2026-02-03

### Features

- Tiered file watching for FD reduction (reduces open file descriptors)
- Click worktree indicator in header to open worktree switcher
- Alt+C copy shortcut in file preview mode and inline edit
- Show copy hint on first text selection in file preview

### Bug Fixes

- Replace adapter Watch() with tiered watcher for FD reduction
- Return ToastMsg directly instead of ShowToast cmd

### Internal

- Unify ToastMsg types into msg package
- Add FD reduction patterns to adapter-creator-guide

## [v0.63.0] - 2026-02-02

### Features

- Character-level text selection in file browser
- Shared text selection module extracted to internal/ui
- Documentation migrated to sidecar.haplab.com with Haplab branding

### Bug Fixes

- Fix files line swallowing in file browser
- Fix text selection in line-wrapped files

## [v0.62.0] - 2026-02-02

### Features

- Stream session results per-adapter for incremental loading
- Client-side session pagination
- Animated braille spinner for adapter loading indicator
- Loading indicator while adapter batches are still arriving
- Scrollbar component (RenderScrollbar) with dedicated theme keys
- Scrollbars in file browser, git status, conversations, workspace sidebar, and modal viewport
- Auto-scroll modal viewport to focused element on Tab
- Target branch selector for merge workflow
- Fetch remote PR as workspace (F key)

### Bug Fixes

- Improved drag handle between panes
- Parallelize adapter loading and fix scroll-to-load-more
- Fix conversation and viewport line backgrounds
- Fix modal background color and scrollbar characters
- Scroll to focused element on SetFocus in modal
- Persist worktree base branch to .sidecar-base file
- Resolve main worktree path for .td-root
- Handle enter key on orphaned worktrees
- Dynamic dimensions for inline editor session
- Save/restore terminal state around editor/tmux launch
- Guard against non-terminal stdout at startup
- Tmux resize and dimension fixes
- Fix update preview modal not rendering for td-only updates
- Scrollbar resizing fix
- Various bug fixes from review session

### Dependencies

- Updated td to v0.29.0

## [v0.61.0] - 2026-01-31

### Features

- Breadcrumb navigation in git diff view

### Bug Fixes

- Fix showClock config not disabling clock in header bar
- File preview behavior improvements
- Suppress stray `[` characters leaked from mouse motion CSI sequences using time-gating

### Dependencies

- Updated td to v0.28.0

## [v0.60.0] - 2026-01-30

### Bug Fixes

- Fix file browser shortcuts (f, ctrl+p) intercepting text input during file creation, search, and other input modes

## [v0.59.0] - 2026-01-30

### Features

- Modal and shortcut capture improvements
- Allow `[` to be typed in interactive shells
- Show friendly error when issue not found in Open Issue modal
- Error messaging for invalid configs
- Improve sidebar discoverability with hints, Escape restore, and flash

### Bug Fixes

- Better merge errors in worktree merge
- Git diff performance guard
- Fix workspace performance and rendering bugs
- Fix diagnostics modal showing stale version info
- Fix config.Save() silently deleting user prompts from config.json
- Better install messaging

### Dependencies

- Updated td to v0.27.0

## [v0.58.0] - 2026-01-30

### Dependencies

- Updated td to latest version with installer process support

## [v0.56.1] - 2026-01-29

### Dependencies

- Updated td to v0.26.0

## [v0.56.0] - 2026-01-29

### Features

- **Preview tabs in file browser**: j/k navigation creates ephemeral preview tabs (italic in tab bar) that auto-replace on next navigation. Press `t`, `enter`, or `e` to pin permanently. Prevents duplicate tabs when browsing then opening files.

### Bug Fixes

- **Changelog modal not updating after fetch**: Added `clearChangelogModal()` after async content load so modal rebuilds with actual content instead of staying stuck on "Loading changelog..."
- **'t' key not opening new tabs**: `TabOpenNew` now skips dedup so `t` always creates a new tab
- **Preview tab ANSI escape codes visible**: Fixed `[3m...[0m` leaking into tab labels by applying italic via lipgloss style attributes instead of pre-rendering
- **Issue search modal footer layout**: Moved hints to custom footer (always visible), only show buttons when results exist

### Docs

- Added async content invalidation warning to modal caching guide

## [v0.55.0] - 2026-01-29

### Features

- td issue search anywhere: search, preview, and manage issues from any plugin via shortcut
- Scrollable search results with status tags, type icons, and priority display
- Ctrl+x toggle to show/hide closed issues in search
- Markdown rendering in issue preview modal with vim scrolling
- Back navigation and yank shortcuts in issue modals
- Line wrapping toggle (w shortcut) for diff and file preview
- Better UX for merge diffs
- Improved workspaces sidebar layout

### Bug Fixes

- Fix issue input modal interactivity and add recency-sorted search
- Fix markdown scrolling edge cases
- Fix git diff wrapping
- Reset changelog scroll offset on Esc
- Migrate confirm_stash_pop to modal library with mouse support
- Fix pointer receivers and UI improvements

### Dependencies

- Update td dependency

## [v0.54.0] - 2026-01-28

### Dependencies

- Update td to v0.24.0

## [v0.53.1] - 2026-01-28

### Bug Fixes

- Fix commit preview reloading repeatedly on watcher events causing screen flashing

## [v0.53.0] - 2026-01-28

### Features

- Auto-load preview on cursor landing after state transitions
- Add search highlighting in markdown preview mode

### Bug Fixes

- Fix ANSI mouse escape sequences appearing in modal filter inputs
- Fix stale shell entries across worktrees
- Better git error display (modal)
- Throttle inline editor mouse drag to prevent subprocess spam

## [v0.52.0] - 2026-01-27

### Features

- **Auto-Update Notifications**: Automatic update checking with in-app notification when new versions are available

### Bug Fixes

- Fix project add modal after refactor bugs
- Fix pull failing when deleting worktree from inside it
- Fix scrolling in changelog
- Fix edit state not restored when tab-switching from edit mode
- Preserve inline editor state when switching tabs
- Fix td plugin state not resetting on project switch
- Fix project add modal path input not accepting keyboard input

### Improvements

- Faster scrolling performance
- Better full refresh on project switch
- Improved td first-run experience with system detection
- Refactored update modal to use declarative modal library

## [v0.51.0] - 2026-01-27

### Bug Fixes

- Minor fixes to conversation content search

## [v0.50.0] - 2026-01-26

### Features

- **Shell Persistence**: Workspace shells now persist across sidecar restarts with multi-instance sync
- **Resume Conversation to Workspace**: Resume conversations directly into a workspace

### Bug Fixes

- Prevent tests from corrupting user config file
- Forward all keys to vim in inline edit mode
- Add delay after tmux resize before attach to prevent rendering issues
- Use list ID for agent focus in create worktree modal
- Add `.sidecar/` to default gitignore entries
- Make single focus default for list sections
- Fix race conditions in workspace plugin
- Update manifest when recreating orphaned shell

### Dependencies

- Updated td dependency to v0.23.0

## [v0.49.0] - 2026-01-26

### Dependencies

- Updated td dependency to v0.22.1

## [v0.48.0] - 2026-01-25

### Features

- **AI Agent Selection**: Optional AI agent selection when creating new workspace shells
- **Improved File Search**: Enhanced file search capabilities
- **Mouse Support**: Added mouse support for inline vim editor

### Improvements

- Workspaces UX enhancements
- Inline editor improvements

### Dependencies

- Updated td dependency from v0.21.0 to v0.22.0

## [v0.47.0] - 2026-01-25

### Features

- **Inline Editor**: Render editor in preview pane with session detection
- **Worktree Switcher**: Indicate current worktree and preserve last selection

### Improvements

- Cooler empty workspace screen
- Skeleton loading animation in conversations plugin
- Gracefully handle non-vim editors in inline editor
- Handle deleted worktrees more gracefully
- Initial abstraction of tty plugin

### Bug Fixes

- Fix inline editor syntax highlighting and theme colors
- Fix switching from Worktree view to git

## [v0.46.0] - 2026-01-25

### Improvements

- Watcher improvements for better file monitoring
- Better project switching with improved context handling
- Enhanced conversation switch guidance
- Cursor position improvements in modals

### Bug Fixes

- Project switcher context improvements

### Documentation

- Docs changes and additional tests

## [v0.45.0] - 2026-01-24

### Bug Fixes

- Add io.Closer return value to Watch() method for proper resource cleanup in adapter implementations

## [v0.44.0] - 2026-01-23

### Features

- **File Browser**: Fast file browser with improved performance
- **Projects**: Inline project creation from project switcher modal

### Improvements

- Eliminate interactive typing latency in worktree mode
- Unify modal priority with ModalKind type and activeModal() helper
- Remove side-scrolling
- Update keybindings
- Revert poll interval to 2s (fingerprint cache approach sufficient)

### Bug Fixes

- Fix button hit region calculation in project add modal
- Fix exit shell in some situations
- Remove dead shells properly

### Dependencies

- Updated embedded td to v0.21.0

## [v0.43.0] - 2026-01-23

### Features

- **Interactive Mode**: Character-level selection granularity with drag-to-select
- **Interactive Mode**: Selection background with preserved foreground colors
- **Git**: Add git amend commit shortcut (A) in git-status
- **Workspace**: Renamed worktrees to workspaces for clarity

### Improvements

- **Interactive Mode**: Incremental parsing with targeted session refresh reducing CPU usage
- **Interactive Mode**: Named shells upon creation for better session tracking
- **Modal**: Only close modals when clicking outside them (improved UX)
- **Input**: Align interactive copy/paste hints with configured keys
- Filter partial SGR mouse sequences to prevent stray ESC forwarding
- Enhanced keyboard shortcut handling and escape sequence processing

### Bug Fixes

- Fixed selections in interactive mode
- Fixed stray ESC forwarding in partial mouse sequence filter

### Dependencies

- Updated embedded td to latest version

## [v0.42.0] - 2026-01-23

### Improvements

- Enhanced keyboard shortcut handling and bindings
- Improved gitstatus plugin event handling

### Dependencies

- Updated embedded td to latest version

## [v0.41.0] - 2026-01-22

### Bug Fixes

- Fixed feature flags being reset during config saves
- Fixed interactive mode scroll to use previewOffset instead of tmux commands
- Fixed config save overwriting user settings
- Fixed git repo root detection from subdirectory in gitstatus plugin

### Improvements

- Enhanced tmux pane resizing for detached sessions
- Improved tmux pane width synchronization

## [v0.40.0] - 2026-01-22

### Performance

- **Interactive Mode**: Improved output capture and rendering performance
- **Interactive Mode**: Enhanced auto-scroll alignment with interactive content
- **Interactive Mode**: Fixed cursor spacing and synchronized pane sizing

### Dependencies

- Updated embedded td to latest version

## [v0.39.0] - 2026-01-22

### Performance

- **Interactive Mode**: Three-state visibility polling (visible+focused, visible+unfocused, not visible)
- **Interactive Mode**: Fixed duplicate poll chain bug causing 200% CPU usage
- **Interactive Mode**: Correct generation map usage for shell vs workspace polling

## [v0.38.0] - 2026-01-22

### Features

- **Interactive Mode**: Beta interactive shell mode behind feature flag (`features.interactiveMode`)

### Improvements

- **Workspace**: Modal keyboard navigation with tab/shift+tab cycling

### Dependencies

- Updated embedded td to v0.20.0

## [v0.37.0] - 2026-01-21

### Dependencies

- Updated embedded td to v0.19.0 (performance fix)

## [v0.36.0] - 2026-01-21

### Features

- **Themes**: Live theme switcher modal with persistence

## [v0.35.0] - 2026-01-21

### Improvements

- **File Browser**: Refactored scroll-to-line logic into reusable helper

## [v0.34.0] - 2026-01-20

### Features

- **Tabs**: Configurable tab themes with 16 built-in presets
- **Workspace**: File picker modal (`f`) in diff view
- **Workspace**: File headers and navigation in diff pane
- **Project Switcher**: Header click opens project switcher
- **Shells**: Rename shells with persistent display names
- **Shortcuts**: Improved modal layout

### Performance

- **Tasks**: Pre-fetch task details to eliminate lag in task tab
- **Adapters**: Cache metadata and reduce file I/O

### Bug Fixes

- **Merge**: Better error handling and resolution actions
- **Workspace**: Fix race conditions in caches and pre-fetch
- **Workspace**: Flash preview pane on invalid key interactions
- **Workspace**: Fix workspace deletion from non-repo cwd
- **Watchers**: Fix race conditions and buffer issues
- **Project Switcher**: Don't hijack filter input
- **Shells**: Fix display name persistence when saving defaults
- Guard flashPreviewTime against zero-value time

### Dependencies

- Updated embedded td to v0.18.0

## [v0.33.0] - 2026-01-20

### Features

- **Workspace**: Multiple shells per workspace - open and manage multiple terminal sessions
- **Workspace**: [+] buttons in Shells and Workspaces sub-headers for quick creation
- **Workspace**: Persist and restore workspace/shell selection across sessions
- **Project Switcher**: g/G navigation to jump to first/last project
- **File Browser**: Auto-refresh tree on plugin focus

### Bug Fixes

- **Workspace**: Fix orphaned tmux sessions on workspace delete/merge
- **Workspace**: Fix shell selection shift when earlier shell removed
- **Workspace**: Fix shell selection bugs and use name-based polling

## [v0.32.0] - 2026-01-20

### Features

- **Workspace**: Improved shell UX and navigation

### Bug Fixes

- **Workspace**: Auto-focus newly created workspace in list and preview
- **Workspace**: Handle waitForSession failure in ensureShellAndAttach

## [v0.31.0] - 2026-01-20

### Features

- **Workspace**: Project shell as first entry in workspace list

### Bug Fixes

- **Workspace**: Shell preview shows output immediately
- **Workspace**: Auto-attach to existing shell with improved primer text
- **Workspace**: Fixed shell preview, primer, and project switch issues
- **Workspace**: Replace fixed sleep with retry loop in ensureShellAndAttach
- **Project Switcher**: Better help modal
- **Website**: Fixed hamburger menu navigation links

## [v0.30.0] - 2026-01-20

### Features

- **Project Switcher**: Type-to-filter support - type to filter projects by name/path in real-time, shows match count, Esc clears filter or closes modal
- **Project Switcher**: j/k keyboard navigation now works correctly (previously went to text input)

### Bug Fixes

- Fixed project switcher Esc handler missing context update
- Fixed project switcher hover state not clearing on filter change

### Documentation

- Added project switcher developer guide (`docs/deprecated/guides/project-switcher-dev-guide.md`)

## [v0.29.0] - 2026-01-19

### Features

- **Project Switcher**: Press `@` to switch between configured projects without restarting sidecar. Configure projects in `~/.config/sidecar/config.json` with `projects.list`. Supports keyboard navigation (j/k, Enter) and mouse interaction. State (active plugin, cursor positions) is remembered per project.
- **File Browser**: Toggle git-ignored file visibility with `H` key, state persists across sessions

### Dependencies

- Updated embedded td to v0.17.0

## [v0.28.0] - 2026-01-19

### Features

- **File Browser**: Vim-style `:<number>` line jump in file preview

### Bug Fixes

- **Workspace**: Reload commit status when cached list is empty

### Dependencies

- Updated embedded td to v0.16.0

## [v0.27.1] - 2026-01-19

### Bug Fixes

- **Conversations**: Use adapter-specific agent names instead of hardcoded "claude"

## [v0.27.0] - 2026-01-19

### Bug Fixes

- **Cursor Adapter**: Use blob hash as message ID to prevent cache collisions

## [v0.26.0] - 2026-01-19

### Features

- **Git Blame View**: Added blame view to file browser plugin
- **Thinking Status**: Added thinking status indicator to workspace with detection priority fix
- **Truncation Cache**: Added truncation cache to eliminate ANSI parser allocation churn

### Performance

- **Conversations Plugin**: Performance improvements with code review refinements

### Bug Fixes

- Fixed memory leak in workspace output panel horizontal scrolling
- Fixed unicode truncation and extracted blame constants

### Dependencies

- Updated embedded td to v0.15.1

## [v0.25.0] - 2026-01-17

### Features

- **Memory Profiling**: Added pprof instrumentation for diagnosing memory leaks (enable with `SIDECAR_PPROF=1`)
- **TD Theme Integration**: Embedded td now respects sidecar's theme colors for markdown rendering

### Dependencies

- Updated embedded td to v0.14.0 (includes theme support for markdown rendering)

## [v0.24.0] - 2026-01-17

### Dependencies

- Updated embedded td to v0.13.0

## [v0.23.0] - 2026-01-17

### Features

- **File Browser Improvements**: Support for vim-like line jumps (`:<number>`) in file browser

### Performance

- **Memory Optimizations**: Improved memory usage for long-running sessions

### Dependencies

- Updated embedded td to latest version

## [v0.22.0] - 2026-01-17

### Features

- **Yank keyboard shortcuts**: Added y/Y keys for copying content in conversations plugin
- **Send-to-workspace integration**: Launch agents directly from td monitor to workspaces

### Bug Fixes

- Fixed workspace session lookup for nested directories and sanitized names
- Fixed send-to-workspace with lazy loaded npm environments
- Fixed Unicode truncation and refactored modal initialization
- Fixed memory leak and CPU performance in workspace output pane
- Fixed off-by-one mouse hit regions in workspace modals
- Fixed commit status not showing for workspaces with unset BaseBranch
- Fixed O(n²) cache eviction in session metadata cache
- Fixed detectDefaultBranch() not being called due to caller defaults

### Changes

- Removed YAML config support (JSON only)
- Extracted resolveBaseBranch() helper to deduplicate default branch detection
- Replaced hardcoded 'main' defaults with detectDefaultBranch()

## [v0.21.0] - 2026-01-17

### Bug Fixes

- Fixed pullAfterMerge corrupting working tree when on base branch (uses pull --ff-only instead of update-ref)

## [v0.20.0] - 2026-01-17

### Features

- **Simplified workspace kanban**: Removed "Thinking" status, streamlined to Active/Waiting/Done/Paused
- Updated Waiting status icon from 💬 to ⧗ for better clarity

### Dependencies

- Updated embedded td to latest version

## [v0.19.0] - 2026-01-16

### Features

- **Workspace merge improvements**: Gracefully handle existing MRs, mouse support for merge modal
- **Workspace conversation integration**: Better workspace-conversation linking
- **Website**: TUI-themed homepage with interactive demo, agents section
- **Docusaurus documentation site**: Added Docusaurus 3.9 documentation site

### Bug Fixes

- Fixed race condition in cleanup completion
- Added branch deletion warnings
- Fixed workspace click offset
- Fixed workspace create modal mouse support

### Performance

- Workspace adaptive polling and optimized tmux capture

### Improvements

- Split large files for better maintainability

## [v0.18.0] - 2026-01-15

### Features

- **Workspace diff improvements**: Show commits in diff pane even when no uncommitted changes
- **Workspace conversation preservation**: Conversations now preserved after workspace deletion
- **Workspace-aware conversations**: Conversations plugin now understands workspace context
- **Mouse support**: Comprehensive mouse support added to workspace plugin
- **Workspace guide**: Added workspace explanation to welcome guide

### Bug Fixes

- Fixed SanitizeBranchName `.lock` suffix handling
- Improved workspace conversation detection

## [v0.17.0] - 2026-01-15

### Features

- \*\*Workspace prompts: Create workspaces with custom prompts attached
- **Auto-generated default prompts**: New users get starter prompts automatically
- \*\*PR indicator: Workspaces with open PRs now show visual indicator
- **Inline tmux guide**: Tmux setup instructions integrated into workspace view
- **Better waiting/paused visibility**: Clearer distinction between waiting and paused states in workspaces

### Bug Fixes

- Fixed 20+ Unicode byte-slicing bugs in UI string truncation across multiple components
- Fixed empty prompt picker UI display
- Added prompt creation guide for new users

### Dependencies

- Updated embedded td to v0.12.3 (from v0.12.2)

## [v0.16.3] - 2026-01-14

### Improvements

- Improved kanban board in workspaces plugin

### Bug Fixes

- Use launcher script for agent prompts to avoid shell escaping issues
- Change 'c' key in merge workflow to skip cleanup (keep workspace) instead of advancing to cleanup

## [v0.16.2] - 2026-01-14

### Bug Fixes

- Escape agent messages properly in workspaces plugin
- Pass task context to all agent types in workspaces plugin
- Better workspace initial environment handling
- Minor improvements to Claude Code adapter
- Consolidate env var commands in workspace sessions (cleaner output)

### Dependencies

- Updated embedded td to latest

## [v0.16.1] - 2026-01-14

### Bug Fixes

- Session list now only reserves space for duration/token columns when data exists (more room for titles)

## [v0.16.0] - 2026-01-14

### Features

- **Conversations UI Overhaul**: Premium experience for viewing Claude Code sessions
  - Colorful model badges (opus=purple, sonnet=green, haiku=blue)
  - Token flow indicators showing input→output usage
  - Tool-specific icons (Read, Edit, Write, Bash, Search, etc.)
  - Enhanced thinking block styling with expand/collapse
  - Session list shows adapter icons and token counts
  - Improved main pane header with model badge, stats, and cost estimate

### Bug Fixes

- Fixed XML tags appearing in session titles (now properly extracts user queries)
- Fixed session titles for messages starting with local command caveats
- Skip trivial commands (/clear, /compact) when finding session title
- Filter out metadata-only sessions (no messages) from session list
- Improved extraction of text content from tool inputs in message display

### Dependencies

- Updated embedded td to v0.12.2 (from v0.12.1)

## [v0.15.0] - 2026-01-14

### Features

- Remember workspace diff mode (staged/unstaged preference persists)
- Documented workspaces plugin in README

### Bug Fixes

- Fixed git diff view for commits
- Many QoL changes and bug fixes
- Ignore double-click on folders in git status (single-click handles expansion)
- Clear stale push hash on push error
- Add shift+tab support for workspace pane switching

### Dependencies

- Updated embedded td to v0.12.1 (from v0.12.0)

## [v0.14.7] - 2026-01-14

### Features

- Auto-add sidecar state files (.sidecar-agent, .sidecar-task, .td-root) to .gitignore on workspace creation

### Bug Fixes

- Fixed nil pointer in stageAllAndCommit when git tree fails to initialize
- Clear preview pane when workspace is deleted to prevent stale content
- Cancel merge workflow on error instead of proceeding with broken state
- Show "No workspace selected" message when workspace list is empty

## [v0.14.6] - 2026-01-14

### Bug Fixes

- Fixed panels not extending to footer row in Files, Conversations, and Git plugins (drag handle appeared longer than panels)

## [v0.14.5] - 2026-01-14

### Features

- Added confirmation dialog before deleting workspaces

### Bug Fixes

- Fixes from code review

## [v0.14.4] - 2026-01-14

### Bug Fixes

- Fixed layout rendering issues where plugin header would scroll off-screen
- Improved width calculations to properly account for borders and padding
- Added ANSI-aware truncation to handle escape codes correctly
- Added tab expansion for proper alignment in terminal output

### Dependencies

- Updated embedded td to latest version

## [v0.14.3] - 2026-01-14

### Bug Fixes

- Fixed horizontal scroll to preserve syntax highlighting in diffs and workspace views

### Dependencies

- Updated embedded td to v0.12.0 (from v0.11.0)

## [v0.14.2] - 2026-01-13

### Bug Fixes

- Fixed installation failure due to missing td types (updated td v0.10.0 → v0.11.0)

## [v0.14.1] - 2026-01-13

### Bug Fixes

- Fixed border rendering issues in conversations plugin
- Improved gradient border rendering in td integration

## [v0.14.0] - 2026-01-13

### Features

- **Theming System**: Thread-safe theming infrastructure with customizable colors
- **Unified Sidebars**: Consistent collapsible sidebar behavior across all plugins

### Improvements

- Cache improvements in conversation adapters
- Performance optimizations for conversations loading
- Render cache LRU comment fix

### Bug Fixes

- Fixed race condition and CPU optimization for session adapters
- Fixed losing mouse interactivity after editing a file
- Fixed quit bug in td
- Fixed IsValidHexColor comment to match regex behavior

### Dependencies

- Updated embedded td to v0.10.0 (from v0.9.0)

## [v0.13.2] - 2026-01-10

### Improvements

- Conversations plugin performance improvements
- Modal button hover/click behavior refined
- Modals now have more uniform styling

### Dependencies

- Updated embedded td to v0.9.0 (from v0.7.0)

## [v0.13.1] - 2026-01-10

### Bug Fixes

- Fixed off-by-one error in git sidebar commit click detection when working tree is clean

## [v0.13.0] - 2026-01-10

### Features

- **Git Graph View**: Visualize commit history as ASCII graph with `g` key toggle
- **Improved Git List View**: Better commit display with cleaner formatting

### Improvements

- Git sidebar UI refinements and polish
- Updated modal creator guide documentation

### Bug Fixes

- Fixed conversations plugin rendering issues
- Various conversations plugin stability fixes

## [v0.12.1] - 2026-01-08

### Bug Fixes

- Fixed intermittent crashes while an agent was running by mutex-protecting Claude Code adapter session cache

### Dependencies

- Updated embedded td to v0.7.0 (from v0.5.0)

## [v0.12.0] - 2026-01-07

### Features

- **Interactive Modal Buttons**: File browser modals now have clickable Confirm/Cancel buttons
- **Tab Navigation**: Tab key cycles focus between input field and modal buttons
- **Mouse Hover**: Buttons highlight on mouse hover (dark pink)
- **Path Auto-Complete**: Move modal shows fuzzy-matched directory suggestions

### Improvements

- Better visual feedback for modal button interactions (focus vs hover states)

## [v0.11.0] - 2026-01-07

### Bug Fixes

- Fixed WARN logs appearing in non-git directories (plugin unavailable now logs at DEBUG level)

## [v0.10.0] - 2026-01-07

### Features

- **Git History Search**: Search commits with `/` key, regex support, case-sensitive toggle
- **Author Filter**: Filter commits by author with `f` key
- **Path Filter**: Filter commits by file path with `p` key
- **Inline Commit Stats**: Display +/- stats next to selected commits

### Improvements

- Removed delta external tool fallback (built-in diff viewer only)
- Moved tree search bar inside pane for consistent UX
- Consolidated horizontal scroll bindings (h, left, <, H)

### Refactoring

- Removed single-pane mode from conversations plugin (~400 lines)
- Updated adapter-creator-guide with Icon() requirement

### Bug Fixes

- Fixed duplicate horizontal scroll bindings in git diff view
- Added unknown adapter fallback ("?") in badge rendering
- Added explicit .git exclusion in file search

## [v0.9.0] - 2026-01-07

### Features

- **File Info Modal**: View file info with git status via info modal
- **Copy Paths**: Copy files/paths from right panel of files plugin
- **Session Persistence**: Remember previously opened plugin/tab across restarts
- **File Memory**: File browser remembers open file across projects
- **Colorful Tabs**: Improved visual tab styling
- **Adapter Icons**: Populate AdapterIcon in session creation

### Improvements

- Better missing-td screen
- Improved git repo readability
- Removed emojis from info modal

### Refactoring

- Split filebrowser plugin.go into handlers.go and operations.go

### Bug Fixes

- Various fixes from code review

### Dependencies

- Updated embedded td to v0.5.0 (from v0.4.23)

## [v0.8.4] - 2026-01-06

### Dependencies

- Updated embedded td to v0.4.23 (from v0.4.22)

## [v0.8.3] - 2026-01-06

### Bug Fixes

- Fixed mouse wheel scrolling not working when cursor is over session or turn items in conversations plugin
- Added `scrollDetailPane()` for detail view mouse scrolling

## [v0.8.2] - 2026-01-06

### Dependencies

- Updated embedded td to v0.4.22

## [v0.8.1] - 2026-01-06

### Dependencies

- Updated embedded td from v0.4.18 to v0.4.21

### Documentation

- Updated release guide to document td sync requirement before releases

## [v0.8.0] - 2026-01-05

### Features

- **Cursor CLI Adapter**: Full support for Cursor Agent sessions with query extraction, system context filtering, meaningful session names, model info, and resume command support
- **In-App Updates**: Update sidecar and td directly from the app with interactive button in diagnostics modal
- **Markdown Rendering**: Toggle markdown preview in file browser with 'm' key

### UI Improvements

- Turn detail shown in right pane (two-pane layout)
- Improved conversations plugin layout

### Bug Fixes

- Fixed markdown cache invalidation on window resize
- Fixed detail pane height overflow with scroll indicators
- Optimized regex compilation in cursor adapter

## [v0.7.2] - 2026-01-05

### Features

- Force version check on diagnostics modal open (bypasses 3-hour cache)

## [v0.7.1] - 2026-01-05

### Bug Fixes

- Fixed `Y` key to copy correct adapter-specific resume command instead of always copying `claude --resume`

## [v0.7.0] - 2026-01-05

### Features

- **In-App Update Feature**: Update sidecar and td directly from within the app
  - Press `!` to open diagnostics modal
  - Press `u` or click **Update** button to install updates
  - Animated spinner shows installation progress
  - Restart prompt after successful update

## [v0.6.1] - 2026-01-05

### Changes

- Reduced version check cache TTL from 6 hours to 3 hours

## [v0.6.0] - 2026-01-05

### Features

- **Markdown Rendering in Conversations**: LLM responses render with proper markdown formatting
  - Code blocks with syntax highlighting
  - Headers, lists, emphasis
  - Automatic fallback to plain text for narrow terminals
  - Cached rendering for performance
