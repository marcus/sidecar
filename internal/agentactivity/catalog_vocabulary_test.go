package agentactivity

import (
	"slices"
	"sort"
	"testing"

	"github.com/marcus/sidecar/internal/agentactivity/manifests"
	"github.com/marcus/sidecar/internal/agentcatalog"
)

// The two vocabularies used to line up by hand. This makes it structural.
//
// agentcatalog names the providers Sidecar can start; this package names the
// provider it can see running in a pane. They were equal family for family, but
// nothing said so, and the cost of them drifting is no longer cosmetic: since
// td-11040b the process name is the evidence a hook's --kind claim is checked
// against, so a catalog family with no case in identifyProcessName is a family
// whose panes have no identity at all. That degrades rather than breaks — an
// unnamable occupant passes the gate instead of failing it — which is precisely
// why it would go unnoticed without this test.
// Both halves of the catalog are covered. A launchable family is checked
// through its launch command, which is the spelling its own panes report; a
// detection-only family has no command, so it is checked through its id and
// every alias it records. Aliases are not asserted for launchable families
// here, because some of them are conversation adapter ids ("cursor-cli",
// "pi-agent") rather than process spellings; the process spellings are asserted
// against Herdr's own table by TestUpstreamAliasesResolveForClaimedFamilies.
func TestTheProcessNameVocabularyMatchesTheAgentCatalog(t *testing.T) {
	for _, family := range agentcatalog.Families() {
		t.Run(family.ID, func(t *testing.T) {
			if got := identifyProcessName(family.Command); got != family.ID {
				t.Fatalf("identifyProcessName(%q) = %q, want %q.\n"+
					"A catalog family whose launch command this resolver cannot name has panes with no\n"+
					"provider identity, so `agent report-session --kind %s` is never checked against the\n"+
					"pane. Add a case to identifyProcessName in activity.go.",
					family.Command, got, family.ID, family.ID)
			}
		})
	}
	for _, family := range agentcatalog.DetectionFamilies() {
		t.Run("detection/"+family.ID, func(t *testing.T) {
			if family.Command != "" {
				t.Fatalf("detection-only family %q has Command %q; it is not launchable and must not claim to be",
					family.ID, family.Command)
			}
			for _, spelling := range append([]string{family.ID}, family.Aliases...) {
				if got := identifyProcessName(spelling); got != family.ID {
					t.Fatalf("identifyProcessName(%q) = %q, want %q.\n"+
						"A detection-only family this resolver cannot name never becomes an Observation.Agent,\n"+
						"so its vendored manifest is never evaluated and its panes show no badge at all.\n"+
						"Add a case to identifyProcessName in activity.go.",
						spelling, got, family.ID)
				}
			}
		})
	}
}

// aliasGatedFamilies are the ten families whose process gate is the alias table
// rather than a hand-written predicate, spelled here so the tests that cover
// that whole path keep covering it.
//
// They used to be reachable as agentcatalog.DetectionFamilies(), and that
// stopped being true when the catalog moved to TOML and every one of them
// gained a launch command. Reading them from the catalog now would silently
// cover nothing, which is the failure this list exists to prevent: what these
// ten have in common is a gate, not a missing command.
var aliasGatedFamilies = []string{
	"cline", "devin", "droid", "hermes", "kilo",
	"kimi", "kiro", "maki", "qodercli", "qwen",
}

// The two spellings of the alias-gated set, the list above and this package's
// aliasGatedFamily switch, are what Supports and processGate are built on.
// They are separate so that agentactivity's hot path does not import the
// catalog, which makes them exactly the kind of pair that drifts.
func TestAliasGatedSetMatchesTheCatalog(t *testing.T) {
	for _, id := range aliasGatedFamilies {
		if !aliasGatedFamily(id) {
			t.Errorf("%q is alias-gated here and aliasGatedFamily() does not know it; its panes refuse with %s.process-mismatch", id, id)
		}
		if !Supports(id) {
			t.Errorf("Supports(%q) = false; Detect would answer \"unsupported-agent\" for every pane running it", id)
		}
		if _, ok := agentcatalog.Lookup(id); !ok {
			t.Errorf("%q is not a catalog family at all", id)
		}
	}
	gated := map[string]bool{}
	for _, id := range aliasGatedFamilies {
		gated[id] = true
	}
	// Every other launchable family owes the engine a hand-written gate, and
	// commandGate refuses anything it has no case for. A family added to the
	// catalog with neither is a pane that is never evaluated.
	for _, family := range agentcatalog.Families() {
		if gated[family.ID] || Supports(family.ID) {
			continue
		}
		if _, declared := familiesWithNoScreenManifest[family.ID]; !declared {
			t.Errorf("launchable family %q has no hand-written gate, is not alias-gated, and is not declared as having no screen manifest", family.ID)
		}
	}
}

