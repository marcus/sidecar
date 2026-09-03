// Command fixtureprovider is the reference Sidecar plugin used by Sidecar's own
// tests. It is a real executable, built by the test binary, so the host's argv,
// JSON, environment, timeout, and process-lifecycle handling are exercised
// against a child process rather than an in-memory fake.
//
// It speaks both protocol identifiers from one binary, because that is the
// property the host has to hold: the same executable answers
// sidecar.terminal-resource/v1 with matchers and resolve, and
// sidecar.plugin/v1-draft with collections, actions, list, get, and act. It
// answers on whichever identifier the request carried, which is also how the
// "wrong dialect" cases are built.
//
// It performs no network access, reads no credentials, and needs none.
//
// It simulates the hostile cases on demand, selected either by the -mode flag
// (which is what an argv-level test uses) or by a `mode:<name>:` prefix on the
// request's subject — the locator on resolve, the query on list, the id on get,
// the action on act — which is what a test driving one configured instance uses.
//
// It lives under testdata/ so `go build ./...` and `go vet ./...` ignore it;
// the test binary builds it explicitly.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	resourceProtocol = "sidecar.terminal-resource/v1"
	pluginProtocol   = "sidecar.plugin/v1-draft"
)

type request struct {
	Protocol   string `json:"protocol"`
	Method     string `json:"method"`
	Instance   string `json:"instance"`
	DeadlineMs int64  `json:"deadlineMs"`
	Host       *struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"host,omitempty"`
	Context *struct {
		Project *struct {
			Root    string `json:"root"`
			WorkDir string `json:"workDir"`
			Name    string `json:"name"`
			Branch  string `json:"branch"`
			HostID  string `json:"hostId"`
		} `json:"project,omitempty"`
		Selection *struct {
			Text string `json:"text"`
		} `json:"selection,omitempty"`
	} `json:"context,omitempty"`
	Params *struct {
		// resolve
		Matcher string `json:"matcher"`
		Locator string `json:"locator"`
		// list and get
		Collection string `json:"collection"`
		Query      string `json:"query"`
		View       string `json:"view"`
		Sort       struct {
			Key string `json:"key"`
			Dir string `json:"dir"`
		} `json:"sort"`
		Filters map[string]string `json:"filters"`
		Cursor  string            `json:"cursor"`
		Limit   int               `json:"limit"`
		ID      string            `json:"id"`
		// act
		Action string            `json:"action"`
		Inputs map[string]string `json:"inputs"`
	} `json:"params,omitempty"`
}

type matcher struct {
	ID       string `json:"id"`
	Pattern  string `json:"pattern"`
	Priority int    `json:"priority,omitempty"`
}

type info struct {
	Kind    string `json:"kind,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	DocsURL string `json:"docsUrl,omitempty"`
}

type status struct {
	Label string `json:"label"`
	Tone  string `json:"tone,omitempty"`
}

type field struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Kind  string `json:"kind,omitempty"`
}

type body struct {
	Format string `json:"format,omitempty"`
	Text   string `json:"text"`
}

type timelineItem struct {
	When  string `json:"when,omitempty"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text,omitempty"`
}

type section struct {
	Title  string         `json:"title,omitempty"`
	Body   *body          `json:"body,omitempty"`
	Fields []field        `json:"fields,omitempty"`
	Items  []timelineItem `json:"items,omitempty"`
}

type document struct {
	Identity        string         `json:"identity"`
	Title           string         `json:"title"`
	Subtitle        string         `json:"subtitle,omitempty"`
	Status          *status        `json:"status,omitempty"`
	Fields          []field        `json:"fields,omitempty"`
	Body            *body          `json:"body,omitempty"`
	Sections        []section      `json:"sections,omitempty"`
	SourceURL       string         `json:"sourceUrl,omitempty"`
	UpdatedAt       string         `json:"updatedAt,omitempty"`
	FreshForSeconds float64        `json:"freshForSeconds,omitempty"`
	Extra           map[string]any `json:"aFieldTheHostHasNeverHeardOf,omitempty"`
}

type column struct {
	ID        string `json:"id"`
	Label     string `json:"label,omitempty"`
	Width     int    `json:"width,omitempty"`
	Align     string `json:"align,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Primary   bool   `json:"primary,omitempty"`
	Secondary bool   `json:"secondary,omitempty"`
}

type view struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

type sortKey struct {
	ID      string `json:"id"`
	Label   string `json:"label,omitempty"`
	Default string `json:"default,omitempty"`
}

type refresh struct {
	EverySeconds int      `json:"everySeconds,omitempty"`
	Watch        []string `json:"watch,omitempty"`
}

type filterChoice struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

type filter struct {
	ID      string         `json:"id"`
	Label   string         `json:"label,omitempty"`
	Kind    string         `json:"kind,omitempty"`
	Choices []filterChoice `json:"choices,omitempty"`
	Default string         `json:"default,omitempty"`
}

type collection struct {
	ID      string    `json:"id"`
	Title   string    `json:"title,omitempty"`
	Search  string    `json:"search,omitempty"`
	Columns []column  `json:"columns,omitempty"`
	Views   []view    `json:"views,omitempty"`
	Sort    []sortKey `json:"sort,omitempty"`
	Filters []filter  `json:"filters,omitempty"`
	Detail  *bool     `json:"detail,omitempty"`
	Refresh *refresh  `json:"refresh,omitempty"`
	Context []string  `json:"context,omitempty"`
}

type actionInput struct {
	ID       string   `json:"id"`
	Label    string   `json:"label,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Required bool     `json:"required,omitempty"`
	Choices  []string `json:"choices,omitempty"`
	Default  string   `json:"default,omitempty"`
}

