package configui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/agentintegration"
	"github.com/marcus/sidecar/internal/agentlifecycle"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/mouse"
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
	// The table names its columns in the CLI's own words.
	for _, want := range []string{"PROVIDER", "STATUS", "ACTIONS"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the table has no %s column:\n%s", want, view)
		}
	}
}

// TestUnsupportedProvidersCollapseToOneLine pins the difference between the two
// kinds of entry the service returns. An agent Sidecar can install for is a
// row; an agent it has surveyed and ships nothing for has no action, no files
// and no tier, so it is named in one sentence instead of given four empty
// columns.
func TestUnsupportedProvidersCollapseToOneLine(t *testing.T) {
	m, _, _ := integrationsFixture(t)
	view := openIntegrations(t, m)

	state := m.agentIntegrations()
	rows, unsupported := splitIntegrations(state.list)
	if len(rows) == 0 || len(unsupported) == 0 {
		t.Fatalf("the fixture needs both kinds of entry: %d rows, %d unsupported", len(rows), len(unsupported))
	}

	summary := ""
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Unsupported:") {
			summary = line
		}
	}
	if summary == "" {
		t.Fatalf("no summary line for the unsupported agents:\n%s", view)
	}
	for _, index := range unsupported {
		name := state.list[index].Provider
		if !strings.Contains(view, name) {
			t.Fatalf("the summary does not name %q:\n%s", name, view)
		}
		// No row, and therefore no control and no hit region either.
		id := regionIntegrationRow + itoa(index)
		for _, c := range m.controls {
			if c.id == id {
				t.Fatalf("%q was given a table row", name)
			}
		}
	}
	// Every supported provider does have one.
	for _, index := range rows {
		id := regionIntegrationRow + itoa(index)
		found := false
		for _, c := range m.controls {
			if c.id == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q has no table row", state.list[index].Provider)
		}
	}
}

func TestTheDetailBoxNamesTheExactFilesOfTheRowUnderTheCursor(t *testing.T) {
	m, svc, asset := integrationsFixture(t)
	openIntegrations(t, m)

	focusIntegration(t, m, "opencode")
	view := ansi.Strip(m.View(160, 45))
	shown := "~" + strings.TrimPrefix(asset, svc.Env.Home)
	if !strings.Contains(view, shown) {
		t.Fatalf("the detail box does not name the file an install would write (%s):\n%s", shown, view)
	}
	for _, want := range []string{"Files", "Tier", "Agent", "Report", "Gaps"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the detail box has no %s line:\n%s", want, view)
		}
	}

	// It follows the cursor rather than waiting for Enter, and shows the row
	// the cursor is actually on.
	focusIntegration(t, m, "claude")
	view = ansi.Strip(m.View(160, 45))
	if !strings.Contains(view, "sidecar agent integration status claude") {
		t.Fatalf("the detail box did not follow the cursor to claude:\n%s", view)
	}
	if strings.Contains(view, "sidecar agent integration status opencode") {
		t.Fatalf("the detail box still describes opencode:\n%s", view)
	}
}

// TestOnlyOfferedActionsAreLive is the table's honesty rule. Every action in
// the vocabulary is painted on every row so the column keeps its shape, but a
// pill the service would refuse carries no control, no shortcut and no hit
// region: it is a label saying where that verb lives, not a button that argues
// back when pressed.
func TestOnlyOfferedActionsAreLive(t *testing.T) {
	m, svc, _ := integrationsFixture(t)
	openIntegrations(t, m)
	index := focusIntegration(t, m, "opencode")

	st, err := svc.Status("opencode")
	if err != nil {
		t.Fatal(err)
	}
	offered := map[agentintegration.Action]bool{}
	for _, act := range st.Offered {
		offered[act] = true
	}
	if !offered[agentintegration.ActionInstall] || offered[agentintegration.ActionUpdate] {
		t.Fatalf("the fixture no longer exercises the distinction: %v", st.Offered)
	}

	view := ansi.Strip(m.View(160, 45))
	// Every verb is on screen, whether or not it is on offer.
	for _, act := range agentintegration.Actions() {
		if !strings.Contains(view, integrationActionLabel(act)) {
			t.Fatalf("the action column does not paint %s:\n%s", act, view)
		}
	}

	controls := map[string]control{}
	for _, c := range m.controls {
		controls[c.id] = c
	}
	regions := map[string]bool{}
	for _, r := range m.mouse.HitMap.Regions() {
		regions[r.ID] = true
	}
	for _, act := range agentintegration.Actions() {
		id := integrationActionID(index, act)
		_, declared := controls[id]
		if declared != offered[act] {
			t.Fatalf("%s: declared=%v, offered=%v", act, declared, offered[act])
		}
		if regions[id] != offered[act] {
			t.Fatalf("%s: hit region=%v, offered=%v", act, regions[id], offered[act])
		}
		// The focused row is the one that owns the letters, and only for the
		// verbs it can actually run.
		if declared && controls[id].key != integrationActionKey(act) {
			t.Fatalf("%s on the focused row carries key %q", act, controls[id].key)
		}
	}
}

