package agentintegration

// Where a Sidecar integration asset came from.
//
// Every asset Sidecar ships has an upstream counterpart in Herdr's
// src/integration/assets, and the valuable half of that counterpart is
// provider-specific knowledge: which hook or plugin event means working,
// blocked, idle, or a session reference, and the ordering guards around it. The
// transport half is Sidecar's own. So the maintenance question after a Herdr
// sync is never "what does this file do" but "what changed since the version we
// wrote ours against", and that question needs a recorded answer.
//
// This is that answer, and it is Go data rather than a comment in each asset for
// two reasons. The sync tool has to read it without parsing a header out of five
// different comment syntaxes, and the Claude and Codex assets are not files at
// all: they are hook entries built as Go values in claude_install.go and
// codex_install.go, so there is no header to put it in. See
// docs/plans/active/herdr-detection-parity.md, Phase 6.
//
// A record here is a claim about provenance, so it is either established from
// evidence or it is UnknownPortedVersion. It is never a guess: with "unknown"
// the sync report shows the whole current upstream file, which is the correct
// amount of work when nobody can say what the port was reviewed against.

// UnknownPortedVersion is the Version of a Sidecar asset whose upstream
// starting point could not be established. The sync report then renders the
// whole upstream file rather than a diff.
const UnknownPortedVersion = "unknown"

// PortedFrom records the Herdr integration version one Sidecar asset was
// written against.
type PortedFrom struct {
	// Provider is the Sidecar provider id, matching Adapter.Provider().
	Provider string `json:"provider"`
	// UpstreamID is the Herdr agent id the upstream assets are vendored under,
	// which is what UpstreamLock.Provider is keyed by.
	UpstreamID string `json:"upstream_id"`
	// UpstreamDir is the directory name under Herdr's src/integration/assets.
	UpstreamDir string `json:"upstream_dir"`
	// Version is the HERDR_INTEGRATION_VERSION the port was written against, as
	// a string so UnknownPortedVersion is expressible in the same field.
	Version string `json:"version"`
	// Commit is the Herdr commit carrying that version, when one is known. The
	// sync tool reads the upstream files at this commit to diff them against
	// what it just vendored, so a byte change that upstream did not bump a
	// version for is still visible.
	Commit string `json:"commit,omitempty"`
	// Evidence says where Version and Commit come from. It is prose because the
	// provenance of a port is prose; the point is that a reader can check it.
	Evidence string `json:"evidence"`
}

// herdrInspectedCommit is the Herdr commit
// docs/plans/active/notification-agent-lifecycle-hooks.md records as inspected
// while Sidecar's three integrations were written: "Herdr commit 4a3b04f5... was
// inspected for the report/release command contract, per-source sequence
// handling, hook authority resolver, capability allowlist, and provider assets."
// It is dated 2026-08-30 03:03 +0300, one day before the commits that added
// assets/opencode/sidecar-lifecycle.js, claude_install.go and codex_install.go.
const herdrInspectedCommit = "4a3b04f59ba3b7d8a15cea187b23e1e80c343b0c"

// herdrVendoredCommit is the Herdr commit upstream.lock.json pins, and therefore
// the exact bytes of upstream/pi/herdr-agent-state.ts that the Pi port was read
// against. It is a different commit from herdrInspectedCommit because the Pi
// port was written months later, from the vendored tree rather than from a
// separate inspection, and naming the wrong one would make the next sync report
// diff against a file nobody read.
const herdrVendoredCommit = "d08e44686d8b19bd9555cc99ec9068d9fde05f16"

