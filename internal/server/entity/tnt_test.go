package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestPrimedTNTRestsOnTheBlocksCollisionShape checks that the body's fall is
// clipped against each block's collision shape rather than the full cell.
// vanilla: Entity.move
func TestPrimedTNTRestsOnTheBlocksCollisionShape(t *testing.T) {
	cases := []struct {
		block string
		feet  float64
	}{
		{"minecraft:white_carpet", 63.0625},
		{"minecraft:oak_slab", 63.5},
		{"minecraft:oak_fence", 64.5},
		{"minecraft:stone", 64},
	}
	for _, c := range cases {
		t.Run(c.block, func(t *testing.T) {
			w := newTestWorld()
			w.set(4, 63, 2, registry.MustState(c.block, nil))
			tnt := NewTNT(1, 4.5, 67, 2.5, 0, 0, 0, 1000, 0, 0)
			for range 60 {
				tnt.Step(&w)
			}
			if math.Abs(tnt.Y-c.feet) > 1e-9 {
				t.Errorf("TNT feet on %s = %v, want %v", c.block, tnt.Y, c.feet)
			}
		})
	}
}

// TestPrimedTNTDriftsDownAFlowingChannel checks that a water channel whose
// level rises along +x carries a primed TNT downstream, and that the fuse's
// last tick takes no push.
// vanilla: PrimedTnt.tick, Entity.updateFluidInteraction
func TestPrimedTNTDriftsDownAFlowingChannel(t *testing.T) {
	w := newTestWorld()
	for x := int32(-2); x <= 14; x++ {
		for z := int32(-1); z <= 1; z++ {
			w.set(x, 63, z, registry.MustState("minecraft:stone", nil))
		}
	}
	for x := range int32(7) {
		w.set(x, 64, 0, registry.MustState("minecraft:water", map[string]string{"level": string(rune('1' + x))}))
	}
	tnt := NewTNT(1, 2.5, 64, 0.5, 0, 0, 0, 1000, 0, 0)
	for range 60 {
		tnt.Step(&w)
	}
	if tnt.X < 3.5 {
		t.Errorf("TNT x after 60 ticks in the flow = %v, want it drifted downstream of 2.5", tnt.X)
	}

	last := NewTNT(2, 2.5, 64, 0.5, 0, 0, 0, 1, 0, 0)
	last.Step(&w)
	if last.VX != 0 {
		t.Errorf("TNT on its last fuse tick took a push of %v", last.VX)
	}
}

// TestPrimedTNTInTheVoidStillExplodesOnItsFuse checks that a body far below
// the dimension's bottom is not discarded and blasts when the fuse runs out.
// vanilla: PrimedTnt.tick
func TestPrimedTNTInTheVoidStillExplodesOnItsFuse(t *testing.T) {
	w := newTestWorld()
	tnt := NewTNT(1, 0.5, float64(w.MinY())-200, 0.5, 0, 0, 0, 5, 0, 0)
	var events []TNTEvent
	for i := range 5 {
		if tnt.Dead {
			t.Fatalf("TNT dead after %d ticks, before its fuse ran out", i)
		}
		events = tnt.Step(&w)
	}
	if len(events) != 1 || events[0].Kind != TntExploded || !tnt.Dead {
		t.Fatalf("events = %v, dead = %v; want one explosion on the last fuse tick", events, tnt.Dead)
	}
}

// TestATNTBlastsWithItsOwnPower checks the blast event carries the body's power.
// vanilla: PrimedTnt.explode
func TestATNTBlastsWithItsOwnPower(t *testing.T) {
	w := newTestWorld()
	tnt := NewTNT(1, 2.5, 64, 0.5, 0, 0, 0, 1, 0, 0)
	tnt.Power = 8
	events := tnt.Step(&w)
	if len(events) != 1 || events[0].Kind != TntExploded || events[0].Radius != 8 {
		t.Fatalf("events = %+v, want one TntExploded with Radius 8", events)
	}
}
