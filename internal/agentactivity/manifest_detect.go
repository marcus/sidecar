package agentactivity

import (
	"github.com/marcus/sidecar/internal/agentactivity/manifest"
	"github.com/marcus/sidecar/internal/agentactivity/manifests"
)

// The screen lane: every observation Sidecar classifies is classified here, by
// Herdr's vendored manifests through the ported engine.
//
// There used to be a second lane — a Go rule table per provider — and a shadow
// mode that ran both and logged the differences. Phase 2 of
// docs/plans/active/herdr-detection-parity.md moved the providers over one at a
// time and deleted both. What is still Sidecar's, and lives in this file, is
// the process gate that refuses before any rule is evaluated, the two evidence
// strings no manifest can produce, and the fallback policy.

// ManifestAgentID maps a Sidecar provider family to the Herdr agent id whose
// manifest classifies it.
//
// One mapping is not the identity: Sidecar's family is `copilot` and Herdr's
// manifest is `github-copilot.toml`. Everything else Sidecar claims shares
// Herdr's spelling, including `antigravity` (whose *manifest id* is "agy" while
// its file is antigravity.toml — the file name is what the loader keys on).
// Keeping the whole mapping in one function is what stops a second, subtly
// different copy appearing next to a new call site.
func ManifestAgentID(agent string) string {
	switch agent {
	case "copilot":
		return "github-copilot"
	default:
		return agent
	}
}

// HerdrAgentLabel maps a Sidecar provider family to Herdr's *label* for it —
// the string `agent_label` returns (src/detect/mod.rs:121 at e2b85c7), which is
// what `herdr agent explain --agent` takes, what keys its alias table, and what
// names Herdr's own local override file.
//
// It does not name Sidecar's: a local override under
// ~/.config/sidecar/agent-detection is named after the vendored manifest file it
// replaces (manifests.OverridePath), so `agy` is Herdr's override name and
// `antigravity.toml` is Sidecar's. This label is for talking to Herdr.
//
// It is a second, different mapping from ManifestAgentID, and the difference is
// not tidy: Herdr's Antigravity manifest lives in antigravity.toml but its label
// is "agy", and its Copilot manifest lives in github-copilot.toml but its label
// is "copilot". Conflating the two silently reads the wrong file for one of the
// two providers, so both mappings are spelled out rather than derived.
func HerdrAgentLabel(agent string) string {
	switch agent {
	case "antigravity":
		return "agy"
	default:
		return agent
	}
}

// processGate applies the process refusal without evaluating any rules. It is
// Sidecar's, not Herdr's, and it is stricter: Herdr will happily evaluate
// claude.toml against whatever is on a pane it believes is Claude, while
// Sidecar refuses unless the pane's own process evidence names this provider.
// That refusal stays through the cutover, so it lives here rather than inside
// the engine.
//
// There is one rule above the per-provider ones, and it is that a *resolved*
// process identity settles the question in both directions. Before it there
// were ten inconsistent answers to "which identity input counts", and both
// directions of that were wrong.
//
// The missing direction cost a whole provider. Pi installs as a plain
// `#!/usr/bin/env node` shim, so tmux reports the pane's foreground command as
// `node`, and piProcess — an exact match on "pi" — refused before any rule ran.
// `sidecar agent list` reported `evidence=pi.process-mismatch` and `sidecar
// agent start --kind pi` ran to its full timeout, because agentcontrol's
// detector consults Detect and nothing else, so it never saw a state to be
// ready on. The screen lane answered `idle` for the very same capture the
// moment the gate let it through, which is how we know the gate was the whole
// of that defect. See docs/plans/active/herdr-parity-close-the-gap.md, Slice 1's
// result.
//
// The open direction cost worse than a missing badge. Four of the launchable
// gates accept a bare `node` or `bun` with no other evidence, which is the only
// reason a Node-installed agent is reachable at all where nothing can resolve
// argv. On that same Pi pane it also means claudeProcess("node") is true, so
// claude.toml would be evaluated against a Pi screen. Letting a resolved
// identity refuse every provider it does not name is what closes that, and it
// is why the widening had to land here rather than in the resolver alone: the
// plan's rule is that identity and the gate widen together or not at all.
//
// When nothing is resolved — a platform with no process-identity adapter, or a
// pane whose foreground group is its own shell — the per-provider command rules
// below stand exactly as they were, runtime allowances included, because there
// `pane_current_command` is the only evidence there is.
func processGate(agent, command string, ob Observation) bool {
	// Read through identifyAgentName rather than comparing the raw string: that
	// is what Identify reads ob.ProcessIdentity through, and reading one input
	// two ways is how a gate and the identity it is checking drift apart. It
	// also means an unresolvable value — a bare runtime, a path, anything the
	// alias table cannot name — is *not* an identity and falls through, rather
	// than refusing every provider on a pane nothing was learned about.
	if identity := identifyAgentName(ob.ProcessIdentity); identity != "" {
		return identity == agent
	}
	return commandGate(agent, command)
}

