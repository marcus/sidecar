package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/agentlifecycle"
	"github.com/marcus/sidecar/internal/agentlifecycle/lifecyclestore"
)

// The subprocess seam, closed.
//
// internal/agentintegration/steelthread_test.go says out loud that the one link
// its end-to-end wiring does not exercise is the subprocess boundary between a
// bundled asset and `sidecar agent report`. That hole is exactly where a real
// bug lived, and it is worth stating precisely because the shape recurs.
//
// The Pi asset shipped a report counter seeded at `Date.now() * 1000`, copied
// from upstream, whose transport is a socket that bounds nothing. Sidecar's
// store bounds the field: MaxSequence is 1 << 40, about 1.10e12, enforced
// unconditionally in agentlifecycle.Validate. The seed is about 1.79e15, roughly
// 1600x over. Every single report was therefore rejected as "sequence N exceeds
// 1099511627776" — and in total silence, because reports spawn with
// stdio "ignore" and their exit codes are never read.
//
// Nothing caught it. The asset's own harnesses ran, the Go mirror agreed element
// for element, the golden checksum was updated, and the whole suite passed,
// because every one of those tests compares argv against argv. Not one of them
// asked whether the argv is something the shipped CLI would accept.
//
// That is the question this file asks, for every asset, over the argv the assets
// really spawn: the ordering harnesses record each report process's complete
// argv, and each one is pushed through the real flag parser, the real report
// construction, agentlifecycle.Validate, and an append to a real store. A value
// the store would refuse fails here instead of on a user's machine.
func TestBundledAssetsSpawnArgvTheShippedCLIAccepts(t *testing.T) {
	node := assetHarnessNode(t)

	for _, provider := range []struct {
		name string
		run  func(t *testing.T, node, dir string) [][]string
	}{
		{name: "pi", run: runPiOrderingHarness},
		{name: "opencode", run: runOpenCodeOrderingHarness},
		{name: "kilo", run: runKiloOrderingHarness},
	} {
		t.Run(provider.name, func(t *testing.T) {
			argvs := provider.run(t, node, t.TempDir())
			if len(argvs) == 0 {
				t.Fatal("the ordering harness recorded no report processes, so this asserts nothing")
			}

			// One store for the whole provider, so the reports arrive against a
			// single run exactly as they would live. That is what makes the
			// second and later reports meaningful: the store enforces a strictly
			// increasing sequence per run, so a batch that all validates
			// individually can still be rejected as a batch.
			store := lifecyclestore.NewMemory()
			var stored []uint64
			for _, argv := range argvs {
				if seq, ok := acceptAssetArgv(t, store, argv); ok {
					stored = append(stored, seq)
				}
			}
			if len(stored) < 1 {
				t.Fatal("no state report reached the store; the seam proves nothing")
			}
			// Store-assigned sequences start at one and climb by one. A gap here
			// would mean a report was rejected without failing above.
			for i, got := range stored {
				if want := uint64(i + 1); got != want {
					t.Fatalf("stored sequences are %v; the store assigns 1, 2, 3 in arrival order, so %d at position %d means a report was lost",
						stored, got, i)
				}
			}
		})
	}
}

// acceptAssetArgv pushes one recorded argv through the real CLI path and fails
// the test if anything refuses it. It reports the stored sequence for a state
// report, and ok=false for a verb that stores nothing here (a session binding,
// which does not go to the lifecycle store at all).
//
// Identity is synthesized rather than derived, because deriving it needs a live
// tmux pane. That is the one part of the real path this cannot run, and it is
// also the part the asset has no influence over: every identity field is
// Sidecar's own, and the asset cannot select a pane through a flag. Everything
// the ASSET chooses — the verb, the flags, the source, the provider, the state,
// the reason, and whether a sequence is sent — goes through the shipped parser
// and the shipped validator exactly as it would in production.
func acceptAssetArgv(t *testing.T, store lifecyclestore.Store, argv []string) (uint64, bool) {
	t.Helper()
	if len(argv) < 2 || argv[0] != "agent" {
		t.Fatalf("an asset spawned %v, which is not a `sidecar agent ...` invocation", argv)
	}

	var out, errOut bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errOut}

	switch verb := argv[1]; verb {
	case "report-session":
		cmd := RootCommand().FindSubcommand("agent").FindSubcommand("report-session")
		if cmd == nil {
			t.Fatal("no `agent report-session` command is registered")
		}
		if _, code := parseReportSessionFlags(env, argv[2:], RenderHelp(cmd)); code != -1 {
			t.Fatalf("the shipped CLI refused an argv the asset spawns: %v\nexit %d: %s", argv, code, errOut.String())
		}
		return 0, false

	case "report", "end", "release":
		kind := map[string]agentlifecycle.Kind{
			"report":  agentlifecycle.KindState,
			"end":     agentlifecycle.KindEnd,
			"release": agentlifecycle.KindRelease,
		}[verb]
		cmd := RootCommand().FindSubcommand("agent").FindSubcommand(verb)
		if cmd == nil {
			t.Fatalf("no `agent %s` command is registered", verb)
		}
		f, code := parseLifecycleFlags(env, argv[2:], RenderHelp(cmd), kind)
		if code != -1 {
			t.Fatalf("the shipped CLI refused an argv the asset spawns: %v\nexit %d: %s", argv, code, errOut.String())
		}

		// The same record runLifecycleWrite builds, with identity synthesized.
		rec := agentlifecycle.Report{
			SchemaVersion: agentlifecycle.SchemaVersion,
			ID:            "asset-seam-" + strconv.Itoa(int(time.Now().UnixNano()%1e9)),
			Kind:          kind,
			Identity: agentlifecycle.Identity{
				Host:              "seam-host",
				ServerIncarnation: "pid=4242",
				PaneID:            "%1",
				Provider:          f.provider,
				RunID:             "seamrun0000000000000000",
				ProcessGeneration: "pid=31337,start=Wed-Sep-2-09-00-00-2026",
			},
			Source:        f.source,
			SourceVersion: f.sourceVersion,
			Sequence:      f.seq,
			ObservedAt:    time.Now(),
			Reason:        agentlifecycle.ReasonCode(f.reason),
			Detail:        f.detail,
		}
		switch kind {
		case agentlifecycle.KindState:
			rec.State = agentactivity.State(f.state)
		case agentlifecycle.KindEnd:
			rec.Outcome = agentlifecycle.Outcome(f.outcome)
		}

		// Validate first, and separately, because that is the call that refused
		// the over-bound sequence and the error it gives names the field.
		if err := agentlifecycle.Validate(rec, time.Now()); err != nil {
			t.Fatalf("the shipped validator refused the record an asset's argv builds: %v\nargv: %v", err, argv)
		}

		// Then the store, which is where a sequence the asset chose has to
		// coexist with every other report from the same run.
		if f.seqSet {
			if _, err := store.Append(rec); err != nil {
				t.Fatalf("the store refused a report the asset spawns: %v\nargv: %v", err, argv)
			}
			return rec.Sequence, true
		}
		got, _, err := store.AppendNext(rec)
		if err != nil {
			t.Fatalf("the store refused a report the asset spawns: %v\nargv: %v", err, argv)
		}
		return got.Sequence, true
	}

	t.Fatalf("an asset spawned an unknown verb: %v", argv)
	return 0, false
}

