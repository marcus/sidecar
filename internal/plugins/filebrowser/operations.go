package filebrowser

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/clip"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/filefind"
	"github.com/marcus/sidecar/internal/markdown"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/textselect"
	"github.com/marcus/sidecar/internal/tty"
)

// openFile returns a command to open a file in the user's editor.
func (p *Plugin) openFile(path string) tea.Cmd {
	return p.openFileAtLine(path, 0)
}

// openFileAtLine returns a command to open a file in the user's editor at a specific line.
func (p *Plugin) openFileAtLine(path string, lineNo int) tea.Cmd {
	return func() tea.Msg {
		// One resolution for every $EDITOR launch in Sidecar, so the file
		// browser cannot drift from notes or git status.
		editor := tty.ResolveEditor()
		fullPath := filepath.Join(p.ctx.WorkDir, path)
		return plugin.OpenFileMsg{Editor: editor, Path: fullPath, LineNo: lineNo}
	}
}

// getCurrentPreviewLine returns the 0-indexed line number to use when opening the current
// preview file in an editor. Uses middle of visible viewport by default, or selection start
// if text is selected.
func (p *Plugin) getCurrentPreviewLine() int {
	// If text is selected, use selection start
	if p.selection.HasSelection() {
		return p.selection.Start.Line
	}

	// Calculate middle of viewport
	visibleHeight := p.visibleContentHeight()
	if visibleHeight <= 0 {
		return p.previewScroll
	}

	targetLine := p.previewScroll + (visibleHeight / 2)

	// Clamp to valid range
	maxLine := len(p.previewLines) - 1
	if maxLine < 0 {
		maxLine = 0
	}
	if targetLine > maxLine {
		targetLine = maxLine
	}
	if targetLine < 0 {
		targetLine = 0
	}

	return targetLine
}

// openFileAtCurrentLine opens the current preview file at the current preview position.
func (p *Plugin) openFileAtCurrentLine(path string) tea.Cmd {
	lineNo := p.getCurrentPreviewLine()
	return p.openFileAtLine(path, lineNo)
}

// revealInFileManager reveals the file/directory in the system file manager.
func (p *Plugin) revealInFileManager(path string) tea.Cmd {
	if p.ctx == nil {
		return nil
	}
	return docview.Reveal(p.ctx.WorkDir, path)
}

// validateDestPath checks that destination path is within workdir.
// Returns error if path escapes the project directory.
func (p *Plugin) validateDestPath(dstPath string) error {
	// Clean and resolve the destination path
	cleanDst := filepath.Clean(dstPath)

	// Get absolute paths for comparison
	absDst, err := filepath.Abs(cleanDst)
	if err != nil {
		return fmt.Errorf("invalid destination path")
	}

	absWorkDir, err := filepath.Abs(p.ctx.WorkDir)
	if err != nil {
		return fmt.Errorf("failed to resolve work directory")
	}

	// Check if destination is within workdir
	relPath, err := filepath.Rel(absWorkDir, absDst)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("cannot move files outside project directory")
	}

	return nil
}

// validateFilename checks for invalid filename characters and patterns.
func validateFilename(name string) error {
	if name == "" {
		return fmt.Errorf("filename cannot be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid filename")
	}
	// Check for null bytes and control characters
	for _, r := range name {
		if r == 0 || (r < 32 && r != '\t') {
			return fmt.Errorf("filename contains invalid characters")
		}
	}
	// Check for characters invalid on common filesystems
	invalidChars := []rune{'<', '>', ':', '"', '|', '?', '*'}
	for _, c := range invalidChars {
		if strings.ContainsRune(name, c) {
			return fmt.Errorf("filename contains invalid character: %c", c)
		}
	}
	return nil
}

// parentDirPath returns the parent directory of a root-relative path, with ""
// meaning the project root itself.
func parentDirPath(rel string) string {
	if rel == "" {
		return ""
	}
	dir := filepath.Dir(rel)
	if dir == "." || dir == string(filepath.Separator) {
		return ""
	}
	return dir
}

