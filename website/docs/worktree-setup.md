---
sidebar_position: 7
title: Worktree Setup Hooks
---

# Worktree Setup Hooks

When sidecar creates a new worktree, it automatically runs a series of setup steps so the workspace is ready to use — no manual `npm install` or config copying required.

## What happens automatically

Every time a worktree is created, sidecar:

1. **Copies env files** from the main worktree into the new one
2. **Creates symlinks** for any directories you've opted in to share (e.g. `node_modules`)
3. **Runs `.worktree-setup.sh`** if it exists at the project root

Setup failures are non-fatal — if a step fails, sidecar logs a warning and continues. The worktree is always created even if setup encounters errors.

## Env file copying

Sidecar copies these files from the main worktree automatically (if they exist):

- `.env`
- `.env.local`
- `.env.development`
- `.env.development.local`

Files are copied as-is, preserving permissions. Missing files are silently skipped. Your API keys, database URLs, and local overrides are available in the new workspace immediately — without committing secrets to git.

## Directory symlinks

By default, no directories are symlinked. To share large directories (like `node_modules`) across worktrees, configure `symlinkDirs` in your project config:

```json
// .sidecar/config.json
{
  "plugins": {
    "workspace": {
      "symlinkDirs": ["node_modules", ".venv"]
    }
  }
}
```

Sidecar replaces any existing directory in the new worktree with a symlink to the main worktree's copy. Only directories that exist in the main worktree are linked — missing ones are skipped.

**When to use this:** Large directories that are identical across branches (e.g. unmodified `node_modules`) save significant disk space and setup time. Avoid symlinking directories that differ between branches.

## The `.worktree-setup.sh` hook

Place a `.worktree-setup.sh` file at your project root (in the main worktree). Sidecar runs it automatically with `bash` whenever a new worktree is created.

The script runs with the **new worktree as the working directory** in a clean, isolated environment.

### Environment variables

| Variable | Value |
|----------|-------|
| `MAIN_WORKTREE` | Absolute path to the main worktree |
| `WORKTREE_BRANCH` | Name of the new branch |
| `WORKTREE_PATH` | Absolute path to the new worktree |

### Creating the hook

```bash
touch .worktree-setup.sh
chmod +x .worktree-setup.sh
```

Add `.worktree-setup.sh` to `.gitignore` if it contains anything machine-specific, or commit it if it should apply to the whole team.

### Examples

**Install dependencies:**

```bash
#!/bin/bash
npm install
```

**Start backing services:**

```bash
#!/bin/bash
docker-compose up -d db redis
```

**Run a makefile target:**

```bash
#!/bin/bash
make setup
```

**Copy config from example:**

```bash
#!/bin/bash
if [ ! -f config.yaml ]; then
  cp config.example.yaml config.yaml
fi
```

**Combine multiple steps:**

```bash
#!/bin/bash
set -e

echo "Setting up worktree: $WORKTREE_BRANCH"
echo "Main worktree: $MAIN_WORKTREE"

# Install dependencies
npm install

# Copy any additional config not covered by env file copying
if [ ! -f .env.test ]; then
  cp "$MAIN_WORKTREE/.env.test" .env.test 2>/dev/null || true
fi

# Start services
docker-compose up -d db

echo "Setup complete."
```

## Error handling

If the setup script exits with a non-zero status, sidecar logs a warning with the script output but **does not block worktree creation**. The worktree is available immediately regardless.

To re-run setup manually:

```bash
cd /path/to/new-worktree
bash /path/to/main-worktree/.worktree-setup.sh
```

## See also

- [Workspaces Plugin](./workspaces-plugin.md) — full worktree and workspace management
