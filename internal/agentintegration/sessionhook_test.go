package agentintegration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentlifecycle"
)

// The shared session-identity adapter's suite.
//
// Every property here is one the four providers share, so it is asserted for
// all of them off DefaultAdapters rather than written four times. A provider
// added later is covered the moment it is registered, which is the point: these
// are the invariants that make an installer safe to point at a file somebody
// else owns, and a new descriptor is exactly the kind of change that could
// break one without anybody noticing.
//
// Everything runs inside t.TempDir with an injected Env. That is not a nicety:
// this machine carries a real ~/.gemini/config/hooks.json and a real
// ~/.cursor/hooks.json, and a test that touched either would be rewriting the
// configuration of an agent the developer runs.

// sessionHookAdapters is every registered adapter built on the shared
// session-identity implementation.
func sessionHookAdapters(t *testing.T) []sessionHookAdapter {
	t.Helper()
	var out []sessionHookAdapter
	for _, a := range DefaultAdapters() {
		switch v := a.(type) {
		case AntigravityAdapter:
			out = append(out, v.sessionHookAdapter)
		}
	}
	if len(out) == 0 {
		t.Fatal("no session-identity adapters are registered, so this file asserts nothing")
	}
	return out
}

// sessionHookFixture is one adapter against an empty temporary home with its
// provider CLI on a fake PATH.
func sessionHookFixture(t *testing.T, a sessionHookAdapter, opts ...func(*Env)) (Service, Env, sessionHookPaths) {
	t.Helper()
	home := t.TempDir()
	command := a.integration.command
	env := Env{
		Home:       home,
		ConfigHome: filepath.Join(home, ".config"),
		LookPath: func(file string) (string, error) {
			if file == command {
				return filepath.Join(home, "bin", command), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "" },
		UID:             os.Getuid(),
	}
	for _, o := range opts {
		o(&env)
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, a.integration.pathsFor(env)
}

// TestASessionHookInstallsAndUninstallsCleanly is the round trip: an empty tree
// gains exactly the canonical file, reads as current, and gives it back.
func TestASessionHookInstallsAndUninstallsCleanly(t *testing.T) {
	for _, a := range sessionHookAdapters(t) {
		t.Run(a.Provider(), func(t *testing.T) {
			svc, _, paths := sessionHookFixture(t, a)

			st, err := svc.Status(a.Provider())
			if err != nil {
				t.Fatal(err)
			}
			if st.Status != agentlifecycle.StatusNotInstalled {
				t.Fatalf("a fresh tree reads as %s, want not-installed", st.Status)
			}

			applyTo(t, svc, a.Provider(), ActionInstall)
			got := readFileForTest(t, paths.File)
			if want := a.asset().Content; got != want {
				t.Fatalf("installed file is not the canonical asset\n got: %s\nwant: %s", got, want)
			}
			st, err = svc.Status(a.Provider())
			if err != nil {
				t.Fatal(err)
			}
			if st.Status != agentlifecycle.StatusCurrent {
				t.Fatalf("after install the status is %s, want current", st.Status)
			}
			if st.EffectiveTier != agentlifecycle.TierSessionIdentity {
				t.Fatalf("tier %q after install, want session-identity", st.EffectiveTier)
			}

			// Installing again is a no-op rather than a second entry. The
			// difference matters because a doubled entry reports every session
			// twice, and idempotency is what makes an install verb safe to put
			// behind a button somebody can press twice.
			if p := applyTo(t, svc, a.Provider(), ActionInstall); !p.Unchanged {
				t.Fatalf("a second install produced %d operations, want none", len(p.Ops))
			}
			if scan := scanHookTree(true, []byte(readFileForTest(t, paths.File)), a.integration.spec); len(scan.owned) != 1 {
				t.Fatalf("after two installs the file holds %d Sidecar entries, want one", len(scan.owned))
			}

			if p := applyTo(t, svc, a.Provider(), ActionUninstall); p.Unchanged {
				t.Fatal("uninstall had nothing to remove")
			}
			if _, err := os.Lstat(paths.File); !os.IsNotExist(err) {
				t.Fatal("uninstall left behind a file Sidecar created")
			}
		})
	}
}

// TestASessionHookEntryClaimsItsOwnProviderKind is the binding contract. The
// entry exists to say which conversation occupies the pane, and it says so with
// --kind; a wrong kind there is a cold restore offering one agent's session to
// another CLI, which is exactly the bug td-11040b closed.
func TestASessionHookEntryClaimsItsOwnProviderKind(t *testing.T) {
	for _, a := range sessionHookAdapters(t) {
		t.Run(a.Provider(), func(t *testing.T) {
			command := sessionHookInstalledCommand(t, a)
			if !invokesReportSession(command) {
				t.Fatalf("the installed command %q is not recognised as Sidecar's own, so uninstall could never find it", command)
			}
			want := "--kind " + a.Provider()
			if !strings.Contains(command, want) {
				t.Fatalf("the installed command %q does not carry %q", command, want)
			}
			if strings.Contains(command, "--seq") {
				t.Fatalf("the installed command %q sends a sequence; the store assigns those", command)
			}
		})
	}
}

// sessionHookInstalledCommand digs the command out of the canonical asset, so
// it is read from the bytes an install writes rather than from a copy of them.
func sessionHookInstalledCommand(t *testing.T, a sessionHookAdapter) string {
	t.Helper()
	scan := scanHookTree(true, []byte(a.asset().Content), a.integration.spec)
	if scan.parseErr != "" {
		t.Fatalf("the canonical asset does not survive its own scan: %s", scan.parseErr)
	}
	if len(scan.owned) != 1 {
		t.Fatalf("the canonical asset scans as %d Sidecar entries, want exactly one", len(scan.owned))
	}
	if !scan.owned[0].groupCanonical {
		t.Fatal("the canonical asset's own entry does not read as canonically placed")
	}
	entry, err := parseJSONObject(scan.owned[0].raw)
	if err != nil {
		t.Fatal(err)
	}
	command, ok := entryCommand(entry, a.integration.spec)
	if !ok {
		t.Fatal("the canonical entry carries no command")
	}
	return command
}

// TestASessionHookPreservesEverythingElseInTheFile is the ownership promise
// made against a file full of somebody else's configuration: install adds one
// entry, uninstall takes back exactly that entry, and the bytes either side are
// the user's.
func TestASessionHookPreservesEverythingElseInTheFile(t *testing.T) {
	for _, a := range sessionHookAdapters(t) {
		t.Run(a.Provider(), func(t *testing.T) {
			svc, _, paths := sessionHookFixture(t, a)
			before := sessionHookUserFile(a.integration.spec)
			writeFileForTest(t, paths.File, before)

			applyTo(t, svc, a.Provider(), ActionInstall)
			after := readFileForTest(t, paths.File)

			// Everything the user wrote is still readable as itself.
			var userAfter any
			if err := json.Unmarshal([]byte(after), &userAfter); err != nil {
				t.Fatalf("install produced invalid JSON: %v\n%s", err, after)
			}
			if !strings.Contains(after, "keep-me") {
				t.Fatalf("install dropped a top-level key the user owns:\n%s", after)
			}
			if !strings.Contains(after, "user-hook.sh") {
				t.Fatalf("install dropped a hook the user owns:\n%s", after)
			}

			if p := applyTo(t, svc, a.Provider(), ActionUninstall); p.Unchanged {
				t.Fatal("uninstall found nothing to remove")
			}
			back := readFileForTest(t, paths.File)
			if !jsonEqualForTest(t, back, before) {
				t.Fatalf("uninstall did not give the file back\n got: %s\nwant: %s", back, before)
			}
			if !strings.Contains(back, "user-hook.sh") {
				t.Fatalf("uninstall removed a hook the user owns:\n%s", back)
			}
		})
	}
}

// sessionHookUserFile builds a plausible pre-existing configuration for one
// provider's shape: a top-level key that has nothing to do with hooks, and one
// hook of the user's own on an event Sidecar does not touch.
func sessionHookUserFile(spec hookEntrySpec) string {
	handler := `{"type":"command","` + spec.cmdKey() + `":"user-hook.sh"}`
	if spec.flat {
		handler = `[` + handler + `]`
	} else {
		handler = `[{"hooks":[` + handler + `]}]`
	}
	events := `{"UserOwnedEvent":` + handler + `}`
	var top string
	if spec.namedBlocks {
		top = `{"keep-me":{"UserOwnedEvent":` + handler + `},"user-block":` + events + `}`
	} else {
		top = `{"keep-me":{"unrelated":true},"` + spec.blockKey() + `":` + events + `}`
	}
	var out []byte
	var err error
	if out, err = jsonIndentForTest(top); err != nil {
		return top
	}
	return string(out)
}

func jsonIndentForTest(s string) ([]byte, error) {
	members, err := parseJSONFile([]byte(s))
	if err != nil {
		return nil, err
	}
	return renderJSONFile(members), nil
}

func jsonEqualForTest(t *testing.T, a, b string) bool {
	t.Helper()
	return mustParseAnyEqual(mustParseAny(t, a), mustParseAny(t, b))
}

func mustParseAnyEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// TestASessionHookNeverAdoptsAUserHookThatLooksLikeSidecars is the direction
// that matters. A command that merely mentions the verb is the user's, forever:
// adopting one would mean uninstall deleting a hook Sidecar did not write.
func TestASessionHookNeverAdoptsAUserHookThatLooksLikeSidecars(t *testing.T) {
	for _, a := range sessionHookAdapters(t) {
		t.Run(a.Provider(), func(t *testing.T) {
			spec := a.integration.spec
			for _, lookalike := range []string{
				"echo sidecar agent report-session --kind " + a.Provider(),
				"my-wrapper sidecar agent report-session --hook-stdin",
				"sidecar-helper agent report-session --kind " + a.Provider(),
			} {
				file := sessionHookFileWithCommand(spec, lookalike)
				scan := scanHookTree(true, []byte(file), spec)
				if scan.parseErr != "" {
					t.Fatalf("fixture unusable: %s", scan.parseErr)
				}
				if len(scan.owned) != 0 {
					t.Fatalf("Sidecar claimed a user hook running %q", lookalike)
				}
			}
		})
	}
}

// sessionHookFileWithCommand renders a file holding one handler on the
// canonical event running the given command.
func sessionHookFileWithCommand(spec hookEntrySpec, command string) string {
	entry := json.RawMessage(`{"type":"command","` + spec.cmdKey() + `":` + string(mustJSONString(command)) + `}`)
	item := entry
	if !spec.flat {
		item = marshalJSONObject([]jsonMember{{key: "hooks", val: marshalJSONArray([]json.RawMessage{entry})}})
	}
	top, err := appendCanonicalEntry(nil, item, spec)
	if err != nil {
		return "{}"
	}
	return string(renderJSONFile(top))
}

// TestASessionHookRefusesAFileItCannotRead is the safety gate. Rewriting a file
// the scan could not interpret is a clobber by construction, so every verb
// declines and says why rather than guessing.
func TestASessionHookRefusesAFileItCannotRead(t *testing.T) {
	for _, a := range sessionHookAdapters(t) {
		t.Run(a.Provider(), func(t *testing.T) {
			svc, _, paths := sessionHookFixture(t, a)
			writeFileForTest(t, paths.File, "{ this is not json")

			st, err := svc.Status(a.Provider())
			if err != nil {
				t.Fatal(err)
			}
			if st.Status != agentlifecycle.StatusNeedsRepair {
				t.Fatalf("an unreadable file reads as %s, want needs-repair", st.Status)
			}
			for _, act := range Actions() {
				if _, err := svc.Apply(a.Provider(), act); err == nil {
					t.Fatalf("%s rewrote a file Sidecar could not read", act)
				}
			}
			if got := readFileForTest(t, paths.File); got != "{ this is not json" {
				t.Fatalf("the unreadable file was modified: %s", got)
			}
		})
	}
}

// TestASessionHookRepairsADuplicateOrTamperedEntry pins the two damaged shapes
// a converge verb has to recognise. Both read as needs-repair rather than
// current, because an entry that still invokes the verb is still Sidecar's, and
// repair converges on exactly one canonical copy.
func TestASessionHookRepairsADuplicateOrTamperedEntry(t *testing.T) {
	for _, a := range sessionHookAdapters(t) {
		t.Run("duplicate/"+a.Provider(), func(t *testing.T) {
			svc, _, paths := sessionHookFixture(t, a)
			applyTo(t, svc, a.Provider(), ActionInstall)
			// A second copy of the same entry, which would report every session
			// twice.
			doubled := sessionHookDouble(t, a, readFileForTest(t, paths.File))
			writeFileForTest(t, paths.File, doubled)

			st, err := svc.Status(a.Provider())
			if err != nil {
				t.Fatal(err)
			}
			if st.Status != agentlifecycle.StatusNeedsRepair {
				t.Fatalf("two installed entries read as %s, want needs-repair", st.Status)
			}
			applyTo(t, svc, a.Provider(), ActionRepair)
			scan := scanHookTree(true, []byte(readFileForTest(t, paths.File)), a.integration.spec)
			if len(scan.owned) != 1 {
				t.Fatalf("repair left %d Sidecar entries, want exactly one", len(scan.owned))
			}
		})

		t.Run("tampered/"+a.Provider(), func(t *testing.T) {
			svc, _, paths := sessionHookFixture(t, a)
			applyTo(t, svc, a.Provider(), ActionInstall)
			installed := readFileForTest(t, paths.File)
			tampered := strings.Replace(installed, "--hook-stdin", "--hook-stdin --source hand-written", 1)
			if tampered == installed {
				t.Fatal("fixture did not tamper with anything")
			}
			writeFileForTest(t, paths.File, tampered)

			st, err := svc.Status(a.Provider())
			if err != nil {
				t.Fatal(err)
			}
			if st.Status != agentlifecycle.StatusNeedsRepair {
				t.Fatalf("a tampered entry reads as %s, want needs-repair", st.Status)
			}
			applyTo(t, svc, a.Provider(), ActionRepair)
			if got := readFileForTest(t, paths.File); got != a.asset().Content {
				t.Fatalf("repair did not converge on the canonical asset:\n%s", got)
			}
		})
	}
}

// sessionHookDouble returns the installed file with Sidecar's entry present
// twice, which is what an install run by two Sidecar versions would leave.
func sessionHookDouble(t *testing.T, a sessionHookAdapter, installed string) string {
	t.Helper()
	spec := a.integration.spec
	top, err := parseJSONFile([]byte(installed))
	if err != nil {
		t.Fatal(err)
	}
	top, err = appendCanonicalEntry(top, a.integration.item(), spec)
	if err != nil {
		t.Fatal(err)
	}
	return string(renderJSONFile(top))
}

// TestASessionHookRefusesInstallWithoutItsProviderButStillCleansUp is the same
// rule Claude follows: install is gated on the CLI being installed, and
// uninstall is gated on what is on disk, so a user who removed the provider can
// still take Sidecar's entry back out.
func TestASessionHookRefusesInstallWithoutItsProviderButStillCleansUp(t *testing.T) {
	for _, a := range sessionHookAdapters(t) {
		t.Run(a.Provider(), func(t *testing.T) {
			svc, _, paths := sessionHookFixture(t, a)
			applyTo(t, svc, a.Provider(), ActionInstall)

			gone := Service{Env: svc.Env, Adapters: svc.Adapters}
			gone.Env.LookPath = func(string) (string, error) { return "", errors.New("not found") }

			st, err := gone.Status(a.Provider())
			if err != nil {
				t.Fatal(err)
			}
			if st.Status != agentlifecycle.StatusProviderMissing {
				t.Fatalf("status %s with no CLI, want provider-missing", st.Status)
			}
			if st.EffectiveTier != agentlifecycle.TierScreenFallback {
				t.Fatalf("tier %q with no provider", st.EffectiveTier)
			}
			if _, err := gone.Apply(a.Provider(), ActionInstall); err == nil {
				t.Fatal("install was allowed with the provider CLI missing")
			}
			if p := applyTo(t, gone, a.Provider(), ActionUninstall); p.Unchanged {
				t.Fatal("uninstall had nothing to remove")
			}
			if _, err := os.Lstat(paths.File); !os.IsNotExist(err) {
				t.Fatal("the entry was not removed")
			}
		})
	}
}

// TestASessionHookOffersExactlyTheActionsThatWouldNotRefuse keeps the surface
// honest: a pill a user can press is an action the service would run.
func TestASessionHookOffersExactlyTheActionsThatWouldNotRefuse(t *testing.T) {
	for _, a := range sessionHookAdapters(t) {
		t.Run(a.Provider(), func(t *testing.T) {
			svc, _, _ := sessionHookFixture(t, a)
			for _, step := range []struct {
				name string
				act  Action
			}{
				{"not installed", ""},
				{"installed", ActionInstall},
			} {
				if step.act != "" {
					applyTo(t, svc, a.Provider(), step.act)
				}
				st, err := svc.Status(a.Provider())
				if err != nil {
					t.Fatal(err)
				}
				offered := map[Action]bool{}
				for _, act := range st.Offered {
					offered[act] = true
				}
				for _, act := range Actions() {
					_, err := svc.Plan(a.Provider(), act)
					if wouldRun := err == nil; wouldRun != offered[act] {
						t.Fatalf("%s: %s offered=%v but planning err=%v", step.name, act, offered[act], err)
					}
				}
			}
		})
	}
}
