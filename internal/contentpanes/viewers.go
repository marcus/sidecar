package contentpanes

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/issueview"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/noteview"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/resource"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/workspacediff"
)

// Config supplies only replaceable viewer dependencies. Constructing a Deck
// remains free of filesystem, database, git, and provider work.
type Config struct {
	Renderer         *markdown.Renderer
	ResourceResolver resourceview.Resolver
	// PluginCalls is how a collection or row tab reaches its protocol plugin.
	// It is injected for the same reason ResourceResolver is: the host's
	// describe pass finishes long after a restored tab is armed.
	PluginCalls resourceview.CallsFor
	// Source loads Document identity and bytes. Nil uses today's local
	// filepreview path so tests constructing Config{} keep working.
	Source Source
	// ConfigureViewer attaches host presentation behavior (for example issue
	// navigation handlers or Diff paint state). It must remain free of I/O.
	ConfigureViewer func(kind panelayout.Kind, model any)
}

type viewer interface {
	model() any
	load(SurfaceContext, contentlink.Ref, int) tea.Cmd
	reload(SurfaceContext, contentlink.Ref, int) tea.Cmd
	arm(SurfaceContext, contentlink.Ref, int, TabState)
	focus(SurfaceContext, contentlink.Ref, int) tea.Cmd
	apply(SurfaceContext, any) (tea.Cmd, bool)
	reference(contentlink.Ref) (contentlink.Ref, string)
	snapshot(contentlink.Ref) TabState
}

func newViewer(cfg Config, kind panelayout.Kind) viewer {
	var v viewer
	switch kind {
	case panelayout.Document:
		v = &documentViewer{view: docview.New(cfg.Renderer), source: cfg.documentSource()}
	case panelayout.Issue:
		v = &issueViewer{view: issueview.New(cfg.Renderer), source: cfg.documentSource()}
	case panelayout.Note:
		v = &noteViewer{view: noteview.New(cfg.Renderer), source: cfg.documentSource()}
	case panelayout.Diff:
		v = &diffViewer{view: &workspacediff.View{}, source: cfg.documentSource()}
	case panelayout.Resource:
		view := resourceview.New(cfg.Renderer, cfg.ResourceResolver)
		view.SetCallsFor(cfg.PluginCalls)
		v = &resourceViewer{view: view}
	default:
		panic("contentpanes: viewer requested for non-content kind")
	}
	if cfg.ConfigureViewer != nil {
		cfg.ConfigureViewer(kind, v.model())
	}
	return v
}

func normalizeRef(ctx SurfaceContext, ref contentlink.Ref) (contentlink.Ref, panelayout.Kind, string, bool) {
	_ = ctx
	ref.Value = strings.TrimSpace(ref.Value)
	switch ref.Kind {
	case contentlink.KindURL:
		safe, ok := contentlink.SafeHTTPURL(ref.Value)
		if !ok {
			return contentlink.Ref{}, panelayout.Primary, "", false
		}
		ref.Value = safe
		return ref, panelayout.Primary, ref.Value, true
	case contentlink.KindInternal:
		raw := (&url.URL{Scheme: "sidecar", Host: ref.Namespace, Path: "/" + ref.Value}).String()
		parsed, err := contentlink.ParseInternalURI(raw)
		if err != nil {
			return contentlink.Ref{}, panelayout.Primary, "", false
		}
		ref = parsed.Ref
		if ref.Namespace == "note" {
			ref.Value = noteview.NormalizeID(ref.Value)
			if ref.Value == "" {
				return contentlink.Ref{}, panelayout.Note, "", false
			}
			return ref, panelayout.Note, ref.Value, true
		}
		return ref, panelayout.Primary, ref.Namespace + "\x00" + ref.Value, true
	case contentlink.KindFile:
		path := filepath.Clean(ref.Value)
		// Absolute refs are admitted because the host owns path resolution and
		// may deliberately preview a validated file from another worktree in
		// place. Relative traversal is never a valid resolved host reference.
		if !filepath.IsAbs(path) && (path == "." || path == "" || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))) {
			return contentlink.Ref{}, panelayout.Document, "", false
		}
		ref.Value = filepath.ToSlash(path)
		if ref.Line < 0 {
			ref.Line = 0
		}
		return ref, panelayout.Document, ref.Value, true
	case contentlink.KindIssue:
		ref.Value = issueview.NormalizeID(ref.Value)
		if ref.Value == "" {
			return contentlink.Ref{}, panelayout.Issue, "", false
		}
		return ref, panelayout.Issue, ref.Value, true
	case contentlink.KindDiff:
		target, ok := workspacediff.ParseSpec(ref.Value)
		if !ok {
			return contentlink.Ref{}, panelayout.Diff, "", false
		}
		ref.Value = target.Identity()
		return ref, panelayout.Diff, ref.Value, true
	case contentlink.KindResource:
		rf := resourceRef(ref)
		if !rf.Valid() {
			return contentlink.Ref{}, panelayout.Resource, "", false
		}
		return ref, panelayout.Resource, resourceview.TabKey(rf), true
	default:
		return contentlink.Ref{}, panelayout.Primary, "", false
	}
}

