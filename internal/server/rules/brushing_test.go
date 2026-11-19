package rules

import "testing"

func TestABrushInsideTheCooldownCountsNothing(t *testing.T) {
	p := NewBrushProgress()
	if step := p.Brush(100, 1); !step.Counted {
		t.Fatal("the first brush was refused")
	}
	if step := p.Brush(109, 1); step.Counted {
		t.Error("a brush nine ticks on was counted, want the cooldown to refuse it")
	}
	if step := p.Brush(110, 1); !step.Counted {
		t.Error("a brush ten ticks on was refused")
	}
	if p.Count != 2 {
		t.Errorf("count = %d, want 2", p.Count)
	}
}

func TestTheFirstBrushedFaceIsKept(t *testing.T) {
	p := NewBrushProgress()
	p.Brush(0, 3)
	p.Brush(10, 5)
	if p.Face != 3 {
		t.Errorf("face = %d, want 3", p.Face)
	}
}
