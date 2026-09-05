package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/agentcatalog"
	"github.com/marcus/sidecar/internal/agentcontrol"
	"github.com/marcus/sidecar/internal/agentsession"
	"github.com/marcus/sidecar/internal/agenttranscript"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/managedtarget"
	"github.com/marcus/sidecar/internal/shellstate"
	"github.com/marcus/sidecar/internal/workspaceops"
)

var newAgentTerminal = func() agentcontrol.Terminal { return agentcontrol.NewLocalTerminal() }

func agentCommand() *Command {
	common := []Flag{{Name: "--project", Arg: "NAME", Summary: "Target project (slug, basename, or path; or a worktree it created, by path or basename)"}, {Name: "--shell", Arg: "NAME", Summary: "Resolve the project from a registered shell"}, {Name: "--host", Arg: "ID", Summary: "Run the verb on a registered remote host (requires an explicit TARGET)"}, {Name: "--json", Summary: "Write stable structured JSON", Bool: true}, {Name: "--help", Short: "-h", Summary: "Show this help", Bool: true}}
	sessionRefFlag := Flag{Name: "--include-session-ref", Summary: "Include the bound conversation's value, not only its presence", Bool: true}
	listFlags := append(append([]Flag{}, common...), sessionRefFlag)
	list := &Command{Name: "list", Summary: "List live managed agents", Usage: "sidecar agent list [--project NAME] [--include-session-ref] [--json]", Flags: listFlags, ExitCodes: agentExitCodes(), Examples: []Example{{Command: "sidecar agent list --json"}}, Agent: AgentDoc{Invocation: "sidecar agent list --json", Summary: "List live managed agents and their current status"}, Run: runAgentList}
	getFlags := append(append([]Flag{}, common...), sessionRefFlag)
	get := &Command{Name: "get", Summary: "Get one managed agent", Usage: "sidecar agent get [TARGET] [--project NAME] [--include-session-ref] [--json]", Long: "TARGET is a managed tmux session name or unique display name. Inside a managed shell it may be omitted.\n\nAn explicit TARGET is searched across every registered project. When that finds the same name in several projects, the caller's own project — the one SIDECAR_SHELL belongs to — breaks the tie; outside a managed shell the refusal lists the projects, and --project NAME (a slug, path, or a worktree Sidecar created, by path or basename) or --shell NAME picks one. This rule is shared by get, start, prompt, wait, read, and send-keys.\n\nsessionRef reports whether the shell is bound to an exact provider conversation. Its value is shown for your own shell, or with --include-session-ref; otherwise only the kind and whether an official integration reported it are returned, so ordinary output does not carry conversation identifiers into logs.", Flags: getFlags, Args: ArgSpec{Min: 0, Max: 1}, ExitCodes: agentExitCodes(), Examples: []Example{{Command: "sidecar agent get reviewer --json"}}, Agent: AgentDoc{Invocation: "sidecar agent get [TARGET] --json", Summary: "Read one managed agent's provider and lifecycle state"}, Run: runAgentGet}
	startFlags := append([]Flag{}, common...)
	startFlags = append(startFlags, Flag{Name: "--kind", Arg: "KIND", Summary: "Catalog provider kind (required)"}, Flag{Name: "--timeout", Arg: "DURATION", Summary: "Bound the readiness wait (default 30s)"})
	start := &Command{Name: "start", Summary: "Start a provider in an idle managed shell and wait for readiness", Usage: "sidecar agent start [TARGET] --kind KIND [--timeout DURATION] [-- AGENT_ARG ...]", Long: "Refuses commands, editors, copy mode, agents, ambiguous panes, and replacement processes. Provider arguments remain structured until the final shell boundary.", Flags: startFlags, Args: ArgSpec{Min: 0, Max: -1}, ExitCodes: agentExitCodes(), Examples: []Example{{Command: "sidecar agent start reviewer --kind codex --timeout 30s"}}, Agent: AgentDoc{Invocation: "sidecar agent start [TARGET] --kind KIND", Summary: "Start a known provider in a shell and return only when it is ready"}, Mutates: true, Run: runAgentStart}
	promptFlags := append([]Flag{}, common...)
	promptFlags = append(promptFlags,
		Flag{Name: "--wait", Summary: "Submit and wait for the agent to settle under one pinned target", Bool: true},
		Flag{Name: "--until", Arg: "STATUS", Summary: "Repeatable settled state: idle, done, blocked, or working (default idle, done, blocked)"},
		Flag{Name: "--timeout", Arg: "DURATION", Summary: "Required with --wait; there is no implicit timeout"})
	prompt := &Command{
		Name:    "prompt",
		Summary: "Send a prompt to a managed agent, optionally waiting for it to settle",
		Usage:   "sidecar agent prompt [TARGET] TEXT [--wait] [--until STATUS]... [--timeout DURATION] [--json]",
		Long: "With two positional arguments the first is the target and the second is the prompt.\n" +
			"With one, the prompt goes to the shell named by SIDECAR_SHELL — unless that one\n" +
			"argument names a managed target, which is read as a missing prompt rather than as\n" +
			"a prompt that happens to be a target's name. Empty text is a usage error too.\n\n" +
			"Nothing is written to a target that is blocked, unidentified, stale, dead, or\n" +
			"occupied by a replacement process. The text goes through the same ordered,\n" +
			"bracketed-paste-aware path the embedded terminal uses, and the submission key is\n" +
			"sent separately, so a headless prompt delivers exactly what typing it would.\n\n" +
			"A prompt sent from idle or done must produce an observed lifecycle change within " + agentcontrol.PromptStallWindow.String() + "\n" +
			"or the command reports agent_prompt_stalled. A prompt sent to an agent that is\n" +
			"already working makes no claim about which turn is which: completion of the turn\n" +
			"already in flight may satisfy --wait.\n\n" +
			"Every prompt result carries a receipt with submission, wait, and the pinned target.\n" +
			"submission is submitted, not_submitted, or unknown; unknown means a transport/write\n" +
			"failure may have landed and must not be retried automatically. wait is settled,\n" +
			"timeout, cancelled, replaced, stalled, failed, not_started, or not_requested. The\n" +
			"same receipt is included in the error envelope, so a timeout keeps exit 1 and code\n" +
			"timeout while proving the prompt was submitted and only the wait expired. Feature,\n" +
			"host, and target-resolution refusals report not_submitted/not_started before a write;\n" +
			"they may carry only the requested host/session until a physical pane is resolved.",
		Flags:     promptFlags,
		Args:      ArgSpec{Min: 1, Max: 2},
		ExitCodes: agentExitCodes(),
		Examples: []Example{
			{Command: `sidecar agent prompt reviewer "Review the current diff and report only actionable findings." --wait --timeout 2m`},
			{Command: `sidecar agent prompt "Summarise what changed." --json`, Description: "the shell you are running in"},
		},
		Agent:   AgentDoc{Invocation: "sidecar agent prompt [TARGET] TEXT [--wait --timeout DURATION]", Summary: "Send a prompt to a managed agent and optionally wait for it to settle"},
		Mutates: true,
		Run:     runAgentPrompt,
	}

	waitFlags := append([]Flag{}, common...)
	waitFlags = append(waitFlags,
		Flag{Name: "--until", Arg: "STATUS", Summary: "Repeatable settled state: idle, done, blocked, or working (default idle, done, blocked)"},
		Flag{Name: "--timeout", Arg: "DURATION", Summary: "Required; there is no implicit timeout"})
	wait := &Command{
		Name:    "wait",
		Summary: "Wait for a managed agent to reach a settled state",
		Usage:   "sidecar agent wait [TARGET] [--until STATUS]... --timeout DURATION [--json]",
		Long: "Observes the target without writing to it. The target stays pinned to the same\n" +
			"tmux session, pane, pane process, server, and provider for the whole wait: a\n" +
			"replacement occupant is reported as agent_replaced rather than satisfying it.",
		Flags:     waitFlags,
		Args:      ArgSpec{Min: 0, Max: 1},
		ExitCodes: agentExitCodes(),
		Examples: []Example{
			{Command: "sidecar agent wait reviewer --timeout 5m --json"},
			{Command: "sidecar agent wait reviewer --until done --timeout 5m", Description: "blocked no longer settles the wait"},
		},
		Agent: AgentDoc{Invocation: "sidecar agent wait [TARGET] --timeout DURATION", Summary: "Wait for a managed agent to reach idle, done, or blocked"},
		Run:   runAgentWait,
	}

	readFlags := append([]Flag{}, common...)
	readFlags = append(readFlags,
		Flag{Name: "--source", Arg: "SOURCE", Summary: "visible, recent, recent-unwrapped, detection, or transcript (default visible)"},
		Flag{Name: "--lines", Arg: "N", Summary: "Bound the result to the last N lines"},
		Flag{Name: "--ansi", Summary: "Preserve styling where the source has it", Bool: true})
	read := &Command{
		Name:    "read",
		Summary: "Read a managed agent's output without touching it",
		Usage:   "sidecar agent read [TARGET] [--source SOURCE] [--lines N] [--ansi] [--json]",
		Long: "Every source is a passive snapshot. Reads never scroll, resize, or otherwise\n" +
			"manipulate the agent's own screen.\n\n" +
			"  visible           the current screen\n" +
			"  recent            the screen plus recent scrollback\n" +
			"  recent-unwrapped  recent, with soft-wrapped lines joined back together\n" +
			"  detection         the exact slice the lifecycle detector read\n" +
			"  transcript        the provider's own conversation, once an exact session\n" +
			"                    binding exists; otherwise transcript_unavailable. It is\n" +
			"                    never guessed from the newest session in the same directory.",
		Flags:     readFlags,
		Args:      ArgSpec{Min: 0, Max: 1},
		ExitCodes: agentExitCodes(),
		Examples: []Example{
			{Command: "sidecar agent read reviewer --source recent-unwrapped --lines 120"},
			{Command: "sidecar agent read reviewer --source detection --json", Description: "the evidence behind the status"},
		},
		Agent: AgentDoc{Invocation: "sidecar agent read [TARGET] [--source SOURCE] [--lines N]", Summary: "Read a managed agent's terminal output passively"},
		Run:   runAgentRead,
	}

	sendKeys := &Command{
		Name:    "send-keys",
		Summary: "Send validated logical keys to a managed agent's UI",
		Usage:   "sidecar agent send-keys [TARGET] KEY [KEY ...] [--json]",
		Long: "With two or more positional arguments the first is the target and the rest are\n" +
			"keys. With exactly one, the key goes to the shell named by SIDECAR_SHELL.\n\n" +
			"Keys are named, not typed: enter, esc, tab, space, backspace, delete, insert,\n" +
			"the arrows, home, end, pageup, pagedown, f1-f12, ctrl+<letter>, ctrl+space,\n" +
			"alt+<key>, shift+tab, shift+enter, shift+<arrow>, and any single character.\n" +
			"The whole list is validated before any of it is written, so a typo sends\n" +
			"nothing at all.\n\n" +
			"This is for answering an agent's UI, not for typing at it: prompt text belongs\n" +
			"to sidecar agent prompt. When a wait returns blocked the sequence is read the\n" +
			"screen, decide, then send keys. Sidecar never answers an approval for you.",
		Flags:     common,
		Args:      ArgSpec{Min: 1, Max: -1},
		ExitCodes: agentExitCodes(),
		Examples: []Example{
			{Command: "sidecar agent send-keys reviewer down enter"},
			{Command: "sidecar agent send-keys reviewer esc", Description: "dismiss a picker"},
		},
		Agent:   AgentDoc{Invocation: "sidecar agent send-keys [TARGET] KEY [KEY ...]", Summary: "Answer a blocked agent's UI with validated logical keys"},
		Mutates: true,
		Run:     runAgentSendKeys,
	}

	// The lifecycle reporting surface. These are deliberately not gated behind
	// agent_control: that flag governs *driving* an agent, while these only
	// record what a provider says about itself, and a pane whose integration is
	// installed should keep reporting whether or not the operator has opted in
	// to agent control.
	lcReport, lcEnd, lcRelease, lcExplain, lcManifests := lifecycleCommands()

	// Sub is rendered in slice order by both RenderHelp and the generated CLI
	// doc, so it is kept alphabetical and TestCLIDocDrift enforces the result.
	sub := []*Command{lcEnd, lcExplain, get, integrationCommand(), list, lcManifests, prompt, read, lcRelease, lcReport, agentReportSessionCommand(), sendKeys, start, wait}
	return &Command{Name: "agent", Summary: "Inspect, start, and coordinate agents in Sidecar-managed shells", Usage: "sidecar agent <command>", Long: "Provider-aware control over shells Sidecar owns.\n\nThe safe sequence is: create the layout separately with sidecar create shell, start the provider with agent start, prompt and wait, read before you send keys, and never close a target you did not create.\n\nWith --host ID the verb runs on that registered host instead, as one invocation over the existing ssh connection, and the host's own answer is what you get back. A remote verb needs an explicit TARGET, because the omitted-target rule names the shell you are in and that shell is on this machine. Conversation identifiers stay on the host that owns them: remote output reports whether a shell is bound, not what it is bound to, unless you ask with --include-session-ref.\n\nThe report, end, release, and explain commands are a separate surface: they record and inspect the lifecycle events a provider's own integration reports, and they are not gated behind agent_control.", Sub: sub, Run: runAgentRoot}
}