type documentViewer struct {
	view   *docview.Model
	source Source
}

func (v *documentViewer) model() any { return v.view }

func (v *documentViewer) bindLoader(ctx SurfaceContext, ref contentlink.Ref) {
	if v.view == nil {
		return
	}
	if v.source == nil {
		v.view.SetLoader(nil)
		return
	}
	src := v.source
	v.view.SetLoader(func(_, path string, epoch uint64, ifRevision string) tea.Cmd {
		req := ref
		req.Value = path
		return documentLoadCmd(src, ctx, req, ifRevision, epoch)
	})
}

func (v *documentViewer) load(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	root := ctx.Root
	// A resolved file reference may deliberately name a regular file outside
	// the source surface (for example ~/.config/sidecar/config.json or another
	// worktree). filepath.Join does not preserve an absolute second argument,
	// so give absolute references no root to join while relative references
	// retain the surface that gives them meaning.
	if filepath.IsAbs(filepath.FromSlash(ref.Value)) {
		root = ""
	}
	v.bindLoader(ctx, ref)
	// Arm already applied persisted render/wrap. Load resets them; put the
	// armed values back so restore does not turn a raw tab into rendered.
	armed := v.view.Title() != "" && v.view.NeedsLoad()
	rendered, wrap := v.view.Rendered(), v.view.Wrap()
	cmd := v.view.Load(id, root, ref.Value, ref.Line, ctx.Epoch)
	if armed {
		v.view.SetRendered(rendered)
		v.view.SetWrap(wrap)
		return cmd
	}
	// Load defaults every line-zero target to rendered. The shared content rule
	// is narrower: only Markdown opens rendered; source and plain text stay raw.
	v.view.SetRendered(terminallink.Markdown(ref.Value) && ref.Line == 0)
	return cmd
}
func (v *documentViewer) loadFile(ctx SurfaceContext, ref contentlink.Ref, id int, file *os.File) tea.Cmd {
	v.bindLoader(ctx, ref)
	cmd := v.view.LoadFile(id, file, ref.Value, ref.Line, ctx.Epoch)
	v.view.SetRendered(terminallink.Markdown(ref.Value) && ref.Line == 0)
	return cmd
}
func (v *documentViewer) reload(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	v.bindLoader(ctx, ref)
	if v.view.NeedsLoad() {
		return v.load(ctx, ref, id)
	}
	return v.view.Reload()
}
func (v *documentViewer) arm(ctx SurfaceContext, ref contentlink.Ref, id int, state TabState) {
	v.bindLoader(ctx, ref)
	v.view.Arm(id, ref.Value, ctx.Epoch)
	v.view.SetRendered(state.Rendered)
	v.view.SetWrap(state.Wrap)
	v.view.SetPendingScroll(state.Scroll)
}
func (v *documentViewer) focus(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	v.bindLoader(ctx, ref)
	if v.view.NeedsLoad() {
		return v.load(ctx, ref, id)
	}
	v.view.ApplyLine(ref.Line)
	return nil
}
func (v *documentViewer) apply(_ SurfaceContext, msg any) (tea.Cmd, bool) {
	m, ok := msg.(docview.LoadedMsg)
	return nil, ok && v.view.SetResult(m)
}
func (v *documentViewer) reference(ref contentlink.Ref) (contentlink.Ref, string) {
	return ref, ref.Value
}
func (v *documentViewer) snapshot(ref contentlink.Ref) TabState {
	return TabState{Ref: ref, Scroll: v.view.ScrollOffset(), Wrap: v.view.Wrap(), Rendered: v.view.Rendered()}
}

