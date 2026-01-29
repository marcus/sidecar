# Native Agent Orchestration in Sidecar

Spec for agent orchestration in sidecar, backed by td as the task engine.

## Problem Statement

Single-agent coding sessions degrade over long contexts. The model accumulates thousands of tokens of history and optimizes for "done" over "correct." There's no independent check on the work, no structured audit trail, and no way to recover if the agent goes off-track.

Current multi-agent orchestrators share several problems:

1. **Opaque execution** - Agent work happens in subprocesses with limited visibility. The user watches terminal output scroll by without structured insight into progress, decisions, or blockers.

2. **Over-prescriptive prompts** - Agent instructions run 500+ lines of rigid rules that constrain model reasoning. Models know how to write code. They don't need to be told "NEVER use git diff" or "Maximum informativeness, minimum verbosity. NO EXCEPTIONS."

3. **Model selection as core complexity** - Multi-tier complexity classifiers mapped to model levels across multiple providers add configuration surface area that most users don't need.

4. **Separate tooling** - Orchestration runs outside the developer's primary workflow, with no integration into git status, file browsing, or task management.

5. **Duplicate state systems** - Orchestrators maintain their own SQLite ledgers for state, duplicating what a task engine like td already provides.

## Design Principles

### Plan-Build-Review Cycle
The core loop: plan the work, implement it, independently validate it. Isolated agents checking each other's work produces better code than single-agent runs where context degradation is real.

### Independent Validation
Validators run in their own sessions and never see the implementer's session context. They have full access to the task (description, acceptance criteria, logs, git history, codebase) but not the implementer's reasoning or conversation history. This is enforced naturally by td's session isolation — each validator gets its own session ID, so they see the task's shared state but not another session's internal dialogue. This prevents the "rubber stamp" problem where reviewers unconsciously accept work because the reasoning sounds plausible.

### Task-Driven Execution
Every run starts from a task with acceptance criteria. The system knows what "done" means before work begins. This maps directly to td's issue model.

### Rejection Loop
When validators reject work, findings route back to the implementer with specific, actionable feedback. The loop continues until consensus or explicit failure. This is fundamentally different from "run once and hope."

### Workspace Isolation
Git worktrees prevent agent work from contaminating the main branch. Sidecar's workspace plugin already manages worktrees — this is a natural integration point.

### Worktree-Agnostic Agents
Agents are started with the worktree as their working directory but are never told they're in a worktree. Agent prompts contain no worktree paths. The agent assumes it's working in whatever directory it was started in. Worktree creation, lifecycle, and cleanup are entirely the orchestrator's and sidecar's responsibility. This keeps agent prompts clean, avoids path-related bugs, and means the same prompts work identically in "direct" mode (no worktree).

### Task-ID-Driven Prompts
The orchestrator passes a task ID and brief td commands to agents. It does **not** inject task content into prompts. Agents read their own context from td. This keeps prompts small, avoids stale context, and trains agents to treat the task engine as their source of truth.

### Minimal Prompt Philosophy
Use minimal role descriptions and let the model's native capabilities drive behavior. The prompt describes *what* the agent needs to accomplish, not *how* to think.

Example agent prompt:
```
You are implementing task td-a1b2.

View the task and its full context:
  td show td-a1b2
  td context td-a1b2

Start a work session and log your progress:
  td usage
  td log "your progress here"

Commit when done.
```

The orchestrator does **not** pass task titles, descriptions, acceptance criteria, or any other task content in the prompt. It passes a task ID and brief td commands. The agent reads the task itself using td. This is a deliberate design choice with several benefits:

1. **Agents learn td** - Every agent interaction reinforces td as the source of truth. Agents that use td commands to read context, log progress, and record decisions produce better audit trails than agents that receive pre-digested context.

2. **No stale context** - If a prior agent updated the task (added logs, comments, or a handoff), the next agent reads the live state, not a snapshot the orchestrator captured earlier.

3. **Task engine is pluggable** - The orchestrator doesn't parse td output or understand td's data model. It just knows "pass the task ID and these commands." Swapping td for Jira, Linear, or a custom backend means changing the command templates, not the orchestration logic.

4. **Smaller prompts** - Task context can be large (descriptions, logs, handoffs, comments, file lists). Letting the agent fetch what it needs keeps the initial prompt minimal. The agent's tool use fetches exactly what's relevant.

The model knows how to write code. Tell it where to find its assignment, not what it says.