func agentExitCodes() []ExitCode {
	return []ExitCode{{Code: 0, Summary: "success"}, {Code: 1, Summary: "transport, timeout, or internal failure"}, {Code: 2, Summary: "usage error or version skew"}, {Code: 3, Summary: "target is not registered"}, {Code: 5, Summary: "feature disabled or semantic/value refusal"}}
}

func runAgentRoot(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("agent")
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprint(env.Stdout, RenderHelp(cmd))
		return 0
	}
	if sub := cmd.FindSubcommand(args[0]); sub != nil {
		return sub.Run(env, args[1:])
	}
	cliErrf(env.Stderr, "unknown agent command %q\n\n%s", args[0], RenderHelp(cmd))
	return 2
}

type agentFlags struct {
	json           bool
	project, shell string
	// host names a registered remote host. It is accepted by every control
	// verb rather than declared per command, because "run this somewhere else"
	// is orthogonal to what the verb does.
	host           string
	positional     []string
	wait           bool
	ansi           bool
	lines          int
	timeout        time.Duration
	until          []agentcontrol.Status
	source         agentcontrol.ReadSource
	includeSession bool
}

// agentOpt is the option set one agent subcommand accepts. Options are declared
// per command rather than parsed permissively so `--wait` on a read, or
// `--source` on a prompt, is a usage error instead of a silent no-op.
type agentOpt uint8