// TestMovingTheCursorChangesOnlyTheHighlight is the exit gate for the table.
//
// Every row is one line whatever the cursor is doing, so the table block is
// byte-identical from every cursor position once colour is stripped, and every
// row's hit region stays in the cell it was in. The detail box below is the one
// thing that follows the cursor, which is what it is for.
func TestMovingTheCursorChangesOnlyTheHighlight(t *testing.T) {
	m, _, _ := integrationsFixture(t)
	openIntegrations(t, m)
	rows, _ := splitIntegrations(m.agentIntegrations().list)
	if len(rows) < 2 {
		t.Fatalf("the fixture has %d rows", len(rows))
	}

	type shape struct {
		lines  []string
		rects  map[string]mouse.Rect
		height int
	}
	shapes := make([]shape, 0, len(rows))
	for _, index := range rows {
		focusIntegration(t, m, m.agentIntegrations().list[index].Provider)
		view := ansi.Strip(m.View(160, 45))
		lines := strings.Split(view, "\n")
		s := shape{height: len(lines), rects: map[string]mouse.Rect{}}
		// The table block: the column header and every row under it.
		for _, line := range lines {
			if strings.Contains(line, "PROVIDER") && strings.Contains(line, "ACTIONS") {
				s.lines = append(s.lines, line)
			}
		}
		for _, r := range m.mouse.HitMap.Regions() {
			if strings.HasPrefix(r.ID, regionIntegrationRow) {
				s.rects[r.ID] = r.Rect
			}
		}
		if len(s.rects) != len(rows) {
			t.Fatalf("cursor on row %d declared %d row regions, want %d", index, len(s.rects), len(rows))
		}
		shapes = append(shapes, s)
	}

	first := shapes[0]
	for i, s := range shapes[1:] {
		if s.height != first.height {
			t.Fatalf("cursor position %d painted %d lines, position 0 painted %d", i+1, s.height, first.height)
		}
		for id, rect := range first.rects {
			if s.rects[id] != rect {
				t.Fatalf("cursor position %d moved row %s from %+v to %+v", i+1, id, rect, s.rects[id])
			}
		}
	}
}

