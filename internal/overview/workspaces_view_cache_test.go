package overview

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/terminalperf"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/workspaceinventory"
	"github.com/marcus/sidecar/internal/workspacelist"
)

func globalListCacheFixture(t testing.TB) (*Model, *tty.OutputBuffer) {
	t.Helper()
	m, _, buffer := globalTerminalFixture(t)
	items := make([]workspacelist.Item, 0, 36)
	for i := 0; i < 36; i++ {
		id := fmt.Sprintf("synthetic-%02d", i)
		workspace := workspaceinventory.Workspace{
			ID: id, ProjectKey: "synthetic-project", ProjectName: "Fixture",
			Kind: workspaceinventory.KindWorktree, Name: fmt.Sprintf("Workspace %02d", i),
			Path: t.TempDir(), Live: i == 0, Plain: true,
		}
		m.catalog[id] = workspace
		items = append(items, listItem(workspace.Item(), workspace.ProjectName, 0, false))
	}
	m.workspaces.SetItems(items)
	m.workspaces.SelectID("synthetic-00")
	m.preview.workspaceID = "synthetic-00"
	return m, buffer
}

func renderCounts(t testing.TB, m *Model, mutate func()) terminalperf.Snapshot {
	t.Helper()
	_ = m.WorkspacesView(200, 50)
	counters := &terminalperf.Counters{}
	restore := terminalperf.Install(counters)
	defer restore()
	mutate()
	_ = m.WorkspacesView(200, 50)
	return counters.Snapshot()
}

func TestGlobalTerminalOnlyFramesReuseWorkspaceList(t *testing.T) {
	m, fixture, buffer := globalTerminalFixture(t)
	_ = m.WorkspacesView(200, 50)
	counters := &terminalperf.Counters{}
	restore := terminalperf.Install(counters)
	defer restore()

	for i := 1; i <= 4; i++ {
		buffer.Update(fixture.Frame(i))
		_ = m.WorkspacesView(200, 50)
	}
	snapshot := counters.Snapshot()
	if snapshot.GlobalWorkspaceListRendered != 0 || snapshot.GlobalWorkspacePreviewRendered != 4 {
		t.Fatalf("terminal-only render counters = %+v, want list=0 preview=4", snapshot)
	}
}

func TestGlobalWorkspaceListCacheInvalidators(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Model)
	}{
		{"selection", func(m *Model) { m.workspaces.SelectID("synthetic-01") }},
		{"list scroll", func(m *Model) { m.workspaces.SetScrollViewport(3) }},
		{"outer divider geometry", func(m *Model) { m.sidebarWidth += 5 }},
		{"preview focus", func(m *Model) { m.preview.focus = focusPreview }},
		{"sort", func(m *Model) { m.workspaces.SetSort(workspacelist.SortName) }},
		{"filter query", func(m *Model) { m.workspaces.Filter().SetQuery("03"); m.workspaces.Reproject() }},
		{"filter focus", func(m *Model) { m.workspaces.FocusFilter() }},
		{"pulse", func(m *Model) { m.pulseFrame++; m.workspaces.SetPulseFrame(m.pulseFrame) }},
		{"scrollbar hover", func(m *Model) { m.wsBar.hover = true }},
		{"scrollbar drag", func(m *Model) {
			bar := m.wsBar.bar
			m.wsBar.gesture.Press(bar, m.wsBar.originY+bar.TrackTop, m.wsBar.originY+bar.TrackTop, false, m.workspaces.ScrollOffset())
		}},
		{"inventory revision", func(m *Model) { m.workspaceListDataRevision++ }},
		{"theme revision", func(m *Model) { m.workspaceListThemeRevision++ }},
		{"view overlay", func(m *Model) { m.openViewFlyout() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := globalListCacheFixture(t)
			snapshot := renderCounts(t, m, func() { tt.mutate(m) })
			if snapshot.GlobalWorkspaceListRendered != 1 {
				t.Fatalf("counters = %+v, want one list render", snapshot)
			}
		})
	}
}

func TestGlobalWorkspaceListCacheGeometryTransitions(t *testing.T) {
	m, _ := globalListCacheFixture(t)
	_ = m.WorkspacesView(200, 50)
	counters := &terminalperf.Counters{}
	restore := terminalperf.Install(counters)
	defer restore()

	for _, size := range [][2]int{{201, 50}, {201, 51}, {70, 40}, {200, 50}} {
		before := counters.Snapshot().GlobalWorkspaceListRendered
		_ = m.WorkspacesView(size[0], size[1])
		if got := counters.Snapshot().GlobalWorkspaceListRendered - before; got != 1 {
			t.Fatalf("size %dx%d list renders = %d, want 1", size[0], size[1], got)
		}
	}
}

func TestWorkspaceOverlayMaskCoversEveryListOverlay(t *testing.T) {
	m := &Model{}
	tests := []struct {
		name string
		set  func()
		want uint8
	}{
		{"rename", func() { m.renameOpen = true }, workspaceOverlayRename},
		{"create", func() { m.createOpen = true }, workspaceOverlayCreate},
		{"delete", func() { m.deleteOpen = true }, workspaceOverlayDelete},
		{"view", func() { m.viewFlyoutOpen = true }, workspaceOverlayView},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.renameOpen, m.createOpen, m.deleteOpen, m.viewFlyoutOpen = false, false, false, false
			tt.set()
			if got := m.workspaceOverlayMask(); got != tt.want {
				t.Fatalf("mask = %08b, want %08b", got, tt.want)
			}
		})
	}
}

