package filebrowser

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/plugin"
)

// openFile returns a command to open a file in the user's editor.
func (p *Plugin) openFile(path string) tea.Cmd {
	return p.openFileAtLine(path, 0)
}

// openFileAtLine returns a command to open a file in the user's editor at a specific line.
func (p *Plugin) openFileAtLine(path string, lineNo int) tea.Cmd {
	return func() tea.Msg {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			editor = "vim"
		}
		fullPath := filepath.Join(p.ctx.WorkDir, path)
		return plugin.OpenFileMsg{Editor: editor, Path: fullPath, LineNo: lineNo}
	}
}

// revealInFileManager reveals the file/directory in the system file manager.
func (p *Plugin) revealInFileManager(path string) tea.Cmd {
	return func() tea.Msg {
		fullPath := filepath.Join(p.ctx.WorkDir, path)
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			// macOS: open -R reveals in Finder with file selected
			cmd = exec.Command("open", "-R", fullPath)
		case "windows":
			// Windows: explorer /select, reveals in Explorer with file selected
			cmd = exec.Command("explorer", "/select,", fullPath)
		case "linux":
			// Linux: xdg-open opens the parent directory
			cmd = exec.Command("xdg-open", filepath.Dir(fullPath))
		default:
			return RevealErrorMsg{Err: fmt.Errorf("reveal not supported on %s", runtime.GOOS)}
		}
		if err := cmd.Start(); err != nil {
			return RevealErrorMsg{Err: err}
		}
		return nil
	}
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

		// Check for case-only rename (e.g., "File.txt" -> "file.txt")
		// On case-insensitive filesystems, we need a two-step rename via temp file
		isCaseOnlyRename := strings.EqualFold(src, dst) && src != dst

		if isCaseOnlyRename {
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
			f.Close()
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
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

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

	if p.contentSearchQuery == "" {
		return
	}

	query := strings.ToLower(p.contentSearchQuery)

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
				EndCol:   absIdx + len(p.contentSearchQuery),
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

// openQuickOpen enters quick open mode.
func (p *Plugin) openQuickOpen() (plugin.Plugin, tea.Cmd) {
	// Build file cache if empty
	if len(p.quickOpenFiles) == 0 {
		p.buildFileCache()
	}

	p.quickOpenMode = true
	p.quickOpenQuery = ""
	p.quickOpenCursor = 0
	p.updateQuickOpenMatches()

	return p, nil
}

// updateQuickOpenMatches filters files using fuzzy matching.
func (p *Plugin) updateQuickOpenMatches() {
	p.quickOpenMatches = FuzzyFilter(p.quickOpenFiles, p.quickOpenQuery, quickOpenMaxResults)

	// Reset cursor if out of bounds
	if p.quickOpenCursor >= len(p.quickOpenMatches) {
		if len(p.quickOpenMatches) > 0 {
			p.quickOpenCursor = len(p.quickOpenMatches) - 1
		} else {
			p.quickOpenCursor = 0
		}
	}
}

// selectQuickOpenMatch opens the selected file in preview.
func (p *Plugin) selectQuickOpenMatch() (plugin.Plugin, tea.Cmd) {
	if len(p.quickOpenMatches) == 0 || p.quickOpenCursor >= len(p.quickOpenMatches) {
		return p, nil
	}

	match := p.quickOpenMatches[p.quickOpenCursor]

	// Close quick open
	p.quickOpenMode = false
	p.quickOpenQuery = ""
	p.quickOpenMatches = nil
	p.quickOpenCursor = 0

	// Find the file in tree by walking down the path (efficient)
	targetNode := p.findAndExpandPath(match.Path)

	if targetNode != nil {
		p.tree.Flatten()

		// Move tree cursor to file
		if idx := p.tree.IndexOf(targetNode); idx >= 0 {
			p.treeCursor = idx
			p.ensureTreeCursorVisible()
		}
	}

	// Load preview and pin (explicit user selection)
	p.activePane = PanePreview
	cmd := p.openTab(match.Path, TabOpenReplace)
	p.pinTab(p.activeTab)
	return p, cmd
}

// openProjectSearch enters project-wide search mode.
func (p *Plugin) openProjectSearch() (plugin.Plugin, tea.Cmd) {
	p.projectSearchMode = true
	p.projectSearchState = NewProjectSearchState()
	p.clearProjectSearchModal()
	return p, nil
}

// openProjectSearchResult opens the selected search result.
func (p *Plugin) openProjectSearchResult() (plugin.Plugin, tea.Cmd) {
	state := p.projectSearchState
	if state == nil || len(state.Results) == 0 {
		return p, nil
	}

	path, lineNo := state.GetSelectedFile()
	if path == "" {
		return p, nil
	}

	// Capture search query before closing project search
	searchQuery := state.Query

	// Close project search
	p.projectSearchMode = false
	p.projectSearchState = nil
	p.clearProjectSearchModal()

	// Find the file in tree by walking down the path (efficient - only loads needed dirs)
	targetNode := p.findAndExpandPath(path)

	if targetNode != nil {
		p.tree.Flatten()

		// Move tree cursor to file
		if idx := p.tree.IndexOf(targetNode); idx >= 0 {
			p.treeCursor = idx
			p.ensureTreeCursorVisible()
		}
	}

	// Load preview and pin (explicit user selection)
	p.activePane = PanePreview
	cmd := p.openTabAtLine(path, lineNo, TabOpenReplace)
	p.pinTab(p.activeTab)

	// Set up content search for highlighting the matched term
	if searchQuery != "" {
		p.contentSearchMode = true
		p.contentSearchCommitted = true // Skip input phase, enable n/N navigation
		p.contentSearchQuery = searchQuery
		p.contentSearchMatches = nil // Will be populated after preview loads
		p.contentSearchCursor = 0
		if cmd == nil {
			p.updateContentMatches()
			if lineNo > 0 && len(p.contentSearchMatches) > 0 {
				p.scrollToNearestMatch(p.previewScroll)
			}
		}
	}

	return p, cmd
}

// openProjectSearchResultInNewTab opens the selected search result in a new tab.
func (p *Plugin) openProjectSearchResultInNewTab() (plugin.Plugin, tea.Cmd) {
	state := p.projectSearchState
	if state == nil || len(state.Results) == 0 {
		return p, nil
	}

	path, lineNo := state.GetSelectedFile()
	if path == "" {
		return p, nil
	}

	// Close project search
	p.projectSearchMode = false
	p.projectSearchState = nil
	p.clearProjectSearchModal()

	// Find the file in tree by walking down the path
	targetNode := p.findAndExpandPath(path)

	if targetNode != nil {
		p.tree.Flatten()

		// Move tree cursor to file
		if idx := p.tree.IndexOf(targetNode); idx >= 0 {
			p.treeCursor = idx
			p.ensureTreeCursorVisible()
		}
	}

	// Load preview in new tab
	p.activePane = PanePreview
	cmd := p.openTabAtLine(path, lineNo, TabOpenNew)

	return p, cmd
}

// buildFileCache walks the filesystem to build the quick open file list.
// Respects gitignore and has limits to prevent issues on huge repos.
func (p *Plugin) buildFileCache() {
	p.quickOpenFiles = nil
	p.quickOpenError = ""

	ctx, cancel := context.WithTimeout(context.Background(), quickOpenTimeout)
	defer cancel()

	count := 0
	limited := false

	err := filepath.WalkDir(p.ctx.WorkDir, func(path string, d fs.DirEntry, err error) error {
		// Check timeout
		select {
		case <-ctx.Done():
			limited = true
			return filepath.SkipAll
		default:
		}

		if err != nil {
			return nil // Skip unreadable entries
		}

		// Get relative path
		rel, err := filepath.Rel(p.ctx.WorkDir, path)
		if err != nil {
			return nil
		}

		// Skip root
		if rel == "." {
			return nil
		}

		// Skip common large/irrelevant directories
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == ".sidecar" || name == "node_modules" || name == "vendor" ||
				name == ".next" || name == "dist" || name == "build" ||
				name == "__pycache__" || name == ".venv" || name == "venv" ||
				name == ".idea" || name == ".vscode" {
				return filepath.SkipDir
			}
			// Check gitignore for directories
			if p.tree != nil && p.tree.gitIgnore != nil {
				if p.tree.gitIgnore.IsIgnored(rel, true) {
					return filepath.SkipDir
				}
			}
			return nil // Don't add directories to file list
		}

		// Skip hidden files (starting with .)
		if strings.HasPrefix(name, ".") {
			return nil
		}

		// Check gitignore for files
		if p.tree != nil && p.tree.gitIgnore != nil {
			if p.tree.gitIgnore.IsIgnored(rel, false) {
				return nil
			}
		}

		// Check file limit
		if count >= quickOpenMaxFiles {
			limited = true
			return filepath.SkipAll
		}

		p.quickOpenFiles = append(p.quickOpenFiles, rel)
		count++
		return nil
	})

	if err != nil && err != filepath.SkipAll {
		p.quickOpenError = "scan error: " + err.Error()
	} else if limited {
		if ctx.Err() != nil {
			p.quickOpenError = "scan timed out"
		} else {
			p.quickOpenError = "limited to 50000 files"
		}
	}

	// Sort files by path for consistent ordering
	sort.Strings(p.quickOpenFiles)
}