// commandGate is the no-identity fallback: what a provider will accept from
// `pane_current_command` alone. Each provider owns its own answer in its own
// file, because the runtime allowances are per-provider knowledge — Claude and
// Codex ship npm launchers, Pi deliberately allows none (see piProcess).
func commandGate(agent, command string) bool {
	switch agent {
	case "claude":
		return claudeProcess(command)
	case "codex":
		return codexProcess(command)
	case "grok":
		return grokProcess(command)
	case "antigravity":
		return antigravityProcess(command)
	case "pi":
		return piProcess(command)
	case "copilot":
		return copilotProcess(command)
	case "cursor":
		// Cursor used to be the one provider whose gate read a second input
		// here, `ob.ProcessIdentity == "cursor"`, because `agent` and `node` are
		// shared names. processGate answers that for every provider now, so the
		// clause is gone rather than kept as a special case that can only ever
		// be false on this branch.
		return cursorProcess(command)
	case "opencode":
		return openCodeProcess(command)
	case "amp":
		return ampProcess(command)
	case "muse":
		return museProcess(command)
	default:
		// An alias-gated family has no hand-written gate because it has no
		// hand-written anything: the whole of its Sidecar detection code is one
		// alias case in identifyProcessName. The refusal it owes the engine is
		// the same one every provider above owes — evaluate qwen.toml only against a pane
		// actually running Qwen — and identifyProcessName already answers that
		// from Herdr's own alias table, so the gate is that answer.
		//
		// It used to read ob.ProcessIdentity here as well, for the same reason
		// Cursor's branch did, and the case that forced it is worth keeping:
		// a pane whose pane_current_command is `node` and whose argv[0] basename
		// is `qwen` is identified as Qwen by Identify and then arrives here with
		// a command name the alias table cannot name. Gating on the command
		// alone refused it, which is worse than not claiming the pane at all —
		// Supports is true, so the row got a provider chip whose state could
		// only ever be unknown, with no idle fallback behind it. processGate
		// answers that for every provider now, so this branch reads the command
		// and nothing else. See TestDetectionOnlyGateAcceptsEitherIdentityInput.
		//
		// With no identity resolved it stays stricter than the launchable
		// providers' gates, and that is deliberate: a pane running one of these
		// under a runtime that leaves the command unnamed is refused rather than
		// evaluated. Refusing costs a missing badge; a bare runtime allowance
		// for ten agents nobody here has captured would let one agent's manifest
		// read another's screen on exactly the platforms where nothing can
		// contradict it.
		//
		// The residual is the Node-installed CLI whose bin is a
		// `#!/usr/bin/env node` shim on a platform with no process-identity
		// adapter: tmux reports `node`, nothing resolves, and the pane is never
		// claimed. Where an adapter does resolve argv, processGate's identity
		// rule is what now reaches it.
		if aliasGatedFamily(agent) {
			return identifyProcessName(command) == agent
		}
		return false
	}
}

