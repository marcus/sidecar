// Package agentactivity classifies live agent terminal observations without
// depending on tmux, Bubble Tea, or persistent conversation storage.
//
// # Where the rules live
//
// Not here. Every provider is classified by Herdr's vendored detection manifest
// for it, executed by internal/agentactivity/manifest; anything Sidecar knows
// that upstream does not is a data overlay under manifests/sidecar/. This
// package owns what surrounds that: a stricter-than-Herdr process gate per
// provider, the identity resolver, the two evidence strings no manifest can
// produce, the low-evidence fallback policy, and Tracker, which turns a stream
// of verdicts into transitions. See docs/reference/herdr-detection-parity.md.
//
// # Spinner glyph sets are provider-owned, and are upstream's problem now
//
// The rule tables that used to live here shared this failure mode, and it is
// worth remembering because it is what the cutover bought. A spinner set that
// is too narrow does not merely miss activity, it inverts the verdict: Claude
// shared one eleven-glyph braille pattern with Codex, so the frames outside
// that set left the title rule unmatched, evaluation fell through to the
// prompt-box idle rule while subagents were still running, and Tracker turned
// that working→idle transition into a completed turn. Sessions reported "done"
// with work in flight. Claude Code 2.1.228 then switched to half-circle frames
// (U+25D0–U+25D3), which Sidecar's hand-written pattern never learned and
// upstream's `osc_title_working` already covered.
//
// If a spinner is missed today, the fix is a fixture and either an overlay rule
// or a pull request to Herdr — never a regex added back into this package.
package agentactivity

import (
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/agentactivity/manifest"
)

type State string

const (
	StateUnknown State = "unknown"
	StateIdle    State = "idle"
	StateWorking State = "working"
	StateBlocked State = "blocked"
)

type Observation struct {
	Agent          string
	Screen         string
	PaneTitle      string
	CurrentCommand string
	// ProcessIdentity is a provider name resolved from the pane's foreground
	// process group and argv[0]. It disambiguates shared runtimes such as Node
	// without promoting phrases from another agent's transcript.
	ProcessIdentity string
	// PaneHeight is the pane's own row count (tmux #{pane_height}). It is the
	// manifest engine's read window: Herdr reads the tail of the buffer N rows
	// deep where N is the pane's height, so a capture carrying scrollback has to
	// be bounded to the same N or a resolved historical prompt wins a rule it
	// should never have seen. Zero means the height was not available and the
	// engine falls back to 24, Herdr's own DEFAULT_DETECTION_ROWS. See
	// docs/reference/herdr-detection-parity.md ("Read window").
	PaneHeight int
	CapturedAt time.Time
}

type Result struct {
	State           State
	Evidence        string
	VisibleIdle     bool
	VisibleWorking  bool
	VisibleBlocker  bool
	SkipStateUpdate bool
	// FallbackIdle distinguishes provider-owned explicit idle evidence from the
	// conservative no-match policy for a positively identified live process.
	// Fallback idle may establish/display idle, but cannot announce completion.
	FallbackIdle bool
}

var (
	semanticVersionCommand = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	claudeScreenIdentity   = regexp.MustCompile(`(?ms)(^─{8,}\s*$\n^❯.*$\n^─{8,}\s*$|manual mode on · \? for shortcuts)`)
	// Codex-as-node is the common npm launcher. Identity has to survive the
	// startup header scrolling off: working/approval chrome is enough.
	// The composer glyph alone is not — other TUIs use ›.
	codexScreenIdentity = regexp.MustCompile(`(?is)(OpenAI Codex \(v[^)]+\)|• Working \(.*esc to interrupt\)|No, and tell Codex what to do differently|Would you like to run the following command\?|/ T R A N S C R I P T /|Allow command\?)`)
)