type action struct {
	ID         string        `json:"id"`
	Title      string        `json:"title,omitempty"`
	On         string        `json:"on,omitempty"`
	Collection string        `json:"collection,omitempty"`
	Inputs     []actionInput `json:"inputs,omitempty"`
	Mutates    bool          `json:"mutates,omitempty"`
	Confirm    *bool         `json:"confirm,omitempty"`
	Key        string        `json:"key,omitempty"`
}

type item struct {
	ID        string            `json:"id"`
	Cells     map[string]string `json:"cells,omitempty"`
	Status    *status           `json:"status,omitempty"`
	SourceURL string            `json:"sourceUrl,omitempty"`
}

type notice struct {
	Tone string `json:"tone,omitempty"`
	Text string `json:"text"`
}

type omitted struct {
	Suppressed int `json:"suppressed,omitempty"`
	Dropped    int `json:"dropped,omitempty"`
}

type coverage struct {
	Source    string `json:"source"`
	State     string `json:"state,omitempty"`
	Reason    string `json:"reason,omitempty"`
	ElapsedMs int    `json:"elapsedMs,omitempty"`
}

type page struct {
	Outcome    string     `json:"outcome,omitempty"`
	Items      []item     `json:"items,omitempty"`
	NextCursor string     `json:"nextCursor,omitempty"`
	Total      int        `json:"total,omitempty"`
	Notices    []notice   `json:"notices,omitempty"`
	Omitted    *omitted   `json:"omitted,omitempty"`
	Coverage   []coverage `json:"coverage,omitempty"`
}

type openTarget struct {
	Collection string `json:"collection,omitempty"`
	ID         string `json:"id,omitempty"`
}

type outcome struct {
	Status  string      `json:"status,omitempty"`
	Message string      `json:"message,omitempty"`
	Refresh []string    `json:"refresh,omitempty"`
	Open    *openTarget `json:"open,omitempty"`
}

type protocolError struct {
	Code      string `json:"code"`
	Message   string `json:"message,omitempty"`
	Retryable *bool  `json:"retryable,omitempty"`
	SetupHint string `json:"setupHint,omitempty"`
}

type response struct {
	Protocol    string         `json:"protocol"`
	Plugin      *info          `json:"plugin,omitempty"`
	Provider    *info          `json:"provider,omitempty"`
	Context     []string       `json:"context,omitempty"`
	Matchers    []matcher      `json:"matchers,omitempty"`
	Collections []collection   `json:"collections,omitempty"`
	Actions     []action       `json:"actions,omitempty"`
	Resource    *document      `json:"resource,omitempty"`
	Page        *page          `json:"page,omitempty"`
	Outcome     *outcome       `json:"outcome,omitempty"`
	Error       *protocolError `json:"error,omitempty"`
}

// wire is the protocol identifier this invocation answers on: whatever the
// request carried, so the dialect is never guessed. It defaults to the frozen
// resource identifier for a request that carried nothing readable.
var wire = resourceProtocol

func main() {
	mode := flag.String("mode", "", "hostile behaviour to simulate")
	pidfile := flag.String("pidfile", "", "where a forked descendant records its pid")
	sleep := flag.Duration("sleep", 0, "how long the descendant mode sleeps")
	flag.Parse()

	if *mode == "descendant" {
		// A descendant that outlives its parent and keeps holding the inherited
		// stdout pipe. Only a process-group kill ends it.
		time.Sleep(*sleep)
		return
	}

	// Keep the raw request bytes as well as the decoded form: request-echo
	// hands them back so a host test can assert the envelope the host actually
	// wrote, rather than trusting the host's own encoder.
	rawRequest, _ := io.ReadAll(os.Stdin)
	var req request
	// A missing or unreadable request is not fatal: some modes are about what
	// the host does with the output regardless of the request.
	_ = json.Unmarshal(rawRequest, &req)
	if req.Protocol == pluginProtocol {
		wire = pluginProtocol
	}

	effective := *mode
	subject := requestSubject(req)
	if rest, ok := strings.CutPrefix(subject, "mode:"); ok {
		name, tail, _ := strings.Cut(rest, ":")
		effective = name
		subject = tail
	}

	run(effective, req, rawRequest, subject, *pidfile)
}

// requestSubject is the one string a `mode:` prefix can ride on, for whichever
// method is being called.
func requestSubject(req request) string {
	if req.Params == nil {
		return ""
	}
	switch req.Method {
	case "resolve":
		return req.Params.Locator
	case "list":
		return req.Params.Query
	case "get":
		return req.Params.ID
	case "act":
		return req.Params.Action
	default:
		return ""
	}
}

