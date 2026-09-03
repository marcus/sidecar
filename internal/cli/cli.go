// Package cli provides Sidecar's non-interactive command dispatch boundary.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/shellstate"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspaceops"
)

// exitInputRejected is the exit code for "the command parsed, and a value
// inside it was refused": a display name already in use, a branch that already
// exists, a base ref this machine does not have.
//
// Additive rather than a redefinition. Exit 2 keeps meaning exactly what it
// meant — this command is not usable as written, an unknown flag, a missing
// required one — which for a caller on another machine is a statement about
// version skew (internal/hosts.FailUnsupported). Collapsing the two is how a
// user whose rename collided with an existing name came to be told to update
// Sidecar on one of their machines.
const exitInputRejected = 5

// Run dispatches a non-interactive command. handled=false leaves legacy TUI
// flag parsing entirely untouched.
func Run(args []string, stdout, stderr io.Writer) (handled bool, exitCode int) {
	if len(args) == 0 {
		return false, 0
	}

	// `sidecar -config <path> notify post ...` is how an isolated proof run
	// reaches a subcommand, and how the flag is spelled everywhere else. Global
	// flags written before the command used to make args[0] unmatchable, so
	// dispatch fell through to TUI startup and died with "Sidecar requires an
	// interactive terminal". Strip them here and apply the one that changes
	// where a command reads and writes.
	featureOverrides := leadingFeatureOverrides(args)
	args, configPath, ok := stripGlobalFlags(args)
	if !ok || len(args) == 0 {
		return false, 0
	}
	if !namesCommand(args[0]) {
		return false, 0
	}
	if configPath != "" {
		// Before defaultEnv, and before any command resolves a path: -config
		// moves state.json and the config directory together (td-8d18de).
		config.SetConfigPath(configPath)
	}

	env := defaultEnv(stdout, stderr)
	env.FeatureOverrides = featureOverrides

	if args[0] == "-h" || args[0] == "--help" {
		return true, runHelpCommand(env, args[1:])
	}
	if args[0] == "help" {
		return true, runHelpCommand(env, args[1:])
	}
	// The flag aliases match how an agent may probe an unfamiliar binary;
	// "sidecar agents" answers the same way for the same reason.
	if isAgentsCommand(args[0]) {
		_, _ = fmt.Fprint(env.Stdout, RenderAgents(RootCommand()))
		return true, 0
	}

	root := RootCommand()
	cmd := root.FindSubcommand(args[0])
	if cmd == nil {
		return false, 0
	}

	// A launch command records what the app should do and reports handled=false
	// so ordinary startup carries on in this same process.
	if cmd.Launch != nil {
		return cmd.Launch(env, args[1:])
	}

	// Fail closed before the handler, not on its first write. cmd.Run for a
	// mutating verb reaches tmux and git before anything asserts a path, so a
	// proof run that forgot to move the state tree used to leave a real session
	// or a real branch behind and only then refuse. Non-mutating verbs are
	// untouched: reading is what a misconfigured proof is allowed to do.
	if mutatesState(cmd, args[1:]) {
		if err := config.CheckStateIsolation(); err != nil {
			cliErrf(env.Stderr, "sidecar %s refuses to run: %v\n", cmd.Name, err)
			return true, 1
		}
	}

	if cmd.Run != nil {
		return true, cmd.Run(env, args[1:])
	}

	return true, 0
}

func leadingFeatureOverrides(args []string) map[string]bool {
	result := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			break
		}
		name, value, hasValue := arg, "", false
		if at := strings.IndexByte(arg, '='); at > 0 {
			name, value, hasValue = arg[:at], arg[at+1:], true
		}
		canonical := "-" + strings.TrimLeft(name, "-")
		switch canonical {
		case "-enable-feature", "-disable-feature":
			if !hasValue {
				if i+1 >= len(args) {
					return result
				}
				i++
				value = args[i]
			}
			for _, feature := range strings.Split(value, ",") {
				feature = strings.TrimSpace(feature)
				if feature != "" {
					result[feature] = canonical == "-enable-feature"
				}
			}
		case "-config", "-project":
			if !hasValue {
				i++
			}
		case "-debug":
		default:
			return result
		}
	}
	return result
}

// mutatesState resolves the deepest subcommand these arguments name and reports
// whether it changes state outside this process.
//
// It walks the tree rather than reading the top-level command's marker so that
// `sidecar shell list` is not gated by `sidecar shell send`'s.
func mutatesState(cmd *Command, args []string) bool {
	rest := args
	for i, arg := range args {
		rest = args[i:]
		if arg == "--" || strings.HasPrefix(arg, "-") {
			break
		}
		sub := cmd.FindSubcommand(arg)
		if sub == nil {
			break
		}
		cmd = sub
		rest = args[i+1:]
	}
	if !cmd.Mutates {
		return false
	}
	return !asksForHelp(cmd, rest)
}

