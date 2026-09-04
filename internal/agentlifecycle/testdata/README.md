# Agent lifecycle Phase A evidence

## What is in here

`capabilities.json` is the machine-readable capability matrix: one entry per
provider integration, carrying the tier Phase A evidence justifies, which
transitions were actually covered, and how the evidence was obtained.
`capability_test.go` loads it and re-derives each tier from the recorded
coverage, so the file cannot claim an authority its own evidence does not
support.

`traces/<provider>/*.tsv` are sanitized real event traces. They are the
difference between a documented contract and a proved one, and the tier in
`capabilities.json` is only allowed to say `real-trace` for a provider that has
files here.

The prose companion, including the per-provider gap analysis and the
steel-thread decision, is `docs/reference/agent-lifecycle-capability-matrix.md`.

### Traces whose value is an absence

Some traces are checked in to record that a provider emitted **nothing** — the
two Claude cancellation captures are the current examples, and that absence is
the single fact capping Claude below full lifecycle authority.

An absence is only as strong as the interval it was measured over, and there are
two kinds of interval. **A window is owed when the interval is a watch**: the
listener stayed attached and nothing arrived, and the trace simply stops.
"Nothing fired" over one second and over eighteen are very different claims, and
a reader cannot tell them apart from a trace that ends. Such a trace must carry a
trailing comment row naming its window:

```
# capture-window: 18s
```

**A window is not owed when the interval is delimited by recorded rows.** "No
permission event appeared between before_agent_start and agent_settled" is
bounded by two rows a reader can see, so its strength is in the fixture already
and a duration would add nothing. `traces/pi/tool-turn.tsv` is that shape and
deliberately carries no window.

The distinction matters because it is also the test for whether an absence claim
belongs in a trace file at all. A claim the six columns cannot support is not
made stronger by a window: it is prose about a run, and it belongs beside the
evidence that does support it. That trace carries a note saying exactly this
about Pi's untyped event bus, which no column here observes.

`hooktrace_test.go` requires a window for the watch-shaped traces and rejects a
value `time.ParseDuration` cannot read, so the evidence stays attached to the
claim rather than living in a session log nobody will find. Traces that only
record what did happen need no such row.

Note what these traces do **not** do. A test reading a static fixture cannot
notice that a provider's behavior changed; it fails only once a human has
re-traced and edited the file. They are fixture-integrity guards. Discovering
that a gap has closed is the requalification procedure's job, on its stated
cadence.

## Trace provenance

Captured 2026-08-30 on darwin/arm64.

| Trace | Provider | Version | Model | Outcome | Captured |
| --- | --- | --- | --- | --- | --- |
| `traces/opencode/tool-turn-with-permission.tsv` | opencode | 1.18.23 | openai/gpt-4o-mini | success | Phase A |
| `traces/opencode/session-error-turn.tsv` | opencode | 1.18.23 | google/gemini-2.5-flash | provider auth error | Phase A |
| `traces/opencode/cancelled-turn.tsv` | opencode | 1.18.25 | openai/gpt-4o-mini | user cancellation | Phase B |
| `traces/opencode/provider-error-named.tsv` | opencode | 1.18.25 | google/gemini-2.5-flash | provider auth error | Phase B |
| `traces/pi/simple-turn.tsv` | pi | 0.84.3 | openrouter z-ai/glm-5.3-flash | success | Slice 1 |
| `traces/pi/tool-turn.tsv` | pi | 0.84.3 | openrouter z-ai/glm-5.3-flash | success, one bash tool call | Slice 1 |
| `traces/pi/cancelled-turn.tsv` | pi | 0.84.3 | openrouter z-ai/glm-5.3-flash | user cancellation | Slice 1 |
| `traces/pi/error-turn-and-quit.tsv` | pi | 0.84.3 | openrouter stealth/ox-alpha | provider 404, then /quit | Slice 1 |
| `traces/kilo/simple-turn.tsv` | kilo | 7.5.9 | openrouter z-ai/glm-5.3-flash | success | Slice 2 |
| `traces/kilo/tool-turn.tsv` | kilo | 7.5.9 | openrouter z-ai/glm-5.3-flash | success, one bash tool call | Slice 2 |
| `traces/kilo/blocked-turn.tsv` | kilo | 7.5.9 | openrouter z-ai/glm-5.3-flash | success, one permission ask and reply | Slice 2 |
| `traces/kilo/error-turn.tsv` | kilo | 7.5.9 | openrouter (nonexistent model id) | provider error | Slice 2 |
| `traces/kimi/tool-turn-with-permission.tsv` | kimi-code | 0.40.1 | openrouter openai/gpt-4.1-mini | success, one Bash call, approved | Slice 2 |
| `traces/kimi/cancelled-turn.tsv` | kimi-code | 0.40.1 | openrouter openai/gpt-4.1-mini | user cancellation | Slice 2 |
| `traces/kimi/session-end.tsv` | kimi-code | 0.40.1 | openrouter openai/gpt-4.1-mini | /quit | Slice 2 |
| `traces/kimi/exec-turn-auto-approves.tsv` | kimi-code | 0.40.1 | openrouter openai/gpt-4.1-mini | success under `kimi -p`, no permission pair | Slice 2 |
| `traces/qwen/session-start.tsv` | qwen-code | 0.23.0 | none (no auth type selected) | session start, before authentication | Slice 4 |