// TestEveryOfferedActionIsClickableOnEveryRow is the other half of the exit
// gate. The old route painted a row's pills only while it had focus, so
// reaching one with the pointer meant first clicking the row, which repainted
// the page and moved the pill out from under the cursor.
func TestEveryOfferedActionIsClickableOnEveryRow(t *testing.T) {
	m, _, _ := integrationsFixture(t)
	openIntegrations(t, m)
	// The cursor stays on the first row for the whole test: nothing below may
	// depend on moving it.
	focusIntegration(t, m, m.agentIntegrations().list[0].Provider)
	m.View(160, 45)

	state := m.agentIntegrations()
	rows, _ := splitIntegrations(state.list)
	rects := map[string]mouse.Rect{}
	for _, r := range m.mouse.HitMap.Regions() {
		rects[r.ID] = r.Rect
	}
	for _, index := range rows {
		st := state.list[index]
		if len(st.Offered) == 0 {
			t.Fatalf("%s offers nothing, so the row proves nothing", st.Provider)
		}
		rowRect, ok := rects[regionIntegrationRow+itoa(index)]
		if !ok {
			t.Fatalf("%s has no row region", st.Provider)
		}
		for _, act := range st.Offered {
			rect, ok := rects[integrationActionID(index, act)]
			if !ok {
				t.Fatalf("%s has no clickable %s pill", st.Provider, act)
			}
			if rect.Y != rowRect.Y || rect.H != 1 {
				t.Fatalf("%s's %s pill is at %+v, off its own row %+v", st.Provider, act, rect, rowRect)
			}
			if rect.X < rowRect.X || rect.X+rect.W > rowRect.X+rowRect.W {
				t.Fatalf("%s's %s pill at %+v runs outside the row %+v", st.Provider, act, rect, rowRect)
			}
			// A click resolves to the pill rather than to the row under it,
			// which is what registering the pills last buys.
			if hit := m.mouse.HitMap.Test(rect.X, rect.Y); hit == nil || hit.ID != integrationActionID(index, act) {
				t.Fatalf("a click on %s's %s pill lands on %v", st.Provider, act, hit)
			}
		}
	}

	// And pressing one acts on its own provider, from a cursor that never
	// moved. opencode is the row that offers a real install in this fixture,
	// and it is not the row the cursor is on.
	target, index := agentintegration.Status{}, -1
	for _, i := range rows {
		if i != rows[0] && slices.Contains(state.list[i].Offered, agentintegration.ActionInstall) {
			target, index = state.list[i], i
			break
		}
	}
	if index < 0 {
		t.Fatal("no row below the cursor offers an install")
	}
	runIntegrationControl(t, m, integrationActionID(index, agentintegration.ActionInstall))
	if m.confirm == nil {
		t.Fatalf("clicking %s's install pill raised nothing", target.Provider)
	}
	if !strings.Contains(ansi.Strip(m.View(160, 45)), target.Provider) {
		t.Fatalf("the confirmation is not about %s", target.Provider)
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
		// One anchoring is enough now. The row cursor is a position among the
		// cursor-visitable controls of the previous frame, and the table
		// declares exactly one of those per row whatever the cursor is doing,
		// so the positions do not move under it. While the focused row grew its
		// own pills, and those pills were cursor stops, focusing a row shifted
		// every row below it and a single anchoring landed on the wrong
		// provider.
		m.focusControlByID(regionIntegrationRow + itoa(i))
		m.View(160, 45)
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
		// Every provider is reachable: put the cursor on it and its row is
		// painted, with a hit region on the line it was painted on. A pane too
		// short to hold every row at once windows them (see
		// windowIntegrationRows), so what has to hold is that the cursor's own
		// row is always on screen, never that all of them are -- a region over
		// a row the window scrolled away would be a click on a provider the
		// user cannot see.
		//
		// The name itself is checked by identity rather than by text: a
		// 60-column pane spends its last columns on the action pills and
		// abbreviates the name, which is the trade the column plan makes
		// deliberately.
		state := m.agentIntegrations()
		rows, _ := splitIntegrations(state.list)
		for _, index := range rows {
			focusIntegrationRow(t, m, index, size[0], size[1])
			view = m.View(size[0], size[1])
			painted := strings.Split(view, "\n")
			regions := map[string]mouse.Rect{}
			for _, r := range m.mouse.HitMap.Regions() {
				regions[r.ID] = r.Rect
			}
			id := regionIntegrationRow + itoa(index)
			rect, ok := regions[id]
			if !ok {
				t.Fatalf("%dx%d lost the row for %s while the cursor was on it", size[0], size[1], state.list[index].Provider)
			}
			if rect.X+rect.W > size[0] {
				t.Fatalf("%dx%d: %s's row runs to column %d", size[0], size[1], state.list[index].Provider, rect.X+rect.W)
			}
			// The pane paints its content two lines into the frame, so a
			// region past the last painted line is a target over nothing.
			if rect.Y+rect.H > len(painted) {
				t.Fatalf("%dx%d: %s's row region ends at line %d of %d painted", size[0], size[1], state.list[index].Provider, rect.Y+rect.H, len(painted))
			}
		}
	}
}

