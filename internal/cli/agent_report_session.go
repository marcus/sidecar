package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/agentcatalog"
	"github.com/marcus/sidecar/internal/agentlifecycle/lifecycleenv"
	"github.com/marcus/sidecar/internal/agentsession"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/shellstate"
)

// maxHookStdinBytes bounds the provider payload read from stdin.
//
// Provider hook input is untrusted local data, and the only fields Sidecar wants
// from it are two short identifiers. A cap here is what stops a provider whose
// payload grows — or a hook wired to the wrong stream — from making a reporting
// command hold an arbitrary amount of memory.
const maxHookStdinBytes = 1 << 20

type reportSessionFlags struct {
	kind      string
	id        string
	path      string
	source    string
	clear     bool
	hookStdin bool
	json      bool
}

func agentReportSessionExitCodes() []ExitCode {
	return []ExitCode{
		{Code: 0, Summary: "recorded, or a no-op outside a Sidecar-managed shell"},
		{Code: 1, Summary: "the binding could not be written"},
		{Code: 2, Summary: "usage error"},
		{Code: 5, Summary: "invalid reference, untrusted source, unusable hook payload, unverifiable shell context, a provider that does not occupy the pane, or a stale provider generation"},
	}
}

func agentReportSessionCommand() *Command {
	return &Command{
		Name:    "report-session",
		Summary: "Bind the pane's exact native agent conversation",
		Usage:   "sidecar agent report-session --kind KIND (--id ID | --path ABS_PATH | --clear) [--source SOURCE] [--hook-stdin] [--json]",
		Long: "Records which of a provider's own conversations is running in this managed shell, so a cold restart can " +
			"offer to resume that exact conversation and `agent read --source transcript` can return it.\n\n" +
			"This is an integration surface rather than a coordination command: it is meant to be called by a provider " +
			"hook, and it fails open exactly like `agent report` — outside a Sidecar-managed shell it exits 0, prints " +
			"nothing, and records nothing.\n\n" +
			"The shell it binds is derived from the managed-shell environment and verified against live tmux. A hook " +
			"chooses only which conversation it is talking about; it can never select another shell, pane, host, or " +
			"tmux server through a flag.\n\n" +
			"A report wins only if it comes from the provider process that currently occupies the pane. Late output " +
			"from an exited or replaced provider is ignored rather than allowed to overwrite its successor's binding.\n\n" +
			"--kind is a claim, not evidence. Some providers read another provider's settings file on purpose, so an " +
			"installed hook can fire under an agent it was not installed for. A kind that does not match the pane's own " +
			"provider is refused rather than bound, because a wrong binding would offer to resume one agent's " +
			"conversation with a different agent.\n\n" +
			"Only an official Sidecar integration source may set an auto-resumable reference. A path reference must be " +
			"absolute, normalized, and inside one of that provider's approved conversation store roots. Session values " +
			"are never interpolated into a shell command, and never appear in ordinary `agent list` output.",
		Flags: []Flag{
			{Name: "--kind", Arg: "KIND", Summary: "Catalog agent kind, e.g. codex or claude (required)"},
			{Name: "--id", Arg: "ID", Summary: "Provider session identifier"},
			{Name: "--path", Arg: "ABS_PATH", Summary: "Absolute path to the provider's own transcript file"},
			{Name: "--clear", Summary: "Remove the shell's session binding", Bool: true},
			{Name: "--source", Arg: "SOURCE", Summary: "Reporting integration source; defaults to this provider's official source"},
			{Name: "--hook-stdin", Summary: "Read the provider's hook payload as bounded JSON on stdin", Bool: true},
			{Name: "--json", Summary: "Write stable structured JSON", Bool: true},
			{Name: "--help", Short: "-h", Summary: "Show this help", Bool: true},
		},
		ExitCodes: agentReportSessionExitCodes(),
		Examples: []Example{
			{Command: "sidecar agent report-session --kind codex --id 019f2c8a-1d4e-7b02-9c11-6f3a0b7d2e55"},
			{Command: "sidecar agent report-session --kind claude --hook-stdin"},
			{Command: "sidecar agent report-session --kind codex --clear"},
		},
		Mutates: true,
		Run:     runAgentReportSession,
	}
}

