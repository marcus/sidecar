// sidecar-integration: id=sidecar.omp.extension schema=1 version=1
//
// The line above is what makes this file Sidecar's. The installer identifies an
// asset it may replace or remove by that marker and by nothing else -- not by
// its name, and not by where it sits. A file called sidecar-omp-lifecycle.js
// without the marker is somebody else's, and Sidecar refuses to touch it.
//
// Sidecar lifecycle integration for OMP (oh-my-pi).
//
// WHY THIS FILE IS NOT THE PI ASSET WITH A DIFFERENT NAME
//
// OMP is a rebranded fork of Pi's codebase and its extension API is Pi's, so
// the two assets share a shape. They do not share a mapping, and the
// differences are all measured against OMP 18.1.8's own shipped TypeScript
// rather than assumed from the family resemblance:
//
//   - OMP HAS NO agent_settled EVENT AT ALL. Its ExtensionAPI.on registry
//     (src/extensibility/extensions/types.ts) ends a run on agent_end, which
//     carries a willContinue flag for the case Pi solved with a second event.
//     So the Pi port's central rule -- "turn completion is agent_settled, never
//     agent_end" -- is not portable here, and its replacement is the pair of
//     guards in the agent_end branch below.
//   - OMP HAS A PERMISSION SYSTEM. tool_approval_requested and
//     tool_approval_resolved are real, typed events carrying a toolName and an
//     approvalMode, and the `ask` tool blocks a turn on a question. Pi's blocked
//     lane is structurally unreachable; OMP's is the ordinary case.
//   - OMP RETRIES PROVIDER ERRORS BY ITSELF, and a naive adapter reports idle
//     in the middle of one. The retry hold below is upstream's answer to that.
//   - OMP GATES ON ctx.hasUI, NOT ON ctx.mode. The Pi asset gates on mode
//     because an RPC Pi session reports hasUI true while being headless. OMP
//     computes hasUI as `isInteractive || mode === "rpc-ui"` (src/main.ts:1830),
//     so it is already false for print, json and plain rpc, and upstream's OMP
//     asset uses it. Kept, because it is the provider half.
//
// WHY THIS IS .js AND NOT .ts
//
// OMP's loader accepts a bare `.ts` or `.js` file in an extension directory
// (isExtensionFile, src/extensibility/extensions/loader.ts:511-513) and OMP runs
// on Bun, so either extension loads. `node` is not Bun: it cannot import a `.ts`
// module, and the harness pattern that keeps this file and its Go mirror from
// drifting -- run the asset's own pure mapping under node, replay a fixture
// through it, compare the argv element for element -- requires exactly that.
// Shipping .ts would mean vendoring a TypeScript-capable runner into the test
// path to buy nothing OMP can tell apart. So this is .js on purpose.
//
// WHAT THIS SENDS
//
// Lanes, a bounded reason code, and -- when OMP supplies one -- the conversation
// this pane is on, as the session file's path or OMP's own session id. It never
// sends prompt text, response text, tool arguments or results, file contents,
// question text, provider error text, or environment values. There is no code
// path here that puts any of those on a command line; see buildArgs.
//
// STRUCTURE
//
// The event->report mapping is a pure function (`mapEvent`) over an explicit
// state object, exported so it can be driven directly by tests, with the argv
// builder (`buildArgs`) beside it. The runtime half -- spawning, ordering,
// timeouts, and the two setTimeout timers -- is separate and touches no mapping
// logic.
//
// The timers are the one structural difference from the Pi and Kilo assets, and
// they are why `mapEvent` emits `schedule` and `cancel` actions rather than
// owning a clock. A mapping that called setTimeout could not be replayed against
// a Go mirror over a fixed fixture, and the debounce is exactly the kind of
// behavior that drifts silently. So the mapping decides WHEN a timer should be
// armed and the runtime owns the clock; a timer firing re-enters the mapping as
// an ordinary event (`idle_timer`, `retry_timer`), which is what makes both
// halves drivable from the same .tsv.
//
// PROVENANCE
//
// The provider half -- which OMP event means which lane, the retry hold, the
// idle debounce, the blocked ladder, and every guard around them -- is ported
// from Herdr's omp integration at HERDR_INTEGRATION_VERSION=9
// (internal/agentintegration/upstream/omp/herdr-agent-state.ts) and is kept
// verbatim in behavior. The transport half is Sidecar's own. See
// internal/agentintegration/portedfrom.go for the recorded provenance, and the
// notes below for the places this deliberately differs, each named where it
// happens.

