package app

import (
	"testing"
)

func TestModalKind_Constants(t *testing.T) {
	// Verify modal constants are defined and have distinct values
	tests := []struct {
		name  string
		modal ModalKind
		want  int
	}{
		{"ModalNone", ModalNone, 0},
		{"ModalPalette", ModalPalette, 1},
		{"ModalHelp", ModalHelp, 2},
		{"ModalUpdate", ModalUpdate, 3},
		{"ModalDiagnostics", ModalDiagnostics, 4},
		{"ModalQuitConfirm", ModalQuitConfirm, 5},
		{"ModalProjectSwitcher", ModalProjectSwitcher, 6},
		{"ModalWorktreeSwitcher", ModalWorktreeSwitcher, 7},
		{"ModalThemeSwitcher", ModalThemeSwitcher, 8},
		{"ModalIssueInput", ModalIssueInput, 9},
		{"ModalIssuePreview", ModalIssuePreview, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.modal) != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, int(tt.modal), tt.want)
			}
		})
	}
}

func TestActiveModal_NoModals(t *testing.T) {
	m := &Model{
		showPalette:        false,
		showHelp:           false,
		updateModalState:   UpdateModalClosed,
		showDiagnostics:    false,
		showQuitConfirm:    false,
		showProjectSwitcher: false,
		showWorktreeSwitcher: false,
		showThemeSwitcher:  false,
		showIssueInput:     false,
		showIssuePreview:   false,
	}

	result := m.activeModal()
	if result != ModalNone {
		t.Errorf("activeModal() = %d, want %d (ModalNone)", result, ModalNone)
	}
}

func TestActiveModal_PaletteHighestPriority(t *testing.T) {
	m := &Model{
		showPalette:        true,
		showHelp:           true,
		updateModalState:   UpdateModalClosed,
		showDiagnostics:    true,
		showQuitConfirm:    true,
		showProjectSwitcher: true,
		showWorktreeSwitcher: true,
		showThemeSwitcher:  true,
		showIssueInput:     true,
		showIssuePreview:   true,
	}

	result := m.activeModal()
	if result != ModalPalette {
		t.Errorf("activeModal() = %d, want %d (ModalPalette)", result, ModalPalette)
	}
}

func TestActiveModal_HelpPriority(t *testing.T) {
	m := &Model{
		showPalette:        false,
		showHelp:           true,
		updateModalState:   UpdateModalClosed,
		showDiagnostics:    true,
		showQuitConfirm:    true,
		showProjectSwitcher: true,
		showWorktreeSwitcher: true,
		showThemeSwitcher:  true,
		showIssueInput:     true,
		showIssuePreview:   true,
	}

	result := m.activeModal()
	if result != ModalHelp {
		t.Errorf("activeModal() = %d, want %d (ModalHelp)", result, ModalHelp)
	}
}

func TestActiveModal_UpdateModalPriority(t *testing.T) {
	m := &Model{
		showPalette:        false,
		showHelp:           false,
		updateModalState:   UpdateModalPreview,
		showDiagnostics:    true,
		showQuitConfirm:    true,
		showProjectSwitcher: true,
		showWorktreeSwitcher: true,
		showThemeSwitcher:  true,
		showIssueInput:     true,
		showIssuePreview:   true,
	}

	result := m.activeModal()
	if result != ModalUpdate {
		t.Errorf("activeModal() = %d, want %d (ModalUpdate)", result, ModalUpdate)
	}
}

func TestActiveModal_DiagnosticsPriority(t *testing.T) {
	m := &Model{
		showPalette:        false,
		showHelp:           false,
		updateModalState:   UpdateModalClosed,
		showDiagnostics:    true,
		showQuitConfirm:    true,
		showProjectSwitcher: true,
		showWorktreeSwitcher: true,
		showThemeSwitcher:  true,
		showIssueInput:     true,
		showIssuePreview:   true,
	}

	result := m.activeModal()
	if result != ModalDiagnostics {
		t.Errorf("activeModal() = %d, want %d (ModalDiagnostics)", result, ModalDiagnostics)
	}
}

func TestActiveModal_QuitConfirmPriority(t *testing.T) {
	m := &Model{
		showPalette:        false,
		showHelp:           false,
		updateModalState:   UpdateModalClosed,
		showDiagnostics:    false,
		showQuitConfirm:    true,
		showProjectSwitcher: true,
		showWorktreeSwitcher: true,
		showThemeSwitcher:  true,
		showIssueInput:     true,
		showIssuePreview:   true,
	}

	result := m.activeModal()
	if result != ModalQuitConfirm {
		t.Errorf("activeModal() = %d, want %d (ModalQuitConfirm)", result, ModalQuitConfirm)
	}
}

func TestActiveModal_ProjectSwitcherPriority(t *testing.T) {
	m := &Model{
		showPalette:        false,
		showHelp:           false,
		updateModalState:   UpdateModalClosed,
		showDiagnostics:    false,
		showQuitConfirm:    false,
		showProjectSwitcher: true,
		showWorktreeSwitcher: true,
		showThemeSwitcher:  true,
		showIssueInput:     true,
		showIssuePreview:   true,
	}

	result := m.activeModal()
	if result != ModalProjectSwitcher {
		t.Errorf("activeModal() = %d, want %d (ModalProjectSwitcher)", result, ModalProjectSwitcher)
	}
}