// callOnlyModes misbehave on the method being exercised but describe normally,
// which is what a real plugin that is correctly installed and returns a bad
// answer looks like. Without this a host test aimed at resolve, list, get, or
// act would never get past describe.
var callOnlyModes = map[string]bool{
	"no-identity":         true,
	"no-title":            true,
	"empty-response":      true,
	"over-limit-document": true,
	"hostile-document":    true,
	"request-echo":        true,
	"env-report":          true,
	"cursor-loop":         true,
	"act-hang":            true,
	"hostile-page":        true,
	"over-limit-page":     true,
	"undeclared-cells":    true,
	"page-shaped-get":     true,
	"act-failed":          true,
}

func run(mode string, req request, rawRequest []byte, subject, pidfile string) {
	if req.Method == "describe" && callOnlyModes[mode] {
		emit(describeResponse())
		return
	}
	switch mode {
	case "malformed":
		fmt.Print(`{"protocol":"` + wire + `", "resource": {`)
		return
	case "no-output":
		return
	case "extra-stdout":
		emit(describeResponse())
		fmt.Println(`{"protocol":"` + wire + `"}`)
		return
	case "trailing-log":
		emit(describeResponse())
		fmt.Println("provider: describe complete")
		return
	case "oversize":
		// One valid JSON object, far past the response byte limit.
		emit(response{
			Protocol: wire,
			Resource: &document{
				Identity: "CASH-1",
				Title:    "oversize",
				Body:     &body{Format: "text", Text: strings.Repeat("x", 512*1024)},
			},
		})
		return
	case "bad-protocol":
		emit(response{Protocol: "sidecar.terminal-resource/v99", Provider: &info{Kind: "fixture"}})
		return
	case "no-protocol":
		fmt.Println(`{"provider":{"kind":"fixture"},"matchers":[]}`)
		return
	case "resource-only":
		// A plugin that only ever speaks the frozen resource protocol, whatever
		// it is asked on. Asked on the plugin identifier this is a protocol
		// failure, and the host must say so rather than silently downgrading.
		resp := describeResourceResponse()
		resp.Protocol = resourceProtocol
		emit(resp)
		return
	case "dup-matchers":
		emit(response{
			Protocol: wire,
			Provider: &info{Kind: "fixture", Name: "Fixture"},
			Matchers: []matcher{
				{ID: "issue-key", Pattern: `\bCASH-[1-9][0-9]*\b`},
				{ID: "issue-key", Pattern: `\bGRES-[1-9][0-9]*\b`},
			},
		})
		return
	case "invalid-re2":
		emit(response{
			Protocol: wire,
			Provider: &info{Kind: "fixture", Name: "Fixture"},
			Matchers: []matcher{{ID: "broken", Pattern: `([a-z`}},
		})
		return
	case "too-many-matchers":
		resp := response{Protocol: wire, Provider: &info{Kind: "fixture"}}
		for i := 0; i < 40; i++ {
			resp.Matchers = append(resp.Matchers, matcher{ID: fmt.Sprintf("m%d", i), Pattern: fmt.Sprintf("X%d-[0-9]+", i)})
		}
		emit(resp)
		return
	case "wide-collection":
		emit(wideCollectionDescribe())
		return
	case "too-many-collections":
		emit(tooManyCollectionsDescribe())
		return
	case "watch-escape":
		emit(watchEscapeDescribe("/etc"))
		return
	case "watch-home":
		emit(watchEscapeDescribe("~"))
		return
	case "watch-relative":
		emit(watchEscapeDescribe("relative/path"))
		return
	case "too-many-watch":
		emit(tooManyWatchDescribe())
		return
	case "unknown-action-target":
		resp := describePluginResponse()
		resp.Actions = append(resp.Actions, action{ID: "mystery", Title: "Mystery", On: "sidebar"})
		emit(resp)
		return
	case "action-unknown-collection":
		resp := describePluginResponse()
		resp.Actions = append(resp.Actions, action{ID: "orphan", Title: "Orphan", On: "item", Collection: "nowhere"})
		emit(resp)
		return
	case "choice-without-choices":
		resp := describePluginResponse()
		resp.Actions = append(resp.Actions, action{
			ID: "pick", Title: "Pick", On: "global",
			Inputs: []actionInput{{ID: "which", Kind: "choice"}},
		})
		emit(resp)
		return
	case "crash":
		os.Exit(3)
	case "crash-after-output":
		emit(describeResponse())
		os.Exit(7)
	case "hang", "act-hang":
		// Not select{}: a Go program whose only goroutine blocks forever
		// panics with a deadlock, which would exercise the crash path instead
		// of the timeout path.
		time.Sleep(10 * time.Minute)
		return
	case "stderr-flood":
		chunk := strings.Repeat("noise ", 1024)
		for i := 0; i < 256; i++ {
			fmt.Fprintln(os.Stderr, chunk)
		}
		emit(answer(req, subject))
		return
	case "fork-descendant":
		forkDescendant(pidfile)
		// The parent exits immediately, without writing a response. The
		// descendant holds the inherited stdout pipe open, so only a
		// process-group kill can end the invocation.
		return
	case "error-response":
		retryable := false
		emit(response{Protocol: wire, Error: &protocolError{
			Code:      "unauthorized",
			Message:   "fixture credentials are missing",
			Retryable: &retryable,
			SetupHint: "run fixtureprovider configure",
		}})
		return
	case "unknown-code":
		emit(response{Protocol: wire, Error: &protocolError{Code: "teapot", Message: "unknown code"}})
		return
	case "hostile-document":
		emit(hostileDocument())
		return
	case "no-identity":
		emit(response{Protocol: wire, Resource: &document{Title: "has a title, no identity"}})
		return
	case "no-title":
		emit(response{Protocol: wire, Resource: &document{Identity: "CASH-1"}})
		return
	case "empty-response":
		emit(response{Protocol: wire})
		return
	case "describe-shaped-resolve":
		emit(describeResponse())
		return
	case "resolve-shaped-describe":
		emit(resolveResponse("CASH-1"))
		return
	case "page-shaped-get":
		emit(response{Protocol: wire, Page: &page{Outcome: "answered"}})
		return
	case "over-limit-document":
		emit(overLimitDocument())
		return
	case "over-limit-page":
		emit(overLimitPage())
		return
	case "hostile-page":
		emit(hostilePage())
		return
	case "undeclared-cells":
		emit(undeclaredCellsPage())
		return
	case "cursor-loop":
		// Every page points at itself. A host that paged eagerly would never
		// stop; the contract is that it pages only when the user asks.
		emit(response{Protocol: wire, Page: &page{
			Outcome:    "answered",
			Items:      []item{{ID: "loop-1", Cells: map[string]string{"title": "same row, forever"}}},
			NextCursor: "loop",
		}})
		return
	case "act-failed":
		emit(response{Protocol: wire, Outcome: &outcome{
			Status:  "failed",
			Message: "the fixture refused",
		}})
		return
	case "request-echo":
		emit(requestEcho(rawRequest))
		return
	case "env-report":
		emit(envReport())
		return
	case "slow":
		time.Sleep(2 * time.Second)
		emit(answer(req, subject))
		return
	}

	emit(answer(req, subject))
}