const (
	optTimeout agentOpt = 1 << iota
	optUntil
	optWait
	optSource
	optLines
	optANSI
	optIncludeSession
)

func (a agentOpt) has(opt agentOpt) bool { return a&opt != 0 }

func parseAgentArgs(env Env, args []string, help string, allowed agentOpt) (agentFlags, int) {
	var f agentFlags
	usage := func(format string, a ...any) int {
		cliErrf(env.Stderr, format+"\n\n%s", append(a, help)...)
		return 2
	}
	value := func(arg, name string, i int) (string, int, bool) {
		v, n, ok := takeFlagArg(arg, args, i, name)
		if !ok || v == "" {
			return "", i, false
		}
		return v, n, true
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, _, _ := strings.Cut(arg, "=")
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return f, 0
		case arg == "--json":
			f.json = true
		case name == "--project":
			v, n, ok := value(arg, "--project", i)
			if !ok {
				return f, usage("--project requires a value")
			}
			f.project, i = v, n
		case name == "--shell":
			v, n, ok := value(arg, "--shell", i)
			if !ok {
				return f, usage("--shell requires a value")
			}
			f.shell, i = v, n
		case name == "--host":
			v, n, ok := value(arg, "--host", i)
			if !ok {
				return f, usage("--host requires a value")
			}
			f.host, i = v, n
		case name == "--wait" && allowed.has(optWait):
			f.wait = true
		case name == "--include-session-ref" && allowed.has(optIncludeSession):
			f.includeSession = true
		case name == "--ansi" && allowed.has(optANSI):
			f.ansi = true
		case name == "--timeout" && allowed.has(optTimeout):
			v, n, ok := value(arg, "--timeout", i)
			if !ok {
				return f, usage("--timeout requires a value")
			}
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return f, usage("invalid --timeout %q", v)
			}
			f.timeout, i = d, n
		case name == "--until" && allowed.has(optUntil):
			v, n, ok := value(arg, "--until", i)
			if !ok {
				return f, usage("--until requires a value")
			}
			status, err := agentcontrol.ParseStatus(v)
			if err != nil {
				return f, usage("%v", err)
			}
			f.until, i = append(f.until, status), n
		case name == "--source" && allowed.has(optSource):
			v, n, ok := value(arg, "--source", i)
			if !ok {
				return f, usage("--source requires a value")
			}
			source, err := agentcontrol.ParseReadSource(v)
			if err != nil {
				return f, usage("%v", err)
			}
			f.source, i = source, n
		case name == "--lines" && allowed.has(optLines):
			v, n, ok := value(arg, "--lines", i)
			if !ok {
				return f, usage("--lines requires a value")
			}
			lines, err := strconv.Atoi(v)
			if err != nil || lines <= 0 {
				return f, usage("invalid --lines %q", v)
			}
			f.lines, i = lines, n
		default:
			if strings.HasPrefix(arg, "-") {
				return f, usage("unknown option %q", arg)
			}
			f.positional = append(f.positional, arg)
		}
	}
	if allowed.has(optSource) && f.source == "" {
		// Documented default: omitting --source reads the current screen.
		f.source = agentcontrol.SourceVisible
	}
	return f, -1
}