// TestARowScrolledOutOfTheWindowKeepsNoLiveTarget is the other half of the
// window's contract. The rows give way before the detail box does, and a row
// the window hid must take its pills with it: a hit region over a line that is
// no longer painted is a click that installs for a provider the user is not
// looking at.
func TestARowScrolledOutOfTheWindowKeepsNoLiveTarget(t *testing.T) {
	m, _, _ := integrationsFixture(t)
	openIntegrations(t, m)
	fakeIntegrations(t, m, 17)

	const width, height = 80, 30
	state := m.agentIntegrations()
	rows, _ := splitIntegrations(state.list)

	focusIntegrationRow(t, m, rows[0], width, height)
	if state.rowScroll != 0 {
		t.Fatalf("the window starts at %d with the cursor on the first row", state.rowScroll)
	}
	first := integrationRegionRows(m)
	if len(first) == 0 || len(first) >= len(rows) {
		t.Fatalf("%d of %d rows are live at %dx%d; the window painted nothing or everything", len(first), len(rows), width, height)
	}
	if !first[rows[0]] {
		t.Fatal("the cursor's own row is not live")
	}
	// The detail box is what the rows gave way for, so it has to be on screen.
	text := strings.Join(detailColumn(t, m, width, height), "\n")
	for _, want := range []string{"Files", "Tier", "Report", "Gaps"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the window hid rows and still lost the detail box's %s line:\n%s", want, text)
		}
	}

	// Walking to the last row scrolls the window, and everything it left
	// behind loses its row region and its pills with it.
	last := rows[len(rows)-1]
	focusIntegrationRow(t, m, last, width, height)
	if state.rowScroll == 0 {
		t.Fatal("the window did not follow the cursor to the last row")
	}
	live := integrationRegionRows(m)
	if !live[last] {
		t.Fatal("the last row is not live with the cursor on it")
	}
	regions := map[string]bool{}
	for _, r := range m.mouse.HitMap.Regions() {
		regions[r.ID] = true
	}
	for _, index := range rows {
		if live[index] {
			continue
		}
		for _, act := range state.list[index].Offered {
			if regions[integrationActionID(index, act)] {
				t.Fatalf("row %d is off screen but its %s pill is still clickable", index, act)
			}
		}
	}
	if !first[rows[0]] || live[rows[0]] {
		t.Fatal("the first row is still live after the window scrolled past it")
	}
}

// focusIntegrationRow puts the keyboard on one provider's row at a given pane
// size. The cursor is driven through focus rather than by writing the route's
// own cursor field, because the route reads its cursor back off the focused
// control on every frame -- which is what keeps the highlight and the detail
// box naming the same provider.
func focusIntegrationRow(t *testing.T, m *Model, index, width, height int) {
	t.Helper()
	m.detailFocus = true
	m.View(width, height)
	m.focusControlByID(regionIntegrationRow + itoa(index))
	m.View(width, height)
	if got := m.agentIntegrations().cursor; got != index {
		t.Fatalf("the cursor landed on row %d, want %d", got, index)
	}
}