func answer(req request, subject string) response {
	switch req.Method {
	case "describe":
		return describeResponse()
	case "resolve":
		return resolveResponse(subject)
	case "list":
		return listResponse(req, subject)
	case "get":
		return getResponse(req, subject)
	case "act":
		return actResponse(req, subject)
	default:
		// An unknown method is an internal error, never a crash.
		return response{Protocol: wire, Error: &protocolError{
			Code:    "internal",
			Message: "unknown method",
		}}
	}
}

// describeResponse answers on whichever dialect the request arrived on: the
// frozen shape for a resource provider, the full shape for a plugin.
func describeResponse() response {
	if wire != pluginProtocol {
		return describeResourceResponse()
	}
	return describePluginResponse()
}

func describeResourceResponse() response {
	return response{
		Protocol: wire,
		Provider: &info{
			Kind:    "fixture",
			Name:    "Fixture",
			Version: "1.0.0",
			DocsURL: "https://example.test/sidecar-fixture/setup",
		},
		Matchers: fixtureMatchers(),
	}
}

func describePluginResponse() response {
	detail := true
	confirm := true
	return response{
		Protocol: wire,
		Plugin: &info{
			Kind:    "fixture",
			Name:    "Fixture",
			Version: "1.0.0",
			DocsURL: "https://example.test/sidecar-fixture/setup",
		},
		// The second kind is from a protocol version this host has never heard
		// of. It must be dropped, not refused: forward compatibility.
		Context:  []string{"project", "a-kind-from-the-future"},
		Matchers: fixtureMatchers(),
		Collections: []collection{
			{
				ID:     "results",
				Title:  "Results",
				Search: "required",
				Columns: []column{
					{ID: "rank", Label: "#", Width: 3, Align: "right", Kind: "number"},
					{ID: "title", Label: "Title", Primary: true},
					{ID: "source", Label: "Source", Width: 14},
					{ID: "excerpt", Label: "Excerpt", Secondary: true},
				},
				Sort: []sortKey{
					{ID: "relevance", Label: "Relevance", Default: "desc"},
					{ID: "recency", Label: "Recency"},
				},
				// Three filters, one of each shape the protocol has. The first
				// is the collection's SCOPE, which the host always shows.
				Filters: []filter{
					{
						ID: "scope", Label: "Scope", Kind: "choice", Default: "everything",
						Choices: []filterChoice{
							{ID: "everything", Title: "Everything"},
							{ID: "project", Title: "This project"},
							{ID: "notes", Title: "Notes only"},
						},
					},
					{
						// No stated default: the first declared choice is it.
						ID: "source", Label: "Source", Kind: "choice",
						Choices: []filterChoice{
							{ID: "any", Title: "Any"},
							{ID: "notes", Title: "notes"},
							{ID: "shell", Title: "shell"},
							{ID: "mail", Title: "mail"},
						},
					},
					{ID: "since", Label: "Since", Kind: "text"},
				},
				Detail:  &detail,
				Context: []string{"project"},
			},
			{
				ID:     "sources",
				Title:  "Sources",
				Search: "none",
				Columns: []column{
					{ID: "name", Label: "Source", Primary: true},
					{ID: "health", Label: "Health", Kind: "status"},
					{ID: "fresh", Label: "Fresh", Kind: "timestamp"},
				},
				Views: []view{{ID: "all", Title: "All"}, {ID: "stale", Title: "Stale"}},
				// Below the floor on purpose: the host clamps it to 15 rather
				// than polling a child process every five seconds.
				Refresh: &refresh{EverySeconds: 5},
			},
		},
		Actions: []action{
			{ID: "refresh-source", Title: "Refresh source", On: "item", Collection: "sources", Mutates: true, Confirm: &confirm, Key: "R"},
			{
				ID: "log-note", Title: "Log note", On: "item", Collection: "results", Mutates: true,
				Inputs: []actionInput{{ID: "text", Label: "Note", Kind: "multiline", Required: true}},
			},
			{
				ID: "capture", Title: "Capture", On: "collection", Collection: "results", Mutates: true,
				Inputs: []actionInput{{ID: "kind", Label: "Kind", Kind: "choice", Choices: []string{"note", "task"}, Default: "note"}},
			},
			{ID: "transition", Title: "Transition", On: "resource", Mutates: true, Key: "t"},
		},
	}
}