// reportSessionResult is the JSON contract of a report-session call.
//
// It never carries the reference value. The command's own caller already knows
// what it reported, and a value here would be a conversation identifier written
// to whatever captures the hook's output.
type reportSessionResult struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Managed       bool                   `json:"managed"`
	Decision      agentsession.Decision  `json:"decision,omitempty"`
	Kind          string                 `json:"kind,omitempty"`
	RefKind       agentsession.RefKind   `json:"refKind,omitempty"`
	Reported      bool                   `json:"reported,omitempty"`
	Bound         bool                   `json:"bound"`
	Shell         string                 `json:"shell,omitempty"`
	Conflicts     []reportSessionConflct `json:"conflicts,omitempty"`
	Note          string                 `json:"note,omitempty"`
}

// reportSessionConflct names another managed shell already claiming this exact
// conversation. Deduplication is global per host, so the report is still
// recorded — the conflict decides who may resume, and hiding it would leave a
// user with two shells that both look resumable.
type reportSessionConflct struct {
	Project string `json:"project,omitempty"`
	Shell   string `json:"shell"`
	Name    string `json:"name,omitempty"`
}

type reportSessionError struct {
	SchemaVersion int    `json:"schemaVersion"`
	Code          string `json:"code"`
	Message       string `json:"message"`
}

// reportSessionSchemaVersion is the wire version of the two contracts above.
const reportSessionSchemaVersion = 1

func parseReportSessionFlags(env Env, args []string, help string) (reportSessionFlags, int) {
	var f reportSessionFlags
	usage := func(format string, a ...any) int {
		cliErrf(env.Stderr, format+"\n\n%s", append(a, help)...)
		return 2
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return f, 0
		case arg == "--json":
			f.json = true
		case arg == "--clear":
			f.clear = true
		case arg == "--hook-stdin":
			f.hookStdin = true
		case strings.HasPrefix(arg, "--kind"):
			v, n, ok := takeFlagArg(arg, args, i, "--kind")
			if !ok {
				return f, usage("--kind requires a value")
			}
			f.kind, i = v, n
		case strings.HasPrefix(arg, "--id"):
			v, n, ok := takeFlagArg(arg, args, i, "--id")
			if !ok {
				return f, usage("--id requires a value")
			}
			f.id, i = v, n
		case strings.HasPrefix(arg, "--path"):
			v, n, ok := takeFlagArg(arg, args, i, "--path")
			if !ok {
				return f, usage("--path requires a value")
			}
			f.path, i = v, n
		case strings.HasPrefix(arg, "--source"):
			v, n, ok := takeFlagArg(arg, args, i, "--source")
			if !ok {
				return f, usage("--source requires a value")
			}
			f.source, i = v, n
		default:
			return f, usage("unknown argument %q", arg)
		}
	}
	if strings.TrimSpace(f.kind) == "" {
		return f, usage("--kind is required")
	}
	// The three ways to name a conversation are mutually exclusive. Accepting
	// two and picking one would make the command's behavior depend on an
	// argument order the caller cannot see.
	chosen := 0
	for _, set := range []bool{strings.TrimSpace(f.id) != "", strings.TrimSpace(f.path) != "", f.clear, f.hookStdin} {
		if set {
			chosen++
		}
	}
	if chosen == 0 {
		return f, usage("one of --id, --path, --clear, or --hook-stdin is required")
	}
	if chosen > 1 {
		return f, usage("--id, --path, --clear, and --hook-stdin are mutually exclusive")
	}
	return f, -1
}

// hookPayload is the subset of a provider hook's stdin JSON that Sidecar reads.
//
// Codex and Claude Code both deliver these field names, which is why one shape
// serves both. Everything else in the payload — prompt text, tool arguments,
// model names — is deliberately not decoded: a field this struct does not name
// is a field that cannot reach a Sidecar record.
type hookPayload struct {
	SessionID string `json:"session_id"`
	// SessionIDCamel is the same field under the spelling Devin also uses.
	//
	// Devin is the first provider whose payload is not uniformly snake_case:
	// Herdr's own devin asset reads "session_id" and "sessionId" in that order
	// and takes whichever is a non-empty string, which is a statement that a
	// released Devin has been seen writing the camelCase one. Reading only
	// snake_case would leave the pane unbound on exactly those payloads, and
	// silently, because a hook that finds no session records nothing by design.
	// Every other provider omits it, so the second spelling costs nothing.
	SessionIDCamel string `json:"sessionId"`
	TranscriptPath string `json:"transcript_path"`
	HookEventName  string `json:"hook_event_name"`
	// AgentID is set when the event belongs to a sub-agent rather than the
	// conversation occupying the pane.
	AgentID string `json:"agent_id"`
}

