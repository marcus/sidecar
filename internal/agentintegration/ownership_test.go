package agentintegration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The adapter interface has to describe two structurally different provider
// APIs without either one being the special case. These tests pin the parts of
// that contract a future adapter could quietly break.

// TestEveryAdapterDeclaresWhatItOwnsAtEveryPathItTouches is the interface's
// central invariant. Ownership used to be one boolean with two unwritten
// meanings, decided by whichever adapter happened to set it; anything reading a
// FileState had to know which provider it came from to know what it had been
// told. Declaring it per asset is what makes the surfaces provider-agnostic,
// so an adapter that ships an asset without saying which shape it is has
// reintroduced exactly the ambiguity this replaced.
func TestEveryAdapterDeclaresWhatItOwnsAtEveryPathItTouches(t *testing.T) {
	for _, a := range DefaultAdapters() {
		assets := a.Assets()
		if len(assets) == 0 {
			t.Fatalf("%s ships no assets, so nothing describes what installing it does", a.Provider())
		}
		for _, as := range assets {
			switch as.Ownership {
			case OwnsFile, OwnsEntry:
			default:
				t.Fatalf("%s asset %q declares ownership %q, which is neither shape", a.Provider(), as.Name, as.Ownership)
			}
			if as.Source != a.Source() {
				t.Fatalf("%s asset %q carries source %q, not the adapter's %q", a.Provider(), as.Name, as.Source, a.Source())
			}
			if as.Version == "" {
				t.Fatalf("%s asset %q has no version, so an installed copy can never be called outdated", a.Provider(), as.Name)
			}
			if as.Content == "" {
				t.Fatalf("%s asset %q has no content, so no surface can show what installing it adds", a.Provider(), as.Name)
			}
		}
	}
}

// TestEveryAssetIsAtAPathTheAdapterAlsoReportsOn is what keeps plurality
// honest. Codex edits two files, and while an asset was singular the second one
// was simply undescribed: Assets() answered "hooks.json" and said nothing about
// the config.toml feature flag and trust record without which hooks.json does
// nothing at all. An adapter that grows a third file must grow a third asset,
// and an asset that names a file the adapter never inspects is a description of
// something that does not happen.
func TestEveryAssetIsAtAPathTheAdapterAlsoReportsOn(t *testing.T) {
	env := testEnv(t)
	for _, a := range DefaultAdapters() {
		paths := a.Inspect(env).TargetPaths
		for _, as := range a.Assets() {
			found := false
			for _, p := range paths {
				if strings.HasSuffix(p, "/"+as.Name) {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s declares asset %q but reports target paths %v, none of which is it",
					a.Provider(), as.Name, paths)
			}
		}
	}
}

// TestCodexDescribesBothOfTheFilesItEdits is the concrete case the interface
// change exists for, named rather than left to the general rule above. Codex's
// hooks.json is inert without the config.toml feature flag and trust record,
// so an integration that described only the first was describing the half that
// does not work on its own.
func TestCodexDescribesBothOfTheFilesItEdits(t *testing.T) {
	assets := (CodexAdapter{}).Assets()
	if len(assets) != 2 {
		t.Fatalf("codex ships %d assets, want one per file it edits", len(assets))
	}
	names := []string{assets[0].Name, assets[1].Name}
	if names[0] != "hooks.json" || names[1] != "config.toml" {
		t.Fatalf("codex assets are %v, want hooks.json and config.toml", names)
	}
	if !strings.Contains(assets[1].Content, "hooks = true") {
		t.Fatal("the config.toml asset does not show the feature flag, which is the whole reason it exists")
	}
	if !strings.Contains(assets[1].Content, "trusted_hash") {
		t.Fatal("the config.toml asset does not show the trust record Codex refuses to run without")
	}
}