// buildDirCache walks the filesystem to build directory list for path auto-complete.
// Similar to buildFileCache but collects directories instead of files.
func (p *Plugin) buildDirCache() {
	p.dirCache = nil

	ctx, cancel := context.WithTimeout(context.Background(), quickOpenTimeout)
	defer cancel()

	count := 0
	limited := false

	err := filepath.WalkDir(p.ctx.WorkDir, func(path string, d fs.DirEntry, err error) error {
		// Check timeout
		select {
		case <-ctx.Done():
			limited = true
			return filepath.SkipAll
		default:
		}

		if err != nil {
			return nil // Skip unreadable entries
		}

		// Get relative path
		rel, err := filepath.Rel(p.ctx.WorkDir, path)
		if err != nil {
			return nil
		}

		// Skip root
		if rel == "." {
			return nil
		}

		// Only process directories
		if !d.IsDir() {
			return nil
		}

		name := d.Name()

		// Skip common large/irrelevant directories
		if name == ".git" || name == ".sidecar" || name == "node_modules" || name == "vendor" ||
			name == ".next" || name == "dist" || name == "build" ||
			name == "__pycache__" || name == ".venv" || name == "venv" ||
			name == ".idea" || name == ".vscode" {
			return filepath.SkipDir
		}

		// Check gitignore for directories
		if p.tree != nil && p.tree.gitIgnore != nil {
			if p.tree.gitIgnore.IsIgnored(rel, true) {
				return filepath.SkipDir
			}
		}

		// Check dir limit
		if count >= dirCacheMaxDirs {
			limited = true
			return filepath.SkipAll
		}

		p.dirCache = append(p.dirCache, rel)
		count++
		return nil
	})

	if err != nil && err != filepath.SkipAll {
		// Silently ignore scan errors for directory cache
		_ = err
	}
	_ = limited // Ignore limited status for now

	// Sort directories for consistent ordering
	sort.Strings(p.dirCache)
}

