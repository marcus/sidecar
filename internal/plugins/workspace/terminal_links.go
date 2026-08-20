package workspace

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/mouse"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/targetactivation"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/tty"
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

type terminalLinkMemo struct {
	surfaces map[string]terminalLinkSurfaceMemo
}

type terminalLinkSurfaceMemo struct {
	rawRoot  string
	root     string
	target   string
	buffer   *tty.OutputBuffer
	revision uint64
	paths    map[string]terminalLinkResolution
	specs    map[string]terminalLinkResolution
	newSpecs int
}

func (p *Plugin) terminalLinkTarget(termPanel bool) string {
	if termPanel {
		return p.termPanelSession + "\x00" + p.termPanelPaneID
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

type terminalLinkResolution struct {
	rel string
	ok  bool
}

type terminalLineLinkResolver struct {
	plugin  *Plugin
	context terminalLinkSurfaceContext
	buffer  *tty.OutputBuffer
}

func (r *terminalLineLinkResolver) links(line string) []terminalLink {
	return r.plugin.resolvedTerminalLinks(r.context, r.buffer, line)
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

func decorateTerminalLinks(line string, resolved *terminalLineLinkResolver) string {
	// tmux output is untrusted. Remove source-supplied OSC controls and
	// synthesize OSC-8 only for URLs that pass safeHTTPURL.
	line = stripSourceOSC8(line)
	links := detectTerminalLinks(line)
	if resolved != nil {
		links = resolved.links(line)
	}
	return terminallink.Decorate(line, spansFromTerminalLinks(links))
}

func spansFromTerminalLinks(links []terminalLink) []terminallink.Span {
	spans := make([]terminallink.Span, 0, len(links))
	for _, link := range links {
		spans = append(spans, link.span())
	}
	return spans
}

func (p *Plugin) terminalLinkResolver(termPanel bool, buffer *tty.OutputBuffer) *terminalLineLinkResolver {
	if p.paneRoot == nil || buffer == nil {
		return nil
	}
	context := p.terminalLinkSurfaceContext(termPanel)
	if !context.ok {
		return nil
	}
	return &terminalLineLinkResolver{plugin: p, context: context, buffer: buffer}
}

func (p *Plugin) terminalLinkSurfaceContext(termPanel bool) terminalLinkSurfaceContext {
	return p.terminalLinkSurfaceContextWithFreshRoot(termPanel, false)
}

func (p *Plugin) terminalLinkSurfaceContextWithFreshRoot(termPanel, freshRoot bool) terminalLinkSurfaceContext {
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
	if !freshRoot && p.terminalLinkMemo.surfaces != nil {
		if memo, found := p.terminalLinkMemo.surfaces[surface]; found &&
			memo.rawRoot == filepath.Clean(rawRoot) && memo.target == target && memo.root != "" {
			return terminalLinkSurfaceContext{rawRoot: memo.rawRoot, root: memo.root, surface: surface, target: target, ok: true}
		}
	}
	rootResolver := filepath.EvalSymlinks
	if p.terminalRootResolver != nil {
		rootResolver = p.terminalRootResolver
	}
	root, err := rootResolver(rawRoot)
	if err != nil {
		return terminalLinkSurfaceContext{}
	}
	return terminalLinkSurfaceContext{rawRoot: filepath.Clean(rawRoot), root: filepath.Clean(root), surface: surface, target: target, ok: true}
}

func (p *Plugin) invalidateTerminalLinkSurface(surface string) {
	if p.terminalLinkMemo.surfaces != nil {
		delete(p.terminalLinkMemo.surfaces, surface)
	}
}

func (p *Plugin) resolvedTerminalLinks(context terminalLinkSurfaceContext, buffer *tty.OutputBuffer, line string) []terminalLink {
	if p.paneRoot == nil || buffer == nil || !context.ok {
		return detectTerminalLinks(line)
	}
	revision := buffer.Revision()
	if p.terminalLinkMemo.surfaces == nil {
		p.terminalLinkMemo.surfaces = make(map[string]terminalLinkSurfaceMemo)
	}
	memo, found := p.terminalLinkMemo.surfaces[context.surface]
	if !found || memo.root != context.root || memo.target != context.target || memo.buffer != buffer || memo.revision != revision {
		memo = terminalLinkSurfaceMemo{rawRoot: context.rawRoot, root: context.root, target: context.target, buffer: buffer, revision: revision,
			paths: make(map[string]terminalLinkResolution), specs: make(map[string]terminalLinkResolution)}
	}
	if memo.specs == nil {
		memo.specs = make(map[string]terminalLinkResolution)
	}
	resolver := resolveTerminalPathFromResolvedBase
	if p.terminalPathResolver != nil {
		resolver = p.terminalPathResolver
	}
	// Matchers are the live provider snapshot, and they are the whole of what
	// makes a resource key clickable: an empty set is a scan that finds none,
	// so an unready, failed, disabled or unconfigured provider leaves its keys
	// as ordinary text. Matching stays pure — no process starts here.
	links := activatableTerminalLinks(terminallink.ScanWith(line, terminallink.Options{
		Resolve: func(raw string) (string, terminallink.Extra, bool) {
			resolution, found := memo.paths[raw]
			if !found {
				rel, _, ok := resolver(context.root, raw)
				resolution = terminalLinkResolution{rel: rel, ok: ok}
				memo.paths[raw] = resolution
			}
			if !resolution.ok {
				return "", terminallink.Extra{}, false
			}
			return resolution.rel, terminallink.Extra{Raw: raw}, true
		},
		ResolveDiff: func(raw string) (string, terminallink.Extra, bool) {
			resolution, found := memo.specs[raw]
			if !found {
				if memo.newSpecs >= terminallink.MaxNewDiffResolves {
					return "", terminallink.Extra{}, false
				}
				memo.newSpecs++
				value, ok := p.resolveTerminalSpec(context.root, raw)
				resolution = terminalLinkResolution{rel: value, ok: ok}
				memo.specs[raw] = resolution
			}
			if !resolution.ok {
				return "", terminallink.Extra{}, false
			}
			if resolution.rel == "" {
				return raw, terminallink.Extra{Raw: raw}, true
			}
			return resolution.rel, terminallink.Extra{Raw: raw}, true
		},
		Matchers: p.resourceMatchers,
	}), true)
	for i := range links {
		if links[i].Kind == terminallink.KindFile && links[i].Raw != "" {
			links[i].Root = context.root
		}
	}
	p.terminalLinkMemo.surfaces[context.surface] = memo
	return links
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
	buffer := p.terminalOutputBuffer(termPanel)
	context := p.terminalLinkSurfaceContext(termPanel)
	for _, link := range p.resolvedTerminalLinks(context, buffer, ui.ExpandTabs(line, tabStopWidth)) {
		if point.Col >= link.StartCol && point.Col <= link.EndCol {
			return link, context, termPanel, true
		}
	}
	return terminalLink{}, context, termPanel, false
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
	case targetactivation.PlanOpenResource:
		return p.activateResourceLink(resourceview.Ref{
			Instance: plan.Provider,
			Matcher:  plan.Matcher,
			Locator:  plan.Locator,
		})
	case targetactivation.PlanOpenDiff:
		return p.activateDiffLink(plan.Spec)
	case targetactivation.PlanOpenFile:
		return p.activateFilePlan(plan, link, context, termPanel)
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
		targetactivation.PlanOpenIssue, targetactivation.PlanOpenDiff,
		targetactivation.PlanOpenResource, targetactivation.PlanAttachSession:
		return true
	default:
		return false
	}
}

// activateFilePlan re-resolves the file token against this surface's own root:
// the plan carries the token as the text wrote it, and a terminal's root is not
// the project's.
func (p *Plugin) activateFilePlan(plan targetactivation.Plan, link terminalLink, context terminalLinkSurfaceContext, termPanel bool) (tea.Cmd, bool) {
	raw := plan.Path
	root := link.Root
	if root == "" {
		root = context.root
	}
	if link.Root != "" {
		fresh := p.terminalLinkSurfaceContextWithFreshRoot(termPanel, true)
		if !fresh.ok || fresh.surface != context.surface || fresh.target != context.target || fresh.root != link.Root {
			p.invalidateTerminalLinkSurface(context.surface)
			return nil, false
		}
		root = fresh.root
	}
	if root == "" {
		cmd := p.openTerminalPath(raw, plan.Line)
		if cmd != nil {
			p.clearTerminalSelection()
		}
		return cmd, cmd != nil
	}
	display, abs, ok := resolveTerminalPathFromResolvedBase(root, raw)
	if !ok {
		return nil, false
	}
	surface := strings.TrimSuffix(context.surface, ":panel")
	cmd := p.openResolvedFilePreview(root, surface, display, abs, plan.Line)
	if cmd != nil {
		p.clearTerminalSelection()
	}
	return cmd, cmd != nil
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
	return resolveTerminalPathFromResolvedBase(baseResolved, raw)
}

func resolveTerminalPathFromResolvedBase(baseResolved, raw string) (relative, absolute string, ok bool) {
	return terminallink.ResolveFile(baseResolved, raw)
}

func (p *Plugin) resolveTerminalSpec(root, raw string) (string, bool) {
	if p.terminalSpecResolver != nil {
		return p.terminalSpecResolver(root, raw)
	}
	value, _, ok := terminallink.ResolveGitSpec(root, raw)
	return value, ok
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
