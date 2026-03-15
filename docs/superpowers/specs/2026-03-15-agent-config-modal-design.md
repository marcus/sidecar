# Agent Config Modal Design

## Problem

When a tmux session is killed/stopped and the user presses 's' to start a new agent, there is no opportunity to choose an agent type, toggle skip-permissions, or select a prompt. The agent starts immediately with the previously-saved type and no options. Similarly, restarting a running agent offers no configuration.

## Solution

A new standalone "Agent Config Modal" that presents agent type selection, skip-permissions toggle, and prompt picker before starting or restarting an agent on an existing worktree.

## Entry Points

1. **'s' with no agent running** — opens the agent config modal directly (replaces the current immediate `StartAgent` call)
2. **'s' with agent running → "Restart"** — after selecting restart in the existing attach/restart choice modal, transitions to the agent config modal (replaces the current immediate restart)

The existing attach/restart choice modal is unchanged. "Attach" still attaches immediately. Only "Restart" now routes through the new modal.

## Modal Layout

```
┌─── Start Agent: <worktree-name> ───┐
│                                     │
│ Prompt:                             │
│ ┌─(none)─────────────────────────┐  │
│   Press Enter to select a prompt    │
│                                     │
│ Agent:                              │
│   > Claude                          │
│     Codex                           │
│     Copilot                         │
│     Gemini                          │
│     Cursor                          │
│     OpenCode                        │
│     Pi                              │
│     Amp                             │
│     None                            │
│                                     │
│   ☐ Auto-approve all actions        │
│     (Adds --dangerously-skip-...)   │
│                                     │
│       [ Start ]  [ Cancel ]         │
└─────────────────────────────────────┘
```

