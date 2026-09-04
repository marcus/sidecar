package agentactivity

import (
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentcatalog"
)

// everyRegisteredFamily is every agent Supports answers true for, read from the
// catalog rather than typed out, so a family added to agentcatalog is covered by
// the invariant below on the day it is added rather than on the day somebody
// remembers to extend a literal list.
func everyRegisteredFamily(t *testing.T) []string {
	t.Helper()
	var ids []string
	for _, family := range agentcatalog.Families() {
		// A family with no vendored screen manifest has no gate to assert:
		// Supports is false for it, so Detect answers unsupported-agent before
		// any gate runs. They are declared in familiesWithNoScreenManifest and
		// covered by the hooks lane instead.
		if _, hooksOnly := familiesWithNoScreenManifest[family.ID]; hooksOnly {
			continue
		}
		ids = append(ids, family.ID)
	}
	for _, family := range agentcatalog.DetectionFamilies() {
		ids = append(ids, family.ID)
	}
	for _, id := range ids {
		if !Supports(id) {
			t.Fatalf("catalog family %q is not Supports()ed; the gate invariant below would not cover it", id)
		}
	}
	if len(ids) < 20 {
		t.Fatalf("read %d families from the catalog, want the twenty registered ones: %v", len(ids), ids)
	}
	return ids
}

// TestOneAgentsManifestIsNeverEvaluatedAgainstAnotherAgentsPane is Slice 3's
// exit gate, stated as a test, and it is the pin that makes widening the gate
// safe rather than merely useful.
//
// The hole it closes was open before the widening, not opened by it. Pi installs
// as a `#!/usr/bin/env node` shim, so a live Pi pane reports
// pane_current_command=node — and claudeProcess("node") is true, as are Codex's,
// Grok's and Antigravity's. Anything that asked Detect about Claude on that pane
// got claude.toml evaluated against a Pi screen. The rule that fixes it is that
// a resolved process identity refuses every provider it does not name, so the
// invariant is stated over every ordered pair of registered families rather than
// over the four gates that happened to be loose.
//
// Every command in the inner loop is one that would pass A's own command gate if
// the identity were not there, including A's own name: identity outranks the
// command, because the command is the weaker evidence and is what a shim lies
// about.
func TestOneAgentsManifestIsNeverEvaluatedAgainstAnotherAgentsPane(t *testing.T) {
	families := everyRegisteredFamily(t)
	// A screen carrying several providers' chrome at once. If a gate ever lets
	// the wrong manifest through, this is what it reads, so the failure is a
	// wrong verdict rather than a quiet unknown.
	screen := "Working...\nesc to interrupt\n△ Permission required\nDo you want to proceed?\n⠋ Thinking\n"
	for _, agent := range families {
		for _, identity := range families {
			if identity == agent {
				continue
			}
			for _, command := range []string{agent, identity, "node", "bun", "agent", "zsh", ""} {
				ob := Observation{
					Agent:           agent,
					ProcessIdentity: identity,
					CurrentCommand:  command,
					PaneTitle:       "⠼ repo - amp - task",
					Screen:          screen,
				}
				got := Detect(ob)
				if got.State != StateUnknown || got.Evidence != agent+".process-mismatch" {
					t.Fatalf("Detect(agent=%s, argv0=%s, command=%q) = %+v, want %s.process-mismatch:\n"+
						"the pane's own process is %s, so %s.toml must never be evaluated against its screen",
						agent, identity, command, got, agent, identity, agent)
				}
				if explain := ExplainManifest(ob); explain != nil {
					t.Fatalf("Detect(agent=%s, argv0=%s, command=%q) evaluated %d rules; a refusal evaluates none",
						agent, identity, command, len(explain.EvaluatedRules))
				}
			}
		}
	}
}

// The positive half of the same rule: a resolved identity admits the provider it
// names, whatever pane_current_command says. Without this the invariant above
// could be satisfied by refusing everything.
func TestAResolvedProcessIdentityAdmitsTheProviderItNames(t *testing.T) {
	for _, agent := range everyRegisteredFamily(t) {
		for _, command := range []string{"node", "bun", "agent", "zsh", "", "vim"} {
			ob := Observation{Agent: agent, ProcessIdentity: agent, CurrentCommand: command, Screen: "\n"}
			got := Detect(ob)
			if got.Evidence == agent+".process-mismatch" {
				t.Errorf("Detect(agent=%s, argv0=%s, command=%q) refused the pane its own argv[0] names", agent, agent, command)
				continue
			}
			if explain := ExplainManifest(ob); explain == nil || len(explain.EvaluatedRules) == 0 {
				t.Errorf("Detect(agent=%s, command=%q) evaluated no rules", agent, command)
			}
		}
	}
}

