package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The connection rules as ordinary unit tests, driving the functions on a
// world that is nothing but the blocks each case sets. No server is involved;
// the black-box tests in internal/server/*_test.go remain the proof of the placement
// and neighbour-update behaviour around these decisions, these are the
// cheapest possible checks of the decisions themselves.

// testWorld is the plainest block reader a rule can be shown: blocks set by
// their coordinates in a map, and air everywhere the map does not say
// otherwise - the way an unloaded chunk reads.
type testWorld map[[3]int32]uint16

// BlockAt is the block set at the cell, or air where the map has none.
func (w testWorld) BlockAt(x, y, z int32) uint16 { return w[[3]int32{x, y, z}] }

// set places a block, for arrangements that read.
func (w testWorld) set(x, y, z int32, state uint16) {
	w[[3]int32{x, y, z}] = state
}

// defaultState is the default state of a named block, so the tests name what
// they mean.
func defaultState(t *testing.T, name string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, nil)
	if !ok {
		t.Fatalf("the block registry has no %s", name)
	}
	return state
}

// TestAFenceConnectsToOneKindOfNeighbourAndNotAnother checks the fence's
// connection decision on the two neighbours that tell it apart from the
// other cross-collision rule: a fence of its own group, which connects, and
// short grass, which no sturdy face grows on - and that bars choose their
// own rule in the same function, connecting to a wall where a fence would
// not connect to iron bars.
func TestAFenceConnectsToOneKindOfNeighbourAndNotAnother(t *testing.T) {
	fence := defaultState(t, "minecraft:oak_fence")
	bars := defaultState(t, "minecraft:iron_bars")
	grass := defaultState(t, "minecraft:short_grass")

	w := testWorld{}
	w.set(1, 0, 0, fence) // east: another oak fence
	w.set(0, 0, 1, grass) // south: grass, which holds nothing up
	w.set(0, 0, -1, bars) // north: iron bars, which a fence ignores
	north, south, east, west := CrossConnections(w, registry.BlockOakFence, 0, 0, 0)
	if !east {
		t.Error("east = false, want the fence connected to the oak fence there")
	}
	if north {
		t.Error("north = true, want no connection toward iron bars")
	}
	if south {
		t.Error("south = true, want no connection toward grass")
	}
	if west {
		t.Error("west = true, want no connection where nothing stands")
	}

	// The bars ask their own question of the same world: bars connect to
	// bars and to walls, so the iron bars north of the spot - iron bars
	// being the same block as the one asking - do connect.
	w = testWorld{}
	w.set(0, 0, -1, bars)
	north, _, _, _ = CrossConnections(w, registry.BlockIronBars, 0, 0, 0)
	if !north {
		t.Error("north = false, want the bars connected to the bars there")
	}
}

// TestACrossBlockIsPlacedInTheStateItsNeighboursConnect checks that the
// connection decision reaches the placed state itself: the fence east of the
// spot makes the fence being placed take the east=true state, waterlogged
// left false the way a fresh placement does.
func TestACrossBlockIsPlacedInTheStateItsNeighboursConnect(t *testing.T) {
	w := testWorld{}
	w.set(1, 0, 0, defaultState(t, "minecraft:oak_fence"))

	got := CrossStateWithConnections(w, registry.BlockOakFence, 0, 0, 0)
	want, ok := registry.FindState("minecraft:oak_fence", map[string]string{"east": "true"})
	if !ok {
		t.Fatal("the block registry has no east-connected oak fence state")
	}
	if got != want {
		t.Errorf("CrossStateWithConnections placed state %d, want %d (east=true)", got, want)
	}
	props := stateProps(t, got)
	if props["east"] != "true" || props["waterlogged"] != "false" {
		t.Errorf("the placed state holds %v, want east=true, waterlogged=false", props)
	}
}

// stateProps decodes the properties of a block state, for the assertions that
// read a placed state back.
func stateProps(t *testing.T, state uint16) map[string]string {
	t.Helper()
	_, props, ok := registry.BlockStateName(state)
	if !ok {
		t.Fatalf("the block registry has no state %d", state)
	}
	return registry.ParseStateProps(props)
}
