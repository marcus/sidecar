package workspace

import (
	"strconv"

	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/paneframe"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/terminalperf"
	"github.com/marcus/sidecar/internal/tty"
)

// projectPreviewCache stores only paneframe.Compose's immutable bytes and the
// small immutable metadata produced while those bytes were drawn. Mutable hit
// maps, pane origins, and the current Layout are deliberately never retained.
type projectPreviewCache struct {
	valid   bool
	key     projectPreviewKey
	output  string
	actions []headerChipPlacement
	start   startAgentButtonHit
}

type projectPreviewKey struct {
	width, height int
	revision      uint64
	theme         uint64
	layout        string
	header        string
	activePane    FocusPane
	viewMode      ViewMode
	flash         bool
	hoverClose    int
	hoverLayout   int
	terminal      terminalPreviewKey
	documents     string
	backgrounds   tty.BackgroundMode
	backgroundMax int
	defaultBg     string
	search        terminalSearchPreviewKey
	selection     terminalSelectionPreviewKey
}

type terminalPreviewKey struct {
	buffer       *tty.OutputBuffer
	bufferRev    uint64
	scroll       int
	freezeActive bool
	freezeStart  int
	interactive  bool
	mouse        bool
	paneWidth    int
	paneHeight   int
	bar          [2]uint8
}

type terminalSearchPreviewKey struct {
	input      bool
	panel      bool
	query      string
	current    int
	generation uint64
	matches    int
}

type terminalSelectionPreviewKey struct {
	panel       bool
	active      bool
	rectangular bool
	startLine   int
	startCol    int
	endLine     int
	endCol      int
	anchorLine  int
	anchorCol   int
}

func (p *Plugin) bumpProjectPreviewRevision() {
	p.projectPreviewRevision++
	if p.projectPreviewRevision == 0 {
		p.projectPreviewRevision++
	}
}

// projectPreviewKeyFor returns false for pane kinds whose models do not yet
// expose a cheap visual revision, and for document-local overlays whose
// mutable render scratch must be rebuilt by View. Those cases keep the proven
// uncached compositor path.
func (p *Plugin) projectPreviewKeyFor(layout PaneLayout, width, height int) (projectPreviewKey, bool) {
	key := projectPreviewKey{
		width: width, height: height,
		revision:      p.projectPreviewRevision,
		theme:         styles.VisualRevision(),
		activePane:    p.activePane,
		viewMode:      p.viewMode,
		flash:         p.previewFlashActive(),
		hoverClose:    p.hoverPaneClose,
		hoverLayout:   p.hoverPaneLayout,
		backgrounds:   p.backgrounds,
		backgroundMax: p.backgroundSpanMax,
		defaultBg:     p.terminalDefaultBackground,
		search: terminalSearchPreviewKey{
			input: p.terminalSearch.InputActive, panel: p.terminalSearch.Panel,
			query: p.terminalSearch.Query(), current: p.terminalSearch.Current,
			generation: p.terminalSearch.Generation, matches: len(p.terminalSearch.Matches),
		},
		selection: terminalSelectionPreviewKey{
			panel: p.selectionPanel, active: p.selection.Active, rectangular: p.selection.Rectangular,
			startLine: p.selection.Start.Line, startCol: p.selection.Start.Col,
			endLine: p.selection.End.Line, endCol: p.selection.End.Col,
			anchorLine: p.selection.Anchor.Line, anchorCol: p.selection.Anchor.Col,
		},
	}
	if p.selectingShell() {
		if shell := p.getSelectedShell(); shell != nil {
			key.header = "shell:" + shell.Name + "\x00" + shell.TmuxName
		}
	} else if wt := p.selectedWorktree(); wt != nil {
		key.header = "worktree:" + wt.Name + "\x00" + wt.TaskID + "\x00" + wt.IdentityKey()
		if wt.IsOrphaned {
			key.header += "\x00orphaned"
		}
		if wt.Agent == nil {
			key.header += "\x00empty"
		}
	}

	var layoutID, documents []byte
	appendInt := func(dst []byte, values ...int) []byte {
		for _, value := range values {
			dst = strconv.AppendInt(dst, int64(value), 10)
			dst = append(dst, ':')
		}
		return dst
	}
	host := paneHost{p}
	for _, placement := range layout.Leaves {
		node := placement.Node
		if node == nil || node.Split != nil {
			return projectPreviewKey{}, false
		}
		layoutID = append(layoutID, 'L')
		layoutID = appendInt(layoutID, node.ID, int(node.Kind), node.ContentID,
			placement.Box.X, placement.Box.Y, placement.Box.W, placement.Box.H, int(host.Chrome(node)))
		switch node.Kind {
		case panelayout.Terminal:
			leaf := p.primaryTermPane()
			buffer := p.terminalOutputBuffer(false)
			key.terminal.buffer = buffer
			if buffer != nil {
				key.terminal.bufferRev = buffer.Revision()
			}
			key.terminal.scroll = leaf.Scroll
			key.terminal.freezeActive = leaf.Freeze.Active()
			key.terminal.freezeStart = leaf.Freeze.Start()
			key.terminal.interactive = p.interactiveDescribes(false)
			key.terminal.mouse = p.paneMouseReporting(false)
			key.terminal.paneWidth, key.terminal.paneHeight = p.resolvedPaneGeometry(false, key.terminal.interactive)
			bar := p.terminalBarStyle(false)
			key.terminal.bar = [2]uint8{uint8(bar.Thumb), uint8(bar.Track)}
		case panelayout.Document:
			doc := p.docs[node.ContentID]
			if doc == nil || doc.mode != nil || doc.editing() {
				return projectPreviewKey{}, false
			}
			documents = append(documents, 'D')
			documents = appendInt(documents, node.ID, node.ContentID, doc.tabs.Active,
				p.hoverTabClose.IndexFor(node.ID))
			documents = append(documents, doc.root...)
			documents = append(documents, 0)
			documents = append(documents, doc.surface...)
			documents = append(documents, 0)
			for _, item := range doc.tabs.Items {
				var revision uint64
				title := ""
				if item.View != nil {
					revision = item.View.VisualRevision()
					title = item.View.Title()
				}
				documents = strconv.AppendUint(documents, revision, 10)
				documents = append(documents, ':')
				documents = append(documents, title...)
				documents = append(documents, 0)
			}
			generation := p.ensureDocLinkResolution().SnapshotForRoot(doc.root).Generation()
			documents = strconv.AppendUint(documents, generation, 10)
			documents = append(documents, ':')
			documents = strconv.AppendUint(documents, p.linkMatcherGeneration, 10)
			documents = append(documents, ';')
		default:
			return projectPreviewKey{}, false
		}
	}
	for _, divider := range layout.Dividers {
		layoutID = append(layoutID, 'S')
		state := host.HandleState(divider.SplitID)
		layoutID = appendInt(layoutID, divider.SplitID, int(divider.Axis),
			divider.Box.X, divider.Box.Y, divider.Box.W, divider.Box.H, int(state))
	}
	key.layout = string(layoutID)
	key.documents = string(documents)
	return key, true
}

