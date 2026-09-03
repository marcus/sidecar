// Package panecodec owns the projection between *state.PaneLayoutJSON and
// (contentpanes.State + live-leaf records).
//
// Per-kind tab identity — tree shape, refs, scroll, unknown-kind collapse —
// already lives in contentpanes.Encode/Decode. This package maps the two
// vocabularies (kind names, split axis names, tab field names) and supplies
// the live-leaf persistence shape termpanes lacks: session name, leaf name,
// and the parent split's axis/ratio for Primary and Shell leaves.
//
// PaneLayoutJSON uses kind terminal|shell|doc|issue|note|diff|resource and
// axis cols|rows. contentpanes.State uses primary|document|issue|note|diff|
// resource and axis columns|rows. "document" and "primary" are never written
// into PaneLayoutJSON. Session on a shell leaf is a tmux session name, never
// a pane id.
//
// A shell node is represented in contentpanes.State as Kind "shell" so the
// tree round-trips through this package. contentpanes.Decode treats that as
// an unknown kind and drops it; hosts re-request the leaf from Live records.
package panecodec

import (
	"github.com/marcus/sidecar/internal/contentlink"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/state"
)

// Wire kind names for PaneLayoutJSON. These are the only spellings Encode writes.
const (
	KindTerminal = "terminal"
	KindShell    = "shell"
	KindDoc      = "doc"
	KindIssue    = "issue"
	KindNote     = "note"
	KindDiff     = "diff"
	KindResource = "resource"
)

const (
	axisCols = "cols"
	axisRows = "rows"

	stateKindPrimary  = "primary"
	stateKindDocument = "document"
	stateKindShell    = "shell"
	stateAxisColumns  = "columns"
	stateAxisRows     = "rows"

	docModeRaw      = "raw"
	docModeRendered = "rendered"
)

// Live is the persistable identity of one live terminal leaf.
type Live struct {
	Kind    string // KindTerminal or KindShell
	Session string // tmux session name, never a pane id
	Name    string
	// Axis and Ratio describe the parent split that placed this leaf, in
	// PaneLayoutJSON vocabulary (cols/rows). They let a host re-splice a
	// shell that contentpanes.Decode dropped.
	Axis     string
	Ratio    int
	NewFirst bool
}

// Options carries host-owned input the projection cannot invent. Encode reads
// Live to overlay session/name onto terminal and shell leaves. Decode uses
// AcceptTab for host target resolution (missing files, path escape) and
// returns the live-leaf records it extracted.
type Options struct {
	Live []Live
	// AcceptTab, when set, is host-owned tab admission. Decode drops a tab
	// for which it returns false. Nil admits every structurally valid tab.
	AcceptTab func(kind string, tab contentpanes.TabState) bool
}

// Encode projects contentpanes.State plus live-leaf records onto PaneLayoutJSON.
// Empty content leaves (no tabs) are omitted the way encodePaneNode omitted them.
// Root, Surface, and Open are host policy and are left unset.
func Encode(st contentpanes.State, opts Options) *state.PaneLayoutJSON {
	out := encodeNode(st.Root, opts.Live)
	if out == nil {
		return nil
	}
	if st.FocusKind != "" {
		out.FocusKind = jsonKind(st.FocusKind)
	}
	return out
}

// Decode projects PaneLayoutJSON onto contentpanes.State plus live-leaf records.
// Unknown kinds are preserved in State so contentpanes.Decode can drop them and
// collapse their splits. Legacy issue Issue/Scroll becomes a one-tab IssueTabs.
func Decode(layout *state.PaneLayoutJSON, opts Options) (contentpanes.State, []Live) {
	if layout == nil {
		return contentpanes.State{Version: 1}, nil
	}
	var live []Live
	root := decodeNode(layout, "", 0, false, &live, opts)
	st := contentpanes.State{Version: 1, Root: root}
	if layout.FocusKind != "" {
		st.FocusKind = stateKind(layout.FocusKind)
	}
	return st, live
}