### Provider-Agnostic by Default
Start with a single configured provider (the user's preferred CLI agent) and add multi-provider support later if needed. Most users use one provider.

### Transparent Execution
Sidecar's TUI shows agent work in real-time: which files are being modified, what the plan is, validation progress, rejection reasons. Orchestration is not a black box.

### td as the Native Task Engine
td is the state store. Tasks, logs, handoffs, sessions, and reviews all use td's existing infrastructure. No parallel state system.

### Agents Use td Directly
Agents are told a task ID and given td commands to run. They read their own context, log their own progress, and record their own decisions. The orchestrator coordinates the lifecycle (which agent runs when, in what worktree, with what role) but does not mediate the task content.

## Architecture

### Component Overview

```
                    ┌─────────────────────────────────┐
                    │          Sidecar TUI             │
                    │                                  │
                    │  ┌───────┐ ┌──────┐ ┌────────┐  │
                    │  │Git    │ │Files │ │TD      │  │
                    │  │Status │ │      │ │Monitor │  │
                    │  └───────┘ └──────┘ └────────┘  │
                    │  ┌──────────────────────────┐   │
                    │  │   Agent Orchestrator      │   │
                    │  │   Plugin (new)            │   │
                    │  └────────────┬─────────────┘   │
                    └───────────────┼──────────────────┘
                                    │
                    ┌───────────────┼──────────────────┐
                    │               ▼                   │
                    │     Orchestration Engine          │
                    │     (internal/orchestrator/)      │
                    │                                   │
                    │  ┌─────────┐  ┌──────────────┐   │
                    │  │Planner  │  │Implementer   │   │
                    │  │Agent    │  │Agent         │   │
                    │  └────┬────┘  └──────┬───────┘   │
                    │       │              │            │
                    │  ┌────▼──────────────▼───────┐   │
                    │  │   Validator Agents         │   │
                    │  │   (1-N, blind, parallel)   │   │
                    │  └───────────────────────────┘   │
                    │                                   │
                    │  ┌───────────────────────────┐   │
                    │  │   Agent Runner             │   │
                    │  │   (shells out to CLI)      │   │
                    │  └───────────────────────────┘   │
                    └───────────────────────────────────┘
                                    │
                    ┌───────────────┼──────────────────┐
                    │               ▼                   │
                    │      td (task engine)             │
                    │  tasks, logs, handoffs, sessions  │
                    └──────────────────────────────────┘
```

### Core Packages

#### `internal/orchestrator/`

The orchestration engine, independent of the TUI. Can be tested and run standalone.

```
internal/orchestrator/
  engine.go          # Core lifecycle: plan → build → validate → iterate
  agent.go           # Agent abstraction (role, prompt builder, runner)
  runner.go          # Shells out to CLI agents (claude, codex, gemini)
  planner.go         # Planning phase logic
  validator.go       # Validation phase logic (blind, parallel)
  workspace.go       # Worktree/isolation management
  events.go          # Event types emitted during orchestration
  config.go          # Orchestration settings
  taskengine.go      # TaskEngineAdapter interface
  taskengine_td.go   # td implementation of TaskEngineAdapter
```

#### `internal/plugins/orchestrator/`

The sidecar plugin that provides the TUI for orchestration.

```
internal/plugins/orchestrator/
  plugin.go          # Plugin interface implementation
  view.go            # Rendering (plan view, progress, validation results)
  handlers.go        # Key/mouse input handling
  commands.go        # Plugin commands for footer hints
```

### Orchestration Engine Design

#### Engine Lifecycle

```go
type Engine struct {
    taskID     string            // task ID (e.g. "td-a1b2")
    runID      string            // unique run ID (e.g. "sc-a1b2c3")
    workspace  *Workspace        // git worktree or direct
    runner     AgentRunner       // CLI agent executor
    taskEngine TaskEngineAdapter // td (or other task backend)
    events     chan Event        // progress events for TUI
    config     *Config           // orchestration settings
}

type Config struct {
    Provider       string         // "claude", "codex", "gemini"
    MaxIterations  int            // rejection loop limit (default: 3)
    ValidatorCount int            // number of validators (default: 2)
    Workspace      string         // "worktree" (default), "direct", "docker"
    AutoMerge      bool           // auto-merge worktree on success
    AgentTimeout   time.Duration  // kill agent if no output (default: 10m)
}
```

#### Phases

**Core principle: task ID in, td commands in, everything else the agent reads itself.**

The orchestrator never reads task content from td and injects it into prompts. It passes a task ID and short instructions for how the agent should orient itself using the task engine. This keeps prompts small, avoids stale context, and trains agents to treat td as their source of truth.

**Phase transitions are entirely the orchestrator's responsibility.** Agents never log phase events. The orchestrator logs a `starting` event before spawning an agent, a `spawned` event immediately after process start (PID acquired), a `running` event on first output, and a `done` event when the process exits. This is mechanical — no agent cooperation required. If the orchestrator crashes between `starting` and `spawned`, recovery knows the agent was never started and can safely retry. If it crashes between `spawned` and `running`, recovery knows the agent may be running silently and should not re-spawn blindly.

**Phase 1: Plan**

The planner agent receives a minimal prompt:

```
You are planning the implementation for task td-a1b2.

Read the task and its full context:
  td show td-a1b2
  td context td-a1b2

Start a work session and log your plan:
  td usage
  td log --decision "your plan here"

Plan the implementation. Use your judgment on scope and structure.
Your plan will be reviewed before implementation begins.
Log your plan as a decision in td when done.
```

The prompt is identical regardless of whether the task is a fully specified epic, a leaf task with acceptance criteria, or a bare task created from a user's idea. The planner reads the task, assesses what's there, and plans accordingly.

The orchestrator does **not** read the task and inject its content. The planner agent runs `td show` and `td context` itself, reads acceptance criteria, prior logs, handoffs, and comments, then produces a plan.

The planner always runs, regardless of how well-specified the task is. Even for detailed epics with subtasks, the planner reads the codebase and logs its assessment. For bare tasks created from a user prompt, the planner does the heavy lifting: reading code, determining scope, and adding structure. The planner's effort scales naturally with how much planning the task needs — no special instructions required.

The plan is whatever the planner updates in td — either task edits (description/acceptance criteria/subtasks) or explicit decision logs. There is no structured `Plan` type in the orchestrator. The TUI's Plan Review view prioritizes planner decision logs, but falls back to "task updated by planner at <timestamp>" if no decision log exists. The user can accept, modify (by editing the task in td), or reject before implementation begins. If the user rejects the plan, the orchestrator calls `td unstart <id> --reason "plan rejected"` to return the task to `open`, logs a `phase:plan` orchestration event with `status:"rejected"`, and ends the run.

The transition from "planner running" to "plan ready for review" happens when the planner exits **and** the orchestrator detects planner activity, defined as either:
- a log entry authored by the planner session (decision or progress), **or**
- a task update attributed to the planner session (title/description/criteria/subtasks changed).

If neither is found, the run is marked as failed with a clear "planner produced no updates" message and a one-click "retry plan" action.

**Phase 2: Implement**

The implementer agent works in an isolated worktree. The orchestrator starts it with the worktree as its working directory — the agent doesn't need to know it's in a worktree. It receives:

```
You are implementing task td-a1b2.

Read the task and plan:
  td show td-a1b2
  td context td-a1b2

Log progress as you work:
  td usage
  td log "what you did"

Before finishing, record a handoff:
  td handoff td-a1b2 --done "..." --remaining "..."

Commit when done.
```

The implementer reads the task and the planner's logged decisions from td itself. It does not receive: validator instructions, previous rejection details from other tasks, or prescriptive coding rules. The agent works in whatever directory it was started in — worktree management is entirely the orchestrator's responsibility.

Progress events stream to the TUI:
- Files being modified
- Commits made
- Agent thinking/reasoning (if available from provider)

**Phase 3: Validate**

Once the implementer finishes and a handoff exists, the orchestrator submits the task for review (`td review <id>`). This moves the task to `in_review` and enforces td's "different session must review" guard.

N validator agents run in parallel in the same worktree (read-only by convention). Each receives:

```
You are reviewing the implementation of task td-a1b2.

Read the task and its full context:
  td show td-a1b2
  td context td-a1b2

Review the implementation diff against the task's acceptance criteria.

Log your review:
  td usage
  td log "your findings"

If you're the reviewer-of-record, finalize in td:
  td approve td-a1b2 --reason "approved because..."
  # or
  td reject td-a1b2 --reason "rejected because..."
```

Validators run in independent td sessions. They have full access to the task definition, acceptance criteria, logs, git history, and codebase — the more context they have, the better their review. What they naturally don't see is the implementer's session-internal conversation history, since each agent runs in its own CLI session. This is sufficient isolation; no additional filtering is needed.

To keep td's review workflow authoritative while still supporting multiple validators:
- All validators log findings to td.
- Exactly one validator (the reviewer-of-record) is instructed to call `td approve` or `td reject --reason "..."`.
- The orchestrator still aggregates all validator results for display and iteration logic, but td's status transitions reflect the reviewer-of-record's decision.

Each validator independently assesses:
- Does the implementation satisfy acceptance criteria?
- Are there bugs, security issues, or missing edge cases?
- Do tests pass? (if the validator can run them)

Validators return structured results:

```go
type ValidationResult struct {
    Approved bool
    Findings []Finding
}

type Finding struct {
    Severity string  // "error", "warning", "info"
    File     string
    Line     int
    Message  string
}
```

**Phase 4: Iterate or Complete**

If all validators approve: the reviewer-of-record calls `td approve`, the orchestrator marks the run complete, optionally merges the worktree, and updates the task status.

If any validator rejects: the reviewer-of-record calls `td reject` (returns the task to `in_progress`). The orchestrator logs findings to td as comments or blocker logs, then launches a fresh implementer with:

```
You are fixing issues found during review of task td-a1b2.

Read the task and review feedback:
  td show td-a1b2
  td context td-a1b2

The review comments describe what needs to be fixed.
Commit when done.

Log progress:
  td usage
  td log "what you fixed"
```

The implementer reads the rejection findings from td (where they were logged as comments/blockers), not from an inline prompt blob. Loop back to Phase 3.

After `MaxIterations` rejections: stop, report failure, log to td with details.

**Cancellation**

The user can cancel a running orchestration at any time (`c` key in the running view). Cancellation:

1. Sends SIGTERM to the running agent process, then SIGKILL after 5 seconds if it hasn't exited
2. Logs `{"run_id":"...","phase":"cancelled"}` to td
3. Leaves the worktree in place with whatever changes the agent made

The worktree and its contents are managed by sidecar's existing workspace lifecycle — the user can inspect, keep, or discard changes through the workspace plugin. The orchestrator does not clean up worktrees. The task remains in its current td status (typically `in_progress`); the user can re-run orchestration on the same task with no manual cleanup needed.

#### Agent Runner

The runner shells out to CLI agents:

```go
type AgentRunner interface {
    Run(ctx context.Context, prompt string, workDir string, env []string) (*AgentResult, error)
    Stream(ctx context.Context, prompt string, workDir string, env []string) (*AgentStream, error)
}

// AgentStream provides both events and completion status
type AgentStream struct {
    Events <-chan AgentEvent
    Done   <-chan AgentResult // sends final result (exit code, error) on completion
}

// ClaudeRunner implements AgentRunner using claude CLI
type ClaudeRunner struct {
    binary string  // path to claude binary
}

// CodexRunner implements AgentRunner using codex CLI
type CodexRunner struct {
    binary string
}
```

Each runner:
- Spawns the CLI process with a minimal prompt (task ID + td commands)
- Sets `TD_SESSION_ID` env var so the agent gets its own td session (format: `sc-<run_id>-<role>`, e.g. `sc-a1b2c3-impl1`)
- Sets working directory to the worktree (the agent is worktree-agnostic — it just works in its cwd)
- Captures stdout/stderr
- Optionally streams events (for real-time TUI updates)
- Returns structured output or raw text
- Monitors process exit code, wall-clock time, and output liveness

The prompt is intentionally small. The agent's first actions will be running td commands to read its assignment. This means the agent needs tool access to run shell commands (which CLI agents like Claude Code and Codex already have).

Prompts should avoid `td usage --new-session` because the runner sets `TD_SESSION_ID` explicitly. Agents should run `td usage` to view their assigned session without rotating it.

No model level abstraction. The user configures their CLI agent with whatever model they want. The orchestrator doesn't care.

#### Event System

The engine emits events consumed by the TUI plugin:

```go
type Event struct {
    Type      EventType
    Timestamp time.Time
    Data      interface{}
}

type EventType int
const (
    EventPlanStarted EventType = iota
    EventPlanReady
    EventImplementationStarted
    EventFileModified
    EventImplementationDone
    EventValidationStarted
    EventValidatorResult
    EventIterationStarted
    EventComplete
    EventFailed
)
```

#### Heartbeat & Stuck Agent Detection

CLI agents can hang, crash, or loop indefinitely. The orchestrator monitors agent liveness by watching for output activity:

- The agent runner tracks the timestamp of the last stdout/stderr output from the agent process.
- Every `AgentTimeout` (default 10 minutes), the engine checks if the agent has produced any output.
- If no output for `AgentTimeout`, the engine kills the process and logs the timeout to td.

This is simpler than a database heartbeat column. The runner already captures stdout/stderr - it just needs a timer alongside it.

```go
// In the agent runner's Stream() implementation
select {
case event := <-agentOutput:
    lastActivity = time.Now()
    events <- event
case <-time.After(config.AgentTimeout):
    process.Kill()
    taskEngine.LogBlocker(taskID, fmt.Sprintf(
        "%s agent timed out after %v with no output", role, config.AgentTimeout))
    events <- Event{Type: EventFailed, Data: TimeoutError{Role: role}}
case <-ctx.Done():
    process.Kill()
    events <- Event{Type: EventFailed, Data: CancelledError{Role: role}}
}
```

The runner also monitors for:
- **Process exit code**: Non-zero exit means the agent crashed. Log the exit code and stderr tail to td as a blocker.
- **Maximum wall-clock time**: A separate per-phase timeout (default: 30 minutes) kills agents that produce output but never complete. This catches infinite loops with output.
- **Silent completion**: Process exits with code 0 but produced no meaningful output. Log as a warning and treat as failure.

The TUI shows the time since last agent output in the progress view, so users can see if an agent appears stuck before the timeout fires.

#### Run State & Crash Recovery

Orchestration runs need to survive sidecar restarts. Instead of maintaining a separate state file, the orchestrator stores run state in td itself using structured logs. td is already the single source of truth for task lifecycle - run state is part of that lifecycle.

**How it works:**

The orchestrator logs phase transitions to td as JSON-structured orchestration log entries. Each entry includes a run ID (`sc-<6hex>`) to distinguish multiple orchestration runs on the same task:

```go
type OrchestrationEvent struct {
    RunID      string `json:"run_id"`               // e.g. "sc-a1b2c3"
    Phase      string `json:"phase"`                // plan, implement, validate, iterate, complete, failed, cancelled
    Status     string `json:"status,omitempty"`      // starting, spawned, running, done
    Provider   string `json:"provider,omitempty"`
    Iteration  int    `json:"iteration,omitempty"`
    Validator  int    `json:"validator,omitempty"`
    Approved   *bool  `json:"approved,omitempty"`
    Validators int    `json:"validators,omitempty"`
    MaxIter    int    `json:"max_iter,omitempty"`
    Error      string `json:"error,omitempty"`
    ExitCode   *int   `json:"exit_code,omitempty"`
}
```

Example log sequence:

```
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"plan","status":"starting","provider":"claude","validators":2,"max_iter":3}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"plan","status":"spawned"}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"plan","status":"running"}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"plan","status":"done"}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"implement","status":"starting","iteration":1}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"implement","status":"spawned","iteration":1}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"implement","status":"running","iteration":1}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"implement","status":"done","iteration":1}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"validate","status":"starting","iteration":1}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"validate","iteration":1,"validator":1,"approved":true}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"validate","iteration":1,"validator":2,"approved":false}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"iterate","iteration":2}'
td log --type orchestration '{"run_id":"sc-a1b2c3","phase":"complete"}'
```

The run ID is generated once at launch and included in every orchestration event. Recovery filters to the latest incomplete run ID. td may want a `--json-pretty` display option for `td context` to render these readably.

The `spawned` status is the minimal guardrail for recovery: if the last log is `starting` with no `spawned`, the agent never started and can be safely re-spawned. If `spawned` exists but no `running`, treat the agent as possibly still running (silent/buffered output) and ask the user before re-spawning.

On sidecar startup, the orchestrator plugin checks td for any task that is `in_progress` and has orchestration logs but no `phase:complete`, `phase:failed`, or `phase:cancelled` entry for the latest run ID. This means the run was interrupted.

**Recovery logic:**

```go
type RecoveryAction int
const (
    AskUser    RecoveryAction = iota // Prompt user: resume, restart, or abandon
    AutoResume                        // Safe to resume automatically
)

func (e *Engine) RecoverIfNeeded(taskID string) (*RecoveryState, error) {
    // Read orchestration logs from td, filtered to latest incomplete run
    logs := e.taskEngine.GetOrchestrationLogs(taskID)
    if logs == nil {
        return nil, nil // no active run
    }

    runID := logs.LatestIncompleteRunID()
    if runID == "" {
        return nil, nil // all runs completed
    }

    runLogs := logs.ForRun(runID)
    lastPhase := runLogs.LastPhase()
    lastStatus := runLogs.LastStatus()

    switch lastPhase {
    case "plan":
        return &RecoveryState{RunID: runID, Phase: "plan", Action: AskUser}, nil

    case "implement", "iterate":
        return &RecoveryState{
            RunID:     runID,
            Phase:     lastPhase,
            Iteration: runLogs.LastIteration(),
            Action:    AskUser,
        }, nil

    case "validate":
        completed := runLogs.CompletedValidators()
        if len(completed) == runLogs.ValidatorCount() {
            // All validators finished but orchestrator died before processing
            return &RecoveryState{RunID: runID, Phase: "validate", Action: AutoResume}, nil
        }
        return &RecoveryState{
            RunID:     runID,
            Phase:     "validate",
            Remaining: runLogs.ValidatorCount() - len(completed),
            Action:    AutoResume,
        }, nil
    }

    // Check for phases that logged "starting" but never "running"
    // (orchestrator crashed between logging intent and spawning agent)
    if lastStatus == "starting" {
        // Safe to retry — the agent was never spawned
        return &RecoveryState{RunID: runID, Phase: lastPhase, Action: AutoResume}, nil
    }

    return nil, nil
}
```

**Why td instead of a local state file:**

1. **Single source of truth.** The task's td logs already capture everything that happened. Adding orchestration phase logs to the same stream means there's one place to look, not two.

2. **Survives worktree deletion.** If the user deletes a worktree or cleans up `.sidecar-*` files, the run state is still in td.

3. **Portable.** If someone resumes a task on a different machine (or a different sidecar instance), the run state travels with the task. The user does need to remember which task to restart, but `td list --status in_progress` shows them.

4. **Auditable.** The orchestration log entries are visible in `td context`, so humans and future agents can see exactly what phases ran and what happened.

**What td needs for this:**

A new log type: `orchestration`. td already supports typed logs (`progress`, `blocker`, `decision`, `hypothesis`, `tried`, `result`, `security`). Adding `orchestration` is a one-line change to the enum. The TaskEngineAdapter encapsulates this:

```go
type TaskEngineAdapter interface {
    // ... existing methods ...

    // Run state (stored as structured logs in the task engine)
    LogOrchestrationEvent(taskID string, event string) error
    GetOrchestrationLogs(taskID string) ([]OrchestrationLog, error)
}
```

For non-td backends (Jira, Linear), orchestration logs could map to:
- Jira: Custom field or structured comment with a `[orchestration]` prefix
- Linear: Comment with metadata
- GitHub Issues: Comment with a parseable format

The adapter parses whatever format the backend uses. The orchestrator only sees typed `OrchestrationLog` structs.

**The one thing a local file does better:** Reading run state from td requires a subprocess call (`td` CLI), which takes ~50ms. A file read takes microseconds. On sidecar startup, checking "are there any interrupted runs?" is slower via td. This is acceptable - it happens once at startup, and `td list --status in_progress` is fast enough. If it becomes a problem, we can add a lightweight cache file that's purely a performance optimization, not a source of truth.

### TUI Plugin Design

The orchestrator plugin integrates with sidecar's existing plugin system. All UI implementation must follow `docs/guides/ui-feature-guide.md`:

- **Modals**: Built with `internal/modal`, rendered with `ui.OverlayModal`, no manual hit region math. Use `ensureModal()` pattern in both View and Update handlers. Each modal view mode gets a dedicated `FocusContext()` for correct footer hints.
- **Keyboard shortcuts**: Commands + FocusContext + bindings must match. Names are short (one word preferred). Priorities set per the guide. See `docs/guides/keyboard-shortcuts-reference.md` for established patterns.
- **Mouse support**: Rebuild hit regions on each render. General regions first, specific last.
- **Rendering**: Constrain output to allocated height. Use `contentHeight := height - headerLines - footerLines`. Never render plugin-level footers — the app renders the unified footer from `Commands()`.

#### View Modes

1. **Task Selection** - Pick a td task to work on, or press `n` to start from an idea
2. **Plan Review** - See the planner's decision logs from td, accept/modify/reject
3. **Implementation Progress** - Watch files being modified, see agent output
4. **Validation Results** - See each validator's findings, approval/rejection
5. **Iteration View** - Show rejection feedback being sent back to implementer
6. **Complete/Failed** - Final status with summary

#### Worktree List Integration

Orchestration worktrees appear in the workspace plugin's worktree list with a live status badge:

- `⚡ Planning` — planner agent running
- `⚡ Implementing (1/3)` — implementer running, showing iteration/max
- `⚡ Validating` — validators running
- `✓ Complete` — all validators approved
- `✗ Failed` — max iterations exhausted
- `⏸ Interrupted` — sidecar exited mid-run

The badge updates in real-time as the orchestration engine emits events.

#### Run Detail Modal

> Implementation note: follows `docs/guides/ui-feature-guide.md` — built with `internal/modal`, uses `ui.OverlayModal` for background dim, `ensureModal()` in both View/Update, dedicated `FocusContext()` for footer hints. The modal uses `modal.Custom` sections for the live-updating timeline.

Accessible from the worktree list (press `Enter` on an orchestration worktree) or from the orchestrator plugin. This modal is **live-updating** — it polls orchestration state from td at a reasonable interval (1-2 seconds) so users watching the modal see progress in real time without needing to dismiss and reopen.

```
┌─ Run sc-a1b2c3 ───────────────────────────────────────┐
│                                                         │
│  td-a1b2: Add rate limiting to API endpoints            │
│  Claude Code · Iteration 2 of 3 · ⚡ Implementing       │
│                                                         │
│  Timeline                                               │
│  14:02  ✓ Plan accepted                                 │
│  14:03  ✓ Implementation done (iteration 1)             │
│  14:16  ✓ Validation: 1 approved, 1 rejected            │
│         ├ Validator 1: approved                          │
│         └ Validator 2: rejected — 2 findings             │
│           • error: missing rate limit on /api/upload     │
│           • warning: no test for burst handling          │
│  14:19  ⚡ Implementation started (iteration 2)          │
│         └ Last output: 3s ago                            │
│                                                         │
│  [ Cancel Run ]                                          │
│                                                         │
│  c to cancel · Esc to dismiss · d to view diff           │
└─────────────────────────────────────────────────────────┘
```

When the modal detects an interrupted run (on sidecar startup or when the user opens it), it shows recovery actions:

```
│  14:19  ⏸ Interrupted during implementation (iteration 2) │
│         └ sidecar exited 2h ago                            │
│                                                            │
│  [ Resume ]    [ Restart ]    [ Abandon ]                  │
│                                                            │
│  Enter to resume · r to restart · Esc to dismiss           │
```

- **Resume**: Re-launches the agent for the interrupted phase. The agent reads td context to pick up where it left off.
- **Restart**: Creates a new run ID, starts from planning phase.
- **Abandon**: Logs `phase:cancelled`, leaves worktree for user to manage via workspace plugin.

#### Cross-Plugin Integration

The orchestrator plugin leverages sidecar's existing plugins:

- **Git Status**: Shows real-time diff as agent modifies files in the worktree
- **File Browser**: Navigate to files the agent is changing
- **TD Monitor**: Task status updates automatically as orchestration progresses
- **Workspace**: Worktree creation/management for isolated agent work

Messages between plugins:

```go
// Orchestrator → Git Status
gitstatus.RefreshMsg{}

// Orchestrator → File Browser
filebrowser.NavigateToFileMsg{Path: "src/auth/oauth.go"}

// Orchestrator → TD Monitor (via td CLI)
// td log "Plan accepted: implement OAuth with JWT"
// td start td-123

// Orchestrator → Workspace
workspace.CreateWorktreeMsg{Branch: "agent/td-123-oauth"}
```

#### Launch Modal

> Implementation note: follows `docs/guides/ui-feature-guide.md` — modal library, `ensureModal()` pattern, dedicated `FocusContext()` per view mode, no plugin-level footer rendering.

The primary entry point for orchestration. Designed for one-keypress launch on the happy path while exposing configuration for users who want it.

**Design philosophy**: The workspace create modal is a multi-step form wizard with 6+ focus steps because worktree creation has many independent parameters (name, branch, prompt, task, agent, permissions). Orchestration is different — most configuration has sensible defaults. The modal should feel more like a confirmation dialog than a form. It supports two entry modes: selecting an existing task, or typing an idea.

**Trigger**: Press `Enter` on a task in the task list, or `r` (run) from anywhere in the orchestrator plugin. Press `n` (new) to open the modal in idea-first mode with focus on the text input. Can also be invoked cross-plugin from TD Monitor (e.g., "Run orchestration" action on a task).

**Two entry modes**:

1. **From existing task** — User selects a task from the list and presses Enter. The modal shows the task header and skips the idea input.

2. **From idea** — User presses `n` or opens the modal without a task selected. The modal shows a text input where the user types their idea. On submit, the orchestrator creates a minimal td task from the input before launching:

```go
// Auto-create task from user idea
taskID, err := taskEngine.CreateTask(ideaText, CreateOpts{
    Label: "source:prompt",
})
```

The created task has the user's text as the title, no description or acceptance criteria (the planner adds those), and a `source:prompt` label so it's identifiable. From this point forward, the orchestrator has a task ID and the flow is identical.

**Layout** (from existing task):

```
┌─ Run Task ────────────────────────────────────────────┐
│                                                        │
│  td-a1b2: Add rate limiting to API endpoints           │
│  P1 · feature · 5pts                                   │
│                                                        │
│  Provider                                              │
│  ▸ Claude Code                                         │
│    Codex                                               │
│    Gemini                                              │
│    OpenCode                                            │
│                                                        │
│  ─────────────────────────────────────────────────     │
│  Iterations: 3    Validators: 2    Workspace: worktree │
│  ─────────────────────────────────────────────────     │
│                                                        │
│             [ Run ]          [ Cancel ]                 │
│                                                        │
│  Enter to run · Tab for options · Esc to cancel        │
└────────────────────────────────────────────────────────┘
```

**Layout** (from idea):

```
┌─ Run Task ────────────────────────────────────────────┐
│                                                        │
│  What do you want to build?                            │
│  ┌──────────────────────────────────────────────────┐  │
│  │ Allow users to change the theme                  │  │
│  └──────────────────────────────────────────────────┘  │
│  A task will be created automatically · source:prompt  │
│                                                        │
│  Provider                                              │
│  ▸ Claude Code                                         │
│    Codex                                               │
│    Gemini                                              │
│    OpenCode                                            │
│                                                        │
│  ─────────────────────────────────────────────────     │
│  Iterations: 2    Validators: 1    Workspace: worktree │
│  ─────────────────────────────────────────────────     │
│                                                        │
│             [ Run ]          [ Cancel ]                 │
│                                                        │
│  Enter to run · Tab for options · Esc to cancel        │
└────────────────────────────────────────────────────────┘
```

**Focus steps** (existing task — 4 total, idea mode — 5 total):

| Step | ID | Component | Notes |
|------|----|-----------|-------|
| 0* | `idea-input` | `modal.Input` | Text input for idea (only in idea mode) |
| 0/1 | `provider-list` | `modal.List` (single-focus) | Provider selection, j/k to navigate |
| 1/2 | `options-row` | Custom section | Iterations / validators / workspace (inline editable) |
| 2/3 | `run-btn` | Button | Primary action |
| 3/4 | `cancel-btn` | Button | Cancel |

**Fast path**: When opened from an existing task, modal opens with focus on the provider list. If the user's last-used provider is pre-selected (persisted in state.json), pressing `Enter` immediately hits the Run button. Two keypresses total from task list to running orchestration.

**Quick-idea path**: `n` opens the modal in idea mode with focus on the text input. Type the idea, press `Enter` to advance to provider, press `Enter` again to run. Three keypresses from anywhere to running orchestration on a new idea.

**Task header** (non-interactive, existing task mode only): The top of the modal shows the task summary, read from td at modal open time. This is the one place the orchestrator reads task content — purely for display in the modal, never passed to agent prompts. Shows:
- Task ID and title
- Priority, type, and points (single line, muted style)
- If task has `source:prompt` label, shows "from prompt" in muted text

**Provider list**: Same agent types as the workspace plugin (`AgentTypeOrder`), minus `AgentNone`. Reuses the existing `AgentType` constants and display names. Pre-selects the last provider used (from state.json) or the first available.

```go
// Reuse from workspace plugin
AgentTypeOrder = []AgentType{
    AgentClaude,
    AgentCodex,
    AgentGemini,
    AgentCursor,
    AgentOpenCode,
}
```

Detection: Gray out unavailable providers (binary not found) but still show them. Workspace plugin already has `detectAvailableAgents()` - reuse this.

**Options row** (collapsed by default): Shows current values inline as a read-only summary. When focused (via Tab), each value becomes editable:

- **Iterations**: Number input, default 3, range 1-10. Controls `MaxIterations`.
- **Validators**: Number input, default 2, range 0-5. Zero means no validation (single-agent mode, like TRIVIAL complexity).
- **Workspace**: Cycle through "worktree" / "direct" / "docker". Default "worktree".

**Progressive defaults**: The modal pre-populates sensible defaults based on task metadata:

| Task Signal | Default Validators | Default Iterations |
|-------------|-------------------|-------------------|
| Type `chore` or ≤3 points | 0 | 1 |
| Has acceptance criteria | 2 | 3 |
| Type `bug` | 1 | 2 |
| Otherwise | 1 | 2 |

Users can always override. The defaults reduce friction for simple tasks (no validation overhead for a typo fix) while providing full orchestration for complex tasks that have explicit acceptance criteria.

Most users never touch these. The collapsed display keeps the modal compact while making configuration accessible.

**Keyboard shortcuts**:

| Key | Action |
|-----|--------|
| `Enter` | Run (from any focus) or confirm selection |
| `Esc` | Cancel |
| `Tab` / `Shift+Tab` | Cycle focus |
| `j` / `k` or `↑` / `↓` | Navigate provider list (focus 0) |
| `←` / `→` | Adjust numeric values in options row (focus 1) |

**Quick-run shortcut**: From the task list view, `Shift+Enter` skips the modal entirely and launches with the last-used provider and default settings. True one-keypress launch for repeat users.

**State on submit**:

```go
type LaunchConfig struct {
    TaskID         string    // set from existing task or auto-created from idea
    IdeaText       string    // non-empty only in idea mode (used to create task)
    Provider       AgentType
    MaxIterations  int
    ValidatorCount int
    Workspace      string   // "worktree", "direct", "docker"
}
```

On submit, the orchestrator:
1. If idea mode: creates td task from `IdeaText` with `source:prompt` label, sets `TaskID`
2. Generates run ID (`sc-<6hex>`)
3. Creates the worktree (if workspace mode is "worktree")
4. Calls `td start <taskID>`
5. Logs `{"run_id":"...","phase":"plan","status":"starting",...}` to td
6. Emits `EventPlanStarted`
7. Transitions to Plan Review view mode (shows planner running)
8. Persists selected provider to state.json

**Modal construction** (using existing modal library):

```go
p.launchModal = modal.New("Run Task",
    modal.WithWidth(60),
    modal.WithPrimaryAction(launchRunID),
    modal.WithHints(true),
).
    // Idea mode: show text input; existing task mode: show task header
    AddSection(modal.When(ideaMode,
        modal.Input(launchIdeaID, &ideaInput,
            modal.WithPlaceholder("What do you want to build?"),
        ),
    )).
    AddSection(modal.When(!ideaMode,
        p.taskHeaderSection(taskID),              // Custom: reads td show
    )).
    AddSection(modal.Spacer()).
    AddSection(modal.Text("Provider")).
    AddSection(modal.List(launchProviderID,       // Single-focus list
        providerItems,
        modal.WithSelected(lastUsedIdx),
        modal.WithSingleFocus(true),
    )).
    AddSection(modal.Spacer()).
    AddSection(p.optionsRowSection()).            // Custom: inline config
    AddSection(modal.Spacer()).
    AddSection(modal.Buttons(
        modal.Btn(" Run ", launchRunID),
        modal.Btn(" Cancel ", launchCancelID),
    ))
```

**Comparison with workspace create modal**:

| Aspect | Workspace Create | Orchestration Launch |
|--------|-----------------|---------------------|
| Focus steps | 8 (name, branch, prompt, task, agent, perms, create, cancel) | 4-5 (idea input in idea mode, provider, options, run, cancel) |
| Required input | Name (text input with validation) | Idea text (idea mode only) |
| Dropdowns | Branch (filtered), Task (filtered) | None |
| Text inputs | 3 (name, branch, task search) | 0-1 (idea input in idea mode) |
| Primary purpose | Configure from scratch | Confirm and go |
| Fast path | Cannot skip — name is required | Shift+Enter skips modal entirely (existing task) |
| Conditional sections | Prompt ticket mode hides/shows task | Idea mode swaps header for text input |

The orchestration modal is deliberately simpler. The workspace modal is a creation form; the orchestration modal is a launch confirmation (or a one-field creation + launch confirmation in idea mode).

#### Keyboard Commands

Each context below maps to a `FocusContext()` return value, a set of `Commands()` entries, and bindings in `internal/keymap/bindings.go` — per `docs/guides/ui-feature-guide.md`. Command names are one word where possible. `q` behavior follows `isRootContext()` conventions (orchestrator-select is a root context).

```
Context: orchestrator-select
  Enter    Open launch modal for selected task
  Shift+Enter  Quick-run with last provider + defaults
  n        Open launch modal in idea mode (type an idea, task auto-created)
  /        Search tasks

Context: orchestrator-plan
  Enter    Accept plan
  e        Edit plan (opens in editor)
  r        Regenerate plan
  Esc      Cancel

Context: orchestrator-running
  v        Toggle validator detail view
  d        View diff so far
  f        View modified files
  c        Cancel run
  Tab      Switch to git status plugin (shows live diff)

Context: orchestrator-results
  m        Merge worktree to main
  d        View final diff
  Enter    Accept and close task
  r        Retry with modifications
```

### td Integration Points

There are two levels of td integration: what the **orchestrator** does (lifecycle management) and what **agents** do (self-directed via td commands in their prompt).

#### Orchestrator-side (lifecycle coordination)

The orchestrator calls td directly for lifecycle transitions that require coordination:

| Event | td Command | Why orchestrator, not agent |
|-------|------------|----------------------------|
| Run starts | `td start <id>` | Must happen before any agent spawns |
| Plan rejected | `td unstart <id> --reason "plan rejected"` | Return task to `open` if user rejects plan |
| Session creation | `TD_SESSION_ID` env var per agent | Agents need isolated sessions |
| Phase transition | `td log --type orchestration '<json>'` | Run state for crash recovery |
| Agent timeout | `td log --blocker "agent timed out"` | Orchestrator monitors liveness |
| Submit for review | `td review <id>` | Moves task to `in_review` once handoff exists |
| Review decision | `td approve <id>` / `td reject <id>` | Reviewer-of-record session enforces td review guard |
| Iteration start | `td log --blocker "findings..."` | Routes validator output to td before next implementer |
| Run complete | `td log --type orchestration '<json phase:complete>'` | Marks run state as finished |
| Run failed | `td log --type orchestration '<json phase:failed>'` | Marks run state for recovery |
| Run cancelled | `td log --type orchestration '<json phase:cancelled>'` | User-initiated cancellation |
| Handoff | `td handoff <id> --done "..." --remaining "..."` | Captures final state for future sessions |

#### Agent-side (self-directed)

Agents are told to use td commands in their prompt. The orchestrator does not enforce or verify these - it trusts the agent to follow the instructions:

| Action | td Command in Prompt | Purpose |
|--------|---------------------|---------|
| Read assignment | `td show <id>` | Agent reads task title, description, criteria |
| Read full context | `td context <id>` | Agent reads logs, handoffs, comments, deps |
| Orient session | `td usage` | Agent sees its session state and open work |
| Log progress | `td log "what I did"` | Creates audit trail |
| Log decisions | `td log --decision "why I chose X"` | Captures reasoning for future sessions |
| Log blockers | `td log --blocker "stuck on Y"` | Signals issues |

This split means the orchestrator handles **when** things happen (lifecycle), while agents handle **what** they learn and record (content). The orchestrator never needs to parse task content.

#### Session Management

The orchestrator creates a td session for each agent role by setting `TD_SESSION_ID` as an environment variable. Session IDs follow the format `sc-<run_id>-<role>`:

- Planner: `sc-a1b2c3-plan`
- Implementer (iteration 1): `sc-a1b2c3-impl1`
- Validator 1 (iteration 1): `sc-a1b2c3-val1i1`
- Validator 2 (iteration 1): `sc-a1b2c3-val2i1`

This produces unique, human-readable session IDs that are traceable back to the run. td stores these under `.todos/sessions/<branch>/explicit_sc-a1b2c3-impl1.json`. The `sc-` prefix makes it clear these sessions belong to sidecar orchestration. td's existing `TD_SESSION_ID` mechanism supports arbitrary strings, so no changes to td are needed for this.

**Important:** agent prompts should **not** call `td usage --new-session` or otherwise rotate sessions. The orchestrator assigns the session explicitly via `TD_SESSION_ID`; agents should only run `td usage` to view their current session. This keeps all logs and review history correctly attributed to the run.

### Task Engine Adapter Pattern

The orchestrator's only coupling to td is a set of **prompt templates** and **lifecycle commands**. These are defined in a task engine adapter:

```go
// Sentinel errors for idempotent handling
var (
    ErrTaskNotFound      = errors.New("task not found")
    ErrInvalidTransition = errors.New("invalid state transition")
    ErrSelfReview        = errors.New("reviewer cannot be implementer")
)

type TaskEngineAdapter interface {
    // Task creation (for idea-first flow)
    // Creates a minimal task with the given title and source:prompt label
    CreateTask(title string) (taskID string, err error)

    // Lifecycle commands (called by orchestrator)
    // StartTask returns nil if already in_progress (idempotent)
    StartTask(taskID string) error
    UnstartTask(taskID string, reason string) error
    SubmitForReview(taskID string) error
    ApproveTask(taskID string) error
    RejectTask(taskID string, reason string) error
    LogBlocker(taskID string, message string) error
    RecordHandoff(taskID string, done, remaining string) error

    // Orchestration run state (stored as JSON logs in the task engine)
    LogOrchestrationEvent(taskID string, event OrchestrationEvent) error
    GetOrchestrationLogs(taskID string) (*OrchestrationLogs, error)

    // Prompt fragments (included in agent prompts)
    ViewTaskCmd(taskID string) string      // e.g. "td show td-a1b2"
    FullContextCmd(taskID string) string   // e.g. "td context td-a1b2"
    OrientSessionCmd() string              // e.g. "td usage"
    LogProgressCmd() string                // e.g. "td log \"your progress\""
    LogDecisionCmd() string                // e.g. "td log --decision \"your decision\""

    // Session management
    // Returns a session ID for the given run and role, e.g. "sc-a1b2c3-impl1"
    SessionID(runID string, role string) string
}
```

The `StartTask` method is idempotent — if the task is already `in_progress`, it returns nil. This is critical for crash recovery: retrying a phase transition is always safe. Other `ErrInvalidTransition` errors (e.g., trying to approve a task that's not `in_review`) are logged and surfaced to the user.

The default implementation uses td. Alternative implementations could wrap Jira, Linear, GitHub Issues, or any other task system. The adapter provides both the CLI commands the orchestrator calls and the command strings injected into agent prompts.

This means swapping task engines is a matter of implementing a new adapter — the orchestration logic, agent runner, and TUI plugin don't change.

#### Task Engine Portability Assessment

The adapter pattern is designed to make the task engine swappable. Here's what's portable and what would need work:

**Fully portable (adapter handles it):**

| Capability | td | Jira/Linear/GitHub | Notes |
|---|---|---|---|
| Create task | `td create` | API call | Adapter's `CreateTask()` |
| Read task | `td show` | API call | Adapter's `ViewTaskCmd()` returns CLI string for agent |
| State transitions | `td start`, `td unstart`, `td review`, `td approve`, `td reject` | API calls | Adapter's lifecycle methods |
| Log progress | `td log` | Add comment via API | Adapter's `LogProgressCmd()` returns CLI string |
| Orchestration state | `td log --type orchestration` | Structured comment | Adapter's `LogOrchestrationEvent()` |
| Task creation labels | `--label source:prompt` | Jira labels, Linear labels | Adapter's `CreateTask()` |

**Requires a CLI wrapper for the backend:**

Agents interact with the task engine by running shell commands (e.g., `td show td-a1b2`). This is fundamental to the task-ID-driven prompt design. For Jira or Linear, agents would need a CLI tool that provides equivalent commands:

```
# td adapter prompt fragments
td show td-a1b2
td context td-a1b2
td log --decision "my plan"

# hypothetical Jira adapter prompt fragments
jira-cli show PROJ-123
jira-cli context PROJ-123
jira-cli comment PROJ-123 --type decision "my plan"
```

The adapter's prompt fragment methods (`ViewTaskCmd`, `FullContextCmd`, `LogProgressCmd`, etc.) return these command strings. The orchestrator doesn't know or care what CLI the agent runs — it just injects the strings the adapter provides.

If no CLI exists for the backend, a thin wrapper would need to be built. This is the main integration cost for non-td backends.

**td-specific features that degrade gracefully:**

| Feature | td | Without td | Impact |
|---|---|---|---|
| Session isolation (`TD_SESSION_ID`) | Native support | Adapter ignores or simulates | Validators still get separate sessions; audit trail less granular |
| Typed logs (`--type orchestration`) | Native log types | Prefixed comments (e.g., `[orchestration] {...}`) | Recovery parsing works the same via adapter |
| `td context` (full task context) | Rich multi-section output | Varies by backend | Agents get whatever the CLI provides |
| `source:prompt` label | Native labels | Backend-specific labels or tags | Display-only; no functional impact |

**Not coupled to td at all:**

- Orchestration engine lifecycle (plan → implement → validate → iterate)
- Agent runner (shells out to CLI agents)
- Event system (TUI updates)
- Run ID generation and session ID format
- Crash recovery logic (reads from adapter, not td directly)
- All TUI views and modals
- Worktree management

**Bottom line:** The orchestrator is coupled to the *adapter interface*, not to td. Swapping backends requires: (1) implementing the adapter (~15 methods), and (2) providing a CLI tool that agents can run to read/write task state. The orchestration engine, agent runner, TUI, and all recovery logic are unchanged.

### Configuration

Added to sidecar's `config.json`:

```json
{
  "plugins": {
    "orchestrator": {
      "enabled": true,
      "provider": "claude",
      "maxIterations": 3,
      "validatorCount": 2,
      "workspace": "worktree",
      "autoMerge": false,
      "providerBinary": ""
    }
  }
}
```

Minimal configuration. The provider binary is auto-detected if not specified. Model selection is left to the CLI agent's own configuration.

## Implementation Phases

### Phase 1: Engine Core

Build the orchestration engine as a standalone package (`internal/orchestrator/`). No TUI dependency. Testable with mock runners.

- Engine lifecycle (plan → build → validate → iterate)
- Agent runner interface + Claude runner implementation
- Task engine adapter interface + td implementation (including orchestration log type)
- Workspace management (worktree creation, cleanup)
- Event emission
- Heartbeat timeout (agent liveness monitoring)
- Crash recovery (read orchestration logs from td, reconstruct run state)
- Unit tests with mock agent runner

### Phase 2: TUI Plugin

Build the sidecar plugin that wraps the engine.

- Plugin boilerplate (ID, Init, Start, Stop, Update, View, Commands)
- Task selection view (reads from td)
- Plan review view
- Implementation progress view (event stream rendering)
- Validation results view
- Cross-plugin messaging (git status refresh, file browser navigation)
- Keyboard commands and footer hints

### Phase 3: Multi-Provider

Add runners for additional CLI agents.

- Codex runner
- Gemini runner
- Provider auto-detection
- Per-task provider override

### Phase 4: Advanced Features

- Docker workspace isolation
- Parallel task orchestration (multiple tasks running simultaneously)
- Custom validator configurations (security-focused, test-focused)
- Orchestration templates (configurable agent topologies)
- Resume interrupted orchestrations

## Design Decisions

### Why shell out to CLI agents instead of API calls?

CLI agents handle authentication, model selection, tool use, and context management. The orchestrator doesn't need to reimplement any of that. It just needs to give the agent a prompt and a working directory.

### Why not a separate process communicating with sidecar?

Adding IPC complexity for something that benefits from tight TUI integration is wrong. The orchestrator needs to emit events that update the view in real-time, trigger cross-plugin navigation, and read td state directly. In-process is simpler and more responsive.

### Why pass task IDs instead of task content?

Three reasons:

1. **Agents that use tools learn faster than agents that receive pre-digested context.** When an agent runs `td show td-a1b2` and reads the task itself, it exercises the same tool-use patterns it needs for the rest of the work. When it runs `td log "implemented rate limiter"`, it builds an audit trail that persists beyond its session. Spoon-feeding context in the prompt teaches the agent nothing about the workflow.

2. **The orchestrator stays simple and pluggable.** It doesn't parse td output, doesn't understand td's data model, doesn't manage token budgets for task content. It knows: "pass this task ID and these command templates." Swapping td for Jira means changing the adapter's command strings, not the orchestration logic.

3. **Context is always live.** If the planner logs a decision, the implementer reads it from td in real-time. If a validator logs a rejection, the next implementer reads it fresh. No risk of caching stale snapshots or assembling context from an outdated read.

The tradeoff: agents need tool access to run shell commands. CLI agents (Claude Code, Codex, Gemini CLI) already have this. API-only agents without tool use would need a different integration path.

### Why store run state in td instead of a local file?

A local `.sidecar-orchestration/run.json` file would be faster to read (~microseconds vs ~50ms for a td CLI call) and wouldn't require parsing log messages. But it creates a second source of truth. If the file says "phase: validate" but td's logs show the last event was implementation, which do you trust? The answer is always "td" - so just use td.

Storing orchestration state as td logs means:
- One place to look for everything about a task's lifecycle
- State survives worktree deletion and machine switches
- `td context` shows orchestration events alongside progress logs and decisions
- The TaskEngineAdapter pattern keeps this pluggable - other backends store run state however they need to

td needs one small addition: an `orchestration` log type. This is a one-line enum change. The log message is JSON, parsed by the adapter via `json.Unmarshal` on recovery. td may also want a display enhancement to pretty-print orchestration log entries in `td context` output.

### Why independent validation?

Validators run in their own td sessions and naturally don't see the implementer's session conversation. But they have full access to the task's shared state: description, acceptance criteria, logs, git history, and the full codebase. This gives them maximum context for quality review while preventing them from unconsciously deferring to the implementer's reasoning. The isolation is a natural consequence of td's session model, not an artificial restriction.

### Why does the planner always run?

Even for well-specified tasks with detailed acceptance criteria, the planner reads the codebase and logs its assessment before implementation begins. This produces better results than skipping straight to implementation because:

1. **The planner catches mismatches** between the task description and the actual codebase state. A task written last week may reference files that have since changed.
2. **The plan becomes a contract** that the user reviews. The user accepts the plan, not just the task description. This is the last checkpoint before agents start modifying code.
3. **The implementer benefits from the planner's codebase analysis.** The planner's decision logs (read by the implementer via `td context`) contain file paths, approach notes, and risk assessments that orient the implementer faster than reading the whole codebase from scratch.

For simple tasks, the planner's work is light — it confirms the approach and logs a brief decision. The overhead is one agent invocation, which is worth the consistency of always having a reviewed plan.

### Why a unified entry point (task ID always)?

The orchestrator always receives a task ID. When the user starts from an idea (text string), the orchestrator creates a minimal td task before anything else. This means:

1. **One engine path.** No branching logic for "idea vs. task vs. epic." The planner always gets a task ID, reads it from td, and plans accordingly.
2. **Everything is tracked.** Even ideas get a td task, so there's a record of what was requested, what was planned, and what was implemented.
3. **The planner adapts naturally.** A planner that reads a bare task with just a title does more work than one that reads a detailed epic. No special instructions needed — the planner uses its judgment based on what it finds.

The `source:prompt` label on auto-created tasks lets users distinguish them from manually created tasks without adding noise to agent prompts.

### Why not complexity-based model selection?

It adds substantial configuration surface area for marginal benefit. Most users want to use their preferred model for everything. If cost matters, they can configure their CLI agent to use a cheaper model. The orchestrator doesn't need to second-guess the user's model choice.

If demand emerges, this can be added later as an optional feature without changing the core architecture.

## Implementation Watch Points

Three areas that will need particular care during implementation.

### 1. Planner-to-Implementation Handoff

The transition from planning to implementation is the most stateful moment in the lifecycle. The planner process exits, the TUI must display the plan for review, and the user's accept/reject decision gates the entire rest of the run.

```
                    Orchestrator                TUI                     User
                        │                        │                        │
  log(plan,starting) ───┤                        │                        │
  spawn planner ────────┤                        │                        │
  log(plan,spawned) ────┤                        │                        │
  log(plan,running) ────┤──── EventPlanStarted──▶│ "Planning..."          │
                        │                        │                        │
          ┌─────────────┤                        │                        │
          │ planner runs│                        │                        │
          │ logs to td  │──── EventFileModified─▶│ (live updates)         │
          │ exits       │                        │                        │
          └─────────────┤                        │                        │
                        │                        │                        │
  detect exit ──────────┤                        │                        │
  log(plan,done) ───────┤──── EventPlanReady ──▶│ show plan review ──────▶│
                        │                        │◀──── accept/reject ────│
                        │◀── PlanRejected ───────│                        │
  td unstart task ──────┤                        │                        │
  log(plan,rejected) ───┤                        │                        │
                        │◀── PlanAccepted ───────│                        │
  log(impl,starting) ───┤                        │                        │
  spawn implementer ────┤──── EventImplStarted─▶│ "Implementing..."      │
  log(impl,spawned) ────┤                        │                        │
                        │                        │                        │
```

Key: the orchestrator blocks between `EventPlanReady` and receiving the user's decision. No agent is running during plan review. The plan lives in td as planner-authored updates (decision logs or task edits) — the TUI reads and displays them, the user reviews, and only then does the orchestrator proceed.

### 2. Run Detail Modal Responsiveness

The Run Detail Modal is how users judge the entire system. If it feels laggy or shows stale state, the orchestrator feels untrustworthy.

- Poll td for orchestration logs every 1-2 seconds while the modal is open. Use sidecar's existing adaptive polling pattern (faster when state is changing, slower when idle).
- The timeline is append-only — new events add to the bottom. Never re-render the full timeline on each poll; diff against the last known event count.
- Show "Last output: Xs ago" for the active agent so the user can see liveness without watching raw output.
- Test: open the modal, leave it open for a full plan→implement→validate cycle, verify every transition appears without dismissing/reopening.

### 3. Recovery Testing

The write-then-act pattern (`starting` → spawn → `running`) creates clean seams for testing, but recovery is still the hardest thing to get right because it's the least exercised code path.

- Write tests that kill the engine at every phase transition point (after `starting` but before `running`, after `running` but before `done`, mid-validation with partial results).
- Verify that recovery from each kill point produces the correct `RecoveryState` and that resuming from that state reaches `phase:complete`.
- Test the degenerate case: multiple interrupted runs on the same task. Recovery must find the latest incomplete run ID and ignore older completed/failed/cancelled runs.
- Keep recovery logic in the engine (not the TUI plugin) so it's testable without rendering.

## Open Questions

1. **Plan editing UX** - Should the plan be editable in sidecar's inline editor, or should it open an external editor? Inline is more integrated; external is more capable for large edits.

2. **Validator prompt customization** - Should users be able to configure what validators look for (e.g., "focus on security" or "focus on test coverage")? Or is the default "assess against acceptance criteria" sufficient?

3. **Agent output streaming** - Different CLI agents have different streaming capabilities. Claude Code streams; others may not. How much of the real-time progress view depends on streaming?

4. **Worktree naming convention** - `agent/<task-id>-<slug>`? `orchestrator/<task-id>`? Should this match the workspace plugin's conventions?

5. **Failure escalation** - After max iterations, the orchestrator logs a handoff to td and marks the run as failed. Should it also offer to open the worktree in the user's editor for manual intervention? The worktree still exists with the agent's partial work.

6. ~~**Orchestration log format**~~ **Resolved**: JSON log messages with a structured `OrchestrationEvent` type. Each entry includes a `run_id` for disambiguation. td may want a pretty-print option for `td context` display.
