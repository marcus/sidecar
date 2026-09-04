// Runtime harness for the bundled Kilo asset.
//
// Everything pinned here is invisible to a test that drives the pure mapping,
// and the whole runtime half of the asset sits in that blind spot: the replay
// harness calls mapEvent and buildArgs directly, so it never touches the hook
// names the factory returns, never touches how the event payload is flattened,
// and would pass unchanged if the asset returned a hook called "chat.messages",
// or read `properties.session_id`, or stopped reading `properties.status` at
// all. Each of those is a silent failure in production: the plugin installs,
// loads, and reports the wrong thing or nothing.
//
// So this harness calls the real factory, drives real events through the hooks
// it returns, and reports three things:
//
//   - the hook names the asset actually returned;
//   - the exact argv every report process was spawned with;
//   - the order those processes completed in.
//
// ORDERING
//
// Reports are serialized. Each one is a subprocess taking an exclusive lock on
// an append-only store that enforces a strictly increasing sequence per run, so
// spawning them concurrently assigns sequences in order and delivers them out of
// order, and the store correctly rejects the loser. That defect cost OpenCode's
// exit gate two attempts and silently dropped its terminal report in two live
// runs out of three. Upstream's kilo asset has no queue at all, which is exactly
// why this is pinned before Kilo ever runs live.
//
// The stub binary is rigged to exit in the opposite order to the one its
// processes are started in: the binding sleeps longest and the last report sleeps
// least, so
//
//   serialized  -> session, working-turn_start, blocked-permission_request, idle-turn_complete
//   concurrent  -> that list reversed
//
// The two outcomes are distinguishable by the recorded order alone, with no
// timing assertion to go flaky on a loaded machine.
//
// The stub labels itself from the REPORT'S OWN CONTENT -- the verb, then the
// state and reason -- rather than from a sequence number, which the asset does
// not send at all. Labelling by content also makes the recorded order assert the
// mapping instead of only the ordering.
//
// Usage: ordering-harness.mjs <stub-path> <order-log-path> <argv-dir>

import { writeFileSync, chmodSync, readFileSync, existsSync, mkdirSync } from "node:fs"
import { join } from "node:path"

const [stub, orderLog, argvDir] = process.argv.slice(2)
if (!stub || !orderLog || !argvDir) {
  console.error("usage: ordering-harness.mjs <stub-path> <order-log-path> <argv-dir>")
  process.exit(2)
}
mkdirSync(argvDir, { recursive: true })

writeFileSync(
  stub,
  `#!/bin/sh
label=""
state=""
reason=""
prev=""
for a in "$@"; do
  if [ "$a" = "report-session" ]; then label="session"; fi
  case "$prev" in
    --state) state="$a" ;;
    --reason) reason="$a" ;;
  esac
  prev="$a"
done
if [ -z "$label" ]; then label="$state-$reason"; fi
printf '%s\\n' "$@" > "$SIDECAR_ARGV_DIR/$label"
case "$label" in
  session) sleep 0.6 ;;
  working-turn_start) sleep 0.45 ;;
  blocked-permission_request) sleep 0.3 ;;
  *) sleep 0.1 ;;
esac
echo "$label" >> "$SIDECAR_ORDER_LOG"
`,
  "utf8",
)
chmodSync(stub, 0o755)

process.env.SIDECAR_MANAGED_SHELL = "1"
process.env.SIDECAR_BIN = stub
process.env.SIDECAR_ORDER_LOG = orderLog
process.env.SIDECAR_ARGV_DIR = argvDir

const { SidecarLifecycle } = await import("./sidecar-lifecycle.js")
if (typeof SidecarLifecycle !== "function") {
  console.error("the asset's plugin export is not a function; Kilo would never call it")
  process.exit(2)
}

const hooks = await SidecarLifecycle({})
if (!hooks || typeof hooks !== "object") {
  console.error("the factory returned no hook object")
  process.exit(2)
}

// The payload shapes are deliberately the ones Kilo really hands a plugin, not
// the flattened shape the mapping reads. `session.status` carries an OBJECT
// whose `type` is the discriminator, which is the whole of the upstream bug this
// port fixes: an asset that read `properties.status` as a string would fall
// through to a session report here and the recorded argv would say so.
const SESSION = "ses_orderharness"

hooks.event({ event: { type: "session.created", properties: { sessionID: SESSION } } })
hooks["chat.message"]({ sessionID: SESSION })
hooks.event({ event: { type: "permission.asked", properties: { sessionID: SESSION } } })
hooks.event({ event: { type: "session.status", properties: { sessionID: SESSION, status: { type: "idle" } } } })

const recorded = () =>
  existsSync(orderLog) ? readFileSync(orderLog, "utf8").trim().split("\n").filter(Boolean) : []

const started = Date.now()
const deadline = started + 30_000
while (Date.now() < deadline && recorded().length < 4) {
  await new Promise((resolve) => setTimeout(resolve, 25))
}

const order = recorded()
const argv = {}
for (const label of order) {
  const path = join(argvDir, label)
  argv[label] = existsSync(path) ? readFileSync(path, "utf8").split("\n").filter(Boolean) : []
}

process.stdout.write(
  JSON.stringify({
    order,
    argv,
    hooks: Object.keys(hooks),
    elapsedMs: Date.now() - started,
  }),
)
