package agentactivity

import (
	"sort"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentactivity/manifests"
	"github.com/marcus/sidecar/internal/agentcatalog"
)

// unregisteredManifests are the vendored Herdr manifests Sidecar deliberately
// has no family for. Each entry carries the reason, because "we have not got to
// it yet" and "we decided not to" are different facts and only the second one
// should survive a sync without being re-argued.
// familiesWithNoScreenManifest are catalog families Herdr ships hooks-only,
// with no screen rules for Sidecar to inherit. They are launchable and they
// have an identity in identifyProcessName -- a hook report has to be checked
// against the pane's process name -- but Supports is false for them, so no row
// carries a provider chip whose state could only ever be unknown.
var familiesWithNoScreenManifest = map[string]string{
	"omp":        "Herdr gives OMP lifecycle authority through hooks (integration v9) and ships no screen manifest at all.",
	"mastracode": "Herdr ships a Mastra Code hooks integration (v2) and no screen manifest.",
}

var unregisteredManifests = map[string]string{
	"gemini": "Decision 4: Antigravity replaced it and `agy` is already a full family. " +
		"The manifest stays vendored because the sync mirrors the whole catalog, so registering it later is one alias line.",
}

// The vendored manifest set and the registered families are one relationship
// held in two files, and a sync moves the first without touching the second.
// This is what makes an addition upstream a decision here rather than a manifest
// nobody notices: a new agent in Herdr's catalog fails this test until someone
// either registers it or records why not.
//
// It is also the exit gate of Phase 4, stated as a test. Herdr vendors 21 screen
// manifests; Sidecar registers 20 of them and declines exactly one. The two
// catalog families with no manifest at all are held out here and asserted
// separately, because this test is about the manifest set rather than the
// catalog: folding them in would make "every family has a manifest" untrue and
// the count arithmetic below meaningless.
func TestEveryVendoredManifestIsRegisteredOrDeclaredUnregistered(t *testing.T) {
	lock, err := manifests.LoadLock()
	if err != nil {
		t.Fatalf("load upstream.lock.json: %v", err)
	}

	registered := map[string]string{}
	for _, family := range agentcatalog.Families() {
		if _, hooksOnly := familiesWithNoScreenManifest[family.ID]; hooksOnly {
			continue
		}
		registered[HerdrAgentLabel(family.ID)] = "launchable"
	}
	for _, family := range agentcatalog.DetectionFamilies() {
		registered[HerdrAgentLabel(family.ID)] = "detection-only"
	}

	vendored := map[string]bool{}
	for _, agent := range lock.Agents {
		vendored[agent.ID] = true
		kind, ok := registered[agent.ID]
		if ok {
			if !Supports(sidecarFamilyForLabel(t, agent.ID)) {
				t.Errorf("%s manifest is vendored and registered as %s, but Supports says no", agent.ID, kind)
			}
			continue
		}
		if _, declared := unregisteredManifests[agent.ID]; !declared {
			t.Errorf("vendored manifest %q has no Sidecar family and no entry in unregisteredManifests.\n"+
				"A synced-in agent is a decision: register it in agentcatalog (detection-only is one line and\n"+
				"costs no theme work), or record here why Sidecar declines it.", agent.ID)
		}
	}

	for label, kind := range registered {
		if !vendored[label] {
			t.Errorf("family %q (%s) has no vendored manifest; its panes would report %s.manifest-unavailable", label, kind, label)
		}
	}
	for id := range unregisteredManifests {
		if !vendored[id] {
			t.Errorf("unregisteredManifests names %q, which is no longer vendored; drop the entry", id)
		}
	}

	if got, want := len(lock.Agents), len(registered)+len(unregisteredManifests); got != want {
		t.Errorf("lock pins %d manifests; %d registered plus %d declared unregistered = %d", got, len(registered), len(unregisteredManifests), want)
	}
}

// sidecarFamilyForLabel inverts HerdrAgentLabel over both halves of the catalog.
func sidecarFamilyForLabel(t *testing.T, label string) string {
	t.Helper()
	for _, family := range append(agentcatalog.Families(), agentcatalog.DetectionFamilies()...) { //nolint:gocritic // one combined pass over both halves
		if HerdrAgentLabel(family.ID) == label {
			return family.ID
		}
	}
	t.Fatalf("no Sidecar family has Herdr label %q", label)
	return ""
}

// The whole of a detection-only family's live path, end to end: the process name
// resolves to the family, the family is supported, the gate admits it, and the
// vendored manifest produces a verdict. Journey 4 of the plan is this test.
func TestDetectionOnlyFamiliesClassifyThroughTheirVendoredManifest(t *testing.T) {
	for _, id := range aliasGatedFamilies {
		family, _ := agentcatalog.Lookup(id)
		t.Run(family.ID, func(t *testing.T) {
			if got := Identify(Observation{CurrentCommand: family.ID}); got != family.ID {
				t.Fatalf("Identify(%q) = %q", family.ID, got)
			}
			ob := Observation{Agent: family.ID, CurrentCommand: family.ID, Screen: "\n"}
			result, explain := DetectManifest(ob)
			if explain == nil {
				t.Fatalf("no manifest evaluated: %s", result.Evidence)
			}
			if len(explain.EvaluatedRules) == 0 {
				t.Fatalf("%s.toml evaluated no rules", family.ID)
			}
			// A blank screen matches nothing, so every one of them lands on the
			// conservative fallback rather than on an invented verdict.
			if result.State != StateIdle || result.Evidence != family.ID+".known-live-fallback" || !result.FallbackIdle {
				t.Fatalf("blank screen got %+v, want the low-evidence idle fallback", result)
			}
		})
	}
}

