package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestMovementStatPicksTheFirstMatch walks each case of
// checkMovementStatistics in its order.
func TestMovementStatPicksTheFirstMatch(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dx, dy, dz float64
		m          Mover
		stat       string
		cm         int32
	}{
		{"walked", 3, 0, 4, Mover{OnGround: true}, "minecraft:walk_one_cm", 500},
		{"sprinted", 1, 0, 0, Mover{OnGround: true, Sprinting: true, Crouching: true}, "minecraft:sprint_one_cm", 100},
		{"crouched", 0, 0, 0.3, Mover{OnGround: true, Crouching: true}, "minecraft:crouch_one_cm", 30},
		{"swam counts the rise", 0, 3, 4, Mover{Swimming: true, Water: Submerged}, "minecraft:swim_one_cm", 500},
		{"under water", 0, 1, 0, Mover{Water: Submerged, OnGround: true}, "minecraft:walk_under_water_one_cm", 100},
		{"waded flat", 1, 5, 0, Mover{Water: Wading}, "minecraft:walk_on_water_one_cm", 100},
		{"climbed the rise alone", 1, 0.5, 0, Mover{Climbing: true, OnGround: true}, "minecraft:climb_one_cm", 50},
		{"climbing down is nothing", 0, -1, 0, Mover{Climbing: true, OnGround: true}, "", 0},
		{"glided", 0, -3, 4, Mover{FallFlying: true}, "minecraft:aviate_one_cm", 500},
		{"flew", 0.3, 0, 0, Mover{}, "minecraft:fly_one_cm", 30},
		{"a short hop is not flown", 0.25, 1, 0, Mover{}, "", 0},
		{"stood still", 0, 0, 0, Mover{OnGround: true}, "", 0},
	} {
		key, cm := MovementStat(tc.dx, tc.dy, tc.dz, tc.m)
		stat := ""
		if key != registry.NoCustomStat {
			stat = key.Name()
		}
		if stat != tc.stat || cm != tc.cm {
			t.Errorf("%s: MovementStat = %q %d, want %q %d", tc.name, stat, cm, tc.stat, tc.cm)
		}
	}
}

// TestClimbingIsOffForASpectatorAndAGliderInGlideBlocks: onClimbable is
// false for a spectator and for a glider in a can_glide_through block, even
// in vines.
func TestClimbingIsOffForASpectatorAndAGliderInGlideBlocks(t *testing.T) {
	vine := registry.MustState("minecraft:vine", map[string]string{"north": "true"})
	ladder := registry.MustState("minecraft:ladder", map[string]string{"facing": "north"})
	air := registry.MustState("minecraft:air", nil)
	for _, tc := range []struct {
		name                  string
		feet                  uint16
		spectator, fallFlying bool
		want                  bool
	}{
		{"walker in vines", vine, false, false, true},
		{"spectator in vines", vine, true, false, false},
		{"glider in vines", vine, false, true, false},
		{"glider on a ladder", ladder, false, true, true},
		{"walker in air", air, false, false, false},
	} {
		if got := Climbing(tc.feet, air, tc.spectator, tc.fallFlying); got != tc.want {
			t.Errorf("%s: Climbing = %t, want %t", tc.name, got, tc.want)
		}
	}
}