// fallbackIsLowEvidence reports whether a provider's no-match idle should be
// marked FallbackIdle.
//
// It is true everywhere except Antigravity, and the exception is deliberate,
// pre-existing, and was re-examined twice in Phase 2 — at the cutover and again
// at its review, after the overlay gained a blocker for the permission prompt.
//
// Upstream `antigravity.toml` has three rules and none of them is an idle rule,
// and neither Sidecar capture of an idle Antigravity pane matches anything at
// all (testdata/antigravity/idle_fallback.txt, interrupted.txt). So the fallback
// is that provider's only route to reporting a completed turn, and marking it
// low-evidence would make "done" unreachable for it; see
// TestRealAntigravityCompletedFallbackStillCreatesUnseenDone.
//
// The review's question was whether the two overlay blockers and the status
// footer had bought enough coverage to flip it. They have not, and cannot: they
// are working and blocked rules, and what would justify flipping this is an
// *idle* rule — a screen this provider paints when a turn is finished that
// something can match. Antigravity's finished screen is its composer plus the
// "? for shortcuts" footer, which is also its startup screen, so there is no
// rule to write that distinguishes a completed turn from a session that never
// started one. Until upstream gains an idle rule, this stays the one provider
// whose fallback can announce a completion.
//
// The cost is stated rather than hidden: an Antigravity screen nobody has
// captured reads as a finished turn rather than as an unknown one. That is why
// the two speculative retain rules its old rule table carried were deleted
// rather than carried forward — see antigravity.go — and why the permission
// prompt got an overlay rule rather than being left to the fallback: on this
// provider, "no rule matched" is not a quiet answer. Remove the exception the
// day upstream gains an idle rule, or a captured idle screen justifies an
// overlay one.
func fallbackIsLowEvidence(agent string) bool { return agent != "antigravity" }

// manifestInput is the observation as the engine takes it, and the two refusals
// that happen before any rule is evaluated.
//
// The bool reports whether there is anything to evaluate. When it is false the
// Result is final and no manifest was loaded, which is why the explain-producing
// path returns a nil record in that case: there is nothing to explain about
// rules that never ran.
func manifestInput(ob Observation) (*manifest.Compiled, manifest.Input, Result, bool) {
	agent := ob.Agent
	if !Supports(agent) {
		return nil, manifest.Input{}, Result{State: StateUnknown, Evidence: "unsupported-agent"}, false
	}
	if !processGate(agent, ob.CurrentCommand, ob) {
		return nil, manifest.Input{}, Result{State: StateUnknown, Evidence: agent + ".process-mismatch"}, false
	}
	compiled, _, err := manifests.Load(ManifestAgentID(agent))
	if err != nil {
		return nil, manifest.Input{}, Result{State: StateUnknown, Evidence: agent + ".manifest-unavailable"}, false
	}
	return compiled, manifest.Input{
		// The agent asked about, not the id of the manifest that answered.
		// Those differ under a local override, which may legitimately declare
		// one agent's id and carry another's alias, and Herdr reports the
		// requested one (agent_label, manifest.rs:501).
		Agent:  agent,
		Screen: ob.Screen,
		Title:  ob.PaneTitle,
		// Progress is always empty under tmux: tmux consumes OSC 9;4 and
		// exposes no payload. The osc_progress rules still evaluate, to a
		// recorded no-match, which is how explain reports the gap honestly.
		Progress: "",
		Rows:     ob.PaneHeight,
	}, Result{}, true
}

