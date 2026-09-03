package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/uirequest"
)

func runOpen(env Env, args []string) int {
	openCmd := RootCommand().FindSubcommand("open")
	openHelp := RenderHelp(openCmd)

	jsonOutput := false
	wantDiff := false
	splitMode := "auto"
	splitSet := false
	atCell := ""
	atSeen := false
	waitDuration := 1200 * time.Millisecond
	lineNo := 0
	shellFlag := ""
	projectFlag := ""
	providerFlag := ""
	pluginFlag := ""
	collectionFlag := ""
	queryFlag := ""
	querySet := false
	var filterFlags map[string]string
	sessions := false
	sessionsRow := ""
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			_, _ = fmt.Fprint(env.Stdout, openHelp)
			return 0
		case arg == "--json":
			jsonOutput = true
		case arg == "--diff":
			wantDiff = true
		case arg == "--provider":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "--provider requires a provider instance id\n\n%s", openHelp)
				return 2
			}
			i++
			providerFlag = args[i]
			if providerFlag == "" {
				cliErrf(env.Stderr, "--provider requires a provider instance id\n\n%s", openHelp)
				return 2
			}
		case strings.HasPrefix(arg, "--provider="):
			providerFlag = strings.TrimPrefix(arg, "--provider=")
			if providerFlag == "" {
				cliErrf(env.Stderr, "--provider requires a provider instance id\n\n%s", openHelp)
				return 2
			}
		case arg == "--plugin" || strings.HasPrefix(arg, "--plugin="):
			val, next, ok := takeFlagArg(arg, args, i, "--plugin")
			if !ok || val == "" {
				cliErrf(env.Stderr, "--plugin requires a plugin instance id\n\n%s", openHelp)
				return 2
			}
			pluginFlag = val
			i = next
		case arg == "--collection" || strings.HasPrefix(arg, "--collection="):
			val, next, ok := takeFlagArg(arg, args, i, "--collection")
			if !ok || val == "" {
				cliErrf(env.Stderr, "--collection requires a collection id\n\n%s", openHelp)
				return 2
			}
			collectionFlag = val
			i = next
		case arg == "--query" || strings.HasPrefix(arg, "--query="):
			val, next, ok := takeFlagArg(arg, args, i, "--query")
			if !ok {
				cliErrf(env.Stderr, "--query requires a value\n\n%s", openHelp)
				return 2
			}
			queryFlag, querySet = val, true
			i = next
		case arg == "--filter" || strings.HasPrefix(arg, "--filter="):
			val, next, ok := takeFlagArg(arg, args, i, "--filter")
			if !ok {
				cliErrf(env.Stderr, "--filter requires id=value\n\n%s", openHelp)
				return 2
			}
			i = next
			id, value, ok := parseFilterFlag(val)
			if !ok {
				cliErrf(env.Stderr, "--filter takes id=value, not %q\n\n%s", val, openHelp)
				return 2
			}
			if filterFlags == nil {
				filterFlags = map[string]string{}
			}
			filterFlags[id] = value
		case arg == "--shell":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "--shell requires a shell name\n\n%s", openHelp)
				return 2
			}
			i++
			shellFlag = args[i]
			if shellFlag == "" {
				cliErrf(env.Stderr, "--shell requires a shell name\n\n%s", openHelp)
				return 2
			}
		case strings.HasPrefix(arg, "--shell="):
			shellFlag = strings.TrimPrefix(arg, "--shell=")
			if shellFlag == "" {
				cliErrf(env.Stderr, "--shell requires a shell name\n\n%s", openHelp)
				return 2
			}
		case arg == "--project":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "--project requires a project name\n\n%s", openHelp)
				return 2
			}
			i++
			projectFlag = args[i]
			if projectFlag == "" {
				cliErrf(env.Stderr, "--project requires a project name\n\n%s", openHelp)
				return 2
			}
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
			if projectFlag == "" {
				cliErrf(env.Stderr, "--project requires a project name\n\n%s", openHelp)
				return 2
			}
		case arg == "--sessions" || strings.HasPrefix(arg, "--sessions="):
			sessions = true
			if strings.HasPrefix(arg, "--sessions=") {
				sessionsRow = strings.TrimPrefix(arg, "--sessions=")
			}
			// Unlike layout, open requires a positional target. A following
			// bare word is that target, not an optional ROW. Name the row
			// with --sessions=ID.
		case arg == "--line":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "--line requires a line number argument\n\n%s", openHelp)
				return 2
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				cliErrf(env.Stderr, "invalid line number %q\n\n%s", args[i], openHelp)
				return 2
			}
			lineNo = n
		case strings.HasPrefix(arg, "--line="):
			val := strings.TrimPrefix(arg, "--line=")
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				cliErrf(env.Stderr, "invalid line number %q\n\n%s", val, openHelp)
				return 2
			}
			lineNo = n
		case arg == "--split":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "--split requires an argument (auto|right|below)\n\n%s", openHelp)
				return 2
			}
			i++
			splitMode = strings.ToLower(args[i])
			splitSet = true
			if splitMode != "auto" && splitMode != "right" && splitMode != "below" {
				cliErrf(env.Stderr, "invalid split option %q (must be auto, right, or below)\n\n%s", args[i], openHelp)
				return 2
			}
		case strings.HasPrefix(arg, "--split="):
			splitMode = strings.ToLower(strings.TrimPrefix(arg, "--split="))
			splitSet = true
			if splitMode != "auto" && splitMode != "right" && splitMode != "below" {
				cliErrf(env.Stderr, "invalid split option %q (must be auto, right, or below)\n\n%s", splitMode, openHelp)
				return 2
			}
		case arg == "--at":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "--at requires a grid cell argument (col or col.row)\n\n%s", openHelp)
				return 2
			}
			i++
			atCell = args[i]
			atSeen = true
		case strings.HasPrefix(arg, "--at="):
			atCell = strings.TrimPrefix(arg, "--at=")
			atSeen = true
		case arg == "--wait":
			if i+1 >= len(args) {
				cliErrf(env.Stderr, "--wait requires a duration argument\n\n%s", openHelp)
				return 2
			}
			i++
			d, err := parseWaitDuration(args[i])
			if err != nil {
				cliErrf(env.Stderr, "invalid wait duration %q: %v\n\n%s", args[i], err, openHelp)
				return 2
			}
			waitDuration = d
		case strings.HasPrefix(arg, "--wait="):
			val := strings.TrimPrefix(arg, "--wait=")
			d, err := parseWaitDuration(val)
			if err != nil {
				cliErrf(env.Stderr, "invalid wait duration %q: %v\n\n%s", val, err, openHelp)
				return 2
			}
			waitDuration = d
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, openHelp)
				return 2
			}
			positional = append(positional, arg)
		}
	}

	if sessions && (shellFlag != "" || projectFlag != "") {
		cliErrf(env.Stderr, "--sessions cannot be combined with --shell or --project\n\n%s", openHelp)
		return 2
	}
	// --provider is the older spelling of --plugin's locator form. One instance
	// wins: naming both is a mistake worth saying out loud rather than a
	// precedence rule to remember.
	if pluginFlag != "" && providerFlag != "" && pluginFlag != providerFlag {
		cliErrf(env.Stderr, "--plugin and --provider name different instances; pass one\n\n%s", openHelp)
		return 2
	}
	if pluginFlag == "" {
		pluginFlag = providerFlag
	}
	providerFlag = pluginFlag
	if collectionFlag != "" && pluginFlag == "" {
		cliErrf(env.Stderr, "--collection needs the --plugin whose collection it names\n\n%s", openHelp)
		return 2
	}
	if querySet && collectionFlag == "" {
		cliErrf(env.Stderr, "--query needs a --collection to search\n\n%s", openHelp)
		return 2
	}
	if len(filterFlags) > 0 && collectionFlag == "" {
		cliErrf(env.Stderr, "--filter needs a --collection to narrow\n\n%s", openHelp)
		return 2
	}
	if collectionFlag != "" && wantDiff {
		cliErrf(env.Stderr, "--collection and --diff name different kinds of target\n\n%s", openHelp)
		return 2
	}
	if collectionFlag != "" && lineNo > 0 {
		cliErrf(env.Stderr, "--line does not apply to a plugin collection\n\n%s", openHelp)
		return 2
	}
	if providerFlag != "" && wantDiff {
		cliErrf(env.Stderr, "--provider and --diff name different kinds of target\n\n%s", openHelp)
		return 2
	}
	if providerFlag != "" && lineNo > 0 {
		cliErrf(env.Stderr, "--line does not apply to a provider resource\n\n%s", openHelp)
		return 2
	}

	atCell = strings.TrimSpace(atCell)
	if atSeen && atCell == "" {
		cliErrf(env.Stderr, "--at requires a grid cell argument (col or col.row); omit the flag to auto-place\n\n%s", openHelp)
		return 2
	}
	if atCell != "" {
		if splitSet {
			cliErrf(env.Stderr, "--at and --split are mutually exclusive: --at is a requirement (declines rather than land elsewhere), --split a preference\n\n%s", openHelp)
			return 2
		}
		if _, ok := panelayout.ParseCell(atCell); !ok {
			cliErrf(env.Stderr, "invalid cell %q for --at (use col or col.row, 1-based, like 2.1)\n\n%s", atCell, openHelp)
			return 2
		}
	}

	if wantDiff || collectionFlag != "" {
		// A collection names what to open by itself; a positional is the
		// optional row inside it.
		if len(positional) > 1 {
			cliErrf(env.Stderr, "open accepts at most one target\n\n%s", openHelp)
			return 2
		}
	} else if len(positional) != 1 {
		cliErrf(env.Stderr, "open requires exactly one target (path, td-xxxxxx, or a git spec)\n\n%s", openHelp)
		return 2
	}

	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// open needs no project state directory of its own — it writes a request
	// onto the bus for a running instance — so it resolves without creating
	// one. It is still a mutating verb for the isolation gate: the request bus
	// is state outside this process.
	var dest openDestination
	var err error
	if sessions {
		dest, err = resolveSessionsDestination(ctx, env.StateDir, sessionsRow)
	} else {
		dest, err = resolveOpenDestination(ctx, env.StateDir, shellFlag, projectFlag, resolveProjectOnly)
	}
	if err != nil {
		cliErrln(env.Stderr, err)
		return destExitCode(err)
	}
	if err := refuseRelayIfUnavailable(env.StateDir, dest.Origin); err != nil {
		cliErrln(env.Stderr, err)
		return destExitCode(err)
	}

	raw := ""
	if len(positional) == 1 {
		raw = positional[0]
	}
	if dest.Resolved != uirequest.ResolvedCurrentShell {
		if workDir := resolveTargetWorkDirForDest(env.StateDir, dest, raw); workDir != "" {
			dest.Origin.WorkDir = workDir
		}
	}
	var target uirequest.Target
	if collectionFlag != "" {
		target, err = uirequest.ResolveCollectionTarget(pluginFlag, collectionFlag, queryFlag, raw, filterFlags)
	} else {
		target, err = uirequest.ResolveTarget(dest.Origin.WorkDir, raw, lineNo, uirequest.ResolveOptions{Diff: wantDiff, Provider: providerFlag})
	}
	if err != nil {
		cliErrf(env.Stderr, "validation error: %v\n\n%s", err, openHelp)
		return 2
	}

	// What to call the thing in the human line. A collection tab has no
	// locator, so naming it by its empty Value would print a bare sentence with
	// a hole in it.
	label := target.Value
	if label == "" && target.Collection != "" {
		label = target.Provider + "/" + target.Collection
	}

	options := uirequest.Options{}
	if atCell != "" {
		// A cell replaces any axis preference: it is the whole placement.
		options.At = atCell
	} else if splitMode != "auto" || splitSet {
		options.Split = splitMode
	}
	req := uirequest.Request{
		Version:   1,
		ID:        uirequest.NewRequestID(),
		CreatedAt: time.Now().UTC(),
		TTLMs:     int(uirequest.DefaultTTL / time.Millisecond),
		Origin:    dest.Origin,
		Action:    uirequest.ActionOpen,
		Target:    target,
		Options:   options,
	}

	_, err = uirequest.WriteRequest(env.StateDir, req)
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	if waitDuration <= 0 {
		// Fire-and-forget
		if jsonOutput {
			_ = json.NewEncoder(env.Stdout).Encode(openResult(req, dest, nil))
		} else {
			_, _ = fmt.Fprintf(env.Stdout, "Sent open request for %s.\n", label)
		}
		return 0
	}

	deadline := time.Now().Add(waitDuration)
	var acks []uirequest.Ack
	for time.Now().Before(deadline) {
		found, err := uirequest.ReadAcks(env.StateDir, req.ID, req.Action)
		if err == nil && len(found) > 0 {
			acks = found
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	_ = uirequest.Cleanup(env.StateDir, req.ID, req.Action)

	if len(acks) == 0 {
		if jsonOutput {
			_ = json.NewEncoder(env.Stdout).Encode(openResult(req, dest, nil))
		}
		if dest.Origin.TmuxSession != "" {
			cliErrf(env.Stderr, "no running Sidecar instance is showing this shell (%s)\n", dest.Origin.TmuxSession)
		} else {
			cliErrf(env.Stderr, "no running Sidecar instance is showing this project (%s)\n", dest.Origin.ProjectKey)
		}
		return 3
	}

	hasDeclined := false
	var declineReason string
	hasOpened := false
	allRetargeted := true
	for _, ack := range acks {
		if ack.Status == uirequest.StatusDeclined {
			hasDeclined = true
			if ack.Reason != "" {
				declineReason = ack.Reason
			}
		}
		if ack.Status == uirequest.StatusOpened || ack.Status == uirequest.StatusRetargeted {
			hasOpened = true
			if ack.Status != uirequest.StatusRetargeted {
				allRetargeted = false
			}
		}
	}

	if hasDeclined && !hasOpened {
		if jsonOutput {
			_ = json.NewEncoder(env.Stdout).Encode(openResult(req, dest, acks))
		}
		if declineReason == "" {
			declineReason = "the window is too small to split"
		}
		cliErrf(env.Stderr, "instance declined open request: %s\n", declineReason)
		return 4
	}

	if jsonOutput {
		if err := json.NewEncoder(env.Stdout).Encode(openResult(req, dest, acks)); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		return 0
	}

	shellLabel := dest.DisplayName
	if shellLabel == "" {
		shellLabel = dest.Origin.ProjectKey
	}
	if hasOpened {
		if allRetargeted {
			_, _ = fmt.Fprintf(env.Stdout, "Opened %s in the split already beside %q.\n", label, shellLabel)
		} else {
			_, _ = fmt.Fprintf(env.Stdout, "Opened %s in a split beside %q.\n", label, shellLabel)
		}
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "Queued %s for %q; it opens when the user selects that shell.\n", label, shellLabel)
	}
	return 0
}

func openResult(req uirequest.Request, dest openDestination, acks []uirequest.Ack) uirequest.Result {
	return uirequest.Result{
		Action:    req.Action,
		Target:    req.Target,
		Shell:     dest.Origin.TmuxSession,
		Name:      dest.DisplayName,
		Project:   dest.Origin.ProjectKey,
		Resolved:  dest.Resolved,
		Delivered: len(acks),
		Results:   acks,
	}
}

func parseWaitDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "0" || s == "0s" || s == "0ms" {
		return 0, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Millisecond, nil
	}
	return time.ParseDuration(s)
}
