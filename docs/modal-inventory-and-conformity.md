# Sidecar Modal Inventory & Quality Conformity

This document provides a complete inventory of all modals in Sidecar and tracks their conformity with modal best practices defined in `docs/guides/ui-feature-guide.md` and `docs/spec-modal-library.md`.

## Quality Checklist

Modals should conform to these best practices:

### Core Requirements
- **Overlay Method**: Uses `ui.OverlayModal()` for dimmed background OR has justified use of `lipgloss.Place()` for solid overlay
- **Interactive UI**: Has interactive buttons (not just key hints like "[Enter] Confirm") with normal/focused/hover states
- **Keyboard Navigation**: Tab/Shift+Tab cycling through inputs/buttons, Enter to confirm, Esc to cancel
- **Height Constraints**: Properly constrained to available height with no content overflow

### Mouse Support (for modals with interactive buttons)
- **Hit Regions Registered**: Interactive elements have hit regions in render function
- **Regions Cleared**: Hit regions cleared at start of render (via `p.mouseHandler.Clear()`)
- **Hit Region Accuracy**: Calculations match rendered layout (accounting for borders/padding)
- **Hover State**: Buttons have hover styling, hover tracked separately from focus

### Accessibility
- **Focus Indication**: Focused button clearly styled differently from normal/hover
- **Visual Feedback**: All interactive elements have clear hover/focus states
- **Keyboard Shortcuts**: Clearly indicated via hint text (e.g., "Tab to switch • Enter to confirm")

## Modal Inventory

### Legend
- Y = Fully conforms
- ~ = Partially conforms (notes in Conformity Details)
- N = Does not conform (needs work)
- view-only = View-only modal (different category, see notes)

---

## APP-LEVEL MODALS (internal/app/view.go)

| Modal | Type | Overlay Method | Buttons | Keyboard Nav | Mouse Hit Regions | Hover State | Status |
|-------|------|---|---|---|---|---|---|
| Quit Confirm | Confirmation | Y `ui.OverlayModal()` | Y Styled buttons | Y Tab/Enter/Esc | N Not registered | Y Yes | ~ |
| Command Palette | Search | Y `ui.OverlayModal()` | Y Item selection | Y Arrow keys/Enter | ~ Items only | Y Yes | Y |
| Help Modal | Info | Y `ui.OverlayModal()` | N Key hints only | N Esc only | N No | N No | ~ |
| Diagnostics | Info | Y `ui.OverlayModal()` | N Key hints only | N Esc only | N No | N No | ~ |
| Project Switcher | Selection | Y `ui.OverlayModal()` | N Cursor nav | Y Arrow keys/Enter | ~ Items only | Y Yes | ~ |
| Project Add | Form | Y `ui.OverlayModal()` | Y Tab nav | Y Tab/Enter/Esc | ~ Partial | Y Yes | ~ |
| Theme Switcher | Selection | Y `ui.OverlayModal()` | N Cursor nav | Y Arrow keys/Enter | ~ Items only | Y Yes | ~ |
| Community Browser | Search | Y `ui.OverlayModal()` | ~ Mixed | Y Arrow keys/Enter | N No | Y Yes | ~ |

**App-Level Notes:**
- Quit Confirm has full button styling (normal/focused/hover) but lacks mouse hit regions for buttons
- Help, Diagnostics, and Community Browser are primarily informational (Esc to close)
- Project/Theme switchers use cursor navigation rather than Tab-cycling
- None of the app-level modals have registered mouse hit regions for buttons — all could benefit from this

---

## WORKSPACE PLUGIN MODALS (internal/plugins/workspace/view_modals.go)

| Modal | Type | Overlay Method | Buttons | Keyboard Nav | Mouse Hit Regions | Hover State | Status |
|-------|------|---|---|---|---|---|---|
| Create Worktree | Form | Y `ui.OverlayModal()` | Y Tab cycling | Y Tab/Shift+Tab/Enter/Esc | Y Full | Y Yes | Y |
| Task Link | Dropdown | Y `ui.OverlayModal()` | Y Tab cycling | Y Tab/Arrow/Enter/Esc | Y Full | Y Yes | Y |
| Confirm Delete | Confirmation | Y `ui.OverlayModal()` | Y Tab cycling | Y Tab/Enter/Esc | Y Full | Y Yes | Y |
| Confirm Delete Shell | Confirmation | Y `ui.OverlayModal()` | Y Tab cycling | Y Tab/Enter/Esc | Y Full | Y Yes | Y |
| Rename Shell | Form | Y `ui.OverlayModal()` | Y Tab cycling | Y Tab/Enter/Esc | Y Full | Y Yes | Y |
| Prompt Picker | Selection | Y `ui.OverlayModal()` | N Search/Navigation | Y Arrow/Tab/Enter/Esc | Y Full | Y Yes | Y |
| Agent Choice | Selection | Y `ui.OverlayModal()` | Y Tab cycling | Y Tab/Enter/Esc | Y Full | Y Yes | Y |
| Merge Workflow | Multi-step | Y `ui.OverlayModal()` | Y Tab cycling | Y Tab/Enter/Esc | Y Full | Y Yes | Y |
| Commit for Merge | Form | Y `ui.OverlayModal()` | Y Tab cycling | Y Tab/Enter/Esc | Y Full | Y Yes | Y |
| Type Selector | Selection | Y `ui.OverlayModal()` | Y Tab cycling | Y Tab/Enter/Esc | Y Full | Y Yes | Y |

