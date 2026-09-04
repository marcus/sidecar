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

Nothing here is a trace. These two files say what the port *does*; they are
translations of upstream's tests and of Sidecar's own report shape, and no
capture is involved in either. The evidence that a released Kimi really emits
these events, in this order, is separate and lives in
`internal/agentlifecycle/testdata/traces/kimi/` — four sanitized captures of
kimi-code 0.40.1 which are what earned this source its `advisory` tier.

The division is deliberate and worth keeping. A fixture here failing means the
port changed. A trace there failing means somebody re-measured the provider.