// pathWithin reports whether child is parent or lives underneath it. Both paths
// are root-relative, and "" means the project root, which contains everything.
// The trailing separator is essential: a bare prefix test would report "foobar"
// as living inside "foo".
func pathWithin(child, parent string) bool {
	if parent == "" {
		return true
	}
	if child == "" {
		return false
	}
	c := filepath.Clean(child)
	pa := filepath.Clean(parent)
	if c == pa {
		return true
	}
	return strings.HasPrefix(c, pa+string(filepath.Separator))
}

// displayDropDir renders a destination directory for humans: the project root
// has no name of its own.
func displayDropDir(dir string) string {
	if dir == "" {
		return "./"
	}
	return dir + "/"
}

// validateMove reports why moving srcRel to dstRel is not allowed, returning ""
// when the move is fine. Both paths are root-relative and dstRel is the *full*
// destination path, including the name the node will carry when it lands.
//
// This is deliberately free of plugin state so every move surface can share it:
// the drag-drop gesture and the keyboard 'm' dialog both call it, and they used
// to disagree - a drag into a folder's own subtree said "Can't move a folder
// into itself" while the same move typed into the dialog reached os.Rename and
// surfaced a raw "invalid argument".
//
// Agent-parity gap (deliberate, and the reason this is state-free): there is no
// CLI/API/MCP surface for moving a file in the files plugin yet, so the move
// capability is TUI-only today. A future headless surface must call this rather
// than restating the rules a third time.
func validateMove(srcRel string, srcIsDir bool, dstRel string) string {
	if srcRel == "" {
		return "Can't move the project root"
	}
	dstDir := parentDirPath(dstRel)
	// Into itself: a directory cannot contain itself. Into its own subtree is
	// the dangerous one - os.Rename of a directory into its own descendant
	// either fails or, on some systems, detaches the subtree. pathWithin's
	// separator guard is what keeps "foo" from matching "foobar".
	if dstDir == srcRel || (srcIsDir && pathWithin(dstDir, srcRel)) {
		return "Can't move a folder into itself"
	}
	// Where it already is: a no-op move.
	if filepath.Clean(dstRel) == filepath.Clean(srcRel) {
		return filepath.Base(srcRel) + " is already in " + displayDropDir(dstDir)
	}
	return ""
}

// executeFileOp performs the pending file operation.
func (p *Plugin) executeFileOp() (plugin.Plugin, tea.Cmd) {
	input := p.fileOpTextInput.Value()

	// Handle create operations
	if p.fileOpMode == FileOpCreateFile || p.fileOpMode == FileOpCreateDir {
		if input == "" {
			p.fileOpMode = FileOpNone
			return p, nil
		}
		return p, p.doCreate(input, p.fileOpMode == FileOpCreateDir)
	}

	if p.fileOpTarget == nil || input == "" {
		p.fileOpMode = FileOpNone
		return p, nil
	}

	// Validate filename (for rename: the input, for move: basename of path)
	var nameToValidate string
	if p.fileOpMode == FileOpRename {
		nameToValidate = input
	} else {
		nameToValidate = filepath.Base(input)
	}
	if err := validateFilename(nameToValidate); err != nil {
		p.fileOpError = err.Error()
		return p, nil
	}

	srcPath := filepath.Join(p.ctx.WorkDir, p.fileOpTarget.Path)
	var dstPath string

	switch p.fileOpMode {
	case FileOpRename:
		// Rename: new name in same directory
		// Disallow path separators in rename (would be a move)
		if strings.Contains(input, string(filepath.Separator)) || strings.Contains(input, "/") {
			p.fileOpError = "use 'm' to move to a different directory"
			return p, nil
		}
		dstPath = filepath.Join(filepath.Dir(srcPath), input)
	case FileOpMove:
		// Move: relative path from workdir only (no absolute paths)
		if filepath.IsAbs(input) {
			p.fileOpError = "absolute paths not allowed"
			return p, nil
		}
		// Same rules the drag gesture enforces, from the same helper, so the two
		// move surfaces cannot drift apart.
		if reason := validateMove(p.fileOpTarget.Path, p.fileOpTarget.IsDir, input); reason != "" {
			p.fileOpError = reason
			return p, nil
		}
		dstPath = filepath.Join(p.ctx.WorkDir, input)
	}

	// Validate destination is within project directory
	if err := p.validateDestPath(dstPath); err != nil {
		p.fileOpError = err.Error()
		return p, nil
	}

	// For moves, check if parent directory exists
	if p.fileOpMode == FileOpMove {
		parentDir := filepath.Dir(dstPath)
		if _, err := os.Stat(parentDir); os.IsNotExist(err) {
			// Enter confirmation mode to ask user if they want to create the directory
			p.fileOpConfirmCreate = true
			p.fileOpConfirmPath = parentDir
			return p, nil
		}
	}

	return p, p.doFileOp(srcPath, dstPath)
}

