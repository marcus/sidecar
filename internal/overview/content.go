package overview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/paneframe"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/termpreview"
)

// Content, Size and Render are the shared frame's contract, aliased rather than
// redeclared so a leaf on this surface and a leaf in the project workspace
// plugin are the same type.
type (
	Content = paneframe.Content
	Size    = paneframe.Size
	Render  = paneframe.Render
)

// Content kinds are the registry keys the adapter is chosen by. They match the
// project plugin's, because a leaf must not be written under one name on one
// surface and restored as another on the other.
const (
	contentKindTerminal = "terminal"
	contentKindDoc      = "doc"
	contentKindIssue    = "issue"
	contentKindDiff     = "diff"
	// One key for every external provider. A leaf is a Resource leaf; which
	// provider filled it is the tab's business, not the layout's.
	contentKindResource = "resource"
	contentKindNote     = "note"
)

// paneContent adapts a leaf to the content contract. It is the one place that
// maps a leaf kind to an implementation, so nothing in the render path asks what
// kind of leaf it is drawing. A leaf whose content is gone has none, and the
// canvas leaves its box blank rather than letting a neighbour spread into it.
func (m *Model) paneContent(node *panelayout.Node) Content {
	if node == nil || node.Split != nil {
		return nil
	}
	switch node.Kind {
	case panelayout.Document:
		if m.preview.doc == nil || m.preview.doc.view() == nil {
			return nil
		}
		return &docContent{m: m, doc: m.preview.doc}
	case panelayout.Issue:
		if m.preview.issue == nil || m.preview.issue.view() == nil {
			return nil
		}
		return &issueContent{m: m, issue: m.preview.issue}
	case panelayout.Diff:
		if m.preview.diff == nil || m.preview.diff.view() == nil {
			return nil
		}
		return &diffContent{m: m, diff: m.preview.diff}
	case panelayout.Resource:
		if m.preview.resource == nil || m.preview.resource.view() == nil {
			return nil
		}
		return &resourceContent{m: m, res: m.preview.resource}
	case panelayout.Note:
		if m.preview.note == nil || m.preview.note.view() == nil {
			return nil
		}
		return &noteContent{m: m, note: m.preview.note}
	default:
		return &terminalContent{m: m, leafID: node.ID, kind: node.Kind}
	}
}

// terminalContent is the live pane leaf. Its header row is drawn from inside its
// own body by the shared buffer renderer, exactly as on the project surface.
type terminalContent struct {
	m      *Model
	size   Size
	leafID int
	kind   panelayout.Kind
}

func (c *terminalContent) Kind() string { return contentKindTerminal }

// Title is the workspace this pane is showing, which is the name the list chose
// it by.
func (c *terminalContent) Title() string {
	if c.kind == panelayout.Shell {
		return c.m.terminalLeaf(c.leafID).Name
	}
	if workspace, ok := c.m.SelectedWorkspace(); ok {
		return workspace.Name
	}
	return ""
}

// SetSize records the box and nothing else. The live pane is resized from
// syncTerminalGeometry, on the state change that moved the box; a resize
// returned here would put a SIGWINCH into the agent on every frame.
func (c *terminalContent) SetSize(size Size) tea.Cmd {
	c.size = size
	return nil
}

func (c *terminalContent) View(Render) string {
	return c.m.renderOutputTerminalLeaf(c.leafID, c.kind, c.size.Width, c.size.Height)
}

// docContent is the document leaf: the pane's own header row above a document
// viewport.
type docContent struct {
	m    *Model
	doc  *previewDoc
	size Size
}

func (c *docContent) Kind() string { return contentKindDoc }

func (c *docContent) Title() string {
	if view := c.doc.view(); view != nil {
		return view.Title()
	}
	return ""
}

func (c *docContent) SetSize(size Size) tea.Cmd {
	c.size = size
	if view := c.doc.view(); view != nil {
		view.SetSize(size.Width, max(size.Height-termpreview.HeaderRows, 0))
	}
	return c.m.preparePreviewDocFrame(c.doc)
}

// View draws the leaf at its own origin. Where the box is, not only how big it
// is: a pointer gesture over the document's text is hit-tested against it.
func (c *docContent) View(render Render) string {
	return c.m.renderPreviewDoc(c.doc, termpreview.Box{
		X: render.Origin.X, Y: render.Origin.Y, W: c.size.Width, H: c.size.Height,
	})
}

// issueContent is the td issue leaf.
type issueContent struct {
	m     *Model
	issue *previewIssue
	size  Size
}

func (c *issueContent) Kind() string { return contentKindIssue }

func (c *issueContent) Title() string {
	if view := c.issue.view(); view != nil {
		return view.Title()
	}
	return ""
}

func (c *issueContent) SetSize(size Size) tea.Cmd {
	c.size = size
	return nil
}

// View draws the leaf at its own origin. Where the box is, not only how big it
// is: a pointer gesture over the card's text is hit-tested against it.
func (c *issueContent) View(render Render) string {
	return c.m.renderPreviewIssue(c.issue, termpreview.Box{
		X: render.Origin.X, Y: render.Origin.Y, W: c.size.Width, H: c.size.Height,
	})
}

// diffContent is the Diff leaf.
type diffContent struct {
	m    *Model
	diff *previewDiff
	size Size
}

func (c *diffContent) Kind() string { return contentKindDiff }

func (c *diffContent) Title() string {
	if view := c.diff.view(); view != nil {
		return view.Target.TabLabel()
	}
	return "Diff"
}

func (c *diffContent) SetSize(size Size) tea.Cmd {
	c.size = size
	return nil
}

// View draws the leaf at its own origin. Where the box is, not only how big it
// is: a pointer gesture over the patch is hit-tested against it.
func (c *diffContent) View(render Render) string {
	return c.m.renderPreviewDiff(c.diff, termpreview.Box{
		X: render.Origin.X, Y: render.Origin.Y, W: c.size.Width, H: c.size.Height,
	})
}

// resourceContent is the external-resource leaf: one Resource pane per surface,
// whatever provider answered for the tabs inside it.
type resourceContent struct {
	m    *Model
	res  *previewResource
	size Size
}

func (c *resourceContent) Kind() string { return contentKindResource }

func (c *resourceContent) Title() string {
	if view := c.res.view(); view != nil {
		return view.Title()
	}
	return "Resource"
}

func (c *resourceContent) SetSize(size Size) tea.Cmd {
	c.size = size
	return nil
}

// View draws the leaf at its own origin. Where the box is, not only how big it
// is: a pointer gesture over the card's text is hit-tested against it.
func (c *resourceContent) View(render Render) string {
	return c.m.renderPreviewResource(c.res, termpreview.Box{
		X: render.Origin.X, Y: render.Origin.Y, W: c.size.Width, H: c.size.Height,
	})
}

type noteContent struct {
	m    *Model
	note *previewNote
	size Size
}

func (c *noteContent) Kind() string { return contentKindNote }

func (c *noteContent) Title() string {
	if view := c.note.view(); view != nil {
		return view.Title()
	}
	return ""
}

func (c *noteContent) SetSize(size Size) tea.Cmd {
	c.size = size
	return nil
}

func (c *noteContent) View(Render) string {
	return c.m.renderPreviewNote(c.note, termpreview.Box{W: c.size.Width, H: c.size.Height})
}
