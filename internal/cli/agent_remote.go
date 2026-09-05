package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/marcus/sidecar/internal/agentcontrol"
	"github.com/marcus/sidecar/internal/agentremote"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/hosts"
)

// newRemoteRunner is the transport seam. Production dials the host over the
// ssh ControlMaster; tests replace it and assert on the argv.
var newRemoteRunner = productionRemoteRunner

// productionRemoteRunner resolves a registered host and runs one verb on it.
//
// It builds a one-shot client rather than a Registry because a CLI process has
// no stream to be healthy: see hosts.OneShotClient. Reading the config here,
// once, is also what makes "no such host" a refusal with the registered names
// in it instead of a connection attempt to a name nobody registered.
func productionRemoteRunner(env Env, hostID string) (agentremote.Runner, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if !remoteHostsEnabled(env, cfg) {
		return nil, &agentcontrol.Error{Code: agentcontrol.ErrFeatureDisabled,
			Message: fmt.Sprintf("remote hosts are disabled; enable the %s feature", features.SidecarRemoteHosts.Name)}
	}
	registered := hostsFromConfigForCLI(cfg)
	for _, host := range registered {
		if host.ID != hostID {
			continue
		}
		client := hosts.OneShotClient(host, hosts.ClientOptions{})
		return func(ctx context.Context, _ string, args []string, out any) error {
			return client.RunSidecar(ctx, args, out)
		}, nil
	}
	return nil, &agentcontrol.Error{Code: agentcontrol.ErrHostUnavailable, Message: unknownHostMessage(hostID, registered)}
}

// remoteHostsEnabled answers whether this run may reach a registered host.
//
// It resolves the same way agentControlEnabled does, and for the same reason:
// a CLI invocation's -enable-feature lands in Env.FeatureOverrides, while
// features.IsEnabled reads the global manager that a CLI process never sets up
// from config. Asking the global manager alone made `-enable-feature
// sidecar_remote_hosts` a flag that parsed, was accepted, and did nothing —
// every --host verb refused as if the feature were off. Found by running the
// command rather than by a test, which is why there is now a test.
func remoteHostsEnabled(env Env, cfg *config.Config) bool {
	if enabled, ok := env.FeatureOverrides[features.SidecarRemoteHosts.Name]; ok {
		return enabled
	}
	if cfg == nil {
		return false
	}
	return cfg.Features.Flags[features.SidecarRemoteHosts.Name]
}

// hostsFromConfigForCLI lists registered hosts without consulting the feature
// flag a second time.
//
// hosts.FromConfig returns nothing when the flag is off, reading the global
// manager — which is the rollback guarantee for the TUI and exactly wrong here,
// because the CLI has already decided the question against Env.FeatureOverrides.
// Going through it would make an -enable-feature run see zero hosts and report
// "no host is registered as X" for a host that is registered.
func hostsFromConfigForCLI(cfg *config.Config) []hosts.Host {
	if cfg == nil {
		return nil
	}
	registered := make([]hosts.Host, 0, len(cfg.Hosts.List))
	seen := make(map[string]bool, len(cfg.Hosts.List))
	for _, entry := range cfg.Hosts.List {
		target := strings.TrimSpace(entry.Target)
		if target == "" || entry.Disabled {
			continue
		}
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			id = target
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		registered = append(registered, hosts.Host{
			ID:           id,
			Target:       target,
			RemoteBinary: strings.TrimSpace(entry.Binary),
			RemoteConfig: strings.TrimSpace(entry.Config),
			Env:          append([]string(nil), entry.Env...),
		})
	}
	return registered
}

func unknownHostMessage(hostID string, registered []hosts.Host) string {
	if len(registered) == 0 {
		return fmt.Sprintf("no host is registered as %q; register one with `sidecar host add`", hostID)
	}
	names := make([]string, 0, len(registered))
	for _, host := range registered {
		names = append(names, host.ID)
	}
	return fmt.Sprintf("no host is registered as %q; registered hosts are %s", hostID, strings.Join(names, ", "))
}

// remoteClient builds the addressed host's client without writing output, so
// prompt can attach a proved not-submitted receipt to failures that happen
// before any transport is opened.
func remoteClient(env Env, f agentFlags) (agentremote.Client, error) {
	runner, err := newRemoteRunner(env, f.host)
	if err != nil {
		return agentremote.Client{}, err
	}
	return agentremote.Client{HostID: f.host, Run: runner, Project: f.project}, nil
}

// remoteTarget applies the one target rule that differs from the local path: a
// remote verb needs an explicit target.
//
// The omitted-target rule resolves SIDECAR_SHELL, which names a shell on THIS
// machine. Sending it to another host would either miss entirely or — worse,
// and the reason this is a refusal rather than a best effort — hit a shell
// that happens to carry the same generated name on the other machine. Two
// Sidecars started in the same-named project produce the same session names by
// construction, so this is a realistic collision rather than a theoretical one.
func remoteTarget(f agentFlags, target string, explicit bool) (string, error) {
	if !explicit || strings.TrimSpace(target) == "" {
		return "", &agentcontrol.Error{
			Code:    agentcontrol.ErrNotFound,
			Message: "--host requires an explicit target: the current shell is on this machine, not on " + f.host,
		}
	}
	return target, nil
}

// remoteContext returns the caller's context.
func remoteContext(env Env) context.Context {
	if env.Ctx != nil {
		return env.Ctx
	}
	return context.Background()
}

// ---------------------------------------------------------------------------
// Verb dispatch. Each of these is entered only when --host was given, and each
// returns a process exit code.
// ---------------------------------------------------------------------------

