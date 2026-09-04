package configui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/agentintegration"
	"github.com/marcus/sidecar/internal/agentlifecycle"
	"github.com/marcus/sidecar/internal/config"
)

// The Integrations route is tested against a Service over a temporary HOME, so
// the mutation tests exercise the real installer rather than a stand-in for it
// and still never touch a developer's own ~/.config/opencode.

func integrationsFixture(t *testing.T) (*Model, *agentintegration.Service, string) {
	t.Helper()
	m, _ := configFixture(t, &config.Config{})
	home := t.TempDir()
	configHome := filepath.Join(home, ".config")
	if err := os.MkdirAll(filepath.Join(configHome, "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := &agentintegration.Service{
		Env: agentintegration.Env{
			Home:       home,
			ConfigHome: configHome,
			LookPath: func(file string) (string, error) {
				if file == agentintegration.OpenCodeProvider {
					return filepath.Join(home, "bin", "opencode"), nil
				}
				return "", errors.New("not found")
			},
			ProviderVersion: func(string) string { return "1.18.25" },
			UID:             os.Getuid(),
		},
		Adapters: agentintegration.DefaultAdapters(),
	}
	m.SetIntegrationService(svc)
	asset := filepath.Join(configHome, "opencode", agentintegration.OpenCodeOwnedDir, agentintegration.OpenCodeAssetName)
	return m, svc, asset
}

// settle runs whatever the surface queued until nothing is left, feeding each
// result back through Handle the way the host does. Discovery is asynchronous
// by design, so a test that skipped this would be asserting the loading state.
func settle(t *testing.T, m *Model) {
	t.Helper()
	for i := 0; i < 20; i++ {
		cmd := m.TakePending()
		if cmd == nil {
			return
		}
		msg := cmd()
		if msg == nil {
			continue
		}
		feed(t, m, msg)
	}
	t.Fatal("the surface never stopped queueing work")
}

func feed(t *testing.T, m *Model, msg tea.Msg) {
	t.Helper()
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			if c == nil {
				continue
			}
			feed(t, m, c())
		}
	case Msg:
		if cmd := m.Handle(msg); cmd != nil {
			feed(t, m, cmd())
		}
	}
}

func openIntegrations(t *testing.T, m *Model) string {
	t.Helper()
	m.Open(PageAgents)
	runIntegrationControl(t, m, regionAgentIntegrations)
	settle(t, m)
	if m.Route().Child != ChildAgentIntegrations {
		t.Fatalf("route is %q, want the integrations child", m.Route().Child)
	}
	return ansi.Strip(m.View(160, 45))
}

func TestTheAgentsPageLeadsToIntegrations(t *testing.T) {
	m, _, _ := integrationsFixture(t)
	m.Open(PageAgents)
	view := ansi.Strip(m.View(160, 45))
	if !strings.Contains(view, "Integrations") {
		t.Fatalf("the Agents page does not offer Integrations:\n%s", view)
	}

	view = openIntegrations(t, m)
	if !strings.Contains(view, "Back to Agents") {
		t.Fatalf("the route does not offer a way back:\n%s", view)
	}
	if !m.Escape() {
		t.Fatal("Escape did not leave the route")
	}
	if m.Route().Page != PageAgents || m.Route().IsChild() {
		t.Fatalf("Escape landed on %+v", m.Route())
	}
}

func TestDiscoveryNeverRunsOnARenderPath(t *testing.T) {
	// Discovery stats directories, hashes files, and looks up executables on
	// PATH. None of that may happen while a frame is being painted, so opening
	// the route must leave the work queued rather than done.
	m, _, _ := integrationsFixture(t)
	m.Open(PageAgents)

	// The control is run but its command is deliberately held, so the queued
	// work has demonstrably not happened yet while the frames below are
	// painted.
	m.View(160, 45)
	m.detailFocus = true
	var held tea.Cmd
	for i, c := range m.controls {
		if c.id == regionAgentIntegrations {
			m.focusControlIndex(i)
			held = m.runControl(i)
			break
		}
	}
	if held == nil {
		t.Fatal("opening Integrations produced no command")
	}

	state := m.agentIntegrations()
	if !state.checking || state.checked {
		t.Fatalf("opening the route did not schedule discovery: %+v", state)
	}
	// Rendering repeatedly must not perform it either, and must not queue a
	// second probe per frame.
	for i := 0; i < 3; i++ {
		view := ansi.Strip(m.View(160, 45))
		if !strings.Contains(view, "Checking") {
			t.Fatalf("the route does not say it is still looking:\n%s", view)
		}
	}
	if m.agentIntegrations().checked {
		t.Fatal("rendering performed the discovery")
	}

	feed(t, m, held())
	settle(t, m)
	if !m.agentIntegrations().checked || len(m.agentIntegrations().list) == 0 {
		t.Fatalf("discovery produced nothing: %+v", m.agentIntegrations())
	}
}

