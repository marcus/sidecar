package pluginhost

import "github.com/marcus/sidecar/internal/resource"

// SanitizePage enforces every page bound and returns rows safe to render.
//
// It truncates rather than refusing, which is the same rule SanitizeDocument
// follows and for the same reason: a plugin is not required to pre-truncate to
// the host's numbers, the numbers are not sent in the request, and a page that
// is merely too big should still show the user their results. Items past the
// limit are dropped and the page is marked truncated; a cell longer than the
// bound is cut; a cell whose column the plugin never declared is dropped,
// because the host has nowhere to draw it.
//
// The only refusal is structural, and it belongs to the caller: a row with no
// id cannot be opened or acted on, so it is dropped here rather than shown as a
// row that does nothing.
func SanitizePage(w *WirePage, collection Collection) Page {
	if w == nil {
		return Page{Outcome: OutcomeAnswered}
	}
	page := Page{
		Outcome:    CoercePageOutcome(w.Outcome),
		NextCursor: resource.SanitizeLine(w.NextCursor, resource.MaxLocatorChars),
	}
	if w.Total > 0 {
		page.Total = w.Total
	}

	columns := make(map[string]bool, len(collection.Columns))
	for _, col := range collection.Columns {
		columns[col.ID] = true
	}

	if len(w.Items) > 0 {
		page.Truncated = len(w.Items) > MaxPageItems
		items := make([]Item, 0, min(len(w.Items), MaxPageItems))
		for _, wi := range w.Items {
			if len(items) == MaxPageItems {
				break
			}
			id := resource.SanitizeLine(wi.ID, resource.MaxIdentityChars)
			if id == "" {
				continue
			}
			item := Item{ID: id, SourceURL: resource.SanitizeURL(wi.SourceURL)}
			if len(wi.Cells) > 0 {
				cells := make(map[string]string, len(wi.Cells))
				for key, value := range wi.Cells {
					// An undeclared column has no width, no header, and no
					// place in the row. Keeping it would be keeping a string
					// nothing can ever paint.
					if len(columns) > 0 && !columns[key] {
						continue
					}
					cells[key] = resource.SanitizeLine(value, MaxCellChars)
				}
				if len(cells) > 0 {
					item.Cells = cells
				}
			}
			if wi.Status != nil {
				label := resource.SanitizeLine(wi.Status.Label, resource.MaxStatusLabelChars)
				if label != "" {
					item.Status = &resource.Status{Label: label, Tone: resource.CoerceTone(wi.Status.Tone)}
				}
			}
			items = append(items, item)
		}
		page.Items = items
	}

	if w.Omitted != nil {
		// A negative count is not "held back a negative number of rows"; it is
		// noise, and the summary row would render it as data.
		page.Omitted = Omitted{Suppressed: max(0, w.Omitted.Suppressed), Dropped: max(0, w.Omitted.Dropped)}
	}

	if len(w.Coverage) > 0 {
		page.CoverageTruncated = len(w.Coverage) > MaxCoverageRows
		rows := make([]Coverage, 0, min(len(w.Coverage), MaxCoverageRows))
		for _, wc := range w.Coverage {
			if len(rows) == MaxCoverageRows {
				break
			}
			source := resource.SanitizeLine(wc.Source, MaxCoverageSourceChars)
			if source == "" {
				// A row that names no source explains nothing: the first column
				// is the whole of what makes the row readable.
				continue
			}
			rows = append(rows, Coverage{
				Source:    source,
				State:     CoerceCoverageState(wc.State),
				Reason:    resource.SanitizeLine(wc.Reason, MaxCoverageReasonChars),
				ElapsedMs: max(0, wc.ElapsedMs),
			})
		}
		if len(rows) > 0 {
			page.Coverage = rows
		}
	}

	if len(w.Notices) > 0 {
		notices := make([]Notice, 0, min(len(w.Notices), MaxNotices))
		for _, wn := range w.Notices {
			if len(notices) == MaxNotices {
				break
			}
			text := resource.SanitizeLine(wn.Text, MaxNoticeChars)
			if text == "" {
				continue
			}
			notices = append(notices, Notice{Tone: resource.CoerceTone(wn.Tone), Text: text})
		}
		if len(notices) > 0 {
			page.Notices = notices
		}
	}

	return page
}

// SanitizeOutcome enforces the act response's bounds. An outcome with no
// readable status is a failure, because an action whose result the host cannot
// read must never be reported to the user as having worked.
func SanitizeOutcome(w *WireOutcome) Outcome {
	if w == nil {
		return Outcome{Status: ActFailed}
	}
	out := Outcome{
		Status:  CoerceActStatus(w.Status),
		Message: resource.SanitizeLine(w.Message, MaxOutcomeMessageChars),
	}
	if len(w.Refresh) > 0 {
		seen := make(map[string]bool, len(w.Refresh))
		for _, raw := range w.Refresh {
			if len(out.Refresh) == MaxRefreshCollections {
				break
			}
			id := resource.SanitizeLine(raw, MaxCollectionIDChars)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out.Refresh = append(out.Refresh, id)
		}
	}
	if w.Open != nil {
		collection := resource.SanitizeLine(w.Open.Collection, MaxCollectionIDChars)
		id := resource.SanitizeLine(w.Open.ID, resource.MaxIdentityChars)
		// Both halves or neither: a row with no collection is not addressable,
		// and a collection with no row is not an open instruction.
		if collection != "" && id != "" {
			out.Open = &OpenTarget{Collection: collection, ID: id}
		}
	}
	return out
}
