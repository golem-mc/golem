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

// TestRainPutsOutABoatTouchingLavaOrFire checks that ignition cannot win
// over rain, while a roof keeps the hull burning.
// vanilla: Entity.applyEffectsFromBlocks
func TestRainPutsOutABoatTouchingLavaOrFire(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"minecraft:lava", "minecraft:fire"} {
		for _, roof := range []bool{false, true} {
			w := newTestWorld()
			w.rain = true
			w.set(0, 0, 0, registry.MustState(block, nil))
			if roof {
				w.set(0, 2, 0, registry.MustState("minecraft:stone", nil))
			}
			b := NewBoat(1, 0.5, 0.5, 0.5, 0)
			b.NoGravity = true
			b.Invulnerable = true
			b.FireTicks, b.Burning = 100, true
			stepBurningBoat(b, w, false)
			if (b.FireTicks > 0) != roof || (b.Extinguishes > 0) == roof {
				t.Errorf("%s (roof %v): FireTicks=%d Extinguishes=%v", block, roof, b.FireTicks, b.Extinguishes)
			}
			if !roof {
				stepBurningBoat(b, w, false)
				if b.FireTicks != 0 || b.Extinguishes > 0 {
					t.Errorf("%s in continuing rain: FireTicks=%d Extinguishes=%v, want no burn or repeated extinguish sound", block, b.FireTicks, b.Extinguishes)
				}
			}
		}
	}
}

// vanilla: Entity.baseTick
func TestBoatPeriodicBurnUsesCurrentLavaContact(t *testing.T) {
	t.Parallel()
	for _, ridden := range []bool{false, true} {
		for _, entering := range []bool{false, true} {
			w := newTestWorld()
			lava := registry.MustState("minecraft:lava", nil)
			if !entering {
				w.set(0, 0, 0, lava)
			}
			b := NewBoat(1, 0.5, 0.5, 0.5, 0)
			b.NoGravity, b.Invulnerable = true, true
			stepBurningBoat(b, w, ridden)
			b.Invulnerable = false
			b.FireTicks = 300
			if entering {
				w.set(0, 0, 0, lava)
			} else {
				w.set(0, 0, 0, 0)
			}
			b.Step(w, ridden)
			want := float64(10)
			if entering {
				want = 0
			}
			if b.HurtDamage != want {
				t.Errorf("entering %v, ridden %v: periodic damage = %v, want %v", entering, ridden, b.HurtDamage, want)
			}
			b.Invulnerable = true
			b.InsideBlocks(w, b.X, b.Y, b.Z, 0, 0, nil, nil)
			wantFire := 299
			if entering {
				wantFire = 300
			}
			if b.FireTicks != wantFire {
				t.Errorf("entering %v, ridden %v: fire = %d, want %d", entering, ridden, b.FireTicks, wantFire)
			}
		}
	}
}

// vanilla: BaseFireBlock.entityInside, AbstractBoat.tick
func TestBoatFireContactDamageWhileRidden(t *testing.T) {
	t.Parallel()
	for _, ridden := range []bool{false, true} {
		for _, tc := range []struct {
			block  string
			damage float32
		}{
			{"minecraft:fire", 1}, {"minecraft:soul_fire", 2}, {"minecraft:lava", 4},
		} {
			w := newTestWorld()
			w.set(0, 0, 0, registry.MustState(tc.block, nil))
			b := NewBoat(1, 0.5, 0.5, 0.5, 0)
			b.NoGravity = true
			stepBurningBoat(b, w, ridden)
			if b.HurtDamage != float64(tc.damage)*20 {
				t.Errorf("%s, ridden %v: damage %v, want %v", tc.block, ridden, b.HurtDamage, tc.damage*20)
			}
		}
	}
}

func stepBurningBoat(b *Boat, w World, ridden bool) {
	x, y, z := b.X, b.Y, b.Z
	b.Step(w, ridden)
	b.InsideBlocks(w, x, y, z, b.VX, b.VZ, nil, nil)
}
