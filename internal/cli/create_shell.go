package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcus/sidecar/internal/agentcatalog"
	"github.com/marcus/sidecar/internal/agentcontrol"
	"github.com/marcus/sidecar/internal/shellstate"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspaceops"
)

func runCreateShell(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("create").FindSubcommand("shell")
	help := RenderHelp(cmd)

	usage := newUsageReporter(env, wantsJSON(args), help)
	flags := createCommonFlags{wait: createWaitDefault}
	nameFlag := ""
	cwdFlag := ""
	runCmd := ""
	typeCmd := ""
	agentKind := ""
	skipPerms := false
	var positional []string
	// extra are provider arguments written after `--`, the same vocabulary
	// `agent start TARGET --kind KIND -- ARGS` has: they follow the family's
	// launch command. This verb takes no positionals, so everything after the
	// terminator is theirs.
	var extra []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if isHelp(arg) {
			_, _ = fmt.Fprint(env.Stdout, help)
			return 0
		}
		if arg == "--" {
			extra = append(extra, args[i+1:]...)
			break
		}
		next, handled, code := applyCreateCommonFlag(arg, args, i, usage, &flags)
		if handled {
			if code != 0 {
				return code
			}
			i = next
			continue
		}
		switch {
		case arg == "--name" || strings.HasPrefix(arg, "--name="):
			val, next, ok := takeFlagArg(arg, args, i, "--name")
			if !ok || val == "" {
				return usage("--name requires a display name")
			}
			nameFlag = val
			i = next
		case arg == "--cwd" || strings.HasPrefix(arg, "--cwd="):
			val, next, ok := takeFlagArg(arg, args, i, "--cwd")
			if !ok || strings.TrimSpace(val) == "" {
				return usage("--cwd requires a directory")
			}
			cwdFlag = val
			i = next
		case arg == "--run" || strings.HasPrefix(arg, "--run="):
			val, next, ok := takeFlagArg(arg, args, i, "--run")
			if !ok || val == "" {
				return usage("--run requires a command")
			}
			runCmd = val
			i = next
		case arg == "--type" || strings.HasPrefix(arg, "--type="):
			val, next, ok := takeFlagArg(arg, args, i, "--type")
			if !ok || val == "" {
				return usage("--type requires a command")
			}
			typeCmd = val
			i = next
		case arg == "--agent" || strings.HasPrefix(arg, "--agent="):
			val, next, ok := takeFlagArg(arg, args, i, "--agent")
			if !ok || val == "" {
				return usage("--agent requires an agent type")
			}
			agentKind = val
			i = next
		case arg == "--skip-permissions":
			skipPerms = true
		default:
			if strings.HasPrefix(arg, "-") {
				return usage("unknown option %q", arg)
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) != 0 {
		return usage("create shell takes no positional arguments")
	}
	if runCmd != "" && typeCmd != "" {
		return usage("--run and --type are mutually exclusive")
	}
	// Trimmed before it is judged, not after: `--agent "  "` names no family, and
	// a guard that ran against the untrimmed value would let it through and then
	// record a shell with no agent type but SkipPerms set — durable state, and
	// replayed on every later start of that shell.
	agentKind = strings.TrimSpace(agentKind)
	if skipPerms && agentKind == "" {
		return usage("--skip-permissions requires --agent")
	}
	// Provider arguments extend the family's launch command, so they need a
	// family, and they need this run to be the one launching it: --run and
	// --type are opaque command lines nothing here can append to.
	if len(extra) > 0 && agentKind == "" {
		return usage("provider arguments after -- require --agent")
	}
	if len(extra) > 0 && (runCmd != "" || typeCmd != "") {
		return usage("provider arguments after -- extend --agent's launch; put them in the --run or --type command instead")
	}
	if flags.splitSet && flags.tab {
		return usage("--split and --tab name different placements")
	}
	// An explicit working directory belongs to a managed workspace shell: its
	// durable row is what keeps the live pane, JSON result, provider start, and
	// cold restore on one directory. A terminal-panel split has no such row, so
	// refuse the combination before destination resolution can register a
	// project or write a UI request.
	if cwdFlag != "" && flags.splitSet {
		return usage("--cwd creates a managed workspace shell and cannot be used with --split")
	}
	var explicitWorkDir string
	if cwdFlag != "" {
		var cwdErr error
		explicitWorkDir, cwdErr = resolveCreateShellCWD(cwdFlag)
		if cwdErr != nil {
			return emitAgentError(env, flags.jsonOutput, &agentcontrol.Error{
				Code: agentcontrol.ErrNotReady, Message: cwdErr.Error(), Err: cwdErr,
			})
		}
	}

	// One flag, two layers. The floor is the durable record: --agent always
	// writes the agent family into the shell's manifest row, ungated, because
	// that record is what keeps the shell on the Activity board while the agent
	// boots and whenever live screen identification misses a frame. On top of
	// that, when agent control is enabled and the caller named no command of
	// their own, the same flag starts the provider and waits for it to be ready.
	//
	// The layering is what lets one flag serve both callers. A viewer creating a
	// shell on a remote host passes --agent with --run: it owns the launch, so
	// the record is all it wants, and it gets the same answer whether or not
	// that host has agent control turned on.
	startAgent := false
	if agentKind != "" {
		// The family is checked before anything is created, and against the
		// families this Sidecar can actually launch, because a value a caller has
		// just typed deserves a verdict: `--agent claud` would otherwise create a
		// shell recorded as "claud", and every surface keyed on the type would
		// disagree with the pane. See workspaceops.KnownAgentType.
		//
		// Through emitAgentError so that both of this block's value refusals
		// answer a --json caller in the same shape. They already share an exit
		// code; printing one as an envelope and the other as prose would make
		// that code the only thing a script could rely on.
		configured := loadCreateConfig().Plugins.Workspace.AgentStart
		if !workspaceops.KnownAgentType(agentKind, configured) {
			return emitAgentError(env, flags.jsonOutput, &agentcontrol.Error{
				Code: agentcontrol.ErrNotReady,
				Message: fmt.Sprintf("unknown agent type %q; known types are %s",
					agentKind, strings.Join(workspaceops.KnownAgentTypes(configured), ", ")),
			})
		}
		if runCmd == "" && typeCmd == "" {
			enabled, err := agentControlEnabled(env)
			if err != nil {
				return emitAgentError(env, flags.jsonOutput, err)
			}
			startAgent = enabled
		}
		// Arguments for a launch this run will not perform would be dropped on
		// the floor, and a caller that wrote `-- --model X` and got a shell
		// with no provider in it has been told nothing. Refused instead.
		if len(extra) > 0 && !startAgent {
			return emitAgentError(env, flags.jsonOutput, &agentcontrol.Error{Code: agentcontrol.ErrFeatureDisabled, Message: "provider arguments after -- start the provider, which needs the agent_control feature; enable it, or launch with --run"})
		}
		// Only the start needs a launchable catalog family. A configured name
		// this Sidecar can record but agent control cannot launch is refused
		// here rather than after the shell exists, which is where
		// ResolveAgentLaunchArgv would otherwise say it.
		if startAgent {
			if _, ok := agentcatalog.FindLaunch(agentKind); !ok {
				return emitAgentError(env, flags.jsonOutput, &agentcontrol.Error{Code: agentcontrol.ErrNotReady, Message: fmt.Sprintf("unknown agent kind %q", agentKind)})
			}
		}
	}

	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	dest, err := resolveCreateDestination(ctx, env.StateDir, flags.shellFlag, flags.projectFlag, registerProject)
	if err != nil {
		cliErrln(env.Stderr, err)
		return createDestExitCode(err)
	}
	// --cwd changes where the new shell starts, not which project owns it. It was
	// validated before project resolution, then applied only after ownership was
	// independently resolved from --project/--shell/the caller. Using an
	// arbitrary destination directory as project evidence would bind an
	// unregistered checkout to whichever project happened to be ambient.
	if explicitWorkDir != "" {
		dest.Origin.WorkDir = explicitWorkDir
	}
	// --agent writes a field of a workspace shell's durable record, and a
	// beside-the-session split adds no such record ("do not add a workspace
	// row", below). Refused rather than accepted and dropped: a caller that
	// asked for the durable agent type and silently did not get one is exactly
	// the defect this flag exists to fix.
	if agentKind != "" && flags.splitSet {
		return usage("--agent records a workspace shell's agent type, and a beside-the-session split adds no workspace row")
	}
	// A shell asked for from inside a Sidecar shell opens beside that session
	// by default; switching the whole workspace to the new shell is what --tab
	// asks for explicitly (nt-7c82c9). Outside any Sidecar shell there is
	// nothing to split beside, so workspace placement is the only option.
	if dest.Origin.TmuxSession == "" && !flags.tab {
		if identity, idErr := currentShellIdentity(ctx); idErr == nil {
			dest.Origin.TmuxSession = identity.session
			if dest.Origin.WorkDir == "" {
				dest.Origin.WorkDir = identity.path
			}
		}
	}
	// --agent implies workspace placement for the same reason it is refused
	// with --split: it names a field only a workspace row has. It is not
	// spelled --tab because the caller did not ask where the shell goes; it
	// asked for something only one placement can carry.
	if flags.splitSet || (!flags.tab && cwdFlag == "" && agentKind == "" && dest.Origin.TmuxSession != "") {
		if dest.Origin.TmuxSession == "" {
			return usage("%s", createSplitNeedsShell)
		}
		if !flags.splitSet {
			flags.splitMode = "auto"
		}
		code := runCreateShellSplit(env, dest, flags, nameFlag, runCmd, typeCmd)
		// Beside-the-session was only the default, not something the caller
		// asked for: a DECLINE (feature off, no room, no live instance) falls
		// back to a workspace shell so the command still lands.
		//
		// Only a decline. A verdict about the command itself is final wherever
		// placement would have put it, and retrying it can only produce the
		// same refusal twice — which is exactly what exit 5 did when it was
		// missing from this list: `--name <too long>` printed its refusal, then
		// "created a workspace shell instead", then the refusal again, having
		// created nothing. Listed by what falls through rather than by what
		// does not, so the next new code is final by default.
		declined := code == 3 || code == 4
		if !declined || flags.splitSet {
			return code
		}
		cliErrf(env.Stderr, "no beside-the-session placement available; created a workspace shell instead\n")
	}

	return runCreateShellWorkspace(env, dest, flags, nameFlag, runCmd, typeCmd, agentKind, skipPerms, startAgent, extra)
}

// resolveCreateShellCWD resolves an explicit shell destination before any
// tmux session, manifest row, or UI request is created. Relative paths are
// relative to the caller's current directory. A leading ~ names the caller's
// home directory; named-user expansion is intentionally refused rather than
// delegated to a shell whose rules would differ across platforms.
func resolveCreateShellCWD(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("invalid --cwd: directory must not be empty")
	}
	raw := value
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve --cwd %q: %w", value, err)
		}
		if raw == "~" {
			raw = home
		} else {
			raw = filepath.Join(home, strings.TrimPrefix(raw, "~/"))
		}
	} else if strings.HasPrefix(raw, "~") {
		return "", fmt.Errorf("invalid --cwd %q: only ~ and ~/path home expansion are supported", value)
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve --cwd %q: %w", value, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("invalid --cwd %q: %w", value, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("invalid --cwd %q: not a directory", value)
	}
	// tmux chdirs before starting the shell, so its effective directory is the
	// filesystem target rather than a symlink spelling. Persist that same path
	// so JSON, provider start, and cold restore all agree with the live pane.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs), nil
}