import { spawn } from "node:child_process"

const SOURCE = "sidecar.omp.extension"
const PROVIDER = "omp"

// VERSION is the bundled asset version, carried on every state report because
// authority is granted to a source at a version, never to a source forever.
const VERSION = "1"

// REPORT_TIMEOUT_MS bounds one report subprocess. A hung `sidecar agent report`
// -- a wedged lock, a stalled filesystem -- must not stall the queue behind it
// for the rest of the session, so it is killed and the queue moves on.
const REPORT_TIMEOUT_MS = 5000

// IDLE_DEBOUNCE_MS and RETRY_GRACE_MS are upstream's two constants, unchanged.
//
// The debounce exists because OMP can end a run and immediately start another
// one; publishing idle the instant agent_end arrives makes a pane flicker
// through idle between two halves of the same piece of work. The grace period is
// how long a retryable provider error is held as `working` before it is admitted
// to be a block: OMP retries by itself, so a failure that is about to be retried
// is still work in progress, and only a failure that is still there after the
// grace has run is something a human has to look at.
const IDLE_DEBOUNCE_MS = 250
const RETRY_GRACE_MS = 2500

// BLOCKED_CHANNEL is the untyped event-bus channel a cooperating extension
// publishes a block on.
//
// Upstream listens on "herdr:blocked". Sidecar listens on its own namespace
// instead, and that is the one deliberate rename in the provider half. Herdr's
// channel is Herdr's protocol: if Sidecar consumed it, a machine with both
// projects installed would have one project's approval protocol driving the
// other project's lane, which is precisely the identity collision the parity
// plan's first decision refuses.
//
// Unlike Pi's, this branch is NOT the only producer of `blocked` here, and it is
// not what the blocked lane rests on. OMP has typed approval events and an `ask`
// tool, both handled below, so the bus channel is the same cooperative
// extension-to-extension protocol it is upstream and nothing published it during
// this port's trace runs. It is kept because it costs one comparison and because
// removing a branch upstream has is not a port.
const BLOCKED_CHANNEL = "sidecar:blocked"

// RETRYABLE_ERROR is upstream's classifier, character for character.
//
// It decides whether a failed run is one OMP will retry (hold the pane at
// `working`) or one a human has to see (`blocked`). Keeping it verbatim is the
// point: the list is a record of which provider error strings OMP's own retry
// path recovers from, and narrowing it here would make Sidecar report a block
// OMP is about to clear by itself.
//
// It has no lookahead, no backreference and no named group, so Go's RE2 accepts
// it unchanged and the Go mirror runs the same pattern rather than an
// approximation of it. The flags differ only in spelling: /i here, (?i) there.
const RETRYABLE_ERROR =
  /overloaded|provider.?returned.?error|rate.?limit|too many requests|429|500|502|503|504|service.?unavailable|server.?error|internal.?error|network.?error|connection.?error|connection.?refused|connection.?lost|websocket.?closed|websocket.?error|other side closed|fetch failed|upstream.?connect|reset before headers|socket hang up|ended without|http2 request did not get a response|timed? out|timeout|terminated|retry delay/i

// newState returns the mapping's initial state.
//
// The field names are upstream's, because the ladder they feed is upstream's.
// `lastState`/`lastMessage` exist only to suppress an exact repeat, and
// `blockedMessage`/`failureMessage` are carried purely so that suppression
// behaves identically -- neither is ever transmitted. See buildArgs.
//
// The two durations live in the state rather than being read from the
// environment inside the mapping, because the mapping does no I/O of any kind
// and because a replay has to be able to pin them.
function newState(options) {
  return {
    rootSession: false,
    agentActive: false,
    retryHoldActive: false,
    failureBlocked: false,
    failureMessage: undefined,
    blockedCount: 0,
    blockedMessage: undefined,
    lastState: undefined,
    lastMessage: undefined,
    sessionPath: undefined,
    sessionId: undefined,
    idleDebounceMs: options?.idleDebounceMs ?? IDLE_DEBOUNCE_MS,
    retryGraceMs: options?.retryGraceMs ?? RETRY_GRACE_MS,
  }
}

