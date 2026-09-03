package workspace

import (
	"testing"

	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/inlineedit"
	"github.com/marcus/sidecar/internal/panesearch"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/terminalperf"
	"github.com/marcus/sidecar/internal/tty"
)

func TestProjectPreviewCacheInvalidatesVisibleInputs(t *testing.T) {
	tests := map[string]func(*projectActiveSessionFixture){
		"terminal output revision": func(f *projectActiveSessionFixture) {
			f.buffer.Update(f.terminal.Frame(2))
		},
		"document visual revision": func(f *projectActiveSessionFixture) {
			f.viewer.ToggleRenderMode()
		},
		"document resolution generation": func(f *projectActiveSessionFixture) {
			candidate := contentlink.Pending{Kind: contentlink.KindFile, Raw: "new-file.md"}
			f.p.ensureDocLinkResolution().PutForRoot(f.root, candidate,
				contentlink.Ref{Kind: contentlink.KindFile, Value: "new-file.md"}, true)
		},
		"layout geometry": func(f *projectActiveSessionFixture) {
			f.p.paneRoot.Split.Ratio = 60
		},
		"zoom": func(f *projectActiveSessionFixture) {
			f.p.paneZoom.Set(f.p.paneLayoutModalScope(), f.p.paneRoot, 2)
		},
		"focus chrome": func(f *projectActiveSessionFixture) {
			f.p.paneFocus = 2
		},
		"terminal background": func(f *projectActiveSessionFixture) {
			f.p.backgrounds = tty.BackgroundMode("always")
		},
		"terminal selection": func(f *projectActiveSessionFixture) {
			f.p.selection.Active = true
			f.p.selection.Start.Line, f.p.selection.Start.Col = 1, 1
			f.p.selection.End.Line, f.p.selection.End.Col = 1, 4
		},
		"terminal search": func(f *projectActiveSessionFixture) {
			f.p.terminalSearch.SetQuery("README")
			f.p.terminalSearch.SourceKey = "fixture"
			f.p.terminalSearch.Generation++
		},
		"header hover": func(f *projectActiveSessionFixture) {
			f.p.hoverPaneClose = 2
		},
		"surface update revision": func(f *projectActiveSessionFixture) {
			f.p.bumpProjectPreviewRevision()
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			root, terminal := prepareProjectActiveSessionRoot(t)
			fixture := newProjectActiveSessionFixture(t, root, terminal)
			_ = fixture.p.View(220, 58)
			counters := &terminalperf.Counters{}
			restore := terminalperf.Install(counters)
			t.Cleanup(restore)

			mutate(&fixture)
			_ = fixture.p.View(220, 58)
			snapshot := counters.Snapshot()
			if snapshot.ProjectPreviewComposes != 1 || snapshot.ProjectPreviewCacheHits != 0 {
				t.Fatalf("invalidation counters = %+v, want one fresh compose", snapshot)
			}
			_ = fixture.p.View(220, 58)
			snapshot = counters.Snapshot()
			if snapshot.ProjectPreviewComposes != 1 || snapshot.ProjectPreviewCacheHits != 1 {
				t.Fatalf("settled counters = %+v, want subsequent cache hit", snapshot)
			}
		})
	}
}

func TestProjectPreviewRevisionExcludesOnlySidebarPulse(t *testing.T) {
	p := New()
	before := p.projectPreviewRevision
	_, _ = p.Update(activityAnimationTickMsg{generation: p.activityAnimationGeneration})
	if p.projectPreviewRevision != before {
		t.Fatalf("sidebar-only pulse advanced preview revision %d -> %d", before, p.projectPreviewRevision)
	}
	_, _ = p.Update(struct{}{})
	if p.projectPreviewRevision != before+1 {
		t.Fatalf("ordinary update revision = %d, want %d", p.projectPreviewRevision, before+1)
	}
}

func TestProjectPreviewCacheInvalidatesSizeAndTheme(t *testing.T) {
	root, terminal := prepareProjectActiveSessionRoot(t)
	fixture := newProjectActiveSessionFixture(t, root, terminal)
	_ = fixture.p.View(220, 58)
	counters := &terminalperf.Counters{}
	restore := terminalperf.Install(counters)
	t.Cleanup(restore)

	_ = fixture.p.View(221, 58)
	if snapshot := counters.Snapshot(); snapshot.ProjectPreviewComposes != 1 || snapshot.ProjectPreviewCacheHits != 0 {
		t.Fatalf("resize counters = %+v, want fresh compose", snapshot)
	}

	originalTheme := styles.GetCurrentTheme()
	originalThemeName := styles.GetCurrentThemeName()
	styles.ApplyTheme("dracula")
	t.Cleanup(func() {
		// Restore both halves of theme state. The registered name preserves the
		// caller's selection, while the exact value preserves any active palette
		// overrides that are not present in the registry entry.
		styles.ApplyTheme(originalThemeName)
		styles.ApplyThemeColors(originalTheme)
	})
	_ = fixture.p.View(221, 58)
	if snapshot := counters.Snapshot(); snapshot.ProjectPreviewComposes != 2 || snapshot.ProjectPreviewCacheHits != 0 {
		t.Fatalf("theme counters = %+v, want second fresh compose", snapshot)
	}
}

func TestProjectPreviewCacheBypassesMutableAndUnsupportedLeaves(t *testing.T) {
	root, terminal := prepareProjectActiveSessionRoot(t)
	fixture := newProjectActiveSessionFixture(t, root, terminal)
	canvas := fixture.p.previewLayoutBox(220, 58)
	layout, ok := LayoutPaneTreeWithZoom(fixture.p.paneRoot, canvas, paneTreeFloors(), fixture.p.paneFocus, 0)
	if !ok {
		t.Fatal("fixture pane tree did not lay out")
	}
	doc := fixture.p.docs[2]

	doc.mode = &panesearch.Mode{}
	if _, ok := fixture.p.projectPreviewKeyFor(layout, 220, 58); ok {
		t.Fatal("document search overlay was cacheable")
	}
	doc.mode = nil
	doc.edit = &inlineedit.Session{Active: true}
	if _, ok := fixture.p.projectPreviewKeyFor(layout, 220, 58); ok {
		t.Fatal("inline document editor was cacheable")
	}
	doc.edit = nil

	fixture.p.paneRoot.Split.B.Kind = PaneIssue
	if _, ok := fixture.p.projectPreviewKeyFor(layout, 220, 58); ok {
		t.Fatal("unsupported passive pane was cacheable")
	}
}

func TestProjectPreviewCacheInvalidatesDocumentTabIdentity(t *testing.T) {
	root, terminal := prepareProjectActiveSessionRoot(t)
	fixture := newProjectActiveSessionFixture(t, root, terminal)
	_ = fixture.p.View(220, 58)

	second := docview.New(nil)
	loaded, ok := second.Load(3, root, "README.md", 0, fixture.p.ctx.Epoch)().(docview.LoadedMsg)
	if !ok || !second.SetResult(loaded) {
		t.Fatal("second document did not load")
	}
	fixture.p.docs[2].tabs.Append(second)
	counters := &terminalperf.Counters{}
	restore := terminalperf.Install(counters)
	t.Cleanup(restore)
	_ = fixture.p.View(220, 58)
	if snapshot := counters.Snapshot(); snapshot.ProjectPreviewComposes != 1 || snapshot.ProjectPreviewCacheHits != 0 {
		t.Fatalf("tab counters = %+v, want fresh compose", snapshot)
	}
}
