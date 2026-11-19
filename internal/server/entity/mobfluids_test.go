package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The mob's fluid machinery as ordinary unit tests, the way the item
// simulation's water is run: each case steps a mob against a world that is
// nothing but the blocks it sets, and asserts on the numbers the vanilla
// parts run on — the depth the float goal draws its kicks at, the hops the
// jump block turns them into, the push a current gives. The black-box
// tests in internal/server/*_test.go remain the proof of a mob the server actually
// spawns and shows around.

// pool builds the standing water a bobbing test swims in: a five-by-five
// room of water six blocks deep on a stone floor, walled in so nobody
// wanders out of it, and the height of the water's surface, the top
// source cell's own eight ninths over its floor.
func pool(t *testing.T) (w testWorld, surface float64) {
	t.Helper()
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w = newTestWorld()
	floor(w, -3, -3, 3, 3, 63, stone)
	for y := int32(64); y <= 69; y++ {
		for x := int32(-2); x <= 2; x++ {
			for z := int32(-2); z <= 2; z++ {
				w.set(x, y, z, water)
			}
		}
	}
	for y := int32(64); y <= 72; y++ {
		for i := int32(-3); i <= 3; i++ {
			w.set(i, y, -3, stone)
			w.set(i, y, 3, stone)
			w.set(-3, y, i, stone)
			w.set(3, y, i, stone)
		}
	}
	return w, 69 + fluidOwnHeight(fluidCellOf(water))
}

// fallingColumn builds a falling column of the given fluid at x=z=0 from
// y=60 to 63, with a stone wall beside it on +x when wall is set.
// vanilla: FlowingFluid.getFlow adds the downward share only beside a
// solid face.
func fallingColumn(t *testing.T, fluid string, wall bool) testWorld {
	t.Helper()
	falling, ok := registry.FindState(fluid, map[string]string{"level": "8"})
	if !ok {
		t.Fatalf("the block registry has no falling %s state", fluid)
	}
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	for y := int32(60); y <= 63; y++ {
		w.set(0, y, 0, falling)
		if wall {
			w.set(1, y, 0, stone)
		}
	}
	w.set(0, 59, 0, stone)
	return w
}

// TestAFallingWaterColumnCarriesAMobDown checks the push the fluid
// interaction gives a body in a waterfall: against a wall the current
// points down the fall and carries the body with it; in open air the
// column has no current, and the body sinks only as still water lets it.
func TestAFallingWaterColumnCarriesAMobDown(t *testing.T) {
	walled := aimless(NewMob(TypeSheep, 1, 0.5, 63, 0.5, 1))
	walled.Tick(fallingColumn(t, "minecraft:water", true), nil)
	if walled.VY >= 0 {
		t.Errorf("vy after one tick in a falling column against a wall = %v, want the current carrying it down", walled.VY)
	}
	if walled.Y >= 63 {
		t.Errorf("y after one tick in a falling column against a wall = %v, want it carried below where it started", walled.Y)
	}

	free := aimless(NewMob(TypeSheep, 2, 0.5, 63, 0.5, 1))
	free.updateMobFluids(fallingColumn(t, "minecraft:water", false))
	if free.VX != 0 || free.VY != 0 || free.VZ != 0 {
		t.Errorf("velocity after the fluid pass in a free-standing column = %v %v %v, want no current", free.VX, free.VY, free.VZ)
	}
}

func TestMobWaterSlowDown(t *testing.T) {
	for _, tc := range []struct {
		kind MobType
		want float64
	}{
		{TypePolarBear, 0.98},
		{TypeSkeletonHorse, 0.96},
		{TypeCow, 0.8},
	} {
		t.Run(tc.kind.Name(), func(t *testing.T) {
			m := NewMob(tc.kind, 1, 0.5, 66, 0.5, 1)
			if got := m.waterSlowDown(); got != tc.want {
				t.Errorf("waterSlowDown() = %v, want %v", got, tc.want)
			}
			w, _ := pool(t)
			m.updateMobFluids(w)
			if !m.InWater() {
				t.Fatal("mob must start in water")
			}
			m.VX, m.VY, m.VZ = 1, 0, -0.5
			m.travelInWater(w, mobGravity, true, m.Y)
			want := float64(float32(tc.want)) // the drag is a float
			if !closeEnough(m.VX, want) || !closeEnough(m.VZ, -0.5*want) {
				t.Errorf("horizontal velocity = (%v, %v), want (%v, %v)", m.VX, m.VZ, want, -0.5*want)
			}
			if !closeEnough(m.X, 1.5) || !closeEnough(m.Z, 0) {
				t.Errorf("position = (%v, %v), want (1.5, 0): drag applies after movement", m.X, m.Z)
			}
			if !closeEnough(m.VY, -mobGravity/16) {
				t.Errorf("vertical velocity = %v, want %v", m.VY, -mobGravity/16)
			}
		})
	}
}

// TestWaterMovementEfficiencyAndDolphinsGraceSpeedAMobInWater: a zombie
// walking the floor of a pool takes the water's 0.02 push and 0.8 drag at
// water_movement_efficiency 0, its own pace and the 0.546 drag at 1, and
// Dolphin's Grace's 0.96 drag over the water's own.
// vanilla: LivingEntity.travelInWater
func TestWaterMovementEfficiencyAndDolphinsGraceSpeedAMobInWater(t *testing.T) {
	t.Parallel()
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, -3, -3, 3, 3, 63, stone)
	for y := int32(64); y <= 66; y++ {
		for x := int32(-2); x <= 2; x++ {
			for z := int32(-2); z <= 2; z++ {
				w.set(x, y, z, water)
			}
		}
	}
	run := func(efficiency float64, grace bool) float64 {
		m := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		m.attrs.SetBase(registry.AttributeWaterMovementEfficiency, efficiency)
		if grace {
			m.effects.Add(effect.Instance{ID: registry.MobEffectDolphinsGrace, Duration: 100})
		}
		m.OnGround = true
		m.walk.dirX, m.walk.dirZ, m.walk.speed = 0, 1, 0.23
		m.updateMobFluids(w)
		if !m.InWater() {
			t.Fatal("the zombie must start in water")
		}
		m.travelInWater(w, mobGravity, true, m.Y)
		return math.Hypot(m.VX, m.VZ)
	}
	for _, tc := range []struct {
		name       string
		efficiency float64
		grace      bool
		want       float64
	}{
		{"plain", 0, false, 0.23 * 0.98 * 0.02 * 0.8},
		{"efficiency 1", 1, false, 0.23 * 0.98 * 0.23 * 0.54600006},
		{"dolphins grace", 0, true, 0.23 * 0.98 * 0.02 * 0.96},
	} {
		// float32 arithmetic, as vanilla's
		if got := run(tc.efficiency, tc.grace); math.Abs(got-tc.want) > 1e-7 {
			t.Errorf("%s: horizontal speed after a tick = %v, want %v", tc.name, got, tc.want)
		}
	}
}
