package gitstatus

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/hostproto"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/workspaceinventory"
)

// The bound-host half of the Git plugin.
//
// One rule governs everything here: while bound, this plugin must not read this
// machine's repository for the project it is showing. A same-named checkout is a
// different project, and rendering its branch, its staged files, or its patches
// under a remote label is the failure the remote-project work exists to prevent
// — a green test that does it has still failed.
//
// The rule is kept structurally rather than by vigilance: while bound, repoRoot
// stays empty and hasRepo stays false, so no code path here has a directory to
// run git in even if one were reached.

// remoteBound reports that this plugin is showing another machine's project.
func (p *Plugin) remoteBound() bool {
	return p.ctx != nil && p.ctx.HostID != ""
}

// remoteWorkspaceID is the durable identity the host resolves repository state
// against: the bound worktree, or the project's main checkout.
//
// It is composed here rather than remembered, so a bind that moves to another
// worktree cannot leave this surface reading the previous one. It is deliberately
// the same id Files composes, because it is the same workspace.
func (p *Plugin) remoteWorkspaceID() string {
	if p.ctx == nil || p.ctx.ProjectKey == "" {
		return ""
	}
	key := p.ctx.HostWorktreeKey
	if key == "" {
		key = p.ctx.ProjectKey
	}
	return p.ctx.ProjectKey + ":" + string(workspaceinventory.KindWorktree) + ":" + key
}

func (p *Plugin) hostVerbs() hostproto.VerbCapabilities {
	if p.ctx == nil || p.ctx.HostVerbs == nil {
		return hostproto.VerbCapabilities{}
	}
	return p.ctx.HostVerbs()
}

func (p *Plugin) hostShows() bool {
	return p.ctx != nil && p.ctx.HostShows != nil && p.ctx.HostShows()
}

// remoteUnavailable is why this bound surface cannot read a repository, or ""
// when it can. A local project always answers "".
func (p *Plugin) remoteUnavailable() string {
	if !p.remoteBound() {
		return ""
	}
	if p.ctx.RemoteRunner == nil {
		return "[" + p.ctx.HostID + "] is not reachable from this Sidecar"
	}
	return remoteRepoUnavailable(p.ctx.HostID, p.remoteWorkspaceID(), p.hostVerbs(), p.hostShows())
}

// remoteAvailable reports that a bound surface has everything it needs to read
// the host.
func (p *Plugin) remoteAvailable() bool {
	return p.remoteBound() && p.remoteUnavailable() == ""
}

// unavailableReason is the sentence the Git tab paints instead of a repository.
//
// The connection reasons are knowable before any call; the host's own refusal —
// this workspace is not a git repository — is only knowable from an answer, so
// it is remembered from the last read and reported here alongside them.
func (p *Plugin) unavailableReason() string {
	if reason := p.remoteUnavailable(); reason != "" {
		return strings.TrimSpace(reason)
	}
	return strings.TrimSpace(p.remoteRefusal)
}

// repoSource is the repository-read seam for whichever machine owns this
// project. A nil source means there is nothing to read and the view is a
// sentence.
func (p *Plugin) repoSource() RepoSource {
	if p.repoSourceOverride != nil {
		return p.repoSourceOverride
	}
	if p.remoteBound() {
		if !p.remoteAvailable() {
			return nil
		}
		return &remoteRepoSource{
			hostID:      p.ctx.HostID,
			workspaceID: p.remoteWorkspaceID(),
			run:         p.ctx.RemoteRunner,
		}
	}
	if !p.hasRepo || p.tree == nil || p.repoRoot == "" {
		return nil
	}
	return localRepoSource{root: p.repoRoot, load: p.statusLoader, history: p.historyLoader}
}

