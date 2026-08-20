package notify

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/marcus/sidecar/internal/terminallink"
	"github.com/marcus/sidecar/internal/uirequest"
)

// Calls to action
//
// A notification's actionable references come from two places that must agree:
// the targets a poster attached deliberately (notify.Target, which the CLI's
// --target will write) and the ids a scan finds in the text (terminallink
// spans). Reconciling them is state-free and lives here, next to the model, so
// the centre, a future CLI, and any test all number and activate exactly the
// same list.
//
// The rule is the plan's: stored targets first, in the order they were
// attached; the scan then fills gaps, in reading order (title, then body). A
// scanned span that says the same thing as a stored target is not a second
// call to action — it is the stored target's location in the text, which is
// what lets it be underlined where it is written.

// CTAField names the field a call to action was found in. A stored target that
// appears nowhere in the text has CTAFieldNone and no columns: it is real, it
// is numbered, and it simply has nothing to underline.
type CTAField string

const (
	CTAFieldNone  CTAField = ""
	CTAFieldTitle CTAField = "title"
	CTAFieldBody  CTAField = "body"
)

// CallToAction is one numbered, activatable reference of a notification.
type CallToAction struct {
	// Number is the 1-based digit that jumps to this target.
	Number int
	// Target is the activation vocabulary every surface shares.
	Target uirequest.Target
	// Project qualifies a target in another checkout; empty means "here".
	Project string
	// Label is how the target reads in a list — the text as written when the
	// scan found it, the stored value otherwise.
	Label string
	// Field, StartCol and EndCol locate the target in CTAText's output, in
	// visual columns, EndCol inclusive. StartCol is -1 when Field is
	// CTAFieldNone.
	Field    CTAField
	StartCol int
	EndCol   int
}

// CTATitle is the title text the centre renders and the scan reads — the same
// string for both, so a span's columns are the columns on screen. Untrusted
// text is stripped of source OSC sequences here, once, per the safety rule in
// internal/targetactivation's package doc.
func CTATitle(n Notification) string {
	title := strings.TrimSpace(terminallink.StripOSC8(n.Title))
	if title == "" {
		title = n.SourceInfo().Label
	}
	return title
}

// CTABody is the body text the centre renders and the scan reads, whitespace
// collapsed exactly as the centre collapses it.
func CTABody(n Notification) string {
	return strings.Join(strings.Fields(terminallink.StripOSC8(n.Body)), " ")
}

// CallsToAction reconciles a notification's stored targets with what a scan of
// its title and body finds, and numbers the result 1..N.
//
// opts is passed straight to terminallink.ScanWith, so the caller decides how
// much verification it can afford: a nil Resolve leaves bare filenames as
// ordinary text (the I/O-free choice), and a Resolve that stats against the
// project root earns those spans an underline.
func CallsToAction(n Notification, opts terminallink.Options) []CallToAction {
	title, body := CTATitle(n), CTABody(n)
	scanned := append(
		fieldSpans(CTAFieldTitle, title, opts),
		fieldSpans(CTAFieldBody, body, opts)...)

	var out []CallToAction
	seen := map[string]bool{}
	// Stored targets first: a poster that named a target meant it, whether or
	// not the text happens to spell it out.
	for _, stored := range n.Targets {
		target, ok := targetFromStored(stored)
		if !ok {
			continue
		}
		key := ctaKey(target, stored.Project)
		if seen[key] {
			continue
		}
		seen[key] = true
		cta := CallToAction{Target: target, Project: stored.Project,
			Label: ctaLabel(target), Field: CTAFieldNone, StartCol: -1, EndCol: -1}
		// Locate it in the text, if the text says the same thing. Same project
		// only: a span is written in this checkout's terms and cannot stand for
		// a target that names another one.
		for i := range scanned {
			if scanned[i].used || stored.Project != "" {
				continue
			}
			if ctaKey(scanned[i].target, "") != key {
				continue
			}
			scanned[i].used = true
			cta.Field, cta.StartCol, cta.EndCol = scanned[i].field, scanned[i].start, scanned[i].end
			cta.Label = scanned[i].label
			break
		}
		out = append(out, cta)
	}
	// The scan fills the gaps.
	for _, span := range scanned {
		if span.used {
			continue
		}
		key := ctaKey(span.target, "")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, CallToAction{Target: span.target, Label: span.label,
			Field: span.field, StartCol: span.start, EndCol: span.end})
	}
	for i := range out {
		out[i].Number = i + 1
	}
	return out
}

