package agentintegration

import (
	"strings"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Mastra Code CLI integration.
//
// # Why this port has no script and no assets/mastracode directory
//
// Herdr's mastracode integration is two things: a table of eleven (event,
// action) rows in Rust (MASTRACODE_HOOK_EVENTS in src/integration/mod.rs), and a
// shell script dropped into ~/.mastracode/hooks that every one of those rows
// invokes. The table is the provider half -- which hook event means working,
// blocked, idle, or a session reference -- and it is kept here verbatim. The
// script is the whole of the transport half, and it does exactly three things:
// gate on HERDR_ENV and HERDR_SOCKET_PATH, shell out to python3 to pull
// session_id off the hook payload on stdin, and write one JSON-RPC frame to a
// unix socket.
//
// None of those three jobs survives the transport swap, for the reasons kimi.go
// states in full: `sidecar agent report` is itself the gate, and
// `sidecar agent report-session --hook-stdin` reads the provider payload with a
// bounded reader, so the python3 dependency goes away rather than being
// reimplemented. What is left for a script to do is nothing, so there is no
// script: the eleven config entries invoke the CLI directly, exactly as
// ClaudeAdapter, CodexAdapter and KimiAdapter already do with the same upstream
// shape.
//
// # Sidecar needs no removed-events list, and that is a property rather than an
// omission
//
// Herdr carries MASTRACODE_REMOVED_HOOK_EVENTS -- (SessionStart, idle) and
// (SessionEnd, release), two rows an earlier integration version shipped -- and
// its installer removes those exact command strings by hand on every install and
// uninstall. Sidecar's ownership rule makes the list unnecessary: an entry is
// Sidecar's when its command invokes Sidecar's mastracode source, whatever event
// it sits under, so a stale row from any earlier asset version is stripped by the
// same pass that strips a current one. A list of superseded rows is a list that
// can go stale; a rule cannot.
//
// # Three events are BLOCKING, and a bare report on one of them can stop the agent
//
// This is the finding that most shaped the port, and it is measured from Mastra
// Code 0.38.0's own executor rather than read from a document. `runHooksForEvent`
// treats PreToolUse, Stop and UserPromptSubmit as blocking events
// (isBlockingEvent in @mastra/code-sdk/dist/hooks/types.js), and for those three
// an exit code of exactly 2 refuses the tool call or the turn:
//
//	if (result.exitCode === 2 && blocking) return { allowed: false, blockReason: ... }
//
// `sidecar agent report` exits 2 on a usage error. That is not hypothetical for
// an installed integration: hooks.json outlives the binary that wrote it, so a
// user who downgrades Sidecar below the release that added a flag this asset
// passes would find every prompt and every tool call refused by their agent, with
// Sidecar's own usage text as the refusal message. A hook surface that fails open
// is the contract `sidecar agent report --help` states, and an exit code the
// provider reads as "block" breaks it.
//
// So the three rows on blocking events end their command with `|| true`, and the
// eight rows on non-blocking events do not. The guard is narrow on purpose:
// Mastra Code surfaces any other non-zero exit as a warning line to the user,
// which is diagnostic rather than behaviour-changing and is worth keeping, and
// the eight unguarded rows are enough for a systemic failure to still be visible.
// TestMastracodeCannotBlockTheAgentItReportsOn is the guard on the guard.
//
// # The session binding is on AgentStart, not on SessionStart, and that is a
// measured correction rather than a preference
//
// Upstream binds the pane's conversation on SessionStart, which is the obvious
// event and is what every other provider's port does. On Mastra Code 0.38.0 that
// event carries no conversation.
//
// The mechanism is exact and is in the released package. `createMastraCode`
// constructs the hook manager with the LITERAL string "session-init" as its
// session id (index.js). The only thing that ever replaces it is
// `wireSessionConcerns`, which calls `hookManager.setSessionId` from a
// subscription on the session's `thread_created` and `thread_changed` events.
// And `MastraTUI.run()` dispatches `hookMgr.runSessionStart()` immediately after
// `init()`, before any thread exists. So SessionStart's payload always reads
// `"session_id": "session-init"`, on every session, in every project, on every
// machine, and no later SessionStart fires once the real thread arrives.
//
// That was measured, not inferred: the first proof run installed upstream's
// mapping unchanged and Sidecar's shells.json came back holding
// `{"kind":"id","value":"session-init","reported":true}` while the TUI was
// showing `Created thread: b42847d3-…`. A reference from an official source is
// marked resumable, so shipping that row would have made every mastracode shell
// on a machine claim the same non-existent conversation and a cold restore offer
// to resume it. agentsession's own rule is that resuming the wrong conversation
// is worse than resuming none.
//
// Every OTHER event carries the real thread id, captured in the same run:
// UserPromptSubmit, AgentStart, PermissionRequest, PermissionResult, PreToolUse,
// PostToolUse, AgentEnd and Stop all read `"session_id":
// "b42847d3-d1ce-4105-9e5b-a7d8e9daf72a"`. So the binding moves to AgentStart,
// which is non-blocking, fires once per agent run, and re-binds a pane whose
// thread changed under `/new` or a resume without needing an event for it. That
// is not a new idea either: Herdr's own pi asset re-binds per turn on its
// agent-start event for the same reason.
//
// AgentStart therefore carries two rows, the session one first, which is the
// order Herdr's pi asset also uses. Mastra Code dispatches an event's hooks
// sequentially, so the order in the array is the order they run.
//
// # Ordering is sequential, and unlike Kimi that costs nothing
//
// Mastra Code awaits each hook in turn for a given event (`for (const hook of
// applicable) { const result = await executeHook(...) }`), so two rows on one
// event cannot race the way Kimi's parallel dispatch would. That is what makes
// the AgentStart pair above safe: the session report and the state report are
// two processes, they run in array order, and the store's sequence follows the
// order the events happened rather than the order a scheduler chose. Under
// Kimi's contract the same pair would have been a coin toss.
//
// # Deliberate differences from upstream
//
//  1. The transport is Sidecar's, per the rule above.
//  2. NO --seq is passed. Upstream stamps time.time_ns() into its own seq field,
//     which its socket does not bound; Sidecar's store bounds the field at
//     MaxSequence and assigns under the lock it already holds for the append.
//     See PiReportArgs, which carries the full account of why.
//  3. Each row carries a bounded Sidecar reason code. Upstream sends only a
//     lane, because Herdr's wire has no reason vocabulary. The reason is what
//     makes `sidecar agent explain` able to say which provider event authored a
//     pane's state, and every value below is from the frozen allowlist in
//     agentlifecycle.Reasons().
//  4. The three blocking-event rows are guarded, per the section above. Herdr's
//     asset is a shell script whose every exit path is `exit 0`, so upstream has
//     this property by construction and did not need to state it; Sidecar's
//     transport is a general-purpose CLI and has to.
//  5. The session binding is on AgentStart rather than on SessionStart, per the
//     section above. It is the one row of upstream's eleven whose event moved,
//     and it moved because the event upstream chose carries a placeholder rather
//     than a conversation on every released Mastra Code this port was measured
//     against.

// Mastracode integration identity.
const (
	MastracodeProvider = "mastracode"
	MastracodeSource   = "sidecar.mastracode.hooks"

	// MastracodeAssetVersion is the bundled entry set's version. Authority is
	// granted to a source at a version, so this changing is what makes an
	// installed copy "outdated" rather than merely different.
	//
	// Bump it whenever the rendered entries change, once a release has shipped
	// `agent integration install mastracode`. Until then the bytes may be revised
	// in place, because there is no earlier copy of version 1 anywhere to be
	// misread. asset_golden_test.go states the whole bump order.
	MastracodeAssetVersion = "1"

	// MastracodeAssetSchema is the plan schema the asset declares.
	MastracodeAssetSchema = 1

	// MastracodeConfigName is the file Mastra Code reads its global hooks from.
	MastracodeConfigName = "hooks.json"

	// MastracodeBackupSuffix names the recoverable copy kept beside hooks.json
	// before any rewrite of a pre-existing file.
	MastracodeBackupSuffix = ".sidecar-backup"

	// MastracodeDirName is the directory Mastra Code keeps its global
	// configuration in, under the home directory.
	//
	// It is a bare directory NAME rather than a path because that is what Mastra
	// Code itself treats it as: `configDirName` is validated by
	// validateConfigDirName, which rejects an absolute path, either separator,
	// "." and "..", and the global hooks path is
	// path.join(os.homedir(), configDirName, "hooks.json").
	MastracodeDirName = ".mastracode"

	// mastracodeHookTimeoutMS bounds how long Mastra Code waits for one report,
	// in MILLISECONDS.
	//
	// The unit is the trap. Every other provider Sidecar installs into counts
	// this field in seconds and shares hookTimeoutSec; Mastra Code's executor
	// reads `hook.timeout ?? DEFAULT_TIMEOUT` straight into setTimeout, where
	// DEFAULT_TIMEOUT is 1e4. Writing hookTimeoutSec here would give every hook a
	// ten-millisecond budget and SIGKILL each report before it could append.
	// The value is upstream's MASTRACODE_HOOK_TIMEOUT_MS.
	mastracodeHookTimeoutMS = 10_000
)

// mastracodeBlockingEvents are the three events on which Mastra Code reads exit
// code 2 as a refusal, from isBlockingEvent in its own hooks/types module.
//
// It is a set rather than a field on each row because it is a property of the
// provider, not of the mapping: a row added later on one of these events must
// inherit the guard without anyone remembering to ask for it.
var mastracodeBlockingEvents = map[string]bool{
	"PreToolUse":       true,
	"Stop":             true,
	"UserPromptSubmit": true,
}

// mastracodeFailOpenSuffix is what keeps a report on a blocking event from
// refusing the agent's own work. See the package comment.
const mastracodeFailOpenSuffix = " || true"

// MastracodeHook is one row of the provider half: which Mastra Code event
// becomes which Sidecar report.
type MastracodeHook struct {
	// Event is the Mastra Code hook event name.
	Event string
	// State is the lane this event reports, or empty for the session hook,
	// which reports a conversation reference rather than a lane.
	State agentactivity.State
	// Reason is the bounded reason code the report carries.
	Reason agentlifecycle.ReasonCode
	// Why records what this row means, in one line. It is installed as the
	// entry's `description`, which Mastra Code's own `/hooks` command prints, so
	// a user looking at what a tool added to their configuration is told what
	// each row is for without reading Sidecar's source.
	Why string
}

// mastracodeHooks is the provider half, ported row for row from Herdr's
// MASTRACODE_HOOK_EVENTS at HERDR_INTEGRATION_VERSION=2, in upstream's order with
// one row moved.
//
// A reviewer diffing this against src/integration/mod.rs should see the same
// eleven rows in the same sequence, with one difference: upstream's first row
// binds the session on SessionStart and this one binds it on AgentStart, sitting
// immediately before AgentStart's own lane row. The section above carries the
// measurement that moved it.
//
// Order across events is preserved for review rather than for behaviour, since
// Mastra Code matches hooks by event key. Order WITHIN an event is behaviour:
// Mastra Code dispatches an event's hooks sequentially in array order, so the
// AgentStart pair binds the conversation before it reports the lane, which is the
// order Herdr's pi asset also uses.
//
// The lane of each row is upstream's. The reason code is Sidecar's, and each one
// is the narrowest value in the frozen vocabulary that is true of the event:
//
//   - UserPromptSubmit is the user handing the model a turn, and AgentStart is
//     the run that turn starts, so both are turn_start.
//   - PreToolUse is work continuing through a tool, so tool_use.
//   - PermissionRequest and PermissionResult are the permission pair. Mastra
//     Code's PermissionResult fires on every outcome -- approved, declined,
//     dismissed, auto_approved, auto_declined -- carrying it in a `decision`
//     field, so one unblocking row is sufficient and the pane cannot latch.
//   - SubagentStart and SubagentEnd both report working, because a sub-agent
//     ending returns the pane to its parent's work rather than to idle; only the
//     reason distinguishes them.
//   - Interrupt is the user cancelling, so `cancelled` rather than turn_complete.
//     Both report idle; only the reason tells a later reader whether the turn
//     finished.
//   - AgentStart carries the session binding as well as its lane, for the reason
//     the section above gives. It is the only event with two rows, and they are
//     two different reports rather than two of the same one.
//   - AgentEnd and Stop are both turn completion. Mastra Code emits AgentEnd for
//     the agent run and Stop for the turn around it, and upstream maps both to
//     idle; the port keeps that rather than picking one, because which of the two
//     arrives last is the provider's business and either one alone would leave a
//     turn open if the other were the last event of some path.
var mastracodeHooks = []MastracodeHook{
	{
		Event: "UserPromptSubmit", State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonTurnStart, Why: "the user submitted a prompt, so a turn has begun",
	},
	{
		Event:  "AgentStart",
		Reason: "", Why: "bind the pane to the conversation this agent run belongs to",
	},
	{
		Event: "AgentStart", State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonTurnStart, Why: "an agent run started, which is the pane working",
	},
	{
		Event: "PreToolUse", State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonToolUse, Why: "a tool is about to run, which is the pane working",
	},
	{
		Event: "PermissionRequest", State: agentactivity.StateBlocked,
		Reason: agentlifecycle.ReasonPermissionRequest, Why: "the pane is waiting on the user to approve a tool, a sandbox request or a plan",
	},
	{
		Event: "PermissionResult", State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonPermissionResolved, Why: "the approval was answered, however it was answered",
	},
	{
		Event: "SubagentStart", State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonSubagentStart, Why: "a sub-agent started, which is the pane working",
	},
	{
		Event: "SubagentEnd", State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonSubagentStop, Why: "a sub-agent finished, so the pane is back on its parent's work rather than idle",
	},
	{
		Event: "Interrupt", State: agentactivity.StateIdle,
		Reason: agentlifecycle.ReasonCancelled, Why: "the user interrupted the run",
	},
	{
		Event: "AgentEnd", State: agentactivity.StateIdle,
		Reason: agentlifecycle.ReasonTurnComplete, Why: "the agent run finished",
	},
	{
		Event: "Stop", State: agentactivity.StateIdle,
		Reason: agentlifecycle.ReasonTurnComplete, Why: "the turn finished",
	},
}

// MastracodeHooks returns the provider half, copied so a caller cannot reorder
// the package's own table.
func MastracodeHooks() []MastracodeHook {
	return append([]MastracodeHook(nil), mastracodeHooks...)
}

// Session reports whether this row binds a conversation rather than a lane.
func (h MastracodeHook) Session() bool { return h.State == "" }

// Blocking reports whether Mastra Code would read a non-zero exit from this
// row's command as a refusal of the agent's own work.
func (h MastracodeHook) Blocking() bool { return mastracodeBlockingEvents[h.Event] }

// MastracodeHookArgv is the exact `sidecar agent ...` argv one row invokes, with
// the binary name omitted so it lines up with what the CLI's own parser is
// handed, and without the shell guard, which is not part of the argv.
//
// NEITHER VERB CARRIES --seq. See the package comment above, and PiReportArgs,
// which carries the full account of why a per-event hook process must let the
// store assign.
//
// No payload field ever reaches this argv. The session hook passes Mastra Code's
// own stdin straight through to --hook-stdin, where a bounded reader takes the
// identifier it wants; the state hooks pass nothing but a lane and a bounded
// code. Prompt text, tool names, tool arguments and the user_message Mastra Code
// puts on UserPromptSubmit never appear on a command line Sidecar wrote.
func MastracodeHookArgv(h MastracodeHook) []string {
	if h.Session() {
		return []string{
			"agent", reportSessionVerb,
			"--kind", MastracodeProvider,
			"--source", MastracodeSource,
			"--hook-stdin",
		}
	}
	return []string{
		"agent", "report",
		"--source", MastracodeSource,
		"--source-version", MastracodeAssetVersion,
		"--provider", MastracodeProvider,
		"--state", string(h.State),
		"--reason", string(h.Reason),
	}
}

// MastracodeHookCommand is the command string one row installs into hooks.json.
//
// Mastra Code runs it through `/bin/sh -c`, which is what makes the fail-open
// guard on a blocking event expressible at all.
//
// It relies on PATH rather than embedding an absolute binary location, for the
// reason reportSessionCommand states: the hook only matters inside a
// Sidecar-managed shell, where Sidecar is on PATH by construction, and an
// embedded path would silently break every hook the next time the binary moved.
func MastracodeHookCommand(h MastracodeHook) string {
	command := "sidecar " + strings.Join(MastracodeHookArgv(h), " ")
	if h.Blocking() {
		command += mastracodeFailOpenSuffix
	}
	return command
}

// MastracodeHookArgvCorpus is every argv the installed integration can spawn, in
// the order the hooks are declared.
//
// It is exported for TestBundledAssetsSpawnArgvTheShippedCLIAccepts in
// internal/cli, which pushes each one through the real flag parser, the real
// report construction, agentlifecycle.Validate and an append to a real store.
// For an asset that is a file of JavaScript that test has to run the file to
// learn what it spawns; for an asset that is a table of commands the table is
// the answer, and taking it from here rather than re-deriving it is what makes
// the test about the shipped bytes.
func MastracodeHookArgvCorpus() [][]string {
	out := make([][]string, 0, len(mastracodeHooks))
	for _, h := range mastracodeHooks {
		out = append(out, MastracodeHookArgv(h))
	}
	return out
}

// invokesMastracodeReport is the ownership test for one hook entry, and it is
// deliberately narrow in the same direction invokesReportSession is.
//
// A command is Sidecar's when its first word is the sidecar binary by base name,
// its next two words are one of the two report verbs, and it names Sidecar's own
// mastracode source in a --source pair. A command that merely mentions any of
// that -- `echo sidecar agent report`, a wrapper script, a hook of the user's
// that happens to log the source name -- is not adopted, rewritten, or deleted.
//
// The source pair is required rather than just the verb, because hooks.json is
// one file that could hold hooks for more than one Sidecar integration if
// mastracode ever gained a second one, and "an entry that invokes a Sidecar verb"
// would then be an ownership rule that claimed the other integration's entries
// too.
//
// The trailing `|| true` on a blocking row is invisible here, because the rule
// reads from the front. That is deliberate: the guard is part of what Sidecar
// installs, but ownership must not depend on it, or removing the guard in a later
// asset version would orphan every entry the previous one wrote.
func invokesMastracodeReport(command string) bool {
	fields := strings.Fields(command)
	if len(fields) < 3 {
		return false
	}
	base := strings.TrimSuffix(baseName(fields[0]), ".exe")
	if base != "sidecar" || fields[1] != "agent" {
		return false
	}
	switch fields[2] {
	case "report", reportSessionVerb:
	default:
		return false
	}
	for i := 3; i+1 < len(fields); i++ {
		if fields[i] == "--source" && fields[i+1] == MastracodeSource {
			return true
		}
	}
	return false
}
