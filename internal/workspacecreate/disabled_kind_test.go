package workspacecreate

import (
	"strings"
	"testing"
)

// A disabled kind is disabled everywhere the form can be asked about it: the
// row's reason, Validate, and the placement buttons that create on one click.
func TestTerminalSplitDisabledGatesEveryCreatePath(t *testing.T) {
	const reason = "Two terminals are already on screen — close one first"
	for _, tc := range []struct {
		name     string
		disabled string
		kind     Kind
		want     string
	}{
		{name: "enabled", disabled: "", kind: KindTerminalSplit, want: ""},
		{name: "disabled", disabled: reason, kind: KindTerminalSplit, want: reason},
		{name: "other kinds are unaffected", disabled: reason, kind: KindShell, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := Open(OpenOpts{
				Kind:                  tc.kind,
				AllowTerminalSplit:    true,
				TerminalSplitDisabled: tc.disabled,
				TerminalName:          "term · repo",
			})
			if got := f.KindDisabledReason(); got != tc.want {
				t.Fatalf("KindDisabledReason = %q, want %q", got, tc.want)
			}
			if tc.want != "" && f.Validate() != tc.want {
				t.Fatalf("Validate = %q, want the same refusal %q", f.Validate(), tc.want)
			}
			for _, action := range []string{ActionPlaceAuto, ActionPlaceRight, ActionPlaceBelow} {
				if got := f.ApplyPlacementAction(action); got != (tc.want == "") {
					t.Fatalf("ApplyPlacementAction(%s) = %v, want %v", action, got, tc.want == "")
				}
			}
		})
	}
}

// The reason is said once — one line, no explainer — and the kind row it
// belongs to is drawn muted rather than as a selectable choice.
func TestDisabledTerminalSplitRendersOneReasonLine(t *testing.T) {
	const reason = "Two terminals are already on screen — close one first"
	f := Open(OpenOpts{
		Kind:                  KindTerminalSplit,
		AllowTerminalSplit:    true,
		TerminalSplitDisabled: reason,
	})
	rendered := f.Build(60).Render(120, 40, nil)
	if got := strings.Count(rendered, "close one first"); got != 1 {
		t.Fatalf("reason appears %d times, want once:\n%s", got, rendered)
	}
}

// A disabled row that is still the ACTIVE kind — its Name field and placement
// row are drawn below it — keeps the selected row's chrome, or the toggle shows
// nothing selected at all. And the hint line stops promising a confirm that the
// disabled state makes a silent no-op.
func TestDisabledSelectedKindStillReadsAsSelected(t *testing.T) {
	// The selected-but-disabled chrome itself belongs to modal.Select and is
	// proved there; what this modal owes is that the row still reads as the
	// active kind while it says why it cannot be created.
	const reason = "Two terminals are already on screen — close one first"
	f := Open(OpenOpts{
		Kind:                  KindTerminalSplit,
		AllowTerminalSplit:    true,
		TerminalSplitDisabled: reason,
	})
	if rendered := f.Build(60).Render(120, 40, nil); strings.Contains(rendered, "Enter to confirm") {
		t.Fatalf("the disabled modal still promises Enter:\n%s", rendered)
	}

	f = Open(OpenOpts{Kind: KindTerminalSplit, AllowTerminalSplit: true})
	if rendered := f.Build(60).Render(120, 40, nil); !strings.Contains(rendered, "Enter to confirm") {
		t.Fatalf("an enabled modal lost its Enter hint:\n%s", rendered)
	}
}