// Herdr's alias table is the source for both the catalog's recorded aliases and
// this package's resolver, so the catalog must not carry a spelling upstream
// does not have or miss one it does.
func TestDetectionOnlyAliasesMatchTheUpstreamTable(t *testing.T) {
	table, err := manifests.LoadAliases()
	if err != nil {
		t.Fatalf("load aliases.upstream.json: %v", err)
	}
	for _, id := range aliasGatedFamilies {
		family, ok := agentcatalog.Lookup(id)
		if !ok {
			t.Errorf("%q is not a catalog family", id)
			continue
		}
		upstream, ok := table.Agents[family.ID]
		if !ok {
			t.Errorf("alias-gated family %q has no entry in the upstream alias table", family.ID)
			continue
		}
		recorded := append([]string{family.ID}, family.Aliases...)
		sort.Strings(recorded)
		want := append([]string(nil), upstream...)
		sort.Strings(want)
		if !slices.Equal(recorded, want) {
			t.Errorf("%s aliases = %v, upstream table says %v", family.ID, recorded, want)
		}
	}
}

// Claude's version-string argv[0] is the one identity this resolver infers from
// a shape rather than a name, so no other family may ever present that shape.
func TestNoOtherCatalogFamilyLaunchesUnderAVersionShapedArgv0(t *testing.T) {
	for _, family := range agentcatalog.Families() {
		if family.ID == "claude" {
			continue
		}
		if claudeVersionArgv0(family.Command) {
			t.Fatalf("catalog family %q launches %q, which the resolver reads as Claude's version argv[0]; "+
				"its reports would be refused as the wrong provider", family.ID, family.Command)
		}
	}
}

func TestOnlyClaudesOwnVersionArgv0ResolvesToAProvider(t *testing.T) {
	// The trailing-space case is deliberate: tmux pads some command fields, and
	// identifyProcessName trims before matching.
	claude := []string{"claude", "2.0.14", "1.0.0", "10.20.30", " 1.2.3 "}
	for _, command := range claude {
		if got := identifyProcessName(command); got != "claude" {
			t.Errorf("identifyProcessName(%q) = %q, want claude", command, got)
		}
	}

	// Everything version-adjacent but not Claude's exact format stays unnamed.
	// Unnamed is the safe answer: VerifyReportedKind passes an unnamable
	// occupant and refuses a differently-named one, so a loose pattern here
	// turns into refused legitimate reports rather than wrong bindings.
	notClaude := []string{
		"v1.2.3",
		"1.2",
		"1.2.3.4",
		"1.2.3-beta",
		"claude 1.2.3",
		"node1.2", // a runtime with a version glued on is not a version
	}
	for _, command := range notClaude {
		if got := identifyProcessName(command); got != "" {
			t.Errorf("identifyProcessName(%q) = %q, want no identity", command, got)
		}
	}
}

// upstreamAliases reads Herdr's `lookup_agent` table out of the vendored
// aliases.upstream.json, restricted to the families Sidecar claims today and
// re-keyed from Herdr's label to Sidecar's family id.
//
// It used to be a hand-copied Go literal. Driving it from the extracted file
// instead is the point of extracting the file: a sync that adds an alias for a
// family Sidecar claims now fails this test on the sync pull request, which is
// the moment somebody can act on it, rather than waiting for a user's pane to
// show no badge.
//
// Phase 4 widened "claims" from ten families to twenty: the ten launchable ones
// and the ten detection-only ones. The two upstream agents still ignored are
// gemini, excluded by Decision 4 because Antigravity replaced it, and omp,
// which upstream ships hooks-only with no screen manifest to execute.
//
// Muse's `muse-bin-<version>` launcher spelling is not in the alias list because
// upstream matches it by shape (`is_muse_versioned_binary`); the extracted
// versioned_binary_prefixes table carries the prefix and one representative
// value stands in for the shape.
func upstreamAliases(t *testing.T) map[string][]string {
	t.Helper()
	table, err := manifests.LoadAliases()
	if err != nil {
		t.Fatalf("load aliases.upstream.json: %v", err)
	}
	out := make(map[string][]string, len(sidecarFamilies()))
	for _, family := range sidecarFamilies() {
		label := HerdrAgentLabel(family)
		aliases, ok := table.Agents[label]
		if !ok {
			t.Fatalf("upstream alias table has no entry for %q (Herdr label %q)", family, label)
		}
		out[family] = append([]string(nil), aliases...)
		if prefix, ok := table.VersionedBinaryPrefixes[label]; ok {
			out[family] = append(out[family], prefix+"0.1.0-R708.1")
		}
	}
	return out
}

// handGatedFamilies are the ten providers with a hand-written process gate.
var handGatedFamilies = []string{
	"claude", "codex", "grok", "antigravity", "pi",
	"copilot", "cursor", "opencode", "amp", "muse",
}

// sidecarFamilies is every family Sidecar has an identity for and a vendored
// screen manifest for: twenty of Herdr's twenty-three agents. The three missing
// are gemini, excluded by Decision 4, and omp and mastracode, which upstream
// ships hooks-only with no screen rules to inherit; those two are catalog
// families with no manifest and are declared in familiesWithNoScreenManifest.
func sidecarFamilies() []string {
	return append(append([]string(nil), handGatedFamilies...), aliasGatedFamilies...)
}

