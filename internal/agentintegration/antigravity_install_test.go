package agentintegration

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

func antigravityFixture(t *testing.T, opts ...func(*Env)) (Service, Env, sessionHookPaths) {
	t.Helper()
	return sessionHookFixture(t, NewAntigravityAdapter().sessionHookAdapter, opts...)
}

// TestAntigravityInstallsUnderItsSharedConfigHome pins the path, which is the
// one fact about this provider that has already moved once. agy's own changelog
// records fixing its /hooks command writing to ~/.gemini/antigravity-cli
// instead of the shared ~/.gemini/config, and the runtime directory is never
// read for hooks.
func TestAntigravityInstallsUnderItsSharedConfigHome(t *testing.T) {
	_, env, paths := antigravityFixture(t)
	if want := filepath.Join(env.Home, ".gemini", "config", "hooks.json"); paths.File != want {
		t.Fatalf("the adapter targets %s, want %s", paths.File, want)
	}
}

// TestAntigravityIgnoresHerdrsConfigDirSeam records a deliberate divergence
// from upstream. Herdr honours ANTIGRAVITY_CLI_CONFIG_DIR here; the variable
// appears nowhere in agy 1.1.22's shipped binary, so it is Herdr's own test
// seam rather than the provider's contract. Following it would install into a
// directory the provider never opens, which is worse than having no override,
// and it is worth a test rather than a comment because "honour every override
// upstream honours" is exactly the reflex a later port would apply.
func TestAntigravityIgnoresHerdrsConfigDirSeam(t *testing.T) {
	elsewhere := t.TempDir()
	_, env, paths := antigravityFixture(t, func(e *Env) {
		// Set through the general accessors an adapter could reach for. None of
		// them may move this provider.
		e.ConfigHome = elsewhere
	})
	if want := filepath.Join(env.Home, ".gemini", "config", "hooks.json"); paths.File != want {
		t.Fatalf("the adapter targets %s, want %s", paths.File, want)
	}
	if strings.HasPrefix(paths.File, elsewhere) {
		t.Fatal("XDG_CONFIG_HOME moved Antigravity's hooks file, which agy does not read")
	}
}

