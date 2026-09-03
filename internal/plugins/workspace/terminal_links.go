package workspace

import (
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/targetactivation"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/ui"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacediff"
)

// terminalLink is a scanned span plus the one thing the scanner cannot know:
// the canonical root this host resolved it against. The kind is
// terminallink's own — this host used to keep a parallel kind enum and two
// translations to and from it, and that private vocabulary is exactly what
// stopped a third surface from activating anything.
type terminalLink struct {
	Kind     terminallink.Kind
	StartCol int
	EndCol   int
	Value    string
	Line     int
	Root     string // canonical selected surface root for resolved bare paths
	Raw      string // original candidate, revalidated on activation
	Provider string // resource links only: the configured provider instance
	Matcher  string // resource links only: the provider-stable matcher ID
}

// span rebuilds the scanned span this link came from, so activation can speak
// the shared span→target→plan path rather than a local switch.
func (l terminalLink) span() terminallink.Span {
	return terminallink.Span{
		Kind:     l.Kind,
		StartCol: l.StartCol,
		EndCol:   l.EndCol,
		Value:    l.Value,
		Extra: terminallink.Extra{
			Line:     l.Line,
			Raw:      l.Raw,
			Provider: l.Provider,
			Matcher:  l.Matcher,
		},
	}
}

func (p *Plugin) terminalLinkTarget(termPanel bool) string {
	if termPanel {
		return p.requireShellTermPane().Session + "\x00" + p.requireShellTermPane().PaneID
	}
	if p.selectingShell() {
		if shell := p.getSelectedShell(); shell != nil && shell.Agent != nil {
			return shell.Agent.TmuxSession + "\x00" + shell.Agent.TmuxPane
		}
		return ""
	}
	if wt := p.selectedWorktree(); wt != nil && wt.Agent != nil {
		return wt.Agent.TmuxSession + "\x00" + wt.Agent.TmuxPane
	}
	return ""
}

type terminalLinkSurfaceContext struct {
	rawRoot string
	root    string
	surface string
	target  string
	ok      bool
}

func safeHTTPURL(raw string) (string, bool) {
	return terminallink.SafeHTTPURL(raw)
}

func detectTerminalLinks(line string) []terminalLink {
	return activatableTerminalLinks(terminallink.Scan(line, nil, nil), false)
}

// activatableTerminalLinks keeps the spans this host can act on. Issue and
// git-spec spans are among them only when leaves is true — both open a leaf
// of the pane tree, so without a tree there is nothing to open and an
// underline would promise a click that goes nowhere. The issue-preview modal
// is not this host's route and never was.
func activatableTerminalLinks(spans []terminallink.Span, leaves bool) []terminalLink {
	links := make([]terminalLink, 0, len(spans))
	for _, span := range spans {
		if !terminallink.Activatable(span.Kind) {
			continue
		}
		if !leaves && requiresPaneLeaf(span.Kind) {
			continue
		}
		raw := span.Extra.Raw
		if span.Kind == terminallink.KindDiff && raw == "" {
			raw = span.Value
		}
		links = append(links, terminalLink{
			Kind:     span.Kind,
			StartCol: span.StartCol,
			EndCol:   span.EndCol,
			Value:    span.Value,
			Line:     span.Extra.Line,
			Raw:      raw,
			Provider: span.Extra.Provider,
			Matcher:  span.Extra.Matcher,
		})
	}
	return links
}

// requiresPaneLeaf reports the kinds that can only open a leaf of the pane
// tree. Without a tree there is nothing to open, and an underline would promise
// a click that goes nowhere. The issue-preview modal is not this host's route
// and never was.
func requiresPaneLeaf(kind terminallink.Kind) bool {
	switch kind {
	case terminallink.KindIssue, terminallink.KindDiff, terminallink.KindResource:
		return true
	default:
		return false
	}
}

