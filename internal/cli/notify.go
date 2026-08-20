package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/uirequest"
)

// notifyWait is how long a post or dismiss waits for a running instance to
// acknowledge before falling back to writing the log directly. It is shorter
// than `open`'s because nothing is being placed on screen for the caller to
// look at: the notification is filed either way.
const notifyWait = 900 * time.Millisecond

func runNotifyRoot(env Env, args []string) int {
	cmd := RootCommand().FindSubcommand("notify")
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprint(env.Stdout, RenderHelp(cmd))
		return 0
	}
	sub := cmd.FindSubcommand(args[0])
	if sub != nil && sub.Run != nil {
		return sub.Run(env, args[1:])
	}
	cliErrf(env.Stderr, "unknown notify command %q\n\n%s", args[0], RenderHelp(cmd))
	return 2
}

func runNotifyPost(env Env, args []string) int {
	help := RenderHelp(RootCommand().FindSubcommand("notify").FindSubcommand("post"))

	jsonOutput := false
	body := ""
	source := string(notify.SourceAgent)
	expiry := ""
	var targetSpecs []string
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := func(flag string) (string, bool) {
			if strings.HasPrefix(arg, flag+"=") {
				return strings.TrimPrefix(arg, flag+"="), true
			}
			if arg == flag {
				if i+1 >= len(args) {
					return "", false
				}
				i++
				return args[i], true
			}
			return "", false
		}
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return 0
		case arg == "--json":
			jsonOutput = true
		case arg == "--body" || strings.HasPrefix(arg, "--body="):
			v, ok := value("--body")
			if !ok {
				cliErrf(env.Stderr, "--body requires text\n\n%s", help)
				return 2
			}
			body = v
		case arg == "--source" || strings.HasPrefix(arg, "--source="):
			v, ok := value("--source")
			if !ok {
				cliErrf(env.Stderr, "--source requires a source id\n\n%s", help)
				return 2
			}
			source = v
		case arg == "--expiry" || strings.HasPrefix(arg, "--expiry="):
			v, ok := value("--expiry")
			if !ok {
				cliErrf(env.Stderr, "--expiry requires a duration\n\n%s", help)
				return 2
			}
			expiry = v
		case arg == "--target" || strings.HasPrefix(arg, "--target="):
			v, ok := value("--target")
			if !ok {
				cliErrf(env.Stderr, "--target requires kind:value[:line][@project]\n\n%s", help)
				return 2
			}
			targetSpecs = append(targetSpecs, v)
		default:
			if strings.HasPrefix(arg, "-") {
				cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
				return 2
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		cliErrf(env.Stderr, "notify post requires exactly one title\n\n%s", help)
		return 2
	}
	if !notify.ValidSource(notify.SourceID(source)) {
		cliErrf(env.Stderr, "unknown source %q (one of: %s)\n\n%s", source, strings.Join(notify.SourceIDs(), ", "), help)
		return 2
	}

	// Targets are parsed before anything is posted: a malformed target is a
	// usage error the agent can fix, not a notification that quietly does less
	// than it says.
	targets, err := notify.ParseTargetSpecs(targetSpecs)
	if err != nil {
		cliErrf(env.Stderr, "%s\n\n%s", err, help)
		return 2
	}

	n := notify.Notification{
		ID:      notify.NewID(),
		Source:  notify.SourceID(source),
		Title:   strings.TrimSpace(positional[0]),
		Body:    body,
		Origin:  notifyOrigin(env),
		Sticky:  false,
		Targets: targets,
	}
	switch strings.TrimSpace(strings.ToLower(expiry)) {
	case "":
		// The source's default expiry, applied when the record is stored.
	case "0", "never", "sticky":
		n.Sticky = true
	default:
		d, err := time.ParseDuration(expiry)
		if err != nil || d < 0 {
			cliErrf(env.Stderr, "invalid expiry %q (a duration such as 10s, or \"never\")\n\n%s", expiry, help)
			return 2
		}
		exp := time.Now().UTC().Add(d)
		n.ExpiresAt = &exp
	}
	// The user's per-source expiries live in config, and this process is the
	// one completing the record — without this a `sidecar notify post` would
	// carry the built-in default while the TUI used the configured one.
	if cfg, err := config.Load(); err == nil {
		notify.ApplyConfig(cfg.Notifications)
	}
	n = notify.Normalize(n, time.Now())

	payload, err := json.Marshal(n)
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}

	delivered, outcome := notifyDeliver(env, uirequest.Request{
		Origin:  originForRequest(n.Origin),
		Action:  uirequest.ActionNotify,
		Target:  uirequest.Target{Kind: uirequest.TargetKindNotification},
		Payload: payload,
	})

	if !delivered {
		// No instance took it. The notification is not lost: it goes straight
		// into the log the next Sidecar start reads.
		store, err := notify.Open(env.StateDir)
		if err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		defer func() { _ = store.Close() }()
		if _, err := store.Post(n); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
	}

	if jsonOutput {
		return writeNotifyJSON(env, notifyResult{Action: "post", ID: n.ID, Delivered: delivered, Notification: &n})
	}
	if delivered {
		_, _ = fmt.Fprintf(env.Stdout, "Posted %s (%s).\n", n.ID, n.Source)
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "Stored %s (%s); %s\n", n.ID, n.Source, outcome.explain())
	}
	return 0
}

