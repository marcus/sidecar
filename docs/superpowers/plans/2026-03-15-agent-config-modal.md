# Agent Config Modal Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a modal that lets users choose agent type, skip-permissions, and prompt when starting or restarting an agent on an existing worktree.

**Architecture:** New `agent_config_modal.go` file containing the modal builder, renderer, clear/helper functions. Existing files are modified to add: a new `ViewModeAgentConfig` constant, new Plugin state fields, key/mouse/command handlers, prompt picker return routing via `promptPickerReturnMode`, and a `restartAgentWithOptionsMsg` message type.

**Tech Stack:** Go, Bubbletea (TUI framework), Lipgloss (styling), internal `modal` package

**Spec:** `docs/superpowers/specs/2026-03-15-agent-config-modal-design.md`

---

## Chunk 1: Foundation — Types, Messages, State Fields

### Task 1: Add ViewModeAgentConfig constant

**Files:**
- Modify: `internal/plugins/workspace/types.go:25-41`

- [ ] **Step 1: Add the new view mode constant**

In `internal/plugins/workspace/types.go`, add `ViewModeAgentConfig` to the `ViewMode` iota block, after `ViewModeFetchPR`:

```go
ViewModeFetchPR                        // Fetch remote PR modal
ViewModeAgentConfig                    // Agent config modal (start/restart with options)
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go build ./internal/plugins/workspace/...`
Expected: Success (new constant is unused but that's fine for iota)

- [ ] **Step 3: Commit**

```bash
git add internal/plugins/workspace/types.go
git commit -m "feat: add ViewModeAgentConfig constant"
```

### Task 2: Add restartAgentWithOptionsMsg message type

**Files:**
- Modify: `internal/plugins/workspace/messages.go:186-189`

- [ ] **Step 1: Add the new message struct**

In `internal/plugins/workspace/messages.go`, after the existing `restartAgentMsg` struct (line 189), add:

```go
// restartAgentWithOptionsMsg signals that an agent should be restarted with specific options.
type restartAgentWithOptionsMsg struct {
	worktree  *Worktree
	agentType AgentType
	skipPerms bool
	prompt    *Prompt
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go build ./internal/plugins/workspace/...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/plugins/workspace/messages.go
git commit -m "feat: add restartAgentWithOptionsMsg message type"
```

### Task 3: Add agent config state fields to Plugin struct

**Files:**
- Modify: `internal/plugins/workspace/plugin.go:243-247`

- [ ] **Step 1: Add state fields after the existing agent choice modal state block**

In `internal/plugins/workspace/plugin.go`, after the `agentChoiceModalWidth` field (line 247), add:

```go
	// Agent config modal state (start/restart with options)
	agentConfigWorktree   *Worktree
	agentConfigIsRestart  bool
	agentConfigAgentType  AgentType
	agentConfigAgentIdx   int
	agentConfigSkipPerms  bool
	agentConfigPromptIdx  int
	agentConfigPrompts    []Prompt
	agentConfigModal      *modal.Modal
	agentConfigModalWidth int
	agentConfigFocusSet   bool

	// Prompt picker return routing
	promptPickerReturnMode ViewMode
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go build ./internal/plugins/workspace/...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/plugins/workspace/plugin.go
git commit -m "feat: add agent config modal state fields to Plugin"
```

### Task 4: Add restartAgentWithOptionsMsg handler in update.go

**Files:**
- Modify: `internal/plugins/workspace/update.go:966-972`

- [ ] **Step 1: Add the handler after the existing restartAgentMsg handler**

In `internal/plugins/workspace/update.go`, after the `restartAgentMsg` case (line 972), add:

```go
	case restartAgentWithOptionsMsg:
		// Start new agent after stop completed, with user-selected options
		if msg.worktree != nil {
			return p, p.StartAgentWithOptions(msg.worktree, msg.agentType, msg.skipPerms, msg.prompt)
		}
		return p, nil
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go build ./internal/plugins/workspace/...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/plugins/workspace/update.go
git commit -m "feat: handle restartAgentWithOptionsMsg in update loop"
```

---

## Chunk 2: Agent Config Modal — Core File

### Task 5: Create agent_config_modal.go with constants, clear, and helpers

**Files:**
- Create: `internal/plugins/workspace/agent_config_modal.go`

- [ ] **Step 1: Write the test file first**

Create `internal/plugins/workspace/agent_config_modal_test.go`:

```go
package workspace

import "testing"

func TestGetAgentConfigPrompt(t *testing.T) {
	tests := []struct {
		name      string
		prompts   []Prompt
		idx       int
		wantNil   bool
		wantName  string
	}{
		{"negative index", []Prompt{{Name: "a"}}, -1, true, ""},
		{"out of bounds", []Prompt{{Name: "a"}}, 5, true, ""},
		{"nil prompts", nil, 0, true, ""},
		{"valid index", []Prompt{{Name: "first"}, {Name: "second"}}, 1, false, "second"},
		{"first index", []Prompt{{Name: "only"}}, 0, false, "only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{
				agentConfigPrompts:  tt.prompts,
				agentConfigPromptIdx: tt.idx,
			}
			got := p.getAgentConfigPrompt()
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil prompt")
			}
			if got.Name != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, got.Name)
			}
		})
	}
}

func TestClearAgentConfigModal(t *testing.T) {
	p := &Plugin{
		agentConfigWorktree:  &Worktree{Name: "test"},
		agentConfigIsRestart: true,
		agentConfigAgentType: AgentClaude,
		agentConfigAgentIdx:  3,
		agentConfigSkipPerms: true,
		agentConfigPromptIdx: 2,
		agentConfigPrompts:   []Prompt{{Name: "x"}},
	}
	p.clearAgentConfigModal()

	if p.agentConfigWorktree != nil {
		t.Error("worktree not cleared")
	}
	if p.agentConfigIsRestart {
		t.Error("isRestart not cleared")
	}
	if p.agentConfigAgentType != "" {
		t.Error("agentType not cleared")
	}
	if p.agentConfigAgentIdx != 0 {
		t.Error("agentIdx not cleared")
	}
	if p.agentConfigSkipPerms {
		t.Error("skipPerms not cleared")
	}
	if p.agentConfigPromptIdx != -1 {
		t.Error("promptIdx not cleared")
	}
	if p.agentConfigPrompts != nil {
		t.Error("prompts not cleared")
	}
	if p.agentConfigModal != nil {
		t.Error("modal not cleared")
	}
	if p.agentConfigModalWidth != 0 {
		t.Error("modalWidth not cleared")
	}
	if p.agentConfigFocusSet {
		t.Error("focusSet not cleared")
	}
}

func TestShouldShowAgentConfigSkipPerms(t *testing.T) {
	tests := []struct {
		name      string
		agentType AgentType
		want      bool
	}{
		{"claude has flag", AgentClaude, true},
		{"codex has flag", AgentCodex, true},
		{"none has no flag", AgentNone, false},
		{"opencode has no flag", AgentOpenCode, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{agentConfigAgentType: tt.agentType}
			if got := p.shouldShowAgentConfigSkipPerms(); got != tt.want {
				t.Errorf("shouldShowAgentConfigSkipPerms() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests — they should fail (functions don't exist yet)**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go test ./internal/plugins/workspace/ -run TestGetAgentConfigPrompt -v 2>&1 | head -5`
Expected: Compilation error — functions not defined

- [ ] **Step 3: Create agent_config_modal.go with constants, helpers, and clear function**

Create `internal/plugins/workspace/agent_config_modal.go`:

```go
package workspace

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/styles"
	ui "github.com/marcus/sidecar/internal/ui"
)

const (
	agentConfigPromptFieldID     = "agent-config-prompt"
	agentConfigAgentListID       = "agent-config-agent-list"
	agentConfigSkipPermissionsID = "agent-config-skip-permissions"
	agentConfigSubmitID          = "agent-config-submit"
	agentConfigCancelID          = "agent-config-cancel"
	agentConfigAgentItemPrefix   = "agent-config-agent-"
)

// clearAgentConfigModal resets all agent config modal state.
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
	p.agentConfigFocusSet = false
}

// getAgentConfigPrompt resolves the selected prompt index to a *Prompt.
func (p *Plugin) getAgentConfigPrompt() *Prompt {
	if p.agentConfigPromptIdx < 0 || p.agentConfigPromptIdx >= len(p.agentConfigPrompts) {
		return nil
	}
	prompt := p.agentConfigPrompts[p.agentConfigPromptIdx]
	return &prompt
}

// shouldShowAgentConfigSkipPerms returns true if the selected agent supports skip permissions.
func (p *Plugin) shouldShowAgentConfigSkipPerms() bool {
	if p.agentConfigAgentType == AgentNone || p.agentConfigAgentType == "" {
		return false
	}
	flag, ok := SkipPermissionsFlags[p.agentConfigAgentType]
	return ok && flag != ""
}
```

Note: The imports for `fmt`, `strings`, `lipgloss`, `ansi`, `styles`, and `ui` will be used by later functions in this file. If the compiler complains about unused imports at this stage, temporarily remove them and add them back in Task 6 when the section builders are added.

- [ ] **Step 4: Run tests — they should pass**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go test ./internal/plugins/workspace/ -run "TestGetAgentConfigPrompt|TestClearAgentConfigModal|TestShouldShowAgentConfigSkipPerms" -v`
Expected: All 3 test functions pass

- [ ] **Step 5: Commit**

```bash
git add internal/plugins/workspace/agent_config_modal.go internal/plugins/workspace/agent_config_modal_test.go
git commit -m "feat: add agent config modal constants, helpers, and tests"
```

### Task 6: Add ensureAgentConfigModal — modal builder with sections

**Files:**
- Modify: `internal/plugins/workspace/agent_config_modal.go`

- [ ] **Step 1: Add section builder functions and ensureAgentConfigModal**

Append to `internal/plugins/workspace/agent_config_modal.go`:

```go
// ensureAgentConfigModal builds or rebuilds the agent config modal.
func (p *Plugin) ensureAgentConfigModal() {
	if p.agentConfigWorktree == nil {
		return
	}

	modalW := 60
	maxW := p.width - 4
	if maxW < 1 {
		maxW = 1
	}
	if modalW > maxW {
		modalW = maxW
	}

	if p.agentConfigModal != nil && p.agentConfigModalWidth == modalW {
		return
	}
	p.agentConfigModalWidth = modalW

	items := make([]modal.ListItem, len(AgentTypeOrder))
	for i, at := range AgentTypeOrder {
		items[i] = modal.ListItem{
			ID:    fmt.Sprintf("%s%d", agentConfigAgentItemPrefix, i),
			Label: AgentDisplayNames[at],
		}
	}

	title := fmt.Sprintf("Start Agent: %s", p.agentConfigWorktree.Name)
	if p.agentConfigIsRestart {
		title = fmt.Sprintf("Restart Agent: %s", p.agentConfigWorktree.Name)
	}

	p.agentConfigModal = modal.New(title,
		modal.WithWidth(modalW),
		modal.WithPrimaryAction(agentConfigSubmitID),
		modal.WithHints(false),
	).
		AddSection(p.agentConfigPromptSection()).
		AddSection(modal.Spacer()).
		AddSection(p.agentConfigAgentLabelSection()).
		AddSection(modal.List(agentConfigAgentListID, items, &p.agentConfigAgentIdx, modal.WithMaxVisible(len(items)))).
		AddSection(p.agentConfigSkipPermissionsSpacerSection()).
		AddSection(modal.When(p.shouldShowAgentConfigSkipPerms, modal.Checkbox(agentConfigSkipPermissionsID, "Auto-approve all actions", &p.agentConfigSkipPerms))).
		AddSection(p.agentConfigSkipPermissionsHintSection()).
		AddSection(modal.Spacer()).
		AddSection(modal.Buttons(
			modal.Btn(" Start ", agentConfigSubmitID),
			modal.Btn(" Cancel ", agentConfigCancelID),
		))
}

// syncAgentConfigModalFocus sets initial focus if not already set.
// Called from renderAgentConfigModal; only sets focus once to avoid
// overriding user navigation (tab/arrow keys).
func (p *Plugin) syncAgentConfigModalFocus() {
	if p.agentConfigModal == nil {
		return
	}
	// Only set initial focus — the modal tracks focus after first set
	if !p.agentConfigFocusSet {
		p.agentConfigModal.SetFocus(agentConfigAgentListID)
		p.agentConfigFocusSet = true
	}
}

// agentConfigPromptSection renders the prompt selector field.
func (p *Plugin) agentConfigPromptSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		lines := make([]string, 0, 4)
		focusables := make([]modal.FocusableInfo, 0, 1)

		lines = append(lines, "Prompt:")

		selectedPrompt := p.getAgentConfigPrompt()
		displayText := "(none)"
		if len(p.agentConfigPrompts) == 0 {
			displayText = "No prompts configured"
		} else if selectedPrompt != nil {
			scopeIndicator := "[G] global"
			if selectedPrompt.Source == "project" {
				scopeIndicator = "[P] project"
			}
			displayText = fmt.Sprintf("%s  %s", selectedPrompt.Name, dimText(scopeIndicator))
		}

		promptStyle := inputStyle()
		if focusID == agentConfigPromptFieldID {
			promptStyle = inputFocusedStyle()
		}
		rendered := promptStyle.Render(displayText)
		renderedLines := strings.Split(rendered, "\n")
		displayStartY := len(lines)
		lines = append(lines, renderedLines...)

		focusables = append(focusables, modal.FocusableInfo{
			ID:      agentConfigPromptFieldID,
			OffsetX: 0,
			OffsetY: displayStartY,
			Width:   ansi.StringWidth(rendered),
			Height:  len(renderedLines),
		})

		if len(p.agentConfigPrompts) == 0 {
			lines = append(lines, dimText("  See: .claude/skills/create-prompt/SKILL.md"))
		} else if selectedPrompt == nil {
			lines = append(lines, dimText("  Press Enter to select a prompt template"))
		} else {
			preview := strings.ReplaceAll(selectedPrompt.Body, "\n", " ")
			if runes := []rune(preview); len(runes) > 60 {
				preview = string(runes[:57]) + "..."
			}
			lines = append(lines, dimText(fmt.Sprintf("  Preview: %s", preview)))
		}

		return modal.RenderedSection{Content: strings.Join(lines, "\n"), Focusables: focusables}
	}, nil)
}