// isAbsoluteSessionPath reports whether a session file path may be reported.
//
// This is upstream's OMP form, and it is the half of the two assets that Pi's
// never received: Herdr's Pi asset accepts a session path only when it
// `startsWith("/")`, which silently discards every Windows path, and the OMP
// variant fixed it and has a test for it
// (upstream/herdr-agent-state.test.ts:232-241). Sidecar's Pi port adopted the
// fixed form; this is where it came from.
//
// Sidecar's own `agent report-session --path` re-validates and additionally
// requires the path to sit inside one of the provider's approved store roots, so
// this is a filter on what is worth sending, not a security boundary.
function isAbsoluteSessionPath(value) {
  if (typeof value !== "string" || value.length === 0) return false
  if (value.startsWith("/")) return true
  // C:\... and C:/..., the two shapes a Windows absolute path arrives in.
  return /^[A-Za-z]:[\\/]/.test(value)
}

// parseDurationEnv is upstream's, and it is here rather than in the runtime half
// because upstream's own test drives the debounce to zero through it and this
// port keeps that lever.
function parseDurationEnv(raw, fallback) {
  if (!raw) return fallback
  const parsed = Number.parseInt(raw, 10)
  if (!Number.isFinite(parsed) || parsed < 0) return fallback
  return parsed
}

// retryableErrorMessage decides whether a finished run failed in a way OMP will
// retry.
//
// Upstream reads the last assistant message out of `event.messages` and looks at
// its `stopReason` and `errorMessage`. The flattened event carries exactly those
// two fields and nothing else from the message list, which is deliberate: an
// AgentMessage is the whole conversation turn, and the mapping has no business
// being able to see it.
function retryableErrorMessage(ev) {
  if (ev.stopReason !== "error") return undefined
  const message = typeof ev.errorMessage === "string" ? ev.errorMessage : ""
  if (!RETRYABLE_ERROR.test(message)) return undefined
  return message || "retryable provider error"
}

// askBlockedMessage is upstream's label for a turn blocked on the `ask` tool.
// Upstream reads the first entry of args.questions that has a string question;
// the flattened event carries that one string, because the rest of the payload
// is model-authored text this file must never hold.
function askBlockedMessage(ev) {
  if (typeof ev.question === "string" && ev.question.length > 0) return ev.question
  return "waiting for user input"
}