func runCreateShellWorkspace(env Env, dest openDestination, flags createCommonFlags, nameFlag, runCmd, typeCmd, agentKind string, skipPerms, startAgent bool, extra []string) int {
	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	proj, err := registeredProjectForCreate(env.StateDir, dest)
	if err != nil {
		cliErrln(env.Stderr, err)
		return createDestExitCode(err)
	}
	if proj.Path == "" {
		cliErrln(env.Stderr, "no Sidecar project is registered for this directory; pass --project or run from a registered project")
		return 2
	}
	// dest.Origin.WorkDir already carries the caller's own worktree, resolved
	// by resolveCreateDestination's ladder (SIDECAR_SHELL, tmux identity, or
	// cwd) before registeredProjectForCreate collapsed it to the owning
	// project's root. proj.Path is only the fallback for a destination that
	// resolved no directory of its own (an explicit --project, say): using it
	// unconditionally is what put a worktree's new shell in the main checkout
	// (td-e3a93d).
	workDir := dest.Origin.WorkDir
	if workDir == "" {
		workDir = proj.Path
	}

	display, session := workspaceops.ShellNames(proj.Path, existingShellDefinitions(proj))
	if custom := strings.TrimSpace(nameFlag); custom != "" {
		var err error
		display, err = shellstate.NormalizeName(custom)
		if err != nil {
			cliErrln(env.Stderr, err)
			return exitInputRejected
		}
	}

	spec := workspaceops.ManagedShellSpec{
		ShellSpec: workspaceops.ShellSpec{
			WorkDir:     workDir,
			SessionName: session,
			DisplayName: display,
		},
		ProjectRoot: proj.Path,
		// The durable statement of which agent family this shell is for, in the
		// same field the TUI's Create Shell writes. It is written whether or not
		// this run also starts the process: it is the backstop that keeps the
		// shell on the Activity board while the agent is still booting and
		// whenever live screen identification momentarily fails — which is the
		// whole reason a remote agent shell used to go missing where its local
		// twin did not.
		AgentType: agentKind,
		SkipPerms: skipPerms,
	}
	if _, err := workspaceops.CreateManagedShell(spec); err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	var seedErr error
	if runCmd != "" {
		seedErr = workspaceops.StartAgentInShell(ctx, session, runCmd)
	} else if typeCmd != "" {
		seedErr = workspaceops.TypeInShell(ctx, session, typeCmd)
	} else if startAgent {
		_, seedErr = startCreatedAgent(ctx, proj, session, display, workDir, agentKind, skipPerms, extra)
	}

	focus := true
	payload := uirequest.CreatePayload{
		Kind:        uirequest.CreateKindShell,
		Session:     session,
		DisplayName: display,
		Focus:       &focus,
	}
	dest.Origin.ProjectKey = proj.Key
	dest.Origin.WorkDir = workDir
	req, reqErr := writeCreateRequest(env, dest, payload, uirequest.Target{
		Kind:  uirequest.TargetKindShell,
		Value: session,
	}, uirequest.Options{})
	if reqErr != nil {
		cliErrln(env.Stderr, reqErr)
		if seedErr != nil {
			cliErrln(env.Stderr, seedErr)
		}
		return 1
	}

	acks := pollCreateAcks(env.StateDir, req.ID, req.Action, flags.wait)
	result := createShellResult{
		Shell: createShellInfo{
			DisplayName: display,
			Session:     session,
			WorkDir:     workDir,
		},
		Project:   proj.Key,
		Acked:     len(acks) > 0,
		Surface:   createAckSurface(acks),
		Placement: createPlacementWorkspace,
	}

	if flags.jsonOutput {
		if err := json.NewEncoder(env.Stdout).Encode(result); err != nil {
			cliErrln(env.Stderr, err)
			if seedErr != nil {
				return 1
			}
			return 1
		}
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "Created shell %q (%s).\n", display, session)
	}
	if seedErr != nil {
		cliErrln(env.Stderr, seedErr)
		return 1
	}
	return 0
}