func runNotifyDismiss(env Env, args []string) int {
	help := RenderHelp(RootCommand().FindSubcommand("notify").FindSubcommand("dismiss"))

	jsonOutput := false
	var positional []string
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
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		cliErrf(env.Stderr, "notify dismiss requires exactly one notification id\n\n%s", help)
		return 2
	}
	id := positional[0]

	all, err := notify.ReadAll(notify.Path(env.StateDir))
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}
	var target notify.Notification
	found := false
	for _, n := range all {
		if n.ID == id {
			target, found = n, true
			break
		}
	}
	if !found {
		cliErrf(env.Stderr, "no notification with id %q\n", id)
		return 3
	}
	caller := notifyOrigin(env)
	if !notify.MayDismiss(target, caller) {
		cliErrf(env.Stderr, "notification %s was posted by another caller; a caller may only dismiss its own\n", id)
		return 4
	}

	delivered, _ := notifyDeliver(env, notifyDismissRequest(caller, id))

	if !delivered {
		store, err := notify.Open(env.StateDir)
		if err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		defer func() { _ = store.Close() }()
		if err := store.Dismiss(id); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
	}

	if jsonOutput {
		return writeNotifyJSON(env, notifyResult{Action: "dismiss", ID: id, Delivered: delivered})
	}
	_, _ = fmt.Fprintf(env.Stdout, "Dismissed %s.\n", id)
	return 0
}

func runNotifyList(env Env, args []string) int {
	help := RenderHelp(RootCommand().FindSubcommand("notify").FindSubcommand("list"))

	jsonOutput := false
	includeDismissed := false
	unreadOnly := false
	for _, arg := range args {
		switch {
		case isHelp(arg):
			_, _ = fmt.Fprint(env.Stdout, help)
			return 0
		case arg == "--json":
			jsonOutput = true
		case arg == "--all":
			includeDismissed = true
		case arg == "--unread":
			unreadOnly = true
		default:
			cliErrf(env.Stderr, "unknown option %q\n\n%s", arg, help)
			return 2
		}
	}

	// Read the log directly rather than asking an instance: listing must work
	// with no TUI running, and reading never needs one.
	all, err := notify.ReadAll(notify.Path(env.StateDir))
	if err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}
	items := notify.Active(all)
	switch {
	case unreadOnly:
		items = notify.Unread(all)
	case includeDismissed:
		items = all
	}

	if jsonOutput {
		out := struct {
			Unread int                   `json:"unread"`
			Items  []notify.Notification `json:"items"`
		}{Unread: notify.UnreadCount(all), Items: items}
		if out.Items == nil {
			out.Items = []notify.Notification{}
		}
		if err := json.NewEncoder(env.Stdout).Encode(out); err != nil {
			cliErrln(env.Stderr, err)
			return 1
		}
		return 0
	}

	if len(items) == 0 {
		_, _ = fmt.Fprintln(env.Stdout, "No notifications.")
		return 0
	}
	for _, n := range items {
		mark := "●"
		if n.Read() {
			mark = " "
		}
		if n.Dismissed() {
			mark = "×"
		}
		line := fmt.Sprintf("%s %s  %-8s %s", mark, n.ID, n.Source, n.Title)
		if n.Body != "" {
			line += "  — " + strings.ReplaceAll(n.Body, "\n", " ")
		}
		_, _ = fmt.Fprintln(env.Stdout, line)
	}
	return 0
}