// getPathSuggestions returns fuzzy-matched directory suggestions for the query.
func (p *Plugin) getPathSuggestions(query string) []string {
	if query == "" {
		return nil
	}

	// Build cache if needed
	if len(p.dirCache) == 0 {
		p.buildDirCache()
	}

	// Use FuzzyFilter for matching
	matches := FuzzyFilter(p.dirCache, query, dirCacheMaxResults)

	var paths []string
	for _, m := range matches {
		paths = append(paths, m.Path)
	}
	return paths
}

// updateSearchMatches finds all nodes matching the search query.
func (p *Plugin) updateSearchMatches() {
	p.searchMatches = nil
	if p.searchQuery == "" {
		return
	}

	query := strings.ToLower(p.searchQuery)

	// Walk entire tree (not just visible nodes), excluding ignored files
	p.walkTreeForSearch(p.tree.Root, func(node *FileNode) {
		name := strings.ToLower(node.Name)
		if strings.Contains(name, query) {
			p.searchMatches = append(p.searchMatches, node)
		}
	})

	// Limit matches to prevent UI clutter
	if len(p.searchMatches) > 20 {
		p.searchMatches = p.searchMatches[:20]
	}

	p.searchCursor = 0
}

// findAndExpandPath finds a file by path, expanding only the directories along the way.
// This is much faster than walkTree for deep paths since it only loads needed directories.
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

		// Expand directory if it's not the final component
		if found.IsDir && i < len(parts)-1 {
			found.IsExpanded = true
		}

		current = found
	}

	return current
}

// walkTree recursively visits all nodes in the tree.
func (p *Plugin) walkTree(node *FileNode, fn func(*FileNode)) {
	if node == nil {
		return
	}
	for _, child := range node.Children {
		fn(child)
		if child.IsDir {
			// Load children if not already loaded
			if len(child.Children) == 0 {
				_ = p.tree.loadChildren(child)
			}
			p.walkTree(child, fn)
		}
	}
}

// walkTreeForSearch recursively visits nodes, skipping ignored files/directories.
func (p *Plugin) walkTreeForSearch(node *FileNode, fn func(*FileNode)) {
	if node == nil {
		return
	}
	for _, child := range node.Children {
		// Skip .git explicitly (even if not in .gitignore)
		if child.Name == ".git" {
			continue
		}
		// Skip ignored files and directories (like node_modules, etc.)
		if child.IsIgnored {
			continue
		}
		fn(child)
		if child.IsDir {
			// Load children if not already loaded
			if len(child.Children) == 0 {
				_ = p.tree.loadChildren(child)
			}
			p.walkTreeForSearch(child, fn)
		}
	}
}

