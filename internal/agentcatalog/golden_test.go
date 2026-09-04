package agentcatalog

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/catalog.golden from the current catalog")

// The golden file is the whole observable surface of a family: what it launches,
// what it launches with auto-approve on, what it resumes, and every field a
// consumer can read off it. It exists because this catalog moved from Go
// literals to embedded TOML, and the one thing a data migration must not do is
// change a command line. A diff here is a provider launching differently than
// it did before, which is the failure nobody notices until an agent starts with
// the wrong flag.
//
// It is deliberately not restricted to the ten families that existed at the
// move: a new family adds lines rather than changing them, so the file stays
// readable as "what Sidecar runs for every provider it knows".
func TestCatalogGolden(t *testing.T) {
	got := renderCatalog(t)
	path := filepath.Join("testdata", "catalog.golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (regenerate with `go test ./internal/agentcatalog -run TestCatalogGolden -update-golden`)", err)
	}
	if got != string(want) {
		t.Errorf("catalog changed.\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func renderCatalog(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	section := func(title string, list []Family) {
		fmt.Fprintf(&b, "# %s\n", title)
		for _, family := range list {
			writeFamily(t, &b, family)
		}
	}
	section("launchable, in picker order", Families())
	section("detection-only, in id order", DetectionFamilies())

	section("legacy launch, in id order", LegacyFamilies())
	return b.String()
}

func writeFamily(t *testing.T, b *strings.Builder, family Family) {
	t.Helper()
	fmt.Fprintf(b, "  %s\n", family.ID)
	fmt.Fprintf(b, "    name        %s\n", family.Name)
	fmt.Fprintf(b, "    short       %s\n", family.Short)
	fmt.Fprintf(b, "    command     %s\n", family.Command)
	fmt.Fprintf(b, "    skipPerms   %s\n", family.SkipPermissionsArg)
	fmt.Fprintf(b, "    aliases     %s\n", strings.Join(family.Aliases, " "))
	fmt.Fprintf(b, "    adapter     %s\n", family.ConversationAdapterID())
	fmt.Fprintf(b, "    resumeKinds %s\n", strings.Join(family.ResumeKinds, " "))
	fmt.Fprintf(b, "    launch      %s\n", argvLine(family.LaunchArgv(nil, false)))
	fmt.Fprintf(b, "    launch+skip %s\n", argvLine(family.LaunchArgv([]string{"--model", "space value"}, true)))
	for _, kind := range []string{"id", "path"} {
		fmt.Fprintf(b, "    resume/%-4s %s\n", kind, argvLine(family.ResumeArgv(kind, "SESSION", nil)))
	}
}

func argvLine(argv []string, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	return DisplayCommand(argv)
}