// currentShellTarget is the omitted-target rule: inside a managed shell the
// command addresses that shell. Outside one there is nothing to fall back to —
// deliberately not the user's focused TUI row, which would make the same
// command mean different things depending on where somebody's cursor is.
func currentShellTarget() string { return os.Getenv(shellstate.SessionEnv) }

// splitAgentTarget applies the positional rule the agent commands share: the
// leading positional is the target only when the caller supplied more than the
// command's own arguments.
func splitAgentTarget(positional []string, wantArgs int) (target string, rest []string, explicit bool) {
	if len(positional) > wantArgs {
		return positional[0], positional[1:], true
	}
	return currentShellTarget(), positional, false
}

// resolveAgentTarget turns the shared flags into a pinned target.
//
// lookup may be nil. Passing one lets a command that has already scanned for
// managed targets — `agent prompt`, checking whether its lone positional names
// one — reuse that scan instead of walking every project's worktrees again.
func resolveAgentTarget(env Env, lookup *shellTargetLookup, target string, f agentFlags, explicit bool) (agentcontrol.Target, int) {
	resolved, err := resolveAgentTargetError(env, lookup, target, f, explicit)
	if err != nil {
		return agentcontrol.Target{}, emitAgentError(env, f.json, err)
	}
	return resolved, 0
}

func resolveAgentTargetError(env Env, lookup *shellTargetLookup, target string, f agentFlags, explicit bool) (agentcontrol.Target, error) {
	if target == "" {
		return agentcontrol.Target{}, &agentcontrol.Error{Code: agentcontrol.ErrNotFound, Message: "target is required outside a managed shell"}
	}
	if lookup == nil {
		lookup = &shellTargetLookup{}
	}
	tgt, code, err := lookup.find(env, target, f.shell, f.project, explicit && f.shell == "" && f.project == "")
	if err != nil {
		typed := &agentcontrol.Error{Code: agentcontrol.ErrTransport, Message: err.Error(), Err: err}
		if code == shellTargetUnregistered || code == exitInputRejected {
			typed.Code = agentcontrol.ErrNotFound
		}
		return agentcontrol.Target{}, typed
	}
	return targetFromShell(tgt), nil
}

// agentService builds the service and the cleanup its terminal needs. The
// control-mode pool a wait opens has to be released before the process exits,
// and a one-shot read must not pay for one at all.
func agentService(env Env) (agentcontrol.Service, func(), context.Context) {
	terminal := newAgentTerminal()
	release := func() {
		if closer, ok := terminal.(interface{ Close() }); ok {
			closer.Close()
		}
	}
	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return agentcontrol.Service{Terminal: terminal, Transcript: transcriptReader(env)}, release, ctx
}

// transcriptReader binds `agent read --source transcript` to the conversation a
// shell was reported to be running.
//
// The lookup goes through the same shell-target resolver every other agent
// command uses and then through shellstate, so the only way to reach a
// transcript is a binding an official integration actually reported. There is no
// path here that searches for a session by working directory, which is what
// makes the refusal honest rather than merely conservative.
func transcriptReader(env Env) agentcontrol.TranscriptReader {
	return agenttranscript.Reader{
		Lookup: func(target agentcontrol.Target) (agenttranscript.Binding, bool, error) {
			var lookup shellTargetLookup
			tgt, code, err := lookup.resolve(env, target.Session, "", "", false, target.Namespace)
			if err != nil {
				return agenttranscript.Binding{}, false, err
			}
			if code > 0 {
				return agenttranscript.Binding{}, false, nil
			}
			ref, kind, ok, err := shellstate.SessionRefAtPath(tgt.ManifestPath,
				shellstate.Identity{TmuxName: tgt.Session, Namespace: tgt.Namespace})
			if err != nil {
				return agenttranscript.Binding{}, false, err
			}
			return agenttranscript.Binding{Kind: kind, Ref: ref}, ok, nil
		},
	}
}

func runAgentPrompt(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("agent").FindSubcommand("prompt")
	help := RenderHelp(cmd)
	f, code := parseAgentArgs(env, args, help, optWait|optUntil|optTimeout)
	if code >= 0 {
		return code
	}
	if len(f.positional) == 0 || len(f.positional) > 2 {
		cliErrf(env.Stderr, "agent prompt takes [TARGET] TEXT\n\n%s", help)
		return 2
	}
	if f.wait && f.timeout <= 0 {
		cliErrf(env.Stderr, "--wait requires --timeout; there is no implicit timeout\n\n%s", help)
		return 2
	}
	if !f.wait && (f.timeout > 0 || len(f.until) > 0) {
		cliErrf(env.Stderr, "--timeout and --until apply to --wait\n\n%s", help)
		return 2
	}
	// One positional is the prompt text, sent to this shell. But if that one
	// word names a managed target, the caller meant `agent prompt TARGET TEXT`
	// and left the text off — and carrying on would type the target's own name
	// into the caller's own shell, which is both useless and unasked for. Say
	// what is missing instead, and name the way out: someone whose prompt
	// genuinely is one word that collides with a shell name needs to be told
	// how to send it, not merely that they cannot.
	//
	// The scan is memoized because the happy path runs the identical lookup a
	// few lines below through resolveAgentShellTarget, and it walks
	// `git worktree list` per registered project. Paying for that twice on
	// every one-positional prompt is a cost with nothing to show for it.
	var guard shellTargetLookup
	if len(f.positional) == 1 {
		if _, _, err := guard.find(env, f.positional[0], f.shell, f.project, f.shell == "" && f.project == ""); err == nil {
			cliErrf(env.Stderr,
				"agent prompt %s: the prompt text is missing\n\nIf %s really is the prompt, name the target explicitly: sidecar agent prompt %s %q\n\n%s",
				f.positional[0], f.positional[0], f.positional[0], f.positional[0], help)
			return 2
		}
	}
	// Empty text is a usage mistake like every other one here, answered before
	// a target is resolved. The service refuses it too, but that refusal is an
	// operational code — a caller cannot tell "I built the command line wrong"
	// from "the agent would not take it" if both leave by the same exit.
	if strings.TrimSpace(f.positional[len(f.positional)-1]) == "" {
		cliErrf(env.Stderr, "agent prompt TEXT must not be empty\n\n%s", help)
		return 2
	}
	name, rest, explicit := splitAgentTarget(f.positional, 1)
	requestedTarget := agentcontrol.Target{Host: "local", Project: f.project}
	if explicit {
		requestedTarget.Session = name
	}
	if f.host != "" {
		requestedTarget.Host = f.host
	}
	if err := agentControlError(env); err != nil {
		return emitPromptError(env, f.json, agentcontrol.WithPromptReceipt(err, requestedTarget, agentcontrol.SubmissionNotSubmitted, agentcontrol.PromptWaitNotStarted))
	}
	if f.host != "" {
		return runRemoteAgentPrompt(env, f, name, rest[0], explicit)
	}
	target, err := resolveAgentTargetError(env, &guard, name, f, explicit)
	if err != nil {
		return emitPromptError(env, f.json, agentcontrol.WithPromptReceipt(err, requestedTarget, agentcontrol.SubmissionNotSubmitted, agentcontrol.PromptWaitNotStarted))
	}
	svc, release, ctx := agentService(env)
	defer release()
	result, err := svc.Prompt(ctx, agentcontrol.PromptRequest{Target: target, Text: rest[0], Wait: f.wait, Until: f.until, Timeout: f.timeout})
	if err != nil {
		return emitPromptError(env, f.json, err)
	}
	return emitPromptResult(env, f.json, result)
}

