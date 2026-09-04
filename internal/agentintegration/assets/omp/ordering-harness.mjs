// Runtime harness for the bundled OMP asset.
//
// Everything pinned here is invisible to a test that drives the pure mapping,
// and the whole runtime half of the asset sits in that blind spot: the replay
// harness calls mapEvent and buildArgs directly, so it never touches readCtx,
// never touches the subscriptions, never touches the timer wiring, and would
// pass unchanged if `pi.on` registered "agent_started", if readCtx read
// getSessionId where it means getSessionFile, if lastAssistantError stopped
// scanning the message list, or if a `schedule` action never armed anything.
// Each of those is a silent failure in production -- the extension installs,
// loads, and reports the wrong thing or nothing. So this harness installs the
// real factory against a stub OMP, drives real events through it, and reports
// four things:
//
//   - the event names the asset actually subscribed to, on both registries;
//   - the exact argv every report process was spawned with;
//   - the order those processes completed in;
//   - that a debounced idle really is published by a timer the asset armed.
//
// ORDERING
//
// Reports are serialized. Each one is a subprocess taking an exclusive lock on
// an append-only store that enforces a strictly increasing sequence per run, so
// spawning them concurrently assigns sequences in order and delivers them out of
// order -- and the store correctly rejects the loser. That defect silently
// dropped OpenCode's terminal report in two live runs out of three.
//
// The stub binary is rigged to exit in the opposite order to the one its
// processes are started in: the binding sleeps longest and the last report
// sleeps least, so serialized and concurrent produce different recorded orders
// and the assertion cannot pass by luck. The two outcomes are distinguishable by
// the recorded order alone, with no timing assertion to go flaky on a loaded
// machine.
//
// The stub labels itself from the REPORT'S OWN CONTENT -- the verb, then the
// state and reason -- so the recorded order asserts the mapping as well as the
// ordering.
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
  working-session_start) sleep 0.45 ;;
  blocked-permission_request) sleep 0.3 ;;
  working-permission_resolved) sleep 0.2 ;;
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
// The debounce is driven to zero, which is the same lever upstream's own test
// pulls (HERDR_OMP_IDLE_DEBOUNCE_MS = "0" in herdr-agent-state.test.ts). It
// keeps the harness fast and it does not weaken what is asserted: zero is still
// a setTimeout, so the idle report below still has to travel through the timer
// the mapping armed rather than being published inline.
process.env.SIDECAR_OMP_IDLE_DEBOUNCE_MS = "0"

const { default: install } = await import("./sidecar-omp-lifecycle.js")
if (typeof install !== "function") {
  console.error("the asset's default export is not a function; OMP would drop the module silently")
  process.exit(2)
}

// The same shape upstream's own test harness builds: the typed listener registry
// plus the untyped string-keyed event bus. Both record which names were
// registered, because a subscription to a name OMP never emits is a whole
// extension that does nothing and says nothing.
const handlers = new Map()
const eventHandlers = new Map()
const pi = {
  on(event, handler) {
    handlers.set(event, handler)
  },
  events: {
    on(event, handler) {
      eventHandlers.set(event, handler)
      return () => {}
    },
  },
}

install(pi)

// The ctx is deliberately the shape OMP hands a listener, with the two session
// accessors returning DIFFERENT values and only one of them a path. A readCtx
// that swapped getSessionFile for getSessionId would then bind by id instead of
// by path, and the recorded argv says which one happened.
const ctx = {
  hasUI: true,
  mode: "tui",
  isIdle: () => false,
  sessionManager: {
    getSessionFile: () => "/tmp/omp-order.jsonl",
    getSessionId: () => "omp-order",
  },
}

// Delivered with no awaiting between them, which is exactly how a burst arrives
// and is the condition the race needed. session_start emits the binding and a
// forced working report; the approval pair adds one report each; agent_end adds
// none of its own and arms the idle timer, whose firing is what publishes the
// fifth.
handlers.get("session_start")({}, ctx)
handlers.get("tool_approval_requested")({ toolName: "bash", approvalMode: "ask" }, ctx)
handlers.get("tool_approval_resolved")({ toolName: "bash", approved: true }, ctx)
handlers.get("agent_end")({ messages: [{ role: "assistant", stopReason: "stop" }] }, ctx)

const recorded = () =>
  existsSync(orderLog) ? readFileSync(orderLog, "utf8").trim().split("\n").filter(Boolean) : []

const started = Date.now()
const deadline = started + 30_000
while (Date.now() < deadline && recorded().length < 5) {
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
    events: [...handlers.keys()],
    busEvents: [...eventHandlers.keys()],
    elapsedMs: Date.now() - started,
  }),
)