// isCaseOnlyRename reports whether src -> dst is a rename that changes nothing
// but the letter case of the name, within one directory (e.g. "File.txt" ->
// "file.txt"). On a case-insensitive filesystem that needs a two-step rename via
// a temp file, which skips doFileOp's destination-exists check - so the test has
// to be as narrow as possible.
//
// Comparing whole paths with EqualFold would also match foo/x.txt -> Foo/x.txt,
// a real move between two directories that coexist on a case-sensitive
// filesystem, and would then clobber an existing Foo/x.txt with no warning.
// That distinction cannot be exercised on a case-insensitive filesystem (macOS
// APFS by default), which is why this predicate is unit-tested directly rather
// than only through the filesystem.
func isCaseOnlyRename(src, dst string) bool {
	return filepath.Dir(src) == filepath.Dir(dst) &&
		strings.EqualFold(filepath.Base(src), filepath.Base(dst)) &&
		src != dst
}

// doFileOp performs the actual file move/rename operation.
func (p *Plugin) doFileOp(src, dst string) tea.Cmd {
	return func() tea.Msg {
		// Create parent directories if needed (for move)
		dstDir := filepath.Dir(dst)
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return FileOpErrorMsg{Err: err}
		}

		// Check if source and destination are the same
		if src == dst {
			return FileOpErrorMsg{Err: fmt.Errorf("source and destination are the same")}
		}

		if isCaseOnlyRename(src, dst) {
			// Two-step rename: src -> temp -> dst
			tempPath := src + ".sidecar-rename-tmp"
			if err := os.Rename(src, tempPath); err != nil {
				return FileOpErrorMsg{Err: fmt.Errorf("rename failed: %w", err)}
			}
			if err := os.Rename(tempPath, dst); err != nil {
				// Try to rollback
				_ = os.Rename(tempPath, src)
				return FileOpErrorMsg{Err: fmt.Errorf("rename failed: %w", err)}
			}
		} else {
			// Check if destination exists (only for non-case-only renames)
			if _, err := os.Stat(dst); err == nil {
				return FileOpErrorMsg{Err: fmt.Errorf("destination already exists: %s", filepath.Base(dst))}
			}

			// Perform the move/rename
			if err := os.Rename(src, dst); err != nil {
				return FileOpErrorMsg{Err: err}
			}
		}

		return FileOpSuccessMsg{Src: src, Dst: dst}
	}
}

