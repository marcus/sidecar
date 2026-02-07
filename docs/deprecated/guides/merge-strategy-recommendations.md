# Merge Strategy Configuration Recommendations

This document provides a comprehensive review of git merge and pull operations in sidecar's git-status and workspace plugins, along with recommendations for making these operations more flexible through user configuration.

## Current State Analysis

### Git Status Plugin (`internal/plugins/gitstatus/`)

The git-status plugin handles pull operations through a modal menu offering four strategies:

| Strategy | Command | Use Case |
|----------|---------|----------|
| Pull (merge) | `git pull` | Creates merge commit, preserves branch topology |
| Pull (rebase) | `git pull --rebase` | Replays local commits on top of upstream |
| Pull (fast-forward only) | `git pull --ff-only` | Only pulls if fast-forward possible (safest) |
| Pull (rebase + autostash) | `git pull --rebase --autostash` | Rebase with automatic stash/unstash of changes |

**Location:** `pull_menu.go:42-47`, `remote.go:21-62`

### Workspace Plugin (`internal/plugins/workspace/`)

The workspace plugin handles merge workflows with two paths:

#### 1. PR Workflow
- Push branch to remote
- Create PR via `gh pr create`
- Wait for PR merge (polling)
- Cleanup: delete worktree, branches, pull base

#### 2. Direct Merge (No PR)
**Location:** `merge.go:430-498`

```go
// Current hardcoded behavior:
1. git fetch origin <baseBranch>
2. git checkout <baseBranch>
3. git pull origin <baseBranch>
4. git merge <branch> --no-ff -m "Merge branch '<branch>'"  // Hardcoded --no-ff
5. git push origin <baseBranch>
```

#### Post-Merge Pull
**Location:** `merge.go:500-551`

```go
// When on base branch: git pull --ff-only origin <branch>  // Hardcoded
// Otherwise: git fetch + git update-ref
```

#### Divergence Resolution
**Location:** `merge.go:644-705`

Offers two options when local/remote diverge:
- Rebase: `git pull --rebase origin <branch>`
- Merge: `git pull origin <branch>`

---

## Identified Issues & Blind Spots

### 1. No Configurable Defaults

**Problem:** Users must select their preferred strategy every time they pull or merge.

**Impact:**
- Slows down workflow for users with consistent preferences
- Power users who always use rebase must click through menu each time
- Teams can't enforce consistent merge strategies

### 2. Limited Merge Strategies

**Problem:** Direct merge is hardcoded to `--no-ff` (no-fast-forward).

**Missing options:**
- `--ff` (fast-forward when possible, merge commit otherwise)
- `--ff-only` (fail if fast-forward not possible)
- `--squash` (squash all commits into one)
- Rebase-based integration (rebase then fast-forward)

### 3. No Squash Merge Support

**Problem:** Many teams prefer squash merges to keep main branch history clean.

**Current behavior:** Only `--no-ff` merge creates merge commits with full history.

### 4. Commit Message Customization

**Problem:** Merge commit messages are hardcoded:
```go
mergeMsg := fmt.Sprintf("Merge branch '%s'", branch)
```

**Missing:**
- Configurable message templates
- Option to include PR title/number
- Option to include commit summary

### 5. Missing Safety Options

**Problem:** Some git safety features are not exposed:

- **GPG signing:** No option for `git commit -S` or `git merge -S`
- **Verified merges:** No `--verify-signatures` option
- **Force push safety:** Direct merge push doesn't use `--force-with-lease`

### 6. Autostash Inconsistency

**Problem:** Autostash is available for pull-rebase but not for:
- Post-merge pull operations
- Divergence resolution operations

### 7. No Upstream Tracking Configuration

**Problem:** Branch tracking is implicitly set with `-u origin <branch>` but users can't configure:
- Default remote name (always `origin`)
- Whether to set upstream tracking by default

### 8. Pull Conflict Recovery

**Problem:** When pull conflicts occur, the only options are:
- Abort (lose progress)
- Dismiss (manual resolution)

**Missing:**
- Continue rebase after fixing conflicts
- Skip problematic commit during rebase
- Conflict resolution guidance

### 9. Rebase Interactive Not Supported

**Problem:** No access to interactive rebase (`git rebase -i`) which is useful for:
- Squashing commits before merge
- Reordering commits
- Editing commit messages

---

## Recommended Configuration Schema

Add to `internal/config/config.go`:

```go
// GitConfig configures git operation defaults.
type GitConfig struct {
    // DefaultPullStrategy sets the default pull strategy.
    // Values: "merge", "rebase", "ff-only", "autostash"
    // Default: "" (prompt user each time)
    DefaultPullStrategy string `json:"defaultPullStrategy,omitempty"`

    // DefaultMergeStrategy sets the default merge strategy for direct merges.
    // Values: "no-ff", "ff", "ff-only", "squash", "rebase"
    // Default: "no-ff"
    DefaultMergeStrategy string `json:"defaultMergeStrategy,omitempty"`

    // MergeCommitTemplate is a Go template for merge commit messages.
    // Available variables: .Branch, .BaseBranch, .PRTitle, .PRNumber
    // Default: "Merge branch '{{.Branch}}'"
    MergeCommitTemplate string `json:"mergeCommitTemplate,omitempty"`

    // SignCommits enables GPG signing for commits.
    // Default: false
    SignCommits bool `json:"signCommits,omitempty"`

    // SignMerges enables GPG signing for merge commits.
    // Default: false
    SignMerges bool `json:"signMerges,omitempty"`

    // AutostashOnPull automatically stashes changes before pull operations.
    // Default: false
    AutostashOnPull bool `json:"autostashOnPull,omitempty"`

    // DefaultRemote is the default remote name.
    // Default: "origin"
    DefaultRemote string `json:"defaultRemote,omitempty"`

    // SetUpstreamOnPush automatically sets upstream tracking on push.
    // Default: true
    SetUpstreamOnPush bool `json:"setUpstreamOnPush,omitempty"`

    // ProtectedBranches lists branches that require extra confirmation for operations.
    // Default: ["main", "master", "develop", "release/*"]
    ProtectedBranches []string `json:"protectedBranches,omitempty"`

    // PreMergeChecks lists commands to run before allowing merge.
    // Example: ["go test ./...", "npm run lint"]
    // Default: []
    PreMergeChecks []string `json:"preMergeChecks,omitempty"`
}
```

Add to `PluginsConfig`:

```go
type PluginsConfig struct {
    Git           GitConfig             `json:"git"`  // New
    GitStatus     GitStatusPluginConfig `json:"git-status"`
    // ... existing fields
}
```

### Example User Configuration

```json
{
  "plugins": {
    "git": {
      "defaultPullStrategy": "rebase",
      "defaultMergeStrategy": "squash",
      "mergeCommitTemplate": "Merge {{.Branch}} into {{.BaseBranch}}",
      "signCommits": true,
      "autostashOnPull": true,
      "protectedBranches": ["main", "production"]
    }
  }
}
```

---

## Implementation Recommendations

### Priority 1: Default Pull Strategy

**Files to modify:**
- `internal/config/config.go` - Add GitConfig struct
- `internal/plugins/gitstatus/pull_menu.go` - Check config before showing menu
- `internal/plugins/gitstatus/update_handlers.go` - Execute default strategy

**Behavior:**
1. If `defaultPullStrategy` is set, execute it directly (skip menu)
2. If unset or empty, show menu as today
3. Add "Always use this" checkbox to menu for quick config

### Priority 2: Default Merge Strategy

**Files to modify:**
- `internal/plugins/workspace/merge.go` - `performDirectMerge()` function

**Implementation:**
```go
func (p *Plugin) performDirectMerge(wt *Worktree) tea.Cmd {
    strategy := p.ctx.Config.Plugins.Git.DefaultMergeStrategy
    if strategy == "" {
        strategy = "no-ff" // backward compatible default
    }

    var mergeArgs []string
    switch strategy {
    case "ff":
        mergeArgs = []string{"merge", branch, "-m", mergeMsg}
    case "ff-only":
        mergeArgs = []string{"merge", branch, "--ff-only"}
    case "squash":
        mergeArgs = []string{"merge", branch, "--squash"}
    case "rebase":
        // Rebase then fast-forward
        // 1. git rebase baseBranch branch
        // 2. git checkout baseBranch
        // 3. git merge branch --ff-only
    default: // "no-ff"
        mergeArgs = []string{"merge", branch, "--no-ff", "-m", mergeMsg}
    }
    // ...
}
```

### Priority 3: Squash Merge Support

**Additional changes:**
- After `git merge --squash`, need to commit separately
- Should prompt for squash commit message
- Can default to concatenated commit messages or PR title

