# Creating Prompts

Prompts are reusable templates that configure the initial context for agents when creating workspaces. They help standardize common workflows like code reviews, bug fixes, or feature implementation.

## Configuration Locations

Prompts are defined in JSON config files:

| Scope   | Path                                  | Override Priority |
|---------|---------------------------------------|-------------------|
| Global  | `~/.config/sidecar/config.json`       | Lower             |
| Project | `.sidecar/config.json` (in project)   | Higher            |

Project prompts override global prompts with the same name.

## Config File Format

```json
{
  "prompts": [
    {
      "name": "Code Review",
      "ticketMode": "optional",
      "body": "Do a detailed code review of {{ticket || 'open reviews'}}.\nFocus on correctness, edge cases, and test coverage."
    },
    {
      "name": "Bug Fix",
      "ticketMode": "required",
      "body": "Fix issue {{ticket}}. Use td to track progress.\nRun tests before marking complete."
    },
    {
      "name": "Setup Project",
      "ticketMode": "none",
      "body": "Set up the development environment and verify all tests pass."
    }
  ]
}
```

## Default Prompts

If no config file exists, the app automatically creates one with 5 built-in prompt templates:
- Begin Work on Ticket (required)
- Code Review Ticket (required)
- Plan to Epic (No Impl) (none)
- Plan to Epic + Implement (none)
- TD Review Session (none)

You can customize these by editing the config file. Press `d` in the prompt picker to install defaults if none are configured.

## Prompt Fields

### name (required)
Display name shown in the prompt picker. Keep it concise.

### ticketMode (optional)
Controls how the ticket/task field behaves:

| Mode       | Behavior                                      |
|------------|-----------------------------------------------|
| `optional` | Task field shown, can be empty (default)      |
| `required` | Task must be selected before creating         |
| `none`     | Task field is hidden, prompt stands alone     |

### body (required)
The prompt text sent to the agent. Supports template variables. Use `\n` for newlines.

## Template Variables

### `{{ticket}}`
Expands to the selected task ID. Returns empty string if no task selected.

```json
"body": "Fix issue {{ticket}}."
// With task td-abc123: "Fix issue td-abc123."
// Without task: "Fix issue ."
```

### `{{ticket || 'fallback'}}`
Expands to task ID, or the fallback text if no task selected.

```json
"body": "Review {{ticket || 'all open items'}}."
// With task td-abc123: "Review td-abc123."
// Without task: "Review all open items."
```

Note: Only single quotes are supported for fallback text:
- `{{ticket || 'default'}}` ✓ Works
- `{{ticket || "default"}}` ✗ Won't work

## Examples

### Task-Required Workflow
```json
{
  "name": "Implement Task",
  "ticketMode": "required",
  "body": "Begin work on {{ticket}}. Use td to track progress.\nRead the task description carefully before starting."
}
```

### Standalone Workflow
```json
{
  "name": "Run Tests",
  "ticketMode": "none",
  "body": "Run the full test suite. Fix any failures.\nReport a summary when complete."
}
```

### Flexible Workflow
```json
{
  "name": "Code Review Session",
  "ticketMode": "optional",
  "body": "Start a review session for {{ticket || 'open reviews'}}.\nCreate td tasks for any issues found."
}
```

### Backlog Refinement
```json
{
  "name": "Backlog Refinement",
  "ticketMode": "none",
  "body": "Start a backlog refinement session. Use td to find all tasks in the backlog starting with the oldest. For each task:\n\n1. Determine relevance - has it been implemented, is it still needed? If not relevant, comment on the task and close it. If questionable, leave open with notes.\n\n2. Update tasks with out-of-date code references to reflect the current state of the codebase.\n\nUse sub-agents to analyze multiple tasks in parallel. Present a report of findings and changes when complete."
}
```

## Scope Indicators

In the prompt picker, prompts show their source:
- `[G]` - Global prompt (from `~/.config/sidecar/`)
- `[P]` - Project prompt (from `.sidecar/`)

Project prompts take precedence when names match.

## Important Notes

- Prompt names must match exactly (case-sensitive) for project overrides to work
- Only single quotes work in fallback syntax
- The ticketMode field defaults to `optional` if omitted
- Config files are loaded when creating a workspace; restart the app to reload changes

## Tips

1. **Keep names short** - They appear in a table with limited width
2. **Use `ticketMode: none`** for prompts that don't need a specific task
3. **Use fallbacks** (`{{ticket || 'default'}}`) for optional task association
4. **Project prompts** can customize global defaults for specific repos
5. **Use `\n`** for newlines in prompt bodies