// doCreate creates a new file or directory.
func (p *Plugin) doCreate(name string, isDir bool) tea.Cmd {
	return func() tea.Msg {
		// Validate filename
		if err := validateFilename(name); err != nil {
			return FileOpErrorMsg{Err: err}
		}

		// Determine parent directory based on current selection
		var parentDir string
		if p.fileOpTarget != nil {
			if p.fileOpTarget.IsDir {
				parentDir = filepath.Join(p.ctx.WorkDir, p.fileOpTarget.Path)
			} else {
				// If a file is selected, create in its parent directory
				parentDir = filepath.Join(p.ctx.WorkDir, filepath.Dir(p.fileOpTarget.Path))
			}
		} else {
			parentDir = p.ctx.WorkDir
		}

		fullPath := filepath.Join(parentDir, name)

		// Validate path is within project
		absPath, err := filepath.Abs(fullPath)
		if err != nil {
			return FileOpErrorMsg{Err: fmt.Errorf("invalid path")}
		}
		absWorkDir, err := filepath.Abs(p.ctx.WorkDir)
		if err != nil {
			return FileOpErrorMsg{Err: fmt.Errorf("failed to resolve work directory")}
		}
		relPath, err := filepath.Rel(absWorkDir, absPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			return FileOpErrorMsg{Err: fmt.Errorf("cannot create files outside project directory")}
		}

		// Check if already exists
		if _, err := os.Stat(fullPath); err == nil {
			return FileOpErrorMsg{Err: fmt.Errorf("already exists: %s", name)}
		}

		if isDir {
			if err := os.MkdirAll(fullPath, 0755); err != nil {
				return FileOpErrorMsg{Err: err}
			}
		} else {
			// Create parent directories if needed
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				return FileOpErrorMsg{Err: err}
			}
			f, err := os.Create(fullPath)
			if err != nil {
				return FileOpErrorMsg{Err: err}
			}
			_ = f.Close()
		}

		return CreateSuccessMsg{Path: fullPath, IsDir: isDir}
	}
}

// doDelete deletes the target file or directory.
func (p *Plugin) doDelete() tea.Cmd {
	return func() tea.Msg {
		if p.fileOpTarget == nil {
			return FileOpErrorMsg{Err: fmt.Errorf("no target selected")}
		}

		fullPath := filepath.Join(p.ctx.WorkDir, p.fileOpTarget.Path)

		// Validate path is within project (safety check)
		absPath, err := filepath.Abs(fullPath)
		if err != nil {
			return FileOpErrorMsg{Err: fmt.Errorf("invalid path")}
		}
		absWorkDir, err := filepath.Abs(p.ctx.WorkDir)
		if err != nil {
			return FileOpErrorMsg{Err: fmt.Errorf("failed to resolve work directory")}
		}
		relPath, err := filepath.Rel(absWorkDir, absPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			return FileOpErrorMsg{Err: fmt.Errorf("cannot delete files outside project directory")}
		}

		// Don't allow deleting the project root
		if relPath == "." {
			return FileOpErrorMsg{Err: fmt.Errorf("cannot delete project root")}
		}

		// Remove file or directory (recursively for directories)
		if err := os.RemoveAll(fullPath); err != nil {
			return FileOpErrorMsg{Err: err}
		}

		return DeleteSuccessMsg{Path: fullPath}
	}
}

// doPaste copies the clipboard file/directory to the target location.
func (p *Plugin) doPaste(targetNode *FileNode) tea.Cmd {
	return func() tea.Msg {
		if p.clipboardPath == "" {
			return FileOpErrorMsg{Err: fmt.Errorf("nothing to paste")}
		}

		srcPath := filepath.Join(p.ctx.WorkDir, p.clipboardPath)

		// Determine destination directory
		var destDir string
		if targetNode.IsDir {
			destDir = filepath.Join(p.ctx.WorkDir, targetNode.Path)
		} else {
			// If a file is selected, paste into its parent directory
			destDir = filepath.Join(p.ctx.WorkDir, filepath.Dir(targetNode.Path))
		}

		// Check if source exists
		srcInfo, err := os.Stat(srcPath)
		if err != nil {
			return FileOpErrorMsg{Err: fmt.Errorf("source not found: %s", filepath.Base(p.clipboardPath))}
		}

		// Generate destination path
		srcName := filepath.Base(p.clipboardPath)
		destPath := filepath.Join(destDir, srcName)

		// Handle name conflicts by appending _copy or _copy2, etc.
		if _, err := os.Stat(destPath); err == nil {
			base := srcName
			ext := filepath.Ext(srcName)
			if ext != "" {
				base = srcName[:len(srcName)-len(ext)]
			}
			for i := 1; ; i++ {
				suffix := "_copy"
				if i > 1 {
					suffix = fmt.Sprintf("_copy%d", i)
				}
				newName := base + suffix + ext
				destPath = filepath.Join(destDir, newName)
				if _, err := os.Stat(destPath); os.IsNotExist(err) {
					break
				}
				if i > 100 {
					return FileOpErrorMsg{Err: fmt.Errorf("too many copies")}
				}
			}
		}

		// Validate destination is within project
		absDestPath, err := filepath.Abs(destPath)
		if err != nil {
			return FileOpErrorMsg{Err: fmt.Errorf("invalid path")}
		}
		absWorkDir, err := filepath.Abs(p.ctx.WorkDir)
		if err != nil {
			return FileOpErrorMsg{Err: fmt.Errorf("failed to resolve work directory")}
		}
		relPath, err := filepath.Rel(absWorkDir, absDestPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			return FileOpErrorMsg{Err: fmt.Errorf("cannot paste outside project directory")}
		}

		// Copy file or directory
		if srcInfo.IsDir() {
			if err := copyDir(srcPath, destPath); err != nil {
				return FileOpErrorMsg{Err: err}
			}
		} else {
			if err := copyFile(srcPath, destPath); err != nil {
				return FileOpErrorMsg{Err: err}
			}
		}

		return PasteSuccessMsg{Src: srcPath, Dst: destPath}
	}
}

