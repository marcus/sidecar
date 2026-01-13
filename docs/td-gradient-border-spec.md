# Gradient Border Theming for TD Plugin in Sidecar

## Overview

Add gradient border support to td monitor that sidecar can inject, maintaining standalone functionality with default lipgloss borders.

## Design

**Injection Pattern**: Function types for renderers + `EmbeddedOptions` struct

- `PanelRenderer func(content string, width, height int, state PanelState) string`
- `ModalRenderer func(content string, width, height int, modalType ModalType, depth int) string`

**Panel States** (5 total): Normal, Active, Hover, DividerHover, DividerActive

**Modal Types** (6 total): Issue, Handoffs, BoardPicker, Form, Confirmation, Stats

---

## Implementation

### Phase 1: TD Monitor Changes (`~/code/td/pkg/monitor`)

#### 1.1 Add Types (`types.go`)

```go
// PanelState represents the visual state of a panel
type PanelState int

const (
    PanelStateNormal PanelState = iota
    PanelStateActive
    PanelStateHover
    PanelStateDividerHover
    PanelStateDividerActive
)

// ModalType represents the type of modal for styling
type ModalType int

const (
    ModalTypeIssue ModalType = iota
    ModalTypeHandoffs
    ModalTypeBoardPicker
    ModalTypeForm
    ModalTypeConfirmation
    ModalTypeStats
)

// PanelRenderer renders content in a bordered panel
type PanelRenderer func(content string, width, height int, state PanelState) string

// ModalRenderer renders content in a modal box
type ModalRenderer func(content string, width, height int, modalType ModalType, depth int) string
```

#### 1.2 Add to Model (`model.go`)

Add fields to Model struct:

```go
PanelRenderer PanelRenderer
ModalRenderer ModalRenderer
```

Add EmbeddedOptions:

```go
type EmbeddedOptions struct {
    BaseDir       string
    Interval      time.Duration
    Version       string
    PanelRenderer PanelRenderer
    ModalRenderer ModalRenderer
}

func NewEmbeddedWithOptions(opts EmbeddedOptions) (*Model, error)
```

#### 1.3 Refactor `wrapPanel()` (`view.go`)

- Add `determinePanelState(panel Panel) PanelState` helper
- Check `m.PanelRenderer != nil` and use it, else fall back to existing lipgloss code

#### 1.4 Refactor Modal Wrappers (`view.go`)

Update these functions to check `m.ModalRenderer`:

- `wrapModalWithDepth()` - use `ModalTypeIssue` with depth
- `wrapHandoffsModal()` - use `ModalTypeHandoffs`
- `wrapBoardPickerModal()` - use `ModalTypeBoardPicker`
- `renderFormModal()` - use `ModalTypeForm`
- `wrapConfirmationModal()` - use `ModalTypeConfirmation`
- `renderStatsModal()` - use `ModalTypeStats`

---

### Phase 2: Sidecar Changes (`~/code/sidecar`)

#### 2.1 Create Adapter (`internal/styles/td_renderers.go`)

New file with:

- `CreateTDPanelRenderer() monitor.PanelRenderer` - maps PanelState to gradients
- `CreateTDModalRenderer() monitor.ModalRenderer` - maps ModalType/depth to gradients

Gradient mappings:
| State/Type | Gradient |
|------------|----------|
| Active | Theme's `GradientBorderActive` (purple→blue) |
| Normal | Theme's `GradientBorderNormal` (dark gray) |
| Hover | Lightened normal |
| DividerHover | Cyan gradient |
| DividerActive | Orange gradient |
| Modal depth 1 | Active gradient |
| Modal depth 2 | Cyan gradient |
| Modal depth 3+ | Orange gradient |
| Handoffs | Green gradient |
| Confirmation | Red gradient |

#### 2.2 Update Plugin (`internal/plugins/tdmonitor/plugin.go`)

In `Init()`, use `NewEmbeddedWithOptions()`:

```go
opts := monitor.EmbeddedOptions{
    BaseDir:       ctx.WorkDir,
    Interval:      pollInterval,
    Version:       "",
    PanelRenderer: styles.CreateTDPanelRenderer(),
    ModalRenderer: styles.CreateTDModalRenderer(),
}
model, err := monitor.NewEmbeddedWithOptions(opts)
```

---

## Files to Modify

| Codebase | File                                   | Action                                                         |
| -------- | -------------------------------------- | -------------------------------------------------------------- |
| td       | `pkg/monitor/types.go`                 | Add PanelState, ModalType, renderer types                      |
| td       | `pkg/monitor/model.go`                 | Add renderer fields, EmbeddedOptions, NewEmbeddedWithOptions() |
| td       | `pkg/monitor/view.go`                  | Refactor wrapPanel() + modal wrappers                          |
| sidecar  | `internal/styles/td_renderers.go`      | **New file** - adapter functions                               |
| sidecar  | `internal/plugins/tdmonitor/plugin.go` | Use NewEmbeddedWithOptions()                                   |

---

## Verification

1. **Standalone TD**: Run `td monitor` - should work with default lipgloss borders
2. **Embedded in Sidecar**: Run `sidecar` - TD panels should show gradient borders
3. **Panel States**:
   - Click panels to test active state
   - Hover mouse over panels
   - Hover/drag dividers between panels
4. **Modal Types**: Open each modal type and verify gradient colors:
   - Issue details (Enter on task)
   - Handoffs modal (H key)
   - Board picker (B key)
   - Form modal (create/edit)
   - Confirmation (delete)
   - Stats modal (S key)
5. **Modal Depth**: Open issue → Tab to epic tasks → Enter to open nested → verify color progression (purple→cyan→orange)
6. **Theme Switch**: Change sidecar theme, verify TD gradients update accordingly
