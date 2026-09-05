package agentactivity

import (
	"path/filepath"
	"slices"
	"strings"
)

// Process-tree scoring: naming the agent behind a generic runtime.
//
// Ported from Herdr's src/detect/mod.rs at d08e4468 — `identify_agent_in_job`
// and the helper chain beneath it. Sidecar previously matched argv[0] basenames
// only, which is enough for an agent that renames its own process and useless
// for the common npm install shape: a `#!/usr/bin/env node` shim leaves the
// *interpreter* in argv[0], tmux reports `node`, and neither identity input
// names the agent, so the pane is never claimed at all.
//
// The measured case is a synthetic `#!/usr/bin/env node` shim named `qwen`
// (comm `node`, argv `["node", ".../bin/qwen"]`), which went from
// `evidence=provider-not-identified` to `kind=qwen status=idle`. Pi is *not*
// that case: it rewrites its own process title, so argv[0] and comm are both
// `pi` and only tmux's pane_current_command says `node`. Pi was already
// identified here and was refused one step later, by the gate — which is why
// `sidecar agent start --kind pi` ran to its full timeout, and why widening this
// resolver alone would not have fixed it. See
// docs/plans/implemented/herdr-parity-close-the-gap.md, Slice 3's result, for both
// measurements.
//
// The shape of the fix is upstream's: when a process is a generic runtime,
// unwrap it using its own argv; otherwise take argv[0]/comm as the identity;
// score the members of the foreground job so a real program beats a runtime;
// and prefer the process group leader over everything when it is recognisable.
//
// # What is deliberately not ported
//
// `windows_cmd_arg_agent_name`, `powershell_arg_agent_name`,
// `command_text_agent_name` and `command_text_token`. Sidecar has no Windows
// process-identity adapter — process_identity_other.go answers nothing on every
// platform that is not darwin or linux — so those four arms could never be
// reached, and unreachable code with no way to test it is how a port rots. The
// `cmd`/`powershell`/`pwsh` arms of wrappedAgentNameFromRuntimeArgv are absent
// for the same reason and are named in its default case so the omission is
// visible rather than forgotten. If a Windows adapter is ever written, these
// come back with it.

// identifyAgentName is this port's spelling of upstream's `identify_agent`.
//
// It is identifyProcessName — the single alias table this package owns — minus
// the "shell" bucket. Upstream has no such bucket: its `lookup_agent` answers
// None for bash and zsh, so a shell is an unrecognised program and scoring skips
// it. Sidecar's table names shells because ForegroundShellReady needs to, and
// folding that answer back into scoring would let a plain `bash` pane be
// "identified" as the agent "shell".
//
// Routing every ported helper through identifyProcessName rather than a second
// table is the rule from the plan: the vocabulary must not fork, and
// catalog_vocabulary_test.go pins that one table against agentcatalog.
func identifyAgentName(name string) string {
	if agent := identifyProcessName(name); agent != "" && agent != "shell" {
		return agent
	}
	return ""
}

// identifyAgentInJob names the agent running in a foreground job, and returns
// the process-name spelling it was identified from.
//
// Upstream: `identify_agent_in_job`, src/detect/mod.rs:243 at d08e4468. Two
// passes, in this order and for this reason:
//
//  1. The process group leader wins outright if it is recognisable. The leader
//     is the program the pane is actually running; a node MCP helper further
//     down the job is a child of the agent, not the agent.
//  2. Otherwise every member is scored and the highest score wins, with the
//     first of an equal pair kept. The ladder is in processPriority.
func identifyAgentInJob(group int, processes []foregroundProcess) (agent, processName string) {
	for _, process := range processes {
		if process.PID != group {
			continue
		}
		candidate := normalizedProcessName(process)
		if identified := identifyAgentName(candidate); identified != "" {
			return identified, candidate
		}
		break
	}

	bestScore := 0
	for _, process := range processes {
		candidate := normalizedProcessName(process)
		identified := identifyAgentName(candidate)
		if identified == "" {
			continue
		}
		score := processPriority(process, candidate)
		if agent != "" && bestScore >= score {
			continue
		}
		bestScore, agent, processName = score, identified, candidate
	}
	return agent, processName
}

