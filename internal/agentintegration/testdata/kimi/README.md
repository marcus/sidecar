# Kimi integration fixtures

These are the checked-in form of the two things a reviewer has to be able to
check without reading Go: what each Kimi hook event becomes, and what the
sequence of lanes looks like when a real turn walks through them.

`hook-table.tsv` is the provider half. It is the Sidecar-shaped translation of
Herdr's `KIMI_HOOK_EVENTS` at `HERDR_INTEGRATION_VERSION=7`
(`src/integration/mod.rs`), row for row and in upstream's order. Diff it against
that table on the next Herdr sync; `TestKimiHookTableMatchesItsRecordedFixture`
fails if the shipped `kimiHooks` and this file ever disagree, so the file cannot
quietly go stale.

Upstream has no payload fixtures for Kimi to translate. Its shared
`herdr-agent-state.test.ts` drives Pi, OMP, OpenCode and Kilo, all of which are
scripts with a state machine inside them; Kimi's asset is a transport shim with
no state, so upstream tests it in `src/integration/tests.rs` by asserting the
*config it produces*. Three of those tests are translated directly and keep
their upstream names in their Go names: the install that must preserve a
pre-existing `Notification` hook, the idempotent second install, and the
`KIMI_CODE_HOME` relocation. The fourth,
`kimi_question_hooks_report_blocked_until_the_question_finishes`, is the
AskUserQuestion ladder and is translated as an assertion over this table.

`lane-walk.tsv` is one turn's worth of events with the lane each one puts the
pane in, and it drives every branch of the ladder including the ones a
successful turn never reaches: a question, a permission request, a compaction,
a sub-agent, and an interrupt. It is fed through the real lifecycle store, the
real `StoreSource`, and the real resolver.

Nothing here is a trace. No capture of a live Kimi Code CLI session backs any of
it, which is why `capabilities.json` records this source at `screen-fallback`
with `evidence: docs-only` and an empty `covered` set. Traces, when they are
captured, go in `internal/agentlifecycle/testdata/traces/kimi/` and are what
promotes the tier.
