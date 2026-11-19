package entity

import (
	"slices"
	"testing"
)

// TestAFastOrbIsLitByALavaCellItCrossed: an orb moving three blocks in one
// tick over a single lava cell, from air to air, catches fire from it.
// vanilla: Entity.checkInsideBlocks
func TestAFastOrbIsLitByALavaCellItCrossed(t *testing.T) {
	w := newTestWorld()
	w.set(4, 65, 0, defaultState(t, "minecraft:lava"))
	o := NewOrb(1, 2.5, 65, 0.5, 3, 0, 0, 1, 1)
	o.Step(w, nil, func(*Orb) []*Orb { return nil })
	if o.X-orbHalfSize <= 5 {
		t.Fatalf("orb at x=%v, want its box past the lava cell", o.X)
	}
	if o.fireTicks <= 0 {
		t.Errorf("fire ticks = %d after crossing the lava, want lit", o.fireTicks)
	}
}

// TestAFastBoatBreaksALilyPadItCrossed: a hull moving clean over a cell
// within one tick finds the lily pad there, whose cell neither its start nor
// its end box covers.
// vanilla: Entity.checkInsideBlocks, LilyPadBlock.entityInside
func TestAFastBoatBreaksALilyPadItCrossed(t *testing.T) {
	w := newTestWorld()
	w.set(4, 64, 0, lilyPadState)
	b := NewBoat(1, 2.5, 64.1, 0.5, 0) // over the pad's 1.5/16 collision, in its cell
	b.NoGravity = true
	b.VX = 4
	b.Step(w, false)
	if b.X-BoatHalfWidth <= 5 {
		t.Fatalf("boat at x=%v, want its box past the pad's cell", b.X)
	}
	pads, _ := b.InsideBlocks(w, 2.5, 64.1, 0.5, b.VX, b.VZ, nil, nil)
	if !slices.Equal(pads, []LilyPadCell{{4, 64, 0}}) {
		t.Errorf("pads = %v, want the one crossed at (4, 64, 0)", pads)
	}
}