// processPriority is upstream's `process_priority` (src/detect/mod.rs:685 at
// d08e4468), a three-rung ladder over one question: how much did we have to
// work to get this name, and how much does that work vouch for it.
//
//	3 — the resolved name differs from the kernel's comm, so something was
//	    genuinely unwrapped (node → qwen). That is the strongest evidence in the
//	    job: nothing produces it by accident.
//	2 — the name equals comm and is not a runtime, i.e. the process is literally
//	    called `claude`.
//	1 — the name equals comm and is a generic runtime. A bare `node` that
//	    resolved to nothing better is the weakest thing that can still match.
func processPriority(process foregroundProcess, normalizedName string) int {
	lower := strings.ToLower(normalizedName)
	if lower != strings.ToLower(process.Comm) {
		return 3
	}
	if !isGenericRuntimeOrShell(lower) {
		return 2
	}
	return 1
}

// normalizedProcessName resolves one process to the best name available for it,
// which may be the agent hidden behind a runtime.
//
// Upstream: `normalized_process_name`, src/detect/mod.rs:359 at d08e4468. The
// order of the four attempts is upstream's and matters:
//
//  1. If the effective name is a generic runtime, unwrap it from its own argv.
//  2. If the effective name is already an agent, keep it.
//  3. The Qwen special case: argv[0] is a node/bun interpreter even though the
//     effective name is not a runtime (a renamed thread, `MainThread` on some
//     builds), and the wrapped script is Qwen's package entry point. Upstream
//     restricts this rescue to Qwen and so does this port — widening it is a new
//     decision, not a transcription.
//  4. Otherwise fall back to argv[0], then to the cmdline's first token.
func normalizedProcessName(process foregroundProcess) string {
	effective := process.Argv0
	if strings.TrimSpace(effective) == "" {
		effective = process.Comm
	}
	lower := strings.ToLower(effective)

	if isGenericRuntimeOrShell(lower) {
		if wrapped := wrappedAgentNameFromRuntimeArgv(lower, process.Argv); wrapped != "" {
			return wrapped
		}
	}

	if identifyAgentName(effective) != "" {
		return effective
	}

	if len(process.Argv) > 0 {
		switch normalizeProcessName(process.Argv[0]) {
		case "node", "bun":
			wrapped := wrappedAgentNameFromRuntimeArgv(process.Argv[0], process.Argv)
			if wrapped != "" && identifyAgentName(wrapped) == "qwen" {
				return wrapped
			}
		}
	}

	if wrapped := argv0AgentName(process.Argv); wrapped != "" {
		return wrapped
	}
	// Upstream falls back to the process's raw cmdline here, which on Linux is
	// /proc/<pid>/cmdline and on Sidecar's adapters is argv re-joined with
	// spaces. So this can only ever differ from argv0AgentName when argv[0]
	// itself contains whitespace, in which case it sees a truncated path and
	// declines. It is kept because the ordering is upstream's and because
	// cmdlineArgv0AgentName is the branch that canonicalises a Nix wrapper's
	// alias path; dropping it would make the next sync's diff lie about what
	// was ported.
	if wrapped := cmdlineArgv0AgentName(strings.Join(process.Argv, " ")); wrapped != "" {
		return wrapped
	}

	return effective
}

// nodeEvalFlags and shellEvalFlags are the arguments whose payload is an inline
// program rather than a path. A token after one of them is source text, so
// upstream refuses the whole process rather than reading a word out of a script;
// `python3 -c 'import time' /tmp/codex` must not be Codex.
var (
	nodeEvalFlags   = []string{"-e", "--eval", "-p", "--print"}
	shellEvalFlags  = []string{"-c"}
	pythonEvalFlags = []string{"-c"}
	pythonModFlags  = []string{"-m"}
)