func TestGlobalWorkspacePinInvalidatesListCache(t *testing.T) {
	m, _ := globalListCacheFixture(t)
	oldSave := savePinnedWorkspaceIDs
	savePinnedWorkspaceIDs = func([]string) error { return nil }
	t.Cleanup(func() { savePinnedWorkspaceIDs = oldSave })
	snapshot := renderCounts(t, m, func() { _ = m.toggleWorkspacePin() })
	if snapshot.GlobalWorkspaceListRendered != 1 {
		t.Fatalf("pin counters = %+v, want one list render", snapshot)
	}
}

func TestGlobalWorkspaceListCacheInvalidatesOnSidebarRoundTripAndThemeMessage(t *testing.T) {
	m, _ := globalListCacheFixture(t)
	_ = m.WorkspacesView(200, 50)
	counters := &terminalperf.Counters{}
	restore := terminalperf.Install(counters)
	defer restore()

	_ = m.toggleWorkspaceSidebar()
	_ = m.WorkspacesView(200, 50)
	_ = m.toggleWorkspaceSidebar()
	_ = m.WorkspacesView(200, 50)
	if got := counters.Snapshot().GlobalWorkspaceListRendered; got != 1 {
		t.Fatalf("sidebar round-trip list renders = %d, want 1 on restore", got)
	}

	before := counters.Snapshot().GlobalWorkspaceListRendered
	_ = m.Update(msg.ThemeChangedMsg{})
	_ = m.WorkspacesView(200, 50)
	if got := counters.Snapshot().GlobalWorkspaceListRendered - before; got != 1 {
		t.Fatalf("theme message list renders = %d, want 1", got)
	}
}

func TestGlobalWorkspaceListCacheIgnoresPreviewOnlyState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Model, *tty.OutputBuffer)
	}{
		{"terminal output", func(_ *Model, b *tty.OutputBuffer) { b.Update("changed terminal output") }},
		{"preview scroll", func(m *Model, _ *tty.OutputBuffer) { m.previewTerminalLeaf().Scroll++ }},
		{"terminal scrollbar hover", func(m *Model, _ *tty.OutputBuffer) { m.hoverTermBar = !m.hoverTermBar }},
		{"preview divider hover", func(m *Model, _ *tty.OutputBuffer) {
			m.hoverHandleRegion = previewPaneDividerKind
			m.hoverHandleSplit = 7
		}},
		{"preview tab close hover", func(m *Model, _ *tty.OutputBuffer) { m.previewCloseHover = true; m.hoverPreviewClose = 2 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, buffer := globalListCacheFixture(t)
			snapshot := renderCounts(t, m, func() { tt.mutate(m, buffer) })
			if snapshot.GlobalWorkspaceListRendered != 0 || snapshot.GlobalWorkspacePreviewRendered != 1 {
				t.Fatalf("counters = %+v, want list=0 preview=1", snapshot)
			}
		})
	}
}

func TestGlobalWorkspaceListCacheReplaysExactRegionOrder(t *testing.T) {
	m, _ := globalListCacheFixture(t)
	_ = m.WorkspacesView(200, 24)
	want := m.workspacesMouse.HitMap.Regions()
	if len(want) == 0 || want[len(want)-1].ID != workspacesDividerRegion {
		t.Fatalf("initial region order does not end with outer divider: %#v", want)
	}

	m.workspacesMouse.HitMap.AddRect("stale", 0, 0, 1, 1, nil)
	counters := &terminalperf.Counters{}
	restore := terminalperf.Install(counters)
	defer restore()
	_ = m.WorkspacesView(200, 24)
	got := m.workspacesMouse.HitMap.Regions()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cache-hit region order changed\n got: %#v\nwant: %#v", got, want)
	}
	if counters.Snapshot().GlobalWorkspaceListRendered != 0 {
		t.Fatalf("region replay rebuilt list: %+v", counters.Snapshot())
	}
}

// M4d-a: the caret is part of the query row's picture, not a detail of the
// text. The list cache keyed on the query and on whether the row had focus but
// not on where the caret was, so `home`, `alt+←` and every other caret move
// left the ▌ drawn where it used to be — and the next keystroke went in
// somewhere the user could not see.
func TestGlobalWorkspaceListCacheInvalidatesOnACaretMove(t *testing.T) {
	m, _ := globalListCacheFixture(t)
	m.workspaces.FocusFilter()
	m.workspaces.Filter().SetQuery("space")
	m.workspaces.Reproject()
	if got := ansi.Strip(m.WorkspacesView(200, 50)); !strings.Contains(got, "/ space▌") {
		t.Fatalf("the caret does not start at the end of the query:\n%s", got)
	}
	if result := m.workspaces.FilterKey(tea.KeyPressMsg{Code: tea.KeyHome}); result != workspacelist.KeyHandled {
		t.Fatalf("home in the query = %v, want the field to have taken it", result)
	}
	if got := ansi.Strip(m.WorkspacesView(200, 50)); !strings.Contains(got, "/ ▌space") {
		t.Fatalf("the caret move did not redraw the query row:\n%s", got)
	}
}