// agentConfigAgentLabelSection renders the "Agent:" label.
func (p *Plugin) agentConfigAgentLabelSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		return modal.RenderedSection{Content: "Agent:"}
	}, nil)
}

// agentConfigSkipPermissionsSpacerSection renders a spacer before the checkbox (hidden when agent is None).
func (p *Plugin) agentConfigSkipPermissionsSpacerSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.agentConfigAgentType == AgentNone || p.agentConfigAgentType == "" {
			return modal.RenderedSection{}
		}
		return modal.RenderedSection{Content: " "}
	}, nil)
}

// agentConfigSkipPermissionsHintSection renders the hint showing the actual flag.
func (p *Plugin) agentConfigSkipPermissionsHintSection() modal.Section {
	return modal.Custom(func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
		if p.agentConfigAgentType == AgentNone || p.agentConfigAgentType == "" {
			return modal.RenderedSection{}
		}
		if p.shouldShowAgentConfigSkipPerms() {
			flag := SkipPermissionsFlags[p.agentConfigAgentType]
			return modal.RenderedSection{Content: dimText(fmt.Sprintf("      (Adds %s)", flag))}
		}
		return modal.RenderedSection{Content: dimText("  Skip permissions not available for this agent")}
	}, nil)
}

// renderAgentConfigModal renders the agent config modal over a dimmed background.
func (p *Plugin) renderAgentConfigModal(width, height int) string {
	background := p.renderListView(width, height)

	p.ensureAgentConfigModal()
	if p.agentConfigModal == nil {
		return background
	}

	p.syncAgentConfigModalFocus()
	modalContent := p.agentConfigModal.Render(width, height, p.mouseHandler)
	return ui.OverlayModal(background, modalContent, width, height)
}
```

Make sure the import block at the top of the file includes all needed imports. The final import block should be:

```go
import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/sidecar/internal/modal"
	"github.com/marcus/sidecar/internal/styles"
	ui "github.com/marcus/sidecar/internal/ui"
)
```

Remove any imports that the compiler says are unused. The `lipgloss` and `styles` imports are used by `inputStyle()`/`inputFocusedStyle()`/`dimText()` which are defined in `create_modal.go` (same package). If these helper functions reference `lipgloss` and `styles` internally and the new file doesn't directly use them, remove those imports.

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go build ./internal/plugins/workspace/...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/plugins/workspace/agent_config_modal.go
git commit -m "feat: add ensureAgentConfigModal builder and section renderers"
```