// A pane running an agent under a spelling upstream knows and Sidecar does not
// has no provider identity at all: no state badge, and `agent report-session
// --kind` never checked against it. Herdr's alias table is the shared
// vocabulary, so every entry in it for a family Sidecar claims must resolve —
// for all twenty families since Phase 4, not just the launchable ten.
func TestUpstreamAliasesResolveForClaimedFamilies(t *testing.T) {
	for family, aliases := range upstreamAliases(t) {
		for _, alias := range aliases {
			t.Run(family+"/"+alias, func(t *testing.T) {
				if got := identifyProcessName(alias); got != family {
					t.Fatalf("identifyProcessName(%q) = %q, want %q (upstream alias for %s)",
						alias, got, family, family)
				}
			})
		}
	}
}

// Every family in either half of the catalog must be in the list above, or
// declared as having no screen manifest, so the alias assertion cannot quietly
// stop covering one.
func TestUpstreamAliasTableCoversEveryCatalogFamily(t *testing.T) {
	claimed := make(map[string]bool, len(handGatedFamilies)+len(aliasGatedFamilies))
	for _, family := range sidecarFamilies() {
		claimed[family] = true
	}
	check := func(kind string, families []agentcatalog.Family) {
		for _, family := range families {
			if claimed[family.ID] {
				continue
			}
			if _, declared := familiesWithNoScreenManifest[family.ID]; declared {
				continue
			}
			t.Errorf("%s family %q is in neither handGatedFamilies nor aliasGatedFamilies and is not declared "+
				"in familiesWithNoScreenManifest; add it to one, or its upstream aliases go unasserted", kind, family.ID)
		}
	}
	check("catalog", agentcatalog.Families())
	check("detection-only", agentcatalog.DetectionFamilies())
}

// The two families with no screen manifest still have an upstream alias record,
// and it still has to resolve: a hook report from one of them is checked against
// the pane's process name by VerifyReportedKind, so a spelling this resolver
// cannot name is a legitimate report refused.
func TestUpstreamAliasesResolveForFamiliesWithNoScreenManifest(t *testing.T) {
	table, err := manifests.LoadAliases()
	if err != nil {
		t.Fatalf("load aliases.upstream.json: %v", err)
	}
	for id := range familiesWithNoScreenManifest {
		aliases, ok := table.Agents[id]
		if !ok {
			t.Errorf("%q has no upstream alias entry", id)
			continue
		}
		for _, alias := range aliases {
			if got := identifyProcessName(alias); got != id {
				t.Errorf("identifyProcessName(%q) = %q, want %q", alias, got, id)
			}
		}
		if Supports(id) {
			t.Errorf("Supports(%q) = true, but no manifest is vendored for it; its rows would carry a chip that can never leave unknown", id)
		}
	}
}

// npm and Windows shims present the same program under a wrapper extension,
// and a pane's command can arrive path-qualified. Upstream folds both away
// before its alias lookup; so does this resolver, which is why the table above
// carries only bare names.
func TestLauncherSuffixAndPathSpellingsResolve(t *testing.T) {
	cases := map[string]string{
		"claude.cmd":                     "claude",
		"CLAUDE.EXE":                     "claude",
		"opencode.js":                    "opencode",
		"cursor-agent.cmd":               "cursor",
		"codex.ps1":                      "codex",
		"amp.bat":                        "amp",
		"/opt/homebrew/bin/opencode":     "opencode",
		`C:\Users\a\AppData\claude.cmd`:  "claude",
		"/usr/local/bin/cursor-agent/":   "cursor",
		"/bin/zsh":                       "shell",
		" /Users/a/.local/bin/grok-cli ": "grok",
	}
	for command, want := range cases {
		if got := identifyProcessName(command); got != want {
			t.Errorf("identifyProcessName(%q) = %q, want %q", command, got, want)
		}
	}
}

// The version-shaped argv[0] is matched before suffix stripping so that
// stripping can never manufacture Claude's shape out of something else.
func TestLauncherSuffixStrippingCannotManufactureClaudesVersionArgv0(t *testing.T) {
	for _, command := range []string{"1.2.3.js", "1.2.3.exe", "1.2.3.cmd", "1.2.3.bat", "1.2.3.ps1"} {
		if got := identifyProcessName(command); got != "" {
			t.Errorf("identifyProcessName(%q) = %q, want no identity", command, got)
		}
	}
}

// Herdr treats these as generic runtimes rather than agents; Sidecar must not
// name a provider for any of them either. Sidecar's "shell" bucket is
// deliberately narrower (see identifyProcessName), so the assertion is only
// that none of them resolves to a provider family.
func TestHerdrGenericRuntimesNeverResolveToAProvider(t *testing.T) {
	// src/detect/mod.rs:696 `is_generic_runtime_or_shell` at e2b85c7.
	runtimes := []string{
		"sh", "bash", "zsh", "fish", "tmux", "node", "bun", "cmd",
		"powershell", "pwsh", "python", "python3", "python3.12",
	}
	for _, runtime := range runtimes {
		if got := identifyProcessName(runtime); got != "" && got != "shell" {
			t.Errorf("identifyProcessName(%q) = %q, want no provider identity", runtime, got)
		}
	}
}