// integrationRegionRows is the set of provider indices with a live row region.
func integrationRegionRows(m *Model) map[int]bool {
	out := map[int]bool{}
	for _, r := range m.mouse.HitMap.Regions() {
		if !strings.HasPrefix(r.ID, regionIntegrationRow) {
			continue
		}
		index, err := strconv.Atoi(strings.TrimPrefix(r.ID, regionIntegrationRow))
		if err != nil {
			continue
		}
		out[index] = true
	}
	return out
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

// detailColumn is the detail pane's own text, cut out of a rendered frame. The
// sidebar sits to its left on every line, so a test that read whole lines would
// be reading two panes at once.
func detailColumn(t *testing.T, m *Model, width, height int) []string {
	t.Helper()
	view := ansi.Strip(m.View(width, height))
	var out []string
	for _, line := range strings.Split(view, "\n") {
		runes := []rune(line)
		start := m.sidebarWidth + 2
		end := len(runes) - 2
		if start >= end {
			continue
		}
		out = append(out, strings.TrimRight(string(runes[start:end]), " "))
	}
	return out
}

// fakeIntegrations builds a discovered list of n installable providers, plus
// one Sidecar ships nothing for, and hands it to the route through the same
// message discovery delivers.
//
// It goes through the message rather than through a stand-in Service because
// Service.List reads the global capability registry, so a fake service would
// still return the machine's real provider set. The message is the seam the
// route actually consumes, and the shapes below are the shapes it has to paint.
func fakeIntegrations(t *testing.T, m *Model, n int) {
	t.Helper()
	statuses := []agentlifecycle.IntegrationStatus{
		agentlifecycle.StatusCurrent,
		agentlifecycle.StatusOutdated,
		agentlifecycle.StatusNeedsRepair,
		agentlifecycle.StatusNotInstalled,
		agentlifecycle.StatusProviderMissing,
	}
	offers := [][]agentintegration.Action{
		{agentintegration.ActionUninstall},
		{agentintegration.ActionUpdate, agentintegration.ActionUninstall},
		{agentintegration.ActionRepair, agentintegration.ActionUninstall},
		{agentintegration.ActionInstall, agentintegration.ActionUninstall},
		{agentintegration.ActionUninstall},
	}
	list := make([]agentintegration.Status, 0, n+1)
	for i := 0; i < n; i++ {
		list = append(list, agentintegration.Status{
			IntegrationReport: agentlifecycle.IntegrationReport{
				Provider:      fmt.Sprintf("agent%02d", i),
				Source:        fmt.Sprintf("agent%02d", i),
				Status:        statuses[i%len(statuses)],
				EffectiveTier: agentlifecycle.Tier("advisory"),
				TargetPaths:   []string{fmt.Sprintf("/home/someone/.config/agent%02d/plugin/sidecar-lifecycle.js", i)},
				// A long gap list on every row, so a route that counted them
				// anywhere would be caught.
				KnownGaps: []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"},
			},
			Offered: offers[i%len(offers)],
		})
	}
	list = append(list, agentintegration.Status{
		IntegrationReport: agentlifecycle.IntegrationReport{
			Provider: "surveyed", Status: agentlifecycle.StatusUnsupported,
			EffectiveTier: agentlifecycle.Tier("screen-fallback"), KnownGaps: []string{"a", "b", "c"},
		},
	})
	feed(t, m, agentIntegrationsMsg{List: list})
	if got := len(m.agentIntegrations().list); got != n+1 {
		t.Fatalf("the route took %d providers, want %d", got, n+1)
	}
}

// TestTheTableHasOneShapeForThreeSevenAndSeventeenProviders is the plan's
// third exit-gate clause. A row is one line and rows are consecutive, whatever
// there are three of or seventeen of.
func TestTheTableHasOneShapeForThreeSevenAndSeventeenProviders(t *testing.T) {
	for _, count := range []int{3, 7, 17} {
		m, _, _ := integrationsFixture(t)
		openIntegrations(t, m)
		fakeIntegrations(t, m, count)
		m.View(160, 45)

		rects := map[int]mouse.Rect{}
		for _, r := range m.mouse.HitMap.Regions() {
			if !strings.HasPrefix(r.ID, regionIntegrationRow) {
				continue
			}
			index, err := strconv.Atoi(strings.TrimPrefix(r.ID, regionIntegrationRow))
			if err != nil {
				t.Fatalf("%d providers: unreadable row id %q", count, r.ID)
			}
			rects[index] = r.Rect
		}
		if len(rects) != count {
			t.Fatalf("%d providers painted %d rows", count, len(rects))
		}
		for i := 0; i < count; i++ {
			rect, ok := rects[i]
			if !ok {
				t.Fatalf("%d providers: row %d is missing", count, i)
			}
			if rect.H != 1 {
				t.Fatalf("%d providers: row %d is %d lines tall", count, i, rect.H)
			}
			if rect.W != rects[0].W || rect.X != rects[0].X {
				t.Fatalf("%d providers: row %d is %+v, row 0 is %+v", count, i, rect, rects[0])
			}
			if i > 0 && rect.Y != rects[i-1].Y+1 {
				t.Fatalf("%d providers: row %d sits at y=%d, row %d at y=%d", count, i, rect.Y, i-1, rects[i-1].Y)
			}
		}
		// The rest of the page is the same page: the same column header, the
		// same detail fields, the same closing note.
		text := strings.Join(detailColumn(t, m, 160, 45), "\n")
		for _, want := range []string{"PROVIDER", "ACTIONS", "Files", "Gaps", "Unsupported: surveyed", "--dry-run"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%d providers: the page lost %q:\n%s", count, want, text)
			}
		}
	}
}