// jumpToSearchMatch navigates to the currently selected search match.
func (p *Plugin) jumpToSearchMatch() {
	if len(p.searchMatches) == 0 || p.searchCursor >= len(p.searchMatches) {
		return
	}

	matchPath := p.searchMatches[p.searchCursor].Path

	// Find the node fresh from the current tree (in case tree was refreshed)
	var currentNode *FileNode
	p.walkTree(p.tree.Root, func(node *FileNode) {
		if node.Path == matchPath {
			currentNode = node
		}
	})

	if currentNode == nil {
		return
	}

	// Expand all parent directories to make the match visible
	p.expandParents(currentNode)

	// Reflatten the tree after expanding
	p.tree.Flatten()

	// Find the match in the flat list
	for i, node := range p.tree.FlatList {
		if node.Path == matchPath {
			p.treeCursor = i
			p.ensureTreeCursorVisible()
			break
		}
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
	// Find the file node in tree
	var targetNode *FileNode
	p.walkTree(p.tree.Root, func(node *FileNode) {
		if node.Path == path {
			targetNode = node
		}
	})

	if targetNode == nil {
		// File not found in tree, maybe it's new or ignored
		return p, nil
	}

	// Expand parents to make the file visible
	p.expandParents(targetNode)
	p.tree.Flatten()

	// Move tree cursor to file
	if idx := p.tree.IndexOf(targetNode); idx >= 0 {
		p.treeCursor = idx
		p.ensureTreeCursorVisible()
	}

	// Load preview
	p.activePane = PanePreview
	return p, p.openTab(path, TabOpenNew)
}

// clearTextSelection resets all text selection state.
func (p *Plugin) clearTextSelection() {
	p.textSelectionActive = false
	p.textSelectionStart = 0
	p.textSelectionEnd = 0
	p.textSelectionAnchor = 0
}

// hasTextSelection returns true if there is an active or completed selection.
func (p *Plugin) hasTextSelection() bool {
	return p.textSelectionStart != p.textSelectionEnd ||
		p.textSelectionActive ||
		(p.textSelectionStart == 0 && p.textSelectionEnd > 0)
}

// isLineSelected returns true if the given line is within the selection range.
func (p *Plugin) isLineSelected(lineNo int) bool {
	if !p.hasTextSelection() {
		return false
	}
	return lineNo >= p.textSelectionStart && lineNo <= p.textSelectionEnd
}

// copySelectedTextToClipboard copies the selected lines to the system clipboard.
func (p *Plugin) copySelectedTextToClipboard() tea.Cmd {
	return func() tea.Msg {
		if !p.hasTextSelection() {
			return nil
		}

		start := p.textSelectionStart
		end := p.textSelectionEnd
		if start > end {
			start, end = end, start
		}

		// Build selected text from raw lines (not syntax-highlighted)
		var sb strings.Builder
		for i := start; i <= end; i++ {
			if i < len(p.previewLines) {
				sb.WriteString(p.previewLines[i])
				if i < end {
					sb.WriteString("\n")
				}
			}
		}

		text := sb.String()
		if text == "" {
			return nil
		}

		if err := clipboard.WriteAll(text); err != nil {
			return msg.ShowToast("Copy failed: "+err.Error(), 2*time.Second)
		}

		lineCount := end - start + 1
		return msg.ShowToast(fmt.Sprintf("Copied %d line(s)", lineCount), 2*time.Second)
	}
}

// copyFileContentsToClipboard copies the entire file contents to the system clipboard.
func (p *Plugin) copyFileContentsToClipboard() tea.Cmd {
	return func() tea.Msg {
		if len(p.previewLines) == 0 {
			return msg.ToastMsg{Message: "No content to copy", Duration: 2 * time.Second}
		}
		text := strings.Join(p.previewLines, "\n")
		if err := clipboard.WriteAll(text); err != nil {
			return msg.ToastMsg{Message: "Copy failed: " + err.Error(), Duration: 2 * time.Second, IsError: true}
		}
		return msg.ToastMsg{Message: fmt.Sprintf("Copied %d lines", len(p.previewLines)), Duration: 2 * time.Second}
	}
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
	if p.contentSearchMode && p.contentSearchQuery != "" {
		p.updateContentMatches()
	}
}

// renderMarkdownContent renders the current preview content as markdown.
func (p *Plugin) renderMarkdownContent() {
	if p.markdownRenderer == nil || len(p.previewLines) == 0 {
		return
	}
	content := strings.Join(p.previewLines, "\n")
	// Subtract padding for margins
	width := p.previewWidth - 6
	if width < 30 {
		width = 30
	}
	p.markdownRendered = p.markdownRenderer.RenderContent(content, width)
}