**Workspace Notes:**
- Highest conformity rate — all modals use `ui.OverlayModal()`
- All have comprehensive mouse support with hit regions
- Complex forms use sophisticated validation and dropdown integration
- Proper height calculations with modal width constraints
- Button Y-position calculations account for borders, padding, and wrapping content

---

## GIT STATUS PLUGIN MODALS

### Confirmation Modals (internal/plugins/gitstatus/confirm_*.go)

| Modal | Type | Overlay Method | Buttons | Keyboard Nav | Mouse Hit Regions | Hover State | Status |
|-------|------|---|---|---|---|---|---|
| Confirm Discard | Confirmation | Y `ui.OverlayModal()` | Y Styled buttons | Y Tab/Enter/Esc | ~ Partial | Y Yes | ~ |
| Confirm Stash Pop | Confirmation | Y `ui.OverlayModal()` | Y Styled buttons | Y Tab/Enter/Esc | ~ Partial | Y Yes | ~ |

### Menu Modals (internal/plugins/gitstatus/pull_menu.go, push_menu.go)

| Modal | Type | Overlay Method | Buttons | Keyboard Nav | Mouse Hit Regions | Hover State | Status |
|-------|------|---|---|---|---|---|---|
| Pull Menu | Selection | Y `ui.OverlayModal()` | N Arrow nav | Y Arrow/Enter/Esc | Y Full | Y Yes | ~ |
| Pull Conflict | Menu | Y `ui.OverlayModal()` | N Arrow nav | Y Arrow/Enter/Esc | ~ Partial | Y Yes | ~ |
| Push Menu | Selection | Y `ui.OverlayModal()` | N Arrow nav | Y Arrow/Enter/Esc | Y Full | Y Yes | ~ |

### Commit & Branch Modals (internal/plugins/gitstatus/commit_view.go, branch_picker.go)

| Modal | Type | Overlay Method | Buttons | Keyboard Nav | Mouse Hit Regions | Hover State | Status |
|-------|------|---|---|---|---|---|---|
| Commit Message | Form | Y `ui.OverlayModal()` | Y Styled button | Y Tab/Enter/Esc | Y Full | Y Yes | Y |
| Branch Picker | Selection | Y `ui.OverlayModal()` | ~ Arrow nav | Y Arrow/Enter/Esc | Y Full | Y Yes | Y |

**Git Status Notes:**
- Commit modal is exemplary: proper height calculation, button hit region tracking, textarea support
- Menu modals (pull/push) use cursor-based selection rather than Tab-cycling
- Confirmation modals have buttons but don't register mouse hit regions
- Good practice: `estimateCommitModalHeight()` and `registerCommitButtonHitRegion()` for accurate layout

---

## FILE BROWSER PLUGIN MODALS (internal/plugins/filebrowser/)

### View-Only Information Modals

| Modal | Type | Category | Overlay Method | Interactive | Focus Support | Status |
|-------|------|---|---|---|---|---|
| Blame View | Info | View-only | N Rendered view | N Read-only | Y Navigation | view-only |
| File Info | Info | View-only | N Rendered view | N Read-only | N No | view-only |
| Project Search | Results | View-only | N Rendered view | ~ Navigation | Y Navigation | view-only |

**File Browser Notes:**
- These are view-only information modals, not action modals
- They don't use `ui.OverlayModal()` — they're rendered as full views with modal styling
- Blame view has scroll navigation (cursor tracking)
- Info modal is static informational display
- Project search is interactive for navigation but not for form input

---

## Summary Statistics

| Category | Total | Fully Conform (Y) | Partially Conform (~) | Need Work (N) |
|----------|-------|---|---|---|
| App-Level | 8 | 1 (12%) | 7 (88%) | 0 |
| Workspace | 10 | 9 (90%) | 1 (10%) | 0 |
| Git Status | 8 | 2 (25%) | 5 (63%) | 1 (12%) |
| File Browser | 3 | 0 (0%) | 0 (0%) | 0 (view-only) |
| **TOTAL** | **29** | **12 (41%)** | **13 (45%)** | **1 (3%)** |

---

## Detailed Conformity Notes

### Fully Conforming Modals (Priority: Maintain)

