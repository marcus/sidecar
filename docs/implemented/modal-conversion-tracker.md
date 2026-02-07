# Modal Conversion Tracker

This document tracks the conversion of all modals in Sidecar to the new `internal/modal` library system.

**Legend:**
- ✓ = Converted to `internal/modal`
- ○ = Not yet converted

---

## Conversion Guide

### Reference Implementation

The **Rename Shell modal** is the simplest complete example of a converted modal. Study these files:
- `internal/plugins/workspace/view_modals.go` - search for `ensureRenameShellModal`, `renameShellInfoSection`, `renderRenameShellModal`
- `internal/plugins/workspace/keys.go` - search for `handleRenameShellKeys`
- `internal/plugins/workspace/mouse.go` - search for `handleRenameShellModalMouse`
- `internal/plugins/workspace/plugin.go` - search for `renameShellModal` fields

Also see `.claude/skills/ui-features/SKILL.md` for the modal initialization pattern.

### Conversion Checklist

#### Add
- [ ] `ensureXxxModal()` function with width caching (prevents rebuild every frame)
- [ ] `xxxModal *modal.Modal` field in Plugin struct
- [ ] `xxxModalWidth int` field for width cache
- [ ] Call `ensureXxxModal()` at start of **key handler** (critical - prevents dropped first keypress)
- [ ] Call `ensureXxxModal()` at start of **mouse handler**
- [ ] `handleXxxModalMouse(msg tea.MouseMsg) tea.Cmd` function
- [ ] Early return in main `handleMouse()` for the view mode (before `mouseHandler.HandleMouse`)

#### Remove
- [ ] `xxxFocus int` field (modal handles focus internally)
- [ ] `xxxButtonHover int` field (modal handles hover internally)
- [ ] Manual hit region registration in render (modal.Render registers regions automatically)
- [ ] Old `regionXxx` constants from plugin.go
- [ ] Manual tab/shift-tab/enter/esc handling (modal.HandleKey handles these)
- [ ] References to old region constants in mouse_test.go
- [ ] Hover handling case in `handleMouseHover` for this view mode

#### Keep
- [ ] Session/target item field (e.g., `renameShellSession *ShellSession`)
- [ ] Input model field (e.g., `renameShellInput textinput.Model`)
- [ ] Error string field (e.g., `renameShellError string`)
- [ ] Clear function - update to also clear modal and width cache

#### Update
- [ ] `clearXxxModal()` to set modal to nil and width cache to 0
- [ ] Initialization code (when modal opens) - remove focus/hover initialization
- [ ] Tracker table below - mark as ✓

### Common Pitfalls

1. **Dropped first keypress**: Forgetting to call `ensureXxxModal()` in the key handler. The modal is created in View, but Update runs first, so the first key after opening hits a nil modal.

2. **Negative width**: Not clamping `modalW` to a minimum (e.g., 20) when terminal is narrow.

3. **Mouse not working**: Forgetting the early return in `handleMouse()` before `mouseHandler.HandleMouse(msg)` is called.

4. **Stale modal on reopen**: Forgetting to reset `xxxModalWidth` to 0 in the clear function, causing the cached modal to be reused with stale state.

---

## App-Level Modals

| Modal | Type | Converted |
|-------|------|-----------|
| Quit Confirm | Confirmation | ✓ |
| Command Palette | Search | N/A* |
| Help Modal | Info | ✓ |
| Diagnostics | Info | ✓ |
| Project Switcher | Selection | ✓ |
| Project Add | Form | ✓ |
| Theme Switcher | Selection | ✓ |
| Community Browser | Search | ✓ |

---

## Workspace Plugin Modals

| Modal | Type | Converted |
|-------|------|-----------|
| Create Worktree | Form | ✓ |
| Task Link | Dropdown | ✓ |
| Confirm Delete | Confirmation | ✓ |
| Confirm Delete Shell | Confirmation | ✓ |
| Rename Shell | Form | ✓ |
| Prompt Picker | Selection | ✓ |
| Agent Choice | Selection | ✓ |
| Merge Workflow | Multi-step | ✓ |
| Commit for Merge | Form | ✓ |
| Type Selector | Selection | ✓ |

---

## Git Status Plugin Modals

| Modal | Type | Converted |
|-------|------|-----------|
| Confirm Discard | Confirmation | ✓ |
| Confirm Stash Pop | Confirmation | ✓ |
| Pull Menu | Selection | ✓ |
| Pull Conflict | Menu | ✓ |
| Push Menu | Selection | ✓ |
| Commit Message | Form | ✓ |
| Branch Picker | Selection | ✓ |

---

## File Browser Plugin Modals

| Modal | Type | Converted |
|-------|------|-----------|
| Blame View | Info | ✓ |
| File Info | Info | ✓ |
| Project Search | Results | ✓ |

---

## Notes

**\*Command Palette (N/A):** Intentionally not converted. It's a full Bubble Tea component with complex features (fuzzy search, multi-layer display, match highlighting) that the modal library doesn't support. Already uses `ui.OverlayModal` for backdrop correctly.

---

## Summary

**Total Modals:** 27 (excluding N/A)
**Converted:** 27 (100%)
**Remaining:** 0 (0%)
