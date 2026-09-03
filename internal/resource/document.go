package resource

import (
	"net/url"
	"sort"
	"strings"
	"time"
)

// Tone is the coarse severity of a status pill. It is presentation-neutral on
// purpose: the host maps it to a palette, the provider does not choose colors.
type Tone string

// Stable v1 tones. Anything else coerces to ToneNeutral.
const (
	ToneNeutral Tone = "neutral"
	ToneInfo    Tone = "info"
	ToneSuccess Tone = "success"
	ToneWarning Tone = "warning"
	ToneDanger  Tone = "danger"
)

// CoerceTone maps an arbitrary provider string onto a known tone.
func CoerceTone(v string) Tone {
	switch Tone(v) {
	case ToneInfo:
		return ToneInfo
	case ToneSuccess:
		return ToneSuccess
	case ToneWarning:
		return ToneWarning
	case ToneDanger:
		return ToneDanger
	default:
		return ToneNeutral
	}
}

// Format is how a body's text should be interpreted.
type Format string

// Stable v1 formats. Anything else coerces to FormatText, which is the safer
// of the two: unknown markup is shown, never interpreted.
const (
	FormatText     Format = "text"
	FormatMarkdown Format = "markdown"
)

// CoerceFormat maps an arbitrary provider string onto a known format.
func CoerceFormat(v string) Format {
	if Format(v) == FormatMarkdown {
		return FormatMarkdown
	}
	return FormatText
}

// FieldKind tells the host how to present a field value. It never changes
// validation: a timestamp that does not parse is still shown as the text the
// provider sent, because the alternative is silently hiding a value the user
// can see in the service itself.
type FieldKind string

// Stable v1 field kinds. Anything else coerces to FieldKindText.
const (
	FieldKindText      FieldKind = "text"
	FieldKindTimestamp FieldKind = "timestamp"
	FieldKindUser      FieldKind = "user"
)

// CoerceFieldKind maps an arbitrary provider string onto a known kind.
func CoerceFieldKind(v string) FieldKind {
	switch FieldKind(v) {
	case FieldKindTimestamp:
		return FieldKindTimestamp
	case FieldKindUser:
		return FieldKindUser
	default:
		return FieldKindText
	}
}

// Status is the optional single-line state of a resource.
type Status struct {
	Label string
	Tone  Tone
}

// Field is one ordered label/value pair in the bounded grid.
type Field struct {
	Label string
	Value string
	// Kind is a presentation hint. M1 owns what it does with it; M0's job is
	// only to carry and bound it, never to lose it.
	Kind FieldKind
}

// Body is the optional long-form text of a resource.
type Body struct {
	Format Format
	Text   string
	// Truncated reports that the provider's body exceeded MaxBodyBytes and was
	// cut at a rune boundary. The host shows the user what it kept and says so;
	// it does not refuse a document for being long.
	Truncated bool
}

// Document is the whole of what a provider may put on screen. Every string in
// it has already been through Sanitize*: valid UTF-8, bounded, control-free,
// and OSC-free. SourceURL, when non-empty, is a validated http/https URL.
type Document struct {
	// Identity is the provider-stable canonical ID. It may differ from the
	// locator that produced the lookup; the host re-keys the tab when it does.
	Identity string
	Title    string
	Subtitle string
	// Status is nil when the provider supplied none.
	Status *Status
	Fields []Field
	// Body is nil when the provider supplied none.
	Body      *Body
	SourceURL string
	// UpdatedAt is the zero time when absent or unparseable. An unparseable
	// timestamp is dropped, never an error.
	UpdatedAt time.Time
	// FreshFor is the clamped freshness hint the cache honors.
	FreshFor time.Duration
	// Sections are the titled blocks under the card. Empty for every document
	// a frozen-protocol provider returns.
	Sections []Section
}

// Shape names which of a Reference's alternatives is set.
type Shape int

const (
	// ShapeInvalid is a reference that is no shape, or more than one.
	ShapeInvalid Shape = iota
	// ShapeMatched is {instance, matcher, locator}: what a scanned span
	// produces and what `resolve` consumes. It is the frozen resource
	// protocol's only shape and is unchanged by the plugin protocol.
	ShapeMatched
	// ShapeCollection is {instance, collection} plus the user-owned view
	// position: what a plugin's collection tab points at. `list` consumes it.
	ShapeCollection
	// ShapeItem is {instance, collection, locator}: one row of a collection,
	// which `get` consumes. It is a distinct shape rather than a matched
	// document because a plugin row is addressed by its collection and ID, and
	// there is no matcher anywhere in that journey to invent.
	ShapeItem
)

// Reference is what a plugin-shaped tab points at, and the only plugin-shaped
// value that reaches persisted state. It carries no secret: a locator such as
// CASH-1245 and a user-typed query are the minimum needed to restore the pane
// the user had open.
type Reference struct {
	Instance string
	Matcher  string
	// Locator is the matched locator in ShapeMatched and the row ID in
	// ShapeItem. It is empty in ShapeCollection.
	Locator string

	// Collection is the plugin-declared collection ID. Non-empty is what makes
	// a reference one of the two plugin shapes.
	Collection string
	// Query, View, Sort and CursorID are a collection tab's view position,
	// restored verbatim so relaunch reopens the list the user was reading
	// rather than the collection's default page.
	Query    string
	View     string
	Sort     string
	CursorID string
	// Filters is the applied filter set, sorted by ID. It is a slice rather
	// than a map because a reference is copied everywhere and compared as a
	// value: a map would alias between two tabs and would give one state two
	// spellings depending on iteration order.
	Filters []FilterValue
}

