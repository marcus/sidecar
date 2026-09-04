package agentcatalog

import (
	"strings"
	"testing"
)

// The load-bearing property of a detection-only family is that no surface which
// offers a user a choice can reach it. Getting this wrong shows up as an agent
// in a creation picker that cannot be launched, so every accessor that answers
// "what can Sidecar start" is asserted here rather than assumed from the fact
// that the two lists are separate variables.
func TestDetectionOnlyFamiliesAreNeverOfferedAsAChoice(t *testing.T) {
	for _, family := range DetectionFamilies() {
		t.Run(family.ID, func(t *testing.T) {
			for _, offered := range Families() {
				if offered.ID == family.ID {
					t.Fatalf("%s is in Families(), which is the creation picker's order", family.ID)
				}
			}
			if _, ok := Find(family.ID); ok {
				t.Fatalf("Find(%q) resolved; configuration pages would list it as selectable", family.ID)
			}
			if Known(family.ID) {
				t.Fatalf("Known(%q) is true; it is not a family Sidecar can start", family.ID)
			}
			if _, ok := FindLaunch(family.ID); ok {
				t.Fatalf("FindLaunch(%q) resolved; an execution boundary would try to start it", family.ID)
			}
			if _, ok := Lookup(family.ID); ok {
				t.Fatalf("Lookup(%q) resolved; resume and session binding would treat it as launchable", family.ID)
			}
			if _, err := BuildLaunch(family.ID, nil, false); err == nil {
				t.Fatalf("BuildLaunch(%q) built a command for a family with no Command", family.ID)
			}
			if family.CanResume() {
				t.Fatalf("%s claims a native resume", family.ID)
			}
			if _, err := BuildResume(family.ID, "id", "abc", nil); err == nil {
				t.Fatalf("BuildResume(%q) built a resume", family.ID)
			}
			// Resolve honours an allowlist, and a user can write anything into
			// one. An unknown entry is dropped, and a detection-only id must be
			// dropped the same way rather than smuggled into the picker through
			// configuration.
			if got := Resolve([]string{family.ID}); len(got) != len(Families()) {
				t.Fatalf("Resolve([%q]) = %d families; an allowlist naming nothing selectable must fall back to everything", family.ID, len(got))
			}
			for _, id := range ResolvePicker([]string{family.ID, "claude"}, true) {
				if id == family.ID {
					t.Fatalf("ResolvePicker offered %q", family.ID)
				}
			}
		})
	}
}

// The fields a detection-only family does carry, and the ones it must not.
// Every field left empty here is a cost the family is not paying: a command it
// cannot be launched with, a resume nothing implements, an adapter that would
// have to exist, and a skip-permissions flag that would be a guess.
func TestDetectionOnlyFamiliesCarryOnlyIdentity(t *testing.T) {
	seen := map[string]bool{}
	for _, family := range DetectionFamilies() {
		if strings.TrimSpace(family.ID) == "" || strings.TrimSpace(family.Name) == "" || strings.TrimSpace(family.Short) == "" {
			t.Errorf("%+v is missing an id, a name, or a short label", family)
		}
		if seen[family.ID] {
			t.Errorf("duplicate detection-only family %q", family.ID)
		}
		seen[family.ID] = true
		if family.Command != "" || family.SkipPermissionsArg != "" || family.AdapterID != "" ||
			len(family.ResumeArgs) != 0 || len(family.ResumeKinds) != 0 {
			t.Errorf("%s carries launch, resume or adapter fields: %+v", family.ID, family)
		}
		if _, ok := Find(family.ID); ok {
			t.Errorf("%s is in both halves of the catalog", family.ID)
		}
		for _, alias := range family.Aliases {
			if alias == family.ID {
				t.Errorf("%s repeats its own id in Aliases", family.ID)
			}
		}
	}
	// ConversationAdapterID falls back to ID, which is correct for a family with
	// no adapter only because nothing looks one up for these. Stated so the
	// fallback is not mistaken for a claim that an adapter exists.
	for _, family := range DetectionFamilies() {
		if got := family.ConversationAdapterID(); got != family.ID {
			t.Errorf("%s ConversationAdapterID = %q", family.ID, got)
		}
	}
}