// The gate is the only refusal a detection-only family has, so it has to be a
// real one: a pane running a shell, or running another agent, must not have this
// family's manifest evaluated against it.
func TestDetectionOnlyFamiliesRefuseAPaneRunningSomethingElse(t *testing.T) {
	for _, id := range aliasGatedFamilies {
		for _, command := range []string{"zsh", "node", "claude", ""} {
			got := Detect(Observation{Agent: id, CurrentCommand: command, Screen: "⠋ Working\n"})
			if got.State != StateUnknown || got.Evidence != id+".process-mismatch" {
				t.Errorf("Detect(%s on %q) = %+v, want %s.process-mismatch", id, command, got, id)
			}
		}
	}
}

// The gate reads two identity inputs and they have to be checked against each
// other, because Identify prefers the one processGate used not to read. On a
// pane whose pane_current_command is a shared runtime and whose foreground
// argv[0] basename names the agent, Identify answers from ob.ProcessIdentity, so
// Observation.Agent is the family while CurrentCommand is still `node`. A gate
// reading only the command refused that pane, and the refusal is worse than not
// claiming it at all: Supports is true, so the row carries a provider chip whose
// state can only ever be unknown, with no idle fallback behind it. Nothing
// exercised the two inputs together, which is why that shipped.
func TestDetectionOnlyGateAcceptsEitherIdentityInput(t *testing.T) {
	for _, id := range aliasGatedFamilies {
		for _, command := range []string{"node", "bun", "agent"} {
			t.Run(id+"/"+command, func(t *testing.T) {
				ob := Observation{CurrentCommand: command, ProcessIdentity: id, Screen: "\n"}
				if got := Identify(ob); got != id {
					t.Fatalf("Identify(argv0 %q under %q) = %q, want %q", id, command, got, id)
				}
				ob.Agent = id
				got := Detect(ob)
				if got.Evidence == id+".process-mismatch" {
					t.Fatalf("Identify claimed this pane for %s and Detect refused it as a process mismatch; "+
						"the row shows a %s chip that can never leave unknown", id, id)
				}
				if got.State != StateIdle || got.Evidence != id+".known-live-fallback" || !got.FallbackIdle {
					t.Fatalf("Detect(%s under %q) = %+v, want the low-evidence idle fallback", id, command, got)
				}
			})
		}
	}
}

// The other direction of the same widening: an argv[0] naming a *different*
// agent is still a refusal. The gate accepts either input naming this family,
// never merely some family.
func TestDetectionOnlyGateRefusesAnotherFamilysProcessIdentity(t *testing.T) {
	for i, id := range aliasGatedFamilies {
		other := aliasGatedFamilies[(i+1)%len(aliasGatedFamilies)]
		got := Detect(Observation{Agent: id, CurrentCommand: "node", ProcessIdentity: other, Screen: "⠋ Working\n"})
		if got.State != StateUnknown || got.Evidence != id+".process-mismatch" {
			t.Errorf("Detect(%s with argv0 %s) = %+v, want %s.process-mismatch", id, other, got, id)
		}
	}
}

// Two mappings and one vocabulary: a detection-only family's id is Herdr's own
// label, so neither mapping may need a case for it. A family that did would be
// carrying a Sidecar-only spelling for no gain, and the loader keys on the file
// name, so a missed case reads the wrong manifest rather than failing.
func TestDetectionOnlyFamilyIDsAreHerdrsOwnLabels(t *testing.T) {
	for _, id := range aliasGatedFamilies {
		if got := ManifestAgentID(id); got != id {
			t.Errorf("ManifestAgentID(%q) = %q; an alias-gated family must not need a manifest-id mapping", id, got)
		}
		if got := HerdrAgentLabel(id); got != id {
			t.Errorf("HerdrAgentLabel(%q) = %q; an alias-gated family must not need a label mapping", id, got)
		}
		if !HasVendoredManifest(id) {
			t.Errorf("no vendored manifest compiles for %q", id)
		}
	}
}

// The ids and display names, printed once so a reviewer can read the twenty
// families and the one declared exclusion without opening three files.
func TestDetectionOnlyRoster(t *testing.T) {
	var rows []string
	for _, family := range agentcatalog.Families() {
		kind := "launchable      "
		if _, hooksOnly := familiesWithNoScreenManifest[family.ID]; hooksOnly {
			kind = "no manifest     "
		}
		rows = append(rows, "  "+kind+pad(family.ID)+family.Name)
	}
	for _, family := range agentcatalog.DetectionFamilies() {
		rows = append(rows, "  detection-only  "+pad(family.ID)+family.Name)
	}
	sort.Strings(rows)
	for id, why := range unregisteredManifests {
		rows = append(rows, "  unregistered    "+pad(id)+why)
	}
	t.Log("Herdr screen-manifest agents and what Sidecar does with each:\n" + strings.Join(rows, "\n"))
}

func pad(id string) string {
	if len(id) >= 14 {
		return id + " "
	}
	return id + strings.Repeat(" ", 14-len(id))
}
