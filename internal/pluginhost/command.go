package pluginhost

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/marcus/sidecar/internal/resource"
)

// CommandProvider is the default Provider: a short-lived child process per
// request, one JSON object in, one JSON object out. It also carries the
// instance configuration's claimed hosts, which are host-side data a provider
// process never sees.
type CommandProvider struct {
	instance   string
	argv       []string
	dir        string
	env        []string
	claimHosts []string
	// protocol is the identifier this instance is dispatched with. It is
	// decided once, from the config section the entry was read from, and never
	// negotiated: a plugin does not get to upgrade itself by answering with a
	// different string.
	protocol string
	// home bounds declared watch paths. Resolved at construction so no
	// validation pass reads the environment.
	home string

	describeTimeout time.Duration
	resolveTimeout  time.Duration

	runner Runner
	host   HostInfo
	log    *slog.Logger

	// declaredContext is the context kinds the most recent successful describe
	// declared. Filtering against it here, at the process boundary, is what
	// makes "an undeclared kind is never sent" a property of the host rather
	// than a promise each caller has to keep.
	mu              sync.RWMutex
	declaredContext []ContextKind
}

var _ Provider = (*CommandProvider)(nil)
var _ PluginProvider = (*CommandProvider)(nil)
var _ claimHostsProvider = (*CommandProvider)(nil)

// CommandConfig is everything a CommandProvider needs. It is resolved once, at
// construction, so no invocation reads configuration or the environment.
type CommandConfig struct {
	Instance string
	Argv     []string
	// Dir is the neutral working directory — a Sidecar config directory, never
	// a selected repository.
	Dir string
	// PassEnv names variables whose current values are inherited on top of the
	// documented base environment.
	PassEnv []string
	// ClaimHosts is the instance configuration's claimed hostnames, already
	// validated and lowercased by internal/config. It is host-side data: it
	// never reaches the child process or the protocol.
	ClaimHosts []string
	// HostEnv is the os.Environ()-shaped environment to draw from.
	HostEnv []string
	// ResolveTimeout is clamped; zero takes the default.
	ResolveTimeout time.Duration
	// Runner defaults to ExecRunner.
	Runner Runner
	// Host identifies Sidecar to the provider.
	Host HostInfo
	// Log receives metadata-only invocation records. Nil discards them.
	Log *slog.Logger
	// Protocol is the wire identifier to dispatch with. Empty means the frozen
	// resource protocol, so an existing caller keeps the behaviour it had.
	Protocol string
	// Home bounds declared watch paths. Empty resolves from HostEnv's HOME and
	// then from the OS, once, here — never on an invocation path.
	Home string
}

// NewCommandProvider builds a provider over a configured argv.
func NewCommandProvider(cfg CommandConfig) (*CommandProvider, error) {
	if cfg.Instance == "" {
		return nil, errors.New("pluginhost: instance id is required")
	}
	if len(cfg.Argv) == 0 || cfg.Argv[0] == "" {
		return nil, errors.New("pluginhost: command argv is required")
	}
	runner := cfg.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	protocol := cfg.Protocol
	if protocol == "" {
		protocol = resource.Protocol
	}
	if protocol != resource.Protocol && protocol != Protocol {
		return nil, errors.New("pluginhost: unsupported protocol " + protocol)
	}
	env := BuildEnv(cfg.PassEnv, cfg.HostEnv)
	if protocol == Protocol {
		env = withPluginMarker(env)
	}
	return &CommandProvider{
		instance:        cfg.Instance,
		argv:            append([]string(nil), cfg.Argv...),
		dir:             cfg.Dir,
		env:             env,
		claimHosts:      normalizeClaimHosts(cfg.ClaimHosts),
		protocol:        protocol,
		home:            resolveHome(cfg.Home, cfg.HostEnv),
		describeTimeout: resource.DescribeTimeout,
		resolveTimeout:  resource.ClampResolveTimeout(cfg.ResolveTimeout),
		runner:          runner,
		host:            cfg.Host,
		log:             cfg.Log,
	}, nil
}