func TestARouteReachedWithoutDiscoverySaysSoRatherThanClaimingToBeChecking(t *testing.T) {
	// buildAgentIntegrations used to queue discovery from the render path when
	// it found the route unchecked — a restored route, or a direct push. The
	// queue is drained only by a key, a mouse event, or TakePending, so the
	// route painted "Checking which agents are installed…" over work nothing
	// was running, and stayed there until the user happened to press something.
	//
	// A render path may not start work, so the honest state is "not looked
	// yet", with the key that looks named on screen.
	m, _, _ := integrationsFixture(t)
	m.Open(PageAgents)
	m.PushChild(ChildAgentIntegrations, "Integrations")

	for i := 0; i < 3; i++ {
		view := ansi.Strip(m.View(160, 45))
		if strings.Contains(view, "Checking") {
			t.Fatalf("the route says it is checking with nothing running:\n%s", view)
		}
		if !strings.Contains(view, "Press R") {
			t.Fatalf("the route does not name the key that checks:\n%s", view)
		}
	}
	if cmd := m.TakePending(); cmd != nil {
		t.Fatal("rendering queued discovery")
	}
	if state := m.agentIntegrations(); state.checking || state.checked {
		t.Fatalf("rendering changed the route's state: %+v", state)
	}

	// And the key it names does the work.
	runIntegrationControl(t, m, regionIntegrationRecheck)
	settle(t, m)
	if state := m.agentIntegrations(); !state.checked || len(state.list) == 0 {
		t.Fatalf("Recheck produced nothing: %+v", state)
	}
	if view := ansi.Strip(m.View(160, 45)); !strings.Contains(view, "opencode") {
		t.Fatalf("the list did not appear after Recheck:\n%s", view)
	}
}

func TestTheRouteShowsEveryProviderAndItsHonestState(t *testing.T) {
	m, _, _ := integrationsFixture(t)
	view := openIntegrations(t, m)
	// grok is the unsupported exemplar: a recorded capability with no bundled
	// adapter, so the route has to show it and say so. It took that job from pi
	// while pi's capability entry was retracted, and keeps it now that pi ships
	// an adapter of its own.
	for _, want := range []string{"opencode", "codex", "claude", "pi", "kilo", "grok", "unsupported"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the route does not mention %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "0 of 5 installed") {
		t.Fatalf("the summary is wrong:\n%s", view)
	}
}

func TestTheFocusedRowNamesTheExactFilesAndOffersOnlyAcceptedActions(t *testing.T) {
	m, svc, asset := integrationsFixture(t)
	openIntegrations(t, m)

	focusIntegration(t, m, "opencode")
	view := ansi.Strip(m.View(160, 45))
	shown := "~" + strings.TrimPrefix(asset, svc.Env.Home)
	if !strings.Contains(view, shown) {
		t.Fatalf("the focused row does not name the file an install would write (%s):\n%s", shown, view)
	}
	if !strings.Contains(view, "Install") {
		t.Fatalf("the focused row does not offer Install:\n%s", view)
	}
	// Nothing is installed, so the verbs that need an installation must not be
	// on offer: a pill that refuses when pressed is worse than one that is not
	// there.
	for _, absent := range []string{"Update", "Repair"} {
		if strings.Contains(view, absent) {
			t.Fatalf("the route offers %s with nothing installed:\n%s", absent, view)
		}
	}

	st, err := svc.Status("opencode")
	if err != nil {
		t.Fatal(err)
	}
	// The route offers exactly what the service says it would accept, rather
	// than a list this page decided on.
	for _, act := range st.Offered {
		if !strings.Contains(view, integrationActionLabel(act)) {
			t.Fatalf("the service offers %s but the route does not:\n%s", act, view)
		}
	}
}