These modals are excellent examples and should be used as templates:

1. **Workspace: Create Worktree Modal**
   - Exemplary form with Tab-cycling through 6+ fields
   - Comprehensive validation (branch name sanitization)
   - Dropdown autocomplete with dynamic suggestions
   - Fully registered mouse hit regions
   - Proper height constraints with field wrapping
   - See: `internal/plugins/workspace/view_modals.go:renderCreateModal()`

2. **Workspace: Merge Workflow Modal**
   - Complex multi-step workflow
   - Proper state tracking for step progress
   - Full mouse and keyboard support
   - Conditional field visibility

3. **Git Status: Commit Modal**
   - Excellent height estimation: `estimateCommitModalHeight()`
   - Textarea integration with proper sizing
   - Button hit region calculation: `registerCommitButtonHitRegion()`
   - Accounts for wrapped content in staged files display
   - Progress indicator and error display

### Partially Conforming - Category 1: Missing Mouse Hit Regions (Priority: Medium)

These modals have all the UI polish but lack mouse support for buttons:

1. **App: Quit Confirm Modal**
   - Has beautiful button styling with focus/hover states
   - Has keyboard support (Tab/Enter/Esc)
   - **Missing**: Mouse hit regions for buttons
   - **Fix**: Add `mouseHandler.HitMap.AddRect()` for Quit/Cancel buttons
   - Estimated effort: 10 lines of code

2. **Git: Confirm Discard Modal**
   - Similar situation: buttons styled but not clickable
   - **Missing**: Mouse hit regions for buttons
   - **Fix**: Register button regions during render

3. **Git: Confirm Stash Pop Modal**
   - Same pattern as Confirm Discard
   - Needs mouse hit regions

### Partially Conforming - Category 2: Info-Only Modals (Priority: Low)

These modals are informational (Esc to close) and don't need full interactivity:

1. **App: Help Modal**
   - Shows keyboard shortcuts
   - No interactive buttons needed
   - Current design is appropriate

2. **App: Diagnostics Modal**
   - Shows version/debug info
   - No interactive buttons needed
   - Current design is appropriate

3. **App: Community Browser Modal**
   - Allows browsing but limited interaction
   - Uses arrow keys for navigation
   - Could benefit from mouse support for items

### Partially Conforming - Category 3: Selection-Based Navigation (Priority: Low)

These modals use cursor/arrow-key navigation instead of Tab cycling:

1. **App: Project Switcher**
   - Uses arrow keys + Enter pattern
   - Has hover state for items
   - Could add Tab support for consistency

2. **App: Theme Switcher**
   - Same pattern as Project Switcher

3. **Git: Pull Menu & Push Menu**
   - Arrow-key selection between options
   - Simpler interaction model (appropriate for 2-3 options)

### Needs Work

1. **Git: Pull Conflict Modal** (1 of 29)
   - Partial mouse support
   - Could improve button region registration

---

## Improvement Roadmap

### Phase 1: Quick Wins (Low Effort, High Impact)
**Estimated: 2-3 hours**

Add mouse hit regions to confirmation modals:
1. `internal/app/view.go` - Quit Confirm modal (10 lines)
2. `internal/plugins/gitstatus/confirm_discard.go` - Add button regions (15 lines)
3. `internal/plugins/gitstatus/confirm_stash_pop.go` - Add button regions (15 lines)

These already have perfect styling and keyboard support — just add clickability.

### Phase 2: Navigation Consistency (Medium Effort)
**Estimated: 4-6 hours**

Extend Tab-cycling support to selection-based modals:
1. **App: Project Switcher** — Add Tab to cycle items + buttons
2. **App: Theme Switcher** — Add Tab to cycle items + buttons
3. **Git: Pull Menu** — Add Tab to cycle options (optional, arrow keys work well)

This improves keyboard accessibility for users who prefer Tab-based navigation.

### Phase 3: Info Modal Enhancement (Low Priority)
**Estimated: 6-8 hours**

Upgrade view-only modals to support interaction:
1. **File Browser: Blame View** — Add mouse clickability for line selection
2. **File Browser: Project Search** — Add Tab/arrow navigation for results

These work fine as-is but could be more interactive.

### Phase 4: App-Level Modals Polish (Medium Priority)
**Estimated: 8-10 hours**

Full Tab-cycling support for app modals:
1. Command Palette — Already excellent, maintain
2. Help Modal — Could add Tab-cycling through sections (nice-to-have)
3. Community Browser — Add better Tab navigation

---

## Common Patterns & Recommendations

### For New Modals: Use This Template