// notifyDismissRequest builds the dismissal the app answers. The request
// carries the *caller's* origin, never the target's: sending the target's made
// the host compare the record against itself, so MayDismiss passed
// unconditionally and anything able to write a request file could dismiss
// anyone's notification. The id travels in Target.Value, which is what the host
// resolves the record from.
func notifyDismissRequest(caller notify.Origin, id string) uirequest.Request {
	return uirequest.Request{
		Origin: originForRequest(caller),
		Action: uirequest.ActionNotify,
		Target: uirequest.Target{Kind: uirequest.TargetKindNotification, Value: id},
	}
}

// notifyResult is the --json shape for post and dismiss.
type notifyResult struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	// Delivered reports whether a running instance took it. False means the
	// log was written directly and it appears at the next start.
	Delivered    bool                 `json:"delivered"`
	Notification *notify.Notification `json:"notification,omitempty"`
}

func writeNotifyJSON(env Env, res notifyResult) int {
	if err := json.NewEncoder(env.Stdout).Encode(res); err != nil {
		cliErrln(env.Stderr, err)
		return 1
	}
	return 0
}

// deliveryOutcome is why a request was not taken. The CLI reported every
// failure as "no running Sidecar instance", which was a lie in the two cases
// that actually happen — an instance running on another project, and an
// instance that never answered — and sent the user looking for a process that
// was in front of them.
type deliveryOutcome int

const (
	deliveryNoInstance deliveryOutcome = iota
	deliveryDeclined
	deliveryTimedOut
	deliveryWriteFailed
	deliveryTaken
)

func (o deliveryOutcome) explain() string {
	switch o {
	case deliveryDeclined:
		return "no running Sidecar instance is showing this project, so it appears there within a second if one opens it, or at next start."
	case deliveryTimedOut:
		return "a running Sidecar instance did not answer in time, so it appears there within a second, or at next start."
	case deliveryWriteFailed:
		return "the request could not be handed to a running Sidecar instance, so it appears at next start."
	default:
		return "no running Sidecar instance, so it appears at next start."
	}
}

// notifyDeliver writes the request and reports whether a live instance took
// it, and why not when it did not. No announced instance means no request is
// written at all: there is nobody to answer, and the caller writes the log
// itself.
func notifyDeliver(env Env, req uirequest.Request) (bool, deliveryOutcome) {
	instances, err := uirequest.ListInstances(env.StateDir)
	if err != nil || len(instances) == 0 {
		return false, deliveryNoInstance
	}
	req.ID = uirequest.NewRequestID()
	req.Version = 1
	req.CreatedAt = time.Now().UTC()
	req.TTLMs = int(uirequest.DefaultTTL / time.Millisecond)
	if _, err := uirequest.WriteRequest(env.StateDir, req); err != nil {
		return false, deliveryWriteFailed
	}
	defer func() { _ = uirequest.Cleanup(env.StateDir, req.ID, req.Action) }()

	deadline := time.Now().Add(notifyWait)
	for time.Now().Before(deadline) {
		acks, err := uirequest.ReadAcks(env.StateDir, req.ID, req.Action)
		if err == nil {
			for _, ack := range acks {
				if ack.Status == uirequest.StatusOpened {
					return true, deliveryTaken
				}
			}
			if len(acks) >= len(instances) {
				// Every instance answered and none took it.
				return false, deliveryDeclined
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	return false, deliveryTimedOut
}

// notifyOrigin identifies the caller. Inside a Sidecar shell that is the
// shell's registered origin; anywhere else it is the working directory, which
// is enough to let a caller dismiss what it posted from the same place.
func notifyOrigin(env Env) notify.Origin {
	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if dest, err := resolveOpenDestination(ctx, env.StateDir, "", ""); err == nil {
		return notify.Origin{
			TmuxSession: dest.Origin.TmuxSession,
			ProjectKey:  dest.Origin.ProjectKey,
			WorkDir:     dest.Origin.WorkDir,
			PID:         os.Getpid(),
		}
	}
	wd, _ := os.Getwd()
	return notify.Origin{WorkDir: wd, PID: os.Getpid()}
}

func originForRequest(o notify.Origin) uirequest.Origin {
	return uirequest.Origin{
		TmuxSession: o.TmuxSession,
		ProjectKey:  o.ProjectKey,
		WorkDir:     o.WorkDir,
		PID:         os.Getpid(),
	}
}