// updateRemote is the bound plugin's whole message loop.
//
// It is a separate entry point rather than a guard inside the local one because
// the local handlers reach stage, discard, push, and the patch loaders, all of
// which take a working directory on this disk. Nothing routed here can arrive
// at one.
func (p *Plugin) updateRemote(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	if !p.remoteAvailable() {
		return p, nil
	}
	switch msg := msg.(type) {
	case tea.PasteMsg:
		// A bound pane's search bars are the same text inputs as a local
		// pane's, so a bracketed paste lands in them the same way.
		if handled, cmd := p.handleSearchPaste(msg); handled {
			return p, cmd
		}

	case app.RefreshMsg:
		return p, p.reload()

	case app.PluginFocusedMsg:
		return p, p.reload()

	case plugin.HostInventoryMsg:
		// The host's snapshot moved. That is the whole of a bound pane's live
		// refresh: internal/livewatch is a filesystem signal and does not cross
		// the boundary, so this and an explicit r are what it has.
		return p, p.reload()

	case RecentCommitsLoadedMsg:
		return p, p.applyRecentCommits(msg)

	case MoreCommitsLoadedMsg:
		return p, p.applyMoreCommits(msg)

	case FilteredCommitsLoadedMsg:
		return p, p.applyFilteredCommits(msg)

	case CommitPreviewLoadedMsg:
		return p, p.applyCommitPreview(msg)

	case BranchListLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.applyBranchList(msg)
		return p, nil

	case BranchErrorMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		// The local half opens an error modal, which is a view a bound pane does
		// not render. A failed listing closes the picker and says so once.
		p.closeBranchPicker()
		return p, func() tea.Msg {
			return app.ToastMsg{Message: "Branch list failed: " + msg.Err.Error(), Duration: 4 * time.Second, IsError: true}
		}

	case StatusSnapshotLoadedMsg:
		if plugin.IsStale(p.ctx, msg) || msg.RequestID != p.activeStatusRequestID {
			return p, nil
		}
		return p, p.applyStatusSnapshot(msg)

	case InlineDiffLoadedMsg:
		return p, p.applyInlineDiffLoaded(msg)

	case DiffLoadedMsg:
		return p, p.applyDiffLoaded(msg)

	case tea.KeyPressMsg:
		return p.updateRemoteKeys(msg)

	case tea.MouseMsg:
		return p.updateRemoteMouse(msg)
	}
	return p, nil
}

// updateRemoteMouse is the bound pane's pointer.
//
// The mouse was inert until now, which stopped being defensible once the commit
// list and the diff pane were on screen: a click that does nothing on rows that
// are visibly clickable reads as a broken pane, not as a refusal. So the subset
// that corresponds to gestures the keyboard already performs — selection,
// scrolling, pane focus, the divider, the scrollbars — routes to the local
// handlers themselves, exactly as the two diff surfaces do, because everything
// they reach reads through RepoSource.
//
// The one pointer gesture that does not is the double-click that opens a file
// in an editor, and it refuses from the table like its key. The modal view modes
// a bound pane never enters are simply not routed.
func (p *Plugin) updateRemoteMouse(msg tea.MouseMsg) (plugin.Plugin, tea.Cmd) {
	switch p.viewMode {
	case ViewModeStatus:
		if p.tree == nil {
			// The first status answer has not landed; there are no rows to hit.
			return p, nil
		}
		return p.handleMouse(msg)
	case ViewModeDiff:
		return p.handleDiffMouse(msg)
	case ViewModeBranchPicker:
		// The picker lists the host's branches; clicking one refuses by name,
		// through the same call the keyboard's Enter goes through.
		return p.handleBranchPickerMouse(msg)
	}
	return p, nil
}