type issueViewer struct {
	view   *issueview.Model
	source Source
}

func (v *issueViewer) model() any { return v.view }

func (v *issueViewer) bindLoader(ctx SurfaceContext, ref contentlink.Ref) {
	if v.view == nil {
		return
	}
	if v.source == nil {
		v.view.SetLoader(nil)
		return
	}
	src := v.source
	view := v.view
	v.view.SetLoader(func(_, issueID string, epoch uint64, ifRevision string) tea.Cmd {
		req := ref
		req.Value = issueID
		return issueLoadCmd(src, ctx, req, ifRevision, epoch, view)
	})
}

func (v *issueViewer) load(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	root := ctx.Root
	if name, adopted := v.view.Owner(); name != "" && adopted != "" && !ctx.Source.Remote() {
		root = adopted
	}
	v.bindLoader(ctx, ref)
	return v.view.Load(id, root, ref.Value, ctx.Epoch)
}
func (v *issueViewer) reload(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	return v.load(ctx, ref, id)
}
func (v *issueViewer) arm(ctx SurfaceContext, ref contentlink.Ref, id int, state TabState) {
	v.bindLoader(ctx, ref)
	v.view.Arm(id, ref.Value, ctx.Epoch)
	v.view.RestoreOwner(state.OwnerName, state.OwnerRoot)
	v.view.SetPendingScroll(state.Scroll)
}
func (v *issueViewer) focus(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	v.bindLoader(ctx, ref)
	if v.view.NeedsLoad() {
		return v.load(ctx, ref, id)
	}
	return nil
}
func (v *issueViewer) apply(_ SurfaceContext, msg any) (tea.Cmd, bool) {
	m, ok := msg.(issueview.LoadedMsg)
	return nil, ok && v.view.SetResult(m)
}
func (v *issueViewer) reference(ref contentlink.Ref) (contentlink.Ref, string) {
	return ref, ref.Value
}
func (v *issueViewer) snapshot(ref contentlink.Ref) TabState {
	out := TabState{Ref: ref, Scroll: v.view.ScrollOffset()}
	if name, root := v.view.Owner(); name != "" && root != "" {
		out.OwnerName, out.OwnerRoot = name, root
	}
	return out
}

type noteViewer struct {
	view   *noteview.Model
	source Source
}

func (v *noteViewer) model() any { return v.view }

func (v *noteViewer) bindLoader(ctx SurfaceContext, ref contentlink.Ref) {
	if v.view == nil {
		return
	}
	if v.source == nil {
		v.view.SetLoader(nil)
		return
	}
	src := v.source
	v.view.SetLoader(func(_, noteID string, epoch uint64, ifRevision string) tea.Cmd {
		req := ref
		req.Value = noteID
		return noteLoadCmd(src, ctx, req, ifRevision, epoch)
	})
}

func (v *noteViewer) load(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	v.bindLoader(ctx, ref)
	return v.view.Load(id, ctx.Root, ref.Value, ctx.Epoch)
}
func (v *noteViewer) reload(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	return v.load(ctx, ref, id)
}
func (v *noteViewer) arm(ctx SurfaceContext, ref contentlink.Ref, id int, state TabState) {
	v.bindLoader(ctx, ref)
	v.view.Arm(id, ref.Value, ctx.Epoch)
	v.view.SetPendingScroll(state.Scroll)
}
func (v *noteViewer) focus(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	v.bindLoader(ctx, ref)
	if v.view.NeedsLoad() {
		return v.load(ctx, ref, id)
	}
	return nil
}
func (v *noteViewer) apply(_ SurfaceContext, msg any) (tea.Cmd, bool) {
	m, ok := msg.(noteview.LoadedMsg)
	return nil, ok && v.view.SetResult(m)
}
func (v *noteViewer) reference(ref contentlink.Ref) (contentlink.Ref, string) {
	return ref, ref.Value
}
func (v *noteViewer) snapshot(ref contentlink.Ref) TabState {
	return TabState{Ref: ref, Scroll: v.view.ScrollOffset()}
}

type diffViewer struct {
	view   *workspacediff.View
	source Source
}