func encodeNode(n *contentpanes.NodeState, live []Live) *state.PaneLayoutJSON {
	if n == nil {
		return nil
	}
	if n.A != nil || n.B != nil {
		axis := axisCols
		if n.Axis == stateAxisRows {
			axis = axisRows
		}
		return &state.PaneLayoutJSON{Split: &state.PaneSplitJSON{
			Axis:  axis,
			Ratio: panelayout.ClampRatio(n.Ratio),
			A:     encodeNode(n.A, live),
			B:     encodeNode(n.B, live),
		}}
	}
	kind := jsonKind(n.Kind)
	if kind == "" {
		return nil
	}
	if kind == KindTerminal || kind == KindShell {
		out := &state.PaneLayoutJSON{Kind: kind}
		applyLive(out, live)
		return out
	}
	if n.Pane == nil || len(n.Pane.Tabs) == 0 {
		return nil
	}
	out := &state.PaneLayoutJSON{Kind: kind, Active: n.Pane.Active}
	encodeTabs(out, n.Pane)
	if !hasTabs(out) {
		return nil
	}
	return out
}

func decodeNode(j *state.PaneLayoutJSON, parentAxis string, parentRatio int, newFirst bool, live *[]Live, opts Options) *contentpanes.NodeState {
	if j == nil {
		return nil
	}
	if j.Split != nil {
		axis := stateAxisColumns
		if j.Split.Axis == axisRows {
			axis = stateAxisRows
		}
		a := decodeNode(j.Split.A, j.Split.Axis, j.Split.Ratio, true, live, opts)
		b := decodeNode(j.Split.B, j.Split.Axis, j.Split.Ratio, false, live, opts)
		if a == nil {
			return b
		}
		if b == nil {
			return a
		}
		return &contentpanes.NodeState{Axis: axis, Ratio: j.Split.Ratio, A: a, B: b}
	}
	switch j.Kind {
	case KindTerminal:
		*live = append(*live, Live{Kind: KindTerminal, Session: j.Session, Name: j.Name, Axis: parentAxis, Ratio: parentRatio, NewFirst: newFirst})
		return &contentpanes.NodeState{Kind: stateKindPrimary}
	case KindShell:
		if liveHas(*live, KindShell) {
			return nil
		}
		*live = append(*live, Live{Kind: KindShell, Session: j.Session, Name: j.Name, Axis: parentAxis, Ratio: parentRatio, NewFirst: newFirst})
		return &contentpanes.NodeState{Kind: stateKindShell}
	case KindDoc, KindIssue, KindNote, KindDiff, KindResource:
		pane := decodePane(j, opts)
		if pane == nil {
			return nil
		}
		return &contentpanes.NodeState{Kind: stateKind(j.Kind), Pane: pane}
	default:
		if j.Kind == "" {
			return nil
		}
		// Preserve unknown kinds so contentpanes.Decode can drop them.
		n := &contentpanes.NodeState{Kind: j.Kind}
		if pane := decodePane(j, opts); pane != nil {
			n.Pane = pane
		}
		return n
	}
}