// updateRemoteKeys is the bound pane's keyboard: movement, the sidebar, the
// host's patches and history, and an explicit refresh.
//
// It is deliberately the reachable subset — wiring a key here that has nothing
// behind it would tell the user a gesture works when it does not — and every
// key it does not perform answers out of the refusal table rather than falling
// through to silence.
//
// The two diff surfaces are the local handlers themselves, not copies of them.
// Everything they reach now reads through RepoSource, and the two loaders that
// still take a directory on this disk — a folder's aggregate patch and a
// full-file view — refuse for a bound pane at their own door, so a key that
// lands on one is a no-op rather than a local git invocation.
func (p *Plugin) updateRemoteKeys(msg tea.KeyPressMsg) (plugin.Plugin, tea.Cmd) {
	// The overlays are this viewer's own state over rows the host answered, so
	// they route to the handlers themselves. Search runs here over the page in
	// hand; the path filter goes back to the host as a query.
	if p.historySearchMode {
		return p.updateHistorySearch(msg)
	}
	if p.pathFilterMode {
		return p.updatePathFilter(msg)
	}
	if p.viewMode == ViewModeBranchPicker {
		return p.updateBranchPicker(msg)
	}
	if p.tree == nil {
		if msg.String() == "r" {
			return p, p.reload()
		}
		return p, nil
	}
	if p.viewMode == ViewModeDiff {
		return p.updateDiff(msg)
	}
	if p.activePane == PaneDiff {
		return p.updateStatusDiffPane(msg)
	}

	entries := p.treeEntries()
	// The write keys whose only meaning is a write. Keys that mean one thing on
	// a file row and another on a commit row resolve the row below and refuse
	// out of the same table.
	if what, ok := remoteRefusedKeys[msg.String()]; ok {
		return p, p.refuseRemote(what)
	}
	switch msg.String() {
	case "j", "down":
		return p, p.cursorDown()

	case "k", "up":
		return p, p.cursorUp()

	case "g":
		return p, p.cursorToTop()

	case "G":
		return p, p.cursorToBottom()

	case "h":
		// Jump to the commits section.
		fileCount := len(entries)
		if len(p.activeCommits()) > 0 && p.cursor < fileCount {
			p.cursor = fileCount
			p.ensureCommitVisible(0)
			return p, p.autoLoadCommitPreview()
		}

	case "l", "right":
		if p.sidebarVisible {
			if p.cursorOnCommit() && p.previewCommit != nil {
				p.activePane = PaneDiff
			} else if p.selectedDiffFile != "" {
				p.activePane = PaneDiff
			}
		}

	case "enter":
		// A folder's expansion and a commit's preview pane are both this
		// viewer's own display state. Enter on a file opens it in an editor,
		// which no host verb answers.
		if p.cursorOnCommit() {
			if p.previewCommit != nil {
				p.activePane = PaneDiff
			}
		} else if p.cursor < len(entries) {
			if entries[p.cursor].IsFolder {
				entries[p.cursor].IsExpanded = !entries[p.cursor].IsExpanded
				return p, p.autoLoadDiff()
			}
			return p, p.openFileEntry(entries[p.cursor].Path)
		}

	case "O":
		// Following a file to the Files tab is navigation, and that tab is
		// bound to the same host, so the file it lands on is the host's.
		if !p.cursorOnCommit() && p.cursor < len(entries) {
			return p, p.openInFileBrowser(entries[p.cursor].Path)
		}

	case "o":
		// The commit link is built from the remote URL `repo status` already
		// returned. The URL is the host's fact and opening it is this machine's
		// browser, which is the correct division of the two.
		if p.cursorOnCommit() {
			return p, p.openCommitInGitHub()
		}

	case "d":
		if p.cursorOnCommit() {
			if p.previewCommit != nil {
				p.activePane = PaneDiff
			}
			return p, nil
		}
		if p.cursor < len(entries) && !entries[p.cursor].IsFolder {
			entry := entries[p.cursor]
			p.diffReturnMode = p.viewMode
			p.viewMode = ViewModeDiff
			p.diffFile = entry.Path
			p.diffStaged = entry.Staged
			p.diffCommit = ""
			p.diffCommitSubject = ""
			p.diffCommitShortHash = ""
			p.diffScroll = 0
			p.diffLoaded = false
			return p, p.loadDiff(entry.Path, entry.Staged, entry.Status)
		}

	case "b":
		// The picker lists the host's branches. Switching to one is a write and
		// refuses by name; the modal's own hint says so before it is tried.
		p.branchReturnMode = p.viewMode
		p.branchCursor = 0
		p.viewMode = ViewModeBranchPicker
		p.clearBranchPickerModal()
		return p, p.loadBranches()

	case "f":
		// On a commit, filter the log by its author — a host filter, so the
		// answer is about the whole log rather than about the page in hand. On
		// a file row the same key is fetch, which is a write on the host.
		if !p.hasSelectedCommit() {
			return p, p.refuseRemote(refuseFetch)
		}
		commits := p.activeCommits()
		commit := commits[p.selectedCommitIndex()]
		p.historyFilterAuthor = commit.Author
		p.historyFilterActive = true
		return p, p.loadFilteredCommits()

	case "F":
		if p.historyFilterActive {
			p.historyFilterAuthor = ""
			p.historyFilterPath = ""
			p.historyFilterActive = false
			p.filteredCommits = nil
			if p.showCommitGraph && len(p.recentCommits) > 0 {
				p.commitGraphLines = ComputeGraphForCommits(p.recentCommits)
			}
		}

	case "p":
		if p.cursorOnCommit() {
			p.pathFilterMode = true
			p.pathFilterField.Reset()
			p.pathFilterField.Focus()
		}

	case "/":
		// Subject search is the viewer's: it runs over the rows the host
		// already answered, and the sidebar header names the page it searched.
		if p.cursorOnCommit() {
			if p.historySearchState == nil {
				p.historySearchState = NewHistorySearchState()
			}
			p.historySearchState.Reset()
			p.historySearchMode = true
		}

	case "n":
		if p.historySearchState != nil && p.historySearchState.Committed && len(p.historySearchState.Matches) > 0 {
			p.historySearchState.Cursor++
			if p.historySearchState.Cursor >= len(p.historySearchState.Matches) {
				p.historySearchState.Cursor = 0
			}
			return p, p.jumpToSearchMatch()
		}

	case "N":
		if p.historySearchState != nil && p.historySearchState.Committed && len(p.historySearchState.Matches) > 0 {
			p.historySearchState.Cursor--
			if p.historySearchState.Cursor < 0 {
				p.historySearchState.Cursor = len(p.historySearchState.Matches) - 1
			}
			return p, p.jumpToSearchMatch()
		}

	case "esc":
		if p.historySearchState != nil && p.historySearchState.Committed {
			p.clearSearchState()
		}

	case "v":
		// The graph is drawn from the parent hashes the rows carry, whichever
		// machine answered them.
		if p.cursorOnCommit() {
			p.showCommitGraph = !p.showCommitGraph
			_ = state.SetGitGraphEnabled(p.showCommitGraph)
			if p.showCommitGraph {
				p.commitGraphLines = ComputeGraphForCommits(p.activeCommits())
			}
		}

	case "y":
		// The commit is already in hand and the clipboard is this machine's.
		if p.cursorOnCommit() {
			return p, p.copyCommitToClipboard()
		}

	case "Y":
		if p.cursorOnCommit() {
			return p, p.copyCommitIDToClipboard()
		}

	case "\\":
		p.toggleSidebar()
		if !p.sidebarVisible {
			return p, appmsg.ShowFlash("Sidebar hidden (\\ to restore)")
		}

	case "r":
		return p, p.reload()
	}
	return p, nil
}

