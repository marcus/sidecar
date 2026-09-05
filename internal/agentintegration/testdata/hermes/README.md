# Hermes fixtures

These are hook invocations, not captured traces. Each row is one call Hermes's `invoke_hook` would make into the plugin: the hook name, the `platform` kwarg, and the `session_id` kwarg, with `-` for a kwarg Hermes does not pass at all.

The distinction from a trace matters. The captured trace lives next door in `internal/agentlifecycle/testdata/traces/hermes` and is what the capability tier rests on; these files are the branch table, and they include branches a released Hermes may never produce so that the gate is asserted rather than assumed. Which rows are measured and which are constructed is stated in each file's header.

Both readers drive the same rows: `TestBundledHermesAssetBehavesLikeTheGate` runs the shipped Python through `assets/hermes/replay-harness.py` and the Go mirror in `hermes.go` over each file, and requires identical ordered argv.
