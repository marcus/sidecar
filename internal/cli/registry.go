package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/marcus/sidecar/internal/config"
)

// RootCommand returns the root command hierarchy.
func RootCommand() *Command {
	root := &Command{
		Name:    "sidecar",
		Summary: "A TUI dashboard for AI coding agents. When run without a command, starts the interactive TUI.",
		Usage:   "sidecar <command> [options]",
	}

	helpCmd := &Command{
		Name:    "help",
		Summary: "Show help for commands or emit JSON command metadata",
		Usage:   "sidecar help [--json] [<command>]",
		Long:    "Show help for Sidecar commands, or emit the full machine-readable command tree.",
		Flags: []Flag{
			{Name: "--json", Summary: "Write the command tree as JSON to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 0, Max: -1, Description: "Optional command path to inspect"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "success"},
			{Code: 2, Summary: "unknown command"},
		},
		Examples: []Example{
			{Command: "sidecar help"},
			{Command: "sidecar help open"},
			{Command: "sidecar help --json"},
		},
		Run: runHelpCommand,
	}

	nameCmd := &Command{
		Name:    "name",
		Summary: "Print the current shell's display name",
		Usage:   "sidecar shell name [--json]",
		Long: "Print the Sidecar display name of the managed shell or worktree agent containing\n" +
			"this command. Reads registered Sidecar state (authoritative), not the agent SDK\n" +
			"or $SIDECAR_SHELL_NAME, so reopening another agent in place keeps its context.\n\n" +
			"Human output is the display name alone, one line, for easy scripting.\n" +
			"JSON includes the stable tmux session id and display name.",
		Flags: []Flag{
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "success"},
			{Code: 1, Summary: "identity or state failure"},
			{Code: 2, Summary: "usage error"},
		},
		Examples: []Example{
			{Command: "sidecar shell name"},
			{Command: "sidecar shell name --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar shell name",
			Summary:    "Read the name this shell shows the user",
		},
		Run: runShellName,
	}

	renameCmd := &Command{
		Name:    "rename",
		Summary: "Rename the current shell's display name",
		Usage:   "sidecar shell rename [--json] <display-name>",
		Long: "Rename only the Sidecar-managed shell or worktree agent containing this command.\n" +
			"This changes Sidecar's display name; it does not rename the tmux session, Git\n" +
			"branch, or worktree directory.\n\n" +
			"The current display name is also published as $SIDECAR_SHELL_NAME. \"Shell 3\"\n" +
			"is the unset default; a previous task's name is equally stale — rename when\n" +
			"the name no longer describes the work in this shell.",
		Flags: []Flag{
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "success"},
			{Code: 1, Summary: "identity or state failure"},
			{Code: 2, Summary: "usage or validation error"},
		},
		Examples: []Example{
			{Command: "sidecar shell rename \"shell rename implementation\""},
		},
		Agent: AgentDoc{
			Invocation: "sidecar shell rename \"<short context>\"",
			Summary:    "Keep the shell's name describing the work you are doing now",
		},
		Run: runShellRename,
	}

	shellCmd := &Command{
		Name:    "shell",
		Summary: "Manage the current Sidecar shell context",
		Usage:   "sidecar shell <command>",
		Long:    "Manage the current Sidecar-managed shell or worktree agent context.",
		Sub:     []*Command{nameCmd, renameCmd},
		Run:     runShellRoot,
	}

	openCmd := &Command{
		Name:    "open",
		Summary: "Show a file, a td issue, a git diff, or a provider resource in a split pane",
		Usage:   "sidecar open [options] [<target>]",
		Long: "Show a file, a td issue, a git diff, or an external provider resource to the user as a\n" +
			"split pane in a Sidecar workspace. From a Sidecar shell this targets that shell.\n" +
			"Otherwise it targets the unique running instance, or a specific --shell / --project.\n" +
			"--diff with no spec is the working tree. --provider names a configured terminal resource\n" +
			"provider instance and is required for a resource: a bare locator is never guessed at.\n" +
			"--split only overrides the split axis; it never halves a live terminal after content is open.",
		Targets: []TargetDoc{
			{Target: "path", Summary: "A file inside the target workspace, optionally \"path:line\""},
			{Target: "td-xxxxxx", Summary: "A td issue id"},
			{Target: "--diff", Summary: "Working-tree diff (wt); add a spec for a commit or range"},
			{Target: "spec", Summary: "A git commit or range (abc1234, A..B); --diff accepts HEAD and branch names"},
			{Target: "locator", Summary: "With --provider, a resource key such as CASH-1245"},
		},
		Flags: []Flag{
			{Name: "--line", Arg: "N", Summary: "Line to reveal (alternative to \"path:line\")"},
			{Name: "--diff", Summary: "Open a Diff leaf (working tree if no spec)", Bool: true},
			{Name: "--provider", Arg: "ID", Summary: "Open a locator through a configured terminal resource provider"},
			{Name: "--shell", Arg: "NAME", Summary: "Target a registered shell by display name or tmux name"},
			{Name: "--project", Arg: "NAME", Summary: "Target a project's Workspaces surface (slug, basename, or path)"},
			{Name: "--split", Arg: "auto|right|below", Summary: "Where to place a new pane (default auto)"},
			{Name: "--wait", Arg: "DURATION", Summary: "Time to wait for instances to acknowledge (default 1200ms; 0 = fire and forget)"},
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 0, Max: 1, Description: "File, td-xxxxxx, or git spec; omitted with --diff for the working tree"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "opened or queued"},
			{Code: 1, Summary: "state failure"},
			{Code: 2, Summary: "usage or validation error"},
			{Code: 3, Summary: "no running instance, or several running with no target"},
			{Code: 4, Summary: "an instance declined (e.g. the window is too small to split)"},
		},
		Examples: []Example{
			{Command: "sidecar open internal/cli/cli.go", Description: "file, in a split beside the terminal"},
			{Command: "sidecar open internal/cli/cli.go:88", Description: "file at a line"},
			{Command: "sidecar open td-348d88", Description: "td issue"},
			{Command: "sidecar open --diff", Description: "working-tree Diff leaf"},
			{Command: "sidecar open --diff HEAD", Description: "that commit, not the working tree"},
			{Command: "sidecar open abc1234", Description: "commit, unless a file of that name exists"},
			{Command: "sidecar open --provider jira-work CASH-1245", Description: "resource pane for that provider's locator"},
			{Command: "sidecar open --json --split below README.md", Description: "structured result for the agent"},
			{Command: "sidecar open --project sidecar README.md", Description: "from any terminal, that project's Workspaces surface"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar open <path>[:line] | td-xxxxxx | --diff [spec] | --provider ID <locator>",
			Summary:    "Put a file, a td issue, a git diff, or a provider resource in front of the user",
		},
		Run: runOpen,
	}

	agentsCmd := &Command{
		Name:    "agents",
		Summary: "List what an agent can do from Sidecar",
		Usage:   "sidecar --agents",
		Long: "List the Sidecar commands worth reaching for, one line each.\n" +
			"Also spelled \"sidecar --agents\".",
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "success"},
		},
		Examples: []Example{
			{Command: "sidecar --agents"},
		},
		Run: func(env Env, _ []string) int {
			_, _ = fmt.Fprint(env.Stdout, RenderAgents(RootCommand()))
			return 0
		},
	}

	setupCmd := &Command{
		Name:    "setup",
		Summary: "Start Sidecar with Configuration open on Sidecar Setup",
		Usage:   "sidecar setup [options]",
		Long: "Start Sidecar normally, with Configuration open on the Sidecar Setup page.\n" +
			"Setup lists what is left to do — add a project, install tmux, connect agent\n" +
			"instructions — and opens a focused repair for each one.\n\n" +
			"This is a launch route, not a second settings interface: it renders nothing in\n" +
			"the terminal and changes nothing on its own. Sidecar's ordinary options still\n" +
			"apply (sidecar setup -project /path). Escape returns to the surface underneath,\n" +
			"and the header gear reopens Configuration at any time.\n\n" +
			"If startup fails before Sidecar can draw — a malformed config file, a terminal\n" +
			"that is not interactive — it exits nonzero with the specific next step and a\n" +
			"support path that uploads nothing.",
		Flags: []Flag{
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 0, Max: 0},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "Sidecar ran and exited normally"},
			{Code: 1, Summary: "startup failed before the first frame"},
			{Code: 2, Summary: "usage error"},
		},
		Examples: []Example{
			{Command: "sidecar setup"},
			{Command: "sidecar setup -project ~/code/myproject", Description: "that project's Setup"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar setup",
			Summary:    "Open Sidecar's Configuration on Setup so the user can finish configuring it",
		},
		Launch: runSetupLaunch,
	}

	root.Sub = []*Command{agentsCmd, helpCmd, notifyCommand(), openCmd, setupCmd, shellCmd, terminalLinksCommand()}
	return root
}

