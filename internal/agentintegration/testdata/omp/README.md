# OMP fixtures

These are **not traces**. They are event fixtures: three are translated from Herdr's own `herdr-agent-state.test.ts` (vendored at `internal/agentintegration/upstream/`), which drives the OMP extension handlers directly with a fake host object rather than recording a real session, and the rest drive branches that test does not reach at all.

Captured traces live in `internal/agentlifecycle/testdata/traces/<provider>/`, they are sanitized recordings of a real provider at a recorded version, and they are the only thing that may back an `evidence: real-trace` claim in `capabilities.json`. Do not move these files there to make a tier look earned.

What they are for is equivalence: `TestBundledOmpAssetBehavesLikeTheHandler` replays each one through the shipped JavaScript under `node` and through `OmpHandler` in Go, and requires an identical ordered action list. That is the test that has caught real drift between an asset and its Go mirror before. It reads the directory rather than a list, so a fixture added here is driven without anyone remembering to register it.

## Format

Tab separated, `#` comments ignored, fourteen columns:

```
offset_ms  event  reason  has_ui  idle  session_path  session_id  tool  question  blocked_active  blocked_label  will_continue  stop_reason  error_message
```

`-` means the field was absent. For `has_ui`, `idle` and `will_continue` that is a tri-state and all three are load-bearing: an absent `ctx.hasUI` is not a claim that the session has a UI, an absent `ctx.isIdle` must neither open a turn nor close one, and an absent `willContinue` is an older OMP build rather than a scheduled continuation.

`offset_ms` is documentation rather than input. Nothing here waits: the two timers the mapping arms are driven as ordinary `idle_timer` and `retry_timer` rows, which is what makes the debounce and the retry grace replayable deterministically on both sides.

## Events

The nine names OMP itself emits (`session_start`, `session_switch`, `agent_start`, `agent_end`, `tool_approval_requested`, `tool_approval_resolved`, `tool_execution_start`, `tool_execution_end`, `session_shutdown`), plus `blocked` for the untyped event-bus channel and the two synthetic timer names above.

There is no `agent_settled` row and there never will be. OMP has no such event: its `ExtensionAPI.on` registry ends a run on `agent_end`, which is why this port's guards are different from Pi's even though the two assets look alike.

## What each fixture pins

| Fixture | Upstream case | What it asserts |
| --- | --- | --- |
| `reload-preserves-working.tsv` | "reload preserves working state when the agent is active" (:186, driven for the Oh My Pi module) | A session start with `isIdle() === false` reports `working` first, not `idle`. A reload replaces the extension mid-run with no second `agent_start`. |
| `will-continue-does-not-settle.tsv` | "Oh My Pi keeps working when a turn ends with a scheduled continuation" (:501, citing OMP issue #2851) | An `agent_end` carrying `willContinue: true` publishes nothing and arms nothing; the next unqualified end still settles. |
| `windows-session-path-is-bound.tsv` | "OMP accepts POSIX and Windows session paths" (:232) | A `C:\...` path is bound rather than discarded. This is the fix Herdr's Pi asset never received and Sidecar's Pi port adopted from here. |
| `relative-session-path-is-discarded.tsv` | :240 | A relative path is not a session reference and is not sent. |
| `duplicate-agent-end-is-ignored.tsv` | — | The `agentActive` guard: a duplicate or late end arriving after the turn already closed emits nothing, so it cannot cancel a retry hold and publish a false idle. |
| `retry-hold-then-block.tsv` | — | A retryable provider error holds the pane at `working` (an exact repeat, so nothing is published) and arms the 2500ms grace; only the timer's firing publishes `blocked`. |
| `a-plain-failure-is-not-a-retry.tsv` | — | A failure the classifier does not recognise is a finished turn and takes the ordinary debounced idle path. |
| `blocked-outranks-provider-error.tsv` | — | The ladder, every rung against the next: an approval outranks an outstanding provider failure, and resolving the approval returns the pane to `blocked` rather than to `working`. |
| `approval-blocks-and-unblocks.tsv` | — | The typed permission lane, which Pi has no equivalent of at all. |
| `ask-tool-blocks-on-a-question.tsv` | — | Only the `ask` tool blocks; a `read` tool's start and end produce nothing. |
| `ask-without-a-question-still-blocks.tsv` | — | Upstream's `"waiting for user input"` fallback when no question text is readable. |
| `session-switch-resets-the-session.tsv` | — | A switch rebinds the new transcript and drops every counter from the old one, so an abandoned approval cannot hold the new session at `blocked`. |
| `headless-session-is-ignored.tsv` | — | A session with `hasUI` false produces nothing at all, on every handler that could otherwise adopt it. |
| `agent-start-adopts-an-unseen-session.tsv` | — | `activateRootSession` re-entered from a later handler, and the one binding it emits rather than two. |
| `session-id-only-binds-by-id.tsv` | — | With no session file, the binding falls back to OMP's session id. |
| `unknown-idleness-is-not-working.tsv` | — | An absent `ctx.isIdle` is unknown, and unknown is not a running turn. |
| `session-shutdown-cancels-without-reporting.tsv` | — | `session_shutdown` cancels pending timers and reports nothing. It is not an exit: OMP emits it for a session swap too. |
| `bus-channel-blocks-and-unblocks.tsv` | — | The cooperative `sidecar:blocked` channel still drives the ladder, though it is not what this provider's blocked lane rests on. |

The upstream socket-retry cases (:471 and :540) are deliberately not translated: their whole subject is Herdr's two-attempt socket write, and Sidecar replaces that transport with a bounded subprocess.