func fixtureMatchers() []matcher {
	return []matcher{
		{ID: "issue-key", Pattern: `\b(?:CASH|GRES|AVATAXUI)-[1-9][0-9]*\b`, Priority: 100},
		{ID: "build-id", Pattern: `\bBUILD-[0-9a-f]{7}\b`},
	}
}

func wideCollectionDescribe() response {
	resp := describePluginResponse()
	wide := collection{ID: "wide", Title: "Wide", Search: "none"}
	for i := 0; i < 13; i++ {
		wide.Columns = append(wide.Columns, column{ID: fmt.Sprintf("c%d", i), Label: fmt.Sprintf("C%d", i), Primary: i == 0})
	}
	resp.Collections = append(resp.Collections, wide)
	return resp
}

func tooManyCollectionsDescribe() response {
	resp := describePluginResponse()
	resp.Collections = nil
	for i := 0; i < 20; i++ {
		resp.Collections = append(resp.Collections, collection{
			ID:      fmt.Sprintf("c%d", i),
			Title:   fmt.Sprintf("Collection %d", i),
			Columns: []column{{ID: "name", Label: "Name", Primary: true}},
		})
	}
	resp.Actions = nil
	return resp
}

func watchEscapeDescribe(path string) response {
	resp := describePluginResponse()
	resp.Collections[1].Refresh = &refresh{EverySeconds: 60, Watch: []string{path}}
	return resp
}

func tooManyWatchDescribe() response {
	resp := describePluginResponse()
	var paths []string
	for i := 0; i < 9; i++ {
		paths = append(paths, fmt.Sprintf("~/fixture/watch-%d", i))
	}
	resp.Collections[1].Refresh = &refresh{Watch: paths}
	return resp
}

func resolveResponse(locator string) response {
	if locator == "" {
		return response{Protocol: wire, Error: &protocolError{Code: "not_found", Message: "no locator"}}
	}
	if strings.HasPrefix(locator, "MISSING-") {
		return response{Protocol: wire, Error: &protocolError{Code: "not_found", Message: "no such issue"}}
	}
	project, _, _ := strings.Cut(locator, "-")
	return response{
		Protocol: wire,
		Resource: &document{
			Identity: locator,
			Title:    "Synthetic " + locator + " for host tests",
			Subtitle: "Task",
			Status:   &status{Label: "IN PROGRESS", Tone: "info"},
			Fields: []field{
				{Label: "Project", Value: project},
				{Label: "Assignee", Value: "Fixture User", Kind: "user"},
				{Label: "Priority", Value: "High"},
				{Label: "Created", Value: "2026-08-01T09:00:00Z", Kind: "timestamp"},
				{Label: "Component", Value: "billing", Kind: "not-a-real-kind"},
			},
			Body: &body{
				Format: "markdown",
				Text:   "Deterministic body for `" + locator + "`.\n\n- one\n- two\n",
			},
			SourceURL:       "https://fixture.example.test/browse/" + locator,
			UpdatedAt:       "2026-08-17T17:31:00Z",
			FreshForSeconds: 60,
			// Every resolve carries an unknown field: forward compatibility is a
			// protocol rule, so a host that chokes on one has a bug.
			Extra: map[string]any{"nested": true},
		},
	}
}

func listResponse(req request, query string) response {
	params := req.Params
	if params == nil {
		return response{Protocol: wire, Error: &protocolError{Code: "invalid_request", Message: "no params"}}
	}
	switch params.Collection {
	case "results":
		return resultsPage(query, params.Cursor, params.Filters)
	case "sources":
		return sourcesPage()
	default:
		return response{Protocol: wire, Error: &protocolError{
			Code: "not_found", Message: "no such collection",
		}}
	}
}

