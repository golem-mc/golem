package entity

import (
	"testing"
)

// TestLlamaSpitFliesThenFalls: the tick moves by the velocity it began with,
// then drags and drops it.
// vanilla: LlamaSpit.tick
func TestLlamaSpitFliesThenFalls(t *testing.T) {
	w := newTestWorld()
	spit := NewThrown(1, ThrownLlamaSpit, 0, 64, 0, 0, 0, 1.5, 0, 3)
	spit.Step(w, nil, nil)
	if !closeEnough(spit.Z, 1.5) {
		t.Errorf("z = %v after one tick, want the undragged 1.5", spit.Z)
	}
	if !closeEnough(spit.VZ, 1.5*llamaSpitDrag) || !closeEnough(spit.VY, -llamaSpitGravity) {
		t.Errorf("velocity z %v y %v, want 1.5 dragged and 0.06 down", spit.VZ, spit.VY)
	}
	if spit.Dead {
		t.Error("spit in open air ended")
	}
}
