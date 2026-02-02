package ui

import (
	"strings"
	"testing"
)

func TestNewBrailleSpinner(t *testing.T) {
	s := NewBrailleSpinner()
	if s.IsActive() {
		t.Error("expected spinner to be inactive by default")
	}
}

func TestBrailleSpinnerStartStop(t *testing.T) {
	s := NewBrailleSpinner()
	s.Start()
	if !s.IsActive() {
		t.Error("expected active after Start()")
	}
	s.Stop()
	if s.IsActive() {
		t.Error("expected inactive after Stop()")
	}
}

func TestBrailleSpinnerView(t *testing.T) {
	s := NewBrailleSpinner()
	s.Start()
	view := s.View()
	if len(view) == 0 {
		t.Error("expected non-empty view when active")
	}
	if !strings.ContainsAny(view, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Error("expected braille characters in view")
	}
}

func TestBrailleSpinnerViewInactive(t *testing.T) {
	s := NewBrailleSpinner()
	view := s.View()
	if view != "" {
		t.Error("expected empty view when inactive")
	}
}

func TestBrailleSpinnerAnimation(t *testing.T) {
	s := NewBrailleSpinner()
	s.Start()
	view1 := s.View()
	s.Tick()
	view2 := s.View()
	if view1 == view2 {
		t.Error("expected different views after tick")
	}
}

func TestBrailleSpinnerTickWhenStopped(t *testing.T) {
	s := NewBrailleSpinner()
	s.Tick() // should be a no-op
	if s.frame != 0 {
		t.Error("tick should not advance frame when inactive")
	}
}

func TestBrailleSpinnerViewFill(t *testing.T) {
	s := NewBrailleSpinner()
	s.Start()
	view := s.ViewFill(40, "Loading...")
	if !strings.Contains(view, "Loading...") {
		t.Error("expected label in ViewFill output")
	}
}