// asksForHelp reports whether these arguments — everything after the resolved
// subcommand path — request usage rather than the verb's work.
//
// Printing usage is the one thing a misconfigured run should still be able to
// do, so help is exempt from the isolation gate. But this decides whether a
// safety gate arms, and the first version of it simply scanned every argument
// for -h/--help/help. That let a flag VALUE disarm it: `shell send --run help`
// and `shell send --run --help` are ordinary things to send into a shell, and
// both sailed past the gate and reached tmux. So this reads the arguments the
// way the verb's own parser does instead of grepping them:
//
//   - only the flag spellings count. The bare word "help" is a subcommand of
//     root and of nothing that mutates, so treating it as a request here could
//     only ever misread a worktree named "help".
//   - a token following a value-taking flag is that flag's value, skipped.
//   - nothing after `--` is a flag at all.
//
// Anything it cannot read as a request for usage is treated as the verb running
// for real, which is the direction that fails closed.
func asksForHelp(cmd *Command, args []string) bool {
	takesValue := make(map[string]bool, len(cmd.Flags))
	for _, flag := range cmd.Flags {
		if flag.Bool {
			continue
		}
		if flag.Name != "" {
			takesValue[flag.Name] = true
		}
		if flag.Short != "" {
			takesValue[flag.Short] = true
		}
	}
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--":
			return false
		case arg == "-h" || arg == "--help":
			return true
		case takesValue[arg]:
			// Its value is next, and a value is never a flag. `--name=X` needs
			// no skip: the whole token is one argument.
			i++
		}
	}
	return false
}

// globalValueFlags are the process-level flags that take a value. They are
// declared in cmd/sidecar as ordinary flag.Flag values; this list is only how
// dispatch knows to step over the value as well as the name.
var globalValueFlags = map[string]bool{
	"-config": true, "-project": true,
	"-enable-feature": true, "-disable-feature": true,
}

// globalBoolFlags are the process-level flags that take no value.
var globalBoolFlags = map[string]bool{"-debug": true}

// stripGlobalFlags removes leading global flags from args and reports the
// -config value among them. ok is false for anything it does not recognise —
// an unknown flag before the command is left for flag.Parse to complain about,
// exactly as it did before, rather than being silently dropped.
func stripGlobalFlags(args []string) (rest []string, configPath string, ok bool) {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		arg := args[0]
		if arg == "-h" || arg == "--help" {
			return args, configPath, true
		}
		if isAgentsCommand(arg) {
			return args, configPath, true
		}
		name, value, hasValue := arg, "", false
		if i := strings.IndexByte(arg, '='); i > 0 {
			name, value, hasValue = arg[:i], arg[i+1:], true
		}
		// Go's flag package accepts one dash or two for the same flag.
		canonical := "-" + strings.TrimLeft(name, "-")
		switch {
		case globalValueFlags[canonical]:
			if !hasValue {
				if len(args) < 2 {
					return args, configPath, false
				}
				value = args[1]
				args = args[1:]
			}
			if canonical == "-config" {
				configPath = value
			}
			args = args[1:]
		case globalBoolFlags[canonical]:
			args = args[1:]
		default:
			return args, configPath, false
		}
	}
	return args, configPath, true
}

// namesCommand reports whether tok is something Run answers. Anything else
// belongs to ordinary TUI startup.
func namesCommand(tok string) bool {
	switch tok {
	case "-h", "--help", "help":
		return true
	}
	if isAgentsCommand(tok) {
		return true
	}
	return RootCommand().FindSubcommand(tok) != nil
}

func isAgentsCommand(tok string) bool {
	switch tok {
	case "agents", "--agents", "-a":
		return true
	default:
		return false
	}
}

func runShellName(env Env, args []string) int {
	nameCmd := RootCommand().FindSubcommand("shell").FindSubcommand("name")
	nameHelp := RenderHelp(nameCmd)
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "-h", "--help":
			if _, err := fmt.Fprint(env.Stdout, nameHelp); err != nil {
				return 1
			}
			return 0
		case "--json":
			jsonOutput = true
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, nameHelp)
				return 2
			}
			cliErrf(env.Stderr, "shell name takes no positional arguments\n\n%s", nameHelp)
			return 2
		}
	}
	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	identity, err := currentShellIdentity(ctx)
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}
	result, err := lookupCurrentShellName(ctx, env.StateDir, identity)
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}
	if jsonOutput {
		if err := json.NewEncoder(env.Stdout).Encode(result); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		return 0
	}
	if _, err := fmt.Fprintln(env.Stdout, result.Name); err != nil {
		return 1
	}
	return 0
}