func TestActiveModal_WorktreeSwitcherPriority(t *testing.T) {
	m := &Model{
		showPalette:        false,
		showHelp:           false,
		updateModalState:   UpdateModalClosed,
		showDiagnostics:    false,
		showQuitConfirm:    false,
		showProjectSwitcher: false,
		showWorktreeSwitcher: true,
		showThemeSwitcher:  true,
		showIssueInput:     true,
		showIssuePreview:   true,
	}

	result := m.activeModal()
	if result != ModalWorktreeSwitcher {
		t.Errorf("activeModal() = %d, want %d (ModalWorktreeSwitcher)", result, ModalWorktreeSwitcher)
	}
}

func TestActiveModal_ThemeSwitcherPriority(t *testing.T) {
	m := &Model{
		showPalette:        false,
		showHelp:           false,
		updateModalState:   UpdateModalClosed,
		showDiagnostics:    false,
		showQuitConfirm:    false,
		showProjectSwitcher: false,
		showWorktreeSwitcher: false,
		showThemeSwitcher:  true,
		showIssueInput:     true,
		showIssuePreview:   true,
	}

	result := m.activeModal()
	if result != ModalThemeSwitcher {
		t.Errorf("activeModal() = %d, want %d (ModalThemeSwitcher)", result, ModalThemeSwitcher)
	}
}

func TestActiveModal_IssueInputPriority(t *testing.T) {
	m := &Model{
		showPalette:        false,
		showHelp:           false,
		updateModalState:   UpdateModalClosed,
		showDiagnostics:    false,
		showQuitConfirm:    false,
		showProjectSwitcher: false,
		showWorktreeSwitcher: false,
		showThemeSwitcher:  false,
		showIssueInput:     true,
		showIssuePreview:   true,
	}

	result := m.activeModal()
	if result != ModalIssueInput {
		t.Errorf("activeModal() = %d, want %d (ModalIssueInput)", result, ModalIssueInput)
	}
}

func TestActiveModal_IssuePreviewLowest(t *testing.T) {
	m := &Model{
		showPalette:        false,
		showHelp:           false,
		updateModalState:   UpdateModalClosed,
		showDiagnostics:    false,
		showQuitConfirm:    false,
		showProjectSwitcher: false,
		showWorktreeSwitcher: false,
		showThemeSwitcher:  false,
		showIssueInput:     false,
		showIssuePreview:   true,
	}

	result := m.activeModal()
	if result != ModalIssuePreview {
		t.Errorf("activeModal() = %d, want %d (ModalIssuePreview)", result, ModalIssuePreview)
	}
}

func TestHasModal_WithNoModals(t *testing.T) {
	m := &Model{
		showPalette:        false,
		showHelp:           false,
		updateModalState:   UpdateModalClosed,
		showDiagnostics:    false,
		showQuitConfirm:    false,
		showProjectSwitcher: false,
		showWorktreeSwitcher: false,
		showThemeSwitcher:  false,
		showIssueInput:     false,
		showIssuePreview:   false,
	}

	result := m.hasModal()
	if result {
		t.Errorf("hasModal() = true, want false")
	}
}

func TestHasModal_WithPalette(t *testing.T) {
	m := &Model{
		showPalette: true,
	}

	result := m.hasModal()
	if !result {
		t.Errorf("hasModal() = false, want true")
	}
}

func TestHasModal_WithHelp(t *testing.T) {
	m := &Model{
		showHelp: true,
	}

	result := m.hasModal()
	if !result {
		t.Errorf("hasModal() = false, want true")
	}
}

func TestHasModal_WithUpdateModal(t *testing.T) {
	m := &Model{
		updateModalState: UpdateModalPreview,
	}

	result := m.hasModal()
	if !result {
		t.Errorf("hasModal() = false, want true")
	}
}

func TestHasModal_WithDiagnostics(t *testing.T) {
	m := &Model{
		showDiagnostics: true,
	}

	result := m.hasModal()
	if !result {
		t.Errorf("hasModal() = false, want true")
	}
}

func TestTabBounds(t *testing.T) {
	bounds := TabBounds{Start: 10, End: 50}

	if bounds.Start != 10 {
		t.Errorf("Start = %d, want 10", bounds.Start)
	}
	if bounds.End != 50 {
		t.Errorf("End = %d, want 50", bounds.End)
	}
}

func TestModalPriority_ImpactOfUpdateModalClosed(t *testing.T) {
	// When UpdateModalState is UpdateModalClosed, it should NOT activate ModalUpdate
	m := &Model{
		showPalette:      false,
		showHelp:         false,
		updateModalState: UpdateModalClosed,
		showDiagnostics:  false,
	}

	result := m.activeModal()
	if result != ModalNone {
		t.Errorf("With UpdateModalClosed, activeModal() = %d, want %d (ModalNone)", result, ModalNone)
	}
}
