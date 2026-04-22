# JJ Support: Migration and Test Matrix

This guide covers how to enable JJ mode, what differs from Git mode today, and what to test.

## Enablement

1. Install `jj` and confirm `jj --version` works.
2. Enable the feature flag and VCS preference in `~/.config/sidecar/config.json`:

```json
{
  "features": { "flags": { "jj_plugin": true } },
  "plugins": { "vcs": { "preferred": "auto" } }
}
```

3. Open Sidecar in a JJ workspace (`jj root` succeeds from that directory).

## Migration Notes (Git -> JJ)

- Startup tab selection:
  - `preferred: "auto"`: JJ tab in JJ workspaces, Git tab otherwise.
  - `preferred: "jj"`: prefer JJ tab, but still falls back to Git when JJ detection fails.
  - `preferred: "git"`: always Git tab.
- JJ tab is currently read-focused (status/log/diff and file navigation) and intentionally narrower than full Git plugin workflows.
- Workspaces and file browser integration work through app message routing and focus commands.

## Capability Differences

- JJ tab supports:
  - status/log/diff browsing
  - pane focus switching and paging
  - open selected file in file browser
- Git tab remains the fallback for workflows not yet in JJ mode.

## Test Matrix

| Scenario | Setup | Expected |
| --- | --- | --- |
| Git-only repo, `preferred=auto` | No JJ workspace metadata | Git tab registers, JJ tab not shown |
| JJ workspace, `preferred=auto` | `jj root` succeeds | JJ tab registers, Git tab skipped |
| JJ workspace, `preferred=git` | `jj root` succeeds | Git tab registers, JJ tab skipped |
| JJ preferred, JJ unavailable | `jj` missing or detection fails | Startup falls back to Git tab |
| Feature flag off | `jj_plugin=false` | Git tab registers regardless of JJ workspace |
| Project switch into JJ workspace | switch from git-only project | plugin set reinitializes and JJ appears in auto mode |
| Project switch out of JJ workspace | switch from JJ to git-only | plugin set reinitializes and Git appears |

## Verification Commands

```bash
go test ./...
```

For startup behavior checks, run Sidecar from representative repos with each `plugins.vcs.preferred` value.
