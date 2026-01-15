package gitstatus

// GraphColumn tracks the state of one branch line in the commit graph.
type GraphColumn struct {
	CommitHash string // Hash of commit this column is tracking toward
	Active     bool   // Whether this column is currently in use
}

// GraphLine holds the ASCII characters for one commit row.
type GraphLine struct {
	Chars []rune // The ASCII characters for this line (*, |, \, /, etc.)
	Width int    // Total visual width of this line
}

// GraphState maintains state while computing graph for commit list.
type GraphState struct {
	columns []GraphColumn // Active branch columns
}

// NewGraphState creates a new graph computation state.
func NewGraphState() *GraphState {
	return &GraphState{
		columns: make([]GraphColumn, 0),
	}
}

// findCommitColumn finds which column is tracking toward a given commit hash.
// Returns -1 if not found.
func (g *GraphState) findCommitColumn(hash string) int {
	for i, col := range g.columns {
		if col.Active && col.CommitHash == hash {
			return i
		}
	}
	return -1
}

// findOrCreateColumn finds an empty column or creates a new one.
func (g *GraphState) findOrCreateColumn(hash string) int {
	// Look for inactive column to reuse
	for i, col := range g.columns {
		if !col.Active {
			g.columns[i] = GraphColumn{CommitHash: hash, Active: true}
			return i
		}
	}
	// Create new column
	g.columns = append(g.columns, GraphColumn{CommitHash: hash, Active: true})
	return len(g.columns) - 1
}

// updateColumns updates state after processing a commit.
func (g *GraphState) updateColumns(commit *Commit, col int) {
	if len(commit.ParentHashes) == 0 {
		// Root commit - deactivate column
		if col >= 0 && col < len(g.columns) {
			g.columns[col].Active = false
		}
		return
	}

	// First parent continues in same column
	if col >= 0 && col < len(g.columns) {
		g.columns[col].CommitHash = commit.ParentHashes[0]
	}

	// Additional parents need their own columns (for merge commits)
	for i := 1; i < len(commit.ParentHashes); i++ {
		g.findOrCreateColumn(commit.ParentHashes[i])
	}
}

// ComputeGraphLine generates the GraphLine for a commit given current state.
// Updates state for the next commit.
func (g *GraphState) ComputeGraphLine(commit *Commit) GraphLine {
	line := GraphLine{Chars: make([]rune, 0)}

	// Find which column this commit is in (match hash to tracked column)
	commitCol := g.findCommitColumn(commit.Hash)

	// If commit not found in columns, it starts a new branch
	if commitCol == -1 {
		commitCol = g.findOrCreateColumn(commit.Hash)
	}

	// For merge commits, find where second parent will come from
	mergeFromCol := -1
	if len(commit.ParentHashes) > 1 {
		mergeFromCol = g.findCommitColumn(commit.ParentHashes[1])
		if mergeFromCol == -1 {
			// Second parent not yet in a column - it will get one
			mergeFromCol = len(g.columns)
		}
	}

	// Determine number of columns to render (include merge target if needed)
	numCols := len(g.columns)
	if mergeFromCol >= numCols {
		numCols = mergeFromCol + 1
	}

	// Build the line characters
	for i := 0; i < numCols; i++ {
		// Check if this column is active (exists and active)
		colActive := i < len(g.columns) && g.columns[i].Active

		if i == commitCol {
			line.Chars = append(line.Chars, '*') // This commit
		} else if commit.IsMerge && mergeFromCol != -1 && i > commitCol && i <= mergeFromCol {
			// Merge line coming from right
			if i == mergeFromCol {
				line.Chars = append(line.Chars, '\\')
			} else if colActive {
				line.Chars = append(line.Chars, '|') // Passing branch with merge over
			} else {
				line.Chars = append(line.Chars, '_')
			}
		} else if commit.IsMerge && mergeFromCol != -1 && i < commitCol && i >= mergeFromCol {
			// Merge line coming from left
			if i == mergeFromCol {
				line.Chars = append(line.Chars, '/')
			} else if colActive {
				line.Chars = append(line.Chars, '|') // Passing branch with merge over
			} else {
				line.Chars = append(line.Chars, '_')
			}
		} else if colActive {
			line.Chars = append(line.Chars, '|') // Passing branch
		} else {
			line.Chars = append(line.Chars, ' ') // Empty column
		}
		line.Chars = append(line.Chars, ' ') // Spacing between columns
	}

	// Update columns for next commit
	g.updateColumns(commit, commitCol)

	line.Width = len(line.Chars)
	return line
}

// ComputeGraphForCommits generates GraphLine for each commit in order.
func ComputeGraphForCommits(commits []*Commit) []GraphLine {
	if len(commits) == 0 {
		return nil
	}

	g := NewGraphState()
	lines := make([]GraphLine, len(commits))
	for i, commit := range commits {
		lines[i] = g.ComputeGraphLine(commit)
	}
	return lines
}

// GraphLineString returns the graph line as a plain string (for testing/debug).
func (gl *GraphLine) String() string {
	return string(gl.Chars)
}
