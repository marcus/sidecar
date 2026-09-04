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
