package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/notifydelivery"
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
		Summary: "Rename the current shell, or one named with --target",
		Usage:   "sidecar shell rename [--target SESSION [--project NAME]] [--json] <display-name>",
		Long: "Rename the Sidecar-managed shell or worktree agent containing this command, or\n" +
			"with --target, one you are not sitting in. This changes Sidecar's display name;\n" +
			"it does not rename the tmux session, Git branch, or worktree directory.\n\n" +
			"The current display name is also published as $SIDECAR_SHELL_NAME. \"Shell 3\"\n" +
			"is the unset default; a previous task's name is equally stale — rename when\n" +
			"the name no longer describes the work in this shell.\n\n" +
			"--target takes a tmux session name: a sidecar-sh-… record from `sidecar shell\n" +
			"list`, or a sidecar-ws-… worktree agent. The session must belong to the resolved\n" +
			"project (--project, or the project this directory is in) — a name Sidecar does\n" +
			"not own is refused rather than renamed. --shell and --project only scope a\n" +
			"--target; without one, the current shell is the only subject.",
		Flags: []Flag{
			{Name: "--target", Arg: "SESSION", Summary: "Rename this tmux session instead of the current shell"},
			{Name: "--shell", Arg: "NAME", Summary: "Resolve the project from a registered shell (with --target)"},
			{Name: "--project", Arg: "NAME", Summary: "Target project (slug, basename, or path; with --target)"},
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "renamed, or already named that"},
			{Code: 1, Summary: "identity, ambiguity, or state failure"},
			{Code: 2, Summary: "usage error; without --target, also a rejected display name (the current-shell form's long-standing code)"},
			{Code: 3, Summary: "--target names no session this project owns"},
			{Code: 5, Summary: "with --target: a value was rejected — the display name (already used, or not legal), or an unknown --project / --shell"},
		},
		Examples: []Example{
			{Command: "sidecar shell rename \"shell rename implementation\""},
			{Command: "sidecar shell rename --target sidecar-sh-sidecar-2 --json \"release prep\""},
			{Command: "sidecar shell rename --project sidecar --target sidecar-ws-sidecar-fix-auth \"fix auth\""},
		},
		Agent: AgentDoc{
			Invocation: "sidecar shell rename [--target SESSION] \"<short context>\"",
			Summary:    "Keep the shell's name describing the work you are doing now",
		},
		Mutates: true,
		Run:     runShellRename,
	}

	sendCmd := &Command{
		Name:    "send",
		Summary: "Run or type a command in a shell you are not sitting in",
		Usage:   "sidecar shell send --target SESSION (--run COMMAND | --type COMMAND) [--project NAME] [--json]",
		Long: "Send a command to an existing Sidecar-managed session. --run presses Enter;\n" +
			"--type leaves the command on the prompt for the user to read and run. This is\n" +
			"the same distinction `sidecar create shell --run/--type` makes, for a session\n" +
			"that already exists.\n\n" +
			"--target is required and must name a session the resolved project owns: a\n" +
			"sidecar-sh-… record in its shells.json, or a sidecar-ws-… agent for one of its\n" +
			"registered worktrees. tmux resolves a session name against whatever answers to\n" +
			"it, so an unregistered name is refused (exit 3) rather than typed into.\n\n" +
			"The keys go to the tmux server this process resolves, and the session must be\n" +
			"running: a record for a session that is not up is a tmux failure (exit 1), not\n" +
			"a silent success.",
		Flags: []Flag{
			{Name: "--target", Arg: "SESSION", Summary: "The tmux session to send to (required)"},
			{Name: "--run", Arg: "COMMAND", Summary: "Execute COMMAND in the session"},
			{Name: "--type", Arg: "COMMAND", Summary: "Type COMMAND without pressing Enter"},
			{Name: "--shell", Arg: "NAME", Summary: "Resolve the project from a registered shell"},
			{Name: "--project", Arg: "NAME", Summary: "Target project (slug, basename, or path)"},
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 0, Max: 0},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "sent"},
			{Code: 1, Summary: "tmux, ambiguity, or state failure"},
			{Code: 2, Summary: "usage error"},
			{Code: 3, Summary: "--target names no session this project owns, or one recorded on a different tmux server"},
			{Code: 5, Summary: "an unknown --project or --shell"},
		},
		Examples: []Example{
			{Command: "sidecar shell send --target sidecar-sh-sidecar-2 --run \"claude\"", Description: "start an agent in an existing shell"},
			{Command: "sidecar shell send --target sidecar-ws-sidecar-fix-auth --run \"go test ./...\""},
			{Command: "sidecar shell send --target sidecar-sh-sidecar-2 --type \"git push\" --json", Description: "leave it for the user to run"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar shell send --target SESSION (--run COMMAND | --type COMMAND)",
			Summary:    "Start an agent or run a command in another Sidecar shell",
		},
		Mutates: true,
		Run:     runShellSend,
	}

	listCmd := &Command{
		Name:    "list",
		Summary: "List this project's shell records",
		Usage:   "sidecar shell list [--json]",
		Long: "List Sidecar-managed shell records for the current project. Live records\n" +
			"are listed first, then forgotten ones, so either surface can restore a record\n" +
			"by tmux name.\n\n" +
			"This reads shells.json directly; it does not start or inspect tmux sessions.",
		Flags: []Flag{
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--shell", Arg: "NAME", Summary: "Resolve the project from a registered shell"},
			{Name: "--project", Arg: "NAME", Summary: "Target project (slug, basename, or path)"},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 0, Max: 0},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "success"},
			{Code: 1, Summary: "state failure"},
			{Code: 2, Summary: "usage error"},
			{Code: 5, Summary: "an unknown --project or --shell"},
		},
		Examples: []Example{
			{Command: "sidecar shell list"},
			{Command: "sidecar shell list --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar shell list --json",
			Summary:    "See this project's live and forgotten shell records",
		},
		Run: runShellList,
	}

	forgetCmd := &Command{
		Name:    "forget",
		Summary: "Forget a shell record by tmux name",
		Usage:   "sidecar shell forget [--json] <tmux-name>",
		Long: "Forget a Sidecar-managed shell record in the current project. The definition\n" +
			"moves to a tombstone so `sidecar shell restore` can put it back; the tmux\n" +
			"session is not started or killed.\n\n" +
			"A name that is already forgotten is already in that state (exit 0). A name\n" +
			"that is in neither the live list nor the tombstones is not found (exit 1).\n\n" +
			"A forgotten record stays restorable for 14 days by default; set\n" +
			"shells.tombstoneRetention in config.json (\"30d\", \"336h\", or \"forever\") to\n" +
			"change the window.",
		Flags: []Flag{
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--shell", Arg: "NAME", Summary: "Resolve the project from a registered shell"},
			{Name: "--project", Arg: "NAME", Summary: "Target project (slug, basename, or path)"},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "The tmux session name recorded in shells.json"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "forgotten, or already forgotten"},
			{Code: 1, Summary: "not found, or state failure"},
			{Code: 2, Summary: "usage error"},
			{Code: 5, Summary: "an unknown --project or --shell"},
		},
		Examples: []Example{
			{Command: "sidecar shell forget sidecar-sh-sidecar-1"},
			{Command: "sidecar shell forget --json sidecar-sh-sidecar-1"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar shell forget <tmux-name>",
			Summary:    "Drop a shell record from this project without killing tmux",
		},
		Mutates: true,
		Run:     runShellForget,
	}

	deleteCmd := &Command{
		Name:    "delete",
		Summary: "Delete a shell: close its tmux session and forget its record",
		Usage:   "sidecar shell delete --target SESSION [--project NAME] [--json]",
		Long: "Delete a Sidecar-managed shell. This closes the tmux session and moves the\n" +
			"record to a tombstone, which is exactly what Delete does in the Sessions\n" +
			"browser — the same workspaceops call, so the two cannot drift.\n\n" +
			"`sidecar shell forget` is the half of this that only drops the record; use it\n" +
			"for a shell whose session is already gone, or one recorded on another tmux\n" +
			"server. Either way `sidecar shell restore` can put the record back.\n\n" +
			"--target is required and must name a session the resolved project owns: a\n" +
			"sidecar-sh-… record in its shells.json. tmux resolves a session name against\n" +
			"whatever answers to it, so an unregistered name is refused (exit 3) rather\n" +
			"than killed. A sidecar-ws-… worktree session resolves but is refused (exit 5):\n" +
			"removing a checkout carries branch-cleanup decisions this verb cannot express.\n\n" +
			"A sidecar-tp-… target is different: it is a beside-the-session terminal split\n" +
			"(`create shell`'s default placement, or --split), never a managed shell, so it\n" +
			"has no record to tombstone — this is the only CLI path that closes one, and\n" +
			"--shell/--project are refused alongside it since neither applies.\n\n" +
			"There is no current-shell form. Deleting the shell you are sitting in would\n" +
			"kill the session running the command, so the subject is always named.",
		Flags: []Flag{
			{Name: "--target", Arg: "SESSION", Summary: "The tmux session to delete (required)"},
			{Name: "--shell", Arg: "NAME", Summary: "Resolve the project from a registered shell"},
			{Name: "--project", Arg: "NAME", Summary: "Target project (slug, basename, or path)"},
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 0, Max: 0},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "deleted"},
			{Code: 1, Summary: "tmux, ambiguity, or state failure"},
			{Code: 2, Summary: "usage error, including a missing --target"},
			{Code: 3, Summary: "--target names no session this project owns (or, for a sidecar-tp-… split, no live session by that name), or one recorded on a different tmux server"},
			{Code: 5, Summary: "a value was rejected: --target names a worktree, or an unknown --project / --shell"},
		},
		Examples: []Example{
			{Command: "sidecar shell delete --target sidecar-sh-sidecar-2"},
			{Command: "sidecar shell delete --target sidecar-sh-sidecar-2 --project sidecar --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar shell delete --target SESSION",
			Summary:    "Close a Sidecar shell and forget it, the way the user's Delete does",
		},
		Mutates: true,
		Run:     runShellDelete,
	}

	restoreCmd := &Command{
		Name:    "restore",
		Summary: "Restore a forgotten shell record by tmux name",
		Usage:   "sidecar shell restore [--json] <tmux-name>",
		Long: "Restore a forgotten Sidecar-managed shell record in the current project.\n" +
			"Display name, agent type, skip-perms, and working directory come back with it.\n" +
			"The tmux session is not started.\n\n" +
			"A name that is still live is already in that state (exit 0). A name that is in\n" +
			"neither the live list nor the tombstones is not found (exit 1) — including a\n" +
			"record whose retention window (shells.tombstoneRetention, 14 days by default)\n" +
			"has passed.",
		Flags: []Flag{
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--shell", Arg: "NAME", Summary: "Resolve the project from a registered shell"},
			{Name: "--project", Arg: "NAME", Summary: "Target project (slug, basename, or path)"},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "The tmux session name recorded in shells.json"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "restored, or already live"},
			{Code: 1, Summary: "not found, or state failure"},
			{Code: 2, Summary: "usage error"},
			{Code: 5, Summary: "an unknown --project or --shell"},
		},
		Examples: []Example{
			{Command: "sidecar shell restore sidecar-sh-sidecar-1"},
			{Command: "sidecar shell restore --json sidecar-sh-sidecar-1"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar shell restore <tmux-name>",
			Summary:    "Put a forgotten shell record back so it can be recreated",
		},
		Mutates: true,
		Run:     runShellRestore,
	}

	shellCmd := &Command{
		Name:    "shell",
		Summary: "Manage Sidecar shell records and the current shell's name",
		Usage:   "sidecar shell <command>",
		Long:    "List, forget, restore, and delete this project's shells; read or rename a shell; and send a command into one.",
		Sub:     []*Command{deleteCmd, forgetCmd, listCmd, nameCmd, renameCmd, restoreCmd, sendCmd},
		Run:     runShellRoot,
	}

	createShellCmd := &Command{
		Name:    "shell",
		Summary: "Create a Sidecar-managed workspace shell",
		Usage:   "sidecar create shell [options]",
		Long: "Create a new Sidecar-managed shell in the resolved project's workspace.\n" +
			"The shell is recorded in shells.json so it appears in Sidecar whether or not\n" +
			"an instance is running. --run executes a command in the new shell; --type types\n" +
			"it without pressing Enter so the user can review it.\n\n" +
			"From inside a Sidecar shell the default placement is a live terminal beside\n" +
			"the current shell. --tab places the shell in the workspace instead, switching\n" +
			"to a completely new surface; --split auto|right|below picks the side of the\n" +
			"beside-the-session split explicitly (the workspace_terminal_panel feature,\n" +
			"on by default, must not be disabled). Beside-the-session modes need a running\n" +
			"instance and a current shell (SIDECAR_SHELL / --shell) and do not add a\n" +
			"workspace row: the result's session is a sidecar-tp-… terminal split, not a\n" +
			"managed shell, so it is invisible to `shell list`, refused by the agent verbs,\n" +
			"and closable only with `shell delete --target` (nothing else names it).\n" +
			"Handing a shell to a second agent (coordinate-agents) needs a workspace row, so\n" +
			"use --tab rather than the default placement.\n\n" +
			"--agent records which agent family the shell is for, in the same durable field\n" +
			"the TUI's Create Shell writes. That record is what keeps the shell on the\n" +
			"Activity board while the agent is booting and whenever live screen\n" +
			"identification misses a frame. With the agent_control feature enabled and no\n" +
			"--run or --type of your own, --agent also starts the provider and returns only\n" +
			"when it is ready; otherwise it records the family and starts nothing, and --run\n" +
			"(or `sidecar shell send --run` afterwards) owns the launch. Because only a\n" +
			"workspace row carries the field, --agent places the shell there: it is refused\n" +
			"with --split and overrides the beside-the-session default.\n\n" +
			"Provider arguments go after `--`, as `agent start` takes them: `--agent claude\n" +
			"-- --model fable` starts the family's command with those arguments appended and\n" +
			"still records the family. They need --agent and they need agent_control, since\n" +
			"they describe a launch this command performs. They belong to --agent's launch,\n" +
			"so with --run or --type they are refused: put them in your own command instead.\n" +
			"A configured launch override (.sidecar-agent-start, plugins.workspace.agentStart)\n" +
			"takes them appended, unless it contains shell syntax such as a pipe, in which\n" +
			"case the start is refused and names the command to put them in.\n\n" +
			"Usage refusals with --json are `{\"error\":{\"code\":\"usage\",...}}` on stderr,\n" +
			"like the agent verbs; without --json they are the reason and the help text.\n" +
			"The result carries `project`, the slug every other verb's --project accepts.",
		Flags: []Flag{
			{Name: "--name", Arg: "NAME", Summary: "Display name (default: the next Shell N)"},
			{Name: "--agent", Arg: "TYPE", Summary: "Record the agent family (claude, codex, …), and start it when agent_control is on"},
			{Name: "--", Arg: "ARGS", Summary: "Provider arguments appended to --agent's launch command"},
			{Name: "--skip-permissions", Summary: "Pass the selected provider's auto-approve flag", Bool: true},
			{Name: "--run", Arg: "COMMAND", Summary: "Execute COMMAND in the new shell"},
			{Name: "--type", Arg: "COMMAND", Summary: "Type COMMAND without pressing Enter"},
			{Name: "--shell", Arg: "NAME", Summary: "Resolve the project from a registered shell"},
			{Name: "--project", Arg: "NAME", Summary: "Target project (slug, basename, or path; or a worktree it created, by path or basename)"},
			{Name: "--split", Arg: "auto|right|below", Summary: "Place a live terminal beside the current shell"},
			{Name: "--tab", Summary: "Open as a workspace shell instead of beside this session", Bool: true},
			{Name: "--wait", Arg: "DURATION", Summary: "Time to wait for instances to acknowledge (default 1200ms; 0 = fire and forget)"},
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 0, Max: 0},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "created (missing ack is non-fatal in workspace-shell mode)"},
			{Code: 1, Summary: "state or tmux failure"},
			{Code: 2, Summary: "usage error, or this directory is not in a registered project"},
			{Code: 3, Summary: "no running instance (split mode)"},
			{Code: 4, Summary: "instance declined (cap, too small, or feature off)"},
			{Code: 5, Summary: "a value was rejected: --name, --agent, an unknown --project / --shell, or provider arguments with agent_control off"},
		},
		Examples: []Example{
			{Command: "sidecar create shell --name reviewer --agent codex --json"},
			{Command: "sidecar create shell --name \"dev server\" --run \"python3 -m http.server\""},
			{Command: "sidecar create shell --agent claude --run claude", Description: "an agent shell the board knows is one"},
			{Command: "sidecar create shell --tab --name orchestrator --agent claude -- --model fable", Description: "the catalog command with provider arguments"},
			{Command: "sidecar create shell --split right --run \"python3 -m http.server 8765\""},
			{Command: "sidecar create shell --json --wait 0"},
			{Command: "sidecar create shell --type \"go test ./...\"", Description: "type a command for the user to review"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar create shell [--name NAME] [--agent TYPE [-- ARGS...]] [--run COMMAND | --type COMMAND] [--split auto|right|below | --tab]",
			Summary:    "Create a shell beside the current session (default) or as a workspace tab (--tab)",
		},
		Mutates: true,
		Run:     runCreateShell,
	}

	createWorktreeCmd := &Command{
		Name:    "worktree",
		Summary: "Create a Sidecar-managed git worktree",
		Usage:   "sidecar create worktree [options] <name> [-- ARGS...]",
		Long: "Create a git worktree with the same setup pipeline as the TUI create modal:\n" +
			"plan, add, pending-creation journal, identity, and configured hook/env-file rules.\n" +
			"--agent records the agent family on the worktree and, with agent_control on,\n" +
			"starts it in the worktree session (sidecar-ws-…) and returns when it is ready.\n" +
			"Provider arguments go after `--`, as `agent start` takes them: `--agent claude\n" +
			"-- --model fable` appends them to the family's command. --run COMMAND launches\n" +
			"the session with your own command instead; given with --agent it still records\n" +
			"the family and starts nothing else, the layering `create shell` has. --no-launch\n" +
			"skips the launch after the worktree and setup still complete.\n\n" +
			"The name comes before `--`. Because `--` also ends flag parsing, a name that\n" +
			"starts with a dash may stand alone after it (`create worktree -- -fix`), but\n" +
			"once provider arguments follow, or the lone value looks like a flag, the command\n" +
			"is refused rather than guessing which value was meant as the name.\n\n" +
			"--plan resolves the same plan and prints it without changing anything: no\n" +
			"worktree is added, no directory is created, no journal is written. It answers\n" +
			"the questions a confirmation has to ask — branch, path, source ref and OID,\n" +
			"remote policy, and whether a setup hook will run — while every validation\n" +
			"failure (an existing branch, an occupied path, an unsafe hook) still surfaces\n" +
			"as exit 5. --run and --no-launch describe a launch --plan never performs, so\n" +
			"they are refused with it; --agent and --skip-permissions are kept, since they\n" +
			"come back as plan fields.\n\n" +
			"--expect-source-oid OID pins a previously confirmed plan: if the base ref no\n" +
			"longer resolves to OID when this command runs, it is refused with exit 5 and a\n" +
			"message naming both commits. A caller that showed a --plan result in a\n" +
			"confirmation passes the plan's sourceOid back here, and gets the same\n" +
			"source-moved guard the TUI's confirmation gets from executing its stored plan.\n\n" +
			"The result carries `project`, the slug the agent verbs' --project accepts, and\n" +
			"those verbs also accept the worktree's path or basename as --project. Usage\n" +
			"refusals with --json are `{\"error\":{\"code\":\"usage\",...}}` on stderr, like\n" +
			"the agent verbs; without --json they are the reason and the help text.",
		Flags: []Flag{
			{Name: "--base", Arg: "REF", Summary: "Base ref (default HEAD)"},
			{Name: "--plan", Summary: "Resolve and print the plan without creating anything", Bool: true},
			{Name: "--expect-source-oid", Arg: "OID", Summary: "Refuse (exit 5) if the base ref no longer resolves to this commit"},
			{Name: "--agent", Arg: "TYPE", Summary: "Record the agent family and, with agent_control on and no --run, start it in the worktree session"},
			{Name: "--", Arg: "ARGS", Summary: "Provider arguments appended to --agent's launch command"},
			{Name: "--skip-permissions", Summary: "Pass the agent's auto-approve flag", Bool: true},
			{Name: "--run", Arg: "COMMAND", Summary: "Execute COMMAND in the new worktree session"},
			{Name: "--no-launch", Summary: "Create the worktree without launching a session", Bool: true},
			{Name: "--shell", Arg: "NAME", Summary: "Resolve the project from a registered shell"},
			{Name: "--project", Arg: "NAME", Summary: "Target project (slug, basename, or path; or a worktree it created, by path or basename)"},
			{Name: "--wait", Arg: "DURATION", Summary: "Time to wait for instances to acknowledge (default 1200ms; 0 = fire and forget)"},
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "Worktree display name (also the branch slug)"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "created (missing ack is non-fatal), or plan resolved with --plan"},
			{Code: 1, Summary: "git, setup, or tmux failure"},
			{Code: 2, Summary: "usage error (an unknown flag, a refused flag combination)"},
			{Code: 5, Summary: "a value was rejected: the plan (branch exists, path occupied, unknown base ref, unsafe hook), an unknown --project / --shell, or the source moved past --expect-source-oid"},
		},
		Examples: []Example{
			{Command: "sidecar create worktree fix-auth --base main --agent claude"},
			{Command: "sidecar create worktree orchestrate --agent claude --json -- --model fable", Description: "the catalog command with provider arguments; the family is recorded"},
			{Command: "sidecar create worktree scratch --no-launch --json"},
			{Command: "sidecar create worktree fix-auth --base main --plan --json", Description: "what would be created, without creating it"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar create worktree <name> [--base REF] [--agent TYPE [-- ARGS...] | --run COMMAND] [--no-launch | --plan]",
			Summary:    "Create a Sidecar-visible git worktree with the same setup as the TUI",
		},
		Mutates: true,
		Run:     runCreateWorktree,
	}

	createCmd := &Command{
		Name:    "create",
		Summary: "Create a Sidecar-managed shell or worktree",
		Usage:   "sidecar create <command>",
		Long:    "Create Sidecar-owned shells and worktrees so they appear in the workspace.",
		Sub:     []*Command{createShellCmd, createWorktreeCmd},
		Run:     runCreateRoot,
	}

	openCmd := &Command{
		Name:    "open",
		Summary: "Show a file, a td issue, a note, a git diff, a plugin resource, or a plugin collection in a split pane",
		Usage:   "sidecar open [options] [<target>]",
		Long: "Show a file, a td issue, a td note, a git diff, an external resource, or a plugin collection to the user as a\n" +
			"split pane in a Sidecar workspace. From a Sidecar shell this targets that shell.\n" +
			"Otherwise it targets the unique running instance, or a specific --shell / --project.\n" +
			"--sessions addresses the global Sessions surface of a running instance.\n" +
			"Pass --sessions=ROW for a durable inventory ID or display name; a following\n" +
			"bare word is the open target, not the row. Mutually exclusive with --shell\n" +
			"and --project.\n" +
			"--diff with no spec is the working tree. --plugin names a configured plugin instance:\n" +
			"with --collection it opens that collection's tab (add --query to open it searched,\n" +
			"--filter id=value for one of the collection's own declared filters, or a\n" +
			"positional row id to open that row's document instead — a row takes --filter too,\n" +
			"because that is the scope the row is expanded under), and without --collection it\n" +
			"opens a matched locator through the plugin's matchers. --provider is the older spelling\n" +
			"of the locator form and still works. Either way the instance is required for a resource:\n" +
			"a bare locator is never guessed at.\n" +
			"--split only overrides the split axis; it never halves a live terminal after content is open.\n" +
			"--at places the pane at an explicit grid cell and is a requirement: a kind whose open\n" +
			"would retarget an existing pane, or any cell that cannot be honored exactly, declines\n" +
			"rather than land elsewhere (--split expresses a preference; --at, a demand).\n\n" +
			"From a Sidecar-managed pane whose geometry lease is held by a connected viewer,\n" +
			"the open lands on that viewer's screen — not on a host TUI that may not be running.\n" +
			"There is no --host flag: routing is the lease. A relayed open never queues: if that\n" +
			"row is not on the viewer's screen, or the lease holder cannot receive pane requests\n" +
			"(disconnected, too old, or presence expired), the command declines (exit 4).",
		Targets: []TargetDoc{
			{Target: "path", Summary: "A file inside the target workspace, optionally \"path:line\""},
			{Target: "td-xxxxxx", Summary: "A td issue id"},
			{Target: "sidecar://note/nt-xxxx", Summary: "A td note, opened as a read-only pane"},
			{Target: "--diff", Summary: "Working-tree diff (wt); add a spec for a commit or range"},
			{Target: "spec", Summary: "A git commit or range (abc1234, A..B); --diff accepts HEAD and branch names"},
			{Target: "locator", Summary: "With --plugin (or --provider), a resource key such as CASH-1245"},
			{Target: "row id", Summary: "With --plugin and --collection, one row of that collection"},
		},
		Flags: []Flag{
			{Name: "--line", Arg: "N", Summary: "Line to reveal (alternative to \"path:line\")"},
			{Name: "--diff", Summary: "Open a Diff leaf (working tree if no spec)", Bool: true},
			{Name: "--plugin", Arg: "ID", Summary: "Open through a configured plugin instance (collection tab with --collection, otherwise a matched locator)"},
			{Name: "--provider", Arg: "ID", Summary: "Alias for --plugin's locator form, kept for the frozen resource protocol"},
			{Name: "--collection", Arg: "C", Summary: "With --plugin, the collection to open as a tab"},
			{Name: "--query", Arg: "Q", Summary: "With --collection, the query the tab opens searched on"},
			{Name: "--filter", Arg: "ID=VALUE", Summary: "With --collection, one applied filter (repeatable)"},
			{Name: "--shell", Arg: "NAME", Summary: "Target a registered shell by display name or tmux name"},
			{Name: "--project", Arg: "NAME", Summary: "Target a project's Workspaces surface (slug, basename, or path)"},
			{Name: "--sessions", Arg: "[=ROW]", Summary: "Target the global Sessions surface (optional row as --sessions=ID)"},
			{Name: "--split", Arg: "auto|right|below", Summary: "Where to place a new pane (default auto)"},
			{Name: "--at", Arg: "COL[.ROW]", Summary: "Place at an explicit grid cell (1-based); a requirement, mutually exclusive with --split"},
			{Name: "--wait", Arg: "DURATION", Summary: "Time to wait for instances to acknowledge (default 1200ms; 0 = fire and forget)"},
			{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 0, Max: 1, Description: "File, td-xxxxxx, sidecar://note/nt-xxxx, or git spec; omitted with --diff for the working tree"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "opened or queued"},
			{Code: 1, Summary: "state failure"},
			{Code: 2, Summary: "usage or validation error"},
			{Code: 3, Summary: "no running instance, or several running with no target"},
			{Code: 4, Summary: "an instance declined (too small to split, row not on screen, or the lease holder cannot receive pane requests)"},
			{Code: 5, Summary: "an unknown --project or --shell"},
		},
		Examples: []Example{
			{Command: "sidecar open internal/cli/cli.go", Description: "file, in a split beside the terminal"},
			{Command: "sidecar open internal/cli/cli.go:88", Description: "file at a line"},
			{Command: "sidecar open td-348d88", Description: "td issue"},
			{Command: "sidecar open sidecar://note/nt-4jdj4e", Description: "td note pane"},
			{Command: "sidecar open --diff", Description: "working-tree Diff leaf"},
			{Command: "sidecar open --diff HEAD", Description: "that commit, not the working tree"},
			{Command: "sidecar open abc1234", Description: "commit, unless a file of that name exists"},
			{Command: "sidecar open --provider jira-work CASH-1245", Description: "resource pane for that provider's locator"},
			{Command: "sidecar open --plugin recall --collection results --query dex --split right", Description: "a collection tab beside the terminal, opened searched"},
			{Command: "sidecar open --plugin recall --collection results --query dex --filter profile=docs", Description: "the same tab, scoped by one of the collection's own filters"},
			{Command: "sidecar open --plugin ongoing --collection projects recall", Description: "one row's document tab"},
			{Command: "sidecar open --json --split below README.md", Description: "structured result for the agent"},
			{Command: "sidecar open README.md --at 2.1", Description: "explicit cell: second column, top row"},
			{Command: "sidecar open --project sidecar README.md", Description: "from any terminal, that project's Workspaces surface"},
			{Command: "sidecar open --sessions README.md", Description: "the selected row on the global Sessions surface"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar open <path>[:line] | td-xxxxxx | sidecar://note/nt-xxxx | --diff [spec] | --plugin ID [--collection C [--query Q] [--filter ID=VALUE]...] [<locator-or-row>] [--split right|below] [--at COL[.ROW]]",
			Summary:    "Put a file, issue, note, diff, resource, or plugin collection in front of the user on the lease holder's screen",
		},
		Mutates: true,
		Run:     runOpen,
	}

	agentsCmd := &Command{
		Name:    "agents",
		Summary: "List what an agent can do from Sidecar",
		Usage:   "sidecar agents",
		Long: "List the Sidecar commands worth reaching for, one line each.\n" +
			"Also available as \"sidecar --agents\" and \"sidecar -a\".",
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "success"},
		},
		Examples: []Example{
			{Command: "sidecar agents"},
			{Command: "sidecar --agents"},
			{Command: "sidecar -a"},
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

	root.Sub = []*Command{agentCommand(), agentsCmd, contentCommand(), createCmd, helpCmd, hostCommand(), layoutCommand(), notifyCommand(), openCmd, pluginCommand(), projectCommand(), repoCommand(), requestCommand(), sessionCommand(), setupCmd, shellCmd, terminalLinksCommand(), worktreeCommand()}
	return root
}

