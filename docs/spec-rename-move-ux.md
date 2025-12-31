# Plan: Improve File Rename/Move Input UX

## Problem

When pressing `m` (move) or `r` (rename) in the filebrowser plugin, the text input doesn't support cursor navigation (left/right arrows). Users must backspace and retype the entire path/filename.

## Current Implementation

**Location:** `internal/plugins/filebrowser/plugin.go` (lines 680-710)

The plugin uses raw string accumulation without cursor tracking:

- `fileOpInput string` stores the input
- Only supports: backspace, esc, enter, and printable chars
- No cursor position tracking

## Solution: Use bubbles/textinput

Replace manual input handling with Charm's `textinput` component (already used in `internal/palette/palette.go`). Provides:

- Left/right arrow navigation
- Home/End keys
- Ctrl+Left/Right for word navigation
- Proper cursor rendering
- Character insertion at cursor position

---

## Implementation Steps

### 1. Update Plugin State

**File:** `internal/plugins/filebrowser/plugin.go`

- Add import: `"github.com/charmbracelet/bubbles/textinput"`
- Replace `fileOpInput string` → `fileOpTextInput textinput.Model`

### 2. Initialize textinput on 'r'/'m' Keys

**File:** `internal/plugins/filebrowser/plugin.go` (~lines 550-568)

When entering rename/move mode:

```go
p.fileOpTextInput = textinput.New()
p.fileOpTextInput.SetValue(node.Name)  // or node.Path for move
p.fileOpTextInput.Focus()
p.fileOpTextInput.CursorEnd()
```

### 3. Update handleFileOpKey

**File:** `internal/plugins/filebrowser/plugin.go` (~lines 680-710)

- Keep esc/enter handling
- Delegate all other keys to `p.fileOpTextInput.Update(msg)`

### 4. Update executeFileOp

**File:** `internal/plugins/filebrowser/plugin.go` (~lines 267-330)

- Replace `p.fileOpInput` → `p.fileOpTextInput.Value()`
- Add filename validation (empty, invalid chars)
- For moves: check if parent dir exists
  - If not, set `fileOpConfirmCreate = true` and show prompt
  - Add new state: `fileOpConfirmCreate bool`
  - Handle `y` to create dir + proceed, any other key to return to edit mode

### 5. Update View Rendering

**File:** `internal/plugins/filebrowser/view.go` (~lines 150-173)

- Replace manual cursor rendering with `p.fileOpTextInput.View()`
- When `fileOpConfirmCreate` is true, show:
  `"Create 'foo/bar'? [y]es / [n]o"`

### 6. Update Tests

**File:** `internal/plugins/filebrowser/fileops_test.go`

- Add tests for new validation functions

---

## Files to Modify

| File                                           | Changes                                                   |
| ---------------------------------------------- | --------------------------------------------------------- |
| `internal/plugins/filebrowser/plugin.go`       | Replace string input with textinput.Model, add validation |
| `internal/plugins/filebrowser/view.go`         | Use textinput.View() for rendering                        |
| `internal/plugins/filebrowser/fileops_test.go` | Add validation tests                                      |

---

## New Error Handling

| Error                                    | Condition                                 |
| ---------------------------------------- | ----------------------------------------- |
| "filename cannot be empty"               | Empty input on enter                      |
| "filename contains invalid character: X" | Invalid chars (null, control, `<>:"\|?*`) |

### Non-Existent Directory Handling (Move only)

When user enters a path where parent directory doesn't exist:

1. Show warning: "directory 'foo/bar' does not exist"
2. Offer two options via prompt:
   - Press `y` to create the directory and proceed
   - Press `n` (or any other key) to stay in edit mode and change path
3. Validate the path is valid before offering to create (no invalid chars, within workdir)

---

## User Experience After Implementation

| Key              | Action            |
| ---------------- | ----------------- |
| Left/Right       | Move cursor       |
| Home/End         | Jump to start/end |
| Ctrl+Left/Right  | Move by word      |
| Backspace/Delete | Delete at cursor  |
| Any char         | Insert at cursor  |
