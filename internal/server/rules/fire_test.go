package rules

import (
	"math/rand"
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

func TestFireNeighborShapeKeepsAgeAndUnsupportedInput(t *testing.T) {
	requested := registry.MustState("minecraft:fire", map[string]string{"age": "11", "east": "true"})
	for _, tc := range []struct {
		name string
		w    testWorld
		want uint16
	}{
		{
			"floor clears lean",
			testWorld{{0, 1, 0}: registry.BlockNetherrack.DefaultState()},
			registry.MustState("minecraft:fire", map[string]string{"age": "11"}),
		},
		{
			"fuel sets lean",
			testWorld{{-1, 2, 0}: registry.BlockOakPlanks.DefaultState()},
			registry.MustState("minecraft:fire", map[string]string{"age": "11", "west": "true"}),
		},
		{"unsupported keeps input", testWorld{}, requested},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NeighborShapedState(tc.w, nil, requested, 0, 2, 0); got != tc.want {
				t.Errorf("shaped fire = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestFireCanBurnReadsTheTable checks the fuel test the survival and the
// lean flags both read: the flammability table's ignite odds, and the
// waterlogged rule the getter applies over them.
func TestFireCanBurnReadsTheTable(t *testing.T) {
	if !blocklogic.FireCanBurn(defaultState(t, "minecraft:oak_planks")) {
		t.Error("oak planks were not fuel")
	}
	if blocklogic.FireCanBurn(defaultState(t, "minecraft:stone")) {
		t.Error("stone was fuel")
	}
	waterlogged, ok := registry.FindState("minecraft:oak_slab", map[string]string{"waterlogged": "true"})
	if !ok {
		t.Fatal("the block registry has no waterlogged oak slab")
	}
	if blocklogic.FireCanBurn(waterlogged) {
		t.Error("a waterlogged slab was fuel")
	}
	dry, ok := registry.FindState("minecraft:oak_slab", map[string]string{"waterlogged": "false"})
	if !ok {
		t.Fatal("the block registry has no dry oak slab")
	}
	if !blocklogic.FireCanBurn(dry) {
		t.Error("a dry oak slab was not fuel")
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

// TestExplosionFireCellsRollsOneInThree pins the odds over a long row of
// cleared cells on solid floor, and that a cell over air, or over a floor
// that is not a solid-render block, never burns.
func TestExplosionFireCellsRollsOneInThree(t *testing.T) {
	const n = 3000
	stone := defaultState(t, "minecraft:stone")
	soulSand := defaultState(t, "minecraft:soul_sand")
	glass := defaultState(t, "minecraft:glass")
	for _, tc := range []struct {
		name  string
		floor uint16
	}{{"stone", stone}, {"soul sand", soulSand}} {
		t.Run(tc.name, func(t *testing.T) {
			w := fireWorld{testWorld{}}
			var cells [][3]int32
			for x := range int32(n) {
				w.set(x, 0, 0, tc.floor)
				cells = append(cells, [3]int32{x, 1, 0})
			}
			lit := len(ExplosionFireCells(w, rand.New(rand.NewSource(7)), cells))
			if lit < n/3-150 || lit > n/3+150 {
				t.Errorf("%d of %d cells burned, want about a third", lit, n)
			}
		})
	}
	w := fireWorld{testWorld{}}
	var cells [][3]int32
	for x := range int32(n) {
		w.set(x, 0, 0, glass)
		cells = append(cells, [3]int32{x, 1, 0}, [3]int32{x, 5, 0})
	}
	if got := ExplosionFireCells(w, rand.New(rand.NewSource(7)), cells); len(got) != 0 {
		t.Errorf("%d cells burned over glass or air, want none", len(got))
	}
}