func (v *diffViewer) model() any { return v.view }
func diffRoot(ctx SurfaceContext) string {
	if ctx.DiffRoot != "" {
		return ctx.DiffRoot
	}
	return ctx.Root
}
func (v *diffViewer) bindLoader(ctx SurfaceContext) {
	if v.view == nil {
		return
	}
	if v.source == nil {
		v.view.Loader = nil
		return
	}
	v.view.Loader = sourceDiffLoader{src: v.source, ctx: ctx}
}
func (v *diffViewer) load(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	target, ok := workspacediff.ParseSpec(ref.Value)
	if !ok {
		return nil
	}
	v.view.Target = target
	if target.Kind == workspacediff.TargetCommit {
		v.view.Focus = workspacediff.FocusCommitFiles
	}
	surface := ctx.DiffSurface
	if surface == "" {
		surface = ctx.Surface
	}
	root := diffRoot(ctx)
	v.bindLoader(ctx)
	v.view.BindGeneration(root, surface, ctx.Epoch, uint64(id))
	v.view.State = workspacediff.LoadStateLoading
	switch target.Kind {
	case workspacediff.TargetWorkingTree:
		return v.view.LoadSnapshotCmd(ctx.BaseRef, false)
	case workspacediff.TargetRange:
		return v.view.LoadRange()
	case workspacediff.TargetCommit:
		return v.view.LoadCommit(target.A)
	default:
		return nil
	}
}
func (v *diffViewer) reload(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	return v.load(ctx, ref, id)
}
func (v *diffViewer) arm(ctx SurfaceContext, ref contentlink.Ref, id int, state TabState) {
	target, _ := workspacediff.ParseSpec(ref.Value)
	v.bindLoader(ctx)
	v.view.Target = target
	surface := ctx.DiffSurface
	if surface == "" {
		surface = ctx.Surface
	}
	root := diffRoot(ctx)
	v.view.BindGeneration(root, surface, ctx.Epoch, uint64(id))
	v.view.State = workspacediff.LoadStateUnknown
	v.view.Scope = workspacediff.ParseScope(state.Scope)
	v.view.ViewMode = workspacediff.ParseViewMode(state.Mode)
	v.view.DiffScroll = max(state.Scroll, 0)
	if state.Path != "" {
		v.view.Target.Path = state.Path
	}
}
func (v *diffViewer) focus(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	if v.view.State == workspacediff.LoadStateUnknown || v.view.State == workspacediff.LoadStateLoading {
		return v.load(ctx, ref, id)
	}
	return nil
}
func (v *diffViewer) apply(ctx SurfaceContext, msg any) (tea.Cmd, bool) {
	expectedSurface := ctx.DiffSurface
	if expectedSurface == "" {
		expectedSurface = ctx.Surface
	}
	accepts := func(epoch uint64, surface, identity string) bool {
		return epoch == ctx.Epoch && surface == expectedSurface && identity == v.view.Target.Identity()
	}
	switch m := msg.(type) {
	case workspacediff.SnapshotMsg:
		if !accepts(m.Epoch, m.WorkspaceID, m.Identity) {
			return nil, false
		}
		return v.view.ApplySnapshotMsg(m, diffRoot(ctx), ctx.Surface), true
	case workspacediff.RangeMsg:
		if !accepts(m.Epoch, m.WorkspaceID, m.Identity) {
			return nil, false
		}
		return v.view.ApplyRangeMsg(m), true
	case workspacediff.CommitDetailMsg:
		if !accepts(m.Epoch, m.WorkspaceID, m.Identity) {
			return nil, false
		}
		return v.view.ApplyCommitDetail(m), true
	case workspacediff.CommitFileDiffMsg:
		if !accepts(m.Epoch, m.WorkspaceID, m.Identity) {
			return nil, false
		}
		return v.view.ApplyCommitFileDiff(m), true
	case workspacediff.WorkingTreeFileMsg:
		if !accepts(m.Epoch, m.WorkspaceID, m.Identity) {
			return nil, false
		}
		return v.view.ApplyWorkingTreeFile(m), true
	default:
		return nil, false
	}
}
func (v *diffViewer) reference(ref contentlink.Ref) (contentlink.Ref, string) {
	ref.Value = v.view.Target.Identity()
	return ref, ref.Value
}
func (v *diffViewer) snapshot(ref contentlink.Ref) TabState {
	path := v.view.SelectedFileName()
	return TabState{Ref: ref, Scope: v.view.Scope.Persist(), Mode: v.view.ViewMode.Persist(), Scroll: v.view.DiffScroll, Path: path}
}

