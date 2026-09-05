package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/shellstate"
	"github.com/marcus/sidecar/internal/uirequest"
)

type projectJSONItem struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Key    string `json:"key"`
	Theme  string `json:"theme,omitempty"`
	OpenIn string `json:"openIn,omitempty"`
	// AddedAt is when the project was registered with Sidecar, absent for a
	// project registered before Sidecar recorded it. It is a registration date,
	// not a creation date, and it is reported rather than computed: an agent
	// reading this gets the same fact the switcher's "Date added" column shows.
	AddedAt string `json:"addedAt,omitempty"`
}

type projectCurrentJSON struct {
	Shell   *projectJSONItem `json:"shell,omitempty"`
	Visible *projectJSONItem `json:"visible,omitempty"`
	Aligned bool             `json:"aligned"`
}

type projectListJSON struct {
	Projects []projectJSONItem `json:"projects"`
	Shell    *projectJSONItem  `json:"shell,omitempty"`
	Visible  *projectJSONItem  `json:"visible,omitempty"`
	Aligned  bool              `json:"aligned"`
}

type projectAddJSON struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Key      string `json:"key"`
	Theme    string `json:"theme,omitempty"`
	OpenIn   string `json:"openIn,omitempty"`
	Switched bool   `json:"switched"`
}

