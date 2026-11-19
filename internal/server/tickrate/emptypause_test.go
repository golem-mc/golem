package tickrate

import "testing"

func TestEmptyPause(t *testing.T) {
	p := NewEmptyPause(1)
	for i := 1; i < 20; i++ {
		if paused, _ := p.Tick(0, false); paused {
			t.Fatalf("paused on empty tick %d, want from 20", i)
		}
	}
	if paused, first := p.Tick(0, false); !paused || !first {
		t.Fatal("not first paused on empty tick 20")
	}
	if paused, first := p.Tick(0, false); !paused || first {
		t.Fatal("tick 21 should be paused but not first")
	}
	if paused, _ := p.Tick(1, false); paused {
		t.Fatal("paused with a player online")
	}
	for range 19 {
		p.Tick(0, false)
	}
	if paused, _ := p.Tick(0, true); paused {
		t.Fatal("paused while sprinting")
	}
	off := NewEmptyPause(0)
	for range 100 {
		if paused, _ := off.Tick(0, false); paused {
			t.Fatal("zero seconds paused")
		}
	}
}

// vanilla: MinecraftServer.tickServer reads the current threshold each turn.
func TestEmptyPauseSetSeconds(t *testing.T) {
	p := NewEmptyPause(2)
	for range 19 {
		p.Tick(0, false)
	}
	p.SetSeconds(1)
	if p.Seconds() != 1 {
		t.Fatalf("seconds = %d, want 1", p.Seconds())
	}
	if paused, first := p.Tick(0, false); !paused || !first {
		t.Fatal("lowering the threshold should pause on empty tick 20")
	}
	p.SetSeconds(2)
	if paused, _ := p.Tick(0, false); paused {
		t.Fatal("raising the threshold should resume the world")
	}
	p.SetSeconds(0)
	for range 30 {
		if paused, _ := p.Tick(0, false); paused {
			t.Fatal("disabling the pause should resume the world")
		}
	}
	p.SetSeconds(2)
	for range 18 {
		if paused, _ := p.Tick(0, false); paused {
			t.Fatal("disabled turns must not advance the empty counter")
		}
	}
	if paused, first := p.Tick(0, false); !paused || !first {
		t.Fatal("reenabling the pause should retain the empty counter")
	}
	if paused, _ := p.Tick(1, false); paused {
		t.Fatal("a joining player should resume the world")
	}
}
