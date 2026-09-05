// Package projectlist is the state-free presentation core behind Sidecar's
// project collection: what a destination looks like, how the collection is
// filtered and ordered, and how its metadata reads.
//
// It holds no Bubble Tea state and touches no filesystem, so a headless caller
// can adopt the same filtering, ordering and formatting the modal uses. The
// modal sections above it decide only where things are drawn and which regions
// are clickable. It deliberately mirrors internal/workspacelist's vocabulary —
// Label, SortActionID, SortFromLabel, SortIndex — because the two collection
// surfaces answer the same question and must not invent two names for it.
package projectlist

import (
	"sort"
	"strings"
	"time"
)

// Destination kinds. Overview is the cross-project surface, not a project, and
// it is deliberately outside the sorting domain.
const (
	KindOverview = "overview"
	KindProject  = "project"
)

// Sort is an explicit user-chosen ordering. Sorting is presentation only: it
// never changes identities and never triggers collection.
type Sort uint8

const (
	// SortActivity orders by the latest recorded Sidecar activity in the
	// project. It is first because recency is what a switcher is usually for.
	SortActivity Sort = iota
	SortName
	// SortAdded orders by when the project was registered with Sidecar. It is
	// "Date added" and not "Date created" on purpose: Sidecar records the
	// registration, and it knows nothing about when the directory or the
	// repository came into being.
	SortAdded
)

// SortModes is the offered set, in the order the sort menu lists them.
var SortModes = []Sort{SortActivity, SortName, SortAdded}

// Label is what the control reads and what a state file persists.
func (s Sort) Label() string {
	switch s {
	case SortName:
		return "Name"
	case SortAdded:
		return "Date added"
	default:
		return "Last activity"
	}
}

// IsDate reports whether the mode orders by a timestamp, which is what decides
// the direction wording and whether unknown values are possible.
func (s Sort) IsDate() bool { return s == SortActivity || s == SortAdded }

// SortActionID names the choice a surface reports when a sort is picked, so a
// shared handler answers to one name rather than two.
func SortActionID(mode Sort) string { return "project-sort-" + mode.Label() }

// SortFromAction resolves an action ID back to a mode within the offered set.
func SortFromAction(action string, modes []Sort) (Sort, bool) {
	for _, mode := range modes {
		if action == SortActionID(mode) {
			return mode, true
		}
	}
	return 0, false
}

// SortFromLabel resolves a persisted label back to a mode, case-insensitively.
// Labels are what surfaces persist: they read plainly in a state file and
// survive the enum being reordered.
func SortFromLabel(label string, modes []Sort) (Sort, bool) {
	for _, mode := range modes {
		if strings.EqualFold(strings.TrimSpace(label), mode.Label()) {
			return mode, true
		}
	}
	return 0, false
}

// SortIndex is a mode's position in the offered set, for a list cursor.
func SortIndex(mode Sort, modes []Sort) int {
	for i, candidate := range modes {
		if candidate == mode {
			return i
		}
	}
	return 0
}

// Order is the direction within a sort. The words differ by mode — a name has
// no "newest" — but the axis is one, so it persists and toggles as one.
type Order uint8

const (
	OrderAscending Order = iota
	OrderDescending
)

// DefaultOrder is what a mode starts in: names read A–Z, dates read newest
// first, because that is what each is usually wanted for.
func (s Sort) DefaultOrder() Order {
	if s.IsDate() {
		return OrderDescending
	}
	return OrderAscending
}

// OrderLabel words the direction for the mode it applies to.
func OrderLabel(mode Sort, order Order) string {
	if mode.IsDate() {
		if order == OrderDescending {
			return "newest first"
		}
		return "oldest first"
	}
	if order == OrderDescending {
		return "Z–A"
	}
	return "A–Z"
}

// OrderFromLabel resolves a persisted direction label back to an Order. It
// accepts either mode's wording so a stored value survives the sort changing.
func OrderFromLabel(label string) (Order, bool) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "newest first", "z–a", "z-a", "descending":
		return OrderDescending, true
	case "oldest first", "a–z", "a-z", "ascending":
		return OrderAscending, true
	}
	return OrderAscending, false
}

// Toggle flips the direction.
func (o Order) Toggle() Order {
	if o == OrderAscending {
		return OrderDescending
	}
	return OrderAscending
}

// View is the layout the collection is drawn in. It is a view of one ordered
// collection, not a second collection: selection, filter and order survive a
// change of view.
type View uint8

const (
	ViewList View = iota
	ViewGrid
)

// ViewModes is the offered set, in control order.
var ViewModes = []View{ViewList, ViewGrid}

func (v View) Label() string {
	if v == ViewGrid {
		return "Grid"
	}
	return "List"
}

// ViewActionID names the choice a surface reports when a view is picked.
func ViewActionID(v View) string { return "project-view-" + v.Label() }

// ViewFromAction resolves an action ID back to a view.
func ViewFromAction(action string) (View, bool) {
	for _, v := range ViewModes {
		if action == ViewActionID(v) {
			return v, true
		}
	}
	return 0, false
}

// ViewFromLabel resolves a persisted label back to a view.
func ViewFromLabel(label string) (View, bool) {
	for _, v := range ViewModes {
		if strings.EqualFold(strings.TrimSpace(label), v.Label()) {
			return v, true
		}
	}
	return 0, false
}

