// Behavioral test harness for the bundled OMP asset.
//
// Reads a fixture .tsv on argv, drives it through the asset's REAL mapping, and
// prints the ordered action list as JSON. The Go test compares that against what
// OmpHandler produces from the same fixture.
//
// This is the mechanism that has caught real drift between a shipped asset and
// its Go mirror -- twice in OpenCode's pair, in behaviors that only appeared
// against a live provider -- and it is why the OMP asset ships as .js: `node`
// cannot import a .ts module, and there is no version of this test that does not
// run the asset itself.
//
// A report or a binding renders as the exact argv it would spawn. A timer action
// renders as a `#`-prefixed marker instead, because the scheduling decision is
// part of the mapping and would otherwise be the one thing in this asset that
// nothing compares between the two implementations. Nothing ever spawns a
// marker: the runtime switches on the action kind before it reaches buildArgs.
//
// The spawn boundary is not stubbed so much as not reached: mapEvent and
// buildArgs are pure and are what the runtime path itself calls, so this
// exercises the same code rather than a copy of it.

import { readFileSync } from "node:fs"
import SidecarLifecycle from "./sidecar-omp-lifecycle.js"

// The mapping hangs off the factory because OMP drops a module whose default
// export is not a function. See the export-surface note in the asset.
const { buildArgs, mapEvent, newState } = SidecarLifecycle.internals

const fixturePath = process.argv[2]
if (!fixturePath) {
  console.error("usage: replay-harness.mjs <fixture.tsv>")
  process.exit(2)
}

// Columns: offset_ms, event, reason, has_ui, idle, session_path, session_id,
// tool, question, blocked_active, blocked_label, will_continue, stop_reason,
// error_message. "-" means the field was absent, which for the three boolean
// columns is a tri-state the guards depend on: an absent ctx.isIdle is unknown
// and must not be reported as working, and an absent willContinue is an older
// OMP build rather than a scheduled continuation.
const FIELDS = 14

const tri = (value) => (value === "-" ? undefined : value === "true")
const text = (value) => (value === "-" ? undefined : value)

const st = newState()
const emitted = []

for (const line of readFileSync(fixturePath, "utf8").trim().split("\n")) {
  if (line.startsWith("#") || line.trim() === "") continue
  const cols = line.split("\t")
  if (cols.length !== FIELDS) {
    console.error(`malformed fixture row (${cols.length} columns, want ${FIELDS}): ${line}`)
    process.exit(2)
  }
  const ev = {
    type: cols[1],
    reason: text(cols[2]),
    hasUI: tri(cols[3]),
    idle: tri(cols[4]),
    sessionPath: text(cols[5]),
    sessionId: text(cols[6]),
    tool: text(cols[7]),
    question: text(cols[8]),
    blockedActive: tri(cols[9]) === true,
    blockedLabel: text(cols[10]),
    willContinue: tri(cols[11]),
    stopReason: text(cols[12]),
    errorMessage: text(cols[13]),
  }
  for (const action of mapEvent(st, ev)) {
    switch (action.kind) {
      case "cancel":
        emitted.push(["#cancel"])
        break
      case "schedule":
        emitted.push(["#schedule", action.timer, String(action.delayMs)])
        break
      default:
        emitted.push(buildArgs(action, st.sessionId))
    }
  }
}

process.stdout.write(JSON.stringify(emitted))