// mapEvent is the pure event->actions mapping.
//
// It mutates `st` and returns zero or more actions, each one of:
//
//   {kind: "report", state, reason}          a lane report
//   {kind: "session", sessionPath|sessionId} a conversation binding
//   {kind: "schedule", timer, delayMs}       arm the idle or retry timer
//   {kind: "cancel"}                         disarm both timers
//
// It performs no I/O, reads no clock and knows nothing about subprocesses, which
// is what makes it directly comparable with the Go handler.
function mapEvent(st, ev) {
  const actions = []
  // Upstream's activateRootSession calls updateSessionRef and reportSession, and
  // three of the handlers below then call both again on the same event. On
  // Herdr's socket that is one extra frame; here it would be one extra
  // subprocess spawning `agent report-session` with byte-identical argv. So a
  // binding is emitted at most once per event. The per-TURN re-binding upstream
  // does is kept in full -- it is what recovers a binding Sidecar lost to a
  // restart mid-session -- and only the within-one-event duplicate is dropped.
  let boundThisEvent = false

  // desiredState is upstream's ladder, unchanged, and its order is load-bearing
  // at every rung. An explicit block outranks a provider failure, because a
  // human is being asked something; a provider failure outranks working; a retry
  // hold reads as working, because OMP is still going to do the work.
  const desiredState = () => {
    if (st.blockedCount > 0) return { state: "blocked", message: st.blockedMessage }
    if (st.failureBlocked) return { state: "blocked", message: st.failureMessage }
    if (st.agentActive || st.retryHoldActive) return { state: "working", message: undefined }
    return { state: "idle", message: undefined }
  }

  // publishState suppresses an exact repeat unless forced. `force` exists for
  // two callers, session_start and session_switch, which re-assert the lane even
  // when it has not changed: a reload replaces this extension mid-run and
  // Sidecar has no record of what the previous instance reported.
  const publishState = (reason, force) => {
    const next = desiredState()
    if (!force && next.state === st.lastState && next.message === st.lastMessage) return
    st.lastState = next.state
    st.lastMessage = next.message
    actions.push({ kind: "report", state: next.state, reason })
  }

  const updateSessionRef = () => {
    st.sessionPath = isAbsoluteSessionPath(ev.sessionPath) ? ev.sessionPath : undefined
    st.sessionId = typeof ev.sessionId === "string" && ev.sessionId.length > 0 ? ev.sessionId : undefined
  }

  // reportSession emits the binding, or nothing when OMP has told us nothing to
  // bind. Path is preferred over id because a path identifies the exact
  // transcript a restore would resume, which an id alone does not.
  const reportSession = () => {
    if (boundThisEvent) return
    boundThisEvent = true
    if (st.sessionPath) {
      actions.push({ kind: "session", sessionPath: st.sessionPath })
      return
    }
    if (st.sessionId) {
      actions.push({ kind: "session", sessionId: st.sessionId })
    }
  }

  const cancelTimers = () => {
    actions.push({ kind: "cancel" })
  }

  const clearFailureState = () => {
    st.retryHoldActive = false
    st.failureBlocked = false
    st.failureMessage = undefined
  }

  // activateRootSession is upstream's, including the part that reads oddly: it
  // is called both to OPEN the session and, from four later handlers, to adopt a
  // session whose session_start this extension was not loaded for. The hasUI
  // gate is checked every time, so a headless invocation can never latch.
  const activateRootSession = () => {
    if (ev.hasUI !== true) return false
    st.rootSession = true
    updateSessionRef()
    reportSession()
    return true
  }

  const activateBlocked = (message) => {
    cancelTimers()
    st.blockedCount += 1
    st.blockedMessage = message
    publishState(ompBlockedReason(ev), false)
  }

  const deactivateBlocked = () => {
    st.blockedCount = Math.max(0, st.blockedCount - 1)
    if (st.blockedCount === 0) st.blockedMessage = undefined
    publishState("permission_resolved", false)
  }

  switch (ev.type) {
    case "session_start": {
      // The gate is hasUI rather than mode, which is the opposite of the Pi
      // asset's choice and is correct for this provider: OMP sets hasUI from
      // `isInteractive || mode === "rpc-ui"`, so print, json and plain rpc are
      // already excluded. See the header note.
      if (!activateRootSession()) return actions
      // A reload can replace this extension mid-run without emitting another
      // agent_start, so the run's true state is read back from ctx rather than
      // assumed idle. `=== false` and not `!== true`: an absent isIdle means
      // "unknown", and unknown must not be reported as working.
      st.agentActive = ev.idle === false
      publishState("session_start", true)
      return actions
    }

    case "session_switch": {
      if (!activateRootSession()) return actions
      // A switch is a different conversation in the same process: every counter
      // from the previous one is meaningless and is dropped rather than carried.
      cancelTimers()
      clearFailureState()
      st.agentActive = false
      st.blockedCount = 0
      st.blockedMessage = undefined
      publishState("session_change", true)
      return actions
    }

    case "agent_start": {
      if (!st.rootSession && !activateRootSession()) return actions
      updateSessionRef()
      reportSession()
      cancelTimers()
      clearFailureState()
      st.agentActive = true
      publishState("turn_start", false)
      return actions
    }

    case "agent_end": {
      if (!st.rootSession) return actions
      // OMP can emit duplicate or late end events while auto-retry is already
      // holding the pane at working. An unqualified duplicate end must not
      // cancel the retry hold and publish a false idle.
      if (!st.agentActive) return actions
      // A continuation is already scheduled, so this end is not a settle. Older
      // builds omit the field and fall through as before, which is why this is
      // `=== true` and not a truthiness test.
      if (ev.willContinue === true) return actions

      st.agentActive = false

      const retryable = retryableErrorMessage(ev)
      if (retryable) {
        // holdForRetry: stay at working, and arm the timer that admits the
        // failure if the retry never lands.
        cancelTimers()
        st.retryHoldActive = true
        st.failureBlocked = false
        st.failureMessage = retryable
        publishState("provider_error", false)
        actions.push({ kind: "schedule", timer: "retry", delayMs: st.retryGraceMs })
        return actions
      }

      // scheduleIdle: debounce, so a run that is immediately followed by another
      // does not flicker the pane through idle.
      cancelTimers()
      clearFailureState()
      actions.push({ kind: "schedule", timer: "idle", delayMs: st.idleDebounceMs })
      return actions
    }

    case "idle_timer": {
      publishState("turn_complete", false)
      return actions
    }

    case "retry_timer": {
      st.retryHoldActive = false
      st.failureBlocked = true
      publishState("provider_error", false)
      return actions
    }

    case "tool_approval_requested": {
      if (!st.rootSession && !activateRootSession()) return actions
      activateBlocked(ev.reason || `${ev.tool || "Tool"} approval`)
      return actions
    }

    case "tool_approval_resolved": {
      if (!st.rootSession && !activateRootSession()) return actions
      deactivateBlocked()
      return actions
    }

    case "tool_execution_start": {
      // Only the `ask` tool blocks. Every other tool is work in progress and is
      // already covered by the lane agent_start opened, which is why this asset
      // does not claim tool_use: subscribing to every tool would be a second
      // event stream whose ordering has to be reasoned about, to say something
      // the working lane already says.
      if (ev.tool !== "ask") return actions
      if (!st.rootSession && !activateRootSession()) return actions
      activateBlocked(askBlockedMessage(ev))
      return actions
    }

    case "tool_execution_end": {
      if (ev.tool !== "ask") return actions
      if (!st.rootSession && !activateRootSession()) return actions
      deactivateBlocked()
      return actions
    }

    case "blocked": {
      // The cooperative bus channel. See BLOCKED_CHANNEL.
      if (!st.rootSession) return actions
      if (!ev.blockedActive) {
        deactivateBlocked()
        return actions
      }
      activateBlocked(ev.blockedLabel)
      return actions
    }

    case "session_shutdown": {
      // Upstream cancels its pending timers here and reports nothing, and this
      // keeps both halves. session_shutdown is not an exit: OMP emits it for a
      // session swap as well, and releasing the lane on one would hand a live
      // pane back to screen detection in the middle of a run. Sidecar already
      // owns process liveness for every provider, so `process_exit` is not
      // claimed in the capability entry.
      if (!st.rootSession) return actions
      cancelTimers()
      return actions
    }

    default:
      return actions
  }
}

