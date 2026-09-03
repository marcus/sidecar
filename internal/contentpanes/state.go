package contentpanes

import (
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/panelayout"
)

const stateVersion = 1

// State is the reference-only persistence boundary for a Deck. It contains no
// loaded document, issue, diff, provider response, error, or rendered bytes.
type State struct {
	Version   int          `json:"version"`
	Source    *SourceState `json:"source,omitempty"`
	Root      *NodeState   `json:"root,omitempty"`
	FocusKind string       `json:"focusKind,omitempty"`
	Hidden    []PaneState  `json:"hidden,omitempty"`
}

// SourceState is the durable half of SourceContext. HostIncarnation, loaded
// bodies, and rendered rows are never serialized.
type SourceState struct {
	HostID        string `json:"hostId,omitempty"`
	ProjectKey    string `json:"projectKey,omitempty"`
	ProjectRoot   string `json:"projectRoot,omitempty"`
	WorkspaceID   string `json:"workspaceId,omitempty"`
	WorkspaceKind string `json:"workspaceKind,omitempty"`
	WorkspaceKey  string `json:"workspaceKey,omitempty"`
	Root          string `json:"root,omitempty"`
}

// NodeState is a pane tree. Leaf Pane is omitted for the primary surface.
type NodeState struct {
	Kind  string     `json:"kind,omitempty"`
	Axis  string     `json:"axis,omitempty"`
	Ratio int        `json:"ratio,omitempty"`
	A     *NodeState `json:"a,omitempty"`
	B     *NodeState `json:"b,omitempty"`
	Pane  *PaneState `json:"pane,omitempty"`
}

// PaneState is one homogeneous passive tab strip.
type PaneState struct {
	Kind   string     `json:"kind"`
	Active int        `json:"active,omitempty"`
	Tabs   []TabState `json:"tabs"`
}

// TabState contains target identity and user-owned view position only.
type TabState struct {
	Ref      contentlink.Ref `json:"ref"`
	Scroll   int             `json:"scroll,omitempty"`
	Wrap     bool            `json:"wrap,omitempty"`
	Rendered bool            `json:"rendered,omitempty"`
	Scope    string          `json:"scope,omitempty"`
	Mode     string          `json:"mode,omitempty"`
	Path     string          `json:"path,omitempty"`
	// OwnerName and OwnerRoot identify a cross-project issue tab's owning
	// store. Empty on a local issue. Restore reinstates the adoption before
	// the first load so the card does not re-run the search.
	OwnerName string `json:"ownerName,omitempty"`
	OwnerRoot string `json:"ownerRoot,omitempty"`
	// View, Sort, CursorID and Filters are a plugin collection tab's view position, the
	// rest of which (its query) is identity-adjacent enough to live on the Ref.
	// They are here for the reason Scope and Mode are: they are what the user
	// chose, not what the tab points at.
	View     string `json:"view,omitempty"`
	Sort     string `json:"sort,omitempty"`
	CursorID string `json:"cursorId,omitempty"`
	// Filters is the applied filter set the user chose, {id: value}. It sits
	// beside View and Sort for the same reason they do: it is a choice, not
	// part of what the tab points at.
	Filters map[string]string `json:"filters,omitempty"`
}

func persistSource(src SourceContext) *SourceState {
	out := SourceState{
		HostID:        src.HostID,
		ProjectKey:    src.ProjectKey,
		ProjectRoot:   src.ProjectRoot,
		WorkspaceID:   src.WorkspaceID,
		WorkspaceKind: string(src.WorkspaceKind),
		WorkspaceKey:  src.WorkspaceKey,
		Root:          src.Root,
	}
	if out == (SourceState{}) {
		return nil
	}
	return &out
}

// Encode projects the deck to references and view state only.
func (d *Deck) Encode() State {
	if d == nil {
		return State{Version: stateVersion}
	}
	state := State{Version: stateVersion, Source: persistSource(d.ctx.Source), Root: d.encodeNode(d.root)}
	if leaf := panelayout.Find(d.root, d.focus); leaf != nil && leaf.Split == nil {
		state.FocusKind = kindName(leaf.Kind)
	}
	for _, kind := range []panelayout.Kind{panelayout.Document, panelayout.Issue, panelayout.Note, panelayout.Diff, panelayout.Resource} {
		if p := d.hidden[kind]; p != nil {
			state.Hidden = append(state.Hidden, d.encodePane(p))
		}
	}
	return state
}

func (d *Deck) encodeNode(node *panelayout.Node) *NodeState {
	if node == nil {
		return nil
	}
	if node.Split != nil {
		axis := "columns"
		if node.Split.Axis == panelayout.Rows {
			axis = "rows"
		}
		return &NodeState{Axis: axis, Ratio: node.Split.Ratio, A: d.encodeNode(node.Split.A), B: d.encodeNode(node.Split.B)}
	}
	out := &NodeState{Kind: kindName(node.Kind)}
	if p := d.panes[node.ID]; p != nil {
		pane := d.encodePane(p)
		out.Pane = &pane
	}
	return out
}

