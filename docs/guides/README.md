# Sidecar Guides and Skills

Documentation and guides for Sidecar are organized into skills and active operational guides:

- **Skills (`.agents/skills/` & `.claude/skills/`)**: Interactive agent skill modules for plugin creation, modals, adapters, multi-agent coordination, drag-pane interactions, and keyboard shortcuts.
- **Active Guides (`docs/guides/active/`)**: Operational, performance, architecture, and feature guides:
  - [`creating-plugins.md`](active/creating-plugins.md) — Putting a tool's data in Sidecar as a protocol plugin, with a runnable example under [`examples/hello-plugin/`](examples/hello-plugin/).
  - [`adding-new-agent-clis.md`](active/adding-new-agent-clis.md) — Step-by-step developer and agent guide to adding new AI agent CLIs.
  - [`pane-layout-automation.md`](active/pane-layout-automation.md) — Multi-pane layout composition and automation via CLI.
  - [`notifications-and-alerting.md`](active/notifications-and-alerting.md) — Toast alerts, sound cues, quiet hours, and notification targets.
  - [`remote-agent-control.md`](active/remote-agent-control.md) — Coordinating and controlling agents on remote hosts over SSH.
  - [`demo-environments.md`](active/demo-environments.md) — Running ephemeral isolated demo environments.
  - [`headless-testing.md`](active/headless-testing.md) — Testing Sidecar headlessly with `tmux-drive.sh`.
  - [`tmux-compatibility.md`](active/tmux-compatibility.md) — Tmux version compatibility contract and verification.
  - [`releasing.md`](active/releasing.md) — Release process and versioning.
  - [`worktree-creation.md`](active/worktree-creation.md) — Git worktree creation workflows.
  - [`embedding-tasks.md`](active/embedding-tasks.md) — Integrating tasks into the workspace.
  - [`getting-started.md`](active/getting-started.md) — Installation and initial setup.
- **Archived Guides (`docs/guides/deprecated/`)**: Preserved historical guides from earlier development phases.

## Quick Skill Tutorial

1. **Find a relevant skill**:
   - Browse the available skill list in `AGENTS.md` / `GEMINI.md`
   - Or list local repo skills with `ls .agents/skills`

2. **Open the skill instructions**:
   - Each skill is documented in `SKILL.md`
   - Example: `cat .agents/skills/coordinate-agents/SKILL.md`

3. **Follow the referenced workflow files**:
   - Skills point to `references/`, `scripts/`, or templates
   - Prefer using those artifacts directly instead of rewriting from scratch