// sessionID is the conversation identifier the payload names, under either
// spelling. Snake_case wins when a payload carries both, which is the order
// upstream reads them in.
func (p hookPayload) sessionID() string {
	if id := strings.TrimSpace(p.SessionID); id != "" {
		return id
	}
	return strings.TrimSpace(p.SessionIDCamel)
}

// readHookPayload decodes a bounded provider payload from stdin.
func readHookPayload(r io.Reader) (hookPayload, error) {
	var p hookPayload
	if r == nil {
		return p, errors.New("no hook payload was provided on stdin")
	}
	data, err := io.ReadAll(io.LimitReader(r, maxHookStdinBytes+1))
	if err != nil {
		return p, fmt.Errorf("could not read the hook payload: %w", err)
	}
	if len(data) > maxHookStdinBytes {
		return p, fmt.Errorf("the hook payload is larger than the %d byte cap", maxHookStdinBytes)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return p, errors.New("the hook payload on stdin was empty")
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("the hook payload is not valid JSON: %w", err)
	}
	return p, nil
}

// resolveReportedKind canonicalizes a hook's --kind claim through the catalog.
//
// It runs before the claim is compared to the pane's occupant, and that order is
// the point. The comparison is a string equality against a resolved process
// name, so without a lookup first an identifier that names no provider at all —
// a typo, or an adapter-vocabulary alias like "claude-code" — came back as
// kind_mismatch: "this pane runs claude, but the report claims claude-code".
// That names the wrong problem twice, since the pane's provider was never in
// question and the two spellings are the same provider.
//
// One lookup answers both halves. An alias becomes its canonical id and is
// checked normally; an identifier the catalog does not know is refused as
// unsupported_kind, which is the truth about it, and is refused without
// consulting the pane at all. Every shipped integration passes a canonical id
// already (hookconfig writes them), so this changes no working path — it makes
// the broken ones say what is broken.
// A DETECTION-ONLY FAMILY COUNTS, and the reason is that this verb answers a
// question about a pane rather than about a launch. agentcatalog.Lookup searches
// the launchable families and their aliases only, which was right while every
// shipped integration belonged to one; the Kilo port broke that assumption. Kilo
// is a family Sidecar recognises in a pane and cannot yet start, and its plugin
// is a Sidecar-installed integration reporting from a pane the user launched
// themselves. Refusing it here made the asset's binding fail on every turn --
// exit 5, "kilo is not an agent kind Sidecar knows" -- while its state reports,
// which take no --kind, were accepted. Found by the live proof; no test could
// have caught it, because every test in the tree passed a launchable id.
//
// FindDetection is consulted after Lookup, not instead of it, so a launchable
// family's aliases keep resolving through the path that already handled them.
// When the catalog gains launch configuration for these families the fallback
// simply stops being reached.
func resolveReportedKind(claim string) (string, error) {
	claim = strings.TrimSpace(claim)
	if family, ok := agentcatalog.Lookup(claim); ok {
		return family.ID, nil
	}
	if family, ok := agentcatalog.FindDetection(claim); ok {
		return family.ID, nil
	}
	return "", fmt.Errorf("%w: %q is not an agent kind Sidecar knows",
		agentsession.ErrUnsupportedKind, claim)
}