// manifestVerdict translates one engine verdict into Sidecar's Result. The
// evidence strings and the fallback shape are Sidecar's, so a caller can compare
// this Result with a Go rule table's field by field; everything the verdict
// itself carries is upstream's.
func manifestVerdict(agent string, verdict manifest.Verdict) Result {
	if verdict.MatchedRule == nil {
		return Result{
			State:        StateIdle,
			Evidence:     agent + ".known-live-fallback",
			FallbackIdle: fallbackIsLowEvidence(agent),
		}
	}
	return Result{
		State:           State(verdict.State),
		Evidence:        verdict.MatchedRule.ID,
		VisibleIdle:     verdict.VisibleIdle,
		VisibleWorking:  verdict.VisibleWorking,
		VisibleBlocker:  verdict.VisibleBlocker,
		SkipStateUpdate: verdict.SkipStateUpdate,
	}
}

// DetectManifestResult classifies an observation through the vendored Herdr
// manifest for its agent and returns the verdict alone.
//
// This is the live path, and it is separate from DetectManifest for one reason:
// building an explain record costs an EvaluatedRule struct and a 240-character
// region preview per rule — around eighteen of each for Codex — and the poller
// runs every 200ms per visible pane and throws all of it away. Evaluate does the
// same work and allocates none of it.
func DetectManifestResult(ob Observation) Result {
	compiled, input, refused, ok := manifestInput(ob)
	if !ok {
		return refused
	}
	return manifestVerdict(ob.Agent, compiled.Evaluate(input))
}

// DetectManifest is DetectManifestResult with the explain record beside the
// verdict, for a caller that wants to show its reasoning: `sidecar agent
// explain`, the census, the differential harness.
//
// The Explain is nil when the process gate refused or the manifest could not be
// loaded: there is nothing to explain about rules that were never evaluated.
func DetectManifest(ob Observation) (Result, *manifest.Explain) {
	compiled, input, refused, ok := manifestInput(ob)
	if !ok {
		return refused, nil
	}
	verdict, explain := compiled.Explain(input)
	return manifestVerdict(ob.Agent, verdict), explain
}

// ExplainManifest is DetectManifest for a caller that only wants the record,
// such as `sidecar agent explain --file`.
func ExplainManifest(ob Observation) *manifest.Explain {
	_, explain := DetectManifest(ob)
	return explain
}

// ExplainVendoredManifest evaluates a screen against the vendored manifest for a
// Herdr agent id, with no provider gate and no Result.
//
// It exists for the agents Sidecar vendors a manifest for and does not claim as
// providers. Since Phase 4 registered the ten detection-only families that is
// `gemini` alone (Decision 4 of the plan), but the reason it was written has not
// gone away, and it is why the entry point stays: when it was added, `kiro` and
// `qodercli` were outside Supports and carried two of the four `\p{Alphabetic}`
// overlay rewrites between them, so those rewrites had no fixture, no census row
// and no differential-harness row, and a dead rule in either file would have
// gone on being dead until someone read the TOML. Both are providers now and
// reach the live path, but the next agent synced in and not yet registered will
// be in exactly the position they were, and `sidecar agent explain --file` is
// what covers it. It is a debugging surface for a manifest rather than a verdict
// for a pane, so refusing an id the binary carries the bytes for was an
// arbitrary limit.
//
// It is deliberately not a second Detect: there is no Sidecar provider here, so
// there is no process gate to apply, no evidence string to mint and no fallback
// policy to choose. A caller that wants a verdict wants Detect and a supported
// agent. The bool reports whether a manifest for the id exists at all.
func ExplainVendoredManifest(agentID string, ob Observation) (*manifest.Explain, bool) {
	compiled, _, err := manifests.Load(agentID)
	if err != nil {
		return nil, false
	}
	_, explain := compiled.Explain(manifest.Input{
		Agent:  agentID,
		Screen: ob.Screen,
		Title:  ob.PaneTitle,
		Rows:   ob.PaneHeight,
	})
	return explain, true
}

// HasVendoredManifest reports whether a Herdr agent id has a vendored manifest
// that compiles, whether or not Sidecar claims it as a provider.
func HasVendoredManifest(agentID string) bool {
	_, _, err := manifests.Load(agentID)
	return err == nil
}