// notifyCommand is the agent-facing side of the notification system: post an
// alert the user sees as a toast and in the notification centre, dismiss one
// you posted, and read the log back.
//
// Listing reads the log directly, so it answers with no Sidecar running.
// Posting prefers a running instance (the user sees it immediately) and falls
// back to writing the log, so nothing is lost either way.
func notifyCommand() *Command {
	postCmd := &Command{
		Name:    "post",
		Summary: "Post a notification the user sees in Sidecar",
		Usage:   "sidecar notify post [options] <title>",
		Long: "Post a notification. It appears as a toast in the running Sidecar instance for\n" +
			"this shell's project and stays in the notification centre until dismissed.\n\n" +
			"With no instance running the notification is written to Sidecar's notification\n" +
			"log and appears at the next start; nothing is lost.\n\n" +
			"--expiry sets how long the toast stays on screen — a duration such as 10s, or\n" +
			"\"never\" for one that waits for the user. Expiry never removes the notification\n" +
			"from the centre.\n\n" +
			"--target attaches a call to action: the notification centre numbers targets 1-N\n" +
			"and the user jumps to one with enter or a digit. Repeat it for several, in the\n" +
			"order they should be numbered. The form is kind:value[:line][@project], where\n" +
			"kind is issue, task, commit, file, session or url; :line applies to files only;\n" +
			"and @project names another checkout by configured project name or by path, in\n" +
			"which case Sidecar switches projects and then lands. Ids written in the title or\n" +
			"body are still found by scanning — --target is for precision and for targets the\n" +
			"text does not spell out.",
		Flags: []Flag{
			{Name: "--body", Arg: "TEXT", Summary: "Detail line shown under the title"},
			{Name: "--target", Arg: "SPEC", Summary: "Call to action, kind:value[:line][@project]; repeatable"},
			{Name: "--source", Arg: "ID", Summary: "Source: agent, waiting, session, tasks, td, system (default agent)"},
			{Name: "--expiry", Arg: "DURATION", Summary: "Toast lifetime (e.g. 10s), or \"never\" (default: the source's)"},
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "The notification title, one short line"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "posted, or stored for the next start"},
			{Code: 1, Summary: "state failure"},
			{Code: 2, Summary: "usage or validation error"},
		},
		Examples: []Example{
			{Command: "sidecar notify post \"Tests are green\""},
			{Command: "sidecar notify post \"Need a decision\" --source waiting --expiry never"},
			{Command: "sidecar notify post \"Build failed\" --body \"go test ./internal/app\" --json"},
			{Command: "sidecar notify post \"Review needed\" --target issue:td-4c1f9a --target file:internal/app/model.go:42"},
			{Command: "sidecar notify post \"Fixed upstream\" --target issue:td-99aabb@braid"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar notify post \"<short title>\" [--body TEXT] [--source ID] [--target kind:value[:line][@project]]",
			Summary:    "Tell the user something happened without making them watch this shell",
		},
		Run: runNotifyPost,
	}

	dismissCmd := &Command{
		Name:    "dismiss",
		Summary: "Dismiss a notification you posted",
		Usage:   "sidecar notify dismiss [--json] <id>",
		Long: "Dismiss one notification. A caller may only dismiss notifications it posted:\n" +
			"identity is the Sidecar shell you are in, or failing that the working directory,\n" +
			"so the notification you posted a moment ago is dismissible and the user's own\n" +
			"and other agents' are not.",
		Flags: []Flag{
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "The notification id from post or list"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "dismissed"},
			{Code: 1, Summary: "state failure"},
			{Code: 2, Summary: "usage error"},
			{Code: 3, Summary: "no notification with that id"},
			{Code: 4, Summary: "that notification was posted by someone else"},
		},
		Examples: []Example{
			{Command: "sidecar notify dismiss ntf-06215f4b1a2c3-9f1e2d3c"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar notify dismiss <id>",
			Summary:    "Take back a notification you posted once it no longer matters",
		},
		Run: runNotifyDismiss,
	}

	listCmd := &Command{
		Name:    "list",
		Summary: "List notifications",
		Usage:   "sidecar notify list [--all] [--unread] [--json]",
		Long: "List notifications, newest first. This reads Sidecar's notification log directly,\n" +
			"so it answers whether or not Sidecar is running and never changes anything.\n\n" +
			"By default dismissed notifications are left out; --all includes them.",
		Flags: []Flag{
			{Name: "--all", Summary: "Include dismissed notifications", Bool: true},
			{Name: "--unread", Summary: "Only notifications the user has not seen", Bool: true},
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 0, Max: 0},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "success"},
			{Code: 1, Summary: "the notification log could not be read"},
			{Code: 2, Summary: "usage error"},
		},
		Examples: []Example{
			{Command: "sidecar notify list"},
			{Command: "sidecar notify list --unread --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar notify list --json",
			Summary:    "See what Sidecar is currently telling the user",
		},
		Run: runNotifyList,
	}

	return &Command{
		Name:    "notify",
		Summary: "Post, dismiss, and list Sidecar notifications",
		Usage:   "sidecar notify <command>",
		Long: "Sidecar's notification surface: a toast in the running instance, an entry in the\n" +
			"notification centre, and a count in the header until the user reads it.",
		Sub: []*Command{dismissCmd, listCmd, postCmd},
		Run: runNotifyRoot,
	}
}

