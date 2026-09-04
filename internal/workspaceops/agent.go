package workspaceops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/marcus/sidecar/internal/agentcatalog"
	"github.com/marcus/sidecar/internal/tty"
)

// AgentSkipFlag returns the CLI flag that opts this agent into auto-approve,
// or "" if the agent has no such flag. Creation forms use this to decide
// whether to show the auto-approve checkbox; do not copy this map elsewhere.
func AgentSkipFlag(agentType string) string {
	if family, ok := agentcatalog.FindLaunch(agentType); ok {
		return family.SkipPermissionsArg
	}
	return ""
}

// agentCommandWildcards are the configured keys that answer for any agent
// rather than naming one. They resolve a command; they do not make a name a
// family.
var agentCommandWildcards = map[string]bool{"*": true, "default": true}

// KnownAgentType reports whether agentType names an agent family this Sidecar
// can launch: a catalog family, a launchable legacy one, or a name the caller
// has configured a start command for.
//
// It exists because a value a caller has just typed deserves a verdict rather
// than a resolution. `--agent claud` records "claud" as the family, and every
// surface that keys off the agent type — the provider column, activity
// identification, session lookup — would then disagree with whatever ends up
// running in the pane. A picker cannot produce that value; a flag can, so the
// flag checks. It is deliberately wider than agentcatalog.Find: this answers
// "can this Sidecar launch it", which includes the legacy and configured
// families a picker no longer offers.
func KnownAgentType(agentType string, configured map[string]string) bool {
	agentType = strings.TrimSpace(agentType)
	if agentType == "" || agentCommandWildcards[agentType] {
		return false
	}
	if _, ok := agentcatalog.FindLaunch(agentType); ok {
		return true
	}
	_, ok := configured[agentType]
	return ok
}

// KnownAgentTypes lists the agent families a caller should choose from, sorted,
// so a refusal can say what was expected rather than only what was wrong.
//
// Narrower than KnownAgentType on purpose, and in one direction only: the
// legacy families agentcatalog keeps launchable are still accepted but are not
// offered here, for the same reason no picker offers them. A list is advice
// about what to type next, and advertising a compatibility case would be advice
// to start using it.
func KnownAgentTypes(configured map[string]string) []string {
	catalog := agentcatalog.Families()
	seen := make(map[string]bool, len(catalog)+len(configured))
	for _, family := range catalog {
		seen[family.ID] = true
	}
	for agent := range configured {
		if !agentCommandWildcards[agent] {
			seen[agent] = true
		}
	}
	out := make([]string, 0, len(seen))
	for agent := range seen {
		out = append(out, agent)
	}
	sort.Strings(out)
	return out
}

var openCodeRunPrefix = regexp.MustCompile(`^(\S+)\s+run(\s+.*)?$`)

// opaqueCommandSyntax is every character after which an appended argument
// might belong to a different command than the provider: pipes, lists,
// redirections, grouping, and command substitution.
const opaqueCommandSyntax = "|&;<>()`\n"

func ResolveAgentCommand(worktreePath, agentType string, configured map[string]string, skipPerms bool) string {
	if strings.TrimSpace(agentType) == "" {
		return ""
	}
	if command := readAgentStart(worktreePath); command != "" {
		return finishAgentCommand(command, agentType, skipPerms)
	}
	return ResolveAgentCommandFromConfig(agentType, configured, skipPerms)
}

// ResolveAgentLaunchArgv preserves the difference between catalog launches
// and legacy user-authored shell commands. Catalog launches remain structured
// argv. A .sidecar-agent-start or plugins.workspace.agentStart override stays
// opaque and is evaluated once through sh -lc; it must never be persisted as
// replayable structured provider metadata.
//
// extra are provider arguments the caller supplied alongside the family. On a
// catalog launch they are argv entries after the command. On an opaque
// command they are appended quoted, one shell word each, so the configured
// command still runs and a value with a space in it stays one argument — but
// only when that command is a plain command line. One that contains shell
// syntax (a pipe, a list, a redirection, a subshell) has no single place the
// arguments belong: appended to `claude | tee log` they would reach tee, and
// the provider would start without them and without a word said. That case
// is refused, naming the command, so the caller puts the arguments in it.
func ResolveAgentLaunchArgv(worktreePath, agentType string, configured map[string]string, skipPerms bool, extra []string) (argv []string, opaque bool, err error) {
	family, ok := agentcatalog.FindLaunch(strings.TrimSpace(agentType))
	if !ok {
		return nil, false, fmt.Errorf("unknown agent kind %q", agentType)
	}
	command := readAgentStart(worktreePath)
	if command == "" {
		for _, key := range []string{agentType, "*", "default"} {
			if command = sanitizeAgentCommand(configured[key]); command != "" {
				break
			}
		}
	}
	if command == "" {
		argv, err := family.LaunchArgv(extra, skipPerms)
		return argv, false, err
	}
	command = finishAgentCommand(command, agentType, skipPerms)
	if len(extra) > 0 {
		for _, arg := range extra {
			if strings.IndexByte(arg, 0) >= 0 {
				return nil, true, fmt.Errorf("provider argument contains NUL")
			}
		}
		if strings.ContainsAny(command, opaqueCommandSyntax) {
			return nil, true, fmt.Errorf("provider arguments cannot be appended to the configured launch command %q (.sidecar-agent-start or plugins.workspace.agentStart): it contains shell syntax, so put the arguments in that command instead", command)
		}
		command += " " + agentcatalog.ShellCommand(extra)
	}
	argv, err = agentcatalog.OpaqueLaunchArgv(command)
	return argv, true, err
}