// runShellRename picks between the two forms of the verb. Without --target the
// call is handed to runShellRenameCurrent: that path resolves "which shell am
// I" from the local tmux environment and is what agents run in every session,
// so its messages and its exit codes do not move. The one thing that does is
// `--`, which only ever turned a refusal into a working rename, and which the
// --target form already accepted.
func runShellRename(env Env, args []string) int {
	if namesShellRenameFlag(args, "--target") {
		return runShellRenameTarget(env, args)
	}
	// --project and --shell scope a --target; alone they have nothing to scope,
	// and the current-shell path would only report them as unknown options.
	for _, flag := range []string{"--project", "--shell"} {
		if namesShellRenameFlag(args, flag) {
			cliErrf(env.Stderr, "%s only applies with --target\n\n%s", flag, RenderHelp(RootCommand().FindSubcommand("shell").FindSubcommand("rename")))
			return 2
		}
	}
	return runShellRenameCurrent(env, args)
}

// namesShellRenameFlag reports whether args pass this flag, stopping at `--`.
//
// The terminator matters because this decides which of the two forms of the
// verb runs: without it, `shell rename -- --target` — renaming the current
// shell to the literal string "--target" — would be routed to the form that
// takes one, and refused for not having supplied it. Same shape of mistake as
// letting a flag value disarm the isolation gate, one decision further out.
func namesShellRenameFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func runShellRenameCurrent(env Env, args []string) int {
	renameCmd := RootCommand().FindSubcommand("shell").FindSubcommand("rename")
	renameHelp := RenderHelp(renameCmd)
	jsonOutput := false
	var positional []string
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "-h", "--help":
			if _, err := fmt.Fprint(env.Stdout, renameHelp); err != nil {
				return 1
			}
			return 0
		case "--json":
			jsonOutput = true
		case "--":
			// Everything after `--` is a value, not a flag. NormalizeName
			// accepts a leading dash, so "-wip" is a legal display name — and
			// this is the form every agent runs in its own shell, so refusing
			// the spelling its --target sibling accepts made one verb disagree
			// with itself about what a name is.
			positional = append(positional, args[i+1:]...)
			i = len(args)
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, renameHelp)
				return 2
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		cliErrf(env.Stderr, "shell rename requires exactly one quoted display name\n\n%s", renameHelp)
		return 2
	}
	name, err := shellstate.NormalizeName(positional[0])
	if err != nil {
		cliErrln(env.Stderr, err)
		return 2
	}
	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	identity, err := currentShellIdentity(ctx)
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}
	result, err := renameCurrentShell(ctx, env.StateDir, identity, name)
	if err != nil {
		cliErrln(env.Stderr, err)
		if shellstate.IsValidation(err) {
			return 2
		}
		return 1
	}
	// Refresh the environment cue for anything started in this shell later.
	// The calling process keeps the value it inherited; the manifest, not the
	// environment, is the authority.
	_ = tty.SetSessionEnv(identity.session, shellstate.NameEnv, result.Name)
	if jsonOutput {
		if err := json.NewEncoder(env.Stdout).Encode(result); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
	} else if result.Changed {
		if _, err := fmt.Fprintf(env.Stdout, "Renamed current Sidecar shell %q to %q.\n", result.OldName, result.Name); err != nil {
			return 1
		}
	} else {
		if _, err := fmt.Fprintf(env.Stdout, "Current Sidecar shell is already named %q.\n", result.Name); err != nil {
			return 1
		}
	}
	return 0
}

// cliErrf / cliErrln write usage and failure messages best-effort: the caller
// already has a more specific exit code, and a broken stderr should not replace it.
func cliErrf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func cliErrln(w io.Writer, a ...any) {
	_, _ = fmt.Fprintln(w, a...)
}

type shellIdentity struct{ session, socket, path string }

func currentShellIdentity(ctx context.Context) (shellIdentity, error) {
	if os.Getenv("TMUX") == "" {
		return shellIdentity{}, fmt.Errorf("not inside tmux; run this command from a Sidecar project shell")
	}
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return shellIdentity{}, fmt.Errorf("tmux did not identify the calling pane; run this command directly from a Sidecar project shell")
	}
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", pane, "#{session_name}\t#{socket_path}\t#{pane_current_path}").Output()
	if err != nil {
		return shellIdentity{}, fmt.Errorf("identify current tmux shell: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] == "" {
		return shellIdentity{}, fmt.Errorf("tmux returned an incomplete current-shell identity")
	}
	if !strings.HasPrefix(parts[0], "sidecar-sh-") && !strings.HasPrefix(parts[0], "sidecar-ws-") {
		return shellIdentity{}, fmt.Errorf("current tmux session is not a Sidecar project shell")
	}
	socket := filepath.Clean(parts[1])
	if resolved, resolveErr := filepath.EvalSymlinks(socket); resolveErr == nil {
		socket = filepath.Clean(resolved)
	}
	path := ""
	if len(parts) == 3 {
		path = filepath.Clean(parts[2])
	}
	return shellIdentity{session: parts[0], socket: socket, path: path}, nil
}

