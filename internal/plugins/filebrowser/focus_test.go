package filebrowser

import (
	"testing"

	"github.com/marcus/sidecar/internal/plugin"
)

// twoPaneBrowser is the file browser in its ordinary shape: a visible tree and
// a preview with a file loaded in it.
func twoPaneBrowser() *Plugin {
	return &Plugin{
		treeVisible: true,
		activePane:  PaneTree,
		previewFile: "main.go",
	}
}

// With the centre open the toggle is a ring: the tree is not the wrap point
// going forward, the preview is, and shift+tab wraps at the other end.
func TestFileBrowserRingWrapsAtThePreview(t *testing.T) {
	p := twoPaneBrowser()
	if p.AtFocusCycleEnd(false) {
		t.Fatal("the tree is not the end of the forward cycle; the preview is")
	}
	if !p.AtFocusCycleEnd(true) {
		t.Fatal("the tree is where a reverse cycle wraps")
	}

	p.activePane = PanePreview
	if !p.AtFocusCycleEnd(false) {
		t.Fatal("the preview is where a forward cycle wraps")
	}
	if p.AtFocusCycleEnd(true) {
		t.Fatal("the preview is not the start of the ring")
	}
}

// Coming back from the centre lands on the window the toggle resumes at.
func TestFileBrowserFocusCycleStart(t *testing.T) {
	p := twoPaneBrowser()
	p.activePane = PanePreview
	p.FocusCycleStart(false)
	if p.activePane != PaneTree {
		t.Fatalf("forward handback focused %v, want the tree", p.activePane)
	}
	p.FocusCycleStart(true)
	if p.activePane != PanePreview {
		t.Fatalf("reverse handback focused %v, want the preview", p.activePane)
	}
}

// A pane that is not drawn is not a stop.
func TestFileBrowserRingSkipsPanesThatAreNotDrawn(t *testing.T) {
	p := twoPaneBrowser()
	p.previewFile = ""
	if !p.AtFocusCycleEnd(false) || !p.AtFocusCycleEnd(true) {
		t.Fatal("with no preview drawn the tree is the whole ring")
	}

	p = twoPaneBrowser()
	p.treeVisible = false
	p.activePane = PanePreview
	if !p.AtFocusCycleEnd(false) || !p.AtFocusCycleEnd(true) {
		t.Fatal("with the tree hidden the preview is the whole ring")
	}
}

// Sub-modes keep tab: each is typing or is a modal with a tab of its own.
func TestFileBrowserSubModesKeepTab(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*Plugin)
	}{
		{"search", func(p *Plugin) { p.searchMode = true }},
		{"content search", func(p *Plugin) { p.contentSearchMode = true }},
		{"quick open", func(p *Plugin) { p.quickOpenMode = true }},
		{"file op modal", func(p *Plugin) { p.fileOpMode = FileOpRename }},
		{"blame", func(p *Plugin) { p.blameMode = true }},
		{"info", func(p *Plugin) { p.infoMode = true }},
		{"inline edit", func(p *Plugin) { p.edit.Active = true }},
		{"line jump", func(p *Plugin) { p.lineJumpMode = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := twoPaneBrowser()
			p.activePane = PanePreview
			tc.apply(p)
			if p.AtFocusCycleEnd(false) || p.AtFocusCycleEnd(true) {
				t.Fatalf("%s offered the centre a tab stop", tc.name)
			}
		})
	}
}

// With the centre closed the shell never asks, and the plugin's own tab handler
// is the exact toggle it always was.
func TestFileBrowserTabTogglesPanesUnchanged(t *testing.T) {
	p := twoPaneBrowser()
	p.handleTreeKey("tab")
	if p.activePane != PanePreview {
		t.Fatalf("tab from the tree focused %v, want the preview", p.activePane)
	}
	p.handlePreviewKey("tab")
	if p.activePane != PaneTree {
		t.Fatalf("tab from the preview focused %v, want the tree", p.activePane)
	}
}

// Files does not hand Tab to the app's Primary-only focus ring: its own handler
// is the whole of what the key does, on a deck with a passive leaf or without
// one. Declaring plugin.PaneFocusRingHost here would replace that.
func TestFileBrowserKeepsItsOwnTab(t *testing.T) {
	if _, ok := plugin.Plugin(twoPaneBrowser()).(plugin.PaneFocusRingHost); ok {
		t.Fatal("Files hands Tab to the app; its own handler is what moves the focus")
	}
}

// With no file open, tab does nothing at all — the preview pane is not drawn,
// so the guard has nowhere to send the focus and the ring is one stop long.
func TestFileBrowserTabWithNothingOpenDoesNothing(t *testing.T) {
	p := twoPaneBrowser()
	p.previewFile = ""
	if stops := p.PaneFocusStops(); len(stops) != 1 {
		t.Fatalf("a browser with nothing open projects %d stops, want the tree alone", len(stops))
	}
	p.handleTreeKey("tab")
	if p.activePane != PaneTree {
		t.Fatalf("tab with no file open focused %v, want the tree it started on", p.activePane)
	}
}