type projectSwitchJSON struct {
	Project  string `json:"project"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Switched bool   `json:"switched"`
}

type projectRemoveJSON struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Removed bool   `json:"removed"`
}

func projectCommand() *Command {
	jsonFlag := Flag{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true}
	helpFlag := Flag{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true}

	currentCmd := &Command{
		Name:    "current",
		Summary: "Print the calling shell's project and the visible project",
		Usage:   "sidecar project current [--json]",
		Long: "Print the Sidecar project owning this shell and the project currently visible in\n" +
			"the running Sidecar TUI.\n\n" +
			"Human output names the shell's project first and mentions the visible one when\n" +
			"it differs. JSON reports both and whether they are aligned.",
		Flags: []Flag{jsonFlag, helpFlag},
		Args:  ArgSpec{Min: 0, Max: 0},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "success (even when no TUI is running)"},
			{Code: 1, Summary: "not a managed shell and current directory is not a configured project"},
			{Code: 2, Summary: "usage error"},
		},
		Examples: []Example{
			{Command: "sidecar project current"},
			{Command: "sidecar project current --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar project current",
			Summary:    "See which project this shell belongs to and what the user is looking at",
		},
		Run: runProjectCurrent,
	}

	listCmd := &Command{
		Name:    "list",
		Summary: "List configured Sidecar projects",
		Usage:   "sidecar project list [--json]",
		Long: "List all Sidecar projects from configuration in list order. Marks the project\n" +
			"owning this shell and the project currently visible in the running Sidecar TUI.\n\n" +
			"This reads configuration directly and does not require a managed shell or a\n" +
			"running Sidecar instance.",
		Flags: []Flag{jsonFlag, helpFlag},
		Args:  ArgSpec{Min: 0, Max: 0},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "success"},
			{Code: 1, Summary: "configuration read failure"},
			{Code: 2, Summary: "usage error"},
		},
		Examples: []Example{
			{Command: "sidecar project list"},
			{Command: "sidecar project list --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar project list --json",
			Summary:    "List configured projects and mark the shell and visible projects",
		},
		Run: runProjectList,
	}

	addCmd := &Command{
		Name:    "add",
		Summary: "Add a new project to Sidecar",
		Usage:   "sidecar project add <name> --path PATH [--theme NAME] [--open-in APP] [--switch] [--json]",
		Long: "Add a project to Sidecar's configuration.\n\n" +
			"The directory specified by --path must already exist and will not be created.\n" +
			"Adding a project does not initialize a Git repository or a td project.\n\n" +
			"With --switch, also switch the running Sidecar TUI to the new project immediately\n" +
			"after writing configuration. If no Sidecar instance is running, add still succeeds.\n" +
			"A landing shell is a separate `sidecar create shell --project` command.",
		Flags: []Flag{
			{Name: "--path", Arg: "PATH", Summary: "Directory path for the project (required)"},
			{Name: "--theme", Arg: "NAME", Summary: "Set a project-specific theme override"},
			{Name: "--open-in", Arg: "APP", Summary: "Set the default editor or IDE to open this project in"},
			{Name: "--switch", Summary: "Switch the running Sidecar TUI to the new project after adding", Bool: true},
			jsonFlag,
			helpFlag,
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "Project display name"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "added (and switched, if requested and Sidecar is running)"},
			{Code: 1, Summary: "configuration I/O failure"},
			{Code: 2, Summary: "usage error (missing name or --path)"},
			{Code: 5, Summary: "a value was rejected (path does not exist, not a directory, or project name/path already exists)"},
		},
		Examples: []Example{
			{Command: "sidecar project add \"vacuum-simulator\" --path ~/code/vacuum-simulator"},
			{Command: "sidecar project add \"vacuum-simulator\" --path ~/code/vacuum-simulator --switch"},
			{Command: "sidecar project add \"vacuum-simulator\" --path ~/code/vacuum-simulator --theme \"Catppuccin Mocha\" --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar project add <name> --path PATH [--switch]",
			Summary:    "Add a project to Sidecar; adding a project does not initialize a Git repository or a td project.",
		},
		Mutates: true,
		Run:     runProjectAdd,
	}

	switchCmd := &Command{
		Name:    "switch",
		Summary: "Switch the running Sidecar TUI to a project",
		Usage:   "sidecar project switch <name> [--wait DURATION] [--json]",
		Long: "Switch the running Sidecar TUI to another configured project.\n\n" +
			"This changes what the user is looking at in the Sidecar window; it does not move\n" +
			"or retarget the calling shell. Unlike `sidecar open`, switch is an intentional\n" +
			"view change that updates visible project context and restores the last active\n" +
			"worktree for that project.",
		Flags: []Flag{
			{Name: "--wait", Arg: "DURATION", Summary: "Time to wait for instance to acknowledge (default 1200ms; 0 = fire and forget)"},
			jsonFlag,
			helpFlag,
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "Target project (name, slug, basename, or path)"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "switched or already showing project"},
			{Code: 1, Summary: "state or communication failure"},
			{Code: 2, Summary: "usage error"},
			{Code: 3, Summary: "no running Sidecar instance, or multiple instances running"},
			{Code: 4, Summary: "running instance declined the switch"},
			{Code: 5, Summary: "unknown project"},
		},
		Examples: []Example{
			{Command: "sidecar project switch vacuum-simulator"},
			{Command: "sidecar project switch \"vacuum-simulator\" --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar project switch <name>",
			Summary:    "Put the user in a configured project by switching the running Sidecar window",
		},
		Mutates: true,
		Run:     runProjectSwitch,
	}

	setCmd := &Command{
		Name:    "set",
		Summary: "Update configuration for an existing project",
		Usage:   "sidecar project set <name> [--name NEW] [--path PATH] [--theme NAME] [--open-in APP] [--clear-theme] [--json]",
		Long: "Change settings for an existing project in Sidecar's configuration. At least one\n" +
			"setting flag is required.\n\n" +
			"Path changes must point to an existing directory and re-validate uniqueness.\n" +
			"Editing a project notifies running Sidecar instances without switching the visible\n" +
			"project.",
		Flags: []Flag{
			{Name: "--name", Arg: "NEW", Summary: "Rename the project"},
			{Name: "--path", Arg: "PATH", Summary: "Change the project directory path"},
			{Name: "--theme", Arg: "NAME", Summary: "Set or change the project theme"},
			{Name: "--clear-theme", Summary: "Remove project theme override (use global theme)", Bool: true},
			{Name: "--open-in", Arg: "APP", Summary: "Set the default editor or IDE to open this project in"},
			jsonFlag,
			helpFlag,
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "Project to edit (name, slug, basename, or path)"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "updated"},
			{Code: 1, Summary: "configuration I/O failure"},
			{Code: 2, Summary: "usage error (no change flags specified or conflicting flags)"},
			{Code: 3, Summary: "ambiguous project name"},
			{Code: 5, Summary: "a value was rejected (unknown project, path does not exist, or name already taken)"},
		},
		Examples: []Example{
			{Command: "sidecar project set \"vacuum-simulator\" --name \"Vacuum Sim\""},
			{Command: "sidecar project set \"vacuum-simulator\" --theme \"Nord\""},
			{Command: "sidecar project set \"vacuum-simulator\" --clear-theme"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar project set <name> [--name NEW] [--path PATH] [--theme NAME] [--open-in APP] [--clear-theme]",
			Summary:    "Change settings or rename a configured project",
		},
		Mutates: true,
		Run:     runProjectSet,
	}

	removeCmd := &Command{
		Name:    "remove",
		Summary: "Remove a project from Sidecar's configuration",
		Usage:   "sidecar project remove <name> --yes [--json]",
		Long: "Remove a project from Sidecar's configuration.\n\n" +
			"--yes is required. Removing a project does not delete the project directory,\n" +
			"its Git repository, or its session history. If the project is currently visible\n" +
			"in a running Sidecar instance, removal is refused; switch to another project\n" +
			"with `sidecar project switch` first.",
		Flags: []Flag{
			{Name: "--yes", Summary: "Confirm removal (required for non-interactive safety)", Bool: true},
			jsonFlag,
			helpFlag,
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "Project to remove (name, slug, basename, or path)"},
		ExitCodes: []ExitCode{
			{Code: 0, Summary: "removed"},
			{Code: 1, Summary: "configuration I/O failure"},
			{Code: 2, Summary: "usage error (missing --yes)"},
			{Code: 3, Summary: "ambiguous project name"},
			{Code: 5, Summary: "a value was rejected (unknown project, or project is currently visible in Sidecar)"},
		},
		Examples: []Example{
			{Command: "sidecar project remove \"vacuum-simulator\" --yes"},
			{Command: "sidecar project remove \"vacuum-simulator\" --yes --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar project remove <name> --yes",
			Summary:    "Remove a project from configuration without deleting files on disk",
		},
		Mutates: true,
		Run:     runProjectRemove,
	}

	return &Command{
		Name:    "project",
		Summary: "Inspect, add, edit, and switch Sidecar projects",
		Usage:   "sidecar project <command>",
		Long: "Manage Sidecar's configured projects and switch between project workspaces.\n\n" +
			"A Sidecar-managed shell is born in a project and cannot change projects for its\n" +
			"lifetime. `sidecar project switch` and `add --switch` change what the running\n" +
			"Sidecar TUI displays to the user; work in the new project occurs in a newly created\n" +
			"shell (`sidecar create shell --project`).",
		Sub: []*Command{addCmd, currentCmd, listCmd, removeCmd, setCmd, switchCmd},
		Run: runProjectRoot,
	}
}

func runProjectRoot(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("project")
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprint(env.Stdout, RenderHelp(cmd))
		return 0
	}
	sub := cmd.FindSubcommand(args[0])
	if sub != nil && sub.Run != nil {
		return sub.Run(env, args[1:])
	}
	cliErrf(env.Stderr, "unknown project command %q\n\n%s", args[0], RenderHelp(cmd))
	return 2
}

func runProjectCurrent(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("project").FindSubcommand("current")
	help := RenderHelp(cmd)

	jsonOutput := false
	for _, arg := range args {
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return 0
		case arg == "--json":
			jsonOutput = true
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
				return 2
			}
			cliErrf(env.Stderr, "project current takes no positional arguments\n\n%s", help)
			return 2
		}
	}

	cfg, err := config.Load()
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	shellProj, err := resolveCallingProject(env, cfg)
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	visibleProj := resolveVisibleProject(env, cfg)
	aligned := false
	if shellProj != nil && visibleProj != nil {
		aligned = canonicalOpenPath(shellProj.Path) == canonicalOpenPath(visibleProj.Path)
	}

	if jsonOutput {
		result := projectCurrentJSON{
			Shell:   shellProj,
			Visible: visibleProj,
			Aligned: aligned,
		}
		if err := json.NewEncoder(env.Stdout).Encode(result); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		return 0
	}

	if _, err := fmt.Fprintf(env.Stdout, "%s  %s\n", shellProj.Name, shortenHomePath(shellProj.Path)); err != nil {
		return 1
	}
	if !aligned && visibleProj != nil {
		if _, err := fmt.Fprintf(env.Stdout, "visible: %s  %s\n", visibleProj.Name, shortenHomePath(visibleProj.Path)); err != nil {
			return 1
		}
	}
	return 0
}

func runProjectList(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("project").FindSubcommand("list")
	help := RenderHelp(cmd)

	jsonOutput := false
	for _, arg := range args {
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return 0
		case arg == "--json":
			jsonOutput = true
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
				return 2
			}
			cliErrf(env.Stderr, "project list takes no positional arguments\n\n%s", help)
			return 2
		}
	}

	cfg, err := config.Load()
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	shellProj, _ := resolveCallingProject(env, cfg)
	visibleProj := resolveVisibleProject(env, cfg)
	aligned := false
	if shellProj != nil && visibleProj != nil {
		aligned = canonicalOpenPath(shellProj.Path) == canonicalOpenPath(visibleProj.Path)
	}

	items := make([]projectJSONItem, 0, len(cfg.Projects.List))
	for _, p := range cfg.Projects.List {
		item := makeProjectJSONItem(env.StateDir, p)
		items = append(items, *item)
	}

	if jsonOutput {
		result := projectListJSON{
			Projects: items,
			Shell:    shellProj,
			Visible:  visibleProj,
			Aligned:  aligned,
		}
		if err := json.NewEncoder(env.Stdout).Encode(result); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		return 0
	}

	if len(cfg.Projects.List) == 0 {
		_, _ = fmt.Fprintln(env.Stdout, "No configured projects.")
		return 0
	}

	nameWidth, pathWidth := 0, 0
	for _, p := range cfg.Projects.List {
		if len(p.Name) > nameWidth {
			nameWidth = len(p.Name)
		}
		short := shortenHomePath(config.ExpandPath(p.Path))
		if len(short) > pathWidth {
			pathWidth = len(short)
		}
	}

	for _, p := range cfg.Projects.List {
		pCanon := canonicalOpenPath(config.ExpandPath(p.Path))
		isShell := shellProj != nil && canonicalOpenPath(shellProj.Path) == pCanon
		isVisible := visibleProj != nil && canonicalOpenPath(visibleProj.Path) == pCanon

		marker := ""
		switch {
		case isShell && isVisible:
			marker = "(shell, visible)"
		case isShell:
			marker = "(shell)"
		case isVisible:
			marker = "(visible)"
		}

		shortPath := shortenHomePath(config.ExpandPath(p.Path))
		if marker != "" {
			_, _ = fmt.Fprintf(env.Stdout, "%-*s  %-*s  %s\n", nameWidth, p.Name, pathWidth, shortPath, marker)
		} else {
			_, _ = fmt.Fprintf(env.Stdout, "%-*s  %s\n", nameWidth, p.Name, shortPath)
		}
	}
	return 0
}

func runProjectAdd(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("project").FindSubcommand("add")
	help := RenderHelp(cmd)

	jsonOutput := false
	switchFlag := false
	pathFlag := ""
	themeFlag := ""
	openInFlag := ""
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return 0
		case arg == "--json":
			jsonOutput = true
		case arg == "--switch":
			switchFlag = true
		case arg == "--path" || strings.HasPrefix(arg, "--path="):
			val, next, ok := takeFlagArg(arg, args, i, "--path")
			if !ok || val == "" {
				cliErrf(env.Stderr, "--path requires a directory path\n\n%s", help)
				return 2
			}
			pathFlag = val
			i = next
		case arg == "--theme" || strings.HasPrefix(arg, "--theme="):
			val, next, ok := takeFlagArg(arg, args, i, "--theme")
			if !ok || val == "" {
				cliErrf(env.Stderr, "--theme requires a theme name\n\n%s", help)
				return 2
			}
			themeFlag = val
			i = next
		case arg == "--open-in" || strings.HasPrefix(arg, "--open-in="):
			val, next, ok := takeFlagArg(arg, args, i, "--open-in")
			if !ok || val == "" {
				cliErrf(env.Stderr, "--open-in requires an application name\n\n%s", help)
				return 2
			}
			openInFlag = val
			i = next
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
				return 2
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		cliErrf(env.Stderr, "project add requires exactly one project name\n\n%s", help)
		return 2
	}
	name := strings.TrimSpace(positional[0])
	if name == "" {
		cliErrf(env.Stderr, "project name is required\n\n%s", help)
		return 2
	}
	if pathFlag == "" {
		cliErrf(env.Stderr, "--path is required\n\n%s", help)
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	expandedPath := config.ExpandPath(pathFlag)
	if validationMsg := config.ValidateProject(cfg.Projects.List, name, expandedPath, -1); validationMsg != "" {
		cliErrln(env.Stderr, validationMsg)
		return exitInputRejected
	}

	var themeCfg *config.ThemeConfig
	if themeFlag != "" {
		themeCfg = &config.ThemeConfig{Name: themeFlag}
	}

	newProj := config.ProjectConfig{
		Name:   name,
		Path:   expandedPath,
		Theme:  themeCfg,
		OpenIn: openInFlag,
	}

	savedProj, err := config.AddProject(newProj)
	if err != nil {
		cliErrln(env.Stderr, err)
		if config.ValidateProject(cfg.Projects.List, name, expandedPath, -1) != "" {
			return exitInputRejected
		}
		return 1
	}

	broadcastConfigReload(env)

	switched := false
	if switchFlag {
		instances, listErr := uirequest.ListInstances(env.StateDir)
		if listErr == nil && len(instances) == 1 {
			req := uirequest.Request{
				ID:     uirequest.NewRequestID(),
				Action: uirequest.ActionSwitchProject,
				Target: uirequest.Target{Kind: "project", Value: savedProj.Path},
				Origin: projectOrigin(env),
			}
			if _, writeErr := uirequest.WriteRequest(env.StateDir, req); writeErr == nil {
				acks := pollProjectSwitchAcks(env.StateDir, req.ID, req.Action, 1200*time.Millisecond)
				if len(acks) > 0 && (acks[0].Status == uirequest.StatusOpened || acks[0].Status == uirequest.StatusUnchanged) {
					switched = true
				}
			}
		}
	}

	themeName := ""
	if savedProj.Theme != nil {
		themeName = savedProj.Theme.Name
	}

	if jsonOutput {
		result := projectAddJSON{
			Name:     savedProj.Name,
			Path:     savedProj.Path,
			Key:      projKey(env.StateDir, savedProj.Path),
			Theme:    themeName,
			OpenIn:   savedProj.OpenIn,
			Switched: switched,
		}
		if err := json.NewEncoder(env.Stdout).Encode(result); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		return 0
	}

	shortPath := shortenHomePath(savedProj.Path)
	if switchFlag {
		if switched {
			_, _ = fmt.Fprintf(env.Stdout, "Added project %q (%s) and switched to it.\n", savedProj.Name, shortPath)
		} else {
			_, _ = fmt.Fprintf(env.Stdout, "Added project %q (%s). No running Sidecar instance to switch to.\n", savedProj.Name, shortPath)
		}
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "Added project %q (%s).\n", savedProj.Name, shortPath)
	}
	return 0
}

func runProjectSwitch(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("project").FindSubcommand("switch")
	help := RenderHelp(cmd)

	jsonOutput := false
	waitDuration := 1200 * time.Millisecond
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return 0
		case arg == "--json":
			jsonOutput = true
		case arg == "--wait" || strings.HasPrefix(arg, "--wait="):
			val, next, ok := takeFlagArg(arg, args, i, "--wait")
			if !ok || val == "" {
				cliErrf(env.Stderr, "--wait requires a duration (e.g. 1200ms)\n\n%s", help)
				return 2
			}
			d, err := time.ParseDuration(val)
			if err != nil {
				cliErrf(env.Stderr, "invalid --wait duration %q: %v\n\n%s", val, err, help)
				return 2
			}
			waitDuration = d
			i = next
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
				return 2
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		cliErrf(env.Stderr, "project switch requires exactly one project name\n\n%s", help)
		return 2
	}
	targetName := positional[0]

	cfg, err := config.Load()
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	proj, code, err := matchConfigProject(env.StateDir, cfg.Projects.List, targetName)
	if err != nil {
		cliErrln(env.Stderr, err)
		return code
	}

	instances, err := uirequest.ListInstances(env.StateDir)
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}
	if len(instances) == 0 {
		cliErrln(env.Stderr, "no Sidecar instance is running")
		return 3
	}
	if len(instances) > 1 {
		cliErrf(env.Stderr, "%s\n", formatAmbiguousInstances(env.StateDir, instances))
		return 3
	}

	req := uirequest.Request{
		ID:     uirequest.NewRequestID(),
		Action: uirequest.ActionSwitchProject,
		Target: uirequest.Target{Kind: "project", Value: proj.Path},
		Origin: projectOrigin(env),
	}

	if _, err := uirequest.WriteRequest(env.StateDir, req); err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	if waitDuration <= 0 {
		if jsonOutput {
			result := projectSwitchJSON{
				Project:  proj.Name,
				Name:     proj.Name,
				Path:     proj.Path,
				Switched: true,
			}
			_ = json.NewEncoder(env.Stdout).Encode(result)
		} else {
			_, _ = fmt.Fprintf(env.Stdout, "Sent switch request for %s.\n", proj.Name)
		}
		return 0
	}

	acks := pollProjectSwitchAcks(env.StateDir, req.ID, req.Action, waitDuration)
	if len(acks) == 0 {
		cliErrln(env.Stderr, "no running Sidecar instance responded")
		return 3
	}

	ack := acks[0]
	if ack.Status == uirequest.StatusDeclined {
		cliErrln(env.Stderr, ack.Reason)
		return 4
	}

	if jsonOutput {
		result := projectSwitchJSON{
			Project:  proj.Name,
			Name:     proj.Name,
			Path:     proj.Path,
			Switched: true,
		}
		if err := json.NewEncoder(env.Stdout).Encode(result); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		return 0
	}

	if ack.Status == uirequest.StatusUnchanged {
		_, _ = fmt.Fprintf(env.Stdout, "Already showing %s.\n", proj.Name)
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "Switched to %s.\n", proj.Name)
	}
	return 0
}

func runProjectSet(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("project").FindSubcommand("set")
	help := RenderHelp(cmd)

	jsonOutput := false
	nameFlag := ""
	nameSet := false
	pathFlag := ""
	pathSet := false
	themeFlag := ""
	themeSet := false
	clearTheme := false
	openInFlag := ""
	openInSet := false
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return 0
		case arg == "--json":
			jsonOutput = true
		case arg == "--name" || strings.HasPrefix(arg, "--name="):
			val, next, ok := takeFlagArg(arg, args, i, "--name")
			if !ok || val == "" {
				cliErrf(env.Stderr, "--name requires a project name\n\n%s", help)
				return 2
			}
			nameFlag = val
			nameSet = true
			i = next
		case arg == "--path" || strings.HasPrefix(arg, "--path="):
			val, next, ok := takeFlagArg(arg, args, i, "--path")
			if !ok || val == "" {
				cliErrf(env.Stderr, "--path requires a directory path\n\n%s", help)
				return 2
			}
			pathFlag = val
			pathSet = true
			i = next
		case arg == "--theme" || strings.HasPrefix(arg, "--theme="):
			val, next, ok := takeFlagArg(arg, args, i, "--theme")
			if !ok || val == "" {
				cliErrf(env.Stderr, "--theme requires a theme name\n\n%s", help)
				return 2
			}
			themeFlag = val
			themeSet = true
			i = next
		case arg == "--clear-theme":
			clearTheme = true
		case arg == "--open-in" || strings.HasPrefix(arg, "--open-in="):
			val, next, ok := takeFlagArg(arg, args, i, "--open-in")
			if !ok || val == "" {
				cliErrf(env.Stderr, "--open-in requires an application name\n\n%s", help)
				return 2
			}
			openInFlag = val
			openInSet = true
			i = next
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
				return 2
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		cliErrf(env.Stderr, "project set requires exactly one project name\n\n%s", help)
		return 2
	}
	targetName := positional[0]

	if !nameSet && !pathSet && !themeSet && !clearTheme && !openInSet {
		cliErrf(env.Stderr, "at least one setting flag (--name, --path, --theme, --clear-theme, --open-in) is required\n\n%s", help)
		return 2
	}
	if themeSet && clearTheme {
		cliErrf(env.Stderr, "--theme and --clear-theme are mutually exclusive\n\n%s", help)
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	proj, code, err := matchConfigProject(env.StateDir, cfg.Projects.List, targetName)
	if err != nil {
		cliErrln(env.Stderr, err)
		return code
	}

	projIndex := -1
	for i, p := range cfg.Projects.List {
		if p.Path == proj.Path {
			projIndex = i
			break
		}
	}

	newName := proj.Name
	if nameSet {
		newName = strings.TrimSpace(nameFlag)
		if newName == "" {
			cliErrln(env.Stderr, "Name is required")
			return exitInputRejected
		}
	}

	newPath := proj.Path
	if pathSet {
		newPath = config.ExpandPath(pathFlag)
		if msg := config.ValidateProject(cfg.Projects.List, newName, newPath, projIndex); msg != "" {
			cliErrln(env.Stderr, msg)
			return exitInputRejected
		}
	} else if nameSet {
		if msg := config.ValidateProjectName(cfg.Projects.List, newName, projIndex); msg != "" {
			cliErrln(env.Stderr, msg)
			return exitInputRejected
		}
	}

	oldPath := proj.Path
	var updatedProj config.ProjectConfig
	err = config.UpdateProject(oldPath, func(p *config.ProjectConfig) {
		if nameSet {
			p.Name = newName
		}
		if pathSet {
			p.Path = newPath
		}
		if themeSet {
			p.Theme = &config.ThemeConfig{Name: themeFlag}
		} else if clearTheme {
			p.Theme = nil
		}
		if openInSet {
			p.OpenIn = openInFlag
		}
		updatedProj = *p
	})
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	broadcastConfigReload(env)

	if jsonOutput {
		result := makeProjectJSONItem(env.StateDir, updatedProj)
		if err := json.NewEncoder(env.Stdout).Encode(result); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		return 0
	}

	if nameSet && newName != proj.Name {
		_, _ = fmt.Fprintf(env.Stdout, "Renamed project %q to %q.\n", proj.Name, newName)
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "Updated project %q.\n", updatedProj.Name)
	}
	return 0
}

func runProjectRemove(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("project").FindSubcommand("remove")
	help := RenderHelp(cmd)

	jsonOutput := false
	yesFlag := false
	var positional []string

	for _, arg := range args {
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return 0
		case arg == "--json":
			jsonOutput = true
		case arg == "--yes":
			yesFlag = true
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
				return 2
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		cliErrf(env.Stderr, "project remove requires exactly one project name\n\n%s", help)
		return 2
	}
	targetName := positional[0]

	if !yesFlag {
		cliErrf(env.Stderr, "--yes is required to remove a project\n\n%s", help)
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	proj, code, err := matchConfigProject(env.StateDir, cfg.Projects.List, targetName)
	if err != nil {
		cliErrln(env.Stderr, err)
		return code
	}

	instances, err := uirequest.ListInstances(env.StateDir)
	if err == nil {
		projCanon := canonicalOpenPath(proj.Path)
		pKey := projKey(env.StateDir, proj.Path)
		for _, inst := range instances {
			instCanon := canonicalOpenPath(inst.WorkDir)
			if (instCanon != "" && instCanon == projCanon) ||
				(inst.ProjectKey != "" && inst.ProjectKey == pKey) ||
				(inst.Project != "" && strings.EqualFold(inst.Project, proj.Name)) {
				cliErrf(env.Stderr, "cannot remove project %q: it is currently visible in Sidecar; switch to another project first with `sidecar project switch`\n", proj.Name)
				return exitInputRejected
			}
		}
	}

	if err := config.RemoveProject(proj.Path); err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	broadcastConfigReload(env)

	if jsonOutput {
		result := projectRemoveJSON{
			Name:    proj.Name,
			Path:    proj.Path,
			Removed: true,
		}
		if err := json.NewEncoder(env.Stdout).Encode(result); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		return 0
	}

	_, _ = fmt.Fprintf(env.Stdout, "Removed project %q.\n", proj.Name)
	return 0
}

func pollProjectSwitchAcks(stateDir, id string, action uirequest.Action, wait time.Duration) []uirequest.Ack {
	if wait <= 0 {
		return nil
	}
	deadline := time.Now().Add(wait)
	var acks []uirequest.Ack
	for time.Now().Before(deadline) {
		found, err := uirequest.ReadAcks(stateDir, id, action)
		if err == nil && len(found) > 0 {
			acks = found
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	_ = uirequest.Cleanup(stateDir, id, action)
	return acks
}

func resolveCallingProject(env Env, cfg *config.Config) (*projectJSONItem, error) {
	if cfg == nil || len(cfg.Projects.List) == 0 {
		return nil, fmt.Errorf("not in a Sidecar project shell and current directory is not a configured project")
	}

	// 1. Check SIDECAR_SHELL via callerShellOrigin
	if origin, ok := callerShellOrigin(env.StateDir); ok {
		if p, found := matchOriginToConfig(env.StateDir, cfg.Projects.List, origin.ProjectKey, origin.WorkDir); found {
			return p, nil
		}
	}

	// 2. Check current tmux shell identity
	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if identity, err := currentShellIdentity(ctx); err == nil {
		if strings.HasPrefix(identity.session, "sidecar-sh-") {
			if origin, err := shellstate.LookupOrigin(env.StateDir, shellstate.Identity{TmuxName: identity.session, Namespace: identity.socket}); err == nil {
				if p, found := matchOriginToConfig(env.StateDir, cfg.Projects.List, origin.ProjectKey, origin.WorkDir); found {
					return p, nil
				}
			}
		} else if strings.HasPrefix(identity.session, "sidecar-ws-") {
			if projectRoot, _, err := currentManagedWorktree(ctx, env.StateDir, identity); err == nil {
				if p, found := matchPathToConfig(env.StateDir, cfg.Projects.List, projectRoot); found {
					return p, nil
				}
			}
		}
	}

	// 3. Fall back to cwd matching against configured projects
	if cwd, err := os.Getwd(); err == nil {
		if p, found := matchCwdToConfig(env.StateDir, cfg.Projects.List, cwd); found {
			return p, nil
		}
	}

	return nil, fmt.Errorf("not in a Sidecar project shell and current directory is not a configured project")
}

func matchOriginToConfig(stateDir string, list []config.ProjectConfig, projectKey, workDir string) (*projectJSONItem, bool) {
	canonWork := canonicalOpenPath(workDir)
	if canonWork != "" {
		for _, p := range list {
			if canonicalOpenPath(config.ExpandPath(p.Path)) == canonWork {
				return makeProjectJSONItem(stateDir, p), true
			}
		}
	}
	if projectKey != "" {
		for _, p := range list {
			k := projKey(stateDir, config.ExpandPath(p.Path))
			if k == projectKey || strings.EqualFold(p.Name, projectKey) {
				return makeProjectJSONItem(stateDir, p), true
			}
		}
	}
	return nil, false
}

func matchPathToConfig(stateDir string, list []config.ProjectConfig, path string) (*projectJSONItem, bool) {
	canon := canonicalOpenPath(path)
	for _, p := range list {
		if canonicalOpenPath(config.ExpandPath(p.Path)) == canon {
			return makeProjectJSONItem(stateDir, p), true
		}
	}
	return nil, false
}

func matchCwdToConfig(stateDir string, list []config.ProjectConfig, cwd string) (*projectJSONItem, bool) {
	canonCwd := canonicalOpenPath(cwd)
	var best *config.ProjectConfig
	bestLen := -1

	for i := range list {
		p := &list[i]
		canonProj := canonicalOpenPath(config.ExpandPath(p.Path))
		if canonCwd == canonProj || strings.HasPrefix(canonCwd, canonProj+string(filepath.Separator)) {
			if len(canonProj) > bestLen {
				best = p
				bestLen = len(canonProj)
			}
		}
	}
	if best != nil {
		return makeProjectJSONItem(stateDir, *best), true
	}
	return nil, false
}

func resolveVisibleProject(env Env, cfg *config.Config) *projectJSONItem {
	instances, err := uirequest.ListInstances(env.StateDir)
	if err != nil || len(instances) != 1 {
		return nil
	}
	inst := instances[0]
	if cfg != nil {
		for _, p := range cfg.Projects.List {
			pCanon := canonicalOpenPath(config.ExpandPath(p.Path))
			if (inst.WorkDir != "" && canonicalOpenPath(inst.WorkDir) == pCanon) ||
				(inst.ProjectKey != "" && projKey(env.StateDir, p.Path) == inst.ProjectKey) ||
				(inst.Project != "" && strings.EqualFold(p.Name, inst.Project)) {
				return makeProjectJSONItem(env.StateDir, p)
			}
		}
	}
	key := inst.ProjectKey
	if key == "" {
		key = inst.Project
	}
	name := inst.Project
	if name == "" {
		name = filepath.Base(inst.WorkDir)
	}
	return &projectJSONItem{
		Name: name,
		Path: inst.WorkDir,
		Key:  key,
	}
}

func makeProjectJSONItem(stateDir string, p config.ProjectConfig) *projectJSONItem {
	expanded := config.ExpandPath(p.Path)
	themeName := ""
	if p.Theme != nil {
		themeName = p.Theme.Name
	}
	addedAt := ""
	if p.AddedAt != nil {
		addedAt = p.AddedAt.UTC().Format(time.RFC3339)
	}
	return &projectJSONItem{
		Name:    p.Name,
		Path:    expanded,
		Key:     projKey(stateDir, expanded),
		Theme:   themeName,
		OpenIn:  p.OpenIn,
		AddedAt: addedAt,
	}
}

func shortenHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + strings.TrimPrefix(path, home)
}

func projKey(stateDir, path string) string {
	if stateDir != "" {
		if m := projectdir.LookupAllWithBase(stateDir, []string{path}); len(m) > 0 {
			if dir, ok := m[path]; ok {
				return filepath.Base(dir)
			}
		}
	}
	if dir, ok := projectdir.Lookup(path); ok {
		return filepath.Base(dir)
	}
	return filepath.Base(path)
}

func matchConfigProject(stateDir string, list []config.ProjectConfig, target string) (config.ProjectConfig, int, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return config.ProjectConfig{}, 2, fmt.Errorf("project name is required")
	}
	canonTarget := canonicalOpenPath(config.ExpandPath(target))

	// 1. Check exact slug / key match
	var slugHits []config.ProjectConfig
	for _, p := range list {
		if projKey(stateDir, config.ExpandPath(p.Path)) == target {
			slugHits = append(slugHits, p)
		}
	}
	if len(slugHits) == 1 {
		return slugHits[0], 0, nil
	}

	// 2. Check canonical path match
	var pathHits []config.ProjectConfig
	for _, p := range list {
		if canonicalOpenPath(config.ExpandPath(p.Path)) == canonTarget {
			pathHits = append(pathHits, p)
		}
	}
	if len(pathHits) == 1 {
		return pathHits[0], 0, nil
	}

	// 3. Check exact display name match (case-insensitive)
	var nameHits []config.ProjectConfig
	for _, p := range list {
		if strings.EqualFold(strings.TrimSpace(p.Name), target) {
			nameHits = append(nameHits, p)
		}
	}
	if len(nameHits) == 1 {
		return nameHits[0], 0, nil
	}

	// 4. Check basename match
	var baseHits []config.ProjectConfig
	for _, p := range list {
		pPath := config.ExpandPath(p.Path)
		if filepath.Base(filepath.Clean(pPath)) == target || filepath.Base(canonicalOpenPath(pPath)) == target {
			baseHits = append(baseHits, p)
		}
	}
	if len(baseHits) == 1 {
		return baseHits[0], 0, nil
	}

	allHits := append(slugHits, pathHits...)
	allHits = append(allHits, nameHits...)
	allHits = append(allHits, baseHits...)
	uniqueHits := make(map[string]config.ProjectConfig)
	for _, h := range allHits {
		uniqueHits[h.Path] = h
	}

	if len(uniqueHits) > 1 {
		var names []string
		for _, h := range uniqueHits {
			names = append(names, h.Name)
		}
		return config.ProjectConfig{}, 3, fmt.Errorf("project %q matches more than one configured project (%s); pass a full name or path", target, strings.Join(names, ", "))
	}

	return config.ProjectConfig{}, exitInputRejected, fmt.Errorf("unknown project %q", target)
}

func broadcastConfigReload(env Env) {
	instances, err := uirequest.ListInstances(env.StateDir)
	if err != nil || len(instances) == 0 {
		return
	}
	_, _ = uirequest.WriteRequest(env.StateDir, uirequest.Request{
		Action: uirequest.ActionConfigReload,
		Origin: projectOrigin(env),
	})
}

func projectOrigin(env Env) uirequest.Origin {
	wd, _ := os.Getwd()
	orig := uirequest.Origin{
		WorkDir: wd,
		PID:     os.Getpid(),
	}
	if shellOrig, ok := callerShellOrigin(env.StateDir); ok {
		orig.TmuxSession = shellOrig.TmuxName
		orig.Namespace = shellOrig.Namespace
		orig.ProjectKey = shellOrig.ProjectKey
		if shellOrig.WorkDir != "" {
			orig.WorkDir = shellOrig.WorkDir
		}
	}
	return orig
}