func (p *Plugin) reuseProjectPreview(key projectPreviewKey) (string, bool) {
	cache := &p.projectPreview
	if !cache.valid || cache.key != key {
		return "", false
	}
	p.previewActionPlacements = append(p.previewActionPlacements[:0], cache.actions...)
	p.startAgentBtn = cache.start
	terminalperf.Record(terminalperf.ProjectPreviewCacheHit)
	return cache.output, true
}

func (p *Plugin) storeProjectPreview(key projectPreviewKey, output string) {
	p.projectPreview = projectPreviewCache{
		valid: true, key: key, output: output,
		actions: append([]headerChipPlacement(nil), p.previewActionPlacements...),
		start:   p.startAgentBtn,
	}
}

// replayProjectPreviewRegions rebuilds document-derived relative regions at
// this frame's origin. The shared registrar still owns leaf/divider/header/body
// order and therefore pointer precedence.
func (p *Plugin) replayProjectPreviewRegions(layout PaneLayout) {
	for _, placement := range layout.Leaves {
		node := placement.Node
		if node == nil || node.Kind != panelayout.Document {
			continue
		}
		doc := p.docs[node.ContentID]
		if doc == nil || doc.view() == nil {
			continue
		}
		inner := paneframe.GeometryForChrome(placement.Box, paneHost{p}.Chrome(node)).Inner
		doc.boxX, doc.boxY, doc.boxW, doc.boxH = inner.X, inner.Y, inner.W, inner.H
		p.bindPaneSelection(doc.view(), inner)
		frame := doc.view().PreparedFrame()
		frame.EachHitAt(inner.X, inner.Y+terminalHeaderRows, func(hit docview.ContentLinkHit) {
			p.docLinkHits = append(p.docLinkHits, docContentLinkHit{LeafID: doc.leafID, Ref: hit.Ref, Rect: hit.Rect})
		})
	}
}