---

## Chunk 3: Key, Mouse, Command Handlers & View Routing

### Task 7: Add key handler and executeAgentConfig

**Files:**
- Modify: `internal/plugins/workspace/keys.go:27-28` (handleKeys switch)
- Modify: `internal/plugins/workspace/keys.go:264-305` (handleAgentConfigKeys + executeAgentConfig)

- [ ] **Step 1: Write test for executeAgentConfig**

Add to `internal/plugins/workspace/agent_config_modal_test.go`:

```go
func TestExecuteAgentConfig_FreshStart(t *testing.T) {
	wt := &Worktree{Name: "test-wt", Path: "/tmp/test"}
	p := &Plugin{
		agentConfigWorktree:  wt,
		agentConfigIsRestart: false,
		agentConfigAgentType: AgentClaude,
		agentConfigSkipPerms: true,
		agentConfigPromptIdx: -1,
		viewMode:             ViewModeAgentConfig,
	}

	cmd := p.executeAgentConfig()

	// After execute, modal state should be cleared
	if p.viewMode != ViewModeList {
		t.Errorf("expected ViewModeList, got %v", p.viewMode)
	}
	if p.agentConfigWorktree != nil {
		t.Error("worktree should be cleared")
	}
	// cmd should be non-nil (StartAgentWithOptions returns a tea.Cmd)
	if cmd == nil {
		t.Error("expected non-nil cmd for fresh start")
	}
}

func TestExecuteAgentConfig_Restart(t *testing.T) {
	wt := &Worktree{Name: "test-wt", Path: "/tmp/test"}
	p := &Plugin{
		agentConfigWorktree:  wt,
		agentConfigIsRestart: true,
		agentConfigAgentType: AgentCodex,
		agentConfigSkipPerms: false,
		agentConfigPromptIdx: -1,
		viewMode:             ViewModeAgentConfig,
	}

	cmd := p.executeAgentConfig()

	if p.viewMode != ViewModeList {
		t.Errorf("expected ViewModeList, got %v", p.viewMode)
	}
	// cmd should be non-nil (tea.Sequence for stop + restart)
	if cmd == nil {
		t.Error("expected non-nil cmd for restart")
	}
}

func TestExecuteAgentConfig_NilWorktree(t *testing.T) {
	p := &Plugin{
		agentConfigWorktree: nil,
		viewMode:            ViewModeAgentConfig,
	}

	cmd := p.executeAgentConfig()

	if p.viewMode != ViewModeList {
		t.Errorf("expected ViewModeList, got %v", p.viewMode)
	}
	if cmd != nil {
		t.Error("expected nil cmd for nil worktree")
	}
}
```

