package pluginhost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The authoring guide's example plugin, driven through the real host.
//
// docs/guides/active/creating-plugins.md walks an author through
// docs/guides/examples/hello-plugin/hello_plugin.py and tells them it passes
// `sidecar plugin check`. This is what keeps that true: the file the guide
// prints is the file this test runs, through the same Manager, the same
// describe validation, and the same page and document sanitization a pane
// would use. A protocol change that would silence the example fails here
// rather than in a reader's terminal.

// examplePluginPath is the guide's example, addressed from this package's
// directory.
const examplePluginPath = "../../docs/guides/examples/hello-plugin/hello_plugin.py"

// newExamplePlugin wires the example into a real Manager and describes it.
// It skips rather than fails when python3 is absent: a machine without it can
// still run the rest of the suite, and CI has it.
func newExamplePlugin(t *testing.T) *Manager {
	t.Helper()

	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH; the guide's example plugin needs it")
	}
	script, err := filepath.Abs(examplePluginPath)
	if err != nil {
		t.Fatalf("resolving the example plugin: %v", err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("the guide's example plugin is missing: %v", err)
	}

	provider, err := NewCommandProvider(CommandConfig{
		Instance: "hello",
		Argv:     []string{python, script},
		Dir:      t.TempDir(),
		HostEnv:  os.Environ(),
		Host:     HostInfo{Name: "sidecar", Version: "test"},
		Protocol: Protocol,
		Home:     t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewCommandProvider: %v", err)
	}

	m := NewManager(ManagerOptions{})
	m.SetProviders([]Provider{provider}, nil)
	m.DescribeAll(context.Background())
	return m
}

func TestGuideExamplePluginDescribesWhatTheGuideSays(t *testing.T) {
	m := newExamplePlugin(t)

	desc, ok := m.Description("hello")
	if !ok {
		status, _ := m.Status("hello")
		t.Fatalf("describe was refused: %+v", status)
	}
	if desc.Info.Kind != "hello" || desc.Info.Name != "Hello" {
		t.Fatalf("identity = %+v, want the kind and name the guide prints", desc.Info)
	}
	if len(desc.Context) != 1 || desc.Context[0] != ContextProject {
		t.Fatalf("context = %v, want [project]", desc.Context)
	}
	if len(desc.Collections) != 1 {
		t.Fatalf("collections = %d, want the one the guide declares", len(desc.Collections))
	}

	greetings, ok := desc.Collection("greetings")
	if !ok {
		t.Fatal("the greetings collection is missing")
	}
	if greetings.Search != SearchOptional {
		t.Fatalf("search = %q, want optional", greetings.Search)
	}
	if !greetings.Detail {
		t.Fatal("greetings declared detail:true and lost it")
	}
	primary, ok := greetings.PrimaryColumn()
	if !ok || primary.ID != "name" {
		t.Fatalf("primary column = %+v, want name", primary)
	}
	if len(greetings.Sort) != 2 || greetings.Sort[0].Default != SortAsc {
		t.Fatalf("sort keys = %+v, want name defaulting ascending", greetings.Sort)
	}
	// The guide's example declares no matchers and no actions, which is what
	// makes it the smallest complete plugin rather than a resource provider.
	if len(desc.Matchers) != 0 || len(desc.Actions) != 0 {
		t.Fatalf("matchers = %d, actions = %d, want none", len(desc.Matchers), len(desc.Actions))
	}
}

func TestGuideExamplePluginListsAndGets(t *testing.T) {
	m := newExamplePlugin(t)
	ctx := context.Background()

	page, err := m.List(ctx, ListRequest{Instance: "hello", Params: ListParams{Collection: "greetings"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Outcome != OutcomeAnswered || len(page.Items) != 3 {
		t.Fatalf("unqueried page = %+v, want three answered rows", page)
	}
	if page.Items[0].Cells["language"] == "" {
		t.Fatalf("first row = %+v, want every declared column filled", page.Items[0])
	}

	filtered, err := m.List(ctx, ListRequest{Instance: "hello", Params: ListParams{Collection: "greetings", Query: "french"}})
	if err != nil {
		t.Fatalf("List with a query: %v", err)
	}
	if len(filtered.Items) != 1 || filtered.Items[0].ID != "fr" {
		t.Fatalf("query 'french' = %+v, want the one row", filtered.Items)
	}

	// An empty result set is abstained, not answered: the guide's honesty
	// section turns on the host being able to tell the two apart.
	empty, err := m.List(ctx, ListRequest{Instance: "hello", Params: ListParams{Collection: "greetings", Query: "nothing matches this"}})
	if err != nil {
		t.Fatalf("List with no matches: %v", err)
	}
	if empty.Outcome != OutcomeAbstained || len(empty.Items) != 0 {
		t.Fatalf("empty page = %+v, want abstained with no rows", empty)
	}

	doc, err := m.Get(ctx, GetRequest{
		Instance: "hello",
		Params:   GetParams{Collection: "greetings", ID: filtered.Items[0].ID},
		Context:  &Context{Project: &ProjectContext{Root: "/tmp/x", WorkDir: "/tmp/x", Name: "sidecar", Branch: "main"}},
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if doc.Identity != "fr" || doc.Title != "Bonjour" {
		t.Fatalf("document = %+v", doc)
	}
	if len(doc.Sections) != 1 || len(doc.Sections[0].Items) != 1 {
		t.Fatalf("sections = %+v, want one timeline section", doc.Sections)
	}
	// The plugin declared project context, so the host sent it and the
	// document can name it.
	var asked string
	for _, f := range doc.Fields {
		if f.Label == "Asked from" {
			asked = f.Value
		}
	}
	if asked != "sidecar" {
		t.Fatalf("Asked from = %q, want the project the host sent", asked)
	}
}

func TestGuideExamplePluginTypesItsFailures(t *testing.T) {
	m := newExamplePlugin(t)

	_, err := m.Get(context.Background(), GetRequest{Instance: "hello", Params: GetParams{Collection: "greetings", ID: "nope"}})
	if err == nil {
		t.Fatal("a missing row must be a typed error, not an empty document")
	}
	if !strings.Contains(err.Error(), "no greeting") {
		t.Fatalf("error = %v, want the plugin's own message", err)
	}

	// A collection the describe never declared is refused before a process is
	// started, so the example never sees the call.
	if _, err := m.List(context.Background(), ListRequest{Instance: "hello", Params: ListParams{Collection: "nowhere"}}); err == nil {
		t.Fatal("an undeclared collection must be refused")
	}
}