```go
case "squash":
    // Squash merge doesn't auto-commit
    mergeCmd := exec.Command("git", "merge", branch, "--squash")
    // ... execute

    // Now commit
    commitMsg := fmt.Sprintf("Squash merge branch '%s'", branch)
    commitCmd := exec.Command("git", "commit", "-m", commitMsg)
    // ...
```

### Priority 4: Merge Commit Templates

**Implementation:**
```go
import "text/template"

type MergeTemplateData struct {
    Branch     string
    BaseBranch string
    PRTitle    string
    PRNumber   string
}

func renderMergeMessage(tmpl string, data MergeTemplateData) (string, error) {
    if tmpl == "" {
        tmpl = "Merge branch '{{.Branch}}'"
    }
    t, err := template.New("merge").Parse(tmpl)
    if err != nil {
        return "", err
    }
    var buf strings.Builder
    if err := t.Execute(&buf, data); err != nil {
        return "", err
    }
    return buf.String(), nil
}
```

### Priority 5: Protected Branch Warnings

**Implementation:**
```go
func isProtectedBranch(branch string, patterns []string) bool {
    for _, pattern := range patterns {
        if matched, _ := filepath.Match(pattern, branch); matched {
            return true
        }
    }
    return false
}

// Before direct merge:
if isProtectedBranch(baseBranch, config.ProtectedBranches) {
    // Show confirmation modal with extra warning
}
```

---

## Strategy Comparison Guide

For user documentation, include this comparison:

| Strategy | History | Commit Count | Best For |
|----------|---------|--------------|----------|
| No Fast-Forward (`--no-ff`) | Preserves branch topology | +1 merge commit | Feature branches, audit trails |
| Fast-Forward (`--ff`) | Linear when possible | No extra commits | Small changes, single commits |
| Fast-Forward Only (`--ff-only`) | Always linear | Fails if not possible | Strict linear history |
| Squash | Linear | Single commit | Clean history, large features |
| Rebase | Linear, preserves commits | No merge commit | Clean history with detail |

### Team Recommendations

**For open source projects:**
- Default: `squash` (clean history, atomic changes)
- PR workflow recommended

**For internal teams:**
- Default: `no-ff` (preserves context, easier to revert)
- Direct merge acceptable

**For solo developers:**
- Default: `rebase` or `ff` (clean linear history)
- Direct merge preferred for speed

---

## Testing Recommendations

Add test cases for:

1. **Config loading:** Verify default values, validate strategy names
2. **Strategy execution:** Test each merge strategy produces expected git state
3. **Protected branches:** Verify pattern matching works correctly
4. **Template rendering:** Test variable substitution edge cases
5. **Error handling:** Test behavior when merge fails (conflicts, etc.)

Example test structure:
```go
func TestMergeStrategies(t *testing.T) {
    tests := []struct {
        name     string
        strategy string
        wantArgs []string
    }{
        {"no-ff creates merge commit", "no-ff", []string{"merge", "feature", "--no-ff", "-m", "..."}},
        {"squash stages without commit", "squash", []string{"merge", "feature", "--squash"}},
        // ...
    }
    // ...
}
```

---

## Migration Notes

For backward compatibility:

1. **Empty config = current behavior** - All new fields should default to empty/false
2. **Deprecation period** - If changing default behaviors, provide migration warnings
3. **Config validation** - Warn on invalid strategy names, don't fail

---

## Related Files

| File | Purpose |
|------|---------|
| `internal/config/config.go` | Config struct definitions |
| `internal/config/loader.go` | Config loading and defaults |
| `internal/plugins/gitstatus/remote.go` | Pull/fetch operations |
| `internal/plugins/gitstatus/pull_menu.go` | Pull strategy UI |
| `internal/plugins/workspace/merge.go` | Merge workflow |
| `internal/plugins/workspace/diff.go` | Base branch detection |
| `internal/plugins/workspace/worktree.go` | Branch operations |

---

## Summary

This document identifies 9 key areas where sidecar's git integration could be improved through configuration:

1. **Default pull strategy** - Skip menu for users with preferences
2. **Default merge strategy** - Support ff, ff-only, squash, rebase
3. **Squash merge support** - Clean history option
4. **Commit message templates** - Customizable merge messages
5. **GPG signing** - Security for regulated environments
6. **Autostash consistency** - Apply to all pull operations
7. **Upstream tracking** - Configurable remote and tracking
8. **Conflict recovery** - Better options than abort/dismiss
9. **Protected branches** - Safety warnings for important branches

The recommended implementation order prioritizes user-facing workflow improvements (default strategies) before advanced features (signing, templates).
