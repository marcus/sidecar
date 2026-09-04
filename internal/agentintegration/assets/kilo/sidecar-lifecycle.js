// sidecar-integration: id=sidecar.kilo.plugin schema=1 version=1
//
// The line above is what makes this file Sidecar's. The installer identifies an
// asset it may replace or remove by that marker and by nothing else -- not by
// its name, and not by where it sits. A file called sidecar-lifecycle.js
// without the marker is somebody else's, and Sidecar refuses to touch it.
//
// Sidecar lifecycle integration for Kilo Code.
//
// Kilo is an OpenCode fork, so this is the OpenCode plugin contract: a module
// export that is a function is called as a plugin factory, and the object it
// returns is a bag of hooks. Two of those hooks matter here, `chat.message` and
// `event`, and everything below is a translation of Kilo's own bus onto
// `sidecar agent report`.
//
// WHAT THIS SENDS
//
// Lanes, a bounded reason code, and Kilo's own session id. It never sends
// prompt text, response text, tool arguments or results, file paths,
// environment values, or credentials. There is no code path here that reads
// message content. It does not send a sequence at all; see buildArgs.
//
// STRUCTURE
//
// The event->report mapping is a pure function (`mapEvent`) over an explicit
// state object, with the argv builder (`buildArgs`) beside it. The runtime half
// -- spawning, ordering, timeouts -- is separate and touches no mapping logic.
// That split is what makes `TestBundledKiloAssetBehavesLikeTheHandler` possible:
// it runs THIS function against the checked-in fixtures and requires the same
// ordered reports the Go handler produces. The OpenCode pair drifted twice
// before that test existed, both times in behaviour that only appeared against a
// live provider.
//
// PROVENANCE
//
// The provider half -- which Kilo event means which lane, and every guard around
// it -- is ported from Herdr's kilo integration at HERDR_INTEGRATION_VERSION=4
// (internal/agentintegration/upstream/kilo/herdr-agent-state.js). The transport
// half is Sidecar's own. See internal/agentintegration/portedfrom.go for the
// recorded provenance, and the notes below for the four places this deliberately
// differs, each named where it happens.

import { spawn } from "node:child_process"

const SOURCE = "sidecar.kilo.plugin"
const PROVIDER = "kilo"

// VERSION is the bundled asset version, carried on every state report because
// authority is granted to a source at a version, never to a source forever.
const VERSION = "1"

// REPORT_TIMEOUT_MS bounds one report subprocess. A hung `sidecar agent report`
// -- a wedged lock, a stalled filesystem -- must not stall the queue behind it
// for the rest of the session, so it is killed and the queue moves on.
const REPORT_TIMEOUT_MS = 5000

// SESSION_STATE_BY_STATUS is upstream's status vocabulary, kept verbatim.
//
// Kilo 7.5.9's own schema (SessionStatus) admits exactly four discriminators:
// idle, busy, retry and offline. Three of the seven names here therefore cannot
// occur on that release, and `retry` -- which does -- is deliberately absent,
// because adding it would be a mapping change rather than the field-shape fix
// below. An unmapped status re-asserts the session binding instead of a lane,
// which is upstream's behaviour and is harmless: a retry happens inside a turn
// that a busy assertion already opened, so the working lane is already correct
// and nothing here has to move it.
const SESSION_STATE_BY_STATUS = new Map([
  ["idle", "idle"],
  ["active", "working"],
  ["busy", "working"],
  ["pending", "working"],
  ["running", "working"],
  ["streaming", "working"],
  ["working", "working"],
])

// stateFromSessionStatus reads the session.status discriminator.
//
// THIS IS THE ONE GENUINE UPSTREAM BUG THE PORT FIXES, and it is the same shape
// of fix the Pi port made for the same reason: Herdr fixed it in one variant of
// its own asset and not in the other.
//
// Herdr's kilo asset (version 4) accepts a status only when
// `typeof status === "string"`. Kilo's `session.status` event carries an OBJECT
// -- the schema is a union of {type:"idle"}, {type:"busy"}, {type:"retry",...}
// and {type:"offline",...} -- so upstream's kilo asset never maps a status to a
// lane at all on any release that ships that schema, and falls through to
// re-reporting the session on every status event instead. Measured, not read:
// four captures of kilo 7.5.9 in internal/agentlifecycle/testdata/traces/kilo
// record `status=busy` and `status=idle` as `properties.status.type`.
//
// Herdr's own OPENCODE asset at version 10 reads `status?.type` and has done
// since before this port; the kilo variant simply never received it. Sidecar
// takes the fixed form, because session.status is the only state-shaped signal
// Kilo has -- every emission re-asserts ground truth rather than describing a
// transition -- and an asset that cannot read it gives up the one property that
// lets a dropped or misordered event self-correct.
function stateFromSessionStatus(status) {
  const kind = typeof status === "string" ? status : status?.type
  return typeof kind === "string" ? SESSION_STATE_BY_STATUS.get(kind.toLowerCase()) : undefined
}

