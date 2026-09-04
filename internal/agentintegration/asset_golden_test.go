package agentintegration

import "testing"

// The golden checksum guard.
//
// An asset's version is not decoration. Authority is granted to a *source at a
// version*: the capability registry records which version was qualified against
// recorded traces, `agent explain` demotes a report whose source version is not
// that one, and `integration status` decides current-versus-outdated by
// comparing the installed bytes with the bundled asset's. Changing what an asset
// does without changing its version therefore hands a version's earned authority
// to bytes nobody qualified, and leaves every already-installed copy reading as
// `current` while it is a different program.
//
// Until now that rule lived only in a doc comment above each version constant,
// which is to say it lived in whether the next person read it. This is the same
// rule as a test: nothing about a bundled asset's bytes can change without this
// file changing too, and updating this file is the step where the reader is told
// what else the change obliges them to do.
//
// The table is keyed by provider and asset name rather than by index, so an
// adapter that grows a second asset — as Codex did, when hooks.json alone stopped
// describing what it installs — fails here by name instead of shifting a row.

// assetGolden is one bundled asset's pinned identity.
type assetGolden struct {
	provider string
	name     string
	version  string
	checksum string
}

// assetGoldens is the recorded bytes of every asset this build ships.
//
// Updating an entry here is the last step of a version bump, never the first.
// See the failure message below for the order.
var assetGoldens = []assetGolden{
	{provider: OpenCodeProvider, name: "sidecar-lifecycle.js", version: "1", checksum: "2eed6ae1609e7ef8a6ebc66cd0a942b9176feb7706f49814b7c81f3246b204ab"},
	{provider: CodexProvider, name: "hooks.json", version: "1", checksum: "bed1991b2721f08148a2089600ae09d6328b244317355ea66e16c4b4a8de26d0"},
	{provider: CodexProvider, name: "config.toml", version: "1", checksum: "380d955d0141f00dd10fe9d3e769c7d5e31fec036e356a0583e0f4b91d64615f"},
	{provider: ClaudeProvider, name: "settings.json", version: "1", checksum: "0d2ca7075dff1faab0645c82e8fd5a04f5982e4be27667b9afa19e2ab05e6f3d"},
	// Pi stays at version 1 across a content change, which is the one case the
	// bump order below deliberately allows and is worth recording rather than
	// leaving a reader to wonder. A version exists so an already-installed copy
	// can be recognised as outdated and so authority granted to a qualified
	// version is not inherited by unqualified bytes. Neither applies yet: no
	// release has shipped `agent integration install pi`, so there is no copy of
	// version 1 on any machine to be misread, and the change that moved this
	// checksum -- seeding the report sequence from the clock, plus comments --
	// leaves the event-to-lane mapping identical, so the recorded traces that
	// earned the advisory tier still describe this asset exactly. See the note on
	// PiAssetVersion, which states the same rule at the constant.
	{provider: PiProvider, name: "sidecar-lifecycle.js", version: "1", checksum: "054a5b8b0134f2fc1dc8e3e5bb2047c8611bdd77cc618ef3b05c4c2738477516"},
	{provider: KiloProvider, name: "sidecar-lifecycle.js", version: "1", checksum: "207dce18956f6504138f39eb43f44e0af987ae7836b68e4cbeabad9d28e042ee"},
	// Kimi's asset is not a file: it is the managed block the installer writes
	// into the user's config.toml, rendered from kimiHooks. So this checksum
	// covers the twelve event rows, their matchers, the exact CLI command each
	// one spawns, and the timeout -- which is the whole of what the integration
	// does. A change to any of them lands here, which is the intent: the
	// event-to-lane mapping is the part a tier is granted against.
	{provider: KimiProvider, name: "config.toml", version: "1", checksum: "438867d8dc4b6406dd28bb2829438221f0c96e5b57c7d840819e7a729196bd39"},
	// Antigravity's asset is not a file either: it is the hooks.json Sidecar
	// would write into an empty tree, so the checksum covers the block name,
	// the event, the flat handler shape, the exact command including the
	// printf the provider's stdout contract requires, and the timeout. Those
	// are the whole of what the integration does.
	{provider: AntigravityProvider, name: "hooks.json", version: "1", checksum: "389e513eddf2d16c14124205f6409b1000165021853b9211af764d14def4ac60"},
	// Copilot's checksum is worth more than most, because it is the only
	// record anywhere of an entry shape nobody has run: the `bash` command
	// field and the `timeoutSec` timeout are Herdr's word rather than a
	// released Copilot's, so a change to either is a change to an untested
	// claim and should be noticed.
	{provider: CopilotProvider, name: "settings.json", version: "1", checksum: "48174b6c5fbf7770da52242b8fb198e2383cfdd481498bc4b455f9e55acf5511"},
	// Cursor's asset is the file Sidecar would create in an empty tree, so the
	// checksum covers the `version` header as well as the entry. That header
	// goes only into a file Sidecar creates, never into a user's own, so a
	// change here is a change to what a fresh install looks like.
	{provider: CursorProvider, name: "hooks.json", version: "1", checksum: "c5b43c13c5aa7b5bc368dd815c542b9060f2910c985f57b31376d3dab4296a72"},
}

// bumpInstructions is the whole point of the guard: a failure here has to tell
// the reader what to do, because the wrong response — editing the checksum until
// the test is green — is also the easiest one.
const bumpInstructions = `
An asset's bytes changed. Before updating the golden below, do this in order:

  1. Bump the asset's version constant (OpenCodeAssetVersion, CodexAssetVersion,
     ClaudeAssetVersion, PiAssetVersion, KiloAssetVersion, or KimiAssetVersion) if it has not already moved. An installed copy is
     recognised as outdated by its version, so without this every existing
     install keeps reporting itself current while running different code.
  2. Update the matching AssetVersion in internal/agentlifecycle/capabilities.json,
     or every report the new asset sends resolves at screen fallback for
     claiming a version the registry has never heard of.
  3. Requalify: run the node harnesses over the recorded traces
     (go test ./internal/agentintegration/ with node on PATH), and re-record
     evidence if the change affects which events map to which lane.
  4. Only then update the entry in assetGoldens to the new version and checksum.`

func TestEveryBundledAssetMatchesItsRecordedGolden(t *testing.T) {
	want := map[string]assetGolden{}
	for _, g := range assetGoldens {
		key := g.provider + "/" + g.name
		if _, dup := want[key]; dup {
			t.Fatalf("assetGoldens records %s twice", key)
		}
		want[key] = g
	}

	shipped := map[string]bool{}
	for _, adapter := range DefaultAdapters() {
		for _, asset := range adapter.Assets() {
			key := adapter.Provider() + "/" + asset.Name
			shipped[key] = true

			g, ok := want[key]
			if !ok {
				t.Errorf("%s ships an asset with no recorded golden.\n"+
					"Add one: {provider: %q, name: %q, version: %q, checksum: %q}",
					key, adapter.Provider(), asset.Name, asset.Version, asset.Checksum())
				continue
			}
			if asset.Version != g.version {
				t.Errorf("%s is at version %q, the golden records %q.%s",
					key, asset.Version, g.version, bumpInstructions)
			}
			if got := asset.Checksum(); got != g.checksum {
				t.Errorf("%s content checksum is %s, the golden records %s.%s",
					key, got, g.checksum, bumpInstructions)
			}
		}
	}

	for key := range want {
		if !shipped[key] {
			t.Errorf("assetGoldens records %s but no adapter ships it; remove the entry, or the guard is watching a file that no longer exists", key)
		}
	}
}