The Pi traces were captured 2026-09-02 on darwin/arm64; the four rows above are
one Pi 0.84.3 process each for the first three and a second process for the
fourth. Their capture procedure and their extra sanitization rule are in the Pi
section below, because they are the first traces in this directory to record any
event *value*.

The Qwen row was captured 2026-09-04 on darwin/arm64, and it is the first trace here taken with **no model and no credentials** -- a property of the provider rather than a shortcut. Qwen Code fires `SessionStart` before an auth type is selected, so the capture was taken with the pane sitting on its provider picker; the same payload arrives under `qwen -p`, where the run then refuses for want of auth. A session-identity integration has exactly one thing to prove, and for this provider proving it costs one process start.

The error trace is kept deliberately. A failed turn is a real lifecycle path,
and it is the one that shows `session.error` resolving to `session.idle` rather
than latching the pane on `working` — which is exactly the failure mode the
resolver has to survive.

The Phase B pair exists because cancellation and failure are the *same shape* on
the OpenCode bus. `cancelled-turn.tsv` is a turn interrupted by the user;
`provider-error-named.tsv` is the same harness with no credentials for the
selected model. They differ in exactly one recorded value — the bounded error
class name, `MessageAbortedError` against `ProviderAuthError` — and that is the
whole reason the second one is checked in. Without it, "an aborted message means
the user cancelled" would be an assumption rather than a measurement.

Two things about capturing a cancellation, both learned the hard way. The
OpenCode TUI needs **two** Escape presses to interrupt: the first only arms the
confirmation and changes the footer from `esc interrupt` to `esc again to
interrupt`. And the abort reaches the event bus a few seconds after the screen
stops showing the busy indicator, so tearing the session down as soon as the
screen settles records a truncated trace that looks like no events fired.

## How they were captured, and what was not touched

A trace plugin was installed into a **temporary** `XDG_CONFIG_HOME` created
under the run's scratch directory. The user's real `~/.config/opencode` — which
contains their own plugins — was never read, written, copied, or moved. No
provider configuration outside the temporary tree was created or modified.
`~/.local/share/opencode` was left alone as well; it supplied credentials to the
provider as it normally would, and nothing was written back to it by this
harness.

The provider was driven with `opencode run`, one short turn per trace, with the
cheapest available model. The permission event pair was produced by setting
`"permission": {"bash": "ask"}` in the temporary config and asking for a single
`echo`.