// TestTheRouteNeverCountsKnownGaps pins a decision from the plan. The gaps
// recorded for a provider are gaps in that provider's own hook contract, and a
// number beside its name reads as that many faults in Sidecar. The command that
// lists them is the only useful thing to say, and the route says it once per
// focused provider.
func TestTheRouteNeverCountsKnownGaps(t *testing.T) {
	m, _, _ := integrationsFixture(t)
	openIntegrations(t, m)
	fakeIntegrations(t, m, 5)
	for _, size := range [][2]int{{80, 40}, {120, 45}, {200, 50}} {
		text := strings.Join(detailColumn(t, m, size[0], size[1]), "\n")
		for _, forbidden := range []string{"known gap", "known gaps", "10 gaps"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%dx%d: the route says %q:\n%s", size[0], size[1], forbidden, text)
			}
		}
		if !strings.Contains(text, "sidecar agent integration status agent00") {
			t.Fatalf("%dx%d: the route does not name the command that lists them:\n%s", size[0], size[1], text)
		}
	}

	// The same on the real machine's own claude entry, which is where the count
	// used to be printed.
	m2, _, _ := integrationsFixture(t)
	openIntegrations(t, m2)
	focusIntegration(t, m2, "claude")
	text := strings.Join(detailColumn(t, m2, 160, 45), "\n")
	if strings.Contains(text, "known gap") {
		t.Fatalf("the claude row still counts its gaps:\n%s", text)
	}
}

// TestTheTableFitsEighty120And200 checks the three widths the column plan is
// written against: the narrow one where the pills fall back to their letters,
// the ordinary one where they carry their names, and the wide one where the
// tier and files columns are affordable.
func TestTheTableFitsEighty120And200(t *testing.T) {
	m, _, _ := integrationsFixture(t)
	openIntegrations(t, m)
	fakeIntegrations(t, m, 7)

	for _, width := range []int{80, 120, 200} {
		view := m.View(width, 45)
		for i, line := range strings.Split(view, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Fatalf("width %d: line %d is %d columns wide", width, i, w)
			}
		}
		lines := detailColumn(t, m, width, 45)
		text := strings.Join(lines, "\n")
		// The action column survives every width; it is the one thing the
		// layout never gives up.
		if !strings.Contains(text, "ACTIONS") {
			t.Fatalf("width %d lost the action column:\n%s", width, text)
		}
		for _, want := range []string{"PROVIDER", "STATUS", "Files", "Gaps"} {
			if !strings.Contains(text, want) {
				t.Fatalf("width %d lost %q:\n%s", width, want, text)
			}
		}
		// Nothing is clipped mid-word at the right edge.
		for _, line := range lines {
			if strings.HasSuffix(line, "…") && !strings.Contains(line, "  ") {
				t.Fatalf("width %d clipped a line at its right edge: %q", width, line)
			}
		}
		// Every row still carries every offered action.
		state := m.agentIntegrations()
		regions := map[string]bool{}
		for _, r := range m.mouse.HitMap.Regions() {
			regions[r.ID] = true
		}
		rows, _ := splitIntegrations(state.list)
		for _, index := range rows {
			for _, act := range state.list[index].Offered {
				if !regions[integrationActionID(index, act)] {
					t.Fatalf("width %d: %s's %s pill is not clickable", width, state.list[index].Provider, act)
				}
			}
		}
	}

	// The narrow width buys the action column with the pill labels, and says so
	// in a legend rather than leaving four letters unexplained.
	narrow := strings.Join(detailColumn(t, m, 80, 45), "\n")
	if !strings.Contains(narrow, integrationActionLabel(agentintegration.ActionInstall)) {
		t.Fatalf("80 columns has no legend for the compact pills:\n%s", narrow)
	}
	// The wide one can afford the tier and files columns.
	wide := strings.Join(detailColumn(t, m, 200, 45), "\n")
	for _, want := range []string{"TIER", "FILES"} {
		if !strings.Contains(wide, want) {
			t.Fatalf("200 columns does not show the %s column:\n%s", want, wide)
		}
	}
}

// TestTheIntroWrapsRatherThanTruncating covers the paragraph at the top of the
// route, which used to lose its last words to the pane's right edge.
func TestTheIntroWrapsRatherThanTruncating(t *testing.T) {
	const intro = "A small addition to an agent's own configuration, so Sidecar learns what that agent is doing from its own lifecycle events instead of its screen."
	m, _, _ := integrationsFixture(t)
	openIntegrations(t, m)
	for _, width := range []int{60, 80, 120, 200} {
		lines := detailColumn(t, m, width, 45)
		title := 0
		for i, line := range lines {
			if strings.Contains(line, "Back to Agents") {
				title = i
				break
			}
		}
		var paragraph []string
		for _, line := range lines[title+1:] {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				if len(paragraph) > 0 {
					break
				}
				continue
			}
			paragraph = append(paragraph, trimmed)
		}
		if got := strings.Join(paragraph, " "); got != intro {
			t.Fatalf("width %d wrapped the intro to %q", width, got)
		}
	}
}