// Item is one destination's resolved display identity. Every field is
// presentation input: this package matches, orders and formats from these
// values and reads nothing behind them. Data carries the caller's own record so
// a selection resolves back to its owner without this package knowing the type.
type Item struct {
	// ID is the stable identity a selection rides through filtering, sorting
	// and a change of view. It must not encode position.
	ID   string
	Kind string
	// Name is the display name, host prefix included for a remote.
	Name string
	// Path is the destination's own root as its owner reports it. It is shown
	// and searched, never resolved locally for a remote.
	Path string
	// Host is the registered host id; empty is this machine.
	Host string
	// Current marks the destination this TUI is bound to. It is a status, not
	// a selection: the highlighted row is a separate concept.
	Current bool
	// DisabledReason is non-empty when the destination cannot be activated.
	// The row stays visible and says why.
	DisabledReason string
	// AddedAt is when the project was registered with Sidecar. Zero is
	// unknown, which is the honest answer for every project registered before
	// Sidecar started recording it.
	AddedAt time.Time
	// LastActiveAt is the latest recorded Sidecar activity in the project.
	// Zero is unknown.
	LastActiveAt time.Time
	Data         any
}

// Disabled reports whether the destination refuses activation.
func (i Item) Disabled() bool { return i.DisabledReason != "" }

// timestampFor is the value a date mode orders by.
func (i Item) timestampFor(mode Sort) time.Time {
	if mode == SortAdded {
		return i.AddedAt
	}
	return i.LastActiveAt
}

// Matches reports whether query is a case-insensitive substring of the item's
// name, path or host. An empty query matches everything.
func Matches(item Item, query string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return true
	}
	for _, field := range []string{item.Name, item.Path, item.Host} {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

// Filter keeps the items a query matches, in the order handed in. Overview is
// filtered like anything else: a user who typed a query wants what they asked
// for, not a pinned row that ignores it.
func Filter(items []Item, query string) []Item {
	if strings.TrimSpace(query) == "" {
		return append([]Item(nil), items...)
	}
	out := make([]Item, 0, len(items))
	for _, item := range items {
		if Matches(item, query) {
			out = append(out, item)
		}
	}
	return out
}

// Sorted returns the collection in the chosen order without mutating the input.
//
// Three rules hold in every mode. Overview is not a project and stays pinned
// ahead of the collection. An unknown timestamp sorts last in both directions,
// because "we have not recorded this" is not a date and reversing the order
// must not promote it to the top. Ties break on the case-insensitive name and
// then the stable identity, so the same collection always produces the same
// order and a selection never jumps because two rows swapped.
func Sorted(items []Item, mode Sort, order Order) []Item {
	out := append([]Item(nil), items...)
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a], out[b]
		if pinned := x.Kind == KindOverview; pinned != (y.Kind == KindOverview) {
			return pinned
		}
		if x.Kind == KindOverview {
			return false // both pinned: keep the caller's order
		}
		if mode.IsDate() {
			xt, yt := x.timestampFor(mode), y.timestampFor(mode)
			if xt.IsZero() != yt.IsZero() {
				return !xt.IsZero()
			}
			if !xt.IsZero() && !xt.Equal(yt) {
				if order == OrderDescending {
					return xt.After(yt)
				}
				return xt.Before(yt)
			}
			return lessByName(x, y, OrderAscending)
		}
		return lessByName(x, y, order)
	})
	return out
}

func lessByName(x, y Item, order Order) bool {
	xn, yn := strings.ToLower(x.Name), strings.ToLower(y.Name)
	if xn != yn {
		if order == OrderDescending {
			return xn > yn
		}
		return xn < yn
	}
	return x.ID < y.ID
}

// IndexOf finds an item by identity. It returns -1 when the identity is gone,
// which is the caller's signal to fall back rather than land on a row the user
// did not choose.
func IndexOf(items []Item, id string) int {
	if id == "" {
		return -1
	}
	for i, item := range items {
		if item.ID == id {
			return i
		}
	}
	return -1
}

// UnknownLabel is the word an absent timestamp reads as. It is a word and not
// a dash so the row says what is true rather than looking like a value failed
// to render.
const UnknownLabel = "Unknown"

// FormatRelative words a timestamp as an age. A zero time is UnknownLabel.
func FormatRelative(t, now time.Time) string {
	if t.IsZero() {
		return UnknownLabel
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		return "now"
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m ago"
	case d < 24*time.Hour:
		return itoa(int(d/time.Hour)) + "h ago"
	case d < 48*time.Hour:
		return "yesterday"
	case d < 30*24*time.Hour:
		return itoa(int(d/(24*time.Hour))) + "d ago"
	}
	return t.Format("2006-01-02")
}

// FormatDate words a timestamp as a calendar date, which is what a registration
// date is actually read as. A zero time is UnknownLabel.
func FormatDate(t time.Time) string {
	if t.IsZero() {
		return UnknownLabel
	}
	return t.Format("2006-01-02")
}

// MetaColumn is the heading and value the metadata column carries under a given
// sort. The column follows a date sort so the user can see what they ordered
// by; under Name it stays on last activity, which is the fact a switcher is
// most often opened to check.
func MetaColumn(mode Sort) (heading string, byDate bool) {
	if mode == SortAdded {
		return "DATE ADDED", true
	}
	return "LAST ACTIVITY", false
}

// MetaValue is one item's metadata cell under a given sort.
func MetaValue(item Item, mode Sort, now time.Time) string {
	if _, byDate := MetaColumn(mode); byDate {
		return FormatDate(item.AddedAt)
	}
	return FormatRelative(item.LastActiveAt, now)
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
