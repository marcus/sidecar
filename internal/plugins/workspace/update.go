package workspace

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	app "github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/contentpanes"
	"github.com/marcus/sidecar/internal/docview"
	"github.com/marcus/sidecar/internal/gitinit"
	"github.com/marcus/sidecar/internal/inlineedit"
	"github.com/marcus/sidecar/internal/issueview"
	"github.com/marcus/sidecar/internal/migration"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/noteview"
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/pluginbrowser"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
	"github.com/marcus/sidecar/internal/resourceview"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/termpreview"
	"github.com/marcus/sidecar/internal/tty"
	"github.com/marcus/sidecar/internal/uirequest"
	"github.com/marcus/sidecar/internal/workspacecreate"
	"github.com/marcus/sidecar/internal/workspacediff"
	"github.com/marcus/sidecar/internal/workspaceops"
)

// update handles messages. The public Update wrapper in terminal_control.go
// reconciles persistent terminal subscriptions after every state transition.
func (p *Plugin) update(msg tea.Msg) (plugin.Plugin, tea.Cmd) {
	var cmds []tea.Cmd
	if scoped, ok := msg.(interface{ GetOperationScope() OperationScope }); ok {
		scope := scoped.GetOperationScope()
		if scope.OperationID != "" && !p.scopeMatches(scope) {
			if created, isCreated := msg.(CreateWorktreeAddedMsg); isCreated && created.Worktree != nil {
				p.deferredCreations = append(p.deferredCreations, created)
			}
			return p, nil
		}
	}

	// An embedded terminal's messages are scope-tagged, so every live pane
	// editor is offered each one and the session that owns the scope acts.
	if tty.IsTerminalMessage(msg) {
		if cmd := p.routeDocEditMsg(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	switch msg := msg.(type) {
	case notify.SeedLaneTrackersMsg:
		p.queueAgentLaneSeeds(msg.Notifications)
		return p, nil
	case notify.PostedMsg:
		if p.ownsAgentLaneNotification(msg.Notification) {
			p.agentLaneTracker.ReconcilePosted(msg.Notification)
		}
		return p, nil
	case termpreview.HostBackgroundMsg:
		p.terminalDefaultBackground = msg.ANSI
		return p, nil
	case terminalLinkRevalidatedMsg:
		return p, p.applyTerminalLinkRevalidated(msg)
	case inlineedit.StartedMsg:
		return p, p.applyDocEditStarted(msg)
	case inlineedit.ExitedMsg:
		return p, p.applyDocEditExited(msg)
	case activityAnimationTickMsg:
		if msg.generation != p.activityAnimationGeneration {
			return p, nil
		}
		p.activityAnimationScheduled = false
		if !p.activityAnimationNeeded() {
			p.activityAnimationFrame = 0
			return p, nil
		}
		p.activityAnimationFrame++
		return p, p.startActivityAnimation()

	case selectionAutoScrollTickMsg:
		return p, p.advanceSelectionAutoScroll(msg)

	case tea.FocusMsg:
		cmds = appendActivityAnimationCmd(cmds, p.startActivityAnimation())

	case shellStartupResultMsg:
		return p, p.applyShellStartup(msg)

	case terminalHistoryLoadedMsg:
		return p, p.applyTerminalHistory(msg)
	case terminalSearchHistoryLoadedMsg:
		return p, p.applyTerminalSearchHistory(msg)
	case contentpanes.Result:
		return p, p.applyWorkspaceDeckResult(msg)
	case docLinkResolvedMsg:
		p.applyDocLinkResolved(msg)
		return p, nil
	case docview.LoadedMsg:
		if p.contentDeck != nil {
			return p, p.applyWorkspaceDeckBroadcast(msg)
		}
		p.applyDocLoaded(msg)
		return p, nil
	case docview.GitInfoMsg:
		if p.docInfo != nil {
			p.docInfo.ApplyGit(msg)
		}
		return p, nil
	case docSearchMsg:
		// A pane's own search traffic — a file scan, a debounce tick, a ripgrep
		// result — routed back to the pane that issued it. The surface drops what
		// is stale for its epoch.
		return p, p.applyDocSearchMsg(msg)
	case docview.RevealErrorMsg:
		return p, appmsg.ShowToast("Reveal failed: "+msg.Err.Error(), 4*time.Second)
	case issueview.LoadedMsg:
		if p.contentDeck != nil {
			return p, p.applyWorkspaceDeckBroadcast(msg)
		}
		p.applyIssueLoaded(msg)
		return p, nil
	case noteview.LoadedMsg:
		if p.contentDeck != nil {
			return p, p.applyWorkspaceDeckBroadcast(msg)
		}
		p.applyNoteLoaded(msg)
		return p, nil
	case resourceview.ResolvedMsg:
		if p.contentDeck != nil {
			return p, p.applyWorkspaceDeckBroadcast(msg)
		}
		p.applyResourceResolved(msg)
		return p, nil
	case resourceview.OpenRowMsg:
		// Enter on a collection row. It travels as a message rather than a
		// direct call so the row opens through this surface's own open journey,
		// which is what focuses a tab that is already there instead of fetching
		// it twice.
		return p, p.openRowInResourceLeaf(msg.Ref)
	case pluginbrowser.ListedMsg, pluginbrowser.GotMsg, pluginbrowser.ActedMsg, pluginbrowser.DescribedMsg, pluginbrowser.QueryDebouncedMsg, pluginbrowser.ChangedMsg:
		// A collection or row tab's own answers. They reach the tab that asked
		// the same way a resolve reaches a card: as a broadcast each viewer
		// either owns or does not, so a page for a tab that has closed lands
		// nowhere rather than in the wrong pane.
		return p, p.applyPluginBrowserMsg(msg)

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		// Background project plugins still receive layout so returning to the
		// project has current dimensions, but only the visible/focused Workspaces
		// surface owns tmux geometry. Global Workspaces may be displaying this same
		// pane with a different split while the project surface is covered.
		if !p.focused {
			return p, nil
		}
		if p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active {
			// Poll captures cursor atomically - no separate query needed
			resizeCmds := []tea.Cmd{p.resizeInteractivePaneCmd(), p.pollInteractivePaneImmediate()}
			// Also resize the non-interactive pane so both sides match the new window dimensions
			if p.shellLeafVisible() {
				if p.terminalPaneIsPanel(p.interactiveState.LeafID) {
					resizeCmds = append(resizeCmds, p.resizeSelectedPaneCmd())
				} else {
					resizeCmds = append(resizeCmds, p.resizeTermPanelPaneCmd())
				}
			}
			return p, tea.Batch(resizeCmds...)
		}
		// Resize selected pane and terminal panel so capture-pane output matches preview width
		resizeCmds := []tea.Cmd{p.resizeSelectedPaneCmd()}
		if p.shellLeafVisible() {
			resizeCmds = append(resizeCmds, p.resizeTermPanelPaneCmd())
		}
		return p, tea.Batch(resizeCmds...)

	case app.PluginFocusedMsg:
		if p.focused {
			focusCmds := []tea.Cmd{
				// A global Workspaces mutation updates the shared catalog while this
				// project projection is covered. Refreshing on focus updates only the
				// project the user returned to and also covers external mutations.
				func() tea.Msg { return RefreshMsg{} },
				p.maybeAutoCreateShell(),
				p.startActivityAnimation(),
				p.scheduleSessionValidation(60 * time.Second),
				p.pollAllAgentStatusesNow(),
				p.pollAllShellStatusesNow(),
			}
			return p, tea.Batch(focusCmds...)
		}

	case gitinit.ReadyMsg:
		if msg.Root == "" {
			return p, nil
		}
		if !p.refreshing {
			p.refreshing = true
			cmds = append(cmds, p.refreshWorktrees())
		}

	case plugin.HostInventoryMsg:
		return p.handleHostInventory()

	case RefreshMsg:
		if p.remoteBound() {
			p.applyHostInventory()
			cmds = append(cmds, p.reconcileTerminalModels()...)
			return p, tea.Batch(cmds...)
		}
		if !p.refreshing {
			p.refreshing = true
			cmds = append(cmds, p.refreshWorktrees())
			// td-8d18de: `r` re-runs safe tmux discovery so shells that
			// survived a foreign manifest rewrite come back without
			// restarting the app. syncShellsFromManifest never prunes — it
			// merges — so refresh can only ever add sessions back.
			if cmd := p.syncShellsFromManifest(p.currentShellStartupScope()); cmd != nil {
				cmds = append(cmds, cmd)
			} else if p.shellManifest == nil {
				// Startup never got a manifest (an I/O error, or the isolation
				// guard refusing the path), so there is nothing to sync from.
				// Retry the whole load instead of making `r` a no-op — that is
				// exactly the "restart the app" the ticket wants to avoid.
				if cmd := p.loadShellStartup(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}

	case WorkDirDeletedMsg:
		// Current working directory (a worktree) was deleted - request switch to main repo
		p.refreshing = false
		if msg.MainWorktreePath != "" {
			return p, app.SwitchToMainWorktree(msg.MainWorktreePath)
		}
		return p, nil

	case RefreshDoneMsg:
		// Discard stale messages from previous project
		if plugin.IsStale(p.ctx, msg) || msg.OperationID != p.refreshOperationID || !p.scopeMatches(msg.OperationScope) {
			return p, nil
		}
		p.refreshing = false
		p.lastRefresh = time.Now()
		if msg.Err == nil {
			// Preserve selection by stable identity (not display name) across refresh.
			var selectedKey string
			if p.selectedIdx >= 0 && p.selectedIdx < len(p.worktrees) {
				selectedKey = p.worktrees[p.selectedIdx].IdentityKey()
			}

			p.repoSnapshot = msg.Snapshot
			p.worktrees = msg.Worktrees
			p.conflicts = msg.Conflicts
			p.worktreesLoaded = true
			p.rebuildNestedShellsFromState()
			if cmd := p.backfillWorkDirsCmd(); cmd != nil {
				cmds = append(cmds, cmd)
			}

			// Restore selection by finding the worktree with the same name
			if selectedKey != "" {
				for i, wt := range p.worktrees {
					if wt.IdentityKey() == selectedKey {
						p.selectedIdx = i
						break
					}
				}
			}

			// Shell discovery and worktree refresh race at startup. Restore state
			// only after both have applied so a saved shell cannot be mistaken for
			// a missing item and auto-created over.
			cmds = append(cmds, p.completeInitialWorkspaceLoad()...)

			// Bounds check in case the selected worktree was deleted
			if p.selectedIdx >= len(p.worktrees) && len(p.worktrees) > 0 {
				p.selectedIdx = len(p.worktrees) - 1
			}

			// Preserve agent pointers from existing agents map
			for _, wt := range p.worktrees {
				if agent, ok := p.agents[wt.IdentityKey()]; ok {
					wt.Agent = agent
				}
			}
			// Status and stats already came from the same bounded refresh pass.
			// Load only local metadata here; do not fan out more Git processes.
			for _, wt := range p.worktrees {
				if wt.IsMissing {
					continue // Skip metadata for worktrees with missing directories
				}
				// Load linked task ID from centralized worktree data directory
				wt.TaskID = loadTaskLink(p.ctx.ProjectRoot, wt.Path)
				// Load chosen agent type from centralized worktree data directory
				wt.ChosenAgentType = loadAgentType(p.ctx.ProjectRoot, wt.Path)
				// Load stable PR identity/state from centralized worktree data.
				hydrateWorktreePRMetadata(p.operationCtx, p.ctx.ProjectRoot, wt)
				// Load base branch from centralized worktree data directory
				wt.BaseBranch = loadBaseBranch(p.ctx.ProjectRoot, wt.Path)
			}
			p.reconcilePendingCreation()
			p.applyPendingWorkspaceSelection()
			if cmd := p.TakePendingWorkspaceAction(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			if cmd := p.loadSelectedDiff(); cmd != nil {
				cmds = append(cmds, cmd)
			}

			// Reconcile unbound deterministic sessions after every successful
			// inventory refresh. reconnectAgents only observes existing tmux state;
			// it never creates, restarts, or kills a session.
			startValidation := !p.initialReconnectDone
			ownership := p.currentTerminalOwnership()
			cmds = append(cmds, p.reconnectAgents(msg.OperationScope, startValidation, ownership))
		}

	case ConflictsDetectedMsg:
		if msg.Err == nil {
			p.conflicts = msg.Conflicts
		}

	case StatsLoadedMsg:
		// Discard stale messages from previous project
		if plugin.IsStale(p.ctx, msg) || !p.scopeMatches(msg.OperationScope) {
			return p, nil
		}
		if wt := p.findWorktree(msg.WorktreeKey); wt != nil {
			wt.Stats = msg.Stats
		}

	case StatsErrorMsg:
		if plugin.IsStale(p.ctx, msg) || !p.scopeMatches(msg.OperationScope) {
			return p, nil
		}
		if wt := p.findWorktree(msg.WorktreeKey); wt != nil {
			wt.Stats = nil
			wt.Changes = &WorktreeChanges{State: LoadStateError, Err: fmt.Errorf("%s: %w", msg.Command, msg.Err)}
		}

	case DiffLoadedMsg:
		// Discard stale messages from previous project
		if plugin.IsStale(p.ctx, msg) || !p.scopeMatches(msg.OperationScope) {
			return p, nil
		}
		if p.selectedWorktree() != nil && p.selectedWorktree().IdentityKey() == msg.WorktreeKey {
			if msg.Identity != "" && msg.Identity != p.diff.Target.Identity() && p.diff.Target.Identity() != "" {
				return p, nil
			}
			p.bindDiffView()
			if msg.Snapshot != nil {
				p.commitStatusWorktree = msg.WorktreeKey
			}
			cmds = append(cmds, p.diff.ApplyLoadedSnapshot(msg.Snapshot, p.diff.WorkDir, p.diff.WorkspaceID))
		}

	case DiffErrorMsg:
		if plugin.IsStale(p.ctx, msg) || !p.scopeMatches(msg.OperationScope) {
			return p, nil
		}
		if wt := p.selectedWorktree(); wt != nil && wt.IdentityKey() == msg.WorktreeKey {
			p.diff.ApplySnapshotMsg(workspacediff.SnapshotMsg{
				Epoch: msg.Epoch, WorkspaceID: msg.WorktreeKey, Identity: workspacediff.IdentityWorkingTree,
				Err: fmt.Errorf("%s (base %q): %v", msg.Command, msg.BaseRef, msg.Err),
			}, wt.Path, wt.IdentityKey())
		}

	case workspacediff.SnapshotMsg:
		p.bindDiffView()
		cmds = append(cmds, p.diff.ApplySnapshotMsg(msg, p.diff.WorkDir, p.diff.WorkspaceID))
		if p.contentDeck != nil {
			cmds = append(cmds, p.applyWorkspaceDeckBroadcast(msg))
		} else {
			cmds = append(cmds, p.applyDiffLoadedToLeaves(msg))
		}

	case workspacediff.CommitDetailMsg:
		p.bindDiffView()
		cmds = append(cmds, p.diff.ApplyCommitDetail(msg))
		if p.contentDeck != nil {
			cmds = append(cmds, p.applyWorkspaceDeckBroadcast(msg))
		} else {
			cmds = append(cmds, p.applyCommitDetailToLeaves(msg))
		}

	case workspacediff.RangeMsg:
		if p.contentDeck != nil {
			cmds = append(cmds, p.applyWorkspaceDeckBroadcast(msg))
		} else {
			cmds = append(cmds, p.applyRangeToLeaves(msg))
		}

	case workspacediff.CommitFileDiffMsg:
		p.bindDiffView()
		cmds = append(cmds, p.diff.ApplyCommitFileDiff(msg))
		if p.contentDeck != nil {
			cmds = append(cmds, p.applyWorkspaceDeckBroadcast(msg))
		} else {
			cmds = append(cmds, p.applyCommitFileDiffToLeaves(msg))
		}

	case FullFileDiffLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if wt := p.selectedWorktree(); wt != nil {
			p.bindDiffView()
		}
		// Every live Diff view is a candidate, not just the legacy one: a
		// pane-hosted view has its own cursor and its own workspace id, and
		// reconciling against p.diff alone dropped its load — which is what
		// left a pane-hosted full-file view on "Loading full file..." forever.
		if p.fullFileWanted(msg) {
			p.fullFileDiff = gitstatus.BuildFullFileDiff(msg.OldContent, msg.NewContent, msg.Parsed)
			p.fullFileKey = fullFileKeyForMsg(msg)
		}

	case CommitStatusLoadedMsg:
		// Discard stale messages from previous project
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.Err == nil && p.selectedWorktree() != nil && p.selectedWorktree().IdentityKey() == msg.WorkspaceName {
			p.diff.Commits = msg.Commits
			p.commitStatusWorktree = msg.WorkspaceName
			// Clamp diff tab cursor if total items changed
			totalItems := p.diffTabFileCount() + len(p.diff.Commits)
			if totalItems > 0 && p.diff.Cursor >= totalItems {
				p.diff.Cursor = totalItems - 1
			}
		}

	case CommitDetailLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.Err == nil && msg.Commit != nil && p.selectedWorktree() != nil &&
			p.selectedWorktree().IdentityKey() == msg.WorkspaceName {
			p.bindDiffView()
			cmds = append(cmds, p.diff.ApplyCommitDetail(workspacediff.CommitDetailMsg{
				Epoch: msg.Epoch, WorkspaceID: msg.WorkspaceName, Identity: workspacediff.IdentityWorkingTree,
				Hash: msg.CommitHash, Commit: mapPluginCommit(msg.Commit),
			}))
		}

	case CommitFileDiffLoadedMsg:
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		p.bindDiffView()
		cmds = append(cmds, p.diff.ApplyCommitFileDiff(workspacediff.CommitFileDiffMsg{
			Epoch: msg.Epoch, WorkspaceID: msg.WorkspaceName, Identity: workspacediff.IdentityWorkingTree,
			CommitHash: msg.CommitHash, FilePath: msg.FilePath, Raw: msg.Raw, Err: msg.Err,
		}))

	case CreateDoneMsg:
		if msg.Err != nil {
			p.setCreateError(msg.Err.Error())
			// Stay in ViewModeCreate - don't close modal or clear state
		} else {
			p.persistCreateLastAgent()
			if p.createForm == nil && msg.AgentType != "" {
				_ = state.SetLastCreateAgent(string(msg.AgentType))
			}
			p.viewMode = ViewModeList
			p.worktrees = append(p.worktrees, msg.Worktree)

			// Auto-focus newly created worktree (same pattern as click selection)
			p.selectWorktreeAt(len(p.worktrees) - 1)
			p.resetPreviewScroll()
			p.saveSelectionState()
			p.ensureVisible()

			p.clearCreateModal()

			// Load content for preview pane
			cmds = append(cmds, p.loadSelectedContent())

			// Start agent or attach based on selection
			if msg.AgentType != AgentNone && msg.AgentType != "" {
				cmds = append(cmds, p.StartAgentWithOptions(msg.Worktree, msg.AgentType, msg.SkipPerms))
			} else {
				// "None" selected - attach to worktree directory
				cmds = append(cmds, p.AttachToWorktreeDir(msg.Worktree))
			}
		}

	case CreatePlanResolvedMsg:
		p.createBusyStep = ""
		p.createOperationModal = nil
		p.createOperationWidth = 0
		if msg.Err != nil {
			p.setCreateError(msg.Err.Error())
			p.createPlan = nil
		} else {
			p.createPlan = msg.Plan
			p.createCopyEnv = msg.Plan.CopyEnv
			p.createRunHook = msg.Plan.RunHook
		}

	case CreateWorktreeAddedMsg:
		p.createBusyStep = ""
		p.createOperationModal = nil
		p.createOperationWidth = 0
		if msg.Err != nil && msg.Worktree == nil {
			p.setCreateError(msg.Err.Error())
			p.createPlan = nil
			return p, nil
		}
		if msg.Worktree != nil && msg.Err != nil {
			p.surfaceInterruptedCreation(msg.Plan, msg.Worktree, msg.Err)
			return p, nil
		}
		p.createPlan = msg.Plan
		p.selectCreatedWorktree(msg.Worktree)
		p.createBusyStep = "Persisting identity and running selected setup"
		return p, p.runCreateSetupCmd(msg.Plan, msg.Worktree)

	case CreateSetupDoneMsg:
		p.createBusyStep = ""
		p.createOperationModal = nil
		p.createOperationWidth = 0
		p.createPlan = msg.Plan
		p.createSetupResult = msg.Result
		if msg.Result == nil || msg.Result.Worktree == nil {
			p.setCreateError("setup returned no created worktree")
			return p, nil
		}
		warnings := msg.Result.Warnings()
		if len(warnings) > 0 {
			warningText := warnings[0].Action
			if warnings[0].Err != nil {
				warningText += ": " + warnings[0].Err.Error()
			}
			msg.Result.Worktree.SetupWarning = warningText
			// A created worktree remains selected but no agent is started until
			// the user explicitly chooses a recovery action.
			p.selectCreatedWorktree(msg.Result.Worktree)
			return p, nil
		}
		msg.Result.Worktree.SetupWarning = ""
		if err := p.clearPendingCreation(msg.Plan); err != nil {
			msg.Result.Outcomes = append(msg.Result.Outcomes, CreateSetupOutcome{Kind: CreateOutcomeIdentity, Action: "finalize pending creation journal", Required: true, Err: err})
			p.selectCreatedWorktree(msg.Result.Worktree)
			return p, nil
		}
		cmds = append(cmds, p.finishCreatedWorktree(msg.Plan, msg.Result.Worktree)...)

	case CreateOpenAnywayMsg:
		if p.createPlan != nil && p.createSetupResult != nil && p.createSetupResult.Worktree != nil {
			if err := p.clearPendingCreation(p.createPlan); err != nil {
				p.createSetupResult.Outcomes = append(p.createSetupResult.Outcomes, CreateSetupOutcome{Kind: CreateOutcomeIdentity, Action: "finalize pending creation journal before opening", Required: true, Err: err})
				p.createOperationModal = nil
				p.createOperationWidth = 0
				return p, nil
			}
			cmds = append(cmds, p.finishCreatedWorktree(p.createPlan, p.createSetupResult.Worktree)...)
		}

	case CreateRecoveryDeleteDoneMsg:
		p.createBusyStep = ""
		p.createOperationModal = nil
		p.createOperationWidth = 0
		p.createDeleteResult = &msg.Result
		if msg.Result.WorktreeRemoved {
			if p.createSetupResult != nil && p.createSetupResult.Worktree != nil {
				created := p.createSetupResult.Worktree
				key := created.IdentityKey()
				p.removeWorktreeByIdentity(key)
			}
			if p.createPlan != nil {
				if err := p.clearPendingCreation(p.createPlan); err != nil {
					msg.Result.Err = errors.Join(msg.Result.Err, fmt.Errorf("finalize pending creation journal: %w", err))
				}
			}
			if p.selectedIdx >= len(p.worktrees) {
				p.selectedIdx = len(p.worktrees) - 1
			}
			if msg.Result.Err != nil {
				return p, nil
			}
			p.viewMode = ViewModeList
			p.clearCreateModal()
			return p, nil
		}
		if msg.Result.Err != nil {
			if p.createSetupResult != nil {
				p.createSetupResult.Outcomes = append(p.createSetupResult.Outcomes, CreateSetupOutcome{Kind: CreateOutcomeIdentity, Action: "delete newly created worktree", Required: true, Err: msg.Result.Err})
			}
			return p, nil
		}

	case FetchPRListMsg:
		if !p.scopeMatches(msg.OperationScope) {
			return p, nil
		}
		p.fetchPRLoading = false
		if msg.Err != nil {
			p.fetchPRError = msg.Err.Error()
		} else {
			p.fetchPRItems = msg.PRs
			p.fetchPRCursor = 0
		}
		p.clearFetchPRModal() // Invalidate cache: async content arrived

	case FetchPRDoneMsg:
		if !p.scopeMatches(msg.OperationScope) {
			return p, nil
		}
		p.fetchPRLoading = false
		if msg.Err != nil {
			p.fetchPRError = msg.Err.Error()
		} else if msg.AlreadyLocal && msg.Worktree == nil {
			// Worktree already exists — find and focus it
			found := false
			for i, wt := range p.worktrees {
				if wt.Branch == msg.Branch {
					p.viewMode = ViewModeList
					p.selectWorktreeAt(i)
					p.resetPreviewScroll()
					p.saveSelectionState()
					p.ensureVisible()
					p.clearFetchPRState()
					p.toastMessage = "Already local — switched to workspace"
					p.toastTime = time.Now()
					cmds = append(cmds, p.loadSelectedContent())
					found = true
					break
				}
			}
			if !found {
				p.fetchPRError = "Branch exists locally but worktree not found"
			}
		} else {
			p.viewMode = ViewModeList
			p.worktrees = append(p.worktrees, msg.Worktree)
			// Auto-focus newly fetched worktree
			p.selectWorktreeAt(len(p.worktrees) - 1)
			p.resetPreviewScroll()
			p.saveSelectionState()
			p.ensureVisible()
			p.clearFetchPRState()
			if msg.AlreadyLocal {
				p.toastMessage = "Already local — added to workspaces"
				p.toastTime = time.Now()
			}
			cmds = append(cmds, p.loadSelectedContent())
		}

	case DeleteDoneMsg:
		if msg.Err != nil {
			p.deleteWarnings = []string{fmt.Sprintf("Delete failed: %v", msg.Err)}
			break
		}
		p.removeWorktreeByName(msg.Name)
		if p.selectedIdx >= len(p.worktrees) && p.selectedIdx > 0 {
			p.selectedIdx--
		}
		// Store any warnings for display
		p.deleteWarnings = msg.Warnings
		// Clear preview pane content to ensure old diff doesn't persist
		p.diff.Content = ""
		p.diff.Raw = ""
		// Load diff for newly selected worktree
		cmds = append(cmds, p.loadSelectedDiff())

	case RemoteCheckDoneMsg:
		// Update delete modal with remote branch existence info
		if p.viewMode == ViewModeConfirmDelete && p.deleteConfirmWorktree != nil &&
			p.deleteConfirmWorktree.Name == msg.WorkspaceName {
			p.deleteConfirm.HasRemote = msg.Exists
			p.deleteConfirm.Invalidate()
		}

	case WorktreeDirtyCheckedMsg:
		// Only the confirmation that asked may be updated: the answer is about
		// one worktree, and a late one must not relabel a different target.
		if p.viewMode == ViewModeConfirmDelete && p.deleteConfirmWorktree != nil &&
			p.deleteConfirmWorktree.Path == msg.Path {
			p.deleteConfirm.Dirty = msg.Dirty
			p.deleteConfirm.Invalidate()
		}

	case PushDoneMsg:
		// Handle push result notification
		if msg.Err == nil {
			cmds = append(cmds, p.refreshWorktrees())
		}

	// Agent messages
	case AgentStartedMsg:
		// Discard stale messages from previous project
		if plugin.IsStale(p.ctx, msg) {
			return p, nil
		}
		if msg.Err != nil {
			return p, func() tea.Msg {
				return app.ToastMsg{Message: fmt.Sprintf("Failed to start agent: %v", msg.Err), Duration: 5 * time.Second, IsError: true}
			}
		}
		// Create agent record
		agent := &Agent{
			Type:        msg.AgentType,
			TmuxSession: msg.SessionName,
			TmuxPane:    msg.PaneID, // Store pane ID for interactive mode
			StartedAt:   time.Now(),
			OutputBuf:   tty.NewOutputBuffer(outputBufferCap),
		}

		key := msg.WorktreeKey
		if key == "" {
			key = msg.WorkspaceName
		}
		if wt := p.findWorktree(key); wt != nil {
			wt.Agent = agent
			wt.ChosenAgentType = msg.AgentType
			wt.Status = StatusActive
			wt.IsOrphaned = false
			p.agents[wt.IdentityKey()] = agent
		}
		p.managedSessions[msg.SessionName] = true

		// Resize pane to match preview width immediately
		if cmd := p.resizeSelectedPaneCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Start polling for output
		pollKey := msg.WorktreeKey
		if pollKey == "" {
			pollKey = msg.WorkspaceName
		}
		cmds = append(cmds, p.scheduleAgentPoll(pollKey, pollIntervalInitial))

		// If this is a resume operation, enter interactive mode (td-aa4136)
		if p.pendingResumeWorktree == msg.WorkspaceName {
			p.pendingResumeWorktree = ""
			cmds = append(cmds, p.enterInteractiveMode())
		}

	case pollAgentMsg:
		// Timer leak prevention (td-83dc22): ignore stale poll messages.
		// If the worktree was removed or reset since this timer was scheduled,
		// the generation won't match and we drop the message.
		if p.currentTerminalOwnership() == 0 || !p.pollScheduler.IsCurrent(agentPollKey(msg.WorkspaceName), msg.Generation) {
			return p, nil // Stale timer, ignore
		}
		// Skip polling while user is attached to session
		if p.attachedSession == msg.WorkspaceName {
			return p, nil
		}
		// Always poll for status updates (needed for sidebar indicators),
		// but use longer intervals when output isn't visible
		return p, p.handlePollAgent(msg.WorkspaceName, msg.Generation)

	case AgentOutputMsg:
		if p.currentTerminalOwnership() == 0 || !p.pollScheduler.IsCurrent(agentPollKey(msg.WorkspaceName), msg.Generation) {
			return p, nil
		}
		// Ownership is checked before the async capture mutates UI state.
		if wt := p.findWorktree(msg.WorkspaceName); wt != nil && wt.Agent != nil {
			now := time.Now()
			applyObservedAgentType(wt.Agent, msg.AgentType, now)
			if supportsAgentActivity(wt.Agent.Type) {
				applyAgentActivity(wt.Agent, msg.Activity, msg.CapturedAt, now)
				if p.outputVisibleFor(wt.IdentityKey()) && p.dwellSatisfied(now) {
					wt.Agent.Activity.Acknowledge()
				}
			}
			modelOwns := p.primaryTerminalOwns("agent", wt.IdentityKey())
			// LastOutput is the age column's clock, so it has to move only when
			// something actually happened. Setting it on every capture — which is
			// what this path used to do — pinned every agent worktree to "now"
			// forever, making the column useless exactly where it matters most.
			// The shell path has always read the snapshot's change signal; this
			// one ignored it.
			changed := false
			if wt.Agent.OutputBuf != nil && !modelOwns {
				changed = wt.Agent.OutputBuf.ApplySnapshot(
					tty.CaptureSnapshot(tty.CaptureInput{
						Output:     msg.Output,
						BaseLine:   msg.CaptureBase,
						Absolute:   msg.HasHistory,
						PaneHeight: msg.PaneHeight,
						RowsJoined: msg.RowsJoined,
					}))
				if msg.HasHistory {
					p.recordTerminalHistory("agent", wt.Agent.TmuxSession, msg.HistorySize)
				}
			}
			if !modelOwns {
				p.recordPaneGeometry("agent", wt.Agent.TmuxSession, msg.PaneWidth, msg.PaneHeight)
				// Who owns a wheel notch is a property of the pane, so the one
				// capture that observes the flag is where it is kept.
				if msg.HasCursor {
					p.recordPaneMouseReporting("agent", wt.Agent.TmuxSession, msg.MouseReporting)
				}
			}
			if changed {
				wt.Agent.LastOutput = time.Now()
			}
			wt.Agent.WaitingFor = msg.WaitingFor
			wt.Status = worktreeStatusForActivity(wt.Agent, msg.Status)
			// Track poll time for runaway detection (td-018f25)
			wt.Agent.RecordPollTime()
		}
		cmds = appendActivityAnimationCmd(cmds, p.startActivityAnimation())
		// Update bracketed paste mode and cursor position if in interactive mode (td-79ab6163)
		if !p.primaryTerminalOwns("agent", msg.WorkspaceName) && p.viewMode == ViewModeInteractive && !p.selectingShell() &&
			p.interactiveState != nil && p.interactiveState.Active && !p.terminalPaneIsPanel(p.interactiveState.LeafID) {
			if wt := p.selectedWorktree(); wt != nil && wt.IdentityKey() == msg.WorkspaceName {
				p.updateBracketedPasteMode(msg.Output)
				// Use cursor position captured atomically with output (no separate query needed)
				if msg.HasCursor && p.interactiveState != nil && p.interactiveState.Active {
					p.interactiveState.CursorRow = msg.CursorRow
					p.interactiveState.CursorCol = msg.CursorCol
					p.interactiveState.CursorVisible = msg.CursorVisible
					p.interactiveState.PaneHeight = msg.PaneHeight
					p.interactiveState.PaneWidth = msg.PaneWidth
				}
				if resizeCmd := p.maybeResizeInteractivePane(msg.PaneWidth, msg.PaneHeight); resizeCmd != nil {
					cmds = append(cmds, resizeCmd)
				}
			}
		}
		if !p.primaryTerminalOwns("agent", msg.WorkspaceName) && p.viewMode == ViewModeList && !p.selectingShell() {
			if wt := p.selectedWorktree(); wt != nil && wt.IdentityKey() == msg.WorkspaceName && wt.Agent != nil {
				target := wt.Agent.TmuxPane
				if target == "" {
					target = wt.Agent.TmuxSession
				}
				if resizeCmd := p.maybeResizeVisiblePane(target, msg.PaneWidth, msg.PaneHeight, false); resizeCmd != nil {
					cmds = append(cmds, resizeCmd)
				}
			}
		}
		// Presentation is model-driven; this capture cadence remains solely for
		// semantic agent evidence and must not accelerate after terminal bursts.
		interval := pollIntervalActive
		if p.primaryTerminalOwns("agent", msg.WorkspaceName) {
			interval = p.semanticAgentPollInterval()
		}
		switch msg.Status {
		case StatusWaiting:
			interval = pollIntervalWaiting
		case StatusDone, StatusError:
			interval = pollIntervalDone
		}
		// Check for runaway session and throttle if needed (td-018f25)
		if wt := p.findWorktree(msg.WorkspaceName); wt != nil && wt.Agent != nil {
			if wt.Agent.CheckRunaway() {
				interval = pollIntervalThrottled
			}
		}
		// Three visibility states (same as shells):
		// 1. Visible + focused → fast polling
		// 2. Visible + unfocused → medium polling (2s)
		// 3. Not visible → slow polling (10-20s)
		isVisibleOnScreen := p.outputVisibleForUnfocused(msg.WorkspaceName)
		if !isVisibleOnScreen {
			background := p.backgroundPollInterval()
			if background > interval {
				interval = background
			}
		} else if !p.focused {
			// Visible but plugin not focused - use medium interval
			if interval < pollIntervalVisibleUnfocused {
				interval = pollIntervalVisibleUnfocused
			}
		}
		// Use interactive polling in interactive mode for fast response
		if !p.primaryTerminalOwns("agent", msg.WorkspaceName) && p.viewMode == ViewModeInteractive && !p.selectingShell() &&
			p.interactiveState != nil && p.interactiveState.Active && !p.terminalPaneIsPanel(p.interactiveState.LeafID) {
			if wt := p.selectedWorktree(); wt != nil && wt.IdentityKey() == msg.WorkspaceName {
				cmds = append(cmds, p.pollInteractivePane())
				return p, tea.Batch(cmds...)
			}
		}
		if p.primaryTerminalOwns("agent", msg.WorkspaceName) {
			interval = p.semanticAgentPollInterval()
		}
		cmds = append(cmds, p.scheduleAgentPoll(msg.WorkspaceName, interval))
		return p, tea.Batch(cmds...)

	case AgentPollUnchangedMsg:
		if p.currentTerminalOwnership() == 0 || !p.pollScheduler.IsCurrent(agentPollKey(msg.WorkspaceName), msg.Generation) {
			return p, nil
		}
		// Track unchanged poll for throttle reset (td-018f25)
		if wt := p.findWorktree(msg.WorkspaceName); wt != nil && wt.Agent != nil {
			now := time.Now()
			applyObservedAgentType(wt.Agent, msg.AgentType, now)
			if supportsAgentActivity(wt.Agent.Type) {
				applyAgentActivity(wt.Agent, msg.Activity, msg.CapturedAt, now)
				if p.outputVisibleFor(wt.IdentityKey()) && p.dwellSatisfied(now) {
					wt.Agent.Activity.Acknowledge()
				}
			}
			modelOwns := p.primaryTerminalOwns("agent", wt.IdentityKey())
			if wt.Agent.OutputBuf != nil && !modelOwns {
				wt.Agent.OutputBuf.ApplySnapshot(
					tty.CaptureSnapshot(tty.CaptureInput{
						Output:     msg.Output,
						BaseLine:   msg.CaptureBase,
						Absolute:   msg.HasHistory,
						PaneHeight: msg.PaneHeight,
						RowsJoined: msg.RowsJoined,
					}))
				if msg.HasHistory {
					p.recordTerminalHistory("agent", wt.Agent.TmuxSession, msg.HistorySize)
				}
			}
			if !modelOwns {
				p.recordPaneGeometry("agent", wt.Agent.TmuxSession, msg.PaneWidth, msg.PaneHeight)
				// Who owns a wheel notch is a property of the pane, so the one
				// capture that observes the flag is where it is kept.
				if msg.HasCursor {
					p.recordPaneMouseReporting("agent", wt.Agent.TmuxSession, msg.MouseReporting)
				}
			}
			wt.Agent.RecordUnchangedPoll()
			// Update status from session file re-check (td-2fca7d v8).
			// Session files may change even when tmux output is unchanged
			// (e.g., agent finishes but terminal output stays the same).
			wt.Status = worktreeStatusForActivity(wt.Agent, msg.CurrentStatus)
			wt.Agent.WaitingFor = msg.WaitingFor
		}
		cmds = appendActivityAnimationCmd(cmds, p.startActivityAnimation())
		// Content unchanged - use longer interval based on current status
		interval := pollIntervalIdle
		if p.primaryTerminalOwns("agent", msg.WorkspaceName) {
			interval = p.semanticAgentPollInterval()
		}
		switch msg.CurrentStatus {
		case StatusWaiting:
			interval = pollIntervalWaiting
		case StatusDone, StatusError:
			interval = pollIntervalDone
		}
		// If still throttled, maintain throttle interval (td-018f25)
		if wt := p.findWorktree(msg.WorkspaceName); wt != nil && wt.Agent != nil && wt.Agent.PollsThrottled {
			interval = pollIntervalThrottled
		}
		// Three visibility states (same as AgentOutputMsg)
		isVisibleOnScreen := p.outputVisibleForUnfocused(msg.WorkspaceName)
		if !isVisibleOnScreen {
			background := p.backgroundPollInterval()
			if background > interval {
				interval = background
			}
		} else if !p.focused {
			// Visible but plugin not focused - use medium interval
			if interval < pollIntervalVisibleUnfocused {
				interval = pollIntervalVisibleUnfocused
			}
		}
		// Use interactive polling for the selected worktree (td-8856c9: no stagger)
		if !p.primaryTerminalOwns("agent", msg.WorkspaceName) && p.viewMode == ViewModeInteractive && !p.selectingShell() &&
			p.interactiveState != nil && p.interactiveState.Active && !p.terminalPaneIsPanel(p.interactiveState.LeafID) {
			if wt := p.selectedWorktree(); wt != nil && wt.IdentityKey() == msg.WorkspaceName {
				cmds = append(cmds, p.pollInteractivePane())
				// Use cursor position captured atomically with output
				if msg.HasCursor && p.interactiveState != nil && p.interactiveState.Active {
					p.interactiveState.CursorRow = msg.CursorRow
					p.interactiveState.CursorCol = msg.CursorCol
					p.interactiveState.CursorVisible = msg.CursorVisible
					p.interactiveState.PaneHeight = msg.PaneHeight
					p.interactiveState.PaneWidth = msg.PaneWidth
				}
				if resizeCmd := p.maybeResizeInteractivePane(msg.PaneWidth, msg.PaneHeight); resizeCmd != nil {
					cmds = append(cmds, resizeCmd)
				}
				return p, tea.Batch(cmds...)
			}
		}
		if p.primaryTerminalOwns("agent", msg.WorkspaceName) {
			interval = p.semanticAgentPollInterval()
		}
		cmds = append(cmds, p.scheduleAgentPoll(msg.WorkspaceName, interval))
		return p, tea.Batch(cmds...)

	// Shell session messages
	case ShellCreatedMsg:
		if msg.Err != nil {
			// Creation failed, show error toast
			p.pendingPrefillCmd = "" // Clear pending resume
			return p, func() tea.Msg {
				return app.ToastMsg{Message: msg.Err.Error(), Duration: 5 * time.Second, IsError: true}
			}
		}

		// A create — including recreating an offline row under its old tmux
		// name — starts a new life for that name. Recording it here is what
		// makes any death verdict still in flight refuse to close the shell
		// that was just brought back (td-6a4100).
		p.noteShellAlive(msg.SessionName)

		existingIdx := -1
		for i, s := range p.shells {
			if s.TmuxName == msg.SessionName {
				existingIdx = i
				break
			}
		}
		parentIdx, nested := p.findNestedShell(msg.SessionName)
		// Recreate of a sibling must not become a current-worktree row or
		// rewrite its WorkDir (td-4819be / td-8d18de).
		belongsHere := p.ctx != nil && shellDiscoveryPattern(p.ctx.WorkDir).MatchString(msg.SessionName)

		displayAgentType := AgentShell
		if msg.AgentType != AgentNone && msg.AgentType != "" {
			displayAgentType = msg.AgentType
		}

		switch {
		case nested != nil && !belongsHere:
			nested.IsOrphaned = false
			if msg.AgentType != AgentNone && msg.AgentType != "" {
				nested.ChosenAgent = msg.AgentType
			}
			nested.SkipPerms = msg.SkipPerms
			nested.Agent = &Agent{
				Type:        displayAgentType,
				TmuxSession: msg.SessionName,
				TmuxPane:    msg.PaneID,
				OutputBuf:   tty.NewOutputBuffer(outputBufferCap),
				StartedAt:   time.Now(),
			}
			if existingIdx >= 0 {
				p.shells = append(p.shells[:existingIdx], p.shells[existingIdx+1:]...)
			}
			p.managedSessions[msg.SessionName] = true
			if !msg.KeepSelection {
				p.selectNestedShell(parentIdx, nested.TmuxName)
				p.saveSelectionState()
			}
		case existingIdx >= 0:
			revivedShell := p.shells[existingIdx]
			revivedShell.IsOrphaned = false
			if revivedShell.WorkDir == "" && p.ctx != nil {
				revivedShell.WorkDir = p.ctx.WorkDir
			}
			revivedShell.Agent = &Agent{
				Type:        displayAgentType,
				TmuxSession: msg.SessionName,
				TmuxPane:    msg.PaneID,
				OutputBuf:   tty.NewOutputBuffer(outputBufferCap),
				StartedAt:   time.Now(),
			}
			p.managedSessions[msg.SessionName] = true
			if p.shellManifest != nil {
				_ = p.shellManifest.UpdateShell(shellToDefinition(revivedShell))
			}
			if !msg.KeepSelection {
				p.selectTopShellAt(existingIdx)
				p.saveSelectionState()
			}
		default:
			workDir := ""
			if p.ctx != nil {
				workDir = p.ctx.WorkDir
			}
			shell := &ShellSession{
				Name:     msg.DisplayName,
				TmuxName: msg.SessionName,
				WorkDir:  workDir,
				Agent: &Agent{
					Type:        displayAgentType,
					TmuxSession: msg.SessionName,
					TmuxPane:    msg.PaneID,
					OutputBuf:   tty.NewOutputBuffer(outputBufferCap),
					StartedAt:   time.Now(),
				},
				CreatedAt:   time.Now(),
				ChosenAgent: msg.AgentType,
				SkipPerms:   msg.SkipPerms,
			}
			p.shells = append(p.shells, shell)
			p.managedSessions[msg.SessionName] = true
			if p.shellManifest != nil {
				_ = p.shellManifest.AddShell(shellToDefinition(shell))
			}
			if !msg.KeepSelection {
				p.selectTopShellAt(len(p.shells) - 1)
				p.saveSelectionState()
			}
		}
		if !msg.KeepSelection {
			p.activePane = PaneSidebar
			p.jumpPreviewWindow(0)
		}

		// Resize pane to match preview width immediately
		if cmd := p.resizeSelectedPaneCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Start polling for output using stable TmuxName
		cmds = append(cmds, p.scheduleShellPollByName(msg.SessionName, 500*time.Millisecond))

		// If there's a pending resume command, inject it and enter interactive mode (td-aa4136)
		if p.pendingPrefillCmd != "" {
			resumeCmd := p.pendingPrefillCmd
			p.pendingPrefillCmd = "" // Clear pending command
			cmds = append(cmds, p.sendResumeCommandToShell(msg.SessionName, resumeCmd))
			// Enter interactive mode after command is injected
			cmds = append(cmds, func() tea.Msg { return shellResumeInjectedMsg{TmuxSession: msg.SessionName} })
		} else if msg.AgentType != AgentNone && msg.AgentType != "" {
			// td-2ba8a3: Start agent if one was selected (not AgentNone)
			cmds = append(cmds, p.startAgentInShell(msg.SessionName, msg.AgentType, msg.SkipPerms))
		}

	case ShellDetachedMsg:
		// User detached from shell session - resume polling. In v2 mouse mode is
		// declared on tea.View and re-asserted by the renderer on the next frame.
		// Resize pane back to preview dimensions
		if cmd := p.resizeSelectedPaneCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if shell := p.getSelectedShell(); shell != nil {
			// Check liveness before polling - if user typed exit, session is already dead (td-8e3324)
			if sessionExists(shell.TmuxName) {
				cmds = append(cmds, p.scheduleShellPollByName(shell.TmuxName, 0))
			} else {
				// has-session failing is not proof on its own — a server that
				// is down fails the same way — so confirm before closing.
				cmds = append(cmds, p.suspectShellDeath(shell.TmuxName))
			}
		}

	// td-2ba8a3: Shell agent lifecycle messages
	case ShellAgentStartedMsg:
		// Agent started successfully - update shell state
		for i, shell := range p.shells {
			if shell.TmuxName == msg.TmuxName {
				p.shells[i].ChosenAgent = msg.AgentType
				p.shells[i].SkipPerms = msg.SkipPerms
				if p.shells[i].Agent != nil {
					p.shells[i].Agent.Type = msg.AgentType
				}
				break
			}
		}

	case ShellAgentErrorMsg:
		// Agent failed to start - show error toast, shell still usable
		return p, func() tea.Msg {
			return app.ToastMsg{
				Message:  fmt.Sprintf("Failed to start agent: %v", msg.Err),
				Duration: 5 * time.Second,
				IsError:  true,
			}
		}

	case deferredPaneResizeMsg:
		// The window has closed; assert the geometry the surface holds now against
		// the pane size last observed with the output.
		if p.currentTerminalOwnership() == 0 || p.interactiveState == nil {
			return p, nil
		}
		if msg.Generation != p.resizeGeneration {
			// Leftover tick from before a divider drop — the drop already
			// flushed, so this must not fire a second resize.
			return p, nil
		}
		// Cleared before the liveness guard: the retry has arrived either way,
		// and a flag left set describes one still to come, which is what stops
		// the next burst from arming its own.
		p.interactiveState.ResizeRetryPending = false
		if !p.interactiveState.Active {
			return p, nil
		}
		return p, p.maybeResizeInteractivePane(p.interactiveState.PaneWidth, p.interactiveState.PaneHeight)

	case paneResizedMsg:
		if p.currentTerminalOwnership() == 0 || (msg.Ownership != 0 && !p.ownsTerminalOwnership(msg.Ownership)) {
			return p, nil
		}
		// Pane was resized to match preview dimensions - trigger fresh poll so
		// captured content reflects the new width/wrapping.
		// Skip in interactive mode: it manages its own polling chain.
		if p.viewMode == ViewModeInteractive {
			return p, nil
		}
		if p.selectingShell() {
			if shell := p.getSelectedShell(); shell != nil && shell.Agent != nil {
				return p, p.pollShellSessionByName(shell.TmuxName)
			}
		} else {
			return p, p.pollSelectedAgentNowIfVisible()
		}

	case shellAttachAfterCreateMsg:
		// Attach to shell after it was created
		return p, p.attachToShellByIndex(msg.Index)

	case shellAttachByNameMsg:
		return p, p.attachToShellSession(p.findShellByName(msg.TmuxName))

	case shellResumeInjectedMsg:
		// Resume command was injected into shell - enter interactive mode (td-aa4136)
		p.activePane = PaneSidebar
		return p, p.enterInteractiveMode()

	case shellResumeErrorMsg:
		// Failed to inject resume command - just show toast, shell still usable
		return p, func() tea.Msg {
			return app.ToastMsg{Message: "Failed to inject resume command", Duration: 3 * time.Second, IsError: true}
		}

	case worktreeResumeCreatedMsg:
		// Worktree created for resume - start agent with resume command (td-aa4136)
		if msg.Err != nil {
			return p, func() tea.Msg {
				return app.ToastMsg{Message: msg.Err.Error(), Duration: 5 * time.Second, IsError: true}
			}
		}

		// Add worktree to list and select it
		p.worktrees = append(p.worktrees, msg.Worktree)
		p.selectWorktreeAt(len(p.worktrees) - 1)
		p.resetPreviewScroll()
		p.saveSelectionState()
		p.ensureVisible()

		// Store pending resume state to enter interactive mode after agent starts
		p.pendingResumeWorktree = msg.Worktree.Name

		// Start agent with resume command
		return p, p.startAgentWithResumeCmd(msg.Worktree, msg.AgentType, msg.SkipPerms, msg.ResumeArgv)

	case ShellKilledMsg:
		// Timer leak prevention (td-83dc22): increment generation to invalidate pending timers
		p.pollScheduler.Invalidate(shellPollKey(msg.SessionName))
		// Shell session killed, remove from list
		removedIdx := -1
		for i, shell := range p.shells {
			if shell.TmuxName == msg.SessionName {
				removedIdx = i
				// Clean up Agent resources
				if shell.Agent != nil {
					shell.Agent.OutputBuf = nil
					shell.Agent = nil
				}
				p.shells = append(p.shells[:i], p.shells[i+1:]...)
				delete(p.managedSessions, msg.SessionName)
				// Clean up pane cache and active registry (td-018f25)
				globalPaneCache.remove(msg.SessionName)
				globalActiveRegistry.remove(msg.SessionName)
				// Remove from manifest (td-f88fdd)
				if p.shellManifest != nil {
					_ = p.shellManifest.RemoveShell(msg.SessionName)
				}
				break
			}
		}
		// The killed session may be a shell nested under a sibling worktree,
		// which is not in this list at all.
		if removedIdx < 0 && p.dropNestedShell(msg.SessionName) {
			p.saveSelectionState()
			cmds = append(cmds, p.loadSelectedContent())
		}
		if removedIdx >= 0 {
			p.forgetPaneSurfaces("shell:" + msg.SessionName)
			p.retargetAfterKilledTopShell(removedIdx)
			p.saveSelectionState()
			if len(p.shells) > 0 || len(p.worktrees) > 0 {
				cmds = append(cmds, p.loadSelectedContent())
			}
		}

	case shellDeathSuspectedMsg:
		return p, p.handleShellDeathSuspected(msg)

	case shellDeathProbedMsg:
		return p, p.handleShellDeathProbed(msg)

	case ShellSessionDeadMsg:
		if msg.Generation != 0 && !p.pollScheduler.IsCurrent(shellPollKey(msg.TmuxName), msg.Generation) {
			return p, nil
		}
		p.shellLivenessTracker().Forget(msg.TmuxName)
		// Typing `exit` is the common way a shell dies, so the surface that
		// dies with it is usually the interactive one. Leave it rather than
		// stranding the user on a pane that is gone (td-6a4100).
		if p.viewMode == ViewModeInteractive && p.interactiveState != nil && p.interactiveState.Active &&
			p.interactiveState.TargetSession == msg.TmuxName {
			p.exitInteractiveMode()
		}
		// Timer leak prevention (td-83dc22): increment generation to invalidate pending timers
		p.pollScheduler.Invalidate(shellPollKey(msg.TmuxName))
		// Shell session externally terminated (user typed 'exit' in shell)
		// Remove the dead shell from the list
		removedIdx := -1
		for i, shell := range p.shells {
			if shell.TmuxName == msg.TmuxName {
				removedIdx = i
				// Clean up Agent resources
				if shell.Agent != nil {
					shell.Agent.OutputBuf = nil
					shell.Agent = nil
				}
				p.shells = append(p.shells[:i], p.shells[i+1:]...)
				delete(p.managedSessions, msg.TmuxName)
				// Clean up pane cache and active registry (td-018f25)
				globalPaneCache.remove(msg.TmuxName)
				globalActiveRegistry.remove(msg.TmuxName)
				// Retire the manifest record (td-f88fdd), but only tombstone it
				// when this shell exited inside a tmux server that is still
				// running. If the server is the thing that went away, the record
				// is preserved and marked as a cold-restore candidate instead —
				// N per-shell death verdicts arriving at once is exactly how a
				// server crash used to empty shells.json.
				if p.shellManifest != nil {
					_, _ = p.shellManifest.ReapShell(msg.TmuxName, workspaceops.ServerStateOf(p.observedServer()))
				}
				break
			}
		}
		// A nested shell can die on its own too, and its row is not in p.shells.
		if removedIdx < 0 && p.dropNestedShell(msg.TmuxName) {
			p.saveSelectionState()
			return p, p.loadSelectedContent()
		}
		if removedIdx >= 0 {
			p.forgetPaneSurfaces("shell:" + msg.TmuxName)
			p.retargetAfterKilledTopShell(removedIdx)
			p.saveSelectionState()
			if len(p.shells) > 0 || len(p.worktrees) > 0 {
				return p, p.loadSelectedContent()
			}
		}
		return p, nil

	case shellManifestChangedMsg:
		if !p.shellScopeCurrent(msg.scope) {
			return p, nil
		}
		// Manifest changed by another sidecar instance (td-f88fdd)
		// Reload manifest and sync shells
		cmds = append(cmds, p.syncShellsFromManifest(msg.scope))
		// Continue listening for more changes
		if p.shellWatcherMessages != nil {
			cmds = append(cmds, listenForShellManifestChanges(msg.scope, p.shellWatcherMessages))
		}
		return p, tea.Batch(cmds...)

	case uirequest.RequestMsg:
		if cmd := p.handleUIRequest(msg.Request); cmd != nil {
			return p, cmd
		}
		return p, nil

	case shellManifestSyncMsg:
		if !p.shellScopeCurrent(msg.Scope) {
			return p, nil
		}
		// Apply the reloaded manifest (td-f88fdd)
		if msg.Manifest == nil {
			return p, nil
		}
		// The snapshot was taken before a local write (a delete, a rename) that
		// has since landed. Applying it would resurrect what we just removed,
		// so throw it away and take a fresh one (td-8d18de).
		if p.shellManifest != nil && p.shellManifest.Revision() != msg.BaseRevision {
			return p, p.syncShellsFromManifest(msg.Scope)
		}
		p.shellManifest = msg.Manifest
		cmds = append(cmds, p.applyManifestSync(msg))
		// Reload content if a shell is selected
		if p.selectingShell() {
			cmds = append(cmds, p.loadSelectedContent())
		}
		return p, tea.Batch(cmds...)

	case ShellOutputMsg:
		if p.currentTerminalOwnership() == 0 || !p.pollScheduler.IsCurrent(shellPollKey(msg.TmuxName), msg.Generation) {
			return p, nil
		}
		if msg.Err != nil {
			// Preserve the last good screen and retry under a fresh owner.
			return p, p.scheduleShellPollByName(msg.TmuxName, pollIntervalActive)
		}
		// A capture that answered is the positive liveness evidence a later
		// probe needs before it may close this shell (td-6a4100).
		p.noteShellAlive(msg.TmuxName)
		changed := false
		// Update last output time if content changed
		shell := p.findShellByName(msg.TmuxName)
		if shell != nil && shell.Agent != nil {
			now := time.Now()
			applyObservedAgentType(shell.Agent, msg.AgentType, now)
			if supportsAgentActivity(shell.Agent.Type) {
				applyAgentActivity(shell.Agent, msg.Activity, msg.CapturedAt, now)
				if p.shellOutputVisibleFor(shell.TmuxName) && p.dwellSatisfied(now) {
					shell.Agent.Activity.Acknowledge()
				}
			}
			modelOwns := p.primaryTerminalOwns("shell", shell.TmuxName)
			if shell.Agent.OutputBuf != nil && !modelOwns {
				changed = shell.Agent.OutputBuf.ApplySnapshot(
					tty.CaptureSnapshot(tty.CaptureInput{
						Output:     msg.Output,
						BaseLine:   msg.CaptureBase,
						Absolute:   msg.HasHistory,
						PaneHeight: msg.PaneHeight,
						RowsJoined: msg.RowsJoined,
					}))
				if msg.HasHistory {
					p.recordTerminalHistory("shell", shell.TmuxName, msg.HistorySize)
				}
			}
			if !modelOwns {
				p.recordPaneGeometry("shell", shell.TmuxName, msg.PaneWidth, msg.PaneHeight)
				// Who owns a wheel notch is a property of the pane, so the one
				// capture that observes the flag is where it is kept.
				if msg.HasCursor {
					p.recordPaneMouseReporting("shell", shell.TmuxName, msg.MouseReporting)
				}
			}
			if changed {
				shell.Agent.LastOutput = time.Now()
			}
		}
		cmds = appendActivityAnimationCmd(cmds, p.startActivityAnimation())
		// Update bracketed paste mode and cursor position if in interactive mode (td-79ab6163)
		if !p.primaryTerminalOwns("shell", msg.TmuxName) && p.viewMode == ViewModeInteractive && p.selectingShell() &&
			p.interactiveState != nil && p.interactiveState.Active && !p.terminalPaneIsPanel(p.interactiveState.LeafID) {
			if selectedShell := p.getSelectedShell(); selectedShell != nil && selectedShell.TmuxName == msg.TmuxName {
				p.updateBracketedPasteMode(msg.Output)
				// Use cursor position captured atomically with output (no separate query needed)
				if msg.HasCursor && p.interactiveState != nil && p.interactiveState.Active {
					p.interactiveState.CursorRow = msg.CursorRow
					p.interactiveState.CursorCol = msg.CursorCol
					p.interactiveState.CursorVisible = msg.CursorVisible
					p.interactiveState.PaneHeight = msg.PaneHeight
					p.interactiveState.PaneWidth = msg.PaneWidth
				}
				if resizeCmd := p.maybeResizeInteractivePane(msg.PaneWidth, msg.PaneHeight); resizeCmd != nil {
					cmds = append(cmds, resizeCmd)
				}
			}
		}
		if !p.primaryTerminalOwns("shell", msg.TmuxName) && p.viewMode == ViewModeList && p.selectingShell() {
			if selectedShell := p.getSelectedShell(); selectedShell != nil && selectedShell.TmuxName == msg.TmuxName {
				if resizeCmd := p.maybeResizeVisiblePane(msg.TmuxName, msg.PaneWidth, msg.PaneHeight, false); resizeCmd != nil {
					cmds = append(cmds, resizeCmd)
				}
			}
		}
		// Schedule next poll with adaptive interval.
		// Three visibility states:
		// 1. Visible + focused → fast polling (500ms active, 5s idle)
		// 2. Visible + unfocused → medium polling (2s) - user can see output but clicked elsewhere
		// 3. Not visible → slow polling (10-20s)
		interval := pollIntervalActive
		if !changed {
			interval = pollIntervalIdle
		}
		if p.primaryTerminalOwns("shell", msg.TmuxName) {
			interval = p.semanticAgentPollInterval()
		}
		selectedShell := p.getSelectedShell()
		isSelectedShell := selectedShell != nil && selectedShell.TmuxName == msg.TmuxName
		isVisibleOnScreen := isSelectedShell && p.selectingShell() &&
			(p.viewMode == ViewModeList || p.viewMode == ViewModeInteractive)

		if !isVisibleOnScreen {
			// Not visible - use slow background polling
			background := p.backgroundPollInterval()
			if background > interval {
				interval = background
			}
		} else if !p.focused {
			// Visible but plugin not focused - use medium interval so user sees updates
			if interval < pollIntervalVisibleUnfocused {
				interval = pollIntervalVisibleUnfocused
			}
		}
		// If visible AND focused, keep the fast interval (pollIntervalActive/pollIntervalIdle)
		// Use interactive polling in interactive mode for fast response
		if !p.primaryTerminalOwns("shell", msg.TmuxName) && p.viewMode == ViewModeInteractive && p.selectingShell() &&
			p.interactiveState != nil && p.interactiveState.Active && !p.terminalPaneIsPanel(p.interactiveState.LeafID) {
			if selectedShell != nil && selectedShell.TmuxName == msg.TmuxName {
				cmds = append(cmds, p.pollInteractivePane())
				return p, tea.Batch(cmds...)
			}
		}
		if p.primaryTerminalOwns("shell", msg.TmuxName) {
			interval = p.semanticAgentPollInterval()
		}
		cmds = append(cmds, p.scheduleShellPollByName(msg.TmuxName, interval))
		return p, tea.Batch(cmds...)

	case RenameWorktreeDoneMsg:
		if msg.Err != nil {
			p.renameWorktreeError = msg.Err.Error()
			return p, nil
		}
		for _, wt := range p.worktrees {
			if wt.Path == msg.Path {
				wt.Name = msg.NewName
				break
			}
		}
		p.viewMode = ViewModeList
		p.clearRenameWorktreeModal()
		p.saveSelectionState()

	case RenameShellDoneMsg:
		if msg.Err != nil {
			p.renameShellError = msg.Err.Error()
			return p, nil
		}
		// Find shell and update its display name
		for _, shell := range p.shells {
			if shell.TmuxName == msg.TmuxName {
				shell.Name = msg.NewName
				break
			}
		}
		// Keep the environment cue in step with the manifest so anything
		// started in this shell from now on reads the current name.
		setShellEnv(msg.TmuxName, msg.NewName)
		p.viewMode = ViewModeList
		p.clearRenameShellModal()
		// Persist the selection state
		p.saveSelectionState()

	case pollShellByNameMsg:
		// Timer leak prevention (td-83dc22): ignore stale poll messages.
		// If the shell was removed since this timer was scheduled,
		// the generation won't match and we drop the message.
		if p.currentTerminalOwnership() == 0 || !p.pollScheduler.IsCurrent(shellPollKey(msg.TmuxName), msg.Generation) {
			return p, nil // Stale timer, ignore
		}
		// Poll specific shell session for output by name
		if p.findShellByName(msg.TmuxName) != nil {
			return p, p.captureShellSessionByName(msg.TmuxName, msg.Generation)
		}
		return p, nil

	case pollShellMsg:
		// Legacy: poll selected shell session for output
		if shell := p.getSelectedShell(); shell != nil {
			return p, p.pollShellSessionByName(shell.TmuxName)
		}
		return p, nil

	case AgentStoppedMsg:
		if msg.Generation != 0 && !p.pollScheduler.IsCurrent(agentPollKey(msg.WorkspaceName), msg.Generation) {
			return p, nil
		}
		// Timer leak prevention (td-83dc22): increment generation to invalidate pending timers
		p.pollScheduler.Invalidate(agentPollKey(msg.WorkspaceName))
		if wt := p.findWorktree(msg.WorkspaceName); wt != nil {
			// Capture session name before clearing Agent (path identity, like StartAgent)
			sessionName := worktreeTmuxSession(wt)
			wt.Agent = nil
			wt.Status = StatusPaused
			// Clean up cache, active registry, and session tracking (td-53e8a023, td-018f25)
			globalPaneCache.remove(sessionName)
			globalActiveRegistry.remove(sessionName)
			delete(p.managedSessions, sessionName)
			delete(p.agents, wt.IdentityKey())
		}
		return p, nil

	case restartAgentMsg:
		// Start new agent after stop completed
		if msg.worktree != nil {
			agentType := p.resolveWorktreeAgentType(msg.worktree)
			return p, p.StartAgent(msg.worktree, agentType)
		}
		return p, nil

	case restartAgentWithOptionsMsg:
		// Start new agent after stop completed, with user-selected options
		if msg.worktree != nil {
			return p, p.StartAgentWithOptions(msg.worktree, msg.agentType, msg.skipPerms)
		}
		return p, nil

	case TmuxAttachFinishedMsg:
		// Clear attached state
		p.attachedSession = ""

		// In v2 mouse mode is declared on tea.View and re-asserted by the renderer
		// on the next frame after tea.ExecProcess (tmux attach) returns.

		// Resize pane back to preview dimensions
		if cmd := p.resizeSelectedPaneCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Resume polling and refresh to capture what happened while attached
		if wt := p.findWorktree(msg.WorkspaceName); wt != nil && wt.Agent != nil {
			// Immediate poll to get current state
			cmds = append(cmds, p.scheduleAgentPoll(msg.WorkspaceName, 0))
		}
		cmds = append(cmds, p.refreshWorktrees())

	case ApproveResultMsg:
		if msg.Err == nil {
			// Clear waiting state, force immediate poll
			if wt := p.findWorktree(msg.WorkspaceName); wt != nil && wt.Agent != nil {
				wt.Agent.WaitingFor = ""
				wt.Status = StatusActive
			}
			cmds = append(cmds, p.scheduleAgentPoll(msg.WorkspaceName, 0))
		}

	case RejectResultMsg:
		if msg.Err == nil {
			// Clear waiting state, force immediate poll
			if wt := p.findWorktree(msg.WorkspaceName); wt != nil && wt.Agent != nil {
				wt.Agent.WaitingFor = ""
				wt.Status = StatusActive
			}
			cmds = append(cmds, p.scheduleAgentPoll(msg.WorkspaceName, 0))
		}

	case TaskLinkedMsg:
		if msg.Err == nil {
			if wt := p.findWorktree(msg.WorkspaceName); wt != nil {
				wt.TaskID = msg.TaskID
			}
		}

	case TaskSearchResultsMsg:
		p.taskSearchLoading = false
		if msg.Err == nil {
			selectedID := ""
			if p.taskSearchIdx >= 0 && p.taskSearchIdx < len(p.taskSearchFiltered) {
				selectedID = p.taskSearchFiltered[p.taskSearchIdx].ID
			}
			p.taskSearchAll = msg.Tasks
			p.taskSearchFiltered = filterTasks(p.taskSearchInput.Value(), p.taskSearchAll)
			p.taskSearchIdx = selectedTaskIndex(p.taskSearchFiltered, selectedID)
			if p.taskSearchIdx < 0 {
				p.taskSearchIdx = 0
			}
			p.taskSearchScroll = ensureListSelectionVisible(p.taskSearchIdx, p.taskSearchScroll, taskPickerVisibleRows(p.height, p.viewMode == ViewModeTaskLink), len(p.taskSearchFiltered))
			p.taskLinkModal = nil
		}

	case BranchListMsg:
		if msg.Err == nil {
			p.branchAll = msg.Branches
			current := ""
			if p.ctx != nil && p.ctx.WorkDir != "" {
				if b, err := getCurrentBranch(p.ctx.WorkDir); err == nil && b != "" && b != "HEAD" {
					current = b
				}
			}
			if p.createForm != nil {
				p.createForm.SetBranches(p.branchAll, current)
			}
		}

	case createPickerDataMsg:
		p.applyCreatePickerData(msg)

	case workspacecreate.FilesScannedMsg:
		p.applyCreateFileCandidates(msg)

	case LocalBranchesMsg:
		if p.mergeState != nil && msg.Err == nil {
			// Put resolved base branch first, then others
			target := p.mergeState.TargetBranch
			branches := []string{target}
			for _, b := range msg.Branches {
				if b != target {
					branches = append(branches, b)
				}
			}
			p.mergeState.TargetBranches = branches
			p.mergeState.TargetBranchOption = 0 // Default to resolved base branch
			p.mergeModal = nil                  // Force modal rebuild
		}

	case UncommittedChangesCheckMsg:
		wt := p.findWorktree(msg.WorkspaceName)
		if wt != nil && msg.Changes != nil {
			wt.Changes = msg.Changes
			wt.Stats = msg.Stats
			p.conflicts = detectConflictsFromChanges(p.worktrees)
		}
		if msg.Err != nil {
			// Error checking changes - cancel merge and return to list
			p.viewMode = ViewModeList
		} else if msg.HasChanges {
			// Show commit modal
			if wt != nil {
				p.mergeCommitState = &MergeCommitState{
					Worktree:       wt,
					StagedCount:    msg.StagedCount,
					ModifiedCount:  msg.ModifiedCount,
					UntrackedCount: msg.UntrackedCount,
				}
				p.mergeCommitMessageInput = textinput.New()
				p.mergeCommitMessageInput.Placeholder = "Commit message..."
				p.mergeCommitMessageInput.Focus()
				p.mergeCommitMessageInput.CharLimit = 200
				p.viewMode = ViewModeCommitForMerge
			}
		} else if wt != nil {
			// No uncommitted changes, proceed to merge
			cmds = append(cmds, p.proceedToMergeWorkflow(wt))
		}

	case MergeCommitDoneMsg:
		if p.mergeCommitState != nil && p.mergeCommitState.Worktree.Name == msg.WorkspaceName {
			if msg.Err != nil {
				p.mergeCommitState.Error = msg.Err.Error()
			} else {
				// Commit succeeded, proceed to merge workflow
				wt := p.mergeCommitState.Worktree
				p.mergeCommitState = nil
				p.mergeCommitMessageInput = textinput.Model{}
				p.clearCommitForMergeModal()
				// Nothing to commit is a no-op the git view already shows;
				// the merge simply proceeds (audit row 31).
				cmds = append(cmds, p.proceedToMergeWorkflow(wt))
			}
		}

	case MergeStepCompleteMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName {
			if msg.Err != nil {
				if errors.Is(msg.Err, errReviewedSourceChanged) || (msg.Step == MergeStepPush && strings.Contains(msg.Err.Error(), "HEAD changed")) {
					p.mergeState.Step = MergeStepReviewDiff
					p.mergeState.StepStatus[MergeStepPush] = "pending"
					p.mergeState.StepStatus[MergeStepReviewDiff] = "running"
					p.mergeState.ReviewedOID = ""
					p.clearMergeModal()
					cmds = append(cmds, p.loadMergeDiff(p.mergeState.Worktree), appmsg.Alert(notify.SourceWaiting, notify.SeverityWarning, "Reviewed source changed; review the updated diff before pushing"))
					break
				}
				title := fmt.Sprintf("%s Failed", msg.Step.String())
				p.transitionToMergeError(msg.Step, title, msg.Err)
			} else {
				switch msg.Step {
				case MergeStepReviewDiff:
					// ReviewDiff: User manually advances, so mark done here
					p.mergeState.StepStatus[msg.Step] = "done"
					p.mergeState.DiffSummary = msg.Data
					p.mergeState.ReviewedOID = msg.ReviewedOID
				case MergeStepPush:
					p.mergeState.PushRemote = msg.Data
					p.mergeState.BaseRemote = msg.BaseRemote
					p.mergeState.PR.Repository = msg.PR.Repository
					p.mergeState.PR.HeadRef = msg.PR.HeadRef
					p.mergeState.PR.HeadOwner = msg.PR.HeadOwner
					p.mergeState.PR.HeadRepo = msg.PR.HeadRepo
					p.mergeState.PR.HeadOID = msg.PR.HeadOID
					// Push complete - advanceMergeStep handles status transition
					cmds = append(cmds, p.advanceMergeStep())
				case MergeStepCreatePR:
					p.mergeState.PRURL = msg.Data
					p.mergeState.PR = msg.PR
					p.mergeState.ExistingPR = msg.ExistingPRFound
					// Save PR URL to worktree for indicator in list
					if wt := p.mergeState.Worktree; wt != nil && msg.Data != "" {
						wt.PRURL = msg.Data
						wt.PRState = normalizeWorktreePRState(msg.PR.State, true)
						_ = savePRIdentityContext(p.operationCtx, p.ctx.ProjectRoot, wt.Path, msg.PR)
					}
					switch msg.PR.State {
					case "CLOSED":
						p.mergeState.StepStatus[MergeStepCreatePR] = "done"
						p.mergeState.Step = MergeStepWaitingMerge
						p.mergeState.PRPollKind = PRPollClosed
						p.mergeState.PRWatchStopped = true
						p.clearMergeModal()
					case "MERGED":
						p.mergeState.StepStatus[MergeStepCreatePR] = "done"
						p.mergeState.Step = MergeStepWaitingMerge
						cmds = append(cmds, p.checkPRMerged(p.mergeState.Worktree))
					default:
						cmds = append(cmds, p.advanceMergeStep())
					}
				case MergeStepCleanup:
					// Cleanup done, mark done and remove from worktree list
					p.mergeState.StepStatus[msg.Step] = "done"
					p.removeWorktreeByName(msg.WorkspaceName)
					if p.selectedIdx >= len(p.worktrees) && p.selectedIdx > 0 {
						p.selectedIdx--
					}
					p.mergeState.Step = MergeStepDone
				}
			}
		}

	case PRGenerationDoneMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName {
			// Always stop at an editable form before creating anything on GitHub.
			p.mergeState.PRTitle = msg.Title
			p.mergeState.PRBody = msg.Body
			p.mergeState.PRGenerationActive = false
			p.mergeState.StepStatus[MergeStepGeneratePR] = "done"
			p.mergeState.Step = MergeStepEditPR
			p.mergeState.StepStatus[MergeStepEditPR] = "running"
			p.mergeState.PRTitleInput = textinput.New()
			p.mergeState.PRTitleInput.SetValue(msg.Title)
			p.mergeState.PRBodyInput = textarea.New()
			p.mergeState.PRBodyInput.SetValue(msg.Body)
			p.clearMergeModal()
			if msg.Err != nil {
				cmds = append(cmds,
					// The agent's own failure, not the app's (audit row 33).
					appmsg.Alert(notify.SourceAgent, notify.SeverityWarning, "Agent failed, using commit summary"),
				)
			}
		}

	case prGenerationTickMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName &&
			p.mergeState.Step == MergeStepGeneratePR {
			// Advance animation dots (0 -> 1 -> 2 -> 3 -> 0)
			p.mergeState.PRGenerationDots = (p.mergeState.PRGenerationDots + 1) % 4
			p.clearMergeModal() // Force modal rebuild for animation
			cmds = append(cmds, p.schedulePRGenerationTick(msg.WorkspaceName))
		}

	case CheckPRMergedMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName {
			p.mergeState.PRPollKind = msg.Result.Kind
			if msg.Result.Identity.URL != "" {
				p.mergeState.PR = msg.Result.Identity
				p.mergeState.PRURL = msg.Result.Identity.URL
			}
			if wt := p.mergeState.Worktree; wt != nil {
				wt.PRURL = p.mergeState.PRURL
				wt.PRState = worktreePRStateFromPoll(msg.Result.Kind, msg.Result.Identity, wt.PRURL)
				identity := p.mergeState.PR
				identity.URL = wt.PRURL
				identity.State = strings.ToUpper(wt.PRState)
				p.mergeState.PR = identity
				if identity.URL != "" && p.ctx != nil {
					_ = savePRIdentityContext(p.operationCtx, p.ctx.ProjectRoot, wt.Path, identity)
				}
			}
			switch msg.Result.Kind {
			case PRPollMerged:
				p.mergeState.PRPollAttempt = 0
				p.mergeState.ForceDeleteRequired = msg.Result.ForceDeleteRequired
				p.mergeState.ForceDeleteLocalBranch = false
				p.mergeState.StepStatus[MergeStepWaitingMerge] = "done"
				cmds = append(cmds, p.advanceMergeStep())
			case PRPollClosed:
				p.mergeState.PRWatchStopped = true
				p.clearMergeModal()
			case PRPollOpen:
				p.mergeState.PRPollAttempt = 0
				if !p.mergeState.PRWatchStopped {
					cmds = append(cmds, p.schedulePRCheck(msg.WorkspaceName, nextPRPollDelay(0)))
				}
			default:
				p.mergeState.PRPollAttempt++
				if p.mergeState.PRPollAttempt >= 5 {
					p.mergeState.PRWatchStopped = true
				}
				if !p.mergeState.PRWatchStopped {
					cmds = append(cmds, p.schedulePRCheck(msg.WorkspaceName, nextPRPollDelay(p.mergeState.PRPollAttempt)))
				}
				p.clearMergeModal()
			}
		}

	case checkPRMergeMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName && !p.mergeState.PRWatchStopped {
			cmds = append(cmds, p.checkPRMerged(p.mergeState.Worktree))
		}

	case DirectMergePreflightMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName {
			if msg.Err != nil {
				p.transitionToMergeError(MergeStepDirectMerge, "Direct Merge Preflight Failed", msg.Err)
			} else {
				p.mergeState.DirectOperation = msg.Operation
				p.clearMergeModal()
				cmds = append(cmds, p.executeDirectMerge(msg.OperationScope, msg.WorkspaceName, msg.BaseBranch, msg.Operation))
			}
		}

	case DirectMergeDoneMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName {
			if msg.Operation != nil {
				p.mergeState.DirectOperation = msg.Operation
			}
			if msg.Err != nil {
				p.transitionToMergeError(MergeStepDirectMerge, "Direct Merge Failed", msg.Err)
			} else if msg.Operation != nil && msg.Operation.Aborted {
				p.cancelMergeWorkflow()
				p.clearMergeModal()
				cmds = append(cmds, appmsg.Alert(notify.SourceSession, notify.SeverityWarning, "Merge aborted; target restored"))
			} else {
				// Direct merge succeeded, advance to confirmation
				p.mergeState.Step = MergeStepDirectMerge
				p.mergeState.StepStatus[MergeStepDirectMerge] = "running"
				p.clearMergeModal()
				cmds = append(cmds, p.advanceMergeStep())
			}
		}

	case CleanupDoneMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName {
			if p.mergeState.CleanupResults == nil {
				p.mergeState.CleanupResults = msg.Results
			} else {
				// Merge results from local cleanup
				p.mergeState.CleanupResults.LocalWorktreeDeleted = msg.Results.LocalWorktreeDeleted
				p.mergeState.CleanupResults.LocalBranchDeleted = msg.Results.LocalBranchDeleted
				p.mergeState.CleanupResults.Errors = append(
					p.mergeState.CleanupResults.Errors, msg.Results.Errors...)
				p.mergeState.CleanupResults.RemoteBranchDeleted = msg.Results.RemoteBranchDeleted
				p.mergeState.CleanupResults.PullAttempted = msg.Results.PullAttempted
				p.mergeState.CleanupResults.PullSuccess = msg.Results.PullSuccess
				p.mergeState.CleanupResults.PullError = msg.Results.PullError
				p.mergeState.CleanupResults.PullMessage = msg.Results.PullMessage
			}

			// Remove worktree from list if deleted
			if msg.Results.LocalWorktreeDeleted {
				sessionName := tmuxSessionPrefix + sanitizeName(msg.WorkspaceName)
				if wt := p.findWorktree(msg.WorkspaceName); wt != nil {
					sessionName = worktreeTmuxSession(wt)
				}
				delete(p.managedSessions, sessionName)
				globalPaneCache.remove(sessionName)
				p.removeWorktreeByName(msg.WorkspaceName)
				if p.selectedIdx >= len(p.worktrees) && p.selectedIdx > 0 {
					p.selectedIdx--
				}
			}

			// Check if all cleanup tasks are done
			p.checkCleanupComplete()
		}

	case RemoteBranchDeleteMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName {
			if p.mergeState.CleanupResults == nil {
				p.mergeState.CleanupResults = &CleanupResults{}
			}
			if msg.Err != nil {
				p.mergeState.CleanupResults.Errors = append(
					p.mergeState.CleanupResults.Errors,
					fmt.Sprintf("Remote branch: %v", msg.Err))
			} else {
				p.mergeState.CleanupResults.RemoteBranchDeleted = true
			}
			// Check if all cleanup tasks are done
			p.checkCleanupComplete()
		}

	case PullAfterMergeMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName {
			if p.mergeState.CleanupResults == nil {
				p.mergeState.CleanupResults = &CleanupResults{}
			}
			p.mergeState.CleanupResults.PullAttempted = true
			p.mergeState.CleanupResults.PullSuccess = msg.Success
			p.mergeState.CleanupResults.PullError = msg.Err

			// Parse error for summary and divergence detection
			if msg.Err != nil {
				summary, full, diverged := summarizeGitError(msg.Err)
				p.mergeState.CleanupResults.PullErrorSummary = summary
				p.mergeState.CleanupResults.PullErrorFull = full
				p.mergeState.CleanupResults.BranchDiverged = diverged
				p.mergeState.CleanupResults.BaseBranch = msg.Branch
				p.mergeState.CleanupResults.ShowErrorDetails = false
			}

			// Check if all cleanup tasks are done
			p.checkCleanupComplete()
		}

	case RebaseResolutionMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName {
			if msg.Success {
				// Rebase succeeded - update state
				p.mergeState.CleanupResults.PullSuccess = true
				p.mergeState.CleanupResults.PullError = nil
				p.mergeState.CleanupResults.BranchDiverged = false
				p.mergeState.CleanupResults.PullErrorSummary = ""
				p.mergeState.CleanupResults.PullErrorFull = ""
			} else {
				// Rebase failed - update error state
				p.mergeState.CleanupResults.PullError = msg.Err
				summary, full, diverged := summarizeGitError(msg.Err)
				p.mergeState.CleanupResults.PullErrorSummary = summary
				p.mergeState.CleanupResults.PullErrorFull = full
				p.mergeState.CleanupResults.BranchDiverged = diverged
			}
		}

	case MergeResolutionMsg:
		if p.mergeState != nil && p.mergeState.Worktree.Name == msg.WorkspaceName {
			if msg.Success {
				// Merge succeeded - update state
				p.mergeState.CleanupResults.PullSuccess = true
				p.mergeState.CleanupResults.PullError = nil
				p.mergeState.CleanupResults.BranchDiverged = false
				p.mergeState.CleanupResults.PullErrorSummary = ""
				p.mergeState.CleanupResults.PullErrorFull = ""
			} else {
				// Merge failed - update error state
				p.mergeState.CleanupResults.PullError = msg.Err
				summary, full, diverged := summarizeGitError(msg.Err)
				p.mergeState.CleanupResults.PullErrorSummary = summary
				p.mergeState.CleanupResults.PullErrorFull = full
				p.mergeState.CleanupResults.BranchDiverged = diverged
			}
		}

	case reconnectedAgentsMsg:
		if !p.ownsTerminalOwnership(msg.Ownership) || msg.OperationID != p.refreshOperationID {
			return p, nil
		}
		var pollingCmds []tea.Cmd
		for _, result := range msg.Agents {
			wt := p.findWorktree(result.WorktreeKey)
			if wt == nil || result.Agent == nil || wt.Agent != nil {
				continue
			}
			wt.Agent = result.Agent
			p.agents[wt.IdentityKey()] = result.Agent
			p.managedSessions[result.Agent.TmuxSession] = true
			pollingCmds = append(pollingCmds, p.scheduleAgentPoll(wt.IdentityKey(), 0))
		}
		// After reconnecting to existing sessions, detect orphaned worktrees
		// (worktrees with .sidecar-agent file but no tmux session)
		p.detectOrphanedWorktrees()
		// Start periodic session validation to prevent memory leaks (td-41695b)
		if msg.StartValidation && !p.initialReconnectDone {
			p.initialReconnectDone = true
			pollingCmds = append(pollingCmds, p.scheduleSessionValidation(60*time.Second))
		}
		return p, tea.Batch(pollingCmds...)

	case validateManagedSessionsMsg:
		if p.currentTerminalOwnership() == 0 || msg.Generation != p.sessionValidationGeneration {
			return p, nil
		}
		p.sessionValidationScheduled = false
		// Trigger validation of managedSessions against actual tmux sessions
		return p, p.validateManagedSessions(msg.Generation, p.currentTerminalOwnership())

	case validateManagedSessionsResultMsg:
		if p.currentTerminalOwnership() == 0 || msg.Generation != p.sessionValidationGeneration {
			return p, nil
		}
		// Prune managedSessions entries that no longer exist in tmux (td-41695b)
		for session := range p.managedSessions {
			if !msg.ExistingSessions[session] {
				delete(p.managedSessions, session)
			}
		}
		// Schedule next validation in 60 seconds
		return p, p.scheduleSessionValidation(60 * time.Second)

	case OpenCreateModalWithTaskMsg:
		return p, p.openCreateModalWithTask(msg.TaskID, msg.TaskTitle)

	case ResumeConversationMsg:
		// Handle resume from conversations plugin (td-aa4136)
		return p.handleResumeConversation(msg)

	case app.OpenPrefilledShellMsg:
		// A host — today, a Configuration repair — asked for an ordinary shell
		// with a command typed into it. It is the same injection the resume
		// flow uses: the text lands at the prompt and stays there until the
		// user presses Enter.
		if strings.TrimSpace(msg.Command) == "" {
			return p, nil
		}
		return p, p.createShellWithPrefilledCommand(msg.Command)

	case app.OpenIssuePaneMsg:
		return p, p.openIssuePaneMsg(msg)

	case app.OpenDiffPaneMsg:
		return p, p.openDiffPaneMsg(msg)

	case app.OpenResourcePaneMsg:
		return p, p.openResourcePaneMsg(msg)

	case app.AttachSessionMsg:
		return p, p.attachSessionMsg(msg)

	case cursorPositionMsg:
		// Update cached cursor position for interactive mode rendering (td-648af4)
		if p.interactiveState != nil && p.interactiveState.Active {
			p.interactiveState.CursorRow = msg.Row
			p.interactiveState.CursorCol = msg.Col
			p.interactiveState.CursorVisible = msg.Visible
		}

	case InteractiveSessionDeadMsg:
		// Session ended externally - show notification (td-a1c8456f)
		p.exitInteractiveMode()
		p.toastMessage = "Session ended"
		p.toastTime = time.Now()
		// Auto-remove dead shell from list (td-b6904e), once tmux confirms it
		// really is gone rather than merely unreachable (td-6a4100).
		if p.shellSelected {
			if shell := p.getSelectedShell(); shell != nil {
				cmds = append(cmds, p.suspectShellDeath(shell.TmuxName))
			}
		}

	case InteractivePasteResultMsg:
		if msg.SessionDead {
			p.exitInteractiveMode()
			p.toastMessage = "Session ended"
			p.toastTime = time.Now()
			return p, nil
		}
		if msg.Empty {
			return p, func() tea.Msg {
				// Nothing was pasted and nothing is worth keeping about it.
				return appmsg.FlashMsg{Text: "Clipboard empty"}
			}
		}
		if msg.Err != nil {
			return p, func() tea.Msg {
				return app.ToastMsg{Message: "Paste failed: " + msg.Err.Error(), Duration: 2 * time.Second, IsError: true}
			}
		}
		cmds = append(cmds, p.pollInteractivePaneImmediate())

	case ShellLeafCloseProbeMsg:
		return p, p.handleShellLeafCloseProbe(msg)

	case TermPanelSessionCreatedMsg:
		if p.ctx != nil && p.ctx.Logger != nil {
			p.ctx.Logger.Debug("termPanel: SessionCreatedMsg", "session", msg.SessionName, "pane", msg.PaneID, "err", msg.Err, "current", p.requireShellTermPane().Session)
		}
		if msg.Err != nil {
			p.releaseShellTermPane()
			p.setShellLeafFocused(false)
			if p.pendingTermPanelSeed != nil && p.pendingTermPanelSeed.session == msg.SessionName {
				p.pendingTermPanelSeed = nil
			}
			return p, appmsg.Alert(notify.SourceSession, notify.SeverityError, "Terminal: "+msg.Err.Error())
		}
		if msg.SessionName == p.requireShellTermPane().Session {
			p.requireShellTermPane().PaneID = msg.PaneID
			// Session ready — resize to match split dimensions. The shared
			// terminal model opens during reconciliation after this update.
			return p, tea.Batch(
				p.resizeTermPanelPaneCmd(),
				p.resizeSelectedPaneCmd(),
				p.applyPendingTermPanelSeed(msg.SessionName),
			)
		}

	case TermPanelSeedFailedMsg:
		if msg.Err != nil {
			p.toastMessage = msg.Err.Error()
			p.toastTime = time.Now()
		}

	case tea.KeyPressMsg:
		cmd := p.handleKeyPress(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.PasteMsg:
		// The reposition modal owns the keyboard outright. Bracketed paste is a
		// separate Bubble Tea message, so it must be stopped here as well as keys
		// or it can still reach a previously focused filter behind the overlay.
		if p.paneLayoutModal != nil {
			break
		}
		// A live in-file search bar in a document pane owns the keyboard, and
		// it is a text field: it takes a paste exactly as it takes typed
		// characters, before the sidebar filter or the terminal see it.
		if handled, cmd := p.handleDocFindPaste(msg); handled {
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			break
		}
		// v2: bracketed paste arrives as a dedicated message. A focused list
		// filter is a text input and takes the paste first; otherwise it goes
		// to tmux when in interactive mode.
		if handled, cmd := p.handleFilterPaste(msg.Content); handled {
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			break
		}
		if cmd := p.handleInteractivePaste(msg.Content); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseMsg:
		cmd := p.handleMouse(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	default:
		// Forward unrecognized CSI sequences (e.g. CSI u / kitty keyboard
		// protocol) to tmux when in interactive mode.
		if cmd := p.handleUnknownSequence(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	cmds = appendActivityAnimationCmd(cmds, p.startActivityAnimation())
	return p, tea.Batch(cmds...)
}

// completeInitialWorkspaceLoad performs the one-time state restoration that
// needs both independently loaded shell and worktree collections.
func (p *Plugin) completeInitialWorkspaceLoad() []tea.Cmd {
	if p.stateRestored || p.shellStartupLoading || !p.worktreesLoaded {
		return nil
	}
	p.stateRestored = true

	// The saved order is restored before the selection is resolved, so the
	// clamp below lands on the first row of the list the user will actually
	// see rather than the first row of the default one.
	p.restoreListSort()

	var commands []tea.Cmd
	if len(p.worktrees) > 0 || len(p.shells) > 0 {
		p.restoreSelectionState()
		// A destination the user chose in the global browser outranks the
		// project's persisted selection. Shell discovery and the worktree
		// refresh race, so without this the arm that finished second could
		// restore the saved workspace over the one the user just opened.
		// Re-applying here is a no-op once the selection has been consumed.
		p.applyPendingWorkspaceSelection()
		if cmd := p.TakePendingWorkspaceAction(); cmd != nil {
			commands = append(commands, cmd)
		}
		// selectedIdx starts at zero, which is the main checkout, and the list
		// no longer offers that row. Nothing above guarantees the restored or
		// default selection is one the user can see, so land it on the first
		// visible item before any preview loads. This is a no-op whenever the
		// selection is already visible.
		if cmd := p.clampSelectionToFilter(); cmd != nil {
			commands = append(commands, cmd)
		}
		if cmd := p.takePaneRestoreCmd(); cmd != nil {
			commands = append(commands, cmd)
		}
	}

	// Restore terminal panel only after selection is final, since its session
	// identity depends on the selected shell/worktree.
	if p.shellLeafVisible() && p.requireShellTermPane().Session == "" {
		// A restored leaf reattaches the session it owned; createTermPanelSession
		// recreates it in the workspace's workdir when it is gone.
		sessionName := shellSessionSelector(p.restoredShellSession, p.termPanelSessionName())
		p.restoredShellSession = ""
		if sessionName != "" {
			p.requireShellTermPane().Session = sessionName
			p.requireShellTermPane().Buffer = tty.NewOutputBuffer(outputBufferCap)
			commands = append(commands, p.createTermPanelSession(sessionName))
		}
	}

	// Migration is idempotent and non-destructive. Capture immutable inputs so
	// a project switch cannot redirect this goroutine to the new context.
	projectRoot := p.ctx.ProjectRoot
	worktreePaths := make([]string, 0, len(p.worktrees))
	for _, worktree := range p.worktrees {
		if !worktree.IsMissing {
			worktreePaths = append(worktreePaths, worktree.Path)
		}
	}
	go func() { _ = migration.MigrateProject(projectRoot, worktreePaths) }()

	if command := p.maybeAutoCreateShell(); command != nil {
		commands = append(commands, command)
	}
	return commands
}