func runAgentWait(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("agent").FindSubcommand("wait")
	help := RenderHelp(cmd)
	f, code := parseAgentArgs(env, args, help, optUntil|optTimeout)
	if code >= 0 {
		return code
	}
	if len(f.positional) > 1 {
		cliErrf(env.Stderr, "agent wait accepts at most one target\n\n%s", help)
		return 2
	}
	if f.timeout <= 0 {
		cliErrf(env.Stderr, "agent wait requires --timeout; there is no implicit timeout\n\n%s", help)
		return 2
	}
	if code = requireAgentControl(env, f.json); code >= 0 {
		return code
	}
	name, _, explicit := splitAgentTarget(f.positional, 0)
	if f.host != "" {
		return runRemoteAgentWait(env, f, name, explicit)
	}
	target, code := resolveAgentTarget(env, nil, name, f, explicit)
	if code != 0 {
		return code
	}
	svc, release, ctx := agentService(env)
	defer release()
	agent, err := svc.Wait(ctx, agentcontrol.WaitRequest{Target: target, Until: f.until, Timeout: f.timeout})
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	return emitAgent(env, f.json, agent)
}

func runAgentRead(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("agent").FindSubcommand("read")
	help := RenderHelp(cmd)
	f, code := parseAgentArgs(env, args, help, optSource|optLines|optANSI)
	if code >= 0 {
		return code
	}
	if len(f.positional) > 1 {
		cliErrf(env.Stderr, "agent read accepts at most one target\n\n%s", help)
		return 2
	}
	if code = requireAgentControl(env, f.json); code >= 0 {
		return code
	}
	name, _, explicit := splitAgentTarget(f.positional, 0)
	if f.host != "" {
		return runRemoteAgentRead(env, f, name, explicit)
	}
	target, code := resolveAgentTarget(env, nil, name, f, explicit)
	if code != 0 {
		return code
	}
	svc, release, ctx := agentService(env)
	defer release()
	result, err := svc.Read(ctx, agentcontrol.ReadRequest{Target: target, Source: f.source, Lines: f.lines, ANSI: f.ansi})
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	return emitReadResult(env, f.json, result)
}

func runAgentSendKeys(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("agent").FindSubcommand("send-keys")
	help := RenderHelp(cmd)
	f, code := parseAgentArgs(env, args, help, 0)
	if code >= 0 {
		return code
	}
	if len(f.positional) == 0 {
		cliErrf(env.Stderr, "agent send-keys takes [TARGET] KEY [KEY ...]\n\n%s", help)
		return 2
	}
	name, keys, explicit := splitAgentTarget(f.positional, 1)
	// Validating here, before the feature gate and before any target
	// resolution, keeps a mistyped key a usage error rather than a refusal that
	// looks like the agent's fault.
	if err := agentcontrol.ValidateKeys(keys); err != nil {
		cliErrf(env.Stderr, "%v\n\n%s", err, help)
		return 2
	}
	if code = requireAgentControl(env, f.json); code >= 0 {
		return code
	}
	if f.host != "" {
		return runRemoteAgentSendKeys(env, f, name, explicit, keys)
	}
	target, code := resolveAgentTarget(env, nil, name, f, explicit)
	if code != 0 {
		return code
	}
	svc, release, ctx := agentService(env)
	defer release()
	agent, err := svc.SendKeys(ctx, agentcontrol.KeysRequest{Target: target, Keys: keys})
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	return emitAgent(env, f.json, agent)
}

// agentControlEnabled answers whether this run may drive the provider-aware
// agent commands, without deciding what to do about the answer.
//
// Separate from requireAgentControl because the two callers want different
// things from the same fact: `sidecar agent …` is the feature and refuses
// without it, while `create shell --agent` has work to do either way — it
// records the agent family whether or not it can also start it.
func agentControlEnabled(env Env) (bool, error) {
	if enabled, ok := env.FeatureOverrides[features.AgentControl.Name]; ok {
		return enabled, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return false, err
	}
	return cfg.Features.Flags[features.AgentControl.Name], nil
}

func requireAgentControl(env Env, jsonOutput bool) int {
	if err := agentControlError(env); err != nil {
		return emitAgentError(env, jsonOutput, err)
	}
	return -1
}

