package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestABoatCaughtInLavaBurnsAndWaterPutsItOut: lava sets a hull alight for
// 15 s, the flag shows from the next tick, it keeps burning in air and
// water ends it.
// vanilla: Entity.lavaIgnite, Entity.baseTick
func TestABoatCaughtInLavaBurnsAndWaterPutsItOut(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	w.set(0, 0, 0, registry.MustState("minecraft:lava", nil))
	b := NewBoat(1, 0.5, 0, 0.5, 0)
	b.Invulnerable = true
	stepBurningBoat(b, w, false)
	if b.FireTicks != 300 || b.Burning {
		t.Fatalf("first tick in lava: FireTicks=%d Burning=%v, want 300 and the flag still down", b.FireTicks, b.Burning)
	}
	stepBurningBoat(b, w, false)
	if !b.Burning || b.FireTicks < 299 {
		t.Fatalf("second tick: FireTicks=%d Burning=%v, want the flag up", b.FireTicks, b.Burning)
	}
	w.set(0, 0, 0, 0)
	b.Invulnerable = false
	b.FireTicks = 21
	hurt := 0
	for range 3 {
		b.HurtDamage = 0
		stepBurningBoat(b, w, false)
		if b.HurtDamage > 0 {
			hurt++
		}
	}
	if hurt != 1 {
		t.Fatalf("on_fire bites in 3 ticks across the 20th: %d, want 1", hurt)
	}
	for y := int32(-3); y <= 0; y++ {
		w.set(0, y, 0, boatWater)
	}
	stepBurningBoat(b, w, false)
	if b.FireTicks != 0 || b.Extinguishes == 0 {
		t.Fatalf("in water: FireTicks=%d Extinguishes=%v, want the burn out", b.FireTicks, b.Extinguishes)
	}
}

func stepBurningBoat(b *Boat, w World, ridden bool) {
	x, y, z := b.X, b.Y, b.Z
	b.Step(w, ridden)
	b.InsideBlocks(w, x, y, z, b.VX, b.VZ, nil, nil)
}