// copyFile copies a single file.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// copyDir recursively copies a directory.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// updateContentMatches finds all matches of the search query in preview content.
func (p *Plugin) updateContentMatches() {
	p.contentSearchMatches = nil
	p.contentSearchCursor = 0

	if p.contentSearchQuery() == "" {
		return
	}

	query := strings.ToLower(p.contentSearchQuery())

	for lineNo, line := range p.getSearchableLines() {
		lineLower := strings.ToLower(line)
		startIdx := 0
		for {
			idx := strings.Index(lineLower[startIdx:], query)
			if idx == -1 {
				break
			}
			absIdx := startIdx + idx
			p.contentSearchMatches = append(p.contentSearchMatches, ContentMatch{
				LineNo:   lineNo,
				StartCol: absIdx,
				EndCol:   absIdx + len(p.contentSearchQuery()),
			})
			startIdx = absIdx + 1
		}
	}

	// Scroll to first match if any
	if len(p.contentSearchMatches) > 0 {
		p.scrollToContentMatch()
	}
}

// scrollToContentMatch scrolls the preview to show the current match.
// Only scrolls if the match is outside the visible viewport (vim-style).
func (p *Plugin) scrollToContentMatch() {
	if len(p.contentSearchMatches) == 0 || p.contentSearchCursor >= len(p.contentSearchMatches) {
		return
	}

	match := p.contentSearchMatches[p.contentSearchCursor]
	visibleHeight := p.visibleContentHeight()

	maxScroll := len(p.getPreviewLines()) - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}

	// If match is already visible, don't scroll (avoids jarring viewport jumps)
	scrollMargin := 2 // keep a small margin from viewport edges
	viewTop := p.previewScroll + scrollMargin
	viewBottom := p.previewScroll + visibleHeight - scrollMargin
	if match.LineNo >= viewTop && match.LineNo < viewBottom {
		return
	}

	// Match is off-screen: scroll to bring it into view
	if match.LineNo < p.previewScroll+scrollMargin {
		// Match is above viewport: put it near the top with margin
		p.previewScroll = match.LineNo - scrollMargin
	} else {
		// Match is below viewport: put it near the bottom with margin
		p.previewScroll = match.LineNo - visibleHeight + scrollMargin + 1
	}

	if p.previewScroll < 0 {
		p.previewScroll = 0
	}
	if p.previewScroll > maxScroll {
		p.previewScroll = maxScroll
	}
}

// scrollToNearestMatch finds and jumps to the match nearest to the target line.
// Used when opening a file from project search to jump to the selected match.
func (p *Plugin) scrollToNearestMatch(targetLine int) {
	if len(p.contentSearchMatches) == 0 {
		return
	}

	// Find match closest to target line
	bestIdx := 0
	bestDist := intAbs(p.contentSearchMatches[0].LineNo - targetLine)

	for i, match := range p.contentSearchMatches {
		dist := intAbs(match.LineNo - targetLine)
		if dist < bestDist {
			bestDist = dist
			bestIdx = i
		}
	}

	p.contentSearchCursor = bestIdx
	p.scrollToContentMatch()
}