func agentControlError(env Env) error {
	if enabled, ok := env.FeatureOverrides[features.AgentControl.Name]; ok {
		if enabled {
			return nil
		}
		return &agentcontrol.Error{Code: agentcontrol.ErrFeatureDisabled, Message: "agent control is disabled"}
	}
	enabled, err := agentControlEnabled(env)
	if err != nil {
		return err
	}
	if !enabled {
		return &agentcontrol.Error{Code: agentcontrol.ErrFeatureDisabled, Message: "agent control is disabled; enable the agent_control feature"}
	}
	return nil
}

func runAgentList(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("agent").FindSubcommand("list")
	help := RenderHelp(cmd)
	f, code := parseAgentArgs(env, args, help, optIncludeSession)
	if code >= 0 {
		return code
	}
	if len(f.positional) > 0 {
		cliErrf(env.Stderr, "agent list takes no positional arguments\n\n%s", help)
		return 2
	}
	if code = requireAgentControl(env, f.json); code >= 0 {
		return code
	}
	if f.host != "" {
		return runRemoteAgentList(env, f)
	}
	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Unscoped, a list is global: every registered project, with each
	// worktree root attributed to exactly one of them (see
	// managedTargetCandidates), so a live pane is one row however many state
	// directories can see its checkout.
	projects, code, err := scanProjects(env, f.shell, f.project, true)
	if err != nil {
		if code == 1 {
			return emitAgentError(env, f.json, err)
		}
		return emitAgentError(env, f.json, &agentcontrol.Error{Code: agentcontrol.ErrNotFound, Message: err.Error(), Err: err})
	}
	targets, err := managedTargetCandidates(env, projects)
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	svc := agentcontrol.Service{Terminal: newAgentTerminal()}
	agents := make([]agentcontrol.Agent, 0, len(targets))
	refs := newSessionRefCache()
	for _, target := range targets {
		a, e := svc.Get(ctx, targetFromManaged(target))
		if e == nil && a.Agent.Kind != "" {
			// A list never reveals a conversation value unless the caller
			// asked for it by name; presence and capability are what a
			// discovery command owes.
			refs.decorate(&a, target.ManifestPath, target.Session, target.Namespace, f.includeSession)
			agents = append(agents, a)
		}
	}
	return emitAgentList(env, f.json, agents)
}

// emitAgentList renders a list of agents. Extracted so the local and remote
// paths cannot render the same answer two ways: a parity suite that compared
// two renderers would be testing the test.
func emitAgentList(env Env, jsonOutput bool, agents []agentcontrol.Agent) int {
	if jsonOutput {
		return writeAgentJSON(env, map[string]any{"agents": agents})
	}
	if len(agents) == 0 {
		_, _ = fmt.Fprintln(env.Stdout, "No live managed agents.")
		return 0
	}
	for _, a := range agents {
		_, _ = fmt.Fprintf(env.Stdout, "%-20s %-10s %s\n", a.Target.Name, a.Agent.Kind, a.Agent.Status)
	}
	return 0
}

// emitReadResult renders a passive read or an exact-transcript read.
func emitReadResult(env Env, jsonOutput bool, result agentcontrol.ReadResult) int {
	if jsonOutput {
		return writeAgentJSON(env, result)
	}
	for _, message := range result.Messages {
		_, _ = fmt.Fprintf(env.Stdout, "%s: %s\n", message.Role, message.Text)
	}
	if result.Text != "" {
		_, _ = fmt.Fprint(env.Stdout, result.Text)
		if !strings.HasSuffix(result.Text, "\n") {
			_, _ = fmt.Fprintln(env.Stdout)
		}
	}
	return 0
}

func runAgentGet(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("agent").FindSubcommand("get")
	help := RenderHelp(cmd)
	f, code := parseAgentArgs(env, args, help, optIncludeSession)
	if code >= 0 {
		return code
	}
	if len(f.positional) > 1 {
		cliErrf(env.Stderr, "agent get accepts at most one target\n\n%s", help)
		return 2
	}
	if code = requireAgentControl(env, f.json); code >= 0 {
		return code
	}
	if f.host != "" {
		explicit := len(f.positional) == 1
		name := ""
		if explicit {
			name = f.positional[0]
		}
		return runRemoteAgentGet(env, f, name, explicit)
	}
	target := ""
	if len(f.positional) == 1 {
		target = f.positional[0]
	} else {
		target = os.Getenv(shellstate.SessionEnv)
	}
	if target == "" {
		return emitAgentError(env, f.json, &agentcontrol.Error{Code: agentcontrol.ErrNotFound, Message: "target is required outside a managed shell"})
	}
	tgt, code := resolveAgentShellTarget(env, nil, target, f.shell, f.project, len(f.positional) == 1 && f.shell == "" && f.project == "", f.json)
	if code != 0 {
		return code
	}
	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	a, err := (agentcontrol.Service{Terminal: newAgentTerminal()}).Get(ctx, targetFromShell(tgt))
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	// A shell asking about itself already knows its own conversation, so the
	// value is not withheld from it; any other target needs the explicit flag.
	own := tgt.Session != "" && tgt.Session == os.Getenv(shellstate.SessionEnv)
	decorateSessionRef(&a, tgt.ManifestPath, tgt.Session, tgt.Namespace, f.includeSession || own)
	return emitAgent(env, f.json, a)
}

