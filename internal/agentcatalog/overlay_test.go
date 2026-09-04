package agentcatalog

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeOverlay(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Every file Sidecar ships has to parse, and the invariants the loader assumes
// have to hold across all of them. parseBundled panics rather than returning an
// error, so without this the first proof that a new family's file is well
// formed would be a user's binary refusing to show a picker.
func TestEveryBundledFamilyParses(t *testing.T) {
	names, err := bundledFamilies.ReadDir("families")
	if err != nil {
		t.Fatal(err)
	}
	files := parseBundled()
	if len(files) != len(names) {
		t.Fatalf("parsed %d families from %d files", len(files), len(names))
	}

	orders := map[int]string{}
	for _, entry := range names {
		id := strings.TrimSuffix(entry.Name(), ".toml")
		file, ok := files[id]
		if !ok {
			t.Errorf("%s declares an id that is not its file name", entry.Name())
			continue
		}
		// The file name is the id, so an overlay file can name a bundled family
		// by being called after it and the loader never needs a second index.
		if file.ID != id {
			t.Errorf("%s has id %q", entry.Name(), file.ID)
		}
		if strings.TrimSpace(file.Name) == "" || strings.TrimSpace(file.Short) == "" {
			t.Errorf("%s has no name or no short label", entry.Name())
		}
		if file.Order == nil {
			if !file.Legacy {
				t.Errorf("%s has no order and is not legacy; it would sort past every other family", entry.Name())
			}
			continue
		}
		if other, dup := orders[*file.Order]; dup {
			t.Errorf("%s and %s both claim order %d; the picker order would depend on the id tiebreak", entry.Name(), other, *file.Order)
		}
		orders[*file.Order] = entry.Name()
	}
}

// Resume arguments and skip-permissions flags are argv entries, so a value that
// needs shell quoting is a value somebody wrote wrong. DisplayCommand quotes
// rather than fails, which would hide the mistake in a command line that still
// looks plausible.
func TestBundledFamilyArgumentsAreBareArgvEntries(t *testing.T) {
	for _, family := range append(Families(), LegacyFamilies()...) { //nolint:gocritic // one pass over both launchable buckets
		args := append([]string{family.Command, family.SkipPermissionsArg}, family.LaunchArgs...)
		args = append(args, family.ResumeArgs...)
		for _, arg := range args {
			if arg == "" {
				continue
			}
			if displayCommandUnsafe.MatchString(arg) {
				t.Errorf("%s carries argv entry %q, which a shell would read as more than one word", family.ID, arg)
			}
		}
		if len(family.ResumeArgs) > 0 && len(family.ResumeKinds) == 0 {
			t.Errorf("%s has resume arguments and no resume kinds, so CanResume is false and the arguments are dead data", family.ID)
		}
		for _, kind := range family.ResumeKinds {
			if kind != "id" && kind != "path" {
				t.Errorf("%s resumes from kind %q, which no session reference has", family.ID, kind)
			}
		}
	}
}

// An overlay file named after a bundled family changes only what it states.
// This is the property that makes a one-line override worth writing: a user who
// wants Claude launched through a wrapper should not have to restate its name,
// its aliases, its adapter id and its resume arguments to get one.
func TestOverlayOverridesOnlyTheFieldsItStates(t *testing.T) {
	t.Cleanup(resetForTest)
	dir := t.TempDir()
	writeOverlay(t, dir, "claude.toml", "command = \"claude-next\"\n")
	if problems := LoadOverlay(dir); len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}

	family, ok := Find("claude")
	if !ok {
		t.Fatal("claude is gone")
	}
	if family.Command != "claude-next" {
		t.Errorf("command = %q", family.Command)
	}
	if family.Name != "Claude Code" || family.ConversationAdapterID() != "claude-code" ||
		!slices.Equal(family.ResumeArgs, []string{"--resume"}) ||
		family.SkipPermissionsArg != "--dangerously-skip-permissions" {
		t.Errorf("an override restated nothing and lost something: %+v", family)
	}
	// Order is not a Family field, so an override cannot move a family in the
	// picker. Claude stays first.
	if got := Families()[0].ID; got != "claude" {
		t.Errorf("picker now starts with %q", got)
	}
	argv, err := BuildLaunch("claude", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := "claude-next --dangerously-skip-permissions"; DisplayCommand(argv) != want {
		t.Errorf("launch = %q, want %q", DisplayCommand(argv), want)
	}
}

// A file with an id nothing ships adds a family, and it is a whole family: it
// launches, it resumes, and the picker offers it last unless it says otherwise.
func TestOverlayAddsAFamily(t *testing.T) {
	t.Cleanup(resetForTest)
	dir := t.TempDir()
	writeOverlay(t, dir, "housecat.toml", `
name = "Housecat"
short = "Housecat"
command = "housecat"
skip_permissions_arg = "--pounce"
resume_args = ["--resume"]
resume_kinds = ["id"]
`)
	if problems := LoadOverlay(dir); len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}

	families := Families()
	if got := families[len(families)-1].ID; got != "housecat" {
		t.Fatalf("last family = %q, want housecat added at the end", got)
	}
	if !Known("housecat") {
		t.Fatal("Known(housecat) = false")
	}
	argv, err := BuildResume("housecat", "id", "abc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := DisplayCommand(argv); got != "housecat --resume abc" {
		t.Errorf("resume = %q", got)
	}
	if got := Label("housecat"); got != "Housecat" {
		t.Errorf("Label = %q", got)
	}
	// And it is in the picker, which is the whole point of adding one.
	if !slices.Contains(ResolvePicker(nil, true), "housecat") {
		t.Error("the picker does not offer housecat")
	}
}

