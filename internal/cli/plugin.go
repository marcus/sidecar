package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/pluginhost"
	"github.com/marcus/sidecar/internal/plugins/assembly"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/uirequest"
)

// `sidecar plugin` is the non-interactive surface for the plugin ecosystem.
//
// Hosting plugins is a capability Sidecar owns rather than a pleasant view over
// somebody else's, so the standing "presentation layer, no CLI parity"
// exception does not apply: every operation has a non-interactive path.
//
// It dispatches from cli.Run, which runs before flag parsing and before any
// TUI, tmux, state, or log setup. Nothing in this file may reach for any of
// those. `list` and `add` additionally start no process at all: `list` reads
// configuration (unless --describe opts in), and `add` prints what it is about
// to configure and then writes one entry.

// Exit codes shared by the plugin verbs.
const (
	pluginExitOK       = 0
	pluginExitFailed   = 1
	pluginExitUsage    = 2
	pluginExitNotFound = 3
	pluginExitRefused  = 4
)

type pluginJSONItem struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Class      string   `json:"class"`
	Scope      string   `json:"scope"`
	Placements []string `json:"placements"`
	Enabled    bool     `json:"enabled"`
	// Source is the config section a protocol plugin was read from. Empty for
	// an embedded plugin, whose descriptor is a Go literal.
	Source string `json:"source,omitempty"`
	// Protocol is the wire identifier a protocol plugin is dispatched with.
	Protocol string `json:"protocol,omitempty"`
	// Active is Enabled narrowed by the feature flag that governs this
	// plugin's section. An enabled plugin whose flag is off is configured and
	// not hosted, and saying only "enabled" would hide that.
	Active bool `json:"active"`
	// InactiveReason names the flag when Enabled and Active disagree.
	InactiveReason string `json:"inactiveReason,omitempty"`
	// Describe is present only with --describe, which is the flag that opts
	// into starting a process.
	Describe *pluginDescribeReport `json:"describe,omitempty"`
}

type pluginListJSON struct {
	Plugins []pluginJSONItem `json:"plugins"`
}

// pluginDescribeReport is what one describe call produced, in the shape the
// host kept it rather than the shape the plugin sent it.
type pluginDescribeReport struct {
	OK          bool                 `json:"ok"`
	Outcome     string               `json:"outcome"`
	DurationMs  int64                `json:"durationMs"`
	Plugin      *pluginhost.Info     `json:"plugin,omitempty"`
	Context     []string             `json:"context,omitempty"`
	Matchers    []pluginhost.Matcher `json:"matchers,omitempty"`
	Collections []collectionReport   `json:"collections,omitempty"`
	Actions     []actionReport       `json:"actions,omitempty"`
	Error       *errorReport         `json:"error,omitempty"`
}

type collectionReport struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Search       string         `json:"search"`
	Columns      []columnReport `json:"columns"`
	Views        []string       `json:"views,omitempty"`
	Sort         []string       `json:"sort,omitempty"`
	Filters      []filterReport `json:"filters,omitempty"`
	Detail       bool           `json:"detail"`
	EverySeconds int            `json:"everySeconds,omitempty"`
	Watch        []string       `json:"watch,omitempty"`
	Context      []string       `json:"context,omitempty"`
}

type columnReport struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	Align     string `json:"align,omitempty"`
	Width     int    `json:"width,omitempty"`
	Primary   bool   `json:"primary,omitempty"`
	Secondary bool   `json:"secondary,omitempty"`
}

// filterReport is one declared filter. Scope marks the FIRST one, which is the
// collection's scope and the value the host always shows in its pill.
type filterReport struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	Choices []string `json:"choices,omitempty"`
	Default string   `json:"default,omitempty"`
	Scope   bool     `json:"scope,omitempty"`
}

type actionReport struct {
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	On         string        `json:"on"`
	Collection string        `json:"collection,omitempty"`
	Mutates    bool          `json:"mutates"`
	Confirm    bool          `json:"confirm"`
	Key        string        `json:"key,omitempty"`
	Inputs     []inputReport `json:"inputs,omitempty"`
}

type inputReport struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Required bool     `json:"required,omitempty"`
	Choices  []string `json:"choices,omitempty"`
	Default  string   `json:"default,omitempty"`
}

// pluginCheckReport is `plugin check`: what is configured, whether the command
// resolves, what describe said, and whatever explicit call was asked for.
type pluginCheckReport struct {
	ID              string                `json:"id"`
	Source          string                `json:"source"`
	Protocol        string                `json:"protocol"`
	Enabled         bool                  `json:"enabled"`
	State           string                `json:"state"`
	Scope           string                `json:"scope"`
	Placements      []string              `json:"placements"`
	Command         []string              `json:"command"`
	CommandPath     string                `json:"commandPath,omitempty"`
	CommandResolved bool                  `json:"commandResolved"`
	CommandError    string                `json:"commandError,omitempty"`
	PassEnv         []string              `json:"passEnv,omitempty"`
	PassEnvMissing  []string              `json:"passEnvMissing,omitempty"`
	ClaimHosts      []string              `json:"claimHosts,omitempty"`
	Timeout         string                `json:"timeout"`
	Describe        *pluginDescribeReport `json:"describe,omitempty"`
	List            *pluginListCallReport `json:"list,omitempty"`
	Get             *pluginGetCallReport  `json:"get,omitempty"`
}

type pluginListCallReport struct {
	OK         bool   `json:"ok"`
	Outcome    string `json:"outcome"`
	DurationMs int64  `json:"durationMs"`
	Collection string `json:"collection"`
	Query      string `json:"query,omitempty"`
	// Filters is what the host actually sent, after dropping undeclared keys
	// and values equal to their filter's default. Printing the applied set
	// rather than the asked-for one is the point: it is how an author sees that
	// a key was dropped.
	Filters map[string]string `json:"filters,omitempty"`
	Page    *pageReport       `json:"page,omitempty"`
	Error   *errorReport      `json:"error,omitempty"`
}

type pageReport struct {
	Outcome           string           `json:"outcome"`
	Items             []itemReport     `json:"items"`
	NextCursor        string           `json:"nextCursor,omitempty"`
	Total             int              `json:"total,omitempty"`
	Truncated         bool             `json:"truncated,omitempty"`
	Notices           []noticeReport   `json:"notices,omitempty"`
	Omitted           *omittedReport   `json:"omitted,omitempty"`
	Coverage          []coverageReport `json:"coverage,omitempty"`
	CoverageTruncated bool             `json:"coverageTruncated,omitempty"`
}

type omittedReport struct {
	Suppressed int `json:"suppressed,omitempty"`
	Dropped    int `json:"dropped,omitempty"`
}

type coverageReport struct {
	Source    string `json:"source"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	ElapsedMs int    `json:"elapsedMs,omitempty"`
}

type itemReport struct {
	ID        string            `json:"id"`
	Cells     map[string]string `json:"cells,omitempty"`
	Status    *statusReport     `json:"status,omitempty"`
	SourceURL string            `json:"sourceUrl,omitempty"`
}

type noticeReport struct {
	Tone string `json:"tone"`
	Text string `json:"text"`
}

type pluginGetCallReport struct {
	OK         bool            `json:"ok"`
	Outcome    string          `json:"outcome"`
	DurationMs int64           `json:"durationMs"`
	Collection string          `json:"collection"`
	ID         string          `json:"id"`
	Resource   *documentReport `json:"resource,omitempty"`
	Sections   []sectionReport `json:"sections,omitempty"`
	Error      *errorReport    `json:"error,omitempty"`
}

type sectionReport struct {
	Title  string          `json:"title,omitempty"`
	Body   *bodyReport     `json:"body,omitempty"`
	Fields []fieldReport   `json:"fields,omitempty"`
	Items  []timelineEntry `json:"items,omitempty"`
}

type timelineEntry struct {
	When  string `json:"when,omitempty"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text,omitempty"`
}

// pluginCallReport is `plugin call`: one raw method, with the host's envelope
// and the host's validation, printing what the host would have kept.
type pluginCallReport struct {
	ID         string                `json:"id"`
	Protocol   string                `json:"protocol"`
	Method     string                `json:"method"`
	OK         bool                  `json:"ok"`
	Outcome    string                `json:"outcome"`
	DurationMs int64                 `json:"durationMs"`
	Describe   *pluginDescribeReport `json:"describe,omitempty"`
	Page       *pageReport           `json:"page,omitempty"`
	Resource   *documentReport       `json:"resource,omitempty"`
	Sections   []sectionReport       `json:"sections,omitempty"`
	Result     *outcomeReport        `json:"result,omitempty"`
	Error      *errorReport          `json:"error,omitempty"`
}

type outcomeReport struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Refresh []string    `json:"refresh,omitempty"`
	Open    *openReport `json:"open,omitempty"`
}

type openReport struct {
	Collection string `json:"collection"`
	ID         string `json:"id"`
}

