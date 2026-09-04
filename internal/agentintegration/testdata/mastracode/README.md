# Mastra Code integration fixtures

These are the checked-in form of the two things a reviewer has to be able to check without reading Go: what each Mastra Code hook event becomes, and what the sequence of lanes looks like when a real turn walks through them.

`hook-table.tsv` is the provider half. It is the Sidecar-shaped translation of Herdr's `MASTRACODE_HOOK_EVENTS` at `HERDR_INTEGRATION_VERSION=2` (`src/integration/mod.rs`), row for row and in upstream's order. Diff it against that table on the next Herdr sync; `TestMastracodeHookTableMatchesItsRecordedFixture` fails if the shipped `mastracodeHooks` and this file ever disagree, so the file cannot quietly go stale.

Its `blocking` column is Sidecar's own and has no counterpart upstream. Mastra Code treats `PreToolUse`, `Stop` and `UserPromptSubmit` as blocking events, and on those three an exit code of exactly 2 from a hook refuses the tool call or the turn. `sidecar agent report` exits 2 on a usage error, so the three rows on those events end their installed command with `|| true`. The column is in the fixture because a row moved onto a blocking event without the guard is the one change here that could stop a user's agent working, and it should not be possible to make it without editing this file.

Upstream has no payload fixtures for Mastra Code to translate. Its shared `herdr-agent-state.test.ts` drives Pi, OMP, OpenCode and Kilo, all of which are scripts with a state machine inside them; Mastra Code's asset is a transport shim with no state, so upstream tests it in `src/integration/tests.rs` by asserting the *config it produces*. Those tests are translated directly: the install into an existing `hooks.json` that must preserve a hook of the user's, the idempotent second install, and the uninstall that removes only Sidecar's entries.

`lane-walk.tsv` is two turns' worth of events with the lane each one puts the pane in, and it drives every branch of the ladder including the ones a successful turn never reaches: a permission request, a sub-agent pair, and an interrupt. It is fed through the real lifecycle store, the real `StoreSource`, and the real resolver.

Nothing here is a trace. These two files say what the port *does*; they are translations of upstream's tests and of Sidecar's own report shape, and no capture is involved in either. The evidence that a released Mastra Code really emits these events, in this order, is separate and lives in `internal/agentlifecycle/testdata/traces/mastracode/`.

The division is deliberate and worth keeping. A fixture here failing means the port changed. A trace there failing means somebody re-measured the provider.