// resourceViewer is the ONE Resource viewer, for all three of the leaf's tab
// shapes. It dispatches nothing itself: resourceview.Model answers a matched
// document with the resource card and a collection or row with the shared
// plugin browser, so both workspace projections inherit the shapes by holding
// the model they already hold and neither content.go learns a new kind.
type resourceViewer struct{ view *resourceview.Model }

func (v *resourceViewer) model() any { return v.view }
func (v *resourceViewer) load(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	return v.view.Load(id, resourceRef(ref), ctx.Epoch)
}
func (v *resourceViewer) reload(ctx SurfaceContext, ref contentlink.Ref, id int) tea.Cmd {
	return v.load(ctx, ref, id)
}
func (v *resourceViewer) arm(ctx SurfaceContext, ref contentlink.Ref, id int, state TabState) {
	rf := resourceRef(ref)
	// A collection tab's view position is restored before anything is listed,
	// so the first page is the one the user was reading rather than the
	// collection's default followed by a correction.
	rf.View, rf.Sort, rf.CursorID = state.View, state.Sort, state.CursorID
	rf.Filters = resource.FilterValues(state.Filters)
	v.view.Arm(id, rf, ctx.Epoch)
	v.view.SetPendingScroll(state.Scroll)
}
func (v *resourceViewer) focus(_ SurfaceContext, ref contentlink.Ref, _ int) tea.Cmd {
	// A focus may carry a view position the tab is not showing — an `open`
	// naming a query for a collection that is already open. The tab's identity
	// is the collection, so that open focuses rather than creating; dropping
	// the query here would silently answer a different question.
	return v.view.FocusRef(resourceRef(ref))
}
func (v *resourceViewer) apply(_ SurfaceContext, msg any) (tea.Cmd, bool) {
	if m, ok := msg.(resourceview.ResolvedMsg); ok {
		return nil, v.view.Apply(m)
	}
	// The plugin browser's own answers reach a collection or row tab the same
	// way a resolve reaches a card: as a broadcast the viewer either owns or
	// does not.
	if cmd, ok := v.view.ApplyPluginMsg(msg); ok {
		return cmd, true
	}
	return nil, false
}
func (v *resourceViewer) reference(ref contentlink.Ref) (contentlink.Ref, string) {
	rf := v.view.Reference()
	return refFromResource(ref, rf), resourceview.TabKey(rf)
}
func (v *resourceViewer) snapshot(ref contentlink.Ref) TabState {
	rf := v.view.Reference()
	return TabState{
		Ref: refFromResource(ref, rf), Scroll: v.view.Scroll(),
		View: rf.View, Sort: rf.Sort, CursorID: rf.CursorID,
		Filters: resource.FilterMap(rf.Filters),
	}
}

// resourceRef and refFromResource are the one projection between the content
// link vocabulary and the plugin reference, in both directions. Two spellings
// of this mapping is how a shape gets carried one way and dropped the other.
func resourceRef(ref contentlink.Ref) resource.Reference {
	return resource.Reference{
		Instance: ref.Provider, Matcher: ref.Matcher, Locator: ref.Value,
		Collection: ref.Collection, Query: ref.Query,
		Filters: resource.DecodeFilters(ref.Filters),
	}
}

func refFromResource(ref contentlink.Ref, rf resource.Reference) contentlink.Ref {
	ref.Provider, ref.Matcher, ref.Value = rf.Instance, rf.Matcher, rf.Locator
	ref.Collection, ref.Query = rf.Collection, rf.Query
	ref.Filters = resource.EncodeFilters(rf.Filters)
	return ref
}

// SetPluginCalls rebinds how every Resource tab reaches its protocol plugin,
// for existing tabs and for the ones a later open creates. It is the collection
// shape's counterpart to SetResourceResolver, and it starts nothing for the
// same reason: a load whose command is dropped leaves the tab loading forever.
func (d *Deck) SetPluginCalls(calls resourceview.CallsFor) {
	if d == nil {
		return
	}
	d.cfg.PluginCalls = calls
	d.ConfigureViewers(func(_ panelayout.Kind, model any) {
		if view, ok := model.(*resourceview.Model); ok {
			view.SetCallsFor(calls)
		}
	})
}