func runRemoteAgentList(env Env, f agentFlags) int {
	client, err := remoteClient(env, f)
	if err != nil {
		return emitAgentError(env, true, err)
	}
	agents, err := client.List(remoteContext(env))
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	return emitAgentList(env, f.json, agents)
}

func runRemoteAgentGet(env Env, f agentFlags, target string, explicit bool) int {
	client, err := remoteClient(env, f)
	if err != nil {
		return emitAgentError(env, true, err)
	}
	session, err := remoteTarget(f, target, explicit)
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	agent, err := client.Get(remoteContext(env), session, f.includeSession)
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	return emitAgent(env, f.json, agent)
}

func runRemoteAgentStart(env Env, f agentFlags, target, kind string, explicit bool, providerArgs []string) int {
	client, err := remoteClient(env, f)
	if err != nil {
		return emitAgentError(env, true, err)
	}
	session, err := remoteTarget(f, target, explicit)
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	agent, err := client.Start(remoteContext(env), session, kind, f.timeout, providerArgs)
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	return emitAgent(env, f.json, agent)
}

func runRemoteAgentPrompt(env Env, f agentFlags, target, text string, explicit bool) int {
	requested := agentcontrol.Target{Host: f.host, Project: f.project}
	if explicit {
		requested.Session = target
	}
	client, err := remoteClient(env, f)
	if err != nil {
		return emitPromptError(env, f.json, agentcontrol.WithPromptReceipt(err, requested, agentcontrol.SubmissionNotSubmitted, agentcontrol.PromptWaitNotStarted))
	}
	session, err := remoteTarget(f, target, explicit)
	if err != nil {
		return emitPromptError(env, f.json, agentcontrol.WithPromptReceipt(err, requested, agentcontrol.SubmissionNotSubmitted, agentcontrol.PromptWaitNotStarted))
	}
	result, err := client.Prompt(remoteContext(env), session, text, f.wait, f.until, f.timeout)
	if err != nil {
		return emitPromptError(env, f.json, err)
	}
	return emitPromptResult(env, f.json, result)
}

func runRemoteAgentWait(env Env, f agentFlags, target string, explicit bool) int {
	client, err := remoteClient(env, f)
	if err != nil {
		return emitAgentError(env, true, err)
	}
	session, err := remoteTarget(f, target, explicit)
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	agent, err := client.Wait(remoteContext(env), session, f.until, f.timeout)
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	return emitAgent(env, f.json, agent)
}

func runRemoteAgentRead(env Env, f agentFlags, target string, explicit bool) int {
	client, err := remoteClient(env, f)
	if err != nil {
		return emitAgentError(env, true, err)
	}
	session, err := remoteTarget(f, target, explicit)
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	result, err := client.Read(remoteContext(env), session, f.source, f.lines, f.ansi)
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	return emitReadResult(env, f.json, result)
}

func runRemoteAgentSendKeys(env Env, f agentFlags, target string, explicit bool, keys []string) int {
	client, err := remoteClient(env, f)
	if err != nil {
		return emitAgentError(env, true, err)
	}
	session, err := remoteTarget(f, target, explicit)
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	agent, err := client.SendKeys(remoteContext(env), session, keys)
	if err != nil {
		return emitAgentError(env, f.json, err)
	}
	return emitAgent(env, f.json, agent)
}

// ---------------------------------------------------------------------------
// session status / restore on a host.
// ---------------------------------------------------------------------------

// runRemoteSessionDocument relays the host's own plan or result document.
//
// Cold restore executes on the host. The viewer requests it and observes the
// answer; it never rebuilds another machine's plan locally, because the inputs
// a plan is built from — live tmux sessions, the server id, whether a working
// directory still exists, whether a provider binary is installed — are all
// facts about the host and none of them is knowable from here.
func runRemoteSessionDocument(env Env, hostID string, jsonOutput bool, build func(agentremote.Client) ([]string, error)) int {
	runner, err := newRemoteRunner(env, hostID)
	if err != nil {
		return emitAgentError(env, true, err)
	}
	client := agentremote.Client{HostID: hostID, Run: runner}
	args, err := build(client)
	if err != nil {
		return emitAgentError(env, jsonOutput, err)
	}
	var doc agentremote.SessionDocument
	if err := client.Run(remoteContext(env), hostID, args, &doc); err != nil {
		return emitAgentError(env, jsonOutput, agentremote.TranslateError(hostID, err))
	}
	return emitRemoteSessionDocument(env, hostID, jsonOutput, doc)
}

// emitRemoteSessionDocument prints the host's document, annotated with which
// host it describes.
//
// The annotation is not decoration. A restore plan names tmux sessions and
// working directories, and those read exactly like local ones; a plan pasted
// into an issue without its host is a plan nobody can act on.
func emitRemoteSessionDocument(env Env, hostID string, jsonOutput bool, doc agentremote.SessionDocument) int {
	if doc == nil {
		doc = agentremote.SessionDocument{}
	}
	doc["host"] = hostID
	if jsonOutput {
		return writeAgentJSON(env, doc)
	}
	// The human form is the host's own document rendered as indented JSON.
	// Re-implementing writeSessionPlanHuman against a map would be a second
	// renderer of the host's schema, which is the thing this relay exists not
	// to have; --json is the shape a caller should be using here anyway.
	_, _ = fmt.Fprintf(env.Stdout, "host: %s\n", hostID)
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return emitAgentError(env, jsonOutput, err)
	}
	_, _ = fmt.Fprintln(env.Stdout, string(encoded))
	return 0
}
