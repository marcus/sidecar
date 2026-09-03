package pluginbrowser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/marcus/sidecar/internal/state"
)

// fixtureBin is the built path of the plugin protocol's reference fixture. The
// browser's own tests are mostly in-memory, but the properties that only a real
// process has — a describe that spawns, a page that arrives from stdout, an act
// that runs — are proven against the same executable the conformance suite uses
// rather than against a second, kinder fake.
var fixtureBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sidecar-pluginbrowser-fixture-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "pluginbrowser tests: temp dir:", err)
		os.Exit(1)
	}

	// Nothing in this package may touch the developer's own state file: the
	// browser persists its split there, so a test that drags a rail would
	// otherwise rewrite a real preference.
	if err := state.InitWithDir(dir); err != nil {
		fmt.Fprintln(os.Stderr, "pluginbrowser tests: isolating state:", err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}

	fixtureBin = filepath.Join(dir, "fixtureprovider")
	build := exec.Command("go", "build", "-o", fixtureBin, "../pluginhost/testdata/fixtureprovider")
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "pluginbrowser tests: building the fixture plugin:", err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