// pluginConfigReport is what add, remove, enable, and disable print.
type pluginConfigReport struct {
	ID         string   `json:"id"`
	Action     string   `json:"action"`
	Source     string   `json:"source"`
	Command    []string `json:"command,omitempty"`
	PassEnv    []string `json:"passEnv,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Placements []string `json:"placements,omitempty"`
	Enabled    bool     `json:"enabled"`
	Applied    bool     `json:"applied"`
	Message    string   `json:"message,omitempty"`
}

func pluginCommand() *Command {
	jsonFlag := Flag{Name: "--json", Summary: "Write one structured result object to stdout", Bool: true}
	helpFlag := Flag{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true}

	listCmd := &Command{
		Name:    "list",
		Summary: "List the plugins Sidecar can host",
		Usage:   "sidecar plugin list [--describe] [--json]",
		Long: "List every plugin Sidecar knows about: the embedded ones in the order the\n" +
			"header paints them, then every external plugin configured under\n" +
			"plugins.external and terminalResources.providers.\n\n" +
			"Each row reports the plugin's class (who renders it), scope (project plugins\n" +
			"are rebuilt on a project switch, global ones are built once), the placements\n" +
			"its content can occupy, and whether it is enabled. An external row also\n" +
			"reports the config section it was read from.\n\n" +
			"Enablement is plugins.<id>.enabled. Two deprecated feature flags, tasks_plugin\n" +
			"and notes_plugin, still answer for their plugin while that key is absent. A\n" +
			"plugin that is enabled but whose feature flag is off is reported inactive,\n" +
			"naming the flag.\n\n" +
			"Without --describe this reads configuration and runs nothing: no running\n" +
			"Sidecar, no PATH lookup, no subprocess. --describe opts in to running each\n" +
			"active external plugin's describe method, with the same environment, working\n" +
			"directory, and timeout the app uses.",
		Flags: []Flag{
			{Name: "--describe", Summary: "Run describe on each active external plugin", Bool: true},
			jsonFlag, helpFlag,
		},
		Args: ArgSpec{Min: 0, Max: 0},
		ExitCodes: []ExitCode{
			{Code: pluginExitOK, Summary: "success"},
			{Code: pluginExitFailed, Summary: "configuration read failure"},
			{Code: pluginExitUsage, Summary: "usage error"},
		},
		Examples: []Example{
			{Command: "sidecar plugin list"},
			{Command: "sidecar plugin list --json"},
			{Command: "sidecar plugin list --describe --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar plugin list --json",
			Summary:    "See which plugins this Sidecar hosts, their scope, and whether each is on",
		},
		Run: runPluginList,
	}

	checkCmd := &Command{
		Name:    "check",
		Summary: "Describe one external plugin, and optionally call it",
		Usage:   "sidecar plugin check [--list COLLECTION [--query Q]] [--get COLLECTION ID] [--json] <id>",
		Long: "Answer \"is this plugin configured, startable, and speaking the protocol\",\n" +
			"using the exact base environment, working directory, and timeouts the app\n" +
			"uses. This is the authoring surface; it is not a replacement for the\n" +
			"plugin's own CLI.\n\n" +
			"describe always runs. --list and --get are separate, explicit flags because\n" +
			"they can perform network access and print private data; neither is ever\n" +
			"implied. --query applies only to --list, and a collection whose search is\n" +
			"required needs one. --filter applies only to --list too, is repeatable, and\n" +
			"takes id=value naming a filter the collection declared; what is printed back\n" +
			"is what the host actually sent, so a key that was dropped shows as dropped.\n\n" +
			"Only what the host kept is printed, never the plugin's raw stdout: every\n" +
			"string shown has been through the host's own sanitization and bounds, so what\n" +
			"you see is what a pane would draw.",
		Flags: []Flag{
			{Name: "--list", Arg: "COLLECTION", Summary: "Also call list on this collection"},
			{Name: "--query", Arg: "TEXT", Summary: "Query to send with --list"},
			{Name: "--filter", Arg: "ID=VALUE", Summary: "Apply one declared filter with --list (repeatable)"},
			{Name: "--get", Arg: "COLLECTION ID", Summary: "Also call get on this collection row (two values)"},
			jsonFlag, helpFlag,
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "plugin id"},
		ExitCodes: []ExitCode{
			{Code: pluginExitOK, Summary: "the plugin answered every requested call"},
			{Code: pluginExitFailed, Summary: "a call failed"},
			{Code: pluginExitUsage, Summary: "usage error"},
			{Code: pluginExitNotFound, Summary: "no plugin with that id is configured"},
			{Code: pluginExitRefused, Summary: "the governing feature flag is off"},
		},
		Examples: []Example{
			{Command: "sidecar plugin check recall"},
			{Command: "sidecar plugin check recall --list results --query dex --json"},
			{Command: "sidecar plugin check recall --list results --query dex --filter profile=docs"},
			{Command: "sidecar plugin check recall --get results rc:notes:1 --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar plugin check <id> --json",
			Summary:    "Check that a configured plugin starts and speaks the protocol, and see what it declares",
		},
		Run: runPluginCheck,
	}

	callCmd := &Command{
		Name:    "call",
		Summary: "Call one protocol method with the host's envelope",
		Usage:   "sidecar plugin call [--params JSON] [--json] <id> <method>",
		Long: "Run one method — describe, resolve, list, get, or act — through the host's own\n" +
			"envelope, validation, and sanitization, and print what the host would have\n" +
			"kept. It is the authoring loop for a plugin: write a response, call it, see\n" +
			"exactly what survives.\n\n" +
			"--params is the method's params object as JSON:\n" +
			"  resolve  {\"matcher\":\"issue-key\",\"locator\":\"CASH-1\"}\n" +
			"  list     {\"collection\":\"results\",\"query\":\"dex\",\"filters\":{\"profile\":\"docs\"},\"cursor\":\"\",\"limit\":100}\n" +
			"  get      {\"collection\":\"results\",\"id\":\"rc:notes:1\"}\n" +
			"  act      {\"action\":\"log-note\",\"collection\":\"results\",\"id\":\"rc:notes:1\",\"inputs\":{\"text\":\"…\"}}\n\n" +
			"list first runs describe, because the declared columns are what a page is\n" +
			"sanitized against — a cell keyed by an undeclared column is dropped, and that\n" +
			"is a finding worth seeing here rather than in a pane. The same is true of\n" +
			"filters: --filter id=value is shorthand for a key inside params.filters, and\n" +
			"a key the collection never declared, or a value equal to that filter's own\n" +
			"default, is dropped before the plugin is called.\n\n" +
			"No host context is sent: this process has no surface, so it has no project\n" +
			"and no selection to offer.",
		Flags: []Flag{
			{Name: "--params", Arg: "JSON", Summary: "The method's params object"},
			{Name: "--filter", Arg: "ID=VALUE", Summary: "Apply one declared filter to list (repeatable)"},
			jsonFlag, helpFlag,
		},
		Args: ArgSpec{Min: 2, Max: 2, Description: "plugin id and method"},
		ExitCodes: []ExitCode{
			{Code: pluginExitOK, Summary: "the plugin answered"},
			{Code: pluginExitFailed, Summary: "the call failed"},
			{Code: pluginExitUsage, Summary: "usage error"},
			{Code: pluginExitNotFound, Summary: "no plugin with that id is configured"},
			{Code: pluginExitRefused, Summary: "the governing feature flag is off"},
		},
		Examples: []Example{
			{Command: "sidecar plugin call recall describe --json"},
			{Command: `sidecar plugin call recall list --params '{"collection":"results","query":"dex"}' --json`},
			{Command: `sidecar plugin call recall list --params '{"collection":"results","query":"dex"}' --filter profile=docs --json`},
			{Command: `sidecar plugin call dex act --params '{"action":"log-note","collection":"people","id":"p:ada","inputs":{"text":"hi"}}' --json`},
		},
		Agent: AgentDoc{
			Invocation: `sidecar plugin call <id> list --params '{"collection":"…","query":"…"}' --json`,
			Summary:    "Call one plugin protocol method and see exactly what the host keeps",
		},
		Run: runPluginCall,
	}

	addCmd := &Command{
		Name:    "add",
		Summary: "Configure an external plugin",
		Usage:   "sidecar plugin add [--pass-env NAME]... [--scope global] [--placement tab|panes]... [--timeout DURATION] [--claim-host HOST]... [--disabled] [--yes] [--json] <id> --command ARGV...",
		Long: "Append one entry to plugins.external. This is the whole install flow: Sidecar\n" +
			"never scans a directory, never runs every sidecar-* binary on PATH, never\n" +
			"auto-enables anything, and never lets a repository declare a plugin.\n\n" +
			"Everything after --command is the argv, executed directly with no shell. Put\n" +
			"it last.\n\n" +
			"Nothing is started: add prints exactly what will run — every argv element on\n" +
			"its own line, the working directory, and the variables that will be passed by\n" +
			"name — and asks for confirmation. --yes skips the question, which is what a\n" +
			"script or an agent uses.\n\n" +
			"A process boundary is crash isolation, not a sandbox. Configuring a plugin\n" +
			"trusts that executable with your full OS privileges.",
		Flags: []Flag{
			{Name: "--command", Arg: "ARGV...", Summary: "The argv to run; everything after it is part of the command"},
			{Name: "--pass-env", Arg: "NAME", Summary: "Pass this variable's current value through (repeatable, names only)"},
			{Name: "--scope", Arg: "SCOPE", Summary: "Lifecycle: global (the only value this version supports)"},
			{Name: "--placement", Arg: "WHERE", Summary: "tab or panes (repeatable; default both)"},
			{Name: "--timeout", Arg: "DURATION", Summary: "Per-call timeout, clamped to [1s, 60s]"},
			{Name: "--claim-host", Arg: "HOST", Summary: "Hostname whose URLs this plugin may claim (repeatable)"},
			{Name: "--disabled", Summary: "Write the entry turned off", Bool: true},
			{Name: "--yes", Short: "-y", Summary: "Skip the confirmation", Bool: true},
			jsonFlag, helpFlag,
		},
		Args: ArgSpec{Min: 1, Max: -1, Description: "plugin id, then --command and its argv"},
		ExitCodes: []ExitCode{
			{Code: pluginExitOK, Summary: "the entry was written, or the confirmation was declined"},
			{Code: pluginExitFailed, Summary: "the configuration could not be written"},
			{Code: pluginExitUsage, Summary: "usage error, or the entry was refused by validation"},
			{Code: pluginExitRefused, Summary: "plugin_protocol is off"},
		},
		Examples: []Example{
			{Command: "sidecar plugin add recall --yes --command recall sidecar-plugin"},
			{Command: "sidecar plugin add dex --pass-env DEX_PROFILE --placement panes --yes --command dex sidecar-plugin"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar plugin add <id> --yes --command <argv...>",
			Summary:    "Configure an external plugin; prints exactly what will run and starts nothing",
		},
		Mutates: true,
		Run:     runPluginAdd,
	}

	removeCmd := &Command{
		Name:    "remove",
		Summary: "Remove an external plugin's configuration",
		Usage:   "sidecar plugin remove [--json] <id>",
		Long: "Delete one entry from plugins.external. Unknown config sections are preserved,\n" +
			"and removing the last entry removes the key rather than leaving it empty.\n\n" +
			"An entry in terminalResources.providers is not removed here: that section\n" +
			"belongs to the frozen resource protocol, and the message says so.",
		Flags:     []Flag{jsonFlag, helpFlag},
		Args:      ArgSpec{Min: 1, Max: 1, Description: "plugin id"},
		ExitCodes: pluginConfigExitCodes(),
		Examples:  []Example{{Command: "sidecar plugin remove recall"}},
		Agent: AgentDoc{
			Invocation: "sidecar plugin remove <id> --json",
			Summary:    "Delete an external plugin's config entry",
		},
		Mutates: true,
		Run:     runPluginRemove,
	}

	enableCmd := &Command{
		Name:      "enable",
		Summary:   "Turn an external plugin on",
		Usage:     "sidecar plugin enable [--json] <id>",
		Long:      "Set enabled:true on the plugins.external entry.\n\nEnablement is read at startup, so a running Sidecar needs a restart.",
		Flags:     []Flag{jsonFlag, helpFlag},
		Args:      ArgSpec{Min: 1, Max: 1, Description: "plugin id"},
		ExitCodes: pluginConfigExitCodes(),
		Examples:  []Example{{Command: "sidecar plugin enable recall"}},
		Agent: AgentDoc{
			Invocation: "sidecar plugin enable <id> --json",
			Summary:    "Turn a configured external plugin on (takes effect on restart)",
		},
		Mutates: true,
		Run:     runPluginEnable,
	}

	disableCmd := &Command{
		Name:      "disable",
		Summary:   "Turn an external plugin off",
		Usage:     "sidecar plugin disable [--json] <id>",
		Long:      "Set enabled:false on the plugins.external entry. The entry is kept, so turning\nit back on needs no argv.\n\nEnablement is read at startup, so a running Sidecar needs a restart.",
		Flags:     []Flag{jsonFlag, helpFlag},
		Args:      ArgSpec{Min: 1, Max: 1, Description: "plugin id"},
		ExitCodes: pluginConfigExitCodes(),
		Examples:  []Example{{Command: "sidecar plugin disable recall"}},
		Agent: AgentDoc{
			Invocation: "sidecar plugin disable <id> --json",
			Summary:    "Turn a configured external plugin off (takes effect on restart)",
		},
		Mutates: true,
		Run:     runPluginDisable,
	}

	changedCmd := &Command{
		Name:    "changed",
		Summary: "Tell running Sidecar instances a plugin's data moved",
		Usage:   "sidecar plugin changed [--collection C] [--json] <id>",
		Long: "Write one request onto the bus saying that a plugin's data changed. Every\n" +
			"running instance re-lists the visible tabs of that plugin; a tab nobody is\n" +
			"looking at costs nothing, so this is safe from a shell hook.\n\n" +
			"It is the poke for a change no declared watch path would catch. A plugin\n" +
			"whose store is one file should declare it under refresh.watch instead and\n" +
			"need no hook at all.\n\n" +
			"--collection narrows the refresh to one collection. Omit it when the tool\n" +
			"does not know what it touched.\n\n" +
			"This starts no plugin and reads no configuration: it neither knows nor cares\n" +
			"whether the id names a configured plugin, because only a running instance\n" +
			"has the tabs that would answer.",
		Flags: []Flag{
			{Name: "--collection", Arg: "C", Summary: "Narrow the refresh to one collection"},
			jsonFlag, helpFlag,
		},
		Args: ArgSpec{Min: 1, Max: 1, Description: "plugin id"},
		ExitCodes: []ExitCode{
			{Code: pluginExitOK, Summary: "the request was written"},
			{Code: pluginExitFailed, Summary: "the request could not be written"},
			{Code: pluginExitUsage, Summary: "usage error"},
		},
		Examples: []Example{
			{Command: "sidecar plugin changed dex --collection people"},
			{Command: "sidecar plugin changed recall --json"},
		},
		Agent: AgentDoc{
			Invocation: "sidecar plugin changed <id> [--collection C]",
			Summary:    "Re-list a plugin's visible tabs after your tool wrote something",
		},
		Mutates: true,
		Run:     runPluginChanged,
	}

	return &Command{
		Name:    "plugin",
		Summary: "Inspect and configure the plugins Sidecar hosts",
		Usage:   "sidecar plugin <command> [options]",
		Long: "Inspect and configure the plugins Sidecar hosts. A plugin is either embedded\n" +
			"(compiled into Sidecar, with its own UI) or external: an explicitly configured\n" +
			"executable that answers JSON on stdout and that Sidecar renders itself.\n\n" +
			"An external plugin speaks one of two protocols, decided by the config section\n" +
			"it is written in and never by the executable. plugins.external entries speak\n" +
			"sidecar.plugin/v1-draft, which has describe, resolve, list, get, and act;\n" +
			"terminalResources.providers entries speak the frozen\n" +
			"sidecar.terminal-resource/v1, which has describe and resolve. The\n" +
			"`sidecar terminal-links` verbs remain the surface for that older section.\n\n" +
			"The draft protocol is behind the plugin_protocol feature flag. Turn it on\n" +
			"with `sidecar --enable-feature=plugin_protocol`, or set\n" +
			"features.flags.plugin_protocol.\n\n" +
			"Writing one: docs/guides/active/creating-plugins.md is the walkthrough, from\n" +
			"choosing a class to a plugin that passes `plugin check`, with a complete\n" +
			"example under docs/guides/examples/hello-plugin/. The contract itself is\n" +
			"docs/reference/plugin-protocol.md.",
		Sub: []*Command{listCmd, checkCmd, callCmd, addCmd, removeCmd, enableCmd, disableCmd, changedCmd},
		Run: runPluginRoot,
	}
}

func pluginConfigExitCodes() []ExitCode {
	return []ExitCode{
		{Code: pluginExitOK, Summary: "the configuration was written"},
		{Code: pluginExitFailed, Summary: "the configuration could not be written"},
		{Code: pluginExitUsage, Summary: "usage error"},
		{Code: pluginExitNotFound, Summary: "no plugin with that id is configured"},
		{Code: pluginExitRefused, Summary: "plugin_protocol is off, or the entry is in a section this verb does not own"},
	}
}

func runPluginRoot(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("plugin")
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprint(env.Stdout, RenderHelp(cmd))
		return pluginExitOK
	}
	sub := cmd.FindSubcommand(args[0])
	if sub != nil && sub.Run != nil {
		return sub.Run(env, args[1:])
	}
	cliErrf(env.Stderr, "unknown plugin command %q\n\n%s", args[0], RenderHelp(cmd))
	return pluginExitUsage
}

// loadPluginConfig reads configuration and initializes the feature manager from
// it. The TUI initializes the manager after this dispatch point, so a CLI
// process has to do it itself: without this the deprecated flags a descriptor
// still reads as aliases, and plugin_protocol itself, would answer from their
// built-in defaults rather than from the user's configuration.
func loadPluginConfig(env Env) (*config.Config, bool) {
	cfg, err := config.Load()
	if err != nil {
		cliErrln(env.Stderr, err)
		return nil, false
	}
	features.Init(cfg)
	for name, enabled := range env.FeatureOverrides {
		features.SetOverride(name, enabled)
	}
	return cfg, true
}

// requireProtocolFlag refuses a verb that would run or configure a draft-protocol
// plugin while the flag is off, naming the flag and how to turn it on. A
// terminalResources instance is never gated here: it answers the frozen
// protocol, whose own flag governs it.
func requireProtocolFlag(env Env, instance *config.PluginInstance) bool {
	if instance != nil && instance.IsLegacyResourceProvider() {
		return true
	}
	if features.IsEnabled(features.PluginProtocol.Name) {
		return true
	}
	cliErrf(env.Stderr, "the plugin protocol is off; turn it on with `sidecar --enable-feature=%s` or set features.flags.%s\n",
		features.PluginProtocol.Name, features.PluginProtocol.Name)
	return false
}

func runPluginList(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("plugin").FindSubcommand("list")
	help := RenderHelp(cmd)

	jsonOutput, describe := false, false
	for _, arg := range args {
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return pluginExitOK
		case arg == "--json":
			jsonOutput = true
		case arg == "--describe":
			describe = true
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
				return pluginExitUsage
			}
			cliErrf(env.Stderr, "plugin list takes no positional arguments\n\n%s", help)
			return pluginExitUsage
		}
	}

	cfg, ok := loadPluginConfig(env)
	if !ok {
		return pluginExitFailed
	}

	items := pluginItems(assembly.Descriptors(), cfg)
	items = append(items, protocolPluginItems(cfg)...)

	if describe {
		for i := range items {
			if items[i].Class != string(plugin.ClassProtocol) || !items[i].Active {
				continue
			}
			instance, found := cfg.PluginInstance(items[i].ID)
			if !found {
				continue
			}
			items[i].Describe = describePluginInstance(env.Ctx, instance)
		}
	}

	if jsonOutput {
		if err := json.NewEncoder(env.Stdout).Encode(pluginListJSON{Plugins: items}); err != nil {
			cliErrln(env.Stderr, err)
			return pluginExitFailed
		}
		return pluginExitOK
	}
	writePluginListText(env, items)
	return pluginExitOK
}

func writePluginListText(env Env, items []pluginJSONItem) {
	idWidth, classWidth, scopeWidth, placeWidth := 0, 0, 0, 0
	for _, item := range items {
		idWidth = max(idWidth, len(item.ID))
		classWidth = max(classWidth, len(item.Class))
		scopeWidth = max(scopeWidth, len(item.Scope))
		placeWidth = max(placeWidth, len(strings.Join(item.Placements, ",")))
	}
	for _, item := range items {
		state := "off"
		if item.Enabled {
			state = "on"
		}
		if item.Enabled && !item.Active {
			state = "on (inactive: " + item.InactiveReason + ")"
		}
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
			idWidth, item.ID,
			classWidth, item.Class,
			scopeWidth, item.Scope,
			placeWidth, strings.Join(item.Placements, ","),
			state)
		if item.Source != "" {
			line += "  " + item.Source
		}
		_, _ = fmt.Fprintln(env.Stdout, line)
		if d := item.Describe; d != nil {
			writeDescribeText(env, "    ", d)
		}
	}
}

// reservedPluginID reports whether id already names one of Sidecar's own
// surfaces, and what that surface is called.
//
// It reads the descriptor catalog, which the CLI can see and internal/config
// cannot, and falls back to the list config restates for the app-owned global
// tabs that have no descriptor. Validation refuses the same collision on the
// next load; refusing here means the file never gets the entry that would be
// refused, and the message names the surface rather than the section.
func reservedPluginID(id string) (string, bool) {
	for _, d := range assembly.Descriptors() {
		if d.ID == id {
			return d.Name, true
		}
	}
	return config.ReservedPluginID(id)
}

// pluginItems projects the descriptor catalog onto the reported rows. It is a
// pure function of the catalog and the configuration, so the CLI's answer and
// the settings page's cannot disagree about what is enabled.
func pluginItems(descriptors []plugin.Descriptor, cfg *config.Config) []pluginJSONItem {
	items := make([]pluginJSONItem, 0, len(descriptors))
	for _, d := range descriptors {
		enabled := d.IsEnabled(cfg)
		items = append(items, pluginJSONItem{
			ID:         d.ID,
			Name:       d.Name,
			Class:      string(d.Class),
			Scope:      string(d.Scope),
			Placements: placementStrings(d),
			Enabled:    enabled,
			Active:     enabled,
		})
	}
	return items
}

// protocolPluginItems projects the configured external instances. An enabled
// plugin whose section's feature flag is off is reported inactive and names the
// flag, because "enabled" alone would claim it is being hosted.
func protocolPluginItems(cfg *config.Config) []pluginJSONItem {
	descriptors := plugin.ProtocolDescriptors(cfg)
	items := make([]pluginJSONItem, 0, len(descriptors))
	for _, d := range descriptors {
		enabled := d.IsEnabled(cfg)
		item := pluginJSONItem{
			ID:         d.ID,
			Name:       d.Name,
			Class:      string(d.Class),
			Scope:      string(d.Scope),
			Placements: placementStrings(d),
			Enabled:    enabled,
			Source:     d.Source(),
			Protocol:   instanceProtocol(d.Instance),
			Active:     enabled,
		}
		flag := features.PluginProtocol.Name
		if d.Instance != nil && d.Instance.IsLegacyResourceProvider() {
			flag = features.TerminalResourceProviders.Name
		}
		if enabled && !features.IsEnabled(flag) {
			item.Active = false
			item.InactiveReason = flag
		}
		items = append(items, item)
	}
	return items
}

func placementStrings(d plugin.Descriptor) []string {
	out := make([]string, 0, len(d.Placements))
	for _, p := range d.Placements {
		out = append(out, string(p))
	}
	return out
}

func instanceProtocol(instance *config.PluginInstance) string {
	if instance == nil {
		return ""
	}
	if instance.IsLegacyResourceProvider() {
		return resource.Protocol
	}
	return pluginhost.Protocol
}

// newPluginProvider builds the provider for one configured instance, with the
// same working directory, environment, timeout, and protocol identifier the app
// would use. It starts nothing.
func newPluginProvider(instance config.PluginInstance) (*pluginhost.CommandProvider, error) {
	return pluginhost.NewCommandProvider(pluginhost.CommandConfig{
		Instance:       instance.ID,
		Argv:           instance.Command,
		Dir:            checkWorkingDir(),
		PassEnv:        instance.PassEnv,
		ClaimHosts:     instance.ClaimHosts,
		HostEnv:        os.Environ(),
		ResolveTimeout: instance.Timeout,
		Host:           pluginhost.HostInfo{Name: "sidecar", Version: pluginhost.HostVersion},
		Protocol:       instanceProtocol(&instance),
	})
}

func describePluginInstance(ctx context.Context, instance config.PluginInstance) *pluginDescribeReport {
	if ctx == nil {
		ctx = context.Background()
	}
	provider, err := newPluginProvider(instance)
	if err != nil {
		return &pluginDescribeReport{
			Outcome: "invalid-request",
			Error:   toErrorReport(resource.Errorf(resource.CodeInvalidConfig, "%s", err)),
		}
	}
	started := time.Now()
	desc, describeErr := provider.Describe(ctx)
	out := &pluginDescribeReport{
		Outcome:    pluginhost.OutcomeCode(describeErr),
		DurationMs: time.Since(started).Milliseconds(),
	}
	if describeErr != nil {
		out.Error = toErrorReport(pluginhost.AsResourceError(describeErr))
		return out
	}
	out.OK = true
	info := desc.Info
	out.Plugin = &info
	out.Matchers = desc.Matchers
	for _, kind := range desc.Context {
		out.Context = append(out.Context, string(kind))
	}
	for _, c := range desc.Collections {
		out.Collections = append(out.Collections, toCollectionReport(c))
	}
	for _, a := range desc.Actions {
		out.Actions = append(out.Actions, toActionReport(a))
	}
	return out
}

func toCollectionReport(c pluginhost.Collection) collectionReport {
	out := collectionReport{
		ID:           c.ID,
		Title:        c.Title,
		Search:       string(c.Search),
		Detail:       c.Detail,
		EverySeconds: c.Refresh.EverySeconds,
		Watch:        c.Refresh.Watch,
	}
	for _, col := range c.Columns {
		out.Columns = append(out.Columns, columnReport{
			ID: col.ID, Label: col.Label, Kind: string(col.Kind), Align: string(col.Align),
			Width: col.Width, Primary: col.Primary, Secondary: col.Secondary,
		})
	}
	for _, v := range c.Views {
		out.Views = append(out.Views, v.ID)
	}
	for _, s := range c.Sort {
		if s.Default != "" {
			out.Sort = append(out.Sort, s.ID+" ("+string(s.Default)+")")
			continue
		}
		out.Sort = append(out.Sort, s.ID)
	}
	for i, f := range c.Filters {
		report := filterReport{
			ID: f.ID, Label: f.Label, Kind: string(f.Kind), Default: f.Default,
			// The first declared filter is the collection's scope.
			Scope: i == 0,
		}
		for _, choice := range f.Choices {
			report.Choices = append(report.Choices, choice.ID)
		}
		out.Filters = append(out.Filters, report)
	}
	for _, kind := range c.Context {
		out.Context = append(out.Context, string(kind))
	}
	return out
}

func toActionReport(a pluginhost.Action) actionReport {
	out := actionReport{
		ID: a.ID, Title: a.Title, On: string(a.On), Collection: a.Collection,
		Mutates: a.Mutates, Confirm: a.Confirm, Key: a.Key,
	}
	for _, in := range a.Inputs {
		out.Inputs = append(out.Inputs, inputReport{
			ID: in.ID, Label: in.Label, Kind: string(in.Kind),
			Required: in.Required, Choices: in.Choices, Default: in.Default,
		})
	}
	return out
}

func toPageReport(page pluginhost.Page) *pageReport {
	out := &pageReport{
		Outcome:    string(page.Outcome),
		Items:      []itemReport{},
		NextCursor: page.NextCursor,
		Total:      page.Total,
		Truncated:  page.Truncated,
	}
	for _, item := range page.Items {
		row := itemReport{ID: item.ID, Cells: item.Cells, SourceURL: item.SourceURL}
		if item.Status != nil {
			row.Status = &statusReport{Label: item.Status.Label, Tone: string(item.Status.Tone)}
		}
		out.Items = append(out.Items, row)
	}
	for _, notice := range page.Notices {
		out.Notices = append(out.Notices, noticeReport{Tone: string(notice.Tone), Text: notice.Text})
	}
	if page.Omitted.Any() {
		out.Omitted = &omittedReport{Suppressed: page.Omitted.Suppressed, Dropped: page.Omitted.Dropped}
	}
	for _, row := range page.Coverage {
		out.Coverage = append(out.Coverage, coverageReport{
			Source: row.Source, State: string(row.State), Reason: row.Reason, ElapsedMs: row.ElapsedMs,
		})
	}
	out.CoverageTruncated = page.CoverageTruncated
	return out
}

func toSectionReports(sections []resource.Section) []sectionReport {
	if len(sections) == 0 {
		return nil
	}
	out := make([]sectionReport, 0, len(sections))
	for _, s := range sections {
		report := sectionReport{Title: s.Title}
		if s.Body != nil {
			report.Body = &bodyReport{Format: string(s.Body.Format), Text: s.Body.Text, Truncated: s.Body.Truncated}
		}
		for _, f := range s.Fields {
			report.Fields = append(report.Fields, fieldReport{Label: f.Label, Value: f.Value, Kind: string(f.Kind)})
		}
		for _, item := range s.Items {
			entry := timelineEntry{Title: item.Title, Text: item.Text}
			if !item.When.IsZero() {
				entry.When = item.When.Format(time.RFC3339)
			}
			report.Items = append(report.Items, entry)
		}
		out = append(out, report)
	}
	return out
}

func toOutcomeReport(outcome pluginhost.Outcome) *outcomeReport {
	out := &outcomeReport{
		Status:  string(outcome.Status),
		Message: outcome.Message,
		Refresh: outcome.Refresh,
	}
	if outcome.Open != nil {
		out.Open = &openReport{Collection: outcome.Open.Collection, ID: outcome.Open.ID}
	}
	return out
}

// parseFilterFlag reads one --filter id=value. The id may not be empty and the
// value may be: `--filter since=` clears a text filter, which is a different
// intention from not naming it at all.
func parseFilterFlag(raw string) (string, string, bool) {
	id, value, found := strings.Cut(raw, "=")
	id = strings.TrimSpace(id)
	if !found || id == "" {
		return "", "", false
	}
	return id, value, true
}

func runPluginCheck(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("plugin").FindSubcommand("check")
	help := RenderHelp(cmd)

	var (
		jsonOutput bool
		listName   string
		query      string
		getName    string
		getID      string
		wantList   bool
		wantGet    bool
		filters    map[string]string
		positional []string
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return pluginExitOK
		case arg == "--json":
			jsonOutput = true
		case arg == "--list":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "--list needs a collection id\n\n%s", help)
				return pluginExitUsage
			}
			i++
			listName, wantList = args[i], true
		case strings.HasPrefix(arg, "--list="):
			listName, wantList = strings.TrimPrefix(arg, "--list="), true
		case arg == "--query":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "--query needs a value\n\n%s", help)
				return pluginExitUsage
			}
			i++
			query = args[i]
		case strings.HasPrefix(arg, "--query="):
			query = strings.TrimPrefix(arg, "--query=")
		case arg == "--filter":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "--filter needs id=value\n\n%s", help)
				return pluginExitUsage
			}
			i++
			id, value, ok := parseFilterFlag(args[i])
			if !ok {
				cliErrf(env.Stderr, "--filter takes id=value, not %q\n\n%s", args[i], help)
				return pluginExitUsage
			}
			if filters == nil {
				filters = map[string]string{}
			}
			filters[id] = value
		case strings.HasPrefix(arg, "--filter="):
			id, value, ok := parseFilterFlag(strings.TrimPrefix(arg, "--filter="))
			if !ok {
				cliErrf(env.Stderr, "--filter takes id=value, not %q\n\n%s", arg, help)
				return pluginExitUsage
			}
			if filters == nil {
				filters = map[string]string{}
			}
			filters[id] = value
		case arg == "--get":
			if i+2 >= len(args) {
				cliErrf(env.Stderr, "--get needs a collection id and a row id\n\n%s", help)
				return pluginExitUsage
			}
			getName, getID, wantGet = args[i+1], args[i+2], true
			i += 2
		case strings.HasPrefix(arg, "-"):
			cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
			return pluginExitUsage
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		cliErrf(env.Stderr, "plugin check takes exactly one plugin id\n\n%s", help)
		return pluginExitUsage
	}
	if query != "" && !wantList {
		cliErrf(env.Stderr, "--query applies only to --list\n\n%s", help)
		return pluginExitUsage
	}
	if len(filters) > 0 && !wantList {
		cliErrf(env.Stderr, "--filter applies only to --list\n\n%s", help)
		return pluginExitUsage
	}

	cfg, ok := loadPluginConfig(env)
	if !ok {
		return pluginExitFailed
	}
	instance, found := cfg.PluginInstance(positional[0])
	if !found {
		cliErrf(env.Stderr, "no plugin named %q is configured in plugins.external or terminalResources.providers\n", positional[0])
		return pluginExitNotFound
	}
	if !requireProtocolFlag(env, &instance) {
		return pluginExitRefused
	}

	report := inspectPluginInstance(instance)
	failed := !report.CommandResolved

	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if instance.Enabled && report.CommandResolved {
		report.Describe = describePluginInstance(ctx, instance)
		report.State = pluginStateFromDescribe(instance, report.Describe)
		if !report.Describe.OK {
			failed = true
		}
	}

	if wantList || wantGet {
		switch {
		case report.Describe == nil || !report.Describe.OK:
			cliErrf(env.Stderr, "plugin %q cannot be called: %s\n", instance.ID, report.State)
			failed = true
		case instance.IsLegacyResourceProvider():
			cliErrf(env.Stderr, "plugin %q speaks %s, which has no list or get\n", instance.ID, resource.Protocol)
			failed = true
		default:
			provider, err := newPluginProvider(instance)
			if err != nil {
				cliErrln(env.Stderr, err)
				return pluginExitFailed
			}
			if wantList {
				report.List = callList(ctx, provider, report.Describe, listName, query, filters)
				if !report.List.OK {
					failed = true
				}
			}
			if wantGet {
				report.Get = callGet(ctx, provider, getName, getID)
				if !report.Get.OK {
					failed = true
				}
			}
		}
	}

	if jsonOutput {
		if code := writeJSON(env, report); code != 0 {
			return code
		}
	} else {
		writePluginCheckText(env, report)
	}
	if failed {
		return pluginExitFailed
	}
	return pluginExitOK
}

// inspectPluginInstance does the configuration and command-resolution half of a
// check. It resolves argv[0] through PATH but never runs it.
func inspectPluginInstance(instance config.PluginInstance) pluginCheckReport {
	report := pluginCheckReport{
		ID:         instance.ID,
		Source:     instance.Source,
		Protocol:   instanceProtocol(&instance),
		Enabled:    instance.Enabled,
		Scope:      instance.Scope,
		Placements: instance.Placements,
		Command:    instance.Command,
		PassEnv:    instance.PassEnv,
		ClaimHosts: instance.ClaimHosts,
		Timeout:    instance.Timeout.String(),
		State:      string(pluginhost.StateUnchecked),
	}
	if !instance.Enabled {
		report.State = string(pluginhost.StateDisabled)
	}
	if len(instance.Command) > 0 {
		path, err := exec.LookPath(instance.Command[0])
		if err != nil {
			report.CommandError = "not found on PATH or not executable"
			if instance.Enabled {
				report.State = string(pluginhost.StateIncompatible)
			}
		} else {
			report.CommandPath = path
			report.CommandResolved = true
		}
	}
	// Presence only. A passEnv value is never printed, logged, or rendered.
	for _, name := range instance.PassEnv {
		if _, ok := os.LookupEnv(name); !ok {
			report.PassEnvMissing = append(report.PassEnvMissing, name)
		}
	}
	return report
}

func pluginStateFromDescribe(instance config.PluginInstance, d *pluginDescribeReport) string {
	if !instance.Enabled {
		return string(pluginhost.StateDisabled)
	}
	if d == nil {
		return string(pluginhost.StateUnchecked)
	}
	if d.OK {
		return string(pluginhost.StateReady)
	}
	switch d.Outcome {
	case "protocol", "invalid-describe", "spawn", "shape", "invalid_config", "invalid_request":
		return string(pluginhost.StateIncompatible)
	default:
		return string(pluginhost.StateTemporarilyFailed)
	}
}

func callList(ctx context.Context, provider *pluginhost.CommandProvider, describe *pluginDescribeReport, name, query string, filters map[string]string) *pluginListCallReport {
	out := &pluginListCallReport{Collection: name, Query: query}
	collection, ok := declaredCollection(describe, name)
	if !ok {
		out.Outcome = "invalid-request"
		out.Error = toErrorReport(resource.Errorf(resource.CodeNotFound, "the plugin declares no collection named %q", name))
		return out
	}
	// What is reported is what the host will actually send, not what was asked
	// for: an undeclared key or a value equal to its default is dropped, and
	// seeing that here is the whole point of printing it.
	out.Filters = pluginhost.NormalizeFilters(collection, filters)
	started := time.Now()
	page, err := provider.List(ctx, pluginhost.ListParams{Collection: name, Query: query, Filters: filters}, nil, collection)
	out.DurationMs = time.Since(started).Milliseconds()
	out.Outcome = pluginhost.OutcomeCode(err)
	if err != nil {
		out.Error = toErrorReport(pluginhost.AsResourceError(err))
		return out
	}
	out.OK = true
	out.Page = toPageReport(page)
	return out
}

func callGet(ctx context.Context, provider *pluginhost.CommandProvider, name, id string) *pluginGetCallReport {
	out := &pluginGetCallReport{Collection: name, ID: id}
	started := time.Now()
	doc, err := provider.Get(ctx, pluginhost.GetParams{Collection: name, ID: id}, nil)
	out.DurationMs = time.Since(started).Milliseconds()
	out.Outcome = pluginhost.OutcomeCode(err)
	if err != nil {
		out.Error = toErrorReport(pluginhost.AsResourceError(err))
		return out
	}
	out.OK = true
	out.Resource = toDocumentReport(doc)
	out.Sections = toSectionReports(doc.Sections)
	return out
}

// declaredCollection rebuilds the validated collection from the describe report
// so a page is sanitized against exactly the columns the plugin declared, which
// is what the host does.
func declaredCollection(describe *pluginDescribeReport, name string) (pluginhost.Collection, bool) {
	if describe == nil {
		return pluginhost.Collection{}, false
	}
	for _, c := range describe.Collections {
		if c.ID != name {
			continue
		}
		out := pluginhost.Collection{ID: c.ID, Title: c.Title, Search: pluginhost.SearchMode(c.Search), Detail: c.Detail}
		for _, f := range c.Filters {
			filter := pluginhost.Filter{ID: f.ID, Label: f.Label, Kind: pluginhost.FilterKind(f.Kind), Default: f.Default}
			for _, choice := range f.Choices {
				filter.Choices = append(filter.Choices, pluginhost.FilterOption{ID: choice, Title: choice})
			}
			out.Filters = append(out.Filters, filter)
		}
		for _, col := range c.Columns {
			out.Columns = append(out.Columns, pluginhost.Column{
				ID: col.ID, Label: col.Label, Kind: pluginhost.ColumnKind(col.Kind),
				Align: pluginhost.Align(col.Align), Width: col.Width,
				Primary: col.Primary, Secondary: col.Secondary,
			})
		}
		return out, true
	}
	return pluginhost.Collection{}, false
}

func writePluginCheckText(env Env, report pluginCheckReport) {
	enabled := "enabled"
	if !report.Enabled {
		enabled = "disabled"
	}
	_, _ = fmt.Fprintf(env.Stdout, "%s  [%s, %s]  %s\n", report.ID, enabled, report.State, report.Source)
	_, _ = fmt.Fprintf(env.Stdout, "  protocol  %s\n", report.Protocol)
	_, _ = fmt.Fprintf(env.Stdout, "  command   %s\n", strings.Join(report.Command, " "))
	if report.CommandResolved {
		_, _ = fmt.Fprintf(env.Stdout, "  resolves  %s\n", report.CommandPath)
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "  resolves  no — %s\n", report.CommandError)
	}
	_, _ = fmt.Fprintf(env.Stdout, "  scope     %s\n", report.Scope)
	_, _ = fmt.Fprintf(env.Stdout, "  places    %s\n", strings.Join(report.Placements, ","))
	_, _ = fmt.Fprintf(env.Stdout, "  timeout   %s\n", report.Timeout)
	if len(report.PassEnv) > 0 {
		// Names only, and presence only. A value never reaches this output.
		_, _ = fmt.Fprintf(env.Stdout, "  passEnv   %s\n", strings.Join(report.PassEnv, ", "))
		if len(report.PassEnvMissing) > 0 {
			_, _ = fmt.Fprintf(env.Stdout, "            unset in this environment: %s\n", strings.Join(report.PassEnvMissing, ", "))
		}
	}
	if len(report.ClaimHosts) > 0 {
		_, _ = fmt.Fprintf(env.Stdout, "  claims    %s\n", strings.Join(report.ClaimHosts, ", "))
	}
	if report.Describe != nil {
		writeDescribeText(env, "  ", report.Describe)
	}
	if r := report.List; r != nil {
		if r.OK {
			_, _ = fmt.Fprintf(env.Stdout, "  list      ok in %dms — %s, %s, %d row(s)\n", r.DurationMs, r.Collection, r.Page.Outcome, len(r.Page.Items))
			// The applied set, not the asked-for one: a dropped key is a
			// finding, so it is printed as an absence rather than hidden.
			if len(r.Filters) > 0 {
				parts := make([]string, 0, len(r.Filters))
				for _, key := range pluginhost.FilterKeys(r.Filters) {
					parts = append(parts, key+"="+r.Filters[key])
				}
				_, _ = fmt.Fprintf(env.Stdout, "            filters   %s\n", strings.Join(parts, " "))
			}
			for _, item := range r.Page.Items {
				_, _ = fmt.Fprintf(env.Stdout, "            %s  %s\n", item.ID, primaryCell(item))
			}
			if o := r.Page.Omitted; o != nil {
				_, _ = fmt.Fprintf(env.Stdout, "            omitted   %d below floor, %d over budget\n", o.Suppressed, o.Dropped)
			}
			for _, row := range r.Page.Coverage {
				line := fmt.Sprintf("            coverage  %s  %s", row.Source, row.State)
				if row.ElapsedMs > 0 {
					line += fmt.Sprintf("  %dms", row.ElapsedMs)
				}
				if row.Reason != "" {
					line += "  " + row.Reason
				}
				_, _ = fmt.Fprintln(env.Stdout, line)
			}
			if r.Page.CoverageTruncated {
				_, _ = fmt.Fprintln(env.Stdout, "            coverage  the plugin sent more sources than Sidecar keeps")
			}
			for _, notice := range r.Page.Notices {
				_, _ = fmt.Fprintf(env.Stdout, "            %s: %s\n", notice.Tone, notice.Text)
			}
			if r.Page.NextCursor != "" {
				_, _ = fmt.Fprintln(env.Stdout, "            more pages available")
			}
			if r.Page.Truncated {
				_, _ = fmt.Fprintln(env.Stdout, "            the page was truncated to the host limit")
			}
		} else {
			_, _ = fmt.Fprintf(env.Stdout, "  list      failed in %dms — %s\n", r.DurationMs, r.Outcome)
			writeErrorText(env, "            ", r.Error)
		}
	}
	if r := report.Get; r != nil {
		if r.OK {
			_, _ = fmt.Fprintf(env.Stdout, "  get       ok in %dms — %s  %s\n", r.DurationMs, r.Resource.Identity, r.Resource.Title)
			for _, section := range r.Sections {
				_, _ = fmt.Fprintf(env.Stdout, "            section %s (%s)\n", section.Title, sectionShape(section))
			}
		} else {
			_, _ = fmt.Fprintf(env.Stdout, "  get       failed in %dms — %s\n", r.DurationMs, r.Outcome)
			writeErrorText(env, "            ", r.Error)
		}
	}
}

// primaryCell is the honest stand-in for the row label in a text listing: the
// declared primary column when the report carries one, and otherwise the first
// non-empty cell.
func primaryCell(item itemReport) string {
	for _, key := range []string{"title", "name"} {
		if v, ok := item.Cells[key]; ok && v != "" {
			return v
		}
	}
	for _, v := range item.Cells {
		if v != "" {
			return v
		}
	}
	return ""
}

func sectionShape(section sectionReport) string {
	switch {
	case section.Body != nil:
		return "body"
	case len(section.Fields) > 0:
		return fmt.Sprintf("%d field(s)", len(section.Fields))
	default:
		return fmt.Sprintf("%d timeline item(s)", len(section.Items))
	}
}

func writeDescribeText(env Env, indent string, d *pluginDescribeReport) {
	if !d.OK {
		_, _ = fmt.Fprintf(env.Stdout, "%sdescribe  failed in %dms — %s\n", indent, d.DurationMs, d.Outcome)
		writeErrorText(env, indent+"          ", d.Error)
		return
	}
	name := d.Plugin.Name
	if name == "" {
		name = d.Plugin.Kind
	}
	_, _ = fmt.Fprintf(env.Stdout, "%sdescribe  ok in %dms — %s %s, %d matcher(s), %d collection(s), %d action(s)\n",
		indent, d.DurationMs, name, d.Plugin.Version, len(d.Matchers), len(d.Collections), len(d.Actions))
	pad := indent + "          "
	if len(d.Context) > 0 {
		_, _ = fmt.Fprintf(env.Stdout, "%sreads context %s\n", pad, strings.Join(d.Context, ", "))
	}
	for _, m := range d.Matchers {
		_, _ = fmt.Fprintf(env.Stdout, "%smatcher %s (priority %d)  %s\n", pad, m.ID, m.Priority, m.Pattern)
	}
	for _, c := range d.Collections {
		columns := make([]string, 0, len(c.Columns))
		for _, col := range c.Columns {
			mark := ""
			switch {
			case col.Primary:
				mark = "*"
			case col.Secondary:
				mark = "^"
			}
			columns = append(columns, col.ID+mark)
		}
		line := fmt.Sprintf("%scollection %s %q search=%s columns=%s", pad, c.ID, c.Title, c.Search, strings.Join(columns, ","))
		if len(c.Views) > 0 {
			line += " views=" + strings.Join(c.Views, ",")
		}
		if len(c.Sort) > 0 {
			line += " sort=" + strings.Join(c.Sort, ",")
		}
		if c.Detail {
			line += " detail"
		}
		if c.EverySeconds > 0 {
			line += fmt.Sprintf(" poll=%ds", c.EverySeconds)
		}
		_, _ = fmt.Fprintln(env.Stdout, line)
		for _, f := range c.Filters {
			scope := ""
			if f.Scope {
				scope = " scope"
			}
			filterLine := fmt.Sprintf("%s  filter %s %q kind=%s%s", pad, f.ID, f.Label, f.Kind, scope)
			if len(f.Choices) > 0 {
				filterLine += " choices=" + strings.Join(f.Choices, ",")
			}
			if f.Default != "" {
				filterLine += " default=" + f.Default
			}
			_, _ = fmt.Fprintln(env.Stdout, filterLine)
		}
		for _, path := range c.Watch {
			_, _ = fmt.Fprintf(env.Stdout, "%s  watches %s\n", pad, path)
		}
	}
	for _, a := range d.Actions {
		line := fmt.Sprintf("%saction %s %q on=%s", pad, a.ID, a.Title, a.On)
		if a.Collection != "" {
			line += " collection=" + a.Collection
		}
		if a.Mutates {
			line += " mutates"
		}
		if a.Confirm {
			line += " confirm"
		}
		if a.Key != "" {
			line += " key=" + a.Key
		}
		_, _ = fmt.Fprintln(env.Stdout, line)
		for _, in := range a.Inputs {
			required := ""
			if in.Required {
				required = " required"
			}
			choices := ""
			if len(in.Choices) > 0 {
				choices = " choices=" + strings.Join(in.Choices, "|")
			}
			_, _ = fmt.Fprintf(env.Stdout, "%s  input %s %q kind=%s%s%s\n", pad, in.ID, in.Label, in.Kind, required, choices)
		}
	}
	if d.Plugin.DocsURL != "" {
		_, _ = fmt.Fprintf(env.Stdout, "%sdocs %s\n", pad, d.Plugin.DocsURL)
	}
}

// callParams is every method's params object in one struct, because --params is
// one JSON value and the method decides which fields matter.
type callParams struct {
	Matcher    string               `json:"matcher"`
	Locator    string               `json:"locator"`
	Collection string               `json:"collection"`
	Query      string               `json:"query"`
	View       string               `json:"view"`
	Sort       pluginhost.SortOrder `json:"sort"`
	Filters    map[string]string    `json:"filters"`
	Cursor     string               `json:"cursor"`
	Limit      int                  `json:"limit"`
	ID         string               `json:"id"`
	Action     string               `json:"action"`
	Inputs     map[string]string    `json:"inputs"`
}

func runPluginCall(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("plugin").FindSubcommand("call")
	help := RenderHelp(cmd)

	var (
		jsonOutput  bool
		rawParams   string
		flagFilters map[string]string
		positional  []string
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return pluginExitOK
		case arg == "--json":
			jsonOutput = true
		case arg == "--params":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "--params needs a JSON object\n\n%s", help)
				return pluginExitUsage
			}
			i++
			rawParams = args[i]
		case strings.HasPrefix(arg, "--params="):
			rawParams = strings.TrimPrefix(arg, "--params=")
		case arg == "--filter", strings.HasPrefix(arg, "--filter="):
			raw := strings.TrimPrefix(arg, "--filter=")
			if arg == "--filter" {
				if i+1 >= len(args) {
					cliErrf(env.Stderr, "--filter needs id=value\n\n%s", help)
					return pluginExitUsage
				}
				i++
				raw = args[i]
			}
			fid, value, ok := parseFilterFlag(raw)
			if !ok {
				cliErrf(env.Stderr, "--filter takes id=value, not %q\n\n%s", raw, help)
				return pluginExitUsage
			}
			if flagFilters == nil {
				flagFilters = map[string]string{}
			}
			flagFilters[fid] = value
		case strings.HasPrefix(arg, "-"):
			cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
			return pluginExitUsage
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 2 {
		cliErrf(env.Stderr, "plugin call takes a plugin id and a method\n\n%s", help)
		return pluginExitUsage
	}
	id, method := positional[0], positional[1]
	switch method {
	case pluginhost.MethodDescribe, pluginhost.MethodResolve, pluginhost.MethodList, pluginhost.MethodGet, pluginhost.MethodAct:
	default:
		cliErrf(env.Stderr, "unknown method %q; the methods are describe, resolve, list, get, and act\n\n%s", method, help)
		return pluginExitUsage
	}

	var params callParams
	if rawParams != "" {
		if err := json.Unmarshal([]byte(rawParams), &params); err != nil {
			cliErrf(env.Stderr, "--params is not a JSON object: %v\n", err)
			return pluginExitUsage
		}
	}
	if len(flagFilters) > 0 {
		if method != pluginhost.MethodList {
			cliErrf(env.Stderr, "--filter applies only to list\n\n%s", help)
			return pluginExitUsage
		}
		// A flag wins over the same key inside --params: it is the later, more
		// specific statement of the same intention.
		if params.Filters == nil {
			params.Filters = map[string]string{}
		}
		for k, v := range flagFilters {
			params.Filters[k] = v
		}
	}

	cfg, ok := loadPluginConfig(env)
	if !ok {
		return pluginExitFailed
	}
	instance, found := cfg.PluginInstance(id)
	if !found {
		cliErrf(env.Stderr, "no plugin named %q is configured in plugins.external or terminalResources.providers\n", id)
		return pluginExitNotFound
	}
	if !requireProtocolFlag(env, &instance) {
		return pluginExitRefused
	}
	provider, err := newPluginProvider(instance)
	if err != nil {
		cliErrln(env.Stderr, err)
		return pluginExitFailed
	}

	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	report := pluginCallReport{ID: instance.ID, Protocol: instanceProtocol(&instance), Method: method}
	started := time.Now()
	var callErr error

	switch method {
	case pluginhost.MethodDescribe:
		report.Describe = describePluginInstance(ctx, instance)
		report.OK = report.Describe.OK
		report.Outcome = report.Describe.Outcome
		report.DurationMs = report.Describe.DurationMs
		report.Error = report.Describe.Error
		if jsonOutput {
			if code := writeJSON(env, report); code != 0 {
				return code
			}
		} else {
			writeDescribeText(env, "", report.Describe)
		}
		if !report.OK {
			return pluginExitFailed
		}
		return pluginExitOK

	case pluginhost.MethodResolve:
		var doc resource.Document
		doc, callErr = provider.Resolve(ctx, resource.Reference{
			Instance: instance.ID, Matcher: params.Matcher, Locator: params.Locator,
		})
		if callErr == nil {
			report.Resource = toDocumentReport(doc)
			report.Sections = toSectionReports(doc.Sections)
		}

	case pluginhost.MethodList:
		// describe first: the declared columns are what a page is sanitized
		// against, and a cell keyed by an undeclared column is dropped.
		describe := describePluginInstance(ctx, instance)
		if !describe.OK {
			report.Describe = describe
			report.Outcome = describe.Outcome
			report.Error = describe.Error
			report.DurationMs = describe.DurationMs
			return finishCall(env, jsonOutput, report)
		}
		collection, declared := declaredCollection(describe, params.Collection)
		if !declared {
			report.Outcome = "invalid-request"
			report.Error = toErrorReport(resource.Errorf(resource.CodeNotFound, "the plugin declares no collection named %q", params.Collection))
			return finishCall(env, jsonOutput, report)
		}
		started = time.Now()
		var page pluginhost.Page
		page, callErr = provider.List(ctx, pluginhost.ListParams{
			Collection: params.Collection, Query: params.Query, View: params.View,
			Sort: params.Sort, Filters: params.Filters, Cursor: params.Cursor, Limit: params.Limit,
		}, nil, collection)
		if callErr == nil {
			report.Page = toPageReport(page)
		}

	case pluginhost.MethodGet:
		var doc resource.Document
		doc, callErr = provider.Get(ctx, pluginhost.GetParams{Collection: params.Collection, ID: params.ID}, nil)
		if callErr == nil {
			report.Resource = toDocumentReport(doc)
			report.Sections = toSectionReports(doc.Sections)
		}

	case pluginhost.MethodAct:
		var outcome pluginhost.Outcome
		outcome, callErr = provider.Act(ctx, pluginhost.ActParams{
			Action: params.Action, Collection: params.Collection, ID: params.ID,
			Matcher: params.Matcher, Locator: params.Locator, Inputs: params.Inputs,
		}, nil)
		if callErr == nil {
			report.Result = toOutcomeReport(outcome)
		}
	}

	report.DurationMs = time.Since(started).Milliseconds()
	report.Outcome = pluginhost.OutcomeCode(callErr)
	if callErr != nil {
		report.Error = toErrorReport(pluginhost.AsResourceError(callErr))
	} else {
		report.OK = true
	}
	return finishCall(env, jsonOutput, report)
}

func finishCall(env Env, jsonOutput bool, report pluginCallReport) int {
	if jsonOutput {
		if code := writeJSON(env, report); code != 0 {
			return code
		}
	} else {
		writeCallText(env, report)
	}
	if !report.OK {
		return pluginExitFailed
	}
	return pluginExitOK
}

func writeCallText(env Env, report pluginCallReport) {
	if !report.OK {
		_, _ = fmt.Fprintf(env.Stdout, "%s %s failed in %dms — %s\n", report.ID, report.Method, report.DurationMs, report.Outcome)
		writeErrorText(env, "  ", report.Error)
		return
	}
	_, _ = fmt.Fprintf(env.Stdout, "%s %s ok in %dms\n", report.ID, report.Method, report.DurationMs)
	if p := report.Page; p != nil {
		_, _ = fmt.Fprintf(env.Stdout, "  %s, %d row(s)", p.Outcome, len(p.Items))
		if p.Total > 0 {
			_, _ = fmt.Fprintf(env.Stdout, ", total %d", p.Total)
		}
		_, _ = fmt.Fprintln(env.Stdout)
		for _, item := range p.Items {
			_, _ = fmt.Fprintf(env.Stdout, "  %s  %s\n", item.ID, primaryCell(item))
		}
		for _, notice := range p.Notices {
			_, _ = fmt.Fprintf(env.Stdout, "  %s: %s\n", notice.Tone, notice.Text)
		}
		if p.NextCursor != "" {
			_, _ = fmt.Fprintf(env.Stdout, "  nextCursor %s\n", p.NextCursor)
		}
	}
	if r := report.Resource; r != nil {
		_, _ = fmt.Fprintf(env.Stdout, "  %s  %s\n", r.Identity, r.Title)
		if r.Subtitle != "" {
			_, _ = fmt.Fprintf(env.Stdout, "  %s\n", r.Subtitle)
		}
		for _, f := range r.Fields {
			_, _ = fmt.Fprintf(env.Stdout, "  %-16s %s\n", f.Label, f.Value)
		}
		for _, section := range report.Sections {
			_, _ = fmt.Fprintf(env.Stdout, "  section %s (%s)\n", section.Title, sectionShape(section))
		}
	}
	if o := report.Result; o != nil {
		_, _ = fmt.Fprintf(env.Stdout, "  %s: %s\n", o.Status, o.Message)
		if len(o.Refresh) > 0 {
			_, _ = fmt.Fprintf(env.Stdout, "  refresh %s\n", strings.Join(o.Refresh, ", "))
		}
		if o.Open != nil {
			_, _ = fmt.Fprintf(env.Stdout, "  open %s/%s\n", o.Open.Collection, o.Open.ID)
		}
	}
}

func runPluginAdd(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("plugin").FindSubcommand("add")
	help := RenderHelp(cmd)

	entry := config.PluginInstanceConfig{Enabled: true}
	var (
		jsonOutput bool
		yes        bool
		positional []string
		sawCommand bool
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if sawCommand {
			entry.Command = append(entry.Command, arg)
			continue
		}
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return pluginExitOK
		case arg == "--json":
			jsonOutput = true
		case arg == "--yes" || arg == "-y":
			yes = true
		case arg == "--disabled":
			entry.Enabled = false
		case arg == "--command":
			sawCommand = true
		case arg == "--pass-env", arg == "--scope", arg == "--placement", arg == "--timeout", arg == "--claim-host":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "%s needs a value\n\n%s", arg, help)
				return pluginExitUsage
			}
			i++
			if code := applyAddFlag(env, &entry, arg, args[i], help); code != 0 {
				return code
			}
		case strings.HasPrefix(arg, "--pass-env="), strings.HasPrefix(arg, "--scope="),
			strings.HasPrefix(arg, "--placement="), strings.HasPrefix(arg, "--timeout="),
			strings.HasPrefix(arg, "--claim-host="):
			name, value, _ := strings.Cut(arg, "=")
			if code := applyAddFlag(env, &entry, name, value, help); code != 0 {
				return code
			}
		case strings.HasPrefix(arg, "-"):
			cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
			return pluginExitUsage
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		cliErrf(env.Stderr, "plugin add takes exactly one plugin id\n\n%s", help)
		return pluginExitUsage
	}
	entry.ID = positional[0]
	if len(entry.Command) == 0 {
		cliErrf(env.Stderr, "plugin add needs --command followed by the argv to run\n\n%s", help)
		return pluginExitUsage
	}

	cfg, ok := loadPluginConfig(env)
	if !ok {
		return pluginExitFailed
	}
	if !requireProtocolFlag(env, nil) {
		return pluginExitRefused
	}
	if name, reserved := reservedPluginID(entry.ID); reserved {
		cliErrf(env.Stderr, "%q is the id of Sidecar's built-in %s surface; the id is the config key, the CLI name and the persisted tab id, so choose another one\n",
			entry.ID, name)
		return pluginExitUsage
	}
	if existing, found := cfg.PluginInstance(entry.ID); found {
		cliErrf(env.Stderr, "a plugin named %q is already configured in %s\n", entry.ID, existing.Source)
		return pluginExitUsage
	}

	// Validate before showing the plan: a plan for an entry that will be
	// refused is a plan for something that will never run.
	probe := &config.Config{}
	probe.Plugins.External = []config.PluginInstanceConfig{entry}
	if err := probe.Validate(); err != nil {
		cliErrln(env.Stderr, err)
		return pluginExitUsage
	}
	normalized := probe.Plugins.External[0]

	report := pluginConfigReport{
		ID: normalized.ID, Action: "add", Source: config.PluginSourceExternal,
		Command: normalized.Command, PassEnv: normalized.PassEnv,
		Scope: normalized.Scope, Placements: normalized.Placements, Enabled: normalized.Enabled,
	}

	if !yes {
		writeAddPlan(env, normalized)
		confirmed, err := confirmPluginAdd(env)
		if err != nil {
			cliErrf(env.Stderr, "%v; pass --yes to configure without a confirmation\n", err)
			return pluginExitUsage
		}
		if !confirmed {
			report.Message = "not configured"
			if jsonOutput {
				return writeJSON(env, report)
			}
			_, _ = fmt.Fprintln(env.Stdout, "Not configured.")
			return pluginExitOK
		}
	}

	if err := config.SavePlugins(func(plugins *config.PluginsConfig) {
		plugins.External = append(plugins.External, normalized)
	}); err != nil {
		cliErrln(env.Stderr, err)
		return pluginExitFailed
	}
	report.Applied = true
	report.Message = "configured; restart Sidecar to host it"
	if jsonOutput {
		return writeJSON(env, report)
	}
	_, _ = fmt.Fprintf(env.Stdout, "Configured %s in %s. Restart Sidecar to host it.\n", normalized.ID, config.PluginSourceExternal)
	return pluginExitOK
}

func applyAddFlag(env Env, entry *config.PluginInstanceConfig, name, value, help string) int {
	switch name {
	case "--pass-env":
		entry.PassEnv = append(entry.PassEnv, value)
	case "--scope":
		entry.Scope = value
	case "--placement":
		entry.Placements = append(entry.Placements, value)
	case "--claim-host":
		entry.ClaimHosts = append(entry.ClaimHosts, value)
	case "--timeout":
		d, err := time.ParseDuration(value)
		if err != nil {
			cliErrf(env.Stderr, "--timeout %q is not a duration\n\n%s", value, help)
			return pluginExitUsage
		}
		entry.Timeout = d
	}
	return 0
}

// writeAddPlan prints exactly what will run before anything is written. The
// argv is printed one element per line rather than joined, because a joined
// line hides where an argument containing a space begins and ends — and the
// whole point of this screen is that the reader can see precisely what they are
// trusting.
func writeAddPlan(env Env, entry config.PluginInstanceConfig) {
	_, _ = fmt.Fprintf(env.Stdout, "Configure %q as an external plugin.\n\n", entry.ID)
	_, _ = fmt.Fprintln(env.Stdout, "  Sidecar will run, directly and with no shell:")
	for i, arg := range entry.Command {
		_, _ = fmt.Fprintf(env.Stdout, "    %-8s %s\n", "argv["+strconv.Itoa(i)+"]", arg)
	}
	_, _ = fmt.Fprintf(env.Stdout, "  Working directory: %s\n", checkWorkingDir())
	_, _ = fmt.Fprintf(env.Stdout, "  Protocol:          %s\n", pluginhost.Protocol)
	_, _ = fmt.Fprintf(env.Stdout, "  Scope:             %s\n", entry.Scope)
	_, _ = fmt.Fprintf(env.Stdout, "  Placements:        %s\n", strings.Join(entry.Placements, ", "))
	_, _ = fmt.Fprintf(env.Stdout, "  Timeout:           %s\n", entry.Timeout)
	if len(entry.PassEnv) > 0 {
		_, _ = fmt.Fprintf(env.Stdout, "  Passed variables:  %s (names only; values are never stored)\n", strings.Join(entry.PassEnv, ", "))
	}
	if len(entry.ClaimHosts) > 0 {
		_, _ = fmt.Fprintf(env.Stdout, "  Claimed hosts:     %s\n", strings.Join(entry.ClaimHosts, ", "))
	}
	_, _ = fmt.Fprintln(env.Stdout)
	_, _ = fmt.Fprintln(env.Stdout, "  A process boundary is crash isolation, not a sandbox: this executable")
	_, _ = fmt.Fprintln(env.Stdout, "  will run with your full OS privileges.")
	_, _ = fmt.Fprintln(env.Stdout)
	_, _ = fmt.Fprint(env.Stdout, "Configure it? [y/N] ")
}

func confirmPluginAdd(env Env) (bool, error) {
	if env.Stdin == nil {
		return false, fmt.Errorf("there is no input to confirm on")
	}
	line, err := bufio.NewReader(env.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false, fmt.Errorf("the confirmation could not be read")
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func runPluginRemove(env Env, args []string) int {
	return runPluginConfigVerb(env, args, "remove", "removed", func(instance config.PluginInstance) func(*config.PluginsConfig) {
		return func(plugins *config.PluginsConfig) {
			kept := make([]config.PluginInstanceConfig, 0, len(plugins.External))
			for _, entry := range plugins.External {
				if entry.ID != instance.ID {
					kept = append(kept, entry)
				}
			}
			if len(kept) == 0 {
				plugins.External = nil
				return
			}
			plugins.External = kept
		}
	})
}

func runPluginEnable(env Env, args []string) int {
	return runPluginConfigVerb(env, args, "enable", "enabled", func(instance config.PluginInstance) func(*config.PluginsConfig) {
		return setPluginEnabled(instance.ID, true)
	})
}

func runPluginDisable(env Env, args []string) int {
	return runPluginConfigVerb(env, args, "disable", "disabled", func(instance config.PluginInstance) func(*config.PluginsConfig) {
		return setPluginEnabled(instance.ID, false)
	})
}

func setPluginEnabled(id string, enabled bool) func(*config.PluginsConfig) {
	return func(plugins *config.PluginsConfig) {
		for i := range plugins.External {
			if plugins.External[i].ID == id {
				plugins.External[i].Enabled = enabled
				return
			}
		}
	}
}

// runPluginConfigVerb is remove, enable, and disable: the same parse, the same
// lookup, the same refusals, one mutation each.
//
// A terminalResources entry is refused rather than edited. That section belongs
// to the frozen resource protocol and `sidecar terminal-links` is its surface;
// editing it from here would mean two commands owning one section.
func runPluginConfigVerb(env Env, args []string, verb, past string, plan func(config.PluginInstance) func(*config.PluginsConfig)) int {
	cmd := RootCommand().FindSubcommand("plugin").FindSubcommand(verb)
	help := RenderHelp(cmd)

	jsonOutput := false
	var positional []string
	for _, arg := range args {
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return pluginExitOK
		case arg == "--json":
			jsonOutput = true
		case strings.HasPrefix(arg, "-"):
			cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
			return pluginExitUsage
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		cliErrf(env.Stderr, "plugin %s takes exactly one plugin id\n\n%s", verb, help)
		return pluginExitUsage
	}

	cfg, ok := loadPluginConfig(env)
	if !ok {
		return pluginExitFailed
	}
	if !requireProtocolFlag(env, nil) {
		return pluginExitRefused
	}
	instance, found := cfg.PluginInstance(positional[0])
	if !found {
		cliErrf(env.Stderr, "no plugin named %q is configured in plugins.external or terminalResources.providers\n", positional[0])
		return pluginExitNotFound
	}
	if instance.IsLegacyResourceProvider() {
		cliErrf(env.Stderr, "%q is configured in %s, which this verb does not own; edit that section or use `sidecar terminal-links`\n",
			instance.ID, instance.Source)
		return pluginExitRefused
	}

	if err := config.SavePlugins(plan(instance)); err != nil {
		cliErrln(env.Stderr, err)
		return pluginExitFailed
	}

	report := pluginConfigReport{
		ID: instance.ID, Action: verb, Source: instance.Source,
		Enabled: verb == "enable", Applied: true,
		Message: past + "; restart Sidecar for it to take effect",
	}
	if jsonOutput {
		return writeJSON(env, report)
	}
	_, _ = fmt.Fprintf(env.Stdout, "%s %s. Restart Sidecar for it to take effect.\n", instance.ID, past)
	return pluginExitOK
}

// runPluginChanged writes one plugin-changed request onto the bus.
//
// It starts nothing and asks nothing. The request says only that a plugin's
// data moved; every running instance decides for itself whether it has a tab of
// that plugin on screen, and only such a tab spends a process. That is what
// makes this safe to call from a shell hook after every `dex log`.
func runPluginChanged(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("plugin").FindSubcommand("changed")
	help := RenderHelp(cmd)

	jsonOutput := false
	collection := ""
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			_, _ = fmt.Fprint(env.Stdout, help)
			return pluginExitOK
		case arg == "--json":
			jsonOutput = true
		case arg == "--collection" || strings.HasPrefix(arg, "--collection="):
			val, next, ok := takeFlagArg(arg, args, i, "--collection")
			if !ok || val == "" {
				cliErrf(env.Stderr, "--collection requires a collection id\n\n%s", help)
				return pluginExitUsage
			}
			collection = val
			i = next
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
				return pluginExitUsage
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		cliErrf(env.Stderr, "changed requires exactly one plugin id\n\n%s", help)
		return pluginExitUsage
	}
	instance := strings.TrimSpace(positional[0])
	payload, err := json.Marshal(uirequest.PluginChangedPayload{Instance: instance, Collection: collection})
	if err != nil {
		cliErrln(env.Stderr, err)
		return pluginExitFailed
	}
	if _, err := uirequest.DecodePluginChangedPayload(payload); err != nil {
		cliErrf(env.Stderr, "validation error: %v\n\n%s", err, help)
		return pluginExitUsage
	}

	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	origin := uirequest.Origin{PID: os.Getpid()}
	if dest, derr := resolveOpenDestination(ctx, env.StateDir, "", "", resolveProjectOnly); derr == nil {
		origin = dest.Origin
	}
	req := uirequest.Request{
		Version:   1,
		ID:        uirequest.NewRequestID(),
		CreatedAt: time.Now().UTC(),
		TTLMs:     int(uirequest.DefaultTTL / time.Millisecond),
		Origin:    origin,
		Action:    uirequest.ActionPluginChanged,
		Payload:   payload,
	}
	if _, err := uirequest.WriteRequest(env.StateDir, req); err != nil {
		cliErrln(env.Stderr, err)
		return pluginExitFailed
	}
	if jsonOutput {
		out := struct {
			ID         string `json:"id"`
			Instance   string `json:"instance"`
			Collection string `json:"collection,omitempty"`
		}{ID: req.ID, Instance: instance, Collection: collection}
		if err := json.NewEncoder(env.Stdout).Encode(out); err != nil {
			cliErrln(env.Stderr, err)
			return pluginExitFailed
		}
		return pluginExitOK
	}
	what := instance
	if collection != "" {
		what = instance + "/" + collection
	}
	_, _ = fmt.Fprintf(env.Stdout, "Told running Sidecar instances that %s changed.\n", what)
	return pluginExitOK
}