// portedFrom is the recorded provenance of every Sidecar integration asset.
//
// Adding an adapter adds a row here, and TestEveryAdapterRecordsWhatItWasPortedFrom
// is what says so.
var portedFrom = []PortedFrom{
	{
		Provider:    OpenCodeProvider,
		UpstreamID:  "opencode",
		UpstreamDir: "opencode",
		Version:     "10",
		Commit:      herdrInspectedCommit,
		Evidence: "Herdr's opencode assets are at HERDR_INTEGRATION_VERSION=10 at the commit the " +
			"lifecycle plan records as inspected for provider assets, one day before " +
			"assets/opencode/sidecar-lifecycle.js was written. The event mapping itself was derived " +
			"from Sidecar's own traces of OpenCode 1.18.25, so this names what the port was reviewed " +
			"against rather than transcribed from.",
	},
	{
		Provider:    CodexProvider,
		UpstreamID:  "codex",
		UpstreamDir: "codex",
		Version:     "8",
		Commit:      herdrInspectedCommit,
		Evidence: "Herdr's codex asset is at HERDR_INTEGRATION_VERSION=8 at that commit, and it is the " +
			"same shape Sidecar's adapter installs: a single session-identity SessionStart hook. " +
			"Corroborated by codex_install_test.go, whose trusted-hash vector is a live codex-cli " +
			"0.151.0 trust record for Herdr's own herdr-agent-state.sh session hook.",
	},
	{
		Provider:    ClaudeProvider,
		UpstreamID:  "claude",
		UpstreamDir: "claude",
		Version:     "9",
		Commit:      herdrInspectedCommit,
		Evidence: "Herdr's claude asset is at HERDR_INTEGRATION_VERSION=9 at that commit. Sidecar's " +
			"entry is session-identity only for the reason the lifecycle plan records from the same " +
			"inspection: Herdr removed its own Claude lifecycle hook set, and tracing Claude Code " +
			"2.1.220 reproduced both halves of its stated reason.",
	},
	{
		Provider:    PiProvider,
		UpstreamID:  "pi",
		UpstreamDir: "pi",
		Version:     "8",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported line by line from the vendored upstream/pi/herdr-agent-state.ts at that commit, " +
			"where it carries HERDR_INTEGRATION_VERSION=8. The provider half is kept verbatim in " +
			"behavior: the desiredState ladder, the rootSession guard, the mode!=\"tui\" gate rather " +
			"than hasUI, the forced publish and isIdle()===false reload recovery on session_start, the " +
			"session report emitted before the first state report, the per-turn re-binding on " +
			"agent_start, and the isIdle()!==true discard on agent_settled. Every event, ctx field and " +
			"guard was re-checked against the released type definitions in Pi 0.84.3's own package " +
			"(dist/core/extensions/types.d.ts, dist/core/agent-session.js, dist/config.js) rather than " +
			"taken on trust. Four deliberate differences, each with its reason in the asset: the " +
			"transport is Sidecar's; the blocked channel is sidecar:blocked rather than Herdr's " +
			"namespace; the state queue serializes instead of coalescing; and the upstream bug that " +
			"discards a Windows session path is fixed, as Herdr's own OMP variant already had. NOT " +
			"traced: no capture of a live Pi session backs any of it, which is why the capability " +
			"entry is docs-only at session-identity.",
	},
	{
		Provider:    KiloProvider,
		UpstreamID:  "kilo",
		UpstreamDir: "kilo",
		Version:     "4",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported line by line from the vendored upstream/kilo/herdr-agent-state.js at that commit, " +
			"where it carries HERDR_INTEGRATION_VERSION=4. The provider half is kept verbatim in behavior: " +
			"the chat.message work signal, the session.created/session.updated binding, the session.status " +
			"primary signal with its fallback to re-binding, the working set (tool.execute.before/after, " +
			"permission.replied, question.replied, question.rejected, session.compacted), the blocked set " +
			"(permission.asked, question.asked, session.error), session.idle as turn completion, and the " +
			"deliberate silence on session.deleted. Every branch was re-checked against kilo 7.5.9's own " +
			"shipped binary rather than taken on trust, and four sanitized captures of that version back it. " +
			"ONE upstream bug is fixed: upstream reads session.status only when it is a string, and kilo's " +
			"event carries an object whose `type` is the discriminator, so upstream's kilo asset never maps a " +
			"status to a lane at all. Herdr's own opencode asset at version 10 reads `status?.type`; the kilo " +
			"variant never received that fix, exactly as the pi variant never received omp's Windows-path fix. " +
			"Three further deliberate differences, each with its reason in the asset: the transport is " +
			"Sidecar's; an exact repeat of the lane or of the session binding is suppressed, because each " +
			"report here is a subprocess rather than a socket write and Herdr's opencode v10 already " +
			"suppresses the binding case; and the report queue is serialized, which upstream's kilo asset does " +
			"not do and Herdr's opencode asset does. NOT copied: opencode v10's child-session tracking, which " +
			"the kilo asset has never had.",
	},
	{
		Provider:    KimiProvider,
		UpstreamID:  "kimi",
		UpstreamDir: "kimi",
		Version:     "7",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported from Herdr's kimi integration at that commit, where the vendored " +
			"upstream/kimi/herdr-agent-state.sh carries HERDR_INTEGRATION_VERSION=7. The provider half of that " +
			"integration is NOT in the vendored asset: the shell script is pure transport, and the knowledge is " +
			"the twelve (event, matcher, action) rows of KIMI_HOOK_EVENTS in src/integration/mod.rs, which are " +
			"kept row for row in kimiHooks including both PreToolUse matchers and their AskUserQuestion " +
			"complement. Every event name was re-checked against Kimi Code CLI's own published hooks reference " +
			"(the twenty-event table, the four-field [[hooks]] schema, and the stdin payload's session_id) rather " +
			"than taken on trust. Three deliberate differences, each with its reason in kimi.go: the transport is " +
			"Sidecar's, so the dropped shell script and its python3 dependency are gone and the twelve config " +
			"entries invoke the CLI directly, exactly as Sidecar's claude and codex adapters already do with the " +
			"same upstream shape; no --seq is sent, because Sidecar's store assigns; and each row carries a " +
			"bounded Sidecar reason code, which Herdr's wire has no vocabulary for.",
	},
	{
		Provider:    OmpProvider,
		UpstreamID:  "omp",
		UpstreamDir: "omp",
		Version:     "9",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported line by line from the vendored upstream/omp/herdr-agent-state.ts at that commit, " +
			"where it carries HERDR_INTEGRATION_VERSION=9. The provider half is kept verbatim in behavior: the " +
			"four-rung desiredState ladder (explicit block, then provider failure, then working-or-retry-hold, " +
			"then idle), the hasUI gate rather than a mode gate, activateRootSession being re-entered from four " +
			"later handlers so a session whose session_start this extension missed is still adopted, the forced " +
			"publish and isIdle()===false reload recovery on session_start, the full state reset on " +
			"session_switch, the per-turn re-binding on agent_start, agent_end's three guards (rootSession, a " +
			"duplicate end while inactive, and willContinue===true), the 250ms idle debounce, the 2500ms retry " +
			"grace and its error classifier verbatim, the tool_approval_requested/resolved pair, the `ask`-only " +
			"tool_execution_start/end pair with upstream's own label fallbacks, and session_shutdown cancelling " +
			"timers without reporting. Every event, ctx field and guard was re-checked against OMP 18.1.8's own " +
			"shipped TypeScript (src/extensibility/extensions/types.ts, src/extensibility/shared-events.ts, " +
			"src/main.ts, and @oh-my-pi/pi-utils/src/dirs.ts) rather than taken on trust, which is where four of " +
			"the differences from Sidecar's Pi port come from: OMP has no agent_settled event at all, it has a " +
			"real permission system, it retries provider errors itself, and its hasUI is false for print, json " +
			"and plain rpc so the Pi asset's reason for preferring ctx.mode does not apply. Five deliberate " +
			"differences from upstream, each with its reason in the asset: the transport is Sidecar's; the " +
			"blocked channel is sidecar:blocked rather than Herdr's namespace; the state queue serializes " +
			"instead of coalescing; no --seq is sent, because Sidecar's store assigns and upstream's clock seed " +
			"is about 1600x over MaxSequence; and a session binding is emitted at most once per event, because " +
			"upstream's activateRootSession and three of its handlers each report the session and on a " +
			"subprocess transport that is a duplicate spawn rather than a duplicate socket frame. Each report " +
			"also carries a bounded Sidecar reason code, which Herdr's wire has no vocabulary for. NOT copied: " +
			"remove_legacy_pi_extension_from_omp_dir, which deletes a file carrying Herdr's own pi marker out " +
			"of the omp directory. That is Herdr cleaning up a mistake of its own making; Sidecar has never " +
			"installed a Pi asset into an OMP directory, and removing a file on the strength of a marker that " +
			"is not Sidecar's own would break the ownership rule outright. The extension-directory collision " +
			"refusal in install_omp IS copied, with its reason restated in Sidecar's terms.",
	},
	{
		Provider:    AntigravityProvider,
		UpstreamID:  "agy",
		UpstreamDir: "antigravity_cli",
		Version:     "3",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported from Herdr's antigravity_cli integration at that commit, where the vendored " +
			"upstream/antigravity_cli/herdr-agent-state.sh carries HERDR_INTEGRATION_VERSION=3. The provider half is " +
			"kept: the single PreInvocation registration from ANTIGRAVITY_CLI_HOOK_EVENTS in src/integration/mod.rs, " +
			"the flat handler list rather than a matcher group, the ten second timeout, the camelCase conversationId " +
			"field, and the empty JSON object every exit path writes to stdout. Every one of those was re-checked " +
			"against agy 1.1.22's own embedded hooks documentation rather than taken on trust, and the two agree. " +
			"Three deliberate differences, each with its reason in antigravity_install.go: the transport is Sidecar's, " +
			"so the dropped shell script and its python3 dependency are gone and the config entry invokes the CLI " +
			"directly, exactly as Sidecar's claude and codex adapters already do with the same upstream shape; " +
			"ownership is by entry command rather than by Herdr's block name, so a Sidecar entry a user moved into " +
			"another block is still found and removed; and ANTIGRAVITY_CLI_CONFIG_DIR is NOT honoured, because it is " +
			"Herdr's own test seam and appears nowhere in the shipped agy binary, so following it would install into " +
			"a directory the provider never opens.",
	},
	{
		Provider:    CopilotProvider,
		UpstreamID:  "copilot",
		UpstreamDir: "copilot",
		Version:     "3",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported from Herdr's copilot integration at that commit, where the vendored " +
			"upstream/copilot/herdr-agent-state.sh carries HERDR_INTEGRATION_VERSION=3. This is the ONLY port in " +
			"this lane that could not be checked against a released binary: GitHub Copilot CLI is not installed on " +
			"any machine Sidecar has surveyed, so the file (~/.copilot/settings.json), the hooks key, the single " +
			"SessionStart registration from COPILOT_HOOK_EVENTS, the flat handler array, the `bash` command field " +
			"and the `timeoutSec` timeout are all Herdr's word rather than the provider's, and the capability entry " +
			"says so. The upstream asset's own session-id reading -- session_id falling back to sessionId, after " +
			"refusing any hook_event_name that does not normalise to sessionstart -- is what report-session's " +
			"payload parsing reproduces. Three deliberate differences, each with its reason in copilot_install.go: " +
			"the transport is Sidecar's, so the dropped shell script and its python3 dependency are gone and the " +
			"config entry invokes the CLI directly; the Windows `powershell` command field is not written, because " +
			"Sidecar has no Windows support and it would be unreachable and untestable, though the scan reads it so " +
			"an entry in that form is still removable; and Herdr's nine removed-lifecycle event names are not " +
			"copied, because Sidecar never shipped them and the ownership rule finds a Sidecar entry on any event " +
			"without a list that would go stale.",
	},
	{
		Provider:    CursorProvider,
		UpstreamID:  "cursor",
		UpstreamDir: "cursor",
		Version:     "1",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported from Herdr's cursor integration at that commit, where the vendored " +
			"upstream/cursor/herdr-agent-state.sh carries HERDR_INTEGRATION_VERSION=1. The provider half is kept: " +
			"the single sessionStart registration, the flat handler array, and the minimal `{command: ...}` entry " +
			"shape Herdr writes per Cursor's published hooks documentation. The upstream asset's own guard -- accept " +
			"a payload whose hook_event_name is absent or sessionStart, then read session_id, sessionId, " +
			"conversation_id or conversationId -- is structural here, because the entry is registered on one event " +
			"and report-session reads those field spellings itself. Every fact was re-checked against cursor-agent " +
			"2026.08.25's shipped bundle rather than taken on trust, and two of the checks changed the port. " +
			"CURSOR_CONFIG_DIR is NOT honoured, although Herdr honours it: cursor-agent has a config-dir resolver " +
			"that reads it, and its hook loader does not use that resolver, building the user hooks path from " +
			"homedir() and \".cursor\" directly, so following the variable would install into a directory the " +
			"loader never opens. And the top-level `version` member is written only into a file Sidecar creates, " +
			"where Herdr adds it to any file lacking one: the loader never reads the key, so adding it to a user's " +
			"file would edit bytes outside Sidecar's entry for no effect. One further difference: the transport is " +
			"Sidecar's, so the dropped shell script and its python3 dependency are gone.",
	},
	{
		Provider:    GrokProvider,
		UpstreamID:  "grok",
		UpstreamDir: "grok",
		Version:     "1",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported from Herdr's grok integration at that commit, where the vendored " +
			"upstream/grok/herdr-agent-state.sh carries HERDR_INTEGRATION_VERSION=1. The provider half is kept: " +
			"a dedicated hook file in grok's own hooks directory, which grok globs and merges; the SessionStart " +
			"registration in a matcher group with no matcher key, which is grok's documented match-everything " +
			"default; the ten second timeout; and the session-start-only guard, which is structural here because " +
			"the entry is registered on one event rather than dispatched to by a script. Every fact was re-checked " +
			"against the documentation grok 1.0.13 embeds in its own shipped binary rather than taken on trust. " +
			"Three deliberate differences, each with its reason in grok_install.go: the transport is Sidecar's, so " +
			"the dropped shell script and its python3 dependency are gone and the entry invokes the CLI directly; " +
			"ownership is by entry rather than by file, where Herdr deletes its whole herdr.json at uninstall, so a " +
			"hook a user added beside Sidecar's survives and the file is removed only when Sidecar's entry was all " +
			"it held; and GROK_SESSION_ID is not read, because report-session's payload reader serves every " +
			"provider and a per-provider environment read would be a second way for one provider to name a " +
			"session, so the camelCase sessionId on the payload is used instead. NOT followed: Herdr's " +
			"GROK_CONFIG_DIR, which its own comment records as a Herdr-level test seam the grok CLI does not read. " +
			"GROK_HOME, which grok does read, is honoured.",
	},
	{
		Provider:    DevinProvider,
		UpstreamID:  "devin",
		UpstreamDir: "devin",
		Version:     "2",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported from Herdr's devin integration at that commit, where the vendored " +
			"upstream/devin/herdr-agent-state.sh carries HERDR_INTEGRATION_VERSION=2. The provider half is the six " +
			"(event, action) rows of DEVIN_HOOK_EVENTS in src/integration/mod.rs -- SessionStart, UserPromptSubmit, " +
			"PreToolUse, PostToolUse, PermissionRequest and Stop, every one of them mapped to `session` -- plus the " +
			"file the entries go in (config.json, not settings.json) and the absence of a matcher, which is what " +
			"install_devin's ensure_command_hook(.., None) writes. All of that is kept verbatim. Three deliberate " +
			"differences, each with its reason in devin_install.go: the transport is Sidecar's, so the dropped shell " +
			"script and its python3 dependency are gone and the six config entries invoke the CLI directly, exactly " +
			"as the claude and codex adapters already do with the same upstream shape; no --seq is sent, because " +
			"Sidecar's store assigns; and upstream's `devin list --format json` fallback is NOT copied, because it " +
			"guesses which conversation a working directory belongs to and a wrong session binding is acted on by a " +
			"cold restore. The payload's camelCase spelling IS carried: upstream reads both session_id and sessionId, " +
			"so internal/cli's hookPayload now reads both, with a fixture for each. NOT traced: no capture of a live " +
			"Devin session backs any of it, which is why the capability entry is docs-only at screen-fallback.",
	},
	{
		Provider:    DroidProvider,
		UpstreamID:  "droid",
		UpstreamDir: "droid",
		Version:     "3",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported from Herdr's droid integration at that commit, where the vendored " +
			"upstream/droid/herdr-agent-state.sh carries HERDR_INTEGRATION_VERSION=3. The provider half is one row: " +
			"DROID_HOOK_EVENTS in src/integration/mod.rs is [(\"SessionStart\", \"session\")] and nothing else, " +
			"because upstream REMOVED its nine lifecycle rows at that version rather than keeping them; the file the " +
			"entry goes in (~/.factory/settings.json) and the absence of a matcher, which is what install_droid's " +
			"ensure_command_hook(.., None) writes, are the rest of it. All of that is kept verbatim. Every fact was " +
			"re-checked against Factory's own published hooks reference and settings reference rather than taken on " +
			"trust, and one of them is not in Herdr at all: hooks.json SHADOWS the hooks key in settings.json, so an " +
			"entry Sidecar wrote while that file existed would never fire. Sidecar inspects it and says so. Three " +
			"deliberate differences, each with its reason in droid_install.go: the transport is Sidecar's, so the " +
			"dropped shell script and its python3 dependency are gone; no --seq is sent, because Sidecar's store " +
			"assigns; and Herdr's cleanup pass over hooks.json is NOT copied, because Sidecar has never written an " +
			"entry there and an integration that edits a file it does not own is the thing the ownership rule exists " +
			"to prevent. NOT traced: no capture of a live Droid session backs any of it, which is why the capability " +
			"entry is docs-only at screen-fallback.",
	},
	{
		Provider:    QoderCLIProvider,
		UpstreamID:  "qodercli",
		UpstreamDir: "qodercli",
		Version:     "3",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported from Herdr's qodercli integration at that commit, where the vendored " +
			"upstream/qodercli/herdr-agent-state.sh carries HERDR_INTEGRATION_VERSION=3. The provider half is one " +
			"row: QODERCLI_HOOK_EVENTS in src/integration/mod.rs is [(\"SessionStart\", \"session\")] and nothing " +
			"else, because upstream REMOVED its twelve lifecycle rows at that version; the file the entry goes in " +
			"(~/.qoder/settings.json), the \"*\" matcher, which is what install_qodercli's " +
			"ensure_command_hook(.., Some(\"*\")) writes where the devin and droid installers pass None, and the " +
			"$QODER_CONFIG_DIR override are the rest of it. All of that is kept verbatim. Every fact was re-checked " +
			"against Qoder's own published hooks reference rather than taken on trust: the settings.json schema is " +
			"Claude's nested group shape, timeout is in SECONDS with a default of 600, and the SessionStart payload " +
			"carries session_id and hook_event_name. One thing that reference does NOT carry is QODER_CONFIG_DIR, so " +
			"honouring it follows Herdr rather than a published contract, and the capability entry says so. Two " +
			"deliberate differences, each with its reason in qodercli_install.go: the transport is Sidecar's, so the " +
			"dropped shell script and its python3 dependency are gone; and no --seq is sent, because Sidecar's store " +
			"assigns. NOT traced: no capture of a live Qoder session backs any of it, which is why the capability " +
			"entry is docs-only at screen-fallback.",
	},
	{
		Provider:    QwenProvider,
		UpstreamID:  "qwen",
		UpstreamDir: "qwen",
		Version:     "1",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported from Herdr's qwen integration at that commit, where the vendored " +
			"upstream/qwen/herdr-agent-session.sh carries HERDR_INTEGRATION_VERSION=1. The asset is named " +
			"herdr-agent-SESSION rather than herdr-agent-state, which is upstream saying what the integration is: " +
			"QWEN_HOOK_EVENTS in src/integration/mod.rs is [(\"SessionStart\", \"session\")], the first and only " +
			"release, with no lifecycle rows ever added and none removed. The file the entry goes in " +
			"($QWEN_HOME/settings.json, or ~/.qwen/settings.json), the \"*\" matcher that install_qwen's " +
			"ensure_command_hook(.., Some(\"*\")) writes, and the timeout of 10_000 are kept verbatim. That last " +
			"one is the load-bearing detail and it was verified rather than copied: Qwen's own hooks reference " +
			"documents timeout as MILLISECONDS for a command hook and seconds for an HTTP hook, so Herdr's 10_000 " +
			"is ten seconds and the 10 every other provider gets would have been ten milliseconds here. QWEN_HOME " +
			"was verified the same way, in qwen-code's own packages/core/src/config/storage.ts, where " +
			"getGlobalQwenDir resolves it in place of ~/.qwen. Three deliberate differences, each with its reason " +
			"in qwen_install.go: the transport is Sidecar's, so the dropped shell script and its python3 " +
			"dependency are gone; no --seq is sent, because Sidecar's store assigns; and upstream's " +
			"--session-start-source pass-through is NOT copied, because report-session has no such flag and the " +
			"payload's source field answers no question Sidecar's binding asks -- a session id is the same " +
			"conversation whether it arrived at startup, on resume, or after a compact. TRACED against a released " +
			"qwen-code 0.23.0: the SessionStart entry this port installs fired and bound the pane, so the " +
			"capability entry is real-trace at session-identity. The capture is " +
			"internal/agentlifecycle/testdata/traces/qwen/session-start.tsv.",
	},
	{
		Provider:    MastracodeProvider,
		UpstreamID:  "mastracode",
		UpstreamDir: "mastracode",
		Version:     "2",
		Commit:      herdrVendoredCommit,
		Evidence: "Ported from Herdr's mastracode integration at that commit, where the vendored " +
			"upstream/mastracode/herdr-agent-state.sh carries HERDR_INTEGRATION_VERSION=2. The provider half of " +
			"that integration is NOT in the vendored asset: the shell script is pure transport, and the knowledge " +
			"is the eleven (event, action) rows of MASTRACODE_HOOK_EVENTS in src/integration/mod.rs, which are " +
			"kept row for row and in order in mastracodeHooks. Every event name, the hooks.json shape, the " +
			"millisecond timeout unit and the stdin payload's session_id were re-checked against Mastra Code " +
			"0.38.0's own published package (@mastra/code-sdk/dist/hooks/config.js, manager.js, executor.js and " +
			"types.d.ts) rather than taken on trust, and five sanitized captures of that version back the mapping. " +
			"Four deliberate differences, each with its reason in mastracode.go: the transport is Sidecar's, so " +
			"the dropped shell script and its python3 dependency are gone and the eleven config entries invoke the " +
			"CLI directly, exactly as Sidecar's claude, codex and kimi adapters already do with the same upstream " +
			"shape; no --seq is sent, because Sidecar's store assigns; each row carries a bounded Sidecar reason " +
			"code, which Herdr's wire has no vocabulary for; and the three rows on Mastra Code's BLOCKING events " +
			"(PreToolUse, Stop, UserPromptSubmit) end in `|| true`, because that provider reads exit code 2 on " +
			"those three as a refusal of the agent's own work and `sidecar agent report` exits 2 on a usage error, " +
			"where Herdr's shell asset has the same property by construction. ONE row's EVENT moved, and it is the " +
			"only change to the mapping itself: upstream binds the session on SessionStart, and on this release " +
			"that event's payload carries the literal \"session-init\" the hook manager is constructed with rather " +
			"than a thread id. Measured, not inferred -- the first proof run installed upstream's mapping unchanged " +
			"and shells.json came back holding {\"kind\":\"id\",\"value\":\"session-init\",\"reported\":true} " +
			"while the TUI printed a real thread id -- and a reference from an official source is marked resumable, " +
			"so every Mastra Code shell would have claimed the same non-existent conversation. UserPromptSubmit " +
			"carries the placeholder too, so AgentStart is the earliest event that can name the conversation; the " +
			"binding is there, which also re-binds a pane whose thread changed, exactly as Herdr's own pi asset " +
			"re-binds per turn on its agent-start event. NOT copied: MASTRACODE_REMOVED_HOOK_EVENTS, upstream's " +
			"hand-written list of two superseded rows, because Sidecar identifies its own entries by the source " +
			"their command names rather than by an (event, command) pair and therefore strips a stale row of any " +
			"version without a list to keep current.",
	},
}

// PortedFromRecords returns the provenance of every Sidecar integration asset.
//
// The slice is copied, because it is data the sync tool ranges over and a
// caller that sorted it in place would reorder the package's own table.
func PortedFromRecords() []PortedFrom {
	return append([]PortedFrom(nil), portedFrom...)
}

// PortedFromProvider returns one provider's record.
func PortedFromProvider(provider string) (PortedFrom, bool) {
	for _, record := range portedFrom {
		if record.Provider == provider {
			return record, true
		}
	}
	return PortedFrom{}, false
}
