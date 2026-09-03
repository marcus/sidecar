package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Help says how to reach the terminal's own selection. The chords are in the
// binding lists; the bypass is not a binding at all — it is the emulator's, and
// it is the answer to "why can I not select that" on a pane Sidecar does not
// make selectable.
func TestHelpNamesTheShiftOptionBypass(t *testing.T) {
	m := routerTestModel(t, newRouterPlugin())
	m.width, m.height = 120, 40
	m.ensureHelpModal()
	if m.helpModal == nil {
		t.Fatal("no help modal was built")
	}
	help := ansi.Strip(m.helpModal.Render(m.width, m.height, nil))
	if !strings.Contains(help, "Selecting text") {
		t.Fatalf("help has no selection section:\n%s", help)
	}
	if !strings.Contains(help, "shift or option") {
		t.Fatalf("help does not name the terminal's own bypass:\n%s", help)
	}
	if !strings.Contains(help, "alt+c") {
		t.Fatalf("help does not name the configured copy chord:\n%s", help)
	}
}