// FilterValue is one applied filter. Both halves are bounded because both
// survive a relaunch.
type FilterValue struct {
	ID    string
	Value string
}

// SortFilterValues returns the applied set in a stable order, dropping empty
// ids and keeping the LAST value for a repeated one. Callers hand it whatever
// they have; this is the only spelling of an applied set that reaches state.
func SortFilterValues(in []FilterValue) []FilterValue {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]string, len(in))
	for _, f := range in {
		id := strings.TrimSpace(f.ID)
		if id == "" {
			continue
		}
		seen[id] = f.Value
	}
	if len(seen) == 0 {
		return nil
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]FilterValue, 0, len(ids))
	for _, id := range ids {
		out = append(out, FilterValue{ID: id, Value: seen[id]})
	}
	return out
}

// FilterMap projects an applied set onto the map shape the protocol sends.
func FilterMap(in []FilterValue) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for _, f := range in {
		if f.ID == "" {
			continue
		}
		out[f.ID] = f.Value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EncodeFilters renders an applied set as one canonical, comparable string:
// url.Values form, sorted by key and percent-escaped. It exists because
// contentlink.Ref travels through every open journey as a comparable value, and
// one more string keeps it comparable where a map would not.
func EncodeFilters(in []FilterValue) string {
	values := url.Values{}
	for _, f := range SortFilterValues(in) {
		values.Set(f.ID, f.Value)
	}
	if len(values) == 0 {
		return ""
	}
	return values.Encode()
}

// DecodeFilters is the inverse. Anything unparseable is no filters at all: a
// half-read applied set would send the plugin a scope nobody chose.
func DecodeFilters(encoded string) []FilterValue {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil
	}
	values, err := url.ParseQuery(encoded)
	if err != nil {
		return nil
	}
	out := make([]FilterValue, 0, len(values))
	for id, list := range values {
		if len(list) == 0 {
			continue
		}
		out = append(out, FilterValue{ID: id, Value: list[0]})
	}
	return SortFilterValues(out)
}

// FilterValues is the inverse: a map becomes the sorted slice a reference
// carries.
func FilterValues(in map[string]string) []FilterValue {
	if len(in) == 0 {
		return nil
	}
	out := make([]FilterValue, 0, len(in))
	for id, value := range in {
		out = append(out, FilterValue{ID: id, Value: value})
	}
	return SortFilterValues(out)
}

// Equal reports whether two references name the same thing in the same view
// position. It exists because a reference carries an applied filter set, which
// makes the struct uncomparable with ==; every field still takes part, so this
// is value equality and not a looser identity test.
func (r Reference) Equal(other Reference) bool {
	if r.Instance != other.Instance || r.Matcher != other.Matcher || r.Locator != other.Locator ||
		r.Collection != other.Collection || r.Query != other.Query || r.View != other.View ||
		r.Sort != other.Sort || r.CursorID != other.CursorID || len(r.Filters) != len(other.Filters) {
		return false
	}
	for i, f := range r.Filters {
		if f != other.Filters[i] {
			return false
		}
	}
	return true
}

// Shape reports which alternative this reference is, or ShapeInvalid when it is
// none or several. Deciding it in one place is what stops each caller from
// growing its own idea of what "a collection tab" means.
func (r Reference) Shape() Shape {
	switch {
	case r.Collection == "" && r.Matcher != "" && r.Locator != "":
		return ShapeMatched
	case r.Collection != "" && r.Matcher == "" && r.Locator == "":
		return ShapeCollection
	case r.Collection != "" && r.Matcher == "" && r.Locator != "":
		return ShapeItem
	default:
		return ShapeInvalid
	}
}

// IsCollection reports the collection-tab shape.
func (r Reference) IsCollection() bool { return r.Shape() == ShapeCollection }

// IsPlugin reports either of the two shapes that talk to a protocol plugin's
// list/get methods, which is what decides whether a tab renders as the shared
// browser or as the resource card.
func (r Reference) IsPlugin() bool {
	shape := r.Shape()
	return shape == ShapeCollection || shape == ShapeItem
}

// Valid reports whether a reference is well-formed enough to send to a plugin.
// It is a bounds check, not an existence check.
//
// Exactly one shape. A reference naming both a matcher and a collection, or
// neither, is refused rather than sent under a guess: which one the sender
// meant is not something the host can infer, and inferring it is how a restored
// tab silently becomes a different tab.
func (r Reference) Valid() bool {
	if r.Instance == "" || runeLen(r.Instance) > MaxInstanceIDChars {
		return false
	}
	switch r.Shape() {
	case ShapeMatched:
		return runeLen(r.Matcher) <= MaxMatcherIDChars &&
			runeLen(r.Locator) <= MaxLocatorChars
	case ShapeCollection:
		return r.viewPositionInBounds()
	case ShapeItem:
		return runeLen(r.Locator) <= MaxLocatorChars && r.viewPositionInBounds()
	default:
		return false
	}
}

func (r Reference) viewPositionInBounds() bool {
	if len(r.Filters) > MaxFilters {
		return false
	}
	for _, f := range r.Filters {
		if f.ID == "" || runeLen(f.ID) > MaxFilterIDChars || runeLen(f.Value) > MaxFilterValueChars {
			return false
		}
	}
	return runeLen(r.Collection) <= MaxCollectionIDChars &&
		runeLen(r.Query) <= MaxQueryChars &&
		runeLen(r.View) <= MaxViewIDChars &&
		runeLen(r.Sort) <= MaxSortIDChars &&
		runeLen(r.CursorID) <= MaxIdentityChars
}
