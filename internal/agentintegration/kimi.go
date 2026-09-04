package agentintegration

import (
	"strings"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The Kimi Code CLI integration.
//
// # Why this port has no script and no assets/kimi directory
//
// Herdr's kimi integration is two things: a table of twelve (event, matcher,
// action) rows in Rust, and a shell script dropped into ~/.kimi-code/hooks that
// every one of those rows invokes. The table is the provider half -- which hook
// event means working, blocked, idle, or a session reference -- and it is kept
// here verbatim. The script is the whole of the transport half, and it does
// exactly three things: gate on HERDR_ENV and HERDR_SOCKET_PATH, shell out to
// python3 to pull session_id off the hook payload on stdin, and write one
// JSON-RPC frame to a unix socket.
//
// None of those three jobs survives the transport swap. Sidecar's transport is
// a CLI verb, and `sidecar agent report` is itself the gate: it is a hook
// surface that exits 0 and prints nothing outside a Sidecar-managed shell.
// `sidecar agent report-session --hook-stdin` reads the provider payload itself,
// with a bounded reader, so the python3 dependency goes away rather than being
// reimplemented. What is left for a script to do is nothing, so there is no
// script: the twelve config entries invoke the CLI directly.
//
// That is not a new shape for Sidecar. Herdr's claude and codex integrations are
// shell scripts referenced from settings.json and hooks.json in exactly the same
// way, and Sidecar's ClaudeAdapter and CodexAdapter already dropped the script
// and put the CLI command in the config entry. Kimi is the same shape with
// twelve entries instead of one, so it follows the same adapter, the same
// Ownership.OwnsEntry model, and the same "the file is the user's, one region
// inside it is Sidecar's" rule.
//
// # The one-hook-per-event property, which is load-bearing
//
// Kimi runs every hook matching an event in PARALLEL (Kimi Code CLI hooks
// reference, "when multiple rules match the same event, all matching hooks run
// in parallel"). Two hooks firing for one event would be two `sidecar agent
// report` processes racing for a store sequence, and the order they landed in
// would be the order the scheduler happened to run them.
//
// Upstream's table is built so that never happens: the only event with two rows
// is PreToolUse, and its two matchers are exact complements -- `^AskUserQuestion$`
// and `^(?!AskUserQuestion$).*$`. Every other event has exactly one row. So each
// (event, tool) pair fires exactly one hook, and there is no intra-event
// ordering question to answer. TestKimiFiresExactlyOneHookPerEvent pins it,
// because a later row added without a complementary matcher would reintroduce
// the race silently.
//
// # Deliberate differences from upstream
//
//  1. The transport is Sidecar's, per the rule above.
//  2. NO --seq is passed. Upstream stamps time.time_ns() into its own seq field,
//     which its socket does not bound; Sidecar's store bounds the field at
//     MaxSequence and assigns under the lock it already holds for the append.
//     The Pi port shipped a clock-seeded counter twice and dropped every report
//     both times, silently, and the fix recorded in the parity plan is to omit
//     the flag entirely. See PiReportArgs, which carries the full account.
//  3. Each row carries a bounded Sidecar reason code. Upstream sends only a
//     lane, because Herdr's wire has no reason vocabulary. The reason is what
//     makes `sidecar agent explain` able to say which provider event authored a
//     pane's state, and every value below is from the frozen allowlist in
//     agentlifecycle.Reasons().

// Kimi integration identity.
const (
	KimiProvider = "kimi"
	KimiSource   = "sidecar.kimi.hooks"

	// KimiAssetVersion is the bundled block's version. Authority is granted to a
	// source at a version, so this changing is what makes an installed copy
	// "outdated" rather than merely different.
	//
	// Bump it whenever the rendered block changes, once a release has shipped
	// `agent integration install kimi`. Until then the bytes may be revised in
	// place, because there is no earlier copy of version 1 anywhere to be
	// misread. asset_golden_test.go states the whole bump order.
	KimiAssetVersion = "1"

	// KimiAssetSchema is the marker schema the managed block declares. It
	// changes only when the marker line's own format changes, which is why it is
	// separate from the asset version.
	KimiAssetSchema = 1

	// KimiConfigName is the file Kimi Code CLI reads its hooks from.
	KimiConfigName = "config.toml"

	// KimiBackupSuffix names the recoverable copy kept beside config.toml before
	// any rewrite of a pre-existing file.
	KimiBackupSuffix = ".sidecar-backup"
)

// The two matchers upstream uses on PreToolUse, kept as constants because they
// are exact complements of one another and a change to either that is not
// mirrored in the other reintroduces the parallel-hook race described above.
//
// The negative lookahead is a provider-side regular expression: Kimi evaluates
// it, not Sidecar. It is kept verbatim from upstream rather than rewritten,
// because rewriting a matcher is rewriting the provider half.
const (
	kimiAskUserQuestionMatcher = "^AskUserQuestion$"
	kimiOtherToolMatcher       = "^(?!AskUserQuestion$).*$"
)

// KimiHook is one row of the provider half: which Kimi event, filtered by which
// matcher, becomes which Sidecar report.
type KimiHook struct {
	// Event is the Kimi hook event name.
	Event string
	// Matcher is Kimi's regular expression over the event's target, or empty
	// for "every target", which is how Kimi spells an absent matcher key.
	Matcher string
	// State is the lane this event reports, or empty for the session hook,
	// which reports a conversation reference rather than a lane.
	State agentactivity.State
	// Reason is the bounded reason code the report carries.
	Reason agentlifecycle.ReasonCode
	// Why records what this row means, in one line, so a reader of the
	// installed config.toml block is not left to infer it from an event name.
	Why string
}

// KimiHooks is the provider half, ported row for row from Herdr's
// KIMI_HOOK_EVENTS at HERDR_INTEGRATION_VERSION=7, in upstream's order.
//
// The order is upstream's and is preserved for review rather than for
// behaviour: Kimi matches hooks by event, not by position, so nothing in the
// runtime depends on it, and a reviewer diffing this against
// src/integration/mod.rs should see the same twelve rows in the same sequence.
//
// The lane of each row is upstream's. The reason code is Sidecar's, and each one
// is the narrowest value in the frozen vocabulary that is true of the event:
//
//   - UserPromptSubmit is the user handing the model a turn, so turn_start.
//   - PreToolUse on an ordinary tool is work continuing, so tool_use.
//   - PreToolUse on AskUserQuestion is the model asking the human something,
//     which is `question` and not `permission_request`: the vocabulary
//     distinguishes them and Kimi has a separate PermissionRequest event.
//   - PostToolUse and PostToolUseFailure on AskUserQuestion both mean the
//     question is over however it ended, so permission_resolved.
//   - SubagentStart and PreCompact are work the pane is doing that no prompt
//     started, and both have their own reason codes.
//   - PermissionRequest and PermissionResult are the permission pair.
//   - Stop is turn completion.
//   - Interrupt is the user cancelling, so `cancelled` rather than
//     turn_complete. Both report idle; only the reason differs, and it is the
//     reason that tells a later reader whether the turn finished.
var kimiHooks = []KimiHook{
	{
		Event:  "SessionStart",
		Reason: "", Why: "bind the pane to the conversation Kimi just started or resumed",
	},
	{
		Event: "UserPromptSubmit", State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonTurnStart, Why: "the user submitted a prompt, so a turn has begun",
	},
	{
		Event: "PreToolUse", Matcher: kimiOtherToolMatcher, State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonToolUse, Why: "a tool that is not a question is about to run",
	},
	{
		Event: "PreToolUse", Matcher: kimiAskUserQuestionMatcher, State: agentactivity.StateBlocked,
		Reason: agentlifecycle.ReasonQuestion, Why: "the model is asking the user a question and cannot proceed until it is answered",
	},
	{
		Event: "PostToolUse", Matcher: kimiAskUserQuestionMatcher, State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonPermissionResolved, Why: "the question was answered, so the pane is working again",
	},
	{
		Event: "PostToolUseFailure", Matcher: kimiAskUserQuestionMatcher, State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonPermissionResolved, Why: "the question ended without an answer, so the pane is no longer blocked on it",
	},
	{
		Event: "SubagentStart", State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonSubagentStart, Why: "a sub-agent started, which is the pane working",
	},
	{
		Event: "PreCompact", State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonCompaction, Why: "context compaction is the pane working, not the pane idle",
	},
	{
		Event: "PermissionRequest", State: agentactivity.StateBlocked,
		Reason: agentlifecycle.ReasonPermissionRequest, Why: "the pane is waiting on the user to approve a tool",
	},
	{
		Event: "PermissionResult", State: agentactivity.StateWorking,
		Reason: agentlifecycle.ReasonPermissionResolved, Why: "the approval was answered, however it was answered",
	},
	{
		Event: "Stop", State: agentactivity.StateIdle,
		Reason: agentlifecycle.ReasonTurnComplete, Why: "the turn finished",
	},
	{
		Event: "Interrupt", State: agentactivity.StateIdle,
		Reason: agentlifecycle.ReasonCancelled, Why: "the user interrupted the turn",
	},
}

// KimiHooks returns the provider half, copied so a caller cannot reorder the
// package's own table.
func KimiHooks() []KimiHook {
	return append([]KimiHook(nil), kimiHooks...)
}

// Session reports whether this row binds a conversation rather than a lane.
func (h KimiHook) Session() bool { return h.State == "" }

// KimiHookArgv is the exact `sidecar agent ...` argv one row invokes, with the
// binary name omitted so it lines up with what the CLI's own parser is handed.
//
// NEITHER VERB CARRIES --seq. See the package comment above, and PiReportArgs,
// which carries the full account of why a per-event hook process must let the
// store assign.
//
// No payload field ever reaches this argv. The session hook passes Kimi's own
// stdin straight through to --hook-stdin, where a bounded reader takes the two
// identifiers it wants; the state hooks pass nothing but a lane and a bounded
// code. Prompt text, tool names, tool arguments and question text never appear
// on a command line Sidecar wrote.
func KimiHookArgv(h KimiHook) []string {
	if h.Session() {
		return []string{
			"agent", "report-session",
			"--kind", KimiProvider,
			"--source", KimiSource,
			"--hook-stdin",
		}
	}
	return []string{
		"agent", "report",
		"--source", KimiSource,
		"--source-version", KimiAssetVersion,
		"--provider", KimiProvider,
		"--state", string(h.State),
		"--reason", string(h.Reason),
	}
}

// KimiHookCommand is the command string one row installs into config.toml.
//
// It relies on PATH rather than embedding an absolute binary location, for the
// reason reportSessionCommand states: the hook only matters inside a
// Sidecar-managed shell, where Sidecar is on PATH by construction, and an
// embedded path would silently break every hook the next time the binary moved.
//
// Every argv element is a single shell word, so joining on a space round-trips
// through strings.Fields exactly. TestKimiCommandsRoundTripThroughFields pins
// that, because the ownership rule below reads a command by splitting it.
func KimiHookCommand(h KimiHook) string {
	return "sidecar " + strings.Join(KimiHookArgv(h), " ")
}

// KimiHookArgvCorpus is every argv the installed integration can spawn, in the
// order the hooks are declared.
//
// It is exported for TestBundledAssetsSpawnArgvTheShippedCLIAccepts in
// internal/cli, which pushes each one through the real flag parser, the real
// report construction, agentlifecycle.Validate and an append to a real store.
// For an asset that is a file of JavaScript that test has to run the file to
// learn what it spawns; for an asset that is a table of commands the table is
// the answer, and taking it from here rather than re-deriving it is what makes
// the test about the shipped bytes.
func KimiHookArgvCorpus() [][]string {
	out := make([][]string, 0, len(kimiHooks))
	for _, h := range kimiHooks {
		out = append(out, KimiHookArgv(h))
	}
	return out
}

// invokesKimiReport is the ownership test for one hook entry, and it is
// deliberately narrow in the same direction invokesReportSession is.
//
// A command is Sidecar's when its first word is the sidecar binary by base
// name, its next two words are one of the two report verbs, and it names
// Sidecar's own kimi source in a --source pair. A command that merely mentions
// any of that -- `echo sidecar agent report`, a wrapper script, a hook of the
// user's that happens to log the source name -- is not adopted, rewritten, or
// deleted.
//
// The source pair is required rather than just the verb, because config.toml is
// one file that could hold hooks for more than one Sidecar integration if kimi
// ever gained a second one, and "an entry that invokes a Sidecar verb" would
// then be an ownership rule that claimed the other integration's entries too.
func invokesKimiReport(command string) bool {
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
		if fields[i] == "--source" && fields[i+1] == KimiSource {
			return true
		}
	}
	return false
}

// baseName is filepath.Base for a command word, honouring both separators.
//
// filepath.Base alone is platform-dependent: on a unix build it does not treat
// a backslash as a separator, so a Windows-shaped command like
// `C:\tools\sidecar.exe agent report` would read as the whole path rather than
// as `sidecar.exe`, and the entry would not be recognised as Sidecar's on the
// one platform where an uninstall most needs to find it.
func baseName(command string) string {
	if i := strings.LastIndexAny(command, `/\`); i >= 0 {
		return command[i+1:]
	}
	return command
}
