package agentactivity

import (
	"strings"
	"testing"
)

func TestForegroundJobMembersIgnoresOnlyInitAdoptedMembers(t *testing.T) {
	tests := []struct {
		name      string
		processes []foregroundProcess
		want      []string
	}{
		{
			name: "detached daemon is not shell work",
			processes: []foregroundProcess{
				{PID: 100, ParentPID: 10, Argv0: "zsh"},
				{PID: 101, ParentPID: 1, Argv0: "git"},
			},
			want: []string{"zsh"},
		},
		{
			name: "live helper still refuses shell readiness",
			processes: []foregroundProcess{
				{PID: 100, ParentPID: 10, Argv0: "zsh"},
				{PID: 101, ParentPID: 100, Argv0: "helper"},
			},
			want: []string{"zsh", "helper"},
		},
		{
			name:      "adopted group leader remains authoritative",
			processes: []foregroundProcess{{PID: 100, ParentPID: 1, Argv0: "claude"}},
			want:      []string{"claude"},
		},
		{
			name: "leader is first even when the scan found it last",
			processes: []foregroundProcess{
				{PID: 101, ParentPID: 100, Argv0: "helper"},
				{PID: 100, ParentPID: 10, Argv0: "zsh"},
			},
			want: []string{"zsh", "helper"},
		},
		{
			name:      "a process with no argv[0] is not a member",
			processes: []foregroundProcess{{PID: 100, ParentPID: 10, Argv0: "  "}},
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := foregroundJobMembers(100, tt.processes)
			if len(got) != len(tt.want) {
				t.Fatalf("members = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i].Argv0 != tt.want[i] {
					t.Fatalf("members = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// resetForegroundIdentityCache empties the TTL cache. Fixture-driven tests reuse
// the same pane PID and foreground group across cases, which is exactly the key
// the cache is built on, so without this a later case reads an earlier one's
// answer and passes for the wrong reason.
func resetForegroundIdentityCache() {
	foregroundIdentities.Lock()
	foregroundIdentities.entries = make(map[int]foregroundIdentityEntry)
	foregroundIdentities.Unlock()
}

// job builds a foreground job whose members carry a comm, an argv, and argv[0]
// taken from argv — the shape both platform adapters produce. It mirrors
// upstream's `foreground_process` test helper (src/detect/mod.rs:730 at
// d08e4468).
func job(pid int, comm string, argv ...string) foregroundProcess {
	argv0 := ""
	if len(argv) > 0 {
		argv0 = argv[0]
	}
	return foregroundProcess{PID: pid, ParentPID: 2, Comm: comm, Argv0: argv0, Argv: argv}
}

// TestIdentifyAgentInJobNamesAgentsBehindGenericRuntimes is the exit-gate test
// for Slice 3's first item: the measured Pi and Qwen cases from the plan, plus
// the rest of upstream's install shapes.
func TestIdentifyAgentInJobNamesAgentsBehindGenericRuntimes(t *testing.T) {
	tests := []struct {
		name      string
		group     int
		processes []foregroundProcess
		want      string
	}{
		{
			// The measured case. Pi 0.84.3 installs a `#!/usr/bin/env node`
			// shim, so tmux reports `node` and argv[0] is the interpreter.
			name:      "node shim names pi",
			group:     123,
			processes: []foregroundProcess{job(123, "node", "node", "/Users/x/.local/bin/pi")},
			want:      "pi",
		},
		{
			name:      "node shim names qwen",
			group:     123,
			processes: []foregroundProcess{job(123, "node", "node", "/Users/x/.local/bin/qwen")},
			want:      "qwen",
		},
		{
			// Upstream: identify_agent_in_job_detects_node_wrapped_pi_package_cli.
			name:  "node running pi's package entry point names pi",
			group: 123,
			processes: []foregroundProcess{job(123, "node.exe", "node.exe",
				`C:\Users\herdr\AppData\Roaming\npm\node_modules\@earendil-works\pi-coding-agent\dist\cli.js`)},
			want: "pi",
		},
		{
			// Upstream: identify_agent_in_job_detects_node_wrapped_pi_bundled_cli.
			name:  "node running pi's bundled entry point names pi",
			group: 123,
			processes: []foregroundProcess{job(123, "node.exe",
				`C:\Users\herdr\AppData\Local\pi-node\current\node.exe`,
				`C:\Users\herdr\AppData\Local\pi-node\current/node_modules/@earendil-works/pi-coding-agent/dist/bundle/cli.js`)},
			want: "pi",
		},
		{
			// Upstream: identify_agent_in_job_detects_node_wrapped_qwen, second
			// case. argv[0] is not a runtime name, so this reaches the Qwen
			// package-path rescue rather than the runtime unwrap.
			name:  "a renamed thread running qwen's package entry point names qwen",
			group: 123,
			processes: []foregroundProcess{job(123, "MainThread", "node.exe",
				`C:\Users\user\AppData\Roaming\npm\node_modules\@qwen-code\qwen-code\dist\index.js`)},
			want: "qwen",
		},
		{
			// mastracode's package entry point. It resolved to nothing until the
			// catalog gained a mastracode family, and this is the assertion that
			// records the change: the package path names the program, the alias
			// table can now place that name, and the process is identified.
			name:  "mastracode's package entry point names mastracode",
			group: 123,
			processes: []foregroundProcess{job(123, "node.exe", "node.exe",
				`C:\Users\herdr\AppData\Roaming\npm\node_modules\mastracode\dist\cli.js`)},
			want: "mastracode",
		},
		{
			// Upstream: identify_agent_in_job_prefers_wrapped_codex.
			name:  "wrapped codex beats a plain shell member",
			group: 123,
			processes: []foregroundProcess{
				job(1, "node", "node", "/path/to/bin/codex"),
				job(2, "bash", "bash"),
			},
			want: "codex",
		},
		{
			// Upstream: identify_agent_in_job_detects_python_version_wrapped_hermes.
			name:  "versioned python names hermes",
			group: 123,
			processes: []foregroundProcess{job(123, "python3.12",
				"/nix/store/example/bin/python3.12", "/nix/store/example/bin/hermes", "--resume", "session-id")},
			want: "hermes",
		},
		{
			// Upstream: identify_agent_in_job_detects_shell_wrapped_pi.
			name:      "sh running a pi script names pi",
			group:     123,
			processes: []foregroundProcess{job(1, "sh", "/bin/sh", "/tmp/test-bin/pi")},
			want:      "pi",
		},
		{
			// Upstream: identify_agent_in_job_detects_windows_cursor_install.
			name:  "cursor's bundled node install names cursor",
			group: 123,
			processes: []foregroundProcess{job(123, "node.exe",
				`C:\Users\user\AppData\Local\cursor-agent\versions\2026.08.11-e8db854\node.exe`,
				`C:\Users\user\AppData\Local\cursor-agent\versions\2026.08.11-e8db854\index.js`)},
			want: "cursor",
		},
		{
			// Upstream: identify_agent_in_job_detects_nix_wrapped_codex_from_cmdline_argv0.
			name:      "a nix wrapper's argv[0] names codex",
			group:     123,
			processes: []foregroundProcess{job(1, ".codex-wrapped", "/etc/profiles/per-user/user/bin/codex", "--model", "gpt-5")},
			want:      "codex",
		},
		{
			// Upstream: identify_agent_in_job_canonicalizes_nix_wrapped_aliases_from_cmdline_argv0.
			name:      "a nix wrapper's alias canonicalises to claude",
			group:     123,
			processes: []foregroundProcess{job(1, ".claude-code-wrapped", "/nix/store/example/bin/claude-code")},
			want:      "claude",
		},
		{
			// Upstream: identify_agent_in_job_detects_opencode2_as_opencode. The
			// name reported is the spelling found, not the family id.
			name:      "opencode2 keeps its own spelling",
			group:     123,
			processes: []foregroundProcess{job(123, "opencode2", "opencode2", "--standalone")},
			want:      "opencode",
		},
		{
			// Upstream: identify_agent_in_job_detects_python_script_named_codex.
			name:      "python running a script called codex names codex",
			group:     123,
			processes: []foregroundProcess{job(1, "python3", "python3", "/tmp/codex", "--model", "gpt-5")},
			want:      "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if agent, _ := identifyAgentInJob(tt.group, tt.processes); agent != tt.want {
				t.Fatalf("identifyAgentInJob = %q, want %q", agent, tt.want)
			}
		})
	}
}

// TestIdentifyAgentInJobRefusesRatherThanGuesses collects every case where
// upstream deliberately answers None. Each one is a false positive somebody
// would otherwise have shipped: an inline script is not a program name, a
// package Sidecar has not registered is not an agent, and a `cursor-agent`
// directory in a workspace is not an install.
func TestIdentifyAgentInJobRefusesRatherThanGuesses(t *testing.T) {
	tests := []struct {
		name      string
		group     int
		processes []foregroundProcess
	}{
		{
			name:      "node running an unknown script is not an agent",
			group:     123,
			processes: []foregroundProcess{job(123, "node", "node", "/Users/x/projects/server/index.js")},
		},
		{
			name:      "a bare node is not an agent",
			group:     123,
			processes: []foregroundProcess{job(123, "node", "node")},
		},
		{
			// Upstream: identify_agent_in_job_ignores_python_c_argument_named_codex.
			name:      "python -c payload is source text, not a program",
			group:     123,
			processes: []foregroundProcess{job(1, "python3", "python3", "-c", "import time; time.sleep(60)", "/tmp/codex")},
		},
		{
			// Upstream: identify_agent_in_job_ignores_node_eval_argument_named_codex.
			name:      "node -e payload is source text, not a program",
			group:     123,
			processes: []foregroundProcess{job(1, "node", "node", "-e", "setTimeout(() => {}, 60000)", "/tmp/codex")},
		},
		{
			// Upstream: identify_agent_in_job_ignores_shell_c_argument_named_codex.
			name:      "bash -c payload is source text, not a program",
			group:     123,
			processes: []foregroundProcess{job(1, "bash", "bash", "-c", "sleep 60", "/tmp/codex")},
		},
		{
			name:      "python -m names a module, not a script",
			group:     123,
			processes: []foregroundProcess{job(1, "python3", "python3", "-m", "codex")},
		},
		{
			name:      "a long eval flag with an inline value refuses too",
			group:     123,
			processes: []foregroundProcess{job(1, "node", "node", "--eval=require('/tmp/codex')")},
		},
		{
			name:      "a short eval flag with a glued payload refuses too",
			group:     123,
			processes: []foregroundProcess{job(1, "python3", "python3", "-cimport codex")},
		},
		{
			// node -r consumes its value, so the loader is not the program.
			name:      "a loader argument is not the script",
			group:     123,
			processes: []foregroundProcess{job(1, "node", "node", "-r", "/tmp/codex")},
		},
		{
			// Upstream: wrapped_agent_name_from_runtime_argv_ignores_plain_shell_flags.
			name:      "an interactive login shell has no script argument",
			group:     123,
			processes: []foregroundProcess{job(1, "bash", "bash", "-lc")},
		},
		{
			// tmux's argv names a session, never a program.
			name:      "nested tmux is not unwrapped",
			group:     123,
			processes: []foregroundProcess{job(1, "tmux", "tmux", "new-session", "-s", "claude")},
		},
		{
			// Upstream: identify_agent_in_job_ignores_non_cli_pi_package_scripts.
			name:  "a sibling script in pi's package is not pi",
			group: 123,
			processes: []foregroundProcess{job(123, "node.exe", "node.exe",
				`C:\Users\herdr\AppData\Roaming\npm\node_modules\@earendil-works\pi-coding-agent\scripts\build.js`)},
		},
		{
			name:  "an unrelated package's cli.js is not pi",
			group: 123,
			processes: []foregroundProcess{job(123, "node.exe", "node.exe",
				`C:\workspace\node_modules\other-package\dist\bundle\cli.js`)},
		},
		{
			// Upstream: identify_agent_in_job_ignores_invalid_windows_cursor_install_paths.
			name:  "a cursor-agent directory with a system node is not cursor",
			group: 123,
			processes: []foregroundProcess{job(123, "node.exe",
				`C:\Program Files\nodejs\node.exe`, `C:\workspace\cursor-agent\versions\test\index.js`)},
		},
		{
			name:  "cursor's postinstall script is not cursor",
			group: 123,
			processes: []foregroundProcess{job(123, "node.exe",
				`C:\Users\user\AppData\Local\cursor-agent\versions\2026.08.11-e8db854\node.exe`,
				`C:\Users\user\AppData\Local\cursor-agent\versions\2026.08.11-e8db854\scripts\postinstall.js`)},
		},
		{
			// Upstream: cmdline_argv0_agent_name_requires_exact_agent_basename.
			name:      "a helper whose name merely contains an agent is not that agent",
			group:     123,
			processes: []foregroundProcess{job(1, "my-codex-helper", "/tmp/my-codex-helper")},
		},
		{
			// A plain shell must never score as the agent "shell": upstream has
			// no such family and identifyAgentName drops the bucket.
			name:      "a plain interactive shell is not an agent",
			group:     100,
			processes: []foregroundProcess{job(100, "zsh", "-zsh")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if agent, name := identifyAgentInJob(tt.group, tt.processes); agent != "" {
				t.Fatalf("identifyAgentInJob = (%q, %q), want no identification", agent, name)
			}
		})
	}
}

// TestIdentifyAgentInJobPrefersTheProcessGroupLeader translates upstream's
// identify_agent_in_job_prefers_recognized_process_group_leader and
// ..._falls_back_when_process_group_leader_is_unrecognized. The pair is the
// whole reason scoring is two passes rather than one: an agent's own MCP helper
// is a recognisable node process further down the same job, and without the
// leader preference a Claude pane running a codex-named helper would be labelled
// Codex.
func TestIdentifyAgentInJobPrefersTheProcessGroupLeader(t *testing.T) {
	leaderKnown := []foregroundProcess{
		job(42, "claude", "claude"),
		job(43, "node", "node", "/tmp/mcp/bin/codex"),
	}
	if agent, name := identifyAgentInJob(42, leaderKnown); agent != "claude" || name != "claude" {
		t.Fatalf("recognised leader = (%q, %q), want (claude, claude)", agent, name)
	}

	leaderUnknown := []foregroundProcess{
		job(42, "bash", "bash"),
		job(43, "node", "node", "/tmp/mcp/bin/codex"),
	}
	if agent, name := identifyAgentInJob(42, leaderUnknown); agent != "codex" || name != "codex" {
		t.Fatalf("unrecognised leader = (%q, %q), want (codex, codex)", agent, name)
	}
}

// TestProcessPriorityRanksUnwrappedNamesHighest pins the score ladder directly,
// because the fallback pass in identifyAgentInJob is only correct if the ladder
// is: an unwrapped name outranks a literal one, and a literal one outranks a
// bare runtime.
func TestProcessPriorityRanksUnwrappedNamesHighest(t *testing.T) {
	unwrapped := processPriority(job(1, "node", "node", "/usr/local/bin/qwen"), "qwen")
	literal := processPriority(job(2, "claude", "claude"), "claude")
	runtime := processPriority(job(3, "node", "node"), "node")
	if unwrapped <= literal || literal <= runtime {
		t.Fatalf("priority ladder = unwrapped %d, literal %d, runtime %d; want strictly decreasing",
			unwrapped, literal, runtime)
	}

	// A job where the only two candidates differ by rung: the bun-wrapped amp
	// must win over a member literally named amp only if the ladder is applied,
	// so invert it to prove the pass is not just "first match wins".
	processes := []foregroundProcess{
		job(11, "amp", "amp"),
		job(12, "bun", "bun", "/home/x/.bun/bin/opencode"),
	}
	if agent, _ := identifyAgentInJob(10, processes); agent != "opencode" {
		t.Fatalf("scored identification = %q, want opencode (rung 3 beats rung 2)", agent)
	}
}

// TestGenericRuntimePredicateIsNotTheShellBucket pins the distinction the plan
// insists on: isGenericRuntimeOrShell scores process-tree candidates,
// identifyProcessName's "shell" bucket gates a launch, and the two lists differ
// in both directions. Collapsing them would either let ForegroundShellReady
// launch into a running node, or stop scoring from unwrapping a `nu` job.
func TestGenericRuntimePredicateIsNotTheShellBucket(t *testing.T) {
	for _, name := range []string{"node", "bun", "tmux", "cmd", "powershell", "pwsh",
		"python", "python3", "python3.12", "python3.12.1", "sh", "bash", "zsh", "fish"} {
		if !isGenericRuntimeOrShell(name) {
			t.Errorf("isGenericRuntimeOrShell(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"claude", "codex", "pi", "vim", "pythonw", "python-config",
		"python3.", "python3.x", "nodejs", "nu"} {
		if isGenericRuntimeOrShell(name) {
			t.Errorf("isGenericRuntimeOrShell(%q) = true, want false", name)
		}
	}
	// The two directions of the divergence, stated as assertions rather than
	// prose so a rename cannot quietly merge them.
	if identifyProcessName("nu") != "shell" || isGenericRuntimeOrShell("nu") {
		t.Error("`nu` must be a launchable shell and not a scoring runtime")
	}
	if identifyProcessName("node") != "" || !isGenericRuntimeOrShell("node") {
		t.Error("`node` must be a scoring runtime and not a launchable shell")
	}
	// A path and a launcher suffix normalise the same way the alias table does.
	if !isGenericRuntimeOrShell("/opt/homebrew/bin/node") || !isGenericRuntimeOrShell(`C:\nodejs\node.exe`) {
		t.Error("the predicate must read a path token, not only a bare name")
	}
}

// TestAgentEnvHintIsValidatedThroughTheAliasTable covers the hint's parser: the
// first record wins, an unknown value is no hint rather than an error, and the
// value goes through the one alias table so `SIDECAR_AGENT=claude-code` names
// Claude and `SIDECAR_AGENT=bash` names nothing.
func TestAgentEnvHintIsValidatedThroughTheAliasTable(t *testing.T) {
	environ := func(entries ...string) []byte {
		return []byte(strings.Join(entries, "\x00") + "\x00")
	}
	tests := []struct {
		name    string
		environ []byte
		want    string
	}{
		{name: "a known agent is accepted", environ: environ("PATH=/usr/bin", "SIDECAR_AGENT=qwen", "TERM=xterm"), want: "qwen"},
		{name: "an alias canonicalises", environ: environ("SIDECAR_AGENT=claude-code"), want: "claude"},
		{name: "case and spacing are forgiven", environ: environ("SIDECAR_AGENT= Codex "), want: "codex"},
		{name: "an unknown value is no hint", environ: environ("SIDECAR_AGENT=not-an-agent"), want: ""},
		{name: "a shell is not a provider", environ: environ("SIDECAR_AGENT=bash"), want: ""},
		{name: "an empty value is no hint", environ: environ("SIDECAR_AGENT="), want: ""},
		{name: "no hint at all", environ: environ("PATH=/usr/bin", "TERM=xterm"), want: ""},
		{name: "an empty environment is no hint", environ: nil, want: ""},
		{
			// Upstream returns on the first match rather than continuing to
			// search, and a second record is not more trustworthy than the
			// first. Pinned so nobody "improves" it into a scan.
			name:    "the first record wins even when it is invalid",
			environ: environ("SIDECAR_AGENT=nonsense", "SIDECAR_AGENT=claude"),
			want:    "",
		},
		{
			// A variable that merely ends in the hint's name is not the hint.
			name:    "a suffix match is not the variable",
			environ: environ("MY_SIDECAR_AGENT=claude"),
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseAgentEnvHint(tt.environ); got != tt.want {
				t.Fatalf("parseAgentEnvHint = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAgentIdentityPrefersEvidenceOverAHint pins the precedence documented on
// ResolveForegroundAgent, using a stubbed hint reader so each rung can be driven
// independently of a live process table.
//
// It deliberately diverges from upstream's `probe_foreground_process_from_jobs`
// (src/pane.rs:608 at d08e4468), which reads the leader's hint *before*
// identifying the leader. Herdr's `HERDR_AGENT` is written by Herdr's own
// wrapper; Sidecar's `SIDECAR_AGENT` is a bare environment variable Sidecar only
// ever reads, so under upstream's order `export SIDECAR_AGENT=codex` in a Claude
// pane would relabel it. Here evidence wins and the hint answers only where
// evidence is silent — which is the case the hint exists for.
//
// "shell" is not evidence of an agent, so it does not block a hint: an
// interactive shell owning the job is exactly where something under it may still
// have published one.
func TestAgentIdentityPrefersEvidenceOverAHint(t *testing.T) {
	restore := readProcessAgentHint
	t.Cleanup(func() { readProcessAgentHint = restore })

	tests := []struct {
		name      string
		group     int
		processes []foregroundProcess
		hints     map[int]string
		want      string
	}{
		{
			// The divergence, stated as a case: a hint cannot rename a pane
			// whose process evidence already names an agent.
			name:      "the leader's own identity outranks a conflicting leader hint",
			group:     10,
			processes: []foregroundProcess{job(10, "node", "node", "/usr/local/bin/qwen")},
			hints:     map[int]string{10: "pi"},
			want:      "qwen",
		},
		{
			name:      "the leader's identity outranks a member's hint",
			group:     10,
			processes: []foregroundProcess{job(10, "node", "node", "/usr/local/bin/qwen"), job(11, "sandbox", "sandbox")},
			hints:     map[int]string{11: "pi"},
			want:      "qwen",
		},
		{
			name:      "scored identification outranks a member hint too",
			group:     10,
			processes: []foregroundProcess{job(10, "sandbox", "sandbox"), job(11, "node", "node", "/usr/local/bin/qwen")},
			hints:     map[int]string{11: "pi"},
			want:      "qwen",
		},
		{
			name:      "the leader's hint answers once nothing is identified",
			group:     10,
			processes: []foregroundProcess{job(10, "sandbox", "sandbox"), job(11, "helper", "helper")},
			hints:     map[int]string{10: "claude"},
			want:      "claude",
		},
		{
			name:      "a member hint is used once the leader publishes none",
			group:     10,
			processes: []foregroundProcess{job(10, "sandbox", "sandbox"), job(11, "helper", "helper")},
			hints:     map[int]string{11: "cline"},
			want:      "cline",
		},
		{
			name:      "the leader's hint outranks a member's",
			group:     10,
			processes: []foregroundProcess{job(10, "sandbox", "sandbox"), job(11, "helper", "helper")},
			hints:     map[int]string{10: "claude", 11: "cline"},
			want:      "claude",
		},
		{
			// An interactive shell owning the job is not an agent identity, so
			// it must not swallow the hint the way `qwen` above does.
			name:      "a hint still answers over a shell",
			group:     10,
			processes: []foregroundProcess{job(10, "zsh", "-zsh")},
			hints:     map[int]string{10: "claude"},
			want:      "claude",
		},
		{
			name:      "an unknown hint value does not displace real evidence",
			group:     10,
			processes: []foregroundProcess{job(10, "node", "node", "/usr/local/bin/qwen")},
			hints:     map[int]string{10: ""},
			want:      "qwen",
		},
		{
			name:      "nothing anywhere names nothing",
			group:     10,
			processes: []foregroundProcess{job(10, "sandbox", "sandbox")},
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readProcessAgentHint = func(pid int) string { return tt.hints[pid] }
			identity := foregroundEvidenceIdentity(tt.group, tt.processes)
			if got := foregroundAgentIdentity(tt.group, identity, tt.processes); got != tt.want {
				t.Fatalf("foregroundAgentIdentity = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestEvidenceIdentityNeverReadsAHint is the guard for the seam that keeps
// SIDECAR_AGENT away from lifecycle authority. ResolveForegroundProcess feeds
// lifecycleenv.OccupantKind, which can *refuse* a hook report; an environment
// variable anyone in the session can set must never be able to cause that.
//
// The check is mechanical rather than by inspection: the hint reader is replaced
// with one that fails the test if it is called at all.
func TestEvidenceIdentityNeverReadsAHint(t *testing.T) {
	restore := readProcessAgentHint
	t.Cleanup(func() { readProcessAgentHint = restore })
	readProcessAgentHint = func(pid int) string {
		t.Fatalf("the evidence-only resolver read the environment of pid %d", pid)
		return ""
	}

	processes := []foregroundProcess{job(10, "node", "node", "/usr/local/bin/qwen")}
	if got := foregroundEvidenceIdentity(10, processes); got != "qwen" {
		t.Fatalf("foregroundEvidenceIdentity = %q, want qwen", got)
	}
	if got := foregroundEvidenceIdentity(10, []foregroundProcess{job(10, "zsh", "-zsh")}); got != "shell" {
		t.Fatalf("foregroundEvidenceIdentity for an idle shell = %q, want shell", got)
	}
	if got := foregroundEvidenceIdentity(10, []foregroundProcess{job(10, "mystery", "mystery")}); got != "" {
		t.Fatalf("foregroundEvidenceIdentity for an unknown program = %q, want empty", got)
	}

	// The same guard one level up, because the cost ladder now decides who
	// reads what: ResolveForegroundProcess must take the evidence rung and
	// nothing else, whatever is in the pane's environment.
	groupRestore, processRestore := readForegroundProcessGroup, readForegroundProcesses
	t.Cleanup(func() {
		readForegroundProcessGroup, readForegroundProcesses = groupRestore, processRestore
		resetForegroundIdentityCache()
	})
	resetForegroundIdentityCache()
	readForegroundProcessGroup = func(int) int { return 10 }
	readForegroundProcesses = func(int) []foregroundProcess {
		return []foregroundProcess{job(10, "sandbox", "sandbox")}
	}
	if got := ResolveForegroundProcess(200); got != "" {
		t.Fatalf("ResolveForegroundProcess = %q, want empty", got)
	}
}

// stubForegroundPlatform replaces the three platform seams for one test: the
// foreground group of a pane, the process-table walk, and the per-process hint
// read. walks counts the walks, which is the only mechanical way to hold the
// cost ladder to its claim.
func stubForegroundPlatform(t *testing.T, group int, processes []foregroundProcess, hints map[int]string) (walks *int) {
	t.Helper()
	groupRestore, processRestore, hintRestore := readForegroundProcessGroup, readForegroundProcesses, readProcessAgentHint
	t.Cleanup(func() {
		readForegroundProcessGroup, readForegroundProcesses, readProcessAgentHint = groupRestore, processRestore, hintRestore
		resetForegroundIdentityCache()
	})
	resetForegroundIdentityCache()

	count := 0
	readForegroundProcessGroup = func(int) int { return group }
	readForegroundProcesses = func(int) []foregroundProcess {
		count++
		return processes
	}
	readProcessAgentHint = func(pid int) string { return hints[pid] }
	return &count
}

// TestASandboxPaneReachesTheHintWithoutAWalk is the deliverable of the hint's
// redesign, stated as the case it exists for.
//
// A pane whose pane_current_command is `docker` — or bwrap, sandbox-exec,
// firejail, nix, ssh — hides its agent behind a wrapper the alias table cannot
// place. It used to reach the hint never: both callers gated on
// NeedsProcessIdentity, which is true only for node/bun/agent/python, so the
// only panes that could read a hint were the ones process evidence already
// answered. The pane now pays two per-process reads and no process-table walk,
// because the foreground process group id is the group leader's pid.
//
// This is the Linux shape and only the Linux shape: stubForegroundPlatform
// declares a platform that has hints, because macOS returns another process's
// environment to nobody. There the same pane reads nothing at all, which
// TestTheTwoResolversShareOneScan is the regression for a cache that used to
// thrash. One entry per pane carried a single "hints were read" bit, so an
// evidence-only resolve overwrote a hinted one and the next hinted resolve
// re-walked the process table — roughly doubling the walks for any pane that
// both agentcontrol's observer and the workspace poll were watching, inside a
// window meant to hold them to one.
//
// The entry now carries the members the walk collected, so the hint pass runs
// against them instead of walking again, in either arrival order.
func TestTheTwoResolversShareOneScan(t *testing.T) {
	processes := []foregroundProcess{job(10, "sandbox", "sandbox"), job(11, "helper", "helper")}

	t.Run("evidence first", func(t *testing.T) {
		walks := stubForegroundPlatform(t, 10, processes, map[int]string{11: "cline"})
		if got := ResolveForegroundProcess(200); got != "" {
			t.Fatalf("ResolveForegroundProcess = %q, want empty", got)
		}
		if got := ResolveForegroundAgent(200, "node"); got != "cline" {
			t.Fatalf("ResolveForegroundAgent = %q, want cline", got)
		}
		if *walks != 1 {
			t.Fatalf("walks = %d, want 1: the hint pass must re-use the members the scan collected", *walks)
		}
	})

	t.Run("hint first", func(t *testing.T) {
		walks := stubForegroundPlatform(t, 10, processes, map[int]string{11: "cline"})
		if got := ResolveForegroundAgent(200, "node"); got != "cline" {
			t.Fatalf("ResolveForegroundAgent = %q, want cline", got)
		}
		if got := ResolveForegroundProcess(200); got != "" {
			t.Fatalf("ResolveForegroundProcess = %q, want empty", got)
		}
		if *walks != 1 {
			t.Fatalf("walks = %d, want 1: the evidence answer is cached by the same scan", *walks)
		}
	})

	// A cheap leader-hint resolve must not evict a scan's evidence either: the
	// halves are stamped independently.
	t.Run("a cheap resolve does not evict the evidence half", func(t *testing.T) {
		walks := stubForegroundPlatform(t, 10, processes, map[int]string{10: "claude"})
		if got := ResolveForegroundProcess(200); got != "" {
			t.Fatalf("ResolveForegroundProcess = %q, want empty", got)
		}
		if got := ResolveForegroundAgent(200, "docker"); got != "claude" {
			t.Fatalf("ResolveForegroundAgent = %q, want claude", got)
		}
		if got := ResolveForegroundProcess(200); got != "" {
			t.Fatalf("ResolveForegroundProcess = %q, want empty", got)
		}
		if *walks != 1 {
			t.Fatalf("walks = %d, want 1", *walks)
		}
	})
}

// TestNeedsProcessIdentityStaysCheapForIdleShells pins the cost gate. Answering
// true here means a process-table walk per poll per pane, so the interactive
// shells must stay out even though the scoring predicate names them.
func TestNeedsProcessIdentityStaysCheapForIdleShells(t *testing.T) {
	for _, command := range []string{"node", "bun", "agent", "python", "python3", "python3.12", "node.exe"} {
		if !NeedsProcessIdentity(command) {
			t.Errorf("NeedsProcessIdentity(%q) = false; a runtime that hides an agent must be resolved", command)
		}
	}
	for _, command := range []string{"sh", "bash", "zsh", "fish", "nu", "tmux", "vim", "claude", "pi", ""} {
		if NeedsProcessIdentity(command) {
			t.Errorf("NeedsProcessIdentity(%q) = true; that makes every such pane scan the process table on every poll", command)
		}
	}
}

// TestIdentifiedProcessNameKeepsItsOwnSpelling pins the second return value of
// identifyAgentInJob: it is the spelling the agent was found under, not the
// family id. `opencode2` is a real install alias and reporting it as `opencode`
// would lose the only clue about which binary is running.
func TestIdentifiedProcessNameKeepsItsOwnSpelling(t *testing.T) {
	agent, name := identifyAgentInJob(123, []foregroundProcess{job(123, "opencode2", "opencode2", "--standalone")})
	if agent != "opencode" || name != "opencode2" {
		t.Fatalf("identifyAgentInJob = (%q, %q), want (opencode, opencode2)", agent, name)
	}
}