// newState returns the mapping's initial state.
//
// `lane` is the last lane reported and `session` the last session bound. Both
// exist only to suppress an exact repeat; see the note on `lane` in mapEvent.
function newState() {
  return { lane: "", session: "" }
}

// mapEvent is the pure event->actions mapping.
//
// It mutates `st` and returns zero or more actions, each
// {kind, state?, reason?, sessionId?}. It performs no I/O and knows nothing
// about subprocesses, which is what makes it directly comparable with the Go
// handler.
//
// The event shape is a flattening of what Kilo hands a hook: the event type (or
// the literal "chat.message" for the hook of that name, which is not a bus event
// and so cannot collide), the session id, and the raw status value. Nothing else
// from the payload is touched.
function mapEvent(st, ev) {
  const actions = []

  // adopt takes the session id the event carried. Upstream attaches the event's
  // own sessionID to every state report it sends rather than remembering one, so
  // adopting on every event is the same behaviour expressed once.
  const adopt = () => {
    if (ev.sessionId) st.session = ev.sessionId
  }

  // lane suppresses an exact repeat of the lane already reported.
  //
  // This is the FIRST deliberate difference from upstream, and it belongs to the
  // transport half. Upstream writes each report to a socket, so a repeat costs a
  // few bytes; here each report is a subprocess that takes an exclusive lock on
  // an append-only store, and Kilo emits session.status busy several times per
  // turn. Every repeat would be a process spawn and a consumed sequence that told
  // Sidecar nothing new. Sidecar's OpenCode asset suppresses the identical case
  // for the identical reason.
  const lane = (state, reason) => {
    if (st.lane === state) return
    st.lane = state
    actions.push({ kind: "report", state, reason })
  }

  // bind emits the session binding, and is the SECOND deliberate difference:
  // upstream re-reports the binding on every session.created, session.updated
  // and unmapped session.status, and Kilo emits session.updated once per message.
  // Sidecar sends it only when the session actually changes. Herdr's own opencode
  // asset at version 10 already does exactly this, comparing against a remembered
  // reportedRootSessionID, so this is upstream's newer behaviour rather than an
  // invention.
  const bind = () => {
    if (!ev.sessionId || ev.sessionId === st.session) return
    st.session = ev.sessionId
    actions.push({ kind: "session", sessionId: ev.sessionId })
  }

  switch (ev.type) {
    case "chat.message":
      // The user's message has been accepted. This is the earliest work signal
      // Kilo gives and it arrives before the first busy status.
      adopt()
      lane("working", "turn_start")
      return actions

    case "session.created":
    case "session.updated":
      bind()
      return actions

    case "session.status": {
      // adopt belongs to the two lane branches only. bind adopts as part of
      // binding, and adopting before it would make bind's own repeat check
      // compare the id against itself and suppress every rotation. Upstream has
      // no such state at all -- it hands the event's own id to whichever message
      // it sends -- so this is the one place expressing that as remembered state
      // needs care.
      const state = stateFromSessionStatus(ev.status)
      if (state === "working") {
        adopt()
        lane("working", "turn_start")
      } else if (state === "idle") {
        adopt()
        lane("idle", "turn_complete")
      } else {
        bind()
      }
      return actions
    }

    case "tool.execute.before":
    case "tool.execute.after":
      // UNREACHABLE ON KILO 7.5.9, and kept anyway because it costs one
      // comparison. These two are plugin HOOKS in Kilo -- the runtime calls
      // Plugin.trigger("tool.execute.before", ...) -- and not bus events, so an
      // `event` handler never sees them. traces/kilo/tool-turn.tsv is a turn in
      // which a bash tool really ran and neither name appears anywhere between
      // chat.message and session.idle. Upstream lists them here in both its kilo
      // and its opencode assets, so the branch is upstream's reading and is kept
      // verbatim; what the port does not do is claim `tool_use` in its capability
      // entry on the strength of a branch nothing reaches.
      adopt()
      lane("working", "tool_use")
      return actions

    case "permission.replied":
    case "question.replied":
    case "question.rejected":
      adopt()
      lane("working", "permission_resolved")
      return actions

    case "session.compacted":
      adopt()
      lane("working", "compaction")
      return actions

    case "permission.asked":
      adopt()
      lane("blocked", "permission_request")
      return actions

    case "question.asked":
      adopt()
      lane("blocked", "question")
      return actions

    case "session.error":
      // Upstream reports blocked for a session error, and the port keeps that.
      // It reads oddly and it is safe, for a reason the traces measure: a
      // session.error is followed within a millisecond by session.status idle,
      // so the blocked lane it opens is closed by the next state-shaped
      // assertion rather than latching. See traces/kilo/error-turn.tsv. This is
      // also the concrete reason the status fix above is load-bearing: without
      // it, nothing would close that lane until session.idle.
      adopt()
      lane("blocked", "provider_error")
      return actions

    case "session.idle":
      adopt()
      lane("idle", "turn_complete")
      return actions

    case "session.deleted":
      // Upstream ignores it, and so does this. A deleted session is not a
      // finished turn, and Sidecar owns process liveness for every provider.
      return actions

    default:
      return actions
  }
}