func runAgentStart(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("agent").FindSubcommand("start")
	help := RenderHelp(cmd)
	f := agentFlags{}
	kind := ""
	timeout := 30 * time.Second
	var extra []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			extra = append(extra, args[i+1:]...)
			break
		}
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return 0
		case arg == "--json":
			f.json = true
		case arg == "--kind" || strings.HasPrefix(arg, "--kind="):
			v, n, ok := takeFlagArg(arg, args, i, "--kind")
			if !ok || v == "" {
				cliErrf(env.Stderr, "--kind requires a value\n\n%s", help)
				return 2
			}
			kind = v
			i = n
		case arg == "--timeout" || strings.HasPrefix(arg, "--timeout="):
			v, n, ok := takeFlagArg(arg, args, i, "--timeout")
			if !ok {
				return 2
			}
			d, e := time.ParseDuration(v)
			if e != nil || d <= 0 {
				cliErrf(env.Stderr, "invalid --timeout %q\n\n%s", v, help)
				return 2
			}
			timeout = d
			i = n
		case arg == "--project" || strings.HasPrefix(arg, "--project="):
			v, n, ok := takeFlagArg(arg, args, i, "--project")
			if !ok {
				return 2
			}
			f.project = v
			i = n
		case arg == "--shell" || strings.HasPrefix(arg, "--shell="):
			v, n, ok := takeFlagArg(arg, args, i, "--shell")
			if !ok {
				return 2
			}
			f.shell = v
			i = n
		case arg == "--host" || strings.HasPrefix(arg, "--host="):
			v, n, ok := takeFlagArg(arg, args, i, "--host")
			if !ok || v == "" {
				cliErrf(env.Stderr, "--host requires a value\n\n%s", help)
				return 2
			}
			f.host = v
			i = n
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
				return 2
			}
			f.positional = append(f.positional, arg)
		}
	}
	if len(f.positional) > 1 || kind == "" {
		cliErrf(env.Stderr, "agent start requires --kind and at most one target\n\n%s", help)
		return 2
	}
	if code := requireAgentControl(env, f.json); code >= 0 {
		return code
	}
	if f.host != "" {
		// The provider argv is built on the host, not here: the catalog that
		// knows how to launch a provider is the one on the machine the
		// provider will run on, and a viewer building argv for a host running a
		// different Sidecar version would be guessing. The extra arguments
		// cross as separate argv entries, which is what keeps them out of any
		// shell string.
		f.timeout = timeout
		explicit := len(f.positional) == 1
		name := ""
		if explicit {
			name = f.positional[0]
		}
		return runRemoteAgentStart(env, f, name, kind, explicit, extra)
	}
	target := ""
	if len(f.positional) == 1 {
		target = f.positional[0]
	} else {
		target = os.Getenv(shellstate.SessionEnv)
	}
	if target == "" {
		return emitAgentError(env, f.json, &agentcontrol.Error{Code: agentcontrol.ErrNotFound, Message: "target is required outside a managed shell"})
	}
	argv, err := agentcatalog.BuildLaunch(kind, extra, false)
	if err != nil {
		return emitAgentError(env, f.json, &agentcontrol.Error{Code: agentcontrol.ErrNotReady, Message: err.Error(), Err: err})
	}
	tgt, code := resolveAgentShellTarget(env, nil, target, f.shell, f.project, len(f.positional) == 1 && f.shell == "" && f.project == "", f.json)
	if code != 0 {
		return code
	}
	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	a, err := (agentcontrol.Service{Terminal: newAgentTerminal()}).Start(ctx, agentcontrol.StartRequest{Target: targetFromShell(tgt), Kind: kind, Argv: argv, Timeout: timeout})
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	recordStartedAgentKind(tgt, kind)
	return emitAgent(env, f.json, a)
}

func resolveAgentShellTarget(env Env, lookup *shellTargetLookup, target, shellFlag, projectFlag string, globalExplicit, jsonOutput bool) (shellTarget, int) {
	if lookup == nil {
		lookup = &shellTargetLookup{}
	}
	tgt, code, err := lookup.find(env, target, shellFlag, projectFlag, globalExplicit)
	if err == nil {
		return tgt, 0
	}
	typed := &agentcontrol.Error{Code: agentcontrol.ErrTransport, Message: err.Error(), Err: err}
	if code == shellTargetUnregistered || code == exitInputRejected {
		typed.Code = agentcontrol.ErrNotFound
	}
	return shellTarget{}, emitAgentError(env, jsonOutput, typed)
}

func targetFromManaged(t managedtarget.Target) agentcontrol.Target {
	return agentcontrol.Target{Host: t.Host, Project: t.Project, Session: t.Session, Name: t.Name, Namespace: t.Namespace}
}
func targetFromShell(t shellTarget) agentcontrol.Target {
	return agentcontrol.Target{Host: "local", Project: t.Project.Key, Session: t.Session, Name: t.DisplayName, Namespace: t.Namespace}
}

// startCreatedAgent starts the provider for a shell or worktree session that
// `create` just made, and returns when it is ready. extra are provider
// arguments the caller wrote after `--`; they follow the family's command the
// way `agent start -- ARGS` appends them.
func startCreatedAgent(ctx context.Context, proj registeredProject, session, display, workDir, kind string, skipPerms bool, extra []string) (agentcontrol.Agent, error) {
	cfg := loadCreateConfig()
	argv, _, err := workspaceops.ResolveAgentLaunchArgv(workDir, kind, cfg.Plugins.Workspace.AgentStart, skipPerms, extra)
	if err != nil {
		return agentcontrol.Agent{}, &agentcontrol.Error{Code: agentcontrol.ErrNotReady, Message: err.Error(), Err: err}
	}
	target := agentcontrol.Target{Host: "local", Project: proj.Key, Session: session, Name: display}
	readyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	svc := agentcontrol.Service{Terminal: newAgentTerminal()}
	ready, err := svc.WaitShellReady(readyCtx, target, 30*time.Second)
	if err != nil {
		return agentcontrol.Agent{}, err
	}
	return svc.Start(readyCtx, agentcontrol.StartRequest{Target: ready.Target, Kind: kind, Argv: argv, Timeout: 30 * time.Second})
}
func emitAgent(env Env, jsonOutput bool, a agentcontrol.Agent) int {
	if jsonOutput {
		return writeAgentJSON(env, a)
	}
	_, e := fmt.Fprintf(env.Stdout, "%s  %s  %s\n", a.Target.Name, a.Agent.Kind, a.Agent.Status)
	if e != nil {
		return 1
	}
	return 0
}