// wrappedAgentNameFromRuntimeArgv unwraps a runtime using its own argv.
//
// Upstream: `wrapped_agent_name_from_runtime_argv`, src/detect/mod.rs:397 at
// d08e4468. The `cmd`, `powershell` and `pwsh` arms are deliberately absent —
// see this file's header. `tmux` is present as an explicit refusal, as upstream
// has it: a pane whose foreground process is tmux is nested tmux, and its argv
// names a session, not a program.
func wrappedAgentNameFromRuntimeArgv(runtime string, argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	name := normalizeProcessName(runtime)
	switch {
	case name == "node":
		if cursor := cursorAgentNameFromBundledNodeArgv(argv); cursor != "" {
			return cursor
		}
		return scriptArgAgentName(argv, nodeEvalFlags, nil)
	case name == "bun":
		return scriptArgAgentName(argv, nodeEvalFlags, nil)
	case isPythonRuntime(name):
		return scriptArgAgentName(argv, pythonEvalFlags, pythonModFlags)
	case name == "sh" || name == "bash" || name == "zsh" || name == "fish":
		return scriptArgAgentName(argv, shellEvalFlags, nil)
	default:
		// tmux, and the unported cmd/powershell/pwsh.
		return ""
	}
}

// scriptArgAgentName walks a runtime's argv looking for the first positional
// argument — the script it was asked to run — and names the agent from it.
//
// Upstream: `script_arg_agent_name`, src/detect/mod.rs:521 at d08e4468. Three
// rules, each of which exists to avoid a specific false positive:
//
//   - `--` ends option parsing, so the next token is the script whatever it
//     looks like.
//   - an eval or module flag means the payload is source text or a module name,
//     and the answer is a refusal rather than a guess. Refusing is the point:
//     an inline script is not a program name.
//   - a flag that takes a value consumes the next token, so `node -r ./codex
//     app.js` is not Codex.
func scriptArgAgentName(argv, evalFlags, moduleFlags []string) string {
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			if i+1 < len(argv) {
				return agentNameFromPathToken(argv[i+1])
			}
			return ""
		}
		if flagMatches(arg, evalFlags) || flagMatches(arg, moduleFlags) {
			return ""
		}
		if strings.HasPrefix(arg, "-") {
			if optionTakesValue(arg) {
				i++
			}
			continue
		}
		return agentNameFromPathToken(arg)
	}
	return ""
}

// flagMatches accepts a flag spelled bare (`-c`), with an attached short payload
// (`-cprint(1)`), or as a long option with an inline value (`--eval=...`).
// Upstream: `flag_matches`, src/detect/mod.rs:551 at d08e4468.
func flagMatches(arg string, flags []string) bool {
	for _, flag := range flags {
		if arg == flag || shortFlagPayload(arg, flag) || longFlagValue(arg, flag) {
			return true
		}
	}
	return false
}

// shortFlagPayload: `-c` with the program glued on, as in `-cimport sys`.
// Upstream: `short_flag_payload`, src/detect/mod.rs:557 at d08e4468.
func shortFlagPayload(arg, flag string) bool {
	return strings.HasPrefix(flag, "-") && !strings.HasPrefix(flag, "--") &&
		strings.HasPrefix(arg, flag) && len(arg) > len(flag)
}

// longFlagValue: `--eval=...`. Upstream: `long_flag_value`,
// src/detect/mod.rs:564 at d08e4468.
func longFlagValue(arg, flag string) bool {
	if !strings.HasPrefix(flag, "--") {
		return false
	}
	rest, ok := strings.CutPrefix(arg, flag)
	return ok && strings.HasPrefix(rest, "=")
}

// optionTakesValue names the runtime options whose value is a separate token.
// Upstream: `option_takes_value`, src/detect/mod.rs:571 at d08e4468 — the list
// is upstream's verbatim, node's loader family plus python's short options.
func optionTakesValue(arg string) bool {
	switch arg {
	case "-r", "--require", "--loader", "--import", "--experimental-loader",
		"--inspect-port", "-W", "-X", "-S", "-L", "-o":
		return true
	}
	return false
}

// argv0AgentName names the agent from argv[0] alone.
// Upstream: `argv0_agent_name`, src/detect/mod.rs:587 at d08e4468.
func argv0AgentName(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return agentNameFromPathToken(argv[0])
}

// cmdlineArgv0AgentName names the agent from the first whitespace-delimited
// token of a raw command line.
// Upstream: `cmdline_argv0_agent_name`, src/detect/mod.rs:591 at d08e4468.
func cmdlineArgv0AgentName(cmdline string) string {
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return ""
	}
	return agentNameFromPathToken(fields[0])
}