func focusIntegration(t *testing.T, m *Model, provider string) int {
	t.Helper()
	m.View(160, 45)
	state := m.agentIntegrations()
	for i, st := range state.list {
		if st.Provider != provider {
			continue
		}
		m.detailFocus = true
		// Focus, render, and re-anchor once. The row cursor is a *position*
		// among the cursor-visitable controls of the previous frame, and the
		// previously focused row's action pills are cursor controls too: when
		// focus leaves that row its pills vanish, every later row shifts up by
		// the pill count, and a single focus lands on the wrong provider. Rows
		// before the target are unfocused after the first render, so the
		// second anchoring is computed against a pill-free prefix and
		// converges.
		for range [2]int{} {
			m.focusControlByID(regionIntegrationRow + itoa(i))
			m.View(160, 45)
		}
		if m.focusedID != regionIntegrationRow+itoa(i) {
			t.Fatalf("focus landed on %q, want row %d (%s)", m.focusedID, i, provider)
		}
		return i
	}
	t.Fatalf("no row for %q", provider)
	return -1
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestInstallingIsConfirmedByNamingTheFilesAndThenActuallyInstalls(t *testing.T) {
	m, svc, asset := integrationsFixture(t)
	openIntegrations(t, m)
	index := focusIntegration(t, m, "opencode")

	// Pressing Install plans first; nothing is written yet.
	runIntegrationControl(t, m, integrationActionID(index, agentintegration.ActionInstall))
	if _, err := os.Stat(asset); !os.IsNotExist(err) {
		t.Fatalf("pressing Install wrote a file before the confirmation: %v", err)
	}
	if m.confirm == nil {
		t.Fatal("no confirmation was raised")
	}

	view := ansi.Strip(m.View(160, 45))
	// The confirmation names the file with home abbreviated, which is what the
	// pane can fit and what a user recognises.
	shown := "~" + strings.TrimPrefix(asset, svc.Env.Home)
	if !strings.Contains(view, shown) {
		t.Fatalf("the confirmation does not name the file it will write (%s):\n%s", shown, view)
	}
	if !strings.Contains(view, "write") {
		t.Fatalf("the confirmation does not say what it will do:\n%s", view)
	}

	// Declining changes nothing.
	m.DismissConfirm()
	if _, err := os.Stat(asset); !os.IsNotExist(err) {
		t.Fatalf("declining still wrote the file: %v", err)
	}

	// Confirming does. Dismissing put the cursor back at the top of the page, so
	// the row has to be reselected before its pills exist again.
	index = focusIntegration(t, m, "opencode")
	runIntegrationControl(t, m, integrationActionID(index, agentintegration.ActionInstall))
	if m.confirm == nil {
		t.Fatal("no confirmation on the second attempt")
	}
	// Through the Apply button, which is what clears the confirmation and runs
	// the mutation in the running application.
	runIntegrationControl(t, m, "confirm-apply")
	settle(t, m)

	if _, err := os.Stat(asset); err != nil {
		t.Fatalf("confirming did not install: %v", err)
	}
	st, err := svc.Status("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != agentlifecycle.StatusCurrent {
		t.Fatalf("status after install: %s", st.Status)
	}
	// The route re-reads rather than assuming, so what it shows is what is on
	// disk.
	view = ansi.Strip(m.View(160, 45))
	if !strings.Contains(view, "current") || !strings.Contains(view, "1 of 5 installed") {
		t.Fatalf("the route did not refresh after the mutation:\n%s", view)
	}
}

func integrationActionID(index int, act agentintegration.Action) string {
	return regionIntegrationAction + itoa(index) + "-" + string(act)
}

// runIntegrationControl focuses and runs one control by id, feeding whatever it
// returns back through the host's message path.
func runIntegrationControl(t *testing.T, m *Model, id string) {
	t.Helper()
	m.View(160, 45)
	m.detailFocus = true
	for i, c := range m.controls {
		if c.id != id {
			continue
		}
		m.focusControlIndex(i)
		if cmd := m.runControl(i); cmd != nil {
			feed(t, m, cmd())
		}
		return
	}
	t.Fatalf("control %q is not on screen", id)
}

func TestUninstallingRemovesOnlyWhatSidecarInstalled(t *testing.T) {
	m, svc, asset := integrationsFixture(t)
	openIntegrations(t, m)
	if _, err := svc.Apply("opencode", agentintegration.ActionInstall); err != nil {
		t.Fatal(err)
	}
	// Something of the user's, in the directory Sidecar just wrote into.
	other := filepath.Join(filepath.Dir(asset), "mine.js")
	if err := os.WriteFile(other, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.queueIntegrationProbe()
	settle(t, m)
	index := focusIntegration(t, m, "opencode")

	runIntegrationControl(t, m, integrationActionID(index, agentintegration.ActionUninstall))
	if m.confirm == nil {
		t.Fatal("uninstall was not confirmed")
	}
	if view := ansi.Strip(m.View(160, 45)); !strings.Contains(view, "Only files Sidecar installed are removed.") {
		t.Fatalf("the confirmation does not say what survives:\n%s", view)
	}
	// Through the Apply button, which is what clears the confirmation and runs
	// the mutation in the running application.
	runIntegrationControl(t, m, "confirm-apply")
	settle(t, m)

	if _, err := os.Stat(asset); !os.IsNotExist(err) {
		t.Fatalf("the asset survived the uninstall: %v", err)
	}
	if b, err := os.ReadFile(other); err != nil || string(b) != "mine\n" {
		t.Fatalf("an unrelated plugin was removed: %v %q", err, string(b))
	}
}

// TestTheRouteAndTheCLIProduceTheSameChange is the parity gate for step 3: the
// same action, through the two surfaces, has to leave the tree in the same
// state and be described by the same plan.
func TestTheRouteAndTheCLIProduceTheSameChange(t *testing.T) {
	viaUI, uiSvc, uiAsset := integrationsFixture(t)
	openIntegrations(t, viaUI)
	index := focusIntegration(t, viaUI, "opencode")
	runIntegrationControl(t, viaUI, integrationActionID(index, agentintegration.ActionInstall))
	if viaUI.confirm == nil {
		t.Fatal("no confirmation")
	}
	runIntegrationControl(t, viaUI, "confirm-apply")
	settle(t, viaUI)

	// The CLI's equivalent is a direct Apply through the same service, which is
	// exactly what the command does.
	_, cliSvc, cliAsset := integrationsFixture(t)
	cliPlan, err := cliSvc.Apply("opencode", agentintegration.ActionInstall)
	if err != nil {
		t.Fatal(err)
	}

	uiBytes, err := os.ReadFile(uiAsset)
	if err != nil {
		t.Fatal(err)
	}
	cliBytes, err := os.ReadFile(cliAsset)
	if err != nil {
		t.Fatal(err)
	}
	if string(uiBytes) != string(cliBytes) {
		t.Fatal("the two surfaces installed different bytes")
	}

	uiStatus, err := uiSvc.Status("opencode")
	if err != nil {
		t.Fatal(err)
	}
	cliStatus, err := cliSvc.Status("opencode")
	if err != nil {
		t.Fatal(err)
	}
	// Paths differ by fixture, so compare everything that should not.
	uiStatus.TargetPaths, cliStatus.TargetPaths = nil, nil
	uiStatus.Files, cliStatus.Files = nil, nil
	uiStatus.ProviderPath, cliStatus.ProviderPath = "", ""
	x, _ := json.Marshal(uiStatus)
	y, _ := json.Marshal(cliStatus)
	if string(x) != string(y) {
		t.Fatalf("the two surfaces report different state:\n%s\n%s", x, y)
	}
	if cliPlan.StatusAfter != agentlifecycle.StatusCurrent {
		t.Fatalf("the CLI plan ended at %s", cliPlan.StatusAfter)
	}
}

func TestARefusalIsReportedInlineRatherThanAsADialog(t *testing.T) {
	m, svc, _ := integrationsFixture(t)
	openIntegrations(t, m)
	index := focusIntegration(t, m, "opencode")

	// Install behind the route's back, so the plan it asks for is refused.
	if _, err := svc.Apply("opencode", agentintegration.ActionInstall); err != nil {
		t.Fatal(err)
	}
	runIntegrationControl(t, m, integrationActionID(index, agentintegration.ActionInstall))

	if m.confirm != nil {
		t.Fatal("a refusal raised a confirmation")
	}
	view := ansi.Strip(m.View(160, 45))
	if !strings.Contains(view, "nothing to do") && !strings.Contains(view, "already") {
		t.Fatalf("the route does not say why nothing happened:\n%s", view)
	}
}

func TestTheRouteFitsEveryTerminalSizeAndKeepsItsRowsReachable(t *testing.T) {
	m, _, _ := integrationsFixture(t)
	openIntegrations(t, m)
	for _, size := range [][2]int{{60, 24}, {100, 30}, {160, 45}, {200, 50}} {
		view := m.View(size[0], size[1])
		lines := strings.Split(view, "\n")
		if len(lines) != size[1] {
			t.Fatalf("%dx%d painted %d lines", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > size[0] {
				t.Fatalf("%dx%d line %d is %d wide", size[0], size[1], i, w)
			}
		}
		// The row the cursor is on stays visible, which is the property this
		// is really about: the cursor must never be able to walk onto a row
		// that was clipped away.
		//
		// It used to look for the literal "opencode", which was the same
		// assertion only while the alphabetically-sorted list was short enough
		// for every provider to fit in 24 lines. The kilo port made it five
		// providers and opencode fell below the fold of the DEFAULT frame,
		// where the cursor is on the first row -- ordinary list behaviour, and
		// not what this test exists to catch. Focusing opencode brings it back
		// into view at 60x24, which is what the check below states directly.
		stripped := ansi.Strip(view)
		if !strings.Contains(stripped, "Agents") {
			t.Fatalf("%dx%d lost the provider list entirely:\n%s", size[0], size[1], stripped)
		}
		for _, provider := range []string{"claude", "opencode"} {
			focusIntegration(t, m, provider)
			focused := ansi.Strip(m.View(size[0], size[1]))
			if !strings.Contains(focused, provider) {
				t.Fatalf("%dx%d clipped %s away while the cursor was on it:\n%s",
					size[0], size[1], provider, focused)
			}
		}
	}
}

func TestIntegrationActionKeysDoNotCollideWithOtherPages(t *testing.T) {
	// controlCommand maps a shortcut letter to a footer name globally, so a
	// letter this route reuses would make the footer name someone else's
	// action.
	seen := map[string]string{}
	for _, key := range []string{"r", "c", "o", "a", "i", "d", "e", "f", "g", "t"} {
		cmd, ok := controlCommand(key)
		if !ok {
			t.Fatalf("existing key %q lost its command", key)
		}
		seen[key] = cmd.Name
	}
	for _, act := range agentintegration.Actions() {
		key := integrationActionKey(act)
		if key == "" {
			t.Fatalf("%s has no shortcut", act)
		}
		if name, taken := seen[key]; taken {
			t.Fatalf("%s uses %q, which already means %q", act, key, name)
		}
		cmd, ok := controlCommand(key)
		if !ok {
			t.Fatalf("%s's key %q has no footer command", act, key)
		}
		// The visible label and the footer name have to agree, or the pill and
		// the footer describe different actions.
		if !strings.Contains(integrationActionLabel(act), cmd.Name) {
			t.Fatalf("%s is labelled %q but the footer says %q", act, integrationActionLabel(act), cmd.Name)
		}
		if !strings.HasPrefix(integrationActionLabel(act), strings.ToUpper(key)) {
			t.Fatalf("%s's label %q does not carry its key %q", act, integrationActionLabel(act), key)
		}
		seen[key] = cmd.Name
	}
}

func TestIntegrationsAreFindableInSearch(t *testing.T) {
	for _, query := range []string{"integration", "opencode", "hook"} {
		pages := SearchPages(query)
		var found bool
		for _, p := range pages {
			if p == PageAgents {
				found = true
			}
		}
		if !found {
			t.Fatalf("searching %q does not reach the Agents page: %v", query, pages)
		}
	}
}