// intAbs returns the absolute value of x.
func intAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ensureFileCache starts a background scan when the quick-open file cache is
// missing or the disk has moved under it. The existing cache is left in place
// until the scan lands, so the modal keeps showing something usable.
func (p *Plugin) ensureFileCache() tea.Cmd {
	if p.ctx == nil {
		return nil
	}
	return p.quickOpen.Ensure(p.ctx.WorkDir, p.ctx.Epoch)
}

// ensureDirCache is ensureFileCache for the path auto-complete directory list.
func (p *Plugin) ensureDirCache() tea.Cmd {
	if p.ctx == nil {
		return nil
	}
	return p.dirCache.EnsureDirs(p.ctx.WorkDir, p.ctx.Epoch)
}

// getPathSuggestions returns fuzzy-matched directory suggestions for the query,
// plus a command to (re)build the directory cache when it is missing or stale.
func (p *Plugin) getPathSuggestions(query string) ([]string, tea.Cmd) {
	if query == "" {
		return nil, nil
	}

	cmd := p.ensureDirCache()

	// Use FuzzyFilter for matching
	matches := filefind.FuzzyFilter(p.dirCache.Files, query, dirCacheMaxResults)

	var paths []string
	for _, m := range matches {
		paths = append(paths, m.Path)
	}
	return paths, cmd
}

// updateFileOpSuggestions recomputes the move modal's path auto-complete from
// the current directory cache, returning any scan command the filter needs.
func (p *Plugin) updateFileOpSuggestions() tea.Cmd {
	if p.fileOpMode != FileOpMove {
		return nil
	}

	query := p.fileOpTextInput.Value()
	if query == "" {
		p.fileOpSuggestions = nil
		p.fileOpSuggestionIdx = -1
		p.fileOpShowSuggestions = false
		return nil
	}

	suggestions, cmd := p.getPathSuggestions(query)
	p.fileOpSuggestions = suggestions
	p.fileOpSuggestionIdx = -1
	p.fileOpShowSuggestions = len(suggestions) > 0
	return cmd
}

// updateSearchMatches finds all files matching the search query using the quick
// open cache, returning a command that rebuilds the cache when it is missing or
// stale. Matches come from whatever the cache holds now; the scan result
// refreshes them when it lands.
func (p *Plugin) updateSearchMatches() tea.Cmd {
	p.searchMatches = nil
	if p.searchQuery() == "" {
		return nil
	}

	// Same cache as Ctrl+P
	cmd := p.ensureFileCache()

	// Use fuzzy filter on cached files (same as Ctrl+P)
	p.searchMatches = filefind.FuzzyFilter(p.quickOpen.Files, p.searchQuery(), 20)
	p.searchCursor = 0
	p.followSearchCursor()
	return cmd
}

// refilterSearchMatches re-runs the search filter against a cache that just
// landed, keeping the user on the match they had selected. The query did not
// change, so resetting the selection would move the ground under them.
func (p *Plugin) refilterSearchMatches() {
	var selected string
	if p.searchCursor >= 0 && p.searchCursor < len(p.searchMatches) {
		selected = p.searchMatches[p.searchCursor].Path
	}

	p.searchMatches = nil
	if p.searchQuery() == "" {
		p.searchCursor = 0
		p.followSearchCursor()
		return
	}
	p.searchMatches = filefind.FuzzyFilter(p.quickOpen.Files, p.searchQuery(), 20)

	p.searchCursor = 0
	for i, match := range p.searchMatches {
		if match.Path == selected {
			p.searchCursor = i
			break
		}
	}
	p.followSearchCursor()
}