// agentNameFromPathToken is the single place a path-shaped argv token becomes a
// name. Upstream: `agent_name_from_path_token`, src/detect/mod.rs:595 at
// d08e4468. Three attempts, cheapest first: the basename, a known package entry
// point, then the symlink target on disk.
func agentNameFromPathToken(token string) string {
	trimmed := strings.Trim(token, `"'`)
	if trimmed == "" || strings.HasPrefix(trimmed, "-") {
		return ""
	}
	if name := agentNameFromBasename(pathBasename(trimmed)); name != "" {
		return name
	}
	if name := agentNameFromKnownPackagePath(trimmed); name != "" {
		return name
	}
	return resolvedAgentNameFromPathToken(trimmed)
}

// agentNameFromKnownPackagePath recognises the package entry points that carry
// no agent name in their basename at all — the script is `cli.js` or `index.js`
// and the identity is in the directory above it.
//
// Upstream: `agent_name_from_known_package_path`, src/detect/mod.rs:606 at
// d08e4468. The suffix match is anchored at the end of the path and every
// component must match, which is what keeps `C:\workspace\dist\bundle\cli.js`
// and a sibling `scripts/build.js` out.
//
// Upstream names three packages. Two resolve here: `pi` and `qwen` are both
// registered Sidecar families. `mastracode` is not — it is Slice 2's work — so
// the returned name simply fails to resolve through identifyProcessName and the
// process goes unidentified. That is the correct outcome and it is left as a
// data consequence rather than a special case: when mastracode is registered,
// this path starts working with no edit here. Do not "fix" it by dropping the
// arm.
func agentNameFromKnownPackagePath(path string) string {
	raw := pathComponents(path)
	endsWith := func(suffix ...string) bool {
		if len(raw) < len(suffix) {
			return false
		}
		tail := raw[len(raw)-len(suffix):]
		for i := range suffix {
			if !strings.EqualFold(tail[i], suffix[i]) {
				return false
			}
		}
		return true
	}
	if endsWith("node_modules", "@earendil-works", "pi-coding-agent", "dist", "cli.js") ||
		endsWith("node_modules", "@earendil-works", "pi-coding-agent", "dist", "bundle", "cli.js") {
		return "pi"
	}

	normalized := make([]string, len(raw))
	for i, component := range raw {
		normalized[i] = normalizeProcessName(component)
	}
	for i := 0; i+5 <= len(normalized); i++ {
		if slices.Equal(normalized[i:i+5], []string{"node_modules", "@qwen-code", "qwen-code", "dist", "index"}) {
			return "qwen"
		}
	}
	for i := 0; i+4 <= len(normalized); i++ {
		if slices.Equal(normalized[i:i+4], []string{"node_modules", "mastracode", "dist", "cli"}) {
			// Not a Sidecar family yet. See this function's doc comment.
			return "mastracode"
		}
	}
	return ""
}