Modal width: 60 (narrower than create modal's 70, since fewer fields).

Sections from top to bottom:
1. **Prompt selector** — clickable field, opens the existing `ViewModePromptPicker` overlay on Enter. Displays selected prompt name or "(none)".
2. **Agent type list** — scrollable list of `AgentTypeOrder` entries. Pre-selected to the worktree's saved agent type.
3. **Skip permissions checkbox** — conditionally shown when the selected agent has a non-empty `SkipPermissionsFlags` entry. Shows the actual flag as a hint below.
4. **Buttons** — "Start" (primary action) and "Cancel".

## New State Fields on Plugin

```go
// Agent config modal state
agentConfigWorktree    *Worktree    // Target worktree
agentConfigIsRestart   bool         // true = stop first, false = fresh start
agentConfigAgentType   AgentType    // Selected agent type
agentConfigAgentIdx    int          // List selection index
agentConfigSkipPerms   bool         // Skip permissions toggle
agentConfigPromptIdx   int          // Selected prompt index (-1 = none)
agentConfigPrompts     []Prompt     // Loaded prompts for this modal (independent of createPrompts)
agentConfigModal       *modal.Modal // Cached modal instance
agentConfigModalWidth  int          // For rebuild detection

// Prompt picker return routing
promptPickerReturnMode ViewMode     // Which view mode to return to after prompt picker
```

## New View Mode

```go
ViewModeAgentConfig // Agent configuration modal
```

## New File: `agent_config_modal.go`

Contains:
- `ensureAgentConfigModal()` — builds the modal declaratively using the `modal` package
- `syncAgentConfigModalFocus()` — syncs focus state
- `renderAgentConfigModal()` — renders modal over dimmed background
- `clearAgentConfigModal()` — resets all state fields (see explicit list below)
- `getAgentConfigPrompt()` — resolves `agentConfigPromptIdx` to `*Prompt` (see definition below)
- Custom section builders for prompt display, agent label, skip permissions spacer/hint
- `shouldShowAgentConfigSkipPerms()` — returns true if selected agent has a skip-permissions flag

Section builders follow the same patterns as `create_modal.go` but reference `agentConfig*` state fields instead of `create*` fields.

### Element IDs

```go
const (
    agentConfigPromptFieldID     = "agent-config-prompt"
    agentConfigAgentListID       = "agent-config-agent-list"
    agentConfigSkipPermissionsID = "agent-config-skip-permissions"
    agentConfigSubmitID          = "agent-config-submit"
    agentConfigCancelID          = "agent-config-cancel"
    agentConfigAgentItemPrefix   = "agent-config-agent-"
)
```

### `clearAgentConfigModal()` — explicit field reset

```go
func (p *Plugin) clearAgentConfigModal() {
    p.agentConfigWorktree = nil
    p.agentConfigIsRestart = false
    p.agentConfigAgentType = ""
    p.agentConfigAgentIdx = 0
    p.agentConfigSkipPerms = false
    p.agentConfigPromptIdx = -1
    p.agentConfigPrompts = nil
    p.agentConfigModal = nil
    p.agentConfigModalWidth = 0
}
```

### `getAgentConfigPrompt()` — prompt resolution

```go
func (p *Plugin) getAgentConfigPrompt() *Prompt {
    if p.agentConfigPromptIdx < 0 || p.agentConfigPromptIdx >= len(p.agentConfigPrompts) {
        return nil
    }
    prompt := p.agentConfigPrompts[p.agentConfigPromptIdx]
    return &prompt
}
```

## Key Handler: `handleAgentConfigKeys()`

Located in `keys.go`. Follows the same pattern as other modal key handlers:

```go
func (p *Plugin) handleAgentConfigKeys(msg tea.KeyMsg) tea.Cmd {
    p.ensureAgentConfigModal()
    if p.agentConfigModal == nil {
        return nil
    }

    prevAgentIdx := p.agentConfigAgentIdx
    action, cmd := p.agentConfigModal.HandleKey(msg)

    // Sync agent type when selection changes
    if p.agentConfigAgentIdx != prevAgentIdx {
        if p.agentConfigAgentIdx >= 0 && p.agentConfigAgentIdx < len(AgentTypeOrder) {
            p.agentConfigAgentType = AgentTypeOrder[p.agentConfigAgentIdx]
        }
    }

    switch action {
    case "cancel", agentConfigCancelID:
        p.viewMode = ViewModeList
        p.clearAgentConfigModal()
        return nil
    case agentConfigPromptFieldID:
        // Open prompt picker, set return mode to agent config
        p.promptPickerReturnMode = ViewModeAgentConfig
        p.promptPicker = NewPromptPicker(p.agentConfigPrompts, p.width, p.height)
        p.viewMode = ViewModePromptPicker
        return nil
    case agentConfigSubmitID:
        return p.executeAgentConfig()
    }

    return cmd
}
```

## Execution: `executeAgentConfig()`

```go
func (p *Plugin) executeAgentConfig() tea.Cmd {
    wt := p.agentConfigWorktree
    agentType := p.agentConfigAgentType
    skipPerms := p.agentConfigSkipPerms
    prompt := p.getAgentConfigPrompt()
    isRestart := p.agentConfigIsRestart

    p.viewMode = ViewModeList
    p.clearAgentConfigModal()

    if wt == nil {
        return nil
    }

    if isRestart {
        return tea.Sequence(
            p.StopAgent(wt),
            func() tea.Msg {
                return restartAgentWithOptionsMsg{
                    worktree:  wt,
                    agentType: agentType,
                    skipPerms: skipPerms,
                    prompt:    prompt,
                }
            },
        )
    }
    return p.StartAgentWithOptions(wt, agentType, skipPerms, prompt)
}
```

## New Message Type

```go
// restartAgentWithOptionsMsg signals that an agent should be restarted with specific options.
type restartAgentWithOptionsMsg struct {
    worktree  *Worktree
    agentType AgentType
    skipPerms bool
    prompt    *Prompt
}
```

Handled in `update.go` alongside the existing `restartAgentMsg`:

```go
case restartAgentWithOptionsMsg:
    if msg.worktree != nil {
        return p, p.StartAgentWithOptions(msg.worktree, msg.agentType, msg.skipPerms, msg.prompt)
    }
    return p, nil
```

## Changes to Existing Files

### `keys.go`

1. **Line ~807** (`case "s"`, no agent branch): Replace `return p.StartAgent(wt, p.resolveWorktreeAgentType(wt))` with opening the agent config modal:
   ```go
   p.agentConfigWorktree = wt
   p.agentConfigIsRestart = false
   p.agentConfigAgentType = p.resolveWorktreeAgentType(wt)
   p.agentConfigAgentIdx = p.agentTypeIndex(p.agentConfigAgentType)
   p.agentConfigSkipPerms = false
   p.agentConfigPromptIdx = -1
   p.agentConfigPrompts = LoadPrompts(configDir, p.ctx.ProjectRoot)
   p.viewMode = ViewModeAgentConfig
   return nil
   ```

   Where `configDir` is obtained from `os.UserHomeDir() + "/.config/sidecar"` (same pattern as `initCreateModalBase()`).

2. **`executeAgentChoice()`** (line ~298, restart branch): Replace immediate restart with transitioning to agent config modal:
   ```go
   // Restart agent: open config modal instead of restarting immediately
   p.agentConfigWorktree = wt
   p.agentConfigIsRestart = true
   p.agentConfigAgentType = p.resolveWorktreeAgentType(wt)
   p.agentConfigAgentIdx = p.agentTypeIndex(p.agentConfigAgentType)
   p.agentConfigSkipPerms = false
   p.agentConfigPromptIdx = -1
   home, _ := os.UserHomeDir()
   configDir := filepath.Join(home, ".config", "sidecar")
   p.agentConfigPrompts = LoadPrompts(configDir, p.ctx.ProjectRoot)
   p.viewMode = ViewModeAgentConfig
   return nil
   ```

3. **`handleKeys()` switch**: Add `case ViewModeAgentConfig: return p.handleAgentConfigKeys(msg)`

### `view_list.go`

Add rendering case:
```go
case ViewModeAgentConfig:
    return p.renderAgentConfigModal(width, height)
```

### `commands.go`

1. Add `case ViewModeAgentConfig:` to `Commands()` — return Confirm/Cancel commands.
2. Add `case ViewModeAgentConfig:` to `KeyBindings()` — return keybinding context `"workspace-agent-config"`.
3. `ConsumesTextInput()` — no change needed. The agent config modal has no text input fields. When the prompt picker is opened from it, `ViewModePromptPicker` handles text consumption.

### `mouse.go`

Add `case ViewModeAgentConfig:` mouse handler delegating to `p.agentConfigModal.HandleMouse()`. Follow the same pattern as `ViewModeAgentChoice` mouse handling.

Note: `isModalViewMode()` uses a default `true` return for unknown view modes, so `ViewModeAgentConfig` is automatically treated as a modal. No change needed there.

### `plugin.go`

1. Add state fields listed above to the `Plugin` struct.
2. Add `promptPickerReturnMode ViewMode` field.
3. `outputVisibleForUnfocused()` — no change needed. It already returns `false` for all non-list/non-interactive view modes, which is correct (suppress polling while modal is open).

### `update.go`

1. Add `case restartAgentWithOptionsMsg:` handler (see above).

2. **Modify `PromptSelectedMsg` handler** to use `promptPickerReturnMode`:
   ```go
   case PromptSelectedMsg:
       returnMode := p.promptPickerReturnMode
       p.promptPicker = nil
       p.clearPromptPickerModal()

       if returnMode == ViewModeAgentConfig {
           p.viewMode = ViewModeAgentConfig
           if msg.Prompt != nil {
               for i, pr := range p.agentConfigPrompts {
                   if pr.Name == msg.Prompt.Name {
                       p.agentConfigPromptIdx = i
                       break
                   }
               }
           } else {
               p.agentConfigPromptIdx = -1
           }
       } else {
           // Existing create modal logic (unchanged)
           p.viewMode = ViewModeCreate
           if msg.Prompt != nil {
               for i, pr := range p.createPrompts {
                   if pr.Name == msg.Prompt.Name {
                       p.createPromptIdx = i
                       break
                   }
               }
               if msg.Prompt.TicketMode == TicketNone {
                   p.createFocus = 4
               } else {
                   p.createFocus = 3
               }
           } else {
               p.createPromptIdx = -1
               p.createFocus = 3
           }
       }
   ```

3. **Modify `PromptCancelledMsg` handler** to use `promptPickerReturnMode`:
   ```go
   case PromptCancelledMsg:
       returnMode := p.promptPickerReturnMode
       p.promptPicker = nil
       p.clearPromptPickerModal()
       if returnMode == ViewModeAgentConfig {
           p.viewMode = ViewModeAgentConfig
       } else {
           p.viewMode = ViewModeCreate
       }
   ```

4. **Set `promptPickerReturnMode` in existing create modal flow** — in `keys.go` where the create modal opens the prompt picker (around line ~1004 and ~1026), add:
   ```go
   p.promptPickerReturnMode = ViewModeCreate
   ```

5. **Modify `PromptInstallDefaultsMsg` handler** to respect `promptPickerReturnMode`. Currently it always reloads into `p.createPrompts`. When the picker was opened from agent config, it should update `agentConfigPrompts` instead:
   ```go
   case PromptInstallDefaultsMsg:
       // ... existing home dir / WriteDefaultPromptsToConfig logic ...
       if p.promptPickerReturnMode == ViewModeAgentConfig {
           p.agentConfigPrompts = LoadPrompts(configDir, p.ctx.ProjectRoot)
           p.promptPicker = NewPromptPicker(p.agentConfigPrompts, p.width, p.height)
       } else {
           p.createPrompts = LoadPrompts(configDir, p.ctx.ProjectRoot)
           p.promptPicker = NewPromptPicker(p.createPrompts, p.width, p.height)
       }
   ```

### `messages.go`

Add `restartAgentWithOptionsMsg` struct.

### `types.go`

Add `ViewModeAgentConfig` constant.

### `plugin.go` `Init()`

Add keymap registration for the new modal context:

```go
// Agent config modal context
ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-agent-config")
ctx.Keymap.RegisterPluginBinding("enter", "confirm", "workspace-agent-config")
ctx.Keymap.RegisterPluginBinding("tab", "next-field", "workspace-agent-config")
ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-field", "workspace-agent-config")
```

## Prompt Picker Integration

When the user activates the prompt field in the agent config modal:
1. `promptPickerReturnMode` is set to `ViewModeAgentConfig`
2. `promptPicker` is created with `p.agentConfigPrompts` (the modal's own prompt list)
3. `viewMode` switches to `ViewModePromptPicker`
4. The existing prompt picker overlay renders on top
5. On `PromptSelectedMsg`: handler checks `promptPickerReturnMode`, returns to `ViewModeAgentConfig`, updates `agentConfigPromptIdx` by matching `msg.Prompt.Name` against `agentConfigPrompts`
6. On `PromptCancelledMsg`: handler checks `promptPickerReturnMode`, returns to `ViewModeAgentConfig` with no changes

The existing create modal prompt picker flow is updated to set `promptPickerReturnMode = ViewModeCreate` before opening the picker, maintaining backward compatibility.

## Initialization

When opening the modal (both entry points):
- `agentConfigPrompts` is loaded via `LoadPrompts(configDir, p.ctx.ProjectRoot)` — independent of `createPrompts`
- `agentConfigAgentType` is pre-selected from `resolveWorktreeAgentType(wt)` (the worktree's saved agent type)
- `agentConfigAgentIdx` is set via `agentTypeIndex()` to match
- `agentConfigSkipPerms` defaults to `false`
- `agentConfigPromptIdx` defaults to `-1` (none)

## Edge Cases

- **Worktree deleted while modal is open**: `executeAgentConfig()` checks `wt == nil` and returns nil. This is consistent with the existing `restartAgentMsg` handler's behavior. Stale pointer risk is pre-existing and not addressed here.
- **Agent starts from another source while modal is open**: The modal operates on captured state. If the agent starts externally, the user will see the updated status after closing the modal. No special handling needed — same as existing modals.

## Testing

- Unit test for `handleAgentConfigKeys`: confirm, cancel, agent selection changes
- Unit test for `executeAgentConfig`: fresh start vs restart paths
- Unit test for `getAgentConfigPrompt`: valid index, invalid index, nil prompts
- Unit test for prompt picker return routing: `PromptSelectedMsg` and `PromptCancelledMsg` with both `ViewModeAgentConfig` and `ViewModeCreate` return modes
- Unit test for modal rendering (snapshot or content check)
- Integration: 's' key on stopped worktree opens modal, confirm starts agent with selected options