func (p *Plugin) terminalLinkSurfaceContext(termPanel bool) terminalLinkSurfaceContext {
	if p.ctx == nil {
		return terminalLinkSurfaceContext{}
	}
	rawRoot := p.ctx.WorkDir
	surface := ""
	if p.selectingShell() {
		shell := p.getSelectedShell()
		if shell == nil || shell.TmuxName == "" {
			return terminalLinkSurfaceContext{}
		}
		if shell.WorkDir != "" {
			rawRoot = shell.WorkDir
		}
		surface = "shell:" + shell.TmuxName
	} else {
		wt := p.selectedWorktree()
		if wt == nil {
			return terminalLinkSurfaceContext{}
		}
		rawRoot = wt.Path
		surface = workspaceSurfaceIdentity(wt)
	}
	if termPanel {
		surface += ":panel"
	}
	target := p.terminalLinkTarget(termPanel)
	cached, _ := p.terminalPane(termPanel).LinkContext.(terminalLinkSurfaceContext)
	if p.terminalPane(termPanel) == nil {
		return terminalLinkSurfaceContext{}
	}
	if cached.surface == surface && cached.rawRoot == filepath.Clean(rawRoot) && cached.target == target && cached.root != "" {
		return cached
	}
	root, err := filepath.EvalSymlinks(rawRoot)
	if err != nil {
		return terminalLinkSurfaceContext{}
	}
	context := terminalLinkSurfaceContext{rawRoot: filepath.Clean(rawRoot), root: filepath.Clean(root), surface: surface, target: target, ok: true}
	p.terminalPane(termPanel).LinkContext = context
	return context
}

func stripSourceOSC8(line string) string {
	return terminallink.StripOSC8(line)
}

func (p *Plugin) terminalLinkAt(action mouse.MouseAction) (terminalLink, terminalLinkSurfaceContext, bool, bool) {
	point, line, ok := p.terminalPointAndLine(action)
	if !ok {
		return terminalLink{}, terminalLinkSurfaceContext{}, false, false
	}
	termPanel := action.Region != nil && action.Region.ID == regionTermPanelContent
	context := p.terminalLinkSurfaceContext(termPanel)
	state := p.primaryTermPane().LinkState
	if termPanel {
		state = p.requireShellTermPane().LinkState
	}
	span, ok := state.SpanAt(ui.ExpandTabs(line, tabStopWidth), point.Line, point.Col)
	if !ok {
		return terminalLink{}, context, termPanel, false
	}
	links := activatableTerminalLinks([]terminallink.Span{span}, p.paneRoot != nil)
	if len(links) != 1 {
		return terminalLink{}, context, termPanel, false
	}
	if links[0].Kind == terminallink.KindFile && links[0].Raw != "" {
		links[0].Root = context.root
	}
	return links[0], context, termPanel, true
}

func (p *Plugin) activateTerminalLink(action mouse.MouseAction) (tea.Cmd, bool) {
	link, context, termPanel, ok := p.terminalLinkAt(action)
	if !ok {
		return nil, false
	}
	return p.activateResolvedTerminalLink(link, context, termPanel)
}

// activateResolvedTerminalLink executes the plan the shared service resolves
// for this link. The decision — is this well formed, what does it open, is the
// URL safe — belongs to targetactivation, which every surface shares; only the
// execution below is this host's, because only it owns these panes.
func (p *Plugin) activateResolvedTerminalLink(link terminalLink, context terminalLinkSurfaceContext, termPanel bool) (tea.Cmd, bool) {
	plan, err := targetactivation.PlanForSpan(link.span())
	if err != nil {
		return nil, false
	}
	switch plan.Kind {
	case targetactivation.PlanOpenURL:
		p.clearTerminalSelection()
		return openInBrowser(plan.URL), true
	case targetactivation.PlanOpenIssue:
		return p.activateIssueLink(plan.Issue)
	case targetactivation.PlanOpenNote:
		return p.activateNoteLink(plan.Note)
	case targetactivation.PlanOpenResource:
		return p.activateResourceLink(resourceview.Ref{
			Instance:   plan.Provider,
			Matcher:    plan.Matcher,
			Locator:    plan.Locator,
			Collection: plan.Collection,
			Query:      plan.Query,
			Filters:    resource.DecodeFilters(plan.Filters),
		})
	case targetactivation.PlanOpenDiff:
		return p.revalidateTerminalLink(link, context, termPanel)
	case targetactivation.PlanOpenFile:
		return p.revalidateTerminalLink(link, context, termPanel)
	case targetactivation.PlanAttachSession:
		// The same lookup the public AttachSessionMsg does, and the same gate:
		// a name matching no shell and no worktree agent attaches nothing.
		if cmd := p.attachSessionMsg(app.AttachSessionMsg{Session: plan.Session}); cmd != nil {
			p.clearTerminalSelection()
			return cmd, true
		}
		return nil, false
	default:
		return nil, false
	}
}