// runPiOrderingHarness runs Pi's ordering harness and returns each report
// process's argv in the order the processes completed, which for a serialized
// queue is the order they were spawned.
func runPiOrderingHarness(t *testing.T, node, dir string) [][]string {
	t.Helper()
	out := runAssetHarness(t, node, "pi", dir,
		filepath.Join(dir, "sidecar-stub"), filepath.Join(dir, "order.log"), filepath.Join(dir, "argv"))
	var result struct {
		Order []string            `json:"order"`
		Argv  map[string][]string `json:"argv"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("the pi ordering harness output is not JSON: %q (%v)", out, err)
	}
	argvs := make([][]string, 0, len(result.Order))
	for _, label := range result.Order {
		argvs = append(argvs, result.Argv[label])
	}
	return argvs
}

// runOpenCodeOrderingHarness does the same for OpenCode, whose harness keys its
// recorded argv by the sequence the asset assigned rather than by a content
// label. OpenCode legitimately holds a counter: its plugin is one long-lived
// process, which is the shape --seq was designed for.
func runOpenCodeOrderingHarness(t *testing.T, node, dir string) [][]string {
	t.Helper()
	out := runAssetHarness(t, node, "opencode", dir,
		filepath.Join(dir, "sidecar-stub"), filepath.Join(dir, "order.log"), filepath.Join(dir, "argv"))
	var result struct {
		Order []string            `json:"order"`
		Argv  map[string][]string `json:"argv"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("the opencode ordering harness output is not JSON: %q (%v)", out, err)
	}
	// Sorted by the sequence the asset assigned, so the store sees them in the
	// order the asset intended rather than the order the stub happened to exit
	// in — that harness deliberately inverts the exit order to prove the queue.
	keys := make([]string, 0, len(result.Argv))
	for k := range result.Argv {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.Atoi(keys[i])
		b, _ := strconv.Atoi(keys[j])
		return a < b
	})
	argvs := make([][]string, 0, len(keys))
	for _, k := range keys {
		argvs = append(argvs, result.Argv[k])
	}
	return argvs
}

// runKiloOrderingHarness does the same for Kilo, whose harness keys its recorded
// argv by content label exactly as Pi's does. Kilo, like Pi, sends no sequence at
// all: its plugin is one long-lived process but the reports are subprocesses, and
// the store's AppendNext assigns under the lock it already holds.
func runKiloOrderingHarness(t *testing.T, node, dir string) [][]string {
	t.Helper()
	out := runAssetHarness(t, node, "kilo", dir,
		filepath.Join(dir, "sidecar-stub"), filepath.Join(dir, "order.log"), filepath.Join(dir, "argv"))
	var result struct {
		Order []string            `json:"order"`
		Argv  map[string][]string `json:"argv"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("the kilo ordering harness output is not JSON: %q (%v)", out, err)
	}
	argvs := make([][]string, 0, len(result.Order))
	for _, label := range result.Order {
		argvs = append(argvs, result.Argv[label])
	}
	return argvs
}

func runAssetHarness(t *testing.T, node, provider, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(node, append([]string{"ordering-harness.mjs"}, args...)...)
	cmd.Dir = filepath.Join("..", "agentintegration", "assets", provider)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running the %s ordering harness: %v\n%s", provider, err, stderr.String())
	}
	return out
}

// assetHarnessNode mirrors agentintegration's requireNode: the harnesses need a
// real node, and a machine without one skips rather than passing vacuously —
// unless SIDECAR_REQUIRE_NODE=1 says the run is one where that would hide a
// regression, in which case a missing node is a failure.
func assetHarnessNode(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err == nil {
		return node
	}
	if os.Getenv("SIDECAR_REQUIRE_NODE") == "1" {
		t.Fatal("SIDECAR_REQUIRE_NODE=1 but node is not on PATH; the asset argv seam cannot be checked")
	}
	t.Skip("node is not installed; skipping the asset argv seam check")
	return ""
}
