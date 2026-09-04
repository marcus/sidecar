package agentintegration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

func grokFixture(t *testing.T, opts ...func(*Env)) (Service, Env, sessionHookPaths) {
	t.Helper()
	return sessionHookFixture(t, NewGrokAdapter().sessionHookAdapter, opts...)
}

// TestGrokOwnsItsOwnFileInTheDirectoryGrokGlobs pins where the entry lands.
// grok merges every `<grok home>/hooks/*.json`, so Sidecar writes one of its
// own rather than editing the user's, and honours GROK_HOME, which is grok's
// own variable rather than Herdr's seam.
func TestGrokOwnsItsOwnFileInTheDirectoryGrokGlobs(t *testing.T) {
	_, env, paths := grokFixture(t)
	if want := filepath.Join(env.Home, ".grok", "hooks", GrokHookFileName); paths.File != want {
		t.Fatalf("the adapter targets %s, want %s", paths.File, want)
	}

	relocated := t.TempDir()
	_, _, moved := grokFixture(t, func(e *Env) { e.GrokHome = relocated })
	if want := filepath.Join(relocated, "hooks", GrokHookFileName); moved.File != want {
		t.Fatalf("GROK_HOME did not move the hook file: %s", moved.File)
	}
}

// TestGrokKeepsAHookAUserAddedToSidecarsFile is the reason ownership stays the
// entry rule even for a file only Sidecar writes. Herdr deletes its equivalent
// file outright at uninstall; a user who added a hook of their own to a file
// named after Sidecar would lose it. The entry rule cannot do that, and it
// costs nothing.
func TestGrokKeepsAHookAUserAddedToSidecarsFile(t *testing.T) {
	svc, _, paths := grokFixture(t)
	applyTo(t, svc, GrokProvider, ActionInstall)

	// The user adds their own group beside Sidecar's, in Sidecar's file.
	installed := readFileForTest(t, paths.File)
	withTheirs := strings.Replace(installed,
		`"SessionStart": [`,
		`"SessionStart": [
        {
          "hooks": [
            {
              "type": "command",
              "command": "theirs.sh"
            }
          ]
        },`, 1)
	if withTheirs == installed {
		t.Fatal("fixture did not add a user hook")
	}
	writeFileForTest(t, paths.File, withTheirs)

	applyTo(t, svc, GrokProvider, ActionUninstall)
	after := readFileForTest(t, paths.File)
	if strings.Contains(after, "report-session") {
		t.Fatalf("uninstall left Sidecar's entry behind:\n%s", after)
	}
	if !strings.Contains(after, "theirs.sh") {
		t.Fatalf("uninstall deleted a hook the user added to Sidecar's own file:\n%s", after)
	}
}

// TestGrokRemovesItsFileWhenItHeldNothingElse is the other half: a file that
// held nothing but Sidecar's entry is one Sidecar created, so it goes.
func TestGrokRemovesItsFileWhenItHeldNothingElse(t *testing.T) {
	svc, _, paths := grokFixture(t)
	applyTo(t, svc, GrokProvider, ActionInstall)
	applyTo(t, svc, GrokProvider, ActionUninstall)
	if _, err := readFileMaybe(paths.File); err == nil {
		t.Fatal("uninstall left an empty hook file behind")
	}
}

// TestGrokRegistersOnSessionStartWithNoMatcher pins the group shape. grok's
// documentation says an omitted matcher matches everything, and a matcher on
// SessionStart would test the start source (startup, resume, and so on) --
// binding the session is right for all of them, so there is nothing to filter.
// Herdr's config has the same shape.
func TestGrokRegistersOnSessionStartWithNoMatcher(t *testing.T) {
	a := NewGrokAdapter().sessionHookAdapter
	top, err := parseJSONFile([]byte(a.asset().Content))
	if err != nil {
		t.Fatal(err)
	}
	hooksIdx, ok := lastMember(top, "hooks")
	if !ok {
		t.Fatalf("the asset has no hooks member:\n%s", a.asset().Content)
	}
	events, err := parseJSONObject(top[hooksIdx].val)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].key != "SessionStart" {
		t.Fatalf("the asset registers %d events, want exactly SessionStart:\n%s", len(events), a.asset().Content)
	}
	groups, err := parseJSONArray(events[0].val)
	if err != nil || len(groups) != 1 {
		t.Fatalf("SessionStart holds %d groups (%v), want one", len(groups), err)
	}
	group, err := parseJSONObject(groups[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, hasMatcher := lastMember(group, "matcher"); hasMatcher {
		t.Fatalf("the group carries a matcher; an omitted one already matches every start source:\n%s", a.asset().Content)
	}
	if _, ok := lastMember(group, "hooks"); !ok {
		t.Fatalf("the group has no hooks array, so it is not grok's grouped shape:\n%s", a.asset().Content)
	}
}

// TestGrokAndClaudeEntriesCoexistOnOneMachine is the offline half of the
// cross-firing story. grok reads ~/.claude/settings.json by design, so both
// entries can be installed at once and both will fire inside a grok session.
// They must not interfere with each other on disk: each installer owns its own
// file, and neither adopts, rewrites or removes the other's entry.
//
// Which of the two actually binds is decided at report time and is proved in
// internal/cli by TestBothEntriesFireInAGrokSessionAndOnlyGroksBinds.
func TestGrokAndClaudeEntriesCoexistOnOneMachine(t *testing.T) {
	home := t.TempDir()
	env := Env{
		Home:            home,
		ConfigHome:      filepath.Join(home, ".config"),
		LookPath:        func(file string) (string, error) { return filepath.Join(home, "bin", file), nil },
		ProviderVersion: func(string) string { return "" },
		UID:             os.Getuid(),
	}
	svc := Service{Env: env, Adapters: DefaultAdapters()}
	applyTo(t, svc, GrokProvider, ActionInstall)
	applyTo(t, svc, ClaudeProvider, ActionInstall)

	grokPaths := NewGrokAdapter().integration.pathsFor(env)
	claudePaths := claudePathsFor(env)
	if grokPaths.File == claudePaths.Settings {
		t.Fatal("the two integrations target the same file")
	}

	for _, tc := range []struct {
		name     string
		path     string
		wantKind string
	}{
		{"grok", grokPaths.File, "--kind grok"},
		{"claude", claudePaths.Settings, "--kind claude"},
	} {
		got := readFileForTest(t, tc.path)
		if !strings.Contains(got, tc.wantKind) {
			t.Fatalf("%s's file does not carry %s:\n%s", tc.name, tc.wantKind, got)
		}
	}

	// Each status is current, and neither reads the other's entry as damage.
	for _, provider := range []string{GrokProvider, ClaudeProvider} {
		st, err := svc.Status(provider)
		if err != nil {
			t.Fatal(err)
		}
		if st.Status != agentlifecycle.StatusCurrent {
			t.Fatalf("%s reads as %s with both installed, want current", provider, st.Status)
		}
	}

	// Removing one leaves the other exactly as it was.
	before := readFileForTest(t, claudePaths.Settings)
	applyTo(t, svc, GrokProvider, ActionUninstall)
	if after := readFileForTest(t, claudePaths.Settings); after != before {
		t.Fatalf("uninstalling grok changed Claude's settings.json\n got: %s\nwant: %s", after, before)
	}
	st, err := svc.Status(ClaudeProvider)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("Claude reads as %s after grok was removed, want current", st.Status)
	}
}