// terminalHandlesPlanKind is the parity assertion's other half: every plan kind
// a scanned span can produce must be dispatched above. Its twin lives on the
// global workspaces surface (internal/overview).
func terminalHandlesPlanKind(kind targetactivation.PlanKind) bool {
	switch kind {
	case targetactivation.PlanOpenURL, targetactivation.PlanOpenFile,
		targetactivation.PlanOpenIssue, targetactivation.PlanOpenNote, targetactivation.PlanOpenDiff,
		targetactivation.PlanOpenResource, targetactivation.PlanAttachSession:
		return true
	default:
		return false
	}
}

func (p *Plugin) openResolvedFilePreview(root, surface, display, abs string, line int) tea.Cmd {
	var file *os.File
	var err error
	if display != "" && !filepath.IsAbs(filepath.FromSlash(display)) {
		file, err = openContainedRegularFile(root, display)
	} else {
		file, err = terminallink.OpenRegular(abs)
	}
	if err != nil {
		return nil
	}
	if p.paneRoot == nil {
		_ = file.Close()
		return p.activateFileForRoot(root, display, line)
	}
	return p.openDocPaneFileForSurface(root, surface, display, line, file)
}

func (p *Plugin) activateFileForRoot(root, display string, line int) tea.Cmd {
	if p.ctx == nil || display == "" || filepath.IsAbs(filepath.FromSlash(display)) {
		return nil
	}
	ctxResolved, err := filepath.EvalSymlinks(p.ctx.WorkDir)
	if err != nil {
		ctxResolved = filepath.Clean(p.ctx.WorkDir)
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil
	}
	// Which root the terminal was scanned against is this host's knowledge, and
	// it is the whole of what the shell needs: a path outside the current
	// project is a cross-project jump, which the shell can now make. It used to
	// be a silent no-op — the last place a link named something real and
	// nothing happened.
	project := ""
	if filepath.Clean(rootResolved) != filepath.Clean(ctxResolved) {
		project = root
	}
	return app.ActivateTargetIn(uirequest.Target{
		Kind:  uirequest.TargetKindFile,
		Value: filepath.ToSlash(display),
		Line:  line,
	}, project)
}

func (p *Plugin) openTerminalPath(raw string, line int) tea.Cmd {
	if p.ctx == nil {
		return nil
	}
	base := p.ctx.WorkDir
	if shell := p.getSelectedShell(); shell != nil {
		if shell.WorkDir != "" {
			base = shell.WorkDir
		}
	} else {
		if wt := p.selectedWorktree(); wt != nil {
			base = wt.Path
		}
	}
	display, abs, ok := resolveTerminalPath(base, raw)
	if !ok {
		return nil
	}
	baseResolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		baseResolved = filepath.Clean(base)
	}
	_, surface, _ := p.selectedTerminalSurface()
	return p.openResolvedFilePreview(filepath.Clean(baseResolved), surface, display, abs, line)
}

func resolveTerminalPath(base, raw string) (relative, absolute string, ok bool) {
	baseResolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", "", false
	}
	return terminallink.ResolveFile(baseResolved, raw)
}

// activateDiffLink opens the clicked git spec against the selected terminal
// surface as a Diff leaf tab. DiffTarget re-resolves in the checkout so a
// short click and sidecar open share one Identity; .. vs ... is kept.
func (p *Plugin) activateDiffLink(raw string) (tea.Cmd, bool) {
	root, surface, ok := p.selectedTerminalSurface()
	if !ok {
		return nil, false
	}
	target := uirequest.DiffTarget(root, raw)
	if target.Kind != workspacediff.TargetCommit && target.Kind != workspacediff.TargetRange {
		return nil, false
	}
	cmd := p.openDiffPaneForSurface(root, surface, target)
	diff, _ := p.activeDiffPane()
	if diff == nil || diff.tabs.Find(target.Identity()) < 0 {
		return nil, false
	}
	p.clearTerminalSelection()
	return cmd, true
}
