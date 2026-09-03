package pluginhost

import "github.com/marcus/sidecar/internal/resource"

// The wire types are exactly the JSON shapes in the protocol document. They
// exist so decoding is total — every field is optional at the JSON layer and
// every rule is enforced in one place — rather than scattered across json tags
// a future field could quietly bypass.
//
// Unknown JSON fields are ignored: encoding/json's default behaviour is the
// forward-compatibility rule the protocol asks for, so no decoder here may set
// DisallowUnknownFields.

// WireColumn is one `columns[]` element of a collection.
type WireColumn struct {
	ID        string `json:"id"`
	Label     string `json:"label,omitempty"`
	Width     int    `json:"width,omitempty"`
	Align     string `json:"align,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Primary   bool   `json:"primary,omitempty"`
	Secondary bool   `json:"secondary,omitempty"`
}

// WireView is one `views[]` element.
type WireView struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// WireSortKey is one `sort[]` element.
type WireSortKey struct {
	ID      string `json:"id"`
	Label   string `json:"label,omitempty"`
	Default string `json:"default,omitempty"`
}

// WireFilterChoice is one `choices[]` element of a choice filter.
type WireFilterChoice struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// WireFilter is one `filters[]` element of a collection.
type WireFilter struct {
	ID      string             `json:"id"`
	Label   string             `json:"label,omitempty"`
	Kind    string             `json:"kind,omitempty"`
	Choices []WireFilterChoice `json:"choices,omitempty"`
	Default string             `json:"default,omitempty"`
}

// WireRefresh is a collection's `refresh` object.
type WireRefresh struct {
	EverySeconds int      `json:"everySeconds,omitempty"`
	Watch        []string `json:"watch,omitempty"`
}

// WireCollection is one `collections[]` element of a describe result. Detail is
// a pointer because a collection that says nothing about it means "rows can be
// opened", which is the useful default, and an omitted false would silently
// disable Enter.
type WireCollection struct {
	ID      string        `json:"id"`
	Title   string        `json:"title,omitempty"`
	Search  string        `json:"search,omitempty"`
	Columns []WireColumn  `json:"columns,omitempty"`
	Views   []WireView    `json:"views,omitempty"`
	Sort    []WireSortKey `json:"sort,omitempty"`
	Filters []WireFilter  `json:"filters,omitempty"`
	Detail  *bool         `json:"detail,omitempty"`
	Refresh *WireRefresh  `json:"refresh,omitempty"`
	Context []string      `json:"context,omitempty"`
}

// WireActionInput is one `inputs[]` element of an action.
type WireActionInput struct {
	ID       string   `json:"id"`
	Label    string   `json:"label,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Required bool     `json:"required,omitempty"`
	Choices  []string `json:"choices,omitempty"`
	Default  string   `json:"default,omitempty"`
}

// WireAction is one `actions[]` element. Confirm is a pointer because the
// default depends on the action: a mutating action with no inputs confirms
// unless the plugin explicitly said not to.
type WireAction struct {
	ID         string            `json:"id"`
	Title      string            `json:"title,omitempty"`
	On         string            `json:"on,omitempty"`
	Collection string            `json:"collection,omitempty"`
	Inputs     []WireActionInput `json:"inputs,omitempty"`
	Mutates    bool              `json:"mutates,omitempty"`
	Confirm    *bool             `json:"confirm,omitempty"`
	Key        string            `json:"key,omitempty"`
}

// WireItem is one `page.items[]` element.
type WireItem struct {
	ID        string               `json:"id"`
	Cells     map[string]string    `json:"cells,omitempty"`
	Status    *resource.WireStatus `json:"status,omitempty"`
	SourceURL string               `json:"sourceUrl,omitempty"`
}

// WireNotice is one `page.notices[]` element.
type WireNotice struct {
	Tone string `json:"tone,omitempty"`
	Text string `json:"text"`
}

// WireOmitted is the `page.omitted` object: what the plugin held back.
type WireOmitted struct {
	Suppressed int `json:"suppressed,omitempty"`
	Dropped    int `json:"dropped,omitempty"`
}

// WireCoverage is one `page.coverage[]` element.
type WireCoverage struct {
	Source    string `json:"source"`
	State     string `json:"state,omitempty"`
	Reason    string `json:"reason,omitempty"`
	ElapsedMs int    `json:"elapsedMs,omitempty"`
}

// WirePage is the `page` object of a list response.
type WirePage struct {
	Outcome    string         `json:"outcome,omitempty"`
	Items      []WireItem     `json:"items,omitempty"`
	NextCursor string         `json:"nextCursor,omitempty"`
	Total      int            `json:"total,omitempty"`
	Notices    []WireNotice   `json:"notices,omitempty"`
	Omitted    *WireOmitted   `json:"omitted,omitempty"`
	Coverage   []WireCoverage `json:"coverage,omitempty"`
}

// WireOpen is the `outcome.open` object.
type WireOpen struct {
	Collection string `json:"collection,omitempty"`
	ID         string `json:"id,omitempty"`
}

// WireOutcome is the `outcome` object of an act response.
type WireOutcome struct {
	Status  string    `json:"status,omitempty"`
	Message string    `json:"message,omitempty"`
	Refresh []string  `json:"refresh,omitempty"`
	Open    *WireOpen `json:"open,omitempty"`
}
