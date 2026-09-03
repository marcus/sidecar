package layoutapply

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/panecodec"
	"github.com/marcus/sidecar/internal/panelayout"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/uirequest"
)

func applySpec(h Host, req uirequest.Request, payload uirequest.LayoutPayload, root, surface string) tea.Cmd {
	columns, err := uirequest.DecodeLayoutColumns(payload.Columns)
	if err != nil {
		h.Ack(req, uirequest.StatusDeclined, "invalid layout spec: "+err.Error(), nil, nil)
		return nil
	}
	spec := uirequest.LayoutSpec{Columns: columns}
	if err := uirequest.ValidateLayoutSpec(spec); err != nil {
		h.Ack(req, uirequest.StatusDeclined, err.Error(), nil, nil)
		return nil
	}

	items := make([]ItemPlan, 0, len(spec.Columns)*panelayout.MaxGridRows)
	for _, column := range spec.Columns {
		for _, pane := range column.Panes {
			items = append(items, ItemPlan{Spec: pane})
		}
	}

	liveSessions := h.LiveShellSessions()
	firstViolation := -1
	note := func(i int, verdict, reason string) {
		items[i].Verdict, items[i].Reason = verdict, reason
		if firstViolation < 0 && verdict == uirequest.ItemVerdictDeclined && reason != "" {
			firstViolation = i
		}
	}

	newShells := 0
	passiveSeen := make(map[panelayout.Kind]int)
	for i := range items {
		item := &items[i]
		kind, known := panelayout.KindByName(strings.TrimSpace(item.Spec.Kind))
		if !known {
			note(i, uirequest.ItemVerdictDeclined, fmt.Sprintf("unknown pane kind %q", item.Spec.Kind))
			continue
		}
		item.Kind = kind
		switch item.Kind {
		case panelayout.Primary:
			continue
		case panelayout.Shell:
			if item.Spec.Session != "" {
				if !liveSessions[item.Spec.Session] {
					note(i, uirequest.ItemVerdictDeclined,
						fmt.Sprintf("no live terminal named %q is on screen; shells are carried by session as layout get prints them", item.Spec.Session))
				}
				continue
			}
			if h.SplitOrigin() == "" {
				note(i, uirequest.ItemVerdictDeclined, SpecOriginRequired)
				continue
			}
			if !h.TerminalEnabled() {
				note(i, uirequest.ItemVerdictDeclined, h.TerminalOffReason())
				continue
			}
			if h.TermPanelSessionName() == "" {
				note(i, uirequest.ItemVerdictDeclined, SpecOriginRequired)
				continue
			}
			newShells++
		default:
			passiveSeen[item.Kind]++
			if passiveSeen[item.Kind] > 1 {
				note(i, uirequest.ItemVerdictDeclined,
					fmt.Sprintf("a %s pane is already part of this spec; passive kinds keep one pane each", item.Kind.Name()))
				continue
			}
			targets, refusal := h.ResolveTargets(item.Kind, item.Spec)
			if refusal != "" {
				note(i, uirequest.ItemVerdictDeclined, refusal)
				continue
			}
			item.Targets = targets
		}
	}

	globalFailure := ""
	carried := 0
	for i := range items {
		if items[i].Kind == panelayout.Shell && items[i].Spec.Session != "" && items[i].Verdict != uirequest.ItemVerdictDeclined {
			carried++
		}
	}
	for _, session := range sortedSessionKeys(liveSessions) {
		covered := false
		for i := range items {
			if items[i].Kind == panelayout.Shell && items[i].Spec.Session == session {
				covered = true
				break
			}
		}
		if !covered {
			globalFailure = fmt.Sprintf("this spec omits the live terminal %q; carry it with {\"kind\":\"shell\",\"session\":\"%s\"} or close it first", session, session)
			break
		}
	}
	if globalFailure == "" && 1+carried+newShells > panelayout.LiveLeafCap {
		globalFailure = panelayout.LiveCapMessage
	}
	if globalFailure != "" {
		for i := range items {
			if items[i].Verdict != uirequest.ItemVerdictDeclined {
				note(i, uirequest.ItemVerdictDeclined, globalFailure)
			}
		}
	}

	trial, layout := buildSpecTrees(spec, items)

	if firstViolation < 0 {
		failure := ""
		if peer, placed := h.PeerBox(); !placed {
			failure = tooSmall
		} else if _, _, fits := panelayout.LayoutPanes(panelayout.Clone(trial), peer, h.Floors()); !fits {
			failure = needsLarger
		}
		if failure != "" {
			for i := range items {
				if items[i].Verdict != uirequest.ItemVerdictDeclined {
					note(i, uirequest.ItemVerdictDeclined, failure)
				}
			}
		}
	}

	if firstViolation >= 0 {
		for i := range items {
			if items[i].Verdict != uirequest.ItemVerdictDeclined && items[i].Reason == "" {
				note(i, uirequest.ItemVerdictDeclined, "would have opened; the spec declined before commit")
			}
		}
		h.Ack(req, uirequest.StatusDeclined, items[firstViolation].Reason, LayoutAcks(h, items, surface, false), nil)
		return nil
	}

	for i := range items {
		if items[i].Verdict != "" {
			continue
		}
		if specPaneIsCarried(items[i]) {
			items[i].Verdict = uirequest.ItemVerdictCarried
			continue
		}
		items[i].Verdict = uirequest.ItemVerdictOpened
	}

	layout.Root = root
	layout.Surface = surface
	layout.Open = true
	cmds := []tea.Cmd{}
	if cmd := h.RestoreSpec(layout); cmd != nil {
		cmds = append(cmds, cmd)
	}

	for i := range items {
		item := &items[i]
		if item.Kind != panelayout.Shell || item.Spec.Session != "" {
			continue
		}
		verdict, reason, cmd := h.AdoptSpecShell(item.Spec)
		item.Verdict, item.Reason = verdict, reason
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	h.AfterSpecCommit()
	h.Ack(req, uirequest.StatusOpened, "", LayoutAcks(h, items, surface, true), nil)
	return tea.Batch(cmds...)
}

func buildSpecTrees(spec uirequest.LayoutSpec, items []ItemPlan) (*panelayout.Node, *state.PaneLayoutJSON) {
	type builtColumn struct {
		node  *panelayout.Node
		saved *state.PaneLayoutJSON
	}
	built := make([]builtColumn, 0, len(spec.Columns))
	index, nextID := 0, 1
	for _, column := range spec.Columns {
		nodes := make([]*panelayout.Node, 0, len(column.Panes))
		saved := make([]*state.PaneLayoutJSON, 0, len(column.Panes))
		for range column.Panes {
			item := &items[index]
			nodes = append(nodes, &panelayout.Node{ID: nextID, Kind: item.Kind})
			nextID++
			saved = append(saved, specLeafJSON(item))
			index++
		}
		built = append(built, builtColumn{node: stackRows(nodes), saved: stackSavedRows(saved)})
	}
	columnNodes := make([]*panelayout.Node, 0, len(built))
	columnSaved := make([]*state.PaneLayoutJSON, 0, len(built))
	for _, b := range built {
		columnNodes = append(columnNodes, b.node)
		columnSaved = append(columnSaved, b.saved)
	}
	return chainColumns(columnNodes), chainColumnsSaved(columnSaved)
}

func specLeafJSON(item *ItemPlan) *state.PaneLayoutJSON {
	switch item.Kind {
	case panelayout.Primary:
		return &state.PaneLayoutJSON{Kind: panecodec.KindTerminal}
	case panelayout.Shell:
		return &state.PaneLayoutJSON{Kind: panecodec.KindShell, Session: item.Spec.Session}
	default:
		saved := &state.PaneLayoutJSON{Kind: specStateKind(item.Kind)}
		for _, t := range item.Targets {
			switch item.Kind {
			case panelayout.Document:
				saved.Tabs = append(saved.Tabs, state.PaneDocTabJSON{Path: t.Value})
			case panelayout.Issue:
				saved.IssueTabs = append(saved.IssueTabs, state.PaneIssueTabJSON{Issue: t.Value})
			case panelayout.Note:
				saved.NoteTabs = append(saved.NoteTabs, state.PaneNoteTabJSON{Note: t.Value})
			case panelayout.Diff:
				saved.DiffTabs = append(saved.DiffTabs, state.PaneDiffTabJSON{Spec: t.Value})
			case panelayout.Resource:
				// Exactly one shape per record, chosen by what the target
				// carries. A plugin collection has no matcher to write, and a
				// matched locator has no collection.
				tab := state.PaneResourceTabJSON{Provider: t.Provider}
				if t.Collection != "" {
					tab.Collection, tab.Query, tab.Locator = t.Collection, t.Query, t.Value
					tab.Filters = t.Filters
				} else {
					tab.Matcher, tab.Locator = t.Matcher, t.Value
				}
				saved.ResourceTabs = append(saved.ResourceTabs, tab)
			}
		}
		return saved
	}
}

func specStateKind(kind panelayout.Kind) string {
	switch kind {
	case panelayout.Document:
		return panecodec.KindDoc
	case panelayout.Issue:
		return panecodec.KindIssue
	case panelayout.Note:
		return panecodec.KindNote
	case panelayout.Diff:
		return panecodec.KindDiff
	default:
		return panecodec.KindResource
	}
}

func sortedSessionKeys(sessions map[string]bool) []string {
	out := make([]string, 0, len(sessions))
	for session := range sessions {
		out = append(out, session)
	}
	sort.Strings(out)
	return out
}

func stackRows(nodes []*panelayout.Node) *panelayout.Node {
	root := nodes[0]
	for _, next := range nodes[1:] {
		root = &panelayout.Node{ID: panelayout.MaxID(root) + 1, Split: &panelayout.Split{
			Axis: panelayout.Rows, Ratio: 50, A: root, B: next,
		}}
	}
	return root
}

func stackSavedRows(saved []*state.PaneLayoutJSON) *state.PaneLayoutJSON {
	root := saved[0]
	for _, next := range saved[1:] {
		root = &state.PaneLayoutJSON{Split: &state.PaneSplitJSON{Axis: "rows", Ratio: 50, A: root, B: next}}
	}
	return root
}

func chainColumns(columns []*panelayout.Node) *panelayout.Node {
	root := columns[0]
	for _, next := range columns[1:] {
		root = &panelayout.Node{ID: panelayout.MaxID(root) + 1, Split: &panelayout.Split{
			Axis: panelayout.Columns, Ratio: 50, A: root, B: next,
		}}
	}
	return root
}

func chainColumnsSaved(columns []*state.PaneLayoutJSON) *state.PaneLayoutJSON {
	root := columns[0]
	for _, next := range columns[1:] {
		root = &state.PaneLayoutJSON{Split: &state.PaneSplitJSON{Axis: "cols", Ratio: 50, A: root, B: next}}
	}
	return root
}