// Identify returns the live program owning an agent pane when the existing
// tmux metadata or current UI makes that identity unambiguous. It deliberately
// returns an empty string for shared runtimes such as node and bun unless the
// current screen distinguishes the provider. Callers can retain their prior
// identity in that case without paying for a process-tree scan.
func Identify(ob Observation) string {
	if identity := identifyAgentName(ob.ProcessIdentity); identity != "" {
		return identity
	}
	command := strings.ToLower(strings.TrimSpace(ob.CurrentCommand))
	if identity := identifyProcessName(command); identity != "" {
		return identity
	}

	// Shared process names need live UI chrome or a resolved argv0. Herdr
	// never claims Cursor from screen text: `node` is unknown unless argv
	// names a known agent, and `agent` is Cursor only when it resolves to
	// cursor-agent. We have pane_current_command plus ProcessIdentity, so Codex /
	// Claude / Grok may still be claimed from distinctive chrome, but Cursor is
	// process-or-alias only (plus the Cursor Agent header as a last resort when
	// the comm name is the unresolvable `agent` alias).
	// Empty Identify lets callers retain a prior *positive* live identity — not
	// a launch preference.
	if command == "agent" || command == "node" || command == "bun" {
		current := identityWindow(ob)
		if command != "agent" {
			if claudeScreenIdentity.MatchString(current) {
				return "claude"
			}
			if codexScreenIdentity.MatchString(current) {
				return "codex"
			}
		}
		if grokScreenIdentity.MatchString(current) {
			return "grok"
		}
		if command == "agent" {
			if lookUpAgentAlias() == "cursor" {
				return "cursor"
			}
			if cursorScreenIdentity.MatchString(current) {
				return "cursor"
			}
		}
	}
	return ""
}