func runCreateShellSplit(env Env, dest openDestination, flags createCommonFlags, nameFlag, runCmd, typeCmd string) int {
	display := strings.TrimSpace(nameFlag)
	if display != "" {
		normalized, err := shellstate.NormalizeName(display)
		if err != nil {
			cliErrln(env.Stderr, err)
			return exitInputRejected
		}
		display = normalized
	}

	workDir := dest.Origin.WorkDir
	if proj, err := registeredProjectForCreate(env.StateDir, dest); err == nil && proj.Path != "" {
		dest.Origin.ProjectKey = proj.Key
		if workDir == "" {
			workDir = proj.Path
		}
		if dest.Origin.WorkDir == "" {
			dest.Origin.WorkDir = proj.Path
		}
	}

	focus := true
	payload := uirequest.CreatePayload{
		Kind:        uirequest.CreateKindShell,
		DisplayName: display,
		Focus:       &focus,
		Run:         runCmd,
		Type:        typeCmd,
	}
	req, reqErr := writeCreateRequest(env, dest, payload, uirequest.Target{
		Kind:  uirequest.TargetKindShell,
		Value: dest.Origin.TmuxSession,
	}, uirequest.Options{Split: flags.splitMode})
	if reqErr != nil {
		cliErrln(env.Stderr, reqErr)
		return 1
	}

	result := createShellResult{
		Shell: createShellInfo{
			DisplayName: display,
			WorkDir:     workDir,
		},
		Project:   dest.Origin.ProjectKey,
		Placement: flags.splitMode,
	}

	emit := func() {
		if flags.jsonOutput {
			if err := json.NewEncoder(env.Stdout).Encode(result); err != nil {
				cliErrln(env.Stderr, err)
			}
			return
		}
		if result.Shell.Session != "" {
			_, _ = fmt.Fprintf(env.Stdout, "Created split %q (%s).\n", display, result.Shell.Session)
			return
		}
		_, _ = fmt.Fprintf(env.Stdout, "Sent split request for %q.\n", display)
	}

	if flags.wait <= 0 {
		emit()
		return 0
	}

	acks := pollCreateAcks(env.StateDir, req.ID, req.Action, flags.wait)
	result.Acked = len(acks) > 0
	result.Surface = createAckSurface(acks)
	if session := createAckSession(acks); session != "" {
		result.Shell.Session = session
	}

	if createAcksOpened(acks) {
		emit()
		return 0
	}
	emit()
	if createAcksAllDeclined(acks) {
		reason := createAcksDeclinedReason(acks)
		if reason == "" {
			reason = "the window is too small to split"
		}
		cliErrln(env.Stderr, reason)
		return 4
	}
	if dest.Origin.TmuxSession != "" {
		cliErrf(env.Stderr, "no running Sidecar instance is showing this shell (%s)\n", dest.Origin.TmuxSession)
	} else {
		cliErrf(env.Stderr, "no running Sidecar instance is showing this project (%s)\n", dest.Origin.ProjectKey)
	}
	return 3
}

type createShellInfo struct {
	DisplayName string `json:"displayName"`
	Session     string `json:"session"`
	WorkDir     string `json:"workDir"`
}

type createShellResult struct {
	Shell createShellInfo `json:"shell"`
	// Project is the registered project slug the shell belongs to: the value
	// `--project` on every other verb accepts, so a caller holding this result
	// can address what it created without guessing the selector.
	Project   string `json:"project,omitempty"`
	Acked     bool   `json:"acked"`
	Surface   string `json:"surface,omitempty"`
	Placement string `json:"placement"`
}