// TestAntigravityRegistersOnPreInvocation is the event choice, asserted rather
// than described. Antigravity has no session event: the conversation id rides
// every payload, so the question is which event fires earliest and always.
// PreInvocation runs before the model is called on every turn; the tool events
// fire only for a turn that calls a tool, and PostInvocation and Stop come
// later. Herdr subscribes to PreInvocation for the same reason.
func TestAntigravityRegistersOnPreInvocation(t *testing.T) {
	a := NewAntigravityAdapter().sessionHookAdapter
	top, err := parseJSONFile([]byte(a.asset().Content))
	if err != nil {
		t.Fatal(err)
	}
	blockIdx, ok := lastMember(top, AntigravityBlockName)
	if !ok {
		t.Fatalf("the asset has no %q block:\n%s", AntigravityBlockName, a.asset().Content)
	}
	events, err := parseJSONObject(top[blockIdx].val)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].key != AntigravityEvent {
		var got []string
		for _, e := range events {
			got = append(got, e.key)
		}
		t.Fatalf("the asset registers %v, want exactly [%s]", got, AntigravityEvent)
	}
	// PreInvocation takes a flat handler list. The matcher/hooks group wrapper
	// is valid only for the two tool events, and Antigravity ignores a matcher
	// on this one, so wrapping would ship a shape the provider does not read.
	handlers, err := parseJSONArray(events[0].val)
	if err != nil {
		t.Fatalf("%s does not hold an array: %v", AntigravityEvent, err)
	}
	if len(handlers) != 1 {
		t.Fatalf("%s holds %d handlers, want one", AntigravityEvent, len(handlers))
	}
	handler, err := parseJSONObject(handlers[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, wrapped := lastMember(handler, "hooks"); wrapped {
		t.Fatal("the handler is wrapped in a matcher group, which Antigravity reads only for tool events")
	}
	if typ, _ := memberString(handler, "type"); typ != "command" {
		t.Fatalf("handler type is %q, want command; it is the only type Antigravity supports", typ)
	}
}

// TestAntigravityWritesAJSONObjectToStdout is quirk 2 of the provider half.
// Antigravity reads a hook's stdout as JSON and Sidecar's report command prints
// nothing on success, so the entry appends a printf. The suffix is also what
// makes the hook fail open: the exit status the provider sees is printf's, so a
// Sidecar that is missing or refusing cannot fail an Antigravity turn.
func TestAntigravityWritesAJSONObjectToStdout(t *testing.T) {
	command := sessionHookInstalledCommand(t, NewAntigravityAdapter().sessionHookAdapter)
	if !strings.HasSuffix(command, `; printf '{}\n'`) {
		t.Fatalf("the installed command does not end by writing an empty JSON object: %q", command)
	}
	// Nothing is interpolated into the shell. The payload travels on stdin,
	// where --hook-stdin reads it as bounded JSON, so no prompt, path or
	// environment content can reach a command Antigravity runs under sh -c.
	if strings.ContainsAny(command, "$`&|<>()") {
		t.Fatalf("the installed command carries shell metacharacters beyond the fixed suffix: %q", command)
	}
	if strings.Count(command, ";") != 1 {
		t.Fatalf("the installed command carries %d shell separators, want exactly the one suffix: %q",
			strings.Count(command, ";"), command)
	}
}

// TestASessionHookCommandIsOneInvocationPlusAtMostOneSuffix is what lets
// SessionHookArgvCorpus split on the first `;` and call that shell parsing
// done. A provider that later needed a pipeline, a subshell or a second
// separator would break that assumption silently, and the argv corpus would
// then be testing an argv nothing spawns.
func TestASessionHookCommandIsOneInvocationPlusAtMostOneSuffix(t *testing.T) {
	for _, a := range sessionHookAdapters(t) {
		t.Run(a.Provider(), func(t *testing.T) {
			command := sessionHookInstalledCommand(t, a)
			if n := strings.Count(command, ";"); n > 1 {
				t.Fatalf("the command carries %d separators: %q", n, command)
			}
			head := strings.SplitN(command, ";", 2)[0]
			if got, want := strings.TrimSpace(head), reportSessionCommand(a.Provider()); got != want {
				t.Fatalf("the command before the suffix is %q, want exactly %q", got, want)
			}
		})
	}
}

// TestAntigravityFindsItsEntryInAnyNamedBlock is why the scan walks every
// top-level block rather than only Sidecar's own. hooks.json is keyed by hook
// NAME, and blocks from every source merge, so an entry a user moved into a
// block of their own still fires. A scan that looked only under `sidecar` would
// report not-installed, install a second copy beside it, and double every
// report -- and uninstall would never find the first one.
func TestAntigravityFindsItsEntryInAnyNamedBlock(t *testing.T) {
	svc, _, paths := antigravityFixture(t)
	a := NewAntigravityAdapter().sessionHookAdapter

	moved := `{
  "mine": {
    "enabled": true,
    "` + AntigravityEvent + `": [` + string(a.integration.item()) + `]
  }
}
`
	writeFileForTest(t, paths.File, moved)

	st, err := svc.Status(AntigravityProvider)
	if err != nil {
		t.Fatal(err)
	}
	// It is Sidecar's entry, in the wrong place: needs-repair rather than
	// current, because the block it sits in is not the one Sidecar qualified.
	if st.Status != agentlifecycle.StatusNeedsRepair {
		t.Fatalf("a relocated Sidecar entry reads as %s, want needs-repair", st.Status)
	}
	applyTo(t, svc, AntigravityProvider, ActionRepair)

	after := readFileForTest(t, paths.File)
	scan := scanHookTree(true, []byte(after), a.integration.spec)
	if len(scan.owned) != 1 {
		t.Fatalf("repair left %d Sidecar entries across all blocks, want one:\n%s", len(scan.owned), after)
	}
	if scan.owned[0].block != AntigravityBlockName {
		t.Fatalf("the surviving entry is in block %q, want %q:\n%s", scan.owned[0].block, AntigravityBlockName, after)
	}
	// The user's block keeps the member that was theirs, because removing an
	// entry from a block is not the same as removing the block.
	if !strings.Contains(after, `"enabled"`) {
		t.Fatalf("repair dropped the user's own block member:\n%s", after)
	}
}

// TestAntigravityLeavesABlockItDidNotEmpty is the other half of the block rule.
// A block that held nothing but Sidecar's entry goes with the entry; a block
// that still holds something of the user's keeps everything but the entry.
func TestAntigravityLeavesABlockItDidNotEmpty(t *testing.T) {
	svc, _, paths := antigravityFixture(t)
	a := NewAntigravityAdapter().sessionHookAdapter
	entry := string(a.integration.item())

	writeFileForTest(t, paths.File, `{
  "theirs": {
    "enabled": false,
    "`+AntigravityEvent+`": [`+entry+`, {"type": "command", "command": "theirs.sh"}]
  },
  "`+AntigravityBlockName+`": {
    "`+AntigravityEvent+`": [`+entry+`]
  }
}
`)

	applyTo(t, svc, AntigravityProvider, ActionUninstall)
	after := readFileForTest(t, paths.File)

	if strings.Contains(after, "report-session") {
		t.Fatalf("uninstall left a Sidecar entry behind:\n%s", after)
	}
	if !strings.Contains(after, "theirs.sh") {
		t.Fatalf("uninstall removed a hook the user owns:\n%s", after)
	}
	if !strings.Contains(after, `"enabled"`) {
		t.Fatalf("uninstall removed a block member the user owns:\n%s", after)
	}
	if strings.Contains(after, `"`+AntigravityBlockName+`"`) {
		t.Fatalf("the block that held nothing but Sidecar's entry survived it:\n%s", after)
	}
}

// TestAntigravityKeepsAHandlerListItDidNotWrite pins the flat-list removal: an
// event array Sidecar shares with the user loses exactly one element.
func TestAntigravityKeepsAHandlerListItDidNotWrite(t *testing.T) {
	svc, _, paths := antigravityFixture(t)
	a := NewAntigravityAdapter().sessionHookAdapter

	writeFileForTest(t, paths.File, `{
  "`+AntigravityBlockName+`": {
    "`+AntigravityEvent+`": [{"type": "command", "command": "first.sh"}, `+string(a.integration.item())+`, {"type": "command", "command": "last.sh"}]
  }
}
`)
	applyTo(t, svc, AntigravityProvider, ActionUninstall)

	after := readFileForTest(t, paths.File)
	var parsed map[string]map[string][]map[string]any
	if err := json.Unmarshal([]byte(after), &parsed); err != nil {
		t.Fatalf("uninstall produced invalid JSON: %v\n%s", err, after)
	}
	handlers := parsed[AntigravityBlockName][AntigravityEvent]
	if len(handlers) != 2 {
		t.Fatalf("the event array holds %d handlers, want the user's two:\n%s", len(handlers), after)
	}
	if handlers[0]["command"] != "first.sh" || handlers[1]["command"] != "last.sh" {
		t.Fatalf("the user's handlers were reordered or replaced:\n%s", after)
	}
}