func (d *Deck) encodePane(p *pane) PaneState {
	out := PaneState{Kind: kindName(p.kind), Active: p.active, Tabs: make([]TabState, 0, len(p.tabs))}
	for _, t := range p.tabs {
		out.Tabs = append(out.Tabs, t.view.snapshot(t.ref))
	}
	return out
}

// Decode restores references in an armed state without starting any load.
// Unknown kinds and invalid tabs collapse out of the tree. If the surviving
// state has no primary leaf, Decode returns a safe primary-only deck. Hosts
// must return [Deck.LoadVisible] after Decode, or every restored pane stays on
// its loading placeholder until a later select happens to start work.
func Decode(ctx SurfaceContext, cfg Config, state State) *Deck {
	d := New(ctx, cfg)
	seen := make(map[panelayout.Kind]bool)
	nextNodeID := 0
	root, hasPrimary := d.decodeNode(state.Root, seen, &nextNodeID)
	if root == nil || !hasPrimary {
		return d
	}
	d.root = root
	d.focus = panelayout.FirstOfKind(root, panelayout.Primary).ID
	if focusKind, ok := parseKind(state.FocusKind); ok {
		if leaf := panelayout.FirstOfKind(root, focusKind); leaf != nil {
			d.focus = leaf.ID
		}
	}
	for _, saved := range state.Hidden {
		kind, ok := parseKind(saved.Kind)
		if !ok || kind == panelayout.Primary || seen[kind] {
			continue
		}
		p := d.decodePane(saved, kind, 0)
		if p == nil {
			continue
		}
		d.hidden[kind] = p
		seen[kind] = true
	}
	return d
}

func (d *Deck) decodeNode(saved *NodeState, seen map[panelayout.Kind]bool, nextID *int) (*panelayout.Node, bool) {
	if saved == nil {
		return nil, false
	}
	if saved.A != nil || saved.B != nil {
		a, aPrimary := d.decodeNode(saved.A, seen, nextID)
		b, bPrimary := d.decodeNode(saved.B, seen, nextID)
		if a == nil {
			return b, bPrimary
		}
		if b == nil {
			return a, aPrimary
		}
		axis := panelayout.Columns
		if saved.Axis == "rows" {
			axis = panelayout.Rows
		}
		ratio := saved.Ratio
		if ratio == 0 {
			ratio = 50
		}
		*nextID++
		return &panelayout.Node{ID: *nextID, Split: &panelayout.Split{
			Axis: axis, Ratio: panelayout.ClampRatio(ratio), A: a, B: b,
		}}, aPrimary || bPrimary
	}
	kind, ok := parseKind(saved.Kind)
	if !ok || seen[kind] {
		return nil, false
	}
	if kind == panelayout.Primary {
		seen[kind] = true
		*nextID++
		return &panelayout.Node{ID: *nextID, Kind: kind}, true
	}
	if saved.Pane == nil {
		return nil, false
	}
	p := d.decodePane(*saved.Pane, kind, 0)
	if p == nil {
		return nil, false
	}
	seen[kind] = true
	*nextID++
	p.leafID = *nextID
	d.panes[p.leafID] = p
	return &panelayout.Node{ID: p.leafID, Kind: kind, ContentID: p.leafID}, false
}

func (d *Deck) decodePane(saved PaneState, kind panelayout.Kind, leafID int) *pane {
	p := &pane{kind: kind, leafID: leafID}
	identities := make(map[string]bool)
	for i, state := range saved.Tabs {
		ref, gotKind, identity, ok := normalizeRef(d.ctx, state.Ref)
		if !ok || gotKind != kind || identities[identity] {
			continue
		}
		identities[identity] = true
		d.nextTabID++
		v := newViewer(d.cfg, kind)
		v.arm(d.ctx, ref, int(d.nextTabID), state)
		if i == saved.Active {
			p.active = len(p.tabs)
		}
		p.tabs = append(p.tabs, &tab{id: d.nextTabID, identity: identity, ref: ref, view: v, ctx: d.ctx})
	}
	if len(p.tabs) == 0 {
		return nil
	}
	if p.active < 0 || p.active >= len(p.tabs) {
		p.active = 0
	}
	return p
}

func parseKind(raw string) (panelayout.Kind, bool) {
	switch raw {
	case "primary", "terminal":
		return panelayout.Primary, true
	case "document", "doc":
		return panelayout.Document, true
	case "issue":
		return panelayout.Issue, true
	case "note":
		return panelayout.Note, true
	case "diff":
		return panelayout.Diff, true
	case "resource":
		return panelayout.Resource, true
	default:
		return 0, false
	}
}