// TestTheFooterNamesTheActionsTheCursorCanRun closes the loop between the pills
// and the footer. The action column paints four verbs on every row, but only
// the ones the service offers on the focused row carry a shortcut, so only
// those may be advertised: a hint for a key that refuses is worse than no hint.
func TestTheFooterNamesTheActionsTheCursorCanRun(t *testing.T) {
	m, svc, _ := integrationsFixture(t)
	openIntegrations(t, m)
	focusIntegration(t, m, "opencode")
	m.View(160, 45)

	st, err := svc.Status("opencode")
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, c := range m.Commands() {
		named[c.Name] = true
	}
	want := map[agentintegration.Action]bool{}
	for _, act := range st.Offered {
		want[act] = true
	}
	for _, act := range agentintegration.Actions() {
		cmd, ok := controlCommand(integrationActionKey(act))
		if !ok {
			t.Fatalf("%s has no footer command", act)
		}
		if named[cmd.Name] != want[act] {
			t.Fatalf("the footer names %q=%v while the service offers it=%v", cmd.Name, named[cmd.Name], want[act])
		}
	}
}

// TestEveryPillRegionLandsOnThePillItWasPaintedFrom is the hit-region check the
// width test did not make. That one proves a region exists at 80, 120 and 200;
// this one proves the region is over the pill.
//
// The action column's regions are not measured from the painted line: they are
// laid out from the right edge of the row, one pill width at a time, on the
// assumption that a pill is its text plus the two columns styles.Button pads it
// with. Nothing checked that assumption, so a change to the chip's padding, or
// to the gap the pills are joined with, would move every action target off its
// pill silently -- and the compact form, where a pill is three columns wide, is
// where a one-column drift stops overlapping the pill at all.
func TestEveryPillRegionLandsOnThePillItWasPaintedFrom(t *testing.T) {
	m, _, _ := integrationsFixture(t)
	openIntegrations(t, m)
	fakeIntegrations(t, m, 7)

	for _, width := range []int{80, 120, 200} {
		const height = 60
		lines := detailColumn(t, m, width, height)
		state := m.agentIntegrations()
		rows, _ := splitIntegrations(state.list)
		table := layoutIntegrationTable(integrationRows(state.list, rows), paneContentWidth(width-m.sidebarWidth))
		rects := map[string]mouse.Rect{}
		for _, r := range m.mouse.HitMap.Regions() {
			rects[r.ID] = r.Rect
		}
		for _, index := range rows {
			st := state.list[index]
			rowRect, ok := rects[regionIntegrationRow+itoa(index)]
			if !ok {
				t.Fatalf("width %d: %s has no row region", width, st.Provider)
			}
			line := ""
			for _, candidate := range lines {
				if strings.HasPrefix(candidate, strings.Repeat(" ", RowIndent)+st.Provider) {
					line = candidate
					break
				}
			}
			if line == "" {
				t.Fatalf("width %d: %s's row is not painted", width, st.Provider)
			}
			for _, act := range st.Offered {
				rect, ok := rects[integrationActionID(index, act)]
				if !ok {
					t.Fatalf("width %d: %s's %s pill has no region", width, st.Provider, act)
				}
				text := integrationPillText(act, table.compact)
				// One column in from the region's left edge is where the pill's
				// own padding ends and its text begins.
				at := rect.X - rowRect.X + 1
				runes := []rune(line)
				if at < 0 || at+len([]rune(text)) > len(runes) {
					t.Fatalf("width %d: %s's %s region starts at column %d of a %d-column row", width, st.Provider, act, at, len(runes))
				}
				if got := string(runes[at : at+len([]rune(text))]); got != text {
					t.Fatalf("width %d: %s's %s region covers %q, not the pill %q\nrow: %q", width, st.Provider, act, got, text, line)
				}
				if rect.W != ansi.StringWidth(text)+2 {
					t.Fatalf("width %d: %s's %s region is %d columns for a %d-column pill", width, st.Provider, act, rect.W, ansi.StringWidth(text)+2)
				}
			}
		}
	}
}