// resolvedAgentNameFromPathToken follows a symlink and names the agent from what
// it points at. Upstream: `resolved_agent_name_from_path_token`,
// src/detect/mod.rs:652 at d08e4468.
//
// This is what recovers Cursor from its official install, which puts a
// `cursor-agent` binary behind an `agent` symlink on PATH. It touches the
// filesystem, so it runs last and only for tokens that are actually paths —
// upstream's `components().count() < 2` guard, spelled here as "contains a
// separator". A bare name is never stat'ed.
func resolvedAgentNameFromPathToken(token string) string {
	if !strings.ContainsAny(token, `/\`) {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(token)
	if err != nil {
		return ""
	}
	return agentNameFromBasename(filepath.Base(resolved))
}

// agentNameFromBasename is upstream's `agent_name_from_basename`
// (src/detect/mod.rs:663 at d08e4468): parse the basename through the alias
// table and return the canonical family id, so `ghcs` becomes `copilot` and
// `claude-code` becomes `claude`.
func agentNameFromBasename(basename string) string {
	return identifyAgentName(basename)
}

// pathBasename is upstream's `path_basename` (src/detect/mod.rs:679 at
// d08e4468): the last non-empty component across either separator, with the
// original spelling intact.
//
// It is not normalizeProcessName, and the difference is load-bearing:
// normalizeProcessName also lowercases and strips one launcher suffix, so it
// turns `cli.js` into `cli` — which is exactly the extension
// agentNameFromKnownPackagePath and cursorAgentNameFromBundledNodeArgv match on.
func pathBasename(path string) string {
	components := pathComponents(path)
	if len(components) == 0 {
		return path
	}
	return components[len(components)-1]
}

// pathComponents splits on either separator and drops empty components, so a
// Windows path parses on Linux and a trailing slash does not produce an empty
// basename. Upstream gets this from Rust's `split`+`filter` inline; it is named
// here because four call sites want it.
func pathComponents(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
}

// pathParentAndBasename splits a path at its last separator, requiring both
// halves to be non-empty. Upstream: `path_parent_and_basename`,
// src/detect/mod.rs:437 at d08e4468.
func pathParentAndBasename(path string) (parent, basename string, ok bool) {
	split := strings.LastIndexAny(path, `/\`)
	if split < 0 {
		return "", "", false
	}
	parent = strings.TrimRight(path[:split], `/\`)
	basename = path[split+1:]
	if parent == "" || basename == "" {
		return "", "", false
	}
	return parent, basename, true
}

// cursorAgentNameFromBundledNodeArgv recognises Cursor's bundled-Node install,
// where both the interpreter and the script live in the same versioned
// directory: `.../cursor-agent/versions/<version>/node.exe` running
// `.../cursor-agent/versions/<version>/index.js`.
//
// Upstream: `cursor_agent_name_from_bundled_node_argv`, src/detect/mod.rs:414 at
// d08e4468. Every one of the four conditions is a refusal upstream tests
// directly: the script must be `index.js` and not `scripts/postinstall.js`, the
// two directories must be the same one, and the three trailing components must
// spell `cursor-agent/versions/<non-empty>`. A `cursor-agent` directory
// somewhere in a workspace with a system node does not match.
func cursorAgentNameFromBundledNodeArgv(argv []string) string {
	if len(argv) < 2 {
		return ""
	}
	runtimeParent, runtimeName, ok := pathParentAndBasename(argv[0])
	if !ok {
		return ""
	}
	scriptParent, scriptName, ok := pathParentAndBasename(argv[1])
	if !ok {
		return ""
	}
	if !strings.EqualFold(runtimeName, "node.exe") ||
		!strings.EqualFold(scriptName, "index.js") ||
		!strings.EqualFold(runtimeParent, scriptParent) {
		return ""
	}

	tail := pathComponents(runtimeParent)
	if len(tail) < 3 {
		return ""
	}
	version, versions, pkg := tail[len(tail)-1], tail[len(tail)-2], tail[len(tail)-3]
	if !strings.EqualFold(pkg, "cursor-agent") || !strings.EqualFold(versions, "versions") ||
		strings.TrimSpace(version) == "" {
		return ""
	}
	return "cursor"
}

// isGenericRuntimeOrShell reports whether a process name is a shared runtime or
// a shell rather than a program identity.
//
// Upstream: `is_generic_runtime_or_shell`, src/detect/mod.rs:696 at d08e4468,
// with upstream's list verbatim.
//
// This is a scoring predicate and it is deliberately NOT the "shell" bucket in
// identifyProcessName. That bucket answers a launch-readiness question for
// ForegroundShellReady — "is this pane's sole foreground process an interactive
// shell I may type into" — and it names `nu` and does not name `node`, `tmux`,
// `cmd` or python. This one answers "should I try to look past this process for
// the real program", which is a different question with a different list, and
// merging them would either make ForegroundShellReady accept a launch into a
// running `node`, or stop this scoring from unwrapping `nu`-launched work.
func isGenericRuntimeOrShell(name string) bool {
	name = normalizeProcessName(name)
	if isPythonRuntime(name) {
		return true
	}
	switch name {
	case "sh", "bash", "zsh", "fish", "tmux", "node", "bun", "cmd", "powershell", "pwsh":
		return true
	}
	return false
}

// isPythonRuntime matches `python` and `python<version>` where the version is
// dot-separated digits: python3, python3.12, python3.12.1. Upstream:
// `is_python_runtime`, src/detect/mod.rs:713 at d08e4468. `python-config` and
// `pythonw` are not runtimes and must not match.
func isPythonRuntime(name string) bool {
	if name == "python" {
		return true
	}
	version, ok := strings.CutPrefix(name, "python")
	if !ok || version == "" {
		return false
	}
	for _, part := range strings.Split(version, ".") {
		if part == "" {
			return false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}