// ompBlockedReason picks the bounded Sidecar reason code for a block.
//
// Herdr's wire has no reason vocabulary at all -- it carries the label and
// nothing else -- so this is Sidecar's addition rather than a translation. The
// `ask` tool is a question rather than a permission request, and the two are
// separate codes in the frozen allowlist, so the distinction is kept.
function ompBlockedReason(ev) {
  return ev.type === "tool_execution_start" ? "question" : "permission_request"
}

// buildArgs turns one action into the exact argv for the Sidecar CLI.
//
// Direct argv, never a shell string: nothing from OMP is interpolated into a
// command line, and every value is either a bounded enum or an identifier
// Sidecar re-validates on the way in.
//
// A STATE REPORT CARRIES ONLY --session-id, never the path. Upstream's
// withSessionRef prefers the session file's path on every message it sends,
// state reports included. Sidecar splits the two: `agent report` has no path
// flag at all, because the binding is not a per-report fact -- it travels on its
// own verb, `agent report-session`, which is where reportSession above sends the
// path. What the state report needs the id for is identity, and the lifecycle
// store keeps only a host-salted fingerprint of it.
//
// NO --seq IS SENT, BY EITHER VERB. Upstream numbers every message it sends,
// seeded from `Date.now() * 1000`. Herdr's socket bounds nothing; Sidecar's
// store bounds the field at MaxSequence = 1 << 40 and enforces it
// unconditionally, so that seed sits about 1600x over the ceiling and every
// report would be rejected -- silently, because reports spawn with stdio
// "ignore" and their exit codes are never read. That is not a hypothetical: it
// is exactly what happened to the Pi asset between its proof and its review, and
// the fix there was to omit the flag rather than re-tune the seed. `sidecar
// agent report --help` names omitting it as what a per-event hook process should
// do, and `AppendNext` assigns under the lock it already holds for the append.
// The queue below is serialized, so the order the store assigns is the order the
// events happened.
//
// NEITHER THE BLOCKED LABEL NOR THE PROVIDER ERROR TEXT IS EVER IN AN ARGV. Both
// are unbounded text -- one authored by a model through the `ask` tool, one by a
// provider's error path -- `--detail` would put them into Sidecar's store, and
// this file's rule is that nothing but lanes, bounded codes and conversation
// identifiers goes over the wire. They are still carried in the mapping's state,
// because upstream compares them when suppressing a repeat and the port keeps
// that behavior exactly.
function buildArgs(action, sessionId) {
  if (action.kind === "session") {
    const args = ["agent", "report-session", "--kind", PROVIDER, "--source", SOURCE]
    if (action.sessionPath) args.push("--path", action.sessionPath)
    else if (action.sessionId) args.push("--id", action.sessionId)
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

// SidecarLifecycle is the OMP extension factory.
//
// OMP requires a module's DEFAULT export to be a function `(pi) => void |
// Promise<void>` and drops the module otherwise -- silently, with no error
// anywhere. That is the same trap Pi and OpenCode have, and it is why this
// returns nothing, why every export of this module is a function, and why the
// pure mapping hangs off the factory as a property rather than being exported
// beside it.
export default function SidecarLifecycle(pi) {
  // FAILING OPEN. Outside a Sidecar-managed shell nothing is spawned at all and
  // no handler is even registered. Nothing in this file may delay, block, or
  // change what OMP does.
  const bin = process.env.SIDECAR_BIN
  if (process.env.SIDECAR_MANAGED_SHELL !== "1" || !bin) return

  const st = newState({
    idleDebounceMs: parseDurationEnv(process.env.SIDECAR_OMP_IDLE_DEBOUNCE_MS, IDLE_DEBOUNCE_MS),
    retryGraceMs: parseDurationEnv(process.env.SIDECAR_OMP_RETRY_GRACE_MS, RETRY_GRACE_MS),
  })

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

  // The serialization point, and a deliberate difference from upstream.
  //
  // Upstream's state queue is a SINGLE SLOT that coalesces: a state enqueued
  // while another is in flight replaces whatever was waiting, so an intermediate
  // lane can be dropped and only the newest is sent. That is the right shape for
  // its transport, a socket write with a 500ms-then-1500ms retry where a slow
  // consumer could otherwise accumulate a backlog of stale lanes.
  //
  // Sidecar serializes without dropping. Each report here is a subprocess
  // against an append-only store that assigns a strictly increasing sequence per
  // run in arrival order, so ordering is the whole contract and a dropped
  // intermediate is a lane the notification lane never sees at all. Its depth is
  // bounded by the state-change rate rather than the event rate, because
  // publishState suppresses exact repeats. And a coalescing queue's output
  // depends on subprocess timing, which would make the asset and its Go mirror
  // impossible to compare over a fixed fixture -- the one test that has ever
  // caught real drift between them.
  let queue = Promise.resolve()
  const enqueue = (args) => {
    queue = queue.then(() => runOnce(args)).catch(() => {})
    return queue
  }

  // The clock the mapping deliberately does not own. A `schedule` action arms
  // one of two timers; a `cancel` action disarms both; a timer that fires
  // re-enters the mapping as an ordinary event, which is what lets a fixture
  // drive the debounced idle and the retry admission without a real clock.
  const timers = { idle: undefined, retry: undefined }
  const clearTimers = () => {
    for (const name of ["idle", "retry"]) {
      if (timers[name]) clearTimeout(timers[name])
      timers[name] = undefined
    }
  }

  const handle = (ev) => {
    for (const action of mapEvent(st, ev)) {
      switch (action.kind) {
        case "cancel":
          clearTimers()
          break
        case "schedule": {
          const name = action.timer
          const timer = setTimeout(() => {
            timers[name] = undefined
            handle({ type: `${name}_timer` })
          }, action.delayMs)
          // A pending report timer must never be the reason a finished OMP
          // process stays alive. unref is optional on purpose: a host without it
          // still works, it just holds the loop open a little longer.
          timer.unref?.()
          timers[name] = timer
          break
        }
        default:
          enqueue(buildArgs(action, st.sessionId))
      }
    }
  }

  // readCtx flattens the parts of OMP's ctx the mapping reads. Every read is
  // guarded: a host that throws from a getter must not take reporting down, and
  // an absent isIdle stays undefined rather than collapsing to a boolean, which
  // the session_start guard depends on.
  const readCtx = (ctx) => {
    let idle
    try {
      idle = ctx?.isIdle?.()
    } catch (e) {
      idle = undefined
    }
    let sessionPath
    try {
      sessionPath = ctx?.sessionManager?.getSessionFile?.()
    } catch (e) {
      sessionPath = undefined
    }
    let sessionId
    try {
      sessionId = ctx?.sessionManager?.getSessionId?.()
    } catch (e) {
      sessionId = undefined
    }
    return { hasUI: ctx?.hasUI, idle, sessionPath, sessionId }
  }

  // lastAssistantError reads the two fields retryableErrorMessage needs out of
  // agent_end's message list, and nothing else. Upstream scans the same list for
  // the last assistant message; this keeps that scan here, in the runtime half,
  // so the mapping never holds an AgentMessage at all.
  const lastAssistantError = (event) => {
    const messages = Array.isArray(event?.messages) ? event.messages : []
    for (let i = messages.length - 1; i >= 0; i -= 1) {
      const message = messages[i]
      if (message?.role !== "assistant") continue
      return {
        stopReason: message.stopReason,
        errorMessage: typeof message.errorMessage === "string" ? message.errorMessage : "",
      }
    }
    return { stopReason: undefined, errorMessage: "" }
  }

  // firstQuestion reads the one string upstream's askBlockedMessage reads.
  const firstQuestion = (event) => {
    const questions = Array.isArray(event?.args?.questions) ? event.args.questions : []
    for (const entry of questions) {
      if (typeof entry?.question === "string") return entry.question
    }
    return undefined
  }

  pi.events.on(BLOCKED_CHANNEL, (data) => {
    handle({ type: "blocked", blockedActive: !!data?.active, blockedLabel: data?.label })
  })

  pi.on("session_start", (_event, ctx) => {
    handle({ type: "session_start", ...readCtx(ctx) })
  })

  pi.on("session_switch", (event, ctx) => {
    handle({ type: "session_switch", reason: event?.reason, ...readCtx(ctx) })
  })

  pi.on("agent_start", (_event, ctx) => {
    handle({ type: "agent_start", ...readCtx(ctx) })
  })

  pi.on("agent_end", (event, ctx) => {
    handle({
      type: "agent_end",
      willContinue: event?.willContinue,
      ...lastAssistantError(event),
      ...readCtx(ctx),
    })
  })

  pi.on("tool_approval_requested", (event, ctx) => {
    handle({
      type: "tool_approval_requested",
      reason: event?.reason,
      tool: event?.toolName,
      ...readCtx(ctx),
    })
  })

  pi.on("tool_approval_resolved", (event, ctx) => {
    handle({ type: "tool_approval_resolved", tool: event?.toolName, ...readCtx(ctx) })
  })

  pi.on("tool_execution_start", (event, ctx) => {
    handle({
      type: "tool_execution_start",
      tool: event?.toolName,
      question: firstQuestion(event),
      ...readCtx(ctx),
    })
  })

  pi.on("tool_execution_end", (event, ctx) => {
    handle({ type: "tool_execution_end", tool: event?.toolName, ...readCtx(ctx) })
  })

  pi.on("session_shutdown", () => {
    handle({ type: "session_shutdown" })
  })

  // DELIBERATELY NOT SUBSCRIBED.
  //
  // turn_start / turn_end: a "turn" in this codebase's vocabulary is one
  // provider round trip, so both fire several times inside a single agent run.
  // Pi's traces measured that directly and OMP inherits the shape.
  //
  // auto_retry_start / auto_retry_end: OMP does have these, and they look like a
  // cleaner source for the retry hold than classifying an error string. They are
  // not used, because the provider half is upstream's and upstream does not use
  // them; adopting them would be a new mapping that no trace here backs. It is
  // the obvious next version of this asset, and it is recorded in the capability
  // entry as such rather than guessed at now.
  //
  // session_compact / auto_compaction_*: a compaction happens inside a run whose
  // lane is already `working`.
}

// EXPORT SURFACE.
//
// OMP drops a module whose default export is not a function, without an error,
// so the pure mapping hangs off the factory rather than being exported beside
// it. `exports-harness.mjs` asserts that every export of this module is a
// function and that the default one is the factory, because the failure mode is
// the worst kind: every test passes, the extension installs cleanly, and it
// reports nothing at all.
SidecarLifecycle.internals = {
  newState,
  mapEvent,
  buildArgs,
  isAbsoluteSessionPath,
  parseDurationEnv,
  retryableErrorMessage,
  askBlockedMessage,
  ompBlockedReason,
  SOURCE,
  PROVIDER,
  VERSION,
  REPORT_TIMEOUT_MS,
  IDLE_DEBOUNCE_MS,
  RETRY_GRACE_MS,
  BLOCKED_CHANNEL,
  RETRYABLE_ERROR,
}