func runAgentReportSession(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("agent").FindSubcommand("report-session")
	help := RenderHelp(cmd)
	f, code := parseReportSessionFlags(env, args, help)
	if code >= 0 {
		return code
	}

	stateDir := env.StateDir
	if stateDir == "" {
		stateDir = config.StateDir()
	}

	ctx, err := lifecycleenv.Resolve(stateDir)
	if err != nil {
		return emitReportSessionError(env, f.json, "invalid_context", err)
	}
	if !ctx.Managed {
		// Fail open. A hook that runs outside a managed shell has nothing to
		// say and must not make noise or fail the provider's operation.
		return emitReportSessionResult(env, f.json, reportSessionResult{
			SchemaVersion: reportSessionSchemaVersion,
			Managed:       false,
			Note:          "not inside a Sidecar-managed shell; nothing was recorded",
		})
	}

	kind, kindErr := resolveReportedKind(f.kind)
	if kindErr != nil {
		return emitReportSessionError(env, f.json, reportSessionCode(kindErr), kindErr)
	}

	// --kind describes the settings file the hook was installed in, not the
	// process that ran it, and those differ: grok reads ~/.claude/settings.json
	// for Claude Code compatibility, so Sidecar's Claude entry fires inside grok
	// sessions carrying --kind claude. Check the claim against the pane's actual
	// occupant before anything reads a payload or touches the manifest — a report
	// from the wrong provider has nothing to say about this pane's conversation,
	// and binding it would make a cold restore offer `claude --resume` on a grok
	// session id.
	if err := lifecycleenv.VerifyReportedKind(ctx.PanePID, kind); err != nil {
		return emitReportSessionError(env, f.json, "kind_mismatch", err)
	}

	refKind := agentsession.RefID
	value := strings.TrimSpace(f.id)

	if f.hookStdin {
		payload, err := readHookPayload(env.Stdin)
		if err != nil {
			return emitReportSessionError(env, f.json, "invalid_payload", err)
		}
		if strings.TrimSpace(payload.AgentID) != "" {
			// A sub-agent event describes a nested conversation, not the one
			// occupying the pane. Binding the pane to it would make a restore
			// resume the wrong side of the conversation.
			return emitReportSessionResult(env, f.json, reportSessionResult{
				SchemaVersion: reportSessionSchemaVersion,
				Managed:       true,
				Kind:          kind,
				Note:          "the payload belongs to a sub-agent; the pane's own binding was left alone",
			})
		}
		switch {
		case payload.sessionID() != "":
			refKind, value = agentsession.RefID, payload.sessionID()
		case strings.TrimSpace(payload.TranscriptPath) != "":
			refKind, value = agentsession.RefPath, strings.TrimSpace(payload.TranscriptPath)
		default:
			return emitReportSessionResult(env, f.json, reportSessionResult{
				SchemaVersion: reportSessionSchemaVersion,
				Managed:       true,
				Kind:          kind,
				Note:          "the payload named no session; nothing was recorded",
			})
		}
	} else if strings.TrimSpace(f.path) != "" {
		refKind, value = agentsession.RefPath, strings.TrimSpace(f.path)
	}

	source := strings.TrimSpace(f.source)
	if source == "" {
		source = agentsession.OfficialSourceFor(kind)
	}
	if source == "" {
		// Without this the validator refuses with "the source is empty", which
		// describes the symptom rather than the cause: Sidecar ships no official
		// integration for this provider, so there is no source to default to.
		return emitReportSessionError(env, f.json, "unsupported_kind",
			fmt.Errorf("%w: Sidecar has no official integration for %q, so --source must be given explicitly",
				agentsession.ErrUnsupportedKind, kind))
	}

	// Resolve the manifest that owns this shell through the same resolver the
	// other agent commands use, so there is one answer to "which project is
	// this shell in".
	tgt, code, err := findShellTarget(env, ctx.Session, "", "", false, ctx.Namespace)
	if err != nil || code > 0 {
		if err == nil {
			err = fmt.Errorf("this shell is not a registered Sidecar managed shell")
		}
		return emitReportSessionError(env, f.json, "invalid_context", err)
	}

	live, err := lifecycleenv.LiveProviderGeneration(ctx.PanePID)
	if err != nil {
		return emitReportSessionError(env, f.json, "invalid_context",
			fmt.Errorf("could not determine which process occupies the pane: %w", err))
	}

	// The reporter's own generation is derived strictly, with no fallback to the
	// pane's root process. ctx.ProcessGeneration would fall back, and a report
	// left behind by an exited provider falls back to exactly the value
	// LiveProviderGeneration returns when the pane is empty — so the fence would
	// compare two fallbacks, find them equal, and accept the late report it
	// exists to reject.
	reporting, err := lifecycleenv.ReportingProviderGeneration(ctx.PanePID)
	if err != nil {
		return emitReportSessionError(env, f.json, "stale_generation",
			fmt.Errorf("%w: %v", agentsession.ErrStaleGeneration, err))
	}

	update := shellstate.SessionUpdate{
		Kind: kind,
		Live: live,
		Now:  time.Now,
	}
	if f.clear {
		update.Clear = true
		update.Ref = agentsession.Ref{Generation: reporting, Source: source}
	} else {
		ref, err := agentsession.Validate(agentsession.Report{
			Kind:       kind,
			RefKind:    refKind,
			Value:      value,
			Source:     source,
			Generation: reporting,
		}, agentsession.OSRoots(), time.Now)
		if err != nil {
			return emitReportSessionError(env, f.json, reportSessionCode(err), err)
		}
		update.Ref = ref
	}

	id := shellstate.Identity{TmuxName: tgt.Session, Namespace: tgt.Namespace}
	out, err := shellstate.BindSessionAtPath(tgt.ManifestPath, id, update)
	if err != nil {
		if errors.Is(err, agentsession.ErrStaleGeneration) {
			// A late report is not a failure of the hook's process; it simply
			// lost. Say so in JSON and exit 5 so an integration test can tell
			// the difference, but write nothing.
			return emitReportSessionError(env, f.json, "stale_generation", err)
		}
		return emitReportSessionError(env, f.json, reportSessionCode(err), err)
	}

	res := reportSessionResult{
		SchemaVersion: reportSessionSchemaVersion,
		Managed:       true,
		Decision:      out.Decision,
		Kind:          out.Kind,
		Shell:         tgt.Session,
		Bound:         out.Ref != nil && !out.Ref.Empty(),
	}
	if out.Ref != nil && !out.Ref.Empty() {
		res.RefKind = out.Ref.Kind
		res.Reported = out.Ref.Reported
		res.Conflicts = reportSessionConflicts(stateDir, tgt, out)
	}
	return emitReportSessionResult(env, f.json, res)
}