// claudeVersionArgv0 reports the one non-name argv[0] this resolver treats as a
// provider identity: Claude Code renames its own process to its version string,
// so a pane running it reports "2.0.14" rather than "claude".
//
// It is spelled out rather than inlined because what a false positive costs
// changed. This resolver was written to label a pane in the UI, where guessing
// wrong meant a wrong label; it is now also the evidence
// lifecycleenv.VerifyReportedKind checks a hook's --kind claim against, where
// guessing wrong means refusing a legitimate report. The direction is the safe
// one — a wrongly-named pane refuses a report rather than binding one provider's
// conversation to another — but it is a refusal, so the pattern stays exactly as
// narrow as Claude's own format: three dot-separated integers, nothing else. No
// "v" prefix, no prerelease suffix, no fourth component, and nothing that merely
// contains a version. TestOnlyClaudesOwnVersionArgv0ResolvesToAProvider and
// TestTheProcessNameVocabularyMatchesTheAgentCatalog pin both halves of that:
// the shape, and that no other catalog family could ever present this argv[0].
// identityWindow is the bounded screen Identify reads when a pane's command
// name is a shared runtime. It is deliberately not the detection window, and
// the difference is the order of two steps rather than the depth: identity is a
// cheaper, more forgiving question than a verdict — a startup header twenty
// rows up still names the program that painted it — so the padding a tall pane
// carries below its content must not be allowed to spend the budget.
//
// Trailing blank rows are therefore dropped *before* the last 24 rows are
// taken, which is what the pre-manifest RegionCurrent did and the opposite of
// manifest.ReadWindow. ReadWindow is right for detection and was wrong here:
// tmux pads a capture out to the full pane height, so on a real 40-row pane
// showing a five-line Codex startup banner the last 24 rows are 24 rows of
// padding and Identify saw an empty window. A fresh start or a `/clear` on any
// pane taller than 24 rows silently stopped resolving `node` and `bun` to their
// provider. TestIdentityWindowSurvivesTallPanePadding is the regression.
//
// The SGR strip is shared with the engine so the two lanes see the same bytes;
// only the trim order differs, and it differs on purpose.
func identityWindow(ob Observation) string {
	lines := strings.Split(ansi.Strip(ob.Screen), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > manifest.DefaultDetectionRows {
		lines = lines[len(lines)-manifest.DefaultDetectionRows:]
	}
	return strings.Join(lines, "\n")
}

func claudeVersionArgv0(command string) bool {
	return semanticVersionCommand.MatchString(command)
}

// launcherSuffixes are the wrapper extensions a process name may carry without
// changing which program is running. Matches Herdr's list verbatim
// (`normalized_agent_lookup_name`, src/detect/mod.rs:668 at e2b85c7), and only
// one is stripped, as upstream does.
var launcherSuffixes = []string{".exe", ".cmd", ".bat", ".ps1", ".js"}

// normalizeProcessName reduces a process name to the spelling the alias table
// is written in. It is Herdr's `path_basename` + `normalized_agent_lookup_name`
// pair (src/detect/mod.rs:668-683 at e2b85c7) rather than a Sidecar invention,
// so a vendored upstream alias list can be asserted against this resolver
// directly: lower-case, trim, take the last non-empty path component across
// either separator, then strip at most one launcher suffix.
//
// It exists because npm and Windows shims present `claude.cmd`, `codex.exe`,
// `opencode.js` and paths like `/opt/homebrew/bin/opencode`, and carrying a
// case per spelling is how the two vocabularies drift.
func normalizeProcessName(command string) string {
	command = strings.ToLower(strings.TrimSpace(command))
	name := command
	if index := strings.LastIndexAny(strings.TrimRight(name, `/\`), `/\`); index >= 0 {
		if base := strings.Trim(name[index+1:], `/\`); base != "" {
			name = base
		}
	}
	for _, suffix := range launcherSuffixes {
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix) {
			name = name[:len(name)-len(suffix)]
			break
		}
	}
	return name
}

func identifyProcessName(command string) string {
	command = strings.ToLower(strings.TrimSpace(command))
	// Claude's version-shaped argv[0] is tested before normalisation on
	// purpose: stripping a launcher suffix first would let "1.2.3.js" read as
	// Claude, which is exactly the widening the claudeVersionArgv0 comment
	// forbids. Claude renames its own process to a bare version string, never
	// to a suffixed or path-qualified one.
	if claudeVersionArgv0(command) {
		return "claude"
	}
	// Alias spellings below are Herdr's `lookup_agent` table
	// (src/detect/mod.rs:193 at e2b85c7) for the families Sidecar claims,
	// plus the Sidecar-only spellings noted per case.
	name := normalizeProcessName(command)
	switch {
	case oneOf(name, "claude", "claude-code"):
		return "claude"
	// "codex-cli" is Sidecar-only; upstream knows just "codex".
	case oneOf(name, "codex", "codex-cli"):
		return "codex"
	// Upstream lists "grok" and "grok-build"; the prefix is a Sidecar superset.
	case name == "grok" || strings.HasPrefix(name, "grok-"):
		return "grok"
	case oneOf(name, "agy", "antigravity", "antigravity-cli"):
		return "antigravity"
	case name == "pi":
		return "pi"
	case oneOf(name, "copilot", "github-copilot", "ghcs"):
		return "copilot"
	case oneOf(name, "cursor", "cursor-agent"):
		return "cursor"
	case oneOf(name, "opencode", "opencode2", "open-code"):
		return "opencode"
	case oneOf(name, "amp", "amp-local"):
		return "amp"
	// Upstream lists "muse", "muse-code", "muse-cli" and `muse-bin-<digit>`;
	// the prefix is a Sidecar superset that already covers all four.
	case name == "muse" || strings.HasPrefix(name, "muse-"):
		return "muse"
	// The twelve families whose whole Sidecar detection code is this alias case:
	// the vendored manifest supplies every rule and the engine executes it, and
	// none of them has a hand-written process gate. Ten arrived detection-only
	// in Phase 4 of docs/plans/active/herdr-detection-parity.md and became
	// launchable when the catalog moved to TOML; `omp` and `mastracode` have no
	// screen manifest upstream at all, so they are named here only so a pane
	// running one has an identity for VerifyReportedKind to check a hook report
	// against.
	//
	// The spellings are Herdr's `lookup_agent` table verbatim, including the
	// ones carrying a space, which upstream has because it lowercases display
	// names. agentcatalog is the other half of the pair and
	// TestTheProcessNameVocabularyMatchesTheAgentCatalog keeps the two in step.
	//
	// None of them needs the versioned-binary prefix rule: `muse` is the only
	// entry in the extracted versioned_binary_prefixes table.
	case name == "cline":
		return "cline"
	case oneOf(name, "devin", "devin cli", "devin-cli"):
		return "devin"
	case name == "droid":
		return "droid"
	case oneOf(name, "hermes", "hermes-agent"):
		return "hermes"
	case oneOf(name, "kilo", "kilo code", "kilo-code"):
		return "kilo"
	case oneOf(name, "kimi", "kimi code", "kimi-code"):
		return "kimi"
	case oneOf(name, "kiro", "kiro-cli"):
		return "kiro"
	case name == "maki":
		return "maki"
	// Herdr's label for Qoder is `qodercli`, which is also its manifest id and
	// its file name; `qoder` is one of the four process spellings, not the id.
	case oneOf(name, "qoder", "qodercli", "qoderclicn", "qodercn"):
		return "qodercli"
	case oneOf(name, "qwen", "qwen code", "qwen-code"):
		return "qwen"
	case name == "omp":
		return "omp"
	case oneOf(name, "mastracode", "mastra code", "mastra-code"):
		return "mastracode"
	// Deliberately narrower than Herdr's `is_generic_runtime_or_shell`, which
	// also lists tmux, node, bun, cmd, powershell and python[3[.N]]. That
	// predicate scores process-tree candidates; this one gates a launch
	// (ForegroundShellReady), so it names only interactive shells. The scoring
	// predicate now exists, separately, as isGenericRuntimeOrShell in
	// process_tree.go — the two lists are different on both sides (this one has
	// `nu`, that one has `node`, `tmux`, `cmd` and python) because they answer
	// different questions. Merging them is not a simplification.
	case oneOf(name, "sh", "bash", "zsh", "fish", "nu", "pwsh"):
		return "shell"
	default:
		return ""
	}
}

// NeedsProcessIdentity reports whether tmux's command name is shared by
// multiple agent CLIs and therefore benefits from a foreground process scan.
//
// This is a cost gate, not a correctness one. Its callers poll every workspace
// row, and answering true means paying for a process-table walk (a full
// kern.proc.all on macOS) behind a two-second cache. So it names the runtimes
// that hide an agent and nothing else.
//
// It is no longer a gate any caller of ResolveForegroundAgent applies: it is the
// top rung of that resolver's own cost ladder (agentIdentityEffort), because
// duplicating it at each call site is what kept AgentHintEnv permanently out of
// reach of the sandbox panes it exists for — a `docker` pane answers false here
// and so never reached the hint. It survives as an exported predicate for the
// other question it answers: whether a pane needs live UI chrome captured to
// name its provider (workspace's shellNeedsIdentityScreen).
//
// It is therefore a strict subset of isGenericRuntimeOrShell, and the three
// exclusions are deliberate:
//
//   - The interactive shells. `sh`, `bash`, `zsh` and `fish` are in the scoring
//     predicate because a shell-wrapped agent (`/bin/sh /usr/local/bin/pi`) is a
//     real install shape upstream tests. But an idle shell is the overwhelmingly
//     common pane, so admitting them here would make every idle row in every
//     workspace scan the process table forever to find nothing. Those panes are
//     still resolved on the paths that scan unconditionally — agentcontrol's
//     Inspect and observer, which is what `sidecar agent list` and `agent start`
//     read.
//   - `tmux`. wrappedAgentNameFromRuntimeArgv refuses it by construction, so a
//     scan could not produce an answer even in principle.
//   - `cmd`, `powershell`, `pwsh`. No Windows process-identity adapter exists,
//     so the scan would return nothing on every platform that reaches this.
//
// Python was added when process-tree scoring landed: upstream unwraps
// `python3.12 /nix/store/.../hermes --resume …`, and a python pane is rare
// enough that the scan is affordable where a shell pane is not.
func NeedsProcessIdentity(command string) bool {
	name := normalizeProcessName(command)
	if isPythonRuntime(name) {
		return true
	}
	switch name {
	case "agent", "node", "bun":
		return true
	default:
		return false
	}
}

// Detect dispatches an observation to the vendored Herdr manifest for its
// provider. Keeping dispatch here lets the workspace poll remain
// product-neutral while each provider file owns only its process gate.
//
// There is one lane. Every provider Sidecar claims was cut over in Phase 2 of
// docs/plans/active/herdr-detection-parity.md, so the Go rule tables, the
// selector that used to choose between the two lanes, and the shadow mode that
// compared them are all gone. What is left of each `<provider>.go` is the
// process gate, plus an identity pattern for cursor and grok; Claude's and
// Codex's identity patterns sit beside Identify above, because that is the only
// thing that reads them.
func Detect(ob Observation) Result { return DetectManifestResult(ob) }

// Supports reports whether Sidecar has provider-owned activity evidence rules.
//
// It answers true for twenty agents: the ten with a hand-written process gate,
// and the ten below that reach the engine through the alias table alone. Both
// groups have the same two things for detection purposes, a vendored Herdr
// manifest and a gate, which is the point. Supports is a statement about the
// screen lane, not about launch, resume, or a conversation adapter;
// agentcatalog.Families and agentcatalog.DetectionOnly are what answer those.
//
// `omp` and `mastracode` are deliberately absent. They are launchable catalog
// families and identifyProcessName names them, but upstream ships them
// hooks-only with no screen manifest, so there is nothing for this lane to
// execute and claiming otherwise would put a provider chip on a row whose state
// could only ever be unknown.
func Supports(agent string) bool {
	switch agent {
	case "codex", "claude", "grok", "antigravity", "pi", "copilot", "cursor", "opencode", "amp", "muse":
		return true
	default:
		return aliasGatedFamily(agent)
	}
}

// aliasGatedFamily names the ten families whose process gate is the alias table
// rather than a hand-written predicate.
//
// It used to be called detectionOnly, and the rename is the whole of what
// changed when those ten gained launch commands: they were never gated this way
// *because* they could not be launched, but because nobody had captured the
// runtime allowances a hand-written gate encodes. That is still true, and the
// stricter gate is still the right answer for them (see commandGate).
//
// It is spelled here rather than read from agentcatalog so this package stays
// free of that import on its hot path; TestAliasGatedSetMatchesTheCatalog is
// what stops the two lists drifting.
func aliasGatedFamily(agent string) bool {
	switch agent {
	case "cline", "devin", "droid", "hermes", "kilo", "kimi", "kiro", "maki", "qodercli", "qwen":
		return true
	default:
		return false
	}
}

// Tracker owns transition policy while classification remains state-free.
type Tracker struct {
	State          State
	Evidence       string
	ChangedAt      time.Time
	Seen           bool
	VisibleBlocker bool
	// IdleInferred records that the current idle state came from the absence
	// of activity rather than an explicit completion marker. Providers without
	// a completion signal can never assert "done", and views use this to say so
	// instead of letting their absence from the done lane read as a bug.
	IdleInferred    bool
	idleCandidateAt time.Time
	skipSince       time.Time
}

const IdleDebounce = 400 * time.Millisecond

// SkipRetentionCap bounds how long an overlay rule may retain the prior state.
// Retention exists so a transcript or model picker does not erase a live turn,
// but an overlay left open — or a rule matching chrome that never clears —
// would otherwise hold a confident badge forever. Past the cap the tracker
// admits it no longer knows rather than continuing to assert stale evidence.
const SkipRetentionCap = 2 * time.Minute

// ResetForProcessChange clears semantic state from the prior pane owner while
// allowing a confirmed new process's first explicit idle observation to land
// immediately. That first idle is initialization, not a completion event.
func (t *Tracker) ResetForProcessChange(now time.Time) {
	*t = Tracker{
		State:           StateUnknown,
		Evidence:        "live-process-changed",
		Seen:            true,
		idleCandidateAt: now.Add(-IdleDebounce),
	}
}

func (t *Tracker) Apply(result Result, now time.Time) bool {
	// Visibility belongs to the current capture, not the retained semantic
	// state. Overlay/viewer captures may deliberately retain StateBlocked, but
	// they must not retain evidence that the blocker is still on screen.
	t.VisibleBlocker = result.State == StateBlocked && result.VisibleBlocker && !result.SkipStateUpdate
	if result.SkipStateUpdate {
		if t.skipSince.IsZero() {
			t.skipSince = now
		}
		if now.Sub(t.skipSince) < SkipRetentionCap {
			return false
		}
		// Cap exceeded: stop retaining and let the overlay's own StateUnknown
		// land, so the badge reads unknown instead of a stale certainty.
	} else {
		t.skipSince = time.Time{}
	}
	if result.State == StateIdle {
		if t.State == StateIdle {
			t.idleCandidateAt = time.Time{}
			return false
		}
		if result.VisibleIdle {
			t.idleCandidateAt = time.Time{}
		} else {
			if t.idleCandidateAt.IsZero() {
				t.idleCandidateAt = now
				return false
			}
			if now.Sub(t.idleCandidateAt) < IdleDebounce {
				return false
			}
		}
	} else {
		t.idleCandidateAt = time.Time{}
	}
	if result.State == t.State && result.Evidence == t.Evidence {
		return false
	}
	previous := t.State
	t.State = result.State
	t.Evidence = result.Evidence
	t.ChangedAt = now
	switch result.State {
	case StateWorking, StateBlocked:
		t.Seen = false
	case StateIdle:
		// Initial/restart idle is quiet; only a transition from live work creates done.
		t.Seen = result.FallbackIdle || previous == StateUnknown || previous == ""
		t.IdleInferred = result.FallbackIdle
	}
	if result.State != StateIdle {
		t.IdleInferred = false
	}
	return true
}

// Snapshot is the persistable projection of a tracker. It carries only the
// fields that survive a restart meaningfully: transition policy timers are
// in-process concerns and are deliberately dropped.
type Snapshot struct {
	State        string    `json:"state"`
	Evidence     string    `json:"evidence,omitempty"`
	ChangedAt    time.Time `json:"changedAt"`
	Seen         bool      `json:"seen"`
	IdleInferred bool      `json:"idleInferred,omitempty"`
}

func (t Tracker) Snapshot() Snapshot {
	return Snapshot{State: string(t.State), Evidence: t.Evidence, ChangedAt: t.ChangedAt, Seen: t.Seen, IdleInferred: t.IdleInferred}
}

// Restore rebuilds a tracker from persisted state. A restored idle keeps its
// original ChangedAt, so a turn that finished before the restart still reads
// as recently finished rather than as "just observed".
func Restore(s Snapshot) Tracker {
	return Tracker{State: State(s.State), Evidence: s.Evidence, ChangedAt: s.ChangedAt, Seen: s.Seen, IdleInferred: s.IdleInferred}
}

func (t *Tracker) Acknowledge() { t.Seen = true }

func (t Tracker) DisplayState() string {
	if t.State == StateIdle && !t.Seen {
		return "done"
	}
	return string(t.State)
}