```go
// Workspace example - best practices
func (p *Plugin) renderMyNewModal(width, height int) string {
    // 1. Render background
    background := p.renderListView(width, height)

    // 2. Calculate modal dimensions with constraints
    modalW := 70
    if modalW > width - 4 { modalW = width - 4 }

    // 3. Build content with proper structure
    var sb strings.Builder
    // ... render content ...

    // 4. Wrap in style
    modal := styles.ModalBox.Render(sb.String())

    // 5. Register mouse hit regions
    p.registerMyNewModalHitRegions(width, height, modalW)

    // 6. Return overlay
    return ui.OverlayModal(background, modal, width, height)
}
```

### For App-Level Modals: Quick Mouse Support

App modals need to register hit regions in the main Model:

```go
// In app/view.go or separate app/mouse_modals.go
func (m Model) registerQuitConfirmHitRegions(width, height int) {
    if m.activeModal() != ModalQuitConfirm {
        return // Only register when modal is active
    }

    // Calculate modal center position
    modalW := 40
    startX := (width - modalW) / 2
    startY := (height - 10) / 2

    // Register button regions
    m.mouseHandler.HitMap.AddRect("quit-button", startX+2, startY+4, 8, 1, nil)
    m.mouseHandler.HitMap.AddRect("cancel-button", startX+12, startY+4, 8, 1, nil)
}
```

### Border + Padding Offset Rule

When calculating hit regions for content inside a modal with borders and padding:

```
Modal structure:
├─ Border (1 line top)
├─ Padding (1 line vertical)
└─ Content starts here (at Y = modalStartY + 2)

If using: Border(lipgloss.RoundedBorder()) + Padding(1, 2)
Content Y offset = +2 (not +1!)
```

### Text Wrapping in Hit Regions

When content may wrap (file paths, descriptions):

```go
contentWidth := modalW - 6 // Account for border(2) + padding(4)
pathWidth := ansi.StringWidth(pathLine)
pathLineCount := (pathWidth + contentWidth - 1) / contentWidth
currentY += pathLineCount  // Not just +1!
```

---

## Testing Modal Conformity

### Keyboard Testing Checklist
- [ ] Tab moves focus between elements
- [ ] Shift+Tab moves focus backward
- [ ] Enter executes focused button
- [ ] Esc cancels modal
- [ ] Arrow keys work for list/menu items (if applicable)

### Mouse Testing Checklist
- [ ] Click buttons to trigger actions
- [ ] Hover shows visual feedback
- [ ] Focus takes precedence over hover
- [ ] Scroll wheel works in scrollable areas (if applicable)
- [ ] Double-click works for items (if applicable)

### Visual Testing Checklist
- [ ] Modal fits within available height
- [ ] Content doesn't overflow
- [ ] Buttons are clearly visible
- [ ] Focused state is obvious
- [ ] Hover state is obvious
- [ ] Disabled state (if any) is obvious

---

## Future Improvements & Suggestions

### 1. Modal Registry System
Create a `internal/modal/registry.go` that:
- Tracks all active modals
- Auto-registers/clears hit regions
- Provides helpers for common modal patterns
- Benefits: Consistency, easier testing, fewer bugs

### 2. Modal Testing Helper
Add `internal/modal/testing.go` with:
- Mock modal renderer
- Hit region validator
- Keyboard nav tester
- Mouse click simulator
- Benefits: Automated conformity testing, catches regressions

### 3. Modal Theme Customization
Extend `internal/styles/` to support:
- Custom modal width/height percentages per theme
- Font size scaling for modal content
- Button style variants
- Benefits: Better accessibility, theme flexibility

### 4. Accessibility Audit
Consider adding:
- ARIA labels (if terminal supports them)
- Keyboard shortcut hints in modals
- High-contrast mode support
- Larger text option for info modals

### 5. Modal Animation System
Optional enhancement:
- Fade in/out transitions
- Slide transitions
- Stack tracking for nested modals
- Benefits: More polished UX, better visual feedback

---

## Files Referenced

**Best Practice Guides:**
- `docs/guides/ui-feature-guide.md` — UI feature guide (modals, shortcuts, mouse)
- `docs/spec-modal-library.md` — Modal library spec

**Implementation Examples:**
- `internal/ui/overlay.go` — `OverlayModal()` function
- `internal/app/view.go` — App-level modals
- `internal/plugins/workspace/view_modals.go` — Best-practice modals
- `internal/plugins/gitstatus/commit_view.go` — Exemplary form modal
- `internal/plugins/filebrowser/view_*.go` — View-only modals

**Mouse Support:**
- `internal/mouse/` — Hit map and handler implementation
- `internal/plugins/workspace/mouse.go` — Mouse handler example
- `internal/plugins/gitstatus/mouse.go` — Menu modal mouse support

---

## Questions & Contacts

For questions about modal implementation:
- See `CLAUDE.md` for project context and conventions
- Review existing similar modals for patterns
- Use `internal/plugins/workspace/` as the gold standard

For issues or suggestions:
- Create a task with `td` for tracking
- Reference this document for context
- Include test case (modal name + steps)
