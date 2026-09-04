package agentcatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// preTOMLLaunchFamilies are the ten families that existed as Go literals before
// the catalog moved to embedded TOML, plus the one legacy family, named here
// rather than read from Families() so that adding a family cannot quietly
// shrink what this test covers.
var preTOMLLaunchFamilies = []string{
	"claude", "codex", "copilot", "antigravity", "cursor",
	"opencode", "pi", "amp", "grok", "muse",
	"aider",
}

// testdata/pre-toml-argv.golden was generated from the Go literals on the
// commit before the TOML move and is never regenerated. It is the exit gate of
// that migration stated as a file: eleven providers, their launch line, their
// auto-approve line and their resume line, byte for byte.
//
// A data migration is allowed to change how the catalog is stored and is not
// allowed to change one character of what a provider is started with. Nothing
// else in the suite would catch a transposed flag, because every other test
// compares the catalog against itself.
func TestPreTOMLFamiliesLaunchByteIdentically(t *testing.T) {
	var b strings.Builder
	for _, id := range preTOMLLaunchFamilies {
		family, ok := FindLaunch(id)
		if !ok {
			t.Fatalf("%s is no longer launchable; the migration dropped a family", id)
		}
		fmt.Fprintf(&b, "%s\n", id)
		fmt.Fprintf(&b, "  launch      %s\n", argvLine(family.LaunchArgv(nil, false)))
		fmt.Fprintf(&b, "  launch+args %s\n", argvLine(family.LaunchArgv([]string{"--model", "space value"}, false)))
		fmt.Fprintf(&b, "  launch+skip %s\n", argvLine(family.LaunchArgv([]string{"--model", "space value"}, true)))
		fmt.Fprintf(&b, "  resume      %s\n", argvLine(family.ResumeArgv("id", "SESSION", nil)))
	}
	path := filepath.Join("testdata", "pre-toml-argv.golden")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != string(want) {
		t.Errorf("a provider is launched differently than it was before the TOML move.\n--- before ---\n%s\n--- now ---\n%s", want, got)
	}
}