func lookupCurrentShellName(ctx context.Context, stateDir string, identity shellIdentity) (shellstate.LookupResult, error) {
	if strings.HasPrefix(identity.session, "sidecar-sh-") {
		return shellstate.LookupCurrent(stateDir, shellstate.Identity{TmuxName: identity.session, Namespace: identity.socket})
	}
	projectRoot, worktreeRoot, err := currentManagedWorktree(ctx, stateDir, identity)
	if err != nil {
		return shellstate.LookupResult{}, err
	}
	name, err := workspaceops.LookupWorktreeDisplayName(stateDir, projectRoot, worktreeRoot)
	if err != nil {
		return shellstate.LookupResult{}, err
	}
	return shellstate.LookupResult{Shell: identity.session, Name: name}, nil
}

func renameCurrentShell(ctx context.Context, stateDir string, identity shellIdentity, name string) (shellstate.RenameResult, error) {
	if strings.HasPrefix(identity.session, "sidecar-sh-") {
		result, err := shellstate.RenameCurrent(stateDir, shellstate.RenameRequest{TmuxName: identity.session, Namespace: identity.socket, Name: name})
		if err != nil {
			return shellstate.RenameResult{}, err
		}
		if result.Changed {
			origin, _ := shellstate.LookupOrigin(stateDir, shellstate.Identity{TmuxName: identity.session, Namespace: identity.socket})
			_, _ = uirequest.WriteRequest(stateDir, uirequest.Request{
				Action: uirequest.ActionRenameShell,
				Origin: uirequest.Origin{
					TmuxSession: identity.session,
					Namespace:   identity.socket,
					ProjectKey:  origin.ProjectKey,
					WorkDir:     origin.WorkDir,
					PID:         os.Getpid(),
				},
				Target: uirequest.Target{Kind: uirequest.TargetKindShell, Value: result.Name},
			})
		}
		return result, nil
	}
	projectRoot, worktreeRoot, err := currentManagedWorktree(ctx, stateDir, identity)
	if err != nil {
		return shellstate.RenameResult{}, err
	}
	result, err := workspaceops.RenameWorktreeDisplayName(ctx, stateDir, projectRoot, worktreeRoot, name)
	if err != nil {
		return shellstate.RenameResult{}, err
	}
	if result.Changed {
		// Persistence is authoritative. The request is a best-effort repaint cue
		// for already-running Sidecar instances; a restart reads the same value.
		_, _ = uirequest.WriteRequest(stateDir, uirequest.Request{
			Action: uirequest.ActionRenameWorktree,
			Origin: uirequest.Origin{
				TmuxSession: identity.session,
				Namespace:   identity.socket,
				WorkDir:     worktreeRoot,
				PID:         os.Getpid(),
			},
			Target: uirequest.Target{Kind: uirequest.TargetKindWorktree, Value: result.Name},
		})
	}
	return shellstate.RenameResult{Shell: identity.session, OldName: result.OldName, Name: result.Name, Changed: result.Changed}, nil
}

func currentManagedWorktree(ctx context.Context, stateDir string, identity shellIdentity) (projectRoot, worktreeRoot string, err error) {
	if identity.path == "" {
		return "", "", fmt.Errorf("tmux did not identify the managed worktree path")
	}
	worktreeRoot = workspaceops.WorktreeRoot(ctx, identity.path)
	if worktreeRoot == "" {
		return "", "", fmt.Errorf("current Sidecar worktree session is not inside a Git worktree")
	}
	if want := workspaceops.WorktreeSessionName(worktreeRoot, ""); identity.session != want {
		return "", "", fmt.Errorf("current tmux session does not match its Sidecar worktree identity")
	}
	projectRoot = workspaceops.MainWorktreePath(ctx, worktreeRoot)
	if projectRoot == "" {
		return "", "", fmt.Errorf("cannot resolve the owning Sidecar project")
	}
	if _, lookupErr := workspaceops.LookupWorktreeDisplayName(stateDir, projectRoot, worktreeRoot); lookupErr != nil {
		return "", "", lookupErr
	}
	return projectRoot, worktreeRoot, nil
}

func isHelp(arg string) bool { return arg == "-h" || arg == "--help" || arg == "help" }