// An identity input the alias table cannot name is not an identity, and must
// fall through to the command rules rather than refuse. "shell" is the case that
// matters in the field: the resolver answers it for a pane whose foreground
// group is its own shell, which is stale evidence about the agent, not evidence
// against it. A bare runtime name and a path to one are the same shape.
func TestAnUnresolvableProcessIdentityFallsThroughToTheCommandRules(t *testing.T) {
	for _, identity := range []string{"", "shell", "zsh", "node", "bun", "agent", "/usr/local/bin/node", "not-an-agent"} {
		got := Detect(Observation{Agent: "claude", ProcessIdentity: identity, CurrentCommand: "claude", Screen: "\n"})
		if got.Evidence != "claude.known-live-fallback" {
			t.Errorf("Detect(claude, argv0=%q, command=claude) = %+v, want the command gate to stand", identity, got)
		}
	}
}

// TestTheNoIdentityGateIsUnchanged is a pin, not a specification: it records
// exactly what each provider accepts from pane_current_command alone, because
// that is the whole of the evidence on a platform with no process-identity
// adapter (process_identity_other.go resolves nothing). Widening the gate must
// not narrow this, or a Claude pane on such a platform stops being detectable.
//
// The runtime allowances are the rows worth reading: claude, codex, grok and
// antigravity accept a bare `node` and `bun` here and everybody else does not.
// That is the loose half of the old gate, kept deliberately, because with no
// identity resolved it is the only thing that reaches a Node-installed agent at
// all — and it is safe to keep only because processGate now refuses it the
// moment an identity says otherwise.
func TestTheNoIdentityGateIsUnchanged(t *testing.T) {
	tests := []struct {
		agent   string
		accepts []string
		refuses []string
	}{
		// claudeProcess is an exact match plus the version-shaped argv[0] Claude
		// Code renames itself to; it is narrower than identifyProcessName, which
		// also knows "claude-code" and strips launcher suffixes. That asymmetry
		// is pre-existing and is recorded here rather than quietly fixed.
		{"claude", []string{"claude", "node", "bun", "1.2.3"}, []string{"zsh", "codex", "pi", "agent", "", "claude-code", "claude.cmd"}},
		{"codex", []string{"codex", "codex-cli", "node", "bun"}, []string{"zsh", "claude", "pi", "agent", ""}},
		{"grok", []string{"grok", "grok-build", "node", "bun"}, []string{"zsh", "claude", "pi", "agent", ""}},
		{"antigravity", []string{"agy", "antigravity", "node", "bun"}, []string{"zsh", "claude", "pi", "agent", ""}},
		// Pi allows no runtime; see piProcess for the reasoning and the residual.
		{"pi", []string{"pi"}, []string{"node", "bun", "zsh", "claude", "agent", ""}},
		{"copilot", []string{"copilot", "github-copilot", "ghcs"}, []string{"node", "bun", "zsh", "claude", ""}},
		{"cursor", []string{"cursor-agent", "cursor", "cursor-agent.cmd", "agent"}, []string{"node", "bun", "zsh", "claude", ""}},
		{"opencode", []string{"opencode", "open-code"}, []string{"node", "bun", "zsh", "claude", "agent", ""}},
		{"amp", []string{"amp", "amp-local"}, []string{"node", "bun", "zsh", "claude", "agent", ""}},
		{"muse", []string{"muse", "muse-code", "muse-cli", "muse-bin-3"}, []string{"node", "bun", "zsh", "claude", "agent", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			for _, command := range tt.accepts {
				ob := Observation{Agent: tt.agent, CurrentCommand: command, Screen: "\n"}
				if got := Detect(ob); got.Evidence == tt.agent+".process-mismatch" {
					t.Errorf("%s gate refused %q with no identity resolved; it accepted it before the widening", tt.agent, command)
				}
			}
			for _, command := range tt.refuses {
				ob := Observation{Agent: tt.agent, CurrentCommand: command, Screen: "\n"}
				got := Detect(ob)
				if got.State != StateUnknown || got.Evidence != tt.agent+".process-mismatch" {
					t.Errorf("%s gate accepted %q with no identity resolved: %+v", tt.agent, command, got)
				}
			}
		})
	}
	// The ten detection-only families have no hand-written gate: their whole
	// no-identity rule is the alias table, and it is exact.
	for _, family := range agentcatalog.DetectionFamilies() {
		if got := Detect(Observation{Agent: family.ID, CurrentCommand: family.ID, Screen: "\n"}); got.Evidence == family.ID+".process-mismatch" {
			t.Errorf("%s gate refused its own command name", family.ID)
		}
		for _, command := range []string{"node", "bun", "agent", "zsh", ""} {
			got := Detect(Observation{Agent: family.ID, CurrentCommand: command, Screen: "\n"})
			if got.State != StateUnknown || got.Evidence != family.ID+".process-mismatch" {
				t.Errorf("%s gate accepted %q with no identity resolved: %+v", family.ID, command, got)
			}
		}
	}
}

