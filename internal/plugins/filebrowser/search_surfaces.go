package filebrowser

import (
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/filefind"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/projectsearch"
)

// This file is the Files plugin's half of the two shared search surfaces. The
// finder (ctrl+p) and the project search (f) live in internal/filefind and
// internal/projectsearch so a workspace file pane can host the same two
// surfaces; what stays here is only what "open this" means in this plugin:
// move the tree cursor, open a tab, pin it, and set up the content search.

// fileFinder returns the finder, creating it on first use and keeping it
// pointed at the current project. It shares the plugin's file cache, which the
// tree search filters too, so a scan serves both.
func (p *Plugin) fileFinder() *filefind.Finder {
	root, epoch := p.finderRoot()
	if p.finder == nil {
		p.finder = filefind.NewFinder(&p.quickOpen, root, epoch)
		return p.finder
	}
	p.finder.SetRoot(root, epoch)
	return p.finder
}

// projectSearchSurface returns the live project search, or nil when none is
// open. It keeps the search's root, epoch, and render size current.
func (p *Plugin) projectSearchSurface() *projectsearch.Search {
	if p.projectSearch == nil {
		return nil
	}
	root, epoch := p.contextRoot()
	p.projectSearch.SetRoot(root, epoch)
	p.projectSearch.SetSize(p.width, p.height)
	return p.projectSearch
}

// finderRoot is the root the find-by-name index belongs to. While bound it is
// the host's, and the cache's scanner is the host's catalog verb — the finder
// itself does not know or care which machine answered.
//
// It is deliberately not contextRoot: project search runs ripgrep on that
// root, and a remote path handed to a local process is exactly the failure
// this area exists to prevent.
func (p *Plugin) finderRoot() (string, uint64) {
	if !p.remoteBound() {
		p.quickOpen.Scan = nil
		return p.contextRoot()
	}
	p.quickOpen.Scan = p.scanRemoteCandidates
	return p.remoteRoot(), p.ctx.Epoch
}

func (p *Plugin) contextRoot() (string, uint64) {
	if p.ctx == nil {
		return "", 0
	}
	return p.ctx.WorkDir, p.ctx.Epoch
}

// --- Quick open (ctrl+p) ---------------------------------------------------

// openQuickOpen enters quick open mode.
func (p *Plugin) openQuickOpen() (plugin.Plugin, tea.Cmd) {
	f := p.fileFinder()
	cmd := f.Open()
	p.quickOpenMode = true
	return p, cmd
}

// renderQuickOpenModalContent renders the quick open modal box content.
func (p *Plugin) renderQuickOpenModalContent() string {
	f := p.fileFinder()
	f.SetPreferredWidth(p.boxWidthOffTheDivider(filefind.PreferredWidth))
	return f.View(p.width, p.height, p.mouseHandler)
}

// boxWidthOffTheDivider is preferred, adjusted if a box that wide would land on
// the column the pane divider occupies — with either a border or the blank
// gutter ui.OverlayModal keeps on each side of the box.
//
// Centring is arithmetic and the divider is wherever the user dragged it, so
// the two coincide sooner or later. A border there reads as welded to the
// frame: one unbroken vertical line runs from the top of the plugin to the
// bottom, through the box. The gutter there is worse — it blanks the divider
// for the box's full height, punching a two-dozen-row hole in the pane's
// vertical rule, which is neither overlapping it cleanly nor keeping clear of
// it. Two cells of width moves the box one column, which is all it takes, and
// the surface computes its own hit regions from the same number, so nothing
// else has to know.
func (p *Plugin) boxWidthOffTheDivider(preferred int) int {
	if p.width <= 0 {
		return preferred
	}
	p.calculatePaneWidths()
	frame := map[int]bool{p.treeWidth: true, p.treeWidth + 1: true}

	width := preferred
	if maxW := modal.ContentBoxWidth(p.width); width > maxW {
		// The surface would clamp it anyway; adjust the width it will really use.
		width = maxW
	}
	for range 4 {
		if width < modal.MinModalWidth {
			return preferred
		}
		left, right := (p.width-width)/2, (p.width-width)/2+width-1
		if !frame[left] && !frame[right] && !frame[left-1] && !frame[right+1] {
			return width
		}
		width -= 2
	}
	return width
}

// handleQuickOpenKey handles key input during quick open mode.
func (p *Plugin) handleQuickOpenKey(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	res, cmd := p.fileFinder().HandleKey(msg)
	return p.applyFinderResult(res, cmd)
}

// handleQuickOpenMouse handles mouse events in the quick open modal.
func (p *Plugin) handleQuickOpenMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	res, cmd := p.fileFinder().HandleMouse(msg, p.mouseHandler)
	plug, cmd := p.applyFinderResult(res, cmd)
	return plug.(*Plugin), cmd
}