// reportSessionConflicts finds other managed shells on this host already bound
// to the same conversation.
func reportSessionConflicts(stateDir string, tgt shellTarget, out shellstate.SessionOutcome) []reportSessionConflct {
	if out.Ref == nil || out.Ref.Empty() {
		return nil
	}
	projects, err := loadRegisteredProjects(stateDir)
	if err != nil {
		return nil
	}
	var holders []agentsession.Holder
	for _, proj := range projects {
		path := projectManifestPath(proj)
		if path == "" {
			continue
		}
		found, err := shellstate.SessionHoldersAtPath(path, proj.Key)
		if err != nil {
			continue
		}
		holders = append(holders, found...)
	}
	want := agentsession.DedupKey(out.Kind, *out.Ref)
	var conflicts []reportSessionConflct
	for _, h := range holders {
		if h.Session == tgt.Session {
			continue
		}
		if agentsession.DedupKey(h.Kind, h.Ref) != want {
			continue
		}
		conflicts = append(conflicts, reportSessionConflct{Project: h.Project, Shell: h.Session, Name: h.Name})
	}
	return conflicts
}

func reportSessionCode(err error) string {
	switch {
	case errors.Is(err, agentsession.ErrKindMismatch):
		return "kind_mismatch"
	case errors.Is(err, agentsession.ErrStaleGeneration):
		return "stale_generation"
	case errors.Is(err, agentsession.ErrUntrustedSource):
		return "untrusted_source"
	case errors.Is(err, agentsession.ErrOutsideStoreRoot):
		return "outside_store_root"
	case errors.Is(err, agentsession.ErrUnsupportedKind):
		return "unsupported_kind"
	case errors.Is(err, agentsession.ErrInvalidRef):
		return "invalid_reference"
	default:
		return "store_failed"
	}
}

func emitReportSessionResult(env Env, jsonOutput bool, res reportSessionResult) int {
	if jsonOutput {
		if err := json.NewEncoder(env.Stdout).Encode(res); err != nil {
			cliErrln(env.Stderr, err.Error())
			return 1
		}
		return 0
	}
	// Success is silent, for the same reason `agent report` is: this runs from
	// a provider hook inside the user's own agent terminal, and a line of
	// output per session event would be a regression Sidecar caused.
	return 0
}

func emitReportSessionError(env Env, jsonOutput bool, code string, err error) int {
	if jsonOutput {
		_ = json.NewEncoder(env.Stderr).Encode(reportSessionError{
			SchemaVersion: reportSessionSchemaVersion,
			Code:          code,
			Message:       err.Error(),
		})
	} else {
		cliErrln(env.Stderr, err.Error())
	}
	if code == "store_failed" {
		return 1
	}
	return exitInputRejected
}
