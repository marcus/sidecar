package jj

// GraphRow represents one row in the rendered commit graph.
type GraphRow struct {
	// Commit is non-nil for commit rows, nil for connector rows.
	Commit *StructuredCommit
	// Column is the lane index for this row's primary node.
	Column int
	// Lanes describes active lanes at this row. Each entry is a commit ID
	// the lane connects toward, or "" if the lane is inactive.
	Lanes []string
	// MergeFrom is set on connector rows to indicate a merge line coming
	// from this column index (right side) into Column (left side). -1 if none.
	MergeFrom int
	// ForkTo is set on connector rows to indicate a fork line going
	// from Column to this column index. -1 if none.
	ForkTo int
}

// LayoutGraph takes topologically sorted commits and produces graph rows
// with lane assignments for rendering.
func LayoutGraph(commits []StructuredCommit) []GraphRow {
	if len(commits) == 0 {
		return nil
	}

	var rows []GraphRow
	// lanes tracks active lanes: each slot holds the commit ID the lane
	// connects toward (the commit we expect to see next in that lane).
	var lanes []string

	for i := range commits {
		c := &commits[i]

		// Find which lane this commit occupies (a lane expecting this commit ID).
		col := -1
		for j, target := range lanes {
			if target == c.CommitID {
				col = j
				break
			}
		}

		if col == -1 {
			// New lane -- allocate at first empty slot or append.
			col = findEmptyLane(lanes)
			if col == len(lanes) {
				lanes = append(lanes, "")
			}
		}

		// Snapshot lanes for this commit row.
		laneCopy := make([]string, len(lanes))
		copy(laneCopy, lanes)

		// Clear this lane -- the commit has arrived.
		lanes[col] = ""

		// Assign parent edges to lanes.
		for pi, parentID := range c.Parents {
			if pi == 0 {
				// First parent continues in the same column.
				lanes[col] = parentID
			} else {
				// Additional parents get a new lane.
				slot := findEmptyLane(lanes)
				if slot == len(lanes) {
					lanes = append(lanes, "")
				}
				lanes[slot] = parentID
			}
		}

		// Add the commit row.
		rows = append(rows, GraphRow{
			Commit:    c,
			Column:    col,
			Lanes:     laneCopy,
			MergeFrom: -1,
			ForkTo:    -1,
		})

		// Add a connector row between commits (except after the last).
		if i < len(commits)-1 {
			connLanes := make([]string, len(lanes))
			copy(connLanes, lanes)
			rows = append(rows, GraphRow{
				Commit:    nil,
				Column:    col,
				Lanes:     connLanes,
				MergeFrom: -1,
				ForkTo:    -1,
			})
		}
	}

	return rows
}

func findEmptyLane(lanes []string) int {
	for i, l := range lanes {
		if l == "" {
			return i
		}
	}
	return len(lanes)
}