// ResolveAgentCommandFromConfig resolves an agent's launch command from
// configuration and the built-in defaults alone, without consulting the
// checkout's .sidecar-agent-start file.
//
// It exists for callers whose worktree is on ANOTHER machine. Handing a remote
// path to ResolveAgentCommand does not fail — it reads THIS machine's file at a
// path that, on a developer's two similarly laid out machines, usually exists
// here too, and launches whatever that file says. That is the same failure class
// as resolving a remote workspace's path against the local filesystem, and it
// is silent.
func ResolveAgentCommandFromConfig(agentType string, configured map[string]string, skipPerms bool) string {
	if strings.TrimSpace(agentType) == "" {
		return ""
	}
	command := ""
	for _, key := range []string{agentType, "*", "default"} {
		if command = sanitizeAgentCommand(configured[key]); command != "" {
			break
		}
	}
	if command == "" {
		family, ok := agentcatalog.FindLaunch(agentType)
		if !ok {
			return ""
		}
		// LaunchArgs are part of the command, not arguments to it: for a family
		// whose bare command is not its agent, dropping them starts a program
		// that prints help and exits. They are joined bare rather than quoted
		// because a catalog family's subcommand is a plain word by construction,
		// which TestBundledFamilyArgumentsAreBareArgvEntries holds it to.
		//
		// Only the catalog default gets them. A configured command is the whole
		// line the user wrote, and splicing a subcommand into it would run
		// something they did not ask for.
		command = strings.TrimSpace(strings.Join(append([]string{family.Command}, family.LaunchArgs...), " "))
	}
	return finishAgentCommand(command, agentType, skipPerms)
}

// finishAgentCommand applies the per-agent rewrites every resolution shares,
// whatever the command's source was.
func finishAgentCommand(command, agentType string, skipPerms bool) string {
	if agentType == "opencode" {
		if match := openCodeRunPrefix.FindStringSubmatch(command); len(match) > 0 {
			command = strings.TrimSpace(match[1] + match[2])
		}
	}
	if skipPerms && AgentSkipFlag(agentType) != "" {
		command += " " + AgentSkipFlag(agentType)
	}
	return command
}

func WorktreeSessionName(path, name string) string {
	value := filepath.Base(path)
	if strings.TrimSpace(value) == "" || value == "." {
		value = name
	}
	var cleaned strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			cleaned.WriteRune(r)
		} else if cleaned.Len() > 0 && !strings.HasSuffix(cleaned.String(), "-") {
			cleaned.WriteByte('-')
		}
	}
	return "sidecar-ws-" + strings.Trim(cleaned.String(), "-")
}

func readAgentStart(worktreePath string) string {
	data, err := os.ReadFile(filepath.Join(worktreePath, ".sidecar-agent-start"))
	if err != nil {
		return ""
	}
	data = bytes.TrimSpace(data)
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return ""
	}
	return sanitizeAgentCommand(string(data))
}

func sanitizeAgentCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" || strings.ContainsAny(command, "\r\n") || !utf8.ValidString(command) {
		return ""
	}
	var cleaned strings.Builder
	for _, r := range command {
		if r == '\uFFFD' || r == '\uFEFF' || unicode.Is(unicode.Cf, r) || unicode.IsControl(r) {
			continue
		}
		cleaned.WriteRune(r)
	}
	command = strings.TrimSpace(cleaned.String())
	if command == "" {
		return ""
	}
	return command
}

type TmuxRunner interface {
	Run(context.Context, ...string) ([]byte, error)
}

type ExecTmuxRunner struct{}

func (ExecTmuxRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "tmux", args...).CombinedOutput()
}