func encodeTabs(out *state.PaneLayoutJSON, pane *contentpanes.PaneState) {
	if pane == nil {
		return
	}
	switch jsonKind(pane.Kind) {
	case KindDoc:
		tabs := make([]state.PaneDocTabJSON, 0, len(pane.Tabs))
		active := 0
		for i, tab := range pane.Tabs {
			path := tab.Ref.Value
			if path == "" {
				path = tab.Path
			}
			if path == "" {
				continue
			}
			if i == pane.Active {
				active = len(tabs)
			}
			mode := docModeRaw
			if tab.Rendered {
				mode = docModeRendered
			}
			tabs = append(tabs, state.PaneDocTabJSON{Path: path, Mode: mode, Wrap: tab.Wrap, Scroll: tab.Scroll})
		}
		out.Tabs, out.Active = tabs, active
	case KindIssue:
		tabs := make([]state.PaneIssueTabJSON, 0, len(pane.Tabs))
		active := 0
		for i, tab := range pane.Tabs {
			id := tab.Ref.Value
			if id == "" {
				continue
			}
			if i == pane.Active {
				active = len(tabs)
			}
			item := state.PaneIssueTabJSON{Issue: id, Scroll: tab.Scroll}
			if tab.OwnerName != "" && tab.OwnerRoot != "" {
				item.OwnerName, item.OwnerRoot = tab.OwnerName, tab.OwnerRoot
			}
			tabs = append(tabs, item)
		}
		out.IssueTabs, out.Active = tabs, active
	case KindNote:
		tabs := make([]state.PaneNoteTabJSON, 0, len(pane.Tabs))
		active := 0
		for i, tab := range pane.Tabs {
			id := tab.Ref.Value
			if id == "" {
				continue
			}
			if i == pane.Active {
				active = len(tabs)
			}
			tabs = append(tabs, state.PaneNoteTabJSON{Note: id, Scroll: tab.Scroll})
		}
		out.NoteTabs, out.Active = tabs, active
	case KindDiff:
		tabs := make([]state.PaneDiffTabJSON, 0, len(pane.Tabs))
		active := 0
		for i, tab := range pane.Tabs {
			spec := tab.Ref.Value
			if spec == "" {
				continue
			}
			if i == pane.Active {
				active = len(tabs)
			}
			tabs = append(tabs, state.PaneDiffTabJSON{
				Spec: spec, Path: tab.Path, Scope: tab.Scope, Mode: tab.Mode, Scroll: tab.Scroll,
			})
		}
		out.DiffTabs, out.Active = tabs, active
	case KindResource:
		tabs := make([]state.PaneResourceTabJSON, 0, len(pane.Tabs))
		active := 0
		for i, tab := range pane.Tabs {
			saved, ok := resourceTabJSON(tab)
			if !ok {
				continue
			}
			if i == pane.Active {
				active = len(tabs)
			}
			tabs = append(tabs, saved)
		}
		out.ResourceTabs, out.Active = tabs, active
	}
}

func decodePane(j *state.PaneLayoutJSON, opts Options) *contentpanes.PaneState {
	if j == nil {
		return nil
	}
	pane := contentpanes.PaneState{Kind: stateKind(j.Kind)}
	admit := func(orig int, tab contentpanes.TabState) {
		if opts.AcceptTab != nil && !opts.AcceptTab(j.Kind, tab) {
			return
		}
		if orig == j.Active {
			pane.Active = len(pane.Tabs)
		}
		pane.Tabs = append(pane.Tabs, tab)
	}
	switch j.Kind {
	case KindDoc:
		for i, tab := range j.Tabs {
			if tab.Path == "" {
				continue
			}
			admit(i, contentpanes.TabState{
				Ref:      contentlink.Ref{Kind: contentlink.KindFile, Value: tab.Path},
				Scroll:   tab.Scroll,
				Wrap:     tab.Wrap,
				Rendered: tab.Mode != docModeRaw,
			})
		}
	case KindIssue:
		raw := issueTabs(j)
		for i, tab := range raw {
			if tab.Issue == "" {
				continue
			}
			item := contentpanes.TabState{
				Ref:    contentlink.Ref{Kind: contentlink.KindIssue, Value: tab.Issue},
				Scroll: tab.Scroll,
			}
			if tab.OwnerName != "" && tab.OwnerRoot != "" {
				item.OwnerName, item.OwnerRoot = tab.OwnerName, tab.OwnerRoot
			}
			admit(i, item)
		}
	case KindNote:
		for i, tab := range j.NoteTabs {
			if tab.Note == "" {
				continue
			}
			admit(i, contentpanes.TabState{
				Ref:    contentlink.Ref{Kind: contentlink.KindInternal, Namespace: "note", Value: tab.Note},
				Scroll: tab.Scroll,
			})
		}
	case KindDiff:
		for i, tab := range j.DiffTabs {
			if tab.Spec == "" {
				continue
			}
			admit(i, contentpanes.TabState{
				Ref:    contentlink.Ref{Kind: contentlink.KindDiff, Value: tab.Spec},
				Scroll: tab.Scroll,
				Scope:  tab.Scope,
				Mode:   tab.Mode,
				Path:   tab.Path,
			})
		}
	case KindResource:
		for i, tab := range j.ResourceTabs {
			state, ok := resourceTabState(tab)
			if !ok {
				continue
			}
			admit(i, state)
		}
	default:
		return nil
	}
	if len(pane.Tabs) == 0 {
		return nil
	}
	if pane.Active < 0 || pane.Active >= len(pane.Tabs) {
		pane.Active = 0
	}
	return &pane
}