func worktreeCommand() *Command {
	deleteCmd := &Command{
		Name:    "delete",
		Summary: "Plan or delete a Sidecar-visible git worktree",
		Usage:   "sidecar worktree delete <name|branch|path> [--project NAME] [--plan|--dry-run | --yes] [options]",
		Long: "Delete a git worktree through the same lifecycle the TUI's confirmation uses.\n" +
			"Sidecar first resolves the target from the owning project's live `git worktree`\n" +
			"inventory and applies the shared refusal rules: the main, bare, detached, locked,\n" +
			"missing, and prunable worktrees cannot be deleted. The target may be its Sidecar\n" +
			"display name, checked-out branch, path basename, absolute path, or sidecar-ws-…\n" +
			"session name. Use --project with the project slug returned by `create worktree`,\n" +
			"or run from the project or one of its worktrees.\n\n" +
			"--plan and --dry-run are aliases. They read the same confirmation facts without\n" +
			"changing git, tmux, Sidecar state, or a pending-creation journal: target identity,\n" +
			"dirtiness, remote-branch availability, cleanup choices, and the pinned HEAD OID.\n" +
			"A real deletion always requires --yes. For a plan-first deletion, use the returned\n" +
			"absolute path as TARGET and pass its branch and headOid back with --expect-branch\n" +
			"and --expect-head-oid. Both expectations are required together, so a branch rename\n" +
			"at the same commit is refused rather than mistaken for the confirmed checkout.\n\n" +
			"Deleting closes the Sidecar worktree session and any managed shells rooted in the\n" +
			"worktree before removing its directory, then forgets those shell records. A dirty\n" +
			"worktree is force-removed only after --yes, matching the warning and decision in\n" +
			"the TUI confirmation. --delete-local-branch and --delete-remote-branch are explicit\n" +
			"counterparts to its unchecked branch-cleanup boxes; the default is to keep both.\n" +
			"If this was a failed `create worktree`, its exact pending-creation journal is cleared\n" +
			"only after the worktree removal succeeds. Branch and journal cleanup failures are\n" +
			"reported as warnings after the primary deletion succeeds, as is a rooted shell that\n" +
			"could not be closed; those warnings do not skip the remaining requested cleanup.",
		Flags: []Flag{
			{Name: "--project", Arg: "NAME", Summary: "Owning project (slug, basename, or path)"},
			{Name: "--plan", Summary: "Resolve and print the deletion plan without changing anything", Bool: true},
			{Name: "--dry-run", Summary: "Alias for --plan", Bool: true},
			{Name: "--yes", Summary: "Confirm worktree removal (required unless planning)", Bool: true},
			{Name: "--delete-local-branch", Summary: "Also delete the checked-out local branch", Bool: true},
			{Name: "--delete-remote-branch", Summary: "Also delete the branch from origin when it exists", Bool: true},
			{Name: "--expect-branch", Arg: "BRANCH", Summary: "Refuse if the absolute target no longer checks out this planned branch"},
			{Name: "--expect-head-oid", Arg: "OID", Summary: "Refuse if HEAD differs from a previously returned plan"},
			{Name: "--json", Summary: "Write one structured plan or result object to stdout", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "Worktree display name, branch, path, or Sidecar session name"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "plan resolved, or worktree deleted (cleanup warnings may be present)"},
			{Code: 1, Summary: "git, tmux, Sidecar state, or primary deletion failure"},
			{Code: 2, Summary: "usage error, including a deletion without --yes"},
			{Code: 5, Summary: "project, target, target state, branch choice, or expected branch/HEAD was refused"},
		},
		Examples: []Example{
			{Command: "sidecar worktree delete fix-auth --project sidecar --plan --json", Description: "inspect the exact target and copy its path, branch, and headOid"},
			{Command: "sidecar worktree delete /path/to/repo-fix-auth --project sidecar --expect-branch fix-auth --expect-head-oid abc123 --yes --json", Description: "delete only if the confirmed checkout identity has not moved"},
			{Command: "sidecar worktree delete /path/to/repo-fix-auth --delete-local-branch --yes"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar worktree delete TARGET --plan --json; then use its path with --expect-branch BRANCH --expect-head-oid OID --yes",
			Summary:    "Inspect, then delete a worktree through Sidecar's full teardown lifecycle",
		},
		Mutates: true,
		Run:     runWorktreeDelete,
	}
	return &Command{
		Name:    "worktree",
		Summary: "Manage Sidecar-visible git worktrees",
		Usage:   "sidecar worktree <command>",
		Long:    "Plan and perform worktree lifecycle operations through Sidecar's shared core.",
		Sub:     []*Command{deleteCmd},
		Run:     runWorktreeRoot,
	}
}

// notifyCommand is the agent-facing side of the notification system: post an
// alert the user sees as a toast and in the notification centre, dismiss one
// you posted, and read the log back.
//
// Listing reads the log directly, so it answers with no Sidecar running.
// Posting prefers a running instance (the user sees it immediately) and falls
// back to writing the log, so nothing is lost either way.
func notifyCommand() *Command {
	configSetCmd := &Command{
		Name:    "set",
		Summary: "Set notification delivery, quiet hours, custom sounds, and SSH delivery",
		Long:    "Set one or more global notification settings. Modes are off, background, or always. Quiet hours are off or a local wall-clock range such as 22:00-08:00. Custom sound paths may be absolute, start with ~, or be relative to config.json; an empty --*-path= restores the built-in cue. SSH delivery has two independent switches, both off by default: --ssh-managed-hosts lets this machine deliver notifications forwarded by a registered remote host, and --ssh-terminal picks the outer terminal to notify through when Sidecar itself runs inside an SSH session. The complete prospective configuration is validated before write, preserves unrelated rules, and applies to running Sidecar instances without restart.",
		Usage:   "sidecar notify config set [options]",
		Flags: []Flag{
			{Name: "--native", Arg: "MODE", Summary: "Set system notifications: off, background, or always"},
			{Name: "--sound", Arg: "MODE", Summary: "Set sounds: off, background, or always"},
			{Name: "--quiet-hours", Arg: "RANGE", Summary: "Set off or local HH:MM-HH:MM (equal times mean all day)"},
			{Name: "--attention-path", Arg: "PATH", Summary: "Set the attention cue file; empty restores built-in"},
			{Name: "--done-path", Arg: "PATH", Summary: "Set the done cue file; empty restores built-in"},
			{Name: "--failure-path", Arg: "PATH", Summary: "Set the failure cue file; empty restores built-in"},
			{Name: "--ssh-managed-hosts", Arg: "on|off", Summary: "Deliver notifications forwarded by registered remote hosts"},
			{Name: "--ssh-terminal", Arg: "TERMINAL", Summary: "Set off, auto, ghostty, iterm2, wezterm, or kitty"},
			{Name: "--json", Summary: "Write the resulting notification configuration as JSON", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		ExitCodes: []ExitCode{{Code: 0, Summary: "saved"}, {Code: 1, Summary: "configuration I/O failure"}, {Code: 2, Summary: "usage or validation error"}},
		Examples: []Example{
			{Command: "sidecar notify config set --native background --sound background"},
			{Command: "sidecar notify config set --quiet-hours 22:00-08:00 --json"},
			{Command: "sidecar notify config set --attention-path ~/Sounds/attention.wav"},
			{Command: "sidecar notify config set --ssh-managed-hosts on --json"},
			{Command: "sidecar notify config set --ssh-terminal ghostty"},
		},
		Agent:   AgentDoc{Invocation: "sidecar notify config set [--native MODE] [--sound MODE] [--quiet-hours RANGE] [--ssh-managed-hosts on|off] [--ssh-terminal TERMINAL] --json", Summary: "Change external notification settings without restarting Sidecar"},
		Mutates: true,
		Run:     runNotifyConfigSet,
	}
	configCmd := &Command{
		Name:    "config",
		Summary: "Show or change notification delivery configuration",
		Usage:   "sidecar notify config [--json]",
		Long:    "Print resolved notification settings and defaults without changing the file. Use config set for global modes, quiet hours, and custom sound paths; use source set for per-source rules.",
		Flags:   []Flag{{Name: "--json", Summary: "Write notification configuration as JSON", Bool: true}, {Name: "--help", Short: "-h", Summary: "Show this help", Bool: true}},
		Sub:     []*Command{configSetCmd},
		Agent:   AgentDoc{Invocation: "sidecar notify config --json", Summary: "Inspect external notification modes and rules"},
		Run:     runNotifyConfig,
	}
	statusCmd := &Command{
		Name:      "status",
		Summary:   "Probe native and sound provider availability",
		Usage:     "sidecar notify status [--json]",
		Long:      "Probe providers without sending a notification or changing configuration.",
		Flags:     []Flag{{Name: "--json", Summary: "Write provider capabilities as JSON", Bool: true}, {Name: "--help", Short: "-h", Summary: "Show this help", Bool: true}},
		ExitCodes: []ExitCode{{Code: 0, Summary: "probe completed"}, {Code: 1, Summary: "output failure"}, {Code: 2, Summary: "usage error"}},
		Examples:  []Example{{Command: "sidecar notify status --json"}},
		Agent:     AgentDoc{Invocation: "sidecar notify status --json", Summary: "Check native notification and sound providers without sending anything"},
		Run:       runNotifyStatus,
	}
	testCmd := &Command{
		Name:    "test",
		Summary: "Explicitly test enabled notification channels",
		Usage:   "sidecar notify test --channel native|sound|all [--event waiting|done|failure] [--source SOURCE] [--json]",
		Long:    "Exercise enabled providers without creating a notification-centre record. Explicit tests bypass foreground and quiet-hours suppression but still honor disabled channels and unavailable providers.",
		Flags: []Flag{
			{Name: "--channel", Arg: "CHANNEL", Summary: "Test native, sound, or all (required)"},
			{Name: "--event", Arg: "EVENT", Summary: "Use waiting, done, or failure (default waiting)"},
			{Name: "--source", Arg: "SOURCE", Summary: "Test the selected registered source rule (default follows event)"},
			{Name: "--json", Summary: "Write per-channel attempted/provider/delivered/error results", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		ExitCodes: []ExitCode{{Code: 0, Summary: "requested channels delivered"}, {Code: 1, Summary: "provider or output failure"}, {Code: 2, Summary: "usage error"}, {Code: 3, Summary: "a requested channel was disabled or unavailable"}},
		Examples: []Example{
			{Command: "sidecar notify test --channel all --event waiting --json"},
			{Command: "sidecar notify test --channel native --source td --json"},
		},
		Agent: AgentDoc{Invocation: "sidecar notify test --channel native|sound|all [--event EVENT] [--source SOURCE] --json", Summary: "Explicitly test enabled providers without filing a centre notification"},
		Run:   runNotifyTest,
	}
	sourceSetCmd := &Command{
		Name:    "set",
		Summary: "Set one notification source rule",
		Usage:   "sidecar notify source set <source> [options]",
		Long:    "Change one or more fields for a registered notification source. The same validation and targeted save boundary as Configuration preserves unrelated root keys and unknown future source entries, and the running app reloads the result without restart.",
		Flags: []Flag{
			{Name: "--toast", Arg: "on|off", Summary: "Enable or disable the in-app toast"},
			{Name: "--native", Arg: "on|off", Summary: "Enable or disable system notifications for this source"},
			{Name: "--sound", Arg: "CUE", Summary: "Set none, event, attention, done, or failure"},
			{Name: "--expiry", Arg: "DURATION", Summary: "Set a Go duration or sticky"},
			{Name: "--json", Summary: "Write the resulting resolved source rule as JSON", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		Args:      ArgSpec{Min: 1, Max: 1, Description: "Registered source: agent, waiting, session, tasks, td, or system"},
		ExitCodes: []ExitCode{{Code: 0, Summary: "saved"}, {Code: 1, Summary: "configuration I/O failure"}, {Code: 2, Summary: "usage or validation error"}},
		Examples: []Example{
			{Command: "sidecar notify source set waiting --toast on --native on --sound attention --expiry sticky"},
			{Command: "sidecar notify source set td --native on --json"},
		},
		Agent: AgentDoc{Invocation: "sidecar notify source set <source> [--toast on|off] [--native on|off] [--sound CUE] [--expiry DURATION] --json", Summary: "Change one notification source rule without restarting Sidecar"},
		Run:   runNotifySourceSet,
	}
	sourceCmd := &Command{
		Name:    "source",
		Summary: "Configure per-source notification rules",
		Usage:   "sidecar notify source <command>",
		Long:    "Inspect resolved rules with notify config; use source set for deterministic non-interactive mutation.",
		Sub:     []*Command{sourceSetCmd},
		Run:     runNotifySourceRoot,
	}
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
			"text does not spell out. A session target attaches a Sidecar-owned tmux\n" +
			"session — the sidecar-sh-… and sidecar-ws-… names shells and worktree agents\n" +
			"run under, which are also the only ones found by scanning. A task target opens\n" +
			"the Tasks tab.",
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
		Mutates: true,
		Run:     runNotifyPost,
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
		Mutates: true,
		Run:     runNotifyDismiss,
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
		Summary: "Configure, test, post, dismiss, and list Sidecar notifications",
		Usage:   "sidecar notify <command>",
		Long: "Sidecar's notification surface: a toast in the running instance, an entry in the\n" +
			"notification centre, and a count in the header until the user reads it.",
		Sub: []*Command{configCmd, dismissCmd, listCmd, postCmd, sourceCmd, statusCmd, testCmd},
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
	stateDir := config.StateDir()
	return Env{
		Stdout:               stdout,
		Stderr:               stderr,
		Stdin:                os.Stdin,
		StateDir:             stateDir,
		Ctx:                  context.Background(),
		NotificationDelivery: notifydelivery.NewDefault(stateDir),
	}
}