// TestTheMarkerRuleIsNeverRunAgainstAFileTheUserOwns pins the reason the
// ownership decision moved into the asset. Scanning a user's settings.json or
// config.toml for a `// sidecar-integration:` comment could never succeed --
// neither format has that comment syntax -- so an entry adapter always got back
// a FileState claiming it did not own a file it demonstrably had an entry in,
// and then corrected it afterwards. Running the wrong rule and fixing up after
// is the thing that made "owned" mean two things.
//
// The direction that matters is the unsafe one: a marker comment is bytes any
// process can write into a file, so if the marker rule still ran for OwnsEntry
// then pasting one line into a user's settings.json would hand Sidecar the
// whole file -- and uninstall deletes what it owns.
func TestTheMarkerRuleIsNeverRunAgainstAFileTheUserOwns(t *testing.T) {
	env := testEnv(t)
	path := env.Home + "/lookalike.json"
	writeFileT(t, path, "// sidecar-integration: id="+ClaudeSource+" schema=1 version=1\n{}\n")

	entry := (ClaudeAdapter{}).settingsAsset()
	got := inspectFile(env, path, entry)
	if got.Unsafe != "" {
		t.Fatalf("fixture unusable: %s (%s)", got.Unsafe, got.UnsafeDetail)
	}
	if got.Owned {
		t.Fatal("an OwnsEntry asset claimed a whole user-owned file because its bytes carried a marker comment")
	}
	if got.Ownership != OwnsEntry {
		t.Fatalf("inspection did not record the ownership shape it was asked about, got %q", got.Ownership)
	}

	// The same bytes under an OwnsFile asset of the same source are owned,
	// which is what proves the difference is the declared shape rather than
	// anything about the content.
	file := entry
	file.Ownership = OwnsFile
	if got := inspectFile(env, path, file); !got.Owned {
		t.Fatal("an OwnsFile asset did not recognise its own marker")
	}
}

// TestAnOwnedEntryFileCarriesTheShapeASurfaceNeeds is the data half of the
// rendering fix. Codex's config.toml is owned in the entry sense and has no
// version of its own, which rendered as a dangling "sidecar-owned version "
// with nothing after it and told the user Sidecar owned a configuration file
// full of their own settings. The rendering itself is pinned in internal/cli.
func TestAnOwnedEntryFileCarriesTheShapeASurfaceNeeds(t *testing.T) {
	env := testEnv(t)
	writeFileT(t, env.Home+"/.codex/hooks.json", "{}\n")
	writeFileT(t, env.Home+"/.codex/config.toml", "[features]\nhooks = true\n")

	for _, f := range (CodexAdapter{}).Inspect(env).Files {
		if f.Owned && f.Ownership == "" {
			t.Fatalf("%s is owned but does not say in which sense, so no surface can describe it honestly", f.Path)
		}
	}
}

// TestAnAssetThatDeclaresNoOwnershipIsNotSidecarsFile pins the safety direction
// of the marker rule's default.
//
// The rule ran for anything that was not explicitly OwnsEntry, so an asset whose
// Ownership field was never set — a new adapter, a half-finished declaration, a
// struct built by a caller that did not know about the field — fell through into
// the marker check. Since a marker is just a comment line, that made a malformed
// declaration one paste away from Sidecar concluding it owned a user's file, and
// uninstall deletes what it owns. Unknown ownership must mean "not Sidecar's".
func TestAnAssetThatDeclaresNoOwnershipIsNotSidecarsFile(t *testing.T) {
	env := testEnv(t)
	path := env.Home + "/undeclared.json"

	undeclared := (ClaudeAdapter{}).settingsAsset()
	undeclared.Ownership = "" // never declared
	writeFileT(t, path, fmt.Sprintf("// sidecar-integration: id=%s schema=%d version=1\n{}\n",
		undeclared.Source, undeclared.SchemaVersion))

	got := inspectFile(env, path, undeclared)
	if got.Unsafe != "" {
		t.Fatalf("fixture unusable: %s (%s)", got.Unsafe, got.UnsafeDetail)
	}
	if got.Owned {
		t.Fatal("an asset that declared no ownership claimed a file because its bytes carried a marker")
	}
	if got.Version != "" {
		t.Fatalf("an unowned file was given a version %q", got.Version)
	}
}