func resultsPage(query, cursor string, filters map[string]string) response {
	switch query {
	case "filters":
		// Echo exactly what reached the plugin, so a host test can prove from
		// the outside that undeclared keys and default values were dropped
		// rather than trusting the host's own normalizer.
		return response{Protocol: wire, Page: &page{
			Outcome: "answered",
			Items: []item{{
				ID:    "filters-echo",
				Cells: map[string]string{"title": encodeFilters(filters), "source": "fixture"},
			}},
			Total: 1,
		}}
	case "coverage":
		return coveragePage()
	case "failed":
		// Every asked source failed. The page says so; it does not report an
		// empty list as "no matches", which would be a claim nothing made.
		return response{Protocol: wire, Page: &page{
			Outcome:  "failed",
			Notices:  []notice{{Tone: "danger", Text: "every source failed (the fixture is pretending its index is gone)"}},
			Coverage: []coverage{{Source: "notes", State: "failed", Reason: "index missing", ElapsedMs: 4}},
		}}
	case "":
		// The host answers an empty required query itself, so reaching here is
		// itself a finding: say so in data rather than pretending.
		return response{Protocol: wire, Page: &page{Outcome: "abstained",
			Notices: []notice{{Tone: "warning", Text: "the host sent an empty required query"}}}}
	case "nothing":
		return response{Protocol: wire, Page: &page{Outcome: "abstained"}}
	case "degraded":
		return response{Protocol: wire, Page: &page{
			Outcome: "degraded",
			Items:   []item{resultItem(1, "partial answer", "notes")},
			Total:   1,
			Notices: []notice{{Tone: "warning", Text: "1 of 4 sources did not answer (mail: checkpoint stale)"}},
		}}
	case "future":
		// An outcome from a later protocol version. The host must not read it
		// as a completeness guarantee.
		return response{Protocol: wire, Page: &page{Outcome: "entangled"}}
	}
	items := []item{
		resultItem(1, query+" schema notes", "notes"),
		resultItem(2, query+" context --json", "shell"),
		resultItem(3, query+" retrieval eval", "mail"),
	}
	next := ""
	if cursor == "" {
		next = "page-2"
	}
	return response{Protocol: wire, Page: &page{
		Outcome:    "answered",
		Items:      items,
		Total:      len(items),
		NextCursor: next,
	}}
}

// encodeFilters renders the applied filters as one deterministic string, sorted
// by key, so a test can assert on it without depending on map order.
func encodeFilters(filters map[string]string) string {
	if len(filters) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+filters[k])
	}
	return strings.Join(parts, ";")
}

// coveragePage is a degraded page carrying the two things a one-line notice
// cannot: what was held back, as counts, and thirteen sources' states, as a
// table. Thirteen is the number recall's own profile has, and it is more than
// four 200-character notices can carry.
func coveragePage() response {
	states := []struct {
		source  string
		state   string
		reason  string
		elapsed int
	}{
		{"notes", "answered", "", 12},
		{"shell", "answered", "", 31},
		{"td", "answered", "", 44},
		{"mail", "unhealthy", "checkpoint stale since 2026-08-30", 2},
		{"calendar", "timeout", "no answer within the 2s budget", 2000},
		{"web", "skipped", "not in the selected profile", 0},
		{"slack", "failed", "auth token expired", 8},
		{"drive", "answered", "", 77},
		{"photos", "skipped", "not in the selected profile", 0},
		{"music", "answered", "", 5},
		{"books", "unhealthy", "index rebuilt 4 minutes ago", 19},
		{"contacts", "answered", "", 6},
		{"archive", "timeout", "no answer within the 2s budget", 2000},
	}
	rows := make([]coverage, 0, len(states))
	for _, s := range states {
		rows = append(rows, coverage{Source: s.source, State: s.state, Reason: s.reason, ElapsedMs: s.elapsed})
	}
	return response{Protocol: wire, Page: &page{
		Outcome: "degraded",
		Items: []item{
			resultItem(1, "coverage schema notes", "notes"),
			resultItem(2, "coverage context --json", "shell"),
		},
		Total:    2,
		Notices:  []notice{{Tone: "warning", Text: "5 of 13 sources did not answer (mail: checkpoint stale)"}},
		Omitted:  &omitted{Suppressed: 1, Dropped: 6},
		Coverage: rows,
	}}
}

func resultItem(rank int, title, source string) item {
	return item{
		ID: fmt.Sprintf("rc:%s:%d", source, rank),
		Cells: map[string]string{
			"rank":    fmt.Sprint(rank),
			"title":   title,
			"source":  source,
			"excerpt": "…" + title + " appears here…",
		},
		Status:    &status{Label: "exact", Tone: "success"},
		SourceURL: "https://fixture.example.test/rc/" + source,
	}
}