// Display is how a call to action reads in the numbered list. A target in
// another project carries that project's name — design 1e's
// "repo/td-xxxxxx" — so a jump that will change projects says so before it is
// taken.
func (c CallToAction) Display() string {
	project := strings.TrimSpace(c.Project)
	if project == "" {
		return c.Label
	}
	project = strings.TrimRight(project, "/")
	if idx := strings.LastIndexAny(project, "/\\"); idx >= 0 {
		project = project[idx+1:]
	}
	if project == "" {
		return c.Label
	}
	return project + "/" + c.Label
}

// CallToActionAt returns the call to action a digit key names.
func CallToActionAt(list []CallToAction, number int) (CallToAction, bool) {
	if number < 1 || number > len(list) {
		return CallToAction{}, false
	}
	return list[number-1], true
}

type scannedCTA struct {
	target uirequest.Target
	field  CTAField
	start  int
	end    int
	label  string
	used   bool
}

func fieldSpans(field CTAField, text string, opts terminallink.Options) []scannedCTA {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var out []scannedCTA
	for _, span := range terminallink.ScanWith(text, opts) {
		if !terminallink.Activatable(span.Kind) {
			continue
		}
		target, ok := uirequest.TargetFromSpan(span)
		if !ok {
			continue
		}
		out = append(out, scannedCTA{
			target: target, field: field,
			start: span.StartCol, end: span.EndCol,
			label: ctaLabel(target),
		})
	}
	// ScanWith emits kind by kind (urls, then files, then issues…); reading
	// order is what a numbered list has to follow.
	sort.SliceStable(out, func(i, j int) bool { return out[i].start < out[j].start })
	return out
}

// targetFromStored maps the notification vocabulary onto the activation
// vocabulary. Every notify target kind maps since Phase 5c; a kind that ever
// stops mapping is dropped rather than numbered, because a digit that cannot
// jump is worse than no digit.
func targetFromStored(stored Target) (uirequest.Target, bool) {
	value := strings.TrimSpace(stored.Value)
	if value == "" {
		return uirequest.Target{}, false
	}
	switch stored.Kind {
	case TargetFile:
		return uirequest.Target{Kind: uirequest.TargetKindFile, Value: value, Line: stored.Line}, true
	case TargetIssue:
		return uirequest.Target{Kind: uirequest.TargetKindIssue, Value: value}, true
	case TargetURL:
		return uirequest.Target{Kind: uirequest.TargetKindURL, Value: value}, true
	case TargetCommit:
		return uirequest.Target{Kind: uirequest.TargetKindDiff, Value: value}, true
	case TargetTask:
		return uirequest.Target{Kind: uirequest.TargetKindTask, Value: value}, true
	case TargetSession:
		return uirequest.Target{Kind: uirequest.TargetKindSession, Value: value}, true
	default:
		return uirequest.Target{}, false
	}
}

func ctaKey(target uirequest.Target, project string) string {
	value := target.Value
	if target.Kind == uirequest.TargetKindFile {
		value = strings.TrimPrefix(value, "./")
	}
	return fmt.Sprintf("%s|%s|%d|%s|%s", target.Kind, value, target.Line, target.Provider, project)
}

func ctaLabel(target uirequest.Target) string {
	label := target.Value
	if target.Kind == uirequest.TargetKindFile && target.Line > 0 {
		label += ":" + strconv.Itoa(target.Line)
	}
	return label
}