// TestAPreviewsAfterStateMatchesWhatTheOpActuallyDoes is the rendering half of
// the ownership work, and it covers the direction the previous tests did not.
//
// Every entry-file op shared one after-state that said "Sidecar's entry is in
// this file", including the uninstall op whose whole purpose is to take it out.
// The op list was correct, so applying always did the right thing and no test
// noticed; only the dry run lied — to the user who chose to read the preview
// instead of trusting the code, which is the worst audience to lie to.
func TestAPreviewsAfterStateMatchesWhatTheOpActuallyDoes(t *testing.T) {
	// afterFor returns the after-state the plan predicts for one path.
	afterFor := func(t *testing.T, p Plan, path string) (FileState, bool) {
		t.Helper()
		for _, op := range p.Ops {
			if op.Kind == OpWrite && op.Path == path {
				return op.After, true
			}
		}
		return FileState{}, false
	}

	t.Run("claude settings.json", func(t *testing.T) {
		svc, _, paths := claudeFixture(t)
		writeFileForTest(t, paths.Settings, `{"model": "opus"}`)

		install, err := svc.Plan(ClaudeProvider, ActionInstall)
		if err != nil {
			t.Fatal(err)
		}
		after, ok := afterFor(t, install, paths.Settings)
		if !ok {
			t.Fatal("install planned no write to settings.json")
		}
		if !after.Owned || after.Ownership != OwnsEntry || after.Version != ClaudeAssetVersion {
			t.Fatalf("install predicted %+v; wanted an owned entry at version %s", after, ClaudeAssetVersion)
		}

		applyTo(t, svc, ClaudeProvider, ActionInstall)
		uninstall, err := svc.Plan(ClaudeProvider, ActionUninstall)
		if err != nil {
			t.Fatal(err)
		}
		after, ok = afterFor(t, uninstall, paths.Settings)
		if !ok {
			t.Fatal("uninstall planned no write to settings.json")
		}
		if after.Owned || after.Ownership == OwnsEntry {
			t.Fatalf("uninstall predicted %+v; the op removes Sidecar's entry, so the file it leaves is the user's", after)
		}
		if after.Version != "" {
			t.Fatalf("uninstall predicted a Sidecar version %q on a file it just cleaned", after.Version)
		}
	})

	t.Run("codex hooks.json and config.toml", func(t *testing.T) {
		svc, _, paths := codexFixture(t)
		writeFileForTest(t, paths.Config, "[features]\nhooks = true\n")

		install, err := svc.Plan(CodexProvider, ActionInstall)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{paths.Hooks, paths.Config} {
			after, ok := afterFor(t, install, path)
			if !ok {
				t.Fatalf("install planned no write to %s", path)
			}
			if !after.Owned || after.Ownership != OwnsEntry || after.Version != CodexAssetVersion {
				t.Fatalf("install predicted %+v for %s; wanted an owned entry", after, path)
			}
		}

		applyTo(t, svc, CodexProvider, ActionInstall)
		uninstall, err := svc.Plan(CodexProvider, ActionUninstall)
		if err != nil {
			t.Fatal(err)
		}
		// config.toml is the case the reviewer named: it is the user's file, it
		// survives uninstall, and the op that strips Sidecar's trust records
		// must not describe the result as containing them.
		after, ok := afterFor(t, uninstall, paths.Config)
		if !ok {
			t.Fatal("uninstall planned no write to config.toml")
		}
		if after.Owned || after.Ownership == OwnsEntry {
			t.Fatalf("uninstall predicted %+v for config.toml; it removes Sidecar's trust records", after)
		}
		if after.Version != "" {
			t.Fatalf("uninstall predicted a Sidecar version %q on the user's config.toml", after.Version)
		}
	})

	// Kimi is the third OwnsEntry adapter and the first whose entry is a fenced
	// region of a TOML file rather than a key in a document, so the preview it
	// renders is the one place that shape could describe itself wrongly. The
	// user's own hook is here so uninstall leaves a file to write rather than
	// removing one: an OpRemove has no after-state to get wrong, and the case
	// this test exists for is the write that ends with Sidecar's block gone.
	t.Run("kimi config.toml", func(t *testing.T) {
		svc, _, paths := kimiFixture(t)
		kimiSetUp(t, paths)
		writeFileForTest(t, paths.Config,
			"default_model = \"moonshot\"\n\n[[hooks]]\nevent = \"Notification\"\ncommand = \"echo keep\"\ntimeout = 3\n")

		install, err := svc.Plan(KimiProvider, ActionInstall)
		if err != nil {
			t.Fatal(err)
		}
		after, ok := afterFor(t, install, paths.Config)
		if !ok {
			t.Fatal("install planned no write to config.toml")
		}
		if !after.Owned || after.Ownership != OwnsEntry || after.Version != KimiAssetVersion {
			t.Fatalf("install predicted %+v; wanted an owned entry at version %s", after, KimiAssetVersion)
		}

		applyTo(t, svc, KimiProvider, ActionInstall)
		uninstall, err := svc.Plan(KimiProvider, ActionUninstall)
		if err != nil {
			t.Fatal(err)
		}
		after, ok = afterFor(t, uninstall, paths.Config)
		if !ok {
			t.Fatal("uninstall planned no write to config.toml")
		}
		if after.Owned || after.Ownership == OwnsEntry {
			t.Fatalf("uninstall predicted %+v for config.toml; it removes Sidecar's block, so what it leaves is the user's file", after)
		}
		if after.Version != "" {
			t.Fatalf("uninstall predicted a Sidecar version %q on the user's config.toml", after.Version)
		}
	})

	// The session-identity entry adapters share one implementation, so the
	// providers come from the registry rather than from a list here: an
	// integration registered without its preview being checked is not possible.
	for _, provider := range SessionEntryProviders() {
		t.Run(provider+" session-identity entry", func(t *testing.T) {
			svc, _, paths := sessionEntryFixture(t, provider)
			writeFileForTest(t, paths.Settings, `{"theme": "dark"}`)

			install, err := svc.Plan(provider, ActionInstall)
			if err != nil {
				t.Fatal(err)
			}
			after, ok := afterFor(t, install, paths.Settings)
			if !ok {
				t.Fatalf("install planned no write to %s", paths.Settings)
			}
			spec, _ := sessionEntrySpecFor(provider)
			if !after.Owned || after.Ownership != OwnsEntry || after.Version != spec.version {
				t.Fatalf("install predicted %+v; wanted an owned entry at version %s", after, spec.version)
			}

			applyTo(t, svc, provider, ActionInstall)
			uninstall, err := svc.Plan(provider, ActionUninstall)
			if err != nil {
				t.Fatal(err)
			}
			after, ok = afterFor(t, uninstall, paths.Settings)
			if !ok {
				t.Fatalf("uninstall planned no write to %s", paths.Settings)
			}
			if after.Owned || after.Ownership == OwnsEntry {
				t.Fatalf("uninstall predicted %+v; the op removes Sidecar's entries, so the file it leaves is the user's", after)
			}
			if after.Version != "" {
				t.Fatalf("uninstall predicted a Sidecar version %q on the user's file", after.Version)
			}
		})
	}
}

