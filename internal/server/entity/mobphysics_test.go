package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The walk and shove arithmetic as unit tests of their own: each drives a
// mob against a world that is nothing but the floor it sets and asserts on
// the plain numbers the vanilla parts run on.
// TestAMobWalksAtThePaceItsAttributeSquaresTo holds the walk to vanilla's
// travel arithmetic: the walk's acceleration is the pace the move control
// set multiplied by itself, the body moves with the whole of what it
// carries, and the ground's friction takes its share off after the move —
// a zombie's 0.23 attribute walking out at 0.11654 blocks a tick, the
// figure a vanilla server's own chase traces at.
func TestAMobWalksAtThePaceItsAttributeSquaresTo(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, stone)

	zombie := aimless(NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1))
	// A player within reach keeps the hostile despawn from taking the
	// walker before its walk is measured; its goals are stripped, so the
	// body goes only where the test points it, far short of the player.
	watcher := Player{X: 10.5, Y: 64, Z: 0.5}
	target := 20.5
	zombie.seekTo(w, target, 64, 0.5, 1.0, 0)
	for range 30 {
		zombie.Tick(w, []Player{watcher})
	}
	// The steady pace, measured over the walk that follows the ramp-in.
	was := zombie.X
	for range 40 {
		zombie.Tick(w, []Player{watcher})
	}
	perTick := (zombie.X - was) / 40
	if want := 0.23 * 0.23 / (1 - slipperiness*airDrag); math.Abs(perTick-want) > 0.002 {
		t.Errorf("the zombie's steady walk = %v a tick, want the vanilla %v", perTick, want)
	}
}

// TestBlockExtentReadsTheModelledShapes holds the heights the world's own
// collision boxes give: the families with modelled shapes reach their
// boxes' tops, everything else keeps the generated table's per-state
// word.
func TestBlockExtentReadsTheModelledShapes(t *testing.T) {
	state := func(name string, props map[string]string) uint16 {
		t.Helper()
		s, ok := registry.FindState(name, props)
		if !ok {
			t.Fatalf("no state %s %v", name, props)
		}
		return s
	}
	for _, tc := range []struct {
		name  string
		props map[string]string
		want  float64
	}{
		{"minecraft:oak_fence", nil, 1.5},
		{"minecraft:cobblestone_wall", nil, 1.5},
		{"minecraft:oak_fence_gate", map[string]string{"open": "false"}, 1.5},
		{"minecraft:oak_fence_gate", map[string]string{"open": "true"}, 0},
		{"minecraft:chest", nil, 14.0 / 16},
		{"minecraft:lantern", nil, 9.0 / 16},
		{"minecraft:candle", map[string]string{"candles": "3"}, 6.0 / 16},
		{"minecraft:iron_chain", map[string]string{"axis": "x"}, 9.5 / 16},
		{"minecraft:creeper_head", nil, 0.5},
		// The unmodelled blocks keep the generated table's own word.
		{"minecraft:stone", nil, 1},
		{"minecraft:farmland", nil, 15.0 / 16},
		{"minecraft:white_carpet", nil, 1.0 / 16},
		{"minecraft:soul_sand", nil, 14.0 / 16},
		{"minecraft:torch", nil, 0},
	} {
		if got := blockExtent(state(tc.name, tc.props)); got != tc.want {
			t.Errorf("blockExtent(%s %v) = %v, want %v", tc.name, tc.props, got, tc.want)
		}
	}
}

// TestAMobSlidesFarthestOnIce holds the drag to the block under the feet:
// a shove of 0.4 spends its slide on each floor at the distance its
// friction times the air's allows, about 3.7 blocks on ice against 0.88 on
// stone.
func TestAMobSlidesFarthestOnIce(t *testing.T) {
	for _, tc := range []struct {
		block    string
		min, max float64
	}{
		{"minecraft:stone", 0.8, 0.95},
		{"minecraft:slime_block", 0.5, 0.65},
		{"minecraft:ice", 3.4, 4.0},
		{"minecraft:blue_ice", 3.85, 4.3},
	} {
		w := newTestWorld()
		floor(w, -40, -40, 40, 40, 63, defaultState(t, tc.block))
		cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
		cow.Tick(w, nil)
		cow.Tick(w, nil)
		cow.VX = 0.4
		was := cow.X
		for range 60 {
			cow.Tick(w, nil)
		}
		if got := cow.X - was; got < tc.min || got > tc.max {
			t.Errorf("slide on %s = %v blocks, want within [%v, %v]", tc.block, got, tc.min, tc.max)
		}
	}
}
