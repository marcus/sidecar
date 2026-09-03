package workspacecreate

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/mouse"
)

// The chooser's shapes, styles and click geometry belong to modal.Select and
// are tested there (internal/modal/select_test.go); what belongs here is that
// the Create Workspace modal wears them correctly.

// Opening on the Name field still shows which kind is selected: the frame is
// idle because the control does not have focus, and the selected segment stays
// lit because the selection is a different signal from the focus.
func TestKindControlKeepsShellSelectedWhenNameFocused(t *testing.T) {
	f := Open(testOpts(KindShell))
	view := renderForm(t, f)
	if f.Modal().FocusedID() != FieldName {
		t.Fatalf("focus = %q, want %s", f.Modal().FocusedID(), FieldName)
	}
	if f.Kind() != KindShell {
		t.Fatalf("kind = %v, want shell", f.Kind())
	}
	if !strings.Contains(ansi.Strip(view), "❯ Shell") {
		t.Fatalf("the selected Shell row is not pointed at while Name has focus:\n%s", ansi.Strip(view))
	}
	// The selected row keeps the selected chrome — its own colours, not the
	// idle fill its neighbours wear.
	shellRow, worktreeRow := rowLine(view, "❯ Shell"), rowLine(view, "Worktree")
	if shellRow == "" || worktreeRow == "" {
		t.Fatalf("could not find the kind rows:\n%s", view)
	}
	if styleRun(shellRow) == styleRun(worktreeRow) {
		t.Fatalf("the selected row wears the same chrome as an idle one:\n%s", view)
	}

	focused := Open(OpenOpts{Kind: KindShell, FocusKind: true})
	m := focused.Build(52)
	m.Render(80, 40, mouse.NewHandler())
	m.SetFocus(FieldKind)
	withFocus := m.Render(80, 40, mouse.NewHandler())
	if withFocus == view {
		t.Fatal("the kind control drew the same with and without focus")
	}
}

// rowLine is the first rendered line containing needle.
func rowLine(view, needle string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(line), needle) {
			return line
		}
	}
	return ""
}

// styleRun is a line's escape sequences with its text removed, which is the
// chrome a row is wearing.
func styleRun(line string) string {
	var b strings.Builder
	for _, part := range strings.Split(line, "\x1b") {
		if i := strings.IndexByte(part, 'm'); i >= 0 {
			b.WriteString(part[:i+1])
		}
	}
	return b.String()
}