// applyFinderResult turns a finder outcome into this plugin's behaviour: the
// chosen file becomes the pinned preview tab, with the tree cursor moved onto
// it.
func (p *Plugin) applyFinderResult(res filefind.Result, cmd tea.Cmd) (plugin.Plugin, tea.Cmd) {
	switch res.Outcome {
	case filefind.OutcomeCancelled:
		p.quickOpenMode = false
		return p, cmd

	case filefind.OutcomeOpen:
		p.quickOpenMode = false
		p.revealInTree(res.Path)

		// Load preview and pin (explicit user selection)
		p.activePane = PanePreview
		openCmd := p.openTab(res.Path, TabOpenReplace)
		p.pinTab(p.activeTab)
		return p, tea.Batch(cmd, openCmd)
	}

	return p, cmd
}

// --- Project search (f) ----------------------------------------------------

// openProjectSearch enters project-wide search mode.
func (p *Plugin) openProjectSearch() (plugin.Plugin, tea.Cmd) {
	root, epoch := p.contextRoot()
	p.projectSearch = projectsearch.New(root, epoch)
	p.projectSearch.SetSize(p.width, p.height)
	p.projectSearchMode = true
	return p, nil
}

// closeProjectSearch drops the search entirely; reopening starts fresh. Close
// kills the ripgrep process still running for it, which dropping the pointer
// does not.
func (p *Plugin) closeProjectSearch() {
	p.projectSearch.Close()
	p.projectSearchMode = false
	p.projectSearch = nil
}

// renderProjectSearchModalContent renders the project search modal box content.
func (p *Plugin) renderProjectSearchModalContent() string {
	search := p.projectSearchSurface()
	if search == nil {
		return ""
	}
	search.SetPreferredWidth(p.boxWidthOffTheDivider(projectsearch.PreferredWidth))
	return search.View(p.width, p.height, p.mouseHandler)
}

// handleProjectSearchKey handles key input during project search mode.
func (p *Plugin) handleProjectSearchKey(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	search := p.projectSearchSurface()
	if search == nil {
		p.closeProjectSearch()
		return p, nil
	}
	res, cmd := search.HandleKey(msg)
	return p.applyProjectSearchResult(res, cmd)
}

// handleProjectSearchMouse handles mouse events in the project search modal.
func (p *Plugin) handleProjectSearchMouse(msg tea.MouseMsg) (*Plugin, tea.Cmd) {
	search := p.projectSearchSurface()
	if search == nil {
		return p, nil
	}
	res, cmd := search.HandleMouse(msg, p.mouseHandler)
	plug, cmd := p.applyProjectSearchResult(res, cmd)
	return plug.(*Plugin), cmd
}

// applyProjectSearchResult turns a search outcome into this plugin's
// behaviour: the hit becomes the pinned preview tab (or a new tab), scrolled to
// the matched line, with the content search primed to highlight the term.
func (p *Plugin) applyProjectSearchResult(res projectsearch.Result, cmd tea.Cmd) (plugin.Plugin, tea.Cmd) {
	switch res.Outcome {
	case projectsearch.OutcomeCancelled:
		p.closeProjectSearch()
		return p, cmd

	case projectsearch.OutcomeOpenExternal:
		p.closeProjectSearch()
		return p, tea.Batch(cmd, p.openFileAtLine(res.Path, res.Line))

	case projectsearch.OutcomeOpen:
		p.closeProjectSearch()
		p.revealInTree(res.Path)

		if res.NewTab {
			// Load preview in new tab
			p.activePane = PanePreview
			return p, tea.Batch(cmd, p.openTabAtLine(res.Path, res.Line, TabOpenNew))
		}

		// Load preview and pin (explicit user selection)
		p.activePane = PanePreview
		openCmd := p.openTabAtLine(res.Path, res.Line, TabOpenReplace)
		p.pinTab(p.activeTab)

		// Set up content search for highlighting the matched term
		if res.Query != "" {
			p.contentSearchMode = true
			p.contentSearchCommitted = true // Skip input phase, enable n/N navigation
			p.contentSearchField.SetQuery(res.Query)
			p.contentSearchField.Blur()
			p.contentSearchMatches = nil // Will be populated after preview loads
			p.contentSearchCursor = 0
			if openCmd == nil {
				p.updateContentMatches()
				if res.Line > 0 && len(p.contentSearchMatches) > 0 {
					p.scrollToNearestMatch(p.previewScroll)
				}
			}
		}

		return p, tea.Batch(cmd, openCmd)
	}

	return p, cmd
}

// revealInTree walks the tree down to path, expanding as it goes, and moves the
// tree cursor onto it. It is a no-op when the path is not in the tree.
func (p *Plugin) revealInTree(path string) {
	// Find the file in tree by walking down the path (efficient - only loads
	// needed dirs)
	targetNode := p.findAndExpandPath(path)
	if targetNode == nil {
		return
	}

	p.tree.Flatten()
	p.syncWatcherDirs()

	// Move tree cursor to file
	if idx := p.tree.IndexOf(targetNode); idx >= 0 {
		p.treeCursor = idx
		p.ensureTreeCursorVisible()
	}
}