// TestASymlinkedSessionIdentitySettingsFileIsRefusedUnwritten pins the Lstat
// rule for the entry adapters, the way the Claude and Codex suites pin it for
// theirs.
//
// A settings file that is a symlink is refused rather than followed, because
// following one writes through to wherever it points -- which is how an
// installer that meant to edit ~/.qwen/settings.json edits something else
// entirely. The check lives in the shared inspection, so proving it once per
// registered provider is what stops a future adapter from opting out of it by
// accident.
func TestASymlinkedSessionIdentitySettingsFileIsRefusedUnwritten(t *testing.T) {
	for _, provider := range SessionEntryProviders() {
		t.Run(provider, func(t *testing.T) {
			svc, _, paths := sessionEntryFixture(t, provider)
			target := filepath.Join(t.TempDir(), "elsewhere.json")
			writeFileForTest(t, target, `{"theme": "dark"}`)
			if err := os.Symlink(target, paths.Settings); err != nil {
				t.Skipf("symlinks are unavailable here: %v", err)
			}

			if _, err := svc.Plan(provider, ActionInstall); err == nil {
				t.Fatal("install was planned through a symlink; it would write to whatever the link points at")
			}
			if got := readFileForTest(t, target); got != `{"theme": "dark"}` {
				t.Fatalf("the symlink's target was written after all: %q", got)
			}
		})
	}
}

// sessionEntryFixture builds one session-identity adapter's world inside
// t.TempDir: its configuration directory exists, its CLI resolves on PATH, and
// nothing outside the temporary tree is reachable. It is driven off the spec so
// it works for every registered provider, including the two whose directory
// comes from an override and the one whose directory hangs off $XDG_CONFIG_HOME.
func sessionEntryFixture(t *testing.T, provider string) (Service, Env, sessionEntryPaths) {
	t.Helper()
	spec, ok := sessionEntrySpecFor(provider)
	if !ok {
		t.Fatalf("%s is not a session-identity entry provider", provider)
	}
	home := t.TempDir()
	env := Env{
		Home:       home,
		ConfigHome: filepath.Join(home, ".config"),
		LookPath: func(file string) (string, error) {
			if file == spec.command {
				return filepath.Join(home, "bin", spec.command), nil
			}
			return "", errors.New("not found")
		},
		ProviderVersion: func(string) string { return "0.0.0" },
		UID:             os.Getuid(),
	}
	paths := spec.pathsFor(env)
	if paths.Dir == "" {
		t.Fatalf("%s resolved no configuration directory from the fixture Env", provider)
	}
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return Service{Env: env, Adapters: DefaultAdapters()}, env, paths
}

func testEnv(t *testing.T) Env {
	t.Helper()
	return Env{Home: t.TempDir(), UID: os.Getuid()}
}

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(path[:strings.LastIndex(path, "/")], 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