// A malformed file is reported and skipped, and nothing else in the directory
// is affected. A personal config file must not be able to stop Sidecar
// starting, and a user who breaks one family's file must not lose the rest.
func TestOverlaySkipsAMalformedFileAndKeepsTheRest(t *testing.T) {
	t.Cleanup(resetForTest)
	dir := t.TempDir()
	writeOverlay(t, dir, "broken.toml", "command = \"nope\nthis is not toml [[[\n")
	writeOverlay(t, dir, "housecat.toml", "name = \"Housecat\"\nshort = \"Housecat\"\ncommand = \"housecat\"\n")
	writeOverlay(t, dir, "notes.txt", "ignored, not a .toml file")

	problems := LoadOverlay(dir)
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want exactly the broken file", problems)
	}
	if !strings.Contains(problems[0].Error(), "broken.toml") {
		t.Errorf("problem does not name the file: %v", problems[0])
	}
	if Known("broken") {
		t.Error("the malformed family was registered anyway")
	}
	if !Known("housecat") {
		t.Error("a malformed sibling took housecat down with it")
	}
	if !Known("claude") {
		t.Error("a malformed overlay file dropped the bundled catalog")
	}
}

func TestOverlayOnAMissingDirectoryIsNotAnError(t *testing.T) {
	t.Cleanup(resetForTest)
	if problems := LoadOverlay(filepath.Join(t.TempDir(), "nothing-here")); len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if problems := LoadOverlay(""); len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if !Known("claude") {
		t.Error("the bundled catalog is gone")
	}
}

// Loading twice is loading once: the catalog is rebuilt from the bundled set
// every time, so a family removed from the directory is removed from the
// catalog rather than surviving as a leftover.
func TestOverlayReloadIsNotCumulative(t *testing.T) {
	t.Cleanup(resetForTest)
	dir := t.TempDir()
	writeOverlay(t, dir, "housecat.toml", "name = \"Housecat\"\nshort = \"Housecat\"\ncommand = \"housecat\"\n")
	LoadOverlay(dir)
	if !Known("housecat") {
		t.Fatal("housecat was not added")
	}
	if err := os.Remove(filepath.Join(dir, "housecat.toml")); err != nil {
		t.Fatal(err)
	}
	LoadOverlay(dir)
	if Known("housecat") {
		t.Error("a removed overlay file left its family behind")
	}
	if len(Families()) != len(build(parseBundled(), nil).launch) {
		t.Error("the catalog did not return to the bundled set")
	}
}

// A user can add a detection-only family too: a file with no command is a
// family Sidecar can name but never offers to start. That is the same rule the
// bundled set follows, stated once, so the overlay cannot smuggle an
// unlaunchable family into a picker.
func TestOverlayFamilyWithNoCommandIsDetectionOnly(t *testing.T) {
	t.Cleanup(resetForTest)
	dir := t.TempDir()
	writeOverlay(t, dir, "watcher.toml", "name = \"Watcher\"\nshort = \"Watcher\"\n")
	LoadOverlay(dir)

	if !DetectionOnly("watcher") {
		t.Error("DetectionOnly(watcher) = false")
	}
	if Known("watcher") {
		t.Error("a family with no command reached the launchable list")
	}
	if slices.Contains(ResolvePicker(nil, true), "watcher") {
		t.Error("the picker offered a family with no command")
	}
}

