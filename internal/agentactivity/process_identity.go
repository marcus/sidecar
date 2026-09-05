package agentactivity

import (
	"bytes"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const foregroundIdentityTTL = 2 * time.Second

// foregroundIdentityEntry is one pane's cached answer. Its two halves are
// stamped independently, and that is the point rather than bookkeeping: the
// evidence half is filled by a process-table walk, the agent half may be filled
// with no walk at all, and either caller may arrive first.
//
// The scan's raw member list is kept alongside its answer so the hint pass can
// run against it later without walking again. Before that, an evidence-only
// resolve and a hint-aware resolve of the same pane overwrote each other's
// entry and each forced the other to re-walk inside the same TTL window —
// roughly doubling the process-table walks for any pane that both
// agentcontrol's observer and the workspace poll were looking at.
type foregroundIdentityEntry struct {
	group int

	// The evidence half: what the process table said, and the members it said
	// it about. Valid while scannedAt is fresh.
	identity  string
	processes []foregroundProcess
	scannedAt time.Time

	// The hint-aware half: evidence when evidence names an agent, otherwise a
	// hint. Valid while agentAt is fresh. It can be filled without scannedAt
	// ever being set — that is the cheap path, and it is the only path a pane
	// running an unrecognised wrapper gets.
	agent   string
	agentAt time.Time
}

// foregroundIdentityFresh is the TTL check, applied per half rather than per
// entry so a cheap hint read never extends the life of a stale scan.
func foregroundIdentityFresh(at, now time.Time) bool {
	return !at.IsZero() && now.Sub(at) < foregroundIdentityTTL
}

// foregroundIdentityEffort is how much work a pane's tmux command justifies
// spending to name the program behind it. The ladder lives here, in the
// resolver, rather than in each caller: "what can this pane afford" is one
// question with one answer, and it was previously duplicated as a
// NeedsProcessIdentity gate at every call site — which is what kept a hint
// permanently out of reach of the panes it exists for. See ResolveForegroundAgent.
type foregroundIdentityEffort int

const (
	// identityEffortNone reads nothing at all. pane_current_command already
	// names the program (a `claude` pane is Claude), or it names an interactive
	// shell, which is the overwhelmingly common pane and must stay free.
	identityEffortNone foregroundIdentityEffort = iota

	// identityEffortLeaderHint looks up the pane's foreground process group and
	// reads that group leader's environment: two per-process reads, and no
	// process-table walk. This is what an unrecognised command gets — `docker`,
	// `bwrap`, `sandbox-exec`, `firejail`, `nix`, `ssh` — which are the wrapper
	// shapes AgentHintEnv exists for and the ones that never reached it while
	// each caller gated on NeedsProcessIdentity.
	identityEffortLeaderHint

	// identityEffortEvidenceScan walks the foreground job and answers from
	// process evidence only. ResolveForegroundProcess, and nothing else.
	identityEffortEvidenceScan

	// identityEffortAgentScan walks the foreground job and falls back to a hint
	// when the walk names nothing. Reserved for the runtimes that genuinely
	// hide an agent (NeedsProcessIdentity), because the walk is a full
	// kern.proc.all on macOS and a /proc ReadDir on linux.
	identityEffortAgentScan
)

// agentIdentityEffort maps tmux's pane_current_command onto the ladder.
//
// The two ends are the cost argument. A pane whose command already names an
// agent or a shell learns nothing from either read, so it pays nothing; a pane
// running node/bun/agent/python is the one shape where the program's own name
// is provably absent from every cheap input, so it pays for the walk. Everything
// in between — anything the alias table cannot place — is a possible sandbox
// wrapper, and it gets the leader's hint and no walk.
func agentIdentityEffort(command string) foregroundIdentityEffort {
	name := normalizeProcessName(command)
	if name == "" {
		return identityEffortNone
	}
	if NeedsProcessIdentity(name) {
		return identityEffortAgentScan
	}
	if identifyProcessName(name) != "" {
		// A known agent, or a shell. Both are already answered.
		return identityEffortNone
	}
	return identityEffortLeaderHint
}

// foregroundProcess is the platform-neutral slice of process-table state the
// identity resolver works from.
//
// It started as pid/ppid/argv[0] — just enough to decide which members still
// belong to a foreground job. Process-tree scoring needs more, and each field
// below is here because one rule cannot be evaluated without it:
//
//   - PID and ParentPID answer "is this member still the shell's job". A daemon
//     may double-fork, be adopted by init, and retain its old process group;
//     Git's fsmonitor daemon does exactly that on macOS. See foregroundJobMembers.
//   - PID also identifies the process group leader, which scoring prefers over
//     every other member.
//   - Comm is the kernel's short process name. processPriority compares the
//     resolved name against it: a name that differs from comm was genuinely
//     unwrapped and scores higher than one that merely repeats it.
//   - Argv0 is argv[0] as executed, which is not the executable path — a
//     launcher running node with `exec -a claude` reports claude here.
//   - Argv is the whole command line, which is the only place the agent's name
//     appears when it is installed as a `#!/usr/bin/env node` shim.
//
// The environment is deliberately absent. It is read through a separate seam,
// lazily, and only on the path that is allowed to consider a hint — see
// platformProcessAgentHint and ResolveForegroundAgent.
type foregroundProcess struct {
	PID       int
	ParentPID int
	Comm      string
	Argv0     string
	Argv      []string
}

// foregroundJobMembers filters a raw process-group scan down to the members that
// still belong to the pane shell's job, leader first.
//
// The init-adopted filter is load-bearing and predates process-tree scoring: a
// double-forked daemon that keeps the pane's process group is not work the shell
// is waiting on, and counting it made ForegroundShellReady refuse to launch into
// a perfectly idle pane whenever Git's fsmonitor was running. The filter is
// applied here, once, to the richer list, so every consumer — the shell gate,
// evidence resolution and hint resolution alike — sees the same job.
//
// The group leader is exempt because it is authoritative even when its parent
// has exited; putting it first is what lets the shell gate's "exactly one
// member" check and the scoring ladder read the same slice.
func foregroundJobMembers(group int, processes []foregroundProcess) []foregroundProcess {
	var leader []foregroundProcess
	var members []foregroundProcess
	for _, process := range processes {
		if strings.TrimSpace(process.Argv0) == "" {
			continue
		}
		if process.PID != group && process.ParentPID == 1 {
			continue
		}
		if process.PID == group {
			leader = append(leader, process)
		} else {
			members = append(members, process)
		}
	}
	return append(leader, members...)
}

var foregroundIdentities = struct {
	sync.Mutex
	entries map[int]foregroundIdentityEntry
}{entries: make(map[int]foregroundIdentityEntry)}

// AgentHintEnv is the launch-time process-identity hint: a provider name a
// wrapper command may publish into its own environment so Sidecar can name an
// agent it cannot see.
//
// Herdr's equivalent is `HERDR_AGENT` (src/platform/mod.rs:346 at d08e4468,
// `parse_agent_env_hint`). It exists for the pane Sidecar cannot resolve from
// the process table at all: a sandbox or container wrapper whose foreground
// process is the sandbox, with the agent running out of reach inside it.
//
// It is a hint and never a claim. See ResolveForegroundAgent for the seam that
// keeps it away from lifecycle authority, and why that seam must not be
// collapsed.
//
// Reading it means reading another process's environment, which both adapters
// can do and neither can do unconditionally: Linux denies another user's
// /proc/<pid>/environ, and macOS withholds the environment of a *restricted*
// binary from everyone, including the same user. The macOS half is the
// surprising one and is measured in platformProcessAgentHint
// (process_identity_darwin.go) — read it before concluding from a live proof
// that the hint does not work, because a proof whose stand-in wrapper is a
// system binary sees nothing for that reason alone.
const AgentHintEnv = "SIDECAR_AGENT"

// parseAgentEnvHint reads AgentHintEnv out of a raw NUL-separated environment
// block. Upstream: `parse_agent_env_hint`, src/platform/mod.rs:346 at d08e4468.
//
// The first matching record wins, including when its value is not a known
// agent — upstream returns on the first match rather than continuing to search,
// and a second SIDECAR_AGENT further down the block is not more trustworthy than
// the first. An unknown value is no hint, not an error: the value is validated
// through identifyProcessName, the one alias table, so a hint can only ever name
// a family Sidecar already knows.
func parseAgentEnvHint(environ []byte) string {
	prefix := []byte(AgentHintEnv + "=")
	for _, record := range bytes.Split(environ, []byte{0}) {
		value, ok := bytes.CutPrefix(record, prefix)
		if !ok {
			continue
		}
		return validatedAgentHint(string(value))
	}
	return ""
}

// validatedAgentHint turns a raw hint value into a family id, or "".
//
// "shell" is rejected along with everything else unknown, because identifyAgentName
// drops that bucket: `SIDECAR_AGENT=bash` names no provider.
func validatedAgentHint(value string) string {
	return identifyAgentName(strings.TrimSpace(value))
}

// readProcessAgentHint is the seam through which AgentHintEnv is read, one
// process at a time.
//
// It is a variable so the precedence ladder can be driven from a table test
// without a live process table, and so a test can assert the *negative* that
// matters most: that the evidence-only resolver never reads an environment at
// all. Production always holds the platform implementation.
var readProcessAgentHint = platformProcessAgentHint

// readForegroundProcessGroup and readForegroundProcesses are the platform seams
// for "which group owns the terminal" and "walk the process table".
//
// They are variables for the same reason readProcessAgentHint is: the ladder's
// cost claim is the part most likely to rot, and the only mechanical way to pin
// "this pane does not walk the process table" is to substitute a walk that fails
// the test when it is called. Production always holds the platform
// implementations.
var (
	readForegroundProcessGroup = platformForegroundProcessGroup
	readForegroundProcesses    = platformForegroundProcesses
)

// ResolveForegroundAgent names the agent running in a pane, for detection and
// display. It is the hint-aware resolver. currentCommand is tmux's
// pane_current_command, which decides how much the answer is allowed to cost.
//
// # Why this is a different function from ResolveForegroundProcess
//
// This one may consider AgentHintEnv. That one may not, and the split is the
// whole point rather than an accident of history.
//
// ResolveForegroundProcess feeds lifecycleenv.OccupantKind, which feeds
// VerifyReportedKind, which *refuses* a hook report whose claimed kind disagrees
// with the pane's occupant. AgentHintEnv is an environment variable: anything
// running in the session can set it. If a hint could reach OccupantKind, then
// exporting `SIDECAR_AGENT=codex` in a Claude pane would make Sidecar reject
// Claude's own reports — a writable variable would have acquired the power to
// switch off a lifecycle lane. So the hint stops here, on the display side,
// where being wrong costs a wrong badge and nothing else.
//
// Do not "simplify" these two back into one function. If a caller needs both
// answers it should ask for both.
//
// # Precedence, and a deliberate divergence from upstream
//
// Upstream's `probe_foreground_process_from_jobs` (src/pane.rs:608 at d08e4468)
// reads the process group leader's hint *before* identifying the leader, so a
// hint outranks process evidence. This port inverts that: evidence first, hint
// only when evidence names nothing.
//
// The difference is who can write the hint. Herdr's `HERDR_AGENT` is set by
// Herdr's own wrapper, so trusting it over evidence is trusting itself.
// Sidecar's `SIDECAR_AGENT` is a bare environment variable that Sidecar reads
// and never writes (see the Slice 3 result in
// docs/plans/implemented/herdr-parity-close-the-gap.md), so anything in the session
// can set it. Under upstream's order, `export SIDECAR_AGENT=codex` in a Claude
// pane would relabel that pane — the display-side echo of exactly the failure
// the resolver split exists to prevent, and the hint's only observable effect on
// a pane whose evidence already answers.
//
// Inverting it also removes a cost: upstream's order forces a hint read on every
// trivially identified leader, once per pane per poll.
//
// So the ladder is: identifyAgentInJob across the job (which already prefers the
// leader, then scores members), then the leader's hint, then any other member's
// hint. Hints keep upstream's leader-before-members order among themselves,
// because a member's hint may have been inherited from an unrelated ancestor.
func ResolveForegroundAgent(panePID int, currentCommand string) string {
	return resolveForegroundIdentity(panePID, agentIdentityEffort(currentCommand)).agent
}

// ResolveForegroundProcess identifies the known program in the pane's actual
// foreground process group, including "shell" when the group belongs to an
// interactive shell. Unlike pane_current_command, this is process ownership
// evidence and is therefore safe for agent-control's shell-ready gate and for
// lifecycleenv's occupant check.
//
// Process evidence only. It never reads an environment; see ResolveForegroundAgent.
// It also takes no command, and therefore no cost ladder: its callers ask about
// one pane deliberately (agent start, the occupant check), not about every row
// on screen, so it always scans.
func ResolveForegroundProcess(panePID int) string {
	return resolveForegroundIdentity(panePID, identityEffortEvidenceScan).identity
}

// resolveForegroundIdentity answers as much of the pane's identity as the
// caller's effort allows, filling the cache entry's two halves independently.
//
// Caching is keyed by pane PID and validated against the foreground group, so a
// new foreground job invalidates immediately while steady-state polling does
// not re-read. Each half is computed only if the caller needs it and the cache
// does not already hold it fresh, which is what lets an evidence-only resolve
// and a hint-aware resolve of the same pane cooperate instead of evicting each
// other: the hint pass re-uses the members the scan already collected.
func resolveForegroundIdentity(panePID int, effort foregroundIdentityEffort) foregroundIdentityEntry {
	if panePID <= 0 || effort == identityEffortNone {
		return foregroundIdentityEntry{}
	}
	group := readForegroundProcessGroup(panePID)
	if group <= 0 {
		return foregroundIdentityEntry{}
	}
	now := time.Now()
	entry := cachedForegroundIdentity(panePID, group, now)

	if effort != identityEffortLeaderHint && entry.scannedAt.IsZero() {
		entry.processes = readForegroundProcesses(group)
		entry.identity = foregroundEvidenceIdentity(group, entry.processes)
		entry.scannedAt = now
	}
	if effort != identityEffortEvidenceScan && entry.agentAt.IsZero() {
		// On the leader-hint path there are no members to pass — unless a scan
		// happens to be cached, in which case they are free — so this reads one
		// environment: the group leader's. The process group id is the leader's
		// pid, which is why the cheap path needs no scan to find it.
		entry.agent = foregroundAgentIdentity(group, entry.identity, entry.processes)
		entry.agentAt = now
	}

	entry.group = group
	storeForegroundIdentity(panePID, entry, now)
	return entry
}

// cachedForegroundIdentity returns the pane's entry with any expired half
// cleared, or a zero entry when the foreground group has changed.
func cachedForegroundIdentity(panePID, group int, now time.Time) foregroundIdentityEntry {
	foregroundIdentities.Lock()
	entry, ok := foregroundIdentities.entries[panePID]
	foregroundIdentities.Unlock()
	if !ok || entry.group != group {
		return foregroundIdentityEntry{}
	}
	if !foregroundIdentityFresh(entry.scannedAt, now) {
		entry.identity, entry.processes, entry.scannedAt = "", nil, time.Time{}
	}
	if !foregroundIdentityFresh(entry.agentAt, now) {
		entry.agent, entry.agentAt = "", time.Time{}
	}
	return entry
}

func storeForegroundIdentity(panePID int, entry foregroundIdentityEntry, now time.Time) {
	foregroundIdentities.Lock()
	defer foregroundIdentities.Unlock()
	if len(foregroundIdentities.entries) > 256 {
		for pid, cached := range foregroundIdentities.entries {
			if !foregroundIdentityFresh(cached.scannedAt, now) && !foregroundIdentityFresh(cached.agentAt, now) {
				delete(foregroundIdentities.entries, pid)
			}
		}
	}
	foregroundIdentities.entries[panePID] = entry
}

// foregroundEvidenceIdentity is the process-only answer: scored identification
// across the job, falling back to "shell" when the job names no agent but does
// contain an interactive shell.
//
// The shell fallback is not part of upstream's scoring — Herdr has no "shell"
// answer at all — and it is kept separate from it here for the same reason
// isGenericRuntimeOrShell is kept separate from identifyProcessName's shell
// bucket: "an interactive shell is sitting in this pane" is a launch-readiness
// fact, not an identity.
func foregroundEvidenceIdentity(group int, processes []foregroundProcess) string {
	if agent, _ := identifyAgentInJob(group, processes); agent != "" {
		return agent
	}
	for _, process := range processes {
		if identifyArgv0(process.Argv0) == "shell" {
			return "shell"
		}
	}
	return ""
}

// foregroundAgentIdentity applies the precedence documented on
// ResolveForegroundAgent: process evidence, then the group leader's hint, then
// any other member's hint.
//
// identity is the evidence answer the caller already has, and processes are the
// job members it was computed from — both empty on the cheap path, where no
// scan was run. That is why the leader's hint is read by group id rather than by
// looking the leader up in processes: the process group id *is* the leader's
// pid, so the one caller with no member list still asks the right process.
//
// "shell" is not an agent name here. It says an interactive shell owns the job,
// which is precisely the case where a hint may still name what is running under
// it, so it does not block the hint the way a real identification does.
//
// No environment is read at all when evidence already names an agent, which is
// the common case on the scanning path.
func foregroundAgentIdentity(group int, identity string, processes []foregroundProcess) string {
	if identity != "" && identity != "shell" {
		return identity
	}
	if hint := readProcessAgentHint(group); hint != "" {
		return hint
	}
	for _, process := range processes {
		if process.PID == group {
			continue
		}
		if hint := readProcessAgentHint(process.PID); hint != "" {
			return hint
		}
	}
	return ""
}

// ForegroundShellReady is the strict launch gate for a managed tmux pane. It
// accepts only the pane shell's own process group with exactly one member, and
// that member must resolve to a known interactive shell. Unknown helpers are
// busy, not ignorable.
//
// It reads argv[0] and nothing else on purpose. Process-tree scoring exists to
// look *past* a runtime to the program behind it, which is the opposite of what
// this question wants: a pane running `node` is occupied whether or not the node
// turns out to be an agent, and a hint is irrelevant because nobody launches
// into a hint.
func ForegroundShellReady(panePID int, currentCommand string) bool {
	if panePID <= 0 || readForegroundProcessGroup(panePID) != panePID {
		return false
	}
	processes := readForegroundProcesses(panePID)
	if len(processes) != 1 || identifyArgv0(processes[0].Argv0) != "shell" {
		return false
	}
	return identifyProcessName(strings.ToLower(strings.TrimSpace(currentCommand))) == "shell"
}

func identifyArgv0(argv0 string) string {
	argv0 = strings.TrimSpace(argv0)
	if argv0 == "" {
		return ""
	}
	resolved := argv0
	if target, err := filepath.EvalSymlinks(argv0); err == nil {
		resolved = target
	}
	name := strings.TrimPrefix(filepath.Base(resolved), "-")
	return identifyProcessName(name)
}

// HasProcessIdentity reports whether this platform can disambiguate a pane's
// foreground job by argv[0].
//
// It exists so the answer is read from the build rather than restated as a
// GOOS list somewhere else — the host protocol's hello carries this bit, and a
// second copy of "which platforms are implemented" would be wrong the moment a
// third one is added.
func HasProcessIdentity() bool { return processIdentitySupported }