func sourcesPage() response {
	return response{Protocol: wire, Page: &page{
		Outcome: "answered",
		Items: []item{
			{ID: "notes", Cells: map[string]string{"name": "notes", "health": "ok", "fresh": "2026-08-20T16:40:00Z"}},
			{
				ID:     "mail",
				Cells:  map[string]string{"name": "mail", "health": "stale", "fresh": "2026-07-01T09:00:00Z"},
				Status: &status{Label: "stale", Tone: "warning"},
			},
		},
		Total: 2,
	}}
}

func getResponse(req request, id string) response {
	if id == "" {
		return response{Protocol: wire, Error: &protocolError{Code: "invalid_request", Message: "no id"}}
	}
	if strings.HasPrefix(id, "missing") {
		return response{Protocol: wire, Error: &protocolError{Code: "not_found", Message: "no such row"}}
	}
	collectionID := ""
	if req.Params != nil {
		collectionID = req.Params.Collection
	}
	return response{
		Protocol: wire,
		Resource: &document{
			Identity: id,
			Title:    "Fixture row " + id,
			Subtitle: collectionID + " · 2026-08-14",
			Status:   &status{Label: "exact", Tone: "success"},
			Fields: []field{
				{Label: "Collection", Value: collectionID},
				{Label: "Locator", Value: id},
			},
			Body: &body{Format: "markdown", Text: "Deterministic detail for `" + id + "`.\n"},
			Sections: []section{
				{Title: "Evidence", Body: &body{Format: "markdown", Text: "Evidence for " + id + ".\n"}},
				{Title: "Attributes", Fields: []field{{Label: "Source", Value: "notes"}}},
				{Title: "Timeline", Items: []timelineItem{
					{When: "2026-08-14T10:02:00Z", Title: "Note added", Text: "first"},
					{When: "2026-08-20T16:40:00Z", Title: "Linked from td-3fa2c1"},
				}},
				// A section that claims to be two things at once. The host picks
				// one rather than refusing the document.
				{Title: "Ambiguous", Body: &body{Text: "body wins"}, Items: []timelineItem{{Title: "dropped"}}},
			},
			UpdatedAt:       "2026-08-20T16:40:00Z",
			FreshForSeconds: 60,
		},
	}
}

func actResponse(req request, actionID string) response {
	inputs := map[string]string{}
	collectionID, rowID, locator := "", "", ""
	if req.Params != nil {
		if req.Params.Inputs != nil {
			inputs = req.Params.Inputs
		}
		collectionID, rowID, locator = req.Params.Collection, req.Params.ID, req.Params.Locator
	}
	switch actionID {
	case "log-note":
		if strings.TrimSpace(inputs["text"]) == "" {
			return response{Protocol: wire, Outcome: &outcome{Status: "failed", Message: "a note needs text"}}
		}
		return response{Protocol: wire, Outcome: &outcome{
			Status:  "done",
			Message: "Logged a note for " + rowID,
			Refresh: []string{"results"},
			Open:    &openTarget{Collection: collectionID, ID: rowID},
		}}
	case "refresh-source":
		return response{Protocol: wire, Outcome: &outcome{Status: "done", Message: "Refreshed " + rowID, Refresh: []string{"sources"}}}
	case "capture":
		return response{Protocol: wire, Outcome: &outcome{Status: "done", Message: "Captured a " + inputs["kind"]}}
	case "transition":
		return response{Protocol: wire, Outcome: &outcome{Status: "done", Message: "Transitioned " + locator}}
	default:
		return response{Protocol: wire, Error: &protocolError{Code: "not_found", Message: "no such action"}}
	}
}

// hostileDocument is a well-formed response whose every string is trying to
// escape the card.
func hostileDocument() response {
	osc := "\x1b]8;;https://evil.test\x1b\\click\x1b]8;;\x1b\\"
	return response{
		Protocol: wire,
		Resource: &document{
			Identity:        "CASH-1" + osc,
			Title:           "title" + osc + "\x07\x1b[31m",
			Subtitle:        strings.Repeat("s", 1000),
			Status:          &status{Label: "state" + osc, Tone: "chartreuse"},
			Fields:          hostileFields(),
			Body:            &body{Format: "asciidoc", Text: "[x](javascript:alert(1))\n<script>y</script>\n" + osc},
			Sections:        hostileSections(osc),
			SourceURL:       "javascript:alert(1)",
			UpdatedAt:       "yesterday",
			FreshForSeconds: 1e9,
		},
	}
}

// hostileSections is over every section bound at once — more sections than the
// limit, more timeline items than the limit, an over-long heading, an OSC 8
// hyperlink in the heading, and an unparseable timestamp on every entry — while
// staying inside the response byte limit, because this case is about host-side
// truncation and the oversize case is a different fixture mode.
func hostileSections(osc string) []section {
	out := make([]section, 0, 10)
	for i := 0; i < 10; i++ {
		items := make([]timelineItem, 0, 205)
		for j := 0; j < 205; j++ {
			items = append(items, timelineItem{When: "z", Title: "t", Text: "x"})
		}
		items[0].Title = "t" + osc
		out = append(out, section{Title: strings.Repeat("H", 200) + osc, Items: items})
	}
	return out
}