// buildArgs turns one action into the exact argv for the Sidecar CLI.
//
// Direct argv, never a shell string: nothing from Kilo is interpolated into a
// command line, and every value is either a bounded enum or an identifier
// Sidecar re-validates on the way in.
//
// NO --seq IS SENT, BY EITHER VERB, AND THAT IS THE THIRD DELIBERATE DIFFERENCE.
// Upstream numbers every message it sends, seeding the counter at
// `Date.now() * 1000`. That seed is about 1.79e15 against Sidecar's
// MaxSequence of 1 << 40, roughly 1600x over the ceiling, and the Pi port
// shipped it once and had every single report rejected -- silently, because
// reports spawn with stdio "ignore" and their exit codes are never read. Omitting
// the flag removes the class rather than re-tuning the constant:
// `sidecar agent report --help` names that as what a per-event hook process
// should do, and lifecyclestore.AppendNext assigns under the exclusive lock it
// already takes for the append. Ordering is held by the serialized queue below,
// not by the numbering.
function buildArgs(action, sessionId) {
  if (action.kind === "session") {
    const args = ["agent", "report-session", "--kind", PROVIDER, "--source", SOURCE]
    args.push("--id", action.sessionId)
    return args
  }
  const args = [
    "agent", "report",
    "--source", SOURCE,
    "--source-version", VERSION,
    "--provider", PROVIDER,
  ]
  if (sessionId) args.push("--session-id", sessionId)
  args.push("--state", action.state)
  args.push("--reason", action.reason)
  return args
}

const SidecarLifecycle = async () => {
  // FAILING OPEN. Outside a Sidecar-managed shell nothing is spawned at all and
  // no hook is even returned. Nothing in this file may delay, block, or change
  // what Kilo does.
  const bin = process.env.SIDECAR_BIN
  if (process.env.SIDECAR_MANAGED_SHELL !== "1" || !bin) return {}

  const st = newState()

  // runOnce resolves when the report process exits, or when the timeout fires,
  // whichever comes first. It never rejects: a reporting failure is diagnostic
  // and must never surface to the agent.
  const runOnce = (args) =>
    new Promise((resolve) => {
      let done = false
      const finish = () => {
        if (done) return
        done = true
        clearTimeout(timer)
        resolve()
      }
      let child
      const timer = setTimeout(() => {
        try {
          child && child.kill("SIGKILL")
        } catch (e) {
          /* ignore */
        }
        finish()
      }, REPORT_TIMEOUT_MS)
      try {
        child = spawn(bin, args, { stdio: "ignore" })
        child.on("error", finish)
        child.on("exit", finish)
      } catch (e) {
        finish()
      }
    })

  // The serialization point, and the FOURTH deliberate difference.
  //
  // Upstream's kilo asset has no queue at all: every report is an independent
  // socket write, fired concurrently. Herdr's opencode asset at version 10 added
  // a requestChain for the same reason this exists. Sidecar must serialize
  // because each report is a subprocess against an append-only store that assigns
  // a strictly increasing sequence per run in arrival order: spawning
  // concurrently assigns in order and delivers out of order, and the store
  // correctly rejects the loser. That defect silently dropped OpenCode's terminal
  // report in two live runs out of three.
  //
  // Depth is bounded by the state-change rate rather than the event rate, because
  // `lane` above suppresses exact repeats.
  let queue = Promise.resolve()
  const enqueue = (action) => {
    const args = buildArgs(action, st.session)
    queue = queue.then(() => runOnce(args)).catch(() => {})
    return queue
  }

  const handle = (ev) => {
    for (const action of mapEvent(st, ev)) enqueue(action)
  }

  return {
    "chat.message": async (input) => {
      handle({ type: "chat.message", sessionId: input?.sessionID, status: undefined })
    },

    event: async ({ event }) => {
      const properties = event?.properties ?? {}
      const sessionId =
        typeof properties.sessionID === "string" && properties.sessionID
          ? properties.sessionID
          : undefined
      handle({ type: event?.type, sessionId, status: properties.status })
    },
  }
}

// EXPORT SURFACE -- measured, not assumed, and the measurement is the reason this
// convention is kept rather than relaxed.
//
// OpenCode's plugin loader requires EVERY export of a plugin module to be a
// plugin factory: one non-function export and the whole module is imported and
// then never called, silently. Kilo 7.5.9 is more forgiving -- its loader
// (`for (let R of Object.values(A)) { ... if (!q) continue; ... }`) skips a
// non-function export and keeps the rest -- and that was measured with a probe
// plugin exporting a string beside a factory, which loaded and ran.
//
// The asset still holds itself to the stricter rule, because the cost is zero,
// because the three JavaScript assets Sidecar ships then have one export
// convention between them rather than three, and because relying on a fork being
// laxer than its upstream is a bet on a difference nobody promised to keep. So
// the pure mapping hangs off the factory as a property rather than being exported
// beside it.
SidecarLifecycle.internals = {
  newState,
  mapEvent,
  buildArgs,
  stateFromSessionStatus,
  SOURCE,
  PROVIDER,
  VERSION,
  REPORT_TIMEOUT_MS,
}

export { SidecarLifecycle }