Cancellation cannot be produced by `opencode run`, because there is no
interactive session to interrupt. The Phase B cancellation trace therefore drove
the real TUI inside a **private tmux server** (`tmux -S` on a dedicated socket,
never the machine's default server), sent one prompt, waited for
`session.status {"type":"busy"}`, and then sent Escape twice. The tmux server
was killed at the end of the run and nothing outside the temporary tree was
touched.

## Sanitization

Sanitization is by construction rather than by redaction, which is the only
version worth trusting: the trace plugin never had the content in the first
place. It recorded event and hook **names**, the `session.status` discriminator,
the tool name, and booleans for whether an identity field was present. It never
recorded prompt text, response text, tool arguments, tool results, file paths,
session identifiers, credentials, or environment values.

The checked-in `.tsv` additionally replaces wall-clock time with a millisecond
offset from the first event, so the fixture is stable across runs, and drops
startup catalog chatter (`plugin.added`, `catalog.updated`, `integration.updated`,
`reference.updated`) that says nothing about lifecycle.

Columns are: `offset_ms`, `kind` (`bus` or `hook`), `type`, `status`, `tool`,
`session-id present`, `parent-id present`, and — on Phase B traces only — the
bounded `error class name`. The Phase A files predate error-name capture and
have seven columns; readers must tolerate both widths, which `readTrace` in
`capability_test.go` does.

The error column records a class name such as `MessageAbortedError`, truncated
to 64 bytes. It never carries the error message, stack, or any provider payload
text. A class name is a closed vocabulary chosen by the provider's own source,
which is what makes it safe to record where a message would not be.

Phase B traces additionally drop `message.part.delta`. The TUI streams one of
those per token, so a single cancelled turn produced 1604 of them; they carry no
lifecycle information and would bury the fixture.

## Pi

Pi's traces use the six-column hook layout — `offset_ms`, `event`, `session`,
`turn`, `tool`, `fields` — that `readHookTrace` in `hooktrace_test.go` reads, the
same one Codex and Claude use. Pi is bus-shaped rather than hook-shaped, but the
columns carry the same evidence and a second reader would have bought nothing.
`turn` is always `-`: Pi's `turn_start` carries a `turnIndex`, which is a
position rather than an identifier, and it is recorded as a field name only.

### How they were captured, and what was not touched

A tracer extension was installed into a **temporary** `PI_CODING_AGENT_DIR`
created under the run's scratch directory. Pi resolves both its extension
directory and its sessions directory from that variable
(`dist/config.js:420-426`, `:455-457`), so the whole agent tree moved with it.
The user's real `~/.pi` — which holds their own extensions, including Herdr's —
was read once to see what a run needs and never written, moved, or deleted.
Only `settings.json`, `models.json`, `models-store.json` and `trust.json` were
copied *out*, and the copy's `defaultModel` was pointed at a live cheap model
after the configured one turned out to have been retired. `auth.json` is empty
on this machine: Pi reads its OpenRouter credential from the environment, so
nothing secret was copied anywhere.

Sidecar's own asset was installed beside the tracer with
`sidecar agent integration install pi`, not by hand, so the run also proves the
installer. Pi was driven in a real TUI inside a Sidecar-managed shell on a
**private tmux server** (`tmux -S` on a dedicated socket, never the machine's
default server), with `XDG_STATE_HOME`, `-config` and `SIDECAR_ISOLATED_STATE=1`
holding Sidecar's own state off the real tree. `HERDR_ENV` was never set: with it
set, Herdr's extension and Sidecar's would both claim the pane.

One trap is worth recording for whoever recaptures. The `sidecar` **CLI**
dispatches in `cmd/sidecar/main.go` before `main` unsets `TMUX`, deliberately and
by comment. A CLI-driven proof run started from inside a tmux pane therefore
talks to the socket named in `$TMUX` — the machine's default server — no matter
what `TMUX_TMPDIR` says. Unset `TMUX` in the harness. `scripts/tmux-drive.sh` is
unaffected because it launches the TUI, which does reach the unset.

### Sanitization

Sanitization is by construction, as everywhere else in this directory: the
tracer recorded event names, `ctx` discriminators, and payload field **names**,
and never had prompt text, response text, tool arguments, tool results, file
contents, or environment values. Session identifiers were mapped to
`session-N` placeholders inside the tracer process, so no real identifier ever
reached a file.

Pi's traces are the first here to record event **values**, and the rule is
narrow, closed, and enforced. A value is recorded only for a key whose vocabulary
is fixed by Pi's own source, which is the same rule that let the OpenCode Phase B
traces record a bounded error class name where a message would have been a
privacy failure. The permitted keys are exactly:

| Key | Why a value is safe |
| --- | --- |
| `type` | Pi's own event discriminator |
| `reason` | Pi's own bounded reason enum (`startup`, `reload`, `quit`, …) |
| `ctx.mode` | The session mode discriminator (`tui`, `rpc`, …) |
| `ctx.isIdle` | A tri-state boolean, or absent |
| `ctx.sessionFile` | `present` or `absent` only — never the path |
| `ctx.sessionId` | `present` or `absent` only — never the id |

Every other key appears as a bare name: `prompt`, `images`, `message`, `args`,
`result` and `systemPrompt` are all in these fixtures, and all of them as names
alone. The last four rows are derived observations rather than payload fields,
and they are recorded at all because the shipped asset's guards are built on
exactly them — a bare name would not show whether a guard was correct.

`TestNoHookTraceCarriesAValue` enforces this as an allowlist: a `=` on any key
outside the table fails, whatever the value looks like. Before that check it
compared only the `session` and `turn` columns, which was sufficient while every
trace here held bare names, and would have let a future capture record
`prompt=<the user's prompt>` without a single test noticing. Widening the
allowlist is a deliberate act, and it means asserting that the key's values
cannot carry user content.

## Kilo

Kilo's traces use the same six-column hook layout Pi's do, with one addition worth knowing before reading them: the `event` column carries a `bus:` or `hook:` prefix. Kilo is an OpenCode fork and its plugin surface has both shapes, and whether a name arrives on the event bus or as a plugin hook is exactly the distinction that decides whether two of the ported asset's branches can ever fire. `turn` is always `-`: kilo has no turn identifier on the events the asset reads.

### How they were captured, and what was not touched

`@kilocode/cli` 7.5.9 was installed into a scratch npm prefix under the run's directory and never onto the maintainer's `PATH`. Every run went through a wrapper that cleared the environment and pointed `HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME` at a temporary tree, so the whole of kilo's config, data, state and cache moved with it and the maintainer's real `~/.config/kilo` was never read, written, copied or moved. The only variable passed through from the ambient environment was `OPENROUTER_API_KEY`, which kilo reads for provider credentials exactly as it normally would; nothing was written back to it.

A tracer plugin was installed into the temporary `$XDG_CONFIG_HOME/kilo/plugin/`. `HERDR_ENV` was never set: with it set, Herdr's plugin and Sidecar's would both claim the pane.

The provider was driven with `kilo run`, one short turn per trace, with a cheap model. The permission pair was produced by setting `"permission": {"bash": "ask"}` in the temporary `kilo.json` and asking for a single `echo`. **`kilo run` is non-interactive and answers its own prompt**, which is why the blocked trace shows one millisecond between `permission.asked` and `permission.replied` and why its comment says exactly what that does and does not prove.

The error trace used a model id that does not exist, which is the cheapest way to reach `session.error` and is the path that shows a failed turn resolving rather than latching the pane on working.

### Two traps, both found the hard way in this run

**`KILO_CONFIG_DIR` is not isolation.** Kilo creates its XDG default config directory at startup whatever that variable says, so a run that relocates only `KILO_CONFIG_DIR` still writes into `~/.config/kilo`. This run put one zero-byte migration marker there before the mistake was caught; the file was removed and the directory restored to its previous contents. The config directory was not the only one reached: `~/.local/share/kilo` and `~/.local/state/kilo` both carry the run's date and an empty `repos` directory was created under the first, though every file already there is intact and unmodified and `~/.cache/kilo` was never touched. Move `HOME` and every `XDG_*` variable, which is what the wrapper described above ended up doing. A provider's own config-dir override is a statement about where it reads, not a promise about where it writes.

**The `TMUX` trap recorded in the Pi section above is real, and this run rediscovered it.** A single `tmux` invocation made outside the env file every other command sourced created a short-lived session on the maintainer's default server. It ended on its own and nothing of theirs was touched. The rule is to put `unset TMUX` in the file every command sources, not to remember it at each call site.

### Sanitization

Sanitization is by construction, as everywhere else in this directory: the tracer recorded event names and payload field **names**, and never had prompt text, response text, tool arguments, tool results, file contents, or environment values. Session identifiers were mapped to `session-N` placeholders inside the tracer process, so no real identifier ever reached a file. The tracer also dropped the per-token `message.part.delta` and `message.part.updated` streams and the startup catalog chatter, which carry no lifecycle information and would bury the fixture.

Four keys carry a value, under the same narrow rule the Pi section states, and each is asserted in `valueBearingTraceKeys`:

| Key | Why a value is safe |
| --- | --- |
| `status` | Kilo's own `session.status` discriminator, closed by its shipped schema at `idle`, `busy`, `retry` and `offline` |
| `error` | A bounded error class name, truncated to 64 bytes, never the message or the payload |
| `info.id` | `present` only, never an identifier |
| `info.parentID` | `present` or `absent` only, which is what records that no trace here contains a subagent |

## Kimi Code CLI

Kimi's traces use the same six-column hook layout. They are the first here whose
provider half is a *table of configuration entries* rather than a script, so what
they have to prove is slightly different: not that a state machine is right, but
that every event upstream's twelve rows depend on really fires, in the order the
ladder assumes, on a released Kimi.

Two values are recorded that no earlier trace carried, both added to
`valueBearingTraceKeys` deliberately. `source` is `SessionStart`'s own
discriminator (`startup` or `resume`) and `client_type` names which client
produced the payload; both are enumerations in Kimi's published hooks reference.
Kimi's `reason` — which is what distinguishes a cancelled `Interrupt` from an
exiting `SessionEnd` — was already allowlisted. The tool name is deliberately
*not* recorded as a field value even though the payload carries `tool_name`: it
already has a column of its own, and recording it twice would widen the
allowlist for nothing.

### How they were captured, and what was not touched

kimi-code 0.40.1 was installed into a scratch prefix with the vendor's own
`KIMI_INSTALL_DIR` and `KIMI_NO_MODIFY_PATH=1`, so nothing reached the user's
`PATH` or shell rc. Its whole data directory was redirected with
`KIMI_CODE_HOME`; the user has no `~/.kimi-code` and one was never created. The
provider was pointed at OpenRouter through a `type = "openai"` entry in the
scratch `config.toml`, using a credential the environment already exported, with
`telemetry = false`.

Sidecar's own block was installed with `sidecar agent integration install kimi`
rather than by hand, so the run proves the installer too, and `kimi doctor`
reported the resulting file valid — which is what shows Kimi accepts the
negative-lookahead matcher upstream's table uses. Kimi was then driven in a real
TUI inside a Sidecar-managed shell on a **private tmux server**, with
`XDG_STATE_HOME`, `-config` and `SIDECAR_ISOLATED_STATE=1` under a scratch tree.

One trap beyond the `TMUX` one recorded above, and it is the same shape. This
harness ran inside the maintainer's own Sidecar shell, and `SIDECAR_TMUX_SERVER`
leaked from that environment into the shell the run created: the new pane's
environment claimed server 87110 while its private server was 91044, and every
hook report was refused with `lifecycle_invalid_context`. The private socket was
never at risk — the session really was on the private server — but the evidence
was, and the failure is silent from the provider's side because a hook surface
fails open. **Scrub every `SIDECAR_*` variable, not only `TMUX`, before creating
a shell in a proof run.**

A second, smaller one: a macOS login shell re-runs `path_helper`, which puts
`/etc/paths.d` entries ahead of an inherited `PATH`. A `SIDECAR_BIN` shim placed
first in the harness's `PATH` therefore lost to the Homebrew-installed `sidecar`
inside the created pane. Export the `PATH` again *in the pane* before starting
the provider, and check `command -v sidecar` before trusting a run.

## Re-capturing

There is no `-update` flag. These are hand-captured real traces against a paid
provider, and regenerating one should be a deliberate act with a recorded
version, not a side effect of running the suite. To add a provider or refresh a
version, repeat the procedure above and update both the table here and
`capabilities.json`.