type AgentLaunchSpec struct {
	SessionName, WorkDir, AgentCommand, TaskID string
	Env                                        map[string]string
	StartAgent                                 bool
}

type AgentLaunchResult struct {
	SessionName, PaneID string
	Reconnected         bool
}

func LaunchWorktreeSession(ctx context.Context, spec AgentLaunchSpec) (AgentLaunchResult, error) {
	return launchWorktreeSession(ctx, spec, ExecTmuxRunner{}, tty.NewSession)
}

func LaunchWorktreeSessionWithRunner(ctx context.Context, spec AgentLaunchSpec, runner TmuxRunner) (AgentLaunchResult, error) {
	return launchWorktreeSession(ctx, spec, runner, func(args ...string) error {
		_, err := runner.Run(ctx, args...)
		return err
	})
}

func launchWorktreeSession(ctx context.Context, spec AgentLaunchSpec, runner TmuxRunner, newSession func(...string) error) (AgentLaunchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result := AgentLaunchResult{SessionName: spec.SessionName}
	if spec.SessionName == "" || spec.WorkDir == "" {
		return result, fmt.Errorf("session name and worktree path are required")
	}
	if _, err := runner.Run(ctx, "has-session", "-t", spec.SessionName); err == nil {
		result.Reconnected = true
		result.PaneID = paneIDWithRunner(ctx, spec.SessionName, runner)
		return result, nil
	}
	if err := newSession("new-session", "-d", "-s", spec.SessionName, "-c", spec.WorkDir); err != nil {
		return result, fmt.Errorf("create session: %w", err)
	}
	failCreatedSession := func(err error) (AgentLaunchResult, error) {
		if _, cleanupErr := runner.Run(context.Background(), "kill-session", "-t", spec.SessionName); cleanupErr != nil {
			return result, fmt.Errorf("%w; cleanup session: %v", err, cleanupErr)
		}
		return result, err
	}
	send := func(command string) error {
		if command == "" {
			return nil
		}
		output, err := runner.Run(ctx, "send-keys", "-t", spec.SessionName, command, "Enter")
		if err != nil {
			return fmt.Errorf("send session command: %s: %w", strings.TrimSpace(string(output)), err)
		}
		return nil
	}
	if err := send("export TD_SESSION_ID=" + ShellQuote(spec.SessionName)); err != nil {
		return failCreatedSession(err)
	}
	if err := send(GenerateSingleEnvCommand(spec.Env)); err != nil {
		return failCreatedSession(err)
	}
	if spec.TaskID != "" {
		if err := send("td start " + ShellQuote(spec.TaskID)); err != nil {
			return failCreatedSession(err)
		}
	}
	if spec.StartAgent {
		if strings.TrimSpace(spec.AgentCommand) == "" {
			return failCreatedSession(fmt.Errorf("agent command is empty"))
		}
		time.Sleep(100 * time.Millisecond)
		if err := send(spec.AgentCommand); err != nil {
			return failCreatedSession(fmt.Errorf("start agent: %w", err))
		}
	}
	result.PaneID = paneIDWithRunner(ctx, spec.SessionName, runner)
	return result, nil
}

func StartAgentInShell(ctx context.Context, sessionName, command string) error {
	return StartAgentInShellWithRunner(ctx, sessionName, command, ExecTmuxRunner{})
}

func StartAgentInShellWithRunner(ctx context.Context, sessionName, command string, runner TmuxRunner) error {
	return sendKeysToShell(ctx, sessionName, command, true, runner)
}

// TypeInShell types command into the session without pressing Enter, so the
// user can review it. This is the send-keys core behind resume injection.
func TypeInShell(ctx context.Context, sessionName, command string) error {
	return TypeInShellWithRunner(ctx, sessionName, command, ExecTmuxRunner{})
}

func TypeInShellWithRunner(ctx context.Context, sessionName, command string, runner TmuxRunner) error {
	return sendKeysToShell(ctx, sessionName, command, false, runner)
}

func sendKeysToShell(ctx context.Context, sessionName, command string, execute bool, runner TmuxRunner) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("agent command is empty")
	}
	args := []string{"send-keys", "-t", sessionName, command}
	if execute {
		args = append(args, "Enter")
	}
	output, err := runner.Run(ctx, args...)
	if err != nil {
		verb := "type command"
		if execute {
			verb = "start agent"
		}
		return fmt.Errorf("%s: %s: %w", verb, strings.TrimSpace(string(output)), err)
	}
	return nil
}

func paneIDWithRunner(ctx context.Context, sessionName string, runner TmuxRunner) string {
	output, err := runner.Run(ctx, "list-panes", "-t", sessionName, "-F", "#{pane_id}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
}