// resolveHome picks the home directory watch paths are bounded to. It reads the
// environment the child will see first, so a test that hands the provider a
// synthetic environment gets the same answer the child would.
func resolveHome(configured string, hostEnv []string) string {
	if configured != "" {
		return configured
	}
	for _, kv := range hostEnv {
		if name, value, ok := strings.Cut(kv, "="); ok && name == "HOME" {
			return value
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// Protocol reports the wire identifier this instance is dispatched with.
func (p *CommandProvider) Protocol() string { return p.protocol }

// SpeaksPluginProtocol reports whether list, get, and act are available on this
// instance.
func (p *CommandProvider) SpeaksPluginProtocol() bool { return p.protocol == Protocol }

// Instance reports the configured instance ID.
func (p *CommandProvider) Instance() string { return p.instance }

// ClaimHosts reports the instance's claimed hostnames. It is a copy.
func (p *CommandProvider) ClaimHosts() []string { return append([]string(nil), p.claimHosts...) }

// Argv exposes the resolved command for diagnostics. It is a copy.
func (p *CommandProvider) Argv() []string { return append([]string(nil), p.argv...) }

// Env exposes the computed child environment for diagnostics. Values are real,
// so a caller that prints this must redact — `terminal-links check` reports
// names only.
func (p *CommandProvider) Env() []string { return append([]string(nil), p.env...) }

// ResolveTimeout reports the clamped per-invocation timeout.
func (p *CommandProvider) ResolveTimeout() time.Duration { return p.resolveTimeout }

// Describe runs the describe method and validates the result.
func (p *CommandProvider) Describe(ctx context.Context) (Description, error) {
	req := p.newRequest(MethodDescribe, p.describeTimeout, nil)
	if p.host.Name != "" || p.host.Version != "" {
		host := p.host
		req.Host = &host
	}
	resp, err := p.invoke(ctx, MethodDescribe, req, p.describeTimeout)
	if err != nil {
		return Description{}, err
	}
	if resp.Error != nil {
		// A typed error is authoritative: the plugin is telling the host it
		// has nothing to declare right now.
		p.setDeclaredContext(nil)
		return Description{}, resource.SanitizeError(resp.Error)
	}
	if resp.Resource != nil || resp.Page != nil || resp.Outcome != nil {
		return Description{}, &TransportError{
			Instance: p.instance,
			Method:   MethodDescribe,
			Reason:   ReasonShape,
			Detail:   "describe returned a result of another method",
		}
	}
	if !p.SpeaksPluginProtocol() {
		// A resource provider's describe is validated by the frozen rules and
		// nothing else. Collections and actions it happened to send are not
		// read, because the identifier it was asked on does not have them.
		return ValidateDescription(p.instance, resp.identity(), resp.Matchers)
	}
	desc, err := ValidateDescribe(p.instance, resp, p.home)
	if err != nil {
		return Description{}, err
	}
	p.setDeclaredContext(desc.Context)
	return desc, nil
}

// Resolve runs the resolve method and sanitizes the document.
func (p *CommandProvider) Resolve(ctx context.Context, ref resource.Reference) (resource.Document, error) {
	if !ref.Valid() || ref.Instance != p.instance {
		return resource.Document{}, &TransportError{
			Instance: p.instance,
			Method:   MethodResolve,
			Reason:   ReasonInvalidRequest,
			Detail:   "reference is not addressed to this instance or exceeds its bounds",
		}
	}
	req := p.newRequest(MethodResolve, p.resolveTimeout, &ResolveParams{Matcher: ref.Matcher, Locator: ref.Locator})
	return p.document(ctx, MethodResolve, req)
}

// List runs the list method and sanitizes the page.
//
// A `search: required` collection with an empty query is answered here, without
// starting a process: the plugin has nothing to say until there is a query, and
// spawning a child to be told so once per keystroke is exactly the cost this
// protocol is trying not to pay.
func (p *CommandProvider) List(ctx context.Context, params ListParams, callCtx *Context, collection Collection) (Page, error) {
	if err := p.requirePluginMethod(MethodList); err != nil {
		return Page{}, err
	}
	if params.Collection == "" {
		return Page{}, p.invalidRequest(MethodList, "list names no collection")
	}
	if collection.Search == SearchRequired && strings.TrimSpace(params.Query) == "" {
		return Page{Outcome: OutcomeAbstained}, nil
	}
	params.Limit = ClampListLimit(params.Limit)
	// Filters are narrowed here, at the process boundary, for the same reason
	// context is: "a key the collection did not declare never reaches the
	// plugin" has to be a property of the host, not a promise each caller keeps.
	params.Filters = NormalizeFilters(collection, params.Filters)
	req := p.newRequest(MethodList, p.resolveTimeout, &params)
	req.Context = p.allowedContext(callCtx)

	resp, err := p.invoke(ctx, MethodList, req, p.resolveTimeout)
	if err != nil {
		return Page{}, err
	}
	if resp.Error != nil {
		return Page{}, resource.SanitizeError(resp.Error)
	}
	if resp.Page == nil {
		return Page{}, &TransportError{
			Instance: p.instance,
			Method:   MethodList,
			Reason:   ReasonShape,
			Detail:   "list did not return a page",
		}
	}
	return SanitizePage(resp.Page, collection), nil
}

// Get runs the get method and sanitizes the document, sections included.
//
// collection is the validated declaration, for the same reason List takes it:
// the applied filters are narrowed against it here, at the process boundary, so
// "a key the collection did not declare never reaches the plugin" is a property
// of the host rather than a promise every caller has to keep.
func (p *CommandProvider) Get(ctx context.Context, params GetParams, callCtx *Context, collection Collection) (resource.Document, error) {
	if err := p.requirePluginMethod(MethodGet); err != nil {
		return resource.Document{}, err
	}
	if params.Collection == "" || params.ID == "" {
		return resource.Document{}, p.invalidRequest(MethodGet, "get needs both a collection and an id")
	}
	params.Filters = NormalizeFilters(collection, params.Filters)
	req := p.newRequest(MethodGet, p.resolveTimeout, &params)
	req.Context = p.allowedContext(callCtx)
	return p.document(ctx, MethodGet, req)
}

// Act runs the act method and sanitizes the outcome. It is the only method that
// mutates, and the host never calls it without the user having confirmed.
func (p *CommandProvider) Act(ctx context.Context, params ActParams, callCtx *Context) (Outcome, error) {
	if err := p.requirePluginMethod(MethodAct); err != nil {
		return Outcome{}, err
	}
	if params.Action == "" {
		return Outcome{}, p.invalidRequest(MethodAct, "act names no action")
	}
	req := p.newRequest(MethodAct, p.resolveTimeout, &params)
	req.Context = p.allowedContext(callCtx)

	resp, err := p.invoke(ctx, MethodAct, req, p.resolveTimeout)
	if err != nil {
		return Outcome{}, err
	}
	if resp.Error != nil {
		return Outcome{}, resource.SanitizeError(resp.Error)
	}
	if resp.Outcome == nil {
		return Outcome{}, &TransportError{
			Instance: p.instance,
			Method:   MethodAct,
			Reason:   ReasonShape,
			Detail:   "act did not return an outcome",
		}
	}
	return SanitizeOutcome(resp.Outcome), nil
}

// document runs a method that returns a resource and sanitizes it. resolve and
// get differ only in what they are addressed by, so they share this.
func (p *CommandProvider) document(ctx context.Context, method string, req Request) (resource.Document, error) {
	resp, err := p.invoke(ctx, method, req, p.resolveTimeout)
	if err != nil {
		return resource.Document{}, err
	}
	if resp.Error != nil {
		return resource.Document{}, resource.SanitizeError(resp.Error)
	}
	if resp.hasDescribeShape() {
		return resource.Document{}, &TransportError{
			Instance: p.instance,
			Method:   method,
			Reason:   ReasonShape,
			Detail:   method + " returned a describe result",
		}
	}
	// A resource the host cannot key or label is a protocol violation, not a
	// blank card. Everything else about a document is truncated, never refused.
	doc, structural := resource.SanitizeDocument(resp.Resource)
	if structural != nil {
		return resource.Document{}, &TransportError{
			Instance: p.instance,
			Method:   method,
			Reason:   ReasonInvalidResource,
			Detail:   structural.Detail,
			Err:      structural,
		}
	}
	return doc, nil
}

func (p *CommandProvider) newRequest(method string, timeout time.Duration, params any) Request {
	return Request{
		Protocol:   p.protocol,
		Method:     method,
		Instance:   p.instance,
		DeadlineMs: timeout.Milliseconds(),
		Params:     params,
	}
}

// requirePluginMethod refuses list, get, and act on an instance dispatched with
// the frozen resource identifier — before a process is started, because a
// resource provider asked for a method its protocol does not have would either
// crash or answer something the host cannot read.
func (p *CommandProvider) requirePluginMethod(method string) error {
	if p.SpeaksPluginProtocol() {
		return nil
	}
	return &TransportError{
		Instance: p.instance,
		Method:   method,
		Reason:   ReasonInvalidRequest,
		Detail:   "this instance speaks " + resource.Protocol + ", which has no " + method + " method",
	}
}

func (p *CommandProvider) invalidRequest(method, detail string) error {
	return &TransportError{Instance: p.instance, Method: method, Reason: ReasonInvalidRequest, Detail: detail}
}

// allowedContext narrows what the caller offered to what the plugin declared it
// reads. A plugin that has never described successfully has declared nothing,
// so it receives nothing.
func (p *CommandProvider) allowedContext(callCtx *Context) *Context {
	p.mu.RLock()
	declared := p.declaredContext
	p.mu.RUnlock()
	return callCtx.filter(declared)
}

func (p *CommandProvider) setDeclaredContext(kinds []ContextKind) {
	p.mu.Lock()
	p.declaredContext = append([]ContextKind(nil), kinds...)
	p.mu.Unlock()
}

// invoke is the whole process boundary: encode, run, decode, log. Everything it
// records is metadata — instance, method, duration, outcome, byte counts — and
// nothing else ever reaches a log line.
func (p *CommandProvider) invoke(ctx context.Context, method string, req Request, timeout time.Duration) (_ *Response, err error) {
	payload, marshalErr := json.Marshal(req)
	if marshalErr != nil {
		return nil, &TransportError{Instance: p.instance, Method: method, Reason: ReasonInvalidRequest, Detail: "request could not be encoded", Err: marshalErr}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, runErr := p.runner.Run(runCtx, RunSpec{
		Argv:      p.argv,
		Dir:       p.dir,
		Env:       p.env,
		Stdin:     payload,
		MaxStdout: resource.MaxResponseBytes,
	})

	defer func() { p.record(method, result, err) }()

	if runErr != nil {
		return nil, &TransportError{Instance: p.instance, Method: method, Reason: ReasonSpawn, Detail: "the provider command could not be run", Err: runErr}
	}
	if result.TimedOut {
		reason := ReasonTimeout
		if ctx.Err() != nil {
			reason = ReasonCanceled
		}
		return nil, &TransportError{Instance: p.instance, Method: method, Reason: reason, Detail: "the process group was killed"}
	}
	if result.StdoutTruncated {
		return nil, &TransportError{Instance: p.instance, Method: method, Reason: ReasonOversize, Detail: "stdout exceeded the response byte limit"}
	}
	if result.ExitCode != 0 {
		return nil, &TransportError{Instance: p.instance, Method: method, Reason: ReasonExit, Detail: "the provider exited non-zero"}
	}
	resp, reason, detail := decodeResponse(result.Stdout, p.protocol)
	if reason != "" {
		return nil, &TransportError{Instance: p.instance, Method: method, Reason: reason, Detail: detail}
	}
	return resp, nil
}

func (p *CommandProvider) record(method string, result RunResult, err error) {
	if p.log == nil {
		return
	}
	// Locator, title, body, URL, credentials, stdout and stderr are absent by
	// construction: none of them is in scope here.
	p.log.Debug("plugin invocation",
		"instance", p.instance,
		"method", method,
		"duration_ms", result.Duration.Milliseconds(),
		"outcome", OutcomeCode(err),
		"stdout_bytes", result.StdoutBytes,
		"stderr_bytes", result.StderrBytes,
		"exit_code", result.ExitCode,
	)
}