func issueTabs(j *state.PaneLayoutJSON) []state.PaneIssueTabJSON {
	if j.IssueTabs != nil {
		return j.IssueTabs
	}
	if j.Issue != "" {
		return []state.PaneIssueTabJSON{{Issue: j.Issue, Scroll: j.Scroll}}
	}
	return nil
}

func liveHas(live []Live, kind string) bool {
	for _, l := range live {
		if l.Kind == kind {
			return true
		}
	}
	return false
}

func applyLive(j *state.PaneLayoutJSON, live []Live) {
	if j == nil {
		return
	}
	for _, l := range live {
		if l.Kind != j.Kind {
			continue
		}
		if l.Session != "" {
			j.Session = l.Session
		}
		if l.Name != "" {
			j.Name = l.Name
		}
	}
}

func hasTabs(j *state.PaneLayoutJSON) bool {
	if j == nil {
		return false
	}
	return len(j.Tabs) > 0 || len(j.IssueTabs) > 0 || len(j.NoteTabs) > 0 ||
		len(j.DiffTabs) > 0 || len(j.ResourceTabs) > 0
}

func jsonKind(stateKind string) string {
	switch stateKind {
	case stateKindPrimary, KindTerminal:
		return KindTerminal
	case stateKindDocument, KindDoc:
		return KindDoc
	case KindIssue, KindNote, KindDiff, KindResource, KindShell:
		return stateKind
	default:
		return stateKind
	}
}

func stateKind(jsonKind string) string {
	switch jsonKind {
	case KindTerminal:
		return stateKindPrimary
	case KindDoc:
		return stateKindDocument
	default:
		return jsonKind
	}
}

// resourceTabJSON projects one Resource tab onto its persisted record, choosing
// the shape from the reference rather than from anything the writer remembered
// to set. A tab that is no shape is dropped rather than written half-formed.
func resourceTabJSON(tab contentpanes.TabState) (state.PaneResourceTabJSON, bool) {
	ref := tab.Ref
	if ref.Provider == "" {
		return state.PaneResourceTabJSON{}, false
	}
	out := state.PaneResourceTabJSON{Provider: ref.Provider, Scroll: tab.Scroll}
	switch {
	case ref.Collection != "" && ref.Matcher == "" && ref.Value == "":
		out.Collection = ref.Collection
		out.Query, out.View, out.Sort, out.CursorID = ref.Query, tab.View, tab.Sort, tab.CursorID
		out.Filters = tab.Filters
	case ref.Collection != "" && ref.Matcher == "" && ref.Value != "":
		out.Collection, out.Locator = ref.Collection, ref.Value
	case ref.Collection == "" && ref.Matcher != "" && ref.Value != "":
		out.Matcher, out.Locator = ref.Matcher, ref.Value
	default:
		return state.PaneResourceTabJSON{}, false
	}
	return out, true
}

// resourceTabState is the inverse, and it is where an ambiguous record is
// refused. A record naming both a matcher and a collection, or naming neither,
// is dropped: which one it meant is not knowable here, and guessing would
// restore a tab pointing at something the user never opened.
func resourceTabState(tab state.PaneResourceTabJSON) (contentpanes.TabState, bool) {
	if tab.Provider == "" {
		return contentpanes.TabState{}, false
	}
	ref := contentlink.Ref{Kind: contentlink.KindResource, Provider: tab.Provider}
	out := contentpanes.TabState{Scroll: tab.Scroll}
	switch {
	case tab.Collection != "" && tab.Matcher == "" && tab.Locator == "":
		ref.Collection, ref.Query = tab.Collection, tab.Query
		out.View, out.Sort, out.CursorID = tab.View, tab.Sort, tab.CursorID
		out.Filters = tab.Filters
	case tab.Collection != "" && tab.Matcher == "" && tab.Locator != "":
		ref.Collection, ref.Value = tab.Collection, tab.Locator
	case tab.Collection == "" && tab.Matcher != "" && tab.Locator != "":
		ref.Matcher, ref.Value = tab.Matcher, tab.Locator
	default:
		return contentpanes.TabState{}, false
	}
	out.Ref = ref
	return out, true
}