// TestAPiShimPaneReachesTheManifestEngine is the measured case this widening
// exists for, driven end to end through Detect.
//
// Slice 1 proved the failure live on Pi 0.84.3: the pane's foreground command is
// `node`, piProcess matched only the literal "pi", and Detect returned
// pi.process-mismatch before a rule ran. `sidecar agent list` showed that
// evidence and `sidecar agent start --kind pi` timed out on it. The screen lane
// answered correctly for the same capture the whole time, which is what made the
// gate the entire defect.
//
// There is no checked-in idle capture of a live Pi pane — testdata/pi holds a
// working fixture, a shell false positive and a proof record, and Slice 1's
// artifacts are hook traces rather than screens — so the idle screen here is
// constructed: upstream's pi.toml has exactly one rule, `working_literal`, so
// "any screen without the literal Working..." is the whole of what the idle
// fallback needs, and a composer line is the honest minimum. The working half
// below uses the real fixture text so the case that a rule genuinely fires is
// covered by something captured.
func TestAPiShimPaneReachesTheManifestEngine(t *testing.T) {
	const shimCommand = "node" // what tmux reports for a `#!/usr/bin/env node` bin

	t.Run("idle", func(t *testing.T) {
		ob := Observation{
			Agent:           "pi",
			CurrentCommand:  shimCommand,
			ProcessIdentity: "pi",
			Screen:          "▌ pi\n\n> \n",
			PaneHeight:      24,
		}
		result, explain := DetectManifest(ob)
		if explain == nil {
			t.Fatalf("the gate refused a live Pi pane: %s", result.Evidence)
		}
		if len(explain.EvaluatedRules) == 0 {
			t.Fatal("pi.toml evaluated no rules")
		}
		if result.State != StateIdle || result.Evidence != "pi.known-live-fallback" || !result.FallbackIdle {
			t.Fatalf("Detect(pi under node) = %+v, want the low-evidence idle fallback", result)
		}
	})

	t.Run("working", func(t *testing.T) {
		fixture := readObservationFixture(t, "pi", "working_compatibility.txt")
		if !strings.Contains(fixture.Screen, "Working...") {
			t.Fatalf("testdata/pi/working_compatibility.txt no longer carries the literal this rule reads: %q", fixture.Screen)
		}
		// The fixture is a pane running `pi` directly; the shim is the same
		// screen behind a runtime command name.
		fixture.CurrentCommand = shimCommand
		fixture.ProcessIdentity = "pi"
		got := Detect(fixture)
		if got.State != StateWorking || got.Evidence != "working_literal" {
			t.Fatalf("Detect(pi under node) = %+v, want working_literal", got)
		}
	})

	t.Run("identity is still required", func(t *testing.T) {
		// Without the resolved argv the refusal is exactly the one Slice 1
		// measured. Pi gets no bare-runtime allowance, because pi.toml's single
		// rule is a generic literal and would then claim any Node pane.
		got := Detect(Observation{Agent: "pi", CurrentCommand: shimCommand, Screen: "Working...\n"})
		if got.State != StateUnknown || got.Evidence != "pi.process-mismatch" {
			t.Fatalf("Detect(pi under node, no identity) = %+v, want pi.process-mismatch", got)
		}
	})
}
