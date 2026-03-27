package app

import (
	"testing"
	"time"
)

func TestIntroModel_Update(t *testing.T) {
	m := NewIntroModel("")

	if !m.Active {
		t.Error("NewIntroModel should be active")
	}

	// Simulate running for a few seconds
	// Total duration depends on last letter delay + travel time
	// Max delay ~ 0.6s. Travel time ~ 1-2s?

	const dt = 16 * time.Millisecond
	timeout := 5 * time.Second
	start := time.Now()

	for !m.Done {
		m.Update(dt)
		if time.Since(start) > timeout {
			t.Fatal("Intro animation timed out")
		}
	}

	if !m.Done {
		t.Error("IntroModel should be done after simulation")
	}

	// Verify final state
	// Letters should be at target positions (0, 1, 2...)

	for i, l := range m.Letters {
		targetX := float64(i)
		if l.CurrentX < targetX-0.1 || l.CurrentX > targetX+0.1 {
			t.Errorf("Letter %d not at target X. Got %f, want %f", i, l.CurrentX, targetX)
		}

		// Verify color is close to end color
		// We can't easily access the interpolated color fields as they are exported but we need to check values
		// TODO: Verify color interpolation when exact match is possible with float precision handling
	}
}