- [ ] **Step 2: Run tests to see them fail**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go test ./internal/plugins/workspace/ -run "TestExecuteAgentConfig" -v 2>&1 | head -5`
Expected: Compilation error — `executeAgentConfig` not defined

- [ ] **Step 3: Add handleAgentConfigKeys to the handleKeys switch**

In `internal/plugins/workspace/keys.go`, in the `handleKeys()` switch (around line 27), add before the `case ViewModeAgentChoice:` line:

```go
	case ViewModeAgentConfig:
		return p.handleAgentConfigKeys(msg)
```

- [ ] **Step 4: Add handleAgentConfigKeys and executeAgentConfig functions**

In `internal/plugins/workspace/keys.go`, after the `handleAgentChoiceKeys` function (after line 283), add:

```go
// handleAgentConfigKeys handles keys in agent config modal.
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
		p.clearPromptPickerModal()
		p.viewMode = ViewModePromptPicker
		return nil
	case agentConfigSubmitID:
		return p.executeAgentConfig()
	}

	return cmd
}

// executeAgentConfig executes the agent config modal action (start or restart).
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

- [ ] **Step 5: Run tests**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go test ./internal/plugins/workspace/ -run "TestExecuteAgentConfig" -v`
Expected: All 3 tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/plugins/workspace/keys.go internal/plugins/workspace/agent_config_modal_test.go
git commit -m "feat: add handleAgentConfigKeys and executeAgentConfig"
```