func hostileFields() []field {
	out := make([]field, 0, 60)
	for i := 0; i < 60; i++ {
		out = append(out, field{
			Label: fmt.Sprintf("label-%d-%s", i, strings.Repeat("L", 200)),
			Value: strings.Repeat("V", 2000),
		})
	}
	return out
}

// hostilePage is a well-formed page whose every string is trying to escape the
// table.
func hostilePage() response {
	osc := "\x1b]8;;https://evil.test\x1b\\click\x1b]8;;\x1b\\"
	items := []item{
		{
			ID:        "row-1" + osc,
			Cells:     map[string]string{"title": "t" + osc + "\x1b[31m", "excerpt": strings.Repeat("E", 2000)},
			Status:    &status{Label: "state" + osc, Tone: "chartreuse"},
			SourceURL: "javascript:alert(1)",
		},
		// A row with no id cannot be opened or acted on, so it is not a row.
		{Cells: map[string]string{"title": "no id"}},
	}
	notices := make([]notice, 0, 10)
	for i := 0; i < 10; i++ {
		notices = append(notices, notice{Tone: "chartreuse", Text: strings.Repeat("N", 900) + osc})
	}
	return response{Protocol: wire, Page: &page{Outcome: "answered", Items: items, Notices: notices}}
}

// overLimitPage is merely too big in the dimensions the host truncates — more
// rows than the page limit, and a cell longer than the cell limit — while
// staying inside the response byte limit, because oversize stdout is a refusal
// and this case must render.
func overLimitPage() response {
	const rows = 550
	items := make([]item, 0, rows)
	for i := 0; i < rows; i++ {
		items = append(items, item{
			ID:    fmt.Sprintf("row-%d", i),
			Cells: map[string]string{"title": fmt.Sprintf("row %d", i)},
		})
	}
	items[0].Cells["title"] = strings.Repeat("T", 900)
	// Over the coverage bound too, with a reason past its own limit: the table
	// is truncated and marked, never refused.
	cover := make([]coverage, 0, 80)
	for i := 0; i < 80; i++ {
		cover = append(cover, coverage{
			Source: fmt.Sprintf("source-%d", i), State: "answered", Reason: strings.Repeat("R", 400),
		})
	}
	return response{Protocol: wire, Page: &page{
		Outcome: "answered", Items: items, Total: rows,
		Omitted:  &omitted{Suppressed: -3, Dropped: 9},
		Coverage: cover,
	}}
}

// undeclaredCellsPage keys a cell by a column the describe never declared. The
// host has nowhere to paint it, so it must drop it rather than carry it.
func undeclaredCellsPage() response {
	return response{Protocol: wire, Page: &page{
		Outcome: "answered",
		Items: []item{{
			ID:    "row-1",
			Cells: map[string]string{"title": "declared", "smuggled": "undeclared"},
		}},
	}}
}

// overLimitDocument is a well-formed document that is merely too big in every
// dimension the host truncates. It must render, not fail.
func overLimitDocument() response {
	fields := make([]field, 0, 40)
	for i := 0; i < 40; i++ {
		fields = append(fields, field{
			Label: fmt.Sprintf("f%d-%s", i, strings.Repeat("L", 120)),
			Value: strings.Repeat("V", 900),
		})
	}
	return response{
		Protocol: wire,
		Resource: &document{
			Identity: "CASH-1",
			Title:    strings.Repeat("T", 500),
			Subtitle: strings.Repeat("S", 300),
			Status:   &status{Label: strings.Repeat("P", 200), Tone: "warning"},
			Fields:   fields,
			Body:     &body{Format: "text", Text: strings.Repeat("é", 60*1024)},
		},
	}
}

// requestEcho returns the request the host actually sent, so a host test can
// assert the envelope from the outside rather than trusting its own encoder.
func requestEcho(rawRequest []byte) response {
	return response{
		Protocol: wire,
		Resource: &document{
			Identity: "request-echo",
			Title:    "request",
			Body:     &body{Format: "text", Text: string(rawRequest)},
		},
	}
}

// envReport returns the child's own execution environment as a document, so
// host tests can assert argv, working directory, and the environment allowlist
// from the outside.
func envReport() response {
	wd, _ := os.Getwd()
	return response{
		Protocol: wire,
		Resource: &document{
			Identity: "env-report",
			Title:    "environment",
			Body: &body{
				Format: "text",
				Text: "argv=" + strings.Join(os.Args, " ") + "\n" +
					"cwd=" + wd + "\n" +
					"env=" + strings.Join(os.Environ(), "\n"),
			},
		},
	}
}

func forkDescendant(pidfile string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "-mode=descendant", "-sleep=120s")
	// Inheriting stdout is the point: the host's drain cannot finish while the
	// descendant holds the write end, so only killing the process group ends
	// the invocation.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return
	}
	if pidfile != "" {
		_ = os.WriteFile(pidfile, []byte(fmt.Sprint(cmd.Process.Pid)), 0o644)
	}
	// Deliberately no Wait: the parent leaves the descendant running.
}

func emit(resp response) {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(resp); err != nil {
		os.Exit(1)
	}
}