// findAndExpandPath finds a file by path, expanding only the directories along the way.
// Only the directories named by the path are read from disk; siblings stay unloaded.
func (p *Plugin) findAndExpandPath(path string) *FileNode {
	if p.tree == nil || p.tree.Root == nil || path == "" {
		return nil
	}

	// Split path into components
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) == 0 {
		return nil
	}

	// Walk down the tree following the path
	current := p.tree.Root
	var descended []*FileNode
	for i, part := range parts {
		// Load children if not already loaded
		if len(current.Children) == 0 && current.IsDir {
			_ = p.tree.loadChildren(current)
		}

		// Find the matching child
		var found *FileNode
		for _, child := range current.Children {
			if child.Name == part {
				found = child
				break
			}
		}

		if found == nil {
			return nil // Path not found in tree
		}

		// Directories along the way are expanded only once the whole path
		// resolves; a miss must leave the tree exactly as it was.
		if found.IsDir && i < len(parts)-1 {
			descended = append(descended, found)
		}

		current = found
	}

	for _, dir := range descended {
		dir.IsExpanded = true
	}
	return current
}

// jumpToSearchMatch navigates to the currently selected search match.
func (p *Plugin) jumpToSearchMatch() {
	if len(p.searchMatches) == 0 || p.searchCursor >= len(p.searchMatches) {
		return
	}

	matchPath := p.searchMatches[p.searchCursor].Path

	// Use efficient targeted tree walking
	targetNode := p.findAndExpandPath(matchPath)
	if targetNode == nil {
		return
	}

	p.tree.Flatten()
	p.syncWatcherDirs()

	if idx := p.tree.IndexOf(targetNode); idx >= 0 {
		p.treeCursor = idx
		p.ensureTreeCursorVisible()
	}
}

// expandParents expands all ancestor directories of a node.
func (p *Plugin) expandParents(node *FileNode) {
	if node == nil || node.Parent == nil {
		return
	}

	// Don't try to expand the root itself
	if node.Parent == p.tree.Root {
		return
	}

	// Recursively expand parents first (going up the tree)
	p.expandParents(node.Parent)

	// Then expand this node's parent directory
	if node.Parent.IsDir && !node.Parent.IsExpanded {
		// Load children if not already loaded
		if len(node.Parent.Children) == 0 {
			_ = p.tree.loadChildren(node.Parent)
		}
		node.Parent.IsExpanded = true
	}
}

// navigateToFile navigates the file browser to a specific file path.
// Used when other plugins request navigation (e.g., git plugin opening file in browser).
func (p *Plugin) navigateToFile(path string) (plugin.Plugin, tea.Cmd) {
	// Targeted descent: only the directories named by the path are read.
	targetNode := p.findAndExpandPath(path)
	if targetNode == nil {
		// File not found in tree, maybe it's new or ignored
		return p, nil
	}

	// Expand parents to make the file visible
	p.expandParents(targetNode)
	p.tree.Flatten()
	p.syncWatcherDirs()

	// Move tree cursor to file
	if idx := p.tree.IndexOf(targetNode); idx >= 0 {
		p.treeCursor = idx
		p.ensureTreeCursorVisible()
	}

	// Load preview
	p.activePane = PanePreview
	return p, p.openTab(path, TabOpenNew)
}

// copySelectedTextToClipboard copies the selected text to every clipboard
// within reach — the system clipboard and, over OSC 52, the terminal's — with
// character-level precision using the shared ui.SelectionState.
func (p *Plugin) copySelectedTextToClipboard() tea.Cmd {
	if !p.selection.HasSelection() {
		return nil
	}
	startLine := p.selection.Start.Line
	endLine := p.selection.End.Line
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}
	if startLine < 0 {
		startLine = 0
	}
	display := p.previewDisplayLines()
	if endLine >= len(display) {
		endLine = len(display) - 1
	}
	if endLine < startLine {
		return nil
	}

	result := p.selection.SelectedText(p.previewSelectionLines(startLine, endLine+1), startLine, 8)
	if len(result) == 0 {
		return nil
	}

	lineCount := endLine - startLine + 1
	return clip.Copy(textselect.SelectionText(result), func(r clip.Result) tea.Msg {
		return msg.FlashMsg{Text: r.Message(fmt.Sprintf("Copied %d line(s)", lineCount))}
	})
}

