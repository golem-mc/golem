package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The fire placement rules as ordinary unit tests, on flames lit in the map
// a fireWorld is. The black-box tests in internal/server/server_portals_test.go and
// the engine's own tests in internal/server/world remain the proof of what a lit fire
// then does; these are the checks of where one may stand and the state it
// takes when it does.

// fireWorld is the plainest world a fire rule can be shown: blocks set by
// their coordinates in a map, air everywhere the map does not say otherwise,
// and the void line a floor below the map's ground.
type fireWorld struct {
	testWorld
}

// VoidY is where the void begins, the FireWorld half beyond the blocks.
func (fireWorld) VoidY() int32 { return 0 }

// TestFireSurvivesOnAFloorOrBesideFuel checks vanilla's canSurvive: a sturdy
// top face below, or a flammable neighbour in any of the six directions —
// and neither a stone wall nor the void's neighbourhood keeps one standing.
// A half-height top is no full face: a bottom slab and a cake hold nothing.
func TestFireSurvivesOnAFloorOrBesideFuel(t *testing.T) {
	planks := defaultState(t, "minecraft:oak_planks")
	stone := defaultState(t, "minecraft:stone")

	// On a stone floor: survives.
	w := fireWorld{testWorld{}}
	w.set(0, 1, 0, stone)
	if !FireSurvivesAt(w, 0, 2, 0) {
		t.Error("fire on a stone floor was refused")
	}
	// Beside planks, with nothing below: survives.
	w = fireWorld{testWorld{}}
	w.set(1, 2, 0, planks)
	if !FireSurvivesAt(w, 0, 2, 0) {
		t.Error("fire leaning on planks was refused")
	}
	// A stone wall beside it is not fuel: refused.
	w = fireWorld{testWorld{}}
	w.set(1, 2, 0, stone)
	if FireSurvivesAt(w, 0, 2, 0) {
		t.Error("fire leaning on stone was allowed")
	}
	// A bottom slab's half-height top is not a full face: refused.
	w = fireWorld{testWorld{}}
	w.set(0, 1, 0, defaultState(t, "minecraft:stone_slab"))
	if FireSurvivesAt(w, 0, 2, 0) {
		t.Error("fire on a bottom slab was allowed")
	}
	// A cake's half-height top is not a full face either: refused.
	w = fireWorld{testWorld{}}
	w.set(0, 1, 0, defaultState(t, "minecraft:cake"))
	if FireSurvivesAt(w, 0, 2, 0) {
		t.Error("fire on a cake was allowed")
	}
	// Below the void line: refused whatever is there.
	w = fireWorld{testWorld{}}
	w.set(0, -1, 0, stone)
	if FireSurvivesAt(w, 0, 0, 0) {
		t.Error("fire in the void was allowed")
	}
}

// TestFireStateForPlacement checks the state a lit or spread fire takes: the
// soul fire above a soul-fire base, the plain default where the block below
// is fuel or sturdy, and the lean on whichever fuel-carrying neighbours
// there are where neither holds.
func TestFireStateForPlacement(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	planks := defaultState(t, "minecraft:oak_planks")
	soulSand := defaultState(t, "minecraft:soul_sand")
	plain := FireState
	soul := blocklogic.SoulFire
	leaningWest, ok := registry.FindState("minecraft:fire", map[string]string{"west": "true"})
	if !ok {
		t.Fatal("the block registry has no leaning fire")
	}

	// Above soul sand: the soul fire, whatever else is around.
	w := fireWorld{testWorld{}}
	w.set(0, 1, 0, soulSand)
	w.set(1, 2, 0, planks)
	if got := FireStateForPlacement(w, 0, 2, 0); got != soul {
		t.Errorf("fire above soul sand = %s, want the soul fire", blockLabel(got))
	}
	// Above stone: the plain default.
	w = fireWorld{testWorld{}}
	w.set(0, 1, 0, stone)
	if got := FireStateForPlacement(w, 0, 2, 0); got != plain {
		t.Errorf("fire above stone = %s, want the default", blockLabel(got))
	}
	// Above planks, which are themselves fuel: the plain default.
	w = fireWorld{testWorld{}}
	w.set(0, 1, 0, planks)
	if got := FireStateForPlacement(w, 0, 2, 0); got != plain {
		t.Errorf("fire above planks = %s, want the default", blockLabel(got))
	}
	// In the air beside a planks wall to the west: the lean on that wall
	// and nothing else.
	w = fireWorld{testWorld{}}
	w.set(-1, 2, 0, planks)
	if got := FireStateForPlacement(w, 0, 2, 0); got != leaningWest {
		t.Errorf("fire beside a planks wall = %s, want the west lean", blockLabel(got))
	}
	// In the air with nothing anywhere: the default, every flag down.
	w = fireWorld{testWorld{}}
	if got := FireStateForPlacement(w, 0, 2, 0); got != plain {
		t.Errorf("fire in the open air = %s, want the default", blockLabel(got))
	}
}

// blockLabel names a block state for a failure message; the engine's fire
// tests keep their own, this one standing in for the placement half.
func blockLabel(state uint16) string {
	name, props, ok := registry.BlockStateName(state)
	if !ok {
		return "unknown"
	}
	if props == "" {
		return name
	}
	return name + " " + props
}