func TestDetectionOnlyReportsExactlyTheSecondList(t *testing.T) {
	for _, family := range DetectionFamilies() {
		if !DetectionOnly(family.ID) {
			t.Errorf("DetectionOnly(%q) = false", family.ID)
		}
		if _, ok := FindDetection(family.ID); !ok {
			t.Errorf("FindDetection(%q) = false", family.ID)
		}
	}
	for _, family := range Families() {
		if DetectionOnly(family.ID) {
			t.Errorf("DetectionOnly(%q) = true for a launchable family", family.ID)
		}
	}
	for _, id := range []string{"", "  ", "aider", "gemini", "omp", "qoder", "unknown"} {
		if DetectionOnly(id) {
			t.Errorf("DetectionOnly(%q) = true", id)
		}
	}
	// The id is Herdr's label, not the prettier product name: "qoder" is one of
	// that agent's process spellings and must not resolve as the family.
	if _, ok := FindDetection("qoder"); ok {
		t.Fatal("FindDetection(\"qoder\") resolved; the family id is qodercli")
	}
}

// Sidecar ships no detection-only family today, so this drives the accessor
// through an overlay rather than through the bundled set. The property is the
// same one every accessor here owes: a caller cannot reach into catalog storage
// by mutating what it was handed.
func TestDetectionFamiliesReturnsACopy(t *testing.T) {
	dir := t.TempDir()
	writeOverlay(t, dir, "watcher.toml", "name = \"Watcher\"\nshort = \"Watcher\"\n")
	if problems := LoadOverlay(dir); len(problems) != 0 {
		t.Fatalf("overlay problems: %v", problems)
	}
	t.Cleanup(resetForTest)

	first := DetectionFamilies()
	if len(first) != 1 || first[0].ID != "watcher" {
		t.Fatalf("detection families = %+v", first)
	}
	first[0].Name = "mutated"
	if DetectionFamilies()[0].Name == "mutated" {
		t.Fatal("DetectionFamilies aliased catalog storage")
	}
}

// Label and ShortLabel are the only two id-to-name mappings in the codebase, and
// they answer for both lists. They deliberately do not follow the rule the test
// above pins: naming a family is not offering it as a choice, and a caller
// holding an id read off a *pane* has the same question a picker has.
//
// This is the gap the first pass left. Ten families were registered with a Name
// and a Short that nothing read, because Label knew only the launchable list and
// fell through to the raw id, so a Qoder pane was labelled `qodercli` -- the name
// of a manifest file. Anything that renames a family here without teaching these
// two now fails.
func TestLabelAndShortLabelNameBothHalvesOfTheCatalog(t *testing.T) {
	for _, family := range append(Families(), DetectionFamilies()...) {
		if got := Label(family.ID); got != family.Name {
			t.Errorf("Label(%q) = %q, want %q", family.ID, got, family.Name)
		}
		if got := ShortLabel(family.ID); got != family.Short {
			t.Errorf("ShortLabel(%q) = %q, want %q", family.ID, got, family.Short)
		}
	}
	// The two answers that are not a family, and the pass-through that lets a
	// provider this catalog has never heard of still render as itself.
	for id, want := range map[string]string{"": "None (attach only)", "shell": "Project Shell", "warp": "warp"} {
		if got := Label(id); got != want {
			t.Errorf("Label(%q) = %q, want %q", id, got, want)
		}
	}
	if got := ShortLabel("warp"); got != "warp" {
		t.Errorf("ShortLabel(warp) = %q, want the id back", got)
	}
}

// The agent chip lowercases whatever it is given, so ShortLabel lowercased is
// the token every workspace row and Sessions card renders. For the launchable
// ten that token has always been the id, and styles.AgentLabel now gets there
// through this function instead of through the id itself. That only stays true
// while Short lowercased *is* the id, which is a property of the data rather
// than a rule anything enforces -- so it is enforced here, in the package that
// owns the data, rather than discovered when a chip silently renames itself.
func TestLaunchableShortNamesLowercaseToTheirIDs(t *testing.T) {
	for _, family := range Families() {
		got := strings.ToLower(family.Short)
		if got == family.ID || got == family.Command {
			continue
		}
		t.Errorf("family %q launches %q and has Short %q, which lowercases to %q; the agent chip renders "+
			"that token, so this renames the chip on every row showing this provider. Keep Short a lowercase "+
			"spelling of either the id or the command, or decide deliberately that the chip changes.",
			family.ID, family.Command, family.Short, got)
	}
	// Qoder is the one family where the two answers differ, and the chip takes
	// the command. Its id is `qodercli` because that is Herdr's label and the
	// name of the vendored manifest file; the program a user installs and types
	// is `qoder`, so a chip reading `qodercli` would name a manifest rather than
	// a provider. Pinned here so the exception stays a decision.
	if got := strings.ToLower(ShortLabel("qodercli")); got != "qoder" {
		t.Errorf("qodercli chip token = %q, want qoder", got)
	}
}