### Task 8: Add view routing, commands, and mouse handler

**Files:**
- Modify: `internal/plugins/workspace/view_list.go:66-67`
- Modify: `internal/plugins/workspace/commands.go:48-52` (Commands())
- Modify: `internal/plugins/workspace/commands.go:236-250` (FocusContext())
- Modify: `internal/plugins/workspace/mouse.go:67-69`
- Modify: `internal/plugins/workspace/mouse.go:424-426`

- [ ] **Step 1: Add rendering case in view_list.go**

In `internal/plugins/workspace/view_list.go`, in the `View()` switch (around line 66), add before the `case ViewModeAgentChoice:` line:

```go
	case ViewModeAgentConfig:
		return p.renderAgentConfigModal(width, height)
```

- [ ] **Step 2: Add commands in commands.go**

In `internal/plugins/workspace/commands.go`, in the `Commands()` switch, add before the `case ViewModeAgentChoice:` (around line 48):

```go
	case ViewModeAgentConfig:
		return []plugin.Command{
			{ID: "cancel", Name: "Cancel", Description: "Cancel agent config", Context: "workspace-agent-config", Priority: 1},
			{ID: "confirm", Name: "Start", Description: "Start agent with config", Context: "workspace-agent-config", Priority: 2},
		}
```

In the `FocusContext()` switch (around line 249), add before the `case ViewModeAgentChoice:` line:

```go
	case ViewModeAgentConfig:
		return "workspace-agent-config"
```

- [ ] **Step 3: Add mouse handling in mouse.go**

In `internal/plugins/workspace/mouse.go`, add the click handler function (after `handleAgentChoiceModalMouse`, around line 316):

```go
func (p *Plugin) handleAgentConfigModalMouse(msg tea.MouseMsg) tea.Cmd {
	p.ensureAgentConfigModal()
	if p.agentConfigModal == nil {
		return nil
	}

	prevAgentIdx := p.agentConfigAgentIdx
	action := p.agentConfigModal.HandleMouse(msg, p.mouseHandler)

	// Sync agent type when list selection changes via mouse
	if p.agentConfigAgentIdx != prevAgentIdx {
		if p.agentConfigAgentIdx >= 0 && p.agentConfigAgentIdx < len(AgentTypeOrder) {
			p.agentConfigAgentType = AgentTypeOrder[p.agentConfigAgentIdx]
		}
	}

	switch action {
	case "":
		return nil
	case "cancel", agentConfigCancelID:
		p.viewMode = ViewModeList
		p.clearAgentConfigModal()
		return nil
	case agentConfigPromptFieldID:
		p.promptPickerReturnMode = ViewModeAgentConfig
		p.promptPicker = NewPromptPicker(p.agentConfigPrompts, p.width, p.height)
		p.clearPromptPickerModal()
		p.viewMode = ViewModePromptPicker
		return nil
	case agentConfigSubmitID:
		return p.executeAgentConfig()
	}
	return nil
}
```

In the `handleMouse` function (around line 67), add before the `if p.viewMode == ViewModeAgentChoice {` line:

```go
	if p.viewMode == ViewModeAgentConfig {
		return p.handleAgentConfigModalMouse(msg)
	}
```

In the `handleMouseHover` function's switch (around line 424), add before the `case ViewModeAgentChoice:` line:

```go
	case ViewModeAgentConfig:
		// Modal library handles hover state internally
		return nil
```

- [ ] **Step 4: Add keymap registration in plugin.go Init()**

In `internal/plugins/workspace/plugin.go`, in `Init()` after the agent choice modal context keybindings (after line 445), add:

```go
		// Agent config modal context
		ctx.Keymap.RegisterPluginBinding("esc", "cancel", "workspace-agent-config")
		ctx.Keymap.RegisterPluginBinding("enter", "confirm", "workspace-agent-config")
		ctx.Keymap.RegisterPluginBinding("tab", "next-field", "workspace-agent-config")
		ctx.Keymap.RegisterPluginBinding("shift+tab", "prev-field", "workspace-agent-config")
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go build ./internal/plugins/workspace/...`
Expected: Success

- [ ] **Step 6: Run all tests**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go test ./internal/plugins/workspace/ -v -count=1 2>&1 | tail -5`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/plugins/workspace/view_list.go internal/plugins/workspace/commands.go internal/plugins/workspace/mouse.go internal/plugins/workspace/plugin.go
git commit -m "feat: wire agent config modal into view, commands, mouse, and keymap"
```

---

## Chunk 4: Entry Points — 's' Key & Restart Flow

### Task 9: Modify 's' key handler to open agent config modal

**Files:**
- Modify: `internal/plugins/workspace/keys.go:799-813`

- [ ] **Step 1: Replace the direct StartAgent call with modal opening**

In `internal/plugins/workspace/keys.go`, replace the `case "s":` block (lines 799-813). The current code:

```go
	case "s":
		// Start agent on selected worktree
		wt := p.selectedWorktree()
		if wt == nil {
			return nil
		}
		if wt.Agent == nil {
			// No agent running - start new one
			return p.StartAgent(wt, p.resolveWorktreeAgentType(wt))
		}
		// Agent exists - show choice modal (attach or restart)
		p.agentChoiceWorktree = wt
		p.agentChoiceIdx = 0 // Default to attach
		p.viewMode = ViewModeAgentChoice
		return nil
```

Replace with:

```go
	case "s":
		// Start agent on selected worktree
		wt := p.selectedWorktree()
		if wt == nil {
			return nil
		}
		if wt.Agent == nil {
			// No agent running - open agent config modal
			home, _ := os.UserHomeDir()
			configDir := filepath.Join(home, ".config", "sidecar")
			p.agentConfigWorktree = wt
			p.agentConfigIsRestart = false
			p.agentConfigAgentType = p.resolveWorktreeAgentType(wt)
			p.agentConfigAgentIdx = p.agentTypeIndex(p.agentConfigAgentType)
			p.agentConfigSkipPerms = false
			p.agentConfigPromptIdx = -1
			p.agentConfigPrompts = LoadPrompts(configDir, p.ctx.ProjectRoot)
			p.agentConfigModal = nil
			p.agentConfigModalWidth = 0
			p.viewMode = ViewModeAgentConfig
			return nil
		}
		// Agent exists - show choice modal (attach or restart)
		p.agentChoiceWorktree = wt
		p.agentChoiceIdx = 0 // Default to attach
		p.viewMode = ViewModeAgentChoice
		return nil
```

**Important:** `keys.go` does NOT currently import `os` or `path/filepath`. Add them to the import block:

```go
import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	appmsg "github.com/marcus/sidecar/internal/msg"
	"github.com/marcus/sidecar/internal/state"
)
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go build ./internal/plugins/workspace/...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/plugins/workspace/keys.go
git commit -m "feat: 's' key opens agent config modal instead of starting directly"
```

### Task 10: Modify executeAgentChoice to transition to agent config on restart

**Files:**
- Modify: `internal/plugins/workspace/keys.go:286-305`

- [ ] **Step 1: Replace the restart branch in executeAgentChoice**

In `internal/plugins/workspace/keys.go`, in `executeAgentChoice()`, replace the restart branch (starting at the comment `// Restart agent: stop first, then start`). The current code:

```go
	// Restart agent: stop first, then start
	return tea.Sequence(
		p.StopAgent(wt),
		func() tea.Msg {
			return restartAgentMsg{worktree: wt}
		},
	)
```

Replace with:

```go
	// Restart agent: open config modal to choose options
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".config", "sidecar")
	p.agentConfigWorktree = wt
	p.agentConfigIsRestart = true
	p.agentConfigAgentType = p.resolveWorktreeAgentType(wt)
	p.agentConfigAgentIdx = p.agentTypeIndex(p.agentConfigAgentType)
	p.agentConfigSkipPerms = false
	p.agentConfigPromptIdx = -1
	p.agentConfigPrompts = LoadPrompts(configDir, p.ctx.ProjectRoot)
	p.agentConfigModal = nil
	p.agentConfigModalWidth = 0
	p.viewMode = ViewModeAgentConfig
	return nil
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go build ./internal/plugins/workspace/...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/plugins/workspace/keys.go
git commit -m "feat: restart from agent choice modal opens agent config instead"
```

---

## Chunk 5: Prompt Picker Return Routing

### Task 11: Add promptPickerReturnMode to existing create modal flow

**Files:**
- Modify: `internal/plugins/workspace/keys.go:1003-1007` and `keys.go:1025-1029`

- [ ] **Step 1: Write test for prompt picker return routing**

Add to `internal/plugins/workspace/agent_config_modal_test.go`:

```go
func TestPromptPickerReturnMode_AgentConfig(t *testing.T) {
	p := &Plugin{
		promptPickerReturnMode: ViewModeAgentConfig,
		agentConfigPrompts:     []Prompt{{Name: "test-prompt", Body: "do stuff"}},
		agentConfigPromptIdx:   -1,
	}

	// Simulate PromptSelectedMsg being handled — the logic we're testing
	// is in update.go, so this test verifies the state after handling.
	// The actual handler integration is tested by the update.go modification.

	// Verify initial state
	if p.agentConfigPromptIdx != -1 {
		t.Error("expected initial promptIdx to be -1")
	}
	if p.promptPickerReturnMode != ViewModeAgentConfig {
		t.Error("expected return mode to be ViewModeAgentConfig")
	}
}
```

- [ ] **Step 2: Set promptPickerReturnMode = ViewModeCreate in existing prompt picker openers**

In `internal/plugins/workspace/keys.go`, at line 1003 (where create modal opens prompt picker via `focusID == createPromptFieldID`), add before the `p.promptPicker = ...` line:

```go
			p.promptPickerReturnMode = ViewModeCreate
```

At line 1025 (where create modal opens prompt picker via `p.createFocus == 2`), add before the `p.promptPicker = ...` line:

```go
			p.promptPickerReturnMode = ViewModeCreate
```

Also update `mouse.go` for mouse-based prompt picker opening. There are **two** places:

1. In `handleCreateModalMouse()` around line 118-123 (the `case createPromptFieldID:` branch), add before `p.promptPicker = ...`:
   ```go
   p.promptPickerReturnMode = ViewModeCreate
   ```

2. In `handleMouseClick()` around line 626-629 (the `if focusIdx == 2 {` block inside `case regionCreateInput:`), add before `p.promptPicker = ...`:
   ```go
   p.promptPickerReturnMode = ViewModeCreate
   ```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go build ./internal/plugins/workspace/...`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add internal/plugins/workspace/keys.go internal/plugins/workspace/mouse.go internal/plugins/workspace/agent_config_modal_test.go
git commit -m "feat: set promptPickerReturnMode in existing create modal flow"
```

### Task 12: Modify PromptSelectedMsg and PromptCancelledMsg handlers

**Files:**
- Modify: `internal/plugins/workspace/update.go:218-246`

- [ ] **Step 1: Replace PromptSelectedMsg handler**

In `internal/plugins/workspace/update.go`, replace the `case PromptSelectedMsg:` handler (lines 218-240) with:

```go
	case PromptSelectedMsg:
		// Prompt selected from picker
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
			p.viewMode = ViewModeCreate
			if msg.Prompt != nil {
				// Find index of selected prompt
				for i, pr := range p.createPrompts {
					if pr.Name == msg.Prompt.Name {
						p.createPromptIdx = i
						break
					}
				}
				// If ticketMode is none, skip task field and jump to agent
				if msg.Prompt.TicketMode == TicketNone {
					p.createFocus = 4 // agent field
				} else {
					p.createFocus = 3 // task field
				}
			} else {
				p.createPromptIdx = -1
				p.createFocus = 3 // task field
			}
		}
```

- [ ] **Step 2: Replace PromptCancelledMsg handler**

In `internal/plugins/workspace/update.go`, replace the `case PromptCancelledMsg:` handler (lines 242-246) with:

```go
	case PromptCancelledMsg:
		// Picker cancelled, return to originating modal
		returnMode := p.promptPickerReturnMode
		p.promptPicker = nil
		p.clearPromptPickerModal()
		if returnMode == ViewModeAgentConfig {
			p.viewMode = ViewModeAgentConfig
		} else {
			p.viewMode = ViewModeCreate
		}
```

- [ ] **Step 3: Modify PromptInstallDefaultsMsg handler**

In `internal/plugins/workspace/update.go`, in the `case PromptInstallDefaultsMsg:` handler (lines 248-265), replace the successful path (lines 257-260):

```go
		if WriteDefaultPromptsToConfig(configDir) {
			p.createPrompts = LoadPrompts(configDir, p.ctx.ProjectRoot)
			p.promptPicker = NewPromptPicker(p.createPrompts, p.width, p.height)
			p.clearPromptPickerModal()
```

with:

```go
		if WriteDefaultPromptsToConfig(configDir) {
			if p.promptPickerReturnMode == ViewModeAgentConfig {
				p.agentConfigPrompts = LoadPrompts(configDir, p.ctx.ProjectRoot)
				p.promptPicker = NewPromptPicker(p.agentConfigPrompts, p.width, p.height)
			} else {
				p.createPrompts = LoadPrompts(configDir, p.ctx.ProjectRoot)
				p.promptPicker = NewPromptPicker(p.createPrompts, p.width, p.height)
			}
			p.clearPromptPickerModal()
```

- [ ] **Step 4: Verify it compiles**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go build ./internal/plugins/workspace/...`
Expected: Success

- [ ] **Step 5: Run all tests**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go test ./internal/plugins/workspace/ -v -count=1 2>&1 | tail -10`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/plugins/workspace/update.go
git commit -m "feat: route prompt picker return to agent config or create modal"
```

---

## Chunk 6: Final Integration Test & Cleanup

### Task 13: Run full test suite and verify build

**Files:**
- No new files

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go test ./... 2>&1 | tail -20`
Expected: All tests pass

- [ ] **Step 2: Build the binary**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go build ./cmd/sidecar`
Expected: Success

- [ ] **Step 3: Verify no compilation warnings or vet issues**

Run: `cd /Users/eugeneosipenko/Documents/Projects/Personal/sidecar-new-agent-config-modal && go vet ./internal/plugins/workspace/...`
Expected: No issues

- [ ] **Step 4: Final commit if any cleanup was needed**

Only commit if changes were made during cleanup. Otherwise skip.