// terminalLinksCommand is the protocol/admin surface for terminal resource
// providers. It deliberately does not describe or resolve anything the user did
// not ask for: `list` reads configuration, `check` adds one describe, and
// `--resolve` is a separate explicit flag because it can reach the network and
// print private resource data.
func terminalLinksCommand() *Command {
	listCmd := &Command{
		Name:    "list",
		Summary: "List configured terminal resource providers",
		Usage:   "sidecar terminal-links list [--describe] [--json] [--config PATH]",
		Long: "List the terminal resource providers configured under \"terminalResources\".\n" +
			"By default this reads configuration and resolves each command on PATH; it starts\n" +
			"no process. --describe additionally asks each enabled provider to describe itself,\n" +
			"which is local and non-interactive but does spawn one child per instance.\n\n" +
			"passEnv is reported by name and presence only. A passed value is never printed.\n\n" +
			"Enabling a provider trusts that executable with your full OS privileges: a process\n" +
			"boundary is crash isolation, not a sandbox.",
		Flags: []Flag{
			{Name: "--describe", Summary: "Also run each enabled provider's describe method", Bool: true},
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--config", Arg: "PATH", Summary: "Read a specific config file"},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 0, Max: 0},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "success"},
			{Code: 1, Summary: "configuration could not be read"},
			{Code: 2, Summary: "usage error"},
		},
		Examples: []Example{
			{Command: "sidecar terminal-links list"},
			{Command: "sidecar terminal-links list --json"},
			{Command: "sidecar terminal-links list --describe --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar terminal-links list --json",
			Summary:    "See which terminal resource providers are configured and whether their commands resolve",
		},
		Run: runTerminalLinksList,
	}

	checkCmd := &Command{
		Name:    "check",
		Summary: "Check one terminal resource provider instance",
		Usage:   "sidecar terminal-links check [--resolve LOCATOR] [--json] [--config PATH] <instance>",
		Long: "Check one configured provider instance: that it is enabled, that its command\n" +
			"resolves, and that its describe method answers the protocol. The child runs with\n" +
			"the exact working directory, base environment, passEnv policy, and timeout Sidecar\n" +
			"uses in the TUI, so this is the authoritative host-environment proof.\n\n" +
			"--resolve is separate and explicit because it can perform network access and print\n" +
			"private resource data. Without it, nothing is resolved.\n\n" +
			"The provider's stderr is drained and discarded, never printed: reproduce provider\n" +
			"failures by running the provider's own CLI deliberately.",
		Flags: []Flag{
			{Name: "--resolve", Arg: "LOCATOR", Summary: "Also resolve one locator (may hit the network and print private data)"},
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--config", Arg: "PATH", Summary: "Read a specific config file"},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "The provider instance id from configuration"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "the instance checked out"},
			{Code: 1, Summary: "the command, describe, or resolve failed"},
			{Code: 2, Summary: "usage error"},
			{Code: 3, Summary: "no provider instance with that id is configured"},
		},
		Examples: []Example{
			{Command: "sidecar terminal-links check jira-work"},
			{Command: "sidecar terminal-links check jira-work --json"},
			{Command: "sidecar terminal-links check jira-work --resolve CASH-1245 --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar terminal-links check <instance> --json",
			Summary:    "Prove a terminal resource provider is configured and speaking the protocol",
		},
		Run: runTerminalLinksCheck,
	}

	return &Command{
		Name:    "terminal-links",
		Summary: "Inspect terminal resource providers",
		Usage:   "sidecar terminal-links <command>",
		Long: "Inspect the external executables that teach Sidecar to recognize resource keys in\n" +
			"terminal output. This is a protocol and administration surface, not a replacement\n" +
			"for a provider's own CLI.",
		Sub: []*Command{checkCmd, listCmd},
		Run: runTerminalLinksRoot,
	}
}

func runHelpCommand(env Env, args []string) int {
	jsonOutput := false
	var path []string
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		} else if arg == "-h" || arg == "--help" {
			// Show help for help itself
			_, _ = fmt.Fprint(env.Stdout, RenderHelp(RootCommand().FindSubcommand("help")))
			return 0
		} else if strings.HasPrefix(arg, "-") {
			cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, RenderHelp(RootCommand().FindSubcommand("help")))
			return 2
		} else {
			path = append(path, arg)
		}
	}

	root := RootCommand()
	if len(path) == 0 {
		if jsonOutput {
			if err := RenderJSON(env.Stdout, root); err != nil {
				cliErrln(env.Stderr, err)
				return 1
			}
			return 0
		}
		_, _ = fmt.Fprint(env.Stdout, RenderHelp(root))
		return 0
	}

	curr := root
	for _, segment := range path {
		sub := curr.FindSubcommand(segment)
		if sub == nil {
			cliErrf(env.Stderr, "unknown command %q\n\n%s", strings.Join(path, " "), RenderHelp(curr))
			return 2
		}
		curr = sub
	}

	if jsonOutput {
		if err := RenderJSON(env.Stdout, curr); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		return 0
	}

	_, _ = fmt.Fprint(env.Stdout, RenderHelp(curr))
	return 0
}

func runShellRoot(env Env, args []string) int {
	shellCmd := RootCommand().FindSubcommand("shell")
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprint(env.Stdout, RenderHelp(shellCmd))
		return 0
	}
	sub := shellCmd.FindSubcommand(args[0])
	if sub != nil && sub.Run != nil {
		return sub.Run(env, args[1:])
	}
	cliErrf(env.Stderr, "unknown shell command %q\n\n%s", args[0], RenderHelp(shellCmd))
	return 2
}

func defaultEnv(stdout, stderr io.Writer) Env {
	return Env{
		Stdout:   stdout,
		Stderr:   stderr,
		StateDir: config.StateDir(),
		Ctx:      context.Background(),
	}
}