// copyFileContentsToClipboard copies the entire file contents to the system clipboard.
func (p *Plugin) copyFileContentsToClipboard() tea.Cmd {
	if p.ctx != nil && p.previewFile != "" {
		return docview.YankContents(p.ctx.WorkDir, p.previewFile)
	}
	if len(p.previewLines) == 0 {
		return msg.ShowFlash("No content to copy")
	}
	lineCount := len(p.previewLines)
	return clip.Copy(strings.Join(p.previewLines, "\n"), func(r clip.Result) tea.Msg {
		return msg.FlashMsg{Text: r.Message(fmt.Sprintf("Copied %d lines", lineCount))}
	})
}

// isMarkdownFile returns true if the current preview file is a markdown file.
func (p *Plugin) isMarkdownFile() bool {
	if p.previewFile == "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(p.previewFile))
	return ext == ".md" || ext == ".markdown"
}

// getPreviewLines returns the current preview content lines based on render mode.
// When markdown render mode is active, returns rendered lines; otherwise raw/highlighted.
func (p *Plugin) getPreviewLines() []string {
	if p.markdownRenderMode && p.isMarkdownFile() && len(p.markdownRendered) > 0 {
		return p.markdownRendered
	}
	if len(p.previewHighlighted) > 0 {
		return p.previewHighlighted
	}
	return p.previewLines
}

// getSearchableLines returns plain-text lines for content search.
// In markdown render mode, strips ANSI codes from rendered lines.
func (p *Plugin) getSearchableLines() []string {
	if p.markdownRenderMode && p.isMarkdownFile() && len(p.markdownRendered) > 0 {
		stripped := make([]string, len(p.markdownRendered))
		for i, line := range p.markdownRendered {
			stripped[i] = ansi.Strip(line)
		}
		return stripped
	}
	return p.previewLines
}

// toggleMarkdownRender toggles between rendered and raw markdown view.
func (p *Plugin) toggleMarkdownRender() {
	if !p.isMarkdownFile() {
		return
	}
	p.markdownRenderMode = !p.markdownRenderMode
	if p.markdownRenderMode && len(p.markdownRendered) == 0 {
		p.renderMarkdownContent()
	}
	// Re-run search if active (line indices change between modes)
	if p.contentSearchMode && p.contentSearchQuery() != "" {
		p.updateContentMatches()
	}
}

// handleThemeChanged rebuilds the rendered markdown preview under the new
// theme. Rendered row indices can shift, so content search matches are
// recomputed and scroll/selection are preserved where still valid, clamped
// otherwise.
func (p *Plugin) handleThemeChanged() {
	if !p.markdownRenderMode || !p.isMarkdownFile() {
		return
	}
	p.markdownRendered = nil
	p.renderMarkdownContent()

	lineCount := len(p.getPreviewLines())
	p.clampPreviewScroll()
	if lineCount == 0 {
		p.selection.Clear()
	} else {
		maxLine := lineCount - 1
		if p.selection.Active {
			if p.selection.Start.Line > maxLine || p.selection.End.Line > maxLine {
				p.selection.Clear()
			}
		}
	}

	if p.contentSearchMode && p.contentSearchQuery() != "" {
		p.updateContentMatches()
	}
}

// renderMarkdownContent renders the current preview content as markdown.
//
// Rendered rows draw into the full content column — the line-number gutter is
// empty in this mode. Glamour's document style reserves a 2-column margin on
// each side of its word-wrap width: body text wraps at W−4 and no output line
// exceeds W−2. Passing contentWidth+2 keeps that left margin as this mode's
// visual indent while the text reaches the frame's right edge, instead of the
// whole render stopping several columns short of it (td-65095b).
func (p *Plugin) renderMarkdownContent() {
	if p.markdownRenderer == nil || len(p.previewLines) == 0 {
		return
	}
	content := strings.Join(p.previewLines, "\n")
	width := p.previewContentWidth() + 2
	if width < markdown.MinWidthForMarkdown {
		width = markdown.MinWidthForMarkdown
	}
	p.markdownRendered = p.markdownRenderer.RenderContent(content, width)
}
