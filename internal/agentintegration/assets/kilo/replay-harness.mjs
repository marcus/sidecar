// Behavioral test harness for the bundled Kilo asset.
//
// Reads a fixture .tsv on argv, drives it through the asset's REAL mapping, and
// prints the ordered argv list each report would have produced as JSON. The Go
// test compares that against what KiloHandler produces from the same fixture.
//
// This is the mechanism that has caught real drift between OpenCode's shipped
// JavaScript and its Go mirror twice, in behaviours that only appeared against a
// live provider.
//
// The spawn boundary is not stubbed so much as not reached: mapEvent and
// buildArgs are pure and are what the runtime path itself calls, so this
// exercises the same code rather than a copy of it.

import { readFileSync } from "node:fs"
import { SidecarLifecycle } from "./sidecar-lifecycle.js"

const { buildArgs, mapEvent, newState } = SidecarLifecycle.internals

const fixturePath = process.argv[2]
if (!fixturePath) {
  console.error("usage: replay-harness.mjs <fixture.tsv>")
  process.exit(2)
}

// Columns: offset_ms, event, session_id, status_type, status_string.
//
// "-" means the field was absent. The two status columns are separate on
// purpose and only one may be set on a row: `status_type` builds the object
// shape kilo actually emits, `{type: "..."}`, and `status_string` builds the
// bare string shape upstream's asset is the only thing that has ever produced.
// Keeping them apart is what lets a fixture drive the upstream bug and the fix
// separately, rather than asserting the difference in prose.
const FIELDS = 5

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
  const statusType = text(cols[3])
  const statusString = text(cols[4])
  if (statusType !== undefined && statusString !== undefined) {
    console.error(`fixture row sets both status columns, which no real event can: ${line}`)
    process.exit(2)
  }
  const ev = {
    type: cols[1],
    sessionId: text(cols[2]),
    status: statusType !== undefined ? { type: statusType } : statusString,
  }
  for (const action of mapEvent(st, ev)) {
    emitted.push(buildArgs(action, st.session))
  }
}

process.stdout.write(JSON.stringify(emitted))