// renderBoundView is the Git tab while it is showing another machine.
//
// "Not connected", "too old for the repository contract", "no bound worktree",
// and "not a git repository" are four different things for the user to do next,
// so they are four different sentences. None of them is this machine's no-repo
// view, which offers to run `git init` here, under a label that names the host.
func (p *Plugin) renderBoundView() string {
	if reason := p.unavailableReason(); reason != "" {
		return styles.Title.Render(pluginName) + "\n\n" +
			styles.Muted.Render(pluginName+" is unavailable: "+reason)
	}
	if p.tree == nil {
		return styles.Title.Render(pluginName) + "\n\n" +
			styles.Muted.Render("Loading ["+p.ctx.HostID+"]…")
	}
	// The full-screen diff and the branch picker are the same views they are
	// locally: one renders a patch and the other a list of branches, and which
	// machine produced either is below the renderer.
	switch p.viewMode {
	case ViewModeDiff:
		if p.sidebarVisible {
			return p.renderDiffTwoPane()
		}
		return p.renderDiffModal()
	case ViewModeBranchPicker:
		return p.renderBranchPicker()
	}
	return p.renderThreePaneView()
}

// boundDiffPaneNotice is what a bound diff pane says when the selected row is
// one this build cannot answer honestly, or "" when it can.
//
// It is derived at render time from the row itself rather than remembered, so
// it cannot survive the selection that produced it.
func (p *Plugin) boundDiffPaneNotice() string {
	if !p.remoteBound() {
		return ""
	}
	entries := p.treeEntries()
	if p.cursor >= len(entries) || !entries[p.cursor].IsFolder {
		return ""
	}
	// A folder row is an aggregate, not one repository read. Reading it from a
	// host would be one round trip per file in the folder, on a cursor move.
	return "A folder's combined patch is not read from [" + p.ctx.HostID + "]. Press enter to open it and read one file's patch."
}

// boundFullFileNotice names the one diff view a bound pane cannot draw.
func boundFullFileNotice(hostID string) string {
	return "Full-file view is not available on [" + hostID + "]: the host answers patches, not file contents."
}

// noticeContentHeight is the room left for a patch under a sentence explaining
// what is missing from it. The pane's height is the app's, not this view's, so
// the notice comes out of the content rather than growing past it.
func noticeContentHeight(contentHeight int) int {
	if contentHeight <= 2 {
		return 1
	}
	return contentHeight - 2
}

// truncationLabel marks a patch the source had to cut, in the header where the
// view mode is. A short patch rendered as if it were whole is a lie about the
// change, and it is not one the reader can see for themselves.
func truncationLabel(truncated bool) string {
	if !truncated {
		return ""
	}
	return " " + lipgloss.NewStyle().Foreground(styles.Warning).Render("(truncated)")
}