func emitPromptResult(env Env, jsonOutput bool, result agentcontrol.PromptResult) int {
	if jsonOutput {
		return writeAgentJSON(env, result)
	}
	name := result.Target.Name
	if name == "" {
		name = result.Target.Session
	}
	if result.Receipt.Wait == agentcontrol.PromptWaitSettled {
		_, _ = fmt.Fprintf(env.Stdout, "Prompt submitted to %s; agent settled as %s.\n", name, result.Agent.Status)
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "Prompt submitted to %s.\n", name)
	}
	return 0
}

func emitPromptError(env Env, jsonOutput bool, err error) int {
	if jsonOutput {
		return emitAgentError(env, true, err)
	}
	var typed *agentcontrol.Error
	if !agentcontrol.AsError(err, &typed) || typed.Receipt == nil {
		return emitAgentError(env, false, err)
	}
	name := typed.Receipt.Target.Name
	if name == "" {
		name = typed.Receipt.Target.Session
	}
	destination := ""
	if name != "" {
		destination = " to " + name
	}
	switch typed.Receipt.Submission {
	case agentcontrol.SubmissionSubmitted:
		if typed.Receipt.Wait == agentcontrol.PromptWaitTimeout || typed.Receipt.Wait == agentcontrol.PromptWaitCancelled || typed.Receipt.Wait == agentcontrol.PromptWaitFailed {
			cliErrf(env.Stderr, "Prompt submitted%s, but the agent did not settle: %s\n", destination, typed.Error())
		} else {
			cliErrf(env.Stderr, "Prompt submitted%s: %s\n", destination, typed.Error())
		}
	case agentcontrol.SubmissionNotSubmitted:
		cliErrf(env.Stderr, "Prompt was not submitted%s: %s\n", destination, typed.Error())
	default:
		if name != "" {
			destination = " for " + name
		}
		cliErrf(env.Stderr, "Prompt submission outcome is unknown%s: %s\n", destination, typed.Error())
	}
	return agentErrorExitCode(typed.Code)
}
func writeAgentJSON(env Env, v any) int {
	if err := json.NewEncoder(env.Stdout).Encode(v); err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}
	return 0
}
func emitAgentError(env Env, jsonOutput bool, err error) int {
	var typed *agentcontrol.Error
	if !agentcontrol.AsError(err, &typed) {
		typed = &agentcontrol.Error{Code: agentcontrol.ErrTransport, Message: err.Error(), Err: err}
	}
	if jsonOutput {
		_, _ = env.Stderr.Write(append(agentcontrol.MarshalError(typed), '\n'))
	} else {
		cliErrln(env.Stderr, typed.Error())
	}
	return agentErrorExitCode(typed.Code)
}

func agentErrorExitCode(code agentcontrol.ErrorCode) int {
	switch code {
	case agentcontrol.ErrNotFound:
		return 3
	case agentcontrol.ErrTransport, agentcontrol.ErrTimeout, agentcontrol.ErrHostUnavailable:
		// host_unavailable joins the retryable failures: nothing was refused,
		// the machine could not be reached, and trying again later is the fix.
		return 1
	case agentcontrol.ErrVersionSkew, agentcontrol.ErrUsage:
		// Exit 2 is what agentExitCodes has always documented as "usage error
		// or version skew", and it is what the host itself exited with. A
		// caller relaying a remote verb keeps the same status it would have
		// seen running it there.
		return 2
	default:
		return 5
	}
}

// decorateSessionRef adds the session-binding projection to an agent result.
//
// The projection is deliberately asymmetric. Kind and Reported say whether this
// shell can be resumed and whether an official integration vouched for it, which
// is what a caller deciding what to do needs. The value names the conversation
// and is withheld unless the caller is that shell or asked for it explicitly:
// `agent list` output is routinely captured into logs and CI artifacts, and a
// conversation identifier written there cannot be unwritten.
//
// A shell with no binding gets no sessionRef key at all rather than an empty
// one, so "not bound" and "bound but redacted" stay distinguishable.
func decorateSessionRef(a *agentcontrol.Agent, manifestPath, session, namespace string, includeValue bool) {
	newSessionRefCache().decorate(a, manifestPath, session, namespace, includeValue)
}

// sessionRefCache reads each shell manifest at most once.
//
// SessionRefAtPath parses a whole manifest to answer about one shell, and
// `agent list` asks about every shell in every registered project -- so without
// this the command re-parses the same file once per row it prints. It follows
// the same reasoning as shellTargetLookup's memo: the scan is per-manifest, the
// questions are per-shell, and a loop is the wrong place to pay for a file.
type sessionRefCache struct {
	byManifest map[string][]shellstate.Definition
}

func newSessionRefCache() *sessionRefCache {
	return &sessionRefCache{byManifest: map[string][]shellstate.Definition{}}
}

func (c *sessionRefCache) definitions(manifestPath string) []shellstate.Definition {
	if defs, ok := c.byManifest[manifestPath]; ok {
		return defs
	}
	defs, err := shellstate.ListAtPath(manifestPath)
	if err != nil {
		// A manifest that cannot be read means no binding is known, which is
		// the same answer as no binding recorded. Caching the empty result
		// keeps a broken file from being re-read once per row.
		defs = nil
	}
	c.byManifest[manifestPath] = defs
	return defs
}

func (c *sessionRefCache) decorate(a *agentcontrol.Agent, manifestPath, session, namespace string, includeValue bool) {
	if a == nil || manifestPath == "" || session == "" {
		return
	}
	var (
		ref agentsession.Ref
		ok  bool
	)
	for _, def := range c.definitions(manifestPath) {
		if def.TmuxName != session {
			continue
		}
		if def.Namespace != "" && namespace != "" && def.Namespace != namespace {
			continue
		}
		if def.Agent != nil && def.Agent.Session != nil && !def.Agent.Session.Empty() {
			ref, ok = *def.Agent.Session, true
		}
		break
	}
	if !ok || ref.Empty() {
		return
	}
	projected := &agentcontrol.SessionRef{Kind: string(ref.Kind), Reported: ref.Reported}
	if includeValue {
		projected.Value = ref.Value
	}
	a.Agent.SessionRef = projected
}
