# Kilo fixtures

These are **not traces**. They are event fixtures written against Herdr's own `kilo` integration at `HERDR_INTEGRATION_VERSION=4` (vendored at `internal/agentintegration/upstream/kilo/herdr-agent-state.js`) and against kilo 7.5.9's own event shapes, and they drive the mapping directly rather than recording a session.

Captured traces live in `internal/agentlifecycle/testdata/traces/kilo/`. Those are sanitized recordings of kilo 7.5.9, they are the only thing that may back an `evidence: real-trace` claim in `capabilities.json`, and Kilo has four of them. Do not move these files there, and do not move those files here: the two directories answer different questions. A trace says what the provider did; a fixture says what the mapping does with an event, including events a capture happened not to contain.

What these are for is equivalence: `TestBundledKiloAssetBehavesLikeTheHandler` replays each one through the shipped JavaScript under `node` and through `KiloHandler` in Go, and requires an identical ordered argv list. That is the test that has caught real drift between an asset and its Go mirror before, twice, in behaviour that only showed up against a live provider.

## Format

Tab separated, `#` comments ignored, five columns:

```
offset_ms  event  session_id  status_type  status_string
```

`-` means the field was absent. `event` is the bus event's own name, or the literal `chat.message` for the plugin hook of that name; kilo has no bus event called `chat.message`, so the two cannot collide.

The two status columns are separate on purpose and at most one may be set on a row. `status_type` builds the object shape kilo actually emits, `{"type": "..."}`. `status_string` builds the bare string shape, which no released kilo produces and which upstream's asset is the only thing that has ever read. Keeping them apart is what lets a fixture drive the upstream bug and the fix separately, rather than asserting the difference in prose. Both readers reject a row that sets both.

## What each fixture pins

| Fixture | What it asserts |
| --- | --- |
| `simple-turn.tsv` | The ordinary turn: a binding, then working, then idle. The second `busy` in a row emits nothing, which is the repeat suppression the transport half needs. |
| `status-object-is-read.tsv` | The upstream bug this port fixes. A `{"type":"busy"}` maps to working and a `{"type":"idle"}` to idle; upstream's string-only read maps neither and re-reports the session instead. |
| `status-string-is-still-read.tsv` | The fix is a widening, not a replacement: the bare string form upstream reads still maps, and the lookup is case-insensitive, which is upstream's own `toLowerCase`. |
| `unmapped-status-rebinds.tsv` | An unmapped status (`retry`, which kilo does emit and upstream's vocabulary does not carry) falls through to re-binding rather than asserting a lane. |
| `permission-blocks-and-resolves.tsv` | `permission.asked` blocks and `permission.replied` returns to working. This is the pair the blocked lane rests on. |
| `question-blocks-and-resolves.tsv` | The question pair does the same, and `question.rejected` after a `question.replied` is suppressed because the lane has not changed. |
| `session-error-is-blocked-then-idle.tsv` | Upstream maps every `session.error` to blocked and the port keeps it; the next `session.status` idle closes the lane rather than leaving it latched. |
| `tool-events-map-to-working.tsv` | The two tool branches, driven directly. They are unreachable against kilo 7.5.9, where tool events are plugin hooks rather than bus events, and they are kept because they cost one comparison. `tool_use` is not claimed on the strength of them. |
| `compaction-maps-to-working.tsv` | `session.compacted` reopens the working lane, which is what keeps a pane from reading idle through a compaction. |
| `session-rotation-rebinds.tsv` | A repeated `session.updated` for the session already bound emits nothing; a new id emits a fresh binding. |
| `ignored-events-report-nothing.tsv` | `session.deleted` and every event outside the switch produce nothing at all. |
| `chat-message-binds-nothing-but-carries-the-session.tsv` | `chat.message` adopts the session id without emitting a binding, and the working report it does emit carries that id as `--session-id`. |

Two upstream behaviours are deliberately not translated into fixtures. The socket retry is Herdr's transport and Sidecar replaces it with a bounded subprocess. The `agent_session_id` that upstream attaches to its own session report has no counterpart: Sidecar's binding travels on its own verb, `agent report-session`, and its shape is asserted by the argv the harnesses compare.