// The picker offers what is installed. This drives the whole rule through a
// fake PATH rather than the machine's, because a test that passes only on a
// developer's laptop is not a test of the rule.
func TestPickerOffersOnlyInstalledFamilies(t *testing.T) {
	t.Cleanup(resetForTest)
	t.Cleanup(resetInstalledForTest)
	resetForTest()

	present := map[string]bool{"claude": true, "codex": true, "qoder": true}
	primeInstalled(func(command string) (string, error) {
		if present[command] {
			return "/fake/bin/" + command, nil
		}
		return "", exec.ErrNotFound
	})

	got := ResolvePicker(nil, true)
	want := []string{"", "claude", "codex", "qodercli"}
	if !slices.Equal(got, want) {
		t.Fatalf("picker = %v, want %v", got, want)
	}
	// Worktree mode puts None last and applies the same filter.
	if got := ResolvePicker(nil, false); !slices.Equal(got, []string{"claude", "codex", "qodercli", ""}) {
		t.Fatalf("worktree picker = %v", got)
	}
	// Configuration still lists everything: the page answers "what exists".
	if len(Families()) < 20 {
		t.Fatalf("Families() shrank to %d; the filter belongs to the picker, not the catalog", len(Families()))
	}
	if !Installed("claude") || Installed("amp") {
		t.Errorf("Installed disagrees with the fake PATH")
	}
	// Qoder is the case worth pinning: the family id is qodercli and the
	// command is qoder, so a filter that probed the id would hide it.
	if !Installed("qodercli") {
		t.Error("Installed(qodercli) probed the id rather than the command")
	}
}

// A family the user named in plugins.workspace.agents is offered whether or not
// its command is on PATH. Naming it is the stronger signal, and a machine they
// have not installed it on yet is their business.
func TestConfiguredFamiliesAreOfferedEvenWhenNotInstalled(t *testing.T) {
	t.Cleanup(resetForTest)
	t.Cleanup(resetInstalledForTest)
	resetForTest()
	primeInstalled(func(string) (string, error) { return "", exec.ErrNotFound })

	got := ResolvePicker([]string{"amp", "grok"}, false)
	if !slices.Equal(got, []string{"amp", "grok", ""}) {
		t.Fatalf("picker = %v, want the configured families and None", got)
	}
}

// With nothing installed and nothing configured the picker offers everything
// rather than nothing. An empty picker is a dead end, and a machine where no
// catalog command resolves is far more likely to be an unusual PATH than a
// machine with no agents at all.
func TestPickerFallsBackToEveryFamilyWhenNothingIsInstalled(t *testing.T) {
	t.Cleanup(resetForTest)
	t.Cleanup(resetInstalledForTest)
	resetForTest()
	primeInstalled(func(string) (string, error) { return "", exec.ErrNotFound })

	if got, want := len(ResolvePicker(nil, true)), len(Families())+1; got != want {
		t.Fatalf("picker has %d entries, want every family plus None (%d)", got, want)
	}
}

// Before the PATH probe has run, every family is offered. "Not installed" and
// "not asked" are the same zero value and must not be the same decision: a
// picker that opened before the probe landed used to be the whole catalog and
// must stay that way rather than becoming empty.
func TestPickerOffersEverythingBeforeThePathProbeRuns(t *testing.T) {
	t.Cleanup(resetInstalledForTest)
	resetInstalledForTest()
	if InstalledKnown() {
		t.Fatal("InstalledKnown is true with no probe")
	}
	if Installed("claude") {
		t.Fatal("Installed answered yes with no probe")
	}
	if got, want := len(ResolvePicker(nil, false)), len(Families())+1; got != want {
		t.Fatalf("picker has %d entries, want %d", got, want)
	}
}

// The real probe on the real PATH. It asserts nothing about which agents this
// machine has -- that is not a property of the code -- only that asking is
// answerable and that the answer is consistent with what exec.LookPath says.
func TestPrimeInstalledAgreesWithLookPath(t *testing.T) {
	t.Cleanup(resetInstalledForTest)
	PrimeInstalled()
	if !InstalledKnown() {
		t.Fatal("InstalledKnown is false after PrimeInstalled")
	}
	for _, family := range Families() {
		_, err := exec.LookPath(family.Command)
		if got, want := Installed(family.ID), err == nil; got != want {
			t.Errorf("Installed(%s) = %v, LookPath(%s) err = %v", family.ID, got, family.Command, err)
		}
		if err != nil && !errors.Is(err, exec.ErrNotFound) {
			t.Logf("LookPath(%s): %v", family.Command, err)
		}
	}
}
